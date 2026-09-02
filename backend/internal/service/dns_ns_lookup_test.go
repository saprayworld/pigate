package service

import (
	"context"
	"errors"
	"net"
	"testing"
)

// stubResolveNSHostIPs overrides the package-level resolveNSHostIPs var for
// the duration of a test and restores it afterwards (precedent: stubLookupIP
// in kernel/policy_chain_test.go), so these tests never issue a real DNS
// query. callCount lets a test assert the stub was never reached (e.g. for
// an invalid name that must be rejected before any lookup).
func stubResolveNSHostIPs(t *testing.T, ips []net.IP, err error, callCount *int) {
	t.Helper()
	orig := resolveNSHostIPs
	resolveNSHostIPs = func(ctx context.Context, name string) ([]net.IP, error) {
		if callCount != nil {
			*callCount++
		}
		return ips, err
	}
	t.Cleanup(func() { resolveNSHostIPs = orig })
}

// resetNSLookupLimiter gives each test (or subtest that needs a full burst)
// a fresh token bucket, since nsLookupLimiter is a shared package-level var
// and tests otherwise run in whatever order go test picks.
func resetNSLookupLimiter(t *testing.T) {
	t.Helper()
	orig := nsLookupLimiter
	nsLookupLimiter = newNSLookupRateLimiter()
	t.Cleanup(func() { nsLookupLimiter = orig })
}

func TestResolveNameserver_ValidNameDedupesSortsAndCaps(t *testing.T) {
	resetNSLookupLimiter(t)
	ips := []net.IP{
		net.ParseIP("2001:db8::53"),
		net.ParseIP("203.0.113.53"),
		net.ParseIP("203.0.113.53"), // duplicate
		net.ParseIP("198.51.100.10"),
		net.ParseIP("2001:db8::54"),
	}
	stubResolveNSHostIPs(t, ips, nil, nil)

	svc := &DNSServerService{}
	got, err := svc.ResolveNameserver(context.Background(), "ns1.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Deduped: 4 unique addresses (2 v4, 2 v6), v4 sorted first.
	want := []string{"198.51.100.10", "203.0.113.53", "2001:db8::53", "2001:db8::54"}
	if len(got) != len(want) {
		t.Fatalf("expected %d addresses, got %v", len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q (full: %v)", i, got[i], want[i], got)
		}
	}
}

func TestResolveNameserver_CapsAtMaxGlueIPs(t *testing.T) {
	resetNSLookupLimiter(t)
	ips := []net.IP{
		net.ParseIP("198.51.100.1"),
		net.ParseIP("198.51.100.2"),
		net.ParseIP("198.51.100.3"),
		net.ParseIP("198.51.100.4"),
		net.ParseIP("198.51.100.5"),
	}
	stubResolveNSHostIPs(t, ips, nil, nil)

	svc := &DNSServerService{}
	got, err := svc.ResolveNameserver(context.Background(), "ns1.example.com")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(got) != 4 {
		t.Errorf("expected result capped at 4, got %d: %v", len(got), got)
	}
}

func TestResolveNameserver_InvalidNameRejectedWithoutLookup(t *testing.T) {
	resetNSLookupLimiter(t)
	badNames := []string{"", "ns1..x", "ns1\nx"}
	for _, name := range badNames {
		t.Run(name, func(t *testing.T) {
			calls := 0
			stubResolveNSHostIPs(t, nil, nil, &calls)

			svc := &DNSServerService{}
			_, err := svc.ResolveNameserver(context.Background(), name)
			if !errors.Is(err, ErrNSLookupInvalidName) {
				t.Errorf("expected ErrNSLookupInvalidName, got %v", err)
			}
			if calls != 0 {
				t.Errorf("expected the resolver stub to never be called for an invalid name, got %d calls", calls)
			}
		})
	}
}

func TestResolveNameserver_EmptyResultIsNotFound(t *testing.T) {
	resetNSLookupLimiter(t)
	stubResolveNSHostIPs(t, []net.IP{}, nil, nil)

	svc := &DNSServerService{}
	_, err := svc.ResolveNameserver(context.Background(), "ns1.example.com")
	if !errors.Is(err, ErrNSLookupNotFound) {
		t.Errorf("expected ErrNSLookupNotFound, got %v", err)
	}
}

func TestResolveNameserver_ResolverErrorIsPropagated(t *testing.T) {
	resetNSLookupLimiter(t)
	wantErr := errors.New("simulated servfail")
	stubResolveNSHostIPs(t, nil, wantErr, nil)

	svc := &DNSServerService{}
	_, err := svc.ResolveNameserver(context.Background(), "ns1.example.com")
	if err == nil {
		t.Fatalf("expected an error, got nil")
	}
	if !errors.Is(err, wantErr) {
		t.Errorf("expected the resolver error to be propagated, got %v", err)
	}
}

func TestResolveNameserver_RateLimitedAfterBurst(t *testing.T) {
	resetNSLookupLimiter(t)
	stubResolveNSHostIPs(t, []net.IP{net.ParseIP("203.0.113.53")}, nil, nil)

	svc := &DNSServerService{}
	// Burst is 5 (newNSLookupRateLimiter): the first 5 calls must succeed.
	for i := 0; i < 5; i++ {
		if _, err := svc.ResolveNameserver(context.Background(), "ns1.example.com"); err != nil {
			t.Fatalf("call %d: expected no error within burst, got %v", i, err)
		}
	}
	// The 6th call in immediate succession must be rate limited.
	if _, err := svc.ResolveNameserver(context.Background(), "ns1.example.com"); !errors.Is(err, ErrNSLookupRateLimited) {
		t.Errorf("expected ErrNSLookupRateLimited after exhausting the burst, got %v", err)
	}
}
