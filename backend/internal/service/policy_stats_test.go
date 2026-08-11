package service

import (
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
)

// newPolicyStatsTestServices wires a FirewallService + TrafficStatsService +
// RingBuffer + PolicyStatsService against the same in-memory repo, mirroring
// main.go's construction order (docs/ref/todo/
// firewall-policy-rule-usage-stats-plan.md T-05).
func newPolicyStatsTestServices(t *testing.T, acct *fakeTrafficAccounting) (*db.Repository, *FirewallService, *TrafficStatsService, *logs.RingBuffer, *PolicyStatsService) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
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
	statsSvc := NewPolicyStatsService(repo, fwSvc, trafficSvc, ringBuffer)
	return repo, fwSvc, trafficSvc, ringBuffer, statsSvc
}

func mustCreatePolicy(t *testing.T, repo *db.Repository, rule model.PolicyRule) {
	t.Helper()
	if rule.Action == "" {
		rule.Action = "ACCEPT"
	}
	if rule.Source == nil {
		rule.Source = []string{"ALL"}
	}
	if rule.Destination == nil {
		rule.Destination = []string{"ALL"}
	}
	if rule.Service == nil {
		rule.Service = []string{"ALL"}
	}
	rule.Chain = model.NormalizePolicyChain(rule.Chain)
	if err := repo.CreatePolicy(rule); err != nil {
		t.Fatalf("CreatePolicy(%s) failed: %v", rule.ID, err)
	}
}

// TestPolicyStats_AvailableFalseBeforeFirstPoll covers Final acceptance:
// before the poller has ever completed a tick, Available must be false (not
// a misleading all-zero table).
func TestPolicyStats_AvailableFalseBeforeFirstPoll(t *testing.T) {
	repo, _, _, _, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true})

	got, err := statsSvc.GetPolicyRuleStats("")
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	if got.Available {
		t.Errorf("expected Available=false before any poll tick, got true")
	}
}

// TestPolicyStats_DisabledRuleOmitted covers Final acceptance: a disabled
// rule never appears in Rules (frontend renders "—" instead of 0/Unused).
func TestPolicyStats_DisabledRuleOmitted(t *testing.T) {
	repo, _, trafficSvc, _, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-1": {Bytes: 100, Packets: 1}},
			{"rule-1": {Bytes: 500, Packets: 5}},
		},
	})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "Enabled", Chain: model.PolicyChainForward, Status: true})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-2", Name: "Disabled", Chain: model.PolicyChainForward, Status: false})

	trafficSvc.poll() // seed
	trafficSvc.poll() // delta

	got, err := statsSvc.GetPolicyRuleStats("")
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	for _, r := range got.Rules {
		if r.RuleID == "rule-2" {
			t.Fatalf("disabled rule must be omitted from Rules, found: %+v", r)
		}
	}
	if len(got.Rules) != 1 || got.Rules[0].RuleID != "rule-1" {
		t.Fatalf("expected exactly rule-1 in Rules, got %+v", got.Rules)
	}
}

// TestPolicyStats_UnusedVsActive covers Final acceptance: rule with 0 delta
// is Unused; rule with traffic is not, and Percent sums correctly across
// both (Design decision 2: percent always spans every chain).
func TestPolicyStats_UnusedVsActive(t *testing.T) {
	repo, _, trafficSvc, _, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-active": {Bytes: 0, Packets: 0}, "rule-unused": {Bytes: 0, Packets: 0}},
			{"rule-active": {Bytes: 1000, Packets: 10}, "rule-unused": {Bytes: 0, Packets: 0}},
		},
	})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-active", Name: "Active", Chain: model.PolicyChainForward, Status: true})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-unused", Name: "Unused", Chain: model.PolicyChainForward, Status: true})

	trafficSvc.poll()
	trafficSvc.poll()

	got, err := statsSvc.GetPolicyRuleStats("")
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	if got.TotalBytes != 1000 {
		t.Fatalf("expected TotalBytes=1000, got %d", got.TotalBytes)
	}
	var active, unused *model.PolicyRuleStat
	for i := range got.Rules {
		switch got.Rules[i].RuleID {
		case "rule-active":
			active = &got.Rules[i]
		case "rule-unused":
			unused = &got.Rules[i]
		}
	}
	if active == nil || unused == nil {
		t.Fatalf("expected both rules present, got %+v", got.Rules)
	}
	if active.Unused {
		t.Errorf("rule-active should not be Unused")
	}
	if !unused.Unused {
		t.Errorf("rule-unused should be Unused")
	}
	if active.Percent != 100 {
		t.Errorf("expected rule-active Percent=100, got %v", active.Percent)
	}
	if unused.Percent != 0 {
		t.Errorf("expected rule-unused Percent=0, got %v", unused.Percent)
	}
}

// TestPolicyStats_ChainFilterKeepsGlobalPercent covers Final acceptance:
// ?chain= filters the returned Rules, but TotalBytes/Percent still reflect
// every chain (Design decision 2).
func TestPolicyStats_ChainFilterKeepsGlobalPercent(t *testing.T) {
	repo, _, trafficSvc, _, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-fwd": {Bytes: 0}, "rule-in": {Bytes: 0}},
			{"rule-fwd": {Bytes: 300}, "rule-in": {Bytes: 700}},
		},
	})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-fwd", Name: "Forward", Chain: model.PolicyChainForward, Status: true})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-in", Name: "Input", Chain: model.PolicyChainInput, Status: true})

	trafficSvc.poll()
	trafficSvc.poll()

	got, err := statsSvc.GetPolicyRuleStats(model.PolicyChainForward)
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	if len(got.Rules) != 1 || got.Rules[0].RuleID != "rule-fwd" {
		t.Fatalf("chain filter should keep only rule-fwd, got %+v", got.Rules)
	}
	if got.TotalBytes != 1000 {
		t.Fatalf("TotalBytes must still span every chain (300+700=1000), got %d", got.TotalBytes)
	}
	if got.Rules[0].Percent != 30 {
		t.Fatalf("expected rule-fwd Percent=30 (300/1000), got %v", got.Rules[0].Percent)
	}
}

// TestPolicyStats_LastMatchedHybridLogVsCounter covers Design decision 3: a
// Log-enabled rule prefers the ring-buffer log source; a rule without Log
// falls back to the poll-based counter source.
func TestPolicyStats_LastMatchedHybridLogVsCounter(t *testing.T) {
	repo, _, trafficSvc, ringBuffer, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-log": {Bytes: 0}, "rule-nolog": {Bytes: 0}},
			{"rule-log": {Bytes: 100}, "rule-nolog": {Bytes: 200}},
		},
	})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-log", Name: "WithLog", Chain: model.PolicyChainForward, Status: true, Log: true})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-nolog", Name: "NoLog", Chain: model.PolicyChainForward, Status: true, Log: false})

	logTime := time.Now().UTC().Format(time.RFC3339)
	ringBuffer.Add(model.FirewallLog{ID: "log-1", Time: logTime, RuleID: "rule-log", Action: "PASS"})

	trafficSvc.poll()
	trafficSvc.poll()

	got, err := statsSvc.GetPolicyRuleStats("")
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	var withLog, noLog *model.PolicyRuleStat
	for i := range got.Rules {
		switch got.Rules[i].RuleID {
		case "rule-log":
			withLog = &got.Rules[i]
		case "rule-nolog":
			noLog = &got.Rules[i]
		}
	}
	if withLog == nil || noLog == nil {
		t.Fatalf("expected both rules, got %+v", got.Rules)
	}
	if withLog.LastMatchedSource != "log" || withLog.LastMatchedAt != logTime {
		t.Errorf("expected rule-log to resolve from the ring buffer log (%q), got source=%q at=%q", logTime, withLog.LastMatchedSource, withLog.LastMatchedAt)
	}
	if noLog.LastMatchedSource != "counter" || noLog.LastMatchedAt == "" {
		t.Errorf("expected rule-nolog to fall back to the poll counter source, got source=%q at=%q", noLog.LastMatchedSource, noLog.LastMatchedAt)
	}
}

// TestPolicyStats_ClearedRingBufferFallsBackToCounter covers Final
// acceptance: clearing the ring buffer must not crash, and a Log-enabled
// rule's lastMatchedAt gracefully falls back to the poll-based counter
// source (or empty) instead of erroring.
func TestPolicyStats_ClearedRingBufferFallsBackToCounter(t *testing.T) {
	repo, _, trafficSvc, ringBuffer, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-log": {Bytes: 0}},
			{"rule-log": {Bytes: 100}},
		},
	})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-log", Name: "WithLog", Chain: model.PolicyChainForward, Status: true, Log: true})
	ringBuffer.Add(model.FirewallLog{ID: "log-1", Time: time.Now().UTC().Format(time.RFC3339), RuleID: "rule-log", Action: "PASS"})
	ringBuffer.Clear()

	trafficSvc.poll()
	trafficSvc.poll()

	got, err := statsSvc.GetPolicyRuleStats("")
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	if len(got.Rules) != 1 {
		t.Fatalf("expected 1 rule, got %+v", got.Rules)
	}
	if got.Rules[0].LastMatchedSource != "counter" {
		t.Errorf("expected fallback to counter source after ring buffer clear, got %q", got.Rules[0].LastMatchedSource)
	}
}

// TestPolicyStats_CountersSinceFromSuccessfulApply covers Final acceptance:
// CountersSince reflects FirewallService.LastAppliedAt(), which only moves on
// a SUCCESSFUL SyncFirewallRules.
func TestPolicyStats_CountersSinceFromSuccessfulApply(t *testing.T) {
	repo, fwSvc, _, _, statsSvc := newPolicyStatsTestServices(t, &fakeTrafficAccounting{})
	mustCreatePolicy(t, repo, model.PolicyRule{ID: "rule-1", Name: "R1", Chain: model.PolicyChainForward, Status: true})

	got, err := statsSvc.GetPolicyRuleStats("")
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	if got.CountersSince != "" {
		t.Fatalf("expected empty CountersSince before any apply, got %q", got.CountersSince)
	}

	if err := fwSvc.SyncFirewallRules(); err != nil {
		t.Fatalf("SyncFirewallRules: %v", err)
	}

	got, err = statsSvc.GetPolicyRuleStats("")
	if err != nil {
		t.Fatalf("GetPolicyRuleStats: %v", err)
	}
	if got.CountersSince == "" {
		t.Errorf("expected non-empty CountersSince after a successful apply")
	}
}
