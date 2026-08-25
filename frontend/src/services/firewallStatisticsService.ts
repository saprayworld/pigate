import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { policyService } from "./policyService"
import { type PolicyChain, type PolicyRule } from "@/data-mockup/mockData"
import { STATS_WINDOWS, mockWindowScale, type StatsWindow } from "@/lib/statsWindow"
import { type TopDeniedSource, type TopDeniedPort } from "./statisticsService"

// Statistics -> Firewall page (docs/ref/todo/statistics-firewall-page-plan.md
// T-09) — backs GET /api/statistics/firewall. Types mirror
// backend/internal/model/firewall_stats.go field-for-field (see
// docs/openapi.yaml for the source of truth). Same RAM-only, never-persisted
// data source as every other Statistics endpoint — nothing here survives a
// pigate restart. Two independently-sourced, differently-scoped byte/event
// accountings are combined here — NEVER mix them into one percentage:
//  - acceptedBytes/acceptedPackets/blockedBytes/blockedPackets and every
//    trend/chains/rules figure come from the nft per-rule counter (exact,
//    but ONLY for traffic that matched a user-defined policy rule — traffic
//    dropped by the board's built-in default-drop is NOT included).
//  - blockedEvents and every denyTrend/blockedSources/blockedPorts/
//    recentBlockedEvents figure come from NFLOG (an event count, which DOES
//    include default-drop traffic).

export type { TopDeniedSource, TopDeniedPort }

export interface FirewallTrendPoint {
  ts: string
  acceptedBytes: number
  blockedBytes: number
  acceptedPackets: number
  blockedPackets: number
}

export interface FirewallDenyPoint {
  ts: string
  events: number
}

export interface FirewallChainStat {
  chain: PolicyChain
  acceptedBytes: number
  blockedBytes: number
  acceptedPackets: number
  blockedPackets: number
  percent: number
}

export interface FirewallRuleStatRow {
  ruleId: string
  name: string
  chain?: PolicyChain
  action?: "ACCEPT" | "DROP"
  log?: boolean
  monitored?: boolean
  bytes: number
  packets: number
  percent: number
  unused: boolean
  lastMatchedAt?: string
  lastMatchedSource?: "log" | "counter"
}

export interface FirewallBlockedEvent {
  ts: string
  sourceIp: string
  proto: string
  port: string
  chain?: PolicyChain
  ruleName?: string
}

export interface FirewallStatistics {
  window: StatsWindow
  generatedAt: string
  // False until the nft rule-counter poller has completed at least one tick
  // (mirrors PolicyRuleStats.available).
  available: boolean
  // True when the NFLOG deny ring hit one of its per-bucket tracking caps in
  // this window.
  truncated: boolean
  countersSince?: string
  acceptedBytes: number
  acceptedPackets: number
  blockedBytes: number
  blockedPackets: number
  blockedEvents: number
  rulesEnabled: number
  rulesUnused: number
  trend: FirewallTrendPoint[]
  denyTrend: FirewallDenyPoint[]
  chains: FirewallChainStat[]
  rules: FirewallRuleStatRow[]
  blockedSources: TopDeniedSource[]
  blockedPorts: TopDeniedPort[]
  recentBlockedEvents: FirewallBlockedEvent[]
  limit: number
}

// hashId is a small deterministic (non-cryptographic) string hash, used only
// to synthesize reproducible mock data — never random, so a mock-mode dev
// session shows stable numbers across renders/reloads (mirrors
// policyStatsService.ts's own hashId).
function hashId(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) >>> 0
  }
  return h
}

function pointsFor(window: StatsWindow): number {
  return STATS_WINDOWS.find((w) => w.value === window)?.points ?? 12
}

const mockBlockedSourcesRaw = [
  { ip: "203.0.113.9", hostname: "203.0.113.9", count: 42 },
  { ip: "203.0.113.44", hostname: "203.0.113.44", count: 18 },
  { ip: "198.51.100.201", hostname: "198.51.100.201", count: 6 },
]

const mockBlockedPortsRaw = [
  { proto: "TCP", port: "22", count: 30 },
  { proto: "TCP", port: "23", count: 20 },
  { proto: "UDP", port: "3389", count: 12 },
]

// buildMockFirewallStatistics synthesizes a FirewallStatistics response from
// the current mock-mode policy rules (all chains), deterministic per rule id
// (docs/ref/todo/statistics-firewall-page-plan.md T-09): always exercises at
// least one Unused rule (bytes=0) and, when every rule happens to be
// disabled, an available=false response (mirrors the real backend's
// "never polled yet"/"nothing enabled to poll" degrade).
function buildMockFirewallStatistics(rules: PolicyRule[], window: StatsWindow, limit: number): FirewallStatistics {
  const scale = Math.max(1, Math.round(mockWindowScale(window)))
  const enabled = rules.filter((r) => r.status)
  const n = pointsFor(window)
  const spanMs = 5 * 60 * 1000
  const now = Date.now()

  const rows = enabled.map((r) => {
    const h = hashId(r.id)
    const unused = h % 4 === 3
    const bytes = unused ? 0 : (2000 + (h % 30000)) * scale
    const packets = unused ? 0 : Math.max(1, Math.round(bytes / (64 + (h % 900))))
    let lastMatchedAt: string | undefined
    let lastMatchedSource: "log" | "counter" | undefined
    if (!unused) {
      const secondsAgo = 5 + (h % 3600)
      lastMatchedAt = new Date(now - secondsAgo * 1000).toISOString()
      lastMatchedSource = r.log ? "log" : "counter"
    }
    return { rule: r, h, unused, bytes, packets, lastMatchedAt, lastMatchedSource }
  })

  const totalBytes = rows.reduce((sum, r) => sum + r.bytes, 0) || 1
  let acceptedBytes = 0, acceptedPackets = 0, blockedBytes = 0, blockedPackets = 0
  const chainTotals = new Map<PolicyChain, FirewallChainStat>()
  for (const r of rows) {
    const chain = r.rule.chain
    if (!chainTotals.has(chain)) {
      chainTotals.set(chain, { chain, acceptedBytes: 0, blockedBytes: 0, acceptedPackets: 0, blockedPackets: 0, percent: 0 })
    }
    const ct = chainTotals.get(chain)!
    if (r.rule.action === "ACCEPT") {
      acceptedBytes += r.bytes
      acceptedPackets += r.packets
      ct.acceptedBytes += r.bytes
      ct.acceptedPackets += r.packets
    } else {
      blockedBytes += r.bytes
      blockedPackets += r.packets
      ct.blockedBytes += r.bytes
      ct.blockedPackets += r.packets
    }
  }
  const chainByteTotal = acceptedBytes + blockedBytes || 1
  const chains = Array.from(chainTotals.values())
    .map((ct) => ({ ...ct, percent: Math.round(((ct.acceptedBytes + ct.blockedBytes) / chainByteTotal) * 1000) / 10 }))
    .sort((a, b) => a.chain.localeCompare(b.chain))

  const ruleRows: FirewallRuleStatRow[] = rows
    .map((r) => ({
      ruleId: r.rule.id,
      name: r.rule.name,
      chain: r.rule.chain,
      action: r.rule.action,
      log: r.rule.log,
      monitored: r.rule.monitored,
      bytes: r.bytes,
      packets: r.packets,
      percent: Math.round((r.bytes / totalBytes) * 1000) / 10,
      unused: r.unused,
      lastMatchedAt: r.lastMatchedAt,
      lastMatchedSource: r.lastMatchedSource,
    }))
    .sort((a, b) => b.bytes - a.bytes || a.ruleId.localeCompare(b.ruleId))
    .slice(0, limit)

  // Accept/drop byte trend (nft-rule-counter-sourced) — a deterministic
  // ramp+sine shape, summing exactly to acceptedBytes/blockedBytes so the
  // sum(trend)==total invariant holds in mock mode too.
  const trendShape: number[] = []
  for (let i = 0; i < n; i++) {
    trendShape.push(0.3 + Math.max(0, Math.sin((i / n) * Math.PI * 4)))
  }
  const shapeSum = trendShape.reduce((a, b) => a + b, 0) || 1
  const trend: FirewallTrendPoint[] = trendShape.map((v, i) => {
    const ts = new Date(now - (n - 1 - i) * spanMs).toISOString()
    return {
      ts,
      acceptedBytes: Math.round((v / shapeSum) * acceptedBytes),
      blockedBytes: Math.round((v / shapeSum) * blockedBytes),
      acceptedPackets: Math.round((v / shapeSum) * acceptedPackets),
      blockedPackets: Math.round((v / shapeSum) * blockedPackets),
    }
  })

  // Blocked-events trend (NFLOG-sourced) — independent unit/shape from
  // `trend` above (deliberately never derived from it).
  const blockedEvents = Math.round(90 * scale)
  const denyShape: number[] = []
  for (let i = 0; i < n; i++) {
    denyShape.push(i % 5 === 0 ? 0 : 0.5 + Math.max(0, Math.cos((i / n) * Math.PI * 3)))
  }
  const denyShapeSum = denyShape.reduce((a, b) => a + b, 0) || 1
  const denyTrend: FirewallDenyPoint[] = denyShape.map((v, i) => ({
    ts: new Date(now - (n - 1 - i) * spanMs).toISOString(),
    events: Math.round((v / denyShapeSum) * blockedEvents),
  }))

  const blockedSourcesTotal = mockBlockedSourcesRaw.reduce((sum, r) => sum + r.count * scale, 0) || 1
  const blockedSources: TopDeniedSource[] = mockBlockedSourcesRaw
    .map((r) => ({ ...r, count: Math.round(r.count * scale), percent: Math.round(((r.count * scale) / blockedSourcesTotal) * 1000) / 10 }))
    .slice(0, limit)

  const blockedPortsTotal = mockBlockedPortsRaw.reduce((sum, r) => sum + r.count * scale, 0) || 1
  const blockedPorts: TopDeniedPort[] = mockBlockedPortsRaw
    .map((r) => ({ ...r, count: Math.round(r.count * scale), percent: Math.round(((r.count * scale) / blockedPortsTotal) * 1000) / 10 }))
    .slice(0, limit)

  const recentBlockedEvents: FirewallBlockedEvent[] = Array.from({ length: Math.min(limit, 8) }, (_, i) => {
    const src = mockBlockedSourcesRaw[i % mockBlockedSourcesRaw.length]
    const port = mockBlockedPortsRaw[i % mockBlockedPortsRaw.length]
    return {
      ts: new Date(now - i * 45_000).toISOString(),
      sourceIp: src.ip,
      proto: port.proto,
      port: port.port,
      chain: (["forward", "input", "output"] as PolicyChain[])[i % 3],
    }
  })

  // available=false whenever there is no enabled rule to have polled counters
  // for at all — mirrors the real backend's "never polled yet" degrade
  // (docs/ref/todo/statistics-firewall-page-plan.md T-09: "ต้องมีเคส
  // available=false").
  const available = enabled.length > 0

  return {
    window,
    generatedAt: new Date().toISOString(),
    available,
    truncated: false,
    countersSince: new Date(now - 90_000).toISOString(),
    acceptedBytes,
    acceptedPackets,
    blockedBytes,
    blockedPackets,
    blockedEvents,
    rulesEnabled: enabled.length,
    rulesUnused: rows.filter((r) => r.unused).length,
    trend,
    denyTrend,
    chains,
    rules: ruleRows,
    blockedSources,
    blockedPorts,
    recentBlockedEvents,
    limit,
  }
}

async function parseError(response: Response, fallback: string): Promise<never> {
  const errBody = await response.json().catch(() => ({}))
  throw new Error(errBody.message || fallback)
}

export const firewallStatisticsService = {
  // GET /api/statistics/firewall
  getFirewallStatistics: async (window: StatsWindow = "1h", limit = 100): Promise<FirewallStatistics> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      const rules = await policyService.getAll()
      return buildMockFirewallStatistics(rules, window, limit)
    }

    const params = new URLSearchParams({ window, limit: String(limit) })
    const response = await fetch(`${API_BASE_URL}/statistics/firewall?${params.toString()}`)
    if (!response.ok) {
      await parseError(response, `Failed to fetch firewall statistics: ${response.statusText}`)
    }
    return response.json()
  },
}
