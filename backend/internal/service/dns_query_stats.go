package service

import (
	"sort"
	"sync"
	"time"

	"pigate/internal/model"
)

// "Top Queried Domains" pipeline (docs/ref/todo/statistics-dns-top-domain-plan.md
// T-07) — a RAM-only 5-minute bucket ring, structurally identical to the deny
// ring in statistics.go (same span/max, same "mutex + map increment only, no
// I/O" contract on the hot path) but keyed by domain instead of source IP,
// fed by kernel.DNSServerManager.WatchDNSLog via RecordDNSEvent instead of the
// NFLOG watcher.
const (
	// domainBucketSpan/domainBucketMax mirror denyBucketSpan/denyBucketMax so
	// the domain ring covers the same 24h window at the same granularity.
	domainBucketSpan = 5 * time.Minute
	domainBucketMax  = 288

	// maxTrackedDomains bounds a single bucket's domain map the same way
	// maxTrackedDenySources bounds the deny ring — a flood of unique domains
	// (or an attacker deliberately querying garbage names) cannot grow a
	// bucket's map without limit. Not user-configurable (plan §3 T-07: "ค่า
	// คงที่ ไม่เปิดให้ปรับ" — unlike the reverse-cache TTL/cap in T-08).
	maxTrackedDomains = 500
)

// domainBucket is one 5-minute bucket of query counts, keyed by domain name.
// RAM-only, never persisted (plan §0/Caution 16 — dns_server_settings gains
// only 3 config columns, no query/domain table).
type domainBucket struct {
	ts           string
	domainCount  map[string]uint64
	typeByDomain map[string]string
	queries      uint64
}

// dnsQueryStats holds the domain ring + the opt-in switch state. Kept as its
// own struct (embedded into StatisticsService below) purely for file
// organization — the mutex still guards only these fields, same discipline
// as denyBuckets/mu above it.
type dnsQueryStats struct {
	mu            sync.RWMutex
	enabled       bool
	buckets       []domainBucket
	reverseCache  *dnsReverseCache
}

// RecordDNSEvent is the single entry point from the DNS query-log watcher
// (main.go: `go dnsServerManager.WatchDNSLog(ctx, statisticsService.RecordDNSEvent)`).
// It must be O(1), never block, never do I/O and never log the domain itself
// (plan §5 item 2) — it runs directly on the kernel-layer log read loop,
// mirroring RecordFirewallLog's contract above.
func (s *StatisticsService) RecordDNSEvent(ev model.DNSLogEvent) {
	switch ev.Kind {
	case model.DNSLogQuery:
		s.recordDomainQuery(ev.Domain, ev.QueryType)
	case model.DNSLogAnswer:
		if ev.Domain != "" && ev.AnswerIP != "" {
			s.dns.reverseCache.Put(ev.AnswerIP, ev.Domain)
		}
	}
}

func (s *StatisticsService) recordDomainQuery(domain, qtype string) {
	if domain == "" {
		return
	}
	ts := time.Now().Truncate(domainBucketSpan).Format(time.RFC3339)

	s.dns.mu.Lock()
	defer s.dns.mu.Unlock()
	if !s.dns.enabled {
		// Defense in depth: RecordDNSEvent is only wired up while the switch
		// is on, but a race between disabling and an in-flight event must
		// still not leak a data point into the ring.
		return
	}

	var b *domainBucket
	if n := len(s.dns.buckets); n > 0 && s.dns.buckets[n-1].ts == ts {
		b = &s.dns.buckets[n-1]
	} else {
		s.dns.buckets = append(s.dns.buckets, domainBucket{
			ts:           ts,
			domainCount:  make(map[string]uint64),
			typeByDomain: make(map[string]string),
		})
		if len(s.dns.buckets) > domainBucketMax {
			s.dns.buckets = s.dns.buckets[len(s.dns.buckets)-domainBucketMax:]
		}
		b = &s.dns.buckets[len(s.dns.buckets)-1]
	}

	b.queries++
	if _, exists := b.domainCount[domain]; exists || len(b.domainCount) < maxTrackedDomains {
		b.domainCount[domain]++
		if qtype != "" {
			b.typeByDomain[domain] = qtype
		}
	}
}

// SetDNSLoggingEnabled toggles the opt-in switch (mirrors
// DNSServerSettings.QueryLogging). Turning it off clears both the domain ring
// and the reverse cache immediately (plan §5 item 1 — privacy: no lingering
// history once the user opts out).
func (s *StatisticsService) SetDNSLoggingEnabled(enabled bool) {
	s.dns.mu.Lock()
	s.dns.enabled = enabled
	s.dns.mu.Unlock()
	if !enabled {
		s.ClearDNSStats()
	}
}

// ClearDNSStats wipes the domain ring and reverse cache. Called when the
// query-logging switch is turned off, and available for tests/handlers that
// need a clean slate.
func (s *StatisticsService) ClearDNSStats() {
	s.dns.mu.Lock()
	s.dns.buckets = nil
	s.dns.mu.Unlock()
	s.dns.reverseCache.Clear()
}

// SetReverseCacheLimits forwards to the reverse cache's SetLimits — see
// dns_reverse_cache.go for the clamp/evict-immediately contract (plan T-08).
func (s *StatisticsService) SetReverseCacheLimits(ttlMinutes, maxEntries int) {
	s.dns.reverseCache.SetLimits(ttlMinutes, maxEntries)
}

// domainSnapshot aggregates the domain ring's per-domain query counts over
// the requested window, mirroring denySnapshot's bucket-selection logic.
func (s *StatisticsService) domainSnapshot(window string) (totals map[string]uint64, typeByDomain map[string]string, totalQueries uint64, enabled bool, truncated bool) {
	totals = make(map[string]uint64)
	typeByDomain = make(map[string]string)

	s.dns.mu.RLock()
	defer s.dns.mu.RUnlock()

	enabled = s.dns.enabled

	var windowBuckets []domainBucket
	if window == trafficWindow1h {
		n := trafficWindow1hBuckets
		if len(s.dns.buckets) < n {
			n = len(s.dns.buckets)
		}
		windowBuckets = s.dns.buckets[len(s.dns.buckets)-n:]
	} else {
		windowBuckets = s.dns.buckets
	}

	for _, b := range windowBuckets {
		for k, v := range b.domainCount {
			totals[k] += v
		}
		for k, v := range b.typeByDomain {
			typeByDomain[k] = v
		}
		totalQueries += b.queries
		if len(b.domainCount) >= maxTrackedDomains {
			truncated = true
		}
	}
	return
}

// buildTopDomains ranks the domain snapshot into TopDomain rows. Sort is
// deterministic (count desc, then domain asc) mirroring every other Top*
// builder in this package.
func buildTopDomains(totals map[string]uint64, typeByDomain map[string]string, totalQueries uint64) []model.TopDomain {
	out := make([]model.TopDomain, 0, len(totals))
	for domain, count := range totals {
		if count == 0 {
			continue
		}
		out = append(out, model.TopDomain{
			Domain:    domain,
			QueryType: typeByDomain[domain],
			Count:     count,
			Percent:   percentOf(count, totalQueries),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Domain < out[j].Domain
	})
	if len(out) > statsTopN {
		out = out[:statsTopN]
	}
	return out
}
