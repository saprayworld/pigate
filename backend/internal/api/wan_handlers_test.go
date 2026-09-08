package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
	"pigate/internal/service"
)

// setupWanTestServer builds a test Server (mock kernels + in-memory DB, like
// buildTestServer) plus a WanMonitor wired via the additive SetWanMonitor
// setter, and returns the mock probe so tests can drive
// SetICMPDead/SetAllDead scenarios.
func setupWanTestServer(t *testing.T) (http.Handler, *db.Repository, *kernel.MockPathProbe, *service.WanMonitor) {
	server, repo := buildTestServer(t, false)
	probe := kernel.NewMockPathProbe()
	monitor := service.NewWanMonitor(repo, probe, service.NewEventLogService(repo), service.NewNetEventBus(), service.NewWanUplinkMetricsRing())
	server.SetWanMonitor(monitor)

	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	return handler, repo, probe, monitor
}

const wanTestAuthToken = "mock_session_id_test_token"

func validWanUplinkInputJSON() model.WanUplinkInput {
	return model.WanUplinkInput{
		Name: "Primary", Interface: "eth0", Priority: 1,
		ProbeTargets: []string{"1.1.1.1"}, ProbeMethod: model.WanProbeMethodAuto, ProbeTCPPort: 443,
		ProbeIntervalSeconds: 6, ProbeCount: 3, ProbeTimeoutMs: 1000,
		LossThresholdPct: 50, LatencyThresholdMs: 200,
		FailStrikes: 3, RecoverStrikes: 3, Status: true, Description: "main uplink",
	}
}

func TestWanUplinksCRUD(t *testing.T) {
	handler, repo, _, _ := setupWanTestServer(t)

	// 1. GET (empty)
	req := httptest.NewRequest("GET", "/api/wan/uplinks", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var list []model.WanUplink
	if err := json.Unmarshal(rec.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0 uplinks initially, got %d", len(list))
	}

	// 2. POST (create)
	body, _ := json.Marshal(validWanUplinkInputJSON())
	req = httptest.NewRequest("POST", "/api/wan/uplinks", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", rec.Code, rec.Body.String())
	}
	var created model.WanUplink
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if created.ID == "" || created.Name != "Primary" {
		t.Fatalf("unexpected created uplink: %+v", created)
	}

	dbUplink, err := repo.GetWanUplinkByID(created.ID)
	if err != nil || dbUplink == nil {
		t.Fatalf("expected uplink persisted, err=%v uplink=%v", err, dbUplink)
	}

	// 3. PUT (update)
	updateInput := validWanUplinkInputJSON()
	updateInput.Name = "Primary Renamed"
	body, _ = json.Marshal(updateInput)
	req = httptest.NewRequest("PUT", "/api/wan/uplinks/"+created.ID, bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var updated model.WanUplink
	json.Unmarshal(rec.Body.Bytes(), &updated)
	if updated.Name != "Primary Renamed" {
		t.Errorf("expected renamed, got %q", updated.Name)
	}

	// 4. PUT unknown id -> 404
	req = httptest.NewRequest("PUT", "/api/wan/uplinks/does-not-exist", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown id, got %d", rec.Code)
	}

	// 5. DELETE
	req = httptest.NewRequest("DELETE", "/api/wan/uplinks/"+created.ID, nil)
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	gone, _ := repo.GetWanUplinkByID(created.ID)
	if gone != nil {
		t.Error("expected uplink deleted")
	}

	// 6. DELETE unknown id -> 404
	req = httptest.NewRequest("DELETE", "/api/wan/uplinks/does-not-exist", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 for unknown id, got %d", rec.Code)
	}
}

func TestWanUplinkCreate_HostnameTargetRejected(t *testing.T) {
	handler, _, _, _ := setupWanTestServer(t)

	input := validWanUplinkInputJSON()
	input.ProbeTargets = []string{"google.com"}
	body, _ := json.Marshal(input)
	req := httptest.NewRequest("POST", "/api/wan/uplinks", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for a hostname probe target, got %d: %s", rec.Code, rec.Body.String())
	}
	var resp map[string]string
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if resp["message"] == "" {
		t.Error("expected a non-empty error message naming the field")
	}
}

func TestWanUplinkCreate_DuplicateInterfaceRejected(t *testing.T) {
	handler, _, _, _ := setupWanTestServer(t)

	body, _ := json.Marshal(validWanUplinkInputJSON())
	req := httptest.NewRequest("POST", "/api/wan/uplinks", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("expected first create to succeed, got %d: %s", rec.Code, rec.Body.String())
	}

	second := validWanUplinkInputJSON()
	second.Name = "Duplicate"
	body, _ = json.Marshal(second)
	req = httptest.NewRequest("POST", "/api/wan/uplinks", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a duplicate interface, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWanStatus_ReportsUnknownForNeverProbedUplink(t *testing.T) {
	handler, repo, _, _ := setupWanTestServer(t)

	created, err := repo.CreateWanUplink(validWanUplinkInputJSON())
	if err != nil {
		t.Fatalf("CreateWanUplink failed: %v", err)
	}

	req := httptest.NewRequest("GET", "/api/wan/status", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp model.WanStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(resp.Uplinks) != 1 {
		t.Fatalf("expected exactly 1 uplink in status, got %d", len(resp.Uplinks))
	}
	entry := resp.Uplinks[0]
	if entry.UplinkID != created.ID {
		t.Errorf("UplinkID = %q, want %q", entry.UplinkID, created.ID)
	}
	if entry.State != model.WanStateUnknown {
		t.Errorf("expected state=unknown for a never-probed uplink, got %q", entry.State)
	}
	if entry.Name != "Primary" {
		t.Errorf("expected Name to be carried through from config, got %q", entry.Name)
	}
}

func TestWanStatus_ReflectsMonitorState(t *testing.T) {
	handler, repo, probe, monitor := setupWanTestServer(t)
	created, err := repo.CreateWanUplink(validWanUplinkInputJSON())
	if err != nil {
		t.Fatalf("CreateWanUplink failed: %v", err)
	}
	_ = probe

	// Drive a real probe round through the monitor's own background loop
	// (its only entry point — probeUplink/tick are package-private by
	// design) and poll briefly until GetStates() reflects it.
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if len(monitor.GetStates()) > 0 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	req := httptest.NewRequest("GET", "/api/wan/status", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var resp model.WanStatusResponse
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if len(resp.Uplinks) != 1 {
		t.Fatalf("expected 1 uplink, got %d", len(resp.Uplinks))
	}
	if resp.Uplinks[0].UplinkID != created.ID {
		t.Errorf("UplinkID mismatch: %q vs %q", resp.Uplinks[0].UplinkID, created.ID)
	}
	if resp.Uplinks[0].State == "" {
		t.Error("expected a non-empty state after a probe round")
	}
}

func TestWanMetrics_RequiresUplinkParam(t *testing.T) {
	handler, _, _, _ := setupWanTestServer(t)
	req := httptest.NewRequest("GET", "/api/wan/metrics?window=1h", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 when uplink param is missing, got %d", rec.Code)
	}
}

func TestWanMetrics_UnknownUplinkReturnsEmptySeriesNotError(t *testing.T) {
	handler, _, _, _ := setupWanTestServer(t)
	req := httptest.NewRequest("GET", "/api/wan/metrics?uplink=does-not-exist&window=1h", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 with an empty/zero series, got %d: %s", rec.Code, rec.Body.String())
	}
	var points []model.WanMetricPoint
	if err := json.Unmarshal(rec.Body.Bytes(), &points); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if len(points) == 0 {
		t.Error("expected a full zero-valued window, not an empty array")
	}
}

func TestWanUplinksRequireAuth(t *testing.T) {
	handler, _, _, _ := setupWanTestServer(t)
	req := httptest.NewRequest("GET", "/api/wan/uplinks", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without a session, got %d", rec.Code)
	}
}

// =========================================================================
// Task 16: kill switch / manual override / Phase 2 status fields
// =========================================================================

// setupWanFailoverTestServer builds on setupWanTestServer, additionally
// wiring a real service.WanFailoverController (SetWanFailover) so PUT/POST
// /api/wan/failover and the Phase 2 fields on GET /api/wan/status can be
// exercised end-to-end, not just degrade-to-zero-value.
func setupWanFailoverTestServer(t *testing.T) (http.Handler, *db.Repository, *kernel.MockPathProbe, *service.WanMonitor, *service.WanFailoverController) {
	server, repo := buildTestServer(t, false)
	probe := kernel.NewMockPathProbe()
	bus := service.NewNetEventBus()
	eventLog := service.NewEventLogService(repo)
	monitor := service.NewWanMonitor(repo, probe, eventLog, bus, service.NewWanUplinkMetricsRing())
	server.SetWanMonitor(monitor)

	controller := service.NewWanFailoverController(repo, monitor, server.routingService, eventLog, bus)
	server.SetWanFailover(controller)

	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	return handler, repo, probe, monitor, controller
}

func TestWanFailoverSettings_GetAndUpdate(t *testing.T) {
	handler, repo, _, _, _ := setupWanFailoverTestServer(t)

	// GET default settings (enabled=false, mode=auto — the migration seed,
	// docs/ref/todo/multi-wan-failover-plan.md Caution 9: installing this
	// feature must not change behavior on an existing deployment).
	req := httptest.NewRequest("GET", "/api/wan/failover", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}
	var settings model.WanFailoverSettings
	if err := json.Unmarshal(rec.Body.Bytes(), &settings); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if settings.Enabled {
		t.Error("expected default Enabled=false (kill switch off by default)")
	}
	if settings.Mode != model.WanFailoverModeAuto {
		t.Errorf("expected default mode=auto, got %q", settings.Mode)
	}

	uplink, err := repo.CreateWanUplink(validWanUplinkInputJSON())
	if err != nil {
		t.Fatalf("CreateWanUplink failed: %v", err)
	}
	update := model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: uplink.ID, MinHoldSeconds: 30, RevertDelaySeconds: 60}
	body, _ := json.Marshal(update)
	req = httptest.NewRequest("PUT", "/api/wan/failover", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	saved, err := repo.GetWanFailoverSettings()
	if err != nil {
		t.Fatalf("GetWanFailoverSettings failed: %v", err)
	}
	if !saved.Enabled || saved.Mode != model.WanFailoverModeManual || saved.ManualUplinkID != uplink.ID || saved.MinHoldSeconds != 30 || saved.RevertDelaySeconds != 60 {
		t.Errorf("settings not persisted correctly: %+v", saved)
	}
}

func TestWanFailoverSettings_ManualModeValidation(t *testing.T) {
	handler, _, _, _, _ := setupWanFailoverTestServer(t)

	// manualUplinkId empty -> 400
	body, _ := json.Marshal(model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeManual})
	req := httptest.NewRequest("PUT", "/api/wan/failover", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty manualUplinkId in manual mode, got %d: %s", rec.Code, rec.Body.String())
	}

	// manualUplinkId doesn't refer to a real uplink -> 400
	body, _ = json.Marshal(model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeManual, ManualUplinkID: "does-not-exist"})
	req = httptest.NewRequest("PUT", "/api/wan/failover", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a manualUplinkId that does not exist, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestWanFailoverOverride_SetsManualModeAndUplink(t *testing.T) {
	handler, repo, _, _, _ := setupWanFailoverTestServer(t)
	uplink, err := repo.CreateWanUplink(validWanUplinkInputJSON())
	if err != nil {
		t.Fatalf("CreateWanUplink failed: %v", err)
	}

	body, _ := json.Marshal(map[string]string{"uplinkId": uplink.ID})
	req := httptest.NewRequest("POST", "/api/wan/failover/override", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", rec.Code, rec.Body.String())
	}

	saved, err := repo.GetWanFailoverSettings()
	if err != nil {
		t.Fatalf("GetWanFailoverSettings failed: %v", err)
	}
	if !saved.Enabled || saved.Mode != model.WanFailoverModeManual || saved.ManualUplinkID != uplink.ID {
		t.Errorf("expected manual override to set enabled=true mode=manual manualUplinkId=%q, got %+v", uplink.ID, saved)
	}

	// Empty uplinkId -> 400
	body, _ = json.Marshal(map[string]string{"uplinkId": ""})
	req = httptest.NewRequest("POST", "/api/wan/failover/override", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for empty uplinkId, got %d: %s", rec.Code, rec.Body.String())
	}

	// uplinkId that does not exist -> 400
	body, _ = json.Marshal(map[string]string{"uplinkId": "does-not-exist"})
	req = httptest.NewRequest("POST", "/api/wan/failover/override", bytes.NewBuffer(body))
	addSessionCookie(req, wanTestAuthToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("expected 400 for a nonexistent uplinkId, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestWanFailover_ReadOnlyAdminCanReadButNotMutate covers D-8: GET routes
// (both /api/wan/status and /api/wan/failover) stay authRoute (readable by
// admin_readonly), while PUT /api/wan/failover and POST
// /api/wan/failover/override are superAdminRoute (403 for admin_readonly).
func TestWanFailover_ReadOnlyAdminCanReadButNotMutate(t *testing.T) {
	handler, repo, _, _, _ := setupWanFailoverTestServer(t)
	if err := repo.CreateUser(model.User{
		ID: "user-wan-viewer", Username: "wanviewer", PasswordHash: "x",
		Role: model.RoleAdminReadonly, Status: model.StatusActive,
	}); err != nil {
		t.Fatalf("create viewer user: %v", err)
	}
	AddSession("wan_viewer_token", "wanviewer")
	token := "wan_viewer_token"

	req := httptest.NewRequest("GET", "/api/wan/status", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api/wan/status as read-only admin, got %d", rec.Code)
	}

	req = httptest.NewRequest("GET", "/api/wan/failover", nil)
	addSessionCookie(req, token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET /api/wan/failover as read-only admin, got %d", rec.Code)
	}

	body, _ := json.Marshal(model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto})
	req = httptest.NewRequest("PUT", "/api/wan/failover", bytes.NewBuffer(body))
	addSessionCookie(req, token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for PUT /api/wan/failover as read-only admin, got %d: %s", rec.Code, rec.Body.String())
	}

	body, _ = json.Marshal(map[string]string{"uplinkId": "whatever"})
	req = httptest.NewRequest("POST", "/api/wan/failover/override", bytes.NewBuffer(body))
	addSessionCookie(req, token)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("expected 403 for POST /api/wan/failover/override as read-only admin, got %d: %s", rec.Code, rec.Body.String())
	}
}

// TestWanFailoverDisableEditBlocksMutation mirrors
// TestDNSBlocklistDisableEditBlocksMutation — GET must still work in
// -disable-edit=true mode, both mutating WAN failover routes must be 403.
func TestWanFailoverDisableEditBlocksMutation(t *testing.T) {
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

	req := httptest.NewRequest("GET", "/api/wan/failover", nil)
	addSessionCookie(req, token)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for GET in disable-edit mode, got %d: %s", rec.Code, rec.Body.String())
	}

	cases := []struct {
		method, path string
		body         any
	}{
		{"PUT", "/api/wan/failover", model.WanFailoverSettings{Enabled: true, Mode: model.WanFailoverModeAuto}},
		{"POST", "/api/wan/failover/override", map[string]string{"uplinkId": "whatever"}},
	}
	for _, c := range cases {
		body, _ := json.Marshal(c.body)
		req := httptest.NewRequest(c.method, c.path, bytes.NewBuffer(body))
		addSessionCookie(req, token)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s: expected 403 in disable-edit mode, got %d: %s", c.method, c.path, rec.Code, rec.Body.String())
		}
	}
}

// TestWanStatus_Phase2FieldsPopulatedFromController drives a real
// WanFailoverController (via Start(ctx), polling like
// TestWanStatus_ReflectsMonitorState does for the monitor) and confirms
// GET /api/wan/status's four Phase 2 fields end up populated from it.
func TestWanStatus_Phase2FieldsPopulatedFromController(t *testing.T) {
	handler, repo, _, monitor, controller := setupWanFailoverTestServer(t)

	created, err := repo.CreateWanUplink(validWanUplinkInputJSON())
	if err != nil {
		t.Fatalf("CreateWanUplink failed: %v", err)
	}
	if err := repo.UpdateWanFailoverSettings(model.WanFailoverSettings{
		Enabled: true, Mode: model.WanFailoverModeAuto, MinHoldSeconds: 0, RevertDelaySeconds: 0,
	}); err != nil {
		t.Fatalf("UpdateWanFailoverSettings failed: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	monitor.Start(ctx)
	controller.Start(ctx)

	// wanMonitorTickInterval=1s + wanFailoverTickInterval=2s should resolve
	// this within ~3s in the common case; a generous 15s deadline avoids
	// flakiness under a loaded CI/dev machine.
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if controller.Status().ActiveUplinkID == created.ID {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	req := httptest.NewRequest("GET", "/api/wan/status", nil)
	addSessionCookie(req, wanTestAuthToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	var resp model.WanStatusResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("decode failed: %v", err)
	}
	if resp.ActiveUplinkID != created.ID {
		t.Fatalf("expected activeUplinkId=%q, got %q (lastSwitchReason=%q)", created.ID, resp.ActiveUplinkID, resp.LastSwitchReason)
	}
	if resp.LastSwitchAt == "" {
		t.Error("expected a non-empty lastSwitchAt once a decision has been made")
	}
	if resp.LastSwitchReason == "" {
		t.Error("expected a non-empty lastSwitchReason once a decision has been made")
	}

	// Regression guard: each per-uplink entry's own Active field (WanMonitor
	// itself never sets it — see WanUplinkState's doc comment) must be
	// derived from the top-level ActiveUplinkID, not left permanently false.
	if len(resp.Uplinks) != 1 || !resp.Uplinks[0].Active {
		t.Fatalf("expected the single uplink entry's Active field to be true once it is the active uplink, got %+v", resp.Uplinks)
	}
}
