package db

import (
	"database/sql"
	"testing"
)

// TestMigrationBackfillsPolicyNat simulates upgrading a box whose
// firewall_policies table predates policy-based source NAT (no `nat` column).
// NAT used to be automatic on Role=WAN interfaces, so to preserve behaviour the
// migration must backfill nat=1 on every ACCEPT policy that egresses a WAN
// interface — or leaves the egress unrestricted ('' / 'ALL') — while leaving
// DROP policies and LAN-only ACCEPT policies at nat=0. The backfill must also be
// one-shot so an admin who later turns nat off is not overridden on the next boot.
func TestMigrationBackfillsPolicyNat(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Build the full current schema first (this also adds the nat column).
	if err := migrate(rawDB); err != nil {
		t.Fatalf("initial migrate failed: %v", err)
	}

	// Seed interfaces so the WAN-role subquery in the backfill resolves.
	seedIface := func(id, name, role string) {
		t.Helper()
		if _, err := rawDB.Exec(`INSERT INTO network_interfaces
			(id, name, alias, role, type, addressing_mode, ip, netmask, gateway, mac_address, admin_access, status, speed)
			VALUES (?, ?, ?, ?, 'ethernet', 'dhcp', '0.0.0.0', '24', '0.0.0.0', '00:00:00:00:00:00', 'PING', 'up', '1000 Mbps')`,
			id, name, name, role); err != nil {
			t.Fatalf("failed to seed interface %s: %v", name, err)
		}
	}
	seedIface("if-wan", "eth0", "WAN")
	seedIface("if-lan", "eth1", "LAN")

	// Seed representative policies.
	if _, err := rawDB.Exec(`INSERT INTO firewall_policies
		(id, name, in_interface, out_interface, action, log, nat, status, priority) VALUES
		('p-wan',  'lan-to-wan',  'eth1', 'eth0', 'ACCEPT', 0, 0, 1, 1),
		('p-all',  'out-all',     'eth1', 'ALL',  'ACCEPT', 0, 0, 1, 2),
		('p-empty','out-empty',   'eth1', '',     'ACCEPT', 0, 0, 1, 3),
		('p-lan',  'lan-to-lan',  'eth1', 'eth1', 'ACCEPT', 0, 0, 1, 4),
		('p-drop', 'drop-wan',    'eth1', 'eth0', 'DROP',   0, 0, 1, 5)`); err != nil {
		t.Fatalf("failed to seed policies: %v", err)
	}

	// Simulate a legacy DB: drop the nat column so the next migrate re-adds it
	// and runs the one-shot backfill.
	if _, err := rawDB.Exec("ALTER TABLE firewall_policies DROP COLUMN nat"); err != nil {
		t.Fatalf("failed to drop nat column to simulate legacy schema: %v", err)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("upgrade migrate failed: %v", err)
	}

	want := map[string]int{
		"p-wan":   1, // ACCEPT egressing WAN → NAT on
		"p-all":   1, // ACCEPT out='ALL' → NAT on (preserve old blanket masquerade)
		"p-empty": 1, // ACCEPT out='' → NAT on
		"p-lan":   0, // ACCEPT LAN→LAN → NAT off
		"p-drop":  0, // DROP → NAT off regardless of egress
	}
	for id, exp := range want {
		var nat int
		if err := rawDB.QueryRow("SELECT nat FROM firewall_policies WHERE id = ?", id).Scan(&nat); err != nil {
			t.Fatalf("failed to read nat for %s: %v", id, err)
		}
		if nat != exp {
			t.Errorf("policy %s: nat = %d, want %d", id, nat, exp)
		}
	}

	// One-shot guard: an admin who deliberately turns a backfilled policy's nat
	// back off must not have it flipped on again by a later boot.
	if _, err := rawDB.Exec("UPDATE firewall_policies SET nat = 0 WHERE id = 'p-wan'"); err != nil {
		t.Fatalf("failed to clear nat: %v", err)
	}
	if err := migrate(rawDB); err != nil {
		t.Fatalf("second migrate failed (not idempotent): %v", err)
	}
	var natAfter int
	if err := rawDB.QueryRow("SELECT nat FROM firewall_policies WHERE id = 'p-wan'").Scan(&natAfter); err != nil {
		t.Fatalf("failed to re-read nat: %v", err)
	}
	if natAfter != 0 {
		t.Errorf("p-wan nat was re-flipped to %d on second migrate; backfill must be one-shot", natAfter)
	}
}
