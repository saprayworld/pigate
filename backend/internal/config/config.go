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
//	Defaults()                                  -> code defaults (1:1 with cmd/pigate/main.go flags)
//	Parse(r io.Reader)                          -> raw "key=value" syntax, no type conversion
//	Resolve(defaults, fileVals, explicit)       -> merges + type-converts, defaults < file < explicit
//	Write(w io.Writer, cfg Config)               -> serializes a Config back to "key=value" (round-trips with Parse)
//
// Precedence (low to high): code default < config file < CLI flag explicitly
// set by the user. main.go is responsible for I/O (reading/writing the actual
// file, calling flag.Visit) and for logging; this package never touches a
// filesystem path or the log package directly, so it stays 100% unit
// testable (see config_test.go).
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
