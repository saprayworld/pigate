# Offline Interfaces UI — จัดการ interface ที่มี config ใน DB แต่หายไปจาก kernel (issue #49)

> แผนงานสำหรับ issue #49: ทำให้ interface ที่ตั้งค่าไว้ใน DB แต่ไม่มีอยู่จริงใน kernel
> (VLAN ที่ parent หาย, USB NIC ที่ถอดออก) **มองเห็นได้และลบ config ได้จากหน้า UI**
> โดยไม่ต้องใช้ sqlite3 — ต่อยอด design principle จาก #46/#48 (tolerate dangling refs,
> การลบเป็นการตัดสินใจของผู้ใช้เท่านั้น ห้ามลบอัตโนมัติ)
>
> เขียนเมื่อ: 2026-07-15 · Reference branch: `feat/offline-interfaces-ui` (แตกจาก `main` หลัง merge PR #54)

## 0. Goal and Scope

**Goal (พฤติกรรมที่ผู้ใช้เห็น):**
- หน้า Interfaces มี toggle "แสดง offline interfaces" — **ค่าเริ่มต้นปิด**
- เมื่อเปิด: แถว interface ที่มี config ใน DB แต่ไม่มี kernel link ปรากฏพร้อม badge `OFFLINE`
- ผู้ใช้กดลบ config ของ offline interface ออกจาก DB ได้ (ปุ่มถังขยะ) — ปลดล็อกเคสแบบ #46

**เงื่อนไขทางเทคนิค:**
- DTO ของ `GET /api/interfaces` แยกให้ออกว่าแถวไหน offline (`status: "offline"` + `managed: true`)
- consumer ภายในตัวอื่นของ data layer (firewall sync, dhcpcd, hostname, dashboard) **ต้องไม่เห็นแถว offline** — พฤติกรรมเดิมห้ามเปลี่ยน

**Out of scope:**
- แก้ไข config (PUT/PATCH) ของ offline interface — v1 block ด้วย 409; เปิดเป็น follow-up ได้
  (ถ้าทำจริงจะ synergy กับ #48: แก้ DB แล้ว link กลับมา → self-heal apply ให้เอง)
- การลบ config อัตโนมัติ — **ห้ามทำ** (invariant จาก #48)
- Event bus / startup reconciliation → จบไปแล้วใน #48 (PR #54)

## 1. Current State (สำรวจโค้ดจริง 2026-07-15)

| ส่วน | สถานะ | หลักฐาน (file:line) |
|---|---|---|
| Frontend: badge `OFFLINE` | **มีแล้ว** (รอ status จาก backend) | `frontend/src/pages/Interfaces.tsx:~935` |
| Frontend: ปุ่ม reset/delete สำหรับแถว offline | **มีแล้ว** | `Interfaces.tsx:~958-978` |
| Frontend: `handleDeleteInterface` + confirm dialog | **มีแล้ว** | `Interfaces.tsx:~473` |
| Frontend: toggle "แสดง offline" (default off) | **ไม่มี** | ตรวจด้วย grep `showOffline` — ไม่พบ |
| Frontend API client: `interfaceService.delete` | **มีแล้ว** | `frontend/src/services/interfaceService.ts:~341` |
| Route: `DELETE /api/interfaces/{id}` (authRoute) | **มีแล้ว** | `backend/internal/api/router.go:~62` |
| Handler: `HandleDeleteInterface` | **มีแต่เป็น dead code สำหรับ non-VLAN** — เช็ค `iface.Status != "offline"` (`handlers.go:~919`) แต่ backend ไม่เคย produce `"offline"` (Status มีแค่ `up`/`down` — `service/interface.go:~381-384`, `model/types.go:~143`) และ lookup ผ่าน `GetDataLayerInterfaceByID` ซึ่งหาแถว DB-only ไม่เจอ → 404 เสมอ | `backend/internal/api/handlers.go:~892-930` |
| Service: `GetDataLayerInterface` | วนเฉพาะ kernel interfaces — แถว DB ที่ไม่มี kernel link **หายไปจากผลลัพธ์** | `backend/internal/service/interface.go:~423-501` |
| Service: `DeleteVlanInterface` | ลบได้เฉพาะเมื่อ link ยังอยู่ — `DeleteVlan` fail → return error ทั้งที่เป้าหมายคือลบ config | `service/interface.go:~807-820` |
| Kernel layer (`interfaces.go`/`real_*.go`/`mock.go`) | **ไม่ต้องแตะ** — การลบ config เป็น DB write ล้วน ไม่มี OS capability ใหม่ | — |
| DB: `repo.DeleteInterface` | **มีแล้ว** ไม่ต้องมี migration | ใช้อยู่ใน `FlushInterfaceConfig` (`service/interface.go:~658`) |
| `main.go` wiring / `install.sh` | **ไม่ต้องแตะ** | — |
| openapi (`docs/openapi.yaml` + `frontend/public/openapi.yaml`) | status enum ยังไม่มี `offline`; DELETE description อธิบายพฤติกรรมเดิม | `docs/openapi.yaml:~581` |

**สรุป:** frontend พร้อมเกือบ 100% (ขาดแค่ toggle) — งานจริงกระจุกอยู่ที่ backend ชั้น service/handler:
ทำให้แถว offline โผล่ใน data layer ของ **เฉพาะ API interfaces** และทำให้ delete path ที่มีอยู่แล้วใช้งานได้จริง

## 2. Technical Approach

**กลไกหลัก:** เพิ่ม method ใหม่ข้าง `GetDataLayerInterface` เดิม (ไม่แก้ของเดิม):

```go
// GetDataLayerInterfaceIncludingOffline: data layer เดิม + แถว DB ที่ไม่มี kernel link
// (Status="offline", Managed=true) — ใช้เฉพาะ API interfaces เท่านั้น
func (s *InterfaceService) GetDataLayerInterfaceIncludingOffline() ([]model.NetworkInterface, error) {
    list, err := s.GetDataLayerInterface()
    ...
    return appendOfflineRows(list, dbIfaces), nil // pure function → unit test ได้ตรง ๆ
}
```

- `HandleGetInterfaces` และ `GetDataLayerInterfaceByID` เปลี่ยนไปใช้ตัว IncludingOffline
- handler ที่ mutate kernel (`Toggle`/`Update`/`Patch`/`ScanWifi`/`WifiStatus`) ใส่ guard
  `if iface.Status == "offline"` → 409 พร้อมข้อความชัดเจน
- `DeleteVlanInterface` ใช้ `kernelInterfaceNameSet()` (มีอยู่แล้ว `service/interface.go:~210`)
  เช็คก่อน: link ไม่อยู่ → ข้าม `DeleteVlan` แล้วลบ DB row + prune DNS binding ตามเดิม
- Frontend: state `showOffline` (default `false`) + `<Switch>` ใน toolbar, filter รายการฝั่ง client

**ทางเลือกที่พิจารณาแล้วปัดตก:**
1. *แก้ `GetDataLayerInterface` เดิมให้รวม offline* — ปัดตก: มี caller 7 จุดนอก API interfaces
   (`service/firewall.go:87`, `service/dhcpcd.go:77,108,151`, `service/hostname.go:130`,
   `api/handlers.go:309,2644`) ที่จะได้ interface ผีไปประมวลผล เช่น dhcpcd อาจสั่ง start
   client บน link ที่ไม่มีจริง
2. *query param `?include_offline=true`* — ปัดตก: DTO แยกด้วย `status` ได้อยู่แล้ว, frontend
   filter ฝั่ง client เพียงพอ, ลด surface ของ API contract
3. *endpoint ใหม่แยก `GET /api/interfaces/offline`* — ปัดตก: over-engineering, UI ใช้ตารางเดียว

**Pattern ต้นแบบ:** การแยก pure function เพื่อเลี่ยงข้อจำกัด mock ตามแนว `TestRecreateVlanIfPossible`
(`service/interface_test.go:~1026`) — เพราะ mock `GetKernelInterfaces` mirror ทุกแถว DB เข้า kernel list
(`service/interface.go:~329-341`) เคส offline จึงไม่มีทางเกิดใน mock mode

## 3. Steps (เรียงชั้นในสุด → นอกสุด)

### Step 1 — service: เพิ่ม offline rows ใน data layer
**File:** `backend/internal/service/interface.go` (~ต่อจาก `GetDataLayerInterface` บรรทัด ~501)
- เพิ่ม `appendOfflineRows(dataLayer []model.NetworkInterface, dbIfaces []model.NetworkInterface) []model.NetworkInterface`
  (pure function): แถว DB ที่ชื่อไม่อยู่ใน dataLayer → set `Status = "offline"`, `Managed = true`, append
- เพิ่ม `GetDataLayerInterfaceIncludingOffline()` ตาม §2
- แก้ `GetDataLayerInterfaceByID` (~503) ให้เรียกตัว IncludingOffline

### Step 2 — service: `DeleteVlanInterface` ทน link ที่หายไปแล้ว
**File:** `backend/internal/service/interface.go:~807-820`
- ก่อน `s.network.DeleteVlan(iface.Name)`: ถ้า `!s.kernelInterfaceNameSet()[iface.Name]` → log + ข้าม
  netlink delete ไปลบ DB row (+ prune DNS binding เดิม) เลย

### Step 3 — handlers: เปิดใช้ delete path + guard offline
**File:** `backend/internal/api/handlers.go`
- `HandleGetInterfaces` (~473): เปลี่ยนเป็น `GetDataLayerInterfaceIncludingOffline()`
- `HandleToggleInterface` (~822), `HandleUpdateInterface` (~504), `HandlePatchInterface` (~669),
  `HandleScanWifi` (~850), `HandleGetWifiStatus` (~871): เพิ่ม guard offline → 409
  `"interface is offline; only delete is allowed"`
- `HandleResetInterface` (~932): หลัง `FlushInterfaceConfig` ถ้า refresh (`~946`) ได้ `nil`
  (เคส offline — ลบ row แล้วไม่เหลืออะไรใน kernel) → ตอบ `{"success": true}` แทน 500
- `HandleDeleteInterface` (~892): เพิ่ม `logEvent` (`network.interface_deleted`) ให้ non-VLAN path
  ด้วย — ตอนนี้มีเฉพาะ VLAN path (~913)

### Step 4 — unit tests
**File:** `backend/internal/service/interface_test.go`
- `appendOfflineRows`: DB row ไม่มีใน kernel → offline/managed; มีครบ → ไม่เพิ่มแถวซ้ำ
- `DeleteVlanInterface` เมื่อ link หาย: ลบ DB row สำเร็จโดยไม่เรียก `DeleteVlan` (ใช้ tracker เดิม)

### Step 5 — openapi ทั้งสองไฟล์
**Files:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` (ต้อง sync กัน)
- schema interface `status`: เพิ่มค่า `offline` + คำอธิบาย
- `DELETE /interfaces/{id}` (~581): อธิบายเงื่อนไข offline-only สำหรับ non-VLAN และ 409 ของ handler อื่น

### Step 6 — frontend: toggle แสดง offline
**File:** `frontend/src/pages/Interfaces.tsx`
- state `showOffline` (default `false`) + `<Switch>` + `<Label>` "แสดง offline interfaces" ใน toolbar
  เหนือตาราง (ใช้ shadcn `Switch` ที่ import อยู่แล้ว ~บรรทัด 51)
- filter: `showOffline ? interfaces : interfaces.filter(i => i.status !== "offline")`
- (optional) badge จำนวนแถวที่ถูกซ่อน ข้าง toggle เมื่อมี offline > 0
- `vlanParentOptions` (~489): เพิ่มเงื่อนไข `i.status !== "offline"` กันสร้าง VLAN บน parent ผี

> **ไม่ต้องทำ:** kernel interface ใหม่/`mock.go` (ไม่มี OS capability ใหม่ — ลบเป็น DB write),
> `main.go` wiring (ไม่มี service/manager ใหม่), migration (ไม่มี schema ใหม่),
> `install.sh` (ไม่มีสิทธิ์ใหม่), `backup.go` (ตาราง interfaces อยู่ใน backup อยู่แล้ว),
> `InitApplyConfig` (ไม่มี state ใหม่ต้อง apply ตอน boot)

## 4. Related API

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/interfaces` | authRoute (ทุก role อ่านได้) | **เปลี่ยน:** รวมแถว offline (`status:"offline"`, `managed:true`); password ถูก mask ด้วย `maskInterfacePasswords` เดิม |
| DELETE | `/api/interfaces/{id}` | authRoute — `RoleReadOnlyMiddleware` block non-super_admin (`middleware.go:~123`) | **เปิดใช้จริง:** non-VLAN ลบได้เฉพาะ `status == "offline"`; VLAN ลบได้เสมอ (ทน link หาย) |
| POST | `/api/interfaces/{id}/toggle`, PUT/PATCH `/{id}`, GET `/{id}/scan`, `/{id}/wifi-status` | authRoute | **เปลี่ยน:** เจอแถว offline → 409 (เดิม 404 เพราะหาไม่เจอ) |

- ไม่มี route ใหม่ — ทุกเส้นมีอยู่แล้วใน `router.go:~56-64`
- `-disable-edit=true`: `DisableEditMiddleware` (`middleware.go:~333`) block DELETE อยู่แล้ว → read-only
  mode เห็นแถว offline ได้แต่ลบไม่ได้ — ถูกต้องตาม design

## 5. Cautions

1. **ห้ามแก้ `GetDataLayerInterface` เดิมเด็ดขาด** — caller 7 จุด (ดู §2 ข้อปัดตก 1) จะได้แถวผี:
   dhcpcd (`HandleLinkEvent` ใช้มันหา target) อาจตัดสินใจ start/stop client กับ link ที่ไม่มีจริง,
   firewall sync จะ generate rule จาก interface ผี → ใช้ method ใหม่เฉพาะ API interfaces เท่านั้น
2. **`GetDataLayerInterfaceByID` เปลี่ยนแล้วกระทบ 7 handler ที่ใช้มัน** — จากเดิม offline row = 404
   กลายเป็น "เจอ" → handler ที่ mutate kernel จะพยายามยิง netlink ใส่ link ที่ไม่มี → error ไม่สื่อ
   → ต้องใส่ guard 409 ครบทุกตัวตาม Step 3 มิฉะนั้นผู้ใช้กด toggle จากหน้าอื่น (เช่น API ตรง) จะได้ 500
3. **`HandleResetInterface` จะ 500 ทั้งที่สำเร็จ** — สำหรับแถว offline: `FlushInterfaceConfig` ลบ row
   แล้ว refresh ByID ได้ `nil` (`handlers.go:~946-949`) → frontend ขึ้น error ทั้งที่ลบสำเร็จแล้ว
   → ต้อง handle เคส nil ตาม Step 3 (ปุ่ม reset ของแถว offline ใน UI มีอยู่แล้ว จะโดนเคสนี้แน่)
4. **เคส offline สร้างไม่ได้ใน mock mode** — mock `GetKernelInterfaces` mirror ทุกแถว DB
   (`interface.go:~329-341`) → toggle ใน UI จะไม่มีแถวให้เห็นเมื่อรัน `-mock=true`
   → unit test ต้อง test pure function `appendOfflineRows` ตรง ๆ; การทดสอบ UI จริงทำบน VM:
   สร้าง VLAN ผ่าน PiGate → `ip link del <vlan>` → เปิด toggle → เห็นแถว OFFLINE → กดลบ
5. **ทำงานร่วมกับ self-heal #48** — ต้อง regression-test ลำดับนี้บน VM: ลบ config ของ offline
   interface → เสียบ/สร้าง link กลับมา → `InterfaceAdded` → `ReapplyInterfaceByName` ต้อง log
   `"not a managed interface, ignoring"` (ไม่ apply config ที่เพิ่งลบ) และกลับกัน: **ห้าม**มี path ไหน
   ลบ config อัตโนมัติเมื่อ link หาย (invariant #48 — งานนี้เพิ่มเฉพาะ "ผู้ใช้กดลบเอง")
6. **การลบ non-VLAN ไม่ prune references ใน subsystem อื่น** — VLAN path prune DNS Server binding
   (`interface.go:~825-847`) แต่ `repo.DeleteInterface` เปล่า ๆ จะเหลือ dangling ref ใน dhcp-range/QoS/
   DNS binding — ระบบทนได้ตาม #46/#48 แต่เพื่อความสม่ำเสมอ ให้ non-VLAN delete path ใช้การ prune
   DNS binding แบบเดียวกับ VLAN (ย้าย logic prune เป็น helper ใช้ร่วมกัน) ส่วน dhcp-range/QoS
   ปล่อยเป็น dangling ที่ tolerate ได้ (ผู้ใช้เห็นและแก้เองในหน้านั้น ๆ)
7. **อย่าลืม sync openapi ทั้งสองไฟล์** — `docs/openapi.yaml` และ `frontend/public/openapi.yaml`
   drift กันได้ง่ายเพราะแก้มือทั้งคู่
8. **Frontend styling** — toggle ใช้ shadcn `Switch` + semantic colors เท่านั้น; badge OFFLINE เดิมใช้
   `border-warning/20 bg-warning/10 text-warning` อยู่แล้ว (`Interfaces.tsx:~936`) ไม่ต้องแตะ

## 6. Summary Checklist (Definition of Done)

- [ ] `service/interface.go`: `appendOfflineRows` + `GetDataLayerInterfaceIncludingOffline` + ByID ใช้ตัวใหม่
- [ ] `service/interface.go`: `DeleteVlanInterface` ข้าม netlink delete เมื่อ link ไม่อยู่ใน kernel
- [ ] `service/interface.go`: ย้าย DNS-binding prune เป็น helper ใช้ร่วม VLAN/non-VLAN delete
- [ ] `api/handlers.go`: `HandleGetInterfaces` ใช้ IncludingOffline
- [ ] `api/handlers.go`: guard 409 ใน Toggle/Update/Patch/ScanWifi/WifiStatus สำหรับแถว offline
- [ ] `api/handlers.go`: `HandleResetInterface` ไม่ 500 เมื่อ refresh เป็น nil (เคส offline)
- [ ] `api/handlers.go`: `HandleDeleteInterface` non-VLAN path มี `logEvent`
- [ ] `service/interface_test.go`: test `appendOfflineRows` + `DeleteVlanInterface` link หาย
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml`: status enum `offline` + DELETE/409 (sync ทั้งคู่)
- [ ] `frontend/src/pages/Interfaces.tsx`: toggle `showOffline` default off + filter + `vlanParentOptions` ตัด offline
- [ ] `go build ./...` + `go test ./...` ผ่าน; `yarn build` + `yarn lint` ผ่าน
- [ ] ทดสอบ mock mode: toggle แสดงผลปกติ (ไม่มีแถว offline — คาดหวังตาม Caution 4), delete VLAN ปกติยังทำงาน
- [ ] ทดสอบ VM: สร้าง VLAN → `ip link del` → เปิด toggle เห็น OFFLINE → ลบสำเร็จ → row หายจาก DB
- [ ] ทดสอบ VM: regression self-heal ตาม Caution 5 (ลบ config แล้ว link กลับมา → ignoring)
- [ ] ทดสอบ role: admin_readonly ลบไม่ได้ (403), `-disable-edit=true` ลบไม่ได้
- [ ] อัปเดต README Feature Status (ถ้าตาราง Interfaces มีหมายเหตุเกี่ยวข้อง) + ปิด issue #49 ใน PR
