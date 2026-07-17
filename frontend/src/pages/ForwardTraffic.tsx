import { useEffect, useState } from "react"
import {
  ArrowRightLeft,
  Search,
  Trash2,
  Pause,
  Play,
  Loader2,
  Info,
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
import { useLiveLogs } from "@/hooks/useLiveLogs"
import { getErrorMessage } from "@/lib/errors"
import { authService } from "@/services/authService"
import { type SSELogEntry } from "@/services/dashboardService"
import {
  trafficLogService,
  type TrafficLog,
} from "@/services/trafficLogService"

const FETCH_LIMIT = 200

/* Client-side mirror of the server's forward-traffic filter (handlers.go
 * HandleGetTrafficLogs): action equality + a case-insensitive substring across
 * src/dest/port/proto/inIface/outIface/reason. Kept in lockstep so a row pushed
 * over SSE is shown only when it would also pass the server filter (Caution 8). */
function matchesFilter(l: SSELogEntry, action: string, needle: string): boolean {
  if (action !== "all" && (l.action ?? "").toUpperCase() !== action.toUpperCase()) {
    return false
  }
  if (needle) {
    const hay = [l.src, l.dest, l.port, l.proto, l.inIface ?? "", l.outIface ?? "", l.reason]
      .join(" ")
      .toLowerCase()
    if (!hay.includes(needle.toLowerCase())) return false
  }
  return true
}

const ACTION_OPTIONS = [
  { value: "all", label: "All verdicts" },
  { value: "PASS", label: "PASS only" },
  { value: "DROP", label: "DROP only" },
]

/* Action badge: PASS -> primary, DROP -> destructive. Colors go through theme
 * variables only (rules_of_work.md §1) so both light and dark render correctly. */
function ActionBadge({ action }: { action: string }) {
  const isDrop = action === "DROP"
  return (
    <Badge
      variant="outline"
      className={
        isDrop
          ? "bg-destructive/10 text-destructive border-destructive/20"
          : "bg-primary/10 text-primary border-primary/20"
      }
    >
      {action}
    </Badge>
  )
}

function formatLogTime(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

export default function ForwardTraffic() {
  const { alert, confirm } = useAlert()
  const canClear = authService.getRole() === "super_admin"

  const [isClearing, setIsClearing] = useState(false)
  const [isPaused, setIsPaused] = useState(false)
  // Bumped on Clear to force an immediate snapshot refetch even while paused (the
  // clear SSE event covers the streaming case; this covers the paused one).
  const [clearNonce, setClearNonce] = useState(0)

  // Filters
  const [action, setAction] = useState("all")
  const [search, setSearch] = useState("")
  const [debouncedSearch, setDebouncedSearch] = useState("")

  // Debounce free-text search so we don't hit the API per keystroke.
  useEffect(() => {
    const id = setTimeout(() => setDebouncedSearch(search), 400)
    return () => clearTimeout(id)
  }, [search])

  // Live feed over SSE. The snapshot is server-filtered; incoming pushed rows are
  // filtered client-side by the same predicate so filtered rows never leak in.
  const { logs, isLoading } = useLiveLogs<TrafficLog>({
    fetchSnapshot: () =>
      trafficLogService.getTrafficLogs({
        action: action === "all" ? "" : action,
        q: debouncedSearch,
        limit: FETCH_LIMIT,
      }),
    refreshKey: `${action}|${debouncedSearch}|${clearNonce}`,
    paused: isPaused,
    transform: (raw) =>
      matchesFilter(raw, action, debouncedSearch)
        ? ({ ...raw, inIface: raw.inIface ?? "-", outIface: raw.outIface ?? "-" } as TrafficLog)
        : null,
  })

  const handleClear = async () => {
    const ok = await confirm(
      "ยืนยันการล้าง Forward Traffic Log",
      "คุณต้องการล้าง log ทราฟฟิกที่บันทึกอยู่ใน RAM ทั้งหมดใช่หรือไม่? (ข้อมูลนี้ไม่ได้ถูกบันทึกถาวรอยู่แล้ว)"
    )
    if (!ok) return
    setIsClearing(true)
    try {
      await trafficLogService.clearTrafficLogs()
      setClearNonce((n) => n + 1)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถล้าง log ได้: " + getErrorMessage(err))
    } finally {
      setIsClearing(false)
    }
  }

  return (
    <div className="space-y-4">
      {/* Page header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <ArrowRightLeft className="size-5" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight">Forward Traffic</h1>
            <p className="text-xs text-muted-foreground">
              เหตุการณ์ PASS/DROP ของแพ็กเก็ตที่วิ่งผ่านเครื่อง (LAN↔WAN) แบบเรียลไทม์
            </p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            onClick={() => setIsPaused((p) => !p)}
          >
            {isPaused ? <Play className="size-4" /> : <Pause className="size-4" />}
            {isPaused ? "Resume" : "Pause"}
          </Button>
          {canClear && (
            <Button variant="destructive" size="sm" onClick={handleClear} disabled={isClearing}>
              {isClearing ? <Loader2 className="size-4 animate-spin" /> : <Trash2 className="size-4" />}
              Clear
            </Button>
          )}
        </div>
      </div>

      {/* Explanatory note: this view is a recent sample, not a complete record,
          and established connections are accepted without a per-packet log. */}
      <div className="flex items-start gap-2 rounded-lg border border-primary/20 bg-primary/5 p-3 text-xs text-muted-foreground">
        <Info className="mt-0.5 size-4 shrink-0 text-primary" />
        <span>
          หน้านี้แสดง <span className="font-medium text-foreground">ตัวอย่างล่าสุด</span> ของแพ็กเก็ตที่วิ่งผ่าน
          firewall (เก็บใน RAM ไม่ใช่บันทึกครบทุกแพ็กเก็ต) — จะเห็นเฉพาะแพ็กเก็ตที่
          <span className="font-medium text-foreground"> เปิด connection ใหม่</span> บน policy ที่เปิด Log ไว้
          และแพ็กเก็ตที่ <span className="font-medium text-foreground">ถูก DROP</span>; ทราฟฟิกของ connection
          ที่เปิดค้างไว้แล้ว (established) จะไม่ปรากฏโดยตั้งใจ
        </span>
      </div>

      <Card>
        <CardHeader className="gap-3">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {logs.length} entries {isPaused && <span className="text-warning">(paused)</span>}
          </CardTitle>
          {/* Filter bar */}
          <div className="flex flex-wrap items-center gap-2">
            <div className="relative w-full sm:w-64">
              <Search className="absolute left-2.5 top-1/2 size-4 -translate-y-1/2 text-muted-foreground" />
              <Input
                value={search}
                onChange={(e) => setSearch(e.target.value)}
                placeholder="ค้นหา src / dest / port / interface / reason..."
                className="h-9 pl-8 text-xs"
              />
            </div>
            <Select value={action} onValueChange={setAction}>
              <SelectTrigger className="h-9 w-40 text-xs bg-background">
                <SelectValue placeholder="Verdict" />
              </SelectTrigger>
              <SelectContent>
                {ACTION_OPTIONS.map((opt) => (
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
              กำลังโหลด Forward Traffic Log...
            </div>
          ) : logs.length === 0 ? (
            <div className="py-12 text-center text-sm text-muted-foreground">
              ยังไม่มีเหตุการณ์ — เปิด Log บน Firewall Policy เพื่อดูทราฟฟิกที่วิ่งผ่าน
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-24">Time</TableHead>
                    <TableHead className="w-24">Action</TableHead>
                    <TableHead>Src</TableHead>
                    <TableHead>Dest</TableHead>
                    <TableHead className="w-20">Port</TableHead>
                    <TableHead className="w-20">Proto</TableHead>
                    <TableHead className="w-32">Interface</TableHead>
                    <TableHead>Reason</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {logs.map((l) => (
                    <TableRow key={l.id}>
                      <TableCell className="whitespace-nowrap font-mono text-xs text-muted-foreground">
                        {formatLogTime(l.time)}
                      </TableCell>
                      <TableCell>
                        <ActionBadge action={l.action} />
                      </TableCell>
                      <TableCell className="font-mono text-xs">{l.src}</TableCell>
                      <TableCell className="font-mono text-xs">{l.dest}</TableCell>
                      <TableCell className="font-mono text-xs">{l.port}</TableCell>
                      <TableCell className="text-xs">{l.proto}</TableCell>
                      <TableCell className="whitespace-nowrap font-mono text-xs">
                        {l.inIface}
                        <span className="mx-1 text-muted-foreground">→</span>
                        {l.outIface}
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">{l.reason}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
