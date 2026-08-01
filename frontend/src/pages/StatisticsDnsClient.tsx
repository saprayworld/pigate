import { useCallback, useEffect, useRef, useState } from "react"
import { NavLink, Navigate, useNavigate, useParams } from "react-router-dom"
import { ArrowLeft, Loader2, RefreshCw } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { getErrorMessage } from "@/lib/errors"
import {
  dnsStatisticsService,
  type DNSClientDrilldown,
} from "@/services/dnsStatisticsService"
import {
  useStatsWindow,
  StatsWindowSelect,
  DomainStatsTable,
  DnsStatsPrivacyNote,
} from "@/components/statistics/DnsStatsShared"

// Client drill-down page (docs/ref/todo/statistics-nav-restructure-plan.md
// T-04) — replaces the former Dialog drill-down: which domains a given
// client queried, reached via a Top Source Hosts row click on
// /statistics/dns. `client` may legitimately be the literal string "unknown"
// (a real bucket, not an error) — it is fetched normally, only its LABEL
// renders as "ไม่ทราบต้นทาง".
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

  const load = useCallback(async (c: string, win: "1h" | "24h", showLoading: boolean, isStale: () => boolean, isNewTarget: boolean) => {
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
        <div className="flex items-center gap-2">
          <StatsWindowSelect value={window_} onChange={setWindow} />
          <Button
            variant="outline"
            size="sm"
            onClick={() => load(client, window_, true, () => false, false)}
            disabled={isLoading}
            className="cursor-pointer gap-1.5"
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
            Refresh
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
        <Card>
          <CardContent>
            <DomainStatsTable
              rows={data?.domains ?? []}
              emptyLabel="ไม่พบโดเมนที่เครื่องนี้ค้นหาในช่วงเวลานี้"
              onRowClick={(domain) => navigate(`/statistics/dns/domain/${encodeURIComponent(domain)}?window=${window_}`)}
            />
          </CardContent>
        </Card>
      ) : null}

      <DnsStatsPrivacyNote />
    </div>
  )
}
