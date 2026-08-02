package service

import (
	"fmt"
	"testing"
	"time"

	"pigate/internal/model"
)

// TestGetTrafficTopHosts_ReturnsMoreThanStatsTopN is plan T-05 case 1: the
// endpoint must not be cut to statsTopN like GetStatistics's
// TopSources/TopDestinations are — proving buildTopHosts's limit parameter
// (not the old statsTopN constant) drives this response.
func TestGetTrafficTopHosts_ReturnsMoreThanStatsTopN(t *testing.T) {
	flows := make([]model.FlowSample, 0, statsTopN+5)
	for i := 0; i < statsTopN+5; i++ {
		flows = append(flows, model.FlowSample{
			Key:     fmt.Sprintf("f%d", i),
			SrcIP:   fmt.Sprintf("192.168.1.%d", 10+i),
			DstIP:   "8.8.8.8",
			Proto:   6,
			DstPort: 443,
		})
	}
	seedFlows := make([]model.FlowSample, len(flows))
	copy(seedFlows, flows)
	for i := range flows {
		flows[i].BytesOrig = uint64(100 + i)
		flows[i].BytesReply = uint64(200 + i)
	}
	acct := &fakeTrafficAccounting{flowResponses: [][]model.FlowSample{seedFlows, flows}}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	got := s.GetTrafficTopHosts("1h", 100)
	if len(got.Sources) != statsTopN+5 {
		t.Fatalf("expected %d source rows (more than statsTopN=%d), got %d: %+v", statsTopN+5, statsTopN, len(got.Sources), got.Sources)
	}
	if len(got.Destinations) != 1 || got.Destinations[0].IP != "8.8.8.8" {
		t.Fatalf("unexpected destinations: %+v", got.Destinations)
	}
	if got.Limit != 100 {
		t.Fatalf("expected limit echoed as 100, got %d", got.Limit)
	}
	if got.Window != "1h" {
		t.Fatalf("expected window 1h, got %q", got.Window)
	}
}

// TestGetTrafficTopHosts_ClampsLimit is plan T-05 case 1 (limit clamping):
// <=0 falls back to the default, and an absurdly large value clamps to
// trafficTopHostsMaxLimit.
func TestGetTrafficTopHosts_ClampsLimit(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	if got := s.GetTrafficTopHosts("1h", 0).Limit; got != trafficTopHostsDefaultLimit {
		t.Fatalf("expected default limit %d for <=0, got %d", trafficTopHostsDefaultLimit, got)
	}
	if got := s.GetTrafficTopHosts("1h", -5).Limit; got != trafficTopHostsDefaultLimit {
		t.Fatalf("expected default limit %d for negative, got %d", trafficTopHostsDefaultLimit, got)
	}
	if got := s.GetTrafficTopHosts("1h", 999999).Limit; got != trafficTopHostsMaxLimit {
		t.Fatalf("expected clamp to %d, got %d", trafficTopHostsMaxLimit, got)
	}
}

// TestGetTrafficTopHosts_DeterministicRanking is plan T-05 case 1
// (determinism under repeated calls / map-iteration-order safety).
func TestGetTrafficTopHosts_DeterministicRanking(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{
				{Key: "a", SrcIP: "192.168.1.10", DstIP: "8.8.8.8", Proto: 6, DstPort: 443},
				{Key: "b", SrcIP: "192.168.1.11", DstIP: "1.1.1.1", Proto: 6, DstPort: 443},
			},
			{
				{Key: "a", SrcIP: "192.168.1.10", DstIP: "8.8.8.8", Proto: 6, DstPort: 443, BytesOrig: 100, BytesReply: 100},
				{Key: "b", SrcIP: "192.168.1.11", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 100, BytesReply: 100},
			},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	first := s.GetTrafficTopHosts("1h", 100)
	for i := 0; i < 20; i++ {
		got := s.GetTrafficTopHosts("1h", 100)
		if len(got.Sources) != len(first.Sources) {
			t.Fatalf("source count changed across calls")
		}
		for j := range got.Sources {
			if got.Sources[j].IP != first.Sources[j].IP {
				t.Fatalf("ranking not deterministic across calls: call0=%+v callN=%+v", first.Sources, got.Sources)
			}
		}
	}
}

// TestGetTrafficTopHosts_TruncatedPropagates is plan T-05 case 1 (Truncated
// propagation): flooding past the conversation cap must surface
// Truncated=true.
func TestGetTrafficTopHosts_TruncatedPropagates(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	n := s.traffic.maxTrackedConversations + 50
	flows := make([]model.FlowSample, n)
	for i := 0; i < n; i++ {
		flows[i] = model.FlowSample{
			Key:     fmt.Sprintf("scan-%d", i),
			SrcIP:   "192.168.1.5",
			DstIP:   fmt.Sprintf("10.0.%d.%d", i/255, i%255),
			Proto:   6,
			DstPort: uint16(1000 + i%1000),
		}
	}
	s.traffic.acct = &fakeTrafficAccounting{flowResponses: [][]model.FlowSample{flows}}
	s.traffic.poll() // seed

	for i := range flows {
		flows[i].BytesOrig = 20
		flows[i].BytesReply = 80
	}
	s.traffic.acct = &fakeTrafficAccounting{flowResponses: [][]model.FlowSample{flows}}
	s.traffic.poll() // delta, exceeds the conversation cap

	got := s.GetTrafficTopHosts("1h", 500)
	if !got.Truncated {
		t.Fatalf("expected Truncated=true once a bucket hits the conversation cap")
	}
}

// TestGetTrafficHostDetail_SourceOnly is plan T-05 case 2: an IP that only
// ever appears as a flow's SrcIP gets rows in AsSource and an empty
// AsDestination, with Direction="outbound".
func TestGetTrafficHostDetail_SourceOnly(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 200, BytesReply: 800}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	got := s.GetTrafficHostDetail("1h", "192.168.1.50", 100)
	if !got.Found {
		t.Fatalf("expected Found=true")
	}
	if len(got.AsSource) != 1 {
		t.Fatalf("expected 1 asSource row, got %+v", got.AsSource)
	}
	if len(got.AsDestination) != 0 {
		t.Fatalf("expected 0 asDestination rows (reverse-lookup requirement), got %+v", got.AsDestination)
	}
	if got.AsSource[0].Direction != "outbound" {
		t.Fatalf("expected Direction=outbound, got %q", got.AsSource[0].Direction)
	}
	if got.TotalBytes != 1000 || got.TotalBytesUp != 200 || got.TotalBytesDown != 800 {
		t.Fatalf("unexpected totals: %+v", got)
	}
}

// TestGetTrafficHostDetail_DestinationOnly is plan T-05 case 2 (mirror
// image): an IP that only ever appears as a flow's DstIP gets rows in
// AsDestination and an empty AsSource, with Direction="inbound" — this is
// the reverse drill-down the owner specifically asked for (plan §1.3).
func TestGetTrafficHostDetail_DestinationOnly(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "173.194.76.94", Proto: 6, DstPort: 443}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "173.194.76.94", Proto: 6, DstPort: 443, BytesOrig: 300, BytesReply: 700}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	got := s.GetTrafficHostDetail("1h", "173.194.76.94", 100)
	if !got.Found {
		t.Fatalf("expected Found=true")
	}
	if len(got.AsDestination) != 1 {
		t.Fatalf("expected 1 asDestination row, got %+v", got.AsDestination)
	}
	if len(got.AsSource) != 0 {
		t.Fatalf("expected 0 asSource rows, got %+v", got.AsSource)
	}
	if got.AsDestination[0].Direction != "inbound" {
		t.Fatalf("expected Direction=inbound, got %q", got.AsDestination[0].Direction)
	}
	if got.TotalBytes != 1000 || got.TotalBytesUp != 300 || got.TotalBytesDown != 700 {
		t.Fatalf("unexpected totals: %+v", got)
	}
}

// TestGetTrafficHostDetail_TotalsSumBeforeTruncation is plan T-05 case 2:
// TotalBytes must equal the sum over BOTH lists computed BEFORE the limit
// cut, and each row's Percent must be relative to that total (summing to
// ~100% here since there is exactly one conversation per direction).
func TestGetTrafficHostDetail_TotalsSumBeforeTruncation(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{
				{Key: "out", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53},
				{Key: "in", SrcIP: "1.1.1.1", DstIP: "192.168.1.50", Proto: 6, DstPort: 443},
			},
			{
				{Key: "out", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 100},
				{Key: "in", SrcIP: "1.1.1.1", DstIP: "192.168.1.50", Proto: 6, DstPort: 443, BytesOrig: 250, BytesReply: 250},
			},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	// limit=1 truncates each list to 1 row (already 1 row each here, so this
	// mainly guards that a real cap wouldn't change the header total).
	got := s.GetTrafficHostDetail("1h", "192.168.1.50", 1)
	wantTotal := uint64(200 + 500)
	if got.TotalBytes != wantTotal {
		t.Fatalf("expected TotalBytes=%d, got %d", wantTotal, got.TotalBytes)
	}
	var sumPercent float64
	for _, r := range got.AsSource {
		sumPercent += r.Percent
	}
	for _, r := range got.AsDestination {
		sumPercent += r.Percent
	}
	if sumPercent < 99.0 || sumPercent > 100.01 {
		t.Fatalf("expected row percents to sum to ~100%% of TotalBytes, got %v", sumPercent)
	}
}

// TestGetTrafficHostDetail_NotFound is plan T-05 case 2: an IP absent from
// the window returns Found=false with empty (never nil) slices.
func TestGetTrafficHostDetail_NotFound(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 200, BytesReply: 800}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	got := s.GetTrafficHostDetail("1h", "192.168.1.99", 100)
	if got.Found {
		t.Fatalf("expected Found=false for an IP absent from the window")
	}
	if got.AsSource == nil || len(got.AsSource) != 0 {
		t.Fatalf("expected AsSource to be an empty (non-nil) slice, got %#v", got.AsSource)
	}
	if got.AsDestination == nil || len(got.AsDestination) != 0 {
		t.Fatalf("expected AsDestination to be an empty (non-nil) slice, got %#v", got.AsDestination)
	}
}

// TestGetTrafficHostDetail_MalformedKeySkipped is plan T-05 case 2: a bucket
// containing a malformed conversation key must never panic — it is simply
// skipped, same as buildTopConversations already guarantees.
func TestGetTrafficHostDetail_MalformedKeySkipped(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.traffic.addBucket(
		time.Now(),
		map[string]dirBytes{},
		map[string]uint64{},
		nil,
		map[string]dirBytes{},
		map[string]dirBytes{"malformed-key-no-pipes": {Orig: 10, Reply: 10}},
		dirBytes{},
	)

	got := s.GetTrafficHostDetail("1h", "192.168.1.50", 100)
	if got.Found {
		t.Fatalf("expected Found=false when only a malformed key exists")
	}
	if len(got.AsSource) != 0 || len(got.AsDestination) != 0 {
		t.Fatalf("expected no rows from a malformed key, got asSource=%+v asDestination=%+v", got.AsSource, got.AsDestination)
	}
}

// TestGetStatistics_StillCappedAtStatsTopN is plan T-05 case 3: a regression
// guard that the buildTopHosts refactor (adding a limit parameter) did not
// change GetStatistics's existing statsTopN-cut behavior.
func TestGetStatistics_StillCappedAtStatsTopN(t *testing.T) {
	flows := make([]model.FlowSample, 0, statsTopN+10)
	for i := 0; i < statsTopN+10; i++ {
		flows = append(flows, model.FlowSample{
			Key:     fmt.Sprintf("f%d", i),
			SrcIP:   fmt.Sprintf("192.168.1.%d", 10+i),
			DstIP:   "8.8.8.8",
			Proto:   6,
			DstPort: 443,
		})
	}
	seedFlows := make([]model.FlowSample, len(flows))
	copy(seedFlows, flows)
	for i := range flows {
		flows[i].BytesOrig = uint64(100 + i)
		flows[i].BytesReply = uint64(200 + i)
	}
	acct := &fakeTrafficAccounting{flowResponses: [][]model.FlowSample{seedFlows, flows}}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	stats := s.GetStatistics("1h")
	if len(stats.TopSources) != statsTopN {
		t.Fatalf("expected GetStatistics to stay capped at statsTopN=%d, got %d", statsTopN, len(stats.TopSources))
	}
}
