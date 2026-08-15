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
	appliedPortForwards     []model.PortForward
}

func (t *trackingFirewallManager) ApplyRules(
	rules []model.PolicyRule,
	ifaces []model.NetworkInterface,
	addrs []model.AddressObject,
	svcs []model.ServiceObject,
	dhcpServerIfaces []string,
	dnsServerIfaces []string,
	portForwards []model.PortForward,
) error {
	t.appliedRules = rules
	t.appliedIfaces = ifaces
	t.appliedAddrs = addrs
	t.appliedSvcs = svcs
	t.appliedDhcpServerIfaces = dhcpServerIfaces
	t.appliedDnsServerIfaces = dnsServerIfaces
	t.appliedPortForwards = portForwards
	return nil
}

// FQDNResolutions satisfies kernel.FirewallManager (docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md T-02, Caution 11) — this test
// double never resolves FQDN entries, so an empty map is sufficient.
func (t *trackingFirewallManager) FQDNResolutions() map[string][]string {
	return map[string][]string{}
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
	_, err = svc.GetPolicies("")
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

// TestFirewallService_PolicyChain covers the input/output chain plan
// (docs/ref/todo/input-output-chain-firewall-plan.md): empty chain normalizes
// to "forward", GetPolicies(chain) filters correctly, and reorder is scoped
// per chain (an id from another chain is rejected rather than silently
// stealing that chain's priority sequence — Caution 3).
func TestFirewallService_PolicyChain(t *testing.T) {
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

	addrSrc := model.AddressObject{ID: "addr-src-chain", Name: "SRC_CHAIN", Type: "subnet", Value: "10.1.0.0/24"}
	addrDst := model.AddressObject{ID: "addr-dst-chain", Name: "DST_CHAIN", Type: "subnet", Value: "10.2.0.0/24"}
	svcObj := model.ServiceObject{ID: "svc-chain", Name: "SVC_CHAIN", Protocol: "TCP", Port: "22", Type: "custom"}
	if err := svc.CreateAddress(addrSrc); err != nil {
		t.Fatalf("CreateAddress (src) failed: %v", err)
	}
	if err := svc.CreateAddress(addrDst); err != nil {
		t.Fatalf("CreateAddress (dst) failed: %v", err)
	}
	if err := svc.CreateService(svcObj); err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}

	base := model.PolicyRule{
		Source:      []string{"SRC_CHAIN"},
		Destination: []string{"DST_CHAIN"},
		Service:     []string{"SVC_CHAIN"},
		Action:      "ACCEPT",
		Status:      true,
	}

	// No chain set at all -> must normalize to forward, not error.
	forwardImplicit := base
	forwardImplicit.ID = "rule-chain-implicit-forward"
	forwardImplicit.Name = "Implicit Forward"
	if err := svc.CreatePolicy(forwardImplicit); err != nil {
		t.Fatalf("CreatePolicy (implicit forward) failed: %v", err)
	}
	got, err := svc.GetPolicyByID("rule-chain-implicit-forward")
	if err != nil || got == nil {
		t.Fatalf("GetPolicyByID (implicit forward) failed: %v", err)
	}
	if got.Chain != model.PolicyChainForward {
		t.Errorf("expected chain to normalize to %q, got %q", model.PolicyChainForward, got.Chain)
	}

	in1 := base
	in1.ID = "rule-chain-in-1"
	in1.Name = "Local-In 1"
	in1.Chain = model.PolicyChainInput
	in2 := base
	in2.ID = "rule-chain-in-2"
	in2.Name = "Local-In 2"
	in2.Chain = model.PolicyChainInput
	out1 := base
	out1.ID = "rule-chain-out-1"
	out1.Name = "Local-Out 1"
	out1.Chain = model.PolicyChainOutput

	for _, r := range []model.PolicyRule{in1, in2, out1} {
		if err := svc.CreatePolicy(r); err != nil {
			t.Fatalf("CreatePolicy (%s) failed: %v", r.Name, err)
		}
	}

	inputPolicies, err := svc.GetPolicies(model.PolicyChainInput)
	if err != nil {
		t.Fatalf("GetPolicies(input) failed: %v", err)
	}
	if len(inputPolicies) != 2 {
		t.Fatalf("expected 2 input policies, got %d", len(inputPolicies))
	}
	for _, p := range inputPolicies {
		if p.Chain != model.PolicyChainInput {
			t.Errorf("GetPolicies(input) returned a %q-chain rule: %+v", p.Chain, p)
		}
	}

	outputPolicies, err := svc.GetPolicies(model.PolicyChainOutput)
	if err != nil {
		t.Fatalf("GetPolicies(output) failed: %v", err)
	}
	if len(outputPolicies) != 1 {
		t.Fatalf("expected 1 output policy, got %d", len(outputPolicies))
	}

	// Reorder input chain: valid — both ids belong to "input".
	if err := svc.ReorderPolicies(model.PolicyChainInput, []string{"rule-chain-in-2", "rule-chain-in-1"}); err != nil {
		t.Fatalf("ReorderPolicies(input) failed: %v", err)
	}
	reordered, err := svc.GetPolicies(model.PolicyChainInput)
	if err != nil {
		t.Fatalf("GetPolicies(input) after reorder failed: %v", err)
	}
	if len(reordered) != 2 || reordered[0].ID != "rule-chain-in-2" {
		t.Fatalf("expected rule-chain-in-2 first after reorder, got %+v", reordered)
	}

	// Reorder input chain with an id that actually belongs to output — must
	// be rejected (Caution 3), not silently accepted.
	if err := svc.ReorderPolicies(model.PolicyChainInput, []string{"rule-chain-out-1", "rule-chain-in-1"}); err == nil {
		t.Fatalf("expected ReorderPolicies(input) to reject an id from chain output, got nil error")
	}

	// Update: an empty chain in the input struct must NOT move a rule to
	// forward — service.UpdatePolicy normalizes empty to forward internally,
	// but callers of the service (the API handler) are responsible for
	// resolving "" to the existing chain before calling UpdatePolicy
	// (Caution 2) — the handler-level test lives in api/handlers_test.go.
	in1.Name = "Local-In 1 Renamed"
	if err := svc.UpdatePolicy(in1); err != nil {
		t.Fatalf("UpdatePolicy (input) failed: %v", err)
	}
	updated, err := svc.GetPolicyByID("rule-chain-in-1")
	if err != nil || updated == nil {
		t.Fatalf("GetPolicyByID (rule-chain-in-1) failed: %v", err)
	}
	if updated.Chain != model.PolicyChainInput {
		t.Errorf("expected chain to stay %q, got %q", model.PolicyChainInput, updated.Chain)
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

	addr.Entries = []model.AddressEntry{{Type: "subnet", Value: "192.168.2.0/24"}}
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
