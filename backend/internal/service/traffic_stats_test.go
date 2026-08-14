package service

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// fakeTrafficAccounting is a scripted TrafficAccountingManager: each call to
// DumpFlows/DumpRuleCounters returns the next queued response (and the last
// one repeats once the queue is drained), letting tests drive
// TrafficStatsService.poll() deterministically without a real kernel.
type fakeTrafficAccounting struct {
	flowResponses [][]model.FlowSample
	flowCall      int
	ruleResponses []map[string]model.RuleCounter
	ruleCall      int
}

func (f *fakeTrafficAccounting) DumpFlows() ([]model.FlowSample, error) {
	if len(f.flowResponses) == 0 {
		return nil, nil
	}
	idx := f.flowCall
	if idx >= len(f.flowResponses) {
		idx = len(f.flowResponses) - 1
	}
	f.flowCall++
	return f.flowResponses[idx], nil
}

func (f *fakeTrafficAccounting) DumpRuleCounters() (map[string]model.RuleCounter, error) {
	if len(f.ruleResponses) == 0 {
		return map[string]model.RuleCounter{}, nil
	}
	idx := f.ruleCall
	if idx >= len(f.ruleResponses) {
		idx = len(f.ruleResponses) - 1
	}
	f.ruleCall++
	return f.ruleResponses[idx], nil
}

// WatchFlowEnd is not driven through the real event plumbing by these tests
// (they call TrafficStatsService.onFlowEnd directly for determinism) — this
// stub only needs to satisfy kernel.TrafficAccountingManager and block until
// ctx is cancelled, like the real/mock implementations' contract.
func (f *fakeTrafficAccounting) WatchFlowEnd(ctx context.Context, cb func(model.FlowSample)) error {
	<-ctx.Done()
	return nil
}

// fakeDhcpForTraffic is a minimal DhcpManager stub — only GetActiveLeases is
// exercised by TrafficStatsService.
type fakeDhcpForTraffic struct {
	leases []model.ActiveDhcpLease
}

func (f *fakeDhcpForTraffic) ApplyConfig(cfgs []model.DhcpConfig, reservations []model.DhcpReservation) error {
	return nil
}
func (f *fakeDhcpForTraffic) GetActiveLeases() ([]model.ActiveDhcpLease, error) { return f.leases, nil }
func (f *fakeDhcpForTraffic) ReloadConfig() error                               { return nil }
func (f *fakeDhcpForTraffic) WatchLeases(ctx context.Context, callback func(event string, lease model.ActiveDhcpLease)) error {
	<-ctx.Done()
	return nil
}

func newTestTrafficStatsService(t *testing.T, acct *fakeTrafficAccounting, dhcp *fakeDhcpForTraffic) *TrafficStatsService {
	t.Helper()
	sqliteDB, err := db.InitDB(":memory:")
	if err != nil {
		t.Fatalf("init db: %v", err)
	}
	t.Cleanup(func() { sqliteDB.Close() })
	repo := db.NewRepository(sqliteDB)
	if dhcp == nil {
		dhcp = &fakeDhcpForTraffic{}
	}
	return NewTrafficStatsService(acct, repo, dhcp, kernel.NewMockSystemStats(), 0, 0, 0)
}

func TestTrafficStats_SeedThenDelta(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 200, BytesReply: 800}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 300, BytesReply: 1200}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)

	s.poll() // seed only — must not create a bucket / count traffic
	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 0 {
		t.Fatalf("expected 0 observed bytes after seed-only poll, got %d", detail.ObservedBytes)
	}

	s.poll() // delta = 1500-1000 = 500
	detail = s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 500 {
		t.Fatalf("expected 500 observed bytes after delta poll, got %d", detail.ObservedBytes)
	}
	if len(detail.TopTalkers) != 1 || detail.TopTalkers[0].IP != "192.168.1.50" || detail.TopTalkers[0].Bytes != 500 {
		t.Fatalf("unexpected top talkers: %+v", detail.TopTalkers)
	}
}

func TestTrafficStats_NegativeDeltaClamped(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 1000, BytesReply: 4000}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 1000, BytesReply: 4000}},
			// A brand-new flow reusing the same key with a smaller byte count
			// (would happen if a dead flow's key were reused) must clamp to 0,
			// never underflow, in BOTH directions independently.
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 20, BytesReply: 80}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // delta 0 (no change) — no bucket created
	s.poll() // delta would be negative — clamped to 0, no bucket created

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 0 {
		t.Fatalf("expected 0 observed bytes (all deltas clamped/zero), got %d", detail.ObservedBytes)
	}
}

func TestTrafficStats_RuleCounterResetDetection(t *testing.T) {
	acct := &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-1": {Bytes: 1000, Packets: 10}}, // seed
			{"rule-1": {Bytes: 1500, Packets: 15}}, // delta 500/5
			// nftables rebuilt the ruleset (ApplyRules) — counter restarted.
			{"rule-1": {Bytes: 200, Packets: 2}}, // reset: delta == cur (200/2), not negative
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll()
	s.poll()
	s.poll()

	detail := s.GetTrafficDetail("1h")
	if len(detail.TopRules) != 1 {
		t.Fatalf("expected 1 top rule, got %+v", detail.TopRules)
	}
	got := detail.TopRules[0]
	if got.Bytes != 700 || got.Packets != 7 {
		t.Fatalf("expected accumulated bytes=700 packets=7 (500+200 / 5+2), got bytes=%d packets=%d", got.Bytes, got.Packets)
	}
}

func TestTrafficStats_Categorization(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			// proto 6 (TCP) dstPort 443 matches the default "HTTPS" object
			// seeded by migrations (see newTestTrafficStatsService).
			{{Key: "https", SrcIP: "192.168.1.10", Proto: 6, DstPort: 443, BytesOrig: 0, BytesReply: 0}},
			{{Key: "https", SrcIP: "192.168.1.10", Proto: 6, DstPort: 443, BytesOrig: 150, BytesReply: 850}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)

	s.poll() // seed
	s.poll() // delta: https=1000

	detail := s.GetTrafficDetail("1h")
	if len(detail.Categories) != 1 || detail.Categories[0].Name != "HTTPS" || detail.Categories[0].Bytes != 1000 {
		t.Fatalf("expected a single HTTPS category with 1000 bytes, got %+v", detail.Categories)
	}
}

// TestTrafficStats_OnFlowEnd_CreditsOnlyDeltaAboveBaseline is plan T-07 case
// (ก): poll already credited 1000 bytes for a flow (baseline 0 -> 1000), then
// the conntrack DESTROY event reports the flow's final cumulative count as
// 1500. onFlowEnd must credit only the 500-byte difference — total observed
// across poll+event must be 1500, never 1000+1500=2500 (plan Caution 1, the
// core double-count regression this phase must not introduce).
func TestTrafficStats_OnFlowEnd_CreditsOnlyDeltaAboveBaseline(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 0, BytesReply: 0}},     // seed
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 200, BytesReply: 800}}, // delta 1000 (200/800)
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // delta 1000, baseline now (200,800)

	// plan T-08 case 1 (§5): poll saw (200,800) -> event reports the flow's
	// final cumulative count as (300,1200). onFlowEnd must credit only the
	// (100,400) difference — total observed across poll+event must be 1500,
	// never 1000+1500=2500.
	s.onFlowEnd(model.FlowSample{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 300, BytesReply: 1200})

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 1500 {
		t.Fatalf("expected combined poll+event observed bytes of 1500 (1000 from poll + 500 from event delta), got %d", detail.ObservedBytes)
	}
	if _, exists := s.flowState["f1"]; exists {
		t.Fatalf("expected flowState baseline for f1 to be deleted after onFlowEnd, so a later poll cannot double-credit it")
	}

	// plan T-08 case 1: up/down must reflect the final cumulative split, not
	// just the combined total (300 orig, 1200 reply).
	bd := s.GetTrafficBreakdown("1h")
	if got := bd.Hosts["192.168.1.50"]; got.Total() != 1500 || got.Orig != 300 || got.Reply != 1200 {
		t.Fatalf("expected host total=1500 orig=300 reply=1200, got %+v", got)
	}
}

// TestTrafficStats_OnFlowEnd_NoBaselineCreditsFull is plan T-07 case (ข): a
// flow that is born and dies entirely between two poll ticks has no baseline
// in flowState at all when its DESTROY event arrives — onFlowEnd must credit
// its full byte count in this case (this is exactly the gap this phase
// exists to close).
func TestTrafficStats_OnFlowEnd_NoBaselineCreditsFull(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	s.onFlowEnd(model.FlowSample{Key: "never-polled", SrcIP: "192.168.1.60", DstIP: "1.1.1.1", Proto: 17, DstPort: 53, BytesOrig: 300, BytesReply: 477})

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 777 {
		t.Fatalf("expected full 777 bytes credited for a flow never seen by poll, got %d", detail.ObservedBytes)
	}
	if len(detail.TopTalkers) != 1 || detail.TopTalkers[0].IP != "192.168.1.60" || detail.TopTalkers[0].Bytes != 777 {
		t.Fatalf("unexpected top talkers: %+v", detail.TopTalkers)
	}

	// plan T-08 case 2: event of a key poll never saw credits BOTH
	// directions in full, not just the combined total.
	bd := s.GetTrafficBreakdown("1h")
	if got := bd.Hosts["192.168.1.60"]; got.Orig != 300 || got.Reply != 477 {
		t.Fatalf("expected full-credit both directions orig=300 reply=477, got %+v", got)
	}
}

// TestTrafficStats_OnFlowEnd_AfterPruneDoesNotGoNegative is plan T-07 case
// (ค): a flow's baseline was already pruned by processFlows (flowStaleMisses
// consecutive polls without seeing it — a stale/expired conntrack entry)
// before its DESTROY event finally arrives. onFlowEnd must not underflow or
// otherwise go negative; it degrades to the same "no baseline" full-credit
// path as case (ข), which is always >= 0 by construction.
func TestTrafficStats_OnFlowEnd_AfterPruneDoesNotGoNegative(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f-prune", SrcIP: "192.168.1.70", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 100, BytesReply: 400}}, // seed
			{}, // miss 1
			{}, // miss 2
			{}, // miss 3 -> flowStaleMisses reached, baseline pruned
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	for i := 0; i < 4; i++ {
		s.poll()
	}
	if _, exists := s.flowState["f-prune"]; exists {
		t.Fatalf("expected f-prune baseline to already be pruned after %d consecutive misses", flowStaleMisses)
	}

	s.onFlowEnd(model.FlowSample{Key: "f-prune", SrcIP: "192.168.1.70", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 300, BytesReply: 600})

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 900 {
		t.Fatalf("expected onFlowEnd to credit the full 900 bytes (no baseline survives the prune) without going negative/underflowed, got observed=%d", detail.ObservedBytes)
	}

	// plan T-08 case 3: after a prune, neither direction may underflow —
	// both must simply be credited in full (no baseline to diff against).
	bd := s.GetTrafficBreakdown("1h")
	if got := bd.Hosts["192.168.1.70"]; got.Orig != 300 || got.Reply != 600 {
		t.Fatalf("expected full-credit both directions after prune orig=300 reply=600, got %+v", got)
	}
}

// TestTrafficStats_ProcessFlows_OneDirectionRegressesClampsIndependently is
// plan T-08 case 4: a flow whose orig counter regresses (key collision/NAT
// port reuse) while reply keeps growing normally must clamp ONLY the
// regressed direction to 0, not treat the whole flow as invalid — the reply
// direction's legitimate delta must still be counted (plan §5 Caution 3).
func TestTrafficStats_ProcessFlows_OneDirectionRegressesClampsIndependently(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.80", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 5000, BytesReply: 1000}},
			// orig regresses 5000 -> 1000 (must clamp to 0), reply grows
			// normally 1000 -> 1600 (must credit the full 600 delta).
			{{Key: "f1", SrcIP: "192.168.1.80", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 1000, BytesReply: 1600}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // orig delta clamped to 0, reply delta 600

	bd := s.GetTrafficBreakdown("1h")
	if bd.Observed != 600 {
		t.Fatalf("expected only the reply direction's 600-byte delta observed (orig clamped, not underflowed), got %d", bd.Observed)
	}
	if got := bd.Hosts["192.168.1.80"]; got.Orig != 0 || got.Reply != 600 {
		t.Fatalf("expected orig clamped to 0 and reply credited 600, got %+v", got)
	}
}

// TestCategorize_NoMatchFallsBackToOther exercises the categorizer directly
// (bypassing the DB-backed Service Objects cache) so it can assert the "no
// Service Object matches" -> "Other" fallback in isolation, and that the
// narrowest matching port range wins over a wider one.
func TestCategorize_NoMatchFallsBackToOther(t *testing.T) {
	s := &TrafficStatsService{}
	s.svcCache = []categoryEntry{
		{name: "ALL", protocols: []uint8{6, 17}, portStart: 1, portEnd: 65535},
		{name: "HTTPS", protocols: []uint8{6}, portStart: 443, portEnd: 443},
	}
	s.svcCachedAt = time.Now()

	if got := s.categorize(6, 443); got != "HTTPS" {
		t.Fatalf("expected narrowest match HTTPS, got %q", got)
	}
	if got := s.categorize(6, 8080); got != "ALL" {
		t.Fatalf("expected fallback to the wider ALL match, got %q", got)
	}
	if got := s.categorize(132, 0); got != "Other" {
		t.Fatalf("expected Other for a non-TCP/UDP proto with no ICMP object, got %q", got)
	}
}

// T-03 (matched-endpoints plan): ServiceNameFor wraps categorize but returns
// "" instead of "Other" for callers that want to fall back to raw PROTO/PORT
// display rather than a misleading generic label.
func TestServiceNameFor(t *testing.T) {
	s := &TrafficStatsService{}
	s.svcCache = []categoryEntry{
		{name: "ALL", protocols: []uint8{6, 17}, portStart: 1, portEnd: 65535},
		{name: "HTTPS", protocols: []uint8{6}, portStart: 443, portEnd: 443},
	}
	s.svcCachedAt = time.Now()

	if got := s.ServiceNameFor("TCP", "443"); got != "HTTPS" {
		t.Fatalf("expected HTTPS, got %q", got)
	}
	if got := s.ServiceNameFor("tcp", "8080"); got != "ALL" {
		t.Fatalf("expected ALL (case-insensitive proto), got %q", got)
	}
	if got := s.ServiceNameFor("UDP", "not-a-port"); got != "" {
		t.Fatalf("expected empty for unparsable port, got %q", got)
	}
	if got := s.ServiceNameFor("ICMP", "-"); got != "" {
		t.Fatalf("expected empty for no ICMP object match, got %q", got)
	}
	if got := s.ServiceNameFor("proto-132", "-"); got != "" {
		t.Fatalf("expected empty for unmatched other-protocol, got %q", got)
	}
}

// TestBuildCategoryEntries_MultiEntryServiceObjectSharesLabel covers T-08's
// acceptance for the Traffic page: a single Service Object with a TCP/80 and
// a TCP/443 entry must categorize BOTH ports under the same object name
// (docs/ref/todo/multi-value-address-service-objects-plan.md T-08).
func TestBuildCategoryEntries_MultiEntryServiceObjectSharesLabel(t *testing.T) {
	svcs := []model.ServiceObject{
		{
			Name: "Web",
			Entries: []model.ServiceEntry{
				{Protocol: "TCP", Port: "80"},
				{Protocol: "TCP", Port: "443"},
			},
		},
	}
	s := &TrafficStatsService{}
	s.svcCache = buildCategoryEntries(svcs)
	s.svcCachedAt = time.Now()

	if got := s.categorize(6, 80); got != "Web" {
		t.Fatalf("expected TCP/80 entry to categorize as Web, got %q", got)
	}
	if got := s.categorize(6, 443); got != "Web" {
		t.Fatalf("expected TCP/443 entry to categorize as Web, got %q", got)
	}
	if got := s.categorize(6, 8080); got != "Other" {
		t.Fatalf("expected no match outside either entry to fall back to Other, got %q", got)
	}
}

// TestBuildCategoryEntries_SingleEntryObjectUnchanged locks in that a plain
// single-entry Service Object still categorizes exactly as before multi-value
// support (docs/ref/todo/multi-value-address-service-objects-plan.md T-08
// acceptance: "object ค่าเดียวผลลัพธ์เหมือนเดิม").
func TestBuildCategoryEntries_SingleEntryObjectUnchanged(t *testing.T) {
	svcs := []model.ServiceObject{
		{Name: "SSH", Entries: []model.ServiceEntry{{Protocol: "TCP", Port: "22"}}},
	}
	s := &TrafficStatsService{}
	s.svcCache = buildCategoryEntries(svcs)
	s.svcCachedAt = time.Now()

	if got := s.categorize(6, 22); got != "SSH" {
		t.Fatalf("expected TCP/22 to categorize as SSH, got %q", got)
	}
	if got := s.categorize(6, 23); got != "Other" {
		t.Fatalf("expected TCP/23 to fall back to Other, got %q", got)
	}
}

// raceFakeAcct returns fresh, monotonically growing flow/rule data on every
// call — used to keep poll() genuinely mutating the newest (still-open)
// bucket's maps for the duration of the race test below, rather than
// stalling out after a couple of no-op ticks.
type raceFakeAcct struct {
	mu    sync.Mutex
	bytes uint64
}

func (a *raceFakeAcct) DumpFlows() ([]model.FlowSample, error) {
	a.mu.Lock()
	a.bytes += 1000
	b := a.bytes
	a.mu.Unlock()
	return []model.FlowSample{
		{Key: "race-flow", SrcIP: "192.168.1.77", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: b / 5, BytesReply: b - b/5},
	}, nil
}

func (a *raceFakeAcct) DumpRuleCounters() (map[string]model.RuleCounter, error) {
	a.mu.Lock()
	b := a.bytes
	a.mu.Unlock()
	return map[string]model.RuleCounter{"race-rule": {Bytes: b, Packets: b / 100}}, nil
}

func (a *raceFakeAcct) WatchFlowEnd(ctx context.Context, cb func(model.FlowSample)) error {
	<-ctx.Done()
	return nil
}

// TestTrafficStats_GetTrafficDetailNoRaceWithPoll drives poll() (the
// background collector goroutine, normally ticked every flowPollInterval),
// onFlowEnd() (the conntrack DESTROY event callback, normally invoked from
// the WatchFlowEnd goroutine started by StartFlowEndWatcher) and
// GetTrafficDetail() (the HTTP handler path) concurrently and many times
// over, to catch a concurrent map read/write between any pair of them (plan
// T-07 case ง). Run with `go test -race` — this test alone proves nothing
// under the normal (non -race) test runner; the point is the race detector's
// instrumentation.
func TestTrafficStats_GetTrafficDetailNoRaceWithPoll(t *testing.T) {
	s := newTestTrafficStatsService(t, nil, nil)
	s.acct = &raceFakeAcct{}

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.poll()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.onFlowEnd(model.FlowSample{
				Key:        fmt.Sprintf("race-event-%d", i),
				SrcIP:      "192.168.1.99",
				DstIP:      "1.1.1.1",
				Proto:      17,
				DstPort:    53,
				BytesOrig:  uint64(i + 1),
				BytesReply: uint64(i + 1),
			})
		}
	}()
	go func() {
		defer wg.Done()
		// Exercise all 7 supported windows (docs/ref/todo/
		// statistics-window-granularity-plan.md T-07 item 7), not just 1h/24h,
		// so a race introduced by the new bucket-selection path would still be
		// caught here.
		for i := 0; i < iterations; i++ {
			for w := range statsWindowBuckets {
				_ = s.GetTrafficDetail(w)
			}
		}
	}()
	wg.Wait()
}

// TestTrafficStats_ProcessFlows_CapsTrackedFlows feeds more than
// maxTrackedFlows distinct flow keys into processFlows directly and asserts
// the flow-baseline map never grows past the cap (plan §5 Caution 5) — a
// port-scan/P2P burst must not let this map grow unbounded.
func TestTrafficStats_ProcessFlows_CapsTrackedFlows(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	flows := make([]model.FlowSample, maxTrackedFlows+500)
	for i := range flows {
		flows[i] = model.FlowSample{
			Key:        fmt.Sprintf("flow-%d", i),
			SrcIP:      "10.0.0.1",
			DstIP:      "1.1.1.1",
			Proto:      6,
			DstPort:    443,
			BytesOrig:  uint64(i),
			BytesReply: uint64(i) * 2,
		}
	}

	hostDeltas := make(map[string]dirBytes)
	catDeltas := make(map[string]uint64)
	dstDeltas := make(map[string]dirBytes)
	convDeltas := make(map[string]dirBytes)
	s.processFlows(flows, hostDeltas, catDeltas, dstDeltas, convDeltas) // seed poll — still populates flowState

	if len(s.flowState) > maxTrackedFlows {
		t.Fatalf("expected flowState capped at %d entries, got %d", maxTrackedFlows, len(s.flowState))
	}
}

func TestMergeUint64Map_CapsNewKeys(t *testing.T) {
	dst := map[string]uint64{"a": 1, "b": 2}
	src := map[string]uint64{"a": 10, "c": 100, "d": 100}
	mergeUint64Map(dst, src, 2) // cap at 2 distinct keys — "a" (existing) may grow, "c"/"d" (new) must not both fit
	if dst["a"] != 11 {
		t.Fatalf("expected existing key 'a' to keep accumulating, got %d", dst["a"])
	}
	if len(dst) > 2 {
		t.Fatalf("expected map capped at 2 entries, got %d: %+v", len(dst), dst)
	}
}

// --- Statistics page (docs/ref/todo/statistics-page-plan.md T-04) ---

// TestGetTrafficBreakdown_DstAndConvDeltas is plan T-04 case 1: polling twice
// with the same flow must produce a *delta* (not cumulative) figure for both
// Dests and Convs, and the sum of Hosts/Dests/Convs for a single-flow window
// must each equal Observed (only one flow contributed).
func TestGetTrafficBreakdown_DstAndConvDeltas(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 200, BytesReply: 800}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53, BytesOrig: 300, BytesReply: 1200}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)

	s.poll() // seed only
	s.poll() // delta = 500 (orig 100 / reply 400)

	bd := s.GetTrafficBreakdown("1h")
	if bd.Observed != 500 {
		t.Fatalf("expected observed=500, got %d", bd.Observed)
	}
	if got := bd.Hosts["192.168.1.50"]; got.Total() != 500 || got.Orig != 100 || got.Reply != 400 {
		t.Fatalf("expected host delta total=500 orig=100 reply=400, got %+v", got)
	}
	if got := bd.Dests["8.8.8.8"]; got.Total() != 500 || got.Orig != 100 || got.Reply != 400 {
		t.Fatalf("expected dest delta total=500 orig=100 reply=400, got %+v", got)
	}
	wantKey := convKey(model.FlowSample{SrcIP: "192.168.1.50", DstIP: "8.8.8.8", Proto: 17, DstPort: 53})
	if got := bd.Convs[wantKey]; got.Total() != 500 || got.Orig != 100 || got.Reply != 400 {
		t.Fatalf("expected conversation delta total=500 orig=100 reply=400 for key %q, got %+v", wantKey, got)
	}
}

// TestGetTrafficBreakdown_OnFlowEndDoesNotDoubleCountDstConv is plan T-04
// case 2: once poll() has already credited a flow, its DESTROY event must
// only credit dst/conv for the additional bytes above the poll baseline —
// same invariant onFlowEnd already guarantees for Hosts (plan Caution 2).
func TestGetTrafficBreakdown_OnFlowEndDoesNotDoubleCountDstConv(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 0, BytesReply: 0}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 200, BytesReply: 800}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // delta 1000, baseline now (200,800)

	s.onFlowEnd(model.FlowSample{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 300, BytesReply: 1200})

	bd := s.GetTrafficBreakdown("1h")
	if bd.Observed != 1500 {
		t.Fatalf("expected observed=1500 (no double count), got %d", bd.Observed)
	}
	if got := bd.Dests["1.1.1.1"]; got.Total() != 1500 {
		t.Fatalf("expected dest total 1500 (no double count), got %+v", got)
	}
	wantKey := convKey(model.FlowSample{SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443})
	if got := bd.Convs[wantKey]; got.Total() != 1500 {
		t.Fatalf("expected conversation total 1500 (no double count), got %+v", got)
	}
}

// TestGetTrafficBreakdown_OnFlowEndNoBaselineCreditsDstConvInFull is plan
// T-04 case 3: a flow that is born and dies entirely between two poll ticks
// (no baseline ever seen) must still show up in Dests/Convs, not just Hosts.
func TestGetTrafficBreakdown_OnFlowEndNoBaselineCreditsDstConvInFull(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	s.onFlowEnd(model.FlowSample{Key: "never-polled", SrcIP: "192.168.1.60", DstIP: "9.9.9.9", Proto: 17, DstPort: 53, BytesOrig: 333, BytesReply: 444})

	bd := s.GetTrafficBreakdown("1h")
	if got := bd.Hosts["192.168.1.60"]; got.Total() != 777 {
		t.Fatalf("expected host credited in full, got %+v", got)
	}
	if got := bd.Dests["9.9.9.9"]; got.Total() != 777 {
		t.Fatalf("expected dest credited in full, got %+v", got)
	}
	wantKey := convKey(model.FlowSample{SrcIP: "192.168.1.60", DstIP: "9.9.9.9", Proto: 17, DstPort: 53})
	if got := bd.Convs[wantKey]; got.Total() != 777 {
		t.Fatalf("expected conversation credited in full for key %q, got %+v", wantKey, got)
	}
}

// TestGetTrafficBreakdown_CapsConversationsAndReportsTruncated is plan T-04
// case 4: a burst of far more than maxTrackedConversations distinct
// conversations must not panic, must cap each bucket's convBytes map at
// maxTrackedConversations, and must surface Truncated=true.
func TestGetTrafficBreakdown_CapsConversationsAndReportsTruncated(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	n := s.maxTrackedConversations + 1000
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
	hostDeltas := make(map[string]dirBytes)
	catDeltas := make(map[string]uint64)
	dstDeltas := make(map[string]dirBytes)
	convDeltas := make(map[string]dirBytes)
	// This must not panic even though every key is brand new (seed poll).
	s.processFlows(flows, hostDeltas, catDeltas, dstDeltas, convDeltas)
	s.addBucket(time.Now(), hostDeltas, catDeltas, nil, dstDeltas, convDeltas, dirBytes{})

	// A second pass with nonzero deltas so the caps are exercised on the
	// live path (seed rounds never populate deltas).
	for i := range flows {
		flows[i].BytesOrig = 20
		flows[i].BytesReply = 80
	}
	hostDeltas = make(map[string]dirBytes)
	catDeltas = make(map[string]uint64)
	dstDeltas = make(map[string]dirBytes)
	convDeltas = make(map[string]dirBytes)
	s.processFlows(flows, hostDeltas, catDeltas, dstDeltas, convDeltas)
	s.addBucket(time.Now(), hostDeltas, catDeltas, nil, dstDeltas, convDeltas, dirBytes{})

	if len(convDeltas) > s.maxTrackedConversations {
		t.Fatalf("expected convDeltas capped at %d, got %d", s.maxTrackedConversations, len(convDeltas))
	}

	bd := s.GetTrafficBreakdown("1h")
	if !bd.Truncated {
		t.Fatalf("expected Truncated=true once a bucket hits the conversation cap")
	}
}

// --- Bandwidth series (docs/ref/todo/statistics-overview-bandwidth-chart-plan.md T-04) ---

func seriesSum(series []model.BandwidthPoint) (bytes, up, down uint64) {
	for _, p := range series {
		bytes += p.Bytes
		up += p.BytesUp
		down += p.BytesDown
	}
	return
}

// TestBandwidthSeries_SumEqualsObserved is plan T-04 case 1: for both windows,
// sum(Series[].Bytes) must equal Observed exactly — computed in the same
// RLock/loop by construction (plan §2.5), not coincidentally.
func TestBandwidthSeries_SumEqualsObserved(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	now := time.Now()
	hostDeltas := map[string]dirBytes{"192.168.1.10": {Orig: 100, Reply: 400}}
	catDeltas := map[string]uint64{"Other": 500}
	dstDeltas := map[string]dirBytes{"1.1.1.1": {Orig: 100, Reply: 400}}
	convDeltas := map[string]dirBytes{"k1": {Orig: 100, Reply: 400}}
	// addBucket assumes callers append in chronological (oldest -> newest)
	// order, same as poll()/onFlowEnd in production — insert oldest first.
	for i := 4; i >= 0; i-- {
		s.addBucket(now.Add(-time.Duration(i)*trafficDetailBucketSpan), hostDeltas, catDeltas, nil, dstDeltas, convDeltas, dirBytes{Orig: 100, Reply: 400})
	}

	for _, window := range []string{"1h", "24h"} {
		bd := s.GetTrafficBreakdown(window)
		bytes, _, _ := seriesSum(bd.Series)
		if bytes != bd.Observed {
			t.Fatalf("window %s: sum(series.bytes)=%d != observed=%d", window, bytes, bd.Observed)
		}
	}
}

// TestBandwidthSeries_UpPlusDownEqualsBytes is plan T-04 case 2.
func TestBandwidthSeries_UpPlusDownEqualsBytes(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	now := time.Now()
	s.addBucket(now.Add(-5*trafficDetailBucketSpan), nil, nil, nil, nil, nil, dirBytes{Orig: 33, Reply: 44})
	s.addBucket(now, nil, nil, nil, nil, nil, dirBytes{Orig: 111, Reply: 222})

	for _, window := range []string{"1h", "24h"} {
		bd := s.GetTrafficBreakdown(window)
		for i, p := range bd.Series {
			if p.BytesUp+p.BytesDown != p.Bytes {
				t.Fatalf("window %s point %d: bytesUp(%d)+bytesDown(%d) != bytes(%d)", window, i, p.BytesUp, p.BytesDown, p.Bytes)
			}
		}
	}
}

// TestBandwidthSeries_FixedLengthAndSpacing is plan T-04 case 3: series always
// has a fixed length (12 for 1h, 288 for 24h), even with an empty ring, sorted
// oldest -> newest with no duplicate/missing 5-minute steps.
func TestBandwidthSeries_FixedLengthAndSpacing(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	cases := []struct {
		window string
		want   int
	}{{"1h", trafficWindow1hBuckets}, {"24h", trafficDetailBucketMax}}
	for _, c := range cases {
		bd := s.GetTrafficBreakdown(c.window)
		if len(bd.Series) != c.want {
			t.Fatalf("window %s: expected %d points, got %d", c.window, c.want, len(bd.Series))
		}
		seen := make(map[string]bool, len(bd.Series))
		var prev time.Time
		for i, p := range bd.Series {
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
			if p.Bytes != 0 || p.BytesUp != 0 || p.BytesDown != 0 {
				t.Fatalf("window %s point %d: expected zero-valued point on an empty ring, got %+v", c.window, i, p)
			}
		}
	}
}

// TestBandwidthSeries_ZeroFillMidGap is plan T-04 case 4: two buckets 20
// minutes apart must leave the buckets strictly between them at zero, not
// missing or shifted.
func TestBandwidthSeries_ZeroFillMidGap(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	now := time.Now().Truncate(trafficDetailBucketSpan)
	s.addBucket(now.Add(-4*trafficDetailBucketSpan), nil, nil, nil, nil, nil, dirBytes{Orig: 0, Reply: 2000})
	s.addBucket(now, nil, nil, nil, nil, nil, dirBytes{Orig: 1000, Reply: 0})

	bd := s.GetTrafficBreakdown("1h")
	n := len(bd.Series)
	// Newest point (index n-1) corresponds to "now"; index n-5 to "now-4*span".
	if got := bd.Series[n-1].BytesUp; got != 1000 {
		t.Fatalf("expected newest point up=1000, got %d", got)
	}
	if got := bd.Series[n-5].BytesDown; got != 2000 {
		t.Fatalf("expected point at now-4*span down=2000, got %d", got)
	}
	for i := n - 4; i < n-1; i++ {
		p := bd.Series[i]
		if p.Bytes != 0 || p.BytesUp != 0 || p.BytesDown != 0 {
			t.Fatalf("expected zero-filled gap at index %d, got %+v", i, p)
		}
	}
}

// TestBandwidthSeries_LanRelativeDirection is plan T-04 case 5: a flow from a
// public source to a private destination must count as "down" (entering the
// LAN), never "up" — tested through both the poll()/processFlows path (has a
// flowSampleState baseline) and the onFlowEnd path (no baseline, plan §2.2).
func TestBandwidthSeries_LanRelativeDirection(t *testing.T) {
	t.Run("via poll", func(t *testing.T) {
		acct := &fakeTrafficAccounting{
			flowResponses: [][]model.FlowSample{
				{{Key: "f1", SrcIP: "8.8.8.8", DstIP: "192.168.1.50", Proto: 17, DstPort: 53, BytesOrig: 0, BytesReply: 0}},
				{{Key: "f1", SrcIP: "8.8.8.8", DstIP: "192.168.1.50", Proto: 17, DstPort: 53, BytesOrig: 900, BytesReply: 0}},
			},
		}
		s := newTestTrafficStatsService(t, acct, nil)
		s.poll() // seed
		s.poll() // delta: Orig=900 (public -> private)

		bd := s.GetTrafficBreakdown("1h")
		_, up, down := seriesSum(bd.Series)
		if up != 0 || down != 900 {
			t.Fatalf("expected public->private Orig delta to land entirely in down, got up=%d down=%d", up, down)
		}
	})

	t.Run("via onFlowEnd no baseline", func(t *testing.T) {
		s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
		s.onFlowEnd(model.FlowSample{Key: "f2", SrcIP: "8.8.8.8", DstIP: "192.168.1.50", Proto: 17, DstPort: 53, BytesOrig: 700, BytesReply: 0})

		bd := s.GetTrafficBreakdown("1h")
		_, up, down := seriesSum(bd.Series)
		if up != 0 || down != 700 {
			t.Fatalf("expected public->private onFlowEnd delta to land entirely in down, got up=%d down=%d", up, down)
		}
	})
}

// TestBandwidthSeries_TrafficDetailAndBreakdownUnchanged is plan T-04 case 6
// (regression guard for T-02): GetTrafficDetail/GetTrafficBreakdown's existing
// fields must be byte-for-byte identical to pre-T-02 behavior for a mixed set
// of LAN-relative directions — only the new Series field is additive.
func TestBandwidthSeries_TrafficDetailAndBreakdownUnchanged(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{
				{Key: "lan-out", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 0, BytesReply: 0},
				{Key: "wan-in", SrcIP: "8.8.8.8", DstIP: "192.168.1.60", Proto: 17, DstPort: 53, BytesOrig: 0, BytesReply: 0},
			},
			{
				{Key: "lan-out", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 200, BytesReply: 800},
				{Key: "wan-in", SrcIP: "8.8.8.8", DstIP: "192.168.1.60", Proto: 17, DstPort: 53, BytesOrig: 900, BytesReply: 0},
			},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // deltas: lan-out 1000, wan-in 900

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 1900 {
		t.Fatalf("expected ObservedBytes=1900, got %d", detail.ObservedBytes)
	}

	bd := s.GetTrafficBreakdown("1h")
	if bd.Observed != 1900 {
		t.Fatalf("expected Observed=1900, got %d", bd.Observed)
	}
	if got := bd.Hosts["192.168.1.50"]; got.Total() != 1000 || got.Orig != 200 || got.Reply != 800 {
		t.Fatalf("expected lan-out host delta total=1000 orig=200 reply=800 (flow-relative, unaffected by LAN-relative Observed flip), got %+v", got)
	}
	if got := bd.Hosts["8.8.8.8"]; got.Total() != 900 || got.Orig != 900 || got.Reply != 0 {
		t.Fatalf("expected wan-in host delta total=900 orig=900 reply=0 (flow-relative), got %+v", got)
	}
}

// TestBandwidthSeries_CarryToEdgeWhenRingWiderThanWindow is plan T-04 case 7
// (§2.5/§7 item 6 — locked decision): a bucket older than the series axis
// must still be counted in Observed and carried into series[0], not dropped.
func TestBandwidthSeries_CarryToEdgeWhenRingWiderThanWindow(t *testing.T) {
	t.Run("24h", func(t *testing.T) {
		s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
		now := time.Now()
		s.addBucket(now.Add(-30*time.Hour), nil, nil, nil, nil, nil, dirBytes{Orig: 555, Reply: 111})
		s.addBucket(now, nil, nil, nil, nil, nil, dirBytes{Orig: 10, Reply: 20})

		bd := s.GetTrafficBreakdown("24h")
		bytes, _, _ := seriesSum(bd.Series)
		if bytes != bd.Observed {
			t.Fatalf("expected sum(series.bytes)=%d to equal observed=%d even with an out-of-axis bucket", bytes, bd.Observed)
		}
		if bd.Series[0].Bytes < 555+111 {
			t.Fatalf("expected the out-of-axis 30h-old bucket to be carried into series[0], got %+v", bd.Series[0])
		}
	})

	t.Run("1h", func(t *testing.T) {
		s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
		now := time.Now()
		s.addBucket(now.Add(-3*time.Hour), nil, nil, nil, nil, nil, dirBytes{Orig: 77, Reply: 33})
		s.addBucket(now, nil, nil, nil, nil, nil, dirBytes{Orig: 5, Reply: 5})

		bd := s.GetTrafficBreakdown("1h")
		bytes, _, _ := seriesSum(bd.Series)
		if bytes != bd.Observed {
			t.Fatalf("expected sum(series.bytes)=%d to equal observed=%d even with an out-of-axis bucket", bytes, bd.Observed)
		}
		if bd.Series[0].Bytes < 77+33 {
			t.Fatalf("expected the out-of-axis 3h-old bucket to be carried into series[0], got %+v", bd.Series[0])
		}
	})
}

// TestBandwidthSeries_1hIsSubsetOf24h is plan T-04 case 8: for the same
// underlying ring, the 1h window's totals must never exceed the 24h window's
// (1h buckets are always a trailing subset of the 24h buckets — the ring is
// append-only, so this holds structurally, not just numerically).
func TestBandwidthSeries_1hIsSubsetOf24h(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	now := time.Now()
	for i := 39; i >= 0; i-- {
		s.addBucket(now.Add(-time.Duration(i)*trafficDetailBucketSpan), nil, nil, nil, nil, nil, dirBytes{Orig: uint64(i + 1), Reply: uint64(i + 1)})
	}

	bd1h := s.GetTrafficBreakdown("1h")
	bd24h := s.GetTrafficBreakdown("24h")
	if bd1h.Observed > bd24h.Observed {
		t.Fatalf("expected observed(1h)=%d <= observed(24h)=%d", bd1h.Observed, bd24h.Observed)
	}
	sum1h, _, _ := seriesSum(bd1h.Series)
	sum24h, _, _ := seriesSum(bd24h.Series)
	if sum1h > sum24h {
		t.Fatalf("expected sum(series(1h))=%d <= sum(series(24h))=%d", sum1h, sum24h)
	}
}

// TestTrafficStats_CurrentRates_MatchesElapsed is plan T-06 item 1: rotating
// the accumulator over a known elapsed duration must produce
// bps == delta_bytes*8/elapsed_seconds. lastRotateAt is set directly (same
// package as TrafficStatsService) rather than sleeping the test, so the
// expected elapsed is controlled precisely; the actual elapsed the code used
// is then read back from s.lastRateElapsed to compute the expected bps,
// tolerating only the code's own float64->uint64 truncation, not test flake.
func TestTrafficStats_CurrentRates_MatchesElapsed(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 1000, BytesReply: 4000}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 2000, BytesReply: 9000}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed

	s.lastRotateAt = time.Now().Add(-10 * time.Second)
	s.poll() // delta orig=1000, reply=5000

	elapsed := s.lastRateElapsed.Seconds()
	if elapsed <= 0 {
		t.Fatalf("expected positive elapsed, got %v", s.lastRateElapsed)
	}
	rates := s.CurrentRates()
	r, ok := rates.Hosts["192.168.1.50"]
	if !ok {
		t.Fatalf("expected rate entry for 192.168.1.50, got %+v", rates.Hosts)
	}
	wantUp := uint64(float64(1000) * 8 / elapsed)
	wantDown := uint64(float64(5000) * 8 / elapsed)
	if r.UpBps != wantUp || r.DownBps != wantDown {
		t.Fatalf("rate mismatch: got up=%d down=%d, want up=%d down=%d (elapsed=%v)", r.UpBps, r.DownBps, wantUp, wantDown, elapsed)
	}
	// Forcing elapsed to ~10s on a 1000/5000-byte delta must land in a
	// plausible Kbps range — an 8x-off value here would mean a bit/byte
	// mixup (plan Caution 1), not just a rounding difference.
	if wantUp < 100 || wantUp > 2000 || wantDown < 1000 || wantDown > 10000 {
		t.Fatalf("computed rate outside plausible range: up=%d down=%d", wantUp, wantDown)
	}
}

// TestTrafficStats_CurrentRates_DecaysToZeroOnQuietTick is plan T-06 item 2 —
// the single most important case of this whole feature (plan Caution 3): a
// tick with zero byte delta must rotate the rate back down instead of leaving
// the previous tick's speed stuck forever, which would happen if rotateRates
// ran after poll()'s early-return for a silent tick.
func TestTrafficStats_CurrentRates_DecaysToZeroOnQuietTick(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 1000, BytesReply: 4000}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 2000, BytesReply: 9000}},
			// identical byte counts -> delta 0 -> genuinely quiet tick
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, BytesOrig: 2000, BytesReply: 9000}},
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // delta -> rate becomes non-zero

	rates := s.CurrentRates()
	if r, ok := rates.Hosts["192.168.1.50"]; !ok || (r.UpBps == 0 && r.DownBps == 0) {
		t.Fatalf("expected a non-zero rate before the quiet tick, got %+v (ok=%v)", rates.Hosts["192.168.1.50"], ok)
	}

	s.poll() // quiet tick: no byte delta at all

	rates = s.CurrentRates()
	if r, ok := rates.Hosts["192.168.1.50"]; ok && (r.UpBps != 0 || r.DownBps != 0) {
		t.Fatalf("expected rate to decay to 0 after a quiet tick (accumulator must rotate before poll()'s early-return), got %+v", r)
	}
}

// TestTrafficStats_CurrentRates_IncludesOnFlowEndDelta is plan T-06 item 3: a
// flow reported only via onFlowEnd (never seen by poll()'s own DumpFlows)
// must still be visible through CurrentRates() — otherwise a flow that is
// born and dies entirely between two poll ticks would silently vanish from
// the "current speed" view even though it already counts toward the bucket
// ring's byte totals.
func TestTrafficStats_CurrentRates_IncludesOnFlowEndDelta(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	s.poll() // establishes an initial lastRotateAt

	s.lastRotateAt = time.Now().Add(-5 * time.Second)
	s.onFlowEnd(model.FlowSample{
		Key: "flow-end-1", SrcIP: "192.168.1.60", DstIP: "8.8.8.8", Proto: 17, DstPort: 53,
		BytesOrig: 100, BytesReply: 300,
	})
	// onFlowEnd itself never rotates (only poll() does) — a subsequent quiet
	// poll() tick is what picks up the accumulator's onFlowEnd delta into
	// lastRate.
	s.poll()

	elapsed := s.lastRateElapsed.Seconds()
	if elapsed <= 0 {
		t.Fatalf("expected positive elapsed, got %v", s.lastRateElapsed)
	}
	rates := s.CurrentRates()
	r, ok := rates.Hosts["192.168.1.60"]
	if !ok {
		t.Fatalf("expected onFlowEnd's delta to be visible via CurrentRates, got %+v", rates.Hosts)
	}
	wantUp := uint64(float64(100) * 8 / elapsed)
	wantDown := uint64(float64(300) * 8 / elapsed)
	if r.UpBps != wantUp || r.DownBps != wantDown {
		t.Fatalf("onFlowEnd rate mismatch: got up=%d down=%d want up=%d down=%d", r.UpBps, r.DownBps, wantUp, wantDown)
	}
}

// TestTrafficStats_CurrentRatesNoRaceWithPoll is plan T-06 item 5 — same
// pattern as TestTrafficStats_GetTrafficDetailNoRaceWithPoll above, but
// exercising CurrentRates() concurrently with poll()/onFlowEnd() instead of
// GetTrafficDetail(), to catch the exact class of bug (returning an internal
// map that the poller goroutine keeps mutating) the CurrentRates doc comment
// warns about. Run with `go test -race`.
func TestTrafficStats_CurrentRatesNoRaceWithPoll(t *testing.T) {
	s := newTestTrafficStatsService(t, nil, nil)
	s.acct = &raceFakeAcct{}

	const iterations = 300
	var wg sync.WaitGroup
	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.poll()
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			s.onFlowEnd(model.FlowSample{
				Key:        fmt.Sprintf("rate-race-event-%d", i),
				SrcIP:      "192.168.1.98",
				DstIP:      "1.1.1.1",
				Proto:      17,
				DstPort:    53,
				BytesOrig:  uint64(i + 1),
				BytesReply: uint64(i + 1),
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.CurrentRates()
		}
	}()
	wg.Wait()
}

// TestNormalizeStatsWindow_AllSevenAndFallback is docs/ref/todo/
// statistics-window-granularity-plan.md T-07 item 6 at the normalizeStatsWindow
// level: all 7 canonical values pass through unchanged; anything else
// (including empty, garbage, and — critically — the UPPERCASE button labels
// like "1H"/"24H"/"15M" and a value with surrounding whitespace) falls back to
// "1h". normalizeStatsWindow must NOT be case-insensitive (plan §0 D-4/§6
// item 3) — a frontend bug that sends the label instead of the value must be
// caught by this test, not silently masked by a toLowerCase() here.
func TestNormalizeStatsWindow_AllSevenAndFallback(t *testing.T) {
	for w := range statsWindowBuckets {
		if got := normalizeStatsWindow(w); got != w {
			t.Errorf("normalizeStatsWindow(%q) = %q, want unchanged", w, got)
		}
	}

	fallbackCases := []string{
		"", "evil", "99h", "2h", "1H", "24H", "15M", " 1h", "../etc/passwd",
	}
	for _, w := range fallbackCases {
		if got := normalizeStatsWindow(w); got != trafficWindow1h {
			t.Errorf("normalizeStatsWindow(%q) = %q, want %q", w, got, trafficWindow1h)
		}
	}
}

// TestStatsWindowBucketCount_AllSeven is T-07 item 1's helper-level check:
// statsWindowBucketCount must return exactly the table in statsWindowBuckets
// for all 7 windows.
func TestStatsWindowBucketCount_AllSeven(t *testing.T) {
	want := map[string]int{
		"15m": 3, "30m": 6, "1h": 12, "3h": 36, "6h": 72, "12h": 144, "24h": 288,
	}
	for w, n := range want {
		if got := statsWindowBucketCount(w); got != n {
			t.Errorf("statsWindowBucketCount(%q) = %d, want %d", w, got, n)
		}
	}
}

// TestLastNBuckets_NoPanicWhenNExceedsLen is T-07 item 4's helper-level check:
// lastNBuckets must never panic when n > len(ring), returning the whole ring
// instead.
func TestLastNBuckets_NoPanicWhenNExceedsLen(t *testing.T) {
	ring := []int{1, 2, 3}
	got := lastNBuckets(ring, 288)
	if len(got) != 3 || got[0] != 1 || got[2] != 3 {
		t.Fatalf("lastNBuckets(ring of 3, 288) = %v, want the whole ring unchanged", got)
	}
	empty := lastNBuckets([]int{}, 288)
	if len(empty) != 0 {
		t.Fatalf("lastNBuckets(empty ring, 288) = %v, want empty", empty)
	}
}

// TestBandwidthSeries_AllSevenWindows_FixedLength is T-07 item 1: series/
// breakdown length across all 7 windows, from GetTrafficBreakdown, matches
// statsWindowBuckets exactly, even on an empty ring.
func TestBandwidthSeries_AllSevenWindows_FixedLength(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	cases := []struct {
		window string
		want   int
	}{
		{"15m", 3}, {"30m", 6}, {"1h", 12}, {"3h", 36}, {"6h", 72}, {"12h", 144}, {"24h", 288},
	}
	for _, c := range cases {
		bd := s.GetTrafficBreakdown(c.window)
		if len(bd.Series) != c.want {
			t.Fatalf("window %s: expected %d points, got %d", c.window, c.want, len(bd.Series))
		}
	}
}

// TestBandwidthSeries_AllSevenWindows_SumEqualsObserved is T-07 items 1/3: a
// full 288-bucket ring, checked at all 7 windows — the sum invariant must
// hold everywhere, and a short window (e.g. 15m -> 3 trailing buckets) must
// only include ITS OWN trailing buckets, not the whole ring carried into the
// first point (plan §6 item 1/D-1).
func TestBandwidthSeries_AllSevenWindows_SumEqualsObserved(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	now := time.Now()
	for i := trafficDetailBucketMax - 1; i >= 0; i-- {
		s.addBucket(now.Add(-time.Duration(i)*trafficDetailBucketSpan), nil, nil, nil, nil, nil, dirBytes{Orig: 1, Reply: 1})
	}

	for window, n := range statsWindowBuckets {
		bd := s.GetTrafficBreakdown(window)
		bytes, _, _ := seriesSum(bd.Series)
		if bytes != bd.Observed {
			t.Fatalf("window %s: sum(series.bytes)=%d != observed=%d", window, bytes, bd.Observed)
		}
		if bd.Observed != uint64(n*2) {
			t.Fatalf("window %s: expected observed=%d (only the last %d of 288 buckets, 2 bytes each), got %d — a short window must not pull in the whole ring", window, n*2, n, bd.Observed)
		}
	}
}

// TestBandwidthSeries_RingSmallerThanWindow_ZeroFillsNoPanic is T-07 item 4:
// a freshly-booted service (ring has only 2 buckets) asked for the widest
// window (24h, 288 buckets) must not panic and must return a zero-filled,
// fixed-length series.
func TestBandwidthSeries_RingSmallerThanWindow_ZeroFillsNoPanic(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	now := time.Now()
	s.addBucket(now.Add(-trafficDetailBucketSpan), nil, nil, nil, nil, nil, dirBytes{Orig: 5, Reply: 5})
	s.addBucket(now, nil, nil, nil, nil, nil, dirBytes{Orig: 5, Reply: 5})

	bd := s.GetTrafficBreakdown("24h")
	if len(bd.Series) != 288 {
		t.Fatalf("expected series length 288 even with only 2 buckets in the ring, got %d", len(bd.Series))
	}
	if bd.Observed != 20 {
		t.Fatalf("expected observed=20, got %d", bd.Observed)
	}
	bytes, _, _ := seriesSum(bd.Series)
	if bytes != bd.Observed {
		t.Fatalf("sum(series.bytes)=%d != observed=%d", bytes, bd.Observed)
	}
}

// TestGetTrafficBreakdownForDests_SumEqualsDestTotals is plan
// statistics-dns-page-revamp-plan.md T-03: for a dstSet of 2 IPs (one
// tracked, one not), DestSeries must be fixed-length/zero-filled per window
// and sum(DestSeries[].Bytes) must equal DestTotals.Total() exactly, at more
// than one window value.
func TestGetTrafficBreakdownForDests_SumEqualsDestTotals(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)
	now := time.Now()
	for i := trafficDetailBucketMax - 1; i >= 0; i-- {
		ts := now.Add(-time.Duration(i) * trafficDetailBucketSpan)
		dstDeltas := map[string]dirBytes{
			"8.8.8.8": {Orig: 3, Reply: 7},     // in dstSet
			"1.1.1.1": {Orig: 100, Reply: 100}, // NOT in dstSet — must be excluded
		}
		s.addBucket(ts, nil, nil, nil, dstDeltas, nil, dirBytes{Orig: 103, Reply: 107})
	}

	dstSet := []string{"8.8.8.8", "9.9.9.9"} // 9.9.9.9 never appears — must not error or add bytes

	for _, window := range []string{"15m", "24h"} {
		n := statsWindowBucketCount(window)
		bd := s.GetTrafficBreakdownForDests(window, dstSet)

		if len(bd.DestSeries) != n {
			t.Fatalf("window %s: expected DestSeries length %d, got %d", window, n, len(bd.DestSeries))
		}

		bytes, up, down := seriesSum(bd.DestSeries)
		if bytes != bd.DestTotals.Total() {
			t.Fatalf("window %s: sum(DestSeries.bytes)=%d != DestTotals.Total()=%d", window, bytes, bd.DestTotals.Total())
		}
		if up != bd.DestTotals.Orig || down != bd.DestTotals.Reply {
			t.Fatalf("window %s: DestSeries up/down (%d/%d) != DestTotals.Orig/Reply (%d/%d)", window, up, down, bd.DestTotals.Orig, bd.DestTotals.Reply)
		}

		wantTotal := uint64(n) * 10 // only 8.8.8.8 (3+7=10) counted per bucket, 1.1.1.1 excluded
		if bd.DestTotals.Total() != wantTotal {
			t.Fatalf("window %s: expected DestTotals.Total()=%d (only dstSet IPs), got %d", window, wantTotal, bd.DestTotals.Total())
		}
	}

	// Empty/nil dstIPs must behave exactly like GetTrafficBreakdown: no
	// DestSeries, zero DestTotals (plan T-03 "nil = zero extra cost").
	bd := s.GetTrafficBreakdownForDests("1h", nil)
	if bd.DestSeries != nil {
		t.Fatalf("expected nil DestSeries for empty dstIPs, got %+v", bd.DestSeries)
	}
	if bd.DestTotals.Total() != 0 {
		t.Fatalf("expected zero DestTotals for empty dstIPs, got %+v", bd.DestTotals)
	}
}

// TestTrafficStats_PendingDeltas_DrainAccumulatesAndClears (docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md T-08, issue #141): every delta
// processRuleCounters computes is also mirrored into pendingRuleDeltas;
// DrainRuleDeltas returns the accumulated total and clears it.
func TestTrafficStats_PendingDeltas_DrainAccumulatesAndClears(t *testing.T) {
	acct := &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-1": {Bytes: 1000, Packets: 10}}, // seed
			{"rule-1": {Bytes: 1500, Packets: 15}}, // delta 500/5
			{"rule-1": {Bytes: 1800, Packets: 18}}, // delta 300/3
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll()
	s.poll()

	pending := s.DrainRuleDeltas()
	if pending["rule-1"].Bytes != 500 || pending["rule-1"].Packets != 5 {
		t.Fatalf("expected pending delta 500/5 after first real poll, got %+v", pending["rule-1"])
	}

	// Drain must clear the accumulator.
	if again := s.DrainRuleDeltas(); len(again) != 0 {
		t.Fatalf("expected empty pending deltas immediately after drain, got %+v", again)
	}

	s.poll()
	pending = s.DrainRuleDeltas()
	if pending["rule-1"].Bytes != 300 || pending["rule-1"].Packets != 3 {
		t.Fatalf("expected pending delta 300/3 after second real poll, got %+v", pending["rule-1"])
	}
}

// TestTrafficStats_EndApply_ResetsBaselineAndBumpsEpoch verifies EndApply
// zeroes existing baselines (keeping keys/ruleSeeded) and increments the
// epoch, and that a subsequent poll credits the FULL post-apply counter
// value as a fresh delta (since the kernel counter itself restarted at 0
// too) rather than treating it as a "reset" double-credit.
func TestTrafficStats_EndApply_ResetsBaselineAndBumpsEpoch(t *testing.T) {
	acct := &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-1": {Bytes: 1000, Packets: 10}}, // seed
			{"rule-1": {Bytes: 1500, Packets: 15}}, // delta 500/5
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll()
	s.poll()
	_ = s.DrainRuleDeltas()

	epochBefore := s.ruleEpoch()
	s.EndApply()
	if s.ruleEpoch() != epochBefore+1 {
		t.Fatalf("expected EndApply to increment epoch from %d, got %d", epochBefore, s.ruleEpoch())
	}
	snap := s.RuleCounterSnapshot()
	if snap["rule-1"].Bytes != 0 || snap["rule-1"].Packets != 0 {
		t.Fatalf("expected baseline zeroed after EndApply, got %+v", snap["rule-1"])
	}
	if !s.RuleCountersReady() {
		t.Fatalf("expected ruleSeeded to remain true after EndApply")
	}

	// Simulate the newly-applied ruleset producing fresh traffic starting
	// from 0 again (nft counters always start at 0 after a flush+rebuild).
	acct.ruleResponses = append(acct.ruleResponses, map[string]model.RuleCounter{"rule-1": {Bytes: 120, Packets: 3}})
	s.poll()
	pending := s.DrainRuleDeltas()
	if pending["rule-1"].Bytes != 120 || pending["rule-1"].Packets != 3 {
		t.Fatalf("expected the full post-apply value (120/3) credited once, got %+v", pending["rule-1"])
	}
}

// TestTrafficStats_ProcessRuleCounters_StaleEpochDiscardsWholeDump is the
// direct regression test for D-5/Caution 10: a dump whose epoch no longer
// matches current must not touch baseline/pending/lastRuleHit at all.
func TestTrafficStats_ProcessRuleCounters_StaleEpochDiscardsWholeDump(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	staleEpoch := s.ruleEpoch()
	s.EndApply() // bumps the epoch, s.applyEpoch is now staleEpoch+1

	ruleDeltas := make(map[string]model.RuleCounter)
	s.processRuleCounters(map[string]model.RuleCounter{"rule-1": {Bytes: 9999, Packets: 99}}, ruleDeltas, staleEpoch)

	if len(ruleDeltas) != 0 {
		t.Fatalf("expected a stale-epoch dump to produce no deltas, got %+v", ruleDeltas)
	}
	if pending := s.DrainRuleDeltas(); len(pending) != 0 {
		t.Fatalf("expected a stale-epoch dump to leave pendingRuleDeltas untouched, got %+v", pending)
	}
	snap := s.RuleCounterSnapshot()
	if _, ok := snap["rule-1"]; ok {
		t.Fatalf("expected a stale-epoch dump to never seed a new rule id, got %+v", snap)
	}
}

// TestTrafficStats_PollRuleCountersOnce_FeedsBaselineAndPending exercises
// the FlushBeforeApply helper directly, outside the normal poll() cadence.
func TestTrafficStats_PollRuleCountersOnce_FeedsBaselineAndPending(t *testing.T) {
	acct := &fakeTrafficAccounting{
		ruleResponses: []map[string]model.RuleCounter{
			{"rule-1": {Bytes: 1000, Packets: 10}}, // seed
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	if err := s.PollRuleCountersOnce(); err != nil {
		t.Fatalf("PollRuleCountersOnce (seed): %v", err)
	}
	acct.ruleResponses = append(acct.ruleResponses, map[string]model.RuleCounter{"rule-1": {Bytes: 1100, Packets: 11}})
	if err := s.PollRuleCountersOnce(); err != nil {
		t.Fatalf("PollRuleCountersOnce (delta): %v", err)
	}
	pending := s.DrainRuleDeltas()
	if pending["rule-1"].Bytes != 100 || pending["rule-1"].Packets != 1 {
		t.Fatalf("expected PollRuleCountersOnce to feed a 100/1 delta, got %+v", pending["rule-1"])
	}
}
