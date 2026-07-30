package db

import (
	"database/sql"
	"testing"

	"pigate/internal/model"
)

// TestMigrationAddsDNSStatsColumnsToLegacyDNSServerSettings simulates
// upgrading a box whose dns_server_settings table predates the DNS
// Statistics feature (docs/ref/todo/statistics-dns-top-domain-plan.md T-02/
// T-11 item 13): only the `interfaces` column exists. After migrate(), the
// new columns must exist and read back the package defaults (0/60/4096), not
// zero-valued garbage.
func TestMigrationAddsDNSStatsColumnsToLegacyDNSServerSettings(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Legacy schema: only `interfaces`, matching the pre-T-02 CREATE TABLE.
	if _, err := rawDB.Exec(`CREATE TABLE dns_server_settings (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		interfaces TEXT NOT NULL DEFAULT ''
	);`); err != nil {
		t.Fatalf("failed to create legacy dns_server_settings table: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO dns_server_settings (id, interfaces) VALUES (1, 'eth0')`); err != nil {
		t.Fatalf("failed to seed legacy row: %v", err)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	repo := NewRepository(rawDB)
	settings, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings failed after migration: %v", err)
	}
	if len(settings.Interfaces) != 1 || settings.Interfaces[0] != "eth0" {
		t.Errorf("expected interfaces preserved as [eth0], got %v", settings.Interfaces)
	}
	if settings.QueryLogging {
		t.Errorf("expected query_logging default false, got true")
	}
	if settings.DNSCacheTTLMinutes != model.DNSCacheTTLDefault {
		t.Errorf("DNSCacheTTLMinutes = %d, want default %d", settings.DNSCacheTTLMinutes, model.DNSCacheTTLDefault)
	}
	if settings.DNSCacheMaxEntries != model.DNSCacheEntriesDefault {
		t.Errorf("DNSCacheMaxEntries = %d, want default %d", settings.DNSCacheMaxEntries, model.DNSCacheEntriesDefault)
	}

	// Idempotent: running migrate again must not fail or duplicate columns.
	if err := migrate(rawDB); err != nil {
		t.Fatalf("second migrate failed (not idempotent): %v", err)
	}
}

// TestGetDNSServerSettings_FreshDB covers a brand-new install: the seeded row
// must already read back the package defaults (docs/ref/todo/
// statistics-dns-top-domain-plan.md Final Acceptance: "แสดงค่าเริ่มต้น 60
// นาที / 4096 entry ตั้งแต่ครั้งแรก").
func TestGetDNSServerSettings_FreshDB(t *testing.T) {
	rawDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer rawDB.Close()

	repo := NewRepository(rawDB)
	settings, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings failed: %v", err)
	}
	if settings.QueryLogging {
		t.Errorf("expected query_logging default false on fresh install")
	}
	if settings.DNSCacheTTLMinutes != model.DNSCacheTTLDefault {
		t.Errorf("DNSCacheTTLMinutes = %d, want %d", settings.DNSCacheTTLMinutes, model.DNSCacheTTLDefault)
	}
	if settings.DNSCacheMaxEntries != model.DNSCacheEntriesDefault {
		t.Errorf("DNSCacheMaxEntries = %d, want %d", settings.DNSCacheMaxEntries, model.DNSCacheEntriesDefault)
	}
}

// TestGetDNSServerSettings_ClampsOutOfRangeStoredValue covers plan §5 item
// 17: a value that ended up out of range in the DB (hand-edited, or a
// corrupted restore) must be clamped back to the default on read, never
// returned as-is.
func TestGetDNSServerSettings_ClampsOutOfRangeStoredValue(t *testing.T) {
	rawDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer rawDB.Close()

	if _, err := rawDB.Exec("UPDATE dns_server_settings SET dns_cache_ttl_minutes = 0, dns_cache_max_entries = 999999 WHERE id = 1"); err != nil {
		t.Fatalf("failed to seed out-of-range values: %v", err)
	}

	repo := NewRepository(rawDB)
	settings, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings failed: %v", err)
	}
	if settings.DNSCacheTTLMinutes != model.DNSCacheTTLDefault {
		t.Errorf("expected TTL clamped to default %d, got %d", model.DNSCacheTTLDefault, settings.DNSCacheTTLMinutes)
	}
	if settings.DNSCacheMaxEntries != model.DNSCacheEntriesDefault {
		t.Errorf("expected max entries clamped to default %d, got %d", model.DNSCacheEntriesDefault, settings.DNSCacheMaxEntries)
	}
}
