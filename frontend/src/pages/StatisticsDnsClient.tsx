import { useCallback, useEffect, useRef, useState } from "react"
import { NavLink, Navigate, useNavigate, useParams } from "react-router"
import { ArrowLeft, Loader2, RefreshCw } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes } from "@/lib/formatBytes"
import {
  dnsStatisticsService,
  type DNSClientDrilldown,
} from "@/services/dnsStatisticsService"
import {
  useStatsWindow,
  StatsWindowTabs,
  DomainStatsTable,
  DnsStatsPrivacyNote,
  DnsVolumeInfoButton,
  type StatsWindow,
} from "@/components/statistics/DnsStatsShared"
import { TrafficStatCard } from "@/components/statistics/TrafficStatsShared"
import { TrafficTrendCard } from "@/components/statistics/TrafficTrendCard"

// Client drill-down page (docs/ref/todo/statistics-nav-restructure-plan.md
// T-04, extended by docs/ref/todo/statistics-dns-page-revamp-plan.md T-12) —
// replaces the former Dialog drill-down: which domains a given client
// queried, reached via a Top Source Hosts row click on /statistics/dns.
// `client` may legitimately be the literal string "unknown" (a real bucket,
// not an error) — it is fetched normally, only its LABEL renders as
// "ไม่ทราบต้นทาง", and — per plan §1.1/Caution 9 — the Volume stat card and
// TrafficTrendCard are hidden entirely for it: there's no IP to join against
// conntrack for the unknown bucket, so showing a "0 B" volume would be
// actively misleading rather than merely empty.
const REFRESH_INTERVAL_MS = 10_000

export default function StatisticsDnsClient() {
  const navigate = useNavigate()
  const { client: rawClient } = useParams<{ client: string }>()
  // useParams() already returns the decoded value — do NOT encodeURIComponent
  // it again before calling the service (it encodes internally).
  const client = (rawClient ?? "").trim()
  const [window_, setWindow] = useStatsWindow()
  const [data, setData] = useState<DNSClientDrilldown | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (c: string, win: StatsWindow, showLoading: boolean, isStale: () => boolean, isNewTarget: boolean) => {
    if (showLoading) setIsLoading(true)
    // A fresh client/window means the previous drill-down's data is no
    // longer relevant — drop it before fetching so the table never briefly
    // shows the wrong target's rows.
    if (isNewTarget) {
      setData(null)
      setError(null)
    }
    try {
      const result = await dnsStatisticsService.getClientDomains(c, win)
      if (isStale()) return
      setData(result)
      setError(null)
    } catch (err) {
      if (isStale()) return
      if (showLoading) {
        setError(getErrorMessage(err))
      }
      // Background refresh errors are swallowed — keep showing the last
      // known snapshot rather than flashing an error on every failed poll.
    } finally {
      if (!isStale() && showLoading) setIsLoading(false)
    }
  }, [])

  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  })

  useEffect(() => {
    if (!client) return
    let ignore = false
    loadRef.current(client, window_, true, () => ignore, true)
    const id = setInterval(() => loadRef.current(client, window_, false, () => ignore, false), REFRESH_INTERVAL_MS)
    return () => {
      ignore = true
      clearInterval(id)
    }
  }, [client, window_])

  if (!client) {
    return <Navigate to="/statistics/dns" replace />
  }

  const title = client === "unknown"
    ? "ไม่ทราบต้นทาง"
    : `${data?.hostname ? `${data.hostname} ` : ""}(${client})`

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 space-y-2">
          <Button asChild variant="outline" size="sm" className="cursor-pointer gap-1.5">
            <NavLink to={`/statistics/dns?window=${window_}`}>
              <ArrowLeft className="size-4" />
              กลับไปหน้า DNS Statistics
            </NavLink>
          </Button>
          <h1 className="truncate text-lg font-bold tracking-tight">
            โดเมนที่ {title} ค้นหา
          </h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatsWindowTabs value={window_} onChange={setWindow} />
          <DnsVolumeInfoButton />
          <Button
            variant="outline"
            size="sm"
            onClick={() => load(client, window_, true, () => false, false)}
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

      {error && !data && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-destructive">{error}</CardContent>
        </Card>
      )}

      {isLoading && !data ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
        </div>
      ) : !error || data ? (
        <>
          <div className={`grid grid-cols-2 gap-3 ${client === "unknown" ? "lg:grid-cols-2" : "lg:grid-cols-3"}`}>
            <TrafficStatCard label={`Total Queries (${window_})`} value={String(data?.totalQueries ?? 0)} />
            <TrafficStatCard label="Domains" value={String(data?.domains.length ?? 0)} />
            {client !== "unknown" && (
              <TrafficStatCard
                label="Volume"
                value={fmtBytes(data?.totalBytes ?? 0)}
                breakdown={{ down: fmtBytes(data?.totalBytesDown ?? 0), up: fmtBytes(data?.totalBytesUp ?? 0) }}
              />
            )}
          </div>

          {/* No IP exists for the "unknown" bucket, so there's nothing to
              join against conntrack — the trend chart is skipped entirely
              rather than rendering a flat zero line. */}
          {client !== "unknown" && (
            <TrafficTrendCard
              series={data?.series}
              window={window_}
              subtitle="ยอดต่อ 5 นาที เฉพาะทราฟฟิกของเครื่องนี้ · Up/Down นับตามทิศทางของ flow ตรงกับบรรทัด Down/Up ในคอลัมน์ Traffic ของตารางด้านล่าง"
            />
          )}

          <Card>
            <CardContent>
              <DomainStatsTable
                rows={data?.domains ?? []}
                emptyLabel="ไม่พบโดเมนที่เครื่องนี้ค้นหาในช่วงเวลานี้"
                onRowClick={(domain) => navigate(`/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}`)}
              />
            </CardContent>
          </Card>
        </>
      ) : null}

      <DnsStatsPrivacyNote />
    </div>
  )
}
