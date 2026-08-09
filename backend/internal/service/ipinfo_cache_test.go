package service

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"pigate/internal/model"
)

func TestIPInfoCacheTTLExpiry(t *testing.T) {
	c := newIPInfoCache()
	c.ttl = 20 * time.Millisecond
	c.put("8.8.8.8", model.IPInfoLookup{Ip: "8.8.8.8", City: "Mountain View"})

	if v, ok := c.get("8.8.8.8"); !ok || v.City != "Mountain View" {
		t.Fatalf("expected immediate cache hit, got ok=%v v=%+v", ok, v)
	}

	time.Sleep(40 * time.Millisecond)

	if _, ok := c.get("8.8.8.8"); ok {
		t.Fatalf("expected cache entry to have expired")
	}
}

func TestIPInfoCacheCapEviction(t *testing.T) {
	c := newIPInfoCache()
	c.maxEntries = 3

	for i := 0; i < 5; i++ {
		ip := ipForIndex(i)
		c.put(ip, model.IPInfoLookup{Ip: ip})
		// storedAt granularity matters for the oldest-eviction check below —
		// ensure each entry gets a distinct timestamp.
		time.Sleep(1 * time.Millisecond)
	}

	c.mu.Lock()
	n := len(c.entries)
	c.mu.Unlock()
	if n > 3 {
		t.Fatalf("expected cache to stay at or below maxEntries=3, got %d", n)
	}

	// The earliest-inserted entries should have been evicted first.
	if _, ok := c.get(ipForIndex(0)); ok {
		t.Fatalf("expected oldest entry to have been evicted")
	}
	if _, ok := c.get(ipForIndex(4)); !ok {
		t.Fatalf("expected newest entry to still be cached")
	}
}

func ipForIndex(i int) string {
	return "203.0." + string(rune('A'+i)) + ".1"
}

func TestIPInfoCacheNegativeEntry(t *testing.T) {
	c := newIPInfoCache()
	c.negativeTTL = 20 * time.Millisecond

	if c.isNegativelyCached("9.9.9.9") {
		t.Fatalf("expected no negative entry yet")
	}

	c.putNegative("9.9.9.9")

	if !c.isNegativelyCached("9.9.9.9") {
		t.Fatalf("expected negative entry to be live")
	}
	if _, ok := c.get("9.9.9.9"); ok {
		t.Fatalf("get() must never return a negative entry as a positive hit")
	}

	time.Sleep(40 * time.Millisecond)
	if c.isNegativelyCached("9.9.9.9") {
		t.Fatalf("expected negative entry to have expired")
	}
}

func TestIPInfoCacheSingleflight(t *testing.T) {
	c := newIPInfoCache()
	var calls int32

	const n = 20
	var wg sync.WaitGroup
	wg.Add(n)
	ranCount := int32(0)

	start := make(chan struct{})
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			<-start
			_, _, ran := c.singleflightDo("1.2.3.4", func() (model.IPInfoLookup, error) {
				atomic.AddInt32(&calls, 1)
				time.Sleep(10 * time.Millisecond)
				return model.IPInfoLookup{Ip: "1.2.3.4"}, nil
			})
			if ran {
				atomic.AddInt32(&ranCount, 1)
			}
		}()
	}
	close(start)
	wg.Wait()

	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Fatalf("expected provider fn to run exactly once, ran %d times", got)
	}
	if got := atomic.LoadInt32(&ranCount); got != 1 {
		t.Fatalf("expected exactly one goroutine to be the single-flight leader, got %d", got)
	}
}
