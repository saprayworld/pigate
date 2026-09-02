package model

import "strings"

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
	// GlueIPs is the NS-delegation glue: the IP address(es) of the
	// nameserver named in Value. Only meaningful for Type == "NS" and only
	// for a non-apex record name. When non-empty, the generator emits
	// `server=/<fqdn>/<ip>` per IP so dnsmasq actually FORWARDS queries
	// under that name to the delegated nameserver (forwarding-based
	// delegation), instead of only publishing the NS record via dns-rr.
	// Empty (the default, and the value of every pre-existing row) keeps
	// the previous publish-only behaviour byte-for-byte.
	GlueIPs []string `json:"glueIps"`
	// DelegationMode selects HOW an NS record's subtree is forwarded.
	// "" (every pre-existing row) and "glue" behave identically to before:
	// forward to the GlueIPs, or publish-only when there are none.
	// "upstream" emits a single `server=/<fqdn>/#` instead, handing the
	// subtree to the box's normal upstream resolvers — the only way to get
	// a complete answer when the delegated nameserver replies with a CNAME
	// pointing outside its own zone (dnsmasq cannot merge records from two
	// different sources; see docs/ref/todo/dns-ns-delegation-cname-fix-plan.md §2).
	DelegationMode string `json:"delegationMode"`
}

type DNSZoneInput struct {
	ZoneName        string `json:"zoneName"`
	ForwardTo       string `json:"forwardTo"`
	AllowedIPs      string `json:"allowedIps"`
	IsAuthoritative bool   `json:"isAuthoritative"`
	Enabled         bool   `json:"enabled"`
}

type DNSRecordInput struct {
	Name           string   `json:"name"`
	Type           string   `json:"type"`
	Value          string   `json:"value"`
	TTL            int      `json:"ttl"`
	GlueIPs        []string `json:"glueIps"`
	DelegationMode string   `json:"delegationMode"`
}

// DNSNSGlueMaxIPs caps how many glue IPs one NS record may carry. Each
// one becomes its own `server=` line in pigate-dns.conf; 4 covers every
// realistic delegation (2 NS x A+AAAA) without letting one record bloat
// the config.
const DNSNSGlueMaxIPs = 4

// NS delegation modes — see DNSRecord.DelegationMode doc comment.
const (
	DNSNSDelegationModeGlue     = "glue"
	DNSNSDelegationModeUpstream = "upstream"
)

// EffectiveNSDelegationMode normalizes DelegationMode to exactly one of
// the two real modes. Called by BOTH ValidateDNSRecord and the generator
// so the two can never disagree (same discipline as EncodeDNSNameHex).
func EffectiveNSDelegationMode(r DNSRecord) string {
	switch strings.ToLower(strings.TrimSpace(r.DelegationMode)) {
	case DNSNSDelegationModeUpstream:
		return DNSNSDelegationModeUpstream
	default:
		// "" (pre-existing rows), "glue", and any unrecognized/garbage value
		// all fall back to the pre-existing glue behaviour — the generator
		// stays safe by default, the validator is the one that rejects
		// unrecognized values outright.
		return DNSNSDelegationModeGlue
	}
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
	// UpstreamMode selects where the DNS Server (dnsmasq) sources its upstream
	// resolvers from when generating pigate-dns.conf: DNSUpstreamModeSystem
	// (default) reads System DNS (service/dns.go) at generate-time, read-only;
	// DNSUpstreamModeCustom uses UpstreamServers exclusively. See
	// docs/ref/todo/dns-server-settings-tab-and-upstream-plan.md §2. Changing
	// this restarts dnsmasq (like QueryLogging), unlike TTL/cap.
	UpstreamMode string `json:"upstreamMode"`
	// UpstreamServers is used only when UpstreamMode == DNSUpstreamModeCustom
	// (<= DNSUpstreamMaxServers bare IPv4/IPv6 addresses, no port/hostname/DoH/
	// DoT). Kept (but unused) when switching back to "system" so the user's
	// entries are not lost.
	UpstreamServers []string `json:"upstreamServers"`
}

// BlockedDomain is one deny-list entry (docs/ref/todo/dns-blocked-domains-plan.md
// §2). Domain and any Subdomain of it are answered NXDOMAIN or a sinkhole
// address by dnsmasq, depending on Mode, instead of being forwarded upstream
// or resolved against a zone. Stored in its own table (dns_blocked_domains) —
// additive to, and independent from, dns_zones/dns_records.
type BlockedDomain struct {
	ID        string `json:"id"`
	Domain    string `json:"domain"`
	Mode      string `json:"mode"`
	Enabled   bool   `json:"enabled"`
	Comment   string `json:"comment"`
	CreatedAt string `json:"createdAt"`
}

// BlockedDomainInput is the create/update request body for a BlockedDomain
// (no ID/CreatedAt — server-assigned).
type BlockedDomainInput struct {
	Domain  string `json:"domain"`
	Mode    string `json:"mode"`
	Enabled bool   `json:"enabled"`
	Comment string `json:"comment"`
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
