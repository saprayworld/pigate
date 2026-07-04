# Export/Import System — Design & Implementation Plan

เอกสารแผนงานสำหรับทำระบบ Export/Import Configuration ของ PiGate ให้สมบูรณ์
(สถานะ: TODO / วางแผน — สำรวจโค้ดล่าสุดเมื่อ 2026-07-04 บน branch `main`)

---

## 1. สถานะปัจจุบัน (สิ่งที่มีอยู่แล้ว)

ระบบนี้ **ไม่ได้เริ่มจากศูนย์** — มีโครงร่างอยู่แล้วทั้ง backend และ frontend แต่ยังไม่สมบูรณ์:

| ส่วน | ไฟล์ | สถานะ |
|---|---|---|
| API endpoint | `backend/internal/api/router.go:130-131` — `GET /api/system/config/export`, `POST /api/system/config/import` | ลงทะเบียนแล้ว (ผ่าน `authRoute`) |
| Backend handler | `backend/internal/api/handlers.go:1635` (`HandleExportConfig`), `:1669` (`HandleImportConfig`) | มีแบบ naive — ดู gap ข้อ 2 |
| Frontend UI | `frontend/src/pages/SettingsMaintenance.tsx` (~บรรทัด 302-361 logic, ~855-945 UI Card "Backup & Restore") | ใช้งานได้ ครบ export ดาวน์โหลด .json + import อัปโหลดไฟล์ |
| Frontend service | `frontend/src/services/systemService.ts` — `exportConfig()` / `importConfig()` | มีทั้ง mock mode (localStorage) และ real mode |
| OpenAPI | `docs/openapi.yaml:1451` (`/system/config/export`), `:1513` (`/system/config/import`) | มี spec คร่าว ๆ แล้ว ต้อง update ตาม schema ใหม่ |
| DB file backup | `backend/internal/db/connection.go` — `backupDatabase()` สำรองไฟล์ .db ทุกครั้งตอน start | ใช้ pattern นี้ซ้ำได้ตอน import |
| Timezone normalize | `db.NormalizeTimezone()` (`connection.go:503`) — export ไว้ให้ import path ใช้กับ backup เก่า | ใช้อยู่แล้วใน `HandleImportConfig` |

README ระบุ Import/Export = "Mock" ทั้งสองฝั่ง — ความจริงคือ backend มีของจริงบางส่วนแล้ว แต่ครอบคลุมข้อมูลไม่ครบและ flow ไม่ปลอดภัย

## 2. ช่องว่าง (Gap Analysis) ของโค้ดปัจจุบัน

### 2.1 Export (`HandleExportConfig`) — ข้อมูลไม่ครบ + รั่วความลับ

ตาราง config ทั้งหมดใน SQLite (`db/connection.go`) เทียบกับสิ่งที่ export จริง:

| ตาราง | Export แล้ว? | หมายเหตุ |
|---|---|---|
| `address_objects` | ✅ | |
| `service_objects` | ✅ | |
| `firewall_policies` (+`policy_addresses`, `policy_services`) | ✅ | ผ่าน `GetPolicies()` |
| `static_routes` | ✅ | |
| `network_interfaces` | ⚠️ | export ตรงจาก repo → **มี `wifi_password` / `backup_wifi_password` เป็น plaintext** |
| `dhcp_configs` | ❌ **บั๊ก** | ใช้ `GetDHCPConfig()` (legacy คืน row แรก row เดียว) ทั้งที่ตอนนี้เป็น multi-config ต่อ interface → ต้องใช้ `GetDHCPConfigs()` |
| `dhcp_reservations` | ✅ | |
| `dns_zones` + `dns_records` | ❌ ขาด | DNS Server (dnsmasq local zones) |
| `dns_server_settings` | ❌ ขาด | listen interfaces ของ DNS server |
| `system_dns_settings` | ❌ ขาด | System DNS (mode wan/static, primary/secondary, local domain) |
| `qos_rules` | ❌ ขาด | |
| `system_time_settings` | ✅ | |
| `system_hostname_settings` | ✅ | |
| `users` | ❌ (ตั้งใจ?) | ต้องตัดสินใจ — ดู §3.4 |
| `dhcp_leases` | ➖ ไม่ต้อง | runtime state ไม่ใช่ config (ตาม design หลักของโปรเจค) |

ปัญหาอื่นของ export:
- ไม่มี `schemaVersion` สำหรับรองรับ migration ของไฟล์ backup ในอนาคต (มีแค่ `version: "v1.0.0-Release"` hardcode ซึ่งไม่ตรงกับ frontend mock ที่ใช้ `v1.5.0-Release` ด้วย)
- ไม่มี checksum ตรวจไฟล์ถูกแก้/เสียหาย
- error จาก repo ถูก `_` ทิ้งหมด — ถ้าอ่านตารางไหน fail จะได้ backup เงียบ ๆ ที่ข้อมูลหายไปบางส่วน
- **สิทธิ์:** ใช้ `authRoute` → `admin_readonly` GET ได้ → อ่าน Wi-Fi password ทุก interface ได้ (ปกติ `HandleGetInterfaces` mask ผ่าน `maskInterfacePasswords()` แต่ export ไม่ mask) → ต้องเปลี่ยนเป็น `superAdminRoute`

### 2.2 Import (`HandleImportConfig`) — flow อันตราย

- **ไม่มี validation** — decode แล้วยัดลง DB เลย ไม่เช็ค schemaVersion, ไม่ validate ค่า (repo มี `validateAddressObject`/`validateServiceObject` ช่วยได้บางส่วน แต่ policy/route/interface ไม่ผ่าน service validation)
- **ไม่มี transaction** — fail กลางทางได้ DB ครึ่ง ๆ กลาง ๆ, error ทุกจุดถูก `_ =` ทิ้ง แล้วตอบ 200 OK เสมอ
- **Merge แบบชนกัน** — ใช้ `CreateXxx` โดยไม่ลบของเดิม: import ซ้ำ = UNIQUE constraint fail เงียบ ๆ (name ซ้ำ) หรือได้ข้อมูล duplicate; ไม่ใช่ semantic "restore"
- **ไม่ apply ลง kernel** — เขียนแค่ DB (ยกเว้น time settings ที่ผ่าน `timeService.Update`) → nftables/routes/DHCP/DNS/QoS จริงไม่เปลี่ยนจนกว่าจะ restart ทั้งที่ UI ฝั่ง frontend เขียนบอกผู้ใช้ว่า "สั่ง Apply ruleset ใหม่อีกครั้งทันทีหลังการนำเข้า"
- **ไม่ snapshot ก่อนเขียนทับ** — พังแล้วกู้คืนไม่ได้
- **ไม่จำกัดขนาด body** — ควรมี `http.MaxBytesReader`
- ครอบคลุมข้อมูลไม่ครบเหมือนฝั่ง export (ไม่มี DNS zones, QoS, system DNS, DHCP multi-config)

## 3. การออกแบบที่เสนอ

### 3.1 รูปแบบไฟล์ Backup (JSON, schema v2)

```jsonc
{
  "meta": {
    "device": "PiGate Firewall Gateway",
    "hostname": "pigate-rpi5",          // จาก system_hostname_settings
    "appVersion": "v0.1.0-pre",          // version ของ binary
    "schemaVersion": 2,                  // v1 = รูปแบบเดิม (ยังรับ import ได้)
    "exportedAt": "2026-07-04T12:00:00Z",
    "checksum": "sha256:..."             // sha256 ของ field "config" (canonical JSON)
  },
  "config": {
    "interfaces":        [...],
    "staticRoutes":      [...],
    "addresses":         [...],
    "serviceObjects":    [...],
    "policies":          [...],          // รวม sources/destinations/services ใน struct (GetPolicies ทำอยู่แล้ว)
    "dhcpConfigs":       [...],          // plural! จาก GetDHCPConfigs()
    "dhcpReservations":  [...],
    "dnsZones":          [...],          // แต่ละ zone ฝัง records ไว้ใน object เพื่อคง FK
    "dnsServerSettings": {...},          // listen interfaces
    "systemDns":         {...},
    "qosRules":          [...],
    "systemTime":        {...},          // ตัด field Status (live-only) ออก
    "systemHostname":    {...},
    "users":             [...]           // optional — เฉพาะเมื่อผู้ใช้ติ๊ก include users (ดู §3.4)
  }
}
```

- Backward compat: ถ้า decode แล้วไม่มี `meta.schemaVersion` → ตีความเป็น v1 (รูปแบบปัจจุบันที่ field อยู่ระดับบน `systemSettings`/`hostnameSettings`/`config.dhcp.config`) และ map เข้า struct v2 ก่อนประมวลผลต่อ ใช้ `db.NormalizeTimezone` กับ timezone เดิมตามที่ comment ในโค้ดตั้งใจไว้
- ชื่อไฟล์ที่ frontend ตั้งตอนดาวน์โหลด: `pigate-backup-<hostname>-<YYYYMMDD-HHmmss>.json`

### 3.2 Import Semantics: **Replace (wipe & restore)**

เลือก replace ไม่ใช่ merge เพราะเป้าหมายคือ "คืนค่าเครื่องให้เหมือนตอน backup" และตัดปัญหา UNIQUE ชนกัน/duplicate ทั้งหมด ขั้นตอน:

1. **Validate** — decode (จำกัด body เช่น 10 MB), เช็ค schemaVersion ที่รองรับ, ตรวจ checksum (ถ้ามี — v1 ไม่มีก็ข้าม), วิ่ง validation ราย object ก่อนแตะ DB, เช็ค referential integrity ในไฟล์เอง (policy อ้าง address/service id ที่มีในไฟล์, dns_record อ้าง zone ในไฟล์)
2. **Snapshot** — copy ไฟล์ SQLite เป็น `<db>.backup-preimport-<timestamp>` (reuse pattern จาก `backupDatabase()` ใน connection.go — ควร refactor ให้เรียกซ้ำได้)
3. **Restore ใน transaction เดียว** — `BEGIN` → `DELETE` ตาราง config (เว้น `users` ถ้าไม่ได้ import users, เว้น row ที่ `system=1` ของ address/service objects แล้ว insert เฉพาะ custom) → `INSERT` จากไฟล์ → `COMMIT`; error ใด ๆ → `ROLLBACK` แล้วตอบ 4xx/5xx พร้อมรายละเอียด — DB เดิมไม่ถูกแตะ
4. **Re-apply ลง kernel** ตามลำดับเดียวกับ startup ใน `cmd/pigate/main.go` (ลำดับนี้สำคัญ อย่าสลับ):
   time → interfaces → routes → hostname → DHCP → DNS server (zones) → DNS (system) → firewall → QoS
   โดยเรียก `InitApplyConfig()`/`ApplyDNSConfig()` ที่มีอยู่แล้วบน service แต่ละตัว (ทุกตัวถูก inject เข้า `api.Server` อยู่แล้ว — ไม่ต้องแก้ wiring)
   - apply step ที่ fail ให้ **เก็บสะสมแล้วรายงาน** (HTTP 200 + `warnings: []`) ไม่ rollback DB เพราะ DB คือ source of truth ใหม่แล้ว และ reboot เครื่องจะ apply ซ้ำเองตาม startup pattern
5. **Response** สรุปผล: จำนวน object ต่อ section, warnings, และธงบอกว่า interface config เปลี่ยน (frontend ใช้เตือนให้ผู้ใช้เตรียม reconnect)

### 3.3 ความลับในไฟล์ Backup (Wi-Fi passwords)

ไฟล์ backup ต้องมี wifi password จริง มิฉะนั้น restore แล้ว Wi-Fi WAN ใช้ไม่ได้ ข้อเสนอ (ทำเป็นขั้น):

- **Phase 1 (ขั้นต่ำ):** export password จริงได้ แต่ (ก) จำกัด endpoint เป็น `superAdminRoute` ทั้ง export/import (ข) frontend แสดงคำเตือนชัดเจนว่าไฟล์มีรหัสผ่าน Wi-Fi ให้เก็บรักษาอย่างปลอดภัย
- **Phase 2 (ทางเลือก, แนะนำ):** รองรับ passphrase encryption — ผู้ใช้ใส่ passphrase ตอน export → เข้ารหัสทั้ง `config` ด้วย AES-256-GCM (key จาก `golang.org/x/crypto` Argon2id/scrypt — อยู่ในตระกูล dependency ที่โปรเจคใช้อยู่แล้ว) → ตอน import ถ้า `meta.encrypted=true` ให้ถาม passphrase ห้ามคิดฟีเจอร์ mask-password-in-backup เพราะ restore แล้วใช้ไม่ได้จริง

### 3.4 Users ในไฟล์ Backup

- ค่า default: **ไม่ export users** (bcrypt hash ก็คือ credential material; และ import users ข้ามเครื่องเสี่ยง lock-out)
- มี checkbox "รวมบัญชีผู้ใช้" ตอน export; ตอน import ถ้าไฟล์มี users:
  - แสดงเตือนก่อน apply
  - ห้ามลบ/ปิด account ของผู้ที่กำลัง import (actor) — pattern เดียวกับ `CountActiveSuperAdmins()` ใน user service ที่กันการปิด super_admin คนสุดท้ายอยู่แล้ว
  - หลัง import users → purge sessions ของ user ที่หายไป/ถูกปิด (`RemoveSessionsForUser` มีอยู่แล้วใน api)

### 3.5 Interface Import ข้ามเครื่อง

Interface config ผูกกับชื่อ (`eth0`, `wlan0`) และ MAC ของเครื่องเดิม:
- Match ด้วย `name` — interface ในไฟล์ที่ไม่มีบนเครื่องจริง → **skip + warning** (อย่าสร้าง row ผี เพราะ `InitApplyConfigurationAtStartup` / netlink monitor จะสับสน)
- ไม่ restore field ที่เป็น runtime/hardware identity: `status`, `speed`, `mac_address`/`real_mac_address` ใช้ของเครื่องจริง (restore เฉพาะ `mac_mode`, `laa_mac_address`, `randomized_mac` ที่เป็น "config")
- การ apply interface ใหม่อาจ **ตัดการเชื่อมต่อของ admin เอง** (เปลี่ยน IP ของ LAN ที่กำลังใช้เข้า UI) — frontend ต้องเตือนก่อนยืนยัน

## 4. แผนงานเป็นขั้นตอน (ไฟล์ที่ต้องแก้/สร้าง)

### Phase 1 — Backend: Export ให้ครบและปลอดภัย (งานเล็ก เห็นผลไว)

1. **`backend/internal/model/backup.go` (ใหม่)** — struct `BackupFile`, `BackupMeta`, `BackupConfig` (typed ทั้งหมด เลิกใช้ `map[string]interface{}`), `DNSZoneBackup` (zone + records ฝังใน object)
2. **`backend/internal/service/backup.go` (ใหม่)** — `BackupService` รับ `*db.Repository` + service อื่นที่ต้องใช้ apply:
   - `Export(includeUsers bool) (*model.BackupFile, error)` — อ่านทุกตารางตาม §3.1, **เช็ค error ทุกตัว**, ใช้ `GetDHCPConfigs()` (plural), คำนวณ checksum
   - ย้าย logic ออกจาก handler ให้เหลือ handler บาง ๆ ตาม layering ของโปรเจค (api → service → db)
3. **`backend/internal/api/handlers.go`** — เขียน `HandleExportConfig` ใหม่ให้เรียก `backupService.Export()`; ตั้ง header `Content-Disposition: attachment; filename=...` เพื่อให้ browser ดาวน์โหลดตรงได้ด้วย
4. **`backend/internal/api/router.go`** — เปลี่ยน 2 เส้นทาง export/import จาก `authRoute` → `superAdminRoute`
5. **`backend/cmd/pigate/main.go`** — สร้าง `backupService` แล้วส่งเข้า `api.NewServer` (เพิ่ม field ใน `Server` struct + parameter — ไฟล์ `handlers.go` บรรทัด 22-82)
6. **Tests:** `backend/internal/service/backup_test.go` — export กับ `:memory:` DB ที่ seed แล้ว ตรวจว่าครบทุก section / error propagation

### Phase 2 — Backend: Import แบบ validate → snapshot → transaction → re-apply

7. **`backend/internal/db/backup_repo.go` (ใหม่)** — method บน `Repository`:
   - `RestoreConfig(cfg model.BackupConfig, includeUsers bool) error` — ทำ wipe+insert ทุกตารางใน **transaction เดียว** (ต้องคุมลำดับ FK: ลบ `policy_addresses`/`policy_services`/`dns_records` ก่อนแม่; insert แม่ก่อนลูก; คง system objects)
   - Refactor `backupDatabase()` ใน `connection.go` ให้ export ใช้ซ้ำเป็น pre-import snapshot ได้ (เปลี่ยน suffix ได้ เช่น `.backup-preimport-...`)
8. **`backend/internal/service/backup.go`** — `Import(raw []byte, opts ImportOptions) (*ImportResult, error)`:
   - decode + ตรวจ schemaVersion (รองรับ v1 เดิม → map เป็น v2), checksum, validation ราย object, in-file referential integrity
   - filter interfaces ตาม §3.5 (match ชื่อกับเครื่องจริงผ่าน `InterfaceService`)
   - snapshot DB → `repo.RestoreConfig(...)` → re-apply kernel ตามลำดับ §3.2 ข้อ 4 → คืน `ImportResult{Counts, Warnings}`
9. **`backend/internal/api/handlers.go`** — เขียน `HandleImportConfig` ใหม่: `http.MaxBytesReader` (10 MB), เรียก service, ตอบ error จริงแทน 200-เสมอ; ถ้า import users → purge sessions ตาม §3.4
10. **Tests:** import round-trip (export → wipe → import → export อีกครั้งต้องเท่ากัน), import ไฟล์พัง/schema ผิด → DB ไม่เปลี่ยน (ตรวจ rollback), import v1 backup เก่า, interface ข้ามเครื่อง → ถูก skip พร้อม warning

### Phase 3 — Frontend

11. **`frontend/src/services/systemService.ts`** — พิมพ์ type `BackupFile`/`ImportResult` แทน `any`; `exportConfig(includeUsers)`; `importConfig` คืน `ImportResult`; อัปเดต mock mode ให้ payload หน้าตาเดียวกับ backend v2 (ตอนนี้ mock กับ backend คนละ shape กันอยู่)
12. **`frontend/src/pages/SettingsMaintenance.tsx`** —
    - Export: checkbox "รวมบัญชีผู้ใช้", คำเตือนว่าไฟล์มีรหัสผ่าน Wi-Fi, ตั้งชื่อไฟล์ตาม §3.1
    - Import: เปลี่ยนจาก submit ตรง → เปิด `AlertDialog` ยืนยัน (ใช้ `useAlert` provider ที่มีอยู่) แสดง preview จาก meta ในไฟล์ (hostname เครื่องต้นทาง, exportedAt, จำนวน object) + เตือนว่าจะเขียนทับทั้งหมดและอาจหลุดการเชื่อมต่อ; หลังสำเร็จแสดง warnings จาก `ImportResult`
    - ใช้ shadcn/ui primitives เท่านั้น, สี status ผ่าน theme variables (ห้าม `text-red-400` hardcode เพิ่ม — ของเดิมในหน้านี้มีอยู่ อย่า copy pattern นั้น), Dialog ที่มี portal component ใช้ `modal={false}` ตาม `docs/rules_of_work.md`
13. **UI role-gating:** ซ่อน/disable Card Backup & Restore สำหรับ `admin_readonly` (endpoint เป็น superAdmin แล้ว UI ควรสอดคล้อง)

### Phase 4 — เอกสาร + เก็บงาน

14. **`docs/openapi.yaml`** — อัปเดต schema ของ `/system/config/export` + `/system/config/import` (request/response, error codes, หมายเหตุ super_admin only); copy ไป `frontend/public/openapi.yaml` ด้วย (มี 2 ที่)
15. **`README.md`** — เปลี่ยน Feature Status ของ Import/Export เมื่อเสร็จ
16. (ถ้าทำ §3.3 Phase 2) เพิ่ม encryption แบบ opt-in — เป็นงานแยกหลังระบบหลักนิ่ง

## 5. ข้อควรระวัง (Cautions)

1. **ห้ามลืมเปลี่ยนเป็น `superAdminRoute`** — นี่คือช่องรั่ว Wi-Fi password ต่อ `admin_readonly` ที่มีอยู่ *ตอนนี้* ควรแก้เป็นอย่างแรกแม้ยังไม่ทำส่วนอื่น
2. **อย่า restore แล้วไม่ apply / apply แล้วไม่ restore** — DB คือ source of truth ของโปรเจค kernel state ต้อง reconcile จาก DB เสมอ (netlink monitor ก็ทำงานบนสมมติฐานนี้) ลำดับ apply ต้องตรงกับ `main.go` startup
3. **Netlink monitor กำลังวิ่งอยู่ระหว่าง import** — การเปลี่ยน routes/interfaces ชุดใหญ่ระหว่าง monitor ทำงานอาจโดน reconcile สวน ตรวจว่า `InitApplyConfig` ของ routing ปลอดภัยเมื่อถูกเรียกซ้ำขณะ monitor ทำงาน (มัน idempotent ระดับ startup อยู่แล้ว แต่ startup เรียกก่อน monitor start — import เรียกหลัง) ถ้าจำเป็นให้เพิ่มกลไก pause/resume ที่ `service/netlink_monitor.go`
4. **System objects** (`address_objects.system=1`, `service_objects.type='system'`, routes `type IN ('system','defaultgateway')`) — seed โดยระบบ อย่าลบ/เขียนทับจากไฟล์ import (โค้ดเดิม skip ตอน insert แล้ว แต่ตอนเปลี่ยนเป็น wipe ต้อง **ไม่ wipe row เหล่านี้** ด้วย)
5. **Policy FK RESTRICT** — `policy_addresses.address_id` เป็น `ON DELETE RESTRICT` → ตอน wipe ต้องลบ junction tables/policies ก่อน address/service objects มิฉะนั้น transaction fail
6. **อย่า persist ค่า live-only** — `SystemTimeSettings.Status` ต้องถูกตัดก่อนเก็บ (โค้ดเดิมทำถูกแล้ว คงไว้), `dhcp_leases` ไม่อยู่ใน backup
7. **Import ตัดขา admin ได้ 2 ทาง** — (ก) interface IP เปลี่ยน (ข) users ถูกแทนที่ ต้องมี guard ฝั่ง backend (ไม่ปิด actor) + คำเตือนฝั่ง frontend ทั้งคู่
8. **ห้ามใช้ `exec.Command`** ทุกขั้นตอน re-apply ต้องผ่าน service → kernel interface เดิม (Netlink/D-Bus) เท่านั้น และ mock (`kernel/mock.go`) ต้องยังทำงานได้ครบ flow เพื่อทดสอบบนเครื่อง dev ด้วย `-mock=true`
9. **`-disable-edit` mode** — `DisableEditMiddleware` ครอบทั้ง mux อยู่แล้ว POST import จะโดนบล็อกเอง (ถูกต้อง) แต่ GET export ยังทำงาน — ตั้งใจให้เป็นแบบนั้น (read-only ก็ backup ได้) และเอกสารต้องระบุ
10. **ขนาด/ความทนทานของ input** — จำกัด body size, ห้าม trust ค่าจากไฟล์ (เช่น id ที่ยาวผิดปกติ, timezone แปลก) — วิ่ง validation ชุดเดียวกับ API ปกติ, CHECK constraints ใน SQLite ช่วยเป็นตาข่ายชั้นสุดท้าย
11. **SD card wear** — snapshot ก่อน import เป็นการ copy ไฟล์ DB ทั้งไฟล์ ยอมรับได้เพราะ import เป็นเหตุการณ์นาน ๆ ครั้ง แต่อย่าทำ snapshot อัตโนมัติถี่กว่านั้น
12. **Mock mode ฝั่ง frontend** (`IS_MOCK_MODE`) เก็บของใน localStorage คนละ shape กับ backend — เมื่อเปลี่ยน schema เป็น v2 ต้องแก้ mock ให้ตรงกัน ไม่งั้นทดสอบ UI แล้วเจอ shape mismatch

## 6. ลำดับการทดสอบ (Definition of Done)

- `cd backend && go test ./...` ผ่าน รวม test ใหม่: export ครบ section, import round-trip, rollback เมื่อไฟล์พัง, v1 compat, interface skip
- ทดสอบ end-to-end บน dev ด้วย `-mock=true`: สร้าง config หลากหลาย (policy+address+service, DHCP 2 interface, DNS zone+records, QoS, static route) → export → ลบ DB → start ใหม่ → import → ตรวจทุกหน้า UI ว่าข้อมูลกลับมาครบและ `POST /api/*/apply` ไม่ error
- ทดสอบ import ไฟล์ backup รุ่นเก่า (v1) ที่ export จากโค้ดปัจจุบัน
- `cd frontend && yarn lint && yarn build` ผ่าน
- ตรวจสิทธิ์: login ด้วย `admin_readonly` → เรียก export/import ต้องได้ 403
