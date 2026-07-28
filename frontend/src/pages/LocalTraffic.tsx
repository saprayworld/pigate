import { useState } from "react"
import { ShieldAlert } from "lucide-react"

import { TrafficLogPage } from "@/components/logs/TrafficLogPage"
import { type TrafficChainFilter } from "@/services/trafficLogService"

const LOCAL_CHAIN_OPTIONS: Array<{ value: TrafficChainFilter; label: string }> = [
  { value: "local", label: "All local" },
  { value: "input", label: "Local-In only" },
  { value: "output", label: "Local-Out only" },
]

export default function LocalTraffic() {
  // "local" (input+output) is the default; the dropdown can narrow to one
  // side. Changing it flows through TrafficLogPage's refreshKey, which
  // resets pagination (fresh cursor + first page), same as any other filter.
  const [chain, setChain] = useState<TrafficChainFilter>("local")

  return (
    <TrafficLogPage
      title="Local Traffic"
      description="เหตุการณ์ PASS/DROP ของทราฟฟิกที่เข้า/ออกจากตัวบอร์ดเอง (ไม่ใช่ทราฟฟิกที่วิ่งผ่าน)"
      icon={ShieldAlert}
      chainParam={chain}
      extraFilter={{ value: chain, onChange: setChain, options: LOCAL_CHAIN_OPTIONS }}
      clearConfirmTitle="ยืนยันการล้าง Local Traffic Log"
      clearConfirmMessage="คุณต้องการล้าง log ทราฟฟิกที่บันทึกอยู่ใน RAM ทั้งหมดใช่หรือไม่? (ข้อมูลนี้ไม่ได้ถูกบันทึกถาวรอยู่แล้ว — การล้างจะล้างทั้ง Forward Traffic และ Local Traffic เพราะใช้ buffer เดียวกัน)"
      noteContent={
        <>
          <span className="font-medium text-foreground">Local-In</span> คือทราฟฟิกที่{" "}
          <span className="font-medium text-foreground">ปลายทางเป็นตัวบอร์ดเอง</span> (เช่น มีคนเข้าหน้าเว็บ/SSH,
          ping, หรือมีการสแกนพอร์ตจากภายนอก) ส่วน{" "}
          <span className="font-medium text-foreground">Local-Out</span> คือทราฟฟิกที่{" "}
          <span className="font-medium text-foreground">บอร์ดส่งออกเอง</span> (เช่น การ query DNS/NTP ของบอร์ด)
          — connection ที่เปิดค้างไว้แล้ว (established) จะไม่ปรากฏ และจะเห็นเฉพาะกฎที่เปิด Log ไว้
          (ยกเว้นกฎโครงสร้างบางจุดที่ log เสมอ เช่น final drop)
        </>
      }
    />
  )
}
