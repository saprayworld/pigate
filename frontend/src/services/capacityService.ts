import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { STATS_WINDOWS, type StatsWindow } from "@/lib/statsWindow"

// Statistics -> Capacity service (docs/ref/todo/
// statistics-capacity-visibility-plan.md T-10, GitHub issue #123) — backs
// the CapacityIndicator pills on Overview/Traffic/DNS and the standalone
// /statistics/capacity page. Types mirror backend/internal/model/statistics.go's
// RingCapacity/CapacityPoint/CapacityStatistics field-for-field; do not let
// these drift from the Go structs (see docs/openapi.yaml for the source of
// truth). Same RAM-only, never-persisted data source as the rest of the
// Statistics pages — nothing here survives a pigate restart, and this
// response never contains a domain name, IP address, or hostname.

export interface CapacityPoint {
  ts: string
  count: number
}

export type CapacityRingGroup = "traffic" | "dns" | "firewall"
export type CapacityRingKind = "bucket" | "flat"

export interface RingCapacity {
  id: string
  group: CapacityRingGroup
  label: string
  kind: CapacityRingKind
  cap: number
  capSource: string
  current: number
  currentPercent: number
  peak: number
  peakPercent: number
  fullBuckets: number
  truncated: boolean
  // APPROXIMATE RAM footprint (entries currently held x entryBytes) — NEVER
  // a measured value. For a "bucket" ring this reflects the WHOLE ring (up
  // to 24h of history), not just the requested window.
  estimatedBytes: number
  entryBytes: number
  // Only present when the request asked for it (series: true) AND
  // kind === "bucket".
  series?: CapacityPoint[]
}

export interface CapacityStatistics {
  window: StatsWindow
  bucketCount: number
  // Always exactly 10 entries, in a fixed order (traffic.hosts, traffic.dests,
  // traffic.conversations, firewall.denySources, firewall.denyPorts,
  // dns.pairs, dns.clients, dns.reverseCache, dns.domainIps,
  // dns.domainIpsPerDomain).
  rings: RingCapacity[]
  generatedAt: string
}

function bucketCountFor(window: StatsWindow): number {
  return STATS_WINDOWS.find((w) => w.value === window)?.points ?? 12
}

// mockRingSpecs mirrors backend/internal/service/statistics_capacity.go's
// metadata table (id/group/label/kind/capSource/entryBytes) field-for-field,
// plus a target currentPercent/peakPercent/fullBuckets used ONLY by the mock
// generator below — chosen so mock mode always exercises every status color
// at once: dns.pairs sits in the warn zone (>=70%), traffic.conversations is
// pinned at 100% + truncated (the danger/full case), everything else is a
// comfortable "ok" percentage.
const mockRingSpecs: {
  id: string
  group: CapacityRingGroup
  label: string
  kind: CapacityRingKind
  capSource: string
  entryBytes: number
  cap: number
  currentPercent: number
  peakPercent: number
  truncated: boolean
}[] = [
  { id: "traffic.hosts", group: "traffic", label: "Top Source Hosts", kind: "bucket", capSource: "traffic-stats-max-hosts", entryBytes: 55, cap: 500, currentPercent: 22, peakPercent: 35, truncated: false },
  { id: "traffic.dests", group: "traffic", label: "Top Destinations", kind: "bucket", capSource: "traffic-stats-max-dests", entryBytes: 55, cap: 500, currentPercent: 30, peakPercent: 44, truncated: false },
  { id: "traffic.conversations", group: "traffic", label: "Top Conversations", kind: "bucket", capSource: "traffic-stats-max-conversations", entryBytes: 110, cap: 600, currentPercent: 100, peakPercent: 100, truncated: true },
  { id: "firewall.denySources", group: "firewall", label: "Top Denied Sources", kind: "bucket", capSource: "deny-stats-max-sources", entryBytes: 47, cap: 500, currentPercent: 12, peakPercent: 18, truncated: false },
  { id: "firewall.denyPorts", group: "firewall", label: "Top Denied Ports", kind: "bucket", capSource: "deny-stats-max-ports", entryBytes: 42, cap: 300, currentPercent: 9, peakPercent: 15, truncated: false },
  { id: "dns.pairs", group: "dns", label: "DNS (domain, client) pairs", kind: "bucket", capSource: "dns-stats-max-pairs", entryBytes: 110, cap: 2400, currentPercent: 74, peakPercent: 82, truncated: false },
  { id: "dns.clients", group: "dns", label: "DNS distinct clients", kind: "bucket", capSource: "dns-stats-max-clients", entryBytes: 47, cap: 200, currentPercent: 18, peakPercent: 25, truncated: false },
  { id: "dns.reverseCache", group: "dns", label: "DNS reverse cache (IP -> domain)", kind: "flat", capSource: "DNS Server > Settings (DNS Cache Max Entries)", entryBytes: 90, cap: 1000, currentPercent: 41, peakPercent: 41, truncated: false },
  { id: "dns.domainIps", group: "dns", label: "DNS domain -> resolved IP index", kind: "flat", capSource: "dns-stats-max-domains", entryBytes: 2000, cap: 1000, currentPercent: 27, peakPercent: 27, truncated: false },
  { id: "dns.domainIpsPerDomain", group: "dns", label: "DNS resolved IPs ต่อโดเมน (สูงสุด)", kind: "flat", capSource: "dns-stats-max-ips-per-domain", entryBytes: 55, cap: 32, currentPercent: 50, peakPercent: 50, truncated: false },
]

function mockSeries(window: StatsWindow, current: number, peak: number): CapacityPoint[] {
  const n = bucketCountFor(window)
  const points: CapacityPoint[] = []
  const now = Date.now()
  for (let i = 0; i < n; i++) {
    const ts = new Date(now - (n - 1 - i) * 5 * 60_000).toISOString()
    // Deterministic ramp from ~40% of current up to peak, ending at current
    // for the newest (last) point — just enough shape for the chart to look
    // like real ring activity, no randomness (keeps mock mode reproducible).
    const isLast = i === n - 1
    const ramped = Math.round(current * 0.4 + (peak - current * 0.4) * (i / Math.max(1, n - 1)))
    points.push({ ts, count: isLast ? current : Math.min(peak, Math.max(0, ramped)) })
  }
  return points
}

function buildMockCapacityStatistics(window: StatsWindow, withSeries: boolean): CapacityStatistics {
  const bucketCount = bucketCountFor(window)
  const rings: RingCapacity[] = mockRingSpecs.map((spec) => {
    const current = Math.round((spec.cap * spec.currentPercent) / 100)
    const peak = Math.max(current, Math.round((spec.cap * spec.peakPercent) / 100))
    const fullBuckets = spec.kind === "bucket" && spec.truncated ? Math.max(1, Math.round(bucketCount * 0.15)) : 0
    // estimatedBytes: for a bucket ring, approximate the whole 24h ring
    // (288 buckets) holding roughly `current` entries each — the SAME
    // whole-ring-not-window convention the real backend uses; for a flat
    // index it's just current x entryBytes.
    const totalEntries = spec.kind === "bucket" ? current * 288 : current
    const ring: RingCapacity = {
      id: spec.id,
      group: spec.group,
      label: spec.label,
      kind: spec.kind,
      cap: spec.cap,
      capSource: spec.capSource,
      current,
      currentPercent: spec.currentPercent,
      peak,
      peakPercent: spec.peakPercent,
      fullBuckets,
      truncated: spec.truncated || fullBuckets > 0,
      estimatedBytes: totalEntries * spec.entryBytes,
      entryBytes: spec.entryBytes,
    }
    if (withSeries && spec.kind === "bucket") {
      ring.series = mockSeries(window, current, peak)
    }
    return ring
  })

  return {
    window,
    bucketCount,
    rings,
    generatedAt: new Date().toISOString(),
  }
}

export const capacityService = {
  // GET /api/statistics/capacity
  getCapacityStatistics: async (
    window: StatsWindow = "1h",
    opts?: { series?: boolean }
  ): Promise<CapacityStatistics> => {
    const withSeries = opts?.series ?? false

    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 120))
      return buildMockCapacityStatistics(window, withSeries)
    }

    const params = new URLSearchParams({ window })
    if (withSeries) params.set("series", "1")
    const response = await fetch(`${API_BASE_URL}/statistics/capacity?${params.toString()}`)
    if (!response.ok) {
      throw new Error(`Failed to fetch capacity statistics: ${response.statusText}`)
    }
    return response.json()
  },
}
