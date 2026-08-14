package service

import (
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// newTestPolicyCounterStoreDeps builds a Repository + one monitored + one
// unmonitored PolicyRule, and a TrafficStatsService wired to a scripted
// fakeTrafficAccounting, for PolicyCounterStore tests.
func newTestPolicyCounterStoreDeps(t *testing.T) (*db.Repository, *TrafficStatsService, *fakeTrafficAccounting) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })
	repo := db.NewRepository(sqliteDB)

	addr := model.AddressObject{ID: "addr-pcs-src", Name: "PCS_SRC", Type: "subnet", Value: "10.0.0.0/24"}
	dst := model.AddressObject{ID: "addr-pcs-dst", Name: "PCS_DST", Type: "subnet", Value: "8.8.8.8/32"}
	svc := model.ServiceObject{ID: "svc-pcs", Name: "PCS_SVC", Protocol: "UDP", Port: "53", Type: "custom"}
	if err := repo.CreateAddress(addr); err != nil {
		t.Fatalf("CreateAddress: %v", err)
	}
	if err := repo.CreateAddress(dst); err != nil {
		t.Fatalf("CreateAddress: %v", err)
	}
	if err := repo.CreateService(svc); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	monitored := model.PolicyRule{
		ID: "rule-monitored", Name: "Monitored",
		Source: []string{"PCS_SRC"}, Destination: []string{"PCS_DST"}, Service: []string{"PCS_SVC"},
		Action: "ACCEPT", Status: true,
	}
	unmonitored := model.PolicyRule{
		ID: "rule-unmonitored", Name: "Unmonitored",
		Source: []string{"PCS_SRC"}, Destination: []string{"PCS_DST"}, Service: []string{"PCS_SVC"},
		Action: "ACCEPT", Status: true,
	}
	if err := repo.CreatePolicy(monitored); err != nil {
		t.Fatalf("CreatePolicy(monitored): %v", err)
	}
	if err := repo.CreatePolicy(unmonitored); err != nil {
		t.Fatalf("CreatePolicy(unmonitored): %v", err)
	}
	if err := repo.SetPolicyMonitored("rule-monitored", true); err != nil {
		t.Fatalf("SetPolicyMonitored: %v", err)
	}

	acct := &fakeTrafficAccounting{}
	ts := NewTrafficStatsService(acct, repo, &fakeDhcpForTraffic{}, kernel.NewMockSystemStats(), 0, 0, 0)
	return repo, ts, acct
}

// TestPolicyCounterStore_FlushAccumulatesAcrossApplies simulates two
// ApplyRules cycles (EndApply resets the traffic-stats baseline in between)
// and asserts the persisted total in PolicyCounterStore keeps accumulating
// instead of resetting.
func TestPolicyCounterStore_FlushAccumulatesAcrossApplies(t *testing.T) {
	repo, ts, acct := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// --- Apply cycle 1 ---
	acct.ruleResponses = []map[string]model.RuleCounter{
		{"rule-monitored": {Bytes: 1000, Packets: 10}}, // seed
		{"rule-monitored": {Bytes: 1400, Packets: 14}}, // delta 400/4
	}
	ts.poll()
	ts.poll()
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush (cycle 1): %v", err)
	}
	ts.EndApply() // simulates FirewallService calling EndApply after ApplyRules succeeds

	totals := store.Totals()
	if totals["rule-monitored"].Bytes != 400 || totals["rule-monitored"].Packets != 4 {
		t.Fatalf("expected 400/4 after cycle 1, got %+v", totals["rule-monitored"])
	}

	// --- Apply cycle 2: kernel counters restart at 0 after the flush ---
	acct.ruleResponses = append(acct.ruleResponses, map[string]model.RuleCounter{"rule-monitored": {Bytes: 250, Packets: 2}})
	ts.poll()
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush (cycle 2): %v", err)
	}

	totals = store.Totals()
	if totals["rule-monitored"].Bytes != 650 || totals["rule-monitored"].Packets != 6 {
		t.Fatalf("expected accumulated 650/6 across two apply cycles, got %+v", totals["rule-monitored"])
	}

	// Persisted DB value must match the RAM cache.
	dbCounters, err := repo.GetPolicyRuleCounters()
	if err != nil {
		t.Fatalf("GetPolicyRuleCounters: %v", err)
	}
	if len(dbCounters) != 1 || dbCounters[0].Bytes != 650 || dbCounters[0].Packets != 6 {
		t.Fatalf("expected DB to hold 650/6, got %+v", dbCounters)
	}
}

// TestPolicyCounterStore_UnmonitoredRuleNeverWritten asserts a delta for a
// rule that isn't monitored is dropped, not persisted.
func TestPolicyCounterStore_UnmonitoredRuleNeverWritten(t *testing.T) {
	repo, ts, acct := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)

	acct.ruleResponses = []map[string]model.RuleCounter{
		{"rule-unmonitored": {Bytes: 1000, Packets: 10}}, // seed
		{"rule-unmonitored": {Bytes: 1500, Packets: 15}}, // delta 500/5
	}
	ts.poll()
	ts.poll()
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	totals := store.Totals()
	if _, ok := totals["rule-unmonitored"]; ok {
		t.Fatalf("expected no entry for an unmonitored rule, got %+v", totals)
	}
	// The fixture's "rule-monitored" is monitored, so it always has its own
	// zeroed row — assert specifically that rule-unmonitored got none.
	dbCounters, err := repo.GetPolicyRuleCounters()
	if err != nil {
		t.Fatalf("GetPolicyRuleCounters: %v", err)
	}
	for _, c := range dbCounters {
		if c.RuleID == "rule-unmonitored" {
			t.Fatalf("expected no DB row for an unmonitored rule, got %+v", c)
		}
	}
}

// TestPolicyCounterStore_ZeroDeltaDoesNotWriteDB asserts a Flush with only
// zero-value deltas never calls AddPolicyRuleCounterDeltas's DB write path
// (Caution 6) — verified indirectly via updated_at staying unchanged.
func TestPolicyCounterStore_ZeroDeltaDoesNotWriteDB(t *testing.T) {
	repo, ts, acct := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)

	// Seed once with a real delta so a row exists with a known updated_at.
	acct.ruleResponses = []map[string]model.RuleCounter{
		{"rule-monitored": {Bytes: 100, Packets: 1}},
		{"rule-monitored": {Bytes: 200, Packets: 2}},
	}
	ts.poll()
	ts.poll()
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	before, _ := repo.GetPolicyRuleCounters()

	// No new poll -> DrainRuleDeltas returns empty -> Flush must no-op.
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush (no-op): %v", err)
	}
	after, _ := repo.GetPolicyRuleCounters()
	if before[0].UpdatedAt != after[0].UpdatedAt {
		t.Fatalf("expected no-op Flush to leave updated_at unchanged: before=%q after=%q", before[0].UpdatedAt, after[0].UpdatedAt)
	}
}

// TestPolicyCounterStore_SetMonitoredOnOff exercises the toggle lifecycle:
// enabling creates a zeroed row, disabling deletes it and clears the cache.
func TestPolicyCounterStore_SetMonitoredOnOff(t *testing.T) {
	repo, ts, _ := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)

	if err := store.SetMonitored("rule-unmonitored", true); err != nil {
		t.Fatalf("SetMonitored(true): %v", err)
	}
	totals := store.Totals()
	if c, ok := totals["rule-unmonitored"]; !ok || c.Bytes != 0 {
		t.Fatalf("expected a zeroed cache entry after enabling, got %+v (ok=%v)", c, ok)
	}
	monitoredIDs, _ := repo.GetMonitoredPolicyIDs()
	if !monitoredIDs["rule-unmonitored"] {
		t.Fatalf("expected rule-unmonitored to be persisted as monitored")
	}

	if err := store.SetMonitored("rule-unmonitored", false); err != nil {
		t.Fatalf("SetMonitored(false): %v", err)
	}
	totals = store.Totals()
	if _, ok := totals["rule-unmonitored"]; ok {
		t.Fatalf("expected cache entry removed after disabling, got %+v", totals)
	}
	dbCounters, _ := repo.GetPolicyRuleCounters()
	for _, c := range dbCounters {
		if c.RuleID == "rule-unmonitored" {
			t.Fatalf("expected no DB row for rule-unmonitored after disabling, got %+v", c)
		}
	}
}

// TestPolicyCounterStore_ResetRule zeroes an accumulated total.
func TestPolicyCounterStore_ResetRule(t *testing.T) {
	repo, ts, acct := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)

	acct.ruleResponses = []map[string]model.RuleCounter{
		{"rule-monitored": {Bytes: 100, Packets: 1}},
		{"rule-monitored": {Bytes: 300, Packets: 3}},
	}
	ts.poll()
	ts.poll()
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if store.Totals()["rule-monitored"].Bytes != 200 {
		t.Fatalf("expected 200 before reset, got %+v", store.Totals()["rule-monitored"])
	}

	if err := store.ResetRule("rule-monitored"); err != nil {
		t.Fatalf("ResetRule: %v", err)
	}
	got := store.Totals()["rule-monitored"]
	if got.Bytes != 0 || got.Packets != 0 {
		t.Fatalf("expected zeroed counter after reset, got %+v", got)
	}
}

// TestPolicyCounterStore_LoadReadsPersistedState asserts Load() (used at
// startup, and via Reload() after an import) populates the cache from DB.
func TestPolicyCounterStore_LoadReadsPersistedState(t *testing.T) {
	repo, ts, acct := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)

	acct.ruleResponses = []map[string]model.RuleCounter{
		{"rule-monitored": {Bytes: 10, Packets: 1}},
		{"rule-monitored": {Bytes: 60, Packets: 6}},
	}
	ts.poll()
	ts.poll()
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// Fresh store instance, simulating a process restart.
	fresh := NewPolicyCounterStore(repo, ts, time.Hour)
	if err := fresh.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	got := fresh.Totals()["rule-monitored"]
	if got.Bytes != 50 || got.Packets != 5 {
		t.Fatalf("expected Load to restore 50/5, got %+v", got)
	}
}
