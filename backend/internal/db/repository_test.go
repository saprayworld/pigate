package db

import (
	"runtime"
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
	user, err := repo.GetUserByUsername("pigate")
	if err != nil {
		t.Errorf("Error getting pigate user: %v", err)
	}
	if user == nil {
		t.Errorf("Default pigate user not seeded")
	} else if user.Username != "pigate" {
		t.Errorf("Expected username 'pigate', got '%s'", user.Username)
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

func TestAddressObjectValidation(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	// Case 1: Invalid Subnet (has extra letters)
	addrBadSubnet := model.AddressObject{
		ID:    "addr-bad-sub",
		Name:  "Bad_Subnet",
		Type:  "subnet",
		Value: "192.168.1.0w/24",
	}
	if err := repo.CreateAddress(addrBadSubnet); err == nil {
		t.Error("Expected error for invalid subnet, but got nil")
	}

	// Case 2: Valid Subnet
	addrOkSubnet := model.AddressObject{
		ID:    "addr-ok-sub",
		Name:  "Ok_Subnet",
		Type:  "subnet",
		Value: "192.168.1.0/24",
	}
	if err := repo.CreateAddress(addrOkSubnet); err != nil {
		t.Errorf("Expected valid subnet creation to succeed, got: %v", err)
	}

	// Case 3: Invalid Range (wrong delimiter or format)
	addrBadRange1 := model.AddressObject{
		ID:    "addr-bad-rng1",
		Name:  "Bad_Range1",
		Type:  "range",
		Value: "10.0.0.1_10.0.0.10",
	}
	if err := repo.CreateAddress(addrBadRange1); err == nil {
		t.Error("Expected error for range without hyphen, but got nil")
	}

	// Case 4: Invalid Range (invalid IP address)
	addrBadRange2 := model.AddressObject{
		ID:    "addr-bad-rng2",
		Name:  "Bad_Range2",
		Type:  "range",
		Value: "10.0.0.999-10.0.0.10",
	}
	if err := repo.CreateAddress(addrBadRange2); err == nil {
		t.Error("Expected error for invalid start IP in range, but got nil")
	}

	// Case 5: Invalid Range (IPv4/IPv6 family mismatch)
	addrBadRange3 := model.AddressObject{
		ID:    "addr-bad-rng3",
		Name:  "Bad_Range3",
		Type:  "range",
		Value: "10.0.0.1-2001:db8::1",
	}
	if err := repo.CreateAddress(addrBadRange3); err == nil {
		t.Error("Expected error for IP version mismatch in range, but got nil")
	}

	// Case 6: Valid Range
	addrOkRange := model.AddressObject{
		ID:    "addr-ok-rng",
		Name:  "Ok_Range",
		Type:  "range",
		Value: "10.0.0.1 - 10.0.0.10",
	}
	if err := repo.CreateAddress(addrOkRange); err != nil {
		t.Errorf("Expected valid range creation to succeed, got: %v", err)
	}

	// Case 7: Invalid FQDN (has invalid characters)
	addrBadFQDN := model.AddressObject{
		ID:    "addr-bad-fqdn",
		Name:  "Bad_FQDN",
		Type:  "fqdn",
		Value: "example$.com",
	}
	if err := repo.CreateAddress(addrBadFQDN); err == nil {
		t.Error("Expected error for invalid FQDN, but got nil")
	}

	// Case 8: Valid FQDN
	addrOkFQDN := model.AddressObject{
		ID:    "addr-ok-fqdn",
		Name:  "Ok_FQDN",
		Type:  "fqdn",
		Value: "api.pigate.local",
	}
	if err := repo.CreateAddress(addrOkFQDN); err != nil {
		t.Errorf("Expected valid FQDN creation to succeed, got: %v", err)
	}
}

func TestServiceObjectValidation(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	// Case 1: Invalid port format (letters)
	svcBadPort1 := model.ServiceObject{
		ID:       "svc-bad-p1",
		Name:     "Bad_Port1",
		Protocol: "TCP",
		Port:     "8080ss",
		Type:     "custom",
	}
	if err := repo.CreateService(svcBadPort1); err == nil {
		t.Error("Expected error for port containing letters, but got nil")
	}

	// Case 2: Invalid port range (letters)
	svcBadPort2 := model.ServiceObject{
		ID:       "svc-bad-p2",
		Name:     "Bad_Port2",
		Protocol: "TCP",
		Port:     "80-88xx",
		Type:     "custom",
	}
	if err := repo.CreateService(svcBadPort2); err == nil {
		t.Error("Expected error for range containing letters, but got nil")
	}

	// Case 3: Invalid protocol
	svcBadProto := model.ServiceObject{
		ID:       "svc-bad-proto",
		Name:     "Bad_Proto",
		Protocol: "SCTP",
		Port:     "80",
		Type:     "custom",
	}
	if err := repo.CreateService(svcBadProto); err == nil {
		t.Error("Expected error for unsupported protocol, but got nil")
	}

	// Case 4: Invalid port number range (> 65535)
	svcBadPortRange1 := model.ServiceObject{
		ID:       "svc-bad-pr1",
		Name:     "Bad_PortRange1",
		Protocol: "TCP",
		Port:     "70000",
		Type:     "custom",
	}
	if err := repo.CreateService(svcBadPortRange1); err == nil {
		t.Error("Expected error for port > 65535, but got nil")
	}

	// Case 5: Invalid port range values (start > end)
	svcBadPortRange2 := model.ServiceObject{
		ID:       "svc-bad-pr2",
		Name:     "Bad_PortRange2",
		Protocol: "TCP",
		Port:     "8080-8000",
		Type:     "custom",
	}
	if err := repo.CreateService(svcBadPortRange2); err == nil {
		t.Error("Expected error for range where start > end, but got nil")
	}

	// Case 6: ICMP protocol with wrong port value
	svcBadICMP := model.ServiceObject{
		ID:       "svc-bad-icmp",
		Name:     "Bad_ICMP",
		Protocol: "ICMP",
		Port:     "80",
		Type:     "custom",
	}
	if err := repo.CreateService(svcBadICMP); err == nil {
		t.Error("Expected error for ICMP with numeric port, but got nil")
	}

	// Case 7: Valid single port
	svcOk1 := model.ServiceObject{
		ID:       "svc-ok1",
		Name:     "Ok_HTTP",
		Protocol: "TCP",
		Port:     "80",
		Type:     "custom",
	}
	if err := repo.CreateService(svcOk1); err != nil {
		t.Errorf("Expected valid HTTP service creation to succeed, got: %v", err)
	}

	// Case 8: Valid port range
	svcOk2 := model.ServiceObject{
		ID:       "svc-ok2",
		Name:     "Ok_Ephemeral",
		Protocol: "UDP",
		Port:     "32768-61000",
		Type:     "custom",
	}
	if err := repo.CreateService(svcOk2); err != nil {
		t.Errorf("Expected valid port range service creation to succeed, got: %v", err)
	}

	// Case 9: Valid ICMP
	svcOkICMP := model.ServiceObject{
		ID:       "svc-ok-icmp",
		Name:     "Ok_ICMP",
		Protocol: "ICMP",
		Port:     "-",
		Type:     "custom",
	}
	if err := repo.CreateService(svcOkICMP); err != nil {
		t.Errorf("Expected valid ICMP service creation to succeed, got: %v", err)
	}
}

func TestHexIPParserAndRouteSyncFallback(t *testing.T) {
	// Test parseHexIP
	cases := []struct {
		hexStr   string
		expected string
		err      bool
	}{
		{"0022A8C0", "192.168.34.0", false},
		{"0122A8C0", "192.168.34.1", false},
		{"00000000", "0.0.0.0", false},
		{"FFFFFFFF", "255.255.255.255", false},
		{"123", "", true},
		{"ZZZZZZZZ", "", true},
	}

	for _, tc := range cases {
		res, err := parseHexIP(tc.hexStr)
		if tc.err {
			if err == nil {
				t.Errorf("Expected error for hex %s, got nil", tc.hexStr)
			}
		} else {
			if err != nil {
				t.Errorf("Unexpected error for hex %s: %v", tc.hexStr, err)
			}
			if res != tc.expected {
				t.Errorf("Expected %s for hex %s, got %s", tc.expected, tc.hexStr, res)
			}
		}
	}

	// Test SyncRoutesFromOS fallback on non-Linux or dummy run
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()
	repo := NewRepository(db)
	repo.SetMockMode(false, true) // Enable mockFromReal sync

	// Clear interfaces first
	err = repo.ClearInterfaces()
	if err != nil {
		t.Errorf("ClearInterfaces failed: %v", err)
	}

	// Sync routes
	err = repo.SyncRoutesFromOS()
	if err != nil {
		if runtime.GOOS == "linux" {
			t.Errorf("SyncRoutesFromOS failed on Linux: %v", err)
		} else {
			t.Logf("SyncRoutesFromOS returned error on non-Linux: %v", err)
		}
	}

	// Sync DNS
	err = repo.SyncDNSFromOS()
	if err != nil {
		if runtime.GOOS == "linux" {
			t.Errorf("SyncDNSFromOS failed on Linux: %v", err)
		} else {
			t.Logf("SyncDNSFromOS returned error on non-Linux: %v", err)
		}
	}

	// Verify DNS in DB (if on Linux)
	if runtime.GOOS == "linux" {
		dns, err := repo.GetDNSConfig()
		if err != nil {
			t.Errorf("GetDNSConfig failed: %v", err)
		}
		if dns.PrimaryDNS == "" {
			t.Errorf("Expected populated PrimaryDNS, got empty")
		}
		t.Logf("DNS config after sync: Mode=%s, Primary=%s, Secondary=%s, LocalDomain=%s",
			dns.Mode, dns.PrimaryDNS, dns.SecondaryDNS, dns.LocalDomain)
	}

	// Sync routes verification can remain if needed, but we don't assert interface count
	if runtime.GOOS == "linux" {
		routes, err := repo.GetRoutes()
		if err != nil {
			t.Errorf("Failed to get routes: %v", err)
		}
		t.Logf("Found %d routes in DB after sync from OS", len(routes))
	}
}

func TestInterfaceSubtype(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()
	repo := NewRepository(db)

	// Test GetDeviceType safety
	subtype := GetDeviceType("non-existent-device-12345")
	if subtype != "unknown" {
		t.Errorf("Expected 'unknown' subtype for non-existent device, got '%s'", subtype)
	}

	// Create test interface with subtype 'veth'
	iface := model.NetworkInterface{
		ID:             "iface-test-veth",
		Name:           "veth0",
		Alias:          "VETH_Test",
		Role:           "LAN",
		Type:           "ethernet",
		Subtype:        "veth",
		AddressingMode: "static",
		IP:             "192.168.99.1",
		Netmask:        "24",
		Gateway:        "",
		MacAddress:     "00:11:22:33:44:55",
		AdminAccess:    []string{"PING"},
		Status:         "up",
		Speed:          "10 Gbps",
	}

	err = repo.CreateInterfaceForTest(iface)
	if err != nil {
		t.Fatalf("CreateInterfaceForTest failed: %v", err)
	}

	// Retrieve by ID and check
	fetched, err := repo.GetInterfaceByID("iface-test-veth")
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if fetched == nil {
		t.Fatalf("Expected to fetch interface, got nil")
	}
	if fetched.Subtype != "veth" {
		t.Errorf("Expected subtype 'veth', got '%s'", fetched.Subtype)
	}

	// Retrieve all and check
	list, err := repo.GetInterfaces()
	if err != nil {
		t.Fatalf("GetInterfaces failed: %v", err)
	}
	found := false
	for _, item := range list {
		if item.ID == "iface-test-veth" {
			found = true
			if item.Subtype != "veth" {
				t.Errorf("Expected list item subtype 'veth', got '%s'", item.Subtype)
			}
		}
	}
	if !found {
		t.Error("Did not find created interface in GetInterfaces list")
	}
}

