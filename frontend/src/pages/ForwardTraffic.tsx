import { ArrowRightLeft } from "lucide-react"

import { TrafficLogPage } from "@/components/logs/TrafficLogPage"

export default function ForwardTraffic() {
  return (
    <TrafficLogPage
      title="Forward Traffic"
      description="เหตุการณ์ PASS/DROP ของแพ็กเก็ตที่วิ่งผ่านเครื่อง (LAN↔WAN) แบบเรียลไทม์"
      icon={ArrowRightLeft}
      chainParam="forward"
      clearConfirmTitle="ยืนยันการล้าง Forward Traffic Log"
      clearConfirmMessage="คุณต้องการล้าง log ทราฟฟิกที่บันทึกอยู่ใน RAM ทั้งหมดใช่หรือไม่? (ข้อมูลนี้ไม่ได้ถูกบันทึกถาวรอยู่แล้ว — การล้างจะล้างทั้ง Forward Traffic และ Local Traffic เพราะใช้ buffer เดียวกัน)"
      noteContent={
        <>
          หน้านี้แสดง <span className="font-medium text-foreground">ตัวอย่างล่าสุด</span> ของแพ็กเก็ตที่วิ่งผ่าน
          firewall (เก็บใน RAM ไม่ใช่บันทึกครบทุกแพ็กเก็ต) — จะเห็นเฉพาะแพ็กเก็ตที่
          <span className="font-medium text-foreground"> เปิด connection ใหม่</span> บน policy ที่เปิด Log ไว้
          และแพ็กเก็ตที่ <span className="font-medium text-foreground">ถูก DROP</span>; ทราฟฟิกของ connection
          ที่เปิดค้างไว้แล้ว (established) จะไม่ปรากฏโดยตั้งใจ
        </>
      }
    />
  )
}
