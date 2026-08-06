import { useMemo } from "react"
import {
  ResponsiveContainer,
  BarChart,
  Bar,
  XAxis,
  YAxis,
  Tooltip,
  CartesianGrid,
} from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { cn } from "@/lib/utils"
import { useTheme } from "@/hooks/useTheme"
import type { DNSQueryPoint } from "@/services/dnsStatisticsService"
import { statsWindowLongLabel, type StatsWindow } from "@/lib/statsWindow"

// DnsQueryTrendCard is the Statistics -> DNS overview page's DNS-query-count
// bar chart (docs/ref/todo/statistics-dns-query-bar-chart-plan.md T-07). A
// new component rather than a mode on TrafficTrendCard: that component is
// bytes-shaped end to end (2 lines up/down, Bytes/Speed toggle, fmtBytes/
// fmtRate, LAN-relative subtitle) and this data is a single query-count value
// per bucket — reusing it would only save the grid/axis theme colors, at the
// cost of making a 4-page-shared component fragile (plan §2.6). The
// useTheme()-derived colors, tickInterval formula, isAnimationActive={false},
// and X-axis time-label formatting below intentionally mirror
// TrafficTrendCard's pattern so the two charts read consistently.
export function DnsQueryTrendCard({
  series,
  window: statsWindow,
  className,
}: {
  series: DNSQueryPoint[] | undefined
  window: StatsWindow
  className?: string
}) {
  const { theme } = useTheme()
  const isDark = theme === "dark"
  const grid = isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.06)"
  const axis = isDark ? "rgba(255,255,255,0.45)" : "rgba(0,0,0,0.45)"
  const barColor = "var(--primary)"

  const points = useMemo(() => series ?? [], [series])

  const data = useMemo(
    () =>
      points.map((p) => {
        const d = new Date(p.ts)
        const label = Number.isNaN(d.getTime())
          ? p.ts
          : d.toLocaleTimeString(undefined, { hour: "2-digit", minute: "2-digit", hour12: false })
        return { time: label, count: p.count }
      }),
    [points]
  )

  const hasSignal = points.some((p) => p.count > 0)

  // Aim for ~8-12 X-axis labels regardless of window (3/6/12/36/72/144/288
  // points for 15m/30m/1h/3h/6h/12h/24h respectively) — same formula as
  // TrafficTrendCard so both charts feel consistent.
  const tickInterval = Math.max(0, Math.ceil(data.length / 10) - 1)

  const windowLabel = statsWindowLongLabel(statsWindow)

  return (
    <Card className={cn(className)}>
      <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-x-4 gap-y-1 space-y-0">
        <div>
          <CardTitle className="text-base font-semibold">DNS Queries · {windowLabel}</CardTitle>
          <p className="text-[11px] text-muted-foreground">
            จำนวน query ต่อช่วง 5 นาที · แท่งสุดท้ายยังเก็บข้อมูลไม่ครบช่วง · เก็บใน RAM เท่านั้น
          </p>
        </div>
      </CardHeader>
      <CardContent>
        <div className="h-56 w-full">
          {!hasSignal ? (
            <div className="flex h-full items-center justify-center text-sm text-muted-foreground">
              ยังไม่มี DNS query ในช่วงเวลานี้
            </div>
          ) : (
            <ResponsiveContainer width="100%" height="100%">
              <BarChart data={data} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
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
                  allowDecimals={false}
                  tickFormatter={(v) => Number(v).toLocaleString()}
                  width={48}
                />
                <Tooltip
                  cursor={{ fill: isDark ? "rgba(255,255,255,0.06)" : "rgba(0,0,0,0.04)" }}
                  contentStyle={{
                    backgroundColor: isDark ? "oklch(0.205 0 0)" : "#fff",
                    border: `1px solid ${grid}`,
                    borderRadius: "8px",
                    fontSize: "12px",
                    color: isDark ? "#fff" : "#111",
                  }}
                  formatter={(value) => [`${Number(value).toLocaleString()} ครั้ง / 5 นาที`, "Queries"]}
                />
                <Bar dataKey="count" name="Queries" fill={barColor} radius={[2, 2, 0, 0]} isAnimationActive={false} />
              </BarChart>
            </ResponsiveContainer>
          )}
        </div>
      </CardContent>
    </Card>
  )
}
