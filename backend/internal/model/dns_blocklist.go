package model

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
)

// DNS blocklist import (docs/ref/todo/dns-blocklist-import-plan.md, revision
// 3 — per-list blockMode). Deliberately does NOT use SQLite: metadata lives
// in a single JSON manifest file (see BlocklistManifest / kernel layer T-02)
// and the domain list itself lives only in generated dnsmasq config files
// (<id>.hosts / <id>.conf) — never in the DB, never held as a Go []string of
// 90k+ entries longer than it takes to render those files (plan §2.2/§2.3).
//
// This file is pure Go: it only ever reads from an io.Reader the caller
// supplies (a fetched HTTP body, an uploaded file, or a previously-generated
// <id>.hosts being streamed back) and returns bytes/values — no filesystem,
// no network. All I/O lives in the kernel layer (plan §2.3 item 5).

// DNSBlocklist is one entry of the blocklist manifest (docs/ref/todo/
// dns-blocklist-import-plan.md §2.3) — metadata about a subscribed URL or
// uploaded hosts-format file that has been parsed into <id>.hosts (and,
// for BlockMode == DNSBlockModeNXDomain, also <id>.conf). It is intentionally
// shaped like BlockedDomain/DNSZone (camelCase JSON, plain strings for
// timestamps) but is NOT stored in SQLite — see the package doc comment.
type DNSBlocklist struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	SourceType string `json:"sourceType"`
	URL        string `json:"url,omitempty"`
	// BlockMode selects which dnsmasq mechanism this list is rendered with —
	// DNSBlockModeSinkhole (addn-hosts, exact-match, default) or
	// DNSBlockModeNXDomain (conf-file with address=/d/, suffix-match). Reuses
	// the exact same constants as the deny-list (BlockedDomain.Mode); see
	// NormalizeBlocklistBlockMode for why the *default* differs between the
	// two features. omitempty so a manifest written before this field existed
	// round-trips byte-for-byte until rewritten (normalized on load, plan
	// §2.3 schema note).
	BlockMode     string `json:"blockMode,omitempty"`
	Enabled       bool   `json:"enabled"`
	DomainCount   int    `json:"domainCount"`
	FileBytes     int64  `json:"fileBytes"`
	Sha256        string `json:"sha256"`
	LastFetchedAt string `json:"lastFetchedAt,omitempty"`
	LastError     string `json:"lastError,omitempty"`
	CreatedAt     string `json:"createdAt"`
}

// DNSBlocklistInput is the create/update request body for a DNSBlocklist (no
// ID/timestamps/computed fields — server-assigned/derived).
type DNSBlocklistInput struct {
	Name      string `json:"name"`
	URL       string `json:"url,omitempty"`
	BlockMode string `json:"blockMode,omitempty"`
	Enabled   bool   `json:"enabled"`
}

// BlocklistRef is the minimal per-list information kernel.DNSServerManager.
// ApplyZones needs to render the right dnsmasq directive for an enabled
// blocklist: the id (which doubles as the <id>.hosts/<id>.conf filename
// stem) and its BlockMode (which mechanism/file to point at — plan §2.1).
// Deliberately NOT the full DNSBlocklist (the kernel layer has no business
// knowing Name/URL/DomainCount/etc.) and NOT a bare []string of ids (plan §3
// T-02 item 1: "kernel ต้องรู้โหมดถึงจะเลือก directive ถูก").
type BlocklistRef struct {
	ID        string
	BlockMode string
}

// BlocklistManifestSchemaVersion is the current schema version written by
// this codebase. See ValidateBlocklistManifest / the store's Load logic
// (service/dns_blocklist_store.go) for the fail-closed rule when a manifest
// on disk carries a newer version than this binary understands.
const BlocklistManifestSchemaVersion = 1

// BlocklistManifest is the whole contents of
// /var/lib/pigate/blocklists/manifest.json (plan §2.3) — the sole metadata
// store for the blocklist feature, replacing what would otherwise be a
// SQLite table.
type BlocklistManifest struct {
	SchemaVersion int            `json:"schemaVersion"`
	UpdatedAt     string         `json:"updatedAt"`
	Lists         []DNSBlocklist `json:"lists"`
}

// ValidateBlocklistManifest checks structural invariants of a manifest
// before it is persisted: a positive schema version, unique list IDs, and
// every list passing its own field validators. It does not check
// cross-cutting quotas (DNSBlocklistsMax, DNSBlocklistMaxTotalDomains,
// DNSBlocklistMaxNXDomainDomains) — those are business rules enforced by the
// service layer at the point a list is added/changed, not a structural
// property of an already-persisted manifest.
func ValidateBlocklistManifest(m BlocklistManifest) error {
	if m.SchemaVersion <= 0 {
		return fmt.Errorf("manifest schemaVersion %d must be positive", m.SchemaVersion)
	}
	seen := make(map[string]bool, len(m.Lists))
	for _, l := range m.Lists {
		if err := ValidateDNSBlocklistID(l.ID); err != nil {
			return err
		}
		if seen[l.ID] {
			return fmt.Errorf("manifest has duplicate list id %q", l.ID)
		}
		seen[l.ID] = true
		if err := ValidateDNSBlocklistName(l.Name); err != nil {
			return err
		}
		if l.SourceType != DNSBlocklistSourceURL && l.SourceType != DNSBlocklistSourceUpload {
			return fmt.Errorf("list %q sourceType %q is invalid (allowed: %q, %q)", l.ID, l.SourceType, DNSBlocklistSourceURL, DNSBlocklistSourceUpload)
		}
		if l.SourceType == DNSBlocklistSourceURL {
			if err := ValidateDNSBlocklistURL(l.URL); err != nil {
				return err
			}
		}
		if _, err := NormalizeBlocklistBlockMode(l.BlockMode); err != nil {
			return err
		}
	}
	return nil
}

// Blocklist source types, limits and defaults (docs/ref/todo/
// dns-blocklist-import-plan.md §2.1.4 / §3 T-01). Every numeric constant
// below carries the reasoning for its value in its own comment — do not
// change one without re-reading the corresponding plan section.
const (
	DNSBlocklistSourceURL    = "url"
	DNSBlocklistSourceUpload = "upload"

	// DNSBlocklistsMax caps the number of subscribed/uploaded lists (not the
	// number of domains) — a small number is enough to cover "a couple of
	// public blocklists plus a personal upload" while keeping the manifest
	// and the UI table trivially small.
	DNSBlocklistsMax = 8

	// DNSBlocklistMaxFileBytes bounds a single fetched/uploaded hosts file —
	// 16 MiB comfortably covers StevenBlack "unified" (~1.9 MB) with a lot of
	// headroom while still being a sane bound on outbound fetch size and
	// upload body size (also enforced at the HTTP layer, T-08).
	DNSBlocklistMaxFileBytes = 16 << 20

	// DNSBlocklistMaxDomainsPerList bounds one list after parsing/dedupe —
	// well above the largest well-known public list (StevenBlack unified is
	// ~93.5k) so it never rejects a legitimate list, while still being a
	// concrete ceiling against a pathological/malicious source.
	DNSBlocklistMaxDomainsPerList = 300000

	// DNSBlocklistMaxTotalDomains bounds the sum of DomainCount across every
	// enabled list, regardless of blockMode — sized for "several large public
	// lists at once" (plan §0/§2.1.4), independent from the deny-list's own
	// DNSBlockedDomainsMax (1000), which this feature does not touch.
	DNSBlocklistMaxTotalDomains = 500000

	// DNSBlocklistMaxNXDomainDomains bounds the sum of DomainCount across
	// only the lists whose BlockMode == DNSBlockModeNXDomain. Those domains
	// are rendered into a dnsmasq conf-file (address=/d/) that dnsmasq must
	// parse TWICE per Apply — once for `dnsmasq --test` and once again on
	// actual start (plan §2.1.3) — unlike addn-hosts (sinkhole mode), which
	// is never parsed during --test at all. 150000 was picked to comfortably
	// fit "StevenBlack unified (~93k) plus one more sizeable list" while
	// staying well under DNSBlocklistMaxTotalDomains; it is a rough estimate
	// from documentation, not a measurement, and MUST be re-tuned after
	// measuring real Apply time on actual hardware (plan §2.1.3/§2.1.4,
	// recorded in T-13).
	DNSBlocklistMaxNXDomainDomains = 150000

	// DNSBlocklistNameMax bounds the free-text list name field.
	DNSBlocklistNameMax = 64

	// DNSBlocklistMaxLineBytes bounds a single line read from a hosts file —
	// lines longer than this are skipped (and counted), not treated as a
	// parse error, since a hostile/corrupt source could otherwise use one
	// enormous line to defeat the parser or exhaust memory.
	DNSBlocklistMaxLineBytes = 512

	// DNSBlocklistSinkholeIP is the address every domain in a sinkhole-mode
	// list resolves to — we always render this fixed value ourselves and
	// discard whatever IP the source file specified (plan §2.2 security
	// requirement: never forward a third-party-controlled IP to dnsmasq).
	DNSBlocklistSinkholeIP = "0.0.0.0"

	// DNSBlocklistFetchTimeout bounds a single subscribe-URL fetch.
	DNSBlocklistFetchTimeout = 60 * time.Second

	// DNSBlocklistDefaultBlockMode is applied when a caller does not specify
	// a blockMode (empty string) — DNSBlockModeSinkhole, which is the
	// OPPOSITE default of the deny-list (ValidateBlockedDomain defaults to
	// DNSBlockModeNXDomain). See NormalizeBlocklistBlockMode for why.
	DNSBlocklistDefaultBlockMode = DNSBlockModeSinkhole
)

var (
	// reBlocklistID matches the manifest "id" field, which doubles as the
	// filename stem for <id>.hosts/<id>.conf on disk — kept intentionally
	// narrow (lower-case alnum only, no '.', '/', '_') since the kernel layer
	// uses it directly to build a filesystem path (plan §3 T-02 item 2:
	// "kernel ต้อง validateBlocklistID ... ห้ามใช้ filepath.Clean แทน").
	reBlocklistID = regexp.MustCompile(`^bl-[a-z0-9]{1,32}$`)
)

// ValidateDNSBlocklistName checks the free-text list name.
func ValidateDNSBlocklistName(name string) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("blocklist name must not be empty")
	}
	if strings.TrimSpace(name) != name {
		return fmt.Errorf("blocklist name %q must not have leading or trailing whitespace", name)
	}
	if len(name) > DNSBlocklistNameMax {
		return fmt.Errorf("blocklist name %q exceeds %d characters", name, DNSBlocklistNameMax)
	}
	if strings.ContainsAny(name, "\n\r") {
		return fmt.Errorf("blocklist name must not contain newlines")
	}
	return nil
}

// ValidateDNSBlocklistID checks a manifest list id — see reBlocklistID for
// why the charset is deliberately narrower than a general identifier.
func ValidateDNSBlocklistID(id string) error {
	if !reBlocklistID.MatchString(id) {
		return fmt.Errorf("blocklist id %q is invalid (must match ^bl-[a-z0-9]{1,32}$)", id)
	}
	return nil
}

// ValidateDNSBlocklistURL checks a subscribe URL before it is stored and
// before every fetch/refresh. HTTPS-only per explicit owner decision (plan
// §3 T-01 item 4) — this is a defense-in-depth check at the value-validation
// layer; the actual SSRF defense (rejecting non-public IPs at dial time) is
// in service/dns_blocklist_fetch.go (T-04), not here.
func ValidateDNSBlocklistURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("blocklist url must not be empty")
	}
	if len(rawURL) > 2048 {
		return fmt.Errorf("blocklist url exceeds 2048 characters")
	}
	if strings.ContainsAny(rawURL, "\n\r") {
		return fmt.Errorf("blocklist url must not contain newlines")
	}
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("blocklist url %q is not a valid URL: %w", rawURL, err)
	}
	if u.Scheme != "https" {
		return fmt.Errorf("blocklist url %q must use https (owner decision: https-only)", rawURL)
	}
	if u.Host == "" {
		return fmt.Errorf("blocklist url %q has no host", rawURL)
	}
	if u.User != nil {
		return fmt.Errorf("blocklist url must not contain userinfo (user:pass@)")
	}
	if port := u.Port(); port != "" && port != "443" {
		return fmt.Errorf("blocklist url %q must use port 443 (or no explicit port)", rawURL)
	}
	return nil
}

// NormalizeBlocklistBlockMode maps an input blockMode value (as received
// from the API/manifest, possibly empty) onto one of the two canonical mode
// constants — reusing model.DNSBlockModeSinkhole / model.DNSBlockModeNXDomain
// (dns_validate.go), the SAME constants the deny-list uses. It deliberately
// does NOT declare a new set of mode constants for blocklists (plan §3 T-01
// item 4b).
//
// IMPORTANT: the default here is the OPPOSITE of ValidateBlockedDomain's
// default. An empty blockMode normalizes to DNSBlockModeSinkhole (NOT
// DNSBlockModeNXDomain like the deny-list) because at blocklist scale
// (tens of thousands of domains) sinkhole (addn-hosts) is dramatically
// cheaper for dnsmasq to load and query (plan §2.1.3), and public blocklists
// already enumerate every subdomain they mean to block explicitly, so the
// "subdomain coverage" that nxdomain mode buys (plan §2.1) is not needed the
// way it is for the small, hand-curated deny-list (plan §2.1.4).
func NormalizeBlocklistBlockMode(mode string) (string, error) {
	switch mode {
	case "":
		return DNSBlocklistDefaultBlockMode, nil
	case DNSBlockModeSinkhole, DNSBlockModeNXDomain:
		return mode, nil
	default:
		return "", fmt.Errorf("blockMode %q is invalid (allowed: %q, %q)", mode, DNSBlockModeSinkhole, DNSBlockModeNXDomain)
	}
}

// ValidateBlocklistDomain checks a single domain name as it will be
// interpolated into a generated dnsmasq file (addn-hosts `0.0.0.0 <domain>`
// line, or conf-file `address=/<domain>/` line) — the same charset/shape
// rules as the deny-list's per-domain checks, factored out here so
// ValidateBlockedDomain (dns_validate.go) and ParseHostsBlocklist share one
// implementation instead of two copies drifting apart.
func ValidateBlocklistDomain(domain string) error {
	if domain == "" {
		return fmt.Errorf("domain must not be empty")
	}
	if strings.TrimSpace(domain) != domain {
		return fmt.Errorf("domain %q must not have leading or trailing whitespace", domain)
	}
	if len(domain) > 253 {
		return fmt.Errorf("domain %q exceeds 253 characters", domain)
	}
	if !reZoneName.MatchString(domain) {
		return fmt.Errorf("domain %q contains invalid characters (allowed: letters, digits, '.', '-')", domain)
	}
	if !strings.Contains(domain, ".") {
		return fmt.Errorf("domain %q must contain at least one '.'", domain)
	}
	if strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return fmt.Errorf("domain %q must not start or end with '.'", domain)
	}
	if strings.HasPrefix(domain, "-") || strings.HasSuffix(domain, "-") {
		return fmt.Errorf("domain %q must not start or end with '-'", domain)
	}
	return nil
}

// BlocklistParseStat summarizes what ParseHostsBlocklist did with an input
// file — surfaced to the UI so an import that silently drops most lines is
// visible, rather than only showing a final DomainCount.
type BlocklistParseStat struct {
	TotalLines      int
	Accepted        int
	SkippedComment  int
	SkippedInvalid  int
	SkippedExcluded int
	Duplicates      int
}

// blocklistBuiltinNames are hostnames commonly present in public hosts-format
// blocklists (StevenBlack et al.) that refer to the loopback/broadcast
// housekeeping entries every such file starts with — never something a user
// intends to block, and blocking "localhost" would break the device itself.
var blocklistBuiltinNames = map[string]bool{
	"localhost":             true,
	"localhost.localdomain": true,
	"local":                 true,
	"localhost4":            true,
	"localhost6":            true,
	"ip6-localhost":         true,
	"ip6-loopback":          true,
	"ip6-localnet":          true,
	"ip6-mcastprefix":       true,
	"ip6-allnodes":          true,
	"ip6-allrouters":        true,
	"broadcasthost":         true,
	"0.0.0.0":               true,
}

// blocklistExcludeMatch reports whether domain equals, or is a subdomain of
// (label-boundary, never a raw suffix check), any key in exclude. Mirrors
// service/dns_block_index.go's Match algorithm (strings.IndexByte, never
// strings.Split/strings.HasSuffix) so "notexample.com" is correctly NOT
// considered a subdomain of an excluded "example.com".
func blocklistExcludeMatch(domain string, exclude map[string]bool) bool {
	if len(exclude) == 0 {
		return false
	}
	if exclude[domain] {
		return true
	}
	rest := domain
	for i := 0; i < 16; i++ {
		dot := strings.IndexByte(rest, '.')
		if dot < 0 {
			break
		}
		rest = rest[dot+1:]
		if rest == "" {
			break
		}
		if exclude[rest] {
			return true
		}
	}
	return false
}

// ParseHostsBlocklist reads a hosts-format file (plain "<ip> <hostname...>"
// lines, '#' comments, blank lines) from r and returns the sorted set of
// accepted domains plus parse statistics.
//
// Security requirement (plan §2.2): the source IP column, if present, is
// ALWAYS discarded — a public hosts file can map any name to any IP, and
// trusting it would let whoever controls the source (or a MITM, or an
// uploader) spoof arbitrary DNS answers on this device. Every accepted
// domain is later re-rendered by RenderHostsFile/RenderBlocklistConfFile
// using our own fixed values (DNSBlocklistSinkholeIP / no IP at all) — this
// function itself never returns an IP, by construction, for either
// blockMode (the same parser feeds both — do not special-case one mode to
// "trust" the IP).
//
// exclude is a set of already-lower-cased domains (e.g. this device's own
// DNS zones/hostname) that must never be blocked; matching is label-boundary
// (see blocklistExcludeMatch), same semantics as dnsBlockIndex.Match.
func ParseHostsBlocklist(r io.Reader, exclude map[string]bool) ([]string, BlocklistParseStat, error) {
	var stat BlocklistParseStat
	accepted := make(map[string]bool)

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, DNSBlocklistMaxLineBytes), DNSBlocklistMaxLineBytes)
	for {
		ok := scanner.Scan()
		if !ok {
			if err := scanner.Err(); err != nil {
				// bufio.ErrTooLong from an over-length line is treated as a
				// skip, not a hard parse failure — a single pathological line
				// must not sink the whole import.
				if err == bufio.ErrTooLong {
					stat.TotalLines++
					stat.SkippedInvalid++
					// Scanner is now stuck on the same oversized line; there is
					// no supported way to resume mid-line, so stop here. Any
					// domains already accepted are still returned.
					break
				}
				return nil, stat, err
			}
			break
		}
		stat.TotalLines++
		line := scanner.Text()

		if idx := strings.IndexByte(line, '#'); idx >= 0 {
			line = line[:idx]
		}
		fields := strings.Fields(line)
		if len(fields) == 0 {
			stat.SkippedComment++
			continue
		}

		var hostFields []string
		if net.ParseIP(fields[0]) != nil {
			hostFields = fields[1:]
		} else {
			hostFields = fields
		}
		if len(hostFields) == 0 {
			stat.SkippedInvalid++
			continue
		}
		if len(hostFields) > 16 {
			hostFields = hostFields[:16]
		}

		for _, raw := range hostFields {
			name := strings.ToLower(strings.TrimSuffix(raw, "."))
			if blocklistBuiltinNames[name] {
				stat.SkippedInvalid++
				continue
			}
			if err := ValidateBlocklistDomain(name); err != nil {
				stat.SkippedInvalid++
				continue
			}
			if blocklistExcludeMatch(name, exclude) {
				stat.SkippedExcluded++
				continue
			}
			if accepted[name] {
				stat.Duplicates++
				continue
			}
			if len(accepted) >= DNSBlocklistMaxDomainsPerList {
				return nil, stat, fmt.Errorf("blocklist exceeds maximum of %d domains", DNSBlocklistMaxDomainsPerList)
			}
			accepted[name] = true
			stat.Accepted++
		}
	}

	domains := make([]string, 0, len(accepted))
	for d := range accepted {
		domains = append(domains, d)
	}
	sort.Strings(domains)
	return domains, stat, nil
}

// RenderHostsFile renders the canonical <id>.hosts artifact for a blocklist
// (plan §2.1.1: this file is the source of truth for the list's domains
// regardless of blockMode — <id>.conf, when present, is always derived from
// it). domains is sorted for a deterministic byte-for-byte output (so tests
// can compare bytes directly, and so re-generating from the same domain set
// twice produces an identical file/sha256).
func RenderHostsFile(id string, domains []string, generatedAt time.Time) []byte {
	sorted := append([]string(nil), domains...)
	sort.Strings(sorted)

	var b strings.Builder
	fmt.Fprintf(&b, "# PiGate blocklist %s — generated %s\n", id, generatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# %d domain(s). Do not edit — regenerated on every refresh/import.\n", len(sorted))
	for _, d := range sorted {
		fmt.Fprintf(&b, "%s %s\n", DNSBlocklistSinkholeIP, d)
	}
	return []byte(b.String())
}

// RenderBlocklistConfFile renders the <id>.conf artifact used for
// DNSBlockModeNXDomain lists — a plain dnsmasq config file, one
// `address=/<domain>/` directive per line (no batching multiple domains into
// one directive: dnsmasq reads config lines through a bounded buffer
// (MAXDNAME) and undocumented over-length-line behavior is not a risk worth
// taking against a file dnsmasq will load — plan §2.1).
//
// Per the dnsmasq manual: "-A, --address=/<domain>[/<domain>...]/[<ipaddr>]
// ... one or more domains with no address returns a no-such-domain answer,
// so --address=/example.com/ is equivalent to --server=/example.com/ and
// returns NXDOMAIN for example.com and all its subdomains" — i.e. omitting
// the trailing <ipaddr> is what turns this into an NXDOMAIN directive, and
// matching is done on whole labels (suffix/subdomain coverage), matching
// dnsBlockIndex's own suffix-match semantics for nxdomain-mode lists
// (service/dns_blocklist_index.go, T-06).
//
// address= is used here instead of server= (which the small, ≤1000-entry
// deny-list uses) because of a documented performance asymmetry at
// blocklist scale: dnsmasq 2.86's "domain search rewrite" made address=
// lookups grow as O(log2 n) in the number of configured domains, while
// dnsmasq 2.88's changelog separately notes that reading large numbers of
// --server options could take O(n^2) time at start — a cost specific to the
// --server code path. At tens of thousands of domains, address= is the
// documented-safe choice; --server is not. Do NOT change the deny-list to
// address=/d/ to "be consistent" — at ≤1000 entries there is no benefit, and
// it would break buildDNSConfig's byte-compatible golden tests for no gain
// (plan §2.1).
func RenderBlocklistConfFile(id string, domains []string, generatedAt time.Time) []byte {
	sorted := append([]string(nil), domains...)
	sort.Strings(sorted)

	var b strings.Builder
	fmt.Fprintf(&b, "# PiGate blocklist %s (nxdomain mode) — generated %s\n", id, generatedAt.UTC().Format(time.RFC3339))
	fmt.Fprintf(&b, "# %d domain(s). Do not edit — regenerated on every refresh/import/mode switch.\n", len(sorted))
	for _, d := range sorted {
		fmt.Fprintf(&b, "address=/%s/\n", d)
	}
	return []byte(b.String())
}

// ParseHostsFileDomains streams a previously-generated <id>.hosts file
// (i.e. one we ourselves produced with RenderHostsFile — NOT an arbitrary
// third-party hosts file, which must go through ParseHostsBlocklist instead)
// back out domain-by-domain, calling fn once per domain. Used to rebuild the
// statistics index (T-06) and to re-render <id>.conf when a list's blockMode
// is switched without re-fetching (T-05) — both cases where holding all
// ~90k+ domains as a single []string in memory would be wasteful.
//
// fn returning a non-nil error stops the scan and that error is returned.
func ParseHostsFileDomains(r io.Reader, fn func(string) error) error {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, DNSBlocklistMaxLineBytes), DNSBlocklistMaxLineBytes)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			continue
		}
		if err := fn(fields[1]); err != nil {
			return err
		}
	}
	return scanner.Err()
}
