import { useCallback } from "react"
import { useSearchParams } from "react-router"
import { TriangleAlert } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { type TopDomain, type DNSClientStat } from "@/services/dnsStatisticsService"
import { STATS_WINDOWS, parseStatsWindow, type StatsWindow } from "@/lib/statsWindow"

// Shared presentation pieces for the three DNS statistics pages
// (docs/ref/todo/statistics-nav-restructure-plan.md §2.6 / T-01) — extracted
// from the former components/dns/DnsStatisticsTab.tsx so the list page
// (StatisticsDns) and the two detail pages (StatisticsDnsDomain /
// StatisticsDnsClient) stay visually identical. No data fetching, no router
// navigation here — callbacks only.

// Re-exported so every existing `import { type StatsWindow } from
// "@/components/statistics/DnsStatsShared"` keeps working — the type itself
// now lives in @/lib/statsWindow (docs/ref/todo/
// statistics-window-granularity-plan.md §2.3/T-10), the single source of
// truth shared with the service layer.
export type { StatsWindow }

// Time window lives in the URL (`?window=15m|30m|1h|3h|6h|12h|24h`, plan
// §2.2) so a row click that navigates to a drill-down page — and the Back
// link from it — always carries the same window the user was looking at.
export function useStatsWindow(): [StatsWindow, (w: StatsWindow) => void] {
  const [searchParams, setSearchParams] = useSearchParams()
  const window_ = parseStatsWindow(searchParams.get("window"))

  const setWindow = useCallback(
    (w: StatsWindow) => {
      setSearchParams(
        (prev) => {
          const next = new URLSearchParams(prev)
          next.set("window", w)
          return next
        },
        // replace: true keeps clicking through the 7 window buttons from
        // flooding browser history (plan §6 item 8) — Back must leave the
        // page entirely, not just step back one window.
        { replace: true }
      )
    },
    [setSearchParams]
  )

  return [window_, setWindow]
}

// StatsWindowTabs is the segmented control that replaces the old <Select>
// (docs/ref/todo/statistics-window-granularity-plan.md §2.4/T-10) — same
// visual pattern as the Addressing Mode control (pages/Interfaces.tsx
// ~1635-1656), sized down for 7 buttons instead of 2.
export function StatsWindowTabs({
  value,
  onChange,
}: {
  value: StatsWindow
  onChange: (w: StatsWindow) => void
}) {
  return (
    <div className="max-w-full overflow-x-auto">
      <div
        role="group"
        aria-label="ช่วงเวลา"
        className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5"
      >
        {STATS_WINDOWS.map((w) => {
          const active = w.value === value
          return (
            <button
              key={w.value}
              type="button"
              aria-pressed={active}
              onClick={() => onChange(w.value)}
              className={`cursor-pointer rounded-md px-2 py-1 text-[11px] font-medium transition ${active
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:bg-muted hover:text-foreground"
                }`}
            >
              {w.label}
            </button>
          )
        })}
      </div>
    </div>
  )
}

export function DomainStatsTable({
  rows,
  emptyLabel,
  onRowClick,
}: {
  rows: TopDomain[]
  emptyLabel: string
  onRowClick?: (domain: string) => void
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead className="text-xs font-medium text-muted-foreground">Domain</TableHead>
          <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Type</TableHead>
          <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground">Count</TableHead>
          <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground">%</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.length === 0 ? (
          <TableRow>
            <TableCell colSpan={4} className="py-8 text-center text-xs text-muted-foreground">
              {emptyLabel}
            </TableCell>
          </TableRow>
        ) : (
          rows.map((d) => (
            <TableRow
              key={d.domain}
              onClick={onRowClick ? () => onRowClick(d.domain) : undefined}
              className={onRowClick ? "cursor-pointer hover:bg-muted/50" : "hover:bg-transparent"}
              title={onRowClick ? "คลิกเพื่อดูว่าเครื่องไหนถามโดเมนนี้บ้าง" : undefined}
            >
              <TableCell className="max-w-[220px] truncate py-3 font-mono text-xs font-medium text-foreground" title={d.domain}>
                {d.domain}
              </TableCell>
              <TableCell className="py-3">
                <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-medium text-primary">
                  {d.queryType}
                </Badge>
              </TableCell>
              <TableCell className="py-3 text-right font-mono text-xs text-foreground">{d.count}</TableCell>
              <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{d.percent.toFixed(1)}%</TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}

export function ClientStatsTable({
  rows,
  emptyLabel,
  onRowClick,
}: {
  rows: DNSClientStat[]
  emptyLabel: string
  onRowClick?: (ip: string, hostname: string) => void
}) {
  return (
    <Table>
      <TableHeader>
        <TableRow className="hover:bg-transparent">
          <TableHead className="text-xs font-medium text-muted-foreground">Host</TableHead>
          <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground">Count</TableHead>
          <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground">%</TableHead>
        </TableRow>
      </TableHeader>
      <TableBody>
        {rows.length === 0 ? (
          <TableRow>
            <TableCell colSpan={3} className="py-8 text-center text-xs text-muted-foreground">
              {emptyLabel}
            </TableCell>
          </TableRow>
        ) : (
          rows.map((c) => (
            <TableRow
              key={c.ip}
              onClick={onRowClick ? () => onRowClick(c.ip, c.hostname) : undefined}
              className={onRowClick ? "cursor-pointer hover:bg-muted/50" : "hover:bg-transparent"}
              title={onRowClick ? "คลิกเพื่อดูว่าเครื่องนี้ค้นหาโดเมนอะไรบ้าง" : undefined}
            >
              <TableCell className="max-w-[220px] truncate py-3 text-xs text-foreground" title={`${c.hostname || c.ip}${c.hostname ? ` (${c.ip})` : ""}`}>
                {c.ip === "unknown" ? (
                  <span className="text-muted-foreground">ไม่ทราบต้นทาง</span>
                ) : (
                  <>
                    <span className="font-medium">{c.hostname || c.ip}</span>
                    {c.hostname && (
                      <span className="ml-1.5 font-mono text-[10px] text-muted-foreground">{c.ip}</span>
                    )}
                  </>
                )}
              </TableCell>
              <TableCell className="py-3 text-right font-mono text-xs text-foreground">{c.count}</TableCell>
              <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{c.percent.toFixed(1)}%</TableCell>
            </TableRow>
          ))
        )}
      </TableBody>
    </Table>
  )
}

export function DnsStatsPrivacyNote() {
  return (
    <p className="text-[10px] text-muted-foreground">
      ข้อมูลสถิติ DNS เก็บไว้ใน RAM เท่านั้น — restart pigate แล้วจะเริ่มนับใหม่ทั้งหมด
      และเป็นข้อมูลการใช้อินเทอร์เน็ตส่วนบุคคลของคนในบ้าน โปรดใช้ด้วยความระมัดระวัง
    </p>
  )
}

export function DnsStatsTruncatedWarning() {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
      <TriangleAlert className="h-4 w-4 shrink-0" />
      อันดับอาจไม่ครบ เนื่องจากจำนวนคู่ (โดเมน, เครื่อง) ในช่วงเวลานี้เกินขีดจำกัดการติดตาม
    </div>
  )
}
