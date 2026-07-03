package service

import (
	"strings"
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/model"
)

// trackingTimeManager records every call so tests can assert both the values
// applied and the order they were applied in.
type trackingTimeManager struct {
	calls          []string // ordered log of applied operations
	timezone       string
	ntp            bool
	ntpServer      string
	setTimeCalled  bool
	lastSetTime    time.Time
	statusNTPSync  bool
	statusCurrTime string
}

func (t *trackingTimeManager) GetTimeStatus() (*model.TimeStatus, error) {
	ct := t.statusCurrTime
	if ct == "" {
		ct = time.Now().Format(time.RFC3339)
	}
	return &model.TimeStatus{CurrentTime: ct, NTPSynchronized: t.statusNTPSync}, nil
}

func (t *trackingTimeManager) SetTimezone(tz string) error {
	t.timezone = tz
	t.calls = append(t.calls, "timezone")
	return nil
}

func (t *trackingTimeManager) SetNTP(enable bool) error {
	t.ntp = enable
	t.calls = append(t.calls, "ntp")
	return nil
}

func (t *trackingTimeManager) SetTime(tm time.Time) error {
	t.setTimeCalled = true
	t.lastSetTime = tm
	t.calls = append(t.calls, "settime")
	return nil
}

func (t *trackingTimeManager) SetNTPServer(server string) error {
	t.ntpServer = server
	t.calls = append(t.calls, "ntpserver")
	return nil
}

func newTestTimeService(t *testing.T) (*TimeService, *trackingTimeManager) {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("Failed to init memory db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })

	repo := db.NewRepository(sqliteDB)
	tm := &trackingTimeManager{}
	return NewTimeService(repo, tm), tm
}

// --- Validation -----------------------------------------------------------

func TestValidateTimezone(t *testing.T) {
	valid := []string{"Asia/Bangkok", "UTC", "America/New_York", "Etc/GMT+7"}
	for _, tz := range valid {
		if err := ValidateTimezone(tz); err != nil {
			t.Errorf("expected %q valid, got: %v", tz, err)
		}
	}

	invalid := []string{
		"",
		"Asia/Bangkok (GMT+7:00)", // legacy display format, has spaces + parens
		"Asia/Bangkok; rm -rf /",  // injection-ish
		"Not/AReal_Zone_Name_XYZ", // resolves-fail
		"Asia/Bang kok",           // space
		"../../etc/passwd",        // path traversal chars are actually allowed by charset but LoadLocation fails
	}
	for _, tz := range invalid {
		if err := ValidateTimezone(tz); err == nil {
			t.Errorf("expected %q invalid, got nil", tz)
		}
	}
}

func TestValidateNTPServer(t *testing.T) {
	valid := []string{
		"",
		"pool.ntp.org",
		"time.google.com time.cloudflare.com",
		"192.168.1.1",
		"192.168.1.1 pool.ntp.org",
		"2.pool.ntp.org",
	}
	for _, s := range valid {
		if err := ValidateNTPServer(s); err != nil {
			t.Errorf("expected %q valid, got: %v", s, err)
		}
	}

	// Injection attempts that must be rejected — these are the security-critical
	// cases: anything that could inject a new timesyncd directive into the ini.
	injections := []string{
		"pool.ntp.org\nFallbackNTP=evil.example.com",
		"pool.ntp.org\r\n[Time]",
		"evil]\n[Manager",
		"a=b",
		"pool.ntp.org #comment",
		"bad_host!",
		"host_with_underscore",
	}
	for _, s := range injections {
		if err := ValidateNTPServer(s); err == nil {
			t.Errorf("expected injection %q to be rejected, got nil", s)
		}
	}
}

func TestValidateManualTime(t *testing.T) {
	if _, err := ValidateManualTime("2026-07-03T15:04:05+07:00"); err != nil {
		t.Errorf("expected valid RFC3339, got: %v", err)
	}
	bad := []string{
		"not-a-date",
		"2026-07-03 15:04:05",  // not RFC3339
		"1999-01-01T00:00:00Z", // year too low
		"3001-01-01T00:00:00Z", // year too high
	}
	for _, d := range bad {
		if _, err := ValidateManualTime(d); err == nil {
			t.Errorf("expected %q invalid, got nil", d)
		}
	}
}

// --- Apply behaviour ------------------------------------------------------

func TestUpdateAppliesInOrder(t *testing.T) {
	svc, tm := newTestTimeService(t)

	err := svc.Update(model.SystemTimeSettings{
		Timezone:  "America/New_York",
		NTPSync:   true,
		NTPServer: "time.google.com",
	})
	if err != nil {
		t.Fatalf("Update failed: %v", err)
	}

	// timezone changed (seed is Asia/Bangkok), server changed (seed pool.ntp.org),
	// then ntp toggle — in that order.
	want := []string{"timezone", "ntpserver", "ntp"}
	if strings.Join(tm.calls, ",") != strings.Join(want, ",") {
		t.Errorf("apply order = %v, want %v", tm.calls, want)
	}
	if tm.timezone != "America/New_York" || tm.ntpServer != "time.google.com" || !tm.ntp {
		t.Errorf("applied values wrong: tz=%q server=%q ntp=%v", tm.timezone, tm.ntpServer, tm.ntp)
	}
	if tm.setTimeCalled {
		t.Error("Update must never call SetTime")
	}
}

func TestUpdateSkipsUnchanged(t *testing.T) {
	svc, tm := newTestTimeService(t)

	// Seed defaults are Asia/Bangkok / ntpSync=true / pool.ntp.org. Re-save the
	// same tz+server but flip NTP off: only "ntp" should be applied.
	if err := svc.Update(model.SystemTimeSettings{
		Timezone:  "Asia/Bangkok",
		NTPSync:   false,
		NTPServer: "pool.ntp.org",
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if strings.Join(tm.calls, ",") != "ntp" {
		t.Errorf("expected only ntp applied, got %v", tm.calls)
	}
}

func TestUpdateRejectsBadInput(t *testing.T) {
	svc, tm := newTestTimeService(t)

	if err := svc.Update(model.SystemTimeSettings{Timezone: "Bogus/Zone", NTPServer: "pool.ntp.org"}); err == nil {
		t.Error("expected bad timezone to be rejected")
	}
	if err := svc.Update(model.SystemTimeSettings{Timezone: "UTC", NTPServer: "evil\n[Time]"}); err == nil {
		t.Error("expected injected NTP server to be rejected")
	}
	if len(tm.calls) != 0 {
		t.Errorf("no kernel calls expected on validation failure, got %v", tm.calls)
	}
}

func TestSetManualTimeRejectedWhenNTPOn(t *testing.T) {
	svc, tm := newTestTimeService(t)

	// Seed default has ntpSync=true.
	err := svc.SetManualTime("2026-07-03T15:04:05+07:00")
	if err == nil {
		t.Fatal("expected SetManualTime to be rejected while NTP is on")
	}
	if tm.setTimeCalled {
		t.Error("SetTime must not be called when rejected")
	}
}

func TestSetManualTimeAllowedWhenNTPOff(t *testing.T) {
	svc, tm := newTestTimeService(t)

	if err := svc.Update(model.SystemTimeSettings{Timezone: "Asia/Bangkok", NTPSync: false, NTPServer: "pool.ntp.org"}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	if err := svc.SetManualTime("2026-07-03T15:04:05+07:00"); err != nil {
		t.Fatalf("SetManualTime failed: %v", err)
	}
	if !tm.setTimeCalled {
		t.Error("expected SetTime to be called")
	}
}

func TestInitApplyConfigNeverSetsTime(t *testing.T) {
	svc, tm := newTestTimeService(t)

	if err := svc.InitApplyConfig(); err != nil {
		t.Fatalf("InitApplyConfig failed: %v", err)
	}
	if tm.setTimeCalled {
		t.Fatal("InitApplyConfig must NEVER call SetTime")
	}
	// Should apply tz, server and ntp toggle from the seeded config.
	if tm.timezone == "" {
		t.Error("expected timezone applied at startup")
	}
}
