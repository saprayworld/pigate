package service

import (
	"fmt"
	"log"
	"net"
	"sort"
	"strings"
	"sync"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type RoutingService struct {
	repo                  *db.Repository
	routing               kernel.RoutingManager
	enableEditSystemRoute bool
	disabledSystemRoutes  map[string]model.StaticRoute

	// mu guards ONLY the four maps below (Task 14 WAN failover metric
	// overrides) — it is a separate lock from disabledSystemRoutes above
	// (which stays unguarded, matching its pre-Task-14 behavior; this file
	// has never been safe for concurrent enable-edit-system-route callers
	// and Task 14 does not change that).
	mu sync.Mutex
	// failoverOverrides is the WAN failover controller's (T-15,
	// wan_failover.go) desired default-route metric per interface — set
	// wholesale via SetFailoverMetricOverrides. Read (never written) by
	// enforceInterfaceMetrics on every reconcile pass.
	failoverOverrides map[string]int
	// preOverrideMetrics snapshots each interface's default-route metric (via
	// kernel.RoutingManager.DefaultRouteMetric, Decision C) from the moment
	// BEFORE its first override was applied, so ClearFailoverMetricOverride/
	// the kill switch can restore the exact pre-override value later rather
	// than guessing at one.
	preOverrideMetrics map[string]int
	// restorePending marks an interface whose override was just removed (or
	// never had one to begin with, transiently, immediately after
	// ClearAllFailoverMetricOverrides) as needing a ONE-TIME restore of its
	// preOverrideMetrics snapshot on the next enforceInterfaceMetrics pass —
	// consumed (deleted) the moment that restore happens.
	restorePending map[string]bool
	// bypassedIfaces is the current (recomputed every reconcile pass) set of
	// interfaces that have an active failover override but are being
	// overridden by an even-higher-precedence active DB static 0.0.0.0/0
	// route (D-2 precedence level 1) — exposed read-only via
	// FailoverBypassedInterfaces() for the API/UI (T-16).
	bypassedIfaces map[string]bool
}

func NewRoutingService(repo *db.Repository, routing kernel.RoutingManager) *RoutingService {
	return &RoutingService{
		repo:                  repo,
		routing:               routing,
		enableEditSystemRoute: false,
		disabledSystemRoutes:  make(map[string]model.StaticRoute),
		failoverOverrides:     make(map[string]int),
		preOverrideMetrics:    make(map[string]int),
		restorePending:        make(map[string]bool),
		bypassedIfaces:        make(map[string]bool),
	}
}

func (s *RoutingService) SetEnableEditSystemRoute(enable bool) {
	s.enableEditSystemRoute = enable
	s.routing.SetEnableEditSystemRoute(enable)
}

func (s *RoutingService) IsEnableEditSystemRoute() bool {
	return s.enableEditSystemRoute
}

func (s *RoutingService) ToggleSystemRouteDirectly(id string) error {
	if !s.enableEditSystemRoute {
		return fmt.Errorf("enable-edit-system-route flag is not enabled")
	}

	if route, found := s.disabledSystemRoutes[id]; found {
		// Currently disabled, enable it back
		route.Status = true
		if err := s.routing.AddRoute(route); err != nil {
			return err
		}
		delete(s.disabledSystemRoutes, id)
		return nil
	}

	// Currently enabled in kernel, disable it
	routes, err := s.GetKernelRouting()
	if err != nil {
		return err
	}
	var targetRoute *model.StaticRoute
	for _, r := range routes {
		if r.ID == id {
			targetRoute = &r
			break
		}
	}
	if targetRoute == nil {
		return fmt.Errorf("system route with ID %q not found", id)
	}

	if err := s.routing.DeleteRoute(*targetRoute); err != nil {
		return err
	}
	targetRoute.Status = false
	s.disabledSystemRoutes[id] = *targetRoute
	return nil
}

// GetKernelRouting retrieves active routing configuration from the kernel.
func (s *RoutingService) GetKernelRouting() ([]model.StaticRoute, error) {
	return s.repo.GetKernelRoutes()
}

// GetDatabaseRouting retrieves static routes configured in the database.
func (s *RoutingService) GetDatabaseRouting() ([]model.StaticRoute, error) {
	return s.repo.GetDatabaseRoutes()
}

// GetRouting retrieves kernel routes merged with database configurations without modifying kernel state.
func (s *RoutingService) GetRouting() ([]model.StaticRoute, error) {
	kernelRoutes, err := s.GetKernelRouting()
	if err != nil {
		return nil, fmt.Errorf("failed to get kernel routes: %w", err)
	}

	dbRoutes, err := s.GetDatabaseRouting()
	if err != nil {
		return nil, fmt.Errorf("failed to get database routes: %w", err)
	}

	if s.enableEditSystemRoute {
		var filtered []model.StaticRoute
		for _, r := range dbRoutes {
			if r.Type == "custom" || r.Type == "customgateway" {
				filtered = append(filtered, r)
			}
		}
		dbRoutes = filtered
	}

	// Helper to get canonical CIDR representation
	canonicalCIDR := func(str string) string {
		_, ipNet, err := net.ParseCIDR(str)
		if err != nil {
			return str
		}
		return ipNet.String()
	}

	type routeCompareKey struct {
		dest  string
		gw    string
		iface string
	}

	kMap := make(map[routeCompareKey]model.StaticRoute)
	for _, kr := range kernelRoutes {
		key := routeCompareKey{
			dest:  canonicalCIDR(kr.Destination),
			gw:    strings.TrimSpace(kr.Gateway),
			iface: strings.TrimSpace(kr.Interface),
		}
		kMap[key] = kr
	}

	var result []model.StaticRoute
	matchedKeys := make(map[routeCompareKey]bool)

	prioritizeKernelRoutes := s.repo.GetPrioritizeKernelRoutes()

	for _, dbRoute := range dbRoutes {
		key := routeCompareKey{
			dest:  canonicalCIDR(dbRoute.Destination),
			gw:    strings.TrimSpace(dbRoute.Gateway),
			iface: strings.TrimSpace(dbRoute.Interface),
		}

		if kr, found := kMap[key]; found {
			matchedKeys[key] = true
			if prioritizeKernelRoutes {
				dbRoute.Metric = kr.Metric
				dbRoute.Scope = kr.Scope
				dbRoute.Src = kr.Src
				dbRoute.Proto = kr.Proto
			}
		} else {
			if prioritizeKernelRoutes && (dbRoute.Type == "system" || dbRoute.Type == "defaultgateway") {
				dbRoute.Status = false
			}
		}
		result = append(result, dbRoute)
	}

	for _, kr := range kernelRoutes {
		key := routeCompareKey{
			dest:  canonicalCIDR(kr.Destination),
			gw:    strings.TrimSpace(kr.Gateway),
			iface: strings.TrimSpace(kr.Interface),
		}
		if !matchedKeys[key] {
			routeID := fmt.Sprintf("route-sys-%s-%s-%s",
				strings.ReplaceAll(canonicalCIDR(kr.Destination), "/", "_"),
				strings.ReplaceAll(kr.Gateway, ".", "_"),
				kr.Interface,
			)
			kr.ID = routeID
			kr.KernelOnly = true
			result = append(result, kr)
		}
	}

	if s.enableEditSystemRoute {
		for _, dr := range s.disabledSystemRoutes {
			result = append(result, dr)
		}
	}

	return result, nil
}

// ApplyConfigRoute saves static route configuration to the DB and applies it to the kernel.
func (s *RoutingService) ApplyConfigRoute(route model.StaticRoute) error {
	// Basic input validation
	if _, _, err := net.ParseCIDR(route.Destination); err != nil {
		return fmt.Errorf("invalid destination CIDR %q: %w", route.Destination, err)
	}
	if route.Gateway != "" && route.Gateway != "default" && net.ParseIP(route.Gateway) == nil {
		return fmt.Errorf("invalid gateway IP %q", route.Gateway)
	}
	if route.Interface == "" {
		return fmt.Errorf("interface name cannot be empty")
	}

	if s.enableEditSystemRoute && (route.KernelOnly || strings.HasPrefix(route.ID, "route-sys-") || route.Type == "system") {
		log.Printf("[RoutingService] Bypassing database. Applying system route directly to kernel: %+v", route)

		// If it's an update, the destination/gateway/interface might have changed, meaning we need to delete the old route
		var oldRoute *model.StaticRoute
		routes, err := s.GetKernelRouting()
		if err == nil {
			for _, r := range routes {
				if r.ID == route.ID {
					oldRoute = &r
					break
				}
			}
		}
		if oldRoute == nil {
			if r, found := s.disabledSystemRoutes[route.ID]; found {
				oldRoute = &r
			}
		}

		if oldRoute != nil {
			log.Printf("[RoutingService] Deleting old system route from kernel: %+v", oldRoute)
			if oldRoute.Status {
				_ = s.routing.DeleteRoute(*oldRoute)
			}
		}

		// Remove from disabled list if it was there
		delete(s.disabledSystemRoutes, route.ID)

		if route.Status {
			if err := s.routing.AddRoute(route); err != nil {
				return fmt.Errorf("failed to add route directly to kernel: %w", err)
			}
		} else {
			// Save in disabled list
			s.disabledSystemRoutes[route.ID] = route
		}
		return nil
	}

	// Set type based on gateway
	if route.Gateway == "" {
		route.Type = "custom"
	} else {
		route.Type = "customgateway"
	}

	// Resolve/check default gateway
	defaultGw := s.repo.GetDefaultGatewayIP("")
	if defaultGw == "" {
		defaultGw = s.repo.GetDefaultGatewayIP(route.Interface)
	}

	if route.Gateway != "" && (route.Gateway == defaultGw || route.Gateway == "default") {
		route.Gateway = "default"
	}

	existing, err := s.repo.GetRouteByID(route.ID)
	if err != nil {
		return fmt.Errorf("failed to check existing route: %w", err)
	}

	if existing != nil {
		if err := s.repo.UpdateRoute(route); err != nil {
			return fmt.Errorf("failed to update route in database: %w", err)
		}
	} else {
		if err := s.repo.CreateRoute(route); err != nil {
			return fmt.Errorf("failed to create route in database: %w", err)
		}
	}

	// Reconcile system/kernel routing table
	if err := s.reconcileKernelRoutingTable(); err != nil {
		return fmt.Errorf("route saved to database but failed to apply to kernel: %w", err)
	}

	return nil
}

// RemoveConfigRoute deletes a static route configuration from DB and updates the kernel.
func (s *RoutingService) RemoveConfigRoute(id string) error {
	if s.enableEditSystemRoute && strings.HasPrefix(id, "route-sys-") {
		log.Printf("[RoutingService] Bypassing database. Removing system route directly: %s", id)
		if _, found := s.disabledSystemRoutes[id]; found {
			delete(s.disabledSystemRoutes, id)
			return nil
		}

		routes, err := s.GetKernelRouting()
		if err != nil {
			return fmt.Errorf("failed to list kernel routes: %w", err)
		}
		var targetRoute *model.StaticRoute
		for _, r := range routes {
			if r.ID == id {
				targetRoute = &r
				break
			}
		}
		if targetRoute == nil {
			// Try merged view
			merged, err := s.GetRouting()
			if err == nil {
				for _, r := range merged {
					if r.ID == id {
						targetRoute = &r
						break
					}
				}
			}
		}
		if targetRoute == nil {
			return fmt.Errorf("system route with ID %q not found", id)
		}
		if err := s.routing.DeleteRoute(*targetRoute); err != nil {
			return fmt.Errorf("failed to delete system route directly from kernel: %w", err)
		}
		return nil
	}

	if err := s.repo.DeleteRoute(id); err != nil {
		return fmt.Errorf("failed to delete route from database: %w", err)
	}

	// Reconcile system/kernel routing table
	if err := s.reconcileKernelRoutingTable(); err != nil {
		return fmt.Errorf("route deleted from database but failed to apply to kernel: %w", err)
	}

	return nil
}

// BulkRemoveConfigRoutes deletes multiple static route configurations from DB and
// updates the kernel. It returns how many routes were actually removed (on a
// partial failure the count covers what was deleted before the error).
func (s *RoutingService) BulkRemoveConfigRoutes(ids []string) (int64, error) {
	var systemIDs []string
	var dbIDs []string
	for _, id := range ids {
		if s.enableEditSystemRoute && strings.HasPrefix(id, "route-sys-") {
			systemIDs = append(systemIDs, id)
		} else {
			dbIDs = append(dbIDs, id)
		}
	}

	var removed int64
	for _, id := range systemIDs {
		if err := s.RemoveConfigRoute(id); err != nil {
			return removed, err
		}
		removed++
	}

	if len(dbIDs) > 0 {
		n, err := s.repo.BulkDeleteRoutes(dbIDs)
		if err != nil {
			return removed, fmt.Errorf("failed to bulk delete routes from database: %w", err)
		}
		removed += n
		// Reconcile system/kernel routing table
		if err := s.reconcileKernelRoutingTable(); err != nil {
			return removed, fmt.Errorf("routes deleted from database but failed to apply to kernel: %w", err)
		}
	}
	return removed, nil
}

// ToggleConfigRoute toggles route status in the DB and reconciles kernel routing.
func (s *RoutingService) ToggleConfigRoute(id string) error {
	if s.enableEditSystemRoute && strings.HasPrefix(id, "route-sys-") {
		return s.ToggleSystemRouteDirectly(id)
	}

	if err := s.repo.ToggleRouteStatus(id); err != nil {
		return fmt.Errorf("failed to toggle route status in database: %w", err)
	}

	// Reconcile system/kernel routing table
	if err := s.reconcileKernelRoutingTable(); err != nil {
		return fmt.Errorf("route status toggled in database but failed to apply to kernel: %w", err)
	}

	return nil
}

// InitApplyConfig applies database static routing configurations directly to the kernel at startup.
func (s *RoutingService) InitApplyConfig() error {
	log.Printf("[Startup] Fetching static routes from database...")
	dbRoutes, err := s.GetDatabaseRouting()
	if err != nil {
		return fmt.Errorf("failed to load static routes from DB: %w", err)
	}

	log.Printf("[Startup] Database routes: %v", dbRoutes)

	// We apply them by passing the list to kernel RoutingManager.
	// We call ApplyRoutes, which reconciles all configured DB routes with the kernel.
	log.Printf("[Startup] Applying %d static routes configuration to kernel...", len(dbRoutes))
	if err := s.routing.ApplyRoutes(dbRoutes); err != nil {
		return fmt.Errorf("failed to apply static routes to kernel: %w", err)
	}

	log.Printf("[Startup] Successfully applied static routes configuration at startup.")
	return nil
}

// ReconcileKernelRoutingTable is the exported entry point for reconciling kernel
// routing state against the DB, invoked by the NetEventBus AddrRouteChanged/LinkChanged
// subscription (the running-state counterpart of InitApplyConfig). It wraps the
// unexported worker so the wiring in main.go has a stable, documented hook.
func (s *RoutingService) ReconcileKernelRoutingTable() error {
	return s.reconcileKernelRoutingTable()
}

// reconcileKernelRoutingTable loads all DB routes and reconciles kernel routing state.
func (s *RoutingService) reconcileKernelRoutingTable() error {
	dbRoutes, err := s.GetDatabaseRouting()
	if err != nil {
		return fmt.Errorf("failed to fetch database routes: %w", err)
	}
	if err := s.routing.ApplyRoutes(dbRoutes); err != nil {
		return err
	}

	// After DB static routes are applied, enforce per-interface default-route metrics
	// for dhcp interfaces (dhcpcd installs its own default route with its own metric;
	// this overrides it for multi-WAN failover ordering).
	s.enforceInterfaceMetrics(dbRoutes)
	return nil
}

// enforceInterfaceMetrics overrides the default-route priority for interfaces
// according to a 4-level precedence order (docs/ref/todo/
// multi-wan-failover-plan.md Task 14 / D-2 — also recorded permanently in
// tech_stack_design.md). It is deliberately non-fatal throughout (logs and
// continues) so a single interface error doesn't abort routing
// reconciliation.
//
// Precedence, highest to lowest, PER INTERFACE:
//  1. An active DB static route for 0.0.0.0/0 on the interface — ApplyRoutes
//     already owns that route's metric; NEVER enforce anything here,
//     override or not (an override affected by this is reported via
//     FailoverBypassedInterfaces() for the API/UI, T-16, but is otherwise a
//     no-op until the static route is removed/disabled).
//  2. A Task 14 WAN failover metric override (failoverOverrides) — wins
//     regardless of AddressingMode or whether the interface even has its own
//     Metric configured, because a failover decision must be able to move
//     traffic off ANY uplink, not just ones an operator happened to set a
//     metric on. The pre-override kernel metric is snapshotted (Decision C)
//     the first time an override starts on that interface.
//  3. A pending restore (restorePending) — the override was just
//     cleared/removed for this interface; restore its pre-override snapshot
//     (or its own configured Metric, if no snapshot exists) exactly once,
//     then go quiet.
//  4. Otherwise: the original (pre-Task-14) Phase 1 behavior, unchanged —
//     enforce the interface's own configured Metric only when
//     AddressingMode=="dhcp" and Metric is set.
func (s *RoutingService) enforceInterfaceMetrics(dbRoutes []model.StaticRoute) {
	ifaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		log.Printf("[Routing] Warning: could not load interfaces for metric enforcement: %v", err)
		return
	}

	// Interfaces that have an active DB default route — static_routes takes precedence there.
	dbDefaultRouteIfaces := make(map[string]bool)
	for _, rt := range dbRoutes {
		if rt.Status && isDefaultDestination(rt.Destination) {
			dbDefaultRouteIfaces[rt.Interface] = true
		}
	}

	s.mu.Lock()
	overrides := make(map[string]int, len(s.failoverOverrides))
	for k, v := range s.failoverOverrides {
		overrides[k] = v
	}
	restorePending := make(map[string]bool, len(s.restorePending))
	for k := range s.restorePending {
		restorePending[k] = true
	}
	prevBypassed := make(map[string]bool, len(s.bypassedIfaces))
	for k := range s.bypassedIfaces {
		prevBypassed[k] = true
	}
	s.mu.Unlock()

	newBypassed := make(map[string]bool)

	// Pass 1: every interface known to the DB, in the SAME order
	// GetInterfacesFromDB returned them. When there are no overrides/
	// restores in play at all, this pass alone reproduces the pre-Task-14
	// call order/count/values exactly (the plan's explicit regression
	// requirement) — precedence levels 1-3 above are no-ops in that case.
	seen := make(map[string]bool, len(ifaces))
	for _, iface := range ifaces {
		seen[iface.Name] = true
		s.enforceOneInterfaceMetric(iface.Name, iface, true, dbDefaultRouteIfaces, overrides, restorePending, prevBypassed, newBypassed)
	}

	// Pass 2: any override/restore-pending target with no row in interfaces
	// at all — a WAN uplink may be configured on an interface pigate hasn't
	// learned about (yet), see D-2. Sorted for a deterministic order.
	extraSet := make(map[string]bool)
	for name := range overrides {
		if !seen[name] {
			extraSet[name] = true
		}
	}
	for name := range restorePending {
		if !seen[name] {
			extraSet[name] = true
		}
	}
	extra := make([]string, 0, len(extraSet))
	for name := range extraSet {
		extra = append(extra, name)
	}
	sort.Strings(extra)
	for _, name := range extra {
		s.enforceOneInterfaceMetric(name, model.NetworkInterface{}, false, dbDefaultRouteIfaces, overrides, restorePending, prevBypassed, newBypassed)
	}

	s.mu.Lock()
	s.bypassedIfaces = newBypassed
	s.mu.Unlock()
}

// enforceOneInterfaceMetric applies enforceInterfaceMetrics' 4-level
// precedence to a single interface name.
func (s *RoutingService) enforceOneInterfaceMetric(
	name string,
	iface model.NetworkInterface,
	hasIfaceRow bool,
	dbDefaultRouteIfaces map[string]bool,
	overrides map[string]int,
	restorePending map[string]bool,
	prevBypassed map[string]bool,
	newBypassed map[string]bool,
) {
	override, hasOverride := overrides[name]

	if dbDefaultRouteIfaces[name] {
		// Level 1: static-route-owned — never enforce, override or not.
		if hasOverride {
			newBypassed[name] = true
			if !prevBypassed[name] {
				log.Printf("[Routing] WAN failover override on %s is bypassed by an active static 0.0.0.0/0 route", name)
			}
		} else if hasIfaceRow && iface.AddressingMode == "dhcp" && iface.Metric != nil {
			// Preserves the exact pre-Task-14 skip log for the no-override case.
			log.Printf("[Routing] Skipping metric enforcement on %s: an active static route for 0.0.0.0/0 already governs it", name)
		}
		return
	}

	if hasOverride {
		// Level 2.
		s.applyFailoverOverride(name, override)
		return
	}

	if restorePending[name] {
		// Level 3.
		s.restoreFailoverOverride(name, iface, hasIfaceRow)
		return
	}

	// Level 4: unchanged Phase 1 behavior.
	if hasIfaceRow && iface.AddressingMode == "dhcp" && iface.Metric != nil {
		if err := s.routing.EnforceDefaultRouteMetric(name, *iface.Metric); err != nil {
			log.Printf("[Routing] Warning: failed to enforce metric %d on %s: %v", *iface.Metric, name, err)
		}
	}
}

// applyFailoverOverride enforces a Task 14 WAN failover metric override on
// name, snapshotting the interface's pre-override default-route metric
// (kernel.RoutingManager.DefaultRouteMetric, Decision C) the FIRST time an
// override is seen for this interface — the snapshot survives across
// reconcile passes until this override is cleared/restored (it is not
// re-taken on every pass while the override simply keeps being present).
func (s *RoutingService) applyFailoverOverride(name string, metric int) {
	s.mu.Lock()
	_, alreadySnapshotted := s.preOverrideMetrics[name]
	s.mu.Unlock()

	if !alreadySnapshotted {
		if cur, found, err := s.routing.DefaultRouteMetric(name); err != nil {
			log.Printf("[Routing] Warning: could not snapshot current default-route metric on %s before applying WAN failover override: %v", name, err)
		} else if found {
			s.mu.Lock()
			s.preOverrideMetrics[name] = cur
			s.mu.Unlock()
		}
	}

	if err := s.routing.EnforceDefaultRouteMetric(name, metric); err != nil {
		log.Printf("[Routing] Warning: failed to enforce WAN failover metric override %d on %s: %v", metric, name, err)
	}
}

// restoreFailoverOverride handles the restorePending precedence level: it
// restores name's default-route metric to its pre-override snapshot exactly
// once, falling back to the interface's own configured Metric if no
// snapshot was ever taken (e.g. the interface had no default route yet when
// the override started), and logging a warning + skipping if neither is
// available. Either way the pending entry (and any snapshot) is consumed so
// this never fires again for the same override episode.
//
// Design decision (QA finding, kill-switch-off restore silently no-ops):
// applyFailoverOverride (below) ALWAYS enforces the override the moment it
// is set, regardless of whether a pre-override snapshot could be taken —
// a failover decision (or the kill switch being flipped ON) must be able to
// move traffic off any uplink even if pigate has never observed a live
// default route on it yet. The three-tier fallback here (snapshot > own
// configured Metric > warn-and-leave-as-is) is the symmetric restore-side
// choice: restore the best value we actually know, but never invent one.
// The warn-and-leave-as-is branch is intentionally NOT a bug fix target —
// it is correct for interfaces pigate genuinely has no prior metric
// knowledge of (e.g. a WAN uplink configured on a link that was never
// actually applied/brought up). What WAS a bug (and is fixed separately,
// kernel/mock.go MockNetwork.ConfigureInterface + SetRoutingSeed) is that
// under -mock=true this branch was reachable for the COMMON case too — any
// interface that had gone through a normal ConfigureInterface call (which
// happens for every DB interface at startup) should already have a
// snapshot-able live metric, exactly like a real kernel would.
func (s *RoutingService) restoreFailoverOverride(name string, iface model.NetworkInterface, hasIfaceRow bool) {
	s.mu.Lock()
	snapshot, hasSnapshot := s.preOverrideMetrics[name]
	delete(s.preOverrideMetrics, name)
	delete(s.restorePending, name)
	s.mu.Unlock()

	restoreTo := snapshot
	if !hasSnapshot {
		if hasIfaceRow && iface.Metric != nil {
			restoreTo = *iface.Metric
		} else {
			log.Printf("[Routing] Warning: no snapshot or configured metric available to restore %s after its WAN failover override was cleared; leaving the current kernel metric as-is", name)
			return
		}
	}

	if err := s.routing.EnforceDefaultRouteMetric(name, restoreTo); err != nil {
		log.Printf("[Routing] Warning: failed to restore default-route metric %d on %s after clearing its WAN failover override: %v", restoreTo, name, err)
	}
}

// SetFailoverMetricOverrides atomically replaces the FULL set of Task 14 WAN
// failover metric overrides with desired (ifaceName -> metric). The
// controller (wan_failover.go, T-15) always writes the whole map in one
// call rather than one Set/Clear at a time, so a reconcile pass racing
// between two individual calls can never observe a half-updated state (e.g.
// two interfaces both temporarily at the "active" metric). Any interface
// that HAD an override but is absent from desired is queued for a one-time
// restore (restorePending) on the next enforceInterfaceMetrics pass.
// Idempotent: returns changed=false (and touches nothing) when desired is
// identical to the current override set, so callers can skip an
// unnecessary ReconcileKernelRoutingTable()/kernel round-trip.
func (s *RoutingService) SetFailoverMetricOverrides(desired map[string]int) (changed bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(desired) == len(s.failoverOverrides) {
		same := true
		for k, v := range desired {
			if cur, ok := s.failoverOverrides[k]; !ok || cur != v {
				same = false
				break
			}
		}
		if same {
			return false
		}
	}

	for iface := range s.failoverOverrides {
		if _, stillPresent := desired[iface]; !stillPresent {
			s.restorePending[iface] = true
		}
	}

	s.failoverOverrides = make(map[string]int, len(desired))
	for k, v := range desired {
		s.failoverOverrides[k] = v
	}
	return true
}

// SetFailoverMetricOverride is a thin single-interface wrapper over
// SetFailoverMetricOverrides, for callers (T-16 API layer, tests) that only
// need to touch one interface — multi-interface callers (T-15's controller)
// should prefer SetFailoverMetricOverrides directly for its atomicity.
func (s *RoutingService) SetFailoverMetricOverride(iface string, metric int) bool {
	s.mu.Lock()
	desired := make(map[string]int, len(s.failoverOverrides)+1)
	for k, v := range s.failoverOverrides {
		desired[k] = v
	}
	desired[iface] = metric
	s.mu.Unlock()
	return s.SetFailoverMetricOverrides(desired)
}

// ClearFailoverMetricOverride removes a single interface's override (queuing
// its one-time restore). Idempotent: returns false if iface had no override
// to begin with.
func (s *RoutingService) ClearFailoverMetricOverride(iface string) bool {
	s.mu.Lock()
	if _, ok := s.failoverOverrides[iface]; !ok {
		s.mu.Unlock()
		return false
	}
	desired := make(map[string]int, len(s.failoverOverrides))
	for k, v := range s.failoverOverrides {
		if k != iface {
			desired[k] = v
		}
	}
	s.mu.Unlock()
	return s.SetFailoverMetricOverrides(desired)
}

// ClearAllFailoverMetricOverrides removes every override at once (the kill
// switch's "turn failover off" path, T-15) — idempotent, returns false if
// there was nothing to clear.
func (s *RoutingService) ClearAllFailoverMetricOverrides() (changed bool) {
	return s.SetFailoverMetricOverrides(map[string]int{})
}

// FailoverOverrides returns a defensive copy of the current WAN failover
// metric override set.
func (s *RoutingService) FailoverOverrides() map[string]int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make(map[string]int, len(s.failoverOverrides))
	for k, v := range s.failoverOverrides {
		out[k] = v
	}
	return out
}

// FailoverBypassedInterfaces returns the (sorted, defensive-copy) list of
// interface names that currently have a WAN failover override queued but
// are being overridden by an even-higher-precedence active DB static
// 0.0.0.0/0 route (see enforceInterfaceMetrics' precedence level 1) — used
// by the API/UI (T-16) to warn an operator their failover override is
// currently inert.
func (s *RoutingService) FailoverBypassedInterfaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.bypassedIfaces))
	for k := range s.bypassedIfaces {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// isDefaultDestination reports whether a destination string represents the IPv4
// default route (0.0.0.0/0), tolerating unnormalized forms like "0.0.0.0/0" or "default".
func isDefaultDestination(dest string) bool {
	d := strings.TrimSpace(dest)
	if d == "default" || d == "0.0.0.0/0" {
		return true
	}
	if _, ipNet, err := net.ParseCIDR(d); err == nil {
		return ipNet.String() == "0.0.0.0/0"
	}
	return false
}
