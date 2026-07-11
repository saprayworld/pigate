package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"pigate/internal/model"
)

func TestQosAPIRoutes(t *testing.T) {
	handler, repo := setupTestServer(t)
	authToken := "mock_session_id_test_token"

	// 1. Get QoS Rules (initially empty)
	req := httptest.NewRequest("GET", "/api/qos/rules", nil)
	addSessionCookie(req, authToken)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var rules []model.QosRule
	if err := json.Unmarshal(rec.Body.Bytes(), &rules); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("Expected 0 rules, got %d", len(rules))
	}

	// 2. Create a QoS Rule (POST /api/qos/rules)
	ruleInput := model.QosRuleInput{
		Name:            "Limit download to 50",
		Interface:       "eth0",
		EgressRateMbps:  50,
		EgressCeilMbps:  100,
		IngressRateMbps: 10,
		IngressCeilMbps: 20,
		Priority:        10,
		Status:          true,
		Description:     "Limit download to 50 Mbps",
	}
	body, _ := json.Marshal(ruleInput)
	req = httptest.NewRequest("POST", "/api/qos/rules", bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusCreated {
		t.Fatalf("Expected 201 Created, got %d. Body: %s", rec.Code, rec.Body.String())
	}

	var created model.QosRule
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("Failed to decode created rule: %v", err)
	}
	if created.Name != ruleInput.Name || created.EgressRateMbps != 50 {
		t.Errorf("Mismatch in created rule data")
	}

	// Verify it exists in DB
	dbRule, err := repo.GetQosRuleByID(created.ID)
	if err != nil {
		t.Fatalf("Failed to fetch rule from DB: %v", err)
	}
	if dbRule.Name != ruleInput.Name {
		t.Errorf("Expected name %s, got %s", ruleInput.Name, dbRule.Name)
	}

	// 3. Get QoS Rule by ID (GET /api/qos/rules/{id})
	req = httptest.NewRequest("GET", "/api/qos/rules/"+created.ID, nil)
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var fetched model.QosRule
	if err := json.Unmarshal(rec.Body.Bytes(), &fetched); err != nil {
		t.Fatalf("Failed to decode fetched rule: %v", err)
	}
	if fetched.ID != created.ID {
		t.Errorf("Expected ID %s, got %s", created.ID, fetched.ID)
	}

	// 4. Update QoS Rule (PUT /api/qos/rules/{id})
	ruleInput.EgressRateMbps = 60
	body, _ = json.Marshal(ruleInput)
	req = httptest.NewRequest("PUT", "/api/qos/rules/"+created.ID, bytes.NewBuffer(body))
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var updated model.QosRule
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("Failed to decode updated rule: %v", err)
	}
	if updated.EgressRateMbps != 60 {
		t.Errorf("Expected egress rate 60, got %d", updated.EgressRateMbps)
	}

	// 5. Toggle QoS Rule (POST /api/qos/rules/{id}/toggle)
	req = httptest.NewRequest("POST", "/api/qos/rules/"+created.ID+"/toggle", nil)
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	var toggled model.QosRule
	if err := json.Unmarshal(rec.Body.Bytes(), &toggled); err != nil {
		t.Fatalf("Failed to decode toggled rule: %v", err)
	}
	if toggled.Status {
		t.Errorf("Expected status to be false after toggle")
	}

	// 6. Delete QoS Rule (DELETE /api/qos/rules/{id})
	req = httptest.NewRequest("DELETE", "/api/qos/rules/"+created.ID, nil)
	addSessionCookie(req, authToken)
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("Expected 200 OK, got %d", rec.Code)
	}

	// Verify rule is gone
	_, err = repo.GetQosRuleByID(created.ID)
	if err == nil {
		t.Errorf("Expected rule to be deleted from DB, but it still exists")
	}
}
