package db

import (
	"database/sql"
	"testing"
)

// TestMigrationBackfillsPolicyChain simulates upgrading a box whose
// firewall_policies table predates the input/output chain feature (no `chain`
// column). Every existing row must come out as "forward" after migrate, which
// is exactly backward-compatible with pre-feature behaviour (every policy was
// implicitly a forward-chain rule) — see
// docs/ref/todo/input-output-chain-firewall-plan.md T-15 / Caution 12.
func TestMigrationBackfillsPolicyChain(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Build the full current schema first (this also adds the chain column).
	if err := migrate(rawDB); err != nil {
		t.Fatalf("initial migrate failed: %v", err)
	}

	if _, err := rawDB.Exec(`INSERT INTO firewall_policies
		(id, name, in_interface, out_interface, action, log, nat, status, priority) VALUES
		('p-1', 'legacy-1', 'eth1', 'eth0', 'ACCEPT', 0, 0, 1, 1),
		('p-2', 'legacy-2', 'eth1', 'ALL',  'DROP',   0, 0, 1, 2)`); err != nil {
		t.Fatalf("failed to seed legacy policies: %v", err)
	}

	// Simulate a legacy DB: drop the chain column so the next migrate re-adds
	// it (via the DEFAULT 'forward') and exercises the migration guard.
	if _, err := rawDB.Exec("ALTER TABLE firewall_policies DROP COLUMN chain"); err != nil {
		t.Fatalf("failed to drop chain column to simulate legacy schema: %v", err)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("upgrade migrate failed: %v", err)
	}

	rows, err := rawDB.Query("SELECT id, chain FROM firewall_policies ORDER BY id")
	if err != nil {
		t.Fatalf("failed to query chain column after migrate: %v", err)
	}
	defer rows.Close()

	got := map[string]string{}
	for rows.Next() {
		var id, chain string
		if err := rows.Scan(&id, &chain); err != nil {
			t.Fatalf("scan failed: %v", err)
		}
		got[id] = chain
	}
	for _, id := range []string{"p-1", "p-2"} {
		if got[id] != "forward" {
			t.Errorf("policy %s: chain = %q, want \"forward\"", id, got[id])
		}
	}

	// A fresh DB (never had the legacy schema) also has the column, with the
	// default applying to any row inserted without specifying chain.
	if _, err := rawDB.Exec(`INSERT INTO firewall_policies
		(id, name, in_interface, out_interface, action, log, nat, status, priority) VALUES
		('p-3', 'no-chain-specified', 'eth1', 'eth0', 'ACCEPT', 0, 0, 1, 3)`); err != nil {
		t.Fatalf("failed to insert without chain: %v", err)
	}
	var chain3 string
	if err := rawDB.QueryRow("SELECT chain FROM firewall_policies WHERE id = 'p-3'").Scan(&chain3); err != nil {
		t.Fatalf("failed to read chain for p-3: %v", err)
	}
	if chain3 != "forward" {
		t.Errorf("p-3: chain = %q, want \"forward\" (column DEFAULT)", chain3)
	}

	// CHECK constraint rejects an unknown chain value.
	if _, err := rawDB.Exec(`INSERT INTO firewall_policies
		(id, name, chain, in_interface, out_interface, action, log, nat, status, priority) VALUES
		('p-bad', 'bad-chain', 'bogus', 'eth1', 'eth0', 'ACCEPT', 0, 0, 1, 4)`); err == nil {
		t.Errorf("expected CHECK constraint to reject chain='bogus', got no error")
	}
}
