package service

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"sync"
	"time"

	"pigate/internal/kernel"
	"pigate/internal/model"
)

// blocklistStore is the metadata store for the DNS blocklist import feature
// (docs/ref/todo/dns-blocklist-import-plan.md §2.3) — it deliberately
// replaces what would otherwise be a SQLite table with a single JSON
// manifest file (/var/lib/pigate/blocklists/manifest.json), per the owner's
// explicit decision (plan R1) to not use SQLite at all for this feature, not
// even for metadata. All actual byte I/O of that file lives in the kernel
// layer (kernel.DNSServerManager.ReadBlocklistManifest/
// WriteBlocklistManifest/QuarantineBlocklistManifest, T-02); this store only
// does marshal/unmarshal, locking, and the versioning/normalization business
// rules described below.
//
// Locking: mu is a single sync.RWMutex covering the ENTIRE
// read-modify-write cycle of every mutation (load the current cache, apply
// the change, marshal, write) — not just the final write step. Two
// concurrent mutations (e.g. two refreshes racing) must not interleave and
// silently drop one of them.
//
// No flock: there is exactly one pigate process on the host and it is the
// only writer of /var/lib/pigate/blocklists — the same single-writer
// assumption the project's SQLite database already relies on (SQLite here
// is opened by a single process too). A cross-process file lock would add
// complexity for a scenario (multiple pigate processes writing the same
// data directory concurrently) that is already out of scope for the whole
// project.
type blocklistStore struct {
	mu sync.RWMutex

	manager  kernel.DNSServerManager
	cache    model.BlocklistManifest
	loaded   bool
	readOnly bool
}

// newBlocklistStore constructs a store bound to manager. Callers must call
// Load() once (e.g. at startup) before using the store; List/Get on an
// unloaded store simply return an empty result rather than panicking, so a
// caller that forgets Load() fails soft.
func newBlocklistStore(manager kernel.DNSServerManager) *blocklistStore {
	return &blocklistStore{manager: manager}
}

// emptyManifest returns a fresh, schema-current, empty manifest — used both
// as the starting point when no manifest.json exists yet and as the
// recovery state after quarantining a corrupt one.
func emptyManifest() model.BlocklistManifest {
	return model.BlocklistManifest{
		SchemaVersion: model.BlocklistManifestSchemaVersion,
		UpdatedAt:     time.Now().UTC().Format(time.RFC3339),
		Lists:         []model.DNSBlocklist{},
	}
}

// Load reads manifest.json (via the kernel layer) into the in-RAM cache.
// Must be called before any other method is useful; safe to call again
// later (e.g. to force a reload), though nothing in this codebase currently
// does so since every mutation already keeps the cache in sync itself.
//
// Failure handling (plan §2.3 item 3):
//   - missing file (manager returns nil, nil)            -> empty manifest, not an error
//   - malformed JSON                                       -> quarantine the file, log, start fresh, not an error
//   - schemaVersion greater than this binary understands  -> readOnly=true, return an error (fail closed:
//     never overwrite a newer manifest we don't understand)
func (s *blocklistStore) Load() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.loadLocked()
}

// loadLocked does the actual work of Load(); callers must already hold the
// write lock.
func (s *blocklistStore) loadLocked() error {
	raw, err := s.manager.ReadBlocklistManifest()
	if err != nil {
		return fmt.Errorf("read blocklist manifest: %w", err)
	}
	if len(raw) == 0 {
		s.cache = emptyManifest()
		s.loaded = true
		s.readOnly = false
		return nil
	}

	var m model.BlocklistManifest
	if err := json.Unmarshal(raw, &m); err != nil {
		log.Printf("[blocklistStore] manifest.json is corrupt/unparsable, quarantining and starting fresh: %v", err)
		if qerr := s.manager.QuarantineBlocklistManifest(); qerr != nil {
			log.Printf("[blocklistStore] failed to quarantine corrupt manifest: %v", qerr)
		}
		s.cache = emptyManifest()
		s.loaded = true
		s.readOnly = false
		return nil
	}

	if m.SchemaVersion > model.BlocklistManifestSchemaVersion {
		// Fail closed: a newer binary wrote this file using a schema we don't
		// understand. Keep it exactly as-is on disk (never persistLocked from
		// here on) and refuse further writes, rather than risk silently
		// dropping fields we don't know about.
		s.cache = m
		s.loaded = true
		s.readOnly = true
		return fmt.Errorf("blocklist manifest schemaVersion %d is newer than this binary supports (%d); refusing to read or write it", m.SchemaVersion, model.BlocklistManifestSchemaVersion)
	}

	// Normalize BlockMode on every list (plan §2.3 schema note / §3 T-03 item
	// 2b): a manifest written before the blockMode field existed has "" for
	// every list, which must become DNSBlockModeSinkhole (the blocklist
	// default — NOT DNSBlockModeNXDomain like the deny-list, see
	// model.NormalizeBlocklistBlockMode). A garbage/unknown value (hand-edited
	// file, future value this binary doesn't know) is clamped the same way,
	// with a warning logged — mirroring db/backup_repo.go's restore-time mode
	// clamp for the deny-list. A single list's bad mode must never fail the
	// whole manifest load.
	for i := range m.Lists {
		normalized, err := model.NormalizeBlocklistBlockMode(m.Lists[i].BlockMode)
		if err != nil {
			log.Printf("[blocklistStore] list %q has invalid blockMode %q, clamping to %q", m.Lists[i].ID, m.Lists[i].BlockMode, model.DNSBlocklistDefaultBlockMode)
			normalized = model.DNSBlocklistDefaultBlockMode
		}
		m.Lists[i].BlockMode = normalized
	}

	s.cache = m
	s.loaded = true
	s.readOnly = false
	return nil
}

// persistLocked marshals the current cache and writes it via the kernel
// layer. Callers must already hold the write lock. Refuses to write at all
// if readOnly is set (a newer-than-known schemaVersion was loaded).
func (s *blocklistStore) persistLocked() error {
	if s.readOnly {
		return fmt.Errorf("blocklist manifest is read-only (schemaVersion %d is newer than this binary supports)", s.cache.SchemaVersion)
	}

	s.cache.SchemaVersion = model.BlocklistManifestSchemaVersion
	s.cache.UpdatedAt = time.Now().UTC().Format(time.RFC3339)

	// Deterministic ordering so repeated marshals of unchanged data produce
	// identical bytes (useful for tests and for not needlessly wearing the
	// SD card with no-op-content writes).
	sort.SliceStable(s.cache.Lists, func(i, j int) bool {
		return s.cache.Lists[i].CreatedAt < s.cache.Lists[j].CreatedAt
	})

	raw, err := json.MarshalIndent(s.cache, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal blocklist manifest: %w", err)
	}
	if err := s.manager.WriteBlocklistManifest(raw); err != nil {
		return fmt.Errorf("write blocklist manifest: %w", err)
	}
	return nil
}

// ensureLoadedLocked lazily loads the manifest the first time it is needed,
// so a caller that forgets to call Load() explicitly still works. Callers
// must already hold the write lock.
func (s *blocklistStore) ensureLoadedLocked() error {
	if s.loaded {
		return nil
	}
	return s.loadLocked()
}

// List returns a copy of every list currently in the manifest — read-only,
// served entirely from the in-RAM cache (no disk access), per plan §2.3 item
// 4 (reduce SD wear). The returned slice (and each element, which is a
// plain struct with no pointer/slice fields) is a copy; mutating it never
// affects the store's internal cache.
func (s *blocklistStore) List() []model.DNSBlocklist {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]model.DNSBlocklist, len(s.cache.Lists))
	copy(out, s.cache.Lists)
	return out
}

// Get returns a copy of the list with the given id, or ok=false if it does
// not exist. Served from the in-RAM cache like List().
func (s *blocklistStore) Get(id string) (model.DNSBlocklist, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, l := range s.cache.Lists {
		if l.ID == id {
			return l, true
		}
	}
	return model.DNSBlocklist{}, false
}

// Add inserts a new list and persists the manifest. Returns an error
// (without mutating the cache) if a list with the same ID already exists —
// callers are expected to have generated a fresh, validated ID already.
func (s *blocklistStore) Add(l model.DNSBlocklist) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoadedLocked(); err != nil {
		return err
	}
	for _, existing := range s.cache.Lists {
		if existing.ID == l.ID {
			return fmt.Errorf("blocklist id %q already exists", l.ID)
		}
	}
	s.cache.Lists = append(s.cache.Lists, l)
	if err := s.persistLocked(); err != nil {
		// Roll back the in-RAM change so the cache never claims a list exists
		// that failed to persist.
		s.cache.Lists = s.cache.Lists[:len(s.cache.Lists)-1]
		return err
	}
	return nil
}

// Update looks up the list with id, calls mutate on a pointer to a working
// copy, and — if mutate returns nil — persists the mutated manifest. The
// entire lookup-mutate-persist sequence runs under the single write lock
// (plan §3 T-03 item 3: "ถือ write lock คลุมทั้ง read-modify-write"), so two
// concurrent Update calls for different (or the same) ids can never
// interleave. If mutate returns an error, or id does not exist, the cache is
// left untouched and that error is returned.
func (s *blocklistStore) Update(id string, mutate func(*model.DNSBlocklist) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoadedLocked(); err != nil {
		return err
	}
	idx := -1
	for i, l := range s.cache.Lists {
		if l.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return fmt.Errorf("blocklist id %q not found", id)
	}

	before := s.cache.Lists[idx]
	working := before
	if err := mutate(&working); err != nil {
		return err
	}
	s.cache.Lists[idx] = working
	if err := s.persistLocked(); err != nil {
		s.cache.Lists[idx] = before
		return err
	}
	return nil
}

// Remove deletes the list with id and persists the manifest. Removing a
// nonexistent id is not an error (idempotent — mirrors the kernel layer's
// RemoveBlocklistFile semantics for a missing file).
func (s *blocklistStore) Remove(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoadedLocked(); err != nil {
		return err
	}
	idx := -1
	for i, l := range s.cache.Lists {
		if l.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		return nil
	}
	before := append([]model.DNSBlocklist(nil), s.cache.Lists...)
	s.cache.Lists = append(s.cache.Lists[:idx], s.cache.Lists[idx+1:]...)
	if err := s.persistLocked(); err != nil {
		s.cache.Lists = before
		return err
	}
	return nil
}

// Toggle flips Enabled for the list with id and persists the manifest.
func (s *blocklistStore) Toggle(id string) error {
	return s.Update(id, func(l *model.DNSBlocklist) error {
		l.Enabled = !l.Enabled
		return nil
	})
}

// ReplaceAll overwrites the entire list set (used by backup/restore import,
// T-09) and persists the manifest. lists is copied before being stored so
// the caller's slice can be freely reused/mutated afterward.
func (s *blocklistStore) ReplaceAll(lists []model.DNSBlocklist) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := s.ensureLoadedLocked(); err != nil {
		return err
	}
	before := s.cache.Lists
	s.cache.Lists = append([]model.DNSBlocklist(nil), lists...)
	if err := s.persistLocked(); err != nil {
		s.cache.Lists = before
		return err
	}
	return nil
}
