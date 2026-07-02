package service

import (
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type trackingFirewallManager struct {
	appliedRules            []model.PolicyRule
	appliedIfaces           []model.NetworkInterface
	appliedAddrs            []model.AddressObject
	appliedSvcs             []model.ServiceObject
	appliedDhcpServerIfaces []string
	appliedDnsServerIfaces  []string
}

func (t *trackingFirewallManager) ApplyRules(
	rules []model.PolicyRule,
	ifaces []model.NetworkInterface,
	addrs []model.AddressObject,
	svcs []model.ServiceObject,
	dhcpServerIfaces []string,
	dnsServerIfaces []string,
) error {
	t.appliedRules = rules
	t.appliedIfaces = ifaces
	t.appliedAddrs = addrs
	t.appliedSvcs = svcs
	t.appliedDhcpServerIfaces = dhcpServerIfaces
	t.appliedDnsServerIfaces = dnsServerIfaces
	return nil
}

func TestFirewallService_Policies(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	tracker := &trackingFirewallManager{}
	mockNet := kernel.NewMockNetwork()
	ifaceSvc := NewInterfaceService(repo, mockNet)
	svc := NewFirewallService(repo, tracker, ifaceSvc)

	// Test GetPolicies
	_, err = svc.GetPolicies()
	if err != nil {
		t.Fatalf("GetPolicies failed: %v", err)
	}

	// Create prerequisite address & service objects
	addrSrc := model.AddressObject{ID: "addr-src-test", Name: "SRC_TEST", Type: "subnet", Value: "10.0.0.0/24"}
	addrDst := model.AddressObject{ID: "addr-dst-test", Name: "DST_TEST", Type: "subnet", Value: "8.8.8.8/32"}
	svcObj := model.ServiceObject{ID: "svc-http-test", Name: "HTTP_TEST", Protocol: "TCP", Port: "80", Type: "custom"}

	if err := svc.CreateAddress(addrSrc); err != nil {
		t.Fatalf("CreateAddress (src) failed: %v", err)
	}
	if err := svc.CreateAddress(addrDst); err != nil {
		t.Fatalf("CreateAddress (dst) failed: %v", err)
	}
	if err := svc.CreateService(svcObj); err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}

	// Try creating a policy rule
	rule := model.PolicyRule{
		ID:           "rule-test-1",
		Name:         "Allow HTTP",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"SRC_TEST"},
		Destination:  []string{"DST_TEST"},
		Service:      []string{"HTTP_TEST"},
		Action:       "ACCEPT",
		Log:          true,
		Status:       true,
	}

	if err := svc.CreatePolicy(rule); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	// Verify it was created
	found, err := svc.GetPolicyByID("rule-test-1")
	if err != nil || found == nil {
		t.Fatalf("GetPolicyByID failed: %v", err)
	}
	if found.Name != "Allow HTTP" {
		t.Errorf("Expected policy name 'Allow HTTP', got '%s'", found.Name)
	}

	// Sync rules to kernel and verify tracker received it
	if err := svc.SyncFirewallRules(); err != nil {
		t.Fatalf("SyncFirewallRules failed: %v", err)
	}

	if len(tracker.appliedRules) == 0 {
		t.Errorf("Expected applied rules in kernel tracker to be > 0, got 0")
	}

	// Test Update
	rule.Name = "Allow HTTP Updated"
	if err := svc.UpdatePolicy(rule); err != nil {
		t.Fatalf("UpdatePolicy failed: %v", err)
	}

	updated, err := svc.GetPolicyByID("rule-test-1")
	if err != nil || updated == nil {
		t.Fatalf("GetPolicyByID failed: %v", err)
	}
	if updated.Name != "Allow HTTP Updated" {
		t.Errorf("Expected name to be updated, got '%s'", updated.Name)
	}

	// Test Toggle Status
	toggled, err := svc.TogglePolicyStatus("rule-test-1")
	if err != nil || toggled == nil {
		t.Fatalf("TogglePolicyStatus failed: %v", err)
	}
	if toggled.Status != false {
		t.Errorf("Expected status to be false after toggle, got %t", toggled.Status)
	}

	// Test Toggle Log
	toggledLog, err := svc.TogglePolicyLog("rule-test-1")
	if err != nil || toggledLog == nil {
		t.Fatalf("TogglePolicyLog failed: %v", err)
	}
	if toggledLog.Log != false {
		t.Errorf("Expected log to be false after toggle, got %t", toggledLog.Log)
	}

	// Test Delete
	if err := svc.DeletePolicy("rule-test-1"); err != nil {
		t.Fatalf("DeletePolicy failed: %v", err)
	}

	deleted, err := svc.GetPolicyByID("rule-test-1")
	if err == nil && deleted != nil {
		t.Errorf("Expected policy to be deleted, but still found it")
	}
}

func TestFirewallService_AddressObjects(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	tracker := &trackingFirewallManager{}
	mockNet := kernel.NewMockNetwork()
	ifaceSvc := NewInterfaceService(repo, mockNet)
	svc := NewFirewallService(repo, tracker, ifaceSvc)

	addr := model.AddressObject{
		ID:          "addr-test-1",
		Name:        "Local_Subnet",
		Type:        "subnet",
		Value:       "192.168.1.0/24",
		System:      false,
		RefPolicies: []string{},
	}

	if err := svc.CreateAddress(addr); err != nil {
		t.Fatalf("CreateAddress failed: %v", err)
	}

	found, err := svc.GetAddressByID("addr-test-1")
	if err != nil || found == nil {
		t.Fatalf("GetAddressByID failed: %v", err)
	}
	if found.Value != "192.168.1.0/24" {
		t.Errorf("Expected value '192.168.1.0/24', got '%s'", found.Value)
	}

	addr.Value = "192.168.2.0/24"
	if err := svc.UpdateAddress(addr); err != nil {
		t.Fatalf("UpdateAddress failed: %v", err)
	}

	updated, err := svc.GetAddressByID("addr-test-1")
	if err != nil || updated == nil {
		t.Fatalf("GetAddressByID failed: %v", err)
	}
	if updated.Value != "192.168.2.0/24" {
		t.Errorf("Expected updated value '192.168.2.0/24', got '%s'", updated.Value)
	}

	if err := svc.DeleteAddress("addr-test-1"); err != nil {
		t.Fatalf("DeleteAddress failed: %v", err)
	}

	deleted, err := svc.GetAddressByID("addr-test-1")
	if err == nil && deleted != nil {
		t.Errorf("Expected address object to be deleted, but still found it")
	}
}

func TestFirewallService_ServiceObjects(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	tracker := &trackingFirewallManager{}
	mockNet := kernel.NewMockNetwork()
	ifaceSvc := NewInterfaceService(repo, mockNet)
	svc := NewFirewallService(repo, tracker, ifaceSvc)

	srvObj := model.ServiceObject{
		ID:          "svc-test-1",
		Name:        "HTTP_Port",
		Protocol:    "TCP",
		Port:        "80",
		Type:        "custom",
		RefPolicies: []string{},
	}

	if err := svc.CreateService(srvObj); err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}

	found, err := svc.GetServiceByID("svc-test-1")
	if err != nil || found == nil {
		t.Fatalf("GetServiceByID failed: %v", err)
	}
	if found.Port != "80" {
		t.Errorf("Expected port '80', got '%s'", found.Port)
	}

	srvObj.Port = "8080"
	if err := svc.UpdateService(srvObj); err != nil {
		t.Fatalf("UpdateService failed: %v", err)
	}

	updated, err := svc.GetServiceByID("svc-test-1")
	if err != nil || updated == nil {
		t.Fatalf("GetServiceByID failed: %v", err)
	}
	if updated.Port != "8080" {
		t.Errorf("Expected updated port '8080', got '%s'", updated.Port)
	}

	if err := svc.DeleteService("svc-test-1"); err != nil {
		t.Fatalf("DeleteService failed: %v", err)
	}

	deleted, err := svc.GetServiceByID("svc-test-1")
	if err == nil && deleted != nil {
		t.Errorf("Expected service object to be deleted, but still found it")
	}
}
