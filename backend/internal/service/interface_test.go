package service

import (
	"errors"
	"fmt"
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
	callOrder            []string // ordered log of "toggle-up:eth0", "configure:eth0", etc.
	createdVlans         []string // "eth0:100"
	deletedVlans         []string // "eth0.100"
	createVlanErr        error    // when set, CreateVlan returns it
}

func (t *trackingNetworkManager) ToggleInterface(name string, up bool) error {
	if t.toggledInterfaces == nil {
		t.toggledInterfaces = make(map[string]bool)
	}
	t.toggledInterfaces[name] = up
	if up {
		t.callOrder = append(t.callOrder, "toggle-up:"+name)
	} else {
		t.callOrder = append(t.callOrder, "toggle-down:"+name)
	}
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
	t.callOrder = append(t.callOrder, "configure:"+name)
	return nil
}

func (t *trackingNetworkManager) ConfigureWifi(name string, ssid string, password string, security string, backupSSID string, backupPassword string, backupSecurity string, macMode string) error {
	t.wifiConfigured = append(t.wifiConfigured, name)
	return nil
}

func (t *trackingNetworkManager) GetWifiStatus(name string) (*model.WifiConnectionStatus, error) {
	return nil, nil
}

func (t *trackingNetworkManager) CreateVlan(parent string, vlanID int) error {
	if t.createVlanErr != nil {
		return t.createVlanErr
	}
	t.createdVlans = append(t.createdVlans, fmt.Sprintf("%s:%d", parent, vlanID))
	t.callOrder = append(t.callOrder, fmt.Sprintf("create-vlan:%s.%d", parent, vlanID))
	return nil
}

func (t *trackingNetworkManager) DeleteVlan(name string) error {
	t.deletedVlans = append(t.deletedVlans, name)
	t.callOrder = append(t.callOrder, "delete-vlan:"+name)
	return nil
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

// TestSetInterfaceState verifies the configure/activate separation: the "up" leg brings
// the link up BEFORE reapplying configuration (so the static gateway route lands on an up
// link), the "down" leg only toggles the link down without reconfiguring, and status is
// persisted in both cases.
func TestSetInterfaceState(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}

	iface := model.NetworkInterface{
		ID:             "iface-set",
		Name:           "eth-set",
		Alias:          "Set Test",
		Role:           "LAN",
		Type:           "ethernet",
		AddressingMode: "static",
		IP:             "192.168.5.1",
		Netmask:        "24",
		Gateway:        "192.168.5.254",
		Status:         "down",
		AdminAccess:    []string{"PING"},
	}
	if err := repo.CreateInterfaceForTest(iface); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	// --- up leg: toggle up must happen BEFORE configure ---
	tracker := &trackingNetworkManager{}
	svc := NewInterfaceService(repo, tracker)
	if err := svc.SetInterfaceState(iface, true); err != nil {
		t.Fatalf("SetInterfaceState(up) failed: %v", err)
	}
	if len(tracker.callOrder) != 2 {
		t.Fatalf("expected exactly 2 kernel calls on up leg, got %v", tracker.callOrder)
	}
	if tracker.callOrder[0] != "toggle-up:eth-set" {
		t.Errorf("expected toggle-up first, got %q", tracker.callOrder[0])
	}
	if tracker.callOrder[1] != "configure:eth-set" {
		t.Errorf("expected configure second, got %q", tracker.callOrder[1])
	}
	if stored, err := repo.GetInterfaceByID("iface-set"); err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	} else if stored.Status != "up" {
		t.Errorf("expected status 'up' persisted after up leg, got %q", stored.Status)
	}

	// --- down leg: toggle down, NO configure ---
	tracker2 := &trackingNetworkManager{}
	svc2 := NewInterfaceService(repo, tracker2)
	if err := svc2.SetInterfaceState(iface, false); err != nil {
		t.Fatalf("SetInterfaceState(down) failed: %v", err)
	}
	if len(tracker2.configuredInterfaces) != 0 {
		t.Errorf("expected no ConfigureInterface on down leg, got %v", tracker2.configuredInterfaces)
	}
	if up, toggled := tracker2.toggledInterfaces["eth-set"]; !toggled || up {
		t.Errorf("expected interface toggled down, got toggled=%v up=%v", toggled, up)
	}
	if stored, err := repo.GetInterfaceByID("iface-set"); err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	} else if stored.Status != "down" {
		t.Errorf("expected status 'down' persisted after down leg, got %q", stored.Status)
	}
}

// TestManagedFlag verifies GetDataLayerInterface marks an interface Managed only when it
// has a config row in the database. Uses mock kernel mode for a deterministic interface list.
func TestManagedFlag(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}
	repo.SetMockMode(true, false)

	// Seed a DB row for eth0 (present in the mock kernel list) → should be managed.
	// eth1 is in the mock kernel list but has no DB row → should be unmanaged.
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

	svc := NewInterfaceService(repo, &trackingNetworkManager{})
	list, err := svc.GetDataLayerInterface()
	if err != nil {
		t.Fatalf("GetDataLayerInterface failed: %v", err)
	}

	byName := make(map[string]model.NetworkInterface)
	for _, it := range list {
		byName[it.Name] = it
	}

	if eth0, ok := byName["eth0"]; !ok {
		t.Errorf("expected eth0 to be present in data layer")
	} else if !eth0.Managed {
		t.Errorf("expected eth0 (has DB row) to be Managed=true")
	}
	if eth1, ok := byName["eth1"]; !ok {
		t.Errorf("expected eth1 to be present in data layer")
	} else if eth1.Managed {
		t.Errorf("expected eth1 (no DB row) to be Managed=false")
	}
}

// --- VLAN interface management (issue #20) ---

// newVlanTestService returns a service in mock mode with a fresh in-memory DB and a
// tracking network manager, so GetKernelInterfaces returns the mock eth0/wlan0/eth1.
func newVlanTestService(t *testing.T) (*InterfaceService, *db.Repository, *trackingNetworkManager) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })

	repo := db.NewRepository(sqliteDB)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear DB interfaces: %v", err)
	}
	repo.SetMockMode(true, false)

	tracker := &trackingNetworkManager{toggledInterfaces: make(map[string]bool)}
	return NewInterfaceService(repo, tracker), repo, tracker
}

func TestCreateVlanInterface_Success(t *testing.T) {
	svc, repo, tracker := newVlanTestService(t)

	iface, err := svc.CreateVlanInterface(model.CreateVlanInput{
		Parent:         "eth0",
		VlanID:         100,
		Alias:          "vlan100",
		Role:           "LAN",
		AddressingMode: "static",
		IP:             "192.168.100.1",
		Netmask:        "24",
	})
	if err != nil {
		t.Fatalf("CreateVlanInterface failed: %v", err)
	}
	if iface.Name != "eth0.100" {
		t.Errorf("expected name eth0.100, got %q", iface.Name)
	}
	if iface.Subtype != "vlan" || iface.Type != "ethernet" {
		t.Errorf("expected subtype=vlan type=ethernet, got subtype=%q type=%q", iface.Subtype, iface.Type)
	}
	if iface.VlanParent == nil || *iface.VlanParent != "eth0" || iface.VlanID == nil || *iface.VlanID != 100 {
		t.Errorf("expected VlanParent=eth0 VlanID=100, got %+v/%+v", iface.VlanParent, iface.VlanID)
	}

	// Kernel link created + brought up
	if len(tracker.createdVlans) != 1 || tracker.createdVlans[0] != "eth0:100" {
		t.Errorf("expected CreateVlan(eth0,100), got %v", tracker.createdVlans)
	}
	if up, ok := tracker.toggledInterfaces["eth0.100"]; !ok || !up {
		t.Errorf("expected eth0.100 toggled up")
	}

	// Persisted to DB
	stored, err := repo.GetInterfaceByID("iface-eth0.100")
	if err != nil || stored == nil {
		t.Fatalf("expected VLAN row persisted, got %v (err=%v)", stored, err)
	}
	if stored.VlanID == nil || *stored.VlanID != 100 || stored.VlanParent == nil || *stored.VlanParent != "eth0" {
		t.Errorf("persisted VLAN metadata wrong: %+v", stored)
	}
}

func TestCreateVlanInterface_Validation(t *testing.T) {
	cases := []struct {
		name  string
		input model.CreateVlanInput
		want  error
	}{
		{"vlan id too high", model.CreateVlanInput{Parent: "eth0", VlanID: 5000}, ErrVlanInvalid},
		{"vlan id zero", model.CreateVlanInput{Parent: "eth0", VlanID: 0}, ErrVlanInvalid},
		{"parent missing", model.CreateVlanInput{Parent: "eth9", VlanID: 10}, ErrVlanInvalid},
		{"parent wireless", model.CreateVlanInput{Parent: "wlan0", VlanID: 10}, ErrVlanInvalid},
		{"static without ip", model.CreateVlanInput{Parent: "eth0", VlanID: 10, AddressingMode: "static"}, ErrVlanInvalid},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, _, tracker := newVlanTestService(t)
			_, err := svc.CreateVlanInterface(tc.input)
			if !errors.Is(err, tc.want) {
				t.Errorf("expected error %v, got %v", tc.want, err)
			}
			if len(tracker.createdVlans) != 0 {
				t.Errorf("expected no kernel link created on validation failure, got %v", tracker.createdVlans)
			}
		})
	}
}

func TestCreateVlanInterface_Duplicate(t *testing.T) {
	svc, _, _ := newVlanTestService(t)
	in := model.CreateVlanInput{Parent: "eth0", VlanID: 100, AddressingMode: "dhcp"}
	if _, err := svc.CreateVlanInterface(in); err != nil {
		t.Fatalf("first create failed: %v", err)
	}
	_, err := svc.CreateVlanInterface(in)
	if !errors.Is(err, ErrVlanExists) {
		t.Errorf("expected ErrVlanExists on duplicate, got %v", err)
	}
}

func TestCreateVlanInterface_CannotStackOnVlan(t *testing.T) {
	svc, _, _ := newVlanTestService(t)
	if _, err := svc.CreateVlanInterface(model.CreateVlanInput{Parent: "eth0", VlanID: 100, AddressingMode: "dhcp"}); err != nil {
		t.Fatalf("create base vlan failed: %v", err)
	}
	// Now try to create a VLAN whose parent is the VLAN we just made.
	_, err := svc.CreateVlanInterface(model.CreateVlanInput{Parent: "eth0.100", VlanID: 200, AddressingMode: "dhcp"})
	if !errors.Is(err, ErrVlanInvalid) {
		t.Errorf("expected ErrVlanInvalid when stacking VLAN on VLAN, got %v", err)
	}
}

func TestDeleteVlanInterface(t *testing.T) {
	svc, repo, tracker := newVlanTestService(t)
	if _, err := svc.CreateVlanInterface(model.CreateVlanInput{Parent: "eth0", VlanID: 100, AddressingMode: "dhcp"}); err != nil {
		t.Fatalf("create failed: %v", err)
	}

	if err := svc.DeleteVlanInterface("iface-eth0.100"); err != nil {
		t.Fatalf("DeleteVlanInterface failed: %v", err)
	}
	if len(tracker.deletedVlans) != 1 || tracker.deletedVlans[0] != "eth0.100" {
		t.Errorf("expected DeleteVlan(eth0.100), got %v", tracker.deletedVlans)
	}
	stored, err := repo.GetInterfaceByID("iface-eth0.100")
	if err != nil {
		t.Fatalf("GetInterfaceByID error: %v", err)
	}
	if stored != nil {
		t.Errorf("expected VLAN row removed, still present: %+v", stored)
	}
}

func TestDeleteVlanInterface_RefusesNonVlan(t *testing.T) {
	svc, repo, tracker := newVlanTestService(t)
	// Seed a non-vlan interface row.
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "iface-eth0", Name: "eth0", Alias: "eth0", Role: "LAN", Type: "ethernet",
		Subtype: "device", AddressingMode: "static", IP: "192.168.1.1", Netmask: "24",
		Gateway: "", MacAddress: "aa:bb:cc:dd:ee:ff", Status: "up", Speed: "1000 Mbps",
	}); err != nil {
		t.Fatalf("seed eth0 failed: %v", err)
	}
	err := svc.DeleteVlanInterface("iface-eth0")
	if err == nil {
		t.Fatalf("expected error deleting a non-vlan interface, got nil")
	}
	if len(tracker.deletedVlans) != 0 {
		t.Errorf("expected no DeleteVlan call for non-vlan, got %v", tracker.deletedVlans)
	}
}

// TestStartupRecreatesMissingVlan verifies the boot-time recreate step (the core fix
// of issue #20): a VLAN row in the DB whose link is absent from the kernel is
// re-created via CreateVlan. Uses real (non-mock) kernel listing so the DB VLAN row
// is genuinely missing from the kernel interface list.
func TestStartupRecreatesMissingVlan(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()
	repo := db.NewRepository(sqliteDB)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("clear interfaces: %v", err)
	}
	repo.SetMockMode(false, false)

	// Seed a VLAN row for a link that does not exist in the kernel.
	parent := "eth0"
	vid := 250
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "iface-eth0.250", Name: "eth0.250", Alias: "vlan250", Role: "LAN",
		Type: "ethernet", Subtype: "vlan", AddressingMode: "dhcp", IP: "0.0.0.0",
		Netmask: "24", Gateway: "", MacAddress: "aa:bb:cc:dd:ee:ff", Status: "up",
		Speed: "1000 Mbps", VlanParent: &parent, VlanID: &vid,
	}); err != nil {
		t.Fatalf("seed vlan row: %v", err)
	}

	tracker := &trackingNetworkManager{toggledInterfaces: make(map[string]bool)}
	svc := NewInterfaceService(repo, tracker)
	if err := svc.InitApplyConfigurationAtStartup(); err != nil {
		t.Fatalf("InitApplyConfigurationAtStartup failed: %v", err)
	}

	found := false
	for _, v := range tracker.createdVlans {
		if v == "eth0:250" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected startup to recreate missing VLAN eth0.250 (CreateVlan called), got createdVlans=%v", tracker.createdVlans)
	}
}
