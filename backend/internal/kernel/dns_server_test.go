//go:build linux

package kernel

import (
	"os"
	"strings"
	"testing"

	"pigate/internal/model"
)

// TestBuildDNSConfig_ListenInterfaces covers the self-healing listen-interface
// emission (issue #50): every configured name is emitted (including ones that may
// not exist yet), invalid names are skipped, and an empty list emits no
// `interface=` line at all.
func TestBuildDNSConfig_ListenInterfaces(t *testing.T) {
	t.Run("emits interface line per name without skipping missing ones", func(t *testing.T) {
		cfg := buildDNSConfig(nil, []string{"eth0.301", "wlan1"}, nil, false, nil, nil)
		for _, want := range []string{"interface=eth0.301", "interface=wlan1"} {
			if !strings.Contains(cfg, want) {
				t.Errorf("expected config to contain %q, got:\n%s", want, cfg)
			}
		}
	})

	t.Run("skips invalid names but keeps valid ones", func(t *testing.T) {
		cfg := buildDNSConfig(nil, []string{"eth0", "bad\ninterface=evil", "wlan0"}, nil, false, nil, nil)
		if !strings.Contains(cfg, "interface=eth0") || !strings.Contains(cfg, "interface=wlan0") {
			t.Errorf("expected valid interfaces to be emitted, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "interface=evil") {
			t.Errorf("injected directive must not appear in config, got:\n%s", cfg)
		}
		// The injected directive must not survive on its own line either.
		for _, line := range strings.Split(cfg, "\n") {
			if strings.TrimSpace(line) == "interface=evil" {
				t.Errorf("injection produced a standalone directive line: %q", line)
			}
		}
	})

	t.Run("no interfaces means no interface line", func(t *testing.T) {
		cfg := buildDNSConfig(nil, nil, nil, false, nil, nil)
		if strings.Contains(cfg, "interface=") {
			t.Errorf("expected no interface= line for empty list, got:\n%s", cfg)
		}
	})
}

// TestBuildDNSConfig_ZonesAndUpstreams sanity-checks that the interface refactor
// left the existing zone/upstream rendering intact.
func TestBuildDNSConfig_ZonesAndUpstreams(t *testing.T) {
	zones := []model.DNSZone{
		{
			ZoneName:        "internal.local",
			Enabled:         true,
			IsAuthoritative: true,
			Records: []model.DNSRecord{
				{Name: "www", Type: "A", Value: "192.168.1.10"},
			},
		},
		{
			ZoneName:        "corp.example",
			Enabled:         true,
			IsAuthoritative: false,
			ForwardTo:       "10.0.0.53",
		},
		{
			ZoneName:        "disabled.local",
			Enabled:         false,
			IsAuthoritative: true,
		},
	}
	cfg := buildDNSConfig(zones, []string{"eth0"}, []string{"1.1.1.1"}, false, nil, nil)

	for _, want := range []string{
		"no-resolv",
		"server=1.1.1.1",
		"local=/internal.local/",
		"host-record=www.internal.local,192.168.1.10",
		"server=/corp.example/10.0.0.53",
		"interface=eth0",
	} {
		if !strings.Contains(cfg, want) {
			t.Errorf("expected config to contain %q, got:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, "disabled.local") {
		t.Errorf("disabled zone must not be emitted, got:\n%s", cfg)
	}
}

// TestBuildDNSConfig_NSRecord covers the NS record case in buildDNSConfig
// (docs/ref/todo/dns-ns-record-support-plan.md T-07): dnsmasq has no
// ns-record directive, so NS is published via dns-rr=<fqdn>,2,<hex>.
func TestBuildDNSConfig_NSRecord(t *testing.T) {
	t.Run("apex NS record", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "@", Type: "NS", Value: "ns1.example.local"},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.example.local")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		wantLine := "dns-rr=example.local,2," + wantHex
		if !strings.Contains(cfg, wantLine) {
			t.Errorf("expected config to contain %q, got:\n%s", wantLine, cfg)
		}
		if !strings.Contains(cfg, "dns-rr=example.local,2,") {
			t.Errorf("expected dns-rr prefix for apex NS record, got:\n%s", cfg)
		}
	})

	t.Run("subdomain NS record with short target", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1"},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.example.local")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		wantLine := "dns-rr=sub.example.local,2," + wantHex
		if !strings.Contains(cfg, wantLine) {
			t.Errorf("expected short NS target to be qualified with zone name, got:\n%s", cfg)
		}
	})

	t.Run("invalid NS value is skipped, not injected", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "@", Type: "NS", Value: "ns1\ndns-rr=evil"},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		if strings.Contains(cfg, "evil") {
			t.Errorf("invalid NS value must not leak into config, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "dns-rr=") {
			t.Errorf("no dns-rr line should be emitted for an invalid NS record, got:\n%s", cfg)
		}
	})

	t.Run("no NS record is no-regression", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "www", Type: "A", Value: "192.168.1.10"},
					{Name: "alias", Type: "CNAME", Value: "www"},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		if strings.Contains(cfg, "dns-rr=") {
			t.Errorf("zone without NS records must not contain dns-rr=, got:\n%s", cfg)
		}
	})
}

// TestBuildDNSConfig_QueryLogByteIdentical locks in that queryLog=false
// produces byte-for-byte the same config as before this feature existed
// (docs/ref/todo/statistics-dns-top-domain-plan.md T-11 item 12 / §5 item 20)
// — no accidental blank-line/whitespace drift from the new branch.
func TestBuildDNSConfig_QueryLogByteIdentical(t *testing.T) {
	zones := []model.DNSZone{{ZoneName: "internal.local", Enabled: true, IsAuthoritative: true}}
	withoutFeature := "# /etc/dnsmasq.d/pigate-dns.conf — Generated by PiGate\n\n" +
		"# DNS listen interfaces (from DNS Server settings)\ninterface=eth0\n\n" +
		"# Upstream resolvers (from System DNS)\nno-resolv\nserver=1.1.1.1\n\n" +
		"# Zone: internal.local\nlocal=/internal.local/\n\n"

	got := buildDNSConfig(zones, []string{"eth0"}, []string{"1.1.1.1"}, false, nil, nil)
	if got != withoutFeature {
		t.Errorf("queryLog=false must produce byte-identical output to pre-feature config.\nwant:\n%q\ngot:\n%q", withoutFeature, got)
	}
}

// TestBuildDNSConfig_UpstreamValidation covers T-04/T-08 item 5 (🔒): each
// upstream server string is validated with net.ParseIP as a last line of
// defense before being written to `server=<ip>` (defense-in-depth against a
// DB row written directly by config import, bypassing the handler/service
// validators). Malformed values must never appear in the output.
func TestBuildDNSConfig_UpstreamValidation(t *testing.T) {
	t.Run("malformed entries are filtered, valid ones kept", func(t *testing.T) {
		upstreams := []string{
			"1.1.1.1",
			"1.1.1.1\nlog-facility=/etc/x", // newline injection
			"8.8.8.8#5353",                 // port suffix, not a bare IP
			"dns.google",                   // hostname, not an IP
			"8.8.8.8",
		}
		cfg := buildDNSConfig(nil, nil, upstreams, false, nil, nil)

		for _, want := range []string{"server=1.1.1.1", "server=8.8.8.8"} {
			if !strings.Contains(cfg, want) {
				t.Errorf("expected valid upstream %q to be emitted, got:\n%s", want, cfg)
			}
		}
		for _, bad := range []string{
			"log-facility=/etc/x",
			"server=8.8.8.8#5353",
			"server=dns.google",
		} {
			if strings.Contains(cfg, bad) {
				t.Errorf("malformed/injected upstream must not appear in config, found %q in:\n%s", bad, cfg)
			}
		}
		if strings.Count(cfg, "no-resolv") != 1 {
			t.Errorf("expected exactly one no-resolv line when valid upstreams remain, got:\n%s", cfg)
		}
	})

	// Security-critical: no-resolv is process-global and tells dnsmasq to
	// ignore /run/dnsmasq/resolv.conf entirely. If every configured upstream
	// is filtered out but no-resolv is still emitted, dnsmasq is left with
	// ZERO usable upstream — DNS for the whole network breaks (plan §5 item
	// 4). The check must happen AFTER filtering, not before.
	t.Run("all upstreams invalid means no no-resolv line at all", func(t *testing.T) {
		upstreams := []string{
			"1.1.1.1\nlog-facility=/etc/x",
			"not-an-ip",
			"8.8.8.8#5353",
			"   ",
			"",
		}
		cfg := buildDNSConfig(nil, nil, upstreams, false, nil, nil)
		if strings.Contains(cfg, "no-resolv") {
			t.Errorf("no-resolv must NOT appear when every upstream was filtered out (would leave dnsmasq with zero upstreams), got:\n%s", cfg)
		}
		if strings.Contains(cfg, "server=") {
			t.Errorf("expected no server= lines when every upstream was filtered out, got:\n%s", cfg)
		}
	})

	t.Run("empty upstream list means no no-resolv line", func(t *testing.T) {
		cfg := buildDNSConfig(nil, nil, nil, false, nil, nil)
		if strings.Contains(cfg, "no-resolv") {
			t.Errorf("expected no no-resolv line for an empty upstream list, got:\n%s", cfg)
		}
	})
}

// TestBuildDNSConfig_QueryLogDirectives covers the opt-in query-logging
// directives (plan §2) — path is the hardcoded DNSQueryLogPath constant,
// never derived from any input.
func TestBuildDNSConfig_QueryLogDirectives(t *testing.T) {
	cfg := buildDNSConfig(nil, nil, nil, true, nil, nil)
	for _, want := range []string{"log-queries", "log-facility=" + DNSQueryLogPath, "log-async=25"} {
		if !strings.Contains(cfg, want) {
			t.Errorf("expected config to contain %q when queryLog=true, got:\n%s", want, cfg)
		}
	}
}

// TestBuildDNSConfig_BlockedDomains covers the deny-list rendering
// (docs/ref/todo/dns-blocked-domains-plan.md T-07).
func TestBuildDNSConfig_BlockedDomains(t *testing.T) {
	zones := []model.DNSZone{{ZoneName: "internal.local", Enabled: true, IsAuthoritative: true}}

	t.Run("empty deny-list is byte-identical to baseline", func(t *testing.T) {
		baseline := buildDNSConfig(zones, []string{"eth0"}, []string{"1.1.1.1"}, false, nil, nil)
		withEmpty := buildDNSConfig(zones, []string{"eth0"}, []string{"1.1.1.1"}, false, []model.BlockedDomain{}, nil)
		if withEmpty != baseline {
			t.Errorf("empty blocked list must be byte-identical to nil.\nbaseline:\n%q\ngot:\n%q", baseline, withEmpty)
		}
		if strings.Contains(baseline, "Blocked") {
			t.Errorf("baseline (no blocked domains) must not mention 'Blocked', got:\n%s", baseline)
		}
	})

	t.Run("nxdomain mode emits server directive", func(t *testing.T) {
		blocked := []model.BlockedDomain{{Domain: "ads.example.com", Mode: model.DNSBlockModeNXDomain, Enabled: true}}
		cfg := buildDNSConfig(nil, nil, nil, false, blocked, nil)
		if !strings.Contains(cfg, "server=/ads.example.com/\n") {
			t.Errorf("expected server=/ads.example.com/ line, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "address=/ads.example.com/") {
			t.Errorf("nxdomain mode must not emit address= line, got:\n%s", cfg)
		}
	})

	t.Run("sinkhole mode emits both IPv4 and IPv6 address directives", func(t *testing.T) {
		blocked := []model.BlockedDomain{{Domain: "ads.example.com", Mode: model.DNSBlockModeSinkhole, Enabled: true}}
		cfg := buildDNSConfig(nil, nil, nil, false, blocked, nil)
		for _, want := range []string{"address=/ads.example.com/0.0.0.0", "address=/ads.example.com/::"} {
			if !strings.Contains(cfg, want) {
				t.Errorf("expected %q, got:\n%s", want, cfg)
			}
		}
	})

	t.Run("disabled entry is not emitted", func(t *testing.T) {
		blocked := []model.BlockedDomain{{Domain: "ads.example.com", Mode: model.DNSBlockModeNXDomain, Enabled: false}}
		cfg := buildDNSConfig(nil, nil, nil, false, blocked, nil)
		if strings.Contains(cfg, "ads.example.com") {
			t.Errorf("disabled entry must not be emitted, got:\n%s", cfg)
		}
	})

	t.Run("entry colliding with an enabled zone name is skipped", func(t *testing.T) {
		blocked := []model.BlockedDomain{{Domain: "internal.local", Mode: model.DNSBlockModeNXDomain, Enabled: true}}
		cfg := buildDNSConfig(zones, []string{"eth0"}, nil, false, blocked, nil)
		if strings.Contains(cfg, "server=/internal.local/") {
			t.Errorf("entry colliding with an enabled zone name must be skipped, got:\n%s", cfg)
		}
	})

	t.Run("embedded newline in domain does not inject a directive", func(t *testing.T) {
		blocked := []model.BlockedDomain{{Domain: "ads.example.com\nlog-facility=/etc/x", Mode: model.DNSBlockModeNXDomain, Enabled: true}}
		cfg := buildDNSConfig(nil, nil, nil, false, blocked, nil)
		if strings.Contains(cfg, "log-facility=/etc/x") {
			t.Errorf("newline-injected directive must not appear in config, got:\n%s", cfg)
		}
	})
}

// TestBuildDNSConfig_BlocklistDirectives covers rendering of already-resolved
// blocklist directive lines (docs/ref/todo/dns-blocklist-import-plan.md
// §2.1/§2.7, T-02) — buildDNSConfig itself does no I/O, it only emits
// whatever ready-made "addn-hosts="/"conf-file=" lines it's handed.
func TestBuildDNSConfig_BlocklistDirectives(t *testing.T) {
	t.Run("empty blocklist directives is byte-identical to baseline", func(t *testing.T) {
		baseline := buildDNSConfig(nil, nil, nil, false, nil, nil)
		withEmpty := buildDNSConfig(nil, nil, nil, false, nil, []string{})
		if withEmpty != baseline {
			t.Errorf("empty blocklist directives must be byte-identical to nil.\nbaseline:\n%q\ngot:\n%q", baseline, withEmpty)
		}
		if strings.Contains(baseline, "Blocklists") {
			t.Errorf("baseline (no blocklists) must not mention 'Blocklists', got:\n%s", baseline)
		}
	})

	t.Run("emits addn-hosts and conf-file lines in order given", func(t *testing.T) {
		directives := []string{
			"addn-hosts=/var/lib/pigate/blocklists/bl-aaa111.hosts",
			"conf-file=/var/lib/pigate/blocklists/bl-bbb222.conf",
		}
		cfg := buildDNSConfig(nil, nil, nil, false, nil, directives)
		idxHosts := strings.Index(cfg, directives[0])
		idxConf := strings.Index(cfg, directives[1])
		if idxHosts < 0 || idxConf < 0 {
			t.Fatalf("expected both directives present, got:\n%s", cfg)
		}
		if idxHosts > idxConf {
			t.Errorf("expected addn-hosts line before conf-file line (order preserved), got:\n%s", cfg)
		}
		if !strings.Contains(cfg, "# Blocklists (bulk import)\n") {
			t.Errorf("expected '# Blocklists (bulk import)' heading, got:\n%s", cfg)
		}
	})
}

// TestResolveBlocklistDirectives_StatCheck covers ApplyZones' file-existence
// guard (plan Caution 1/16): only refs whose file actually exists (and is
// non-empty) on disk produce a directive; a missing file must never be
// emitted, since a dangling conf-file= target makes dnsmasq refuse to start
// entirely.
func TestResolveBlocklistDirectives_StatCheck(t *testing.T) {
	orig := blocklistDir
	blocklistDir = t.TempDir()
	defer func() { blocklistDir = orig }()

	// bl-sink1 has a real, non-empty .hosts file -> sinkhole mode, emitted.
	if err := os.WriteFile(blocklistHostsPath("bl-sink1"), []byte("0.0.0.0 ads.example.com\n"), 0644); err != nil {
		t.Fatalf("failed to seed hosts file: %v", err)
	}
	// bl-nx1 has a real, non-empty .conf file -> nxdomain mode, emitted.
	if err := os.WriteFile(blocklistConfPath("bl-nx1"), []byte("address=/tracker.example.com/\n"), 0644); err != nil {
		t.Fatalf("failed to seed conf file: %v", err)
	}
	// bl-missing has no file at all on disk -> must be skipped, not emitted.

	m := &RealDNSServerManager{}
	got := m.resolveBlocklistDirectives([]model.BlocklistRef{
		{ID: "bl-sink1", BlockMode: model.DNSBlockModeSinkhole},
		{ID: "bl-nx1", BlockMode: model.DNSBlockModeNXDomain},
		{ID: "bl-missing", BlockMode: model.DNSBlockModeSinkhole},
	})

	wantHosts := "addn-hosts=" + blocklistHostsPath("bl-sink1")
	wantConf := "conf-file=" + blocklistConfPath("bl-nx1")
	joined := strings.Join(got, "\n")
	if !strings.Contains(joined, wantHosts) {
		t.Errorf("expected %q in resolved directives, got: %v", wantHosts, got)
	}
	if !strings.Contains(joined, wantConf) {
		t.Errorf("expected %q in resolved directives, got: %v", wantConf, got)
	}
	if strings.Contains(joined, "bl-missing") {
		t.Errorf("a ref whose file does not exist must not be emitted at all, got: %v", got)
	}
	if len(got) != 2 {
		t.Errorf("expected exactly 2 directives (missing one skipped), got %d: %v", len(got), got)
	}
}
