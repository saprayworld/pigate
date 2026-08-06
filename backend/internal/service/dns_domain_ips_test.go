package service

import (
	"sync"
	"testing"
	"time"

	"pigate/internal/model"
)

// TestDNSDomainIPs_DomainCap covers plan §2.1: a brand-new domain is
// rejected once len(byDomain) >= maxDomains, and Truncated() latches true.
func TestDNSDomainIPs_DomainCap(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(60, 100, 16) // min allowed maxDomains

	for i := 0; i < 100; i++ {
		d.Put("domain-"+ipFromInt(i)+".example.com", ipFromInt(i))
	}
	d.mu.RLock()
	size := len(d.byDomain)
	d.mu.RUnlock()
	if size != 100 {
		t.Fatalf("expected 100 domains admitted, got %d", size)
	}
	if d.Truncated() {
		t.Fatalf("expected not truncated yet, filled exactly to cap")
	}

	// One more brand-new domain should be rejected.
	d.Put("overflow.example.com", "9.9.9.9")
	d.mu.RLock()
	size = len(d.byDomain)
	_, overflowExists := d.byDomain["overflow.example.com"]
	d.mu.RUnlock()
	if size != 100 {
		t.Errorf("expected domain count to stay at cap 100, got %d", size)
	}
	if overflowExists {
		t.Errorf("expected overflow domain to be rejected once maxDomains is hit")
	}
	if !d.Truncated() {
		t.Errorf("expected Truncated()=true after a domain was rejected")
	}
}

// TestDNSDomainIPs_PerDomainIPCap covers plan §2.1: a new IP within an
// existing domain is rejected once maxIPsPerDomain is hit, but an existing
// IP must still get its lastSeen refreshed even while the domain is "full".
func TestDNSDomainIPs_PerDomainIPCap(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(60, 1000, 2) // min allowed maxIPsPerDomain

	d.Put("example.com", "1.1.1.1")
	d.Put("example.com", "2.2.2.2")
	if d.Truncated() {
		t.Fatalf("expected not truncated after exactly filling the per-domain cap")
	}

	// A third, brand-new IP should be rejected.
	d.Put("example.com", "3.3.3.3")
	ips := d.IPsFor("example.com")
	if len(ips) != 2 {
		t.Fatalf("expected 2 IPs kept for example.com, got %d: %+v", len(ips), ips)
	}
	for _, e := range ips {
		if e.IP == "3.3.3.3" {
			t.Errorf("expected 3.3.3.3 to be rejected once maxIPsPerDomain is hit")
		}
	}
	if !d.Truncated() {
		t.Errorf("expected Truncated()=true after a per-domain IP was rejected")
	}

	// An existing IP must still refresh lastSeen even while "full".
	d.mu.Lock()
	d.byDomain["example.com"]["1.1.1.1"] = time.Now().Add(-time.Hour)
	d.mu.Unlock()
	d.Put("example.com", "1.1.1.1")
	d.mu.RLock()
	refreshed := d.byDomain["example.com"]["1.1.1.1"]
	d.mu.RUnlock()
	if time.Since(refreshed) > 2*time.Second {
		t.Errorf("expected lastSeen refreshed to ~now, got %v ago", time.Since(refreshed))
	}
}

// TestDNSDomainIPs_TTLExpiry covers plan §2.1: entries past ttl are evicted
// lazily on read (IPsFor/Snapshot), and ipDomains is kept consistent.
func TestDNSDomainIPs_TTLExpiry(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(1, 1000, 16) // 1 minute TTL

	d.mu.Lock()
	d.byDomain["old.example.com"] = map[string]time.Time{
		"1.2.3.4": time.Now().Add(-2 * time.Minute),
	}
	d.ipDomains["1.2.3.4"] = map[string]struct{}{"old.example.com": {}}
	d.mu.Unlock()

	ips := d.IPsFor("old.example.com")
	if len(ips) != 0 {
		t.Errorf("expected expired IP to be evicted from IPsFor, got %+v", ips)
	}

	d.mu.RLock()
	_, domainStillExists := d.byDomain["old.example.com"]
	_, refStillExists := d.ipDomains["1.2.3.4"]
	d.mu.RUnlock()
	if domainStillExists {
		t.Errorf("expected domain with zero remaining IPs to be dropped")
	}
	if refStillExists {
		t.Errorf("expected ipDomains entry to be cleaned up on expiry")
	}

	// A fresh Put should not be expired.
	d.Put("fresh.example.com", "5.5.5.5")
	ips = d.IPsFor("fresh.example.com")
	if len(ips) != 1 || ips[0].IP != "5.5.5.5" {
		t.Fatalf("expected fresh IP to survive, got %+v", ips)
	}
}

// TestDNSDomainIPs_SharedFlag covers plan §2.1: the Shared flag becomes true
// once a second domain references the same IP, and false again once that
// second domain's reference is evicted (e.g. via TTL expiry).
func TestDNSDomainIPs_SharedFlag(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(60, 1000, 16)

	d.Put("a.example.com", "10.0.0.1")
	ips := d.IPsFor("a.example.com")
	if len(ips) != 1 || ips[0].Shared {
		t.Fatalf("expected shared=false with only one domain referencing the IP, got %+v", ips)
	}

	d.Put("b.example.com", "10.0.0.1")
	ips = d.IPsFor("a.example.com")
	if len(ips) != 1 || !ips[0].Shared {
		t.Fatalf("expected shared=true once a second domain references the IP, got %+v", ips)
	}
	ips = d.IPsFor("b.example.com")
	if len(ips) != 1 || !ips[0].Shared {
		t.Fatalf("expected shared=true on b.example.com's row too, got %+v", ips)
	}

	// Force b.example.com's reference to expire, then trigger eviction via
	// sweepExpired (a.example.com's own reference is untouched/fresh).
	d.mu.Lock()
	d.byDomain["b.example.com"]["10.0.0.1"] = time.Now().Add(-2 * time.Hour)
	d.ttl = time.Minute
	d.mu.Unlock()
	d.sweepExpired()

	ips = d.IPsFor("a.example.com")
	if len(ips) != 1 || ips[0].Shared {
		t.Fatalf("expected shared=false again after the other domain's reference expired, got %+v", ips)
	}
	d.mu.RLock()
	_, bExists := d.byDomain["b.example.com"]
	d.mu.RUnlock()
	if bExists {
		t.Errorf("expected b.example.com to be dropped once its only IP expired")
	}
}

// TestDNSDomainIPs_Snapshot covers plan §2.1: Snapshot inverts to ip->domain,
// picking the most recent lastSeen on collision for determinism.
func TestDNSDomainIPs_Snapshot(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(60, 1000, 16)

	d.Put("older.example.com", "8.8.8.8")
	time.Sleep(2 * time.Millisecond)
	d.Put("newer.example.com", "8.8.8.8")
	d.Put("solo.example.com", "9.9.9.9")

	snap := d.Snapshot()
	if snap["8.8.8.8"] != "newer.example.com" {
		t.Errorf("expected collision to resolve to the most-recently-seen domain, got %q", snap["8.8.8.8"])
	}
	if snap["9.9.9.9"] != "solo.example.com" {
		t.Errorf("Snapshot()[9.9.9.9] = %q, want solo.example.com", snap["9.9.9.9"])
	}
}

// TestDNSDomainIPs_Clear covers plan §2.1/T-02: Clear wipes byDomain,
// ipDomains, and the truncated flag.
func TestDNSDomainIPs_Clear(t *testing.T) {
	d := newDNSDomainIPs()
	// maxDomains below dnsDomainIPsMaxDomainsMin would be clamped back to the
	// default by SetLimits, so set the cap directly to exercise a small
	// domain cap deterministically.
	d.mu.Lock()
	d.maxDomains = 2
	d.mu.Unlock()
	d.Put("a.example.com", "1.1.1.1")
	d.Put("b.example.com", "2.2.2.2")
	d.Put("c.example.com", "3.3.3.3") // rejected, sets truncated

	if !d.Truncated() {
		t.Fatalf("setup: expected truncated=true before Clear")
	}

	d.Clear()

	d.mu.RLock()
	domains := len(d.byDomain)
	refs := len(d.ipDomains)
	d.mu.RUnlock()
	if domains != 0 || refs != 0 {
		t.Errorf("expected Clear to empty byDomain/ipDomains, got domains=%d refs=%d", domains, refs)
	}
	if d.Truncated() {
		t.Errorf("expected Truncated()=false after Clear")
	}
	if got := d.IPsFor("a.example.com"); got != nil {
		t.Errorf("expected IPsFor to return nil after Clear, got %+v", got)
	}
}

// TestDNSDomainIPs_ClearedByRecordDNSEvent proves that disabling DNS query
// logging (SetDNSLoggingEnabled(false) -> ClearDNSStats) wipes this index
// too, not just the query ring/reverse cache (plan §1.6/§4 item 6 privacy).
func TestDNSDomainIPs_ClearedByRecordDNSEvent(t *testing.T) {
	svc := newTestStatisticsService(t, &fakeTrafficAccounting{})
	svc.SetDNSLoggingEnabled(true)

	svc.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: "example.com", AnswerIP: "1.2.3.4"})

	if got := svc.dns.domainIPs.IPsFor("example.com"); len(got) != 1 {
		t.Fatalf("setup: expected 1 IP recorded for example.com, got %+v", got)
	}

	svc.SetDNSLoggingEnabled(false)

	if got := svc.dns.domainIPs.IPsFor("example.com"); len(got) != 0 {
		t.Errorf("expected domainIPs index cleared after disabling query logging, got %+v", got)
	}
}

// TestDNSDomainIPs_StatsFor covers docs/ref/todo/statistics-dns-review-fixes-plan.md
// T-03: an unknown domain, a domain with normal (non-shared) IPs, a domain
// with an IP shared with another domain, and an entry past its TTL.
func TestDNSDomainIPs_StatsFor(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(60, 1000, 16)

	d.Put("solo.example.com", "1.1.1.1")
	d.Put("solo.example.com", "1.1.1.2")

	d.Put("a.example.com", "2.2.2.2")
	d.Put("b.example.com", "2.2.2.2") // shared IP between a.example.com and b.example.com

	stats := d.StatsFor([]string{"solo.example.com", "a.example.com", "b.example.com", "unknown.example.com"})

	if _, ok := stats["unknown.example.com"]; ok {
		t.Errorf("expected no entry for a domain not present in the index, got %+v", stats["unknown.example.com"])
	}

	if got := stats["solo.example.com"]; got.Count != 2 || got.Shared {
		t.Errorf("solo.example.com: expected Count=2 Shared=false, got %+v", got)
	}

	if got := stats["a.example.com"]; got.Count != 1 || !got.Shared {
		t.Errorf("a.example.com: expected Count=1 Shared=true (IP shared with b.example.com), got %+v", got)
	}
	if got := stats["b.example.com"]; got.Count != 1 || !got.Shared {
		t.Errorf("b.example.com: expected Count=1 Shared=true, got %+v", got)
	}

	// TTL expiry: an entry past its TTL must not be counted.
	d.mu.Lock()
	d.byDomain["expired.example.com"] = map[string]time.Time{
		"9.9.9.9": time.Now().Add(-2 * time.Hour),
	}
	if d.ipDomains["9.9.9.9"] == nil {
		d.ipDomains["9.9.9.9"] = make(map[string]struct{})
	}
	d.ipDomains["9.9.9.9"]["expired.example.com"] = struct{}{}
	d.mu.Unlock()

	stats = d.StatsFor([]string{"expired.example.com"})
	if got, ok := stats["expired.example.com"]; ok {
		t.Errorf("expected expired.example.com to have no live IPs left (evicted), got %+v", got)
	}
}

// TestDNSDomainIPs_DomainsForIP covers docs/ref/todo/statistics-dns-ip-filter-
// plan.md T-01: an IP shared by 2 domains must return both, an unknown IP
// returns nil, and an expired entry is neither returned nor left behind.
func TestDNSDomainIPs_DomainsForIP(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(60, 1000, 16)

	d.Put("a.example.com", "203.0.113.1")
	time.Sleep(2 * time.Millisecond)
	d.Put("b.example.com", "203.0.113.1")
	d.Put("solo.example.com", "203.0.113.2")

	got := d.DomainsForIP("203.0.113.1")
	if len(got) != 2 {
		t.Fatalf("expected 2 domains sharing 203.0.113.1, got %+v", got)
	}
	// Newest lastSeen first: b.example.com was Put after a.example.com.
	if got[0].Domain != "b.example.com" || got[1].Domain != "a.example.com" {
		t.Errorf("expected [b.example.com, a.example.com] newest-first, got %+v", got)
	}

	if got := d.DomainsForIP("203.0.113.2"); len(got) != 1 || got[0].Domain != "solo.example.com" {
		t.Errorf("expected solo.example.com for 203.0.113.2, got %+v", got)
	}

	if got := d.DomainsForIP("10.10.10.10"); got != nil {
		t.Errorf("expected nil for an unknown IP, got %+v", got)
	}

	if got := d.DomainsForIP(""); got != nil {
		t.Errorf("expected nil for an empty ip, got %+v", got)
	}

	// TTL expiry: an entry past its TTL must not be returned and must be swept.
	d.mu.Lock()
	d.byDomain["expired.example.com"] = map[string]time.Time{
		"203.0.113.3": time.Now().Add(-2 * time.Hour),
	}
	d.ipDomains["203.0.113.3"] = map[string]struct{}{"expired.example.com": {}}
	d.mu.Unlock()

	if got := d.DomainsForIP("203.0.113.3"); len(got) != 0 {
		t.Errorf("expected expired domain to be excluded, got %+v", got)
	}
	d.mu.RLock()
	_, stillExists := d.ipDomains["203.0.113.3"]
	d.mu.RUnlock()
	if stillExists {
		t.Errorf("expected expired entry to be swept from ipDomains after DomainsForIP")
	}
}

// TestDNSDomainIPs_IPsForMany covers T-01: batching IPsFor across several
// domains in one call, including a domain absent from the index.
func TestDNSDomainIPs_IPsForMany(t *testing.T) {
	d := newDNSDomainIPs()
	d.SetLimits(60, 1000, 16)

	d.Put("a.example.com", "198.51.100.1")
	d.Put("a.example.com", "198.51.100.2")
	d.Put("b.example.com", "198.51.100.1") // shared with a.example.com

	out := d.IPsForMany([]string{"a.example.com", "b.example.com", "unknown.example.com"})

	if _, ok := out["unknown.example.com"]; ok {
		t.Errorf("expected no entry for a domain absent from the index, got %+v", out["unknown.example.com"])
	}

	aIPs := out["a.example.com"]
	if len(aIPs) != 2 {
		t.Fatalf("expected 2 IPs for a.example.com, got %+v", aIPs)
	}
	for _, e := range aIPs {
		if e.IP == "198.51.100.1" && !e.Shared {
			t.Errorf("expected 198.51.100.1 marked shared, got %+v", e)
		}
		if e.IP == "198.51.100.2" && e.Shared {
			t.Errorf("expected 198.51.100.2 not shared, got %+v", e)
		}
	}

	bIPs := out["b.example.com"]
	if len(bIPs) != 1 || bIPs[0].IP != "198.51.100.1" || !bIPs[0].Shared {
		t.Errorf("expected b.example.com to have 1 shared IP, got %+v", bIPs)
	}
}

// TestDNSDomainIPs_Race exercises Put/IPsFor/Snapshot/SetLimits/sweepExpired
// concurrently under -race.
func TestDNSDomainIPs_Race(t *testing.T) {
	d := newDNSDomainIPs()
	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(4)
	go func() {
		defer wg.Done()
		for i := 0; ; i++ {
			select {
			case <-stop:
				return
			default:
				d.Put("domain.example.com", ipFromInt(i%1000))
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				d.IPsFor("domain.example.com")
			}
		}
	}()
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
				d.Snapshot()
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
				d.SetLimits(60+(i%10), 1000, 2+(i%10))
				d.sweepExpired()
			}
		}
	}()

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}
