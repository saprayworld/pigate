package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
	"pigate/internal/service"
)

func setupTestServer(t *testing.T) (http.Handler, *db.Repository) {
	return setupTestServerWithCORS(t, false)
}

func setupTestServerWithCORS(t *testing.T, allowDevCORS bool) (http.Handler, *db.Repository) {
	server, repo := buildTestServer(t, allowDevCORS)
	handler := RegisterRoutes(server)

	// Add test session token to activeSessions since IsSessionValid no longer allows mock_session_id_* bypass
	AddSession("mock_session_id_test_token", "pigate")

	return handler, repo
}

// buildTestServer constructs a *Server backed by mock kernels and an in-memory
// DB, returning it (and the repo) so tests that need the server internals — e.g.
// flushing the event log to assert audit trails — can reach them.
func buildTestServer(t *testing.T, allowDevCORS bool) (*Server, *db.Repository) {
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
	fwService := service.NewFirewallService(repo, fw, ifaceService)
	dns := kernel.NewDNSManager(true)
	dnsService := service.NewDNSService(repo, dns)
	qos := kernel.NewMockQos()
	qosService := service.NewQosService(repo, qos)
	dhcpServerService := service.NewDhcpServerService(repo, dhcp)
	dnsServer := kernel.NewMockDNSServerManager()
	dnsServerService := service.NewDNSServerService(repo, dnsServer, dnsService)
	hostnameMgr := kernel.NewMockHostnameManager()
	dhcpcdMgr := kernel.NewMockDhcpcdManager()
	hostnameService := service.NewHostnameService(repo, hostnameMgr, dhcpcdMgr, ifaceService)
	timeService := service.NewTimeService(repo, kernel.NewMockTimeManager())
	wifiPresetService := service.NewWifiPresetService(repo, ifaceService)

	testHealthChecker := service.NewDhcpHealthChecker(repo, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), net, service.NewEventLogService(repo), service.NewNetEventBus())
	systemServiceSvc := service.NewSystemServiceService(kernel.NewMockSystemServiceManager(), repo)
	trafficStatsService := service.NewTrafficStatsService(kernel.NewMockTrafficAccounting(nil), repo, dhcp, kernel.NewMockSystemStats(), 0, 0, 0)
	statisticsService := service.NewStatisticsService(trafficStatsService, repo, dhcp, 2400, 200, 500, 300) // dns-stats-max-pairs / dns-stats-max-clients defaults; deny-stats defaults
	// Mirrors cmd/pigate/main.go's SetLogBuffer wiring (docs/ref/todo/
	// firewall-log-buffer-capacity-plan.md T-03/T-05, issue #134) so the
	// firewall.logBuffer ring/usage endpoint tests exercise the real wired
	// path, not just the nil-logBuffer degrade path.
	statisticsService.SetLogBuffer(ringBuffer)
	// Mirrors cmd/pigate/main.go's blocklist service wiring (docs/ref/todo/
	// dns-blocklist-import-plan.md T-08) so tests exercising
	// /api/dns/blocklists* hit the real service instead of the nil-service
	// 503 degrade path.
	dnsBlocklistService := service.NewDNSBlocklistService(repo, dnsServer)
	if err := dnsBlocklistService.Load(); err != nil {
		t.Fatalf("Failed to load DNS blocklist manifest: %v", err)
	}
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, allowDevCORS, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), routingService, fwService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, service.NewUserService(repo), nil, service.NewSystemStatusService(kernel.NewMockSystemStats(), repo, hostnameService, timeService, "test"), service.NewPowerService(kernel.NewMockPowerManager()), service.NewEventLogService(repo), testHealthChecker, wifiPresetService, systemServiceSvc, nil, trafficStatsService, statisticsService, service.NewIPInfoService(true, service.NewMockIPInfoProvider()), dnsBlocklistService) // ipinfo enabled+mock provider for handler tests

	return server, repo
}

// addSessionCookie attaches the session token via the pigate_session cookie,
// the single supported auth channel (Authorization: Bearer was removed).
func addSessionCookie(req *http.Request, token string) {
	req.AddCookie(&http.Cookie{Name: SessionKey, Value: token})
}

// sessionCookieFromRec extracts the pigate_session token from a login response's
// Set-Cookie, mirroring how a browser obtains the session (cookie-only auth).
func sessionCookieFromRec(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	for _, c := range rec.Result().Cookies() {
		if c.Name == SessionKey {
			return c.Value
		}
	}
	t.Fatal("expected login response to set the pigate_session cookie, none found")
	return ""
}

// TestAPICORSHeaders verifies the dev-origin echo is gated behind allowDevCORS.
// With the flag OFF (production default) no Access-Control-Allow-Origin is echoed
// even for a known dev origin; with it ON the origin is echoed. The preflight
// still returns 204 either way.
func TestAPICORSHeaders(t *testing.T) {
	t.Run("gate off — no dev origin echoed", func(t *testing.T) {
		handler, _ := setupTestServerWithCORS(t, false)

		req := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("Expected status code %d, got %d", http.StatusNoContent, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "" {
			t.Errorf("Expected no Access-Control-Allow-Origin when gate is off, got '%s'", got)
		}
	})

	t.Run("gate on — dev origin echoed", func(t *testing.T) {
		handler, _ := setupTestServerWithCORS(t, true)

		req := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
		req.Header.Set("Origin", "http://localhost:5173")
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		if rec.Code != http.StatusNoContent {
			t.Errorf("Expected status code %d, got %d", http.StatusNoContent, rec.Code)
		}
		if got := rec.Header().Get("Access-Control-Allow-Origin"); got != "http://localhost:5173" {
			t.Errorf("Expected Access-Control-Allow-Origin 'http://localhost:5173', got '%s'", got)
		}
		if got := rec.Header().Get("Access-Control-Allow-Credentials"); got != "true" {
			t.Errorf("Expected Access-Control-Allow-Credentials 'true', got '%s'", got)
		}
	})
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

	// The session token must arrive only via the HttpOnly Set-Cookie, never in
	// the JSON body (cookie-only auth).
	var rawBody map[string]json.RawMessage
	json.Unmarshal(rec.Body.Bytes(), &rawBody)
	if _, hasToken := rawBody["token"]; hasToken {
		t.Error("login response body must not contain a token field (cookie-only auth)")
	}
	authToken := sessionCookieFromRec(t, rec)
	if authToken == "" {
		t.Fatal("Expected login to set a non-empty session cookie")
	}

	// 3. Request protected resource without token (should fail)
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected 401 Unauthorized for missing auth token, got %d", rec.Code)
	}

	// 4. Request protected resource with the valid session cookie
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for updating address, got %d", rec.Code)
	}

	// 4. Delete system object (should fail)
	req = httptest.NewRequest("DELETE", "/api/addresses/addr-1", nil) // 'ALL' ID
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request when deleting system predefined address object, got %d", rec.Code)
	}

	// 5. Delete custom object
	req = httptest.NewRequest("DELETE", "/api/addresses/"+created.ID, nil)
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for deleting address, got %d", rec.Code)
	}
}

// TestAddressCRUDAPI_EntriesBody covers T-09's API scope: creating/updating
// an Address Object via the new "entries" body (multiple entries), and the
// 400 path when neither entries nor legacy type/value is supplied
// (docs/ref/todo/multi-value-address-service-objects-plan.md T-09).
func TestAddressCRUDAPI_EntriesBody(t *testing.T) {
	handler, _ := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// 1. Create with entries body (multiple entries).
	addrInput := model.AddressObjectInput{
		Name: "Multi_Entries_Addr",
		Entries: []model.AddressEntry{
			{Type: "subnet", Value: "10.20.0.0/24"},
			{Type: "subnet", Value: "10.20.1.0/24"},
		},
	}
	body, _ := json.Marshal(addrInput)
	req := httptest.NewRequest("POST", "/api/addresses", bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for creating address with entries body, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var created model.AddressObject
	json.NewDecoder(rec.Body).Decode(&created)
	if len(created.Entries) != 2 {
		t.Fatalf("Expected 2 entries in create response, got %d: %+v", len(created.Entries), created.Entries)
	}
	// Legacy Type/Value must mirror Entries[0] for old clients.
	if created.Type != "subnet" || created.Value != "10.20.0.0/24" {
		t.Errorf("Expected legacy type/value to mirror Entries[0], got type=%q value=%q", created.Type, created.Value)
	}

	// 2. GET by list must also return the full entries.
	req = httptest.NewRequest("GET", "/api/addresses", nil)
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var list []model.AddressObject
	json.NewDecoder(rec.Body).Decode(&list)
	var found *model.AddressObject
	for i := range list {
		if list[i].ID == created.ID {
			found = &list[i]
		}
	}
	if found == nil || len(found.Entries) != 2 {
		t.Fatalf("Expected created address with 2 entries in list, got %+v", found)
	}

	// 3. Update with a replaced entries body (different count).
	updateInput := model.AddressObjectInput{
		Name: "Multi_Entries_Addr",
		Entries: []model.AddressEntry{
			{Type: "fqdn", Value: "svc.pigate.local"},
		},
	}
	body, _ = json.Marshal(updateInput)
	req = httptest.NewRequest("PUT", "/api/addresses/"+created.ID, bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for updating address with entries body, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var updated model.AddressObject
	json.NewDecoder(rec.Body).Decode(&updated)
	if len(updated.Entries) != 1 || updated.Entries[0].Value != "svc.pigate.local" {
		t.Fatalf("Expected update to fully replace entries, got %+v", updated.Entries)
	}

	// 4. 400 when body has neither entries nor legacy type/value.
	emptyInput := model.AddressObjectInput{Name: "No_Entries_Addr"}
	body, _ = json.Marshal(emptyInput)
	req = httptest.NewRequest("POST", "/api/addresses", bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request creating an address with no entries/legacy fields, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// 5. 400 on malformed request payload (unparsable JSON).
	req = httptest.NewRequest("POST", "/api/addresses", bytes.NewBufferString("not json"))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for malformed JSON payload, got %d", rec.Code)
	}
}

// TestAddressCRUDAPI_LegacyBodyStillWorks locks in that the old single-value
// {type, value} request body (no "entries" key) still creates/updates an
// address object correctly — required for backward compat with any client
// not yet updated to send entries (plan §5 compat requirement).
func TestAddressCRUDAPI_LegacyBodyStillWorks(t *testing.T) {
	handler, _ := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	addrInput := model.AddressObjectInput{
		Name:  "Legacy_Body_Addr",
		Type:  "subnet",
		Value: "192.168.50.0/24",
	}
	body, _ := json.Marshal(addrInput)
	req := httptest.NewRequest("POST", "/api/addresses", bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for creating address with legacy body, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var created model.AddressObject
	json.NewDecoder(rec.Body).Decode(&created)
	if len(created.Entries) != 1 || created.Entries[0].Value != "192.168.50.0/24" {
		t.Fatalf("Expected legacy body to be normalized into a single entry, got %+v", created.Entries)
	}
}

// TestServiceCRUDAPI_EntriesBody mirrors TestAddressCRUDAPI_EntriesBody for
// Service Objects (T-09 API scope).
func TestServiceCRUDAPI_EntriesBody(t *testing.T) {
	handler, _ := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// 1. Create with entries body (multiple entries).
	svcInput := model.ServiceObjectInput{
		Name: "Multi_Entries_Svc",
		Entries: []model.ServiceEntry{
			{Protocol: "TCP", Port: "80"},
			{Protocol: "TCP", Port: "443"},
		},
	}
	body, _ := json.Marshal(svcInput)
	req := httptest.NewRequest("POST", "/api/services", bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for creating service with entries body, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var created model.ServiceObject
	json.NewDecoder(rec.Body).Decode(&created)
	if len(created.Entries) != 2 {
		t.Fatalf("Expected 2 entries in create response, got %d: %+v", len(created.Entries), created.Entries)
	}
	if created.Protocol != "TCP" || created.Port != "80" {
		t.Errorf("Expected legacy protocol/port to mirror Entries[0], got protocol=%q port=%q", created.Protocol, created.Port)
	}

	// 2. Update with a replaced entries body.
	updateInput := model.ServiceObjectInput{
		Name:    "Multi_Entries_Svc",
		Entries: []model.ServiceEntry{{Protocol: "UDP", Port: "53"}},
	}
	body, _ = json.Marshal(updateInput)
	req = httptest.NewRequest("PUT", "/api/services/"+created.ID, bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for updating service with entries body, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var updated model.ServiceObject
	json.NewDecoder(rec.Body).Decode(&updated)
	if len(updated.Entries) != 1 || updated.Entries[0].Port != "53" {
		t.Fatalf("Expected update to fully replace entries, got %+v", updated.Entries)
	}

	// 3. 400 when body has neither entries nor legacy protocol/port.
	emptyInput := model.ServiceObjectInput{Name: "No_Entries_Svc"}
	body, _ = json.Marshal(emptyInput)
	req = httptest.NewRequest("POST", "/api/services", bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request creating a service with no entries/legacy fields, got %d. Body: %s", rec.Code, rec.Body.String())
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
	addSessionCookie(req, authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("Expected 400 Bad Request for scanning on ethernet interface, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// 2. Scan on wireless interface (should succeed with 200 OK)
	req = httptest.NewRequest("GET", "/api/interfaces/iface-2/scan", nil)
	addSessionCookie(req, authToken)
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
	fwService := service.NewFirewallService(repo, fw, ifaceService)
	dns := kernel.NewDNSManager(true)
	dnsService := service.NewDNSService(repo, dns)
	qos := kernel.NewMockQos()
	qosService := service.NewQosService(repo, qos)
	dhcpServerService := service.NewDhcpServerService(repo, dhcp)
	dnsServer := kernel.NewMockDNSServerManager()
	dnsServerService := service.NewDNSServerService(repo, dnsServer, dnsService)
	hostnameMgr := kernel.NewMockHostnameManager()
	dhcpcdMgr := kernel.NewMockDhcpcdManager()
	hostnameService := service.NewHostnameService(repo, hostnameMgr, dhcpcdMgr, ifaceService)
	timeService := service.NewTimeService(repo, kernel.NewMockTimeManager())

	// Server initialized with disableEdit = true
	testHealthChecker := service.NewDhcpHealthChecker(repo, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), net, service.NewEventLogService(repo), service.NewNetEventBus())
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, true, false, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), routingService, fwService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, service.NewUserService(repo), nil, service.NewSystemStatusService(kernel.NewMockSystemStats(), repo, hostnameService, timeService, "test"), service.NewPowerService(kernel.NewMockPowerManager()), service.NewEventLogService(repo), testHealthChecker, nil, nil, nil, nil, nil, nil, nil)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for PUT /api/system/dns, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// 3. Verify updated DNS Config
	req = httptest.NewRequest("GET", "/api/system/dns", nil)
	addSessionCookie(req, authToken)
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

// TestDNSServerSettingsGrandfatherValidation verifies the tolerate-dangling-refs
// semantics of PUT /api/dns/settings (issue #46, Step 1): a name already saved is
// accepted even when it no longer exists in the kernel, so the user can keep or
// remove it; only a *newly added* name that doesn't exist is rejected with 400.
func TestDNSServerSettingsGrandfatherValidation(t *testing.T) {
	handler, repo := setupTestServer(t)
	token := "mock_session_id_test_token"

	// Seed a dangling interface name (no matching kernel link).
	if err := repo.SetDNSServerInterfaces([]string{"eth0.999"}); err != nil {
		t.Fatalf("seed dns server settings failed: %v", err)
	}

	put := func(names []string) *httptest.ResponseRecorder {
		return vlanReq(t, handler, "PUT", "/api/dns/settings", token, model.DNSServerSettings{
			Interfaces:         names,
			DNSCacheTTLMinutes: model.DNSCacheTTLDefault,
			DNSCacheMaxEntries: model.DNSCacheEntriesDefault,
		})
	}

	// (a) Keeping the saved dangling name is allowed (grandfathered) -> 200.
	if rec := put([]string{"eth0.999"}); rec.Code != http.StatusOK {
		t.Fatalf("(a) keep dangling name: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// (b) Removing the dangling name is allowed -> 200.
	if rec := put([]string{}); rec.Code != http.StatusOK {
		t.Fatalf("(b) remove dangling name: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if got, _ := repo.GetDNSServerInterfaces(); len(got) != 0 {
		t.Fatalf("(b) expected settings emptied, got %v", got)
	}

	// (c) Adding a brand-new name that doesn't exist is rejected -> 400.
	if rec := put([]string{"nope0"}); rec.Code != http.StatusBadRequest {
		t.Fatalf("(c) add fake name: expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// (d) Adding a real kernel interface is accepted -> 200.
	if rec := put([]string{"eth0"}); rec.Code != http.StatusOK {
		t.Fatalf("(d) add real interface: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if got, _ := repo.GetDNSServerInterfaces(); len(got) != 1 || got[0] != "eth0" {
		t.Fatalf("(d) expected settings [eth0], got %v", got)
	}
}

// dnsStatsTestServer is a copy of buildTestServer's wiring, but keeps a
// handle on the *kernel.MockDNSServerManager (for ApplyCount) so
// TestDNSServerSettingsUpdate_* below can assert exactly when ApplyZones
// fires (docs/ref/todo/statistics-dns-top-domain-plan.md T-11 item 7).
func dnsStatsTestServer(t *testing.T) (http.Handler, *db.Repository, *kernel.MockDNSServerManager) {
	t.Helper()
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
	fwService := service.NewFirewallService(repo, fw, ifaceService)
	dns := kernel.NewDNSManager(true)
	dnsService := service.NewDNSService(repo, dns)
	qos := kernel.NewMockQos()
	qosService := service.NewQosService(repo, qos)
	dhcpServerService := service.NewDhcpServerService(repo, dhcp)
	dnsServer := kernel.NewMockDNSServerManager()
	dnsServerService := service.NewDNSServerService(repo, dnsServer, dnsService)
	hostnameMgr := kernel.NewMockHostnameManager()
	dhcpcdMgr := kernel.NewMockDhcpcdManager()
	hostnameService := service.NewHostnameService(repo, hostnameMgr, dhcpcdMgr, ifaceService)
	timeService := service.NewTimeService(repo, kernel.NewMockTimeManager())
	wifiPresetService := service.NewWifiPresetService(repo, ifaceService)
	testHealthChecker := service.NewDhcpHealthChecker(repo, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), net, service.NewEventLogService(repo), service.NewNetEventBus())
	systemServiceSvc := service.NewSystemServiceService(kernel.NewMockSystemServiceManager(), repo)
	trafficStatsService := service.NewTrafficStatsService(kernel.NewMockTrafficAccounting(nil), repo, dhcp, kernel.NewMockSystemStats(), 0, 0, 0)
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, false, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), routingService, fwService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, service.NewUserService(repo), nil, service.NewSystemStatusService(kernel.NewMockSystemStats(), repo, hostnameService, timeService, "test"), service.NewPowerService(kernel.NewMockPowerManager()), service.NewEventLogService(repo), testHealthChecker, wifiPresetService, systemServiceSvc, nil, trafficStatsService, service.NewStatisticsService(trafficStatsService, repo, dhcp, 2400, 200, 500, 300), service.NewIPInfoService(true, service.NewMockIPInfoProvider()), nil) // dns-stats-max-pairs / dns-stats-max-clients defaults; deny-stats defaults; ipinfo enabled+mock provider for handler tests
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	return handler, repo, dnsServer
}

// TestDNSServerSettingsUpdate_TTLCapOnlyDoesNotCallApplyZones is T-11 item 7
// (🔒, plan §5 item 18): changing only dnsCacheTtlMinutes/dnsCacheMaxEntries
// must never restart dnsmasq.
func TestDNSServerSettingsUpdate_TTLCapOnlyDoesNotCallApplyZones(t *testing.T) {
	handler, repo, dnsServer := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	baseline := dnsServer.ApplyCount

	rec := vlanReq(t, handler, "PUT", "/api/dns/settings", token, model.DNSServerSettings{
		Interfaces:         []string{},
		QueryLogging:       false,
		DNSCacheTTLMinutes: 30,
		DNSCacheMaxEntries: 8192,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if dnsServer.ApplyCount != baseline {
		t.Errorf("ApplyZones called %d time(s) for a TTL/cap-only change, want %d (no restart)", dnsServer.ApplyCount, baseline)
	}

	settings, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings: %v", err)
	}
	if settings.DNSCacheTTLMinutes != 30 || settings.DNSCacheMaxEntries != 8192 {
		t.Errorf("settings not persisted: %+v", settings)
	}
}

// TestDNSServerSettingsUpdate_QueryLoggingChangeCallsApplyZonesOnce is T-11
// item 7: flipping queryLogging must call ApplyZones exactly once.
func TestDNSServerSettingsUpdate_QueryLoggingChangeCallsApplyZonesOnce(t *testing.T) {
	handler, _, dnsServer := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	baseline := dnsServer.ApplyCount

	rec := vlanReq(t, handler, "PUT", "/api/dns/settings", token, model.DNSServerSettings{
		Interfaces:         []string{},
		QueryLogging:       true,
		DNSCacheTTLMinutes: model.DNSCacheTTLDefault,
		DNSCacheMaxEntries: model.DNSCacheEntriesDefault,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if dnsServer.ApplyCount != baseline+1 {
		t.Errorf("ApplyZones called %d time(s) after enabling queryLogging, want %d", dnsServer.ApplyCount, baseline+1)
	}
}

// TestDNSServerSettingsUpdate_OutOfRangeRejected is T-11 item 7 (🔒, plan §5
// item 17): out-of-range TTL/cap returns 400 and the DB is left untouched.
func TestDNSServerSettingsUpdate_OutOfRangeRejected(t *testing.T) {
	handler, repo, dnsServer := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	before, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings: %v", err)
	}
	baseline := dnsServer.ApplyCount

	cases := []model.DNSServerSettings{
		{Interfaces: []string{}, DNSCacheTTLMinutes: 0, DNSCacheMaxEntries: model.DNSCacheEntriesDefault},
		{Interfaces: []string{}, DNSCacheTTLMinutes: -5, DNSCacheMaxEntries: model.DNSCacheEntriesDefault},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: 99999},
	}
	for _, c := range cases {
		rec := vlanReq(t, handler, "PUT", "/api/dns/settings", token, c)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("input %+v: expected 400, got %d. Body: %s", c, rec.Code, rec.Body.String())
		}
	}

	after, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings: %v", err)
	}
	if after.QueryLogging != before.QueryLogging || after.DNSCacheTTLMinutes != before.DNSCacheTTLMinutes || after.DNSCacheMaxEntries != before.DNSCacheMaxEntries {
		t.Errorf("DB changed after rejected requests: before=%+v after=%+v", before, after)
	}
	if dnsServer.ApplyCount != baseline {
		t.Errorf("ApplyZones called after a rejected request: baseline=%d now=%d", baseline, dnsServer.ApplyCount)
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
	fwService := service.NewFirewallService(repo, fw, ifaceService)
	dns := kernel.NewDNSManager(true)
	dnsService := service.NewDNSService(repo, dns)
	qos := kernel.NewMockQos()
	qosService := service.NewQosService(repo, qos)
	dhcpServerService := service.NewDhcpServerService(repo, dhcp)
	dnsServer := kernel.NewMockDNSServerManager()
	dnsServerService := service.NewDNSServerService(repo, dnsServer, dnsService)
	hostnameMgr := kernel.NewMockHostnameManager()
	dhcpcdMgr := kernel.NewMockDhcpcdManager()
	hostnameService := service.NewHostnameService(repo, hostnameMgr, dhcpcdMgr, ifaceService)
	timeService := service.NewTimeService(repo, kernel.NewMockTimeManager())

	testHealthChecker := service.NewDhcpHealthChecker(repo, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), net, service.NewEventLogService(repo), service.NewNetEventBus())
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, false, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), routingService, fwService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, service.NewUserService(repo), nil, service.NewSystemStatusService(kernel.NewMockSystemStats(), repo, hostnameService, timeService, "test"), service.NewPowerService(kernel.NewMockPowerManager()), service.NewEventLogService(repo), testHealthChecker, nil, nil, nil, nil, nil, nil, nil)
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
	authToken := sessionCookieFromRec(t, rec)

	// 2. Try fetching a protected resource like stats, should get 403 Forbidden
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusForbidden {
		t.Errorf("Expected 403 Forbidden when accessing stats before changing initial password, got %d", rec.Code)
	}

	// 3. Change password via PUT /api/system/password
	changePayload := model.ChangePasswordRequest{CurrentPassword: "pigate", NewPassword: "new_secure_pass"}
	changeBody, _ := json.Marshal(changePayload)
	req = httptest.NewRequest("PUT", "/api/system/password", bytes.NewBuffer(changeBody))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK for changing password, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// 4. Try fetching stats again, should succeed now
	req = httptest.NewRequest("GET", "/api/dashboard/stats", nil)
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("Expected 200 OK for stats after changing password, got %d", rec.Code)
	}
}

// TestChangePasswordRejectsWeakPassword verifies the server-side password policy
// is enforced on the self-service change-password endpoint — an API caller that
// bypasses the frontend length check must still be rejected, and the current
// password must remain unchanged.
func TestChangePasswordRejectsWeakPassword(t *testing.T) {
	handler, repo := setupTestServer(t)

	// A new password shorter than the minimum (8) must be rejected with 400.
	changePayload := model.ChangePasswordRequest{CurrentPassword: "pigate", NewPassword: "short"}
	changeBody, _ := json.Marshal(changePayload)
	req := httptest.NewRequest("PUT", "/api/system/password", bytes.NewBuffer(changeBody))
	addSessionCookie(req, "mock_session_id_test_token")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("Expected 400 for a too-short new password, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// The original password must still work — the weak change was not persisted.
	user, err := repo.GetUserByUsername("pigate")
	if err != nil || user == nil {
		t.Fatalf("Failed to reload user: %v", err)
	}
	if bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte("pigate")) != nil {
		t.Error("Original password should remain valid after a rejected weak change")
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
	fwService := service.NewFirewallService(repo, fw, ifaceService)
	dns := kernel.NewDNSManager(true)
	dnsService := service.NewDNSService(repo, dns)
	qos := kernel.NewMockQos()
	qosService := service.NewQosService(repo, qos)
	dhcpServerService := service.NewDhcpServerService(repo, dhcp)
	dnsServer := kernel.NewMockDNSServerManager()
	dnsServerService := service.NewDNSServerService(repo, dnsServer, dnsService)
	hostnameMgr := kernel.NewMockHostnameManager()
	dhcpcdMgr := kernel.NewMockDhcpcdManager()
	hostnameService := service.NewHostnameService(repo, hostnameMgr, dhcpcdMgr, ifaceService)
	timeService := service.NewTimeService(repo, kernel.NewMockTimeManager())

	testHealthChecker := service.NewDhcpHealthChecker(repo, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), net, service.NewEventLogService(repo), service.NewNetEventBus())
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, false, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), routingService, fwService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, service.NewUserService(repo), nil, service.NewSystemStatusService(kernel.NewMockSystemStats(), repo, hostnameService, timeService, "test"), service.NewPowerService(kernel.NewMockPowerManager()), service.NewEventLogService(repo), testHealthChecker, nil, nil, nil, nil, nil, nil, nil)
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

	authToken := sessionCookieFromRec(t, rec)

	req = httptest.NewRequest("GET", "/api/auth/session", nil)
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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

func TestDashboardSystemStatusAPIs(t *testing.T) {
	handler, _ := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		addSessionCookie(req, authToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// 1. Performance metrics: backward-compat flat fields + detail objects.
	rec := get("/api/dashboard/performance")
	if rec.Code != http.StatusOK {
		t.Fatalf("performance: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var metrics model.SystemMetrics
	if err := json.Unmarshal(rec.Body.Bytes(), &metrics); err != nil {
		t.Fatalf("performance: decode failed: %v", err)
	}
	if metrics.MemDetail.TotalBytes == 0 {
		t.Errorf("performance: expected non-zero memDetail.totalBytes")
	}
	if metrics.Storage.Path != "/" {
		t.Errorf("performance: expected storage.path '/', got %q", metrics.Storage.Path)
	}

	// 2. System info: version + hostname present, uptime numeric.
	rec = get("/api/system/info")
	if rec.Code != http.StatusOK {
		t.Fatalf("system info: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var info model.SystemInfo
	if err := json.Unmarshal(rec.Body.Bytes(), &info); err != nil {
		t.Fatalf("system info: decode failed: %v", err)
	}
	if info.Version != "test" {
		t.Errorf("system info: expected version 'test', got %q", info.Version)
	}
	if info.Hostname == "" {
		t.Errorf("system info: expected non-empty hostname")
	}
	if info.SystemTime == "" {
		t.Errorf("system info: expected non-empty systemTime")
	}

	// 3. Traffic history: valid shape (buckets may be empty pre-Start).
	rec = get("/api/dashboard/traffic")
	if rec.Code != http.StatusOK {
		t.Fatalf("traffic: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var hist model.TrafficHistory
	if err := json.Unmarshal(rec.Body.Bytes(), &hist); err != nil {
		t.Fatalf("traffic: decode failed: %v", err)
	}
	if hist.Interfaces == nil {
		t.Errorf("traffic: expected non-nil interfaces array")
	}
}

// TestHandleGetTrafficDetail_WindowWhitelist asserts the ?window query param
// is whitelisted to {"1h","24h"} rather than passed raw into the service
// (docs/ref/todo/dashboard-traffic-detail-plan.md T-09) — any other value,
// including an empty/missing one, must silently fall back to "1h".
func TestHandleGetTrafficDetail_WindowWhitelist(t *testing.T) {
	handler, _ := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		addSessionCookie(req, authToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	cases := []struct {
		query        string
		expectWindow string
	}{
		{"", "1h"},
		{"?window=15m", "15m"},
		{"?window=30m", "30m"},
		{"?window=1h", "1h"},
		{"?window=3h", "3h"},
		{"?window=6h", "6h"},
		{"?window=12h", "12h"},
		{"?window=24h", "24h"},
		{"?window=evil", "1h"},
		{"?window=99h", "1h"},
		{"?window=2h", "1h"},
		// Uppercase button labels (plan §0 D-4/§6 item 3): must NOT be
		// normalized to their lowercase equivalent — they are unrecognized
		// values, same as any other garbage.
		{"?window=1H", "1h"},
		{"?window=24H", "1h"},
		{"?window=15M", "1h"},
		{"?window=%201h", "1h"}, // " 1h" (leading space)
	}
	for _, c := range cases {
		rec := get("/api/dashboard/traffic-detail" + c.query)
		if rec.Code != http.StatusOK {
			t.Fatalf("query=%q: expected 200, got %d. Body: %s", c.query, rec.Code, rec.Body.String())
		}
		var detail model.TrafficDetail
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("query=%q: decode failed: %v", c.query, err)
		}
		if detail.Window != c.expectWindow {
			t.Errorf("query=%q: expected window=%q, got %q", c.query, c.expectWindow, detail.Window)
		}
		if detail.Categories == nil || detail.TopTalkers == nil || detail.TopRules == nil {
			t.Errorf("query=%q: expected non-nil (possibly empty) slices, got categories=%v topTalkers=%v topRules=%v",
				c.query, detail.Categories, detail.TopTalkers, detail.TopRules)
		}
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
	fwService := service.NewFirewallService(repo, fw, ifaceService)
	dns := kernel.NewDNSManager(true)
	dnsService := service.NewDNSService(repo, dns)
	qos := kernel.NewMockQos()
	qosService := service.NewQosService(repo, qos)
	dhcpServerService := service.NewDhcpServerService(repo, dhcp)
	dnsServer := kernel.NewMockDNSServerManager()
	dnsServerService := service.NewDNSServerService(repo, dnsServer, dnsService)
	hostnameMgr := kernel.NewMockHostnameManager()
	dhcpcdMgr := kernel.NewMockDhcpcdManager()
	hostnameService := service.NewHostnameService(repo, hostnameMgr, dhcpcdMgr, ifaceService)
	timeService := service.NewTimeService(repo, kernel.NewMockTimeManager())

	testHealthChecker := service.NewDhcpHealthChecker(repo, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), net, service.NewEventLogService(repo), service.NewNetEventBus())
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, false, false, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), routingService, fwService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, service.NewUserService(repo), nil, service.NewSystemStatusService(kernel.NewMockSystemStats(), repo, hostnameService, timeService, "test"), service.NewPowerService(kernel.NewMockPowerManager()), service.NewEventLogService(repo), testHealthChecker, nil, nil, nil, nil, nil, nil, nil)
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
		Alias:                "LAN_TestSync",
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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
	addSessionCookie(req, authToken)
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

// --- VLAN interface management API (issue #20) ---

func vlanReq(t *testing.T, handler http.Handler, method, path, token string, body any) *httptest.ResponseRecorder {
	t.Helper()
	var buf *bytes.Buffer
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewBuffer(b)
	} else {
		buf = bytes.NewBuffer(nil)
	}
	req := httptest.NewRequest(method, path, buf)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

func TestVlanAPICreateAndDelete(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	// Create a VLAN on eth0 (present in the mock kernel).
	rec := vlanReq(t, handler, "POST", "/api/interfaces/vlan", token, model.CreateVlanInput{
		Parent: "eth0", VlanID: 100, Alias: "vlan100", Role: "LAN", AddressingMode: "dhcp",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var created model.NetworkInterface
	json.NewDecoder(rec.Body).Decode(&created)
	if created.Name != "eth0.100" || created.Subtype != "vlan" {
		t.Fatalf("unexpected created VLAN: %+v", created)
	}

	// It should now show up in the interface list, up + managed.
	rec = vlanReq(t, handler, "GET", "/api/interfaces", token, nil)
	var list []model.NetworkInterface
	json.NewDecoder(rec.Body).Decode(&list)
	var found *model.NetworkInterface
	for i := range list {
		if list[i].Name == "eth0.100" {
			found = &list[i]
		}
	}
	if found == nil {
		t.Fatalf("created VLAN not present in interface list")
	}

	// Delete it while it is up (VLANs can be deleted regardless of offline state).
	rec = vlanReq(t, handler, "DELETE", "/api/interfaces/"+found.ID, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK deleting up VLAN, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Confirm it's gone.
	rec = vlanReq(t, handler, "GET", "/api/interfaces", token, nil)
	list = nil
	json.NewDecoder(rec.Body).Decode(&list)
	for _, it := range list {
		if it.Name == "eth0.100" {
			t.Errorf("VLAN eth0.100 still present after delete")
		}
	}
}

func TestVlanAPICreateValidationAndConflict(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	// Invalid VLAN ID -> 400
	rec := vlanReq(t, handler, "POST", "/api/interfaces/vlan", token, model.CreateVlanInput{
		Parent: "eth0", VlanID: 9999,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for invalid VLAN ID, got %d", rec.Code)
	}

	// Wireless parent -> 400
	rec = vlanReq(t, handler, "POST", "/api/interfaces/vlan", token, model.CreateVlanInput{
		Parent: "wlan0", VlanID: 10,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for wireless parent, got %d", rec.Code)
	}

	// Create once, then duplicate -> 409
	rec = vlanReq(t, handler, "POST", "/api/interfaces/vlan", token, model.CreateVlanInput{
		Parent: "eth0", VlanID: 200, AddressingMode: "dhcp",
	})
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201 for first create, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	rec = vlanReq(t, handler, "POST", "/api/interfaces/vlan", token, model.CreateVlanInput{
		Parent: "eth0", VlanID: 200, AddressingMode: "dhcp",
	})
	if rec.Code != http.StatusConflict {
		t.Errorf("expected 409 for duplicate VLAN, got %d", rec.Code)
	}
}

// A physical (non-vlan) interface that is up must still be protected by the
// offline-only delete guard — the VLAN branch must not weaken it.
func TestDeletePhysicalInterfaceStillOfflineGuarded(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	// Find a real, up, non-vlan interface from the live list.
	rec := vlanReq(t, handler, "GET", "/api/interfaces", token, nil)
	var list []model.NetworkInterface
	json.NewDecoder(rec.Body).Decode(&list)
	var target *model.NetworkInterface
	for i := range list {
		if list[i].Subtype != "vlan" && list[i].Status == "up" {
			target = &list[i]
			break
		}
	}
	if target == nil {
		t.Skip("no up physical interface available in mock kernel")
	}

	rec = vlanReq(t, handler, "DELETE", "/api/interfaces/"+target.ID, token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 deleting an up physical interface, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

func TestVlanAPIViewerForbidden(t *testing.T) {
	handler, repo := setupTestServer(t)

	// Create a read-only user and a session for them.
	if err := repo.CreateUser(model.User{
		ID: "user-viewer", Username: "viewer", PasswordHash: "x",
		Role: model.RoleAdminReadonly, Status: model.StatusActive,
	}); err != nil {
		t.Fatalf("create viewer user: %v", err)
	}
	AddSession("viewer_token", "viewer")

	rec := vlanReq(t, handler, "POST", "/api/interfaces/vlan", "viewer_token", model.CreateVlanInput{
		Parent: "eth0", VlanID: 300, AddressingMode: "dhcp",
	})
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for read-only user creating VLAN, got %d", rec.Code)
	}
}

// TestInterfaceAliasAPIValidation covers the HTTP mapping of the alias rules on
// PUT /api/interfaces/{id}: 409 for conflicts (duplicate alias or another
// interface's OS name), 400 for a malformed alias, and normalization of an
// omitted alias to the OS name instead of persisting "".
func TestInterfaceAliasAPIValidation(t *testing.T) {
	handler, repo := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	put := func(t *testing.T, alias string) *httptest.ResponseRecorder {
		t.Helper()
		payload := map[string]any{
			"alias": alias, "role": "LAN", "addressingMode": "static",
			"ip": "192.168.1.1", "netmask": "24", "gateway": "",
			"macAddress": "DC:A6:32:AA:BB:C1", "adminAccess": []string{"PING", "HTTP", "SSH"},
		}
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("PUT", "/api/interfaces/iface-1", bytes.NewBuffer(body))
		addSessionCookie(req, authToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Duplicate of wlan0's seeded alias "WAN_WiFi", case-insensitive.
	if rec := put(t, "wan_wifi"); rec.Code != http.StatusConflict {
		t.Errorf("duplicate alias: expected 409, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	// Another interface's OS name.
	if rec := put(t, "WLAN0"); rec.Code != http.StatusConflict {
		t.Errorf("alias == other OS name: expected 409, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	// Malformed alias.
	if rec := put(t, "bad alias!"); rec.Code != http.StatusBadRequest {
		t.Errorf("malformed alias: expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Omitted/empty alias normalizes to the OS name — in the response and in the DB.
	rec := put(t, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("empty alias: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.NetworkInterface
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.Alias != "eth0" {
		t.Errorf("expected response alias normalized to \"eth0\", got %q", got.Alias)
	}
	stored, err := repo.GetInterfaceByID("iface-1")
	if err != nil || stored == nil {
		t.Fatalf("load iface-1: %v", err)
	}
	if stored.Alias != "eth0" {
		t.Errorf("expected persisted alias \"eth0\", got %q", stored.Alias)
	}
}

// --- DNS Server upstream resolver (docs/ref/todo/
// dns-server-settings-tab-and-upstream-plan.md T-08 item 4) ---

// TestDNSServerSettingsUpdate_UpstreamChangeCallsApplyZonesOnce is T-08 item
// 4(a): changing upstreamServers (mode + list) must call ApplyZones exactly
// once, same as interfaces/queryLogging.
func TestDNSServerSettingsUpdate_UpstreamChangeCallsApplyZonesOnce(t *testing.T) {
	handler, repo, dnsServer := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	baseline := dnsServer.ApplyCount

	rec := vlanReq(t, handler, "PUT", "/api/dns/settings", token, model.DNSServerSettings{
		Interfaces:         []string{},
		QueryLogging:       false,
		DNSCacheTTLMinutes: model.DNSCacheTTLDefault,
		DNSCacheMaxEntries: model.DNSCacheEntriesDefault,
		UpstreamMode:       model.DNSUpstreamModeCustom,
		UpstreamServers:    []string{"1.1.1.1", "8.8.8.8"},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if dnsServer.ApplyCount != baseline+1 {
		t.Errorf("ApplyZones called %d time(s) after changing upstream, want %d", dnsServer.ApplyCount, baseline+1)
	}

	settings, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings: %v", err)
	}
	if settings.UpstreamMode != model.DNSUpstreamModeCustom || len(settings.UpstreamServers) != 2 {
		t.Errorf("upstream settings not persisted: %+v", settings)
	}
}

// TestDNSServerSettingsUpdate_TTLCapOnlyDoesNotChangeUpstreamOrApply is T-08
// item 4(b): a TTL/cap-only PUT that repeats the current (default "system",
// empty list) upstream fields must NOT bump ApplyCount (regression guard for
// PR #109 already covered above; this variant additionally exercises the
// upstream fields as part of the "unchanged" comparison).
func TestDNSServerSettingsUpdate_TTLCapOnlyDoesNotChangeUpstreamOrApply(t *testing.T) {
	handler, _, dnsServer := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	baseline := dnsServer.ApplyCount

	rec := vlanReq(t, handler, "PUT", "/api/dns/settings", token, model.DNSServerSettings{
		Interfaces:         []string{},
		QueryLogging:       false,
		DNSCacheTTLMinutes: 30,
		DNSCacheMaxEntries: 8192,
		UpstreamMode:       model.DNSUpstreamModeSystem,
		UpstreamServers:    []string{},
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if dnsServer.ApplyCount != baseline {
		t.Errorf("ApplyZones called %d time(s) for a TTL/cap-only change, want %d (no restart)", dnsServer.ApplyCount, baseline)
	}
}

// TestDNSServerSettingsUpdate_UpstreamRejected is T-08 item 4(c) (🔒): an
// invalid upstream mode/IP returns 400 and the DB is left untouched.
func TestDNSServerSettingsUpdate_UpstreamRejected(t *testing.T) {
	handler, repo, dnsServer := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	before, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings: %v", err)
	}
	baseline := dnsServer.ApplyCount

	cases := []model.DNSServerSettings{
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{}},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{"1.1.1.1\nlog-facility=/tmp/x"}},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{"8.8.8.8#5353"}},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{"dns.google"}},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{"127.0.0.53"}},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{"1.1.1.1", "1.1.1.1"}},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{"1.1.1.1", "8.8.8.8", "9.9.9.9", "1.0.0.1", "8.8.4.4"}},
		{Interfaces: []string{}, DNSCacheTTLMinutes: model.DNSCacheTTLDefault, DNSCacheMaxEntries: model.DNSCacheEntriesDefault, UpstreamMode: "bogus"},
	}
	for _, c := range cases {
		rec := vlanReq(t, handler, "PUT", "/api/dns/settings", token, c)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("input %+v: expected 400, got %d. Body: %s", c, rec.Code, rec.Body.String())
		}
	}

	after, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings: %v", err)
	}
	if after.UpstreamMode != before.UpstreamMode || len(after.UpstreamServers) != len(before.UpstreamServers) {
		t.Errorf("DB changed after rejected requests: before=%+v after=%+v", before, after)
	}
	if dnsServer.ApplyCount != baseline {
		t.Errorf("ApplyZones called after a rejected request: baseline=%d now=%d", baseline, dnsServer.ApplyCount)
	}
}

// TestHandleUpdateDNSConfig_NeverCallsApplyZones is T-08 item 4(d) — the T-06
// regression guard: PUT /api/system/dns (System DNS) must never bump
// MockDNSServerManager.ApplyCount, in either DNS Server upstream mode. This
// locks the removal of the old ApplyAll() call so nobody re-adds it.
func TestHandleUpdateDNSConfig_NeverCallsApplyZones(t *testing.T) {
	handler, repo, dnsServer := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	for _, mode := range []string{model.DNSUpstreamModeSystem, model.DNSUpstreamModeCustom} {
		var servers []string
		if mode == model.DNSUpstreamModeCustom {
			servers = []string{"1.1.1.1"}
		}
		if err := repo.SetDNSServerSettings(false, model.DNSCacheTTLDefault, model.DNSCacheEntriesDefault, mode, servers); err != nil {
			t.Fatalf("seed upstream mode %q: %v", mode, err)
		}

		baseline := dnsServer.ApplyCount
		rec := vlanReq(t, handler, "PUT", "/api/system/dns", token, model.DNSConfigInput{
			Mode: "static", PrimaryDNS: "9.9.9.9", SecondaryDNS: "1.1.1.1", LocalDomain: "pigate.local",
		})
		if rec.Code != http.StatusOK {
			t.Fatalf("mode %q: expected 200, got %d. Body: %s", mode, rec.Code, rec.Body.String())
		}
		if dnsServer.ApplyCount != baseline {
			t.Errorf("mode %q: System DNS update bumped ApplyCount from %d to %d (must stay 0-delta — dnsmasq write path must be gone)", mode, baseline, dnsServer.ApplyCount)
		}
	}
}

// TestHandleUpdateDNSConfig_InvalidInputRejected is T-08 item 7 (🔒, T-09):
// primaryDns/localDomain that would inject a config directive must 400, and
// must not touch the DB.
func TestHandleUpdateDNSConfig_InvalidInputRejected(t *testing.T) {
	handler, repo, _ := dnsStatsTestServer(t)
	token := "mock_session_id_test_token"

	before, err := repo.GetDNSConfig()
	if err != nil {
		t.Fatalf("GetDNSConfig: %v", err)
	}

	cases := []model.DNSConfigInput{
		{Mode: "static", PrimaryDNS: "1.1.1.1\nDNS=8.8.8.8", LocalDomain: "pigate.local"},
		{Mode: "static", PrimaryDNS: "", LocalDomain: "pigate.local"},
		{Mode: "wan", LocalDomain: "not a domain"},
		{Mode: "wan", LocalDomain: "evil\nDomains=~."},
	}
	for _, c := range cases {
		rec := vlanReq(t, handler, "PUT", "/api/system/dns", token, c)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("input %+v: expected 400, got %d. Body: %s", c, rec.Code, rec.Body.String())
		}
	}

	after, err := repo.GetDNSConfig()
	if err != nil {
		t.Fatalf("GetDNSConfig: %v", err)
	}
	if after.Mode != before.Mode || after.PrimaryDNS != before.PrimaryDNS || after.LocalDomain != before.LocalDomain {
		t.Errorf("DB changed after rejected requests: before=%+v after=%+v", before, after)
	}
}

// TestHandleGetDNSDomainClients_InputValidation is drilldown plan T-05 item
// 8 (🔒): the `domain` param on GET /api/statistics/dns/domain must reject
// script/control-char/oversized payloads with 400, and never echo the raw
// value it rejected back in the response body.
func TestHandleGetDNSDomainClients_InputValidation(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.statistics.SetDNSLoggingEnabled(true)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	get := func(rawQuery string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/statistics/dns/domain?"+rawQuery, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	badDomains := []string{
		"<script>alert(1)</script>",
		"evil.com%0Aextra",       // newline
		"evil.com%00",            // NUL byte
		"has space.example.com",  // space
		strings.Repeat("a", 300), // too long
	}
	for _, d := range badDomains {
		rec := get("domain=" + url.QueryEscape(d))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("domain=%q: expected 400, got %d. Body: %s", d, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), d) {
			t.Errorf("domain=%q: rejected value was echoed back in the response body: %s", d, rec.Body.String())
		}
	}

	// No domain param at all -> 400.
	if rec := get(""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing domain: expected 400, got %d", rec.Code)
	}

	// A valid domain never queried -> 200 with an empty client list, not 404.
	rec := get("domain=" + url.QueryEscape("never-queried.example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid-but-unqueried domain: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var drill model.DNSDomainDrilldown
	if err := json.Unmarshal(rec.Body.Bytes(), &drill); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(drill.Clients) != 0 {
		t.Errorf("expected empty client list for an unqueried domain, got %+v", drill.Clients)
	}

	// window whitelist: an unrecognized value silently falls back to "1h".
	rec = get("domain=example.com&window=evil")
	if rec.Code != http.StatusOK {
		t.Fatalf("window=evil: expected 200, got %d", rec.Code)
	}
	var drill2 model.DNSDomainDrilldown
	if err := json.Unmarshal(rec.Body.Bytes(), &drill2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if drill2.Window != "1h" {
		t.Errorf("expected window=evil to fall back to 1h, got %q", drill2.Window)
	}
}

// TestHandleGetDNSClientDomains_InputValidation is drilldown plan T-05 item 8
// (🔒): the `client` param on GET /api/statistics/dns/client must accept only
// a parseable IP or the literal "unknown", rejecting anything else with 400.
func TestHandleGetDNSClientDomains_InputValidation(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.statistics.SetDNSLoggingEnabled(true)
	server.statistics.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: ""})
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	get := func(rawQuery string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/statistics/dns/client?"+rawQuery, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := get("client=notanip"); rec.Code != http.StatusBadRequest {
		t.Errorf("client=notanip: expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	if rec := get(""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing client: expected 400, got %d", rec.Code)
	}

	// The reserved "unknown" bucket is a valid, non-IP value -> 200, and since
	// we recorded an event with an empty ClientIP above it must show up here.
	rec := get("client=unknown")
	if rec.Code != http.StatusOK {
		t.Fatalf("client=unknown: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var drill model.DNSClientDrilldown
	if err := json.Unmarshal(rec.Body.Bytes(), &drill); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(drill.Domains) != 1 || drill.Domains[0].Domain != "example.com" {
		t.Errorf("expected client=unknown to surface example.com, got %+v", drill.Domains)
	}

	// A syntactically valid IP that was never queried -> 200 with an empty
	// domain list, not 404.
	rec = get("client=192.168.99.99")
	if rec.Code != http.StatusOK {
		t.Fatalf("valid-but-unqueried client: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var drill2 model.DNSClientDrilldown
	if err := json.Unmarshal(rec.Body.Bytes(), &drill2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(drill2.Domains) != 0 {
		t.Errorf("expected empty domain list for an unqueried client, got %+v", drill2.Domains)
	}
}

// TestHandleGetDNSIPDomains_InputValidation covers docs/ref/todo/statistics-
// dns-ip-filter-plan.md T-04 (🔒): the `ip` param on GET
// /api/statistics/dns/ip must reject anything that doesn't parse as a
// netip.Addr with 400, never echo the raw value it rejected back in the
// response body, never call the service on a validation failure, normalize a
// valid-but-non-canonical IPv6 literal, and fall back window to "1h" on an
// invalid value rather than 400.
func TestHandleGetDNSIPDomains_InputValidation(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.statistics.SetDNSLoggingEnabled(true)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	get := func(rawQuery string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/statistics/dns/ip?"+rawQuery, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Missing ip -> 400.
	if rec := get(""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing ip: expected 400, got %d", rec.Code)
	}

	badIPs := []string{
		"1.2.3",           // incomplete
		"abc",             // not an IP at all
		"192.168.1.1; ls", // shell-injection-shaped payload
		"192.168.1.256",   // out-of-range octet
		"01.2.3.4",        // leading zero, rejected by netip.ParseAddr
	}
	for _, ip := range badIPs {
		rec := get("ip=" + url.QueryEscape(ip))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("ip=%q: expected 400, got %d. Body: %s", ip, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), ip) {
			t.Errorf("ip=%q: rejected value was echoed back in the response body: %s", ip, rec.Body.String())
		}
	}

	// A valid IP nothing resolved to -> 200 with an empty domains list, not 404.
	rec := get("ip=" + url.QueryEscape("10.9.9.9"))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid-but-unknown ip: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.DNSIPDomains
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.Domains) != 0 {
		t.Errorf("expected empty domains list for an unknown IP, got %+v", got.Domains)
	}
	if got.IP != "10.9.9.9" {
		t.Errorf("expected ip echoed back as canonical form, got %q", got.IP)
	}

	// A valid, non-canonical IPv6 literal -> 200, field `ip` normalized.
	rec = get("ip=" + url.QueryEscape("2001:DB8::1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("non-canonical ipv6: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got2 model.DNSIPDomains
	if err := json.Unmarshal(rec.Body.Bytes(), &got2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got2.IP != "2001:db8::1" {
		t.Errorf("expected ip normalized to canonical lowercase form, got %q", got2.IP)
	}

	// window whitelist: an unrecognized value silently falls back to "1h",
	// never a 400.
	rec = get("ip=10.0.0.1&window=9h")
	if rec.Code != http.StatusOK {
		t.Fatalf("window=9h: expected 200, got %d", rec.Code)
	}
	var got3 model.DNSIPDomains
	if err := json.Unmarshal(rec.Body.Bytes(), &got3); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got3.Window != "1h" {
		t.Errorf("expected window=9h to fall back to 1h, got %q", got3.Window)
	}
}

// TestHandleGetDNSQueryStatistics_WindowWhitelistAndDisabledState mirrors
// TestHandleGetTrafficDetail_WindowWhitelist above for the new
// /api/statistics/dns endpoint, plus the disabled-switch empty-state case
// (drilldown plan T-05 item 6/8).
func TestHandleGetDNSQueryStatistics_WindowWhitelistAndDisabledState(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	cases := []struct {
		query        string
		expectWindow string
	}{
		{"", "1h"},
		{"?window=1h", "1h"},
		{"?window=24h", "24h"},
		{"?window=evil", "1h"},
	}
	for _, c := range cases {
		rec := get("/api/statistics/dns" + c.query)
		if rec.Code != http.StatusOK {
			t.Fatalf("query=%q: expected 200, got %d. Body: %s", c.query, rec.Code, rec.Body.String())
		}
		var stats model.DNSQueryStatistics
		if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
			t.Fatalf("query=%q: decode failed: %v", c.query, err)
		}
		if stats.Window != c.expectWindow {
			t.Errorf("query=%q: expected window=%q, got %q", c.query, c.expectWindow, stats.Window)
		}
		// setupTestServer's StatisticsService starts with query logging off.
		if stats.Enabled {
			t.Errorf("query=%q: expected Enabled=false (query logging opt-in is off by default)", c.query)
		}
		if stats.TopDomains == nil || stats.TopClients == nil {
			t.Errorf("query=%q: expected non-nil (possibly empty) slices, got topDomains=%v topClients=%v", c.query, stats.TopDomains, stats.TopClients)
		}
		// docs/ref/todo/dns-blocked-query-statistics-plan.md T-10: the
		// blocked-side fields must also be non-nil empty slices in the
		// disabled state, same contract as TopDomains/TopClients above.
		if stats.TopBlockedDomains == nil || stats.TopBlockedClients == nil || stats.BlockedSeries == nil {
			t.Errorf("query=%q: expected non-nil (possibly empty) blocked slices, got topBlockedDomains=%v topBlockedClients=%v blockedSeries=%v",
				c.query, stats.TopBlockedDomains, stats.TopBlockedClients, stats.BlockedSeries)
		}
		if stats.BlockedQueries != 0 || stats.TotalBlockedDomains != 0 || stats.TotalBlockedClients != 0 {
			t.Errorf("query=%q: expected blocked counters to be 0 while disabled, got %+v", c.query, stats)
		}
	}
}

// TestHandleGetDNSQueryStatistics_NoNewQueryParamAccepted covers
// docs/ref/todo/dns-blocked-query-statistics-plan.md T-10's explicit
// constraint: GET /api/statistics/dns must NOT accept any new query
// parameter for the blocked-query feature — an unrecognized param is simply
// ignored, exactly like any other unknown query string key, and the
// response shape/behavior is unaffected.
func TestHandleGetDNSQueryStatistics_NoNewQueryParamAccepted(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	req := httptest.NewRequest("GET", "/api/statistics/dns?window=1h&blockedOnly=true&tab=blocked", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var stats model.DNSQueryStatistics
	if err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if stats.Window != "1h" {
		t.Errorf("expected window=1h (unaffected by the unknown params), got %q", stats.Window)
	}
}

// TestStatsWindowParam_AllEndpoints_SevenValuesAndFallback is docs/ref/todo/
// statistics-window-granularity-plan.md T-07 item 6 (§0 D-4 — the test the
// owner explicitly required): every endpoint that accepts ?window= must
// return exactly the 7 canonical (lowercase) values unchanged, and every
// unrecognized value — INCLUDING the uppercase button labels "1H"/"24H"/
// "15M", which must NOT be silently lowercased — falls back to "1h", never a
// 400/500/panic.
func TestStatsWindowParam_AllEndpoints_SevenValuesAndFallback(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	longString := strings.Repeat("a", 10*1024)
	fallbackValues := []string{
		"", "evil", "99h", "2h", "1H", "24H", "15M", " 1h", "../etc/passwd", longString,
	}
	canonical := []string{"15m", "30m", "1h", "3h", "6h", "12h", "24h"}

	endpoints := []struct {
		name string
		path func(window string) string
	}{
		{"dashboard/traffic-detail", func(w string) string { return "/api/dashboard/traffic-detail?window=" + url.QueryEscape(w) }},
		{"statistics/traffic", func(w string) string { return "/api/statistics/traffic?window=" + url.QueryEscape(w) }},
		{"statistics/traffic/hosts", func(w string) string { return "/api/statistics/traffic/hosts?window=" + url.QueryEscape(w) }},
		{"statistics/traffic/host", func(w string) string {
			return "/api/statistics/traffic/host?ip=192.168.1.50&window=" + url.QueryEscape(w)
		}},
		{"statistics/dns", func(w string) string { return "/api/statistics/dns?window=" + url.QueryEscape(w) }},
		{"statistics/dns/domain", func(w string) string {
			return "/api/statistics/dns/domain?domain=example.com&window=" + url.QueryEscape(w)
		}},
		{"statistics/dns/client", func(w string) string {
			return "/api/statistics/dns/client?client=unknown&window=" + url.QueryEscape(w)
		}},
		{"statistics/capacity", func(w string) string { return "/api/statistics/capacity?window=" + url.QueryEscape(w) }},
		{"statistics/firewall", func(w string) string { return "/api/statistics/firewall?window=" + url.QueryEscape(w) }},
	}

	for _, ep := range endpoints {
		for _, w := range canonical {
			rec := get(ep.path(w))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s window=%q: expected 200, got %d. Body: %s", ep.name, w, rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s window=%q: decode failed: %v", ep.name, w, err)
			}
			if got, _ := body["window"].(string); got != w {
				t.Errorf("%s window=%q: expected response window=%q, got %q", ep.name, w, w, got)
			}
		}
		for _, w := range fallbackValues {
			rec := get(ep.path(w))
			if rec.Code != http.StatusOK {
				t.Fatalf("%s window=%q: expected 200 (fallback, not error), got %d. Body: %s", ep.name, w, rec.Code, rec.Body.String())
			}
			var body map[string]any
			if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("%s window=%q: decode failed: %v", ep.name, w, err)
			}
			if got, _ := body["window"].(string); got != "1h" {
				t.Errorf("%s window=%q: expected fallback window=\"1h\", got %q", ep.name, w, got)
			}
		}
	}
}

// TestDNSStatisticsEndpoints_RequireAuth is drilldown plan T-05 item 8 (🔒):
// all three /api/statistics/dns* endpoints must require a valid session,
// same as every other authRoute.
func TestDNSStatisticsEndpoints_RequireAuth(t *testing.T) {
	handler, _ := setupTestServer(t)

	paths := []string{
		"/api/statistics/dns",
		"/api/statistics/dns/domain?domain=example.com",
		"/api/statistics/dns/client?client=unknown",
	}
	for _, p := range paths {
		req := httptest.NewRequest("GET", p, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("path=%q without session: expected 401, got %d", p, rec.Code)
		}
	}
}

// TestHandleGetIPInfo_RequiresAuth mirrors TestDNSStatisticsEndpoints_RequireAuth
// for the new /api/statistics/ipinfo endpoint (docs/ref/todo/
// statistics-host-ipinfo-plan.md T-07).
func TestHandleGetIPInfo_RequiresAuth(t *testing.T) {
	handler, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/statistics/ipinfo?ip=8.8.8.8", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("without session: expected 401, got %d", rec.Code)
	}
}

// TestHandleGetIPInfo_InputValidationAndStates covers plan T-07's acceptance
// checklist: bad ip -> 400 without ever reaching the service, a LAN ip ->
// 400 (isGloballyRoutable rejects it), a public ip with the feature enabled
// (buildTestServer wires an enabled mock provider) -> 200 with the expected
// fields, and the feature-disabled case -> 404.
func TestHandleGetIPInfo_InputValidationAndStates(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	get := func(rawQuery string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/statistics/ipinfo?"+rawQuery, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Missing ip -> 400.
	if rec := get(""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing ip: expected 400, got %d", rec.Code)
	}

	badIPs := []string{"1.2.3", "abc", "192.168.1.1; ls", "192.168.1.256"}
	for _, ip := range badIPs {
		rec := get("ip=" + url.QueryEscape(ip))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("ip=%q: expected 400, got %d. Body: %s", ip, rec.Code, rec.Body.String())
		}
	}

	// LAN IP -> 400 (ErrIPInfoNotPublic), never reaches the provider.
	lanIPs := []string{"192.168.1.10", "10.0.0.1", "100.64.0.1", "::ffff:10.0.0.1"}
	for _, ip := range lanIPs {
		rec := get("ip=" + url.QueryEscape(ip))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("lan ip=%q: expected 400, got %d. Body: %s", ip, rec.Code, rec.Body.String())
		}
	}

	// Public IP with the feature enabled (buildTestServer's server has
	// ipinfo-enabled=true wired to a mock provider) -> 200.
	rec := get("ip=" + url.QueryEscape("8.8.8.8"))
	if rec.Code != http.StatusOK {
		t.Fatalf("public ip, feature enabled: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.IPInfoLookup
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Ip != "8.8.8.8" {
		t.Errorf("expected ip=8.8.8.8, got %+v", got)
	}
}

// TestHandleGetIPInfo_Disabled verifies ErrIPInfoDisabled maps to 404 (plan
// T-07: "ไม่ใช่ 403 เพื่อไม่บอกใบ้สถานะ config"). buildTestServer wires an
// ENABLED mock provider by default (for the other tests above), so this
// swaps the server's ipInfo field for a disabled instance directly — same
// package, no exported setter needed.
func TestHandleGetIPInfo_Disabled(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.ipInfo = service.NewIPInfoService(false, service.NewMockIPInfoProvider())
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	req := httptest.NewRequest("GET", "/api/statistics/ipinfo?ip=8.8.8.8", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("feature disabled: expected 404, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetIPReference_RequiresAuth mirrors
// TestHandleGetIPInfo_RequiresAuth for the new reference-popover endpoint
// (docs/ref/todo/reference-popover-plan.md Step 4).
func TestHandleGetIPReference_RequiresAuth(t *testing.T) {
	handler, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/statistics/reference/ip?ip=8.8.8.8", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("without session: expected 401, got %d", rec.Code)
	}
}

// TestHandleGetDomainReference_RequiresAuth mirrors
// TestHandleGetIPReference_RequiresAuth for the domain sibling route.
func TestHandleGetDomainReference_RequiresAuth(t *testing.T) {
	handler, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/statistics/reference/domain?domain=example.com", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("without session: expected 401, got %d", rec.Code)
	}
}

// TestHandleGetIPReference_InputValidation covers plan §4 item 4/Step 4's
// acceptance checklist: bad ip -> 400 without ever echoing the raw value, a
// valid IPv4-mapped IPv6 literal ("::ffff:192.168.1.1") must classify as
// scope="lan" (plan §5 Caution 1's explicit trap case), an explicit
// window=24h must never change the response's window away from the fixed
// "1h", and limit=999 must clamp rather than 400.
func TestHandleGetIPReference_InputValidation(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.statistics.SetDNSLoggingEnabled(true)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	get := func(rawQuery string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/statistics/reference/ip?"+rawQuery, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := get(""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing ip: expected 400, got %d", rec.Code)
	}

	badIPs := []string{"1.2.3", "abc", "192.168.1.1; ls", "<script>alert(1)</script>", "192.168.1.256"}
	for _, ip := range badIPs {
		rec := get("ip=" + url.QueryEscape(ip))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("ip=%q: expected 400, got %d. Body: %s", ip, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), ip) {
			t.Errorf("ip=%q: rejected value was echoed back in the response body: %s", ip, rec.Body.String())
		}
	}

	// ::ffff:192.168.1.1 must classify as lan, never public — the exact
	// bypass isPrivateIP alone would miss (plan §5 Caution 1).
	rec := get("ip=" + url.QueryEscape("::ffff:192.168.1.1"))
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.IPReferenceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Scope != model.ReferenceScopeLAN {
		t.Errorf("expected ::ffff:192.168.1.1 to classify as scope=lan, got %q", got.Scope)
	}

	// window is never accepted from the client — the response's window field
	// must always be the fixed "1h" regardless of what's passed.
	rec = get("ip=8.8.8.8&window=24h")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got2 model.IPReferenceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got2.Window != "1h" {
		t.Errorf("expected window=24h to be ignored and response window stay 1h, got %q", got2.Window)
	}

	// limit=999 clamps (never a 400).
	for _, d := range []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com"} {
		server.statistics.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: d, AnswerIP: "8.8.8.8"})
	}
	rec = get("ip=8.8.8.8&limit=999")
	if rec.Code != http.StatusOK {
		t.Fatalf("limit=999: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got3 model.IPReferenceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got3); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got3.Domains) != 4 {
		t.Errorf("expected 4 domain rows (below the clamp ceiling), got %d", len(got3.Domains))
	}
}

// TestHandleGetIPReference_QueryLoggingDisabled covers plan Step 4's
// "query logging ปิด = 200 + enabled:false" case.
func TestHandleGetIPReference_QueryLoggingDisabled(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.statistics.SetDNSLoggingEnabled(false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	req := httptest.NewRequest("GET", "/api/statistics/reference/ip?ip=8.8.8.8", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.IPReferenceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled {
		t.Errorf("expected enabled=false")
	}
	if len(got.Domains) != 0 {
		t.Errorf("expected empty domains while disabled, got %+v", got.Domains)
	}
}

// TestHandleGetDomainReference_InputValidation mirrors
// TestHandleGetIPReference_InputValidation for the domain route.
func TestHandleGetDomainReference_InputValidation(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.statistics.SetDNSLoggingEnabled(true)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	get := func(rawQuery string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/statistics/reference/domain?"+rawQuery, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	if rec := get(""); rec.Code != http.StatusBadRequest {
		t.Errorf("missing domain: expected 400, got %d", rec.Code)
	}

	badDomains := []string{"<script>alert(1)</script>", "evil.com%0Aextra", "evil.com%00", "has space.example.com"}
	for _, d := range badDomains {
		rec := get("domain=" + url.QueryEscape(d))
		if rec.Code != http.StatusBadRequest {
			t.Errorf("domain=%q: expected 400, got %d. Body: %s", d, rec.Code, rec.Body.String())
		}
		if strings.Contains(rec.Body.String(), d) {
			t.Errorf("domain=%q: rejected value was echoed back in the response body: %s", d, rec.Body.String())
		}
	}

	// window is never accepted — response window stays the fixed "1h".
	rec := get("domain=example.com&window=24h")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.DomainReferenceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Window != "1h" {
		t.Errorf("expected window=24h to be ignored, got %q", got.Window)
	}

	// A valid domain never queried -> 200 with an empty ip list, not 404.
	rec = get("domain=" + url.QueryEscape("never-queried.example.com"))
	if rec.Code != http.StatusOK {
		t.Fatalf("valid-but-unqueried domain: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got2 model.DomainReferenceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got2); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got2.IPs) != 0 {
		t.Errorf("expected empty ip list for an unqueried domain, got %+v", got2.IPs)
	}
}

// TestHandleGetDomainReference_QueryLoggingDisabled mirrors
// TestHandleGetIPReference_QueryLoggingDisabled for the domain route.
func TestHandleGetDomainReference_QueryLoggingDisabled(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.statistics.SetDNSLoggingEnabled(false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	req := httptest.NewRequest("GET", "/api/statistics/reference/domain?domain=example.com", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.DomainReferenceSummary
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled {
		t.Errorf("expected enabled=false")
	}
	if len(got.IPs) != 0 {
		t.Errorf("expected empty ips while disabled, got %+v", got.IPs)
	}
}

// TestCapacityStatisticsEndpoint is docs/ref/todo/
// statistics-capacity-visibility-plan.md T-08 (API portion): the new
// /api/statistics/capacity route must require auth (like every other
// /api/statistics/* route), return exactly 11 rings (docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-06, issue #134, added the 11th,
// firewall.logBuffer) with no PII, and treat `series` as a strict "1"/"true"
// whitelist (anything else, including missing, means false — never a 400).
func TestCapacityStatisticsEndpoint(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Requires auth.
	req := httptest.NewRequest("GET", "/api/statistics/capacity", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("without session: expected 401, got %d", rec.Code)
	}

	// Default (no series param): 11 rings, every series omitted.
	rec = get("/api/statistics/capacity")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	rings, _ := body["rings"].([]any)
	if len(rings) != 11 {
		t.Fatalf("expected 11 rings, got %d: %s", len(rings), rec.Body.String())
	}
	var foundLogBuffer bool
	for _, raw := range rings {
		row, _ := raw.(map[string]any)
		if _, hasSeries := row["series"]; hasSeries {
			t.Errorf("ring %v: expected series to be omitted when series param is absent", row["id"])
		}
		// No PII: none of these keys should ever be present on a capacity row.
		for _, forbidden := range []string{"domain", "ip", "hostname", "client", "src", "mac"} {
			if _, ok := row[forbidden]; ok {
				t.Errorf("ring %v: unexpected PII-shaped field %q in response", row["id"], forbidden)
			}
		}
		if row["id"] == "firewall.logBuffer" {
			foundLogBuffer = true
			if row["capSource"] != "traffic-log-buffer-capacity" {
				t.Errorf("firewall.logBuffer: expected capSource=traffic-log-buffer-capacity, got %v", row["capSource"])
			}
			if row["group"] != "firewall" || row["kind"] != "flat" {
				t.Errorf("firewall.logBuffer: expected group=firewall kind=flat, got group=%v kind=%v", row["group"], row["kind"])
			}
		}
	}
	if !foundLogBuffer {
		t.Errorf("expected a firewall.logBuffer ring in the response, got %s", rec.Body.String())
	}
	if rings[len(rings)-1].(map[string]any)["id"] != "firewall.logBuffer" {
		t.Errorf("expected firewall.logBuffer to be the last ring, got %s", rec.Body.String())
	}

	// series=1 must include series arrays.
	rec = get("/api/statistics/capacity?series=1")
	body = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	rings, _ = body["rings"].([]any)
	foundBucketSeries := false
	for _, raw := range rings {
		row, _ := raw.(map[string]any)
		if row["kind"] == "bucket" {
			if _, hasSeries := row["series"]; hasSeries {
				foundBucketSeries = true
			}
		}
	}
	if !foundBucketSeries {
		t.Errorf("series=1: expected at least one bucket-kind ring to carry a series array")
	}

	// series=garbage falls back to false, never a 400.
	rec = get("/api/statistics/capacity?series=garbage")
	if rec.Code != http.StatusOK {
		t.Fatalf("series=garbage: expected 200 (graceful fallback), got %d", rec.Code)
	}
}

// TestHandleGetFirewallStatistics covers the Statistics -> Firewall page
// endpoint (docs/ref/todo/statistics-firewall-page-plan.md T-07/T-06): no
// session -> 401, an unrecognized/out-of-range window/limit is normalized
// server-side rather than rejected, and the happy-path response has the
// expected top-level shape.
func TestHandleGetFirewallStatistics(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	get := func(path string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", path, nil)
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec
	}

	// Requires auth.
	req := httptest.NewRequest("GET", "/api/statistics/firewall", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("without session: expected 401, got %d", rec.Code)
	}

	// Happy path: 200, expected top-level shape, limit echoed back clamped to
	// the default when omitted.
	rec = get("/api/statistics/firewall")
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	for _, field := range []string{
		"window", "generatedAt", "available", "truncated", "acceptedBytes", "acceptedPackets",
		"blockedBytes", "blockedPackets", "blockedEvents", "rulesEnabled", "rulesUnused",
		"trend", "denyTrend", "chains", "rules", "blockedSources", "blockedPorts",
		"recentBlockedEvents", "limit",
	} {
		if _, ok := body[field]; !ok {
			t.Errorf("expected field %q in response, got %s", field, rec.Body.String())
		}
	}
	if got, _ := body["window"].(string); got != "1h" {
		t.Errorf("expected default window=1h, got %q", got)
	}
	if got, _ := body["limit"].(float64); got != 100 {
		t.Errorf("expected default limit=100, got %v", got)
	}

	// window/limit are normalized/clamped, never rejected with a 400.
	rec = get("/api/statistics/firewall?window=garbage&limit=99999")
	if rec.Code != http.StatusOK {
		t.Fatalf("window=garbage&limit=99999: expected 200 (graceful fallback), got %d", rec.Code)
	}
	body = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got, _ := body["window"].(string); got != "1h" {
		t.Errorf("window=garbage: expected fallback window=1h, got %q", got)
	}
	if got, _ := body["limit"].(float64); got != 500 {
		t.Errorf("limit=99999: expected clamp to max 500, got %v", got)
	}

	rec = get("/api/statistics/firewall?window=24h&limit=0")
	body = nil
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if got, _ := body["window"].(string); got != "24h" {
		t.Errorf("expected window=24h, got %q", got)
	}
	if got, _ := body["limit"].(float64); got != 100 {
		t.Errorf("limit=0: expected fallback to default 100, got %v", got)
	}
}
