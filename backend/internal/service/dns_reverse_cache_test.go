package service

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"pigate/internal/model"
)

// TestDNSReverseCache_TTLExpiry covers T-11 item 4: expired entries return
// "" from Lookup, and a re-seen entry has its TTL refreshed.
func TestDNSReverseCache_TTLExpiry(t *testing.T) {
	c := newDNSReverseCache()
	c.SetLimits(1, 128) // 1 minute TTL

	c.mu.Lock()
	c.entries["1.2.3.4"] = dnsReverseEntry{domain: "old.example.com", lastSeen: time.Now().Add(-2 * time.Minute)}
	c.mu.Unlock()

	if got := c.Lookup("1.2.3.4"); got != "" {
		t.Errorf("expected expired entry to return \"\", got %q", got)
	}

	c.Put("1.2.3.5", "fresh.example.com")
	if got := c.Lookup("1.2.3.5"); got != "fresh.example.com" {
		t.Errorf("Lookup = %q, want fresh.example.com", got)
	}
	// Re-seeing refreshes lastSeen — should not be expired even after we'd
	// have expired the original entry.
	c.Put("1.2.3.5", "fresh.example.com")
	c.mu.RLock()
	seen := c.entries["1.2.3.5"].lastSeen
	c.mu.RUnlock()
	if time.Since(seen) > time.Second {
		t.Errorf("expected lastSeen refreshed to ~now, got %v ago", time.Since(seen))
	}
}

// TestDNSReverseCache_CapEviction covers T-11 item 4: inserting past the cap
// never exceeds it and never panics.
func TestDNSReverseCache_CapEviction(t *testing.T) {
	c := newDNSReverseCache()
	c.SetLimits(60, 128)

	for i := 0; i < 500; i++ {
		ip := ipFromInt(i)
		c.Put(ip, "domain-example.com")
	}

	c.mu.RLock()
	size := len(c.entries)
	c.mu.RUnlock()
	if size > 128 {
		t.Errorf("cache size = %d, want <= 128", size)
	}
}

// TestDNSReverseCache_MultiFlag covers T-11 item 4: same IP, new domain ->
// last writer wins + multi flag set.
func TestDNSReverseCache_MultiFlag(t *testing.T) {
	c := newDNSReverseCache()
	c.Put("9.9.9.9", "first.example.com")
	c.Put("9.9.9.9", "second.example.com")

	if got := c.Lookup("9.9.9.9"); got != "second.example.com" {
		t.Errorf("Lookup = %q, want second.example.com (last writer wins)", got)
	}
	c.mu.RLock()
	multi := c.entries["9.9.9.9"].multi
	c.mu.RUnlock()
	if !multi {
		t.Error("expected multi=true after two different domains for the same IP")
	}
}

// TestDNSReverseCache_SetLimits covers T-11 item 5 (🔒): lowering maxEntries
// evicts immediately, lowering TTL expires old entries immediately at the
// next Lookup, and out-of-range/zero/negative input clamps rather than
// disabling or unbounding the cache.
func TestDNSReverseCache_SetLimits(t *testing.T) {
	t.Run("lowering maxEntries evicts immediately", func(t *testing.T) {
		c := newDNSReverseCache()
		c.SetLimits(60, 4096)
		for i := 0; i < 4096; i++ {
			c.Put(ipFromInt(i), "example.com")
		}
		c.mu.RLock()
		before := len(c.entries)
		c.mu.RUnlock()
		if before != 4096 {
			t.Fatalf("setup: expected 4096 entries, got %d", before)
		}

		c.SetLimits(60, 128)

		c.mu.RLock()
		after := len(c.entries)
		c.mu.RUnlock()
		if after > 128 {
			t.Errorf("expected size <= 128 immediately after SetLimits, got %d", after)
		}
	})

	t.Run("lowering TTL expires old entries at next Lookup", func(t *testing.T) {
		c := newDNSReverseCache()
		c.SetLimits(60, 4096)
		c.mu.Lock()
		c.entries["5.5.5.5"] = dnsReverseEntry{domain: "old.example.com", lastSeen: time.Now().Add(-90 * time.Second)}
		c.mu.Unlock()

		if got := c.Lookup("5.5.5.5"); got != "old.example.com" {
			t.Fatalf("setup: expected entry to still be valid under 60min TTL, got %q", got)
		}

		c.SetLimits(1, 4096) // 1 minute TTL — the 90s-old entry is now expired

		if got := c.Lookup("5.5.5.5"); got != "" {
			t.Errorf("expected entry to be expired after shortening TTL, got %q", got)
		}
	})

	t.Run("out-of-range values are clamped, never disable or unbound the cache", func(t *testing.T) {
		c := newDNSReverseCache()
		cases := []struct{ ttl, max int }{
			{0, 0},
			{-1, -1},
			{9999, 999999},
		}
		for _, tc := range cases {
			c.SetLimits(tc.ttl, tc.max)
			c.mu.RLock()
			ttl, maxSize := c.ttl, c.maxSize
			c.mu.RUnlock()
			if ttl <= 0 {
				t.Errorf("SetLimits(%d, %d): ttl clamped to non-positive %v", tc.ttl, tc.max, ttl)
			}
			if maxSize < model.DNSCacheEntriesMin || maxSize > model.DNSCacheEntriesMax {
				t.Errorf("SetLimits(%d, %d): maxSize %d escaped bounds [%d,%d]", tc.ttl, tc.max, maxSize, model.DNSCacheEntriesMin, model.DNSCacheEntriesMax)
			}
		}
	})
}

// TestDNSReverseCache_LookupMany covers the batch lookup used once per
// /api/statistics/traffic request.
func TestDNSReverseCache_LookupMany(t *testing.T) {
	c := newDNSReverseCache()
	c.Put("142.250.80.46", "www.youtube.com")
	c.Put("173.194.76.94", "googlevideo.com")

	got := c.LookupMany([]string{"142.250.80.46", "8.8.8.8", "173.194.76.94"})
	if len(got) != 2 {
		t.Fatalf("expected 2 resolved entries, got %d: %v", len(got), got)
	}
	if got["142.250.80.46"] != "www.youtube.com" {
		t.Errorf("got[142.250.80.46] = %q", got["142.250.80.46"])
	}
	if _, ok := got["8.8.8.8"]; ok {
		t.Error("expected unmapped IP to be absent from result, not present with empty string")
	}
}

// TestDNSReverseCache_Race exercises Put/Lookup/SetLimits concurrently under
// -race (T-11 item 14 covers the full package; this is the cache's own
// isolated version of the same guarantee).
func TestDNSReverseCache_Race(t *testing.T) {
	c := newDNSReverseCache()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(3)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				c.Put(ipFromInt(i%1000), "example.com")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				c.Lookup(ipFromInt(i % 1000))
			}
		}
	}()
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				c.SetLimits(60+(i%10), 128+(i%100))
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// ipFromInt generates a deterministic, distinct IPv4 address string for test
// fixtures needing many unique cache keys.
func ipFromInt(i int) string {
	b := byte((i >> 16) & 0xff)
	c := byte((i >> 8) & 0xff)
	d := byte(i & 0xff)
	return fmt.Sprintf("10.%d.%d.%d", b, c, d)
}
