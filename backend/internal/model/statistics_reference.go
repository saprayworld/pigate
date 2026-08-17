package model

// Reference popover ("hover ที่ IP/Domain แล้วเห็นสรุปข้อมูลอ้างอิงแบบ
// FortiGate" — docs/ref/todo/reference-popover-plan.md Step 1) DTOs for the
// two lightweight GET /api/statistics/reference/{ip,domain} endpoints. These
// are intentionally NOT the same shape as DNSIPDomains/DNSDomainDrilldown
// (statistics.go): a hover popover can fire dozens of times a minute, so
// this only carries the top few entries + counts, never series[] or a full
// per-client join (plan §2.1/Step 2 — "ไม่ต้องคำนวณ series[] / per-client
// join ใด ๆ").
//
// 🔒 Display-only, poisonable data: the domain<->IP mapping backing both
// DTOs below is derived from dnsmasq's answer log for LAN clients that can
// query any domain they choose — exactly the same caveat as
// DNSIPDomains/DNSDomainDrilldown (statistics.go). NEVER use any field on
// this page to drive firewall rule generation, policy matching, routing, or
// QoS decisions (plan §5 item 6).

// ReferenceScope is IPReferenceSummary.Scope's enum — decided EXCLUSIVELY by
// the backend via isGloballyRoutable() (service/ipinfo.go), never by
// isPrivateIP (too few ranges covered — see that function's doc comment) and
// never by client-supplied input (plan §2.3/Caution 1). The frontend must
// treat this as the sole source of truth for which of the two popover modes
// to render; it is a security boundary (a public IP can safely be looked up
// via ipinfo.io, a LAN one must never be).
type ReferenceScope string

const (
	ReferenceScopePublic ReferenceScope = "public"
	ReferenceScopeLAN    ReferenceScope = "lan"
)

// ReferenceDomainRef is one row of IPReferenceSummary.Domains. Its meaning
// changes with Scope (plan Step 1 — "domains เปลี่ยนความหมายตาม scope"):
//   - Scope == "public": Domain resolved to this IP at least once
//     (dns_domain_ips.go's DomainsForIP) — LastSeen is set, Count is 0.
//   - Scope == "lan": Domain is one this client (the IP) queried in the
//     fixed 1h reference window — Count is its query count, LastSeen is "".
type ReferenceDomainRef struct {
	Domain string `json:"domain"`
	// LastSeen is an RFC3339 UTC timestamp, only populated when Scope ==
	// "public" (see doc comment above) — empty string otherwise.
	LastSeen string `json:"lastSeen,omitempty"`
	// Count is this domain's query count in the fixed 1h reference window,
	// only populated when Scope == "lan" — zero otherwise.
	Count uint64 `json:"count,omitempty"`
}

// IPReferenceSummary is the GET /api/statistics/reference/ip response — a
// lightweight hover-popover summary of a single IP, reusing the same
// RAM-only indices DNSIPDomains/GetDNSClientDomains already read (plan §2.1:
// "reuse index/aggregation เดิมทั้งหมด ห้าม copy-paste logic").
type IPReferenceSummary struct {
	// IP is the canonical (net/netip-normalized) form of the address that was
	// looked up — never the raw client-supplied string (same convention as
	// DNSIPDomains.IP).
	IP string `json:"ip"`
	// Hostname is resolved from a DHCP lease/reservation, same pattern as
	// DNSClientStat.Hostname — display only, falls back to IP itself when
	// unknown.
	Hostname string `json:"hostname"`
	// Mac is the DHCP lease's MAC address for IP, when known — empty
	// otherwise. Display only, never used for any access-control decision.
	Mac string `json:"mac"`
	// Scope is "public" or "lan", decided by isGloballyRoutable() alone (see
	// ReferenceScope doc comment) — the ONLY field that determines which of
	// the two popover UI modes to render, and the ONLY gate the frontend may
	// use to decide whether it is safe to also call
	// GET /api/statistics/ipinfo for this IP.
	Scope ReferenceScope `json:"scope"`
	// Enabled mirrors DNSIPDomains.Enabled: false means DNS query logging is
	// switched off, in which case Domains is an empty (never nil) slice and
	// DomainCount is 0 — this is NOT an error, the UI must show a muted
	// notice with a link to turn logging on rather than treating it as a
	// failure (plan Step 9 — DnsLoggingDisabledNotice).
	Enabled bool `json:"enabled"`
	// Found is true when this IP has ANY reference data at all in this
	// window (a resolved hostname, at least one related domain, or observed
	// traffic) — a hint for the UI to render a minimal "no data yet" state
	// rather than an empty-looking card. It is never used to gate a 404;
	// this endpoint always returns 200 for a syntactically valid IP.
	Found bool `json:"found"`
	// Domains is capped at the caller's limit (clampQueryLimit(r, 3, 10) at
	// the HTTP layer) — never nil, an empty slice means no known domain
	// reference for this IP/scope. See ReferenceDomainRef's doc comment for
	// how its shape depends on Scope.
	Domains []ReferenceDomainRef `json:"domains"`
	// DomainCount is the TRUE total count of matching domains BEFORE the
	// limit above was applied (plan Step 2 — "domainCount นับก่อนตัด limit"),
	// so the UI can render "+N more" correctly even though Domains itself is
	// truncated.
	DomainCount int `json:"domainCount"`
	// Bytes/BytesUp/BytesDown are this IP's observed traffic volume in the
	// fixed 1h reference window (Window below) — Dests[ip] when Scope ==
	// "public", Hosts[ip] when Scope == "lan" (service/traffic_stats.go's
	// TrafficBreakdown), the same conntrack-derived join every other
	// statistics endpoint in this package uses. Zero when there was no
	// observed traffic for this IP in the window — not an error.
	Bytes     uint64 `json:"bytes"`
	BytesUp   uint64 `json:"bytesUp"`
	BytesDown uint64 `json:"bytesDown"`
	// Window is always "1h" (plan §2.4 — the reference endpoints do not
	// accept a window query parameter, this is echoed back purely so the UI
	// can render a "last 1 hour" label without hardcoding the string itself).
	Window      string `json:"window"`
	GeneratedAt string `json:"generatedAt"`
}

// DomainReferenceSummary is the GET /api/statistics/reference/domain
// response — a lightweight hover-popover summary of a single domain, reusing
// dns_domain_ips.go's IPsFor (the same forward index DNSDomainDrilldown
// reads) without any traffic series or per-client join (plan §2.1/Step 2).
type DomainReferenceSummary struct {
	// Domain is the normalized (model.NormalizeQueryDomain) form of the
	// domain that was looked up — never the raw client-supplied string.
	Domain string `json:"domain"`
	// Enabled mirrors DNSDomainDrilldown.Enabled: false means DNS query
	// logging is switched off — IPs is an empty (never nil) slice, IPCount/
	// QueryCount/Clients are 0. Not an error (see IPReferenceSummary.Enabled
	// doc comment, same UI treatment).
	Enabled bool `json:"enabled"`
	// Found is true when this domain has ANY reference data at all in this
	// window (at least one resolved IP, or at least one query) — same
	// non-error "no data yet" hint as IPReferenceSummary.Found.
	Found bool `json:"found"`
	// IPs is every IP dns_domain_ips.go's IPsFor(domain) still remembers this
	// domain resolving to, capped at the caller's limit (clampQueryLimit(r,
	// 3, 10)) — never nil.
	IPs []ReferenceIPRef `json:"ips"`
	// IPCount is the TRUE total count of resolved IPs BEFORE the limit above
	// was applied — same "+N more" support as IPReferenceSummary.DomainCount.
	IPCount int `json:"ipCount"`
	// QueryCount is this domain's total query count in the fixed 1h
	// reference window (same meaning as DNSDomainDrilldown.TotalQueries, just
	// window-fixed).
	QueryCount uint64 `json:"queryCount"`
	// Clients is the distinct count of client IPs that queried this domain in
	// the window — same meaning as len(DNSDomainDrilldown.Clients) would
	// imply, computed directly rather than by building the full client rows
	// (this endpoint never needs per-client rows/bytes, plan Step 2).
	Clients int `json:"clients"`
	// SharedIPs is true when at least one of the domain's resolved IPs
	// (across the FULL set, before the limit above) is also known to resolve
	// for another domain — same CDN/shared-hosting warning signal as
	// DNSDomainDrilldown.SharedIPs.
	SharedIPs bool `json:"sharedIPs"`
	// IPsTruncated mirrors DNSDomainDrilldown.IPsTruncated: true when this
	// domain's own resolved-IP set is at (or over) the configured
	// dns-stats-max-ips-per-domain cap, meaning IPs above may be missing
	// entries even before the response-level limit was applied.
	IPsTruncated bool `json:"ipsTruncated"`
	// Bytes/BytesUp/BytesDown are this domain's total observed traffic volume
	// (join of ALL of the domain's known resolved IPs, not just the limited
	// IPs slice above) in the fixed 1h reference window.
	Bytes     uint64 `json:"bytes"`
	BytesUp   uint64 `json:"bytesUp"`
	BytesDown uint64 `json:"bytesDown"`
	// Window is always "1h" — see IPReferenceSummary.Window's doc comment.
	Window      string `json:"window"`
	GeneratedAt string `json:"generatedAt"`
}

// ReferenceIPRef is one row of DomainReferenceSummary.IPs.
type ReferenceIPRef struct {
	IP string `json:"ip"`
	// LastSeen is an RFC3339 UTC timestamp of the most recent DNS answer that
	// resolved Domain to this IP (dns_domain_ips.go's domainIPEntry.LastSeen).
	LastSeen string `json:"lastSeen"`
	// Shared is true when this IP is currently also known to resolve for at
	// least one other domain (same meaning as DNSDomainIP.Shared).
	Shared bool `json:"shared"`
}
