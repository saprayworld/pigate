package db

import (
	"database/sql"
	"testing"
)

// TestMigrationAddsRoleStatusToLegacyUsers simulates upgrading a box whose
// users table predates the multi-user system (no role/status columns) and has
// an existing "pigate" account with is_initial=0. After migration that account
// MUST become super_admin/active, otherwise the upgrade locks everyone out.
func TestMigrationAddsRoleStatusToLegacyUsers(t *testing.T) {
	rawDB, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open sqlite: %v", err)
	}
	defer rawDB.Close()

	// Legacy schema: no role, no status columns.
	_, err = rawDB.Exec(`CREATE TABLE users (
		id TEXT PRIMARY KEY,
		username TEXT UNIQUE NOT NULL,
		password_hash TEXT NOT NULL,
		is_initial INTEGER DEFAULT 0,
		created_at DATETIME DEFAULT CURRENT_TIMESTAMP
	);`)
	if err != nil {
		t.Fatalf("failed to create legacy users table: %v", err)
	}
	_, err = rawDB.Exec(`INSERT INTO users (id, username, password_hash, is_initial)
		VALUES ('user-pigate', 'pigate', 'legacyhash', 0)`)
	if err != nil {
		t.Fatalf("failed to insert legacy pigate: %v", err)
	}

	if err := migrate(rawDB); err != nil {
		t.Fatalf("migrate failed: %v", err)
	}

	var role, status string
	var isInitial int
	err = rawDB.QueryRow("SELECT role, status, is_initial FROM users WHERE username = 'pigate'").
		Scan(&role, &status, &isInitial)
	if err != nil {
		t.Fatalf("failed to read migrated pigate: %v", err)
	}
	if role != "super_admin" {
		t.Errorf("legacy pigate role = %q, want super_admin", role)
	}
	if status != "active" {
		t.Errorf("legacy pigate status = %q, want active", status)
	}
	if isInitial != 0 {
		t.Errorf("legacy pigate is_initial changed to %d, want 0 (migration must not force re-init)", isInitial)
	}

	// Idempotent: running migrate again must not fail or duplicate columns.
	if err := migrate(rawDB); err != nil {
		t.Fatalf("second migrate failed (not idempotent): %v", err)
	}
}
