# OS Service Status/Control Panel — ทำ Mock → Real (issue #82)

> แผนงานแปลงการ์ด "Network Services Status (ควบคุมบริการย่อย)" ในหน้า Settings & Maintenance
> จาก mock 100% (hardcoded slice + no-op restart) ให้ query สถานะ systemd unit จริงผ่าน D-Bus
> และ restart จริงผ่าน D-Bus พร้อม audit event — ตามแม่แบบของ Power Control
>
> เขียนเมื่อ: 2026-07-22 · Reference branch: `main` (แยกงานที่ `feat/os-service-status`)
> อ้างอิง: issue #82 · CLAUDE.md · `docs/tech_stack_design.md` · `docs/wifi_wpa_working_instruction.md`
> สถานะ README Feature Status: "Setting (Overall) — services list/restart panel ยังเป็น mock" → target: Completed
> **Owner ล็อกการตัดสินใจแล้ว 2026-07-22:** D1=YES (แสดง per-interface units), D2=`authRoute`,
> D3=YES (`pigate.service` แบบ status-only), D4=YES (`ssh.service`)

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย (พฤติกรรมที่ผู้ใช้เห็น):**
  - การ์ดแสดงรายชื่อ systemd service จริงที่ PiGate ใช้/พึ่งพา พร้อมสถานะ Running/Stopped/Failed/Unavailable ที่อ่านสดจากระบบ
  - ปุ่ม Restart สั่ง restart unit จริงผ่าน D-Bus แล้วบันทึก audit event ใน Event Log
  - unit ที่ห้าม restart (เช่น `pigate.service` เอง) แสดงสถานะได้แต่ไม่มีปุ่ม restart ที่กดได้
- **เงื่อนไขทางเทคนิค (ต้องเป็นจริง):**
  - ห้าม `exec.Command`/`systemctl` — ใช้ D-Bus helper เดิมใน `kernel/dbus_systemd.go` เท่านั้น
  - kernel capability ใหม่ต้องมีทั้ง `real_*.go` (linux) และ `mock.go` (side-effect-free) ตามกฎ CLAUDE.md
  - `{id}` ที่ client ส่งมา restart **ห้าม**เป็น unit name ดิบ — ต้องเป็น slug ที่ map กลับหา unit ใน catalog ฝั่ง server (กัน systemd unit injection)
- **Out of scope (รอบนี้):**
  - ไม่ทำ start/stop/enable/disable — รอบแรกมีแค่ **query status + restart** (ตาม UI เดิมที่มีแค่ปุ่ม Restart)
  - ไม่เพิ่ม DB table/migration — catalog เป็น static ในโค้ด + enumerate interface สดจาก repo (ไม่ persist, รักษา SD card)
  - ไม่รวมเข้า Backup/Restore (ไม่มี config ใหม่ให้ backup)
  - ไม่แตะ `install.sh`/Polkit เพิ่ม — restart ผ่าน `systemd1.Manager.RestartUnit` ใช้สิทธิ์เดียวกับที่ dhcpcd/dnsmasq restart ใช้อยู่แล้ว (ยืนยันตอนทดสอบ real board)

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง ณ 2026-07-22)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Frontend การ์ด + ปุ่ม Restart + badge | ✅ มี (status enum = running/stopped/failed) | `frontend/src/pages/SettingsMaintenance.tsx:997-1065` |
| Frontend API client (real + `IS_MOCK_MODE` localStorage) | ✅ มี แต่ mock ทั้งคู่ | `frontend/src/services/systemService.ts:308-350` |
| Frontend type + seed data | ❌ ผิด (nftables/isc-dhcp-server/NetworkManager) | `frontend/src/data-mockup/mockData.ts:579-604` |
| Route GET/POST restart | ✅ มี (`authRoute`) | `backend/internal/api/router.go:162-163` |
| Handler `HandleGetSystemServices` | ❌ hardcoded slice 4 ตัว | `backend/internal/api/handlers.go:2264-2273` |
| Handler `HandleRestartService` | ❌ no-op `WriteHeader(200)` ล้วน | `backend/internal/api/handlers.go:2275-2278` |
| Model `NetworkServiceStatus` | ⚠️ มี แต่ขาด `RestartAllowed` | `backend/internal/model/types.go:338-344` |
| D-Bus systemd helper | ✅ มี restart + bool status (`IsServiceActiveViaDBus`) | `backend/internal/kernel/dbus_systemd.go:43-80` |
| kernel `SystemServiceManager` interface | ❌ ยังไม่มี | `backend/internal/kernel/interfaces.go` |
| service layer | ❌ ยังไม่มี | — |
| แม่แบบที่ล้อได้ (Power) | ✅ ครบวง interface→real/mock→service→main wiring | `interfaces.go:192`, `real_power.go`, `mock.go:432`, `service/power.go`, `main.go:122/154/176/295` |
| unit-name แบบ dynamic ที่ใช้อยู่แล้ว | ✅ `wpa_supplicant@<if>.service`, `dhcpcd@<if>.service` | `real_network.go:57`, `dhcpcd.go:24` |

**สรุป:** primitive ครบทุกชิ้น (D-Bus restart + status query มีแล้ว, UI/route มีแล้ว) งานคือ
เพิ่ม kernel manager 1 ตัว + service catalog layer + ต่อสาย handler/main + แก้ seed data frontend — ล้อ Power ทั้งวง

## 2. แนวทางเทคนิค

### 2.1 กลไก
- **kernel layer:** เพิ่ม interface `SystemServiceManager { GetStatus(unit) (model.ServiceRuntimeState, error); Restart(unit) error }`
  - `real_system_service.go` (linux) delegate ไปยัง helper ใน `dbus_systemd.go`: อ่าน `Unit.ActiveState` + `Unit.LoadState` (แยก "not-installed" ออกจาก "stopped") ผ่าน `Manager.GetUnit`, restart ผ่าน `RestartServiceViaDBus`
  - `mock.go`: คืน `running` ทุก unit, `Restart` แค่ log (zero side effect)
- **service layer:** `SystemServiceService` ถือ **catalog** (slug → unit + display name + `RestartAllowed`) + enumerate interface สดจาก `repo`
  - singleton: `dnsmasq`, `systemd-resolved`, `systemd-timesyncd`, `ssh` (D4), `pigate` (D3, RestartAllowed=false)
  - dynamic (D1): WLAN interface → `wpa_supplicant@<if>.service`; DHCP-client interface → `dhcpcd@<if>.service`
  - map `ActiveState`: `active`→running, `failed`→failed, LoadState≠loaded→unavailable, อื่น ๆ →stopped
  - `RestartByID(slug)`: หา slug ใน catalog → ถ้าไม่พบ error(not-found), ถ้า `RestartAllowed==false` error(forbidden), มิฉะนั้นเรียก `kernel.Restart(unit)`

### 2.2 ทำไมเลือกแนวนี้ / ทางเลือกที่ตัดทิ้ง
- **catalog อยู่ service ไม่ใช่ kernel:** kernel รับ unit name มา query/act เฉย ๆ ไม่รู้จัก policy ว่า unit ไหนควรโชว์ — ล้อ PowerManager ที่ kernel ไม่รู้เรื่อง audit/business
- **slug ไม่ใช่ unit ดิบจาก client:** ถ้าให้ client ส่ง unit name ตรง ๆ เข้า `RestartUnit` = ให้สั่ง restart systemd unit อะไรก็ได้ (privilege escalation) → บังคับผ่าน catalog whitelist
- **ตัด `nftables.service` ทิ้ง:** PiGate โปรแกรม nftables ตรงผ่าน netlink (`google/nftables`) ไม่ได้ใช้ unit นั้น — โชว์ไปก็ลวง
- **ตัด `systemd-hostnamed`/`systemd-timedated` ทิ้ง:** เป็น D-Bus-activated transient (activate แล้ว exit) — โชว์ running/stopped ไม่มีความหมาย
- **แม่แบบที่ล้อ:** `real_power.go` (โครง real D-Bus manager), `service/power.go` (โครง service), `mock.go` `MockPowerManager` (โครง mock)

## 3. ขั้นตอน (เรียงตาม dependency: kernel → service → main → api → frontend)

**Step 1 — model** · **File:** `backend/internal/model/types.go` (~338)
เพิ่ม struct `ServiceRuntimeState { ActiveState string; Loaded bool }` (ค่า kernel คืน) และเพิ่ม field `RestartAllowed bool` ใน `NetworkServiceStatus`; อนุญาตค่า status `"unavailable"` เพิ่ม

**Step 2 — kernel D-Bus helper** · **File:** `backend/internal/kernel/dbus_systemd.go` (~65)
เพิ่มฟังก์ชัน `GetUnitRuntimeState(unit) (model.ServiceRuntimeState, error)` — ใช้ `GetUnit` เดิมอ่าน `ActiveState`+`LoadState`; คง `IsServiceActiveViaDBus`/`RestartServiceViaDBus` เดิมไว้

**Step 3 — kernel interface + real** · **File:** `interfaces.go` + **new** `backend/internal/kernel/real_system_service.go`
เพิ่ม `SystemServiceManager` ใน `interfaces.go`; เขียน `RealSystemServiceManager` (`//go:build linux`) delegate helper Step 2 + `RestartServiceViaDBus`
> **Sensitive: D-Bus control** — ต้องผ่าน review เข้ม

**Step 4 — kernel mock** · **File:** `backend/internal/kernel/mock.go` (~447 ต่อจาก MockPowerManager)
`MockSystemServiceManager`: `GetStatus` คืน active/loaded ทุก unit, `Restart` แค่ log — zero side effect

**Step 5 — service** · **new** `backend/internal/service/system_service.go`
`SystemServiceService{ mgr kernel.SystemServiceManager; repo *db.Repository }` + catalog + `List()` + `RestartByID(slug)` พร้อม guard
> **Sensitive: unit-name validation (whitelist)** — ต้องผ่าน review เข้ม

**Step 6 — wiring** · **File:** `backend/cmd/pigate/main.go` (~122/154/176/295) + `backend/internal/api/handlers.go` (struct ~40 + `NewServer` ~53-107)
เลือก real/mock ตาม `-mock` (ล้อ `powerMgr`), สร้าง `SystemServiceService`, ต่อเข้า `NewServer` + struct field

**Step 7 — api handler + router** · **File:** `handlers.go:2264-2278` + `router.go:162-163`
`HandleGetSystemServices` → `svc.List()`; `HandleRestartService` → validate id → `RestartByID` → `s.logEvent(model.EventCategorySystem, "service.restarted", model.EventSeverityWarning, ...)`; error code: 400 unknown id / 403 not-allowed / 500 D-Bus fail; route คง `authRoute` ทั้งคู่ (D2)
> **Sensitive: auth + input validation** — ต้องผ่าน review เข้ม
> ไม่ต้องใช้ `logPowerEvent`/`Flush()` เพราะ process ไม่ตาย (ต่างจาก reboot); `logEvent` ปกติพอ

**Step 8 — frontend type + seed** · **File:** `frontend/src/data-mockup/mockData.ts:579-604`
แก้ type เพิ่ม `restartAllowed` + status `"unavailable"`; เปลี่ยน `initialNetworkServices` เป็น identifier จริง (dnsmasq/systemd-resolved/systemd-timesyncd/ssh/pigate)

**Step 9 — frontend page + service** · **File:** `SettingsMaintenance.tsx:1037-1052` + `systemService.ts:322-350`
row ที่ `restartAllowed===false` ไม่แสดงปุ่ม restart ที่กดได้; render badge `"unavailable"`; ปรับ branch `IS_MOCK_MODE` ให้ตรง shape ใหม่
> **ไม่ต้อง:** poll/รอ backend หาย (restart service เดียวไม่ทำ backend ตาย ต่างจาก reboot) — refetch list หลัง restart พอ

## 4. Related API

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/system/services` | authRoute (existing) | คืน list จริงจาก catalog + สถานะสด |
| POST | `/api/system/services/{id}/restart` | authRoute (existing, D2) | restart unit ที่ map จาก slug; `-disable-edit=true` บล็อกอัตโนมัติ (DisableEditMiddleware บล็อก POST); read-only role ถูกบล็อกโดย RoleReadOnlyMiddleware อยู่แล้ว |

route มีอยู่แล้วทั้งคู่ — ไม่เพิ่ม route ใหม่ แค่เปลี่ยน handler ให้ทำงานจริง

## 5. Cautions

1. **Unit injection ผ่าน `{id}`** — ถ้า handler เอา `{id}` ไปยัด `RestartUnit` ตรง ๆ = restart systemd unit อะไรก็ได้ → **ป้องกัน:** `{id}` เป็น slug, resolve ผ่าน catalog whitelist เท่านั้น, slug ไม่พบ = 400
2. **Restart `pigate.service` เอง** — `RestartUnit` unit ตัวเองจะฆ่า process กลาง request → **ป้องกัน:** `RestartAllowed=false` + guard 403 ใน `RestartByID` (กันซ้ำที่ service layer ไม่พึ่ง UI อย่างเดียว)
3. **`ActiveState` ไม่พอแยก not-installed** — unit ที่ไม่ได้ติดตั้ง `GetUnit` อาจ error หรือ LoadState=not-found → **ป้องกัน:** อ่าน `LoadState` ด้วย, map เป็น `unavailable` และ frontend ไม่โชว์ปุ่ม restart
4. **`ssh.service` vs `sshd.service`** — Debian/RPi ใช้ `ssh.service` (alias `sshd.service`) → ใช้ `ssh.service` เป็น unit name จริง (ยืนยันตอนทดสอบ real board)
5. **ทับซ้อนกับ flow เดิม (D1)** — restart `wpa_supplicant@`/`dhcpcd@` ที่นี่ = `RestartUnit` เดียวกับ `RestartDhcpcd`/wifi reconfigure ที่มีอยู่ ไม่ใช่ path ใหม่ (ไม่มีความเสี่ยงเพิ่ม) แต่ให้ note ว่าเป็น unit เดียวกัน อย่าทำ logic reconcile ซ้ำ
6. **mock ต้อง zero side effect** — dev รัน `-mock=true` บนเครื่องตัวเอง; `MockSystemServiceManager.Restart` ต้องแค่ log ห้ามแตะ systemd จริง
7. **สอง mock layer** — `IS_MOCK_MODE` (frontend standalone, localStorage) กับ backend `-mock=true` (MockSystemServiceManager) แยกกัน ต้องแก้ seed data ให้ตรงกันทั้งคู่ (Step 4 + Step 8)
8. **ทดสอบ real board มีความเสี่ยงหลุดการเชื่อมต่อ** — restart `systemd-resolved`/`dnsmasq`/`ssh` อาจตัด DNS/SSH ชั่วครู่ → ทดสอบเมื่อมีทางเข้าถึงบอร์ดสำรอง (console/physical) เท่านั้น, restart `pigate` ควรถูกบล็อก (ห้ามลองปลดบล็อก)

## 6. Summary Checklist (Definition of Done)

- [ ] Step 1: `model/types.go` — `ServiceRuntimeState` + `RestartAllowed` + status `unavailable`
- [ ] Step 2: `kernel/dbus_systemd.go` — `GetUnitRuntimeState`
- [ ] Step 3: `kernel/interfaces.go` + `real_system_service.go` — interface + real (sensitive)
- [ ] Step 4: `kernel/mock.go` — `MockSystemServiceManager` (zero side effect)
- [ ] Step 5: `service/system_service.go` — catalog + `List` + `RestartByID` guard (sensitive)
- [ ] Step 6: `main.go` + `handlers.go` — real/mock select + wiring
- [ ] Step 7: `handlers.go` + `router.go` — handler จริง + audit event + error codes (sensitive)
- [ ] Step 8: `mockData.ts` — type + seed จริง
- [ ] Step 9: `SettingsMaintenance.tsx` + `systemService.ts` — restartAllowed/unavailable + mock branch
- [ ] **Final acceptance (ทดสอบรวมครั้งเดียวหลังครบ Step 1-9):**
  - [ ] `cd backend && go build ./... && go test ./...` ผ่าน
  - [ ] `cd frontend && yarn build && yarn lint` ผ่าน
  - [ ] real backend `-mock=true`: GET คืน unit จริง (ไม่มี nftables/isc-dhcp-server/NetworkManager); `pigate` มี restartAllowed=false; per-interface rows ตรง interface ปัจจุบัน (D1)
  - [ ] restart happy path: `POST .../dnsmasq/restart` = 200 + มี `service.restarted` event ใน Event Log พร้อม username
  - [ ] guardrails: id ไม่รู้จัก = 400; `pigate`/restartAllowed=false = 403; ทั้งคู่ไม่เรียก D-Bus
  - [ ] `grep` ยืนยันไม่มี `exec.Command`/`systemctl` — ผ่าน `dbus_systemd.go` เท่านั้น
  - [ ] frontend real mode: list จาก API, ซ่อน/disable restart สำหรับ read-only row, badge `unavailable` ทำงาน, restart click round-trip
  - [ ] frontend standalone mock (`IS_MOCK_MODE`): การ์ดยังทำงานด้วย seed ใหม่
  - [ ] read-only role restart ไม่ได้ (D2 — RoleReadOnlyMiddleware)
- [ ] **Docs:** ไม่ต้องแก้ `openapi.yaml` (route เดิม, schema เพิ่มแค่ optional field — เพิ่ม `restartAllowed` ใน schema ทั้งสองไฟล์ให้ตรง); อัปเดต README Feature Status เป็น Completed
