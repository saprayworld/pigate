package service

import (
	"errors"
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

	// eventLog is the central audit/event log (T-23), injected via
	// SetEventLog after both services exist (main.go, mirrors
	// DhcpServerService.SetEventLog). Nil-safe: left nil, enforcement
	// failures are still logged via log.Printf but never reach the event
	// log/UI.
	eventLog *EventLogService

	// reconcileMu serializes the ENTIRE reconcileKernelRoutingTable operation
	// (ApplyRoutes + enforceInterfaceMetrics together) — deliberately a
	// SEPARATE lock from mu below (mu is locked/unlocked repeatedly, in short
	// bursts, from deep inside the enforce path; holding mu across the whole
	// reconcile would deadlock the very code that reconcile itself calls).
	// Why this exists (T-22): every RouteDel/RouteAdd this service performs
	// generates a kernel netlink route-change event that NetlinkMonitor picks
	// up and republishes as AddrRouteChanged, which main.go's "routing" bus
	// subscriber reacts to by calling ReconcileKernelRoutingTable() again —
	// concurrently with the WAN failover controller's own periodic tick and
	// any HTTP-triggered reconcile (e.g. a manual override POST). Without
	// serialization, two goroutines could interleave RouteDel/RouteAdd calls
	// on the very route T-20/T-21 are trying to move safely, widening the
	// window for exactly the "route disappears" bug this whole fix addresses.
	reconcileMu sync.Mutex

	// mu guards ONLY the maps/fields below (Task 14 WAN failover metric
	// overrides, extended by T-23's enforcement-failure log guard) — it is a
	// separate lock from disabledSystemRoutes above (which stays unguarded,
	// matching its pre-Task-14 behavior; this file has never been safe for
	// concurrent enable-edit-system-route callers and Task 14 does not change
	// that) and from reconcileMu above (see its doc comment for why they must
	// stay separate).
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
	// enforceFailedLogged is T-23's per-interface "already logged this
	// enforcement-failure episode to the event log" guard, mirroring
	// wan_failover.go's bypassedLogged pattern — cleared the moment
	// enforcement next succeeds (or becomes a no-op because the interface is
	// already at the desired metric) on that interface, so a future
	// recurrence logs again. Also exposed read-only via
	// EnforceFailedInterfaces() for the API/UI status.
	enforceFailedLogged map[string]bool
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
		enforceFailedLogged:   make(map[string]bool),
	}
}

// SetEventLog injects the central event log (T-23). See the eventLog field
// doc comment — nil-safe, wired once from main.go after both services exist.
func (s *RoutingService) SetEventLog(eventLog *EventLogService) {
	s.eventLog = eventLog
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

// reconcileKernelRoutingTable loads all DB routes and reconciles kernel
// routing state. reconcileMu-guarded (T-22) for its entire duration — see the
// field's doc comment; every exported path that mutates routing
// (ApplyConfigRoute, RemoveConfigRoute, BulkRemoveConfigRoutes,
// ToggleConfigRoute, ReconcileKernelRoutingTable) funnels through this one
// unexported function, so locking here alone is sufficient. None of the code
// this function calls (ApplyRoutes, enforceInterfaceMetrics and everything
// it calls) ever calls back into reconcileKernelRoutingTable or
// ReconcileKernelRoutingTable itself, so there is no self-deadlock risk.
// InitApplyConfig (startup) deliberately does NOT go through this lock: it
// runs once, synchronously, before NetlinkMonitor/the WAN failover
// controller/the HTTP server are started (see cmd/pigate/main.go's startup
// sequence), so there is nothing else that could be reconciling
// concurrently at that point.
func (s *RoutingService) reconcileKernelRoutingTable() error {
	s.reconcileMu.Lock()
	defer s.reconcileMu.Unlock()

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
//
// T-21 (docs/ref/wan-failover-findings.md): when overrides or a pending
// restore ARE in play, the actual kernel calls are additionally split into
// two ordered phases — every DEMOTE (moving an interface to a HIGHER,
// worse metric) before every PROMOTE (moving an interface to a LOWER,
// better metric). This is the invariant that actually fixes the WAN
// failover route-disappears bug: Linux allows only one default route per
// (dst, tos, priority) metric per routing table — netlink RouteAdd uses
// NLM_F_EXCL, so adding a second default route at an already-occupied
// metric fails with EEXIST even though the outgoing interface differs (see
// kernel.ErrDefaultRouteMetricConflict). The uplink being PROMOTED into the
// active band must only receive its new (lower) metric AFTER whatever
// uplink previously held that exact metric has already been DEMOTED off of
// it — i.e. the metric "slot" must be vacated before it is claimed by
// someone else. Getting this ordering wrong (as the pre-T-21 single-pass
// code did, processing interfaces in arbitrary DB order) is exactly what
// let a losing interface's RouteDel succeed while the winning interface's
// RouteAdd failed, leaving the losing interface with ZERO default route.
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

	type ifaceTarget struct {
		name        string
		iface       model.NetworkInterface
		hasIfaceRow bool
	}

	// Build the full target list in the SAME order/composition as before
	// T-21: every DB interface (GetInterfacesFromDB order), then any
	// override/restore-pending name with no DB row at all, sorted.
	seen := make(map[string]bool, len(ifaces))
	targets := make([]ifaceTarget, 0, len(ifaces))
	for _, iface := range ifaces {
		seen[iface.Name] = true
		targets = append(targets, ifaceTarget{iface.Name, iface, true})
	}
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
		targets = append(targets, ifaceTarget{name, model.NetworkInterface{}, false})
	}

	// CRITICAL REGRESSION GUARD (Task 14): with no overrides and no pending
	// restore in play at all, precedence levels 2/3 can never fire for ANY
	// interface — only level 1 (bypass bookkeeping, no kernel call) and
	// level 4 (unchanged Phase 1 behavior) ever run. The two-phase
	// demote/promote machinery below would be a pure no-op in that case
	// anyway (every entry would land in one bucket, in the same relative
	// order), but classifying demote-vs-promote requires an EXTRA
	// DefaultRouteMetric kernel round-trip per interface that pre-Task-14
	// code never made — so short-circuit straight back to the original
	// single-pass call sequence to guarantee byte-for-byte parity (call
	// order/count/values) with pre-Task-14 behavior, exactly as the plan
	// requires.
	if len(overrides) == 0 && len(restorePending) == 0 {
		for _, t := range targets {
			_ = s.enforceOneInterfaceMetric(t.name, t.iface, t.hasIfaceRow, dbDefaultRouteIfaces, overrides, restorePending, prevBypassed, newBypassed)
		}
		s.mu.Lock()
		s.bypassedIfaces = newBypassed
		s.mu.Unlock()
		return
	}

	// --- Two-phase path (T-21): overrides and/or a pending restore exist ---

	// PLAN: classify every target, WITHOUT touching the kernel or mutating
	// any service state, as either "nothing to enforce here right now"
	// (level 1 bypass, or no applicable precedence level at all — dispatched
	// immediately below since it never issues a kernel call and its ordering
	// relative to the enforced group is irrelevant) or "will be enforced at
	// metric X" (deferred into the demote/promote classification).
	var toEnforce []ifaceTarget
	targetMetric := make(map[string]int, len(targets))
	for _, t := range targets {
		if metric, ok := s.previewOneInterfaceMetric(t.name, t.iface, t.hasIfaceRow, dbDefaultRouteIfaces, overrides, restorePending); ok {
			targetMetric[t.name] = metric
			toEnforce = append(toEnforce, t)
			continue
		}
		_ = s.enforceOneInterfaceMetric(t.name, t.iface, t.hasIfaceRow, dbDefaultRouteIfaces, overrides, restorePending, prevBypassed, newBypassed)
	}

	// Classify each planned target as a DEMOTE (target metric higher/worse
	// than what RoutingManager.DefaultRouteMetric currently reports for that
	// interface) or a PROMOTE (target lower/better than current). An unknown
	// current value (no live default route yet, or the read itself errored)
	// carries no collision risk either way, so it is treated as a promote —
	// safe to run in any order since there is nothing on that interface to
	// vacate yet.
	var demotes, promotes []ifaceTarget
	for _, t := range toEnforce {
		metric := targetMetric[t.name]
		cur, found, err := s.routing.DefaultRouteMetric(t.name)
		switch {
		case err != nil:
			log.Printf("[Routing] Warning: could not read current default-route metric on %s before enforcing %d (proceeding without demote/promote ordering for this interface): %v", t.name, metric, err)
			promotes = append(promotes, t)
		case !found:
			promotes = append(promotes, t)
		case metric > cur:
			demotes = append(demotes, t)
		default:
			promotes = append(promotes, t)
		}
	}

	// EXECUTE: every demote, then every promote. If a kernel call reports
	// kernel.ErrDefaultRouteMetricConflict (another interface still holds
	// the target metric — an ordering edge case, e.g. an interface outside
	// the DB, or the conflicting interface's own demote failing), retry it
	// exactly once after the full demote+promote sweep has completed, by
	// which point the conflict has very likely resolved itself.
	type conflict struct {
		name   string
		metric int
	}
	var conflicts []conflict
	commit := func(t ifaceTarget) {
		if err := s.enforceOneInterfaceMetric(t.name, t.iface, t.hasIfaceRow, dbDefaultRouteIfaces, overrides, restorePending, prevBypassed, newBypassed); err != nil {
			if errors.Is(err, kernel.ErrDefaultRouteMetricConflict) {
				conflicts = append(conflicts, conflict{t.name, targetMetric[t.name]})
			}
		}
	}
	for _, t := range demotes {
		commit(t)
	}
	for _, t := range promotes {
		commit(t)
	}
	for _, c := range conflicts {
		log.Printf("[Routing] Retrying metric enforcement on %s at %d after a metric-slot conflict (likely a demote/promote ordering edge case involving an interface outside the DB)", c.name, c.metric)
		if err := s.routing.EnforceDefaultRouteMetric(c.name, c.metric); err != nil {
			log.Printf("[Routing] Warning: retry failed to enforce metric %d on %s: %v", c.metric, c.name, err)
			s.reportEnforceFailure(c.name, c.metric, "retry", err)
		} else {
			s.clearEnforceFailedInterface(c.name)
		}
	}

	s.mu.Lock()
	s.bypassedIfaces = newBypassed
	s.mu.Unlock()
}

// previewOneInterfaceMetric mirrors enforceOneInterfaceMetric's 4-level
// precedence decision, WITHOUT any side effect whatsoever (no kernel calls,
// no snapshot-taking, no consuming restorePending, no logging) — used purely
// by the two-phase (demote-before-promote) planning pass to learn what
// target metric (if any) an interface WOULD be enforced to, so the real
// (side-effecting) enforcement can be scheduled in the correct order. ok is
// false when this interface has nothing to enforce right now, either because
// level 1 (an active DB static default route) bypasses it, or because none
// of levels 2-4 apply.
func (s *RoutingService) previewOneInterfaceMetric(
	name string,
	iface model.NetworkInterface,
	hasIfaceRow bool,
	dbDefaultRouteIfaces map[string]bool,
	overrides map[string]int,
	restorePending map[string]bool,
) (metric int, ok bool) {
	if dbDefaultRouteIfaces[name] {
		return 0, false
	}
	if m, hasOverride := overrides[name]; hasOverride {
		return m, true
	}
	if restorePending[name] {
		s.mu.Lock()
		snapshot, hasSnapshot := s.preOverrideMetrics[name]
		s.mu.Unlock()
		if hasSnapshot {
			return snapshot, true
		}
		if hasIfaceRow && iface.Metric != nil {
			return *iface.Metric, true
		}
		return 0, false
	}
	if hasIfaceRow && iface.AddressingMode == "dhcp" && iface.Metric != nil {
		return *iface.Metric, true
	}
	return 0, false
}

// enforceOneInterfaceMetric applies enforceInterfaceMetrics' 4-level
// precedence to a single interface name, actually touching the kernel (and
// consuming/mutating service state for levels 2/3) as needed. Returns the
// error (if any) the underlying kernel.RoutingManager.EnforceDefaultRouteMetric
// call reported — nil when nothing needed enforcing or it succeeded — so the
// two-phase caller can detect kernel.ErrDefaultRouteMetricConflict and retry.
// Errors are always logged here regardless of what the caller does with the
// return value, matching this function's pre-T-21 log-and-continue contract.
func (s *RoutingService) enforceOneInterfaceMetric(
	name string,
	iface model.NetworkInterface,
	hasIfaceRow bool,
	dbDefaultRouteIfaces map[string]bool,
	overrides map[string]int,
	restorePending map[string]bool,
	prevBypassed map[string]bool,
	newBypassed map[string]bool,
) error {
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
		return nil
	}

	if hasOverride {
		// Level 2.
		return s.applyFailoverOverride(name, override)
	}

	if restorePending[name] {
		// Level 3.
		return s.restoreFailoverOverride(name, iface, hasIfaceRow)
	}

	// Level 4: unchanged Phase 1 behavior.
	if hasIfaceRow && iface.AddressingMode == "dhcp" && iface.Metric != nil {
		if err := s.routing.EnforceDefaultRouteMetric(name, *iface.Metric); err != nil {
			log.Printf("[Routing] Warning: failed to enforce metric %d on %s: %v", *iface.Metric, name, err)
			return err
		}
	}
	return nil
}

// applyFailoverOverride enforces a Task 14 WAN failover metric override on
// name, snapshotting the interface's pre-override default-route metric
// (kernel.RoutingManager.DefaultRouteMetric, Decision C) the FIRST time an
// override is seen for this interface — the snapshot survives across
// reconcile passes until this override is cleared/restored (it is not
// re-taken on every pass while the override simply keeps being present).
//
// T-21: also skips the actual EnforceDefaultRouteMetric call entirely when
// the interface is already observed at the desired metric — this is safe to
// add here (unlike inside enforceOneInterfaceMetric's shared level-4 branch)
// because this function is only ever reached via the T-21 two-phase path
// (the Task-14 regression guard above guarantees overrides is empty on the
// legacy single-pass path, so this function is simply never called there).
//
// T-23: reports a failed enforcement to the central event log (log-once per
// episode, see reportEnforceFailure) so a stuck WAN failover override is
// visible to an operator instead of only ever appearing in a log line.
func (s *RoutingService) applyFailoverOverride(name string, metric int) error {
	s.mu.Lock()
	_, alreadySnapshotted := s.preOverrideMetrics[name]
	s.mu.Unlock()

	cur, found, err := s.routing.DefaultRouteMetric(name)
	if err != nil {
		log.Printf("[Routing] Warning: could not read current default-route metric on %s before applying WAN failover override: %v", name, err)
	} else if found && !alreadySnapshotted {
		s.mu.Lock()
		s.preOverrideMetrics[name] = cur
		s.mu.Unlock()
	}

	if err == nil && found && cur == metric {
		// Already at the desired metric — nothing to enforce, and any prior
		// enforcement-failure episode for this interface is now over.
		s.clearEnforceFailedInterface(name)
		return nil
	}

	if enforceErr := s.routing.EnforceDefaultRouteMetric(name, metric); enforceErr != nil {
		log.Printf("[Routing] Warning: failed to enforce WAN failover metric override %d on %s: %v", metric, name, enforceErr)
		s.reportEnforceFailure(name, metric, "failover-override", enforceErr)
		return enforceErr
	}
	s.clearEnforceFailedInterface(name)
	return nil
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
// T-21: like applyFailoverOverride, this function is only ever reached via
// the two-phase path — restorePending is guaranteed empty on the legacy
// single-pass (regression-guarded) path — so it is safe to add a
// skip-if-already-at-target check here without risking the Task 14
// regression guard.
//
// T-23: reports a failed restore to the central event log (log-once per
// episode) so a stuck kill-switch-off restore is visible to an operator.
func (s *RoutingService) restoreFailoverOverride(name string, iface model.NetworkInterface, hasIfaceRow bool) error {
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
			return nil
		}
	}

	if cur, found, err := s.routing.DefaultRouteMetric(name); err == nil && found && cur == restoreTo {
		s.clearEnforceFailedInterface(name)
		return nil
	}

	if err := s.routing.EnforceDefaultRouteMetric(name, restoreTo); err != nil {
		log.Printf("[Routing] Warning: failed to restore default-route metric %d on %s after clearing its WAN failover override: %v", restoreTo, name, err)
		s.reportEnforceFailure(name, restoreTo, "failover-restore", err)
		return err
	}
	s.clearEnforceFailedInterface(name)
	return nil
}

// reportEnforceFailure logs an EnforceDefaultRouteMetric failure occurring
// while applying/restoring a Task 14 WAN failover metric override (T-23) —
// the scenario where a failed enforcement is a genuinely urgent,
// user-visible condition (a WAN uplink silently losing its default route).
// cause is a short machine-readable tag ("failover-override"/
// "failover-restore"/"retry") folded into the message. Logs to the central
// event log at most ONCE per "episode" (see enforceFailedLogged's doc
// comment), cleared by clearEnforceFailedInterface once enforcement next
// succeeds, so a persisting failure does not spam the event log on every
// reconcile tick. Nil-safe with respect to s.eventLog (log.Printf at the
// call site always happens regardless).
func (s *RoutingService) reportEnforceFailure(name string, metric int, cause string, err error) {
	s.mu.Lock()
	alreadyLogged := s.enforceFailedLogged[name]
	s.enforceFailedLogged[name] = true
	s.mu.Unlock()

	if alreadyLogged || s.eventLog == nil {
		return
	}
	s.eventLog.Log(model.EventCategoryNetwork, "wan-failover", model.EventSeverityCritical,
		model.EventActorSystem, name,
		fmt.Sprintf("Failed to enforce WAN failover default-route metric %d on interface %s (%s): %v", metric, name, cause, err))
}

// clearEnforceFailedInterface resets reportEnforceFailure's per-interface
// "already logged this episode" guard once enforcement succeeds (or becomes
// a no-op because the interface is already at the desired metric) — a
// future recurrence logs again.
func (s *RoutingService) clearEnforceFailedInterface(name string) {
	s.mu.Lock()
	delete(s.enforceFailedLogged, name)
	s.mu.Unlock()
}

// EnforceFailedInterfaces returns the (sorted, defensive-copy) list of
// interface names currently within a logged WAN-failover
// enforcement-failure episode (T-23) — used by the API/UI to surface a
// currently-stuck override/restore the same way FailoverBypassedInterfaces
// already surfaces a bypassed one.
func (s *RoutingService) EnforceFailedInterfaces() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, 0, len(s.enforceFailedLogged))
	for k := range s.enforceFailedLogged {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
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
