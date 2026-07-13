# Security Hardening Cleanups — รวบ review findings 8–11 (Low)

> แผนงานรวบ finding ระดับ Low ทั้ง 4 ของ security review ไว้แผนเดียว (แต่ละอันเล็ก แยก PR
> ไม่คุ้ม): (8) `SendWpaCommand` log คำสั่งเต็ม → เสี่ยง PSK รั่วอนาคต, (9) `rand.Read`
> ไม่เช็ค error → token เดาได้ถ้า entropy ล่ม, (10) CORS อนุญาต dev origins ใน binary
> production, (11) audit trail — **ไม่ใช่สร้างใหม่** (ระบบ event log กลางมีแล้ว) แต่ปิดช่องว่าง
> coverage (QoS ไม่ log อะไรเลย)
>
> เขียนเมื่อ: 2026-07-11 · Reference branch: `fix/security-cleanups-8-11` · Issue: #37
> ชุดปิดท้าย security review ต่อจาก finding 3–7 (#32–#36)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- (8) log ของ wpa control socket ไม่มีทางพ่น secret (PSK) ลง journal แม้อนาคตจะมีคำสั่งที่พก secret
- (9) token/ID จาก `crypto/rand` fail-closed — entropy ล่ม = คืน error/panic ไม่ใช่ token ศูนย์
- (10) dev CORS origins (`localhost:5173/3000`) เปิดเฉพาะเมื่อสั่ง flag — binary production ปิดโดย default
- (11) ทุก mutating handler ผูก actor (username) ผ่าน `logEvent` — เริ่มจากอุดช่อง QoS ที่ตอนนี้ log 0

**Out of scope (ตัดออกชัดเจน):**
- สร้าง audit ring buffer แยก (review ข้อ 11 เสนอไว้) — **ไม่ทำ**: ระบบ event log กลาง
  (`EventLogService` + `SystemEvent.Actor`) ทำหน้าที่นี้อยู่แล้ว การเพิ่ม buffer ที่สองซ้ำซ้อน
- เพิ่ม flag/UI ตั้งค่า retention ของ event log — คนละเรื่อง
- redact log ทุกจุดในระบบ — เฉพาะ wpa command ที่เป็นผิว PSK จริง

## 1. Current State (สำรวจโค้ดจริง 2026-07-11)

| # | ส่วน | สถานะ | อ้างอิง |
|---|---|---|---|
| 8 | `SendWpaCommand` log คำสั่งเต็ม | ต้องแก้ | `backend/internal/kernel/wpa.go:153` — `log.Printf("...datagram: %q", command)`; response ก็ log ที่ `:168` |
| 8 | คำสั่งปัจจุบันไม่มี secret | ยืนยัน (ยังไม่รั่ววันนี้) | grep ผู้เรียก `SendWpaCommand` → ส่ง `RECONFIGURE`/`RECONNECT` เท่านั้น (PSK เขียนผ่าน config file ไม่ใช่ผ่าน socket) |
| 9 | `generateRandomToken` ทิ้ง error | ต้องแก้ | `backend/internal/api/handlers.go:122-126` — `_, _ = rand.Read(b)` |
| 9 | ผู้เรียก generateRandomToken | 9 จุด (token + IDs) | `handlers.go:189` (session token — critical), `:953/1099/1189/1263/1462/2277/2364` (resource IDs) |
| 10 | CORS dev origins hardcode | ต้อง gate | `backend/internal/api/middleware.go:73` — `CORSMiddleware` เป็น package func ไม่มีเงื่อนไข env |
| 10 | จุด wire CORS + pattern flag | มีแบบให้ยึด | `router.go:172` (`return CORSMiddleware(handler)`); `Server.disableEdit` field ที่ thread จาก `main.go:166` เป็น pattern เดียวกัน |
| 11 | ระบบ audit **มีแล้ว** (review claim stale) | ปิดช่องว่าง | `model/types.go:319-327` (`SystemEvent{Actor,...}`), `service/event_log.go`, `s.logEvent()` (`handlers.go:142`) เรียก 28 จุด ครอบ 9 category |
| 11 | QoS ไม่ audit เลย | ต้องเพิ่ม | grep `logEvent` ใน handler QoS ทั้งหมด = 0 (มี `EventCategoryQos` แต่ไม่มีใครใช้) |
| 11 | mutating routes vs log calls | ช่องว่าง | 65 mutating routes (`router.go`) vs 28 `logEvent` — ต้องไล่ทีละ handler หา missing |
| — | frontend / kernel mock / db / install.sh | ไม่เกี่ยว | ทั้ง 4 จบใน backend api/kernel layer; ไม่มี migration/boot/permission |

สรุป: 4 งานเล็กในไฟล์เดิม — (8) `wpa.go`, (9) `handlers.go` helper เดียว, (10) `middleware.go`+
`router.go`+`main.go` thread flag, (11) เติม `logEvent` ใน handler ที่ขาด (เริ่ม QoS)

## 2. Technical Approach

**(8) redact ก่อน log — log เฉพาะ verb ถ้าคำสั่งพก secret**
```go
// wpa.go — helper เล็กๆ
func redactWpaCommand(cmd string) string {
    // คำสั่งพก secret (มี "psk"/"password"/"wep_key") → log แค่ token แรก
    verb := strings.Fields(cmd)
    if len(verb) > 0 && containsSecretKeyword(cmd) {
        return verb[0] + " [redacted]"
    }
    return cmd
}
// :153 → log.Printf("...datagram: %q", redactWpaCommand(command))
```
ทำเชิงรับล่วงหน้า (คำสั่งวันนี้ไม่มี secret แต่กันตอนอนาคตเพิ่ม `SET_NETWORK ... psk`)

**(9) fail-closed** — session token เป็น security boundary: entropy ล่มต้องไม่ปล่อยผ่าน
```go
func generateRandomToken() (string, error) {
    b := make([]byte, 16)
    if _, err := rand.Read(b); err != nil {
        return "", err
    }
    return hex.EncodeToString(b), nil
}
```
ผู้เรียก session token (`HandleLogin`) → error = 500 ไม่ออก token; ผู้เรียก resource ID
อีก 8 จุด → คืน 500 เช่นกัน (ID ชนกัน/เดาได้ก็ไม่ควรปล่อย) — ทางเลือกดู §ทางเลือก

**(10) gate ด้วย flag ผ่าน Server field** (pattern เดียวกับ `disableEdit`)
- เพิ่ม `-allow-dev-cors` (default `false`) ใน `main.go`, ส่งเข้า `NewServer`, เก็บเป็น
  `Server.allowDevCORS`
- `CORSMiddleware` เปลี่ยนเป็น method `(s *Server) CORSMiddleware` (หรือรับ bool param) —
  เช็ค origin เฉพาะเมื่อ `s.allowDevCORS` เป็น true; production (ไม่ส่ง flag) = ไม่ echo dev origin

**(11) เติม logEvent ใน mutating handler ที่ขาด** — เริ่ม QoS (Create/Update/Delete/Toggle/Sync/Clear)
ด้วย `EventCategoryQos`; แล้วไล่ตรวจ handler อื่นให้ครบ ใช้รูปแบบเดิมของ `s.logEvent(r, category,
action, severity, target, msg)` ที่มีอยู่ 28 จุด (actor มาจาก context อัตโนมัติ)

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *(9) ให้ resource ID ที่ generate ไม่ได้ fallback ไปใช้ค่าอื่น (timestamp ฯลฯ)* — ตัดทิ้ง:
   ซ่อนความล้มเหลวของ entropy ทำให้ token path ที่ critical อาจถูกมองข้าม; fail-closed
   ทั้งหมดผ่าน helper เดียวเรียบง่ายและปลอดภัยกว่า
2. *(9) แยก generateRandomToken เป็น 2 ตัว (token แบบ fail-closed, ID แบบ best-effort)* —
   ตัดทิ้ง: เพิ่มความซับซ้อนโดย ID ที่เดาได้ก็เป็นความเสี่ยงเล็กๆ อยู่ดี — คืน error ทั้งคู่ง่ายกว่า
3. *(10) ลบ dev origins ทิ้งเลย* — ตัดทิ้ง: workflow dev (`yarn dev` 5173 → backend 8081)
   จำเป็นต้องใช้; gate ด้วย flag เก็บ dev ได้โดย production ปลอดภัย
4. *(10) gate ด้วย `mock` flag ที่มีอยู่* — ตัดทิ้ง: `mock` เป็นเรื่อง kernel ไม่ใช่ CORS;
   dev อาจรัน `mock=false` บนบอร์ดจริงแต่ยังเปิด frontend แยก — flag เฉพาะทางชัดเจนกว่า
5. *(11) สร้าง audit ring buffer แยกตาม review* — ตัดทิ้ง: `EventLogService` + `SystemEvent.Actor`
   ทำงานนี้แล้ว (review เขียนตอนระบบยังไม่เสร็จ — ดู `docs/ref/complete/central-event-log-system-plan.md`)

**Pattern ที่ยึด:** `Server.disableEdit` threading (`main.go`→`NewServer`→`router.go`) สำหรับ (10);
`s.logEvent` 28 จุดที่มีอยู่สำหรับ (11); `SanitizeWpaInput` วินัยเดียวกันสำหรับ (8)

## 3. Steps

### Step 1 — (8) redact wpa command log
**File:** `backend/internal/kernel/wpa.go:153` (+ helper ใกล้ `SanitizeWpaInput:18`)
เพิ่ม `redactWpaCommand` + ใช้ที่ `:153`; พิจารณา response log `:168` ด้วย (response ของ
คำสั่ง secret อาจสะท้อนค่า — redact ถ้าคำสั่งเป็น secret type)

### Step 2 — (9) fail-closed token/ID
**File:** `backend/internal/api/handlers.go:122-126` + ผู้เรียก 9 จุด
- เปลี่ยน `generateRandomToken()` → คืน `(string, error)`
- `HandleLogin:189` — error → `writeError(500)` ไม่ AddSession/ไม่ set cookie
- 8 จุด resource ID — error → `writeError(500)` ก่อน repo.Create
- (ทางเลือกลด churn: ทำ wrapper `mustRandomID()` ที่ panic เมื่อ error สำหรับ ID path
  แต่ session token ต้อง handle error แบบ graceful — ดู Caution 2)

### Step 3 — (10) gate CORS dev origins
**File:** `backend/cmd/pigate/main.go:~41` — เพิ่ม `allowDevCORS := flag.Bool("allow-dev-cors", false, ...)`
**File:** `backend/internal/api/handlers.go` (`NewServer` + `Server` struct) — รับ + เก็บ field
**File:** `backend/internal/api/middleware.go:69-88` + `router.go:172` — `CORSMiddleware`
เป็น method เช็ค `s.allowDevCORS` ก่อน echo dev origin
> **สิ่งที่ไม่ต้องทำ:** ไม่แตะ `install.sh`/systemd unit — production ไม่ส่ง flag ก็ปิดเอง
> (default false); เอกสาร dev workflow เพิ่มหมายเหตุว่าต้องใส่ `-allow-dev-cors` ตอน `yarn dev`

### Step 4 — (11) เติม audit ใน QoS + ไล่ปิดช่องที่เหลือ
**File:** `backend/internal/api/handlers.go` — QoS handlers (Create/Update/Delete/Toggle/Sync/Clear Qos)
เพิ่ม `s.logEvent(r, model.EventCategoryQos, "qos.rule.create", ..., target, msg)` หลังสำเร็จ
แล้วไล่ตรวจ mutating handler อื่นทั้งหมดว่ามี logEvent ครบ (เทียบ 65 routes) — เติมที่ขาด
> **สิ่งที่ไม่ต้องทำ:** ไม่เพิ่ม schema/table — เขียนผ่าน `EventLogService` (batch writer,
> SD-card safe) ที่มีอยู่; ไม่แตะ frontend (หน้า EventLogs แสดง event ใหม่อัตโนมัติ)

### Step 5 — Tests
**File:** `backend/internal/kernel/wpa_test.go`, `backend/internal/api/handlers_test.go`
- (8) `redactWpaCommand("SET_NETWORK 0 psk \"secret\"")` → ไม่มี `secret`; `RECONFIGURE` → เดิม
- (9) mock/inject rand failure ยาก → อย่างน้อย test ว่า signature ใหม่คืน error propagate
  (HandleLogin เมื่อ token gen fail → 500) ผ่าน seam ที่ทดสอบได้; token ปกติยัง unique
- (10) request มี `Origin: localhost:5173` เมื่อ `allowDevCORS=false` → ไม่มี ACAO header;
  เมื่อ true → มี
- (11) POST qos rule → มี SystemEvent actor=ผู้ใช้ถูกบันทึก (ผ่าน mock/spy EventLogService)

### Step 6 — Docs
**File:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` — ไม่ต้องแก้ (พฤติกรรม API
เดิม ยกเว้น 500 กรณี entropy ล่มซึ่งหายาก); **แต่**อัปเดต dev-run doc ให้ระบุ `-allow-dev-cors`

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| POST | `/api/auth/login` | public | (9) 500 แทนการออก token ศูนย์เมื่อ entropy ล่ม (หายากมาก) |
| POST/PUT/DELETE | resource ต่างๆ | ตามเดิม | (9) 500 เมื่อสร้าง ID ไม่ได้; (11) บันทึก audit event เพิ่ม (QoS ฯลฯ) |
| ทุก endpoint | `/api/*` | — | (10) dev origin ถูก echo เฉพาะเมื่อรันด้วย `-allow-dev-cors` |

`-disable-edit` mode: ไม่กระทบทั้ง 4 ข้อ

## 5. Cautions

1. **(8) redact ต้องดูที่ตัวคำสั่ง ไม่ใช่ตัดทั้งหมด** — log ยังมีประโยชน์ debug; ตัดเฉพาะ
   argument ของคำสั่งที่มี keyword secret (psk/password/wep_key) ไม่ใช่ปิด log ทั้งฟังก์ชัน;
   ระวัง response log `:168` ด้วย — บางคำสั่ง secret สะท้อนค่ากลับมา
2. **(9) แยกความสำคัญ session token vs resource ID** — session token (`HandleLogin`) **ต้อง**
   fail graceful (500 + ไม่ set cookie) ห้าม panic กลาง request; resource ID panic พอรับได้
   แต่เพื่อความสม่ำเสมอคืน error ทั้งคู่ดีกว่า — อย่าให้ path ใด "ปล่อยผ่านด้วยค่าศูนย์" เด็ดขาด
   (นั่นคือตัวช่องโหว่)
3. **(9) churn 9 จุด** — เปลี่ยน signature helper กระทบผู้เรียกทุกจุด; แก้ให้ครบ ไม่งั้น
   build fail (ดีกว่าลืมเงียบ) — ไล่ตาม grep 9 จุดใน current state
4. **(10) ถ้าลืมส่ง `-allow-dev-cors` ตอน dev** — `yarn dev` (5173) จะโดน CORS block เรียก
   API ไม่ได้ (อาการเฉพาะ dev, production ปกติ) → ต้องเขียนใน dev doc ชัดๆ และ log ตอน
   startup ว่า dev CORS เปิด/ปิด (เหมือน `log Disable Edit Mode` ที่ `main.go:59`)
5. **(10) `CORSMiddleware` ต้องอยู่นอกสุดเหมือนเดิม** — เปลี่ยนเป็น method อย่าเผลอย้าย
   ตำแหน่งใน chain (`router.go:172` outermost มีเหตุผลเรื่อง 403 ต้องมี CORS header)
6. **(11) logEvent เรียกหลังสำเร็จเท่านั้น** — pattern เดิม (`handlers.go:142` comment) log
   เมื่อ operation สำเร็จ ไม่ใช่ก่อน; อย่า log mutation ที่ยัง error (จะได้ audit ปลอม)
7. **ทดสอบบนอุปกรณ์จริง**: (10) ต้องยืนยันว่า production build (ไม่มี flag) เรียก API ผ่าน
   web UI ปกติ (same-origin ไม่พึ่ง dev CORS อยู่แล้ว) และ (8) ดู journal จริงว่าไม่มี secret
   หลุด — ทดสอบ mock + embedded build บน WSL ก่อน ผู้ใช้ deploy เอง (workflow เดิม)

## 6. Summary Checklist (Definition of Done)

- [ ] (8) `kernel/wpa.go` — `redactWpaCommand` + ใช้ที่ log คำสั่ง (+ response ถ้าจำเป็น)
- [ ] (9) `api/handlers.go` — `generateRandomToken` คืน error + แก้ผู้เรียก 9 จุด (session=500 graceful)
- [ ] (10) `main.go` flag `-allow-dev-cors` + `NewServer`/`Server` field + `CORSMiddleware`
      เป็น method + log สถานะตอน startup
- [ ] (11) `api/handlers.go` — เติม `logEvent` ใน QoS handlers + ไล่ปิดช่อง mutating handler อื่น
- [ ] Tests: redact / token error propagate / CORS gated by flag / QoS audit event actor
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] ทดสอบ mock mode: login/logout ปกติ; สร้าง/แก้ QoS แล้วเห็น event ในหน้า Event Logs
      พร้อม actor; `yarn dev` + `-allow-dev-cors` เรียก API ได้, ไม่มี flag = โดน block (ยืนยัน gate)
- [ ] ทดสอบ embedded build: web UI production เรียก API ผ่าน (same-origin), journal ไม่มี secret
- [ ] อัปเดต dev-run doc ให้ระบุ `-allow-dev-cors`; OpenAPI ไม่ต้องแก้ (จดเหตุผลไว้)
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/` + อัปเดต security review artifact
      (findings 8–11 → done — ปิด review ครบทุกข้อ)
