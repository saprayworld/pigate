package service

import (
	"fmt"
	"sync"
	"testing"

	"pigate/internal/model"
)

func newTestStatisticsService(t *testing.T, acct *fakeTrafficAccounting) *StatisticsService {
	t.Helper()
	traffic := newTestTrafficStatsService(t, acct, nil)
	return NewStatisticsService(traffic, traffic.repo, traffic.dhcp)
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
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, Bytes: 0}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, Bytes: 1000}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll() // seed
	s.traffic.poll() // delta 1000

	stats := s.GetStatistics("1h")
	if stats.ObservedBytes != 1000 {
		t.Fatalf("expected observedBytes=1000, got %d", stats.ObservedBytes)
	}
	if len(stats.TopSources) != 1 || stats.TopSources[0].IP != "192.168.1.50" || stats.TopSources[0].Bytes != 1000 {
		t.Fatalf("unexpected top sources: %+v", stats.TopSources)
	}
	if !stats.TopSources[0].Private {
		t.Fatalf("expected 192.168.1.50 to be flagged Private (RFC1918)")
	}
	if len(stats.TopDestinations) != 1 || stats.TopDestinations[0].IP != "8.8.8.8" || stats.TopDestinations[0].Bytes != 1000 {
		t.Fatalf("unexpected top destinations: %+v", stats.TopDestinations)
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
	for _, h := range stats.TopSources {
		if h.Bytes > stats.ObservedBytes {
			t.Fatalf("top source bytes exceed observedBytes: %+v", h)
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
