package service

import (
	"testing"

	"pigate/internal/model"
)

// TestGetIPReference_ScopeDecidedByIsGloballyRoutable is plan §5 Caution 1's
// direct test: scope must be decided EXCLUSIVELY by isGloballyRoutable, never
// isPrivateIP — an IPv4-mapped IPv6 literal wrapping a private address must
// still come back "lan", not "public" (the exact bypass isPrivateIP alone
// would miss).
func TestGetIPReference_ScopeDecidedByIsGloballyRoutable(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	cases := []struct {
		ip    string
		scope model.ReferenceScope
	}{
		{"192.168.1.1", model.ReferenceScopeLAN},
		{"::ffff:192.168.1.1", model.ReferenceScopeLAN},
		{"10.0.0.5", model.ReferenceScopeLAN},
		{"8.8.8.8", model.ReferenceScopePublic},
		{"2606:4700:4700::1111", model.ReferenceScopePublic},
	}
	for _, c := range cases {
		got := s.GetIPReference(c.ip, 3)
		if got.Scope != c.scope {
			t.Errorf("ip=%s: expected scope %q, got %q", c.ip, c.scope, got.Scope)
		}
		if got.Window != referenceWindow {
			t.Errorf("ip=%s: expected window %q, got %q", c.ip, referenceWindow, got.Window)
		}
	}
}

// TestGetIPReference_PublicDomainsFromReverseIndex verifies the public-scope
// branch reuses dns_domain_ips.go's DomainsForIP (plan §2.1/Step 2 — no
// copy-pasted logic) and that DomainCount reflects the TRUE total before the
// limit truncates Domains.
func TestGetIPReference_PublicDomainsFromReverseIndex(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	domains := []string{"a.example.com", "b.example.com", "c.example.com", "d.example.com"}
	for _, d := range domains {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: d, AnswerIP: "93.184.216.34"})
	}

	got := s.GetIPReference("93.184.216.34", 2)
	if got.Scope != model.ReferenceScopePublic {
		t.Fatalf("expected public scope, got %q", got.Scope)
	}
	if got.DomainCount != len(domains) {
		t.Errorf("expected domainCount=%d (before limit), got %d", len(domains), got.DomainCount)
	}
	if len(got.Domains) != 2 {
		t.Errorf("expected Domains truncated to limit=2, got %d", len(got.Domains))
	}
	for _, d := range got.Domains {
		if d.LastSeen == "" {
			t.Errorf("expected public-scope domain ref to carry LastSeen, got %+v", d)
		}
	}
}

// TestGetIPReference_LANDomainsFromClientQueries verifies the LAN-scope
// branch surfaces the domains THIS client queried (not the reverse index),
// with Count populated instead of LastSeen (plan Step 1 —
// "domains เปลี่ยนความหมายตาม scope").
func TestGetIPReference_LANDomainsFromClientQueries(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.50"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.50"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "other.example.com", QueryType: "A", ClientIP: "192.168.1.50"})
	// A different client's query must never show up in 192.168.1.50's list.
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "someone-elses.example.com", QueryType: "A", ClientIP: "192.168.1.99"})

	got := s.GetIPReference("192.168.1.50", 3)
	if got.Scope != model.ReferenceScopeLAN {
		t.Fatalf("expected lan scope, got %q", got.Scope)
	}
	if got.DomainCount != 2 {
		t.Errorf("expected domainCount=2, got %d", got.DomainCount)
	}
	if len(got.Domains) != 2 {
		t.Fatalf("expected 2 domain refs, got %+v", got.Domains)
	}
	if got.Domains[0].Domain != "example.com" || got.Domains[0].Count != 2 {
		t.Errorf("expected example.com ranked first with count=2, got %+v", got.Domains[0])
	}
	for _, d := range got.Domains {
		if d.Domain == "someone-elses.example.com" {
			t.Errorf("leaked another client's query into this client's reference: %+v", got.Domains)
		}
	}
}

// TestGetIPReference_QueryLoggingDisabled is plan Step 2's "enabled=false"
// contract: no error, 200-equivalent zero-value response, Domains empty (not
// nil), DomainCount 0 — never touches the domainIPs index while disabled.
func TestGetIPReference_QueryLoggingDisabled(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(false)

	got := s.GetIPReference("8.8.8.8", 3)
	if got.Enabled {
		t.Fatalf("expected Enabled=false")
	}
	if got.Domains == nil {
		t.Errorf("expected Domains to be a non-nil empty slice, got nil")
	}
	if len(got.Domains) != 0 || got.DomainCount != 0 {
		t.Errorf("expected empty domains while disabled, got %+v (count=%d)", got.Domains, got.DomainCount)
	}
}

// TestGetIPReference_LimitClamp defends the direct (non-HTTP) call path: an
// out-of-range limit is clamped, never causes a panic/negative slice.
func TestGetIPReference_LimitClamp(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)
	for i := 0; i < 15; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: string(rune('a'+i)) + ".example.com", AnswerIP: "93.184.216.34"})
	}

	got := s.GetIPReference("93.184.216.34", 999)
	if len(got.Domains) != referenceMaxLimit {
		t.Errorf("expected limit clamped to %d, got %d rows", referenceMaxLimit, len(got.Domains))
	}

	got2 := s.GetIPReference("93.184.216.34", -1)
	if len(got2.Domains) != referenceDefaultLimit {
		t.Errorf("expected non-positive limit to fall back to default=%d, got %d rows", referenceDefaultLimit, len(got2.Domains))
	}
}

// TestGetDomainReference_ReusesForwardIndex verifies GetDomainReference reads
// dns_domain_ips.go's IPsFor (same forward index GetDNSDomainClients does)
// and that IPCount reflects the TRUE total before the limit truncates IPs.
func TestGetDomainReference_ReusesForwardIndex(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	ips := []string{"1.1.1.1", "1.1.1.2", "1.1.1.3", "1.1.1.4"}
	for _, ip := range ips {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "example.com", AnswerIP: ip})
	}
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.50"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.51"})

	got := s.GetDomainReference("example.com", 2)
	if got.IPCount != len(ips) {
		t.Errorf("expected ipCount=%d (before limit), got %d", len(ips), got.IPCount)
	}
	if len(got.IPs) != 2 {
		t.Errorf("expected IPs truncated to limit=2, got %d", len(got.IPs))
	}
	if got.QueryCount != 2 {
		t.Errorf("expected queryCount=2, got %d", got.QueryCount)
	}
	if got.Clients != 2 {
		t.Errorf("expected 2 distinct clients, got %d", got.Clients)
	}
}

// TestGetDomainReference_QueryLoggingDisabled mirrors
// TestGetIPReference_QueryLoggingDisabled for the domain side.
func TestGetDomainReference_QueryLoggingDisabled(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(false)

	got := s.GetDomainReference("example.com", 3)
	if got.Enabled {
		t.Fatalf("expected Enabled=false")
	}
	if got.IPs == nil {
		t.Errorf("expected IPs to be a non-nil empty slice, got nil")
	}
	if len(got.IPs) != 0 || got.IPCount != 0 {
		t.Errorf("expected empty ips while disabled, got %+v (count=%d)", got.IPs, got.IPCount)
	}
}
