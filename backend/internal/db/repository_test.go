package db

import (
	"runtime"
	"strings"
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
		Nat:          true,
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
	if !fetchedRule.Nat {
		t.Errorf("Expected nat=true to round-trip via GetPolicyByID, got false")
	}

	// nat must also survive an update that flips it off, then back through GetPolicies.
	updated := *fetchedRule
	updated.Nat = false
	if err := repo.UpdatePolicy(updated); err != nil {
		t.Fatalf("Failed to update policy: %v", err)
	}
	policies, err := repo.GetPolicies()
	if err != nil {
		t.Fatalf("Failed to list policies: %v", err)
	}
	if len(policies) != 1 || policies[0].Nat {
		t.Errorf("Expected nat=false after update via GetPolicies, got %+v", policies)
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

// TestInterfaceMetricColumn verifies the metric column (added by migration/schema)
// round-trips through create/update, distinguishing "unset" (NULL) from a value,
// including metric 0 which must remain distinct from NULL.
func TestInterfaceMetricColumn(t *testing.T) {
	sqliteDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer sqliteDB.Close()
	repo := NewRepository(sqliteDB)

	metric := 100
	iface := model.NetworkInterface{
		ID:             "iface-metric-db",
		Name:           "eth-metric",
		Alias:          "WAN_Metric",
		Role:           "WAN",
		Type:           "ethernet",
		AddressingMode: "dhcp",
		IP:             "10.0.0.2",
		Netmask:        "24",
		Gateway:        "10.0.0.1",
		Metric:         &metric,
		MacAddress:     "00:11:22:33:44:66",
		AdminAccess:    []string{"PING"},
		Status:         "up",
		Speed:          "1000 Mbps",
	}
	if err := repo.CreateInterfaceForTest(iface); err != nil {
		t.Fatalf("CreateInterfaceForTest failed: %v", err)
	}

	fetched, err := repo.GetInterfaceByID("iface-metric-db")
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if fetched.Metric == nil || *fetched.Metric != 100 {
		t.Fatalf("Expected metric 100, got %v", fetched.Metric)
	}

	// Update to metric 0 — a valid Linux priority that must survive as non-NULL.
	zero := 0
	fetched.Metric = &zero
	if err := repo.UpdateInterface(*fetched); err != nil {
		t.Fatalf("UpdateInterface (metric 0) failed: %v", err)
	}
	got, err := repo.GetInterfaceByID("iface-metric-db")
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if got.Metric == nil || *got.Metric != 0 {
		t.Fatalf("Expected metric 0 to persist as non-NULL, got %v", got.Metric)
	}

	// Clearing to nil must store NULL and read back as nil.
	got.Metric = nil
	if err := repo.UpdateInterface(*got); err != nil {
		t.Fatalf("UpdateInterface (nil metric) failed: %v", err)
	}
	cleared, err := repo.GetInterfaceByID("iface-metric-db")
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if cleared.Metric != nil {
		t.Fatalf("Expected metric nil after clearing, got %d", *cleared.Metric)
	}

	// List read path must also surface the column without error.
	list, err := repo.GetInterfacesFromDB()
	if err != nil {
		t.Fatalf("GetInterfacesFromDB failed: %v", err)
	}
	found := false
	for _, item := range list {
		if item.ID == "iface-metric-db" {
			found = true
			if item.Metric != nil {
				t.Errorf("Expected nil metric in list read, got %d", *item.Metric)
			}
		}
	}
	if !found {
		t.Error("Did not find created interface in GetInterfacesFromDB list")
	}
}

// TestAliasMigrationDedup simulates upgrading a legacy database that predates the
// unique alias index: it may hold duplicate aliases (case-insensitive) and rows
// with an empty alias. Boot (InitDB) must repair them instead of failing on
// CREATE UNIQUE INDEX.
func TestAliasMigrationDedup(t *testing.T) {
	dsn := t.TempDir() + "/legacy.db"

	first, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("initial InitDB failed: %v", err)
	}
	// Recreate the legacy state: no index yet, conflicting/empty aliases present.
	if _, err := first.Exec("DROP INDEX idx_network_interfaces_alias"); err != nil {
		t.Fatalf("drop index: %v", err)
	}
	insert := `INSERT INTO network_interfaces (
		id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, mac_address, admin_access, status, speed
	) VALUES (?, ?, ?, 'LAN', 'ethernet', 'device', 'static', '10.0.0.1', '24', '', 'aa:bb:cc:dd:ee:ff', 'PING', 'up', '1000 Mbps')`
	for _, row := range [][2]string{
		{"iface-eth7", "eth7"}, {"iface-eth8", "eth8"}, // duplicate alias, different case
		{"iface-eth9", "eth9"}, {"iface-eth10", "eth10"}, // both empty
	} {
		alias := map[string]string{"eth7": "Uplink", "eth8": "uplink", "eth9": "", "eth10": ""}[row[1]]
		if _, err := first.Exec(insert, row[0], row[1], alias); err != nil {
			t.Fatalf("seed legacy row %s: %v", row[1], err)
		}
	}
	first.Close()

	// Reboot: migration must de-duplicate and recreate the index without error.
	second, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("InitDB on legacy data failed: %v", err)
	}
	defer second.Close()

	rows, err := second.Query("SELECT name, alias FROM network_interfaces")
	if err != nil {
		t.Fatalf("query aliases: %v", err)
	}
	defer rows.Close()
	seen := map[string]string{}
	for rows.Next() {
		var name, alias string
		if err := rows.Scan(&name, &alias); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if alias == "" {
			t.Errorf("interface %s still has an empty alias after migration", name)
		}
		lower := strings.ToLower(alias)
		if prev, dup := seen[lower]; dup {
			t.Errorf("aliases still duplicated after migration: %s and %s share %q", prev, name, alias)
		}
		seen[lower] = name
	}
}

// TestHTTPSAdminAccessMigration verifies that boot backfills HTTPS on exactly the
// interfaces that already allow the web UI over HTTP, so an upgraded box does not
// lock the admin out of port 443 once HTTP starts 308-redirecting to HTTPS.
func TestHTTPSAdminAccessMigration(t *testing.T) {
	dsn := t.TempDir() + "/legacy.db"

	first, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("initial InitDB failed: %v", err)
	}
	insert := `INSERT INTO network_interfaces (
		id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, mac_address, admin_access, status, speed
	) VALUES (?, ?, ?, 'LAN', 'ethernet', 'device', 'static', '10.0.0.1', '24', '', 'aa:bb:cc:dd:ee:ff', ?, 'up', '1000 Mbps')`
	rows := []struct{ id, name, alias, adminAccess string }{
		{"iface-lan", "lan0", "LAN0", "PING,HTTP,SSH"},   // has HTTP → should gain HTTPS
		{"iface-wan", "wan0", "WAN0", "PING"},            // no HTTP → untouched
		{"iface-both", "eth7", "ETH7", "HTTP,HTTPS,SSH"}, // already has HTTPS → untouched
		{"iface-lower", "eth8", "ETH8", "ping,http"},     // case-insensitive → should gain HTTPS
	}
	for _, r := range rows {
		if _, err := first.Exec(insert, r.id, r.name, r.alias, r.adminAccess); err != nil {
			t.Fatalf("seed row %s: %v", r.name, err)
		}
	}
	first.Close()

	second, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("InitDB on legacy data failed: %v", err)
	}
	defer second.Close()

	get := func(id string) string {
		var v string
		if err := second.QueryRow("SELECT admin_access FROM network_interfaces WHERE id = ?", id).Scan(&v); err != nil {
			t.Fatalf("query admin_access for %s: %v", id, err)
		}
		return v
	}

	hasToken := func(csv, token string) bool {
		for _, t := range strings.Split(csv, ",") {
			if strings.EqualFold(strings.TrimSpace(t), token) {
				return true
			}
		}
		return false
	}

	if got := get("iface-lan"); !hasToken(got, "HTTPS") {
		t.Errorf("iface-lan: expected HTTPS backfilled, got %q", got)
	}
	if got := get("iface-lower"); !hasToken(got, "HTTPS") {
		t.Errorf("iface-lower: expected HTTPS backfilled (case-insensitive), got %q", got)
	}
	// WAN with only PING must not be touched — user intentionally closed the web UI.
	if got := get("iface-wan"); hasToken(got, "HTTPS") {
		t.Errorf("iface-wan: HTTPS must NOT be added to a PING-only interface, got %q", got)
	}
	// A row that already had HTTPS must be left byte-for-byte unchanged (idempotent).
	if got := get("iface-both"); got != "HTTP,HTTPS,SSH" {
		t.Errorf("iface-both: expected unchanged 'HTTP,HTTPS,SSH', got %q", got)
	}
}
