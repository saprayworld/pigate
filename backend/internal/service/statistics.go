package service

import (
	"net/netip"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// Statistics page pipeline (docs/ref/todo/statistics-page-plan.md) — composes
// the /api/statistics/traffic response from two independent sources that
// already exist:
//   - TrafficStatsService's bucket ring (byte deltas, via GetTrafficBreakdown)
//     for Top Source Hosts / Top Destinations / Top Conversations.
//   - This service's own RAM-only "deny ring" (event counts, fed by
//     RecordFirewallLog from the NFLOG watcher) for Top Denied Sources/Ports.
//
// The deny ring is kept separate from TrafficStatsService's byte buckets
// deliberately (plan §2): different source (NFLOG, not conntrack), different
// unit (event count, not bytes), and it must never be scanned from the ring
// buffer of raw log entries on every request — RecordFirewallLog does O(1)
// counting as each event arrives instead (plan Caution 4).
const (
	// denyBucketSpan/denyBucketMax mirror trafficDetailBucketSpan/
	// trafficDetailBucketMax so the deny ring covers the same 24h window at
	// the same 5-minute granularity.
	denyBucketSpan = 5 * time.Minute
	denyBucketMax  = 288

	// maxTrackedDenySources/maxTrackedDenyPorts bound the deny ring's
	// per-bucket maps the same way maxTrackedHosts/maxTrackedDests bound the
	// byte buckets (plan §2 T-03) — a scan/flood can't grow either map
	// without limit. eventsCount (below) is tracked unconditionally
	// (uncapped) so DeniedEvents always reflects the true number of DROP
	// entries seen, even once a bucket's src/port maps have hit their caps.
	maxTrackedDenySources = 500
	maxTrackedDenyPorts   = 300
)

// deniedBucket is one 5-minute bucket of DROP-event counts, keyed by source
// IP and by "<PROTO>/<PORT>". RAM-only, never persisted (plan Caution 9).
type deniedBucket struct {
	ts        string
	srcCount  map[string]uint64
	portCount map[string]uint64
	events    uint64
}

// StatisticsService composes the Statistics page response. It holds no
// kernel handle of its own — all data comes through TrafficStatsService (for
// bytes) and repo/dhcp (for hostname enrichment, same as TrafficStatsService
// itself uses), plus the deny ring below (fed by RecordFirewallLog).
type StatisticsService struct {
	traffic *TrafficStatsService
	repo    *db.Repository
	dhcp    kernel.DhcpManager

	mu          sync.RWMutex
	denyBuckets []deniedBucket

	// dns holds the "Top Queried Domains" ring + reverse cache (docs/ref/todo/
	// statistics-dns-top-domain-plan.md T-07/T-08) — see dns_query_stats.go /
	// dns_reverse_cache.go. Never nil (always constructed in
	// NewStatisticsService), so RecordDNSEvent/GetStatistics never need a
	// nil-check.
	dns *dnsQueryStats
}

// NewStatisticsService constructs the service. traffic must be the same
// *TrafficStatsService instance main.go already started (this service adds
// no ticker/goroutine of its own — plan T-06).
//
// maxDNSPairs/maxDNSClients are the per-bucket DNS query-stats caps (see
// dns_query_stats.go). In production they come from the bootstrap config
// keys dns-stats-max-pairs / dns-stats-max-clients, resolved by
// internal/config and wired through by cmd/pigate/main.go — this package
// deliberately does not import internal/config itself, to keep the
// service layer decoupled from the bootstrap config format. A <= 0 value is
// defense-in-depth for direct callers (tests, future call sites) and falls
// back to defaultMaxTrackedDNSPairs/defaultMaxTrackedDNSClients; the
// authoritative range validation lives in config.Resolve, not here.
func NewStatisticsService(traffic *TrafficStatsService, repo *db.Repository, dhcp kernel.DhcpManager, maxDNSPairs, maxDNSClients int) *StatisticsService {
	if maxDNSPairs <= 0 {
		maxDNSPairs = defaultMaxTrackedDNSPairs
	}
	if maxDNSClients <= 0 {
		maxDNSClients = defaultMaxTrackedDNSClients
	}
	return &StatisticsService{
		traffic: traffic,
		repo:    repo,
		dhcp:    dhcp,
		dns: &dnsQueryStats{
			reverseCache: newDNSReverseCache(),
			maxPairs:     maxDNSPairs,
			maxClients:   maxDNSClients,
		},
	}
}

// RecordFirewallLog is the NFLOG-watcher hook (main.go stampAndPush, plan
// T-06): it must be O(1), never block, never do I/O, and never panic — it
// runs directly on the NFLOG read loop, and any of those would make packet
// logging itself stall (plan Caution 4). Only DROP entries are counted;
// PASS/AUDIT entries are ignored (this card is specifically "Top Denied").
func (s *StatisticsService) RecordFirewallLog(entry model.FirewallLog) {
	if entry.Action != "DROP" {
		return
	}

	ts := time.Now().Truncate(denyBucketSpan).Format(time.RFC3339)
	portKey := entry.Proto + "/" + entry.Port

	s.mu.Lock()
	defer s.mu.Unlock()

	var b *deniedBucket
	if n := len(s.denyBuckets); n > 0 && s.denyBuckets[n-1].ts == ts {
		b = &s.denyBuckets[n-1]
	} else {
		s.denyBuckets = append(s.denyBuckets, deniedBucket{
			ts:        ts,
			srcCount:  make(map[string]uint64),
			portCount: make(map[string]uint64),
		})
		if len(s.denyBuckets) > denyBucketMax {
			s.denyBuckets = s.denyBuckets[len(s.denyBuckets)-denyBucketMax:]
		}
		b = &s.denyBuckets[len(s.denyBuckets)-1]
	}

	b.events++
	if _, exists := b.srcCount[entry.Src]; exists || len(b.srcCount) < maxTrackedDenySources {
		b.srcCount[entry.Src]++
	}
	if _, exists := b.portCount[portKey]; exists || len(b.portCount) < maxTrackedDenyPorts {
		b.portCount[portKey]++
	}
}

// denySnapshot aggregates the deny ring's src/port counts over the requested
// window, mirroring GetTrafficBreakdown's bucket-selection logic
// (trafficWindow1hBuckets trailing buckets for "1h", the whole ring for
// "24h").
func (s *StatisticsService) denySnapshot(window string) (srcTotals, portTotals map[string]uint64, totalEvents uint64, truncated bool) {
	srcTotals = make(map[string]uint64)
	portTotals = make(map[string]uint64)

	s.mu.RLock()
	defer s.mu.RUnlock()

	var windowBuckets []deniedBucket
	if window == trafficWindow1h {
		n := trafficWindow1hBuckets
		if len(s.denyBuckets) < n {
			n = len(s.denyBuckets)
		}
		windowBuckets = s.denyBuckets[len(s.denyBuckets)-n:]
	} else {
		windowBuckets = s.denyBuckets
	}

	for _, b := range windowBuckets {
		for k, v := range b.srcCount {
			srcTotals[k] += v
		}
		for k, v := range b.portCount {
			portTotals[k] += v
		}
		totalEvents += b.events
		if len(b.srcCount) >= maxTrackedDenySources || len(b.portCount) >= maxTrackedDenyPorts {
			truncated = true
		}
	}
	return
}

// GetStatistics composes the /api/statistics/traffic response for the given
// window ("1h" default, or "24h"). window must already be whitelisted by the
// caller (the API handler) — this method only re-validates defensively.
func (s *StatisticsService) GetStatistics(window string) model.TrafficStatistics {
	if window != trafficWindow24h {
		window = trafficWindow1h
	}

	breakdown := s.traffic.GetTrafficBreakdown(window)
	leaseByIP, resByIP := s.traffic.hostLookup()

	srcTotals, portTotals, deniedEvents, denyTruncated := s.denySnapshot(window)

	domainTotals, typeByDomain, dnsQueries, dnsLoggingEnabled, dnsTruncated := s.domainSnapshot(window)

	// ipDomain enrichment (T-08): resolved once per request (one RLock) from
	// every IP that could appear in TopSources/TopDestinations/
	// TopConversations. When query logging is off / the cache is empty this
	// is just an empty map, so buildTopHosts/buildTopConversations fall back
	// to their pre-existing behavior byte-for-byte (regression guard, plan
	// §5 item 12 / T-11 item 11).
	ips := make([]string, 0, len(breakdown.Hosts)+len(breakdown.Dests))
	for ip := range breakdown.Hosts {
		ips = append(ips, ip)
	}
	for ip := range breakdown.Dests {
		ips = append(ips, ip)
	}
	ipDomain := s.dns.reverseCache.LookupMany(ips)

	return model.TrafficStatistics{
		Window:            window,
		ObservedBytes:     breakdown.Observed,
		Accuracy:          breakdown.Accuracy,
		TopSources:        buildTopHosts(breakdown.Hosts, breakdown.Observed, leaseByIP, resByIP, ipDomain, statsTopN),
		TopDestinations:   buildTopHosts(breakdown.Dests, breakdown.Observed, leaseByIP, resByIP, ipDomain, statsTopN),
		TopConversations:  buildTopConversations(breakdown.Convs, breakdown.Observed, leaseByIP, resByIP, ipDomain),
		DeniedSources:     buildTopDeniedSources(srcTotals, deniedEvents, leaseByIP, resByIP),
		DeniedPorts:       buildTopDeniedPorts(portTotals, deniedEvents),
		DeniedSampled:     true,
		DeniedEvents:      deniedEvents,
		Truncated:         breakdown.Truncated || denyTruncated,
		TopDomains:        buildTopDomains(domainTotals, typeByDomain, dnsQueries),
		DNSQueries:        dnsQueries,
		DNSLoggingEnabled: dnsLoggingEnabled,
		DNSTruncated:      dnsTruncated,
		Series:            breakdown.Series,
		GeneratedAt:       time.Now().UTC().Format(time.RFC3339),
	}
}

// buildTopHosts ranks a byte-total map (either breakdown.Hosts or
// breakdown.Dests — same shape) into the Top Source Hosts / Top Destinations
// card rows, cut to at most limit rows. Sort is deterministic (bytes desc,
// then IP asc) so tests never flake on map iteration order, mirroring
// buildTopTalkers. This is the ONE ranking/sorting implementation shared by
// GetStatistics (which always passes statsTopN, keeping /api/statistics/traffic
// byte-for-byte unchanged) and GetTrafficTopHosts (statistics_traffic.go,
// which passes the caller's clamped limit — docs/ref/todo/
// statistics-traffic-page-plan.md T-03).
//
// bytesUp/bytesDown are always the flow's own orig/reply direction (Orig ->
// up, Reply -> down) regardless of whether ip is the flow's SrcIP or DstIP —
// i.e. "down" consistently means the download-heavy reply traffic (e.g. a
// file fetched from an internet destination), matching Top Conversations.
// Ranking/Bytes/Percent are always v.Total().
func buildTopHosts(totals map[string]dirBytes, observed uint64, leaseByIP map[string]model.ActiveDhcpLease, resByIP map[string]model.DhcpReservation, ipDomain map[string]string, limit int) []model.TopHost {
	out := make([]model.TopHost, 0, len(totals))
	for ip, v := range totals {
		bytes := v.Total()
		if bytes == 0 {
			continue
		}
		up, down := v.Orig, v.Reply
		hostname, mac := hostnameFor(ip, leaseByIP, resByIP)
		out = append(out, model.TopHost{
			IP:        ip,
			Hostname:  hostname,
			MAC:       mac,
			Bytes:     bytes,
			Percent:   percentOf(bytes, observed),
			BytesUp:   up,
			BytesDown: down,
			Private:   isPrivateIP(ip),
			Domain:    ipDomain[ip],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}

// buildTopConversations turns breakdown.Convs (keyed by convKey: "src|dst|proto|dstPort")
// back into display rows. A key that fails to parse (should never happen —
// convKey is the only writer) is skipped defensively rather than panicking.
func buildTopConversations(totals map[string]dirBytes, observed uint64, leaseByIP map[string]model.ActiveDhcpLease, resByIP map[string]model.DhcpReservation, ipDomain map[string]string) []model.TopConversation {
	out := make([]model.TopConversation, 0, len(totals))
	for key, v := range totals {
		bytes := v.Total()
		if bytes == 0 {
			continue
		}
		parts := strings.SplitN(key, "|", 4)
		if len(parts) != 4 {
			continue
		}
		srcIP, dstIP, proto, portStr := parts[0], parts[1], parts[2], parts[3]
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			continue
		}
		srcHostname, _ := hostnameFor(srcIP, leaseByIP, resByIP)
		dstHostname, _ := hostnameFor(dstIP, leaseByIP, resByIP)
		out = append(out, model.TopConversation{
			SrcIP:       srcIP,
			SrcHostname: srcHostname,
			DstIP:       dstIP,
			DstHostname: dstHostname,
			Proto:       proto,
			DstPort:     uint16(port),
			Bytes:       bytes,
			Percent:     percentOf(bytes, observed),
			BytesUp:     v.Orig,
			BytesDown:   v.Reply,
			DstDomain:   ipDomain[dstIP],
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Bytes != out[j].Bytes {
			return out[i].Bytes > out[j].Bytes
		}
		if out[i].SrcIP != out[j].SrcIP {
			return out[i].SrcIP < out[j].SrcIP
		}
		if out[i].DstIP != out[j].DstIP {
			return out[i].DstIP < out[j].DstIP
		}
		return out[i].DstPort < out[j].DstPort
	})
	if len(out) > statsTopN {
		out = out[:statsTopN]
	}
	return out
}

// buildTopDeniedSources ranks the deny ring's per-source event counts.
// Percent is against totalEvents (this window's actual DROP-event count),
// never against ObservedBytes — the two are different units (plan Caution 3).
func buildTopDeniedSources(totals map[string]uint64, totalEvents uint64, leaseByIP map[string]model.ActiveDhcpLease, resByIP map[string]model.DhcpReservation) []model.TopDeniedSource {
	out := make([]model.TopDeniedSource, 0, len(totals))
	for ip, count := range totals {
		if count == 0 {
			continue
		}
		hostname, _ := hostnameFor(ip, leaseByIP, resByIP)
		out = append(out, model.TopDeniedSource{
			IP:       ip,
			Hostname: hostname,
			Count:    count,
			Percent:  percentOf(count, totalEvents),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].IP < out[j].IP
	})
	if len(out) > statsTopN {
		out = out[:statsTopN]
	}
	return out
}

// buildTopDeniedPorts ranks the deny ring's per-"proto/port" event counts.
func buildTopDeniedPorts(totals map[string]uint64, totalEvents uint64) []model.TopDeniedPort {
	out := make([]model.TopDeniedPort, 0, len(totals))
	for key, count := range totals {
		if count == 0 {
			continue
		}
		proto, port := key, "-"
		if idx := strings.IndexByte(key, '/'); idx >= 0 {
			proto, port = key[:idx], key[idx+1:]
		}
		out = append(out, model.TopDeniedPort{
			Proto:   proto,
			Port:    port,
			Count:   count,
			Percent: percentOf(count, totalEvents),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		if out[i].Proto != out[j].Proto {
			return out[i].Proto < out[j].Proto
		}
		return out[i].Port < out[j].Port
	})
	if len(out) > statsTopN {
		out = out[:statsTopN]
	}
	return out
}

// isPrivateIP reports whether ip (a plain IP string, no port) is RFC1918/
// link-local/loopback/ULA — a LAN address (plan §3 T-03: "ห้ามเขียน regex
// เอง", use net/netip's own classifiers). An unparseable string (e.g. "-",
// the NFLOG parser's placeholder for a truncated packet) is treated as not
// private, since it isn't a usable address either way.
func isPrivateIP(ip string) bool {
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return false
	}
	return addr.IsPrivate() || addr.IsLoopback() || addr.IsLinkLocalUnicast()
}
