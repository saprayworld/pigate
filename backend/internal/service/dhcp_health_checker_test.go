package service

import (
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// defaultTestDhcpHealthSettings mirrors the shipped defaults (T-04 seed row).
func defaultTestDhcpHealthSettings() model.DhcpHealthSettings {
	return model.DhcpHealthSettings{
		Enabled:                true,
		CheckIntervalSeconds:   60,
		ConsecutiveStrikes:     3,
		MinRunningSeconds:      30,
		RestartBackoffSeconds:  300,
		MaxRestartsBeforePause: 3,
	}
}

// =========================================================================
// classifyAddrs
// =========================================================================

func TestDhcpHealthClassifyAddrs(t *testing.T) {
	cases := []struct {
		name           string
		addrs          []string
		wantReal       bool
		wantLinkLocal  bool
		descriptionMsg string
	}{
		{"real only", []string{"192.168.1.5/24"}, true, false, "a normal LAN address must classify as real, not link-local"},
		{"link-local only", []string{"169.254.1.2/16"}, false, true, "an APIPA address must classify as link-local"},
		{"none", []string{}, false, false, "no addresses at all must yield both false"},
		{"real and link-local", []string{"192.168.1.5/24", "169.254.7.8/16"}, true, true, "a coexisting real+link-local address must set both flags"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			hasReal, hasLinkLocal := classifyAddrs(tc.addrs)
			if hasReal != tc.wantReal || hasLinkLocal != tc.wantLinkLocal {
				t.Fatalf("%s: classifyAddrs(%v) = (%t,%t), want (%t,%t)",
					tc.descriptionMsg, tc.addrs, hasReal, hasLinkLocal, tc.wantReal, tc.wantLinkLocal)
			}
		})
	}
}

// =========================================================================
// decideNextState
// =========================================================================

func TestDhcpHealthDecideNextState_EligibilityGateResetsStrikesOnly(t *testing.T) {
	settings := defaultTestDhcpHealthSettings()
	now := time.Now()

	// Pre-existing state simulating an interface mid-episode: some strikes
	// accumulated, one restart already performed, ceiling already logged.
	state := &ifaceHealthState{
		strikes:              5,
		runningSince:         now.Add(-time.Hour),
		restartsSinceRecover: 2,
		lastRestartAt:        now.Add(-time.Minute),
		ceilingLogged:        true,
	}

	action := decideNextState(state, false /* isUp */, false /* isRunning */, false, false, settings, now)

	if action != actionNone {
		t.Fatalf("ineligible interface must never take an action, got %v", action)
	}
	if state.strikes != 0 {
		t.Fatalf("ineligible interface must reset strikes to 0, got %d", state.strikes)
	}
	if !state.runningSince.IsZero() {
		t.Fatalf("ineligible interface must clear runningSince, got %v", state.runningSince)
	}
	// Caution 5: backoff/ceiling bookkeeping must NOT reset on mere loss of
	// eligibility — only a genuine return to health resets those.
	if state.restartsSinceRecover != 2 {
		t.Fatalf("restartsSinceRecover must be preserved across an eligibility loss, got %d", state.restartsSinceRecover)
	}
	if !state.ceilingLogged {
		t.Fatalf("ceilingLogged must be preserved across an eligibility loss")
	}
}

func TestDhcpHealthDecideNextState_FirstTickAlreadyRunningDefersStrikes(t *testing.T) {
	// Decision 3: an interface observed up+running with no prior state must
	// have runningSince set to now, deferring strike counting by one
	// MinRunningSeconds window.
	settings := defaultTestDhcpHealthSettings()
	now := time.Now()
	state := &ifaceHealthState{} // zero value: no prior state

	action := decideNextState(state, true, true, false, true /* only link-local */, settings, now)

	if action != actionNone {
		t.Fatalf("first tick must never act immediately (min-running guard), got %v", action)
	}
	if !state.runningSince.Equal(now) {
		t.Fatalf("runningSince must be set to now on first observed tick, got %v want %v", state.runningSince, now)
	}
	if state.strikes != 0 {
		t.Fatalf("no strike should be counted before MinRunningSeconds elapses, got %d", state.strikes)
	}
}

func TestDhcpHealthDecideNextState_MinRunningGuardBlocksStrikeCounting(t *testing.T) {
	settings := defaultTestDhcpHealthSettings()
	now := time.Now()
	// Became running 5s ago; MinRunningSeconds is 30 — must not count yet.
	state := &ifaceHealthState{runningSince: now.Add(-5 * time.Second)}

	action := decideNextState(state, true, true, false, true, settings, now)

	if action != actionNone {
		t.Fatalf("within the min-running window no action must be taken, got %v", action)
	}
	if state.strikes != 0 {
		t.Fatalf("within the min-running window no strike must be counted, got %d", state.strikes)
	}
}

func TestDhcpHealthDecideNextState_StrikeAccumulationBeforeAction(t *testing.T) {
	settings := defaultTestDhcpHealthSettings() // ConsecutiveStrikes = 3
	now := time.Now()
	state := &ifaceHealthState{runningSince: now.Add(-time.Hour)} // well past min-running

	for i := 1; i < settings.ConsecutiveStrikes; i++ {
		action := decideNextState(state, true, true, false, true, settings, now)
		if action != actionNone {
			t.Fatalf("strike %d/%d must not yet act, got %v", i, settings.ConsecutiveStrikes, action)
		}
		if state.strikes != i {
			t.Fatalf("expected strikes=%d after tick %d, got %d", i, i, state.strikes)
		}
	}

	// The ConsecutiveStrikes-th tick must finally act (no real IP -> restart branch).
	action := decideNextState(state, true, true, false, true, settings, now)
	if action != actionRestart {
		t.Fatalf("reaching ConsecutiveStrikes with no real IP must trigger a restart, got %v", action)
	}
}

func TestDhcpHealthDecideNextState_DeleteBranchWhenRealCoexists(t *testing.T) {
	settings := defaultTestDhcpHealthSettings()
	now := time.Now()
	state := &ifaceHealthState{runningSince: now.Add(-time.Hour), strikes: settings.ConsecutiveStrikes - 1}

	action := decideNextState(state, true, true, true /* hasReal */, true /* hasLinkLocal */, settings, now)

	if action != actionDeleteAddr {
		t.Fatalf("a real IP coexisting with a link-local address must trigger deleteAddr, got %v", action)
	}
	if state.strikes != 0 {
		t.Fatalf("deleteAddr must reset strikes to 0 (rule 1), got %d", state.strikes)
	}
}

func TestDhcpHealthDecideNextState_RestartBranchWhenNoRealIP(t *testing.T) {
	settings := defaultTestDhcpHealthSettings()
	now := time.Now()
	state := &ifaceHealthState{runningSince: now.Add(-time.Hour), strikes: settings.ConsecutiveStrikes - 1}

	// No real IP at all (neither hasReal nor hasLinkLocal) must also go
	// through the restart branch, same as link-local-only.
	action := decideNextState(state, true, true, false, false, settings, now)

	if action != actionRestart {
		t.Fatalf("no IPv4 address at all must trigger a restart, got %v", action)
	}
	if state.strikes != 0 {
		t.Fatalf("restart must reset strikes to 0 (rule 1), got %d", state.strikes)
	}
	if state.restartsSinceRecover != 1 {
		t.Fatalf("restart must increment restartsSinceRecover, got %d", state.restartsSinceRecover)
	}
	if !state.lastRestartAt.Equal(now) {
		t.Fatalf("restart must stamp lastRestartAt = now, got %v", state.lastRestartAt)
	}
}

func TestDhcpHealthDecideNextState_ActionResetsStrikesButNotCeilingBookkeeping(t *testing.T) {
	// State-machine rule 1: any real action (deleteAddr OR restart) resets
	// strikes but leaves restartsSinceRecover/ceilingLogged untouched.
	settings := defaultTestDhcpHealthSettings()
	now := time.Now()
	state := &ifaceHealthState{
		runningSince:         now.Add(-time.Hour),
		strikes:              settings.ConsecutiveStrikes - 1,
		restartsSinceRecover: 1,
		ceilingLogged:        false,
	}

	action := decideNextState(state, true, true, false, false, settings, now)
	if action != actionRestart {
		t.Fatalf("expected restart, got %v", action)
	}
	if state.strikes != 0 {
		t.Fatalf("strikes must reset to 0 after a restart, got %d", state.strikes)
	}
	if state.restartsSinceRecover != 2 {
		t.Fatalf("restartsSinceRecover must accumulate (not reset) across restarts, got %d", state.restartsSinceRecover)
	}
}

func TestDhcpHealthDecideNextState_BackoffBlocksRepeatedRestart(t *testing.T) {
	settings := defaultTestDhcpHealthSettings() // RestartBackoffSeconds = 300
	now := time.Now()
	state := &ifaceHealthState{
		runningSince:         now.Add(-time.Hour),
		strikes:              settings.ConsecutiveStrikes - 1,
		restartsSinceRecover: 1,
		lastRestartAt:        now.Add(-10 * time.Second), // well within the 300s backoff
	}

	action := decideNextState(state, true, true, false, false, settings, now)

	if action != actionRestartSkippedBackoff {
		t.Fatalf("a restart attempted within the backoff window must be skipped, got %v", action)
	}
	// Not a "real action": strikes must NOT reset here, matching the plan's
	// intent that only deleteAddr/restart resets strikes.
	if state.strikes != settings.ConsecutiveStrikes {
		t.Fatalf("backoff-skip must not reset strikes, got %d want %d", state.strikes, settings.ConsecutiveStrikes)
	}
}

func TestDhcpHealthDecideNextState_CeilingReachedLoggedOnce(t *testing.T) {
	settings := defaultTestDhcpHealthSettings() // MaxRestartsBeforePause = 3
	now := time.Now()
	state := &ifaceHealthState{
		runningSince:         now.Add(-time.Hour),
		strikes:              settings.ConsecutiveStrikes - 1,
		restartsSinceRecover: settings.MaxRestartsBeforePause, // already at the ceiling
		lastRestartAt:        now.Add(-time.Hour),             // backoff long elapsed
	}

	action := decideNextState(state, true, true, false, false, settings, now)
	if action != actionRestartCeilingReached {
		t.Fatalf("hitting the ceiling must report actionRestartCeilingReached, got %v", action)
	}
	if !state.ceilingLogged {
		t.Fatalf("ceilingLogged must be set true after reporting the ceiling")
	}

	// Drive strikes back up to the threshold again on subsequent ticks; the
	// ceiling notice must not fire a second time.
	for i := 0; i < settings.ConsecutiveStrikes; i++ {
		action = decideNextState(state, true, true, false, false, settings, now)
	}
	if action != actionNone {
		t.Fatalf("ceiling must only be reported once per episode, got %v on a later tick", action)
	}
}

func TestDhcpHealthDecideNextState_HealthyRecoveryResetsEverything(t *testing.T) {
	settings := defaultTestDhcpHealthSettings()
	now := time.Now()
	state := &ifaceHealthState{
		runningSince:         now.Add(-time.Hour),
		strikes:              7,
		restartsSinceRecover: 2,
		ceilingLogged:        true,
	}

	action := decideNextState(state, true, true, true /* hasReal */, false /* hasLinkLocal */, settings, now)

	if action != actionNone {
		t.Fatalf("a genuinely healthy interface must not trigger any action, got %v", action)
	}
	if state.strikes != 0 {
		t.Fatalf("healthy recovery must reset strikes, got %d", state.strikes)
	}
	if state.restartsSinceRecover != 0 {
		t.Fatalf("healthy recovery must reset restartsSinceRecover, got %d", state.restartsSinceRecover)
	}
	if state.ceilingLogged {
		t.Fatalf("healthy recovery must clear ceilingLogged")
	}
}

func TestDhcpHealthDecideNextState_DeleteBranchRateLimitedByStrikeReset(t *testing.T) {
	// Rule 2: deleteAddr has no backoff/ceiling of its own — only the strike
	// reset from rule 1 limits how often it fires, so a 169.254 address
	// re-added on every tick can only be deleted once every ConsecutiveStrikes
	// ticks, not on every tick.
	settings := defaultTestDhcpHealthSettings() // ConsecutiveStrikes = 3
	now := time.Now()
	state := &ifaceHealthState{runningSince: now.Add(-time.Hour)}

	var actions []healthAction
	for i := 0; i < settings.ConsecutiveStrikes*2; i++ {
		actions = append(actions, decideNextState(state, true, true, true, true, settings, now))
	}

	// Expect: None, None, DeleteAddr, None, None, DeleteAddr (for ConsecutiveStrikes=3)
	deleteCount := 0
	for i, a := range actions {
		if a == actionDeleteAddr {
			deleteCount++
			if (i+1)%settings.ConsecutiveStrikes != 0 {
				t.Fatalf("deleteAddr fired at tick %d, expected only every %d-th tick", i+1, settings.ConsecutiveStrikes)
			}
		}
	}
	if deleteCount != 2 {
		t.Fatalf("expected exactly 2 deleteAddr actions across %d ticks, got %d (actions=%v)", len(actions), deleteCount, actions)
	}
}

// =========================================================================
// findLinkLocalCIDR
// =========================================================================

func TestDhcpHealthFindLinkLocalCIDR(t *testing.T) {
	if got := findLinkLocalCIDR([]string{"192.168.1.5/24", "169.254.3.4/16"}); got != "169.254.3.4/16" {
		t.Fatalf("expected to find the link-local CIDR, got %q", got)
	}
	if got := findLinkLocalCIDR([]string{"192.168.1.5/24"}); got != "" {
		t.Fatalf("expected empty string when no link-local address is present, got %q", got)
	}
}

// =========================================================================
// GetSettings / UpdateSettings validation
// =========================================================================

func newTestDhcpHealthChecker(t *testing.T) (*DhcpHealthChecker, *db.Repository, *trackingDhcpcdManager) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)

	mockNet := kernel.NewMockNetwork()
	ifaceService := NewInterfaceService(repo, mockNet)
	dhcpcdTracker := &trackingDhcpcdManager{}
	dhcpcdService := NewDhcpcdService(repo, ifaceService, dhcpcdTracker)
	eventLog := NewEventLogService(repo)
	bus := NewNetEventBus()

	checker := NewDhcpHealthChecker(repo, ifaceService, dhcpcdService, mockNet, eventLog, bus)
	return checker, repo, dhcpcdTracker
}

func TestDhcpHealthUpdateSettings_ValidatesRanges(t *testing.T) {
	checker, _, _ := newTestDhcpHealthChecker(t)

	valid := defaultTestDhcpHealthSettings()
	if err := checker.UpdateSettings(valid); err != nil {
		t.Fatalf("valid settings must be accepted, got error: %v", err)
	}

	cases := []struct {
		name string
		mut  func(s *model.DhcpHealthSettings)
	}{
		{"interval too low", func(s *model.DhcpHealthSettings) { s.CheckIntervalSeconds = 1 }},
		{"interval too high", func(s *model.DhcpHealthSettings) { s.CheckIntervalSeconds = 999999 }},
		{"strikes too low", func(s *model.DhcpHealthSettings) { s.ConsecutiveStrikes = 0 }},
		{"strikes too high", func(s *model.DhcpHealthSettings) { s.ConsecutiveStrikes = 21 }},
		{"minRunning negative", func(s *model.DhcpHealthSettings) { s.MinRunningSeconds = -1 }},
		{"backoff negative", func(s *model.DhcpHealthSettings) { s.RestartBackoffSeconds = -1 }},
		{"maxRestarts too low", func(s *model.DhcpHealthSettings) { s.MaxRestartsBeforePause = 0 }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := defaultTestDhcpHealthSettings()
			tc.mut(&s)
			if err := checker.UpdateSettings(s); err == nil {
				t.Fatalf("expected validation error for %s", tc.name)
			}
		})
	}
}

func TestDhcpHealthGetSettings_ReturnsSeededDefaults(t *testing.T) {
	checker, _, _ := newTestDhcpHealthChecker(t)

	settings, err := checker.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}
	want := defaultTestDhcpHealthSettings()
	if *settings != want {
		t.Fatalf("seeded defaults mismatch: got %+v want %+v", *settings, want)
	}
}

// =========================================================================
// tick() light integration test
// =========================================================================

// TestDhcpHealthTick_MockModeNeverTouchesRealNetlink verifies tick() guards
// mock mode first and returns immediately without panicking or attempting any
// real kernel access — safe to run on CI with no netlink privileges.
func TestDhcpHealthTick_MockModeNeverTouchesRealNetlink(t *testing.T) {
	checker, repo, dhcpcdTracker := newTestDhcpHealthChecker(t)

	if !repo.IsMockMode() {
		t.Fatalf("test repo must be in mock mode")
	}

	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("failed to clear DB interfaces: %v", err)
	}
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-eth0",
		Name:           "eth0",
		Alias:          "eth0",
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "dhcp",
		Status:         "up",
	}); err != nil {
		t.Fatalf("failed to seed eth0: %v", err)
	}

	settings, err := checker.GetSettings()
	if err != nil {
		t.Fatalf("GetSettings failed: %v", err)
	}

	defer func() {
		if r := recover(); r != nil {
			t.Fatalf("tick() must not panic in mock mode, got: %v", r)
		}
	}()
	checker.tick(*settings)

	if len(dhcpcdTracker.snapshotRestarted()) != 0 {
		t.Fatalf("mock mode must never trigger a real dhcpcd restart, got %v", dhcpcdTracker.snapshotRestarted())
	}
}
