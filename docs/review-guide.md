# คู่มือการรีวิวความปลอดภัย (Security Review Guide) — PiGate

> เอกสารนี้เป็น "วิธีการที่ทำซ้ำได้" สำหรับรีวิวความปลอดภัยของ PiGate
> ผู้รีวิวคนถัดไปควรทำตามลำดับในเอกสารนี้ได้โดยไม่ต้องรู้ประวัติของโปรเจคมาก่อน
> ฉบับภาษาอังกฤษ: `docs/review-guide-eng.md`
> อัปเดตล่าสุด: 2026-07-17 (อ้างอิงสถานะโค้ด ณ branch `main` @ `ec1e1d0`; รอบก่อนหน้า: 2026-07-08 บน `feat/dialog-to-drawer`)

---

## 1. วัตถุประสงค์และขอบเขต

PiGate คือ firewall/gateway controller ที่รันบน Raspberry Pi ด้วยสิทธิ์ `cap_net_admin,cap_net_raw`
และควบคุม kernel ผ่าน Netlink/D-Bus โดยตรง — **ความผิดพลาดด้านความปลอดภัยของโปรเจคนี้
หมายถึงการยึดครอง gateway ของเครือข่ายได้ทั้งวง** ดังนั้นการรีวิวต้องเข้มงวดกว่าเว็บแอปทั่วไป

ขอบเขตการรีวิวครอบคลุม:

| ส่วน | ที่อยู่ | ความสำคัญ |
|---|---|---|
| Authentication / Session / RBAC | `backend/internal/api/middleware.go`, `handlers.go`, `internal/service/user.go` | สูงมาก |
| การสร้างกฎ firewall / routing / QoS | `internal/service/firewall.go`, `routing.go`, `qos.go`, `internal/kernel/real_*.go` | สูงมาก |
| การเขียนไฟล์ config ระบบ (wpa_supplicant, dnsmasq) | `internal/kernel/wpa.go`, `dhcp_server.go`, `dns_server.go` | สูงมาก |
| การเรียก OS (Netlink, D-Bus, exec) | `internal/kernel/` ทั้งหมด | สูงมาก |
| Database / SQL | `internal/db/repository.go`, `connection.go` | สูง |
| การจัดการความลับ (Wi-Fi password, backup) | `internal/service/backup.go`, `backup_crypto.go`, `handlers.go` | สูง |
| Frontend (XSS, token storage) | `frontend/src/services/`, `components/` | กลาง |
| การติดตั้ง/สิทธิ์ระบบ | `install.sh`, `build.sh`, Polkit/sudoers ที่ติดตั้ง | สูง |
| Dependencies | `backend/go.mod`/`go.sum`, `frontend/package.json`/`yarn.lock` | กลาง |

**อ่านก่อนเริ่ม:** `docs/tech_stack_design.md` (แนวคิดการออกแบบทั้งหมด โดยเฉพาะ §4.3 โครงสร้าง nftables)
และ `docs/wifi_wpa_working_instruction.md` (ก่อนแตะโค้ด Wi-Fi)

---

## 2. การเตรียมสภาพแวดล้อม

รีวิวบนเครื่อง dev ได้อย่างปลอดภัยด้วย mock mode (ไม่แตะ kernel จริง):

```bash
cd backend
go build -o pigate-backend ./cmd/pigate
./pigate-backend -port=8081 -db=/tmp/review.db -mock=true
go test ./...
```

ติดตั้งเครื่องมือวิเคราะห์ (ครั้งแรกครั้งเดียว):

```bash
go install golang.org/x/vuln/cmd/govulncheck@latest
go install honnef.co/go/tools/cmd/staticcheck@latest
go install github.com/securego/gosec/v2/cmd/gosec@latest
```

---

## 3. Threat Model โดยย่อ (ใช้ตีกรอบว่าอะไร "เสี่ยง")

ผู้โจมตีที่ต้องคำนึงถึง เรียงตามความน่าจะเป็น:

1. **อุปกรณ์ในเครือข่าย LAN ที่ถูกยึด** (มือถือ/IoT ติดมัลแวร์) — เห็น traffic HTTP ของหน้าแอดมิน, ยิง API ได้โดยตรง, brute-force login ได้
2. **ผู้ใช้ role `admin_readonly` ที่พยายามยกระดับสิทธิ์** — ต้องพิสูจน์ว่า middleware ปิดทุกทางที่เขียนข้อมูลได้
3. **ผู้ดูแลที่ล็อกอินแล้วแต่ป้อนข้อมูลอันตราย (โดยตั้งใจหรือถูกหลอก)** — ค่า SSID/hostname/zone name ที่มีอักขระพิเศษ ต้องไม่กลายเป็นการ inject config หรือคำสั่ง
4. **ไฟล์ backup ที่หลุด** — มีรหัสผ่าน Wi-Fi และ hash ของบัญชี
5. **XSS ใน frontend** — token อยู่ใน `localStorage` ถ้ามี XSS จะขโมย session ได้ทันที

สิ่งที่ *ไม่อยู่* ในขอบเขต: ผู้โจมตีที่มี root บนตัว Pi อยู่แล้ว, การโจมตีทางกายภาพ, WAN-side attack (สมมติว่า nftables ปิด input จาก WAN ตามดีไซน์)

---

## 4. Checklist การรีวิวรายหมวด (ทำตามลำดับ)

รูปแบบของแต่ละหมวด: **ดูที่ไหน → ดูอย่างไร → ความเสี่ยงถ้าพลาด → แนวทางแก้**

### A. Authentication และ Session

**ดูที่ไหน:** `internal/api/middleware.go`, `internal/api/handlers.go` (ส่วน AUTHENTICATION HANDLERS), `internal/service/user.go`, `internal/db/connection.go` (ฟังก์ชัน `seed`)

**ดูอย่างไร:**
- ตรวจว่า token สร้างจาก `crypto/rand` เท่านั้น (ห้าม `math/rand`) และยาว ≥ 16 bytes — ดู `generateRandomToken()` ใน `handlers.go`
- ตรวจว่า error จาก `rand.Read` **ไม่ถูกเพิกเฉย** (ปัจจุบันบรรทัด `_, _ = rand.Read(b)` เพิกเฉยอยู่ — ถ้า entropy ล้มเหลวจะได้ token เป็นศูนย์ล้วน)
- ตรวจอายุ session: ฝั่ง server (`activeSessions` map) มีการหมดอายุหรือไม่ ปัจจุบัน **ไม่มี TTL** — token ใช้ได้จนกว่า process จะ restart แม้ cookie ฝั่ง browser หมดอายุใน 24 ชม.
- ตรวจ password hashing: ต้องเป็น bcrypt (cost ≥ 10) — ดู `service/user.go` และ `HandleLogin`
- ตรวจ flow บังคับเปลี่ยนรหัสครั้งแรก (`is_initial`) ว่า bypass ได้เฉพาะ 3 endpoint ที่จำเป็น (`/api/system/password`, `/api/auth/logout`, `/api/auth/session`)
- ตรวจรหัสผ่านเริ่มต้น: `connection.go` ต้อง generate จาก `crypto/rand` (มี `generateRandomPassword` แล้ว) และมีค่าตายตัว `pigate` เฉพาะเมื่อ DSN คือ `:memory:` (โหมดทดสอบ) เท่านั้น
- ตรวจ rate limit ของ `/api/auth/login`: token bucket ต่อ IP — ทดสอบด้วย `for i in $(seq 10); do curl -s -o /dev/null -w '%{http_code}\n' -X POST localhost:8081/api/auth/login -d '{}'; done` ต้องเห็น 429

**ความเสี่ยง:** session ไม่หมดอายุ = token ที่หลุด (ผ่าน XSS/log/network sniff) ใช้ได้ตลอดไป; token อ่อนแอ = ปลอม session เข้ายึด gateway ได้

**แนวทางแก้:**
- เพิ่ม TTL ให้ session ฝั่ง server (เก็บ `expiresAt` ใน map แล้วเช็คใน `IsSessionValid` + goroutine กวาดทิ้ง) และต่ออายุแบบ sliding เมื่อมี activity
- เปลี่ยน `_, _ = rand.Read(b)` ให้ panic หรือคืน error
- พิจารณาจำกัดจำนวน session ต่อผู้ใช้

### B. Authorization / RBAC

**ดูที่ไหน:** `internal/api/middleware.go` (`RoleReadOnlyMiddleware`, `SuperAdminMiddleware`), `internal/api/router.go`, `internal/service/user.go` (guard rails)

**ดูอย่างไร:**
- ยืนยันหลักการ **fail-closed**: role ที่ไม่รู้จัก/หายไปต้องถูกปฏิบัติเป็น read-only (ปัจจุบันทำถูก: `if role != model.RoleSuperAdmin`)
- ไล่ `router.go` ทีละบรรทัด — ทุก route ที่ mutate ต้องผ่าน `authRoute` หรือ `superAdminRoute`; route ที่คืนความลับ (export config) หรือสั่ง power ต้องเป็น `superAdminRoute` แม้แต่ GET
- **หา GET ที่มี side effect** — `RoleReadOnlyMiddleware` บล็อคเฉพาะ POST/PUT/DELETE/PATCH ดังนั้น GET ใดๆ ที่ไปเปลี่ยน state (เช่น scan Wi-Fi ที่สั่ง kernel ทำงาน) จะหลุดผ่าน role read-only — ตรวจ `HandleScanWifi` และ handler GET อื่นๆ ว่ายอมรับได้หรือไม่
- ตรวจ guard rails ใน `user.go`: ห้ามลดบทบาทตัวเอง, ห้ามลบ/ปิดตัวเอง, ต้องเหลือ super_admin ที่ active ≥ 1 เสมอ — มี unit test ครอบ (`user_test.go`)
- ตรวจว่า `AuthMiddleware` query DB ทุก request เพื่อให้การปิด/ลบบัญชีมีผลทันที (ทำแล้ว — คงพฤติกรรมนี้ไว้)

**ความเสี่ยง:** route ใหม่ที่ลืมครอบ middleware = ผู้ไม่ล็อกอินหรือ read-only แก้ firewall ได้

**แนวทางแก้:** ทุกครั้งที่เพิ่ม route ใหม่ ให้ใช้ helper `authRoute`/`superAdminRoute` เท่านั้น ห้าม `mux.Handle` ตรงๆ (ยกเว้น login/logout) และเพิ่ม test ใน `handlers_test.go` ที่ยิงทุก route โดยไม่มี token แล้ว expect 401

### C. Transport Security (จุดอ่อนเชิงโครงสร้างที่ใหญ่ที่สุดตอนนี้)

**ดูที่ไหน:** `cmd/pigate/main.go` (บรรทัด `http.ListenAndServe`), `handlers.go` (`http.SetCookie`), `frontend/src/services/authService.ts`

**ดูอย่างไร:**
- ปัจจุบันเสิร์ฟ **HTTP เปล่าทุก interface** (`":"+port`) และ cookie ตั้ง `Secure: false`
- ~~token ถูกส่งซ้ำสองทาง: HttpOnly cookie **และ** JSON body ที่ frontend เก็บลง `localStorage`~~ → **แก้แล้ว (cookie-only-session-auth-plan)**: token มากับ `Set-Cookie` (HttpOnly) ทางเดียว; frontend ไม่เก็บ token ใน `localStorage` อีก

**ความเสี่ยง:** ใครก็ตามใน LAN ที่ sniff ได้ (ARP spoof, rogue AP) เห็นรหัสผ่านและ token เป็น plaintext (แก้ด้วย TLS — ดูข้อ 1). ส่วนช่องทาง "XSS ขโมย token จาก `localStorage`" ปิดไปแล้วเพราะ token อยู่ใน HttpOnly cookie เท่านั้น

**แนวทางแก้ (เรียงตามความคุ้ม):**
1. รองรับ TLS: self-signed cert ที่ generate ตอน install + flag `-tls-cert`/`-tls-key` แล้วตั้ง `Secure: true`
2. เลิกส่ง token ใน response body — ใช้ HttpOnly cookie ทางเดียว (frontend เลิกอ่าน `data.token` และเลิกเก็บ `localStorage`; ใช้ `credentials: "include"` ใน fetch) — จะตัด vector "XSS ขโมย token" ทิ้งทั้งก้อน
3. อย่างน้อยที่สุด: bind เฉพาะ IP ของ LAN management interface แทน `0.0.0.0`

**C.1 CSRF (ตรวจคู่กับ cookie เสมอ)**
- หลังทำ cookie-only auth แล้ว auth มาทาง cookie **ทางเดียว** (ตัด `Authorization: Bearer` ทิ้ง) ดังนั้น CSRF ป้องกันด้วยกลไกเดียวคือ cookie เป็น `SameSite=Strict` (เบราว์เซอร์บล็อค cross-site request ทั้งหมด)
- **จุดที่ต้องเฝ้า (สำคัญขึ้นกว่าเดิม):** ตอนนี้ `SameSite=Strict` คือด่านเดียวที่กัน CSRF — ทุกรอบรีวิวต้องยืนยันว่า cookie login/logout ยังตั้ง `SameSite=Strict` (ห้ามลดเป็น `Lax`/`None`) เพราะไม่มี Bearer header เป็น backstop อีกแล้ว
- แนวทางเสริม (defense-in-depth): เช็ค `Origin`/`Sec-Fetch-Site` header สำหรับ request ที่ mutate — reject ถ้าเป็น cross-site

### D. Input Validation และ Config-File Injection

นี่คือหมวดที่ต้องละเอียดที่สุด เพราะ input ผู้ใช้ถูกเขียนลงไฟล์ config ของ OS จริง

**D.1 wpa_supplicant (`internal/kernel/wpa.go`)**
- `SanitizeWpaInput` ตัด `\n`, `\r`, `"` — ตรวจว่า **ทุกค่า** ที่ลง config ผ่านฟังก์ชันนี้ (ssid, psk) และตรวจว่าค่าที่ sanitize แล้วยังถูกครอบ `"` ในไฟล์
- คำถามที่ต้องถามทุกรอบรีวิว: มีฟิลด์ใหม่ (เช่น `country`, identity ของ EAP) ถูกเพิ่มเข้า `GenerateWpaConfig` โดยไม่ผ่าน sanitize หรือไม่?
- ตรวจว่าไฟล์เขียนแบบ atomic (temp + rename) และ permission ไม่กว้างกว่า 0600 เพราะมี PSK อยู่ข้างใน
- ตรวจข้อจำกัดของ wpa_supplicant เอง: psk แบบ quoted ต้องยาว 8–63 ตัว — ควร validate ที่ service layer เพื่อกัน config ที่ apply ไม่ผ่าน

**D.2 dnsmasq (`internal/kernel/dhcp_server.go`, `dns_server.go`)**
- ค่าที่ลงไฟล์: interface name, IP range, MAC, hostname ของ reservation, zone name, record name/value
- **จุดตรวจสำคัญ:** ค่า string เหล่านี้ถ้ามี `\n` จะกลายเป็น directive ใหม่ของ dnsmasq ได้ (config injection) — ตามรอยจาก handler → service → kernel ว่าทุกฟิลด์ผ่าน validation (regex / `net.ParseIP` / `net.ParseMAC`) ก่อนถึง `fmt.Sprintf` ที่ประกอบไฟล์
- มี safety net อยู่แล้ว: `dnsmasq --test` ตรวจ syntax ก่อน apply (ดู `validateDnsmasqConfig`) — แต่ **อย่าใช้แทน validation** เพราะ config ที่ inject มาอาจ syntax ถูกต้อง
- แนวทางแก้ถ้าพบช่อง: เพิ่ม regex whitelist ต่อฟิลด์ที่ service layer เช่น hostname `^[a-zA-Z0-9-]{1,63}$`, zone `^[a-z0-9.-]+$` และ reject แทนการ silently strip

**D.3 Firewall / Routing / QoS (`internal/service/firewall.go`, `routing.go`, `qos.go`)**
- ทุก address ต้องผ่าน `net.ParseIP`/`net.ParseCIDR`, port ต้องเป็น 1–65535, interface name ต้อง match กับ interface ที่มีจริง
- ตรวจว่าการเรียงกฎ nftables คงโครงสร้าง 4 ส่วนตาม `docs/tech_stack_design.md` §4.3 (sanity drop → audit log → dynamic accept → final drop) — การสลับลำดับคือบั๊กความปลอดภัยแม้ไม่มี "injection"
- ทดสอบใน mock mode: สร้าง policy ที่มีค่าแปลกๆ (CIDR ผิดรูป, port 0, ชื่อ interface ปลอม) ผ่าน API ตรงๆ ด้วย `curl` — ต้องได้ 400 ไม่ใช่ 500 หรือ apply สำเร็จ

**D.4 คำสั่ง exec ที่เหลืออยู่**
- รันคำสั่งนี้ทุกรอบรีวิว และไล่ดูผลทีละรายการ:
  ```bash
  grep -rn "exec.Command\|execCommand(" backend/internal backend/cmd --include='*.go' | grep -v _test.go
  ```
- ที่ยอมรับได้ปัจจุบัน (argument คงที่ ไม่มี user input ตำแหน่ง executable): `dnsmasq --test --conf-file=<tempfile ที่เราสร้างเอง>`, `modprobe ifb`, และ dhcpcd ผ่าน sudoers ที่จำกัดไว้ใน `install.sh`
- **ทุกการ exec ใหม่ = default reject** เว้นแต่พิสูจน์ได้ว่าไม่มี Netlink/D-Bus ทางเลือก และ argument ทุกตัวไม่มาจาก user input

### E. SQL / Database

**ดูที่ไหน:** `internal/db/repository.go`, `connection.go`

**ดูอย่างไร:**
- Grep หา SQL ที่ประกอบ string:
  ```bash
  grep -n "Sprintf" backend/internal/db/*.go
  ```
- ปัจจุบันพบ 3 จุด (บรรทัด ~388, ~398, ~1150 ของ `repository.go`) ที่ใช้ `fmt.Sprintf` ประกอบ `IN (%s)` — ตรวจว่า `%s` คือ **สตริง placeholder** (`?,?,?`) ที่สร้างจากจำนวน element ไม่ใช่ค่าจริง และค่าถูกส่งเป็น args แยก → ถ้าใช่ ถือว่าปลอดภัย ต้องยืนยันแบบนี้ทุกรอบ
- ค่า id ทั้งหมดต้องส่งผ่าน parameter binding (`?`) เสมอ

**ความเสี่ยง:** SQLite injection → อ่าน/แก้ users table → ยึดระบบ

**แนวทางแก้:** กติกาตายตัว — ค่าจาก user ห้ามอยู่ใน format string ของ SQL เด็ดขาด อนุญาต `Sprintf` เฉพาะการ generate placeholder

### F. การจัดการความลับ (Secrets)

**ดูที่ไหน:** `internal/api/handlers.go` (`maskInterfacePasswords`), `internal/service/backup.go`, `backup_crypto.go`, `internal/api/router.go` (export/import routes)

**ดูอย่างไร:**
- Wi-Fi password ใน DB เก็บ plaintext (จำเป็น เพราะต้อง generate wpa config) — ตรวจว่า **ทุก endpoint ที่คืนข้อมูล interface ผ่าน `maskInterfacePasswords` ก่อนเสมอ** grep หา `WifiPassword` ใน handlers ทุกจุด
- ตรวจว่า export config เป็น `superAdminRoute` (ใช่) และ backup ที่มีความลับรองรับการเข้ารหัส
- `backup_crypto.go`: AES-256-GCM + Argon2id (time=1, mem=64MiB, threads=4), salt/nonce จาก `crypto/rand`, error ตอน decrypt เป็น generic (กัน oracle) — โครงนี้ดีแล้ว ตรวจว่า parameter ไม่ถูกลดเกรดในอนาคต และการ import backup เก่าที่ไม่เข้ารหัสมีคำเตือน
- ตรวจว่า log (`log.Printf`) ไม่พิมพ์รหัสผ่าน — grep `password` ใน statement ที่ log; ตอนนี้ `wpa.go` log แค่ `HasPassword=%t` (ถูกต้อง) แต่ **`SendWpaCommand` log ตัว command เต็มๆ** — ถ้าอนาคตมี command ที่ฝัง PSK (เช่น `SET_NETWORK ... psk`) จะรั่วลง journal ทันที ต้อง redact
- ห้าม commit `.env` / key ใดๆ ตาม `CLAUDE.md`

**แนวทางแก้:** เพิ่ม test ที่ยืนยันว่า response JSON ของทุก interface endpoint ไม่มี password จริง; เพิ่ม redaction ใน `SendWpaCommand` ก่อน log

### G. DoS / Resource Limits

**ดูที่ไหน:** `internal/api/middleware.go` (rate limiter), `handlers.go` (ทุกจุดที่ decode JSON), `internal/logs/ringbuffer.go`, `cmd/pigate/main.go`

**ดูอย่างไร:**
- Rate limiter เก็บ limiter ต่อ IP ใน map **โดยไม่มีการกวาดทิ้ง** — ผู้โจมตีใน LAN spoof source ได้จำกัด แต่ IPv6 privacy address ทำให้ map โตได้เรื่อยๆ → ควรมี cleanup goroutine หรือ LRU
- Request body size limit: endpoint import config ครอบ `http.MaxBytesReader` 10 MB แล้ว (ดี) แต่ endpoint JSON อื่นๆ ยังไม่มี → ควรครอบ limit เล็กๆ (เช่น 1 MB) ที่ middleware กลางให้ครบทุก endpoint
- `http.Server` ถูกสร้างผ่าน `http.ListenAndServe` ตรงๆ = **ไม่มี ReadTimeout/WriteTimeout/IdleTimeout** → slowloris ค้าง connection ได้ → สร้าง `http.Server{}` เองพร้อม timeout
- Ring buffer log มีขนาดคงที่ (ดีแล้ว — กันทั้ง memory โตและ SD card สึก)

### H. Frontend

**ดูที่ไหน:** `frontend/src/services/*.ts`, `frontend/src/components/`, `frontend/src/pages/`

**ดูอย่างไร:**
- XSS sinks: `grep -rn "dangerouslySetInnerHTML" frontend/src` — ปัจจุบันมีจุดเดียวคือ `components/ui/chart.tsx` (โค้ด shadcn มาตรฐาน ฉีดเฉพาะ CSS ที่สร้างจาก config ภายใน ไม่ใช่ user input) — ตรวจซ้ำทุกรอบว่าไม่มีจุดใหม่ และไม่มีการใช้ `eval`/`new Function`
- Token storage: ดูหมวด C — เป้าหมายระยะยาวคือเลิกใช้ `localStorage`
- Role ที่เก็บฝั่ง client (`pigate_role`) เป็นแค่ UI hint — ยืนยันว่า **การบังคับสิทธิ์จริงอยู่ที่ backend เท่านั้น** (อยู่แล้ว) ห้ามให้ frontend เป็นด่านเดียว
- Mock mode: `IS_MOCK_MODE` ต้อง resolve เป็น false ใน production build — ตรวจ `services/config.ts` ว่าเงื่อนไขผูกกับ build env ไม่ใช่ runtime toggle ที่ผู้ใช้เปิดได้

### I. การติดตั้งและ OS Hardening

**ดูที่ไหน:** `install.sh`, `build.sh`, systemd unit / Polkit rules / sudoers ที่สคริปต์สร้าง

**ดูอย่างไร:**
- binary ต้องรันเป็น user `pigate` + capabilities ไม่ใช่ root — ตรวจว่าไม่มีโค้ดใหม่ที่ assume root (เช่น เขียนไฟล์ใน path ที่ pigate ไม่มีสิทธิ์แล้ว fallback แปลกๆ)
- sudoers entries ต้อง **จำกัดเป็นรายคำสั่ง+รายอาร์กิวเมนต์** (dhcpcd/dhclient เท่านั้น) ห้ามมี wildcard ที่กว้างขึ้น
- Polkit rules ต้องจำกัดเฉพาะ action ของ wpa_supplicant/systemd-resolved ที่จำเป็น
- ตรวจ systemd unit ว่าควรเพิ่ม hardening: `NoNewPrivileges=yes`, `ProtectSystem=strict` + `ReadWritePaths=` เฉพาะที่จำเป็น, `ProtectHome=yes`, `PrivateTmp=yes`
- ไฟล์ DB (`pigate.db`) permission ต้อง 0600 และ owner `pigate`

### J. Dependencies / Supply Chain

รันทุกรอบรีวิว:

```bash
cd backend && govulncheck ./... && staticcheck ./... && gosec ./...
cd ../frontend && yarn audit
```

- Go deps ต้อง pin ผ่าน `go.sum`; การเพิ่ม dependency ใหม่ต้องมีเหตุผล (นโยบายโปรเจค: stdlib / golang.org/x / โมดูลที่รู้จักดีเท่านั้น)
- เทียบ diff ของ `go.mod`/`yarn.lock` กับรอบก่อนว่ามีอะไรเพิ่ม

---

## 5. Scorecard ประเมินรายหัวข้อมาตรฐาน (อัปเดตทุกรอบรีวิว)

ตารางนี้สรุปสถานะของโปรเจคตามหัวข้อความปลอดภัยมาตรฐาน 12 หัวข้อ ให้ผู้รีวิวรอบถัดไป
**ประเมินเกรดใหม่ทุกรอบ** โดยใช้วิธีตรวจจากหมวด checklist ที่อ้างอิงไว้ (A = ดีมาก, F = ยังไม่มี)

| หัวข้อ | เกรด | สรุปสถานะ | วิธีตรวจ (หมวด) |
|---|---|---|---|
| Authentication | B+ | bcrypt, error แบบ generic ไม่ leak account, บังคับเปลี่ยนรหัสครั้งแรก, รหัสเริ่มต้นสุ่ม; error ของ `rand.Read` fail-closed แล้ว; หักคะแนน: ไม่มี lockout ต่อบัญชี | A |
| Session Management | A- | มี sliding idle TTL (15 นาที) + absolute cap (7 วัน) + cap ต่อผู้ใช้ (5, evict ตัวเก่าสุด) + sweeper; เช็ค DB ทุก request, purge เมื่อลบบัญชี/import config | A |
| Authorization | A- | RBAC แบบ fail-closed, guard rails ครบพร้อม test; route ใหม่ทั้งหมด (port-forward, VLAN, event/traffic log, metrics SSE) ครอบ middleware ถูกต้อง, clear audit log เป็น super_admin เท่านั้น; ยังต้องระวัง GET ที่มี side effect | B |
| Password Hashing | A- | bcrypt cost 10 ใช้สม่ำเสมอทั้ง create/login/reset | A |
| CSRF | B | cookie-only auth กันด้วย `SameSite=Strict` กลไกเดียว (ตัด Bearer แล้ว); ยังแนะนำเพิ่มเช็ค `Origin`/`Sec-Fetch-Site` เป็น defense-in-depth (ยังไม่ทำ) | C.1 |
| Cookie Security | A- | `HttpOnly` + `SameSite=Strict`, `Secure` ตั้งราย request จาก `r.TLS` (มีผลจริงภายใต้ HTTPS), รวมศูนย์ที่ `setSessionCookie`, ไม่มี token ใน `localStorage` | C |
| CORS | A- | whitelist แบบ exact match ไม่ใช่ wildcard; dev origins ถูก gate ด้วย `-allow-dev-cors` (ปิดใน production); ตัด `Authorization` ออกจาก allowed headers | B, C |
| Rate Limiting | B | limiter ที่ login มี `lastSeen` eviction ต่อรายการ + hard cap 4096; endpoint แพง (scan/apply) ยังไม่มี limit | A, G |
| File Upload | A- | มีทางเดียวคือ config import: super_admin only, cap 10 MB, transaction เดียว + snapshot, purge session หลัง import; ไม่มีการเขียนไฟล์ upload ลง filesystem | F |
| Secrets | A- | mask password ทุก response ของ interface (ตรวจแล้ว), export จำกัด super_admin, backup crypto ถูกตำรา, `redactWpaCommand` ตัด PSK ก่อน log, traffic log parse เฉพาะ packet header (ไม่มี payload/ความลับ) | F |
| TLS/HTTPS | A- | **มีแล้ว** — self-signed ECDSA P-256 cert generate ตอน startup (key 0600, ไม่เก็บใน DB/backup), HTTPS เป็นหลักที่ :443 (TLS 1.2+) + server timeouts, HTTP→HTTPS 308 redirect, มี plain-HTTP fallback ท้ายสุด; หักคะแนน: self-signed อย่างเดียว (ยังไม่มี ACME/CA), ตัด HSTS ออกเพื่อคง fallback | C |
| Input Validation | B | SQL parameterized, wpa sanitize, `ParseIP`/`ParseMAC`; เส้นทาง dnsmasq ฝั่ง DNS (zone/record/reservation/interface) validate ครบ 3 ชั้น (handler + import + generation); **ช่องใหม่: ฟิลด์ scope ของ DHCP (`startIp`/`endIp`/`gateway`/`netmask`/`dns1`/`dns2`) ยังไม่ validate → inject directive dnsmasq ได้ (finding 11)** | D, E |

**ลำดับความสำคัญของการแก้:** ~~TLS~~ (**เสร็จแล้ว** — https-server-foundation) → ~~session TTL~~
(**เสร็จแล้ว** — server-side-session-ttl) → **validate scope ของ DHCP (finding 11 — ใหม่, config injection)** →
อัปเกรด Go toolchain เป็น ≥1.26.5 (finding 12) → เพิ่มเช็ค `Origin`/`Sec-Fetch-Site` กัน CSRF

## 6. สิ่งที่ทำได้ดีแล้ว (ณ รอบรีวิวนี้ — คงไว้ อย่าถอยหลัง)

1. **ไม่ shell out สำหรับงาน kernel** — ใช้ `google/nftables`, `vishvananda/netlink`, D-Bus, unix socket ตรง; exec ที่เหลือมี argument คงที่ทั้งหมด → ตัด command injection เชิงโครงสร้าง
2. **RBAC แบบ fail-closed** — role ไม่รู้จัก = read-only; endpoint ความลับ/power เป็น super_admin เท่านั้นแม้แต่ GET
3. **Guard rails ระบบผู้ใช้** — ห้ามลดสิทธิ์/ลบ/ปิดตัวเอง, บังคับเหลือ super_admin active ≥ 1, บังคับเปลี่ยนรหัสครั้งแรก, ตรวจ DB ทุก request ทำให้ปิดบัญชีมีผลทันที
4. **bcrypt สำหรับรหัสผ่าน + รหัสเริ่มต้นสุ่มจาก `crypto/rand`** (hardcode เฉพาะโหมดทดสอบ in-memory)
5. **Rate limit ที่ login** กัน brute force ระดับพื้นฐาน
6. **Backup encryption ถูกหลักวิชา** — AES-256-GCM + Argon2id, พารามิเตอร์เก็บใน meta, error แบบ generic กัน oracle, export/import จำกัด super_admin
7. **SQL ใช้ parameter binding ทั่วทั้ง repo** (จุด `Sprintf` มีเฉพาะ placeholder ของ IN-clause)
8. **`SanitizeWpaInput` กัน newline/quote injection** ใน wpa config + มี test
9. **รันด้วย capabilities ไม่ใช่ root** + user แยก + Polkit/sudoers จำกัดขอบเขต
10. **dnsmasq `--test` ก่อน apply** เป็น safety net อีกชั้น
11. **Frontend แทบไม่มี XSS sink** — จุด `dangerouslySetInnerHTML` เดียวเป็นของ shadcn chart ที่ไม่รับ user input
12. **มี TLS เป็นค่าเริ่มต้น** — self-signed ECDSA P-256 cert generate อัตโนมัติตอน startup (private key 0600, ไม่เก็บใน DB/backup), HTTPS เป็นหลักที่ :443 (TLS 1.2+) + server timeouts ครบ, HTTP→HTTPS 308 redirect, มี plain-HTTP fallback เพื่อไม่ให้ cert ล้มเหลวแล้วแอดมินเข้าไม่ได้
13. **Session ฝั่ง server แข็งแรง** — sliding idle TTL + absolute cap + cap ต่อผู้ใช้ + sweeper และมี `SessionAlive` สำหรับ SSE ที่ไม่ต่ออายุตอน heartbeat
14. **เส้นทาง dnsmasq ฝั่ง DNS validate ครบ** — whitelist ของ zone/record/reservation/interface บังคับ 3 ชั้น (handler, backup import, generation-time) พร้อม test (เหลือเฉพาะ scope fields เป็นช่องเดียว — finding 11)
15. **Security headers + body cap** — CSP เข้ม (`script-src 'self'`), `X-Frame-Options: DENY`, nosniff, `Referrer-Policy: no-referrer` ทุก response; body cap กลาง 1 MB (import คง 10 MB); map ของ rate limiter มีขอบเขตแล้ว (idle eviction + hard cap 4096); ตัด HSTS ออกโดยตั้งใจเพื่อคง HTTP fallback
16. **Port-forward / NAT สร้างผ่าน netlink expression + validate ก่อน persist** — `ValidatePortForward` (interface/protocol/IPv4/port-range/conflict) รันที่ชั้น repository และ import; กฎ DNAT/SNAT เป็น expression ของ google/nftables ไม่ใช่ string ของ shell — ไม่มี surface ให้ inject

## 7. จุดที่ต้องปรับปรุง (Findings เรียงตามความรุนแรง)

| # | ระดับ | ปัญหา | ที่อยู่ | แนวทางแก้ |
|---|---|---|---|---|
| 1 | ~~สูง~~ **แก้แล้ว** | ~~ไม่มี TLS — รหัสผ่าน/token วิ่งเป็น plaintext ใน LAN, cookie `Secure:false`~~ → self-signed ECDSA cert generate ตอน startup (`service/tls_cert.go`), HTTPS เป็นหลักที่ :443 (TLS 1.2+), HTTP→HTTPS 308 redirect, cookie `Secure` จาก `r.TLS`; คง plain-HTTP fallback เพื่อไม่ให้ cert ล้มเหลวแล้วแอดมินเข้าไม่ได้ | `cmd/pigate/main.go`, `service/tls_cert.go`, `api/session.go`, `install.sh` | **เสร็จ** (https-server-foundation) |
| 2 | ~~สูง~~ **แก้แล้ว** | ~~token เก็บใน `localStorage` และถูกส่งใน JSON body ทั้งที่มี HttpOnly cookie แล้ว — XSS ใดๆ ขโมย session ได้~~ → cookie-only auth: `LoginResponse` ไม่มี `token`, frontend เลิกเก็บ `localStorage` (เหลือ flag `pigate_logged_in` ที่ไม่ลับ), `AuthMiddleware`/logout อ่าน cookie ทางเดียว | `authService.ts`, `HandleLogin`, `middleware.go` | **เสร็จ** (cookie-only-session-auth-plan) — cookie เป็นช่องทางเดียว, `credentials: "include"` |
| 3 | ~~กลาง–สูง~~ **แก้แล้ว** | ~~session ไม่มีวันหมดอายุฝั่ง server จนกว่าจะ restart~~ → `api/session.go`: sliding idle TTL (15 นาที) + absolute cap (7 วัน) + cap ต่อผู้ใช้ (5, evict ตัวเก่าสุด) + `StartSessionSweeper`; SSE heartbeat ใช้ `SessionAlive` เพื่อไม่ให้ stream ที่เปิดค้างต่ออายุ session ตลอดไป | `api/session.go`, `middleware.go` | **เสร็จ** (server-side-session-ttl) |
| 4 | ~~กลาง~~ **แก้แล้ว** | ~~ไม่มี server timeouts (slowloris); body size limit มีเฉพาะ import (10 MB) ส่วน endpoint อื่นไม่มี~~ → timeout เพิ่มแล้วตั้งแต่งาน HTTPS; เพิ่ม `BodyLimitMiddleware` cap 1 MB ทุก endpoint (import คง 10 MB) และ SSE log stream เคลียร์ write deadline รายคอนเนกชันเพื่อไม่ให้ `WriteTimeout` ตัดทุก 60 วินาที | `main.go`, `middleware.go`, `handlers.go` | **เสร็จ** (http-server-hardening-plan) |
| 5 | ~~กลาง~~ **แก้แล้ว** | ~~rate limiter map โตได้ไม่จำกัด (ไม่มี eviction)~~ → เพิ่ม `lastSeen` รายรายการ + `StartLimiterSweeper` (idle 10 นาที) + hard cap 4096; key ด้วย `net.SplitHostPort` | `middleware.go` | **เสร็จ** (rate-limiter-eviction-plan) |
| 6 | ~~กลาง~~ **แก้แล้ว** | ~~ไม่มี security headers (`Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`) บนหน้า SPA~~ → `SecurityHeadersMiddleware` ตั้ง CSP (`script-src 'self'` เข้ม) + XFO + nosniff + Referrer-Policy ทุก response; ตัด HSTS ออกโดยตั้งใจเพื่อคง HTTP fallback | middleware | **เสร็จ** (security-headers-middleware-plan) |
| 7 | ~~ต่ำ–กลาง~~ **แก้แล้ว** | ~~`SendWpaCommand` log command เต็ม อาจรั่ว PSK ใน journal~~ → `redactWpaCommand` ตัด argument ของคำสั่งที่มี keyword secret (psk/password/wep_key/sae_password) ก่อน log ทั้ง request และ response | `kernel/wpa.go` | **เสร็จ** (security-hardening-cleanups-plan) |
| 8 | ~~ต่ำ~~ **แก้แล้ว** | ~~`_, _ = rand.Read(b)` เพิกเฉย error ตอนสร้าง token~~ → `generateRandomToken` คืน `(string, error)`; login และจุดสร้าง resource ID ทั้ง 7 fail-closed ด้วย 500 แทนการคืนค่าที่เดาได้ | `handlers.go` | **เสร็จ** (security-hardening-cleanups-plan) |
| 9 | ~~ต่ำ~~ **แก้แล้ว** | ~~CORS อนุญาต origin dev (`localhost:5173`) แม้ใน production binary~~ → echo dev origin เฉพาะเมื่อมี flag `-allow-dev-cors` (default off); production ไม่ echo cross-origin | `middleware.go` | **เสร็จ** (security-hardening-cleanups-plan) |
| 10 | ~~ต่ำ~~ **แก้แล้ว** | ~~ไม่มี audit log ว่าใครแก้ config อะไรเมื่อไร~~ → `EventLogService` (`SystemEvent.Actor`) มีอยู่แล้ว จึงเป็นการปิดช่องว่าง coverage — QoS (เดิม log 0) + address/service/DNS zone+record/DNS settings/DHCP reservation+scope/interface-reset บันทึก event พร้อม actor แล้ว | service layer / `handlers.go` | **เสร็จ** (security-hardening-cleanups-plan) |
| 11 | กลาง | **DHCP scope config injection (ใหม่).** ฟิลด์ `startIp`/`endIp`/`gateway`/`netmask`/`dns1`/`dns2` ถูกเขียนลง `pigate-dhcp.conf` (`dhcp-range=`/`dhcp-option=`) โดย **ไม่มี validation** ทั้งบน `POST`/`PUT /api/dhcp/configs` **และ** บน backup import — ค่าที่มี newline จะ inject directive dnsmasq ตามใจ (เช่น `dhcp-script=/tmp/x` → รันคำสั่งทุกครั้งที่มี lease event). Reservation กับ DNS record validate แล้ว แต่เส้นทาง scope ตกหล่น. `dnsmasq --test` ผ่านเพราะบรรทัดที่ inject มา syntax ถูกต้อง. ทดสอบจริง: `gateway:"192.168.1.1\ndhcp-script=…"` → HTTP 200 และเก็บลง DB ตามตัวอักษร | `api/handlers.go` (`HandleCreateDHCPConfig`/`HandleUpdateDHCPConfigByID`), `service/backup.go` (ลูป validation ของ import), `kernel/dhcp_server.go` | เพิ่ม `ValidateDhcpConfig` (interface ผ่าน `ValidateInterfaceName`, IP ผ่าน `net.ParseIP`) แล้วเรียกทั้งใน 2 handler, ในลูป import ถัดจาก `ValidateReservation`, และเป็น defense-in-depth ตอน generate ใน `dhcp_server.go` (skip/reject scope ที่ผิดเหมือน reservation ที่บรรทัด 92) |
| 12 | ต่ำ | **Go stdlib TLS vuln (ใหม่).** `govulncheck` แจ้ง GO-2026-5856 (Encrypted Client Hello privacy leak ใน `crypto/tls`) เข้าถึงได้ผ่าน `http.Server.ServeTLS` ที่เพิ่งเพิ่ม. PiGate ใช้ self-signed ไม่มี ECH จึงเสี่ยงจริงน้อย แต่ call path มีอยู่ | toolchain (`go1.26.4`) | build ใหม่ด้วย Go ≥ 1.26.5 (รุ่นที่แก้แล้ว) แล้วรัน `govulncheck` ยืนยันว่าเคลียร์ |

> เมื่อแก้ finding ใดแล้ว ให้ย้ายไปหมวด 6 พร้อมระบุ commit และปรับเกรดในหมวด 5 — เอกสารนี้คือ living document

## 8. ขั้นตอนทำซ้ำ (สรุปสำหรับผู้รีวิวรอบถัดไป)

1. อ่าน `docs/tech_stack_design.md` + เอกสารนี้ (โดยเฉพาะหมวด 7 ว่า finding เก่าแก้หรือยัง)
2. Build + `go test ./...` + รัน mock mode
3. รันชุดเครื่องมือหมวด J (govulncheck / staticcheck / gosec / yarn audit) — เก็บ output แนบรายงาน
4. รัน grep ประจำ:
   ```bash
   cd backend
   grep -rn "exec.Command\|execCommand(" internal cmd --include='*.go' | grep -v _test.go
   grep -n  "Sprintf" internal/db/*.go
   grep -rn "math/rand" internal cmd --include='*.go' | grep -v _test.go
   grep -rn "dangerouslySetInnerHTML\|eval(" ../frontend/src
   ```
5. ไล่ checklist หมวด A–J โดยเน้น **diff ตั้งแต่รอบรีวิวก่อน** (`git log --stat <last-review-tag>..HEAD`) — ไฟล์ใหม่ใน `kernel/` และ route ใหม่ใน `router.go` คือจุดเสี่ยงอันดับหนึ่ง
6. ทดสอบเชิงพฤติกรรมผ่าน API จริง (mock mode): login rate limit, ยิง endpoint mutate ด้วย role read-only ต้องได้ 403, ยิงโดยไม่มี token ต้องได้ 401, ส่ง input ผิดรูป (CIDR/port/hostname) ต้องได้ 400
7. อัปเดตหมวด 5–7 ของเอกสารนี้ (เกรด scorecard, สิ่งที่ทำได้ดี, findings) + วันที่บนหัวเอกสาร แล้วสรุปผลเป็นรายงานสั้น (finding ใหม่, finding เดิมที่ปิดแล้ว, ความเสี่ยงคงค้าง)
