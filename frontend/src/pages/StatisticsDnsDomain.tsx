import { useCallback, useEffect, useRef, useState } from "react"
import { NavLink, Navigate, useNavigate, useParams } from "react-router-dom"
import { ArrowLeft, Loader2, RefreshCw } from "lucide-react"
import { Card, CardContent } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { getErrorMessage } from "@/lib/errors"
import {
  dnsStatisticsService,
  type DNSDomainDrilldown,
} from "@/services/dnsStatisticsService"
import {
  useStatsWindow,
  StatsWindowTabs,
  ClientStatsTable,
  DnsStatsPrivacyNote,
  type StatsWindow,
} from "@/components/statistics/DnsStatsShared"

// Domain drill-down page (docs/ref/todo/statistics-nav-restructure-plan.md
// T-03) — replaces the former Dialog drill-down: which hosts queried a given
// domain, reached via a Top Domains row click on /statistics/dns.
const REFRESH_INTERVAL_MS = 10_000

export default function StatisticsDnsDomain() {
  const navigate = useNavigate()
  const { domain: rawDomain } = useParams<{ domain: string }>()
  // useParams() already returns the decoded value — do NOT encodeURIComponent
  // it again before calling the service (it encodes internally).
  const domain = (rawDomain ?? "").trim()
  const [window_, setWindow] = useStatsWindow()
  const [data, setData] = useState<DNSDomainDrilldown | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (d: string, win: StatsWindow, showLoading: boolean, isStale: () => boolean, isNewTarget: boolean) => {
    if (showLoading) setIsLoading(true)
    // A fresh domain/window means the previous drill-down's data is no
    // longer relevant — drop it before fetching so the table never briefly
    // shows the wrong target's rows.
    if (isNewTarget) {
      setData(null)
      setError(null)
    }
    try {
      const result = await dnsStatisticsService.getDomainClients(d, win)
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
    if (!domain) return
    let ignore = false
    loadRef.current(domain, window_, true, () => ignore, true)
    const id = setInterval(() => loadRef.current(domain, window_, false, () => ignore, false), REFRESH_INTERVAL_MS)
    return () => {
      ignore = true
      clearInterval(id)
    }
  }, [domain, window_])

  if (!domain) {
    return <Navigate to="/statistics/dns" replace />
  }

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
            Source Hosts ที่ค้นหา <span className="font-mono">{domain}</span>
          </h1>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <StatsWindowTabs value={window_} onChange={setWindow} />
          <Button
            variant="outline"
            size="sm"
            onClick={() => load(domain, window_, true, () => false, false)}
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
        <Card>
          <CardContent>
            <ClientStatsTable
              rows={data?.clients ?? []}
              emptyLabel="ไม่พบเครื่องที่ค้นหาโดเมนนี้ในช่วงเวลานี้"
              onRowClick={(ip) => navigate(`/statistics/dns/client/${encodeURIComponent(ip)}?window=${window_}`)}
            />
          </CardContent>
        </Card>
      ) : null}

      <DnsStatsPrivacyNote />
    </div>
  )
}
