package service

import (
	"sync"
	"time"

	"pigate/internal/model"
)

// ipInfoCache is a RAM-only cache of Public IP Info lookups (docs/ref/todo/
// statistics-host-ipinfo-plan.md T-03) — modeled after dnsReverseCache
// (sync.RWMutex + map + evictExpiredLocked/evictToSizeLocked), but with two
// extra features that cache does not need: negative entries (a failed lookup
// is cached too, for IPInfoNegativeTTL, so a transient provider outage does
// not turn into a hammering loop from the page's 10s auto-refresh) and a
// hand-rolled single-flight (plan Caution 6: golang.org/x/sync is not in
// go.sum, so this must not add it as a dependency).
//
// NEVER persist any of this to disk storage or the data access layer from
// this file (plan Caution 5 — RAM-only, SD card wear).
type ipInfoCache struct {
	mu          sync.Mutex
	entries     map[string]ipInfoCacheEntry
	maxEntries  int
	ttl         time.Duration
	negativeTTL time.Duration

	// inflight implements single-flight: the first caller for a given key
	// creates the channel and stores it; concurrent callers for the same key
	// find it already present and just wait on it instead of triggering a
	// second provider request. The channel is closed (not sent on) once the
	// first caller finishes, which is what wakes every waiter.
	inflight map[string]chan struct{}
}

type ipInfoCacheEntry struct {
	value      model.IPInfoLookup
	storedAt   time.Time
	ttl        time.Duration
	isNegative bool
}

func newIPInfoCache() *ipInfoCache {
	return &ipInfoCache{
		entries:     make(map[string]ipInfoCacheEntry),
		maxEntries:  model.IPInfoCacheMaxEntries,
		ttl:         model.IPInfoCacheTTLDefault,
		negativeTTL: model.IPInfoNegativeTTL,
		inflight:    make(map[string]chan struct{}),
	}
}

// get returns (value, true) on a live cache hit (positive or negative — the
// caller distinguishes via the returned entry's isNegative through getRaw if
// needed; get itself only reports positive hits since that's all Lookup
// callers want). Expired entries are evicted lazily.
func (c *ipInfoCache) get(ip string) (model.IPInfoLookup, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, hit, _ := c.lookupLocked(ip)
	return v, hit
}

// isNegativelyCached indicates whether ip currently has a live negative
// (failed-lookup) cache entry — used by the service layer to short-circuit
// without calling the provider again.
func (c *ipInfoCache) isNegativelyCached(ip string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, _, negative := c.lookupLocked(ip)
	return negative
}

// lookupLocked is the single source of truth for "is ip live-cached right
// now" — c.mu must already be held. It exists so singleflightDo can perform
// its leader-vs-follower decision and the cache check as one atomic step
// (see singleflightDo doc comment for why that atomicity matters); get() and
// isNegativelyCached() are thin locking wrappers around it so there is only
// one place that implements the expiry/eviction rule.
func (c *ipInfoCache) lookupLocked(ip string) (value model.IPInfoLookup, hit bool, negative bool) {
	e, ok := c.entries[ip]
	if !ok {
		return model.IPInfoLookup{}, false, false
	}
	if time.Since(e.storedAt) > e.ttl {
		delete(c.entries, ip)
		return model.IPInfoLookup{}, false, false
	}
	if e.isNegative {
		return model.IPInfoLookup{}, false, true
	}
	v := e.value
	v.CachedAt = e.storedAt
	return v, true, false
}

// put stores a successful lookup result and returns the time it was stored
// at, so the caller (IPInfoService.Lookup, "live" path) can populate
// model.IPInfoLookup.CachedAt on the value it returns to the client — the
// entry stored in the cache and the entry returned for this call must report
// the same CachedAt.
func (c *ipInfoCache) put(ip string, v model.IPInfoLookup) time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	storedAt := time.Now()
	c.storeLocked(ip, ipInfoCacheEntry{value: v, storedAt: storedAt, ttl: c.ttl, isNegative: false})
	return storedAt
}

// putNegative records a failed lookup so repeated failures within
// negativeTTL don't retrigger a provider request.
func (c *ipInfoCache) putNegative(ip string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.storeLocked(ip, ipInfoCacheEntry{storedAt: time.Now(), ttl: c.negativeTTL, isNegative: true})
}

func (c *ipInfoCache) storeLocked(ip string, e ipInfoCacheEntry) {
	if _, exists := c.entries[ip]; !exists && len(c.entries) >= c.maxEntries {
		c.evictExpiredLocked()
		if len(c.entries) >= c.maxEntries {
			c.evictOldestLocked()
		}
	}
	c.entries[ip] = e
}

func (c *ipInfoCache) evictExpiredLocked() {
	now := time.Now()
	for k, e := range c.entries {
		if now.Sub(e.storedAt) > e.ttl {
			delete(c.entries, k)
		}
	}
}

func (c *ipInfoCache) evictToSizeLocked() {
	for len(c.entries) > c.maxEntries {
		c.evictOldestLocked()
	}
}

// evictOldestLocked removes the single oldest (by storedAt) entry — a linear
// scan, same tradeoff dnsReverseCache accepts (evictOldestLocked pattern),
// fine at IPInfoCacheMaxEntries (1000) scale.
func (c *ipInfoCache) evictOldestLocked() {
	var oldestKey string
	var oldestAt time.Time
	first := true
	for k, e := range c.entries {
		if first || e.storedAt.Before(oldestAt) {
			oldestKey = k
			oldestAt = e.storedAt
			first = false
		}
	}
	if !first {
		delete(c.entries, oldestKey)
	}
}

// singleflightDo ensures only one concurrent caller per key actually runs fn;
// every other concurrent caller for the same key blocks until the first
// finishes, then reports that leader's cached outcome instead of calling fn
// itself. This is a hand-rolled minimal single-flight (plan Caution 6 —
// golang.org/x/sync/singleflight is not available).
//
// Cache-hit check and the leader/follower decision are performed as ONE
// atomic step under c.mu (both the initial check and the re-check after
// waiting on a leader's channel). This closes a TOCTOU window that used to
// exist when IPInfoService.Lookup checked the cache itself *before* calling
// singleflightDo: a caller could observe a cache miss, then stall (GC pause,
// scheduler preemption, whatever) long enough for another goroutine to run
// as leader *start to finish* — including deleting the inflight entry and
// closing its channel — before the stalled caller ever reached this
// function. Finding no inflight entry, it would then wrongly appoint itself
// a brand-new leader and call fn (the provider) a second time, even though
// the cache had a fresh answer the entire time. By making the cache check
// and the inflight-map check happen under the same lock acquisition here,
// there is no gap in which that can happen: either the cache already has the
// answer (return it), or it doesn't and this goroutine atomically becomes
// leader or follower for this key.
//
// Returns fn's result when this goroutine was the leader (ran==true). When
// this goroutine found the answer already cached (either up front or after
// waiting for another leader), ran==false and result/err report that cached
// outcome directly: err is nil with a valid value on a positive hit, or
// ErrIPInfoProvider on a negative (previously-failed) hit — the caller does
// NOT need to re-check the cache itself.
func (c *ipInfoCache) singleflightDo(key string, fn func() (model.IPInfoLookup, error)) (result model.IPInfoLookup, err error, ran bool) {
	c.mu.Lock()
	if v, hit, negHit := c.lookupLocked(key); hit {
		c.mu.Unlock()
		return v, nil, false
	} else if negHit {
		c.mu.Unlock()
		return model.IPInfoLookup{}, ErrIPInfoProvider, false
	}

	if ch, inFlight := c.inflight[key]; inFlight {
		c.mu.Unlock()
		<-ch
		// The leader is guaranteed to have written its outcome (positive or
		// negative) to the cache before closing ch (see below), so this is a
		// definitive answer, not another racy peek.
		c.mu.Lock()
		v, hit, negHit := c.lookupLocked(key)
		c.mu.Unlock()
		if hit {
			return v, nil, false
		}
		if negHit {
			return model.IPInfoLookup{}, ErrIPInfoProvider, false
		}
		// The leader's entry expired/was evicted in the tiny window between
		// it being written and us reading it — extremely unlikely, but fall
		// back to ErrIPInfoProvider rather than silently returning a zero
		// value as a "success".
		return model.IPInfoLookup{}, ErrIPInfoProvider, false
	}

	ch := make(chan struct{})
	c.inflight[key] = ch
	c.mu.Unlock()

	result, err = fn()

	c.mu.Lock()
	delete(c.inflight, key)
	close(ch)
	c.mu.Unlock()

	return result, err, true
}
