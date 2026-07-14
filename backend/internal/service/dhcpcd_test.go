package service

import (
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

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
