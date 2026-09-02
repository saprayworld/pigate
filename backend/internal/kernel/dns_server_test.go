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

// TestBuildDNSConfig_NSDelegation covers forwarding-based NS delegation
// (docs/ref/todo/dns-ns-delegation-plan.md T-04/T-11): NS records that carry
// GlueIPs additionally emit `server=/<fqdn>/<ip>` (on top of the existing
// dns-rr= publish line), except at the zone apex where server= must never be
// emitted (§2.5) — plus the glue host-record= rules (§2.4/§2.6).
func TestBuildDNSConfig_NSDelegation(t *testing.T) {
	t.Run("subdomain NS with single glue IP emits both dns-rr and server", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.sub.example.local", GlueIPs: []string{"203.0.113.53"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.sub.example.local")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		if !strings.Contains(cfg, "dns-rr=sub.example.local,2,"+wantHex) {
			t.Errorf("expected publish (dns-rr=) line to still be present, got:\n%s", cfg)
		}
		if !strings.Contains(cfg, "server=/sub.example.local/203.0.113.53") {
			t.Errorf("expected delegation (server=) line, got:\n%s", cfg)
		}
	})

	t.Run("multiple glue IPs v4+v6 all emitted without duplicates", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.provider.example", GlueIPs: []string{"203.0.113.53", "2001:db8::53"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)
		for _, want := range []string{
			"server=/sub.example.local/203.0.113.53",
			"server=/sub.example.local/2001:db8::53",
		} {
			if !strings.Contains(cfg, want) {
				t.Errorf("expected %q, got:\n%s", want, cfg)
			}
		}
	})

	t.Run("two NS records same name with overlapping glue IPs dedupe server lines", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.provider.example", GlueIPs: []string{"203.0.113.53"}},
					{Name: "sub", Type: "NS", Value: "ns2.provider.example", GlueIPs: []string{"203.0.113.53", "198.51.100.10"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)
		count := strings.Count(cfg, "server=/sub.example.local/203.0.113.53")
		if count != 1 {
			t.Errorf("expected exactly 1 occurrence of the duplicated server= line, got %d in:\n%s", count, cfg)
		}
		if !strings.Contains(cfg, "server=/sub.example.local/198.51.100.10") {
			t.Errorf("expected the non-duplicated glue IP's server= line, got:\n%s", cfg)
		}
	})

	t.Run("apex NS with glue publishes but never emits server for the zone", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "@", Type: "NS", Value: "ns1.example.local", GlueIPs: []string{"203.0.113.53"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.example.local")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		if !strings.Contains(cfg, "dns-rr=example.local,2,"+wantHex) {
			t.Errorf("expected apex dns-rr= publish line to still be present, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "server=/example.local/") {
			t.Errorf("apex NS record must NEVER emit server=/<zone>/ (would forward-hijack the whole zone), got:\n%s", cfg)
		}
	})

	t.Run("invalid glue IPs are skipped, not injected", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.provider.example", GlueIPs: []string{"1.2.3.4\nserver=/evil/6.6.6.6", "127.0.0.1"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)
		if strings.Contains(cfg, "evil") {
			t.Errorf("injected glue IP value must not leak into config, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "server=/sub.example.local/127.0.0.1") {
			t.Errorf("loopback glue IP must not be emitted, got:\n%s", cfg)
		}
	})

	t.Run("glue host-record rules", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					// target in the parent zone, outside the delegated name -> glue host-record.
					{Name: "sub", Type: "NS", Value: "ns1.example.local", GlueIPs: []string{"203.0.113.53"}},
					// target under the delegated name itself -> no glue host-record.
					{Name: "deleg", Type: "NS", Value: "ns1.deleg.example.local", GlueIPs: []string{"203.0.113.54"}},
					// target outside the zone entirely (public name) -> no glue host-record.
					{Name: "pub", Type: "NS", Value: "rohin.ns.cloudflare.com", GlueIPs: []string{"203.0.113.55"}},
					// an existing A record for the same host name as another NS's target ->
					// glue host-record must not override it.
					{Name: "existing", Type: "NS", Value: "ns1exists.example.local", GlueIPs: []string{"203.0.113.56"}},
					{Name: "ns1exists", Type: "A", Value: "192.168.9.9"},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		if !strings.Contains(cfg, "host-record=ns1.example.local,203.0.113.53") {
			t.Errorf("expected glue host-record for target in parent zone, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "host-record=ns1.deleg.example.local,") {
			t.Errorf("must NOT emit glue host-record for a target under the delegated name, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "cloudflare") {
			t.Errorf("must NEVER emit a glue host-record for a target outside the zone (public-name hijack), got:\n%s", cfg)
		}
		if strings.Contains(cfg, "host-record=ns1exists.example.local,203.0.113.56") {
			t.Errorf("must NOT override an existing A record with a glue host-record, got:\n%s", cfg)
		}
		if !strings.Contains(cfg, "host-record=ns1exists.example.local,192.168.9.9") {
			t.Errorf("the user's own A record for ns1exists.example.local must still be emitted, got:\n%s", cfg)
		}
	})

	t.Run("no glue is byte-for-byte no-regression", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.example.local"},
				},
			},
		}
		got := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.example.local")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		want := "# /etc/dnsmasq.d/pigate-dns.conf — Generated by PiGate\n\n" +
			"# Zone: example.local\nlocal=/example.local/\n" +
			"dns-rr=sub.example.local,2," + wantHex + "\n\n"
		if got != want {
			t.Errorf("NS record without glue must produce byte-identical output to the pre-delegation generator.\nwant:\n%q\ngot:\n%q", want, got)
		}
	})
}

// TestBuildDNSConfig_NSDelegationUpstreamMode covers the "upstream" delegation
// mode added by docs/ref/todo/dns-ns-delegation-cname-fix-plan.md T-04/T-10:
// an NS record with DelegationMode "upstream" emits `server=/<fqdn>/#`
// instead of (or on top of skipping) the glue-IP server= lines, so dnsmasq
// hands the subtree to the box's normal upstream resolvers — required to get
// a complete answer when the delegated nameserver replies with a CNAME
// pointing outside its own zone.
func TestBuildDNSConfig_NSDelegationUpstreamMode(t *testing.T) {
	t.Run("subdomain upstream mode without glue emits server=/name/# only", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.provider.example", DelegationMode: "upstream"},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.provider.example")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		if !strings.Contains(cfg, "dns-rr=sub.example.local,2,"+wantHex) {
			t.Errorf("expected publish (dns-rr=) line to still be present, got:\n%s", cfg)
		}
		if !strings.Contains(cfg, "server=/sub.example.local/#") {
			t.Errorf("expected upstream delegation line server=/sub.example.local/#, got:\n%s", cfg)
		}
		for _, line := range strings.Split(cfg, "\n") {
			if strings.HasPrefix(line, "server=/sub.example.local/") && line != "server=/sub.example.local/#" {
				t.Errorf("must not emit a glue-IP server= line in upstream mode, got line: %q\nfull config:\n%s", line, cfg)
			}
		}
	})

	t.Run("upstream mode with glue IP suppresses glue server= but keeps glue host-record", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.example.local", DelegationMode: "upstream", GlueIPs: []string{"203.0.113.53"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		if !strings.Contains(cfg, "server=/sub.example.local/#") {
			t.Errorf("expected upstream delegation line, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "server=/sub.example.local/203.0.113.53") {
			t.Errorf("must not also emit the glue-IP server= line in upstream mode, got:\n%s", cfg)
		}
		if !strings.Contains(cfg, "host-record=ns1.example.local,203.0.113.53") {
			t.Errorf("glue host-record for the nameserver's own name (in the parent zone) must still be emitted, got:\n%s", cfg)
		}
	})

	t.Run("apex upstream mode never emits server= for the zone", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "@", Type: "NS", Value: "ns1.example.local", DelegationMode: "upstream"},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.example.local")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		if !strings.Contains(cfg, "dns-rr=example.local,2,"+wantHex) {
			t.Errorf("expected apex dns-rr= publish line to still be present, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "server=/example.local/") {
			t.Errorf("apex NS record must NEVER emit server=/<zone>/ even in upstream mode (would forward-hijack the whole zone), got:\n%s", cfg)
		}
	})

	t.Run("apex upstream mode with glue also never emits server= for the zone", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "@", Type: "NS", Value: "ns1.example.local", DelegationMode: "upstream", GlueIPs: []string{"203.0.113.53"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)
		if strings.Contains(cfg, "server=/example.local/") {
			t.Errorf("apex NS record with glue IPs AND upstream mode must NEVER emit server=/<zone>/, got:\n%s", cfg)
		}
	})

	t.Run("colliding glue and upstream records for the same name yield only server=/name/# regardless of order", func(t *testing.T) {
		glueRec := model.DNSRecord{Name: "sub", Type: "NS", Value: "ns1.provider.example", GlueIPs: []string{"203.0.113.53"}}
		upstreamRec := model.DNSRecord{Name: "sub", Type: "NS", Value: "ns2.provider.example", DelegationMode: "upstream"}

		orderA := []model.DNSZone{{
			ZoneName: "example.local", Enabled: true, IsAuthoritative: true,
			Records: []model.DNSRecord{glueRec, upstreamRec},
		}}
		orderB := []model.DNSZone{{
			ZoneName: "example.local", Enabled: true, IsAuthoritative: true,
			Records: []model.DNSRecord{upstreamRec, glueRec},
		}}

		cfgA := buildDNSConfig(orderA, nil, nil, false, nil, nil)
		cfgB := buildDNSConfig(orderB, nil, nil, false, nil, nil)

		// The two dns-rr= publish lines legitimately differ in relative order
		// (each is written as its own record is processed, and the two
		// records have different Values/targets), so we don't assert full
		// byte-identical output here — only that the delegation OUTCOME
		// (which server= line survives) is the same regardless of order,
		// which is exactly what the upstreamDelegatedNames pre-pass
		// (T-04 item 1) guarantees.
		for _, cfg := range []string{cfgA, cfgB} {
			if !strings.Contains(cfg, "server=/sub.example.local/#") {
				t.Errorf("expected server=/sub.example.local/#, got:\n%s", cfg)
			}
			if strings.Contains(cfg, "server=/sub.example.local/203.0.113.53") {
				t.Errorf("must not emit the glue server= line when another record for the same name uses upstream mode, got:\n%s", cfg)
			}
			if strings.Count(cfg, "server=/sub.example.local/#") != 1 {
				t.Errorf("expected exactly one server=/sub.example.local/# line, got:\n%s", cfg)
			}
		}
	})

	t.Run("unknown DelegationMode value is rejected by the record validator, not leaked into output", func(t *testing.T) {
		// buildDNSConfig runs model.ValidateDNSRecord on every record before
		// processing it (pre-existing defense-in-depth against a DB row that
		// bypassed handler validation), and that validator rejects any
		// DelegationMode outside {"", "glue", "upstream"} (T-02). So an
		// invalid value never reaches the mode == "upstream"/"glue" branch at
		// all — the whole record (dns-rr= included) is skipped, which is a
		// stronger guarantee than merely "falls back to glue" and still
		// satisfies the same invariant: the garbage string must never appear
		// in the generated config. (model.EffectiveNSDelegationMode's own
		// "unrecognized -> glue" fallback is covered directly, independent
		// of this record-level gate, by TestEffectiveNSDelegationMode in
		// internal/model.)
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.provider.example", DelegationMode: "bogus", GlueIPs: []string{"203.0.113.53"}},
				},
			},
		}
		cfg := buildDNSConfig(zones, nil, nil, false, nil, nil)

		if strings.Contains(cfg, "bogus") {
			t.Errorf("garbage DelegationMode value must never appear in the generated config, got:\n%s", cfg)
		}
		if strings.Contains(cfg, "dns-rr=sub.example.local,") {
			t.Errorf("record with an invalid DelegationMode must be skipped entirely (fails model.ValidateDNSRecord), got:\n%s", cfg)
		}
		if strings.Contains(cfg, "server=/sub.example.local/") {
			t.Errorf("record with an invalid DelegationMode must not emit any server= line, got:\n%s", cfg)
		}
	})

	t.Run("record with explicit empty DelegationMode is byte-for-byte no-regression", func(t *testing.T) {
		zones := []model.DNSZone{
			{
				ZoneName:        "example.local",
				Enabled:         true,
				IsAuthoritative: true,
				Records: []model.DNSRecord{
					{Name: "sub", Type: "NS", Value: "ns1.example.local", GlueIPs: []string{"203.0.113.53"}, DelegationMode: ""},
				},
			},
		}
		got := buildDNSConfig(zones, nil, nil, false, nil, nil)

		wantHex, err := model.EncodeDNSNameHex("ns1.example.local")
		if err != nil {
			t.Fatalf("EncodeDNSNameHex: %v", err)
		}
		want := "# /etc/dnsmasq.d/pigate-dns.conf — Generated by PiGate\n\n" +
			"# Zone: example.local\nlocal=/example.local/\n" +
			"dns-rr=sub.example.local,2," + wantHex + "\n" +
			"# NS delegation (forwarding-based)\n" +
			"server=/sub.example.local/203.0.113.53\n" +
			"host-record=ns1.example.local,203.0.113.53\n\n"
		if got != want {
			t.Errorf("empty DelegationMode must produce byte-identical output to the pre-delegation-mode generator.\nwant:\n%q\ngot:\n%q", want, got)
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
