package service

import (
	"sort"
	"sync"
	"time"

	"pigate/internal/model"
)

// "Top Queried Domains" pipeline (docs/ref/todo/statistics-dns-top-domain-plan.md
// T-07), extended by docs/ref/todo/dns-query-statistics-drilldown-plan.md T-02
// to add the client dimension — a RAM-only 5-minute bucket ring, structurally
// identical to the deny ring in statistics.go (same span/max, same "mutex +
// map increment only, no I/O" contract on the hot path) but keyed by
// (domain, clientIP) pairs instead of a single dimension, fed by
// kernel.DNSServerManager.WatchDNSLog via RecordDNSEvent instead of the NFLOG
// watcher.
const (
	// domainBucketSpan/domainBucketMax mirror denyBucketSpan/denyBucketMax so
	// the domain ring covers the same 24h window at the same granularity.
	domainBucketSpan = 5 * time.Minute
	domainBucketMax  = 288

	// defaultMaxTrackedDNSPairs/defaultMaxTrackedDNSClients are the fallback
	// defaults used when a caller (NewStatisticsService) passes a non-positive
	// value for the corresponding limit. The effective, normal-path values
	// come from the bootstrap config keys dns-stats-max-pairs /
	// dns-stats-max-clients (see docs/ref/todo/
	// dns-stats-tracking-limits-config-plan.md) — config.Defaults() must stay
	// in sync with these two literals.
	//
	// The caps bound a single bucket's (domain, client) pair count and
	// distinct-client count the same way maxTrackedDenySources bounds the
	// deny ring — a flood of unique domains/clients (or an attacker
	// deliberately querying garbage names) cannot grow a bucket's maps
	// without limit. Both caps must be enforced independently (drilldown plan
	// §2.1/Caution 3 — missing either one makes the other pointless).
	//
	// RAM note: the cap is per 5-minute bucket and the ring holds 288 buckets
	// (24h), so worst-case tracked pairs is roughly maxPairs x 288.
	defaultMaxTrackedDNSPairs   = 2400
	defaultMaxTrackedDNSClients = 200
	// dnsUnknownClient is the reserved key used when the query-log event
	// carries no parseable client IP (drilldown plan §2.1/Caution 6). It can
	// never collide with a real client because the ring is keyed by IP, not
	// hostname, and "unknown" cannot parse as an IP address.
	dnsUnknownClient = "unknown"
	// dnsStatsTopN is the row cap for the DNS Query Statistics tab's tables
	// (drilldown plan T-02) — a dedicated full-page tab, so intentionally
	// larger than statsTopN (the 10-row card cap used elsewhere).
	dnsStatsTopN = 50
)

// domainBucket is one 5-minute bucket of query counts, keyed by
// (domain, clientIP) pair. RAM-only, never persisted (plan §0/Caution 12 —
// dns_server_settings gains only config columns, no query/domain table).
//
// pairs is the single source of truth: pairs[domain][clientIP] = count.
// Per-domain totals (domainSnapshot, GetDNSQueryStatistics.TopDomains) and
// per-client totals (GetDNSQueryStatistics.TopClients) are both derived from
// it, so a drilldown's sum always matches the top-level table's count for the
// same key (drilldown plan §2.1 — the reason a second, separate ring was
// rejected).
type domainBucket struct {
	ts    string
	pairs map[string]map[string]uint64 // domain -> clientIP -> count

	// pairCount is the number of (domain, client) pairs currently tracked in
	// this bucket — checked before adding a *new* pair (an existing pair can
	// always be incremented). Enforced at both the domain and the inner
	// client-map level; missing either would make the cap meaningless
	// (drilldown plan Caution 3).
	pairCount int
	// clientCount tracks how many times each distinct client has been seen
	// in this bucket (summed across all domains) — used to enforce the
	// configurable per-bucket client cap independently of the pair cap, and
	// doubles as the per-bucket source for the "Top Clients" table.
	clientCount map[string]uint64

	typeByDomain map[string]string
	// queries is the bucket's total query count. It is incremented for every
	// event unconditionally, never gated by the pair/client caps above — the
	// window total must stay accurate even once a bucket is truncated
	// (drilldown plan §2.1/T-02 item 4).
	queries uint64
}

// dnsQueryStats holds the domain ring + the opt-in switch state. Kept as its
// own struct (embedded into StatisticsService below) purely for file
// organization — the mutex still guards only these fields, same discipline
// as denyBuckets/mu above it.
type dnsQueryStats struct {
	mu           sync.RWMutex
	enabled      bool
	buckets      []domainBucket
	reverseCache *dnsReverseCache
	domainIPs    *dnsDomainIPs

	// maxPairs/maxClients are the effective per-bucket caps, set once at
	// construction (NewStatisticsService) and never mutated afterwards —
	// this is why the existing reads under mu.RLock() need no extra
	// synchronization. A setter is deliberately not provided: changing the
	// cap mid-flight would make the Truncated semantics meaningless for
	// buckets that were filled under the old cap.
	maxPairs   int
	maxClients int
}

// RecordDNSEvent is the single entry point from the DNS query-log watcher
// (main.go: `go dnsServerManager.WatchDNSLog(ctx, statisticsService.RecordDNSEvent)`).
// It must be O(1), never block, never do I/O and never log the domain itself
// (plan §5 item 2) — it runs directly on the kernel-layer log read loop,
// mirroring RecordFirewallLog's contract above.
func (s *StatisticsService) RecordDNSEvent(ev model.DNSLogEvent) {
	switch ev.Kind {
	case model.DNSLogQuery:
		s.recordDomainQuery(ev.Domain, ev.QueryType, ev.ClientIP)
	case model.DNSLogAnswer:
		if ev.Domain != "" && ev.AnswerIP != "" {
			s.dns.reverseCache.Put(ev.AnswerIP, ev.Domain)
			s.dns.domainIPs.Put(ev.Domain, ev.AnswerIP)
		}
	}
}

// recordDomainQuery is the hot-path counter behind RecordDNSEvent. It must
// stay O(1), allocation-light (an inner map is only created the first time a
// domain is seen in a bucket) and must never call hostLookup/hostnameFor or
// otherwise resolve a hostname — that enrichment happens only when a
// response is served, never on the watcher's read loop (drilldown plan §5
// item 2/Caution 2).
func (s *StatisticsService) recordDomainQuery(domain, qtype, client string) {
	if domain == "" {
		return
	}
	if client == "" {
		client = dnsUnknownClient
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
			pairs:        make(map[string]map[string]uint64),
			clientCount:  make(map[string]uint64),
			typeByDomain: make(map[string]string),
		})
		if len(s.dns.buckets) > domainBucketMax {
			s.dns.buckets = s.dns.buckets[len(s.dns.buckets)-domainBucketMax:]
		}
		b = &s.dns.buckets[len(s.dns.buckets)-1]
	}

	// queries is never gated by the caps below — the window total must
	// remain accurate even once a bucket has hit its tracked-pair/client cap.
	b.queries++

	clients := b.pairs[domain]
	_, pairExists := clients[client]
	if !pairExists {
		// A brand-new (domain, client) pair — only admitted while both the
		// pair cap and the client cap still have room. An existing pair is
		// always allowed to keep incrementing.
		if b.pairCount >= s.dns.maxPairs {
			return
		}
		if _, clientSeen := b.clientCount[client]; !clientSeen && len(b.clientCount) >= s.dns.maxClients {
			return
		}
		if clients == nil {
			clients = make(map[string]uint64)
			b.pairs[domain] = clients
		}
		b.pairCount++
	}
	clients[client]++
	b.clientCount[client]++
	if qtype != "" {
		b.typeByDomain[domain] = qtype
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

// ClearDNSStats wipes the domain ring, reverse cache and domain->IP index.
// Called when the query-logging switch is turned off, and available for
// tests/handlers that need a clean slate.
func (s *StatisticsService) ClearDNSStats() {
	s.dns.mu.Lock()
	s.dns.buckets = nil
	s.dns.mu.Unlock()
	s.dns.reverseCache.Clear()
	s.dns.domainIPs.Clear()
}

// SetReverseCacheLimits forwards to the reverse cache's SetLimits — see
// dns_reverse_cache.go for the clamp/evict-immediately contract (plan T-08).
// It also forwards the same ttlMinutes to the domain->IP index's SetLimits
// (plan §1.6/T-02 — the two indices share one TTL knob), leaving that
// index's own maxDomains/maxIPsPerDomain caps untouched (those are set
// separately via SetDomainIPsLimits, config keys dns-stats-max-domains /
// dns-stats-max-ips-per-domain, plan T-05).
func (s *StatisticsService) SetReverseCacheLimits(ttlMinutes, maxEntries int) {
	s.dns.reverseCache.SetLimits(ttlMinutes, maxEntries)
	maxDomains, maxIPsPerDomain := s.dns.domainIPs.caps()
	s.dns.domainIPs.SetLimits(ttlMinutes, maxDomains, maxIPsPerDomain)
}

// SetDomainIPsLimits forwards to the domain->IP index's SetLimits. Called
// separately from SetReverseCacheLimits because maxDomains/maxIPsPerDomain
// come from their own bootstrap config keys (dns-stats-max-domains /
// dns-stats-max-ips-per-domain, plan T-05) rather than the reverse cache's
// ttl/maxEntries pair; ttlMinutes should be passed the same value given to
// SetReverseCacheLimits so both indices' TTLs stay in sync (plan §1.6).
func (s *StatisticsService) SetDomainIPsLimits(ttlMinutes, maxDomains, maxIPsPerDomain int) {
	s.dns.domainIPs.SetLimits(ttlMinutes, maxDomains, maxIPsPerDomain)
}

// dnsWindowBuckets selects the trailing statsWindowBucketCount(window)
// buckets, mirroring denySnapshot/domainSnapshot's bucket-selection logic.
// Callers must hold at least s.dns.mu.RLock(). window is normalized here too
// (defense-in-depth, same as GetTrafficDetail/getTrafficBreakdown), so a
// caller that forgot to normalize still gets a safe bucket count.
func (s *StatisticsService) dnsWindowBuckets(window string) []domainBucket {
	return lastNBuckets(s.dns.buckets, statsWindowBucketCount(window))
}

// domainSnapshot aggregates the domain ring's per-domain query counts over
// the requested window, mirroring denySnapshot's bucket-selection logic.
// totals is derived from each bucket's pairs map (summed across all
// clients) — the values/behavior observable from here are unchanged from
// before the (domain, client) pair ring existed (drilldown plan T-02 item 5:
// this must be a pure refactor, the "Top Queried Domains" card and its tests
// must not move).
func (s *StatisticsService) domainSnapshot(window string) (totals map[string]uint64, typeByDomain map[string]string, totalQueries uint64, enabled bool, truncated bool) {
	totals = make(map[string]uint64)
	typeByDomain = make(map[string]string)

	s.dns.mu.RLock()
	defer s.dns.mu.RUnlock()

	enabled = s.dns.enabled

	for _, b := range s.dnsWindowBuckets(window) {
		for domain, clients := range b.pairs {
			for _, count := range clients {
				totals[domain] += count
			}
		}
		for k, v := range b.typeByDomain {
			typeByDomain[k] = v
		}
		totalQueries += b.queries
		if b.pairCount >= s.dns.maxPairs || len(b.clientCount) >= s.dns.maxClients {
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

// GetDNSQueryStatistics, GetDNSDomainClients and GetDNSClientDomains (the 3
// DNS statistics endpoints' composers) moved to statistics_dns.go once the
// domain->IP forward index (dns_domain_ips.go, T-02) and the traffic-
// breakdown byte join (docs/ref/todo/statistics-dns-page-revamp-plan.md
// §1.1/§1.2, T-04) were added — this file stays focused on the ring + hot
// path + the generic ranking helpers below, which statistics_dns.go reuses
// as its base ranking/sort before decorating rows with the byte-volume
// fields.

// rankTopDomains is buildTopDomains with a caller-supplied row cap, used by
// the DNS Query Statistics tab (dnsStatsTopN) instead of the card-sized
// statsTopN. buildTopDomains itself is left untouched (fixed at statsTopN)
// so the pre-existing "Top Queried Domains" card's behavior stays byte-for-
// byte identical (drilldown plan T-02 item 5 regression guard).
func rankTopDomains(totals map[string]uint64, typeByDomain map[string]string, totalQueries uint64, topN int) []model.TopDomain {
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
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}

// rankDNSClients ranks a client -> count map into DNSClientStat rows,
// enriching each with a hostname the same way buildTopDeniedSources does.
// Sort is deterministic (count desc, then IP asc).
func rankDNSClients(totals map[string]uint64, totalQueries uint64, leaseByIP map[string]model.ActiveDhcpLease, resByIP map[string]model.DhcpReservation, topN int) []model.DNSClientStat {
	out := make([]model.DNSClientStat, 0, len(totals))
	for client, count := range totals {
		if count == 0 {
			continue
		}
		hostname, _ := hostnameFor(client, leaseByIP, resByIP)
		out = append(out, model.DNSClientStat{
			IP:       client,
			Hostname: hostname,
			Count:    count,
			Percent:  percentOf(count, totalQueries),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > topN {
		out = out[:topN]
	}
	return out
}
