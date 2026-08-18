package db

import (
	"reflect"
	"testing"

	"pigate/internal/model"
)

// TestCreatePolicy_MultiInterfaces_RoundTrip covers docs/ref/todo/
// multi-interface-firewall-rule-plan.md T-04 acceptance: creating a rule
// with several in-interfaces round-trips through GetPolicyByID/GetPolicies
// with order preserved, and updating it down to a single interface removes
// the extra child rows.
func TestCreatePolicy_MultiInterfaces_RoundTrip(t *testing.T) {
	sqlDB, _ := InitDB(":memory:")
	defer sqlDB.Close()
	repo := NewRepository(sqlDB)

	rule := model.PolicyRule{
		ID:            "rule-multi-if",
		Name:          "Multi If Rule",
		Chain:         model.PolicyChainForward,
		InInterfaces:  []string{"eth0", "wlan0", "eth1"},
		OutInterfaces: []string{"eth2"},
		Source:        []string{"ALL"},
		Destination:   []string{"ALL"},
		Service:       []string{"ALL"},
		Action:        "ACCEPT",
		Status:        true,
	}
	if err := repo.CreatePolicy(rule); err != nil {
		t.Fatalf("CreatePolicy failed: %v", err)
	}

	fetched, err := repo.GetPolicyByID("rule-multi-if")
	if err != nil || fetched == nil {
		t.Fatalf("GetPolicyByID failed: %v", err)
	}
	if !reflect.DeepEqual(fetched.InInterfaces, []string{"eth0", "wlan0", "eth1"}) {
		t.Errorf("InInterfaces = %v, want [eth0 wlan0 eth1] (order preserved)", fetched.InInterfaces)
	}
	if fetched.InInterface != "eth0" {
		t.Errorf("InInterface (legacy mirror) = %q, want eth0", fetched.InInterface)
	}
	if !reflect.DeepEqual(fetched.OutInterfaces, []string{"eth2"}) {
		t.Errorf("OutInterfaces = %v, want [eth2]", fetched.OutInterfaces)
	}

	// GetPolicies (bulk load path) must agree.
	all, err := repo.GetPolicies()
	if err != nil {
		t.Fatalf("GetPolicies failed: %v", err)
	}
	var bulk *model.PolicyRule
	for i := range all {
		if all[i].ID == "rule-multi-if" {
			bulk = &all[i]
		}
	}
	if bulk == nil {
		t.Fatalf("rule-multi-if not found in GetPolicies result")
	}
	if !reflect.DeepEqual(bulk.InInterfaces, []string{"eth0", "wlan0", "eth1"}) {
		t.Errorf("GetPolicies: InInterfaces = %v, want [eth0 wlan0 eth1]", bulk.InInterfaces)
	}

	// Update down to a single in-interface: the extra child rows must be
	// deleted, not just ignored.
	fetched.InInterfaces = []string{"eth0"}
	if err := repo.UpdatePolicy(*fetched); err != nil {
		t.Fatalf("UpdatePolicy failed: %v", err)
	}
	afterUpdate, err := repo.GetPolicyByID("rule-multi-if")
	if err != nil || afterUpdate == nil {
		t.Fatalf("GetPolicyByID after update failed: %v", err)
	}
	if !reflect.DeepEqual(afterUpdate.InInterfaces, []string{"eth0"}) {
		t.Errorf("after update InInterfaces = %v, want [eth0]", afterUpdate.InInterfaces)
	}

	var rowCount int
	if err := sqlDB.QueryRow(
		"SELECT COUNT(*) FROM policy_interfaces WHERE policy_id = ? AND direction = 'in'", "rule-multi-if",
	).Scan(&rowCount); err != nil {
		t.Fatalf("count policy_interfaces rows: %v", err)
	}
	if rowCount != 1 {
		t.Errorf("expected exactly 1 policy_interfaces row (direction=in) after shrinking, got %d", rowCount)
	}
}

// TestCreatePolicy_InterfaceCapEnforced covers T-04 acceptance: exceeding
// the configured per-direction cap is rejected, and nothing is written to
// the DB.
func TestCreatePolicy_InterfaceCapEnforced(t *testing.T) {
	sqlDB, _ := InitDB(":memory:")
	defer sqlDB.Close()
	repo := NewRepository(sqlDB)
	repo.SetPolicyInterfaceLimit(8)

	nineIfaces := make([]string, 9)
	for i := range nineIfaces {
		nineIfaces[i] = "eth" + string(rune('0'+i))
	}

	rule := model.PolicyRule{
		ID:           "rule-over-cap",
		Name:         "Over Cap Rule",
		Chain:        model.PolicyChainForward,
		InInterfaces: nineIfaces,
		Source:       []string{"ALL"},
		Destination:  []string{"ALL"},
		Service:      []string{"ALL"},
		Action:       "ACCEPT",
		Status:       true,
	}
	if err := repo.CreatePolicy(rule); err == nil {
		t.Fatalf("expected CreatePolicy to reject 9 interfaces against a cap of 8")
	}

	fetched, err := repo.GetPolicyByID("rule-over-cap")
	if err != nil {
		t.Fatalf("GetPolicyByID failed: %v", err)
	}
	if fetched != nil {
		t.Errorf("expected no policy to be persisted after a rejected create, got %+v", fetched)
	}
}

// TestGetPolicies_BackfilledLegacySingleInterfaceUnchanged covers T-04
// acceptance: a policy created before this feature (only the legacy scalar
// columns populated, no policy_interfaces rows) must read back with exactly
// the same single-interface value it always had, once backfilled.
func TestGetPolicies_BackfilledLegacySingleInterfaceUnchanged(t *testing.T) {
	sqlDB, _ := InitDB(":memory:")
	defer sqlDB.Close()

	if _, err := sqlDB.Exec(`INSERT INTO firewall_policies
		(id, name, chain, in_interface, out_interface, action, log, nat, status, priority) VALUES
		('p-legacy', 'legacy', 'forward', 'eth0', 'ALL', 'ACCEPT', 0, 0, 1, 1)`); err != nil {
		t.Fatalf("failed to seed legacy policy row: %v", err)
	}
	// Run the migration backfill again on this now-open connection.
	if err := migrate(sqlDB); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	repo := NewRepository(sqlDB)
	fetched, err := repo.GetPolicyByID("p-legacy")
	if err != nil || fetched == nil {
		t.Fatalf("GetPolicyByID failed: %v", err)
	}
	if !reflect.DeepEqual(fetched.InInterfaces, []string{"eth0"}) {
		t.Errorf("InInterfaces = %v, want [eth0]", fetched.InInterfaces)
	}
	if !reflect.DeepEqual(fetched.OutInterfaces, []string{"ALL"}) {
		t.Errorf("OutInterfaces = %v, want [ALL]", fetched.OutInterfaces)
	}
	if fetched.InInterface != "eth0" || fetched.OutInterface != "ALL" {
		t.Errorf("legacy scalar mirrors = (%q, %q), want (eth0, ALL)", fetched.InInterface, fetched.OutInterface)
	}
}
