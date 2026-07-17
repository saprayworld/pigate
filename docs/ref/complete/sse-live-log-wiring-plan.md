# SSE Live-Log Wiring — ต่อ live-log stream เข้า frontend จริง (เลิก polling หน้า log)

> แผนงาน: ต่อ SSE stream (`/api/dashboard/logs/stream`) ที่ตอนนี้เป็น **dead code ทั้งสองฝั่ง**
> เข้ากับหน้า Dashboard Recent Logs + Forward Traffic แทน interval polling —
> พร้อม**แก้ backend stream ที่ออกแบบผิด** (ส่ง log ล่าสุดตัวเดียวซ้ำทุก 3 วิ — ทั้งซ้ำทั้งหลุด)
> ให้เป็น push จริงผ่าน pub/sub บน ring buffer
>
> เขียนเมื่อ: 2026-07-17 · Reference branch: `feat/sse-live-log-wiring` · Issue: #41
> **Blocker เดิมปลดแล้ว:** #33 (WriteTimeout ฆ่า SSE) ปิดไปแล้ว 2026-07-13 —
> `HandleLogStream` เคลียร์ write deadline ต่อ connection แล้ว (`handlers.go:~2367`)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- Dashboard (Recent Logs/Alerts) และ Forward Traffic รับ log entry ใหม่แบบ **push ทันที**
  ผ่าน SSE — ไม่มี interval polling ของ log สองหน้านี้เหลืออยู่
- Backend stream ส่ง **เฉพาะ entry ใหม่** (ไม่ซ้ำ ไม่หลุด), มี heartbeat, และ**ตัด stream
  เมื่อ session ถูก revoke/หมดอายุ** (ปิด gap ที่ issue #41 ระบุ)
- reconnect อัตโนมัติ (EventSource) + refetch snapshot เมื่อต่อใหม่ → ไม่มี entry หาย/ซ้ำใน UI
- กด Clear จาก browser หนึ่ง → ทุก browser ที่เปิด stream อยู่เห็นรายการว่างตาม

**Out of scope (ตัดชัด):**
- **EventLogs.tsx** (audit log จาก SQLite `/api/logs/events`) — คนละ buffer คนละ semantics,
  polling 10s เดิมเหมาะแล้ว (batch writer เขียนเป็นรอบอยู่แล้ว)
- SSE สำหรับ dashboard widget อื่น (perf/traffic history/interfaces) — ยัง polling ตามเดิม
- Last-Event-ID resume (ส่ง entry ที่พลาดช่วงหลุดแบบ exact) — ใช้ snapshot-refetch แทน
- polling fallback ถาวรเมื่อ SSE พัง (ดู rejected alternative 3)

## 1. Current State (สำรวจโค้ดจริง 2026-07-17)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Backend endpoint | มี — `GET /api/dashboard/logs/stream` (authRoute) | `router.go:42`, `handlers.go:2345` |
| พฤติกรรม stream ปัจจุบัน | **ผิด** — ticker 3s ส่ง `logsList[0]` (ตัวล่าสุดตัวเดียว) ซ้ำทุกรอบ; entry ที่มาถี่กว่า 3s หลุด, entry เดิมส่งซ้ำไม่รู้จบ | `handlers.go:~2378-2392` |
| WriteTimeout kill (#33) | แก้แล้ว — `SetWriteDeadline(time.Time{})` ต่อ connection | `handlers.go:~2367` |
| Ring buffer | ไม่มี pub/sub — มีแค่ `Add/GetAll/Clear` (mutex, copy ทั้งก้อน) | `logs/ringbuffer.go` |
| ผู้เขียน buffer | NFLOG watcher callback ใน main.go — stamp RFC3339 UTC + `uuid.NewString()` ต่อ entry (**มี unique ID ใช้ dedupe ได้**); mock synthesizes ให้เอง | `main.go:292-301`, `mock.go:580` |
| Session validation | `ValidateSession(token)` **เลื่อน sliding idle expiry ทุกครั้งที่เรียก** — ใช้ re-check เป็นรอบไม่ได้ตรง ๆ (ดู Caution 2) | `session.go:120`, `middleware.go:74` |
| Frontend wrapper | มี `connectSSELogs()` ครบ (EventSource + withCredentials + mock-mode simulation) แต่ **ไม่มีใครเรียก** | `dashboardService.ts:266` |
| Dashboard Recent Logs | polling — `usePoll(getRecentLogs, 10_000, refreshKey)`; ใช้ผ่าน `logsToAlerts` ใน AlertsCard; ปุ่ม Refresh bump `refreshKey` | `Dashboard.tsx:613,626` |
| Forward Traffic | polling 5s + filter ฝั่ง server (`action`,`q`,`limit`) + ปุ่ม Pause | `ForwardTraffic.tsx:123`, `handlers.go:385` |
| 401 → /login hook | fetch wrapper redirect เมื่อ 401 — **ยกเว้น URL ที่มี `/auth/session`**; EventSource ไม่ผ่าน hook นี้ (ไม่ใช่ fetch) | `config.ts:38-45` |
| CORS dev mode | CORSMiddleware echo origin + credentials เมื่อ `-allow-dev-cors` — รองรับ credentialed EventSource แล้ว | `middleware.go:44-46` |
| OpenAPI | มี `/dashboard/logs/stream` แล้ว — ต้องอัปเดต description ตาม event format ใหม่ | `docs/openapi.yaml:333` |

สรุป: โครงมีครบสองฝั่งแต่ไม่เคยถูกใช้จริง; งานหลักคือ **ทำ stream ให้ถูกก่อน** (ring buffer
pub/sub + rewrite handler) แล้วค่อยต่อ frontend (hook ใหม่ 1 ตัว ใช้ร่วมสองหน้า); ไม่แตะ
kernel/db/main.go/install.sh เลย

## 2. Technical Approach

**Backend — pub/sub บน RingBuffer (push จริง แทน server-side polling):**

```go
// logs/ringbuffer.go
type LogEvent struct {
    Kind  string            // "log" | "clear"
    Entry model.FirewallLog // เฉพาะ Kind=="log"
}
func (r *RingBuffer) Subscribe(buf int) (ch <-chan LogEvent, cancel func())
// Add()/Clear() แจ้งทุก subscriber แบบ non-blocking:
//   select { case sub <- ev: default: /* drop — ห้าม block NFLOG loop */ }
```

**Backend — rewrite `HandleLogStream`:** subscribe → loop select 3 ทาง
(client done / event / heartbeat ticker ~25s):
- event Kind=="log" → `data: {FirewallLog json}\n\n` (default message event —
  `es.onmessage` เดิมใช้ได้ ไม่ต้องแก้ parser)
- event Kind=="clear" → `event: clear\ndata: {}\n\n`
- heartbeat → ส่ง comment `: ping\n\n` (กัน idle-close + ตรวจ peer ตาย) และ
  **re-check session แบบไม่เลื่อน expiry** (ฟังก์ชันใหม่ `SessionAlive(token)` — Caution 2)
  ถ้า session หาย → return ปิด stream
- คงบรรทัด `SetWriteDeadline(time.Time{})` และ `event: connected` เดิมไว้

**Frontend — hook ใหม่ `useLiveLogs` ใช้ร่วมสองหน้า** (ตามแนว `usePowerControl` ที่
แชร์ logic จุดเดียว): รับ `{ fetchSnapshot, refreshKey, paused }` → คืน `logs[]` + สถานะ
- mount / SSE `open` (รวม auto-reconnect) / `refreshKey` เปลี่ยน → fetch snapshot
  (endpoint list เดิม) แล้ว merge กับ entry ที่มาจาก SSE โดย **dedupe ด้วย `log.id`** (uuid)
- `onmessage` → prepend (newest-first) + cap ที่ 500 (= ความจุ buffer, `main.go:74`)
- `clear` event → ล้าง state
- `onerror` → EventSource reconnect เองอยู่แล้ว; hook เรียก `fetchSnapshot()` หนึ่งครั้ง —
  ถ้าสาเหตุคือ 401 fetch ตัวนี้จะพาเข้า redirect hook ใน `config.ts` ให้เอง (bounce /login)
- `paused` (Forward Traffic) → ปิด EventSource; resume → ต่อใหม่ (open → refetch เอง)

**ทางเลือกที่ตัดทิ้ง:**
1. *คง server-side ticker แต่ส่ง delta ด้วย sequence cursor* — ยังมี latency 3s และต้องแก้
   ring buffer พอ ๆ กับทำ pub/sub; pub/sub ให้ push จริงในราคาเท่ากัน
2. *ใช้ `ValidateSession` re-check ใน heartbeat* — มันเลื่อน sliding expiry → SSE ที่เปิดทิ้งไว้
   จะต่ออายุ session ไม่รู้จบ ทำ idle-logout ฝั่ง server เป็นหมัน (Caution 2)
3. *เก็บ polling ไว้เป็น fallback ถาวรคู่ SSE* — เสี่ยง double-fetch/เขียน state ชนกัน และ
   ซับซ้อนเกินประโยชน์; EventSource auto-reconnect + snapshot-on-open ครอบเคส backend
   restart อยู่แล้ว (ตัดตามที่ issue เปิดทางให้ตัดสินใจ)
4. *WebSocket แทน SSE* — ต้องเพิ่ม dep + auth ซับซ้อนขึ้น; ทิศทางเดียว server→client
   SSE พอดีงานและของเดิมมีอยู่แล้ว

**Pattern:** handler ตาม `HandleLogStream` เดิม (โครง header/flusher); hook ตาม
`usePoll` (`Dashboard.tsx:60`) + `usePowerControl` (shared hook); subscriber-drop
semantics ตามข้อกำหนด `TrafficLogManager` (`interfaces.go:22-28` — ห้าม stall ผู้ผลิต)

## 3. Steps (ชั้นในสุด → นอก)

### Step 1 — ring buffer pub/sub
**File:** `backend/internal/logs/ringbuffer.go` — เพิ่ม `LogEvent`, `Subscribe(buf)`,
notify ใน `Add()`/`Clear()` แบบ non-blocking (ตาม §2); subscriber list ใต้ mutex เดิม
- test: `ringbuffer_test.go` (ใหม่) — subscribe รับครบ, slow subscriber ไม่ block Add,
  cancel แล้วไม่ leak, Clear ส่ง event

### Step 2 — session peek แบบไม่เลื่อนอายุ
**File:** `backend/internal/api/session.go` — `SessionAlive(token string) bool`
(RLock อ่านอย่างเดียว: มี entry และยังไม่เลย absolute/idle deadline — **ห้าม** แตะ
`expiresAt`) + test ใน `session_test.go` ว่าเรียกแล้ว expiry ไม่ขยับ

### Step 3 — rewrite HandleLogStream
**File:** `backend/internal/api/handlers.go:~2345` — ตาม §2; อ่าน token จาก cookie
ตอน connect (`r.Cookie(SessionKey)`); ลบ logic `logsList[0]` เดิมทิ้งทั้งก้อน

### Step 4 — OpenAPI (สองไฟล์)
**File:** `docs/openapi.yaml:~333` + `frontend/public/openapi.yaml` — อัปเดต description:
event format (`connected`/default message = FirewallLog/`clear`), heartbeat comment,
พฤติกรรมตัด stream เมื่อ session ตาย

### Step 5 — frontend wrapper
**File:** `frontend/src/services/dashboardService.ts:266` — `connectSSELogs` เพิ่มพารามิเตอร์
`onClear?`, `onOpen?` (ผูก `es.addEventListener("clear",…)`, `es.onopen`); mock-mode
simulation เดิมคงไว้ (เพิ่ม no-op ให้ callback ใหม่)

### Step 6 — hook `useLiveLogs` (ใหม่)
**File:** `frontend/src/hooks/useLiveLogs.ts` — ตาม §2; generic พอให้ Dashboard
(getRecentLogs) และ ForwardTraffic (getTrafficLogs + filter) ใช้ร่วม

### Step 7 — Dashboard
**File:** `frontend/src/pages/Dashboard.tsx:613` — แทน `usePoll(getRecentLogs,…)` ด้วย
`useLiveLogs({ fetchSnapshot: dashboardService.getRecentLogs, refreshKey })`;
ลบ `LOGS_INTERVAL`; widget อื่นคง `usePoll` เดิม

### Step 8 — Forward Traffic
**File:** `frontend/src/pages/ForwardTraffic.tsx:~120` — แทน `setInterval` ด้วย
`useLiveLogs({ fetchSnapshot: () => trafficLogService.getTrafficLogs({action,q,limit}),
refreshKey: filterKey, paused: isPaused })` + **filter entry จาก SSE ฝั่ง client** ให้ตรง
กับ logic server (`handlers.go:385-418`: action เท่ากัน + substring ใน
src/dest/port/proto/inIface/outIface/reason, case-insensitive); filter เปลี่ยน → refetch

> **ไม่ต้องทำ:** kernel layer (NFLOG watcher เดิมใช้ได้ ทั้ง real/mock), db, `main.go`,
> `install.sh`, backup (ไม่มี config ใหม่), netlink monitor, boot-apply — ไม่มี state ใหม่ทั้งสิ้น

## 4. Related API

| Method | Path | Role | หมายเหตุ |
|---|---|---|---|
| GET | `/api/dashboard/logs/stream` | authRoute (route เดิม) | เปลี่ยน wire format ภายใน (ไม่มี consumer เดิม — เปลี่ยนได้อิสระ); GET อย่างเดียว `-disable-edit` ไม่เกี่ยว |
| GET | `/api/dashboard/logs`, `/api/logs/traffic` | authRoute (เดิม) | กลายเป็น snapshot fetch ตอน mount/reconnect/refresh เท่านั้น |

## 5. Cautions

1. **stream เดิมออกแบบผิด — ห้ามต่อ frontend เข้า format เดิม** — ถ้า wire UI เข้า
   ticker-3s เดิม จะเห็น log ตัวเดียวเด้งซ้ำและ entry ที่มาถี่หลุดหาย. *กัน:* ทำ Step 1-3
   ให้เสร็จก่อนแตะ frontend; ห้ามสลับลำดับ
2. **`ValidateSession` เลื่อน sliding expiry** (`session.go:120`) — ถ้าใช้ re-check ใน
   heartbeat, SSE ที่เปิดค้างจะกลายเป็นเครื่องต่ออายุ session อัตโนมัติ → server-side idle
   TTL ไร้ผลทั้งระบบ. *กัน:* Step 2 เพิ่ม `SessionAlive` แบบ read-only + test ยืนยันว่า
   expiry ไม่ขยับ
3. **subscriber ห้าม block ผู้ผลิต** — `Add()` ถูกเรียกจาก NFLOG callback; ถ้า send เข้า
   channel แบบ blocking แล้ว SSE client ช้า/ค้าง netlink read loop จะ stall ทั้งระบบ log
   (ข้อกำหนดเดียวกับ `TrafficLogManager`, `interfaces.go:22-28`). *กัน:* non-blocking send
   + drop เมื่อเต็ม (UI ได้ของครบอยู่ดีจาก snapshot รอบถัดไป) + test slow-subscriber
4. **อย่าทำ WriteTimeout regression** — #33 แก้ด้วย `SetWriteDeadline(time.Time{})` ใน
   handler; ตอน rewrite ต้องคงบรรทัดนี้ ไม่งั้น stream หลุดทุก ~60s เงียบ ๆ (EventSource
   reconnect กลบอาการ → โหลด snapshot ถี่โดยไม่รู้ตัว). ทดสอบจริงต้องเปิดยาว >60s
5. **EventSource ไม่บอก HTTP status** — 401 ตอน reconnect มองไม่เห็นจาก `onerror`
   (browser ไม่ expose). *กัน:* hook fetch snapshot ใน `onerror` — fetch นี้ผ่าน 401
   redirect hook (`config.ts:38-45`) พา /login ให้; **ห้าม**ใช้ `/auth/session` เป็นตัว probe
   (ถูก exclude จาก hook — `config.ts:40`)
6. **reconnect ทำ entry ซ้ำ/หายใน UI** — ช่วง SSE หลุด entry ที่พลาดไม่ถูกส่งย้อน; ถ้า
   append อย่างเดียวจะได้ list มีรู, ถ้า refetch อย่างเดียวอาจชนกับ event ที่เพิ่งมา.
   *กัน:* ทุก `onopen` refetch snapshot แล้ว merge dedupe ด้วย `id` (uuid มีอยู่แล้ว —
   `main.go:296`); test hook เคสนี้
7. **ปุ่ม Refresh (Dashboard) และ filter/Pause (ForwardTraffic) ต้องยังทำงาน** — logs
   เลิกใช้ `usePoll` แล้ว `refreshKey` จะไม่มีผลกับ logs ถ้าไม่ส่งเข้า hook. *กัน:* hook รับ
   `refreshKey`/`paused` ตาม Step 6-8; Pause = ปิด ES ไม่ใช่แค่หยุด render (ประหยัด
   connection และตรง label "paused")
8. **client-side filter ต้อง mirror server** (ForwardTraffic) — ถ้า logic เพี้ยน (เช่นลืม
   field `reason`) แถวจาก SSE จะโผล่ทั้งที่ filter ไว้. *กัน:* ก๊อป semantics จาก
   `handlers.go:385-418` มาเป็น helper เดียว + unit-ระดับ manual test ใน DoD
9. **จำนวน SSE connection ต่อ browser** — HTTP/1.1 จำกัด ~6 connection/host; เปิด
   Dashboard + ForwardTraffic คนละ tab หลาย ๆ tab อาจชนเพดาน (แต่ละ tab เปิด stream
   ของตัวเอง). v1 ยอมรับได้ (การใช้งานจริงคือ admin คนเดียว) — จดไว้; ถ้าเจอปัญหา
   ค่อยย้ายไป SharedWorker/single-stream เฟสหลัง
10. **mock mode สองชั้นต้องไม่พัง** — backend `-mock=true`: `MockTrafficLog`
    (`mock.go:580`) ยัง feed buffer → SSE ใช้ได้จริง; frontend `IS_MOCK_MODE`:
    `connectSSELogs` simulate ด้วย interval เดิม — hook ต้องทำงานทั้งสอง path
    (จุดทดสอบใน DoD)

## 6. Summary Checklist (Definition of Done)

- [x] `logs/ringbuffer.go` — `LogEvent` + `Subscribe` + notify ใน Add/Clear (non-blocking)
      + `ringbuffer_test.go` (รับครบ / slow-subscriber ไม่ block / cancel ไม่ leak / Clear event)
- [x] `api/session.go` — `SessionAlive` (ไม่เลื่อน expiry) + test ใน `session_test.go`
- [x] `api/handlers.go` — rewrite `HandleLogStream` (push จาก Subscribe + heartbeat +
      ตัดเมื่อ session ตาย + คง SetWriteDeadline) — ลบ logic `logsList[0]` เดิม
- [x] `go build ./...` + `go test ./...` ผ่าน
- [x] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — อัปเดต `/dashboard/logs/stream` (sync)
- [x] `dashboardService.ts` — `connectSSELogs` รองรับ `onClear`/`onOpen` (mock path ยังทำงาน)
- [x] `frontend/src/hooks/useLiveLogs.ts` (ใหม่) — snapshot-on-open + dedupe-by-id +
      clear event + paused + refreshKey
- [x] `Dashboard.tsx` — Recent Logs ใช้ `useLiveLogs`; ปุ่ม Refresh ยัง refresh logs ได้
- [x] `ForwardTraffic.tsx` — ใช้ `useLiveLogs` + client-side filter ตรง server semantics;
      Pause/Resume + เปลี่ยน filter ทำงานถูก
- [x] `yarn build` + `yarn lint` ผ่าน
- [x] ทดสอบ mock (workstation, curl กับ backend `-mock=true`): stream ส่ง `event: connected`
      แล้ว push แต่ละ entry สด (uuid ไม่ซ้ำ); POST `/dashboard/logs/clear` จาก client อื่น →
      stream ที่เปิดอยู่เห็น `event: clear` (broadcast ทุก subscriber)
- [x] ทดสอบพฤติกรรม runtime: stream อยู่รอด >60s (วัดจริง 65s ยังไม่หลุด — Caution 4 ผ่าน,
      SetWriteDeadline ไม่ regress); logout กลาง stream → ถูกตัดภายใน ~1 heartbeat (วัดจริง 25s)
      > หมายเหตุ: การทดสอบ browser จริง (สองแท็บเห็น clear ตามกัน, reconnect เมื่อปิด backend,
      > bounce /login) ยังควรรีวิวด้วยตาบน UI จริงอีกครั้งก่อน merge
- [x] ยืนยันไม่มี interval polling ของ log เหลือในสองหน้า (double-fetch = 0)
- [x] อัปเดต issue #41 (unblocked แล้ว + ลิงก์แผน) และพิจารณาเอา label `wontfix` ออก
- [x] เสร็จแล้วย้ายแผนไป `docs/ref/complete/`
