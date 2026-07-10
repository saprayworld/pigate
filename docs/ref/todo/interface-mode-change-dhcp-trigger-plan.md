# Interface Mode Change → DHCP Trigger — เปลี่ยน Static→DHCP ขณะ up แล้ว start dhcpcd ให้อัตโนมัติ

> แผนงานแก้บั๊ก: เปลี่ยน addressing mode จาก **Static → DHCP** ขณะ interface กำลัง
> UP อยู่ แล้วกด Save → static IP ถูกถอด แต่ **dhcpcd client ไม่ถูก start** →
> interface ค้างไม่มี IP ต้อง toggle ปิด-เปิดเองถึงจะได้ lease. แก้ด้วยแนวทาง B
> (รวม decision logic ไว้ที่เดียวใน `DhcpcdService` แล้วให้ handler เรียกหลัง Save).
>
> วันที่เขียน: 2026-07-10 · Branch อ้างอิง: `main` (commit `d3e059b`) · Issue: #22

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (พฤติกรรมที่ผู้ใช้เห็น):**
1. interface กำลัง UP → Edit เปลี่ยน mode เป็น **DHCP** → กด Save → dhcpcd ถูก start
   ทันที (ethernet) หรือเมื่อ associate แล้ว (wifi) โดย**ไม่ต้อง toggle ปิด-เปิดเอง**
2. interface กำลัง UP → Edit เปลี่ยน mode เป็น **Static** → กด Save → dhcpcd ตัวเก่า
   (ถ้ามี lease ค้าง) ถูก **stop/release** ไม่ให้ค้าง
3. พฤติกรรมเดิมทุกอย่างคงเดิม: startup sync, link-event (toggle/สายหลุด), Wi-Fi
   ที่ยังไม่ associate ยังต้องรอ `RUNNING` เหมือนเดิม

**เงื่อนไขทางเทคนิค:** logic ตัดสินใจ start/stop dhcpcd (ethernet vs wifi + running)
ต้อง **อยู่ที่เดียว** ไม่ duplicate; เส้นทางเรียกต้องไม่เกิด circular dependency

**นอกขอบเขต:**
- ไม่แก้ layering เดิมที่ `service/dhcpcd.go` เรียก `netlink.LinkByName` ตรง ๆ
  (`SyncActiveInterfaces` ทำอยู่แล้ว — ตามรอย pattern เดิมเพื่อความสม่ำเสมอ)
- ไม่แตะ `ConfigureInterface` (kernel) — ให้ kernel ทำหน้าที่ configure อย่างเดียว
  ตามเดิม การ trigger dhcpcd เป็นงานของ service/handler (ดู §2 ทางเลือกที่ตัดทิ้ง)
- ไม่แตะ DHCP **server** (dnsmasq) — คนละระบบกับ dhcpcd **client**
- ไม่มี schema/migration ใหม่ · ไม่แตะ `install.sh`/Polkit (ไม่มี privilege ใหม่)

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Save flow → service | มีแล้ว | `handlers.go` `HandlePatchInterface`(~:589)/`HandleUpdateInterface`(~:479) → `ApplyInterfaceConfig` |
| `ApplyInterfaceConfig` | **ไม่ start dhcpcd** — ConfigureWifi → ConfigureInterface → repo.UpdateInterface เท่านั้น | `service/interface.go:381-439` |
| `ConfigureInterface` dhcp branch | flush IPv4 addr แล้ว `return nil` เฉย ๆ | `kernel/real_network.go:207-209` |
| dhcpcd start จุดเดียวคือ event/startup | **ต้นเหตุ** — mode change ขณะ up ไม่เกิด Link event → ไม่มีใคร trigger | ดูสองแถวถัดไป |
| `DhcpcdService.HandleLinkUpdate` | start/stop จาก **Link event** (flag up/down/running); early-return ถ้า mode != dhcp (:78) | `service/dhcpcd.go:49-113` |
| `DhcpcdService.SyncActiveInterfaces` | loop ทุก iface, อ่าน flag จาก `netlink.LinkByName`, guard `IsMockMode`(:128); เรียกครั้งเดียวตอน startup | `service/dhcpcd.go:116-169`, `main.go:224` |
| **Logic ซ้ำ** ethernet/wifi/running | อยู่ 2 ที่ (`HandleLinkUpdate` :87-112 กับ `SyncActiveInterfaces` :149-167) — เกือบเหมือนกันเป๊ะ | จุดที่แผนนี้จะรวมให้เหลือที่เดียว |
| netlink monitor → dhcpcd | monitor เรียก `HandleLinkUpdate` เฉพาะ `linkChan`; Addr event ทำแค่ `reconcile()` (routing/DNS) | `netlink_monitor.go:110-127` |
| `DhcpcdManager` interface + real + mock | ครบ — `StartDhcpcd`/`StopDhcpcd`/`RestartDhcpcd`/`SetShareHostname` มีทั้ง real (D-Bus) และ mock (no-op) | `kernel/interfaces.go:104-116`, `kernel/dhcpcd.go`, `kernel/mock.go:327-352` |
| `api.Server` ถือ dhcpcdService | **ยังไม่ถือ** — struct มี `interfaceService` แต่ไม่มี `dhcpcdService`; `NewServer` ก็ยังไม่รับ | `handlers.go:25-95` |
| main.go wiring | `dhcpcdService` ถูกสร้าง (:129) และส่งเข้า netlinkMonitor/backupService แต่**ไม่ได้ส่งเข้า `api.NewServer`**(:160) | `main.go:129,151,156,160` |
| Test | `dhcpcd_test.go` ทดสอบ `HandleLinkUpdate` (mock mode); `trackingDhcpcdManager` (มี `startCalls`/`stopCalls`/`restarted`) มีใน `hostname_test.go` reuse ได้ | `service/dhcpcd_test.go`, `service/hostname_test.go:25-50` |

**สรุป:** งานจริงกระจุกที่ `service/dhcpcd.go` (extract helper + เพิ่ม `SyncInterface`) +
wiring `dhcpcdService` เข้า `api.Server` (struct + NewServer + main.go) + เรียกใน 2 handler
ไม่มี migration ไม่มี kernel ใหม่ ไม่มี frontend

---

## 2. แนวทางเทคนิค (แนวทาง B — รวม decision)

**หลักคิด:** สกัด "การตัดสินใจ start/stop dhcpcd สำหรับ iface ที่เป็น dhcp" (ethernet
vs wifi + running) ออกเป็น helper ตัวเดียว แล้วให้ 3 ทางเรียกใช้ร่วมกัน:
`HandleLinkUpdate` (flag จาก event) · `SyncActiveInterfaces` (flag จาก kernel, loop) ·
`SyncInterface(name)` **ตัวใหม่** (flag จาก kernel, ตัวเดียว — สำหรับ mode change)

```go
// service/dhcpcd.go — helper กลาง (kernel-free, เทสต์ได้ตรง)
// ครอบเฉพาะ dhcp-mode decision; ผู้เรียกกรอง mode != dhcp มาก่อน
func (s *DhcpcdService) applyDhcpcdDecision(name string, isWifi, isUp, isRunning bool) {
    switch {
    case !isUp:                s.stopDhcpcd(name)          // down → stop
    case isWifi && !isRunning: /* wifi up แต่ยังไม่ associate → รอ */
    default:                   s.startDhcpcd(name)         // ethernet up / wifi running → start
    }
}

// SyncInterface: ใช้หลัง mode change (ไม่มี Link event ให้ยึด) — อ่าน flag จาก kernel เอง
func (s *DhcpcdService) SyncInterface(name string) {
    // guard mock mode เหมือน SyncActiveInterfaces (:128) — mock ไม่แตะ kernel
    // หา iface ใน data layer; ถ้า mode != "dhcp" → stopDhcpcd(name) แล้ว return
    //   (สำคัญ: ปล่อย lease เก่าตอนสลับมา static — HandleLinkUpdate ไม่ทำข้อนี้ให้)
    // ถ้า mode == "dhcp" → LinkByName เอา flag → applyDhcpcdDecision(...)
}
```

**จุดต่างสำคัญ (subtlety ที่ต้องคุมให้ถูก):** ขา **mode → static** ต้อง `stopDhcpcd`
แต่ `HandleLinkUpdate` (:78) กับ `SyncActiveInterfaces` (:124) **จงใจ early-return/continue
สำหรับ non-dhcp** (ไม่ stop) — เพราะ link flap บน static ไม่ควรยิง stop ซ้ำ ๆ. ดังนั้น
"static → stop" ใส่**เฉพาะใน `SyncInterface`** ไม่ยัดลง helper กลาง → พฤติกรรม 2 ทางเดิม
ไม่เปลี่ยน (helper กลางครอบแค่ decision ของ dhcp เท่านั้น)

**ผู้เรียก `SyncInterface`:** ให้ **handler** (`HandlePatchInterface`/`HandleUpdateInterface`)
เรียกหลัง `ApplyInterfaceConfig` สำเร็จ — เพราะ handler อยู่ชั้นบนสุด ถือได้ทั้ง
`interfaceService` และ `dhcpcdService` โดยไม่ circular

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
- *ให้ `InterfaceService.ApplyInterfaceConfig` เรียก dhcpcd เอง* — ตัดทิ้ง: InterfaceService
  จะต้องถือ dhcpcd ซึ่ง `DhcpcdService → InterfaceService` อยู่แล้ว = **circular**; ถ้าฉีด
  `kernel.DhcpcdManager` ตรง (เลี่ยง circular ได้) ก็ยังต้อง **duplicate logic** ethernet/wifi
  ที่อยู่ใน DhcpcdService อยู่ดี
- *ให้ `ConfigureInterface` (kernel) คืนสัญญาณให้ service ไป trigger* — ตัดทิ้ง: kernel layer
  ไม่ควรสั่ง service; และ service รู้ `mode=="dhcp"` เองอยู่แล้ว ไม่ต้องให้ kernel บอก —
  เปลี่ยน signature กระทบ `interfaces.go` + `mock.go` โดยได้ประโยชน์น้อย
- *ให้ netlink monitor จับ Addr event แล้ว sync dhcpcd* — ตัดทิ้ง: Addr event ยิงบ่อยและ
  ไม่บอกเจตนา "เปลี่ยน mode"; จุด trigger ที่ถูกคือ handler ซึ่งรู้จังหวะ Save แน่นอน

**Template ในโค้ดเดิมที่ใช้ตาม:** logic ตัดสินใจของ `SyncActiveInterfaces`
(`service/dhcpcd.go:149-167`) และ mock-guard (:128); การถือ service ใน `Server`
ตาม field `interfaceService` เดิม (`handlers.go:33`)

---

## 3. ขั้นตอน (เรียงตาม dependency: service → wiring → handler → test → docs)

### Step 1 — สกัด helper กลาง + refactor 2 ผู้เรียกเดิม
**File:** `backend/internal/service/dhcpcd.go`
- เพิ่ม `applyDhcpcdDecision(name string, isWifi, isUp, isRunning bool)` (โค้ด §2)
- `HandleLinkUpdate` (:87-112): แทน block ethernet/wifi ด้วยการเรียก helper (คง early-return
  mode != dhcp ที่ :78 ไว้)
- `SyncActiveInterfaces` (:149-167): แทน block เดียวกันด้วย helper (คง `continue` non-dhcp
  ที่ :124 และ mock-guard :128 ไว้)
> พฤติกรรมของสองตัวนี้ต้อง **เท่าเดิมทุกประการ** — `dhcpcd_test.go` ที่มีอยู่ต้องผ่านโดยไม่แก้

### Step 2 — เพิ่ม `SyncInterface(name string)`
**File:** `backend/internal/service/dhcpcd.go` (วางใต้ `SyncActiveInterfaces`)
- guard `s.repo.IsMockMode()` → log แล้ว return (mock ไม่แตะ kernel)
- ดึง `GetDataLayerInterface`, หา iface ตามชื่อ; ไม่เจอ → return
- `AddressingMode != "dhcp"` → `s.stopDhcpcd(name)`; return
- เป็น dhcp → `netlink.LinkByName(name)` เอา flag → `applyDhcpcdDecision(name, isWifi, isUp, isRunning)`

### Step 3 — ให้ `api.Server` ถือ `dhcpcdService`
**File:** `backend/internal/api/handlers.go`
- เพิ่ม field `dhcpcdService *service.DhcpcdService` ใน `Server` struct (~:33 ใต้ interfaceService)
- เพิ่มพารามิเตอร์ใน `NewServer(...)` (~:57 ถัดจาก ifaceService) + เซ็ตใน return (~:80)

### Step 4 — wiring ใน main.go
**File:** `backend/cmd/pigate/main.go:160`
- ส่ง `dhcpcdService` เข้า `api.NewServer(...)` (ตัวแปรมีอยู่แล้วที่ :129 — แค่เพิ่ม argument)

### Step 5 — handler เรียก `SyncInterface` หลัง Save
**File:** `backend/internal/api/handlers.go`
- `HandlePatchInterface` (หลัง `ApplyInterfaceConfig` สำเร็จ ~:702) และ
  `HandleUpdateInterface` (~:571): เพิ่ม `s.dhcpcdService.SyncInterface(iface.Name)`
- เรียกเป็น **non-fatal** (SyncInterface ไม่คืน error อยู่แล้ว — log ภายใน) เพื่อไม่ให้
  dhcpcd ที่ start ไม่ขึ้นไปทำให้ Save (ซึ่งบันทึก DB สำเร็จแล้ว) กลายเป็น 500
> **ไม่ต้องทำ:** ไม่ต้องเรียกใน `HandleToggleInterface` — toggle สร้าง Link event อยู่แล้ว
> (`HandleLinkUpdate` จัดการให้); ไม่ต้องเรียกใน `HandleResetInterface` — reset ลบ row =
> ไม่มี config dhcp ให้ start

### Step 6 — Test ฝั่ง service
**File:** `backend/internal/service/dhcpcd_test.go`
- ใช้ `trackingDhcpcdManager` (`hostname_test.go:25`) แทน `MockDhcpcdManager` เพื่อ assert call
- test `applyDhcpcdDecision`/`SyncInterface` decision: ethernet up→`startCalls`, down→`stopCalls`,
  wifi up-not-running→ไม่มี call, wifi running→start, mode=static→`stopCalls`
- ยืนยัน `HandleLinkUpdate` เดิมยัง assert เท่าเดิม (ไม่ regress)
> หมายเหตุ: ขา `SyncInterface` ที่ต้องอ่าน kernel จริงเทสต์ได้จำกัดใน mock mode — เทสต์
> `applyDhcpcdDecision` (kernel-free) ตรง ๆ เป็นตัวครอบ logic หลัก

### Step 7 — Docs (optional, ไม่กระทบ contract)
**Files:** `docs/openapi.yaml` + `frontend/public/openapi.yaml`
- ไม่มี schema/endpoint ใหม่; เพิ่มได้แค่ประโยคใน description ของ PATCH/PUT ว่า
  "เปลี่ยน mode เป็น dhcp ขณะ up จะ start dhcpcd ให้อัตโนมัติ" (sync สองไฟล์)
- อัปเดต `docs/ref/complete/dhcp-service-design.md` ถ้ามีหัวข้อ dhcpcd client trigger

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| PATCH | `/api/interfaces/{id}` | authRoute (mutation → super_admin) | **เปลี่ยน**: หลัง save เรียก `SyncInterface` → start/stop dhcpcd ตาม mode + flag ปัจจุบัน |
| PUT | `/api/interfaces/{id}` | authRoute | เหมือน PATCH |

ทุก route มีอยู่แล้ว ไม่มี route ใหม่ · ไม่มี payload/response schema เปลี่ยน ·
`-disable-edit=true` บล็อก PATCH/PUT ผ่าน `DisableEditMiddleware` เหมือนเดิม (SyncInterface
รันหลัง ApplyInterfaceConfig จึงถูกบล็อกไปพร้อมกัน) — ไม่ต้องแก้

---

## 5. ข้อควรระวัง

1. **ห้ามเปลี่ยนพฤติกรรมของ `HandleLinkUpdate`/`SyncActiveInterfaces` ตอน refactor** —
   สองตัวนี้ถูกเรียกจาก netlink monitor และ startup; ถ้า helper กลางเผลอใส่ "static → stop"
   ลงไป จะทำให้ static interface โดน stopDhcpcd ทุก Link event (สาย flap = ยิง stop รัว) →
   ป้องกันด้วยการคง early-return non-dhcp ที่เดิม แล้วใส่ stop เฉพาะใน `SyncInterface`
2. **SyncInterface ต้อง guard mock mode** — มันเรียก `netlink.LinkByName` (แตะ kernel จริง)
   เหมือน `SyncActiveInterfaces:128`; ถ้าลืม guard → `-mock=true` บน dev workstation จะไป
   อ่าน/สั่ง interface จริง = ผิดกติกา "mock ปลอดภัย 100%"
3. **SyncInterface ต้องเรียก *หลัง* `ApplyInterfaceConfig` (ซึ่ง repo.UpdateInterface แล้ว)** —
   เพราะมันอ่าน mode ล่าสุดจาก `GetDataLayerInterface` (data layer = DB overlay); ถ้าเรียกก่อน
   จะเห็น mode เก่า → ตัดสินใจผิด
4. **start dhcpcd ต้อง non-fatal ต่อ Save** — Save บันทึก DB สำเร็จไปแล้วก่อนถึง SyncInterface;
   ถ้า D-Bus start dhcpcd ล้มเหลวต้อง log warning เฉย ๆ ไม่ทำให้ handler ตอบ 500 (ผู้ใช้จะ
   เข้าใจผิดว่า config ไม่ถูกบันทึก) — `SyncInterface` จึงออกแบบให้ไม่คืน error
5. **Wi-Fi + static/associate timing เป็น known limitation เดิม** — เปลี่ยนเป็น dhcp บน wifi
   ที่ยังไม่ associate: `SyncInterface` เห็น `!RUNNING` → ไม่ start (ถูกต้อง) แล้ว dhcpcd จะ
   start เองเมื่อ `HandleLinkUpdate` เห็น RUNNING ภายหลัง — ไม่ใช่บั๊ก ไม่ต้องแก้ในงานนี้
6. **ทิศ dhcp→static ที่ interface down อยู่** — `SyncInterface` จะ `stopDhcpcd` (idempotent,
   D-Bus stop unit ที่ไม่ได้รันอยู่ = no-op) ปลอดภัย
7. **ทดสอบบอร์ดจริงเสี่ยงตัดขาดตัวเอง** — เปลี่ยน mode ของ interface ที่เป็นทางเชื่อม
   browser/SSH ทำให้ IP เปลี่ยน/หลุด: ทดสอบเฉพาะเมื่อมี physical access หรือผ่าน interface อื่น;
   ทำ mock mode ให้ผ่านก่อนเสมอ

---

## 6. Checklist สรุป (Definition of Done)

- [ ] `service/dhcpcd.go` — สกัด `applyDhcpcdDecision` + refactor `HandleLinkUpdate`/`SyncActiveInterfaces` ให้เรียกใช้ (พฤติกรรมเท่าเดิม)
- [ ] `service/dhcpcd.go` — เพิ่ม `SyncInterface(name)` (mock-guard + static→stop + dhcp→decision)
- [ ] `api/handlers.go` — เพิ่ม field `dhcpcdService` ใน `Server` + พารามิเตอร์ใน `NewServer`
- [ ] `cmd/pigate/main.go` — ส่ง `dhcpcdService` เข้า `api.NewServer`
- [ ] `api/handlers.go` — `HandlePatchInterface` + `HandleUpdateInterface` เรียก `SyncInterface` หลัง save (non-fatal)
- [ ] `service/dhcpcd_test.go` — test decision ครบเคส (ethernet/wifi/up/running/static) + ไม่ regress `HandleLinkUpdate`
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — (optional) เพิ่มประโยค description PATCH/PUT (sync สองไฟล์)
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน
- [ ] ทดสอบ mock mode (`-mock=true`): เปลี่ยน mode ผ่าน API แล้วดู log `[MockDhcpcd] Simulating starting/stopping` ตรงตาม mode
- [ ] ทดสอบบอร์ดจริง (physical access เท่านั้น): static→dhcp ขณะ up (ethernet) → `ip addr` ได้ lease โดยไม่ต้อง toggle; dhcp→static ขณะ up → dhcpcd ถูก stop (`systemctl is-active dhcpcd@<iface>` = inactive); wifi static→dhcp → dhcpcd start หลัง associate
- [ ] อัปเดต `docs/ref/*` ที่เกี่ยวข้องถ้าพฤติกรรม dhcpcd trigger ถูกอ้างถึง; ปิด issue #22
