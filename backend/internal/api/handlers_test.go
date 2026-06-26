package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
	"pigate/internal/service"
)

func setupTestServer(t *testing.T) (http.Handler, *db.Repository) {
	// Initialize memory database
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}

	repo := db.NewRepository(sqliteDB)
	fw := kernel.NewMockFirewall(true)
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)
	ifaceService := service.NewInterfaceService(repo, net)
	routingService := service.NewRoutingService(repo, rt)

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, ifaceService, routingService)
	handler := RegisterRoutes(server)

	// Add test session token to activeSessions since IsSessionValid no longer allows mock_session_id_* bypass
	AddSession("mock_session_id_test_token", "pigate")

	return handler, repo
}

func TestAPICORSHeaders(t *testing.T) {
	handler, _ := setupTestServer(t)

	req := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
	req.Header.Set("Origin", "http://localhost:5173")
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusNoContent {
		t.Errorf("Expected status code %d, got %d", http.StatusNoContent, rec.Code)
	}

	corsHeader := rec.Header().Get("Access-Control-Allow-Origin")
	if corsHeader != "http://localhost:5173" {
		t.Errorf("Expected CORS Access-Control-Allow-Origin 'http://localhost:5173', got '%s'", corsHeader)
	}
}

func TestAPIAuthenticationFlow(t *testing.T) {
	handler, _ := setupTestServer(t)

	// 1. Attempt login with wrong password
	loginPayload := model.LoginRequest{Username: "pigate", Password: "wrong_password"}
	body, _ := json.Marshal(loginPayload)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected unauthorized status 401, got %d", rec.Code)
	}

	// 2. Attempt login with correct password
	loginPayload.Password = "pigate"
	body, _ = json.Marshal(loginPayload)
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	rec = httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected ok status 200, got %d", rec.Code)
	}

	var loginRes model.LoginResponse
	json.NewDecoder(rec.Body).Decode(&loginRes)
	if loginRes.Token == "" {
		t.Fatal("Expected login to return a session token, got empty string")
	}

	// 3. Request protected resource without token (should fail)
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for missing auth token, got %d", rec.Code)
	}

	// 4. Request protected resource with valid auth header token
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for authorized request, got %d", rec.Code)
	}

	var stats model.DashboardStats
	json.NewDecoder(rec.Body).Decode(&stats)
	if stats.FirewallStatus != "Active" {
		t.Errorf("Expected stats firewallStatus 'Active', got '%s'", stats.FirewallStatus)
	}
}

func TestAddressCRUDAPI(t *testing.T) {
	handler, _ := setupTestServer(t)

	// Auth token shortcut (bypass by prepending mock token syntax)
	authToken := "mock_session_id_test_token"

	// 1. List addresses
	req := httptest.NewRequest("GET", "/api/addresses", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var list []model.AddressObject
	json.NewDecoder(rec.Body).Decode(&list)
	if len(list) != 1 || list[0].Name != "ALL" {
		t.Errorf("Expected initial address list with seeded 'ALL' object, got %v", list)
	}

	// 2. Create address
	addrInput := model.AddressObjectInput{
		Name:    "Office_Network",
		Type:    "subnet",
		Value:   "10.10.0.0/16",
		Comment: "Corporate LAN",
	}
	body, _ := json.Marshal(addrInput)
	req = httptest.NewRequest("POST", "/api/addresses", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for creating address, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var created model.AddressObject
	json.NewDecoder(rec.Body).Decode(&created)
	if created.ID == "" || created.Name != "Office_Network" {
		t.Errorf("Failed to create address correctly, got %v", created)
	}

	// 3. Update address
	addrInput.Value = "10.10.5.0/24"
	body, _ = json.Marshal(addrInput)
	req = httptest.NewRequest("PUT", "/api/addresses/"+created.ID, bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for updating address, got %d", rec.Code)
	}

	// 4. Delete system object (should fail)
	req = httptest.NewRequest("DELETE", "/api/addresses/addr-1", nil) // 'ALL' ID
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request when deleting system predefined address object, got %d", rec.Code)
	}

	// 5. Delete custom object
	req = httptest.NewRequest("DELETE", "/api/addresses/"+created.ID, nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for deleting address, got %d", rec.Code)
	}
}

func TestWifiScanAPI(t *testing.T) {
	handler, repo := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// Seed test interfaces
	macMode := "hardware"
	reconnect := false
	failover := false
	macAddr1 := "DC:A6:32:AA:BB:C1"
	_ = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:                   "iface-1",
		Name:                 "eth0",
		Alias:                "LAN_Internal",
		Role:                 "LAN",
		Type:                 "ethernet",
		AddressingMode:       "static",
		IP:                   "192.168.1.1",
		Netmask:              "24",
		MacAddress:           macAddr1,
		AdminAccess:          []string{"PING", "HTTP", "SSH"},
		Status:               "up",
		Speed:                "1000 Mbps",
		MacMode:              &macMode,
		RealMacAddress:       &macAddr1,
		RandomizeOnReconnect: &reconnect,
		FailoverEnabled:      &failover,
	})

	macAddr2 := "4E:88:2F:BC:A1:90"
	_ = repo.CreateInterfaceForTest(model.NetworkInterface{
		ID:                   "iface-2",
		Name:                 "wlan0",
		Alias:                "WAN_WiFi",
		Role:                 "WAN",
		Type:                 "wireless",
		AddressingMode:       "dhcp",
		IP:                   "10.0.0.45",
		Netmask:              "24",
		MacAddress:           macAddr2,
		AdminAccess:          []string{"PING"},
		Status:               "up",
		Speed:                "72 Mbps",
		MacMode:              &macMode,
		RealMacAddress:       &macAddr2,
		RandomizeOnReconnect: &reconnect,
		FailoverEnabled:      &failover,
	})

	// 1. Scan on ethernet interface (should fail with 400 Bad Request)
	req := httptest.NewRequest("GET", "/api/interfaces/iface-1/scan", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for scanning on ethernet interface, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// 2. Scan on wireless interface (should succeed with 200 OK)
	req = httptest.NewRequest("GET", "/api/interfaces/iface-2/scan", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for scanning on wireless interface, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestDisableEditMode(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	repo := db.NewRepository(sqliteDB)
	fw := kernel.NewMockFirewall(true)
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)
	ifaceService := service.NewInterfaceService(repo, net)
	routingService := service.NewRoutingService(repo, rt)

	// Server initialized with disableEdit = true
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, true, ifaceService, routingService)
	handler := RegisterRoutes(server)

	// Add test session token to activeSessions since IsSessionValid no longer allows mock_session_id_* bypass
	AddSession("mock_session_id_test_token", "pigate")

	// 1. Login should succeed (POST /api/auth/login)
	loginPayload := model.LoginRequest{Username: "pigate", Password: "pigate"}
	body, _ := json.Marshal(loginPayload)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for login in read-only mode, got %d", rec.Code)
	}

	authToken := "mock_session_id_test_token"

	// 2. Read operations should succeed (GET /api/interfaces)
	req = httptest.NewRequest("GET", "/api/interfaces", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for GET /api/interfaces in read-only mode, got %d", rec.Code)
	}

	// 3. Write operations should fail (POST /api/policies)
	policyInput := model.PolicyRuleInput{
		Name:         "Block_Test",
		InInterface:  "eth0",
		OutInterface: "wlan0",
		Source:       []string{"ALL"},
		Destination:  []string{"ALL"},
		Service:      []string{"ALL"},
		Action:       "DROP",
		Log:          false,
		Status:       true,
	}
	policyBody, _ := json.Marshal(policyInput)
	req = httptest.NewRequest("POST", "/api/policies", bytes.NewBuffer(policyBody))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden for POST /api/policies in read-only mode, got %d", rec.Code)
	}
}

func TestDNSConfigAPI(t *testing.T) {
	handler, _ := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// 1. Fetch default DNS Config
	req := httptest.NewRequest("GET", "/api/system/dns", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET /api/system/dns, got %d", rec.Code)
	}

	var dnsCfg model.DNSConfig
	json.NewDecoder(rec.Body).Decode(&dnsCfg)

	if dnsCfg.Mode != "static" || dnsCfg.PrimaryDNS != "1.1.1.1" || dnsCfg.SecondaryDNS != "8.8.8.8" || dnsCfg.LocalDomain != "pigate.local" {
		t.Errorf("Unexpected default DNS config: %+v", dnsCfg)
	}

	// 2. Update DNS Config
	updatePayload := model.DNSConfigInput{
		Mode:         "wan",
		PrimaryDNS:   "9.9.9.9",
		SecondaryDNS: "1.0.0.1",
		LocalDomain:  "pigate.internal",
	}
	body, _ := json.Marshal(updatePayload)
	req = httptest.NewRequest("PUT", "/api/system/dns", bytes.NewBuffer(body))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for PUT /api/system/dns, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// 3. Verify updated DNS Config
	req = httptest.NewRequest("GET", "/api/system/dns", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for GET after update, got %d", rec.Code)
	}

	var updatedCfg model.DNSConfig
	json.NewDecoder(rec.Body).Decode(&updatedCfg)

	if updatedCfg.Mode != "wan" || updatedCfg.PrimaryDNS != "9.9.9.9" || updatedCfg.SecondaryDNS != "1.0.0.1" || updatedCfg.LocalDomain != "pigate.internal" {
		t.Errorf("Updated DNS config did not match expected values: %+v", updatedCfg)
	}
}

func TestForcePasswordChangeFlow(t *testing.T) {
	// Initialize memory database
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	// Explicitly set is_initial to 1 for test
	_, err = sqliteDB.Exec("UPDATE users SET is_initial = 1 WHERE username = 'pigate'")
	if err != nil {
		t.Fatalf("Failed to set is_initial to 1: %v", err)
	}

	repo := db.NewRepository(sqliteDB)
	fw := kernel.NewMockFirewall(true)
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)
	ifaceService := service.NewInterfaceService(repo, net)
	routingService := service.NewRoutingService(repo, rt)

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, ifaceService, routingService)
	handler := RegisterRoutes(server)

	// 1. Login with correct password
	loginPayload := model.LoginRequest{Username: "pigate", Password: "pigate"}
	body, _ := json.Marshal(loginPayload)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected login status 200, got %d", rec.Code)
	}

	var loginRes model.LoginResponse
	json.NewDecoder(rec.Body).Decode(&loginRes)
	if !loginRes.MustChangePassword {
		t.Error("Expected MustChangePassword to be true")
	}

	// 2. Try fetching a protected resource like stats, should get 403 Forbidden
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden when accessing stats before changing initial password, got %d", rec.Code)
	}

	// 3. Change password via PUT /api/system/password
	changePayload := model.ChangePasswordRequest{CurrentPassword: "pigate", NewPassword: "new_secure_pass"}
	changeBody, _ := json.Marshal(changePayload)
	req = httptest.NewRequest("PUT", "/api/system/password", bytes.NewBuffer(changeBody))
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for changing password, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// 4. Try fetching stats again, should succeed now
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for stats after changing password, got %d", rec.Code)
	}
}

func TestCheckSessionAPI(t *testing.T) {
	// Setup server
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	fw := kernel.NewMockFirewall(true)
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)
	ifaceService := service.NewInterfaceService(repo, net)
	routingService := service.NewRoutingService(repo, rt)

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, ifaceService, routingService)
	handler := RegisterRoutes(server)

	// 1. Check session without token (should fail with 401)
	req := httptest.NewRequest("GET", "/api/auth/session", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for session check without token, got %d", rec.Code)
	}

	// 2. Check session with valid token (normal user)
	// Update user to not be initial
	_, _ = sqliteDB.Exec("UPDATE users SET is_initial = 0 WHERE username = 'pigate'")
	
	// Login to get token
	loginPayload := model.LoginRequest{Username: "pigate", Password: "pigate"}
	body, _ := json.Marshal(loginPayload)
	req = httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	var loginRes model.LoginResponse
	json.NewDecoder(rec.Body).Decode(&loginRes)

	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for session check with valid token, got %d", rec.Code)
	}

	var sessionRes map[string]interface{}
	json.NewDecoder(rec.Body).Decode(&sessionRes)

	if sessionRes["valid"] != true || sessionRes["username"] != "pigate" || sessionRes["mustChangePassword"] != false {
		t.Errorf("Unexpected session response for normal user: %v", sessionRes)
	}

	// 3. Check session with initial user (must change password)
	_, _ = sqliteDB.Exec("UPDATE users SET is_initial = 1 WHERE username = 'pigate'")
	
	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	req.Header.Set("Authorization", "Bearer "+loginRes.Token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for session check even with mustChangePassword active, got %d", rec.Code)
	}

	json.NewDecoder(rec.Body).Decode(&sessionRes)
	if sessionRes["valid"] != true || sessionRes["username"] != "pigate" || sessionRes["mustChangePassword"] != true {
		t.Errorf("Unexpected session response for initial user: %v", sessionRes)
	}
}

func setupTestServerWithFirewall(t *testing.T) (http.Handler, *db.Repository, *kernel.MockFirewall) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}

	repo := db.NewRepository(sqliteDB)
	fw := kernel.NewMockFirewall(true)
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)
	ifaceService := service.NewInterfaceService(repo, net)
	routingService := service.NewRoutingService(repo, rt)

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, ifaceService, routingService)
	handler := RegisterRoutes(server)

	AddSession("mock_session_id_test_token", "pigate")

	return handler, repo, fw
}

func TestInterfaceUpdateSyncsFirewall(t *testing.T) {
	handler, repo, fw := setupTestServerWithFirewall(t)
	authToken := "mock_session_id_test_token"

	// Seed test interface
	macMode := "hardware"
	reconnect := false
	failover := false
	macAddr := "DC:A6:32:AA:BB:C1"
	
	iface := model.NetworkInterface{
		ID:                   "iface-test-sync",
		Name:                 "eth-test-sync",
		Alias:                "LAN_Internal",
		Role:                 "LAN",
		Type:                 "ethernet",
		AddressingMode:       "static",
		IP:                   "192.168.1.1",
		Netmask:              "24",
		MacAddress:           macAddr,
		AdminAccess:          []string{"PING", "HTTP", "SSH"},
		Status:               "up",
		Speed:                "1000 Mbps",
		MacMode:              &macMode,
		RealMacAddress:       &macAddr,
		RandomizeOnReconnect: &reconnect,
		FailoverEnabled:      &failover,
	}
	if err := repo.CreateInterfaceForTest(iface); err != nil {
		t.Fatalf("CreateInterfaceForTest failed: %v", err)
	}

	// Reset ApplyCount (just in case)
	fw.ApplyCount = 0

	// 1. Update interface with NO changes to AdminAccess (different order)
	updatePayloadNoChange := iface
	updatePayloadNoChange.Alias = "LAN_Updated_Alias"
	updatePayloadNoChange.AdminAccess = []string{"SSH", "PING", "HTTP"} 

	bodyBytes, _ := json.Marshal(updatePayloadNoChange)
	req := httptest.NewRequest("PUT", "/api/interfaces/iface-test-sync", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	if fw.ApplyCount != 0 {
		t.Errorf("Expected firewall sync count to be 0 (no admin access change), got %d", fw.ApplyCount)
	}

	// 2. Update interface WITH changes to AdminAccess
	updatePayloadWithChange := updatePayloadNoChange
	updatePayloadWithChange.AdminAccess = []string{"PING", "HTTPS"} 

	bodyBytes2, _ := json.Marshal(updatePayloadWithChange)
	req = httptest.NewRequest("PUT", "/api/interfaces/iface-test-sync", bytes.NewBuffer(bodyBytes2))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	if fw.ApplyCount != 1 {
		t.Errorf("Expected firewall sync count to be 1 after admin access change, got %d", fw.ApplyCount)
	}
}

func TestInterfacePatchAPI(t *testing.T) {
	handler, repo := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// Seed test interface with some initial settings, including wifi SSID and password
	macMode := "hardware"
	reconnect := false
	failover := false
	macAddr := "DC:A6:32:AA:BB:C1"
	wifiSSID := "InitialSSID"
	wifiPassword := "InitialPassword"
	wifiSecurity := "WPA2"

	iface := model.NetworkInterface{
		ID:                   "iface-test-patch",
		Name:                 "wlan_patch_test",
		Alias:                "WLAN_Initial",
		Role:                 "WAN",
		Type:                 "wireless",
		AddressingMode:       "dhcp",
		IP:                   "10.0.0.99",
		Netmask:              "24",
		MacAddress:           macAddr,
		AdminAccess:          []string{"PING"},
		Status:               "up",
		Speed:                "72 Mbps",
		MacMode:              &macMode,
		RealMacAddress:       &macAddr,
		RandomizeOnReconnect: &reconnect,
		FailoverEnabled:      &failover,
		WifiSSID:             &wifiSSID,
		WifiPassword:         &wifiPassword,
		WifiSecurity:         &wifiSecurity,
	}

	if err := repo.CreateInterfaceForTest(iface); err != nil {
		t.Fatalf("CreateInterfaceForTest failed: %v", err)
	}
	if err := repo.UpdateInterface(iface); err != nil {
		t.Fatalf("UpdateInterface failed: %v", err)
	}

	// Update only SSID via PATCH, omitting the password field. The password should not be overwritten.
	patchPayload := map[string]interface{}{
		"wifiSSID": "PatchedSSID",
	}
	bodyBytes, _ := json.Marshal(patchPayload)
	req := httptest.NewRequest("PATCH", "/api/interfaces/iface-test-patch", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Verify that the response masks the password
	var responseData model.NetworkInterface
	if err := json.Unmarshal(rec.Body.Bytes(), &responseData); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if responseData.WifiPassword == nil || *responseData.WifiPassword != "••••••••" {
		t.Errorf("Expected response WifiPassword to be masked as '••••••••', got %v", responseData.WifiPassword)
	}

	// Verify database state
	updated, err := repo.GetInterfaceByID("iface-test-patch")
	if err != nil {
		t.Fatalf("GetInterfaceByID failed: %v", err)
	}

	if updated.WifiSSID == nil || *updated.WifiSSID != "PatchedSSID" {
		t.Errorf("Expected SSID to be PatchedSSID, got %v", updated.WifiSSID)
	}

	if updated.WifiPassword == nil || *updated.WifiPassword != "InitialPassword" {
		t.Errorf("Expected password to remain InitialPassword, got %v", updated.WifiPassword)
	}

	// Now try PATCH sending an empty password string. Since security is not "Open", it should also not be overwritten.
	patchPayloadEmptyPassword := map[string]interface{}{
		"wifiPassword": "",
	}
	bodyBytes, _ = json.Marshal(patchPayloadEmptyPassword)
	req = httptest.NewRequest("PATCH", "/api/interfaces/iface-test-patch", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	updated2, _ := repo.GetInterfaceByID("iface-test-patch")
	if updated2.WifiPassword == nil || *updated2.WifiPassword != "InitialPassword" {
		t.Errorf("Expected password to remain InitialPassword even when empty string is sent in PATCH, got %v", updated2.WifiPassword)
	}

	// Now try PATCH sending the masked password placeholder ('••••••••'). It should ignore it and keep the original password.
	patchPayloadMaskedPassword := map[string]interface{}{
		"wifiPassword": "••••••••",
	}
	bodyBytes, _ = json.Marshal(patchPayloadMaskedPassword)
	req = httptest.NewRequest("PATCH", "/api/interfaces/iface-test-patch", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Check DB again
	updated3, _ := repo.GetInterfaceByID("iface-test-patch")
	if updated3.WifiPassword == nil || *updated3.WifiPassword != "InitialPassword" {
		t.Errorf("Expected password to remain InitialPassword even when '••••••••' masked password is sent in PATCH, got %v", updated3.WifiPassword)
	}

	// Also check that response returned has '••••••••'
	var responseData3 model.NetworkInterface
	if err := json.Unmarshal(rec.Body.Bytes(), &responseData3); err != nil {
		t.Fatalf("Failed to parse response: %v", err)
	}
	if responseData3.WifiPassword == nil || *responseData3.WifiPassword != "••••••••" {
		t.Errorf("Expected response WifiPassword to be masked as '••••••••', got %v", responseData3.WifiPassword)
	}
}

func TestGetDataLayerAndResetAPI(t *testing.T) {
	handler, repo := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// 1. Fetch interfaces via GET /api/interfaces.
	// Since we are in mockMode, it should return mock interfaces (eth0, wlan0, eth1).
	// eth1 exists in kernel mock but NOT in DB.
	req := httptest.NewRequest("GET", "/api/interfaces", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var list []model.NetworkInterface
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("Failed to decode interfaces list: %v", err)
	}

	// Verify we have eth0, wlan0, and eth1
	var foundEth1 bool
	for _, item := range list {
		if item.Name == "eth1" {
			foundEth1 = true
			if item.Alias != "eth1" {
				t.Errorf("Expected default alias 'eth1' for eth1, got '%s'", item.Alias)
			}
		}
	}
	if !foundEth1 {
		t.Fatal("Expected to find eth1 in data layer interfaces list")
	}

	// Verify eth1 is NOT in the database yet
	dbIface, err := repo.GetInterfaceByID("iface-eth1")
	if err != nil {
		t.Fatalf("Failed to check DB: %v", err)
	}
	if dbIface != nil {
		t.Fatal("Expected eth1 to NOT exist in DB initially")
	}

	// 2. Perform a PUT request on eth1 to modify it. This should UPSERT it into the DB.
	var eth1Update model.NetworkInterface
	for _, item := range list {
		if item.Name == "eth1" {
			eth1Update = item
			break
		}
	}
	eth1Update.Alias = "Configured_Eth1"
	eth1Update.Role = "WAN"
	eth1Update.AddressingMode = "static"
	eth1Update.IP = "192.168.20.20"
	eth1Update.Netmask = "24"
	eth1Update.Gateway = "192.168.20.1"

	bodyBytes, _ := json.Marshal(eth1Update)
	req = httptest.NewRequest("PUT", "/api/interfaces/iface-eth1", bytes.NewBuffer(bodyBytes))
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for updating eth1, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Verify it was saved to the DB
	dbIface, err = repo.GetInterfaceByID("iface-eth1")
	if err != nil || dbIface == nil {
		t.Fatalf("Expected eth1 to be saved in DB, got error: %v or nil", err)
	}
	if dbIface.Alias != "Configured_Eth1" || dbIface.IP != "192.168.20.20" {
		t.Errorf("Unexpected values in DB for eth1: %+v", dbIface)
	}

	// 3. Perform a Reset request via POST /api/interfaces/iface-eth1/reset.
	// This should Flush/Delete it from DB and return kernel defaults.
	req = httptest.NewRequest("POST", "/api/interfaces/iface-eth1/reset", nil)
	req.Header.Set("Authorization", "Bearer "+authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for reset eth1, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var resetRes model.NetworkInterface
	if err := json.NewDecoder(rec.Body).Decode(&resetRes); err != nil {
		t.Fatalf("Failed to decode reset response: %v", err)
	}
	if resetRes.Alias != "eth1" || resetRes.IP != "192.168.2.100" {
		t.Errorf("Expected reset interface to return kernel defaults, got: %+v", resetRes)
	}

	// Verify it was deleted from DB
	dbIface, err = repo.GetInterfaceByID("iface-eth1")
	if err != nil {
		t.Fatalf("Failed to check DB: %v", err)
	}
	if dbIface != nil {
		t.Fatal("Expected eth1 config to be flushed/deleted from DB")
	}
}
