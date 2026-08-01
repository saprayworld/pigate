// CHART_BG_CLASSES is the shared ordered palette for stacked-bar/legend chart
// components (docs/ref/todo/statistics-overview-bandwidth-chart-plan.md
// T-08A) — moved here verbatim from pages/Dashboard.tsx (ProtocolBreakdownCard)
// so new cards (e.g. TopHostsShareCard) can reuse the same theme-variable
// colors (bg-chart-1..5, src/index.css) without duplicating the array. Do not
// change the values/order — anything already indexing into this array
// (Dashboard's Protocol Breakdown) must keep the same color per index.
export const CHART_BG_CLASSES = ["bg-chart-1", "bg-chart-2", "bg-chart-3", "bg-chart-4", "bg-chart-5"]
