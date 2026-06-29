package service

import (
	"fmt"
	"log"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// QosService coordinates QoS bandwidth rules between the database and the kernel.
// It follows the same pattern as FirewallService: DB as source of truth, kernel synced on change.
type QosService struct {
	repo *db.Repository
	qos  kernel.QosManager
}

// NewQosService creates a new QosService instance.
func NewQosService(repo *db.Repository, qos kernel.QosManager) *QosService {
	return &QosService{
		repo: repo,
		qos:  qos,
	}
}

// ─── CRUD Methods ─────────────────────────────────────────────────────────────

// GetRules retrieves all QoS rules from the database.
func (s *QosService) GetRules() ([]model.QosRule, error) {
	return s.repo.GetQosRules()
}

// GetRuleByID retrieves a single QoS rule by its ID.
func (s *QosService) GetRuleByID(id string) (*model.QosRule, error) {
	return s.repo.GetQosRuleByID(id)
}

// CreateRule inserts a new rule into the database and syncs all rules to the kernel.
func (s *QosService) CreateRule(input model.QosRuleInput) (*model.QosRule, error) {
	rule, err := s.repo.CreateQosRule(input)
	if err != nil {
		return nil, err
	}
	if err := s.SyncToKernel(); err != nil {
		log.Printf("[QosService] Warning: kernel sync failed after create rule %q: %v", rule.Name, err)
	}
	return rule, nil
}

// UpdateRule updates an existing rule and re-syncs rules to the kernel.
func (s *QosService) UpdateRule(id string, input model.QosRuleInput) (*model.QosRule, error) {
	rule, err := s.repo.UpdateQosRule(id, input)
	if err != nil {
		return nil, err
	}
	if err := s.SyncToKernel(); err != nil {
		log.Printf("[QosService] Warning: kernel sync failed after update rule %q: %v", id, err)
	}
	return rule, nil
}

// DeleteRule removes a rule from the database, then re-applies remaining rules
// for that interface only (more targeted than full sync).
func (s *QosService) DeleteRule(id string) error {
	// Retrieve the interface name before deleting
	rule, err := s.repo.GetQosRuleByID(id)
	if err != nil {
		return err
	}
	ifaceName := rule.Interface

	if err := s.repo.DeleteQosRule(id); err != nil {
		return err
	}

	// Re-apply remaining rules for this interface
	remaining, err := s.repo.GetQosRulesByInterface(ifaceName)
	if err != nil {
		return fmt.Errorf("reload rules after delete for %s: %w", ifaceName, err)
	}
	if err := s.qos.ApplyQosRules(remaining); err != nil {
		log.Printf("[QosService] Warning: kernel sync failed after delete rule %q: %v", id, err)
	}
	return nil
}

// ToggleRuleStatus flips enabled/disabled and re-syncs the kernel.
func (s *QosService) ToggleRuleStatus(id string) (*model.QosRule, error) {
	if err := s.repo.ToggleQosRuleStatus(id); err != nil {
		return nil, err
	}
	rule, err := s.repo.GetQosRuleByID(id)
	if err != nil {
		return nil, err
	}
	if err := s.SyncToKernel(); err != nil {
		log.Printf("[QosService] Warning: kernel sync failed after toggle rule %q: %v", id, err)
	}
	return rule, nil
}

// ─── Kernel Sync Methods ──────────────────────────────────────────────────────

// SyncToKernel is the single source of truth sync — loads all rules from DB
// and applies them to the kernel. Clears and rebuilds per interface (idempotent).
func (s *QosService) SyncToKernel() error {
	rules, err := s.repo.GetQosRules()
	if err != nil {
		return fmt.Errorf("load qos rules from db: %w", err)
	}
	if err := s.qos.ApplyQosRules(rules); err != nil {
		return fmt.Errorf("apply qos rules to kernel: %w", err)
	}
	return nil
}

// GetIfaceStatus returns live qdisc/class state from the kernel for a given interface.
// This reflects actual kernel state, not the database.
func (s *QosService) GetIfaceStatus(ifaceName string) (*model.QosIfaceStatus, error) {
	return s.qos.GetIfaceQosStatus(ifaceName)
}

// ClearIface disables all DB rules for an interface and clears the kernel qdisc.
func (s *QosService) ClearIface(ifaceName string) error {
	if err := s.repo.DisableQosRulesByInterface(ifaceName); err != nil {
		return fmt.Errorf("disable db rules for %s: %w", ifaceName, err)
	}
	if err := s.qos.ClearQosRules(ifaceName); err != nil {
		return fmt.Errorf("clear kernel qdisc on %s: %w", ifaceName, err)
	}
	return nil
}

// InitApplyConfig loads all QoS rules from the database and applies them to the
// kernel at startup. Mirrors the pattern of FirewallService.InitApplyConfig.
func (s *QosService) InitApplyConfig() error {
	log.Printf("[Startup] Syncing QoS rules to kernel...")
	if err := s.SyncToKernel(); err != nil {
		return fmt.Errorf("qos startup sync: %w", err)
	}
	log.Printf("[Startup] QoS rules applied successfully.")
	return nil
}
