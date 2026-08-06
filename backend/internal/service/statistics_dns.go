package service

import (
	"sort"
	"strings"
	"time"

	"pigate/internal/model"
)

// Statistics -> DNS page pipeline (docs/ref/todo/statistics-dns-page-revamp-plan.md
// T-04) — composes the 3 DNS statistics endpoints' responses:
//
//	GET /api/statistics/dns        -> GetDNSQueryStatistics
//	GET /api/statistics/dns/domain -> GetDNSDomainClients
//	GET /api/statistics/dns/client -> GetDNSClientDomains
//
// All three were originally in dns_query_stats.go (the plain-count-only
// version, drilldown plan T-02); moved here once the domain<->IP forward
// index (dns_domain_ips.go, T-02) and the traffic-breakdown byte join (plan
// §1.1/§1.2) were added, so dns_query_stats.go can stay focused on the ring +
// hot path + generic ranking helpers (rankTopDomains/rankDNSClients), which
// this file reuses rather than re-implementing.
//
// Locking discipline (plan §2.1/Caution 3): every method below fully drains
// s.dns.mu (the ring's lock) BEFORE touching s.dns.domainIPs or calling into
// TrafficStatsService — s.dns.mu is never held while domainIPs' own lock or
// the traffic bucket ring's lock is taken. Each method calls at most one of
// GetTrafficBreakdown/GetTrafficBreakdownForIP/GetTrafficBreakdownForDests
// and at most one hostLookup() per request (plan T-04 mandatory rule) —
// GetTrafficBreakdownForDests/GetTrafficBreakdownForIP already return
// everything GetTrafficBreakdown would (Hosts/Dests/Convs/Observed/Accuracy/
// Series are identical for a given window regardless of focus), so using the
// more specific accessor is never a second call.

// GetDNSQueryStatistics composes the /api/statistics/dns response: the two
// top-level tables (Top Domains / Top Clients), now with the byte-volume join
// (plan §2.2/T-04). window must already be whitelisted by the caller (the API
// handler) — this method only re-validates defensively.
func (s *StatisticsService) GetDNSQueryStatistics(window string) model.DNSQueryStatistics {
	window = normalizeStatsWindow(window)

	s.dns.mu.RLock()
	enabled := s.dns.enabled
	if !enabled {
		s.dns.mu.RUnlock()
		// Disabled: never touch domainIPs or the traffic breakdown (plan
		// T-04 mandatory rule / privacy) — every slice field is a non-nil
		// empty slice, never nil.
		return model.DNSQueryStatistics{
			Window:      window,
			Enabled:     false,
			TopDomains:  []model.DNSDomainStat{},
			TopClients:  []model.DNSClientStat{},
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}

	domainTotals := make(map[string]uint64)
	typeByDomain := make(map[string]string)
	clientTotals := make(map[string]uint64)
	// domainClients/clientDomains derive the distinct-client-per-domain and
	// distinct-domain-per-client counts from the SAME pairs ring data this
	// loop already walks (plan §0.1 — "derive ได้จาก map เดิม", no new source
	// of truth needed).
	domainClients := make(map[string]map[string]struct{})
	clientDomains := make(map[string]map[string]struct{})
	var totalQueries uint64
	var truncated bool

	for _, b := range s.dnsWindowBuckets(window) {
		for domain, clients := range b.pairs {
			for client, count := range clients {
				domainTotals[domain] += count
				clientTotals[client] += count
				if domainClients[domain] == nil {
					domainClients[domain] = make(map[string]struct{})
				}
				domainClients[domain][client] = struct{}{}
				if clientDomains[client] == nil {
					clientDomains[client] = make(map[string]struct{})
				}
				clientDomains[client][domain] = struct{}{}
			}
		}
		for k, v := range b.typeByDomain {
			typeByDomain[k] = v
		}
		totalQueries += b.queries
		if b.pairCount >= s.dns.maxPairs || len(b.clientCount) >= s.dns.maxClients {
			truncated = true
		}
	}
	s.dns.mu.RUnlock() // ring lock released before touching domainIPs/traffic below (plan Caution 3)

	if s.dns.domainIPs.Truncated() {
		truncated = true
	}

	// Exactly ONE traffic-breakdown call and ONE hostLookup call for this
	// whole request (plan T-04 mandatory rule).
	breakdown := s.traffic.GetTrafficBreakdown(window)
	leaseByIP, resByIP := s.traffic.hostLookup()

	// Domain -> IP join (plan §1.1): computed over EVERY domain seen in the
	// window (not just the top-N cut below), because DomainBytes is the
	// window-wide denominator for each row's bytesPercent, not just a sum of
	// the visible rows.
	domainIPRows := make(map[string][]domainIPEntry, len(domainTotals))
	domainBytes := make(map[string]dirBytes, len(domainTotals))
	var domainBytesTotal dirBytes
	for domain := range domainTotals {
		ips := s.dns.domainIPs.IPsFor(domain)
		domainIPRows[domain] = ips
		var total dirBytes
		for _, e := range ips {
			v := breakdown.Dests[e.IP]
			total.Orig += v.Orig
			total.Reply += v.Reply
		}
		domainBytes[domain] = total
		domainBytesTotal.Orig += total.Orig
		domainBytesTotal.Reply += total.Reply
	}
	domainBytesDenominator := domainBytesTotal.Total()

	// Base ranking/sort reused from the ring-only implementation
	// (rankTopDomains, dns_query_stats.go) — decorated below with the
	// clients/ipCount/sharedIps/bytes* fields the byte join adds.
	baseDomains := rankTopDomains(domainTotals, typeByDomain, totalQueries, dnsStatsTopN)
	topDomains := make([]model.DNSDomainStat, len(baseDomains))
	for i, td := range baseDomains {
		ips := domainIPRows[td.Domain]
		var shared bool
		for _, e := range ips {
			if e.Shared {
				shared = true
				break
			}
		}
		bytes := domainBytes[td.Domain]
		topDomains[i] = model.DNSDomainStat{
			TopDomain: td,
			Clients:   len(domainClients[td.Domain]),
			IPCount:   len(ips),
			SharedIPs: shared,
			Bytes:     bytes.Total(),
			BytesUp:   bytes.Orig,
			BytesDown: bytes.Reply,
			// bytesPercent denominator here is DomainBytes (the window-wide
			// sum of every domain's join-derived bytes) — NOT ObservedBytes
			// (plan §2.2/T-04: "ของโดเมนใช้ denominator = DomainBytes").
			BytesPercent: percentOf(bytes.Total(), domainBytesDenominator),
		}
	}
	if len(domainTotals) > dnsStatsTopN {
		truncated = true
	}

	// Base ranking/sort reused from rankDNSClients, decorated with
	// domains/bytes*.
	topClients := rankDNSClients(clientTotals, totalQueries, leaseByIP, resByIP, dnsStatsTopN)
	// Domain enrichment (docs/ref/todo/statistics-dns-host-domain-label-plan.md
	// T-02): ONE reverseCache.LookupMany batch over just the top-N IPs, called
	// here — after s.dns.mu.RUnlock() above — because reverseCache has its own
	// mutex, separate from the ring lock, and must never be touched while
	// s.dns.mu is held (plan Caution 3 / this file's header comment).
	ipDomain := s.dns.reverseCache.LookupMany(dnsClientStatIPs(topClients))
	for i := range topClients {
		ip := topClients[i].IP
		topClients[i].Domains = len(clientDomains[ip])
		topClients[i].Domain = ipDomain[ip]
		v := breakdown.Hosts[ip]
		topClients[i].Bytes = v.Total()
		topClients[i].BytesUp = v.Orig
		topClients[i].BytesDown = v.Reply
		// bytesPercent denominator here is ObservedBytes (the window-wide
		// network total) — NOT DomainBytes (plan §2.2/T-04: "ของ client ใช้
		// denominator = ObservedBytes"). Deliberately the opposite
		// denominator from the domain rows above — do not swap these.
		topClients[i].BytesPercent = percentOf(v.Total(), breakdown.Observed)
	}
	if len(clientTotals) > dnsStatsTopN {
		truncated = true
	}

	return model.DNSQueryStatistics{
		Window:        window,
		Enabled:       true,
		TotalQueries:  totalQueries,
		Truncated:     truncated,
		TopDomains:    topDomains,
		TopClients:    topClients,
		ObservedBytes: breakdown.Observed,
		DomainBytes:   domainBytesDenominator,
		TotalDomains:  len(domainTotals),
		TotalClients:  len(clientTotals),
		Accuracy:      breakdown.Accuracy,
		GeneratedAt:   time.Now().UTC().Format(time.RFC3339),
	}
}

// GetDNSDomainClients composes the /api/statistics/dns/domain response: for
// a single domain, the clients that queried it in the window (drilldown plan
// T-02), now with the Resolved IPs table + per-client/per-IP volume (plan
// §2.2/T-04). domain must already be validated/normalized by the caller (the
// API handler).
func (s *StatisticsService) GetDNSDomainClients(window, domain string) model.DNSDomainDrilldown {
	window = normalizeStatsWindow(window)

	s.dns.mu.RLock()
	enabled := s.dns.enabled
	if !enabled {
		s.dns.mu.RUnlock()
		return model.DNSDomainDrilldown{
			Domain:      domain,
			Window:      window,
			Enabled:     false,
			Clients:     []model.DNSClientStat{},
			IPs:         []model.DNSDomainIP{},
			Series:      []model.BandwidthPoint{},
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}

	clientTotals := make(map[string]uint64)
	// clientDomainsAll tracks, for every client seen in this window (not just
	// the ones that queried `domain`), the full set of domains it queried —
	// same union-across-buckets/across-domains pattern as GetDNSQueryStatistics's
	// clientDomains and GetDNSClientDomains' domainClients (docs/ref/todo/
	// statistics-dns-review-fixes-plan.md T-06: the "Source Hosts" table's
	// Domains column needs "how many domains has this client asked,
	// system-wide, in this window" — not scoped to the current domain, which
	// would trivially always be 1).
	clientDomainsAll := make(map[string]map[string]struct{})
	var totalQueries uint64
	var truncated bool

	for _, b := range s.dnsWindowBuckets(window) {
		for d, clients := range b.pairs {
			if d == domain {
				for client, count := range clients {
					clientTotals[client] += count
					totalQueries += count
				}
			}
			for client := range clients {
				if clientDomainsAll[client] == nil {
					clientDomainsAll[client] = make(map[string]struct{})
				}
				clientDomainsAll[client][d] = struct{}{}
			}
		}
		if b.pairCount >= s.dns.maxPairs {
			truncated = true
		}
	}
	// Keep only the clients that actually queried `domain` (clientTotals) to
	// bound clientDomains' size, same trimming as GetDNSClientDomains' T-04
	// change.
	clientDomains := make(map[string]map[string]struct{}, len(clientTotals))
	for c := range clientTotals {
		clientDomains[c] = clientDomainsAll[c]
	}
	s.dns.mu.RUnlock() // ring lock released before touching domainIPs/traffic below (plan Caution 3)

	ips := s.dns.domainIPs.IPsFor(domain)
	ipsTruncated := s.dns.domainIPs.Truncated()

	ipStrs := make([]string, len(ips))
	for i, e := range ips {
		ipStrs[i] = e.IP
	}

	// Exactly ONE traffic-breakdown call for this request (plan T-04
	// mandatory rule) — GetTrafficBreakdownForDests already returns
	// everything GetTrafficBreakdown would (Dests/Convs/Observed/Accuracy/
	// Series), plus DestSeries/DestTotals for this domain's IP set. Safe to
	// call with an empty ipStrs (returns DestSeries/DestTotals zero-valued).
	breakdown := s.traffic.GetTrafficBreakdownForDests(window, ipStrs)
	leaseByIP, resByIP := s.traffic.hostLookup()

	ipRows := make([]model.DNSDomainIP, 0, len(ips))
	var totalBytes dirBytes
	for _, e := range ips {
		v := breakdown.Dests[e.IP]
		totalBytes.Orig += v.Orig
		totalBytes.Reply += v.Reply
	}
	totalBytesSum := totalBytes.Total()
	for _, e := range ips {
		v := breakdown.Dests[e.IP]
		ipRows = append(ipRows, model.DNSDomainIP{
			IP:           e.IP,
			Bytes:        v.Total(),
			BytesUp:      v.Orig,
			BytesDown:    v.Reply,
			BytesPercent: percentOf(v.Total(), totalBytesSum),
			Shared:       e.Shared,
			LastSeen:     e.LastSeen.UTC().Format(time.RFC3339),
		})
	}
	sort.Slice(ipRows, func(i, j int) bool {
		if ipRows[i].Bytes != ipRows[j].Bytes {
			return ipRows[i].Bytes > ipRows[j].Bytes
		}
		return ipRows[i].IP < ipRows[j].IP
	})

	var sharedIPs bool
	for _, row := range ipRows {
		if row.Shared {
			sharedIPs = true
			break
		}
	}

	// Per-client bytes (plan §1.2 — the accurate, conversation-level join):
	// scan breakdown.Convs ONCE for src==<any client of this domain> AND
	// dst in this domain's IP set, skipping the whole scan when the domain
	// has no known IPs at all (fast path — plan §4 Caution 4).
	clientBytes := make(map[string]dirBytes, len(clientTotals))
	if len(ipStrs) > 0 {
		ipSet := make(map[string]struct{}, len(ipStrs))
		for _, ip := range ipStrs {
			ipSet[ip] = struct{}{}
		}
		for key, v := range breakdown.Convs {
			src, dst, _, _, ok := parseConvKey(key)
			if !ok {
				continue // malformed key — skip, same as the rest of this package
			}
			if _, isClient := clientTotals[src]; !isClient {
				continue
			}
			if _, inDomain := ipSet[dst]; !inDomain {
				continue
			}
			cur := clientBytes[src]
			cur.Orig += v.Orig
			cur.Reply += v.Reply
			clientBytes[src] = cur
		}
	}

	clients := rankDNSClients(clientTotals, totalQueries, leaseByIP, resByIP, dnsStatsTopN)
	// Domain enrichment (docs/ref/todo/statistics-dns-host-domain-label-plan.md
	// T-02): ONE reverseCache.LookupMany batch over just the top-N IPs, called
	// here — after s.dns.mu.RUnlock() above — for the same locking reason as
	// GetDNSQueryStatistics above.
	ipDomain := s.dns.reverseCache.LookupMany(dnsClientStatIPs(clients))
	for i := range clients {
		clients[i].Domain = ipDomain[clients[i].IP]
		v := clientBytes[clients[i].IP]
		clients[i].Bytes = v.Total()
		clients[i].BytesUp = v.Orig
		clients[i].BytesDown = v.Reply
		// bytesPercent here is against THIS domain's TotalBytes (the
		// conversation-level bytes exchanged with this domain's IPs), not
		// ObservedBytes — plan §2.2 note (b): a domain drill-down's client
		// row bytes are a strict subset of that client's window-wide total.
		clients[i].BytesPercent = percentOf(v.Total(), totalBytesSum)
		// Domains = how many domains this client queried system-wide in this
		// window (not scoped to `domain`, which would trivially always be 1)
		// — same "system-wide, not drill-down-scoped" meaning as
		// GetDNSClientDomains' Clients field (plan T-06).
		clients[i].Domains = len(clientDomains[clients[i].IP])
	}
	if len(clientTotals) > dnsStatsTopN {
		truncated = true
	}

	series := breakdown.DestSeries
	if series == nil {
		// No known IPs (or GetTrafficBreakdownForDests was called with an
		// empty set) — DestSeries is nil in that case; build a zero-filled
		// array of the same fixed length instead (plan §2.3: "zero-filled
		// ไม่ใช่ nil"), reusing breakdown.Series' already-correct Ts axis so
		// this never re-derives the window's bucket boundaries itself.
		series = make([]model.BandwidthPoint, len(breakdown.Series))
		for i, p := range breakdown.Series {
			series[i].Ts = p.Ts
		}
	}

	return model.DNSDomainDrilldown{
		Domain:         domain,
		Window:         window,
		Enabled:        true,
		TotalQueries:   totalQueries,
		Truncated:      truncated,
		Clients:        clients,
		IPs:            ipRows,
		TotalBytes:     totalBytesSum,
		TotalBytesUp:   totalBytes.Orig,
		TotalBytesDown: totalBytes.Reply,
		SharedIPs:      sharedIPs,
		IPsTruncated:   ipsTruncated,
		Series:         series,
		Accuracy:       breakdown.Accuracy,
		ObservedBytes:  breakdown.Observed,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

// GetDNSClientDomains composes the /api/statistics/dns/client response: for
// a single client IP (or the reserved "unknown" bucket), the domains it
// queried in the window (drilldown plan T-02), now with per-domain volume +
// a time series (plan §2.2/T-04). client must already be validated/
// normalized by the caller (the API handler).
func (s *StatisticsService) GetDNSClientDomains(window, client string) model.DNSClientDrilldown {
	window = normalizeStatsWindow(window)

	s.dns.mu.RLock()
	enabled := s.dns.enabled
	if !enabled {
		s.dns.mu.RUnlock()
		return model.DNSClientDrilldown{
			Client:      client,
			Window:      window,
			Enabled:     false,
			Domains:     []model.DNSDomainStat{},
			Series:      []model.BandwidthPoint{},
			GeneratedAt: time.Now().UTC().Format(time.RFC3339),
		}
	}

	domainTotals := make(map[string]uint64)
	typeByDomain := make(map[string]string)
	// domainClientsAll tracks, for every domain seen in this window (not just
	// the ones `client` queried), the full set of clients that queried it —
	// same union-across-buckets pattern as GetDNSQueryStatistics's
	// domainClients (plan §0.1/T-04: derived from the same ring loop already
	// running here, no new data source).
	domainClientsAll := make(map[string]map[string]struct{})
	var totalQueries uint64
	var truncated bool

	for _, b := range s.dnsWindowBuckets(window) {
		for domain, clients := range b.pairs {
			if count, ok := clients[client]; ok {
				domainTotals[domain] += count
				totalQueries += count
				if qtype := b.typeByDomain[domain]; qtype != "" {
					typeByDomain[domain] = qtype
				}
			}
			if domainClientsAll[domain] == nil {
				domainClientsAll[domain] = make(map[string]struct{})
			}
			for c := range clients {
				domainClientsAll[domain][c] = struct{}{}
			}
		}
		if b.pairCount >= s.dns.maxPairs {
			truncated = true
		}
	}
	// Keep only the domains `client` actually queried (domainTotals) to bound
	// domainClients' size (plan T-04 item 1).
	domainClients := make(map[string]map[string]struct{}, len(domainTotals))
	for domain := range domainTotals {
		domainClients[domain] = domainClientsAll[domain]
	}
	s.dns.mu.RUnlock() // ring lock released before touching domainIPs/traffic below (plan Caution 3)

	if s.dns.domainIPs.Truncated() {
		truncated = true
	}

	baseDomains := rankTopDomains(domainTotals, typeByDomain, totalQueries, dnsStatsTopN)
	if len(domainTotals) > dnsStatsTopN {
		truncated = true
	}

	// ipCount/sharedIps for every top domain, in a single locked pass (plan
	// T-03/T-04) — never Snapshot() (collapses shared IPs to one domain, see
	// dns_domain_ips.go's Snapshot doc comment) and never one IPsFor call per
	// domain (would re-lock domainIPs once per row).
	domainNames := make([]string, len(baseDomains))
	for i, td := range baseDomains {
		domainNames[i] = td.Domain
	}
	domainIPStats := s.dns.domainIPs.StatsFor(domainNames)

	// The reserved "unknown" client bucket has no IP to join against — never
	// attempt a traffic-breakdown call for it (plan §4 item 9 / final
	// acceptance: "client=unknown ... ไม่มีการพยายาม join volume"). Clients/
	// IPCount/SharedIPs don't need conntrack at all (they come straight from
	// the ring loop and domainIPs above), so they're still filled in here.
	if client == dnsUnknownClient {
		domains := make([]model.DNSDomainStat, len(baseDomains))
		for i, td := range baseDomains {
			stat := domainIPStats[td.Domain]
			domains[i] = model.DNSDomainStat{
				TopDomain: td,
				Clients:   len(domainClients[td.Domain]),
				IPCount:   stat.Count,
				SharedIPs: stat.Shared,
			}
		}
		return model.DNSClientDrilldown{
			Client:       client,
			Window:       window,
			Enabled:      true,
			TotalQueries: totalQueries,
			Truncated:    truncated,
			Domains:      domains,
			Series:       []model.BandwidthPoint{},
			GeneratedAt:  time.Now().UTC().Format(time.RFC3339),
		}
	}

	// Exactly ONE traffic-breakdown call and ONE hostLookup call for this
	// request (plan T-04 mandatory rule).
	breakdown := s.traffic.GetTrafficBreakdownForIP(window, client)
	leaseByIP, resByIP := s.traffic.hostLookup()
	hostname, _ := hostnameFor(client, leaseByIP, resByIP)

	// Per-domain bytes (plan §1.2 — the accurate, conversation-level join):
	// scan breakdown.Convs ONCE for src==client, mapping each dst IP to a
	// domain via a SINGLE domainIPs.Snapshot() call (never one scan/lookup
	// per domain — plan T-04 item 3).
	snapshot := s.dns.domainIPs.Snapshot()
	domainBytes := make(map[string]dirBytes, len(domainTotals))
	clientPrefix := client + "|"
	for key, v := range breakdown.Convs {
		if !strings.HasPrefix(key, clientPrefix) {
			continue // fast path: this key's src cannot be client
		}
		src, dst, _, _, ok := parseConvKey(key)
		if !ok || src != client {
			continue
		}
		domain, known := snapshot[dst]
		if !known {
			continue
		}
		cur := domainBytes[domain]
		cur.Orig += v.Orig
		cur.Reply += v.Reply
		domainBytes[domain] = cur
	}

	clientTotal := breakdown.Hosts[client]
	clientTotalBytes := clientTotal.Total()

	domains := make([]model.DNSDomainStat, len(baseDomains))
	for i, td := range baseDomains {
		bytes := domainBytes[td.Domain]
		stat := domainIPStats[td.Domain]
		domains[i] = model.DNSDomainStat{
			TopDomain: td,
			Bytes:     bytes.Total(),
			BytesUp:   bytes.Orig,
			BytesDown: bytes.Reply,
			// bytesPercent here is against THIS client's TotalBytes (its
			// window-wide total across ALL destinations), not the sum of
			// Domains[].Bytes — same "strict subset" relationship as
			// GetDNSDomainClients' client rows above, just from the other
			// side of the join.
			BytesPercent: percentOf(bytes.Total(), clientTotalBytes),
			// Clients = how many clients system-wide queried this domain in
			// this window (not just `client`); IPCount/SharedIPs = this
			// domain's forward-index IP set, system-wide — same values the
			// overview/domain drill-down rows show for this domain, not
			// scoped to the single client being drilled into (docs/ref/todo/
			// statistics-dns-review-fixes-plan.md §2/T-04 — R-3 fix).
			Clients:   len(domainClients[td.Domain]),
			IPCount:   stat.Count,
			SharedIPs: stat.Shared,
		}
	}

	return model.DNSClientDrilldown{
		Client:         client,
		Hostname:       hostname,
		Window:         window,
		Enabled:        true,
		TotalQueries:   totalQueries,
		Truncated:      truncated,
		Domains:        domains,
		TotalBytes:     clientTotalBytes,
		TotalBytesUp:   clientTotal.Orig,
		TotalBytesDown: clientTotal.Reply,
		Series:         breakdown.HostSeries,
		Accuracy:       breakdown.Accuracy,
		GeneratedAt:    time.Now().UTC().Format(time.RFC3339),
	}
}

// dnsClientStatIPs collects the IPs of an already top-N-trimmed slice of
// DNSClientStat rows, for a single reverseCache.LookupMany batch call (plan
// T-02) — never call reverseCache.Lookup per-row.
func dnsClientStatIPs(clients []model.DNSClientStat) []string {
	ips := make([]string, len(clients))
	for i, c := range clients {
		ips[i] = c.IP
	}
	return ips
}
