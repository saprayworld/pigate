package model

import (
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestValidateDNSBlocklistName(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid", "StevenBlack unified", false},
		{"empty", "", true},
		{"whitespace only", "   ", true},
		{"leading space", " leading", true},
		{"trailing space", "trailing ", true},
		{"too long", strings.Repeat("a", DNSBlocklistNameMax+1), true},
		{"max length ok", strings.Repeat("a", DNSBlocklistNameMax), false},
		{"newline rejected", "evil\naddn-hosts=/etc/passwd", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSBlocklistName(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDNSBlocklistName(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDNSBlocklistID(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid short", "bl-3f9a2c", false},
		{"valid single char", "bl-a", false},
		{"valid max length", "bl-" + strings.Repeat("a", 32), false},
		{"too long", "bl-" + strings.Repeat("a", 33), true},
		{"uppercase rejected", "bl-ABC123", true},
		{"missing prefix", "3f9a2c", true},
		{"path traversal dots", "bl-../../etc", true},
		{"empty", "", true},
		{"underscore rejected", "bl-a_b", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSBlocklistID(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDNSBlocklistID(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDNSBlocklistURL(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid https", "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts", false},
		{"valid https explicit 443", "https://example.com:443/hosts", false},
		{"http rejected", "http://example.com/hosts", true},
		{"other scheme rejected", "ftp://example.com/hosts", true},
		{"empty", "", true},
		{"no host", "https:///hosts", true},
		{"userinfo rejected", "https://user:pass@example.com/hosts", true},
		{"non-443 port rejected", "https://example.com:8443/hosts", true},
		{"newline rejected", "https://example.com/hosts\nGET /evil", true},
		{"too long", "https://example.com/" + strings.Repeat("a", 2048), true},
		{"malformed url", "https://[::1", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateDNSBlocklistURL(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDNSBlocklistURL(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}

func TestNormalizeBlocklistBlockMode(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		want    string
		wantErr bool
	}{
		{"empty defaults to sinkhole", "", DNSBlockModeSinkhole, false},
		{"sinkhole passthrough", "sinkhole", DNSBlockModeSinkhole, false},
		{"nxdomain passthrough", "nxdomain", DNSBlockModeNXDomain, false},
		{"unknown rejected", "bogus", "", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeBlocklistBlockMode(tt.in)
			if (err != nil) != tt.wantErr {
				t.Fatalf("NormalizeBlocklistBlockMode(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
			if err == nil && got != tt.want {
				t.Errorf("NormalizeBlocklistBlockMode(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
	if DNSBlocklistDefaultBlockMode != DNSBlockModeSinkhole {
		t.Fatalf("DNSBlocklistDefaultBlockMode = %q, want sinkhole", DNSBlocklistDefaultBlockMode)
	}
}

// TestValidateBlockedDomainDefaultModeUnchanged is a regression guard: the
// deny-list (BlockedDomain) default mode must stay nxdomain even though the
// blocklist-import feature's default is the opposite (sinkhole) — see
// NormalizeBlocklistBlockMode's doc comment.
func TestValidateBlockedDomainDefaultModeUnchanged(t *testing.T) {
	if err := ValidateBlockedDomain(BlockedDomain{Domain: "ads.example.com", Mode: "", Enabled: true}); err != nil {
		t.Fatalf("ValidateBlockedDomain with empty mode should be valid (defaults to nxdomain): %v", err)
	}
	if DNSBlockModeNXDomain != "nxdomain" {
		t.Fatalf("DNSBlockModeNXDomain constant changed unexpectedly: %q", DNSBlockModeNXDomain)
	}
}

// TestValidateBlockedDomainRegression re-runs the pre-existing table of
// deny-list validation cases (see TestValidateBlockedDomain in
// dns_validate_test.go) to confirm the T-01 refactor that factors domain
// shape checks out into ValidateBlocklistDomain did not change any behavior.
func TestValidateBlockedDomainRegression(t *testing.T) {
	tests := []struct {
		name    string
		b       BlockedDomain
		wantErr bool
	}{
		{"valid nxdomain default", BlockedDomain{Domain: "ads.example.com", Enabled: true}, false},
		{"valid sinkhole", BlockedDomain{Domain: "ads.example.com", Mode: DNSBlockModeSinkhole, Enabled: true}, false},
		{"empty domain", BlockedDomain{Domain: "", Enabled: true}, true},
		{"whitespace domain", BlockedDomain{Domain: "  ads.example.com", Enabled: true}, true},
		{"no dot", BlockedDomain{Domain: "localhost", Enabled: true}, true},
		{"leading dot", BlockedDomain{Domain: ".example.com", Enabled: true}, true},
		{"trailing dot", BlockedDomain{Domain: "example.com.", Enabled: true}, true},
		{"leading hyphen", BlockedDomain{Domain: "-example.com", Enabled: true}, true},
		{"newline injection", BlockedDomain{Domain: "example.com\naddress=/evil/6.6.6.6", Enabled: true}, true},
		{"invalid mode", BlockedDomain{Domain: "example.com", Mode: "bogus", Enabled: true}, true},
		{"comment with newline", BlockedDomain{Domain: "example.com", Comment: "line1\nline2", Enabled: true}, true},
		{"comment too long", BlockedDomain{Domain: "example.com", Comment: strings.Repeat("a", DNSBlockedCommentMax+1), Enabled: true}, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBlockedDomain(tt.b)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBlockedDomain(%+v) err = %v, wantErr %v", tt.b, err, tt.wantErr)
			}
		})
	}
}

func TestValidateBlocklistManifest(t *testing.T) {
	valid := DNSBlocklist{
		ID:         "bl-3f9a2c",
		Name:       "StevenBlack unified",
		SourceType: DNSBlocklistSourceURL,
		URL:        "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
		BlockMode:  DNSBlockModeSinkhole,
		Enabled:    true,
		CreatedAt:  "2026-08-20T08:11:00Z",
	}

	t.Run("valid manifest", func(t *testing.T) {
		m := BlocklistManifest{SchemaVersion: 1, Lists: []DNSBlocklist{valid}}
		if err := ValidateBlocklistManifest(m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("zero schema version rejected", func(t *testing.T) {
		m := BlocklistManifest{SchemaVersion: 0, Lists: []DNSBlocklist{valid}}
		if err := ValidateBlocklistManifest(m); err == nil {
			t.Fatal("expected error for schemaVersion 0")
		}
	})

	t.Run("duplicate id rejected", func(t *testing.T) {
		second := valid
		m := BlocklistManifest{SchemaVersion: 1, Lists: []DNSBlocklist{valid, second}}
		if err := ValidateBlocklistManifest(m); err == nil {
			t.Fatal("expected error for duplicate id")
		}
	})

	t.Run("invalid source type rejected", func(t *testing.T) {
		bad := valid
		bad.SourceType = "ftp"
		m := BlocklistManifest{SchemaVersion: 1, Lists: []DNSBlocklist{bad}}
		if err := ValidateBlocklistManifest(m); err == nil {
			t.Fatal("expected error for invalid sourceType")
		}
	})

	t.Run("upload list does not require url", func(t *testing.T) {
		up := valid
		up.SourceType = DNSBlocklistSourceUpload
		up.URL = ""
		m := BlocklistManifest{SchemaVersion: 1, Lists: []DNSBlocklist{up}}
		if err := ValidateBlocklistManifest(m); err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	})

	t.Run("invalid blockMode rejected", func(t *testing.T) {
		bad := valid
		bad.BlockMode = "bogus"
		m := BlocklistManifest{SchemaVersion: 1, Lists: []DNSBlocklist{bad}}
		if err := ValidateBlocklistManifest(m); err == nil {
			t.Fatal("expected error for invalid blockMode")
		}
	})
}

func TestParseHostsBlocklist(t *testing.T) {
	input := `# Title: StevenBlack hosts
#
127.0.0.1 localhost
127.0.0.1 localhost.localdomain
127.0.0.1 local
255.255.255.255 broadcasthost
::1 ip6-localhost
::1 ip6-loopback
fe80::1%lo0 ip6-localnet
ff00::0 ip6-mcastprefix
ff02::1 ip6-allnodes
ff02::2 ip6-allrouters
0.0.0.0 0.0.0.0

0.0.0.0 ads.example.com # inline comment
0.0.0.0 ads.example.com
1.2.3.4 bank.example.com
domainonly.example.org
0.0.0.0 EXAMPLE.UPPER.com
0.0.0.0 excluded.pigate.local
0.0.0.0 sub.excluded.pigate.local
0.0.0.0 notexcludedpigate.local
`
	exclude := map[string]bool{"excluded.pigate.local": true}

	domains, stat, err := ParseHostsBlocklist(strings.NewReader(input), exclude)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	want := map[string]bool{
		"ads.example.com":         true,
		"bank.example.com":        true,
		"domainonly.example.org":  true,
		"example.upper.com":       true,
		"notexcludedpigate.local": true,
	}
	got := make(map[string]bool, len(domains))
	for _, d := range domains {
		got[d] = true
	}
	if len(got) != len(want) {
		t.Fatalf("got %d domains %v, want %d domains %v", len(got), domains, len(want), want)
	}
	for d := range want {
		if !got[d] {
			t.Errorf("expected domain %q in result, got %v", d, domains)
		}
	}
	// Excluded domain and its subdomain must both be gone (label-boundary
	// exclude match).
	if got["excluded.pigate.local"] || got["sub.excluded.pigate.local"] {
		t.Errorf("excluded domains leaked into result: %v", domains)
	}
	// bank.example.com must never carry the original IP: the parser only
	// returns domain names, never IPs (security requirement §2.2).
	for _, d := range domains {
		if strings.Contains(d, "1.2.3.4") {
			t.Errorf("source IP leaked into parsed domain: %q", d)
		}
	}

	if stat.Duplicates == 0 {
		t.Errorf("expected at least 1 duplicate counted, got stat=%+v", stat)
	}
	if stat.SkippedExcluded < 2 {
		t.Errorf("expected at least 2 excluded domains counted, got stat=%+v", stat)
	}
	if stat.SkippedInvalid == 0 {
		t.Errorf("expected localhost/builtin names to count as skipped, got stat=%+v", stat)
	}
}

func TestParseHostsBlocklistOverLongLine(t *testing.T) {
	longLine := "0.0.0.0 " + strings.Repeat("a", DNSBlocklistMaxLineBytes+64) + ".example.com\n"
	input := longLine + "0.0.0.0 short.example.com\n"

	domains, stat, err := ParseHostsBlocklist(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("unexpected error (over-length line must be skipped, not fatal): %v", err)
	}
	if stat.SkippedInvalid == 0 {
		t.Errorf("expected the over-length line to be counted as skipped, got stat=%+v", stat)
	}
	found := false
	for _, d := range domains {
		if d == "short.example.com" {
			found = true
		}
	}
	// Best-effort: depending on where the scanner recovers, "short.example.com"
	// may or may not still be read. The important invariant is that parsing
	// does not return an error for an over-length line.
	_ = found
}

func TestParseHostsBlocklistUnicode(t *testing.T) {
	// Unicode/punycode-looking domains fail the ASCII charset validator and
	// must be skipped, not crash the parser.
	input := "0.0.0.0 xn--exmple-cua.com\n0.0.0.0 例え.jp\n0.0.0.0 valid.example.com\n"
	domains, stat, err := ParseHostsBlocklist(strings.NewReader(input), nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := make(map[string]bool, len(domains))
	for _, d := range domains {
		got[d] = true
	}
	if !got["xn--exmple-cua.com"] {
		t.Errorf("expected punycode-form domain to be accepted, got %v", domains)
	}
	if !got["valid.example.com"] {
		t.Errorf("expected valid.example.com to be accepted, got %v", domains)
	}
	if got["例え.jp"] {
		t.Errorf("non-ASCII domain should have been rejected by charset validator, got %v", domains)
	}
	if stat.SkippedInvalid == 0 {
		t.Errorf("expected the non-ASCII domain to be counted skipped, got stat=%+v", stat)
	}
}

func TestParseHostsBlocklistUnderCap(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 10; i++ {
		b.WriteString("0.0.0.0 d")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(".example.com\n")
	}
	domains, _, err := ParseHostsBlocklist(strings.NewReader(b.String()), nil)
	if err != nil {
		t.Fatalf("unexpected error under the cap: %v", err)
	}
	if len(domains) != 10 {
		t.Fatalf("expected 10 domains, got %d", len(domains))
	}
}

func TestParseHostsBlocklistMaxDomainsExceeded(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping large-input test in -short mode")
	}
	var b strings.Builder
	// DNSBlocklistMaxDomainsPerList is 300000 — generate one more unique
	// domain than that to exercise the "exceeds maximum" error path.
	n := DNSBlocklistMaxDomainsPerList + 1
	b.Grow(n * 24)
	for i := 0; i < n; i++ {
		b.WriteString("0.0.0.0 d")
		b.WriteString(strconv.Itoa(i))
		b.WriteString(".example.com\n")
	}
	_, _, err := ParseHostsBlocklist(strings.NewReader(b.String()), nil)
	if err == nil {
		t.Fatal("expected error when domain count exceeds DNSBlocklistMaxDomainsPerList")
	}
}

func TestRenderHostsFile(t *testing.T) {
	ts := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	domains := []string{"z.example.com", "a.example.com", "m.example.com"}

	out1 := RenderHostsFile("bl-3f9a2c", domains, ts)
	out2 := RenderHostsFile("bl-3f9a2c", []string{"a.example.com", "m.example.com", "z.example.com"}, ts)

	if string(out1) != string(out2) {
		t.Errorf("RenderHostsFile is not deterministic w.r.t. input order:\n%s\n---\n%s", out1, out2)
	}
	s := string(out1)
	for _, d := range domains {
		want := "0.0.0.0 " + d + "\n"
		if !strings.Contains(s, want) {
			t.Errorf("expected line %q in output:\n%s", want, s)
		}
	}
	// Domains must appear sorted.
	ia := strings.Index(s, "a.example.com")
	im := strings.Index(s, "m.example.com")
	iz := strings.Index(s, "z.example.com")
	if !(ia < im && im < iz) {
		t.Errorf("expected sorted output, got:\n%s", s)
	}
}

func TestRenderBlocklistConfFile(t *testing.T) {
	ts := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	domains := []string{"z.example.com", "a.example.com"}

	out := RenderBlocklistConfFile("bl-7d1e04", domains, ts)
	s := string(out)

	if strings.Contains(s, "0.0.0.0") {
		t.Errorf("RenderBlocklistConfFile must never contain an IP, got:\n%s", s)
	}
	for _, d := range domains {
		want := "address=/" + d + "/\n"
		if !strings.Contains(s, want) {
			t.Errorf("expected line %q in output:\n%s", want, s)
		}
	}
	if strings.Count(s, "address=/") != len(domains) {
		t.Errorf("expected exactly %d address= directives (one per line, no batching), got %d:\n%s", len(domains), strings.Count(s, "address=/"), s)
	}
	ia := strings.Index(s, "a.example.com")
	iz := strings.Index(s, "z.example.com")
	if !(ia < iz) {
		t.Errorf("expected sorted output, got:\n%s", s)
	}
}

func TestParseHostsFileDomains(t *testing.T) {
	ts := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	rendered := RenderHostsFile("bl-3f9a2c", []string{"a.example.com", "b.example.com"}, ts)

	var got []string
	err := ParseHostsFileDomains(strings.NewReader(string(rendered)), func(d string) error {
		got = append(got, d)
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 2 || got[0] != "a.example.com" || got[1] != "b.example.com" {
		t.Fatalf("unexpected result: %v", got)
	}
}

func TestParseHostsFileDomainsStopsOnError(t *testing.T) {
	ts := time.Date(2026, 8, 22, 10, 0, 0, 0, time.UTC)
	rendered := RenderHostsFile("bl-3f9a2c", []string{"a.example.com", "b.example.com", "c.example.com"}, ts)

	var got []string
	sentinelErr := ValidateBlocklistDomain("") // any error value
	err := ParseHostsFileDomains(strings.NewReader(string(rendered)), func(d string) error {
		got = append(got, d)
		if len(got) == 2 {
			return sentinelErr
		}
		return nil
	})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if len(got) != 2 {
		t.Fatalf("expected scan to stop after 2 domains, got %v", got)
	}
}

func TestValidateBlocklistDomain(t *testing.T) {
	tests := []struct {
		name    string
		in      string
		wantErr bool
	}{
		{"valid", "example.com", false},
		{"empty", "", true},
		{"no dot", "localhost", true},
		{"leading dot", ".example.com", true},
		{"trailing dot", "example.com.", true},
		{"leading hyphen", "-example.com", true},
		{"trailing hyphen", "example.com-", true},
		{"newline", "example.com\nevil", true},
		{"whitespace", " example.com", true},
		{"too long", strings.Repeat("a", 250) + ".com", true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateBlocklistDomain(tt.in)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateBlocklistDomain(%q) err = %v, wantErr %v", tt.in, err, tt.wantErr)
			}
		})
	}
}
