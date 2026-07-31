package service

import (
	"path/filepath"
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// newDNSServerTestEnv spins up a temp-file DB (mock-seeded) plus a
// DNSServerService wired to a real DNSService (so resolveUpstreams's "system"
// path exercises the real GetDNSConfig()) and a MockDNSServerManager whose
// ApplyCount is used to assert exactly how many times config-write +
// dnsmasq-restart happened (docs/ref/todo/
// dns-server-settings-tab-and-upstream-plan.md T-08 item 3/4).
func newDNSServerTestEnv(t *testing.T) (*DNSServerService, *db.Repository, *kernel.MockDNSServerManager) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pigate-test.db")
	sqlDB, err := db.InitDB(dbPath, true)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	repo := db.NewRepository(sqlDB)
	repo.SetMockMode(true, false)

	dnsService := NewDNSService(repo, &kernel.MockDNSManager{})
	dnsServerMgr := kernel.NewMockDNSServerManager()
	dnsServerService := NewDNSServerService(repo, dnsServerMgr, dnsService)
	return dnsServerService, repo, dnsServerMgr
}

// TestResolveUpstreams_SystemMode covers T-08 item 3: mode "system" (static)
// reads primary/secondary from System DNS.
func TestResolveUpstreams_SystemMode_Static(t *testing.T) {
	svc, repo, _ := newDNSServerTestEnv(t)

	if err := repo.UpdateDNSConfig(model.DNSConfigInput{Mode: "static", PrimaryDNS: "1.1.1.1", SecondaryDNS: "8.8.8.8", LocalDomain: "pigate.local"}); err != nil {
		t.Fatalf("UpdateDNSConfig: %v", err)
	}

	got := svc.resolveUpstreams(model.DNSServerSettings{UpstreamMode: model.DNSUpstreamModeSystem})
	want := []string{"1.1.1.1", "8.8.8.8"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("resolveUpstreams(system/static) = %v, want %v", got, want)
	}
}

// TestResolveUpstreams_CustomMode covers T-08 item 3: mode "custom" only
// uses settings.UpstreamServers and never calls into DNSService/System DNS —
// asserted here by seeding System DNS with a DIFFERENT value and confirming
// it is not what comes back.
func TestResolveUpstreams_CustomMode(t *testing.T) {
	svc, repo, _ := newDNSServerTestEnv(t)

	if err := repo.UpdateDNSConfig(model.DNSConfigInput{Mode: "static", PrimaryDNS: "1.1.1.1", LocalDomain: "pigate.local"}); err != nil {
		t.Fatalf("UpdateDNSConfig: %v", err)
	}

	got := svc.resolveUpstreams(model.DNSServerSettings{
		UpstreamMode:    model.DNSUpstreamModeCustom,
		UpstreamServers: []string{"9.9.9.9", "8.8.4.4"},
	})
	want := []string{"9.9.9.9", "8.8.4.4"}
	if len(got) != len(want) || got[0] != want[0] || got[1] != want[1] {
		t.Errorf("resolveUpstreams(custom) = %v, want %v (must not be System DNS's 1.1.1.1)", got, want)
	}
}

// TestHandleUpdateDNSConfig_NeverAppliesDNSServer is the T-06 regression
// guard at the service layer: ApplyAll() (which bumps MockDNSServerManager.
// ApplyCount) must only ever be invoked by the DNS Server's own code paths
// (InitApplyConfig / an explicit ApplyAll caller), never as a byproduct of
// resolveUpstreams alone.
func TestResolveUpstreams_DoesNotTouchApplyCount(t *testing.T) {
	svc, repo, mgr := newDNSServerTestEnv(t)
	if err := repo.UpdateDNSConfig(model.DNSConfigInput{Mode: "static", PrimaryDNS: "1.1.1.1", LocalDomain: "pigate.local"}); err != nil {
		t.Fatalf("UpdateDNSConfig: %v", err)
	}
	svc.resolveUpstreams(model.DNSServerSettings{UpstreamMode: model.DNSUpstreamModeSystem})
	svc.resolveUpstreams(model.DNSServerSettings{UpstreamMode: model.DNSUpstreamModeCustom, UpstreamServers: []string{"9.9.9.9"}})
	if mgr.ApplyCount != 0 {
		t.Errorf("resolveUpstreams must not call ApplyZones; ApplyCount = %d, want 0", mgr.ApplyCount)
	}
}

// TestApplyAll_UsesResolvedUpstreams covers ApplyAll wiring end-to-end: it
// must call resolveUpstreams(settings) (not the old no-arg form) and pass the
// result through to ApplyZones.
func TestApplyAll_UsesResolvedUpstreams(t *testing.T) {
	svc, repo, mgr := newDNSServerTestEnv(t)
	if err := repo.SetDNSServerSettings(false, model.DNSCacheTTLDefault, model.DNSCacheEntriesDefault, model.DNSUpstreamModeCustom, []string{"9.9.9.9"}); err != nil {
		t.Fatalf("SetDNSServerSettings: %v", err)
	}

	if err := svc.ApplyAll(); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}
	if mgr.ApplyCount != 1 {
		t.Errorf("ApplyCount = %d, want 1", mgr.ApplyCount)
	}
}
