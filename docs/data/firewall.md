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

### Address/Service Objects และตารางลูก Multi-Value (`address_object_values` / `service_object_ports`)

ตั้งแต่ฟีเจอร์ Multi-Value Address & Service Objects (ดู `docs/ref/todo/multi-value-address-service-objects-plan.md`) `address_objects`/`service_objects` แต่ละแถวสามารถมีได้หลาย entry (subnet/range/fqdn หลายค่า หรือ protocol/port หลายคู่) โดยเก็บลงตารางลูกแยกต่างหาก:

```sql
-- ตารางหลักของ Address Object (คอลัมน์ type/value คือ compat mirror ของ entry แรก — ดู Deprecation note)
CREATE TABLE IF NOT EXISTS address_objects (
    id     TEXT PRIMARY KEY,
    name   TEXT UNIQUE NOT NULL,
    type   TEXT NOT NULL CHECK(type IN ('subnet', 'range', 'fqdn')),
    value  TEXT NOT NULL,
    system INTEGER DEFAULT 0 CHECK(system IN (0, 1)),
    comment TEXT
);

-- ตารางหลักของ Service Object (คอลัมน์ protocol/port คือ compat mirror ของ entry แรก — ดู Deprecation note)
CREATE TABLE IF NOT EXISTS service_objects (
    id       TEXT PRIMARY KEY,
    name     TEXT UNIQUE NOT NULL,
    protocol TEXT NOT NULL CHECK(protocol IN ('TCP', 'UDP', 'TCP/UDP', 'ICMP')),
    port     TEXT NOT NULL,
    type     TEXT NOT NULL CHECK(type IN ('system', 'custom')),
    comment  TEXT
);

-- ตารางลูก: entry ทั้งหมดของ Address Object แต่ละตัว (source of truth)
CREATE TABLE IF NOT EXISTS address_object_values (
    address_id TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    type       TEXT NOT NULL CHECK(type IN ('subnet', 'range', 'fqdn')),
    value      TEXT NOT NULL,
    PRIMARY KEY (address_id, seq),
    FOREIGN KEY (address_id) REFERENCES address_objects(id) ON DELETE CASCADE
);

-- ตารางลูก: entry ทั้งหมดของ Service Object แต่ละตัว (source of truth)
CREATE TABLE IF NOT EXISTS service_object_ports (
    service_id TEXT NOT NULL,
    seq        INTEGER NOT NULL,
    protocol   TEXT NOT NULL CHECK(protocol IN ('TCP', 'UDP', 'TCP/UDP', 'ICMP')),
    port       TEXT NOT NULL,
    PRIMARY KEY (service_id, seq),
    FOREIGN KEY (service_id) REFERENCES service_objects(id) ON DELETE CASCADE
);
```

จำนวน entry ต่อ object ถูกจำกัดด้วยคีย์ `max-object-entries` ใน `pigate.conf` (default 64) และจำนวนกฎ nftables ที่ขยายออกมาต่อ policy ถูกจำกัดด้วย `max-expanded-rules-per-policy` (default 4096) — ทั้งสองเป็น file-only key (ไม่มี CLI flag คู่กัน)

**Deprecation note**: คอลัมน์ `address_objects.type`/`address_objects.value` และ `service_objects.protocol`/`service_objects.port` เป็น **compat layer ชั่วคราว** ที่ mirror มาจาก entry แรก (`seq=1`) ของตารางลูกเท่านั้น และ **ต้องไม่ถูกอ่านเพื่อสร้างกฎไฟร์วอลล์อีกต่อไป** (โค้ดปัจจุบันอ่านจากตารางลูกเสมอ) เหตุผลที่ยังต้องเก็บคอลัมน์เหล่านี้ไว้ชั่วคราวแทนที่จะลบทิ้งทันที:
1. SQLite รุ่นเก่าที่ยังพบใน field ไม่รองรับ `DROP COLUMN` แบบตรงไปตรงมา (ต้องสร้างตารางใหม่ทั้งตารางแล้ว copy ข้อมูล ซึ่งมีความเสี่ยงสูงกว่า)
2. คอลัมน์เหล่านี้เป็น `NOT NULL` พร้อม `CHECK` constraint ที่ผูกกับ schema เดิม การลบทันทีจะกระทบ backward-compat ของ backup/restore ที่ยังไม่ผ่านช่วงเปลี่ยนผ่าน
3. ต้องเผื่อกรณี downgrade กลับไปใช้ binary รุ่นก่อนหน้าฟีเจอร์นี้ระหว่างช่วงเปลี่ยนผ่าน — บินารีรุ่นเก่ายังอ่าน `type`/`value`/`protocol`/`port` ได้ตามปกติ

คอลัมน์ทั้งสี่นี้มีแผนถูกลบออกใน major version ถัดไป เมื่อมั่นใจว่าทุก deployment ผ่านช่วงเปลี่ยนผ่านแล้ว

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
| `GET` | `/api/policies/{id}/endpoints` | `HandleGetPolicyRuleEndpoints` | Top IP/Service ที่ตรงกับกฎนี้ (aggregate สดจาก traffic-log ring buffer) — ดูหัวข้อ 10 |

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

---

## 9. Per-Rule Usage Statistics (`GET /api/policies/stats`)

ไฟล์หลัก: `backend/internal/service/policy_stats.go`, `backend/internal/service/traffic_stats.go`, `backend/internal/logs/ringbuffer.go`, `frontend/src/services/policyStatsService.ts`, `frontend/src/components/policy/PolicyChainPage.tsx`, `frontend/src/components/policy/RuleStatsDrawer.tsx`

หน้ารายการกฎ (Firewall/Local-In/Local-Out Policy) มีคอลัมน์ "Usage" และปุ่มดูรายละเอียดสถิติต่อกฎ (ดู `docs/ref/todo/firewall-policy-rule-usage-stats-plan.md` สำหรับ work plan เต็ม) โดย `PolicyStatsService.GetPolicyRuleStats(chain)` รวม 3 แหล่งข้อมูลที่มีอยู่แล้วเข้าด้วยกัน (ไม่เพิ่ม goroutine/kernel call ใหม่):

1. **nft counter snapshot** — จาก `TrafficStatsService.RuleCounterSnapshot()` (ตัวเลข cumulative นับตั้งแต่ poller เห็นกฎนั้นครั้งแรก, reset ทุกครั้งที่มีการ `Apply Settings` เพราะ `RealFirewall.ApplyRules` ทำ `FlushTable` แล้วสร้างกฎใหม่ทั้งหมด)
2. **Traffic log ring buffer** — `RingBuffer.LastMatchedByRule()` สแกนรอบเดียว หา "ใช้งานล่าสุดเมื่อ" ที่แม่นยำสำหรับกฎที่เปิด `Log`
3. **Poll-based fallback** — `TrafficStatsService.RuleLastHits()` (บันทึกเวลาที่ delta > 0 ในรอบ poll ทุก ~10 วินาทีเดิม) สำหรับกฎที่ไม่เปิด `Log`

ข้อจำกัดสำคัญ (ต้องอ่านก่อนตีความตัวเลข):
- เป็น **snapshot ตั้งแต่ Apply ล่าสุด** ไม่ใช่สะสมตลอดชีพของกฎ — ดู `countersSince` ในผลลัพธ์
- `percent`/`totalBytes`/`totalPackets` คำนวณข้าม **ทุก chain รวมกันเสมอ** แม้ใช้ `?chain=` กรองผลลัพธ์
- `lastMatchedSource` เป็น `"log"` หรือ `"counter"` ตามแหล่งที่ resolve ได้ (ดูข้อ 2/3 ด้านบน) ความคลาดเคลื่อนของแหล่ง `"counter"` อยู่ที่ ±10 วินาที
- กฎที่ `status = false` (Disabled) จะไม่ปรากฏใน `rules` เลย เพราะไม่ถูกสร้างใน nftables — ฝั่งหน้าบ้านแสดง "—" แทน 0/Unused
- Endpoint นี้เป็น `authRoute` (ทุก role ที่ล็อกอินเรียกได้) เหมือนกลุ่ม `/api/statistics/*` เพราะข้อมูลอ่อนไหวต่ำ (มีแค่ rule id + byte count)

---

## 10. Matched Endpoints ต่อกฎ (`GET /api/policies/{id}/endpoints`)

ไฟล์หลัก: `backend/internal/service/policy_endpoints.go`, `backend/internal/service/policy_endpoint_labels.go`, `backend/internal/logs/ringbuffer.go` (`AggregateByRule`), `backend/internal/service/traffic_stats.go` (`ServiceNameFor`), `backend/internal/api/handlers.go` (`HandleGetPolicyRuleEndpoints`), `frontend/src/services/policyEndpointsService.ts`, `frontend/src/components/policy/RuleStatsDrawer.tsx`

งานนี้ตอบคำถาม "กฎนี้มี IP/Service อะไรบ้างที่โดนมันจริงๆ" สำหรับ troubleshoot — เพิ่มต่อจากหัวข้อ 9 (bytes/packets รวม) ในส่วน "Endpoints ที่ตรงกับกฎนี้" ของ `RuleStatsDrawer` ดู work plan เต็มที่ `docs/ref/todo/firewall-rule-matched-endpoints-plan.md` (Issue #134/#136 เสร็จสิ้น, PR ที่ merge เข้า `main`)

### แหล่งข้อมูลและวิธีคำนวณ

nftables counter รวมได้แค่ bytes/packets ต่อกฎ **แยกราย IP/port ไม่ได้เลย** ถ้าไม่รื้อไปทำ named-set + per-element counter (ซึ่งยังคืนแค่ element ที่ตั้งไว้ ไม่ใช่ IP จริงที่วิ่งเข้ามา) แหล่งข้อมูลเดียวที่รู้ว่า "แพ็กเก็ตไหนโดนกฎไหน" คือ **traffic-log ring buffer** (`internal/logs/ringbuffer.go`)

- **Option A ที่เลือกใช้**: สแกน ring buffer สดทุกครั้งที่มี request ผ่าน `RingBuffer.AggregateByRule(ruleID)` (สแกนครั้งเดียวใต้ `RLock`, ไม่เพิ่ม goroutine/ticker, ไม่ persist อะไรเพิ่ม) — trade-off ที่ยอมรับคือหน้าต่างข้อมูลเท่ากับความจุ ring buffer ปัจจุบัน (ปรับได้ผ่าน `pigate.conf`, ดู `docs/ref/todo/firewall-log-buffer-capacity-plan.md`)
- หน่วยนับคือ **"จำนวน log entry"** ไม่ใช่ bytes (NFLOG payload ไม่มีขนาดแพ็กเก็ต)
- `PolicyStatsService.GetRuleEndpoints(ruleID, limit)` ทำตามลำดับ: หา rule จาก DB → `AggregateByRule` → ตัด top-N ต่อหมวด (เรียง count desc, tie-break ด้วย key asc) → resolve ชื่อแบบ batch (ไม่เรียกต่อแถว)

### การเลือกชื่อที่แสดงต่อ IP/Service (ต่างจากหน้า Traffic Log โดยตั้งใจ)

ลำดับความสำคัญ: **`addressName` (Address Object ที่ผู้ใช้ตั้งเอง) ชนะเสมอ → `domain` (DNS reverse cache) → `hostname` (DHCP lease)** — ตรงข้ามกับหน้า Traffic Log ที่ domain มาก่อน hostname และไม่รู้จัก Address Object เลย เพราะโจทย์ของฟีเจอร์นี้คือ "ชื่อที่ผู้ใช้ตั้งไว้เอง" ไม่ใช่ "ชื่อที่อินเทอร์เน็ตรู้จัก"

- **IP → Address Object**: `addrMatcher` (pure function, `policy_endpoint_labels.go`) จับคู่เฉพาะ type `subnet`/`range` (ข้าม `fqdn` เพราะเป็นชื่อโดเมนไม่ใช่ช่วง IP) เลือก **prefix/range แคบสุดชนะ** เมื่อซ้อนกัน (tie-break ด้วยชื่อ ascii) ค่า config ที่ parse ไม่ผ่านถูกข้ามเงียบๆ ไม่ error ทั้ง request
- **Port → Service Object**: `TrafficStatsService.ServiceNameFor(proto, port)` ใช้ matcher ตัวเดิมกับ Dashboard (`categorize`) ห้ามเขียนซ้ำ คืน `""` แทน `"Other"` เมื่อไม่ match (ฝั่ง UI จะโชว์ `PROTO/PORT` ดิบแทน)
- `fromRule=true` เมื่อ Address Object/Service Object ที่ match นั้นเป็นตัวที่กฎนี้เองอ้างถึงใน `Source`/`Destination`/`Service` (ช่วย troubleshoot "โดนเพราะ object ตัวไหนที่ฉันตั้ง")

### ข้อจำกัดสำคัญ (ต้องอ่านก่อนตีความตัวเลข)

1. **ต้องเปิด Log ที่กฎนั้นเท่านั้น** — กฎที่ `log=false` จะตอบ `logEnabled: false` พร้อมลิสต์ว่างเสมอ (ไม่ใช่ error) นี่คือข้อจำกัดพื้นฐานที่แก้ไม่ได้ด้วยวิธีอื่นนอกจากบังคับทุกกฎ log (ผลข้างเคียงด้าน performance/privacy สูงเกินไป)
2. **นับเป็นจำนวน log entry ไม่ใช่ bytes** — bytes/packets รวมต่อกฎมีอยู่แล้วในหัวข้อ 9
3. **หน้าต่างข้อมูลเท่ากับ ring buffer ปัจจุบัน** — กฎที่ทราฟฟิกน้อยอาจถูกกฎที่ทราฟฟิกเยอะดันตกออกจากบัฟเฟอร์ก่อนเปิดดู และการ **ล้าง Traffic Log ทำให้ข้อมูลนี้หายทันที** เช่นกัน (เป็น privacy feature ที่ตั้งใจคงไว้)
4. **เห็นเฉพาะ connection ใหม่และแพ็กเก็ตที่ถูก DROP** — พฤติกรรม NFLOG เดียวกับหน้า Traffic Log (`ct state established,related accept` อยู่ก่อนจุด log) ไม่ใช่ทุกแพ็กเก็ตที่วิ่งผ่าน
5. `limit` (query param): default 10, ต้องอยู่ในช่วง 1–50 (นอกช่วง/ไม่ใช่ตัวเลข → 400 ต่างจาก endpoint อื่นในระบบที่มักจะ clamp เงียบๆ)
6. rule id ที่ไม่มีจริง → 404; rule ที่ Disabled ยังตอบ 200 ได้ (อาจมี log เก่าค้างในบัฟเฟอร์) — ฝั่ง UI ต้องสื่อว่าเป็นข้อมูลย้อนหลัง
7. ICMP: `port` เป็น `"-"` ตามที่ NFLOG parser ใส่มา ไม่ถูกแปลงเป็น `"0"`
8. Endpoint นี้เป็น `authRoute` (GET เท่านั้น จึงยังใช้งานได้ภายใต้ `-disable-edit=true`) เหมือนหัวข้อ 9 และไม่แตะ `real_firewall.go` เลย (read/aggregate ล้วน)

### Deep-link ไปหน้า Traffic Log ("ดู log ของกฎนี้")

`RuleStatsDrawer` มีปุ่ม/ลิงก์ระดับหัวข้อและระดับแถว (IP/Service) ที่พาไปหน้า Traffic Log พร้อม filter สำเร็จรูป:

- กฎ `chain=forward` → `/logs/traffic?q=<ชื่อกฎ>`; กฎ `chain=input`/`output` → `/logs/local?q=<ชื่อกฎ>&chain=input|output`
- เพื่อให้ deep-link ด้วยชื่อกฎใช้งานได้ ต้องขยาย backend ก่อน: `GET /api/logs/traffic` พารามิเตอร์ `q` ตอนนี้ค้นหาครอบคลุม **`ruleName`/`ruleId`** เพิ่มจากเดิม (`src/dest/srcPort/port/proto/inIface/outIface/reason/chain`) — ฝั่ง client (`TrafficLogPage.tsx` ฟังก์ชัน `matchesFilter`) ต้องขยาย haystack ให้ตรงกันเป๊ะ (lockstep) ไม่งั้นแถวที่มาทาง SSE จะรั่วข้ามตัวกรอง
- เป็นการค้นหาแบบ **substring บนชื่อกฎ ณ ตอนที่บันทึก log (snapshot-on-write)** — กฎที่ถูกเปลี่ยนชื่อภายหลังจะหาแถวเก่าไม่เจอ และชื่อที่เป็น substring ของกฎอื่นอาจติดมาด้วย (ข้อความนี้แสดงอยู่ใต้ปุ่มใน UI ด้วย)
- ทุกค่าที่ประกอบเป็น URL query ผ่าน `encodeURIComponent` เสมอ

ฝั่งหน้าบ้าน `PolicyChainPage.tsx` เรียก `policyStatsService.getStats(chain)` (ผ่าน `frontend/src/services/policyStatsService.ts`) ทุก ~10 วินาทีอิสระจากการโหลดตารางหลัก (ไม่ทำให้ตารางกระพริบ/รีเซ็ต scroll) และแสดงรายละเอียดครบทุก field ผ่าน `RuleStatsDrawer.tsx` เมื่อกดปุ่มไอคอน (`Activity`) ในแต่ละแถว
