# Dashboard Active Sessions Graph — กราฟจำนวน active session แบบ real-time ในแท็บ Detailed

> เพิ่มการ์ดกราฟ "Active Sessions" ในหน้า Dashboard แท็บ Detailed แสดงจำนวน
> conntrack session รวม (สด ~5 วินาที ผ่าน SSE metrics stream ที่มีอยู่แล้ว)
> พร้อม badge แยกตาม proto (TCP/UDP/ICMP/Other, ~10 วินาที) — ไม่มีตาราง
> รายคอนเนกชัน ไม่มีปุ่ม kill session (ตัดออกจาก scope เดิมตามที่เจ้าของ
> โปรเจกต์ยืนยัน)
>
> Written: 2026-07-30 · Reference branch: `feat/traffic-accounting-accuracy-phase2`
> **Dependency: ต้อง merge branch `feat/traffic-accounting-accuracy-phase2` เข้า
> `main` ก่อน** — งานนี้ต่อยอดจาก `TrafficStatsService`/`DumpFlows`/
> `flowKeyFromParts`/`maxTrackedFlowsPerDump` ที่ branch นั้นเพิ่งเพิ่ม
> (merge เข้า `main` แล้วที่ commit `8a2394b` / PR #103 — เลขบรรทัดด้านล่าง
> ตรวจซ้ำกับ `main` เมื่อ 2026-07-30 แล้ว ดู "Verification notes" ท้ายหัวข้อ 1)
> Status in README Feature Status: Dashboard = Partial → เพิ่มหมายเหตุ
> "Active Sessions graph" ใน Completed items ของ Dashboard

## 0. เป้าหมายและ Scope

**เป้าหมาย:** ผู้ใช้เปิด Dashboard → แท็บ Detailed → เห็นการ์ดใหม่ "Active
Sessions" เป็นกราฟเส้น/พื้นที่ (area chart) แสดงจำนวน conntrack session รวม
ที่ขยับเองทุก ~5 วินาที โดยไม่ต้องกด refresh, มีตัวเลข total ปัจจุบัน + %
การใช้งานเทียบ `nf_conntrack_max`, และ badge เล็ก 4 ตัวแยกตาม proto
(TCP/UDP/ICMP/Other) กำกับว่าอัปเดตทุก ~10 วินาที

**เงื่อนไขทางเทคนิคที่ต้องคงอยู่:**
- ไม่มี endpoint ใหม่ — ใช้ SSE metrics stream (`/api/dashboard/performance/stream`)
  และ endpoint `/api/dashboard/traffic-detail` ที่มีอยู่แล้ว
- ไม่แตะ `flowPollInterval` (10s) ของ conntrack accounting poller (เพิ่งจูน
  เสร็จใน phase 2 — ห้าม regress)
- ไม่เขียนอะไรลง SQLite (RAM ring buffer เท่านั้น)

**Out of scope (ตัดออกจากแผนนี้โดยตั้งใจ):**
- ตารางรายคอนเนกชัน (session table แบบ FortiGate/pfSense) — เป็นงานคนละขนาด
  ต้องมี kernel interface ใหม่ (`SessionManager`), endpoint ใหม่, หน้าใหม่
- ปุ่ม "Kill session" — เป็น kernel mutation ตัวแรกที่ไม่ผ่าน nftables/DB
  เสี่ยงตัดคอนเนกชันผิดตัว/ตัดของ admin เอง ไม่รวมในแผนนี้
- Login session monitor (ใครล็อกอินค้างอยู่, revoke ได้) — คนละฟีเจอร์ คนละ
  layer (`api/session.go`) ทำแยกเป็นแผนต่างหากถ้าต้องการ
- Per-device online session (อุปกรณ์ไหนออนไลน์ตั้งแต่เมื่อไร) — phase หลัง
  ต่อยอดจาก DHCP lease event

## 1. Current State (สำรวจจาก branch `feat/traffic-accounting-accuracy-phase2`)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Frontend UI แท็บ Detailed | มีอยู่แล้ว (Protocol Breakdown/Top Talkers/Top Rules) ต้องเพิ่มการ์ดใหม่ | `frontend/src/pages/Dashboard.tsx` `<TabsContent value="detailed">` (~:875-888) |
| Frontend poll ของแท็บ Detailed | มีอยู่แล้ว ทุก 60s | `Dashboard.tsx:59, 789-793` — `TRAFFIC_DETAIL_INTERVAL = 60_000` |
| SSE metrics stream (ใช้อยู่ทุกหน้าผ่าน StatGrid/temp badge) | มีอยู่แล้ว push ทุก 3s (จะปรับเป็น 5s ในแผนนี้) | `backend/internal/service/system_status.go:29` `metricsPushInterval`; frontend consume ผ่าน `useMetrics()` ใน `frontend/src/components/MetricsProvider.tsx` |
| `cpuSampleInterval` (ผูก invariant กับ push interval) | มีอยู่แล้ว 3s, comment ระบุ invariant ว่าต้อง align กับ push | `system_status.go:21`, comment ~:26-29 |
| conntrack poller ที่มีอยู่ (แหล่งข้อมูล per-proto) | มีอยู่แล้ว วน loop flow ทุก 10s | `backend/internal/service/traffic_stats.go:265-311` `processFlows()`, `flowPollInterval` ที่ :27 |
| ตัวนับ session รวม (จาก proc) | **ยังไม่มี** — ต้องเพิ่ม | จุดใหม่: `kernel/real_system_stats.go` |
| kernel interface สำหรับ host telemetry | มีอยู่แล้ว ตรงหน้าที่ (`/proc`, `/sys`, netlink counters) | `backend/internal/kernel/interfaces.go` `SystemStatsManager` (:180-199) |
| mock ของ `SystemStatsManager` | มีอยู่แล้ว | `backend/internal/kernel/mock.go:556-640` (`MockSystemStats`) |
| DTO `SystemMetrics` / `TrafficDetail` | มีอยู่แล้ว ต้องเพิ่มฟิลด์ additive | `backend/internal/model/types.go` (`SystemMetrics` :534, `TrafficDetail` :638) |
| Route/handler สำหรับ SSE + traffic-detail | มีอยู่แล้ว ไม่ต้องเพิ่ม route ใหม่ | `backend/internal/api/handlers.go:2729, 2769-2771` (comment SSE cadence) |
| openapi ของสอง endpoint ที่เกี่ยวข้อง | มีอยู่แล้ว ต้องแก้ description "~3s"→"~5s" + เพิ่ม field | `docs/openapi.yaml:298, 310, 343, 3219 (PerformanceMetrics), 3546 (TrafficDetail)`; `frontend/public/openapi.yaml` (sync) |
| frontend mock branch ของ dashboard service | มีอยู่แล้ว ต้องเพิ่มค่า sessions/sessionHistory | `frontend/src/services/dashboardService.ts:286-342` (mock `getTrafficDetail`), `:471` (`setInterval(emit, 3000)` mock ของ SSE), type `PerformanceMetrics` :58-68, `TrafficDetail` :125-138 |
| chart lib ที่ใช้ซ้ำได้ | Bandwidth chart ของแท็บ Overview ใช้ `recharts` **ตรงๆ** (import ที่ `Dashboard.tsx:20-30`) ไม่ได้ผ่าน `components/ui/chart.tsx` — การ์ดใหม่ให้ลอกโครงจากตัวนี้ | `frontend/src/pages/Dashboard.tsx:357-430` `BandwidthCard` |
| CapabilityBanner สำหรับ host ไม่มี conntrack | มีอยู่แล้ว capability `conntrack` (`real_capability.go:37,149-187`), component `frontend/src/components/CapabilityBanner.tsx` (`<CapabilityBanner id="conntrack" />`) | ใช้ของเดิม ไม่ต้องเพิ่ม capability ใหม่ |

**Verification notes (ตรวจกับ `main` @ `8a2394b`, 2026-07-30):**
- `maxTrackedFlowsPerDump` เป็น const **unexported** และอยู่ในไฟล์ที่มี
  `//go:build linux` (`kernel/real_traffic_account.go:24`) → service เรียกใช้
  ตรงๆ ไม่ได้ ต้องย้าย/สร้าง const **exported** ในไฟล์ที่ไม่มี build tag
  (`kernel/interfaces.go`) แล้วให้ `real_traffic_account.go` ใช้ตัวเดียวกัน
- จุดเรียก `NewTrafficStatsService` มี 3 ที่: `cmd/pigate/main.go:212`,
  `internal/service/traffic_stats_test.go:85`, `internal/api/handlers_test.go:67`
  (ถ้าเปลี่ยน signature ต้องแก้ครบทั้งสาม)
- comment เตือนเรื่อง race ที่แผนอ้างถึงอยู่ที่ `traffic_stats.go:534-551` จริง
- `install.sh` preload `nf_conntrack` + sysctl ครบแล้ว (:470-537) → Step 10
  ยืนยันว่า **ไม่ต้องแก้**

**สรุป:** งานที่เหลือจริงมีแค่ (ก) เพิ่ม 1 เมธอดอ่าน `/proc` ใน kernel
interface ที่มีอยู่แล้ว (ข) เพิ่มตัวนับ per-proto ในลูปที่มีอยู่แล้ว
(ต้นทุนเกือบ 0) (ค) ring buffer + sampler ฝั่ง service (ง) แนบฟิลด์เข้า DTO
เดิม (จ) การ์ดกราฟใหม่ 1 ใบฝั่ง frontend — **ไม่มี endpoint ใหม่ ไม่มี route
ใหม่ ไม่แตะ firewall**

## 2. แนวทางทางเทคนิค

**กลไกที่เลือก:**
- **Total (สด ~5s):** อ่าน `/proc/sys/net/netfilter/nf_conntrack_count` และ
  `nf_conntrack_max` ตรงๆ ด้วย `os.ReadFile` — ไม่มี dependency ใหม่, ต้นทุน
  แทบเป็นศูนย์ (อ่าน int ตัวเดียว)
- **Per-proto (10s):** นับจาก loop ที่มีอยู่แล้วใน `processFlows()` โดยไม่
  เปิด netlink เพิ่ม
- **ส่งเข้า frontend:** แนบเข้ากับ SSE metrics snapshot ที่ push อยู่แล้ว
  (ไม่เปิด connection ใหม่ — สำคัญเพราะ HTTP/1.1 จำกัด ~6 connection/host และ
  ใช้ไปแล้ว 2 stream คือ log stream + metrics stream)
- **History สำหรับ seed กราฟตอนเปิดหน้า:** RAM ring buffer 30 นาที
  (5s × 360 จุด ≈ 7KB) ฝั่ง `TrafficStatsService`, แนบเป็นฟิลด์ใหม่ใน
  response ของ `/api/dashboard/traffic-detail` ที่มีอยู่แล้ว
- **Cadence:** ปรับ `metricsPushInterval` และ `cpuSampleInterval` จาก 3s →
  **5s ทั้งคู่** (คง invariant เดิมที่ว่าค่า CPU ที่ push ต้องไม่เก่ากว่า 1
  รอบ sample) — ผลพลอยได้คือลดการอ่าน `/proc/stat` ลง ~40% และทำให้ push
  กับ session sampler (5s) ตรงจังหวะกันพอดี ไม่ต้อง dedupe เยอะ

**ทำไมไม่ทำแบบอื่น (ตัวเลือกที่ถูกปฏิเสธ):**
- ~~สร้าง kernel interface `SessionManager` ใหม่ทั้งชุด~~ — เกินความจำเป็น
  สำหรับกราฟตัวเลขรวม; เหมาะกับ scope "ตารางรายคอนเนกชัน" ที่ถูกตัดออกไปแล้ว
- ~~stacked area กราฟเดียวรวม total + per-proto~~ — total (5s, จาก proc)
  กับ per-proto (10s, จาก dump ที่ cap ที่ `maxTrackedFlowsPerDump`) คนละ
  freshness และคนละแหล่ง ผลรวมจะไม่บวกกันลงตัว ดูเหมือนบั๊ก → แยกเป็นเส้น
  total + badge proto แทน
- ~~ลด `flowPollInterval` เป็น 5s เพื่อให้ per-proto สดเท่า total~~ — เพิ่ม
  ภาระ dump conntrack 2 เท่าบน Pi และกระทบ poller ที่เพิ่งจูนเสร็จใน
  phase 2 โดยไม่จำเป็น
- ~~เกาะ session sampler กับ tick ของ metrics broadcaster~~ — broadcaster
  ข้าม tick ทั้งหมดเมื่อไม่มีคนเปิดหน้า dashboard (`hasMetricsSubs()`) ถ้า
  เกาะมัน history ring จะมีรูโหว่ทุกครั้งที่ไม่มีใครดูหน้า → ให้ sampler เป็น
  ticker อิสระใน `TrafficStatsService.run()`

**Template ให้ตามสไตล์เดิม:** โครง sampler+ring ให้ดูสไตล์เดียวกับ
`processFlows()`/delta-tracking ที่มีอยู่ใน `traffic_stats.go`; การ์ดกราฟ
ฝั่ง frontend ให้ลอกโครงจาก `BandwidthCard` ของแท็บ Overview
(`Dashboard.tsx:357-430`) ซึ่งใช้ `recharts` ตรงๆ (`ResponsiveContainer` +
`LineChart`) ไม่ต้องดึง `components/ui/chart.tsx` เข้ามาใหม่

## 3. Steps (เรียงจาก layer ในสุดออกมา)

### Step 1 — DTO
**File:** `backend/internal/model/types.go`
เพิ่ม `type SessionCounts struct{ Total, TCP, UDP, ICMP, Other, Max int;
Available bool; ProtoSampledAt string; ProtoCapped bool }` และ
`type SessionHistoryPoint struct{ T string; Total, TCP, UDP, ICMP, Other int }`
เพิ่มฟิลด์ `Sessions SessionCounts` ใน `SystemMetrics` (:534) และฟิลด์
`Sessions SessionCounts` + `SessionHistory []SessionHistoryPoint` ใน
`TrafficDetail` (:638) — ทุกฟิลด์ additive เขียน doc comment อธิบายว่า
`Total` มาจาก `nf_conntrack_count` (สด) ส่วน TCP/UDP/ICMP/Other มาจาก
conntrack dump ของ poller (เก่าได้ถึง 10s, cap ที่ `maxTrackedFlowsPerDump`)
— ห้ามเอามา stack รวมกับ Total ในกราฟเดียว

### Step 2 — kernel: อ่าน nf_conntrack_count/max
**File:** `backend/internal/kernel/interfaces.go` (เพิ่มเมธอดใน
`SystemStatsManager` ที่มีอยู่แล้ว :180-199), `backend/internal/kernel/real_system_stats.go`,
`backend/internal/kernel/mock.go`

เพิ่ม `GetConntrackCount() (count, max int, available bool)`. Real: อ่าน
`/proc/sys/net/netfilter/nf_conntrack_count` และ `nf_conntrack_max` ด้วย
`os.ReadFile` + `strconv` (path คงที่ ไม่มี input จากผู้ใช้ — ต่อจาก
`r.procRoot` เหมือนเมธอดอื่นในไฟล์นี้); อ่านไม่ได้ =>
`(0,0,false)` **ห้ามคืน error หรือ log ทุกครั้ง** (จะสแปม log ทุก 5s บน WSL
— log เตือนครั้งเดียวด้วย `warnOnceKey()` ที่มีอยู่แล้วที่
`real_system_stats.go:51`) ห้ามใช้ `exec.Command`. Mock: คืน
ตัวเลขสังเคราะห์ที่ขยับตามเวลา (เช่นแกว่งรอบค่าคงที่ด้วย `wave()` ที่มีอยู่)
พร้อม `max=262144, available=true` ห้ามอ่าน `/proc` จริง

### Step 3 — service: นับ per-proto ในลูปที่มีอยู่แล้ว
**File:** `backend/internal/service/traffic_stats.go` (`processFlows()` :265-311),
`backend/internal/kernel/interfaces.go` + `backend/internal/kernel/real_traffic_account.go`
(ย้าย const cap ให้ exported)

เพิ่มตัวนับ per-proto ในลูปเดิม: 6=TCP, 17=UDP, 1/58=ICMP, อื่นๆ=Other พร้อม
ธง `capped` เมื่อ `len(flows) >= kernel.MaxFlowsPerDump` — const นี้ปัจจุบัน
เป็น `maxTrackedFlowsPerDump` (unexported, อยู่ในไฟล์ `//go:build linux`)
ต้องย้ายไปประกาศเป็น `MaxFlowsPerDump` ใน `interfaces.go` (ไม่มี build tag)
แล้วให้ `real_traffic_account.go` อ้างตัวเดียวกัน **ห้าม hardcode เลข 50000
ซ้ำในชั้น service** เก็บผลใต้ mutex แยกจาก state เดิม
**ห้ามเปลี่ยน `flowPollInterval` และห้ามแก้ตรรกะ delta/seed/prune เดิม**

### Step 4 — service: sampler 5s + ring 30 นาที + getter
**File:** `backend/internal/service/traffic_stats.go` (`run()` :143-154),
`backend/cmd/pigate/main.go:212`, `backend/internal/service/traffic_stats_test.go:85`,
`backend/internal/api/handlers_test.go:67`

`TrafficStatsService` รับ dep ใหม่ `kernel.SystemStatsManager` (แก้
`NewTrafficStatsService` + จุดเรียกทั้ง 3 ที่; nil-safe เผื่อ test เดิม)
เพิ่ม const `sessionSampleInterval = 5 * time.Second`,
`sessionRingMax = 360`. ใน `run()` เพิ่ม ticker ตัวที่สองใน `select` เดิม
(**ห้ามสร้าง goroutine ใหม่**) → `sampleSessions()`: เรียก
`GetConntrackCount()` + อ่าน per-proto จาก Step 3 → append
`model.SessionHistoryPoint` ลง ring (ตัดหัวเมื่อเกิน `sessionRingMax`) เปิด
เมธอด `SessionCurrent() model.SessionCounts` และ
`SessionHistory() []model.SessionHistoryPoint` — **ต้องคืน copy ไม่ใช่
reference** (แบบเดียวกับ comment เตือนเรื่อง race ที่มีอยู่แล้วที่
`traffic_stats.go:534-551`). `GetTrafficDetail()` เติมฟิลด์ Sessions +
SessionHistory

### Step 5 — แนบเข้า SSE metrics snapshot (ไม่มี endpoint ใหม่)
**File:** `backend/internal/service/system_status.go` (`GetSystemMetrics()`
:305-335), `backend/cmd/pigate/main.go`

เพิ่ม `sessionCurrentFn func() model.SessionCounts` ใน `SystemStatusService`
+ setter `SetSessionCurrentFn(fn)` (เรียกจาก `main.go` หลังสร้างทั้งสอง
service เสร็จ ~:212 กันปัญหาลำดับการสร้าง; ป้องกัน race ด้วย RWMutex เพราะ
`GetSystemMetrics()` ถูกเรียกจาก broadcaster goroutine) ใน `GetSystemMetrics()`
เติม `metrics.Sessions = ...` แบบ nil-safe **ห้ามอ่าน `/proc`
เพิ่มในเส้นทาง compose นี้** (อ่านจาก snapshot ที่ sampler เตรียมไว้แล้ว)
ห้ามแตะ `hasMetricsSubs()` (พฤติกรรมข้าม compose เมื่อไม่มีคนฟังต้องคงอยู่)

### Step 6 — ปรับ cadence 3s → 5s ทั้งระบบ
**File:** `backend/internal/service/system_status.go:21,29`,
`frontend/src/services/dashboardService.ts:471`, `backend/internal/api/handlers.go:2729,2769-2771`

แก้ `metricsPushInterval` และ `cpuSampleInterval` เป็น `5 * time.Second`
พร้อมอัปเดต comment invariant (:26-29); แก้ mock `setInterval(emit, 3000)`
→ `5000`; แก้ comment ที่อ้าง "~3s" ใน handlers.go

> ไม่ต้องเพิ่ม heartbeat แยกสำหรับ SSE — data ยังไหลทุก 5s ซึ่งยังห่างจาก
> idle-close ของ proxy มาก (log stream ใช้ heartbeat 25s อยู่แล้วเป็นตัวเทียบ)

### Step 7 — frontend: type + mock branch
**File:** `frontend/src/services/dashboardService.ts`

เพิ่ม type `SessionCounts`/`SessionHistoryPoint` และฟิลด์ `sessions` ใน
`PerformanceMetrics` (:58-68), `sessions`+`sessionHistory` ใน `TrafficDetail`
(:125-138) ให้ตรงกับ Step 1 เป๊ะ อัปเดต mock branch ทั้งสามที่ (mock
`getPerformanceMetrics` :193-224, mock `getTrafficDetail` :286-342 และ mock
ของ SSE emit :471) ให้ส่งค่าที่ขยับตามเวลา

### Step 8 — frontend: การ์ดกราฟในแท็บ Detailed
**File:** `frontend/src/pages/Dashboard.tsx` (`<TabsContent value="detailed">` :875-888)

เพิ่ม Card ใหม่ "Active Sessions": (ก) seed ครั้งแรกจาก
`trafficDetail.sessionHistory` (poll เดิม 60s — **ห้ามลด
`TRAFFIC_DETAIL_INTERVAL`**) (ข) ต่อสดจาก `useMetrics()` ที่มีอยู่แล้ว:
`useEffect` append `metrics.sessions` เป็นจุดใหม่เมื่อค่า/timestamp เปลี่ยน,
cap array 360 จุด, **dedupe ตาม timestamp เบาๆ** (ticker สองฝั่งไม่ align
เฟสกันเป๊ะ อาจมี push ที่พา snapshot เดิมมาเป็นครั้งคราว) (ค) line chart จาก
`recharts` ลอกโครง `BandwidthCard` (`Dashboard.tsx:357-430`) — ใช้
`ResponsiveContainer`/`LineChart`/`CartesianGrid`/`XAxis`/`YAxis`/`Tooltip`
ที่ import อยู่แล้วที่ :20-30 (ห้ามเพิ่ม dep ใหม่, ไม่ต้องใช้
`components/ui/chart.tsx`) (ง) หัวการ์ดแสดง total ปัจจุบัน +
"(n% ของ nf_conntrack_max)" + badge TCP/UDP/ICMP/Other กำกับ "อัปเดตทุก ~10s"
(จ) เมื่อ `sessions.available=false` แสดง empty state ด้วย
`<CapabilityBanner id="conntrack" />` ที่มีอยู่แล้ว

> ไม่ต้องสร้าง capability ใหม่, ไม่ต้องมี `setInterval` ใหม่ที่ยิง API
> (กราฟกินจาก SSE เดิมทั้งหมด)

### Step 9 — API contract + เอกสาร
**File:** `docs/openapi.yaml`, `frontend/public/openapi.yaml`, `README.md`

เพิ่มฟิลด์ `sessions` ใน schema `PerformanceMetrics` (:3219) และ
`sessions`+`sessionHistory` ใน schema `TrafficDetail` (:3546) แก้
description ของ `/dashboard/performance/stream` จาก "~3s" → "~5s" (:298, :310)
— **ไม่มี path ใหม่** สองไฟล์ต้องเหมือนกันเป๊ะ **ห้ามแก้**
`backend/internal/api/dist/openapi.yaml` (build artifact) README Feature
Status: ระบุว่า Dashboard มีกราฟ Active Sessions แล้ว

### Step 10 (optional) — install.sh
**File:** `install.sh`

ตรวจแล้วว่าบล็อก sysctl/module เดิม (:470-537) preload `nf_conntrack` อยู่แล้ว
→ **ไม่ต้องแก้อะไรเลย** ห้ามเพิ่ม sysctl ใหม่โดยไม่จำเป็น เพราะ
`nf_conntrack_count`/`max` มีอยู่เองเมื่อโมดูลโหลด

## 4. Related API

| Method | Path | ใครเรียกได้ (role) | พฤติกรรม |
|---|---|---|---|
| GET | `/api/dashboard/performance/stream` (มีอยู่แล้ว) | authRoute (ทุก role ที่ login) | เพิ่มฟิลด์ `sessions` ใน snapshot ที่ push, cadence เปลี่ยนจาก ~3s → ~5s |
| GET | `/api/dashboard/traffic-detail` (มีอยู่แล้ว) | authRoute | เพิ่มฟิลด์ `sessions`+`sessionHistory`, poll interval เดิม 60s ไม่เปลี่ยน |

ไม่มี route ใหม่, ไม่มี mutation → **ไม่กระทบ `-disable-edit` mode** (เป็น
read-only ล้วน), ไม่มี query param ใหม่ที่ต้อง validate

## 5. Cautions

1. **ห้ามลด `flowPollInterval` (10s)** — หัวใจของ accounting ที่เพิ่งจูน
   เสร็จใน phase 2; กราฟนี้ไม่ต้องพึ่งมันสำหรับค่า total (มาจาก `/proc`
   โดยตรง)
2. **ห้าม stack per-proto รวมกับ total ในกราฟเดียว** — คนละ freshness (10s
   vs 5s) และ per-proto ถูก cap ที่ `maxTrackedFlowsPerDump` → ผลรวมจะไม่
   บวกกันลงตัวและดูเหมือนบั๊ก ให้แยกเป็นเส้น total + badge proto
3. **ห้ามอ่าน `/proc` ในเส้นทาง compose ของ `GetSystemMetrics()`** — ถูก
   เรียกทั้งจาก SSE push (5s) และทุก request ของ `/api/dashboard/performance`
   ต้องอ่านจาก snapshot ที่ sampler เตรียมไว้ล่วงหน้าเท่านั้น
4. **ห้ามสแปม log เมื่อ `/proc/sys/net/netfilter/*` ไม่มี** (WSL/dev
   workstation) — ใช้ `warnOnceKey()`/`sync.Once` log แค่ครั้งเดียว
5. **Race บน ring buffer** — `SessionHistory()` ต้องคืน copy (เคสเดียวกับ
   comment ยาวที่เตือนไว้แล้วที่ `traffic_stats.go:534-551`) มิฉะนั้น
   frontend polling พร้อมกับ sampler append จะ race
6. **session sampler ต้องเป็น ticker อิสระ** ไม่ใช่เกาะ tick ของ metrics
   broadcaster — broadcaster ข้าม tick ทั้งหมดเมื่อไม่มีคนเปิดหน้า dashboard
   (`hasMetricsSubs()`) ถ้าเกาะมัน ring จะมีรูโหว่ทุกครั้งที่ไม่มีใครดูหน้า
7. **การเปลี่ยน cadence 3s→5s กระทบ StatGrid/temp badge ทุกหน้า** (ไม่ใช่
   แค่ Dashboard) — ผลกระทบ UX ต่ำ (progress bar มี `transition-all
   duration-500` อยู่แล้วทำให้ดูไหลลื่น) แต่ถ้าลด `cpuSampleInterval` เป็น
   5s ด้วย ค่า CPU จะเป็นค่าเฉลี่ยยาวขึ้น สวยขึ้นแต่กลืน spike สั้นๆ (1-2s)
   ไปบ้าง — เป็น trade-off ที่ยอมรับตามที่เจ้าของโปรเจกต์ยืนยันแล้ว
8. **ห้ามเขียน SQLite จากเส้นทางนี้เลย** — ทั้ง ring buffer และ session
   counts อยู่ใน RAM ล้วน (SD card preservation ตาม `tech_stack_design.md`
   §8)
9. **ไม่มี input จากผู้ใช้ไหลเข้าไปที่ไหนเลย** ในงานนี้ (path คงที่, ไม่มี
   query param ใหม่, ไม่มี mutation) → ความเสี่ยงด้าน security ต่ำมาก และ
   หลบประเด็น privacy ของแผนเดิม (ตารางรายคอนเนกชัน) ไปได้ทั้งหมด เพราะ
   แสดงแค่ตัวเลขรวม ไม่เปิดเผยว่าเครื่องไหนคุยกับปลายทางไหน
10. **ต้อง merge `feat/traffic-accounting-accuracy-phase2` เข้า main ก่อน
    เริ่มงานนี้** — ทำแล้วที่ commit `8a2394b` (PR #103): `DumpFlows`,
    `flowKeyFromParts`, `maxTrackedFlowsPerDump`, `processFlows()` อยู่ใน
    `main` เรียบร้อย

## 6. Summary Checklist (Definition of Done)

- [ ] Step 1: DTO `SessionCounts`/`SessionHistoryPoint` เพิ่มใน `model/types.go`
- [ ] Step 2: `SystemStatsManager.GetConntrackCount()` (interface + real + mock)
- [ ] Step 3: นับ per-proto ใน `processFlows()` โดยไม่เพิ่มการเรียก netlink
- [ ] Step 4: sampler 5s + ring 360 จุด + `SessionCurrent()`/`SessionHistory()` (คืน copy)
- [ ] Step 5: แนบ `Sessions` เข้า `GetSystemMetrics()` แบบ nil-safe
- [ ] Step 6: cadence 3s→5s (`metricsPushInterval`, `cpuSampleInterval`, mock SSE, comments)
- [ ] Step 7: frontend type + mock branch ตรงกับ backend DTO
- [ ] Step 8: การ์ดกราฟในแท็บ Detailed (seed + live append + dedupe + empty state)
- [ ] Step 9: `docs/openapi.yaml` + `frontend/public/openapi.yaml` sync, README Feature Status
- [ ] Step 10 (optional): ตรวจ `install.sh` — ยืนยันแล้วว่าไม่ต้องแก้
- [ ] Test: `cd backend && go build ./... && go test ./... && go test -race ./internal/service/...`
- [ ] Test: `cd frontend && yarn build && yarn lint`
- [ ] Test mock (`-mock=true -allow-dev-cors`): กราฟมีข้อมูลตั้งแต่วินาทีแรก, ขยับเองทุก ~5s, badge proto มีค่าและเปลี่ยนตามเวลา
- [ ] Test real device: total ตรงกับ `cat /proc/sys/net/netfilter/nf_conntrack_count` (±1 รอบ sample); `nmap -sT` ทำให้ conntrack พุ่ง → กราฟพุ่งตาม โดย CPU/log ไม่ผิดปกติ
- [ ] Test real device: การ์ดเดิม 3 ใบ (Protocol Breakdown/Top Talkers/Top Rules) ค่าไม่เพี้ยน (ยืนยันไม่ regress phase 2)
- [ ] Test: WSL/host ไม่มี conntrack → empty state + `CapabilityBanner`, ไม่ 500 ไม่ crash, log ไม่ท่วม
- [ ] Test roles: `-disable-edit=true` และ role read-only ดูกราฟได้ครบปกติ
- [ ] `git diff --stat` ไม่มี `backend/internal/kernel/real_firewall.go` และไม่มี route ใหม่ใน `router.go`
