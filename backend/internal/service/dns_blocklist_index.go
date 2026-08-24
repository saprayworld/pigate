package service

import (
	"hash/fnv"
	"io"
	"sort"
	"strings"
	"sync/atomic"

	"pigate/internal/kernel"
	"pigate/internal/model"
)

// dnsBlocklistIndex is the RAM-only matcher behind the "Blocked Query"
// statistics feature for bulk-imported blocklists (docs/ref/todo/
// dns-blocklist-import-plan.md §2.6/T-06) — the sibling of dnsBlockIndex
// (dns_block_index.go), which stays untouched and keeps serving the
// separate, ≤1000-row deny-list feature.
//
// Why a SEPARATE index instead of merging into dnsBlockIndex (plan §2.6
// item 1-2, Caution 9):
//   - Semantics differ PER LIST, not just per feature: the deny-list always
//     suffix-matches (every rule mirrors dnsmasq's address=/server=
//     "cover every subdomain" behavior). A blocklist entry, however,
//     matches one of two different ways depending on the list's own
//     BlockMode:
//   - BlockMode == model.DNSBlockModeSinkhole (rendered as
//     `addn-hosts=<id>.hosts`, a plain hosts file) — dnsmasq's hosts
//     parsing does NOT cover subdomains, so this index must EXACT-MATCH
//     only. A hosts file mapping ads.example.com does NOT make
//     sub.ads.example.com resolve to 0.0.0.0.
//   - BlockMode == model.DNSBlockModeNXDomain (rendered as
//     `conf-file=<id>.conf` containing `address=/<domain>/` lines) — per
//     dnsmasq's man page, `address=/<domain>/` with no IP returns
//     NXDOMAIN for the domain AND every subdomain of it (label-boundary
//     matching, same rule dnsBlockIndex already implements for the
//     deny-list). This index must SUFFIX-MATCH for these lists.
//     Getting this backwards (or applying one rule to both modes) makes the
//     Statistics page report blocks dnsmasq never actually performed, or
//     miss blocks it did — i.e. "the statistics would lie" (plan Caution 9).
//   - Scale differs by ~500x: the deny-list tops out at 1000 rows and is
//     cheaply stored as map[string]string; a blocklist snapshot can hold up
//     to model.DNSBlocklistMaxTotalDomains (500,000) domains across up to
//     model.DNSBlocklistsMax lists, so it needs a RAM-frugal structure (see
//     below) that dnsBlockIndex was never designed for.
//   - Lifecycle differs: the deny-list is fed straight from the DB
//     (DNSServerService.SetBlockedDomainsSink); a blocklist snapshot is fed
//     by STREAMING <id>.hosts files back off disk through the kernel layer
//     (StatisticsService.SetBlocklists), never held as a single big
//     []string.
//
// RAM layout: each domain contributes exactly one 8-byte FNV-1a 64-bit hash
// to its list's sorted []uint64 slice — never a string. At 500k domains this
// is ~4 MB (8 bytes × 500,000) versus ~30 MB for a map[string]string keyed
// by the domain text itself (plan §2.6 table). BlockMode is stored once PER
// LIST (at most model.DNSBlocklistsMax = 8 lists), not per domain, so
// tracking mode costs nothing extra at the domain level.
//
// Hash-collision risk: at 500,000 entries the chance of any FNV-1a 64-bit
// collision is on the order of 500000^2 / 2^65 ≈ 7e-9 (birthday bound). A
// collision would only ever cause a STATISTICS false-positive/negative (a
// query wrongly reported as blocked-by-list-X, or not reported at all) — it
// can never affect what dnsmasq itself resolves, since dnsmasq never
// consults this index; it is purely an after-the-fact classifier for the
// Statistics page.
//
// Privacy: Match, like dnsBlockIndex.Match, never logs the domain it was
// asked about.
type dnsBlocklistIndex struct {
	snap atomic.Pointer[blocklistSnapshot]
}

// blocklistEntry is one list's contribution to a blocklistSnapshot: its
// sorted hash set plus the identity/mode needed to answer Match.
type blocklistEntry struct {
	id, name string
	// mode is model.DNSBlockModeSinkhole (exact-match only) or
	// model.DNSBlockModeNXDomain (suffix-match, covers subdomains) — see the
	// dnsBlocklistIndex doc comment above for why the two differ.
	mode string
	// hashes holds one FNV-1a 64-bit hash per domain in this list, kept
	// SORTED so Match can binary-search it (sort.SearchInts-style, via
	// sort.Search below) without allocating.
	hashes []uint64
}

// blocklistSnapshot is one atomically-swapped, immutable view of every
// enabled blocklist's contents. hasNXDomain is precomputed once at Set time
// so Match's common case (no nxdomain-mode list configured at all — most
// installs only use the sinkhole default) can skip the parent-label walk
// entirely without re-scanning every list's mode on every query.
type blocklistSnapshot struct {
	lists       []blocklistEntry
	domainCount int
	hasNXDomain bool
}

// blocklistHash returns the FNV-1a 64-bit hash of a lower-cased domain.
// hash/fnv is stdlib, allocation-free for a single Sum64 call.
func blocklistHash(domain string) uint64 {
	h := fnv.New64a()
	_, _ = h.Write([]byte(domain))
	return h.Sum64()
}

// blocklistEntryContains reports whether hash is present in the (sorted)
// hashes slice, via binary search — O(log n), no allocation.
func blocklistEntryContains(hashes []uint64, hash uint64) bool {
	i := sort.Search(len(hashes), func(i int) bool { return hashes[i] >= hash })
	return i < len(hashes) && hashes[i] == hash
}

// Set atomically replaces the whole snapshot. entries should already be
// deduplicated/sorted per list by the caller (BuildBlocklistIndexEntry
// below does both); Set itself does not re-sort, to avoid doing that work
// twice when the caller already produced sorted output while streaming.
func (idx *dnsBlocklistIndex) Set(entries []blocklistEntry) {
	next := &blocklistSnapshot{lists: entries}
	for _, e := range entries {
		next.domainCount += len(e.hashes)
		if e.mode == model.DNSBlockModeNXDomain {
			next.hasNXDomain = true
		}
	}
	idx.snap.Store(next)
}

// Empty reports whether the index currently has no lists at all — a fast
// path callers should check before doing any per-query work, mirroring
// dnsBlockIndex.Empty.
func (idx *dnsBlocklistIndex) Empty() bool {
	snap := idx.snap.Load()
	return snap == nil || len(snap.lists) == 0
}

// DomainCount returns the total number of domains currently indexed across
// every list (sum of each list's hash count) — used for
// diagnostics/tests, never on the hot path.
func (idx *dnsBlocklistIndex) DomainCount() int {
	snap := idx.snap.Load()
	if snap == nil {
		return 0
	}
	return snap.domainCount
}

// Match mirrors dnsmasq's own matching rules for the two blocklist
// mechanisms (plan §2.6 "Match algorithm"):
//  1. Level 0 (the queried name itself) is checked against every list,
//     regardless of mode — an exact hit always counts, since addn-hosts
//     matches the literal name and address=/d/ trivially matches itself
//     too.
//  2. Each subsequent level strips the left-most label (up to 16 levels,
//     via strings.IndexByte — never strings.Split/HasSuffix, to stay
//     allocation-free and enforce label-boundary matching, exactly like
//     dnsBlockIndex.Match) and is checked ONLY against lists whose
//     mode == model.DNSBlockModeNXDomain — sinkhole lists never match a
//     parent domain, since addn-hosts doesn't cover subdomains.
//  3. The shallowest level that matches wins ("more specific domains take
//     precedence", dnsmasq's own precedence rule) — if multiple lists match
//     at the same level, the first one in snapshot order (== manifest
//     order) wins.
//  4. If the snapshot has no nxdomain-mode list at all (hasNXDomain ==
//     false), the parent-label walk is skipped entirely (fast path for the
//     common case).
//
// Never performs I/O and never logs domain (privacy, same contract as
// dnsBlockIndex.Match).
func (idx *dnsBlocklistIndex) Match(domain string) (listName, mode string, ok bool) {
	snap := idx.snap.Load()
	if snap == nil || len(snap.lists) == 0 {
		return "", "", false
	}

	d := strings.ToLower(domain)
	if name, m, found := matchBlocklistLevel(snap.lists, d, true); found {
		return name, m, true
	}

	if !snap.hasNXDomain {
		return "", "", false
	}

	rest := d
	for i := 0; i < 16; i++ {
		dot := strings.IndexByte(rest, '.')
		if dot < 0 {
			break
		}
		rest = rest[dot+1:]
		if rest == "" {
			break
		}
		if name, m, found := matchBlocklistLevel(snap.lists, rest, false); found {
			return name, m, true
		}
	}
	return "", "", false
}

// streamBlocklistHashes reads one list's <id>.hosts file back off disk,
// domain by domain, and returns a sorted slice of the FNV-1a 64-bit hashes
// (T-06) — never a []string of the domains themselves. <id>.hosts is the
// canonical, always-present artifact for every list regardless of BlockMode
// (plan §2.1.1), so this is the only file read here; the derived <id>.conf
// (nxdomain mode only) is never touched.
//
// kernel.DNSServerManager.StreamBlocklistFile is callback-based (it invokes
// fn once per raw line as it reads the file, never materializing the whole
// file), while model.ParseHostsFileDomains is io.Reader-based (it owns its
// own bufio.Scanner with the DNSBlocklistMaxLineBytes cap and applies the
// "# comment" / two-field-only parsing rules for a <id>.hosts file this
// codebase itself generated — see that function's doc comment). An io.Pipe
// bridges the two so ParseHostsFileDomains' parsing logic is reused as-is
// rather than duplicated line-by-line here: a goroutine forwards each line
// StreamBlocklistFile hands it into the pipe's write end, while this
// goroutine reads the pipe's read end through ParseHostsFileDomains. Peak
// RAM is therefore the (growing) hashes slice plus one buffered line, never
// the whole file or a []string of every domain.
//
// A missing file, a validation failure on id, or any read/parse error
// degrades to an empty (nil) hash slice rather than propagating — see
// StatisticsService.SetBlocklists' doc comment for why a single bad list
// must not abort the whole index rebuild.
func streamBlocklistHashes(manager kernel.DNSServerManager, id string) []uint64 {
	pr, pw := io.Pipe()
	go func() {
		streamErr := manager.StreamBlocklistFile(id, func(line string) error {
			if _, err := io.WriteString(pw, line); err != nil {
				return err
			}
			_, err := pw.Write([]byte{'\n'})
			return err
		})
		_ = pw.CloseWithError(streamErr)
	}()

	var hashes []uint64
	_ = model.ParseHostsFileDomains(pr, func(domain string) error {
		hashes = append(hashes, blocklistHash(domain))
		return nil
	})
	// Unblock/terminate the writer goroutine if ParseHostsFileDomains
	// stopped early (e.g. a scan error) before it drained the whole pipe.
	_ = pr.Close()

	sort.Slice(hashes, func(i, j int) bool { return hashes[i] < hashes[j] })
	return hashes
}

// matchBlocklistLevel checks one label-level (exact string d) against every
// list in order; when exactLevel is false (a stripped parent label) it only
// considers lists whose mode == model.DNSBlockModeNXDomain, per Match's
// contract above.
func matchBlocklistLevel(lists []blocklistEntry, d string, exactLevel bool) (listName, mode string, ok bool) {
	hash := blocklistHash(d)
	for _, e := range lists {
		if !exactLevel && e.mode != model.DNSBlockModeNXDomain {
			continue
		}
		if blocklistEntryContains(e.hashes, hash) {
			return e.name, e.mode, true
		}
	}
	return "", "", false
}
