package service

import (
	"sort"
	"time"

	"pigate/internal/model"
)

// Reference popover ("hover ที่ IP/Domain แล้วเห็นสรุปข้อมูลอ้างอิงแบบ
// FortiGate" — docs/ref/todo/reference-popover-plan.md Step 2) — the two
// lightweight aggregations backing GET /api/statistics/reference/ip and
// /reference/domain. Deliberately a SEPARATE, much lighter pipeline from
// GetDNSIPDomains/GetDNSDomainClients (statistics_dns.go): those compute a
// full byte-series join and per-client/per-domain rows meant for a
// drill-down PAGE, while a hover popover can fire dozens of times a minute
// and only ever needs the top few entries + counts (plan §2.1). This file
// reuses the exact same RAM-only indices those methods do
// (dns_domain_ips.go's DomainsForIP/IPsFor, s.dnsWindowBuckets,
// isGloballyRoutable) — it never re-implements their aggregation logic.

// referenceWindow is fixed — the reference endpoints never accept a window
// query parameter from the client (plan §2.4/Q4): the API handler doesn't
// even look at a `window` param, so there is nothing for a client to smuggle
// in here. Echoed back in every response purely for the UI to render a label
// without hardcoding the string itself.
const referenceWindow = "1h"

// referenceDefaultLimit/referenceMaxLimit mirror the HTTP layer's
// clampQueryLimit(r, 3, 10) call (handlers.go) — duplicated here only as
// safety defaults for direct callers (tests), same convention as
// trafficTopHostsDefaultLimit etc. The authoritative clamp for HTTP requests
// happens in the handler, before ip/domain even reach this file.
const (
	referenceDefaultLimit = 3
	referenceMaxLimit     = 10
)

// clampReferenceLimit defends this file's two exported methods against a
// direct (non-HTTP) caller passing an out-of-range limit — the HTTP handler
// already clamps via clampQueryLimit, so in the normal request path this is
// a no-op.
func clampReferenceLimit(limit int) int {
	if limit <= 0 {
		return referenceDefaultLimit
	}
	if limit > referenceMaxLimit {
		return referenceMaxLimit
	}
	return limit
}

// GetIPReference composes the /api/statistics/reference/ip response. ip must
// already be validated/normalized (netip.ParseAddr + addr.String()) by the
// caller (the API handler), exactly like GetDNSIPDomains. Scope is decided
// EXCLUSIVELY by isGloballyRoutable(ip) — never isPrivateIP, never anything
// client-supplied (plan §2.3/Caution 1: this is a security boundary, not a
// UX guard).
func (s *StatisticsService) GetIPReference(ip string, limit int) model.IPReferenceSummary {
	limit = clampReferenceLimit(limit)

	scope := model.ReferenceScopeLAN
	if isGloballyRoutable(ip) {
		scope = model.ReferenceScopePublic
	}

	leaseByIP, resByIP := s.traffic.hostLookup()
	hostname, mac := hostnameFor(ip, leaseByIP, resByIP)
	if hostname == ip {
		// hostnameFor falls back to returning ip itself when there's no
		// lease/reservation match — don't echo that back as a "hostname",
		// same convention DNSClientStat/DNSIPDomains readers expect (a blank
		// Hostname means "unknown", not "same as the IP").
		hostname = ""
	}

	s.dns.mu.RLock()
	enabled := s.dns.enabled
	s.dns.mu.RUnlock()

	summary := model.IPReferenceSummary{
		IP:          ip,
		Hostname:    hostname,
		Mac:         mac,
		Scope:       scope,
		Enabled:     enabled,
		Domains:     []model.ReferenceDomainRef{},
		Window:      referenceWindow,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if !enabled {
		// Disabled: never touch domainIPs or the ring below (same privacy
		// rule as GetDNSIPDomains/GetDNSQueryStatistics) — Domains stays the
		// empty slice set above, DomainCount stays 0. Bytes below are still
		// filled in: they come from conntrack, not DNS query logging, so
		// they are unaffected by this toggle.
		summary.Found = hostname != "" || mac != ""
		breakdown := s.traffic.GetTrafficBreakdown(referenceWindow)
		setReferenceIPBytes(&summary, scope, ip, breakdown)
		return summary
	}

	if scope == model.ReferenceScopePublic {
		// public: Domains = every domain known to have resolved to ip
		// (dns_domain_ips.go's DomainsForIP — the exact same reverse index
		// GetDNSIPDomains reads), newest-first.
		matches := s.dns.domainIPs.DomainsForIP(ip)
		summary.DomainCount = len(matches)
		n := limit
		if n > len(matches) {
			n = len(matches)
		}
		domains := make([]model.ReferenceDomainRef, n)
		for i := 0; i < n; i++ {
			domains[i] = model.ReferenceDomainRef{
				Domain:   matches[i].Domain,
				LastSeen: matches[i].LastSeen.UTC().Format(time.RFC3339),
			}
		}
		summary.Domains = domains
	} else {
		// lan: Domains = top domains THIS client (ip) queried in the fixed
		// window (plan §2.3) — walks the same query-count ring
		// GetDNSClientDomains does, but skips its traffic-breakdown/
		// per-client join entirely (never needed for a hover summary).
		domainTotals := make(map[string]uint64)
		typeByDomain := make(map[string]string)
		var totalForClient uint64
		s.dns.mu.RLock()
		for _, b := range s.dnsWindowBuckets(referenceWindow) {
			for domain, clients := range b.pairs {
				if count, ok := clients[ip]; ok {
					domainTotals[domain] += count
					totalForClient += count
					if qtype := b.typeByDomain[domain]; qtype != "" {
						typeByDomain[domain] = qtype
					}
				}
			}
		}
		s.dns.mu.RUnlock()

		summary.DomainCount = len(domainTotals)
		ranked := rankTopDomains(domainTotals, typeByDomain, totalForClient, limit)
		domains := make([]model.ReferenceDomainRef, len(ranked))
		for i, td := range ranked {
			domains[i] = model.ReferenceDomainRef{Domain: td.Domain, Count: td.Count}
		}
		summary.Domains = domains
	}

	breakdown := s.traffic.GetTrafficBreakdown(referenceWindow)
	setReferenceIPBytes(&summary, scope, ip, breakdown)
	summary.Found = summary.Hostname != "" || summary.DomainCount > 0 || summary.Bytes > 0
	return summary
}

// setReferenceIPBytes fills Bytes/BytesUp/BytesDown from an already-fetched
// TrafficBreakdown, reading Dests[ip] for a public IP (it is the remote
// destination side of a conversation) and Hosts[ip] for a LAN one (it is the
// local source side) — the same Hosts-vs-Dests split
// GetDNSQueryStatistics/GetDNSIPDomains already rely on for their
// client-row/domain-row byte joins.
func setReferenceIPBytes(summary *model.IPReferenceSummary, scope model.ReferenceScope, ip string, breakdown TrafficBreakdown) {
	var v dirBytes
	if scope == model.ReferenceScopePublic {
		v = breakdown.Dests[ip]
	} else {
		v = breakdown.Hosts[ip]
	}
	summary.Bytes = v.Total()
	summary.BytesUp = v.Orig
	summary.BytesDown = v.Reply
}

// GetDomainReference composes the /api/statistics/reference/domain response.
// domain must already be validated/normalized (model.NormalizeQueryDomain)
// by the caller (the API handler), exactly like GetDNSDomainClients.
func (s *StatisticsService) GetDomainReference(domain string, limit int) model.DomainReferenceSummary {
	limit = clampReferenceLimit(limit)

	s.dns.mu.RLock()
	enabled := s.dns.enabled
	s.dns.mu.RUnlock()

	summary := model.DomainReferenceSummary{
		Domain:      domain,
		Enabled:     enabled,
		IPs:         []model.ReferenceIPRef{},
		Window:      referenceWindow,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}

	if !enabled {
		// Disabled: never touch domainIPs or the ring below (same privacy
		// rule as GetDNSDomainClients) — IPs stays the empty slice above.
		return summary
	}

	// IPsFor is the exact same forward index GetDNSDomainClients reads —
	// reused as-is, no copy of its logic.
	ips := s.dns.domainIPs.IPsFor(domain)
	summary.IPCount = len(ips)
	for _, e := range ips {
		if e.Shared {
			summary.SharedIPs = true
			break
		}
	}
	_, maxIPs := s.dns.domainIPs.caps()
	summary.IPsTruncated = maxIPs > 0 && len(ips) >= maxIPs

	n := limit
	if n > len(ips) {
		n = len(ips)
	}
	// IPsFor already returns IP-ascending; re-sort by LastSeen desc (most
	// recently seen first) before truncating, since that is the more useful
	// "top 3" for a hover popover than alphabetical IP order.
	sortedIPs := make([]domainIPEntry, len(ips))
	copy(sortedIPs, ips)
	sort.Slice(sortedIPs, func(i, j int) bool {
		if !sortedIPs[i].LastSeen.Equal(sortedIPs[j].LastSeen) {
			return sortedIPs[i].LastSeen.After(sortedIPs[j].LastSeen)
		}
		return sortedIPs[i].IP < sortedIPs[j].IP
	})
	refs := make([]model.ReferenceIPRef, n)
	ipStrs := make([]string, len(sortedIPs))
	for i, e := range sortedIPs {
		ipStrs[i] = e.IP
		if i < n {
			refs[i] = model.ReferenceIPRef{
				IP:       e.IP,
				LastSeen: e.LastSeen.UTC().Format(time.RFC3339),
				Shared:   e.Shared,
			}
		}
	}
	summary.IPs = refs

	// queryCount/clients: walk the same query-count ring
	// GetDNSDomainClients does, but skip its traffic-breakdown/per-client
	// enrichment entirely (never needed for a hover summary — plan Step 2).
	clientSet := make(map[string]struct{})
	var queryCount uint64
	s.dns.mu.RLock()
	for _, b := range s.dnsWindowBuckets(referenceWindow) {
		if clients, ok := b.pairs[domain]; ok {
			for client, count := range clients {
				queryCount += count
				clientSet[client] = struct{}{}
			}
		}
	}
	s.dns.mu.RUnlock()
	summary.QueryCount = queryCount
	summary.Clients = len(clientSet)

	// Bytes: join over the FULL resolved-IP set (not just the limited IPs
	// slice above) — same "denominator is the whole domain, not the visible
	// rows" rule GetDNSDomainClients' TotalBytes follows.
	breakdown := s.traffic.GetTrafficBreakdownForDests(referenceWindow, ipStrs)
	var total dirBytes
	for _, ip := range ipStrs {
		v := breakdown.Dests[ip]
		total.Orig += v.Orig
		total.Reply += v.Reply
	}
	summary.Bytes = total.Total()
	summary.BytesUp = total.Orig
	summary.BytesDown = total.Reply

	summary.Found = summary.IPCount > 0 || summary.QueryCount > 0
	return summary
}
