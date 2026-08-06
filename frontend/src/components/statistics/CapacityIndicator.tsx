import { useNavigate } from "react-router"
import { Gauge } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Tooltip, TooltipContent, TooltipTrigger } from "@/components/ui/tooltip"
import { cn } from "@/lib/utils"
import type { RingCapacity, CapacityRingGroup } from "@/services/capacityService"
import type { StatsWindow } from "@/lib/statsWindow"
import { ringStatus, ringStatusClasses } from "@/lib/capacityStatus"

// CapacityIndicator is the small pill next to a Statistics page's window
// tabs that surfaces the fullest RAM-only tracking ring for that page's
// group(s) (docs/ref/todo/statistics-capacity-visibility-plan.md T-11,
// GitHub issue #123) — an early-warning signal on top of the existing
// after-the-fact `truncated` badges elsewhere on these pages (a DIFFERENT
// role: this one is proactive, those are reactive; both stay, side by side
// — plan T-12 explicitly forbids touching the existing warnings).
//
// Renders nothing at all when there is no data yet (rings empty/undefined)
// or no ring matches the requested group(s) — a fetch failure upstream must
// never break the page it's attached to, so callers simply don't render this
// component (or pass an empty array) on error.

type GroupFilter = CapacityRingGroup | CapacityRingGroup[]

function matchesGroup(ring: RingCapacity, group?: GroupFilter): boolean {
  if (!group) return true
  if (Array.isArray(group)) return group.includes(ring.group)
  return ring.group === group
}

export function CapacityIndicator({
  rings,
  group,
  window: statsWindow,
  className,
}: {
  rings: RingCapacity[] | undefined
  group?: GroupFilter
  window: StatsWindow
  className?: string
}) {
  const navigate = useNavigate()

  const candidates = (rings ?? []).filter((r) => matchesGroup(r, group))
  if (candidates.length === 0) return null

  const worst = candidates.reduce((a, b) => (b.peakPercent > a.peakPercent ? b : a))
  const status = ringStatus(worst)

  const groupParam = Array.isArray(group) ? group.join(",") : group
  const href = `/statistics/capacity?window=${statsWindow}${groupParam ? `&group=${groupParam}` : ""}`

  return (
    <Tooltip>
      <TooltipTrigger asChild>
        <button
          type="button"
          onClick={() => navigate(href)}
          className={cn("cursor-pointer", className)}
          aria-label="ดูรายละเอียดการใช้งาน capacity"
        >
          <Badge variant="outline" className={cn("gap-1 font-normal", ringStatusClasses[status])}>
            <Gauge className="size-3" />
            {Math.round(worst.peakPercent)}% · {worst.fullBuckets} bucket เต็ม
          </Badge>
        </button>
      </TooltipTrigger>
      <TooltipContent align="end" className="max-w-64 flex-col items-start gap-0.5 whitespace-normal text-xs">
        <p className="font-medium">{worst.label}</p>
        <p>
          {worst.current.toLocaleString()} / {worst.cap.toLocaleString()} (peak {Math.round(worst.peakPercent)}%)
        </p>
        <p className="text-muted-foreground">ที่มาของ cap: {worst.capSource}</p>
        {worst.fullBuckets > 0 && (
          <p className="text-destructive">เคยเต็ม {worst.fullBuckets} ช่วงเวลาในหน้าต่างที่เลือก</p>
        )}
      </TooltipContent>
    </Tooltip>
  )
}
