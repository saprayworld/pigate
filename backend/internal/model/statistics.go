package model

// Statistics page DTOs (docs/ref/todo/statistics-page-plan.md) — the
// /api/statistics/traffic response backing the "Statistics" page (Top
// Source Hosts / Top Destinations / Top Conversations / Top Denied). Kept in
// its own file rather than types.go (already very long) per plan T-01.
//
// All aggregation behind these DTOs is RAM-only (bucket ring in
// service/traffic_stats.go + deny ring in service/statistics.go) — nothing
// here is ever persisted to SQLite (plan Caution 9).

// TopHost is one row ranked by observed bytes — used for both Top Source
// Hosts and Top Destinations (same shape, different aggregation key).
type TopHost struct {
	IP       string  `json:"ip"`
	Hostname string  `json:"hostname"`
	MAC      string  `json:"mac"`
	Bytes    uint64  `json:"bytes"`
	Percent  float64 `json:"percent"`
	// Private is true when IP is RFC1918/link-local/ULA (a LAN address). A
	// flow that originated from the internet can appear as "source" too
	// (conntrack Forward tuple, plan Caution 7) — the UI uses this flag to
	// tell those rows apart from genuine LAN hosts.
	Private bool `json:"private"`
	// Domain is the domain name dnsmasq most recently answered for this IP
	// (docs/ref/todo/statistics-dns-top-domain-plan.md T-01/T-08) — display
	// only, empty when unknown/expired. NEVER used for firewall rule
	// generation, policy matching, or routing decisions (plan §5 item 6).
	Domain string `json:"domain"`
}

// TopConversation is one src -> dst:port flow-tuple row (4-tuple: src IP,
// dst IP, proto, dst port — no src port, plan §0: SrcPort does not exist in
// model.FlowSample).
type TopConversation struct {
	SrcIP       string `json:"srcIp"`
	SrcHostname string `json:"srcHostname"`
	DstIP       string `json:"dstIp"`
	DstHostname string `json:"dstHostname"`
	// Proto is a display string: "TCP"/"UDP"/"ICMP"/"IP:<n>" for anything
	// else.
	Proto   string  `json:"proto"`
	DstPort uint16  `json:"dstPort"`
	Bytes   uint64  `json:"bytes"`
	Percent float64 `json:"percent"`
	// DstDomain is the domain name dnsmasq most recently answered for DstIP —
	// display only, empty when unknown/expired (same source/rules as
	// TopHost.Domain above). SrcIP has no equivalent field: it is always a LAN
	// address in this table.
	DstDomain string `json:"dstDomain"`
}

// TopDomain is one row of the "Top Queried Domains" card — ranked by query
// count, fed by the DNS query-log watcher (docs/ref/todo/
// statistics-dns-top-domain-plan.md T-04/T-07). RAM-only, opt-in (query
// logging must be enabled from the DNS Server page).
type TopDomain struct {
	Domain    string  `json:"domain"`
	QueryType string  `json:"queryType"`
	Count     uint64  `json:"count"`
	Percent   float64 `json:"percent"`
}

// TopDeniedSource is one row of the Top Denied Sources card — ranked by
// event *count*, not bytes (plan §1 item 4: the underlying nftables log rule
// is rate-limited, so this is a sampled approximation, never a byte total).
type TopDeniedSource struct {
	IP       string  `json:"ip"`
	Hostname string  `json:"hostname"`
	Count    uint64  `json:"count"`
	Percent  float64 `json:"percent"`
}

// TopDeniedPort is one row of the Top Denied Ports card, keyed by
// proto+port (e.g. "TCP"/"443").
type TopDeniedPort struct {
	Proto   string  `json:"proto"`
	Port    string  `json:"port"`
	Count   uint64  `json:"count"`
	Percent float64 `json:"percent"`
}

// TrafficStatistics is the /api/statistics/traffic response.
type TrafficStatistics struct {
	Window        string `json:"window"` // "1h" | "24h"
	ObservedBytes uint64 `json:"observedBytes"`
	// Accuracy mirrors TrafficDetail.Accuracy ("estimated" | "near-exact") —
	// same conntrack-poll-vs-DESTROY-event signal, computed the same way.
	Accuracy string `json:"accuracy"`

	TopSources       []TopHost         `json:"topSources"`
	TopDestinations  []TopHost         `json:"topDestinations"`
	TopConversations []TopConversation `json:"topConversations"`

	DeniedSources []TopDeniedSource `json:"deniedSources"`
	DeniedPorts   []TopDeniedPort   `json:"deniedPorts"`
	// DeniedSampled is always true: the nftables log rules that feed this
	// data carry an expr.Limit (rate limit), so DeniedSources/DeniedPorts are
	// a sampled approximation, never an exact packet count (plan Caution 3).
	DeniedSampled bool `json:"deniedSampled"`
	// DeniedEvents is the number of NFLOG DROP events actually counted in
	// this window (not an estimate of real packets dropped).
	DeniedEvents uint64 `json:"deniedEvents"`

	// Truncated is true when any per-bucket map (hosts/dests/conversations)
	// hit its tracking cap during this window (plan §2 T-02/T-03) — the UI
	// should show a warning that the ranking may be incomplete.
	Truncated bool `json:"truncated"`

	// TopDomains, DNSQueries, DNSLoggingEnabled, DNSTruncated are the "Top
	// Queried Domains" card (docs/ref/todo/statistics-dns-top-domain-plan.md
	// T-01/T-07). All zero-valued (empty slice / false) when query logging is
	// disabled — the field always exists so a client doesn't have to special-
	// case its absence, but is never populated without the opt-in switch.
	TopDomains []TopDomain `json:"topDomains"`
	// DNSQueries is the total number of DNS queries observed in this window
	// (used as the percent denominator for TopDomains — a different unit than
	// ObservedBytes, mirroring DeniedEvents above).
	DNSQueries uint64 `json:"dnsQueries"`
	// DNSLoggingEnabled mirrors DNSServerSettings.QueryLogging — lets the UI
	// show an accurate empty-state instead of an empty list.
	DNSLoggingEnabled bool `json:"dnsLoggingEnabled"`
	// DNSTruncated is true when the domain ring hit its per-bucket tracking
	// cap during this window (maxTrackedDomains in service/dns_query_stats.go).
	DNSTruncated bool `json:"dnsTruncated"`

	GeneratedAt string `json:"generatedAt"`
}

// DNSClientStat is one row of the "Top Clients" table on the DNS Query
// Statistics tab (docs/ref/todo/dns-query-statistics-drilldown-plan.md T-01)
// — ranked by query count, one row per source IP that issued DNS queries.
type DNSClientStat struct {
	IP string `json:"ip"`
	// Hostname is resolved from a DHCP lease/reservation, same pattern as
	// TopHost.Hostname/TopDeniedSource.Hostname above — display only, empty
	// when unknown.
	Hostname string  `json:"hostname"`
	Count    uint64  `json:"count"`
	Percent  float64 `json:"percent"`
}

// DNSQueryStatistics is the /api/statistics/dns response backing the DNS
// Query Statistics tab's two top-level tables (Top Domains / Top Clients).
// RAM-only, opt-in (query logging must be enabled from the DNS Server page)
// — same source ring as TrafficStatistics.TopDomains, but with the client
// dimension added (plan T-02).
type DNSQueryStatistics struct {
	Window       string `json:"window"` // "1h" | "24h"
	Enabled      bool   `json:"enabled"`
	TotalQueries uint64 `json:"totalQueries"`
	// Truncated is true when the domain×client pair ring or the client ring
	// hit its tracking cap during this window (plan §2.1 — the configurable
	// per-bucket caps set via dns-stats-max-pairs/dns-stats-max-clients in
	// pigate.conf, see service/dns_query_stats.go).
	Truncated bool `json:"truncated"`
	// TopDomains reuses model.TopDomain (domain/queryType/count/percent) —
	// no separate domain-row DTO for this endpoint (plan T-01 note).
	TopDomains  []TopDomain     `json:"topDomains"`
	TopClients  []DNSClientStat `json:"topClients"`
	GeneratedAt string          `json:"generatedAt"`
}

// DNSDomainDrilldown is the /api/statistics/dns/domain response — for a
// single domain, the list of clients that queried it in the window (plan
// T-01/T-03). Percent here is relative to TotalQueries of this domain, NOT
// the window total (different denominator than DNSQueryStatistics.TopClients
// above — see plan §2 item 6).
type DNSDomainDrilldown struct {
	Domain       string          `json:"domain"`
	Window       string          `json:"window"`
	Enabled      bool            `json:"enabled"`
	TotalQueries uint64          `json:"totalQueries"`
	Truncated    bool            `json:"truncated"`
	Clients      []DNSClientStat `json:"clients"`
	GeneratedAt  string          `json:"generatedAt"`
}

// DNSClientDrilldown is the /api/statistics/dns/client response — for a
// single client IP (or the "unknown" bucket), the list of domains it queried
// in the window (plan T-01/T-03). Percent here is relative to TotalQueries
// of this client, NOT the window total (same rule as DNSDomainDrilldown
// above).
type DNSClientDrilldown struct {
	Client string `json:"client"`
	// Hostname is resolved from a DHCP lease/reservation for Client, same
	// pattern as DNSClientStat.Hostname — display only, empty when unknown.
	Hostname     string      `json:"hostname"`
	Window       string      `json:"window"`
	Enabled      bool        `json:"enabled"`
	TotalQueries uint64      `json:"totalQueries"`
	Truncated    bool        `json:"truncated"`
	Domains      []TopDomain `json:"domains"`
	GeneratedAt  string      `json:"generatedAt"`
}
