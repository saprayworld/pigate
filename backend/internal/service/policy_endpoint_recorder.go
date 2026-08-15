package service

import (
	"sync"

	"pigate/internal/model"
)

// defaultMaxEndpointsPerDirection mirrors config.Defaults().
// MonitoredEndpointsMaxPerRule — defense-in-depth fallback for direct
// callers (tests, future call sites) passing a <=0 value, same convention as
// statistics.go's defaultMaxTrackedDenySources (docs/ref/todo/
// persisted-rule-endpoints-plan.md Caution 14, issue #141 follow-up). The
// authoritative range validation for the config-file-sourced production
// value lives in config.Resolve, not here.
const defaultMaxEndpointsPerDirection = 1000

// pendingEndpoint is one (rule, direction, key)'s in-RAM accumulator between
// two PolicyCounterStore.Flush calls.
type pendingEndpoint struct {
	count     int
	firstSeen string
	lastSeen  string
}

// rulePending holds one monitored rule's three direction maps.
type rulePending struct {
	src map[string]*pendingEndpoint
	dst map[string]*pendingEndpoint
	svc map[string]*pendingEndpoint
}

// PolicyEndpointRecorder is the NFLOG-watcher hook for persisted rule
// endpoints (docs/ref/todo/persisted-rule-endpoints-plan.md E-D1/E-D5, issue
// #141 follow-up) — a RAM-only, O(1)-per-event accumulator whose only
// consumer is PolicyCounterStore.Flush (Drain). It is a sibling hook to
// StatisticsService.RecordFirewallLog (statistics.go lines 161-200), running
// on the exact same NFLOG read loop closure (cmd/pigate/main.go
// stampAndPush), so it is bound by the identical constraints:
//
//	Record() MUST be O(1), MUST NOT block, MUST NOT perform I/O or query the
//	database, and MUST NOT panic. If it does, packet logging for the entire
//	system stalls (Caution 1 of the plan above).
//
// The set of "which rule ids are currently monitored" is refreshed only by
// SetMonitoredRules (called by PolicyCounterStore on toggle/flush) — Record
// NEVER calls into the repository to check this, by design (E-D1).
type PolicyEndpointRecorder struct {
	mu sync.Mutex

	enabled         bool
	maxPerDirection int

	monitored map[string]bool
	pending   map[string]*rulePending // ruleID -> pending accumulator
}

// NewPolicyEndpointRecorder constructs a recorder. maxPerDirection <= 0 falls
// back to defaultMaxEndpointsPerDirection (defense-in-depth — see that
// const's doc comment; Caution 14).
func NewPolicyEndpointRecorder(enabled bool, maxPerDirection int) *PolicyEndpointRecorder {
	if maxPerDirection <= 0 {
		maxPerDirection = defaultMaxEndpointsPerDirection
	}
	return &PolicyEndpointRecorder{
		enabled:         enabled,
		maxPerDirection: maxPerDirection,
		monitored:       make(map[string]bool),
		pending:         make(map[string]*rulePending),
	}
}

// SetMonitoredRules replaces the whole monitored-rule-id set under the same
// mutex Record uses, and drops any pending accumulator for a rule that fell
// out of the new set (it can no longer be flushed — PolicyCounterStore.Flush
// already filters by the same set, so keeping it around would just leak
// RAM). Called by PolicyCounterStore after it reads GetMonitoredPolicyIDs
// (toggle and each Flush) — never called from Record itself.
func (r *PolicyEndpointRecorder) SetMonitoredRules(ids map[string]bool) {
	next := make(map[string]bool, len(ids))
	for id, on := range ids {
		if on {
			next[id] = true
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.monitored = next
	for ruleID := range r.pending {
		if !r.monitored[ruleID] {
			delete(r.pending, ruleID)
		}
	}
}

// Record is the hook called from main.go's stampAndPush for every NFLOG
// entry. See the type doc comment above for the hard performance/safety
// constraints this method must uphold.
func (r *PolicyEndpointRecorder) Record(entry model.FirewallLog) {
	if !r.enabled || entry.RuleID == "" {
		return
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if !r.monitored[entry.RuleID] {
		return
	}

	rp := r.pending[entry.RuleID]
	if rp == nil {
		rp = &rulePending{
			src: make(map[string]*pendingEndpoint),
			dst: make(map[string]*pendingEndpoint),
			svc: make(map[string]*pendingEndpoint),
		}
		r.pending[entry.RuleID] = rp
	}

	// Skip rules mirror logs.RingBuffer.AggregateByRule exactly (Caution 8 of
	// the plan) so the two data sources produce comparable numbers.
	if entry.Src != "" && entry.Src != "-" {
		admitEndpoint(rp.src, entry.Src, entry.Time, r.maxPerDirection)
	}
	if entry.Dest != "" && entry.Dest != "-" {
		admitEndpoint(rp.dst, entry.Dest, entry.Time, r.maxPerDirection)
	}
	if entry.Proto != "" {
		svcKey := entry.Proto + "/" + entry.Port
		admitEndpoint(rp.svc, svcKey, entry.Time, r.maxPerDirection)
	}
}

// admitEndpoint applies the RAM-side admission cap (Caution 14/E-D4 "ด่านที่
// ศูนย์"): a key that already exists always accepts the new event (count++,
// lastSeen updated); a brand-new key is only admitted while the map is under
// maxPerDirection. Never allocates beyond that cap, keeping Record O(1) with
// a fixed worst-case RAM ceiling between two flushes.
func admitEndpoint(m map[string]*pendingEndpoint, key, ts string, maxPerDirection int) {
	if p, exists := m[key]; exists {
		p.count++
		p.lastSeen = ts
		return
	}
	if len(m) >= maxPerDirection {
		return
	}
	m[key] = &pendingEndpoint{count: 1, firstSeen: ts, lastSeen: ts}
}

// Drain returns every pending endpoint across every monitored rule as flat
// model.PersistedEndpoint deltas, and clears the pending accumulators. The
// only intended caller is PolicyCounterStore.Flush (single consumer, per the
// plan). Safe to call concurrently with Record (guarded by the same mutex).
func (r *PolicyEndpointRecorder) Drain() []model.PersistedEndpoint {
	r.mu.Lock()
	defer r.mu.Unlock()

	var out []model.PersistedEndpoint
	for ruleID, rp := range r.pending {
		out = appendPendingDirection(out, ruleID, model.EndpointDirectionSrc, rp.src)
		out = appendPendingDirection(out, ruleID, model.EndpointDirectionDst, rp.dst)
		out = appendPendingDirection(out, ruleID, model.EndpointDirectionSvc, rp.svc)
	}
	r.pending = make(map[string]*rulePending)
	return out
}

func appendPendingDirection(out []model.PersistedEndpoint, ruleID, direction string, m map[string]*pendingEndpoint) []model.PersistedEndpoint {
	for key, p := range m {
		out = append(out, model.PersistedEndpoint{
			RuleID:      ruleID,
			Direction:   direction,
			Key:         key,
			Count:       p.count,
			FirstSeenAt: p.firstSeen,
			LastSeenAt:  p.lastSeen,
		})
	}
	return out
}

// Pending returns a defensive copy of one rule's not-yet-flushed data, keyed
// by direction then endpoint key, for GetRuleEndpoints (E-07) to fold into
// the response so a just-enabled Monitor rule shows data immediately instead
// of waiting for the next flush tick (E-D6). Never clears anything.
func (r *PolicyEndpointRecorder) Pending(ruleID string) map[string]map[string]model.PersistedEndpoint {
	r.mu.Lock()
	defer r.mu.Unlock()

	rp := r.pending[ruleID]
	if rp == nil {
		return nil
	}
	out := map[string]map[string]model.PersistedEndpoint{
		model.EndpointDirectionSrc: copyPendingDirection(ruleID, model.EndpointDirectionSrc, rp.src),
		model.EndpointDirectionDst: copyPendingDirection(ruleID, model.EndpointDirectionDst, rp.dst),
		model.EndpointDirectionSvc: copyPendingDirection(ruleID, model.EndpointDirectionSvc, rp.svc),
	}
	return out
}

func copyPendingDirection(ruleID, direction string, m map[string]*pendingEndpoint) map[string]model.PersistedEndpoint {
	out := make(map[string]model.PersistedEndpoint, len(m))
	for key, p := range m {
		out[key] = model.PersistedEndpoint{
			RuleID:      ruleID,
			Direction:   direction,
			Key:         key,
			Count:       p.count,
			FirstSeenAt: p.firstSeen,
			LastSeenAt:  p.lastSeen,
		}
	}
	return out
}

// ClearRule discards one rule's pending data without flushing it — used when
// Monitor is turned off or the counter is reset (E-D8: neither operation may
// let stale pending data flow back in on the next flush).
func (r *PolicyEndpointRecorder) ClearRule(ruleID string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, ruleID)
}

// Reset discards all pending data for every rule — used after an import
// backup restores a different DB (E-D8), where the old pending deltas would
// otherwise flush against a database that may not even contain those rule
// ids anymore.
func (r *PolicyEndpointRecorder) Reset() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.pending = make(map[string]*rulePending)
}
