package service

import (
	"testing"

	"pigate/internal/db"
	"pigate/internal/model"
)

type trackingRoutingManager struct {
	appliedRoutes         []model.StaticRoute
	addedRoutes           []model.StaticRoute
	deletedRoutes         []model.StaticRoute
	enableEditSystemRoute bool
	enforcedMetrics       map[string]int // ifaceName -> metric passed to EnforceDefaultRouteMetric
}

func (t *trackingRoutingManager) EnforceDefaultRouteMetric(ifaceName string, metric int) error {
	if t.enforcedMetrics == nil {
		t.enforcedMetrics = make(map[string]int)
	}
	t.enforcedMetrics[ifaceName] = metric
	return nil
}

func (t *trackingRoutingManager) ApplyRoutes(routes []model.StaticRoute) error {
	t.appliedRoutes = routes
	return nil
}

func (t *trackingRoutingManager) AddRoute(route model.StaticRoute) error {
	t.addedRoutes = append(t.addedRoutes, route)
	return nil
}

func (t *trackingRoutingManager) DeleteRoute(route model.StaticRoute) error {
	t.deletedRoutes = append(t.deletedRoutes, route)
	return nil
}

func (t *trackingRoutingManager) SetEnableEditSystemRoute(enable bool) {
	t.enableEditSystemRoute = enable
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

// TestReconcileEnforcesInterfaceMetric verifies that reconciliation enforces the
// default-route metric only for dhcp interfaces that set one, skips static ones,
// and yields to an active DB static default route (precedence rule §4.1).
func TestReconcileEnforcesInterfaceMetric(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)
	if err := repo.ClearInterfaces(); err != nil {
		t.Fatalf("Failed to clear interfaces: %v", err)
	}

	metric50 := 50
	metric60 := 60
	metric70 := 70
	seed := []model.NetworkInterface{
		{ID: "if-wan0", Name: "wan0", Alias: "A", Role: "WAN", Type: "ethernet", AddressingMode: "dhcp", Status: "up", Metric: &metric50},   // enforced
		{ID: "if-wan1", Name: "wan1", Alias: "B", Role: "WAN", Type: "ethernet", AddressingMode: "dhcp", Status: "up"},                      // no metric -> skipped
		{ID: "if-wan2", Name: "wan2", Alias: "C", Role: "WAN", Type: "ethernet", AddressingMode: "static", Status: "up", Metric: &metric60}, // static -> skipped
		{ID: "if-wan3", Name: "wan3", Alias: "D", Role: "WAN", Type: "ethernet", AddressingMode: "dhcp", Status: "up", Metric: &metric70},   // has DB default route -> skipped
	}
	for _, iface := range seed {
		if err := repo.CreateInterfaceForTest(iface); err != nil {
			t.Fatalf("Failed to seed interface %s: %v", iface.Name, err)
		}
	}

	// Active DB default route on wan3 -> static_routes wins, enforcement must skip wan3.
	if err := repo.CreateRoute(model.StaticRoute{
		ID:          "route-wan3-default",
		Destination: "0.0.0.0/0",
		Gateway:     "10.0.3.1",
		Interface:   "wan3",
		Metric:      80,
		Status:      true,
		Type:        "customgateway",
	}); err != nil {
		t.Fatalf("Failed to seed default route: %v", err)
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	if err := svc.reconcileKernelRoutingTable(); err != nil {
		t.Fatalf("reconcileKernelRoutingTable failed: %v", err)
	}

	if got, ok := tracker.enforcedMetrics["wan0"]; !ok || got != 50 {
		t.Errorf("expected wan0 metric enforced to 50, got %d (present=%v)", got, ok)
	}
	if _, ok := tracker.enforcedMetrics["wan1"]; ok {
		t.Errorf("wan1 has no metric; enforcement should have been skipped")
	}
	if _, ok := tracker.enforcedMetrics["wan2"]; ok {
		t.Errorf("wan2 is static; enforcement should have been skipped")
	}
	if _, ok := tracker.enforcedMetrics["wan3"]; ok {
		t.Errorf("wan3 has an active DB default route; enforcement should have been skipped (precedence)")
	}
}

func TestEnableEditSystemRouteDirectly(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)
	svc.SetEnableEditSystemRoute(true)

	// Verify setter worked
	if !svc.IsEnableEditSystemRoute() {
		t.Errorf("Expected IsEnableEditSystemRoute to return true")
	}

	// 1. Create a system route directly bypassing DB
	systemRoute := model.StaticRoute{
		ID:          "route-sys-10_0_0_0_24--wlan0",
		Destination: "10.0.0.0/24",
		Gateway:     "",
		Interface:   "wlan0",
		Metric:      0,
		Status:      true,
		Type:        "system",
		KernelOnly:  true,
	}

	if err := svc.ApplyConfigRoute(systemRoute); err != nil {
		t.Fatalf("ApplyConfigRoute failed for system route: %v", err)
	}

	// Verify it was NOT saved to DB
	dbRoute, _ := repo.GetRouteByID(systemRoute.ID)
	if dbRoute != nil {
		t.Errorf("System route should NOT be saved in DB")
	}

	// Verify it was added to the tracker/kernel directly
	if len(tracker.addedRoutes) != 1 || tracker.addedRoutes[0].ID != systemRoute.ID {
		t.Errorf("System route was not directly added to kernel")
	}

	// Reset tracker to isolate the update step
	tracker.deletedRoutes = nil
	tracker.addedRoutes = nil

	// 1.5 Update the system route (e.g. change metric and gateway)
	updatedSystemRoute := systemRoute
	updatedSystemRoute.Metric = 20
	updatedSystemRoute.Gateway = "10.0.0.254"
	if err := svc.ApplyConfigRoute(updatedSystemRoute); err != nil {
		t.Fatalf("ApplyConfigRoute failed for updating system route: %v", err)
	}

	// Verify the old system route was deleted from kernel, and new one added
	if len(tracker.deletedRoutes) != 1 || tracker.deletedRoutes[0].ID != systemRoute.ID {
		t.Errorf("Expected old system route to be deleted from kernel during update, but it wasn't")
	}
	if len(tracker.addedRoutes) != 1 || tracker.addedRoutes[0].Metric != 20 {
		t.Errorf("Expected updated system route to be added to kernel, but it wasn't")
	}

	// Clear/Reset tracker history for subsequent steps to work with the same lengths
	tracker.deletedRoutes = nil
	tracker.addedRoutes = []model.StaticRoute{updatedSystemRoute}
	systemRoute = updatedSystemRoute

	// 2. Toggle the route (disable it)
	if err := svc.ToggleConfigRoute(systemRoute.ID); err != nil {
		t.Fatalf("ToggleConfigRoute failed: %v", err)
	}

	// Verify it was deleted from kernel (added to tracker.deletedRoutes)
	if len(tracker.deletedRoutes) != 1 || tracker.deletedRoutes[0].ID != systemRoute.ID {
		t.Errorf("System route was not directly deleted from kernel during toggle-disable")
	}

	// Verify it shows up in merged list as disabled
	merged, err := svc.GetRouting()
	if err != nil {
		t.Fatalf("GetRouting failed: %v", err)
	}
	foundDisabled := false
	for _, r := range merged {
		if r.ID == systemRoute.ID && !r.Status {
			foundDisabled = true
			break
		}
	}
	if !foundDisabled {
		t.Errorf("Expected disabled system route in GetRouting output, but not found or status is true")
	}

	// 3. Toggle it back (enable it)
	if err := svc.ToggleConfigRoute(systemRoute.ID); err != nil {
		t.Fatalf("ToggleConfigRoute failed: %v", err)
	}

	// Verify it was added back to kernel (addedRoutes length should be 2)
	if len(tracker.addedRoutes) != 2 || tracker.addedRoutes[1].ID != systemRoute.ID {
		t.Errorf("System route was not directly re-added to kernel during toggle-enable")
	}

	// 4. Remove the system route
	if err := svc.RemoveConfigRoute(systemRoute.ID); err != nil {
		t.Fatalf("RemoveConfigRoute failed: %v", err)
	}

	// Verify deletedRoutes length is 2
	if len(tracker.deletedRoutes) != 2 || tracker.deletedRoutes[1].ID != systemRoute.ID {
		t.Errorf("System route was not directly deleted from kernel during removal")
	}
}
