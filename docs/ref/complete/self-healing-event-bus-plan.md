# Self-healing Infrastructure — internal event bus + startup reconciliation (Issue 48)

> แผนงานโครงสร้างพื้นฐาน: เปลี่ยน `netlink_monitor.go` จาก dispatch แบบ hardcode
> (เรียก dhcpcd ตรง ๆ + reconcile รวมที่ทำแค่ routing/DNS client) เป็น **internal
> event bus** ที่แปลง netlink event ดิบ → semantic event (`InterfaceAdded` ฯลฯ)
> ให้ service ต่าง ๆ subscribe เอง + อุด startup reconciliation ที่ยังขาด (QoS)
> — เป้าหมายเชิงผู้ใช้: interface ที่หายแล้วกลับมา ระบบกลับมาทำงานเอง**โดยไม่ต้องแตะ UI**
>
> เขียนเมื่อ: 2026-07-14 · Reference branch: `feat/self-healing-event-bus`
> เกี่ยวข้อง: GitHub issue #48 · หลักฐาน field test ในคอมเมนต์ของ issue (VLAN สร้างกลับ
> นอก PiGate ค้าง DOWN จนกดใน UI) · ต่อยอดจาก #46 (merged) + #50 (merged)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- **Acceptance test หลัก** (จาก field test 2026-07-14): สร้าง VLAN ผ่าน PiGate →
  `ip link del` นอกระบบ → `ip link add` กลับ → interface กลับมา **UP พร้อม static IP
  + dhcp-range (ถ้าตั้ง DHCP) + QoS rules เดิม โดยไม่ต้องแตะ UI**
- NetlinkMonitor แปลง event ดิบ → semantic events; service subscribe ผ่าน bus
  (stdlib ล้วน) — เพิ่ม subscriber ใหม่ไม่ต้องแก้ constructor ของ monitor อีก
- ทุก subsystem ที่อ้าง interface มี startup path ที่ทน interface หาย (ข้าม + log + ทำต่อ)
  — วันนี้เหลือ **QoS ตัวเดียว**ที่ล้มทั้ง sync (ดู §1)
- Invariant คงเดิม: event handler แก้ได้เฉพาะ **runtime state** — ห้ามลบ user config
  ใน DB อัตโนมัติ; ผู้ใช้รับรู้ผ่าน Event Log ทุกครั้งที่ระบบ self-heal

**Out of scope (ตัดชัด):**
- หน้า UI จัดการ offline interfaces → issue #49
- `listen-address=` refinement ฝั่ง DNS Server + ถอด skip ฝั่ง DHCP gen → คงพฤติกรรม
  ปัจจุบัน (ตาม out-of-scope ของแผน #50)
- External event API (SSE/webhook ให้ frontend) — bus นี้เป็น internal เท่านั้น
- ไม่มี API/route/schema/permission ใหม่ใด ๆ (ไม่แตะ openapi, install.sh, db)

## 1. Current State (สำรวจโค้ดจริง 2026-07-14)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| NetlinkMonitor dispatch | **hardcode**: dhcpcd เรียกตรง (`HandleLinkUpdate`) ทันทีทุก event; ที่เหลือ debounce 500ms → `reconcile()` = routing + DNS client เท่านั้น | `backend/internal/service/netlink_monitor.go:~104-150` (loop), `:~168-186` (reconcile) |
| แยก added/removed | **ทำไม่ได้ในโค้ดปัจจุบัน** — loop ไม่อ่าน `Header.Type` (RTM_NEWLINK/RTM_DELLINK); struct มีให้ (`LinkUpdate.Header unix.NlMsghdr`) | monitor `:~110-119`; `vishvananda/netlink@v1.3.1/link_linux.go:2452` |
| Interface: startup | **ทนแล้ว + recreate VLAN ที่หาย** (issue #20) — ข้าม+log ต่อ interface | `service/interface.go:~46-140` (`InitApplyConfigurationAtStartup`) |
| Interface: running state | **ช่องว่างหลักของงานนี้** — ไม่มี path re-apply config เมื่อ link โผล่กลับ (พิสูจน์ด้วย field test); ปุ่ม UP ใน UI เรียก `SetInterfaceState` ซึ่ง toggle + re-apply address อยู่แล้ว = โค้ด re-apply มีอยู่ แต่ไม่มีใครเรียกจาก event | `service/interface.go:~543-563`, monitor `:~168-186` |
| Routing | ทนแล้ว (skip+log ราย route) + reconcile ผ่าน monitor อยู่แล้ว | `kernel/real_routing.go:~152-186`, `service/routing.go:~412` |
| DNS client | reconcile ผ่าน monitor อยู่แล้ว | monitor `:~181` |
| dhcpcd | รับ event ตรงจาก monitor **แบบไม่ debounce** — logic ต้องเห็นทุก flag transition (Wi-Fi รอ RUNNING) | `service/dhcpcd.go:~55-115` |
| DHCP Server | startup ทนแล้ว (gen ข้าม interface ที่หาย) แต่**ไม่มี re-apply ตอน interface กลับมา** → dhcp-range ของ interface นั้นหายจาก config จนกว่าจะมีคน apply | `kernel/dhcp_server.go:~79` (skip), `service/dhcp_server.go:~33` (`ApplyAll`) |
| DNS Server | **self-heal แล้วผ่าน bind-dynamic (#50) — ไม่ต้อง subscribe** | `kernel/dnsmasq_base.go`, `kernel/dns_server.go` |
| Firewall | ทนแล้ว — nft match `iifname` ด้วยชื่อ ไม่ผูก link index → ไม่ต้อง subscribe | `kernel/real_firewall.go:~987-1017` |
| QoS | **ไม่ทน — บั๊ก startup**: interface มี rule แต่ link หาย → `LinkByName` error → `return` ทิ้งทั้ง sync = interface อื่นไม่ถูก apply ไปด้วย | `kernel/real_qos.go:~62-66`, `service/qos.go:~137` |
| Pause/Resume | มีแล้วที่ monitor; BackupService ใช้คร่อม config import | monitor `:~156-166`, `service/backup.go:~234-235` |
| ลำดับ start | monitor start **ก่อน** hostname/DHCP/DNS/firewall/QoS apply | `cmd/pigate/main.go:~197` vs `~215-275` |
| Full-reapply template | `BackupService.reapply()` = ลำดับ reconcile ครบทุก subsystem ที่ถูกต้องอยู่แล้ว — ใช้เป็น template การเรียงลำดับ | `service/backup.go:~460-506` |
| Event Log | มี `Log(category, action, severity, actor, target, message)` พร้อมใช้ | `service/event_log.go:53` |
| Mock | monitor ปิดตัวเองใน mock mode; MockQos log-only | monitor `:~66-69`, `kernel/mock.go:~293` |

**สรุป:** งานกระจุกที่ `service/` (bus ใหม่ + monitor แปลง event + wiring ใน main.go)
กับ kernel จุดเดียว (`real_qos.go` ให้ทนแบบ routing) — ไม่มี API/DB/frontend/install.sh

## 2. Technical Approach

**Event bus (stdlib ล้วน, ไฟล์ใหม่ `service/event_bus.go`):**

```go
type NetEventKind int
const (
    InterfaceAdded   NetEventKind = iota // link index ที่ไม่เคยเห็น
    InterfaceRemoved                     // RTM_DELLINK
    LinkChanged                          // flag เปลี่ยน (Up/Running) — ให้ dhcpcd
    AddrRouteChanged                     // addr/route event — ให้ routing+DNS reconcile
)
type NetEvent struct { Kind NetEventKind; Name string; Up, Running bool }

// Subscribe แบบ debounced (default 500ms — coalesce เป็นครั้งเดียว) หรือ immediate
// (ได้ทุก event ตามลำดับ — สำหรับ dhcpcd) แต่ละ subscriber มี goroutine+queue ของตัวเอง
// เพื่อไม่ให้ handler ที่ช้า (restart dnsmasq ผ่าน D-Bus) block ตัวอื่น/event loop
func (b *NetEventBus) Subscribe(name string, kinds []NetEventKind, mode SubMode, fn func(NetEvent))
func (b *NetEventBus) Publish(e NetEvent)
func (b *NetEventBus) Pause() / Resume()   // ย้าย semantics เดิมของ monitor มาที่ bus
```

**Monitor เป็น translator:** track ชุด link index ที่รู้จัก (seed ด้วย `netlink.LinkList()`
ตอน Start) — `RTM_NEWLINK` + index ใหม่ = `InterfaceAdded`; index เดิม = `LinkChanged`;
`RTM_DELLINK` = `InterfaceRemoved` (ลบจากชุด); addr/route event = `AddrRouteChanged`

**Subscriptions (wiring ใน main.go):**

| Subscriber | Kind | Mode | ทำอะไร |
|---|---|---|---|
| InterfaceService | InterfaceAdded | debounced | `ReapplyInterfaceByName(name)` — apply config จาก DB ของ link นั้น + recreate VLAN ลูกถ้า name เป็น parent |
| DhcpcdService | LinkChanged (+Added/Removed) | **immediate** | `HandleLinkUpdate` logic เดิม (ย้ายจาก direct call) |
| RoutingService+DNSService | AddrRouteChanged, LinkChanged | debounced | `reconcile()` เดิม (routing + DNS client) |
| DhcpServerService | InterfaceAdded | debounced | `ApplyAll()` — เติม dhcp-range ที่เคยถูก skip กลับ |
| QosService | InterfaceAdded | debounced | `SyncToKernel()` — ติด qdisc ให้ link ที่กลับมา |
| EventLogService | Added/Removed | immediate | บันทึกให้ผู้ใช้รับรู้ (self-healing principle) |

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
- *เรียก `InitApplyConfigurationAtStartup` ทั้งก้อนเมื่อมี InterfaceAdded* — ง่ายกว่า แต่
  `ConfigureWifi` จะ re-run กับ Wi-Fi ทุกตัว (เขียน config + RECONFIGURE = เด้ง
  connection) ทั้งที่ event เกี่ยวกับ link เดียว → เลือก re-apply แบบ scoped รายชื่อ
- *Third-party event bus / message queue lib* — ขัด minimal-deps; ความต้องการแค่
  fan-out ในโปรเซสเดียว channel+goroutine พอ
- *ให้ handler ลบ config ที่ dangling ตอน InterfaceRemoved* — **ห้าม** ขัด invariant
  (ดู Caution 6) — Removed ใช้แค่ log + housekeeping ชุด index
- *Startup reconciliation loop แยกต่างหาก (periodic timer)* — ซ้ำซ้อนกับ event-driven
  และเปลือง CPU/SD; boot-time apply + event ครอบเคสครบแล้ว (ไฟดับ = reboot = boot path)

**Template ในโค้ดเดิม:** ลำดับ re-apply ตาม `backup.go:~460-506`; tolerance pattern
ตาม `real_routing.go:~152-186`; debouncer เดิมใน `netlink_monitor.go:~15-36` ใช้ซ้ำได้

## 3. Steps (เรียงชั้นใน → นอก)

### Step 1 — kernel: QoS ทน interface ที่หาย
**File:** `backend/internal/kernel/real_qos.go:~62-66` (ใน `ApplyQosRules`)
- `LinkByName` error → `log.Printf` warning + `continue` interface ถัดไป (ห้าม return)
  — pattern เดียวกับ `real_routing.go:~152-156`; จุดอื่นใน loop เดียวกันที่ `LinkByName`
  ซ้ำ (`:~224,~243,~257,~302` ฝั่ง clear/ingress) เช็คให้ครบว่าทนแบบเดียวกัน

> `interfaces.go` ไม่ต้องแก้ (signature เดิม); `mock.go` ไม่ต้องแก้ (log-only ปลอดภัยแล้ว)

### Step 2 — service: event bus ใหม่ + tests
**File:** `backend/internal/service/event_bus.go` (**ไฟล์ใหม่**) + `event_bus_test.go` (**ใหม่**)
- ตาม §2: Subscribe (debounced/immediate) / Publish / Pause+Resume; ย้าย `debouncer`
  เดิมจาก monitor มาใช้; immediate mode ต้องรักษาลำดับ event (queue ต่อ subscriber)
- Test: coalesce ของ debounced, ลำดับของ immediate, Pause กัน dispatch, Resume กลับมา,
  handler ช้าไม่ block subscriber อื่น

### Step 3 — service: scoped re-apply ใน InterfaceService
**File:** `backend/internal/service/interface.go`
- แยกเนื้อ loop ราย interface ใน `InitApplyConfigurationAtStartup` (`:~85-140` ส่วน
  wifi/toggle/configure) เป็น helper `applyOneInterface(iface model.NetworkInterface)`
  แล้วให้ startup loop เรียก (ลด duplication)
- เพิ่ม `ReapplyInterfaceByName(name string)`: หาแถว DB ตามชื่อ → ไม่พบ = no-op
  (link ที่ PiGate ไม่จัดการ); พบ → `applyOneInterface`; แถม: วนหา VLAN ลูกใน DB ที่
  `VlanParent == name` และไม่มีใน kernel → recreate + apply (เคส USB NIC parent กลับมา)

### Step 4 — service: monitor เป็น translator + publish เข้า bus
**File:** `backend/internal/service/netlink_monitor.go`
- Constructor เปลี่ยนเป็น `NewNetlinkMonitor(repo, bus)` — ตัด routingService/dnsService/
  dhcpcdService ออก (ย้ายไป subscribe ใน main.go); `reconcile()` ย้าย logic ไปเป็น
  subscription ของ routing+DNS (method ใหม่เล็ก ๆ ใน service ที่เกี่ยว หรือ closure ใน main)
- Loop: อ่าน `linkUpdate.Header.Type` + ชุด known index (seed `netlink.LinkList()`)
  → publish semantic event ตาม §2; `Pause/Resume` เดิม delegate ไป bus (call site
  ใน `backup.go:~234-235` ไม่ต้องแก้)

### Step 5 — wiring ใน main.go + ย้ายจุด start
**File:** `backend/cmd/pigate/main.go:~160-197, ~215-275`
- สร้าง bus ก่อน services → ผูก subscriptions ตามตาราง §2 → inject bus เข้า monitor
- **ย้าย `netlinkMonitor.Start` จาก `:~197` ไปหลัง QoS `InitApplyConfig` (`:~275`)**
  — กัน event ระหว่าง boot ยิง re-apply ซ้อนกับ startup apply (ดู Caution 4)
- Event log: subscriber บันทึก Added/Removed ด้วย category network ที่มีอยู่ใน
  `model` (เช็ค constant จริงตอน implement — ยังไม่ได้เช็คชื่อ)

### Step 6 — tests ระดับ service
**Files:** `service/interface_test.go` (ต่อ pattern `TestDeleteVlanInterface`),
`event_bus_test.go` (Step 2)
- `ReapplyInterfaceByName`: interface ใน DB + mock kernel → apply ถูกเรียก; ชื่อไม่อยู่ใน
  DB → no-op; VLAN ลูกของ parent ถูก recreate
- ฝั่ง `real_qos.go` unit test ไม่ได้ (ต้อง netlink จริง) → ทดสอบบน VM (ดู DoD)

### Step 7 — docs
**File:** `docs/ref/complete/dns-system-design.md` หรือไฟล์ design ใหม่
`docs/ref/self-healing-design.md` (ตอนปิดงาน) — บันทึก two-state model + ตาราง
subscription + invariants; **ไม่มี openapi change** (ไม่มี API ใหม่/เปลี่ยน semantics)

## 4. Related API

ไม่มี route ใหม่และไม่มี endpoint ใดเปลี่ยนพฤติกรรม — งานนี้เป็น internal event flow
ทั้งหมด; `-disable-edit` / role middleware ไม่เกี่ยว (ไม่มี mutation endpoint ใหม่)
Self-healing actions โผล่ให้ผู้ใช้เห็นทาง GET `/api/events` (Event Log) ที่มีอยู่แล้ว

## 5. Cautions

1. **`RTM_NEWLINK` ≠ "สร้างใหม่"** — kernel ยิง NEWLINK ทุกครั้งที่ flag/attr เปลี่ยน
   (up/down ก็ NEWLINK) ถ้าตีความทุก NEWLINK เป็น `InterfaceAdded` → re-apply storm
   ทุกครั้งที่ link กระพริบ → ต้อง track ชุด known index (seed จาก `LinkList()` ตอน
   Start) และ Added = index ที่ไม่เคยเห็นเท่านั้น
2. **Loop จาก event ที่ตัวเองสร้าง**: `ReapplyInterfaceByName` เรียก ToggleInterface/
   ConfigureInterface → ยิง LinkChanged/AddrRouteChanged กลับเข้า bus → subscriber
   debounced ทำงานซ้ำ ต้อง**converge**: handler ทุกตัว idempotent (apply state เดิมซ้ำ
   = no-op ฝั่ง kernel) และ `InterfaceAdded` ไม่ re-fire สำหรับ index เดิม — ถ้า test
   บน VM แล้วเห็น log reconcile วนไม่หยุด = เงื่อนไขนี้พัง ห้าม merge
3. **dhcpcd ห้าม debounce**: logic Wi-Fi รอ RUNNING flag (`dhcpcd.go:~55-68`) ต้องเห็น
   ทุก transition — ถ้า coalesce แล้ว event "UP แต่ยังไม่ RUNNING" ถูกกลืน dhcpcd จะ
   ไม่ start ตอน RUNNING มา → Wi-Fi ไม่ได้ IP; subscription ต้องเป็น immediate mode
   และรักษาลำดับ
4. **Race ระหว่าง boot**: ปัจจุบัน monitor start ก่อน DHCP/DNS/firewall/QoS apply
   (`main.go:~197`) — เดิม reconcile ทำแค่ routing+DNS เลยพอรอด แต่พอ subscriber
   รวม `ApplyAll`/`SyncToKernel` แล้ว event ช่วง boot (dhcpcd start ยิง link event เพียบ)
   จะ apply ซ้อนกับ startup path → ย้าย `Start()` ไปหลัง apply ครบทุก subsystem
   (Step 5); ช่องว่าง route drift สั้น ๆ ระหว่าง boot ยอมรับได้เพราะ startup apply
   เพิ่งทำสด ๆ
5. **Pause ต้องคลุม subscriber ใหม่ทั้งหมด**: `backup.go:~234` pause คร่อม config
   import — ถ้า bus ไม่ honor pause ให้ครบ DHCP/QoS re-apply จะยิงกลาง restore
   (DB กำลังถูกแทนที่) → apply state ผสมสองชุด; Pause/Resume ต้องอยู่ที่ระดับ bus
   dispatch ไม่ใช่แค่ reconcile เดิม
6. **ห้ามลบ user config ใน handler ทุกกรณี** (invariant จาก issue #48): `InterfaceRemoved`
   = USB NIC หลุดชั่วคราว/VLAN ที่กำลังจะกลับมา — handler แตะได้เฉพาะ runtime state
   (kernel/dnsmasq/nft); การลบ config เป็น explicit user action เท่านั้น (หน้าที่ #49)
7. **Handler ช้า block event loop**: `ApplyAll` ของ DHCP restart dnsmasq ผ่าน D-Bus
   (วินาที) — ถ้า dispatch ใน goroutine เดียวกับ loop ที่อ่าน netlink channel,
   channel เต็ม → kernel drop subscription → monitor ตาย → ต้อง dispatch ผ่าน
   queue/goroutine ต่อ subscriber (Step 2)
8. **ทดสอบบนเครื่องจริง**: mock mode ปิด monitor (`:~66-69`) — flow event จริงต้องทดสอบ
   บน VM/บอร์ด; ความเสี่ยง lock-out ต่ำ (ไม่แตะ SSH/routing structure) แต่ dnsmasq จะ
   restart ตาม re-apply — ทดสอบนอกเวลาใช้งาน LAB; ทำ acceptance test ใน §0 +
   regression: reboot ปกติ, backup import (Pause ทำงาน), Wi-Fi client ยังได้ IP

## 6. Summary Checklist (Definition of Done)

- [ ] `kernel/real_qos.go` — missing interface = skip+log ทุกจุด `LinkByName` (Step 1)
- [ ] `service/event_bus.go` + `event_bus_test.go` — bus + Pause/Resume + tests
- [ ] `service/interface.go` — `applyOneInterface` helper + `ReapplyInterfaceByName`
      (+ recreate VLAN ลูก)
- [ ] `service/netlink_monitor.go` — translator + known-index tracking + delegate pause
- [ ] `cmd/pigate/main.go` — สร้าง bus + subscriptions ตามตาราง §2 + ย้ายจุด `Start()`
- [ ] Event Log บันทึก InterfaceAdded/Removed + self-heal actions
- [ ] `service/interface_test.go` — tests ของ `ReapplyInterfaceByName` 3 เคส
- [ ] `go build ./...` + `go test ./...` ผ่าน (backend); frontend ไม่แตะ (ยืนยันไม่มี diff)
- [ ] VM/บอร์ด: acceptance test §0 (ip link del/add → กลับมาเองครบ IP/DHCP/QoS)
- [ ] VM/บอร์ด: link flap (down/up หลายครั้งเร็ว ๆ) → ไม่มี reconcile loop ใน log (Caution 2)
- [ ] VM/บอร์ด: Wi-Fi client ได้ IP ปกติ (dhcpcd immediate mode — Caution 3)
- [ ] VM/บอร์ด: reboot ปกติ + backup import กลาง runtime (Pause คลุม — Caution 5)
- [ ] VM/บอร์ด: QoS — ตั้ง rule บน interface สองตัว ลบตัวหนึ่งนอกระบบ → reboot →
      อีกตัว**ยังถูก apply** (บั๊กเดิม Step 1); สร้างกลับ → qdisc กลับมาเอง
- [ ] Docs: design content ลง `docs/ref/self-healing-design.md` ตอนปิดงาน
- [ ] เสร็จแล้วย้ายไฟล์นี้ไป `docs/ref/complete/` + ปิด issue #48
