import { useNavigate } from "react-router"
import { ArrowRight, Gauge } from "lucide-react"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
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
//
// Responsive (matches the AccuracyInfoButton click-to-open pattern in
// TrafficStatsShared.tsx, not a hover Tooltip — touch devices have no
// hover): below `sm` only the color-coded icon shows (the badge's percent/
// bucket text collapses via `hidden sm:inline`), acting as a traffic-light.
// Clicking opens a Popover with the detail; navigating to the full capacity
// page is its own explicit "ดูเพิ่มเติม" button inside the popover, not the
// trigger itself.

type GroupFilter = CapacityRingGroup | CapacityRingGroup[]

function matchesGroup(ring: RingCapacity, group?: GroupFilter): boolean {
  if (!group) return true
  if (Array.isArray(group)) return group.includes(ring.group)
  return ring.group === group
}

export function CapacityIndicator({
  rings,
  group,
  excludeIds,
  window: statsWindow,
  className,
}: {
  rings: RingCapacity[] | undefined
  group?: GroupFilter
  // Ring ids to drop even if they match `group` — e.g. firewall.logBuffer is
  // its own "log count" concern surfaced on the Log & Report page, not the
  // general capacity pill on Overview/Traffic.
  excludeIds?: string[]
  window: StatsWindow
  className?: string
}) {
  const navigate = useNavigate()

  const candidates = (rings ?? []).filter(
    (r) => matchesGroup(r, group) && !excludeIds?.includes(r.id)
  )
  if (candidates.length === 0) return null

  const worst = candidates.reduce((a, b) => (b.peakPercent > a.peakPercent ? b : a))
  const status = ringStatus(worst)

  const groupParam = Array.isArray(group) ? group.join(",") : group
  const href = `/statistics/capacity?window=${statsWindow}${groupParam ? `&group=${groupParam}` : ""}`

  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className={cn("cursor-pointer", ringStatusClasses[status], className)}
          aria-label="ดูสถานะการใช้งาน capacity"
        >
          <Gauge className="size-4" />
          <span className="hidden sm:inline">
            {Math.round(worst.peakPercent)}% · {worst.fullBuckets} bucket เต็ม
          </span>
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-64 space-y-2 text-xs">
        <p className="font-medium">{worst.label}</p>
        <p>
          {worst.current.toLocaleString()} / {worst.cap.toLocaleString()} (peak {Math.round(worst.peakPercent)}%)
        </p>
        <p className="text-muted-foreground">ที่มาของ cap: {worst.capSource}</p>
        {worst.fullBuckets > 0 && (
          <p className="text-destructive">เคยเต็ม {worst.fullBuckets} ช่วงเวลาในหน้าต่างที่เลือก</p>
        )}
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="w-full cursor-pointer justify-center gap-1"
          onClick={() => navigate(href)}
        >
          ดูเพิ่มเติม
          <ArrowRight className="size-3" />
        </Button>
      </PopoverContent>
    </Popover>
  )
}
