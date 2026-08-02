import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { mockHosts, mockDests, mockIpDomains, type TopHost } from "./statisticsService"

// Statistics -> Traffic page (docs/ref/todo/statistics-traffic-page-plan.md
// T-07) — backs the new /statistics/traffic (top-lists) and
// /statistics/traffic/host/:ip (drill-down) pages. Types mirror
// backend/internal/model/statistics.go's TrafficTopHosts /
// TrafficHostConversation / TrafficHostDetail field-for-field; do not let
// these drift from the Go structs (see docs/openapi.yaml for the source of
// truth). TopHost is re-exported from statisticsService.ts rather than
// redeclared here (plan T-07: "the two clients can never drift"). Same
// RAM-only, never-persisted data source as statisticsService.ts's
// TrafficStatistics — nothing here survives a pigate restart.

export type { TopHost }

export interface TrafficTopHosts {
  window: "1h" | "24h"
  observedBytes: number
  accuracy: "estimated" | "near-exact"
  truncated: boolean
  limit: number
  sources: TopHost[]
  destinations: TopHost[]
  generatedAt: string
}

export interface TrafficHostConversation {
  srcIp: string
  srcHostname: string
  dstIp: string
  dstHostname: string
  // "TCP"/"UDP"/"ICMP", or "IP:<n>" for anything else.
  proto: string
  dstPort: number
  bytes: number
  // Relative to TrafficHostDetail.totalBytes (the DRILLED IP's own total),
  // NOT observedBytes — a different denominator than TopHost.percent /
  // TopConversation.percent.
  percent: number
  // bytesUp/bytesDown stay flow-relative (srcIp -> dstIp / dstIp -> srcIp)
  // exactly like TopConversation — direction below is the ONLY IP-relative
  // field, never re-derive up/down from it.
  bytesUp: number
  bytesDown: number
  dstDomain: string
  // "outbound" when the drilled IP is this row's srcIp (asSource), "inbound"
  // when it is the dstIp (asDestination).
  direction: "outbound" | "inbound"
  // The reverse-DNS-cache domain of the OTHER side of the conversation
  // relative to the drilled IP: equals dstDomain for an outbound row, but for
  // an inbound row it is the srcIp's domain (which dstDomain cannot express).
  peerDomain: string
}

export interface TrafficHostDetail {
  ip: string
  hostname: string
  mac: string
  domain: string
  private: boolean
  window: "1h" | "24h"
  accuracy: "estimated" | "near-exact"
  truncated: boolean
  limit: number
  // False when ip appears nowhere in the window's buckets — render an
  // explicit "no data" state, not an empty table.
  found: boolean
  // This IP's total across BOTH directions (as source PLUS as destination).
  totalBytes: number
  totalBytesUp: number
  totalBytesDown: number
  // totalBytes as a percent of observedBytes — a DIFFERENT denominator than
  // each row's own percent above.
  percentOfObserved: number
  observedBytes: number
  asSource: TrafficHostConversation[]
  asDestination: TrafficHostConversation[]
  generatedAt: string
}

// percentOf mirrors the backend's service/statistics_traffic.go percentOf
// (via traffic_stats.go): one decimal place, 0 when total is 0.
function percentOf(part: number, total: number): number {
  if (total === 0) return 0
  return Math.round((part / total) * 1000) / 10
}

// --- Mock data -------------------------------------------------------------
// mockExtraSources/mockExtraDests extend statisticsService's mockHosts/
// mockDests with more synthetic rows purely so THIS page's filter/sort/
// drill-down UX is actually exercisable in `yarn dev` (plan T-07: "~25-40
// host rows and ~40 conversation rows").
const mockExtraSources = Array.from({ length: 12 }, (_, i) => ({
  ip: `192.168.1.${110 + i}`,
  hostname: `Device-${110 + i}`,
  mac: `AA:11:${(110 + i).toString(16).padStart(2, "0")}:00:00:0${i % 10}`,
}))

const mockExtraDests = [
  { ip: "142.250.196.14", hostname: "142.250.196.14" },
  { ip: "104.16.132.229", hostname: "104.16.132.229" },
  { ip: "13.107.42.14", hostname: "13.107.42.14" },
  { ip: "17.253.144.10", hostname: "17.253.144.10" },
  { ip: "31.13.71.36", hostname: "31.13.71.36" },
  { ip: "199.232.44.164", hostname: "199.232.44.164" },
  { ip: "203.0.113.77", hostname: "203.0.113.77" },
  { ip: "198.51.100.23", hostname: "198.51.100.23" },
  { ip: "52.84.150.21", hostname: "52.84.150.21" },
  // IPv6 destination (plan T-07: "at least one IPv6 row").
  { ip: "2606:4700:4700::1111", hostname: "2606:4700:4700::1111" },
]

const allSourceIps = [...mockHosts.map((h) => h.ip), ...mockExtraSources.map((s) => s.ip)]
const allDestIps = [...mockDests.map((d) => d.ip), ...mockExtraDests.map((d) => d.ip)]

const lanHostByIp = new Map<string, { hostname: string; mac: string }>()
for (const h of mockHosts) lanHostByIp.set(h.ip, { hostname: h.hostname, mac: h.mac })
for (const s of mockExtraSources) lanHostByIp.set(s.ip, { hostname: s.hostname, mac: s.mac })

const wanHostByIp = new Map<string, string>()
for (const d of mockDests) wanHostByIp.set(d.ip, d.hostname)
for (const d of mockExtraDests) wanHostByIp.set(d.ip, d.hostname)

function mockHostnameFor(ip: string): { hostname: string; mac: string } {
  const lan = lanHostByIp.get(ip)
  if (lan) return lan
  const wan = wanHostByIp.get(ip)
  if (wan !== undefined) return { hostname: wan, mac: "" }
  return { hostname: ip, mac: "" }
}

function mockIsPrivate(ip: string): boolean {
  return lanHostByIp.has(ip)
}

interface MockConvRaw {
  srcIp: string
  dstIp: string
  proto: string
  dstPort: number
  bytesBase: number
  upRatio: number
}

// mockConversationsRaw is the SINGLE source of truth both getTopHosts and
// getHostDetail derive from in mock mode (plan T-07: "drilling into a host
// shown on page 1 always yields matching data"). Explicit special-case rows
// first (an IPv6 row, a UDP/443 row, an ICMP row, and a destination reached
// by two different LAN sources — 173.194.76.94 — so the reverse drill-down
// has something to show), then a deterministically generated set covering
// every source against two destinations to reach ~35-40 total rows.
const mockConversationsRaw: MockConvRaw[] = [
  { srcIp: "192.168.1.101", dstIp: "2606:4700:4700::1111", proto: "UDP", dstPort: 53, bytesBase: 40_000, upRatio: 0.5 },
  { srcIp: "192.168.1.102", dstIp: "142.250.80.46", proto: "UDP", dstPort: 443, bytesBase: 5_500_000, upRatio: 0.2 },
  { srcIp: "192.168.1.105", dstIp: "8.8.8.8", proto: "ICMP", dstPort: 0, bytesBase: 6_000, upRatio: 0.5 },
  { srcIp: "192.168.1.102", dstIp: "173.194.76.94", proto: "TCP", dstPort: 443, bytesBase: 26_000_000, upRatio: 0.08 },
  { srcIp: "192.168.1.110", dstIp: "173.194.76.94", proto: "TCP", dstPort: 443, bytesBase: 9_800_000, upRatio: 0.1 },
]

const mockProtoPool: { proto: string; port: number }[] = [
  { proto: "TCP", port: 443 },
  { proto: "TCP", port: 80 },
  { proto: "UDP", port: 53 },
  { proto: "TCP", port: 22 },
]

for (let i = 0; i < allSourceIps.length; i++) {
  for (let j = 0; j < 2; j++) {
    const src = allSourceIps[i]
    const dst = allDestIps[(i * 2 + j) % allDestIps.length]
    if (mockConversationsRaw.some((c) => c.srcIp === src && c.dstIp === dst)) continue
    const { proto, port } = mockProtoPool[(i + j) % mockProtoPool.length]
    mockConversationsRaw.push({
      srcIp: src,
      dstIp: dst,
      proto,
      dstPort: port,
      bytesBase: 500_000 + ((i * 37 + j * 911) % 4_000_000),
      upRatio: 0.1 + ((i + j) % 5) * 0.15,
    })
  }
}

function mockScaledConversations(scale: number) {
  return mockConversationsRaw.map((c) => {
    const bytes = Math.round(c.bytesBase * scale)
    const bytesUp = Math.round(bytes * c.upRatio)
    return { ...c, bytes, bytesUp, bytesDown: bytes - bytesUp }
  })
}

function mockBuildTopHosts(
  rows: ReturnType<typeof mockScaledConversations>,
  key: "srcIp" | "dstIp",
  observedBytes: number
): TopHost[] {
  const totals = new Map<string, { bytes: number; bytesUp: number; bytesDown: number }>()
  for (const r of rows) {
    const ip = r[key]
    const cur = totals.get(ip) ?? { bytes: 0, bytesUp: 0, bytesDown: 0 }
    cur.bytes += r.bytes
    cur.bytesUp += r.bytesUp
    cur.bytesDown += r.bytesDown
    totals.set(ip, cur)
  }
  const out: TopHost[] = Array.from(totals.entries()).map(([ip, v]) => {
    const { hostname, mac } = mockHostnameFor(ip)
    return {
      ip,
      hostname,
      mac,
      bytes: v.bytes,
      percent: percentOf(v.bytes, observedBytes),
      bytesUp: v.bytesUp,
      bytesDown: v.bytesDown,
      private: mockIsPrivate(ip),
      domain: mockIpDomains[ip] ?? "",
    }
  })
  out.sort((a, b) => b.bytes - a.bytes || a.ip.localeCompare(b.ip))
  return out
}

export const trafficStatisticsService = {
  // GET /api/statistics/traffic/hosts — full Top Source Hosts / Top
  // Destinations lists (up to `limit` rows each).
  getTopHosts: async (window: "1h" | "24h" = "1h", limit = 100): Promise<TrafficTopHosts> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      const scale = window === "24h" ? 18 : 1
      const rows = mockScaledConversations(scale)
      const observedBytes = rows.reduce((sum, r) => sum + r.bytes, 0)
      const sources = mockBuildTopHosts(rows, "srcIp", observedBytes).slice(0, limit)
      const destinations = mockBuildTopHosts(rows, "dstIp", observedBytes).slice(0, limit)

      return {
        window,
        observedBytes,
        accuracy: "near-exact",
        truncated: false,
        limit,
        sources,
        destinations,
        generatedAt: new Date().toISOString(),
      }
    }

    const params = new URLSearchParams({ window, limit: String(limit) })
    const response = await fetch(`${API_BASE_URL}/statistics/traffic/hosts?${params.toString()}`)
    if (!response.ok) {
      throw new Error(`Failed to fetch traffic top hosts: ${response.statusText}`)
    }
    return response.json()
  },

  // GET /api/statistics/traffic/host?ip=… — per-IP drill-down, both
  // directions. `ip` is passed RAW (decoded) — encoding happens here, the
  // only place it should (plan T-07/§5 Caution 6: never double-encode).
  getHostDetail: async (ip: string, window: "1h" | "24h" = "1h", limit = 100): Promise<TrafficHostDetail> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      const scale = window === "24h" ? 18 : 1
      const rows = mockScaledConversations(scale)
      const observedBytes = rows.reduce((sum, r) => sum + r.bytes, 0)

      const asSourceAll: TrafficHostConversation[] = []
      const asDestinationAll: TrafficHostConversation[] = []
      let totalBytes = 0
      let totalUp = 0
      let totalDown = 0

      for (const r of rows) {
        if (r.srcIp === ip) {
          totalBytes += r.bytes
          totalUp += r.bytesUp
          totalDown += r.bytesDown
          const { hostname: srcHostname } = mockHostnameFor(r.srcIp)
          const { hostname: dstHostname } = mockHostnameFor(r.dstIp)
          asSourceAll.push({
            srcIp: r.srcIp,
            srcHostname,
            dstIp: r.dstIp,
            dstHostname,
            proto: r.proto,
            dstPort: r.dstPort,
            bytes: r.bytes,
            percent: 0,
            bytesUp: r.bytesUp,
            bytesDown: r.bytesDown,
            dstDomain: mockIpDomains[r.dstIp] ?? "",
            direction: "outbound",
            peerDomain: mockIpDomains[r.dstIp] ?? "",
          })
        }
        if (r.dstIp === ip) {
          totalBytes += r.bytes
          totalUp += r.bytesUp
          totalDown += r.bytesDown
          const { hostname: srcHostname } = mockHostnameFor(r.srcIp)
          const { hostname: dstHostname } = mockHostnameFor(r.dstIp)
          asDestinationAll.push({
            srcIp: r.srcIp,
            srcHostname,
            dstIp: r.dstIp,
            dstHostname,
            proto: r.proto,
            dstPort: r.dstPort,
            bytes: r.bytes,
            percent: 0,
            bytesUp: r.bytesUp,
            bytesDown: r.bytesDown,
            dstDomain: mockIpDomains[r.dstIp] ?? "",
            direction: "inbound",
            peerDomain: mockIpDomains[r.srcIp] ?? "",
          })
        }
      }

      const finish = (list: TrafficHostConversation[]): TrafficHostConversation[] => {
        const withPercent = list.map((r) => ({ ...r, percent: percentOf(r.bytes, totalBytes) }))
        withPercent.sort((a, b) => b.bytes - a.bytes || a.dstIp.localeCompare(b.dstIp) || a.dstPort - b.dstPort)
        return withPercent.slice(0, limit)
      }

      const asSource = finish(asSourceAll)
      const asDestination = finish(asDestinationAll)
      const found = asSourceAll.length > 0 || asDestinationAll.length > 0
      const { hostname, mac } = mockHostnameFor(ip)

      return {
        ip,
        hostname,
        mac,
        domain: mockIpDomains[ip] ?? "",
        private: mockIsPrivate(ip),
        window,
        accuracy: "near-exact",
        truncated: false,
        limit,
        found,
        totalBytes,
        totalBytesUp: totalUp,
        totalBytesDown: totalDown,
        percentOfObserved: percentOf(totalBytes, observedBytes),
        observedBytes,
        asSource,
        asDestination,
        generatedAt: new Date().toISOString(),
      }
    }

    const params = new URLSearchParams({ window, limit: String(limit) })
    const response = await fetch(`${API_BASE_URL}/statistics/traffic/host?ip=${encodeURIComponent(ip)}&${params.toString()}`)
    if (!response.ok) {
      throw new Error(`Failed to fetch traffic host detail: ${response.statusText}`)
    }
    return response.json()
  },
}
