# HTTPS Server Foundation — Default HTTPS ด้วย self-signed certificate

> แผนงาน: ทำให้ **HTTPS (443) เป็นช่องทางหลัก**ของเครื่องจริง — self-signed cert สร้าง
> อัตโนมัติ (validity เป็นค่าคงที่ ไม่พึ่ง clock), **HTTP เหลือแค่ 308 redirect ไป HTTPS**
> และ fallback กลับมาเสิร์ฟ HTTP เต็มรูปแบบ**เฉพาะเมื่อ TLS start ไม่ได้จริง ๆ** (last resort
> ให้ admin เข้าเครื่องได้เสมอ), เติม `HTTPS` เข้า default `adminAccess` + migration แถวเดิม
> (ไม่งั้น firewall drop 443 = ล็อคเอาท์ทั้งระบบ), แก้ cookie `Secure` ตาม scheme
> และ install.sh เพิ่ม `CAP_NET_BIND_SERVICE` + ถามตั้งเวลา/NTP ตอนติดตั้ง
>
> เขียนเมื่อ: 2026-07-10 · ปรับเป็น Default-HTTPS หลัง review ร่วมกับเจ้าของโปรเจค: 2026-07-10
> Reference branch: `main` (จะทำงานบน `feat/https-server-foundation`)
> README Feature Status: ยังไม่มีแถว HTTPS → เพิ่มเป็น Completed เมื่อจบงาน

## 0. Goal and Scope

**Goal (พฤติกรรมที่ผู้ใช้เห็น):**
- เครื่องจริงหลัง install/upgrade: เข้า `https://<ip>/` ได้ทันที (self-signed — browser เตือน
  ครั้งแรก ผู้ใช้กด accept); พิมพ์ `http://<ip>:2479` หรือ `http://<ip>` → **ถูก redirect ไป HTTPS**
- **บันได fallback:** (1) ปกติ HTTPS เสิร์ฟระบบ + HTTP redirect → (2) ถ้า TLS เปิดไม่ได้
  (cert เขียน/อ่านไม่ได้, bind 443 ไม่ได้เพราะ unit เก่า ฯลฯ) → HTTP เสิร์ฟเต็มรูปแบบ
  พร้อม warning ดัง ๆ + event log — admin ต้องเข้าเครื่องได้เสมอไม่ว่ากรณีไหน
- **เวลาไม่ใช่เงื่อนไขของ HTTPS**: server ไม่ validate cert ตัวเอง (browser เป็นคน validate
  และ self-signed ขึ้นหน้าเตือนอยู่แล้ว) — clock มีผลแค่ตอน generate cert จึงแก้ที่ตัว cert
  (validity คงที่) ไม่ใช่แก้ด้วยการรอ NTP
- ตอนรัน install.sh: ถาม timezone + เปิด NTP (หรือตั้งเวลา manual ถ้า offline)
- dev/mock mode ไม่กระทบ (HTTPS เปิดด้วย flag; ไม่ส่ง flag = พฤติกรรมเดิมทุกอย่าง)

**เงื่อนไขทางเทคนิค:** single binary เดิม, ไม่มี dependency ใหม่ (stdlib `crypto/*` พอ),
ไม่มี reverse proxy, private key ไม่เข้า DB/backup

**Out of scope (ตัดออกชัดเจน):**
- Certificate upload UI / Let's Encrypt (ACME) / จัดการ cert ผ่านหน้าเว็บ — phase ถัดไป
- HSTS — อันตรายกับ self-signed (browser จะ**ไม่ให้กด bypass** เมื่อมี HSTS) ห้ามใส่จนกว่าจะมี cert จริง
- Block boot รอ NTP sync — ไม่จำเป็น (เวลาไม่ใช่เงื่อนไข TLS) และหน่วง admin เข้าเครื่อง
- Regenerate cert เมื่อ hostname/IP เปลี่ยน — ค่อยทำพร้อม cert UI

## 1. Current State (สำรวจโค้ดจริง ณ 2026-07-10)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Listener: `http.ListenAndServe` เปล่า ๆ ไม่มี TLS, ไม่มี timeout ใด ๆ | ขาด | `cmd/pigate/main.go:272` |
| Firewall: `adminAccess` case `"HTTPS"` เปิด TCP 443 พร้อมแล้ว; `"HTTP"` เปิด {80, 2479} | เสร็จ | `kernel/real_firewall.go:944` / `:922` |
| **Seed LAN `adminAccess = PING,HTTP,SSH` — ไม่มี HTTPS → firewall จะ drop 443!** | **ต้องแก้+migration** | `db/connection.go:763` |
| Default adminAccess จุดอื่น (interface ใหม่/VLAN) ก็ไม่มี HTTPS เช่นกัน | ต้องแก้ | `service/interface.go:~101,~599` |
| Model/FE: enum `AdminAccess` มี HTTPS ครบทั้งสองฝั่ง | เสร็จ | `model/types.go`, `mockData.ts:239` |
| install.sh: setcap มีแค่ `cap_net_admin,cap_net_raw` — **bind 443/80 ไม่ได้** | ต้องแก้ | `install.sh:367` |
| systemd unit: Ambient/Bounding caps ไม่มี `CAP_NET_BIND_SERVICE`, ExecStart ไม่มี flag https | ต้องแก้ | `install.sh:394-398` |
| install.sh: ไม่มีขั้นตอนตั้งเวลา/timezone/NTP | ขาด | ทั้งไฟล์ |
| `/var/lib/pigate` owner `pigate:netdev` 775 → วางโฟลเดอร์ `tls/` ได้เลย | เสร็จ | `install.sh:290-295` |
| FE: `API_BASE_URL = "/api"` same-origin → HTTPS ใช้ได้ทันทีไม่ต้องแก้ | เสร็จ | `services/config.ts:10` |
| CORS: อนุญาตเฉพาะ dev origins `http://localhost:*` — prod เป็น same-origin ไม่พึ่ง CORS | เสร็จ | `api/middleware.go:73` |
| Session cookie: `Secure: false` พร้อม comment "Set to true in HTTPS production" | ต้องแก้ | `api/handlers.go:203` |
| Boot order: timesync ทำงานก่อนทุกอย่าง (apply NTP config ให้ sync ได้เร็วสุดอยู่แล้ว) | เสร็จ | `main.go:164-172` |
| Deps: `golang.org/x/crypto` มีอยู่แล้ว; cert gen ใช้ stdlib ล้วน | เสร็จ | `backend/go.mod:12` |
| openapi `servers:` มีแต่ http | ต้องแก้ | `docs/openapi.yaml:6-10` |
| DB (นอกจาก adminAccess migration)/kernel layer/netlink monitor/backup — ไม่เกี่ยว | ไม่แตะ | เหตุผลใน Steps |

**สรุป:** งานกระจุก 5 จุด: (1) cert generator, (2) listener ladder ใน main.go,
(3) **adminAccess default + migration (กันล็อคเอาท์ — สำคัญสุด)**, (4) cookie Secure,
(5) install.sh caps/unit/ตั้งเวลา

## 2. Technical Approach

**Cert generator** — ไฟล์ใหม่ `backend/internal/service/tls_cert.go`:
```go
// EnsureSelfSignedCert: มี cert เดิมที่ยัง valid → คืน path เดิม (idempotent)
// ไม่มี/parse ไม่ได้/หมดอายุเทียบ clock ปัจจุบัน → generate ใหม่ (ECDSA P-256, key 0600)
//   SAN: hostname, "localhost", 127.0.0.1 + IP ของ interface ณ ตอนสร้าง
//   Validity เป็นค่าคงที่ ไม่คำนวณจาก now: NotBefore 2020-01-01, NotAfter 2056-01-01
//   → gen ตอน clock เป็น 1970 หรือ 2026 ได้ cert ใช้ได้เหมือนกันทุกกรณี
func EnsureSelfSignedCert(dir, hostname string, ips []net.IP) (certPath, keyPath string, generated bool, err error)
```

**Listener ladder** — ใน `main.go` แทน `http.ListenAndServe` เดิม:
```go
// httpsPort > 0 (unit ส่ง 443):
//   1) EnsureSelfSignedCert สำเร็จ + bind :443 ได้
//        → HTTPS เสิร์ฟ handler จริง (TLS MinVersion 1.2 + timeouts)
//        → HTTP :<port> และ :80 เสิร์ฟ redirectHandler (308 → https://<host>/...)
//           (:80 bind fail = warn เฉย ๆ — เป็นของแถม)
//   2) cert ล้มเหลว หรือ bind :443 ไม่ได้
//        → log.Printf warning บอกสาเหตุ + วิธีแก้ (re-run install.sh) + event log
//        → HTTP :<port> เสิร์ฟ handler จริง (fallback — พฤติกรรมเท่าของเดิมวันนี้)
// httpsPort == 0 (dev/mock ไม่ส่ง flag): HTTP :<port> เสิร์ฟ handler จริง — เท่าเดิมทุกอย่าง
```

**Flags ใหม่:** `-https-port` (default `0`; systemd unit ส่ง `443` → เครื่องจริงเป็น
Default HTTPS โดย dev ไม่โดนอะไรเลย), `-tls-dir` (default `<dir ของ -db>/tls`)

**adminAccess migration** — ใน `db/connection.go` (pattern เดียวกับ migration เดิม):
```sql
-- one-time, idempotent: แถวที่มี HTTP แต่ไม่มี HTTPS → เติม HTTPS
-- (ทำใน Go: อ่าน admin_access CSV, เติม, UPDATE — เหมือน de-dup pass ของงาน alias #25)
```

**Cookie:** `Secure: r.TLS != nil` ต่อ request (มี fallback HTTP mode จึงใช้ flag รวมไม่ได้)

- **ทำไมทางนี้:** Default HTTPS ตามเป้าความปลอดภัย โดย HTTP-redirect กันคนหลงทาง +
  ไม่มี credential บน plaintext; fallback ladder ผูกกับ "TLS เปิดไม่ได้จริง" ไม่ผูกกับเวลา
  (เวลาแก้จบที่ตัว cert แล้ว); validity คงที่ = ตัดปัญหา Pi ไม่มี RTC battery ทั้งก้อน
- **ทางเลือกที่ตัดทิ้ง:**
  - Fallback HTTP เมื่อ NTP sync ไม่ได้ — ตัด: server-side TLS ไม่ต้องใช้เวลาที่ถูกต้อง
    ปัญหาเดียวคือ validity ตอน gen ซึ่งแก้ด้วยค่าคงที่ + regenerate-เมื่อ-invalid ตรงจุดกว่า
  - Let's Encrypt/ACME — ตัด: gateway อยู่ใน LAN ไม่มี public domain, เพิ่ม dep
  - Reverse proxy (nginx/caddy) — ตัด: ขัด single binary, เพิ่ม attack surface
  - เก็บ cert/key ใน SQLite — ตัด: key จะติดไป backup/export (schema v2 อ่านจาก DB)
  - HTTPS บนพอร์ต 2479 เดิมแทน 443 — ตัด: firewall HTTPS rule + ความคาดหวังผู้ใช้อยู่ที่ 443
- **Template ที่ลอก:** warn-and-continue ของ `InitApplyConfig` ใน `main.go`;
  migration แบบอ่าน-แก้-เขียนใน Go ตาม `ensureUniqueAliasIndex` (งาน #25)

## 3. Steps (เรียงจากชั้นในสุดออกนอก)

### Step 1 — Cert generator + unit test
**File (ใหม่):** `backend/internal/service/tls_cert.go` — `EnsureSelfSignedCert` ตามด้านบน
**File (ใหม่):** `backend/internal/service/tls_cert_test.go` — idempotent, regenerate เมื่อ
cert เดิม invalid/expired, key perms 0600, SAN ครบ, NotBefore/NotAfter ตรงค่าคงที่
> **ไม่ต้องเพิ่ม kernel interface/mock**: ไม่มีการควบคุม OS — file I/O + stdlib crypto ล้วน

### Step 2 — DB: migration เติม HTTPS ใน adminAccess
**File:** `backend/internal/db/connection.go` (ท้าย `migrate()` ตาม pattern `ensureUniqueAliasIndex`)
- แถวที่ `admin_access` มี `HTTP` แต่ไม่มี `HTTPS` → เติม `HTTPS` + log; idempotent
- แก้ seed (`:763`): LAN → `PING,HTTP,HTTPS,SSH`

### Step 3 — Service: default adminAccess จุดอื่น
**File:** `backend/internal/service/interface.go` — defaults interface ใหม่ (~101) และ
`CreateVlanInterface` (~599): LAN เพิ่ม `HTTPS`
**File:** `frontend/src/services/interfaceService.ts` — mock createVlan/reset defaults ให้ตรงกัน

### Step 4 — main.go: flags + listener ladder
**File:** `backend/cmd/pigate/main.go` (~34 flags, ~269 listener)
- flag `-https-port`/`-tls-dir`; ladder ตาม §2; ทุก listener เป็น `http.Server` พร้อม
  `ReadHeaderTimeout` ฯลฯ; gen cert ใหม่/เข้า fallback → `eventLogService.Log(...)`
- redirectHandler: `https://` + host (ตัด port) + URI เดิม, `308 Permanent Redirect`
> **ไม่ต้อง InitApplyConfig ใหม่**: ขับด้วย flag + ไฟล์ ไม่มี state ใน DB; ลำดับ boot เดิม
> (timesync ก่อน) ครอบอยู่แล้ว

### Step 5 — Cookie Secure ตาม scheme
**File:** `backend/internal/api/handlers.go` (~198 login, ~234 logout) — `Secure: r.TLS != nil`

### Step 6 — install.sh: capabilities + unit + ตั้งเวลา
**File:** `install.sh`
- `:367` → `setcap cap_net_admin,cap_net_raw,cap_net_bind_service+ep`
- `:394-395` → เพิ่ม `CAP_NET_BIND_SERVICE` ใน `AmbientCapabilities` + `CapabilityBoundingSet`
- `:398` → `ExecStart=... -https-port=443`
- ขั้นตอนใหม่ (interactive): แสดงเวลาปัจจุบัน → ถาม timezone (`timedatectl set-timezone`),
  เปิด NTP (`timedatectl set-ntp true`) หรือตั้งเวลา manual ถ้าเครื่อง offline

### Step 7 — Docs
**Files:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` (servers เพิ่ม
`https://<device>/api`), README (Default HTTPS, self-signed warning, **upgrade ต้อง
re-run install.sh**, ตาราง Feature Status เพิ่มแถว HTTPS)

## 4. Related API

| Method | Path | Role | พฤติกรรม | สถานะ |
|---|---|---|---|---|
| ทุก endpoint เดิม | `/api/...` | เดิม | เสิร์ฟผ่าน HTTPS; HTTP = 308 redirect (หรือ fallback เต็มรูปแบบ) | เดิม |

- **ไม่มี endpoint ใหม่** — งานชั้น transport; `-disable-edit` ไม่เกี่ยว
- Script/integration เดิมที่ยิง `http://<ip>:2479/api` ตรง ๆ จะเจอ 308 — client ที่ follow
  redirect + ยอม self-signed (`curl -kL`) ใช้ต่อได้; ระบุใน release note

## 5. Cautions

1. **ล็อคเอาท์จาก redirect + firewall ปิด 443** — จุดอันตรายสุดของงานนี้: ถ้า HTTP กลายเป็น
   redirect แต่ adminAccess ของ interface ไม่มี HTTPS → 443 โดน drop → เข้าอะไรไม่ได้เลยทั้ง
   เครื่องใหม่ (seed เดิมไม่มี HTTPS) และเครื่อง upgrade (แถว DB เดิม). ป้องกัน: Step 2
   (migration+seed) **ต้อง merge พร้อมกันกับ Step 4 ใน PR เดียว** ห้ามแยก release
2. **bind 443/80 โดย user `pigate` ต้องได้ครบสองที่** — setcap (file) + `AmbientCapabilities`
   (unit); ขาดที่ใดที่หนึ่ง → bind fail. ป้องกัน: Step 6 แก้ทั้งคู่ + ladder ทำให้ fail แล้ว
   ยังเหลือ HTTP fallback เสมอ
3. **Upgrade เฉพาะ binary โดยไม่ re-run install.sh** — unit เก่าไม่มี cap/flag → เข้า fallback
   HTTP (พฤติกรรมเท่าของเดิม ไม่มีใครล็อคเอาท์) แต่ไม่ได้ HTTPS. ป้องกัน: warning ใน log
   ต้องบอกวิธีแก้ชัด ๆ + release note
4. **ห้าม `log.Fatalf` ใน goroutine listener ใด ๆ** — จะฆ่า process รวม listener ตัวอื่น; ใช้ warn
5. **`Secure` cookie ตัดสิน per-request จาก `r.TLS`** — มี fallback HTTP mode ถ้าใช้ flag รวม
   แล้ว login ผ่าน HTTP browser จะทิ้ง cookie เงียบ ๆ (Bearer ยังทำงาน สังเกตยาก)
6. **Validity cert ห้ามคำนวณจาก `now`** — Pi clock ตอน first boot อาจเป็น 1970; `NotAfter=now+10y`
   จะได้ cert ตายตั้งแต่เกิดเมื่อ NTP แก้เวลา. ป้องกัน: ค่าคงที่ + regenerate-เมื่อ-invalid (Step 1)
7. **HSTS ห้ามใส่** — self-signed + HSTS = browser ปิดปุ่ม bypass → ล็อคเอาท์ถาวรระดับ browser
8. **Private key ห้ามเข้า backup/export** — ยืนยันแล้ว `service/backup.go` อ่านจาก DB เท่านั้น
   ปลอดภัยโดยโครงสร้าง; ห้ามย้าย cert ไป DB ในอนาคตโดยไม่ตัดออกจาก export
9. **Migration adminAccess ต้อง idempotent + ไม่แตะแถวที่ user ตั้งใจปิด HTTP** — เติม HTTPS
   เฉพาะแถวที่มี HTTP อยู่ (สัญญาณว่า interface นั้นเปิด admin web อยู่แล้ว); แถวที่ไม่มี HTTP
   (เช่น WAN = PING อย่างเดียว) ห้ามแตะ
10. **ทดสอบเครื่องจริงเสี่ยงล็อคตัวเองออก** — เงื่อนไข: มี physical access เท่านั้น, ทดสอบ
    ลำดับ: ยืนยัน `https://<ip>/` login ได้ก่อน แล้วค่อยลองเคส fallback/adminAccess

## 6. Summary Checklist (Definition of Done)

- [ ] `service/tls_cert.go` + `tls_cert_test.go` — gen/idempotent/regenerate-เมื่อ-invalid/perms/SAN/validity คงที่
- [ ] `db/connection.go` — migration เติม HTTPS (idempotent, เฉพาะแถวมี HTTP) + seed LAN มี HTTPS
- [ ] `service/interface.go` — default adminAccess (interface ใหม่ + VLAN) มี HTTPS; FE mock ตรงกัน
- [ ] `main.go` — flags + listener ladder + timeouts + redirect 308 + warn-not-fatal + event log
- [ ] `api/handlers.go` — cookie `Secure: r.TLS != nil` (login + logout)
- [ ] `install.sh` — setcap + unit caps + `-https-port=443` + ขั้นตอนตั้งเวลา/timezone/NTP
- [ ] `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` — servers เพิ่ม https
- [ ] README — Default HTTPS, self-signed warning, upgrade path, Feature Status
- [ ] `go build ./...` + `go test ./...` ผ่าน (รวม test migration adminAccess); `yarn build` ยืนยัน
- [ ] ทดสอบ mock: `-https-port=8443` → `curl -k https://…` ได้, HTTP โดน 308, ไม่ส่ง flag = เดิมทุกอย่าง,
    login ผ่าน https ได้ cookie `Secure` / ผ่าน http (fallback) ไม่มี `Secure`
- [ ] ทดสอบ cert: ลบ `tls/` → สร้างใหม่ + event log; boot ซ้ำไม่สร้างซ้ำ; cert หมดอายุ → regenerate
- [ ] ทดสอบ fallback: ตั้ง `-tls-dir` ชี้ที่เขียนไม่ได้ → HTTP เสิร์ฟเต็มรูปแบบ + warning + event log
- [ ] ทดสอบเครื่องจริง (physical access): re-run install.sh → https เข้าได้, http redirect,
    DB เก่าถูก migrate เติม HTTPS, WAN ที่มีแค่ PING ไม่ถูกแตะ
