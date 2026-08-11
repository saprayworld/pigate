package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

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
