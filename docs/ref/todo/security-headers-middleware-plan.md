# Security Headers Middleware — CSP + X-Frame-Options + nosniff ทุก response

> แผนงานแก้ security review finding 6 (Medium): ตอนนี้ไม่มี response ไหนตั้ง
> Content-Security-Policy / X-Frame-Options / X-Content-Type-Options เลย —
> admin UI โดน clickjack ได้ และไม่มี CSP เป็น backstop กัน script แปลกปลอม
> (ชั้น defense-in-depth ที่งาน cookie-only auth #29 คาดหวังไว้)
> เป้าหมาย: middleware ตัวเดียวตั้ง headers ทุก response โดย**ไม่ทำ SPA หน้าไหนพัง**
>
> เขียนเมื่อ: 2026-07-11 · Reference branch: `feat/security-headers-middleware` · Issue: #35
> ชิ้นสุดท้ายของ remediation roadmap ข้อ 5 (ต่อจาก #33 body cap และ #34 limiter eviction)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- ทุก response (SPA static, API JSON, SSE, 401/403 จาก middleware) มี headers:
  - `Content-Security-Policy` ตามร่างใน §2 — `script-src 'self'` แบบเข้ม (ไม่มี
    unsafe-inline ฝั่ง script), `frame-ancestors 'none'`
  - `X-Frame-Options: DENY` (legacy fallback ของ frame-ancestors)
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
- ทุกหน้าของ SPA ใช้งานได้ครบโดย devtools console **ไม่มี CSP violation แม้แต่รายการเดียว**
  — รวมหน้าเสี่ยงสูง: Dashboard (recharts + shadcn chart), ApiDocs (swagger-ui),
  export config (blob download), SSE log stream, dark/light toggle
- HTTP fallback mode ยังเข้าได้ปกติ (ไม่มี HSTS — ดูเหตุผลใน §2)

**Out of scope (ตัดออกชัดเจน):**
- `Strict-Transport-Security` (HSTS) — **ตั้งไม่ได้โดยดีไซน์**: PiGate มี plain-HTTP
  fallback mode เมื่อ TLS setup ล้มเหลว (`main.go:334-339`) browser ที่จำ HSTS ไว้จะ
  ปฏิเสธ HTTP fallback → แอดมินโดนล็อกออกจากกล่องถาวรจนกว่าจะล้าง HSTS เอง
- CSP nonce/hash สำหรับ style (ไล่แก้ inline style ทั้ง recharts/shadcn/swagger ไม่คุ้ม —
  ดู §2 ทางเลือกข้อ 2)
- Permissions-Policy / COOP / COEP — ผลตอบแทนต่ำสำหรับ admin UI ใน LAN ทำภายหลังได้
- CSP report-only phase + reporting endpoint (ดู §2 ทางเลือกข้อ 4)

## 1. Current State (สำรวจโค้ดจริง 2026-07-11)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Security headers ใน backend | **ไม่มีเลย** | grep `X-Frame\|Content-Security\|nosniff\|Referrer-Policy\|Strict-Transport` ทั้ง backend = 0 hits |
| จุดประกอบ middleware กลาง | มีแล้ว เสียบได้เลย | `backend/internal/api/router.go:~283-289` — `BodyLimitMiddleware`(จาก #33 ถ้า merge แล้ว) → `DisableEditMiddleware` → `CORSMiddleware` นอกสุด |
| SPA serving | `serveStatic` + SPA fallback เขียน header เองเฉพาะ index.html | `backend/internal/api/embed.go:25-64` — fallback ตั้ง `Cache-Control` + `Content-Type` (:54-55); assets ผ่าน `http.FileServer` (mime ตามนามสกุลไฟล์) |
| `index.html` ที่ build แล้ว **ไม่มี inline script** | `script-src 'self'` เข้มได้เลย | `backend/internal/api/dist/index.html` — มีแค่ `<script type="module" src="/assets/...">` |
| Inline style มีจริง 3 แหล่ง | ต้องมี `style-src 'unsafe-inline'` | `frontend/src/components/ui/chart.tsx:95` (ฉีด `<style>` ผ่าน dangerouslySetInnerHTML — จุดเดียวทั้ง repo), recharts/React style attributes, swagger-ui (`pages/ApiDocs.tsx:2` import css + inline styles runtime) |
| `data:` URI ใน CSS bundle | ต้องมี `img-src data:` | grep `data:image\|data:font` ใน `dist/assets/*.css` = 1 hit (ไอคอน swagger) |
| Web workers | ไม่มี — ไม่ต้องมี `worker-src` | grep `new Worker(` ใน dist bundle = 0 |
| Blob usage | มีแค่ export download — CSP ไม่บล็อก | `frontend/src/pages/SettingsMaintenance.tsx:342` (`URL.createObjectURL` + anchor download) |
| External resources | ไม่มีโหลดจริง — `https://` ใน bundle เป็น doc-link string ของไลบรารี | grep dist assets: radix-ui.com, react.dev ฯลฯ (string เฉยๆ) |
| SSE | same-origin → `connect-src 'self'` พอ | `frontend/src/services/dashboardService.ts:282` (`/api/dashboard/logs/stream`) |
| swagger-ui โหลด spec | same-origin | `pages/ApiDocs.tsx:64` — `<SwaggerUI url="/openapi.yaml" />` (ไฟล์อยู่ `frontend/public/`) |
| Dev mode (Vite :5173) | ไม่กระทบ — เอกสาร SPA มาจาก Vite ไม่ใช่ backend | headers จาก backend ติดเฉพาะ API response ซึ่ง CSP ไม่มีผลกับ JSON |
| kernel / service / db / install.sh | ไม่เกี่ยว | งานจบใน api layer (middleware + router) |

สรุป: โค้ดจริงคือ middleware ใหม่ 1 ตัว + เสียบ 1 จุด — **น้ำหนักของงานอยู่ที่การไล่ verify
ทุกหน้าใน embedded build** ว่าไม่มี CSP violation ไม่ใช่ตัวโค้ด

## 2. Technical Approach

**กลไกที่เลือก: middleware ตัวเดียว ตั้ง headers คงที่ทุก response**

```go
// backend/internal/api/middleware.go
const cspPolicy = "default-src 'self'; " +
    "script-src 'self'; " +
    "style-src 'self' 'unsafe-inline'; " + // recharts/shadcn chart/swagger (ดู §1)
    "img-src 'self' data:; " +
    "font-src 'self' data:; " +
    "connect-src 'self'; " +
    "frame-ancestors 'none'; " +
    "base-uri 'self'; form-action 'self'; object-src 'none'"

func SecurityHeadersMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
        h := w.Header()
        h.Set("Content-Security-Policy", cspPolicy)
        h.Set("X-Frame-Options", "DENY")
        h.Set("X-Content-Type-Options", "nosniff")
        h.Set("Referrer-Policy", "no-referrer")
        next.ServeHTTP(w, r)
    })
}
```

เสียบใน `router.go` chain เดิม: `CORSMiddleware(SecurityHeadersMiddleware(handler))` —
ตั้งกับทุก response รวม API JSON (CSP ไม่มีผลกับ JSON แต่ตั้งเหมารวมง่าย/พลาดยากกว่า
แยกเฉพาะ HTML และได้ nosniff ครอบ API ไปด้วย)

**เหตุผลของ directive ที่เข้มได้/ต้องหย่อน (จากการสำรวจจริง §1):**
- `script-src 'self'` เข้มเต็มที่ได้เพราะ index.html ไม่มี inline script — นี่คือชั้นที่
  สำคัญที่สุด (กัน XSS ฉีด `<script>`/inline handler ได้จริง)
- `style-src` ต้องมี `'unsafe-inline'` — ยอมรับ: ความเสี่ยง CSS injection ต่ำกว่า script มาก
  และแหล่ง inline style คือ 3 ไลบรารีที่เราไม่ควบคุม
- `frame-ancestors 'none'` + `X-Frame-Options: DENY` — ไม่มี use case ฝัง PiGate ใน iframe

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *ตั้ง headers เฉพาะ response ที่เป็น HTML (ใน `serveStatic`)* — ตัดทิ้ง: ต้องแตะ 2 จุด
   (fallback `embed.go:47-57` + FileServer path) และลืม endpoint ใหม่ในอนาคตง่าย;
   middleware ครอบหมดจุดเดียว fail-safe กว่า, ค่า header ต่อ response เป็น byte หลักร้อยไม่มีนัยยะ
2. *CSP แบบ nonce/hash เพื่อตัด `'unsafe-inline'` ของ style* — ตัดทิ้ง: ต้อง fork พฤติกรรม
   ของ recharts/shadcn chart/swagger-ui หรือ post-process HTML ตอน serve — ความซับซ้อนสูง
   มากเพื่อปิดช่อง CSS injection ที่เล็ก; `script-src` ที่เข้มคือกำแพงหลักและได้เต็มแล้ว
3. *ใส่ HSTS ด้วย* — ตัดทิ้ง (สำคัญ): ขัดกับดีไซน์ HTTP fallback (`main.go:334-339`) —
   browser จำ HSTS แล้วจะปฏิเสธ plain HTTP ทั้ง origin → วันที่ cert พัง/ถูกลบ แอดมิน
   เข้ากล่องไม่ได้เลย ทั้งที่ fallback ถูกออกแบบมาเพื่อกรณีนั้นโดยเฉพาะ
4. *เริ่มด้วย `Content-Security-Policy-Report-Only` ก่อนค่อย enforce* — ตัดทิ้ง: ต้องมี
   report endpoint + รอบ deploy เพิ่ม; แอปเป็น embedded SPA ที่เราไล่ verify ทุกหน้าได้เอง
   ใน DoD ครอบพฤติกรรมครบกว่า report แบบ passive
5. *เพิ่ม `<meta http-equiv="Content-Security-Policy">` ใน index.html แทน header* —
   ตัดทิ้ง: meta CSP ไม่รองรับ `frame-ancestors`, ไม่ครอบ asset อื่น และผูกกับ build
   ของ frontend — header ฝั่ง Go ครอบทุกอย่างและอยู่ที่เดียวกับ middleware อื่น

**Pattern ที่ยึด:** โครง middleware เดิม (`DisableEditMiddleware` `middleware.go:268`);
จุดประกอบตาม `router.go:283-289` (comment "CORS outermost" เดิมยังถูกต้อง)

## 3. Steps (เรียงชั้นในสุด → นอกสุด)

### Step 1 — `SecurityHeadersMiddleware` + `cspPolicy`
**File:** `backend/internal/api/middleware.go` (ท้ายไฟล์ ต่อจาก middleware อื่น)
ตาม §2 — comment ต้องอธิบาย: ทำไม style ต้อง unsafe-inline (3 แหล่ง), ทำไมห้ามเพิ่ม HSTS
(อ้าง HTTP fallback), และถ้าเพิ่ม external resource ใหม่ใน frontend ต้องมาแก้ CSP ที่นี่

### Step 2 — เสียบใน router chain
**File:** `backend/internal/api/router.go:~288`
`return CORSMiddleware(handler)` → `return CORSMiddleware(SecurityHeadersMiddleware(handler))`
> **สิ่งที่ไม่ต้องทำ:** ไม่แตะ `embed.go` — headers มาจาก middleware ชั้นนอก;
> `Cache-Control`/`Content-Type` เดิมของ SPA fallback (:54-55) ทำงานร่วมกันได้
> (คนละ header ไม่ทับกัน); ไม่แตะ frontend เลย — ไม่มี inline script ให้แก้อยู่แล้ว

### Step 3 — Backend tests
**File:** `backend/internal/api/middleware_test.go` (รวมกับของ #33/#34 ถ้าไฟล์เกิดแล้ว)
- GET `/` (SPA index) และ GET `/api/auth/session` (ผ่าน auth) → ทั้งคู่ต้องมี headers ครบ 4 ตัว
  และค่า CSP ตรงกับ `cspPolicy`
- Response 401 จาก `AuthMiddleware` (ยิงโดยไม่มี cookie) → มี headers ด้วย
  (พิสูจน์ว่า middleware อยู่นอก auth ใน chain)

### Step 4 — Verify ทุกหน้าบน embedded build (หัวใจของงานนี้)
**ไม่ใช่ไฟล์โค้ด — เป็นรอบทดสอบบังคับก่อนเปิด PR:**
`bash build.sh` → เปิดผ่าน HTTPS แล้วไล่ทุกหน้าโดยเปิด devtools console ค้างไว้:
Dashboard (กราฟ recharts + ring ของ shadcn chart + SSE), Interfaces, Firewall Policy,
DHCP, DNS Server, QoS, Static Routes, Event Logs, Forward Traffic, Settings
(**กด export ให้ blob download ทำงานจริง** + import กลับ), ApiDocs (swagger render + expand
endpoint), dark/light toggle, login/logout — **ต้องไม่มี CSP violation แดงใน console เลย**
(ใช้ skill `/verify` ช่วยไล่แบบ headless ได้ แต่รอบสุดท้ายควรดู console ด้วยตาเพราะ
violation ไม่ทำให้ Playwright fail เสมอไป)

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| ทุก endpoint + static | `/*` | ตาม route เดิม | เพิ่ม response headers 4 ตัว — ไม่มี route ใหม่, body/status เดิมทุกอย่าง |

`-disable-edit` mode: ไม่กระทบ — headers เป็น read-only concern; `DisableEditMiddleware`
อยู่ชั้นในกว่าใน chain เดิม

## 5. Cautions

1. **`nosniff` ทำให้ Content-Type ที่ผิดกลายเป็นของพัง ไม่ใช่แค่ warning** — browser จะ
   ปฏิเสธ `<script>`/`<link rel=stylesheet>` ที่ MIME ไม่ตรง; assets ของเราเสิร์ฟผ่าน
   `http.FileServer` (mime จากนามสกุล — Go มี builtin table ครอบ .js/.css/.svg) และ
   fallback ตั้ง `text/html` เองแล้ว (`embed.go:55`) → ความเสี่ยงต่ำ แต่ต้อง verify บน
   embedded build จริง (Step 4) เพราะถ้าพังคือหน้าขาวทั้งแอป
2. **CSP violation บางอย่างโผล่เฉพาะ interaction** — เช่น swagger expand endpoint,
   chart tooltip, dialog ที่ render ครั้งแรกตอนกด → การเปิดหน้าเฉยๆ ไม่พอ ต้อง**กดใช้งานจริง**
   ตามรายการใน Step 4; ห้าม verify แค่ landing ของแต่ละ route
3. **ห้ามเพิ่ม HSTS ทีหลังโดยไม่ได้อ่านดีไซน์ HTTP fallback** — `main.go:334-339` เสิร์ฟ
   plain HTTP เมื่อ TLS พังโดยตั้งใจ; HSTS ที่ browser จำไว้ (`includeSubDomains`/อายุยาว)
   จะทำให้เข้ากล่องไม่ได้ในวันที่ต้องใช้ fallback พอดี → comment ใน middleware ต้องเตือนไว้
   (Step 1) เผื่อคนมาเติม "best practice" ภายหลัง
4. **CORS ต้องอยู่นอกสุดเหมือนเดิม** — comment เดิมที่ `router.go:288` มีเหตุผลอยู่แล้ว
   (403 ต้องมี CORS headers ให้ dev mode); `SecurityHeadersMiddleware` เข้าไปชั้นใน
   ของ CORS — ถ้าสลับกัน dev mode จะอ่าน error response ไม่ได้
5. **อนาคตถ้า frontend เพิ่ม resource ภายนอก** (font CDN, รูป remote, websocket ใหม่)
   หน้านั้นจะพังเงียบๆ เฉพาะ production (dev ผ่าน Vite ไม่มี CSP) — อาการคือ "dev ได้
   prod พัง" → จุดแก้คือ `cspPolicy` ใน `middleware.go` จุดเดียว; จดใน comment (Step 1)
   และควรถือเป็นเหตุผลเสริมที่จะ**ไม่**เพิ่ม external dependency ฝั่ง UI (นโยบายเดิมอยู่แล้ว)
6. **อย่าใช้ `w.Header().Add`** — ถ้า handler ภายในตั้ง header ชื่อเดียวกันซ้ำจะได้ค่าซ้อน
   (browser ตีความ CSP หลายค่าแบบ intersect = เข้มขึ้นจนพังได้) → ใช้ `Set` ใน middleware
   และตอนนี้ไม่มี handler ไหนตั้ง 4 ตัวนี้เอง (grep ยืนยันแล้ว §1) — คงไว้แบบนั้น
7. **ทดสอบบนอุปกรณ์จริง**: ไม่แตะ firewall/routing — ไม่มีความเสี่ยง network lock-out;
   ความเสี่ยงจริงคือ SPA พังทั้งแอปจาก CSP/nosniff ผิด → รอบ verify ใน Step 4 ต้องผ่าน
   ครบบน WSL ก่อน แล้วผู้ใช้ deploy ขึ้นเครื่องจริงเอง (workflow เดิม)

## 6. Summary Checklist (Definition of Done)

- [ ] `backend/internal/api/middleware.go` — `cspPolicy` + `SecurityHeadersMiddleware`
      (comment: เหตุผล unsafe-inline ของ style, คำเตือน HSTS, จุดแก้เมื่อเพิ่ม resource ใหม่)
- [ ] `backend/internal/api/router.go` — `CORSMiddleware(SecurityHeadersMiddleware(handler))`
- [ ] Tests: headers ครบบน SPA index / API response / 401 response
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] Verify รอบเต็มบน embedded build ตาม Step 4 — ทุกหน้า + ทุก interaction สำคัญ
      โดย console ไม่มี CSP violation; export/import ทำงาน; SSE ต่อติด; swagger render
- [ ] ทดสอบ dev mode (`yarn dev` + backend mock) — ทุกอย่างเดิม (CSP ไม่ครอบ Vite,
      ยืนยันว่าไม่มี regression จาก header บน API)
- [ ] ตรวจ HTTP fallback: รัน backend แบบไม่มี TLS แล้วเข้า plain HTTP ได้ปกติ
      (ยืนยันไม่มีใครแอบเพิ่ม HSTS)
- [ ] ไม่มี OpenAPI/README ต้องแก้ (headers ไม่ใช่ API contract — จดเหตุผลไว้ที่นี่)
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/` + อัปเดต security review artifact
      (finding 6 → done — ปิด roadmap ข้อ 5 ครบทั้ง 4+5+6)
