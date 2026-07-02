package service

import (
	"fmt"
	"log"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// FirewallService coordinates operations on policies, address objects,
// and service objects between the database repository and the kernel firewall manager.
type FirewallService struct {
	repo         *db.Repository
	firewall     kernel.FirewallManager
	ifaceService *InterfaceService
}

// NewFirewallService creates a new FirewallService instance.
func NewFirewallService(repo *db.Repository, firewall kernel.FirewallManager, ifaceService *InterfaceService) *FirewallService {
	return &FirewallService{
		repo:         repo,
		firewall:     firewall,
		ifaceService: ifaceService,
	}
}

// =========================================================================
// Firewall Policies Methods
// =========================================================================

// GetPolicies retrieves all firewall policies from the database.
func (s *FirewallService) GetPolicies() ([]model.PolicyRule, error) {
	return s.repo.GetPolicies()
}

// GetPolicyByID retrieves a specific firewall policy by its ID.
func (s *FirewallService) GetPolicyByID(id string) (*model.PolicyRule, error) {
	return s.repo.GetPolicyByID(id)
}

// CreatePolicy inserts a new firewall policy rule into the database.
func (s *FirewallService) CreatePolicy(rule model.PolicyRule) error {
	return s.repo.CreatePolicy(rule)
}

// UpdatePolicy updates an existing firewall policy rule in the database.
func (s *FirewallService) UpdatePolicy(rule model.PolicyRule) error {
	return s.repo.UpdatePolicy(rule)
}

// DeletePolicy deletes a firewall policy rule by its ID.
func (s *FirewallService) DeletePolicy(id string) error {
	return s.repo.DeletePolicy(id)
}

// ReorderPolicies saves all policies in their new order.
func (s *FirewallService) ReorderPolicies(policies []model.PolicyRule) error {
	return s.repo.SaveAllPolicies(policies)
}

// TogglePolicyLog toggles the logging flag on a policy.
func (s *FirewallService) TogglePolicyLog(id string) (*model.PolicyRule, error) {
	if err := s.repo.TogglePolicyLog(id); err != nil {
		return nil, err
	}
	return s.repo.GetPolicyByID(id)
}

// TogglePolicyStatus toggles the status (enabled/disabled) on a policy.
func (s *FirewallService) TogglePolicyStatus(id string) (*model.PolicyRule, error) {
	if err := s.repo.TogglePolicyStatus(id); err != nil {
		return nil, err
	}
	return s.repo.GetPolicyByID(id)
}

// SyncFirewallRules pulls all policies, interfaces, address objects, and service objects
// from the database and applies them to the kernel via the FirewallManager.
func (s *FirewallService) SyncFirewallRules() error {
	rules, err := s.repo.GetPolicies()
	if err != nil {
		return fmt.Errorf("failed to load policies: %w", err)
	}

	ifaces, err := s.ifaceService.GetDataLayerInterface()
	if err != nil {
		return fmt.Errorf("failed to load interfaces from InterfaceService: %w", err)
	}

	addrs, err := s.repo.GetAddresses()
	if err != nil {
		return fmt.Errorf("failed to load address objects: %w", err)
	}

	svcs, err := s.repo.GetServices()
	if err != nil {
		return fmt.Errorf("failed to load service objects: %w", err)
	}

	dhcpCfgs, err := s.repo.GetDHCPConfigs()
	if err != nil {
		return fmt.Errorf("failed to load DHCP configs: %w", err)
	}
	dhcpServerIfaces := []string{}
	for _, cfg := range dhcpCfgs {
		if cfg.Enabled {
			dhcpServerIfaces = append(dhcpServerIfaces, cfg.Interface)
		}
	}

	dnsServerIfaces, err := s.repo.GetDNSServerInterfaces()
	if err != nil {
		return fmt.Errorf("failed to load DNS Server interfaces: %w", err)
	}

	if err := s.firewall.ApplyRules(rules, ifaces, addrs, svcs, dhcpServerIfaces, dnsServerIfaces); err != nil {
		return fmt.Errorf("failed to apply firewall rules: %w", err)
	}
	return nil
}

// InitApplyConfig executes firewall sync at startup.
func (s *FirewallService) InitApplyConfig() error {
	log.Printf("[Startup] Syncing firewall rules to kernel...")
	if err := s.SyncFirewallRules(); err != nil {
		return fmt.Errorf("failed to apply firewall rules at startup: %w", err)
	}
	log.Printf("[Startup] Successfully applied firewall rules at startup.")
	return nil
}

// =========================================================================
// Address Objects Methods
// =========================================================================

// GetAddresses retrieves all address objects from the database.
func (s *FirewallService) GetAddresses() ([]model.AddressObject, error) {
	return s.repo.GetAddresses()
}

// GetAddressByID retrieves a specific address object by its ID.
func (s *FirewallService) GetAddressByID(id string) (*model.AddressObject, error) {
	return s.repo.GetAddressByID(id)
}

// CreateAddress inserts a new address object into the database.
func (s *FirewallService) CreateAddress(addr model.AddressObject) error {
	return s.repo.CreateAddress(addr)
}

// UpdateAddress updates an existing address object in the database.
func (s *FirewallService) UpdateAddress(addr model.AddressObject) error {
	return s.repo.UpdateAddress(addr)
}

// DeleteAddress deletes an address object by its ID.
func (s *FirewallService) DeleteAddress(id string) error {
	return s.repo.DeleteAddress(id)
}

// BulkDeleteAddresses deletes multiple address objects by their IDs.
func (s *FirewallService) BulkDeleteAddresses(ids []string) error {
	return s.repo.BulkDeleteAddresses(ids)
}

// =========================================================================
// Service Objects Methods
// =========================================================================

// GetServices retrieves all service objects from the database.
func (s *FirewallService) GetServices() ([]model.ServiceObject, error) {
	return s.repo.GetServices()
}

// GetServiceByID retrieves a specific service object by its ID.
func (s *FirewallService) GetServiceByID(id string) (*model.ServiceObject, error) {
	return s.repo.GetServiceByID(id)
}

// CreateService inserts a new service object into the database.
func (s *FirewallService) CreateService(svc model.ServiceObject) error {
	return s.repo.CreateService(svc)
}

// UpdateService updates an existing service object in the database.
func (s *FirewallService) UpdateService(svc model.ServiceObject) error {
	return s.repo.UpdateService(svc)
}

// DeleteService deletes a service object by its ID.
func (s *FirewallService) DeleteService(id string) error {
	return s.repo.DeleteService(id)
}
