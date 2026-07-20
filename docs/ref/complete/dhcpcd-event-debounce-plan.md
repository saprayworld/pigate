# dhcpcd-event-debounce — หน่วงการ stop dhcpcd ตอน link flap และตัด event ซ้ำที่ NetlinkMonitor

> แผนงานแก้ Issue #75: ปัจจุบัน DhcpcdService ทำงานทันทีทุก link event ที่จับได้
> โดยไม่มี debounce — link flip-flop สั้นๆ (down→up ภายในไม่ถึงวินาที) ทำให้ dhcpcd
> ถูก StopUnit แล้ว StartUnit ใหม่ ต้องขอ IP ใหม่ทั้งที่ไม่จำเป็น และ kernel ยังยิง
> RTM_NEWLINK ซ้ำด้วย flag เดิมทำให้ stop ถูกเรียกซ้ำ 2 ครั้งติด แผนนี้เพิ่ม (ก) dedupe
> ที่ NetlinkMonitor และ (ข) deferred-stop 2 วินาทีใน DhcpcdService โดยไม่แตะจังหวะ start
>
> เขียนเมื่อ: 2026-07-20 · Reference branch: `fix/dhcpcd-debounce` (แตกจาก `main`)
> อ้างอิง: GitHub Issue #75 · แก้เฉพาะ `backend/internal/service/` — ไม่มีงาน frontend/API/db

## 0. Goal and Scope

**Goal (พฤติกรรมที่ต้องได้):**

1. Link flap สั้นๆ (down แล้วกลับ up ภายใน settle window 2 วินาที) ต้อง**ไม่**เกิดการ
   `StopDhcpcd` เลย — lease เดิมอยู่รอด การเชื่อมต่อไม่หลุด
2. Interface ที่ down จริง (นิ่งเกิน 2 วินาที) ต้องถูก `StopDhcpcd` ตามเดิม (หน่วงได้ไม่เกิน
   settle window)
3. Duplicate event (RTM_NEWLINK ซ้ำด้วย name/up/running เดิม) ต้องไม่ก่อ action ใดๆ ซ้ำ
4. Wi-Fi transition "UP-not-RUNNING → RUNNING" ต้องยังสังเกตได้ครบตามลำดับ และ dhcpcd
   ต้อง start **ทันที** เมื่อ RUNNING (ไม่เพิ่ม latency ให้การขอ lease)
5. `go build ./...` + `go test -race ./...` ผ่าน

**Out of scope (ตัดออกชัดเจน):**
- ไม่แตะ frontend, API handler/route, kernel layer (`interfaces.go`/`dhcpcd.go`/`mock.go`), db schema
- ไม่เปลี่ยนกลไก Debounced mode ของ `NetEventBus` (subscriber อื่นใช้อยู่ ทำงานถูกต้องดี)
- ไม่ทำ config option ให้ผู้ใช้ปรับ settle delay — ใช้ค่าคงที่ในโค้ด (2s ตามที่ issue ขอ 1–2s)
- ไม่แก้พฤติกรรม subscriber อื่น (interface/routing/dns/dhcp-server/qos/event-log)

## 1. Current State (สำรวจโค้ดจริง ณ วันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| การตัดสินใจ start/stop | `applyDhcpcdDecision` ทำทันที ไม่มีหน่วง: `!isUp`→stop, `wifi && !running`→รอเฉยๆ, else→start | `backend/internal/service/dhcpcd.go:~55-68` |
| Event เข้า DhcpcdService | `HandleLinkEvent` subscribe แบบ **Immediate** (ตั้งใจ — Wi-Fi ต้องเห็นทุก transition ตามลำดับ) | `dhcpcd.go:~70-104`, `backend/cmd/pigate/main.go:~196-203` |
| ต้นทาง event | `NetlinkMonitor.handleLinkUpdate` publish `LinkChanged` ทุก RTM_NEWLINK ของ index ที่รู้จัก **โดยไม่เช็คว่า flag เปลี่ยนจริงไหม** — `known` เก็บแค่ `map[int]string` (index→name) ไม่มี flag เดิม | `backend/internal/service/netlink_monitor.go:~135-163`, seed ที่ `~167-179` |
| Bus modes | Debounced = coalesce ต่อ interface, ค่าล่าสุดชนะ, timer เดียวร่วมกันทุก interface reset ทุก event; Immediate = ส่งครบตามลำดับผ่าน queue (64) | `backend/internal/service/event_bus.go:~59-78, ~206-246` |
| Kernel StartDhcpcd | systemd `StartUnit` ผ่าน D-Bus — **idempotent**: unit ที่ active อยู่แล้วไม่ถูก restart, lease ไม่หลุด; `StopUnit` คือ action ทำลายเดียว | `backend/internal/kernel/dhcpcd.go:~36-46`, `dbus_systemd.go:~69-79` |
| Subscriber อื่นของ `LinkChanged` | มีแค่ **routing** (Debounced, reconcile ทั้งตาราง) — subscriber ที่เหลือฟังเฉพาะ `InterfaceAdded`/`InterfaceRemoved`/`AddrRouteChanged` | `main.go:~187-263` |
| Sync paths | `SyncActiveInterfaces` (boot, `main.go:~349`) และ `SyncInterface` (หลัง config save / static→stop) เรียก `applyDhcpcdDecision` ตรงๆ | `dhcpcd.go:~107-203` |
| เทสต์เดิม | `dhcpcd_test.go` มี HandleLinkEvent/ApplyDecision/SyncInterface; `trackingDhcpcdManager` อยู่ใน `hostname_test.go:~25-50` — **ไม่ thread-safe** (append ธรรมดา) | `backend/internal/service/dhcpcd_test.go`, `hostname_test.go` |
| เทสต์ NetlinkMonitor | **ไม่มี** (`netlink_monitor_test.go` ยังไม่ถูกสร้าง) | — |

**สรุป:** งานจริงกระจุกอยู่ 2 ไฟล์ service (`netlink_monitor.go`, `dhcpcd.go`) + เทสต์
ไม่ต้องแตะ kernel/api/db/main.go เลย (wiring เดิมใช้ต่อได้ทั้งหมด)

## 2. Technical Approach

ใช้ **2 กลไกเสริมกัน** เพราะ log ใน issue ชี้ปัญหา 2 ชั้นที่ต่างกัน:

**(ก) Dedupe ที่ NetlinkMonitor** — เปลี่ยน `known` จาก `map[int]string` เป็น
`map[int]linkState{name string; up, running bool}` (seed flag จาก `LinkList` ด้วย)
ใน case RTM_NEWLINK ของ index ที่เคยเห็น: ถ้า `name`, `up`, `running` เท่าเดิมทั้งหมด
→ ไม่ publish (log สั้นๆ ว่า suppressed พอ) — ตัด event ซ้ำแบบ "up=false running=false
สองบรรทัดเวลาเดียวกัน" ทิ้งตั้งแต่ต้นทาง ทุก subscriber ได้ประโยชน์

**(ข) Deferred stop ใน DhcpcdService** — หน่วง**เฉพาะ**การตัดสินใจ stop (`!isUp`) จาก
event path ด้วย per-interface timer (`time.AfterFunc`, ค่าคงที่ `stopSettleDelay = 2s`):

```go
// เพิ่มใน DhcpcdService
mu              sync.Mutex
pendingStops    map[string]*pendingStop // ต่อ interface
stopSettleDelay time.Duration           // default 2s; เทสต์ย่อเหลือ ~20ms

type pendingStop struct { timer *time.Timer; seq uint64 }
```

- Event `!isUp` → บันทึก/reset timer (down ซ้ำ = reset นาฬิกาใหม่ ตามหลัก "รอให้นิ่ง")
- Event `isUp` (ทั้ง running และไม่ running) → **cancel** pending stop ของ interface นั้น
  แล้วเดิน logic เดิม (wifi-not-running → รอ, else → start ทันที)
- Timer fire → ภายใต้ `mu` เช็คว่า entry ยัง current (เทียบ `seq`) ค่อย `stopDhcpcd`
- **Start ยังทำทันที ไม่หน่วง** — ปลอดภัยเพราะ `StartUnit` เป็น idempotent (ดูตาราง §1)
  จึงไม่กระทบ requirement Wi-Fi lease timing เลย และ start ซ้ำจาก event ซ้ำก็ไม่มีผลข้างเคียง

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**

1. *เปลี่ยน subscription เป็น Debounced mode ของ bus* — ตัดทิ้ง: coalesce ค่าล่าสุดทำให้
   intermediate transition ของ Wi-Fi หาย (ตรงตามคอมเมนต์เตือนใน `dhcpcd.go:~70-74`),
   timer ของ Debounced เป็นตัวเดียวร่วมทุก interface (event ของ eth0 เลื่อน flush ของ wlan0 ได้)
   และเพิ่ม latency 500ms+ ให้ start path โดยไม่จำเป็น
2. *Trailing debounce ทุก decision (ทั้ง start และ stop) ใน DhcpcdService* — ตัดทิ้ง:
   เพิ่ม latency 2s ให้การขอ lease ทุกครั้ง และถ้า event stream มาต่อเนื่อง (Wi-Fi scan)
   timer โดน reset เรื่อยๆ จน starve ต้องเพิ่ม max-wait cap ซับซ้อนเกินจำเป็น
3. *แก้ dedupe ที่ monitor อย่างเดียว* — ตัดทิ้ง: แก้ได้แค่ event ซ้ำ แต่ flap down→up จริง
   เป็นคนละ state กัน dedupe ไม่ช่วย ยังสั่ง stop→start อยู่ดี
4. *เพิ่ม state ที่ kernel layer (เช็คว่า unit active ก่อน stop)* — ตัดทิ้ง: ไม่แก้ root cause
   (ปัญหาคือ "ไม่ควร stop เลย" ไม่ใช่ "stop ซ้ำ") และขยาย scope ไป kernel+mock โดยไม่จำเป็น

**Pattern ต้นแบบ:** ใช้สไตล์ timer + mutex + generation เดียวกับ `subscriber.enqueueDebounced`/
`flush` ใน `event_bus.go:~206-246` (stop timer เดิม, ตั้งใหม่, เช็ค staleness ตอน fire)

## 3. Steps (เรียงตามลำดับทำ — ทุก Task อยู่ layer `service` ทั้งหมด)

### T-01: Dedupe LinkChanged ที่ NetlinkMonitor
**File:** `backend/internal/service/netlink_monitor.go`
- เพิ่ม `type linkState struct { name string; up, running bool }` และเปลี่ยน `known`
  เป็น `map[int]linkState` (แก้ทั้ง loop goroutine `~91-127`, `handleLinkUpdate` `~135-163`,
  `seedKnownLinks` `~167-179` — seed ต้องอ่าน flag จาก `l.Attrs().Flags` ด้วย)
- ใน RTM_NEWLINK ของ index ที่ seen: ถ้า state ใหม่ == state เดิมทุก field → log
  `"Link changed (duplicate, suppressed)"` แล้ว return โดยไม่ publish; ถ้าต่าง → update map + publish ตามเดิม
- **ข้อยกเว้น:** ถ้า `u.Attrs() == nil` (name ว่าง, flag เทียบไม่ได้) ให้ publish เสมอ ห้าม suppress
- จุดใช้ `known[...]` ใน addr/route case (`~105, ~118`) เปลี่ยนเป็น `known[idx].name`
- **Acceptance:** คอมไพล์ผ่าน; RTM_NEWLINK ซ้ำ flag เดิมไม่ publish; rename/flag change ยัง publish; DELLINK แล้ว NEWLINK ใหม่ยังเป็น InterfaceAdded
- **depends_on:** —

### T-02: Unit test ของ NetlinkMonitor dedupe (ไฟล์ใหม่)
**File:** `backend/internal/service/netlink_monitor_test.go` (สร้างใหม่)
- ทดสอบ `handleLinkUpdate` ตรงๆ (ไม่ต้อง Start/subscribe kernel จริง): สร้าง
  `netlink.LinkUpdate{Header: unix.NlMsghdr{Type: unix.RTM_NEWLINK}, Link: &netlink.Dummy{LinkAttrs: netlink.LinkAttrs{Index: 5, Name: "eth0", Flags: net.FlagUp | net.FlagRunning}}}`
  ป้อนเข้า monitor ที่ผูกกับ `newNetEventBus(interval สั้น)` + subscriber Immediate ที่เก็บ event ลง channel (assert แบบมี timeout)
- เคสขั้นต่ำ: (1) NEWLINK ซ้ำ 2 ครั้ง flag เดิม → ได้ 1 event; (2) flag เปลี่ยน → ได้ event ใหม่;
  (3) rename ชื่อเปลี่ยน flag เดิม → ได้ event; (4) DELLINK → InterfaceRemoved + index หลุดจาก known,
  NEWLINK ตามหลัง → InterfaceAdded
- **Acceptance:** `go test -race ./internal/service/ -run TestNetlinkMonitor` ผ่าน
- **depends_on:** T-01

### T-03: Deferred stop (settle window) ใน DhcpcdService
**File:** `backend/internal/service/dhcpcd.go`
- เพิ่ม field ตาม §2 (`mu`, `pendingStops`, `stopSettleDelay`, `seq counter`); init ใน
  `NewDhcpcdService` (`~21-27`) ด้วย default `2 * time.Second`
- แยก decision เป็น 2 ทาง:
  - Path event (`HandleLinkEvent` `~75-104`): เปลี่ยนไปเรียกตัวใหม่ เช่น
    `applyDhcpcdDecisionDeferred(name, isWifi, isUp, isRunning)` — `!isUp` → schedule/reset
    pending stop; `isUp` → cancel pending stop แล้วทำ logic เดิม (wifi wait / start ทันที)
  - Path sync (`SyncActiveInterfaces` `~140`, `SyncInterface` `~175, ~202`): ใช้
    `applyDhcpcdDecision` เดิม (อ่าน state จริงจาก kernel มาแล้ว = authoritative) **แต่**ให้
    เข้าไป cancel pending stop ของ interface นั้นก่อนเสมอ (กัน stale stop ยิงทับหลัง save/restore)
- Timer callback: lock `mu` → เช็ค `pendingStops[name]` ยังอยู่และ `seq` ตรง → ลบ entry →
  เรียก `stopDhcpcd` ขณะยังถือ lock (serialize กับ start จาก event goroutine; StopUnit เป็น
  async job submission ผ่าน D-Bus เร็วพอ ไม่ block นาน) → ถ้า stale ให้ return เฉยๆ
- ทุกจุดที่แตะ `pendingStops` และทุกการเรียก `manager.Start/StopDhcpcd` ต้องอยู่ใต้ `mu`
- อัปเดตคอมเมนต์หัว `HandleLinkEvent` (`~70-74`) ให้อธิบายพฤติกรรมใหม่ (Immediate + deferred stop)
- **Acceptance:** คอมไพล์ผ่าน; เทสต์เดิมใน `dhcpcd_test.go` ยังผ่าน (เคส down เดิมอาจต้อง
  ปรับให้รอ settle — ทำใน T-04); `go vet` สะอาด
- **depends_on:** — (อิสระจาก T-01/T-02 ทำขนานเชิง logic ได้ แต่เรียงตามลำดับนี้พอ)

### T-04: Unit tests ของ deferred stop + ทำ tracker ให้ thread-safe
**Files:** `backend/internal/service/dhcpcd_test.go`, `backend/internal/service/hostname_test.go`
- `hostname_test.go:~25-50`: เพิ่ม `sync.Mutex` ให้ `trackingDhcpcdManager` (lock ใน
  `StartDhcpcd`/`StopDhcpcd`/`RestartDhcpcd`/`SetShareHostname` + เพิ่ม getter เช่น
  `snapshotCalls()`) — จำเป็นเพราะ timer callback เรียกจากคนละ goroutine กับ assertion,
  ไม่งั้น `-race` fail
- `dhcpcd_test.go`: ตั้ง `svc.stopSettleDelay = 20 * time.Millisecond` แล้วเพิ่มเทสต์:
  1. **Flap ไม่ stop:** up(start 1 ครั้ง) → down → up ภายใน settle → รอเกิน settle → `stopCalls` ว่าง
  2. **Down จริง stop ครั้งเดียว:** down → down ซ้ำ (duplicate) → รอเกิน settle → `stopCalls == 1`
  3. **Wi-Fi lease timing ไม่ช้าลง:** wlan0 up-not-running (ไม่ start) → running=true →
     `startCalls == 1` **ทันที** โดยไม่ต้องรอ settle window
  4. **Sync cancel pending:** down (pending อยู่) → `SyncInterface` เคส static → stop ทันที
     1 ครั้ง แล้วรอเกิน settle → ไม่มี stop ที่สอง
- ปรับเทสต์เดิมที่ assert เคส down ผ่าน `HandleLinkEvent` ให้รอ settle ก่อนตรวจ
- **Acceptance:** `go test -race ./internal/service/...` ผ่านทั้งหมด
- **depends_on:** T-03

### T-05: Sync คอมเมนต์ wiring และปิดแผน
**Files:** `backend/cmd/pigate/main.go` (คอมเมนต์ `~196-197` เท่านั้น), เอกสารแผนนี้
- ปรับคอมเมนต์ subscriber "dhcpcd" ใน `main.go` ให้สะท้อนว่า Immediate + service หน่วง stop เอง
  (แก้เฉพาะคอมเมนต์ — ไม่แตะโค้ด wiring)
- **ไม่ต้องทำ:** openapi (ไม่มี API เปลี่ยน), README Feature Status (ไม่มี feature ใหม่),
  migration/backup (ไม่มี state ใหม่ใน DB), `install.sh` (สิทธิ์เดิมพอ)
- เมื่อ QA ผ่านแล้ว: ย้ายแผนนี้ไป `docs/ref/complete/` (ขั้นตอนหลัง merge ตามธรรมเนียม)
- **depends_on:** T-01..T-04

## 4. Related API

ไม่มี — ไม่มี route/handler เปลี่ยน, ไม่มีผลกับ `-disable-edit` (กลไกนี้เป็น background
self-healing ไม่ใช่ mutation จากผู้ใช้), ไม่มี openapi ต้อง sync

## 5. Cautions

1. **Suppress LinkChanged กระทบ routing subscriber** — routing subscribe `LinkChanged` ด้วย
   (`main.go:~208-215`) การ suppress เฉพาะกรณี name+up+running เท่าเดิมปลอดภัย เพราะการ
   เปลี่ยน route/address จริงมาทาง `AddrRouteChanged` อยู่แล้ว; **ห้าม** suppress กว้างกว่านี้
   (เช่น เทียบเฉพาะ up) มิฉะนั้น routing reconcile จะพลาด flag transition จริง
2. **`u.Attrs() == nil` ต้อง publish เสมอ** (T-01) — ถ้าเผลอ dedupe ด้วย flag ปริยาย false
   จะกลืน event จริงทิ้ง กลายเป็นบั๊ก dhcpcd ไม่ stop ตอน down จริง
3. **Race timer-vs-event** — timer fire ค้างรอ `mu` อยู่ ระหว่างนั้น event `up` เข้ามา cancel:
   `timer.Stop()` คืน false (fire ไปแล้ว) จึงต้องพึ่ง `seq`/entry check ใน callback เป็นด่านสอง
   ห้ามตัดออก; ทุกการเรียก manager ต้องอยู่ใต้ `mu` เพื่อกัน stop แทรกหลัง start
4. **`trackingDhcpcdManager` ไม่ thread-safe** (พบตอนสำรวจ, `hostname_test.go:~25-50`) —
   เทสต์ใหม่มี goroutine ของ timer เขียน slice พร้อม assertion อ่าน → `-race` fail แน่นอน
   ถ้าไม่เพิ่ม mutex (T-04 ต้องทำก่อน assert)
5. **Sync paths ต้อง cancel pending stop** — ถ้าลืม: user เปลี่ยน config ระหว่าง settle window
   หรือ restore backup แล้ว `SyncActiveInterfaces` สั่ง start เสร็จ → stale timer ยิง stop ตาม
   หลัง 2 วิ → dhcpcd ดับทั้งที่ interface up (บั๊กใหม่ที่แย่กว่าเดิม) — T-03 ระบุจุด cancel ครบแล้ว
6. **Bus Pause/Resume (backup import)** — pending stop timer อยู่นอก bus จึงไม่ถูก pause;
   ยอมรับได้เพราะมันสะท้อน kernel link state (ไม่เกี่ยว DB ที่กำลัง import) และ restore path
   เรียก sync ซึ่ง cancel pending อยู่แล้ว — ไม่ต้องทำอะไรเพิ่ม แต่บันทึกไว้กันคนหลังสงสัย
7. **Mock mode ปลอดภัย 100%** — `NetlinkMonitor.Start` return ก่อนใน mock (`~46-50`) จึงไม่มี
   event จริง; timer ใน DhcpcdService ถ้าถูกกระตุ้นจากเทสต์ก็เรียก mock manager ที่เป็น no-op
8. **ทดสอบบนบอร์ดจริง** — sensitive: กระทบการได้/เสีย IP ของตัวเครื่อง ให้ทดสอบเมื่อมี
   physical access เท่านั้น (จอ+คีย์บอร์ด หรือ interface สำรองที่เป็น static) เคสที่ต้องลอง:
   ถอด/เสียบสาย LAN เร็วๆ (<2s) → lease เดิมอยู่, ปิด AP ค้าง >5s → dhcpcd stop แล้ว
   start ใหม่เมื่อ AP กลับมา, log ต้องไม่มี stop ซ้ำสองบรรทัดติดแบบใน issue
9. **งานนี้เป็น concurrency-sensitive** — reviewer ต้องอ่าน T-03 ละเอียดเป็นพิเศษ
   (lock ordering, seq check) และ CI ต้องรัน `-race` เสมอ

## 6. Summary Checklist (Definition of Done)

- [ ] T-01 `netlink_monitor.go`: `known` เก็บ flag + suppress NEWLINK ซ้ำ (ยกเว้น attrs nil)
- [ ] T-02 `netlink_monitor_test.go` (ใหม่): dedupe/rename/DELLINK-readd ครบ 4 เคส
- [ ] T-03 `dhcpcd.go`: deferred stop 2s + cancel-on-up + sync paths cancel + คอมเมนต์ใหม่
- [ ] T-04 `dhcpcd_test.go` + tracker thread-safe: เทสต์ flap/down-จริง/wifi-timing/sync-cancel
- [ ] T-05 คอมเมนต์ `main.go` wiring sync กับพฤติกรรมใหม่
- [ ] `go build ./...` และ `go test -race ./...` ผ่านทั้ง repo (รันใน `backend/`)
- [ ] PR จาก `fix/dhcpcd-debounce` → `main` อ้าง `Closes #75`

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก Task เสร็จ — สำหรับ ai-qa):**

1. `cd backend && go build ./... && go vet ./... && go test -race ./...` ผ่านทั้งหมด
2. เทสต์ dedupe: NEWLINK ซ้ำ flag เดิม publish ครั้งเดียว; flag/ชื่อเปลี่ยน หรือ attrs nil
   ยัง publish; DELLINK→NEWLINK ได้ InterfaceAdded (จาก `netlink_monitor_test.go`)
3. เทสต์ flap: down→up ภายใน settle window → `StopDhcpcd` ไม่ถูกเรียกเลย
4. เทสต์ down จริง: down นิ่งเกิน settle window → `StopDhcpcd` ถูกเรียกครั้งเดียว (แม้ event down ซ้ำ)
5. เทสต์ Wi-Fi: up-not-running ไม่ start; running=true แล้ว start ทันทีโดยไม่รอ settle
6. เทสต์ sync-cancel: pending stop ถูกยกเลิกเมื่อ `SyncInterface`/`SyncActiveInterfaces` ทำงาน
   และไม่มี stale stop ตามหลัง
7. รัน mock ทั้งระบบ `./pigate-backend -mock=true` ขึ้นได้ปกติ ไม่ panic ไม่มี goroutine leak
   ชัดเจนจาก log
8. (บนบอร์ดจริง — ทำโดยเจ้าของโปรเจกต์ตามเงื่อนไข Caution ข้อ 8 เท่านั้น ไม่ใช่หน้าที่ ai-qa)
   ถอดสาย/เสียบสายเร็ว lease ไม่หลุด; ปิด AP นาน dhcpcd stop/start ถูกจังหวะ; ไม่มี log
   stop ซ้ำสองบรรทัดติด — **ยืนยันแล้ว 2026-07-20** จาก log จริงบนบอร์ด: link flap สั้นๆ
   ถูก defer แล้ว cancel โดยไม่มี `StopDhcpcd` เกิดขึ้นเลย, ปิด AP ค้าง ~27 วิ (ไม่ associate)
   ไม่มี stop เกิดขึ้นระหว่างรอ (ตามดีไซน์ wait-only ของ path นี้), และ dhcpcd start ทันที
   เมื่อ AP กลับมาโดยไม่มี latency เพิ่ม

## 7. Merged

PR [#77](https://github.com/saprayworld/pigate/pull/77) merged เข้า `main` เมื่อ 2026-07-20
(commit `a900724`, merge commit `099cff6`) หลังผ่าน QA loop 1/3 รอบ (พบ + แก้ race-locking
ใน sync paths) และผ่านการทดสอบบนบอร์ดจริงตาม Final Acceptance ข้อ 8 ด้านบน
