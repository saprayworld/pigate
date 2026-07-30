package kernel

import (
	"strings"
	"testing"

	"pigate/internal/model"
)

// TestParseDNSLogLine_Query covers query line parsing (T-11 item 1).
func TestParseDNSLogLine_Query(t *testing.T) {
	ev, ok := parseDNSLogLine("query[A] www.example.com from 192.168.1.101")
	if !ok {
		t.Fatalf("expected ok=true")
	}
	if ev.Kind != model.DNSLogQuery {
		t.Errorf("Kind = %q, want %q", ev.Kind, model.DNSLogQuery)
	}
	if ev.Domain != "www.example.com" {
		t.Errorf("Domain = %q", ev.Domain)
	}
	if ev.QueryType != "A" {
		t.Errorf("QueryType = %q", ev.QueryType)
	}
	if ev.ClientIP != "192.168.1.101" {
		t.Errorf("ClientIP = %q", ev.ClientIP)
	}
}

// TestParseDNSLogLine_Answer covers reply/cached/config ... is <IP> lines
// (T-11 item 1) — one branch handles all three verbs.
func TestParseDNSLogLine_Answer(t *testing.T) {
	cases := []struct {
		name string
		line string
		ip   string
	}{
		{"reply", "reply cdn.example.net is 93.184.216.34", "93.184.216.34"},
		{"cached", "cached www.example.com is 93.184.216.34", "93.184.216.34"},
		{"config", "config pigate.local is 192.168.1.1", "192.168.1.1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ev, ok := parseDNSLogLine(c.line)
			if !ok {
				t.Fatalf("expected ok=true for %q", c.line)
			}
			if ev.Kind != model.DNSLogAnswer {
				t.Errorf("Kind = %q, want %q", ev.Kind, model.DNSLogAnswer)
			}
			if ev.AnswerIP != c.ip {
				t.Errorf("AnswerIP = %q, want %q", ev.AnswerIP, c.ip)
			}
		})
	}
}

// TestParseDNSLogLine_Dropped covers every line shape that must be dropped
// (T-11 item 1): forwarded, non-IP "is" values (CNAME/NXDOMAIN/NODATA), DHCP
// lines, and garbage.
func TestParseDNSLogLine_Dropped(t *testing.T) {
	lines := []string{
		"forwarded www.example.com to 8.8.8.8",
		"reply www.example.com is <CNAME>",
		"reply example.com is NXDOMAIN",
		"reply example.com is NODATA-IPv6",
		"reply example.com is NODATA-IPv4",
		"reply example.com is <REDACTED>",
		"DHCPACK(eth0) 192.168.1.50 aa:bb:cc:dd:ee:ff device",
		"garbage line with no structure",
		"",
		"   ",
	}
	for _, line := range lines {
		if _, ok := parseDNSLogLine(line); ok {
			t.Errorf("expected line to be dropped: %q", line)
		}
	}
}

// TestParseDNSLogLine_AnswerIPv6Normalizes covers T-11 item 2: an IPv6
// address written in short or long form must normalize to the same key as
// net.IP.String() would produce for conntrack, so reverse-cache lookups hit.
func TestParseDNSLogLine_AnswerIPv6Normalizes(t *testing.T) {
	short, ok := parseDNSLogLine("reply example.com is 2001:db8::1")
	if !ok {
		t.Fatal("expected ok=true")
	}
	long, ok := parseDNSLogLine("reply example.com is 2001:0db8:0000:0000:0000:0000:0000:0001")
	if !ok {
		t.Fatal("expected ok=true")
	}
	if short.AnswerIP != long.AnswerIP {
		t.Errorf("short form %q != long form %q, expected same normalized key", short.AnswerIP, long.AnswerIP)
	}
	if short.AnswerIP != "2001:db8::1" {
		t.Errorf("AnswerIP = %q, want canonical form 2001:db8::1", short.AnswerIP)
	}
}

// TestSanitizeDomain covers the parser's untrusted-input defense (T-11 item
// 3, 🔒) for both the query side and the CNAME/reply side — malicious
// domains must be dropped at the parser, never reach the caller.
func TestSanitizeDomain(t *testing.T) {
	t.Run("valid domains pass", func(t *testing.T) {
		for _, d := range []string{"www.example.com", "xn--80ak6aa92e.com", "a-b_c.example.com", "*.googlevideo.com"} {
			if _, ok := sanitizeDomain(d); !ok {
				t.Errorf("expected %q to be accepted", d)
			}
		}
	})

	t.Run("trailing dot stripped, case lowered", func(t *testing.T) {
		got, ok := sanitizeDomain("WWW.Example.COM.")
		if !ok || got != "www.example.com" {
			t.Errorf("got (%q, %v), want (www.example.com, true)", got, ok)
		}
	})

	t.Run("rejects malicious/oversized values", func(t *testing.T) {
		bad := []string{
			"<script>alert(1)</script>.example.com",
			"evil\nexample.com",
			"evil\x00example.com",
			"evil example.com",
			"\"quoted\".example.com",
			strings.Repeat("a", 254),
		}
		for _, d := range bad {
			if _, ok := sanitizeDomain(d); ok {
				t.Errorf("expected %q to be rejected", d)
			}
		}
	})

	t.Run("query-side injection is dropped by parseDNSLogLine", func(t *testing.T) {
		if _, ok := parseDNSLogLine("query[A] <script>alert(1)</script> from 192.168.1.101"); ok {
			t.Error("expected malicious query domain to be dropped")
		}
	})

	t.Run("CNAME/reply-side injection is dropped by parseDNSLogLine", func(t *testing.T) {
		if _, ok := parseDNSLogLine("reply evil\ninterface=evil is 93.184.216.34"); ok {
			t.Error("expected malicious answer domain to be dropped")
		}
		if _, ok := parseDNSLogLine("reply " + strings.Repeat("a", 300) + " is 93.184.216.34"); ok {
			t.Error("expected oversized answer domain to be dropped")
		}
	})
}
