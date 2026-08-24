package service

import (
	"encoding/json"
	"strings"
	"sync"
	"testing"

	"pigate/internal/kernel"
	"pigate/internal/model"
)

func newTestBlocklistStore(t *testing.T) (*blocklistStore, *kernel.MockDNSServerManager) {
	t.Helper()
	mgr := kernel.NewMockDNSServerManager()
	store := newBlocklistStore(mgr)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() on empty manifest returned error: %v", err)
	}
	return store, mgr
}

func sampleBlocklist(id, blockMode string) model.DNSBlocklist {
	return model.DNSBlocklist{
		ID:          id,
		Name:        "Test list " + id,
		SourceType:  model.DNSBlocklistSourceURL,
		URL:         "https://example.invalid/hosts",
		BlockMode:   blockMode,
		Enabled:     true,
		DomainCount: 10,
		FileBytes:   100,
		Sha256:      "deadbeef",
		CreatedAt:   "2026-08-22T00:00:00Z",
	}
}

func TestBlocklistStore_SaveLoadRoundTrip(t *testing.T) {
	store, mgr := newTestBlocklistStore(t)

	if err := store.Add(sampleBlocklist("bl-aaa111", model.DNSBlockModeSinkhole)); err != nil {
		t.Fatalf("Add sinkhole list failed: %v", err)
	}
	if err := store.Add(sampleBlocklist("bl-bbb222", model.DNSBlockModeNXDomain)); err != nil {
		t.Fatalf("Add nxdomain list failed: %v", err)
	}

	// Fresh store reading the same underlying manager must see both lists
	// with blockMode intact.
	store2 := newBlocklistStore(mgr)
	if err := store2.Load(); err != nil {
		t.Fatalf("reload failed: %v", err)
	}
	got := store2.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 lists after reload, got %d", len(got))
	}
	byID := map[string]model.DNSBlocklist{}
	for _, l := range got {
		byID[l.ID] = l
	}
	if byID["bl-aaa111"].BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("bl-aaa111 blockMode = %q, want sinkhole", byID["bl-aaa111"].BlockMode)
	}
	if byID["bl-bbb222"].BlockMode != model.DNSBlockModeNXDomain {
		t.Errorf("bl-bbb222 blockMode = %q, want nxdomain", byID["bl-bbb222"].BlockMode)
	}
}

func TestBlocklistStore_List_ReturnsCopyNotReference(t *testing.T) {
	store, _ := newTestBlocklistStore(t)
	if err := store.Add(sampleBlocklist("bl-copy01", model.DNSBlockModeSinkhole)); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	got := store.List()
	got[0].Name = "mutated by caller"

	got2 := store.List()
	if got2[0].Name == "mutated by caller" {
		t.Fatalf("List() leaked internal storage — mutation of returned slice affected the store's cache")
	}
}

func TestBlocklistStore_MissingManifest_IsEmptyNotError(t *testing.T) {
	mgr := kernel.NewMockDNSServerManager()
	store := newBlocklistStore(mgr)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() on missing manifest returned error: %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("expected empty list, got %d entries", len(got))
	}
}

func TestBlocklistStore_CorruptJSON_QuarantinesAndStartsFresh(t *testing.T) {
	mgr := kernel.NewMockDNSServerManager()
	if err := mgr.WriteBlocklistManifest([]byte("{ this is not valid json")); err != nil {
		t.Fatalf("seed corrupt manifest failed: %v", err)
	}

	store := newBlocklistStore(mgr)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() on corrupt manifest should not return an error (quarantine-and-recover), got: %v", err)
	}
	if got := store.List(); len(got) != 0 {
		t.Fatalf("expected empty list after quarantine-recovery, got %d entries", len(got))
	}

	// Quarantine must have discarded the corrupt bytes (the mock's
	// QuarantineBlocklistManifest clears its in-memory manifest field) rather
	// than leaving them for the next read.
	raw, err := mgr.ReadBlocklistManifest()
	if err != nil {
		t.Fatalf("ReadBlocklistManifest after quarantine: %v", err)
	}
	if len(raw) != 0 {
		t.Fatalf("expected manifest to be cleared after quarantine, got %d bytes", len(raw))
	}

	// Store must still be writable after recovering from corruption.
	if err := store.Add(sampleBlocklist("bl-fresh01", model.DNSBlockModeSinkhole)); err != nil {
		t.Fatalf("Add after quarantine-recovery failed: %v", err)
	}
}

func TestBlocklistStore_MissingBlockModeKey_NormalizesToSinkhole(t *testing.T) {
	mgr := kernel.NewMockDNSServerManager()
	// Simulate a manifest written before the blockMode field existed: no
	// "blockMode" key at all in the JSON.
	raw := `{
		"schemaVersion": 1,
		"updatedAt": "2026-08-01T00:00:00Z",
		"lists": [
			{
				"id": "bl-legacy1",
				"name": "Legacy list",
				"sourceType": "url",
				"url": "https://example.invalid/hosts",
				"enabled": true,
				"domainCount": 5,
				"fileBytes": 50,
				"sha256": "abc123",
				"createdAt": "2026-01-01T00:00:00Z"
			}
		]
	}`
	if err := mgr.WriteBlocklistManifest([]byte(raw)); err != nil {
		t.Fatalf("seed manifest failed: %v", err)
	}

	store := newBlocklistStore(mgr)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() on manifest without blockMode key failed: %v", err)
	}
	got, ok := store.Get("bl-legacy1")
	if !ok {
		t.Fatalf("expected bl-legacy1 to load")
	}
	if got.BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("blockMode = %q, want %q (default for blocklists, not nxdomain)", got.BlockMode, model.DNSBlockModeSinkhole)
	}
}

func TestBlocklistStore_GarbageBlockMode_ClampsWithoutFailingManifest(t *testing.T) {
	mgr := kernel.NewMockDNSServerManager()
	raw := `{
		"schemaVersion": 1,
		"updatedAt": "2026-08-01T00:00:00Z",
		"lists": [
			{
				"id": "bl-good0001",
				"name": "Good list",
				"sourceType": "url",
				"url": "https://example.invalid/a",
				"blockMode": "sinkhole",
				"enabled": true,
				"domainCount": 5,
				"fileBytes": 50,
				"sha256": "abc123",
				"createdAt": "2026-01-01T00:00:00Z"
			},
			{
				"id": "bl-garbage1",
				"name": "Garbage mode list",
				"sourceType": "url",
				"url": "https://example.invalid/b",
				"blockMode": "totally-not-a-mode",
				"enabled": true,
				"domainCount": 5,
				"fileBytes": 50,
				"sha256": "def456",
				"createdAt": "2026-01-02T00:00:00Z"
			}
		]
	}`
	if err := mgr.WriteBlocklistManifest([]byte(raw)); err != nil {
		t.Fatalf("seed manifest failed: %v", err)
	}

	store := newBlocklistStore(mgr)
	if err := store.Load(); err != nil {
		t.Fatalf("Load() with one garbage blockMode should not fail the whole manifest, got: %v", err)
	}
	got := store.List()
	if len(got) != 2 {
		t.Fatalf("expected both lists to still load, got %d", len(got))
	}
	garbage, ok := store.Get("bl-garbage1")
	if !ok {
		t.Fatalf("expected bl-garbage1 to still be present")
	}
	if garbage.BlockMode != model.DNSBlocklistDefaultBlockMode {
		t.Errorf("garbage blockMode clamped to %q, want %q", garbage.BlockMode, model.DNSBlocklistDefaultBlockMode)
	}
	good, ok := store.Get("bl-good0001")
	if !ok || good.BlockMode != model.DNSBlockModeSinkhole {
		t.Errorf("good list's blockMode should be unaffected, got %+v (ok=%v)", good, ok)
	}
}

func TestBlocklistStore_FutureSchemaVersion_FailsClosed(t *testing.T) {
	mgr := kernel.NewMockDNSServerManager()
	future := model.BlocklistManifest{
		SchemaVersion: model.BlocklistManifestSchemaVersion + 1,
		UpdatedAt:     "2026-08-22T00:00:00Z",
		Lists: []model.DNSBlocklist{
			sampleBlocklist("bl-future01", model.DNSBlockModeSinkhole),
		},
	}
	raw, err := json.Marshal(future)
	if err != nil {
		t.Fatalf("marshal future manifest: %v", err)
	}
	if err := mgr.WriteBlocklistManifest(raw); err != nil {
		t.Fatalf("seed future manifest failed: %v", err)
	}

	store := newBlocklistStore(mgr)
	err = store.Load()
	if err == nil {
		t.Fatalf("expected Load() to fail closed on a future schemaVersion, got nil error")
	}
	if !strings.Contains(err.Error(), "schemaVersion") {
		t.Errorf("error should mention schemaVersion, got: %v", err)
	}

	// readOnly must now reject any mutation without touching the on-disk
	// manifest (which still belongs to the newer schema).
	addErr := store.Add(sampleBlocklist("bl-shouldno", model.DNSBlockModeSinkhole))
	if addErr == nil {
		t.Fatalf("expected Add() to fail on a read-only (future-schema) store")
	}

	rawAfter, rerr := mgr.ReadBlocklistManifest()
	if rerr != nil {
		t.Fatalf("ReadBlocklistManifest after failed Add: %v", rerr)
	}
	if string(rawAfter) != string(raw) {
		t.Fatalf("read-only store must never overwrite a newer-schema manifest on disk")
	}
}

func TestBlocklistStore_ConcurrentAdd_Race(t *testing.T) {
	store, _ := newTestBlocklistStore(t)

	const n = 50
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			id := "bl-" + string(rune('a'+(i%26))) + string(rune('a'+((i/26)%26))) + "000000"
			errs[i] = store.Add(sampleBlocklist(id, model.DNSBlockModeSinkhole))
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("concurrent Add[%d] failed: %v", i, err)
		}
	}

	got := store.List()
	if len(got) != n {
		t.Fatalf("expected %d lists after concurrent Add, got %d", n, len(got))
	}
	seen := map[string]bool{}
	for _, l := range got {
		if seen[l.ID] {
			t.Fatalf("duplicate id %q in final list", l.ID)
		}
		seen[l.ID] = true
	}
}

func TestBlocklistStore_UpdateToggleRemove(t *testing.T) {
	store, _ := newTestBlocklistStore(t)
	if err := store.Add(sampleBlocklist("bl-upd00001", model.DNSBlockModeSinkhole)); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	if err := store.Update("bl-upd00001", func(l *model.DNSBlocklist) error {
		l.DomainCount = 999
		return nil
	}); err != nil {
		t.Fatalf("Update failed: %v", err)
	}
	got, ok := store.Get("bl-upd00001")
	if !ok || got.DomainCount != 999 {
		t.Fatalf("Update did not persist: %+v (ok=%v)", got, ok)
	}

	if err := store.Toggle("bl-upd00001"); err != nil {
		t.Fatalf("Toggle failed: %v", err)
	}
	got, _ = store.Get("bl-upd00001")
	if got.Enabled {
		t.Fatalf("Toggle did not flip Enabled")
	}

	if err := store.Remove("bl-upd00001"); err != nil {
		t.Fatalf("Remove failed: %v", err)
	}
	if _, ok := store.Get("bl-upd00001"); ok {
		t.Fatalf("expected list to be gone after Remove")
	}

	// Remove of a nonexistent id is not an error (idempotent).
	if err := store.Remove("bl-doesnotexist"); err != nil {
		t.Fatalf("Remove of nonexistent id should not error, got: %v", err)
	}
}

func TestBlocklistStore_ReplaceAll(t *testing.T) {
	store, _ := newTestBlocklistStore(t)
	if err := store.Add(sampleBlocklist("bl-old000001", model.DNSBlockModeSinkhole)); err != nil {
		t.Fatalf("Add failed: %v", err)
	}

	replacement := []model.DNSBlocklist{
		sampleBlocklist("bl-new000001", model.DNSBlockModeNXDomain),
		sampleBlocklist("bl-new000002", model.DNSBlockModeSinkhole),
	}
	if err := store.ReplaceAll(replacement); err != nil {
		t.Fatalf("ReplaceAll failed: %v", err)
	}

	got := store.List()
	if len(got) != 2 {
		t.Fatalf("expected 2 lists after ReplaceAll, got %d", len(got))
	}
	if _, ok := store.Get("bl-old000001"); ok {
		t.Fatalf("old list should be gone after ReplaceAll")
	}
}
