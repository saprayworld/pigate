# Power Control — Shutdown / Restart อุปกรณ์จริง (ไม่ใช่แค่ระบบ PiGate)

> เอกสารแผนงานสำหรับฟีเจอร์: ทำให้ปุ่ม **Reboot / Shutdown** ในหน้า
> Settings & Maintenance และเมนูผู้ใช้ (nav-user) **สั่งงานบอร์ด Raspberry Pi 5
> จริง** ผ่าน D-Bus แทนที่ handler ที่เป็น stub ตอบ 200 เฉย ๆ ในปัจจุบัน
>
> วันที่เขียน: 2026-07-07 · Branch อ้างอิง: `feat/frontend-ui-redesign`
> สถานะใน README Feature Status: Power Control = **Mock** → เป้าหมายคือ **Completed**

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย:** เมื่อ super_admin กด Reboot/Shutdown จาก UI แล้ว
บอร์ดต้องรีสตาร์ท/ปิดเครื่องจริง โดย:

- ใช้ **D-Bus ไปที่ `org.freedesktop.login1` (systemd-logind)** — ห้ามใช้
  `exec.Command("reboot")` / `shutdown` เด็ดขาด (กติกา no-shell-execution
  ของโปรเจกต์ ดู `docs/tech_stack_design.md`)
- ทำงานได้ภายใต้ user `pigate` ที่ไม่ใช่ root (ผ่าน Polkit rule ไม่ใช่ capability —
  `cap_net_admin/cap_net_raw` ไม่ครอบคลุมการสั่ง power)
- โหมด `-mock=true` บนเครื่อง dev ต้อง **ไม่มีทาง** ไปแตะเครื่องจริง

**นอกขอบเขต:** Power-on ระยะไกล (ทำไม่ได้ทาง software เมื่อบอร์ดดับแล้ว —
overlay `powered-off` ฝั่ง frontend คอมเมนต์ปุ่มจำลองทิ้งไว้แล้ว ถูกต้องแล้ว),
scheduled reboot, System Services panel (ยัง Mock แยกเป็นงานอื่น)

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ |
|---|---|
| Frontend UI | **เสร็จแล้ว** — `power-control.tsx` (overlay), `usePowerControl.tsx` (hook), เรียกจาก `SettingsMaintenance.tsx` และ `nav-user.tsx` |
| Frontend API client | **เสร็จแล้ว** — `systemService.ts` (~บรรทัด 352) เรียก `POST /api/system/reboot` และ `POST /api/system/shutdown` |
| Route + Auth | **มีแล้ว** — `router.go:130-131` ผูกเป็น `authRoute` (POST ถูก `RoleReadOnlyMiddleware` บล็อกถ้าไม่ใช่ super_admin อยู่แล้ว) |
| Backend handler | **Stub** — `handlers.go:1656-1662` `HandleReboot`/`HandleShutdown` ตอบ `200 OK` เฉย ๆ ไม่ทำอะไร |
| Kernel layer | **ยังไม่มี** — ไม่มี `PowerManager` ใน `kernel/interfaces.go` |
| Polkit | **ยังไม่อนุญาต** — rule ใน `install.sh` ครอบคลุมแค่ `systemd1.manage-units`, `hostname1.*`, `timedate1.*` และมี catch-all `return polkit.Result.NO` ปิดท้าย → login1 action จะถูกปฏิเสธแน่นอนถ้าไม่เพิ่ม |
| openapi.yaml | มี `/system/reboot`, `/system/shutdown` อยู่แล้ว (บรรทัด ~1469-1485) แต่ description ยังไม่บอกเงื่อนไข role/พฤติกรรม delay |

สรุป: **งานเกือบทั้งหมดอยู่ฝั่ง backend + install.sh** ฝั่ง frontend แทบไม่ต้องแตะ

---

## 2. แนวทางเทคนิค: `org.freedesktop.login1` ผ่าน D-Bus

ใช้ pattern เดียวกับ `real_hostname.go` (hostname1) และ `dbus_systemd.go` (systemd1):

```go
conn, _ := dbus.SystemBus()
obj := conn.Object("org.freedesktop.login1", dbus.ObjectPath("/org/freedesktop/login1"))

// interactive=false → ถ้า Polkit ไม่อนุญาตให้ error ทันที ไม่เด้ง auth prompt
obj.Call("org.freedesktop.login1.Manager.Reboot", 0, false)
obj.Call("org.freedesktop.login1.Manager.PowerOff", 0, false)
```

เหตุผลที่เลือก login1 แทนทางเลือกอื่น:
- เป็น API มาตรฐานของ systemd-logind สำหรับ power management โดยเฉพาะ
  มี Polkit action id แยกชัดเจน (`org.freedesktop.login1.reboot`,
  `org.freedesktop.login1.power-off`) → จำกัดสิทธิ์ได้แคบที่สุด
- ทางเลือก `systemd1.Manager.StartUnit("reboot.target", "replace-irreversibly")`
  ใช้ action `manage-units` ซึ่ง rule เดิมของเรา match ด้วย unit name —
  ทำได้แต่ปนกับ logic เดิมและสื่อความหมายแย่กว่า
- logind จะสั่ง shutdown แบบ graceful: systemd ไล่ stop service (รวม
  `pigate.service` เอง) → SQLite ถูกปิดอย่างถูกต้อง ไม่ต้องเขียนโค้ด flush เอง

---

## 3. ขั้นตอนการทำ (เรียงลำดับ + ไฟล์ที่ต้องแก้)

### Step 1 — เพิ่ม `PowerManager` interface ในชั้น kernel
**ไฟล์:** `backend/internal/kernel/interfaces.go`

```go
// PowerManager abstracts host power control via org.freedesktop.login1
// (systemd-logind) over D-Bus. Reboot/PowerOff are irreversible.
type PowerManager interface {
    Reboot() error
    PowerOff() error
}
```

### Step 2 — implement ฝั่ง real
**ไฟล์ใหม่:** `backend/internal/kernel/real_power.go` (มี `//go:build linux`
เหมือนไฟล์ D-Bus อื่น)

- `RealPowerManager` + `NewRealPowerManager()`
- เมธอด `Reboot()` / `PowerOff()` เรียก login1 ตาม §2, ส่ง `interactive=false`
- log ด้วย prefix `[RealPower]` ตามสไตล์ `[RealHostname]`

### Step 3 — implement ฝั่ง mock
**ไฟล์:** `backend/internal/kernel/mock.go`

- `MockPowerManager` + `NewMockPowerManager()` — แค่ `log.Printf` ว่าได้รับคำสั่ง
  แล้ว return nil (ห้ามมี side effect ใด ๆ)
- วางท้ายไฟล์ใกล้ `MockHostnameManager` (~บรรทัด 353) ให้เข้ากลุ่มเดียวกัน

### Step 4 — service layer
**ไฟล์ใหม่:** `backend/internal/service/power.go`

- `PowerService{ mgr kernel.PowerManager }` + `NewPowerService(...)`
- `Reboot(requestedBy string)` / `Shutdown(requestedBy string)`:
  1. log ว่าใครสั่ง (audit — handler ดึง username จาก context ส่งมาให้)
  2. **หน่วงการยิงคำสั่งจริงด้วย `time.AfterFunc(1*time.Second, ...)`**
     แล้ว return nil ทันที → เพื่อให้ HTTP response ถูก flush กลับไปหา browser
     ก่อนที่ logind เริ่มฆ่า process (ดูข้อควรระวัง §5.1)
- หมายเหตุ: เนื่องจากคำสั่งจริงรันใน goroutine หลัง response ไปแล้ว
  error จาก D-Bus จะรายงานกลับ client ไม่ได้ — ให้ log error ไว้ และถ้าต้องการ
  fail-fast อาจเช็คก่อนว่าต่อ SystemBus ได้ (optional)

### Step 5 — wiring ใน main
**ไฟล์:** `backend/cmd/pigate/main.go`

- ประกาศ `var powerMgr kernel.PowerManager` คู่กับ manager ตัวอื่น (~บรรทัด 84)
- สาขา mock (`~99`): `powerMgr = kernel.NewMockPowerManager()`
- สาขา real (`~112`): `powerMgr = kernel.NewRealPowerManager()`
- สร้าง `powerService := service.NewPowerService(powerMgr)` (~127)
- ส่งเข้า `api.NewServer(...)` (~144) — ต้องเพิ่ม parameter

> ฟีเจอร์นี้ **ไม่ต้อง** ทำ `InitApplyConfig()` ตอน boot — ไม่มี state ให้ apply
> และไม่เกี่ยวกับ `netlink_monitor.go` (ไม่แตะ route/interface)

### Step 6 — API handler
**ไฟล์:** `backend/internal/api/handlers.go`

- เพิ่ม field `powerService *service.PowerService` ใน `Server` struct (~บรรทัด 24)
  และ parameter ใน `NewServer` (~46)
- แก้ `HandleReboot` / `HandleShutdown` (~1656):
  ```go
  func (s *Server) HandleReboot(w http.ResponseWriter, r *http.Request) {
      username, _ := r.Context().Value(UserContextKey).(string)
      if err := s.powerService.Reboot(username); err != nil {
          s.writeError(w, http.StatusInternalServerError, "Failed to reboot: "+err.Error())
          return
      }
      w.WriteHeader(http.StatusOK)
  }
  ```
  (Shutdown ทำแบบเดียวกัน)
- ควรบันทึกเหตุการณ์ลง ring buffer (`s.logs`) ด้วย เพื่อให้เห็นใน Recent Logs
  ก่อนเครื่องดับ/หลังบูตกลับมา (ring buffer ไม่ persist — หายหลังรีบูต ซึ่งยอมรับได้)

### Step 7 — ปรับ route เป็น super_admin แบบ explicit
**ไฟล์:** `backend/internal/api/router.go` (บรรทัด 130-131)

- เปลี่ยนจาก `authRoute` → `superAdminRoute` สำหรับ `/api/system/reboot` และ
  `/api/system/shutdown`
- ผลลัพธ์เชิงพฤติกรรมเท่าเดิม (POST ถูกบล็อกสำหรับ non-super_admin อยู่แล้วโดย
  `RoleReadOnlyMiddleware`) แต่ทำให้เจตนา "สั่งดับเครื่องได้เฉพาะ super_admin"
  อ่านออกจาก router ตรง ๆ แบบเดียวกับ config export/import

### Step 8 — เพิ่ม Polkit rule
**ไฟล์:** `install.sh` (STEP 3, ไฟล์ `/etc/polkit-1/rules.d/10-pigate-system.rules`)

เพิ่ม action id เหล่านี้เข้าไปใน block เดียวกับ hostname1/timedate1
(if ก้อนที่สอง) สำหรับ `subject.user == "pigate"`:

```
org.freedesktop.login1.reboot
org.freedesktop.login1.reboot-multiple-sessions
org.freedesktop.login1.power-off
org.freedesktop.login1.power-off-multiple-sessions
```

(`*-multiple-sessions` จำเป็นเผื่อกรณีมี user session อื่นค้างอยู่ เช่น SSH —
logind จะสลับไปเช็ค action ตัวนี้แทน)

⚠️ ดูข้อควรระวัง §5.2 เรื่องโครงสร้าง rule เดิมก่อนแก้

### Step 9 — อัปเดตเอกสาร API
**ไฟล์:** `docs/openapi.yaml` (~บรรทัด 1469) และ **`frontend/public/openapi.yaml`**
(ต้อง sync สองไฟล์ให้ตรงกัน — ไฟล์หลังถูก render ผ่านหน้า ApiDocs)

- เพิ่ม `security: [BearerAuth]`, ระบุว่าเป็น super_admin only (403)
- อธิบายพฤติกรรม: ตอบ `200` ทันที แล้วเครื่องเริ่ม reboot/poweroff ภายใน ~1 วินาที
  (client ควรคาดว่า connection จะหลุดหลังจากนั้น)

### Step 10 — อัปเดตสถานะโปรเจกต์
**ไฟล์:** `README.md` (ตาราง Feature Status: Power Control → Completed backend),
`docs/project_status.md` ถ้ามี entry เกี่ยวข้อง

### Step 11 (optional, frontend polish) — ตรวจจับว่าบูตกลับมาแล้วจริง
**ไฟล์:** `frontend/src/hooks/usePowerControl.tsx`

ตอนนี้ countdown 5 วินาทีเป็นตัวเลขสมมุติ — Pi 5 บูตจริงใช้เวลา ~20-40 วินาที
ปรับ flow `rebooting` ให้หลัง countdown จบแล้ว **poll** endpoint เบา ๆ เช่น
`GET /api/auth/session` ทุก 2-3 วินาที จนตอบกลับสำเร็จ แล้วค่อย
`window.location.reload()` (ระหว่าง poll ให้โชว์ overlay ต่อไป)
ข้อความตกแต่งใน `power-control.tsx` ที่โชว์ `systemctl poweroff -i` เป็นแค่
คอสเมติก จะเปลี่ยนเป็นข้อความกลาง ๆ (เช่น `dbus: login1.PowerOff`) หรือคงไว้ก็ได้

---

## 4. API ที่เกี่ยวข้อง (มีอยู่แล้ว ไม่ต้องเพิ่มเส้นใหม่)

| Method | Path | ใครเรียกได้ | พฤติกรรมใหม่ |
|---|---|---|---|
| POST | `/api/system/reboot` | super_admin | ตอบ 200 → หน่วง ~1s → login1 `Reboot(false)` |
| POST | `/api/system/shutdown` | super_admin | ตอบ 200 → หน่วง ~1s → login1 `PowerOff(false)` |

ทั้งสองเส้นถูกบล็อกอัตโนมัติในโหมด `-disable-edit=true` (DisableEditMiddleware
บล็อก POST ทั้งระบบ) — พฤติกรรมนี้ถูกต้องตามที่ต้องการ ไม่ต้องแก้เพิ่ม

---

## 5. ข้อควรระวัง

1. **ต้อง flush HTTP response ก่อนเครื่องเริ่มดับ** — ถ้าเรียก D-Bus ตรง ๆ ใน
   handler, logind อาจเริ่ม stop `pigate.service` ก่อน response ถึง browser →
   frontend จะเข้า branch error ทั้งที่คำสั่งสำเร็จ จึงต้องใช้ `time.AfterFunc`
   ใน service (Step 4) และยอมรับ trade-off ว่า error หลัง delay รายงานได้แค่ใน log

2. **Polkit rule เดิมมี catch-all `return polkit.Result.NO`** ปิดท้าย
   `polkit.addRule` (ดู `install.sh` STEP 3) — ถ้าเพิ่ม login1 action ผิดตำแหน่ง
   (เช่นไปเพิ่ม rule ใหม่ *หลัง* rule เดิมในไฟล์เดียวกัน) จะโดน NO ตัดก่อนถึง
   เสมอ ต้องเพิ่ม action id เข้าไปใน if ก้อนที่สองของ rule เดิมเท่านั้น
   และหลังแก้ต้อง `systemctl restart polkit` (สคริปต์ทำอยู่แล้ว)
   สำหรับเครื่องที่ติดตั้งไปแล้ว ต้องรัน `install.sh` ซ้ำหรือแก้ไฟล์ rule เอง —
   ควรระบุไว้ใน release note

3. **โหมด mock ต้องปลอดภัย 100%** — เส้นทางเลือก manager อยู่ที่ `main.go`
   เท่านั้น ห้ามมีโค้ดใน handler/service ที่ bypass ไปเรียก
   `kernel.NewRealPowerManager()` ตรง ๆ และ `MockPowerManager` ห้ามมี side
   effect ใด ๆ (dev รันบนเครื่องทำงานจริง ถ้าพลาดคือเครื่อง dev ดับ)

4. **ห้ามใช้ `exec.Command`** (`reboot`, `shutdown`, `systemctl`) —
   ขัด constraint หลักของโปรเจกต์ แม้จะ "ง่ายกว่า" ก็ตาม

5. **Inhibitor locks** — ถ้ามี process ถือ block-inhibitor อยู่ `PowerOff(false)`
   อาจ fail ด้วย `OperationRefused` บน Raspberry Pi OS headless แทบไม่เกิด
   แต่ให้ log error ชัด ๆ พอ ไม่ต้องไปขอ action `*-ignore-inhibit` เพิ่ม
   (ให้สิทธิ์แคบที่สุดไว้ก่อน)

6. **ข้อมูลไม่พัง** — logind สั่ง graceful shutdown: `pigate.service` ถูก stop
   ตามปกติ SQLite ปิด connection เอง ไม่ต้องเขียน flush พิเศษ แต่**อย่า**
   เปลี่ยนไปใช้วิธี force เช่น `SetWallMessage`+force flag หรือ sysrq

7. **การทดสอบ:**
   - บนเครื่อง dev: รัน `-mock=true` → กดปุ่มจาก UI → เช็ค log ว่า
     `MockPowerManager` ถูกเรียก และ overlay ฝั่ง frontend ทำงานครบ flow
   - `go build ./...` + `go test ./...` ใน `backend/` ต้องผ่าน
     (interface ใหม่ต้อง implement ครบทั้ง real/mock ไม่งั้น compile error
     จะฟ้องเองถ้าประกาศ type assertion ไว้)
   - บนบอร์ดจริง: ทดสอบ **shutdown ทีหลัง reboot** (reboot กลับมาเองได้
     shutdown ต้องเดินไปถอดปลั๊ก) และทดสอบตอนที่เข้าถึงตัวบอร์ดได้เท่านั้น
     อย่าทดสอบผ่าน remote ที่ไปกู้เครื่องไม่ได้
   - ทดสอบ role: login ด้วย admin ธรรมดา (read-only) → ต้องได้ 403

8. **อย่าลืมว่า UI มีจุดเรียกสองที่** — `SettingsMaintenance.tsx` (Dialog ยืนยัน)
   และ `nav-user.tsx` (confirm ผ่าน `useAlert`) ทั้งคู่ใช้ hook
   `usePowerControl` ร่วมกัน แก้ behavior ที่ hook ที่เดียวพอ ห้ามแก้แยกสองที่

---

## 6. Checklist สรุป (Definition of Done)

- [ ] `kernel/interfaces.go` — เพิ่ม `PowerManager`
- [ ] `kernel/real_power.go` — login1 Reboot/PowerOff ผ่าน D-Bus (ไฟล์ใหม่)
- [ ] `kernel/mock.go` — `MockPowerManager` (log อย่างเดียว)
- [ ] `service/power.go` — delay + audit log (ไฟล์ใหม่)
- [ ] `cmd/pigate/main.go` — เลือก real/mock + wiring เข้า `NewServer`
- [ ] `api/handlers.go` — implement `HandleReboot`/`HandleShutdown` จริง
- [ ] `api/router.go` — เปลี่ยนสองเส้นนี้เป็น `superAdminRoute`
- [ ] `install.sh` — เพิ่ม login1 action ใน Polkit rule (ระวัง catch-all NO)
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — sync spec
- [ ] `README.md` Feature Status — Power Control → Completed
- [ ] (optional) `usePowerControl.tsx` — poll จนบูตกลับมาแล้ว auto-reload
- [ ] ทดสอบ mock บน dev, ทดสอบจริงบนบอร์ด (reboot → shutdown), ทดสอบ 403 ของ role admin
