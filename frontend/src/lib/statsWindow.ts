// Single source of truth for the Statistics pages' time-window selector
// (docs/ref/todo/statistics-window-granularity-plan.md §2.3/T-09) — pure
// TypeScript, no React/JSX/DOM, so both the service layer
// (statisticsService.ts, trafficStatisticsService.ts, dnsStatisticsService.ts)
// and the presentational StatsWindowTabs component can import from here
// without a layering violation. Every value below is derived from the
// backend's 5-minute bucket ring: it MUST stay in sync with statsWindowBuckets
// in backend/internal/service/traffic_stats.go (15m:3, 30m:6, 1h:12, 3h:36,
// 6h:72, 12h:144, 24h:288) — do not add a window here without adding it there
// first.

export type StatsWindow = "15m" | "30m" | "1h" | "3h" | "6h" | "12h" | "24h"

// STATS_WINDOWS is ordered exactly as the buttons should appear on screen.
// label is the UPPERCASE-H button text the owner specified (§0 D-4); value is
// always lowercase because it is sent verbatim to the API/URL, and the
// backend treats any other case ("1H") as an unrecognized value, not an alias
// — never derive label from value via .toUpperCase() or vice versa, keep them
// as two explicit fields so that mistake can't happen by construction.
export const STATS_WINDOWS: readonly {
  value: StatsWindow
  label: string
  minutes: number
  points: number
}[] = [
  { value: "15m", label: "15m", minutes: 15, points: 3 },
  { value: "30m", label: "30m", minutes: 30, points: 6 },
  { value: "1h", label: "1H", minutes: 60, points: 12 },
  { value: "3h", label: "3H", minutes: 180, points: 36 },
  { value: "6h", label: "6H", minutes: 360, points: 72 },
  { value: "12h", label: "12H", minutes: 720, points: 144 },
  { value: "24h", label: "24H", minutes: 1440, points: 288 },
]

const STATS_WINDOW_VALUES = new Set<string>(STATS_WINDOWS.map((w) => w.value))

// parseStatsWindow returns raw unchanged when it is one of the 7 canonical
// (lowercase) values, or "1h" for anything else — including null/undefined,
// empty string, and label-cased values like "1H" (must match the backend's
// normalizeStatsWindow fallback exactly). Deliberately does NOT lowercase raw
// first: a bug that sends the button label instead of its value must show up
// as "the wrong window's data loaded", not be silently masked here (plan §0
// D-4/§6 item 3).
export function parseStatsWindow(raw: string | null | undefined): StatsWindow {
  if (raw && STATS_WINDOW_VALUES.has(raw)) {
    return raw as StatsWindow
  }
  return "1h"
}

// statsWindowLongLabel is the long-form Thai label used in chart card
// headers. "1h"/"24h" produce byte-for-byte the same strings the pre-existing
// TrafficTrendCard.tsx used (regression guard), and the new windows follow
// the same "<duration> ล่าสุด" pattern.
export function statsWindowLongLabel(w: StatsWindow): string {
  switch (w) {
    case "15m":
      return "15 นาทีล่าสุด"
    case "30m":
      return "30 นาทีล่าสุด"
    case "1h":
      return "1 ชม. ล่าสุด"
    case "3h":
      return "3 ชม. ล่าสุด"
    case "6h":
      return "6 ชม. ล่าสุด"
    case "12h":
      return "12 ชม. ล่าสุด"
    case "24h":
      return "24 ชม. ล่าสุด"
  }
}

// statsWindowSeconds converts a window to its duration in seconds.
export function statsWindowSeconds(w: StatsWindow): number {
  const entry = STATS_WINDOWS.find((e) => e.value === w)
  return (entry?.minutes ?? 60) * 60
}

// mockWindowScale is the mock-data-generator scale factor keyed by window —
// "1h" -> 1 and "24h" -> 18 are byte-for-byte the same values the pre-existing
// mock generators used (regression guard); the new windows interpolate
// between them. Callers that use this as a row/entry COUNT must
// Math.max(1, Math.round(...)) it themselves (this can be < 1, e.g. 15m ->
// 0.3) — see docs/ref/todo/statistics-window-granularity-plan.md §6 item 4.
export function mockWindowScale(w: StatsWindow): number {
  switch (w) {
    case "15m":
      return 0.3
    case "30m":
      return 0.6
    case "1h":
      return 1
    case "3h":
      return 3
    case "6h":
      return 5
    case "12h":
      return 10
    case "24h":
      return 18
  }
}
