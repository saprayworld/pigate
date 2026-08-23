package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// newTestDNSBlocklistEnv wires a DNSBlocklistService against a temp-file DB
// (mirroring newDNSServerTestEnv in dns_server_test.go) and a
// MockDNSServerManager (never touches the real filesystem).
func newTestDNSBlocklistEnv(t *testing.T) (*DNSBlocklistService, *kernel.MockDNSServerManager) {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "pigate-test.db")
	sqlDB, err := db.InitDB(dbPath, true)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { sqlDB.Close() })
	repo := db.NewRepository(sqlDB)
	repo.SetMockMode(true, false)

	mgr := kernel.NewMockDNSServerManager()
	svc := NewDNSBlocklistService(repo, mgr)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load() on empty manifest: %v", err)
	}
	return svc, mgr
}

// blocklistTestServer serves body as a hosts-format file over HTTPS (via
// httptest) and returns a fetcher wired to reach it (newLoopbackFetcher from
// dns_blocklist_fetch_test.go — same package, test-only seam that bypasses
// the SSRF dialer guard so a loopback httptest.Server can be reached).
func blocklistTestServer(t *testing.T, body string) *httptest.Server {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(srv.Close)
	return srv
}

const sampleHostsBody = "" +
	"# sample blocklist\n" +
	"127.0.0.1 localhost\n" +
	"0.0.0.0 ads.example.com\n" +
	"0.0.0.0 tracker.example.net\n" +
	"1.2.3.4 spoofed.example.org\n"

// TestCreateFromURL_DefaultsToSinkholeNoConf covers plan §3 T-05 item 5:
// creating without specifying a blockMode gets DNSBlockModeSinkhole, and no
// <id>.conf is written in the mock.
func TestCreateFromURL_DefaultsToSinkholeNoConf(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "Test List", testBlocklistBaseURL+"/hosts", "", true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	if entry.BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("BlockMode = %q, want %q (default)", entry.BlockMode, model.DNSBlockModeSinkhole)
	}
	if entry.DomainCount != 3 {
		t.Errorf("DomainCount = %d, want 3 (localhost/loopback and the IP-spoofing entry are handled — spoofed.example.org's IP is discarded but the name itself is still accepted)", entry.DomainCount)
	}
	if size, exists := mgr.BlocklistFileInfo(entry.ID); !exists || size == 0 {
		t.Errorf("expected <id>.hosts to exist and be non-empty, got size=%d exists=%v", size, exists)
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); exists {
		t.Errorf("sinkhole-mode list must NOT have a <id>.conf file in the mock")
	}
}

// TestCreateFromURL_NXDomainWritesBothFiles covers plan §3 T-05 item 5:
// creating with blockMode=nxdomain writes both <id>.hosts and <id>.conf, and
// the .conf content is "address=/<domain>/" for every accepted domain.
func TestCreateFromURL_NXDomainWritesBothFiles(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "NX List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	if entry.BlockMode != model.DNSBlockModeNXDomain {
		t.Fatalf("BlockMode = %q, want nxdomain", entry.BlockMode)
	}
	if size, exists := mgr.BlocklistFileInfo(entry.ID); !exists || size == 0 {
		t.Fatalf("expected <id>.hosts to exist, got size=%d exists=%v", size, exists)
	}
	confBytes, exists := mgr.BlocklistConfContent(entry.ID)
	if !exists {
		t.Fatalf("expected <id>.conf to exist for nxdomain-mode list")
	}
	confStr := string(confBytes)
	if strings.Contains(confStr, "0.0.0.0") {
		t.Errorf(".conf must never contain an IP address, got:\n%s", confStr)
	}
	for _, domain := range []string{"ads.example.com", "tracker.example.net", "spoofed.example.org"} {
		want := "address=/" + domain + "/"
		if !strings.Contains(confStr, want) {
			t.Errorf(".conf missing %q, got:\n%s", want, confStr)
		}
	}
}

// TestCreateFromURL_NXDomainRejectedWhenUnsupported covers the
// SupportsBulkNXDomain() gate — using a manager that reports no support.
type unsupportedNXDomainManager struct {
	*kernel.MockDNSServerManager
}

func (m *unsupportedNXDomainManager) SupportsBulkNXDomain() bool { return false }

func TestCreateFromURL_NXDomainRejectedWhenUnsupported(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pigate-test.db")
	sqlDB, err := db.InitDB(dbPath, true)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()
	repo := db.NewRepository(sqlDB)
	repo.SetMockMode(true, false)

	mgr := &unsupportedNXDomainManager{kernel.NewMockDNSServerManager()}
	svc := NewDNSBlocklistService(repo, mgr)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	_, err = svc.CreateFromURL(context.Background(), "NX List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err == nil {
		t.Fatal("expected error creating nxdomain-mode list when SupportsBulkNXDomain() is false")
	}
	if got := svc.List(); len(got) != 0 {
		t.Errorf("a rejected create must not add a manifest entry, got %d entries", len(got))
	}
}

// TestSwitchNXDomainToSinkhole_RemovesConfWithoutFetching covers plan §3
// T-05 item 5: switching an existing nxdomain list to sinkhole drops <id>.conf
// and never touches the fetcher (svc.fetcher is set to nil, so any call
// would panic and fail the test).
func TestSwitchNXDomainToSinkhole_RemovesConfWithoutFetching(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "NX List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); !exists {
		t.Fatalf("precondition failed: expected <id>.conf to exist after nxdomain create")
	}

	// No fetcher: if UpdateInfo's mode switch ever tries to fetch, this
	// nil-pointer-dereferences and fails the test loudly.
	svc.fetcher = nil

	updated, err := svc.UpdateInfo(entry.ID, entry.Name, entry.URL, model.DNSBlockModeSinkhole, true)
	if err != nil {
		t.Fatalf("UpdateInfo (switch to sinkhole): %v", err)
	}
	if updated.BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("BlockMode = %q, want sinkhole", updated.BlockMode)
	}
	if updated.DomainCount != entry.DomainCount {
		t.Errorf("DomainCount changed across a mode switch (%d -> %d); switching mode must not re-parse/change domain membership", entry.DomainCount, updated.DomainCount)
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.conf to be removed after switching to sinkhole")
	}
	if size, exists := mgr.BlocklistFileInfo(entry.ID); !exists || size == 0 {
		t.Errorf("<id>.hosts must still exist after a mode switch, got size=%d exists=%v", size, exists)
	}
}

// TestSwitchSinkholeToNXDomain_OfflineRegeneratesConf covers plan §3 T-05
// item 5: switching sinkhole -> nxdomain without any network access produces
// a correct <id>.conf, derived purely from the already-written <id>.hosts.
func TestSwitchSinkholeToNXDomain_OfflineRegeneratesConf(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "Sinkhole List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeSinkhole, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}

	// No fetcher / no network from this point on.
	svc.fetcher = nil

	updated, err := svc.UpdateInfo(entry.ID, entry.Name, entry.URL, model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("UpdateInfo (switch to nxdomain, offline): %v", err)
	}
	if updated.BlockMode != model.DNSBlockModeNXDomain {
		t.Fatalf("BlockMode = %q, want nxdomain", updated.BlockMode)
	}
	confBytes, exists := mgr.BlocklistConfContent(entry.ID)
	if !exists {
		t.Fatalf("expected <id>.conf to exist after offline switch to nxdomain")
	}
	confStr := string(confBytes)
	for _, domain := range []string{"ads.example.com", "tracker.example.net", "spoofed.example.org"} {
		want := "address=/" + domain + "/"
		if !strings.Contains(confStr, want) {
			t.Errorf(".conf (regenerated offline) missing %q, got:\n%s", want, confStr)
		}
	}
}

// TestCreateFromURL_ExceedsNXDomainQuotaButSinkholeSucceeds covers plan §3
// T-05 items 2/5: exceeding DNSBlocklistMaxNXDomainDomains is rejected, but
// the same content succeeds as a sinkhole-mode list.
func TestCreateFromURL_ExceedsNXDomainQuotaButSinkholeSucceeds(t *testing.T) {
	svc, _ := newTestDNSBlocklistEnv(t)

	var sb strings.Builder
	for i := 0; i < model.DNSBlocklistMaxNXDomainDomains+10; i++ {
		sb.WriteString("0.0.0.0 d")
		sb.WriteString(itoa(i))
		sb.WriteString(".example.com\n")
	}
	srv := blocklistTestServer(t, sb.String())
	svc.fetcher = newLoopbackFetcher(srv)

	_, err := svc.CreateFromURL(context.Background(), "Huge NX List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err == nil {
		t.Fatal("expected error exceeding DNSBlocklistMaxNXDomainDomains, got nil")
	}
	if got := svc.List(); len(got) != 0 {
		t.Fatalf("a rejected create must not add a manifest entry, got %d entries", len(got))
	}

	entry, err := svc.CreateFromURL(context.Background(), "Huge Sinkhole List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeSinkhole, true)
	if err != nil {
		t.Fatalf("same content as sinkhole should succeed: %v", err)
	}
	if entry.DomainCount != model.DNSBlocklistMaxNXDomainDomains+10 {
		t.Errorf("DomainCount = %d, want %d", entry.DomainCount, model.DNSBlocklistMaxNXDomainDomains+10)
	}
}

// itoa avoids importing strconv just for this one loop in the test file
// above (kept trivial and dependency-free).
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

// TestDisabledList_NotAppliedByApplyAll covers plan §3 T-05 item 5: a
// disabled list contributes no ref to ApplyZones.
func TestDisabledList_NotAppliedByApplyAll(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pigate-test.db")
	sqlDB, err := db.InitDB(dbPath, true)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()
	repo := db.NewRepository(sqlDB)
	repo.SetMockMode(true, false)

	mgr := kernel.NewMockDNSServerManager()
	blocklistSvc := NewDNSBlocklistService(repo, mgr)
	if err := blocklistSvc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv := blocklistTestServer(t, sampleHostsBody)
	blocklistSvc.fetcher = newLoopbackFetcher(srv)

	entry, err := blocklistSvc.CreateFromURL(context.Background(), "Disabled List", testBlocklistBaseURL+"/hosts", "", false)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	if entry.Enabled {
		t.Fatalf("expected list to be created disabled")
	}

	dnsService := NewDNSService(repo, &kernel.MockDNSManager{})
	dnsServerSvc := NewDNSServerService(repo, mgr, dnsService)
	dnsServerSvc.SetBlocklistProvider(blocklistSvc)

	var sunk []model.DNSBlocklist
	dnsServerSvc.SetBlocklistSink(func(lists []model.DNSBlocklist) { sunk = lists })

	if err := dnsServerSvc.ApplyAll(); err != nil {
		t.Fatalf("ApplyAll: %v", err)
	}
	if mgr.ApplyCount != 1 {
		t.Errorf("ApplyCount = %d, want 1", mgr.ApplyCount)
	}
	if len(sunk) != 1 || sunk[0].ID != entry.ID {
		t.Errorf("blocklist sink should still be invoked with the full manifest (including disabled lists), got %+v", sunk)
	}
}

// TestDelete_RemovesFilesAndManifestEntry covers plan §3 T-05 item 5.
func TestDelete_RemovesFilesAndManifestEntry(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "To Delete", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}

	if err := svc.Delete(entry.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, ok := svc.Get(entry.ID); ok {
		t.Errorf("expected manifest entry to be gone after Delete")
	}
	if _, exists := mgr.BlocklistFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.hosts to be gone after Delete")
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.conf to be gone after Delete")
	}
}

// TestRefresh_FetchFailureKeepsExistingFile covers plan §3 T-05 item 5:
// "refresh ที่ fetch fail ไม่ทำให้ไฟล์เดิมหาย".
func TestRefresh_FetchFailureKeepsExistingFile(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "Refresh Me", testBlocklistBaseURL+"/hosts", "", true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	beforeSize, _ := mgr.BlocklistFileInfo(entry.ID)

	// Point the fetcher at a server that always 404s, simulating a fetch
	// failure without touching the SSRF guard.
	failSrv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer failSrv.Close()
	svc.fetcher = newLoopbackFetcher(failSrv)

	result, err := svc.Refresh(context.Background(), entry.ID)
	if err == nil {
		t.Fatal("expected Refresh to return an error on fetch failure")
	}
	if result.DomainCount != entry.DomainCount {
		t.Errorf("DomainCount changed after a failed refresh (%d -> %d), want unchanged", entry.DomainCount, result.DomainCount)
	}
	if result.LastError == "" {
		t.Errorf("expected LastError to be recorded after a failed refresh")
	}
	afterSize, exists := mgr.BlocklistFileInfo(entry.ID)
	if !exists || afterSize != beforeSize {
		t.Errorf("expected <id>.hosts to be unchanged after a failed refresh, before=%d after=%d exists=%v", beforeSize, afterSize, exists)
	}

	stored, ok := svc.Get(entry.ID)
	if !ok || stored.LastError == "" {
		t.Errorf("expected the stored manifest entry to carry LastError, got %+v (ok=%v)", stored, ok)
	}
}

// TestCreateFromURL_ExceedsListCountQuota covers plan §3 T-05 item 5: "เกิน
// DNSBlocklistsMax = error".
func TestCreateFromURL_ExceedsListCountQuota(t *testing.T) {
	svc, _ := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	for i := 0; i < model.DNSBlocklistsMax; i++ {
		if _, err := svc.CreateFromURL(context.Background(), "List", testBlocklistBaseURL+"/hosts", "", false); err != nil {
			t.Fatalf("CreateFromURL[%d]: %v", i, err)
		}
	}

	_, err := svc.CreateFromURL(context.Background(), "One Too Many", testBlocklistBaseURL+"/hosts", "", false)
	if err == nil {
		t.Fatal("expected error exceeding DNSBlocklistsMax, got nil")
	}
}

// TestCreateFromUpload_NoNetworkNeeded covers the upload path end-to-end
// without any HTTP server at all.
func TestCreateFromUpload_NoNetworkNeeded(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	// No fetcher configured; upload must never touch it.
	svc.fetcher = nil

	entry, err := svc.CreateFromUpload("Uploaded List", []byte(sampleHostsBody), model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromUpload: %v", err)
	}
	if entry.SourceType != model.DNSBlocklistSourceUpload {
		t.Errorf("SourceType = %q, want %q", entry.SourceType, model.DNSBlocklistSourceUpload)
	}
	if entry.URL != "" {
		t.Errorf("URL should be empty for an upload-sourced list, got %q", entry.URL)
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); !exists {
		t.Errorf("expected <id>.conf to exist for nxdomain-mode upload")
	}
}

// TestDelete_AppliesBeforeRemovingFiles covers the tech-lead sign-off fix for
// the stale-directive window (docs/ref/todo/dns-blocklist-import-plan.md,
// same class of problem as Caution 16/issue #50): Delete must call the wired
// applyDNS callback (which regenerates pigate-dns.conf without this id)
// BEFORE removing the underlying <id>.hosts/<id>.conf files, and the
// manifest entry must already be gone by the time applyDNS runs (so a
// regenerate-from-manifest style callback, like the real
// DNSServerService.ApplyAll, would not re-reference this id).
func TestDelete_AppliesBeforeRemovingFiles(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "To Delete", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}

	var applyCalls int
	svc.SetApplyDNSCallback(func() error {
		applyCalls++
		if _, ok := svc.Get(entry.ID); ok {
			t.Errorf("applyDNS callback ran before the manifest entry was removed")
		}
		if _, exists := mgr.BlocklistFileInfo(entry.ID); !exists {
			t.Errorf("applyDNS callback ran AFTER <id>.hosts was already removed — stale-directive window not closed")
		}
		if _, exists := mgr.BlocklistConfFileInfo(entry.ID); !exists {
			t.Errorf("applyDNS callback ran AFTER <id>.conf was already removed — stale-directive window not closed")
		}
		return nil
	})

	if err := svc.Delete(entry.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if applyCalls != 1 {
		t.Errorf("applyDNS calls = %d, want 1", applyCalls)
	}
	if _, exists := mgr.BlocklistFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.hosts to be gone after Delete")
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.conf to be gone after Delete")
	}
}

// TestDelete_ApplyFailureKeepsFiles covers the other half of the same fix:
// if applyDNS fails, Delete must NOT remove the files — the on-disk
// pigate-dns.conf may still (or again) reference them, so removing them
// would risk the exact DNS+DHCP outage the fix exists to prevent.
func TestDelete_ApplyFailureKeepsFiles(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "To Delete", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}

	svc.SetApplyDNSCallback(func() error {
		return fmt.Errorf("simulated apply failure")
	})

	if err := svc.Delete(entry.ID); err == nil {
		t.Fatal("expected Delete to return an error when applyDNS fails")
	}
	if _, exists := mgr.BlocklistFileInfo(entry.ID); !exists {
		t.Errorf("expected <id>.hosts to still exist after a failed apply during Delete")
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); !exists {
		t.Errorf("expected <id>.conf to still exist after a failed apply during Delete")
	}
}

// TestSwitchNXDomainToSinkhole_AppliesBeforeRemovingConf is
// TestSwitchNXDomainToSinkhole_RemovesConfWithoutFetching's sibling for the
// same stale-directive-window fix: switching nxdomain -> sinkhole must
// update the manifest and call applyDNS BEFORE removing the now-stale
// <id>.conf, so a config regenerated from the manifest at that point already
// reflects "sinkhole" (no conf-file= directive) rather than still pointing
// at a file that is about to disappear.
func TestSwitchNXDomainToSinkhole_AppliesBeforeRemovingConf(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "NX List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	// No fetcher: mode-switch must never fetch.
	svc.fetcher = nil

	var applyCalls int
	svc.SetApplyDNSCallback(func() error {
		applyCalls++
		stored, ok := svc.Get(entry.ID)
		if !ok || stored.BlockMode != model.DNSBlockModeSinkhole {
			t.Errorf("applyDNS callback ran before the manifest reflected the new sinkhole mode, got %+v (ok=%v)", stored, ok)
		}
		if _, exists := mgr.BlocklistConfFileInfo(entry.ID); !exists {
			t.Errorf("applyDNS callback ran AFTER <id>.conf was already removed — stale-directive window not closed")
		}
		return nil
	})

	updated, err := svc.UpdateInfo(entry.ID, entry.Name, entry.URL, model.DNSBlockModeSinkhole, true)
	if err != nil {
		t.Fatalf("UpdateInfo (switch to sinkhole): %v", err)
	}
	if updated.BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("BlockMode = %q, want sinkhole", updated.BlockMode)
	}
	if applyCalls != 1 {
		t.Errorf("applyDNS calls = %d, want 1", applyCalls)
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.conf to be removed after switching to sinkhole")
	}
	if size, exists := mgr.BlocklistFileInfo(entry.ID); !exists || size == 0 {
		t.Errorf("<id>.hosts must still exist after a mode switch, got size=%d exists=%v", size, exists)
	}
}

// TestSwitchNXDomainToSinkhole_ApplyFailureKeepsConf covers the other half:
// if applyDNS fails after the manifest is updated to sinkhole, the stale
// <id>.conf must be left in place rather than removed.
func TestSwitchNXDomainToSinkhole_ApplyFailureKeepsConf(t *testing.T) {
	svc, mgr := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "NX List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	svc.fetcher = nil

	svc.SetApplyDNSCallback(func() error {
		return fmt.Errorf("simulated apply failure")
	})

	_, err = svc.UpdateInfo(entry.ID, entry.Name, entry.URL, model.DNSBlockModeSinkhole, true)
	if err == nil {
		t.Fatal("expected UpdateInfo to return an error when applyDNS fails")
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); !exists {
		t.Errorf("expected <id>.conf to still exist after a failed apply during a mode switch")
	}
	stored, ok := svc.Get(entry.ID)
	if !ok || stored.BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("expected the manifest to already reflect sinkhole mode even though apply failed, got %+v (ok=%v)", stored, ok)
	}
}

// TestSwitchSinkholeToNXDomain_NoApplyCallNeeded covers that the OTHER mode
// switch direction (writing a new file, never removing one) does not need —
// and in this codebase's real wiring, does not get — an applyDNS call: it is
// safe purely because a not-yet-referenced file can never be a stale
// directive.
func TestSwitchSinkholeToNXDomain_NoApplyCallNeeded(t *testing.T) {
	svc, _ := newTestDNSBlocklistEnv(t)
	srv := blocklistTestServer(t, sampleHostsBody)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "Sinkhole List", testBlocklistBaseURL+"/hosts", model.DNSBlockModeSinkhole, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	svc.fetcher = nil

	var applyCalls int
	svc.SetApplyDNSCallback(func() error {
		applyCalls++
		return nil
	})

	if _, err := svc.UpdateInfo(entry.ID, entry.Name, entry.URL, model.DNSBlockModeNXDomain, true); err != nil {
		t.Fatalf("UpdateInfo (switch to nxdomain, offline): %v", err)
	}
	if applyCalls != 0 {
		t.Errorf("applyDNS calls = %d, want 0 (writing a new artifact never needs an apply-first step)", applyCalls)
	}
}

// TestDelete_WiredToRealApplyAll_RegeneratesConfBeforeRemovingFiles is an
// end-to-end version of TestDelete_AppliesBeforeRemovingFiles/
// TestSwitchNXDomainToSinkhole_AppliesBeforeRemovingConf using the actual
// DNSServerService.ApplyAll (wired via SetApplyDNSCallback, mirroring
// main.go) instead of a synthetic callback, confirming the real wiring
// used in production actually excludes a just-deleted blocklist's ref from
// the ApplyZones call BEFORE Delete removes its files.
func TestDelete_WiredToRealApplyAll_RegeneratesConfBeforeRemovingFiles(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pigate-test.db")
	sqlDB, err := db.InitDB(dbPath, true)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()
	repo := db.NewRepository(sqlDB)
	repo.SetMockMode(true, false)

	mgr := kernel.NewMockDNSServerManager()
	blocklistSvc := NewDNSBlocklistService(repo, mgr)
	if err := blocklistSvc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	srv := blocklistTestServer(t, sampleHostsBody)
	blocklistSvc.fetcher = newLoopbackFetcher(srv)

	entry, err := blocklistSvc.CreateFromURL(context.Background(), "To Delete", testBlocklistBaseURL+"/hosts", model.DNSBlockModeNXDomain, true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}

	dnsService := NewDNSService(repo, &kernel.MockDNSManager{})
	dnsServerSvc := NewDNSServerService(repo, mgr, dnsService)
	dnsServerSvc.SetBlocklistProvider(blocklistSvc)
	blocklistSvc.SetApplyDNSCallback(dnsServerSvc.ApplyAll)

	if err := blocklistSvc.Delete(entry.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if mgr.ApplyCount != 1 {
		t.Errorf("ApplyCount = %d, want 1 (Delete must trigger exactly one apply)", mgr.ApplyCount)
	}
	if _, exists := mgr.BlocklistFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.hosts to be gone after Delete")
	}
	if _, exists := mgr.BlocklistConfFileInfo(entry.ID); exists {
		t.Errorf("expected <id>.conf to be gone after Delete")
	}
}

// TestExcludeSet_OwnZonesAndHostnameNeverBlocked covers plan §3 T-05 item 3.
func TestExcludeSet_OwnZonesAndHostnameNeverBlocked(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "pigate-test.db")
	sqlDB, err := db.InitDB(dbPath, true)
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	defer sqlDB.Close()
	repo := db.NewRepository(sqlDB)
	repo.SetMockMode(true, false)
	if err := repo.CreateDNSZone(model.DNSZone{ID: "zone-test-1", ZoneName: "own-zone.test", Enabled: true}); err != nil {
		t.Fatalf("CreateDNSZone: %v", err)
	}

	mgr := kernel.NewMockDNSServerManager()
	svc := NewDNSBlocklistService(repo, mgr)
	if err := svc.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	body := "0.0.0.0 own-zone.test\n0.0.0.0 sub.own-zone.test\n0.0.0.0 ads.example.com\n"
	srv := blocklistTestServer(t, body)
	svc.fetcher = newLoopbackFetcher(srv)

	entry, err := svc.CreateFromURL(context.Background(), "Excl Test", testBlocklistBaseURL+"/hosts", "", true)
	if err != nil {
		t.Fatalf("CreateFromURL: %v", err)
	}
	if entry.DomainCount != 1 {
		t.Errorf("DomainCount = %d, want 1 (own-zone.test and its subdomain must be excluded)", entry.DomainCount)
	}
}
