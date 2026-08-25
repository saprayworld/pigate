package model

// Statistics -> Firewall page (docs/ref/todo/statistics-firewall-page-plan.md)
// DTOs. Two independently-sourced, differently-scoped byte/event accountings
// are intentionally kept side by side on this page — never mixed into one
// percentage or one denominator:
//
//   - AcceptedBytes/AcceptedPackets, BlockedBytes/BlockedPackets, and every
//     FirewallRuleStatRow/FirewallChainStat/FirewallTrendPoint byte/packet
//     figure come from the nftables PER-RULE COUNTER (kernel.
//     TrafficAccountingManager.DumpRuleCounters, via TrafficStatsService's
//     bucket ring — see traffic_stats.go's GetFirewallRuleBreakdown). This
//     source is EXACT (the kernel counts it, not an estimate), but it ONLY
//     ever reflects traffic that matched a USER-DEFINED policy rule —
//     traffic dropped by the built-in default-drop (Section 4 of the
//     input/forward chain, see docs/tech_stack_design.md §4.3) is NOT
//     counted here at all, because that drop has no associated DB rule/
//     counter.
//   - BlockedEvents and every FirewallDenyPoint/FirewallBlockedEvent/
//     TopDeniedSource/TopDeniedPort figure come from NFLOG (the SAME "deny
//     ring" statistics.go already feeds via RecordFirewallLog for
//     /api/statistics/traffic's DeniedSources/DeniedPorts). This is an EVENT
//     COUNT, not bytes, and it DOES include packets dropped by the
//     default-drop — a strictly larger, differently-shaped population than
//     BlockedBytes/BlockedPackets above.
//
// Every field here is RAM-only (never persisted to SQLite): the per-rule
// counters additionally reset to 0 on every successful Apply Settings (see
// CountersSince), and the whole response resets on a pigate restart.
type FirewallStatistics struct {
	Window      string `json:"window"`
	GeneratedAt string `json:"generatedAt"`
	// Available is false until TrafficStatsService's rule-counter poller has
	// completed at least one tick (kernel.TrafficAccountingManager via
	// RuleCountersReady) — mirrors PolicyRuleStats.Available. While false,
	// every byte/packet figure sourced from the rule counter is still zero,
	// not yet "confirmed zero".
	Available bool `json:"available"`
	// Truncated is true when the deny ring (BlockedSources/BlockedPorts/
	// RecentBlockedEvents) hit one of its per-bucket tracking caps in this
	// window (see StatisticsService.denySnapshot) — the rule-counter side of
	// this response has no such cap and never sets this.
	Truncated bool `json:"truncated"`
	// CountersSince is the RFC3339 UTC time of the last successful Apply
	// Settings (FirewallService.LastAppliedAt) — every rule-counter-sourced
	// figure on this page is "since" this timestamp, not lifetime. Empty
	// when no successful apply has happened yet this run.
	CountersSince string `json:"countersSince,omitempty"`

	AcceptedBytes   uint64 `json:"acceptedBytes"`
	AcceptedPackets uint64 `json:"acceptedPackets"`
	// BlockedBytes/BlockedPackets are the nft-rule-counter figures — see the
	// package doc comment above: NOT the same population as BlockedEvents,
	// and MUST NEVER be combined with it into a single percentage.
	BlockedBytes   uint64 `json:"blockedBytes"`
	BlockedPackets uint64 `json:"blockedPackets"`
	// BlockedEvents is the NFLOG DROP event count for this window (includes
	// default-drop traffic) — see the package doc comment.
	BlockedEvents uint64 `json:"blockedEvents"`

	RulesEnabled int `json:"rulesEnabled"`
	RulesUnused  int `json:"rulesUnused"`

	// Trend is the Firewall Traffic Trend chart (bytes) — accept vs drop,
	// nft-rule-counter-sourced, one point per 5-minute bucket.
	Trend []FirewallTrendPoint `json:"trend"`
	// DenyTrend is the Blocked Events Trend chart (event count) — NFLOG-
	// sourced, a DIFFERENT unit/axis than Trend above; never render on the
	// same chart.
	DenyTrend []FirewallDenyPoint `json:"denyTrend"`

	Chains []FirewallChainStat   `json:"chains"`
	Rules  []FirewallRuleStatRow `json:"rules"`

	BlockedSources []TopDeniedSource `json:"blockedSources"`
	BlockedPorts   []TopDeniedPort   `json:"blockedPorts"`

	// RecentBlockedEvents is the Recent Blocked Events table (added per the
	// project owner's decision on top of the original scope): the most
	// recent NFLOG DROP entries, newest first, read from the existing
	// traffic-log ring buffer (logs.RingBuffer) — up to Limit rows.
	RecentBlockedEvents []FirewallBlockedEvent `json:"recentBlockedEvents"`

	// Limit is the effective (server-clamped) row cap applied to Rules/
	// BlockedSources/BlockedPorts/RecentBlockedEvents — echoed back so the
	// UI never has to hardcode/guess it.
	Limit int `json:"limit"`
}

// FirewallTrendPoint is one 5-minute bucket of the Firewall Traffic Trend
// chart — accept vs drop bytes/packets, sourced from the nft per-rule
// counter (exact, user-rule-only — see FirewallStatistics' doc comment).
type FirewallTrendPoint struct {
	Ts              string `json:"ts"`
	AcceptedBytes   uint64 `json:"acceptedBytes"`
	BlockedBytes    uint64 `json:"blockedBytes"`
	AcceptedPackets uint64 `json:"acceptedPackets"`
	BlockedPackets  uint64 `json:"blockedPackets"`
}

// FirewallDenyPoint is one 5-minute bucket of the Blocked Events Trend
// chart — an NFLOG EVENT count (not bytes), a completely different
// unit/source than FirewallTrendPoint above.
type FirewallDenyPoint struct {
	Ts     string `json:"ts"`
	Events uint64 `json:"events"`
}

// FirewallChainStat is one row of the Chain Breakdown card/table — accept vs
// drop bytes/packets for one nftables chain ("forward"/"input"/"output"),
// from the same nft-rule-counter source as FirewallTrendPoint. Percent is
// against the sum of AcceptedBytes+BlockedBytes across ALL chains in this
// response (never against BlockedEvents).
type FirewallChainStat struct {
	Chain           string  `json:"chain"`
	AcceptedBytes   uint64  `json:"acceptedBytes"`
	BlockedBytes    uint64  `json:"blockedBytes"`
	AcceptedPackets uint64  `json:"acceptedPackets"`
	BlockedPackets  uint64  `json:"blockedPackets"`
	Percent         float64 `json:"percent"`
}

// FirewallRuleStatRow is one row of the Top Rules by Traffic table — mirrors
// PolicyRuleStat (types.go) field-for-field for the fields they share
// (RuleID/Name/Chain/Action/Log/Bytes/Packets/Percent/Unused/LastMatchedAt/
// LastMatchedSource), plus Monitored for the badge this page also shows.
// Deliberately its own struct rather than reusing PolicyRuleStat: this row
// additionally needs to represent a "stale" rule id (Name == "(deleted
// rule)") that PolicyRuleStat's source (repo.GetPolicies(), always live) can
// never produce.
type FirewallRuleStatRow struct {
	RuleID            string  `json:"ruleId"`
	Name              string  `json:"name"`
	Chain             string  `json:"chain"`
	Action            string  `json:"action"`
	Log               bool    `json:"log"`
	Monitored         bool    `json:"monitored"`
	Bytes             uint64  `json:"bytes"`
	Packets           uint64  `json:"packets"`
	Percent           float64 `json:"percent"`
	Unused            bool    `json:"unused"`
	LastMatchedAt     string  `json:"lastMatchedAt,omitempty"`
	LastMatchedSource string  `json:"lastMatchedSource,omitempty"`
}

// FirewallBlockedEvent is one row of the Recent Blocked Events table (added
// per the project owner's decision on top of the original scope — see
// FirewallStatistics.RecentBlockedEvents): a single NFLOG DROP entry, read
// directly off the existing traffic-log ring buffer (logs.RingBuffer),
// newest first. RuleName is best-effort/empty when the entry predates
// rule-name capture or the drop came from the built-in default-drop rather
// than a user rule (see the package doc comment).
type FirewallBlockedEvent struct {
	Ts       string `json:"ts"`
	SourceIP string `json:"sourceIp"`
	Proto    string `json:"proto"`
	Port     string `json:"port"`
	Chain    string `json:"chain"`
	RuleName string `json:"ruleName,omitempty"`
}
