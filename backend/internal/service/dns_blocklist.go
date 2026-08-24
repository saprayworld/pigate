// Package service: DNSBlocklistService — the business-logic layer for the
// DNS blocklist import feature (docs/ref/todo/dns-blocklist-import-plan.md
// §3 T-05). It composes the pieces built by the earlier tasks: blocklistStore
// (T-03, JSON manifest metadata), blocklistFetcher (T-04, SSRF-guarded HTTP
// fetch) and model's pure parse/render functions (T-01) — this file itself
// does no raw file I/O (that stays in kernel.DNSServerManager) and no direct
// SQLite writes (repo is read-only here, used solely to build the "never
// block this device's own zones" exclude set).
package service

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// DNSBlocklistService is the service-layer entry point for creating,
// refreshing, deleting, toggling and switching the blockMode of subscribed/
// uploaded blocklists. repo is used strictly READ-ONLY here (GetDNSZones,
// for the ingest exclude set) — the blocklist feature itself intentionally
// never touches SQLite (plan §2.3/R1); all of its own state lives in store.
type DNSBlocklistService struct {
	store   *blocklistStore
	manager kernel.DNSServerManager
	fetcher *blocklistFetcher
	repo    *db.Repository

	// applyDNS is optional (nil until SetApplyDNSCallback is called by
	// main.go) — wired to (*DNSServerService).ApplyAll, in the OPPOSITE
	// direction of blocklistProvider (dns_server.go's DNSServerService holds
	// a reference to this service so ApplyAll can read the manifest;
	// DNSBlocklistService needs the reverse reference too, so Delete/
	// UpdateInfo can regenerate /etc/dnsmasq.d/pigate-dns.conf BEFORE
	// removing a <id>.hosts/<id>.conf file still referenced by it — see the
	// stale-directive-window fix below in Delete/UpdateInfo. Mirrors the
	// setter pattern used by SetBlockedDomainsSink/SetBlocklistProvider/
	// SetBlocklistSink. Safe to leave unset (nil): Delete/UpdateInfo then
	// fall back to removing files immediately, matching this feature's
	// pre-fix behavior — used by unit tests that construct
	// DNSBlocklistService directly, without a DNSServerService to apply.
	applyDNS func() error
}

// NewDNSBlocklistService constructs the service. Callers (main.go) must call
// Load() once at startup before relying on List()/ApplyAll wiring returning
// anything meaningful — mirrors blocklistStore's own Load() contract.
func NewDNSBlocklistService(repo *db.Repository, manager kernel.DNSServerManager) *DNSBlocklistService {
	return &DNSBlocklistService{
		store:   newBlocklistStore(manager),
		manager: manager,
		fetcher: newBlocklistFetcher(),
		repo:    repo,
	}
}

// Load reads the on-disk manifest into memory (delegates to blocklistStore.Load).
func (s *DNSBlocklistService) Load() error {
	return s.store.Load()
}

// List returns every blocklist currently in the manifest (a copy — see
// blocklistStore.List).
func (s *DNSBlocklistService) List() []model.DNSBlocklist {
	return s.store.List()
}

// Get returns a single blocklist by id.
func (s *DNSBlocklistService) Get(id string) (model.DNSBlocklist, bool) {
	return s.store.Get(id)
}

// SetApplyDNSCallback wires the function Delete/UpdateInfo call to regenerate
// /etc/dnsmasq.d/pigate-dns.conf BEFORE actually removing a blocklist's
// <id>.hosts/<id>.conf file — see the applyDNS field doc above. In production
// this is main.go's dnsServerService.ApplyAll. Must be called before any
// Delete/UpdateInfo that removes a list/switches away from nxdomain mode, to
// avoid the stale-directive window; main.go wires it right after
// constructing both services, well before either is reachable from an HTTP
// request.
func (s *DNSBlocklistService) SetApplyDNSCallback(fn func() error) {
	s.applyDNS = fn
}

// newBlocklistID mints a fresh manifest id — crypto/rand, prefixed "bl-"
// (mirrors api/handlers.go's randomID("prefix-") pattern, duplicated here
// rather than imported since api depends on service, not the other way
// around). 16 random bytes hex-encode to exactly 32 lower-case hex
// characters, which is both well clear of any practical collision risk and
// exactly at reBlocklistID's {1,32} upper bound — ValidateDNSBlocklistID is
// still checked explicitly below rather than assumed.
func newBlocklistID() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate blocklist id: %w", err)
	}
	id := "bl-" + hex.EncodeToString(b)
	if err := model.ValidateDNSBlocklistID(id); err != nil {
		return "", err
	}
	return id, nil
}

// excludeSet builds the set of domain names ingest() must never accept into
// a blocklist, regardless of what a subscribed/uploaded source file says:
// this device's own enabled DNS zones (blocking them would break local
// resolution), the fixed "pigate.local" zone, and the device's current
// hostname. Matching against this set is label-boundary (subdomains are
// excluded too), via model.ParseHostsBlocklist -> blocklistExcludeMatch.
func (s *DNSBlocklistService) excludeSet() map[string]bool {
	exclude := map[string]bool{"pigate.local": true}
	if hostname, err := os.Hostname(); err == nil {
		hostname = strings.ToLower(strings.TrimSpace(hostname))
		if hostname != "" {
			exclude[hostname] = true
		}
	}
	if s.repo != nil {
		zones, err := s.repo.GetDNSZones()
		if err != nil {
			log.Printf("[DNSBlocklistService] Warning: could not read DNS zones for blocklist exclude set: %v", err)
		} else {
			for _, z := range zones {
				if !z.Enabled || z.ZoneName == "" {
					continue
				}
				exclude[strings.ToLower(z.ZoneName)] = true
			}
		}
	}
	return exclude
}

// blocklistIngestResult is what ingest() computes for the caller to persist
// into the manifest — deliberately NOT a model.DNSBlocklist (ingest has no
// business setting Name/URL/Enabled/timestamps; that stays with the caller).
type blocklistIngestResult struct {
	DomainCount int
	FileBytes   int64
	Sha256      string
}

// checkQuotas enforces plan §2.1.4/§3 T-05 item 2's two cross-list caps
// before a list is (re)written:
//   - model.DNSBlocklistMaxTotalDomains across every OTHER enabled list plus
//     candidateDomains (regardless of mode).
//   - when mode == DNSBlockModeNXDomain, model.DNSBlocklistMaxNXDomainDomains
//     across every OTHER enabled nxdomain-mode list plus candidateDomains.
//
// excludeID is the id of the list being created/updated (so its own
// currently-persisted DomainCount, if any, is not double-counted alongside
// candidateDomains). Disabled lists never contribute to either sum — they
// are not passed to kernel.ApplyZones (see DNSServerService.ApplyAll), so
// they carry no dnsmasq cost right now. If willBeEnabled is false, this list
// itself won't be enforced against dnsmasq once written, so no check is
// necessary at all (an operation can always leave a list disabled,
// regardless of size — it only matters once something asks for it to be
// enabled, be that CreateFromURL/CreateFromUpload/UpdateInfo/Toggle).
func (s *DNSBlocklistService) checkQuotas(excludeID, mode string, candidateDomains int, willBeEnabled bool) error {
	if !willBeEnabled {
		return nil
	}

	var totalAll, totalNX int
	for _, l := range s.store.List() {
		if l.ID == excludeID || !l.Enabled {
			continue
		}
		totalAll += l.DomainCount
		lm, err := model.NormalizeBlocklistBlockMode(l.BlockMode)
		if err == nil && lm == model.DNSBlockModeNXDomain {
			totalNX += l.DomainCount
		}
	}

	totalAll += candidateDomains
	if totalAll > model.DNSBlocklistMaxTotalDomains {
		return fmt.Errorf("enabled blocklists would total %d domains, exceeding the maximum of %d", totalAll, model.DNSBlocklistMaxTotalDomains)
	}

	if mode == model.DNSBlockModeNXDomain {
		totalNX += candidateDomains
		if totalNX > model.DNSBlocklistMaxNXDomainDomains {
			return fmt.Errorf("enabled nxdomain-mode blocklists would total %d domains, exceeding the maximum of %d (nxdomain-mode lists cost more per Apply — see docs/ref/todo/dns-blocklist-import-plan.md §2.1.3)", totalNX, model.DNSBlocklistMaxNXDomainDomains)
		}
	}
	return nil
}

// ingest is the shared pipeline behind CreateFromURL/CreateFromUpload/
// Refresh (plan §3 T-05 item 3): parse raw (a freshly fetched or uploaded
// hosts-format file) into a domain set, enforce quotas, render and write
// <id>.hosts (always) and <id>.conf (only for BlockMode == DNSBlockModeNXDomain
// — any stale <id>.conf from a previous mode is removed otherwise), and
// return the resulting metadata (domain count / file size / sha256 of the
// canonical .hosts content). id must already be model.ValidateDNSBlocklistID
// and mode must already be model.NormalizeBlocklistBlockMode'd by the
// caller. Nothing is written to the manifest here — that is the caller's
// job (store.Add/store.Update), specifically so a failed ingest never leaves
// a manifest entry pointing at files that were never written.
func (s *DNSBlocklistService) ingest(id string, raw []byte, mode string, willBeEnabled bool) (blocklistIngestResult, error) {
	var result blocklistIngestResult

	// (ก) plan §3 T-05 item 2: nxdomain mode always requires a new-enough
	// dnsmasq, regardless of whether this particular list will end up
	// enabled — a disabled nxdomain-mode list is still recorded as
	// nxdomain-mode and could be enabled later without going through this
	// check again (UpdateInfo/Toggle only re-checks quotas, not version
	// support, when blockMode itself isn't changing).
	if mode == model.DNSBlockModeNXDomain && !s.manager.SupportsBulkNXDomain() {
		return result, fmt.Errorf("blockMode %q requires dnsmasq >= 2.86, which this system's dnsmasq does not support", model.DNSBlockModeNXDomain)
	}

	domains, _, err := model.ParseHostsBlocklist(bytes.NewReader(raw), s.excludeSet())
	if err != nil {
		return result, err
	}

	// (ข) the cross-list domain-count quotas.
	if err := s.checkQuotas(id, mode, len(domains), willBeEnabled); err != nil {
		return result, err
	}

	now := time.Now().UTC()
	hostsContent := model.RenderHostsFile(id, domains, now)
	if err := s.manager.WriteBlocklistFile(id, hostsContent); err != nil {
		return result, fmt.Errorf("write blocklist hosts file: %w", err)
	}

	if mode == model.DNSBlockModeNXDomain {
		confContent := model.RenderBlocklistConfFile(id, domains, now)
		if err := s.manager.WriteBlocklistConfFile(id, confContent); err != nil {
			return result, fmt.Errorf("write blocklist conf file: %w", err)
		}
	} else {
		// Drop any stale <id>.conf left over from a previous nxdomain
		// setting — plan §2.1.1: "ห้ามปล่อยไฟล์ orphan ไว้".
		if err := s.manager.RemoveBlocklistConfFile(id); err != nil {
			return result, fmt.Errorf("remove stale blocklist conf file: %w", err)
		}
	}

	sum := sha256.Sum256(hostsContent)
	result.DomainCount = len(domains)
	result.FileBytes = int64(len(hostsContent))
	result.Sha256 = hex.EncodeToString(sum[:])
	return result, nil
}

// streamDomains re-derives the domain list for id purely from its
// already-written <id>.hosts (never from raw/fetched bytes), by bridging
// manager.StreamBlocklistFile's line-callback shape into
// model.ParseHostsFileDomains' io.Reader shape via an io.Pipe — this is the
// SAME parser used for the statistics index (T-06) and by ingest's own
// callers when re-deriving domains, per plan §3 T-05 item 2 ("ห้ามเขียนซ้ำ").
// The whole file is never buffered in memory at once: fn is invoked
// domain-by-domain as StreamBlocklistFile produces lines.
//
// fn must not return a non-nil error from a goroutine-unsafe context; more
// importantly, if fn returns an error, the pipe reader is closed with that
// error so the writer side (StreamBlocklistFile's callback) unblocks and
// stops promptly instead of deadlocking on a pipe nobody is draining anymore.
func (s *DNSBlocklistService) streamDomains(id string, fn func(domain string) error) error {
	pr, pw := io.Pipe()
	parseDone := make(chan error, 1)
	go func() {
		err := model.ParseHostsFileDomains(pr, fn)
		_ = pr.CloseWithError(err)
		parseDone <- err
	}()

	streamErr := s.manager.StreamBlocklistFile(id, func(line string) error {
		_, werr := pw.Write([]byte(line + "\n"))
		return werr
	})
	_ = pw.CloseWithError(streamErr)

	parseErr := <-parseDone
	if streamErr != nil {
		return streamErr
	}
	return parseErr
}

// renderArtifacts switches list id's derived <id>.conf artifact to match
// newMode WITHOUT re-fetching or re-uploading anything (plan §2.1.1/§2.7):
// domains are streamed back out of the already-written <id>.hosts (the
// canonical store) via streamDomains, so this works fully offline and for
// upload-sourced lists that can never be re-fetched at all.
func (s *DNSBlocklistService) renderArtifacts(id, newMode string) error {
	if newMode != model.DNSBlockModeNXDomain {
		return s.manager.RemoveBlocklistConfFile(id)
	}

	var domains []string
	if err := s.streamDomains(id, func(domain string) error {
		domains = append(domains, domain)
		return nil
	}); err != nil {
		return fmt.Errorf("re-read blocklist %q domains from %s.hosts: %w", id, id, err)
	}

	confContent := model.RenderBlocklistConfFile(id, domains, time.Now().UTC())
	if err := s.manager.WriteBlocklistConfFile(id, confContent); err != nil {
		return fmt.Errorf("write blocklist conf file: %w", err)
	}
	return nil
}

// CreateFromURL subscribes to a new URL-sourced blocklist: fetches it now
// (SSRF-guarded, T-04), parses/renders/writes it (ingest), and only THEN
// adds it to the manifest — a fetch/parse failure never leaves a manifest
// row pointing at files that were never written (plan §3 T-05 item 2).
func (s *DNSBlocklistService) CreateFromURL(ctx context.Context, name, rawURL, blockMode string, enabled bool) (model.DNSBlocklist, error) {
	if err := model.ValidateDNSBlocklistName(name); err != nil {
		return model.DNSBlocklist{}, err
	}
	if err := model.ValidateDNSBlocklistURL(rawURL); err != nil {
		return model.DNSBlocklist{}, err
	}
	mode, err := model.NormalizeBlocklistBlockMode(blockMode)
	if err != nil {
		return model.DNSBlocklist{}, err
	}
	if len(s.store.List()) >= model.DNSBlocklistsMax {
		return model.DNSBlocklist{}, fmt.Errorf("maximum of %d blocklists already reached", model.DNSBlocklistsMax)
	}

	id, err := newBlocklistID()
	if err != nil {
		return model.DNSBlocklist{}, err
	}

	raw, err := s.fetcher.Fetch(ctx, rawURL)
	if err != nil {
		return model.DNSBlocklist{}, fmt.Errorf("fetch blocklist: %w", err)
	}

	ingested, err := s.ingest(id, raw, mode, enabled)
	if err != nil {
		return model.DNSBlocklist{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	entry := model.DNSBlocklist{
		ID:            id,
		Name:          name,
		SourceType:    model.DNSBlocklistSourceURL,
		URL:           rawURL,
		BlockMode:     mode,
		Enabled:       enabled,
		DomainCount:   ingested.DomainCount,
		FileBytes:     ingested.FileBytes,
		Sha256:        ingested.Sha256,
		LastFetchedAt: now,
		CreatedAt:     now,
	}
	if err := s.store.Add(entry); err != nil {
		s.cleanupOrphanFiles(id)
		return model.DNSBlocklist{}, err
	}
	return entry, nil
}

// CreateFromUpload adds a new upload-sourced blocklist from raw file bytes
// already read by the HTTP handler (T-08) — no network access here at all.
func (s *DNSBlocklistService) CreateFromUpload(name string, raw []byte, blockMode string, enabled bool) (model.DNSBlocklist, error) {
	if err := model.ValidateDNSBlocklistName(name); err != nil {
		return model.DNSBlocklist{}, err
	}
	mode, err := model.NormalizeBlocklistBlockMode(blockMode)
	if err != nil {
		return model.DNSBlocklist{}, err
	}
	if len(raw) > model.DNSBlocklistMaxFileBytes {
		return model.DNSBlocklist{}, fmt.Errorf("uploaded blocklist exceeds maximum of %d bytes", model.DNSBlocklistMaxFileBytes)
	}
	if len(s.store.List()) >= model.DNSBlocklistsMax {
		return model.DNSBlocklist{}, fmt.Errorf("maximum of %d blocklists already reached", model.DNSBlocklistsMax)
	}

	id, err := newBlocklistID()
	if err != nil {
		return model.DNSBlocklist{}, err
	}

	ingested, err := s.ingest(id, raw, mode, enabled)
	if err != nil {
		return model.DNSBlocklist{}, err
	}

	now := time.Now().UTC().Format(time.RFC3339)
	entry := model.DNSBlocklist{
		ID:          id,
		Name:        name,
		SourceType:  model.DNSBlocklistSourceUpload,
		BlockMode:   mode,
		Enabled:     enabled,
		DomainCount: ingested.DomainCount,
		FileBytes:   ingested.FileBytes,
		Sha256:      ingested.Sha256,
		CreatedAt:   now,
	}
	if err := s.store.Add(entry); err != nil {
		s.cleanupOrphanFiles(id)
		return model.DNSBlocklist{}, err
	}
	return entry, nil
}

// cleanupOrphanFiles best-effort removes <id>.hosts/<id>.conf after ingest
// succeeded but store.Add/store.Update failed to persist the manifest entry
// pointing at them (an astronomically unlikely race, e.g. a duplicate
// crypto/rand id) — the id is discarded either way, so leaving the files
// behind would only be silent disk waste, never a security issue (they are
// not referenced by any manifest entry, so ApplyAll's kernel.ApplyZones
// loop, which only ever iterates the manifest, will never reference them).
func (s *DNSBlocklistService) cleanupOrphanFiles(id string) {
	if err := s.manager.RemoveBlocklistFile(id); err != nil {
		log.Printf("[DNSBlocklistService] cleanup after failed store write for %q: %v", id, err)
	}
}

// Refresh re-fetches a URL-sourced list's content and re-ingests it. A fetch
// (or subsequent parse/quota) failure leaves the existing <id>.hosts/<id>.conf
// and DomainCount completely untouched — only LastError is updated — so a
// transient network problem never takes down a list that is currently
// working (plan §3 T-05 item 2: "Refresh ... คงไฟล์เดิมและ domainCount เดิมไว้").
func (s *DNSBlocklistService) Refresh(ctx context.Context, id string) (model.DNSBlocklist, error) {
	existing, ok := s.store.Get(id)
	if !ok {
		return model.DNSBlocklist{}, fmt.Errorf("blocklist id %q not found", id)
	}
	if existing.SourceType != model.DNSBlocklistSourceURL {
		return model.DNSBlocklist{}, fmt.Errorf("blocklist id %q is not a subscribe-URL list (sourceType=%q), cannot refresh", id, existing.SourceType)
	}
	mode, err := model.NormalizeBlocklistBlockMode(existing.BlockMode)
	if err != nil {
		mode = model.DNSBlocklistDefaultBlockMode
	}

	raw, fetchErr := s.fetcher.Fetch(ctx, existing.URL)
	if fetchErr != nil {
		return s.recordRefreshError(id, fmt.Errorf("fetch blocklist: %w", fetchErr))
	}

	ingested, ingestErr := s.ingest(id, raw, mode, existing.Enabled)
	if ingestErr != nil {
		return s.recordRefreshError(id, ingestErr)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	var result model.DNSBlocklist
	err = s.store.Update(id, func(l *model.DNSBlocklist) error {
		l.DomainCount = ingested.DomainCount
		l.FileBytes = ingested.FileBytes
		l.Sha256 = ingested.Sha256
		l.LastFetchedAt = now
		l.LastError = ""
		result = *l
		return nil
	})
	if err != nil {
		return model.DNSBlocklist{}, err
	}
	return result, nil
}

// recordRefreshError persists refreshErr into the list's LastError field
// (leaving every other field, including DomainCount/Sha256/the underlying
// files, exactly as they were) and returns the resulting entry alongside the
// original error, so the caller/HTTP handler can surface both "here's the
// current (still valid) list" and "here's why the refresh itself failed".
func (s *DNSBlocklistService) recordRefreshError(id string, refreshErr error) (model.DNSBlocklist, error) {
	var result model.DNSBlocklist
	if err := s.store.Update(id, func(l *model.DNSBlocklist) error {
		l.LastError = refreshErr.Error()
		result = *l
		return nil
	}); err != nil {
		return model.DNSBlocklist{}, err
	}
	return result, refreshErr
}

// Delete removes a blocklist's manifest entry FIRST, then (if wired) applies
// so pigate-dns.conf is regenerated without any conf-file=/addn-hosts=
// directive for id, and only THEN removes the underlying <id>.hosts/
// <id>.conf files.
//
// This ordering fixes a stale-directive window found at final tech-lead
// sign-off (docs/ref/todo/dns-blocklist-import-plan.md, same class of
// problem as Caution 16/issue #50): the previous "remove files, then
// manifest entry" order left pigate-dns.conf pointing at a just-deleted file
// for as long as the user hadn't clicked "Apply DNS" again — if dnsmasq got
// restarted for any OTHER reason in that window (e.g. Apply DHCP settings in
// dhcp_server.go, or a crash/reboot), dnsmasq would refuse to start at all
// (a missing conf-file=/addn-hosts= target is fatal to dnsmasq), taking down
// DNS *and* DHCP together.
//
// store.Remove already only mutates/persists the manifest after confirming
// the write succeeds (rolling back the in-RAM cache otherwise), so a failure
// there leaves everything — manifest and files alike — completely untouched.
// If applyDNS itself fails, the files are deliberately left in place: the
// on-disk pigate-dns.conf was either not rewritten at all (still valid,
// still pointing at files that still exist) or was rewritten but dnsmasq
// failed to restart (worth investigating, but again the files these old
// directives might still reference are still present) — either way it is
// never safe to remove the files at that point. Removing the manifest entry
// first when applyDNS then fails means the list disappears from the UI/API
// even though its files linger on disk; that's a strictly better failure
// mode than a full DNS+DHCP outage, and Delete can simply be retried (files
// are removed idempotently) or the operator can investigate why apply failed.
//
// If applyDNS is unset (nil — e.g. unit tests that construct
// DNSBlocklistService directly without a DNSServerService), this falls back
// to removing the files right after the manifest entry, matching this
// feature's original behavior — nothing outside this feature would be able
// to regenerate pigate-dns.conf in that case anyway.
func (s *DNSBlocklistService) Delete(id string) error {
	if err := s.store.Remove(id); err != nil {
		return fmt.Errorf("remove blocklist manifest entry: %w", err)
	}

	if s.applyDNS != nil {
		if err := s.applyDNS(); err != nil {
			return fmt.Errorf("apply DNS configuration after removing blocklist %q from the manifest: %w", id, err)
		}
	}

	if err := s.manager.RemoveBlocklistFile(id); err != nil {
		return fmt.Errorf("remove blocklist files: %w", err)
	}
	return nil
}

// Toggle flips a list's Enabled flag. Enabling a currently-disabled list is
// re-checked against the cross-list domain-count quotas first (a list that
// was allowed to exist while disabled may no longer fit once it starts
// counting toward what gets applied to dnsmasq) — disabling never needs any
// check.
func (s *DNSBlocklistService) Toggle(id string) (model.DNSBlocklist, error) {
	existing, ok := s.store.Get(id)
	if !ok {
		return model.DNSBlocklist{}, fmt.Errorf("blocklist id %q not found", id)
	}
	if !existing.Enabled {
		mode, err := model.NormalizeBlocklistBlockMode(existing.BlockMode)
		if err != nil {
			mode = model.DNSBlocklistDefaultBlockMode
		}
		if err := s.checkQuotas(id, mode, existing.DomainCount, true); err != nil {
			return model.DNSBlocklist{}, err
		}
	}
	if err := s.store.Toggle(id); err != nil {
		return model.DNSBlocklist{}, err
	}
	updated, _ := s.store.Get(id)
	return updated, nil
}

// UpdateInfo updates a list's Name/URL/BlockMode/Enabled metadata. It NEVER
// re-fetches or re-parses source content — when blockMode changes, the
// derived <id>.conf artifact is regenerated purely from the already-written
// <id>.hosts via renderArtifacts (plan §3 T-05 item 2: "UpdateInfo ที่เปลี่ยน
// เฉพาะ blockMode ต้องไม่ fetch ใหม่"), which is what lets mode-switching work
// offline and for upload-sourced lists.
//
// The two mode-switch directions are handled with different ordering
// relative to the manifest write, to close the same stale-directive window
// documented on Delete above:
//   - sinkhole -> nxdomain (renderArtifacts WRITES a new <id>.conf): safe to
//     do before the manifest is updated — pigate-dns.conf still says
//     "sinkhole" for this list until the next Apply, so it never references
//     the new file yet; adding a not-yet-referenced file can never create a
//     dangling directive.
//   - nxdomain -> sinkhole (removes <id>.conf, which pigate-dns.conf's
//     conf-file= directive currently still points at): the manifest is
//     updated FIRST, then (if wired) applyDNS regenerates pigate-dns.conf
//     using the new "sinkhole" mode (addn-hosts=, no reference to <id>.conf
//     at all), and only THEN is the now-unreferenced <id>.conf actually
//     removed from disk.
func (s *DNSBlocklistService) UpdateInfo(id, name, rawURL, blockMode string, enabled bool) (model.DNSBlocklist, error) {
	existing, ok := s.store.Get(id)
	if !ok {
		return model.DNSBlocklist{}, fmt.Errorf("blocklist id %q not found", id)
	}
	if err := model.ValidateDNSBlocklistName(name); err != nil {
		return model.DNSBlocklist{}, err
	}
	if existing.SourceType == model.DNSBlocklistSourceURL {
		if err := model.ValidateDNSBlocklistURL(rawURL); err != nil {
			return model.DNSBlocklist{}, err
		}
	}
	newMode, err := model.NormalizeBlocklistBlockMode(blockMode)
	if err != nil {
		return model.DNSBlocklist{}, err
	}

	modeChanged := newMode != existing.BlockMode
	becomingEnabled := enabled && !existing.Enabled
	switchingAwayFromNXDomain := modeChanged && existing.BlockMode == model.DNSBlockModeNXDomain

	if modeChanged || becomingEnabled {
		if newMode == model.DNSBlockModeNXDomain && !s.manager.SupportsBulkNXDomain() {
			return model.DNSBlocklist{}, fmt.Errorf("blockMode %q requires dnsmasq >= 2.86, which this system's dnsmasq does not support", model.DNSBlockModeNXDomain)
		}
		if err := s.checkQuotas(id, newMode, existing.DomainCount, enabled); err != nil {
			return model.DNSBlocklist{}, err
		}
	}

	if modeChanged && !switchingAwayFromNXDomain {
		// sinkhole -> nxdomain: safe to write the new .conf now, before the
		// manifest reflects the mode change (see doc comment above).
		if err := s.renderArtifacts(id, newMode); err != nil {
			return model.DNSBlocklist{}, err
		}
	}

	var result model.DNSBlocklist
	updateErr := s.store.Update(id, func(l *model.DNSBlocklist) error {
		l.Name = name
		if l.SourceType == model.DNSBlocklistSourceURL {
			l.URL = rawURL
		}
		l.BlockMode = newMode
		l.Enabled = enabled
		result = *l
		return nil
	})
	if updateErr != nil {
		return model.DNSBlocklist{}, updateErr
	}

	if switchingAwayFromNXDomain {
		// nxdomain -> sinkhole: the manifest now says "sinkhole" for id, so
		// apply (if wired) can safely regenerate pigate-dns.conf without a
		// conf-file= directive for it, and only then is the stale <id>.conf
		// removed from disk (see doc comment above / Delete's doc comment for
		// the full stale-directive-window rationale). If applyDNS fails, the
		// stale <id>.conf is deliberately left in place — the manifest
		// already says "sinkhole" but the on-disk pigate-dns.conf may still
		// be the old nxdomain-mode version (or apply may have failed before
		// rewriting it at all), so the file it might still reference must
		// not be removed yet.
		if s.applyDNS != nil {
			if err := s.applyDNS(); err != nil {
				return result, fmt.Errorf("apply DNS configuration after switching blocklist %q to sinkhole mode: %w", id, err)
			}
		}
		if err := s.manager.RemoveBlocklistConfFile(id); err != nil {
			return result, fmt.Errorf("remove stale blocklist conf file: %w", err)
		}
	}

	return result, nil
}
