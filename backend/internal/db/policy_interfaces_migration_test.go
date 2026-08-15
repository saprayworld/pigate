package db

import (
	"database/sql"
	"testing"
)

// TestPolicyInterfaces_BackfillFromLegacyDBIsIdempotent simulates upgrading a
// pre-multi-interface database: firewall_policies rows exist (with only the
// legacy in_interface/out_interface scalar columns populated) but their
// policy_interfaces child rows are missing. Rebooting (InitDB) must backfill
// exactly one seq=1 child row per (policy, direction), and running it again
// must not duplicate rows (docs/ref/todo/
// multi-interface-firewall-rule-plan.md §2.3/T-03).
func TestPolicyInterfaces_BackfillFromLegacyDBIsIdempotent(t *testing.T) {
	dsn := t.TempDir() + "/legacy-policy-interfaces.db"

	first, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("initial InitDB failed: %v", err)
	}

	// Simulate a pre-migration database: seed policies directly (bypassing
	// the repository, which would also write policy_interfaces), and
	// clear out any rows that already exist so the DB genuinely represents
	// "policies exist, no policy_interfaces child rows yet".
	if _, err := first.Exec("DELETE FROM policy_interfaces"); err != nil {
		t.Fatalf("failed to clear policy_interfaces: %v", err)
	}
	if _, err := first.Exec(`INSERT INTO firewall_policies
		(id, name, chain, in_interface, out_interface, action, log, nat, status, priority) VALUES
		('p-legacy-1', 'legacy-1', 'forward', 'eth1', 'eth0', 'ACCEPT', 0, 0, 1, 1),
		('p-legacy-2', 'legacy-2', 'forward', '',     'ALL',  'DROP',   0, 0, 1, 2)`); err != nil {
		t.Fatalf("failed to seed legacy policies: %v", err)
	}

	var policyCount int
	if err := first.QueryRow("SELECT COUNT(*) FROM firewall_policies").Scan(&policyCount); err != nil {
		t.Fatalf("count firewall_policies: %v", err)
	}
	first.Close()

	// Reboot: migration/backfill must run and populate exactly one seq=1 row
	// per (policy, direction) — i.e. 2 rows per policy.
	second, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("InitDB on legacy data (no policy_interfaces rows) failed: %v", err)
	}

	countRows := func(db *sql.DB, table string) int {
		var n int
		if err := db.QueryRow("SELECT COUNT(*) FROM " + table).Scan(&n); err != nil {
			t.Fatalf("count %s: %v", table, err)
		}
		return n
	}

	wantRows := policyCount * 2
	if got := countRows(second, "policy_interfaces"); got != wantRows {
		t.Fatalf("expected %d backfilled policy_interfaces rows (2 per policy), got %d", wantRows, got)
	}

	var inName, outName string
	if err := second.QueryRow("SELECT name FROM policy_interfaces WHERE policy_id = ? AND direction = 'in'", "p-legacy-1").Scan(&inName); err != nil {
		t.Fatalf("query backfilled in-interface for p-legacy-1: %v", err)
	}
	if inName != "eth1" {
		t.Errorf("p-legacy-1 in-interface = %q, want %q", inName, "eth1")
	}
	if err := second.QueryRow("SELECT name FROM policy_interfaces WHERE policy_id = ? AND direction = 'out'", "p-legacy-1").Scan(&outName); err != nil {
		t.Fatalf("query backfilled out-interface for p-legacy-1: %v", err)
	}
	if outName != "eth0" {
		t.Errorf("p-legacy-1 out-interface = %q, want %q", outName, "eth0")
	}

	// Empty legacy scalar must be backfilled as "ALL", not an empty string.
	var emptyInName string
	if err := second.QueryRow("SELECT name FROM policy_interfaces WHERE policy_id = ? AND direction = 'in'", "p-legacy-2").Scan(&emptyInName); err != nil {
		t.Fatalf("query backfilled in-interface for p-legacy-2: %v", err)
	}
	if emptyInName != "ALL" {
		t.Errorf("p-legacy-2 in-interface = %q, want %q (empty legacy scalar backfilled as ALL)", emptyInName, "ALL")
	}
	second.Close()

	// Reboot again (no further mutation in between): row counts must stay
	// exactly the same — the WHERE NOT EXISTS guard must not duplicate rows
	// on a DB that already has its children backfilled.
	third, err := InitDB(dsn)
	if err != nil {
		t.Fatalf("InitDB (second reboot) failed: %v", err)
	}
	defer third.Close()

	if got := countRows(third, "policy_interfaces"); got != wantRows {
		t.Fatalf("backfill not idempotent: expected %d policy_interfaces rows after a second reboot, got %d", wantRows, got)
	}
}
