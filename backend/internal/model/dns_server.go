package model

type DNSZone struct {
	ID              string      `json:"id"`
	ZoneName        string      `json:"zoneName"`
	ForwardTo       string      `json:"forwardTo"`
	AllowedIPs      string      `json:"allowedIps"`
	IsAuthoritative bool        `json:"isAuthoritative"`
	Enabled         bool        `json:"enabled"`
	Records         []DNSRecord `json:"records"`
}

type DNSRecord struct {
	ID     string `json:"id"`
	ZoneID string `json:"zoneId"`
	Name   string `json:"name"`
	Type   string `json:"type"`
	Value  string `json:"value"`
	TTL    int    `json:"ttl"`
}

type DNSZoneInput struct {
	ZoneName        string `json:"zoneName"`
	ForwardTo       string `json:"forwardTo"`
	AllowedIPs      string `json:"allowedIps"`
	IsAuthoritative bool   `json:"isAuthoritative"`
	Enabled         bool   `json:"enabled"`
}

type DNSRecordInput struct {
	Name  string `json:"name"`
	Type  string `json:"type"`
	Value string `json:"value"`
	TTL   int    `json:"ttl"`
}

// DNSServerSettings holds which real LAN interfaces the DNS Server should bind
// (auth-server) to. Independent from DHCP Server configs.
//
// QueryLogging/DNSCacheTTLMinutes/DNSCacheMaxEntries back the "Top Queried
// Domains" + IP->domain enrichment feature (docs/ref/todo/
// statistics-dns-top-domain-plan.md §2.1) — kept in this same DB row rather
// than internal/config, matching the precedent of dhcp_configs.lease_time
// (a user-tunable feature parameter lives in DB, not a boot flag). Changing
// QueryLogging requires a dnsmasq restart (it toggles a config directive);
// changing the TTL/cap does NOT — they are pure in-memory service parameters
// (plan §5 item 18).
type DNSServerSettings struct {
	Interfaces []string `json:"interfaces"`
	// QueryLogging enables both the "Top Queried Domains" card and IP->domain
	// enrichment of Top Destinations/Conversations. Off by default (privacy —
	// plan §5 item 1). Toggling this restarts dnsmasq.
	QueryLogging bool `json:"queryLogging"`
	// DNSCacheTTLMinutes is how long the IP->domain reverse-cache remembers a
	// mapping since it was last seen (1..1440, default 60). Applies
	// immediately, no restart.
	DNSCacheTTLMinutes int `json:"dnsCacheTtlMinutes"`
	// DNSCacheMaxEntries caps the reverse cache's entry count (128..65536,
	// default 4096). Applies immediately (including evicting down to the new
	// cap right away when lowered), no restart.
	DNSCacheMaxEntries int `json:"dnsCacheMaxEntries"`
}

// DNS query-log event kinds emitted by kernel.DNSServerManager.WatchDNSLog
// (docs/ref/todo/statistics-dns-top-domain-plan.md T-03/T-04).
const (
	DNSLogQuery  = "query"
	DNSLogAnswer = "answer"
)

// DNSLogEvent is one parsed line from dnsmasq's query log, streamed by
// kernel.DNSServerManager.WatchDNSLog. Domain/AnswerIP have already passed
// sanitizeDomain/netip.ParseAddr in the kernel-layer parser — this is a
// trusted, normalized shape by the time the service layer sees it (plan §5
// item 2: never log Domain out via log.Printf per-event).
type DNSLogEvent struct {
	Kind string // DNSLogQuery | DNSLogAnswer
	// Domain is the queried name (Kind==query) or the CNAME-chain-resolved
	// name dnsmasq is answering for (Kind==answer) — sanitized (lower-case,
	// charset-whitelisted, <=253 chars) by the parser before this struct is
	// ever constructed.
	Domain string
	// QueryType ("A"/"AAAA"/"HTTPS"/...) is set only for Kind==query.
	QueryType string
	// ClientIP is set only for Kind==query; empty when the log line's client
	// field failed to parse as an IP (never dropped for that alone).
	ClientIP string
	// AnswerIP is set only for Kind==answer, normalized via
	// netip.Addr.String() so it matches the form conntrack reports
	// (real_traffic_account.go), which is essential for IPv6.
	AnswerIP string
}
