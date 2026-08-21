package service

import (
	"strconv"
	"sync"
	"testing"

	"pigate/internal/model"
)

func TestDNSBlockIndexEmpty(t *testing.T) {
	idx := &dnsBlockIndex{}
	if !idx.Empty() {
		t.Fatalf("expected a freshly constructed index to be Empty")
	}
	if _, _, ok := idx.Match("example.com"); ok {
		t.Fatalf("expected no match on an empty index")
	}

	idx.Set([]model.BlockedDomain{{Domain: "ads.example.com", Enabled: true}})
	if idx.Empty() {
		t.Fatalf("expected index to be non-empty after Set with an active rule")
	}
}

func TestDNSBlockIndexExactMatch(t *testing.T) {
	idx := &dnsBlockIndex{}
	idx.Set([]model.BlockedDomain{{Domain: "ads.example.com", Mode: model.DNSBlockModeSinkhole, Enabled: true}})

	rule, mode, ok := idx.Match("ads.example.com")
	if !ok || rule != "ads.example.com" || mode != model.DNSBlockModeSinkhole {
		t.Fatalf("got rule=%q mode=%q ok=%v, want ads.example.com/sinkhole/true", rule, mode, ok)
	}
}

func TestDNSBlockIndexSubdomainMultipleLevels(t *testing.T) {
	idx := &dnsBlockIndex{}
	idx.Set([]model.BlockedDomain{{Domain: "example.com", Enabled: true}})

	cases := []string{"www.example.com", "a.b.c.example.com", "deep.a.b.c.d.e.example.com"}
	for _, d := range cases {
		rule, mode, ok := idx.Match(d)
		if !ok || rule != "example.com" || mode != model.DNSBlockModeNXDomain {
			t.Fatalf("Match(%q) = rule=%q mode=%q ok=%v, want example.com/nxdomain/true", d, rule, mode, ok)
		}
	}
}

func TestDNSBlockIndexNoMatchSimilarSuffix(t *testing.T) {
	idx := &dnsBlockIndex{}
	idx.Set([]model.BlockedDomain{{Domain: "example.com", Enabled: true}})

	if _, _, ok := idx.Match("notexample.com"); ok {
		t.Fatalf("notexample.com must NOT match example.com's rule (label-boundary only)")
	}
	if _, _, ok := idx.Match("myexample.com"); ok {
		t.Fatalf("myexample.com must NOT match example.com's rule")
	}
	if _, _, ok := idx.Match("com"); ok {
		t.Fatalf("bare TLD must not match")
	}
}

func TestDNSBlockIndexDisabledFiltered(t *testing.T) {
	idx := &dnsBlockIndex{}
	idx.Set([]model.BlockedDomain{{Domain: "ads.example.com", Enabled: false}})

	if !idx.Empty() {
		t.Fatalf("expected disabled-only rule set to be treated as Empty")
	}
	if _, _, ok := idx.Match("ads.example.com"); ok {
		t.Fatalf("disabled rule must not match")
	}
}

func TestDNSBlockIndexInvalidFiltered(t *testing.T) {
	idx := &dnsBlockIndex{}
	idx.Set([]model.BlockedDomain{
		{Domain: "", Enabled: true},                               // empty domain
		{Domain: "localhost", Enabled: true},                      // no dot
		{Domain: "ads.example.com", Mode: "block", Enabled: true}, // invalid mode
	})

	if !idx.Empty() {
		t.Fatalf("expected all-invalid rule set to be treated as Empty")
	}
}

func TestDNSBlockIndexEmptyModeDefaultsToNXDomain(t *testing.T) {
	idx := &dnsBlockIndex{}
	idx.Set([]model.BlockedDomain{{Domain: "ads.example.com", Mode: "", Enabled: true}})

	_, mode, ok := idx.Match("ads.example.com")
	if !ok || mode != model.DNSBlockModeNXDomain {
		t.Fatalf("got mode=%q ok=%v, want nxdomain/true", mode, ok)
	}
}

func TestDNSBlockIndexSetReplacesWholeSet(t *testing.T) {
	idx := &dnsBlockIndex{}
	idx.Set([]model.BlockedDomain{{Domain: "old.example.com", Enabled: true}})
	if _, _, ok := idx.Match("old.example.com"); !ok {
		t.Fatalf("expected old.example.com to match before replacement")
	}

	idx.Set([]model.BlockedDomain{{Domain: "new.example.com", Enabled: true}})
	if _, _, ok := idx.Match("old.example.com"); ok {
		t.Fatalf("old.example.com must no longer match after Set replaced the rule set")
	}
	if _, _, ok := idx.Match("new.example.com"); !ok {
		t.Fatalf("expected new.example.com to match after replacement")
	}
}

func TestDNSBlockIndexConcurrency(t *testing.T) {
	idx := &dnsBlockIndex{}
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		n := i
		go func() {
			defer wg.Done()
			idx.Set([]model.BlockedDomain{{Domain: "domain" + strconv.Itoa(n) + ".example.com", Enabled: true}})
		}()
		go func() {
			defer wg.Done()
			idx.Match("sub.domain" + strconv.Itoa(n) + ".example.com")
			idx.Empty()
		}()
	}
	wg.Wait()
}
