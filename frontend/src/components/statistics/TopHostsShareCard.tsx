import { cn } from "@/lib/utils"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { fmtBytes } from "@/lib/formatBytes"
import { CHART_BG_CLASSES } from "@/lib/chartColors"
import { UpDownLine, HostLabel } from "@/components/statistics/HostCells"
import { HostReferenceTrigger } from "@/components/reference/HostReferenceTrigger"
import type { TopHost } from "@/services/statisticsService"

// TopHostsShareCard is the Statistics Overview page's Top 5 Hosts card
// (docs/ref/todo/statistics-overview-bandwidth-chart-plan.md T-08B) —
// modeled on Dashboard.tsx's ProtocolBreakdownCard exactly (stacked bar +
// dot/legend list), reusing the shared CHART_BG_CLASSES palette (T-08A).
//
// Per plan §7 locked decisions:
//   - hosts must already be sorted/limited by the caller (top 5 of
//     topSources, byte-rank order from the API) — never re-sorted here
//     (mtd 2).
//   - percent comes straight from TopHost.percent (denominator =
//     observedBytes for the whole window), never re-normalized to sum to
//     100% across just these 5 rows (mtd 3) — the remainder renders as a
//     trailing "Other" segment/row.
//   - no LAN/Internet filtering (mtd 4) — HostLabel's badge still shows it.
export function TopHostsShareCard({
  hosts,
  observedBytes,
  className,
}: {
  hosts: TopHost[]
  observedBytes: number
  className?: string
}) {
  const sumBytes = hosts.reduce((sum, h) => sum + h.bytes, 0)
  const sumPercent = hosts.reduce((sum, h) => sum + h.percent, 0)
  // Rounding in the API's percentOf (1 decimal place) can push sumPercent
  // slightly over 100 — clamp so the "Other" segment/row never goes
  // negative-width or shows a negative byte figure.
  const otherPercent = Math.max(0, 100 - sumPercent)
  const otherBytes = Math.max(0, observedBytes - sumBytes)
  const showOther = otherPercent > 0.05 && observedBytes > 0

  return (
    <Card className={cn(className)}>
      <CardHeader className="space-y-0">
        <CardTitle className="text-base font-semibold">Top 5 Hosts by Usage</CardTitle>
      </CardHeader>
      <CardContent className="space-y-4">
        {hosts.length === 0 ? (
          <p className="py-8 text-center text-sm text-muted-foreground">
            ยังไม่มีข้อมูล host ในช่วงเวลานี้
          </p>
        ) : (
          <>
            <div className="flex h-2.5 w-full overflow-hidden rounded-full bg-muted">
              {hosts.map((h, i) => (
                <div
                  key={h.ip}
                  className={cn("h-full", CHART_BG_CLASSES[i % CHART_BG_CLASSES.length])}
                  style={{ width: `${Math.max(h.percent, 1)}%` }}
                  title={`${h.hostname}: ${h.percent}%`}
                />
              ))}
              {showOther && (
                <div className="h-full bg-muted-foreground/30" style={{ width: `${Math.max(otherPercent, 1)}%` }} title={`อื่น ๆ: ${otherPercent}%`} />
              )}
            </div>
            <ul className="space-y-2.5">
              {hosts.map((h, i) => (
                <li key={h.ip} className="flex items-center justify-between gap-3 text-sm">
                  <span className="flex min-w-0 items-center gap-2">
                    <span
                      className={cn(
                        "h-2.5 w-2.5 shrink-0 rounded-full",
                        CHART_BG_CLASSES[i % CHART_BG_CLASSES.length]
                      )}
                    />
                    <HostReferenceTrigger ip={h.ip} domain={h.domain}>
                      <HostLabel host={h} />
                    </HostReferenceTrigger>
                  </span>
                  <span className="shrink-0 text-right">
                    <span className="block font-mono text-xs text-muted-foreground">
                      {fmtBytes(h.bytes)} · {h.percent}%
                    </span>
                    <UpDownLine up={h.bytesUp} down={h.bytesDown} />
                  </span>
                </li>
              ))}
              {showOther && (
                <li className="flex items-center justify-between gap-3 text-sm">
                  <span className="flex min-w-0 items-center gap-2">
                    <span className="h-2.5 w-2.5 shrink-0 rounded-full bg-muted-foreground/30" />
                    <span className="truncate text-muted-foreground">อื่น ๆ</span>
                  </span>
                  <span className="shrink-0 font-mono text-xs text-muted-foreground">
                    {fmtBytes(otherBytes)} · {otherPercent.toFixed(1)}%
                  </span>
                </li>
              )}
            </ul>
          </>
        )}
      </CardContent>
    </Card>
  )
}
