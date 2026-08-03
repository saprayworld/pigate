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

// --- Per-IP bandwidth series (docs/ref/todo/statistics-traffic-bandwidth-chart-plan.md T-03) ---

// TestGetTrafficHostDetail_SeriesMatchesTotals is plan T-03 case 1 — the most
// important test of this plan: for both windows, sum(Series[].Bytes) must
// equal TotalBytes, sum(Series[].BytesUp) must equal TotalBytesUp, and
// sum(Series[].BytesDown) must equal TotalBytesDown, exactly (plan §0 item
// 4/§2.2 decision 1 — HostSeries is built from the same convBytes map
// TotalBytes is summed from, under the same RLock).
func TestGetTrafficHostDetail_SeriesMatchesTotals(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{
				{Key: "out", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53},
				{Key: "in", SrcIP: "1.1.1.1", DstIP: "192.168.1.50", Proto: 6, DstPort: 443},
				{Key: "other", SrcIP: "192.168.1.60", DstIP: "9.9.9.9", Proto: 6, DstPort: 443},
			},
			{
				{Key: "out", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 300},
				{Key: "in", SrcIP: "1.1.1.1", DstIP: "192.168.1.50", Proto: 6, DstPort: 443, BytesOrig: 250, BytesReply: 750},
				{Key: "other", SrcIP: "192.168.1.60", DstIP: "9.9.9.9", Proto: 6, DstPort: 443, BytesOrig: 5000, BytesReply: 5000},
			},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	for _, window := range []string{"1h", "24h"} {
		got := s.GetTrafficHostDetail(window, "192.168.1.50", 100)
		bytes, up, down := seriesSum(got.Series)
		if bytes != got.TotalBytes {
			t.Fatalf("window %s: sum(series.bytes)=%d != TotalBytes=%d", window, bytes, got.TotalBytes)
		}
		if up != got.TotalBytesUp {
			t.Fatalf("window %s: sum(series.bytesUp)=%d != TotalBytesUp=%d", window, up, got.TotalBytesUp)
		}
		if down != got.TotalBytesDown {
			t.Fatalf("window %s: sum(series.bytesDown)=%d != TotalBytesDown=%d", window, down, got.TotalBytesDown)
		}
	}
}

// TestGetTrafficHostDetail_SeriesLengthFixed is plan T-03 case 2: Series
// always has a fixed length (12 for 1h, 288 for 24h) with unique,
// oldest -> newest timestamps, mirroring TestBandwidthSeries_FixedLengthAndSpacing.
func TestGetTrafficHostDetail_SeriesLengthFixed(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 100}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	cases := []struct {
		window string
		want   int
	}{{"1h", trafficWindow1hBuckets}, {"24h", trafficDetailBucketMax}}
	for _, c := range cases {
		got := s.GetTrafficHostDetail(c.window, "192.168.1.50", 100)
		if len(got.Series) != c.want {
			t.Fatalf("window %s: expected %d points, got %d", c.window, c.want, len(got.Series))
		}
		seen := make(map[string]bool, len(got.Series))
		var prev time.Time
		for i, p := range got.Series {
			ts, err := time.Parse(time.RFC3339, p.Ts)
			if err != nil {
				t.Fatalf("window %s point %d: ts %q failed to parse: %v", c.window, i, p.Ts, err)
			}
			if seen[p.Ts] {
				t.Fatalf("window %s: duplicate ts %q", c.window, p.Ts)
			}
			seen[p.Ts] = true
			if i > 0 {
				if got := ts.Sub(prev); got != trafficDetailBucketSpan {
					t.Fatalf("window %s point %d: expected 5m spacing, got %s", c.window, i, got)
				}
			}
			prev = ts
		}
	}
}

// TestGetTrafficHostDetail_AllSevenWindows_SeriesLengthFixed is docs/ref/todo/
// statistics-window-granularity-plan.md T-07 item 1: HostSeries length across
// all 7 supported windows must match statsWindowBuckets exactly.
func TestGetTrafficHostDetail_AllSevenWindows_SeriesLengthFixed(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 100}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	cases := []struct {
		window string
		want   int
	}{
		{"15m", 3}, {"30m", 6}, {"1h", 12}, {"3h", 36}, {"6h", 72}, {"12h", 144}, {"24h", 288},
	}
	for _, c := range cases {
		got := s.GetTrafficHostDetail(c.window, "192.168.1.50", 100)
		if len(got.Series) != c.want {
			t.Fatalf("window %s: expected %d points, got %d", c.window, c.want, len(got.Series))
		}
		bytes, up, down := seriesSum(got.Series)
		if bytes != got.TotalBytes || up != got.TotalBytesUp || down != got.TotalBytesDown {
			t.Fatalf("window %s: series sum (bytes=%d up=%d down=%d) != totals (bytes=%d up=%d down=%d)",
				c.window, bytes, up, down, got.TotalBytes, got.TotalBytesUp, got.TotalBytesDown)
		}
	}
}

// TestGetTrafficHostDetail_SeriesIsPerIPNotNetworkWide is plan T-03 case 3 —
// guards against a regression that wires the network-wide breakdown.Series
// into TrafficHostDetail.Series instead of breakdown.HostSeries (plan §2.2
// decision 1/2, Caution 2): with another IP carrying far more traffic than
// the drilled IP, sum(Series) must NOT equal ObservedBytes.
func TestGetTrafficHostDetail_SeriesIsPerIPNotNetworkWide(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{
				{Key: "small", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53},
				{Key: "big", SrcIP: "192.168.1.99", DstIP: "9.9.9.9", Proto: 6, DstPort: 443},
			},
			{
				{Key: "small", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 100},
				{Key: "big", SrcIP: "192.168.1.99", DstIP: "9.9.9.9", Proto: 6, DstPort: 443, BytesOrig: 50000, BytesReply: 50000},
			},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	got := s.GetTrafficHostDetail("1h", "192.168.1.50", 100)
	bytes, _, _ := seriesSum(got.Series)
	if bytes != got.TotalBytes {
		t.Fatalf("sanity: sum(series.bytes)=%d should still equal this IP's TotalBytes=%d", bytes, got.TotalBytes)
	}
	if bytes == got.ObservedBytes {
		t.Fatalf("expected per-IP series sum (%d) to differ from network-wide ObservedBytes (%d) — looks like breakdown.Series leaked in instead of HostSeries", bytes, got.ObservedBytes)
	}
}

// TestGetTrafficHostDetail_SeriesZeroFilledWhenNotFound is plan T-03 case 4:
// an IP with no data at all must still get a fixed-length, all-zero,
// non-nil Series (plan T-01/T-02 step 5).
func TestGetTrafficHostDetail_SeriesZeroFilledWhenNotFound(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 100}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	got := s.GetTrafficHostDetail("1h", "192.168.1.200", 100)
	if got.Found {
		t.Fatalf("expected Found=false for an IP absent from the window")
	}
	if got.Series == nil {
		t.Fatalf("expected Series to be a non-nil, zero-filled slice, got nil")
	}
	if len(got.Series) != trafficWindow1hBuckets {
		t.Fatalf("expected %d zero-filled points, got %d", trafficWindow1hBuckets, len(got.Series))
	}
	bytes, up, down := seriesSum(got.Series)
	if bytes != 0 || up != 0 || down != 0 {
		t.Fatalf("expected all-zero series for a not-found IP, got bytes=%d up=%d down=%d", bytes, up, down)
	}
}

// TestGetTrafficTopHosts_SeriesIsNetworkWide is plan T-03 case 5: the
// list-page endpoint's Series must be the network-wide, LAN-relative series
// (breakdown.Series, "ของฟรี" per plan §2.1) — sum(Series) == ObservedBytes,
// fixed length.
func TestGetTrafficTopHosts_SeriesIsNetworkWide(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{
				{Key: "a", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53},
				{Key: "b", SrcIP: "8.8.4.4", DstIP: "192.168.1.60", Proto: 6, DstPort: 443},
			},
			{
				{Key: "a", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 100},
				{Key: "b", SrcIP: "8.8.4.4", DstIP: "192.168.1.60", Proto: 6, DstPort: 443, BytesOrig: 300, BytesReply: 300},
			},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll()
	s.traffic.poll()

	for _, c := range []struct {
		window string
		want   int
	}{{"1h", trafficWindow1hBuckets}, {"24h", trafficDetailBucketMax}} {
		got := s.GetTrafficTopHosts(c.window, 100)
		if len(got.Series) != c.want {
			t.Fatalf("window %s: expected %d points, got %d", c.window, c.want, len(got.Series))
		}
		bytes, _, _ := seriesSum(got.Series)
		if bytes != got.ObservedBytes {
			t.Fatalf("window %s: sum(series.bytes)=%d != ObservedBytes=%d", c.window, bytes, got.ObservedBytes)
		}
	}
}

// TestGetTrafficBreakdown_HostSeriesNilWithoutFocusIP is plan T-03 case 6 —
// a regression guard for T-02's rename (GetTrafficBreakdown ->
// getTrafficBreakdown wrapper): calling GetTrafficBreakdown (no focus IP)
// must leave HostSeries nil, and Series/Observed/Convs must be unaffected by
// the T-02 change (plan Caution 12).
func TestGetTrafficBreakdown_HostSeriesNilWithoutFocusIP(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 100, BytesReply: 100}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll()
	s.poll()

	bd := s.GetTrafficBreakdown("1h")
	if bd.HostSeries != nil {
		t.Fatalf("expected HostSeries=nil when GetTrafficBreakdown is called without a focus IP, got %+v", bd.HostSeries)
	}
	if bd.Observed != 200 {
		t.Fatalf("expected Observed=200 unchanged, got %d", bd.Observed)
	}
	if got := bd.Convs["192.168.1.50|8.8.8.8|UDP|53"]; got.Total() != 200 {
		t.Fatalf("expected Convs unchanged, got %+v", got)
	}
	if len(bd.Series) != trafficWindow1hBuckets {
		t.Fatalf("expected Series unchanged (len %d), got %d", trafficWindow1hBuckets, len(bd.Series))
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

// TestGetTrafficHostDetail_CurrentRateSameRuleAsTotalBytes is plan T-06 item
// 4: CurrentRateBpsUp/CurrentRateBpsDown must use the exact same
// src==ip-then-dst==ip counting rule (not else-if) that TotalBytes already
// uses, so a same-IP-both-sides conversation (e.g. loopback) counts its rate
// twice too — otherwise the drill-down page's "current speed" figure would
// disagree with its own TotalBytes for the identical reason.
func TestGetTrafficHostDetail_CurrentRateSameRuleAsTotalBytes(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "loop", SrcIP: "192.168.1.50", DstIP: "192.168.1.50", Proto: 6, DstPort: 8080}},
			{{Key: "loop", SrcIP: "192.168.1.50", DstIP: "192.168.1.50", Proto: 6, DstPort: 8080, BytesOrig: 100, BytesReply: 400}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll() // seed
	s.traffic.poll() // delta

	elapsed := s.traffic.lastRateElapsed.Seconds()
	if elapsed <= 0 {
		t.Fatalf("expected positive elapsed after poll, got %v", s.traffic.lastRateElapsed)
	}

	got := s.GetTrafficHostDetail("1h", "192.168.1.50", 100)
	wantUp := uint64(float64(100)*8/elapsed) * 2
	wantDown := uint64(float64(400)*8/elapsed) * 2
	if got.CurrentRateBpsUp != wantUp || got.CurrentRateBpsDown != wantDown {
		t.Fatalf("expected src==dst row to count rate twice like TotalBytes: got up=%d down=%d want up=%d down=%d",
			got.CurrentRateBpsUp, got.CurrentRateBpsDown, wantUp, wantDown)
	}
	if got.RateSampledAt == "" {
		t.Fatalf("expected RateSampledAt to be set once a rate sample exists")
	}
}

// TestGetTrafficHostDetail_ConversationRateMatchesHostTotal checks the
// per-row RateBpsUp/RateBpsDown (asSource/asDestination) sum to the exact
// same host-level CurrentRateBpsUp/CurrentRateBpsDown figure, for a
// same-IP-both-sides conversation that appears in both lists at once (the
// same fixture as TestGetTrafficHostDetail_CurrentRateSameRuleAsTotalBytes
// above) — the per-row rate must not silently diverge from what it sums to.
func TestGetTrafficHostDetail_ConversationRateMatchesHostTotal(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "loop", SrcIP: "192.168.1.50", DstIP: "192.168.1.50", Proto: 6, DstPort: 8080}},
			{{Key: "loop", SrcIP: "192.168.1.50", DstIP: "192.168.1.50", Proto: 6, DstPort: 8080, BytesOrig: 100, BytesReply: 400}},
		},
	}
	s := newTestStatisticsService(t, acct)
	s.traffic.poll() // seed
	s.traffic.poll() // delta

	got := s.GetTrafficHostDetail("1h", "192.168.1.50", 100)
	if len(got.AsSource) != 1 || len(got.AsDestination) != 1 {
		t.Fatalf("expected the loopback conversation in both lists, got asSource=%d asDestination=%d", len(got.AsSource), len(got.AsDestination))
	}
	sumUp := got.AsSource[0].RateBpsUp + got.AsDestination[0].RateBpsUp
	sumDown := got.AsSource[0].RateBpsDown + got.AsDestination[0].RateBpsDown
	if sumUp != got.CurrentRateBpsUp || sumDown != got.CurrentRateBpsDown {
		t.Fatalf("expected per-row rates to sum to the host total: sumUp=%d sumDown=%d hostUp=%d hostDown=%d",
			sumUp, sumDown, got.CurrentRateBpsUp, got.CurrentRateBpsDown)
	}
	if got.AsSource[0].RateBpsUp == 0 && got.AsSource[0].RateBpsDown == 0 {
		t.Fatalf("expected a non-zero per-conversation rate once a sample exists")
	}
}

// TestGetTrafficHostDetail_RateZeroBeforeFirstSample is plan T-05 case 3: a
// fresh service that has never rotated a rate accumulator must report a zero
// rate and an empty RateSampledAt, never a stale/undefined value.
func TestGetTrafficHostDetail_RateZeroBeforeFirstSample(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	got := s.GetTrafficHostDetail("1h", "192.168.1.50", 100)
	if got.CurrentRateBpsUp != 0 || got.CurrentRateBpsDown != 0 {
		t.Fatalf("expected zero rate before any poll tick, got up=%d down=%d", got.CurrentRateBpsUp, got.CurrentRateBpsDown)
	}
	if got.RateSampledAt != "" {
		t.Fatalf("expected empty RateSampledAt before any poll tick, got %q", got.RateSampledAt)
	}
}
