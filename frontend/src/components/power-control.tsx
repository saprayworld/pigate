import { Power, Loader2 } from "lucide-react"

export type PowerStatus = "idle" | "rebooting" | "shutting-down" | "powered-off"

// Full-screen status overlays shown while the board is rebooting / shutting
// down / powered off. Presentational only — renders nothing while idle so it
// can be dropped into any tree unconditionally.
export function PowerStatusOverlay({
  status,
  countdown,
}: {
  status: PowerStatus
  countdown: number
  onPowerOn: () => void
}) {
  if (status === "rebooting") {
    return (
      <div className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-background text-foreground font-mono">
        <div className="space-y-6 text-center max-w-md p-6">
          <Loader2 className="mx-auto h-16 w-16 text-primary animate-spin" />
          <h2 className="text-2xl font-bold tracking-wider text-primary">REBOOTING PIGATE SYSTEM</h2>
          <p className="text-muted-foreground text-sm leading-relaxed">
            กำลังรีสตาร์ทบริการ Linux Kernel และตัวประมวลผลเครือข่าย PiGate... กรุณารอสักครู่
          </p>
          <div className="text-5xl font-extrabold text-foreground font-mono tabular-nums">
            {countdown > 0 ? countdown : "OK"}
          </div>
          <div className="text-[11px] text-muted-foreground/60 border border-border bg-muted p-2 rounded">
            systemctl daemon-reexec && reboot
          </div>
        </div>
      </div>
    )
  }

  if (status === "shutting-down") {
    return (
      <div className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-background text-foreground font-mono">
        <div className="space-y-6 text-center max-w-md p-6">
          <Loader2 className="mx-auto h-16 w-16 text-red-500 animate-spin" />
          <h2 className="text-2xl font-bold tracking-wider text-red-500">SHUTTING DOWN SYSTEM</h2>
          <p className="text-muted-foreground text-sm leading-relaxed">
            กำลังสั่งหยุดโปรเซสเครือข่าย, ถอนการเชื่อมต่อดิสก์ และปิดไฟเลี้ยงบอร์ด Raspberry Pi 5...
          </p>
          <div className="text-[11px] text-muted-foreground/60 border border-border bg-muted p-2 rounded">
            systemctl poweroff -i
          </div>
        </div>
      </div>
    )
  }

  if (status === "powered-off") {
    return (
      <div className="fixed inset-0 z-50 flex flex-col items-center justify-center bg-background text-muted-foreground font-mono">
        <div className="space-y-6 text-center max-w-md border border-border bg-card rounded-xl p-8">
          <Power className="mx-auto h-14 w-14 text-muted-foreground" />
          <h2 className="text-xl font-bold tracking-wider text-muted-foreground">SYSTEM OFFLINE</h2>
          <p className="text-xs text-muted-foreground leading-relaxed">
            อุปกรณ์จะปิดตัวเองในไม่ช้า สามารถตรวจสอบสถานะของบอร์ด Raspberry Pi 5 ได้จากไฟ LED บนบอร์ดจะแสดงเป็นสีแดงค้าง แสดงว่าอุปกรณ์ปิดตัวเองเรียบร้อยแล้ว
          </p>
          {/* <Button
            onClick={onPowerOn}
            className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold w-full gap-2 mt-4"
          >
            <Power className="h-4 w-4" />
            Power On (เปิดเครื่องจำลอง)
          </Button> */}
        </div>
      </div>
    )
  }

  return null
}
