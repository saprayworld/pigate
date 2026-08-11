import { type ReactNode } from "react"
import { Activity, Ban, Info } from "lucide-react"
import {
  Drawer,
  DrawerContent,
  DrawerFooter,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { type PolicyRule } from "@/data-mockup/mockData"
import { type PolicyRuleStat } from "@/services/policyStatsService"
import { fmtBytes } from "@/lib/formatBytes"
import { fmtAbsoluteTime, fmtRelativeTime } from "@/lib/relativeTime"
import { cn } from "@/lib/utils"

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
}

function Field({ label, value }: { label: string; value: ReactNode }) {
  return (
    <div className="space-y-1">
      <div className="text-xs font-medium text-muted-foreground">{label}</div>
      <div className="text-sm text-foreground">{value}</div>
    </div>
  )
}

export default function RuleStatsDrawer({ open, onOpenChange, rule, stat, countersSince, available }: RuleStatsDrawerProps) {
  if (!rule) return null

  return (
    <Drawer direction="right" open={open} onOpenChange={onOpenChange}>
      <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[480px]">
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
