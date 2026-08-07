# Cloudflare Tunnel Integration — เพิ่มการจัดการ Cloudflare Tunnel ด้วย token ผ่าน UI

> เอกสารแผนงานสำหรับฟีเจอร์: เพิ่มความสามารถให้ผู้ใช้กรอก Cloudflare Tunnel token
> ผ่าน UI แล้วให้ PiGate จัดการ start/stop/restart/status/remove/update-token
> ของ `cloudflared` เป็น systemd service ผ่าน D-Bus (ไม่มีความสามารถนี้มาก่อน)
>
> วันที่เขียน: 2026-08-07 · Branch อ้างอิง: `main` (จะแยก branch ใหม่ก่อนเริ่มโค้ด)
> สถานะใน README Feature Status: ไม่มีแถวนี้ → เป้าหมายคือเพิ่มแถวใหม่ "Cloudflare Tunnel: Completed"

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย:** super_admin เข้าหน้า `/system/cloudflare-tunnel` กรอก Cloudflare
  Tunnel token (ได้จาก Cloudflare Zero Trust dashboard) แล้วกด Start →
  `cloudflared` รันเป็น systemd service จริงบนเครื่อง เชื่อมต่อออกไปยัง
  Cloudflare edge ได้ ดูสถานะ running/stopped/failed ได้ อัปเดต token ใหม่ได้
  (restart อัตโนมัติ) ลบ token + หยุด service ได้ สถานะ enabled/disabled คงอยู่
  ข้ามการ reboot (apply ตอน boot เหมือน subsystem อื่น)
- **นอกขอบเขต (ชัดเจน):**
  - ไม่ทำ ingress rules / `config.yml` editor ใน PiGate — จัดการที่ Cloudflare
    dashboard เท่านั้น (token-based remotely-managed tunnel อย่างเดียว)
  - ไม่ดาวน์โหลด/ติดตั้ง `cloudflared` binary ให้อัตโนมัติ — ผู้ใช้ติดตั้งเอง
    บนเครื่อง (ลด supply-chain surface ของ installer), `install.sh` แค่ตรวจ
    และเตือนถ้ายังไม่มี
  - ไม่ทำ Cloudflare API login/OAuth ใดๆ ในระบบ (มีแค่ token ที่ผู้ใช้ก็อปมาวาง)
  - ไม่เพิ่ม metrics/traffic ของ tunnel เข้า Dashboard

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ | ไฟล์:บรรทัด |
|---|---|---|
| D-Bus systemd helper (start/stop/restart/status ของ unit ใดก็ได้) | มีครบแล้ว ใช้ซ้ำได้ทันที | `backend/internal/kernel/dbus_systemd.go` (`StartServiceViaDBus`/`StopServiceViaDBus`/`RestartServiceViaDBus`/`GetUnitRuntimeState`) |
| Pattern kernel manager คุม systemd unit + เขียน config file ให้ root service อ่าน | มีแล้ว (atomic temp+rename) | `backend/internal/kernel/dhcpcd.go:48-77` (`RealDhcpcdManager.SetShareHostname`) |
| Pattern whitelist unit-name กัน injection ที่ service layer | มีแล้ว | `backend/internal/service/system_service.go:39-45,147-163` |
| Mapping ActiveState → running/stopped/failed/unavailable | มีแล้ว ใช้ซ้ำได้ | `backend/internal/service/system_service.go:101-113` |
| Polkit allowlist unit ของ pigate (deny-by-default, มี guard `subject.user != "pigate"`) | มีแล้ว ต้อง**เพิ่มชื่อ unit ใหม่** | `install.sh:247-269` |
| Pattern เก็บ secret ใน SQLite | มีแล้ว = plaintext (`wifi_presets.password`) + mask ที่ handler | `backend/internal/db/wifi_preset_repo.go:12-17`, `backend/internal/service/backup.go:136` |
| Pattern ตาราง single-row settings | มีแล้ว (`system_hostname_settings`, `dhcp_health_settings`) | `backend/internal/db/connection.go:383-407` |
| Route แบบ super_admin only สำหรับข้อมูลลับ (รวม GET) | มีแล้ว (wifi-presets) | `backend/internal/api/router.go:23,89-96` |
| Output chain firewall (traffic ขาออกของ cloudflared) | default accept — ใช้ได้ทันที | `backend/internal/kernel/real_firewall.go:508-520` |
| Sidebar nav (group "System", role-conditional) | มีแล้ว รูปแบบ `isSuperAdmin ? [...] : []` | `frontend/src/components/app-sidebar.tsx:82,157-168` |
| Route guard `SuperAdminRoute` | มีแล้ว ใช้กับ `/system/users` | `frontend/src/App.tsx:52-61,194-206` |
| Cloudflare Tunnel (backend/frontend/install.sh) | **ยังไม่มีอะไรเลย** | grep ไม่พบ cloudflare/tunnel ในโค้ด |

สรุป: งานจริงคือ "ประกอบของที่มีอยู่แล้ว" (D-Bus systemd helper, atomic file
write pattern, single-row settings table, super_admin route pattern) เข้าด้วยกัน
เป็น subsystem ใหม่ — ไม่มีชิ้นไหนต้องคิดกลไกใหม่ทั้งหมด

## 2. แนวทางเทคนิค

**เลือก: Static unit file (root-owned, สร้างโดย `install.sh`) + PiGate เขียนแค่
`EnvironmentFile` แยก (`/var/lib/pigate/cloudflared.env`, mode 0600) + ควบคุม
ผ่าน D-Bus systemd helper เดิม**

```ini
# /etc/systemd/system/cloudflared.service (สร้างโดย install.sh)
[Service]
EnvironmentFile=-/var/lib/pigate/cloudflared.env
ExecStart=<cloudflared bin path> --no-autoupdate --loglevel info tunnel run
Restart=on-failure
RestartSec=5s
User=cloudflared
# ไม่มี [Install] section — PiGate เป็นคนสั่ง start/stop เอง ไม่ enable
```

cloudflared อ่าน token จาก env `TUNNEL_TOKEN` — ไม่ต้องใส่ token ใน `ExecStart`
เลย PiGate เขียนไฟล์เดียว (`TUNNEL_TOKEN=<token>\n`) แบบ atomic (temp+chmod
0600+rename) ตาม pattern `dhcpcd.go:54-69`

**เหตุผลที่เลือกทางนี้ / ทางเลือกที่ตัดทิ้ง:**
- **ตัดทิ้ง: ให้ PiGate เขียน/enable unit file เอง (`EnableUnitFiles`+`daemon-reload`)**
  ต้องให้สิทธิ์เขียน `/etc/systemd/system` + polkit action `manage-unit-files`
  แก่ user `pigate` = เท่ากับให้รันอะไรก็ได้เป็น root ทางอ้อม เสี่ยงเกินประโยชน์
- **ตัดทิ้ง: `cloudflared service install <token>` ผ่าน `exec.Command`**
  ผิด constraint หลักของโปรเจกต์ (ห้าม shell exec) และมันเขียน token ลง
  unit file ที่ world-readable โดย default
- **ตัดทิ้ง: ใส่ token ใน `ExecStart`** — จะรั่วผ่าน `ps aux` และ
  `systemctl show` ที่ user ทุกคนบนเครื่องอ่านได้ ใช้ `EnvironmentFile` 0600 แทน
- **เลือก plaintext ใน SQLite (ไม่เข้ารหัสเพิ่ม)** — สอดคล้องกับ pattern
  `wifi_presets.password` ที่มีอยู่แล้ว, ลดความซับซ้อน, ชดเชยด้วย DB/env file
  permission ที่เข้ม (ตัดสินใจโดยเจ้าของโปรเจกต์ 2026-08-07)

**pattern/ไฟล์แม่แบบที่ให้ทำตาม:**
`dbus_systemd.go`, `dhcpcd.go` (kernel), `system_service.go` (service layer +
ActiveState mapping), `wifi_preset_repo.go` (DB repo + "ค่าว่าง=คงเดิม"),
`router.go:89-96` (superAdminRoute แม้แต่ GET), `app-sidebar.tsx:157-168` +
`App.tsx:52-61,194-206` (sidebar+route guard), `DnsServer.tsx` (โครงหน้า frontend)

## 3. ขั้นตอนการทำ (เรียงลำดับ dependency)

**Step 1 — Model + validation (SENSITIVE)**
**ไฟล์:** `backend/internal/model/cloudflare_tunnel.go` (ใหม่),
`backend/internal/model/cloudflare_tunnel_test.go` (ใหม่)
- `CloudflareTunnelSettings{Enabled, Token, UpdatedAt}`,
  `CloudflareTunnelStatus{Enabled, HasToken, TokenHint, Status, UnitLoaded, BinaryInstalled}`
- `ValidateTunnelToken(token string) error`: ปฏิเสธ `\n`/`\r`/`\x00`/control
  char ใดๆ (กัน env-file injection — ความเสี่ยงสูงสุดของงานนี้), อนุญาตเฉพาะ
  `^[A-Za-z0-9+/=_.-]+$`, ความยาว 32-4096, trim เฉพาะรอบนอกก่อน validate
- `TokenHint(token)` คืน 4 ตัวท้ายเท่านั้น เช่น `****a1b2`
- ห้ามมีฟังก์ชันใดใน package นี้ log ค่า token
- unit test ครอบ newline injection, charset ผิด, ความยาวเกิน/ขาด

**Step 2 — DB migration + repository**
**ไฟล์:** `backend/internal/db/connection.go` (แก้, เพิ่ม migration ต่อท้าย
~บรรทัด 383-407), `backend/internal/db/cloudflare_tunnel_repo.go` (ใหม่),
`backend/internal/db/cloudflare_tunnel_migration_test.go` (ใหม่)
- `CREATE TABLE IF NOT EXISTS cloudflare_tunnel_settings (id INTEGER PRIMARY KEY CHECK(id=1), enabled INTEGER NOT NULL DEFAULT 0 CHECK(enabled IN (0,1)), token TEXT NOT NULL DEFAULT '', updated_at TEXT NOT NULL DEFAULT '')`
  + seed แถว id=1 แบบ `INSERT OR IGNORE`
- Repo ตาม pattern `wifi_preset_repo.go`: `GetCloudflareTunnelSettings()`
  (คืน token เต็ม สำหรับ service layer เท่านั้น), `UpdateCloudflareTunnelSettings(s)`
  (token ว่าง = คงค่าเดิม เหมือน `wifi_preset_repo.go:67-89`), `ClearCloudflareTunnelSettings()`
- migration test ตาม pattern `timezone_migration_test.go`: DB เก่าไม่มีตารางนี้
  ต้อง migrate ขึ้นได้ + มีแถว id=1

**Step 3 — `TunnelManager` interface**
**ไฟล์:** `backend/internal/kernel/interfaces.go` (แก้, เพิ่มท้ายไฟล์)
- Methods: `WriteToken(token string) error`, `RemoveToken() error`, `Start() error`,
  `Stop() error`, `Restart() error`, `Status() (model.ServiceRuntimeState, error)`,
  `BinaryInstalled() bool` (ตรวจด้วย `os.Stat`, ห้าม exec)
- doc comment ระบุชัด: unit name เป็นค่าคงที่ภายใน implementation ไม่รับจาก
  client, ห้าม log/ห้ามส่ง token เป็น D-Bus argument หรือ ExecStart argument

**Step 4 — `RealCloudflaredManager` (SENSITIVE)**
**ไฟล์:** `backend/internal/kernel/real_cloudflared.go` (ใหม่, `//go:build linux`)
- `const cloudflaredUnit = "cloudflared.service"`,
  `cloudflaredEnvPath = "/var/lib/pigate/cloudflared.env"`,
  `cloudflaredBinPath` (ลอง `/usr/local/bin/cloudflared` แล้ว fallback
  `/usr/bin/cloudflared` ด้วย `os.Stat`)
- Start/Stop/Restart/Status เรียก `dbus_systemd.go` helper ตรงๆ — ห้ามเขียน
  D-Bus ใหม่ ห้าม `exec.Command`
- `WriteToken`: validate ซ้ำ (defense in depth) → เขียน `path+".tmp"` ด้วย
  `os.OpenFile(..., O_WRONLY|O_CREATE|O_TRUNC, 0600)` + `os.Chmod` ชัดเจน (กัน
  umask) → `os.Rename` เข้าที่ (ตาม `dhcpcd.go:54-69`) เนื้อหา `TUNNEL_TOKEN=<token>\n`
  บรรทัดเดียว
- `RemoveToken`: `os.Remove` แบบ ignore `ErrNotExist`
- log ได้แค่ `"wrote cloudflared env (token len=%d)"` ห้ามมีค่า token ใน log ใดๆ

**Step 5 — `MockTunnelManager`**
**ไฟล์:** `backend/internal/kernel/mock.go` (แก้, เพิ่มท้ายไฟล์ ตาม pattern
`MockSystemServiceManager` ~บรรทัด 588-606)
- เก็บ state ในหน่วยความจำ (`hasToken bool`, `running bool`) เท่านั้น — **ห้าม
  เขียนไฟล์จริง ห้ามเรียก D-Bus จริง** (dev รัน `-mock=true` บนเครื่องทำงานเอง)
- `BinaryInstalled()` คืน `true` เสมอ, log แบบ `[MockTunnel] ... (no-op)`

**Step 6 — `CloudflareTunnelService`**
**ไฟล์:** `backend/internal/service/cloudflare_tunnel.go` (ใหม่)
- `GetStatus()`: อ่าน DB + `mgr.Status()` + `mgr.BinaryInstalled()` → map
  ActiveState (ใช้ logic เดียวกับ `system_service.go:101-113`) → คืนเฉพาะ
  `TokenHint` ห้ามคืน token เต็ม
- `SetToken(token)`: validate → repo update → `mgr.WriteToken` → ถ้า enabled
  อยู่แล้ว `mgr.Restart()`
- `Start()/Stop()/Restart()`: เขียน `enabled` ลง DB ก่อนสั่ง kernel; `Start()`
  ปฏิเสธด้วย sentinel error `ErrTunnelNoToken` (ยังไม่มี token) และ
  `ErrTunnelNotInstalled` (`BinaryInstalled()==false`)
- `Remove()`: Stop → `mgr.RemoveToken` → `repo.Clear`
- `InitApplyConfig()`: เรียกตอน boot — enabled&&มี token → WriteToken+Start,
  ไม่ enabled → Stop (best-effort, error แค่ log warn ห้ามทำ startup ล้ม)
- ทุก mutation log event ผ่าน `EventLogService` (category `system`, action
  `cloudflare-tunnel-<op>`) โดย message ห้ามมี token

**Step 7 — Wiring**
**ไฟล์:** `backend/cmd/pigate/main.go` (แก้)
- ประกาศ `tunnelMgr kernel.TunnelManager` ในกลุ่มเดียวกับ `systemServiceMgr`
  (~บรรทัด 139), assign mock/real ตามสาขา (~159/~190)
- สร้าง `cloudflareTunnelService` ใกล้ `systemServiceService` (~214) ส่งเข้า
  `api.NewServer`
- เรียก `cloudflareTunnelService.InitApplyConfig()` **ท้ายสุด** ของ apply
  chain ตอน boot (หลัง firewall/QoS) เพราะ tunnel พึ่งพา network/DNS ที่ต้อง
  พร้อมก่อน — error แค่ log warn ไม่ทำ boot ล้ม

**Step 8 — API handlers (SENSITIVE)**
**ไฟล์:** `backend/internal/api/cloudflare_tunnel_handlers.go` (ใหม่),
`backend/internal/api/server.go` (แก้)
- `HandleGetCloudflareTunnel` (GET → status JSON มีแค่ `hasToken`+`tokenHint`
  ห้ามมี token เต็มเด็ดขาด), `HandleUpdateCloudflareTunnel` (PUT, ว่าง=คงเดิม),
  `HandleStart/Stop/RestartCloudflareTunnel`, `HandleRemoveCloudflareTunnel` (DELETE)
- map sentinel error → 400 (no token/invalid/not installed), 500 (D-Bus error)
- error message ที่ส่งกลับ client ห้ามสะท้อน token แม้บางส่วน, ห้าม log PUT body

**Step 9 — Routes (super_admin only, รวม GET)**
**ไฟล์:** `backend/internal/api/router.go` (แก้, กลุ่ม `/api/system` ~บรรทัด 179-197)
- `GET/PUT /api/system/cloudflare-tunnel`,
  `POST /api/system/cloudflare-tunnel/{start,stop,restart}`,
  `DELETE /api/system/cloudflare-tunnel` — ทุกเส้นใช้ `superAdminRoute`
  (เหมือน wifi-presets ที่ `router.go:89-96` เพราะ response เกี่ยวข้อง credential)

**Step 10 — install.sh: unit file + polkit allowlist + env baseline (SENSITIVE)**
**ไฟล์:** `install.sh` (แก้)
- STEP ใหม่หลัง STEP 2.2 dhcpcd (~บรรทัด 221): ตรวจ `command -v cloudflared` —
  ไม่พบ → `log_warn` แล้วทำงานต่อ (**ห้าม `exit 1`** เพราะเป็นฟีเจอร์ optional
  ต่างจาก dhcpcd) แต่ยังสร้าง unit file ตามปกติ + `systemctl daemon-reload`
- แก้ polkit rule (`install.sh:259-266`): เพิ่ม `unit === "cloudflared.service"`
  เข้า allowlist **ต้องอยู่ใน if ของ `manage-units` ก่อนบรรทัด `return NO`
  เท่านั้น ห้ามแตะ guard `subject.user != "pigate"`**
- STEP 4 (~บรรทัด 307): สร้าง `/var/lib/pigate/cloudflared.env` ถ้ายังไม่มี
  ด้วย `chown pigate:pigate` + `chmod 0600` (ต่างจาก `dhcpcd.conf` ที่เป็น 0644
  เพราะไฟล์นี้มี secret)
- เพิ่มหมายเหตุท้ายสคริปต์: เครื่องที่ติดตั้งไปแล้วต้องรัน `install.sh` ซ้ำ
  เพื่อได้ unit + polkit rule ใหม่

**Step 11 — Frontend API client**
**ไฟล์:** `frontend/src/services/cloudflareTunnelService.ts` (ใหม่)
- ตาม pattern `systemService.ts`, export `CloudflareTunnelStatus` type +
  `getTunnelStatus/updateTunnelToken/startTunnel/stopTunnel/restartTunnel/removeTunnel`
- ห้ามเก็บ token ลง `localStorage`/`sessionStorage`

**Step 12 — Frontend: หน้าใหม่ + route + sidebar**
**ไฟล์:** `frontend/src/pages/CloudflareTunnel.tsx` (ใหม่, โครงตาม `DnsServer.tsx`),
`frontend/src/App.tsx` (แก้, เพิ่ม route ใต้ `<Route path="system">` ~บรรทัด 194-206),
`frontend/src/components/app-sidebar.tsx` (แก้, กลุ่ม `"System"` ~บรรทัด 157-168)
- หน้าใหม่: Card สถานะ (Badge running/stopped/failed/unavailable, semantic
  color เท่านั้น), Alert เมื่อ `binaryInstalled=false` (disable ปุ่มควบคุม)
  หรือ `unitLoaded=false` (แนะนำรัน install.sh ซ้ำ), Card Token (input
  password + toggle, placeholder=tokenHint, ว่าง=ไม่เปลี่ยน), ปุ่ม
  Start/Stop/Restart/Remove (Remove ต้องมี AlertDialog ยืนยัน), Alert เตือน
  ความปลอดภัยถาวรในหน้า (tunnel = เปิดช่องทางเข้าจากอินเทอร์เน็ตข้าม input chain),
  poll สถานะทุก ~5s ขณะอยู่หน้านี้ (clear ตอน unmount) + refetch หลังทุก action
- `App.tsx`: เพิ่ม `<Route path="cloudflare-tunnel" element={<SuperAdminRoute><CloudflareTunnel /></SuperAdminRoute>} />`
- `app-sidebar.tsx`: เพิ่มรายการเมนู `isSuperAdmin ? [{path:"/system/cloudflare-tunnel", label:"Cloudflare Tunnel", icon: Cloud}] : []`
  ต่อจาก "Settings & Maintenance", import `Cloud` จาก `lucide-react`
- ทุก router import จาก `"react-router"` เท่านั้น, flat design, dark/light,
  ห้าม `console.log` ค่า token
- > ไม่ต้องแก้ `SettingsMaintenance.tsx` เลย (ตัดสินใจโดยเจ้าของโปรเจกต์: ใช้
  > หน้าแยกแทน card ใน Settings)

**Step 13 — Backup/Restore**
**ไฟล์:** `backend/internal/service/backup.go` (แก้), `backend/internal/model/backup.go` (แก้)
- เพิ่ม `cloudflare_tunnel_settings` เข้า `BackupConfig` ตาม pattern
  wifi_presets (`backup.go:136,733`) — **export token ไปด้วย** (ตัดสินใจโดย
  เจ้าของโปรเจกต์ 2026-08-07 สอดคล้องกับ wifi password เดิม) พร้อม comment
  เตือนว่าไฟล์ backup ที่ไม่ได้ใส่ passphrase จะมี tunnel token แบบ plaintext
- ตอน import ต้องเรียก `WriteToken` + apply desired state ใหม่ ไม่ใช่แค่เขียน DB

**Step 14 — เอกสาร**
**ไฟล์:** `docs/openapi.yaml`, `frontend/public/openapi.yaml`, `README.md`,
`docs/ref/cloudflare-tunnel-design.md` (ใหม่)
- เพิ่ม 6 endpoint ให้ sync กันทั้งสองไฟล์ openapi (response schema ต้องไม่มี
  field token เต็ม)
- README Feature Status เพิ่มแถว Cloudflare Tunnel
- design doc สรุปสถาปัตยกรรมที่เลือก, เหตุผลที่ไม่ทำ dynamic unit/exec,
  ข้อกำหนดติดตั้ง cloudflared binary แยก, ขั้นตอน migrate เครื่องที่ติดตั้งแล้ว

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/system/cloudflare-tunnel` | super_admin | สถานะ + `hasToken` + `tokenHint` (ไม่มี token เต็ม) — เส้นใหม่ |
| PUT | `/api/system/cloudflare-tunnel` | super_admin | อัปเดต token (ว่าง=คงเดิม) + restart ถ้ากำลังรัน — เส้นใหม่ |
| POST | `/api/system/cloudflare-tunnel/start` `/stop` `/restart` | super_admin | สั่ง systemd ผ่าน D-Bus — เส้นใหม่ |
| DELETE | `/api/system/cloudflare-tunnel` | super_admin | stop + ลบ env file + ล้าง token ใน DB — เส้นใหม่ |

ทุกเส้น mutation ถูก `DisableEditMiddleware` บล็อกในโหมด `-disable-edit=true`
อยู่แล้ว (พฤติกรรมที่ถูกต้อง สอดคล้องกับ subsystem อื่น)

## 5. ข้อควรระวัง

1. **Env-file injection ผ่าน newline ใน token (ความเสี่ยงสูงสุด)** — ถ้าปล่อย
   token มี `\n` ผู้ใช้ที่เข้าถึง UI จะเขียนบรรทัด env เพิ่มเข้าไฟล์ที่ systemd
   อ่านให้ service (เปลี่ยนพฤติกรรม process ได้) → ป้องกันด้วย whitelist
   charset + ปฏิเสธ control char ทั้งใน model (Step 1) และซ้ำใน kernel (Step 4)
2. **Token ต้องไม่รั่วทาง `ps`/`systemctl show`** — ห้ามใส่ token ใน `ExecStart`
   หรือ D-Bus argument ใช้ `EnvironmentFile` 0600 เท่านั้น
3. **Token ต้องไม่ถูก log** — handler/service/frontend ต้องไม่ log body ของ PUT
   หรือค่า token ใดๆ ทุกจุด (grep ตรวจก่อนปิดงาน)
4. **สิทธิ์ไฟล์** — `/var/lib/pigate` เป็น 775 `pigate:netdev` ดังนั้น env file
   ต้อง `0600` โดยชัดเจน (`os.Chmod` หลังสร้าง กัน umask) ไม่ใช่ 0644 แบบ dhcpcd.conf
5. **Polkit** — rule ปัจจุบันมี guard `subject.user != "pigate" → NOT_HANDLED`
   ที่**ห้ามแตะ** (เคยมีบั๊ก catch-all `return NO` ทำทั้งเครื่องพัง) การเพิ่ม
   `cloudflared.service` ต้องอยู่ใน if ของ `manage-units` ก่อนบรรทัด `return NO` เท่านั้น
6. **cloudflared ควรรันเป็น user แยก ไม่ใช่ root** — ระบุ `User=` ใน unit
   (outbound-only ไม่ต้องการ capability พิเศษ)
7. **เครื่องที่ติดตั้งไปแล้วต้องรัน `install.sh` ซ้ำ** ไม่งั้น start จะล้มด้วย
   polkit denied และ unit ไม่มีอยู่ — ต้องเขียนใน release note และ UI ต้อง
   แสดง "unavailable"/"unit not loaded" อย่างชัดเจนแทน error ดิบ
8. **cloudflared เปิดทางเข้าจากอินเทอร์เน็ตเข้าสู่ LAN โดยข้าม input chain ของ
   firewall** (outbound tunnel) — เปลี่ยน security posture ของอุปกรณ์ ต้องมี
   Alert เตือนถาวรใน UI และบันทึก event log ทุกครั้งที่ start/stop
9. **Output chain default accept วันนี้ใช้ได้ทันที** แต่ถ้าผู้ใช้เพิ่มกฎ
   local-out เข้มในอนาคต tunnel จะตายเงียบ (ต้องออก TCP/UDP 7844 และ 443) —
   ระบุไว้ในเอกสาร design
10. **Mock mode ต้องไม่มี side effect** — `MockTunnelManager` ห้ามเขียนไฟล์/
    เรียก D-Bus จริง (dev รัน `-mock=true` บนเครื่องทำงานจริง)
11. **`--no-autoupdate` เสมอ** — กัน binary self-update เปลี่ยนตัวเองใต้ระบบ
    โดยไม่มีการควบคุม
12. **Backup ที่ export token ไปด้วย** — ไฟล์ backup ที่ไม่ตั้ง passphrase จะมี
    tunnel token เป็น plaintext ต้องเตือนใน UI export

**การทดสอบ:**
- mock mode ครอบคลุมได้: UI ทั้งหมด, validation (newline/charset/ความยาว),
  role 403, DB round-trip, ไม่มี side effect ต่อ OS
- ต้องทดสอบบนบอร์ดจริงเท่านั้น: ติดตั้ง cloudflared จริง + รัน install.sh ใหม่
  → ใส่ token จริง → tunnel ขึ้น active, permission ไฟล์ 0600, `ps`/`systemctl show`
  ไม่มี token, update token → restart อัตโนมัติ, remove → cleanup ครบ,
  reboot → desired state กลับมาตรงเดิม (`InitApplyConfig`) — ความเสี่ยงต่ำ
  (ไม่กระทบ network/firewall/power ของเครื่อง จึงไม่ล็อกตัวเองออกจากเครื่อง)
- `go build ./...` + `go test ./...` และ `yarn build` + `yarn lint` ต้องผ่าน

## 6. Checklist สรุป (Definition of Done)

- [ ] Step 1: `model/cloudflare_tunnel.go` + test (validation กัน newline injection)
- [ ] Step 2: DB migration + `cloudflare_tunnel_repo.go` + migration test
- [ ] Step 3: `TunnelManager` interface ใน `kernel/interfaces.go`
- [ ] Step 4: `RealCloudflaredManager` (`kernel/real_cloudflared.go`)
- [ ] Step 5: `MockTunnelManager` ใน `kernel/mock.go`
- [ ] Step 6: `service/cloudflare_tunnel.go` (sentinel errors, InitApplyConfig, event log)
- [ ] Step 7: wiring ใน `cmd/pigate/main.go`
- [ ] Step 8: API handlers `api/cloudflare_tunnel_handlers.go`
- [ ] Step 9: routes ใน `api/router.go` (superAdminRoute ทุกเส้น)
- [ ] Step 10: `install.sh` (unit file, polkit allowlist, env baseline 0600)
- [ ] Step 11: `frontend/src/services/cloudflareTunnelService.ts`
- [ ] Step 12: `frontend/src/pages/CloudflareTunnel.tsx` + route + sidebar
- [ ] Step 13: backup/restore รวม token
- [ ] Step 14: openapi (2 ไฟล์) + README + design doc
- [ ] ทดสอบ mock mode ครบ flow (start/stop/restart/update/remove/validation/role)
- [ ] ทดสอบบนบอร์ดจริง (permission, ps/systemctl ไม่มี token, reboot persistence)
- [ ] `go build ./...` + `go test ./...` + `yarn build` + `yarn lint` ผ่านทั้งหมด
- [ ] grep ยืนยันไม่มี log/echo ของ token ในทุกไฟล์ที่แก้/สร้าง
