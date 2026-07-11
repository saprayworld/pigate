# dnsmasq Input Validation — กัน config injection ที่ DNS zone/record + DHCP hostname

> แผนงานแก้ security review finding 7 (Medium, ป้าย **new**): ค่าที่ผู้ใช้กรอก (zone name,
> record name/value, DHCP device name) ถูกเขียนลง dnsmasq config ด้วย `fmt.Sprintf` โดยมีแค่
> `TrimSpace` — ซึ่ง**ไม่ตัด newline กลางสตริง** ค่าอย่าง `1.2.3.4\naddress=/evil/6.6.6.6`
> ฉีด directive ใหม่เข้าไฟล์ได้ และ `dnsmasq --test` จับไม่ได้เพราะบรรทัดที่ฉีดเป็น config ที่ valid
> เป้าหมาย: validate แบบ whitelist (**reject ไม่ใช่ strip**) เลียนแบบวินัยของ `SanitizeWpaInput`
>
> เขียนเมื่อ: 2026-07-11 · Reference branch: `fix/dnsmasq-input-validation` · Issue: #36
> finding ความรุนแรงสูงสุดที่เหลือหลังชุด roadmap ข้อ 5 (#33/#34/#35)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- ค่าที่ลงไฟล์ dnsmasq ทุกตัวผ่าน whitelist validator ก่อนเสมอ — อักขระนอกชุดที่อนุญาต
  (โดยเฉพาะ `\n` `\r`) ทำให้ **request ถูกปฏิเสธ** (400) ไม่ใช่ถูกลบเงียบๆ
- ครอบ **3 ทางเข้า**: (1) create/update handlers, (2) config **import** (`backup.go`),
  (3) generation-time (`ApplyZones`/`ApplyConfig`) เป็น defense-in-depth
- ไม่มี regression กับ record ปกติ: A/AAAA (IP), CNAME/FQDN, MX (pref+target), TXT, PTR,
  forward zone (`ForwardTo`), reservation ที่มีชื่อเว้นวรรค (เดิมแปลงเป็น `-`)

**Out of scope (ตัดออกชัดเจน):**
- Validate ค่าที่ไม่ได้มาจากผู้ใช้ (upstream servers จาก System DNS, interface name จาก OS) —
  ไม่ใช่ผิวโจมตีของ finding นี้
- ปรับ error message ฝั่ง frontend ให้สวย (frontend แสดง `message` จาก 400 อยู่แล้ว —
  พอสำหรับงานนี้; ถ้าจะทำ inline validation ฝั่ง UI เป็นงานเสริมแยก)
- แก้ TXT record ให้รองรับ escaped quote/ค่าซับซ้อน — งานนี้แค่กัน injection ไม่ขยาย feature

## 1. Current State (สำรวจโค้ดจริง 2026-07-11)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| DNS zone config เขียนด้วย Sprintf + TrimSpace เท่านั้น | **ช่องโหว่** | `backend/internal/kernel/dns_server.go:66` (zoneName), `:83` (`local=/zone/`), `:95/108/118/121/124` (record value ต่อ type), `:131` (`server=/zone/ForwardTo`) |
| DHCP reservation name เขียนด้วย Sprintf | **ช่องโหว่** | `backend/internal/kernel/dhcp_server.go:112-118` — `ReplaceAll(name," ","-")` ตัดเว้นวรรคแต่**ไม่ตัด `\n`** |
| `dnsmasq --test` จับ injection ไม่ได้ | ยืนยันแล้ว (บรรทัดที่ฉีดเป็น config ถูกต้อง) | `dns_server.go:26-33`, `dhcp_server.go:52-59` |
| Handlers ไม่ validate อะไรเลย | ต้องเพิ่ม | `handlers.go:2269` (CreateDNSZone), `:2293` (UpdateDNSZone), `:2355` (CreateDNSRecord), `:2379` (UpdateDNSRecord), `:1454` (CreateDHCPReservation), `:1475` (UpdateDHCPReservation) — decode → repo ตรงๆ |
| **Import bypass handlers** | **ต้องครอบ** | `backend/internal/service/backup.go` — import เขียน DB ตรง (`cfg.DnsZones`, records, `DhcpReservations`) แล้ว `InitApplyConfig` (`:490-494`) → ApplyZones/ApplyConfig; validation ที่ handler อย่างเดียวถูกข้าม |
| Service ApplyAll เป็น choke point ก่อนถึง kernel | จุดใส่ defense-in-depth | `service/dns_server.go:29-55` (`ApplyAll`→`ApplyZones`), `service/dhcp_server.go:33-72` (`ApplyAll`→`ApplyConfig`) |
| Pattern validator ที่มีอยู่ | เลียนแบบวินัย แต่ต้อง reject | `backend/internal/kernel/wpa.go:18-24` (`SanitizeWpaInput` — strip `\n\r"`) |
| model package (แชร์ทุก layer ไม่มี dep) | จุดวาง validator ใหม่ | `backend/internal/model/dns_server.go` (DNSZone/Record + Input), `model/types.go:247` (DhcpReservation) |
| Tests เดิมของ DNS/DHCP | มีให้ต่อยอด | `service/dns_test.go`, `kernel/wpa_test.go` (แบบอย่างการเทสต์ sanitizer) |
| frontend / router / install.sh / OpenAPI | เกือบไม่เกี่ยว | เพิ่มแค่คำอธิบาย 400 ใน OpenAPI; ไม่มี route/permission ใหม่ |

สรุป: ช่องโหว่อยู่ที่ kernel config writer แต่**จุดแก้ที่ถูกต้องคือ validate ก่อนถึงตรงนั้น** —
วาง validator ใน `model`, บังคับที่ handler + import (reject) และ skip+log ที่ Apply* (กันชั้นท้าย)

## 2. Technical Approach

**กลไกที่เลือก: whitelist validator ใน `model` เรียกจาก 3 ชั้น (reject-not-strip)**

วาง validator เป็น pure function ใน `model` (แชร์ทุก layer, ไม่มี dependency):

```go
// backend/internal/model/dns_validate.go
var reZoneName = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)   // ไม่มี _ ตาม RFC hostname
var reHostLabel = regexp.MustCompile(`^[a-zA-Z0-9-]{1,63}$`)

func ValidateDNSZone(z DNSZone) error { /* zoneName ผ่าน reZoneName; ForwardTo = IP/host:port */ }
func ValidateDNSRecord(r DNSRecord) error {
    // name: "" | "@" | reHostLabel ; ตาม Type:
    //   A/AAAA → net.ParseIP(value) และตรง family
    //   CNAME  → value เป็น FQDN charset (reZoneName ต่อ label)
    //   MX     → "<pref> <target>": pref เป็นเลข, target FQDN
    //   TXT    → ห้าม \n \r และ "; ยาว ≤255
    //   PTR    → FQDN charset
    // Type อื่น → reject (เดิม switch เงียบๆ ทิ้ง)
}
func ValidateReservationName(name string) error { /* "" ok(→default) | reHostLabel หลังแทนที่ space */ }
```

หัวใจ: ทุก validator **reject** เมื่อเจอ `\n`/`\r`/อักขระนอกชุด — เพราะ regex เป็น full-match
(`^...$`) newline จะทำให้ไม่ match เอง (Go regexp `$` = ปลายสตริง ไม่ใช่ปลายบรรทัด by default)

เรียก 3 ชั้น:
1. **Handler** (`handlers.go`): validate หลัง decode ก่อน `repo.Create*` → 400 + message ชัด
2. **Import** (`backup.go`): validate ทุก zone/record/reservation **ก่อน** single-txn restore →
   ถ้ามีตัวใดพัง reject ทั้ง import (fail-closed — import atomic อยู่แล้ว)
3. **ApplyZones/ApplyConfig** (kernel): ถ้า record/reservation ตัวใด fail validate → **skip + log**
   (ไม่ทำทั้ง apply พังเพราะ 1 record เสีย) — ชั้นสุดท้ายกันค่าที่หลุด DB เก่าไปถึงไฟล์ไม่ได้

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *Strip อักขระอันตราย (เลียน `SanitizeWpaInput` ตรงๆ)* — ตัดทิ้ง: review ระบุชัด
   "reject rather than strip"; strip แล้ว `1.2.3.4\naddress=...` กลายเป็น `1.2.3.4address=...`
   ผู้ใช้ได้ค่าที่ตัวเองไม่ได้ตั้งใจเงียบๆ — reject ให้ feedback ตรงกว่าและปลอดภัยกว่า
2. *Validate ที่ handler อย่างเดียว* — ตัดทิ้ง: import (`backup.go`) เขียน DB ตรงข้าม handler →
   backup ที่ crafted ฉีดได้ (super_admin แต่คือ trust boundary ที่ finding นี้พูดถึงพอดี)
3. *Validate ที่ kernel ApplyZones อย่างเดียว (จุดที่ Sprintf อยู่)* — ตัดทิ้ง: kernel layer
   ควรเป็น OS-only ไม่ถือ business rule (CLAUDE.md); และ error ที่ apply-time ให้ UX แย่
   (ผู้ใช้กด save ผ่าน แล้วพังตอน generate) — validate ที่ handler ให้ 400 ทันทีดีกว่า
   จึงใช้ kernel เป็น **defense-in-depth (skip+log)** ไม่ใช่ด่านหลัก
4. *วาง validator ใน service layer* — ตัดทิ้ง: handler อยู่ api layer, import อยู่ service,
   apply อยู่ kernel — ทั้งสามเรียกได้สะอาดสุดถ้า validator อยู่ `model` (ชั้นล่างสุดที่ทุกคน
   import อยู่แล้ว ไม่สร้าง import cycle)

**Pattern ที่ยึด:** วินัย reject ของ `SanitizeWpaInput` (`kernel/wpa.go:18`) + สไตล์ test ของ
`kernel/wpa_test.go`; regex full-match กัน newline

## 3. Steps (เรียงชั้นในสุด → นอกสุด)

### Step 1 — validator ใน model
**File:** `backend/internal/model/dns_validate.go` (ไฟล์ใหม่)
`ValidateDNSZone`, `ValidateDNSRecord`, `ValidateReservationName` ตาม §2 — คืน `error`
พร้อม message อ่านรู้เรื่อง (เช่น `"zone name contains invalid characters"`)

### Step 2 — defense-in-depth ที่ kernel config writer
**File:** `backend/internal/kernel/dns_server.go:61-135`, `backend/internal/kernel/dhcp_server.go:108-121`
- ใน loop zone/record: `if err := model.ValidateDNSRecord(rec); err != nil { log + continue }`
  (zone ก็เช็ค `ValidateDNSZone` แล้ว skip ทั้ง zone ถ้า zoneName พัง)
- ใน loop reservation: `if err := model.ValidateReservationName(name); err != nil { log + continue }`
> **สิ่งที่ไม่ต้องทำ:** ไม่ต้องแตะ `mock.go` — mock DNS/DHCP ไม่เขียนไฟล์ dnsmasq
> (ไม่มี Sprintf-to-file) จึงไม่มีผิวฉีด; validator หลักอยู่ที่ handler/import ครอบ mock mode อยู่แล้ว

### Step 3 — บังคับที่ handlers
**File:** `backend/internal/api/handlers.go`
- `:2276` CreateDNSZone, `:2307` UpdateDNSZone → `ValidateDNSZone(zone)` ก่อน `repo.Create/Update` → 400
- `:2363` CreateDNSRecord, `:2393` UpdateDNSRecord → `ValidateDNSRecord(record)` → 400
- `:1461` CreateDHCPReservation, `:1475` UpdateDHCPReservation → `ValidateReservationName` → 400

### Step 4 — บังคับที่ import (fail-closed)
**File:** `backend/internal/service/backup.go` (ก่อน single-txn restore ที่เขียน DNS/DHCP)
วน validate `cfg.DnsZones` (+ records ในแต่ละ zone) และ `cfg.DhcpReservations` ทั้งหมด →
ถ้าตัวใด error return ยกเลิก import ทั้งก้อนพร้อม reason (ระบุ zone/record ที่ผิด) —
ไม่แตะ DB (import atomic อยู่แล้ว)

### Step 5 — Tests
**File:** `backend/internal/model/dns_validate_test.go` (ใหม่) + เสริม `service/dns_test.go`
- validator: newline/`;`/อักขระแปลก → error; A ที่ value ไม่ใช่ IP → error; ค่า valid ทุก type → ok
- **injection case ตรงๆ**: `Value = "1.2.3.4\naddress=/evil/6.6.6.6"` → `ValidateDNSRecord` error
- handler: POST record ที่มี newline → 400 (ผ่าน httptest)
- import: backup ที่มี zone ฉีด → import ถูก reject, DB ไม่เปลี่ยน
- kernel skip: record พัง 1 ตัวใน ApplyZones → ตัวอื่นยังลงไฟล์, ตัวพังไม่โผล่ (mock/temp file assert)

### Step 6 — OpenAPI ทั้งสองไฟล์
**File:** `docs/openapi.yaml` + `frontend/public/openapi.yaml`
เพิ่ม response 400 + คำอธิบาย charset ที่อนุญาต ให้ POST/PUT ของ dns zones/records และ
dhcp reservations

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| POST/PUT | `/api/dns/zones`, `/api/dns/zones/{id}` | authRoute (super_admin แก้ได้) | เพิ่ม 400 เมื่อ zoneName/ForwardTo ผิด charset |
| POST/PUT | `/api/dns/zones/{id}/records`, `/api/dns/records/{id}` | authRoute | เพิ่ม 400 เมื่อ name/value ผิดตาม type |
| POST/PUT | `/api/dhcp/reservations`, `/api/dhcp/reservations/{id}` | authRoute | เพิ่ม 400 เมื่อ deviceName ผิด charset |
| POST | `/api/system/config/import` | super_admin | reject ทั้ง import ถ้ามี DNS/DHCP entry ผิด (เดิม restore ผ่านเงียบ) |

`-disable-edit` mode: ไม่กระทบ — เป็น mutation ที่ `DisableEditMiddleware` บล็อกอยู่แล้ว

## 5. Cautions

1. **`TrimSpace` ไม่ตัด newline กลางสตริง** — นี่คือรากของช่องโหว่: `"1.2.3.4\naddress=..."`
   ผ่าน TrimSpace ไม่เปลี่ยน (ตัดแค่หัว/ท้าย) → ต้อง validate ทั้งค่าไม่ใช่ trim; regex
   full-match `^...$` โดย**ไม่ตั้ง flag `(?m)`** จึงกัน `\n` ได้ (ใน Go `$` = ปลายข้อความ)
2. **`dnsmasq --test` ไม่ใช่ตัวกัน injection** — บรรทัดที่ฉีด (`address=/evil/...`) เป็น config
   ถูกต้อง `--test` ผ่านสบาย → ห้ามพึ่ง validateDnsmasqConfig เป็นด่านความปลอดภัย มันกัน
   syntax error เฉยๆ; ด่านจริงคือ whitelist ก่อน Sprintf
3. **Import ต้อง validate ก่อนแตะ DB** — ถ้า validate หลังเขียน DB บางส่วน จะได้สถานะครึ่งๆ
   (import atomic เพื่อกันตรงนี้อยู่แล้ว — วาง validation loop ไว้ต้น flow ก่อน txn เริ่ม)
4. **อย่าทำให้ record เก่าที่ valid อยู่แล้วกลายเป็น invalid** — เช่น device name ที่มีเว้นวรรค
   (เดิมแปลงเป็น `-` ที่ `dhcp_server.go:117`): validator ต้องเช็ค**หลัง**แทนที่ space→`-`
   หรืออนุญาต space แล้วให้ writer แปลง — ไม่งั้น reservation เดิมของผู้ใช้จะ import ไม่ผ่าน
   → เลือก: `ValidateReservationName` อนุญาต space (จะถูกแปลงตอน generate) แต่ reject `\n`/อักขระคุม
5. **CNAME/MX มี normalization ที่ generate-time** (`dns_server.go:104-118` เติม zone, แยก pref) —
   validator ต้องรับ input **ก่อน** normalize (เช่น MX เป็น `"10 mail"` หรือ `"mail"` เดี่ยว);
   ให้ยึดรูปแบบที่ writer รองรับจริง อย่า validate เข้มกว่าที่ writer ยอมรับ ไม่งั้น block ของถูก
6. **Type ที่ไม่รู้จักเดิมถูกทิ้งเงียบ** (`switch` ที่ `dns_server.go:92` ไม่มี default) —
   validator ควร reject type นอก A/AAAA/CNAME/MX/TXT/PTR ที่ handler เพื่อ feedback ชัด
   (ไม่ใช่ปล่อยให้เงียบหายตอน generate)
7. **ทดสอบบนอุปกรณ์จริง**: DNS/DHCP คุมการจ่าย IP/แก้ชื่อของทั้งวง — ถ้า validator เข้มเกิน
   จน apply ไม่ผ่าน อาจทำ DHCP หยุดจ่าย lease → ทดสอบ mock + embedded build ให้ครบทุก
   record type ที่ใช้จริงก่อน แล้วผู้ใช้ deploy เอง; ทดสอบ import ด้วย backup จริงของตัวเอง
   (ที่มี reservation ชื่อเว้นวรรค) ว่ายัง import ผ่าน

## 6. Summary Checklist (Definition of Done)

- [ ] `backend/internal/model/dns_validate.go` — `ValidateDNSZone` / `ValidateDNSRecord` /
      `ValidateReservationName` (whitelist, reject `\n\r`, per-type value)
- [ ] `backend/internal/kernel/dns_server.go` + `dhcp_server.go` — skip+log entry ที่ fail validate
- [ ] `backend/internal/api/handlers.go` — validate ใน 6 handler (dns zone/record + dhcp reservation) → 400
- [ ] `backend/internal/service/backup.go` — validate ทุก DNS/DHCP entry ก่อน restore, reject ทั้ง import ถ้าพัง
- [ ] Tests: injection case ตรงๆ / per-type valid+invalid / handler 400 / import reject / kernel skip
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] ทดสอบ mock mode: สร้าง zone/record/reservation ปกติได้; ใส่ newline/อักขระแปลก → 400
- [ ] ทดสอบ embedded build (`bash build.sh`): DNS/DHCP ทุก record type ที่ใช้จริงยังทำงาน;
      import backup เดิม (มี reservation ชื่อเว้นวรรค) ผ่าน
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — เพิ่ม 400 + charset (sync ทั้งคู่)
- [ ] อัปเดต `docs/ref/dns-system-design.md` + `dhcp-service-design.md` เรื่อง validation rule
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/` + อัปเดต security review artifact (finding 7 → done)
