package service

import (
	"sort"
	"time"

	"pigate/internal/db"
	"pigate/internal/logs"
	"pigate/internal/model"
)

// PolicyStatsService composes the GET /api/policies/stats response by merging
// three data sources that already exist and are already polled elsewhere
// (docs/ref/todo/firewall-policy-rule-usage-stats-plan.md Design decision 3 —
// this service never calls the kernel directly and never starts a new
// goroutine/ticker):
//   - TrafficStatsService.RuleCounterSnapshot() — cumulative nft byte/packet
//     counters since the poller first saw each rule id, reset-detected on
//     every ApplyRules (Design decision 1: nftables' own counters reset to 0
//     on every full-table sync, so these numbers are "since last apply").
//   - RingBuffer.LastMatchedByRule() — precise "last matched" for rules that
//     have Log enabled, from a single scan of the traffic-log ring buffer.
//   - TrafficStatsService.RuleLastHits() — poll-based fallback "last matched"
//     (±flowPollInterval accuracy) for rules that don't have Log enabled, or
//     as a fallback when a Log-enabled rule's evidence fell out of the ring
//     buffer.
type PolicyStatsService struct {
	repo         *db.Repository
	firewall     *FirewallService
	trafficStats *TrafficStatsService
	ringBuffer   *logs.RingBuffer

	// domainLookup is the optional batch DNS-reverse-cache lookup used by
	// GetRuleEndpoints (docs/ref/todo/firewall-rule-matched-endpoints-plan.md
	// T-05). It is set post-construction via SetDomainLookup — mirroring
	// SetPolicyStatsService's own wiring in main.go — specifically so this
	// constructor's signature never changes (StatisticsService, which owns
	// LookupDomains, would otherwise have to become a fifth constructor
	// parameter, and PolicyStatsService/StatisticsService are constructed in
	// an order that makes a direct dependency awkward).
	domainLookup func([]string) map[string]string
}

// NewPolicyStatsService constructs the service. Any dependency may be nil in
// theory (defensive nil-checks below), but main.go always wires all four.
func NewPolicyStatsService(repo *db.Repository, firewall *FirewallService, trafficStats *TrafficStatsService, ringBuffer *logs.RingBuffer) *PolicyStatsService {
	return &PolicyStatsService{
		repo:         repo,
		firewall:     firewall,
		trafficStats: trafficStats,
		ringBuffer:   ringBuffer,
	}
}

// SetDomainLookup wires the batch DNS reverse-cache lookup (typically
// StatisticsService.LookupDomains) used by GetRuleEndpoints to resolve
// destination IPs to a domain name. Optional: GetRuleEndpoints tolerates it
// being unset (Domain simply stays "" on every EndpointHit).
func (s *PolicyStatsService) SetDomainLookup(fn func([]string) map[string]string) {
	s.domainLookup = fn
}

// GetPolicyRuleStats returns usage stats for every enabled policy rule,
// optionally filtered to one chain. chain is trusted to already be validated
// by the caller (one of model.PolicyChainForward/Input/Output, or "" for
// every chain — see api.HandleGetPolicyStats). TotalBytes/TotalPackets/every
// rule's Percent are always computed across ALL chains regardless of the
// chain filter (Design decision 2), so a rule's percentage never changes
// depending on which page it's viewed from. Disabled rules are always
// excluded — they were never created in nftables, so there is no counter to
// report (see "สภาพปัจจุบัน" note in the plan); the frontend shows "—" for
// them instead of 0/Unused.
func (s *PolicyStatsService) GetPolicyRuleStats(chain string) (model.PolicyRuleStats, error) {
	rules, err := s.repo.GetPolicies()
	if err != nil {
		return model.PolicyRuleStats{}, err
	}

	var counters map[string]model.RuleCounter
	var lastHitsByPoll map[string]time.Time
	available := false
	if s.trafficStats != nil {
		counters = s.trafficStats.RuleCounterSnapshot()
		lastHitsByPoll = s.trafficStats.RuleLastHits()
		available = s.trafficStats.RuleCountersReady()
	}
	var lastMatchedByLog map[string]string
	if s.ringBuffer != nil {
		lastMatchedByLog = s.ringBuffer.LastMatchedByRule()
	}

	var countersSince string
	if s.firewall != nil {
		if t := s.firewall.LastAppliedAt(); !t.IsZero() {
			countersSince = t.UTC().Format(time.RFC3339)
		}
	}

	// Totals are always computed across every enabled rule in every chain
	// (Design decision 2), BEFORE the chain filter below is applied.
	var totalBytes, totalPackets uint64
	for _, r := range rules {
		if !r.Status {
			continue
		}
		c := counters[r.ID]
		totalBytes += c.Bytes
		totalPackets += c.Packets
	}

	out := make([]model.PolicyRuleStat, 0, len(rules))
	for _, r := range rules {
		if !r.Status {
			continue
		}
		if chain != "" && r.Chain != chain {
			continue
		}

		c := counters[r.ID]
		stat := model.PolicyRuleStat{
			RuleID:  r.ID,
			Name:    r.Name,
			Chain:   r.Chain,
			Action:  r.Action,
			Log:     r.Log,
			Status:  r.Status,
			Bytes:   c.Bytes,
			Packets: c.Packets,
			Percent: percentOf(c.Bytes, totalBytes),
			Unused:  c.Bytes == 0 && c.Packets == 0,
		}

		// Hybrid "last matched at" (Design decision 3): prefer the precise
		// ring-buffer log source when the rule has Log enabled, otherwise (or
		// if the log evidence isn't/no-longer in the buffer) fall back to the
		// poll-based counter-delta timestamp.
		if r.Log {
			if t, ok := lastMatchedByLog[r.ID]; ok && t != "" {
				stat.LastMatchedAt = t
				stat.LastMatchedSource = "log"
			}
		}
		if stat.LastMatchedAt == "" {
			if t, ok := lastHitsByPoll[r.ID]; ok {
				stat.LastMatchedAt = t.UTC().Format(time.RFC3339)
				stat.LastMatchedSource = "counter"
			}
		}

		out = append(out, stat)
	}

	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].RuleID < out[j].RuleID
	})

	return model.PolicyRuleStats{
		Rules:         out,
		TotalBytes:    totalBytes,
		TotalPackets:  totalPackets,
		CountersSince: countersSince,
		Available:     available,
	}, nil
}
