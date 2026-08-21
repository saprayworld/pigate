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

	t.Run("dns stats max blocked domains defaults when absent", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DNSStatsMaxBlockedDomains != 1000 {
			t.Fatalf("got blocked domains=%d, want 1000", cfg.DNSStatsMaxBlockedDomains)
		}
	})

	t.Run("file overrides dns stats max blocked domains", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-blocked-domains": "5000"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.DNSStatsMaxBlockedDomains != 5000 {
			t.Fatalf("got blocked domains=%d, want 5000", cfg.DNSStatsMaxBlockedDomains)
		}
	})

	t.Run("non-integer dns stats max blocked domains fails fast", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-blocked-domains": "abc"}
		_, _, err := Resolve(Defaults(), fileVals, nil)
		if err == nil {
			t.Fatalf("expected error for dns-stats-max-blocked-domains=abc, got nil")
		}
	})

	t.Run("dns stats max blocked domains zero/negative clamps to default with a warning", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-blocked-domains": "0"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 1 {
			t.Fatalf("expected 1 warning, got %v", warns)
		}
		if cfg.DNSStatsMaxBlockedDomains != 1000 {
			t.Fatalf("got blocked domains=%d, want default 1000", cfg.DNSStatsMaxBlockedDomains)
		}
	})

	t.Run("dns stats max blocked domains absurdly large clamps to default with a warning", func(t *testing.T) {
		fileVals := map[string]string{"dns-stats-max-blocked-domains": "999999"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if len(warns) != 1 {
			t.Fatalf("expected 1 warning, got %v", warns)
		}
		if cfg.DNSStatsMaxBlockedDomains != 1000 {
			t.Fatalf("got blocked domains=%d, want default 1000", cfg.DNSStatsMaxBlockedDomains)
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
	cfg.TrafficLogBufferCapacity = 20000
	cfg.MaxObjectEntries = 128
	cfg.MaxExpandedRulesPerPolicy = 8192
	cfg.FQDNRefreshEnabled = false
	cfg.FQDNRefreshIntervalSeconds = 600
	cfg.FQDNRefreshRetryIntervalSeconds = 60
	cfg.MonitoredCounterFlushIntervalSeconds = 120
	cfg.MonitoredEndpointsEnabled = false
	cfg.MonitoredEndpointsMaxPerRule = 250

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
	if len(keys) != 33 {
		t.Fatalf("expected 33 known keys, got %d: %v", len(keys), keys)
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
	var hasTrafficLogBufferCapacity bool
	for _, k := range keys {
		if k == "traffic-log-buffer-capacity" {
			hasTrafficLogBufferCapacity = true
		}
	}
	if !hasTrafficLogBufferCapacity {
		t.Fatalf("expected traffic-log-buffer-capacity in KnownKeys, got %v", keys)
	}
	var hasMaxObjectEntries, hasMaxExpandedRulesPerPolicy bool
	for _, k := range keys {
		switch k {
		case "max-object-entries":
			hasMaxObjectEntries = true
		case "max-expanded-rules-per-policy":
			hasMaxExpandedRulesPerPolicy = true
		}
	}
	if !hasMaxObjectEntries || !hasMaxExpandedRulesPerPolicy {
		t.Fatalf("expected max-object-entries/max-expanded-rules-per-policy in KnownKeys, got %v", keys)
	}
	var hasFQDNEnabled, hasFQDNInterval, hasFQDNRetry, hasMonitoredFlush bool
	for _, k := range keys {
		switch k {
		case "fqdn-refresh-enabled":
			hasFQDNEnabled = true
		case "fqdn-refresh-interval-seconds":
			hasFQDNInterval = true
		case "fqdn-refresh-retry-interval-seconds":
			hasFQDNRetry = true
		case "monitored-counter-flush-interval-seconds":
			hasMonitoredFlush = true
		}
	}
	if !hasFQDNEnabled || !hasFQDNInterval || !hasFQDNRetry || !hasMonitoredFlush {
		t.Fatalf("expected fqdn-refresh-enabled/fqdn-refresh-interval-seconds/fqdn-refresh-retry-interval-seconds/monitored-counter-flush-interval-seconds in KnownKeys, got %v", keys)
	}
	var hasEndpointsEnabled, hasEndpointsMaxPerRule bool
	for _, k := range keys {
		switch k {
		case "monitored-endpoints-enabled":
			hasEndpointsEnabled = true
		case "monitored-endpoints-max-per-rule":
			hasEndpointsMaxPerRule = true
		}
	}
	if !hasEndpointsEnabled || !hasEndpointsMaxPerRule {
		t.Fatalf("expected monitored-endpoints-enabled/monitored-endpoints-max-per-rule in KnownKeys, got %v", keys)
	}
	if keys[len(keys)-4] != "monitored-endpoints-enabled" || keys[len(keys)-3] != "monitored-endpoints-max-per-rule" {
		t.Fatalf("expected monitored-endpoints-enabled/monitored-endpoints-max-per-rule to be the fourth/third-to-last keys, got %v", keys)
	}
	var hasMaxPolicyInterfacesPerDirection bool
	for _, k := range keys {
		if k == "max-policy-interfaces-per-direction" {
			hasMaxPolicyInterfacesPerDirection = true
		}
	}
	if !hasMaxPolicyInterfacesPerDirection {
		t.Fatalf("expected max-policy-interfaces-per-direction in KnownKeys, got %v", keys)
	}
	if keys[len(keys)-2] != "max-policy-interfaces-per-direction" {
		t.Fatalf("expected max-policy-interfaces-per-direction to be the second-to-last key, got %v", keys)
	}
	var hasDNSStatsMaxBlockedDomains bool
	for _, k := range keys {
		if k == "dns-stats-max-blocked-domains" {
			hasDNSStatsMaxBlockedDomains = true
		}
	}
	if !hasDNSStatsMaxBlockedDomains {
		t.Fatalf("expected dns-stats-max-blocked-domains in KnownKeys, got %v", keys)
	}
	if keys[len(keys)-1] != "dns-stats-max-blocked-domains" {
		t.Fatalf("expected dns-stats-max-blocked-domains to be the last key, got %v", keys)
	}
}

// TestResolve_MonitoredEndpoints covers monitored-endpoints-enabled/
// monitored-endpoints-max-per-rule's default/override/out-of-range/
// non-integer behavior (docs/ref/todo/persisted-rule-endpoints-plan.md E-D9,
// issue #141 follow-up) — same pattern as TestResolve_DenyStatsMaxSourcesPorts
// above.
func TestResolve_MonitoredEndpoints(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if !cfg.MonitoredEndpointsEnabled {
			t.Fatalf("expected monitored-endpoints-enabled to default to true")
		}
		if cfg.MonitoredEndpointsMaxPerRule != 1000 {
			t.Fatalf("got monitored-endpoints-max-per-rule=%d, want default 1000", cfg.MonitoredEndpointsMaxPerRule)
		}
	})

	t.Run("file override", func(t *testing.T) {
		fileVals := map[string]string{
			"monitored-endpoints-enabled":      "false",
			"monitored-endpoints-max-per-rule": "250",
		}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MonitoredEndpointsEnabled {
			t.Fatalf("expected monitored-endpoints-enabled=false to be applied")
		}
		if cfg.MonitoredEndpointsMaxPerRule != 250 {
			t.Fatalf("got monitored-endpoints-max-per-rule=%d, want 250", cfg.MonitoredEndpointsMaxPerRule)
		}
	})

	t.Run("out of range clamps to default with warning", func(t *testing.T) {
		for _, v := range []string{"0", "-1", "19", "5001"} {
			fileVals := map[string]string{"monitored-endpoints-max-per-rule": v}
			cfg, warns, err := Resolve(Defaults(), fileVals, nil)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", v, err)
			}
			if len(warns) != 1 {
				t.Fatalf("Resolve(%q): expected 1 warning, got %v", v, warns)
			}
			if cfg.MonitoredEndpointsMaxPerRule != 1000 {
				t.Fatalf("Resolve(%q): got %d, want default 1000", v, cfg.MonitoredEndpointsMaxPerRule)
			}
		}
	})

	t.Run("boundary values pass without warning", func(t *testing.T) {
		for _, v := range []string{"20", "5000"} {
			fileVals := map[string]string{"monitored-endpoints-max-per-rule": v}
			cfg, warns, err := Resolve(Defaults(), fileVals, nil)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", v, err)
			}
			if len(warns) != 0 {
				t.Fatalf("Resolve(%q): unexpected warnings: %v", v, warns)
			}
			want := 20
			if v == "5000" {
				want = 5000
			}
			if cfg.MonitoredEndpointsMaxPerRule != want {
				t.Fatalf("Resolve(%q): got %d, want %d", v, cfg.MonitoredEndpointsMaxPerRule, want)
			}
		}
	})

	t.Run("non-integer is a fail-fast error", func(t *testing.T) {
		fileVals := map[string]string{"monitored-endpoints-max-per-rule": "abc"}
		if _, _, err := Resolve(Defaults(), fileVals, nil); err == nil {
			t.Fatalf("expected error for non-integer monitored-endpoints-max-per-rule")
		}
	})

	t.Run("non-bool monitored-endpoints-enabled is a fail-fast error", func(t *testing.T) {
		if _, _, err := Resolve(Defaults(), map[string]string{"monitored-endpoints-enabled": "notabool"}, nil); err == nil {
			t.Fatalf("expected error for non-bool monitored-endpoints-enabled")
		}
	})
}

// TestWriteParseRoundTrip_MonitoredEndpointsWrittenLast locks in that the two
// monitored-endpoints keys are still appended right before the newer
// max-policy-interfaces-per-direction key at the very end of the generated
// file (Caution: must not be inserted alphabetically among existing keys).
func TestWriteParseRoundTrip_MonitoredEndpointsWrittenLast(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Defaults()); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("expected at least 4 lines, got %d", len(lines))
	}
	if lines[len(lines)-4] != "monitored-endpoints-enabled=true" {
		t.Fatalf("expected monitored-endpoints-enabled=true as fourth-to-last line, got %q", lines[len(lines)-4])
	}
	if lines[len(lines)-3] != "monitored-endpoints-max-per-rule=1000" {
		t.Fatalf("expected monitored-endpoints-max-per-rule=1000 as third-to-last line, got %q", lines[len(lines)-3])
	}
}

// TestWriteParseRoundTrip_MaxPolicyInterfacesPerDirectionWrittenLast locks in
// that max-policy-interfaces-per-direction (docs/ref/todo/
// multi-interface-firewall-rule-plan.md §2.2, D-2) is appended right before
// the newer dns-stats-max-blocked-domains key at the very end of the
// generated file.
func TestWriteParseRoundTrip_MaxPolicyInterfacesPerDirectionWrittenLast(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Defaults()); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d", len(lines))
	}
	if lines[len(lines)-2] != "max-policy-interfaces-per-direction=8" {
		t.Fatalf("expected max-policy-interfaces-per-direction=8 as second-to-last line, got %q", lines[len(lines)-2])
	}
}

// TestWriteParseRoundTrip_DNSStatsMaxBlockedDomainsWrittenLast locks in that
// the newest file-only key (docs/ref/todo/
// dns-blocked-query-statistics-plan.md T-07) is appended at the very end of
// the generated file.
func TestWriteParseRoundTrip_DNSStatsMaxBlockedDomainsWrittenLast(t *testing.T) {
	var buf bytes.Buffer
	if err := Write(&buf, Defaults()); err != nil {
		t.Fatalf("Write failed: %v", err)
	}
	lines := strings.Split(strings.TrimRight(buf.String(), "\n"), "\n")
	if len(lines) < 1 {
		t.Fatalf("expected at least 1 line, got %d", len(lines))
	}
	if lines[len(lines)-1] != "dns-stats-max-blocked-domains=1000" {
		t.Fatalf("expected dns-stats-max-blocked-domains=1000 as last line, got %q", lines[len(lines)-1])
	}
}

// TestResolve_MaxPolicyInterfacesPerDirection covers docs/ref/todo/
// multi-interface-firewall-rule-plan.md T-02/T-10: default 8, file override,
// clamp+warn out of range (1..64), and fail-fast on a non-integer value.
func TestResolve_MaxPolicyInterfacesPerDirection(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxPolicyInterfacesPerDirection != 8 {
			t.Fatalf("got max-policy-interfaces-per-direction=%d, want default 8", cfg.MaxPolicyInterfacesPerDirection)
		}
	})

	t.Run("file override", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), map[string]string{"max-policy-interfaces-per-direction": "20"}, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxPolicyInterfacesPerDirection != 20 {
			t.Fatalf("got max-policy-interfaces-per-direction=%d, want 20", cfg.MaxPolicyInterfacesPerDirection)
		}
	})

	t.Run("out of range clamps to default with warning", func(t *testing.T) {
		for _, v := range []string{"0", "-1", "65", "999"} {
			cfg, warns, err := Resolve(Defaults(), map[string]string{"max-policy-interfaces-per-direction": v}, nil)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", v, err)
			}
			if len(warns) != 1 {
				t.Fatalf("Resolve(%q): expected 1 warning, got %v", v, warns)
			}
			if cfg.MaxPolicyInterfacesPerDirection != 8 {
				t.Fatalf("Resolve(%q): got %d, want default 8", v, cfg.MaxPolicyInterfacesPerDirection)
			}
		}
	})

	t.Run("boundary values pass without warning", func(t *testing.T) {
		for _, v := range []string{"1", "64"} {
			cfg, warns, err := Resolve(Defaults(), map[string]string{"max-policy-interfaces-per-direction": v}, nil)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", v, err)
			}
			if len(warns) != 0 {
				t.Fatalf("Resolve(%q): unexpected warnings: %v", v, warns)
			}
			want := 1
			if v == "64" {
				want = 64
			}
			if cfg.MaxPolicyInterfacesPerDirection != want {
				t.Fatalf("Resolve(%q): got %d, want %d", v, cfg.MaxPolicyInterfacesPerDirection, want)
			}
		}
	})

	t.Run("non-integer is a fail-fast error", func(t *testing.T) {
		if _, _, err := Resolve(Defaults(), map[string]string{"max-policy-interfaces-per-direction": "abc"}, nil); err == nil {
			t.Fatalf("expected error for non-integer max-policy-interfaces-per-direction")
		}
	})
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

// TestResolve_FQDNRefreshAndMonitoredCounterFlush covers the four new
// file-only keys' default/override/out-of-range/non-integer behavior
// (docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md D-3, issue #141)
// — same pattern as TestResolve_DenyStatsMaxSourcesPorts above.
func TestResolve_FQDNRefreshAndMonitoredCounterFlush(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if !cfg.FQDNRefreshEnabled {
			t.Fatalf("expected fqdn-refresh-enabled to default to true")
		}
		if cfg.FQDNRefreshIntervalSeconds != 300 {
			t.Fatalf("got fqdn-refresh-interval-seconds=%d, want default 300", cfg.FQDNRefreshIntervalSeconds)
		}
		if cfg.FQDNRefreshRetryIntervalSeconds != 30 {
			t.Fatalf("got fqdn-refresh-retry-interval-seconds=%d, want default 30", cfg.FQDNRefreshRetryIntervalSeconds)
		}
		if cfg.MonitoredCounterFlushIntervalSeconds != 300 {
			t.Fatalf("got monitored-counter-flush-interval-seconds=%d, want default 300", cfg.MonitoredCounterFlushIntervalSeconds)
		}
	})

	t.Run("file override", func(t *testing.T) {
		fileVals := map[string]string{
			"fqdn-refresh-enabled":                     "false",
			"fqdn-refresh-interval-seconds":            "600",
			"fqdn-refresh-retry-interval-seconds":      "45",
			"monitored-counter-flush-interval-seconds": "120",
		}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.FQDNRefreshEnabled {
			t.Fatalf("expected fqdn-refresh-enabled=false to be applied")
		}
		if cfg.FQDNRefreshIntervalSeconds != 600 || cfg.FQDNRefreshRetryIntervalSeconds != 45 || cfg.MonitoredCounterFlushIntervalSeconds != 120 {
			t.Fatalf("got %+v", cfg)
		}
	})

	t.Run("out of range clamps to default with warning", func(t *testing.T) {
		fileVals := map[string]string{
			"fqdn-refresh-interval-seconds":            "10",
			"fqdn-refresh-retry-interval-seconds":      "5",
			"monitored-counter-flush-interval-seconds": "1",
		}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 3 {
			t.Fatalf("expected 3 warnings, got %v", warns)
		}
		if cfg.FQDNRefreshIntervalSeconds != 300 || cfg.FQDNRefreshRetryIntervalSeconds != 30 || cfg.MonitoredCounterFlushIntervalSeconds != 300 {
			t.Fatalf("got %+v, want defaults", cfg)
		}
	})

	t.Run("boundary values pass without warning", func(t *testing.T) {
		fileVals := map[string]string{
			"fqdn-refresh-interval-seconds":            "60",
			"fqdn-refresh-retry-interval-seconds":      "10",
			"monitored-counter-flush-interval-seconds": "30",
		}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.FQDNRefreshIntervalSeconds != 60 || cfg.FQDNRefreshRetryIntervalSeconds != 10 || cfg.MonitoredCounterFlushIntervalSeconds != 30 {
			t.Fatalf("got %+v", cfg)
		}

		fileVals = map[string]string{
			"fqdn-refresh-interval-seconds":            "86400",
			"fqdn-refresh-retry-interval-seconds":      "3600",
			"monitored-counter-flush-interval-seconds": "86400",
		}
		cfg, warns, err = Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.FQDNRefreshIntervalSeconds != 86400 || cfg.FQDNRefreshRetryIntervalSeconds != 3600 || cfg.MonitoredCounterFlushIntervalSeconds != 86400 {
			t.Fatalf("got %+v", cfg)
		}
	})

	t.Run("non-integer is a fail-fast error", func(t *testing.T) {
		if _, _, err := Resolve(Defaults(), map[string]string{"fqdn-refresh-interval-seconds": "abc"}, nil); err == nil {
			t.Fatalf("expected error for non-integer fqdn-refresh-interval-seconds")
		}
	})

	t.Run("non-bool fqdn-refresh-enabled is a fail-fast error", func(t *testing.T) {
		if _, _, err := Resolve(Defaults(), map[string]string{"fqdn-refresh-enabled": "notabool"}, nil); err == nil {
			t.Fatalf("expected error for non-bool fqdn-refresh-enabled")
		}
	})
}

// TestResolve_TrafficLogBufferCapacity covers traffic-log-buffer-capacity's
// default/override/out-of-range/non-integer behavior (docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-00/T-06, issue #134) — same pattern
// as TestResolve_DenyStatsMaxSourcesPorts above.
func TestResolve_TrafficLogBufferCapacity(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.TrafficLogBufferCapacity != 10000 {
			t.Fatalf("got %d, want default 10000", cfg.TrafficLogBufferCapacity)
		}
	})

	t.Run("file override", func(t *testing.T) {
		fileVals := map[string]string{"traffic-log-buffer-capacity": "20000"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.TrafficLogBufferCapacity != 20000 {
			t.Fatalf("got %d, want 20000", cfg.TrafficLogBufferCapacity)
		}
	})

	t.Run("out of range clamps to default with warning", func(t *testing.T) {
		for _, v := range []string{"0", "-1", "499", "100001"} {
			fileVals := map[string]string{"traffic-log-buffer-capacity": v}
			cfg, warns, err := Resolve(Defaults(), fileVals, nil)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", v, err)
			}
			if len(warns) != 1 {
				t.Fatalf("Resolve(%q): expected 1 warning, got %v", v, warns)
			}
			if cfg.TrafficLogBufferCapacity != 10000 {
				t.Fatalf("Resolve(%q): got %d, want default 10000", v, cfg.TrafficLogBufferCapacity)
			}
		}
	})

	t.Run("boundary values pass without warning", func(t *testing.T) {
		for _, v := range []string{"500", "100000"} {
			fileVals := map[string]string{"traffic-log-buffer-capacity": v}
			cfg, warns, err := Resolve(Defaults(), fileVals, nil)
			if err != nil {
				t.Fatalf("Resolve(%q) failed: %v", v, err)
			}
			if len(warns) != 0 {
				t.Fatalf("Resolve(%q): unexpected warnings: %v", v, warns)
			}
			want := 500
			if v == "100000" {
				want = 100000
			}
			if cfg.TrafficLogBufferCapacity != want {
				t.Fatalf("Resolve(%q): got %d, want %d", v, cfg.TrafficLogBufferCapacity, want)
			}
		}
	})

	t.Run("non-integer is a fail-fast error", func(t *testing.T) {
		fileVals := map[string]string{"traffic-log-buffer-capacity": "abc"}
		if _, _, err := Resolve(Defaults(), fileVals, nil); err == nil {
			t.Fatalf("expected error for non-integer traffic-log-buffer-capacity")
		}
	})
}

// TestResolve_MaxObjectEntriesAndMaxExpandedRulesPerPolicy covers the two
// multi-value-object caps' default/override/out-of-range/non-integer
// behavior (docs/ref/todo/multi-value-address-service-objects-plan.md
// §2.1/T-00A) — same pattern as TestResolve_TrafficLogBufferCapacity above.
func TestResolve_MaxObjectEntriesAndMaxExpandedRulesPerPolicy(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		cfg, warns, err := Resolve(Defaults(), nil, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxObjectEntries != 64 {
			t.Fatalf("got MaxObjectEntries=%d, want default 64", cfg.MaxObjectEntries)
		}
		if cfg.MaxExpandedRulesPerPolicy != 4096 {
			t.Fatalf("got MaxExpandedRulesPerPolicy=%d, want default 4096", cfg.MaxExpandedRulesPerPolicy)
		}
	})

	t.Run("file override", func(t *testing.T) {
		fileVals := map[string]string{"max-object-entries": "128", "max-expanded-rules-per-policy": "8192"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxObjectEntries != 128 {
			t.Fatalf("got MaxObjectEntries=%d, want 128", cfg.MaxObjectEntries)
		}
		if cfg.MaxExpandedRulesPerPolicy != 8192 {
			t.Fatalf("got MaxExpandedRulesPerPolicy=%d, want 8192", cfg.MaxExpandedRulesPerPolicy)
		}
	})

	t.Run("out of range clamps to default with warning", func(t *testing.T) {
		for _, v := range []string{"0", "-1", "513"} {
			fileVals := map[string]string{"max-object-entries": v}
			cfg, warns, err := Resolve(Defaults(), fileVals, nil)
			if err != nil {
				t.Fatalf("Resolve(max-object-entries=%q) failed: %v", v, err)
			}
			if len(warns) != 1 {
				t.Fatalf("Resolve(max-object-entries=%q): expected 1 warning, got %v", v, warns)
			}
			if cfg.MaxObjectEntries != 64 {
				t.Fatalf("Resolve(max-object-entries=%q): got %d, want default 64", v, cfg.MaxObjectEntries)
			}
		}
		for _, v := range []string{"0", "-1", "63", "65537"} {
			fileVals := map[string]string{"max-expanded-rules-per-policy": v}
			cfg, warns, err := Resolve(Defaults(), fileVals, nil)
			if err != nil {
				t.Fatalf("Resolve(max-expanded-rules-per-policy=%q) failed: %v", v, err)
			}
			if len(warns) != 1 {
				t.Fatalf("Resolve(max-expanded-rules-per-policy=%q): expected 1 warning, got %v", v, warns)
			}
			if cfg.MaxExpandedRulesPerPolicy != 4096 {
				t.Fatalf("Resolve(max-expanded-rules-per-policy=%q): got %d, want default 4096", v, cfg.MaxExpandedRulesPerPolicy)
			}
		}
	})

	t.Run("boundary values pass without warning", func(t *testing.T) {
		fileVals := map[string]string{"max-object-entries": "1"}
		cfg, warns, err := Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxObjectEntries != 1 {
			t.Fatalf("got %d, want 1", cfg.MaxObjectEntries)
		}

		fileVals = map[string]string{"max-object-entries": "512"}
		cfg, warns, err = Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxObjectEntries != 512 {
			t.Fatalf("got %d, want 512", cfg.MaxObjectEntries)
		}

		fileVals = map[string]string{"max-expanded-rules-per-policy": "64"}
		cfg, warns, err = Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxExpandedRulesPerPolicy != 64 {
			t.Fatalf("got %d, want 64", cfg.MaxExpandedRulesPerPolicy)
		}

		fileVals = map[string]string{"max-expanded-rules-per-policy": "65536"}
		cfg, warns, err = Resolve(Defaults(), fileVals, nil)
		if err != nil {
			t.Fatalf("Resolve failed: %v", err)
		}
		if len(warns) != 0 {
			t.Fatalf("unexpected warnings: %v", warns)
		}
		if cfg.MaxExpandedRulesPerPolicy != 65536 {
			t.Fatalf("got %d, want 65536", cfg.MaxExpandedRulesPerPolicy)
		}
	})

	t.Run("non-integer is a fail-fast error", func(t *testing.T) {
		if _, _, err := Resolve(Defaults(), map[string]string{"max-object-entries": "abc"}, nil); err == nil {
			t.Fatalf("expected error for non-integer max-object-entries")
		}
		if _, _, err := Resolve(Defaults(), map[string]string{"max-expanded-rules-per-policy": "abc"}, nil); err == nil {
			t.Fatalf("expected error for non-integer max-expanded-rules-per-policy")
		}
	})
}
