package logs

import (
	"testing"

	"pigate/internal/model"
)

// Empty buffer must not panic and returns zeroed maps/counters.
func TestAggregateByRuleEmptyBuffer(t *testing.T) {
	rb := NewRingBuffer(10)
	agg := rb.AggregateByRule("rule-1")
	if agg.Matched != 0 || agg.Scanned != 0 || agg.OldestTime != "" {
		t.Fatalf("unexpected agg for empty buffer: %+v", agg)
	}
	if len(agg.Sources) != 0 || len(agg.Dests) != 0 || len(agg.Services) != 0 {
		t.Fatalf("expected empty maps, got %+v", agg)
	}
}

// No entry matches the requested ruleID: everything scanned, nothing matched.
func TestAggregateByRuleNoMatch(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(model.FirewallLog{ID: "a", RuleID: "rule-other", Src: "1.1.1.1", Dest: "2.2.2.2", Proto: "TCP", Port: "80", Time: "2026-01-01T00:00:00Z"})
	agg := rb.AggregateByRule("rule-1")
	if agg.Matched != 0 {
		t.Fatalf("expected 0 matched, got %d", agg.Matched)
	}
	if agg.Scanned != 1 {
		t.Fatalf("expected scanned=1, got %d", agg.Scanned)
	}
}

// Entries with an empty RuleID are never counted, even if ruleID == "".
func TestAggregateByRuleEmptyRuleIDNeverMatches(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(model.FirewallLog{ID: "a", RuleID: "", Src: "1.1.1.1", Time: "2026-01-01T00:00:00Z"})
	agg := rb.AggregateByRule("")
	if agg.Matched != 0 {
		t.Fatalf("expected 0 matched for empty ruleID request, got %d", agg.Matched)
	}
	if len(agg.Sources) != 0 {
		t.Fatalf("expected no sources, got %+v", agg.Sources)
	}
}

// Counting, first/last seen ordering (scan is oldest->newest), and "-" skip.
func TestAggregateByRuleCountsAndOrdering(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(model.FirewallLog{ID: "1", RuleID: "rule-1", Src: "1.1.1.1", Dest: "2.2.2.2", Proto: "TCP", Port: "443", Time: "2026-01-01T00:00:00Z"})
	rb.Add(model.FirewallLog{ID: "2", RuleID: "rule-1", Src: "1.1.1.1", Dest: "-", Proto: "TCP", Port: "443", Time: "2026-01-01T00:01:00Z"})
	rb.Add(model.FirewallLog{ID: "3", RuleID: "rule-1", Src: "-", Dest: "2.2.2.2", Proto: "UDP", Port: "53", Time: "2026-01-01T00:02:00Z"})
	rb.Add(model.FirewallLog{ID: "4", RuleID: "rule-other", Src: "9.9.9.9", Time: "2026-01-01T00:03:00Z"})

	agg := rb.AggregateByRule("rule-1")
	if agg.Matched != 3 {
		t.Fatalf("expected matched=3, got %d", agg.Matched)
	}
	if agg.Scanned != 4 {
		t.Fatalf("expected scanned=4, got %d", agg.Scanned)
	}

	src, ok := agg.Sources["1.1.1.1"]
	if !ok || src.Count != 2 {
		t.Fatalf("expected src 1.1.1.1 count=2, got %+v ok=%v", src, ok)
	}
	if src.FirstSeen != "2026-01-01T00:00:00Z" || src.LastSeen != "2026-01-01T00:01:00Z" {
		t.Fatalf("unexpected first/last seen for src: %+v", src)
	}
	if _, ok := agg.Sources["-"]; ok {
		t.Fatal("dash src must be skipped")
	}

	dest, ok := agg.Dests["2.2.2.2"]
	if !ok || dest.Count != 2 {
		t.Fatalf("expected dest 2.2.2.2 count=2, got %+v ok=%v", dest, ok)
	}
	if dest.FirstSeen != "2026-01-01T00:00:00Z" || dest.LastSeen != "2026-01-01T00:02:00Z" {
		t.Fatalf("unexpected first/last seen for dest: %+v", dest)
	}
	if _, ok := agg.Dests["-"]; ok {
		t.Fatal("dash dest must be skipped")
	}

	if svc, ok := agg.Services["TCP/443"]; !ok || svc.Count != 2 {
		t.Fatalf("expected TCP/443 count=2, got %+v ok=%v", svc, ok)
	}
	if svc, ok := agg.Services["UDP/53"]; !ok || svc.Count != 1 {
		t.Fatalf("expected UDP/53 count=1, got %+v ok=%v", svc, ok)
	}

	if agg.OldestTime != "2026-01-01T00:00:00Z" {
		t.Fatalf("expected OldestTime = oldest entry in whole buffer, got %q", agg.OldestTime)
	}
}

// ICMP entries use Port "-" as a valid service key (not skipped, not
// converted to "0").
func TestAggregateByRuleICMPPortDash(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(model.FirewallLog{ID: "1", RuleID: "rule-1", Proto: "ICMP", Port: "-", Time: "2026-01-01T00:00:00Z"})
	agg := rb.AggregateByRule("rule-1")
	if svc, ok := agg.Services["ICMP/-"]; !ok || svc.Count != 1 {
		t.Fatalf("expected ICMP/- count=1, got %+v ok=%v", svc, ok)
	}
}

// AggregateByRule must never expose slices/maps aliasing internal buffer
// state that could be mutated by a subsequent Add.
func TestAggregateByRuleReturnedMapsAreIndependent(t *testing.T) {
	rb := NewRingBuffer(10)
	rb.Add(model.FirewallLog{ID: "1", RuleID: "rule-1", Src: "1.1.1.1", Time: "2026-01-01T00:00:00Z"})
	agg := rb.AggregateByRule("rule-1")
	agg.Sources["1.1.1.1"] = Counted{Count: 999}
	agg2 := rb.AggregateByRule("rule-1")
	if agg2.Sources["1.1.1.1"].Count != 1 {
		t.Fatalf("mutating returned map affected buffer state: %+v", agg2.Sources["1.1.1.1"])
	}
}
