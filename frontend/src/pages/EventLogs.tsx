import { useCallback, useEffect, useRef, useState } from "react"
import {
  ScrollText,
  Search,
  Trash2,
  RefreshCw,
  Loader2,
  ChevronLeft,
  ChevronRight,
  Info,
  TriangleAlert,
  ShieldAlert,
  OctagonAlert,
} from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
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
import { useAlert } from "@/hooks/useAlert"
import { getErrorMessage } from "@/lib/errors"
import { authService } from "@/services/authService"
import {
  logService,
  type SystemEvent,
  type EventSeverity,
} from "@/services/logService"

const PAGE_SIZE = 50
const POLL_INTERVAL = 10_000

const CATEGORY_OPTIONS = [
  { value: "all", label: "All categories" },
  { value: "auth", label: "Authentication" },
  { value: "user", label: "Users" },
  { value: "network", label: "Network" },
  { value: "firewall", label: "Firewall" },
  { value: "route", label: "Routing" },
  { value: "dhcp", label: "DHCP" },
  { value: "dns", label: "DNS" },
  { value: "qos", label: "QoS" },
  { value: "system", label: "System" },
  { value: "config", label: "Config" },
]

const SEVERITY_OPTIONS = [
  { value: "all", label: "All severities" },
  { value: "info", label: "Info" },
  { value: "warning", label: "Warning" },
  { value: "error", label: "Error" },
  { value: "critical", label: "Critical" },
]

/* Severity badges mirror the Dashboard Recent Logs badges so both log views
 * read the same. Colors go through theme variables (--warning/--destructive),
 * never raw palette classes (rules_of_work.md §1). */
const SEVERITY_STYLE: Record<EventSeverity, { badge: string; icon: typeof Info }> = {
  info: { badge: "bg-muted text-muted-foreground border-transparent", icon: Info },
  warning: { badge: "bg-warning/10 text-warning border-warning/20", icon: TriangleAlert },
  error: { badge: "bg-destructive/10 text-destructive border-destructive/20", icon: ShieldAlert },
  critical: { badge: "bg-destructive/10 text-destructive border-destructive/20", icon: OctagonAlert },
}

function SeverityBadge({ severity }: { severity: EventSeverity }) {
  const style = SEVERITY_STYLE[severity] ?? SEVERITY_STYLE.info
  const Icon = style.icon
  return (
    <Badge variant="outline" className={style.badge}>
      <Icon className="size-3" />
      {severity}
    </Badge>
  )
}

function formatEventTime(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, "0")
  return (
    `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ` +
    `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
  )
}

export default function EventLogs() {
  const { alert, confirm } = useAlert()
  const isSuperAdmin = authService.getRole() === "super_admin"

  const [events, setEvents] = useState<SystemEvent[]>([])
  const [total, setTotal] = useState(0)
  const [isLoading, setIsLoading] = useState(true)
  const [isClearing, setIsClearing] = useState(false)

  // Filters + paging
  const [category, setCategory] = useState("all")
  const [severity, setSeverity] = useState("all")
  const [search, setSearch] = useState("")
  const [debouncedSearch, setDebouncedSearch] = useState("")
  const [page, setPage] = useState(0)

  // Debounce free-text search so we don't hit the API per keystroke; a new
  // search term also jumps back to the first page.
  useEffect(() => {
    const id = setTimeout(() => {
      setDebouncedSearch(search)
      setPage(0)
    }, 400)
    return () => clearTimeout(id)
  }, [search])

  // Category/severity changes reset paging too (handlers, not an effect).
  const changeCategory = (value: string) => {
    setCategory(value)
    setPage(0)
  }
  const changeSeverity = (value: string) => {
    setSeverity(value)
    setPage(0)
  }

  const loadEvents = useCallback(
    async (showLoading: boolean) => {
      if (showLoading) setIsLoading(true)
      try {
        const data = await logService.getEvents({
          category: category === "all" ? "" : category,
          severity: severity === "all" ? "" : severity,
          q: debouncedSearch,
          limit: PAGE_SIZE,
          offset: page * PAGE_SIZE,
        })
        setEvents(data.events)
        setTotal(data.total)
      } catch (err) {
        if (showLoading) {
          await alert("ข้อผิดพลาด", "ไม่สามารถโหลด Event Logs ได้: " + getErrorMessage(err))
        }
        // Polling errors are swallowed — keep the last known page.
      } finally {
        if (showLoading) setIsLoading(false)
      }
    },
    [category, severity, debouncedSearch, page, alert]
  )

  const loadEventsRef = useRef(loadEvents)
  useEffect(() => {
    loadEventsRef.current = loadEvents
  })

  // Initial + filter-driven fetch, then background polling (usePoll pattern).
  useEffect(() => {
    loadEventsRef.current(true)
    const id = setInterval(() => loadEventsRef.current(false), POLL_INTERVAL)
    return () => clearInterval(id)
  }, [category, severity, debouncedSearch, page])

  const handleClear = async () => {
    const ok = await confirm(
      "ยืนยันการล้าง Event Logs",
      "คุณต้องการลบประวัติเหตุการณ์ทั้งหมดใช่หรือไม่? ระบบจะบันทึกไว้เสมอว่าใครเป็นผู้ล้าง log และการกระทำนี้ไม่สามารถย้อนกลับได้"
    )
    if (!ok) return
    setIsClearing(true)
    try {
      await logService.clearEvents()
      await loadEvents(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถล้าง Event Logs ได้: " + getErrorMessage(err))
    } finally {
      setIsClearing(false)
    }
  }

  const totalPages = Math.max(1, Math.ceil(total / PAGE_SIZE))
  const rangeStart = total === 0 ? 0 : page * PAGE_SIZE + 1
  const rangeEnd = Math.min(total, page * PAGE_SIZE + events.length)

  return (
    <div className="space-y-4">
      {/* Page header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <ScrollText className="size-5" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight">Event Logs</h1>
            <p className="text-xs text-muted-foreground">
              ประวัติเหตุการณ์สำคัญของระบบ (login, config changes, DHCP leases, reboot)
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => loadEvents(true)} disabled={isLoading}>
            <RefreshCw className={isLoading ? "size-4 animate-spin" : "size-4"} />
            Refresh
          </Button>
          {isSuperAdmin && (
            <Button variant="destructive" size="sm" onClick={handleClear} disabled={isClearing}>
              {isClearing ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
              Clear Logs
            </Button>
          )}
        </div>
      </div>

      <Card>
        <CardHeader className="gap-3">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {total} events
          </CardTitle>
          {/* Filter bar */}
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative w-full sm:w-64">
              <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="ค้นหาข้อความ / actor / target..."
                className="h-9 pl-8 text-xs"
              />
            </div>
            <Select value={category} onValueChange={changeCategory}>
              <SelectTrigger className="h-9 w-44 text-xs bg-background">
                <SelectValue placeholder="Category" />
              </SelectTrigger>
              <SelectContent>
                {CATEGORY_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value} className="text-xs">
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <Select value={severity} onValueChange={changeSeverity}>
              <SelectTrigger className="h-9 w-40 text-xs bg-background">
                <SelectValue placeholder="Severity" />
              </SelectTrigger>
              <SelectContent>
                {SEVERITY_OPTIONS.map((opt) => (
                  <SelectItem key={opt.value} value={opt.value} className="text-xs">
                    {opt.label}
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
          </div>
        </CardHeader>

        <CardContent>
          {isLoading ? (
            <div className="flex items-center justify-center gap-2 py-12 text-sm text-muted-foreground">
              <Loader2 className="size-4 animate-spin" />
              กำลังโหลด Event Logs...
            </div>
          ) : events.length === 0 ? (
            <div className="py-12 text-center text-sm text-muted-foreground">
              ไม่พบเหตุการณ์ที่ตรงกับเงื่อนไข
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-40">Time</TableHead>
                    <TableHead className="w-28">Severity</TableHead>
                    <TableHead className="w-28">Category</TableHead>
                    <TableHead className="w-28">Actor</TableHead>
                    <TableHead>Event</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {events.map((ev, i) => (
                    <TableRow key={ev.id > 0 ? ev.id : `pending-${i}`}>
                      <TableCell className="whitespace-nowrap font-mono text-xs text-muted-foreground">
                        {formatEventTime(ev.time)}
                      </TableCell>
                      <TableCell>
                        <SeverityBadge severity={ev.severity} />
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline" className="font-mono text-[10px] uppercase">
                          {ev.category}
                        </Badge>
                      </TableCell>
                      <TableCell className="text-xs font-medium">{ev.actor}</TableCell>
                      <TableCell className="text-xs">
                        <span className="text-foreground">{ev.message}</span>
                        <span className="ml-2 font-mono text-[10px] text-muted-foreground">
                          {ev.action}
                        </span>
                      </TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}

          {/* Pagination */}
          <div className="mt-4 flex items-center justify-between">
            <p className="text-xs text-muted-foreground">
              แสดง {rangeStart}–{rangeEnd} จาก {total} เหตุการณ์
            </p>
            <div className="flex items-center gap-2">
              <Button
                variant="outline"
                size="sm"
                onClick={() => setPage((p) => Math.max(0, p - 1))}
                disabled={page === 0 || isLoading}
              >
                <ChevronLeft className="size-4" />
                Prev
              </Button>
              <span className="text-xs text-muted-foreground">
                {page + 1} / {totalPages}
              </span>
              <Button
                variant="outline"
                size="sm"
                onClick={() => setPage((p) => Math.min(totalPages - 1, p + 1))}
                disabled={page >= totalPages - 1 || isLoading}
              >
                Next
                <ChevronRight className="size-4" />
              </Button>
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  )
}
