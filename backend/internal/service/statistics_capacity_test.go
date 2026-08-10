package service

import (
	"testing"

	"pigate/internal/model"
)

// capacityRingByID is a small test helper — GetCapacityStatistics always
// returns exactly 10 rings in a fixed order (plan §6/T-08 case 1; the 10th,
// dns.domainIpsPerDomain, added by docs/ref/todo/
// statistics-dns-cap-notification-fix-plan.md §3.4/T-06), so tests
// look up the one they care about by ID rather than hardcoding indices,
// keeping them resilient to (deliberate, documented) reordering.
func capacityRingByID(t *testing.T, rings []model.RingCapacity, id string) model.RingCapacity {
	t.Helper()
	for _, r := range rings {
		if r.ID == id {
			return r
		}
	}
	t.Fatalf("ring %q not found in %d rings", id, len(rings))
	return model.RingCapacity{}
}

// TestGetCapacityStatistics_Empty is plan T-08 case 1: an empty service must
// still return all 10 rings, every count zero, and len(series) == bucket
// count for every window.
func TestGetCapacityStatistics_Empty(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	wantIDs := []string{
		"traffic.hosts", "traffic.dests", "traffic.conversations",
		"firewall.denySources", "firewall.denyPorts",
		"dns.pairs", "dns.clients",
		"dns.reverseCache", "dns.domainIps", "dns.domainIpsPerDomain",
	}

	for _, window := range []string{"15m", "30m", "1h", "3h", "6h", "12h", "24h"} {
		got := s.GetCapacityStatistics(window, true)
		if len(got.Rings) != 10 {
			t.Fatalf("window %s: expected 10 rings, got %d", window, len(got.Rings))
		}
		for i, id := range wantIDs {
			if got.Rings[i].ID != id {
				t.Fatalf("window %s: ring order mismatch at index %d: got %q, want %q", window, i, got.Rings[i].ID, id)
			}
		}
		for _, r := range got.Rings {
			if r.Current != 0 || r.Peak != 0 || r.FullBuckets != 0 || r.Truncated {
				t.Fatalf("window %s: ring %s expected all-zero on an empty service, got %+v", window, r.ID, r)
			}
			if r.Kind == "bucket" {
				if len(r.Series) != got.BucketCount {
					t.Fatalf("window %s: ring %s: len(series)=%d, want bucketCount=%d", window, r.ID, len(r.Series), got.BucketCount)
				}
			} else if len(r.Series) != 0 {
				t.Fatalf("window %s: ring %s (flat) unexpectedly has a non-empty series", window, r.ID)
			}
		}
	}
}

// TestGetCapacityStatistics_DenySourcesFull is plan T-08 case 2: pushing more
// distinct DROP source IPs than the configured deny-sources cap must surface
// current==cap, fullBuckets>=1 and truncated=true on the firewall.denySources
// ring.
func TestGetCapacityStatistics_DenySourcesFull(t *testing.T) {
	acct := &fakeTrafficAccounting{}
	traffic := newTestTrafficStatsService(t, acct, nil)
	// A tiny cap so the test doesn't need thousands of source IPs.
	s := NewStatisticsService(traffic, traffic.repo, traffic.dhcp, defaultMaxTrackedDNSPairs, defaultMaxTrackedDNSClients, 3, defaultMaxTrackedDenyPorts)

	for i := 0; i < 5; i++ {
		s.RecordFirewallLog(model.FirewallLog{
			Action: "DROP",
			Src:    []string{"203.0.113.1", "203.0.113.2", "203.0.113.3", "203.0.113.4", "203.0.113.5"}[i],
			Proto:  "TCP", Port: "22",
		})
	}

	got := s.GetCapacityStatistics("1h", false)
	ring := capacityRingByID(t, got.Rings, "firewall.denySources")
	if ring.Cap != 3 {
		t.Fatalf("expected cap=3, got %d", ring.Cap)
	}
	if ring.Current != 3 {
		t.Fatalf("expected current==cap (3, only the first 3 distinct sources admitted), got %d", ring.Current)
	}
	if ring.FullBuckets < 1 {
		t.Fatalf("expected fullBuckets>=1, got %d", ring.FullBuckets)
	}
	if !ring.Truncated {
		t.Fatalf("expected truncated=true")
	}
	if ring.CapSource != "deny-stats-max-sources" {
		t.Fatalf("expected capSource=deny-stats-max-sources, got %q", ring.CapSource)
	}
}

// TestGetCapacityStatistics_DNSDisabled is plan T-08 case 3: with DNS query
// logging disabled, dns.pairs/dns.clients must report all-zero values — no
// leaking of past-activity counts through the capacity endpoint while the
// opt-in switch is off.
func TestGetCapacityStatistics_DNSDisabled(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	// Logging is enabled, record something, then disable — ClearDNSStats()
	// (called by SetDNSLoggingEnabled(false)) wipes the ring, but this
	// exercises the "was populated, then turned off" path rather than
	// "never populated" (a stronger regression guard).
	s.SetDNSLoggingEnabled(true)
	s.recordDomainQuery("example.com", "A", "192.168.1.50")
	s.SetDNSLoggingEnabled(false)

	got := s.GetCapacityStatistics("1h", true)
	for _, id := range []string{"dns.pairs", "dns.clients"} {
		ring := capacityRingByID(t, got.Rings, id)
		if ring.Current != 0 || ring.Peak != 0 || ring.FullBuckets != 0 {
			t.Fatalf("ring %s: expected all-zero while DNS logging disabled, got %+v", id, ring)
		}
		for _, p := range ring.Series {
			if p.Count != 0 {
				t.Fatalf("ring %s: expected zero series while DNS logging disabled, got %+v", id, ring.Series)
			}
		}
	}
}

// TestGetCapacityStatistics_ReverseCacheCurrent is plan T-08 case 4: N Puts
// into the reverse cache must surface as dns.reverseCache.current==N (a
// "flat"-kind ring, no bucket dimension).
func TestGetCapacityStatistics_ReverseCacheCurrent(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})

	ips := []string{"93.184.216.34", "142.250.80.46", "1.1.1.1", "8.8.8.8"}
	for _, ip := range ips {
		s.dns.reverseCache.Put(ip, "example.com")
	}

	got := s.GetCapacityStatistics("1h", false)
	ring := capacityRingByID(t, got.Rings, "dns.reverseCache")
	if ring.Kind != "flat" {
		t.Fatalf("expected kind=flat, got %q", ring.Kind)
	}
	if ring.Current != len(ips) {
		t.Fatalf("expected current=%d, got %d", len(ips), ring.Current)
	}
	if ring.Peak != ring.Current {
		t.Fatalf("expected peak==current for a flat ring, got peak=%d current=%d", ring.Peak, ring.Current)
	}
	if ring.FullBuckets != 0 {
		t.Fatalf("expected fullBuckets=0 for a flat ring, got %d", ring.FullBuckets)
	}
}

// TestGetCapacityStatistics_WithoutSeries is plan T-08 case 5: withSeries=false
// must omit every ring's Series (nil / not just empty), including the
// disabled-DNS branch and the flat rings.
func TestGetCapacityStatistics_WithoutSeries(t *testing.T) {
	s := newTestStatisticsService(t, &fakeTrafficAccounting{})
	s.RecordFirewallLog(model.FirewallLog{Action: "DROP", Src: "203.0.113.9", Proto: "TCP", Port: "22"})

	got := s.GetCapacityStatistics("1h", false)
	for _, r := range got.Rings {
		if r.Series != nil {
			t.Fatalf("ring %s: expected nil series when withSeries=false, got %+v", r.ID, r.Series)
		}
	}
}
