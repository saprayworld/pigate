package service

import (
	"net/netip"
	"sync"
	"time"

	"pigate/internal/model"
)

// dnsReverseCache maps a normalized destination IP to the domain name
// dnsmasq most recently answered for it (docs/ref/todo/
// statistics-dns-top-domain-plan.md T-08) — display-only enrichment for the
// Top Destinations/Conversations cards. NEVER used for firewall rule
// generation, policy matching, or routing/QoS decisions (plan §5 item 6):
// the mapping can be trivially poisoned by any LAN client (plan §5 item 8),
// so it must stay confined to "what did DNS answer" display, not "fact".
//
// ttl/maxSize are runtime-tunable (from DNSServerSettings, via SetLimits) —
// not const — per plan §2.1's explicit requirement that TTL/cap adjust live
// without any restart.
type dnsReverseCache struct {
	mu      sync.RWMutex
	entries map[string]dnsReverseEntry
	ttl     time.Duration
	maxSize int
}

type dnsReverseEntry struct {
	domain   string
	lastSeen time.Time
	// multi is true once this IP has been observed answering for more than
	// one distinct domain (CDN/cloud-LB IP reuse — plan §2 "last writer wins
	// พร้อม flag ว่าเคยเห็นหลายชื่อ"). Currently tracked for future UI use;
	// Lookup/LookupMany only return the current domain.
	multi bool
}

// newDNSReverseCache constructs the cache with the package defaults
// (model.DNSCacheTTLDefault/DNSCacheEntriesDefault) — callers apply the
// DB-configured values via SetLimits right after construction (main.go boot
// wiring, T-09).
func newDNSReverseCache() *dnsReverseCache {
	return &dnsReverseCache{
		entries: make(map[string]dnsReverseEntry),
		ttl:     time.Duration(model.DNSCacheTTLDefault) * time.Minute,
		maxSize: model.DNSCacheEntriesDefault,
	}
}

// SetLimits applies a new TTL/cap pair, live (no restart of anything — plan
// §2.1 item 4 / §5 item 18). Values are clamped by model's shared bounds as a
// second line of defense even though the API handler already validates
// (plan §5 item 17: DB and backup files are also untrusted input paths).
// Shrinking maxEntries evicts down to the new cap immediately (plan §5 item
// 19) rather than waiting for the next Put.
func (c *dnsReverseCache) SetLimits(ttlMinutes, maxEntries int) {
	if ttlMinutes < model.DNSCacheTTLMin || ttlMinutes > model.DNSCacheTTLMax {
		ttlMinutes = model.DNSCacheTTLDefault
	}
	if maxEntries < model.DNSCacheEntriesMin || maxEntries > model.DNSCacheEntriesMax {
		maxEntries = model.DNSCacheEntriesDefault
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	c.ttl = time.Duration(ttlMinutes) * time.Minute
	c.maxSize = maxEntries
	c.evictExpiredLocked()
	c.evictToSizeLocked()
}

// Put records/refreshes ip -> domain. ip and domain are expected to already
// be normalized/sanitized by the kernel-layer parser (netip.Addr.String() /
// sanitizeDomain) — Put itself re-normalizes ip defensively since a caller
// mistake here would otherwise silently break every Lookup.
func (c *dnsReverseCache) Put(ip, domain string) {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return
	}
	key := addr.String()

	c.mu.Lock()
	defer c.mu.Unlock()

	now := time.Now()
	multi := false
	if existing, ok := c.entries[key]; ok && existing.domain != "" && existing.domain != domain {
		multi = true
	}

	if _, exists := c.entries[key]; !exists && len(c.entries) >= c.maxSize {
		c.evictExpiredLocked()
		if len(c.entries) >= c.maxSize {
			c.evictOldestLocked()
		}
	}

	c.entries[key] = dnsReverseEntry{domain: domain, lastSeen: now, multi: multi}
}

// Lookup returns the domain for ip, or "" when unknown or expired. Never
// returns the IP itself as a fallback (plan T-08: "ห้ามคืน IP แทน").
func (c *dnsReverseCache) Lookup(ip string) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lookupLocked(ip)
}

func (c *dnsReverseCache) lookupLocked(ip string) string {
	e, ok := c.entries[ip]
	if !ok {
		return ""
	}
	if time.Since(e.lastSeen) > c.ttl {
		delete(c.entries, ip)
		return ""
	}
	return e.domain
}

// LookupMany resolves a batch of IPs in one lock acquisition — used once per
// /api/statistics/traffic request when composing TopHost/TopConversation rows
// (plan T-08: "RLock ก้อนเดียว ไม่ล็อกทีละแถว"). Expired entries encountered
// during the batch are lazily evicted, same as single Lookup.
func (c *dnsReverseCache) LookupMany(ips []string) map[string]string {
	out := make(map[string]string, len(ips))
	if len(ips) == 0 {
		return out
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, ip := range ips {
		if d := c.lookupLocked(ip); d != "" {
			out[ip] = d
		}
	}
	return out
}

// Clear empties the cache — called when the user turns query logging off
// (plan §5 item 1: privacy).
func (c *dnsReverseCache) Clear() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries = make(map[string]dnsReverseEntry)
}

// sweepExpired runs a periodic full pass over the map, reusing the watcher's
// existing goroutine/ticker rather than starting a new one just for this
// (plan T-08: "ห้ามสร้าง ticker ใหม่ทั้ง service เพื่อเรื่องนี้"). Lazy
// eviction in Lookup/LookupMany/Put already keeps hot entries honest; this is
// purely to reclaim RAM for IPs that stop being looked up at all.
func (c *dnsReverseCache) sweepExpired() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.evictExpiredLocked()
}

// evictExpiredLocked removes every entry past its TTL. Caller must hold mu.
func (c *dnsReverseCache) evictExpiredLocked() {
	now := time.Now()
	for k, e := range c.entries {
		if now.Sub(e.lastSeen) > c.ttl {
			delete(c.entries, k)
		}
	}
}

// evictToSizeLocked removes the oldest (by lastSeen) entries until the map
// is at or under maxSize. Caller must hold mu. Used by SetLimits when the cap
// is lowered — must take effect immediately, not wait for the next Put (plan
// §5 item 19).
func (c *dnsReverseCache) evictToSizeLocked() {
	for len(c.entries) > c.maxSize {
		c.evictOldestLocked()
	}
}

// evictOldestLocked removes the single least-recently-seen entry. Caller
// must hold mu and must have already confirmed the map is non-empty (a
// zero-length map is a no-op here, harmless).
func (c *dnsReverseCache) evictOldestLocked() {
	var oldestKey string
	var oldestTime time.Time
	first := true
	for k, e := range c.entries {
		if first || e.lastSeen.Before(oldestTime) {
			oldestKey = k
			oldestTime = e.lastSeen
			first = false
		}
	}
	if !first {
		delete(c.entries, oldestKey)
	}
}
