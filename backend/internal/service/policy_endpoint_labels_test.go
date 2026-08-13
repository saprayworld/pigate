package service

import (
	"testing"

	"pigate/internal/model"
)

func TestAddrMatcher_SubnetSlash32(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "SingleHost", Entries: []model.AddressEntry{{Type: "subnet", Value: "192.168.1.10/32"}}},
	})
	if name, ok := m.Match("192.168.1.10"); !ok || name != "SingleHost" {
		t.Fatalf("expected match SingleHost, got %q ok=%v", name, ok)
	}
	if _, ok := m.Match("192.168.1.11"); ok {
		t.Fatal("expected no match for a different host")
	}
}

func TestAddrMatcher_SubnetBareIPDefaultsTo32(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "Bare", Entries: []model.AddressEntry{{Type: "subnet", Value: "10.0.0.5"}}},
	})
	if name, ok := m.Match("10.0.0.5"); !ok || name != "Bare" {
		t.Fatalf("expected match Bare, got %q ok=%v", name, ok)
	}
	if _, ok := m.Match("10.0.0.6"); ok {
		t.Fatal("bare IP must default to /32, not match neighbor")
	}
}

func TestAddrMatcher_SubnetSlash24(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "LAN", Entries: []model.AddressEntry{{Type: "subnet", Value: "192.168.1.0/24"}}},
	})
	if name, ok := m.Match("192.168.1.200"); !ok || name != "LAN" {
		t.Fatalf("expected match LAN, got %q ok=%v", name, ok)
	}
	if _, ok := m.Match("192.168.2.1"); ok {
		t.Fatal("expected no match outside the /24")
	}
}

func TestAddrMatcher_Range(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "DHCPRange", Entries: []model.AddressEntry{{Type: "range", Value: "192.168.1.100-192.168.1.150"}}},
	})
	if name, ok := m.Match("192.168.1.125"); !ok || name != "DHCPRange" {
		t.Fatalf("expected match DHCPRange, got %q ok=%v", name, ok)
	}
	if _, ok := m.Match("192.168.1.99"); ok {
		t.Fatal("expected no match below range start")
	}
	if _, ok := m.Match("192.168.1.151"); ok {
		t.Fatal("expected no match above range end")
	}
}

func TestAddrMatcher_MalformedValuesSkippedSilently(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "BadSubnet", Entries: []model.AddressEntry{{Type: "subnet", Value: "not-an-ip"}}},
		{Name: "BadRange1", Entries: []model.AddressEntry{{Type: "range", Value: "not-a-range"}}},
		{Name: "BadRange2", Entries: []model.AddressEntry{{Type: "range", Value: "10.0.0.5-not-an-ip"}}},
		{Name: "ReversedRange", Entries: []model.AddressEntry{{Type: "range", Value: "10.0.0.50-10.0.0.10"}}},
		{Name: "MixedFamilyRange", Entries: []model.AddressEntry{{Type: "range", Value: "10.0.0.1-::1"}}},
		{Name: "Empty", Entries: []model.AddressEntry{{Type: "subnet", Value: ""}}},
	})
	if _, ok := m.Match("10.0.0.5"); ok {
		t.Fatal("no valid ranges were configured, expected no match")
	}
	if _, ok := m.Match(""); ok {
		t.Fatal("empty IP input must not match / must not panic")
	}
	if _, ok := m.Match("garbage"); ok {
		t.Fatal("garbage IP input must not match / must not panic")
	}
}

func TestAddrMatcher_FQDNSkipped(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "SomeSite", Entries: []model.AddressEntry{{Type: "fqdn", Value: "example.com"}}},
	})
	if _, ok := m.Match("93.184.216.34"); ok {
		t.Fatal("fqdn objects must never be matched by IP")
	}
}

// Overlapping ranges: the narrowest one wins.
func TestAddrMatcher_NarrowestWins(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "Wide", Entries: []model.AddressEntry{{Type: "subnet", Value: "192.168.0.0/16"}}},
		{Name: "Narrow24", Entries: []model.AddressEntry{{Type: "subnet", Value: "192.168.1.0/24"}}},
		{Name: "Narrowest32", Entries: []model.AddressEntry{{Type: "subnet", Value: "192.168.1.10/32"}}},
	})
	if name, ok := m.Match("192.168.1.10"); !ok || name != "Narrowest32" {
		t.Fatalf("expected Narrowest32 to win, got %q ok=%v", name, ok)
	}
	if name, ok := m.Match("192.168.1.20"); !ok || name != "Narrow24" {
		t.Fatalf("expected Narrow24 to win over Wide, got %q ok=%v", name, ok)
	}
	if name, ok := m.Match("192.168.2.1"); !ok || name != "Wide" {
		t.Fatalf("expected Wide to still match outside the narrower ranges, got %q ok=%v", name, ok)
	}
}

// Tie-break by ascending name when sizes are identical.
func TestAddrMatcher_TieBreakByName(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "Zebra", Entries: []model.AddressEntry{{Type: "subnet", Value: "10.0.0.1/32"}}},
		{Name: "Alpha", Entries: []model.AddressEntry{{Type: "subnet", Value: "10.0.0.1/32"}}},
	})
	if name, ok := m.Match("10.0.0.1"); !ok || name != "Alpha" {
		t.Fatalf("expected Alpha (ascending name tie-break), got %q ok=%v", name, ok)
	}
}

func TestAddrMatcher_IPv6(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "V6Net", Entries: []model.AddressEntry{{Type: "subnet", Value: "2001:db8::/32"}}},
		{Name: "V6Host", Entries: []model.AddressEntry{{Type: "subnet", Value: "2001:db8::1/128"}}},
		{Name: "V6Range", Entries: []model.AddressEntry{{Type: "range", Value: "2001:db8::10-2001:db8::20"}}},
	})
	if name, ok := m.Match("2001:db8::1"); !ok || name != "V6Host" {
		t.Fatalf("expected V6Host to win the narrowest match, got %q ok=%v", name, ok)
	}
	if name, ok := m.Match("2001:db8::2"); !ok || name != "V6Net" {
		t.Fatalf("expected V6Net, got %q ok=%v", name, ok)
	}
	if name, ok := m.Match("2001:db8::15"); !ok || name != "V6Range" {
		t.Fatalf("expected V6Range to win over V6Net, got %q ok=%v", name, ok)
	}
	if _, ok := m.Match("::1"); ok {
		t.Fatal("expected no match for an unrelated IPv6 address")
	}
}

// IPv4-mapped IPv6 input (::ffff:a.b.c.d) must be normalized and never panic.
func TestAddrMatcher_IPv4MappedIPv6Input(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "V4Host", Entries: []model.AddressEntry{{Type: "subnet", Value: "192.0.2.1/32"}}},
	})
	if name, ok := m.Match("::ffff:192.0.2.1"); !ok || name != "V4Host" {
		t.Fatalf("expected IPv4-mapped input to match V4Host, got %q ok=%v", name, ok)
	}
}

func TestAddrMatcher_NilAndEmpty(t *testing.T) {
	var m *addrMatcher
	if _, ok := m.Match("1.2.3.4"); ok {
		t.Fatal("nil matcher must not panic and must never match")
	}

	empty := newAddrMatcher(nil)
	if _, ok := empty.Match("1.2.3.4"); ok {
		t.Fatal("empty matcher must never match")
	}
}

// TestAddrMatcher_MultiEntryObjectSameLabel covers T-08's acceptance: a
// single Address Object with two disjoint subnet entries must resolve BOTH
// subnets to the same object Name (docs/ref/todo/
// multi-value-address-service-objects-plan.md T-08).
func TestAddrMatcher_MultiEntryObjectSameLabel(t *testing.T) {
	m := newAddrMatcher([]model.AddressObject{
		{Name: "Branch_Offices", Entries: []model.AddressEntry{
			{Type: "subnet", Value: "192.168.10.0/24"},
			{Type: "subnet", Value: "192.168.20.0/24"},
		}},
	})
	if name, ok := m.Match("192.168.10.5"); !ok || name != "Branch_Offices" {
		t.Fatalf("expected first subnet entry to match Branch_Offices, got %q ok=%v", name, ok)
	}
	if name, ok := m.Match("192.168.20.5"); !ok || name != "Branch_Offices" {
		t.Fatalf("expected second subnet entry to match Branch_Offices, got %q ok=%v", name, ok)
	}
	if _, ok := m.Match("192.168.30.5"); ok {
		t.Fatal("expected no match outside either subnet entry")
	}
}
