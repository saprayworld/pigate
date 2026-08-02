import { useCallback, useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router-dom"
import { BarChart3, RefreshCw, TriangleAlert } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Skeleton } from "@/components/ui/skeleton"
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { cn } from "@/lib/utils"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes } from "@/lib/formatBytes"
import {
  statisticsService,
  type TopConversation,
  type TopDeniedPort,
  type TopDeniedSource,
  type TopDomain,
  type TopHost,
  type TrafficStatistics,
} from "@/services/statisticsService"
import { useStatsWindow, StatsWindowSelect, type StatsWindow } from "@/components/statistics/DnsStatsShared"
import { UpDownLine, HostLabel } from "@/components/statistics/HostCells"
import { BandwidthTrendCard } from "@/components/statistics/BandwidthTrendCard"
import { TopHostsShareCard } from "@/components/statistics/TopHostsShareCard"

const REFRESH_INTERVAL = 10_000

// AccuracyBadge mirrors the Dashboard "Detailed" tab's badge exactly
// (Dashboard.tsx) — driven by the API's accuracy field, never hardcoded, so
// this page never repeats that fixed-label bug.
function AccuracyBadge({ accuracy }: { accuracy?: "estimated" | "near-exact" }) {
  const isNearExact = accuracy === "near-exact"
  return (
    <Badge
      variant="outline"
      className={cn(
        "font-normal",
        isNearExact ? "border-primary/30 text-primary" : "border-muted-foreground/30 text-muted-foreground"
      )}
    >
      {isNearExact ? "ใกล้เคียงจริง" : "ประมาณการ"}
    </Badge>
  )
}

function SampledBadge() {
  return (
    <Badge variant="outline" className="font-normal border-warning/30 text-warning">
      สุ่มตัวอย่าง
    </Badge>
  )
}

function EmptyState({ label }: { label: string }) {
  return <p className="py-8 text-center text-sm text-muted-foreground">{label}</p>
}

function CardSkeleton() {
  return (
    <div className="space-y-3">
      {Array.from({ length: 4 }).map((_, i) => (
        <Skeleton key={i} className="h-8 w-full" />
      ))}
    </div>
  )
}

function HostBar({ percent }: { percent: number }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
      <div
        className="h-full rounded-full bg-primary transition-all duration-500"
        style={{ width: `${Math.min(100, Math.max(percent, 0))}%` }}
      />
    </div>
  )
}

// UpDownLine/HostLabel moved to @/components/statistics/HostCells (docs/ref/
// todo/statistics-overview-bandwidth-chart-plan.md T-08A) — imported above.

function TopHostsCard({
  title,
  hosts,
  emptyLabel,
}: {
  title: string
  hosts: TopHost[]
  emptyLabel: string
}) {
  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="text-base font-semibold">{title}</CardTitle>
      </CardHeader>
      <CardContent className="space-y-3">
        {hosts.length === 0 ? (
          <EmptyState label={emptyLabel} />
        ) : (
          hosts.map((h) => (
            <div key={h.ip} className="space-y-1">
              <div className="flex items-center justify-between gap-3 text-sm">
                <HostLabel host={h} />
                <span className="shrink-0 text-right">
                  <span className="block font-mono text-xs text-muted-foreground">
                    {fmtBytes(h.bytes)} · {h.percent}%
                  </span>
                  <UpDownLine up={h.bytesUp} down={h.bytesDown} />
                </span>
              </div>
              <HostBar percent={h.percent} />
            </div>
          ))
        )}
      </CardContent>
    </Card>
  )
}

function TopConversationsCard({ conversations }: { conversations: TopConversation[] }) {
  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="text-base font-semibold">Top Conversations</CardTitle>
      </CardHeader>
      <CardContent>
        {conversations.length === 0 ? (
          <EmptyState label="ยังไม่มีข้อมูล conversation ในช่วงเวลานี้" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Source</TableHead>
                  <TableHead>Destination</TableHead>
                  <TableHead className="w-20">Proto</TableHead>
                  <TableHead className="w-20 text-right">Port</TableHead>
                  <TableHead className="w-24 text-right">Down</TableHead>
                  <TableHead className="w-24 text-right">Up</TableHead>
                  <TableHead className="w-24 text-right">Total</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {conversations.map((c, i) => (
                  <TableRow key={`${c.srcIp}-${c.dstIp}-${c.proto}-${c.dstPort}-${i}`}>
                    <TableCell className="text-xs">
                      <div className="truncate">{c.srcHostname}</div>
                      {c.srcHostname !== c.srcIp && (
                        <div className="font-mono text-[10px] text-muted-foreground">{c.srcIp}</div>
                      )}
                    </TableCell>
                    <TableCell className="text-xs">
                      {c.dstDomain ? (
                        <>
                          <div className="truncate">{c.dstDomain}</div>
                          <div className="font-mono text-[10px] text-muted-foreground">{c.dstIp}</div>
                        </>
                      ) : (
                        <>
                          <div className="truncate">{c.dstHostname}</div>
                          {c.dstHostname !== c.dstIp && (
                            <div className="font-mono text-[10px] text-muted-foreground">{c.dstIp}</div>
                          )}
                        </>
                      )}
                    </TableCell>
                    <TableCell className="text-xs font-mono">{c.proto}</TableCell>
                    <TableCell className="text-right text-xs font-mono">{c.dstPort}</TableCell>
                    <TableCell className="text-right text-xs font-mono text-primary">
                      {fmtBytes(c.bytesDown)}
                    </TableCell>
                    <TableCell className="text-right text-xs font-mono text-muted-foreground">
                      {fmtBytes(c.bytesUp)}
                    </TableCell>
                    <TableCell className="text-right text-xs font-mono text-muted-foreground">
                      {fmtBytes(c.bytes)}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </div>
        )}
      </CardContent>
    </Card>
  )
}

// TopDomainsCard is the "Top Queried Domains" card (docs/ref/todo/
// statistics-dns-top-domain-plan.md T-13) — same visual layout as
// TopHostsCard, but with an opt-in-aware empty state (dnsLoggingEnabled)
// instead of the generic "no data yet" message, and a truncation badge.
function TopDomainsCard({
  domains,
  dnsLoggingEnabled,
  dnsTruncated,
  window_,
}: {
  domains: TopDomain[]
  dnsLoggingEnabled: boolean
  dnsTruncated: boolean
  window_: StatsWindow
}) {
  const navigate = useNavigate()
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base font-semibold">Top Queried Domains</CardTitle>
        {dnsTruncated && (
          <Badge variant="outline" className="font-normal border-warning/30 text-warning">
            ข้อมูลอาจไม่ครบ
          </Badge>
        )}
      </CardHeader>
      <CardContent className="space-y-3">
        {!dnsLoggingEnabled ? (
          <EmptyState label="ยังไม่ได้เปิดการเก็บสถิติ DNS — เปิดได้ที่หน้า DNS Server" />
        ) : domains.length === 0 ? (
          <EmptyState label="ยังไม่มีข้อมูล DNS query ในช่วงเวลานี้" />
        ) : (
          <>
            {domains.map((d) => (
              <div key={d.domain} className="space-y-1">
                <div className="flex items-center justify-between gap-3 text-sm">
                  <span className="flex min-w-0 items-center gap-2">
                    <button
                      type="button"
                      onClick={() =>
                        navigate(`/statistics/dns/domain/${encodeURIComponent(d.domain)}?window=${window_}`)
                      }
                      title="คลิกเพื่อดูว่าเครื่องไหนถามโดเมนนี้บ้าง"
                      className="min-w-0 cursor-pointer truncate text-left text-foreground/90 hover:text-primary hover:underline"
                    >
                      {d.domain}
                    </button>
                    <span className="shrink-0 font-mono text-[10px] text-muted-foreground">{d.queryType}</span>
                  </span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">
                    {d.count} · {d.percent}%
                  </span>
                </div>
                <HostBar percent={d.percent} />
              </div>
            ))}
            <p className="pt-1 text-[11px] text-muted-foreground">
              ชื่อโดเมนมาจากที่ DNS ตอบ อาจไม่ตรงกับที่เห็นในเบราว์เซอร์
            </p>
          </>
        )}
      </CardContent>
    </Card>
  )
}

function TopDeniedCard({
  sources,
  ports,
  events,
}: {
  sources: TopDeniedSource[]
  ports: TopDeniedPort[]
  events: number
}) {
  return (
    <Card>
      <CardHeader className="flex flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="text-base font-semibold">Top Denied</CardTitle>
        <div className="flex items-center gap-2">
          <span className="text-xs text-muted-foreground">{events} events</span>
          <SampledBadge />
        </div>
      </CardHeader>
      <CardContent>
        <Tabs defaultValue="sources">
          <TabsList>
            <TabsTrigger value="sources">Sources</TabsTrigger>
            <TabsTrigger value="ports">Ports</TabsTrigger>
          </TabsList>
          <TabsContent value="sources" className="space-y-3 pt-2">
            {sources.length === 0 ? (
              <EmptyState label="ไม่พบการปฏิเสธการเชื่อมต่อในช่วงเวลานี้" />
            ) : (
              sources.map((s) => (
                <div key={s.ip} className="space-y-1">
                  <div className="flex items-center justify-between gap-3 text-sm">
                    <span className="flex min-w-0 items-center gap-2">
                      <span className="truncate text-foreground/90">{s.hostname}</span>
                      {s.hostname !== s.ip && (
                        <span className="shrink-0 font-mono text-xs text-muted-foreground">{s.ip}</span>
                      )}
                    </span>
                    <span className="shrink-0 font-mono text-xs text-muted-foreground">
                      {s.count} · {s.percent}%
                    </span>
                  </div>
                  <HostBar percent={s.percent} />
                </div>
              ))
            )}
          </TabsContent>
          <TabsContent value="ports" className="space-y-3 pt-2">
            {ports.length === 0 ? (
              <EmptyState label="ไม่พบการปฏิเสธการเชื่อมต่อในช่วงเวลานี้" />
            ) : (
              ports.map((p) => (
                <div key={`${p.proto}-${p.port}`} className="space-y-1">
                  <div className="flex items-center justify-between gap-3 text-sm">
                    <span className="font-mono text-foreground/90">
                      {p.proto}/{p.port}
                    </span>
                    <span className="shrink-0 font-mono text-xs text-muted-foreground">
                      {p.count} · {p.percent}%
                    </span>
                  </div>
                  <HostBar percent={p.percent} />
                </div>
              ))
            )}
          </TabsContent>
        </Tabs>
      </CardContent>
    </Card>
  )
}

export default function StatisticsOverview() {
  const [window_, setWindow] = useStatsWindow()
  const [stats, setStats] = useState<TrafficStatistics | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const load = useCallback(async (win: "1h" | "24h", showLoading: boolean) => {
    if (showLoading) setIsLoading(true)
    try {
      const data = await statisticsService.getTrafficStatistics(win)
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
    const id = setInterval(() => loadRef.current(window_, false), REFRESH_INTERVAL)
    return () => clearInterval(id)
  }, [window_])

  const hasData =
    stats !== null &&
    (stats.topSources.length > 0 ||
      stats.topDestinations.length > 0 ||
      stats.topConversations.length > 0 ||
      stats.deniedSources.length > 0 ||
      stats.deniedPorts.length > 0 ||
      stats.topDomains.length > 0)

  return (
    <div className="space-y-4">
      {/* Page header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <BarChart3 className="size-5" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight">Overview</h1>
            <p className="text-xs text-muted-foreground">
              กราฟ Bandwidth, Top 5 Hosts, Top Source Hosts, Top Destinations, Top Conversations และ Top Denied ตามช่วงเวลา
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <StatsWindowSelect value={window_} onChange={setWindow} />
          {stats && <AccuracyBadge accuracy={stats.accuracy} />}
          <Button variant="outline" size="sm" onClick={() => load(window_, true)} disabled={isLoading}>
            <RefreshCw className={isLoading ? "size-4 animate-spin" : "size-4"} />
            Refresh
          </Button>
        </div>
      </div>

      {stats?.truncated && (
        <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
          <TriangleAlert className="size-4 shrink-0" />
          ข้อมูลบางส่วนอาจไม่ครบ เนื่องจากจำนวน host/conversation ในช่วงเวลานี้เกินขีดจำกัดการติดตาม
        </div>
      )}

      {error && !stats && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-destructive">{error}</CardContent>
        </Card>
      )}

      {isLoading && !stats && (
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
      )}

      {/* Bandwidth trend + Top 5 Hosts (docs/ref/todo/
          statistics-overview-bandwidth-chart-plan.md T-09) — rendered
          OUTSIDE the ternary below on purpose: the "stats && !hasData" empty
          card would otherwise hide this row too, right when a user watching
          a freshly-booted device most wants to see the graph collecting
          data. hasData itself is untouched (still drives the older cards
          only). */}
      {stats && (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
          <BandwidthTrendCard className="lg:col-span-2" series={stats.series} window={window_} />
          <TopHostsShareCard
            className="lg:col-span-1"
            hosts={stats.topSources.slice(0, 5)}
            observedBytes={stats.observedBytes}
          />
        </div>
      )}

      {stats && !hasData ? (
        <Card>
          <CardContent>
            <EmptyState label="ยังไม่มีข้อมูล traffic ในช่วงเวลานี้ (ระบบเพิ่งเริ่มทำงาน หรือ conntrack ยังไม่พร้อมใช้งาน)" />
          </CardContent>
        </Card>
      ) : stats ? (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <TopHostsCard
            title="Top Source Hosts"
            hosts={stats.topSources}
            emptyLabel="ยังไม่มีข้อมูล source host ในช่วงเวลานี้"
          />
          <TopHostsCard
            title="Top Destinations"
            hosts={stats.topDestinations}
            emptyLabel="ยังไม่มีข้อมูล destination ในช่วงเวลานี้"
          />
          <TopConversationsCard conversations={stats.topConversations} />
          <TopDeniedCard
            sources={stats.deniedSources}
            ports={stats.deniedPorts}
            events={stats.deniedEvents}
          />
          <TopDomainsCard
            domains={stats.topDomains}
            dnsLoggingEnabled={stats.dnsLoggingEnabled}
            dnsTruncated={stats.dnsTruncated}
            window_={window_}
          />
        </div>
      ) : null}

      <p className="text-center text-[11px] text-muted-foreground">
        Auto-refresh ทุก 10 วินาที · Observed: {stats ? fmtBytes(stats.observedBytes) : "-"}
      </p>
    </div>
  )
}
