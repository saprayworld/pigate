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

// testHostsBody is a small hosts-format sample used across the tests below —
// one builtin entry (excluded), two ordinary domains.
const testHostsBody = "127.0.0.1 localhost\n0.0.0.0 ads.example.com\n0.0.0.0 tracker.example.net\n"

// uploadBlocklistReq issues a raw-body POST to /api/dns/blocklists/upload,
// mirroring the contract HandleUploadDNSBlocklist expects (?name=, optional
// ?blockMode=, Content-Type: text/plain, body = the hosts file bytes).
func uploadBlocklistReq(t *testing.T, handler http.Handler, token, name, blockMode, body string) *httptest.ResponseRecorder {
	t.Helper()
	url := "/api/dns/blocklists/upload?name=" + name
	if blockMode != "" {
		url += "&blockMode=" + blockMode
	}
	req := httptest.NewRequest("POST", url, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "text/plain")
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestDNSBlocklistUploadAndLifecycle exercises the full CRUD+toggle path
// through an upload-sourced list (no outbound network needed, unlike
// CreateFromURL) — create, list, toggle, switch blockMode via update, delete.
func TestDNSBlocklistUploadAndLifecycle(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	rec := uploadBlocklistReq(t, handler, token, "TestList", "", testHostsBody)
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var created model.DNSBlocklist
	if err := json.NewDecoder(rec.Body).Decode(&created); err != nil {
		t.Fatalf("decode create response: %v", err)
	}
	if created.BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("expected default blockMode sinkhole, got %q", created.BlockMode)
	}
	if created.DomainCount != 2 {
		t.Errorf("expected 2 domains parsed (localhost excluded as builtin), got %d", created.DomainCount)
	}

	// GET reflects the new list.
	rec = vlanReq(t, handler, "GET", "/api/dns/blocklists", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET blocklists: expected 200, got %d", rec.Code)
	}
	var list []model.DNSBlocklist
	if err := json.NewDecoder(rec.Body).Decode(&list); err != nil {
		t.Fatalf("decode list response: %v", err)
	}
	found := false
	for _, l := range list {
		if l.ID == created.ID {
			found = true
		}
	}
	if !found {
		t.Errorf("created blocklist %q not found in GET list", created.ID)
	}

	// Toggle enabled off.
	rec = vlanReq(t, handler, "POST", "/api/dns/blocklists/"+created.ID+"/toggle", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	// Update: rename and switch to nxdomain mode without re-fetching/uploading.
	rec = vlanReq(t, handler, "PUT", "/api/dns/blocklists/"+created.ID, token, model.DNSBlocklistInput{
		Name: "TestList Renamed", BlockMode: model.DNSBlockModeNXDomain, Enabled: false,
	})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var updated model.DNSBlocklist
	if err := json.NewDecoder(rec.Body).Decode(&updated); err != nil {
		t.Fatalf("decode update response: %v", err)
	}
	if updated.BlockMode != model.DNSBlockModeNXDomain {
		t.Errorf("expected blockMode nxdomain after update, got %q", updated.BlockMode)
	}
	if updated.Name != "TestList Renamed" {
		t.Errorf("expected renamed list, got %q", updated.Name)
	}

	// Delete.
	rec = vlanReq(t, handler, "DELETE", "/api/dns/blocklists/"+created.ID, token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("delete: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestDNSBlocklistInvalidBlockMode verifies an unrecognized blockMode value
// is rejected with 400 (readable, not a 500) on every entry point that
// accepts one — create (JSON body), upload (?blockMode=), update (JSON body)
// — per docs/ref/todo/dns-blocklist-import-plan.md T-08 acceptance.
func TestDNSBlocklistInvalidBlockMode(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	rec := vlanReq(t, handler, "POST", "/api/dns/blocklists", token, model.DNSBlocklistInput{
		Name: "Bad Mode", URL: "https://example.com/hosts", BlockMode: "bogus", Enabled: false,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("create with bogus blockMode: expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	rec = uploadBlocklistReq(t, handler, token, "BadUpload", "bogus", testHostsBody)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("upload with bogus blockMode: expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	created := uploadBlocklistReq(t, handler, token, "ForUpdate", "", testHostsBody)
	if created.Code != http.StatusOK {
		t.Fatalf("seed upload for update test: expected 200, got %d. Body: %s", created.Code, created.Body.String())
	}
	var entry model.DNSBlocklist
	if err := json.NewDecoder(created.Body).Decode(&entry); err != nil {
		t.Fatalf("decode seed upload response: %v", err)
	}
	rec = vlanReq(t, handler, "PUT", "/api/dns/blocklists/"+entry.ID, token, model.DNSBlocklistInput{
		Name: entry.Name, BlockMode: "bogus", Enabled: entry.Enabled,
	})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("update with bogus blockMode: expected 400, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestDNSBlocklistRoleReadOnlyForbidsMutation verifies a non-super_admin
// (admin_readonly) role can GET the blocklists but every mutation — including
// upload, which carries no JSON body — is 403.
func TestDNSBlocklistRoleReadOnlyForbidsMutation(t *testing.T) {
	handler, repo := setupTestServer(t)
	if err := repo.CreateUser(model.User{
		ID: "user-blocklist-viewer", Username: "blviewer", PasswordHash: "x",
		Role: model.RoleAdminReadonly, Status: model.StatusActive,
	}); err != nil {
		t.Fatalf("create viewer user: %v", err)
	}
	AddSession("blocklist_viewer_token", "blviewer")
	token := "blocklist_viewer_token"

	rec := vlanReq(t, handler, "GET", "/api/dns/blocklists", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read-only GET blocklists: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/dns/blocklists", model.DNSBlocklistInput{Name: "X", URL: "https://example.com/hosts"}},
		{"PUT", "/api/dns/blocklists/bl-doesnotexist", model.DNSBlocklistInput{Name: "X"}},
		{"DELETE", "/api/dns/blocklists/bl-doesnotexist", nil},
		{"POST", "/api/dns/blocklists/bl-doesnotexist/toggle", nil},
		{"POST", "/api/dns/blocklists/bl-doesnotexist/refresh", nil},
	}
	for _, c := range cases {
		rec := vlanReq(t, handler, c.method, c.path, token, c.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403 for read-only admin, got %d. Body: %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	rec = uploadBlocklistReq(t, handler, token, "X", "", testHostsBody)
	if rec.Code != http.StatusForbidden {
		t.Errorf("upload: expected 403 for read-only admin, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestDNSBlocklistDisableEditBlocksMutation mirrors TestDisableEditMode
// (handlers_test.go) but targets the new blocklist routes specifically —
// GET must still work in -disable-edit=true mode, every mutation (including
// the raw-body upload endpoint) must be blocked by DisableEditMiddleware.
func TestDNSBlocklistDisableEditBlocksMutation(t *testing.T) {
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

	dnsBlocklistService := service.NewDNSBlocklistService(repo, dnsServer)
	if err := dnsBlocklistService.Load(); err != nil {
		t.Fatalf("load blocklist manifest: %v", err)
	}

	// disableEdit = true (matches TestDisableEditMode's server construction).
	server := NewServer(repo, fw, net, rt, dhcp, ringBuffer, true, false, ifaceService, service.NewDhcpcdService(repo, ifaceService, dhcpcdMgr), routingService, fwService, dnsService, qosService, dhcpServerService, dnsServerService, hostnameService, timeService, service.NewUserService(repo), nil, service.NewSystemStatusService(kernel.NewMockSystemStats(), repo, hostnameService, timeService, "test"), service.NewPowerService(kernel.NewMockPowerManager()), service.NewEventLogService(repo), testHealthChecker, nil, nil, nil, nil, nil, nil, dnsBlocklistService)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	rec := vlanReq(t, handler, "GET", "/api/dns/blocklists", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET blocklists in disable-edit mode: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		method, path string
		body         any
	}{
		{"POST", "/api/dns/blocklists", model.DNSBlocklistInput{Name: "X", URL: "https://example.com/hosts"}},
		{"PUT", "/api/dns/blocklists/bl-doesnotexist", model.DNSBlocklistInput{Name: "X"}},
		{"DELETE", "/api/dns/blocklists/bl-doesnotexist", nil},
		{"POST", "/api/dns/blocklists/bl-doesnotexist/toggle", nil},
		{"POST", "/api/dns/blocklists/bl-doesnotexist/refresh", nil},
	}
	for _, c := range cases {
		rec := vlanReq(t, handler, c.method, c.path, token, c.body)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403 in disable-edit mode, got %d. Body: %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}

	rec = uploadBlocklistReq(t, handler, token, "X", "", testHostsBody)
	if rec.Code != http.StatusForbidden {
		t.Errorf("upload: expected 403 in disable-edit mode, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}
