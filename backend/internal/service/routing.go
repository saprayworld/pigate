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
	repo    *db.Repository
	routing kernel.RoutingManager
}

func NewRoutingService(repo *db.Repository, routing kernel.RoutingManager) *RoutingService {
	return &RoutingService{
		repo:    repo,
		routing: routing,
	}
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
				if dbRoute.Destination == "0.0.0.0/0" {
					dbRoute.Type = "defaultgateway"
				} else if dbRoute.Gateway == "" {
					dbRoute.Type = "system"
				}
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
			result = append(result, kr)
		}
	}

	return result, nil
}

// ApplyConfigRoute saves static route configuration to the DB and applies it to the kernel.
func (s *RoutingService) ApplyConfigRoute(route model.StaticRoute) error {
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
	if err := s.repo.DeleteRoute(id); err != nil {
		return fmt.Errorf("failed to delete route from database: %w", err)
	}

	// Reconcile system/kernel routing table
	if err := s.reconcileKernelRoutingTable(); err != nil {
		return fmt.Errorf("route deleted from database but failed to apply to kernel: %w", err)
	}

	return nil
}

// BulkRemoveConfigRoutes deletes multiple static route configurations from DB and updates the kernel.
func (s *RoutingService) BulkRemoveConfigRoutes(ids []string) error {
	if err := s.repo.BulkDeleteRoutes(ids); err != nil {
		return fmt.Errorf("failed to bulk delete routes from database: %w", err)
	}

	// Reconcile system/kernel routing table
	if err := s.reconcileKernelRoutingTable(); err != nil {
		return fmt.Errorf("routes deleted from database but failed to apply to kernel: %w", err)
	}

	return nil
}

// ToggleConfigRoute toggles route status in the DB and reconciles kernel routing.
func (s *RoutingService) ToggleConfigRoute(id string) error {
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

	log.Printf("Database routes: %v", dbRoutes)

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
	return s.routing.ApplyRoutes(dbRoutes)
}
