// fmtBytes is the shared human-readable byte formatter (docs/ref/todo/
// statistics-overview-bandwidth-chart-plan.md T-08A) — moved here verbatim
// (same formula, same rounding) from what used to be two identical local
// copies in pages/Dashboard.tsx and pages/StatisticsOverview.tsx, so both
// pages (and any new component) share exactly one implementation. Do not
// change the formula/rounding here without checking every caller: this is a
// pure move, not a behavior change.
export function fmtBytes(n: number): string {
  if (!n || n <= 0) return "0 B"
  const units = ["B", "KB", "MB", "GB", "TB"]
  const i = Math.min(units.length - 1, Math.floor(Math.log(n) / Math.log(1024)))
  const v = n / 1024 ** i
  return `${v.toFixed(v >= 100 || i === 0 ? 0 : 1)} ${units[i]}`
}
