# dhcpcd Link-Local (169.254.x.x) Fallback Detection (issue #78)

> แผนงานเพิ่มฟีเจอร์: periodic health-checker ตรวจ interface โหมด DHCP ที่ carrier พร้อม
> (`isUp && isRunning`) แล้วค้างอยู่กับ APIPA `169.254.x.x/16` (หรือไม่มี IPv4 เลย) — ลบ
> เฉพาะ address 169.254 ถ้ามี IP อื่นอยู่ร่วม หรือ restart dhcpcd ของ interface นั้นถ้ามีแค่
> 169.254/ไม่มี IP เลย เกณฑ์ทั้งหมด (ช่วงเวลาเช็ค/จำนวนรอบ/backoff/เพดาน) ปรับได้จริงผ่าน
> DB settings + API ใหม่
>
> เขียนเมื่อ: 2026-07-21 · Reference branch: `fix/usb-wifi-startup-race`
> อ้างอิง: GitHub issue #78, หมายเหตุเบื้องต้น `docs/ref/todo/dhcpcd-link-local-fallback-notes.md`,
> ต่อยอดจากงาน issue #76 (PR #79 ยังไม่ merge ณ วันที่เขียน — ดู Caution 8)
>
> **อัปเดต 2026-07-21 (หลังเขียนแผนนี้):** ระหว่างทดสอบ real-hardware ของ PR #79 พบหลักฐาน
> log จริงเคส "ค้างที่ 169.254" ครั้งแรก (ก่อนหน้านี้มีแต่เคส "ไม่มี IP เลย" จาก AP outage) —
> วิเคราะห์ละเอียดใน `dhcpcd-link-local-fallback-notes.md` §7 เพิ่ม Caution 11/12 ด้านล่าง
> จากผลนั้น ไม่กระทบ Task/สถาปัตยกรรมที่วางไว้ใน §2-§3 (ยังไม่มีอะไร implement จริง)
>
> **อัปเดต 2026-07-21 (รอบ 2 — ตรวจทานแผนกับโค้ดจริงบน `main`):** PR #79 ถูก merge เข้า
> `main` แล้ว (`4e2168d`, รวมคอมมิตแก้เพิ่ม `c9894af` ที่แตะ `netlink_monitor.go`) — Caution 8
> เกิดขึ้นจริงตามคาด งานนี้จึงต้อง**แตก branch ใหม่จาก `main`** (เช่น `feat/dhcp-health-checker`)
> ไม่ใช่ทำต่อบน `fix/usb-wifi-startup-race` อีกแล้ว ไล่ตรวจจุดอ้างอิงทุกจุดในแผนกับ `main`
> ปัจจุบันแล้ว: **ตรงเป๊ะทุกไฟล์** ยกเว้น `netlink_monitor.go` ที่เลขบรรทัดขยับ (แก้ใน §1 /
> T-07 / Caution 8 แล้ว) และเพิ่มข้อตัดสินใจ state-machine 3 ข้อเข้า T-09/T-10 (strike reset
> หลังลงมือ, การจำกัดกิ่ง delete, ความหมาย `runningSince` ใน tick แรก)

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย:** interface โหมด DHCP ที่ carrier พร้อม (`isUp && isRunning`) ค้างสถานะ
  ผิดปกติ (มีแค่ 169.254.x.x หรือไม่มี IPv4 เลย) ติดต่อกันครบ N รอบตามเกณฑ์ที่ผู้ใช้
  ตั้งไว้ ต้องถูกแก้ไขเองโดยไม่ต้องรอผู้ใช้: ลบเฉพาะ address 169.254 (ถ้ามี IP อื่นอยู่
  ร่วม) หรือ restart dhcpcd ของ interface นั้น (ถ้ามีแค่ 169.254/ไม่มี IP เลย) ทุกครั้งที่
  ลงมือต้องลง event log ให้เห็น และต้องมี backoff + เพดานจำนวนครั้งไม่ให้วน restart
  ตลอดไปถ้าแก้ไม่หาย
- **เงื่อนไขทางเทคนิค:** เกณฑ์ทุกตัว (interval, จำนวนรอบ, min-running guard, backoff,
  เพดาน restart, on/off) ต้องเป็นค่าที่ผู้ใช้ปรับได้จริงผ่าน DB settings + API (ไม่ใช่
  const ในโค้ด); interface ที่ `!(isUp && isRunning)` ต้องถูก skip สมบูรณ์ทุก tick
  (ไม่นับ strike, reset counter ทันที) ตามคำตัดสินใจเจ้าของโปรเจกต์
- **Out of scope (รอบนี้):**
  - **Frontend UI** สำหรับตั้งค่า — ทำ backend + REST API ให้ครบก่อน (ปรับค่าได้ผ่าน
    `curl`/API client ทันทีด้วยค่า default ที่ปลอดภัย) ส่วน UI หน้า Settings เป็น phase
    ถัดไป (ต้องได้รับการยืนยันจากเจ้าของโปรเจกต์ก่อนเริ่ม — ดูสรุปท้ายรายงาน)
  - ไม่แตะ dhcpcd-event-debounce / USB-Wi-Fi-startup-race logic ที่มีอยู่ (ใช้ร่วม ไม่ทับ)
  - ไม่ทำ IPv6 (169.254 เป็น IPv4 APIPA เท่านั้น; ไม่ตรวจ fe80::/10)
  - ไม่เปลี่ยน parent/child ของ VLAN หรือ Wi-Fi failover logic ใด ๆ

## 1. สภาพโค้ดปัจจุบัน (ตรวจทานล่าสุดบน `main` หลัง merge PR #79 ณ 2026-07-21)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `NetlinkMonitor.Start` | มี `missedAtStartup []string` แล้ว (จาก #76, **merge เข้า `main` แล้ว** ผ่าน PR #79) — publish synthetic `InterfaceAdded` หลัง seed | `service/netlink_monitor.go:78,126,255-296` |
| `InterfaceService.StartupSkippedInterfaces` | มีจาก #76 — ไม่กระทบ checker นี้โดยตรง (เราไม่ฟัง `InterfaceAdded`) | `service/interface.go:38-45,105-116` |
| `NetEvent`/`NetEventBus` | ไม่มี field address; ไม่มี accessor เช็คสถานะ pause | `service/event_bus.go:51-56,85-94` (ต้องเพิ่ม `IsPaused()`) |
| Periodic ticker ต้นแบบ | มี `runCPUSampler`/`runTrafficCollector` เป็นแม่แบบ `time.NewTicker`+`select ctx.Done()` | `service/system_status.go:175-198` |
| Restart dhcpcd ราย interface | ครบ real+mock ใช้ซ้ำได้ทันที (ไม่ต้องแก้ kernel layer) | `kernel/interfaces.go:119-121`, `kernel/dhcpcd.go:71-77`, `kernel/mock.go:390-393` |
| Concurrency ต้นแบบ (mutex+pendingStops) | `DhcpcdService.mu` คุม start/stop ทุกเส้นทาง — ต้องแชร์ critical section เดียวกัน | `service/dhcpcd.go:30-49,80-171` |
| อ่าน address ปัจจุบันของ interface | **ยืนยันแล้ว: ไม่มี method บน `NetworkManager`** — `netlink.AddrList` ใช้เฉพาะภายใน `ConfigureInterface` และมันลบ IPv4 ทุกตัวทิ้งก่อนเสมอ (ใช้แทน "restart" ไม่ได้) | `kernel/interfaces.go:34-51`, `kernel/real_network.go:198-224` |
| ลบ address เดี่ยวจาก interface | **ยืนยันแล้ว: ไม่มี** — ต้องเพิ่มใหม่ | `kernel/interfaces.go:34-51` |
| Mock kernel network | `MockNetwork` มีครบ 6 เมธอดของ `NetworkManager` วันนี้ — ต้องเพิ่ม 2 เมธอดใหม่ | `kernel/mock.go:74-131` |
| Pattern DB settings แบบ single-row + UI (candidate b) | `system_time_settings`/`system_hostname_settings` — table + repo Get/Update + handler GET/PUT + `authRoute` | `db/connection.go:369-380,844-871`, `db/repository.go:1997-2017`, `api/handlers.go:2003-2031`, `api/router.go:141-145` |
| `internal/config` (candidate a) | ยืนยันจาก docstring ของไฟล์เอง: เป็น **bootstrap flag ล้วน** (`mock`,`db`,`port`,...) ไม่ใช่ subsystem config, ไม่มี UI, ไม่ติด backup | `config/config.go:1-8,37-50` |
| Backup schema v2 pattern เพิ่ม field แบบไม่ bump version | มีแบบอย่างแล้ว (`PortForwards` เป็น `omitempty`) | `model/backup.go:69-75` |
| จุด restore single-row settings ใน backup | มี block `--- 9. Single-row system settings ---` ให้ต่อแถวใหม่ | `db/backup_repo.go:235-270` |
| Startup wiring (main.go) | ลำดับ 6.0-6.5 คงเดิมจาก #76 — checker ใหม่เป็น background loop ไม่ใช่ startup-apply จึงไม่ต้องแทรกใน 6.x | `cmd/pigate/main.go:159-172,392-398` |
| Evidence จริงของอาการ "ค้างที่ 169.254" (ไม่ใช่แค่ "ไม่มี IP เลย") | ยืนยันแล้วบนบอร์ดจริง 2026-07-21 ระหว่างทดสอบ PR #79 — SSID เฉพาะตัวค้างซ้ำสองรอบ restart ติดกัน, cadence re-assert ~11-14s, หลุดได้ด้วย manual toggle ไปตกที่ backup SSID เท่านั้น | `dhcpcd-link-local-fallback-notes.md` §7 |

**สรุป:** ไม่มีส่วนไหนถูกทำไปแล้ว งานใหม่ทั้งหมด กระจุกอยู่ที่ kernel layer (2 เมธอดใหม่ +
mock), DB settings table ใหม่ 1 ตาราง, service ใหม่ 1 ไฟล์ (ตัว checker) + แก้ `dhcpcd.go`/
`event_bus.go` เล็กน้อยเพื่อ concurrency/pause-awareness, API GET/PUT ใหม่, backup wiring,
และ openapi ทั้งสองไฟล์ — **candidate (b) ยืนยันแล้วจากการสำรวจโค้ดจริง** (ดู Section 2)

## 2. แนวทางเทคนิค

### 2.1 กลไก config: เลือก **(b) DB table + API** (ไม่ใช่ (a) `pigate.conf`)

ยืนยันจากการอ่าน `config/config.go:1-8` ตรง ๆ: package `internal/config` ประกาศตัวเองชัดเจน
ว่า "deliberately NOT the SQLite-backed subsystem configuration" — ครอบคลุมเฉพาะ bootstrap
parameter ระดับ process (mock/db path/port ฯลฯ) ที่อ่านครั้งเดียวตอน `main()` เริ่มทำงาน
เกณฑ์ health-checker เป็น **runtime subsystem config** ระดับเดียวกับ `system_time_settings`
โดยธรรมชาติ (ผู้ใช้ต้องปรับได้ระหว่างระบบรันโดยไม่ restart binary, ควรติดไปกับ backup/restore
เหมือนการตั้งค่าอื่น) — ตรง pattern (b) ตรง ๆ ไม่ต้องฝืน schema ของ (a) ที่ไม่ได้ออกแบบมาเพื่อ
เรื่องนี้ Backend + API ทำให้ครบก่อน (ปรับค่าผ่าน API ได้ทันทีด้วย default ที่ปลอดภัย โดยไม่
ต้องรอ UI)

### 2.2 กลไกตรวจจับและแก้ไข: pure decision function + periodic ticker (ไม่ใช่ event-driven)

```go
// service/dhcp_health_checker.go (ใหม่)
type ifaceHealthState struct {
    strikes               int
    runningSince          time.Time // เวลาที่ isRunning เปลี่ยนเป็น true ครั้งล่าสุด
    restartsSinceRecover  int
    lastRestartAt         time.Time
    ceilingLogged         bool
}

// tick ต่อ interface: eligibility gate ต้องมาก่อนเสมอ
if !(isUp && isRunning) {
    resetState(name) // skip สมบูรณ์ ไม่นับ strike (คำตัดสินใจ §2 ของ notes)
    continue
}
if time.Since(state.runningSince) < settings.MinRunningSeconds { continue } // guard reconnect
hasReal, hasLinkLocal := classifyAddrs(addrs) // net.IP.IsLinkLocalUnicast()
if hasReal && !hasLinkLocal { resetState(name); continue } // ปกติ
state.strikes++
if state.strikes < settings.ConsecutiveStrikes { continue }
switch {
case hasReal && hasLinkLocal:
    network.DeleteAddress(name, linkLocalCIDR) // ลบเฉพาะ 169.254
case time.Since(state.lastRestartAt) < backoff: // ยังไม่ครบ backoff
case state.restartsSinceRecover >= settings.MaxRestartsBeforePause:
    logCeilingOnce(name)
default:
    dhcpcdService.RestartForHealthCheck(name) // ผ่าน mutex เดียวกับ dhcpcd.go
}
```

- **ทำไม periodic ticker เป็นแกนหลัก:** ยืนยันจากผลทดสอบจริงใน notes `§6` — เคส "ไม่มี IPv4
  เลย" ไม่มี Address event ให้เกาะเลยตลอด 5-6 นาที เพราะ interface ไม่เคยได้ IP ตั้งแต่ต้น
  event-driven ล้วนตรวจเคสนี้ไม่ได้จริง ต้อง poll เป็นแกนหลัก (ต้นแบบ ticker: `runCPUSampler`)
- **ทำไม eligibility gate อ่าน live flags ทุก tick (ไม่ cache):** ผลทดสอบ `§6.3` ชี้ว่า
  cadence ~65s ของ Wi-Fi retry อาจ alias กับ tick ~60s — ต้องอ่านสดทุกครั้งเพื่อจับจังหวะ
  down-blip ให้ถูก ไม่งั้นช่วง reconnect ทั้งหมดจะถูกนับ strike ผิด
- **ทำไมแยก "ลบ address เดี่ยว" ออกจาก `ConfigureInterface` เดิม:** ยืนยันจากโค้ดจริงว่า
  `ConfigureInterface` โหมด dhcp ลบ IPv4 **ทุกตัว** ก่อนคืนค่า (`real_network.go:219-227`) —
  ใช้แทนไม่ได้เมื่อต้องการเก็บ IP อื่นที่ใช้งานอยู่ไว้ จึงต้องมี `DeleteAddress` แบบเจาะจง
- **ทำไม restart ต้องผ่าน `DhcpcdService` ไม่เรียก `kernel.DhcpcdManager` ตรง:** ต้อง
  แชร์ critical section เดียวกับ `mu`/`pendingStops` (Caution 3 ของแผน #75) ไม่งั้น restart
  จาก checker อาจ race กับ stop/start จาก event path
- **ทางเลือกที่ปฏิเสธ:**
  1. *ใช้ `ConfigureInterface` เพื่อลบ 169.254* — ปฏิเสธ: ลบ IP อื่นที่ใช้งานได้ไปด้วย
     ผิดหลักการ "ลบเฉพาะ 169.254" ของ decision เดิม
  2. *event-driven ล้วน (subscribe `AddrRouteChanged`)* — ปฏิเสธ: ตรวจเคส "ไม่มี IP เลย"
     ไม่ได้เลยตามผลทดสอบจริง `§6`; ใช้เป็นตัวเสริมได้แต่ไม่ใช่แกนหลัก (ไม่ทำรอบนี้เพื่อกันความ
     ซับซ้อนเกิน — ticker เพียงพอสำหรับ SLA "ตรวจทุก ~1 นาที")
  3. *เพิ่ม field address ใน `NetEvent` แล้วให้ `NetlinkMonitor` ตรวจ 169.254 เอง* — ปฏิเสธ:
     ผูก business logic (state machine/strike counting) เข้ากับ translator layer ที่ตั้งใจ
     ให้เป็น thin translator เท่านั้น (`netlink_monitor.go:15-20` คำอธิบายเดิม)
  4. *เก็บ candidate (a) `pigate.conf`* — ปฏิเสธตามเหตุผล 2.1
- **แม่แบบโค้ดที่ยึด:** ticker/`select ctx.Done()` ของ `system_status.go:175-198`, mutex
  wrapper pattern ของ `dhcpcd.go` (Caution 3), CRUD settings pattern ของ `hostname.go`/
  `repository.go:1997-2017`, backup single-row restore ของ `backup_repo.go:235-270`

## 3. ขั้นตอน (เรียง inner-layer-first — ทำครบทุก Task ก่อน แล้วค่อยทดสอบรวมตาม §6)

### T-01 — เพิ่ม kernel capability: อ่าน/ลบ address เดี่ยว
**File:** `backend/internal/kernel/interfaces.go` (แก้ไข, ต่อท้าย `DeleteVlan` ใน `NetworkManager` ~บรรทัด 50)
- เพิ่ม `GetIPv4Addresses(name string) ([]string, error)` (คืน CIDR เช่น `"169.254.1.2/16"`)
- เพิ่ม `DeleteAddress(name string, cidr string) error` (ลบ address ตัวเดียวตาม CIDR ที่ให้มา
  เท่านั้น ไม่แตะ address อื่นบน interface)
- **เสร็จเมื่อ:** คอมไพล์ผ่าน (ยังไม่มี implementation)

### T-02 — Implement จริงใน `real_network.go`
**File:** `backend/internal/kernel/real_network.go` (แก้ไข — วางต่อจาก `ConfigureInterface` ~บรรทัด 260)
- `GetIPv4Addresses`: `netlink.LinkByName` + `netlink.AddrList(link, netlink.FAMILY_V4)` → map
  เป็น `addr.IPNet.String()`
- `DeleteAddress`: `netlink.LinkByName` + `netlink.ParseAddr(cidr)` + `netlink.AddrDel` —
  ถ้า address หายไปแล้ว (ชนกับ dhcpcd ที่เพิ่งได้ lease ระหว่างนั้นพอดี) ต้อง log warning
  แล้วคืน `nil` ไม่ใช่ error (ไม่ถือเป็นความล้มเหลว — ผลลัพธ์ที่ต้องการเกิดขึ้นแล้ว)
- **เสร็จเมื่อ:** คอมไพล์ผ่านบน Linux build tag เดิมของไฟล์

### T-03 — Mock implementation
**File:** `backend/internal/kernel/mock.go` (แก้ไข `MockNetwork` ~บรรทัด 119 ต่อจาก `DeleteVlan`)
- `GetIPv4Addresses`: คืน slice ว่างเสมอ (ปลอดภัย 100% ไม่มี state จำลองก็เพียงพอ — ตัว checker
  เองมี guard mock mode แยกอีกชั้นใน T-08 อยู่แล้ว จุดนี้กันไว้สำหรับ caller อื่นในอนาคตด้วย)
- `DeleteAddress`: log-only no-op ตามสไตล์ `DeleteVlan`/`CreateVlan` เดิม
- **เสร็จเมื่อ:** `go build ./...` ผ่านทั้ง repo (interface ครบทั้ง real+mock)

### T-04 — ตาราง DB settings ใหม่ + seed default
**File:** `backend/internal/db/connection.go` (แก้ไข)
- เพิ่ม `CREATE TABLE IF NOT EXISTS dhcp_health_settings (...)` ต่อจาก `system_hostname_settings`
  (~บรรทัด 380): คอลัมน์ `id INTEGER PRIMARY KEY CHECK(id=1)`, `enabled INTEGER DEFAULT 1
  CHECK(enabled IN (0,1))`, `check_interval_seconds INTEGER NOT NULL DEFAULT 60`,
  `consecutive_strikes INTEGER NOT NULL DEFAULT 3`, `min_running_seconds INTEGER NOT NULL
  DEFAULT 30`, `restart_backoff_seconds INTEGER NOT NULL DEFAULT 300`,
  `max_restarts_before_pause INTEGER NOT NULL DEFAULT 3`
- เพิ่ม seed block ต่อจาก "5.1 Seed Default System Hostname Settings" (~บรรทัด 871) ตามแบบ
  count-then-insert เดิม ใส่ค่า default ตรงกับ column default ข้างต้น
- **เสร็จเมื่อ:** `go test ./internal/db/...` ผ่าน (migration ใหม่ไม่พังของเดิม)

### T-05 — Model struct
**File:** `backend/internal/model/types.go` (แก้ไข — ต่อจาก `SystemHostnameSettings` ~บรรทัด 322)
- เพิ่ม `type DhcpHealthSettings struct` ฟิลด์ตรงกับคอลัมน์ (camelCase json tag:
  `enabled`,`checkIntervalSeconds`,`consecutiveStrikes`,`minRunningSeconds`,
  `restartBackoffSeconds`,`maxRestartsBeforePause`) พร้อม doc comment อธิบายที่มา (issue #78)
- **เสร็จเมื่อ:** คอมไพล์ผ่าน

### T-06 — Repository CRUD
**File:** `backend/internal/db/repository.go` (แก้ไข — ต่อจาก `UpdateHostnameSettings` ~บรรทัด 2017)
- `GetDhcpHealthSettings() (*model.DhcpHealthSettings, error)` / `UpdateDhcpHealthSettings(s
  model.DhcpHealthSettings) error` ตามแบบ hostname settings เป๊ะ (bool↔int mapping)
- **เสร็จเมื่อ:** unit test อ่าน/เขียนค่าได้ตรงที่ seed ไว้ใน T-04

### T-07 — Pause-awareness บน event bus
**File:** `backend/internal/service/event_bus.go` (แก้ไข ต่อจาก `Resume()` ~บรรทัด 193)
- เพิ่ม `func (b *NetEventBus) IsPaused() bool { return b.paused.Load() }`

**File:** `backend/internal/service/netlink_monitor.go` (แก้ไข ต่อจาก `Resume()` ~บรรทัด 303 —
เลขบรรทัดบน `main` หลัง merge PR #79; `Pause` อยู่ ~298)
- เพิ่ม `func (m *NetlinkMonitor) IsPaused() bool { return m.bus.IsPaused() }` (delegate
  ตาม pattern เดิมของ `Pause`/`Resume`)
- **เสร็จเมื่อ:** คอมไพล์ผ่าน — เมธอดนี้ให้ checker เช็คก่อนลงมือทุก tick (T-08)

### T-08 — Restart wrapper ที่แชร์ mutex เดิม
**File:** `backend/internal/service/dhcpcd.go` (แก้ไข — ต่อท้ายไฟล์ หลัง `SyncInterface` ~บรรทัด 344)
- เพิ่ม `func (s *DhcpcdService) RestartForHealthCheck(name string) error`: `s.mu.Lock()` →
  `s.cancelPendingStopLocked(name)` → `s.manager.RestartDhcpcd(name)` → unlock (คอมเมนต์อ้าง
  Caution 3 ของแผน dhcpcd-event-debounce ตรง ๆ เหมือน `syncActiveInterface`)
- **เสร็จเมื่อ:** คอมไพล์ผ่าน; `go test -race ./internal/service/...` เขียว

### T-09 — ตัว checker ใหม่ (หัวใจของฟีเจอร์)
**File:** `backend/internal/service/dhcp_health_checker.go` (ใหม่)
- `DhcpHealthChecker` struct: `repo`, `ifaceService *InterfaceService`, `dhcpcdService
  *DhcpcdService`, `network kernel.NetworkManager`, `eventLog *EventLogService`, `bus
  *NetEventBus`, `mu sync.Mutex` + `states map[string]*ifaceHealthState` (RAM only —
  ไม่ persist ตาม SD-card preservation)
- `NewDhcpHealthChecker(...)` constructor
- `Start(ctx context.Context)`: ticker ตามแบบ `system_status.go:175-186` — อ่าน
  `settings.CheckIntervalSeconds` **ทุก tick** (ไม่ cache ตอน Start เพื่อให้ปรับ interval
  runtime มีผลจริงโดยไม่ต้อง restart service — ใช้ ticker คงที่จาก default แล้ว reset ตัว
  ticker เองเมื่อค่าที่อ่านได้เปลี่ยนจากรอบก่อน)
- `tick()`: guard `IsMockMode()` (ไม่แตะ `netlink.LinkByName` จริงในโหมด mock — ตามสไตล์
  `dhcpcd.go` `syncActiveInterface`) → guard `!settings.Enabled` → guard `bus.IsPaused()`
  (ข้าม tick ทั้งชุดระหว่าง backup import) → loop ทุก interface โหมด dhcp จาก
  `ifaceService.GetDataLayerInterface()` → อ่าน live flags ด้วย `netlink.LinkByName` ตรง
  (ตามสไตล์ `dhcpcd.go`, ไม่ผ่าน kernel interface — ดู Caution 2) → ตรรกะตาม §2.2
- ฟังก์ชัน pure แยกออกมาให้เทสต์ได้โดยไม่แตะ kernel: `classifyAddrs([]string) (hasReal,
  hasLinkLocal bool)` ใช้ `net.ParseCIDR` + `ip.IsLinkLocalUnicast()`, และ
  `decideNextState(state *ifaceHealthState, isUp, isRunning, hasReal, hasLinkLocal bool,
  settings model.DhcpHealthSettings, now time.Time) healthAction` (enum: none/deleteAddr/
  restart/restartSkippedBackoff/restartCeilingReached)
- **ข้อตัดสินใจ state-machine (จากการตรวจทานแผน 2026-07-21 รอบ 2 — บังคับใช้ใน
  `decideNextState` และต้องมีเทสต์ครอบใน T-10):**
  1. *Strike reset หลังลงมือ:* ทุกครั้งที่ตัดสินใจ action จริง (deleteAddr **หรือ** restart)
     ให้ reset `strikes = 0` ทันที — การลงมือครั้งถัดไปต้องสะสม strike ใหม่ครบ
     `ConsecutiveStrikes` อีกรอบเสมอ (บวก backoff สำหรับกิ่ง restart) ส่วน
     `restartsSinceRecover`/`ceilingLogged` **ไม่** reset ตรงนี้ — reset เฉพาะเมื่อกลับ
     healthy จริงตาม Caution 5
  2. *กิ่ง deleteAddr ไม่มี backoff/เพดานของตัวเอง — ใช้ strike reset จากข้อ 1 เป็นตัวจำกัด
     ความถี่:* เคสแย่สุด (มีอะไร re-add 169.254 ซ้ำทั้งที่มี IP จริงอยู่ — notes §7 วัด
     cadence re-assert ได้ ~11-14s) จะลบซ้ำได้ไม่ถี่กว่า `ConsecutiveStrikes ×
     CheckIntervalSeconds` (~180s ที่ค่า default) ไม่ใช่ทุก tick — ยอมรับได้โดยไม่ต้องเพิ่ม
     counter แยก เพราะกิ่งนี้เกิดได้เฉพาะตอนมี IP จริงใช้งานอยู่แล้ว (ไม่ใช่ outage)
  3. *`runningSince` ใน tick แรก:* interface ที่ตรวจพบว่า `isUp && isRunning` อยู่แล้วตั้งแต่
     tick แรกของ checker (ยังไม่มี state เดิม) ให้ตั้ง `runningSince = now` — ผลคือเลื่อน
     การนับ strike ออกไปอีกหนึ่งช่วง `MinRunningSeconds` หลัง checker เริ่ม/pigate restart
     ซึ่งยอมรับได้ (สอดคล้อง known limitation ใน Caution 12 อยู่แล้ว)
- ทุก action ที่ลงมือจริงต้องเรียก `eventLog.Log(model.EventCategoryDhcp, "dhcp.linklocal_*",
  ...)` (severity warning สำหรับ detect/restart, info สำหรับลบ address สำเร็จ, error สำหรับ
  ชนเพดาน — log เพดานแค่ครั้งเดียวต่อ episode ด้วย flag `ceilingLogged`) — ข้อความตอนชนเพดาน
  ต้อง**actionable** ไม่ใช่ generic เฉย ๆ (ดู Caution 11: evidence จริงชี้ว่า restart dhcpcd
  ซ้ำอาจไม่ช่วยถ้าปัญหาอยู่ฝั่ง AP/SSID) เช่น ระบุชื่อ SSID ปัจจุบันที่ associate อยู่ และแนะนำ
  ให้ผู้ใช้ตรวจสอบ AP/DHCP server ของ SSID นั้น ไม่ใช่แค่ "restart ล้มเหลว N ครั้ง"
- `GetSettings()`/`UpdateSettings(model.DhcpHealthSettings) error`: thin wrapper เรียก
  repository ตรง ๆ พร้อม validate ranges (interval 10-3600s, strikes 1-20, minRunning
  0-600s, backoff 0-3600s, maxRestarts 1-20) — ให้ handler (T-12) เรียกใช้ ไม่ validate
  ซ้ำในชั้น API
- **เสร็จเมื่อ:** คอมไพล์ผ่าน; ยังไม่ต่อสายเข้า `main.go` (ทำใน T-11)

### T-10 — เทสต์ตัว checker
**File:** `backend/internal/service/dhcp_health_checker_test.go` (ใหม่)
- Unit เคส `classifyAddrs`: มีทั้ง real+link-local, มีแค่ link-local, ไม่มี IP เลย, มีแค่ real
- Unit เคส `decideNextState`: strike ไม่ครบยังไม่ลงมือ, ครบแล้วแยกกิ่ง delete/restart ถูกต้อง,
  min-running guard กันไม่ให้นับก่อนเวลา, backoff กันไม่ให้ restart ถี่, เพดานทำงานและ log
  ครั้งเดียว, healthy กลับมาแล้ว state/เพดาน reset
- Unit เคสข้อตัดสินใจ state-machine (T-09): action ใด ๆ reset `strikes` เป็น 0 แต่**ไม่**
  reset `restartsSinceRecover`/`ceilingLogged`; กิ่ง deleteAddr ที่ 169.254 ถูก re-add ซ้ำ
  ทุก tick ลงมือลบได้ไม่ถี่กว่า `ConsecutiveStrikes` tick ต่อครั้ง (ไม่ใช่ทุก tick);
  interface ที่ running อยู่ก่อนแล้วใน tick แรกต้องผ่าน min-running guard ก่อนเริ่มนับ strike
- Integration เบา ๆ: เรียก `tick()` ตรงด้วย `kernel.NewMockNetwork()`+`NewMockDhcpcdManager()`
  ยืนยันไม่มี panic และ mock mode ไม่เรียก `netlink.LinkByName` จริง (ผ่านการไม่ crash บน CI
  ที่ไม่มีสิทธิ์ netlink)
- **เสร็จเมื่อ:** `go test ./internal/service/... -run TestDhcpHealth -race` ผ่าน

### T-11 — ต่อสายใน `main.go`
**File:** `backend/cmd/pigate/main.go` (แก้ไข)
- สร้าง `dhcpHealthChecker := service.NewDhcpHealthChecker(repo, ifaceService, dhcpcdService,
  net, eventLogService, eventBus)` ต่อจากการสร้าง service อื่น ๆ (~บรรทัด 172, หลัง
  `systemStatusService`)
- เรียก `dhcpHealthChecker.Start(monitorCtx)` **หลัง** `netlinkMonitor.Start(...)` (~บรรทัด 398)
  เพราะเป็น background self-heal loop เช่นเดียวกับตัว monitor เอง ไม่ใช่ startup-apply
  จึงไม่ต้องแทรกในลำดับ 6.0-6.5 ที่มีอยู่
- **เสร็จเมื่อ:** `go build ./...` ทั้ง repo ผ่าน

> **สิ่งที่ไม่ต้องทำ:** ไม่แทรก checker เข้าลำดับ startup-apply (6.0-6.5) เพราะไม่มี kernel
> state ต้อง apply ตอน boot (มันอ่านค่าจาก DB เองทุก tick อยู่แล้ว); ไม่ subscribe
> `NetEventBus` เป็นตัวกระตุ้นหลัก (เหตุผล §2.2 ทางเลือกที่ปฏิเสธ ข้อ 2)

### T-12 — API: handler + route
**File:** `backend/internal/api/handlers.go` (แก้ไข)
- เพิ่ม field `dhcpHealthChecker *service.DhcpHealthChecker` ใน `Server` struct (~บรรทัด 48)
  และพารามิเตอร์ใน `NewServer` (~บรรทัด 74/99)
- `HandleGetDhcpHealthSettings`/`HandleUpdateDhcpHealthSettings` ต่อจาก `HandleUpdateHostname`
  (~บรรทัด 2031) ตามแบบเป๊ะ: GET คืน settings ตรง ๆ, PUT decode → validate (ใน service) →
  `UpdateSettings` → `s.logEvent(r, model.EventCategoryDhcp, "dhcp.health_settings_changed",
  model.EventSeverityInfo, "system", "...")** → คืน settings ที่บันทึกแล้ว

**File:** `backend/internal/api/router.go` (แก้ไข ต่อจาก `HandleUpdateHostname` ~บรรทัด 145)
- `authRoute("GET /api/system/dhcp-health", s.HandleGetDhcpHealthSettings)`
- `authRoute("PUT /api/system/dhcp-health", s.HandleUpdateDhcpHealthSettings)`
- **เสร็จเมื่อ:** คอมไพล์ผ่าน; endpoint ตอบ 200/400 ตามเคสถูกต้อง (unit test handler)

### T-13 — Backup/Restore
**File:** `backend/internal/model/backup.go` (แก้ไข ~บรรทัด 68-75)
- เพิ่ม `DhcpHealthSettings *DhcpHealthSettings `json:"dhcpHealthSettings,omitempty"`` ใน
  `BackupConfig` — ใช้ pointer + `omitempty` แบบเดียวกับ `PortForwards` (คอมเมนต์เดิมอธิบาย
  เหตุผลตรง ๆ: กัน checksum พังกับ backup v2 เก่าที่ไม่มี field นี้ โดยไม่ต้อง bump
  `CurrentBackupSchemaVersion`)

**File:** `backend/internal/service/backup.go` (แก้ไข)
- `Export()`: อ่าน `s.repo.GetDhcpHealthSettings()` แล้วใส่ pointer เข้า `cfg.DhcpHealthSettings`
  ต่อจากการอ่าน hostname (~บรรทัด 144)

**File:** `backend/internal/db/backup_repo.go` (แก้ไข — ใน `RestoreConfig`, block "9. Single-row
system settings" ~บรรทัด 262-270)
- เพิ่ม `if cfg.DhcpHealthSettings != nil { UPDATE dhcp_health_settings SET ... WHERE id=1 }`
  ตาม pattern เดียวกับ hostname/time ในบล็อกนั้น
- **ไม่ต้อง**แก้ `BackupService.reapply()` (`service/backup.go:463-509`) — checker อ่าน
  settings จาก DB สดทุก tick อยู่แล้ว ไม่มี cached copy ต้อง re-push เข้า kernel ตอน restore
- **เสร็จเมื่อ:** `go test ./internal/service/... -run TestBackup` ผ่านทั้งหมด (ไม่มี regression
  บน checksum ของ backup v2 เก่า)

### T-14 — Docs/contract
**File:** `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (แก้ไขทั้งสองไฟล์ให้ตรงกัน)
- เพิ่ม path `/api/system/dhcp-health` (GET/PUT) และ schema `DhcpHealthSettings` ตามแบบ
  `/api/system/hostname` ที่มีอยู่แล้วในไฟล์เดิม
- **เสร็จเมื่อ:** ทั้งสองไฟล์ diff ตรงกันไม่มีจุดที่ไฟล์ใดไฟล์หนึ่งขาด path/schema นี้

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/system/dhcp-health` | `authRoute` (ทุก role อ่านได้) | คืน `DhcpHealthSettings` ปัจจุบันจาก DB |
| PUT | `/api/system/dhcp-health` | `authRoute` (POST/PUT ถูกบล็อกสำหรับ non-super_admin โดย `RoleReadOnlyMiddleware` อัตโนมัติ) | validate ranges แล้วบันทึก, log event `dhcp.health_settings_changed` |

ทั้งสอง route **ใหม่** (ไม่มีมาก่อน) `-disable-edit=true` บล็อก PUT โดยอัตโนมัติผ่าน
`DisableEditMiddleware` ที่ครอบทุก mutation route อยู่แล้ว (ไม่ต้องเขียน guard เพิ่ม)
**สำคัญ:** `DisableEditMiddleware` ครอบเฉพาะ HTTP mutation route — ไม่ครอบ background
self-heal loop ใด ๆ (ยืนยันจาก `middleware.go:333-334`) ดังนั้นตัว checker ยังทำงาน
(ลบ address/restart dhcpcd) ได้ปกติแม้รันด้วย `-disable-edit=true` เหมือนกับ self-heal
ตัวอื่น ๆ ในระบบ (interface reapply, dhcpcd HandleLinkEvent) — เป็นพฤติกรรมที่ตั้งใจ ไม่ใช่บั๊ก

## 5. ข้อควรระวัง (Cautions)

1. **ห้ามเรียก `kernel.DhcpcdManager.RestartDhcpcd` ตรงจาก checker** — ต้องผ่าน
   `DhcpcdService.RestartForHealthCheck` (T-08) เท่านั้น ไม่งั้น race กับ
   `HandleLinkEvent`/`SyncActiveInterfaces` ที่แชร์ `pendingStops`/`mu` เดียวกันได้ (Caution 3
   เดิมของแผน dhcpcd-event-debounce ใช้ได้ตรงกับที่นี่)
2. **การอ่าน live link flags ใน `tick()` ใช้ `netlink.LinkByName` ตรง ไม่ผ่าน kernel
   interface** — เจตนาเลียนแบบ precedent ที่มีอยู่แล้วใน `dhcpcd.go` (`syncActiveInterface`,
   `SyncInterface`) ซึ่ง guard ด้วย `s.repo.IsMockMode()` ก่อนเสมอ ส่วนการอ่าน/ลบ address
   (T-01/T-02) เป็น capability **ใหม่** จึงต้องผ่าน kernel interface ตามกติกา CLAUDE.md
   (real+mock ครบ) — เป็นความไม่สม่ำเสมอที่มีอยู่แล้วในโค้ดเดิม ไม่ใช่สิ่งที่แผนนี้สร้างขึ้นใหม่
3. **Eligibility gate ต้องเป็น uniform ทุกประเภท interface (ไม่ใช่แค่ Wi-Fi)** — ต่างจาก
   `dhcpcd.go`'s `applyDhcpcdDecisionLocked` ที่แยกกรณี "up but not running" เฉพาะ
   `isWifi` เท่านั้น (สาย LAN ถือว่า running ทันทีที่ up) ตัว checker **ห้ามลอกตรรกะนั้น** —
   ต้องเช็ค `isUp && isRunning` เดียวกันทุกประเภทตามคำตัดสินใจเจ้าของโปรเจกต์ใน notes §2
4. **`DeleteAddress` อาจเจอ address ที่หายไปแล้วระหว่างทาง** (เช่น dhcpcd เพิ่งได้ lease จริง
   ในช่วงเสี้ยววินาทีระหว่างอ่าน addr list กับสั่งลบ) — ต้อง treat เป็น success ไม่ใช่ error
   (T-02) ไม่งั้น event log จะเต็มไปด้วย false-error ที่ไม่มีความหมาย
5. **Backoff/เพดานต้อง reset เมื่อ interface กลับสู่สภาพปกติจริง** (`hasReal && !hasLinkLocal`)
   ไม่ใช่ reset ทุกครั้งที่หลุดจาก eligibility (down/up-not-running) — งั้นเพดานจะไม่มีความหมาย
   เพราะ flap ปกติของ Wi-Fi (ผลทดสอบ `§6`: full down→up cycle ทุก ~65s) จะ reset เพดานตลอด
6. **`bus.IsPaused()` ต้องเช็คทุก tick ก่อนลงมือ** (T-07) — ป้องกัน checker restart dhcpcd/ลบ
   address ระหว่าง backup import กำลังเขียน DB ใหม่ทับ (state ของ `states map` ในตัว checker
   เองไม่ถูก pause กระทบ — ยอมรับได้ เพราะ tick ถัดไปหลัง resume จะอ่าน DB สดใหม่อยู่ดี)
7. **ค่า default ต้องไม่ aggressive เกินไป** — `3 strikes × 60s interval + 30s min-running`
   ≈ 210 วินาทีก่อนลงมือครั้งแรก ยาวพอสำหรับ AP reconnect ปกติ (ผลทดสอบ `§6` เห็น cycle
   ~65s ต่อรอบ ไม่ใช่วินาทีเดียว) แต่เจ้าของโปรเจกต์ควรตรวจสอบตัวเลขเหล่านี้อีกครั้งก่อน merge
   เพราะเป็นตัวเลข "เหมาะสมโดยประมาณ" จากการวิเคราะห์ ไม่ใช่ค่าที่ทดสอบจริงบนบอร์ด — **อัปเดต
   2026-07-21:** เคสค้างที่ 169.254 จริงที่เจอ (`notes.md` §7) อยู่ในหน้าต่างสังเกตได้แค่ ~89
   วินาทีก่อนมีคนแก้ด้วยมือ ซึ่งสั้นกว่า threshold default (~210s) เล็กน้อย — ยังไม่มีหลักฐาน
   ว่า default ปัจจุบันทันจับเคสจริงพอดีหรือไม่ ต้องทดสอบยืนยันตอน implement จริง (ไม่ใช่แค่
   เชื่อเลขจากการวิเคราะห์) ส่วน cadence การ re-assert address เดิมที่วัดได้จริงระหว่างค้าง
   (~11-14s) ยืนยันว่าไม่ชนปัญหา eligibility-gate aliasing แบบ `§6.3` (ที่อยู่ไม่ toggle
   up/down ระหว่างค้าง จึงไม่มีความเสี่ยง false-skip จาก tick ที่ดันไปตกช่วง down-blip)
8. **[เกิดขึ้นจริงแล้ว — ตรวจทานเสร็จ 2026-07-21 รอบ 2]** PR #79 (issue #76) ถูก merge เข้า
   `main` แล้ว (`4e2168d` รวม `c9894af` ที่แก้ `netlink_monitor.go` เพิ่มจากที่สำรวจไว้เดิม)
   — ไล่ทวนไฟล์จริงบน `main` แล้ว: `dhcpcd.go`/`interface.go`/kernel/db/api/backup **เลขบรรทัด
   ตรงตาม §1 ทุกจุด** มีเฉพาะ `netlink_monitor.go` ที่ขยับ (แก้ใน §1 และ T-07 แล้ว: `Start`
   ~78, `publishMissedStartupLinks` ~255, `Pause`/`Resume` ~298/303) งานนี้ให้แตก branch
   ใหม่จาก `main` (`feat/dhcp-health-checker`) — branch `fix/usb-wifi-startup-race` จบไปกับ
   PR #79 แล้ว ไม่ใช้ต่อ
9. **งานนี้แตะย่าน self-heal/netlink/dhcpcd → sensitive** ตามนโยบายโปรเจกต์ — PR ต้องผ่าน
   review เข้ม โดยเฉพาะเงื่อนไข eligibility gate (ข้อ 3), lock ordering (ข้อ 1), และ
   default thresholds (ข้อ 7)
10. **ทดสอบบนบอร์ดจริงต้องมี physical access** — การ restart dhcpcd/ลบ address ผิดเงื่อนไข
    อาจตัดการเชื่อมต่อ interface ที่ใช้เข้าถึงตัวเครื่องเอง (โดยเฉพาะถ้า interface ที่ทดสอบคือ
    ตัวที่ใช้ SSH/UI อยู่) ให้ทดสอบผ่าน LAN interface สำรอง หรือมีจอ-คีย์บอร์ดต่อตรงก่อนเสมอ
11. **"restart dhcpcd" อาจไม่พอสำหรับเคสที่ปัญหาผูกกับ SSID เฉพาะตัว (evidence ใหม่
    2026-07-21, `notes.md` §7.2):** log จริงเจอกรณี full Wi-Fi reconnect (ผ่าน pigate
    service restart เต็มรูปแบบ ไม่ใช่แค่ restart dhcpcd) ยัง associate SSID เดิมที่มีปัญหาซ้ำ
    แล้วได้ 169.254 อีกทั้งสองรอบติดกัน — `RestartForHealthCheck` (T-08) เบากว่านั้นมาก
    (ขอ lease ใหม่เฉย ๆ ไม่แตะ wpa_supplicant/การเลือก SSID เลย) จึงมีโอกาสไม่ช่วยอะไรเลยถ้า
    ต้นตอคือ AP/DHCP-server ของ SSID นั้นเอง ไม่ใช่ตัว client — **ไม่ใช่เหตุผลให้ขยาย scope
    ไปแตะ Wi-Fi failover/SSID-switching** (ยังคงนอก scope ตาม §0 เหมือนเดิม) แต่เป็นเหตุผลที่
    ต้องเน้นย้ำว่า backoff+เพดาน (decision 3) คือ safety net ไม่ใช่ guarantee ว่าจะ"แก้ได้เสมอ"
    — ข้อความ event log ตอนชนเพดาน (T-09 `logCeilingOnce`) ควรสื่อสารให้ผู้ใช้เข้าใจว่าอาจต้อง
    ตรวจฝั่ง AP/SSID เอง ไม่ใช่แค่ "restart แล้วจะหายเอง" — ให้ ai-developer เขียนข้อความ log
    ให้ actionable กว่าค่า generic ทั่วไป
12. **RAM-only state ของ checker (T-09 `states map`) reset ทุกครั้งที่ pigate restart** —
    ระหว่างการทดสอบจริงที่พบ evidence นี้ pigate ถูก restart เอง 3 ครั้งใน ~10 นาที (เพื่อ
    ทดสอบ #76/#79 คนละเรื่อง) ถ้า checker ทำงานอยู่ `restartsSinceRecover`/เพดานจะ reset ตาม
    ไปด้วยทุกรอบ restart — ในสภาพใช้งานจริง pigate ไม่ควร restart ถี่ขนาดนี้ตามปกติ จึงยอมรับ
    เป็น known limitation ไม่ต้องแก้รอบนี้ (เช่น persist state ลง DB) แต่บันทึกไว้เผื่อพบปัญหา
    ซ้ำบนบอร์ดที่ crash/restart บ่อยผิดปกติในอนาคต

## 6. Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก Task เสร็จ — สำหรับ ai-qa)

- [ ] `cd backend && go build ./... && go vet ./... && go test -race ./...` เขียวทั้งหมด
- [ ] Unit: T-10 ทุกเคสผ่าน (`classifyAddrs`, `decideNextState`, mock-mode ไม่แตะ netlink จริง)
- [ ] Unit: T-06 repository round-trip ผ่าน; handler T-12 คืน 400 เมื่อ payload นอกช่วง
      validate, 200 พร้อมค่าที่บันทึกจริงเมื่อ payload ถูกต้อง
- [ ] เทสต์เดิมทั้งหมดของ `dhcpcd_test.go`, `netlink_monitor_test.go`, `interface_test.go`,
      `backup_test.go` ยังผ่าน (ไม่มี regression จากการแก้ `dhcpcd.go`/`event_bus.go`)
- [ ] Mock mode (`-mock=true`): รัน backend, ตั้งค่า interface โหมด DHCP, ยืนยัน checker
      tick ทำงานโดยไม่ crash และไม่มี log พยายามแตะ netlink จริง
- [ ] API: `curl` GET/PUT `/api/system/dhcp-health` ทำงานถูกต้องตาม role (super_admin
      แก้ได้, read-only role แก้ไม่ได้ตาม `RoleReadOnlyMiddleware`, `-disable-edit=true`
      บล็อก PUT)
- [ ] บอร์ดจริง (physical access, มี interface สำรองเข้าเครื่อง): จำลอง 169.254 ค้าง
      (เช่น ปิด AP หรือตัด DHCP server ทิ้งไว้เกิน backoff ที่ตั้งไว้) → log เห็นลำดับ
      "linklocal_detected" → ลบ address หรือ restart ตามเงื่อนไข → ได้ IP จริงกลับมาเอง
      โดยไม่ต้อง intervention
- [ ] บอร์ดจริง: interface ที่ carrier ไม่พร้อม (Wi-Fi ยังไม่ associate, สาย LAN หลุด) —
      ยืนยันจาก log ว่า **ไม่มี** strike ใด ๆ ถูกนับ ไม่มี restart/delete เกิดขึ้นเลย
- [ ] บอร์ดจริง: ตั้ง `maxRestartsBeforePause` ต่ำ ๆ (เช่น 1) แล้วจำลองสถานการณ์แก้ไม่หาย →
      ยืนยัน checker หยุด restart หลังชนเพดาน และ log แจ้งเตือนแค่ครั้งเดียว ไม่ spam ทุก tick
- [ ] Backup: Export แล้ว Import กลับ (พร้อม/ไม่พร้อม users) → `dhcpHealthSettings` คง
      ค่าเดิมถูกต้อง, backup v2 เก่า (ไม่มี field นี้) ยัง import ได้ปกติไม่ผิด checksum
- [ ] `docs/openapi.yaml` และ `frontend/public/openapi.yaml` มี path/schema ตรงกัน และ
      ตรงกับพฤติกรรมจริงของ handler
- [ ] Code บน branch ใหม่ที่แตกจาก `main` (เช่น `feat/dhcp-health-checker`) → PR เข้า `main`
      (ห้าม push ตรง; branch `fix/usb-wifi-startup-race` เดิมจบไปกับ PR #79 แล้ว — Caution 8)

## 7. Checklist (Definition of Done)

- [ ] T-01 `kernel/interfaces.go` — เพิ่ม `GetIPv4Addresses`/`DeleteAddress` ใน `NetworkManager`
- [ ] T-02 `kernel/real_network.go` — implement จริงผ่าน netlink
- [ ] T-03 `kernel/mock.go` — implement mock ปลอดภัย 100%
- [ ] T-04 `db/connection.go` — table `dhcp_health_settings` + seed default
- [ ] T-05 `model/types.go` — struct `DhcpHealthSettings`
- [ ] T-06 `db/repository.go` — `GetDhcpHealthSettings`/`UpdateDhcpHealthSettings`
- [ ] T-07 `event_bus.go`+`netlink_monitor.go` — `IsPaused()` accessor
- [ ] T-08 `service/dhcpcd.go` — `RestartForHealthCheck` wrapper
- [ ] T-09 `service/dhcp_health_checker.go` (ใหม่) — ตัว checker หลัก
- [ ] T-10 `service/dhcp_health_checker_test.go` (ใหม่) — เทสต์ครบตามที่ระบุ
- [ ] T-11 `cmd/pigate/main.go` — ต่อสาย construct + `Start(monitorCtx)`
- [ ] T-12 `api/handlers.go` + `api/router.go` — GET/PUT `/api/system/dhcp-health`
- [ ] T-13 `model/backup.go` + `service/backup.go` + `db/backup_repo.go` — backup wiring
- [ ] T-14 `docs/openapi.yaml` + `frontend/public/openapi.yaml` — sync ทั้งสองไฟล์
- [ ] Final Acceptance §6 ครบทุกข้อ
- [ ] ไม่ต้องแก้ README Feature Status รอบนี้ (ฟีเจอร์ยังไม่มี UI ให้ผู้ใช้เห็น — รอ phase
      ถัดไปที่เพิ่ม frontend ก่อนค่อยอัปเดตตาราง)
