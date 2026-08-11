package service

import (
	"time"

	"pigate/internal/model"
)

// Statistics -> Capacity pipeline (docs/ref/todo/
// statistics-capacity-visibility-plan.md T-06, GitHub issue #123) — composes
// the GET /api/statistics/capacity response by calling the read-only capacity
// readers added to each of the 11 RAM-only tracking structures (T-02..T-05;
// the 10th ring, dns.domainIpsPerDomain, was added by docs/ref/todo/
// statistics-dns-cap-notification-fix-plan.md §3.4/T-06; the 11th ring,
// firewall.logBuffer, was added by docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-03, issue #134):
// TrafficStatsService.CapacityUsage (traffic.hosts/dests/conversations),
// StatisticsService.denyCapacity (firewall.denySources/denyPorts),
// StatisticsService.dnsRingCapacity (dns.pairs/dns.clients), the
// dnsReverseCache/dnsDomainIPs Usage() accessors (dns.reverseCache/
// dns.domainIps), and StatisticsService.logBuffer.Usage() (firewall.logBuffer,
// injected via SetLogBuffer — see statistics.go). Every ring/index metadata
// literal (id/group/label/capSource/entryBytes) lives HERE, in one place, so
// it can never drift from the readers it decorates.
//
// This file never mutates any of the 11 structures — every reader it calls is
// read-only (RLock only, no eviction) — and never calls GetTrafficBreakdown/
// hostLookup (that would be needless work: capacity is purely about map
// sizes, not byte totals or hostnames), and never holds more than one ring's
// lock at a time (each reader above already fully drains its own lock before
// returning). This file never imports internal/config — the firewall.logBuffer
// ring's Cap is read live from the ring buffer itself (logBuffer.Usage()),
// which was already sized from config at startup by main.go.

// capacityRingMeta is this file's single metadata table for the 11 rings —
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
		kind: "flat", capSource: "dns-stats-max-domains",
		// this ring's Current/Cap are counted in DOMAINS only (maxDomains) —
		// the per-domain IP cap (maxIPsPerDomain) has its own dedicated row
		// now, capacityMetaDNSDomainIPsPerDomain below (docs/ref/todo/
		// statistics-dns-cap-notification-fix-plan.md §3.4/T-06), so this
		// row's capSource no longer needs to reference both keys. Each
		// domain holds up to maxIPsPerDomain (ip -> time.Time) entries plus a
		// reverse ipDomains set entry per IP — averaged, each domain costs
		// roughly maxIPsPerDomain x (~15-char IP key + 24B time.Time + map
		// overhead ≈ 55B) + its own map-of-maps overhead ≈ 2000B per domain
		// at the default maxIPsPerDomain=32 (mirrors dns_domain_ips.go's own
		// "~2-4 MB worst case at defaults" comment: 1000 domains x ~2000B ≈ 2MB).
		entryBytes: 2000,
	}
	// capacityMetaDNSDomainIPsPerDomain is the 10th ring: unlike
	// capacityMetaDNSDomainIPs above (which counts domains against
	// maxDomains), this row counts the largest per-domain IP count seen
	// against maxIPsPerDomain — the cap that was previously tracked
	// internally (dns_domain_ips.go's per-Put admission check) but never
	// surfaced anywhere the user could see it hit 100% (docs/ref/todo/
	// statistics-dns-cap-notification-fix-plan.md §2.3/§3.4/T-06: this is
	// the row that lets the Capacity page confirm the exact cause of a
	// "resolved-IP list may be incomplete" warning on the DNS page).
	capacityMetaDNSDomainIPsPerDomain = capacityRingMeta{
		id: "dns.domainIpsPerDomain", group: "dns", label: "DNS resolved IPs ต่อโดเมน (สูงสุด)",
		kind: "flat", capSource: "dns-stats-max-ips-per-domain",
		// same per-entry estimate as capacityMetaTrafficHosts (~15-char IP
		// key + time.Time (24B) + map overhead ≈ 55B) — this ring's "entry"
		// is one (ip, lastSeen) pair within the single most-loaded domain.
		entryBytes: 55,
	}
	// capacityMetaLogBuffer is the 11th ring: the traffic log ring buffer
	// (internal/logs/ringbuffer.go) shared by the Forward/Local Traffic log
	// pages — forward/input/output chains all share this one buffer, so
	// Current/Cap here are for the WHOLE buffer, not per-chain (docs/ref/todo/
	// firewall-log-buffer-capacity-plan.md T-03, issue #134). CapSource points
	// at the real, editable config key (traffic-log-buffer-capacity) that
	// sizes this ring at startup — unlike the compile-time const it used to be.
	capacityMetaLogBuffer = capacityRingMeta{
		id: "firewall.logBuffer", group: "firewall", label: "Firewall Traffic Log Buffer",
		kind: "flat", capSource: "traffic-log-buffer-capacity",
		// model.FirewallLog entry: several short strings (id/time/action/src/
		// dest/ports/proto/ifaces/reason/chain, mostly <20 chars each) plus Go
		// struct/string-header overhead — estimated ~300-550B/entry per the
		// original comment in cmd/pigate/main.go; 400 is the mid-range pick
		// used for EstimatedBytes here.
		entryBytes: 400,
	}
)

// GetCapacityStatistics composes the GET /api/statistics/capacity response —
// all 10 rings, in the fixed order documented on model.CapacityStatistics.
// window must already be whitelisted by the caller (the API handler); this
// method only re-validates defensively (normalizeStatsWindow), same
// convention as every other Get*Statistics method in this package.
// withSeries=false omits every ring's Series field entirely (the DTO's
// omitempty handles the JSON side) — callers that only need the summary
// numbers (e.g. the CapacityIndicator pill polled alongside a page's main
// data) should always pass false to avoid building series arrays nobody
// reads.
func (s *StatisticsService) logBufferRing() model.RingCapacity {
	if s.logBuffer == nil {
		return flatRing(capacityMetaLogBuffer, 0, 0, false)
	}
	used, cap, oldest, _, evicted := s.logBuffer.Usage()
	r := flatRing(capacityMetaLogBuffer, used, cap, evicted > 0)
	r.OldestEntry = oldest
	return r
}

func (s *StatisticsService) GetCapacityStatistics(window string, withSeries bool) model.CapacityStatistics {
	window = normalizeStatsWindow(window)
	bucketCount := statsWindowBucketCount(window)

	traffic := s.traffic.CapacityUsage(window)
	deny := s.denyCapacity(window)
	dns, dnsEnabled := s.dnsRingCapacity(window)
	_ = dnsEnabled // dnsRingCapacity already zeroes everything when disabled (privacy) — no extra branching needed here

	reverseCacheSize, reverseCacheMax := s.dns.reverseCache.Usage()
	domainIPsDomains, domainIPsMaxDomains, domainIPsMaxIPsPerDomain, domainIPsMaxIPsUsed, domainIPsDomainsAtIPCap, domainIPsIndexTruncated := s.dns.domainIPs.Usage()

	rings := []model.RingCapacity{
		bucketRing(capacityMetaTrafficHosts, traffic.Hosts, s.traffic.maxTrackedHosts, withSeries),
		bucketRing(capacityMetaTrafficDests, traffic.Dests, s.traffic.maxTrackedDests, withSeries),
		bucketRing(capacityMetaTrafficConvs, traffic.Convs, s.traffic.maxTrackedConversations, withSeries),
		bucketRing(capacityMetaDenySources, deny.First, s.maxTrackedDenySources, withSeries),
		bucketRing(capacityMetaDenyPorts, deny.Second, s.maxTrackedDenyPorts, withSeries),
		bucketRing(capacityMetaDNSPairs, dns.First, s.dns.maxPairs, withSeries),
		bucketRing(capacityMetaDNSClients, dns.Second, s.dns.maxClients, withSeries),
		flatRing(capacityMetaDNSReverseCache, reverseCacheSize, reverseCacheMax, reverseCacheSize >= reverseCacheMax && reverseCacheMax > 0),
		flatRing(capacityMetaDNSDomainIPs, domainIPsDomains, domainIPsMaxDomains, domainIPsIndexTruncated),
		// 10th ring: the per-domain IP cap (maxIPsPerDomain), which used to be
		// silently discarded here (see the removed `_ = domainIPsMaxIPsPerDomain`
		// this replaced) — Current is the largest per-domain IP count seen
		// anywhere in the index right now, Truncated is true when at least one
		// domain is AT the cap (docs/ref/todo/
		// statistics-dns-cap-notification-fix-plan.md §3.4/T-06).
		flatRing(capacityMetaDNSDomainIPsPerDomain, domainIPsMaxIPsUsed, domainIPsMaxIPsPerDomain, domainIPsDomainsAtIPCap > 0),
		// 11th ring: the traffic log ring buffer. s.logBuffer is nil unless
		// SetLogBuffer was called (main.go wires it after construction, mirroring
		// SetPolicyStatsService) — degrade gracefully to an all-zero row rather
		// than panicking if some caller (e.g. a unit test) built StatisticsService
		// without it.
		s.logBufferRing(),
	}

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
