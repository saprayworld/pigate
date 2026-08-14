package db

import (
	"database/sql"
	"errors"
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

// TestUpdateInterfaceUpsertsMissingRow is an integration-style test against
// the real sqlite driver: it verifies that calling UpdateInterface on an id
// with NO existing row inserts the row (not silently no-op / report success
// while persisting nothing), and that a second call on the now-existing row
// updates in place rather than inserting a duplicate. Note: with
// modernc.org/sqlite, RowsAffected() on a 0-row UPDATE returns (0, nil) - no
// error - so this test alone exercises only the `rows == 0` branch of the
// upsert decision, not the `rowsErr != nil` branch (the actual T-01 defect).
// See TestDecideUpsertAction below for a focused regression test of that
// branch.
func TestUpdateInterfaceUpsertsMissingRow(t *testing.T) {
	sqliteDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer sqliteDB.Close()
	repo := NewRepository(sqliteDB)

	iface := model.NetworkInterface{
		ID:             "iface-upsert-missing",
		Name:           "eth-upsert",
		Alias:          "WAN_Upsert",
		Role:           "WAN",
		Type:           "ethernet",
		AddressingMode: "dhcp",
		IP:             "10.0.1.2",
		Netmask:        "24",
		Gateway:        "10.0.1.1",
		MacAddress:     "00:11:22:33:44:77",
		AdminAccess:    []string{"PING"},
		Status:         "up",
		Speed:          "1000 Mbps",
	}

	// No CreateInterfaceForTest call — the row does not exist yet. Calling
	// UpdateInterface directly on it must upsert (INSERT fallback), not error.
	if err := repo.UpdateInterface(iface); err != nil {
		t.Fatalf("UpdateInterface on missing row failed: %v", err)
	}

	fetched, err := repo.GetInterfaceByID(iface.ID)
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if fetched == nil {
		t.Fatalf("expected row to be created by UpdateInterface, got nil")
	}
	if fetched.Alias != "WAN_Upsert" || fetched.IP != "10.0.1.2" || fetched.Gateway != "10.0.1.1" {
		t.Fatalf("created row fields mismatch: got alias=%q ip=%q gateway=%q", fetched.Alias, fetched.IP, fetched.Gateway)
	}

	// Second call on the same id, with a field changed, must update the
	// existing row in place rather than inserting a duplicate.
	fetched.Alias = "WAN_Upsert_Updated"
	fetched.IP = "10.0.1.3"
	if err := repo.UpdateInterface(*fetched); err != nil {
		t.Fatalf("UpdateInterface on existing row failed: %v", err)
	}

	updated, err := repo.GetInterfaceByID(iface.ID)
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}
	if updated == nil {
		t.Fatalf("expected row to still exist after second UpdateInterface call, got nil")
	}
	if updated.Alias != "WAN_Upsert_Updated" || updated.IP != "10.0.1.3" {
		t.Fatalf("updated row fields mismatch: got alias=%q ip=%q", updated.Alias, updated.IP)
	}

	// Confirm there is still exactly one row for this id (no duplicate insert).
	list, err := repo.GetInterfaces()
	if err != nil {
		t.Fatalf("GetInterfaces failed: %v", err)
	}
	count := 0
	for _, item := range list {
		if item.ID == iface.ID {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("expected exactly 1 row for id %q, found %d", iface.ID, count)
	}
}

// TestDecideUpsertAction is a focused regression test for the T-01 defect:
// UpdateInterface must fall back to INSERT not only when RowsAffected()
// reports 0 rows with no error, but also when RowsAffected() itself returns
// an error (some drivers cannot report affected rows). This is tested
// directly against the pure decideUpsertAction helper with a synthetic
// error, since the real modernc.org/sqlite driver cannot practically be
// forced into a RowsAffected() error.
func TestDecideUpsertAction(t *testing.T) {
	cases := []struct {
		name     string
		rows     int64
		rowsErr  error
		expected upsertAction
	}{
		{"no existing row, no error", 0, nil, upsertActionInsert},
		{"RowsAffected error must still fall back to INSERT", 0, errors.New("driver does not support RowsAffected"), upsertActionInsert},
		{"RowsAffected error with a nonzero (bogus) row count must still fall back to INSERT", 3, errors.New("driver does not support RowsAffected"), upsertActionInsert},
		{"exactly one row updated, no error", 1, nil, upsertActionUpdateOK},
		{"unexpected row count, no error", 2, nil, upsertActionUnexpected},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := decideUpsertAction(tc.rows, tc.rowsErr)
			if got != tc.expected {
				t.Fatalf("decideUpsertAction(%d, %v) = %v, want %v", tc.rows, tc.rowsErr, got, tc.expected)
			}
		})
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

// =========================================================================
// T-09: multi-value Address/Service object entries (docs/ref/todo/
// multi-value-address-service-objects-plan.md)
// =========================================================================

// TestAddressEntries_MultiCRUDAndSeqOrder covers creating/updating an
// AddressObject with several entries: the child rows must round-trip in
// insertion order (seq 1..N) and UpdateAddress must fully replace the
// previous entry set rather than append to it.
func TestAddressEntries_MultiCRUDAndSeqOrder(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	addr := model.AddressObject{
		ID:   "addr-multi",
		Name: "Multi_Subnets",
		Entries: []model.AddressEntry{
			{Type: "subnet", Value: "10.0.1.0/24"},
			{Type: "subnet", Value: "10.0.2.0/24"},
			{Type: "range", Value: "10.0.3.1-10.0.3.10"},
		},
	}
	if err := repo.CreateAddress(addr); err != nil {
		t.Fatalf("CreateAddress with multiple entries failed: %v", err)
	}

	fetched, err := repo.GetAddressByID("addr-multi")
	if err != nil || fetched == nil {
		t.Fatalf("GetAddressByID failed: %v", err)
	}
	if len(fetched.Entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %+v", len(fetched.Entries), fetched.Entries)
	}
	wantOrder := []string{"10.0.1.0/24", "10.0.2.0/24", "10.0.3.1-10.0.3.10"}
	for i, w := range wantOrder {
		if fetched.Entries[i].Value != w {
			t.Errorf("entry[%d]: expected value %q (seq order), got %q", i, w, fetched.Entries[i].Value)
		}
	}
	// Legacy Type/Value fields must mirror Entries[0] (compat).
	if fetched.Type != "subnet" || fetched.Value != "10.0.1.0/24" {
		t.Errorf("expected legacy Type/Value to mirror Entries[0], got type=%q value=%q", fetched.Type, fetched.Value)
	}

	// GetAddresses (list) must also return the same ordered entries.
	list, err := repo.GetAddresses()
	if err != nil {
		t.Fatalf("GetAddresses failed: %v", err)
	}
	var found *model.AddressObject
	for i := range list {
		if list[i].ID == "addr-multi" {
			found = &list[i]
		}
	}
	if found == nil || len(found.Entries) != 3 {
		t.Fatalf("expected addr-multi with 3 entries in GetAddresses, got %+v", found)
	}

	// Update replaces the entry set entirely (fewer entries, different order).
	fetched.Entries = []model.AddressEntry{
		{Type: "fqdn", Value: "api.pigate.local"},
		{Type: "subnet", Value: "10.0.9.0/24"},
	}
	if err := repo.UpdateAddress(*fetched); err != nil {
		t.Fatalf("UpdateAddress with replaced entries failed: %v", err)
	}
	updated, err := repo.GetAddressByID("addr-multi")
	if err != nil || updated == nil {
		t.Fatalf("GetAddressByID after update failed: %v", err)
	}
	if len(updated.Entries) != 2 {
		t.Fatalf("expected 2 entries after update (full replace, not append), got %d: %+v", len(updated.Entries), updated.Entries)
	}
	if updated.Entries[0].Value != "api.pigate.local" || updated.Entries[1].Value != "10.0.9.0/24" {
		t.Errorf("expected replaced entries in seq order, got %+v", updated.Entries)
	}
}

// TestServiceEntries_MultiCRUDAndSeqOrder mirrors
// TestAddressEntries_MultiCRUDAndSeqOrder for ServiceObject/ServiceEntry.
func TestServiceEntries_MultiCRUDAndSeqOrder(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	svc := model.ServiceObject{
		ID:   "svc-multi",
		Name: "Web_Ports",
		Type: "custom",
		Entries: []model.ServiceEntry{
			{Protocol: "TCP", Port: "80"},
			{Protocol: "TCP", Port: "443"},
		},
	}
	if err := repo.CreateService(svc); err != nil {
		t.Fatalf("CreateService with multiple entries failed: %v", err)
	}

	fetched, err := repo.GetServiceByID("svc-multi")
	if err != nil || fetched == nil {
		t.Fatalf("GetServiceByID failed: %v", err)
	}
	if len(fetched.Entries) != 2 {
		t.Fatalf("expected 2 entries, got %d: %+v", len(fetched.Entries), fetched.Entries)
	}
	if fetched.Entries[0].Port != "80" || fetched.Entries[1].Port != "443" {
		t.Errorf("expected entries in seq order 80,443, got %+v", fetched.Entries)
	}
	if fetched.Protocol != "TCP" || fetched.Port != "80" {
		t.Errorf("expected legacy Protocol/Port to mirror Entries[0], got protocol=%q port=%q", fetched.Protocol, fetched.Port)
	}

	// Update replaces the entry set.
	fetched.Entries = []model.ServiceEntry{{Protocol: "UDP", Port: "53"}}
	if err := repo.UpdateService(*fetched); err != nil {
		t.Fatalf("UpdateService with replaced entries failed: %v", err)
	}
	updated, err := repo.GetServiceByID("svc-multi")
	if err != nil || updated == nil {
		t.Fatalf("GetServiceByID after update failed: %v", err)
	}
	if len(updated.Entries) != 1 || updated.Entries[0].Protocol != "UDP" || updated.Entries[0].Port != "53" {
		t.Fatalf("expected fully replaced single entry UDP/53, got %+v", updated.Entries)
	}
}

// TestObjectEntries_RejectInvalidSets covers the reject paths enforced by
// model.ValidateAddressEntries/ValidateServiceEntries via the repository:
// empty entries, entries exceeding the SetObjectLimits cap, duplicate
// entries, and a malformed entry value.
func TestObjectEntries_RejectInvalidSets(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	// Empty entries (and empty legacy Type/Value) must be rejected.
	if err := repo.CreateAddress(model.AddressObject{ID: "addr-empty", Name: "Empty_Entries"}); err == nil {
		t.Error("expected error creating address object with no entries, got nil")
	}
	if err := repo.CreateService(model.ServiceObject{ID: "svc-empty", Name: "Empty_Entries"}); err == nil {
		t.Error("expected error creating service object with no entries, got nil")
	}

	// Duplicate entries must be rejected.
	dupAddr := model.AddressObject{
		ID:   "addr-dup",
		Name: "Dup_Entries",
		Entries: []model.AddressEntry{
			{Type: "subnet", Value: "10.0.0.0/24"},
			{Type: "subnet", Value: "10.0.0.0/24"},
		},
	}
	if err := repo.CreateAddress(dupAddr); err == nil {
		t.Error("expected error creating address object with duplicate entries, got nil")
	}
	dupSvc := model.ServiceObject{
		ID:   "svc-dup",
		Name: "Dup_Entries",
		Entries: []model.ServiceEntry{
			{Protocol: "TCP", Port: "80"},
			{Protocol: "TCP", Port: "80"},
		},
	}
	if err := repo.CreateService(dupSvc); err == nil {
		t.Error("expected error creating service object with duplicate entries, got nil")
	}

	// A malformed entry value must be rejected even when other entries in the
	// same object are valid.
	malformedAddr := model.AddressObject{
		ID:   "addr-malformed",
		Name: "Malformed_Entries",
		Entries: []model.AddressEntry{
			{Type: "subnet", Value: "10.0.0.0/24"},
			{Type: "subnet", Value: "not-a-subnet"},
		},
	}
	if err := repo.CreateAddress(malformedAddr); err == nil {
		t.Error("expected error creating address object with a malformed entry, got nil")
	}

	// Too many entries: lower the cap via SetObjectLimits, then exceed it.
	repo.SetObjectLimits(2)
	tooManyAddr := model.AddressObject{
		ID:   "addr-too-many",
		Name: "Too_Many_Entries",
		Entries: []model.AddressEntry{
			{Type: "subnet", Value: "10.0.0.0/24"},
			{Type: "subnet", Value: "10.0.1.0/24"},
			{Type: "subnet", Value: "10.0.2.0/24"},
		},
	}
	if err := repo.CreateAddress(tooManyAddr); err == nil {
		t.Error("expected error creating address object exceeding SetObjectLimits cap, got nil")
	}
	// Exactly at the cap must still succeed.
	atCapAddr := model.AddressObject{
		ID:   "addr-at-cap",
		Name: "At_Cap_Entries",
		Entries: []model.AddressEntry{
			{Type: "subnet", Value: "10.0.0.0/24"},
			{Type: "subnet", Value: "10.0.1.0/24"},
		},
	}
	if err := repo.CreateAddress(atCapAddr); err != nil {
		t.Errorf("expected address object exactly at the SetObjectLimits cap to succeed, got: %v", err)
	}

	tooManySvc := model.ServiceObject{
		ID:   "svc-too-many",
		Name: "Too_Many_Entries",
		Entries: []model.ServiceEntry{
			{Protocol: "TCP", Port: "80"},
			{Protocol: "TCP", Port: "443"},
			{Protocol: "TCP", Port: "22"},
		},
	}
	if err := repo.CreateService(tooManySvc); err == nil {
		t.Error("expected error creating service object exceeding SetObjectLimits cap, got nil")
	}
}

// TestObjectEntries_SystemLockAndDeleteWhileReferenced verifies that (1) a
// system-predefined object's entries cannot be updated, and (2) an object
// with multiple entries still cannot be deleted while referenced by a
// firewall policy — same referential-integrity rule as the single-value path,
// now exercised through a multi-entry object.
func TestObjectEntries_SystemLockAndDeleteWhileReferenced(t *testing.T) {
	db, _ := InitDB(":memory:")
	defer db.Close()
	repo := NewRepository(db)

	// System lock: the seeded "ALL" address object (addr-1) must reject an
	// update even when the update carries multiple entries.
	allAddr, err := repo.GetAddressByID("addr-1")
	if err != nil || allAddr == nil {
		t.Fatalf("failed to fetch seeded ALL address object: %v", err)
	}
	allAddr.Entries = []model.AddressEntry{
		{Type: "subnet", Value: "1.1.1.1/32"},
		{Type: "subnet", Value: "2.2.2.2/32"},
	}
	if err := repo.UpdateAddress(*allAddr); err == nil {
		t.Error("expected error updating system predefined address object with multiple entries, got nil")
	}

	// Delete-while-referenced: create a multi-entry address/service, reference
	// them from a policy, then confirm delete is blocked until the policy is
	// removed.
	addr := model.AddressObject{
		ID:   "addr-ref-multi",
		Name: "Ref_Multi",
		Entries: []model.AddressEntry{
			{Type: "subnet", Value: "10.10.0.0/24"},
			{Type: "subnet", Value: "10.10.1.0/24"},
		},
	}
	if err := repo.CreateAddress(addr); err != nil {
		t.Fatalf("CreateAddress failed: %v", err)
	}
	dst := model.AddressObject{ID: "addr-ref-dst", Name: "Ref_Dst", Type: "subnet", Value: "8.8.8.8/32"}
	if err := repo.CreateAddress(dst); err != nil {
		t.Fatalf("CreateAddress (dst) failed: %v", err)
	}
	svc := model.ServiceObject{
		ID:   "svc-ref-multi",
		Name: "Ref_Multi_Svc",
		Type: "custom",
		Entries: []model.ServiceEntry{
			{Protocol: "TCP", Port: "80"},
			{Protocol: "TCP", Port: "443"},
		},
	}
	if err := repo.CreateService(svc); err != nil {
		t.Fatalf("CreateService failed: %v", err)
	}

	rule := model.PolicyRule{
		ID:           "rule-ref-multi",
		Name:         "Ref Multi Rule",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"Ref_Multi"},
		Destination:  []string{"Ref_Dst"},
		Service:      []string{"Ref_Multi_Svc"},
		Action:       "ACCEPT",
	}
	if err := repo.CreatePolicy(rule); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	if err := repo.DeleteAddress("addr-ref-multi"); err == nil {
		t.Error("expected error deleting multi-entry address object referenced by a policy, got nil")
	}
	if err := repo.DeleteService("svc-ref-multi"); err == nil {
		t.Error("expected error deleting multi-entry service object referenced by a policy, got nil")
	}

	if err := repo.DeletePolicy("rule-ref-multi"); err != nil {
		t.Fatalf("DeletePolicy failed: %v", err)
	}
	if err := repo.DeleteAddress("addr-ref-multi"); err != nil {
		t.Errorf("expected delete to succeed after policy removal: %v", err)
	}
	if err := repo.DeleteService("svc-ref-multi"); err != nil {
		t.Errorf("expected delete to succeed after policy removal: %v", err)
	}
}

// TestObjectEntries_BackfillFromLegacyDBIsIdempotent simulates upgrading a
// pre-multi-value database: address_objects/service_objects rows exist but
// their address_object_values/service_object_ports child rows are missing
// (as if seeded before this migration existed). Rebooting (InitDB) must
// backfill exactly one seq=1 child row per parent, and running it again must
// not duplicate rows (docs/ref/todo/multi-value-address-service-objects-plan.md
// §3 item 3).
func TestObjectEntries_BackfillFromLegacyDBIsIdempotent(t *testing.T) {
	dsn := t.TempDir() + "/legacy-entries.db"

	first, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("initial InitDB failed: %v", err)
	}
	// Simulate the pre-migration state: parent rows exist (from the normal
	// seed), but their child entry rows do not.
	if _, err := first.Exec("DELETE FROM address_object_values"); err != nil {
		t.Fatalf("failed to clear address_object_values: %v", err)
	}
	if _, err := first.Exec("DELETE FROM service_object_ports"); err != nil {
		t.Fatalf("failed to clear service_object_ports: %v", err)
	}
	// Also add a legacy-style custom row with no child entries, to mirror a
	// pre-migration user database that has custom objects too.
	if _, err := first.Exec(
		"INSERT INTO address_objects (id, name, type, value, system) VALUES (?, ?, ?, ?, 0)",
		"addr-legacy-custom", "Legacy_Custom", "subnet", "172.16.0.0/24"); err != nil {
		t.Fatalf("failed to seed legacy custom address row: %v", err)
	}
	var addrParentCount, svcParentCount int
	if err := first.QueryRow("SELECT COUNT(*) FROM address_objects").Scan(&addrParentCount); err != nil {
		t.Fatalf("count address_objects: %v", err)
	}
	if err := first.QueryRow("SELECT COUNT(*) FROM service_objects").Scan(&svcParentCount); err != nil {
		t.Fatalf("count service_objects: %v", err)
	}
	first.Close()

	// Reboot: migration/backfill must run and populate exactly one seq=1 row
	// per parent.
	second, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("InitDB on legacy data (no child entries) failed: %v", err)
	}

	countRows := func(db *sql.DB, table string) int {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	if got := countRows(second, "address_object_values"); got != addrParentCount {
		t.Fatalf("expected %d backfilled address_object_values rows (1 per parent), got %d", addrParentCount, got)
	}
	if got := countRows(second, "service_object_ports"); got != svcParentCount {
		t.Fatalf("expected %d backfilled service_object_ports rows (1 per parent), got %d", svcParentCount, got)
	}

	var seq int
	if err := second.QueryRow("SELECT seq FROM address_object_values WHERE address_id = ?", "addr-legacy-custom").Scan(&seq); err != nil {
		t.Fatalf("query backfilled seq for legacy custom address: %v", err)
	}
	if seq != 1 {
		t.Errorf("expected backfilled seq=1 for the legacy custom address, got %d", seq)
	}
	second.Close()

	// Reboot again (no further mutation in between): row counts must stay
	// exactly the same — the WHERE NOT EXISTS guard must not duplicate rows
	// on a DB that already has its children backfilled.
	third, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("InitDB (second reboot) failed: %v", err)
	}
	defer third.Close()

	if got := countRows(third, "address_object_values"); got != addrParentCount {
		t.Fatalf("backfill not idempotent: expected %d address_object_values rows after a second reboot, got %d", addrParentCount, got)
	}
	if got := countRows(third, "service_object_ports"); got != svcParentCount {
		t.Fatalf("backfill not idempotent: expected %d service_object_ports rows after a second reboot, got %d", svcParentCount, got)
	}
}

// TestPolicyRuleCounters_MonitoredCRUD covers the persisted "Monitor"
// counter CRUD added for docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md T-07 (issue #141): toggling
// monitored keeps the policy_rule_counters row in lockstep, deltas
// accumulate, zero-delta flushes are a no-op, and reset zeroes the total.
func TestPolicyRuleCounters_MonitoredCRUD(t *testing.T) {
	sqliteDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer sqliteDB.Close()
	repo := NewRepository(sqliteDB)

	addr := model.AddressObject{ID: "addr-mon-src", Name: "MON_SRC", Type: "subnet", Value: "10.0.0.0/24"}
	dst := model.AddressObject{ID: "addr-mon-dst", Name: "MON_DST", Type: "subnet", Value: "8.8.8.8/32"}
	svc := model.ServiceObject{ID: "svc-mon", Name: "MON_SVC", Protocol: "UDP", Port: "53", Type: "custom"}
	if err := repo.CreateAddress(addr); err != nil {
		t.Fatalf("CreateAddress: %v", err)
	}
	if err := repo.CreateAddress(dst); err != nil {
		t.Fatalf("CreateAddress: %v", err)
	}
	if err := repo.CreateService(svc); err != nil {
		t.Fatalf("CreateService: %v", err)
	}

	rule := model.PolicyRule{
		ID: "rule-mon-1", Name: "Monitored Rule",
		Source: []string{"MON_SRC"}, Destination: []string{"MON_DST"}, Service: []string{"MON_SVC"},
		Action: "ACCEPT", Status: true,
	}
	if err := repo.CreatePolicy(rule); err != nil {
		t.Fatalf("CreatePolicy: %v", err)
	}

	// Not monitored yet: no ids, no rows.
	ids, err := repo.GetMonitoredPolicyIDs()
	if err != nil {
		t.Fatalf("GetMonitoredPolicyIDs: %v", err)
	}
	if len(ids) != 0 {
		t.Fatalf("expected no monitored ids yet, got %v", ids)
	}

	// Enable monitoring.
	if err := repo.SetPolicyMonitored("rule-mon-1", true); err != nil {
		t.Fatalf("SetPolicyMonitored(true): %v", err)
	}
	ids, err = repo.GetMonitoredPolicyIDs()
	if err != nil {
		t.Fatalf("GetMonitoredPolicyIDs: %v", err)
	}
	if !ids["rule-mon-1"] {
		t.Fatalf("expected rule-mon-1 to be monitored, got %v", ids)
	}
	counters, err := repo.GetPolicyRuleCounters()
	if err != nil {
		t.Fatalf("GetPolicyRuleCounters: %v", err)
	}
	if len(counters) != 1 || counters[0].RuleID != "rule-mon-1" || counters[0].Bytes != 0 {
		t.Fatalf("expected a single zeroed counter row, got %+v", counters)
	}
	startedAt := counters[0].StartedAt

	// Zero-delta flush must be a true no-op (Caution 6): updated_at must not
	// even change.
	if err := repo.AddPolicyRuleCounterDeltas(map[string]model.RuleCounter{"rule-mon-1": {Bytes: 0, Packets: 0}}); err != nil {
		t.Fatalf("AddPolicyRuleCounterDeltas(zero): %v", err)
	}
	after, _ := repo.GetPolicyRuleCounters()
	if after[0].UpdatedAt != counters[0].UpdatedAt {
		t.Fatalf("zero-delta flush must not write updated_at: before=%q after=%q", counters[0].UpdatedAt, after[0].UpdatedAt)
	}

	// Non-zero delta accumulates; unknown ids are silently skipped.
	if err := repo.AddPolicyRuleCounterDeltas(map[string]model.RuleCounter{
		"rule-mon-1":          {Bytes: 100, Packets: 2},
		"rule-does-not-exist": {Bytes: 999, Packets: 9},
	}); err != nil {
		t.Fatalf("AddPolicyRuleCounterDeltas: %v", err)
	}
	if err := repo.AddPolicyRuleCounterDeltas(map[string]model.RuleCounter{"rule-mon-1": {Bytes: 50, Packets: 1}}); err != nil {
		t.Fatalf("AddPolicyRuleCounterDeltas (second): %v", err)
	}
	counters, err = repo.GetPolicyRuleCounters()
	if err != nil {
		t.Fatalf("GetPolicyRuleCounters: %v", err)
	}
	if len(counters) != 1 || counters[0].Bytes != 150 || counters[0].Packets != 3 {
		t.Fatalf("expected accumulated bytes=150 packets=3, got %+v", counters)
	}
	if counters[0].StartedAt != startedAt {
		t.Fatalf("StartedAt must not change on delta accumulation: got %q, want %q", counters[0].StartedAt, startedAt)
	}

	// Reset zeroes bytes/packets and refreshes started_at.
	if err := repo.ResetPolicyRuleCounter("rule-mon-1"); err != nil {
		t.Fatalf("ResetPolicyRuleCounter: %v", err)
	}
	counters, _ = repo.GetPolicyRuleCounters()
	if counters[0].Bytes != 0 || counters[0].Packets != 0 {
		t.Fatalf("expected reset counter to be zeroed, got %+v", counters[0])
	}

	// Disable monitoring: row must be deleted.
	if err := repo.SetPolicyMonitored("rule-mon-1", false); err != nil {
		t.Fatalf("SetPolicyMonitored(false): %v", err)
	}
	counters, err = repo.GetPolicyRuleCounters()
	if err != nil {
		t.Fatalf("GetPolicyRuleCounters: %v", err)
	}
	if len(counters) != 0 {
		t.Fatalf("expected no counter rows after disabling monitor, got %+v", counters)
	}
	ids, _ = repo.GetMonitoredPolicyIDs()
	if len(ids) != 0 {
		t.Fatalf("expected no monitored ids after disabling monitor, got %v", ids)
	}

	// SetPolicyMonitored on an unknown id is an error.
	if err := repo.SetPolicyMonitored("rule-does-not-exist", true); err == nil {
		t.Fatalf("expected error for SetPolicyMonitored on unknown id")
	}

	// Deleting a monitored rule cascades the counter row.
	if err := repo.SetPolicyMonitored("rule-mon-1", true); err != nil {
		t.Fatalf("SetPolicyMonitored(true) again: %v", err)
	}
	if err := repo.DeletePolicy("rule-mon-1"); err != nil {
		t.Fatalf("DeletePolicy: %v", err)
	}
	counters, _ = repo.GetPolicyRuleCounters()
	if len(counters) != 0 {
		t.Fatalf("expected FK cascade to remove the counter row on delete, got %+v", counters)
	}
}
