package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pigate/internal/model"
	"pigate/internal/service"
)

// TestHandleGetPolicyRuleEndpoints_NoSession covers Final acceptance: no
// session -> 401.
func TestHandleGetPolicyRuleEndpoints_NoSession(t *testing.T) {
	handler, _ := setupTestServer(t)

	req := httptest.NewRequest("GET", "/api/policies/rule-1/endpoints", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetPolicyRuleEndpoints_NotWired covers the defensive
// nil-service branch, mirroring TestHandleGetPolicyStats_NotWired.
func TestHandleGetPolicyRuleEndpoints_NotWired(t *testing.T) {
	handler, _ := setupTestServer(t)

	rec := vlanReq(t, handler, "GET", "/api/policies/rule-1/endpoints", "mock_session_id_test_token", nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 when policyStats is not wired, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetPolicyRuleEndpoints_LimitValidation covers Final acceptance:
// limit=0 / limit=51 / limit=abc -> 400.
func TestHandleGetPolicyRuleEndpoints_LimitValidation(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.SetPolicyStatsService(service.NewPolicyStatsService(server.repo, nil, nil, nil))
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	for _, limit := range []string{"0", "51", "abc", "-1"} {
		rec := vlanReq(t, handler, "GET", "/api/policies/rule-1/endpoints?limit="+limit, token, nil)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("limit=%s: expected 400, got %d. Body: %s", limit, rec.Code, rec.Body.String())
		}
	}
}

// TestHandleGetPolicyRuleEndpoints_NotFound covers Final acceptance: a rule
// id that doesn't exist -> 404.
func TestHandleGetPolicyRuleEndpoints_NotFound(t *testing.T) {
	server, _ := buildTestServer(t, false)
	server.SetPolicyStatsService(service.NewPolicyStatsService(server.repo, nil, nil, nil))
	handler := RegisterRoutes(server)
	AddSession("mock_session_id_test_token", "pigate")
	token := "mock_session_id_test_token"

	rec := vlanReq(t, handler, "GET", "/api/policies/does-not-exist/endpoints", token, nil)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown rule id, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}

// TestHandleGetPolicyRuleEndpoints_OK covers Final acceptance: 200 + correct
// response shape for a real rule, default limit applied when omitted.
func TestHandleGetPolicyRuleEndpoints_OK(t *testing.T) {
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
		Log:         true,
		Status:      true,
	}
	createRec := vlanReq(t, handler, "POST", "/api/policies", token, input)
	if createRec.Code != http.StatusOK {
		t.Fatalf("create policy: expected 200, got %d. Body: %s", createRec.Code, createRec.Body.String())
	}
	var created model.PolicyRule
	if err := json.Unmarshal(createRec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode created policy: %v", err)
	}

	rec := vlanReq(t, handler, "GET", "/api/policies/"+created.ID+"/endpoints", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var got model.PolicyRuleEndpoints
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got.RuleID != created.ID {
		t.Errorf("expected ruleId %q, got %q", created.ID, got.RuleID)
	}
	if got.RuleName != "Test Rule" {
		t.Errorf("expected ruleName %q, got %q", "Test Rule", got.RuleName)
	}
	if !got.LogEnabled {
		t.Errorf("expected logEnabled=true (rule was created with Log:true)")
	}
	if got.Limit != defaultPolicyEndpointsLimit {
		t.Errorf("expected default limit %d, got %d", defaultPolicyEndpointsLimit, got.Limit)
	}
	if got.Sources == nil || got.Destinations == nil || got.Services == nil {
		t.Errorf("expected non-nil (possibly empty) lists, got %+v", got)
	}
}
