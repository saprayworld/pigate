import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { type TopDomain } from "./statisticsService"
import { mockWindowScale, type StatsWindow } from "@/lib/statsWindow"

// DNS Query Statistics tab (docs/ref/todo/dns-query-statistics-drilldown-plan.md
// T-07) — backs the "สถิติ" tab on the DNS Server page. Types mirror
// backend/internal/model/statistics.go's DNSClientStat / DNSQueryStatistics /
// DNSDomainDrilldown / DNSClientDrilldown field-for-field; do not let these
// drift from the Go structs (see docs/openapi.yaml for the source of truth).
// Same RAM-only, display-only, opt-in (dns/settings.queryLogging) data source
// as TrafficStatistics.topDomains in statisticsService.ts — nothing here is
// ever persisted, and a pigate restart resets all counts to zero.

export type { TopDomain }

export interface DNSClientStat {
  ip: string
  hostname: string
  count: number
  percent: number
}

export interface DNSQueryStatistics {
  window: StatsWindow
  enabled: boolean
  totalQueries: number
  truncated: boolean
  topDomains: TopDomain[]
  topClients: DNSClientStat[]
  generatedAt: string
}

export interface DNSDomainDrilldown {
  domain: string
  window: StatsWindow
  enabled: boolean
  totalQueries: number
  truncated: boolean
  clients: DNSClientStat[]
  generatedAt: string
}

export interface DNSClientDrilldown {
  client: string
  hostname: string
  window: StatsWindow
  enabled: boolean
  totalQueries: number
  truncated: boolean
  domains: TopDomain[]
  generatedAt: string
}

// mockHostnames mirrors statisticsService.ts's mockHosts and kernel/mock.go's
// mockDNSQueryEvents matrix (same 3 LAN client IPs).
const mockHostnames: Record<string, string> = {
  "192.168.1.101": "iPhone-13",
  "192.168.1.102": "Android-SmartTV",
  "192.168.1.105": "iPad-Pro",
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

function mockCount(weight: number, scale: number): number {
  return Math.max(1, Math.round(weight * 10 * scale))
}

function normalizeDomain(domain: string): string {
  return domain.trim().toLowerCase().replace(/\.$/, "")
}

function normalizeClient(client: string): string {
  const trimmed = client.trim()
  return trimmed === "unknown" ? trimmed : trimmed.toLowerCase()
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

      const domainTotals = new Map<string, { count: number; queryType: string }>()
      const clientTotals = new Map<string, number>()
      let totalQueries = 0
      for (const p of mockPairs) {
        const count = mockCount(p.weight, scale)
        totalQueries += count
        const d = domainTotals.get(p.domain)
        domainTotals.set(p.domain, { count: (d?.count ?? 0) + count, queryType: p.queryType })
        clientTotals.set(p.client, (clientTotals.get(p.client) ?? 0) + count)
      }

      const topDomains: TopDomain[] = Array.from(domainTotals.entries())
        .map(([domain, v]) => ({
          domain,
          queryType: v.queryType,
          count: v.count,
          percent: totalQueries > 0 ? Math.round((v.count / totalQueries) * 1000) / 10 : 0,
        }))
        .sort((a, b) => b.count - a.count || a.domain.localeCompare(b.domain))

      const topClients: DNSClientStat[] = Array.from(clientTotals.entries())
        .map(([ip, count]) => ({
          ip,
          hostname: mockHostnames[ip] ?? "",
          count,
          percent: totalQueries > 0 ? Math.round((count / totalQueries) * 1000) / 10 : 0,
        }))
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
        generatedAt: new Date().toISOString(),
      }
    }

    const response = await fetch(`${API_BASE_URL}/statistics/dns?window=${window}`)
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS query statistics: ${response.statusText}`)
    }
    return response.json()
  },

  // GET /api/statistics/dns/domain — clients that queried a given domain.
  getDomainClients: async (domain: string, window: StatsWindow = "1h"): Promise<DNSDomainDrilldown> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      // mockCount below already Math.max(1, Math.round(...))s its own
      // weight*10*scale product, so a fractional scale (e.g. 15m -> 0.3) is
      // safe to pass through unrounded here (plan §6 item 4).
      const scale = mockWindowScale(window)
      const target = normalizeDomain(domain)

      const clientTotals = new Map<string, number>()
      let totalQueries = 0
      for (const p of mockPairs) {
        if (p.domain !== target) continue
        const count = mockCount(p.weight, scale)
        totalQueries += count
        clientTotals.set(p.client, (clientTotals.get(p.client) ?? 0) + count)
      }

      const clients: DNSClientStat[] = Array.from(clientTotals.entries())
        .map(([ip, count]) => ({
          ip,
          hostname: mockHostnames[ip] ?? "",
          count,
          percent: totalQueries > 0 ? Math.round((count / totalQueries) * 1000) / 10 : 0,
        }))
        .sort((a, b) => b.count - a.count || a.ip.localeCompare(b.ip))

      return {
        domain: target,
        window,
        enabled: true,
        totalQueries,
        truncated: false,
        clients,
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

  // GET /api/statistics/dns/client — domains a given client queried.
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

      const domains: TopDomain[] = Array.from(domainTotals.entries())
        .map(([d, v]) => ({
          domain: d,
          queryType: v.queryType,
          count: v.count,
          percent: totalQueries > 0 ? Math.round((v.count / totalQueries) * 1000) / 10 : 0,
        }))
        .sort((a, b) => b.count - a.count || a.domain.localeCompare(b.domain))

      return {
        client: target,
        hostname: mockHostnames[target] ?? "",
        window,
        enabled: true,
        totalQueries,
        truncated: false,
        domains,
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
}
