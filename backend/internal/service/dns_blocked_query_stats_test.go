package service

import (
	"testing"

	"pigate/internal/model"
)

// TestStatisticsService_BlockedQuery_MatrixAndDrilldown is docs/ref/todo/
// dns-blocked-query-statistics-plan.md T-06: a blocked domain queried by two
// clients at different weights must produce identical figures no matter
// which of the 4 DNS statistics angles (overview TopBlockedDomains/
// TopBlockedClients, domain drill-down, client drill-down) they're read
// from — the classic "matrix ตรงกันทุกทิศ" regression this feature's plan
// insists on, same as the pre-existing (non-blocked) pair-counting test.
func TestStatisticsService_BlockedQuery_MatrixAndDrilldown(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)
	s.SetBlockedDomains([]model.BlockedDomain{
		{Domain: "ads.example.com", Mode: model.DNSBlockModeNXDomain, Enabled: true},
	})

	for i := 0; i < 3; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads.example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	}
	for i := 0; i < 2; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads.example.com", QueryType: "A", ClientIP: "192.168.1.102"})
	}
	// A normal, never-blocked domain in the mix — must never show up in the
	// blocked-side tables/fields.
	for i := 0; i < 4; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	}

	stats := s.GetDNSQueryStatistics("1h")
	if stats.TotalQueries != 9 {
		t.Fatalf("expected 9 total queries (blocked still counted), got %d", stats.TotalQueries)
	}
	if stats.BlockedQueries != 5 {
		t.Fatalf("expected 5 blocked queries, got %d", stats.BlockedQueries)
	}
	if len(stats.TopBlockedDomains) != 1 {
		t.Fatalf("expected 1 blocked domain row, got %+v", stats.TopBlockedDomains)
	}
	bd := stats.TopBlockedDomains[0]
	if bd.Domain != "ads.example.com" || bd.Count != 5 || bd.Clients != 2 || bd.Rule != "ads.example.com" || bd.Mode != model.DNSBlockModeNXDomain {
		t.Fatalf("unexpected blocked domain row: %+v", bd)
	}

	if len(stats.TopBlockedClients) != 2 {
		t.Fatalf("expected 2 blocked client rows, got %+v", stats.TopBlockedClients)
	}
	byIP := map[string]model.DNSBlockedClientStat{}
	for _, c := range stats.TopBlockedClients {
		byIP[c.IP] = c
	}
	if byIP["192.168.1.101"].Count != 3 || byIP["192.168.1.101"].Domains != 1 {
		t.Fatalf("unexpected blocked client row for .101: %+v", byIP["192.168.1.101"])
	}
	if byIP["192.168.1.102"].Count != 2 || byIP["192.168.1.102"].Domains != 1 {
		t.Fatalf("unexpected blocked client row for .102: %+v", byIP["192.168.1.102"])
	}

	// TopDomains badge (overview) — the blocked domain must be flagged,
	// the normal one must not.
	for _, td := range stats.TopDomains {
		switch td.Domain {
		case "ads.example.com":
			if !td.Blocked || td.BlockedRule != "ads.example.com" || td.BlockedMode != model.DNSBlockModeNXDomain {
				t.Fatalf("expected ads.example.com to be flagged blocked, got %+v", td)
			}
		case "example.com":
			if td.Blocked || td.BlockedRule != "" || td.BlockedMode != "" {
				t.Fatalf("expected example.com to NOT be flagged blocked, got %+v", td)
			}
		default:
			t.Fatalf("unexpected domain in TopDomains: %+v", td)
		}
	}

	// Domain drill-down: sum of client rows must equal the top-level
	// blocked-domain row's Count, and the top-level Blocked flag must be set.
	domainDD := s.GetDNSDomainClients("1h", "ads.example.com")
	if !domainDD.Blocked || domainDD.BlockedRule != "ads.example.com" || domainDD.BlockedMode != model.DNSBlockModeNXDomain {
		t.Fatalf("expected domain drilldown to be flagged blocked, got %+v", domainDD)
	}
	var ddSum uint64
	for _, c := range domainDD.Clients {
		ddSum += c.Count
	}
	if ddSum != bd.Count {
		t.Fatalf("domain drilldown client sum %d != TopBlockedDomains count %d", ddSum, bd.Count)
	}

	// Client drill-down: the blocked domain's row must be flagged and its
	// Count must match the per-client blocked total.
	clientDD := s.GetDNSClientDomains("1h", "192.168.1.101")
	found := false
	for _, d := range clientDD.Domains {
		if d.Domain != "ads.example.com" {
			continue
		}
		found = true
		if !d.Blocked || d.BlockedRule != "ads.example.com" || d.Count != 3 {
			t.Fatalf("unexpected client drilldown blocked row: %+v", d)
		}
	}
	if !found {
		t.Fatalf("expected ads.example.com in client drilldown for 192.168.1.101, got %+v", clientDD.Domains)
	}
}

// TestStatisticsService_BlockedQuery_SubdomainRule covers dnsmasq-style
// subdomain matching flowing all the way through into the classified
// statistics (plan T-06) — a rule for a parent domain must classify a query
// for a subdomain as blocked, with Rule reported as the PARENT, not the
// queried name.
func TestStatisticsService_BlockedQuery_SubdomainRule(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)
	s.SetBlockedDomains([]model.BlockedDomain{
		{Domain: "example.com", Mode: model.DNSBlockModeSinkhole, Enabled: true},
	})

	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "sub.example.com", QueryType: "A", ClientIP: "192.168.1.101"})

	stats := s.GetDNSQueryStatistics("1h")
	if stats.BlockedQueries != 1 {
		t.Fatalf("expected 1 blocked query, got %d", stats.BlockedQueries)
	}
	if len(stats.TopBlockedDomains) != 1 || stats.TopBlockedDomains[0].Domain != "sub.example.com" || stats.TopBlockedDomains[0].Rule != "example.com" {
		t.Fatalf("unexpected blocked domain row: %+v", stats.TopBlockedDomains)
	}
}

// TestStatisticsService_BlockedQuery_IPFilterBadge covers plan T-06's "badge
// บนทั้ง 3 endpoint" requirement for GetDNSIPDomains: a blocked domain's row
// in the IP-filter endpoint must also carry Blocked/BlockedRule/BlockedMode.
func TestStatisticsService_BlockedQuery_IPFilterBadge(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)
	s.SetBlockedDomains([]model.BlockedDomain{
		{Domain: "ads.example.com", Enabled: true},
	})

	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads.example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "ads.example.com", AnswerIP: "9.9.9.9"})

	ipDomains := s.GetDNSIPDomains("1h", "9.9.9.9")
	if len(ipDomains.Domains) != 1 {
		t.Fatalf("expected 1 domain for 9.9.9.9, got %+v", ipDomains.Domains)
	}
	row := ipDomains.Domains[0]
	if !row.Blocked || row.BlockedRule != "ads.example.com" || row.BlockedMode != model.DNSBlockModeNXDomain {
		t.Fatalf("expected blocked badge on IP-filter row, got %+v", row)
	}
}

// TestStatisticsService_BlockedQuery_SeriesInvariant covers plan's mandatory
// invariant: sum(BlockedSeries[].Count) == BlockedQueries always holds.
func TestStatisticsService_BlockedQuery_SeriesInvariant(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)
	s.SetBlockedDomains([]model.BlockedDomain{{Domain: "ads.example.com", Enabled: true}})

	for i := 0; i < 7; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads.example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	}
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.101"})

	stats := s.GetDNSQueryStatistics("1h")
	var sum uint64
	for _, p := range stats.BlockedSeries {
		sum += p.Count
	}
	if sum != stats.BlockedQueries {
		t.Fatalf("sum(BlockedSeries)=%d != BlockedQueries=%d", sum, stats.BlockedQueries)
	}
	if stats.BlockedQueries != 7 {
		t.Fatalf("expected BlockedQueries=7, got %d", stats.BlockedQueries)
	}
}

// TestStatisticsService_BlockedQuery_DisabledIsEmpty covers plan's privacy
// rule: DNS query logging disabled -> every blocked-side field is 0/empty,
// even when a deny-list rule is configured.
func TestStatisticsService_BlockedQuery_DisabledIsEmpty(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetBlockedDomains([]model.BlockedDomain{{Domain: "ads.example.com", Enabled: true}})
	// Logging never enabled -> RecordDNSEvent is a silent no-op.
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads.example.com", QueryType: "A", ClientIP: "192.168.1.101"})

	stats := s.GetDNSQueryStatistics("1h")
	if stats.Enabled {
		t.Fatalf("expected Enabled=false")
	}
	if stats.BlockedQueries != 0 || stats.TotalBlockedDomains != 0 || stats.TotalBlockedClients != 0 {
		t.Fatalf("expected all blocked counters 0 when disabled, got %+v", stats)
	}
	if len(stats.TopBlockedDomains) != 0 || len(stats.TopBlockedClients) != 0 || len(stats.BlockedSeries) != 0 {
		t.Fatalf("expected non-nil empty slices when disabled, got %+v", stats)
	}
}

// TestStatisticsService_BlockedQuery_MaxBlockedDomainsCap covers plan's
// truncation contract: once the per-bucket blocked-domain cap is hit,
// BlockedTruncated must go true while BlockedQueries (the exact count)
// stays accurate — only the per-domain breakdown (TopBlockedDomains'
// row-sum) is allowed to fall behind.
func TestStatisticsService_BlockedQuery_MaxBlockedDomainsCap(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetBlockedStatsLimit(2)
	s.SetDNSLoggingEnabled(true)
	s.SetBlockedDomains([]model.BlockedDomain{
		{Domain: "ads1.example.com", Enabled: true},
		{Domain: "ads2.example.com", Enabled: true},
		{Domain: "ads3.example.com", Enabled: true},
	})

	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads1.example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads2.example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads3.example.com", QueryType: "A", ClientIP: "192.168.1.101"})

	stats := s.GetDNSQueryStatistics("1h")
	if stats.BlockedQueries != 3 {
		t.Fatalf("expected BlockedQueries=3 (uncapped, exact), got %d", stats.BlockedQueries)
	}
	if !stats.BlockedTruncated {
		t.Fatalf("expected BlockedTruncated=true once the per-bucket cap (2) was hit")
	}
	var rowSum uint64
	for _, d := range stats.TopBlockedDomains {
		rowSum += d.Count
	}
	if rowSum >= stats.BlockedQueries {
		t.Fatalf("expected TopBlockedDomains row sum (%d) to fall behind BlockedQueries (%d) once truncated", rowSum, stats.BlockedQueries)
	}
}

// TestStatisticsService_BlockedQuery_EmptyDenyListUnchanged covers plan's
// backward-compatibility rule: with no deny-list configured, the response
// must be identical to what it was before this feature existed.
func TestStatisticsService_BlockedQuery_EmptyDenyListUnchanged(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	for i := 0; i < 3; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	}

	stats := s.GetDNSQueryStatistics("1h")
	if stats.BlockedQueries != 0 || stats.BlockedTruncated || stats.TotalBlockedDomains != 0 || stats.TotalBlockedClients != 0 {
		t.Fatalf("expected all-zero blocked fields with an empty deny-list, got %+v", stats)
	}
	if len(stats.TopBlockedDomains) != 0 || len(stats.TopBlockedClients) != 0 {
		t.Fatalf("expected empty blocked tables, got %+v", stats)
	}
	for _, td := range stats.TopDomains {
		if td.Blocked || td.BlockedRule != "" || td.BlockedMode != "" {
			t.Fatalf("expected no domain flagged blocked, got %+v", td)
		}
	}
}

// TestStatisticsService_BlockedQuery_NoRetroactiveReclassification covers
// plan §0's core design decision: classification is RECORD-TIME only — a
// deny-list change after a query was already recorded must never change
// that query's Blocked status on a later read.
func TestStatisticsService_BlockedQuery_NoRetroactiveReclassification(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	// Recorded while the deny-list was empty -> never blocked.
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.101"})

	// Deny-list now covers example.com, but the query above must stay
	// unblocked (record-time classification is permanent).
	s.SetBlockedDomains([]model.BlockedDomain{{Domain: "example.com", Enabled: true}})

	stats := s.GetDNSQueryStatistics("1h")
	if stats.BlockedQueries != 0 {
		t.Fatalf("expected the earlier query to remain unblocked, got BlockedQueries=%d", stats.BlockedQueries)
	}
	for _, td := range stats.TopDomains {
		if td.Domain == "example.com" && td.Blocked {
			t.Fatalf("expected example.com to remain unblocked (record-time classification), got %+v", td)
		}
	}

	// A NEW query recorded after the deny-list change must be classified.
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	stats = s.GetDNSQueryStatistics("1h")
	if stats.BlockedQueries != 1 {
		t.Fatalf("expected the new query to be classified blocked, got BlockedQueries=%d", stats.BlockedQueries)
	}
}

// TestStatisticsService_BlockedQuery_MaxPairsCapStillEnforced is a
// regression guard: adding the blocked-domain cap must not bypass or
// interact badly with the pre-existing (domain,client) pair cap.
func TestStatisticsService_BlockedQuery_MaxPairsCapStillEnforced(t *testing.T) {
	traffic := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	s := NewStatisticsService(traffic, traffic.repo, traffic.dhcp, 1, defaultMaxTrackedDNSClients, defaultMaxTrackedDenySources, defaultMaxTrackedDenyPorts)
	s.SetDNSLoggingEnabled(true)
	s.SetBlockedDomains([]model.BlockedDomain{{Domain: "ads1.example.com", Enabled: true}, {Domain: "ads2.example.com", Enabled: true}})

	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads1.example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "ads2.example.com", QueryType: "A", ClientIP: "192.168.1.101"})

	stats := s.GetDNSQueryStatistics("1h")
	// BlockedQueries is uncapped (both events counted); Truncated (the pair
	// ring's own signal) must fire once maxPairs=1 is exceeded.
	if stats.BlockedQueries != 2 {
		t.Fatalf("expected BlockedQueries=2 (uncapped), got %d", stats.BlockedQueries)
	}
	if !stats.Truncated {
		t.Fatalf("expected Truncated=true once maxPairs(1) was exceeded")
	}
}
