package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pigate/internal/model"
)

// TestDNSAndDHCPInjectionRejected verifies the create handlers reject values
// carrying an embedded newline (a dnsmasq directive injection) with 400, and
// accept a clean value.
func TestDNSAndDHCPInjectionRejected(t *testing.T) {
	handler, repo := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// A zone to hang records off of.
	zone := model.DNSZone{ID: "zone-test", ZoneName: "test.local", IsAuthoritative: true, Enabled: true}
	if err := repo.CreateDNSZone(zone); err != nil {
		t.Fatalf("seed zone: %v", err)
	}

	post := func(path string, payload any) int {
		body, _ := json.Marshal(payload)
		req := httptest.NewRequest("POST", path, bytes.NewBuffer(body))
		addSessionCookie(req, authToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		return rec.Code
	}

	// Injected A record → 400.
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "www", Type: "A", Value: "1.2.3.4\naddress=/evil/6.6.6.6",
	}); code != http.StatusBadRequest {
		t.Errorf("injected DNS record: expected 400, got %d", code)
	}

	// Clean A record → not 400.
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "www", Type: "A", Value: "192.168.1.10",
	}); code == http.StatusBadRequest {
		t.Errorf("valid DNS record was rejected with 400")
	}

	// Injected reservation device name → 400.
	if code := post("/api/dhcp/reservations", model.DhcpReservationInput{
		DeviceName: "pc\ndhcp-host=evil", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.1.50",
	}); code != http.StatusBadRequest {
		t.Errorf("injected reservation name: expected 400, got %d", code)
	}

	// Clean reservation → not 400.
	if code := post("/api/dhcp/reservations", model.DhcpReservationInput{
		DeviceName: "My Laptop", MacAddress: "aa:bb:cc:dd:ee:ff", IPAddress: "192.168.1.50",
	}); code == http.StatusBadRequest {
		t.Errorf("valid reservation was rejected with 400")
	}
}
