# Dashboard Metrics Stream — เปลี่ยน performance metrics จาก polling เป็น SSE push

> แผนงาน: เปลี่ยนการดึง **performance metrics เร็ว** (CPU/RAM/temp/storage) ของ
> Dashboard StatGrid + badge อุณหภูมิบน site-header จาก interval polling (ยิงซ้ำสองที่)
> มาเป็น **SSE push จาก sampler ที่ backend มีอยู่แล้ว** ผ่าน connection เดียวที่แชร์กัน —
> ลบ double-poll ของ `/api/dashboard/performance` และทำให้ temp badge สดทุกหน้า
>
> เขียนเมื่อ: 2026-07-17 · Reference branch: `feat/dashboard-metrics-stream` · ต่อยอดจาก #41 (log stream)
> **ขอบเขตถูกจำกัดโดยเจ้าของงาน:** stream เฉพาะ metric เร็วเท่านั้น (ดู §0 Out of scope)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- `SystemMetrics` (CPU/RAM/temp/storage) ถูก **push** เข้า Dashboard StatGrid และ
  temp badge บน site-header ทันทีตามจังหวะ sampler ของ backend (~3-5s) — ไม่มี interval
  polling ของ `/api/dashboard/performance` เหลือในทั้งสองจุด
- ใช้ **SSE connection เดียว** แชร์ระหว่าง Dashboard กับ site-header (ผ่าน React context
  ระดับ layout) → ลบสถานการณ์ปัจจุบันที่ poll endpoint เดียวกันซ้ำสองชุด และทำให้ badge
  อุณหภูมิสดในทุกหน้า (site-header mount ทุกหน้า)
- ตัด stream เมื่อ session ถูก revoke/หมดอายุ (ใช้ `SessionAlive` เดิมจาก #41); reconnect
  อัตโนมัติ (EventSource); รอด >60s ไม่หลุด (คง `SetWriteDeadline`)

**Out of scope (ตัดชัด):**
- **traffic history (60s), interfaces (30s), system info (30s)** — ยัง polling ตามเดิม;
  cadence ช้ามาก ได้ประโยชน์จาก stream น้อยแต่ต้อง multiplex event หลายชนิด ไม่คุ้ม
- **`/api/dashboard/stats`** (dashboard cards นับ DHCP/policy) — polling ตามเดิม
- **log stream (#41)** — คนละ stream คนละ semantics (event-driven) ไม่แตะ
- ไม่ลบ endpoint `GET /api/dashboard/performance` เดิม — ยังใช้เป็น mock fallback + จุด
  probe 401 ตอน reconnect (Caution 5)

## 1. Current State (สำรวจโค้ดจริง 2026-07-17)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Backend sampler | มี — `runCPUSampler` ทุก 3s เก็บ CPU usage เข้า cache (background goroutine) | `system_status.go:94`, const `cpuSampleInterval` `:21` |
| Compose DTO | มี — `GetSystemMetrics()` ประกอบ `model.SystemMetrics` จาก cache CPU + live temp/mem/disk (degrade เป็น 0 ถ้าอ่านไม่ได้) | `system_status.go:224` |
| pub/sub metrics | **ไม่มี** — service ยังไม่มี Subscribe/broadcast; มีแค่ getter | `system_status.go` |
| Handler | GET อ่าน cache ตรง ๆ — `s.systemStatus.GetSystemMetrics()` | `handlers.go:348` |
| Route | `GET /api/dashboard/performance` (authRoute) | `router.go:38` |
| Start() wiring | `systemStatusService.Start(monitorCtx)` เรียกแล้วใน main (goroutine ใหม่ launch ในนี้ได้ ไม่ต้องแตะ main เพิ่ม) | `main.go:318` |
| Server struct | มี field `systemStatus *service.SystemStatusService` แล้ว (handler ใหม่ใช้ได้เลย) | `handlers.go:46` |
| Dashboard poll | `usePoll(getPerformanceMetrics, METRICS_INTERVAL=5s)` | `Dashboard.tsx:611`, const `:52` |
| site-header poll | **poll ชุดที่สอง** `getPerformanceMetrics` ทุก 5s เพื่อ temp badge (mount ทุกหน้า) | `site-header.tsx:35-53` |
| Layout mount point | `ShellLayout` เรนเดอร์ `<SiteHeader/>` + `<Outlet/>` — จุดวาง MetricsProvider | `ShellLayout.tsx:24-29` |
| SSE wrapper อ้างอิง | `connectSSELogs` (EventSource + withCredentials + mock sim + onOpen/onClear) ทำ #41 ไว้ | `dashboardService.ts:266` |
| Handler อ้างอิง | `HandleLogStream` (header/flusher/SetWriteDeadline/SessionAlive/heartbeat) | `handlers.go:2345` |
| session peek | `SessionAlive(token)` read-only ไม่เลื่อน expiry (ทำใน #41) | `session.go` |

สรุป: backend มี sampler + composer ครบ ขาดแค่ **pub/sub + handler stream**; frontend ต้อง
เพิ่ม **context แชร์ 1 ตัว + hook** แล้วสลับ 2 จุดที่ poll มาใช้ context; ไม่แตะ
kernel/db/main.go wiring/install.sh (ไม่มี OS access ใหม่ ใช้ `SystemStatsManager` เดิม)

## 2. Technical Approach

**Backend — pub/sub broadcaster ใน SystemStatusService (push snapshot ตาม tick):**
metrics เป็นค่าที่ **sample เป็นรอบ** (ไม่ใช่ event เหมือน log) → broadcaster compose
`GetSystemMetrics()` หนึ่งครั้งต่อ tick แล้ว fan out ให้ทุก subscriber (non-blocking) —
อ่าน /proc รอบเดียวไม่ว่าจะมีกี่ connection:

```go
// system_status.go
const metricsPushInterval = 3 * time.Second // อิงจังหวะ cpuSampleInterval
func (s *SystemStatusService) SubscribeMetrics(buf int) (<-chan model.SystemMetrics, func())
// goroutine ใหม่ใน Start(): ticker → compose GetSystemMetrics() → notify non-blocking (drop ถ้าเต็ม)
```

**Backend — handler ใหม่ `HandleMetricsStream`** (mirror `HandleLogStream`): header SSE +
flusher + `SetWriteDeadline(time.Time{})` + อ่าน token จาก cookie → **push snapshot ทันที
1 ครั้งตอน connect** (กัน UI ว่างช่วง interval แรก) → loop select client-done / metrics-event;
ทุก event **re-check `SessionAlive(token)`** (ไม่เลื่อน expiry) ก่อน write ถ้า session ตาย →
close. ไม่ต้องมี heartbeat แยก เพราะ data ไหลทุก ~3s อยู่แล้ว (ไม่มี idle-close risk)

**Frontend — MetricsProvider (context) ระดับ layout + `useMetrics()`** (แนวเดียวกับ
`HostnameProvider`): เปิด EventSource **ตัวเดียว** ที่ ShellLayout, เก็บ `SystemMetrics`
ล่าสุด, expose ให้ทั้ง Dashboard และ site-header ใช้ร่วม (ตัด double-poll)
- แต่ละ message = full snapshot → **replace state** (ไม่ต้อง dedupe/snapshot-merge แบบ log)
- reconnect: tick ถัดไป (~3s) ส่ง snapshot ใหม่เอง → ไม่ต้อง refetch on-open
- `onerror` → เรียก `getPerformanceMetrics()` หนึ่งครั้ง (ผ่าน 401-redirect hook `config.ts`)

**ทางเลือกที่ตัดทิ้ง:**
1. *per-connection ticker ใน handler (ไม่มี broadcaster)* — ง่ายกว่าแต่ทุก connection อ่าน
   /proc เอง; แม้ N น้อย ก็ไม่สม่ำเสมอ (ต่างเฟส) — broadcaster ให้ค่าชุดเดียวกันทุก client
   และ compose รอบเดียว
2. *broadcast ท้าย `sampleCPU()`* — `sampleCPU` ถือ `cpuMu.Lock()` อยู่ แล้ว
   `GetSystemMetrics()` ก็ `cpuMu.RLock()` → RWMutex ไม่ reentrant = deadlock; ต้องแยก goroutine
3. *multiplex ทุก dashboard data ใน stream เดียว* — เจ้าของงานเลือกตัดออก (ดู §0)
4. *ต่างคนต่างเปิด SSE (Dashboard + site-header)* — เปลือง connection ชน cap 6/host
   (Caution 3) — จึงแชร์ผ่าน context

**Pattern:** broadcaster/Subscribe ตาม `logs/ringbuffer.go` (#41); handler ตาม
`HandleLogStream` (`handlers.go:2345`); context ตาม `HostnameProvider`

## 3. Steps (ชั้นในสุด → นอก)

### Step 1 — pub/sub ใน SystemStatusService
**File:** `backend/internal/service/system_status.go` — เพิ่ม subscriber set (ใต้ mutex ใหม่
หรือ mutex เฉพาะ), `SubscribeMetrics(buf)`, const `metricsPushInterval`, goroutine
`runMetricsBroadcaster(ctx)` (compose `GetSystemMetrics()` → notify non-blocking) และ
launch ใน `Start()` ข้าง sampler เดิม (`:89-90`)
- test: `system_status_test.go` — subscribe รับ snapshot, slow subscriber ไม่ block, cancel ไม่ leak

### Step 2 — handler stream
**File:** `backend/internal/api/handlers.go` (ใกล้ `HandleLogStream` `:2345`) —
`HandleMetricsStream`: mirror โครง header/flusher/SetWriteDeadline/อ่าน token; push snapshot
แรกทันที; loop subscribe + re-check `SessionAlive` ต่อ event; `data: {SystemMetrics json}\n\n`

### Step 3 — route
**File:** `backend/internal/api/router.go:38` (ถัดจาก performance) —
`authRoute("GET /api/dashboard/performance/stream", s.HandleMetricsStream)`

### Step 4 — OpenAPI (สองไฟล์)
**File:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` — เพิ่ม path stream:
event format (default message = `SystemMetrics`), push interval, ตัดเมื่อ session ตาย

### Step 5 — frontend wrapper
**File:** `frontend/src/services/dashboardService.ts` — `connectSSEMetrics({ onMetrics,
onOpen?, onError? })` mirror `connectSSELogs`; mock-mode: `setInterval` เรียก mock generator
ของ `getPerformanceMetrics` (branch mock เดิม `:155-183`)

### Step 6 — MetricsProvider + useMetrics (ใหม่)
**File:** `frontend/src/components/MetricsProvider.tsx` (+ `hooks/useMetrics` หรือ context
ไฟล์เดียว) — เปิด EventSource ตัวเดียว, เก็บ `PerformanceMetrics | null`, expose ผ่าน context

### Step 7 — mount ที่ layout
**File:** `frontend/src/components/layout/ShellLayout.tsx:24` — ครอบ `<SiteHeader/>` + `<main>`
ด้วย `<MetricsProvider>`

### Step 8 — สลับ Dashboard + site-header มาใช้ context
**File:** `frontend/src/pages/Dashboard.tsx:611` — `perf` มาจาก `useMetrics()` แทน
`usePoll(getPerformanceMetrics,…)`; ลบ `METRICS_INTERVAL` (`:52`)
**File:** `frontend/src/components/site-header.tsx:35-53` — temp มาจาก `useMetrics()`;
ลบ `setInterval` + local fetch

> **ไม่ต้องทำ:** kernel layer (ใช้ `SystemStatsManager` เดิม ทั้ง real/mock), db/migration,
> `main.go` (Start ถูกเรียกแล้ว `:318`; NewServer มี systemStatus แล้ว), `install.sh`
> (ไม่มี privilege ใหม่), netlink monitor, backup, boot-apply — ไม่มี state ใหม่

## 4. Related API

| Method | Path | Role | หมายเหตุ |
|---|---|---|---|
| GET | `/api/dashboard/performance/stream` | authRoute (ใหม่) | GET อย่างเดียว `-disable-edit` ไม่เกี่ยว; ไม่มี consumer เดิม |
| GET | `/api/dashboard/performance` | authRoute (เดิม) | คงไว้ — mock fallback + จุด probe 401 ตอน reconnect |

## 5. Cautions

1. **metric เป็นค่า sample ไม่ใช่ event** — ห้ามคาดหวังพฤติกรรม dedupe/snapshot-merge แบบ
   log; แต่ละ push คือ full snapshot → *กัน:* frontend **replace state** ไม่ใช่ append; ไม่
   ต้อง refetch on-open (tick ถัดไปมาเอง)
2. **RWMutex ไม่ reentrant** — ถ้า broadcast ใน `sampleCPU()` ที่ถือ `cpuMu.Lock()` แล้วเรียก
   `GetSystemMetrics()` (ขอ `cpuMu.RLock()`) จะ deadlock. *กัน:* broadcaster เป็น goroutine
   แยก (Step 1) ไม่ถือ lock ตอน compose
3. **เพดาน 6 connection/host (HTTP/1.1)** — Dashboard tab จะถือ log stream (#41) +
   metrics stream = 2 persistent อยู่แล้ว; ถ้า Dashboard กับ site-header ต่าง EventSource กัน
   จะเป็น 3. *กัน:* MetricsProvider เปิด **connection เดียว** ที่ layout แชร์ทั้งคู่ (Step 6-7)
4. **subscriber ห้าม block broadcaster** — ถ้า send เข้า channel แบบ blocking แล้ว SSE client
   ค้าง จะ stall goroutine broadcaster. *กัน:* non-blocking send + drop เมื่อเต็ม (client ได้
   snapshot ครบใน tick ถัดไปอยู่ดี) + test slow-subscriber (ตามข้อกำหนดเดียวกับ #41)
5. **EventSource ไม่บอก HTTP status — 401 ตอน reconnect มองไม่เห็น** — MetricsProvider mount
   ทุกหน้า ถ้า logout/หมดอายุแล้ว reconnect เงียบจะไม่ bounce. *กัน:* `onerror` เรียก
   `getPerformanceMetrics()` หนึ่งครั้ง → ผ่าน 401-redirect hook (`config.ts:38-45`) พา /login
6. **WriteTimeout regression (#33)** — ต้องคง `SetWriteDeadline(time.Time{})` ใน handler ใหม่
   ไม่งั้น stream หลุดทุก ~60s เงียบ ๆ (reconnect กลบอาการ). ทดสอบเปิดยาว >60s
7. **session revoke ต้องตัด stream** — data ไหลทุก ~3s แต่ต้อง re-check `SessionAlive` ต่อ
   event (ไม่ใช่แค่ตอน connect) ไม่งั้น stream ที่เปิดค้างส่ง metric ต่อหลัง revoke. *กัน:*
   Step 2 เช็คก่อน write ทุกครั้ง
8. **mock สองชั้นต้องไม่พัง** — backend `-mock=true`: `GetSystemMetrics()` อ่านจาก
   `MockSystemStats` → stream ใช้ได้จริง; frontend `IS_MOCK_MODE`: `connectSSEMetrics`
   simulate ด้วย interval — hook ต้องทำงานทั้งสอง path (จุดทดสอบใน DoD)
9. **badge อุณหภูมิต้องคง `tempDetail.available` logic** — site-header ซ่อน badge เมื่อ
   `available=false` (host ไม่มี thermal zone). *กัน:* context เก็บทั้ง object; site-header
   อ่าน `available/celsius` เหมือนเดิม ไม่ใช่แค่ตัวเลข

## 6. Summary Checklist (Definition of Done)

- [x] `service/system_status.go` — `SubscribeMetrics` + `runMetricsBroadcaster` (non-blocking)
      + launch ใน `Start()` + `system_status_test.go` (รับ snapshot / slow ไม่ block / cancel ไม่ leak)
- [x] `api/handlers.go` — `HandleMetricsStream` (push snapshot แรก + subscribe + re-check
      SessionAlive ต่อ event + คง SetWriteDeadline)
- [x] `api/router.go` — `authRoute GET /api/dashboard/performance/stream`
- [x] `go build ./...` + `go test ./...` ผ่าน
- [x] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — เพิ่ม stream (sync)
- [x] `dashboardService.ts` — `connectSSEMetrics` (onMetrics/onOpen/onError + mock path)
- [x] `MetricsProvider.tsx` + `useMetrics` (ใหม่) — connection เดียว, expose `PerformanceMetrics`
- [x] `ShellLayout.tsx` — ครอบด้วย MetricsProvider
- [x] `Dashboard.tsx` — StatGrid ใช้ `useMetrics()`; ลบ `METRICS_INTERVAL`
- [x] `site-header.tsx` — temp badge ใช้ `useMetrics()`; ลบ `setInterval` + local fetch
- [x] `yarn build` + `yarn lint` ผ่าน
- [x] ทดสอบ mock (workstation, curl กับ backend `-mock=true`): stream ส่ง `event: connected`
      + snapshot แรกทันที แล้ว push `PerformanceMetrics` เต็มก้อนทุก ~3s (ค่า cpu/temp/mem ขยับจริง);
      revoke session (logout กลาง stream) → ถูกตัดภายใน ~1 push interval (วัดจริง 3s); stream รอด
      >60s (วัดจริง 65s, 23 frames — Caution 6 ผ่าน, SetWriteDeadline ไม่ regress)
      > หมายเหตุ: การรีวิว browser จริง (Dashboard + สลับหน้า เห็น temp badge สดจาก connection
      > เดียวใน network tab, bounce /login เมื่อ 401 ตอน reconnect) ยังควรยืนยันด้วยตาบน UI ก่อน merge
- [x] ยืนยันไม่มี poll `/dashboard/performance` เหลือใน Dashboard + site-header (double-fetch = 0)
- [x] เสร็จแล้วย้ายแผนไป `docs/ref/complete/` + สรุปลง `docs/ref/*` ถ้ามีเนื้อหา design ควรเก็บ
