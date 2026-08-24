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
	"pigate/internal/logs"
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

	// defaultMaxTrackedDenySources/defaultMaxTrackedDenyPorts are the
	// fallback values used when NewStatisticsService is passed a <=0 value
	// (defense-in-depth for direct callers — tests, future call sites —
	// mirroring dns_query_stats.go's defaultMaxTrackedDNSPairs pattern). The
	// authoritative range validation for the config-file-sourced production
	// values lives in config.Resolve, not here. Kept in sync with
	// config.Defaults()'s DenyStatsMaxSources/DenyStatsMaxPorts (docs/ref/todo/
	// statistics-capacity-visibility-plan.md T-14 — these used to be the
	// maxTrackedDenySources/maxTrackedDenyPorts consts directly; promoted to
	// per-service fields so an operator can raise them via
	// deny-stats-max-sources/-ports without a rebuild, same pattern
	// TrafficStatsService's maxTrackedHosts/etc. already follows).
	//
	// eventsCount (below) is tracked unconditionally (uncapped) so
	// DeniedEvents always reflects the true number of DROP entries seen, even
	// once a bucket's src/port maps have hit their caps.
	defaultMaxTrackedDenySources = 500
	defaultMaxTrackedDenyPorts   = 300
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

	// maxTrackedDenySources/maxTrackedDenyPorts are the effective per-bucket
	// caps, set once at construction from the file-only
	// deny-stats-max-sources/-ports config keys (or
	// defaultMaxTrackedDenySources/defaultMaxTrackedDenyPorts when the caller
	// passes <=0 — see NewStatisticsService). Read-only after construction,
	// so no mutex guards these (same pattern as
	// TrafficStatsService.maxTrackedHosts/dnsQueryStats.maxPairs).
	maxTrackedDenySources int
	maxTrackedDenyPorts   int

	// dns holds the "Top Queried Domains" ring + reverse cache (docs/ref/todo/
	// statistics-dns-top-domain-plan.md T-07/T-08) — see dns_query_stats.go /
	// dns_reverse_cache.go. Never nil (always constructed in
	// NewStatisticsService), so RecordDNSEvent/GetStatistics never need a
	// nil-check.
	dns *dnsQueryStats

	// dnsServerManager is optional (nil until SetDNSServerManager is called
	// by main.go, mirroring SetLogBuffer's post-construction wiring pattern
	// below) — used only by SetBlocklists (dns_query_stats.go, docs/ref/todo/
	// dns-blocklist-import-plan.md T-06) to STREAM each enabled blocklist's
	// <id>.hosts file back off disk (via kernel.StreamBlocklistFile +
	// model.ParseHostsFileDomains) when rebuilding the RAM-only
	// dnsBlocklistIndex, instead of holding up to 500,000 domains as a
	// []string. Not a NewStatisticsService parameter, to avoid growing that
	// constructor's already-7-parameter signature further (same reasoning as
	// logBuffer).
	dnsServerManager kernel.DNSServerManager

	// logBuffer is optional (nil until SetLogBuffer is called by main.go,
	// mirroring api.Server.SetPolicyStatsService's pattern) — it feeds the
	// firewall.logBuffer ring in GetCapacityStatistics (docs/ref/todo/
	// firewall-log-buffer-capacity-plan.md T-03, issue #134). NOT added as a
	// NewStatisticsService parameter, to avoid growing that constructor's
	// already-7-parameter signature further; wired via setter instead, same
	// as SetPolicyStatsService.
	logBuffer *logs.RingBuffer
}

// SetLogBuffer wires the traffic log ring buffer into the service after
// construction (main.go calls this once, right after NewStatisticsService,
// mirroring api.Server.SetPolicyStatsService — docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-03/T-05, issue #134). Safe to call
// with nil; GetCapacityStatistics degrades to an all-zero firewall.logBuffer
// row when logBuffer is nil (e.g. a unit test that never calls this).
func (s *StatisticsService) SetLogBuffer(rb *logs.RingBuffer) {
	s.logBuffer = rb
}

// SetDNSServerManager wires the kernel.DNSServerManager handle used by
// SetBlocklists to stream <id>.hosts files back off disk (docs/ref/todo/
// dns-blocklist-import-plan.md T-06). Called once by main.go, mirroring
// SetLogBuffer's post-construction pattern. Safe to leave unset (nil) —
// SetBlocklists then simply skips every list it's handed (a direct caller,
// e.g. a unit test, that never calls this still gets a working, empty
// dnsBlocklistIndex).
func (s *StatisticsService) SetDNSServerManager(m kernel.DNSServerManager) {
	s.dnsServerManager = m
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
//
// maxDenySources/maxDenyPorts are the deny ring's per-bucket caps (docs/ref/
// todo/statistics-capacity-visibility-plan.md T-14) — same <=0 fallback
// convention (defaultMaxTrackedDenySources/defaultMaxTrackedDenyPorts),
// sourced in production from deny-stats-max-sources/-ports.
func NewStatisticsService(traffic *TrafficStatsService, repo *db.Repository, dhcp kernel.DhcpManager, maxDNSPairs, maxDNSClients, maxDenySources, maxDenyPorts int) *StatisticsService {
	if maxDNSPairs <= 0 {
		maxDNSPairs = defaultMaxTrackedDNSPairs
	}
	if maxDNSClients <= 0 {
		maxDNSClients = defaultMaxTrackedDNSClients
	}
	if maxDenySources <= 0 {
		maxDenySources = defaultMaxTrackedDenySources
	}
	if maxDenyPorts <= 0 {
		maxDenyPorts = defaultMaxTrackedDenyPorts
	}
	return &StatisticsService{
		traffic:               traffic,
		repo:                  repo,
		dhcp:                  dhcp,
		maxTrackedDenySources: maxDenySources,
		maxTrackedDenyPorts:   maxDenyPorts,
		dns: &dnsQueryStats{
			reverseCache: newDNSReverseCache(),
			domainIPs:    newDNSDomainIPs(),
			maxPairs:     maxDNSPairs,
			maxClients:   maxDNSClients,
			blockIndex:   &dnsBlockIndex{},
			// blocklistIndex backs the bulk-import blocklist statistics
			// feature (dns_blocklist_index.go, T-06) — a zero-value
			// dnsBlocklistIndex (atomic.Pointer defaults to nil) starts
			// Empty() until the first successful SetBlocklists call, exactly
			// like blockIndex above.
			blocklistIndex: &dnsBlocklistIndex{},
			// Set to the package default here; main.go raises it to the
			// file-only dns-stats-max-blocked-domains config value via
			// SetBlockedStatsLimit right after construction (mirrors
			// SetLogBuffer's post-construction wiring pattern) — a direct
			// caller (tests) that never calls SetBlockedStatsLimit still
			// gets a sane, non-zero cap.
			maxBlockedDomains: defaultMaxTrackedBlockedDomains,
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
	if _, exists := b.srcCount[entry.Src]; exists || len(b.srcCount) < s.maxTrackedDenySources {
		b.srcCount[entry.Src]++
	}
	if _, exists := b.portCount[portKey]; exists || len(b.portCount) < s.maxTrackedDenyPorts {
		b.portCount[portKey]++
	}
}

// denySnapshot aggregates the deny ring's src/port counts over the requested
// window, mirroring GetTrafficBreakdown's bucket-selection logic (the
// statsWindowBucketCount trailing buckets for window — see traffic_stats.go's
// statsWindowBuckets table).
func (s *StatisticsService) denySnapshot(window string) (srcTotals, portTotals map[string]uint64, totalEvents uint64, truncated bool) {
	srcTotals = make(map[string]uint64)
	portTotals = make(map[string]uint64)

	s.mu.RLock()
	defer s.mu.RUnlock()

	windowBuckets := lastNBuckets(s.denyBuckets, statsWindowBucketCount(window))

	for _, b := range windowBuckets {
		for k, v := range b.srcCount {
			srcTotals[k] += v
		}
		for k, v := range b.portCount {
			portTotals[k] += v
		}
		totalEvents += b.events
		if len(b.srcCount) >= s.maxTrackedDenySources || len(b.portCount) >= s.maxTrackedDenyPorts {
			truncated = true
		}
	}
	return
}

// denyCapacity is the read-only capacity reader behind the
// "firewall.denySources"/"firewall.denyPorts" rows of GET
// /api/statistics/capacity (docs/ref/todo/
// statistics-capacity-visibility-plan.md T-03) — mirrors denySnapshot's
// RLock/bucket-selection/axis-before-lock structure exactly, but reads
// per-bucket map LENGTHS instead of aggregating event counts. denySnapshot
// itself is left completely untouched (regression guard: it backs
// /api/statistics/traffic's DeniedSources/DeniedPorts, must stay
// byte-for-byte unchanged).
func (s *StatisticsService) denyCapacity(window string) pairRingUsage {
	window = normalizeStatsWindow(window)

	axisStart, n := statsSeriesAxis(window)
	srcSeries := make([]model.CapacityPoint, n)
	portSeries := make([]model.CapacityPoint, n)
	for i := 0; i < n; i++ {
		ts := axisStart.Add(time.Duration(i) * denyBucketSpan).Format(time.RFC3339)
		srcSeries[i].Ts = ts
		portSeries[i].Ts = ts
	}

	var src, port ringUsage

	s.mu.RLock()
	for _, b := range s.denyBuckets {
		src.TotalEntries += int64(len(b.srcCount))
		port.TotalEntries += int64(len(b.portCount))
	}

	windowBuckets := lastNBuckets(s.denyBuckets, n)
	for i, b := range windowBuckets {
		sLen, pLen := len(b.srcCount), len(b.portCount)

		idx := statsSeriesIndex(b.ts, axisStart, n)
		srcSeries[idx].Count += sLen
		portSeries[idx].Count += pLen

		if sLen > src.Peak {
			src.Peak = sLen
		}
		if pLen > port.Peak {
			port.Peak = pLen
		}
		if sLen >= s.maxTrackedDenySources {
			src.FullBuckets++
		}
		if pLen >= s.maxTrackedDenyPorts {
			port.FullBuckets++
		}
		if i == len(windowBuckets)-1 {
			src.Current, port.Current = sLen, pLen
		}
	}
	s.mu.RUnlock()

	src.Series, port.Series = srcSeries, portSeries
	return pairRingUsage{First: src, Second: port}
}

// GetStatistics composes the /api/statistics/traffic response for the given
// window ("1h" default, or "24h"). window must already be whitelisted by the
// caller (the API handler) — this method only re-validates defensively.
func (s *StatisticsService) GetStatistics(window string) model.TrafficStatistics {
	window = normalizeStatsWindow(window)

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
