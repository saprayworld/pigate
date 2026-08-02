import { useMemo, useState } from "react"
import { ArrowDown, ArrowUp, ChevronsUpDown, Search, TriangleAlert, X } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Input } from "@/components/ui/input"
import { TableHead } from "@/components/ui/table"
import { cn } from "@/lib/utils"

// Shared presentation pieces for the Statistics -> Traffic pages
// (docs/ref/todo/statistics-traffic-page-plan.md T-08) — pure
// components/hooks only: no data fetching, no router navigation here (that
// stays in the pages, T-09/T-10).

export type SortDir = "asc" | "desc"
export interface SortState<T> {
  key: keyof T & string
  dir: SortDir
}

// useSortableRows sorts `rows` by the current sort key, stably and
// type-aware (numbers numerically, strings with localeCompare). Toggling the
// currently-active column flips direction; switching to a new column starts
// at 'desc' for a numeric column, 'asc' for a text column (matched by
// inspecting the first non-null/undefined value found for that key).
export function useSortableRows<T>(
  rows: T[],
  initial: SortState<T>
): { rows: T[]; sort: SortState<T>; toggle: (key: keyof T & string) => void } {
  const [sort, setSort] = useState<SortState<T>>(initial)

  const toggle = (key: keyof T & string) => {
    setSort((prev) => {
      if (prev.key === key) {
        return { key, dir: prev.dir === "asc" ? "desc" : "asc" }
      }
      const sample = rows.find((r) => r[key] !== undefined && r[key] !== null)?.[key]
      const defaultDir: SortDir = typeof sample === "number" ? "desc" : "asc"
      return { key, dir: defaultDir }
    })
  }

  const sorted = useMemo(() => {
    // Array.prototype.sort is stable per spec (Node/V8/all evergreen
    // browsers this app targets) — equal-key rows keep their relative order.
    const copy = [...rows]
    copy.sort((a, b) => {
      const av = a[sort.key]
      const bv = b[sort.key]
      let cmp: number
      if (typeof av === "number" && typeof bv === "number") {
        cmp = av - bv
      } else {
        cmp = String(av ?? "").localeCompare(String(bv ?? ""))
      }
      return sort.dir === "asc" ? cmp : -cmp
    })
    return copy
  }, [rows, sort])

  return { rows: sorted, sort, toggle }
}

// SortableHead renders a <TableHead> whose whole label is a keyboard-focusable
// button carrying a lucide sort-direction indicator.
export function SortableHead<T>({
  label,
  sortKey,
  sort,
  onToggle,
  align = "left",
  className,
}: {
  label: string
  sortKey: keyof T & string
  sort: SortState<T>
  onToggle: (key: keyof T & string) => void
  align?: "left" | "right"
  className?: string
}) {
  const active = sort.key === sortKey
  const Icon = active ? (sort.dir === "asc" ? ArrowUp : ArrowDown) : ChevronsUpDown
  return (
    <TableHead className={cn("text-xs font-medium text-muted-foreground", className)}>
      <button
        type="button"
        onClick={() => onToggle(sortKey)}
        className={cn(
          "inline-flex cursor-pointer items-center gap-1 hover:text-foreground",
          align === "right" && "w-full justify-end"
        )}
      >
        {label}
        <Icon className={cn("size-3", active ? "text-primary" : "text-muted-foreground/60")} />
      </button>
    </TableHead>
  )
}

// TrafficFilterInput is a local (non-debounced) free-text filter box.
export function TrafficFilterInput({
  value,
  onChange,
  placeholder,
  className,
}: {
  value: string
  onChange: (v: string) => void
  placeholder: string
  className?: string
}) {
  return (
    <div className={cn("relative", className)}>
      <Search className="pointer-events-none absolute top-2 left-2.5 h-4 w-4 text-muted-foreground" />
      <Input
        value={value}
        onChange={(e) => onChange(e.target.value)}
        placeholder={placeholder}
        className="h-8 pr-8 pl-8 text-xs"
      />
      {value && (
        <button
          type="button"
          onClick={() => onChange("")}
          className="absolute top-2 right-2.5 cursor-pointer text-muted-foreground hover:text-foreground"
          title="ล้างคำค้นหา"
        >
          <X className="h-4 w-4" />
        </button>
      )}
    </div>
  )
}

// useTextFilter case-insensitive substring-matches `query` across the
// supplied field accessors; an empty/whitespace query returns rows
// unchanged. No debouncing — filtering is entirely local.
export function useTextFilter<T>(rows: T[], query: string, fields: ((row: T) => string | number | undefined)[]): T[] {
  return useMemo(() => {
    const q = query.trim().toLowerCase()
    if (!q) return rows
    return rows.filter((row) =>
      fields.some((f) => {
        const v = f(row)
        return v !== undefined && String(v).toLowerCase().includes(q)
      })
    )
  }, [rows, query, fields])
}

export function TrafficEmptyState({ label }: { label: string }) {
  return <p className="py-8 text-center text-sm text-muted-foreground">{label}</p>
}

export function TruncatedWarning() {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
      <TriangleAlert className="size-4 shrink-0" />
      ข้อมูลบางส่วนอาจไม่ครบ เนื่องจากจำนวน host/conversation ในช่วงเวลานี้เกินขีดจำกัดการติดตาม
    </div>
  )
}

// AccuracyBadge — extracted verbatim from StatisticsOverview.tsx so both the
// Overview page and the two new Traffic pages share ONE definition (plan
// T-09: "extract it into TrafficStatsShared and use it from BOTH pages").
export function AccuracyBadge({ accuracy }: { accuracy?: "estimated" | "near-exact" }) {
  const isNearExact = accuracy === "near-exact"
  return (
    <Badge
      variant="outline"
      className={cn(
        "font-normal",
        isNearExact ? "border-primary/30 text-primary" : "border-muted-foreground/30 text-muted-foreground"
      )}
    >
      {isNearExact ? "ใกล้เคียงจริง" : "ประมาณการ"}
    </Badge>
  )
}

// HostBar — the thin percent bar under each Top Hosts row, extracted
// verbatim from StatisticsOverview.tsx for reuse by the Traffic drill-down's
// "Top peers" summary (plan T-10).
export function HostBar({ percent }: { percent: number }) {
  return (
    <div className="h-1.5 w-full overflow-hidden rounded-full bg-muted">
      <div
        className="h-full rounded-full bg-primary transition-all duration-500"
        style={{ width: `${Math.min(100, Math.max(percent, 0))}%` }}
      />
    </div>
  )
}
