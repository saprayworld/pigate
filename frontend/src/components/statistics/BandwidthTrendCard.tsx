import { useMemo } from "react"
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
import { cn } from "@/lib/utils"
import { fmtBytes } from "@/lib/formatBytes"
import { useTheme } from "@/hooks/useTheme"
import type { BandwidthPoint } from "@/services/statisticsService"
import type { StatsWindow } from "@/components/statistics/DnsStatsShared"

// BandwidthTrendCard is the Statistics Overview page's bandwidth-over-time
// chart (docs/ref/todo/statistics-overview-bandwidth-chart-plan.md T-07) —
// same recharts setup as Dashboard.tsx's BandwidthCard (grid/axis colors via
// useTheme, dot={false}/isAnimationActive={false} for perf at up to 288
// points), but reading TrafficStatistics.series directly instead of the
// Dashboard's own hourly-aggregated ring, and using fmtBytes (auto KB/MB/GB)
// instead of a hardcoded "G" unit — a 5-minute bucket on a home network is
// often well under 1 GB.
//
// IMPORTANT: series direction is LAN-relative (Up = leaving the LAN, Down =
// entering it) — a different convention from the Top Hosts cards' bytesUp/
// bytesDown (flow-relative). The subtitle below says so explicitly (plan §5
// item 18 / §8 item 7).
export function BandwidthTrendCard({
  series,
  window: statsWindow,
  className,
  subtitle,
}: {
  series: BandwidthPoint[] | undefined
  window: StatsWindow
  className?: string
  // subtitle, when provided, REPLACES the default LAN-relative subtitle line
  // below entirely (docs/ref/todo/statistics-traffic-bandwidth-chart-plan.md
  // T-06) — it does not append to it. Required for any caller whose series is
  // NOT LAN-relative (e.g. a per-IP drill-down, which is flow-relative): the
  // default text is simply wrong there. Omit to keep pixel-identical
  // rendering to before this prop existed (Overview page, Traffic list page).
  subtitle?: string
}) {
  const { theme } = useTheme()
  const isDark = theme === "dark"
  const grid = isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.06)"
  const axis = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.45)"
  const downloadColor = "var(--primary)"
  const uploadColor = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.38)"

  const data = useMemo(
    // series ?? [] defensively (plan §5): a stale cached frontend build
    // talking to a newer/older backend must not crash on undefined.map.
    () =>
      (series ?? []).map((p) => {
        const d = new Date(p.ts)
        const label = Number.isNaN(d.getTime())
          ? p.ts
          : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })
        return { time: label, download: p.bytesDown, upload: p.bytesUp, total: p.bytes }
      }),
    [series]
  )
  const points = series ?? []

  const hasSignal = points.some((p) => p.bytes > 0)

  // Aim for ~8-12 X-axis labels regardless of window (12 points for 1h, 288
  // for 24h) — a raw `interval="preserveStartEnd"`/auto at 288 points would
  // overlap into an unreadable smear on a 2/3-width card (plan T-07).
  const tickInterval = Math.max(0, Math.ceil(data.length / 10) - 1)

  const windowLabel = statsWindow === "24h" ? "24 ชม. ล่าสุด" : "1 ชม. ล่าสุด"

  return (
    <Card className={cn(className)}>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-x-4 gap-y-1 space-y-0">
        <div>
          <CardTitle className="text-base font-semibold">Bandwidth · {windowLabel}</CardTitle>
          <p className="text-[11px] text-muted-foreground">
            {subtitle ?? "ยอดต่อ 5 นาที (ไม่ใช่ความเร็ว) · Up/Down นับตามทิศทางเข้า-ออกเครือข่าย LAN"}
          </p>
        </div>
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
          {!hasSignal ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              กำลังเก็บข้อมูล traffic…
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <LineChart data={data} margin={{ top: 8, right: 8, left: -24, bottom: 0 }}>
                <CartesianGrid strokeDasharray="3 3" vertical={false} stroke={grid} />
                <XAxis
                  dataKey="time"
                  stroke={axis}
                  fontSize={11}
                  tickLine={false}
                  axisLine={false}
                  interval={tickInterval}
                />
                <YAxis
                  stroke={axis}
                  fontSize={11}
                  tickLine={false}
                  axisLine={false}
                  tickFormatter={(v) => fmtBytes(Number(v))}
                  width={64}
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
                  formatter={(value, name) => [`${fmtBytes(Number(value))} / 5 นาที`, name]}
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
