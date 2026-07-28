package api

import (
	"encoding/json"
	"net/http"
	"testing"

	"pigate/internal/model"
)

// TestPolicyChainHandlers covers docs/ref/todo/input-output-chain-firewall-plan.md
// Caution 1 (chain must not be dropped silently when the handler assembles
// model.PolicyRule field-by-field) and Caution 2 (PUT without a chain must
// keep the rule's existing chain, never fall back to "forward").
func TestPolicyChainHandlers(t *testing.T) {
	handler, _ := setupTestServer(t)
	token := "mock_session_id_test_token"

	createPolicy := func(t *testing.T, chain string) model.PolicyRule {
		t.Helper()
		input := model.PolicyRuleInput{
			Name:        "Test " + chain,
			Chain:       chain,
			Source:      []string{"ALL"},
			Destination: []string{"ALL"},
			Service:     []string{"ALL"},
			Action:      "DROP",
			Status:      true,
		}
		rec := vlanReq(t, handler, "POST", "/api/policies", token, input)
		if rec.Code != http.StatusOK {
			t.Fatalf("create chain=%q: expected 200, got %d. Body: %s", chain, rec.Code, rec.Body.String())
		}
		var created model.PolicyRule
		if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
			t.Fatalf("decode created policy: %v", err)
		}
		return created
	}

	// Caution 1: chain must survive HandleCreatePolicy's field-by-field struct
	// assembly for all three chains, not just silently become "" or forward.
	inputRule := createPolicy(t, model.PolicyChainInput)
	if inputRule.Chain != model.PolicyChainInput {
		t.Errorf("expected created rule chain %q, got %q", model.PolicyChainInput, inputRule.Chain)
	}
	outputRule := createPolicy(t, model.PolicyChainOutput)
	if outputRule.Chain != model.PolicyChainOutput {
		t.Errorf("expected created rule chain %q, got %q", model.PolicyChainOutput, outputRule.Chain)
	}
	forwardRule := createPolicy(t, model.PolicyChainForward)
	if forwardRule.Chain != model.PolicyChainForward {
		t.Errorf("expected created rule chain %q, got %q", model.PolicyChainForward, forwardRule.Chain)
	}

	// Caution 2: PUT with an empty/omitted chain must NOT move the rule to
	// forward — it must keep the existing chain (input here).
	updateInput := model.PolicyRuleInput{
		Name:        "Test input renamed",
		Chain:       "", // omitted on purpose
		Source:      []string{"ALL"},
		Destination: []string{"ALL"},
		Service:     []string{"ALL"},
		Action:      "DROP",
		Status:      true,
	}
	rec := vlanReq(t, handler, "PUT", "/api/policies/"+inputRule.ID, token, updateInput)
	if rec.Code != http.StatusOK {
		t.Fatalf("update without chain: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var updated model.PolicyRule
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode updated policy: %v", err)
	}
	if updated.Chain != model.PolicyChainInput {
		t.Errorf("PUT without chain must keep existing chain %q, got %q (must NOT silently fall back to forward)", model.PolicyChainInput, updated.Chain)
	}

	// GET /api/policies?chain=input must only return input-chain rules.
	rec = vlanReq(t, handler, "GET", "/api/policies?chain=input", token, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET ?chain=input: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
	var inputList []model.PolicyRule
	if err := json.Unmarshal(rec.Body.Bytes(), &inputList); err != nil {
		t.Fatalf("decode input list: %v", err)
	}
	for _, p := range inputList {
		if p.Chain != model.PolicyChainInput {
			t.Errorf("GET ?chain=input returned a %q-chain rule: %+v", p.Chain, p)
		}
	}
	if len(inputList) != 1 {
		t.Fatalf("expected exactly 1 input-chain rule, got %d: %+v", len(inputList), inputList)
	}

	// GET /api/policies?chain=bogus -> 400.
	rec = vlanReq(t, handler, "GET", "/api/policies?chain=bogus", token, nil)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("GET ?chain=bogus: expected 400, got %d", rec.Code)
	}

	// GET /api/policies (no chain param) -> all three chains present.
	rec = vlanReq(t, handler, "GET", "/api/policies", token, nil)
	var allList []model.PolicyRule
	if err := json.Unmarshal(rec.Body.Bytes(), &allList); err != nil {
		t.Fatalf("decode all list: %v", err)
	}
	seen := map[string]bool{}
	for _, p := range allList {
		seen[p.Chain] = true
	}
	if !seen[model.PolicyChainForward] || !seen[model.PolicyChainInput] || !seen[model.PolicyChainOutput] {
		t.Errorf("expected all 3 chains present in unfiltered GET /api/policies, got chains seen=%v", seen)
	}

	// Reorder scoped by chain: an id from another chain must be rejected.
	reorderBody := map[string]interface{}{
		"chain": model.PolicyChainInput,
		"policies": []model.PolicyRule{
			{ID: outputRule.ID},
		},
	}
	rec = vlanReq(t, handler, "PUT", "/api/policies/reorder", token, reorderBody)
	if rec.Code == http.StatusOK {
		t.Errorf("reorder input chain with an output-chain id: expected non-200, got 200. Body: %s", rec.Body.String())
	}

	// Valid reorder within the same chain succeeds.
	reorderBody = map[string]interface{}{
		"chain": model.PolicyChainInput,
		"policies": []model.PolicyRule{
			{ID: inputRule.ID},
		},
	}
	rec = vlanReq(t, handler, "PUT", "/api/policies/reorder", token, reorderBody)
	if rec.Code != http.StatusOK {
		t.Errorf("valid reorder within chain input: expected 200, got %d. Body: %s", rec.Code, rec.Body.String())
	}
}
