package service

import (
	"errors"
	"sort"
	"strings"

	"pigate/internal/logs"
	"pigate/internal/model"
)

// ErrPolicyRuleNotFound is returned by GetRuleEndpoints when ruleID does not
// match any rule currently in the DB — api.HandleGetPolicyRuleEndpoints
// translates it into an HTTP 404.
var ErrPolicyRuleNotFound = errors.New("policy rule not found")

// defaultEndpointsLimit / min/maxEndpointsLimit mirror the contract in
// docs/ref/todo/firewall-rule-matched-endpoints-plan.md §3 — the API layer
// (T-06) is the source of truth for validating a caller-supplied limit and
// returning 400 out of range; GetRuleEndpoints itself just defensively
// clamps so it never panics or returns an unbounded list if called directly
// (e.g. from a future internal caller or a test).
const (
	defaultEndpointsLimit = 10
	minEndpointsLimit     = 1
	maxEndpointsLimit     = 50
)

// endpointCount pairs a raw key (IP, or "PROTO/PORT") with its tally, used
// only while sorting/truncating before name resolution.
type endpointCount struct {
	key string
	c   logs.Counted
}

// GetRuleEndpoints answers "which IPs/services matched this rule" from one
// of two sources (docs/ref/todo/persisted-rule-endpoints-plan.md E-D6, issue
// #141 follow-up):
//
//   - source="persisted": when the rule has Monitor enabled and the
//     monitored-endpoints-enabled kill switch is on, data comes from the
//     policy_rule_endpoints SQLite table (survives Apply/restart/clearing
//     the traffic log) plus whatever the RAM recorder hasn't flushed yet.
//   - source="buffer": the original behavior (docs/ref/todo/
//     firewall-rule-matched-endpoints-plan.md) — a single scan of the
//     traffic-log ring buffer (RingBuffer.AggregateByRule). Unconditional
//     for a rule that isn't monitored, or when the kill switch is off.
//
// Both paths funnel through the exact same ranking (topEndpointCounts) and
// name-resolution code below — the only thing that differs between them is
// where the map[string]logs.Counted tallies come from (Caution 11: the
// buffer path's behavior/output must be byte-for-byte unchanged).
func (s *PolicyStatsService) GetRuleEndpoints(ruleID string, limit int) (model.PolicyRuleEndpoints, error) {
	if limit < minEndpointsLimit || limit > maxEndpointsLimit {
		limit = defaultEndpointsLimit
	}

	if s.repo == nil {
		return model.PolicyRuleEndpoints{}, ErrPolicyRuleNotFound
	}
	rules, err := s.repo.GetPolicies()
	if err != nil {
		return model.PolicyRuleEndpoints{}, err
	}
	var rule model.PolicyRule
	found := false
	for _, r := range rules {
		if r.ID == ruleID {
			rule = r
			found = true
			break
		}
	}
	if !found {
		return model.PolicyRuleEndpoints{}, ErrPolicyRuleNotFound
	}

	usePersisted := s.endpointsEnabled && rule.Monitored && s.endpointStore != nil

	var srcCounts, dstCounts, svcCounts map[string]logs.Counted
	var uniqueSources, uniqueDests, uniqueServices int
	var scannedEntries int
	var bufferOldestAt string
	respSource := "buffer"
	var collectingSince string
	var capped bool
	var evicted int
	maxPerDirection := s.endpointsMaxPerDirection
	if maxPerDirection <= 0 {
		maxPerDirection = defaultMaxEndpointsPerDirection
	}

	var matchedEntriesFromBuffer int

	if usePersisted {
		respSource = "persisted"

		srcCounts, err = s.persistedDirectionCounts(rule.ID, model.EndpointDirectionSrc, limit)
		if err != nil {
			return model.PolicyRuleEndpoints{}, err
		}
		dstCounts, err = s.persistedDirectionCounts(rule.ID, model.EndpointDirectionDst, limit)
		if err != nil {
			return model.PolicyRuleEndpoints{}, err
		}
		svcCounts, err = s.persistedDirectionCounts(rule.ID, model.EndpointDirectionSvc, limit)
		if err != nil {
			return model.PolicyRuleEndpoints{}, err
		}

		directionRowCounts, err := s.repo.CountPolicyEndpoints(rule.ID)
		if err != nil {
			return model.PolicyRuleEndpoints{}, err
		}
		uniqueSources = directionRowCounts[model.EndpointDirectionSrc]
		uniqueDests = directionRowCounts[model.EndpointDirectionDst]
		uniqueServices = directionRowCounts[model.EndpointDirectionSvc]
		capped = uniqueSources >= maxPerDirection || uniqueDests >= maxPerDirection || uniqueServices >= maxPerDirection

		if totals := s.endpointStore.Totals(); totals != nil {
			collectingSince = totals[rule.ID].StartedAt
		}
		evicted = s.endpointStore.EndpointsEvictedFor(rule.ID)
		// scannedEntries/bufferOldestAt stay at their zero value — they only
		// have meaning for the buffer path (model.PolicyRuleEndpoints doc
		// comment).
	} else {
		var agg logs.RuleAggregate
		if s.ringBuffer != nil {
			agg = s.ringBuffer.AggregateByRule(rule.ID)
		}
		srcCounts, dstCounts, svcCounts = agg.Sources, agg.Dests, agg.Services
		uniqueSources, uniqueDests, uniqueServices = len(agg.Sources), len(agg.Dests), len(agg.Services)
		scannedEntries = agg.Scanned
		bufferOldestAt = agg.OldestTime
		matchedEntriesFromBuffer = agg.Matched
	}

	srcTop := topEndpointCounts(srcCounts, limit)
	destTop := topEndpointCounts(dstCounts, limit)
	svcTop := topEndpointCounts(svcCounts, limit)

	truncated := uniqueSources > len(srcTop) || uniqueDests > len(destTop) || uniqueServices > len(svcTop)

	// Batch name resolution: exactly one addrMatcher build, one
	// LookupHostnames call, one domainLookup call — using only the IPs that
	// survived the top-N cut (plan T-05 step 4).
	ips := make([]string, 0, len(srcTop)+len(destTop))
	for _, e := range srcTop {
		ips = append(ips, e.key)
	}
	for _, e := range destTop {
		ips = append(ips, e.key)
	}

	var addrs []model.AddressObject
	if s.repo != nil {
		addrs, _ = s.repo.GetAddresses()
	}
	matcher := newAddrMatcher(addrs)

	var hostnames map[string]string
	if s.trafficStats != nil {
		hostnames = s.trafficStats.LookupHostnames(ips)
	}
	var domains map[string]string
	if s.domainLookup != nil {
		domains = s.domainLookup(ips)
	}

	sourceObjNames := stringSet(rule.Source)
	destObjNames := stringSet(rule.Destination)

	buildHit := func(e endpointCount, ownObjNames map[string]struct{}) model.EndpointHit {
		addrName, ok := matcher.Match(e.key)
		fromRule := false
		if ok {
			_, fromRule = ownObjNames[addrName]
		} else {
			addrName = ""
		}
		return model.EndpointHit{
			IP:          e.key,
			Count:       e.c.Count,
			FirstSeenAt: e.c.FirstSeen,
			LastSeenAt:  e.c.LastSeen,
			AddressName: addrName,
			Hostname:    hostnames[e.key],
			Domain:      domains[e.key],
			FromRule:    fromRule,
		}
	}

	sources := make([]model.EndpointHit, 0, len(srcTop))
	for _, e := range srcTop {
		sources = append(sources, buildHit(e, sourceObjNames))
	}
	destinations := make([]model.EndpointHit, 0, len(destTop))
	for _, e := range destTop {
		destinations = append(destinations, buildHit(e, destObjNames))
	}

	services := make([]model.ServiceHit, 0, len(svcTop))
	for _, e := range svcTop {
		proto, port := splitServiceKey(e.key)
		var svcName string
		if s.trafficStats != nil {
			svcName = s.trafficStats.ServiceNameFor(proto, port)
		}
		services = append(services, model.ServiceHit{
			Proto:       proto,
			Port:        port,
			Count:       e.c.Count,
			FirstSeenAt: e.c.FirstSeen,
			LastSeenAt:  e.c.LastSeen,
			ServiceName: svcName,
			FromRule:    serviceNameReferencedByRule(svcName, rule.Service),
		})
	}

	// MatchedEntries: buffer path keeps its exact original meaning (Matched
	// from AggregateByRule — a precise count of scanned log entries). The
	// persisted path has no cheap equivalent (that would require summing
	// every row for the src direction, not just the top-N this handler
	// already fetched) — an approximation using the sum of the returned top
	// sources is used instead, which is the closest meaning achievable
	// without an extra full-table query (docs/ref/todo/
	// persisted-rule-endpoints-plan.md E-07: "เป็นค่าประมาณเมื่อมาจากข้อมูล
	// persist").
	matchedEntries := matchedEntriesFromBuffer
	if usePersisted {
		for _, e := range srcTop {
			matchedEntries += e.c.Count
		}
	}

	return model.PolicyRuleEndpoints{
		RuleID:             rule.ID,
		RuleName:           rule.Name,
		Chain:              rule.Chain,
		LogEnabled:         rule.Log,
		MatchedEntries:     matchedEntries,
		UniqueSources:      uniqueSources,
		UniqueDestinations: uniqueDests,
		UniqueServices:     uniqueServices,
		Limit:              limit,
		Truncated:          truncated,
		ScannedEntries:     scannedEntries,
		BufferOldestAt:     bufferOldestAt,
		Sources:            sources,
		Destinations:       destinations,
		Services:           services,
		Source:             respSource,
		CollectingSince:    collectingSince,
		Capped:             capped,
		Evicted:            evicted,
		MaxPerDirection:    maxPerDirection,
	}, nil
}

// persistedDirectionCounts builds the map[string]logs.Counted tally for one
// direction of the persisted path — top-N rows from
// repo.GetTopPolicyEndpoints plus whatever the RAM recorder hasn't flushed
// yet (docs/ref/todo/persisted-rule-endpoints-plan.md E-D6: without folding
// in pending, a rule that just had Monitor turned on would show an empty
// table for up to one flush interval). Pending counts are added onto a
// matching DB row's count, or inserted as a brand-new entry when the DB
// hasn't seen that key yet; first_seen_at always prefers whichever source
// already has data for that key (DB row wins if both exist, since it is by
// definition older).
func (s *PolicyStatsService) persistedDirectionCounts(ruleID, direction string, limit int) (map[string]logs.Counted, error) {
	rows, err := s.repo.GetTopPolicyEndpoints(ruleID, direction, limit)
	if err != nil {
		return nil, err
	}
	out := make(map[string]logs.Counted, len(rows))
	for _, row := range rows {
		out[row.Key] = logs.Counted{Count: row.Count, FirstSeen: row.FirstSeenAt, LastSeen: row.LastSeenAt}
	}

	if s.endpointRecorder == nil {
		return out, nil
	}
	pendingByDir := s.endpointRecorder.Pending(ruleID)
	if pendingByDir == nil {
		return out, nil
	}
	for key, p := range pendingByDir[direction] {
		c, exists := out[key]
		if !exists {
			out[key] = logs.Counted{Count: p.Count, FirstSeen: p.FirstSeenAt, LastSeen: p.LastSeenAt}
			continue
		}
		c.Count += p.Count
		if p.LastSeenAt > c.LastSeen {
			c.LastSeen = p.LastSeenAt
		}
		out[key] = c
	}
	return out, nil
}

// topEndpointCounts sorts m's entries by Count desc, key asc (deterministic
// tie-break, plan §3) and returns at most limit of them. Never mutates or
// aliases m.
func topEndpointCounts(m map[string]logs.Counted, limit int) []endpointCount {
	out := make([]endpointCount, 0, len(m))
	for k, c := range m {
		out = append(out, endpointCount{key: k, c: c})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].c.Count != out[j].c.Count {
			return out[i].c.Count > out[j].c.Count
		}
		return out[i].key < out[j].key
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// splitServiceKey splits a RuleAggregate.Services key ("PROTO/PORT",
// produced by RingBuffer.AggregateByRule) back into its two parts. ICMP's
// "-" port (never converted to "0") passes through untouched.
func splitServiceKey(key string) (proto, port string) {
	idx := strings.Index(key, "/")
	if idx < 0 {
		return key, ""
	}
	return key[:idx], key[idx+1:]
}

// stringSet builds a lookup set from a slice, ignoring empty entries.
func stringSet(items []string) map[string]struct{} {
	out := make(map[string]struct{}, len(items))
	for _, it := range items {
		if it == "" {
			continue
		}
		out[it] = struct{}{}
	}
	return out
}

// serviceNameReferencedByRule reports whether svcName is one of the service
// objects rule.Service actually references, tolerating the same
// trailing-suffix quirk resolveService (real_firewall.go) already tolerates:
// an entry may be the exact object name, or the object name followed by a
// space and some suffix.
func serviceNameReferencedByRule(svcName string, ruleServices []string) bool {
	if svcName == "" {
		return false
	}
	for _, entry := range ruleServices {
		if entry == svcName {
			return true
		}
		if idx := strings.Index(entry, " "); idx >= 0 && entry[:idx] == svcName {
			return true
		}
	}
	return false
}
