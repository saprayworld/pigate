package service

import (
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
)

type trackingRoutingManager struct {
	appliedRoutes []model.StaticRoute
}

func (t *trackingRoutingManager) ApplyRoutes(routes []model.StaticRoute) error {
	t.appliedRoutes = routes
	return nil
}

func TestGetRouting(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	// Enable mock mode so GetKernelRouting returns standard mock kernel routes:
	// - 0.0.0.0/0 via 10.0.0.1 on wlan0 (type defaultgateway)
	// - 192.168.1.0/24 on eth0 (type system)
	// - 10.0.0.0/24 on wlan0 (type system)
	repo.SetMockMode(true, false)

	// Clean DB routes first
	// Note: since this is in-memory DB, default migrations might seed default routes. Let's make sure.
	if _, err := repo.GetDatabaseRoutes(); err != nil {
		t.Fatalf("GetDatabaseRoutes failed: %v", err)
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	// Fetch merged routing
	merged, err := svc.GetRouting()
	if err != nil {
		t.Fatalf("GetRouting failed: %v", err)
	}

	// We expect mock kernel routes to be present in the merged list.
	// Let's verify presence of at least the default gateway route.
	foundDefault := false
	for _, r := range merged {
		if r.Destination == "0.0.0.0/0" {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Errorf("Expected to find default gateway route 0.0.0.0/0 in merged list, but it was missing")
	}

	// Add a custom route to database
	customRoute := model.StaticRoute{
		ID:          "route-custom-1",
		Destination: "172.16.0.0/16",
		Gateway:     "192.168.1.254",
		Interface:   "eth0",
		Metric:      10,
		Description: "Office Internal Net",
		Status:      true,
		Type:        "custom",
	}
	if err := repo.CreateRoute(customRoute); err != nil {
		t.Fatalf("Failed to create route: %v", err)
	}

	// Fetch merged again
	merged, err = svc.GetRouting()
	if err != nil {
		t.Fatalf("GetRouting failed: %v", err)
	}

	// Now we expect BOTH default gateway and the custom route to be present in the merged list.
	foundCustom := false
	for _, r := range merged {
		if r.ID == "route-custom-1" && r.Destination == "172.16.0.0/16" {
			foundCustom = true
		}
	}
	if !foundCustom {
		t.Errorf("Expected custom route route-custom-1 in merged list, but it was missing")
	}

	// Check that the kernel was NOT updated by GetRouting
	if len(tracker.appliedRoutes) > 0 {
		t.Errorf("GetRouting should NOT update or apply routes to the kernel directly")
	}
}

func TestApplyAndRemoveConfigRoute(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	route := model.StaticRoute{
		ID:          "route-custom-test",
		Destination: "8.8.8.8/32",
		Gateway:     "10.0.0.1",
		Interface:   "wlan0",
		Metric:      50,
		Description: "Google DNS Route",
		Status:      true,
		Type:        "custom",
	}

	// 1. Apply config route (should save to DB and reconcile kernel)
	if err := svc.ApplyConfigRoute(route); err != nil {
		t.Fatalf("ApplyConfigRoute failed: %v", err)
	}

	// Verify DB state
	dbRoute, err := repo.GetRouteByID("route-custom-test")
	if err != nil || dbRoute == nil {
		t.Fatalf("Route was not saved in DB: %v", err)
	}
	if dbRoute.Destination != "8.8.8.8/32" {
		t.Errorf("Expected destination '8.8.8.8/32', got '%s'", dbRoute.Destination)
	}

	// Verify Kernel state (tracker should have been called)
	foundInKernel := false
	for _, r := range tracker.appliedRoutes {
		if r.ID == "route-custom-test" {
			foundInKernel = true
		}
	}
	if !foundInKernel {
		t.Errorf("Route was not applied to kernel via tracker")
	}

	// 2. Toggle config route status (should update DB and reconcile kernel)
	if err := svc.ToggleConfigRoute("route-custom-test"); err != nil {
		t.Fatalf("ToggleConfigRoute failed: %v", err)
	}

	dbRouteToggled, _ := repo.GetRouteByID("route-custom-test")
	if dbRouteToggled.Status {
		t.Errorf("Expected route status to be false after toggle, got true")
	}

	// Verify that the toggled route (status=false) was passed to the kernel reconciliation
	foundToggledInKernel := false
	for _, r := range tracker.appliedRoutes {
		if r.ID == "route-custom-test" && !r.Status {
			foundToggledInKernel = true
		}
	}
	if !foundToggledInKernel {
		t.Errorf("Toggled route (status=false) was not applied to kernel reconciliation")
	}

	// 3. Remove config route (should delete from DB and reconcile kernel)
	if err := svc.RemoveConfigRoute("route-custom-test"); err != nil {
		t.Fatalf("RemoveConfigRoute failed: %v", err)
	}

	dbRouteDel, _ := repo.GetRouteByID("route-custom-test")
	if dbRouteDel != nil {
		t.Errorf("Expected route to be deleted from DB, but it still exists")
	}

	// Kernel reconciliation should have been called without the deleted route
	foundDeletedInKernel := false
	for _, r := range tracker.appliedRoutes {
		if r.ID == "route-custom-test" {
			foundDeletedInKernel = true
		}
	}
	if foundDeletedInKernel {
		t.Errorf("Route was still found in kernel reconciliation list after deletion")
	}
}

func TestInitApplyConfig(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)

	// Seed custom route to DB
	route := model.StaticRoute{
		ID:          "route-seed-1",
		Destination: "1.1.1.1/32",
		Gateway:     "10.0.0.1",
		Interface:   "wlan0",
		Metric:      5,
		Status:      true,
		Type:        "custom",
	}
	if err := repo.CreateRoute(route); err != nil {
		t.Fatalf("Failed to seed route: %v", err)
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	// Execute InitApplyConfig
	if err := svc.InitApplyConfig(); err != nil {
		t.Fatalf("InitApplyConfig failed: %v", err)
	}

	// Verify that the seeded route was applied to kernel RoutingManager
	found := false
	for _, r := range tracker.appliedRoutes {
		if r.ID == "route-seed-1" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected seeded route route-seed-1 to be applied to kernel during InitApplyConfig")
	}
}
