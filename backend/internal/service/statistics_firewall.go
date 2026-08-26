package service

import (
	"sort"
	"time"

	"pigate/internal/model"
)

// Statistics -> Firewall page pipeline (docs/ref/todo/
// statistics-firewall-page-plan.md) — composes GET /api/statistics/firewall
// from data that already exists and is already polled elsewhere, exactly
// like statistics.go/statistics_traffic.go do for their own pages:
//   - TrafficStatsService.GetFirewallRuleBreakdown (traffic_stats.go T-03) —
//     per-rule bytes/packets + accept/drop trend, from the bucket ring fed
//     by the nft per-rule counter.
//   - StatisticsService.denySnapshot / denySeries (this file) — the deny
//     ring fed by RecordFirewallLog (NFLOG), for Top Denied Sources/Ports and
//     the Blocked Events Trend chart.
//   - StatisticsService.logBuffer — the existing traffic-log ring buffer,
//     read-only, for Recent Blocked Events and the "last matched (log)"
//     fallback source.
//   - TrafficStatsService.RuleLastHits — the poll-based "last matched"
//     fallback for rules without Log enabled.
//
// Nothing in this file calls the kernel directly or starts a goroutine/
// ticker (plan Caution 4/9) — it only reads state other components already
// collected.
const (
	// firewallStatsDefaultLimit/firewallStatsMaxLimit bound the `limit` query
	// param GetFirewallStatistics applies to Rules/BlockedSources/
	// BlockedPorts/RecentBlockedEvents (plan scope items 6/7: "limit 100
	// default, max 500").
	firewallStatsDefaultLimit = 100
	firewallStatsMaxLimit     = 500
)

// denySeries returns the deny ring's per-bucket EVENT COUNT series (NOT
// bytes — a different source/unit than the rule-counter trend, see
// model.FirewallStatistics' doc comment) for the Blocked Events Trend chart,
// plus the window's total event count and whether any bucket in the window
// hit a per-bucket tracking cap. Mirrors denyCapacity's axis-before-lock/
// single-RLock structure (statistics.go) — deliberately does NOT touch
// denySnapshot/denyCapacity/RecordFirewallLog themselves (T-04: those stay
// byte-for-byte unchanged, RecordFirewallLog especially must stay O(1) since
// it runs on the NFLOG read loop).
func (s *StatisticsService) denySeries(window string) ([]model.FirewallDenyPoint, uint64, bool) {
	window = normalizeStatsWindow(window)

	axisStart, n := statsSeriesAxis(window)
	series := make([]model.FirewallDenyPoint, n)
	for i := 0; i < n; i++ {
		series[i].Ts = axisStart.Add(time.Duration(i) * denyBucketSpan).Format(time.RFC3339)
	}

	var total uint64
	var truncated bool

	s.mu.RLock()
	windowBuckets := lastNBuckets(s.denyBuckets, n)
	for _, b := range windowBuckets {
		idx := statsSeriesIndex(b.ts, axisStart, n)
		series[idx].Events += b.events
		total += b.events
		if len(b.srcCount) >= s.maxTrackedDenySources || len(b.portCount) >= s.maxTrackedDenyPorts {
			truncated = true
		}
	}
	s.mu.RUnlock()

	return series, total, truncated
}

// GetFirewallStatistics composes the /api/statistics/firewall response for
// the given window/limit (docs/ref/todo/statistics-firewall-page-plan.md
// T-05). window/limit are re-validated defensively here even though the HTTP
// handler already whitelists/clamps them (plan T-06).
func (s *StatisticsService) GetFirewallStatistics(window string, limit int) (model.FirewallStatistics, error) {
	window = normalizeStatsWindow(window)
	limit = clampLimit(limit, firewallStatsDefaultLimit, firewallStatsMaxLimit)

	rules, err := s.repo.GetPolicies()
	if err != nil {
		return model.FirewallStatistics{}, err
	}

	actionByRule := make(map[string]string, len(rules))
	chainByRule := make(map[string]string, len(rules))
	ruleByID := make(map[string]model.PolicyRule, len(rules))
	rulesEnabled := 0
	for _, r := range rules {
		if !r.Status {
			continue
		}
		rulesEnabled++
		actionByRule[r.ID] = r.Action
		chainByRule[r.ID] = r.Chain
		ruleByID[r.ID] = r
	}

	breakdown := s.traffic.GetFirewallRuleBreakdown(window, actionByRule)

	var lastHitsByPoll map[string]time.Time
	available := false
	if s.traffic != nil {
		lastHitsByPoll = s.traffic.RuleLastHits()
		available = s.traffic.RuleCountersReady()
	}
	var lastMatchedByLog map[string]string
	if s.logBuffer != nil {
		lastMatchedByLog = s.logBuffer.LastMatchedByRule()
	}

	// Summary totals + chain breakdown, computed in one pass over
	// breakdown.RuleTotals (skips stale/deleted rule ids for the chain
	// breakdown — there is no chain to attribute them to — but they are
	// still represented in the Rules rows below as "(deleted rule)").
	var acceptedBytes, acceptedPackets, blockedBytes, blockedPackets uint64
	chainTotals := make(map[string]*model.FirewallChainStat)
	for id, c := range breakdown.RuleTotals {
		if breakdown.Stale[id] {
			continue
		}
		action := actionByRule[id]
		chain := chainByRule[id]
		ct, ok := chainTotals[chain]
		if !ok {
			ct = &model.FirewallChainStat{Chain: chain}
			chainTotals[chain] = ct
		}
		switch action {
		case "ACCEPT":
			acceptedBytes += c.Bytes
			acceptedPackets += c.Packets
			ct.AcceptedBytes += c.Bytes
			ct.AcceptedPackets += c.Packets
		case "DROP":
			blockedBytes += c.Bytes
			blockedPackets += c.Packets
			ct.BlockedBytes += c.Bytes
			ct.BlockedPackets += c.Packets
		}
	}

	totalChainBytes := acceptedBytes + blockedBytes
	chains := make([]model.FirewallChainStat, 0, len(chainTotals))
	for _, ct := range chainTotals {
		ct.Percent = percentOf(ct.AcceptedBytes+ct.BlockedBytes, totalChainBytes)
		chains = append(chains, *ct)
	}
	sort.Slice(chains, func(i, j int) bool { return chains[i].Chain < chains[j].Chain })

	// Rules rows: every rule id ever seen in this window's ruleBytes, ranked
	// by bytes desc, cut to limit. A stale id (deleted/renamed rule) is
	// rendered with a placeholder name/chain/action rather than skipped or
	// panicking (plan Risk 5).
	//
	// totalRuleBytes is the Percent divisor for every row, stale included, so
	// row percentages always sum to <=100% (issue #156: using
	// acceptedBytes+blockedBytes alone excludes stale bytes from the
	// divisor, letting a stale row's own percentage read over 100%).
	var staleBytes uint64
	for id := range breakdown.Stale {
		staleBytes += breakdown.RuleTotals[id].Bytes
	}
	totalRuleBytes := acceptedBytes + blockedBytes + staleBytes
	rows := make([]model.FirewallRuleStatRow, 0, len(breakdown.RuleTotals))
	rulesUnused := 0
	for _, r := range rules {
		if !r.Status {
			continue
		}
		c := breakdown.RuleTotals[r.ID]
		unused := c.Bytes == 0 && c.Packets == 0
		if unused {
			rulesUnused++
		}
		row := model.FirewallRuleStatRow{
			RuleID:    r.ID,
			Name:      r.Name,
			Chain:     r.Chain,
			Action:    r.Action,
			Log:       r.Log,
			Monitored: r.Monitored,
			Bytes:     c.Bytes,
			Packets:   c.Packets,
			Percent:   percentOf(c.Bytes, totalRuleBytes),
			Unused:    unused,
		}
		if r.Log {
			if t, ok := lastMatchedByLog[r.ID]; ok && t != "" {
				row.LastMatchedAt = t
				row.LastMatchedSource = "log"
			}
		}
		if row.LastMatchedAt == "" {
			if t, ok := lastHitsByPoll[r.ID]; ok {
				row.LastMatchedAt = t.UTC().Format(time.RFC3339)
				row.LastMatchedSource = "counter"
			}
		}
		rows = append(rows, row)
	}
	// Stale ids (present in the bucket ring, no longer a live enabled rule)
	// are appended as their own rows so their traffic is still visible.
	for id := range breakdown.Stale {
		c := breakdown.RuleTotals[id]
		rows = append(rows, model.FirewallRuleStatRow{
			RuleID:  id,
			Name:    "(deleted rule)",
			Bytes:   c.Bytes,
			Packets: c.Packets,
			Percent: percentOf(c.Bytes, totalRuleBytes),
			Unused:  c.Bytes == 0 && c.Packets == 0,
		})
	}
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Bytes != rows[j].Bytes {
			return rows[i].Bytes > rows[j].Bytes
		}
		return rows[i].RuleID < rows[j].RuleID
	})
	if len(rows) > limit {
		rows = rows[:limit]
	}

	srcTotals, portTotals, deniedEvents, denyTruncated := s.denySnapshot(window)
	leaseByIP, resByIP := s.traffic.hostLookup()
	blockedSources := buildTopDeniedSources(srcTotals, deniedEvents, leaseByIP, resByIP, limit)
	blockedPorts := buildTopDeniedPorts(portTotals, deniedEvents, limit)

	denyTrend, _, denySeriesTruncated := s.denySeries(window)

	recentBlockedEvents := s.recentBlockedEvents(limit)

	var countersSince string
	if s.firewall != nil {
		if t := s.firewall.LastAppliedAt(); !t.IsZero() {
			countersSince = t.UTC().Format(time.RFC3339)
		}
	}

	return model.FirewallStatistics{
		Window:              window,
		GeneratedAt:         time.Now().UTC().Format(time.RFC3339),
		Available:           available,
		Truncated:           denyTruncated || denySeriesTruncated,
		CountersSince:       countersSince,
		AcceptedBytes:       acceptedBytes,
		AcceptedPackets:     acceptedPackets,
		BlockedBytes:        blockedBytes,
		BlockedPackets:      blockedPackets,
		BlockedEvents:       deniedEvents,
		RulesEnabled:        rulesEnabled,
		RulesUnused:         rulesUnused,
		Trend:               breakdown.Trend,
		DenyTrend:           denyTrend,
		Chains:              chains,
		Rules:               rows,
		BlockedSources:      blockedSources,
		BlockedPorts:        blockedPorts,
		RecentBlockedEvents: recentBlockedEvents,
		Limit:               limit,
	}, nil
}

// recentBlockedEvents reads up to limit DROP entries off the existing
// traffic-log ring buffer (logs.RingBuffer.GetAll — already the buffer's
// own, existing accessor, no new ring-scanning logic added here), newest
// first (GetAll's own ordering). Returns an empty (non-nil) slice when no
// log buffer is wired (e.g. a unit test that never calls SetLogBuffer) or no
// DROP entry exists yet.
func (s *StatisticsService) recentBlockedEvents(limit int) []model.FirewallBlockedEvent {
	out := make([]model.FirewallBlockedEvent, 0, limit)
	if s.logBuffer == nil {
		return out
	}
	for _, entry := range s.logBuffer.GetAll() {
		if entry.Action != "DROP" {
			continue
		}
		out = append(out, model.FirewallBlockedEvent{
			Ts:       entry.Time,
			SourceIP: entry.Src,
			Proto:    entry.Proto,
			Port:     entry.Port,
			Chain:    entry.Chain,
			RuleName: entry.RuleName,
		})
		if len(out) >= limit {
			break
		}
	}
	return out
}
