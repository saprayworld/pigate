// Shared "last matched at" formatters for the Firewall Policy usage-stats
// feature (docs/ref/todo/firewall-policy-rule-usage-stats-plan.md T-11). No
// new dependency — just Date/Intl, already available everywhere. Input is
// always the raw string PiGate stores/serves (RFC3339 or RFC3339Nano — see
// model.FirewallLog.Time / PolicyRuleStat.lastMatchedAt), which
// `new Date(...)` parses natively; an empty/unparseable value is treated as
// "unknown" rather than throwing, since this always feeds directly into UI
// text.

// fmtRelativeTime returns a short Thai "time ago" string (e.g. "5 วินาทีที่แล้ว",
// "3 ชั่วโมงที่แล้ว"), or "ไม่ทราบ" ("unknown") when at is empty/unparseable —
// matching the plan's Final acceptance wording for missing evidence (cleared
// ring buffer, rule never matched, etc).
export function fmtRelativeTime(at?: string): string {
  if (!at) return "ไม่ทราบ"
  const then = new Date(at)
  if (Number.isNaN(then.getTime())) return "ไม่ทราบ"

  const diffMs = Date.now() - then.getTime()
  if (diffMs < 0) return "เมื่อสักครู่"

  const sec = Math.floor(diffMs / 1000)
  if (sec < 5) return "เมื่อสักครู่"
  if (sec < 60) return `${sec} วินาทีที่แล้ว`

  const min = Math.floor(sec / 60)
  if (min < 60) return `${min} นาทีที่แล้ว`

  const hr = Math.floor(min / 60)
  if (hr < 24) return `${hr} ชั่วโมงที่แล้ว`

  const day = Math.floor(hr / 24)
  return `${day} วันที่แล้ว`
}

// fmtAbsoluteTime returns a localized (Thai locale) absolute date+time
// string for tooltips/detail views, or "ไม่ทราบ" when at is empty/unparseable.
export function fmtAbsoluteTime(at?: string): string {
  if (!at) return "ไม่ทราบ"
  const then = new Date(at)
  if (Number.isNaN(then.getTime())) return "ไม่ทราบ"
  return then.toLocaleString("th-TH", { dateStyle: "medium", timeStyle: "medium" })
}
