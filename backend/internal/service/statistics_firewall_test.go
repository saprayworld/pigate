package service

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
)

// newFirewallStatsTestServices wires a FirewallService + TrafficStatsService
// + RingBuffer + StatisticsService against the same repo, mirroring main.go's
// construction order (docs/ref/todo/statistics-firewall-page-plan.md T-07,
// and newPolicyStatsTestServices in policy_stats_test.go). Uses a file-backed
// temp DB (not ":memory:") — repo.GetPolicies() runs child queries against
// r.db while the parent rows cursor is still open (see repository.go), and a
// ":memory:" DSN gives each new connection-pool connection its OWN separate
// database, so a second connection opened mid-scan would see an empty,
// unmigrated database ("no such table"); TestGetFirewallStatistics_NoRaceWithPoll
// below calls GetFirewallStatistics (and therefore GetPolicies) concurrently,
// so this test helper needs the real multi-connection-safe file-backed
// behavior other concurrent-DB tests in this package already use (e.g.
// backup_test.go/dns_blocklist_test.go's t.TempDir() pattern).
func newFirewallStatsTestServices(t *testing.T, acct kernel.TrafficAccountingManager) (*db.Repository, *FirewallService, *TrafficStatsService, *logs.RingBuffer, *StatisticsService) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pigate-firewall-stats-test.db")
	sqliteDB, err := db.InitDB(dbPath)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })

	repo := db.NewRepository(sqliteDB)
	mockNet := kernel.NewMockNetwork()
	ifaceSvc := NewInterfaceService(repo, mockNet)
	fwSvc := NewFirewallService(repo, kernel.NewMockFirewall(true), ifaceSvc)
	trafficSvc := NewTrafficStatsService(acct, repo, &fakeDhcpForTraffic{}, kernel.NewMockSystemStats(), 0, 0, 0)
	ringBuffer := logs.NewRingBuffer(50)
	statsSvc := NewStatisticsService(trafficSvc, repo, &fakeDhcpForTraffic{}, defaultMaxTrackedDNSPairs, defaultMaxTrackedDNSClients, defaultMaxTrackedDenySources, defaultMaxTrackedDenyPorts)
	statsSvc.SetLogBuffer(ringBuffer)
	statsSvc.SetFirewallService(fwSvc)
	return repo, fwSvc, trafficSvc, ringBuffer, statsSvc
}

// TestGetFirewallRuleBreakdown_SplitsAcceptDrop covers T-07 case 1: accept vs
// drop bytes/packets must be classified correctly per bucket, and
// sum(trend[].acceptedBytes) across the whole window must equal the rule
// total for the ACCEPT-action rule (and likewise for DROP).
func TestGetFirewallRuleBreakdown_SplitsAcceptDrop(t *testing.T) {
	acct := &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"accept-1": {Bytes: 100, Packets: 1}, "drop-1": {Bytes: 50, Packets: 2}},
			{"accept-1": {Bytes: 300, Packets: 3}, "drop-1": {Bytes: 90, Packets: 4}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // first real delta

	actionByRule := map[string]string{"accept-1": "ACCEPT", "drop-1": "DROP"}
	breakdown := s.GetFirewallRuleBreakdown("1h", actionByRule)

	if breakdown.RuleTotals["accept-1"].Bytes != 200 {
		t.Fatalf("expected accept-1 total bytes 200, got %+v", breakdown.RuleTotals["accept-1"])
	}
	if breakdown.RuleTotals["drop-1"].Bytes != 40 {
		t.Fatalf("expected drop-1 total bytes 40, got %+v", breakdown.RuleTotals["drop-1"])
	}

	var sumAccept, sumDrop uint64
	for _, p := range breakdown.Trend {
		sumAccept += p.AcceptedBytes
		sumDrop += p.BlockedBytes
	}
	if sumAccept != 200 {
		t.Fatalf("expected sum(trend accepted bytes) == 200, got %d", sumAccept)
	}
	if sumDrop != 40 {
		t.Fatalf("expected sum(trend blocked bytes) == 40, got %d", sumDrop)
	}
	if len(breakdown.Stale) != 0 {
		t.Fatalf("expected no stale rule ids, got %+v", breakdown.Stale)
	}
}

// TestGetFirewallRuleBreakdown_DeletedRuleMarkedStale covers T-07 case 2: a
// rule id present in the bucket ring but absent from actionByRule (deleted
// after being counted) must not panic and must be reported via Stale, while
// still contributing to RuleTotals.
func TestGetFirewallRuleBreakdown_DeletedRuleMarkedStale(t *testing.T) {
	acct := &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"ghost-rule": {Bytes: 10, Packets: 1}},
			{"ghost-rule": {Bytes: 60, Packets: 3}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll()
	s.poll()

	breakdown := s.GetFirewallRuleBreakdown("1h", map[string]string{})
	if !breakdown.Stale["ghost-rule"] {
		t.Fatalf("expected ghost-rule to be marked stale, got %+v", breakdown.Stale)
	}
	if breakdown.RuleTotals["ghost-rule"].Bytes != 50 {
		t.Fatalf("expected ghost-rule total bytes 50 (still counted), got %+v", breakdown.RuleTotals["ghost-rule"])
	}
	for _, p := range breakdown.Trend {
		if p.AcceptedBytes != 0 || p.BlockedBytes != 0 {
			t.Fatalf("expected a stale rule's bytes excluded from Trend, got %+v", p)
		}
	}
}

// TestDenySeries_MatchesWindowBucketCount covers T-07 case 3: denySeries must
// return exactly statsWindowBucketCount(window) points for every supported
// window, and the sum of its Events must equal the window's total DROP count.
func TestDenySeries_MatchesWindowBucketCount(t *testing.T) {
	_, _, _, _, s := newFirewallStatsTestServices(t, &fakeTrafficAccounting{})

	for i := 0; i < 5; i++ {
		s.RecordFirewallLog(model.FirewallLog{Action: "DROP", Src: "203.0.113.9", Proto: "TCP", Port: "22"})
	}

	for window, n := range statsWindowBuckets {
		series, total, _ := s.denySeries(window)
		if len(series) != n {
			t.Fatalf("window %s: expected %d points, got %d", window, n, len(series))
		}
		if total != 5 {
			t.Fatalf("window %s: expected total 5 events, got %d", window, total)
		}
		var sum uint64
		for _, p := range series {
			sum += p.Events
		}
		if sum != total {
			t.Fatalf("window %s: sum(series[].events)=%d != total=%d", window, sum, total)
		}
	}
}

// TestGetFirewallStatistics_ComposesResponse covers T-07 case 5 (shape): a
// happy-path call with one ACCEPT rule, one DROP rule, and some NFLOG DROP
// events must produce a response with consistent accepted/blocked totals,
// chain rows, rule rows, and blocked-source rows.
func TestGetFirewallStatistics_ComposesResponse(t *testing.T) {
	acct := &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			// Poll 1 (the very first poll ever) seeds accept-1's baseline
			// only — no delta yet (plan Caution 4's "seed, don't count").
			{"accept-1": {Bytes: 100, Packets: 1}},
			// Poll 2 is the first time drop-1 is ever observed, so IT seeds
			// (no delta yet) while accept-1 (already seeded) now produces its
			// first real delta.
			{"accept-1": {Bytes: 500, Packets: 5}, "drop-1": {Bytes: 80, Packets: 4}},
			// Poll 3 is drop-1's first real delta.
			{"accept-1": {Bytes: 500, Packets: 5}, "drop-1": {Bytes: 240, Packets: 12}},
		},
	}
	repo, _, trafficSvc, _, s := newFirewallStatsTestServices(t, acct)
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "accept-1", Name: "Allow Web", Chain: model.PolicyChainForward, Action: "ACCEPT", Status: true})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "drop-1", Name: "Block Telnet", Chain: model.PolicyChainInput, Action: "DROP", Status: true})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "disabled-1", Name: "Disabled Rule", Chain: model.PolicyChainForward, Action: "ACCEPT", Status: false})

	trafficSvc.poll()
	trafficSvc.poll()
	trafficSvc.poll()

	s.RecordFirewallLog(model.FirewallLog{Action: "DROP", Src: "203.0.113.9", Proto: "TCP", Port: "23"})
	s.RecordFirewallLog(model.FirewallLog{Action: "DROP", Src: "203.0.113.9", Proto: "TCP", Port: "23"})

	stats, err := s.GetFirewallStatistics("1h", 100)
	if err != nil {
		t.Fatalf("GetFirewallStatistics: %v", err)
	}
	if !stats.Available {
		t.Fatalf("expected Available=true after a poll tick")
	}
	if stats.AcceptedBytes != 400 {
		t.Fatalf("expected AcceptedBytes=400 (500-100 delta), got %d", stats.AcceptedBytes)
	}
	if stats.BlockedBytes != 160 {
		t.Fatalf("expected BlockedBytes=160 (240-80 delta), got %d", stats.BlockedBytes)
	}
	if stats.BlockedEvents != 2 {
		t.Fatalf("expected BlockedEvents=2 (NFLOG, independent of BlockedBytes), got %d", stats.BlockedEvents)
	}
	if stats.RulesEnabled != 2 {
		t.Fatalf("expected RulesEnabled=2 (disabled-1 excluded), got %d", stats.RulesEnabled)
	}
	if len(stats.Chains) != 2 {
		t.Fatalf("expected 2 chain rows (forward, input), got %+v", stats.Chains)
	}
	if len(stats.Rules) != 2 {
		t.Fatalf("expected 2 rule rows (disabled-1 excluded), got %+v", stats.Rules)
	}
	if len(stats.BlockedSources) != 1 || stats.BlockedSources[0].IP != "203.0.113.9" {
		t.Fatalf("expected 1 blocked source 203.0.113.9, got %+v", stats.BlockedSources)
	}
	if stats.Limit != 100 {
		t.Fatalf("expected Limit echoed back as 100, got %d", stats.Limit)
	}
}

// TestGetFirewallStatistics_NoRaceWithPoll drives poll() concurrently with
// GetFirewallStatistics() across every supported window (mirrors
// TestTrafficStats_GetTrafficDetailNoRaceWithPoll) — run with `go test
// -race`.
func TestGetFirewallStatistics_NoRaceWithPoll(t *testing.T) {
	repo, _, trafficSvc, _, s := newFirewallStatsTestServices(t, &raceFakeAcct{})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "race-rule", Name: "Race Rule", Chain: model.PolicyChainForward, Action: "ACCEPT", Status: true})

	const iterations = 150
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			trafficSvc.poll()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			for w := range statsWindowBuckets {
				if _, err := s.GetFirewallStatistics(w, 100); err != nil {
					t.Errorf("GetFirewallStatistics(%s): %v", w, err)
				}
			}
			s.RecordFirewallLog(model.FirewallLog{Action: "DROP", Src: fmt.Sprintf("10.0.0.%d", i%255), Proto: "TCP", Port: "22"})
		}
	}()
	wg.Wait()
}
