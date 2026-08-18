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

	// lastApplyOKAt is like lastApplyAt but only updated on a SUCCESSFUL
	// SyncFirewallRules — used as the "countersSince" anchor for per-rule
	// usage stats (nftables counters reset to 0 on every ApplyRules, so a
	// failed apply must not move this timestamp). See LastAppliedAt and
	// docs/ref/todo/firewall-policy-rule-usage-stats-plan.md T-04.
	lastApplyOKAt time.Time

	// ruleNames is optional (nil until SetRuleNameResolver is called by
	// main.go) so this stays additive — NewFirewallService's signature is
	// unchanged. When set, recordApply triggers a synchronous snapshot
	// refresh right after every successful SyncFirewallRules, so a
	// brand-new rule that immediately sees traffic already resolves a name
	// instead of waiting for the resolver's background ticker (see
	// docs/ref/todo/traffic-log-rule-name-and-domain-plan.md).
	ruleNames *RuleNameResolver

	// applyMu serializes the body of SyncFirewallRules (docs/ref/todo/
	// fqdn-retry-and-monitored-counters-plan.md T-03, issue #141) — the
	// background FQDNRefresher ticker and a user clicking "Apply" (or any
	// other caller) must never flush the nft table concurrently. Deliberately
	// a SEPARATE mutex from mu above: recordApply locks mu via a defer inside
	// the very function applyMu wraps, so sharing one mutex would deadlock
	// (plan Caution 3).
	applyMu sync.Mutex

	// counterStore/trafficStats are optional (nil until
	// SetPolicyCounterStore/SetTrafficStats are called by main.go), following
	// the same additive-setter pattern as SetRuleNameResolver above — so
	// NewFirewallService's signature never changes. When set, SyncFirewallRules
	// flushes any pending "Monitor" counter deltas to SQLite immediately
	// before ApplyRules (D-4: same point in time nftables' own counters are
	// about to reset to 0), and resets TrafficStatsService's rule-counter
	// baseline via an epoch bump immediately after a successful ApplyRules
	// (D-5 — EndApply, NOT a bare zero-the-baseline call: see its doc comment
	// in traffic_stats.go for the double-count race this specifically avoids).
	counterStore *PolicyCounterStore
	trafficStats *TrafficStatsService
}

// SetPolicyCounterStore wires the optional persisted "Monitor" counter store
// — see the counterStore field doc comment above.
func (s *FirewallService) SetPolicyCounterStore(store *PolicyCounterStore) {
	s.counterStore = store
}

// SetTrafficStats wires the optional TrafficStatsService — see the
// trafficStats field doc comment above.
func (s *FirewallService) SetTrafficStats(ts *TrafficStatsService) {
	s.trafficStats = ts
}

// SetRuleNameResolver wires an optional RuleNameResolver into the service so
// its snapshot gets refreshed immediately after every successful firewall
// apply. Additive — safe to call after construction, and safe to never call
// at all (ruleNames stays nil, recordApply just skips the refresh).
func (s *FirewallService) SetRuleNameResolver(r *RuleNameResolver) {
	s.ruleNames = r
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

// GetPolicies retrieves firewall policies from the database. An empty chain
// returns every chain (used by the kernel sync); a non-empty chain filters to
// just that chain (used by the per-chain frontend pages).
func (s *FirewallService) GetPolicies(chain string) ([]model.PolicyRule, error) {
	all, err := s.repo.GetPolicies()
	if err != nil {
		return nil, err
	}
	if chain == "" {
		return all, nil
	}
	filtered := []model.PolicyRule{}
	for _, p := range all {
		if p.Chain == chain {
			filtered = append(filtered, p)
		}
	}
	return filtered, nil
}

// GetPolicyByID retrieves a specific firewall policy by its ID.
func (s *FirewallService) GetPolicyByID(id string) (*model.PolicyRule, error) {
	return s.repo.GetPolicyByID(id)
}

// CreatePolicy inserts a new firewall policy rule into the database.
func (s *FirewallService) CreatePolicy(rule model.PolicyRule) error {
	rule.Chain = model.NormalizePolicyChain(rule.Chain)
	model.NormalizePolicyRuleInterfaces(&rule)
	if err := model.ValidatePolicyRule(rule); err != nil {
		return err
	}
	s.warnUnknownPolicyInterfaces(rule)
	return s.repo.CreatePolicy(rule)
}

// UpdatePolicy updates an existing firewall policy rule in the database.
func (s *FirewallService) UpdatePolicy(rule model.PolicyRule) error {
	rule.Chain = model.NormalizePolicyChain(rule.Chain)
	model.NormalizePolicyRuleInterfaces(&rule)
	if err := model.ValidatePolicyRule(rule); err != nil {
		return err
	}
	s.warnUnknownPolicyInterfaces(rule)
	return s.repo.UpdatePolicy(rule)
}

// warnUnknownPolicyInterfaces implements D-5 (docs/ref/todo/
// multi-interface-firewall-rule-plan.md §2.2/§2.5): every non-"ALL"
// interface name in rule's InInterfaces/OutInterfaces is compared against
// the interfaces currently known to this device. A name that doesn't exist
// yet (e.g. wlan0/bridge not up at boot, or the operator pre-configuring a
// rule before creating the interface) only produces a log warning — it must
// NEVER block saving the rule (Caution 13), and a failure to even query the
// interface list must be swallowed silently (fail-open on the check itself,
// fail-closed only stays true for name-syntax validation, which already
// happened in ValidatePolicyRule above).
func (s *FirewallService) warnUnknownPolicyInterfaces(rule model.PolicyRule) {
	if s.ifaceService == nil {
		return
	}
	ifaces, err := s.ifaceService.GetDataLayerInterface()
	if err != nil {
		log.Printf("[FirewallService] Could not verify policy %q interfaces exist on this device (skipping check): %v", rule.Name, err)
		return
	}
	known := make(map[string]struct{}, len(ifaces))
	for _, iface := range ifaces {
		known[iface.Name] = struct{}{}
	}
	for _, name := range append(append([]string{}, rule.InInterfaces...), rule.OutInterfaces...) {
		if name == "" || name == "ALL" {
			continue
		}
		if _, ok := known[name]; !ok {
			log.Printf("[FirewallService] Policy %q references interface %q which does not exist on this device yet; the rule was saved and will take effect once the interface is present", rule.Name, name)
		}
	}
}

// DeletePolicy deletes a firewall policy rule by its ID.
func (s *FirewallService) DeletePolicy(id string) error {
	return s.repo.DeletePolicy(id)
}

// ReorderPolicies saves the new priority order (1..N) for every policy id in
// ids, scoped to chain. See db.Repository.SaveChainOrder.
func (s *FirewallService) ReorderPolicies(chain string, ids []string) error {
	return s.repo.SaveChainOrder(model.NormalizePolicyChain(chain), ids)
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
//
// applyMu serializes the whole body against concurrent callers (T-03,
// Caution 3/5) — the FQDNRefresher ticker and a manual "Apply" click must
// never race each other into a double nft-table flush.
//
// Counter bookkeeping happens at exactly two points, both optional
// (counterStore/trafficStats may be nil — see their field doc comments):
//  1. Immediately BEFORE s.firewall.ApplyRules, counterStore.FlushBeforeApply()
//     persists any pending "Monitor" delta to SQLite (D-4) — nftables' own
//     counters are about to be zeroed by the flush this call triggers, so
//     this is the last chance to capture them. A flush failure is only
//     logged; it must never block the apply itself (Caution 4).
//  2. Immediately AFTER a successful ApplyRules, trafficStats.EndApply()
//     atomically zeroes the rule-counter baseline and bumps the apply epoch
//     (D-5) — never a bare "zero the baseline" call, see EndApply's own doc
//     comment in traffic_stats.go for the double-count race this avoids
//     (Caution 10).
func (s *FirewallService) SyncFirewallRules() (err error) {
	s.applyMu.Lock()
	defer s.applyMu.Unlock()

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

	if s.counterStore != nil {
		if flushErr := s.counterStore.FlushBeforeApply(); flushErr != nil {
			log.Printf("[FirewallService] pre-apply Monitor counter flush failed (continuing with apply): %v", flushErr)
		}
	}

	if err := s.firewall.ApplyRules(rules, ifaces, addrs, svcs, dhcpServerIfaces, dnsServerIfaces, portForwards); err != nil {
		return fmt.Errorf("failed to apply firewall rules: %w", err)
	}

	if s.trafficStats != nil {
		s.trafficStats.EndApply()
	}
	return nil
}

// recordApply stores the outcome of the SyncFirewallRules call that just
// finished (err may be nil), under mu, for ApplyHealth to read.
func (s *FirewallService) recordApply(err error) {
	s.mu.Lock()
	s.lastApplyErr = err
	s.lastApplyAt = time.Now()
	if err == nil {
		s.lastApplyOKAt = s.lastApplyAt
	}
	s.mu.Unlock()

	// Refresh the rule-name snapshot right after a successful apply, on
	// every return path of SyncFirewallRules (recordApply runs via defer),
	// so newly created/renamed rules resolve a name immediately rather than
	// waiting up to ruleNameRefreshInterval for the background ticker.
	if err == nil && s.ruleNames != nil {
		if refreshErr := s.ruleNames.Refresh(); refreshErr != nil {
			log.Printf("[FirewallService] rule-name snapshot refresh after apply failed: %v", refreshErr)
		}
	}
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

// LastAppliedAt returns the timestamp of the last SUCCESSFUL SyncFirewallRules
// call (zero Time if none has ever succeeded), for use as the "countersSince"
// anchor by PolicyStatsService.
func (s *FirewallService) LastAppliedAt() time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.lastApplyOKAt
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
