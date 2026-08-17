import { Link } from "react-router"
import { Info } from "lucide-react"

// DnsLoggingDisabledNotice — shown inside the reference popover whenever the
// backend responds with `enabled: false` (DNS query logging switched off —
// docs/ref/todo/reference-popover-plan.md Step 9). This is NOT an error
// state: the rest of the popover (hostname, traffic bytes, the "ดู Traffic"
// button) still renders normally around this notice — only the
// domain/IP-relationship section is empty, which this explains.
export function DnsLoggingDisabledNotice() {
  return (
    <div className="flex items-start gap-1.5 rounded-md border border-dashed border-muted-foreground/30 bg-muted/20 p-2 text-[11px] text-muted-foreground">
      <Info className="mt-0.5 size-3 shrink-0" />
      <p>
        ปิดการบันทึก DNS query อยู่ จึงไม่มีข้อมูล domain ที่เกี่ยวข้อง —{" "}
        <Link to="/network/dns-server?tab=settings" className="text-primary hover:underline">
          เปิดใช้งาน
        </Link>
      </p>
    </div>
  )
}
