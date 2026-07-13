# HTTP Server Hardening — global request body cap + แก้ SSE โดน WriteTimeout ตัด

> แผนงานปิด security review finding 4 (Medium) ส่วนที่เหลือ: ครึ่ง timeouts ทำเสร็จแล้ว
> ตั้งแต่งาน HTTPS foundation (#27) — งานที่เหลือคือ (1) request body cap กลาง ~1 MB
> ให้ทุก endpoint (ตอนนี้มีแค่ config import ที่ cap 10 MB) และ (2) บั๊กแฝงที่เจอระหว่างสำรวจ:
> `WriteTimeout: 60s` จาก #27 **ตัด SSE log stream ทุก ~60 วินาที** โดย EventSource
> reconnect เองเงียบๆ จึงไม่มีใครเห็นอาการ
>
> เขียนเมื่อ: 2026-07-11 · Reference branch: `fix/http-body-cap-sse-deadline` · Issue: #33
> งานลำดับถัดจาก #32 (session TTL) ใน remediation roadmap ของ security review

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- ทุก endpoint มี request body cap **1 MB** ผ่าน middleware กลางตัวเดียว —
  ยกเว้น `POST /api/system/config/import` ที่คง cap 10 MB ของตัวเองไว้
- Body ที่เกิน cap ทำให้ handler อ่าน/decode ล้มเหลว → client ได้ 4xx ไม่ใช่กิน RAM ของ Pi
- SSE log stream (`/api/dashboard/logs/stream`) อยู่ได้ยาวเกิน 60 วินาทีโดยไม่หลุด —
  เคลียร์ write deadline เฉพาะ connection นั้น โดย `WriteTimeout` global ยังคุม
  endpoint ปกติทุกตัวเหมือนเดิม

**Out of scope (ตัดออกชัดเจน):**
- Rate-limiter map eviction (finding 5) และ security headers/CSP (finding 6) — งานแยก
  (roadmap ข้อ 5 ของ review รวม 4+5+6 ไว้ด้วยกัน แต่แยกแผน/แยก PR ให้เล็กและ verify ง่าย)
- เปลี่ยน error response 34 จุดให้เป็น 413 สวยๆ — body เกิน cap จะได้ 400 จาก error path
  เดิมของแต่ละ handler ซึ่งยอมรับได้ (ดู Caution 4)
- ปรับค่า timeouts ที่มีอยู่ (`main.go:318-321, :345-348`) — ค่าปัจจุบันเหมาะสมแล้ว

## 1. Current State (สำรวจโค้ดจริง 2026-07-11)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Server timeouts (HTTPS + HTTP fallback) | **เสร็จแล้ว** (จาก #27) | `backend/cmd/pigate/main.go:315-322` (httpsServer), `:342-349` (httpServer) — ReadHeader 10s / Read 30s / Write 60s / Idle 120s |
| Redirect listener | มี ReadHeaderTimeout แล้ว | `main.go:387` — ตอบ 308 อย่างเดียว ไม่ต้องเพิ่มอะไร |
| Body cap | มีจุดเดียว | `backend/internal/api/handlers.go:2028` — `HandleImportConfig` cap 10 MB + error message ชัดเจน (`:2030-2033`) |
| JSON endpoints อื่นทั้งหมด | **ไม่มี cap** | `json.NewDecoder(r.Body)` 34 จุดใน `handlers.go` — อ่าน body ไม่จำกัด |
| จุดประกอบ middleware กลาง | มีแล้ว ใช้เสียบได้เลย | `backend/internal/api/router.go:~283-289` — `var handler http.Handler = mux` → (`DisableEditMiddleware`) → `CORSMiddleware` นอกสุด |
| SSE handler ไม่ยืด write deadline | **บั๊ก — โดน WriteTimeout ตัดทุก 60s** | `handlers.go:2097-2137` (`HandleLogStream`); grep `ResponseController\|SetWriteDeadline` ทั้ง backend = 0 hits |
| ฝั่ง client มองไม่เห็นอาการ | ไม่ต้องแก้ | `frontend/src/services/dashboardService.ts:283` — `EventSource` auto-reconnect โดย browser เอง stream จึง "ดูปกติ" ทั้งที่หลุด/ต่อใหม่ทุกนาที |
| Go version รองรับ `ResponseController` | พร้อมใช้ (ต้อง Go ≥1.20) | `backend/go.mod:3` — `go 1.26.4` |
| kernel / service / db / install.sh / frontend | ไม่เกี่ยว | ทั้งสองเรื่องจบใน api layer — ไม่มี migration / boot-apply / Polkit |

หมายเหตุ: ตำแหน่งใน review เดิม (`main.go:233`, `handlers.go:1736`) drift ไปแล้ว — review ทำที่
commit `9b13658` ก่อน #27/#28 merge; ตารางข้างบนคือตำแหน่งปัจจุบัน

สรุป: งานจริงเล็กมาก — middleware ใหม่ 1 ตัว + เสียบ 1 จุดใน `router.go` + 2 บรรทัดใน
`HandleLogStream` + tests; **ไม่แตะ frontend เลย**

## 2. Technical Approach

**(1) Body cap: middleware กลางครอบ mux ทั้งก้อน + skip list**

```go
// backend/internal/api/middleware.go
const maxRequestBody = 1 << 20 // 1 MB — JSON config ทุกตัวเล็กกว่านี้มาก

func BodyLimitMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        // import มี cap 10 MB ของตัวเอง — ห้ามครอบซ้ำ (ดู Caution 1)
        if r.URL.Path != "/api/system/config/import" && r.Body != nil {
            r.Body = http.MaxBytesReader(w, r.Body, maxRequestBody)
        }
        next.ServeHTTP(w, r)
    })
}
```

เสียบใน `router.go:~283`: `var handler http.Handler = BodyLimitMiddleware(mux)`
(อยู่ในสุด — `DisableEditMiddleware`/`CORSMiddleware` ครอบทับตามเดิม ลำดับเดิมไม่เปลี่ยน)

**(2) SSE: เคลียร์ write deadline เฉพาะ connection ของ stream**

```go
// ต้นทาง HandleLogStream หลังเช็ค flusher
rc := http.NewResponseController(w)
_ = rc.SetWriteDeadline(time.Time{}) // zero = ไม่มี deadline เฉพาะ conn นี้
```

`WriteTimeout` ของ `http.Server` ทำงานโดยตั้ง write deadline ต่อ connection —
`ResponseController.SetWriteDeadline(time.Time{})` (Go ≥1.20) เขียนทับได้รายตัว
จึงคุมทุก endpoint ปกติเหมือนเดิมแต่ปล่อย stream ตัวเดียว

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *ไล่ใส่ `MaxBytesReader` รายจุดทั้ง 34 handler* — ตัดทิ้ง: ซ้ำซาก, จุดใหม่ในอนาคตลืมง่าย,
   middleware จุดเดียว fail-safe กว่า (endpoint ใหม่ได้ cap อัตโนมัติ)
2. *ตาราง limit per-route (map pattern→size) แล้วถอด cap ใน import ออก* — ตัดทิ้ง:
   over-engineering สำหรับข้อยกเว้นเดียว; cap ของ import อยู่ติด handler พร้อม error message
   เฉพาะทาง (`handlers.go:2030`) อ่านง่ายกว่า — middleware แค่ skip path นั้น
3. *ลบ `WriteTimeout` ออกจาก server เพื่อช่วย SSE* — ตัดทิ้ง: เปิดช่อง slowloris ฝั่ง write
   กลับมาทั้งระบบเพื่อ endpoint เดียว — แก้ที่ตัว stream ถูกจุดกว่า
4. *เปลี่ยน SSE เป็น polling ธรรมดา* — ตัดทิ้ง: เปลี่ยนสถาปัตยกรรม feature ที่ทำงานอยู่
   เกินขอบเขต security fix

**Pattern ที่ยึด:** โครง middleware เดิมใน `middleware.go` (เช่น `DisableEditMiddleware:268`);
จุดประกอบตาม `router.go:283-289`

## 3. Steps (เรียงชั้นในสุด → นอกสุด)

### Step 1 — `BodyLimitMiddleware`
**File:** `backend/internal/api/middleware.go` (ท้ายไฟล์ ใกล้ `DisableEditMiddleware:~268`)
เพิ่ม `maxRequestBody` + `BodyLimitMiddleware` ตาม §2 — comment อธิบายเหตุผล skip import

### Step 2 — เสียบ middleware ใน router
**File:** `backend/internal/api/router.go:~283`
`var handler http.Handler = mux` → `var handler http.Handler = BodyLimitMiddleware(mux)`
> **สิ่งที่ไม่ต้องทำ:** ไม่แตะ `HandleImportConfig` (`handlers.go:2028`) — cap 10 MB
> ของเดิมทำงานต่อเพราะ middleware ข้าม path นี้ให้แล้ว

### Step 3 — SSE เคลียร์ write deadline
**File:** `backend/internal/api/handlers.go:~2110` (ใน `HandleLogStream` หลังเช็ค `flusher`)
เพิ่ม `http.NewResponseController(w).SetWriteDeadline(time.Time{})` + comment ว่าทำไม
(WriteTimeout 60s ของ server จะตัด long-lived response — เคลียร์เฉพาะ conn นี้)

### Step 4 — Backend tests
**File:** `backend/internal/api/middleware_test.go` (ไฟล์ใหม่) หรือรวมใน `handlers_test.go`
- POST body > 1 MB ไป endpoint ปกติ (เช่น `/api/qos/rules`) → ได้ 4xx ไม่ panic
- POST body ~2 MB ไป `/api/system/config/import` (payload JSON ถูก format) →
  **ไม่**โดน cap 1 MB (พิสูจน์ skip list ทำงาน — จะไป fail ที่ validation อื่นแทน ไม่ใช่ read error)
- body เล็กปกติ → ทำงานเดิมทุก endpoint (tests เดิมทั้งชุดคือ regression test อยู่แล้ว)
> **สิ่งที่ไม่ต้องทำ:** unit test ของ SSE deadline — `httptest.ResponseRecorder` ไม่มี
> connection จริงให้ตั้ง deadline (`SetWriteDeadline` คืน `ErrNotSupported` ซึ่งเรา
> ignore อยู่แล้ว) → พิสูจน์ด้วย manual test บน embedded build แทน (ดู DoD)

### Step 5 — OpenAPI ทั้งสองไฟล์
**File:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` (แก้ให้เหมือนกัน)
เพิ่มคำอธิบายระดับ API description: request body จำกัด 1 MB ทุก endpoint
(ยกเว้น import = 10 MB) — จุดเดียวพอ ไม่ต้องไล่ราย endpoint

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| ทุก endpoint | `/api/*` | ตาม route เดิม | body > 1 MB → handler อ่านไม่ผ่าน ได้ 4xx (เดิมอ่านไม่จำกัด) — ไม่มี route ใหม่ |
| POST | `/api/system/config/import` | super_admin | ไม่เปลี่ยน — คง cap 10 MB เดิม |
| GET | `/api/dashboard/logs/stream` | authRoute | stream ไม่หลุดทุก 60s อีกต่อไป (พฤติกรรม contract เดิม แค่เลิก drop) |

`-disable-edit` mode: ไม่กระทบ — `BodyLimitMiddleware` อยู่ชั้นในกว่า `DisableEditMiddleware`
และไม่เกี่ยวกับ method/mutation

## 5. Cautions

1. **ห้ามให้ middleware ครอบ body ของ import ซ้ำ** — `MaxBytesReader` ซ้อนกันตัวเล็กชนะ:
   ถ้า middleware ใส่ 1 MB ก่อนแล้ว import ค่อยใส่ 10 MB ทับ ตัวใน (1 MB) ยังบังคับอยู่ →
   backup จริงขนาด >1 MB จะ import ไม่ได้ทั้งที่ตั้งใจให้ได้ถึง 10 MB → ต้อง skip path
   ใน middleware และมี test ยืนยัน (Step 4 ข้อ 2)
2. **SSE fix ต้องใช้ `ResponseController` ไม่ใช่ยกเลิก `WriteTimeout`** — ลบ WriteTimeout
   ทั้ง server = เปิด slowloris write-side ทั้งระบบกลับมา (ถอย finding 4 ครึ่งที่ทำแล้ว);
   `SetWriteDeadline(time.Time{})` มีผลเฉพาะ connection ของ stream เท่านั้น
3. **`SetWriteDeadline` อาจคืน error บน ResponseWriter ที่ไม่รองรับ** (เช่นใน tests /
   proxy บางแบบ) — ต้อง ignore error แล้วทำงานต่อ (stream จะโดนตัดทุก 60s เหมือนเดิม
   ซึ่งไม่แย่กว่าปัจจุบัน) ห้าม `http.Error` ทิ้งเพราะ client ที่ reconnect ได้อยู่จะพังแทน
4. **Body เกิน cap ให้ 400 ไม่ใช่ 413** — `json.NewDecoder` เจอ `MaxBytesError` แล้ว handler
   ตอบ "Invalid request payload" (400) ตาม error path เดิม 34 จุด — ยอมรับ: การไล่แก้ให้
   ตอบ 413 ทุกจุดคือ churn ใหญ่เพื่อความสวยของ status code; ค่าใช้จ่ายไม่คุ้ม
   (จดไว้เผื่อทำ `readJSON` helper กลางในอนาคต)
5. **เลือก 1 MB เผื่อกว้างแล้ว** — payload จริงใหญ่สุดของระบบคือ firewall/DNS config
   หลัก KB; แต่ถ้าอนาคตมี endpoint รับข้อมูลใหญ่ (เช่น upload ไฟล์อื่น) ต้องเพิ่ม path
   นั้นใน skip list ของ middleware — comment ใน middleware ต้องบอกจุดนี้ไว้
6. **อย่าลืมว่า CORS ต้องอยู่นอกสุดเสมอ** (`router.go:288` comment เดิม) — เสียบ
   `BodyLimitMiddleware` ที่ชั้น mux ด้านใน ไม่ใช่ไปครอบนอก `CORSMiddleware`
   ไม่งั้น response 4xx จาก cap อาจไม่มี CORS headers ให้ dev mode
7. **ทดสอบบนอุปกรณ์จริง**: ไม่แตะ firewall/routing — ไม่มีความเสี่ยง network lock-out;
   จุดต้อง verify จริงคือ SSE (ต้องดูนานเกิน 60 วินาที) และ import backup จริงผ่าน HTTPS
   → ทดสอบบน embedded build ใน WSL ก่อน แล้วผู้ใช้ deploy เอง (workflow เดิม)

## 6. Summary Checklist (Definition of Done)

- [ ] `backend/internal/api/middleware.go` — `maxRequestBody` + `BodyLimitMiddleware`
      (skip `/api/system/config/import`)
- [ ] `backend/internal/api/router.go` — `BodyLimitMiddleware(mux)` ชั้นในสุดของ chain เดิม
- [ ] `backend/internal/api/handlers.go` — `HandleLogStream` เคลียร์ write deadline
      ผ่าน `http.NewResponseController` (ignore error)
- [ ] Tests: body > 1 MB → 4xx / import ~2 MB ไม่โดน cap กลาง / tests เดิมผ่านครบ
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] ทดสอบ mock mode (`-mock=true`): ทุกหน้าใช้งานปกติ, import/export ทำงาน,
      SSE log stream ต่อติด
- [ ] ทดสอบ embedded build (`bash build.sh` + HTTPS): เปิด Dashboard ค้างไว้ **> 3 นาที**
      แล้วดู devtools Network — `logs/stream` ต้องเป็น connection เดียวตลอด
      ไม่มี reconnect ทุก ~60s (เทียบก่อนแก้ให้เห็น drop เดิมด้วยยิ่งดี)
- [ ] ทดสอบ `curl -k -b jar.txt -X POST` ด้วย payload > 1 MB → ได้ 4xx เร็ว ไม่ค้าง
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — เพิ่มคำอธิบาย body limit (sync ทั้งคู่)
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/` + อัปเดต security review artifact
      (finding 4 → done)
