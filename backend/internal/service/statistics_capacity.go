package service

import (
	"time"

	"pigate/internal/model"
)

// Statistics -> Capacity pipeline (docs/ref/todo/
// statistics-capacity-visibility-plan.md T-06, GitHub issue #123) — composes
// the GET /api/statistics/capacity response by calling the read-only capacity
// readers added to each of the 9 RAM-only tracking structures (T-02..T-05):
// TrafficStatsService.CapacityUsage (traffic.hosts/dests/conversations),
// StatisticsService.denyCapacity (firewall.denySources/denyPorts),
// StatisticsService.dnsRingCapacity (dns.pairs/dns.clients), and the
// dnsReverseCache/dnsDomainIPs Usage() accessors (dns.reverseCache/
// dns.domainIps). Every ring/index metadata literal (id/group/label/
// capSource/entryBytes) lives HERE, in one place, so it can never drift from
// the readers it decorates.
//
// This file never mutates any of the 9 structures — every reader it calls is
// read-only (RLock only, no eviction) — and never calls GetTrafficBreakdown/
// hostLookup (that would be needless work: capacity is purely about map
// sizes, not byte totals or hostnames), and never holds more than one ring's
// lock at a time (each reader above already fully drains its own lock before
// returning).

// capacityRingMeta is this file's single metadata table for the 9 rings —
// id/group/label/kind/capSource/entryBytes never come from anywhere else.
type capacityRingMeta struct {
	id         string
	group      string // "traffic" | "dns" | "firewall"
	label      string
	kind       string // "bucket" | "flat"
	capSource  string
	entryBytes int
}

// Per-entry RAM estimates (EntryBytes below) are rough constants — key size +
// value size + Go map bucket overhead — mirroring the style of estimate
// already used for config.go's maxTrafficStatsCap comment ("~110 bytes ≈
// 45-char key + 16B dirBytes + map overhead"). These are NEVER measured at
// runtime (no memory profiling) — purely a documented approximation so
// EstimatedBytes gives the user an order-of-magnitude sense of RAM cost, not
// an exact figure.
var (
	capacityMetaTrafficHosts = capacityRingMeta{
		id: "traffic.hosts", group: "traffic", label: "Top Source Hosts",
		kind: "bucket", capSource: "traffic-stats-max-hosts",
		// key ~15-char IP string + dirBytes (16B) + map bucket overhead ≈ 55B.
		entryBytes: 55,
	}
	capacityMetaTrafficDests = capacityRingMeta{
		id: "traffic.dests", group: "traffic", label: "Top Destinations",
		kind: "bucket", capSource: "traffic-stats-max-dests",
		entryBytes: 55,
	}
	capacityMetaTrafficConvs = capacityRingMeta{
		id: "traffic.conversations", group: "traffic", label: "Top Conversations",
		kind: "bucket", capSource: "traffic-stats-max-conversations",
		// key ~45-char "src|dst|proto|port" string + dirBytes (16B) + map
		// overhead ≈ 110B — same estimate config.go's maxTrafficStatsCap
		// comment already documents for this exact map.
		entryBytes: 110,
	}
	capacityMetaDenySources = capacityRingMeta{
		id: "firewall.denySources", group: "firewall", label: "Top Denied Sources",
		kind: "bucket", capSource: "deny-stats-max-sources",
		// key ~15-char IP string + uint64 count (8B) + map overhead ≈ 47B.
		entryBytes: 47,
	}
	capacityMetaDenyPorts = capacityRingMeta{
		id: "firewall.denyPorts", group: "firewall", label: "Top Denied Ports",
		kind: "bucket", capSource: "deny-stats-max-ports",
		// key ~10-char "PROTO/PORT" string + uint64 count (8B) + map overhead ≈ 42B.
		entryBytes: 42,
	}
	capacityMetaDNSPairs = capacityRingMeta{
		id: "dns.pairs", group: "dns", label: "DNS (domain, client) pairs",
		kind: "bucket", capSource: "dns-stats-max-pairs",
		// outer map keyed by ~30-char domain string pointing at an inner
		// map[clientIP]uint64 (~15-char key + 8B count + overhead ≈ 47B) plus
		// the outer entry's own overhead ≈ 110B total per pair.
		entryBytes: 110,
	}
	capacityMetaDNSClients = capacityRingMeta{
		id: "dns.clients", group: "dns", label: "DNS distinct clients",
		kind: "bucket", capSource: "dns-stats-max-clients",
		// key ~15-char IP string + uint64 count (8B) + map overhead ≈ 47B.
		entryBytes: 47,
	}
	capacityMetaDNSReverseCache = capacityRingMeta{
		id: "dns.reverseCache", group: "dns", label: "DNS reverse cache (IP -> domain)",
		kind: "flat", capSource: "DNS Server > Settings (DNS Cache Max Entries)",
		// key ~15-char IP string + dnsReverseEntry{domain ~30-char string
		// header (16B) + time.Time (24B) + bool} + map overhead ≈ 90B.
		entryBytes: 90,
	}
	capacityMetaDNSDomainIPs = capacityRingMeta{
		id: "dns.domainIps", group: "dns", label: "DNS domain -> resolved IP index",
		kind: "flat", capSource: "dns-stats-max-domains / dns-stats-max-ips-per-domain",
		// this ring's Current/Cap below are counted in DOMAINS, but each
		// domain holds up to maxIPsPerDomain (ip -> time.Time) entries plus a
		// reverse ipDomains set entry per IP — averaged, each domain costs
		// roughly maxIPsPerDomain x (~15-char IP key + 24B time.Time + map
		// overhead ≈ 55B) + its own map-of-maps overhead ≈ 1000B per domain
		// at the default maxIPsPerDomain=16 (mirrors dns_domain_ips.go's own
		// "~1-2 MB worst case at defaults" comment: 1000 domains x ~1000B ≈ 1MB).
		entryBytes: 1000,
	}
)

// GetCapacityStatistics composes the GET /api/statistics/capacity response —
// all 9 rings, in the fixed order documented on model.CapacityStatistics.
// window must already be whitelisted by the caller (the API handler); this
// method only re-validates defensively (normalizeStatsWindow), same
// convention as every other Get*Statistics method in this package.
// withSeries=false omits every ring's Series field entirely (the DTO's
// omitempty handles the JSON side) — callers that only need the summary
// numbers (e.g. the CapacityIndicator pill polled alongside a page's main
// data) should always pass false to avoid building series arrays nobody
// reads.
func (s *StatisticsService) GetCapacityStatistics(window string, withSeries bool) model.CapacityStatistics {
	window = normalizeStatsWindow(window)
	bucketCount := statsWindowBucketCount(window)

	traffic := s.traffic.CapacityUsage(window)
	deny := s.denyCapacity(window)
	dns, dnsEnabled := s.dnsRingCapacity(window)
	_ = dnsEnabled // dnsRingCapacity already zeroes everything when disabled (privacy) — no extra branching needed here

	reverseCacheSize, reverseCacheMax := s.dns.reverseCache.Usage()
	domainIPsDomains, domainIPsMaxDomains, domainIPsMaxIPsPerDomain, domainIPsTruncated := s.dns.domainIPs.Usage()

	rings := []model.RingCapacity{
		bucketRing(capacityMetaTrafficHosts, traffic.Hosts, s.traffic.maxTrackedHosts, withSeries),
		bucketRing(capacityMetaTrafficDests, traffic.Dests, s.traffic.maxTrackedDests, withSeries),
		bucketRing(capacityMetaTrafficConvs, traffic.Convs, s.traffic.maxTrackedConversations, withSeries),
		bucketRing(capacityMetaDenySources, deny.First, s.maxTrackedDenySources, withSeries),
		bucketRing(capacityMetaDenyPorts, deny.Second, s.maxTrackedDenyPorts, withSeries),
		bucketRing(capacityMetaDNSPairs, dns.First, s.dns.maxPairs, withSeries),
		bucketRing(capacityMetaDNSClients, dns.Second, s.dns.maxClients, withSeries),
		flatRing(capacityMetaDNSReverseCache, reverseCacheSize, reverseCacheMax, reverseCacheSize >= reverseCacheMax && reverseCacheMax > 0),
		flatRing(capacityMetaDNSDomainIPs, domainIPsDomains, domainIPsMaxDomains, domainIPsTruncated),
	}
	// dns.domainIps' Cap field is documented (capSource comment above) as
	// covering BOTH maxDomains and maxIPsPerDomain, but RingCapacity.Cap is a
	// single int — Current/Cap are domain-counted (maxDomains) here; the
	// per-domain IP cap (maxIPsPerDomain) has no separate row since this
	// index isn't a per-IP ring. Left as domainIPsMaxIPsPerDomain-independent
	// on purpose (plan T-06: "current/peak/fullBuckets/percent" only needs
	// one denominator per ring).
	_ = domainIPsMaxIPsPerDomain

	return model.CapacityStatistics{
		Window:      window,
		BucketCount: bucketCount,
		Rings:       rings,
		GeneratedAt: time.Now().UTC().Format(time.RFC3339),
	}
}

// bucketRing renders one "bucket"-kind RingCapacity row from a ringUsage
// snapshot + its configured cap. EstimatedBytes is deliberately computed from
// u.TotalEntries (the WHOLE ring currently held, up to 288 buckets / 24h),
// NOT from u.Current (the requested window's newest bucket) — see
// model.RingCapacity.EstimatedBytes' doc comment for why the two can
// legitimately disagree by orders of magnitude.
func bucketRing(meta capacityRingMeta, u ringUsage, cap int, withSeries bool) model.RingCapacity {
	r := model.RingCapacity{
		ID:             meta.id,
		Group:          meta.group,
		Label:          meta.label,
		Kind:           meta.kind,
		Cap:            cap,
		CapSource:      meta.capSource,
		Current:        u.Current,
		CurrentPercent: percentOf(uint64(u.Current), uint64(cap)),
		Peak:           u.Peak,
		PeakPercent:    percentOf(uint64(u.Peak), uint64(cap)),
		FullBuckets:    u.FullBuckets,
		Truncated:      u.FullBuckets > 0,
		EstimatedBytes: u.TotalEntries * int64(meta.entryBytes),
		EntryBytes:     meta.entryBytes,
	}
	if withSeries {
		r.Series = u.Series
	}
	return r
}

// flatRing renders one "flat"-kind RingCapacity row (the DNS reverse cache /
// domain->IP index — no bucket dimension, so Peak==Current and
// FullBuckets==0 always, and Series is never populated regardless of
// withSeries). EstimatedBytes is current*entryBytes — a flat index has no
// "whole ring vs window" distinction to make (unlike bucketRing above), its
// entire content IS "currently held".
func flatRing(meta capacityRingMeta, current, cap int, truncated bool) model.RingCapacity {
	pct := percentOf(uint64(current), uint64(cap))
	return model.RingCapacity{
		ID:             meta.id,
		Group:          meta.group,
		Label:          meta.label,
		Kind:           meta.kind,
		Cap:            cap,
		CapSource:      meta.capSource,
		Current:        current,
		CurrentPercent: pct,
		Peak:           current,
		PeakPercent:    pct,
		FullBuckets:    0,
		Truncated:      truncated,
		EstimatedBytes: int64(current) * int64(meta.entryBytes),
		EntryBytes:     meta.entryBytes,
	}
}
