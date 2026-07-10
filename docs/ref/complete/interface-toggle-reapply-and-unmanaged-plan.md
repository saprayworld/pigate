# Interface Toggle Reapply + Unmanaged Indicator — แยก configure/activate และป้ายสถานะ unmanaged

> เอกสารแผนงานสำหรับ 2 เรื่องที่เกี่ยวพันกัน: (1) แก้บั๊กเส้นทาง toggle interface
> ที่ไม่ reapply config (static gateway/metric หายหลังปิด-เปิด) และแก้พฤติกรรม
> "กด Save แล้ว interface ถูกเปิดขึ้นเองทั้งที่ปิดไว้" โดยแยกการ configure ออกจาก
> การ activate; (2) เพิ่มป้าย **Unmanaged** ใน column Status ของหน้า Interfaces
> สำหรับ interface ที่ไม่มี config ใน database (แสดงเฉยๆ ไม่เปลี่ยน logic ใดๆ)
>
> วันที่เขียน: 2026-07-09 · Branch อ้างอิง: `main` (commit `3db7a81`)

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (พฤติกรรมที่ผู้ใช้เห็น):**

1. กด switch เปิด interface ในตาราง → config จาก DB (static IP, default gateway
   route, metric) ถูก reapply กลับเข้า kernel ครบ ไม่ใช่แค่ `ip link set up`
2. กด Save ในหน้า Edit ขณะ interface ปิดอยู่ → config ถูกบันทึก/เตรียมไว้
   แต่ interface **ยังปิดอยู่เหมือนเดิม** — การเปิด/ปิดทำได้ทาง switch เท่านั้น
3. Interface ที่ปรากฏใน kernel แต่ไม่มี row ใน DB → มี badge **UNMANAGED**
   ต่อท้าย UP/DOWN ใน column Status (บอกว่า pigate ยังไม่เคยตั้งค่า iface นี้)

**เงื่อนไขทางเทคนิค:** เส้นทาง toggle ต้องกลับมาวิ่ง api → service → kernel
ตาม layering ของโปรเจกต์ (ปัจจุบัน handler ข้าม service ไปเรียก kernel ตรง)

**นอกขอบเขต:**
- ไม่ย้าย/ไม่เพิ่ม switch เปิด-ปิดใน Edit drawer (ตัดสินใจแล้วว่าคงไว้ที่ตารางที่เดียว)
- ไม่ disable ปุ่ม switch/Edit ของ interface ที่ unmanaged — logic ทุกอย่างเหมือนเดิม
- ไม่แก้ปัญหา race ของ wireless+static (route ลงไม่ได้จนกว่า wpa จะ associate
  สำเร็จ) — บันทึกเป็น known limitation แบบ non-fatal log เหมือนที่ boot ทำอยู่
- ไม่แตะ PUT `/api/interfaces/{id}` เกินจำเป็น (frontend ใช้ PATCH เท่านั้น)

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Frontend switch → API | เสร็จแล้ว ไม่ต้องแก้ | `Interfaces.tsx:819-822` → `handleToggleStatus` (:430) → `interfaceService.ts:149` `POST /interfaces/{id}/toggle` |
| Route + auth | มีแล้ว | `router.go:54` เป็น `authRoute` (mutation ถูก `RoleReadOnlyMiddleware`/`DisableEditMiddleware` คุมอยู่แล้ว) |
| Toggle handler | **บั๊ก** — เรียก `s.network.ToggleInterface` (kernel) ตรง ข้าม service layer แล้ว `repo.UpdateInterface` ทั้งก้อน (ค่า runtime จาก kernel ปนลง DB) | `handlers.go:680-709` |
| Kernel `ToggleInterface` | ทำแค่ `LinkSetUp/Down` + start/stop `wpa_supplicant@` ผ่าน D-Bus | `real_network.go:38-99` |
| Kernel `ConfigureInterface` | **ต้นเหตุ Save-แอบเปิด** — บังคับ `LinkSetUp` ถ้า link down (:193-197) ก่อน flush addr + ลง static IP + default route (metric ~:244-260) | `real_network.go:186-264` |
| Kernel `ConfigureWifi` | เขียน config atomic แล้ว: service active → `RECONFIGURE` ทาง socket; ไม่ active → **start service เสมอ** (แม้ link down → wpa ดัน link up เอง) | `real_network.go:102-183` |
| Service `ApplyInterfaceConfig` | ConfigureWifi → ConfigureInterface → `repo.UpdateInterface`; ไม่มี logic สถานะ | `interface.go:381-439` |
| Startup apply | ConfigureWifi → ConfigureInterface → ToggleInterface ตาม status ใน DB — **ลำดับนี้พึ่งพา force-up ใน ConfigureInterface** (route ลงตอน link ยัง down ไม่ได้) | `interface.go:27-108` |
| Netlink monitor | reconcile เฉพาะตาราง `static_routes` + enforce metric เฉพาะ **dhcp** (`routing.go:447` ข้าม non-dhcp) → gateway ของ static interface ไม่มีใคร reapply | `netlink_monitor.go:168`, `routing.go:407-458` |
| dhcpcd re-lease | มีแล้ว — `HandleLinkUpdate` stop/start dhcpcd ตาม link event | `dhcpcd.go:49-169` |
| Repo | `UpdateInterface` (:1727) เป็น upsert; `ToggleInterfaceStatus` (:1771) อัปเดตเฉพาะ status **มีอยู่แล้วแต่ไม่มีใครเรียกใช้** | `repository.go` |
| จุด merge kernel/DB | `GetDataLayerInterface` มีตัวแปร `exists` บอกอยู่แล้วว่า iface มี row ใน DB หรือไม่ — จุดใส่ค่า managed พอดี | `interface.go:300-359` (:307) |
| Model | ยังไม่มี field managed | `model/types.go:128-160` |
| Frontend type + Status cell | ยังไม่มี managed; Status cell มี UP / OFFLINE / DOWN | `mockData.ts:251-285`, `Interfaces.tsx:776-790` |
| openapi (2 ไฟล์) | มี `/interfaces` (:416) และ `/interfaces/{id}/toggle` (:594) แล้ว — schema ยังไม่มี `managed` | `docs/openapi.yaml`, `frontend/public/openapi.yaml` |
| จุดที่ผลิต unmanaged อยู่แล้ววันนี้ | `HandleResetInterface` (:774) เรียก `FlushInterfaceConfig` = **ลบ row ทิ้งจาก DB** → หลัง Reset iface กลายเป็น unmanaged โดยไม่มีตัวบอก | `handlers.go:774-796`, `interface.go:442-444` |

**สรุป:** งานจริงกระจุกอยู่ที่ `real_network.go` (2 ฟังก์ชัน), `interface.go`
(service ใหม่ 1 ตัว + startup + managed flag), `handlers.go` (toggle handler),
ส่วน frontend เป็นงานแสดงผลล้วน ๆ ไม่มี migration DB

---

## 2. แนวทางเทคนิค

**หลักคิด: `status` ใน DB คือ desired state** (startup ก็ตีความแบบนี้อยู่แล้วที่
`interface.go:99-104`) — "configure" กับ "activate" ต้องเป็นคนละการกระทำ:

1. **`ConfigureInterface` เลิกบังคับ link up** — ถ้า link down: flush + ลง IP
   address ได้ตามปกติ (netlink `AddrAdd` ทำงานบน link down ได้) แต่**ข้ามการลง
   gateway route** (kernel ปฏิเสธ network unreachable และต่อให้ลงได้ kernel ก็ลบทิ้ง
   ตอน down อยู่ดี) แล้ว log บอก — ไม่เปลี่ยน signature จึงไม่กระทบ `mock.go`
2. **`ConfigureWifi` start service เฉพาะเมื่อ link up** — เขียน config file เสมอ
   (ให้ persistence ทำงาน), `RECONFIGURE` เมื่อ service active เหมือนเดิม, แต่ branch
   start service เพิ่มเงื่อนไขเช็ค link flags ก่อน — กัน Save ตอนปิดแล้ว wpa ดัน link up
3. **เพิ่ม `InterfaceService.SetInterfaceState(iface, up)`** — ทางเดียวที่เปลี่ยนสถานะ:
   - ขา **up**: `kernel.ToggleInterface(name, true)` → `kernel.ConfigureInterface(...)`
     reapply IP/route/metric (route fail = log warning ไม่ fail ทั้งคำขอ — pattern
     เดียวกับ startup) → `repo.ToggleInterfaceStatus(id, "up")`
   - ขา **down**: `kernel.ToggleInterface(name, false)` → `repo.ToggleInterfaceStatus(id, "down")`
   - ไม่ต้องเรียก `ConfigureWifi` ในขา up — config file ค้างบนดิสก์อยู่แล้ว และ
     `ToggleInterface` ขา up start `wpa_supplicant@` ให้เอง (`real_network.go:71-80`);
     DHCP ก็มี dhcpcd monitor จัดการอยู่แล้ว
4. **สลับลำดับใน startup**: ConfigureWifi → **ToggleInterface → ConfigureInterface**
   (เดิม Configure ก่อน Toggle ซึ่งพึ่ง force-up ที่เรากำลังถอดออก)
5. **Managed flag เป็นค่า computed ไม่เก็บ DB**: เพิ่ม `Managed bool json:"managed"`
   ใน model แล้วเซ็ตจาก `exists` ใน `GetDataLayerInterface` — ไม่มี migration

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
- *ย้าย switch ไปไว้ในฟอร์ม Edit* — ตัดทิ้ง: เปิด/ปิดเป็น operation กดเร็วครั้งเดียว
  คนละจังหวะกับแก้ config และ router UI ทั่วไป (pfSense/OPNsense) ก็วางไว้ที่ตาราง
- *ให้ขา toggle-up เรียก `ApplyInterfaceConfig` เต็มตัว* — ตัดทิ้ง: จะ rewrite
  wpa config + restart service ทุกครั้งที่เปิด ทำ Wi-Fi reconnect โดยไม่จำเป็น
- *เก็บ managed ลง DB* — ตัดทิ้ง: derive จากการมี row ได้ตรง ๆ เก็บซ้ำมีแต่จะ drift
- *ให้ netlink monitor reapply gateway ของ interface* — ตัดทิ้ง: monitor มีกติกา
  precedence กับ `static_routes` อยู่แล้ว (`routing.go:427-431`) เพิ่ม enforcement
  ซ้อนเสี่ยง del/add ping-pong; จุด reapply ที่ถูกคือขา toggle-up ซึ่งรู้จังหวะแน่นอน

**Template ในโค้ดเดิมที่ใช้ตาม:** loop ของ `InitApplyConfigurationAtStartup`
(`interface.go:45-105`) สำหรับลำดับ apply + สไตล์ non-fatal warning log

---

## 3. ขั้นตอน (เรียงตาม dependency: model → kernel → service → api → docs → frontend)

### Step 1 — เพิ่ม field `Managed` ใน model
**File:** `backend/internal/model/types.go` (~:143 ใต้ `Status`)
```go
Status  string `json:"status"`  // "up", "down"
Managed bool   `json:"managed"` // true = มี config row ใน DB (pigate เคยตั้งค่า)
```

### Step 2 — `ConfigureInterface` เลิก force-up
**File:** `backend/internal/kernel/real_network.go:192-197`
ลบ block `LinkSetUp` ทิ้ง แล้วจำสถานะ link ไว้ใช้ตัดสินใจเรื่อง route:
```go
linkIsUp := link.Attrs().Flags&net.FlagUp != 0
// ... flush + AddrAdd ตามเดิม ...
if gateway != "" {
    if !linkIsUp {
        log.Printf("[RealNetwork] %s is down; deferring gateway route until toggled up", name)
    } else { /* ลง route ตามเดิม */ }
}
```
> `mock.go:71-75` ไม่ต้องแก้ — signature ไม่เปลี่ยน และ mock เป็น no-op อยู่แล้ว

### Step 3 — `ConfigureWifi` start service เฉพาะเมื่อ link up
**File:** `backend/internal/kernel/real_network.go:146-181` (branch `else` ของ `isActive`)
เพิ่มเช็ค `netlink.LinkByName(name)` + `FlagUp` ก่อน `StartServiceViaDBus`;
ถ้า link down → log แล้ว return nil (config file เขียนเสร็จแล้ว เพียงพอ)

### Step 4 — เพิ่ม `SetInterfaceState` ใน service + managed flag + startup order
**File:** `backend/internal/service/interface.go`
- ฟังก์ชันใหม่ (วางใต้ `ApplyInterfaceConfig` ~:440):
```go
func (s *InterfaceService) SetInterfaceState(iface model.NetworkInterface, up bool) error {
    if err := s.network.ToggleInterface(iface.Name, boolStatus); err != nil { return err }
    if up {
        metric := 0; if iface.Metric != nil { metric = *iface.Metric }
        if err := s.network.ConfigureInterface(iface.Name, iface.AddressingMode,
            iface.IP, iface.Netmask, iface.Gateway, metric); err != nil {
            log.Printf("[Interface] Warning: reapply config for %s failed: %v", iface.Name, err)
        }
    }
    return s.repo.ToggleInterfaceStatus(iface.ID, statusStr)
}
```
- `GetDataLayerInterface`: เซ็ต `kIface.Managed = exists` (สาขา `exists` ที่ :309
  และ `false` ในสาขา else :343)
- `InitApplyConfigurationAtStartup` (:89-104): สลับให้ `ToggleInterface` มาก่อน
  `ConfigureInterface` (เฉพาะเมื่อ status = up; ถ้า down ให้ Toggle down แล้วข้าม
  การลง route โดยธรรมชาติจาก Step 2)

### Step 5 — Toggle handler วิ่งผ่าน service + เลิกให้ Save แตะ status
**File:** `backend/internal/api/handlers.go`
- `HandleToggleInterface` (:680-709): แทน `s.network.ToggleInterface` +
  `s.repo.UpdateInterface` ด้วย `s.interfaceService.SetInterfaceState(*iface, nextStatus == "up")`
- `HandlePatchInterface`: ลบ `updateString("status", &iface.Status)` (:616)
- `HandleUpdateInterface` (PUT): ลบ `iface.Status = updates.Status` (:465)
> ผล: `ApplyInterfaceConfig` จะเขียน status ปัจจุบันจาก data layer (ค่าจริงจาก
> kernel) ลง DB ตอน Save — ไม่มีทางที่ Save จะเปลี่ยนสถานะ link อีก

### Step 6 — Test ฝั่ง service
**File:** `backend/internal/service/interface_test.go`
`trackingNetworkManager` (:20) มีอยู่แล้ว — เพิ่ม test: (1) `SetInterfaceState(up)`
เรียก Toggle **ก่อน** Configure, (2) ขา down ไม่เรียก Configure, (3) `Managed`
เป็น true/false ถูกต้องตามการมี row ใน DB

### Step 7 — openapi ทั้งสองไฟล์ (ต้อง sync กัน)
**Files:** `docs/openapi.yaml` + `frontend/public/openapi.yaml`
- schema `NetworkInterface` (~:416 area): เพิ่ม property `managed: boolean`
- `/interfaces/{id}/toggle` (:594): อัปเดต description ว่าขา up จะ reapply
  configuration (IP/gateway/metric) จาก database ด้วย

### Step 8 — Frontend: type + badge + mock data
**Files:**
- `frontend/src/data-mockup/mockData.ts:265`: เพิ่ม `managed?: boolean` และใส่
  `managed: true` ให้ `initialNetworkInterfaces` ทุกตัว ยกเว้นตั้ง 1 ตัวเป็น
  `false` ไว้ทดสอบ badge ในโหมด mock
- `frontend/src/pages/Interfaces.tsx:776-790` (Status cell): ต่อท้าย badge เมื่อ
  `iface.managed === false` — โทนกลางตาม semantic vars (flat, ไม่มี shadow):
```tsx
{iface.managed === false && (
  <Badge variant="outline" className="ml-1 rounded border-border bg-muted/50 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
    UNMANAGED
  </Badge>
)}
```
> ใช้เงื่อนไข `=== false` ไม่ใช่ falsy — กัน badge เด้งขึ้นกับข้อมูล mock เก่าใน
> localStorage ที่ไม่มี field นี้ (`undefined` ต้องถือว่า managed)

> **สิ่งที่งานนี้ไม่ต้องทำ:** ไม่มี migration/ตารางใหม่ (managed เป็นค่า computed),
> ไม่แตะ `install.sh`/Polkit (ไม่มี privilege ใหม่ — netlink/D-Bus เดิมทั้งหมด),
> ไม่แตะ Backup/Restore (schema interfaces เดิม), ไม่แตะ firewall rule generation

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| POST | `/api/interfaces/{id}/toggle` | authRoute (mutation → super_admin) | **เปลี่ยน**: ขา up reapply config; persist ด้วย `ToggleInterfaceStatus` แทน upsert ทั้งก้อน |
| PATCH | `/api/interfaces/{id}` | authRoute | **เปลี่ยน**: ไม่รับ/ไม่เปลี่ยน `status` อีก; Save ไม่เปิด interface ที่ปิดอยู่ |
| PUT | `/api/interfaces/{id}` | authRoute | เปลี่ยนเหมือน PATCH (ตัดการ set status) |
| GET | `/api/interfaces` | authRoute | **เพิ่ม field** `managed` ใน response (ไม่มี breaking change) |

ทุก route มีอยู่แล้ว ไม่มี route ใหม่ · `-disable-edit=true` บล็อก mutation
ทั้งหมดผ่าน `DisableEditMiddleware` เหมือนเดิม ไม่ต้องแก้

---

## 5. ข้อควรระวัง

1. **ลำดับ startup ต้องสลับพร้อมกันใน commit เดียวกับ Step 2** — ถ้าถอด force-up
   ออกจาก `ConfigureInterface` แต่ startup ยังเรียง Configure→Toggle: interface ที่
   link ยัง down ตอน boot จะไม่ได้ gateway route เลย → บอร์ดไม่มีเน็ตหลัง reboot
2. **Route add อาจ fail แม้ link up แล้ว (no-carrier)** — ethernet ไม่เสียบสาย หรือ
   wireless ที่ยังไม่ทัน associate: `RouteAdd` ตอบ network unreachable → ต้องเป็น
   non-fatal warning (ตาม pattern startup) ไม่ใช่ return 500 ให้ toggle ทั้งคำขอ fail
   ทั้งที่ link up สำเร็จแล้ว; wireless+dhcp ไม่กระทบ (dhcpcd ลง route ให้ตอนได้ lease)
3. **Wireless + static เป็น known limitation** — route จะลงได้ก็ต่อเมื่อ carrier มา
   (wpa เชื่อมสำเร็จ) ซึ่งเกิดหลัง toggle-up ตอบกลับไปแล้ว; boot ปัจจุบันก็มีข้อจำกัด
   เดียวกัน บันทึกไว้ใน docs พอ ไม่แก้ในงานนี้ (แก้จริงต้องรอ event ใน monitor)
4. **พฤติกรรม toggle บน interface ที่ unmanaged เปลี่ยน** — เดิม `UpdateInterface`
   (upsert) จะแอบสร้าง row ให้ (กลายเป็น managed เงียบ ๆ พร้อมค่า runtime ปน);
   ใหม่ `ToggleInterfaceStatus` กระทบ 0 rows → ยังคง unmanaged และสถานะไม่ persist
   ข้าม reboot — **ตั้งใจ**: pigate ไม่ควรจำ state ของสิ่งที่ตัวเองไม่ได้ตั้งค่า
   (การ adopt เข้า DB เกิดจากการกด Save ในหน้า Edit ซึ่ง upsert อยู่แล้ว)
5. **อย่าผูก badge unmanaged กับ status** — สองแกนนี้อิสระต่อกัน (unmanaged ได้ทั้ง
   UP และ DOWN); และหลังกด **Reset** interface จะกลายเป็น unmanaged ทันที
   (`FlushInterfaceConfig` ลบ row) — เป็นพฤติกรรมที่ถูกต้องและ badge ช่วยสื่อสารพอดี
6. **Netlink monitor จะตื่นจาก event ที่เราสร้างเอง** ตอน toggle (link/addr/route
   เปลี่ยน) → `reconcile()` วิ่งหลัง debounce — ไม่อันตราย: `enforceInterfaceMetrics`
   ข้าม static (`routing.go:447`) และ `ApplyRoutes` จัดการเฉพาะ `static_routes`;
   ให้ยืนยันด้วยการดู log บนบอร์ดจริงว่าไม่มี del/add วนซ้ำหลัง toggle
7. **การทดสอบบนบอร์ดจริงเสี่ยงตัดขาดตัวเอง** — toggle interface ที่เป็นทางเชื่อม
   ของ browser/SSH จะหลุดทันที: ทดสอบเฉพาะเมื่อเข้าถึงเครื่องทางกายภาพได้ หรือ
   ทดสอบผ่าน interface อื่นที่ไม่ใช่เส้นทางที่กำลัง toggle; ทำ mock mode ให้ผ่านก่อนเสมอ
8. **Mock mode ต้องปลอดภัย 100% เหมือนเดิม** — ทุกการแก้ใน `real_network.go`
   อยู่หลัง build tag linux และ mock เป็น no-op; ห้ามเพิ่ม side effect ใน `mock.go`

---

## 6. Checklist สรุป (Definition of Done)

- [ ] `model/types.go` — เพิ่ม `Managed bool json:"managed"`
- [ ] `real_network.go` — `ConfigureInterface` ไม่ force-up + defer gateway route เมื่อ link down
- [ ] `real_network.go` — `ConfigureWifi` start service เฉพาะเมื่อ link up
- [ ] `service/interface.go` — เพิ่ม `SetInterfaceState` (up: Toggle→Configure→persist; down: Toggle→persist)
- [ ] `service/interface.go` — `GetDataLayerInterface` เซ็ต `Managed` จาก `exists`
- [ ] `service/interface.go` — startup สลับเป็น Toggle ก่อน Configure
- [ ] `api/handlers.go` — `HandleToggleInterface` เรียก service; ตัด status ออกจาก PATCH (:616) และ PUT (:465)
- [ ] `service/interface_test.go` — test ลำดับ Toggle/Configure ทั้งขา up/down + managed flag
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — เพิ่ม `managed` + อัปเดต description ของ toggle (sync สองไฟล์)
- [ ] `frontend/src/data-mockup/mockData.ts` — type `managed?: boolean` + mock data (มี unmanaged 1 ตัว)
- [ ] `frontend/src/pages/Interfaces.tsx` — badge UNMANAGED ใน Status cell (เงื่อนไข `=== false`)
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] ทดสอบ mock mode (`-mock=true`): toggle up/down, Save ตอนปิดแล้วสถานะไม่เปลี่ยน, badge unmanaged แสดงเมื่อไม่มี row ใน DB (เช่นหลังกด Reset)
- [ ] ทดสอบบอร์ดจริง (มี physical access เท่านั้น): static iface ปิด→เปิด แล้ว `ip route` มี default gateway + metric ครบ; Save config ตอน iface ปิด → link ยัง down; reboot แล้ว config ถูก apply ตามลำดับใหม่
- [ ] อัปเดต `docs/ref/*` ที่เกี่ยวข้องถ้าพฤติกรรม toggle ถูกอ้างถึง (เช็ค `interface-metric-design.md`)
