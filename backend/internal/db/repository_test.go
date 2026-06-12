package db

import (
	"testing"

	"pigate/internal/model"
)

func TestInitDBAndSeeding(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to initialize memory database: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)

	// Test default user
	user, err := repo.GetUserByUsername("admin")
	if err != nil {
		t.Errorf("Error getting admin user: %v", err)
	}
	if user == nil {
		t.Errorf("Default admin user not seeded")
	} else if user.Username != "admin" {
		t.Errorf("Expected username 'admin', got '%s'", user.Username)
	}

	// Test default address objects
	addresses, err := repo.GetAddresses()
	if err != nil {
		t.Errorf("Error getting address list: %v", err)
	}
	if len(addresses) != 1 || addresses[0].Name != "ALL" {
		t.Errorf("Expected 1 seeded address 'ALL', got %v", addresses)
	}

	// Test default service objects
	services, err := repo.GetServices()
	if err != nil {
		t.Errorf("Error getting services list: %v", err)
	}
	if len(services) < 6 {
		t.Errorf("Expected at least 6 seeded service objects, got %d", len(services))
	}
}

func TestAddressCRUDAndLocks(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	// Create address
	addr := model.AddressObject{
		ID:     "addr-custom",
		Name:   "LAN_Internal_Subnet",
		Type:   "subnet",
		Value:  "192.168.10.0/24",
		System: false,
	}
	if err := repo.CreateAddress(addr); err != nil {
		t.Fatalf("Failed to create custom address: %v", err)
	}

	// Verify creation
	fetched, err := repo.GetAddressByID("addr-custom")
	if err != nil || fetched == nil {
		t.Fatalf("Failed to fetch custom address: %v", err)
	}
	if fetched.Value != "192.168.10.0/24" {
		t.Errorf("Expected value '192.168.10.0/24', got '%s'", fetched.Value)
	}

	// Check name duplication check
	exists, err := repo.AddressNameExists("LAN_Internal_Subnet")
	if err != nil || !exists {
		t.Errorf("Expected AddressNameExists to return true, got %v, err: %v", exists, err)
	}

	// Try updating predefined object - should fail
	allAddr, _ := repo.GetAddressByID("addr-1") // seeded 'ALL'
	allAddr.Value = "1.1.1.1/32"
	if err := repo.UpdateAddress(*allAddr); err == nil {
		t.Error("Expected error when updating system predefined address object, but got nil")
	}

	// Try deleting predefined object - should fail
	if err := repo.DeleteAddress("addr-1"); err == nil {
		t.Error("Expected error when deleting system predefined address object, but got nil")
	}

	// Update custom address
	fetched.Value = "192.168.20.0/24"
	if err := repo.UpdateAddress(*fetched); err != nil {
		t.Fatalf("Failed to update custom address: %v", err)
	}

	// Delete custom address
	if err := repo.DeleteAddress("addr-custom"); err != nil {
		t.Fatalf("Failed to delete custom address: %v", err)
	}

	// Verify deleted
	fetchedDel, _ := repo.GetAddressByID("addr-custom")
	if fetchedDel != nil {
		t.Error("Address object was not deleted successfully")
	}
}

func TestFirewallPolicyAndReferentialIntegrity(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	// Create prerequisite address & service objects
	addrSrc := model.AddressObject{ID: "addr-src", Name: "SRC_TEST", Type: "subnet", Value: "10.0.0.0/24"}
	addrDst := model.AddressObject{ID: "addr-dst", Name: "DST_TEST", Type: "subnet", Value: "8.8.8.8/32"}
	svc := model.ServiceObject{ID: "svc-dns", Name: "DNS_TEST", Protocol: "UDP", Port: "53", Type: "custom"}

	_ = repo.CreateAddress(addrSrc)
	_ = repo.CreateAddress(addrDst)
	_ = repo.CreateService(svc)

	// Create Policy rule referencing them
	rule := model.PolicyRule{
		ID:           "rule-test",
		Name:         "Allow DNS",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"SRC_TEST"},
		Destination:  []string{"DST_TEST"},
		Service:      []string{"DNS_TEST"},
		Action:       "ACCEPT",
		Log:          false,
		Status:       true,
	}

	if err := repo.CreatePolicy(rule); err != nil {
		t.Fatalf("Failed to create firewall policy: %v", err)
	}

	// Verify policy creation and relations loading
	fetchedRule, err := repo.GetPolicyByID("rule-test")
	if err != nil || fetchedRule == nil {
		t.Fatalf("Failed to fetch policy rule: %v", err)
	}
	if len(fetchedRule.Source) != 1 || fetchedRule.Source[0] != "SRC_TEST" {
		t.Errorf("Expected source 'SRC_TEST', got %v", fetchedRule.Source)
	}
	if len(fetchedRule.Service) != 1 || fetchedRule.Service[0] != "DNS_TEST" {
		t.Errorf("Expected service 'DNS_TEST', got %v", fetchedRule.Service)
	}

	// Verify refPolicies listing on address & service objects
	fetchedAddr, _ := repo.GetAddressByID("addr-src")
	if len(fetchedAddr.RefPolicies) != 1 || fetchedAddr.RefPolicies[0] != "Allow DNS" {
		t.Errorf("Expected refPolicies to list 'Allow DNS', got %v", fetchedAddr.RefPolicies)
	}

	// Test referential integrity lock: cannot delete address object while referenced
	if err := repo.DeleteAddress("addr-src"); err == nil {
		t.Error("Expected error when deleting referenced address object, but got nil")
	}

	// Delete policy rule
	if err := repo.DeletePolicy("rule-test"); err != nil {
		t.Fatalf("Failed to delete policy rule: %v", err)
	}

	// Try deleting address object again - should succeed now that policy is gone
	if err := repo.DeleteAddress("addr-src"); err != nil {
		t.Errorf("Failed to delete address after policy removal: %v", err)
	}
}

func TestFirewallPolicyValidation(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	// Create valid address and service objects to satisfy foreign keys
	addrSrc := model.AddressObject{ID: "addr-src", Name: "SRC_OK", Type: "subnet", Value: "10.0.0.0/24"}
	addrDst := model.AddressObject{ID: "addr-dst", Name: "DST_OK", Type: "subnet", Value: "192.168.1.0/24"}
	svc := model.ServiceObject{ID: "svc-http", Name: "HTTP_OK", Protocol: "TCP", Port: "80", Type: "custom"}

	_ = repo.CreateAddress(addrSrc)
	_ = repo.CreateAddress(addrDst)
	_ = repo.CreateService(svc)

	// Case 1: Empty name
	ruleEmptyName := model.PolicyRule{
		ID:           "rule-empty-name",
		Name:         "   ",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"SRC_OK"},
		Destination:  []string{"DST_OK"},
		Service:      []string{"HTTP_OK"},
		Action:       "ACCEPT",
	}
	if err := repo.CreatePolicy(ruleEmptyName); err == nil || err.Error() != "policy name cannot be empty" {
		t.Errorf("Expected empty name validation error, got: %v", err)
	}

	// Case 2: Invalid Action
	ruleInvalidAction := model.PolicyRule{
		ID:           "rule-invalid-action",
		Name:         "Invalid Action Rule",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"SRC_OK"},
		Destination:  []string{"DST_OK"},
		Service:      []string{"HTTP_OK"},
		Action:       "REJECT",
	}
	if err := repo.CreatePolicy(ruleInvalidAction); err == nil || err.Error() != "policy action must be ACCEPT or DROP" {
		t.Errorf("Expected invalid action validation error, got: %v", err)
	}

	// Case 3: Empty Source
	ruleEmptySource := model.PolicyRule{
		ID:           "rule-empty-src",
		Name:         "Empty Src Rule",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{},
		Destination:  []string{"DST_OK"},
		Service:      []string{"HTTP_OK"},
		Action:       "ACCEPT",
	}
	if err := repo.CreatePolicy(ruleEmptySource); err == nil || err.Error() != "policy must have at least one source address object" {
		t.Errorf("Expected empty source validation error, got: %v", err)
	}

	// Case 4: Non-existent Source
	ruleBadSource := model.PolicyRule{
		ID:           "rule-bad-src",
		Name:         "Bad Src Rule",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"NON_EXISTENT_SRC"},
		Destination:  []string{"DST_OK"},
		Service:      []string{"HTTP_OK"},
		Action:       "ACCEPT",
	}
	if err := repo.CreatePolicy(ruleBadSource); err == nil || err.Error() != `source address object "NON_EXISTENT_SRC" does not exist` {
		t.Errorf("Expected bad source validation error, got: %v", err)
	}

	// Case 5: Non-existent Destination
	ruleBadDest := model.PolicyRule{
		ID:           "rule-bad-dest",
		Name:         "Bad Dest Rule",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"SRC_OK"},
		Destination:  []string{"NON_EXISTENT_DST"},
		Service:      []string{"HTTP_OK"},
		Action:       "ACCEPT",
	}
	if err := repo.CreatePolicy(ruleBadDest); err == nil || err.Error() != `destination address object "NON_EXISTENT_DST" does not exist` {
		t.Errorf("Expected bad destination validation error, got: %v", err)
	}

	// Case 6: Non-existent Service
	ruleBadSvc := model.PolicyRule{
		ID:           "rule-bad-svc",
		Name:         "Bad Svc Rule",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"SRC_OK"},
		Destination:  []string{"DST_OK"},
		Service:      []string{"NON_EXISTENT_SVC"},
		Action:       "ACCEPT",
	}
	if err := repo.CreatePolicy(ruleBadSvc); err == nil || err.Error() != `service object "NON_EXISTENT_SVC" does not exist` {
		t.Errorf("Expected bad service validation error, got: %v", err)
	}

	// Verify that none of the invalid rules are actually present in the firewall_policies table
	var count int
	_ = db.QueryRow("SELECT COUNT(*) FROM firewall_policies WHERE id IN ('rule-empty-name', 'rule-invalid-action', 'rule-empty-src', 'rule-bad-src', 'rule-bad-dest', 'rule-bad-svc')").Scan(&count)
	if count > 0 {
		t.Errorf("Expected 0 invalid rules to be saved, found %d in database", count)
	}

	// Case 7: Valid Rule
	ruleOk := model.PolicyRule{
		ID:           "rule-ok",
		Name:         "Valid Rule",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"SRC_OK"},
		Destination:  []string{"DST_OK"},
		Service:      []string{"HTTP_OK"},
		Action:       "ACCEPT",
	}
	if err := repo.CreatePolicy(ruleOk); err != nil {
		t.Fatalf("Expected valid policy creation to succeed, got: %v", err)
	}

	// Fetch and confirm
	fetched, err := repo.GetPolicyByID("rule-ok")
	if err != nil || fetched == nil {
		t.Fatalf("Failed to fetch rule-ok: %v", err)
	}
	if fetched.Name != "Valid Rule" {
		t.Errorf("Expected rule name 'Valid Rule', got '%s'", fetched.Name)
	}
}
