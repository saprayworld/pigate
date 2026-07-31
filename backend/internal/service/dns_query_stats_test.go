package service

import (
	"fmt"
	"sync"
	"testing"

	"pigate/internal/model"
)

// TestStatisticsService_DNSDrilldown_PairCounting is drilldown plan T-05
// item 1: 3 clients x 2 domains must rank Top Domains/Top Clients correctly
// and the sum of each domain's/client's drill-down must equal the top-level
// table's count for that same key.
func TestStatisticsService_DNSDrilldown_PairCounting(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	events := []struct {
		domain string
		client string
		n      int
	}{
		{"www.youtube.com", "192.168.1.101", 5},
		{"www.youtube.com", "192.168.1.102", 3},
		{"www.youtube.com", "192.168.1.105", 1},
		{"netflix.com", "192.168.1.101", 2},
		{"netflix.com", "192.168.1.102", 4},
	}
	for _, e := range events {
		for i := 0; i < e.n; i++ {
			s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: e.domain, QueryType: "A", ClientIP: e.client})
		}
	}

	stats := s.GetDNSQueryStatistics("1h")
	if !stats.Enabled {
		t.Fatalf("expected Enabled=true")
	}
	if stats.TotalQueries != 15 {
		t.Fatalf("expected 15 total queries, got %d", stats.TotalQueries)
	}
	if len(stats.TopDomains) != 2 {
		t.Fatalf("expected 2 domains, got %+v", stats.TopDomains)
	}
	if stats.TopDomains[0].Domain != "www.youtube.com" || stats.TopDomains[0].Count != 9 {
		t.Fatalf("expected top domain www.youtube.com/9, got %+v", stats.TopDomains[0])
	}
	if stats.TopDomains[1].Domain != "netflix.com" || stats.TopDomains[1].Count != 6 {
		t.Fatalf("expected second domain netflix.com/6, got %+v", stats.TopDomains[1])
	}

	if len(stats.TopClients) != 3 {
		t.Fatalf("expected 3 clients, got %+v", stats.TopClients)
	}
	// 192.168.1.101: 5+2=7, .102: 3+4=7, .105: 1 -> tie broken by IP asc.
	if stats.TopClients[0].IP != "192.168.1.101" || stats.TopClients[0].Count != 7 {
		t.Fatalf("expected top client 192.168.1.101/7, got %+v", stats.TopClients[0])
	}
	if stats.TopClients[1].IP != "192.168.1.102" || stats.TopClients[1].Count != 7 {
		t.Fatalf("expected second client 192.168.1.102/7, got %+v", stats.TopClients[1])
	}
	if stats.TopClients[2].IP != "192.168.1.105" || stats.TopClients[2].Count != 1 {
		t.Fatalf("expected third client 192.168.1.105/1, got %+v", stats.TopClients[2])
	}

	// drill-down by domain: sum must equal the top-level table's count.
	drillYoutube := s.GetDNSDomainClients("1h", "www.youtube.com")
	var sumYoutube uint64
	for _, c := range drillYoutube.Clients {
		sumYoutube += c.Count
	}
	if sumYoutube != stats.TopDomains[0].Count {
		t.Fatalf("sum of www.youtube.com drilldown (%d) != top-level count (%d)", sumYoutube, stats.TopDomains[0].Count)
	}
	if drillYoutube.TotalQueries != stats.TopDomains[0].Count {
		t.Fatalf("expected drilldown TotalQueries=%d, got %d", stats.TopDomains[0].Count, drillYoutube.TotalQueries)
	}
	if len(drillYoutube.Clients) != 3 {
		t.Fatalf("expected 3 clients for www.youtube.com, got %+v", drillYoutube.Clients)
	}

	// drill-down by client: sum must equal the top-level table's count.
	drill101 := s.GetDNSClientDomains("1h", "192.168.1.101")
	var sum101 uint64
	for _, d := range drill101.Domains {
		sum101 += d.Count
	}
	if sum101 != stats.TopClients[0].Count {
		t.Fatalf("sum of 192.168.1.101 drilldown (%d) != top-level count (%d)", sum101, stats.TopClients[0].Count)
	}
	if len(drill101.Domains) != 2 {
		t.Fatalf("expected 2 domains for 192.168.1.101, got %+v", drill101.Domains)
	}
}

// TestStatisticsService_DNSDrilldown_UnknownClient is drilldown plan T-05
// item 2: an event with an empty ClientIP must land in the "unknown" bucket
// (not be dropped), and be reachable via GetDNSClientDomains("unknown").
func TestStatisticsService_DNSDrilldown_UnknownClient(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: ""})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: ""})

	stats := s.GetDNSQueryStatistics("1h")
	if stats.TotalQueries != 2 {
		t.Fatalf("expected empty ClientIP events to still be counted, got total=%d", stats.TotalQueries)
	}
	if len(stats.TopClients) != 1 || stats.TopClients[0].IP != "unknown" || stats.TopClients[0].Count != 2 {
		t.Fatalf("expected single 'unknown' client row with count 2, got %+v", stats.TopClients)
	}

	drill := s.GetDNSClientDomains("1h", "unknown")
	if drill.TotalQueries != 2 {
		t.Fatalf("expected drilldown by 'unknown' to find 2 queries, got %d", drill.TotalQueries)
	}
	if len(drill.Domains) != 1 || drill.Domains[0].Domain != "example.com" || drill.Domains[0].Count != 2 {
		t.Fatalf("unexpected drilldown domains for 'unknown': %+v", drill.Domains)
	}

	domainDrill := s.GetDNSDomainClients("1h", "example.com")
	if len(domainDrill.Clients) != 1 || domainDrill.Clients[0].IP != "unknown" || domainDrill.Clients[0].Count != 2 {
		t.Fatalf("expected example.com drilldown to show 'unknown' client with count 2, got %+v", domainDrill.Clients)
	}
}

// TestStatisticsService_DNSPairCap is drilldown plan T-05 item 3: flooding
// with 5,000 unique (domain, client) pairs must not panic, must respect
// maxTrackedDNSPairs per bucket, set Truncated=true, yet TotalQueries must
// still count every single event (never dropped by the pair cap).
func TestStatisticsService_DNSPairCap(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	// Every domain is unique (so the pair cap, not the client cap, is what
	// gets exercised) but the client pool is small (well under
	// maxTrackedDNSClients) so the client cap never interferes.
	const total = 5000
	for i := 0; i < total; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{
			Kind:      model.DNSLogQuery,
			Domain:    fmt.Sprintf("pair%d.example.com", i),
			QueryType: "A",
			ClientIP:  fmt.Sprintf("10.0.0.%d", i%50),
		})
	}

	stats := s.GetDNSQueryStatistics("1h")
	if !stats.Truncated {
		t.Fatalf("expected Truncated=true after flooding with 5000 unique pairs")
	}
	if stats.TotalQueries != total {
		t.Fatalf("expected TotalQueries=%d (never capped), got %d", total, stats.TotalQueries)
	}

	s.dns.mu.RLock()
	var maxPairCount int
	for _, b := range s.dns.buckets {
		if b.pairCount > maxPairCount {
			maxPairCount = b.pairCount
		}
		if b.pairCount > maxTrackedDNSPairs {
			t.Errorf("bucket pairCount %d exceeds maxTrackedDNSPairs %d", b.pairCount, maxTrackedDNSPairs)
		}
	}
	s.dns.mu.RUnlock()
	if maxPairCount != maxTrackedDNSPairs {
		t.Fatalf("expected at least one bucket to hit the pair cap (%d), max seen was %d", maxTrackedDNSPairs, maxPairCount)
	}
}

// TestStatisticsService_DNSDrilldown_WindowSelection is drilldown plan T-05
// item 4: the 24h window must be a superset of the 1h window, and a bucket
// older than 1h must not appear in the 1h window's results.
func TestStatisticsService_DNSDrilldown_WindowSelection(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	// dnsWindowBuckets selects "1h" as the trailing trafficWindow1hBuckets (12)
	// buckets by *position*, mirroring denySnapshot/domainSnapshot — so to
	// exercise the boundary we must build a ring with more than 12 buckets:
	// bucket[0] is "old" (must fall outside the 1h window), bucket[1..12] all
	// carry "recent.example.com" (must remain inside the 1h window).
	s.dns.mu.Lock()
	oldBucket := domainBucket{
		ts:           "2000-01-01T00:00:00Z",
		pairs:        map[string]map[string]uint64{"old.example.com": {"192.168.1.200": 9}},
		clientCount:  map[string]uint64{"192.168.1.200": 9},
		typeByDomain: map[string]string{"old.example.com": "A"},
		queries:      9,
	}
	s.dns.buckets = []domainBucket{oldBucket}
	for i := 0; i < trafficWindow1hBuckets; i++ {
		s.dns.buckets = append(s.dns.buckets, domainBucket{
			ts:           fmt.Sprintf("2020-01-01T00:%02d:00Z", i),
			pairs:        map[string]map[string]uint64{"recent.example.com": {"192.168.1.101": 1}},
			clientCount:  map[string]uint64{"192.168.1.101": 1},
			typeByDomain: map[string]string{"recent.example.com": "A"},
			queries:      1,
		})
	}
	s.dns.mu.Unlock()

	stats1h := s.GetDNSQueryStatistics("1h")
	stats24h := s.GetDNSQueryStatistics("24h")

	if stats24h.TotalQueries < stats1h.TotalQueries {
		t.Fatalf("expected 24h total (%d) >= 1h total (%d)", stats24h.TotalQueries, stats1h.TotalQueries)
	}
	for _, d := range stats1h.TopDomains {
		if d.Domain == "old.example.com" {
			t.Fatalf("did not expect old.example.com (bucket beyond the 1h window) in the 1h result: %+v", stats1h.TopDomains)
		}
	}
	var found24h bool
	for _, d := range stats24h.TopDomains {
		if d.Domain == "old.example.com" {
			found24h = true
		}
	}
	if !found24h {
		t.Fatalf("expected old.example.com to appear in the 24h window (whole ring), got %+v", stats24h.TopDomains)
	}
}

// TestStatisticsService_DNSDrilldown_Percent is drilldown plan T-05 item 5:
// Top Domains/Top Clients percentages must never sum above 100%, and a
// drilldown computed from a single-client domain's own total must be 100%.
func TestStatisticsService_DNSDrilldown_Percent(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	for i := 0; i < 7; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "shared.example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	}
	for i := 0; i < 3; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "shared.example.com", QueryType: "A", ClientIP: "192.168.1.102"})
	}
	for i := 0; i < 4; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "solo.example.com", QueryType: "A", ClientIP: "192.168.1.105"})
	}

	stats := s.GetDNSQueryStatistics("1h")
	var domainPercentSum, clientPercentSum float64
	for _, d := range stats.TopDomains {
		domainPercentSum += d.Percent
	}
	for _, c := range stats.TopClients {
		clientPercentSum += c.Percent
	}
	if domainPercentSum > 100.01 {
		t.Fatalf("sum of TopDomains percent exceeds 100%%: %v", domainPercentSum)
	}
	if clientPercentSum > 100.01 {
		t.Fatalf("sum of TopClients percent exceeds 100%%: %v", clientPercentSum)
	}

	// solo.example.com has exactly one client -> that client's drilldown
	// percent must be 100 (computed against solo's own total, not the window).
	drill := s.GetDNSDomainClients("1h", "solo.example.com")
	if len(drill.Clients) != 1 {
		t.Fatalf("expected exactly one client for solo.example.com, got %+v", drill.Clients)
	}
	if drill.Clients[0].Percent != 100 {
		t.Fatalf("expected 100%% for the sole client of solo.example.com, got %v", drill.Clients[0].Percent)
	}
}

// TestStatisticsService_DNSDrilldown_DisabledAndCleared is drilldown plan
// T-05 item 6: with the opt-in switch off, all three methods must return
// Enabled=false and empty lists (never error); ClearDNSStats must empty
// everything immediately.
func TestStatisticsService_DNSDrilldown_DisabledAndCleared(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	stats := s.GetDNSQueryStatistics("1h")
	if stats.Enabled {
		t.Fatalf("expected Enabled=false by default")
	}
	if len(stats.TopDomains) != 0 || len(stats.TopClients) != 0 {
		t.Fatalf("expected empty lists when disabled, got %+v", stats)
	}

	domainDrill := s.GetDNSDomainClients("1h", "example.com")
	if domainDrill.Enabled || len(domainDrill.Clients) != 0 {
		t.Fatalf("expected disabled/empty domain drilldown, got %+v", domainDrill)
	}
	clientDrill := s.GetDNSClientDomains("1h", "192.168.1.101")
	if clientDrill.Enabled || len(clientDrill.Domains) != 0 {
		t.Fatalf("expected disabled/empty client drilldown, got %+v", clientDrill)
	}

	// Enable, record, then clear -> immediate empty.
	s.SetDNSLoggingEnabled(true)
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "example.com", QueryType: "A", ClientIP: "192.168.1.101"})
	if got := s.GetDNSQueryStatistics("1h"); len(got.TopDomains) == 0 {
		t.Fatalf("setup: expected at least one domain before ClearDNSStats")
	}

	s.ClearDNSStats()
	after := s.GetDNSQueryStatistics("1h")
	if len(after.TopDomains) != 0 || len(after.TopClients) != 0 {
		t.Fatalf("expected empty lists immediately after ClearDNSStats, got %+v", after)
	}
}

// TestStatisticsService_DNSDrilldown_TopDomainsRegression is drilldown plan
// T-05 item 7: GetStatistics().TopDomains (the pre-existing /api/statistics
// endpoint, unrelated to the new /api/statistics/dns endpoints) must keep
// computing exactly the values it did before the (domain, client) pair ring
// replaced the old single-dimension domainCount map. This mirrors
// TestStatisticsService_RecordDNSEvent_TopDomainsRanking in
// statistics_test.go but adds multiple clients per domain, which the old
// single-dimension ring could never have distinguished, to prove the
// per-domain sum derived from `pairs` is unaffected by how many distinct
// clients contributed to it.
func TestStatisticsService_DNSDrilldown_TopDomainsRegression(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	for i := 0; i < 5; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "www.youtube.com", QueryType: "A", ClientIP: "192.168.1.101"})
	}
	for i := 0; i < 3; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "netflix.com", QueryType: "A", ClientIP: "192.168.1.102"})
	}
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "cdn.jsdelivr.net", QueryType: "AAAA", ClientIP: "192.168.1.105"})

	stats := s.GetStatistics("1h")
	if stats.DNSQueries != 9 {
		t.Fatalf("expected 9 total queries, got %d", stats.DNSQueries)
	}
	if len(stats.TopDomains) != 3 {
		t.Fatalf("expected 3 distinct domains, got %+v", stats.TopDomains)
	}
	if stats.TopDomains[0].Domain != "www.youtube.com" || stats.TopDomains[0].Count != 5 {
		t.Fatalf("expected top domain www.youtube.com/5, got %+v", stats.TopDomains[0])
	}
	if stats.TopDomains[1].Domain != "netflix.com" || stats.TopDomains[1].Count != 3 {
		t.Fatalf("expected second domain netflix.com/3, got %+v", stats.TopDomains[1])
	}
	if stats.TopDomains[2].Domain != "cdn.jsdelivr.net" || stats.TopDomains[2].Count != 1 || stats.TopDomains[2].QueryType != "AAAA" {
		t.Fatalf("expected third domain cdn.jsdelivr.net/1/AAAA, got %+v", stats.TopDomains[2])
	}
	var sumPercent float64
	for _, d := range stats.TopDomains {
		sumPercent += d.Percent
	}
	if sumPercent > 100.01 {
		t.Fatalf("sum of TopDomains percent exceeds 100%%: %v", sumPercent)
	}
	if !stats.DNSLoggingEnabled {
		t.Fatalf("expected DNSLoggingEnabled=true")
	}
}

// TestStatisticsService_ConcurrentDNSDrilldown is drilldown plan T-05 item 9:
// drive RecordDNSEvent, GetDNSQueryStatistics and GetDNSClientDomains from
// separate goroutines simultaneously. Run with `go test -race`.
func TestStatisticsService_ConcurrentDNSDrilldown(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.RecordDNSEvent(model.DNSLogEvent{
				Kind:      model.DNSLogQuery,
				Domain:    fmt.Sprintf("d%d.example.com", i%50),
				QueryType: "A",
				ClientIP:  fmt.Sprintf("192.168.1.%d", 100+(i%5)),
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.GetDNSQueryStatistics("1h")
			_ = s.GetDNSQueryStatistics("24h")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.GetDNSClientDomains("1h", fmt.Sprintf("192.168.1.%d", 100+(i%5)))
			_ = s.GetDNSDomainClients("1h", fmt.Sprintf("d%d.example.com", i%50))
		}
	}()
	wg.Wait()
}
