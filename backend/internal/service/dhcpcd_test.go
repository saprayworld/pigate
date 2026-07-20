package service

import (
	"sync"
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// newTestDhcpcdServiceWithTracker seeds eth0 (ethernet, DHCP) and wlan0 (wireless,
// DHCP) and returns a DhcpcdService wired to a trackingDhcpcdManager, with the
// settle window shrunk from the 2s production default to a few milliseconds so
// deferred-stop tests don't have to sleep for seconds.
func newTestDhcpcdServiceWithTracker(t *testing.T) (*DhcpcdService, *trackingDhcpcdManager) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)

	mockNet := &trackingNetworkManager{}
	ifaceService := NewInterfaceService(repo, mockNet)

	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
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
		t.Fatalf("Failed to seed eth0: %v", err)
	}
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-wlan0",
		Name:           "wlan0",
		Alias:          "wlan0",
		Role:           "WAN",
		Type:           "wireless",
		AddressingMode: "dhcp",
		Status:         "up",
	}); err != nil {
		t.Fatalf("Failed to seed wlan0: %v", err)
	}

	tracker := &trackingDhcpcdManager{}
	svc := NewDhcpcdService(repo, ifaceService, tracker)
	svc.stopSettleDelay = 20 * time.Millisecond
	return svc, tracker
}

// waitPastSettle sleeps long enough for any settle timer scheduled on svc to have
// fired, so the test can assert on its (non-)effect deterministically.
func waitPastSettle(svc *DhcpcdService) {
	time.Sleep(svc.stopSettleDelay * 5)
}

// TestDhcpcdService_DeferredStop_FlapDoesNotStop covers plan requirement 1: a link
// flap (down then back up inside the settle window) must never call StopDhcpcd.
func TestDhcpcdService_DeferredStop_FlapDoesNotStop(t *testing.T) {
	svc, tracker := newTestDhcpcdServiceWithTracker(t)

	svc.HandleLinkEvent("eth0", true, false)  // up -> start
	svc.HandleLinkEvent("eth0", false, false) // down -> schedule deferred stop
	svc.HandleLinkEvent("eth0", true, false)  // back up within settle -> cancel

	waitPastSettle(svc)

	if got := tracker.snapshotStopCalls(); len(got) != 0 {
		t.Errorf("expected no StopDhcpcd calls for a flap, got %v", got)
	}
}

// TestDhcpcdService_DeferredStop_DownStopsOnce covers plan requirement 2: an
// interface that stays down past the settle window is stopped exactly once, even if
// the down event is delivered more than once (duplicate/repeat event).
func TestDhcpcdService_DeferredStop_DownStopsOnce(t *testing.T) {
	svc, tracker := newTestDhcpcdServiceWithTracker(t)

	svc.HandleLinkEvent("eth0", false, false) // down -> schedule
	svc.HandleLinkEvent("eth0", false, false) // duplicate down -> reset clock, still one pending stop

	waitPastSettle(svc)

	got := tracker.snapshotStopCalls()
	if len(got) != 1 || got[0] != "eth0" {
		t.Errorf("expected exactly one StopDhcpcd(eth0) call, got %v", got)
	}
}

// TestDhcpcdService_DeferredStop_WifiStartsImmediately covers plan requirement 4:
// deferring stop must not add any latency to the Wi-Fi "UP-not-RUNNING -> RUNNING"
// start path. StartDhcpcd must be observed synchronously, with no settle wait.
func TestDhcpcdService_DeferredStop_WifiStartsImmediately(t *testing.T) {
	svc, tracker := newTestDhcpcdServiceWithTracker(t)

	svc.HandleLinkEvent("wlan0", true, false) // up but not associated -> wait, no start
	if got := tracker.snapshotStartCalls(); len(got) != 0 {
		t.Fatalf("expected no StartDhcpcd before RUNNING, got %v", got)
	}

	svc.HandleLinkEvent("wlan0", true, true) // now associated -> start immediately

	got := tracker.snapshotStartCalls()
	if len(got) != 1 || got[0] != "wlan0" {
		t.Errorf("expected exactly one immediate StartDhcpcd(wlan0) call, got %v", got)
	}
}

// TestDhcpcdService_DeferredStop_SyncCancelsPending covers plan requirement 6: a
// sync path (SyncInterface here) must cancel a pending deferred stop scheduled by an
// earlier event, and its own (immediate) stop decision must not be duplicated by the
// stale timer once the settle window elapses.
func TestDhcpcdService_DeferredStop_SyncCancelsPending(t *testing.T) {
	svc, tracker := newTestDhcpcdServiceWithTracker(t)

	svc.HandleLinkEvent("eth0", false, false) // down -> schedule deferred stop

	// Switch eth0 to static in the DB (simulating a config Save) and sync it — this
	// mirrors the "static -> stop" transition that SyncInterface handles directly,
	// and must both cancel the pending timer and stop immediately itself.
	if err := svc.repo.UpdateInterface(model.NetworkInterface{
		ID:             "iface-eth0",
		Name:           "eth0",
		Alias:          "eth0",
		Role:           "LAN",
		AddressingMode: "static",
		IP:             "192.168.1.1",
		Netmask:        "24",
		Status:         "up",
	}); err != nil {
		t.Fatalf("Failed to switch eth0 to static: %v", err)
	}

	svc.SyncInterface("eth0")

	got := tracker.snapshotStopCalls()
	if len(got) != 1 || got[0] != "eth0" {
		t.Fatalf("expected exactly one immediate StopDhcpcd(eth0) from SyncInterface, got %v", got)
	}

	waitPastSettle(svc)

	if got := tracker.snapshotStopCalls(); len(got) != 1 {
		t.Errorf("expected the pending deferred stop to be cancelled by sync, got a second stop: %v", got)
	}
}

// TestDhcpcdService_ConcurrentEventAndSync_NoDataRace exercises the event path
// (HandleLinkEvent, driven by netlink events on its own goroutine) concurrently with
// the sync paths (SyncInterface/SyncActiveInterfaces, driven by HTTP config-save or
// restore on another goroutine) against the same interface, as they can genuinely run
// concurrently in production (Caution 3 in the plan). This does not (and cannot)
// assert any particular start/stop ordering — goroutine scheduling decides that — but
// under `go test -race` it verifies that guarding pendingStops and the
// manager.Start/StopDhcpcd calls with a single mutex across both paths introduced no
// new data race.
func TestDhcpcdService_ConcurrentEventAndSync_NoDataRace(t *testing.T) {
	svc, _ := newTestDhcpcdServiceWithTracker(t)

	const iterations = 100
	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			up := i%2 == 0
			svc.HandleLinkEvent("eth0", up, up)
		}
	}()

	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			if i%2 == 0 {
				svc.SyncInterface("eth0")
			} else {
				svc.SyncActiveInterfaces()
			}
		}
	}()

	wg.Wait()
	waitPastSettle(svc)
}

func TestDhcpcdService_HandleLinkEvent(t *testing.T) {
	// Initialize a memory database
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	// Enable mock mode so we don't call real kernel/exec commands
	repo.SetMockMode(true, false)

	// Create InterfaceService with mock network manager
	mockNet := &trackingNetworkManager{}
	ifaceService := NewInterfaceService(repo, mockNet)

	// Clear default seeded interfaces to start fresh
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}

	// Seed eth0 (Ethernet) in DB as DHCP
	err = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-eth0",
		Name:           "eth0",
		Alias:          "eth0",
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "dhcp",
		Status:         "up",
	})
	if err != nil {
		t.Fatalf("Failed to seed eth0: %v", err)
	}

	// Seed wlan0 (Wi-Fi) in DB as DHCP
	err = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-wlan0",
		Name:           "wlan0",
		Alias:          "wlan0",
		Role:           "WAN",
		Type:           "wireless",
		AddressingMode: "dhcp",
		Status:         "up",
	})
	if err != nil {
		t.Fatalf("Failed to seed wlan0: %v", err)
	}

	dhcpcdService := NewDhcpcdService(repo, ifaceService, kernel.NewMockDhcpcdManager())

	// Test 1: Ethernet interface up
	dhcpcdService.HandleLinkEvent("eth0", true, false)

	// Test 2: Ethernet interface down
	dhcpcdService.HandleLinkEvent("eth0", false, false)

	// Test 3: Wi-Fi interface up but not running
	dhcpcdService.HandleLinkEvent("wlan0", true, false)

	// Test 4: Wi-Fi interface up and running
	dhcpcdService.HandleLinkEvent("wlan0", true, true)

	// Test 5: Wi-Fi interface down
	dhcpcdService.HandleLinkEvent("wlan0", false, false)

	// Test 6: SyncActiveInterfaces in mock mode
	dhcpcdService.SyncActiveInterfaces()
}

// TestDhcpcdService_ApplyDecision covers the shared decision helper used by
// HandleLinkUpdate, SyncActiveInterfaces and SyncInterface: ethernet starts when up,
// Wi-Fi starts only once RUNNING (associated), and both stop when down.
func TestDhcpcdService_ApplyDecision(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	ifaceService := NewInterfaceService(repo, &trackingNetworkManager{})

	cases := []struct {
		label                   string
		isWifi, isUp, isRunning bool
		wantStart, wantStop     bool
	}{
		{"ethernet up -> start", false, true, false, true, false},
		{"ethernet down -> stop", false, false, false, false, true},
		{"wifi up not running -> wait", true, true, false, false, false},
		{"wifi up running -> start", true, true, true, true, false},
		{"wifi down -> stop", true, false, false, false, true},
	}

	for _, c := range cases {
		tracker := &trackingDhcpcdManager{}
		svc := NewDhcpcdService(repo, ifaceService, tracker)
		svc.applyDhcpcdDecision("test0", c.isWifi, c.isUp, c.isRunning)

		gotStart := len(tracker.startCalls) > 0
		gotStop := len(tracker.stopCalls) > 0
		if gotStart != c.wantStart || gotStop != c.wantStop {
			t.Errorf("%s: got start=%v stop=%v, want start=%v stop=%v",
				c.label, gotStart, gotStop, c.wantStart, c.wantStop)
		}
	}
}

// TestDhcpcdService_SyncInterface_StaticStopsDhcpcd verifies that syncing an interface
// whose mode is (now) static releases any running dhcpcd lease — the "static -> stop"
// transition that HandleLinkUpdate/SyncActiveInterfaces intentionally skip. stopDhcpcd
// runs even in mock mode because the mock manager is a safe no-op.
func TestDhcpcdService_SyncInterface_StaticStopsDhcpcd(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-eth0",
		Name:           "eth0",
		Alias:          "eth0",
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "static",
		IP:             "192.168.1.1",
		Netmask:        "24",
		Status:         "up",
	}); err != nil {
		t.Fatalf("Failed to seed eth0: %v", err)
	}

	ifaceService := NewInterfaceService(repo, &trackingNetworkManager{})
	tracker := &trackingDhcpcdManager{}
	svc := NewDhcpcdService(repo, ifaceService, tracker)

	svc.SyncInterface("eth0")

	if len(tracker.stopCalls) != 1 || tracker.stopCalls[0] != "eth0" {
		t.Errorf("expected stopDhcpcd(eth0) for static interface, got stop=%v", tracker.stopCalls)
	}
	if len(tracker.startCalls) != 0 {
		t.Errorf("did not expect startDhcpcd for static interface, got %v", tracker.startCalls)
	}
}

// TestDhcpcdService_SyncInterface_DhcpMockModeNoop verifies that the DHCP branch of
// SyncInterface (which must read live kernel link flags) is skipped in mock mode, so a
// dev workstation running -mock=true never touches the real kernel or dhcpcd.
func TestDhcpcdService_SyncInterface_DhcpMockModeNoop(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
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
		t.Fatalf("Failed to seed eth0: %v", err)
	}

	ifaceService := NewInterfaceService(repo, &trackingNetworkManager{})
	tracker := &trackingDhcpcdManager{}
	svc := NewDhcpcdService(repo, ifaceService, tracker)

	svc.SyncInterface("eth0")

	if len(tracker.startCalls) != 0 || len(tracker.stopCalls) != 0 {
		t.Errorf("expected no dhcpcd calls in mock mode for dhcp interface, got start=%v stop=%v",
			tracker.startCalls, tracker.stopCalls)
	}
}
