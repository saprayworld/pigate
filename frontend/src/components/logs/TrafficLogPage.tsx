import { useEffect, useRef, useState, type ComponentType, type ReactNode } from "react"
import { Search, Trash2, Pause, Play, Loader2, Info } from "lucide-react"

import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { CapabilityBanner } from "@/components/CapabilityBanner"
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
import { usePaginatedLiveLogs } from "@/hooks/usePaginatedLiveLogs"
import { getErrorMessage } from "@/lib/errors"
import { authService } from "@/services/authService"
import { type SSELogEntry } from "@/services/dashboardService"
import {
  trafficLogService,
  type TrafficChainFilter,
  type TrafficLog,
  type TrafficLogBufferUsage,
} from "@/services/trafficLogService"

const PAGE_SIZE = 500
const MAX_ROWS = 5000

// How often the buffer usage summary bar polls GET /api/logs/traffic/usage
// (docs/ref/todo/firewall-log-buffer-capacity-plan.md T-09, issue #134) —
// same cadence as the Statistics -> Capacity page's own polling
// (StatisticsCapacity.tsx REFRESH_INTERVAL_MS).
const USAGE_POLL_INTERVAL_MS = 10_000

const ACTION_OPTIONS = [
  { value: "all", label: "All verdicts" },
  { value: "PASS", label: "PASS only" },
  { value: "DROP", label: "DROP only" },
]

/* Client-side mirror of the server's traffic-log filter (handlers.go
 * HandleGetTrafficLogs): chain equality/group + action equality + a
 * case-insensitive substring across src/dest/srcPort/port/proto/inIface/
 * outIface/reason/chain. Kept in lockstep so a row pushed over SSE is shown only when
 * it would also pass the server filter — including chain, otherwise input/
 * output entries would leak into the Forward Traffic page via the live
 * stream and vice versa (plan §6 Caution 6). */
function matchesFilter(
  l: SSELogEntry,
  action: string,
  needle: string,
  chainParam: TrafficChainFilter
): boolean {
  if (action !== "all" && (l.action ?? "").toUpperCase() !== action.toUpperCase()) {
    return false
  }
  const entryChain = l.chain ?? ""
  if (chainParam) {
    const isLocal = entryChain === "input" || entryChain === "output"
    if (chainParam === "local" ? !isLocal : entryChain !== chainParam) {
      return false
    }
  }
  if (needle) {
    const hay = [l.src, l.dest, l.srcPort, l.port, l.proto, l.inIface ?? "", l.outIface ?? "", l.reason, entryChain]
      .join(" ")
      .toLowerCase()
    if (!hay.includes(needle.toLowerCase())) return false
  }
  return true
}

/* Verdict badge: PASS -> primary, DROP -> destructive. Colors go through
 * theme variables only (rules_of_work.md §1) so both light and dark render
 * correctly. */
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

/* Chain badge: FWD/INP/OUT, one style per chain via theme variables only
 * (no raw Tailwind color classes, no shadow/backdrop-blur — rules_of_work.md). */
const CHAIN_LABEL: Record<string, string> = { forward: "FWD", input: "INP", output: "OUT" }
const CHAIN_STYLE: Record<string, string> = {
  forward: "bg-primary/10 text-primary border-primary/20",
  input: "bg-accent text-accent-foreground border-transparent",
  output: "bg-muted text-muted-foreground border-transparent",
}
function ChainBadge({ chain }: { chain: string }) {
  return (
    <Badge variant="outline" className={CHAIN_STYLE[chain] ?? "bg-muted text-muted-foreground border-transparent"}>
      {CHAIN_LABEL[chain] ?? chain.toUpperCase() ?? "-"}
    </Badge>
  )
}

/* Rule column: shows the resolved rule name (snapshot-on-write, see
 * model.FirewallLog doc comment) when available; if the log carries a
 * ruleId but the name couldn't be resolved (e.g. the rule was deleted
 * after this entry was logged), show the raw id muted so the row still
 * carries *some* identifying detail; otherwise a plain "-". */
function RuleCell({ ruleId, ruleName }: { ruleId?: string; ruleName?: string }) {
  if (ruleName) {
    return <span className="text-xs">{ruleName}</span>
  }
  if (ruleId) {
    return <span className="font-mono text-xs text-muted-foreground">{ruleId}</span>
  }
  return <span className="text-xs text-muted-foreground">-</span>
}

/* IP cell: the address itself, plus an optional muted subtext line showing
 * the domain (from the DNS query cache) or, failing that, a DHCP hostname
 * — both resolved fresh on every fetch (enrich-on-read), never cached
 * client-side. */
function IpCell({ ip, domain, hostname }: { ip: string; domain?: string; hostname?: string }) {
  const sub = domain || hostname
  return (
    <div className="font-mono text-xs">
      <div>{ip}</div>
      {sub && <div className="font-sans text-[10px] text-muted-foreground">{sub}</div>}
    </div>
  )
}

function formatLogTime(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, "0")
  return `${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
}

// Formats the buffer usage bar's "oldest entry" timestamp as a local
// date+time string; falls back to the raw as-stored string if it fails to
// parse (backend's Time field may be RFC3339 or RFC3339Nano — design
// decision 5, docs/ref/todo/firewall-log-buffer-capacity-plan.md).
function formatOldestEntryTime(iso: string): string {
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  return d.toLocaleString()
}

export interface TrafficLogPageProps {
  /** Page title shown in the header (e.g. "Forward Traffic"). */
  title: string
  /** Short one-line description under the title. */
  description: string
  /** Header icon component (lucide-react). */
  icon: ComponentType<{ className?: string }>
  /** Explanatory note content rendered in the info callout above the table. */
  noteContent: ReactNode
  /** Confirmation dialog copy for the Clear button. */
  clearConfirmTitle: string
  clearConfirmMessage: string
  /** Which chain(s) this page shows. "forward" for Forward Traffic; "local"
   *  (input+output) as the default filter for Local Traffic — extraFilter
   *  below lets Local Traffic narrow further to input-only/output-only. */
  chainParam: TrafficChainFilter
  /** Extra dropdown rendered before the verdict filter (e.g. Local Traffic's
   *  All local / Local-In only / Local-Out only selector). Selecting a
   *  different chain here overrides chainParam via onChainParamChange. */
  extraFilter?: {
    value: TrafficChainFilter
    onChange: (value: TrafficChainFilter) => void
    options: Array<{ value: TrafficChainFilter; label: string }>
  }
}

export function TrafficLogPage({
  title,
  description,
  icon: Icon,
  noteContent,
  clearConfirmTitle,
  clearConfirmMessage,
  chainParam,
  extraFilter,
}: TrafficLogPageProps) {
  const { alert, confirm } = useAlert()
  const canClear = authService.getRole() === "super_admin"

  const [isClearing, setIsClearing] = useState(false)
  const [isPaused, setIsPaused] = useState(false)
  // Bumped on Clear to force an immediate first-page refetch even while paused.
  const [clearNonce, setClearNonce] = useState(0)

  // Filters
  const [action, setAction] = useState("all")
  const [search, setSearch] = useState("")
  const [debouncedSearch, setDebouncedSearch] = useState("")

  useEffect(() => {
    const id = setTimeout(() => setDebouncedSearch(search), 400)
    return () => clearTimeout(id)
  }, [search])

  const effectiveChain = extraFilter ? extraFilter.value : chainParam

  const { logs, isLoading, isLoadingMore, hasMore, loadMore } = usePaginatedLiveLogs<TrafficLog>({
    fetchPage: (cursor) =>
      trafficLogService.getTrafficLogs({
        action: action === "all" ? "" : action,
        q: debouncedSearch,
        chain: effectiveChain,
        limit: PAGE_SIZE,
        beforeId: cursor?.beforeId,
        beforeTime: cursor?.beforeTime,
      }),
    // Any filter change (verdict/search/chain) or Clear must reset pagination
    // entirely and refetch the first page (plan §2.7 "เปลี่ยนฟิลเตอร์ = reset ทุกอย่าง").
    refreshKey: `${action}|${debouncedSearch}|${effectiveChain}|${clearNonce}`,
    paused: isPaused,
    transform: (raw) =>
      matchesFilter(raw, action, debouncedSearch, effectiveChain)
        ? ({ ...raw, inIface: raw.inIface ?? "-", outIface: raw.outIface ?? "-", chain: (raw.chain ?? "") as TrafficLog["chain"] } as TrafficLog)
        : null,
    pageSize: PAGE_SIZE,
    maxRows: MAX_ROWS,
  })

  // Infinite scroll: an IntersectionObserver on a sentinel div past the last
  // row triggers loadMore(); a "Load more" button is kept as a fallback for
  // browsers/layouts where the observer doesn't fire.
  const sentinelRef = useRef<HTMLDivElement | null>(null)
  useEffect(() => {
    const el = sentinelRef.current
    if (!el || !hasMore) return
    const observer = new IntersectionObserver(
      (entries) => {
        if (entries[0]?.isIntersecting) loadMore()
      },
      { rootMargin: "200px" }
    )
    observer.observe(el)
    return () => observer.disconnect()
  }, [hasMore, loadMore, logs.length])

  // Buffer usage summary bar (docs/ref/todo/
  // firewall-log-buffer-capacity-plan.md T-09, issue #134) — a small,
  // separate poll from GET /api/logs/traffic/usage, independent of the
  // page's own log rows/pagination. Numbers are for the WHOLE shared ring
  // buffer (all three chains), not just this page's filtered rows — the
  // label below says so explicitly to avoid the user misreading it as a
  // per-page count.
  const [usage, setUsage] = useState<TrafficLogBufferUsage | null>(null)
  useEffect(() => {
    if (isPaused) return
    let cancelled = false
    const load = () => {
      trafficLogService
        .getTrafficLogUsage()
        .then((u) => {
          if (!cancelled) setUsage(u)
        })
        .catch(() => {
          /* transient failure: keep showing the last known usage */
        })
    }
    load()
    const id = setInterval(load, USAGE_POLL_INTERVAL_MS)
    return () => {
      cancelled = true
      clearInterval(id)
    }
  }, [isPaused, clearNonce])

  const handleClear = async () => {
    const ok = await confirm(clearConfirmTitle, clearConfirmMessage)
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
      <CapabilityBanner id="firewall" />

      {/* Page header */}
      <div className="flex flex-wrap items-center justify-between gap-3">
        <div className="flex items-center gap-3">
          <div className="flex size-10 items-center justify-center rounded-lg bg-primary/10 text-primary">
            <Icon className="size-5" />
          </div>
          <div>
            <h1 className="text-lg font-bold tracking-tight">{title}</h1>
            <p className="text-xs text-muted-foreground">{description}</p>
          </div>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" size="sm" onClick={() => setIsPaused((p) => !p)}>
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

      {/* Explanatory note */}
      <div className="flex items-start gap-2 rounded-lg border border-primary/20 bg-primary/5 p-3 text-xs text-muted-foreground">
        <Info className="mt-0.5 size-4 shrink-0 text-primary" />
        <span>{noteContent}</span>
      </div>

      <Card>
        <CardHeader className="gap-3">
          <CardTitle className="text-sm font-medium text-muted-foreground">
            {logs.length} entries {isPaused && <span className="text-warning">(paused)</span>}
          </CardTitle>
          {/* Buffer usage summary — the WHOLE shared ring buffer's fill
             state (all chains combined), not just this page's rows above.
             capacity always comes from the API response, never hardcoded. */}
          {usage && (
            <p className="text-xs text-muted-foreground">
              ใช้ไป {usage.used.toLocaleString()} / {usage.capacity.toLocaleString()} รายการ (
              {usage.usedPercent.toFixed(1)}%) ของบัฟเฟอร์ log ทั้งหมด (รวมทุก chain)
              {usage.oldestEntry && (
                <>
                  {" "}
                  · log เก่าสุดที่ยังอยู่ในระบบ: {formatOldestEntryTime(usage.oldestEntry)}
                </>
              )}
            </p>
          )}
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
            {extraFilter && (
              <Select value={extraFilter.value} onValueChange={(v) => extraFilter.onChange(v as TrafficChainFilter)}>
                <SelectTrigger className="h-9 w-44 text-xs bg-background">
                  <SelectValue placeholder="Chain" />
                </SelectTrigger>
                <SelectContent>
                  {extraFilter.options.map((opt) => (
                    <SelectItem key={opt.value} value={opt.value} className="text-xs">
                      {opt.label}
                    </SelectItem>
                  ))}
                </SelectContent>
              </Select>
            )}
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
              กำลังโหลด...
            </div>
          ) : logs.length === 0 ? (
            <div className="py-12 text-center text-sm text-muted-foreground">
              ยังไม่มีเหตุการณ์ — เปิด Log บน Firewall Policy เพื่อดูทราฟฟิก
            </div>
          ) : (
            <div className="overflow-x-auto">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead className="w-24">Time</TableHead>
                    <TableHead className="w-16">Chain</TableHead>
                    <TableHead className="w-24">Action</TableHead>
                    <TableHead>Src</TableHead>
                    <TableHead>Dest</TableHead>
                    <TableHead className="w-28">Port</TableHead>
                    <TableHead className="w-20">Proto</TableHead>
                    <TableHead className="w-32">Interface</TableHead>
                    <TableHead className="w-40">Rule</TableHead>
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
                        <ChainBadge chain={l.chain} />
                      </TableCell>
                      <TableCell>
                        <ActionBadge action={l.action} />
                      </TableCell>
                      <TableCell>
                        <IpCell ip={l.src} domain={l.srcDomain} hostname={l.srcHostname} />
                      </TableCell>
                      <TableCell>
                        <IpCell ip={l.dest} domain={l.destDomain} hostname={l.destHostname} />
                      </TableCell>
                      <TableCell className="whitespace-nowrap font-mono text-xs">
                        {l.srcPort}
                        <span className="mx-1 text-muted-foreground">→</span>
                        {l.port}
                      </TableCell>
                      <TableCell className="text-xs">{l.proto}</TableCell>
                      <TableCell className="whitespace-nowrap font-mono text-xs">
                        {l.inIface}
                        <span className="mx-1 text-muted-foreground">→</span>
                        {l.outIface}
                      </TableCell>
                      <TableCell>
                        <RuleCell ruleId={l.ruleId} ruleName={l.ruleName} />
                      </TableCell>
                      <TableCell className="text-xs text-muted-foreground">{l.reason}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>

              {/* Infinite-scroll sentinel + fallback button + end-of-data status */}
              <div ref={sentinelRef} className="flex flex-col items-center gap-2 py-4">
                {isLoadingMore ? (
                  <div className="flex items-center gap-2 text-xs text-muted-foreground">
                    <Loader2 className="size-3.5 animate-spin" />
                    กำลังโหลดเพิ่ม...
                  </div>
                ) : hasMore ? (
                  <Button variant="outline" size="sm" onClick={loadMore}>
                    Load more
                  </Button>
                ) : (
                  <div className="text-xs text-muted-foreground">
                    แสดง {logs.length} รายการ · ไม่มีข้อมูลเก่ากว่านี้แล้ว
                  </div>
                )}
              </div>
            </div>
          )}
        </CardContent>
      </Card>
    </div>
  )
}
