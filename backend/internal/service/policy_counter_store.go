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
type PolicyCounterStore struct {
	repo          *db.Repository
	trafficStats  *TrafficStatsService
	flushInterval time.Duration

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

// Reload is an alias for Load, used after a config/backup import replaces
// the DB's contents out from under the running cache (docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md T-12).
func (s *PolicyCounterStore) Reload() error {
	return s.Load()
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

// Flush drains pending deltas from TrafficStatsService, keeps only the ones
// belonging to a currently-monitored rule, folds them into the RAM cache,
// and persists the delta to SQLite in one transaction. Returns immediately
// (no DB write) if there is nothing non-zero to persist (docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md Caution 6).
func (s *PolicyCounterStore) Flush() error {
	if s.trafficStats == nil {
		return nil
	}
	deltas := s.trafficStats.DrainRuleDeltas()
	if len(deltas) == 0 {
		return nil
	}

	monitored, err := s.repo.GetMonitoredPolicyIDs()
	if err != nil {
		return err
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
func (s *PolicyCounterStore) SetMonitored(id string, on bool) error {
	if err := s.Flush(); err != nil {
		log.Printf("[PolicyCounterStore] pre-toggle flush failed (continuing): %v", err)
	}
	if err := s.repo.SetPolicyMonitored(id, on); err != nil {
		return err
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if on {
		now := time.Now().UTC().Format(time.RFC3339)
		if _, exists := s.cache[id]; !exists {
			s.cache[id] = model.MonitoredCounter{RuleID: id, StartedAt: now, UpdatedAt: now}
		}
	} else {
		delete(s.cache, id)
	}
	return nil
}

// ResetRule flushes any pending delta (so it can't be re-applied after the
// reset), then zeroes the persisted counter and its cache entry.
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
	return nil
}
