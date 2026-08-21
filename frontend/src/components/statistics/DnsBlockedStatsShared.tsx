import { useState } from "react"
import { Badge } from "@/components/ui/badge"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  SortableHead,
  TrafficFilterInput,
  useSortableRows,
  useTextFilter,
} from "@/components/statistics/TrafficStatsShared"
import {
  type DNSBlockedDomainStat,
  type DNSBlockedClientStat,
} from "@/services/dnsStatisticsService"
import { HostNameLines } from "@/components/statistics/HostCells"
import { ReferenceHoverProvider } from "@/components/reference/ReferenceHoverProvider"
import { ReferenceTrigger } from "@/components/reference/ReferenceTrigger"
import { IpReferenceContent } from "@/components/reference/IpReferenceContent"
import { DomainReferenceContent } from "@/components/reference/DomainReferenceContent"

// Shared tables for the DNS Query Statistics tab's "Blocked Query" sub-tab
// (docs/ref/todo/dns-blocked-query-statistics-plan.md T-13) — same
// sortable/filterable pattern as DnsStatsShared's DomainStatsTable/
// ClientStatsTable, but deliberately a SEPARATE, smaller pair of components
// rather than reusing those (the row shape has no bytes/queryType/ipCount
// columns — a blocked domain carries essentially no real traffic volume
// worth showing, and mixing the two row shapes into one generic table would
// make both harder to read).

// BlockedDomainStatsTable — the "Top Blocked Domains" table: Domain / Rule /
// Mode / Count / %. Row click navigates to the domain drill-down, same as
// DomainStatsTable's onRowClick.
export function BlockedDomainStatsTable({
  rows,
  emptyLabel,
  onRowClick,
}: {
  rows: DNSBlockedDomainStat[]
  emptyLabel: string
  onRowClick?: (domain: string) => void
}) {
  const [query, setQuery] = useState("")
  const filtered = useTextFilter(rows, query, [(d) => d.domain, (d) => d.rule])
  const { rows: sorted, sort, toggle } = useSortableRows(filtered, { key: "count", dir: "desc" })

  return (
    <ReferenceHoverProvider>
      <div className="space-y-3">
        <TrafficFilterInput value={query} onChange={setQuery} placeholder="ค้นหาโดเมนหรือ rule ที่ถูกบล็อก..." />
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <SortableHead<DNSBlockedDomainStat> label="Domain" sortKey="domain" sort={sort} onToggle={toggle} />
                <SortableHead<DNSBlockedDomainStat> label="Rule" sortKey="rule" sort={sort} onToggle={toggle} />
                <TableHead className="w-24 text-xs font-medium text-muted-foreground">Mode</TableHead>
                <SortableHead<DNSBlockedDomainStat> label="Clients" sortKey="clients" sort={sort} onToggle={toggle} align="right" className="w-20" />
                <SortableHead<DNSBlockedDomainStat> label="Count" sortKey="count" sort={sort} onToggle={toggle} align="right" className="w-20" />
                <SortableHead<DNSBlockedDomainStat> label="% Blocked" sortKey="percent" sort={sort} onToggle={toggle} align="right" className="w-24" />
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
                sorted.map((d) => (
                  <TableRow
                    key={d.domain}
                    onClick={onRowClick ? () => onRowClick(d.domain) : undefined}
                    className={onRowClick ? "cursor-pointer hover:bg-muted/50" : "hover:bg-transparent"}
                    title={onRowClick ? "คลิกเพื่อดูว่าเครื่องไหนถามโดเมนนี้บ้าง" : undefined}
                  >
                    <TableCell className="max-w-[220px] truncate py-3 font-mono text-xs font-medium text-foreground" title={d.domain}>
                      <ReferenceTrigger content={() => <DomainReferenceContent key={d.domain} domain={d.domain} />}>
                        {d.domain}
                      </ReferenceTrigger>
                    </TableCell>
                    <TableCell className="max-w-[180px] truncate py-3 font-mono text-xs text-muted-foreground" title={d.rule}>
                      {d.rule}
                    </TableCell>
                    <TableCell className="py-3">
                      <Badge
                        variant="outline"
                        className="rounded border-warning/20 bg-warning/10 px-1.5 py-0 text-[10px] font-medium text-warning"
                      >
                        {d.mode}
                      </Badge>
                    </TableCell>
                    <TableCell className="py-3 text-right font-mono text-xs text-foreground">{d.clients}</TableCell>
                    <TableCell className="py-3 text-right font-mono text-xs text-foreground">{d.count}</TableCell>
                    <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{d.percent.toFixed(1)}%</TableCell>
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
    </ReferenceHoverProvider>
  )
}

// BlockedClientStatsTable — the "Top Blocked Clients" table: Host / Blocked
// domains / Count / %. Row click navigates to the client drill-down, same
// as ClientStatsTable's onRowClick.
export function BlockedClientStatsTable({
  rows,
  emptyLabel,
  onRowClick,
}: {
  rows: DNSBlockedClientStat[]
  emptyLabel: string
  onRowClick?: (ip: string, hostname: string) => void
}) {
  const [query, setQuery] = useState("")
  const filtered = useTextFilter(rows, query, [(c) => c.ip, (c) => c.hostname])
  const { rows: sorted, sort, toggle } = useSortableRows(filtered, { key: "count", dir: "desc" })

  return (
    <ReferenceHoverProvider>
      <div className="space-y-3">
        <TrafficFilterInput value={query} onChange={setQuery} placeholder="ค้นหา IP หรือ hostname..." />
        <div className="overflow-x-auto">
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <SortableHead<DNSBlockedClientStat> label="Host" sortKey="hostname" sort={sort} onToggle={toggle} />
                <SortableHead<DNSBlockedClientStat> label="Blocked domains" sortKey="domains" sort={sort} onToggle={toggle} align="right" className="w-32" />
                <SortableHead<DNSBlockedClientStat> label="Count" sortKey="count" sort={sort} onToggle={toggle} align="right" className="w-20" />
                <SortableHead<DNSBlockedClientStat> label="% Blocked" sortKey="percent" sort={sort} onToggle={toggle} align="right" className="w-24" />
              </TableRow>
            </TableHeader>
            <TableBody>
              {sorted.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={4} className="py-8 text-center text-xs text-muted-foreground">
                    {emptyLabel}
                  </TableCell>
                </TableRow>
              ) : (
                sorted.map((c) => (
                  <TableRow
                    key={c.ip}
                    onClick={onRowClick ? () => onRowClick(c.ip, c.hostname) : undefined}
                    className={onRowClick ? "cursor-pointer hover:bg-muted/50" : "hover:bg-transparent"}
                    title={onRowClick ? "คลิกเพื่อดูว่าเครื่องนี้ถูกบล็อกโดเมนอะไรบ้าง" : undefined}
                  >
                    <TableCell
                      className="max-w-[220px] truncate py-3 text-xs text-foreground"
                      title={`${c.hostname || c.ip}${c.hostname ? ` (${c.ip})` : ""}`}
                    >
                      {c.ip === "unknown" ? (
                        <span className="text-muted-foreground">ไม่ทราบต้นทาง</span>
                      ) : (
                        <ReferenceTrigger content={() => <IpReferenceContent key={c.ip} ip={c.ip} />}>
                          <HostNameLines host={{ ip: c.ip, hostname: c.hostname, domain: "" }} />
                        </ReferenceTrigger>
                      )}
                    </TableCell>
                    <TableCell className="py-3 text-right font-mono text-xs text-foreground">{c.domains}</TableCell>
                    <TableCell className="py-3 text-right font-mono text-xs text-foreground">{c.count}</TableCell>
                    <TableCell className="py-3 text-right font-mono text-xs text-muted-foreground">{c.percent.toFixed(1)}%</TableCell>
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
    </ReferenceHoverProvider>
  )
}
