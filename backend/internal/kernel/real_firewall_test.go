package kernel

import (
	"net"
	"testing"
)

// TestResolveFQDNIPv4_SortsAscendingBeforeCapping locks in D-2 (docs/ref/
// todo/fqdn-retry-and-monitored-counters-plan.md, issue #141): resolved
// IPv4 addresses must be sorted ascending by raw bytes BEFORE the
// maxFQDNResolvedIPs cap is applied, so DNS round-robin reordering doesn't
// make a >8-A-record domain look "changed" on every refresh tick.
func TestResolveFQDNIPv4_SortsAscendingBeforeCapping(t *testing.T) {
	// Deliberately out-of-order input.
	in := []net.IP{
		net.ParseIP("203.0.113.30").To4(),
		net.ParseIP("203.0.113.10").To4(),
		net.ParseIP("203.0.113.20").To4(),
	}
	orig := lookupIP
	lookupIP = func(string) ([]net.IP, error) { return in, nil }
	t.Cleanup(func() { lookupIP = orig })

	got, err := ResolveFQDNIPv4("example.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := []string{"203.0.113.10", "203.0.113.20", "203.0.113.30"}
	if len(got) != len(want) {
		t.Fatalf("expected %d IPs, got %d: %v", len(want), len(got), got)
	}
	for i, ip := range got {
		if ip.String() != want[i] {
			t.Fatalf("index %d: got %s, want %s (full result %v)", i, ip.String(), want[i], got)
		}
	}
}

// TestResolveFQDNIPv4_CapsAfterSorting asserts that with more than
// maxFQDNResolvedIPs A records, the result is deterministic (the lowest
// maxFQDNResolvedIPs addresses) regardless of the order the resolver
// returned them in — proving sort-then-cap, not cap-then-sort.
func TestResolveFQDNIPv4_CapsAfterSorting(t *testing.T) {
	// Build maxFQDNResolvedIPs+5 addresses in reverse order.
	total := maxFQDNResolvedIPs + 5
	var in []net.IP
	for i := total - 1; i >= 0; i-- {
		in = append(in, net.IPv4(203, 0, 113, byte(i)).To4())
	}
	orig := lookupIP
	lookupIP = func(string) ([]net.IP, error) { return in, nil }
	t.Cleanup(func() { lookupIP = orig })

	got, err := ResolveFQDNIPv4("bigfanout.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != maxFQDNResolvedIPs {
		t.Fatalf("expected %d IPs, got %d: %v", maxFQDNResolvedIPs, len(got), got)
	}
	for i, ip := range got {
		want := net.IPv4(203, 0, 113, byte(i)).To4()
		if ip.String() != want.String() {
			t.Fatalf("index %d: got %s, want %s (lowest %d addresses expected)", i, ip.String(), want.String(), maxFQDNResolvedIPs)
		}
	}
}

// TestResolveFQDNIPv4_FiltersIPv6 asserts only IPv4 answers are kept.
func TestResolveFQDNIPv4_FiltersIPv6(t *testing.T) {
	in := []net.IP{
		net.ParseIP("2001:db8::1"),
		net.ParseIP("203.0.113.5").To4(),
	}
	orig := lookupIP
	lookupIP = func(string) ([]net.IP, error) { return in, nil }
	t.Cleanup(func() { lookupIP = orig })

	got, err := ResolveFQDNIPv4("mixed.test")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 1 || got[0].String() != "203.0.113.5" {
		t.Fatalf("expected only the IPv4 answer, got %v", got)
	}
}

// TestResolveFQDNIPv4_PropagatesResolveError asserts a resolver error is
// returned unchanged (no combos, no silent empty result) — callers (both
// addressCombos and FQDNRefresher) decide how to treat it.
func TestResolveFQDNIPv4_PropagatesResolveError(t *testing.T) {
	wantErr := &net.DNSError{Err: "no such host", IsNotFound: true}
	orig := lookupIP
	lookupIP = func(string) ([]net.IP, error) { return nil, wantErr }
	t.Cleanup(func() { lookupIP = orig })

	_, err := ResolveFQDNIPv4("nope.test")
	if err == nil {
		t.Fatalf("expected error to propagate, got nil")
	}
}

// TestFqdnRecorder_NilSafe asserts a nil *fqdnRecorder never panics.
func TestFqdnRecorder_NilSafe(t *testing.T) {
	var rec *fqdnRecorder
	rec.record("example.test", []string{"1.2.3.4"})
	if got := rec.snapshot(); got != nil {
		t.Fatalf("expected nil snapshot from nil recorder, got %v", got)
	}
}

// TestFqdnRecorder_RecordsEvenEmptyResolutions asserts a failed/empty
// resolution is still recorded with an empty (non-nil-vs-absent) slice, per
// D-1: this is the signal FQDNRefresher needs to know "still needs a retry"
// vs. "never referenced by an enabled rule".
func TestFqdnRecorder_RecordsEvenEmptyResolutions(t *testing.T) {
	rec := newFQDNRecorder()
	rec.record("unresolved.test", nil)
	rec.record("resolved.test", []string{"203.0.113.1", "203.0.113.2"})

	got := rec.snapshot()
	if len(got) != 2 {
		t.Fatalf("expected 2 keys, got %d: %v", len(got), got)
	}
	if ips, ok := got["unresolved.test"]; !ok || len(ips) != 0 {
		t.Fatalf("expected unresolved.test to be present with an empty slice, got %v (ok=%v)", ips, ok)
	}
	if ips := got["resolved.test"]; len(ips) != 2 {
		t.Fatalf("expected resolved.test to have 2 IPs, got %v", ips)
	}
}
