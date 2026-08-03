import { useCallback, useEffect, useRef, useState } from "react"
import { NavLink, useNavigate } from "react-router-dom"
import { BarChart3, Loader2, RefreshCw, Settings2 } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { getErrorMessage } from "@/lib/errors"
import {
  dnsStatisticsService,
  type DNSQueryStatistics,
} from "@/services/dnsStatisticsService"
import {
  useStatsWindow,
  StatsWindowTabs,
  DomainStatsTable,
  ClientStatsTable,
  DnsStatsPrivacyNote,
  DnsStatsTruncatedWarning,
  type StatsWindow,
} from "@/components/statistics/DnsStatsShared"

// DNS Statistics page (docs/ref/todo/statistics-nav-restructure-plan.md T-02)
// — promoted from the DNS Server page's former "สถิติ" tab
// (components/dns/DnsStatisticsTab.tsx, now deleted) into a standalone route.
// Row clicks navigate to a dedicated drill-down page instead of opening a
// Dialog (plan §2.5).
const REFRESH_INTERVAL_MS = 10_000

export default function StatisticsDns() {
  const navigate = useNavigate()
  const [window_, setWindow] = useStatsWindow()
  const [stats, setStats] = useState<DNSQueryStatistics | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

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

  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  })

  useEffect(() => {
    loadRef.current(window_, true)
    const id = setInterval(() => loadRef.current(window_, false), REFRESH_INTERVAL_MS)
    return () => clearInterval(id)
  }, [window_])

  const generatedAtLabel = stats?.generatedAt
    ? new Date(stats.generatedAt).toLocaleTimeString("th-TH", { hour12: false })
    : "-"

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

      {stats?.truncated && <DnsStatsTruncatedWarning />}

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
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader className="space-y-0">
              <CardTitle className="text-base font-semibold">Top Domains</CardTitle>
            </CardHeader>
            <CardContent>
              <DomainStatsTable
                rows={stats?.topDomains ?? []}
                emptyLabel="ยังไม่มีข้อมูลโดเมนในช่วงเวลานี้"
                onRowClick={(domain) => navigate(`/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}`)}
              />
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
      )}

      <DnsStatsPrivacyNote />
    </div>
  )
}
