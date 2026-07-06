import { useEffect, useMemo, useState } from "react"
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

/* -------------------------------------------------------------------------- */
/*  Mock data (frontend-only mockup)                                          */
/* -------------------------------------------------------------------------- */

type Status = "normal" | "warning" | "critical"

interface Metric {
  key: string
  title: string
  subtitle: string
  icon: typeof Cpu
  value: string
  caption: string
  /** 0-100 fill of the meter */
  percent: number
  status: Status
}

function buildMetrics(seed: number): Metric[] {
  // seed lightly perturbs values on refresh so the mockup feels live
  const jitter = (base: number, spread: number) =>
    Math.round(base + Math.sin(seed) * spread)

  const cpu = jitter(34, 4)
  const memUsed = 5.1
  const memPct = Math.round((memUsed / 8) * 100)
  const temp = jitter(58, 3)
  const storagePct = 32

  return [
    {
      key: "cpu",
      title: "CPU",
      subtitle: "4× Cortex-A76",
      icon: Cpu,
      value: `${cpu}%`,
      caption: "2.4 GHz · 58°C",
      percent: cpu,
      status: cpu < 70 ? "normal" : cpu < 90 ? "warning" : "critical",
    },
    {
      key: "memory",
      title: "Memory",
      subtitle: "LPDDR4X",
      icon: MemoryStick,
      value: `${memUsed} / 8 GB`,
      caption: `${memPct}% used`,
      percent: memPct,
      status: memPct < 60 ? "normal" : memPct < 85 ? "warning" : "critical",
    },
    {
      key: "temp",
      title: "Temperature",
      subtitle: "SoC package",
      icon: Thermometer,
      value: `${temp}°C`,
      caption: "Throttle at 80°C",
      percent: Math.round((temp / 80) * 100),
      status: temp < 70 ? "normal" : temp < 80 ? "warning" : "critical",
    },
    {
      key: "storage",
      title: "Storage (NVMe)",
      subtitle: "128 GB",
      icon: HardDrive,
      value: "41 / 128 GB",
      caption: `${storagePct}% used`,
      percent: storagePct,
      status: storagePct < 75 ? "normal" : storagePct < 90 ? "warning" : "critical",
    },
  ]
}

interface Bucket {
  time: string
  download: number
  upload: number
}

const bandwidth: Bucket[] = [
  { time: "00", download: 0.4, upload: 0.1 },
  { time: "02", download: 0.6, upload: 0.15 },
  { time: "04", download: 0.5, upload: 0.12 },
  { time: "06", download: 0.5, upload: 0.15 },
  { time: "08", download: 0.9, upload: 0.2 },
  { time: "10", download: 3.4, upload: 0.9 },
  { time: "12", download: 4.8, upload: 1.3 },
  { time: "14", download: 5.1, upload: 1.4 },
  { time: "16", download: 5.6, upload: 1.6 },
  { time: "18", download: 7.2, upload: 2.1 },
  { time: "20", download: 5.4, upload: 1.7 },
  { time: "22", download: 2.6, upload: 0.7 },
]

interface Iface {
  name: string
  role: string
  detail: string
  status: "up" | "disabled"
  icon: typeof Cable
}

const interfaces: Iface[] = [
  { name: "eth0", role: "WAN", detail: "2.5GbE", status: "up", icon: Cable },
  { name: "wlan0", role: "LAN AP", detail: "7 clients", status: "up", icon: Wifi },
  { name: "eth1", role: "LAN", detail: "USB adapter", status: "disabled", icon: Cable },
]

type AlertLevel = "WARN" | "INFO" | "ERR"

interface AlertItem {
  level: AlertLevel
  message: string
  time: string
}

const alerts: AlertItem[] = [
  { level: "WARN", message: "CPU temperature reached 61°C briefly under load", time: "09:41" },
  { level: "INFO", message: "Firewall rule #3 blocked inbound scan from 185.220.101.4", time: "09:42" },
  { level: "ERR", message: 'WireGuard handshake failed for peer "laptop-work"', time: "08:15" },
  { level: "INFO", message: "Firmware update 2.3.2 available for PiGate OS", time: "Yesterday" },
]

/* -------------------------------------------------------------------------- */
/*  Status color helpers (theme-driven, no hardcoded brand colors)            */
/* -------------------------------------------------------------------------- */

const statusMeter: Record<Status, string> = {
  normal: "bg-primary",
  warning: "bg-amber-500",
  critical: "bg-red-500",
}

const statusBadge: Record<Status, string> = {
  normal: "bg-primary/10 text-primary border-primary/20",
  warning: "bg-amber-500/10 text-amber-500 border-amber-500/20",
  critical: "bg-red-500/10 text-red-500 border-red-500/20",
}

const statusLabel: Record<Status, string> = {
  normal: "Normal",
  warning: "Warning",
  critical: "Critical",
}

const alertStyle: Record<AlertLevel, { badge: string; icon: typeof Info }> = {
  WARN: { badge: "bg-amber-500/10 text-amber-500 border-amber-500/20", icon: TriangleAlert },
  INFO: { badge: "bg-primary/10 text-primary border-primary/20", icon: Info },
  ERR: { badge: "bg-red-500/10 text-red-500 border-red-500/20", icon: ShieldAlert },
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

function BandwidthCard() {
  const { theme } = useTheme()
  const isDark = theme === "dark"
  const grid = isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.06)"
  const axis = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.45)"
  // Use the project palette: primary (emerald) for download, muted for upload.
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
          <ResponsiveContainer width="100%" height="100%">
            <LineChart data={bandwidth} margin={{ top: 8, right: 8, left: -24, bottom: 0 }}>
              <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={grid} />
              <XAxis dataKey="time" stroke={axis} fontSize={11} tickLine={false} axisLine={false} />
              <YAxis
                stroke={axis}
                fontSize={11}
                tickLine={false}
                axisLine={false}
                tickFormatter={(v) => `${v}G`}
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
                formatter={(value, name) => [`${value} GB`, name]}
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
        </div>
      </CardContent>
    </Card>
  )
}

/** Boot time fixed once per page load so uptime ticks up realistically. */
const BOOT_TIME = Date.now() - (3 * 24 * 3600 + 4 * 3600 + 12 * 60 + 33) * 1000

function formatUptime(ms: number): string {
  const total = Math.floor(ms / 1000)
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

function SystemInfoCard() {
  const [now, setNow] = useState(() => Date.now())

  useEffect(() => {
    const id = setInterval(() => setNow(Date.now()), 1000)
    return () => clearInterval(id)
  }, [])

  const rows = [
    { label: "Hostname", value: "PiGate-RPI5", icon: Server, mono: true },
    { label: "Software", value: "PiGate OS 2.3.2", icon: Package, mono: false },
    { label: "OS Base", value: "Raspberry Pi OS (64-bit)", icon: Disc, mono: false },
    { label: "System Uptime", value: formatUptime(now - BOOT_TIME), icon: Timer, mono: true },
    { label: "System Time", value: formatClock(new Date(now)), icon: Clock, mono: true },
  ]

  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="flex items-center gap-2 text-base font-semibold">
          <Server className="h-4 w-4 text-muted-foreground" />
          System Information
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1">
        {rows.map((r) => {
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
                  "truncate text-right text-sm font-medium text-foreground",
                  r.mono && "font-mono text-[13px]"
                )}
              >
                {r.value}
              </span>
            </div>
          )
        })}
      </CardContent>
    </Card>
  )
}

function InterfacesCard() {
  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="flex items-center gap-2 text-base font-semibold">
          <Network className="h-4 w-4 text-muted-foreground" />
          Interfaces
        </CardTitle>
      </CardHeader>
      <CardContent className="space-y-1">
        {interfaces.map((iface) => {
          const Icon = iface.icon
          const isUp = iface.status === "up"
          return (
            <div
              key={iface.name}
              className="flex items-center justify-between border-b border-border/50 py-2.5 last:border-0"
            >
              <div className="flex items-center gap-3">
                <span
                  className={cn(
                    "h-2 w-2 rounded-full",
                    isUp ? "bg-primary" : "bg-muted-foreground/40"
                  )}
                />
                <Icon
                  className={cn("h-4 w-4", isUp ? "text-foreground" : "text-muted-foreground/60")}
                />
                <div className="flex items-baseline gap-2">
                  <span className="font-mono text-sm font-medium text-foreground">{iface.name}</span>
                  <span className="text-xs text-muted-foreground">
                    {iface.role} · {iface.detail}
                  </span>
                </div>
              </div>
              <span
                className={cn(
                  "flex items-center gap-1 text-xs font-medium",
                  isUp ? "text-primary" : "text-muted-foreground"
                )}
              >
                {isUp ? <ArrowUp className="h-3.5 w-3.5" /> : null}
                {isUp ? "up" : "disabled"}
              </span>
            </div>
          )
        })}
      </CardContent>
    </Card>
  )
}

function AlertsCard() {
  return (
    <Card>
      <CardHeader className="space-y-0">
        <CardTitle className="text-base font-semibold">Recent Alerts</CardTitle>
      </CardHeader>
      <CardContent className="space-y-1">
        {alerts.map((a, i) => {
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
              <span className="shrink-0 pt-0.5 font-mono text-xs text-muted-foreground">
                {a.time}
              </span>
            </div>
          )
        })}
      </CardContent>
    </Card>
  )
}

/* -------------------------------------------------------------------------- */
/*  Page                                                                      */
/* -------------------------------------------------------------------------- */

export default function Dashboard() {
  const [seed, setSeed] = useState(0)
  const metrics = useMemo(() => buildMetrics(seed), [seed])

  return (
    <Tabs defaultValue="overview" className="space-y-6">
      {/* Header: view switcher + refresh */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <TabsList>
          <TabsTrigger value="overview">Overview</TabsTrigger>
          <TabsTrigger value="compact">Compact</TabsTrigger>
          <TabsTrigger value="detailed">Detailed</TabsTrigger>
        </TabsList>
        <Button variant="outline" size="sm" onClick={() => setSeed((s) => s + 1)} className="gap-2">
          <RefreshCw className="h-4 w-4" />
          Refresh
        </Button>
      </div>

      {/* Overview */}
      <TabsContent value="overview" className="space-y-6">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {metrics.map((m) => (
            <StatCard key={m.key} metric={m} />
          ))}
        </div>
        <div className="grid gap-4 lg:grid-cols-3">
          <BandwidthCard />
          <SystemInfoCard />
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          <InterfacesCard />
          <AlertsCard />
        </div>
      </TabsContent>

      {/* Compact — denser stat grid + status side by side */}
      <TabsContent value="compact" className="space-y-4">
        <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
          {metrics.map((m) => (
            <StatCard key={m.key} metric={m} compact />
          ))}
        </div>
        <div className="grid gap-4 lg:grid-cols-2">
          <SystemInfoCard />
          <InterfacesCard />
        </div>
      </TabsContent>

      {/* Detailed — everything stacked with full context */}
      <TabsContent value="detailed" className="space-y-6">
        <div className="grid gap-4 sm:grid-cols-2 lg:grid-cols-4">
          {metrics.map((m) => (
            <StatCard key={m.key} metric={m} />
          ))}
        </div>
        <BandwidthCard />
        <div className="grid gap-4 lg:grid-cols-3">
          <div className="lg:col-span-1">
            <SystemInfoCard />
          </div>
          <div className="lg:col-span-1">
            <InterfacesCard />
          </div>
          <div className="lg:col-span-1">
            <AlertsCard />
          </div>
        </div>
      </TabsContent>
    </Tabs>
  )
}
