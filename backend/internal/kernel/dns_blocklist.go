//go:build linux

package kernel

import (
	"bufio"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"pigate/internal/model"
)

// DNS blocklist import — kernel-layer file I/O (docs/ref/todo/
// dns-blocklist-import-plan.md §2.1–§2.3, T-02). This file is the ONLY place
// in the codebase (besides mock.go's in-memory equivalent) that touches
// /var/lib/pigate/blocklists on disk; the service layer never calls os.*
// directly for this feature (plan §2.3 item 5).
//
// SECURITY NOTE: blocklistDir MUST NEVER be moved under /etc/dnsmasq.d.
// dnsmasq auto-scans /etc/dnsmasq.d for *.conf files, so a .conf sitting
// there is loaded unconditionally regardless of whether its owning list is
// "enabled" in our manifest. Keeping this directory outside dnsmasq's
// conf-dir means the ONLY way any file here is ever loaded by dnsmasq is via
// an explicit addn-hosts=/conf-file= directive that ApplyZones itself
// constructs (dns_server.go's resolveBlocklistDirectives), after confirming
// the referenced list is actually enabled.
// blocklistDir is a var (not const) purely so tests in this package can
// point it at a t.TempDir() instead of the real /var/lib/pigate/blocklists —
// production code must never reassign it.
var blocklistDir = "/var/lib/pigate/blocklists"

func blocklistManifestPath() string {
	return filepath.Join(blocklistDir, "manifest.json")
}

func blocklistHostsPath(id string) string {
	return filepath.Join(blocklistDir, id+".hosts")
}

func blocklistConfPath(id string) string {
	return filepath.Join(blocklistDir, id+".conf")
}

// atomicWrite writes content to path without ever leaving a partially
// written file at path: create a temp file in the SAME directory (so the
// final rename is on the same filesystem, hence atomic) → Write → Sync →
// Chmod(0644) → Rename. Any error along the way removes the temp file
// rather than leaving it behind. Shared by the .hosts/.conf/manifest write
// paths (plan §3 T-02 item 2 / §2.2 / §2.3 item 2).
func atomicWrite(path string, content []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create blocklist directory %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp file in %s: %w", dir, err)
	}
	tmpPath := tmp.Name()
	renamed := false
	defer func() {
		if !renamed {
			os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(content); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write temp file %s: %w", tmpPath, err)
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to sync temp file %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp file %s: %w", tmpPath, err)
	}
	if err := os.Chmod(tmpPath, 0644); err != nil {
		return fmt.Errorf("failed to chmod temp file %s: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("failed to rename %s to %s: %w", tmpPath, path, err)
	}
	renamed = true
	return nil
}

// removeIfExists deletes path, treating "already gone" as success (several
// of the interface methods below are documented as "missing file is not an
// error").
func removeIfExists(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to remove %s: %w", path, err)
	}
	return nil
}

// WriteBlocklistFile atomically (over)writes <id>.hosts. id is validated
// FIRST (model.ValidateDNSBlocklistID, ^bl-[a-z0-9]{1,32}$) since it is
// external input concatenated directly into a filesystem path — this is not
// optional defense-in-depth, it is the only thing standing between an
// attacker-influenced id and path traversal.
func (m *RealDNSServerManager) WriteBlocklistFile(id string, content []byte) error {
	if err := model.ValidateDNSBlocklistID(id); err != nil {
		return err
	}
	return atomicWrite(blocklistHostsPath(id), content)
}

// WriteBlocklistConfFile atomically (over)writes <id>.conf (BlockMode
// nxdomain only). Same id validation requirement as WriteBlocklistFile.
func (m *RealDNSServerManager) WriteBlocklistConfFile(id string, content []byte) error {
	if err := model.ValidateDNSBlocklistID(id); err != nil {
		return err
	}
	return atomicWrite(blocklistConfPath(id), content)
}

// RemoveBlocklistFile deletes both <id>.hosts and <id>.conf. It always
// attempts both removals (does not short-circuit on the first error) so a
// stray .conf never survives a failed .hosts removal or vice versa.
func (m *RealDNSServerManager) RemoveBlocklistFile(id string) error {
	if err := model.ValidateDNSBlocklistID(id); err != nil {
		return err
	}
	errHosts := removeIfExists(blocklistHostsPath(id))
	errConf := removeIfExists(blocklistConfPath(id))
	if errHosts != nil {
		return errHosts
	}
	return errConf
}

// RemoveBlocklistConfFile deletes only <id>.conf — used when a list
// switches from nxdomain back to sinkhole mode, so the now-unused derived
// file is not left orphaned on disk.
func (m *RealDNSServerManager) RemoveBlocklistConfFile(id string) error {
	if err := model.ValidateDNSBlocklistID(id); err != nil {
		return err
	}
	return removeIfExists(blocklistConfPath(id))
}

// BlocklistFileInfo reports the size/existence of <id>.hosts. An invalid id
// or a missing file both report exists=false — callers needing to
// distinguish "bad id" from "not written yet" must validate id themselves.
func (m *RealDNSServerManager) BlocklistFileInfo(id string) (int64, bool) {
	return blocklistFileInfo(blocklistHostsPath(id), id)
}

// BlocklistConfFileInfo is BlocklistFileInfo for <id>.conf.
func (m *RealDNSServerManager) BlocklistConfFileInfo(id string) (int64, bool) {
	return blocklistFileInfo(blocklistConfPath(id), id)
}

func blocklistFileInfo(path, id string) (int64, bool) {
	if err := model.ValidateDNSBlocklistID(id); err != nil {
		return 0, false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return 0, false
	}
	return fi.Size(), true
}

// StreamBlocklistFile reads <id>.hosts line-by-line, invoking fn once per
// line — used to rebuild the statistics index and to re-render <id>.conf on
// a mode switch, both cases where holding the whole (potentially 90k+ line)
// file as a single []byte/[]string would be wasteful. A missing file is not
// an error (fn is simply never invoked).
func (m *RealDNSServerManager) StreamBlocklistFile(id string, fn func(line string) error) error {
	if err := model.ValidateDNSBlocklistID(id); err != nil {
		return err
	}
	f, err := os.Open(blocklistHostsPath(id))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to open blocklist file for %q: %w", id, err)
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	scanner.Buffer(make([]byte, 0, model.DNSBlocklistMaxLineBytes), model.DNSBlocklistMaxLineBytes)
	for scanner.Scan() {
		if err := fn(scanner.Text()); err != nil {
			return err
		}
	}
	return scanner.Err()
}

// ReadBlocklistManifest reads manifest.json's raw bytes. A missing file
// returns (nil, nil) — not an error — per plan §2.3 item 3 ("ไฟล์หาย =
// manifest ว่าง ... ไม่ใช่ error").
func (m *RealDNSServerManager) ReadBlocklistManifest() ([]byte, error) {
	data, err := os.ReadFile(blocklistManifestPath())
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read blocklist manifest: %w", err)
	}
	return data, nil
}

// WriteBlocklistManifest atomically (over)writes manifest.json.
func (m *RealDNSServerManager) WriteBlocklistManifest(content []byte) error {
	return atomicWrite(blocklistManifestPath(), content)
}

// QuarantineBlocklistManifest renames a corrupt/unparsable manifest.json to
// manifest.json.corrupt-<unix-seconds> so the store can start a fresh, empty
// manifest instead of the feature being permanently stuck on a JSON parse
// error (plan §2.3 item 3). Nothing to quarantine is not an error.
func (m *RealDNSServerManager) QuarantineBlocklistManifest() error {
	dest := fmt.Sprintf("%s.corrupt-%d", blocklistManifestPath(), time.Now().Unix())
	if err := os.Rename(blocklistManifestPath(), dest); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to quarantine corrupt blocklist manifest: %w", err)
	}
	log.Printf("[DNS Blocklist] Quarantined corrupt manifest.json to %s", dest)
	return nil
}

// --- dnsmasq version probe (plan §2.1.5) ---

// reDnsmasqVersion extracts the "X.Y" from dnsmasq --version's first line,
// e.g. "Dnsmasq version 2.89, Copyright (c) 2000-2022 Simon Kelley".
var reDnsmasqVersion = regexp.MustCompile(`(?i)dnsmasq\s+version\s+(\d+)\.(\d+)`)

var (
	dnsmasqVersionOnce          sync.Once
	dnsmasqSupportsBulkNXDomain bool
)

// SupportsBulkNXDomain runs `dnsmasq --version` (fixed argument, no user
// input whatsoever — the exact same no-shell-injection shape as
// validateDnsmasqConfig's `dnsmasq --test`, so this does not introduce a new
// class of exec.Command usage) exactly once per process (cached via
// sync.Once — plan §3 T-02 item 7: "ห้ามยิงทุกครั้งที่ Apply") and reports
// whether the detected version is >= 2.86, the release that made
// address=/domain/-style lookups scale to tens of thousands of entries (plan
// §2.1.5).
//
// Fail-open by design: if dnsmasq can't be run at all, or its version string
// can't be parsed, this returns true (assume supported) rather than false.
// The worst outcome of a wrong "true" is dnsmasq being slower than expected
// on an old board; the worst outcome of a wrong "false" is a perfectly fine
// board being unable to use nxdomain-mode blocklists at all — the plan
// explicitly picks the former as the safer failure mode.
func (m *RealDNSServerManager) SupportsBulkNXDomain() bool {
	dnsmasqVersionOnce.Do(func() {
		dnsmasqSupportsBulkNXDomain = detectDnsmasqSupportsBulkNXDomain()
	})
	return dnsmasqSupportsBulkNXDomain
}

func detectDnsmasqSupportsBulkNXDomain() bool {
	cmd := exec.Command("dnsmasq", "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("[DNS Blocklist] Warning: could not run `dnsmasq --version` (%v) — assuming nxdomain-mode blocklists are supported (fail-open, plan §2.1.5)", err)
		return true
	}

	match := reDnsmasqVersion.FindStringSubmatch(string(output))
	if match == nil {
		log.Printf("[DNS Blocklist] Warning: could not parse dnsmasq version from %q — assuming nxdomain-mode blocklists are supported (fail-open, plan §2.1.5)", strings.TrimSpace(string(output)))
		return true
	}

	major, errMajor := strconv.Atoi(match[1])
	minor, errMinor := strconv.Atoi(match[2])
	if errMajor != nil || errMinor != nil {
		log.Printf("[DNS Blocklist] Warning: could not parse dnsmasq version numbers from %q — assuming nxdomain-mode blocklists are supported (fail-open, plan §2.1.5)", strings.TrimSpace(string(output)))
		return true
	}

	supported := major > 2 || (major == 2 && minor >= 86)
	if !supported {
		log.Printf("[DNS Blocklist] dnsmasq version %d.%d detected (< 2.86) — nxdomain-mode blocklists are not supported on this host (plan §2.1.5)", major, minor)
	}
	return supported
}
