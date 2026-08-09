import { useCallback, useState, type ReactNode } from "react"
import { useSearchParams } from "react-router"
import { ArrowDown, ArrowUp, Info, TriangleAlert } from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Popover, PopoverContent, PopoverTrigger } from "@/components/ui/popover"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import { fmtBytes } from "@/lib/formatBytes"
import {
  SortableHead,
  TrafficFilterInput,
  useSortableRows,
  useTextFilter,
} from "@/components/statistics/TrafficStatsShared"
import {
  type DNSDomainStat,
  type DNSClientStat,
  type DNSDomainIP,
} from "@/services/dnsStatisticsService"
import { HostNameLines } from "@/components/statistics/HostCells"
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

// DnsTrafficCell — the 2-line Down/Up cell that replaces the separate
// Down / Up / Total columns in all 3 DNS tables (review fix on PR 127, see
// docs/ref/todo/statistics-dns-review-fixes-plan.md T-01/T-02/R-2): top line
// is Down (bytesDown), bottom line is Up (bytesUp) — the column itself sorts
// by the combined `bytes` value (bytesUp + bytesDown, guaranteed by the
// backend DTO contract).
export function DnsTrafficCell({ down, up }: { down: number; up: number }) {
  return (
    <div
      className="flex flex-col items-end leading-tight"
      title={`Down ${fmtBytes(down)} · Up ${fmtBytes(up)}`}
      aria-label={`Down ${fmtBytes(down)} · Up ${fmtBytes(up)}`}
    >
      <span className="flex items-center gap-1 font-mono text-[11px] text-primary">
        <ArrowDown className="size-3" />
        {fmtBytes(down)}
      </span>
      <span className="flex items-center gap-1 font-mono text-[11px] text-muted-foreground">
        <ArrowUp className="size-3" />
        {fmtBytes(up)}
      </span>
    </div>
  )
}

// DomainStatsTable — the "Top Domains" / per-client "Domains queried" table
// (docs/ref/todo/statistics-dns-page-revamp-plan.md T-09). Every column is
// sortable via useSortableRows/SortableHead and the table owns its own
// free-text filter (TrafficFilterInput), mirroring TrafficStatsShared's
// HostsTable pattern — callers keep wrapping this in their own <Card>.
// `%` (percent, query count) and `% Vol` (bytesPercent, bytes) are
// deliberately two separate, differently-labelled columns (plan §1.3) so
// they can never be mistaken for the same metric.
export function DomainStatsTable({
  rows,
  emptyLabel,
  onRowClick,
  query: controlledQuery,
  onQueryChange,
  placeholder = "ค้นหาโดเมน, ประเภท หรือใส่ IP...",
  filterDisabled = false,
  hint,
  banner,
  footerNote,
}: {
  rows: DNSDomainStat[]
  emptyLabel: string
  onRowClick?: (domain: string) => void
  // query/onQueryChange are optional (controlled) — omitted, this component
  // keeps its own uncontrolled state exactly as before (docs/ref/todo/
  // statistics-dns-ip-filter-plan.md T-08). The DNS page (T-09) controls
  // these to sync the filter box with the `?ip=` URL param.
  query?: string
  onQueryChange?: (v: string) => void
  placeholder?: string
  // filterDisabled=true skips useTextFilter (rows are already filtered
  // server-side, e.g. the IP-filter mode's GET /statistics/dns/ip result) —
  // sorting still applies.
  filterDisabled?: boolean
  // hint renders under the search box (e.g. "พิมพ์ IP ให้ครบ..." while the
  // user is mid-way through typing an IP).
  hint?: ReactNode
  // banner renders above the table (e.g. the shared-IP / index-truncated
  // warnings in IP-filter mode).
  banner?: ReactNode
  // footerNote replaces the default "แสดง N จาก M รายการ" line when given.
  footerNote?: ReactNode
}) {
  const [uncontrolledQuery, setUncontrolledQuery] = useState("")
  const query = controlledQuery ?? uncontrolledQuery
  const setQuery = onQueryChange ?? setUncontrolledQuery
  const textFiltered = useTextFilter(rows, query, [(d) => d.domain, (d) => d.queryType])
  const filtered = filterDisabled ? rows : textFiltered
  const { rows: sorted, sort, toggle } = useSortableRows(filtered, { key: "count", dir: "desc" })

  return (
    <div className="space-y-3">
      <div className="space-y-1">
        <TrafficFilterInput value={query} onChange={setQuery} placeholder={placeholder} />
        {hint}
      </div>
      {banner}
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <SortableHead<DNSDomainStat> label="Domain" sortKey="domain" sort={sort} onToggle={toggle} />
              <SortableHead<DNSDomainStat> label="Type" sortKey="queryType" sort={sort} onToggle={toggle} className="w-20" />
              <SortableHead<DNSDomainStat> label="Clients" sortKey="clients" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSDomainStat> label="IPs" sortKey="ipCount" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSDomainStat> label="Count" sortKey="count" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSDomainStat> label="% Query" sortKey="percent" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSDomainStat> label="Traffic" sortKey="bytes" sort={sort} onToggle={toggle} align="right" className="w-28" />
              <SortableHead<DNSDomainStat> label="% Vol" sortKey="bytesPercent" sort={sort} onToggle={toggle} align="right" className="w-20" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {sorted.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="py-8 text-center text-xs text-muted-foreground">
                  {emptyLabel}
                </TableCell>
              </TableRow>
            ) : (
              sorted.map((d) => (
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
                  <TableCell className="py-3 text-right font-mono text-xs text-foreground">{d.clients}</TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-foreground">
                    <span className="inline-flex items-center justify-end gap-1">
                      {d.sharedIps && (
                        <span title="มี IP ที่ใช้ร่วมกับโดเมนอื่น — ปริมาณข้อมูลอาจถูกนับซ้ำ">
                          <TriangleAlert className="size-3 text-warning" />
                        </span>
                      )}
                      {d.ipCount}
                    </span>
                  </TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-foreground">{d.count}</TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{d.percent.toFixed(1)}%</TableCell>
                  <TableCell className="py-3 text-right">
                    <DnsTrafficCell down={d.bytesDown} up={d.bytesUp} />
                  </TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{d.bytesPercent.toFixed(1)}%</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      {footerNote ?? (
        <p className="text-[11px] text-muted-foreground">
          แสดง {sorted.length} จาก {rows.length} รายการ
        </p>
      )}
    </div>
  )
}

// ClientStatsTable — the "Top Source Hosts" / per-domain "Clients" table
// (T-09). Same sortable/filterable pattern as DomainStatsTable above. The
// Host column now renders through HostNameLines, the same component the
// Statistics -> Traffic page uses (docs/ref/todo/
// statistics-dns-host-domain-label-plan.md T-07): domain wins over hostname
// when known, ip always shows on the second line — no onClick passed to it
// and no LAN/Internet Badge, since the whole row is already clickable via
// onRowClick and the badge wasn't asked for (plan §0).
export function ClientStatsTable({
  rows,
  emptyLabel,
  onRowClick,
}: {
  rows: DNSClientStat[]
  emptyLabel: string
  onRowClick?: (ip: string, hostname: string) => void
}) {
  const [query, setQuery] = useState("")
  const filtered = useTextFilter(rows, query, [(c) => c.ip, (c) => c.hostname, (c) => c.domain])
  const { rows: sorted, sort, toggle } = useSortableRows(filtered, { key: "count", dir: "desc" })

  return (
    <div className="space-y-3">
      <TrafficFilterInput value={query} onChange={setQuery} placeholder="ค้นหา IP, hostname, domain..." />
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <SortableHead<DNSClientStat> label="Host" sortKey="hostname" sort={sort} onToggle={toggle} />
              <SortableHead<DNSClientStat> label="Domains" sortKey="domains" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSClientStat> label="Count" sortKey="count" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSClientStat> label="% Query" sortKey="percent" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSClientStat> label="Traffic" sortKey="bytes" sort={sort} onToggle={toggle} align="right" className="w-28" />
              <SortableHead<DNSClientStat> label="% Vol" sortKey="bytesPercent" sort={sort} onToggle={toggle} align="right" className="w-20" />
            </TableRow>
          </TableHeader>
          <TableBody>
            {sorted.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="py-8 text-center text-xs text-muted-foreground">
                  {emptyLabel}
                </TableCell>
              </TableRow>
            ) : (
              sorted.map((c) => (
                <TableRow
                  key={c.ip}
                  onClick={onRowClick ? () => onRowClick(c.ip, c.hostname) : undefined}
                  className={onRowClick ? "cursor-pointer hover:bg-muted/50" : "hover:bg-transparent"}
                  title={onRowClick ? "คลิกเพื่อดูว่าเครื่องนี้ค้นหาโดเมนอะไรบ้าง" : undefined}
                >
                  <TableCell
                    className="max-w-[220px] truncate py-3 text-xs text-foreground"
                    title={c.domain ? `${c.domain} (${c.ip})` : `${c.hostname || c.ip}${c.hostname ? ` (${c.ip})` : ""}`}
                  >
                    {c.ip === "unknown" ? (
                      <span className="text-muted-foreground">ไม่ทราบต้นทาง</span>
                    ) : (
                      <HostNameLines host={c} />
                    )}
                  </TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-foreground">{c.domains}</TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-foreground">{c.count}</TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{c.percent.toFixed(1)}%</TableCell>
                  <TableCell className="py-3 text-right">
                    <DnsTrafficCell down={c.bytesDown} up={c.bytesUp} />
                  </TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{c.bytesPercent.toFixed(1)}%</TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      <p className="text-[11px] text-muted-foreground">
        แสดง {sorted.length} จาก {rows.length} รายการ
      </p>
    </div>
  )
}

// DomainIpTable — the domain drill-down's "Resolved IPs" table (T-09,
// plan §2.2 DNSDomainIP): one row per known IP, default-sorted by bytes
// (volume) descending — this is the table that answers "which IP of this
// domain used the most data". Every column is sortable; a "shared" badge
// flags an IP referenced by more than one domain (bytes double-counted
// across domains, plan §1.1 item 1).
export function DomainIpTable({
  rows,
  emptyLabel,
  onRowClick,
}: {
  rows: DNSDomainIP[]
  emptyLabel: string
  // onRowClick (docs/ref/todo/statistics-dns-ip-filter-plan.md T-10) opens
  // the Statistics -> DNS page's IP-filter mode for this row's IP — answers
  // "is this IP shared with another domain?" in one click, same pattern as
  // DomainStatsTable/ClientStatsTable's onRowClick above.
  onRowClick?: (ip: string) => void
}) {
  const [query, setQuery] = useState("")
  const filtered = useTextFilter(rows, query, [(r) => r.ip])
  const { rows: sorted, sort, toggle } = useSortableRows(filtered, { key: "bytes", dir: "desc" })

  return (
    <div className="space-y-3">
      <TrafficFilterInput value={query} onChange={setQuery} placeholder="ค้นหา IP..." />
      <div className="overflow-x-auto">
        <Table>
          <TableHeader>
            <TableRow className="hover:bg-transparent">
              <SortableHead<DNSDomainIP> label="IP" sortKey="ip" sort={sort} onToggle={toggle} />
              <SortableHead<DNSDomainIP> label="Traffic" sortKey="bytes" sort={sort} onToggle={toggle} align="right" className="w-28" />
              <SortableHead<DNSDomainIP> label="% Vol" sortKey="bytesPercent" sort={sort} onToggle={toggle} align="right" className="w-20" />
              <SortableHead<DNSDomainIP> label="Last seen" sortKey="lastSeen" sort={sort} onToggle={toggle} className="w-40" />
              <TableHead className="w-20 text-right text-xs font-medium text-muted-foreground">Shared</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {sorted.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="py-8 text-center text-xs text-muted-foreground">
                  {emptyLabel}
                </TableCell>
              </TableRow>
            ) : (
              sorted.map((r) => (
                <TableRow
                  key={r.ip}
                  onClick={onRowClick ? () => onRowClick(r.ip) : undefined}
                  className={onRowClick ? "cursor-pointer hover:bg-muted/50" : "hover:bg-transparent"}
                  title={onRowClick ? "คลิกเพื่อดูว่ามีโดเมนอื่นใช้ IP นี้อีกไหม" : undefined}
                >
                  <TableCell className="py-3 font-mono text-xs font-medium text-foreground">{r.ip}</TableCell>
                  <TableCell className="py-3 text-right">
                    <DnsTrafficCell down={r.bytesDown} up={r.bytesUp} />
                  </TableCell>
                  <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{r.bytesPercent.toFixed(1)}%</TableCell>
                  <TableCell className="py-3 font-mono text-[11px] text-muted-foreground">
                    {new Date(r.lastSeen).toLocaleString("th-TH", { dateStyle: "short", timeStyle: "short" })}
                  </TableCell>
                  <TableCell className="py-3 text-right">
                    {r.shared && (
                      <Badge variant="outline" className="rounded border-warning/20 bg-warning/10 px-1.5 py-0 text-[10px] font-medium text-warning">
                        shared
                      </Badge>
                    )}
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </div>
      <p className="text-[11px] text-muted-foreground">
        แสดง {sorted.length} จาก {rows.length} รายการ
      </p>
    </div>
  )
}

// DnsVolumeInfoButton — icon-only Popover (mirrors TrafficStatsShared's
// AccuracyInfoButton) explaining the domain<->IP volume approximation's
// three caveats from plan §1.1: shared IPs get double-counted, non-DNS
// traffic isn't counted, and the mapping has a TTL and can go stale.
export function DnsVolumeInfoButton() {
  return (
    <Popover>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          size="sm"
          className="cursor-pointer"
          aria-label="รายละเอียดความแม่นยำของปริมาณข้อมูล DNS"
        >
          <Info className="size-4" />
        </Button>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80 space-y-2 text-xs">
        <p className="font-medium text-foreground">ปริมาณข้อมูล (Volume) เป็นค่าประมาณ</p>
        <p className="text-muted-foreground">
          ระบบไม่ได้นับ byte แยกตามชื่อโดเมนโดยตรง — ตัวเลขคือผลรวม byte ของ IP ที่โดเมนนั้นเคยถูก DNS
          ตอบกลับ (resolve) ในช่วงเวลานี้
        </p>
        <p className="text-muted-foreground">
          IP ที่ใช้ร่วมกันหลายโดเมน (เช่น CDN หรือ cloud load balancer) จะถูกนับให้ทุกโดเมนที่อ้างถึงมัน —
          ผลรวมของทุกแถวจึงอาจมากกว่าปริมาณข้อมูลจริง แถวที่มีป้าย "shared" คือ IP ลักษณะนี้
        </p>
        <p className="text-muted-foreground">
          ทราฟฟิกที่วิ่งตรงไปยัง IP โดยไม่ผ่านการ resolve ผ่านอุปกรณ์นี้จะไม่ถูกนับให้โดเมนใดเลย
        </p>
        <p className="text-muted-foreground">
          การจับคู่โดเมน↔IP มีอายุจำกัด (TTL) — โดเมนที่ resolve ไว้นานแล้วอาจไม่มีปริมาณข้อมูลแสดงในหน้านี้
        </p>
      </PopoverContent>
    </Popover>
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

// DnsDomainIndexTruncatedWarning is a SEPARATE warning from
// DnsStatsTruncatedWarning above (docs/ref/todo/
// statistics-dns-cap-notification-fix-plan.md §3.3/T-09): the (domain,client)
// pair ring being full and the domain->IP forward index being full are two
// unrelated conditions with two unrelated causes — before this fix they were
// OR'd into one flag and this warning's text always talked about the pair
// ring even when the real cause was the domain->IP index.
export function DnsDomainIndexTruncatedWarning() {
  return (
    <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
      <TriangleAlert className="h-4 w-4 shrink-0" />
      ดัชนีโดเมน→IP เก็บโดเมนครบขีดจำกัดแล้ว (dns-stats-max-domains) — โดเมนใหม่บางส่วนอาจไม่ถูกจับคู่กับปริมาณข้อมูล
    </div>
  )
}
