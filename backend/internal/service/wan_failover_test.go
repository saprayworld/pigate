package service

import (
	"bytes"
	"context"
	"log"
	"strings"
	"sync"
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// --- decideActiveUplink (pure) ---------------------------------------------

func TestDecideActiveUplink_DisabledNeverDecides(t *testing.T) {
	in := wanFailoverDecideInput{
		Now:      time.Now(),
		Uplinks:  []model.WanUplink{{ID: "u1", Priority: 1, Status: true}},
		States:   map[string]model.WanUplinkState{"u1": {State: model.WanStateUp}},
		Settings: model.WanFailoverSettings{Enabled: false},
		Current:  "u9",
	}
	target, _, changed := decideActiveUplink(in)
	if changed || target != "u9" {
		t.Fatalf("expected Enabled=false to never decide anything, got target=%q changed=%v", target, changed)
	}
}

func TestDecideActiveUplink_ManualModeForcesTarget(t *testing.T) {
	in := wanFailoverDecideInput{
		Now:     time.Now(),
		Uplinks: []model.WanUplink{{ID: "u1", Priority: 1}, {ID: "u2", Priority: 2}},
		States: map[string]model.WanUplinkState{
			"u1": {State: model.WanStateDown},
			"u2": {State: model.WanStateDown}, // health is irrelevant in manual mode
		},
		Settings: model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: "u2", MinHoldSeconds: 0, RevertDelaySeconds: 0},
		Current:  "u1",
	}
	target, reason, changed := decideActiveUplink(in)
	if target != "u2" || !changed {
		t.Fatalf("expected manual mode to force u2 (even though both report down), got target=%q changed=%v reason=%q", target, changed, reason)
	}
}

func TestDecideActiveUplink_ManualModeUnknownIDDoesNotSwitch(t *testing.T) {
	in := wanFailoverDecideInput{
		Now:      time.Now(),
		Uplinks:  []model.WanUplink{{ID: "u1", Priority: 1}},
		Settings: model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: "does-not-exist", MinHoldSeconds: 0, RevertDelaySeconds: 0},
		Current:  "u1",
	}
	target, reason, changed := decideActiveUplink(in)
	if changed || target != "u1" {
		t.Fatalf("expected no switch when manualUplinkId does not refer to a real uplink, got target=%q changed=%v", target, changed)
	}
	if reason == "" {
		t.Error("expected a non-empty reason explaining why no switch happened")
	}
}

func TestDecideActiveUplink_StaleUplinkNeverSelected(t *testing.T) {
	in := wanFailoverDecideInput{
		Now: time.Now(),
		Uplinks: []model.WanUplink{
			{ID: "u1", Priority: 1, Status: true},
			{ID: "u2", Priority: 2, Status: true},
		},
		States: map[string]model.WanUplinkState{
			"u1": {State: model.WanStateUp, Stale: true}, // better priority but stale (Decision E)
			"u2": {State: model.WanStateUp, Stale: false},
		},
		Settings:          model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0},
		Current:           "",
		FirstDecisionDone: true,
	}
	target, _, changed := decideActiveUplink(in)
	if target != "u2" || !changed {
		t.Fatalf("expected the non-stale uplink u2 to be selected over the stale (but higher-priority) u1, got target=%q changed=%v", target, changed)
	}
}

// TestDecideActiveUplink_DegradedNeverTriggersOrBlocksASwitch is D-7's
// structural guarantee at the decision layer: "degraded" must never win a
// selection over a plain "up" uplink, and must never itself cause a revert.
func TestDecideActiveUplink_DegradedNeverTriggersOrBlocksASwitch(t *testing.T) {
	in := wanFailoverDecideInput{
		Now: time.Now(),
		Uplinks: []model.WanUplink{
			{ID: "u1", Priority: 1, Status: true}, // degraded, better priority
			{ID: "u2", Priority: 2, Status: true}, // plain up
		},
		States: map[string]model.WanUplinkState{
			"u1": {State: model.WanStateDegraded},
			"u2": {State: model.WanStateUp},
		},
		Settings:          model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0},
		FirstDecisionDone: true,
	}
	target, _, changed := decideActiveUplink(in)
	if target != "u2" || !changed {
		t.Fatalf("expected u2 (up) selected over u1 (degraded, D-7), got target=%q changed=%v", target, changed)
	}

	// Reverse direction: once u2 is active, u1 staying "degraded" (never
	// "up") must never cause a switch back to it.
	in2 := in
	in2.Current = "u2"
	target2, _, changed2 := decideActiveUplink(in2)
	if target2 != "u2" || changed2 {
		t.Fatalf("expected staying on u2 — a degraded u1 must never trigger a switch (D-7), got target=%q changed=%v", target2, changed2)
	}
}

func TestDecideActiveUplink_BootGraceBlocksFirstDecision(t *testing.T) {
	in := wanFailoverDecideInput{
		Now:               time.Now(),
		Uplinks:           []model.WanUplink{{ID: "u1", Priority: 1, Status: true}},
		States:            map[string]model.WanUplinkState{"u1": {State: model.WanStateUp}},
		Settings:          model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0},
		Current:           "",
		FirstDecisionDone: false,
	}
	target, reason, changed := decideActiveUplink(in)
	if changed || target != "" {
		t.Fatalf("expected no decision at all before FirstDecisionDone, got target=%q changed=%v", target, changed)
	}
	if reason == "" {
		t.Error("expected a non-empty reason")
	}
}

func TestDecideActiveUplink_MinHoldSuppressesSwitch(t *testing.T) {
	base := time.Now()
	in := wanFailoverDecideInput{
		Now: base.Add(10 * time.Second),
		Uplinks: []model.WanUplink{
			{ID: "u1", Priority: 1, Status: true},
			{ID: "u2", Priority: 2, Status: true},
		},
		States: map[string]model.WanUplinkState{
			"u1": {State: model.WanStateDown},
			"u2": {State: model.WanStateUp},
		},
		Settings:          model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 60, RevertDelaySeconds: 0},
		Current:           "u1",
		LastSwitchAt:      base, // only 10s ago, MinHoldSeconds=60
		FirstDecisionDone: true,
	}
	target, reason, changed := decideActiveUplink(in)
	if changed || target != "u1" {
		t.Fatalf("expected the switch to u2 to be suppressed by MinHoldSeconds, got target=%q changed=%v reason=%q", target, changed, reason)
	}

	// Past MinHoldSeconds, the same switch must now go through.
	in.Now = base.Add(61 * time.Second)
	target, _, changed = decideActiveUplink(in)
	if !changed || target != "u2" {
		t.Fatalf("expected the switch to u2 to succeed once MinHoldSeconds has elapsed, got target=%q changed=%v", target, changed)
	}
}

// TestDecideActiveUplink_ManualOverrideBypassesMinHold covers the QA Major 2
// finding: a manual override is a direct, already-authenticated super_admin
// action, not unattended automation at flap risk, so it must always take
// effect immediately even mere seconds after the last switch — MinHold only
// governs AUTO mode (see TestDecideActiveUplink_MinHoldSuppressesSwitch for
// the auto-mode regression this must NOT change).
func TestDecideActiveUplink_ManualOverrideBypassesMinHold(t *testing.T) {
	base := time.Now()
	in := wanFailoverDecideInput{
		Now:      base.Add(1 * time.Second), // only 1s since the last switch
		Uplinks:  []model.WanUplink{{ID: "u1", Priority: 1}, {ID: "u2", Priority: 2}},
		Settings: model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: "u2", MinHoldSeconds: 60, RevertDelaySeconds: 0},
		Current:  "u1",
		// A switch just happened 1 second ago — well inside MinHoldSeconds=60.
		LastSwitchAt: base,
	}
	target, reason, changed := decideActiveUplink(in)
	if !changed || target != "u2" {
		t.Fatalf("expected manual override to u2 to succeed immediately despite MinHoldSeconds, got target=%q changed=%v reason=%q", target, changed, reason)
	}
}

// TestDecideActiveUplink_ManualOverrideBypassesRevertDelay covers the QA
// round-2 finding on top of Major 2: the revert-delay check had NO manual-
// mode guard at all (unlike the MinHold check right above it), so a manual
// override to a higher-priority uplink that had not yet been healthy for
// RevertDelaySeconds silently returned changed=false — the API answered 200
// OK while the kernel state never actually moved, with no way for the
// operator to know why. Uses a meaningful RevertDelaySeconds (120, not 0)
// specifically because the original Major 2 tests all used
// RevertDelaySeconds=0, which could never have caught this.
func TestDecideActiveUplink_ManualOverrideBypassesRevertDelay(t *testing.T) {
	base := time.Now()
	in := wanFailoverDecideInput{
		Now: base.Add(time.Second),
		Uplinks: []model.WanUplink{
			{ID: "primary", Priority: 1, Status: true}, // higher priority (better) than backup
			{ID: "backup", Priority: 2, Status: true},
		},
		States: map[string]model.WanUplinkState{
			// primary just became healthy THIS instant — nowhere near
			// RevertDelaySeconds=120 of continuous health.
			"primary": {State: model.WanStateUp, LastChangeAt: base.UTC().Format(time.RFC3339)},
			"backup":  {State: model.WanStateUp},
		},
		Settings:     model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: "primary", MinHoldSeconds: 0, RevertDelaySeconds: 120},
		Current:      "backup",
		LastSwitchAt: base.Add(-time.Hour), // MinHold is not the thing under test here
	}
	target, reason, changed := decideActiveUplink(in)
	if !changed || target != "primary" {
		t.Fatalf("expected manual override to primary to succeed immediately despite RevertDelaySeconds=120 and primary having just become healthy, got target=%q changed=%v reason=%q", target, changed, reason)
	}
}

func TestDecideActiveUplink_RevertDelayBlocksThenAllows(t *testing.T) {
	base := time.Now()
	in := wanFailoverDecideInput{
		Now: base.Add(time.Second),
		Uplinks: []model.WanUplink{
			{ID: "primary", Priority: 1, Status: true},
			{ID: "backup", Priority: 2, Status: true},
		},
		States: map[string]model.WanUplinkState{
			"primary": {State: model.WanStateUp, LastChangeAt: base.UTC().Format(time.RFC3339)}, // just recovered
			"backup":  {State: model.WanStateUp},
		},
		Settings:          model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 30},
		Current:           "backup", // currently on the lower-priority uplink
		LastSwitchAt:      base.Add(-time.Hour),
		FirstDecisionDone: true,
	}
	target, reason, changed := decideActiveUplink(in)
	if changed || target != "backup" {
		t.Fatalf("expected the revert to primary to be suppressed before RevertDelaySeconds elapses, got target=%q changed=%v reason=%q", target, changed, reason)
	}

	in.Now = base.Add(31 * time.Second)
	target, _, changed = decideActiveUplink(in)
	if !changed || target != "primary" {
		t.Fatalf("expected the revert to primary to succeed once RevertDelaySeconds has elapsed, got target=%q changed=%v", target, changed)
	}
}

func TestDecideActiveUplink_AntiBlackholeKeepsCurrentWhenNothingHealthy(t *testing.T) {
	in := wanFailoverDecideInput{
		Now: time.Now(),
		Uplinks: []model.WanUplink{
			{ID: "u1", Priority: 1, Status: true},
			{ID: "u2", Priority: 2, Status: true},
		},
		States: map[string]model.WanUplinkState{
			"u1": {State: model.WanStateDown},
			"u2": {State: model.WanStateDown},
		},
		Settings:          model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0},
		Current:           "u1",
		FirstDecisionDone: true,
	}
	target, reason, changed := decideActiveUplink(in)
	if changed || target != "u1" {
		t.Fatalf("expected to keep the current uplink when nothing is healthy (anti-blackhole), got target=%q changed=%v", target, changed)
	}
	if reason == "" {
		t.Error("expected a non-empty reason")
	}
}

// --- WanFailoverController end-to-end (real WanMonitor + RoutingService) --

// newTestFailoverController wires a real WanFailoverController to a real
// WanMonitor (backed by kernel.MockPathProbe) and a real RoutingService
// (backed by trackingRoutingManager, defined in routing_test.go — same
// package) — mirrors newTestWanMonitor's shape (wan_monitor_test.go).
// startedAt is set far in the past so tests don't have to think about
// wanFailoverStartupGrace unless they are specifically testing it.
func newTestFailoverController(t *testing.T) (*WanFailoverController, *db.Repository, *trackingRoutingManager, *WanMonitor, *kernel.MockPathProbe) {
	t.Helper()
	sqlDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	// modernc.org/sqlite's ":memory:" DSN is per-CONNECTION, not per *sql.DB
	// — database/sql's pool can silently open a second connection under
	// concurrent access (T-26's TestReconcile_ConcurrentCallersSerialized),
	// which would see a completely separate, freshly-migrated-less empty
	// database. Force a single shared connection so every goroutine in this
	// test package actually reads/writes the same in-memory DB.
	sqlDB.SetMaxOpenConns(1)

	repo := db.NewRepository(sqlDB)

	tracker := &trackingRoutingManager{}
	routing := NewRoutingService(repo, tracker)

	probe := kernel.NewMockPathProbe()
	eventLog := NewEventLogService(repo)
	bus := NewNetEventBus()
	ring := NewWanUplinkMetricsRing()
	monitor := NewWanMonitor(repo, probe, eventLog, bus, ring)

	controller := NewWanFailoverController(repo, monitor, routing, eventLog, bus)
	controller.startedAt = time.Now().Add(-2 * wanFailoverStartupGrace)

	return controller, repo, tracker, monitor, probe
}

// createFailoverUplink seeds a WanUplink with FailStrikes=RecoverStrikes=1
// so a single probe round is enough to flip its health state — keeps
// controller tests focused on failover decisions, not on wan_monitor's own
// strike-counting (already covered by wan_monitor_test.go).
func createFailoverUplink(t *testing.T, repo *db.Repository, name, iface string, priority int) model.WanUplink {
	t.Helper()
	u, err := repo.CreateWanUplink(model.WanUplinkInput{
		Name: name, Interface: iface, Priority: priority,
		ProbeTargets: []string{"1.1.1.1"}, ProbeMethod: model.WanProbeMethodICMP,
		ProbeIntervalSeconds: 2, ProbeCount: 1, ProbeTimeoutMs: 100,
		LossThresholdPct: 50, LatencyThresholdMs: 200,
		FailStrikes: 1, RecoverStrikes: 1, Status: true,
	})
	if err != nil {
		t.Fatalf("CreateWanUplink failed: %v", err)
	}
	return *u
}

func setFailoverSettings(t *testing.T, repo *db.Repository, s model.WanFailoverSettings) {
	t.Helper()
	if err := repo.UpdateWanFailoverSettings(s); err != nil {
		t.Fatalf("UpdateWanFailoverSettings failed: %v", err)
	}
}

func TestWanFailoverController_PrimaryDownSwitchesToBackup(t *testing.T) {
	c, repo, tracker, monitor, probe := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	probe.SetAllDead(primary.Interface, true)
	monitor.probeUplink(context.Background(), primary, now.Add(3*time.Second))

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})

	c.tick(now.Add(4 * time.Second))

	status := c.Status()
	if status.ActiveUplinkID != backup.ID {
		t.Fatalf("expected backup uplink active after primary went down, got %q (reason=%q)", status.ActiveUplinkID, status.LastSwitchReason)
	}

	wantActive := wanFailoverActiveMetricBase + backup.Priority
	if got, ok := tracker.enforcedMetrics[backup.Interface]; !ok || got != wantActive {
		t.Errorf("expected backup interface %s enforced at active metric %d, got %d (present=%v)", backup.Interface, wantActive, got, ok)
	}
	wantStandby := wanFailoverStandbyMetricBase + 10*primary.Priority
	if got, ok := tracker.enforcedMetrics[primary.Interface]; !ok || got != wantStandby {
		t.Errorf("expected primary interface %s enforced at standby metric %d, got %d (present=%v)", primary.Interface, wantStandby, got, ok)
	}
}

func TestWanFailoverController_RevertDelayBlocksThenAllows(t *testing.T) {
	c, repo, _, monitor, probe := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	probe.SetAllDead(primary.Interface, true)
	monitor.probeUplink(context.Background(), primary, now.Add(3*time.Second))

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 30,
	})

	t1 := now.Add(4 * time.Second)
	c.tick(t1)
	if got := c.Status().ActiveUplinkID; got != backup.ID {
		t.Fatalf("expected backup active after primary down, got %q", got)
	}

	// Primary recovers.
	probe.SetAllDead(primary.Interface, false)
	recoverAt := t1.Add(5 * time.Second)
	monitor.probeUplink(context.Background(), primary, recoverAt)

	// Immediately after recovery: RevertDelaySeconds has not elapsed yet.
	c.tick(recoverAt.Add(time.Second))
	if got := c.Status().ActiveUplinkID; got != backup.ID {
		t.Fatalf("expected still backup active before RevertDelaySeconds elapsed, got %q", got)
	}

	// Past RevertDelaySeconds: must revert now.
	c.tick(recoverAt.Add(31 * time.Second))
	if got := c.Status().ActiveUplinkID; got != primary.ID {
		t.Fatalf("expected reverted to primary after RevertDelaySeconds elapsed, got %q (reason=%q)", got, c.Status().LastSwitchReason)
	}
}

func TestWanFailoverController_MinHoldBlocksSecondSwitch(t *testing.T) {
	c, repo, _, monitor, probe := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 60, RevertDelaySeconds: 0,
	})

	c.tick(now.Add(time.Second))
	if got := c.Status().ActiveUplinkID; got != primary.ID {
		t.Fatalf("expected primary activated on the first ever decision, got %q", got)
	}

	// Fail primary and tick again, well inside MinHoldSeconds.
	probe.SetAllDead(primary.Interface, true)
	monitor.probeUplink(context.Background(), primary, now.Add(2*time.Second))
	c.tick(now.Add(3 * time.Second))

	if got := c.Status().ActiveUplinkID; got != primary.ID {
		t.Fatalf("expected the switch to backup to be rejected by MinHoldSeconds, got active=%q", got)
	}

	// Past MinHoldSeconds: the switch must now go through.
	c.tick(now.Add(61 * time.Second))
	if got := c.Status().ActiveUplinkID; got != backup.ID {
		t.Fatalf("expected switch to backup once MinHoldSeconds has elapsed, got %q (reason=%q)", got, c.Status().LastSwitchReason)
	}
}

// TestWanFailoverController_ManualOverrideBypassesMinHold is the end-to-end
// (controller.tick) counterpart of TestDecideActiveUplink_
// ManualOverrideBypassesMinHold (QA Major 2): switching ManualUplinkID a
// second time, mere seconds after the first manual switch and well inside
// MinHoldSeconds, must take effect immediately rather than being rejected.
func TestWanFailoverController_ManualOverrideBypassesMinHold(t *testing.T) {
	c, repo, _, monitor, _ := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: primary.ID, MinHoldSeconds: 60, RevertDelaySeconds: 0,
	})
	c.tick(now.Add(time.Second))
	if got := c.Status().ActiveUplinkID; got != primary.ID {
		t.Fatalf("expected primary activated by the first manual override, got %q", got)
	}

	// Immediately (well inside MinHoldSeconds=60) force ManualUplinkID to
	// backup instead — must switch right away, not be rejected.
	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: backup.ID, MinHoldSeconds: 60, RevertDelaySeconds: 0,
	})
	c.tick(now.Add(2 * time.Second))
	if got := c.Status().ActiveUplinkID; got != backup.ID {
		t.Fatalf("expected manual override to backup to take effect immediately despite MinHoldSeconds=60, got %q (reason=%q)", got, c.Status().LastSwitchReason)
	}
}

// TestWanFailoverController_ManualOverrideBypassesRevertDelay is the
// end-to-end (controller.tick) counterpart of TestDecideActiveUplink_
// ManualOverrideBypassesRevertDelay — the exact scenario QA reproduced live
// against the API: primary fails, backup takes over, primary recovers, and
// the operator immediately forces ManualUplinkID back to primary (a
// higher-priority uplink that has NOT yet been healthy for
// RevertDelaySeconds). Before this fix decideActiveUplink silently returned
// changed=false here — the settings PUT/override POST would answer 200 OK
// but the active uplink never actually moved.
func TestWanFailoverController_ManualOverrideBypassesRevertDelay(t *testing.T) {
	c, repo, _, monitor, probe := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	probe.SetAllDead(primary.Interface, true)
	monitor.probeUplink(context.Background(), primary, now.Add(3*time.Second))

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 120,
	})
	t1 := now.Add(4 * time.Second)
	c.tick(t1)
	if got := c.Status().ActiveUplinkID; got != backup.ID {
		t.Fatalf("expected backup active after primary down, got %q", got)
	}

	// Primary recovers.
	probe.SetAllDead(primary.Interface, false)
	recoverAt := t1.Add(5 * time.Second)
	monitor.probeUplink(context.Background(), primary, recoverAt)

	// Immediately after recovery (nowhere near RevertDelaySeconds=120), the
	// operator forces ManualUplinkID back to primary — must switch right
	// away, not be silently rejected.
	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: primary.ID, MinHoldSeconds: 0, RevertDelaySeconds: 120,
	})
	c.tick(recoverAt.Add(time.Second))
	if got := c.Status().ActiveUplinkID; got != primary.ID {
		t.Fatalf("expected manual override to primary to take effect immediately despite RevertDelaySeconds=120, got %q (reason=%q)", got, c.Status().LastSwitchReason)
	}
}

func TestWanFailoverController_AllDownKeepsActiveAndLogsCriticalOnce(t *testing.T) {
	c, repo, _, monitor, probe := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})
	c.tick(now.Add(time.Second))
	activeBefore := c.Status().ActiveUplinkID
	if activeBefore == "" {
		t.Fatal("expected an uplink active before both go down")
	}

	probe.SetAllDead(primary.Interface, true)
	probe.SetAllDead(backup.Interface, true)
	monitor.probeUplink(context.Background(), primary, now.Add(2*time.Second))
	monitor.probeUplink(context.Background(), backup, now.Add(2*time.Second))

	c.tick(now.Add(3 * time.Second))
	c.tick(now.Add(4 * time.Second))
	c.tick(now.Add(5 * time.Second))

	if got := c.Status().ActiveUplinkID; got != activeBefore {
		t.Errorf("expected active uplink to stay %q (anti-blackhole) while all are down, got %q", activeBefore, got)
	}

	events, _, err := c.eventLog.Query(model.EventCategoryNetwork, model.EventSeverityCritical, "", 1000, 0)
	if err != nil {
		t.Fatalf("eventLog.Query failed: %v", err)
	}
	n := 0
	for _, ev := range events {
		if ev.Action == "wan-failover" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("expected exactly 1 critical wan-failover event after 3 all-down ticks, got %d", n)
	}
}

func TestWanFailoverController_KillSwitchOffClearsOverridesOnceThenQuiet(t *testing.T) {
	c, repo, tracker, monitor, _ := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)
	// Seed "the kernel already has a default route at this metric" for both
	// interfaces (Task 14, Decision C) so the override machinery has
	// something to snapshot before overriding, and therefore something to
	// restore once the kill switch clears the overrides again — without
	// this, DefaultRouteMetric reports found=false and (with no DB
	// interfaces.Metric fallback either, since these uplinks' interfaces
	// have no `interfaces` row at all) the restore step correctly has
	// nothing to do (see TestFailoverOverride_* in routing_test.go for that
	// path specifically).
	tracker.SetDefaultRouteMetric(primary.Interface, 100, true)
	tracker.SetDefaultRouteMetric(backup.Interface, 100, true)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})
	c.tick(now.Add(time.Second))
	if got := c.Status().ActiveUplinkID; got == "" {
		t.Fatal("expected an active uplink before disabling")
	}
	callsBeforeDisable := len(tracker.enforceCalls)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: false, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})

	c.tick(now.Add(2 * time.Second)) // kill switch off -> one-time restore pass
	afterFirstDisableTick := len(tracker.enforceCalls)
	if afterFirstDisableTick <= callsBeforeDisable {
		t.Fatalf("expected at least one EnforceDefaultRouteMetric call (restoring overrides) on the first disabled tick, got %d -> %d", callsBeforeDisable, afterFirstDisableTick)
	}
	if got := c.routing.FailoverOverrides(); len(got) != 0 {
		t.Errorf("expected all failover overrides cleared, got %v", got)
	}

	// Further ticks while still disabled must stay completely silent.
	c.tick(now.Add(3 * time.Second))
	c.tick(now.Add(4 * time.Second))
	if got := len(tracker.enforceCalls); got != afterFirstDisableTick {
		t.Errorf("expected zero additional EnforceDefaultRouteMetric calls on subsequent disabled ticks, got %d -> %d", afterFirstDisableTick, got)
	}
}

func TestWanFailoverController_ManualModeWinsOverHealthAndRejectsUnknownID(t *testing.T) {
	c, repo, _, monitor, probe := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	// Primary is healthy, backup is DOWN — manual mode must still be able to
	// force backup active despite it being unhealthy.
	monitor.probeUplink(context.Background(), primary, now)
	probe.SetAllDead(backup.Interface, true)
	monitor.probeUplink(context.Background(), backup, now)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: backup.ID, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})
	c.tick(now.Add(time.Second))

	status := c.Status()
	if status.ActiveUplinkID != backup.ID {
		t.Fatalf("expected manual mode to force the (unhealthy) backup uplink active, got %q (reason=%q)", status.ActiveUplinkID, status.LastSwitchReason)
	}

	// Now point manualUplinkId at a nonexistent ID — must NOT switch, and the
	// currently-active uplink must stay backup.
	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: "does-not-exist", MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})
	c.tick(now.Add(2 * time.Second))
	status = c.Status()
	if status.ActiveUplinkID != backup.ID {
		t.Fatalf("expected active uplink to remain backup when manualUplinkId is invalid, got %q", status.ActiveUplinkID)
	}
	if status.LastSwitchReason == "" {
		t.Error("expected LastSwitchReason to still be populated from the last real switch")
	}
}

// TestWanFailoverController_CheckBypassed_LogsPerInterfaceOnSwitch covers
// the QA finding (Minor 3): checkBypassed's "already logged" guard must be
// tracked per interface, not as one shared bool — otherwise the active
// uplink switching directly from one bypassed interface to a DIFFERENT
// bypassed interface (no non-bypassed tick in between) would silently
// swallow the warning for the new interface.
func TestWanFailoverController_CheckBypassed_LogsPerInterfaceOnSwitch(t *testing.T) {
	c, _, _, _, _ := newTestFailoverController(t)
	routing := c.routing

	ifaceA, ifaceB := "wan-a", "wan-b"
	uplinkA := model.WanUplink{ID: "u1", Interface: ifaceA}
	uplinkB := model.WanUplink{ID: "u2", Interface: ifaceB}

	routing.SetFailoverMetricOverride(ifaceA, wanFailoverActiveMetricBase)
	routing.SetFailoverMetricOverride(ifaceB, wanFailoverActiveMetricBase)

	// An active DB static 0.0.0.0/0 route on BOTH interfaces makes both
	// overrides bypassed (T-14 precedence level 1) at the same time — this
	// is what routing.FailoverBypassedInterfaces() reports from here on.
	dbRoutes := []model.StaticRoute{
		{ID: "r1", Destination: "0.0.0.0/0", Gateway: "10.0.0.1", Interface: ifaceA, Status: true, Type: "customgateway"},
		{ID: "r2", Destination: "0.0.0.0/0", Gateway: "10.0.0.2", Interface: ifaceB, Status: true, Type: "customgateway"},
	}
	routing.enforceInterfaceMetrics(dbRoutes)

	var buf bytes.Buffer
	origOut := log.Writer()
	origFlags := log.Flags()
	log.SetOutput(&buf)
	t.Cleanup(func() {
		log.SetOutput(origOut)
		log.SetFlags(origFlags)
	})

	// Active uplink is A (bypassed) — must log once.
	c.checkBypassed(uplinkA)
	if !strings.Contains(buf.String(), ifaceA) {
		t.Fatalf("expected a bypass warning logged for %s, got: %q", ifaceA, buf.String())
	}

	// A second consecutive check on the SAME still-bypassed interface must
	// stay quiet (the original single-flag behavior this preserves).
	buf.Reset()
	c.checkBypassed(uplinkA)
	if buf.Len() != 0 {
		t.Errorf("expected no repeat log while %s stays bypassed, got: %q", ifaceA, buf.String())
	}

	// The active uplink switches DIRECTLY from A to B — both are bypassed,
	// so there is no non-bypassed tick in between. B must still get its own
	// warning (this is exactly what a single shared bool would have missed).
	buf.Reset()
	c.checkBypassed(uplinkB)
	if !strings.Contains(buf.String(), ifaceB) {
		t.Fatalf("expected a bypass warning logged for the newly-active bypassed interface %s after switching directly from another bypassed interface %s, got: %q", ifaceB, ifaceA, buf.String())
	}
}

func TestWanFailoverController_BootGraceHoldsOverrideUntilStatesKnown(t *testing.T) {
	c, repo, tracker, _, _ := newTestFailoverController(t)
	// Override the harness's "far in the past" startedAt so the boot-grace
	// window is actually in effect for this test.
	c.startedAt = time.Now()

	createFailoverUplink(t, repo, "Primary", "wan-p", 1)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})

	// No probe round has ever run — every uplink's state is "unknown" and
	// wanFailoverStartupGrace has not elapsed — must not write any override.
	c.tick(time.Now())

	if got := c.routing.FailoverOverrides(); len(got) != 0 {
		t.Errorf("expected no overrides written before the boot grace/known-states condition is met, got %v", got)
	}
	if got := c.Status().ActiveUplinkID; got != "" {
		t.Errorf("expected no active uplink decided yet, got %q", got)
	}
	if len(tracker.enforceCalls) != 0 {
		t.Errorf("expected zero EnforceDefaultRouteMetric calls before the first decision, got %d", len(tracker.enforceCalls))
	}
}

// --- T-26: bidirectional switching + concurrency regression tests ---------
// (docs/ref/wan-failover-findings.md)

// TestWanFailover_SwitchBothDirectionsKeepsEveryDefaultRoute drives repeated
// A -> B -> A -> B switches (3+ round trips) directly through
// RoutingService's failover-override API (mirroring what
// WanFailoverController.enforceOverrides does every tick) and asserts that
// after EVERY switch, BOTH interfaces still have a live default route (per
// the T-25 simulated FIB) and the active one sits inside Decision F's
// reserved active band. Run under both DB interface orderings — this is
// what actually catches an ordering-dependent regression, even though
// (see the T-26 handoff notes) Decision F's disjoint per-uplink bands make a
// plain 2-uplink swap collision-free by construction; this test remains a
// valuable end-to-end guard against a reordering/bookkeeping regression in
// enforceInterfaceMetrics itself.
func TestWanFailover_SwitchBothDirectionsKeepsEveryDefaultRoute(t *testing.T) {
	for _, order := range [][2]string{{"wanA", "wanB"}, {"wanB", "wanA"}} {
		t.Run(order[0]+","+order[1], func(t *testing.T) {
			testWanFailoverBidirectionalSwitching(t, order[0], order[1])
		})
	}
}

func testWanFailoverBidirectionalSwitching(t *testing.T, firstInDB, secondInDB string) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)
	seedWanFailoverPairInterfaces(t, repo, firstInDB, secondInDB)

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	const priorityA, priorityB = 1, 2
	priority := map[string]int{"wanA": priorityA, "wanB": priorityB}
	other := map[string]string{"wanA": "wanB", "wanB": "wanA"}

	switchActiveTo := func(active string) {
		standby := other[active]
		desired := map[string]int{
			active:  wanFailoverActiveMetricBase + priority[active],
			standby: wanFailoverStandbyMetricBase + 10*priority[standby],
		}
		svc.SetFailoverMetricOverrides(desired)
		svc.enforceInterfaceMetrics(nil)

		for _, name := range []string{"wanA", "wanB"} {
			metric, found, err := tracker.DefaultRouteMetric(name)
			if err != nil || !found {
				t.Fatalf("after switching active to %q (DB order %s,%s): interface %q lost its default route entirely (found=%v err=%v)", active, firstInDB, secondInDB, name, found, err)
			}
			if name == active && (metric < model.WanReservedActiveMetricMin || metric > model.WanReservedActiveMetricMax) {
				t.Errorf("after switching active to %q: active uplink %q metric %d is not inside the reserved active band %d-%d", active, name, metric, model.WanReservedActiveMetricMin, model.WanReservedActiveMetricMax)
			}
		}
	}

	// A -> B -> A -> B -> A -> B: 3 full round trips.
	sequence := []string{"wanA", "wanB", "wanA", "wanB", "wanA", "wanB"}
	for _, active := range sequence {
		switchActiveTo(active)
	}
}

// TestReconcile_ConcurrentCallersSerialized calls
// RoutingService.ReconcileKernelRoutingTable concurrently from several
// goroutines while the WAN failover controller keeps ticking (which itself
// mutates overrides and reconciles) — T-22's reconcileMu must serialize all
// of this so the simulated FIB ends up in a consistent state with no lost
// routes. Intended to be run with -race (go test -race) to also catch a
// data race in the shared maps, not just a logical inconsistency.
func TestReconcile_ConcurrentCallersSerialized(t *testing.T) {
	c, repo, tracker, monitor, _ := newTestFailoverController(t)

	primary := createFailoverUplink(t, repo, "Primary", "wan-p", 1)
	backup := createFailoverUplink(t, repo, "Backup", "wan-b", 2)

	now := time.Now()
	monitor.probeUplink(context.Background(), primary, now)
	monitor.probeUplink(context.Background(), backup, now)

	setFailoverSettings(t, repo, model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	})

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Several goroutines hammering ReconcileKernelRoutingTable directly,
	// exactly as main.go's "routing" NetEventBus subscriber and an
	// HTTP-triggered reconcile would concurrently with the controller below.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.routing.ReconcileKernelRoutingTable()
				}
			}
		}()
	}

	// The controller keeps ticking concurrently, mutating overrides and
	// triggering its own reconciles.
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 25; i++ {
			c.tick(now.Add(time.Duration(i) * time.Second))
		}
	}()

	time.Sleep(200 * time.Millisecond) // let goroutines actually interleave
	close(stop)
	wg.Wait()

	// After everything settles, both uplinks must still have SOME default
	// route, and the active one must sit inside the reserved active band.
	status := c.Status()
	for _, u := range []model.WanUplink{primary, backup} {
		metric, found, err := tracker.DefaultRouteMetric(u.Interface)
		if err != nil || !found {
			t.Errorf("interface %s (uplink %s) lost its default route entirely after concurrent reconciles (err=%v)", u.Interface, u.ID, err)
			continue
		}
		if u.ID == status.ActiveUplinkID && (metric < model.WanReservedActiveMetricMin || metric > model.WanReservedActiveMetricMax) {
			t.Errorf("active uplink %s metric %d is not inside the reserved active band %d-%d after concurrent reconciles", u.ID, metric, model.WanReservedActiveMetricMin, model.WanReservedActiveMetricMax)
		}
	}
}
