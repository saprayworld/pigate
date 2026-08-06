import type { RingCapacity } from "@/services/capacityService"

// Shared status classification for the Statistics -> Capacity feature
// (docs/ref/todo/statistics-capacity-visibility-plan.md T-11/T-13) — pure
// logic, no React components, so both CapacityIndicator.tsx (the pill) and
// pages/StatisticsCapacity.tsx (the detail page) import the exact same
// thresholds/colors from one place (also keeps CapacityIndicator.tsx a
// components-only file, required by the react-refresh/only-export-components
// lint rule).

export type CapacityStatus = "ok" | "warn" | "danger"

// ringStatus: danger when fullBuckets > 0 (this ring has ACTUALLY dropped
// data at least once) OR peakPercent >= 90; warn at peakPercent >= 70; ok
// otherwise. peakPercent (not currentPercent) drives the color — a ring that
// spiked earlier in the window and has since calmed down should still read
// as a warning, not ok.
export function ringStatus(ring: RingCapacity): CapacityStatus {
  if (ring.fullBuckets > 0 || ring.peakPercent >= 90) return "danger"
  if (ring.peakPercent >= 70) return "warn"
  return "ok"
}

// Semantic-only colors (docs/rules_of_work.md — no hardcoded Tailwind
// palette classes like text-amber-500, no shadow-*/backdrop-blur-*).
// ok uses --primary (green) rather than a neutral gray so the icon-only
// compact form (CapacityIndicator on small screens) still reads as a
// traffic-light: green/yellow/red, not "colored when bad, gray when fine".
export const ringStatusClasses: Record<CapacityStatus, string> = {
  ok: "border-primary/30 bg-primary/10 text-primary",
  warn: "border-warning/30 bg-warning/10 text-warning",
  danger: "border-destructive/30 bg-destructive/10 text-destructive",
}
