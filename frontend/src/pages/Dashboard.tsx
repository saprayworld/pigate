import { useEffect, useMemo, useRef, useState } from "react"
import {
  Cpu,
  MemoryStick,
  Thermometer,
  HardDrive,
  RefreshCw,
  Network,
  Wifi,
  Cable,
  ArrowUp,
  Info,
  TriangleAlert,
  ShieldAlert,
  Server,
  Package,
  Disc,
  Clock,
  Timer,
  Cpu as CpuIcon,
} from "lucide-react"
import {
  ResponsiveContainer,
  LineChart,
  Line,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import { useTheme } from "@/hooks/useTheme"
import { cn } from "@/lib/utils"
import { ifaceLabel } from "@/lib/ifaceLabel"
import {
  dashboardService,
  type PerformanceMetrics,
  type SystemInfo,
  type TrafficHistory,
} from "@/services/dashboardService"
import { interfaceService } from "@/services/interfaceService"
import type { NetworkInterface, FirewallLog } from "@/data-mockup/mockData"

/* -------------------------------------------------------------------------- */
/*  Polling intervals (see design doc §11)                                    */
/* -------------------------------------------------------------------------- */

const METRICS_INTERVAL = 5_000
const INFO_INTERVAL = 30_000
const TRAFFIC_INTERVAL = 60_000
const INTERFACES_INTERVAL = 30_000
const LOGS_INTERVAL = 10_000

/** usePoll fetches immediately, then on `intervalMs`, and again when
 *  `refreshKey` changes. Errors are swallowed so a transient failure doesn't
 *  blank the dashboard — the previous value is kept until the next success. */
function usePoll<T>(fn: () => Promise<T>, intervalMs: number, refreshKey: number): T | null {
  const [data, setData] = useState<T | null>(null)
  const fnRef = useRef(fn)
  // Keep the ref pointed at the latest fn without touching it during render.
  useEffect(() => {
    fnRef.current = fn
  })
  useEffect(() => {
    let active = true
    const run = () => {
      fnRef
        .current()
        .then((d) => {
          if (active) setData(d)
        })
        .catch(() => {
          /* keep last known value */
        })
    }
    run()
    const id = setInterval(run, intervalMs)
    return () => {
      active = false
      clearInterval(id)
    }
  }, [intervalMs, refreshKey])
  return data
}

/* -------------------------------------------------------------------------- */
/*  Formatting helpers                                                        */
/* -------------------------------------------------------------------------- */

function fmtBytes(n: number): string {
  if (!n || n <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / 1024 ** i
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

function formatUptime(totalSeconds: number): string {
  const total = Math.max(0, Math.floor(totalSeconds))
  const d = Math.floor(total / 86400)
  const h = Math.floor((total % 86400) / 3600)
  const m = Math.floor((total % 3600) / 60)
  const s = total % 60
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${d}d ${pad(h)}:${pad(m)}:${pad(s)}`
}

function formatClock(d: Date): string {
  const pad = (n: number) => String(n).padStart(2, "0")
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  )
}

/* -------------------------------------------------------------------------- */
/*  Metric model (built from live PerformanceMetrics)                         */
/* -------------------------------------------------------------------------- */

type Status = "normal" | "warning" | "critical"

interface Metric {
  key: string
  title: string
  subtitle: string
  icon: typeof Cpu
  value: string
  caption: string
  percent: number
  status: Status
}

function buildMetrics(m: PerformanceMetrics | null): Metric[] {
  if (!m) return []

  const cpuStatus: Status = m.cpu < 70 ? "normal" : m.cpu < 90 ? "warning" : "critical"
  const memStatus: Status =
    m.memDetail.percent < 60 ? "normal" : m.memDetail.percent < 85 ? "warning" : "critical"
  const tempAvail = m.tempDetail.available
  const throttle = m.tempDetail.throttleCelsius || 80
  const tempPct = tempAvail ? Math.round((m.tempDetail.celsius / throttle) * 100) : 0
  const tempStatus: Status = !tempAvail
    ? "normal"
    : m.tempDetail.celsius < 70
      ? "normal"
      : m.tempDetail.celsius < 80
        ? "warning"
        : "critical"
  const stStatus: Status =
    m.storage.percent < 75 ? "normal" : m.storage.percent < 90 ? "warning" : "critical"

  const freqStr = m.cpuDetail.freqAvailable ? `${(m.cpuDetail.freqMhz / 1000).toFixed(1)} GHz` : null
  const cpuCaption =
    [freqStr, tempAvail ? `${m.tempDetail.celsius}°C` : null].filter(Boolean).join(" · ") || "—"

  return [
    {
      key: "cpu",
      title: "CPU",
      subtitle: `${m.cpuDetail.cores}× ${m.cpuDetail.modelName}`,
      icon: Cpu,
      value: `${m.cpu}%`,
      caption: cpuCaption,
      percent: m.cpu,
      status: cpuStatus,
    },
    {
      key: "memory",
      title: "Memory",
      subtitle: "RAM",
      icon: MemoryStick,
      value: `${fmtBytes(m.memDetail.usedBytes)} / ${fmtBytes(m.memDetail.totalBytes)}`,
      caption: `${m.memDetail.percent}% used`,
      percent: m.memDetail.percent,
      status: memStatus,
    },
    {
      key: "temp",
      title: "Temperature",
      subtitle: "SoC package",
      icon: Thermometer,
      value: tempAvail ? `${m.tempDetail.celsius}°C` : "N/A",
      caption: tempAvail ? `Throttle at ${throttle}°C` : "No sensor",
      percent: tempPct,
      status: tempStatus,
    },
    {
      key: "storage",
      title: "Storage",
      subtitle: fmtBytes(m.storage.totalBytes),
      icon: HardDrive,
      value: `${fmtBytes(m.storage.usedBytes)} / ${fmtBytes(m.storage.totalBytes)}`,
      caption: `${m.storage.percent}% used`,
      percent: m.storage.percent,
      status: stStatus,
    },
  ]
}

interface Bucket {
  time: string
  download: number
  upload: number
}

/** Aggregate the 5-minute RAM buckets into hourly points (GB) for the 24h chart. */
function aggregateHourly(hist: TrafficHistory | null): Bucket[] {
  if (!hist || hist.buckets.length === 0) return []
  const map = new Map<string, { rx: number; tx: number; label: string; sort: number }>()
  for (const b of hist.buckets) {
    const d = new Date(b.ts)
    const key = `${d.getFullYear()}-${d.getMonth()}-${d.getDate()}-${d.getHours()}`
    const label = String(d.getHours()).padStart(2, "0")
    const cur = map.get(key) ?? { rx: 0, tx: 0, label, sort: d.getTime() }
    cur.rx += b.rxBytes
    cur.tx += b.txBytes
    map.set(key, cur)
  }
  return [...map.values()]
    .sort((a, b) => a.sort - b.sort)
    .map((v) => ({
      time: v.label,
      download: v.rx / 1024 ** 3,
      upload: v.tx / 1024 ** 3,
    }))
}

/* -------------------------------------------------------------------------- */
/*  Status color helpers (theme-driven, no hardcoded brand colors)            */
/* -------------------------------------------------------------------------- */

const statusMeter: Record<Status, string> = {
  normal: "bg-primary",
  warning: "bg-warning",
  critical: "bg-destructive",
}

const statusBadge: Record<Status, string> = {
  normal: "bg-primary/10 text-primary border-primary/20",
  warning: "bg-warning/10 text-warning border-warning/20",
  critical: "bg-destructive/10 text-destructive border-destructive/20",
}

const statusLabel: Record<Status, string> = {
  normal: "Normal",
  warning: "Warning",
  critical: "Critical",
}

type AlertLevel = "WARN" | "INFO" | "ERR"

interface AlertItem {
  level: AlertLevel
  message: string
  time: string
}

const alertStyle: Record<AlertLevel, { badge: string; icon: typeof Info }> = {
  WARN: { badge: "bg-warning/10 text-warning border-warning/20", icon: TriangleAlert },
  INFO: { badge: "bg-primary/10 text-primary border-primary/20", icon: Info },
  ERR: { badge: "bg-destructive/10 text-destructive border-destructive/20", icon: ShieldAlert },
}

// Forward-traffic log entries now carry an RFC3339 UTC timestamp (from the
// kernel NFLOG watcher). Format it to a local HH:MM:SS for display; fall back to
// the raw string if it isn't a parseable date (older/mock "HH:MM:SS" values).
function formatLogTime(time: string): string {
  const d = new Date(time)
  if (isNaN(d.getTime())) return time
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

function logsToAlerts(logs: FirewallLog[]): AlertItem[] {
  return logs.slice(0, 8).map((l) => ({
    level: l.action === "DROP" ? "WARN" : "INFO",
    message: `${l.reason} — ${l.src} → ${l.dest}:${l.port}/${l.proto}`,
    time: formatLogTime(l.time),
  }))
}

/* -------------------------------------------------------------------------- */
/*  Sub-components                                                             */
/* -------------------------------------------------------------------------- */

function StatCard({ metric, compact = false }: { metric: Metric; compact?: boolean }) {
  const Icon = metric.icon
  return (
    <Card size="sm" className="gap-0 ring-foreground/10">
      <CardHeader className="flex flex-row items-center justify-between gap-2 space-y-0">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          <Icon className="h-4 w-4 shrink-0" />
          <span className="text-foreground">{metric.title}</span>
          {!compact && (
            <span className="truncate text-xs font-normal text-muted-foreground">
              ({metric.subtitle})
            </span>
          )}
        </CardTitle>
        <Badge variant="outline" className={cn("border", statusBadge[metric.status])}>
          {statusLabel[metric.status]}
        </Badge>
      </CardHeader>
      <CardContent className="space-y-3 pt-3">
        <div>
          <p className={cn("font-bold tracking-tight text-foreground", compact ? "text-2xl" : "text-3xl")}>
            {metric.value}
          </p>
          <p className="mt-1 text-xs text-muted-foreground">{metric.caption}</p>
        </div>
        <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
          <div
            className={cn("h-full rounded-full transition-all duration-500", statusMeter[metric.status])}
            style={{ width: `${Math.min(100, metric.percent)}%` }}
          />
        </div>
      </CardContent>
    </Card>
  )
}

function StatGrid({
  metrics,
  compact = false,
  gap = "gap-4",
}: {
  metrics: Metric[]
  compact?: boolean
  gap?: string
}) {
  if (metrics.length === 0) {
    return (
      <div className="flex h-24 items-center justify-center rounded-lg border border-dashed border-border text-sm text-muted-foreground">
        Loading system status…
      </div>
    )
  }
  return (
    <div className={cn("grid sm:grid-cols-2 lg:grid-cols-4", gap)}>
      {metrics.map((m) => (
        <StatCard key={m.key} metric={m} compact={compact} />
      ))}
    </div>
  )
}

function BandwidthCard({ data }: { data: Bucket[] }) {
  const { theme } = useTheme()
  const isDark = theme === "dark"
  const grid = isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.06)"
  const axis = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.45)"
  const downloadColor = "var(--primary)"
  const uploadColor = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.38)"

  return (
    <Card className="lg:col-span-2">
      <CardHeader className="flex flex-row items-center justify-between space-y-0">
        <CardTitle className="text-base font-semibold">Bandwidth · last 24h</CardTitle>
        <div className="flex items-center gap-4 text-xs text-muted-foreground">
          <span className="flex items-center gap-1.5">
            <span className="h-2.5 w-2.5 rounded-sm bg-primary" />
            Download
          </span>
          <span className="flex items-center gap-1.5">
            <span className="h-2.5 w-2.5 rounded-sm bg-muted-foreground/40" />
            Upload
          </span>
        </div>
      </CardHeader>
      <CardContent>
        <div className="h-56 w-full">
          {data.length === 0 ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              Collecting traffic data…
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={data} margin={{ top: 8, right: 8, left: -24, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={grid} />
                <XAxis dataKey="time" stroke={axis} fontSize={11} tickLine={false} axisLine={false} />
                <YAxis
                  stroke={axis}
                  fontSize={11}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(v) => `${Number(v).toFixed(2)}G`}
                />
                <Tooltip
                  cursor={{ stroke: isDark ? "rgba(255,255,255,0.15)" : "rgba(0,0,0,0.12)", strokeWidth: 1 }}
                  contentStyle={{
                    backgroundColor: isDark ? "oklch(0.205 0 0)" : "#fff",
                    border: `1px solid ${grid}`,
                    borderRadius: "8px",
                    fontSize: "12px",
                    color: isDark ? "#fff" : "#111",
                  }}
                  formatter={(value, name) => [`${Number(value).toFixed(2)} GB`, name]}
                />
                <Line
                  type="monotone"
                  dataKey="download"
                  name="Download"
                  stroke={downloadColor}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 4, strokeWidth: 0 }}
                  isAnimationActive={false}
                />
                <Line
                  type="monotone"
                  dataKey="upload"
                  name="Upload"
                  stroke={uploadColor}
                  strokeWidth={2}
                  dot={false}
                  activeDot={{ r: 4, strokeWidth: 0 }}
                  isAnimationActive={false}
                />
              </LineChart>
            </ResponsiveContainer>
          )}
        </div>
      </CardContent>
    </Card>
  )
}

function SystemInfoCard({
  info,
  fetchedAt,
  now,
}: {
  info: SystemInfo | null
  fetchedAt: number
  now: number
}) {
  const elapsedSec = Math.floor((now - fetchedAt) / 1000)

  const rows: { label: string; value: string; icon: typeof Server; mono: boolean }[] = []
  if (info) {
    const clockBase = Date.parse(info.systemTime)
    const liveClock = Number.isNaN(clockBase) ? new Date(now) : new Date(clockBase + (now - fetchedAt))
    rows.push({ label: "Hostname", value: info.hostname || "—", icon: Server, mono: true })
    rows.push({ label: "Software", value: `PiGate ${info.version}`, icon: Package, mono: false })
    rows.push({ label: "OS Base", value: info.osName || "—", icon: Disc, mono: false })
    if (info.boardModel) rows.push({ label: "Board", value: info.boardModel, icon: CpuIcon, mono: false })
    if (info.kernelVersion)
      rows.push({ label: "Kernel", value: info.kernelVersion, icon: Package, mono: true })
    rows.push({
      label: "System Uptime",
      value: formatUptime(info.uptimeSeconds + elapsedSec),
      icon: Timer,
      mono: true,
    })
    rows.push({
      label: "System Time",
      value: `${formatClock(liveClock)} (${info.timezone})`,
      icon: Clock,
      mono: true,
    })
  }

  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="flex items-center gap-2 text-base font-semibold">
          <Server className="h-4 w-4 text-muted-foreground" />
          System Information
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1">
        {rows.length === 0 ? (
          <p className="py-4 text-sm text-muted-foreground">Loading…</p>
        ) : (
          rows.map((r) => {
            const Icon = r.icon
            return (
              <div
                key={r.label}
                className="flex items-center justify-between border-b border-border/50 py-1.5 last:border-0"
              >
                <span className="flex items-center gap-2 text-sm text-muted-foreground">
                  <Icon className="h-4 w-4 shrink-0" />
                  {r.label}
                </span>
                <span
                  className={cn(
                    "truncate pl-2 text-right text-sm font-medium text-foreground",
                    r.mono && "font-mono text-[13px]"
                  )}
                >
                  {r.value}
                </span>
              </div>
            )
          })
        )}
      </CardContent>
    </Card>
  )
}

function InterfacesCard({ interfaces }: { interfaces: NetworkInterface[] }) {
  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="flex items-center gap-2 text-base font-semibold">
          <Network className="h-4 w-4 text-muted-foreground" />
          Interfaces
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1">
        {interfaces.length === 0 ? (
          <p className="py-4 text-sm text-muted-foreground">Loading…</p>
        ) : (
          interfaces.map((iface) => {
            const isUp = iface.status === "up"
            const Icon = iface.type === "wireless" ? Wifi : Cable
            const detailParts: string[] = [iface.role]
            if (iface.type === "wireless" && iface.wifiSSID) detailParts.push(iface.wifiSSID)
            else if (iface.speed) detailParts.push(iface.speed)
            return (
              <div
                key={iface.id}
                className="flex items-center justify-between border-b border-border/50 py-2.5 last:border-0"
              >
                <div className="flex items-center gap-3">
                  <span
                    className={cn("h-2 w-2 rounded-full", isUp ? "bg-primary" : "bg-muted-foreground/40")}
                  />
                  <Icon
                    className={cn("h-4 w-4", isUp ? "text-foreground" : "text-muted-foreground/60")}
                  />
                  <div className="flex items-baseline gap-2">
                    <span className="font-mono text-sm font-medium text-foreground">{ifaceLabel(iface)}</span>
                    <span className="text-xs text-muted-foreground">{detailParts.join(" · ")}</span>
                  </div>
                </div>
                <span
                  className={cn(
                    "flex items-center gap-1 text-xs font-medium",
                    isUp ? "text-primary" : "text-muted-foreground"
                  )}
                >
                  {isUp ? <ArrowUp className="h-3.5 w-3.5" /> : null}
                  {iface.status}
                </span>
              </div>
            )
          })
        )}
      </CardContent>
    </Card>
  )
}

function AlertsCard({ alerts }: { alerts: AlertItem[] }) {
  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="text-base font-semibold">Recent Alerts</CardTitle>
      </CardHeader>
      <CardContent className="space-y-1">
        {alerts.length === 0 ? (
          <p className="py-4 text-sm text-muted-foreground">No recent events.</p>
        ) : (
          alerts.map((a, i) => {
            const s = alertStyle[a.level]
            return (
              <div
                key={i}
                className="flex items-start justify-between gap-3 border-b border-border/50 py-2.5 last:border-0"
              >
                <div className="flex items-start gap-3">
                  <Badge
                    variant="outline"
                    className={cn("mt-0.5 shrink-0 border font-semibold", s.badge)}
                  >
                    {a.level}
                  </Badge>
                  <span className="text-sm leading-snug text-foreground/90">{a.message}</span>
                </div>
                <span className="shrink-0 pt-0.5 font-mono text-xs text-muted-foreground">{a.time}</span>
              </div>
            )
          })
        )}
      </CardContent>
    </Card>
  )
}

/* -------------------------------------------------------------------------- */
/*  Page                                                                      */
/* -------------------------------------------------------------------------- */

export default function Dashboard() {
  const [refreshKey, setRefreshKey] = useState(0)
  const [now, setNow] = useState(() => Date.now())

  // 1-second tick drives the live uptime/clock in the System Information card.
  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  const perf = usePoll(dashboardService.getPerformanceMetrics, METRICS_INTERVAL, refreshKey)
  const traffic = usePoll(dashboardService.getTrafficHistory, TRAFFIC_INTERVAL, refreshKey)
  const interfaces = usePoll(interfaceService.getAll, INTERFACES_INTERVAL, refreshKey)
  const logs = usePoll(dashboardService.getRecentLogs, LOGS_INTERVAL, refreshKey)

  // System info is polled together with the client-side fetch timestamp so the
  // uptime/clock can advance locally between polls (no extra state/effect).
  const infoResult = usePoll(
    async () => ({ info: await dashboardService.getSystemInfo(), fetchedAt: Date.now() }),
    INFO_INTERVAL,
    refreshKey
  )
  const info = infoResult?.info ?? null
  const infoFetchedAt = infoResult?.fetchedAt ?? now

  const metrics = useMemo(() => buildMetrics(perf), [perf])
  const chartData = useMemo(() => aggregateHourly(traffic), [traffic])
  const alerts = useMemo(() => logsToAlerts(logs ?? []), [logs])
  const ifaces = interfaces ?? []

  return (
    <Tabs defaultValue="overview" className="space-y-6">
      {/* Header: view switcher + refresh */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="compact">Compact</TabsTrigger>
          <TabsTrigger value="detailed">Detailed</TabsTrigger>
        </TabsList>
        <Button
          variant="outline"
          size="sm"
          onClick={() => setRefreshKey((s) => s + 1)}
          className="gap-2"
        >
          <RefreshCw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      {/* Overview */}
      <TabsContent value="overview" className="space-y-6">
        <StatGrid metrics={metrics} />
        <div className="grid gap-4 lg:grid-cols-3">
          <BandwidthCard data={chartData} />
          <SystemInfoCard info={info} fetchedAt={infoFetchedAt} now={now} />
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          <InterfacesCard interfaces={ifaces} />
          <AlertsCard alerts={alerts} />
        </div>
      </TabsContent>

      {/* Compact — denser stat grid + status side by side */}
      <TabsContent value="compact" className="space-y-4">
        <StatGrid metrics={metrics} compact gap="gap-3" />
        <div className="grid gap-4 lg:grid-cols-2">
          <SystemInfoCard info={info} fetchedAt={infoFetchedAt} now={now} />
          <InterfacesCard interfaces={ifaces} />
        </div>
      </TabsContent>

      {/* Detailed — everything stacked with full context */}
      <TabsContent value="detailed" className="space-y-6">
        <StatGrid metrics={metrics} />
        <BandwidthCard data={chartData} />
        <div className="grid gap-4 lg:grid-cols-3">
          <div className="lg:col-span-1">
            <SystemInfoCard info={info} fetchedAt={infoFetchedAt} now={now} />
          </div>
          <div className="lg:col-span-1">
            <InterfacesCard interfaces={ifaces} />
          </div>
          <div className="lg:col-span-1">
            <AlertsCard alerts={alerts} />
          </div>
        </div>
      </TabsContent>
    </Tabs>
  )
}
