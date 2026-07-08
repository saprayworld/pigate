# Central Event Log — ระบบ log กลางรวบรวมเหตุการณ์ทั้งระบบ

> เอกสารแผนงานสำหรับฟีเจอร์: สร้าง **ระบบ event log กลาง** ที่รวบรวมเหตุการณ์สำคัญ
> ทั้งหมด — Security Events (Login Success/Failed, Password Changed, User
> Created/Updated/Deleted), DHCP lease assignment, Network Changed, Firewall
> Changed, Reboot/Shutdown, Config Import/Export — เก็บลง SQLite แบบถนอม SD card
> พร้อมหน้า UI ใหม่สำหรับดู/กรอง log
>
> วันที่เขียน: 2026-07-08 · Branch อ้างอิง: `main`
> สถานะใน README Feature Status: ยังไม่มีแถวนี้ → เป้าหมายคือเพิ่มแถว "Event Log" = Completed

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย:** ทุกเหตุการณ์สำคัญในระบบถูกบันทึกผ่านจุดเดียว (`EventLogService`)
โดย:

- **Persist ข้าม reboot** — เหตุการณ์ audit (login failed, reboot, user created)
  คือของที่มีค่าที่สุด *หลัง* เครื่องดับ/ถูกโจมตี — ring buffer เดิมเก็บใน RAM
  จึงหายหมด (comment ใน `handlers.go:1684-1686` ยอมรับข้อจำกัดนี้เอง)
- **ถนอม SD card** — เขียนแบบ async batch + จำกัดจำนวนแถว ไม่เขียนถี่
  (constraint หลักจาก `docs/tech_stack_design.md` §8)
- admin ดู log ผ่านหน้า UI ใหม่ กรองตาม category/severity ได้ มี pagination

**นอกขอบเขต:**
- **Firmware Upgrade event** — ฟีเจอร์ firmware upgrade ยังไม่มีในโค้ดเลย
  (grep แล้วไม่พบ) — จองชื่อ action `system.firmware_upgrade` ไว้ใน design
  แต่ไม่มี hook ให้เขียนตอนนี้
- **Firewall packet log (PASS/DROP รายแพ็กเก็ต)** — คนละชนิดกับ event log
  (ความถี่สูงมาก ห้ามลง SQLite) ring buffer `FirewallLog` เดิม + หน้า Dashboard
  Recent Logs คงไว้ตามเดิม การต่อ NFLOG จริงเป็นงานแยกอีกแผน
- **Remote syslog forwarding / export log เป็นไฟล์** — ค่อยต่อยอดทีหลัง
- **SSE stream ของ event log** — หน้าใหม่ใช้ polling ตาม pattern `usePoll`
  ที่ทุกหน้าใช้อยู่ (SSE เดิมใน `HandleLogStream` ก็ stream แค่รายการล่าสุดซ้ำ ๆ
  ทุก 3 วิ — ไม่ใช่ pattern ที่ควรลอก)

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ |
|---|---|
| ระบบ log กลาง | **ไม่มี** — มีแค่ `logs/ringbuffer.go` (RAM, capacity 50, ผูกกับ type `model.FirewallLog` ตายตัว) |
| ข้อมูลใน ring buffer | **เกือบ mock ทั้งหมด** — seed ปลอม 2 รายการที่ `cmd/pigate/main.go:61-62`; ตัวเขียนจริงมีตัวเดียวคือ `logPowerEvent` (`handlers.go:1687-1701`) ที่ยัด power event ลงไปใน shape ของ FirewallLog |
| Login success/failed | **ไม่ log** — `HandleLogin` (`handlers.go:125-176`) ตัดสิน success/fail ครบทุก branch แต่ไม่บันทึกอะไร |
| Password changed | **ไม่ log** — `HandleChangePassword` (`handlers.go:1518-1557`) |
| User created/updated/deleted/toggled | **ไม่ log** — `handlers.go:1583-1645` (actor username มีใน context แล้วทุกตัว) |
| DHCP lease events | **มี hook พร้อมใช้แล้ว** — `service/dhcp_server.go:106-128` `StartLeaseWatcher` รับ D-Bus callback (event "DhcpLeaseAdded"/"DhcpLeaseDeleted") upsert ลง DB อยู่แล้ว แค่แทรก log เพิ่ม |
| Network/Firewall/Route changed | **ไม่ log** — handlers ครบทุกตัว (ดูตาราง §3 Step 6) |
| Reboot/Shutdown | **log ลง RAM เท่านั้น** — `logPowerEvent` หายหลังรีบูต |
| DB | ตาราง `system_events` **ยังไม่มี**; `connection.go` ใช้ WAL mode แล้ว (บรรทัด 42), pattern `CREATE TABLE IF NOT EXISTS` ใน `migrate()` (~บรรทัด 195+) |
| API | มีแค่ `/api/dashboard/logs*` (`router.go:40-42`) ซึ่งเป็นของ FirewallLog |
| Frontend | **ไม่มีหน้า Logs** — sidebar (`app-sidebar.tsx:42-76`) มี group Overview/Network/Policy/System; ไม่มี `logService.ts` |
| Kernel layer | **ไม่เกี่ยว** — event log ไม่แตะ OS เลย ไม่ต้องมี interface ใหม่ใน `kernel/interfaces.go` |

สรุป: **สร้างใหม่เกือบทั้งเส้น** (model → db → service → handlers hook → API →
frontend) แต่ไม่มีงาน kernel/install.sh/Polkit เลย — เป็นงาน pure Go + React

---

## 2. แนวทางเทคนิค

### 2.1 โครงสร้างข้อมูล: `model.SystemEvent`

```go
// SystemEvent is a single audit/event log entry, persisted to SQLite.
type SystemEvent struct {
    ID       int64  `json:"id"`
    Time     string `json:"time"`     // RFC3339 UTC
    Category string `json:"category"` // auth|user|network|firewall|route|dhcp|dns|qos|system|config
    Action   string `json:"action"`   // e.g. "login.failed", "dhcp.lease.add"
    Severity string `json:"severity"` // info|warning|error|critical
    Actor    string `json:"actor"`    // username หรือ "system"
    Target   string `json:"target"`   // สิ่งที่ถูกกระทำ เช่น ชื่อ user/interface/policy
    Message  string `json:"message"`  // ข้อความอ่านง่ายสำหรับ UI
}
```

### 2.2 Storage: hybrid — RAM buffer + async batch เขียน SQLite

`EventLogService` (service layer, ไฟล์ใหม่ `service/event_log.go`) ถือ:

1. **In-memory ring buffer** ของ `SystemEvent` (ยกระดับ `logs.RingBuffer`
   ให้ generic หรือเพิ่ม buffer ตัวที่สองใน package `logs`) — ตอบ query
   "ล่าสุด N รายการ" เร็ว ไม่แตะดิสก์
2. **Async batch writer** — goroutine เดียว, flush ลงตาราง `system_events`
   เมื่อครบ ~10 รายการ หรือทุก ~10 วินาที (แล้วแต่อะไรถึงก่อน) ใน
   transaction เดียว → ลดจำนวน write ลง SD card
3. **`Flush()` แบบ synchronous** — สำหรับเหตุการณ์ critical ที่ process กำลังจะ
   ตาย (reboot/shutdown) ต้อง flush ให้จบก่อน return
4. **Retention** — หลัง flush เช็คจำนวนแถว ถ้าเกิน cap (เช่น 10,000)
   ลบเก่าสุดทิ้งเป็น batch (`DELETE ... WHERE id <= x`) — กันตารางโตไม่จำกัด

เหตุผลที่เลือก และทางเลือกที่ตัดทิ้ง:

- **ทำไมไม่ RAM-only (แบบ ring buffer เดิม):** เหตุการณ์ที่ user ต้องการ
  (reboot, login failed, user created) คือเหตุการณ์ที่ *ต้องรอด* ข้าม reboot —
  RAM-only ตอบโจทย์ไม่ได้โดยนิยาม
- **ทำไมไม่เขียน SQLite ตรง ๆ ทุก event:** ขัด constraint ถนอม SD card;
  login brute force จะกลายเป็น write storm ทันที
- **ทำไมไม่ journald/syslog:** query จาก UI ยาก, ต้อง parse format ภายนอก,
  เพิ่ม dependency — SQLite มีอยู่แล้วและ backup/retention ควบคุมเองได้
- **ทำไม hook ที่ handler layer ไม่ใช่ใน service ทุกตัว:** mutation ทุกอย่าง
  วิ่งผ่าน handler อยู่แล้ว และ handler มี actor username จาก
  `r.Context().Value(UserContextKey)` พร้อมใช้ — ถ้า hook ใน service ต้องส่ง
  actor ทะลุทุก method signature ทั้งโปรเจกต์ (ยกเว้น 2 จุดที่เหตุการณ์เกิดใน
  service เอง: DHCP lease watcher และ boot event)

**Pattern ต้นแบบ:** โครง service + constructor ตาม `service/power.go`
(service เล็ก ไม่มี kernel manager); pattern ตาราง+repo ตาม `db/repository.go`
ของ `dhcp_leases`; pattern goroutine พื้นหลังดู `service/netlink_monitor.go`

---

## 3. ขั้นตอนการทำ (เรียงลำดับ + ไฟล์ที่ต้องแก้)

### Step 1 — เพิ่ม `model.SystemEvent`
**ไฟล์:** `backend/internal/model/types.go` (~บรรทัด 293 ถัดจาก `FirewallLog`)
struct ตาม §2.1 + ค่าคงที่ severity/category เป็น `const` กันพิมพ์ผิด

### Step 2 — ตาราง + index ใน DB
**ไฟล์:** `backend/internal/db/connection.go` — เพิ่มใน `migrate()` (กลุ่ม
`CREATE TABLE IF NOT EXISTS` ~บรรทัด 195+):

```sql
CREATE TABLE IF NOT EXISTS system_events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    ts TEXT NOT NULL, category TEXT NOT NULL, action TEXT NOT NULL,
    severity TEXT NOT NULL, actor TEXT, target TEXT, message TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_system_events_ts ON system_events(id DESC);
CREATE INDEX IF NOT EXISTS idx_system_events_category ON system_events(category);
```

### Step 3 — Repository methods
**ไฟล์:** `backend/internal/db/repository.go` — เพิ่ม
`InsertSystemEvents(events []model.SystemEvent) error` (รับเป็น batch,
transaction เดียว), `GetSystemEvents(filter, limit, offset)`,
`CountSystemEvents(filter)`, `PruneSystemEvents(keep int)`,
`ClearSystemEvents()`

### Step 4 — `EventLogService`
**ไฟล์ใหม่:** `backend/internal/service/event_log.go`

- `NewEventLogService(repo *db.Repository) *EventLogService`
- `Log(category, action, severity, actor, target, message string)` —
  ใส่ timestamp UTC, push เข้า RAM buffer + คิวของ batch writer (channel)
- `Start(ctx context.Context)` — goroutine flush ตามเงื่อนไข §2.2
- `Flush()` — synchronous สำหรับ power event
- `Query(filter) ([]model.SystemEvent, total int, error)` — อ่านจาก SQLite
- `Clear(actor string)` — ลบทั้งตาราง แล้ว log event `config.logs_cleared`
  ทันที (การลบ audit trail ต้องทิ้งรอยไว้เสมอ)

### Step 5 — wiring ใน main
**ไฟล์:** `backend/cmd/pigate/main.go`

- สร้าง `eventLogService := service.NewEventLogService(repo)` หลัง `repo`
  พร้อม (~บรรทัด 121 กลุ่มสร้าง service) แล้ว `Start()` goroutine
- ส่งเข้า `api.NewServer(...)` (~บรรทัด 148) และ setter/parameter ให้
  `dhcpServerService` (Step 7)
- หลัง server พร้อม (ก่อน `ListenAndServe`) log event แรก:
  `system` / `system.boot` / `info` / actor `system` — เป็นหลักฐานว่าบูตเสร็จ
  (คู่กับ event `system.reboot` ที่ persist ไว้ก่อนดับ)
- ลบ seed ปลอม 2 บรรทัด (`main.go:61-62`) — ดูข้อควรระวัง §5.7

> ฟีเจอร์นี้ **ไม่ต้อง** แตะ `kernel/interfaces.go` (ไม่คุยกับ OS),
> **ไม่ต้อง** แก้ `install.sh`/Polkit (ไม่มีสิทธิ์ใหม่), **ไม่ต้อง** มี
> `InitApplyConfig()` (ไม่มี state ต้อง apply ลง kernel ตอน boot) และ
> **ไม่เกี่ยวกับ** `netlink_monitor.go` (optional: จะให้ monitor log เหตุการณ์
> self-heal route เป็น phase ถัดไปได้)

### Step 6 — Hook ใน handlers
**ไฟล์:** `backend/internal/api/handlers.go` — เพิ่ม field
`eventLog *service.EventLogService` ใน `Server` struct (~บรรทัด 24) + parameter
ใน `NewServer` (~47) แล้ว helper สั้น ๆ ที่ดึง actor จาก context เอง:

```go
func (s *Server) logEvent(r *http.Request, category, action, severity, target, msg string)
```

จุด hook (log เฉพาะเมื่อ operation **สำเร็จ** ยกเว้น login.failed):

| เหตุการณ์ | action | จุด hook |
|---|---|---|
| Login สำเร็จ | `login.success` (info) | `HandleLogin` ~171 |
| Login ล้มเหลว (รหัสผิด/บัญชี disabled) | `login.failed` (warning) | `HandleLogin` ~146, ~153 |
| เปลี่ยนรหัสผ่าน | `auth.password_changed` (info) | `HandleChangePassword` ~1556 |
| สร้าง/แก้/ลบ/toggle user | `user.created` ฯลฯ (info; ลบ = warning) | `HandleCreateUser` 1583, `HandleUpdateUser` 1597, `HandleDeleteUser` 1612, `HandleToggleUser` 1629 |
| แก้/toggle interface | `network.interface_changed` (info) | `HandleUpdateInterface` 354, `HandleToggleInterface` 591 |
| สร้าง/แก้/ลบ/apply firewall policy | `firewall.policy_*`, `firewall.applied` (info) | 720, 748, 783, `HandleApplyPolicies` 833 |
| สร้าง/แก้/ลบ/apply static route | `route.*` (info) | 1018, 1046, 1102, `HandleApplyRoutes` 1151 |
| แก้/apply DHCP config | `dhcp.config_changed` (info) | 1180, `HandleApplyDHCP` 1281 |
| แก้ DNS / apply DNS server | `dns.*` (info) | 1491, `HandleApplyDNSServer` 2122 |
| เปลี่ยน hostname / time | `system.hostname_changed`, `system.time_changed` | 1463, 1394 |
| Export/Import config | `config.exported`, `config.imported` (warning) | 1707, 1733 |
| Reboot/Shutdown | `system.reboot`, `system.shutdown` (critical) | 1662, 1674 — **แทนที่ `logPowerEvent` เดิมทั้งฟังก์ชัน** แล้วเรียก `eventLog.Flush()` ก่อน `powerService.*` (ดู §5.2) |

ไม่ต้อง hook ครบทุก endpoint ย่อย (เช่น address/service object รายตัว) ใน
phase แรก — เอา 13 กลุ่มตามที่ user ระบุก่อน ที่เหลือเพิ่มทีหลังได้เพราะ
ช่องทางเดียวกันหมด

### Step 7 — Hook DHCP lease events
**ไฟล์:** `backend/internal/service/dhcp_server.go` (~บรรทัด 106-128)
เพิ่ม field `eventLog *EventLogService` (ส่งผ่าน constructor หรือ setter ใน
main.go) แล้วใน callback ของ `StartLeaseWatcher`: event add →
`dhcp.lease.add` (info, target = MAC, message มี IP+hostname), delete →
`dhcp.lease.remove` — **ไม่ log lease renew** (ดู §5.3)

### Step 8 — API routes
**ไฟล์:** `backend/internal/api/router.go` (~บรรทัด 42 ต่อจากกลุ่ม dashboard)

- `authRoute("GET /api/logs/events", s.HandleGetSystemEvents)` — query params:
  `category`, `severity`, `q` (คำค้นใน message), `limit` (default 50, max 200),
  `offset`; ตอบ `{ events: [...], total: n }`
- `superAdminRoute("POST /api/logs/events/clear", s.HandleClearSystemEvents)` —
  ลบ audit trail ได้เฉพาะ super_admin
- handler สองตัวใหม่ใน `handlers.go` (วางใกล้ `HandleGetRecentLogs` ~310)

### Step 9 — อัปเดตเอกสาร API
**ไฟล์:** `docs/openapi.yaml` และ `frontend/public/openapi.yaml`
(sync สองไฟล์เหมือนเดิม) — เพิ่ม path `/logs/events`, `/logs/events/clear`
พร้อม schema `SystemEvent` และระบุ role

### Step 10 — Frontend: service + หน้า Logs + sidebar
**ไฟล์ใหม่:** `frontend/src/services/logService.ts` — ตาม pattern
`dashboardService.ts` (มี mock branch ผ่าน `services/config.ts` ด้วย —
gen mock events ในหน่วยความจำ)
**ไฟล์ใหม่:** `frontend/src/pages/EventLogs.tsx` — ตาราง shadcn (`Table`,
`Badge` สี severity ผ่าน semantic variables, `Select` filter category/severity,
`Input` ค้นหา, ปุ่ม Clear เฉพาะ super_admin) + polling ด้วย `usePoll`
เหมือน `Dashboard.tsx:602`
**ไฟล์:** `frontend/src/App.tsx` (~บรรทัด 138+) เพิ่ม route `system/logs`;
`frontend/src/components/app-sidebar.tsx` (~บรรทัด 66-73) เพิ่ม
`{ path: "/system/logs", label: "Event Logs", icon: ScrollText }` ใน group System

### Step 11 — อัปเดตสถานะโปรเจกต์
**ไฟล์:** `README.md` (เพิ่มแถว Event Log ใน Feature Status),
`docs/project_status.md` ถ้ามี entry เกี่ยวข้อง

### Step 12 (optional, phase ถัดไป)
- ให้ `netlink_monitor.go` log เหตุการณ์ self-heal route (`network.route_healed`)
- hook address/service object CRUD
- Export log เป็น CSV / remote syslog

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | พฤติกรรม |
|---|---|---|---|
| GET | `/api/logs/events` | ทุก role ที่ login (authRoute) | อ่าน event + filter + pagination (เส้นใหม่) |
| POST | `/api/logs/events/clear` | super_admin เท่านั้น | ลบทั้งตาราง + log `config.logs_cleared` (เส้นใหม่) |

- โหมด `-disable-edit=true`: GET ยังใช้ได้, clear ถูก `DisableEditMiddleware`
  บล็อกอัตโนมัติ (POST ทั้งระบบ) — ถูกต้องตามต้องการ ไม่ต้องแก้
- เส้น `/api/dashboard/logs*` เดิม **ไม่แตะ** — ยังเป็นของ FirewallLog บน Dashboard

---

## 5. ข้อควรระวัง

1. **SD card wear คือ constraint อันดับหนึ่ง** — ถ้าเขียน SQLite ต่อ 1 event
   ตอนโดน login brute force จะกลายเป็น write ต่อวินาทีจำนวนมาก → ต้องเป็น batch
   writer เท่านั้น (§2.2) และ RateLimitMiddleware ที่ครอบ `/api/auth/login`
   อยู่แล้ว (`router.go:11`) ช่วยจำกัดเพดานอีกชั้น
2. **Reboot/Shutdown ต้อง `Flush()` ก่อนสั่ง power** — `PowerService` หน่วง
   D-Bus จริง ~1 วินาทีหลังตอบ 200 (ดู `power.go`) ถ้า event ยังค้างในคิว batch
   ตอน logind ฆ่า process → event หาย เหมือนปัญหาเดิมของ `logPowerEvent` เป๊ะ ๆ
   ลำดับใน handler ต้องเป็น: log → Flush (synchronous) → `powerService.Reboot()`
3. **DHCP renew จะ flood log** — dnsmasq ยิง lease event ตอน renew ด้วย
   (ครึ่งอายุ lease ทุกเครื่อง) ถ้า log ทุก event ตาราง log จะเต็มไปด้วย lease
   ซ้ำ ๆ → log เฉพาะ add ที่ MAC/IP ยังไม่อยู่ใน DB (เทียบก่อน upsert ใน
   `dhcp_server.go:118`) และ delete เท่านั้น
4. **ห้าม log ความลับ** — ห้ามมี password/hash/token โผล่ใน message เด็ดขาด
   รวมถึงกรณี login failed: log แค่ username ที่พยายามใช้ ห้ามแตะ field
   password (ระวังคนพิมพ์ password ลงช่อง username — เป็นข้อมูลใน log ที่
   ยอมรับกันทั่วไปสำหรับ appliance ภายใน แต่ห้ามเผยแพร่ log ออกนอกเครื่อง)
5. **Backup/Restore (schema v2) ไม่รวม `system_events`** — เป็น history
   ไม่ใช่ config, restore ข้ามเครื่องแล้ว log เครื่องอื่นมาปนจะหลอก audit —
   ตรวจว่า `service/backup.go` ไม่กวาดตารางใหม่เข้าไป (Export เป็น typed
   struct รายตาราง จึงไม่โดนโดยอัตโนมัติ แต่ต้อง confirm) และ Import ต้อง
   log event `config.imported` หลังสำเร็จ
6. **การลบ log เป็น operation ทำลาย audit trail** — ต้องเป็น superAdminRoute
   แบบ explicit (เหมือน export/import ที่ `router.go:133-139`) และ `Clear()`
   ต้อง log ทันทีว่าใครลบ (แถวแรกของตารางเปล่าคือ "X ลบ log เมื่อ...")
7. **อย่าลืมลบ seed ปลอมใน `main.go:61-62`** — แต่ระวัง: หน้า Dashboard
   Recent Logs (`Dashboard.tsx:602` + `dashboardService.ts:228`) แสดงข้อมูล
   จาก buffer นี้ ถ้าลบ seed แล้ว buffer ว่างเปล่าในโหมด mock ให้ย้าย seed
   ไปอยู่ใต้เงื่อนไข `if *mockOS` แทนการ seed เสมอ
8. **เวลาเก็บเป็น UTC (RFC3339) เสมอ แปลง timezone ที่ frontend** —
   `timeService.InitApplyConfig()` ถูก apply ก่อนอย่างอื่นใน startup อยู่แล้ว
   (`main.go:156-159` — comment ระบุเหตุผลเรื่อง log timestamp ไว้เอง)
   อย่าเก็บ local time string แบบ `logPowerEvent` เดิม (`15:04:05` ไม่มีวันที่!)
9. **Mock mode ปลอดภัยโดยธรรมชาติ** — EventLogService แตะแค่ SQLite ไฟล์
   local ไม่แตะ OS จึงไม่มี branch real/mock ใน kernel layer แต่ frontend
   mock mode (`services/config.ts`) ต้องมี mock data ให้หน้าใหม่ไม่พัง
10. **Goroutine lifecycle** — batch writer ต้องรับ `context.Context` และ
    flush ครั้งสุดท้ายตอน shutdown ปกติ (graceful) — ดู pattern การถือ ctx ของ
    `StartLeaseWatcher` (`dhcp_server.go:107`)
11. **การทดสอบ:**
    - dev: รัน `-mock=true` → login ผิด/ถูก, สร้าง user, แก้ policy, กด clear
      → เช็คแถวใน `pigate.db` (`sqlite3 pigate.db 'select * from system_events'`)
      และดูหน้า Event Logs; restart binary → log ต้องยังอยู่ (persist จริง)
    - role: login ด้วย admin ธรรมดา → GET ได้, POST clear ต้อง 403
    - `go build ./...` + `go test ./...` (backend), `yarn build` + `yarn lint`
      (frontend) ต้องผ่าน; เพิ่ม unit test ของ batch writer (flush ตามจำนวน/
      เวลา + prune) ใน `service/event_log_test.go`
    - บอร์ดจริง: ทดสอบ reboot แล้วดูว่า event `system.reboot` (ก่อนดับ) +
      `system.boot` (หลังบูต) อยู่ครบคู่ — ทดสอบเฉพาะตอนเข้าถึงบอร์ดได้จริง

---

## 6. Checklist สรุป (Definition of Done)

- [ ] `model/types.go` — เพิ่ม `SystemEvent` + const category/severity
- [ ] `db/connection.go` — ตาราง `system_events` + index
- [ ] `db/repository.go` — Insert (batch) / Get (filter+paging) / Count / Prune / Clear
- [ ] `service/event_log.go` — EventLogService: RAM buffer + batch writer + Flush + retention (ไฟล์ใหม่)
- [ ] `service/event_log_test.go` — test batch/flush/prune (ไฟล์ใหม่)
- [ ] `cmd/pigate/main.go` — สร้าง+Start service, ส่งเข้า NewServer + DhcpServerService, log `system.boot`, ย้าย seed ปลอมไปใต้ `if *mockOS`
- [ ] `api/handlers.go` — helper `logEvent` + hook 13 กลุ่มตามตาราง §3 Step 6 + แทนที่ `logPowerEvent` (Flush ก่อนสั่ง power)
- [ ] `service/dhcp_server.go` — log lease add/remove (ไม่ log renew)
- [ ] `api/router.go` — `GET /api/logs/events` (authRoute), `POST /api/logs/events/clear` (superAdminRoute)
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — sync spec ใหม่
- [ ] `frontend/src/services/logService.ts` — API client + mock branch (ไฟล์ใหม่)
- [ ] `frontend/src/pages/EventLogs.tsx` — ตาราง+filter+paging+clear (ไฟล์ใหม่, shadcn only, semantic colors, dark/light)
- [ ] `frontend/src/App.tsx` + `app-sidebar.tsx` — route + เมนู System › Event Logs
- [ ] `README.md` Feature Status — เพิ่มแถว Event Log
- [ ] ทดสอบ: mock end-to-end, persist ข้าม restart, role 403, build/test/lint ผ่านทั้งสองฝั่ง, ทดสอบ reboot คู่ boot event บนบอร์ดจริง
