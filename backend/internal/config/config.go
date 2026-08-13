// Package config loads PiGate's bootstrap runtime configuration from a flat
// `key=value` file (see docs/ref/todo/config-file-loader-plan.md, issue #68).
//
// This is deliberately NOT the SQLite-backed subsystem configuration that
// lives under internal/db/internal/service (interfaces, firewall, DHCP,
// etc.) — it only covers the small set of process bootstrap parameters that
// were previously settable exclusively via CLI flags in the systemd unit's
// ExecStart line (mock mode, DB path, ports, docker-compat, ...).
//
// The package is split into four pure functions so each stage is testable
// without touching a real file:
//
//	Defaults()                                  -> code defaults (mostly 1:1 with cmd/pigate/main.go flags)
//	Parse(r io.Reader)                          -> raw "key=value" syntax, no type conversion
//	Resolve(defaults, fileVals, explicit)       -> merges + type-converts, defaults < file < explicit
//	Write(w io.Writer, cfg Config)               -> serializes a Config back to "key=value" (round-trips with Parse)
//
// Precedence (low to high): code default < config file < CLI flag explicitly
// set by the user. main.go is responsible for I/O (reading/writing the actual
// file, calling flag.Visit) and for logging; this package never touches a
// filesystem path or the log package directly, so it stays 100% unit
// testable (see config_test.go).
//
// Deliberate exception to the "every key mirrors a CLI flag" rule above:
// dns-stats-max-pairs / dns-stats-max-clients are intentionally FILE-ONLY —
// no flag.Int is registered for them in cmd/pigate/main.go, by design (RAM-
// tuning knobs for an appliance, not day-to-day CLI switches; see
// docs/ref/todo/dns-stats-tracking-limits-config-plan.md). Because no flag
// exists, flag.Visit can never populate them into the "explicit" layer passed
// to Resolve, so precedence for these two keys collapses to code default <
// config file — that's expected, not a bug.
//
// These two keys also use a two-tier validation rule that differs from every
// other key: a non-integer value is still a fail-fast error from applyKey
// (same as port/https-port), but an in-range-syntax value that is out of the
// sane RAM-guard range (<=0 or absurdly large) is NOT fatal — Resolve clamps
// it back to the default and appends a warning instead, so a typo'd value
// never turns into a boot loop that takes the gateway's network off the air.
//
// traffic-stats-max-hosts / -max-dests / -max-conversations (docs/ref/todo/
// statistics-traffic-page-plan.md §1.6/T-02) mirror this exact pattern —
// file-only, same two-tier validation — for the Statistics -> Traffic page's
// per-bucket tracking caps (service/traffic_stats.go).
//
// dns-stats-max-domains / dns-stats-max-ips-per-domain (docs/ref/todo/
// statistics-dns-page-revamp-plan.md §2.1/T-05) mirror the same pattern for
// the Statistics -> DNS page's domain->resolved-IP forward index
// (service/dns_domain_ips.go) — file-only, non-integer is a fail-fast error,
// out-of-range-but-syntactically-valid clamps to the default with a warning.
//
// ipinfo-enabled (docs/ref/todo/statistics-host-ipinfo-plan.md T-06) is also
// file-only (no CLI flag, no two-tier validation needed — it's a plain bool,
// default false) gating the Public IP Info card's backend proxy to
// ipinfo.io: this is the ONE key in this file that decides whether the
// daemon ever makes an outbound request to a third party at all, so it
// defaults OFF and can only be turned on by deliberately editing the config
// file (plan §2 "การเปิดใช้งาน").
//
// deny-stats-max-sources / deny-stats-max-ports (docs/ref/todo/
// statistics-capacity-visibility-plan.md T-14) mirror the exact same pattern
// for the firewall deny ring's per-bucket caps (service/statistics.go),
// which used to be hardcoded consts (maxTrackedDenySources=500,
// maxTrackedDenyPorts=300) — promoted to file-only config keys so their
// capSource on the new GET /api/statistics/capacity response points at a
// real, editable config key like every other ring's cap does.
//
// traffic-log-buffer-capacity (docs/ref/todo/firewall-log-buffer-capacity-plan.md
// T-00, issue #134) is also file-only and uses the same two-tier validation
// as the *-max-* keys above. It differs from every other key in this file in
// one important way: it is not re-read continuously — it is consumed exactly
// once, at process startup, to size internal/logs.RingBuffer via
// logs.NewRingBuffer() in cmd/pigate/main.go, before the ring is handed to
// any subscriber. There is no live re-sizing path, so changing this key only
// takes effect after a `sudo systemctl restart pigate` (or equivalent
// restart of the process) — never while the process is running.
//
// max-object-entries / max-expanded-rules-per-policy (docs/ref/todo/
// multi-value-address-service-objects-plan.md §2.1/T-00A) are also file-only
// and use the same two-tier validation as the *-max-* keys above.
// max-object-entries caps how many sub-entries a single Address/Service
// Object may hold (enforced by db/repository.go's validation on
// create/update); max-expanded-rules-per-policy caps how many nft rules a
// single firewall policy rule may expand into once its address/service
// objects' entries are cross-multiplied (enforced by kernel/real_firewall.go's
// addUserChainRules). Like traffic-log-buffer-capacity, both are only
// consumed at process startup (wired into the repository/kernel layer once
// during cmd/pigate/main.go's construction) — changing either only takes
// effect after a service restart, never live.
package config

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
)

// Config holds all bootstrap parameters that can be sourced from CLI flags
// and/or the config file. Field names mirror the CLI flag names in
// cmd/pigate/main.go (CamelCase of the flag's kebab-case name).
type Config struct {
	Port                   int
	DBPath                 string
	Mock                   bool
	MockFromReal           bool
	DisableEdit            bool
	AllowEditSystemRoutes  bool
	EnableEditSystemRoute  bool
	PrioritizeKernelRoutes bool
	DockerCompat           bool
	HTTPSPort              int
	TLSDir                 string
	AllowDevCORS           bool

	// DNSStatsMaxPairs/DNSStatsMaxClients are the ONLY two fields with no
	// matching CLI flag in cmd/pigate/main.go — they are file-only tuning
	// knobs (see the package doc comment above). Every other field here has
	// a 1:1 flag counterpart.
	DNSStatsMaxPairs   int
	DNSStatsMaxClients int

	// TrafficStatsMaxHosts/TrafficStatsMaxDests/TrafficStatsMaxConversations
	// are also file-only (no matching CLI flag) — same rationale as
	// DNSStatsMaxPairs/DNSStatsMaxClients above (docs/ref/todo/
	// statistics-traffic-page-plan.md §1.6/T-02).
	TrafficStatsMaxHosts         int
	TrafficStatsMaxDests         int
	TrafficStatsMaxConversations int

	// DNSStatsMaxDomains/DNSStatsMaxIPsPerDomain are also file-only (no
	// matching CLI flag) — RAM-guard caps for the domain->resolved-IP forward
	// index (service/dns_domain_ips.go, docs/ref/todo/
	// statistics-dns-page-revamp-plan.md §2.1/T-05). Worst-case RAM for this
	// index is maxDomains x maxIPsPerDomain entries — NOT multiplied by any
	// bucket count, unlike the DNS query-pair stats ring above, because this
	// index is a flat map, not a ring.
	DNSStatsMaxDomains      int
	DNSStatsMaxIPsPerDomain int

	// IPInfoEnabled is also file-only (no matching CLI flag) — gates the
	// Public IP Info card's backend proxy to ipinfo.io (docs/ref/todo/
	// statistics-host-ipinfo-plan.md T-06). Default false; no token support
	// in this phase.
	IPInfoEnabled bool

	// DenyStatsMaxSources/DenyStatsMaxPorts are also file-only (no matching
	// CLI flag) — the firewall deny ring's per-bucket caps (docs/ref/todo/
	// statistics-capacity-visibility-plan.md T-14), promoted from consts
	// (service/statistics.go's old maxTrackedDenySources/maxTrackedDenyPorts)
	// so an operator can raise them without a rebuild, same rationale as
	// TrafficStatsMax* above.
	DenyStatsMaxSources int
	DenyStatsMaxPorts   int

	// TrafficLogBufferCapacity is also file-only (no matching CLI flag) — it
	// sizes the traffic log ring buffer (backend/internal/logs/ringbuffer.go)
	// that main.go creates for the Forward/Local Traffic log pages and the
	// Statistics -> Capacity firewall.logBuffer ring (docs/ref/todo/
	// firewall-log-buffer-capacity-plan.md T-00, issue #134). Unlike the
	// other file-only keys above, this one is only read once at startup to
	// construct the ring buffer — changing it takes effect only after a
	// service restart, never live.
	TrafficLogBufferCapacity int

	// MaxObjectEntries/MaxExpandedRulesPerPolicy are also file-only (no
	// matching CLI flag). MaxObjectEntries is the maximum number of
	// sub-entries allowed in a single Address/Service Object, enforced at
	// validation time in db/repository.go. MaxExpandedRulesPerPolicy is the
	// ceiling on how many nft rules a single firewall policy rule may expand
	// into (its address/service objects' entries cross-multiplied), enforced
	// in kernel/real_firewall.go's addUserChainRules (docs/ref/todo/
	// multi-value-address-service-objects-plan.md §2.1). Both take effect
	// only on process restart, like TrafficLogBufferCapacity above.
	MaxObjectEntries          int
	MaxExpandedRulesPerPolicy int
}

// Defaults returns the Config populated with the exact same defaults as the
// CLI flags registered in cmd/pigate/main.go. Keep this 1:1 with that file —
// it is the single source of truth for "what does pigate do if you tell it
// nothing at all".
func Defaults() Config {
	return Config{
		Port:                   2479,
		DBPath:                 "pigate.db",
		Mock:                   true,
		MockFromReal:           false,
		DisableEdit:            false,
		AllowEditSystemRoutes:  false,
		EnableEditSystemRoute:  false,
		PrioritizeKernelRoutes: false,
		DockerCompat:           false,
		HTTPSPort:              0,
		TLSDir:                 "",
		AllowDevCORS:           false,

		// Keep in sync with defaultMaxTrackedDNSPairs/defaultMaxTrackedDNSClients
		// in internal/service/dns_query_stats.go.
		DNSStatsMaxPairs:   2400,
		DNSStatsMaxClients: 200,

		// Keep in sync with defaultMaxTrackedHosts/defaultMaxTrackedDests/
		// defaultMaxTrackedConversations in internal/service/traffic_stats.go
		// (docs/ref/todo/statistics-traffic-page-plan.md §1.6 table).
		TrafficStatsMaxHosts:         500,
		TrafficStatsMaxDests:         500,
		TrafficStatsMaxConversations: 600,

		// Keep in sync with the defaults documented for dnsDomainIPs in
		// internal/service/dns_domain_ips.go (docs/ref/todo/
		// statistics-dns-page-revamp-plan.md §2.1).
		DNSStatsMaxDomains:      1000,
		DNSStatsMaxIPsPerDomain: 32,

		// Default OFF (docs/ref/todo/statistics-host-ipinfo-plan.md T-06).
		IPInfoEnabled: false,

		// Keep in sync with the old maxTrackedDenySources/maxTrackedDenyPorts
		// consts these keys replace (docs/ref/todo/
		// statistics-capacity-visibility-plan.md T-14).
		DenyStatsMaxSources: 500,
		DenyStatsMaxPorts:   300,

		// Keep in sync with the old trafficLogBufferCapacity const this key
		// replaces in cmd/pigate/main.go (docs/ref/todo/
		// firewall-log-buffer-capacity-plan.md T-00).
		TrafficLogBufferCapacity: 10000,

		// Owner-confirmed defaults (D-3, docs/ref/todo/
		// multi-value-address-service-objects-plan.md §2.1). MaxObjectEntries
		// must be kept in sync with model.DefaultMaxObjectEntries.
		MaxObjectEntries:          64,
		MaxExpandedRulesPerPolicy: 4096,
	}
}

// Known config/flag keys. Intentionally excludes "config" (the path to the
// config file itself) and "v" (print-version, early-return before any config
// handling) — see KnownKeys.
const (
	keyPort                   = "port"
	keyDBPath                 = "db"
	keyMock                   = "mock"
	keyMockFromReal           = "mock-from-real"
	keyDisableEdit            = "disable-edit"
	keyAllowEditSystemRoutes  = "allow-edit-system-routes"
	keyEnableEditSystemRoute  = "enable-edit-system-route"
	keyPrioritizeKernelRoutes = "prioritize-kernel-routes"
	keyDockerCompat           = "docker-compat"
	keyHTTPSPort              = "https-port"
	keyTLSDir                 = "tls-dir"
	keyAllowDevCORS           = "allow-dev-cors"

	// keyDNSStatsMaxPairs/keyDNSStatsMaxClients are file-only (no CLI flag —
	// see the package doc comment).
	keyDNSStatsMaxPairs   = "dns-stats-max-pairs"
	keyDNSStatsMaxClients = "dns-stats-max-clients"

	// keyTrafficStatsMax{Hosts,Dests,Conversations} are also file-only (no
	// CLI flag — docs/ref/todo/statistics-traffic-page-plan.md §1.6).
	keyTrafficStatsMaxHosts         = "traffic-stats-max-hosts"
	keyTrafficStatsMaxDests         = "traffic-stats-max-dests"
	keyTrafficStatsMaxConversations = "traffic-stats-max-conversations"

	// keyDNSStatsMaxDomains/keyDNSStatsMaxIPsPerDomain are also file-only (no
	// CLI flag — docs/ref/todo/statistics-dns-page-revamp-plan.md T-05).
	keyDNSStatsMaxDomains      = "dns-stats-max-domains"
	keyDNSStatsMaxIPsPerDomain = "dns-stats-max-ips-per-domain"

	// keyIPInfoEnabled is also file-only (no CLI flag — docs/ref/todo/
	// statistics-host-ipinfo-plan.md T-06).
	keyIPInfoEnabled = "ipinfo-enabled"

	// keyDenyStatsMaxSources/keyDenyStatsMaxPorts are also file-only (no CLI
	// flag — docs/ref/todo/statistics-capacity-visibility-plan.md T-14).
	keyDenyStatsMaxSources = "deny-stats-max-sources"
	keyDenyStatsMaxPorts   = "deny-stats-max-ports"

	// keyTrafficLogBufferCapacity is also file-only (no CLI flag — docs/ref/
	// todo/firewall-log-buffer-capacity-plan.md T-00, issue #134).
	keyTrafficLogBufferCapacity = "traffic-log-buffer-capacity"

	// keyMaxObjectEntries/keyMaxExpandedRulesPerPolicy are also file-only (no
	// CLI flag — docs/ref/todo/multi-value-address-service-objects-plan.md
	// §2.1/T-00A).
	keyMaxObjectEntries          = "max-object-entries"
	keyMaxExpandedRulesPerPolicy = "max-expanded-rules-per-policy"
)

// maxDNSStatsPairsCap/maxDNSStatsClientsCap are RAM-guard sanity ceilings for
// the two file-only DNS stats keys above. The cap applies per 5-minute
// bucket and the ring holds 288 buckets (24h), so worst-case tracked pairs
// is roughly dns-stats-max-pairs x 288 — an absurdly large value here is a
// direct RAM-growth knob, so Resolve clamps anything above these ceilings
// back to the default rather than trusting an arbitrary file value.
const (
	maxDNSStatsPairsCap   = 50000
	maxDNSStatsClientsCap = 10000
)

// maxTrafficStatsCap is the shared RAM-guard sanity ceiling for all three
// traffic-stats-max-* keys (docs/ref/todo/statistics-traffic-page-plan.md
// §1.6). The cap applies per 5-minute bucket and the ring holds 288 buckets
// (24h): a conversation entry costs ~110 bytes (≈45-char key + 16 B dirBytes
// + map overhead), so even at this ceiling (20000) the worst case is
// 20000 x 288 x 110B ≈ 634 MB — an operator raising a key this high has
// deliberately chosen to trade RAM for a wider tracking window; Resolve still
// clamps a syntactically-valid-but-insane value back to the default rather
// than trusting it blindly, exactly like the DNS stats caps above.
const maxTrafficStatsCap = 20000

// minDNSStatsMaxDomains/maxDNSStatsMaxDomains and
// minDNSStatsMaxIPsPerDomain/maxDNSStatsMaxIPsPerDomain are the accepted
// ranges for the two domain->IP index caps (docs/ref/todo/
// statistics-dns-page-revamp-plan.md §2.1/T-05). Unlike the DNS query-pair
// ring above, this index is NOT a ring — worst-case RAM is simply
// maxDomains x maxIPsPerDomain entries (no x288-bucket multiplier) — e.g. the
// defaults (1000 x 32 = 32000 entries, ~2 MB) or the ceiling of this range
// (20000 x 64 = 1,280,000 entries) is still a bounded, single flat map.
const (
	minDNSStatsMaxDomains      = 100
	maxDNSStatsMaxDomains      = 20000
	minDNSStatsMaxIPsPerDomain = 2
	maxDNSStatsMaxIPsPerDomain = 64
)

// maxDenyStatsCap is the shared RAM-guard sanity ceiling for both
// deny-stats-max-* keys (docs/ref/todo/statistics-capacity-visibility-plan.md
// T-14) — same per-bucket-x-288-buckets reasoning as maxTrafficStatsCap
// above (this ring shares denyBucketMax=288 with the traffic ring), so it
// reuses the identical ceiling.
const maxDenyStatsCap = maxTrafficStatsCap

// minTrafficLogBufferCapacity/maxTrafficLogBufferCapacity are the accepted
// range for traffic-log-buffer-capacity (docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-00, issue #134). At an estimated
// ~300-550 bytes/entry (from the original comment in cmd/pigate/main.go):
// the floor (500) guarantees at least one full page of the Log pages (500
// rows/page) and rejects zero/negative values; the ceiling (100000) is
// roughly 30-55 MB worst case, which is still acceptable RAM on a Pi 5 —
// anything higher risks starving other services on the board.
const (
	minTrafficLogBufferCapacity = 500
	maxTrafficLogBufferCapacity = 100000
)

// minMaxObjectEntries/maxMaxObjectEntries and
// minMaxExpandedRulesPerPolicy/maxMaxExpandedRulesPerPolicy are the accepted
// ranges for the two multi-value-object caps (docs/ref/todo/
// multi-value-address-service-objects-plan.md §2.1). The object-entries floor
// (1) always allows at least a single-value object (parity with the old
// single-value behavior); the ceiling (512) keeps a single object's expansion
// bounded. The expanded-rules-per-policy floor (64) still allows a reasonably
// sized address x service cross-product; the ceiling (65536) bounds worst-case
// nft ruleset size for one policy rule so a misconfigured object pair can't
// blow up the whole firewall table.
const (
	minMaxObjectEntries          = 1
	maxMaxObjectEntries          = 512
	minMaxExpandedRulesPerPolicy = 64
	maxMaxExpandedRulesPerPolicy = 65536
)

// orderedKeys is the fixed key order used by Write (and reused by KnownKeys)
// so the generated file is stable/diffable across runs.
var orderedKeys = []string{
	keyPort,
	keyDBPath,
	keyMock,
	keyMockFromReal,
	keyDisableEdit,
	keyAllowEditSystemRoutes,
	keyEnableEditSystemRoute,
	keyPrioritizeKernelRoutes,
	keyDockerCompat,
	keyHTTPSPort,
	keyTLSDir,
	keyAllowDevCORS,
	// Appended at the end (not alphabetized with the rest) so existing
	// generated config files keep a stable diff — see T-01 in
	// docs/ref/todo/dns-stats-tracking-limits-config-plan.md.
	keyDNSStatsMaxPairs,
	keyDNSStatsMaxClients,
	// Also appended at the end, after the DNS stats keys (docs/ref/todo/
	// statistics-traffic-page-plan.md T-02 step 1).
	keyTrafficStatsMaxHosts,
	keyTrafficStatsMaxDests,
	keyTrafficStatsMaxConversations,
	// Also appended at the end, after the traffic stats keys (docs/ref/todo/
	// statistics-dns-page-revamp-plan.md T-05).
	keyDNSStatsMaxDomains,
	keyDNSStatsMaxIPsPerDomain,
	// Also appended at the end, after the DNS stats domain keys (docs/ref/todo/
	// statistics-host-ipinfo-plan.md T-06).
	keyIPInfoEnabled,
	// Also appended at the end, after the ipinfo-enabled key (docs/ref/todo/
	// statistics-capacity-visibility-plan.md T-14).
	keyDenyStatsMaxSources,
	keyDenyStatsMaxPorts,
	// Appended at the very end, after deny-stats-max-ports, per docs/ref/todo/
	// firewall-log-buffer-capacity-plan.md T-00 — this is the newest file-only
	// key, and keeping new keys strictly appended (rather than alphabetized
	// among the existing ones) keeps already-generated pigate.conf files
	// diffing cleanly across upgrades.
	keyTrafficLogBufferCapacity,
	// Appended at the very end, after traffic-log-buffer-capacity, per
	// docs/ref/todo/multi-value-address-service-objects-plan.md T-00A —
	// keeping new keys strictly appended (rather than alphabetized among the
	// existing ones) keeps already-generated pigate.conf files diffing
	// cleanly across upgrades.
	keyMaxObjectEntries,
	keyMaxExpandedRulesPerPolicy,
}

// KnownKeys returns the list of recognized config/flag keys, in the fixed
// order used by Write. Any key found in the config file that isn't in this
// list is reported by Resolve as a warning rather than an error.
func KnownKeys() []string {
	keys := make([]string, len(orderedKeys))
	copy(keys, orderedKeys)
	return keys
}

func isKnownKey(key string) bool {
	for _, k := range orderedKeys {
		if k == key {
			return true
		}
	}
	return false
}

// Parse reads "key=value" syntax from r. It is pure syntax parsing only — no
// type conversion, no knowledge of which keys are valid — so it returns the
// raw map[string]string for Resolve to interpret.
//
// Rules: each line is trimmed; blank lines and lines starting with '#' (after
// trimming) are skipped; the remaining lines must contain '=' and are split
// on the FIRST '=' only (so a value may itself contain '=', e.g. a path);
// both key and value are trimmed. A non-blank, non-comment line without '='
// is a malformed-line error.
func Parse(r io.Reader) (map[string]string, error) {
	vals := make(map[string]string)
	scanner := bufio.NewScanner(r)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			return nil, fmt.Errorf("config: malformed line %d (expected key=value): %q", lineNo, line)
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		if key == "" {
			return nil, fmt.Errorf("config: malformed line %d (empty key): %q", lineNo, line)
		}
		vals[key] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("config: failed reading config: %w", err)
	}
	return vals, nil
}

// Resolve merges three layers into a final Config: defaults, then fileVals
// (typically parsed from the config file), then explicit (typically the CLI
// flags the user actually set, via flag.Visit) — each layer overriding the
// previous one for the keys it contains. Unknown keys in either map are
// collected into the returned warnings slice rather than causing an error.
// A value that fails type conversion (bool/int) is a fail-fast error,
// regardless of which layer it came from.
func Resolve(defaults Config, fileVals, explicit map[string]string) (Config, []string, error) {
	cfg := defaults
	var warnings []string

	apply := func(source string, vals map[string]string) error {
		// Deterministic iteration order (mainly for stable warning ordering).
		keys := make([]string, 0, len(vals))
		for k := range vals {
			keys = append(keys, k)
		}
		sort.Strings(keys)

		for _, key := range keys {
			value := vals[key]
			if !isKnownKey(key) {
				warnings = append(warnings, fmt.Sprintf("unknown config key %q in %s (ignored)", key, source))
				continue
			}
			if err := applyKey(&cfg, key, value); err != nil {
				return fmt.Errorf("config: %s: %w", source, err)
			}
		}
		return nil
	}

	if err := apply("file", fileVals); err != nil {
		return Config{}, nil, err
	}
	if err := apply("flag", explicit); err != nil {
		return Config{}, nil, err
	}

	// Range-sanity pass (T-01): runs after both layers have been applied, and
	// uses `defaults` (the function argument) as the fallback rather than a
	// package-level literal, so a caller passing custom defaults still
	// behaves sanely. This is a clamp + warning, never a fatal error — see
	// the package doc comment for the rationale.
	if cfg.DNSStatsMaxPairs <= 0 || cfg.DNSStatsMaxPairs > maxDNSStatsPairsCap {
		warnings = append(warnings, fmt.Sprintf(
			"dns-stats-max-pairs=%d out of range (1..%d), using default %d",
			cfg.DNSStatsMaxPairs, maxDNSStatsPairsCap, defaults.DNSStatsMaxPairs))
		cfg.DNSStatsMaxPairs = defaults.DNSStatsMaxPairs
	}
	if cfg.DNSStatsMaxClients <= 0 || cfg.DNSStatsMaxClients > maxDNSStatsClientsCap {
		warnings = append(warnings, fmt.Sprintf(
			"dns-stats-max-clients=%d out of range (1..%d), using default %d",
			cfg.DNSStatsMaxClients, maxDNSStatsClientsCap, defaults.DNSStatsMaxClients))
		cfg.DNSStatsMaxClients = defaults.DNSStatsMaxClients
	}
	if cfg.TrafficStatsMaxHosts <= 0 || cfg.TrafficStatsMaxHosts > maxTrafficStatsCap {
		warnings = append(warnings, fmt.Sprintf(
			"traffic-stats-max-hosts=%d out of range (1..%d), using default %d",
			cfg.TrafficStatsMaxHosts, maxTrafficStatsCap, defaults.TrafficStatsMaxHosts))
		cfg.TrafficStatsMaxHosts = defaults.TrafficStatsMaxHosts
	}
	if cfg.TrafficStatsMaxDests <= 0 || cfg.TrafficStatsMaxDests > maxTrafficStatsCap {
		warnings = append(warnings, fmt.Sprintf(
			"traffic-stats-max-dests=%d out of range (1..%d), using default %d",
			cfg.TrafficStatsMaxDests, maxTrafficStatsCap, defaults.TrafficStatsMaxDests))
		cfg.TrafficStatsMaxDests = defaults.TrafficStatsMaxDests
	}
	if cfg.TrafficStatsMaxConversations <= 0 || cfg.TrafficStatsMaxConversations > maxTrafficStatsCap {
		warnings = append(warnings, fmt.Sprintf(
			"traffic-stats-max-conversations=%d out of range (1..%d), using default %d",
			cfg.TrafficStatsMaxConversations, maxTrafficStatsCap, defaults.TrafficStatsMaxConversations))
		cfg.TrafficStatsMaxConversations = defaults.TrafficStatsMaxConversations
	}
	if cfg.DNSStatsMaxDomains <= 0 || cfg.DNSStatsMaxDomains < minDNSStatsMaxDomains || cfg.DNSStatsMaxDomains > maxDNSStatsMaxDomains {
		warnings = append(warnings, fmt.Sprintf(
			"dns-stats-max-domains=%d out of range (%d..%d), using default %d",
			cfg.DNSStatsMaxDomains, minDNSStatsMaxDomains, maxDNSStatsMaxDomains, defaults.DNSStatsMaxDomains))
		cfg.DNSStatsMaxDomains = defaults.DNSStatsMaxDomains
	}
	if cfg.DNSStatsMaxIPsPerDomain <= 0 || cfg.DNSStatsMaxIPsPerDomain < minDNSStatsMaxIPsPerDomain || cfg.DNSStatsMaxIPsPerDomain > maxDNSStatsMaxIPsPerDomain {
		warnings = append(warnings, fmt.Sprintf(
			"dns-stats-max-ips-per-domain=%d out of range (%d..%d), using default %d",
			cfg.DNSStatsMaxIPsPerDomain, minDNSStatsMaxIPsPerDomain, maxDNSStatsMaxIPsPerDomain, defaults.DNSStatsMaxIPsPerDomain))
		cfg.DNSStatsMaxIPsPerDomain = defaults.DNSStatsMaxIPsPerDomain
	}
	if cfg.DenyStatsMaxSources <= 0 || cfg.DenyStatsMaxSources > maxDenyStatsCap {
		warnings = append(warnings, fmt.Sprintf(
			"deny-stats-max-sources=%d out of range (1..%d), using default %d",
			cfg.DenyStatsMaxSources, maxDenyStatsCap, defaults.DenyStatsMaxSources))
		cfg.DenyStatsMaxSources = defaults.DenyStatsMaxSources
	}
	if cfg.DenyStatsMaxPorts <= 0 || cfg.DenyStatsMaxPorts > maxDenyStatsCap {
		warnings = append(warnings, fmt.Sprintf(
			"deny-stats-max-ports=%d out of range (1..%d), using default %d",
			cfg.DenyStatsMaxPorts, maxDenyStatsCap, defaults.DenyStatsMaxPorts))
		cfg.DenyStatsMaxPorts = defaults.DenyStatsMaxPorts
	}
	if cfg.TrafficLogBufferCapacity < minTrafficLogBufferCapacity || cfg.TrafficLogBufferCapacity > maxTrafficLogBufferCapacity {
		warnings = append(warnings, fmt.Sprintf(
			"traffic-log-buffer-capacity=%d out of range (%d..%d), using default %d",
			cfg.TrafficLogBufferCapacity, minTrafficLogBufferCapacity, maxTrafficLogBufferCapacity, defaults.TrafficLogBufferCapacity))
		cfg.TrafficLogBufferCapacity = defaults.TrafficLogBufferCapacity
	}
	if cfg.MaxObjectEntries < minMaxObjectEntries || cfg.MaxObjectEntries > maxMaxObjectEntries {
		warnings = append(warnings, fmt.Sprintf(
			"max-object-entries=%d out of range (%d..%d), using default %d",
			cfg.MaxObjectEntries, minMaxObjectEntries, maxMaxObjectEntries, defaults.MaxObjectEntries))
		cfg.MaxObjectEntries = defaults.MaxObjectEntries
	}
	if cfg.MaxExpandedRulesPerPolicy < minMaxExpandedRulesPerPolicy || cfg.MaxExpandedRulesPerPolicy > maxMaxExpandedRulesPerPolicy {
		warnings = append(warnings, fmt.Sprintf(
			"max-expanded-rules-per-policy=%d out of range (%d..%d), using default %d",
			cfg.MaxExpandedRulesPerPolicy, minMaxExpandedRulesPerPolicy, maxMaxExpandedRulesPerPolicy, defaults.MaxExpandedRulesPerPolicy))
		cfg.MaxExpandedRulesPerPolicy = defaults.MaxExpandedRulesPerPolicy
	}

	return cfg, warnings, nil
}

// applyKey type-converts value per key's field type and stores it into cfg.
func applyKey(cfg *Config, key, value string) error {
	switch key {
	case keyPort:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.Port = n
	case keyDBPath:
		cfg.DBPath = value
	case keyMock:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.Mock = b
	case keyMockFromReal:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.MockFromReal = b
	case keyDisableEdit:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.DisableEdit = b
	case keyAllowEditSystemRoutes:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.AllowEditSystemRoutes = b
	case keyEnableEditSystemRoute:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.EnableEditSystemRoute = b
	case keyPrioritizeKernelRoutes:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.PrioritizeKernelRoutes = b
	case keyDockerCompat:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.DockerCompat = b
	case keyHTTPSPort:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.HTTPSPort = n
	case keyTLSDir:
		cfg.TLSDir = value
	case keyAllowDevCORS:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.AllowDevCORS = b
	case keyDNSStatsMaxPairs:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		// Range-checking (<=0, absurdly large) is deliberately NOT done here —
		// see Resolve's post-processing pass, which clamps + warns instead of
		// failing fast for an out-of-range (but syntactically valid) value.
		cfg.DNSStatsMaxPairs = n
	case keyDNSStatsMaxClients:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.DNSStatsMaxClients = n
	case keyTrafficStatsMaxHosts:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		// Range-checking is deliberately NOT done here — see Resolve's
		// post-processing pass (clamp + warn, not fail-fast).
		cfg.TrafficStatsMaxHosts = n
	case keyTrafficStatsMaxDests:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.TrafficStatsMaxDests = n
	case keyTrafficStatsMaxConversations:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.TrafficStatsMaxConversations = n
	case keyDNSStatsMaxDomains:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		// Range-checking is deliberately NOT done here — see Resolve's
		// post-processing pass (clamp + warn, not fail-fast).
		cfg.DNSStatsMaxDomains = n
	case keyDNSStatsMaxIPsPerDomain:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.DNSStatsMaxIPsPerDomain = n
	case keyIPInfoEnabled:
		b, err := strconv.ParseBool(value)
		if err != nil {
			return fmt.Errorf("invalid bool for %q: %q: %w", key, value, err)
		}
		cfg.IPInfoEnabled = b
	case keyDenyStatsMaxSources:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		// Range-checking is deliberately NOT done here — see Resolve's
		// post-processing pass (clamp + warn, not fail-fast).
		cfg.DenyStatsMaxSources = n
	case keyDenyStatsMaxPorts:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.DenyStatsMaxPorts = n
	case keyTrafficLogBufferCapacity:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		// Range-checking is deliberately NOT done here — see Resolve's
		// post-processing pass (clamp + warn, not fail-fast).
		cfg.TrafficLogBufferCapacity = n
	case keyMaxObjectEntries:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		// Range-checking is deliberately NOT done here — see Resolve's
		// post-processing pass (clamp + warn, not fail-fast).
		cfg.MaxObjectEntries = n
	case keyMaxExpandedRulesPerPolicy:
		n, err := strconv.Atoi(value)
		if err != nil {
			return fmt.Errorf("invalid int for %q: %q: %w", key, value, err)
		}
		cfg.MaxExpandedRulesPerPolicy = n
	default:
		// Unreachable: callers only invoke applyKey for keys that passed
		// isKnownKey. Kept as a safety net rather than a silent no-op.
		return fmt.Errorf("internal error: applyKey called with unknown key %q", key)
	}
	return nil
}

// keyValue renders a single Config field as its "key=value" string form,
// mirroring the type conversions in applyKey.
func keyValue(cfg Config, key string) string {
	switch key {
	case keyPort:
		return strconv.Itoa(cfg.Port)
	case keyDBPath:
		return cfg.DBPath
	case keyMock:
		return strconv.FormatBool(cfg.Mock)
	case keyMockFromReal:
		return strconv.FormatBool(cfg.MockFromReal)
	case keyDisableEdit:
		return strconv.FormatBool(cfg.DisableEdit)
	case keyAllowEditSystemRoutes:
		return strconv.FormatBool(cfg.AllowEditSystemRoutes)
	case keyEnableEditSystemRoute:
		return strconv.FormatBool(cfg.EnableEditSystemRoute)
	case keyPrioritizeKernelRoutes:
		return strconv.FormatBool(cfg.PrioritizeKernelRoutes)
	case keyDockerCompat:
		return strconv.FormatBool(cfg.DockerCompat)
	case keyHTTPSPort:
		return strconv.Itoa(cfg.HTTPSPort)
	case keyTLSDir:
		return cfg.TLSDir
	case keyAllowDevCORS:
		return strconv.FormatBool(cfg.AllowDevCORS)
	case keyDNSStatsMaxPairs:
		return strconv.Itoa(cfg.DNSStatsMaxPairs)
	case keyDNSStatsMaxClients:
		return strconv.Itoa(cfg.DNSStatsMaxClients)
	case keyTrafficStatsMaxHosts:
		return strconv.Itoa(cfg.TrafficStatsMaxHosts)
	case keyTrafficStatsMaxDests:
		return strconv.Itoa(cfg.TrafficStatsMaxDests)
	case keyTrafficStatsMaxConversations:
		return strconv.Itoa(cfg.TrafficStatsMaxConversations)
	case keyDNSStatsMaxDomains:
		return strconv.Itoa(cfg.DNSStatsMaxDomains)
	case keyDNSStatsMaxIPsPerDomain:
		return strconv.Itoa(cfg.DNSStatsMaxIPsPerDomain)
	case keyIPInfoEnabled:
		return strconv.FormatBool(cfg.IPInfoEnabled)
	case keyDenyStatsMaxSources:
		return strconv.Itoa(cfg.DenyStatsMaxSources)
	case keyDenyStatsMaxPorts:
		return strconv.Itoa(cfg.DenyStatsMaxPorts)
	case keyTrafficLogBufferCapacity:
		return strconv.Itoa(cfg.TrafficLogBufferCapacity)
	case keyMaxObjectEntries:
		return strconv.Itoa(cfg.MaxObjectEntries)
	case keyMaxExpandedRulesPerPolicy:
		return strconv.Itoa(cfg.MaxExpandedRulesPerPolicy)
	default:
		return ""
	}
}

// Write serializes cfg as "key=value" lines (in the fixed KnownKeys order,
// so the file diffs cleanly between runs) preceded by a header comment, to
// w. The output round-trips through Parse+Resolve back to an equal Config.
func Write(w io.Writer, cfg Config) error {
	header := "" +
		"# PiGate bootstrap configuration.\n" +
		"# Generated automatically; you may edit this file.\n" +
		"# Format: key=value, one per line. Lines starting with '#' are comments.\n" +
		"# NOTE: a CLI flag passed to pigate always overrides the matching value here.\n"
	if _, err := io.WriteString(w, header); err != nil {
		return fmt.Errorf("config: write header: %w", err)
	}
	for _, key := range orderedKeys {
		line := key + "=" + keyValue(cfg, key) + "\n"
		if _, err := io.WriteString(w, line); err != nil {
			return fmt.Errorf("config: write key %q: %w", key, err)
		}
	}
	return nil
}
