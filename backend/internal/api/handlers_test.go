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
)

func setupTestServer(t *testing.T) (http.Handler, *db.Repository) {
	// Initialize memory database
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}

	repo := db.NewRepository(sqliteDB)
	fw := kernel.NewMockFirewall()
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false)
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
	fw := kernel.NewMockFirewall()
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)

	// Server initialized with disableEdit = true
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, true)
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
	fw := kernel.NewMockFirewall()
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false)
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
	fw := kernel.NewMockFirewall()
	net := kernel.NewMockNetwork()
	rt := kernel.NewMockRouting()
	dhcp := kernel.NewMockDhcp()
	ringBuffer := logs.NewRingBuffer(50)

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false)
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
