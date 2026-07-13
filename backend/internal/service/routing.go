package service

import (
	"fmt"
	"log"
	"net"
	"strings"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type RoutingService struct {
	repo                  *db.Repository
	routing               kernel.RoutingManager
	enableEditSystemRoute bool
	disabledSystemRoutes  map[string]model.StaticRoute
}

func NewRoutingService(repo *db.Repository, routing kernel.RoutingManager) *RoutingService {
	return &RoutingService{
		repo:                  repo,
		routing:               routing,
		enableEditSystemRoute: false,
		disabledSystemRoutes:  make(map[string]model.StaticRoute),
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

// enforceInterfaceMetrics overrides the default-route priority for every dhcp interface
// that has an explicit Metric set. It is deliberately non-fatal (logs and continues) so a
// single interface error doesn't abort routing reconciliation.
//
// Precedence rule (see interface-metric-design.md §4.1): if the user also configured an
// active DB static route for 0.0.0.0/0 on the same interface, ApplyRoutes already owns
// that route's metric. Enforcing the interface metric on top would cause a del/add
// ping-pong between the two mechanisms, so we let static_routes win and skip enforcement.
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

	for _, iface := range ifaces {
		if iface.Metric == nil || iface.AddressingMode != "dhcp" {
			continue
		}
		if dbDefaultRouteIfaces[iface.Name] {
			log.Printf("[Routing] Skipping metric enforcement on %s: an active static route for 0.0.0.0/0 already governs it", iface.Name)
			continue
		}
		if err := s.routing.EnforceDefaultRouteMetric(iface.Name, *iface.Metric); err != nil {
			log.Printf("[Routing] Warning: failed to enforce metric %d on %s: %v", *iface.Metric, iface.Name, err)
		}
	}
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
