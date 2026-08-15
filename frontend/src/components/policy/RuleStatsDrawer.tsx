import { type ReactNode, useEffect, useState } from "react"
import { useNavigate } from "react-router"
import { Activity, Ban, Info, ExternalLink } from "lucide-react"
import {
  Drawer,
  DrawerContent,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { type PolicyRule } from "@/data-mockup/mockData"
import { type PolicyRuleStat } from "@/services/policyStatsService"
import { policyService } from "@/services/policyService"
import { useAlert } from "@/hooks/useAlert"
import { getErrorMessage } from "@/lib/errors"
import {
  policyEndpointsService,
  type EndpointHit,
  type PolicyRuleEndpoints,
  type ServiceHit,
} from "@/services/policyEndpointsService"
import { fmtBytes } from "@/lib/formatBytes"
import { fmtAbsoluteTime, fmtRelativeTime } from "@/lib/relativeTime"
import { cn } from "@/lib/utils"

// Endpoints panel refresh cadence while the drawer stays open (T-10).
const ENDPOINTS_REFRESH_MS = 10_000

// RuleStatsDrawer is a read-only detail panel for one policy rule's usage
// statistics (docs/ref/todo/firewall-policy-rule-usage-stats-plan.md T-13),
// opened from the icon button PolicyChainPage adds to each row. Deliberately
// separate from the create/edit Drawer in PolicyChainPage.tsx — this one has
// no Combobox, so it keeps the default modal Drawer behavior (docs/
// rules_of_work.md: modal={false} only needed when a Combobox is present).
interface RuleStatsDrawerProps {
  open: boolean
  onOpenChange: (open: boolean) => void
  rule: PolicyRule | null
  stat?: PolicyRuleStat
  countersSince?: string
  available: boolean
  // onChanged (T-16) is called after a Monitor toggle or reset succeeds, so
  // the caller can refetch policies/stats immediately instead of waiting for
  // its own poll cadence (docs/ref/todo/
  // fqdn-retry-and-monitored-counters-plan.md D-6/T-16, issue #141).
  onChanged?: () => void
}

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="space-y-1">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="text-sm text-foreground">{value}</div>
    </div>
  )
}

// EndpointRow renders one Top Source/Destination/Service row: name on top
// (addressName ?? domain ?? hostname, or serviceName), raw IP / PROTO-PORT
// underneath, count + relative last-seen time on the right. onViewLogs (T-13
// row-level deep-link), when provided, adds a small "view in Traffic Log"
// button that navigates filtered to just this IP/port.
function EndpointRow({
  primary,
  secondary,
  count,
  lastSeenAt,
  fromRule,
  onViewLogs,
}: {
  primary: string
  secondary: string
  count: number
  lastSeenAt: string
  fromRule: boolean
  onViewLogs?: () => void
}) {
  return (
    <div className="flex items-center justify-between gap-3 rounded-md border border-border/50 px-3 py-2">
      <div className="min-w-0 space-y-0.5">
        <div className="flex items-center gap-1.5">
          <span className="truncate text-sm font-medium text-foreground">{primary}</span>
          {fromRule ? (
            <Badge variant="outline" className="shrink-0 rounded px-1.5 py-0 text-[10px] font-semibold text-primary">
              จากกฎนี้
            </Badge>
          ) : null}
        </div>
        <div className="truncate text-xs text-muted-foreground">{secondary}</div>
      </div>
      <div className="flex shrink-0 items-center gap-2">
        <div className="space-y-0.5 text-right">
          <div className="text-sm font-semibold text-foreground">{count.toLocaleString()} ครั้ง</div>
          <div className="text-xs text-muted-foreground">{fmtRelativeTime(lastSeenAt)}</div>
        </div>
        {onViewLogs ? (
          <Button
            variant="ghost"
            size="icon"
            className="size-7 cursor-pointer text-muted-foreground"
            title="ดู log ของรายการนี้"
            onClick={onViewLogs}
          >
            <ExternalLink className="size-3.5" />
          </Button>
        ) : null}
      </div>
    </div>
  )
}

function endpointDisplayName(hit: EndpointHit): string {
  return hit.addressName || hit.domain || hit.hostname || hit.ip
}

function serviceDisplayName(hit: ServiceHit): string {
  return hit.serviceName || `${hit.proto}/${hit.port}`
}

// buildTrafficLogPath resolves the target Traffic Log route + query string
// for a policy rule's chain (T-13): "forward" -> Forward Traffic page,
// "input"/"output" -> Local Traffic page pre-filtered to that side via
// ?chain=. Every value is encodeURIComponent'd.
function buildTrafficLogPath(rule: PolicyRule, q: string): string {
  const query = `?q=${encodeURIComponent(q)}`
  if (rule.chain === "forward") {
    return `/logs/traffic${query}`
  }
  return `/logs/local${query}&chain=${encodeURIComponent(rule.chain)}`
}

export default function RuleStatsDrawer({ open, onOpenChange, rule, stat, countersSince, available, onChanged }: RuleStatsDrawerProps) {
  const navigate = useNavigate()
  const { alert, confirm } = useAlert()
  const [endpoints, setEndpoints] = useState<PolicyRuleEndpoints | null>(null)
  const [endpointsError, setEndpointsError] = useState<string | null>(null)
  const [endpointsLoading, setEndpointsLoading] = useState(false)
  const [monitorBusy, setMonitorBusy] = useState(false)
  const [logToggleBusy, setLogToggleBusy] = useState(false)

  // Fetch on open, refresh every 10s while open, and unsubscribe (abort +
  // clear the interval) on close/unmount so no request or timer is ever left
  // running once the drawer is dismissed.
  useEffect(() => {
    if (!open || !rule) {
      return
    }

    let cancelled = false
    const ruleId = rule.id

    const load = async () => {
      setEndpointsLoading(true)
      try {
        const result = await policyEndpointsService.getRuleEndpoints(ruleId)
        if (!cancelled) {
          setEndpoints(result)
          setEndpointsError(null)
        }
      } catch (err) {
        if (!cancelled) {
          setEndpointsError(err instanceof Error ? err.message : "Failed to load matched endpoints")
        }
      } finally {
        if (!cancelled) {
          setEndpointsLoading(false)
        }
      }
    }

    void load()
    const interval = window.setInterval(() => void load(), ENDPOINTS_REFRESH_MS)

    return () => {
      cancelled = true
      window.clearInterval(interval)
      // Reset state as the drawer closes (or the target rule changes) so a
      // reopen never briefly flashes the previous rule's stale data.
      setEndpoints(null)
      setEndpointsError(null)
      setEndpointsLoading(false)
    }
  }, [open, rule])

  // handleToggleMonitor flips rule.monitored via POST /policies/{id}/toggle-monitor
  // (T-16). Turning Monitor OFF discards the accumulated total permanently,
  // so it requires a confirm dialog; turning it ON does not. On any
  // error (including a 403 from a read-only role/-disable-edit — D-7, this
  // page has no frontend role gate) the Switch/accumulated total are left
  // untouched (they're driven entirely by the `rule`/`stat` props, which are
  // only refreshed via onChanged after a real success) and an alert is
  // shown instead.
  const handleToggleMonitor = async () => {
    if (!rule) return
    if (rule.monitored) {
      const ok = await confirm(
        "ปิดการเก็บสถิติสะสม",
        "การปิด Monitor จะลบยอดสะสมทั้งหมดของกฎนี้ทิ้งถาวรและกู้คืนไม่ได้ รวมถึงรายการ Endpoints (IP/Service) ที่เก็บไว้ทั้งหมดด้วย ต้องการดำเนินการต่อหรือไม่?"
      )
      if (!ok) return
    }

    setMonitorBusy(true)
    try {
      await policyService.toggleMonitor(rule.id)
      onChanged?.()
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปลี่ยนสถานะการเก็บสถิติสะสมได้: " + getErrorMessage(err))
    } finally {
      setMonitorBusy(false)
    }
  }

  // handleResetMonitor zeroes the persisted counter — always requires
  // confirm (data loss, though not as final as turning Monitor off since the
  // running total keeps accumulating afterward).
  const handleResetMonitor = async () => {
    if (!rule) return
    const ok = await confirm(
      "รีเซ็ตยอดสะสม",
      "ยอดสะสม (Bytes/Packets) ของกฎนี้จะถูกรีเซ็ตเป็น 0 และเริ่มนับใหม่ทันที พร้อมทั้งลบรายการ Endpoints (IP/Service) ที่เก็บไว้ทั้งหมด ต้องการดำเนินการต่อหรือไม่?"
    )
    if (!ok) return

    setMonitorBusy(true)
    try {
      await policyService.resetMonitorCounter(rule.id)
      onChanged?.()
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถรีเซ็ตยอดสะสมได้: " + getErrorMessage(err))
    } finally {
      setMonitorBusy(false)
    }
  }

  // handleQuickEnableLog is the "เปิด Log ให้กฎนี้" shortcut button (E-12,
  // docs/ref/todo/persisted-rule-endpoints-plan.md E-D7, issue #141
  // follow-up): shown only when Monitor is on but Log is off, so a rule can
  // be a "collector" without a separate confirm — turning Log on itself
  // isn't a data-loss action. Same error-handling convention as
  // handleToggleMonitor/handleResetMonitor: an error just alerts, no local
  // state is guessed at (onChanged?.() re-fetches on real success only).
  const handleQuickEnableLog = async () => {
    if (!rule) return
    setLogToggleBusy(true)
    try {
      await policyService.toggleLog(rule.id)
      onChanged?.()
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปิด Log ให้กฎนี้ได้: " + getErrorMessage(err))
    } finally {
      setLogToggleBusy(false)
    }
  }

  if (!rule) return null

  // viewLogsFor: row-level deep-link (T-13) — navigates to the same
  // Forward/Local page as the header button, but with q set to just this
  // row's value instead of the rule name.
  const viewLogsFor = (q: string) => {
    navigate(buildTrafficLogPath(rule, q))
    onOpenChange(false)
  }

  return (
    <Drawer direction="right" open={open} onOpenChange={onOpenChange}>
      <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[560px]">
        <DrawerHeader className="border-b border-border/50">
          <DrawerTitle className="flex items-center gap-2 text-base font-semibold">
            <Activity className="h-4 w-4 text-muted-foreground" />
            สถิติการใช้งานกฎ: {rule.name}
          </DrawerTitle>
        </DrawerHeader>

        <div className="flex-1 space-y-5 overflow-y-auto p-4">
          <div className="grid grid-cols-2 gap-4">
            <Field label="Chain" value={<Badge variant="secondary" className="rounded px-2 py-0.5 text-[10px] font-mono uppercase">{rule.chain}</Badge>} />
            <Field
              label="Action"
              value={
                <Badge
                  variant="outline"
                  className={cn(
                    "rounded px-2 py-0.5 text-[10px] font-bold",
                    rule.action === "ACCEPT"
                      ? "border-primary/20 bg-primary/10 text-primary"
                      : "border-destructive/20 bg-destructive/10 text-destructive"
                  )}
                >
                  {rule.action}
                </Badge>
              }
            />
            <Field label="Log" value={rule.log ? "เปิด" : "ปิด"} />
            <Field
              label="Status"
              value={
                rule.status ? (
                  <span className="font-semibold text-primary">Enable</span>
                ) : (
                  <span className="text-muted-foreground">Disable</span>
                )
              }
            />
          </div>

          {!rule.status ? (
            <div className="flex items-start gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs text-muted-foreground">
              <Ban className="mt-0.5 h-4 w-4 shrink-0" />
              <span>กฎนี้ถูกปิดใช้งาน (Disabled) จึงไม่ถูกสร้างในระบบ nftables — ไม่มีข้อมูลสถิติการใช้งานให้แสดง</span>
            </div>
          ) : !available ? (
            <div className="flex items-start gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs text-muted-foreground">
              <Info className="mt-0.5 h-4 w-4 shrink-0" />
              <span>ยังไม่มีข้อมูลสถิติ (รอรอบสำรวจถัดไป ~10 วินาที)</span>
            </div>
          ) : stat ? (
            <>
              <div className="grid grid-cols-2 gap-4">
                <Field label="Bytes (ตั้งแต่ Apply ล่าสุด)" value={fmtBytes(stat.bytes)} />
                <Field label="Packets" value={stat.packets.toLocaleString()} />
                <Field label="% ของทราฟฟิกทั้งหมด" value={`${stat.percent.toLocaleString()}%`} />
                <Field
                  label="สถานะการใช้งาน"
                  value={
                    stat.unused ? (
                      <Badge variant="secondary" className="rounded px-2 py-0.5 text-[10px] font-semibold">
                        Unused
                      </Badge>
                    ) : (
                      <span className="font-semibold text-primary">มีการใช้งาน</span>
                    )
                  }
                />
              </div>

              <div className="space-y-1">
                <div className="text-xs font-medium text-muted-foreground">ใช้งานล่าสุดเมื่อ (Last matched at)</div>
                {stat.lastMatchedAt ? (
                  <div className="space-y-0.5">
                    <div className="text-sm text-foreground">{fmtRelativeTime(stat.lastMatchedAt)}</div>
                    <div className="text-xs text-muted-foreground">
                      {fmtAbsoluteTime(stat.lastMatchedAt)}{" "}
                      <Badge variant="outline" className="rounded px-1.5 py-0 text-[10px]">
                        {stat.lastMatchedSource === "log" ? "จาก Traffic Log" : "จาก nft counter poll (±10s)"}
                      </Badge>
                    </div>
                  </div>
                ) : (
                  <div className="text-sm text-muted-foreground">ไม่ทราบ</div>
                )}
              </div>
            </>
          ) : (
            <div className="text-sm text-muted-foreground">ไม่พบข้อมูลสถิติสำหรับกฎนี้</div>
          )}

          <div className="space-y-1 border-t border-border/50 pt-4">
            <div className="text-xs font-medium text-muted-foreground">
              รวบรวมสถิติตั้งแต่ (Counters since)
            </div>
            <div className="text-sm text-foreground">{countersSince ? fmtAbsoluteTime(countersSince) : "ยังไม่เคย Apply Settings"}</div>
          </div>

          <div className="space-y-3 border-t border-border/50 pt-4">
            <div className="flex items-center justify-between gap-2">
              <div className="space-y-0.5">
                <div className="text-sm font-semibold text-foreground">เก็บสถิติสะสม (Monitor)</div>
                <p className="text-xs text-muted-foreground">
                  เปิดแล้วยอดจะสะสมต่อเนื่องไม่รีเซ็ตตอน Apply Settings หรือรีสตาร์ทระบบ
                </p>
              </div>
              <Switch
                checked={rule.monitored}
                disabled={monitorBusy}
                onCheckedChange={() => void handleToggleMonitor()}
              />
            </div>

            {rule.monitored ? (
              <div className="space-y-3 rounded-lg border border-border/50 p-3">
                <div className="grid grid-cols-2 gap-4">
                  <Field label="Bytes สะสม" value={fmtBytes(stat?.monitoredBytes ?? 0)} />
                  <Field label="Packets สะสม" value={(stat?.monitoredPackets ?? 0).toLocaleString()} />
                </div>
                <Field
                  label="เก็บมาตั้งแต่"
                  value={stat?.monitoredSince ? fmtAbsoluteTime(stat.monitoredSince) : "—"}
                />
                <Button
                  variant="outline"
                  size="sm"
                  className="cursor-pointer"
                  disabled={monitorBusy}
                  onClick={() => void handleResetMonitor()}
                >
                  รีเซ็ตค่า
                </Button>
              </div>
            ) : null}
          </div>

          <div className="space-y-3 border-t border-border/50 pt-4">
            <div className="flex items-center justify-between gap-2">
              <div className="text-sm font-semibold text-foreground">Endpoints ที่ตรงกับกฎนี้</div>
              <Button
                variant="outline"
                size="sm"
                className="h-7 cursor-pointer gap-1.5 text-xs"
                onClick={() => {
                  navigate(buildTrafficLogPath(rule, rule.name || rule.id))
                  onOpenChange(false)
                }}
              >
                <ExternalLink className="size-3.5" />
                ดู log ของกฎนี้
              </Button>
            </div>
            <p className="text-[11px] text-muted-foreground">
              เป็นการค้นหาแบบ substring บนชื่อกฎ ณ ตอนที่บันทึก log — ถ้ากฎถูกเปลี่ยนชื่อภายหลัง แถวเก่าจะไม่ตรง
              และชื่อที่เป็น substring ของกฎอื่นอาจติดมาด้วย
            </p>

            {endpoints ? (
              <div className="flex flex-wrap items-center gap-2 text-xs text-muted-foreground">
                {endpoints.source === "persisted" ? (
                  <>
                    <Badge variant="outline" className="shrink-0 rounded px-1.5 py-0 text-[10px]">
                      เก็บถาวร
                    </Badge>
                    <span>
                      เก็บมาตั้งแต่ {endpoints.collectingSince ? fmtAbsoluteTime(endpoints.collectingSince) : "—"} — ไม่หายเมื่อ
                      Apply/รีสตาร์ท/ล้าง Traffic Log
                    </span>
                  </>
                ) : (
                  <>
                    <Badge variant="outline" className="shrink-0 rounded px-1.5 py-0 text-[10px]">
                      จาก Traffic Log
                    </Badge>
                    {endpoints.bufferOldestAt ? <span>ข้อมูลย้อนหลังถึง {fmtAbsoluteTime(endpoints.bufferOldestAt)}</span> : null}
                  </>
                )}
              </div>
            ) : null}

            {endpoints && endpoints.source === "buffer" && !rule.monitored ? (
              <p className="text-xs text-muted-foreground">เปิด Monitor ด้านบนเพื่อเก็บรายการนี้แบบถาวร (จะเริ่มนับใหม่จากศูนย์)</p>
            ) : null}

            {endpointsError ? (
              <div className="flex items-start gap-2 rounded-lg border border-destructive/20 bg-destructive/10 p-3 text-xs text-destructive">
                <Info className="mt-0.5 h-4 w-4 shrink-0" />
                <span>{endpointsError}</span>
              </div>
            ) : !endpoints ? (
              <div className="text-xs text-muted-foreground">{endpointsLoading ? "กำลังโหลด..." : "ไม่มีข้อมูล"}</div>
            ) : !endpoints.logEnabled ? (
              <div className="flex flex-col items-start gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs text-muted-foreground">
                <div className="flex items-start gap-2">
                  <Info className="mt-0.5 h-4 w-4 shrink-0" />
                  <span>
                    {rule.monitored
                      ? "เปิด Monitor แล้วแต่กฎนี้ยังไม่ได้เปิด Log จึงยังไม่มีข้อมูล Endpoints ให้เก็บ"
                      : "กฎนี้ยังไม่ได้เปิด Log จึงไม่มีข้อมูล IP/Service ที่ตรงกับกฎนี้ — เปิด Log ที่กฎนี้เพื่อเริ่มเก็บข้อมูล"}
                  </span>
                </div>
                {rule.monitored ? (
                  <Button
                    variant="outline"
                    size="sm"
                    className="cursor-pointer"
                    disabled={logToggleBusy}
                    onClick={() => void handleQuickEnableLog()}
                  >
                    เปิด Log ให้กฎนี้
                  </Button>
                ) : null}
              </div>
            ) : (
              <>
                {endpoints.truncated ? (
                  <div className="text-xs text-muted-foreground">แสดงเฉพาะ Top {endpoints.limit} รายการต่อหมวด (มีมากกว่านี้)</div>
                ) : null}
                {endpoints.capped ? (
                  <div className="text-xs text-muted-foreground">
                    เก็บได้สูงสุด {endpoints.maxPerDirection.toLocaleString()} รายการต่อหมวด — รายการที่ไม่ถูกพบนานที่สุดจะถูกลบออกก่อน
                    (ลบไปแล้ว {endpoints.evicted.toLocaleString()} รายการ)
                  </div>
                ) : null}

                <div className="space-y-2">
                  <div className="text-xs font-medium text-muted-foreground">
                    Top Source {endpoints.uniqueSources > 0 ? `(${endpoints.uniqueSources} รายการ)` : ""}
                  </div>
                  {endpoints.sources.length === 0 ? (
                    <div className="text-xs text-muted-foreground">ไม่มีข้อมูล</div>
                  ) : (
                    <div className="space-y-1.5">
                      {endpoints.sources.map((hit) => (
                        <EndpointRow
                          key={hit.ip}
                          primary={endpointDisplayName(hit)}
                          secondary={hit.ip}
                          count={hit.count}
                          lastSeenAt={hit.lastSeenAt}
                          fromRule={hit.fromRule}
                          onViewLogs={() => viewLogsFor(hit.ip)}
                        />
                      ))}
                    </div>
                  )}
                </div>

                <div className="space-y-2">
                  <div className="text-xs font-medium text-muted-foreground">
                    Top Destination {endpoints.uniqueDestinations > 0 ? `(${endpoints.uniqueDestinations} รายการ)` : ""}
                  </div>
                  {endpoints.destinations.length === 0 ? (
                    <div className="text-xs text-muted-foreground">ไม่มีข้อมูล</div>
                  ) : (
                    <div className="space-y-1.5">
                      {endpoints.destinations.map((hit) => (
                        <EndpointRow
                          key={hit.ip}
                          primary={endpointDisplayName(hit)}
                          secondary={hit.ip}
                          count={hit.count}
                          lastSeenAt={hit.lastSeenAt}
                          fromRule={hit.fromRule}
                          onViewLogs={() => viewLogsFor(hit.ip)}
                        />
                      ))}
                    </div>
                  )}
                </div>

                <div className="space-y-2">
                  <div className="text-xs font-medium text-muted-foreground">
                    Top Service {endpoints.uniqueServices > 0 ? `(${endpoints.uniqueServices} รายการ)` : ""}
                  </div>
                  {endpoints.services.length === 0 ? (
                    <div className="text-xs text-muted-foreground">ไม่มีข้อมูล</div>
                  ) : (
                    <div className="space-y-1.5">
                      {endpoints.services.map((hit) => (
                        <EndpointRow
                          key={`${hit.proto}/${hit.port}`}
                          primary={serviceDisplayName(hit)}
                          secondary={`${hit.proto}/${hit.port}`}
                          count={hit.count}
                          lastSeenAt={hit.lastSeenAt}
                          fromRule={hit.fromRule}
                          onViewLogs={() => viewLogsFor(hit.proto === "ICMP" && hit.port === "-" ? hit.proto : hit.port)}
                        />
                      ))}
                    </div>
                  )}
                </div>
              </>
            )}
          </div>

          <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
            <Info className="mt-0.5 h-4 w-4 shrink-0" />
            <div className="space-y-1.5">
              <p>
                <strong className="text-foreground">ข้อจำกัดของข้อมูลสถิตินี้:</strong>
              </p>
              <p>1. เป็นตัวเลข "ตั้งแต่ Apply Settings ล่าสุด" ไม่ใช่สะสมตลอดชีพของกฎ (nftables รีเซ็ต counter ทุกครั้งที่ Apply)</p>
              <p>2. % คำนวณเทียบกับทราฟฟิกทุก chain รวมกันเสมอ ไม่ใช่เฉพาะ chain ที่กำลังดูอยู่</p>
              <p>3. "ใช้งานล่าสุดเมื่อ" มาจาก 2 แหล่ง: Traffic Log (แม่นยำ ต้องเปิด Log) หรือ nft counter poll (ทุก 10 วินาที ความคลาดเคลื่อน ±10 วินาที)</p>
              <p>4. กฎที่ปิดใช้งาน (Disabled) จะไม่มีข้อมูลสถิติเลย เพราะไม่ถูกสร้างในระบบ nftables</p>
              <p>5. Endpoints ที่ตรงกับกฎนี้ต้องเปิด Log ที่กฎนั้นด้วย ไม่งั้นจะไม่มีข้อมูลให้แสดงเลย</p>
              <p>6. ตัวนับ Endpoints คือ "จำนวนครั้งที่พบใน Traffic Log" ไม่ใช่ bytes และครอบคลุมเฉพาะช่วงที่ยังอยู่ใน buffer ของ Traffic Log เท่านั้น — ล้าง Log แล้วข้อมูลนี้จะหายไปด้วย</p>
              <p>7. ชื่อที่แสดงสำหรับ IP จะเลือกจากชื่อ Address Object ที่ตั้งไว้ก่อน ตามด้วยชื่อโดเมนจาก DNS แล้วจึงเป็นชื่อ Host จาก DHCP ตามลำดับ</p>
              <p>
                8. ยอดในหมวด "เก็บสถิติสะสม (Monitor)" เป็นคนละชุดข้อมูลกับตัวเลข "ตั้งแต่ Apply ล่าสุด" ด้านบน — สะสมข้าม
                Apply/รีสตาร์ทระบบ และอาจคลาดเคลื่อนได้สูงสุดตามรอบบันทึกลงฐานข้อมูล (~5 นาที) หากไฟดับกะทันหัน
              </p>
              <p>
                9. เมื่อเปิด Monitor รายการ Endpoints (IP/Service) จะถูกเก็บถาวรลงฐานข้อมูลด้วยเช่นกัน ภายใต้เพดานจำนวนรายการต่อหมวด
                — เมื่อเต็มเพดาน รายการที่ไม่ถูกพบนานที่สุดจะถูกลบออกก่อน (LRU) และเริ่มนับใหม่จากศูนย์ ณ วินาทีที่เปิด Monitor
                ไม่มีการดึงข้อมูลเก่าจาก Traffic Log มาใส่ย้อนหลัง
              </p>
              <p>
                10. ไม่ว่าจะเปิด Monitor หรือไม่ กฎนั้นยังต้องเปิด Log ด้วยเสมอจึงจะมีข้อมูล Endpoints ให้เก็บ
                (ข้อจำกัดของ NFLOG ไม่ใช่บั๊ก) และการปิด Monitor หรือกดรีเซ็ตค่าจะลบรายการ Endpoints ที่เก็บไว้ทั้งหมดของกฎนั้นทิ้งไปด้วย
              </p>
            </div>
          </div>
        </div>

        <DrawerFooter className="border-t border-border/50">
          <Button variant="outline" onClick={() => onOpenChange(false)} className="cursor-pointer">
            ปิด
          </Button>
        </DrawerFooter>
      </DrawerContent>
    </Drawer>
  )
}
