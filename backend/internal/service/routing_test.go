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
	enforcedMetrics       map[string]int // ifaceName -> metric passed to EnforceDefaultRouteMetric (last call wins)
	// enforceCalls is the full ordered history of EnforceDefaultRouteMetric
	// calls (ifaceName, metric) — Task 14 tests need call ORDER and COUNT,
	// not just each interface's most-recent value (enforcedMetrics above).
	enforceCalls []enforceCall
	// defaultRouteMetrics backs DefaultRouteMetric (Task 14, Decision C) —
	// tests seed "the kernel currently has this metric on this interface"
	// via SetDefaultRouteMetric before exercising the snapshot/restore path.
	defaultRouteMetrics map[string]int
	defaultRouteFound   map[string]bool
}

type enforceCall struct {
	iface  string
	metric int
}

func (t *trackingRoutingManager) EnforceDefaultRouteMetric(ifaceName string, metric int) error {
	if t.enforcedMetrics == nil {
		t.enforcedMetrics = make(map[string]int)
	}
	t.enforcedMetrics[ifaceName] = metric
	t.enforceCalls = append(t.enforceCalls, enforceCall{iface: ifaceName, metric: metric})
	return nil
}

// SetDefaultRouteMetric seeds what DefaultRouteMetric(ifaceName) reports —
// mirrors kernel.MockRouting.SetDefaultRouteMetric's test-hook shape.
func (t *trackingRoutingManager) SetDefaultRouteMetric(ifaceName string, metric int, found bool) {
	if t.defaultRouteMetrics == nil {
		t.defaultRouteMetrics = make(map[string]int)
		t.defaultRouteFound = make(map[string]bool)
	}
	t.defaultRouteMetrics[ifaceName] = metric
	t.defaultRouteFound[ifaceName] = found
}

func (t *trackingRoutingManager) DefaultRouteMetric(ifaceName string) (int, bool, error) {
	if !t.defaultRouteFound[ifaceName] {
		return 0, false, nil
	}
	return t.defaultRouteMetrics[ifaceName], true, nil
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

// --- Task 14: WAN failover metric override precedence ---------------------

// TestEnforceInterfaceMetrics_NoOverridesRegressionOrder is the plan's
// explicit regression requirement: with no overrides/restores in play at
// all, enforceInterfaceMetrics must call EnforceDefaultRouteMetric in the
// exact same order/count/values as pre-Task-14 (one call per dhcp+Metric-set
// interface, in GetInterfacesFromDB's order, nothing more).
func TestEnforceInterfaceMetrics_NoOverridesRegressionOrder(t *testing.T) {
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

	m10, m20, m30 := 10, 20, 30
	seed := []model.NetworkInterface{
		{ID: "if-a", Name: "wanA", Alias: "A", Role: "WAN", Type: "ethernet", AddressingMode: "dhcp", Status: "up", Metric: &m10},
		{ID: "if-b", Name: "wanB", Alias: "B", Role: "WAN", Type: "ethernet", AddressingMode: "dhcp", Status: "up", Metric: &m20},
		{ID: "if-c", Name: "wanC", Alias: "C", Role: "WAN", Type: "ethernet", AddressingMode: "dhcp", Status: "up", Metric: &m30},
	}
	for _, iface := range seed {
		if err := repo.CreateInterfaceForTest(iface); err != nil {
			t.Fatalf("Failed to seed interface %s: %v", iface.Name, err)
		}
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	svc.enforceInterfaceMetrics(nil)

	want := []enforceCall{{"wanA", 10}, {"wanB", 20}, {"wanC", 30}}
	if len(tracker.enforceCalls) != len(want) {
		t.Fatalf("expected %d EnforceDefaultRouteMetric calls, got %d: %+v", len(want), len(tracker.enforceCalls), tracker.enforceCalls)
	}
	for i, w := range want {
		if tracker.enforceCalls[i] != w {
			t.Errorf("call[%d] = %+v, want %+v (order must match GetInterfacesFromDB's order exactly with no overrides in play)", i, tracker.enforceCalls[i], w)
		}
	}
}

// TestFailoverOverride_WinsOverDhcpMetric covers precedence level 2 beating
// level 4: a WAN failover override must be enforced instead of the
// interface's own configured Metric, not in addition to / averaged with it.
func TestFailoverOverride_WinsOverDhcpMetric(t *testing.T) {
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
	metric100 := 100
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "if-wan0", Name: "wan0", Alias: "A", Role: "WAN", Type: "ethernet",
		AddressingMode: "dhcp", Status: "up", Metric: &metric100,
	}); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	if changed := svc.SetFailoverMetricOverride("wan0", 50); !changed {
		t.Fatal("expected SetFailoverMetricOverride to report changed=true on first set")
	}

	svc.enforceInterfaceMetrics(nil)

	if got, ok := tracker.enforcedMetrics["wan0"]; !ok || got != 50 {
		t.Errorf("expected override metric 50 to win over the interface's own Metric=100, got %d (present=%v)", got, ok)
	}
}

// TestFailoverOverride_WorksOnStaticInterfaceWithNilMetric covers precedence
// level 2 applying regardless of AddressingMode/Metric-nilness (D-2) — the
// pre-Task-14 code would have skipped this interface entirely.
func TestFailoverOverride_WorksOnStaticInterfaceWithNilMetric(t *testing.T) {
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
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "if-wan2", Name: "wan2", Alias: "C", Role: "WAN", Type: "ethernet",
		AddressingMode: "static", Status: "up", // Metric left nil on purpose
	}); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)
	svc.SetFailoverMetricOverride("wan2", 50)

	svc.enforceInterfaceMetrics(nil)

	if got, ok := tracker.enforcedMetrics["wan2"]; !ok || got != 50 {
		t.Errorf("expected the override to be enforced on a static interface with no configured Metric, got %d (present=%v)", got, ok)
	}
}

// TestFailoverOverride_BypassedByActiveStaticDefaultRoute covers precedence
// level 1: an active DB static 0.0.0.0/0 route on the interface must
// suppress enforcement entirely, override or not, and the override must be
// reported via FailoverBypassedInterfaces() while this is the case.
func TestFailoverOverride_BypassedByActiveStaticDefaultRoute(t *testing.T) {
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
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "if-wan0", Name: "wan0", Alias: "A", Role: "WAN", Type: "ethernet",
		AddressingMode: "dhcp", Status: "up",
	}); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)
	svc.SetFailoverMetricOverride("wan0", 50)

	dbRoutes := []model.StaticRoute{
		{ID: "route-wan0-default", Destination: "0.0.0.0/0", Gateway: "10.0.0.1", Interface: "wan0", Status: true, Type: "customgateway"},
	}
	svc.enforceInterfaceMetrics(dbRoutes)

	if len(tracker.enforceCalls) != 0 {
		t.Errorf("expected NO EnforceDefaultRouteMetric calls while an active static 0.0.0.0/0 route governs wan0, got %d calls: %+v", len(tracker.enforceCalls), tracker.enforceCalls)
	}
	bypassed := svc.FailoverBypassedInterfaces()
	if len(bypassed) != 1 || bypassed[0] != "wan0" {
		t.Errorf("expected FailoverBypassedInterfaces() == [wan0], got %v", bypassed)
	}

	// A second reconcile pass in the same state must stay silent/no-op too
	// (transition-only logging is not directly observable here, but the
	// zero-enforce-calls/bypassed-set invariant must hold every pass).
	svc.enforceInterfaceMetrics(dbRoutes)
	if len(tracker.enforceCalls) != 0 {
		t.Errorf("expected still zero EnforceDefaultRouteMetric calls on the 2nd pass, got %d", len(tracker.enforceCalls))
	}
}

// TestFailoverOverride_SetSameValueIsIdempotent covers the required
// idempotency of the override-mutation API itself: re-setting the exact same
// value must report changed=false so callers (T-15) know not to trigger a
// redundant kernel reconcile.
func TestFailoverOverride_SetSameValueIsIdempotent(t *testing.T) {
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	defer sqliteDB.Close()

	repo := db.NewRepository(sqliteDB)
	repo.SetMockMode(true, false)

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	if changed := svc.SetFailoverMetricOverride("wan0", 50); !changed {
		t.Fatal("expected changed=true on first set")
	}
	if changed := svc.SetFailoverMetricOverride("wan0", 50); changed {
		t.Error("expected changed=false when re-setting the exact same override value (2nd call)")
	}
	if changed := svc.SetFailoverMetricOverride("wan0", 50); changed {
		t.Error("expected changed=false on a 3rd identical re-set")
	}

	if got := svc.FailoverOverrides(); len(got) != 1 || got["wan0"] != 50 {
		t.Errorf("expected FailoverOverrides() == {wan0:50}, got %v", got)
	}
}

// TestFailoverOverride_ClearRestoresSnapshotOnceThenQuiet covers precedence
// level 3: clearing an override must restore the pre-override snapshot
// exactly once, then go quiet on subsequent reconcile passes. Uses a static
// (non-dhcp) interface so precedence level 4 can never independently fire
// and mask a restore bug.
func TestFailoverOverride_ClearRestoresSnapshotOnceThenQuiet(t *testing.T) {
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
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "if-wan2", Name: "wan2", Alias: "C", Role: "WAN", Type: "ethernet",
		AddressingMode: "static", Status: "up",
	}); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	tracker := &trackingRoutingManager{}
	tracker.SetDefaultRouteMetric("wan2", 999, true) // pre-override kernel state
	svc := NewRoutingService(repo, tracker)

	svc.SetFailoverMetricOverride("wan2", 50)
	svc.enforceInterfaceMetrics(nil)
	if got, ok := tracker.enforcedMetrics["wan2"]; !ok || got != 50 {
		t.Fatalf("expected override 50 enforced, got %d (present=%v)", got, ok)
	}

	svc.ClearFailoverMetricOverride("wan2")
	svc.enforceInterfaceMetrics(nil) // restore pass

	if len(tracker.enforceCalls) != 2 {
		t.Fatalf("expected exactly 2 EnforceDefaultRouteMetric calls total (apply override, then restore), got %d: %+v", len(tracker.enforceCalls), tracker.enforceCalls)
	}
	if last := tracker.enforceCalls[len(tracker.enforceCalls)-1]; last.iface != "wan2" || last.metric != 999 {
		t.Errorf("expected the restore call to use the snapshot metric 999, got %+v", last)
	}

	// The restore is one-time: a further reconcile pass must be quiet.
	svc.enforceInterfaceMetrics(nil)
	if len(tracker.enforceCalls) != 2 {
		t.Errorf("expected still exactly 2 EnforceDefaultRouteMetric calls after a 3rd (post-restore) pass, got %d", len(tracker.enforceCalls))
	}
}

// TestFailoverOverride_OnInterfaceNotInDB covers overrides pointing at an
// interface with no row in `interfaces` at all — a WAN uplink may be
// configured on an interface pigate hasn't learned about yet; it must still
// be enforced.
func TestFailoverOverride_OnInterfaceNotInDB(t *testing.T) {
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

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)
	svc.SetFailoverMetricOverride("wan-ghost", 50)

	svc.enforceInterfaceMetrics(nil)

	if got, ok := tracker.enforcedMetrics["wan-ghost"]; !ok || got != 50 {
		t.Errorf("expected the override to still be enforced for an interface with no DB row, got %d (present=%v)", got, ok)
	}
}

// TestFailoverOverride_RestoreFallsBackToConfiguredMetricWhenNoSnapshot
// covers the "no live default route to snapshot" edge case (QA finding,
// kill-switch-off restore silently no-ops) when the interface DOES have its
// own configured Metric: DefaultRouteMetric never reports found=true for
// this interface (no live route was ever observed, e.g. the link never came
// up), so applyFailoverOverride cannot take a snapshot — restore must then
// fall back to the interface's own configured Metric rather than leaving
// the overridden value stuck in the kernel forever.
func TestFailoverOverride_RestoreFallsBackToConfiguredMetricWhenNoSnapshot(t *testing.T) {
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
	m77 := 77
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "if-wan4", Name: "wan4", Alias: "D", Role: "WAN", Type: "ethernet",
		AddressingMode: "static", Status: "up", Metric: &m77,
	}); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	// Deliberately do NOT seed tracker.defaultRouteMetrics for wan4 — this
	// simulates DefaultRouteMetric(wan4) returning found=false, i.e. no live
	// default route was ever observed on this interface.
	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	svc.SetFailoverMetricOverride("wan4", 50)
	svc.enforceInterfaceMetrics(nil)
	if got, ok := tracker.enforcedMetrics["wan4"]; !ok || got != 50 {
		t.Fatalf("expected override 50 enforced even without a snapshot-able live route, got %d (present=%v)", got, ok)
	}

	svc.ClearFailoverMetricOverride("wan4")
	svc.enforceInterfaceMetrics(nil) // restore pass

	if len(tracker.enforceCalls) != 2 {
		t.Fatalf("expected exactly 2 EnforceDefaultRouteMetric calls total (apply override, then restore), got %d: %+v", len(tracker.enforceCalls), tracker.enforceCalls)
	}
	if last := tracker.enforceCalls[len(tracker.enforceCalls)-1]; last.iface != "wan4" || last.metric != 77 {
		t.Errorf("expected the restore call to fall back to the interface's own configured Metric (77), got %+v", last)
	}
}

// TestFailoverOverride_RestoreNoOpsWhenNeitherSnapshotNorMetricAvailable
// covers the genuinely-uncoverable edge case documented on
// restoreFailoverOverride: no live default route was ever observed (no
// snapshot) AND the interface has no configured Metric of its own either.
// There is nothing to restore TO, so the pending restore must be a
// documented no-op (log + skip) rather than inventing a value — and,
// crucially, it must not panic or otherwise misbehave, and must still
// consume the pending-restore/snapshot state exactly once.
func TestFailoverOverride_RestoreNoOpsWhenNeitherSnapshotNorMetricAvailable(t *testing.T) {
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
	if err := repo.CreateInterfaceForTest(model.NetworkInterface{
		ID: "if-wan5", Name: "wan5", Alias: "E", Role: "WAN", Type: "ethernet",
		AddressingMode: "static", Status: "up", // Metric left nil on purpose
	}); err != nil {
		t.Fatalf("Failed to seed interface: %v", err)
	}

	tracker := &trackingRoutingManager{}
	svc := NewRoutingService(repo, tracker)

	svc.SetFailoverMetricOverride("wan5", 50)
	svc.enforceInterfaceMetrics(nil)
	if got, ok := tracker.enforcedMetrics["wan5"]; !ok || got != 50 {
		t.Fatalf("expected override 50 enforced, got %d (present=%v)", got, ok)
	}

	svc.ClearFailoverMetricOverride("wan5")
	svc.enforceInterfaceMetrics(nil) // restore pass: nothing to restore to

	if len(tracker.enforceCalls) != 1 {
		t.Fatalf("expected the restore pass to be a documented no-op (only the original override call), got %d calls: %+v", len(tracker.enforceCalls), tracker.enforceCalls)
	}

	// The pending-restore/snapshot state must still be consumed exactly
	// once — a further reconcile pass must stay quiet too, not retry forever.
	svc.enforceInterfaceMetrics(nil)
	if len(tracker.enforceCalls) != 1 {
		t.Errorf("expected still exactly 1 EnforceDefaultRouteMetric call after a 3rd (post-restore-attempt) pass, got %d", len(tracker.enforceCalls))
	}
}
