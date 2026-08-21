import { useMemo } from "react"
import { Pie, PieChart, Cell } from "recharts"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import {
  ChartContainer,
  ChartTooltip,
  ChartTooltipContent,
  type ChartConfig,
} from "@/components/ui/chart"
import { cn } from "@/lib/utils"

// DnsBlockedDonutCard is the Statistics -> DNS overview page's "Allowed vs
// Blocked" donut chart (docs/ref/todo/dns-blocked-query-statistics-plan.md
// T-13) — colors come from a ChartConfig (allowed = --primary, blocked =
// --warning), never a hardcoded hex/Tailwind color class, so both dark/light
// mode read correctly (rules_of_work.md §1). Flat design only: no shadow-*/
// backdrop-blur-* anywhere in this file.
const chartConfig = {
  allowed: { label: "Allowed", color: "var(--primary)" },
  blocked: { label: "Blocked", color: "var(--warning)" },
} satisfies ChartConfig

export function DnsBlockedDonutCard({
  totalQueries,
  blockedQueries,
  blockedPercent,
  className,
}: {
  totalQueries: number
  blockedQueries: number
  // blockedPercent is passed through (rather than recomputed here) so the
  // displayed percentage always matches DNSQueryStatistics.blockedPercent
  // exactly, including its own rounding — never re-derived client-side.
  blockedPercent: number
  className?: string
}) {
  const allowedQueries = Math.max(0, totalQueries - blockedQueries)

  const data = useMemo(
    () => [
      { key: "allowed", value: allowedQueries, label: "Allowed" },
      { key: "blocked", value: blockedQueries, label: "Blocked" },
    ],
    [allowedQueries, blockedQueries]
  )

  // Empty state (docs/ref/todo/dns-blocked-query-statistics-plan.md T-13):
  // no query at all, or a deny-list that has never matched anything —
  // showing an all-grey/degenerate donut would be misleading, so an
  // explicit message replaces the chart instead.
  const isEmpty = totalQueries === 0 || blockedQueries === 0

  return (
    <Card className={cn(className)}>
      <CardHeader>
        <CardTitle className="text-base font-semibold">Allowed vs Blocked</CardTitle>
        <p className="text-[11px] text-muted-foreground">
          สัดส่วน DNS query ที่ถูกบล็อกโดย deny-list เทียบกับ query ปกติในช่วงเวลานี้
        </p>
      </CardHeader>
      <CardContent>
        {isEmpty ? (
          <div className="flex h-56 flex-col items-center justify-center gap-1 text-center text-sm text-muted-foreground">
            <p>{totalQueries === 0 ? "ยังไม่มี DNS query ในช่วงเวลานี้" : "ยังไม่มี query ที่ถูกบล็อกในช่วงเวลานี้"}</p>
          </div>
        ) : (
          <div className="flex flex-col items-center gap-3">
            <ChartContainer config={chartConfig} className="mx-auto aspect-square h-48 w-full">
              <PieChart>
                <ChartTooltip
                  content={
                    <ChartTooltipContent
                      hideLabel
                      formatter={(value, name) => [
                        `${Number(value).toLocaleString()} query`,
                        name === "allowed" ? "Allowed" : "Blocked",
                      ]}
                    />
                  }
                />
                <Pie
                  data={data}
                  dataKey="value"
                  nameKey="key"
                  innerRadius="60%"
                  outerRadius="90%"
                  strokeWidth={2}
                  isAnimationActive={false}
                >
                  {data.map((entry) => (
                    <Cell key={entry.key} fill={`var(--color-${entry.key})`} />
                  ))}
                </Pie>
              </PieChart>
            </ChartContainer>
            <div className="text-center">
              <p className="text-2xl font-bold tracking-tight text-warning">{blockedPercent.toFixed(1)}%</p>
              <p className="text-[11px] text-muted-foreground">ของ query ทั้งหมดถูกบล็อก</p>
            </div>
            <div className="flex items-center gap-4 text-xs">
              <span className="flex items-center gap-1.5">
                <span className="size-2.5 rounded-full bg-primary" />
                Allowed <span className="font-mono text-muted-foreground">{allowedQueries.toLocaleString()}</span>
              </span>
              <span className="flex items-center gap-1.5">
                <span className="size-2.5 rounded-full bg-warning" />
                Blocked <span className="font-mono text-muted-foreground">{blockedQueries.toLocaleString()}</span>
              </span>
            </div>
          </div>
        )}
      </CardContent>
    </Card>
  )
}
