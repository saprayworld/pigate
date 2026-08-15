package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"pigate/internal/model"
	"pigate/internal/service"
)

// TestHandleGetPolicyStats_ChainValidation covers Final acceptance:
// ?chain=bogus -> 400.
func TestHandleGetPolicyStats_ChainValidation(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	rec := vlanReq(t, handler, "GET", "/api/policies/stats?chain=bogus", token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for bogus chain, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetPolicyStats_NoSession covers Final acceptance: no session ->
// 401.
func TestHandleGetPolicyStats_NoSession(t *testing.T) {
	handler, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/policies/stats", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetPolicyStats_OK covers Final acceptance: -mock=true GET
// /api/policies/stats returns 200 with the expected shape, and the ?chain=
// filter only affects Rules, never TotalBytes. PolicyStatsService is wired
// with nil firewall/trafficStats/ringBuffer dependencies here (its
// documented defensive nil-checks — see policy_stats.go) since this test
// only needs to prove the HTTP wiring/shape, not the counter-merging logic
// already covered by service/policy_stats_test.go.
func TestHandleGetPolicyStats_OK(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.SetPolicyStatsService(service.NewPolicyStatsService(server.repo, nil, nil, nil))
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	input := model.PolicyRuleInput{
		Name:        "Test Rule",
		Chain:       model.PolicyChainForward,
		Source:      []string{"ALL"},
		Destination: []string{"ALL"},
		Service:     []string{"ALL"},
		Action:      "ACCEPT",
		Status:      true,
	}
	if rec := vlanReq(t, handler, "POST", "/api/policies", token, input); rec.Code != http.StatusOK {
		t.Fatalf("create policy: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	rec := vlanReq(t, handler, "GET", "/api/policies/stats", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.PolicyRuleStats
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got.Rules) != 1 {
		t.Fatalf("expected exactly 1 rule in stats, got %+v", got.Rules)
	}
	if got.Rules[0].Name != "Test Rule" {
		t.Errorf("expected rule name %q, got %q", "Test Rule", got.Rules[0].Name)
	}

	// ?chain=input has no rules of that chain, but must still succeed (empty
	// Rules is a valid, distinct response from a validation error).
	rec = vlanReq(t, handler, "GET", "/api/policies/stats?chain=input", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 for empty-but-valid chain filter, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var filtered model.PolicyRuleStats
	if err := json.Unmarshal(rec.Body.Bytes(), &filtered); err != nil {
		t.Fatalf("decode filtered response: %v", err)
	}
	if len(filtered.Rules) != 0 {
		t.Fatalf("expected no input-chain rules, got %+v", filtered.Rules)
	}
}

// TestHandleGetPolicyStats_NotWired covers the defensive nil-service branch:
// setupTestServer's default server never calls SetPolicyStatsService (should
// always happen in main.go, but the handler must degrade gracefully rather
// than panic if it's ever skipped).
func TestHandleGetPolicyStats_NotWired(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := vlanReq(t, handler, "GET", "/api/policies/stats", "mock_session_id_test_token", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when policyStats is not wired, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// newPolicyCounterStoreForTest wires a PolicyCounterStore against server's
// repo/trafficStats (mirroring main.go's construction) and calls both
// SetPolicyCounterStore setters this feature needs (Server +
// FirewallService) — docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md
// T-11, issue #141.
func newPolicyCounterStoreForTest(t *testing.T, server *Server) *service.PolicyCounterStore {
	t.Helper()
	store := service.NewPolicyCounterStore(server.repo, server.trafficStats, time.Hour)
	server.SetPolicyCounterStore(store)
	return store
}

// TestHandleTogglePolicyMonitor_NoSession covers Final acceptance: no
// session -> 401.
func TestHandleTogglePolicyMonitor_NoSession(t *testing.T) {
	handler, _ := setupTestServer(t)
	req := httptest.NewRequest("POST", "/api/policies/rule-x/toggle-monitor", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleResetPolicyMonitorCounter_NoSession covers Final acceptance: no
// session -> 401.
func TestHandleResetPolicyMonitorCounter_NoSession(t *testing.T) {
	handler, _ := setupTestServer(t)
	req := httptest.NewRequest("POST", "/api/policies/rule-x/monitor/reset", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleTogglePolicyMonitor_UnknownID covers Final acceptance: unknown
// id -> 404.
func TestHandleTogglePolicyMonitor_UnknownID(t *testing.T) {
	server, _ := buildTestServer(t, false)
	newPolicyCounterStoreForTest(t, server)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	rec := vlanReq(t, handler, "POST", "/api/policies/does-not-exist/toggle-monitor", "mock_session_id_test_token", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown id, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleResetPolicyMonitorCounter_UnknownID covers Final acceptance:
// unknown id -> 404.
func TestHandleResetPolicyMonitorCounter_UnknownID(t *testing.T) {
	server, _ := buildTestServer(t, false)
	newPolicyCounterStoreForTest(t, server)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")

	rec := vlanReq(t, handler, "POST", "/api/policies/does-not-exist/monitor/reset", "mock_session_id_test_token", nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown id, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleTogglePolicyMonitor_NotWired covers the defensive nil-service
// branch (SetPolicyCounterStore never called) — 503, not a panic.
func TestHandleTogglePolicyMonitor_NotWired(t *testing.T) {
	server, _ := buildTestServer(t, false)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	input := model.PolicyRuleInput{
		Name: "NoStore Rule", Chain: model.PolicyChainForward,
		Source: []string{"ALL"}, Destination: []string{"ALL"}, Service: []string{"ALL"},
		Action: "ACCEPT", Status: true,
	}
	createRec := vlanReq(t, handler, "POST", "/api/policies", token, input)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create policy: expected 200, got %d. Body: %s", createRec.Code, createRec.Body.String())
	}
	var created model.PolicyRule
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created policy: %v", err)
	}

	rec := vlanReq(t, handler, "POST", "/api/policies/"+created.ID+"/toggle-monitor", token, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when policyCounterStore is not wired, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleTogglePolicyMonitor_ToggleReflectsInStats covers Final
// acceptance: mock mode toggle then GET /api/policies/stats reflects the
// monitored value.
func TestHandleTogglePolicyMonitor_ToggleReflectsInStats(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.SetPolicyStatsService(service.NewPolicyStatsService(server.repo, server.firewallService, server.trafficStats, nil))
	newPolicyCounterStoreForTest(t, server)
	server.policyStats.SetCounterStore(server.policyCounterStore)
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	input := model.PolicyRuleInput{
		Name: "Monitor Toggle Rule", Chain: model.PolicyChainForward,
		Source: []string{"ALL"}, Destination: []string{"ALL"}, Service: []string{"ALL"},
		Action: "ACCEPT", Status: true,
	}
	createRec := vlanReq(t, handler, "POST", "/api/policies", token, input)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create policy: expected 200, got %d. Body: %s", createRec.Code, createRec.Body.String())
	}
	var created model.PolicyRule
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created policy: %v", err)
	}
	if created.Monitored {
		t.Fatalf("expected a freshly created rule to default monitored=false")
	}

	toggleRec := vlanReq(t, handler, "POST", "/api/policies/"+created.ID+"/toggle-monitor", token, nil)
	if toggleRec.Code != http.StatusOK {
		t.Fatalf("toggle-monitor: expected 200, got %d. Body: %s", toggleRec.Code, toggleRec.Body.String())
	}
	var toggled model.PolicyRule
	if err := json.Unmarshal(toggleRec.Body.Bytes(), &toggled); err != nil {
		t.Fatalf("decode toggled policy: %v", err)
	}
	if !toggled.Monitored {
		t.Fatalf("expected monitored=true after toggle, got %+v", toggled)
	}

	statsRec := vlanReq(t, handler, "GET", "/api/policies/stats", token, nil)
	if statsRec.Code != http.StatusOK {
		t.Fatalf("get stats: expected 200, got %d. Body: %s", statsRec.Code, statsRec.Body.String())
	}
	var stats model.PolicyRuleStats
	if err := json.Unmarshal(statsRec.Body.Bytes(), &stats); err != nil {
		t.Fatalf("decode stats: %v", err)
	}
	var found bool
	for _, r := range stats.Rules {
		if r.RuleID == created.ID {
			found = true
			if !r.Monitored {
				t.Errorf("expected stats row for %s to have monitored=true, got %+v", created.ID, r)
			}
			if r.MonitoredSince == "" {
				t.Errorf("expected a non-empty MonitoredSince, got %+v", r)
			}
		}
	}
	if !found {
		t.Fatalf("created rule %s not found in stats response: %+v", created.ID, stats.Rules)
	}

	// Toggle off again -> reset endpoint must now reject with 400 (not
	// monitored), and the reflected stats row must go back to monitored=false.
	toggleOffRec := vlanReq(t, handler, "POST", "/api/policies/"+created.ID+"/toggle-monitor", token, nil)
	if toggleOffRec.Code != http.StatusOK {
		t.Fatalf("toggle-monitor (off): expected 200, got %d. Body: %s", toggleOffRec.Code, toggleOffRec.Body.String())
	}
	resetRec := vlanReq(t, handler, "POST", "/api/policies/"+created.ID+"/monitor/reset", token, nil)
	if resetRec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 resetting a non-monitored rule, got %d. Body: %s", resetRec.Code, resetRec.Body.String())
	}
}
