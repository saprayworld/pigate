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
	if settings.UpstreamMode != model.DNSUpstreamModeSystem {
		t.Errorf("UpstreamMode = %q, want %q (backward compat DEFAULT)", settings.UpstreamMode, model.DNSUpstreamModeSystem)
	}
	if len(settings.UpstreamServers) != 0 {
		t.Errorf("UpstreamServers = %v, want empty", settings.UpstreamServers)
	}

	// Idempotent: running migrate again must not fail or duplicate columns.
	if err := migrate(rawDB); err != nil {
		t.Fatalf("second migrate failed (not idempotent): %v", err)
	}
}

// TestMigrationAddsUpstreamColumnsToLegacyDNSServerSettings simulates
// upgrading a box whose dns_server_settings table predates the upstream
// resolver feature (docs/ref/todo/dns-server-settings-tab-and-upstream-plan.md
// T-02/T-08 item 1): only the pre-upstream columns exist (interfaces,
// query_logging, dns_cache_ttl_minutes, dns_cache_max_entries). After
// migrate(), the new columns must exist and read back "system" + an empty
// list, not zero-valued garbage — and the config generated afterward must be
// unaffected (byte-for-byte, covered separately by
// TestBuildDNSConfig_QueryLogByteIdentical in the kernel package).
func TestMigrationAddsUpstreamColumnsToLegacyDNSServerSettings(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	if _, err := rawDB.Exec(`CREATE TABLE dns_server_settings (
		id INTEGER PRIMARY KEY CHECK(id = 1),
		interfaces TEXT NOT NULL DEFAULT '',
		query_logging INTEGER NOT NULL DEFAULT 0,
		dns_cache_ttl_minutes INTEGER NOT NULL DEFAULT 60,
		dns_cache_max_entries INTEGER NOT NULL DEFAULT 4096
	);`); err != nil {
		t.Fatalf("failed to create pre-upstream dns_server_settings table: %v", err)
	}
	if _, err := rawDB.Exec(`INSERT INTO dns_server_settings (id, interfaces) VALUES (1, 'eth0')`); err != nil {
		t.Fatalf("failed to seed pre-upstream row: %v", err)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	repo := NewRepository(rawDB)
	settings, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings failed after migration: %v", err)
	}
	if settings.UpstreamMode != model.DNSUpstreamModeSystem {
		t.Errorf("UpstreamMode = %q, want %q", settings.UpstreamMode, model.DNSUpstreamModeSystem)
	}
	if len(settings.UpstreamServers) != 0 {
		t.Errorf("UpstreamServers = %v, want empty", settings.UpstreamServers)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("second migrate failed (not idempotent): %v", err)
	}
}

// TestGetDNSServerSettings_ClampsUnknownUpstreamMode covers plan §5 item 1: a
// mode value that ended up unrecognized in the DB (hand-edited, or imported)
// must be clamped to "system" on read, never propagated as-is to a caller
// that would pass it straight into buildDNSConfig.
func TestGetDNSServerSettings_ClampsUnknownUpstreamMode(t *testing.T) {
	rawDB, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer rawDB.Close()

	if _, err := rawDB.Exec("UPDATE dns_server_settings SET upstream_mode = 'bogus', upstream_servers = '1.1.1.1,not-an-ip,8.8.8.8' WHERE id = 1"); err != nil {
		t.Fatalf("failed to seed bogus upstream values: %v", err)
	}

	repo := NewRepository(rawDB)
	settings, err := repo.GetDNSServerSettings()
	if err != nil {
		t.Fatalf("GetDNSServerSettings failed: %v", err)
	}
	if settings.UpstreamMode != model.DNSUpstreamModeSystem {
		t.Errorf("expected UpstreamMode clamped to %q, got %q", model.DNSUpstreamModeSystem, settings.UpstreamMode)
	}
	if len(settings.UpstreamServers) != 2 || settings.UpstreamServers[0] != "1.1.1.1" || settings.UpstreamServers[1] != "8.8.8.8" {
		t.Errorf("expected non-IP entry dropped, got %v", settings.UpstreamServers)
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
