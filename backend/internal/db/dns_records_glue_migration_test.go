package db

import (
	"database/sql"
	"testing"

	"pigate/internal/model"
)

// TestMigrationAddsGlueIPsToLegacyDNSRecords simulates upgrading a box whose
// dns_records table already has the 'NS' CHECK constraint (post PR 162) but
// predates the glue_ips column (docs/ref/todo/dns-ns-delegation-plan.md
// T-03/T-12): only the pre-glue columns exist. After migrate(), the new
// column must exist and every pre-existing row must read back
// GlueIPs == []string{} (not nil, not [""]).
func TestMigrationAddsGlueIPsToLegacyDNSRecords(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Legacy schema: has 'NS' in the CHECK constraint, but no glue_ips column.
	if _, err := rawDB.Exec(`CREATE TABLE dns_records (
		id         TEXT PRIMARY KEY,
		zone_id    TEXT NOT NULL,
		name       TEXT NOT NULL,
		type       TEXT NOT NULL CHECK(type IN ('A','AAAA','CNAME','MX','TXT','PTR','NS')),
		value      TEXT NOT NULL,
		ttl        INTEGER DEFAULT 300,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`); err != nil {
		t.Fatalf("failed to create legacy dns_records table: %v", err)
	}
	if _, err := rawDB.Exec(
		"INSERT INTO dns_records (id, zone_id, name, type, value, ttl) VALUES (?, ?, ?, ?, ?, ?)",
		"rec-legacy-1", "zone-1", "www", "A", "192.168.1.10", 300,
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
	if records[0].ID != "rec-legacy-1" || records[0].Name != "www" || records[0].Value != "192.168.1.10" {
		t.Errorf("expected legacy row preserved intact, got %+v", records[0])
	}
	if records[0].GlueIPs == nil {
		t.Errorf("expected GlueIPs to be an empty slice, got nil")
	}
	if len(records[0].GlueIPs) != 0 {
		t.Errorf("expected GlueIPs empty for legacy row, got %v", records[0].GlueIPs)
	}

	// Idempotent: running migrate again must not fail or duplicate the column.
	if err := migrate(rawDB); err != nil {
		t.Fatalf("second migrate failed (not idempotent): %v", err)
	}
}

// TestMigrationFromPreNSDNSRecords simulates upgrading a box old enough that
// dns_records predates BOTH the 'NS' CHECK constraint (docs/ref/todo/
// dns-ns-record-support-plan.md) and glue_ips (this plan) — migrate() must
// run the 'NS' rebuild first, then the glue_ips ALTER on the rebuilt table,
// in that order, ending with a table that accepts an NS record with glue.
func TestMigrationFromPreNSDNSRecords(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Pre-NS schema: CHECK constraint does not include 'NS' yet.
	if _, err := rawDB.Exec(`CREATE TABLE dns_records (
		id         TEXT PRIMARY KEY,
		zone_id    TEXT NOT NULL,
		name       TEXT NOT NULL,
		type       TEXT NOT NULL CHECK(type IN ('A','AAAA','CNAME','MX','TXT','PTR')),
		value      TEXT NOT NULL,
		ttl        INTEGER DEFAULT 300,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`); err != nil {
		t.Fatalf("failed to create pre-NS dns_records table: %v", err)
	}
	if _, err := rawDB.Exec(
		"INSERT INTO dns_records (id, zone_id, name, type, value, ttl) VALUES (?, ?, ?, ?, ?, ?)",
		"rec-old-1", "zone-1", "www", "A", "192.168.1.10", 300,
	); err != nil {
		t.Fatalf("failed to seed pre-NS row: %v", err)
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
		t.Errorf("expected GlueIPs empty for pre-NS row, got %v", records[0].GlueIPs)
	}

	// The 'NS' rebuild's migrationQueries end with "PRAGMA foreign_keys=ON;",
	// so from this point on the connection enforces the dns_records.zone_id
	// foreign key — migrate()'s own `CREATE TABLE IF NOT EXISTS dns_zones`
	// created that table (empty) since this minimal test schema never had
	// one; seed the referenced zone row before inserting a new dns_records
	// row against it.
	if _, err := rawDB.Exec(
		"INSERT INTO dns_zones (id, zone_name, is_authoritative, enabled) VALUES (?, ?, 1, 1)",
		"zone-1", "example.local",
	); err != nil {
		t.Fatalf("failed to seed dns_zones row: %v", err)
	}

	// The rebuilt+altered table must now accept an NS record with glue.
	nsRec := model.DNSRecord{
		ID:      "rec-new-ns",
		ZoneID:  "zone-1",
		Name:    "sub",
		Type:    "NS",
		Value:   "ns1.example.local",
		TTL:     300,
		GlueIPs: []string{"203.0.113.53"},
	}
	if err := repo.CreateDNSRecord(nsRec); err != nil {
		t.Fatalf("CreateDNSRecord (NS with glue) failed after migration: %v", err)
	}
	got, err := repo.GetDNSRecordByID("rec-new-ns")
	if err != nil || got == nil {
		t.Fatalf("GetDNSRecordByID failed: %v", err)
	}
	if len(got.GlueIPs) != 1 || got.GlueIPs[0] != "203.0.113.53" {
		t.Errorf("expected GlueIPs [203.0.113.53], got %v", got.GlueIPs)
	}
}

// TestDNSRecordGlueRoundTrip covers Create/Get/Update/Get on a fresh DB
// (InitDB, which seeds the current schema directly — no migration path
// involved): GlueIPs must round-trip exactly, and clearing it via Update
// must read back as []string{}, never nil or [""].
func TestDNSRecordGlueRoundTrip(t *testing.T) {
	rawDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer rawDB.Close()

	repo := NewRepository(rawDB)

	zone := model.DNSZone{
		ID:              "zone-glue-test",
		ZoneName:        "example.local",
		IsAuthoritative: true,
		Enabled:         true,
	}
	if err := repo.CreateDNSZone(zone); err != nil {
		t.Fatalf("CreateDNSZone failed: %v", err)
	}

	rec := model.DNSRecord{
		ID:      "rec-glue-1",
		ZoneID:  zone.ID,
		Name:    "sub",
		Type:    "NS",
		Value:   "ns1.example.local",
		TTL:     300,
		GlueIPs: []string{"203.0.113.53", "2001:db8::53"},
	}
	if err := repo.CreateDNSRecord(rec); err != nil {
		t.Fatalf("CreateDNSRecord failed: %v", err)
	}

	got, err := repo.GetDNSRecordByID(rec.ID)
	if err != nil || got == nil {
		t.Fatalf("GetDNSRecordByID failed: %v", err)
	}
	if len(got.GlueIPs) != 2 || got.GlueIPs[0] != "203.0.113.53" || got.GlueIPs[1] != "2001:db8::53" {
		t.Errorf("expected GlueIPs round-tripped as [203.0.113.53 2001:db8::53], got %v", got.GlueIPs)
	}

	// Update to clear glue: must read back as an empty slice, not nil/[""].
	got.GlueIPs = []string{}
	if err := repo.UpdateDNSRecord(*got); err != nil {
		t.Fatalf("UpdateDNSRecord failed: %v", err)
	}
	updated, err := repo.GetDNSRecordByID(rec.ID)
	if err != nil || updated == nil {
		t.Fatalf("GetDNSRecordByID after update failed: %v", err)
	}
	if updated.GlueIPs == nil {
		t.Errorf("expected GlueIPs to be an empty slice after clearing, got nil")
	}
	if len(updated.GlueIPs) != 0 {
		t.Errorf("expected GlueIPs empty after clearing, got %v", updated.GlueIPs)
	}
}
