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

	// Clean A record → 200 (HandleCreateDNSRecord succeeds with http.StatusOK).
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "www", Type: "A", Value: "192.168.1.10",
	}); code != http.StatusOK {
		t.Errorf("valid DNS record was rejected: expected 200, got %d", code)
	}

	// Injected NS record → 400.
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "@", Type: "NS", Value: "ns1\ndns-rr=evil",
	}); code != http.StatusBadRequest {
		t.Errorf("injected NS record: expected 400, got %d", code)
	}

	// Clean NS record → 200 (HandleCreateDNSRecord succeeds with http.StatusOK).
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "@", Type: "NS", Value: "ns1.example.com",
	}); code != http.StatusOK {
		t.Errorf("valid NS record was rejected: expected 200, got %d", code)
	}

	// NS-delegation glue IPs (docs/ref/todo/dns-ns-delegation-plan.md T-14).

	// Injected glue IP → 400.
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "sub", Type: "NS", Value: "ns1.example.com", GlueIPs: []string{"1.2.3.4\nserver=/evil/6.6.6.6"},
	}); code != http.StatusBadRequest {
		t.Errorf("injected NS glue IP: expected 400, got %d", code)
	}

	// NS at the zone apex ("@") with a glue IP → 400 (apex guard, §2.5).
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "@", Type: "NS", Value: "ns1.example.com", GlueIPs: []string{"203.0.113.53"},
	}); code != http.StatusBadRequest {
		t.Errorf("NS glue IP at zone apex: expected 400, got %d", code)
	}

	// NS at a subdomain with a glue IP → not 400 (the actual delegation path).
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "sub", Type: "NS", Value: "ns1.example.com", GlueIPs: []string{"203.0.113.53"},
	}); code == http.StatusBadRequest {
		t.Errorf("valid NS glue IP at subdomain was rejected with 400")
	}

	// glueIps on a non-NS record type → 400.
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "www2", Type: "A", Value: "1.2.3.4", GlueIPs: []string{"1.2.3.4"},
	}); code != http.StatusBadRequest {
		t.Errorf("glueIps on A record: expected 400, got %d", code)
	}

	// NS delegation mode (docs/ref/todo/dns-ns-delegation-cname-fix-plan.md
	// T-12).

	// NS at a subdomain with delegationMode=upstream, no glueIps → not 400
	// (the whole point of this feature).
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "sub2", Type: "NS", Value: "ns1.example.com", DelegationMode: "upstream",
	}); code == http.StatusBadRequest {
		t.Errorf("valid NS upstream delegation at subdomain was rejected with 400")
	}

	// NS at the zone apex ("@") with delegationMode=upstream and NO glueIps →
	// 400 (apex guard must fire even without glue — this is the regression
	// guard for nsRecordEmitsDelegation/T-05, Caution 1).
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "@", Type: "NS", Value: "ns1.example.com", DelegationMode: "upstream",
	}); code != http.StatusBadRequest {
		t.Errorf("NS upstream delegation at zone apex (no glue): expected 400, got %d", code)
	}

	// Injected delegationMode → 400.
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "sub3", Type: "NS", Value: "ns1.example.com", DelegationMode: "upstream\nserver=/evil/6.6.6.6",
	}); code != http.StatusBadRequest {
		t.Errorf("injected NS delegationMode: expected 400, got %d", code)
	}

	// delegationMode on a non-NS record type → 400.
	if code := post("/api/dns/zones/zone-test/records", model.DNSRecordInput{
		Name: "www3", Type: "A", Value: "1.2.3.4", DelegationMode: "upstream",
	}); code != http.StatusBadRequest {
		t.Errorf("delegationMode on A record: expected 400, got %d", code)
	}

	// PUT an existing record to delegationMode=upstream at the zone apex → 400.
	{
		existing := model.DNSRecord{ID: "rec-put-apex-test", ZoneID: zone.ID, Name: "sub4", Type: "NS", Value: "ns1.example.com", TTL: 300}
		if err := repo.CreateDNSRecord(existing); err != nil {
			t.Fatalf("seed record for PUT test: %v", err)
		}
		body, _ := json.Marshal(model.DNSRecordInput{Name: "@", Type: "NS", Value: "ns1.example.com", DelegationMode: "upstream"})
		req := httptest.NewRequest("PUT", "/api/dns/records/"+existing.ID, bytes.NewBuffer(body))
		addSessionCookie(req, authToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("PUT NS upstream delegation to zone apex: expected 400, got %d", rec.Code)
		}
	}

	// GET /api/dns/resolve-ns with a newline-injected name → 400 (rejected by
	// model.EncodeDNSNameHex before any DNS lookup is issued).
	{
		req := httptest.NewRequest("GET", "/api/dns/resolve-ns?name=ns1%0Aevil", nil)
		addSessionCookie(req, authToken)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("resolve-ns with injected name: expected 400, got %d", rec.Code)
		}
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

	// Injected DHCP scope startIp → 400.
	if code := post("/api/dhcp/configs", model.DhcpConfig{
		Interface: "eth0", StartIP: "192.168.1.10\naddress=/evil/6.6.6.6", EndIP: "192.168.1.200",
	}); code != http.StatusBadRequest {
		t.Errorf("injected DHCP config startIp: expected 400, got %d", code)
	}

	// Clean DHCP scope → not 400.
	if code := post("/api/dhcp/configs", model.DhcpConfig{
		Interface: "eth0", StartIP: "192.168.1.10", EndIP: "192.168.1.200",
		Gateway: "192.168.1.1", Netmask: "255.255.255.0", DNS1: "1.1.1.1",
	}); code == http.StatusBadRequest {
		t.Errorf("valid DHCP config was rejected with 400")
	}

	// Injected blocked-domain domain → 400 and no row written (docs/ref/todo/
	// dns-blocked-domains-plan.md T-07 item 3).
	if code := post("/api/dns/blocked-domains", model.BlockedDomainInput{
		Domain: "ads.example.com\nserver=/evil/6.6.6.6", Mode: model.DNSBlockModeNXDomain, Enabled: true,
	}); code != http.StatusBadRequest {
		t.Errorf("injected blocked domain: expected 400, got %d", code)
	}
	if domains, err := repo.GetBlockedDomains(); err != nil {
		t.Fatalf("GetBlockedDomains: %v", err)
	} else if len(domains) != 0 {
		t.Errorf("expected no blocked domain rows after rejected injection, got %d", len(domains))
	}

	// Clean blocked domain → not 400.
	if code := post("/api/dns/blocked-domains", model.BlockedDomainInput{
		Domain: "ads.example.com", Mode: model.DNSBlockModeNXDomain, Enabled: true,
	}); code == http.StatusBadRequest {
		t.Errorf("valid blocked domain was rejected with 400")
	}
}
