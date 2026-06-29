package service

import (
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
)

type trackingQosManager struct {
	rules           []model.QosRule
	clearCalls      []string
	statusMock      map[string]*model.QosIfaceStatus
	applyCallsCount int
}

func (t *trackingQosManager) ApplyQosRules(rules []model.QosRule) error {
	t.rules = rules
	t.applyCallsCount++
	return nil
}

func (t *trackingQosManager) ClearQosRules(ifaceName string) error {
	t.clearCalls = append(t.clearCalls, ifaceName)
	return nil
}

func (t *trackingQosManager) GetIfaceQosStatus(ifaceName string) (*model.QosIfaceStatus, error) {
	if status, ok := t.statusMock[ifaceName]; ok {
		return status, nil
	}
	return &model.QosIfaceStatus{
		Interface: ifaceName,
		HasQdisc:  false,
		Classes:   []model.QosClass{},
	}, nil
}

func TestQosServiceCRUDAndSync(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	qosMgr := &trackingQosManager{
		statusMock: make(map[string]*model.QosIfaceStatus),
	}
	qosSvc := NewQosService(repo, qosMgr)

	// 1. Initially rules should be empty
	rules, err := qosSvc.GetRules()
	if err != nil {
		t.Fatalf("Failed to get rules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("Expected 0 rules initially, got %d", len(rules))
	}

	// 2. Create a rule
	input := model.QosRuleInput{
		Name:            "Limit eth0",
		Interface:       "eth0",
		MatchSrcIP:      "192.168.1.0/24",
		EgressRateMbps:  50,
		EgressCeilMbps:  100,
		IngressRateMbps: 10,
		IngressCeilMbps: 20,
		Priority:        10,
		Status:          true,
		Description:     "Test rule",
	}

	createdRule, err := qosSvc.CreateRule(input)
	if err != nil {
		t.Fatalf("Failed to create rule: %v", err)
	}
	if createdRule.Name != input.Name || createdRule.EgressRateMbps != input.EgressRateMbps {
		t.Errorf("Mismatch in created rule attributes")
	}

	// Verify sync was triggered
	if qosMgr.applyCallsCount == 0 {
		t.Errorf("Expected ApplyQosRules to be called upon creation")
	}
	if len(qosMgr.rules) != 1 {
		t.Errorf("Expected 1 rule applied to kernel, got %d", len(qosMgr.rules))
	}

	// 3. Update the rule
	updateInput := input
	updateInput.EgressRateMbps = 60
	updatedRule, err := qosSvc.UpdateRule(createdRule.ID, updateInput)
	if err != nil {
		t.Fatalf("Failed to update rule: %v", err)
	}
	if updatedRule.EgressRateMbps != 60 {
		t.Errorf("Expected egress rate 60, got %d", updatedRule.EgressRateMbps)
	}

	// 4. Toggle status
	toggledRule, err := qosSvc.ToggleRuleStatus(createdRule.ID)
	if err != nil {
		t.Fatalf("Failed to toggle rule status: %v", err)
	}
	if toggledRule.Status {
		t.Errorf("Expected toggled status to be false")
	}

	// 5. Delete the rule
	err = qosSvc.DeleteRule(createdRule.ID)
	if err != nil {
		t.Fatalf("Failed to delete rule: %v", err)
	}

	rules, err = qosSvc.GetRules()
	if err != nil {
		t.Fatalf("Failed to get rules: %v", err)
	}
	if len(rules) != 0 {
		t.Errorf("Expected 0 rules after deletion, got %d", len(rules))
	}
}
