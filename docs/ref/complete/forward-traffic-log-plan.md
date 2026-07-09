# Forward Traffic Log — หน้าดู log ทราฟฟิกที่วิ่งผ่าน firewall (forward chain) แบบเรียลไทม์

> เอกสารแผนงานสำหรับฟีเจอร์: อ่านเหตุการณ์ PASS/DROP ของแพ็กเก็ตที่ *วิ่งผ่าน*
> เครื่อง (LAN↔WAN, forward chain) จาก kernel จริงผ่าน **NFLOG (netlink)**
> เข้า ring buffer ใน RAM แล้วแสดงบนหน้า UI ใหม่ "Forward Traffic" —
> แทนที่ข้อมูล mock ที่หน้า Dashboard Recent Logs ใช้อยู่ทุกวันนี้
> พร้อมสร้าง **กลุ่มเมนูใหม่ "Log & Report"** ใน sidebar และย้ายเมนู
> Event Logs (PR #16) เข้ามาเป็น Log & Report › System Events
>
> วันที่เขียน: 2026-07-09 · Branch อ้างอิง: `main` (d0997c5)
> ⚠️ PR #16 (`feat/central-event-log`) ยังไม่ merge และแตะไฟล์เดียวกันหลายจุด
> — **เริ่มงานนี้หลัง PR #16 merge แล้วเท่านั้น** (ดู §5.1)
> README Feature Status: แถว Dashboard ระบุ "firewall log stream still reads
> the in-memory ring buffer (no kernel log reader yet)" → เป้าหมายคือปิดช่องนี้

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย:**

- แพ็กเก็ตที่ชนกฎ forward ซึ่งเปิด Log ไว้ (ปุ่ม log ต่อ policy ที่มีอยู่แล้ว)
  และแพ็กเก็ตที่โดน final FWD DROP ต้องไหลเข้าแอปเป็น `model.FirewallLog`
  จริง ๆ ไม่ใช่ mock — เก็บใน **RAM ring buffer เท่านั้น** (ห้ามลง SQLite —
  ความถี่ระดับแพ็กเก็ต, constraint ถนอม SD card ตาม tech_stack_design.md §8)
- สร้าง **กลุ่มเมนูใหม่ "Log & Report"** ใน sidebar (วางระหว่าง
  Policy & Objects กับ System) เป็นบ้านรวมของหน้า log ทั้งหมด:
  - **Log & Report › Forward Traffic** (หน้าใหม่ของแผนนี้): ตาราง log
    ล่าสุด, filter PASS/DROP, ค้นหา src/dest/port, polling + ปุ่ม pause
  - **Log & Report › System Events**: ย้ายเมนู Event Logs เดิมจาก
    System › Event Logs (PR #16) มาไว้กลุ่มนี้ พร้อมเปลี่ยน label เป็น
    "System Events" (ตัวหน้า/ฟีเจอร์ไม่เปลี่ยน — ย้ายที่อยู่ + route เท่านั้น)
- Dashboard "Recent Logs" เดิมกลายเป็นข้อมูลจริงโดยอัตโนมัติ (ใช้ buffer
  ตัวเดียวกัน) โดย **ไม่ต้องแก้เส้น API เดิม**

**นอกขอบเขต:**
- **Log ของ input chain (INP ACCEPT/AUDIT/DROP)** — ยังยิงเข้า kernel log
  (dmesg) ตามเดิม ไม่ดึงเข้าแอปใน phase นี้ เพื่อคุมปริมาณงานและปริมาณ event
- **Persist log ลง SQLite / export ไฟล์ / remote syslog** — ขัด constraint
  SD card; เป็นงานอนาคตถ้าจำเป็น
- **SSE stream** — `HandleLogStream` เดิม (`handlers.go:~1803`) ส่งรายการ
  ล่าสุดซ้ำ ๆ ทุก 3 วิ ไม่ใช่ pattern ที่ควรลอก; หน้าใหม่ใช้ polling แบบ
  `usePoll` เหมือนทุกหน้า (เส้น SSE เดิมคงไว้ ไม่แตะ)
- **Rule hit counters (`expr.Counter`)** — เป็น roadmap แยกใน
  `project_status.md:~157` คนละกลไกกับ log

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน บน `main`)

| ส่วน | สถานะ |
|---|---|
| กฎ nftables ฝั่ง log | **มีครบแล้ว แต่ยิงเข้า kernel log (printk) ที่ไม่มีใครอ่าน** — `real_firewall.go` ใช้ `expr.Log` + `NFTA_LOG_PREFIX` อย่างเดียว: per-policy prefix `[PiGate] FWD ACCEPT/DROP` เมื่อ `PolicyRule.Log` เปิด (บรรทัด ~494-501 → `buildRuleExpressions` ข้อ 7 ~บรรทัด 861), final `FWD DROP` ~524; ฝั่ง input ~110-390 |
| ตาราง filter | `pigate` family **inet** (`real_firewall.go:56-59`) → payload ที่ NFLOG ส่งมาเป็นได้ทั้ง IPv4 และ IPv6 — parser ต้องดู version nibble |
| ปุ่มเปิด/ปิด log ต่อ policy | มีครบทั้งเส้น: `PolicyRule.Log` + `HandleTogglePolicyLog` (`handlers.go:~809`) + UI หน้า FirewallPolicy — ฟีเจอร์นี้แค่ทำให้ log ที่เปิดไว้ "มีที่ไป" |
| ตัวอ่าน kernel log | **ไม่มี** — ตรงกับ roadmap `project_status.md:~160` (เสนอ /dev/kmsg หรือ journald + SSE ซึ่งแผนนี้จะเลือกทางอื่น — ดู §2) |
| Ring buffer | `logs/ringbuffer.go` — ผูก type `model.FirewallLog`, capacity 50, ใช้เป็น instance เดียวใน `main.go:58`; ข้อมูลจริงในนั้นคือ seed ปลอม 2 แถว (`main.go:61-62`) + `logPowerEvent` (PR #16 จะย้าย seed ไปใต้ mock และถอด logPowerEvent ออก) |
| API เดิม | `GET /api/dashboard/logs`, `POST .../clear`, `GET .../stream` (`router.go:40-42`) — shape `FirewallLog[]` |
| Frontend | Dashboard Recent Logs poll ทุก 10 วิ (`Dashboard.tsx:~602`, `dashboardService.getRecentLogs:~229`); type `FirewallLog` ที่ `data-mockup/mockData.ts:2-11`; **ไม่มีหน้า Forward Traffic และไม่มี group Log & Report** — sidebar บน `main` มี 4 group: (no title)/Network/Policy & Objects/System (`app-sidebar.tsx:~42-76`); PR #16 เพิ่มเมนู Event Logs ไว้ใต้ System ที่ route `/system/logs` |
| Kernel layer interface | `interfaces.go` ไม่มีสัญญาเรื่อง traffic log; `mock.go` firewall ไม่ generate log ใด ๆ |
| Dependencies | `go.mod` มี `mdlayher/netlink v1.11.2` (indirect) อยู่แล้ว; ยังไม่มีไลบรารี NFLOG |
| Capabilities | binary ถือ `cap_net_admin,cap_net_raw` (install.sh) — เพียงพอสำหรับ NFLOG netlink socket, **ไม่ต้องเพิ่มสิทธิ์** |

สรุป: ฝั่ง "ผลิต log" เสร็จอยู่แล้วใน nftables — งานจริงคือ (1) เปลี่ยนปลายทาง
log ของ forward chain จาก printk → NFLOG group, (2) ตัวอ่าน NFLOG ใน kernel
layer + mock, (3) API อ่าน buffer แบบ filter, (4) หน้า UI ใหม่

---

## 2. แนวทางเทคนิค

**เลือก: NFLOG group ผ่าน netlink** — แก้ `expr.Log` ของ forward chain ให้ชี้
group แทน printk แล้วเปิด listener ฝั่ง Go:

```go
// real_firewall.go — จุดสร้าง log expr ฝั่ง forward (policy + final drop)
&expr.Log{
    Key:   (1 << unix.NFTA_LOG_GROUP) | (1 << unix.NFTA_LOG_PREFIX) | (1 << unix.NFTA_LOG_SNAPLEN),
    Group: 100,              // = nft "log prefix ... group 100"
    Snaplen: 64,             // header พอ ไม่ copy ทั้ง payload
    Data:  []byte(logPrefix) // prefix เดิม ส่งมากับ NFULA_PREFIX attribute
}
```

ฝั่งอ่านใช้ **`github.com/florianl/go-nflog/v2`** (pure Go, สร้างบน
`mdlayher/netlink` ที่มีใน go.sum แล้ว, ไม่มี cgo) — ได้ raw packet + prefix
ต่อ event แล้ว parse header เอง (IPv4/IPv6: src/dst/proto/port) →
`model.FirewallLog` → push เข้า ring buffer

**ทางเลือกที่ตัดทิ้ง:**
- **อ่าน `/dev/kmsg`** (ตาม roadmap เดิม) — Debian ตั้ง
  `kernel.dmesg_restrict=1` โดย default → ต้องเพิ่ม `cap_syslog` ให้ binary
  (ขยายสิทธิ์ + แก้ install.sh + เครื่องที่ติดตั้งแล้วต้อง setcap ใหม่)
  และต้อง parse ข้อความ text ของ nft ที่ format ไม่ stable — ปฏิเสธ
- **journald** — ไลบรารี sdjournal ต้องใช้ cgo (โปรเจกต์เป็น pure Go/no-CGO
  ทั้งเส้นเพราะ modernc.org/sqlite) — ปฏิเสธ
- **ลอก SSE `HandleLogStream`** — pattern เดิม broken (ส่งแถวล่าสุดซ้ำทุก 3
  วิ ไม่ diff) — ปฏิเสธ, ใช้ polling
- **dependency ใหม่** ขัด nโยบาย minimal deps บางส่วน แต่เข้าเงื่อนไขข้อยกเว้น:
  ไม่มีทางเลือกใน stdlib/`golang.org/x`, ไลบรารีเล็ก เป็นที่รู้จัก และต่อยอด
  module ที่ pin อยู่แล้ว — ต้อง pin version ใน go.sum ตามปกติ

**Pattern ต้นแบบ:** ตัว watcher ตาม `DhcpServerService.StartLeaseWatcher`
(`service/dhcp_server.go:~107` — ถือ ctx + callback); การเพิ่ม interface
คู่ real/mock ตาม `PowerManager`; หน้า UI ตาม `pages/EventLogs.tsx`
(หลัง PR #16 merge — ตาราง+filter+polling ครบ) หรือ `Users.tsx` ถ้ายังไม่มี

---

## 3. ขั้นตอนการทำ (เรียงลำดับ + ไฟล์ที่ต้องแก้)

### Step 1 — interface ใหม่ใน kernel layer
**ไฟล์:** `backend/internal/kernel/interfaces.go` (~บรรทัด 20 ถัดจาก
`FirewallManager`)

```go
// TrafficLogManager streams forward-chain PASS/DROP packet events (NFLOG).
type TrafficLogManager interface {
    WatchForwardTraffic(ctx context.Context, cb func(model.FirewallLog)) error
}
```

### Step 2 — real implementation (NFLOG listener)
**ไฟล์ใหม่:** `backend/internal/kernel/real_traffic_log.go`
- เปิด `nflog.Open(&nflog.Config{Group: 100, Copymode: nflog.CopyPacket,
  Bufsize: ...})` → `RegisterWithErrFunc`
- callback: อ่าน `attr.Prefix` (แยก PASS/DROP + reason จาก prefix
  `[PiGate] FWD ACCEPT/DROP`), parse `attr.Payload` — version nibble แรก
  แยก IPv4/IPv6, ดึง src/dst/proto แล้วอ่าน port จาก transport header
  (TCP/UDP เท่านั้น; อื่น ๆ ใส่ "-")
- **callback ต้อง non-blocking**: ส่งเข้า buffered channel แล้ว goroutine
  ภายใน drain → เรียก cb; ถ้า channel เต็มให้ทิ้ง event + นับ drop counter
  (log สรุปเป็นระยะ) — ห้าม block netlink read loop
- constant group id เดียว แชร์กับ real_firewall.go (เช่น
  `const ForwardNflogGroup = 100` ในไฟล์นี้)

### Step 3 — mock implementation
**ไฟล์:** `backend/internal/kernel/mock.go` — `MockTrafficLog` สุ่ม
FirewallLog 1 แถวทุก ~4 วิ (สลับ PASS/DROP, IP ในช่วง 192.168.x) จนกว่า ctx
จะจบ — ทำให้หน้าใหม่และ Dashboard มีข้อมูลไหลจริงใน dev และเลิกพึ่ง seed
ปลอมใน main.go

### Step 4 — เปลี่ยนปลายทาง log ฝั่ง forward
**ไฟล์:** `backend/internal/kernel/real_firewall.go`
- `buildRuleExpressions` ข้อ 7 (~บรรทัด 861) และ final FWD DROP (~524):
  ใส่ Group/Snaplen ตาม §2 — **เฉพาะสอง log statement ฝั่ง forward นี้**
- ห้ามแตะ log statement ฝั่ง input (~110-390) — โครง 4 section ของ input
  chain ต้องคงเดิมทุกตัวอักษร (constraint หลักใน CLAUDE.md)

### Step 5 — wiring ใน main.go
**ไฟล์:** `backend/cmd/pigate/main.go`
- ขยาย ring buffer `logs.NewRingBuffer(50)` (~บรรทัด 58) → เช่น 500
  (FirewallLog ~8 field/แถว — RAM หลัก KB เท่านั้น)
- เลือก `TrafficLogManager` real/mock ในบล็อกเดียวกับ manager อื่น (~90-118)
- หลัง `monitorCtx` ถูกสร้าง (~175): start goroutine
  `trafficLog.WatchForwardTraffic(monitorCtx, ...)` → callback เติม
  timestamp RFC3339 UTC + `Add()` เข้า ring buffer ตัวเดิมที่ส่งให้
  `api.NewServer` อยู่แล้ว — Dashboard Recent Logs กลายเป็นข้อมูลจริงทันที
- ลบ seed ปลอม (PR #16 ย้ายไปใต้ `if *mockOS` แล้ว → phase นี้ลบทิ้งได้เลย
  เพราะ MockTrafficLog ป้อนข้อมูลแทน)

> **ไม่ต้อง:** สร้าง service layer แยก (ไม่มี business logic — เส้นทางข้อมูล
> คือ kernel → buffer → handler ตรง ๆ), ไม่มีตาราง DB/migration, ไม่แตะ
> `install.sh`/Polkit (CAP_NET_ADMIN พอ), ไม่มี `InitApplyConfig()`,
> ไม่เกี่ยว `netlink_monitor.go`, ไม่แตะ Backup/Restore

### Step 6 — API อ่านแบบ filter
**ไฟล์:** `backend/internal/api/handlers.go` (วางใกล้ `HandleGetRecentLogs`
~310), `backend/internal/api/router.go` (กลุ่ม logs)
- `authRoute("GET /api/logs/traffic", s.HandleGetTrafficLogs)` — query:
  `action` (PASS|DROP), `q` (ค้น src/dest/port/reason), `limit` (default 100,
  max = capacity) → filter ในหน่วยความจำจาก `s.logs.GetAll()`
- เส้น `/api/dashboard/logs*` เดิม **ไม่แตะ** (Dashboard ใช้ต่อ)

### Step 7 — อัปเดตเอกสาร API
**ไฟล์:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync สองไฟล์)
— path `/logs/traffic` + ระบุว่า response ใช้ schema `FirewallLog` เดิม

### Step 8 — Frontend: กลุ่มเมนู Log & Report + หน้าใหม่
**ไฟล์ใหม่:** `frontend/src/services/trafficLogService.ts` — ตาม pattern
`logService.ts` (PR #16) มี mock branch ผ่าน `services/config.ts`
**ไฟล์ใหม่:** `frontend/src/pages/ForwardTraffic.tsx` — ตาราง shadcn:
Time / Action (badge `primary`=PASS, `destructive`=DROP — semantic vars
เท่านั้น) / Src / Dest / Port / Proto / Reason; filter `Select` action,
`Input` ค้นหา, ปุ่ม Pause/Resume polling (ทุก 5 วิ), ปุ่ม Clear
(เรียก `/api/dashboard/logs/clear` เดิม)
**ไฟล์:** `frontend/src/components/app-sidebar.tsx` (~บรรทัด 64 หลัง group
Policy & Objects) — เพิ่ม group ใหม่ก่อน group System:

```ts
{
  title: "Log & Report",
  items: [
    { path: "/logs/traffic", label: "Forward Traffic", icon: ArrowRightLeft },
    { path: "/logs/events",  label: "System Events",  icon: ScrollText },
  ],
},
```

**ไฟล์:** `frontend/src/App.tsx` — เพิ่ม route group ใหม่ `<Route path="logs">`
(คู่กับ group `network`/`policy`/`system` เดิม ~บรรทัด 143-170):
`logs/traffic` → `<ForwardTraffic />`, `logs/events` → `<EventLogs />`

### Step 8.1 — ย้ายเมนู Event Logs เข้ากลุ่มใหม่ (หลัง PR #16 merge)
**ไฟล์:** `frontend/src/components/app-sidebar.tsx`, `frontend/src/App.tsx`
- ลบ entry `/system/logs` ("Event Logs") ที่ PR #16 เพิ่มไว้ใต้ group System
  และลบ route `system/logs` — ย้ายมาเป็น `/logs/events` label
  "System Events" ตาม Step 8 (component `EventLogs.tsx` เดิม ไม่แก้เนื้อหน้า
  นอกจากหัวข้อหน้า ถ้าต้องการให้ตรง label ใหม่)
- **ไม่ต้องทำ redirect** `/system/logs` → `/logs/events`: PR #16 ยังไม่เคย
  release สู่ผู้ใช้จริง ไม่มี bookmark เก่า และ catch-all `<Route path="*">`
  พาไป dashboard อยู่แล้ว
- backend ไม่เกี่ยว — เส้น API `/api/logs/events` (PR #16) ใช้ชื่อกลางอยู่แล้ว
  ไม่ผูกกับตำแหน่งเมนู

**ไฟล์:** `frontend/src/pages/Dashboard.tsx` (~266 `logsToAlerts`) —
format เวลา RFC3339 → HH:MM:SS ก่อนแสดง (ดู §5.5)

### Step 9 — อัปเดตสถานะโปรเจกต์
`README.md` — เพิ่มแถว "Forward Traffic Log" + แก้หมายเหตุแถว Dashboard;
`docs/project_status.md:~160` — เขียนรายการ roadmap นั้นใหม่ให้ตรงกลไกจริง
(NFLOG ไม่ใช่ kmsg/SSE) แล้วติ๊กสำเร็จ

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | พฤติกรรม |
|---|---|---|---|
| GET | `/api/logs/traffic` | ทุก role ที่ login (authRoute) | อ่าน forward log จาก RAM buffer + filter (เส้นใหม่) |
| GET | `/api/dashboard/logs` | ทุก role | เดิม — ได้ข้อมูลจริงขึ้นมาเองเพราะ buffer ตัวเดียวกัน |
| POST | `/api/dashboard/logs/clear` | ทุก role (mutation → super_admin ผ่าน RoleReadOnlyMiddleware) | เดิม — เคลียร์ buffer |

- โหมด `-disable-edit=true`: GET ใช้ได้, clear ถูกบล็อกอัตโนมัติ — ถูกต้องแล้ว
- ไม่มีข้อมูลลับใน payload (แค่ header 5-tuple) — authRoute เพียงพอ

---

## 5. ข้อควรระวัง

1. **ลำดับงานชนกับ PR #16** — PR #16 แก้ `main.go` (บริเวณ seed/wiring),
   `handlers.go`, `router.go`, `openapi.yaml` และเพิ่มหน้า/`--warning` ที่
   แผนนี้ใช้เป็นต้นแบบ → **rebase/เริ่มหลัง merge เท่านั้น** มิฉะนั้น conflict
   ทุกไฟล์หลัก
2. **เปลี่ยน log เป็น group แล้ว printk หาย** — `dmesg | grep PiGate` จะไม่
   เห็นแถว FWD อีก (INP ยังอยู่) และถ้า **binary ไม่ทำงาน/ crash — ไม่มีใคร
   subscribe group** แพ็กเก็ตยังวิ่งปกติ (log statement ไม่ block traffic)
   แต่ log ช่วงนั้นสูญ — ยอมรับได้เพราะเป็น ephemeral log อยู่แล้ว ระบุใน docs
3. **อัตรา event สูง = ความเสี่ยงหลัก** — policy ที่เปิด Log บนทราฟฟิกหนัก
   อาจยิงหลักพัน event/วินาที: (ก) callback ฝั่ง Go ต้อง non-blocking +
   ทิ้งส่วนเกิน (Step 2), (ข) ring buffer โตแค่ capacity — RAM คงที่,
   (ค) ข้อความอธิบายบนหน้า UI ว่า log คือ "สุ่มตัวอย่างล่าสุด" ไม่ใช่บันทึกครบ,
   (ง) ห้าม forward เข้า SQLite/event log เด็ดขาด
4. **ct established accept ไม่ log** (`real_firewall.go:405-414` ไม่มี
   expr.Log) → แพ็กเก็ตส่วนใหญ่ของ connection ที่เปิดแล้วจะไม่โผล่ในหน้า —
   *by design* (เห็นเฉพาะแพ็กเก็ตเปิด connection + ที่โดน drop) ต้องเขียน
   คำอธิบายบนหน้า UI มิฉะนั้น user จะคิดว่า log หาย
5. **รูปแบบเวลา** — mock/seed เดิมใช้ string `"15:04:05"` และ Dashboard
   แสดง `l.time` ดิบ ๆ (`Dashboard.tsx:~270`); entry จริงจะใช้ RFC3339 UTC
   (บทเรียนจาก event log plan §5.8) → ต้องเพิ่ม formatter ใน Dashboard และ
   หน้าใหม่ ไม่งั้น UI โชว์ string ยาวผิดที่ผิดทาง
6. **inet family** — payload มีทั้ง IPv4/IPv6; parser ห้าม assume IPv4
   (version nibble ก่อนเสมอ) และต้องกัน payload สั้นกว่า header
   (Snaplen 64) ไม่ให้ index out of range — เขียน unit test parser ด้วย
   fixture bytes ทั้งสองตระกูล
7. **Mock mode ปลอดภัย 100%** — MockTrafficLog ห้ามเปิด netlink socket ใด ๆ
   (generator ล้วน) — dev รันบนเครื่องจริงด้วย `-mock=true`
8. **การทดสอบบนบอร์ดจริง** — งานนี้แตะ `ApplyRules` ซึ่ง flush ตาราง
   `pigate` ทั้งตาราง: ถ้า build expr ผิด อาจ apply ruleset เพี้ยนจนหลุดจาก
   เว็บ/SSH ได้ → ทดสอบ `-mock=true` + `go test` ก่อน, ขึ้นบอร์ดเฉพาะตอน
   เข้าถึงเครื่องกายภาพได้, ลำดับทดสอบ: apply rules → ping ผ่าน NAT →
   เปิด Log บน policy เดียว → ดูหน้า Forward Traffic → ปิด Log คืน;
   `go build/test ./...` + `yarn build/lint` ต้องผ่านทั้งคู่

---

## 6. Checklist สรุป (Definition of Done)

> **สถานะ: เสร็จแล้ว (PR #18)** — โค้ด/เอกสาร/ทดสอบ mock ครบทุกข้อ
> เหลือเพียงการทดสอบบนบอร์ดจริง (ข้อสุดท้าย) ที่ต้องเข้าถึงเครื่องกายภาพ

- [x] `kernel/interfaces.go` — เพิ่ม `TrafficLogManager`
- [x] `kernel/real_traffic_log.go` — NFLOG listener + packet parser (ไฟล์ใหม่) + unit test parser (IPv4/IPv6/payload สั้น)
- [x] `kernel/mock.go` — `MockTrafficLog` generator
- [x] `kernel/real_firewall.go` — log ฝั่ง forward (policy + final drop) ชี้ group 100 + snaplen; input chain ไม่แตะ
- [x] `go.mod` — เพิ่ม `florianl/go-nflog/v2` (pin ใน go.sum)
- [x] `cmd/pigate/main.go` — buffer 500, เลือก real/mock, start watcher ใต้ monitorCtx, ลบ seed ปลอม
- [x] `api/handlers.go` + `router.go` — `GET /api/logs/traffic` (filter action/q/limit)
- [x] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — sync
- [x] `frontend/src/services/trafficLogService.ts` + `pages/ForwardTraffic.tsx` (ไฟล์ใหม่, semantic colors, dark/light)
- [x] `app-sidebar.tsx` — group ใหม่ "Log & Report" (Forward Traffic + System Events) + `App.tsx` route group `logs/*`
- [x] ย้าย Event Logs: ลบ `/system/logs` (เมนู+route ของ PR #16) → `/logs/events` label "System Events"
- [x] `pages/Dashboard.tsx` — format เวลา RFC3339 ใน Recent Logs
- [x] `README.md` (แถวใหม่ + แก้หมายเหตุ Dashboard) + `docs/project_status.md:~160`
- [x] ทดสอบ: mock end-to-end (log ไหลเข้าหน้าใหม่ + Dashboard), filter/pause, `-disable-edit` GET ได้/clear โดนบล็อก, เมนู/route ใหม่ทั้งสองหน้าเปิดได้และ `/system/logs` เดิมไม่ค้าง, build/test/lint ผ่านสองฝั่ง
- [ ] บอร์ดจริง: apply rules ไม่หลุดจากเครื่อง, เปิด Log บน policy แล้ว event จริงโผล่, ทราฟฟิกหนักไม่ทำ CPU/RAM บวม (ดูลำดับปลอดภัย §5.8) — **ยังไม่ได้ทดสอบ** (ต้องเข้าถึงเครื่องกายภาพ + `setcap`)
