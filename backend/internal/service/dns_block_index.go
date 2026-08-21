package service

import (
	"strings"
	"sync"

	"pigate/internal/model"
)

// dnsBlockIndex is the RAM-only, in-memory matcher behind the "Blocked
// Domain Query" statistics feature (docs/ref/todo/
// dns-blocked-query-statistics-plan.md T-01). It answers, for a queried
// domain, whether that domain (or any parent of it) is currently covered by
// an ENABLED deny-list entry that has actually been applied to dnsmasq —
// mirroring dnsmasq's own `address=/domain/...`/`server=/domain/` semantics,
// which cover every subdomain of the configured name, not just an exact
// match (see docs/ref/dnsmasq-design.md and
// docs/ref/todo/dns-blocked-domains-plan.md §2).
//
// This index is deliberately fed from DNSServerService.ApplyAll — the same
// list that was just handed to kernel.DNSServerManager.ApplyZones and
// (assuming ApplyZones returned no error) is therefore the list dnsmasq is
// actually enforcing right now — rather than reading straight from the DB.
// A DB row that hasn't been applied yet (or an apply that failed) must never
// be reflected here, or the statistics page would claim a query was
// "blocked" when dnsmasq in fact still resolved it normally. See
// DNSServerService.SetBlockedDomainsSink for the wiring.
//
// Classification is record-time only (dns_query_stats.go's
// recordDomainQuery calls Match once, at the moment a query event arrives,
// and stores the result alongside the ring's existing per-bucket data) — a
// later Set() call (deny-list changed) never re-classifies historical
// buckets, by design (plan §0: "record-time classify ... ไม่ re-classify
// ย้อนหลัง").
type dnsBlockIndex struct {
	mu sync.RWMutex
	// rules maps a lower-cased, dot-free-trailing domain to its block mode
	// (model.DNSBlockModeNXDomain / model.DNSBlockModeSinkhole). nil until
	// the first successful Set() call — Match/Empty treat a nil map the same
	// as an empty one.
	rules map[string]string
}

// Set atomically replaces the whole rule set. Entries that are disabled
// (Enabled == false) or fail model.ValidateBlockedDomain are skipped; an
// empty Mode defaults to model.DNSBlockModeNXDomain, matching
// model.ValidateBlockedDomain's own default-mode behavior. Domain keys are
// stored lower-cased (dnsmasq/DNS names are case-insensitive) — callers must
// pass Match a domain that has already been through the same normalization
// the query ring itself uses (model.NormalizeQueryDomain / the DNS log
// parser's sanitizeDomain), which already lower-cases.
func (idx *dnsBlockIndex) Set(rules []model.BlockedDomain) {
	next := make(map[string]string, len(rules))
	for _, r := range rules {
		if !r.Enabled {
			continue
		}
		if err := model.ValidateBlockedDomain(r); err != nil {
			continue
		}
		mode := r.Mode
		if mode == "" {
			mode = model.DNSBlockModeNXDomain
		}
		next[strings.ToLower(r.Domain)] = mode
	}

	idx.mu.Lock()
	idx.rules = next
	idx.mu.Unlock()
}

// Empty reports whether the index currently has no active rules — a fast
// path callers can check before doing any per-query work (dns_query_stats.go
// calls this before Match on every single query event).
func (idx *dnsBlockIndex) Empty() bool {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	return len(idx.rules) == 0
}

// Match reports whether domain (or any of its parent domains, up to 16
// label levels) is covered by an active deny-list rule, mimicking dnsmasq's
// address=/domain/ behavior of blocking every subdomain of the configured
// name. It checks the exact domain first, then repeatedly strips the
// left-most label (via strings.IndexByte, never strings.Split — this must
// stay allocation-free and O(number of labels), since it runs on every
// query event) and checks each shorter suffix in turn. "notexample.com" is
// NOT considered a subdomain of "example.com" (label-boundary matching
// only, never a raw string suffix check).
//
// This never performs I/O and never logs the domain it was asked about
// (privacy — mirrors recordDomainQuery's own contract in
// dns_query_stats.go).
func (idx *dnsBlockIndex) Match(domain string) (rule, mode string, ok bool) {
	idx.mu.RLock()
	defer idx.mu.RUnlock()
	if len(idx.rules) == 0 {
		return "", "", false
	}

	d := strings.ToLower(domain)
	if m, found := idx.rules[d]; found {
		return d, m, true
	}

	rest := d
	for i := 0; i < 16; i++ {
		dot := strings.IndexByte(rest, '.')
		if dot < 0 {
			break
		}
		rest = rest[dot+1:]
		if rest == "" {
			break
		}
		if m, found := idx.rules[rest]; found {
			return rest, m, true
		}
	}
	return "", "", false
}
