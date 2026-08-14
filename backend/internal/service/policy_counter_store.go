package service

import (
	"context"
	"log"
	"sync"
	"time"

	"pigate/internal/db"
	"pigate/internal/model"
)

// PolicyCounterStore owns the persisted, opt-in "Monitor" per-rule counters
// (docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md D-5/D-6, issue
// #141). It drains the deltas TrafficStatsService's poller already computes
// (DrainRuleDeltas), keeps a RAM cache of the running totals, and persists
// them into SQLite (policy_rule_counters table) on a timer and right before
// every firewall apply. It never talks to the kernel directly.
//
// Since docs/ref/todo/persisted-rule-endpoints-plan.md (issue #141
// follow-up, E-06) it also owns the optional PolicyEndpointRecorder's flush:
// the same Monitor switch and the same flush cadence now also drain/persist
// per-rule endpoint deltas (policy_rule_endpoints table) — see
// SetEndpointRecorder.
type PolicyCounterStore struct {
	repo          *db.Repository
	trafficStats  *TrafficStatsService
	flushInterval time.Duration

	// recorder/maxEndpointsPerDirection are optional (nil/0 until
	// SetEndpointRecorder is called by main.go, additive-setter pattern —
	// E-06). maxEndpointsPerDirection <=0 falls back to
	// defaultMaxEndpointsPerDirection (Caution 14: must never be hardcoded
	// elsewhere).
	recorder                 *PolicyEndpointRecorder
	maxEndpointsPerDirection int

	mu    sync.Mutex
	cache map[string]model.MonitoredCounter
}

// NewPolicyCounterStore constructs the store. flushInterval <= 0 falls back
// to a conservative default (5 minutes) so a misconfigured caller doesn't
// disable persistence entirely — production always passes the resolved
// config.Config.MonitoredCounterFlushIntervalSeconds value.
func NewPolicyCounterStore(repo *db.Repository, trafficStats *TrafficStatsService, flushInterval time.Duration) *PolicyCounterStore {
	if flushInterval <= 0 {
		flushInterval = 5 * time.Minute
	}
	return &PolicyCounterStore{
		repo:          repo,
		trafficStats:  trafficStats,
		flushInterval: flushInterval,
		cache:         make(map[string]model.MonitoredCounter),
	}
}

// Load reads the persisted totals from DB into the RAM cache. Call once at
// startup, after construction, before Start.
func (s *PolicyCounterStore) Load() error {
	counters, err := s.repo.GetPolicyRuleCounters()
	if err != nil {
		return err
	}
	cache := make(map[string]model.MonitoredCounter, len(counters))
	for _, c := range counters {
		cache[c.RuleID] = c
	}
	s.mu.Lock()
	s.cache = cache
	s.mu.Unlock()
	return nil
}

// Reload re-runs Load (used after a config/backup import replaces the DB's
// contents out from under the running cache — docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md T-12) and, since
// docs/ref/todo/persisted-rule-endpoints-plan.md E-06 (issue #141
// follow-up), also resets the endpoint recorder's pending RAM state and
// resyncs its monitored-rule set to the freshly-imported DB — otherwise
// stale pending deltas from the previous DB could flush against rule ids
// that don't even exist in the new one (E-D8).
func (s *PolicyCounterStore) Reload() error {
	if err := s.Load(); err != nil {
		return err
	}
	if s.recorder != nil {
		s.recorder.Reset()
		if ids, err := s.repo.GetMonitoredPolicyIDs(); err != nil {
			log.Printf("[PolicyCounterStore] Reload: failed to refresh recorder monitored set: %v", err)
		} else {
			s.recorder.SetMonitoredRules(ids)
		}
	}
	return nil
}

// SetEndpointRecorder wires the RAM endpoint recorder into the store after
// construction (main.go, E-08) — additive-setter pattern matching
// SetTrafficStats/SetPolicyCounterStore elsewhere in this PR, so
// NewPolicyCounterStore's signature never changes. maxPerDirection <=0
// falls back to defaultMaxEndpointsPerDirection (Caution 14).
func (s *PolicyCounterStore) SetEndpointRecorder(r *PolicyEndpointRecorder, maxPerDirection int) {
	if maxPerDirection <= 0 {
		maxPerDirection = defaultMaxEndpointsPerDirection
	}
	s.recorder = r
	s.maxEndpointsPerDirection = maxPerDirection
}

// EndpointsEvictedFor returns the cached cumulative eviction count for one
// rule (policy_rule_counters.endpoints_evicted), for GetRuleEndpoints (E-07)
// to surface in the API response without an extra DB round trip.
func (s *PolicyCounterStore) EndpointsEvictedFor(id string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return int(s.cache[id].EndpointsEvicted)
}

// Start launches the periodic flush ticker. Ticks are skipped while
// repo.IsMockMode() is true (mock kernel counters are meaningless log-only
// values, and there's nothing worth persisting).
func (s *PolicyCounterStore) Start(ctx context.Context) {
	go s.run(ctx)
}

func (s *PolicyCounterStore) run(ctx context.Context) {
	t := time.NewTicker(s.flushInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			if s.repo.IsMockMode() {
				continue
			}
			if err := s.Flush(); err != nil {
				log.Printf("[PolicyCounterStore] periodic flush failed: %v", err)
			}
		}
	}
}

// Flush drains pending deltas from TrafficStatsService (bytes/packets) and,
// since docs/ref/todo/persisted-rule-endpoints-plan.md E-06 (issue #141
// follow-up), also from the optional PolicyEndpointRecorder (per-IP/service
// endpoint counts) — keeps only the ones belonging to a currently-monitored
// rule, folds the counter deltas into the RAM cache, and persists both to
// SQLite. Returns immediately (no DB read/write at all) if there is nothing
// non-zero to persist from either source (docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md Caution 6 / persisted-rule-
// endpoints-plan.md Caution 4). GetMonitoredPolicyIDs is read at most once
// per call and reused for both the counter and endpoint filtering passes,
// and to resync the recorder's monitored-rule set at the end (Caution 6 of
// the endpoints plan — never call it twice in one Flush).
func (s *PolicyCounterStore) Flush() error {
	var counterDeltas map[string]model.RuleCounter
	if s.trafficStats != nil {
		counterDeltas = s.trafficStats.DrainRuleDeltas()
	}
	var endpointDeltas []model.PersistedEndpoint
	if s.recorder != nil {
		endpointDeltas = s.recorder.Drain()
	}
	if len(counterDeltas) == 0 && len(endpointDeltas) == 0 {
		return nil
	}

	monitored, err := s.repo.GetMonitoredPolicyIDs()
	if err != nil {
		return err
	}

	if err := s.flushCounters(counterDeltas, monitored); err != nil {
		return err
	}
	if err := s.flushEndpoints(endpointDeltas, monitored); err != nil {
		return err
	}

	if s.recorder != nil {
		s.recorder.SetMonitoredRules(monitored)
	}

	return nil
}

// flushCounters is the original counter-delta persistence logic (unchanged
// behavior from before E-06), extracted so Flush can share the single
// GetMonitoredPolicyIDs read with flushEndpoints.
func (s *PolicyCounterStore) flushCounters(deltas map[string]model.RuleCounter, monitored map[string]bool) error {
	if len(deltas) == 0 {
		return nil
	}

	filtered := make(map[string]model.RuleCounter, len(deltas))
	for id, d := range deltas {
		if !monitored[id] {
			continue
		}
		if d.Bytes == 0 && d.Packets == 0 {
			continue
		}
		filtered[id] = d
	}
	if len(filtered) == 0 {
		return nil
	}

	if err := s.repo.AddPolicyRuleCounterDeltas(filtered); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	for id, d := range filtered {
		c := s.cache[id]
		c.RuleID = id
		c.Bytes += d.Bytes
		c.Packets += d.Packets
		c.UpdatedAt = now
		if c.StartedAt == "" {
			c.StartedAt = now
		}
		s.cache[id] = c
	}
	s.mu.Unlock()

	return nil
}

// flushEndpoints persists drained endpoint deltas (docs/ref/todo/
// persisted-rule-endpoints-plan.md E-06, issue #141 follow-up), filtered to
// currently-monitored rule ids only — this is the guard that prevents a
// delta for a rule deleted after it was captured in RAM from ever reaching
// AddPolicyEndpointDeltas and tripping its FK (Caution 6 of that plan).
// Eviction counts returned by the repository are folded into the RAM cache
// so EndpointsEvictedFor stays in sync without an extra DB read.
func (s *PolicyCounterStore) flushEndpoints(deltas []model.PersistedEndpoint, monitored map[string]bool) error {
	if len(deltas) == 0 {
		return nil
	}

	filtered := make([]model.PersistedEndpoint, 0, len(deltas))
	for _, d := range deltas {
		if !monitored[d.RuleID] {
			continue
		}
		filtered = append(filtered, d)
	}
	if len(filtered) == 0 {
		return nil
	}

	evicted, err := s.repo.AddPolicyEndpointDeltas(filtered, s.maxEndpointsPerDirection)
	if err != nil {
		return err
	}
	if len(evicted) == 0 {
		return nil
	}

	s.mu.Lock()
	for id, n := range evicted {
		c := s.cache[id]
		c.RuleID = id
		c.EndpointsEvicted += uint64(n)
		s.cache[id] = c
	}
	s.mu.Unlock()

	return nil
}

// FlushBeforeApply is called from FirewallService.SyncFirewallRules right
// before s.firewall.ApplyRules — it first forces a single synchronous
// rule-counter poll (to capture the last <=10s of movement the normal
// poller hasn't seen yet), then Flushes. Poll errors are only logged — a
// stats-collection failure must never block a firewall apply (docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md Caution 4/D-4).
func (s *PolicyCounterStore) FlushBeforeApply() error {
	if s.trafficStats != nil {
		if err := s.trafficStats.PollRuleCountersOnce(); err != nil {
			log.Printf("[PolicyCounterStore] pre-apply PollRuleCountersOnce failed (continuing with whatever was already pending): %v", err)
		}
	}
	return s.Flush()
}

// Totals returns a copy of the RAM cache — the persisted "Monitor" total per
// rule id, for PolicyStatsService to merge into GET /api/policies/stats.
func (s *PolicyCounterStore) Totals() map[string]model.MonitoredCounter {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]model.MonitoredCounter, len(s.cache))
	for id, c := range s.cache {
		out[id] = c
	}
	return out
}

// SetMonitored turns Monitor on/off for rule id. Flushes any pending delta
// first (so it isn't silently attributed to the new state — e.g. a delta
// collected while the rule was NOT monitored must never leak into the
// freshly-created counter row when turning Monitor on).
//
// Since docs/ref/todo/persisted-rule-endpoints-plan.md E-06 (issue #141
// follow-up): turning Monitor off also discards the recorder's pending
// endpoint data for this rule (ClearRule — E-D8, a delta captured just
// before the toggle must never resurrect a row the DB just deleted), and
// either way the recorder's monitored-rule set is resynced from the DB so
// Record() immediately reflects the new state without waiting for the next
// flush tick. Turning Monitor on deliberately does NOT seed/backfill
// anything into the recorder from the ring buffer (E-D6 — starts from zero).
func (s *PolicyCounterStore) SetMonitored(id string, on bool) error {
	if err := s.Flush(); err != nil {
		log.Printf("[PolicyCounterStore] pre-toggle flush failed (continuing): %v", err)
	}
	if err := s.repo.SetPolicyMonitored(id, on); err != nil {
		return err
	}

	s.mu.Lock()
	if on {
		now := time.Now().UTC().Format(time.RFC3339)
		if _, exists := s.cache[id]; !exists {
			s.cache[id] = model.MonitoredCounter{RuleID: id, StartedAt: now, UpdatedAt: now}
		}
	} else {
		delete(s.cache, id)
	}
	s.mu.Unlock()

	if s.recorder != nil {
		if !on {
			s.recorder.ClearRule(id)
		}
		if ids, err := s.repo.GetMonitoredPolicyIDs(); err != nil {
			log.Printf("[PolicyCounterStore] SetMonitored(%q,%v): failed to refresh recorder monitored set: %v", id, on, err)
		} else {
			s.recorder.SetMonitoredRules(ids)
		}
	}
	return nil
}

// ResetRule flushes any pending delta (so it can't be re-applied after the
// reset), then zeroes the persisted counter and its cache entry. Since
// docs/ref/todo/persisted-rule-endpoints-plan.md E-06 (issue #141
// follow-up), repo.ResetPolicyRuleCounter now also deletes this rule's
// policy_rule_endpoints rows in the same transaction (E-D8), and the
// recorder's pending data for this rule is cleared so it can't flow back in
// on the next flush.
func (s *PolicyCounterStore) ResetRule(id string) error {
	if err := s.Flush(); err != nil {
		log.Printf("[PolicyCounterStore] pre-reset flush failed (continuing): %v", err)
	}
	if err := s.repo.ResetPolicyRuleCounter(id); err != nil {
		return err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	s.mu.Lock()
	s.cache[id] = model.MonitoredCounter{RuleID: id, StartedAt: now, UpdatedAt: now}
	s.mu.Unlock()

	if s.recorder != nil {
		s.recorder.ClearRule(id)
	}
	return nil
}
