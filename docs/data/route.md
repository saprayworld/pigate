# Static Route — บันทึกการทำงานของระบบ

เอกสารนี้อธิบายกระบวนการทำงานของระบบ **Static Route** (เส้นทางเครือข่ายแบบคงที่) ใน PiGate ตั้งแต่โครงสร้างข้อมูล การซิงค์สถานะจากเคอร์เนลผ่าน `/proc/net/route` จนถึงการประยุกต์ใช้เส้นทางเครือข่ายบนระบบปฏิบัติการจริง

---

## 1. Data Model (`StaticRoute`)

ไฟล์: `backend/internal/model/types.go`

```go
type StaticRoute struct {
    ID          string `json:"id"`
    Destination string `json:"destination"` // เครือข่ายปลายทางในรูปแบบ CIDR (เช่น "192.168.10.0/24")
    Gateway     string `json:"gateway"`     // IP ของ Gateway ขัดต่อ (เว้นว่างหากเชื่อมต่อโดยตรง)
    Interface   string `json:"interface"`   // การ์ดเครือข่ายที่ออก เช่น "eth0", "wlan0"
    Metric      int    `json:"metric"`      // ลำดับความสำคัญในการเลือกเส้นทาง (ค่าต่ำกว่าจะถูกเลือกก่อน)
    Description string `json:"description"` // คำอธิบายเพิ่มเติม
    Status      bool   `json:"status"`      // สถานะเปิดใช้งานเส้นทาง (true = Active, false = Inactive)
    Type        string `json:"type"`        // "system" (สร้างจาก OS) หรือ "custom" (ผู้ใช้กำหนด)
}
```

---

## 2. Database Schema (`static_routes`)

ไฟล์: `backend/internal/db/connection.go`

```sql
CREATE TABLE IF NOT EXISTS static_routes (
    id          TEXT PRIMARY KEY,
    destination TEXT NOT NULL,
    gateway     TEXT NOT NULL,
    interface   TEXT NOT NULL,
    metric      INTEGER DEFAULT 0,
    description TEXT,
    status      INTEGER DEFAULT 1 CHECK(status IN (0, 1)),
    type        TEXT NOT NULL CHECK(type IN ('system', 'custom'))
);
```

### ข้อมูลตั้งต้นเมื่อฐานข้อมูลว่างเปล่า (Seed Data)
| ID | Destination | Gateway | Interface | Metric | Type |
|---|---|---|---|---|---|
| `route-1` | `0.0.0.0/0` | `10.0.0.1` | `wlan0` | 100 | system |
| `route-2` | `192.168.1.0/24` | (ว่าง) | `eth0` | 0 | system |
| `route-3` | `10.0.0.0/24` | (ว่าง) | `wlan0` | 0 | system |

---

## 3. การ Sync ข้อมูลเส้นทางจาก OS (`SyncRoutesFromOS`)

ไฟล์: `backend/internal/db/repository.go` — ฟังก์ชัน `SyncRoutesFromOS()`

### การซิงค์จะเกิดเมื่อไหร่
เมื่อเริ่มรันหลังบ้านในโหมดเชื่อมต่อจริงหรือแบบผสม (`mockFromReal = true`) ระบบจะเรียกเมธอดนี้เพื่อดึงตารางเส้นทางล่าสุดมาแสดงผลในฐานข้อมูล

### ลำดับการประมวลผลของ SyncRoutesFromOS:
1. เช็กสภาวะแวดล้อม หากไม่ใช่ระบบปฏิบัติการ **Linux** จะข้ามฟังก์ชันนี้ทันที
2. อ่านข้อมูลดิบจากไฟล์ระบบเคอร์เนล `/proc/net/route`
3. ทำการล้างเส้นทางระบบของเดิมทั้งหมด (`DELETE FROM static_routes WHERE type = 'system'`)
4. วนลูปอ่านข้อมูลของบรรทัดเส้นทางเครือข่าย:
   * แปลง IP ปลายทางและ Gateway จาก Little-Endian Hex เป็น IP string ปกติผ่าน helper `parseHexIP()` (เช่น `"00000000"` → `"0.0.0.0"`, `"0101A8C0"` → `"192.168.1.1"`)
   * แปลงค่า Hex Mask เป็นเลขความยาว Prefix (CIDR) (เช่น `"00FFFFFF"` → `/24`)
   * หาก Gateway เป็น `"0.0.0.0"` จะบันทึกลงฐานข้อมูลเป็นค่าว่าง (แสดงการเชื่อมต่อโดยตรง)
5. บันทึกเส้นทางเครือข่ายระบบที่อ่านมาได้ลงสู่ตารางฐานข้อมูลโดยระบุ `type = 'system'` และ `status = 1`

---

## 4. Helper Functions (OS Parsing)

### `parseHexIP(hexStr string)`
แปลงค่า Hex IP จาก `/proc/net/route` ความยาว 8 ตัวอักษรให้อยู่ในรูป IP string:
* ข้อมูลในไฟล์จัดเก็บแบบ Little-endian เช่น `"0022A8C0"`
* ตัวแปลงจะทำการแบ่งทีละ Byte แล้วเรียงลำดับย้อนกลับ: `C0` (192), `A8` (168), `22` (34), `00` (0) → `"192.168.34.0"`

---

## 5. REST API Endpoints

| Method | Path | Handler | หน้าที่ |
|---|---|---|---|
| `GET` | `/api/routes` | `HandleGetRoutes` | ดึงเส้นทางทั้งหมดในฐานข้อมูล |
| `POST` | `/api/routes` | `HandleCreateRoute` | เพิ่มเส้นทางเครือข่ายที่กำหนดเอง (`type = 'custom'`) |
| `PUT` | `/api/routes/{id}` | `HandleUpdateRoute` | แก้ไขเส้นทางเครือข่ายที่กำหนดเอง |
| `DELETE` | `/api/routes/{id}` | `HandleDeleteRoute` | ลบเส้นทางเครือข่ายที่กำหนดเอง |
| `POST` | `/api/routes/bulk-delete` | `HandleBulkDeleteRoutes` | ลบเส้นทางทีละหลายรายการพร้อมกัน |
| `POST` | `/api/routes/{id}/toggle` | `HandleToggleRoute` | สลับเปิด/ปิดใช้งานสถานะของเส้นทาง (Active/Inactive) |
| `POST` | `/api/routes/apply` | `HandleApplyRoutes` | โหลดตั้งค่าเส้นทางเครือข่ายทั้งหมดลงเคอร์เนลจริง |

---

## 6. ข้อควรระวัง

1. **การล็อกเส้นทางระบบ (System Predefined Routing Lock)** — เส้นทางที่มี `type = 'system'` ถูกสร้างจาก OS จะ**ไม่สามารถอัปเดตหรือลบได้**ผ่าน REST API เพื่อป้องกันไม่ให้ผู้ใช้งานยกเลิกเส้นทางหลักจนขาดการเชื่อมต่อ
2. **การลบแบบ Bulk (Bulk Delete)** — ฟังก์ชันลบแบบหลายรายการพร้อมกันจะมีระบบตรวจหา หากมีเส้นทางระบบรวมอยู่ด้วย ธุรกรรมจะสั่ง Rollback ทันทีและปฏิเสธคำร้องขอเพื่อความปลอดภัย
3. **การชนกันของ Gateway และ CIDR** — ในการกรอกข้อมูลปลายทาง ต้องเป็นรูปแบบ CIDR ที่ถูกต้อง (เช่น `/24`, `/16`) และไอพี Gateway จะต้องอยู่ในวง Subnet เดียวกันกับการ์ดเครือข่ายขาออกที่ระบุไว้

---

## 7. Kernel Integration (Production)

ไฟล์: `backend/internal/kernel/real_routing.go` (วางแผนพัฒนาในระยะที่ 2)

ในโหมดการทำงานจริง ระบบจะใช้ Netlink Socket ในการประมวลผลเพิ่ม/ลบ เส้นทางบน Routing Table ของระบบปฏิบัติการ แทนการใช้ชุดคำสั่งครอบ `ip route add / del` เพื่อป้องกันช่องโหว่ความปลอดภัย

### ตัวอย่างการประยุกต์คำสั่งผ่าน netlink:
```go
import "github.com/vishvananda/netlink"

func (r *RealRouting) ApplyRoutes(routes []model.StaticRoute) error {
    // 1. ดึงข้อมูลเส้นทางปัจจุบันและเคลียร์เส้นทาง custom เดิม
    // 2. วนลูปสร้าง netlink.Route Object จากฐานข้อมูล
    // 3. เรียกใช้งาน netlink.RouteAdd() หรือ netlink.RouteDel() ลงเคอร์เนล
}
```

---

## 8. ไฟล์ที่เกี่ยวข้อง

| ไฟล์ | หน้าที่ |
|---|---|
| [`backend/internal/model/types.go`](../../../backend/internal/model/types.go) | โครงสร้าง structs ของ `StaticRoute` และ `StaticRouteInput` |
| [`backend/internal/db/connection.go`](../../../backend/internal/db/connection.go) | DB Schema ของตาราง `static_routes` และข้อมูล seed |
| [`backend/internal/db/repository.go`](../../../backend/internal/db/repository.go) | ฟังก์ชัน CRUD, การซิงค์และแปลงค่า Hex IP จาก `/proc/net/route` |
| [`backend/internal/api/handlers.go`](../../../backend/internal/api/handlers.go) | ตัวจัดการ request ของ API เส้นทางเครือข่ายทั้งหมด |
| [`backend/internal/kernel/interfaces.go`](../../../backend/internal/kernel/interfaces.go) | `RoutingManager` interface ที่กำหนดฟังก์ชัน `ApplyRoutes` |
| [`backend/internal/kernel/mock.go`](../../../backend/internal/kernel/mock.go) | ตัวจำลองโครงสร้างและพฤติกรรม RoutingManager |
