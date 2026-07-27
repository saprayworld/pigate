package service

import (
	"fmt"
	"log"
	"sync"
	"time"

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

	// mu guards lastApplyErr/lastApplyAt, which record the outcome of the
	// most recent SyncFirewallRules call so SystemCapabilityService can
	// distinguish "nftables not usable at all" (a probe failure) from
	// "nftables is usable but our last rule set failed to apply" (see
	// ApplyHealth / docs/ref/todo/kernel-capability-detection-plan.md §2.2).
	mu           sync.RWMutex
	lastApplyErr error
	lastApplyAt  time.Time
}

// var _ compiles away to nothing at runtime; it just proves FirewallService
// satisfies ApplyHealthReporter (T-06 acceptance).
var _ ApplyHealthReporter = (*FirewallService)(nil)

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

// =========================================================================
// Port Forwarding (DNAT) Methods
// =========================================================================

// GetPortForwards retrieves all port-forward entries from the database.
func (s *FirewallService) GetPortForwards() ([]model.PortForward, error) {
	return s.repo.GetPortForwards()
}

// GetPortForwardByID retrieves a specific port-forward entry by its ID.
func (s *FirewallService) GetPortForwardByID(id string) (*model.PortForward, error) {
	return s.repo.GetPortForwardByID(id)
}

// CreatePortForward inserts a new port-forward entry and re-applies the firewall
// so the DNAT + forward-accept rules take effect immediately.
func (s *FirewallService) CreatePortForward(pf model.PortForward) error {
	if err := s.repo.CreatePortForward(pf); err != nil {
		return err
	}
	return s.SyncFirewallRules()
}

// UpdatePortForward updates an existing port-forward entry and re-applies the firewall.
func (s *FirewallService) UpdatePortForward(pf model.PortForward) error {
	if err := s.repo.UpdatePortForward(pf); err != nil {
		return err
	}
	return s.SyncFirewallRules()
}

// DeletePortForward removes a port-forward entry and re-applies the firewall.
func (s *FirewallService) DeletePortForward(id string) error {
	if err := s.repo.DeletePortForward(id); err != nil {
		return err
	}
	return s.SyncFirewallRules()
}

// SyncFirewallRules pulls all policies, interfaces, address objects, and service objects
// from the database and applies them to the kernel via the FirewallManager. Its outcome
// (error or nil) is always recorded via recordApply — see ApplyHealth.
func (s *FirewallService) SyncFirewallRules() (err error) {
	defer func() { s.recordApply(err) }()

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

	portForwards, err := s.repo.GetPortForwards()
	if err != nil {
		return fmt.Errorf("failed to load port forwards: %w", err)
	}

	if err := s.firewall.ApplyRules(rules, ifaces, addrs, svcs, dhcpServerIfaces, dnsServerIfaces, portForwards); err != nil {
		return fmt.Errorf("failed to apply firewall rules: %w", err)
	}
	return nil
}

// recordApply stores the outcome of the SyncFirewallRules call that just
// finished (err may be nil), under mu, for ApplyHealth to read.
func (s *FirewallService) recordApply(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lastApplyErr = err
	s.lastApplyAt = time.Now()
}

// ApplyHealth implements service.ApplyHealthReporter: it reports whether the
// most recent SyncFirewallRules call succeeded. Before the first ever apply
// (lastApplyAt is zero), it reports ok=true with a zero time so
// SystemCapabilityService does not mistake "never applied yet" for a real
// failure (see docs/ref/todo/kernel-capability-detection-plan.md T-06).
func (s *FirewallService) ApplyHealth() (bool, string, time.Time) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.lastApplyAt.IsZero() {
		return true, "", time.Time{}
	}
	if s.lastApplyErr != nil {
		return false, s.lastApplyErr.Error(), s.lastApplyAt
	}
	return true, "", s.lastApplyAt
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

// BulkDeleteAddresses deletes multiple address objects by their IDs and returns
// the number actually removed (nonexistent IDs are skipped, see repository).
func (s *FirewallService) BulkDeleteAddresses(ids []string) (int64, error) {
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
