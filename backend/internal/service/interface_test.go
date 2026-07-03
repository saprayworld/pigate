package service

import (
	"net"
	"strings"
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
)

// trackingNetworkManager records configuration attempts to verify behavior.
type trackingNetworkManager struct {
	configuredInterfaces []string
	toggledInterfaces    map[string]bool
	wifiConfigured       []string
	configuredMetrics    map[string]int
}

func (t *trackingNetworkManager) ToggleInterface(name string, up bool) error {
	if t.toggledInterfaces == nil {
		t.toggledInterfaces = make(map[string]bool)
	}
	t.toggledInterfaces[name] = up
	return nil
}

func (t *trackingNetworkManager) ScanWifi(name string) ([]model.WifiScanResult, error) {
	return nil, nil
}

func (t *trackingNetworkManager) ConfigureInterface(name string, mode string, ip string, netmask string, gateway string, metric int) error {
	t.configuredInterfaces = append(t.configuredInterfaces, name)
	if t.configuredMetrics == nil {
		t.configuredMetrics = make(map[string]int)
	}
	t.configuredMetrics[name] = metric
	return nil
}

func (t *trackingNetworkManager) ConfigureWifi(name string, ssid string, password string, security string, backupSSID string, backupPassword string, backupSecurity string, macMode string) error {
	t.wifiConfigured = append(t.wifiConfigured, name)
	return nil
}

func (t *trackingNetworkManager) GetWifiStatus(name string) (*model.WifiConnectionStatus, error) {
	return nil, nil
}

func TestInitApplyConfigurationAtStartup(t *testing.T) {
	// Initialize a memory database
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	// Clear default seeded interfaces to start fresh
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}

	// Disable mock mode so s.GetKernelInterfaces() calls net.Interfaces()
	repo.SetMockMode(false, false)

	// Detect any active real kernel interfaces to use for matching tests
	netIfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("Failed to list host net interfaces: %v", err)
	}

	var realIfaceName string
	for _, netIface := range netIfaces {
		// Skip loopback/down interfaces to match backend/internal/service/interface.go filtering
		if netIface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(netIface.Name, "lo") {
			continue
		}
		realIfaceName = netIface.Name
		break
	}

	// 1. Seed a non-existent interface (should be skipped)
	nonExistentName := "nonexistent-iface-xyz"
	err = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-nonexistent",
		Name:           nonExistentName,
		Alias:          "Nonexistent Interface",
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "static",
		IP:             "192.168.99.99",
		Netmask:        "24",
		Status:         "up",
	})
	if err != nil {
		t.Fatalf("Failed to seed non-existent interface: %v", err)
	}

	// 2. Seed an existing interface if found on the host (should be configured)
	if realIfaceName != "" {
		err = repo.CreateInterfaceForTest(model.NetworkInterface{
			ID:             "iface-real",
			Name:           realIfaceName,
			Alias:          "Real Interface",
			Role:           "LAN",
			Type:           "ethernet",
			AddressingMode: "static",
			IP:             "192.168.1.99",
			Netmask:        "24",
			Status:         "up",
		})
		if err != nil {
			t.Fatalf("Failed to seed real interface: %v", err)
		}
	}

	// Instantiate service with the tracking network manager
	tracker := &trackingNetworkManager{
		toggledInterfaces: make(map[string]bool),
	}
	svc := NewInterfaceService(repo, tracker)

	// Execute InitApplyConfigurationAtStartup
	err = svc.InitApplyConfigurationAtStartup()
	if err != nil {
		t.Fatalf("InitApplyConfigurationAtStartup failed: %v", err)
	}

	// Assertions
	// Verify that the nonexistent interface was NOT configured or toggled
	for _, name := range tracker.configuredInterfaces {
		if name == nonExistentName {
			t.Errorf("Expected interface %s to be skipped, but it was configured", nonExistentName)
		}
	}
	if _, toggled := tracker.toggledInterfaces[nonExistentName]; toggled {
		t.Errorf("Expected interface %s to be skipped, but it was toggled", nonExistentName)
	}

	// Verify that the real interface (if it existed) was configured and toggled
	if realIfaceName != "" {
		configured := false
		for _, name := range tracker.configuredInterfaces {
			if name == realIfaceName {
				configured = true
				break
			}
		}
		if !configured {
			t.Errorf("Expected real interface %s to be configured, but it was not", realIfaceName)
		}

		if up, toggled := tracker.toggledInterfaces[realIfaceName]; !toggled || !up {
			t.Errorf("Expected real interface %s to be toggled to up=true, but it was not", realIfaceName)
		}
	}
}

func TestInitApplyConfigurationAtStartupWithWireless(t *testing.T) {
	// Initialize a memory database
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}

	// Disable mock mode so s.GetKernelInterfaces() calls net.Interfaces()
	repo.SetMockMode(false, false)

	// Detect any active real kernel interfaces to use for matching tests
	netIfaces, err := net.Interfaces()
	if err != nil {
		t.Fatalf("Failed to list host net interfaces: %v", err)
	}

	var realIfaceName string
	for _, netIface := range netIfaces {
		if netIface.Flags&net.FlagLoopback != 0 || strings.HasPrefix(netIface.Name, "lo") {
			continue
		}
		realIfaceName = netIface.Name
		break
	}

	if realIfaceName == "" {
		t.Skip("Skipping wireless test because no real non-loopback interfaces exist on host")
	}

	// Seed real interface as a wireless interface with Wi-Fi details
	ssid := "TestWifiSSID"
	password := "TestWifiPassword"
	security := "WPA2"
	err = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:             "iface-real-wifi",
		Name:           realIfaceName,
		Alias:          "Real Wireless",
		Role:           "WAN",
		Type:           "wireless",
		AddressingMode: "dhcp",
		Status:         "up",
		WifiSSID:       &ssid,
		WifiPassword:   &password,
		WifiSecurity:   &security,
	})
	if err != nil {
		t.Fatalf("Failed to seed real wifi interface: %v", err)
	}

	tracker := &trackingNetworkManager{
		toggledInterfaces: make(map[string]bool),
	}
	svc := NewInterfaceService(repo, tracker)

	err = svc.InitApplyConfigurationAtStartup()
	if err != nil {
		t.Fatalf("InitApplyConfigurationAtStartup failed: %v", err)
	}

	// Assertions for wireless configurations
	wifiConfigured := false
	for _, name := range tracker.wifiConfigured {
		if name == realIfaceName {
			wifiConfigured = true
			break
		}
	}
	if !wifiConfigured {
		t.Errorf("Expected wifi to be configured for interface %s, but it was not", realIfaceName)
	}

	configured := false
	for _, name := range tracker.configuredInterfaces {
		if name == realIfaceName {
			configured = true
			break
		}
	}
	if !configured {
		t.Errorf("Expected interface %s to be configured, but it was not", realIfaceName)
	}
}

func intPtr(v int) *int { return &v }

// TestInterfaceMetric covers saving a metric, reading it back, range validation,
// and that a nil metric leaves the historical behavior untouched (metric 0 = "unset"
// is passed to the kernel layer, which falls back to its default).
func TestInterfaceMetric(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}

	base := model.NetworkInterface{
		ID:             "iface-metric",
		Name:           "eth-test",
		Alias:          "WAN Test",
		Role:           "WAN",
		Type:           "ethernet",
		AddressingMode: "dhcp",
		Status:         "up",
		AdminAccess:    []string{"PING"},
	}
	if err := repo.CreateInterfaceForTest(base); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	tracker := &trackingNetworkManager{}
	svc := NewInterfaceService(repo, tracker)

	// 1. Save a valid metric.
	withMetric := base
	withMetric.Metric = intPtr(100)
	if err := svc.ApplyInterfaceConfig(withMetric); err != nil {
		t.Fatalf("ApplyInterfaceConfig with metric 100 failed: %v", err)
	}
	if got := tracker.configuredMetrics["eth-test"]; got != 100 {
		t.Errorf("expected kernel to receive metric 100, got %d", got)
	}
	stored, err := repo.GetInterfaceByID("iface-metric")
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if stored.Metric == nil || *stored.Metric != 100 {
		t.Errorf("expected stored metric 100, got %v", stored.Metric)
	}

	// 2. Clearing the metric (nil) reverts to "unset" — kernel receives 0.
	cleared := base
	cleared.Metric = nil
	if err := svc.ApplyInterfaceConfig(cleared); err != nil {
		t.Fatalf("ApplyInterfaceConfig with nil metric failed: %v", err)
	}
	if got := tracker.configuredMetrics["eth-test"]; got != 0 {
		t.Errorf("expected kernel to receive metric 0 for unset, got %d", got)
	}
	stored, err = repo.GetInterfaceByID("iface-metric")
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if stored.Metric != nil {
		t.Errorf("expected stored metric to be nil after clearing, got %v", *stored.Metric)
	}

	// 3. Out-of-range metrics are rejected.
	for _, bad := range []int{0, -5, 10000} {
		invalid := base
		invalid.Metric = intPtr(bad)
		if err := svc.ApplyInterfaceConfig(invalid); err == nil {
			t.Errorf("expected error for out-of-range metric %d, got nil", bad)
		}
	}
}
