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
	return NewTrafficStatsService(acct, repo, dhcp, kernel.NewMockSystemStats())
}

func TestTrafficStats_SeedThenDelta(t *testing.T) {
	acct := &fakeTrafficAccounting{
		flowResponses: [][]model.FlowSample{
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 1000}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 1500}},
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
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 5000}},
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 5000}},
			// A brand-new flow reusing the same key with a smaller byte count
			// (would happen if a dead flow's key were reused) must clamp to 0,
			// never underflow.
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 100}},
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
			{{Key: "https", SrcIP: "192.168.1.10", Proto: 6, DstPort: 443, Bytes: 0}},
			{{Key: "https", SrcIP: "192.168.1.10", Proto: 6, DstPort: 443, Bytes: 1000}},
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
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 0}},    // seed
			{{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 1000}}, // delta 1000
		},
	}
	s := newTestTrafficStatsService(t, acct, nil)
	s.poll() // seed
	s.poll() // delta 1000, baseline now 1000

	s.onFlowEnd(model.FlowSample{Key: "f1", SrcIP: "192.168.1.50", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 1500})

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 1500 {
		t.Fatalf("expected combined poll+event observed bytes of 1500 (1000 from poll + 500 from event delta), got %d", detail.ObservedBytes)
	}
	if _, exists := s.flowState["f1"]; exists {
		t.Fatalf("expected flowState baseline for f1 to be deleted after onFlowEnd, so a later poll cannot double-credit it")
	}
}

// TestTrafficStats_OnFlowEnd_NoBaselineCreditsFull is plan T-07 case (ข): a
// flow that is born and dies entirely between two poll ticks has no baseline
// in flowState at all when its DESTROY event arrives — onFlowEnd must credit
// its full byte count in this case (this is exactly the gap this phase
// exists to close).
func TestTrafficStats_OnFlowEnd_NoBaselineCreditsFull(t *testing.T) {
	s := newTestTrafficStatsService(t, &fakeTrafficAccounting{}, nil)

	s.onFlowEnd(model.FlowSample{Key: "never-polled", SrcIP: "192.168.1.60", DstIP: "1.1.1.1", Proto: 17, DstPort: 53, Bytes: 777})

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 777 {
		t.Fatalf("expected full 777 bytes credited for a flow never seen by poll, got %d", detail.ObservedBytes)
	}
	if len(detail.TopTalkers) != 1 || detail.TopTalkers[0].IP != "192.168.1.60" || detail.TopTalkers[0].Bytes != 777 {
		t.Fatalf("unexpected top talkers: %+v", detail.TopTalkers)
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
			{{Key: "f-prune", SrcIP: "192.168.1.70", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 500}}, // seed
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

	s.onFlowEnd(model.FlowSample{Key: "f-prune", SrcIP: "192.168.1.70", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: 900})

	detail := s.GetTrafficDetail("1h")
	if detail.ObservedBytes != 900 {
		t.Fatalf("expected onFlowEnd to credit the full 900 bytes (no baseline survives the prune) without going negative/underflowed, got observed=%d", detail.ObservedBytes)
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
		{Key: "race-flow", SrcIP: "192.168.1.77", DstIP: "1.1.1.1", Proto: 6, DstPort: 443, Bytes: b},
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
				Key:     fmt.Sprintf("race-event-%d", i),
				SrcIP:   "192.168.1.99",
				DstIP:   "1.1.1.1",
				Proto:   17,
				DstPort: 53,
				Bytes:   uint64(i + 1),
			})
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; i < iterations; i++ {
			_ = s.GetTrafficDetail("1h")
			_ = s.GetTrafficDetail("24h")
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
			Key:     fmt.Sprintf("flow-%d", i),
			SrcIP:   "10.0.0.1",
			DstIP:   "1.1.1.1",
			Proto:   6,
			DstPort: 443,
			Bytes:   uint64(i),
		}
	}

	hostDeltas := make(map[string]uint64)
	catDeltas := make(map[string]uint64)
	s.processFlows(flows, hostDeltas, catDeltas) // seed poll — still populates flowState

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
