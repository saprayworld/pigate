# Wi-Fi Status Stream — push สถานะการเชื่อมต่อ Wi-Fi แบบ real-time ต่อ interface

> Work plan สำหรับฟีเจอร์: เปลี่ยนหน้า Interfaces จากการ fetch `wifi-status` ครั้งเดียว
> มาเป็น SSE stream ต่อ interface ที่ push สถานะ wpa แบบละเอียด
> (SCANNING → ASSOCIATING → 4WAY_HANDSHAKE → COMPLETED) ให้เห็น "กำลังเชื่อมต่อ" ทีละสเต็ป
>
> เขียนเมื่อ: 2026-07-22 · Reference branch: `main` (งานจริงทำบน `feat/wifi-status-stream`)
> README Feature Status: Interfaces = Completed (คงเดิม; นี่คือ enhancement ของ live status)

## 0. Goal and Scope

**Goal (สิ่งที่ผู้ใช้เห็น):** เปิดหน้า Interfaces แล้วการ์ด wireless interface แสดง
สถานะการเชื่อมต่อ Wi-Fi ที่อัปเดตเองแบบ real-time — โดยเฉพาะช่วง "กำลังเชื่อมต่อ"
ต้องเห็น transition เปลี่ยนไปเรื่อยๆ (SCANNING → ASSOCIATING → 4WAY_HANDSHAKE →
COMPLETED) รวมถึง ssid/freq/keyMgmt/wifiGen โดยไม่ต้อง refresh เอง

**เงื่อนไขทางเทคนิค:** ใช้ SSE endpoint ใหม่ **ต่อ interface**
`GET /api/interfaces/{id}/wifi-status/stream` เลียนแบบ pattern ของ
`/api/dashboard/performance/stream`; ทำงานได้ทั้ง real และ mock; ไม่แตะ SD card
(สถานะเป็น runtime อ่านสดจาก wpa_supplicant ไม่ persist ลง SQLite)

**Out of scope (จงใจตัดออก):**
- ไม่ทำ stream รวมทุก interface ในเส้นเดียว (ผู้ใช้เลือก "แยกต่อ interface")
- ไม่แตะ `NetEventBus` และไม่เพิ่ม `Unsubscribe` ใน v1 (เหตุผล §2) — instant
  push ตอน link up/down จาก netlink เป็น future enhancement
- ไม่ทำ stream ให้ ethernet/vlan (สถานะ Wi-Fi ไม่เกี่ยว) — endpoint คืน 400
  ถ้า interface ไม่ใช่ wireless เหมือน `wifi-status` เดิม
- ไม่ทำ signal strength (dBm/RSSI) ต่อเนื่อง — `WifiConnectionStatus` ปัจจุบัน
  ไม่มีฟิลด์นี้; เพิ่มทีหลังได้ถ้าต้องการ

## 1. Current State (สำรวจโค้ด ณ 2026-07-22)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| SSE pattern (handler) | ✅ มีครบ ใช้เป็นแม่แบบได้ | `api/handlers.go:2540` `HandleMetricsStream` (clear WriteTimeout, `SessionAlive` re-check, snapshot แรก, heartbeat) |
| แหล่งข้อมูล wpa status | ✅ มี ให้ wpa_state ละเอียด | `kernel/real_network.go:509` `GetWifiStatus` → wpa `STATUS` (wpa_state/ssid/bssid/freq/key_mgmt/wifi_generation) |
| model | ✅ มี | `model/types.go:222` `WifiConnectionStatus` |
| mock kernel | ✅ มี (static COMPLETED) | `kernel/mock.go:136` `MockNetwork.GetWifiStatus` |
| handler เดิม (one-shot) | ✅ มี ใช้ guard เดิมซ้ำได้ | `api/handlers.go:907` `HandleGetWifiStatus` (id→iface ผ่าน `GetDataLayerInterfaceByID`, `rejectIfOffline`, เช็ค `Type=="wireless"`) |
| route | ⚠️ มีแต่ one-shot | `api/router.go:65` `GET /api/interfaces/{id}/wifi-status` — ยังไม่มี `/stream` |
| frontend service | ⚠️ one-shot fetch | `services/interfaceService.ts:188` `getWifiStatus` (มี mock branch แล้ว) |
| frontend page | ⚠️ fetch ครั้งเดียว | `pages/Interfaces.tsx:500` useEffect ยิง `getWifiStatus` ครั้งเดียวตอน `interfaces` เปลี่ยน → ไม่เห็น transition เลย |
| EventSource template (FE) | ✅ มี ใช้เป็นแม่แบบได้ | `services/dashboardService.ts:344` `connectSSEMetrics` (มี mock branch แบบ interval ด้วย) |
| **NetEventBus `Unsubscribe`** | ❌ **ไม่มี** | `service/event_bus.go` มีแค่ `Subscribe` (append ถาวร ตอน startup) — **ห้าม** subscribe/unsubscribe ต่อ connection จะ leak |

**สรุป:** infra ครบเกือบหมด งานจริงคือเขียน **1 handler ใหม่ + 1 route + แก้ frontend
2 ไฟล์** ไม่ต้องแตะ kernel/service/db/main.go/install.sh เลย

## 2. Technical Approach

**กลไก:** SSE handler ใหม่ `HandleWifiStatusStream` ที่ทำ **poll loop ต่อ connection**
— บน goroutine ของ request นั้นเอง ตั้ง `time.Ticker` เรียก `s.network.GetWifiStatus(iface.Name)`
แล้ว push เฉพาะเมื่อ status **เปลี่ยนจากรอบก่อน** (dedupe) พร้อม snapshot แรกทันทีตอน connect

```go
// adaptive interval: เร็วตอนกำลังเชื่อม, ช้าตอนนิ่ง
fast, slow := 1*time.Second, 4*time.Second
// transitional = ยังไม่ COMPLETED/DISCONNECTED/INACTIVE/SCANNING-idle → ใช้ fast
```

**ทำไมเลือก poll-only (ไม่ใช้ NetEventBus):**
1. wpa_state ระดับละเอียด (ASSOCIATING/4WAY_HANDSHAKE) **ไม่ไหลผ่าน Netlink** —
   เป็น internal state ของ wpa_supplicant เท่านั้น ยังไงก็ต้อง poll `STATUS`
2. `NetEventBus` **ไม่มี `Unsubscribe`** (`event_bus.go`) — subscriber ถูก append
   ถาวรตอน startup; ถ้าให้ SSE แต่ละเส้น subscribe จะ leak subscriber ทุกครั้งที่
   เปิด/ปิดหน้า → memory + งาน publish โตเรื่อยๆ
3. poll ต่อ connection = ทำงานเฉพาะตอนมีคนดู (หน้า Interfaces เปิดอยู่) ไม่กิน CPU/
   wpa socket ตอนไม่มีใครดู

**Alternatives ที่พิจารณาแล้วตัดทิ้ง:**
- *(ตัด) central broadcaster ใน service ใหม่* แบบ `system_status.go:139` `runMetricsBroadcaster`:
  จะต้อง poll **ทุก** wireless interface ทุก tick แม้ไม่มีใครดู interface นั้น +
  ต้อง wire service เข้า `main.go`/`NewServer` เพิ่ม — เกินจำเป็นเพราะ stream เป็น
  per-interface และผู้ดูพร้อมกันมีน้อย (แอดมิน 1–2 คน)
- *(ตัด v1) เพิ่ม `Unsubscribe`+cancel ใน NetEventBus แล้ว hybrid poll+event*: ดีต่อ
  latency ตอน link down แต่แตะ shared bus ที่ subscriber self-healing หลายตัวพึ่งอยู่
  (`main.go:199–277`) — ความเสี่ยง/ขอบเขตไม่คุ้มกับ v1; แยกเป็น enhancement ทีหลัง

**Template ที่ยึด:** handler ตาม `HandleMetricsStream` (`api/handlers.go:2540`) แบบ
เป๊ะๆ (headers, `SetWriteDeadline(zero)`, token+`SessionAlive`, `event: connected`,
`json.Marshal` → `data: …\n\n`); guard id→iface ตาม `HandleGetWifiStatus`
(`api/handlers.go:907`); frontend ตาม `connectSSEMetrics` (`dashboardService.ts:344`)

## 3. Steps (เรียงจาก inner → outer)

> ไม่ต้องทำ: ❌ kernel interface/real/mock (มี `GetWifiStatus` ครบแล้ว) ·
> ❌ service ใหม่ · ❌ `main.go` wiring · ❌ db migration · ❌ `install.sh`/Polkit
> (ไม่ต้องการสิทธิ์เพิ่ม ใช้ control socket เดิมที่ `GetWifiStatus` ใช้อยู่)

**Step 1 — Handler ใหม่ `HandleWifiStatusStream`**
**File:** `backend/internal/api/handlers.go` (วางถัดจาก `HandleGetWifiStatus` ~บรรทัด 929)
- guard: `GetDataLayerInterfaceByID(id)` → 404, `rejectIfOffline`, `Type=="wireless"` → 400
  (คัดลอกจาก `HandleGetWifiStatus`)
- SSE boilerplate จาก `HandleMetricsStream`: headers + `http.Flusher` + `SetWriteDeadline(time.Time{})` + อ่าน token
- ส่ง `event: connected` + snapshot แรก (`GetWifiStatus` → marshal) ทันที
- loop: `select` บน `r.Context().Done()`, `ticker.C`, และ heartbeat ticker (~25s
  ป้องกัน idle-close เหมือน log stream); ทุก tick เช็ค `SessionAlive(token)` แล้ว
  poll → เทียบกับ snapshot ก่อนหน้า → push เฉพาะเมื่อเปลี่ยน
- adaptive: ถ้า `status.State` เป็น transitional → reset ticker เป็น `fast` ไม่งั้น `slow`
  (ถ้าจะให้ง่ายกว่านี้ ใช้ interval คงที่ ~1.5s + dedupe ก็รับได้ — wpa `STATUS` ถูกมาก)

**Step 2 — Route**
**File:** `backend/internal/api/router.go:65` (ต่อจาก `wifi-status` เดิม)
```go
authRoute("GET /api/interfaces/{id}/wifi-status/stream", s.HandleWifiStatusStream)
```
- ใช้ `authRoute` (ไม่ใช่ superAdmin): เป็น read-only, ไม่รั่ว credential (status ไม่มี
  password) — สอดคล้องกับ `wifi-status` เดิมที่เป็น `authRoute`

**Step 3 — Frontend service: เพิ่ม `connectWifiStatusStream`**
**File:** `frontend/src/services/interfaceService.ts` (ถัดจาก `getWifiStatus` ~บรรทัด 215)
- ลอกโครง `connectSSEMetrics` (`dashboardService.ts:344`): รับ `id` + handlers
  `{ onStatus, onOpen?, onError? }`, คืน cleanup ที่ `es.close()`
- **mock branch** (`IS_MOCK_MODE`): ไม่มี EventSource จริง — เรียก `getWifiStatus(id)`
  (มี mock อยู่แล้ว) ยิง `onStatus` ครั้งเดียว + `onOpen`; ไม่ต้อง simulate transition
  ก็ได้ (สถานะ mock เป็น static COMPLETED)
- real branch: `new EventSource(\`${API_BASE_URL}/interfaces/${id}/wifi-status/stream\`, { withCredentials: true })`

**Step 4 — Frontend page: แทน one-shot ด้วย stream**
**File:** `frontend/src/pages/Interfaces.tsx:500` (useEffect ปัจจุบัน)
- เปลี่ยนจาก loop `await getWifiStatus` เป็น: สำหรับแต่ละ wireless+up interface เปิด
  `connectWifiStatusStream(iface.id, { onStatus: s => setWifiLiveStatuses(prev => ({...prev, [iface.id]: s})) })`
- เก็บ cleanup ทุกเส้นใน array แล้ว `return () => cleanups.forEach(c => c())` ใน useEffect
- interface ที่ไม่ใช่ wireless / ไม่ up → คง logic เดิม (set `DISCONNECTED`)

**Step 5 (optional, polish) — badge สถานะกำลังเชื่อมต่อ**
ถ้าการ์ดยังไม่โชว์ transitional state ให้สวย ค่อยเพิ่ม badge สี (semantic color
เท่านั้น ตาม `rules_of_work.md` — ห้าม hardcode palette) — แยกเป็น polish ทีหลังได้

## 4. Related API

| Method | Path | Role | Behavior |
|---|---|---|---|
| GET | `/api/interfaces/{id}/wifi-status/stream` | authRoute (ทุก logged-in role) | **route ใหม่** · SSE push `WifiConnectionStatus` เมื่อสถานะเปลี่ยน · 404 ถ้าไม่พบ / 400 ถ้าไม่ใช่ wireless / offline |

- **`-disable-edit` mode:** ไม่กระทบ — เป็น GET/read-only, `DisableEditMiddleware`
  บล็อกเฉพาะ mutation
- **`-mock` mode:** stream ทำงานได้ (mock `GetWifiStatus` คืน static) — ปลอดภัย 100%
  ไม่มี side effect ต่อ OS

## 5. Cautions

1. **NetEventBus ไม่มี `Unsubscribe` → ห้าม subscribe ต่อ connection.** ถ้าเผลอให้
   SSE handler เรียก `bus.Subscribe` จะ leak subscriber ถาวรทุกครั้งที่เปิดหน้า
   (`event_bus.go:129` append เข้า slice ไม่มีทางถอด) → งาน `Publish` โตเรื่อยๆ.
   **กัน:** v1 ใช้ poll-only ล้วน ไม่แตะ bus.

2. **WriteTimeout 60s ฆ่า stream เงียบๆ.** server มี global `WriteTimeout` 60s;
   ถ้าไม่เคลียร์ deadline ต่อ connection stream จะตายทุก ~60s (ถูกบังด้วย EventSource
   auto-reconnect เลยดูเหมือนทำงาน แต่กระตุก). **กัน:** เรียก
   `http.NewResponseController(w).SetWriteDeadline(time.Time{})` เหมือน
   `HandleMetricsStream:2553` (เป็น regression ที่เคยเจอใน #33).

3. **CORS + credentialed EventSource.** EventSource ส่ง cookie ผ่าน
   `withCredentials`; ถ้า backend ตอบ ACAO เป็น `*` browser จะปฏิเสธ stream. **กัน:**
   ปล่อยให้ `CORSMiddleware` จัดการ (echo origin เฉพาะ + Allow-Credentials) — **อย่า**
   set ACAO เองใน handler (ดู comment `HandleLogStream:2457`). ต้องรัน backend ด้วย
   `-allow-dev-cors` ตอน `yarn dev` ไม่งั้น browser บล็อก.

4. **session ที่ตายระหว่าง stream.** logout/revoke/idle-timeout ต้องตัด stream.
   **กัน:** อ่าน token ตอน connect แล้วเช็ค `SessionAlive(token)` (ไม่ใช่
   `ValidateSession` — จะได้ไม่ slide idle deadline) ทุก tick/heartbeat แล้ว `return`
   ถ้าตาย เหมือน `HandleMetricsStream:2582`.

5. **poll ถี่เกินไปเปลือง CPU/wpa socket.** ถ้าตั้ง interval สั้นคงที่ (เช่น 500ms)
   ตลอดจะยิง wpa `STATUS` ถี่แม้เชื่อมต่อนิ่งแล้ว. **กัน:** adaptive — เร็ว (~1s) เฉพาะ
   ตอน transitional, ช้า (~4s) ตอน COMPLETED/DISCONNECTED; และ **dedupe** push เฉพาะ
   เมื่อ struct เปลี่ยน (browser ไม่ต้อง re-render ฟรีๆ). ยังเป็น runtime อ่านสด —
   ไม่แตะ SQLite (รักษา SD card).

6. **หลาย stream พร้อมกันจากหน้าเดียว.** หน้า Interfaces เปิด stream ต่อ wireless
   interface หลายเส้นพร้อมกัน → ต้องปิดให้ครบตอน unmount/`interfaces` เปลี่ยน. **กัน:**
   เก็บ cleanup ทุกเส้นแล้วเรียกใน useEffect return; ระวัง dependency array ของ
   useEffect ให้ re-run เมื่อชุด wireless interface เปลี่ยนจริงเท่านั้น (ไม่งั้น
   เปิด/ปิด stream รัวๆ). อ้างอิงการจัดการ EventSource lifecycle ที่
   `MetricsProvider.tsx` / `useLiveLogs.ts`.

7. **การเปลี่ยน useEffect ที่ `Interfaces.tsx:500` อาจกระทบ logic เดิม.** effect เดิม
   จัดการทั้ง wireless-up (fetch) และ wireless-down (set DISCONNECTED). **กัน:** คง
   สาขา wireless-down ไว้เหมือนเดิม เปลี่ยนเฉพาะสาขา up จาก fetch → stream; ทดสอบว่า
   toggle interface down แล้ว badge กลับเป็น DISCONNECTED ถูกต้อง.

## 6. Summary Checklist (Definition of Done)

**Backend**
- [ ] `HandleWifiStatusStream` ใน `api/handlers.go` (guard + SSE + adaptive poll + dedupe + session re-check + heartbeat + clear WriteTimeout)
- [ ] route `GET /api/interfaces/{id}/wifi-status/stream` ใน `router.go` (`authRoute`)
- [ ] `go build ./...` + `go test ./...` ผ่าน

**Frontend**
- [ ] `connectWifiStatusStream` ใน `interfaceService.ts` (real EventSource + mock branch)
- [ ] แก้ useEffect `Interfaces.tsx:500` ใช้ stream + cleanup ครบ, คงสาขา down เดิม
- [ ] `yarn build` + `yarn lint` ผ่าน; badge transitional ใช้ semantic color (ไม่ hardcode palette), flat design

**Docs / contract**
- [ ] เพิ่ม path ใน `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (sync ทั้งคู่) — SSE (`text/event-stream`) แบบเดียวกับ `/dashboard/performance/stream:291`
- [ ] README Feature Status: Interfaces ยัง Completed (ไม่ต้องเปลี่ยน) — ระบุใน PR ว่าเป็น live-status enhancement

**Testing**
- [ ] mock: `-mock=true -allow-dev-cors` → หน้า Interfaces เปิด stream, badge เป็น COMPLETED (static) ไม่ error
- [ ] real device (มี physical access): เชื่อม Wi-Fi ใหม่แล้วสังเกต badge ไล่ SCANNING → … → COMPLETED; toggle down → DISCONNECTED
- [ ] เปิด DevTools Network: ยืนยัน 1 EventSource ต่อ wireless interface, ปิดหน้าแล้ว connection ถูก close (ไม่ leak)
- [ ] role read-only ยังเปิด stream ได้ (เป็น GET); `-disable-edit` ไม่บล็อก
