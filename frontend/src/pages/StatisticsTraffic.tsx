import { useCallback, useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { ArrowDown, ArrowLeftRight, ArrowUp, ChevronsUpDown, RefreshCw } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes, fmtRate } from "@/lib/formatBytes"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { trafficStatisticsService, type TopHost, type TrafficTopHosts } from "@/services/trafficStatisticsService"
import { useStatsWindow, StatsWindowSelect } from "@/components/statistics/DnsStatsShared"
import { HostLabel } from "@/components/statistics/HostCells"
import { BandwidthTrendCard } from "@/components/statistics/BandwidthTrendCard"
import { TopHostsShareCard } from "@/components/statistics/TopHostsShareCard"
import {
  AccuracyBadge,
  SortableHead,
  TrafficEmptyState,
  TrafficFilterInput,
  TruncatedWarning,
  useSortableRows,
  useTextFilter,
} from "@/components/statistics/TrafficStatsShared"

// /statistics/traffic — page 1 of the Statistics -> Traffic feature
// (docs/ref/todo/statistics-traffic-page-plan.md T-09): full Top Source
// Hosts / Top Destinations tables (not the Overview page's top-10 cut),
// client-side filter + sort, row click drills into /statistics/traffic/host/:ip.

const REFRESH_INTERVAL = 10_000
const ROWS_LIMIT = 100
const DISPLAY_LIMIT = 50

type HostRow = TopHost & { rateTotal: number }

function HostsTable({
  title,
  hosts,
  role,
  window_,
  emptyLabel,
}: {
  title: string
  hosts: TopHost[]
  role: "src" | "dst"
  window_: "1h" | "24h"
  emptyLabel: string
}) {
  const navigate = useNavigate()
  const [query, setQuery] = useState("")
  const withRateTotal = hosts.map((h) => ({ ...h, rateTotal: (h.rateBpsDown ?? 0) + (h.rateBpsUp ?? 0) }))
  const filtered = useTextFilter(withRateTotal, query, [
    (h) => h.ip,
    (h) => h.hostname,
    (h) => h.domain,
  ])
  const { rows, sort, toggle } = useSortableRows(filtered, { key: "bytes", dir: "desc" })
  const shown = rows.slice(0, DISPLAY_LIMIT)

  const goToHost = (ip: string) => {
    navigate(`/statistics/traffic/host/${encodeURIComponent(ip)}?window=${window_}&role=${role}`)
  }

  return (
    <Card>
      <CardHeader className="space-y-3">
        <CardTitle className="text-base font-semibold">{title}</CardTitle>
        <TrafficFilterInput value={query} onChange={setQuery} placeholder="ค้นหา IP, hostname, domain..." />
      </CardHeader>
      <CardContent>
        {hosts.length === 0 ? (
          <TrafficEmptyState label={emptyLabel} />
        ) : (
          <>
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <SortableHead<HostRow> label="Host" sortKey="hostname" sort={sort} onToggle={toggle} />
                    <SortableHead<HostRow> label="Down" sortKey="bytesDown" sort={sort} onToggle={toggle} align="right" className="w-24" />
                    <SortableHead<HostRow> label="Up" sortKey="bytesUp" sort={sort} onToggle={toggle} align="right" className="w-24" />
                    <SortableHead<HostRow> label="Total" sortKey="bytes" sort={sort} onToggle={toggle} align="right" className="w-24" />
                    <SortableHead<HostRow> label="%" sortKey="percent" sort={sort} onToggle={toggle} align="right" className="w-16" />
                    <TableHead className="hidden w-28 md:table-cell">
                      <Tooltip>
                        <TooltipTrigger asChild>
                          <button
                            type="button"
                            onClick={() => toggle("rateTotal")}
                            className="inline-flex w-full cursor-pointer items-center justify-end gap-1 text-xs font-medium text-muted-foreground hover:text-foreground"
                          >
                            Speed
                            {sort.key === "rateTotal" ? (
                              sort.dir === "asc" ? (
                                <ArrowUp className="size-3 text-primary" />
                              ) : (
                                <ArrowDown className="size-3 text-primary" />
                              )
                            ) : (
                              <ChevronsUpDown className="size-3 text-muted-foreground/60" />
                            )}
                          </button>
                        </TooltipTrigger>
                        <TooltipContent>
                          ความเร็วเฉลี่ยประมาณ 10 วินาทีล่าสุด (ค่าประมาณจาก conntrack) · เรียงจาก Down+Up
                        </TooltipContent>
                      </Tooltip>
                    </TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {shown.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={6} className="py-8 text-center text-xs text-muted-foreground">
                        ไม่พบรายการที่ตรงกับคำค้นหา
                      </TableCell>
                    </TableRow>
                  ) : (
                    shown.map((h) => (
                      <TableRow
                        key={h.ip}
                        onClick={() => goToHost(h.ip)}
                        role="button"
                        tabIndex={0}
                        onKeyDown={(e) => {
                          if (e.key === "Enter" || e.key === " ") {
                            e.preventDefault()
                            goToHost(h.ip)
                          }
                        }}
                        className="cursor-pointer hover:bg-muted/50"
                        title="คลิกเพื่อดูรายละเอียดการเชื่อมต่อของเครื่องนี้"
                      >
                        <TableCell className="py-3 text-xs">
                          <HostLabel host={h} />
                        </TableCell>
                        <TableCell className="py-3 text-right font-mono text-xs text-primary">{fmtBytes(h.bytesDown)}</TableCell>
                        <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{fmtBytes(h.bytesUp)}</TableCell>
                        <TableCell className="py-3 text-right font-mono text-xs text-foreground">{fmtBytes(h.bytes)}</TableCell>
                        <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{h.percent}%</TableCell>
                        <TableCell className="hidden py-3 text-right font-mono text-[11px] leading-tight text-muted-foreground md:table-cell">
                          <div className="text-primary">{h.rateBpsDown !== undefined ? fmtRate(h.rateBpsDown) : "—"}</div>
                          <div>{h.rateBpsUp !== undefined ? fmtRate(h.rateBpsUp) : "—"}</div>
                        </TableCell>
                      </TableRow>
                    ))
                  )}
                </TableBody>
              </Table>
            </div>
            <p className="pt-2 text-[11px] text-muted-foreground">
              แสดง {shown.length} จาก {rows.length} รายการ
            </p>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function CardSkeleton() {
  return (
    <div className="space-y-3">
      {Array.from({ length: 6 }).map((_, i) => (
        <Skeleton key={i} className="h-8 w-full" />
      ))}
    </div>
  )
}

export default function StatisticsTraffic() {
  const [window_, setWindow] = useStatsWindow()
  const [data, setData] = useState<TrafficTopHosts | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (win: "1h" | "24h", showLoading: boolean) => {
    if (showLoading) setIsLoading(true)
    try {
      const result = await trafficStatisticsService.getTopHosts(win, ROWS_LIMIT)
      setData(result)
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
    const id = setInterval(() => loadRef.current(window_, false), REFRESH_INTERVAL)
    return () => clearInterval(id)
  }, [window_])

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <ArrowLeftRight className="size-5" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight">Traffic</h1>
            <p className="text-xs text-muted-foreground">
              ดูว่าเครื่องไหนใช้เน็ตมากที่สุด และวิ่งไปหาปลายทางไหน
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StatsWindowSelect value={window_} onChange={setWindow} />
          {data && <AccuracyBadge accuracy={data.accuracy} />}
          <Button variant="outline" size="sm" onClick={() => load(window_, true)} disabled={isLoading}>
            <RefreshCw className={isLoading ? "size-4 animate-spin" : "size-4"} />
            Refresh
          </Button>
        </div>
      </div>

      {data?.truncated && <TruncatedWarning />}

      {error && !data && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-destructive">{error}</CardContent>
        </Card>
      )}

      {isLoading && !data && (
        <>
          <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
            <Card className="lg:col-span-2">
              <CardHeader>
                <Skeleton className="h-5 w-40" />
              </CardHeader>
              <CardContent>
                <Skeleton className="h-56 w-full" />
              </CardContent>
            </Card>
            <Card>
              <CardHeader>
                <Skeleton className="h-5 w-40" />
              </CardHeader>
              <CardContent>
                <CardSkeleton />
              </CardContent>
            </Card>
          </div>
          <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
            {[0, 1].map((i) => (
              <Card key={i}>
                <CardHeader>
                  <Skeleton className="h-5 w-40" />
                </CardHeader>
                <CardContent>
                  <CardSkeleton />
                </CardContent>
              </Card>
            ))}
          </div>
        </>
      )}

      {/* Bandwidth trend + Top 5 Hosts (docs/ref/todo/
          statistics-traffic-bandwidth-chart-plan.md T-07) — same
          network-wide/LAN-relative data and layout as the Overview page
          (StatisticsOverview.tsx ~:463-472), so no `subtitle` override is
          passed here. Rendered outside the `data &&` guard's sibling tables
          block is unnecessary here (unlike Overview) since this page has no
          separate "no data" empty state to worry about hiding it behind. */}
      {data && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <BandwidthTrendCard className="lg:col-span-2" series={data.series} window={window_} />
          <TopHostsShareCard
            className="lg:col-span-1"
            hosts={data.sources.slice(0, 5)}
            observedBytes={data.observedBytes}
          />
        </div>
      )}

      {data && (
        <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
          <HostsTable
            title="Top Source Hosts"
            hosts={data.sources}
            role="src"
            window_={window_}
            emptyLabel="ยังไม่มีข้อมูล source host ในช่วงเวลานี้"
          />
          <HostsTable
            title="Top Destinations"
            hosts={data.destinations}
            role="dst"
            window_={window_}
            emptyLabel="ยังไม่มีข้อมูล destination ในช่วงเวลานี้"
          />
        </div>
      )}

      <p className="text-center text-[11px] text-muted-foreground">
        Auto-refresh ทุก 10 วินาที · Observed: {data ? fmtBytes(data.observedBytes) : "-"} · หนึ่งแถวคือ "คู่สนทนา"
        (ต้นทาง-ปลายทาง-โปรโตคอล-พอร์ต) รวมทุกการเชื่อมต่อของบริการเดียวกันเข้าด้วยกัน ไม่ใช่การเชื่อมต่อ TCP รายเส้น
      </p>
    </div>
  )
}
