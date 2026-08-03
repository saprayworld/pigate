package service

import (
	"sort"
	"strconv"
	"strings"
	"time"

	"pigate/internal/model"
)

// Statistics -> Traffic page pipeline (docs/ref/todo/
// statistics-traffic-page-plan.md T-03) — composes the two new endpoints,
// GET /api/statistics/traffic/hosts and GET /api/statistics/traffic/host,
// from the SAME sources GetStatistics already reads (statistics.go):
// s.traffic.GetTrafficBreakdown(window) for byte totals, s.traffic.hostLookup()
// for DHCP hostname enrichment, and s.dns.reverseCache for domain enrichment.
// Neither method here calls the kernel, spawns a goroutine, or takes a lock
// of its own — GetTrafficBreakdown already does its own locking internally
// (plan T-03: "safe to call from an HTTP handler").
const (
	// trafficTopHostsDefaultLimit/trafficTopHostsMaxLimit bound the `limit`
	// query param GetTrafficTopHosts accepts (plan §1.2: "limit (default
	// 100, hard max 500 for lists ... whitelisted server-side").
	trafficTopHostsDefaultLimit = 100
	trafficTopHostsMaxLimit     = 500

	// trafficHostDetailDefaultLimit/trafficHostDetailMaxLimit bound the
	// `limit` query param GetTrafficHostDetail applies independently to each
	// of AsSource/AsDestination (plan §1.2: "300 for a drill-down's
	// conversations").
	trafficHostDetailDefaultLimit = 100
	trafficHostDetailMaxLimit     = 300
)

// clampLimit normalizes a caller-supplied limit to [1, max], substituting
// def when limit is <= 0 (the "missing/unparseable" case the HTTP handler
// already reduces an invalid limit to before it ever reaches this layer —
// this is defense-in-depth for any other caller, e.g. tests).
func clampLimit(limit, def, max int) int {
	if limit <= 0 {
		limit = def
	}
	if limit > max {
		limit = max
	}
	return limit
}

// parseConvKey splits one TrafficBreakdown.Convs/convBytes key (convKey's
// output, "src|dst|proto|port") back into its parts. Shared by
// GetTrafficHostDetail below and getTrafficBreakdown's per-IP series scan in
// traffic_stats.go (docs/ref/todo/statistics-traffic-bandwidth-chart-plan.md
// T-02 step 3) so the two can never drift into disagreeing about what counts
// as a malformed key — a drift there would silently break the
// sum(HostSeries)==TotalBytes invariant. ok is false for a malformed key
// (wrong segment count or an unparseable port) — callers skip the entry, the
// same defensive behavior the old inline code had.
func parseConvKey(key string) (src, dst, proto string, port uint16, ok bool) {
	parts := strings.SplitN(key, "|", 4)
	if len(parts) != 4 {
		return "", "", "", 0, false
	}
	p, err := strconv.ParseUint(parts[3], 10, 16)
	if err != nil {
		return "", "", "", 0, false
	}
	return parts[0], parts[1], parts[2], uint16(p), true
}

// GetTrafficTopHosts backs GET /api/statistics/traffic/hosts — the FULL (not
// statsTopN-cut) Top Source Hosts / Top Destinations lists, up to limit rows
// each (plan §1.2/T-03). window/limit are re-validated defensively here even
// though the HTTP handler already whitelists/clamps them.
func (s *StatisticsService) GetTrafficTopHosts(window string, limit int) model.TrafficTopHosts {
	if window != trafficWindow24h {
		window = trafficWindow1h
	}
	limit = clampLimit(limit, trafficTopHostsDefaultLimit, trafficTopHostsMaxLimit)

	breakdown := s.traffic.GetTrafficBreakdown(window)
	leaseByIP, resByIP := s.traffic.hostLookup()

	ips := make([]string, 0, len(breakdown.Hosts)+len(breakdown.Dests))
	for ip := range breakdown.Hosts {
		ips = append(ips, ip)
	}
	for ip := range breakdown.Dests {
		ips = append(ips, ip)
	}
	ipDomain := s.dns.reverseCache.LookupMany(ips)

	sources := buildTopHosts(breakdown.Hosts, breakdown.Observed, leaseByIP, resByIP, ipDomain, limit)
	destinations := buildTopHosts(breakdown.Dests, breakdown.Observed, leaseByIP, resByIP, ipDomain, limit)

	// Real-time throughput (docs/ref/todo/statistics-traffic-speed-plan.md
	// T-05) — CurrentRates() is called exactly once per request and its
	// snapshot applied to the already-built rows by IP; Sources uses the
	// by-src map, Destinations the by-dst map, matching how Hosts/Dests
	// themselves are two separate maps above. Deliberately NOT threaded
	// through buildTopHosts itself: that helper is shared with
	// StatisticsService.GetStatistics (the Overview page), which must never
	// gain these fields (plan §1.7 byte-compatibility requirement).
	rates := s.traffic.CurrentRates()
	rateSampledAt := ""
	if !rates.At.IsZero() {
		rateSampledAt = rates.At.UTC().Format(time.RFC3339)
	}
	applyRates(sources, rates.Hosts)
	applyRates(destinations, rates.Dests)

	return model.TrafficTopHosts{
		Window:        window,
		ObservedBytes: breakdown.Observed,
		Accuracy:      breakdown.Accuracy,
		Truncated:     breakdown.Truncated,
		Limit:         limit,
		Sources:       sources,
		Destinations:  destinations,
		// Series is network-wide/LAN-relative — the SAME slice
		// StatisticsService.GetStatistics copies into TrafficStatistics.Series
		// for the Overview page (plan §2.1: "ของฟรี" — no extra computation).
		Series:        breakdown.Series,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
		RateSampledAt: rateSampledAt,
	}
}

// applyRates fills RateBpsUp/RateBpsDown on each row of hosts (in place) from
// a CurrentRates() snapshot map keyed by IP, leaving both fields at their
// zero value (and therefore omitted from the JSON via omitempty) for any row
// with no matching key — e.g. a host with byte totals in the window but no
// traffic in the most recent ~10s poll.
func applyRates(hosts []model.TopHost, rates map[string]RatePair) {
	for i := range hosts {
		if r, ok := rates[hosts[i].IP]; ok {
			hosts[i].RateBpsUp = r.UpBps
			hosts[i].RateBpsDown = r.DownBps
		}
	}
}

// GetTrafficHostDetail backs GET /api/statistics/traffic/host — a single IP's
// conversations in both directions (plan §1.3/T-03). ip must already be
// normalized (netip.ParseAddr'd + .String()'d) by the caller (the HTTP
// handler) so it matches the conversation ring's key format exactly; this
// method does not re-validate it as an address, only compares it as a plain
// string against each conversation's srcIP/dstIP.
func (s *StatisticsService) GetTrafficHostDetail(window, ip string, limit int) model.TrafficHostDetail {
	if window != trafficWindow24h {
		window = trafficWindow1h
	}
	limit = clampLimit(limit, trafficHostDetailDefaultLimit, trafficHostDetailMaxLimit)

	// GetTrafficBreakdownForIP (not GetTrafficBreakdown) — Hosts/Dests/Convs/
	// Observed/Series come back identical to a plain GetTrafficBreakdown
	// call, PLUS HostSeries computed for ip in the SAME RLock/snapshot (plan
	// §2.3/T-02 step 4: "ห้ามเรียก breakdown สองรอบ").
	breakdown := s.traffic.GetTrafficBreakdownForIP(window, ip)
	leaseByIP, resByIP := s.traffic.hostLookup()

	// asSourceAll/asDestinationAll hold every matching row BEFORE the limit
	// cut, so TotalBytes/Up/Down (computed below) is always the true sum
	// over the full conversation set, never contradicted by a truncated
	// table (plan T-03 step 2).
	var asSourceAll, asDestinationAll []model.TrafficHostConversation
	var totalBytes, totalUp, totalDown uint64
	// ipsInvolved collects every IP that appears in a kept row (both sides)
	// so the single reverseCache.LookupMany call below covers exactly what
	// the response needs, same pattern as GetStatistics/GetTrafficTopHosts.
	ipsInvolved := make(map[string]struct{})

	for key, v := range breakdown.Convs {
		bytes := v.Total()
		if bytes == 0 {
			continue
		}
		srcIP, dstIP, proto, port, ok := parseConvKey(key)
		if !ok {
			continue // malformed key — should never happen, skip defensively
		}

		if srcIP == ip {
			ipsInvolved[dstIP] = struct{}{}
			totalBytes += bytes
			totalUp += v.Orig
			totalDown += v.Reply
			asSourceAll = append(asSourceAll, model.TrafficHostConversation{
				TopConversation: model.TopConversation{
					SrcIP: srcIP, DstIP: dstIP, Proto: proto, DstPort: port,
					Bytes: bytes, BytesUp: v.Orig, BytesDown: v.Reply,
				},
				Direction: "outbound",
			})
		}
		// A same-IP-both-sides row (e.g. loopback) deliberately lands in BOTH
		// lists (plan T-03 step 2) — not an else-if.
		if dstIP == ip {
			ipsInvolved[srcIP] = struct{}{}
			totalBytes += bytes
			totalUp += v.Orig
			totalDown += v.Reply
			asDestinationAll = append(asDestinationAll, model.TrafficHostConversation{
				TopConversation: model.TopConversation{
					SrcIP: srcIP, DstIP: dstIP, Proto: proto, DstPort: port,
					Bytes: bytes, BytesUp: v.Orig, BytesDown: v.Reply,
				},
				Direction: "inbound",
			})
		}
	}

	_, hasHostRow := breakdown.Hosts[ip]
	_, hasDestRow := breakdown.Dests[ip]
	found := len(asSourceAll) > 0 || len(asDestinationAll) > 0 || hasHostRow || hasDestRow

	ips := make([]string, 0, len(ipsInvolved)+1)
	ips = append(ips, ip)
	for other := range ipsInvolved {
		ips = append(ips, other)
	}
	ipDomain := s.dns.reverseCache.LookupMany(ips)

	// Percent per row is relative to totalBytes (THIS IP's own total), NOT
	// ObservedBytes (plan §1.4) — a different denominator than
	// buildTopConversations/buildTopHosts use.
	finishRows := func(rows []model.TrafficHostConversation, ownIsSrc bool) []model.TrafficHostConversation {
		for i := range rows {
			rows[i].Percent = percentOf(rows[i].Bytes, totalBytes)
			srcHostname, _ := hostnameFor(rows[i].SrcIP, leaseByIP, resByIP)
			dstHostname, _ := hostnameFor(rows[i].DstIP, leaseByIP, resByIP)
			rows[i].SrcHostname = srcHostname
			rows[i].DstHostname = dstHostname
			rows[i].DstDomain = ipDomain[rows[i].DstIP]
			if ownIsSrc {
				rows[i].PeerDomain = ipDomain[rows[i].DstIP]
			} else {
				rows[i].PeerDomain = ipDomain[rows[i].SrcIP]
			}
		}
		sort.Slice(rows, func(i, j int) bool {
			if rows[i].Bytes != rows[j].Bytes {
				return rows[i].Bytes > rows[j].Bytes
			}
			peerI, peerJ := rows[i].DstIP, rows[j].DstIP
			if !ownIsSrc {
				peerI, peerJ = rows[i].SrcIP, rows[j].SrcIP
			}
			if peerI != peerJ {
				return peerI < peerJ
			}
			return rows[i].DstPort < rows[j].DstPort
		})
		if len(rows) > limit {
			rows = rows[:limit]
		}
		return rows
	}

	asSource := finishRows(asSourceAll, true)
	asDestination := finishRows(asDestinationAll, false)
	if asSource == nil {
		asSource = make([]model.TrafficHostConversation, 0)
	}
	if asDestination == nil {
		asDestination = make([]model.TrafficHostConversation, 0)
	}

	hostname, mac := hostnameFor(ip, leaseByIP, resByIP)

	// Real-time throughput for this IP (docs/ref/todo/
	// statistics-traffic-speed-plan.md T-05) — CurrentRates() is called
	// exactly once here (not per-conversation, not a second breakdown call),
	// and its by-conv map is walked with the SAME rule TotalBytes above uses:
	// add when srcIP == ip, add AGAIN (never else-if) when dstIP == ip, so a
	// same-IP-both-sides row counts twice in both figures identically (plan
	// §2.2 "by construction, not by luck").
	var currentRateUp, currentRateDown uint64
	rateSampledAt := ""
	rates := s.traffic.CurrentRates()
	if !rates.At.IsZero() {
		rateSampledAt = rates.At.UTC().Format(time.RFC3339)
		for key, r := range rates.Convs {
			srcIP, dstIP, _, _, ok := parseConvKey(key)
			if !ok {
				continue
			}
			if srcIP == ip {
				currentRateUp += r.UpBps
				currentRateDown += r.DownBps
			}
			if dstIP == ip {
				currentRateUp += r.UpBps
				currentRateDown += r.DownBps
			}
		}
	}

	return model.TrafficHostDetail{
		IP:                ip,
		Hostname:          hostname,
		MAC:               mac,
		Domain:            ipDomain[ip],
		Private:           isPrivateIP(ip),
		Window:            window,
		Accuracy:          breakdown.Accuracy,
		Truncated:         breakdown.Truncated,
		Limit:             limit,
		Found:             found,
		TotalBytes:        totalBytes,
		TotalBytesUp:      totalUp,
		TotalBytesDown:    totalDown,
		PercentOfObserved: percentOf(totalBytes, breakdown.Observed),
		ObservedBytes:     breakdown.Observed,
		AsSource:          asSource,
		AsDestination:     asDestination,
		// Series is breakdown.HostSeries (per-IP, flow-relative), NEVER
		// breakdown.Series (network-wide, LAN-relative — that field is only
		// used by GetTrafficTopHosts above) — plan §2.2 decision 1/2, T-02
		// step 4.
		Series:             breakdown.HostSeries,
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		CurrentRateBpsUp:   currentRateUp,
		CurrentRateBpsDown: currentRateDown,
		RateSampledAt:      rateSampledAt,
	}
}
