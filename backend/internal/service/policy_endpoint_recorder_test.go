package service

import (
	"sort"
	"sync"
	"testing"

	"pigate/internal/model"
)

func TestPolicyEndpointRecorder_TallyThreeDirections(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 1000)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})

	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Dest: "8.8.8.8", Proto: "UDP", Port: "53", Time: "t1"})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Dest: "8.8.8.8", Proto: "UDP", Port: "53", Time: "t2"})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.2", Dest: "1.1.1.1", Proto: "TCP", Port: "443", Time: "t3"})

	deltas := r.Drain()
	if len(deltas) != 6 { // 2 src, 2 dst, 2 svc
		t.Fatalf("expected 6 deltas, got %d: %+v", len(deltas), deltas)
	}

	byDirKey := make(map[string]model.PersistedEndpoint)
	for _, d := range deltas {
		byDirKey[d.Direction+"|"+d.Key] = d
	}

	src1 := byDirKey[model.EndpointDirectionSrc+"|10.0.0.1"]
	if src1.Count != 2 || src1.FirstSeenAt != "t1" || src1.LastSeenAt != "t2" {
		t.Fatalf("unexpected src1: %+v", src1)
	}
	src2 := byDirKey[model.EndpointDirectionSrc+"|10.0.0.2"]
	if src2.Count != 1 {
		t.Fatalf("unexpected src2: %+v", src2)
	}
	dst1 := byDirKey[model.EndpointDirectionDst+"|8.8.8.8"]
	if dst1.Count != 2 {
		t.Fatalf("unexpected dst1: %+v", dst1)
	}
	svc1 := byDirKey[model.EndpointDirectionSvc+"|UDP/53"]
	if svc1.Count != 2 {
		t.Fatalf("unexpected svc1: %+v", svc1)
	}
	svc2 := byDirKey[model.EndpointDirectionSvc+"|TCP/443"]
	if svc2.Count != 1 {
		t.Fatalf("unexpected svc2: %+v", svc2)
	}

	// Drain must clear pending.
	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("expected empty drain after previous drain, got %+v", got)
	}
}

func TestPolicyEndpointRecorder_UnmonitoredRuleNotCounted(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 1000)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})

	r.Record(model.FirewallLog{RuleID: "rule-2", Src: "10.0.0.1", Time: "t1"})
	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("expected no deltas for unmonitored rule, got %+v", got)
	}
}

func TestPolicyEndpointRecorder_DisabledFeatureNotCounted(t *testing.T) {
	r := NewPolicyEndpointRecorder(false, 1000)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})

	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Time: "t1"})
	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("expected no deltas when disabled, got %+v", got)
	}
}

func TestPolicyEndpointRecorder_EmptyRuleIDIgnored(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 1000)
	r.SetMonitoredRules(map[string]bool{"": true}) // defensive: "" should never be set anyway

	r.Record(model.FirewallLog{RuleID: "", Src: "10.0.0.1", Time: "t1"})
	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("expected no deltas for empty RuleID, got %+v", got)
	}
}

// TestPolicyEndpointRecorder_SkipRulesMatchAggregateByRule locks in that "-"
// and "" are skipped for src/dest, and empty Proto skips svc — matching
// logs.RingBuffer.AggregateByRule exactly (Caution 8 of the plan).
func TestPolicyEndpointRecorder_SkipRulesMatchAggregateByRule(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 1000)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})

	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "-", Dest: "-", Proto: "", Time: "t1"})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "", Dest: "", Proto: "", Time: "t2"})

	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("expected no deltas for all-skip entries, got %+v", got)
	}

	// ICMP: proto set, port may be "-" — svc key still recorded (proto != "").
	r.Record(model.FirewallLog{RuleID: "rule-1", Proto: "ICMP", Port: "-", Time: "t3"})
	deltas := r.Drain()
	if len(deltas) != 1 || deltas[0].Direction != model.EndpointDirectionSvc || deltas[0].Key != "ICMP/-" {
		t.Fatalf("expected one svc delta ICMP/-, got %+v", deltas)
	}
}

func TestPolicyEndpointRecorder_AdmissionCap(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 2)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})

	// Two distinct sources fill the cap.
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Time: "t1"})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.2", Time: "t2"})
	// A third distinct source must be dropped (cap=2), but existing keys keep
	// counting.
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.3", Time: "t3"})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Time: "t4"})

	deltas := r.Drain()
	var srcDeltas []model.PersistedEndpoint
	for _, d := range deltas {
		if d.Direction == model.EndpointDirectionSrc {
			srcDeltas = append(srcDeltas, d)
		}
	}
	if len(srcDeltas) != 2 {
		t.Fatalf("expected exactly 2 src keys admitted (cap=2), got %d: %+v", len(srcDeltas), srcDeltas)
	}
	sort.Slice(srcDeltas, func(i, j int) bool { return srcDeltas[i].Key < srcDeltas[j].Key })
	if srcDeltas[0].Key != "10.0.0.1" || srcDeltas[0].Count != 2 {
		t.Fatalf("expected 10.0.0.1 count=2 (existing key kept counting), got %+v", srcDeltas[0])
	}
	if srcDeltas[1].Key != "10.0.0.2" || srcDeltas[1].Count != 1 {
		t.Fatalf("expected 10.0.0.2 count=1, got %+v", srcDeltas[1])
	}
}

func TestPolicyEndpointRecorder_SetMonitoredRulesClearsPendingOfDroppedRule(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 1000)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Time: "t1"})

	// rule-1 drops out of the monitored set.
	r.SetMonitoredRules(map[string]bool{"rule-2": true})

	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("expected pending for rule-1 to be discarded, got %+v", got)
	}
}

func TestPolicyEndpointRecorder_ClearRuleAndReset(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 1000)
	r.SetMonitoredRules(map[string]bool{"rule-1": true, "rule-2": true})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Time: "t1"})
	r.Record(model.FirewallLog{RuleID: "rule-2", Src: "10.0.0.2", Time: "t2"})

	r.ClearRule("rule-1")
	pending := r.Pending("rule-1")
	if pending != nil {
		for _, m := range pending {
			if len(m) != 0 {
				t.Fatalf("expected no pending data for cleared rule-1, got %+v", pending)
			}
		}
	}

	r.Reset()
	if got := r.Drain(); len(got) != 0 {
		t.Fatalf("expected Reset to clear all pending, got %+v", got)
	}
}

func TestPolicyEndpointRecorder_Pending(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 1000)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})
	r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Time: "t1"})

	pending := r.Pending("rule-1")
	srcMap := pending[model.EndpointDirectionSrc]
	if len(srcMap) != 1 || srcMap["10.0.0.1"].Count != 1 {
		t.Fatalf("expected pending src map with 10.0.0.1 count=1, got %+v", srcMap)
	}

	// Pending must not clear anything — Drain right after must still see it.
	deltas := r.Drain()
	if len(deltas) == 0 {
		t.Fatalf("expected Drain after Pending to still see the data (Pending must not clear)")
	}
}

// TestPolicyEndpointRecorder_Race exercises Record/Drain/SetMonitoredRules
// concurrently under -race (Caution 1/acceptance criteria of E-05).
func TestPolicyEndpointRecorder_Race(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 100)
	r.SetMonitoredRules(map[string]bool{"rule-1": true})

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 2000; i++ {
			r.Record(model.FirewallLog{RuleID: "rule-1", Src: "10.0.0.1", Dest: "8.8.8.8", Proto: "TCP", Port: "80", Time: "t"})
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 200; i++ {
			r.Drain()
			r.Pending("rule-1")
		}
	}()

	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := 0; i < 50; i++ {
			r.SetMonitoredRules(map[string]bool{"rule-1": true})
		}
	}()

	wg.Wait()
}

func TestNewPolicyEndpointRecorder_DefaultMaxPerDirectionFallback(t *testing.T) {
	r := NewPolicyEndpointRecorder(true, 0)
	if r.maxPerDirection != defaultMaxEndpointsPerDirection {
		t.Fatalf("expected fallback to defaultMaxEndpointsPerDirection (%d), got %d", defaultMaxEndpointsPerDirection, r.maxPerDirection)
	}
}
