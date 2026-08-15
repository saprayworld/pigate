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
	// PolicyCounterStore tests exercise Flush()'s actual persistence path
	// (QA finding 1, round 1 of the persisted-rule-endpoints bugfix loop:
	// Flush() itself now skips all work when IsMockMode() is true, mirroring
	// the "no writes under -mock=true" requirement in both plans' Final
	// Acceptance) — db.NewRepository defaults IsMockMode() to true for
	// safety, so it must be explicitly turned off here for these tests to
	// still observe real writes. The dedicated mock-mode-skips-writes
	// behavior is covered by TestPolicyCounterStore_Flush_SkipsAllWritesInMockMode
	// below, which sets mock mode back to true deliberately.
	repo.SetMockMode(false, false)

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

// TestPolicyCounterStore_FlushEndpoints_OnlyMonitoredRulesPersisted covers
// E-06 of docs/ref/todo/persisted-rule-endpoints-plan.md (issue #141
// follow-up): endpoints are flushed only for monitored rules, in the same
// Flush() call as the counter delta.
func TestPolicyCounterStore_FlushEndpoints_OnlyMonitoredRulesPersisted(t *testing.T) {
	repo, ts, _ := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)
	recorder := NewPolicyEndpointRecorder(true, 1000)
	store.SetEndpointRecorder(recorder, 1000)
	recorder.SetMonitoredRules(map[string]bool{"rule-monitored": true, "rule-unmonitored": true})

	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.5", Time: "t1"})
	recorder.Record(model.FirewallLog{RuleID: "rule-unmonitored", Src: "10.0.0.6", Time: "t2"})

	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	rows, err := repo.GetTopPolicyEndpoints("rule-monitored", model.EndpointDirectionSrc, 10)
	if err != nil {
		t.Fatalf("GetTopPolicyEndpoints: %v", err)
	}
	if len(rows) != 1 || rows[0].Key != "10.0.0.5" {
		t.Fatalf("expected the monitored rule's endpoint persisted, got %+v", rows)
	}

	unmonRows, err := repo.GetTopPolicyEndpoints("rule-unmonitored", model.EndpointDirectionSrc, 10)
	if err != nil {
		t.Fatalf("GetTopPolicyEndpoints (unmonitored): %v", err)
	}
	if len(unmonRows) != 0 {
		t.Fatalf("expected no endpoint rows for an unmonitored rule, got %+v", unmonRows)
	}
}

// TestPolicyCounterStore_FlushEndpoints_NoDeltaNoWrite locks in Caution 4 of
// the endpoints plan: a Flush with nothing pending (neither counters nor
// endpoints) must not touch the DB at all.
func TestPolicyCounterStore_FlushEndpoints_NoDeltaNoWrite(t *testing.T) {
	repo, ts, _ := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)
	recorder := NewPolicyEndpointRecorder(true, 1000)
	store.SetEndpointRecorder(recorder, 1000)

	if err := store.Flush(); err != nil {
		t.Fatalf("Flush (nothing pending): %v", err)
	}
	counts, err := repo.CountPolicyEndpoints("rule-monitored")
	if err != nil {
		t.Fatalf("CountPolicyEndpoints: %v", err)
	}
	if len(counts) != 0 {
		t.Fatalf("expected no endpoint rows written, got %v", counts)
	}
}

// TestPolicyCounterStore_SetMonitored_ClearsRecorderPendingOnDisable covers
// the E-D8 semantics: turning Monitor off discards the recorder's pending
// data for that rule so it can't flow back in on the next flush.
func TestPolicyCounterStore_SetMonitored_ClearsRecorderPendingOnDisable(t *testing.T) {
	repo, ts, _ := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)
	recorder := NewPolicyEndpointRecorder(true, 1000)
	store.SetEndpointRecorder(recorder, 1000)
	recorder.SetMonitoredRules(map[string]bool{"rule-monitored": true})

	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.5", Time: "t1"})

	if err := store.SetMonitored("rule-monitored", false); err != nil {
		t.Fatalf("SetMonitored(false): %v", err)
	}

	// The pending delta must have been discarded, not silently flushed
	// during SetMonitored's pre-toggle Flush AND not persisted since the
	// rule was already about to become unmonitored.
	rows, err := repo.GetTopPolicyEndpoints("rule-monitored", model.EndpointDirectionSrc, 10)
	if err != nil {
		t.Fatalf("GetTopPolicyEndpoints: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected no endpoint rows after disabling Monitor, got %+v", rows)
	}

	// A stray Record() call after disabling must not be counted (recorder's
	// monitored set was resynced).
	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.9", Time: "t2"})
	if got := recorder.Drain(); len(got) != 0 {
		t.Fatalf("expected recorder to ignore a rule no longer monitored, got %+v", got)
	}
}

// TestPolicyCounterStore_ResetRule_ClearsRecorderPending covers resetting a
// rule's counter also clearing the recorder's pending endpoint data.
func TestPolicyCounterStore_ResetRule_ClearsRecorderPending(t *testing.T) {
	repo, ts, _ := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)
	recorder := NewPolicyEndpointRecorder(true, 1000)
	store.SetEndpointRecorder(recorder, 1000)
	recorder.SetMonitoredRules(map[string]bool{"rule-monitored": true})

	// First flush some data through so there's something to reset.
	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.5", Time: "t1"})
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	// New pending data accumulates after the flush.
	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.6", Time: "t2"})

	if err := store.ResetRule("rule-monitored"); err != nil {
		t.Fatalf("ResetRule: %v", err)
	}

	rows, err := repo.GetTopPolicyEndpoints("rule-monitored", model.EndpointDirectionSrc, 10)
	if err != nil {
		t.Fatalf("GetTopPolicyEndpoints: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("expected reset to clear all endpoint rows (including any pending flushed during ResetRule's pre-flush), got %+v", rows)
	}
}

// TestPolicyCounterStore_Reload_ResetsRecorder covers the E-D8 import-backup
// semantics: Reload() must clear the recorder's pending state and resync its
// monitored-rule set from the freshly-loaded DB.
func TestPolicyCounterStore_Reload_ResetsRecorder(t *testing.T) {
	repo, ts, _ := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)
	recorder := NewPolicyEndpointRecorder(true, 1000)
	store.SetEndpointRecorder(recorder, 1000)
	recorder.SetMonitoredRules(map[string]bool{"rule-monitored": true})

	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.5", Time: "t1"})

	if err := store.Reload(); err != nil {
		t.Fatalf("Reload: %v", err)
	}

	if got := recorder.Drain(); len(got) != 0 {
		t.Fatalf("expected Reload to have cleared pending recorder data, got %+v", got)
	}

	// The recorder's monitored set should still include rule-monitored
	// (still monitored=1 in DB after reload), so a fresh Record must work.
	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.7", Time: "t2"})
	if got := recorder.Drain(); len(got) != 1 {
		t.Fatalf("expected recorder's monitored set to be resynced after Reload, got %+v", got)
	}
}

// TestPolicyCounterStore_EndpointsEvictedFor_TracksEviction covers E-D4/E-06:
// eviction counts returned by the repository are folded into the cache.
func TestPolicyCounterStore_EndpointsEvictedFor_TracksEviction(t *testing.T) {
	repo, ts, _ := newTestPolicyCounterStoreDeps(t)
	store := NewPolicyCounterStore(repo, ts, time.Hour)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	// Admission cap (RAM) is intentionally larger than the DB cap passed to
	// SetEndpointRecorder, so all 5 keys reach the flush and the eviction
	// this test wants to observe happens at the DB layer, not the RAM layer.
	recorder := NewPolicyEndpointRecorder(true, 10)
	store.SetEndpointRecorder(recorder, 3)
	recorder.SetMonitoredRules(map[string]bool{"rule-monitored": true})

	for i := 0; i < 5; i++ {
		recorder.Record(model.FirewallLog{
			RuleID: "rule-monitored",
			Src:    "10.0.2." + string(rune('0'+i)),
			Time:   "t" + string(rune('0'+i)),
		})
	}
	if err := store.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if got := store.EndpointsEvictedFor("rule-monitored"); got != 2 {
		t.Fatalf("expected 2 evicted (5 admitted at cap=3, so 2 over cap), got %d", got)
	}
}

// TestPolicyCounterStore_Flush_SkipsAllWritesInMockMode is the regression
// test for QA finding 1 (round 1 of the persisted-rule-endpoints bugfix
// loop, docs/ref/todo/persisted-rule-endpoints-plan.md Final Acceptance
// "mock mode"): under -mock=true, Flush() must write NOTHING to either
// policy_rule_counters or policy_rule_endpoints — not just when called from
// the periodic ticker (run(), which already checked IsMockMode()), but also
// when called directly, exactly like FlushBeforeApply() does on every
// "Apply Settings" click regardless of mock/real mode. This reproduces QA's
// exact repro: a monitored+logged rule, an event recorded, then Flush()
// (standing in for FlushBeforeApply) called while mock mode is on.
func TestPolicyCounterStore_Flush_SkipsAllWritesInMockMode(t *testing.T) {
	repo, ts, acct := newTestPolicyCounterStoreDeps(t)
	// Flip back to mock mode (newTestPolicyCounterStoreDeps turns it off by
	// default so the other tests in this file can observe real writes).
	repo.SetMockMode(true, false)

	store := NewPolicyCounterStore(repo, ts, time.Hour)
	if err := store.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	recorder := NewPolicyEndpointRecorder(true, 1000)
	store.SetEndpointRecorder(recorder, 1000)
	recorder.SetMonitoredRules(map[string]bool{"rule-monitored": true})

	// Counter delta (bytes/packets).
	acct.ruleResponses = []map[string]model.RuleCounter{
		{"rule-monitored": {Bytes: 1000, Packets: 10}}, // seed
		{"rule-monitored": {Bytes: 1400, Packets: 14}}, // delta 400/4
	}
	ts.poll()
	ts.poll()

	// Endpoint delta (src/dst/svc), mirroring QA's repro exactly.
	recorder.Record(model.FirewallLog{RuleID: "rule-monitored", Src: "10.0.0.9", Dest: "8.8.4.4", Proto: "TCP", Port: "443", Time: "t1"})

	if err := store.Flush(); err != nil {
		t.Fatalf("Flush (mock mode): %v", err)
	}

	// Nothing must have reached policy_rule_counters (still the zeroed row
	// SetPolicyMonitored seeded, untouched) or policy_rule_endpoints.
	counters, err := repo.GetPolicyRuleCounters()
	if err != nil {
		t.Fatalf("GetPolicyRuleCounters: %v", err)
	}
	if len(counters) != 1 || counters[0].Bytes != 0 || counters[0].Packets != 0 {
		t.Fatalf("expected no counter delta written under mock mode, got %+v", counters)
	}
	endpointCounts, err := repo.CountPolicyEndpoints("rule-monitored")
	if err != nil {
		t.Fatalf("CountPolicyEndpoints: %v", err)
	}
	if len(endpointCounts) != 0 {
		t.Fatalf("expected no policy_rule_endpoints rows written under mock mode, got %v", endpointCounts)
	}

	// Also confirm the pending deltas are simply left untouched (not
	// silently dropped) so a later real (non-mock) Flush would still see
	// them — Flush returns immediately before ever draining anything.
	if got := recorder.Drain(); len(got) != 3 {
		t.Fatalf("expected the pending endpoint delta (src+dst+svc) to still be pending after a mock-mode Flush, got %+v", got)
	}
}
