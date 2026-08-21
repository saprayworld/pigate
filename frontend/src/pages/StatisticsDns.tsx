import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { NavLink, useNavigate, useSearchParams } from "react-router"
import { BarChart3, Loader2, RefreshCw, Search, Settings2, TriangleAlert } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes } from "@/lib/formatBytes"
import { classifyIpQuery } from "@/lib/ipQuery"
import {
  dnsStatisticsService,
  type DNSQueryStatistics,
  type DNSIPDomains,
} from "@/services/dnsStatisticsService"
import {
  useStatsWindow,
  StatsWindowTabs,
  DomainStatsTable,
  ClientStatsTable,
  DnsStatsPrivacyNote,
  DnsStatsTruncatedWarning,
  DnsDomainIndexTruncatedWarning,
  DnsVolumeInfoButton,
  type StatsWindow,
} from "@/components/statistics/DnsStatsShared"
import { BlockedDomainStatsTable, BlockedClientStatsTable } from "@/components/statistics/DnsBlockedStatsShared"
import { TrafficStatCard } from "@/components/statistics/TrafficStatsShared"
import { DnsQueryTrendCard } from "@/components/statistics/DnsQueryTrendCard"
import { DnsBlockedDonutCard } from "@/components/statistics/DnsBlockedDonutCard"
import { CapacityIndicator } from "@/components/statistics/CapacityIndicator"
import { capacityService, type RingCapacity } from "@/services/capacityService"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"

// DNS Statistics page (docs/ref/todo/statistics-nav-restructure-plan.md T-02)
// — promoted from the DNS Server page's former "สถิติ" tab
// (components/dns/DnsStatisticsTab.tsx, now deleted) into a standalone route.
// Row clicks navigate to a dedicated drill-down page instead of opening a
// Dialog (plan §2.5). Restructured in docs/ref/todo/
// statistics-dns-page-revamp-plan.md T-10 to mirror pages/StatisticsTraffic.tsx's
// layout: a stat-card row up top, a DnsVolumeInfoButton next to the window
// tabs, and the sortable/filterable tables from T-09.
//
// IP-filter mode (docs/ref/todo/statistics-dns-ip-filter-plan.md T-09):
// typing a complete IP into the Top Domains filter box switches the table to
// GET /statistics/dns/ip's result — every domain the RAM-only forward index
// remembers resolving to that IP — synced with the `?ip=` URL param so the
// mode survives a refresh/Back and is linkable. Only Top Domains is affected;
// Top Source Hosts and the 4 stat cards above stay on the window-wide
// overview at all times.
//
// DNS query-count bar chart (docs/ref/todo/statistics-dns-query-bar-chart-plan.md
// T-08): DnsQueryTrendCard sits between the stat-card row and the truncated
// warning, sourced from stats.querySeries (no extra request) — like the stat
// cards, it always shows the window-wide overview and is unaffected by
// IP-filter mode.
const REFRESH_INTERVAL_MS = 10_000
const IP_QUERY_DEBOUNCE_MS = 300

export default function StatisticsDns() {
  const navigate = useNavigate()
  const [window_, setWindow] = useStatsWindow()
  const [stats, setStats] = useState<DNSQueryStatistics | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  // Capacity indicator pill (docs/ref/todo/statistics-capacity-visibility-plan.md
  // T-12) — same poll cycle as `stats` below, no extra interval; a failed
  // fetch here just hides CapacityIndicator, never surfaces its own error.
  const [capacityRings, setCapacityRings] = useState<RingCapacity[] | undefined>(undefined)

  const [searchParams, setSearchParams] = useSearchParams()
  const [query, setQuery] = useState(() => searchParams.get("ip") ?? "")
  const classification = useMemo(() => classifyIpQuery(query), [query])
  const [ipData, setIpData] = useState<DNSIPDomains | null>(null)
  const [ipLoading, setIpLoading] = useState(false)
  const [ipError, setIpError] = useState<string | null>(null)

  // Keep the `?ip=` URL param in sync with the query box: present only while
  // a complete IP is entered, so a domain search / partial IP / cleared box
  // never leaves a stale `?ip=` around (plan T-09 item 1). replace: true —
  // same "don't flood history" convention as useStatsWindow.
  useEffect(() => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        if (classification.kind === "ip") {
          next.set("ip", classification.ip)
        } else {
          next.delete("ip")
        }
        return next
      },
      { replace: true }
    )
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [classification.kind, classification.ip])

  const load = useCallback(async (win: StatsWindow, showLoading: boolean) => {
    if (showLoading) setIsLoading(true)
    try {
      const data = await dnsStatisticsService.getDNSStatistics(win)
      setStats(data)
      setError(null)
    } catch (err) {
      if (showLoading) {
        setError(getErrorMessage(err))
      }
      // Background refresh errors are swallowed — keep showing the last
      // known snapshot rather than flashing an error on every failed poll.
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }, [])

  const loadCapacity = useCallback(async (win: StatsWindow) => {
    try {
      const result = await capacityService.getCapacityStatistics(win, { series: false })
      setCapacityRings(result.rings)
    } catch {
      // Swallowed on purpose (plan T-12) — see comment on capacityRings above.
    }
  }, [])

  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  })
  const loadCapacityRef = useRef(loadCapacity)
  useEffect(() => {
    loadCapacityRef.current = loadCapacity
  })

  useEffect(() => {
    loadRef.current(window_, true)
    loadCapacityRef.current(window_)
    const id = setInterval(() => {
      loadRef.current(window_, false)
      loadCapacityRef.current(window_)
    }, REFRESH_INTERVAL_MS)
    return () => clearInterval(id)
  }, [window_])

  // IP-filter mode data: debounced (300ms) + refreshed every 10s while the
  // mode stays active + reloaded on window change, with a stale guard so a
  // slow earlier request can never clobber a later one (plan T-09 item 2,
  // same pattern as pages/StatisticsDnsDomain.tsx).
  const loadIP = useCallback(async (ip: string, win: StatsWindow, showLoading: boolean, isStale: () => boolean) => {
    if (showLoading) setIpLoading(true)
    try {
      const result = await dnsStatisticsService.getIPDomains(ip, win)
      if (isStale()) return
      setIpData(result)
      setIpError(null)
    } catch (err) {
      if (isStale()) return
      setIpError(getErrorMessage(err))
    } finally {
      if (!isStale() && showLoading) setIpLoading(false)
    }
  }, [])

  const loadIPRef = useRef(loadIP)
  useEffect(() => {
    loadIPRef.current = loadIP
  })

  useEffect(() => {
    // Leaving IP-filter mode: nothing to fetch, and the stale ipData/ipError
    // below is never rendered while classification.kind !== "ip" (ipMode
    // gates every read of it) — so there is nothing to clear here, avoiding
    // a synchronous setState-in-effect on every domain/ip-partial keystroke.
    if (classification.kind !== "ip") {
      return
    }
    let ignore = false
    const ip = classification.ip
    const debounce = setTimeout(() => loadIPRef.current(ip, window_, true, () => ignore), IP_QUERY_DEBOUNCE_MS)
    const interval = setInterval(() => loadIPRef.current(ip, window_, false, () => ignore), REFRESH_INTERVAL_MS)
    return () => {
      ignore = true
      clearTimeout(debounce)
      clearInterval(interval)
    }
  }, [classification.kind, classification.ip, window_])

  const generatedAtLabel = stats?.generatedAt
    ? new Date(stats.generatedAt).toLocaleTimeString("th-TH", { hour12: false })
    : "-"

  const ipMode = classification.kind === "ip"

  // Tabs (docs/ref/todo/dns-blocked-query-statistics-plan.md T-13): "Query
  // ปกติ" (queries) vs "Blocked Query" (blocked), synced with `?tab=` —
  // default "queries" when the param is missing/unrecognized, same
  // whitelist-and-fallback convention as pages/DnsServer.tsx's activeTab.
  const VALID_TABS = ["queries", "blocked"] as const
  type DnsStatsTab = (typeof VALID_TABS)[number]
  const initialTab = searchParams.get("tab")
  const [activeTab, setActiveTabState] = useState<DnsStatsTab>(
    VALID_TABS.includes(initialTab as DnsStatsTab) ? (initialTab as DnsStatsTab) : "queries"
  )
  const setActiveTab = (tab: string) => {
    const next = VALID_TABS.includes(tab as DnsStatsTab) ? (tab as DnsStatsTab) : "queries"
    setActiveTabState(next)
    setSearchParams(
      (prev) => {
        const params = new URLSearchParams(prev)
        params.set("tab", next)
        return params
      },
      { replace: true }
    )
  }

  // IP-filter mode only makes sense on the "queries" tab (it drives the Top
  // Domains table there) — typing a complete IP while on "blocked" must
  // bounce back to "queries" automatically (plan T-13 final acceptance).
  // Adjusted directly during render (React's documented pattern for
  // "reacting to a prop/derived-value change" — see
  // https://react.dev/learn/you-might-not-need-an-effect) rather than in a
  // useEffect, which would otherwise trigger a cascading extra render.
  const [prevIpMode, setPrevIpMode] = useState(ipMode)
  if (ipMode !== prevIpMode) {
    setPrevIpMode(ipMode)
    if (ipMode && activeTab === "blocked") {
      setActiveTab("queries")
    }
  }

  return (
    <div className="space-y-4">
      {/* Page header — same visual style as pages/StatisticsOverview.tsx */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <BarChart3 className="size-5" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight">DNS Statistics</h1>
            <p className="text-xs text-muted-foreground">
              Top Domains และ Top Source Hosts จาก DNS query ตามช่วงเวลา — อัปเดตล่าสุด {generatedAtLabel}
            </p>
          </div>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <CapacityIndicator rings={capacityRings} group="dns" window={window_} />
          <DnsVolumeInfoButton />
          <StatsWindowTabs value={window_} onChange={setWindow} />
          <Button
            variant="outline"
            size="sm"
            onClick={() => load(window_, true)}
            disabled={isLoading}
            className="cursor-pointer gap-1.5"
            title="Refresh"
            aria-label="Refresh"
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
            <span className="hidden sm:inline">Refresh</span>
          </Button>
        </div>
      </div>

      {stats && stats.enabled && (
        <div className="grid grid-cols-2 gap-3 lg:grid-cols-5">
          <TrafficStatCard label={`Total Queries (${window_})`} value={stats.totalQueries.toLocaleString()} />
          <TrafficStatCard label="Domains found" value={stats.totalDomains.toLocaleString()} />
          <TrafficStatCard label="Clients found" value={stats.totalClients.toLocaleString()} />
          <TrafficStatCard
            label="Volume (attributable)"
            value={fmtBytes(stats.domainBytes)}
            breakdown={{
              down: fmtBytes(stats.topDomains.reduce((sum, d) => sum + d.bytesDown, 0)),
              up: fmtBytes(stats.topDomains.reduce((sum, d) => sum + d.bytesUp, 0)),
            }}
          />
          <TrafficStatCard
            label="Blocked Queries"
            value={stats.blockedQueries.toLocaleString()}
            hint={`${stats.blockedPercent.toFixed(1)}% ของ query ทั้งหมด`}
          />
        </div>
      )}

      {stats && stats.enabled && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <DnsQueryTrendCard
            series={stats.querySeries}
            blockedSeries={stats.blockedSeries}
            window={window_}
            className="lg:col-span-2"
          />
          <DnsBlockedDonutCard
            totalQueries={stats.totalQueries}
            blockedQueries={stats.blockedQueries}
            blockedPercent={stats.blockedPercent}
          />
        </div>
      )}

      {stats?.truncated && <DnsStatsTruncatedWarning />}
      {stats?.domainIndexTruncated && <DnsDomainIndexTruncatedWarning />}
      {stats?.blockedTruncated && (
        <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
          <TriangleAlert className="h-4 w-4 shrink-0" />
          จำนวนโดเมนที่ถูกบล็อกในช่วงเวลานี้เกินขีดจำกัดการติดตาม — ยอดรวม Blocked Queries ยังถูกต้อง
          แต่ตาราง Top Blocked Domains/Clients ด้านล่างอาจแสดงไม่ครบ
        </div>
      )}

      {error && !stats && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-destructive">{error}</CardContent>
        </Card>
      )}

      {isLoading && !stats ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
        </div>
      ) : stats && !stats.enabled ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center gap-3 py-10 text-center">
            <BarChart3 className="h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm font-medium text-foreground">ยังไม่ได้เปิดการเก็บสถิติ DNS</p>
            <p className="max-w-md text-xs text-muted-foreground">
              เปิดสวิตช์ "เก็บสถิติ DNS query" ในแท็บ Settings เพื่อดูโดเมนและเครื่องที่ค้นหาในหน้านี้ —
              ข้อมูลนี้เก็บไว้ใน RAM เท่านั้น (restart แล้วเริ่มนับใหม่) และเป็นข้อมูลส่วนบุคคลของคนในบ้าน
            </p>
            <Button asChild size="sm" variant="outline" className="cursor-pointer gap-1.5">
              <NavLink to="/network/dns-server?tab=settings">
                <Settings2 className="h-4 w-4" />
                ไปที่หน้า DNS Server &gt; Settings
              </NavLink>
            </Button>
          </CardContent>
        </Card>
      ) : (
        <Tabs value={activeTab} onValueChange={setActiveTab}>
          <TabsList>
            <TabsTrigger value="queries" className="cursor-pointer">Query ปกติ</TabsTrigger>
            <TabsTrigger value="blocked" className="cursor-pointer gap-1.5">
              Blocked Query
              {stats && stats.enabled && stats.totalBlockedDomains > 0 && (
                <Badge variant="outline" className="rounded border-warning/20 bg-warning/10 px-1.5 py-0 text-[10px] font-medium text-warning">
                  {stats.totalBlockedDomains}
                </Badge>
              )}
            </TabsTrigger>
          </TabsList>

          <TabsContent value="queries" className="space-y-4">
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader className="flex-row flex-wrap items-center justify-between gap-2 space-y-0">
              <CardTitle className="text-base font-semibold">
                {ipMode ? (
                  <span className="flex flex-wrap items-center gap-2">
                    โดเมนที่ resolve ไปยัง <span className="font-mono">{classification.ip}</span>
                    <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-medium text-primary">
                      โหมด IP
                    </Badge>
                  </span>
                ) : (
                  "Top Domains"
                )}
              </CardTitle>
              {ipMode && (
                <Button
                  variant="ghost"
                  size="sm"
                  className="cursor-pointer gap-1.5 text-xs"
                  onClick={() => setQuery("")}
                >
                  ล้างตัวกรอง IP
                </Button>
              )}
            </CardHeader>
            <CardContent>
              {ipMode ? (
                ipLoading && !ipData ? (
                  <div className="flex items-center justify-center py-12">
                    <Loader2 className="h-6 w-6 animate-spin text-primary" />
                  </div>
                ) : ipError && !ipData ? (
                  <p className="py-8 text-center text-sm text-destructive">{ipError}</p>
                ) : (
                  <DomainStatsTable
                    rows={ipData?.domains ?? []}
                    emptyLabel={`ไม่พบโดเมนที่ resolve ไปยัง ${classification.ip} ในดัชนีขณะนี้ (อาจเป็นเพราะการจับคู่หมดอายุ (TTL) แล้ว หรือทราฟฟิกไปยัง IP นี้ไม่ได้ผ่านการ resolve ผ่านอุปกรณ์นี้)`}
                    query={query}
                    onQueryChange={setQuery}
                    filterDisabled
                    onRowClick={(domain) => navigate(`/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}`)}
                    banner={
                      <>
                        {ipData?.shared && (
                          <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
                            <TriangleAlert className="h-4 w-4 shrink-0" />
                            IP นี้ถูกใช้ร่วมกัน {ipData.domainCount} โดเมน — ปริมาณข้อมูลของแต่ละโดเมนถูกนับซ้ำจาก
                            IP เดียวกัน
                          </div>
                        )}
                        {ipData?.ipsTruncated && (
                          <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
                            <TriangleAlert className="h-4 w-4 shrink-0" />
                            ดัชนีโดเมน→IP เคยเต็มขีดจำกัดในช่วงที่ผ่านมา — รายชื่อโดเมนของ IP นี้อาจไม่ครบ
                          </div>
                        )}
                        {ipData && ipData.domains.length === 0 && (
                          <div className="flex flex-col items-center gap-3 rounded-lg border border-border py-6 text-center">
                            <Search className="h-6 w-6 text-muted-foreground/50" />
                            <div className="flex flex-wrap justify-center gap-2">
                              <Button asChild size="sm" variant="outline" className="cursor-pointer gap-1.5 text-xs">
                                <NavLink to={`/statistics/dns/client/${encodeURIComponent(classification.ip)}?window=${window_}`}>
                                  ดูเป็น client
                                </NavLink>
                              </Button>
                              <Button asChild size="sm" variant="outline" className="cursor-pointer gap-1.5 text-xs">
                                <NavLink to={`/statistics/traffic/host/${encodeURIComponent(classification.ip)}?window=${window_}`}>
                                  ดูทราฟฟิกของ IP นี้
                                </NavLink>
                              </Button>
                            </div>
                          </div>
                        )}
                      </>
                    }
                    footerNote={
                      <p className="text-[11px] text-muted-foreground">
                        พบ {ipData?.domainCount ?? 0} โดเมน · % Vol เทียบเฉพาะกลุ่มโดเมนที่ใช้ IP นี้ (
                        {fmtBytes(ipData?.matchedBytes ?? 0)} รวม)
                      </p>
                    }
                  />
                )
              ) : (
                <DomainStatsTable
                  rows={stats?.topDomains ?? []}
                  emptyLabel="ยังไม่มีข้อมูลโดเมนในช่วงเวลานี้"
                  query={query}
                  onQueryChange={setQuery}
                  hint={
                    classification.kind === "ip-partial" ? (
                      <p className="text-[11px] text-muted-foreground">พิมพ์ IP ให้ครบเพื่อค้นหาโดเมนที่ resolve ไปยัง IP นี้</p>
                    ) : undefined
                  }
                  onRowClick={(domain) => navigate(`/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}`)}
                />
              )}
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="space-y-0">
              <CardTitle className="text-base font-semibold">Top Source Hosts</CardTitle>
            </CardHeader>
            <CardContent>
              <ClientStatsTable
                rows={stats?.topClients ?? []}
                emptyLabel="ยังไม่มีข้อมูลเครื่องต้นทางในช่วงเวลานี้"
                onRowClick={(ip) => navigate(`/statistics/dns/client/${encodeURIComponent(ip)}?window=${window_}`)}
              />
            </CardContent>
          </Card>
        </div>
          </TabsContent>

          <TabsContent value="blocked" className="space-y-4">
            {stats && stats.totalBlockedDomains === 0 ? (
              <Card>
                <CardContent className="flex flex-col items-center justify-center gap-3 py-10 text-center">
                  <BarChart3 className="h-8 w-8 text-muted-foreground/50" />
                  <p className="text-sm font-medium text-foreground">ยังไม่มี DNS query ที่ถูกบล็อกในช่วงเวลานี้</p>
                  <p className="max-w-md text-xs text-muted-foreground">
                    ตั้งค่า deny-list (Blocked Domains) ในหน้า DNS Server เพื่อเริ่มบล็อกและเก็บสถิติโดเมนที่ถูกบล็อก
                  </p>
                  <Button asChild size="sm" variant="outline" className="cursor-pointer gap-1.5">
                    <NavLink to="/network/dns-server?tab=blocked">
                      <Settings2 className="h-4 w-4" />
                      ไปที่หน้า DNS Server &gt; Blocked Domains
                    </NavLink>
                  </Button>
                </CardContent>
              </Card>
            ) : (
              <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                <Card>
                  <CardHeader className="space-y-0">
                    <CardTitle className="text-base font-semibold">Top Blocked Domains</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <BlockedDomainStatsTable
                      rows={stats?.topBlockedDomains ?? []}
                      emptyLabel="ยังไม่มีข้อมูลโดเมนที่ถูกบล็อกในช่วงเวลานี้"
                      onRowClick={(domain) => navigate(`/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}`)}
                    />
                  </CardContent>
                </Card>

                <Card>
                  <CardHeader className="space-y-0">
                    <CardTitle className="text-base font-semibold">Top Blocked Clients</CardTitle>
                  </CardHeader>
                  <CardContent>
                    <BlockedClientStatsTable
                      rows={stats?.topBlockedClients ?? []}
                      emptyLabel="ยังไม่มีข้อมูลเครื่องที่ถูกบล็อกในช่วงเวลานี้"
                      onRowClick={(ip) => navigate(`/statistics/dns/client/${encodeURIComponent(ip)}?window=${window_}`)}
                    />
                  </CardContent>
                </Card>
              </div>
            )}
          </TabsContent>
        </Tabs>
      )}

      <DnsStatsPrivacyNote />
    </div>
  )
}
