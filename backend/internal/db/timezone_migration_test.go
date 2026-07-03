package db

import (
	"testing"
	"time"
)

func TestNormalizeTimezone(t *testing.T) {
	cases := map[string]string{
		"Asia/Bangkok (GMT+7:00)":   "Asia/Bangkok",
		"Asia/Bangkok":              "Asia/Bangkok",
		"UTC (GMT+0:00)":            "UTC",
		"  Asia/Singapore (GMT+8) ": "Asia/Singapore",
		"America/New_York":          "America/New_York",
		"":                          "",
	}
	for in, want := range cases {
		if got := NormalizeTimezone(in); got != want {
			t.Errorf("NormalizeTimezone(%q) = %q, want %q", in, got, want)
		}
	}

	// Idempotent: normalizing an already-normalized value is a no-op.
	once := NormalizeTimezone("Asia/Bangkok (GMT+7:00)")
	if twice := NormalizeTimezone(once); twice != once {
		t.Errorf("NormalizeTimezone not idempotent: %q -> %q", once, twice)
	}
}

// TestSeededTimezoneIsValidIANA guards against a regression where the seed
// stores a display string that time.LoadLocation (and systemd-timedated) reject.
func TestSeededTimezoneIsValidIANA(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	repo := NewRepository(db)
	settings, err := repo.GetSystemTimeSettings()
	if err != nil {
		t.Fatalf("GetSystemTimeSettings failed: %v", err)
	}
	if _, err := time.LoadLocation(settings.Timezone); err != nil {
		t.Errorf("seeded timezone %q is not a valid IANA zone: %v", settings.Timezone, err)
	}
}

// TestMigrationNormalizesLegacyTimezone inserts a legacy display-format value
// and verifies the migration rewrites it to a bare IANA name, and is safe to
// run again.
func TestMigrationNormalizesLegacyTimezone(t *testing.T) {
	db, err := InitDB(":memory:")
	if err != nil {
		t.Fatalf("InitDB failed: %v", err)
	}
	defer db.Close()

	if _, err := db.Exec("UPDATE system_time_settings SET timezone = ? WHERE id = 1", "Asia/Bangkok (GMT+7:00)"); err != nil {
		t.Fatalf("failed to seed legacy value: %v", err)
	}

	if err := normalizeTimezoneSetting(db); err != nil {
		t.Fatalf("normalizeTimezoneSetting failed: %v", err)
	}

	var tz string
	if err := db.QueryRow("SELECT timezone FROM system_time_settings WHERE id = 1").Scan(&tz); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if tz != "Asia/Bangkok" {
		t.Errorf("after migration timezone = %q, want %q", tz, "Asia/Bangkok")
	}

	// Idempotent second run.
	if err := normalizeTimezoneSetting(db); err != nil {
		t.Fatalf("second normalizeTimezoneSetting failed: %v", err)
	}
	if err := db.QueryRow("SELECT timezone FROM system_time_settings WHERE id = 1").Scan(&tz); err != nil {
		t.Fatalf("scan failed: %v", err)
	}
	if tz != "Asia/Bangkok" {
		t.Errorf("after second migration timezone = %q, want %q", tz, "Asia/Bangkok")
	}
}
