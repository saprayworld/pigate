import { useCallback, useEffect, useMemo, useRef, useState } from "react"
import { useSearchParams } from "react-router"
import { Gauge, Loader2, RefreshCw } from "lucide-react"
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip as RechartsTooltip,
  CartesianGrid,
  ReferenceLine,
} from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { cn } from "@/lib/utils"
import { getErrorMessage } from "@/lib/errors"
import { useTheme } from "@/hooks/useTheme"
import { capacityService, type CapacityStatistics, type RingCapacity, type CapacityRingGroup } from "@/services/capacityService"
import { ringStatus, ringStatusClasses } from "@/lib/capacityStatus"
import { useStatsWindow, StatsWindowTabs } from "@/components/statistics/DnsStatsShared"

// /statistics/capacity — the detail page behind every CapacityIndicator pill
// (docs/ref/todo/statistics-capacity-visibility-plan.md T-13, GitHub issue
// #123): current/peak usage vs configured cap for all 9 RAM-only tracking
// rings/indices, grouped by Traffic/DNS/Firewall, each with a per-bucket bar
// chart (bucket-kind rings) or a plain current/cap bar (flat-kind rings).
// window lives in the URL (?window=) via the same useStatsWindow hook every
// other Statistics page uses; ?group= (set by CapacityIndicator's link) only
// highlights the matching section — every group is still shown, since a user
// who followed the link may still want the full picture.

const REFRESH_INTERVAL_MS = 10_000

const GROUP_LABELS: Record<CapacityRingGroup, string> = {
  traffic: "Traffic",
  dns: "DNS",
  firewall: "Firewall",
}
const GROUP_ORDER: CapacityRingGroup[] = ["traffic", "dns", "firewall"]

function RingChart({ ring }: { ring: RingCapacity }) {
  const { theme } = useTheme()
  const isDark = theme === "dark"
  const grid = isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.06)"
  const axis = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.45)"
  const status = ringStatus(ring)
  const barColor = status === "danger" ? "var(--destructive)" : status === "warn" ? "var(--warning)" : "var(--primary)"

  const data = useMemo(
    () =>
      (ring.series ?? []).map((p) => {
        const d = new Date(p.ts)
        const label = Number.isNaN(d.getTime())
          ? p.ts
          : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })
        return { time: label, count: p.count }
      }),
    [ring.series]
  )
  const tickInterval = Math.max(0, Math.ceil(data.length / 10) - 1)

  if (data.length === 0) {
    return <div className="flex h-40 items-center justify-center text-xs text-muted-foreground">ไม่มีข้อมูล series (ลองกด Refresh)</div>
  }

  return (
    <div className="h-40 w-full">
      <ResponsiveContainer width="100%" height="100%">
        <BarChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
          <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={grid} />
          <XAxis dataKey="time" stroke={axis} fontSize={10} tickLine={false} axisLine={false} interval={tickInterval} />
          <YAxis stroke={axis} fontSize={10} tickLine={false} axisLine={false} allowDecimals={false} width={36} />
          <RechartsTooltip
            cursor={{ fill: isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.04)" }}
            contentStyle={{
              backgroundColor: isDark ? "oklch(0.205 0 0)" : "#fff",
              border: `1px solid ${grid}`,
              borderRadius: "8px",
              fontSize: "12px",
              color: isDark ? "#fff" : "#111",
            }}
            formatter={(value) => [`${Number(value).toLocaleString()} รายการ`, "Tracked"]}
          />
          <ReferenceLine y={ring.cap} stroke="var(--destructive)" strokeDasharray="4 4" ifOverflow="extendDomain" />
          <Bar dataKey="count" name="Tracked" fill={barColor} radius={[2, 2, 0, 0]} isAnimationActive={false} />
        </BarChart>
      </ResponsiveContainer>
    </div>
  )
}

function FlatBar({ ring }: { ring: RingCapacity }) {
  const status = ringStatus(ring)
  const barClass = status === "danger" ? "bg-destructive" : status === "warn" ? "bg-warning" : "bg-primary"
  return (
    <div className="h-2 w-full overflow-hidden rounded-full bg-muted">
      <div
        className={cn("h-full rounded-full transition-all duration-500", barClass)}
        style={{ width: `${Math.min(100, Math.max(ring.currentPercent, 0))}%` }}
      />
    </div>
  )
}

function fmtRamBytes(n: number): string {
  if (!n || n <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB"]
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / 1024 ** i
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}

function RingCard({ ring, highlighted }: { ring: RingCapacity; highlighted: boolean }) {
  const status = ringStatus(ring)
  return (
    <Card className={cn(highlighted && "border-primary/50")}>
      <CardHeader>
        <div className="flex flex-wrap items-start justify-between space-y-0">
          <div>
            <CardTitle className="text-sm font-semibold">{ring.label}</CardTitle>
            <p className="mt-0.5 text-[11px] text-muted-foreground">ที่มาของ cap: {ring.capSource}</p>
          </div>
          <Badge variant="outline" className={cn("shrink-0 font-normal", ringStatusClasses[status])}>
            {Math.round(ring.peakPercent)}% peak
          </Badge>
        </div>
      </CardHeader>
      <CardContent className="space-y-3">
        <div className="flex flex-wrap items-baseline justify-between gap-2 text-xs">
          <span className="font-mono text-foreground">
            {ring.current.toLocaleString()} / {ring.cap.toLocaleString()}
          </span>
          <span className="text-muted-foreground">
            peak {ring.peak.toLocaleString()} · {ring.fullBuckets} bucket เต็ม
          </span>
        </div>
        {ring.kind === "bucket" ? <RingChart ring={ring} /> : <FlatBar ring={ring} />}
        <p className="text-[11px] text-muted-foreground">
          RAM โดยประมาณ: {fmtRamBytes(ring.estimatedBytes)} (ประมาณการ ไม่ใช่ค่าที่วัดจริง)
        </p>
      </CardContent>
    </Card>
  )
}

export default function StatisticsCapacity() {
  const [window_, setWindow] = useStatsWindow()
  const [data, setData] = useState<CapacityStatistics | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState<string | null>(null)

  const [searchParams] = useSearchParams()
  const highlightGroup = useMemo(() => {
    const raw = searchParams.get("group")
    if (!raw) return null
    return raw.split(",").filter((g): g is CapacityRingGroup => g === "traffic" || g === "dns" || g === "firewall")
  }, [searchParams])

  const load = useCallback(async (win: typeof window_, showLoading: boolean) => {
    if (showLoading) setIsLoading(true)
    try {
      const result = await capacityService.getCapacityStatistics(win, { series: true })
      setData(result)
      setError(null)
    } catch (err) {
      if (showLoading) setError(getErrorMessage(err))
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

  const ringsByGroup = useMemo(() => {
    const out: Record<CapacityRingGroup, RingCapacity[]> = { traffic: [], dns: [], firewall: [] }
    for (const r of data?.rings ?? []) {
      out[r.group].push(r)
    }
    return out
  }, [data])

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Gauge className="size-5" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight">Capacity</h1>
            <p className="text-xs text-muted-foreground">
              การใช้งาน RAM-only tracking ring/index เทียบ cap ทั้ง 9 มิติ — early warning ก่อนข้อมูลตกหล่น
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

      {error && !data && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-destructive">{error}</CardContent>
        </Card>
      )}

      {isLoading && !data ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
        </div>
      ) : data ? (
        GROUP_ORDER.map((group) => {
          const rings = ringsByGroup[group]
          if (rings.length === 0) return null
          const isHighlighted = highlightGroup?.includes(group) ?? false
          return (
            <div key={group} className="space-y-2">
              <h2 className={cn("text-sm font-semibold", isHighlighted && "text-primary")}>{GROUP_LABELS[group]}</h2>
              <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
                {rings.map((r) => (
                  <RingCard key={r.id} ring={r} highlighted={isHighlighted} />
                ))}
              </div>
            </div>
          )
        })
      ) : null}

      <p className="text-center text-[11px] text-muted-foreground">
        Auto-refresh ทุก 10 วินาที · ข้อมูลเก็บใน RAM เท่านั้น เริ่มนับใหม่ทุกครั้งที่ restart pigate
      </p>
    </div>
  )
}
