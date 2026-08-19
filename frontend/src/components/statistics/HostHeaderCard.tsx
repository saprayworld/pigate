import { Globe, Monitor, RefreshCw } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Card, CardContent } from "@/components/ui/card"
import { Skeleton } from "@/components/ui/skeleton"
import { AccuracyInfoButton } from "@/components/statistics/TrafficStatsShared"
import { StatsWindowTabs, type StatsWindow } from "@/components/statistics/DnsStatsShared"
import { HostReferenceTrigger } from "@/components/reference/HostReferenceTrigger"
import { type TrafficHostDetail } from "@/services/trafficStatisticsService"

// HostHeaderCard — pure presentational header card for the Statistics ->
// Traffic -> Host drill-down page (docs/ref/todo/
// statistics-traffic-host-header-plan.md T-05). No fetching/routing here:
// the page owns `window`/`detail`/`onRefresh`, this component only renders
// them. Replaces the old plain <div> header (title/badges/accuracy/window
// selector/refresh all lived directly in the page — now consolidated here).
export function HostHeaderCard({
  ip,
  detail,
  isLoading,
  window,
  onWindowChange,
  onRefresh,
}: {
  ip: string
  detail: TrafficHostDetail | null
  isLoading: boolean
  window: StatsWindow
  onWindowChange: (w: StatsWindow) => void
  onRefresh: () => void
}) {
  const Icon = detail?.private === false ? Globe : Monitor

  const showActive = detail?.private === true && detail?.active === true
  const hostname = detail?.hostname && detail.hostname !== ip ? detail.hostname : ""

  const metaParts: string[] = []
  if (hostname) metaParts.push(`Hostname: ${hostname}`)
  if (detail?.mac) metaParts.push(`MAC: ${detail.mac}`)
  if (detail?.domain) metaParts.push(`Domain: ${detail.domain}`)

  return (
    <Card>
      <CardContent className="flex flex-wrap items-start justify-between gap-4">
        <div className="flex min-w-0 items-start gap-3">
          <div className="flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Icon className="size-5" />
          </div>
          <div className="min-w-0">
            <div className="flex flex-wrap items-center gap-2">
              <HostReferenceTrigger ip={ip} domain={detail?.domain ?? ""} className="min-w-0">
                <span className="truncate font-mono text-xl font-bold tracking-tight">{ip}</span>
              </HostReferenceTrigger>
              {showActive && (
                <Badge variant="outline" className="font-normal border-primary/30 text-primary">
                  Active
                </Badge>
              )}
              {detail && (
                <Badge
                  variant="outline"
                  className={
                    detail.private
                      ? "font-normal border-primary/30 text-primary"
                      : "font-normal border-muted-foreground/30 text-muted-foreground"
                  }
                >
                  {detail.private ? "LAN" : "Internet"}
                </Badge>
              )}
              {detail?.domain && (
                <Badge variant="outline" className="font-normal border-primary/20 bg-primary/10 text-primary">
                  {detail.domain}
                </Badge>
              )}
            </div>
            {isLoading && !detail ? (
              <Skeleton className="mt-1.5 h-3 w-48" />
            ) : (
              <p className="mt-1 truncate text-xs text-muted-foreground">
                {metaParts.length > 0 ? (
                  metaParts.map((part, i) => (
                    <span key={i} className={part.startsWith("MAC:") ? "font-mono" : undefined}>
                      {i > 0 && " · "}
                      {part}
                    </span>
                  ))
                ) : (
                  "ไม่มีข้อมูลอุปกรณ์ (ไม่พบ DHCP lease/reservation ของ IP นี้)"
                )}
              </p>
            )}
          </div>
        </div>

        <div className="flex w-full min-w-0 flex-wrap items-center justify-start gap-2 sm:w-auto sm:justify-end">
          <AccuracyInfoButton accuracy={detail?.accuracy} />
          <StatsWindowTabs value={window} onChange={onWindowChange} />
          <Button
            variant="outline"
            size="sm"
            onClick={onRefresh}
            disabled={isLoading}
            className="cursor-pointer gap-1.5"
            title="Refresh"
            aria-label="Refresh"
          >
            <RefreshCw className={isLoading ? "h-4 w-4 animate-spin" : "h-4 w-4"} />
            <span className="hidden sm:inline">Refresh</span>
          </Button>
        </div>
      </CardContent>
    </Card>
  )
}
