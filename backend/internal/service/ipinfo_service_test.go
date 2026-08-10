package service

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestIPInfoServiceDisabled(t *testing.T) {
	mock := NewMockIPInfoProvider()
	svc := NewIPInfoService(false, mock)

	_, err := svc.Lookup(context.Background(), "8.8.8.8")
	if !errors.Is(err, ErrIPInfoDisabled) {
		t.Fatalf("expected ErrIPInfoDisabled, got %v", err)
	}
	if mock.Calls != 0 {
		t.Fatalf("expected provider to NEVER be called while disabled, got %d calls", mock.Calls)
	}
}

func TestIPInfoServiceNotPublic(t *testing.T) {
	mock := NewMockIPInfoProvider()
	svc := NewIPInfoService(true, mock)

	lanIPs := []string{"192.168.1.10", "10.0.0.1", "100.64.0.1", "127.0.0.1"}
	for _, ip := range lanIPs {
		_, err := svc.Lookup(context.Background(), ip)
		if !errors.Is(err, ErrIPInfoNotPublic) {
			t.Fatalf("ip=%s: expected ErrIPInfoNotPublic, got %v", ip, err)
		}
	}
	if mock.Calls != 0 {
		t.Fatalf("expected provider to NEVER be called for LAN IPs, got %d calls", mock.Calls)
	}
}

func TestIPInfoServiceCacheHit(t *testing.T) {
	mock := NewMockIPInfoProvider()
	svc := NewIPInfoService(true, mock)

	first, err := svc.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error on first lookup: %v", err)
	}
	if first.Source != "live" {
		t.Fatalf("expected first lookup Source=live, got %q", first.Source)
	}
	if first.CachedAt.IsZero() {
		t.Fatalf("expected live lookup to have a non-zero CachedAt")
	}
	if mock.Calls != 1 {
		t.Fatalf("expected exactly 1 provider call, got %d", mock.Calls)
	}

	second, err := svc.Lookup(context.Background(), "8.8.8.8")
	if err != nil {
		t.Fatalf("unexpected error on second lookup: %v", err)
	}
	if second.Source != "cache" {
		t.Fatalf("expected second lookup Source=cache, got %q", second.Source)
	}
	if second.CachedAt.IsZero() {
		t.Fatalf("expected cached lookup to have a non-zero CachedAt")
	}
	if !second.CachedAt.Equal(first.CachedAt) {
		t.Fatalf("expected cached lookup's CachedAt (%v) to match the original store time (%v)", second.CachedAt, first.CachedAt)
	}
	if mock.Calls != 1 {
		t.Fatalf("expected provider to NOT be called again on cache hit, got %d calls", mock.Calls)
	}
}

func TestIPInfoServiceRateLimited(t *testing.T) {
	mock := NewMockIPInfoProvider()
	svc := NewIPInfoService(true, mock)

	// Burst is 5; using a distinct IP per call to bypass the cache (a repeat
	// IP would short-circuit before ever reaching the limiter).
	ips := []string{"8.8.8.8", "1.1.1.1", "9.9.9.9", "4.2.2.2", "208.67.222.222", "8.8.4.4"}
	var lastErr error
	for _, ip := range ips {
		_, lastErr = svc.Lookup(context.Background(), ip)
	}
	if !errors.Is(lastErr, ErrIPInfoRateLimited) {
		t.Fatalf("expected the 6th distinct-IP lookup to be rate limited, got %v", lastErr)
	}
}

func TestIPInfoServiceSingleflightUnderLoad(t *testing.T) {
	mock := NewMockIPInfoProvider()
	svc := NewIPInfoService(true, mock)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _ = svc.Lookup(context.Background(), "8.8.8.8")
		}()
	}
	close(start)
	wg.Wait()

	if mock.Calls != 1 {
		t.Fatalf("expected exactly 1 provider call under concurrent load for the same IP, got %d", mock.Calls)
	}
}

// TestIPInfoServiceSingleflightStaggeredLoad guards against the race window
// that used to exist between deleting the inflight entry and closing its
// channel (the delete and close must happen under the same lock — see
// ipInfoCache.singleflightDo). Unlike TestIPInfoServiceSingleflightUnderLoad,
// which starts every goroutine at once (mostly exercising the "already
// inflight" branch), this staggers goroutine starts by a tiny random delay so
// some of them land right around the leader's finish, which is exactly the
// window where a duplicate provider call could previously slip through.
func TestIPInfoServiceSingleflightStaggeredLoad(t *testing.T) {
	const rounds = 50
	for i := 0; i < rounds; i++ {
		// A fresh service (and thus a fresh cache/rate-limiter) per round so
		// this round's lookups can only be satisfied by ITS OWN provider call,
		// never by a previous round's cache entry.
		mock := NewMockIPInfoProvider()
		svc := NewIPInfoService(true, mock)
		var wg sync.WaitGroup
		const n = 8
		wg.Add(n)
		for j := 0; j < n; j++ {
			delay := time.Duration(j%3) * time.Microsecond
			go func(d time.Duration) {
				defer wg.Done()
				if d > 0 {
					time.Sleep(d)
				}
				_, _ = svc.Lookup(context.Background(), "8.8.8.8")
			}(delay)
		}
		wg.Wait()

		if mock.Calls != 1 {
			t.Fatalf("round %d: expected exactly 1 provider call for staggered concurrent lookups of the same IP, got %d", i, mock.Calls)
		}
	}
}

func TestIPInfoServiceProviderFailure(t *testing.T) {
	mock := NewMockIPInfoProvider()
	mock.FailFor["203.0.114.55"] = true
	svc := NewIPInfoService(true, mock)

	_, err := svc.Lookup(context.Background(), "203.0.114.55")
	if !errors.Is(err, ErrIPInfoProvider) {
		t.Fatalf("expected ErrIPInfoProvider, got %v", err)
	}
}
