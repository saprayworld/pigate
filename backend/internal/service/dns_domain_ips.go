package service

import (
	"sort"
	"sync"
	"time"

	"pigate/internal/model"
)

// dnsDomainIPs is the forward index domain -> {ip: lastSeen} used to answer
// "what IPs has this domain resolved to" for the Statistics > DNS drill-down
// (docs/ref/todo/statistics-dns-page-revamp-plan.md §2.1, T-02). It is fed
// from the exact same model.DNSLogAnswer events dnsReverseCache already
// consumes (kernel.DNSServerManager.WatchDNSLog via RecordDNSEvent) — this is
// purely an additional, independent index alongside the reverse cache, not a
// replacement for it: the reverse cache answers "IP -> domain" (last-writer-
// wins), this index answers "domain -> IPs" (keeps every IP seen, capped).
//
// Like dnsReverseCache, this is RAM-only (tech_stack_design.md §8 — no
// SQLite for runtime state / SD-card wear) and forgotten on restart by
// design.
//
// SECURITY / TRUST: exactly like dnsReverseCache, the domain->IP mapping is
// derived from dnsmasq's answer log, which any LAN client can influence by
// simply querying attacker-controlled domains. NEVER use this index for
// firewall rule generation, policy matching, or routing/QoS decisions
// (plan §5 item 6/7) — display-only enrichment for the DNS statistics pages.
//
// RAM budget: worst case is maxDomains (default 1000) x maxIPsPerDomain
// (default 16) ~= 16,000 (domain,ip) entries. Each entry is a short string
// key in a nested map plus a time.Time value, so this is on the order of
// ~1 MB worst case — bounded and small relative to the rest of the process,
// same order of magnitude as dnsReverseCache's own cap.
type dnsDomainIPs struct {
	mu sync.RWMutex

	byDomain map[string]map[string]time.Time // domain -> ip -> lastSeen
	ipRefs   map[string]int                  // ip -> number of distinct domains currently referencing it (drives the "shared" flag)

	ttl             time.Duration // shared with dnsReverseCache's TTL via SetLimits, so both indices age out consistently
	maxDomains      int
	maxIPsPerDomain int

	// truncated latches true the first time a Put is rejected because a cap
	// was hit (domain cap or per-domain IP cap) — sticky until Clear/SetLimits,
	// mirroring the "flag once, don't flap" spirit of the other stats rings'
	// Truncated fields (dns_query_stats.go's b.pairCount >= maxPairs checks).
	truncated bool
}

// domainIPEntry is one row of IPsFor's result: a single (ip, lastSeen)
// pairing for a domain, annotated with whether that IP is currently shared
// with at least one other domain (plan §1.1 item 1 — CDN/cloud-LB IP reuse
// inflates volume attribution when double-counted across domains, so the UI
// must be able to warn about it).
type domainIPEntry struct {
	IP       string
	LastSeen time.Time
	Shared   bool
}

const (
	// defaultMaxTrackedDomains/defaultMaxIPsPerDomain are the fallback
	// defaults used when a caller (NewStatisticsService) passes a
	// non-positive value. The effective, normal-path values come from the
	// bootstrap config keys dns-stats-max-domains / dns-stats-max-ips-per-
	// domain (plan T-05) — config.Defaults() must stay in sync with these two
	// literals, same convention as defaultMaxTrackedDNSPairs above.
	defaultMaxTrackedDomains  = 1000
	defaultMaxIPsPerDomain    = 16
	dnsDomainIPsMaxDomainsMin = 100
	dnsDomainIPsMaxDomainsMax = 20000
	dnsDomainIPsPerDomainMin  = 2
	dnsDomainIPsPerDomainMax  = 64
)

// newDNSDomainIPs constructs the index with package defaults — callers apply
// the DB-configured values via SetLimits right after construction, same
// pattern as newDNSReverseCache.
func newDNSDomainIPs() *dnsDomainIPs {
	return &dnsDomainIPs{
		byDomain:        make(map[string]map[string]time.Time),
		ipRefs:          make(map[string]int),
		ttl:             time.Duration(model.DNSCacheTTLDefault) * time.Minute,
		maxDomains:      defaultMaxTrackedDomains,
		maxIPsPerDomain: defaultMaxIPsPerDomain,
	}
}

// SetLimits applies a new ttl/maxDomains/maxIPsPerDomain triple, live (no
// restart, mirroring dnsReverseCache.SetLimits). ttlMinutes is expected to be
// the same value passed to dnsReverseCache.SetLimits (plan §1.6/T-02 — the
// two indices share one TTL knob), so it is clamped with the exact same
// bounds. maxDomains/maxIPsPerDomain are clamped to this package's own
// bounds; an out-of-range or non-positive value falls back to the default
// rather than erroring (config.Resolve is the authoritative validator, this
// is defense-in-depth exactly like dnsReverseCache.SetLimits).
//
// Shrinking a cap does NOT proactively evict here (unlike dnsReverseCache,
// which evicts down to size immediately) — the caps here only gate new
// domains/IPs being *admitted*; existing entries still age out normally by
// TTL, lazily on the next Put/IPsFor/Snapshot that touches them. This keeps
// SetLimits O(1) instead of O(entries) and is safe: a shrunk cap simply means
// growth stops sooner, not that already-admitted data becomes wrong.
func (d *dnsDomainIPs) SetLimits(ttlMinutes, maxDomains, maxIPsPerDomain int) {
	if ttlMinutes < model.DNSCacheTTLMin || ttlMinutes > model.DNSCacheTTLMax {
		ttlMinutes = model.DNSCacheTTLDefault
	}
	if maxDomains < dnsDomainIPsMaxDomainsMin || maxDomains > dnsDomainIPsMaxDomainsMax {
		maxDomains = defaultMaxTrackedDomains
	}
	if maxIPsPerDomain < dnsDomainIPsPerDomainMin || maxIPsPerDomain > dnsDomainIPsPerDomainMax {
		maxIPsPerDomain = defaultMaxIPsPerDomain
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.ttl = time.Duration(ttlMinutes) * time.Minute
	d.maxDomains = maxDomains
	d.maxIPsPerDomain = maxIPsPerDomain
}

// caps returns the currently configured maxDomains/maxIPsPerDomain, so a
// caller that only wants to update ttl (SetReverseCacheLimits — plan §1.6:
// the ttl knob is shared with dnsReverseCache but the domain/IP caps are
// independent, config-key driven, T-05) can pass them straight back into
// SetLimits without racing a direct field read.
func (d *dnsDomainIPs) caps() (maxDomains, maxIPsPerDomain int) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.maxDomains, d.maxIPsPerDomain
}

// Put records/refreshes domain -> ip -> now. Must stay O(1) (bounded by the
// small per-domain cap, never by the full index size), do no I/O, and never
// log the domain name — it runs on the same hot read-loop of WatchDNSLog as
// dnsReverseCache.Put and recordDomainQuery (plan §2.1/Caution 3).
//
// Admission rules (plan §2.1):
//   - a brand-new domain is rejected once len(byDomain) >= maxDomains
//   - a brand-new IP within an existing domain is rejected once that
//     domain's IP set is at maxIPsPerDomain (after trying to reclaim space
//     from that domain's own already-expired IPs first)
//   - an IP already tracked for the domain always gets its lastSeen
//     refreshed, even while the domain is otherwise "full" — refreshing an
//     existing entry never grows anything, so there is no reason to reject it
func (d *dnsDomainIPs) Put(domain, ip string) {
	if domain == "" || ip == "" {
		return
	}
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	ips, exists := d.byDomain[domain]
	if !exists {
		if len(d.byDomain) >= d.maxDomains {
			d.truncated = true
			return
		}
		ips = make(map[string]time.Time)
		d.byDomain[domain] = ips
	}

	if _, already := ips[ip]; !already {
		if len(ips) >= d.maxIPsPerDomain {
			// Try to reclaim space from this domain's own expired entries
			// before giving up — mirrors dnsReverseCache.Put's
			// evictExpiredLocked-then-retry pattern.
			d.evictExpiredInDomainLocked(domain, ips, now)
		}
		if len(ips) >= d.maxIPsPerDomain {
			d.truncated = true
			return
		}
		d.ipRefs[ip]++
	}
	ips[ip] = now
}

// IPsFor returns the (still-live) IPs known for domain, sorted by IP
// ascending, each annotated with lastSeen and whether the IP is currently
// shared with at least one other domain. Expired entries are evicted lazily
// here before the slice is built (plan §2.1 — lazy eviction on read).
func (d *dnsDomainIPs) IPsFor(domain string) []domainIPEntry {
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	ips, ok := d.byDomain[domain]
	if !ok {
		return nil
	}
	d.evictExpiredInDomainLocked(domain, ips, now)
	ips, ok = d.byDomain[domain]
	if !ok {
		return nil
	}

	out := make([]domainIPEntry, 0, len(ips))
	for ip, lastSeen := range ips {
		out = append(out, domainIPEntry{
			IP:       ip,
			LastSeen: lastSeen,
			Shared:   d.ipRefs[ip] > 1,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].IP < out[j].IP })
	return out
}

// domainIPStat is one entry of StatsFor's result: how many (still-live) IPs a
// domain is known to have in the forward index, and whether any of them is
// currently shared with another domain.
type domainIPStat struct {
	Count  int
	Shared bool
}

// StatsFor returns, for each of the given domains, the count of still-live
// IPs known for it and whether any of those IPs is shared with another
// domain — the same (ipCount, sharedIps) values IPsFor's caller would derive
// on its own, but for many domains in a single write-lock hold (docs/ref/
// todo/statistics-dns-review-fixes-plan.md §2/T-03: GetDNSClientDomains needs
// this for every domain a client queried, and calling IPsFor per-domain would
// mean one lock acquisition per domain instead of one for the whole request).
//
// Domains not present in the index are simply absent from the returned map —
// callers read the zero value (domainIPStat{}) for those, same convention as
// a missing key in a Go map read.
//
// Deliberately NOT implemented via Snapshot(): Snapshot() collapses the index
// to a single ip -> domain map (last-writer-wins on IP collisions), which
// would under-count domains that share an IP and can never report Shared
// correctly. This method walks byDomain/ipRefs directly instead, exactly the
// same data IPsFor reads, just batched across domains under one lock.
func (d *dnsDomainIPs) StatsFor(domains []string) map[string]domainIPStat {
	now := time.Now()
	out := make(map[string]domainIPStat, len(domains))

	d.mu.Lock()
	defer d.mu.Unlock()

	for _, domain := range domains {
		ips, ok := d.byDomain[domain]
		if !ok {
			continue
		}
		d.evictExpiredInDomainLocked(domain, ips, now)
		ips, ok = d.byDomain[domain]
		if !ok {
			continue
		}
		stat := domainIPStat{Count: len(ips)}
		for ip := range ips {
			if d.ipRefs[ip] > 1 {
				stat.Shared = true
				break
			}
		}
		out[domain] = stat
	}
	return out
}

// Snapshot inverts the whole index into a single ip -> domain map, used by
// the client drill-down to map a conversation's destination IP back to the
// domain that resolved to it (plan §2.1/T-04 — "Snapshot() คืน map ip->domain
// สำหรับ invert"). Expired entries are swept first (a full pass, since every
// domain is visited anyway).
//
// On IP collision across domains (the same IP resolved for more than one
// domain — exactly the case ipRefs/Shared tracks), the domain whose lastSeen
// for that IP is most recent wins, for deterministic output (plan §2.1
// explicit requirement).
func (d *dnsDomainIPs) Snapshot() map[string]string {
	now := time.Now()

	d.mu.Lock()
	defer d.mu.Unlock()

	d.evictExpiredLocked(now)

	out := make(map[string]string, len(d.ipRefs))
	latest := make(map[string]time.Time, len(d.ipRefs))
	for domain, ips := range d.byDomain {
		for ip, lastSeen := range ips {
			if prev, ok := latest[ip]; !ok || lastSeen.After(prev) {
				latest[ip] = lastSeen
				out[ip] = domain
			}
		}
	}
	return out
}

// Truncated reports whether at least one Put has been rejected since
// construction/last Clear/SetLimits because a cap (maxDomains or
// maxIPsPerDomain) was hit — surfaced to the API response so the UI can warn
// the user the index is incomplete (plan T-04's IPsTruncated field).
func (d *dnsDomainIPs) Truncated() bool {
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.truncated
}

// Clear empties the index — called when the user turns query logging off
// (plan §1.6/§4 item 6: privacy, mirrors dnsReverseCache.Clear).
func (d *dnsDomainIPs) Clear() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byDomain = make(map[string]map[string]time.Time)
	d.ipRefs = make(map[string]int)
	d.truncated = false
}

// sweepExpired does one full eviction pass over the index. NOTE: nothing in
// the running service calls this today — there is deliberately no ticker or
// goroutine for it (plan §2.1 — "ห้ามสร้าง goroutine/ticker ใหม่"), and the
// same is true of dnsReverseCache.sweepExpired. Memory stays bounded without
// it: maxDomains/maxIPsPerDomain are hard ceilings, and lazy TTL eviction in
// Put/IPsFor/Snapshot keeps live data honest. This method exists so a future
// caller (or a test) can reclaim RAM held by domains that stopped being
// looked up entirely — wire it onto an existing loop if that ever matters,
// never onto a new one.
func (d *dnsDomainIPs) sweepExpired() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.evictExpiredLocked(time.Now())
}

// evictExpiredLocked removes every (domain, ip) entry past its TTL across
// the whole index, decrementing/deleting ipRefs correctly and dropping
// domains left with zero IPs. Caller must hold mu (write lock).
func (d *dnsDomainIPs) evictExpiredLocked(now time.Time) {
	for domain, ips := range d.byDomain {
		for ip, lastSeen := range ips {
			if now.Sub(lastSeen) > d.ttl {
				delete(ips, ip)
				d.decRefLocked(ip)
			}
		}
		if len(ips) == 0 {
			delete(d.byDomain, domain)
		}
	}
}

// evictExpiredInDomainLocked removes expired IPs for a single domain only
// (bounded by maxIPsPerDomain, so cheap even inline on Put/IPsFor), keeping
// ipRefs consistent the same way evictExpiredLocked does. Caller must hold mu
// (write lock) and ips must be d.byDomain[domain].
func (d *dnsDomainIPs) evictExpiredInDomainLocked(domain string, ips map[string]time.Time, now time.Time) {
	for ip, lastSeen := range ips {
		if now.Sub(lastSeen) > d.ttl {
			delete(ips, ip)
			d.decRefLocked(ip)
		}
	}
	if len(ips) == 0 {
		delete(d.byDomain, domain)
	}
}

// decRefLocked decrements ipRefs[ip], deleting the key once it reaches zero
// so ipRefs never accumulates stale zero-count entries (which would make
// Snapshot/IPsFor allocate larger maps than necessary but is otherwise
// harmless — still cleaned up for hygiene). Caller must hold mu.
func (d *dnsDomainIPs) decRefLocked(ip string) {
	if n, ok := d.ipRefs[ip]; ok {
		if n <= 1 {
			delete(d.ipRefs, ip)
		} else {
			d.ipRefs[ip] = n - 1
		}
	}
}
