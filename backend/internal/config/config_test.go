package config

import (
	"bytes"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	t.Run("normal key=value", func(t *testing.T) {
		in := "port=8080\nmock=false\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		want := map[string]string{"port": "8080", "mock": "false"}
		if len(got) != len(want) || got["port"] != "8080" || got["mock"] != "false" {
			t.Fatalf("got %v, want %v", got, want)
		}
	})

	t.Run("comments and blank lines skipped", func(t *testing.T) {
		in := "# a comment\n\nport=8080\n   \n# another\nmock=true\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 2 || got["port"] != "8080" || got["mock"] != "true" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("indented comment/blank still skipped after trim", func(t *testing.T) {
		in := "   # indented comment\n   \nport=1\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(got) != 1 || got["port"] != "1" {
			t.Fatalf("got %v", got)
		}
	})

	t.Run("equals sign inside value (path-like)", func(t *testing.T) {
		in := "db=/var/lib/pigate/pigate.db?opt=1\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got["db"] != "/var/lib/pigate/pigate.db?opt=1" {
			t.Fatalf("got %q", got["db"])
		}
	})

	t.Run("empty value is valid (tls-dir=)", func(t *testing.T) {
		in := "tls-dir=\n"
		got, err := Parse(strings.NewReader(in))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		val, ok := got["tls-dir"]
		if !ok || val != "" {
			t.Fatalf("got %q, ok=%v", val, ok)
		}
	})

	t.Run("line without = is an error", func(t *testing.T) {
		in := "port=8080\nthisisnotvalid\n"
		_, err := Parse(strings.NewReader(in))
		if err == nil {
			t.Fatalf("expected error, got nil")
		}
	})
}

func TestResolve(t *testing.T) {
	t.Run("defaults only, no file, no explicit", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg != Defaults() {
			t.Fatalf("got %+v, want defaults %+v", cfg, Defaults())
		}
	})

	t.Run("file overrides default", func(t *testing.T) {
		fileVals := map[string]string{"mock": "false", "port": "9000"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.Mock != false || cfg.Port != 9000 {
			t.Fatalf("got %+v", cfg)
		}
		// Untouched fields keep defaults.
		if cfg.DBPath != Defaults().DBPath {
			t.Fatalf("expected untouched DBPath to stay default, got %q", cfg.DBPath)
		}
	})

	t.Run("explicit flag overrides file", func(t *testing.T) {
		fileVals := map[string]string{"port": "9000"}
		explicit := map[string]string{"port": "1234"}
		cfg, _, err := Resolve(Defaults(), fileVals, explicit)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Port != 1234 {
			t.Fatalf("got port=%d, want 1234", cfg.Port)
		}
	})

	t.Run("explicit flag wins even when file also sets it", func(t *testing.T) {
		fileVals := map[string]string{"mock": "false"}
		explicit := map[string]string{"mock": "true"}
		cfg, _, err := Resolve(Defaults(), fileVals, explicit)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if cfg.Mock != true {
			t.Fatalf("got mock=%v, want true (flag must win over file)", cfg.Mock)
		}
	})

	t.Run("unknown key produces a warning, not an error", func(t *testing.T) {
		fileVals := map[string]string{"totally-unknown-key": "1"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 1 {
			t.Fatalf("expected 1 warning, got %v", warns)
		}
		if cfg != Defaults() {
			t.Fatalf("unknown key must not otherwise alter config, got %+v", cfg)
		}
	})

	t.Run("malformed int fails fast", func(t *testing.T) {
		fileVals := map[string]string{"port": "abc"}
		_, _, err := Resolve(Defaults(), fileVals, nil)
		if err == nil {
			t.Fatalf("expected error for port=abc, got nil")
		}
	})

	t.Run("malformed bool fails fast", func(t *testing.T) {
		fileVals := map[string]string{"mock": "x"}
		_, _, err := Resolve(Defaults(), fileVals, nil)
		if err == nil {
			t.Fatalf("expected error for mock=x, got nil")
		}
	})

	t.Run("dns stats keys default when absent", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DNSStatsMaxPairs != 2400 || cfg.DNSStatsMaxClients != 200 {
			t.Fatalf("got pairs=%d clients=%d, want 2400/200", cfg.DNSStatsMaxPairs, cfg.DNSStatsMaxClients)
		}
	})

	t.Run("file overrides dns stats keys", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-pairs": "5000", "dns-stats-max-clients": "500"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DNSStatsMaxPairs != 5000 || cfg.DNSStatsMaxClients != 500 {
			t.Fatalf("got pairs=%d clients=%d, want 5000/500", cfg.DNSStatsMaxPairs, cfg.DNSStatsMaxClients)
		}
	})

	t.Run("non-integer dns stats value fails fast", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-pairs": "abc"}
		_, _, err := Resolve(Defaults(), fileVals, nil)
		if err == nil {
			t.Fatalf("expected error for dns-stats-max-pairs=abc, got nil")
		}
	})

	t.Run("zero/negative clamps to default with a warning", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-pairs": "0", "dns-stats-max-clients": "-1"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 2 {
			t.Fatalf("expected 2 warnings, got %v", warns)
		}
		if cfg.DNSStatsMaxPairs != 2400 || cfg.DNSStatsMaxClients != 200 {
			t.Fatalf("got pairs=%d clients=%d, want defaults 2400/200", cfg.DNSStatsMaxPairs, cfg.DNSStatsMaxClients)
		}
	})

	t.Run("absurdly large clamps to default with a warning", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-pairs": "999999"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 1 {
			t.Fatalf("expected 1 warning, got %v", warns)
		}
		if cfg.DNSStatsMaxPairs != 2400 {
			t.Fatalf("got pairs=%d, want default 2400", cfg.DNSStatsMaxPairs)
		}
	})

	t.Run("dns stats domain-ip keys default when absent", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DNSStatsMaxDomains != 1000 || cfg.DNSStatsMaxIPsPerDomain != 32 {
			t.Fatalf("got domains=%d ipsPerDomain=%d, want 1000/32", cfg.DNSStatsMaxDomains, cfg.DNSStatsMaxIPsPerDomain)
		}
	})

	t.Run("file overrides dns stats domain-ip keys", func(t *testing.T) {
		// Use 40 here (not 32, the new default) so this test can't pass by
		// accident just because the file value happens to equal the default.
		fileVals := map[string]string{"dns-stats-max-domains": "5000", "dns-stats-max-ips-per-domain": "40"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DNSStatsMaxDomains != 5000 || cfg.DNSStatsMaxIPsPerDomain != 40 {
			t.Fatalf("got domains=%d ipsPerDomain=%d, want 5000/40", cfg.DNSStatsMaxDomains, cfg.DNSStatsMaxIPsPerDomain)
		}
	})

	t.Run("non-integer dns stats domain-ip value fails fast", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-domains": "abc"}
		_, _, err := Resolve(Defaults(), fileVals, nil)
		if err == nil {
			t.Fatalf("expected error for dns-stats-max-domains=abc, got nil")
		}
	})

	t.Run("dns stats domain-ip zero/negative clamps to default with a warning", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-domains": "0", "dns-stats-max-ips-per-domain": "-1"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 2 {
			t.Fatalf("expected 2 warnings, got %v", warns)
		}
		if cfg.DNSStatsMaxDomains != 1000 || cfg.DNSStatsMaxIPsPerDomain != 32 {
			t.Fatalf("got domains=%d ipsPerDomain=%d, want defaults 1000/32", cfg.DNSStatsMaxDomains, cfg.DNSStatsMaxIPsPerDomain)
		}
	})

	t.Run("dns stats domain-ip below accepted range clamps to default with a warning", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-domains": "50", "dns-stats-max-ips-per-domain": "1"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 2 {
			t.Fatalf("expected 2 warnings, got %v", warns)
		}
		if cfg.DNSStatsMaxDomains != 1000 || cfg.DNSStatsMaxIPsPerDomain != 32 {
			t.Fatalf("got domains=%d ipsPerDomain=%d, want defaults 1000/32", cfg.DNSStatsMaxDomains, cfg.DNSStatsMaxIPsPerDomain)
		}
	})

	t.Run("dns stats domain-ip above accepted range clamps to default with a warning", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-domains": "999999", "dns-stats-max-ips-per-domain": "65"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 2 {
			t.Fatalf("expected 2 warnings, got %v", warns)
		}
		if cfg.DNSStatsMaxDomains != 1000 || cfg.DNSStatsMaxIPsPerDomain != 32 {
			t.Fatalf("got domains=%d ipsPerDomain=%d, want defaults 1000/32", cfg.DNSStatsMaxDomains, cfg.DNSStatsMaxIPsPerDomain)
		}
	})
}

func TestWriteParseRoundTrip(t *testing.T) {
	cfg := Defaults()
	cfg.Port = 9999
	cfg.Mock = false
	cfg.DBPath = "/var/lib/pigate/pigate.db"
	cfg.HTTPSPort = 443
	cfg.DockerCompat = true
	cfg.TLSDir = ""
	cfg.DNSStatsMaxPairs = 3000
	cfg.DNSStatsMaxClients = 300
	cfg.DNSStatsMaxDomains = 2000
	cfg.DNSStatsMaxIPsPerDomain = 24
	cfg.IPInfoEnabled = true
	cfg.DenyStatsMaxSources = 800
	cfg.DenyStatsMaxPorts = 400

	var buf bytes.Buffer
	if err := Write(&buf, cfg); err != nil {
		t.Fatalf("Write failed: %v", err)
	}

	fileVals, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	got, warns, err := Resolve(Defaults(), fileVals, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings from round-trip: %v", warns)
	}
	if got != cfg {
		t.Fatalf("round-trip mismatch:\n got  %+v\n want %+v", got, cfg)
	}
}

func TestWriteParseRoundTripDefaults(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Defaults()); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	fileVals, err := Parse(&buf)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	got, _, err := Resolve(Defaults(), fileVals, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if got != Defaults() {
		t.Fatalf("got %+v, want defaults %+v", got, Defaults())
	}
}

func TestKnownKeys(t *testing.T) {
	keys := KnownKeys()
	if len(keys) != 22 {
		t.Fatalf("expected 22 known keys, got %d: %v", len(keys), keys)
	}
	// "config" and "v" must never be treated as config-file keys.
	for _, k := range keys {
		if k == "config" || k == "v" {
			t.Fatalf("KnownKeys must not include %q", k)
		}
	}
	var hasPairs, hasClients bool
	for _, k := range keys {
		if k == "dns-stats-max-pairs" {
			hasPairs = true
		}
		if k == "dns-stats-max-clients" {
			hasClients = true
		}
	}
	if !hasPairs || !hasClients {
		t.Fatalf("expected dns-stats-max-pairs/dns-stats-max-clients in KnownKeys, got %v", keys)
	}
	var hasHosts, hasDests, hasConversations bool
	for _, k := range keys {
		switch k {
		case "traffic-stats-max-hosts":
			hasHosts = true
		case "traffic-stats-max-dests":
			hasDests = true
		case "traffic-stats-max-conversations":
			hasConversations = true
		}
	}
	if !hasHosts || !hasDests || !hasConversations {
		t.Fatalf("expected traffic-stats-max-hosts/-dests/-conversations in KnownKeys, got %v", keys)
	}
	var hasMaxDomains, hasMaxIPsPerDomain bool
	for _, k := range keys {
		switch k {
		case "dns-stats-max-domains":
			hasMaxDomains = true
		case "dns-stats-max-ips-per-domain":
			hasMaxIPsPerDomain = true
		}
	}
	if !hasMaxDomains || !hasMaxIPsPerDomain {
		t.Fatalf("expected dns-stats-max-domains/dns-stats-max-ips-per-domain in KnownKeys, got %v", keys)
	}
	var hasIPInfoEnabled bool
	for _, k := range keys {
		if k == "ipinfo-enabled" {
			hasIPInfoEnabled = true
		}
	}
	if !hasIPInfoEnabled {
		t.Fatalf("expected ipinfo-enabled in KnownKeys, got %v", keys)
	}
	var hasDenySources, hasDenyPorts bool
	for _, k := range keys {
		switch k {
		case "deny-stats-max-sources":
			hasDenySources = true
		case "deny-stats-max-ports":
			hasDenyPorts = true
		}
	}
	if !hasDenySources || !hasDenyPorts {
		t.Fatalf("expected deny-stats-max-sources/deny-stats-max-ports in KnownKeys, got %v", keys)
	}
}

// TestIPInfoEnabledDefaultFalse locks in plan T-06's explicit requirement
// that this key defaults to false and requires no config to run.
func TestIPInfoEnabledDefaultFalse(t *testing.T) {
	if Defaults().IPInfoEnabled != false {
		t.Fatalf("expected ipinfo-enabled to default to false")
	}
	got, warns, err := Resolve(Defaults(), map[string]string{}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if len(warns) != 0 {
		t.Fatalf("unexpected warnings: %v", warns)
	}
	if got.IPInfoEnabled != false {
		t.Fatalf("expected ipinfo-enabled to stay false with no file value set")
	}
}

func TestIPInfoEnabledFileValue(t *testing.T) {
	got, _, err := Resolve(Defaults(), map[string]string{"ipinfo-enabled": "true"}, nil)
	if err != nil {
		t.Fatalf("Resolve failed: %v", err)
	}
	if !got.IPInfoEnabled {
		t.Fatalf("expected ipinfo-enabled=true from file to be applied")
	}
}

// TestResolve_DenyStatsMaxSourcesPorts covers the deny-stats-max-* keys'
// default/override/out-of-range behavior (docs/ref/todo/
// statistics-capacity-visibility-plan.md T-14) — same pattern as the
// DNSStatsMaxDomains/DNSStatsMaxIPsPerDomain coverage above.
func TestResolve_DenyStatsMaxSourcesPorts(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DenyStatsMaxSources != 500 || cfg.DenyStatsMaxPorts != 300 {
			t.Fatalf("got sources=%d ports=%d, want defaults 500/300", cfg.DenyStatsMaxSources, cfg.DenyStatsMaxPorts)
		}
	})

	t.Run("file override", func(t *testing.T) {
		fileVals := map[string]string{"deny-stats-max-sources": "800", "deny-stats-max-ports": "400"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DenyStatsMaxSources != 800 || cfg.DenyStatsMaxPorts != 400 {
			t.Fatalf("got sources=%d ports=%d, want 800/400", cfg.DenyStatsMaxSources, cfg.DenyStatsMaxPorts)
		}
	})

	t.Run("out of range clamps to default with warning", func(t *testing.T) {
		fileVals := map[string]string{"deny-stats-max-sources": "-1", "deny-stats-max-ports": "999999"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 2 {
			t.Fatalf("expected 2 warnings, got %v", warns)
		}
		if cfg.DenyStatsMaxSources != 500 || cfg.DenyStatsMaxPorts != 300 {
			t.Fatalf("got sources=%d ports=%d, want defaults 500/300", cfg.DenyStatsMaxSources, cfg.DenyStatsMaxPorts)
		}
	})

	t.Run("non-integer is a fail-fast error", func(t *testing.T) {
		fileVals := map[string]string{"deny-stats-max-sources": "not-a-number"}
		if _, _, err := Resolve(Defaults(), fileVals, nil); err == nil {
			t.Fatalf("expected error for non-integer deny-stats-max-sources")
		}
	})
}
