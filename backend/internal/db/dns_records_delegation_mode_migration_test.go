package db

import (
	"database/sql"
	"testing"

	"pigate/internal/model"
)

// TestMigrationAddsDelegationModeToLegacyDNSRecords simulates upgrading a box
// whose dns_records table already has both the 'NS' CHECK constraint and the
// glue_ips column (post PR 163) but predates delegation_mode (docs/ref/todo/
// dns-ns-delegation-cname-fix-plan.md T-03/T-11): only the pre-delegation-mode
// columns exist. After migrate(), the new column must exist and every
// pre-existing row must read back DelegationMode == "".
func TestMigrationAddsDelegationModeToLegacyDNSRecords(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Legacy schema: has 'NS' in the CHECK constraint and glue_ips, but no
	// delegation_mode column.
	if _, err := rawDB.Exec(`CREATE TABLE dns_records (
		id         TEXT PRIMARY KEY,
		zone_id    TEXT NOT NULL,
		name       TEXT NOT NULL,
		type       TEXT NOT NULL CHECK(type IN ('A','AAAA','CNAME','MX','TXT','PTR','NS')),
		value      TEXT NOT NULL,
		ttl        INTEGER DEFAULT 300,
		glue_ips   TEXT NOT NULL DEFAULT '',
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`); err != nil {
		t.Fatalf("failed to create legacy dns_records table: %v", err)
	}
	if _, err := rawDB.Exec(
		"INSERT INTO dns_records (id, zone_id, name, type, value, ttl, glue_ips) VALUES (?, ?, ?, ?, ?, ?, ?)",
		"rec-legacy-1", "zone-1", "sub", "NS", "ns1.example.local", 300, "203.0.113.53",
	); err != nil {
		t.Fatalf("failed to seed legacy row: %v", err)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	repo := NewRepository(rawDB)
	records, err := repo.GetDNSRecordsByZone("zone-1")
	if err != nil {
		t.Fatalf("GetDNSRecordsByZone failed after migration: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record preserved, got %d", len(records))
	}
	rec := records[0]
	if rec.ID != "rec-legacy-1" || rec.Name != "sub" || rec.Value != "ns1.example.local" {
		t.Errorf("expected legacy row preserved intact, got %+v", rec)
	}
	if len(rec.GlueIPs) != 1 || rec.GlueIPs[0] != "203.0.113.53" {
		t.Errorf("expected pre-existing GlueIPs preserved, got %v", rec.GlueIPs)
	}
	if rec.DelegationMode != "" {
		t.Errorf("expected DelegationMode to be \"\" for a legacy row, got %q", rec.DelegationMode)
	}

	// Idempotent: running migrate again must not fail or duplicate the column.
	if err := migrate(rawDB); err != nil {
		t.Fatalf("second migrate failed (not idempotent): %v", err)
	}
}

// TestMigrationFromPreGlueDNSRecords simulates upgrading a box old enough
// that dns_records predates BOTH glue_ips and delegation_mode (post PR
// dns-ns-record-support but pre dns-ns-delegation-plan) — migrate() must run
// the glue_ips ALTER, then the delegation_mode ALTER, in that order, ending
// with a table that accepts an NS record with glue AND delegation_mode set.
func TestMigrationFromPreGlueDNSRecords(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Schema predating BOTH glue_ips and delegation_mode, and ALSO predating
	// the 'NS' CHECK constraint, to exercise the full 3-step migration chain
	// ('NS' rebuild -> glue_ips ALTER -> delegation_mode ALTER) in order.
	if _, err := rawDB.Exec(`CREATE TABLE dns_records (
		id         TEXT PRIMARY KEY,
		zone_id    TEXT NOT NULL,
		name       TEXT NOT NULL,
		type       TEXT NOT NULL CHECK(type IN ('A','AAAA','CNAME','MX','TXT','PTR')),
		value      TEXT NOT NULL,
		ttl        INTEGER DEFAULT 300,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`); err != nil {
		t.Fatalf("failed to create pre-glue dns_records table: %v", err)
	}
	if _, err := rawDB.Exec(
		"INSERT INTO dns_records (id, zone_id, name, type, value, ttl) VALUES (?, ?, ?, ?, ?, ?)",
		"rec-old-1", "zone-1", "www", "A", "192.168.1.10", 300,
	); err != nil {
		t.Fatalf("failed to seed pre-glue row: %v", err)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	repo := NewRepository(rawDB)
	records, err := repo.GetDNSRecordsByZone("zone-1")
	if err != nil {
		t.Fatalf("GetDNSRecordsByZone failed after migration: %v", err)
	}
	if len(records) != 1 {
		t.Fatalf("expected 1 record preserved through the rebuild, got %d", len(records))
	}
	if len(records[0].GlueIPs) != 0 {
		t.Errorf("expected GlueIPs empty for pre-glue row, got %v", records[0].GlueIPs)
	}
	if records[0].DelegationMode != "" {
		t.Errorf("expected DelegationMode empty for pre-glue row, got %q", records[0].DelegationMode)
	}

	// The 'NS' rebuild's migrationQueries end with "PRAGMA foreign_keys=ON;",
	// so from this point on the connection enforces the dns_records.zone_id
	// foreign key — seed the referenced zone row before inserting a new
	// dns_records row against it.
	if _, err := rawDB.Exec(
		"INSERT INTO dns_zones (id, zone_name, is_authoritative, enabled) VALUES (?, ?, 1, 1)",
		"zone-1", "example.local",
	); err != nil {
		t.Fatalf("failed to seed dns_zones row: %v", err)
	}

	// The fully-migrated table must now accept an NS record with glue AND
	// delegation_mode = "upstream".
	nsRec := model.DNSRecord{
		ID:             "rec-new-ns",
		ZoneID:         "zone-1",
		Name:           "sub",
		Type:           "NS",
		Value:          "ns1.example.local",
		TTL:            300,
		GlueIPs:        []string{"203.0.113.53"},
		DelegationMode: "upstream",
	}
	if err := repo.CreateDNSRecord(nsRec); err != nil {
		t.Fatalf("CreateDNSRecord (NS with glue + delegation_mode) failed after migration: %v", err)
	}
	got, err := repo.GetDNSRecordByID("rec-new-ns")
	if err != nil || got == nil {
		t.Fatalf("GetDNSRecordByID failed: %v", err)
	}
	if len(got.GlueIPs) != 1 || got.GlueIPs[0] != "203.0.113.53" {
		t.Errorf("expected GlueIPs [203.0.113.53], got %v", got.GlueIPs)
	}
	if got.DelegationMode != "upstream" {
		t.Errorf("expected DelegationMode %q, got %q", "upstream", got.DelegationMode)
	}
}

// TestDNSRecordDelegationModeRoundTrip covers Create/Get/Update/Get on a
// fresh DB (InitDB, which seeds the current schema directly — no migration
// path involved): DelegationMode must round-trip exactly, including clearing
// it back to "" via Update.
func TestDNSRecordDelegationModeRoundTrip(t *testing.T) {
	rawDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer rawDB.Close()

	repo := NewRepository(rawDB)

	zone := model.DNSZone{
		ID:              "zone-delegation-mode-test",
		ZoneName:        "example.local",
		IsAuthoritative: true,
		Enabled:         true,
	}
	if err := repo.CreateDNSZone(zone); err != nil {
		t.Fatalf("CreateDNSZone failed: %v", err)
	}

	rec := model.DNSRecord{
		ID:             "rec-delegation-mode-1",
		ZoneID:         zone.ID,
		Name:           "sub",
		Type:           "NS",
		Value:          "ns1.example.local",
		TTL:            300,
		DelegationMode: "upstream",
	}
	if err := repo.CreateDNSRecord(rec); err != nil {
		t.Fatalf("CreateDNSRecord failed: %v", err)
	}

	got, err := repo.GetDNSRecordByID(rec.ID)
	if err != nil || got == nil {
		t.Fatalf("GetDNSRecordByID failed: %v", err)
	}
	if got.DelegationMode != "upstream" {
		t.Errorf("expected DelegationMode round-tripped as %q, got %q", "upstream", got.DelegationMode)
	}

	// Update back to "": must read back as "".
	got.DelegationMode = ""
	if err := repo.UpdateDNSRecord(*got); err != nil {
		t.Fatalf("UpdateDNSRecord failed: %v", err)
	}
	updated, err := repo.GetDNSRecordByID(rec.ID)
	if err != nil || updated == nil {
		t.Fatalf("GetDNSRecordByID after update failed: %v", err)
	}
	if updated.DelegationMode != "" {
		t.Errorf("expected DelegationMode \"\" after clearing, got %q", updated.DelegationMode)
	}
}
