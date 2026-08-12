package kernel

import (
	"math/rand"
	"testing"
)

// Without a provider, pickRuleID must return the fallback unchanged —
// preserves pre-T-07 behavior when main.go doesn't wire one (should never
// happen, but the mock must degrade gracefully).
func TestMockTrafficLog_PickRuleID_NoProvider(t *testing.T) {
	m := NewMockTrafficLog()
	rng := rand.New(rand.NewSource(1))
	if got := m.pickRuleID(rng, "fallback-id"); got != "fallback-id" {
		t.Fatalf("expected fallback-id with no provider, got %q", got)
	}
}

// An empty provider result also falls back, rather than panicking on
// rng.Intn(0).
func TestMockTrafficLog_PickRuleID_EmptyProvider(t *testing.T) {
	m := NewMockTrafficLog()
	m.SetRuleIDProvider(func() []string { return nil })
	rng := rand.New(rand.NewSource(1))
	if got := m.pickRuleID(rng, "fallback-id"); got != "fallback-id" {
		t.Fatalf("expected fallback-id with empty provider, got %q", got)
	}
}

// A non-empty provider is consulted: the result is always one of the
// provided ids.
func TestMockTrafficLog_PickRuleID_UsesProvider(t *testing.T) {
	m := NewMockTrafficLog()
	ids := []string{"rule-a", "rule-b", "rule-c"}
	m.SetRuleIDProvider(func() []string { return ids })
	rng := rand.New(rand.NewSource(1))

	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		got := m.pickRuleID(rng, "fallback-id")
		found := false
		for _, id := range ids {
			if got == id {
				found = true
			}
		}
		if !found {
			t.Fatalf("pickRuleID returned %q, not one of the provided ids %v", got, ids)
		}
		seen[got] = true
	}
	if len(seen) < 2 {
		t.Fatalf("expected pickRuleID to vary across calls, only saw %v", seen)
	}
}
