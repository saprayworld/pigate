import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { type TopDomain, mockBandwidthSeries } from "./statisticsService"
import { type BandwidthPoint } from "./trafficStatisticsService"
import { mockWindowScale, type StatsWindow } from "@/lib/statsWindow"

// DNS Query Statistics tab (docs/ref/todo/dns-query-statistics-drilldown-plan.md
// T-07, extended by docs/ref/todo/statistics-dns-page-revamp-plan.md T-08) —
// backs the "สถิติ" tab on the DNS Server page and the Statistics -> DNS
// page. Types mirror backend/internal/model/statistics.go's DNSDomainStat /
// DNSDomainIP / DNSClientStat / DNSQueryStatistics / DNSDomainDrilldown /
// DNSClientDrilldown field-for-field; do not let these drift from the Go
// structs (see docs/openapi.yaml for the source of truth). BandwidthPoint is
// re-exported from trafficStatisticsService.ts (which itself re-exports it
// from statisticsService.ts) rather than redeclared here, so the three
// clients can never drift. Same RAM-only, display-only, opt-in
// (dns/settings.queryLogging) data source as TrafficStatistics.topDomains in
// statisticsService.ts — nothing here is ever persisted, and a pigate
// restart resets all counts to zero.

export type { TopDomain, BandwidthPoint }

// DNSDomainIP is one row of a domain drill-down's "Resolved IPs" table
// (plan §2.2) — ranked by approximate volume. Bytes/BytesUp/BytesDown are a
// join between the DNS answer forward index and per-IP conntrack byte
// counters (see the Go DTO's comment for the full caveat list): an IP used
// by more than one domain (`shared`) has its bytes double-counted across
// every domain that references it, and traffic to an IP reached without
// going through this device's DNS resolver is never counted here.
export interface DNSDomainIP {
  ip: string
  bytes: number
  bytesUp: number
  bytesDown: number
  // Percent of the PARENT domain's totalBytes (DNSDomainDrilldown.totalBytes)
  // — a different denominator than DNSDomainStat.bytesPercent below.
  bytesPercent: number
  shared: boolean
  // RFC3339 UTC timestamp of the most recent DNS answer that resolved the
  // domain to this IP — not tied to the stats window (can be older than it).
  lastSeen: string
}

// DNSDomainStat is one row of the domain-centric tables on the Statistics ->
// DNS page (plan §2.2, T-01) — extends TopDomain (domain/queryType/count/
// percent, where percent = % of QUERY COUNT, never confuse with
// bytesPercent below) with the join-derived volume fields.
export interface DNSDomainStat extends TopDomain {
  // Number of distinct client IPs that queried this domain in the window,
  // system-wide — same value regardless of which page/drill-down this row
  // is embedded in (docs/ref/todo/statistics-dns-review-fixes-plan.md T-04:
  // a domain drill-down's client-focused row is NOT scoped to a single
  // client).
  clients: number
  // Number of IPs currently known for this domain in the RAM-only forward
  // index, system-wide — may be 0 when the domain was queried but no
  // matching answer was observed/kept (TTL expiry, or the index was
  // capped).
  ipCount: number
  // True when at least one of this domain's known IPs is also referenced by
  // another domain (e.g. a CDN/cloud load balancer) — bytes below
  // double-counts that IP's traffic against every domain that shares it.
  sharedIps: boolean
  // APPROXIMATION of this domain's traffic volume — sum of the
  // conntrack-derived byte counters of the IPs this domain was last
  // resolved to, NOT a true per-domain packet count. bytesUp/bytesDown are
  // flow-relative (orig = up = client->server, reply = down); bytesUp +
  // bytesDown == bytes always.
  bytes: number
  bytesUp: number
  bytesDown: number
  // Percent of BYTES (never query count) — the denominator is specified on
  // the parent DTO that embeds this row (e.g. DNSQueryStatistics.domainBytes).
  // Always labelled "% Vol" separately from percent's "% Query" in the UI.
  bytesPercent: number
}

// DNSClientStat is one row of the "Top Clients" table on the DNS Query
// Statistics tab — ranked by query count, one row per source IP that issued
// DNS queries.
export interface DNSClientStat {
  ip: string
  hostname: string
  // The domain name dnsmasq most recently answered for ip, from the same
  // ip->domain reverse lookup as TopHost.domain
  // (docs/ref/todo/statistics-dns-host-domain-label-plan.md T-05) — display
  // only, empty when unknown/expired. NEVER used for any decision (firewall,
  // policy, routing, QoS) — the underlying cache can be poisoned by any LAN
  // client simply querying a name it controls. Usually empty for rows in
  // this table: it's keyed by DNS ANSWER ips (mostly Internet destinations),
  // while these rows are LAN client (source) ips.
  domain: string
  count: number
  percent: number
  // Number of distinct domains this client queried in the window,
  // system-wide — NOT scoped to the parent DTO's own domain/client focus
  // (docs/ref/todo/statistics-dns-review-fixes-plan.md T-06): in a domain
  // drill-down's per-client row this is the count across every domain that
  // client asked in the window, not just the drilled-down domain (which
  // would trivially always be 1).
  domains: number
  // The MEANING depends on the parent DTO this row is embedded in:
  //   - DNSQueryStatistics.topClients: this client's TOTAL observed bytes
  //     across ALL destinations in the window, not limited to DNS-resolved
  //     traffic.
  //   - DNSDomainDrilldown.clients: ONLY the bytes exchanged between this
  //     client and the drilled domain's known IPs — a strict subset of the
  //     total above.
  // bytesUp/bytesDown are flow-relative (orig = up, reply = down);
  // bytesUp + bytesDown == bytes.
  bytes: number
  bytesUp: number
  bytesDown: number
  // Percent of BYTES (never query count) — same "% Vol" vs "% Query"
  // distinction as DNSDomainStat.bytesPercent above; denominator specified
  // on the parent DTO.
  bytesPercent: number
}

export interface DNSQueryStatistics {
  window: StatsWindow
  enabled: boolean
  totalQueries: number
  truncated: boolean
  topDomains: DNSDomainStat[]
  topClients: DNSClientStat[]
  // Same network-wide conntrack-observed byte total as
  // TrafficStatistics.observedBytes for this window.
  observedBytes: number
  // Sum of bytes across all rows of topDomains — i.e. only the portion of
  // observedBytes that could be attributed to a known domain via the
  // domain->IP join. Denominator for each topDomains row's bytesPercent.
  domainBytes: number
  // Distinct domains/clients observed in the window (before any per-response
  // row limit is applied).
  totalDomains: number
  totalClients: number
  // "estimated" | "near-exact" — mirrors TrafficStatistics.accuracy, the
  // same conntrack-poll-vs-DESTROY-event signal.
  accuracy: "estimated" | "near-exact"
  generatedAt: string
}

export interface DNSDomainDrilldown {
  domain: string
  window: StatsWindow
  enabled: boolean
  totalQueries: number
  truncated: boolean
  clients: DNSClientStat[]
  // "Resolved IPs" table for this domain — one row per known IP, ranked by
  // approximate volume. Never nil (empty when no IP is known for this
  // domain).
  ips: DNSDomainIP[]
  // Sum of ips[].bytes/bytesUp/bytesDown — this domain's approximate total
  // volume in the window, and the denominator for each ips row's
  // bytesPercent.
  totalBytes: number
  totalBytesUp: number
  totalBytesDown: number
  // True when at least one row of ips has shared=true.
  sharedIps: boolean
  // True when the RAM-only domain->IP forward index hit its per-domain IP
  // cap while building ips above — separate from truncated (which covers
  // the query-count ring).
  ipsTruncated: boolean
  // This domain's approximate volume over time — sum of dstBytes across all
  // of ips[] per 5-minute bucket, flow-relative (orig = up, reply = down).
  // Fixed length equal to the window's bucket count, zero-filled (never
  // nil). Invariant: sum(series[].bytes) == totalBytes.
  series: BandwidthPoint[]
  accuracy: "estimated" | "near-exact"
  // Same network-wide byte total as TrafficStatistics.observedBytes for this
  // window, so the UI can show totalBytes as "% of all traffic".
  observedBytes: number
  generatedAt: string
}

export interface DNSClientDrilldown {
  client: string
  hostname: string
  window: StatsWindow
  enabled: boolean
  totalQueries: number
  truncated: boolean
  // Since docs/ref/todo/statistics-dns-review-fixes-plan.md T-04 (review fix
  // on PR 127): clients = the number of clients, system-wide, that queried
  // that domain in this window (NOT just this one); ipCount/sharedIps =
  // that domain's known IPs in the RAM-only forward index, system-wide —
  // identical meaning to the same fields on the overview/domain drill-down
  // responses, not scoped to `client`. bytes/bytesUp/bytesDown are still
  // ONLY the bytes exchanged between THIS client and that domain's known
  // IPs — unlike clients/ipCount/sharedIps, those stay drill-down-scoped.
  domains: DNSDomainStat[]
  // This client's total observed bytes across ALL destinations in the
  // window — NOT limited to DNS-resolved domains, so it can exceed the sum
  // of domains[].bytes. Always 0 for the reserved "unknown" client bucket
  // (no IP to join against).
  totalBytes: number
  totalBytesUp: number
  totalBytesDown: number
  // This client's traffic over time, flow-relative (orig = up, reply =
  // down). Fixed length equal to the window's bucket count, zero-filled
  // (never nil) for a known client; empty for the "unknown" bucket.
  // Invariant: sum(series[].bytes) == totalBytes.
  series: BandwidthPoint[]
  accuracy: "estimated" | "near-exact"
  generatedAt: string
}

// DNSIPDomain is one row of the /statistics/dns/ip response (docs/ref/todo/
// statistics-dns-ip-filter-plan.md T-06) — a domain that has ever resolved
// to the IP being looked up. Extends DNSDomainStat (count/percent are
// window-scoped for THIS domain; clients/ipCount/sharedIps are this domain's
// system-wide values; bytes/bytesUp/bytesDown are this domain's TOTAL volume
// across ALL of its known IPs, not just the one being looked up).
// bytesPercent here is bytes as a percent of DNSIPDomains.matchedBytes (the
// sum across every row of this response) — deliberately NOT the window-wide
// domainBytes used elsewhere, so the UI must label this "% Vol" as scoped to
// this matched domain group, not the whole window (plan §2.4).
export interface DNSIPDomain extends DNSDomainStat {
  // RFC3339 UTC timestamp of the most recent DNS answer that resolved this
  // domain to the IP being looked up — not tied to the stats window.
  lastSeen: string
}

// DNSIPDomains is the GET /api/statistics/dns/ip response — every domain
// the RAM-only reverse index knows to have resolved to `ip`. Display-only,
// derived from dnsmasq's answer log (poisonable by any LAN client): NEVER
// used to drive firewall rule generation, policy matching, routing, or QoS
// decisions.
export interface DNSIPDomains {
  // Canonical form of the address that was looked up.
  ip: string
  window: StatsWindow
  enabled: boolean
  // Resolved from a DHCP lease/reservation for ip — display only, falls
  // back to ip itself when unknown.
  hostname: string
  // Total DNS queries observed in this window across ALL domains (not just
  // the ones matching this IP) — the denominator for each domains[] row's
  // percent.
  totalQueries: number
  truncated: boolean
  // True when the RAM-only domain->IP forward index hit one of its caps at
  // some point — domains below could be incomplete.
  ipsTruncated: boolean
  // Every domain known to have resolved to ip, sorted by bytes desc, then
  // count desc, then domain asc. Never undefined — an empty array means no
  // domain in the RAM-only index currently maps to this IP.
  domains: DNSIPDomain[]
  domainCount: number
  // True when domainCount > 1 — this IP is used by more than one domain
  // (e.g. a CDN or shared hosting IP), so each domain's bytes double-counts
  // the same underlying traffic to this IP.
  shared: boolean
  // Sum of domains[].bytes/bytesUp/bytesDown across every row of this
  // response — the denominator for each row's bytesPercent.
  matchedBytes: number
  matchedBytesUp: number
  matchedBytesDown: number
  // Conntrack-derived byte counters for the IP being looked up itself (not
  // summed across domains).
  ipBytes: number
  ipBytesUp: number
  ipBytesDown: number
  // This IP's traffic over time, flow-relative (orig = up, reply = down).
  // Fixed length equal to the window's bucket count, zero-filled (never
  // undefined). Invariant: sum(series[].bytes) == ipBytes.
  series: BandwidthPoint[]
  observedBytes: number
  accuracy: "estimated" | "near-exact"
  generatedAt: string
}

// mockHostnames mirrors statisticsService.ts's mockHosts and kernel/mock.go's
// mockDNSQueryEvents matrix (same 3 LAN client IPs).
const mockHostnames: Record<string, string> = {
  "192.168.1.101": "iPhone-13",
  "192.168.1.102": "Android-SmartTV",
  "192.168.1.105": "iPad-Pro",
}

// mockClientDomains gives exactly ONE mock client IP a reverse-lookup
// domain, so mock mode can exercise the "domain wins over hostname" path
// (docs/ref/todo/statistics-dns-host-domain-label-plan.md T-05) — every
// other client is deliberately left with no entry, reflecting the real
// backend's behaviour where a LAN client IP usually has no reverse entry at
// all (see DNSClientStat.domain's doc comment above).
const mockClientDomains: Record<string, string> = {
  "192.168.1.102": "smarttv.home.lan",
}

// mockPairs mirrors kernel/mock.go's mockDNSQueryEvents (domain, client)
// weighted matrix exactly, so mock-mode drill-down shows the same shape of
// data as -mock=true backend responses: www.youtube.com has 3 clients,
// netflix.com has 2, line-apps.com/cdn.jsdelivr.net have 1 each.
const mockPairs: { domain: string; queryType: string; client: string; weight: number }[] = [
  { domain: "www.youtube.com", queryType: "A", client: "192.168.1.101", weight: 5 },
  { domain: "www.youtube.com", queryType: "A", client: "192.168.1.102", weight: 3 },
  { domain: "www.youtube.com", queryType: "A", client: "192.168.1.105", weight: 1 },
  { domain: "googlevideo.com", queryType: "A", client: "192.168.1.101", weight: 4 },
  { domain: "netflix.com", queryType: "A", client: "192.168.1.102", weight: 3 },
  { domain: "netflix.com", queryType: "A", client: "192.168.1.101", weight: 2 },
  { domain: "line-apps.com", queryType: "A", client: "192.168.1.105", weight: 2 },
  { domain: "cdn.jsdelivr.net", queryType: "A", client: "192.168.1.105", weight: 1 },
]

// mockDomainIPs mirrors kernel/mock.go's mockDNSAnswerEvents (plan T-06) —
// same domains/IPs, including the shared IP (64.233.166.127) between
// www.youtube.com and googlevideo.com, and domains deliberately left with no
// entry at all (netflix.com, line-apps.com) so the "no known IP" empty state
// is exercisable in mock mode too. bytesBase/upRatio are purely synthetic,
// scaled the same deterministic way as mockPairs' weight -> count.
const mockDomainIPs: Record<
  string,
  { ip: string; bytesBase: number; upRatio: number; lastSeenMinAgo: number }[]
> = {
  "www.youtube.com": [
    { ip: "142.250.80.46", bytesBase: 9_000_000, upRatio: 0.15, lastSeenMinAgo: 2 },
    { ip: "64.233.166.127", bytesBase: 1_200_000, upRatio: 0.5, lastSeenMinAgo: 5 },
    { ip: "172.217.16.14", bytesBase: 500_000, upRatio: 0.2, lastSeenMinAgo: 8 },
  ],
  "googlevideo.com": [
    { ip: "173.194.76.94", bytesBase: 26_000_000, upRatio: 0.08, lastSeenMinAgo: 1 },
    { ip: "64.233.166.127", bytesBase: 1_200_000, upRatio: 0.5, lastSeenMinAgo: 5 },
  ],
  "cdn.jsdelivr.net": [{ ip: "151.101.1.69", bytesBase: 7_500_000, upRatio: 0.15, lastSeenMinAgo: 4 }],
  // netflix.com / line-apps.com intentionally have no entry here — mirrors
  // kernel/mock.go's mockDNSAnswerEvents, which never answers either domain.
}

// mockIpRefCount counts how many domains reference each IP — derives the
// "shared" flag (an IP referenced by more than one domain), mirroring the
// real forward index's ipRefs (service/dns_domain_ips.go).
const mockIpRefCount = new Map<string, number>()
for (const ips of Object.values(mockDomainIPs)) {
  for (const { ip } of ips) {
    mockIpRefCount.set(ip, (mockIpRefCount.get(ip) ?? 0) + 1)
  }
}

function mockCount(weight: number, scale: number): number {
  return Math.max(1, Math.round(weight * 10 * scale))
}

function percentOf(part: number, total: number): number {
  if (total === 0) return 0
  return Math.round((part / total) * 1000) / 10
}

function normalizeDomain(domain: string): string {
  return domain.trim().toLowerCase().replace(/\.$/, "")
}

function normalizeClient(client: string): string {
  const trimmed = client.trim()
  return trimmed === "unknown" ? trimmed : trimmed.toLowerCase()
}

// mockDomainIpRows builds this domain's "Resolved IPs" table, scaled by
// `scale` the same deterministic way mockCount scales query weight -> count,
// sorted by bytes desc / ip asc (mirrors the real backend's ordering).
function mockDomainIpRows(domain: string, scale: number): DNSDomainIP[] {
  const entries = mockDomainIPs[domain] ?? []
  const rows = entries.map(({ ip, bytesBase, upRatio, lastSeenMinAgo }) => {
    const bytes = Math.max(1, Math.round(bytesBase * scale))
    const bytesUp = Math.round(bytes * upRatio)
    return {
      ip,
      bytes,
      bytesUp,
      bytesDown: bytes - bytesUp,
      shared: (mockIpRefCount.get(ip) ?? 0) > 1,
      lastSeen: new Date(Date.now() - lastSeenMinAgo * 60_000).toISOString(),
    }
  })
  const totalBytes = rows.reduce((sum, r) => sum + r.bytes, 0)
  return rows
    .map((r) => ({ ...r, bytesPercent: percentOf(r.bytes, totalBytes) }))
    .sort((a, b) => b.bytes - a.bytes || a.ip.localeCompare(b.ip))
}

// mockDomainTotals is the sum of mockDomainIpRows(domain, scale) — this
// domain's approximate total volume, {bytes:0,...} for a domain with no
// known IP (netflix.com/line-apps.com).
function mockDomainTotals(domain: string, scale: number): { bytes: number; bytesUp: number; bytesDown: number } {
  return mockDomainIpRows(domain, scale).reduce(
    (acc, r) => ({ bytes: acc.bytes + r.bytes, bytesUp: acc.bytesUp + r.bytesUp, bytesDown: acc.bytesDown + r.bytesDown }),
    { bytes: 0, bytesUp: 0, bytesDown: 0 }
  )
}

// mockClientDomainByteMap splits each known domain's total bytes across the
// clients that queried it, proportional to that client's share of the
// domain's mockPairs weight (mirrors plan §1.2's per-client x per-domain
// join, simplified for mock data). Domains with no known IP contribute
// nothing (their mockDomainTotals is zero).
function mockClientDomainByteMap(
  scale: number
): Map<string, Map<string, { bytes: number; bytesUp: number; bytesDown: number }>> {
  const out = new Map<string, Map<string, { bytes: number; bytesUp: number; bytesDown: number }>>()
  for (const domain of Object.keys(mockDomainIPs)) {
    const domainTotal = mockDomainTotals(domain, scale)
    if (domainTotal.bytes === 0) continue
    const domainPairs = mockPairs.filter((p) => p.domain === domain)
    const weightTotal = domainPairs.reduce((sum, p) => sum + p.weight, 0) || 1
    for (const p of domainPairs) {
      const share = p.weight / weightTotal
      const bytes = Math.round(domainTotal.bytes * share)
      const bytesUp = Math.round(domainTotal.bytesUp * share)
      const perClient = out.get(p.client) ?? new Map()
      perClient.set(domain, { bytes, bytesUp, bytesDown: bytes - bytesUp })
      out.set(p.client, perClient)
    }
  }
  return out
}

// mockClientTotalBytes is a client's TOTAL observed bytes across ALL
// destinations (DNSClientStat.bytes in the overview/topClients context) —
// the DNS-attributable sum above, plus a fixed 40% bonus representing
// traffic to IPs reached without a DNS lookup this device observed (plan
// §1.1 item 2), so mock mode's client total never equals the sum of its
// per-domain bytes exactly, matching the real backend's invariant.
function mockClientTotalBytes(
  client: string,
  domainByClient: Map<string, Map<string, { bytes: number; bytesUp: number; bytesDown: number }>>
): { bytes: number; bytesUp: number; bytesDown: number } {
  const perDomain = domainByClient.get(client)
  let bytes = 0
  let bytesUp = 0
  if (perDomain) {
    for (const v of perDomain.values()) {
      bytes += v.bytes
      bytesUp += v.bytesUp
    }
  }
  bytes = Math.round(bytes * 1.4)
  bytesUp = Math.round(bytesUp * 1.4)
  return { bytes, bytesUp, bytesDown: bytes - bytesUp }
}

// mockDomainsForIP is the reverse of mockDomainIps — every (domain,
// lastSeenMinAgo) pair whose known IPs include `ip`, mirroring the real
// forward index's DomainsForIP (service/dns_domain_ips.go). 64.233.166.127
// is used by both www.youtube.com and googlevideo.com, giving mock mode a
// ready-made "shared IP" test case (plan T-06).
function mockDomainsForIP(ip: string): { domain: string; lastSeenMinAgo: number }[] {
  const out: { domain: string; lastSeenMinAgo: number }[] = []
  for (const [domain, entries] of Object.entries(mockDomainIPs)) {
    const match = entries.find((e) => e.ip === ip)
    if (match) out.push({ domain, lastSeenMinAgo: match.lastSeenMinAgo })
  }
  return out
}

export const dnsStatisticsService = {
  // GET /api/statistics/dns — Top Domains / Top Clients tables.
  getDNSStatistics: async (window: StatsWindow = "1h"): Promise<DNSQueryStatistics> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      // mockCount below already Math.max(1, Math.round(...))s its own
      // weight*10*scale product, so a fractional scale (e.g. 15m -> 0.3) is
      // safe to pass through unrounded here (plan §6 item 4).
      const scale = mockWindowScale(window)

      const domainTotals = new Map<string, { count: number; queryType: string; clients: Set<string> }>()
      const clientTotals = new Map<string, { count: number; domains: Set<string> }>()
      let totalQueries = 0
      for (const p of mockPairs) {
        const count = mockCount(p.weight, scale)
        totalQueries += count
        const d = domainTotals.get(p.domain) ?? { count: 0, queryType: p.queryType, clients: new Set<string>() }
        d.count += count
        d.clients.add(p.client)
        domainTotals.set(p.domain, d)
        const c = clientTotals.get(p.client) ?? { count: 0, domains: new Set<string>() }
        c.count += count
        c.domains.add(p.domain)
        clientTotals.set(p.client, c)
      }

      const domainByClient = mockClientDomainByteMap(scale)

      let domainBytes = 0
      const topDomains: DNSDomainStat[] = Array.from(domainTotals.entries())
        .map(([domain, v]) => {
          const totals = mockDomainTotals(domain, scale)
          domainBytes += totals.bytes
          const ips = mockDomainIpRows(domain, scale)
          return {
            domain,
            queryType: v.queryType,
            count: v.count,
            percent: totalQueries > 0 ? Math.round((v.count / totalQueries) * 1000) / 10 : 0,
            clients: v.clients.size,
            ipCount: ips.length,
            sharedIps: ips.some((ip) => ip.shared),
            bytes: totals.bytes,
            bytesUp: totals.bytesUp,
            bytesDown: totals.bytesDown,
            bytesPercent: 0, // filled below once domainBytes is final
          }
        })
        .sort((a, b) => b.count - a.count || a.domain.localeCompare(b.domain))
      for (const d of topDomains) d.bytesPercent = percentOf(d.bytes, domainBytes)

      const observedBytes = Math.max(domainBytes, Math.round(domainBytes / 0.6))

      const topClients: DNSClientStat[] = Array.from(clientTotals.entries())
        .map(([ip, v]) => {
          const totals = mockClientTotalBytes(ip, domainByClient)
          return {
            ip,
            hostname: mockHostnames[ip] ?? "",
            domain: mockClientDomains[ip] ?? "",
            count: v.count,
            percent: totalQueries > 0 ? Math.round((v.count / totalQueries) * 1000) / 10 : 0,
            domains: v.domains.size,
            bytes: totals.bytes,
            bytesUp: totals.bytesUp,
            bytesDown: totals.bytesDown,
            bytesPercent: percentOf(totals.bytes, observedBytes),
          }
        })
        .sort((a, b) => b.count - a.count || a.ip.localeCompare(b.ip))

      return {
        window,
        // Mock mode always reports the feature enabled (mirrors the real
        // -mock=true backend's WatchDNSLog, which always synthesizes
        // events), so the dev loop exercises the populated tables, not the
        // empty-state.
        enabled: true,
        totalQueries,
        truncated: false,
        topDomains,
        topClients,
        observedBytes,
        domainBytes,
        totalDomains: domainTotals.size,
        totalClients: clientTotals.size,
        accuracy: "near-exact",
        generatedAt: new Date().toISOString(),
      }
    }

    const response = await fetch(`${API_BASE_URL}/statistics/dns?window=${window}`)
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS query statistics: ${response.statusText}`)
    }
    return response.json()
  },

  // GET /api/statistics/dns/domain — clients that queried a given domain,
  // plus the domain's known resolved IPs and volume over time.
  getDomainClients: async (domain: string, window: StatsWindow = "1h"): Promise<DNSDomainDrilldown> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      // mockCount below already Math.max(1, Math.round(...))s its own
      // weight*10*scale product, so a fractional scale (e.g. 15m -> 0.3) is
      // safe to pass through unrounded here (plan §6 item 4).
      const scale = mockWindowScale(window)
      const target = normalizeDomain(domain)

      const clientCounts = new Map<string, number>()
      let totalQueries = 0
      for (const p of mockPairs) {
        if (p.domain !== target) continue
        const count = mockCount(p.weight, scale)
        totalQueries += count
        clientCounts.set(p.client, (clientCounts.get(p.client) ?? 0) + count)
      }

      const ips = mockDomainIpRows(target, scale)
      const totals = mockDomainTotals(target, scale)
      const weightTotal = mockPairs.filter((p) => p.domain === target).reduce((sum, p) => sum + p.weight, 0) || 1

      const clients: DNSClientStat[] = Array.from(clientCounts.entries())
        .map(([ip, count]) => {
          const pairWeight = mockPairs.find((p) => p.domain === target && p.client === ip)?.weight ?? 0
          const share = totals.bytes > 0 ? pairWeight / weightTotal : 0
          const bytes = Math.round(totals.bytes * share)
          const bytesUp = Math.round(totals.bytesUp * share)
          // domains = how many domains this client queried system-wide in
          // this window (T-06, docs/ref/todo/statistics-dns-review-fixes-plan.md)
          // — NOT scoped to `target`, which would trivially always be 1.
          const domainsForClient = new Set(mockPairs.filter((p) => p.client === ip).map((p) => p.domain)).size
          return {
            ip,
            hostname: mockHostnames[ip] ?? "",
            domain: mockClientDomains[ip] ?? "",
            count,
            percent: totalQueries > 0 ? Math.round((count / totalQueries) * 1000) / 10 : 0,
            domains: domainsForClient,
            bytes,
            bytesUp,
            bytesDown: bytes - bytesUp,
            bytesPercent: percentOf(bytes, totals.bytes),
          }
        })
        .sort((a, b) => b.count - a.count || a.ip.localeCompare(b.ip))

      const series = mockBandwidthSeries(window, totals.bytes, totals.bytes > 0 ? totals.bytesUp / totals.bytes : 0.15).series

      return {
        domain: target,
        window,
        enabled: true,
        totalQueries,
        truncated: false,
        clients,
        ips,
        totalBytes: totals.bytes,
        totalBytesUp: totals.bytesUp,
        totalBytesDown: totals.bytesDown,
        sharedIps: ips.some((ip) => ip.shared),
        ipsTruncated: false,
        series,
        accuracy: "near-exact",
        observedBytes: totals.bytes,
        generatedAt: new Date().toISOString(),
      }
    }

    const response = await fetch(
      `${API_BASE_URL}/statistics/dns/domain?domain=${encodeURIComponent(domain)}&window=${window}`
    )
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS domain drill-down: ${response.statusText}`)
    }
    return response.json()
  },

  // GET /api/statistics/dns/client — domains a given client queried, plus
  // this client's total volume over time.
  getClientDomains: async (client: string, window: StatsWindow = "1h"): Promise<DNSClientDrilldown> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      // mockCount below already Math.max(1, Math.round(...))s its own
      // weight*10*scale product, so a fractional scale (e.g. 15m -> 0.3) is
      // safe to pass through unrounded here (plan §6 item 4).
      const scale = mockWindowScale(window)
      const target = normalizeClient(client)

      const domainTotals = new Map<string, { count: number; queryType: string }>()
      let totalQueries = 0
      for (const p of mockPairs) {
        if (p.client !== target) continue
        const count = mockCount(p.weight, scale)
        totalQueries += count
        const d = domainTotals.get(p.domain)
        domainTotals.set(p.domain, { count: (d?.count ?? 0) + count, queryType: p.queryType })
      }

      const domainByClient = mockClientDomainByteMap(scale)
      const perDomainBytes = domainByClient.get(target)
      const clientTotals = mockClientTotalBytes(target, domainByClient)

      const domains: DNSDomainStat[] = Array.from(domainTotals.entries())
        .map(([d, v]) => {
          const b = perDomainBytes?.get(d) ?? { bytes: 0, bytesUp: 0, bytesDown: 0 }
          // clients/ipCount/sharedIps are system-wide values for domain `d`
          // — identical meaning to the overview's topDomains row for the
          // same domain, not scoped to `target` (R-3 fix, T-04/T-07:
          // docs/ref/todo/statistics-dns-review-fixes-plan.md).
          const domainIps = mockDomainIpRows(d, scale)
          const clientsForDomain = new Set(mockPairs.filter((p) => p.domain === d).map((p) => p.client)).size
          return {
            domain: d,
            queryType: v.queryType,
            count: v.count,
            percent: totalQueries > 0 ? Math.round((v.count / totalQueries) * 1000) / 10 : 0,
            clients: clientsForDomain,
            ipCount: domainIps.length,
            sharedIps: domainIps.some((ip) => ip.shared),
            bytes: b.bytes,
            bytesUp: b.bytesUp,
            bytesDown: b.bytesDown,
            bytesPercent: percentOf(b.bytes, clientTotals.bytes),
          }
        })
        .sort((a, b) => b.count - a.count || a.domain.localeCompare(b.domain))

      const series =
        clientTotals.bytes > 0
          ? mockBandwidthSeries(window, clientTotals.bytes, clientTotals.bytesUp / clientTotals.bytes).series
          : mockBandwidthSeries(window, 0).series

      return {
        client: target,
        hostname: mockHostnames[target] ?? "",
        window,
        enabled: true,
        totalQueries,
        truncated: false,
        domains,
        totalBytes: clientTotals.bytes,
        totalBytesUp: clientTotals.bytesUp,
        totalBytesDown: clientTotals.bytesDown,
        series,
        accuracy: "near-exact",
        generatedAt: new Date().toISOString(),
      }
    }

    const response = await fetch(
      `${API_BASE_URL}/statistics/dns/client?client=${encodeURIComponent(client)}&window=${window}`
    )
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS client drill-down: ${response.statusText}`)
    }
    return response.json()
  },

  // GET /api/statistics/dns/ip — every domain that has resolved to a given
  // IP (docs/ref/todo/statistics-dns-ip-filter-plan.md T-06), for the
  // Statistics -> DNS page's IP-filter mode.
  getIPDomains: async (ip: string, window: StatsWindow = "1h"): Promise<DNSIPDomains> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      const scale = mockWindowScale(window)
      const target = ip.trim().toLowerCase()

      const matches = mockDomainsForIP(target)

      // Same domainTotals/totalQueries computation as getDNSStatistics above
      // — count/percent must agree with the overview for the same window.
      const domainTotals = new Map<string, { count: number; queryType: string; clients: Set<string> }>()
      let totalQueries = 0
      for (const p of mockPairs) {
        const count = mockCount(p.weight, scale)
        totalQueries += count
        const d = domainTotals.get(p.domain) ?? { count: 0, queryType: p.queryType, clients: new Set<string>() }
        d.count += count
        d.clients.add(p.client)
        domainTotals.set(p.domain, d)
      }

      const domains: DNSIPDomain[] = matches
        .map(({ domain, lastSeenMinAgo }) => {
          const totals = mockDomainTotals(domain, scale)
          const domainIps = mockDomainIpRows(domain, scale)
          const d = domainTotals.get(domain)
          return {
            domain,
            queryType: d?.queryType ?? "",
            count: d?.count ?? 0,
            percent: d && totalQueries > 0 ? Math.round((d.count / totalQueries) * 1000) / 10 : 0,
            clients: d?.clients.size ?? 0,
            ipCount: domainIps.length,
            sharedIps: domainIps.some((row) => row.shared),
            bytes: totals.bytes,
            bytesUp: totals.bytesUp,
            bytesDown: totals.bytesDown,
            bytesPercent: 0, // filled below once matchedBytes is final
            lastSeen: new Date(Date.now() - lastSeenMinAgo * 60_000).toISOString(),
          }
        })
        .sort((a, b) => b.bytes - a.bytes || b.count - a.count || a.domain.localeCompare(b.domain))

      const matchedBytes = domains.reduce((sum, d) => sum + d.bytes, 0)
      const matchedBytesUp = domains.reduce((sum, d) => sum + d.bytesUp, 0)
      for (const d of domains) d.bytesPercent = percentOf(d.bytes, matchedBytes)

      // IP-level bytes: the per-(domain,ip) bytesBase is defined IP-level in
      // mockDomainIPs (equal across every domain that references it), so any
      // matching row's bytes are this IP's own total.
      const ipRow =
        matches.length > 0
          ? mockDomainIpRows(matches[0].domain, scale).find((row) => row.ip === target)
          : undefined
      const ipBytes = ipRow?.bytes ?? 0
      const ipBytesUp = ipRow?.bytesUp ?? 0

      const series = mockBandwidthSeries(window, ipBytes, ipBytes > 0 ? ipBytesUp / ipBytes : 0.15).series

      return {
        ip: target,
        window,
        enabled: true,
        hostname: mockHostnames[target] ?? target,
        totalQueries,
        truncated: false,
        ipsTruncated: false,
        domains,
        domainCount: domains.length,
        shared: domains.length > 1,
        matchedBytes,
        matchedBytesUp,
        matchedBytesDown: matchedBytes - matchedBytesUp,
        ipBytes,
        ipBytesUp,
        ipBytesDown: ipBytes - ipBytesUp,
        series,
        observedBytes: ipBytes,
        accuracy: "near-exact",
        generatedAt: new Date().toISOString(),
      }
    }

    const response = await fetch(
      `${API_BASE_URL}/statistics/dns/ip?ip=${encodeURIComponent(ip)}&window=${window}`
    )
    if (response.status === 400) {
      throw new Error("IP ไม่ถูกต้อง")
    }
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS IP reverse lookup: ${response.statusText}`)
    }
    return response.json()
  },
}
