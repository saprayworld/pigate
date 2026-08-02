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
	// BytesUp/BytesDown split Bytes by the flow's own orig/reply direction
	// (orig = up, reply = down), same convention for Top Source Hosts and
	// Top Destinations alike — not relative to IP's own role in the flow
	// (bytesUp + bytesDown == bytes always — docs/ref/todo/
	// statistics-split-upload-download-bytes-plan.md §2.5).
	BytesUp   uint64 `json:"bytesUp"`
	BytesDown uint64 `json:"bytesDown"`
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
	// BytesUp/BytesDown split Bytes by direction relative to SrcIP (the row's
	// owner): BytesUp = Orig (SrcIP -> DstIP), BytesDown = Reply (DstIP ->
	// SrcIP). bytesUp + bytesDown == bytes always.
	BytesUp   uint64 `json:"bytesUp"`
	BytesDown uint64 `json:"bytesDown"`
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

// BandwidthPoint is one point of TrafficStatistics.Series — a fixed 5-minute
// bucket of the same bucket ring backing the rest of this response (plan
// docs/ref/todo/statistics-overview-bandwidth-chart-plan.md T-01/T-03).
// Direction here is LAN-relative (Up = leaving the LAN, Down = entering the
// LAN), NOT flow-relative like TopHost.BytesUp/BytesDown above — see the
// plan §2.2/§5 item 18 for why the two can disagree on the same page.
type BandwidthPoint struct {
	// Ts is the RFC3339 start of this bucket, in device local time (same
	// clock/format as the internal bucket ring — intentionally not UTC like
	// GeneratedAt below; the offset in RFC3339 makes both parseable browser-side).
	Ts string `json:"ts"`
	// Bytes always equals BytesUp + BytesDown.
	Bytes     uint64 `json:"bytes"`
	BytesUp   uint64 `json:"bytesUp"`
	BytesDown uint64 `json:"bytesDown"`
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

	// Series is the bandwidth-over-time chart backing the Statistics
	// Overview page's top row (plan T-01/T-03): one BandwidthPoint per raw
	// 5-minute bucket, zero-filled and carried to the nearest edge point when
	// the ring covers a wider span than the window (plan §2.5/§7 item 6), so
	// Series always has a fixed length (12 for "1h", 288 for "24h"), sorted
	// oldest -> newest, and sum(Series[].Bytes) == ObservedBytes exactly.
	Series []BandwidthPoint `json:"series"`

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

// Statistics -> Traffic page DTOs (docs/ref/todo/statistics-traffic-page-plan.md
// T-01) — back the new GET /api/statistics/traffic/hosts and GET
// /api/statistics/traffic/host endpoints. TopHost/TopConversation/
// TrafficStatistics above are deliberately left untouched (they back the
// existing /api/statistics/traffic response and the Overview page and must
// stay byte-for-byte compatible — plan §1.7). Everything below is composed
// from the same RAM-only conversation ring as TrafficStatistics (plan §0) —
// nothing here is ever persisted to SQLite.

// TrafficTopHosts is the GET /api/statistics/traffic/hosts response: the
// FULL (not top-10-cut) Top Source Hosts / Top Destinations lists, up to
// Limit rows each, so the Traffic page can filter/sort more than the
// Overview page's statsTopN cards ever expose (plan §1.2).
type TrafficTopHosts struct {
	Window        string `json:"window"` // "1h" | "24h"
	ObservedBytes uint64 `json:"observedBytes"`
	// Accuracy mirrors TrafficStatistics.Accuracy ("estimated" | "near-exact").
	Accuracy string `json:"accuracy"`
	// Truncated is true when the underlying bucket ring hit one of its
	// per-bucket tracking caps during this window (traffic-stats-max-hosts /
	// -max-dests / -max-conversations, plan §1.6) — the UI must show a
	// warning that the ranking below may be incomplete.
	Truncated bool `json:"truncated"`
	// Limit is the effective row cap actually applied to each list below
	// (1..500, clamped server-side — plan §1.2), echoed back so the UI can
	// tell "fewer than Limit rows exist" apart from "capped at Limit".
	Limit        int       `json:"limit"`
	Sources      []TopHost `json:"sources"`
	Destinations []TopHost `json:"destinations"`
	// Series is the bandwidth-over-time chart backing the Traffic page's top
	// row (docs/ref/todo/statistics-traffic-bandwidth-chart-plan.md T-01/T-02)
	// — the SAME network-wide, LAN-relative series as TrafficStatistics.Series
	// above (Up = leaving the LAN, Down = entering it), fed by the identical
	// bucket computation so sum(Series[].Bytes) == ObservedBytes exactly.
	// Fixed length (12 for "1h", 288 for "24h"), sorted oldest -> newest.
	Series      []BandwidthPoint `json:"series"`
	GeneratedAt string           `json:"generatedAt"`
}

// TrafficHostConversation is one drill-down row of TrafficHostDetail —
// TopConversation's fields (embedded so the JSON stays flat and identical to
// TopConversation's own shape) plus two IP-relative fields that only make
// sense once a conversation is being viewed "from" a particular IP:
//   - Direction: "outbound" when the drilled IP is this row's SrcIP,
//     "inbound" when it is the DstIP (plan §1.3/§1.5) — BytesUp/BytesDown
//     stay flow-relative (Orig/Reply) exactly like TopConversation; Direction
//     is the ONLY field here that is relative to the drilled IP. Never flip
//     BytesUp/BytesDown based on Direction (plan Caution 3 — the bug fixed by
//     commit 10d53ae).
//   - PeerDomain: the reverse-DNS-cache domain of the OTHER side of the
//     conversation relative to the drilled IP. For an outbound row (drilled
//     IP is SrcIP) this equals DstDomain; for an inbound row (drilled IP is
//     DstIP) it is the SrcIP's domain, which DstDomain cannot express.
//
// A row is a 4-tuple conversation (srcIP, dstIP, proto, dstPort), not a
// single TCP connection (plan §1.1 — model.FlowSample carries no source
// port). RAM-only, never persisted.
type TrafficHostConversation struct {
	TopConversation
	Direction  string `json:"direction"`
	PeerDomain string `json:"peerDomain"`
}

// TrafficHostDetail is the GET /api/statistics/traffic/host response — a
// single IP's conversations in both directions (plan §1.3).
//
// Percent on each row of AsSource/AsDestination is relative to TotalBytes
// (THIS IP's own total across both directions in the window), NOT
// ObservedBytes (plan §1.4) — ObservedBytes is included only so the UI can
// also show PercentOfObserved as a second, clearly-labelled denominator.
type TrafficHostDetail struct {
	IP       string `json:"ip"`
	Hostname string `json:"hostname"`
	MAC      string `json:"mac"`
	// Domain is the drilled IP's own reverse-DNS-cache domain (same source as
	// TopHost.Domain), display only.
	Domain string `json:"domain"`
	// Private is true when IP is RFC1918/link-local/loopback/ULA (a LAN
	// address), same classifier as TopHost.Private.
	Private bool   `json:"private"`
	Window  string `json:"window"`
	// Accuracy mirrors TrafficStatistics.Accuracy.
	Accuracy string `json:"accuracy"`
	// Truncated mirrors TrafficTopHosts.Truncated above.
	Truncated bool `json:"truncated"`
	// Limit is the effective per-list row cap actually applied to
	// AsSource/AsDestination independently (1..300, clamped server-side).
	Limit int `json:"limit"`
	// Found is false when IP appears nowhere in the window's buckets (neither
	// as a conversation participant nor in the raw Hosts/Dests maps) — the UI
	// should show an explicit "no data" state, not an empty table.
	Found bool `json:"found"`
	// TotalBytes/TotalBytesUp/TotalBytesDown are this IP's totals across BOTH
	// directions (as source PLUS as destination), computed over the FULL
	// (untruncated) conversation set before the Limit cut below is applied —
	// so the header total never contradicts a truncated table (plan T-03).
	TotalBytes     uint64 `json:"totalBytes"`
	TotalBytesUp   uint64 `json:"totalBytesUp"`
	TotalBytesDown uint64 `json:"totalBytesDown"`
	// PercentOfObserved is TotalBytes as a percent of ObservedBytes (the
	// window total) — a DIFFERENT denominator than the per-row Percent above,
	// included so the UI can show both figures without confusing them (plan
	// §1.4).
	PercentOfObserved float64 `json:"percentOfObserved"`
	ObservedBytes     uint64  `json:"observedBytes"`
	// AsSource lists conversations where IP is the flow's SrcIP ("this IP
	// went to these destinations"); AsDestination lists conversations where
	// IP is the flow's DstIP ("these hosts came to this IP"). Never nil (an
	// empty result is make([]TrafficHostConversation, 0), so the JSON is
	// always `[]`, never `null`).
	AsSource      []TrafficHostConversation `json:"asSource"`
	AsDestination []TrafficHostConversation `json:"asDestination"`
	// Series is the bandwidth-over-time chart backing the drill-down page's
	// per-IP graph (docs/ref/todo/statistics-traffic-bandwidth-chart-plan.md
	// T-01/T-02) — unlike TrafficTopHosts.Series/TrafficStatistics.Series
	// above (both network-wide, LAN-relative), this is THIS IP's traffic
	// ONLY, and direction is flow-relative (Orig = up, Reply = down) — the
	// SAME convention as TotalBytesUp/TotalBytesDown and the AsSource/
	// AsDestination rows above, NOT the LAN-relative convention of
	// TrafficStatistics.Series. Invariant: sum(Series[].Bytes) == TotalBytes,
	// sum(Series[].BytesUp) == TotalBytesUp, sum(Series[].BytesDown) ==
	// TotalBytesDown, by construction (same convBytes map TotalBytes is
	// summed from — see GetTrafficBreakdownForIP). Fixed length (12 for "1h",
	// 288 for "24h") always, even when Found is false (a zero-filled array,
	// never nil).
	Series      []BandwidthPoint `json:"series"`
	GeneratedAt string           `json:"generatedAt"`
}
