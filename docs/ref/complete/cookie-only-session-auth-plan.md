# Cookie-Only Session Auth — เลิกส่ง token ใน login body / เลิกเก็บใน localStorage

> แผนงานแก้ security review finding 2 (High): ปัจจุบัน session token ถูกส่งให้ browser
> สองทาง — HttpOnly cookie **และ** JSON body ของ `/auth/login` ซึ่ง frontend เก็บลง
> `localStorage` แล้วแนบกลับเป็น `Authorization: Bearer` ทำให้ XSS ใดๆ ขโมย session ได้ทันที
> (ทำลายประโยชน์ของ HttpOnly) เป้าหมายคือให้ **cookie เป็นช่องทางเดียว** ของ session token
>
> เขียนเมื่อ: 2026-07-10 · Reference branch: `feat/cookie-only-session-auth` · Issue: #29
> ต่อยอดจาก HTTPS foundation (#27/#28) ที่ทำให้ cookie `Secure` ได้จริงแล้ว

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- Response ของ `POST /api/auth/login` ไม่มี field `token` อีกต่อไป — browser ได้ session
  จาก `Set-Cookie` (HttpOnly, Secure per-request, SameSite=Strict) เท่านั้น
- `localStorage` ไม่มี key `pigate_session` (token) เหลืออยู่ — เก็บได้เฉพาะ UI state ที่ไม่ลับ
  (role, username, mustChangePassword, flag ว่า logged-in)
- Backend เลิกรับ `Authorization: Bearer` — `AuthMiddleware` เช็ค cookie ทางเดียว
- ทุก flow เดิมยังทำงานครบ: login → force change password → ใช้งาน → logout,
  SSE log stream, dev mode ข้าม origin (`localhost:5173` → `localhost:8081`), frontend mock mode

**Out of scope (ตัดออกชัดเจน):**
- Server-side session TTL / sweeper goroutine (finding 3 — งานแยก)
- Security headers / CSP middleware (finding 6)
- Rate-limiter eviction (finding 5)
- การเปลี่ยนรูปแบบ token / ย้าย session store ออกจาก in-memory map

## 1. Current State (สำรวจโค้ดจริง 2026-07-10)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `HandleLogin` ส่ง token ใน body | ต้องแก้ | `backend/internal/api/handlers.go:211` (`model.LoginResponse{Token: token, ...}`) |
| `Set-Cookie` ใน login | เสร็จแล้ว (จาก #27) | `handlers.go:201` — HttpOnly, `Secure: r.TLS != nil`, SameSite=Strict, 24h |
| `LoginResponse.Token` | ต้องลบ field | `backend/internal/model/types.go:52` |
| `AuthMiddleware` รับ Bearer + cookie | ต้องตัด Bearer | `backend/internal/api/middleware.go:96-107` |
| `HandleLogout` รับ Bearer + cookie | ต้องตัด Bearer | `handlers.go:221-230` (ลบ cookie อยู่แล้วที่ :238) |
| CORS อนุญาต header `Authorization` | ต้องเอาออก | `middleware.go:79` (`Allow-Credentials: true` มีแล้วที่ :75) |
| fetch hook แนบ Bearer จาก localStorage | ต้องแก้เป็น `credentials` | `frontend/src/services/config.ts:27-36` (auto-redirect 401 อยู่ที่ :40-46) |
| `authService` เก็บ token ลง localStorage | ต้องแก้ | `frontend/src/services/authService.ts:16-25` (`storeSession`), `:61` (`data.token`), `:83-85` (`getToken`) |
| Route guards อ่าน `pigate_session` ตรงๆ | ต้องแก้ | `frontend/src/App.tsx:30, :54, :71` |
| SSE ส่ง token เป็น query string | ต้องแก้ | `frontend/src/services/dashboardService.ts:280-284` — backend **ไม่เคยอ่าน** `?token=` เลย (grep ทั้ง backend ไม่พบ) ใช้ได้ทุกวันนี้เพราะ cookie same-origin |
| `HandleLogStream` ตั้ง `Access-Control-Allow-Origin: *` | ต้องลบ | `handlers.go:2110` — ขัดกับ credentialed request (ดู Caution 3) |
| Backend tests ใช้ Bearer header | ต้องแก้ | `handlers_test.go` (~15 จุด), `users_test.go` (~10 จุด), `qos_test.go` (~6 จุด) — ส่วนใหญ่สร้าง session ผ่าน `AddSession()` ตรงๆ มีแค่ `handlers_test.go:102-119` ที่อ่าน `LoginResponse.Token` จาก body จริง |
| OpenAPI | ต้องแก้ทั้ง 2 ไฟล์ | `docs/openapi.yaml:52` (token ใน 200), `:2498` (`BearerAuth` scheme) — `frontend/public/openapi.yaml` เป็นสำเนาตรงกัน (diff แล้ว SAME) |
| kernel / service / db layer | ไม่เกี่ยว | session เป็น in-memory map ใน api layer (`middleware.go:24`) ไม่มี migration/บูต/install.sh |

สรุป: งานกระจุกอยู่ที่ `api` layer (handlers + middleware + tests) กับ frontend 4 ไฟล์
(`config.ts`, `authService.ts`, `App.tsx`, `dashboardService.ts`) — ไม่แตะ kernel/db เลย

## 2. Technical Approach

**กลไกที่เลือก: cookie-only ทั้งขาส่งและขารับ**
- ขาส่ง: ลบ `Token` ออกจาก `LoginResponse` — `Set-Cookie` เดิม (ทำครบแล้วจาก #27) ทำหน้าที่แทน
- ขารับ: `AuthMiddleware`/`HandleLogout` อ่าน cookie `pigate_session` อย่างเดียว
- Frontend ให้ browser จัดการ cookie เอง: fetch hook ใน `config.ts` ใส่
  `credentials: "include"` กับทุก request ที่ยิง `/api/` (production เป็น same-origin อยู่แล้ว
  แต่ต้อง `include` เพื่อ dev mode ข้าม origin) และ EventSource ใช้ `{ withCredentials: true }`
- สถานะ "ล็อกอินอยู่ไหม" ฝั่ง JS ใช้ localStorage key ใหม่ `pigate_logged_in` (ค่า `"true"`
  ไม่ใช่ความลับ — เป็นแค่ hint ให้ route guard; ของจริงตัดสินโดย cookie + `/auth/session`)

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *เก็บ Bearer path ไว้ใน middleware เผื่อทดสอบด้วย curl* — ตัดทิ้ง: เมื่อ token ไม่ถูกส่งใน
   body แล้ว Bearer path เป็น dead code ที่ขยาย attack surface เปล่าๆ; curl ใช้ cookie jar
   (`-c`/`-b`) ได้ (ดู Caution 7)
2. *ส่ง token ใน body ต่อไปแต่ให้ frontend เก็บใน memory (ตัวแปร JS) แทน localStorage* —
   ตัดทิ้ง: ยังโดน XSS อ่านได้ (อยู่ใน JS heap), refresh หน้าแล้ว session หาย และซับซ้อนกว่า
   cookie ที่ browser จัดการให้ฟรี
3. *ใช้ query param `?token=` สำหรับ SSE ต่อ* — ตัดทิ้ง: backend ไม่เคยรองรับอยู่แล้ว
   และ token ใน URL รั่วลง log/history ง่าย; EventSource ส่ง cookie ได้เอง

**Pattern ที่ยึด:** โครง `Set-Cookie`/`r.TLS` ที่ทำไว้แล้วใน `handlers.go:197-209` (จาก #27)
และสไตล์ middleware เดิมใน `middleware.go`

## 3. Steps (เรียงชั้นในสุด → นอกสุด)

### Step 1 — ลบ `Token` ออกจาก `LoginResponse`
**File:** `backend/internal/model/types.go:50-55`
ลบ field `Token string \`json:"token"\`` — เหลือ `MustChangePassword`, `Role`

### Step 2 — `HandleLogin` เลิกส่ง token
**File:** `backend/internal/api/handlers.go:211-215`
`writeJSON` เหลือ `model.LoginResponse{MustChangePassword: ..., Role: ...}` — ส่วนสร้าง token,
`AddSession`, `Set-Cookie` (บรรทัด 189-209) คงเดิมทั้งหมด

### Step 3 — ตัด Bearer path ฝั่งรับ
**File:** `backend/internal/api/middleware.go:95-99` — ลบบล็อก `Authorization` header,
เหลือ `r.Cookie(SessionKey)` ทางเดียว
**File:** `backend/internal/api/handlers.go:221-224` — `HandleLogout` เช่นกัน
**File:** `backend/internal/api/middleware.go:79` — ลบ `Authorization` ออกจาก
`Access-Control-Allow-Headers` (เหลือ `Content-Type, X-Requested-With`)

### Step 4 — ลบ `Access-Control-Allow-Origin: *` ใน SSE handler
**File:** `backend/internal/api/handlers.go:2110`
ลบบรรทัดนี้ทิ้ง — CORS ให้ `CORSMiddleware` จัดการที่เดียว (ดู Caution 3)

### Step 5 — แก้ backend tests เป็น cookie
**File:** `backend/internal/api/handlers_test.go`, `users_test.go`, `qos_test.go`
- แทน `req.Header.Set("Authorization", "Bearer "+token)` ทุกจุดด้วย
  `req.AddCookie(&http.Cookie{Name: SessionKey, Value: token})`
  (พิจารณาเพิ่ม helper `func addSessionCookie(req *http.Request, token string)` ใน test file เดียวเพื่อลดซ้ำ)
- `handlers_test.go:102-119` (test ที่ login จริงแล้วอ่าน `loginRes.Token`): เปลี่ยนเป็นอ่าน
  cookie จาก response — `rec.Result().Cookies()` หา `pigate_session`
- เพิ่ม assertion ใหม่: login response body **ต้องไม่มี** field `token` และมี `Set-Cookie`

### Step 6 — OpenAPI ทั้งสองไฟล์
**File:** `docs/openapi.yaml` และ `frontend/public/openapi.yaml` (แก้ให้เหมือนกัน)
- `:52` ลบ `token` ออกจาก 200 schema ของ `/auth/login`; เพิ่มคำอธิบายว่า session มากับ
  `Set-Cookie: pigate_session=...`
- `:2498` เปลี่ยน `BearerAuth` → `cookieAuth` (`type: apiKey, in: cookie, name: pigate_session`)
  และไล่แก้จุดที่อ้าง `BearerAuth` (grep ก่อน)

### Step 7 — fetch hook: จาก Bearer → credentials
**File:** `frontend/src/services/config.ts:27-36`
แทนบล็อกอ่าน localStorage/แนบ Bearer ด้วย `init = { ...init, credentials: "include" }`
สำหรับ URL ที่มี `/api/`; ส่วน auto-redirect 401 (`:40-46`) เปลี่ยน key ที่ลบเป็น
`pigate_logged_in` + keys อื่นๆ ของ session state

### Step 8 — `authService.ts`: เลิกยุ่งกับ token
**File:** `frontend/src/services/authService.ts`
- `storeSession()` เลิกรับ/เก็บ token → เซ็ต `pigate_logged_in = "true"` แทน `SESSION_KEY`
- `login()`: ของจริงเลิกอ่าน `data.token` (ยังอ่าน `data.role`/`data.mustChangePassword`);
  mock mode เก็บ flag แทน token เช่นกัน (พฤติกรรม UI เดิม)
- ลบ `getToken()` (ผู้ใช้ภายนอกมีแค่ `config.ts`/`dashboardService.ts` ซึ่งแก้ใน Step 7/9 อยู่แล้ว)
- `isAuthenticated()` เช็ค `pigate_logged_in`
- `checkSession()`: เลิก gate ด้วย token ใน localStorage — ยิง `GET /auth/session` ตรงๆ
  (cookie ตัดสิน) แล้ว sync flag/role/username ตามผล; 401 → `clearSession()`
- `clearSession()` ลบ key ใหม่ทั้งหมด **และลบ key เก่า `pigate_session` ทิ้งด้วย** (legacy cleanup)
- เพิ่ม one-time cleanup ตอน module load: `localStorage.removeItem("pigate_session")`
  (เครื่องที่ login ค้างจากเวอร์ชันก่อนจะมี token ตกค้าง — ดู Caution 6)

### Step 9 — SSE เลิกส่ง token ใน URL
**File:** `frontend/src/services/dashboardService.ts:279-284`
ลบการอ่าน token + `?token=` ทิ้ง; สร้าง `new EventSource(url, { withCredentials: true })`

### Step 10 — Route guards ใน `App.tsx`
**File:** `frontend/src/App.tsx:30, :54, :71`
เปลี่ยน `localStorage.getItem("pigate_session")` → `authService.isAuthenticated()`
(import จาก service เดียวกัน ไม่อ่าน key ตรงๆ อีก)

> **สิ่งที่ไม่ต้องทำ:** ไม่มี kernel interface / mock kernel / DB migration / `install.sh` /
> netlink monitor / boot-apply — session อยู่ใน memory ของ api layer ล้วนๆ
> `HandleLogin`'s `Set-Cookie` ก็เสร็จอยู่แล้วจากงาน HTTPS (#27)

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| POST | `/api/auth/login` | public (rate-limited) | **response เปลี่ยน**: ไม่มี `token` — เหลือ `mustChangePassword`, `role` + `Set-Cookie` |
| POST | `/api/auth/logout` | public | รับเฉพาะ cookie (เดิมรับ Bearer ด้วย) |
| GET | `/api/auth/session` | authRoute | พฤติกรรมเดิม แต่ auth ผ่าน cookie เท่านั้น |
| GET | `/api/dashboard/logs/stream` | authRoute | เลิกตั้ง `Access-Control-Allow-Origin: *` ใน handler |
| ทุก endpoint อื่น | — | — | เปลี่ยนเฉพาะวิธีแนบ credential (cookie แทน Bearer) — ไม่มี route ใหม่ |

`-disable-edit` mode: ไม่กระทบ — `DisableEditMiddleware` (middleware.go:277) ยกเว้น
login/logout อยู่แล้ว และไม่ได้ผูกกับวิธีแนบ token

## 5. Cautions

1. **Dev mode ข้าม origin ต้องมี `credentials: "include"` ครบทุก request** — ถ้าพลาด request
   ไหน (fetch ที่ไม่ผ่าน hook ใน `config.ts`) จะได้ 401 เฉพาะ dev ทั้งที่ production ปกติ
   (same-origin ส่ง cookie เองเมื่อ `credentials` เป็นค่า default `same-origin`)
   → ทาง hook กลางครอบทุก URL ที่มี `/api/` อยู่แล้ว ให้คงจุดแก้ไว้ที่ hook จุดเดียว อย่าไปแก้ราย service
2. **SameSite=Strict ไม่พังใน dev** — `http://localhost:5173` → `http://localhost:8081`
   ถือเป็น same-site (SameSite ไม่สน port/scheme ต่างกันของ localhost) จึงไม่ต้องลดเป็น Lax;
   แต่ถ้า dev เปิด frontend จาก IP อื่น (`192.168.x.x:5173` → backend อีกเครื่อง) cookie จะไม่ถูกส่ง —
   ข้อจำกัดนี้มีอยู่แล้วเดิมกับ CORS whitelist (middleware.go:73) ไม่ใช่ regression ใหม่
3. **`Access-Control-Allow-Origin: *` + credentialed EventSource = พังทันทีใน dev** —
   browser ปฏิเสธ response ที่ `ACAO: *` เมื่อ `withCredentials: true` ทำให้ SSE เงียบหาย
   → ต้องลบ `handlers.go:2110` (Step 4) พร้อมกันกับ Step 9 เสมอ ห้ามทำแค่ฝั่งเดียว
   (`CORSMiddleware` ตอบ origin เจาะจง + `Allow-Credentials: true` ให้อยู่แล้ว)
4. **ลำดับใน `checkSession()` เปลี่ยน** — เดิม gate ด้วย token ใน localStorage ก่อนยิง network;
   ถ้าเปลี่ยนเป็นยิงทุกครั้งตอน boot หน้า จะมี request `/auth/session` เพิ่มตอนเปิดหน้า login
   (ยังไม่ล็อกอิน → 401) — ไม่อันตราย แต่ควรกันซ้ำ: ใน `App.tsx` ยังเช็ค `isAuthenticated()`
   (flag) ก่อนเรียก `checkSession()` เหมือน logic เดิมที่ :71
5. **Flag `pigate_logged_in` desync ได้** — เช่น cookie หมดอายุ (24h) แต่ flag ยังอยู่ →
   guard ปล่อยผ่านเข้า page แล้ว API แรกตอบ 401 → hook `config.ts` ล้าง state + เด้งไป
   `/login` (พฤติกรรมเดียวกับปัจจุบันเมื่อ token ใน localStorage ตาย) — ยืนยันว่า path :40-46
   ยังทำงานหลังแก้ key
6. **เครื่องที่ login ค้างจากเวอร์ชันเก่า**: มี `pigate_session` ใน localStorage + cookie เดิม —
   Bearer จาก token เก่าจะโดนปฏิเสธ (middleware ไม่อ่าน header แล้ว) แต่ cookie เดิมยังใช้ได้
   จน restart backend → UX ไม่สะดุด แค่ต้องลบ key เก่าทิ้ง (Step 8 ข้อสุดท้าย) กัน token
   ค้างเป็นขยะ/หลุดผ่าน XSS ย้อนหลัง
7. **Workflow ทดสอบมือด้วย curl เปลี่ยน** — เดิม copy token จาก login body มาใส่ `-H`;
   ต่อไปใช้ `curl -k -c jar.txt -X POST https://<ip>/api/auth/login -d '{...}'` แล้วตามด้วย
   `curl -k -b jar.txt ...` — จดไว้เพราะ docs/สคริปต์ทดสอบเดิมที่อ้าง Bearer จะใช้ไม่ได้
   (สแกน `docs/frontend_data_testing_guide.md` ตอนแก้ docs ด้วย — ยังไม่ได้เช็คว่ามีตัวอย่าง Bearer ไหม)
8. **ทดสอบบนอุปกรณ์จริง**: งานนี้ไม่แตะ firewall/routing จึงไม่มีความเสี่ยง lock-out ระดับ network
   แต่ถ้า deploy แล้ว login ไม่ได้จะแก้ config ไม่ได้ทั้งระบบ → ทดสอบ login/logout ให้ผ่านใน
   mock mode + embedded build (`bash build.sh`) บน WSL ก่อนอัปโหลดขึ้นเครื่องจริง
   (ผู้ใช้เป็นคน deploy เอง)

## 6. Summary Checklist (Definition of Done)

- [ ] `model/types.go` — ลบ `Token` จาก `LoginResponse`
- [ ] `handlers.go` — `HandleLogin` ไม่ส่ง token ใน body
- [ ] `middleware.go` — `AuthMiddleware` เช็ค cookie ทางเดียว, CORS headers ไม่มี `Authorization`
- [ ] `handlers.go` — `HandleLogout` เลิกอ่าน Bearer; `HandleLogStream` เลิกตั้ง `ACAO: *`
- [ ] Backend tests: Bearer → `AddCookie` ทุกจุด + test ใหม่ยืนยัน body ไม่มี `token` และมี `Set-Cookie`
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] `frontend/src/services/config.ts` — hook ใช้ `credentials: "include"`, 401 redirect ล้าง key ใหม่
- [ ] `frontend/src/services/authService.ts` — flag `pigate_logged_in`, ไม่มี `getToken`, legacy cleanup
- [ ] `frontend/src/services/dashboardService.ts` — EventSource `withCredentials`, ไม่มี `?token=`
- [ ] `frontend/src/App.tsx` — guards ใช้ `authService.isAuthenticated()`
- [ ] `yarn build` + `yarn lint` ผ่าน (ใน `frontend/`)
- [ ] ทดสอบ mock mode: login → force-change-password flow → ใช้งาน page ต่างๆ → logout;
      role read-only ยังโดนบล็อกการแก้ไข; SSE log stream ต่อติด
- [ ] ทดสอบ dev ข้าม origin: `yarn dev` (5173) + backend `-mock=true` (8081) — login ได้,
      ทุก page โหลดข้อมูลได้, SSE ทำงาน, logout แล้ว cookie หาย
- [ ] ทดสอบ embedded build: `bash build.sh` แล้วเปิดผ่าน HTTPS self-signed — devtools ต้องไม่เห็น
      token ใน response/localStorage เลย
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — sync ทั้งคู่ (login 200 schema + cookieAuth)
- [ ] เช็ค `docs/frontend_data_testing_guide.md` ว่ามีตัวอย่าง Bearer ต้องอัปเดตไหม
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/` + อัปเดต security review artifact (finding 2 → done)
