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

	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer)
	handler := RegisterRoutes(server)

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
	loginPayload := model.LoginRequest{Username: "admin", Password: "wrong_password"}
	body, _ := json.Marshal(loginPayload)
	req := httptest.NewRequest("POST", "/api/auth/login", bytes.NewBuffer(body))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("Expected unauthorized status 401, got %d", rec.Code)
	}

	// 2. Attempt login with correct password
	loginPayload.Password = "admin"
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
	handler, _ := setupTestServer(t)
	authToken := "mock_session_id_test_token"

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
