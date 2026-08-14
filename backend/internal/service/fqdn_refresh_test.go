package service

import "testing"

// TestDiffResolutions_BootCase: old is empty (unresolved at last apply), new
// resolves — this must be reported as "changed" (docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md T-04 acceptance case 1).
func TestDiffResolutions_BootCase(t *testing.T) {
	old := map[string][]string{"example.test": {}}
	new := map[string][]string{"example.test": {"203.0.113.1", "203.0.113.2"}}

	changed, anyUnresolved := diffResolutions(old, new)
	if len(changed) != 1 || changed[0] != "example.test" {
		t.Fatalf("expected example.test to be reported changed, got %v", changed)
	}
	if anyUnresolved {
		t.Fatalf("expected anyUnresolved=false once resolved, got true")
	}
}

// TestDiffResolutions_Unchanged: identical old/new must not be reported as
// changed (case 2).
func TestDiffResolutions_Unchanged(t *testing.T) {
	old := map[string][]string{"example.test": {"203.0.113.1", "203.0.113.2"}}
	new := map[string][]string{"example.test": {"203.0.113.1", "203.0.113.2"}}

	changed, anyUnresolved := diffResolutions(old, new)
	if len(changed) != 0 {
		t.Fatalf("expected no changes, got %v", changed)
	}
	if anyUnresolved {
		t.Fatalf("expected anyUnresolved=false, got true")
	}
}

// TestDiffResolutions_OrderOrCountDiffers: an order or count difference in
// the resolved IP list must be treated as a real change (case 3 — both
// sides are already sorted+capped by ResolveFQDNIPv4, D-2, so this reflects
// an actual DNS answer change, not resolver reordering noise).
func TestDiffResolutions_OrderOrCountDiffers(t *testing.T) {
	t.Run("order differs", func(t *testing.T) {
		old := map[string][]string{"example.test": {"203.0.113.1", "203.0.113.2"}}
		new := map[string][]string{"example.test": {"203.0.113.2", "203.0.113.1"}}
		changed, _ := diffResolutions(old, new)
		if len(changed) != 1 {
			t.Fatalf("expected order difference to be reported changed, got %v", changed)
		}
	})

	t.Run("count differs", func(t *testing.T) {
		old := map[string][]string{"example.test": {"203.0.113.1"}}
		new := map[string][]string{"example.test": {"203.0.113.1", "203.0.113.2"}}
		changed, _ := diffResolutions(old, new)
		if len(changed) != 1 {
			t.Fatalf("expected count difference to be reported changed, got %v", changed)
		}
	})
}

// TestDiffResolutions_UnresolvedStaysUnresolved: a FQDN that fails to
// resolve on this pass too must report anyUnresolved=true and NOT be
// reported as changed (case 4 — nothing meaningfully changed: still empty).
func TestDiffResolutions_UnresolvedStaysUnresolved(t *testing.T) {
	old := map[string][]string{"still-down.test": {}}
	new := map[string][]string{"still-down.test": nil}

	changed, anyUnresolved := diffResolutions(old, new)
	if len(changed) != 0 {
		t.Fatalf("expected no change when staying unresolved, got %v", changed)
	}
	if !anyUnresolved {
		t.Fatalf("expected anyUnresolved=true, got false")
	}
}

// TestDiffResolutions_MixedMultipleFQDNs exercises a realistic multi-FQDN
// tick: one resolved+unchanged, one changed, one still unresolved.
func TestDiffResolutions_MixedMultipleFQDNs(t *testing.T) {
	old := map[string][]string{
		"stable.test":  {"10.0.0.1"},
		"rotated.test": {"10.0.0.2"},
		"down.test":    {},
	}
	new := map[string][]string{
		"stable.test":  {"10.0.0.1"},
		"rotated.test": {"10.0.0.3"},
		"down.test":    nil,
	}

	changed, anyUnresolved := diffResolutions(old, new)
	if len(changed) != 1 || changed[0] != "rotated.test" {
		t.Fatalf("expected only rotated.test to be changed, got %v", changed)
	}
	if !anyUnresolved {
		t.Fatalf("expected anyUnresolved=true (down.test still unresolved)")
	}
}

// TestStringSlicesEqual is a small direct check of the helper diffResolutions
// relies on.
func TestStringSlicesEqual(t *testing.T) {
	cases := []struct {
		a, b []string
		want bool
	}{
		{nil, nil, true},
		{[]string{}, nil, true},
		{[]string{"a"}, []string{"a"}, true},
		{[]string{"a", "b"}, []string{"a"}, false},
		{[]string{"a"}, []string{"b"}, false},
	}
	for _, c := range cases {
		if got := stringSlicesEqual(c.a, c.b); got != c.want {
			t.Errorf("stringSlicesEqual(%v, %v) = %v, want %v", c.a, c.b, got, c.want)
		}
	}
}
