package kernel

import (
	"testing"
	"time"
)

// TestMockTrafficAccounting_DumpRuleCounters_SomeRulesUnused covers docs/
// ref/todo/firewall-policy-rule-usage-stats-plan.md T-08: in -mock=true dev
// mode, at least one rule id must deterministically get a zero counter (so
// the "Unused" status in the per-rule usage stats UI is exercisable), while
// every other rule stays > 0 once enough time has elapsed. Deterministic on
// rule position, never random (repeated calls must agree).
func TestMockTrafficAccounting_DumpRuleCounters_SomeRulesUnused(t *testing.T) {
	ids := []string{"rule-0", "rule-1", "rule-2", "rule-3", "rule-4", "rule-5", "rule-6", "rule-7"}
	m := NewMockTrafficAccounting(func() []string { return ids })

	// Force elapsed > 0 so a non-unused rule's synthetic rate actually
	// produces bytes (start defaults to time.Now(), so elapsed could be ~0 on
	// the very first call in an extremely fast test run).
	m.start = m.start.Add(-5 * time.Second)

	counters, err := m.DumpRuleCounters()
	if err != nil {
		t.Fatalf("DumpRuleCounters: %v", err)
	}

	var haveUnused, haveUsed bool
	for i, id := range ids {
		c, ok := counters[id]
		if !ok {
			t.Fatalf("expected an entry for %q", id)
		}
		if i%4 == 3 {
			if c.Bytes != 0 || c.Packets != 0 {
				t.Errorf("expected rule %q (index %d) to be deterministically unused (0 bytes/packets), got %+v", id, i, c)
			}
			haveUnused = true
		} else {
			if c.Bytes == 0 {
				t.Errorf("expected rule %q (index %d) to have non-zero bytes, got %+v", id, i, c)
			}
			haveUsed = true
		}
	}
	if !haveUnused || !haveUsed {
		t.Fatalf("expected both an unused and a used rule in the fixture, haveUnused=%v haveUsed=%v", haveUnused, haveUsed)
	}

	// Determinism: calling again must produce the exact same unused/used
	// split (never random).
	counters2, err := m.DumpRuleCounters()
	if err != nil {
		t.Fatalf("DumpRuleCounters (2nd call): %v", err)
	}
	for i, id := range ids {
		if i%4 == 3 && counters2[id].Bytes != 0 {
			t.Errorf("expected rule %q to remain deterministically unused on repeated calls", id)
		}
	}
}
