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

// GetRuleEndpoints answers "which IPs/services matched this rule" by
// scanning the traffic-log ring buffer once (RingBuffer.AggregateByRule,
// T-02) and resolving names (T-03/T-04) for the top N of each category. It
// never touches the kernel, never starts a goroutine, and never calls the
// repository or a lookup function more than once per request — see plan T-05
// and the "ห้าม" list there.
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

	var agg logs.RuleAggregate
	if s.ringBuffer != nil {
		agg = s.ringBuffer.AggregateByRule(rule.ID)
	}

	uniqueSources, uniqueDests, uniqueServices := len(agg.Sources), len(agg.Dests), len(agg.Services)

	srcTop := topEndpointCounts(agg.Sources, limit)
	destTop := topEndpointCounts(agg.Dests, limit)
	svcTop := topEndpointCounts(agg.Services, limit)

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

	return model.PolicyRuleEndpoints{
		RuleID:             rule.ID,
		RuleName:           rule.Name,
		Chain:              rule.Chain,
		LogEnabled:         rule.Log,
		MatchedEntries:     agg.Matched,
		UniqueSources:      uniqueSources,
		UniqueDestinations: uniqueDests,
		UniqueServices:     uniqueServices,
		Limit:              limit,
		Truncated:          truncated,
		ScannedEntries:     agg.Scanned,
		BufferOldestAt:     agg.OldestTime,
		Sources:            sources,
		Destinations:       destinations,
		Services:           services,
	}, nil
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
