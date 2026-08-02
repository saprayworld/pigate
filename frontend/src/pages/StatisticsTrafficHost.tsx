import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { NavLink, Navigate, useNavigate, useParams, useSearchParams } from "react-router-dom"
import { ArrowLeft, RefreshCw } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import { Table, TableBody, TableCell, TableHeader, TableRow } from "@/components/ui/table"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes } from "@/lib/formatBytes"
import {
  trafficStatisticsService,
  type BandwidthPoint,
  type TrafficHostConversation,
  type TrafficHostDetail,
} from "@/services/trafficStatisticsService"
import { useStatsWindow, StatsWindowSelect, type StatsWindow } from "@/components/statistics/DnsStatsShared"
import { BandwidthTrendCard } from "@/components/statistics/BandwidthTrendCard"
import {
  AccuracyBadge,
  HostBar,
  SortableHead,
  TrafficEmptyState,
  TrafficFilterInput,
  TruncatedWarning,
  useSortableRows,
  useTextFilter,
} from "@/components/statistics/TrafficStatsShared"

// /statistics/traffic/host/:ip — page 2 of the Statistics -> Traffic feature
// (docs/ref/todo/statistics-traffic-page-plan.md T-10): a single IP's
// conversations in BOTH directions (asSource / asDestination tabs), with
// cross drill-down between peers.

const REFRESH_INTERVAL_MS = 10_000
const ROWS_LIMIT = 100

type Role = "src" | "dst"

function roleFromParam(v: string | null): Role | null {
  return v === "src" || v === "dst" ? v : null
}

// peerOf/peerHostnameOf pick the "other side" of a conversation row relative
// to which tab it is shown on (asSource rows are keyed by dstIp, asDestination
// rows by srcIp).
function peerIp(row: TrafficHostConversation, ownIsSrc: boolean): string {
  return ownIsSrc ? row.dstIp : row.srcIp
}
function peerHostname(row: TrafficHostConversation, ownIsSrc: boolean): string {
  return ownIsSrc ? row.dstHostname : row.srcHostname
}

function ConversationTable({
  rows,
  ownIsSrc,
  window_,
  series,
}: {
  rows: TrafficHostConversation[]
  ownIsSrc: boolean
  window_: StatsWindow
  // series is THIS drilled IP's own bandwidth-over-time data (plan
  // docs/ref/todo/statistics-traffic-bandwidth-chart-plan.md T-08) — the SAME
  // series for both tabs (it already covers both directions combined, same
  // as totalBytes), passed down here (rather than lifted to page level)
  // deliberately so it renders alongside "Top peers", which stays computed
  // from the text-filtered rows below (plan Caution 5 — moving that logic up
  // to the page would break "Top peers follows the search box").
  series: BandwidthPoint[]
}) {
  const navigate = useNavigate()
  const [query, setQuery] = useState("")
  const filtered = useTextFilter(rows, query, [
    (r) => peerIp(r, ownIsSrc),
    (r) => peerHostname(r, ownIsSrc),
    (r) => r.peerDomain,
    (r) => r.proto,
    (r) => r.dstPort,
  ])
  const { rows: sorted, sort, toggle } = useSortableRows<TrafficHostConversation>(filtered, {
    key: "bytes",
    dir: "desc",
  })

  const topPeers = useMemo(() => {
    const totals = new Map<string, { hostname: string; domain: string; bytes: number }>()
    let sum = 0
    for (const r of filtered) {
      const ip = peerIp(r, ownIsSrc)
      const cur = totals.get(ip) ?? { hostname: peerHostname(r, ownIsSrc), domain: r.peerDomain, bytes: 0 }
      cur.bytes += r.bytes
      totals.set(ip, cur)
      sum += r.bytes
    }
    return Array.from(totals.entries())
      .map(([ip, v]) => ({ ip, ...v, percent: sum > 0 ? Math.round((v.bytes / sum) * 1000) / 10 : 0 }))
      .sort((a, b) => b.bytes - a.bytes)
      .slice(0, 5)
  }, [filtered, ownIsSrc])

  const goToPeer = (ip: string) => {
    // Cross drill-down: an inbound peer (someone who came TO this IP) is
    // itself viewed as a source on its own page; an outbound peer (a
    // destination this IP went to) is viewed as a destination.
    const nextRole: Role = ownIsSrc ? "dst" : "src"
    navigate(`/statistics/traffic/host/${encodeURIComponent(ip)}?window=${window_}&role=${nextRole}`)
  }

  return (
    <div className="space-y-4">
      {/* Bandwidth (per-IP, flow-relative) + Top peers, side by side (plan
          docs/ref/todo/statistics-traffic-bandwidth-chart-plan.md T-08) —
          `xl` (not `lg` like the Overview page) because this grid sits
          inside a <Card> + TabsContent with the 16rem sidebar already eating
          into the viewport, so the chart needs more room before it gets its
          own row (plan §5 item 6). Top peers keeps its own logic untouched
          (computed from `filtered` above, not lifted out of this
          component). */}
      <div className="grid grid-cols-1 gap-4 xl:grid-cols-3">
        <BandwidthTrendCard
          className={topPeers.length > 0 ? "xl:col-span-2" : "xl:col-span-3"}
          series={series}
          window={window_}
          subtitle="ยอดต่อ 5 นาที เฉพาะทราฟฟิกของ IP นี้ · Up/Down นับตามทิศทางของ flow เหมือนคอลัมน์ Up/Down ในตารางด้านล่าง"
        />
        {topPeers.length > 0 && (
          <div className="space-y-2 rounded-lg border bg-muted/20 p-3 xl:col-span-1">
            <p className="text-xs font-medium text-muted-foreground">Top peers</p>
            {topPeers.map((p) => (
              <div key={p.ip} className="space-y-1">
                <div className="flex items-center justify-between gap-3 text-xs">
                  <span className="min-w-0 truncate">
                    {p.domain || p.hostname || p.ip}
                    {(p.domain || p.hostname) && p.hostname !== p.ip && (
                      <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{p.ip}</span>
                    )}
                  </span>
                  <span className="shrink-0 font-mono text-muted-foreground">
                    {fmtBytes(p.bytes)} · {p.percent}%
                  </span>
                </div>
                <HostBar percent={p.percent} />
              </div>
            ))}
          </div>
        )}
      </div>

      <TrafficFilterInput value={query} onChange={setQuery} placeholder="ค้นหา IP, hostname, domain, proto, port..." />

      {rows.length === 0 ? (
        <TrafficEmptyState label="ไม่พบข้อมูลในทิศทางนี้สำหรับช่วงเวลาที่เลือก" />
      ) : (
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <SortableHead<TrafficHostConversation> label="Peer" sortKey={ownIsSrc ? "dstIp" : "srcIp"} sort={sort} onToggle={toggle} />
                <SortableHead<TrafficHostConversation> label="Proto" sortKey="proto" sort={sort} onToggle={toggle} className="w-20" />
                <SortableHead<TrafficHostConversation> label="Port" sortKey="dstPort" sort={sort} onToggle={toggle} align="right" className="w-20" />
                <SortableHead<TrafficHostConversation> label="Down" sortKey="bytesDown" sort={sort} onToggle={toggle} align="right" className="w-24" />
                <SortableHead<TrafficHostConversation> label="Up" sortKey="bytesUp" sort={sort} onToggle={toggle} align="right" className="w-24" />
                <SortableHead<TrafficHostConversation> label="Total" sortKey="bytes" sort={sort} onToggle={toggle} align="right" className="w-24" />
                <SortableHead<TrafficHostConversation> label="%" sortKey="percent" sort={sort} onToggle={toggle} align="right" className="w-16" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {sorted.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="py-8 text-center text-xs text-muted-foreground">
                    ไม่พบรายการที่ตรงกับคำค้นหา
                  </TableCell>
                </TableRow>
              ) : (
                sorted.map((r, i) => {
                  const ip = peerIp(r, ownIsSrc)
                  const hostname = peerHostname(r, ownIsSrc)
                  return (
                    <TableRow
                      key={`${r.srcIp}-${r.dstIp}-${r.proto}-${r.dstPort}-${i}`}
                      onClick={() => goToPeer(ip)}
                      role="button"
                      tabIndex={0}
                      onKeyDown={(e) => {
                        if (e.key === "Enter" || e.key === " ") {
                          e.preventDefault()
                          goToPeer(ip)
                        }
                      }}
                      className="cursor-pointer hover:bg-muted/50"
                      title="คลิกเพื่อดูรายละเอียดการเชื่อมต่อของเครื่องนี้"
                    >
                      <TableCell className="py-3 text-xs">
                        {r.peerDomain ? (
                          <>
                            <div className="truncate">{r.peerDomain}</div>
                            <div className="font-mono text-[10px] text-muted-foreground">{ip}</div>
                          </>
                        ) : (
                          <>
                            <div className="truncate">{hostname}</div>
                            {hostname !== ip && <div className="font-mono text-[10px] text-muted-foreground">{ip}</div>}
                          </>
                        )}
                      </TableCell>
                      <TableCell className="py-3 font-mono text-xs">{r.proto}</TableCell>
                      <TableCell className="py-3 text-right font-mono text-xs">{r.dstPort}</TableCell>
                      <TableCell className="py-3 text-right font-mono text-xs text-primary">{fmtBytes(r.bytesDown)}</TableCell>
                      <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{fmtBytes(r.bytesUp)}</TableCell>
                      <TableCell className="py-3 text-right font-mono text-xs text-foreground">{fmtBytes(r.bytes)}</TableCell>
                      <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{r.percent}%</TableCell>
                    </TableRow>
                  )
                })
              )}
            </TableBody>
          </Table>
        </div>
      )}
    </div>
  )
}

export default function StatisticsTrafficHost() {
  const { ip: rawIp } = useParams<{ ip: string }>()
  // useParams() already returns the decoded value — do NOT encodeURIComponent
  // it again before calling the service (it encodes internally). Encoding
  // only happens when BUILDING a link (plan §5 Caution 6).
  const ip = (rawIp ?? "").trim()

  const [window_, setWindow] = useStatsWindow()
  const [searchParams, setSearchParams] = useSearchParams()
  const [data, setData] = useState<TrafficHostDetail | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(
    async (targetIp: string, win: "1h" | "24h", showLoading: boolean, isStale: () => boolean, isNewTarget: boolean) => {
      if (showLoading) setIsLoading(true)
      if (isNewTarget) {
        setData(null)
        setError(null)
      }
      try {
        const result = await trafficStatisticsService.getHostDetail(targetIp, win, ROWS_LIMIT)
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
    },
    []
  )

  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  })

  useEffect(() => {
    if (!ip) return
    let ignore = false
    loadRef.current(ip, window_, true, () => ignore, true)
    const id = setInterval(() => loadRef.current(ip, window_, false, () => ignore, false), REFRESH_INTERVAL_MS)
    return () => {
      ignore = true
      clearInterval(id)
    }
  }, [ip, window_])

  // role: URL-driven (?role=src|dst), defaulting to whichever list is
  // non-empty once the first response lands (preferring "src" when both are).
  // autoRoleWrittenRef (not useState) tracks "have we already written the
  // computed default into the URL" — a ref, not state, so this effect never
  // triggers a cascading re-render from calling a state setter on itself.
  const roleParam = roleFromParam(searchParams.get("role"))
  const autoRoleWrittenRef = useRef(false)
  useEffect(() => {
    if (roleParam || autoRoleWrittenRef.current || !data) return
    autoRoleWrittenRef.current = true
    const next: Role = data.asSource.length === 0 && data.asDestination.length > 0 ? "dst" : "src"
    setSearchParams(
      (prev) => {
        const nextParams = new URLSearchParams(prev)
        nextParams.set("role", next)
        return nextParams
      },
      { replace: true }
    )
  }, [roleParam, data, setSearchParams])

  const role: Role = roleParam ?? "src"
  const setRole = (r: Role) => {
    setSearchParams(
      (prev) => {
        const next = new URLSearchParams(prev)
        next.set("role", r)
        return next
      },
      { replace: true }
    )
  }

  if (!ip) {
    return <Navigate to="/statistics/traffic" replace />
  }

  const title = data?.hostname && data.hostname !== ip ? data.hostname : ip

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="min-w-0 space-y-2">
          <Button asChild variant="outline" size="sm" className="cursor-pointer gap-1.5">
            <NavLink to={`/statistics/traffic?window=${window_}`}>
              <ArrowLeft className="size-4" />
              กลับไปหน้า Traffic
            </NavLink>
          </Button>
          <div className="flex flex-wrap items-center gap-2">
            <h1 className="truncate text-lg font-bold tracking-tight">{title}</h1>
            {title !== ip && <span className="font-mono text-xs text-muted-foreground">{ip}</span>}
            {data?.domain && (
              <Badge variant="outline" className="font-normal border-primary/20 bg-primary/10 text-primary">
                {data.domain}
              </Badge>
            )}
            {data && (
              <Badge
                variant="outline"
                className={
                  data.private
                    ? "font-normal border-primary/30 text-primary"
                    : "font-normal border-muted-foreground/30 text-muted-foreground"
                }
              >
                {data.private ? "LAN" : "Internet"}
              </Badge>
            )}
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StatsWindowSelect value={window_} onChange={setWindow} />
          {data && <AccuracyBadge accuracy={data.accuracy} />}
          <Button
            variant="outline"
            size="sm"
            onClick={() => load(ip, window_, true, () => false, false)}
            disabled={isLoading}
            className="cursor-pointer gap-1.5"
          >
            <RefreshCw className={isLoading ? "h-4 w-4 animate-spin" : "h-4 w-4"} />
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
        <div className="space-y-4">
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            {[0, 1, 2, 3].map((i) => (
              <Skeleton key={i} className="h-20 w-full" />
            ))}
          </div>
          <Skeleton className="h-64 w-full" />
        </div>
      )}

      {data && data.found === false && (
        <Card>
          <CardContent className="space-y-2 py-8 text-center">
            <p className="text-sm text-muted-foreground">ไม่พบข้อมูลของ IP นี้ในช่วงเวลาที่เลือก</p>
            {window_ === "1h" && (
              <p className="text-xs text-muted-foreground">ลองเปลี่ยนช่วงเวลาเป็น 24 ชั่วโมงเพื่อดูข้อมูลย้อนหลังเพิ่มเติม</p>
            )}
          </CardContent>
        </Card>
      )}

      {data && data.found && (
        <>
          <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
            <Card>
              <CardHeader className="space-y-0 pb-2">
                <CardTitle className="text-xs font-medium text-muted-foreground">Total</CardTitle>
              </CardHeader>
              <CardContent className="text-lg font-semibold">{fmtBytes(data.totalBytes)}</CardContent>
            </Card>
            <Card>
              <CardHeader className="space-y-0 pb-2">
                <CardTitle className="text-xs font-medium text-muted-foreground">Down</CardTitle>
              </CardHeader>
              <CardContent className="text-lg font-semibold text-primary">{fmtBytes(data.totalBytesDown)}</CardContent>
            </Card>
            <Card>
              <CardHeader className="space-y-0 pb-2">
                <CardTitle className="text-xs font-medium text-muted-foreground">Up</CardTitle>
              </CardHeader>
              <CardContent className="text-lg font-semibold">{fmtBytes(data.totalBytesUp)}</CardContent>
            </Card>
            <Card>
              <CardHeader className="space-y-0 pb-2">
                <CardTitle className="text-xs font-medium text-muted-foreground">% ของทราฟฟิกทั้งหมด</CardTitle>
              </CardHeader>
              <CardContent className="text-lg font-semibold">{data.percentOfObserved}%</CardContent>
            </Card>
          </div>
          <p className="text-[11px] text-muted-foreground">
            หมายเหตุ: % ในตารางด้านล่างคือสัดส่วนเทียบกับทราฟฟิกรวมของ {title} เองเท่านั้น ไม่ใช่ % ของทราฟฟิกทั้งเครือข่าย
            (ซึ่งคือค่า "% ของทราฟฟิกทั้งหมด" ด้านบน)
          </p>

          <Card>
            <CardContent className="pt-4">
              <div className="flex items-center gap-1.5 text-xs font-semibold text-muted-foreground mb-2">View As</div>
              <Tabs value={role} onValueChange={(v) => setRole(v as Role)}>
                <TabsList>
                  <TabsTrigger value="src">Source · {data.asSource.length}</TabsTrigger>
                  <TabsTrigger value="dst">Destination · {data.asDestination.length}</TabsTrigger>
                </TabsList>
                <TabsContent value="src" className="pt-3">
                  <ConversationTable rows={data.asSource} ownIsSrc={true} window_={window_} series={data.series} />
                </TabsContent>
                <TabsContent value="dst" className="pt-3">
                  <ConversationTable rows={data.asDestination} ownIsSrc={false} window_={window_} series={data.series} />
                </TabsContent>
              </Tabs>
            </CardContent>
          </Card>
        </>
      )}

      <p className="text-center text-[11px] text-muted-foreground">
        Auto-refresh ทุก 10 วินาที · ข้อมูลนี้แสดงพฤติกรรมการใช้งานอินเทอร์เน็ตของอุปกรณ์นี้ เก็บใน RAM เท่านั้น
        (restart pigate แล้วจะเริ่มนับใหม่)
      </p>
    </div>
  )
}
