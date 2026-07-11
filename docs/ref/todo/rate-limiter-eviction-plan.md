# Rate-Limiter Eviction — จำกัดขนาด limiters map ด้วย idle sweep + hard cap

> แผนงานแก้ security review finding 5 (Medium): `limiters` map ใน `middleware.go`
> เก็บ token-bucket ต่อ source IP โดย**ไม่เคยลบ** — attacker วน IPv6 privacy addresses
> (หรือ address pool ใหญ่ๆ) ทำให้ map โตไม่จำกัดจน Pi หมด RAM ได้
> แก้ด้วย `lastSeen` ต่อ entry + sweeper goroutine + hard cap พร้อม test ชุดแรกของ limiter
>
> เขียนเมื่อ: 2026-07-11 · Reference branch: `fix/rate-limiter-eviction` · Issue: #34
> งานถัดจาก #32 (session TTL) และ #33 (body cap + SSE) ใน remediation roadmap

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- `limiters` map มีขนาด bounded เสมอ: entry ที่ idle เกิน **10 นาที** ถูก sweeper เก็บกวาด
  และมี **hard cap 4096 entries** เป็น backstop ระหว่างรอบ sweep
- การ evict ไม่มีทางบล็อกผู้ใช้จริง — entry ที่ถูกลบแล้วกลับมาใหม่ = ได้ bucket เต็ม 5 tokens
  (ผลข้างเคียงมีแต่ "ผ่อน" rate limit ชั่วคราว ไม่ใช่ "ขัง" ใคร)
- พฤติกรรม rate limit เดิมของ login คงเดิมทุกอย่าง: 5 tokens, refill 1 ต่อ 2 วินาที, 429 เมื่อหมด
- Limiter มี unit tests เป็นครั้งแรก (ตอนนี้ไม่มีเลย)

**Out of scope (ตัดออกชัดเจน):**
- Account lockout / นับ failed attempts ต่อบัญชี (ยังไม่มีในระบบ — เป็น hardening คนละชั้น
  ที่ review ติไว้ใน Authentication B+ ทำเป็นงานแยกได้ภายหลัง)
- ขยาย rate limit ไป endpoint อื่นนอกจาก login (review เคยติ "expensive endpoints unlimited"
  ใน scorecard — ควรตัดสินใจร่วมกับ finding 6 ทีหลัง ไม่พ่วงในงานนี้)
- Key แบบ IPv6 /64 prefix — พิจารณาแล้วตัดทิ้ง (ดู §2 ทางเลือกข้อ 2)

## 1. Current State (สำรวจโค้ดจริง 2026-07-11)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `rateLimiter` struct | ต้องเพิ่ม `lastSeen` | `backend/internal/api/middleware.go:195-200` — มี `tokens/max/last/mu` |
| `limiters` map + `getLimiter()` ไม่มี eviction | ต้องแก้ | `middleware.go:202-221` — insert อย่างเดียว ไม่มีทางออก |
| `allow()` token bucket | แก้เล็กน้อย (อัปเดต `lastSeen`) | `middleware.go:223-244` |
| **Trap:** field `last` ใช้เป็นตัวชี้วัด idle ไม่ได้ | ต้องเพิ่ม field ใหม่ ห้าม reuse | `middleware.go:229-236` — `last` อัปเดตเฉพาะเมื่อ `refill > 0` (request ที่มาถี่กว่า 2s ไม่แตะ `last`) |
| จุด mount middleware | จุดเดียว — login | `backend/internal/api/router.go:11` — `POST /api/auth/login` เท่านั้น |
| การดึง IP จาก `RemoteAddr` | ใช้ต่อได้ (ปรับเป็น `net.SplitHostPort` ให้สะอาดขึ้น) | `middleware.go:249-253` — `LastIndex(ip, ":")` ทำ key IPv6 ติด bracket `[::1]` (ใช้เป็น key ได้แต่ไม่สวย) |
| Sweeper pattern ในโปรเจกต์ | มีให้ยึด 2 ที่ | `backend/internal/service/event_log.go:82-101`; แผน #32 จะเพิ่ม session sweeper แบบเดียวกันใน api layer |
| จุด start goroutine ใน main | มี ctx พร้อมใช้ | `backend/cmd/pigate/main.go:193` (`monitorCtx`), `:217` (`eventLogService.Start`) |
| Tests ของ limiter | **ไม่มีเลย** | grep `RateLimit\|429\|TooManyRequests` ใน `backend/internal/api/*_test.go` = 0 hits |
| `net/netip` ใน codebase | ยังไม่มีใครใช้ (stdlib ใช้ได้ถ้าต้องการ) | grep ทั้ง backend = 0 hits — งานนี้ไม่จำเป็นต้องใช้ |
| kernel / service / db / frontend / install.sh / OpenAPI | ไม่เกี่ยว | limiter เป็น in-memory ใน api layer; พฤติกรรม 429 ภายนอกไม่เปลี่ยน — ไม่มี contract change |

สรุป: งานทั้งหมดอยู่ในไฟล์เดียว (`middleware.go`) + 1 บรรทัดใน `main.go` + test ใหม่ —
เล็กที่สุดในชุด remediation นี้ แต่ต้องระวังการชนกับงาน #32 ที่แก้ไฟล์เดียวกัน (Caution 1)

## 2. Technical Approach

**กลไกที่เลือก: `lastSeen` + sweeper goroutine + hard cap พร้อม synchronous sweep**

```go
const (
    limiterIdleAfter     = 10 * time.Minute // bucket refill เต็มใน 10s — idle 10 นาที = stale แน่นอน
    limiterSweepInterval = 10 * time.Minute
    maxLimiterEntries    = 4096 // backstop ระหว่างรอบ sweep (~ไม่กี่ร้อย KB)
)

type rateLimiter struct {
    tokens   int
    max      int
    last     time.Time // สำหรับ refill (semantics เดิม)
    lastSeen time.Time // สำหรับ eviction — อัปเดตทุกครั้งที่ allow() ถูกเรียก
    mu       sync.Mutex
}
```

- `allow()` เซ็ต `lastSeen = now` ทุกครั้ง (ไม่ขึ้นกับ refill — ดู trap ใน §1)
- `sweepIdleLimiters()`: ไล่ลบ entry ที่ `now - lastSeen > limiterIdleAfter` —
  แยกเป็นฟังก์ชันให้ test เรียกตรงได้; `StartLimiterSweeper(ctx)` ครอบด้วย ticker +
  `ctx.Done()` ตาม pattern `event_log.go:82-101`
- `getLimiter()`: ก่อน insert ถ้า `len(limiters) >= maxLimiterEntries` → เรียก sweep
  แบบ synchronous ก่อน; ถ้ายังเต็มอยู่ (ทุกตัว active — เกิดได้เฉพาะโดนโจมตี) →
  ลบ entry แรกที่ map iteration เจอ (สุ่มโดยธรรมชาติของ Go map) แล้วค่อย insert
- เปลี่ยนการดึง IP เป็น `net.SplitHostPort(r.RemoteAddr)` (fallback เป็น string เดิม
  ถ้า parse ไม่ได้) — key IPv6 สะอาดขึ้น ไม่ติด bracket

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *LRU ขนาดคงที่ (เช่น `hashicorp/golang-lru`)* — ตัดทิ้ง: เพิ่ม dependency ใหม่ขัดหลัก
   minimal deps เพื่อ map ที่มีของแค่หลักพัน entry; sweeper + cap ให้ผลเดียวกันด้วย stdlib
2. *Key IPv6 ด้วย /64 prefix (ตัดปัญหา privacy-address cycling ที่ต้นเหตุ)* — ตัดทิ้ง:
   PiGate เป็น gateway ของ LAN — client ทุกเครื่องในวงมักได้ address จาก /64 เดียวกัน
   การรวม bucket ต่อ /64 ทำให้เครื่องใดเครื่องหนึ่งใน LAN เผาโควตา login ของทั้งวงได้
   (attacker ใน LAN บล็อกแอดมินตัวจริง) — แลกไม่คุ้ม; memory จัดการด้วย cap/sweep แทน
3. *Reset map ทั้งก้อนเมื่อชน cap* — ตัดทิ้ง: ทิ้งสถานะ limiter ของทุก IP รวมถึงตัวที่กำลัง
   โดน brute-force คุมอยู่ → attacker ตั้งใจชน cap เพื่อปลด limit ตัวเองได้เป็นรอบๆ;
   ลบรายตัว (สุ่ม) จำกัดความเสียหายกว่า
4. *Lazy eviction ใน getLimiter อย่างเดียว ไม่มี goroutine* — ตัดทิ้ง: ถ้าไม่มี login ใหม่เลย
   map ที่โตค้างจากการโจมตีครั้งก่อนจะอยู่ยาว; sweeper ทำให้ steady-state กลับมาเล็กเสมอ
   (และ pattern goroutine นี้มีอยู่แล้วในโปรเจกต์ ไม่ได้เพิ่มความซับซ้อนใหม่)

**Pattern ที่ยึด:** ticker + `ctx.Done()` ตาม `event_log.go:82-101` และ session sweeper
ของแผน #32 (`docs/ref/todo/server-side-session-ttl-plan.md` Step 2) — ตั้งชื่อ/โครงให้ล้อกัน

## 3. Steps (เรียงชั้นในสุด → นอกสุด)

### Step 1 — เพิ่ม `lastSeen` + ค่าคงที่ + แก้ `allow()`
**File:** `backend/internal/api/middleware.go:195-244`
เพิ่ม field `lastSeen` ใน struct, ค่าคงที่ตาม §2, และให้ `allow()` เซ็ต `lastSeen = now`
ทุกครั้งก่อนคำนวณ refill (ภายใต้ `lim.mu` ที่ถืออยู่แล้ว)

### Step 2 — `sweepIdleLimiters()` + `StartLimiterSweeper(ctx)`
**File:** `backend/internal/api/middleware.go` (ต่อจากบล็อก limiter เดิม)
- `sweepIdleLimiters()` ถือ `limitersMu` ไล่ลบ entry idle — **อ่าน `lastSeen` ต้องถือ
  `lim.mu` รายตัวด้วย** (ดู Caution 3) — log จำนวนที่ลบเมื่อ > 0
- `StartLimiterSweeper(ctx)` goroutine + ticker `limiterSweepInterval`

### Step 3 — hard cap ใน `getLimiter()` + ปรับการดึง IP
**File:** `backend/internal/api/middleware.go:207-221, :249-253`
- ใน `getLimiter()`: ถ้าจะ insert ใหม่และ `len(limiters) >= maxLimiterEntries` →
  sweep ก่อน แล้วถ้ายังเต็มลบ entry แรกที่เจอจาก iteration
- ใน `RateLimitMiddleware`: เปลี่ยน `LastIndex` เป็น `net.SplitHostPort` + fallback

### Step 4 — start sweeper ใน `main.go`
**File:** `backend/cmd/pigate/main.go:~217`
เพิ่ม `api.StartLimiterSweeper(monitorCtx)` ถัดจาก `eventLogService.Start(monitorCtx)`
(ถ้างาน #32 merge ก่อน จะมี `api.StartSessionSweeper(monitorCtx)` อยู่แล้ว — วางเรียงกัน)

### Step 5 — Tests ชุดแรกของ limiter
**File:** `backend/internal/api/middleware_test.go` (ไฟล์ใหม่ ถ้า #33 ยังไม่สร้าง)
- ยิง `allow()` 5 ครั้งผ่าน → ครั้งที่ 6 ทันที → false (429 ผ่าน middleware จริงด้วย
  `httptest` + `RemoteAddr` คงที่)
- request ถี่กว่า 2s ต้องอัปเดต `lastSeen` ทุกครั้ง (กัน regression ของ trap `last`)
- `sweepIdleLimiters()`: entry idle (ฉีด `lastSeen` อดีต) หาย, entry active อยู่
- cap: insert เกิน `maxLimiterEntries` (ลูป IP ปลอม) → `len(limiters) <= maxLimiterEntries` เสมอ
- IPv6 `RemoteAddr` (`[2001:db8::1]:1234`) → ได้ key ที่ parse แล้ว ไม่ crash
> **สิ่งที่ไม่ต้องทำ:** ไม่มี kernel/mock/db/migration/install.sh/frontend/OpenAPI —
> พฤติกรรม 429 ภายนอกไม่เปลี่ยน จึงไม่มี contract change; ไม่ต้องแตะ `router.go`
> (mount จุดเดิม); ไม่มี boot-apply เพราะ state เริ่มจากว่างเสมอ

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| POST | `/api/auth/login` | public (rate-limited) | พฤติกรรมภายนอกเดิมทุกอย่าง (5 ครั้ง/burst, refill 2s, 429) — เปลี่ยนเฉพาะ internal memory management |

`-disable-edit` mode: ไม่กระทบ — login ได้รับยกเว้นจาก `DisableEditMiddleware` อยู่แล้ว
(`middleware.go:271`) และ limiter ไม่เกี่ยวกับ mutation

## 5. Cautions

1. **ชนไฟล์กับงาน #32 (session TTL)** — ทั้งสองแผนแก้ `middleware.go` + เพิ่มบรรทัดที่
   `main.go:~217` + สร้าง sweeper คล้ายกัน → ทำทีละงาน (แนะนำ #32 ก่อนเพราะใหญ่กว่า)
   แล้ว rebase; ถ้า #32 ย้าย session store ไป `session.go` แล้ว limiter block ใน
   `middleware.go` จะขยับบรรทัด — ยึดชื่อฟังก์ชันเป็นหลัก ไม่ใช่เลขบรรทัดในแผนนี้
2. **ห้ามใช้ field `last` ตัดสิน idle** — `last` อัปเดตเฉพาะเมื่อ refill > 0 (`middleware.go:236`)
   request ที่มาถี่ต่อเนื่องอาจทิ้ง `last` เก่าไว้ → entry ที่ active โดนลบกลาง burst
   ทำ rate limit หลุด (attacker ได้ bucket ใหม่ 5 tokens ฟรี) → ต้องมี `lastSeen`
   แยกที่อัปเดตทุก `allow()` และมี test กัน regression
3. **Lock ordering ระหว่าง `limitersMu` กับ `lim.mu`** — sweeper ถือ `limitersMu` แล้วต้อง
   อ่าน `lastSeen` ที่คุ้มครองโดย `lim.mu` ของแต่ละ entry; ส่วน `allow()` ถือ `lim.mu`
   โดยไม่แตะ `limitersMu` เลย → ทิศเดียว (`limitersMu` → `lim.mu`) ไม่มี deadlock
   แต่**ห้าม**เพิ่มโค้ดใน `allow()` ที่ย้อนไปหยิบ `limitersMu` เด็ดขาด (จะเกิด lock inversion)
4. **Evict แล้วผู้ใช้จริงต้องไม่โดนขัง** — การลบ entry มีผลแค่ "bucket reset เป็นเต็ม"
   เพราะ entry ใหม่เริ่มที่ 5 tokens (`middleware.go:213-217`) → ยืนยันคุณสมบัตินี้ไว้ใน test
   (ลบระหว่างใช้งานแล้ว request ถัดไปยังผ่าน) — ถ้าอนาคตใครเปลี่ยน initial tokens เป็น 0
   คุณสมบัติ "eviction ปลอดภัยเสมอ" จะพังเงียบๆ
5. **อย่าตั้ง `limiterIdleAfter` สั้นกว่าเวลา refill เต็ม bucket** — bucket เต็มใน 10 วินาที
   (5 tokens × 2s) ค่า 10 นาทีเผื่อไว้ ~60 เท่า; ถ้าลดต่ำมากๆ (เช่น 30s) entry ของ IP
   ที่โดนจำกัดอยู่จะถูกลบเร็วเกิน = ปลด limit ให้ attacker ที่ยิงเป็นจังหวะ
6. **ทดสอบบนอุปกรณ์จริง**: ไม่แตะ firewall/routing — ความเสี่ยงเดียวคือ login พังทั้งระบบ
   ถ้า middleware บั๊ก → ต้องผ่าน mock mode (ยิง login ผิดๆ 6 ครั้งรัวๆ ต้องเจอ 429 แล้ว
   รอ ~2s ยิงใหม่ต้องผ่าน) + embedded build บน WSL ก่อน แล้วผู้ใช้ deploy เอง (workflow เดิม)

## 6. Summary Checklist (Definition of Done)

- [ ] `backend/internal/api/middleware.go` — `lastSeen` + ค่าคงที่ 3 ตัว + `allow()` อัปเดต
      `lastSeen` ทุกครั้ง
- [ ] `backend/internal/api/middleware.go` — `sweepIdleLimiters()` + `StartLimiterSweeper(ctx)`
- [ ] `backend/internal/api/middleware.go` — hard cap ใน `getLimiter()` + `net.SplitHostPort`
- [ ] `backend/cmd/pigate/main.go` — `api.StartLimiterSweeper(monitorCtx)`
- [ ] Tests: 429 หลัง 5 ครั้ง / `lastSeen` อัปเดตแม้ request ถี่ / sweep ลบเฉพาะ idle /
      cap ไม่ทะลุ / IPv6 RemoteAddr / evict แล้ว request ถัดไปผ่าน
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] ทดสอบ mock mode: login ผิด 6 ครั้งรัว → 429; รอ ~2 วินาที → ผ่าน; login ถูกต้องปกติ
- [ ] ทดสอบ embedded build (`bash build.sh` + HTTPS): พฤติกรรม 429 เดิมครบ
- [ ] ไม่มี OpenAPI/README ต้องแก้ (พฤติกรรมภายนอกไม่เปลี่ยน — จดเหตุผลไว้แล้วใน Step 5)
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/` + อัปเดต security review artifact
      (finding 5 → done)
