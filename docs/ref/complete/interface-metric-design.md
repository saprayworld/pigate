# Interface Metric Design — เพิ่มค่า Metric ในการตั้งค่า Interface

> เอกสารออกแบบสำหรับฟีเจอร์: เพิ่มฟิลด์ **Metric** ในการตั้งค่า Network Interface
> เมื่อผู้ใช้ตั้งค่า Metric ไว้ ระบบจะนำค่านี้ไปกำหนด priority ของ **default gateway route**
> (`0.0.0.0/0`) ของ interface นั้นให้โดยอัตโนมัติ — ใช้สำหรับจัดลำดับ WAN หลัก/สำรอง
> (multi-WAN failover) โดยเลขน้อยกว่า = ถูกเลือกใช้ก่อน

---

## 1. สถานะปัจจุบัน (Current State)

การเกิด default gateway route ทุกวันนี้มี **2 เส้นทาง** ที่พฤติกรรมต่างกัน:

### 1.1 Static mode
- `RealNetwork.ConfigureInterface()` (`backend/internal/kernel/real_network.go:187`)
  เมื่อ mode = `static` และมี gateway จะ:
  1. ลบ default route เดิมทั้งหมดบน link นั้น
  2. เพิ่ม default route ใหม่ด้วย **`Priority: 100` (hardcode)** — ไม่มีทางปรับได้จาก UI

### 1.2 DHCP mode
- pigate ไม่ได้เพิ่ม route เอง แต่สั่ง start `dhcpcd@<iface>.service` ผ่าน D-Bus
  (`kernel/dhcpcd.go`, `service/dhcpcd.go`) แล้ว **dhcpcd เป็นคนใส่ default route เอง**
- metric ที่ dhcpcd ใช้เป็นค่า default ของ dhcpcd เอง: `200 + if_index`
  (และ +100 สำหรับ wireless) — pigate ควบคุมค่านี้ไม่ได้เลยในปัจจุบัน

### 1.3 กลไกที่เกี่ยวข้องอื่น ๆ
- **`NetlinkMonitor`** (`service/netlink_monitor.go`) subscribe Link/Addr/Route events
  แล้ว debounce 500ms → เรียก `RoutingService.reconcileKernelRoutingTable()`
  ซึ่งเรียก `RoutingManager.ApplyRoutes(dbRoutes)` เพื่อ sync กับตาราง `static_routes` ใน DB
- **`RealRouting.ApplyRoutes`** (`kernel/real_routing.go`) จัดการเฉพาะ route ที่อยู่ใน DB
  (`static_routes`) — route ที่ dhcpcd สร้าง (ไม่อยู่ใน DB) จะไม่ถูกยุ่ง ยกเว้นเปิด flag
  `-allow-edit-system-routes`
- หน้า **Static Routes** ก็มีฟิลด์ `metric` ของตัวเองอยู่แล้ว (ตาราง `static_routes`)

---

## 2. แนวทางที่เลือก (Design Decision)

### 2.1 เก็บค่า Metric ที่ไหน
เพิ่มฟิลด์ `Metric *int` ใน `model.NetworkInterface` + คอลัมน์ `metric INTEGER` (nullable)
ในตาราง `network_interfaces`

- ใช้ **pointer (`*int`) / NULL** เพื่อแยกให้ออกว่า "ไม่ได้ตั้งค่า" กับ "ตั้งค่าเป็นเลข"
  (metric 0 เป็นค่า valid ใน Linux — ถ้าใช้ `int` ธรรมดาจะแยก 0 กับ "ไม่ตั้ง" ไม่ได้
  ซึ่งเป็น pattern เดียวกับ `IPCheckTimeout *int` ที่มีอยู่แล้ว)
- **เมื่อ Metric = nil → พฤติกรรมเดิมทุกอย่าง** (static ใช้ 100, dhcp ปล่อยตาม dhcpcd)
  เพื่อไม่ให้กระทบเครื่องที่ติดตั้งอยู่แล้ว

### 2.2 การบังคับใช้ค่า Metric (Enforcement) — แยกตาม mode

| Mode | วิธีบังคับใช้ |
|---|---|
| `static` | ส่งค่า metric เข้าไปใน `ConfigureInterface()` ตรง ๆ ตอนสร้าง default route (แทนเลข 100 hardcode) |
| `dhcp` | ให้ **NetlinkMonitor + RoutingService** เป็นคน "แก้ metric ทีหลัง" — เมื่อ dhcpcd ใส่ default route แล้วเกิด Route event → reconcile → ตรวจว่า default route ของ interface นั้นมี priority ตรงกับที่ตั้งไว้หรือไม่ ถ้าไม่ตรงให้ delete + re-add ด้วย priority ใหม่ (คงค่า proto/scope/src เดิมไว้) |

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:** เขียน `metric <n>` ลงไฟล์ `/etc/dhcpcd.conf` แบบ
per-interface แล้ว restart dhcpcd — ตัดทิ้งเพราะ:
1. `/etc/dhcpcd.conf` เป็นไฟล์ root-owned ส่วน pigate รันเป็น user ไม่มีสิทธิ์เขียน
   (ต้องแก้ install.sh เพิ่ม ownership/sudoers ซึ่งขยาย attack surface)
2. dhcpcd@.service ปัจจุบันรัน `dhcpcd -B -q %I` ใช้ config กลาง การไปยุ่งไฟล์ config
   ระบบขัดกับแนวทาง "คุมผ่าน Netlink/D-Bus เท่านั้น" ของโปรเจกต์
3. แนวทาง Netlink enforcement เข้ากับสถาปัตยกรรม self-healing ของ NetlinkMonitor
   ที่มีอยู่แล้วพอดี (dhcpcd ต่อ lease ใหม่/ใส่ route ใหม่เมื่อไหร่ monitor ก็จับแล้วแก้ให้เอง)

### 2.3 จุดที่บังคับใช้ (call sites)
1. **Startup** — `InterfaceService.InitApplyConfigurationAtStartup()` (static path ผ่าน
   ConfigureInterface) และ `RoutingService.InitApplyConfig()` / reconcile รอบแรก (dhcp path)
2. **ผู้ใช้กด Save ในหน้า Interfaces** — `ApplyInterfaceConfig()`
3. **เหตุการณ์เครือข่ายเปลี่ยน** — `NetlinkMonitor.reconcile()` (ครอบคลุมทั้ง dhcpcd
   ได้ lease ใหม่, Wi-Fi failover สลับ SSID แล้ว dhcpcd ใส่ route ใหม่, ถอด/เสียบสาย)

---

## 3. ขั้นตอนการทำงาน (Implementation Plan)

### Phase 1 — Model + Database

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/model/types.go` | เพิ่ม `Metric *int \`json:"metric,omitempty"\`` ใน struct `NetworkInterface` (วางไว้ใกล้ `Gateway`) |
| `backend/internal/db/connection.go` | (1) เพิ่ม `metric INTEGER` ใน `CREATE TABLE IF NOT EXISTS network_interfaces` (บรรทัด ~315) (2) เพิ่ม migration block ตาม pattern เดิม: อ่าน sql จาก `sqlite_master` แล้ว `if !strings.Contains(sql, "metric")` → `ALTER TABLE network_interfaces ADD COLUMN metric INTEGER` (ดู pattern ของ `subtype` บรรทัด ~157) |
| `backend/internal/db/repository.go` | เพิ่มคอลัมน์ `metric` ใน 4 จุด: `GetInterfacesFromDB()` (SELECT + Scan), `GetInterfaceByID()` (SELECT + Scan), `UpdateInterface()` (ทั้งคำสั่ง UPDATE และ INSERT fallback), และ `CreateInterfaceForTest()` ถ้าต้องใช้ในเทสต์ |

### Phase 2 — Kernel layer

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/kernel/interfaces.go` | (1) แก้ลายเซ็น `NetworkManager.ConfigureInterface(name, mode, ip, netmask, gateway string, metric int)` — ตกลง convention: `metric <= 0` หมายถึง "ไม่ตั้ง ใช้ default 100" (2) เพิ่มเมธอดใหม่ใน `RoutingManager`: `EnforceDefaultRouteMetric(ifaceName string, metric int) error` |
| `backend/internal/kernel/real_network.go` | `ConfigureInterface`: ใช้ `metric` แทน `Priority: 100` (ถ้า metric <= 0 ใช้ 100 ตามเดิม) |
| `backend/internal/kernel/real_routing.go` | implement `EnforceDefaultRouteMetric`: list route IPv4 ของ link นั้น → หา route ที่ `Dst == nil \|\| 0.0.0.0/0` และมี Gw → ถ้า `Priority != metric` ให้ copy struct, ตั้ง Priority ใหม่ **โดยคงค่า Protocol / Scope / Src / Gw เดิมไว้ทั้งหมด** แล้ว `RouteDel(เก่า)` + `RouteAdd(ใหม่)` (เปลี่ยน priority ใช้ RouteReplace ไม่ได้ เพราะ kernel มองว่าเป็นคนละ route — ดู pattern เดียวกันใน `ApplyRoutes` บรรทัด ~115) |
| `backend/internal/kernel/mock.go` | อัปเดต `MockNetwork.ConfigureInterface` ให้รับพารามิเตอร์ใหม่ และเพิ่ม `MockRouting.EnforceDefaultRouteMetric` (log อย่างเดียว) — **ถ้าลืม โค้ดจะ compile ไม่ผ่านทั้งโปรเจกต์** เพราะ interface ไม่ครบ |

### Phase 3 — Service layer

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/service/interface.go` | (1) `InitApplyConfigurationAtStartup()` และ `ApplyInterfaceConfig()`: ดึงค่า metric จาก `iface.Metric` (nil → 0) ส่งเข้า `ConfigureInterface(...)` (2) เพิ่ม validation ใน `ApplyInterfaceConfig`: ถ้า `Metric != nil` ต้องอยู่ในช่วง 1–9999 (ปฏิเสธค่าติดลบ/เกิน เพราะ Priority เป็น uint32 ฝั่ง kernel) |
| `backend/internal/service/routing.go` | เพิ่มขั้นตอนท้าย `reconcileKernelRoutingTable()`: หลัง `ApplyRoutes` เสร็จ → `repo.GetInterfacesFromDB()` → สำหรับทุก interface ที่ `Metric != nil` และ `AddressingMode == "dhcp"` → เรียก `routing.EnforceDefaultRouteMetric(iface.Name, *iface.Metric)` (RoutingService มี `repo` อยู่แล้ว ไม่ต้องเพิ่ม dependency) |
| `backend/internal/service/netlink_monitor.go` | **ไม่ต้องแก้** — reconcile ที่มีอยู่จะครอบคลุม enforcement ให้อัตโนมัติผ่าน routing.go |

### Phase 4 — API layer

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/api/handlers.go` | (1) `HandlePatchInterface` (endpoint ที่ frontend ใช้จริง): เพิ่ม `updatePtrInt("metric", &iface.Metric)` — helper รองรับ `null` → เคลียร์ค่ากลับเป็น "ไม่ตั้ง" ให้อยู่แล้ว (2) `HandleUpdateInterface` (PUT): เพิ่ม `if updates.Metric != nil { iface.Metric = updates.Metric }` เพื่อความสอดคล้อง |
| `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` | เพิ่ม property `metric` (type: integer, nullable, minimum: 1, maximum: 9999) ใน schema `NetworkInterface` — มี 2 ไฟล์ ต้องแก้ให้ตรงกันทั้งคู่ |

### Phase 5 — Frontend

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `frontend/src/data-mockup/mockData.ts` | เพิ่ม `metric?: number` ใน interface `NetworkInterface` (เพิ่มค่าตัวอย่างใน `initialNetworkInterfaces` สัก 1 ตัว เช่น WAN = 100 เพื่อทดสอบ mock mode) |
| `frontend/src/pages/Interfaces.tsx` | (1) เพิ่ม state `formMetric` (string ว่าง = ไม่ตั้ง) (2) `openEditDialog`: `setFormMetric(iface.metric != null ? String(iface.metric) : "")` (3) เพิ่ม input ใน dialog — **วางนอกกล่อง Static IP** (แสดงทุก addressing mode เพราะ metric ใช้กับ DHCP/WAN failover เป็นหลัก) เป็น field "Route Metric (ลำดับความสำคัญ Gateway)" type number, placeholder เช่น "ว่าง = อัตโนมัติ", มีคำอธิบายว่าเลขน้อย = ใช้ก่อน (4) validation ใน `handleSave`: ถ้าไม่ว่าง ต้องเป็นจำนวนเต็ม 1–9999 (5) ตอนสร้าง `updates`: `metric: formMetric === "" ? null : parseInt(formMetric)` — ส่ง `null` เพื่อเคลียร์ค่า (ต้อง cast type ให้รองรับ หรือปรับ type เป็น `metric?: number \| null`) (6) (ทางเลือก) แสดงค่า metric เป็น text เล็ก ๆ ในคอลัมน์ IP / Netmask ของตารางเมื่อมีการตั้งค่า |
| `frontend/src/services/interfaceService.ts` | **ไม่ต้องแก้** — `update()` ส่ง `Partial<NetworkInterface>` ผ่าน PATCH อยู่แล้ว ฟิลด์ใหม่ไหลผ่านอัตโนมัติ (mock mode ก็ merge ด้วย spread อยู่แล้ว) |

### Phase 6 — Tests + เอกสาร

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/service/interface_test.go` | เทสต์: save metric แล้วอ่านกลับได้, validation ช่วงค่า, metric=nil ไม่กระทบพฤติกรรมเดิม |
| `backend/internal/service/routing_test.go` | เทสต์ reconcile: interface dhcp ที่ตั้ง metric ต้องมีการเรียก `EnforceDefaultRouteMetric` (ใช้ tracker pattern ที่มีอยู่แล้วในไฟล์นี้) |
| `backend/internal/db/repository_test.go` | เทสต์ migration + CRUD คอลัมน์ metric |
| รันทั้งหมด | `cd backend && go build ./... && go test ./...` และ `cd frontend && yarn build && yarn lint` |

---

## 4. ข้อควรระวัง (Cautions)

1. **การชนกับหน้า Static Routes (สำคัญที่สุด)** — ตาราง `static_routes` มี metric ของตัวเอง
   ถ้าผู้ใช้สร้าง route `0.0.0.0/0` ใน DB ที่ชี้ gateway/interface เดียวกับ default route
   ของ interface ที่ตั้ง Metric ไว้ จะเกิด **ping-pong**: `ApplyRoutes` ดัน metric ตามค่าใน
   `static_routes` ↔ `EnforceDefaultRouteMetric` ดันกลับตามค่า interface วนไม่จบ
   (route event → reconcile → แก้ → event ใหม่ → ...)
   **ต้องกำหนด precedence ชัดเจน**: ใน `reconcileKernelRoutingTable` ให้ข้าม enforcement
   ถ้ามี active DB route `0.0.0.0/0` บน interface เดียวกันอยู่แล้ว (ให้ static_routes ชนะ)
   และ/หรือแจ้งเตือนใน UI — และเขียนเทสต์ครอบเคสนี้

2. **Enforcement ต้อง idempotent** — แก้ metric ด้วย RouteDel+RouteAdd จะ trigger Route
   event เข้า NetlinkMonitor อีกรอบ → reconcile อีกรอบ ถ้าเช็ค `Priority != metric` ก่อน
   ลงมือเสมอ รอบสองจะเป็น no-op และหยุดเอง แต่ถ้าเขียนแบบ del+add ทุกครั้งไม่เช็ค จะวน
   event loop ไม่รู้จบ

3. **ช่วงเวลาสั้น ๆ ที่ไม่มี default route** — การเปลี่ยน metric ใช้ RouteReplace ไม่ได้
   (kernel มองว่า priority ต่างกัน = คนละ route) ต้อง Del แล้ว Add ทำให้มีหน้าต่างเสี้ยว
   วินาทีที่ traffic ขาออกไม่มีทางไป — ยอมรับได้ แต่ควร Add ให้เร็วที่สุดหลัง Del และ log ไว้

4. **ต้องคงค่า route attributes เดิมตอน re-add** — Protocol (dhcp/ra/boot), Scope, Src, Gw
   ของ route ที่ dhcpcd สร้าง ต้อง copy มาครบ **ห้าม** ให้กลายเป็น proto 120 (route ที่
   pigate จัดการเอง) มิฉะนั้น `ApplyRoutes` รอบถัดไปจะมองว่าเป็น managed route ที่ไม่อยู่ใน
   DB แล้ว **ลบทิ้ง** (ดู logic `isManagedRoute` ใน `real_routing.go:88`)

5. **แก้ interface ของ `NetworkManager`/`RoutingManager` ต้องแก้ mock ด้วยเสมอ** —
   ทั้ง `real_*.go` และ `mock.go` มิฉะนั้น build พัง (main.go เลือก impl ตาม flag `-mock`)

6. **Migration SQLite** — ใช้ pattern `strings.Contains(sqlCreate, "metric")` ระวังคำว่า
   `metric` ต้องไม่ไปตรงกับ substring อื่นในตาราง `network_interfaces` (ตรวจแล้ว: ปัจจุบัน
   ไม่มีคำนี้ในสคีมา ใช้ได้) และห้ามใช้ `ALTER TABLE ... ADD COLUMN ... NOT NULL` โดยไม่มี
   DEFAULT — ให้เป็น nullable ไปเลย

7. **อย่าลืมส่งต่อค่าใน `GetDataLayerInterface()`** (`service/interface.go:302-334`) —
   ฟังก์ชันนี้ overwrite ค่าจาก DB ทับค่าจาก kernel ทีละฟิลด์ **ต้องเพิ่ม
   `kIface.Metric = dbIface.Metric`** ด้วย ไม่งั้นค่าที่บันทึกจะไม่โผล่กลับมาที่ UI
   (เป็นจุดที่พลาดง่ายที่สุดเพราะเป็น manual field copy)

8. **Zero value vs ไม่ตั้งค่า** — ใช้ `*int` ตลอดสาย (model → repo → API → frontend
   `number \| null`) อย่าแปลงเป็น `int` กลางทาง ไม่งั้นการ "เคลียร์ค่า" (ส่ง null) จะ
   กลายเป็น metric 0 ซึ่งแปลว่า priority สูงสุดใน Linux — ผลลัพธ์ตรงข้ามกับที่ผู้ใช้ตั้งใจ

9. **การเปลี่ยน IP/Gateway ระหว่างทาง** — คำเตือนเดิมในหน้า Interfaces ใช้ได้กับ metric
   ด้วย: การสลับ WAN หลักอาจทำให้ session ที่ค้างอยู่ (รวมถึง session ที่ผู้ใช้กำลังเปิด
   หน้าเว็บ pigate ผ่าน WAN นั้น) สะดุด ควรเพิ่มข้อความอธิบายใน help box

10. **โหมด `-disable-edit`** — endpoint PATCH/PUT ถูก middleware คุมอยู่แล้ว ฟีเจอร์นี้ไม่
    ต้องทำอะไรเพิ่ม แต่ตอนเทสต์บนเครื่องจริงให้ระวังว่าถ้าเปิด read-only จะเซฟไม่ได้

11. **รองรับเฉพาะ IPv4** — โค้ด routing ปัจจุบันทั้งหมดใช้ `FAMILY_V4`
    `EnforceDefaultRouteMetric` ให้ทำเฉพาะ IPv4 เช่นกัน (default route IPv6 ของ RA
    อย่าไปแตะ) และระบุไว้ใน doc/UI ว่า metric มีผลกับ IPv4 เท่านั้น

12. **Wi-Fi Failover** — เมื่อ failover สลับ SSID สำเร็จ dhcpcd จะได้ lease ใหม่และใส่
    default route ใหม่ (metric ตาม dhcpcd) — NetlinkMonitor จะจับ event และ enforce ให้
    อัตโนมัติ ไม่ต้องเขียนโค้ดพิเศษ แต่**ควรเทสต์ end-to-end บนบอร์ดจริง** ว่าจังหวะ
    debounce 500ms ไม่ทำให้ route ค้าง metric ผิดนาน

---

## 5. ลำดับการลงมือทำ (แนะนำ)

1. Phase 1 (model + DB) → build + test ผ่าน
2. Phase 2 (kernel: แก้ signature + mock ให้ compile ผ่านก่อน แล้วค่อยเขียน
   `EnforceDefaultRouteMetric` จริง)
3. Phase 3 (service + precedence rule ข้อ 4.1) → เขียนเทสต์ reconcile
4. Phase 4 (API + openapi)
5. Phase 5 (frontend) → ทดสอบกับ backend mock mode (`-mock=true`)
6. Phase 6 ทดสอบบนเครื่องจริง: static metric, dhcp metric, ถอดสาย/เสียบสาย,
   Wi-Fi failover, ตั้ง static route 0.0.0.0/0 ทับ (เคส ping-pong)
