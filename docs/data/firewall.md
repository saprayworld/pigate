# Firewall Policy — บันทึกการทำงานของระบบ

เอกสารนี้อธิบายกระบวนการทำงานของระบบ **Firewall Policy** ใน PiGate ตั้งแต่ข้อมูลโครงสร้างฐานข้อมูล การลากจัดลำดับความสำคัญ (Drag & Drop Priority) ไปจนถึงการเขียนคำสั่งลง Linux Kernel `nftables` จริง

---

## 1. Data Model (`PolicyRule`)

ไฟล์: `backend/internal/model/types.go`

```go
type PolicyRule struct {
    ID           string   `json:"id"`
    Name         string   `json:"name"`
    InInterface  string   `json:"inInterface"`  // "eth0", "wlan0" หรือ "" (Any)
    OutInterface string   `json:"outInterface"` // "eth0", "wlan0" หรือ "" (Any)
    Source       []string `json:"source"`       // รายชื่อ Address Objects (เช่น ["LAN_Internal"])
    Destination  []string `json:"destination"`  // รายชื่อ Address Objects (เช่น ["ALL"])
    Service      []string `json:"service"`      // รายชื่อ Service Objects (เช่น ["HTTP", "HTTPS"])
    Action       string   `json:"action"`       // "ACCEPT" หรือ "DROP"
    Log          bool     `json:"log"`          // เปิด/ปิดการบันทึก Log (true/false)
    Status       bool     `json:"status"`       // สถานะเปิดใช้งานกฎ (true = Active, false = Inactive)
    Priority     int      `json:"-"`            // ลำดับความสำคัญในการตรวจสอบกฎ (เรียงจากน้อยไปมาก)
}
```

---

## 2. Database Schema (`firewall_policies`)

ไฟล์: `backend/internal/db/connection.go`

เนื่องจากกฎไฟร์วอลล์รองรับความสัมพันธ์แบบ Many-to-Many กับ Address Objects และ Service Objects โครงสร้างตารางจึงถูกแยกออกเป็น 3 ตารางหลักดังนี้:

```sql
-- 1. ตารางเก็บรายละเอียดกฎหลัก
CREATE TABLE IF NOT EXISTS firewall_policies (
    id            TEXT PRIMARY KEY,
    name          TEXT NOT NULL,
    in_interface  TEXT NOT NULL,
    out_interface TEXT NOT NULL,
    action        TEXT NOT NULL CHECK(action IN ('ACCEPT', 'DROP')),
    log           INTEGER DEFAULT 0 CHECK(log IN (0, 1)),
    status        INTEGER DEFAULT 1 CHECK(status IN (0, 1)),
    priority      INTEGER NOT NULL
);

-- 2. ตารางเชื่อมโยง Address Objects (Many-to-Many)
CREATE TABLE IF NOT EXISTS policy_addresses (
    policy_id        TEXT NOT NULL,
    address_id       TEXT NOT NULL,
    association_type TEXT NOT NULL CHECK(association_type IN ('SOURCE', 'DESTINATION')),
    PRIMARY KEY (policy_id, address_id, association_type),
    FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE,
    FOREIGN KEY (address_id) REFERENCES address_objects(id) ON DELETE RESTRICT
);

-- 3. ตารางเชื่อมโยง Service Objects (Many-to-Many)
CREATE TABLE IF NOT EXISTS policy_services (
    policy_id  TEXT NOT NULL,
    service_id TEXT NOT NULL,
    PRIMARY KEY (policy_id, service_id),
    FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE,
    FOREIGN KEY (service_id) REFERENCES service_objects(id) ON DELETE RESTRICT
);
```

### การป้องกันความสมบูรณ์ของข้อมูล (Data Integrity)
* ตารางเชื่อมโยงกำหนด `ON DELETE RESTRICT` สำหรับ Address และ Service Objects เพื่อป้องกันไม่ให้ผู้ใช้ลบวัตถุระบบที่กฎไฟร์วอลล์กำลังอ้างอิงอยู่
* ฝั่งหน้าบ้าน (Frontend) จะมีกลไกตรวจสอบความสัมพันธ์และจะบล็อกคำสั่งลบหากพบว่าค่า `refPolicies` ไม่ว่าง

---

## 3. กระบวนการประมวลผลและการจัดเรียงกฎ (Rule Evaluation & Ordering)

ไฟร์วอลล์ประมวลผลกฎจากบนลงล่าง (**First-Match Wins**) ดังนั้นลำดับของกฎ (Priority) จึงสำคัญมาก

### ลำดับการจัดเรียงผ่าน Drag & Drop (Frontend)
1. ผู้ใช้ทำการสลับตำแหน่งกฎที่ต้องการบนหน้าจอผ่าน UI ที่ใช้งาน `@dnd-kit/core`
2. ระบบหน้าบ้านจะส่งอาเรย์ของกฎไฟร์วอลล์ที่จัดเรียงลำดับใหม่แล้วไปยัง `PUT /api/policies/reorder`
3. หลังบ้านจะดำเนินการอัปเดตค่า `priority` ในธุรกรรม (Transaction) ตั้งแต่แถวแรกจนถึงแถวสุดท้าย:
   ```go
   func (r *Repository) SaveAllPolicies(policies []model.PolicyRule) error {
       tx, err := r.db.Begin()
       // ... loop update ...
       for idx, p := range policies {
           _, err := tx.Exec("UPDATE firewall_policies SET priority = ? WHERE id = ?", idx+1, p.ID)
       }
       return tx.Commit()
   }
   ```
4. การเปลี่ยนแปลงในฐานข้อมูลจะยังไม่มีผลกับระบบจริง จนกว่าผู้ใช้จะกดปุ่ม **"Apply Settings"** บนหน้าจอ ซึ่งจะไปเรียก `POST /api/policies/apply` เพื่อสร้างกฎลงเคอร์เนล `nftables`

---

## 4. REST API Endpoints

ทุก endpoint ต้องผ่าน JWT/Session authentication ก่อน

| Method | Path | Handler | หน้าที่ |
|---|---|---|---|
| `GET` | `/api/policies` | `HandleGetPolicies` | ดึงกฎไฟร์วอลล์ทั้งหมดเรียงลำดับตาม Priority |
| `POST` | `/api/policies` | `HandleCreatePolicy` | สร้างกฎไฟร์วอลล์ใหม่ (คำนวณ Priority อัตโนมัติให้อยู่ล่างสุด) |
| `PUT` | `/api/policies/{id}` | `HandleUpdatePolicy` | แก้ไขรายละเอียดของกฎ (อัปเดตความสัมพันธ์ใหม่หมด) |
| `DELETE` | `/api/policies/{id}` | `HandleDeletePolicy` | ลบกฎไฟร์วอลล์และข้อมูลความสัมพันธ์ที่เชื่อมโยง |
| `PUT` | `/api/policies/reorder` | `HandleReorderPolicies` | อัปเดต Priority ของทุกกฎหลังจากลากสลับแถว |
| `POST` | `/api/policies/{id}/toggle-log` | `HandleTogglePolicyLog` | เปิด/ปิดการเก็บบันทึก Log |
| `POST` | `/api/policies/{id}/toggle-status` | `HandleTogglePolicyStatus` | เปิด/ปิดการใช้งานกฎชั่วคราว |
| `POST` | `/api/policies/apply` | `HandleApplyPolicies` | นำกฎทั้งหมดไปประมวลผลลงเคอร์เนล Linux จริง |

---

## 5. Mock Mode

| โหมด | พฤติกรรม |
|---|---|
| `mockMode = true` | ทำการเก็บข้อมูล แก้ไข และลากสลับลำดับในตารางฐานข้อมูล SQLite ตามปกติ แต่ฟังก์ชัน `ApplyRules()` จะเพียงส่งออกคำสั่ง mock log ลง Console และไม่เรียกคำสั่งเคอร์เนลใดๆ |
| `mockMode = false` (Production) | นำกฎในฐานข้อมูลทั้งหมดมาแปลงเป็น syntax ของ `nftables` และทำการล้างค่าเก่าจากนั้นเขียนทับลง Linux Kernel จริงผ่าน netlink socket |

---

## 6. ข้อควรระวัง

1. **ลำดับการประมวลผลของ nftables** — `nftables` ตรวจจับความเข้ากันตามลำดับของกฎจริง หากมีการตั้งค่าที่ทับซ้อนกัน กฎตัวบนจะถูกประมวลผลก่อนและหยุดตรวจสอบทันที (First-Match)
2. **กฎ Default Drop Fallback** — ตามหลักการทำงานของ PiGate หน้าบ้านมีนโยบายเป็นแบบปิดกั้นโดยปริยาย (Implicit Deny) ข้อมูลที่ไม่ได้เข้าข่ายกฎข้อใดเลยจะโดน DROP ทั้งหมดที่ Chain ขาเข้าและขาส่งต่อ
3. **การเปลี่ยนชื่อ Address/Service Objects** — หน้าบ้านมีฟีเจอร์ Rename Propagation (`mockSync.ts`) เมื่อมีการเปลี่ยนชื่อวัตถุระบบจะทำการค้นหาและอัปเดตชื่อในกฎของหน่วยความจำชั่วคราวก่อนกดส่งเข้าฐานข้อมูลจริง
4. **ความกว้างและการรองรับ Responsive ของตาราง** — เนื่องจากข้อมูลของกฎมีขนาดใหญ่ ตารางต้องถูกครอบด้วย `overflow-x-auto` ป้องกันข้อผิดพลาด UI หลุดเฟรมบนอุปกรณ์มือถือ

---

## 7. Kernel Integration (Production)

ไฟล์: `backend/internal/kernel/real_firewall.go` (วางแผนพัฒนาในระยะที่ 2)

ในโหมดทำงานจริง ระบบจะใช้ `github.com/google/nftables` เพื่อทำโครงสร้าง Netlink ในการประมวลผลคำสั่งเคอร์เนล โดยไม่เขียนคำสั่งเป็น shell command (เช่น `nft add rule ...`) เพื่อความปลอดภัยสูงระดับ OS

### ตัวอย่าง nftables Schema ที่จำลองขึ้นมา:
```
table inet pigate_firewall {
    chain input {
        type filter hook input priority filter; policy drop;
        ct state established,related accept
        iifname "lo" accept
        # กฎของระบบย่อยจะเข้ามาอยู่ตรงนี้...
    }
    chain forward {
        type filter hook forward priority filter; policy drop;
        ct state established,related accept
        # กฎการ Forward ระหว่าง LAN <-> WAN จะเข้ามาอยู่ตรงนี้...
    }
}
```

---

## 8. ไฟล์ที่เกี่ยวข้อง

| ไฟล์ | หน้าที่ |
|---|---|
| [`backend/internal/model/types.go`](../../../backend/internal/model/types.go) | โครงสร้าง structs ของ `PolicyRule` และ `PolicyRuleInput` |
| [`backend/internal/db/connection.go`](../../../backend/internal/db/connection.go) | DB Schema ของนโยบายไฟร์วอลล์และตัวเชื่อมตาราง |
| [`backend/internal/db/repository.go`](../../../backend/internal/db/repository.go) | ฟังก์ชัน CRUD, การเขียน priority ใหม่ และการเก็บความสัมพันธ์ลงตารางเชื่อมโยง |
| [`backend/internal/api/handlers.go`](../../../backend/internal/api/handlers.go) | ตัวประมวลผล request ของ endpoints `/api/policies` ทั้งหมด |
| [`backend/internal/kernel/interfaces.go`](../../../backend/internal/kernel/interfaces.go) | `FirewallManager` interface ที่ใช้ประกาศเมธอด `ApplyRules` |
| [`backend/internal/kernel/mock.go`](../../../backend/internal/kernel/mock.go) | ตัวจำลองสถานะ FirewallManager สำหรับสภาพแวดล้อมจำลอง |
