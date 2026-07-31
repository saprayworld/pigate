import { useCallback, useEffect, useRef, useState } from "react"
import { BarChart3, Loader2, RefreshCw, Settings2, TriangleAlert } from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import {
  dnsStatisticsService,
  type DNSQueryStatistics,
  type DNSDomainDrilldown,
  type DNSClientDrilldown,
} from "@/services/dnsStatisticsService"
import { getErrorMessage } from "@/lib/errors"

// Drill-down target (T-08): which row was clicked and its key. Kept as a
// single nullable state (per plan §"T-08") so `<Dialog open={!!drilldown}>`
// derives open/closed directly from it — closing the dialog (backdrop/ESC/X)
// clears the state via onOpenChange, which also drops drilldownData below.
type Drilldown =
  | { kind: "domain"; key: string }
  | { kind: "client"; key: string; hostname: string }

// DNS Query Statistics tab (docs/ref/todo/dns-query-statistics-drilldown-plan.md
// T-07): two ranked tables — Top Domains / Top Source Hosts — for the DNS
// Server page's "สถิติ" tab. Kept as its own component (rather than inline in
// DnsServer.tsx, already ~1,780 lines) per plan §5 item 10.
//
// Auto-refresh never fires faster than every 10 seconds (plan §5 item 9), and
// this component never fetches unless `active` is true — the parent controls
// that via the now-controlled <Tabs> in DnsServer.tsx so switching away from
// this tab (and never opening it) never calls the API.
const REFRESH_INTERVAL_MS = 10_000

interface DnsStatisticsTabProps {
  // True only while the "สถิติ" tab is the active tab — gates both the
  // initial fetch and the auto-refresh interval (lazy load guard).
  active: boolean
  // Lets the empty state (queryLogging disabled) send the user to the
  // Settings tab where the switch lives.
  onGoToSettings?: () => void
}

export function DnsStatisticsTab({ active, onGoToSettings }: DnsStatisticsTabProps) {
  const [window_, setWindow] = useState<"1h" | "24h">("1h")
  const [stats, setStats] = useState<DNSQueryStatistics | null>(null)
  const [isLoading, setIsLoading] = useState(false)
  const [error, setError] = useState<string | null>(null)
  const hasLoadedRef = useRef(false)

  // Drill-down dialog (T-08). Kept inside this component (rather than lifted
  // to DnsServer.tsx) so it naturally shares `window_` above: switching
  // 1h/24h while the dialog is open refetches against the same window the
  // two tables are showing, with no separate state to keep in sync.
  const [drilldown, setDrilldown] = useState<Drilldown | null>(null)
  const [drilldownData, setDrilldownData] = useState<DNSDomainDrilldown | DNSClientDrilldown | null>(null)
  const [isDrilldownLoading, setIsDrilldownLoading] = useState(false)
  const [drilldownError, setDrilldownError] = useState<string | null>(null)

  const load = useCallback(async (win: "1h" | "24h", showLoading: boolean) => {
    if (showLoading) setIsLoading(true)
    try {
      const data = await dnsStatisticsService.getDNSStatistics(win)
      setStats(data)
      setError(null)
    } catch (err) {
      if (showLoading) {
        setError(getErrorMessage(err))
      }
      // Background refresh errors are swallowed — keep showing the last
      // known snapshot rather than flashing an error on every failed poll.
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }, [])

  const loadRef = useRef(load)
  useEffect(() => {
    loadRef.current = load
  })

  // Lazy load: only fetch (and only start polling) once this tab has
  // actually been activated at least once.
  useEffect(() => {
    if (!active) return
    hasLoadedRef.current = true
    loadRef.current(window_, true)
    const id = setInterval(() => loadRef.current(window_, false), REFRESH_INTERVAL_MS)
    return () => clearInterval(id)
  }, [active, window_])

  // Fetch drill-down data only while the dialog is actually open, and again
  // whenever `window_` changes while it's open (plan §T-08: "เปลี่ยน window
  // ขณะ dialog เปิดอยู่ต้อง refetch"). `ignore` guards against a stale
  // response landing after the user already switched target/closed dialog.
  const loadDrilldown = useCallback(async (target: Drilldown, win: "1h" | "24h", isStale: () => boolean) => {
    setIsDrilldownLoading(true)
    setDrilldownError(null)
    try {
      const data = target.kind === "domain"
        ? await dnsStatisticsService.getDomainClients(target.key, win)
        : await dnsStatisticsService.getClientDomains(target.key, win)
      if (isStale()) return
      setDrilldownData(data)
    } catch (err) {
      if (isStale()) return
      setDrilldownError(getErrorMessage(err))
    } finally {
      if (!isStale()) setIsDrilldownLoading(false)
    }
  }, [])

  const loadDrilldownRef = useRef(loadDrilldown)
  useEffect(() => {
    loadDrilldownRef.current = loadDrilldown
  })

  useEffect(() => {
    if (!drilldown) return
    let ignore = false
    loadDrilldownRef.current(drilldown, window_, () => ignore)
    return () => {
      ignore = true
    }
  }, [drilldown, window_])

  // Reset any previously-fetched data explicitly (not from inside the effect
  // above, to avoid a synchronous setState-in-effect) so closing the dialog,
  // or opening a different row, never briefly flashes the previous row's data.
  const openDrilldown = (target: Drilldown) => {
    setDrilldownData(null)
    setDrilldownError(null)
    setDrilldown(target)
  }
  const closeDrilldown = () => {
    setDrilldown(null)
    setDrilldownData(null)
    setDrilldownError(null)
  }

  const generatedAtLabel = stats?.generatedAt
    ? new Date(stats.generatedAt).toLocaleTimeString("th-TH", { hour12: false })
    : "-"

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-2 text-xs text-muted-foreground">
          <BarChart3 className="h-4 w-4" />
          อัปเดตล่าสุด {generatedAtLabel}
        </div>
        <div className="flex items-center gap-2">
          <Select value={window_} onValueChange={(v) => setWindow(v as "1h" | "24h")}>
            <SelectTrigger className="h-9 w-28 text-xs bg-background">
              <SelectValue />
            </SelectTrigger>
            <SelectContent>
              <SelectItem value="1h" className="text-xs">1 ชั่วโมง</SelectItem>
              <SelectItem value="24h" className="text-xs">24 ชั่วโมง</SelectItem>
            </SelectContent>
          </Select>
          <Button
            variant="outline"
            size="sm"
            onClick={() => load(window_, true)}
            disabled={isLoading}
            className="cursor-pointer gap-1.5"
          >
            <RefreshCw className={`h-4 w-4 ${isLoading ? "animate-spin" : ""}`} />
            Refresh
          </Button>
        </div>
      </div>

      {stats?.truncated && (
        <div className="flex items-center gap-2 rounded-lg border border-warning/20 bg-warning/10 px-3 py-2 text-xs text-warning">
          <TriangleAlert className="h-4 w-4 shrink-0" />
          อันดับอาจไม่ครบ เนื่องจากจำนวนคู่ (โดเมน, เครื่อง) ในช่วงเวลานี้เกินขีดจำกัดการติดตาม
        </div>
      )}

      {error && !stats && (
        <Card>
          <CardContent className="py-8 text-center text-sm text-destructive">{error}</CardContent>
        </Card>
      )}

      {isLoading && !stats ? (
        <div className="flex items-center justify-center py-12">
          <Loader2 className="h-6 w-6 animate-spin text-primary" />
        </div>
      ) : stats && !stats.enabled ? (
        <Card>
          <CardContent className="flex flex-col items-center justify-center gap-3 py-10 text-center">
            <BarChart3 className="h-8 w-8 text-muted-foreground/50" />
            <p className="text-sm font-medium text-foreground">ยังไม่ได้เปิดการเก็บสถิติ DNS</p>
            <p className="max-w-md text-xs text-muted-foreground">
              เปิดสวิตช์ "เก็บสถิติ DNS query" ในแท็บ Settings เพื่อดูโดเมนและเครื่องที่ค้นหาในหน้านี้ —
              ข้อมูลนี้เก็บไว้ใน RAM เท่านั้น (restart แล้วเริ่มนับใหม่) และเป็นข้อมูลส่วนบุคคลของคนในบ้าน
            </p>
            {onGoToSettings && (
              <Button size="sm" variant="outline" onClick={onGoToSettings} className="cursor-pointer gap-1.5">
                <Settings2 className="h-4 w-4" />
                ไปที่แท็บ Settings
              </Button>
            )}
          </CardContent>
        </Card>
      ) : (
        <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
          <Card>
            <CardHeader className="space-y-0">
              <CardTitle className="text-base font-semibold">Top Domains</CardTitle>
            </CardHeader>
            <CardContent>
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
                  {!stats || stats.topDomains.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={4} className="py-8 text-center text-xs text-muted-foreground">
                        ยังไม่มีข้อมูลโดเมนในช่วงเวลานี้
                      </TableCell>
                    </TableRow>
                  ) : (
                    stats.topDomains.map((d) => (
                      <TableRow
                        key={d.domain}
                        onClick={() => openDrilldown({ kind: "domain", key: d.domain })}
                        className="cursor-pointer hover:bg-muted/50"
                        title="คลิกเพื่อดูว่าเครื่องไหนถามโดเมนนี้บ้าง"
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
            </CardContent>
          </Card>

          <Card>
            <CardHeader className="space-y-0">
              <CardTitle className="text-base font-semibold">Top Source Hosts</CardTitle>
            </CardHeader>
            <CardContent>
              <Table>
                <TableHeader>
                  <TableRow className="hover:bg-transparent">
                    <TableHead className="text-xs font-medium text-muted-foreground">Host</TableHead>
                    <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground">Count</TableHead>
                    <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground">%</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {!stats || stats.topClients.length === 0 ? (
                    <TableRow>
                      <TableCell colSpan={3} className="py-8 text-center text-xs text-muted-foreground">
                        ยังไม่มีข้อมูลเครื่องต้นทางในช่วงเวลานี้
                      </TableCell>
                    </TableRow>
                  ) : (
                    stats.topClients.map((c) => (
                      <TableRow
                        key={c.ip}
                        onClick={() => openDrilldown({ kind: "client", key: c.ip, hostname: c.hostname })}
                        className="cursor-pointer hover:bg-muted/50"
                        title="คลิกเพื่อดูว่าเครื่องนี้ค้นหาโดเมนอะไรบ้าง"
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
            </CardContent>
          </Card>
        </div>
      )}

      <p className="text-[10px] text-muted-foreground">
        ข้อมูลสถิติ DNS เก็บไว้ใน RAM เท่านั้น — restart pigate แล้วจะเริ่มนับใหม่ทั้งหมด
        และเป็นข้อมูลการใช้อินเทอร์เน็ตส่วนบุคคลของคนในบ้าน โปรดใช้ด้วยความระมัดระวัง
      </p>

      {/* Drill-down dialog (T-08) — plain modal Dialog (no Combobox inside,
          so `modal={false}` must NOT be set per docs/rules_of_work.md). Data
          is fetched lazily by the effect above, only while `drilldown` is set. */}
      <Dialog open={!!drilldown} onOpenChange={(open) => { if (!open) closeDrilldown() }}>
        <DialogContent className="sm:max-w-lg">
          <DialogHeader>
            <DialogTitle className="text-sm font-semibold">
              {drilldown?.kind === "domain" ? (
                <>Source Hosts ที่ค้นหา <span className="font-mono">{drilldown.key}</span></>
              ) : drilldown?.kind === "client" ? (
                <>
                  โดเมนที่{" "}
                  {drilldown.key === "unknown"
                    ? "ไม่ทราบต้นทาง"
                    : `${drilldown.hostname ? `${drilldown.hostname} ` : ""}(${drilldown.key})`}{" "}
                  ค้นหา
                </>
              ) : null}
            </DialogTitle>
          </DialogHeader>

          {isDrilldownLoading && !drilldownData ? (
            <div className="flex items-center justify-center py-10">
              <Loader2 className="h-6 w-6 animate-spin text-primary" />
            </div>
          ) : drilldownError ? (
            <p className="py-6 text-center text-sm text-destructive">{drilldownError}</p>
          ) : drilldown?.kind === "domain" ? (
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs font-medium text-muted-foreground">Host</TableHead>
                  <TableHead className="w-[20%] text-right text-xs font-medium text-muted-foreground">Count</TableHead>
                  <TableHead className="w-[20%] text-right text-xs font-medium text-muted-foreground">%</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {!drilldownData || (drilldownData as DNSDomainDrilldown).clients.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={3} className="py-8 text-center text-xs text-muted-foreground">
                      ไม่พบเครื่องที่ค้นหาโดเมนนี้ในช่วงเวลานี้
                    </TableCell>
                  </TableRow>
                ) : (
                  (drilldownData as DNSDomainDrilldown).clients.map((c) => (
                    <TableRow key={c.ip} className="hover:bg-transparent">
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
          ) : drilldown?.kind === "client" ? (
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
                {!drilldownData || (drilldownData as DNSClientDrilldown).domains.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="py-8 text-center text-xs text-muted-foreground">
                      ไม่พบโดเมนที่เครื่องนี้ค้นหาในช่วงเวลานี้
                    </TableCell>
                  </TableRow>
                ) : (
                  (drilldownData as DNSClientDrilldown).domains.map((d) => (
                    <TableRow key={d.domain} className="hover:bg-transparent">
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
          ) : null}
        </DialogContent>
      </Dialog>
    </div>
  )
}
