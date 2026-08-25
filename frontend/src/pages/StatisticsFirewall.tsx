import { useCallback, useEffect, useRef, useState } from "react"
import { useNavigate } from "react-router"
import { ArrowRight, Info, RefreshCw, ShieldAlert } from "lucide-react"
import {
  ResponsiveContainer,
  LineChart,
  Line,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip as RechartsTooltip,
  CartesianGrid,
} from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Badge } from "@/components/ui/badge"
import { Skeleton } from "@/components/ui/skeleton"
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { getErrorMessage } from "@/lib/errors"
import { fmtBytes } from "@/lib/formatBytes"
import { useTheme } from "@/hooks/useTheme"
import { statsWindowLongLabel } from "@/lib/statsWindow"
import { useStatsWindow, StatsWindowTabs } from "@/components/statistics/DnsStatsShared"
import { TrafficEmptyState, TruncatedWarning } from "@/components/statistics/TrafficStatsShared"
import { CapacityIndicator } from "@/components/statistics/CapacityIndicator"
import { capacityService, type RingCapacity } from "@/services/capacityService"
import { ReferenceHoverProvider } from "@/components/reference/ReferenceHoverProvider"
import { HostReferenceTrigger } from "@/components/reference/HostReferenceTrigger"
import {
  firewallStatisticsService,
  type FirewallStatistics,
  type FirewallRuleStatRow,
} from "@/services/firewallStatisticsService"

// /statistics/firewall — Statistics -> Firewall page (docs/ref/todo/
// statistics-firewall-page-plan.md). Combines two independently-sourced,
// differently-scoped byte/event accountings — never mixed into one
// percentage or chart axis (see the page's own tooltips below for the
// user-facing explanation, and the service/model doc comments for the full
// rationale):
//  - acceptedBytes/blockedBytes/trend/chains/rules: nft per-rule counter
//    (exact, but ONLY traffic that matched a user-defined policy rule — the
//    built-in default-drop is NOT included).
//  - blockedEvents/denyTrend/blockedSources/blockedPorts/recentBlockedEvents:
//    NFLOG event count (DOES include default-drop traffic).

const REFRESH_INTERVAL = 10_000

const CHAIN_TO_POLICY_PATH: Record<string, string> = {
  forward: "/policy/firewall",
  input: "/policy/local-in",
  output: "/policy/local-out",
}

function chainLabel(chain?: string): string {
  switch (chain) {
    case "forward":
      return "Firewall (forward)"
    case "input":
      return "Local-In (input)"
    case "output":
      return "Local-Out (output)"
    default:
      return "-"
  }
}

// BlockedBytesInfo — the tooltip the project owner required on every card
// that shows blockedBytes/blockedPackets, explaining the nft-rule-counter
// scope limitation (never rolled into a percentage against blockedEvents).
function BlockedBytesInfo() {
  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button type="button" className="inline-flex cursor-pointer text-muted-foreground hover:text-foreground" aria-label="คำอธิบาย Blocked bytes/packets">
          <Info className="size-3.5" />
        </button>
      </TooltipTrigger>
      <TooltipContent className="max-w-64 text-xs">
        นับเฉพาะ traffic ที่ match กฎ (rule) ที่ผู้ใช้กำหนดไว้เท่านั้น ไม่รวม traffic ที่ถูกบล็อกโดย
        default-drop ของระบบ — ดูตัวเลขที่ครอบคลุมกว่านี้ที่การ์ด "Blocked events"
      </TooltipContent>
    </Tooltip>
  )
}

function SummaryCard({
  label,
  value,
  sub,
  info,
}: {
  label: string
  value: string
  sub?: string
  info?: React.ReactNode
}) {
  return (
    <Card size="sm">
      <CardContent className="space-y-1.5">
        <div className="flex items-center gap-1.5">
          <p className="text-xs text-muted-foreground">{label}</p>
          {info}
        </div>
        <p className="text-xl font-bold tracking-tight text-foreground">{value}</p>
        {sub && <p className="text-[11px] text-muted-foreground">{sub}</p>}
      </CardContent>
    </Card>
  )
}

function TrendCard({ data }: { data: FirewallStatistics }) {
  const { theme } = useTheme()
  const isDark = theme === "dark"
  const grid = isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.06)"
  const axis = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.45)"
  const acceptColor = "var(--primary)"
  const dropColor = "var(--destructive)"

  const chartData = data.trend.map((p) => {
    const d = new Date(p.ts)
    const label = Number.isNaN(d.getTime())
      ? p.ts
      : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })
    return { time: label, accepted: p.acceptedBytes, blocked: p.blockedBytes }
  })
  const hasSignal = chartData.some((p) => p.accepted > 0 || p.blocked > 0)
  const tickInterval = Math.max(0, Math.ceil(chartData.length / 10) - 1)

  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-x-4 gap-y-1 space-y-0">
        <div>
          <CardTitle className="text-base font-semibold">Firewall Traffic Trend (bytes)</CardTitle>
          <p className="text-[11px] text-muted-foreground">
            Accept vs Drop ต่อ bucket 5 นาที · จาก nft rule counter (เฉพาะ traffic ที่ match user rule)
          </p>
        </div>
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="h-2.5 w-2.5 rounded-sm bg-primary" />
            Accept
          </span>
          <span className="flex items-center gap-1.5">
            <span className="h-2.5 w-2.5 rounded-sm bg-destructive" />
            Drop
          </span>
        </div>
      </CardHeader>
      <CardContent>
        <div className="h-56 w-full">
          {!hasSignal ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              กำลังเก็บข้อมูล traffic…
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={chartData} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={grid} />
                <XAxis dataKey="time" stroke={axis} fontSize={11} tickLine={false} axisLine={false} interval={tickInterval} />
                <YAxis stroke={axis} fontSize={11} tickLine={false} axisLine={false} tickFormatter={(v) => fmtBytes(Number(v))} width={64} />
                <RechartsTooltip
                  cursor={{ stroke: isDark ? "rgba(255,255,255,0.15)" : "rgba(0,0,0,0.12)", strokeWidth: 1 }}
                  contentStyle={{
                    backgroundColor: isDark ? "oklch(0.205 0 0)" : "#fff",
                    border: `1px solid ${grid}`,
                    borderRadius: "8px",
                    fontSize: "12px",
                    color: isDark ? "#fff" : "#111",
                  }}
                  formatter={(value, name) => [`${fmtBytes(Number(value))} / 5 นาที`, name]}
                />
                <Line type="monotone" dataKey="accepted" name="Accept" stroke={acceptColor} strokeWidth={2} dot={false} activeDot={{ r: 4, strokeWidth: 0 }} isAnimationActive={false} />
                <Line type="monotone" dataKey="blocked" name="Drop" stroke={dropColor} strokeWidth={2} dot={false} activeDot={{ r: 4, strokeWidth: 0 }} isAnimationActive={false} />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function DenyTrendCard({ data }: { data: FirewallStatistics }) {
  const { theme } = useTheme()
  const isDark = theme === "dark"
  const grid = isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.06)"
  const axis = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.45)"

  const chartData = data.denyTrend.map((p) => {
    const d = new Date(p.ts)
    const label = Number.isNaN(d.getTime())
      ? p.ts
      : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })
    return { time: label, events: p.events }
  })
  const hasSignal = chartData.some((p) => p.events > 0)
  const tickInterval = Math.max(0, Math.ceil(chartData.length / 10) - 1)

  return (
    <Card>
      <CardHeader className="space-y-1">
        <div className="flex items-center gap-1.5">
          <CardTitle className="text-base font-semibold">Blocked Events Trend (events)</CardTitle>
          <Tooltip>
            <TooltipTrigger asChild>
              <button type="button" className="inline-flex cursor-pointer text-muted-foreground hover:text-foreground" aria-label="คำอธิบาย Blocked events">
                <Info className="size-3.5" />
              </button>
            </TooltipTrigger>
            <TooltipContent className="max-w-64 text-xs">
              จำนวนครั้ง (event) ที่ NFLOG เห็น DROP รวมทั้ง traffic ที่โดน default-drop ด้วย — คนละหน่วยกับ
              กราฟ "Firewall Traffic Trend" (bytes) ด้านบน ห้ามเทียบแกนหรือรวม % กัน
            </TooltipContent>
          </Tooltip>
        </div>
        <p className="text-[11px] text-muted-foreground">จำนวน event ต่อ bucket 5 นาที · จาก NFLOG</p>
      </CardHeader>
      <CardContent>
        <div className="h-56 w-full">
          {!hasSignal ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              ยังไม่มี event ถูกบล็อกในช่วงเวลานี้
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={chartData} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={grid} />
                <XAxis dataKey="time" stroke={axis} fontSize={11} tickLine={false} axisLine={false} interval={tickInterval} />
                <YAxis stroke={axis} fontSize={11} tickLine={false} axisLine={false} width={40} allowDecimals={false} />
                <RechartsTooltip
                  cursor={{ fill: isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.04)" }}
                  contentStyle={{
                    backgroundColor: isDark ? "oklch(0.205 0 0)" : "#fff",
                    border: `1px solid ${grid}`,
                    borderRadius: "8px",
                    fontSize: "12px",
                    color: isDark ? "#fff" : "#111",
                  }}
                  formatter={(value) => [`${value} event`, "Blocked"]}
                />
                <Bar dataKey="events" name="Blocked" fill="var(--destructive)" isAnimationActive={false} radius={[3, 3, 0, 0]} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function ChainBreakdownCard({ data }: { data: FirewallStatistics }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-semibold">Chain Breakdown</CardTitle>
        <p className="text-[11px] text-muted-foreground">Accept/Drop bytes แยกตาม chain (forward / input / output)</p>
      </CardHeader>
      <CardContent>
        {data.chains.length === 0 ? (
          <TrafficEmptyState label="ยังไม่มีข้อมูลในช่วงเวลานี้" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs">Chain</TableHead>
                  <TableHead className="text-right text-xs">Accepted</TableHead>
                  <TableHead className="text-right text-xs">Blocked</TableHead>
                  <TableHead className="text-right text-xs">%</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.chains.map((c) => (
                  <TableRow key={c.chain}>
                    <TableCell className="py-2.5 text-xs font-medium">{chainLabel(c.chain)}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs text-primary">{fmtBytes(c.acceptedBytes)}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs text-destructive">{fmtBytes(c.blockedBytes)}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs text-muted-foreground">{c.percent}%</TableCell>
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

function TopRulesCard({ data }: { data: FirewallStatistics }) {
  const navigate = useNavigate()

  const goToPolicy = (row: FirewallRuleStatRow) => {
    const base = (row.chain && CHAIN_TO_POLICY_PATH[row.chain]) || "/policy/firewall"
    navigate(`${base}?q=${encodeURIComponent(row.name)}`)
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-semibold">Top Rules by Traffic</CardTitle>
        <p className="text-[11px] text-muted-foreground">คลิกแถวเพื่อไปหน้านโยบายของ chain นั้น</p>
      </CardHeader>
      <CardContent>
        {data.rules.length === 0 ? (
          <TrafficEmptyState label="ยังไม่มีกฎที่มี traffic ในช่วงเวลานี้" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs">Rule</TableHead>
                  <TableHead className="text-xs">Chain</TableHead>
                  <TableHead className="text-xs">Action</TableHead>
                  <TableHead className="text-right text-xs">Bytes</TableHead>
                  <TableHead className="text-right text-xs">Packets</TableHead>
                  <TableHead className="text-right text-xs">%</TableHead>
                  <TableHead className="text-xs">Last matched</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.rules.map((r) => (
                  <TableRow
                    key={r.ruleId}
                    onClick={() => goToPolicy(r)}
                    role="button"
                    tabIndex={0}
                    onKeyDown={(e) => {
                      if (e.key === "Enter" || e.key === " ") {
                        e.preventDefault()
                        goToPolicy(r)
                      }
                    }}
                    className="cursor-pointer hover:bg-muted/50"
                  >
                    <TableCell className="py-2.5 text-xs">
                      <div className="flex items-center gap-1.5">
                        <span className="truncate">{r.name}</span>
                        {r.unused && (
                          <Badge variant="outline" className="shrink-0 border-muted-foreground/30 text-[10px] text-muted-foreground">
                            Unused
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="py-2.5 text-xs text-muted-foreground">{r.chain ?? "-"}</TableCell>
                    <TableCell className="py-2.5 text-xs">
                      <Badge variant="outline" className={r.action === "DROP" ? "border-destructive/30 text-destructive" : "border-primary/30 text-primary"}>
                        {r.action ?? "-"}
                      </Badge>
                    </TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs">{fmtBytes(r.bytes)}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs text-muted-foreground">{r.packets.toLocaleString()}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs text-muted-foreground">{r.percent}%</TableCell>
                    <TableCell className="py-2.5 text-xs text-muted-foreground">
                      {r.lastMatchedAt ? new Date(r.lastMatchedAt).toLocaleString() : "—"}
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

function BlockedSourcesCard({ data }: { data: FirewallStatistics }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-semibold">Top Blocked Sources</CardTitle>
      </CardHeader>
      <CardContent>
        {data.blockedSources.length === 0 ? (
          <TrafficEmptyState label="ยังไม่มี source ที่ถูกบล็อกในช่วงเวลานี้" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs">Source</TableHead>
                  <TableHead className="text-right text-xs">Events</TableHead>
                  <TableHead className="text-right text-xs">%</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.blockedSources.map((s) => (
                  <TableRow key={s.ip}>
                    <TableCell className="py-2.5 text-xs">
                      <HostReferenceTrigger ip={s.ip}>
                        <span className="truncate">{s.hostname || s.ip}</span>
                      </HostReferenceTrigger>
                    </TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs">{s.count.toLocaleString()}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs text-muted-foreground">{s.percent}%</TableCell>
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

function BlockedPortsCard({ data }: { data: FirewallStatistics }) {
  return (
    <Card>
      <CardHeader>
        <CardTitle className="text-base font-semibold">Top Blocked Ports</CardTitle>
      </CardHeader>
      <CardContent>
        {data.blockedPorts.length === 0 ? (
          <TrafficEmptyState label="ยังไม่มีพอร์ตที่ถูกบล็อกในช่วงเวลานี้" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs">Proto/Port</TableHead>
                  <TableHead className="text-right text-xs">Events</TableHead>
                  <TableHead className="text-right text-xs">%</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.blockedPorts.map((p) => (
                  <TableRow key={`${p.proto}/${p.port}`}>
                    <TableCell className="py-2.5 font-mono text-xs">{p.proto}/{p.port}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs">{p.count.toLocaleString()}</TableCell>
                    <TableCell className="py-2.5 text-right font-mono text-xs text-muted-foreground">{p.percent}%</TableCell>
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

function RecentBlockedEventsCard({ data }: { data: FirewallStatistics }) {
  const navigate = useNavigate()
  return (
    <Card>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-x-4 gap-y-1 space-y-0">
        <div>
          <CardTitle className="text-base font-semibold">Recent Blocked Events</CardTitle>
          <p className="text-[11px] text-muted-foreground">เหตุการณ์บล็อกล่าสุดจาก NFLOG (ring buffer เดียวกับหน้า Log)</p>
        </div>
        <Button variant="outline" size="sm" onClick={() => navigate("/logs/traffic?action=DROP")} className="cursor-pointer">
          ดู log แบบเต็ม
          <ArrowRight className="size-3.5" />
        </Button>
      </CardHeader>
      <CardContent>
        {data.recentBlockedEvents.length === 0 ? (
          <TrafficEmptyState label="ยังไม่มีเหตุการณ์บล็อกในช่วงเวลานี้" />
        ) : (
          <div className="overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs">เวลา</TableHead>
                  <TableHead className="text-xs">Source</TableHead>
                  <TableHead className="text-xs">Proto/Port</TableHead>
                  <TableHead className="text-xs">Chain/Rule</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {data.recentBlockedEvents.map((e, i) => (
                  <TableRow key={`${e.ts}-${i}`}>
                    <TableCell className="py-2.5 text-xs text-muted-foreground">{new Date(e.ts).toLocaleString()}</TableCell>
                    <TableCell className="py-2.5 text-xs">
                      <HostReferenceTrigger ip={e.sourceIp}>
                        <span className="truncate">{e.sourceIp}</span>
                      </HostReferenceTrigger>
                    </TableCell>
                    <TableCell className="py-2.5 font-mono text-xs">{e.proto}/{e.port}</TableCell>
                    <TableCell className="py-2.5 text-xs text-muted-foreground">
                      {chainLabel(e.chain)}
                      {e.ruleName && <span className="ml-1 text-foreground">· {e.ruleName}</span>}
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

export default function StatisticsFirewall() {
  const [window_, setWindow] = useStatsWindow()
  const [data, setData] = useState<FirewallStatistics | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)
  const [capacityRings, setCapacityRings] = useState<RingCapacity[] | undefined>(undefined)

  const load = useCallback(async (win: typeof window_, showLoading: boolean) => {
    if (showLoading) setIsLoading(true)
    try {
      const result = await firewallStatisticsService.getFirewallStatistics(win, 100)
      setData(result)
      setError(null)
    } catch (err) {
      if (showLoading) setError(getErrorMessage(err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }, [])

  const loadCapacity = useCallback(async (win: typeof window_) => {
    try {
      const result = await capacityService.getCapacityStatistics(win, { series: false })
      setCapacityRings(result.rings)
    } catch {
      // Swallowed on purpose — CapacityIndicator simply doesn't render.
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
    }, REFRESH_INTERVAL)
    return () => clearInterval(id)
  }, [window_])

  return (
    <ReferenceHoverProvider>
      <div className="space-y-4">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
              <ShieldAlert className="size-5" />
            </div>
            <div>
              <h1 className="text-lg font-bold tracking-tight">Firewall</h1>
              <p className="text-xs text-muted-foreground">
                สรุปสถิติไฟร์วอลล์ · {statsWindowLongLabel(window_)}
              </p>
            </div>
          </div>
          <div className="flex flex-wrap items-center gap-2">
            <CapacityIndicator rings={capacityRings} group={["firewall"]} window={window_} />
            <StatsWindowTabs value={window_} onChange={setWindow} />
            <Button variant="outline" size="sm" onClick={() => load(window_, true)} disabled={isLoading} title="Refresh" aria-label="Refresh">
              <RefreshCw className={isLoading ? "size-4 animate-spin" : "size-4"} />
              <span className="hidden sm:inline">Refresh</span>
            </Button>
          </div>
        </div>

        {!data?.available && data !== null && (
          <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
            <Info className="size-4 shrink-0" />
            ยังไม่มีข้อมูลตัวนับ nft rule (รอ poll รอบแรกหลัง restart)
          </div>
        )}
        {data?.truncated && <TruncatedWarning />}

        {error && !data && (
          <Card>
            <CardContent className="py-8 text-center text-sm text-destructive">{error}</CardContent>
          </Card>
        )}

        {isLoading && !data && (
          <>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              {Array.from({ length: 4 }).map((_, i) => (
                <Card key={i} size="sm">
                  <CardContent>
                    <Skeleton className="h-12 w-full" />
                  </CardContent>
                </Card>
              ))}
            </div>
            <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
              {[0, 1].map((i) => (
                <Card key={i}>
                  <CardHeader>
                    <Skeleton className="h-5 w-40" />
                  </CardHeader>
                  <CardContent>
                    <Skeleton className="h-56 w-full" />
                  </CardContent>
                </Card>
              ))}
            </div>
          </>
        )}

        {data && (
          <>
            <div className="grid grid-cols-2 gap-3 lg:grid-cols-4">
              <SummaryCard label="Accepted" value={fmtBytes(data.acceptedBytes)} sub={`${data.acceptedPackets.toLocaleString()} packets`} />
              <SummaryCard
                label="Blocked (rule)"
                value={fmtBytes(data.blockedBytes)}
                sub={`${data.blockedPackets.toLocaleString()} packets`}
                info={<BlockedBytesInfo />}
              />
              <SummaryCard label="Blocked events (NFLOG)" value={data.blockedEvents.toLocaleString()} sub="รวม default-drop" />
              <SummaryCard
                label="Rules enabled / unused"
                value={`${data.rulesEnabled} / ${data.rulesUnused}`}
                sub={data.countersSince ? `นับตั้งแต่ ${new Date(data.countersSince).toLocaleString()}` : undefined}
              />
            </div>

            <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
              <TrendCard data={data} />
              <DenyTrendCard data={data} />
            </div>

            <ChainBreakdownCard data={data} />

            <TopRulesCard data={data} />

            <div className="grid grid-cols-1 gap-4 xl:grid-cols-2">
              <BlockedSourcesCard data={data} />
              <BlockedPortsCard data={data} />
            </div>

            <RecentBlockedEventsCard data={data} />
          </>
        )}

        <p className="text-center text-[11px] text-muted-foreground">
          Auto-refresh ทุก 10 วินาที · ข้อมูลทั้งหมดเก็บใน RAM เท่านั้น เริ่มนับใหม่ทุกครั้งที่ restart pigate
          หรือ Apply Settings (เฉพาะตัวนับ nft rule)
        </p>
      </div>
    </ReferenceHoverProvider>
  )
}
