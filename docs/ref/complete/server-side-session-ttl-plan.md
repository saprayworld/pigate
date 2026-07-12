# Server-Side Session TTL — session หมดอายุฝั่ง server ได้จริง + inactivity logout 5 นาที

> แผนงานแก้ security review finding 3 (Medium): ปัจจุบัน `activeSessions` เป็น
> `map[token]username` ที่**ไม่มีวันหมดอายุ** — token ที่หลุด (log/sniff/XSS) ใช้ได้ตลอดไป
> จนกว่าจะ restart process แม้ cookie ฝั่ง browser จะหมดอายุใน 24h แล้วก็ตาม
> เป้าหมาย: `expiresAt` ต่อ session + sweeper + sliding renewal + per-user cap
> **+ inactivity auto-logout 5 นาที** (เพิ่ม requirement 2026-07-11 — สไตล์ FortiGate admintimeout)
>
> เขียนเมื่อ: 2026-07-11 · Reference branch: `feat/server-side-session-ttl` · Issue: #32
> ต่อยอดจาก cookie-only auth (#29/#30) — งานลำดับ 3 ใน remediation roadmap ของ security review

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- **ผู้ใช้ไม่ขยับเมาส์/คีย์บอร์ด 5 นาที → ถูก logout อัตโนมัติ** (token ถูกเพิกถอนฝั่ง server
  ทันทีผ่าน `/auth/logout` แล้วเด้งไปหน้า login) — ทำงานถูกต้องแม้เปิดหลาย tab
- ทุก session มี `expiresAt` ฝั่ง server — idle TTL **15 นาที** แบบ sliding เป็น backstop:
  browser ที่ถูกปิด/crash โดยไม่ logout จะหมดอายุเองใน ≤15 นาที (poll หยุด → ไม่มีการต่ออายุ)
- Absolute cap **7 วัน** นับจาก login — ต่ออายุเกินนี้ไม่ได้ ต้อง login ใหม่
- Sweeper goroutine เก็บกวาด entry หมดอายุ (token ที่ไม่มีใครยิงซ้ำไม่ค้างใน map ตลอดไป)
- จำกัด **5 sessions ต่อ user** — login ที่ 6 เตะ session เก่าสุดออก
- พฤติกรรมเดิมคงครบ: logout, purge ตอนลบ/ปิด user, per-request DB check, force change password

**Out of scope (ตัดออกชัดเจน):**
- ตั้งค่า idle timeout ผ่าน UI/flag (ค่าคงที่ในโค้ดก่อน — ทำเป็น setting ได้ภายหลังบนโครงนี้)
- Warning dialog นับถอยหลังก่อนโดนเด้ง (polish ภายหลัง — ครั้งนี้เด้งตรงไปหน้า login เลย)
- Rate-limiter eviction (finding 5), security headers/CSP (finding 6) — งานแยก
- Persist session ลง SQLite / หน้า UI จัดการ active sessions รายตัว

## 1. Current State (สำรวจโค้ดจริง 2026-07-11)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Session store ไม่มี expiry | ต้องแก้ | `backend/internal/api/middleware.go:22-25` — `activeSessions = map[string]string{}` |
| `AddSession` / `RemoveSession` / `RemoveSessionsForUser` | ต้องแก้ type (logic เดิมใช้ได้) | `middleware.go:27-66` — ผู้เรียก purge: `handlers.go:1906` (ลบ user), `:1924` (ปิด user), `:2068` (import) |
| `IsSessionValid` + `GetUsernameByToken` เช็ค 2 จังหวะ | ต้องรวมเป็น `ValidateSession` เดียว | `middleware.go:39-52`, ใช้ใน `AuthMiddleware` `:100`, `:108` |
| `HandleLogin` สร้าง token + `Set-Cookie` 24h hardcode | ต้องใช้ค่าคงที่ร่วม + helper | `backend/internal/api/handlers.go:189-209` |
| `HandleLogout` เพิกถอน token ฝั่ง server แล้ว | ใช้เป็นกลไก idle logout ได้เลย | `handlers.go:217-242` — `RemoveSession(token)` ที่ :225 |
| Sweeper goroutine | ยังไม่มี | pattern: `backend/internal/service/event_log.go:82-101` (`Start(ctx)` + ticker + `ctx.Done()`) |
| จุด start goroutine ใน main | มี ctx พร้อมใช้ | `backend/cmd/pigate/main.go:193` (`monitorCtx`), `:217` (`eventLogService.Start(monitorCtx)`) |
| **Frontend poll อัตโนมัติหลายจุด** | เหตุที่ server-side 5 นาทีล้วนๆ ใช้ไม่ได้ | `site-header.tsx:48` (perf ทุก 5s **ทุกหน้า**), `Dashboard.tsx:610-618` (poll `/api/dashboard/*`, `/api/interfaces`, `/api/system/info`), `EventLogs.tsx:168` (10s), `ForwardTraffic.tsx:123` (5s) |
| จุด mount hook ฝั่ง frontend | มี layout กลางครอบทุกหน้า protected | `frontend/src/App.tsx:130-134` — `ProtectedRoute` ครอบ `ShellLayout` (`frontend/src/components/layout/ShellLayout.tsx`) |
| `authService.logout()` | พร้อมใช้จาก hook ใหม่ | `frontend/src/services/authService.ts:80-89` — ยิง `/auth/logout` + `clearSession()` |
| Frontend รับมือ 401 กลางคัน | เสร็จแล้ว | `frontend/src/services/config.ts:39-46` — fetch hook ล้าง UI hints + เด้ง `/login` |
| Backend tests สร้าง session ผ่าน `AddSession()` | signature เดิมคงไว้ → ไม่พัง | `handlers_test.go:50, :332, :734, :1185`, `users_test.go:31, :150` |
| OpenAPI | ต้องอัปเดต description 2 ไฟล์ | `docs/openapi.yaml:48-55` (login), `:81-87` (`/auth/session`) — `frontend/public/openapi.yaml` สำเนา |
| kernel / service / db layer | ไม่เกี่ยว | session เป็น in-memory ใน api layer — ไม่มี migration / boot-apply / install.sh |

สรุป: backend อยู่ใน api layer ล้วน (store + middleware + handlers + tests) + 1 บรรทัดใน `main.go`;
frontend เพิ่ม hook ใหม่ 1 ไฟล์ + mount 1 จุดใน `ShellLayout` — ไม่แตะ service อื่น

## 2. Technical Approach

**กลไกที่เลือก: hybrid — frontend idle timer (5 นาที) เป็นตัวตัดสิน "คนไม่อยู่" + server TTL (15 นาที) เป็น backstop**

เหตุผลหลัก: `site-header.tsx:48` poll `/api/dashboard/performance` ทุก 5 วินาทีบน**ทุกหน้า**
— server แยกไม่ออกว่า request มาจากมนุษย์หรือ poller ดังนั้น idle TTL ฝั่ง server 5 นาที
แบบเลื่อนทุก request จะไม่มีวันหมดอายุตราบใดที่ tab เปิดอยู่ ตัวที่รู้ว่า "มนุษย์ไม่อยู่"
มีแค่ frontend (input events) จึงให้ frontend เป็นคนสั่ง terminate ผ่าน `/auth/logout`
(ซึ่ง `RemoveSession` ฝั่ง server ทันที — เป็น server-side revocation จริง ไม่ใช่แค่ล้าง UI)

```go
// ฝั่ง server (backend/internal/api/session.go)
type sessionEntry struct {
    username  string
    createdAt time.Time // absolute cap + หา session เก่าสุดตอนเตะ
    expiresAt time.Time // idle deadline (sliding)
}

const (
    sessionTTL         = 15 * time.Minute   // backstop เมื่อ browser ปิด/crash (poll หยุด)
    sessionAbsoluteMax = 7 * 24 * time.Hour // นับจาก createdAt ไม่ต่อให้เกินนี้
    sessionRenewAfter  = sessionTTL / 2     // ต่ออายุเมื่อเหลือ < 7.5 นาที (กัน Set-Cookie ถี่)
    sweepInterval      = 5 * time.Minute
    maxSessionsPerUser = 5
)
```

```ts
// ฝั่ง frontend (useIdleLogout) — cross-tab ผ่าน localStorage
const IDLE_LOGOUT_MS = 5 * 60_000
// ทุก input event (throttle ~5s) → localStorage.setItem("pigate_last_activity", Date.now())
// interval ทุก 15s → ถ้า now - last > IDLE_LOGOUT_MS → authService.logout() → /login
```

- **`ValidateSession(token)`** รวม validate + renew ใน lock เดียว: หมดอายุ → ลบ + 401;
  เหลือ < 7.5 นาที และไม่ชน absolute cap → เลื่อน `expiresAt` แล้วให้ `AuthMiddleware`
  re-issue cookie (write-lock เฉพาะตอน renew — validation ปกติเป็น read-lock)
- **Sweeper**: ticker 5 นาที กวาด entry หมดอายุ — จำเป็นเพราะ lazy check ลบเฉพาะ token
  ที่มีคนยิงมา token ที่ถูกทิ้งจะไม่มีวันถูกแตะ
- **Per-user cap** ใน `AddSession`: ≥ 5 → ลบตัว `createdAt` เก่าสุด (เตะตัวเก่า ไม่ปฏิเสธตัวใหม่)
- **Cookie helper** `setSessionCookie(w, r, token, expires)` ใช้ร่วม login + renewal

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *Server-side idle 5 นาที + รายชื่อ "passive endpoints" ที่ไม่ต่ออายุ* — ตัดทิ้ง: Dashboard
   poll `/api/interfaces` และ `/api/system/info` ซึ่งเป็น path ที่ผู้ใช้กดเองด้วย แยกด้วย path
   ไม่สะอาด ต้องเพิ่ม marker header ในหลาย service ฝั่ง frontend พลาดจุดเดียว = โดนเด้งมั่ว
   หรือ session อมตะ; และ sliding renewal แบบใดก็กัน attacker ที่ถือ token แล้วยิงต่ออายุเอง
   ไม่ได้อยู่แล้ว (absolute cap เป็นตัวจำกัด) — server-enforced 5 นาทีจึงไม่เพิ่มความปลอดภัยจริง
2. *Persist session ลง SQLite ให้รอด restart* — ตัดทิ้ง: ขัด SD-card preservation, เพิ่ม write
   ทุก renewal และ "restart = ล้าง session" เป็น fail-safe ที่ดีอยู่แล้ว
3. *JWT / self-contained token ฝัง exp* — ตัดทิ้ง: revoke ฝั่ง server ไม่ได้ ทำให้
   `RemoveSessionsForUser` (ลบ/ปิด user) หมดความหมาย; per-request DB check เดิมแข็งแรงกว่า
4. *Sliding ทุก request (Set-Cookie ทุกครั้ง)* — ตัดทิ้ง: write-lock + header ซ้ำซากโดยไม่จำเป็น
   threshold ครึ่ง TTL ให้ผลเท่ากัน
5. *Idle timer แบบ per-tab (ตัวแปรใน memory ไม่ใช่ localStorage)* — ตัดทิ้ง: เปิด 2 tab แล้ว
   tab ที่ไม่ได้ใช้ครบ 5 นาทีจะสั่ง logout ตัด session ของ tab ที่กำลังใช้งานอยู่ด้วย
   (cookie ตัวเดียวกัน) → ต้อง share timestamp ผ่าน localStorage เท่านั้น

**Pattern ที่ยึด:** goroutine + ticker + `ctx.Done()` ตาม `event_log.go:82-101`;
โครง cookie จาก `handlers.go:201-209`; สไตล์ hook จาก `usePowerControl.tsx`

## 3. Steps (เรียงชั้นในสุด → นอกสุด)

### Step 1 — สร้าง `session.go` แยก session store ออกจาก `middleware.go`
**File:** `backend/internal/api/session.go` (ไฟล์ใหม่)
ย้าย `sessionMutex`, `activeSessions`, `AddSession`, `RemoveSession`, `RemoveSessionsForUser`
จาก `middleware.go:21-66` มา แล้วปรับเป็น `map[string]*sessionEntry` + ค่าคงที่ตาม §2
- `AddSession(token, username)` — **signature เดิม** (tests เรียก ~8 จุด) ภายในเซ็ต
  `createdAt`/`expiresAt` + บังคับ `maxSessionsPerUser`
- เพิ่ม `ValidateSession(token)`; ลบ `IsSessionValid`/`GetUsernameByToken` เดิม
  (ผู้เรียกมีแค่ `AuthMiddleware` — grep ยืนยันแล้ว)
- Test helper ใน package: `addSessionWithTimes(token, username, createdAt, expiresAt)`

### Step 2 — Sweeper goroutine
**File:** `backend/internal/api/session.go`
`sweepExpiredSessions()` (logic แยกให้ test เรียกตรงได้) + `StartSessionSweeper(ctx)`
ticker `sweepInterval` ตาม pattern `event_log.go:82-101` — log จำนวนที่กวาดเมื่อ > 0

### Step 3 — `AuthMiddleware` ใช้ `ValidateSession` + จุด renewal
**File:** `backend/internal/api/middleware.go:93-148`
แทน `IsSessionValid` (:100) + `GetUsernameByToken` (:108) ด้วย `ValidateSession` ครั้งเดียว;
ถ้า renew → `setSessionCookie(w, r, token, newExpiry)` **ก่อน** `next.ServeHTTP` (Caution 3)
ส่วน DB check / force-change-password (:120-144) คงเดิม

### Step 4 — Cookie helper + `HandleLogin` ใช้ค่าคงที่ร่วม
**File:** `backend/internal/api/handlers.go:197-209`
แยก `Set-Cookie` block เป็น `setSessionCookie(w, r, token, expires)` (วางใน `session.go`)
`HandleLogin` เรียกด้วย `time.Now().Add(sessionTTL)` — เลิก hardcode `24 * time.Hour`
> **สิ่งที่ไม่ต้องทำ:** `HandleLogout` (:230-239) คง cookie-ลบทิ้ง (MaxAge -1) แบบเดิม;
> `HandleCheckSession` ไม่แก้ — ผ่าน `AuthMiddleware` จึงได้ renewal ฟรี

### Step 5 — start sweeper ใน `main.go`
**File:** `backend/cmd/pigate/main.go:~217`
เพิ่ม `api.StartSessionSweeper(monitorCtx)` ถัดจาก `eventLogService.Start(monitorCtx)`

### Step 6 — Backend tests
**File:** `backend/internal/api/session_test.go` (ไฟล์ใหม่) หรือรวมใน `handlers_test.go`
- token หมดอายุ → 401 + entry ถูกลบ / token เหลือ < 7.5 นาที → `Set-Cookie` ใหม่ + `expiresAt` ขยับ
- token ชน absolute cap → ไม่ renew / login 6 ครั้ง user เดียว → เหลือ 5 ตัวเก่าสุดหาย
- `sweepExpiredSessions()` → entry หมดอายุหาย
- tests เดิมผ่านโดยไม่แก้ (AddSession signature เดิม)

### Step 7 — Frontend hook `useIdleLogout`
**File:** `frontend/src/hooks/useIdleLogout.ts` (ไฟล์ใหม่ — สไตล์เดียวกับ `usePowerControl.tsx`)
- Listen `mousemove` `mousedown` `keydown` `touchstart` `wheel` บน `window` → เขียน
  `localStorage.pigate_last_activity = Date.now()` (throttle ~5 วินาที กันเขียนถี่)
- `setInterval` ทุก 15 วินาที: ถ้า `Date.now() - last > 5 * 60_000` →
  `authService.logout()` แล้ว `window.location.href = "/login"` (ทางเดียวกับ `config.ts:45`)
- cleanup listeners + interval ตอน unmount; เขียน timestamp เริ่มต้นตอน mount
  (กันกรณี key ว่างแล้วโดนเด้งทันที)

### Step 8 — mount hook ใน `ShellLayout`
**File:** `frontend/src/components/layout/ShellLayout.tsx`
เรียก `useIdleLogout()` บนสุดของ component — `ShellLayout` ถูกครอบด้วย `ProtectedRoute`
(`App.tsx:130-134`) จึงทำงานเฉพาะตอน login แล้ว ครอบทุกหน้าในครั้งเดียว
> **สิ่งที่ไม่ต้องทำ:** ไม่มี kernel interface / mock / DB migration / `install.sh` / boot-apply /
> backup schema; ไม่ต้องแก้ fetch hook (`config.ts`) — 401 auto-redirect รองรับ session
> หมดอายุกลางคันอยู่แล้ว; หน้า Login/ChangePassword ไม่ mount hook (ไม่มี session ให้ terminate)

### Step 9 — OpenAPI ทั้งสองไฟล์
**File:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` (แก้ให้เหมือนกัน)
- `:48-55` (login 200): idle TTL 15 นาที (sliding), absolute 7 วัน, สูงสุด 5 sessions/user,
  UI auto-logout เมื่อ inactive 5 นาที
- `:81-87` (`/auth/session`): 401 เมื่อ session หมดอายุฝั่ง server ด้วย

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| POST | `/api/auth/login` | public (rate-limited) | session ได้ TTL; เกิน 5 sessions เตะตัวเก่าสุด |
| POST | `/api/auth/logout` | public | ไม่เปลี่ยน — แต่ถูกเรียกเพิ่มโดย idle timer |
| GET | `/api/auth/session` | authRoute | ไม่เปลี่ยน (ได้ renewal ผ่าน middleware) |
| ทุก endpoint หลัง auth | authRoute | อาจได้ `Set-Cookie` (renewal) และ 401 เมื่อหมดอายุ — ไม่มี route ใหม่ |

`-disable-edit` mode: ไม่กระทบ — `DisableEditMiddleware` (middleware.go:268) ยกเว้น
login/logout อยู่แล้ว idle logout จึงทำงานได้ใน read-only mode ด้วย

## 5. Cautions

1. **เปิดหลาย tab แล้ว timestamp ต้อง share ผ่าน localStorage เท่านั้น** — cookie เป็นตัวเดียว
   ร่วมกันทุก tab ถ้า timer นับ per-tab tab ที่ถูกทิ้งครบ 5 นาทีจะสั่ง logout ตัด session
   ของ tab ที่ใช้งานอยู่ → ทุก tab เขียน/อ่าน `pigate_last_activity` key เดียวกัน
   (localStorage มองเห็นข้าม tab ใน origin เดียวกันโดยธรรมชาติ ไม่ต้องใช้ storage event)
2. **อย่าลืมเขียน timestamp ตอน mount hook** — ถ้า key ไม่มี/เก่าค้างจาก session ก่อน
   (`Date.now() - last` มหาศาล) ผู้ใช้จะโดนเด้งทันทีที่ login เสร็จ → mount แล้ว seed ค่า now ก่อนเสมอ
3. **Header `Set-Cookie` (renewal) ต้องถูกเซ็ตก่อน `next.ServeHTTP`** — เซ็ตหลัง handler
   `WriteHeader` แล้ว header หายเงียบๆ → cookie ฝั่ง browser หมดอายุก่อน server, debug ยาก
4. **Renewal cookie ต้อง attributes เหมือน login cookie ทุกตัว** (Path, HttpOnly,
   `Secure: r.TLS != nil`, SameSite=Strict) — ต่างกันแล้ว browser มองเป็นคนละ cookie เกิดค่าซ้อน
   → ใช้ helper `setSessionCookie` จุดเดียวทั้งสอง path ห้าม copy
5. **หน้าเฝ้าดู log (EventLogs / ForwardTraffic / Dashboard) จะโดนเด้งถ้าไม่แตะเครื่อง 5 นาที
   ทั้งที่จอกำลังอัปเดต** — เป็นพฤติกรรมที่ตั้งใจ (FortiGate ก็เด้ง) แต่ต้องบอกผู้ใช้/จดใน docs;
   ถ้าภายหลังอยากยกเว้น ให้ทำเป็น setting ไม่ใช่แก้ hook เฉพาะหน้า (out of scope ครั้งนี้)
6. **SSE log stream เช็ค auth เฉพาะตอน connect** — session ที่ถูกเพิกถอน/หมดอายุระหว่าง
   stream เปิดจะไม่ถูกตัดทันที (reconnect ครั้งถัดไปโดน 401) — ยอมรับได้ จดกันเข้าใจผิดว่า TTL รั่ว
7. **นาฬิกา Pi ไม่มี RTC battery** — เวลากระโดดตอน NTP sync ทำ session หมดอายุเร็วกว่าจริงได้
   → ทิศ fail-safe (เด้งไป login) ยอมรับได้ ห้ามทำ monotonic hack ให้ซับซ้อน
8. **อย่าเปลี่ยน signature `AddSession(token, username)`** — tests เดิมเรียก ~8 จุด
   (`handlers_test.go:50, :332, :734, :1185`, `users_test.go:31, :150`); การฉีดเวลาใช้
   `addSessionWithTimes` เฉพาะใน test
9. **Per-user cap ต้องเตะตัวเก่าสุด ไม่ใช่ปฏิเสธ login ใหม่** — ปฏิเสธแล้วแอดมินที่ลืม logout
   หลายเครื่องจะเข้าไม่ได้เลยจนกว่าจะ restart (ล็อกตัวเองออกจากกล่อง)
10. **Throttle การเขียน localStorage จาก `mousemove`** — event ยิงถี่ระดับต่อ pixel
    เขียนทุกครั้งจะเปลืองเปล่าๆ → เขียนอย่างมากทุก ~5 วินาทีพอ (ความละเอียดที่เสียไป
    ไม่มีผลกับ threshold 5 นาที)
11. **ทดสอบบนอุปกรณ์จริง**: ไม่แตะ firewall/routing — ไม่มีความเสี่ยง network lock-out
    แต่ auth พังแล้วแก้ config ไม่ได้ทั้งระบบ → ผ่าน mock mode + embedded build
    (`bash build.sh`) บน WSL ก่อน แล้วผู้ใช้ deploy เอง (workflow เดิม)

## 6. Summary Checklist (Definition of Done)

- [ ] `backend/internal/api/session.go` — `sessionEntry` + ค่าคงที่, `AddSession` (คง signature
      + cap), `ValidateSession`, `sweepExpiredSessions`, `StartSessionSweeper`, `setSessionCookie`
- [ ] `backend/internal/api/middleware.go` — ตัด store เดิม; `AuthMiddleware` ใช้
      `ValidateSession` + renewal ก่อน `next.ServeHTTP`
- [ ] `backend/internal/api/handlers.go` — `HandleLogin` ใช้ `setSessionCookie` + `sessionTTL`
- [ ] `backend/cmd/pigate/main.go` — `api.StartSessionSweeper(monitorCtx)`
- [ ] Backend tests: expired → 401 + purge / renewal / absolute cap / cap 5 ต่อ user / sweep —
      tests เดิมผ่านโดยไม่แก้
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] `frontend/src/hooks/useIdleLogout.ts` — cross-tab timestamp, throttle, seed ตอน mount,
      cleanup ตอน unmount
- [ ] `frontend/src/components/layout/ShellLayout.tsx` — mount `useIdleLogout()`
- [ ] `yarn build` + `yarn lint` ผ่าน (ใน `frontend/`)
- [ ] ทดสอบ mock mode: ทิ้งไว้ 5 นาที → เด้งไป login; เปิด 2 tab ใช้งาน tab เดียว →
      ไม่โดนเด้ง; logout ปกติ; read-only role ไม่กระทบ
      (ระหว่าง dev ลดค่าคงที่ชั่วคราวให้สั้นเพื่อไม่ต้องรอจริง)
- [ ] ทดสอบ embedded build (`bash build.sh` + HTTPS self-signed): idle 5 นาทีโดน logout,
      ปิด browser โดยไม่ logout แล้วยิง token เดิมด้วย curl หลัง 15 นาที → 401
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — sync ทั้งคู่ (TTL/renewal/cap/idle logout)
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/` + อัปเดต security review artifact
      (finding 3 → done, Session Mgmt C → B)
