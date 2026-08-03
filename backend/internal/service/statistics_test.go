package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"pigate/internal/model"
)

func newTestStatisticsService(t *testing.T, acct *fakeTrafficAccounting) *StatisticsService {
	t.Helper()
	traffic := newTestTrafficStatsService(t, acct, nil)
	return NewStatisticsService(traffic, traffic.repo, traffic.dhcp, defaultMaxTrackedDNSPairs, defaultMaxTrackedDNSClients)
}

// TestStatisticsService_RecordFirewallLog_OnlyCountsDrop is plan T-04 case 5:
// RecordFirewallLog must count only "DROP" entries and rank them correctly;
// "PASS" entries must not be counted at all.
func TestStatisticsService_RecordFirewallLog_OnlyCountsDrop(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	s.RecordFirewallLog(model.FirewallLog{Action: "PASS", Src: "192.168.1.10", Proto: "TCP", Port: "80"})
	for i := 0; i < 5; i++ {
		s.RecordFirewallLog(model.FirewallLog{Action: "DROP", Src: "203.0.113.9", Proto: "TCP", Port: "22"})
	}
	for i := 0; i < 2; i++ {
		s.RecordFirewallLog(model.FirewallLog{Action: "DROP", Src: "203.0.113.10", Proto: "UDP", Port: "53"})
	}

	stats := s.GetStatistics("1h")
	if stats.DeniedEvents != 7 {
		t.Fatalf("expected 7 DROP events counted (PASS excluded), got %d", stats.DeniedEvents)
	}
	if len(stats.DeniedSources) != 2 {
		t.Fatalf("expected 2 distinct denied sources, got %+v", stats.DeniedSources)
	}
	if stats.DeniedSources[0].IP != "203.0.113.9" || stats.DeniedSources[0].Count != 5 {
		t.Fatalf("expected top denied source to be 203.0.113.9 with count 5, got %+v", stats.DeniedSources[0])
	}
	if len(stats.DeniedPorts) != 2 {
		t.Fatalf("expected 2 distinct denied proto/port pairs, got %+v", stats.DeniedPorts)
	}
	if stats.DeniedPorts[0].Proto != "TCP" || stats.DeniedPorts[0].Port != "22" || stats.DeniedPorts[0].Count != 5 {
		t.Fatalf("expected top denied port to be TCP/22 with count 5, got %+v", stats.DeniedPorts[0])
	}
	if !stats.DeniedSampled {
		t.Fatalf("expected DeniedSampled to always be true")
	}
}

// TestStatisticsService_GetStatistics_ComposesFromBreakdown exercises the
// full GetStatistics path against a real (fake-backed) TrafficStatsService:
// Top Sources/Destinations/Conversations must reflect the same delta the
// bucket ring computed, and their sum must not exceed ObservedBytes.
func TestStatisticsService_GetStatistics_ComposesFromBreakdown(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 0, BytesReply: 0}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 200, BytesReply: 800}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll() // seed
	s.traffic.poll() // delta 1000 (orig 200 / reply 800)

	stats := s.GetStatistics("1h")
	if stats.ObservedBytes != 1000 {
		t.Fatalf("expected observedBytes=1000, got %d", stats.ObservedBytes)
	}
	if len(stats.TopSources) != 1 || stats.TopSources[0].IP != "192.168.1.50" || stats.TopSources[0].Bytes != 1000 {
		t.Fatalf("unexpected top sources: %+v", stats.TopSources)
	}
	if stats.TopSources[0].BytesUp != 200 || stats.TopSources[0].BytesDown != 800 {
		t.Fatalf("expected TopSources up=200/down=800 (orig=up for a source row), got %+v", stats.TopSources[0])
	}
	if !stats.TopSources[0].Private {
		t.Fatalf("expected 192.168.1.50 to be flagged Private (RFC1918)")
	}
	if len(stats.TopDestinations) != 1 || stats.TopDestinations[0].IP != "8.8.8.8" || stats.TopDestinations[0].Bytes != 1000 {
		t.Fatalf("unexpected top destinations: %+v", stats.TopDestinations)
	}
	// Top Destinations uses the same orig-is-up/reply-is-down convention as
	// TopSources and TopConversations — no flip.
	if stats.TopDestinations[0].BytesUp != 200 || stats.TopDestinations[0].BytesDown != 800 {
		t.Fatalf("expected TopDestinations up=200/down=800 (same convention as TopSources), got %+v", stats.TopDestinations[0])
	}
	if stats.TopDestinations[0].Private {
		t.Fatalf("expected 8.8.8.8 to NOT be flagged Private")
	}
	if len(stats.TopConversations) != 1 {
		t.Fatalf("unexpected top conversations: %+v", stats.TopConversations)
	}
	conv := stats.TopConversations[0]
	if conv.SrcIP != "192.168.1.50" || conv.DstIP != "8.8.8.8" || conv.Proto != "UDP" || conv.DstPort != 53 || conv.Bytes != 1000 {
		t.Fatalf("unexpected conversation row: %+v", conv)
	}
	if conv.BytesUp != 200 || conv.BytesDown != 800 {
		t.Fatalf("expected conversation up=200/down=800 (relative to SrcIP, same as TopSources), got %+v", conv)
	}
	for _, h := range stats.TopSources {
		if h.Bytes > stats.ObservedBytes {
			t.Fatalf("top source bytes exceed observedBytes: %+v", h)
		}
		if h.BytesUp+h.BytesDown != h.Bytes {
			t.Fatalf("expected bytesUp+bytesDown == bytes for every TopSources row, got %+v", h)
		}
	}
	for _, h := range stats.TopDestinations {
		if h.BytesUp+h.BytesDown != h.Bytes {
			t.Fatalf("expected bytesUp+bytesDown == bytes for every TopDestinations row, got %+v", h)
		}
	}
	for _, c := range stats.TopConversations {
		if c.BytesUp+c.BytesDown != c.Bytes {
			t.Fatalf("expected bytesUp+bytesDown == bytes for every TopConversations row, got %+v", c)
		}
	}
}

// TestStatisticsService_GetStatistics_OmitsRateFields is plan T-06 item 6:
// TopHost is shared verbatim between GetTrafficTopHosts (Traffic list page,
// rate-populated) and GetStatistics (Overview page, which must never gain
// these fields — plan §1.7). Marshals the actual Overview response and
// asserts the new rate JSON keys never appear, proving `omitempty` is doing
// its job rather than just trusting the zero-value convention.
func TestStatisticsService_GetStatistics_OmitsRateFields(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 6, DstPort: 443}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 6, DstPort: 443, BytesOrig: 100, BytesReply: 400}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	stats := s.GetStatistics("1h")
	raw, err := json.Marshal(stats)
	if err != nil {
		t.Fatalf("marshal TrafficStatistics: %v", err)
	}
	for _, key := range []string{`"rateBpsUp"`, `"rateBpsDown"`, `"rateSampledAt"`} {
		if bytes.Contains(raw, []byte(key)) {
			t.Fatalf("expected Overview response JSON to omit %s entirely (unpopulated + omitempty), got: %s", key, raw)
		}
	}
}

// TestStatisticsService_GetStatistics_WindowFallback mirrors
// HandleGetTrafficDetail's whitelist behavior at the service level: any
// window other than "24h" collapses to "1h".
func TestStatisticsService_GetStatistics_WindowFallback(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	if got := s.GetStatistics("bogus").Window; got != "1h" {
		t.Fatalf("expected fallback window 1h, got %q", got)
	}
	if got := s.GetStatistics("24h").Window; got != "24h" {
		t.Fatalf("expected 24h to pass through, got %q", got)
	}
}

// TestStatisticsService_ConcurrentPollRecordAndGetStatistics is plan T-04
// case 6: drive poll(), RecordFirewallLog(), and GetStatistics() from
// separate goroutines simultaneously. Run with `go test -race`.
func TestStatisticsService_ConcurrentPollRecordAndGetStatistics(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.traffic.acct = &raceFakeAcct{}

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.traffic.poll()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			action := "DROP"
			if i%3 == 0 {
				action = "PASS"
			}
			s.RecordFirewallLog(model.FirewallLog{
				Action: action,
				Src:    fmt.Sprintf("203.0.113.%d", i%20),
				Proto:  "TCP",
				Port:   "443",
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.GetStatistics("1h")
			_ = s.GetStatistics("24h")
		}
	}()
	wg.Wait()
}

// TestStatisticsService_RecordDNSEvent_TopDomainsRanking is T-11 item 8:
// query events must rank correctly and percent must be computed from the
// query count (not observedBytes), with the total never exceeding 100%.
func TestStatisticsService_RecordDNSEvent_TopDomainsRanking(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	for i := 0; i < 5; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "www.youtube.com", QueryType: "A"})
	}
	for i := 0; i < 3; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "netflix.com", QueryType: "A"})
	}
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "cdn.jsdelivr.net", QueryType: "AAAA"})

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

// TestStatisticsService_DomainRingCap is T-11 item 9: flooding with 5,000
// unique domains must not panic, must respect the per-bucket DNS stats caps
// (defaultMaxTrackedDNSPairs/defaultMaxTrackedDNSClients, configurable via
// dns-stats-max-pairs/dns-stats-max-clients), and must set DNSTruncated.
func TestStatisticsService_DomainRingCap(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	for i := 0; i < 5000; i++ {
		s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: fmt.Sprintf("d%d.example.com", i), QueryType: "A"})
	}

	stats := s.GetStatistics("1h")
	if !stats.DNSTruncated {
		t.Fatalf("expected DNSTruncated=true after flooding with 5000 unique domains")
	}
	if len(stats.TopDomains) > statsTopN {
		t.Fatalf("TopDomains exceeds statsTopN: %d", len(stats.TopDomains))
	}
}

// TestStatisticsService_ClearDNSStats is T-11 item 10: clearing wipes both
// the domain ring and the reverse cache (via the destinations/conversations
// domain enrichment) immediately.
func TestStatisticsService_ClearDNSStats(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "142.250.80.46", Proto: 6, DstPort: 443, BytesOrig: 0, BytesReply: 0}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "142.250.80.46", Proto: 6, DstPort: 443, BytesOrig: 150, BytesReply: 850}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	s.SetDNSLoggingEnabled(true)
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: "www.youtube.com", QueryType: "A"})
	s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "www.youtube.com", AnswerIP: "142.250.80.46"})

	before := s.GetStatistics("1h")
	if len(before.TopDomains) == 0 {
		t.Fatalf("setup: expected at least one top domain before Clear")
	}
	if before.TopDestinations[0].Domain != "www.youtube.com" {
		t.Fatalf("setup: expected destination domain enrichment before Clear, got %+v", before.TopDestinations[0])
	}

	s.ClearDNSStats()

	after := s.GetStatistics("1h")
	if len(after.TopDomains) != 0 {
		t.Fatalf("expected TopDomains empty after ClearDNSStats, got %+v", after.TopDomains)
	}
	if after.TopDestinations[0].Domain != "" {
		t.Fatalf("expected destination domain cleared after ClearDNSStats, got %+v", after.TopDestinations[0])
	}
	if after.TopDestinations[0].Bytes != before.TopDestinations[0].Bytes {
		t.Fatalf("expected bytes unaffected by ClearDNSStats: before=%d after=%d", before.TopDestinations[0].Bytes, after.TopDestinations[0].Bytes)
	}
}

// TestStatisticsService_TopHostsConversations_RegressionWhenDNSStatsEmpty is
// T-11 item 11: with the DNS-stats switch off / cache empty (the default
// state of a freshly constructed StatisticsService), TopDestinations/
// TopConversations must be byte-for-byte identical to what they were before
// this feature existed — same bytes/percent/hostname, Domain/DstDomain always
// "".
func TestStatisticsService_TopHostsConversations_RegressionWhenDNSStatsEmpty(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 0, BytesReply: 0}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 250, BytesReply: 750}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	stats := s.GetStatistics("1h")
	if stats.DNSLoggingEnabled {
		t.Fatalf("expected DNSLoggingEnabled=false by default")
	}
	if len(stats.TopDomains) != 0 {
		t.Fatalf("expected empty TopDomains by default, got %+v", stats.TopDomains)
	}
	if len(stats.TopDestinations) != 1 || stats.TopDestinations[0].IP != "8.8.8.8" || stats.TopDestinations[0].Bytes != 1000 {
		t.Fatalf("unexpected top destinations: %+v", stats.TopDestinations)
	}
	if stats.TopDestinations[0].Domain != "" {
		t.Fatalf("expected Domain empty when reverse cache has no data, got %q", stats.TopDestinations[0].Domain)
	}
	if len(stats.TopConversations) != 1 || stats.TopConversations[0].DstDomain != "" {
		t.Fatalf("expected DstDomain empty when reverse cache has no data, got %+v", stats.TopConversations)
	}
}

// TestStatisticsService_ConcurrentDNSEventsAndSetLimits is T-11 item 14: run
// RecordDNSEvent + GetStatistics + SetReverseCacheLimits concurrently under
// `go test -race`.
// TestStatisticsService_TopDestinationsSameConventionAsTopSources guards
// the up/down convention (fix for the PR 117 up/down swap bug): a single
// LAN->internet flow must show up as "down-heavy" (download) in BOTH Top
// Source Hosts and Top Destinations, since bytesUp/bytesDown always track
// the flow's own orig/reply direction regardless of which side (src or
// dst) the row's ip is on — guarding against a future change
// re-introducing a per-row flip.
func TestStatisticsService_TopDestinationsSameConventionAsTopSources(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 0, BytesReply: 0}},
			// Deliberately lopsided: 100 orig (LAN->internet, upload) vs 9000
			// reply (internet->LAN, download) — a typical download-heavy flow.
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 100, BytesReply: 9000}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	stats := s.GetStatistics("1h")
	if len(stats.TopSources) != 1 || len(stats.TopDestinations) != 1 {
		t.Fatalf("expected exactly one source and one destination row, got sources=%+v dests=%+v", stats.TopSources, stats.TopDestinations)
	}
	src := stats.TopSources[0]
	dst := stats.TopDestinations[0]
	if src.BytesUp != 100 || src.BytesDown != 9000 {
		t.Fatalf("expected LAN source row up=100 (orig)/down=9000 (reply), got %+v", src)
	}
	if dst.BytesUp != 100 || dst.BytesDown != 9000 {
		t.Fatalf("expected internet destination row up=100 (orig)/down=9000 (reply), same convention as TopSources, got %+v", dst)
	}
}

func TestStatisticsService_ConcurrentDNSEventsAndSetLimits(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.SetDNSLoggingEnabled(true)

	const iterations = 200
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogQuery, Domain: fmt.Sprintf("d%d.example.com", i%50), QueryType: "A"})
			s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: fmt.Sprintf("d%d.example.com", i%50), AnswerIP: fmt.Sprintf("10.0.%d.%d", (i/256)%256, i%256)})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.GetStatistics("1h")
			_ = s.GetStatistics("24h")
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.SetReverseCacheLimits(60+(i%10), 128+(i%100))
		}
	}()
	wg.Wait()
}
