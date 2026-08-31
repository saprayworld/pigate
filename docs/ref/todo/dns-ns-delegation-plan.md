# แผนงาน: NS Delegation แบบ Forwarding-based (DNS Server / local zones)

สถานะ: TODO — สำหรับ ai-developer ทำทีละ Task, **ทดสอบรวมครั้งเดียวท้ายแผน**

ต่อยอดจาก `docs/ref/todo/dns-ns-record-support-plan.md` (PR 162, commit `6fd034b`)
ที่ทำ NS record แบบ **publish-only** ไว้แล้ว แผนนี้คือ "แนวทาง A" —
ทำให้ delegation ทำงานจริงในระดับ **forwarding** โดยใช้กลไก `server=/<name>/<ip>`
ที่ dnsmasq มีอยู่แล้ว

**นอก scope (เจ้าของโปรเจกต์ตัดออกแล้ว — ห้ามเสนอ/ห้ามทำ)**: เปลี่ยน DNS engine
(bind9/knot/PowerDNS), รัน service คู่ขนาน, implement authoritative resolver เอง
(SOA / AXFR / referral response ตามโปรโตคอล) — ผลลัพธ์ของแผนนี้ตั้งใจให้ "ใช้งานได้จริง
สำหรับ stub resolver ทั่วไป" ไม่ต้อง identical กับ bind9 ทางโปรโตคอล

---

## 1. บริบทจากการสำรวจโค้ด (ของจริง ณ ปัจจุบัน)

| หัวข้อ | สถานะปัจจุบัน |
|---|---|
| Record types | A, AAAA, CNAME, MX, TXT, PTR, **NS** (NS = publish-only) |
| Generator | `buildDNSConfig()` (pure, no I/O) `backend/internal/kernel/dns_server.go:111`; NS case อยู่ที่บรรทัด 269–287 |
| Validator | `model.ValidateDNSRecord` `backend/internal/model/dns_validate.go:85`; NS case บรรทัด 149–161 (เรียก `EncodeDNSNameHex` เป็นตัวการันตีว่า validator = generator) |
| Encoder | `model.EncodeDNSNameHex` `backend/internal/model/dns_wire.go:28` |
| Model | `model.DNSRecord` / `DNSRecordInput` `backend/internal/model/dns_server.go:13,30` — ยังไม่มีช่องเก็บ glue IP |
| DB schema | `dns_records(id, zone_id, name, type, value, ttl, created_at)` `db/connection.go:459`; migration rebuild-table ล่าสุด (เพิ่ม `'NS'` ใน CHECK) `db/connection.go:157-191` |
| Repo | `GetDNSRecordsByZone/GetDNSRecordByID/CreateDNSRecord/UpdateDNSRecord` `db/repository.go:3344-3391` (ระบุคอลัมน์ชัดเจนทุกคำสั่ง — ต้องแก้ทุกจุดเมื่อเพิ่มคอลัมน์) |
| Backup | restore: `db/backup_repo.go:334`; validate ตอน import: `service/backup.go:1053` |
| API | `POST /api/dns/zones/{id}/records` `api/handlers.go:3898`, `PUT /api/dns/records/{id}` `:3934`, ลงทะเบียนที่ `api/router.go:204-216` |
| Frontend | `frontend/src/pages/DnsServer.tsx` — dropdown บรรทัด 2075-2082, ฟอร์ม value บรรทัด 2086-2109, validation `handleSaveRecord` บรรทัด 656-673, info note บรรทัด 1290-1303, ตาราง record บรรทัด 1203-1236 |
| Frontend service | `frontend/src/services/dnsServerService.ts` (`createRecord:257`, `updateRecord:289`), type `DNSRecord` อยู่ที่ `frontend/src/data-mockup/mockData.ts:741` |
| Mock kernel | `MockDNSServerManager.ApplyZones` (`kernel/mock.go:433`) แค่ log/นับ **ไม่ render record** → **ไม่ต้องแก้ mock.go** (ยืนยันแล้ว ไม่ใช่การละเลยกฎ real/mock คู่กัน) |

**Precedent ที่ต้องยึดเป็นแบบ (อ่านก่อนเขียนโค้ด)**
- SSRF/outbound guard: `service/dns_blocklist_fetch.go`, `isGloballyRoutable` + `ipInfoRateLimiter` ใน `service/ipinfo.go:57,337`
- Test seam สำหรับ DNS lookup: `var lookupIP = net.LookupIP` ที่ `kernel/real_firewall.go:62` (stub ใน `kernel/policy_chain_test.go:263`)
- Migration test: `db/dns_server_settings_migration_test.go` (มีเช็ค idempotency: `migrate()` สองรอบ)
- ALTER-column migration + CREATE TABLE ต้องแก้คู่กัน: `subtype` ที่ `db/connection.go:193-203` + `:528`

---

## 2. ข้อเท็จจริงทางเทคนิคที่ verify แล้ว (สำคัญมาก — เป็นฐานของทั้งแผน)

### 2.1 `server=/<sub>/<ip>` ชนะ `local=/<zone>/` จริง — ยืนยันจาก dnsmasq man page

จาก dnsmasq man page (`--server`):

> "More specific domains take precedence over less specific domains, so:
> `--server=/google.com/1.2.3.4` `--server=/www.google.com/2.3.4.5`
> will send queries for google.com and gmail.google.com to 1.2.3.4,
> but www.google.com will go to 2.3.4.5"

> "Matching of domains is normally done on complete labels, so /google.com/
> matches google.com and www.google.com but NOT supergoogle.com."

และ `--local` เป็น **synonym ของ `--server`**:

> "`--local` is a synonym for `--server` to make configuration files clearer in this case."

⇒ `local=/sapray.net/` กับ `server=/www.sapray.net/<ip>` อยู่ใน "ตารางเดียวกัน" และ
dnsmasq เลือกอันที่ **match ยาวที่สุด** ⇒ คำถามที่ตกใต้ `www.sapray.net`
จะถูก forward ไป `<ip>` แทนที่จะโดน NXDOMAIN จากโซนแม่ — **สมมติฐานหลักของแผนนี้ผ่าน**
(ยังต้องยืนยันบนเครื่องจริงอีกชั้นใน Final Acceptance ข้อสุดท้าย)

### 2.2 `dns-rr=` (publish) กับ `server=` (forward) อยู่ร่วมกันได้ ไม่ต้องเลือกอย่างใดอย่างหนึ่ง

dnsmasq ตอบจาก local record ก่อนตัดสินใจ forward เสมอ ⇒
- `dig NS sub.zone` → ตอบจาก `dns-rr=sub.zone,2,<hex>` (local)
- `dig A host.sub.zone` / query อื่น → ไม่มี local record → เข้าเส้นทาง forward → `server=/sub.zone/<ip>`

⇒ **แผนนี้ "เพิ่ม" `server=` โดยไม่ลบ `dns-rr=`** (ไม่ regress ฟีเจอร์ publish เดิม
และเมื่อ NS record ไม่มี glue IP ผลลัพธ์ต้องเหมือนเดิมทุกไบต์)

### 2.3 Forward ไปหา authoritative-only nameserver ใช้ได้

`server=` ของ dnsmasq ส่ง query แบบ recursive (RD=1) แต่ authoritative server
(เช่น `*.ns.cloudflare.com`) จะ **ตอบ authoritative answer สำหรับโซนที่ตัวเองถือ**
โดยไม่สนใจบิต RD ⇒ ใช้งานได้จริงสำหรับกรณี delegate ไป Cloudflare/ผู้ให้บริการอื่น
(สิ่งที่จะไม่ได้คือการ resolve ชื่อ *นอก* โซนนั้นผ่าน server ตัวนี้ ซึ่งเราไม่ต้องการอยู่แล้ว)

### 2.4 แก้ความเข้าใจเรื่อง "in-bailiwick → resolve วนลูป"

โจทย์ระบุว่า NS target ที่อยู่ในโซนเดียวกันจะ "resolve วนลูป" — **ในสถาปัตยกรรมนี้ไม่เกิดลูป**
เพราะเราเขียน `server=/<sub>/<glue-ip>` ด้วย **IP ตรง ๆ** dnsmasq จึงไม่ต้อง resolve
ชื่อ nameserver เลย ปัญหาจริงที่เกิดคือ:

> ถ้า NS target เป็นชื่อในโซนแม่ (เช่น delegate `sub.example.local` ไปที่ `ns1.example.local`)
> แล้วไม่มี A record ของ `ns1.example.local` ⇒ client ที่ลอง `dig A ns1.example.local`
> จะได้ **NXDOMAIN** จาก `local=/example.local/` (delegation ยังทำงาน แต่ชื่อ NS resolve ไม่ได้)

⇒ เราจึงต้อง emit glue `host-record=` ให้ target **เฉพาะกรณีที่ target อยู่ในโซนแม่
แต่ไม่ได้อยู่ใต้ชื่อที่ถูก delegate** (รายละเอียดกติกาอยู่ใน T-04) — ไม่ใช่กันลูป
แต่กัน NXDOMAIN ของชื่อ nameserver

### 2.5 กับดักสำคัญ: NS ที่ apex ของโซน (`Name` = `@` / ว่าง / เท่าชื่อโซน)

ถ้า emit `server=/<zoneName>/<ip>` ⇒ **ทั้งโซนถูก forward ออกไปข้างนอก**
`local=/<zone>/` และ `host-record=` ทุกตัวในโซนจะถูก override (longest match เท่ากัน
แต่ spec ที่มี address ชนะ spec ที่ไม่มี) ⇒ โซน local พังทั้งโซน

**การตัดสินใจของแผนนี้**: NS ที่ apex = **publish-only เท่านั้น ห้าม emit `server=`**
- ระดับ API: ถ้า record เป็น NS + `name` ชี้ที่ apex + มี `glueIps` → ตอบ 400 พร้อมข้อความว่า
  ถ้าต้องการ forward ทั้งโซนให้ใช้ **Forward Zone** (`isAuthoritative=false` + `forwardTo`) ที่มีอยู่แล้ว
- ระดับ generator: ถึงจะหลุด DB มาก็ต้อง skip + `log.Printf` (defense-in-depth)
- ระดับ UI: ปิด/ซ่อนช่อง glue IP เมื่อ Host Name เป็น apex พร้อมคำอธิบาย

### 2.6 สรุปพฤติกรรมที่จะได้หลังแผนนี้

| กรณี | ผลลัพธ์ |
|---|---|
| NS record ไม่มี glue IP | เหมือนเดิมเป๊ะ (publish-only, config byte-identical) |
| NS record ที่ subdomain + glue IP | `dns-rr=` + `server=/<fullName>/<ip>` ต่อ IP → delegation ใช้งานได้จริง |
| NS record ที่ apex + glue IP | ถูกปฏิเสธที่ API (400); ถ้าหลุดมาถึง generator → skip เฉพาะ `server=` (ยังคง `dns-rr=`) |
| NS หลายรายการชื่อเดียวกัน | หลาย `server=` line ของ domain เดียวกัน (dnsmasq รองรับ) — dedupe คู่ (domain, ip) ซ้ำ |
| NS target อยู่ในโซนแม่ (นอกชื่อที่ delegate) | + `host-record=<target>,<glue-ip>` (ถ้ายังไม่มี A/AAAA ชื่อนั้นในโซน) |
| NS target อยู่ใต้ชื่อที่ delegate หรืออยู่นอกโซน | ไม่ต้อง emit `host-record=` |

---

## 3. Data model ที่เลือก (พร้อมเหตุผล)

**เลือก: เพิ่มคอลัมน์ `glue_ips TEXT NOT NULL DEFAULT ''` ใน `dns_records`
(เก็บเป็นสตริงคั่นด้วยจุลภาค) ไม่สร้างตารางใหม่**

| ทางเลือก | ข้อดี | ข้อเสีย | ตัดสิน |
|---|---|---|---|
| (ก) คอลัมน์ `glue_ips TEXT` ใน `dns_records` | ตรง pattern ที่มีอยู่แล้ว (`dns_server_settings.interfaces` เก็บ comma-joined เหมือนกัน), migration เป็น `ALTER TABLE ADD COLUMN` แบบ idempotent ที่ codebase ใช้อยู่ (`subtype`, `metric`, `prefer_5ghz`), แก้ไฟล์น้อย, backup/restore เพิ่มแค่ 1 คอลัมน์ | ไม่ normalize; ต้อง split/join | ✅ **เลือกอันนี้** |
| (ข) ตารางแยก `dns_record_glue(record_id, ip)` | normalize, cascade delete | ต้องเพิ่ม repo CRUD ชุดใหม่, แก้ backup export/import structure, แก้ API response shape มากขึ้น, โค้ดเพิ่มเยอะโดยได้ประโยชน์น้อย (จำกัดไว้ ≤4 IP อยู่แล้ว) | ❌ |

**หมายเหตุสำคัญเรื่อง migration**: การเพิ่มคอลัมน์ครั้งนี้ **ไม่ต้องใช้ rebuild-table**
เพราะไม่ได้แตะ CHECK constraint (ต่างจาก migration `'NS'` เดิม) ⇒ ใช้
`ALTER TABLE dns_records ADD COLUMN glue_ips TEXT NOT NULL DEFAULT ''`
โดยตรวจ idempotency ด้วย `strings.Contains(sqlCreate, "glue_ips")` เหมือน `subtype`
และต้องเพิ่มคอลัมน์ใน `CREATE TABLE IF NOT EXISTS dns_records` (`connection.go:459`) ด้วย
สำหรับ DB ใหม่ (ลำดับใน `migrate()` คือ pre-check ทั้งหมด → แล้วค่อย `CREATE TABLE IF NOT EXISTS`
ดังนั้น DB ใหม่จะข้าม ALTER แล้วได้คอลัมน์จาก CREATE โดยตรง — ตรง pattern `subtype`)

**หมายเหตุลำดับ migration**: DB เก่ามาก (ยังไม่มี `'NS'`) จะเข้า rebuild เดิมก่อน
(สร้าง `dns_records_new` **ไม่มี** `glue_ips`) แล้วค่อยเข้า ALTER ของแผนนี้ →
ต้องวางบล็อก ALTER **ต่อจาก** บล็อก rebuild `'NS'` (`connection.go:191`) เท่านั้น

---

## 4. Task list

### T-01 — model: ฟิลด์ GlueIPs + ค่าคงที่
- layer: model
- files: `backend/internal/model/dns_server.go`
- instruction:
  1. เพิ่มฟิลด์ใน `DNSRecord`:
     ```go
     // GlueIPs is the NS-delegation glue: the IP address(es) of the
     // nameserver named in Value. Only meaningful for Type == "NS" and only
     // for a non-apex record name. When non-empty, the generator emits
     // `server=/<fqdn>/<ip>` per IP so dnsmasq actually FORWARDS queries
     // under that name to the delegated nameserver (forwarding-based
     // delegation), instead of only publishing the NS record via dns-rr.
     // Empty (the default, and the value of every pre-existing row) keeps
     // the previous publish-only behaviour byte-for-byte.
     GlueIPs []string `json:"glueIps"`
     ```
  2. เพิ่มฟิลด์เดียวกันใน `DNSRecordInput` (`GlueIPs []string \`json:"glueIps"\``)
  3. เพิ่มค่าคงที่ (วางใกล้ ๆ กลุ่ม const DNS อื่นใน `dns_validate.go` หรือไฟล์นี้ก็ได้
     แต่ให้อยู่ package `model` และมี doc comment):
     ```go
     // DNSNSGlueMaxIPs caps how many glue IPs one NS record may carry. Each
     // one becomes its own `server=` line in pigate-dns.conf; 4 covers every
     // realistic delegation (2 NS x A+AAAA) without letting one record bloat
     // the config.
     const DNSNSGlueMaxIPs = 4
     ```
  ห้ามแก้ signature ของฟังก์ชันเดิม, ห้ามเพิ่ม dependency
- acceptance:
  - `cd backend && go build ./...` ผ่าน
  - JSON key เป็น `glueIps` (camelCase ตรงกับฟิลด์อื่นในไฟล์)
- depends_on: []

### T-02 — model: validation ของ glue IP (SENSITIVE — input validation)
- layer: model
- files: `backend/internal/model/dns_validate.go`
- instruction:
  ค่านี้ถูก interpolate ลงไฟล์ config ของ dnsmasq โดยตรง → **ต้อง review เข้มเป็นพิเศษ**
  1. ใน `ValidateDNSRecord` **ก่อน** `switch strings.ToUpper(r.Type)` เพิ่ม guard:
     ```go
     if len(r.GlueIPs) > 0 && strings.ToUpper(r.Type) != "NS" {
         return fmt.Errorf("glueIps is only allowed on NS records, not %q", r.Type)
     }
     ```
  2. ใน `case "NS":` (ต่อจากการเช็ค `EncodeDNSNameHex` เดิม) เพิ่มการตรวจ glue IP:
     - `len(r.GlueIPs) > DNSNSGlueMaxIPs` → error
     - ต่อ IP: **ห้าม `TrimSpace`** (ค่าถูกเขียนลง config แบบ verbatim — ค่าที่มีช่องว่างขอบ
       ต้องถูก reject ไม่ใช่ตัดทิ้งเงียบ ๆ ตามวินัยเดียวกับ `ValidateDhcpConfig`
       และ `ValidateDNSServerSettings`)
       - `net.ParseIP(raw) == nil` → `"NS glue IP %q is not a valid IP address"`
         (`net.ParseIP` reject newline/space/`%zone` อยู่แล้ว ⇒ ปิดช่อง directive injection)
       - `ip.IsLoopback()` → error พร้อมเหตุผลว่าจะเกิด forwarding loop กับ dnsmasq เอง
         (ตรง precedent ของ `ValidateDNSServerSettings`)
       - `ip.IsUnspecified()` (`0.0.0.0` / `::`) หรือ `ip.IsMulticast()` → error
       - ค่าซ้ำ (เทียบด้วย `net.ParseIP(raw).String()` เพื่อจับ `1.1.1.1` ซ้ำกับรูปแบบอื่น) → error
     - **อนุญาต private/RFC1918 โดยตั้งใจ** — internal delegation ไปหา NS ในวง LAN
       เป็น use case ที่ถูกต้อง (ต่างจาก `isGloballyRoutable` ที่ใช้กับ outbound HTTP)
       ให้เขียน comment กำกับไว้ชัดเจน เพื่อไม่ให้คนรีวิวรอบหน้าเข้าใจผิดว่าลืม guard
  3. อัปเดต doc comment ของ `ValidateDNSRecord` ให้พูดถึง `GlueIPs`
  **ห้าม** ตรวจ apex ในไฟล์นี้ — `ValidateDNSRecord` ไม่รู้จักชื่อโซน (ทำที่ handler = T-06 แทน)
- acceptance:
  - `cd backend && go build ./...` ผ่าน
  - record ที่ไม่มี `GlueIPs` ให้ผลเหมือนเดิมทุกกรณี (ไม่มี behavior change)
  - `"1.2.3.4\nserver=/evil/6.6.6.6"` และ `" 1.2.3.4"` ถูก reject
- depends_on: ["T-01"]

### T-03 — db: schema + migration + repository + backup restore
- layer: db
- files: `backend/internal/db/connection.go`, `backend/internal/db/repository.go`,
  `backend/internal/db/backup_repo.go`
- instruction:
  1. `connection.go` — เพิ่ม `glue_ips TEXT NOT NULL DEFAULT ''` ใน
     `CREATE TABLE IF NOT EXISTS dns_records` (บรรทัด ~459) ต่อท้าย `ttl`
     (ก่อน `created_at` หรือหลังก็ได้ แต่ต้องสอดคล้องกับ INSERT ที่ระบุคอลัมน์ชัดเจนอยู่แล้ว)
  2. `connection.go` — เพิ่มบล็อก migration **ต่อจาก** บล็อก rebuild `'NS'` (บรรทัด ~191):
     ```go
     // Add glue_ips column to dns_records if it doesn't exist
     // (docs/ref/todo/dns-ns-delegation-plan.md T-03). Additive only — no
     // CHECK constraint changes, so a plain ALTER is enough (unlike the
     // 'NS' rebuild above). NOT NULL DEFAULT '' means every pre-existing
     // row reads back as "no glue" = the previous publish-only behaviour.
     ```
     ตรวจ idempotency ด้วย `strings.Contains(sqlCreateDNSRecordsGlue, "glue_ips")`
     โดย query `sqlite_master` ใหม่ (ห้าม reuse ตัวแปรเดิมจากบล็อก rebuild —
     ค่าใน sqlite_master เปลี่ยนไปแล้วหลัง rebuild) และ `log.Println("[Migration] ...")`
     เมื่อรันจริง
  3. `repository.go` — แก้ทั้ง 4 จุด (`GetDNSRecordsByZone:3345`, `GetDNSRecordByID:3364`,
     `CreateDNSRecord:3377`, `UpdateDNSRecord:3383`) ให้อ่าน/เขียนคอลัมน์ `glue_ips`
     - เขียน: join ด้วย `,` (ไม่มีช่องว่าง)
     - อ่าน: split แล้ว **ตัดสตริงว่างทิ้ง** และคืน `[]string{}` (ไม่ใช่ `nil`) เมื่อไม่มีค่า
       เพื่อให้ JSON เป็น `[]` ไม่ใช่ `null`
     - ทำเป็น helper 2 ตัวในไฟล์นี้ (เช่น `joinGlueIPs` / `splitGlueIPs`) ไม่ต้อง export
     - **ไม่ต้อง** clamp/validate ในชั้น repo (validator ที่ handler + generator
       คุมอยู่แล้ว) แต่ split ต้องกันสตริงว่างเพื่อไม่ให้เกิด `""` ใน slice
  4. `backup_repo.go:334` — เพิ่ม `glue_ips` ใน INSERT ของ restore
     (ใช้ helper เดียวกัน; backup รุ่นเก่าที่ไม่มีฟิลด์นี้ → `""`)
  ห้ามแตะ `DELETE FROM dns_records` (`backup_repo.go:123`) และห้ามเปลี่ยนลำดับ migration เดิม
- acceptance:
  - `cd backend && go build ./...` ผ่าน
  - `InitDB(":memory:")` สร้างตารางที่มี `glue_ips`; รัน `migrate()` ซ้ำไม่ error
  - record เดิมทุกแถวอ่านกลับมาได้ `GlueIPs == []string{}`
- depends_on: ["T-01"]

### T-04 — kernel: generate `server=` (delegation) + glue `host-record=` (SENSITIVE — config generation)
- layer: kernel
- files: `backend/internal/kernel/dns_server.go`
- instruction:
  แก้เฉพาะภายใน `buildDNSConfig` เท่านั้น (ยังต้องเป็น pure function ไม่มี I/O)
  **ห้ามแตะ mock.go** (mock ไม่ render record — ยืนยันแล้วใน §1)

  1. **ก่อน** loop `for _, rec := range zone.Records` ของโซน authoritative ให้เตรียม 3 อย่างต่อโซน:
     - `existingAddrNames map[string]bool` — set ของ `fullName` (lowercase) ของ record
       ชนิด A/AAAA ทั้งหมดในโซนนี้ (pre-pass สั้น ๆ) ใช้กันไม่ให้ glue `host-record`
       ไปชนกับ A record ที่ผู้ใช้ตั้งเองจนอาจทำให้ `dnsmasq --test` ล้มทั้งไฟล์
     - `emittedDelegation map[string]bool` — dedupe คู่ `"<fqdn>|<ip>"`
     - `emittedGlue map[string]bool` — dedupe คู่ `"<target>|<ip>"`
     - `delegationHeaderWritten bool` — ใช้ emit comment หัวข้อครั้งเดียวต่อโซน

  2. ใน `case "NS":` ที่มีอยู่ (บรรทัด 269-287) **คงบรรทัด `dns-rr=` เดิมไว้ทุกกรณี**
     แล้วเพิ่มต่อท้าย:
     ```
     ถ้า len(rec.GlueIPs) == 0            → ไม่ทำอะไรต่อ (output เท่าเดิมทุกไบต์)
     ถ้า fullName == zoneName (apex)      → log.Printf skip + ไม่ emit server= (ดู §2.5)
     มิฉะนั้น:
        - emit comment หัวข้อครั้งแรกของโซน (คงที่ ไม่มีข้อมูลผู้ใช้ในสตริง):
          "# NS delegation (forwarding-based)\n"
        - ต่อ ip ใน rec.GlueIPs:
            * net.ParseIP(ip) == nil || IsLoopback || IsUnspecified || IsMulticast
              → log.Printf skip (defense-in-depth ซ้ำกับ T-02 เผื่อค่าหลุดมาจาก DB/import)
            * dedupe ด้วย emittedDelegation
            * sb.WriteString(fmt.Sprintf("server=/%s/%s\n", fullName, ip))
        - glue host-record (ตามกติกา §2.4) — emit เมื่อครบทุกข้อ:
            (a) target ลงท้ายด้วย "." + zoneName (อยู่ในโซนแม่) และ target != zoneName
            (b) target ไม่ได้อยู่ใต้ fullName (ไม่เท่ากับ fullName และไม่ลงท้ายด้วย "."+fullName)
            (c) !existingAddrNames[strings.ToLower(target)]
            (d) ยังไม่ถูก emit (emittedGlue)
          → sb.WriteString(fmt.Sprintf("host-record=%s,%s\n", target, ip)) ต่อ IP ที่ผ่าน
     ```
  3. เขียน comment เหนือบล็อกใหม่ อธิบายสั้น ๆ ว่า:
     - นี่คือ forwarding-based delegation ไม่ใช่ authoritative referral
     - `local=` เป็น synonym ของ `server=` และ dnsmasq เลือก match ที่ยาวที่สุด
       จึงทำให้ `server=/<sub>/<ip>` ชนะ `local=/<zone>/` ของโซนแม่ (อ้าง §2.1 ของแผนนี้)
     - ทำไม apex ถึงถูก skip (อ้าง §2.5)
  4. **ห้าม** เปลี่ยนข้อความ/ลำดับ/บรรทัดว่างของส่วนอื่นในไฟล์ config
     โซนที่ไม่มี NS-with-glue ต้องได้ output เดิม **byte-for-byte**
- acceptance:
  - `cd backend && go build ./...` ผ่าน
  - โซนที่ไม่มี NS record หรือ NS ที่ไม่มี glue → output เหมือนก่อนแก้ทุกไบต์
  - ไม่มีการเพิ่ม `exec.Command`, ไม่มี I/O ใหม่ในฟังก์ชัน pure
- depends_on: ["T-02"]

### T-05 — service: ตัว resolve ชื่อ nameserver → IP (SENSITIVE — outbound query)
- layer: service
- files: `backend/internal/service/dns_ns_lookup.go` (ไฟล์ใหม่)
- instruction:
  ทำเป็น method บน `*DNSServerService` (มีอยู่แล้วใน `api.Server.dnsServerService`)
  เพื่อ **ไม่ต้องแก้ signature ของ `api.NewServer`**
  ```go
  func (s *DNSServerService) ResolveNameserver(ctx context.Context, name string) ([]string, error)
  ```
  ข้อกำหนด:
  1. **validate input ก่อนทุกอย่าง** ด้วย `model.EncodeDNSNameHex(name)` (reuse ตัวเดิม —
     ได้ charset/label/ความยาว/กัน newline ฟรี และการันตีว่ากติกาเดียวกับที่ generator ยอมรับ)
     ถ้าไม่ผ่าน → คืน error ที่ห่อไว้ (sentinel `ErrNSLookupInvalidName`)
     **ห้าม log ค่า raw ก่อน validate** (กัน log injection)
  2. resolve ผ่าน seam ระดับ package เพื่อให้ test stub ได้ (ตาม precedent
     `kernel/real_firewall.go:62`):
     ```go
     // resolveNSHostIPs is the seam tests replace; production uses the Go
     // resolver (PreferGo) so lookups follow /etc/resolv.conf, i.e. the same
     // resolver the box itself uses (dnsmasq/systemd-resolved).
     var resolveNSHostIPs = func(ctx context.Context, name string) ([]net.IP, error) {
         r := &net.Resolver{PreferGo: true}
         addrs, err := r.LookupIPAddr(ctx, name)
         ...
     }
     ```
  3. timeout: หุ้ม ctx ด้วย `context.WithTimeout` **3 วินาที** (ค่าคงที่ในไฟล์นี้ มี doc comment)
  4. rate limit: token bucket ระดับ service (burst 5, เติม 1/วินาที) —
     ลอกรูปแบบจาก `ipInfoRateLimiter` (`service/ipinfo.go:337`) แต่ประกาศตัวใหม่ในไฟล์นี้
     เกิน → sentinel `ErrNSLookupRateLimited`
  5. ผลลัพธ์: dedupe, จำกัดไม่เกิน `model.DNSNSGlueMaxIPs` ตัว, เรียง IPv4 ก่อน IPv6,
     คืนเป็น canonical string (`ip.String()`); ถ้าไม่มีผลลัพธ์ → `ErrNSLookupNotFound`
  6. **ห้ามใช้ `isGloballyRoutable` กรองผลลัพธ์** — NS ในวง LAN (192.168.x/10.x) เป็น
     use case ที่ถูกต้อง เขียน comment อธิบายไว้ให้ชัด ว่าทำไมถึงต่างจาก
     `dns_blocklist_fetch.go`/`ipinfo.go` (ที่นั่นเรา **เชื่อมต่อ** ไปยัง IP นั้นจริง
     จึงเป็น SSRF ได้ ที่นี่เราแค่เอาค่าไปเขียนลงไฟล์ config — ผู้ใช้พิมพ์เองก็ได้อยู่แล้ว)
  7. log: log ได้เฉพาะชื่อที่ผ่าน validate แล้ว + จำนวน IP ที่ได้ (ห้าม log ค่า raw input)
  8. ทำงานได้ทั้ง `-mock=true` และโหมดจริง (ไม่ใช่ kernel capability → ไม่ต้องมี mock คู่)
     ให้เขียน comment กำกับว่าจงใจไม่แตะ `kernel/` เพราะไม่ใช่ OS control
- acceptance:
  - `cd backend && go build ./...` ผ่าน
  - ไม่มี dependency ใหม่ (stdlib เท่านั้น)
  - ไม่มี `exec.Command`
- depends_on: ["T-01"]

### T-06 — api: endpoint ค้นหา IP อัตโนมัติ + apex guard (SENSITIVE)
- layer: api
- files: `backend/internal/api/handlers.go`, `backend/internal/api/router.go`
- instruction:
  1. **Endpoint ใหม่** — `HandleResolveNameserver`:
     - route: `authRoute("GET /api/dns/resolve-ns", s.HandleResolveNameserver)`
       วางต่อจากบรรทัด `authRoute("PUT /api/dns/settings", ...)` (`router.go:216`)
       พร้อม comment อธิบาย
     - **ต้องเป็น GET** โดยเจตนา: เป็นการอ่านล้วน ๆ ⇒ `DisableEditMiddleware`
       (`middleware.go:334`) และ `RoleReadOnlyMiddleware` (`:128`) จะไม่บล็อก
       (precedent: `GET /api/interfaces/{id}/scan`)
     - อ่านชื่อจาก query param `name`; ถ้า `s.dnsServerService == nil` → 503
     - เรียก `s.dnsServerService.ResolveNameserver(r.Context(), name)` แล้ว map error:
       | เงื่อนไข | HTTP |
       |---|---|
       | `ErrNSLookupInvalidName` | 400 |
       | `ErrNSLookupNotFound` | 404 |
       | `ErrNSLookupRateLimited` | 429 |
       | error อื่น (timeout/servfail) | 502 |
     - สำเร็จ → 200 `{"name":"<validated name>","ips":["..."]}`
       (ต้องคืนชื่อที่ผ่าน validate/normalize แล้วเท่านั้น ห้าม echo raw input กลับไป)
     - **ไม่ต้อง** `logEvent` (เป็นการอ่าน ไม่ใช่การเปลี่ยน config) — สอดคล้องกับ
       handler อ่านอื่น ๆ ในไฟล์นี้
  2. **Apex guard** ใน `HandleCreateDNSRecord` (`:3898`) และ `HandleUpdateDNSRecord` (`:3934`):
     - หลัง `model.ValidateDNSRecord(record)` ผ่านแล้ว ถ้า `record.Type` (upper) == `"NS"`
       และ `len(record.GlueIPs) > 0`:
       - โหลดโซนด้วย `s.repo.GetDNSZoneByID(record.ZoneID)` (create ใช้ path param,
         update ใช้ `existing.ZoneID`)
       - ถ้าโซนไม่พบ → 400/404 ตามรูปแบบเดิมของ handler
       - normalize `name := strings.TrimSpace(record.Name)`; ถ้า `name == ""` หรือ `"@"`
         หรือ `strings.EqualFold(name, zone.ZoneName)` ⇒ **400** พร้อมข้อความไทยที่บอกทางออก:
         ระบุ glue IP กับ NS ที่ apex ของโซนไม่ได้ เพราะจะทำให้ทั้งโซนถูกส่งต่อออกไป —
         ถ้าต้องการแบบนั้นให้ใช้โซนแบบ "Forward Zone" แทน
     - ทำเป็น helper ตัวเดียวใช้ร่วมกันสองที่ (เช่น `validateNSGlueAgainstZone`)
       เพื่อไม่ให้กติกาสองที่หลุดจากกัน
  3. `s.repo.CreateDNSRecord/UpdateDNSRecord` ต้องได้ `record.GlueIPs` ติดไปด้วย
     (สร้าง struct จาก `input.GlueIPs`)
- acceptance:
  - `cd backend && go build ./...` ผ่าน
  - `GET /api/dns/resolve-ns?name=<ค่ามี newline>` → 400 โดยไม่มีการยิง DNS query ออกไป
  - POST record NS + glueIps ที่ apex → 400; ที่ subdomain → 200
- depends_on: ["T-02", "T-05"]

### T-07 — เอกสาร API (openapi ทั้งสองสำเนา)
- layer: api
- files: `docs/openapi.yaml`, `frontend/public/openapi.yaml`
- instruction:
  1. ในสคีมา DNS record (บริเวณบรรทัด 9027-9049 ของทั้งสองไฟล์) เพิ่ม property:
     ```yaml
     glueIps:
       type: array
       items:
         type: string
       description: >-
         NS-delegation glue IPs (NS records only, max 4). When set, PiGate also
         emits `server=/<fqdn>/<ip>` so dnsmasq forwards every query under that
         name to the delegated nameserver (forwarding-based delegation), on top
         of publishing the NS record itself. Not allowed on the zone apex
         (use a Forward Zone instead) and not allowed on non-NS records - both
         return 400. Loopback/unspecified/multicast addresses are rejected.
       example: ["203.0.113.53"]
     ```
     และแก้คำอธิบาย `value` ของ NS จาก "no real delegation" เป็นข้อความที่สะท้อนว่า
     ตอนนี้มี delegation แบบ forwarding เมื่อระบุ `glueIps`
  2. เพิ่ม path ใหม่ `/dns/resolve-ns` (GET) ใกล้ ๆ `/dns/settings`:
     query param `name` (required, string), response 200 =
     `{name: string, ips: string[]}`, และ 400 / 404 / 429 / 502 / 503
     พร้อม description ว่า resolve ผ่าน resolver ของตัวเครื่อง มี timeout 3s และ rate limit
  3. แก้ **ทั้งสองไฟล์ให้เนื้อหาเหมือนกันเป๊ะ** (`frontend/public/openapi.yaml`
     คือสำเนาที่หน้า ApiDocs ใช้)
  **ห้ามแก้มือ** `backend/internal/api/dist/openapi.yaml` — เป็น build artifact จาก `build.sh`
- acceptance: ทั้งสองไฟล์เนื้อหาส่วนนี้ตรงกัน, YAML ยัง parse ได้
- depends_on: []

### T-08 — frontend: type + API client
- layer: frontend
- files: `frontend/src/data-mockup/mockData.ts`, `frontend/src/services/dnsServerService.ts`
- instruction:
  1. `mockData.ts:741` — เพิ่ม `glueIps?: string[]` ใน `interface DNSRecord`
     (optional เพื่อไม่ให้ข้อมูล mock เดิมพัง)
  2. `dnsServerService.ts` — เพิ่มเมธอดใหม่:
     ```ts
     resolveNameserver: async (name: string): Promise<string[]> => { ... }
     ```
     - IS_MOCK_MODE → `await delay(400)` แล้วคืนค่าคงที่ (เช่น `["203.0.113.53"]`)
       พร้อมคอมเมนต์ว่าเป็นค่าจำลอง
     - โหมดจริง → `fetch(`${API_BASE_URL}/dns/resolve-ns?name=${encodeURIComponent(name)}`)`
       ต้อง `encodeURIComponent` เสมอ; ถ้า `!response.ok` ให้ throw error ที่อ่านข้อความ
       `message` จาก body ตามรูปแบบ error handling ของไฟล์นี้
     - คืน `data.ips ?? []`
  3. `createRecord`/`updateRecord` ไม่ต้องแก้ signature (ใช้
     `Omit<DNSRecord,"id"|"zoneId">` อยู่แล้ว → `glueIps` ไหลไปเองเมื่อ caller ใส่มา)
     แต่ mock-mode branch ต้อง preserve `glueIps` ที่ส่งเข้ามา (ตรวจให้แน่ใจว่า
     `{...record}` ครอบคลุมอยู่แล้ว)
- acceptance: `cd frontend && yarn build && yarn lint` ผ่าน, ไม่มี dependency ใหม่
- depends_on: []

### T-09 — frontend: ฟอร์ม glue IP + ปุ่มค้นหา IP อัตโนมัติ + ตาราง + info note
- layer: frontend
- files: `frontend/src/pages/DnsServer.tsx`
- instruction:
  ยึด `docs/rules_of_work.md`: ใช้เฉพาะ shadcn ใน `components/ui/`, **ห้ามสีแบบ hardcode**
  (`text-emerald-500` ฯลฯ) ใช้ตัวแปรธีม, **ห้าม `shadow-*` / `backdrop-blur-*`**,
  รองรับทั้ง dark/light
  1. state ใหม่: `recGlueIps` (string, ค่าที่ผู้ใช้พิมพ์ คั่นด้วยจุลภาค),
     `isResolvingNs` (boolean), และล้าง/เติมค่าให้ครบทั้งใน `openCreateRecModal`
     และ `openEditRecModal` (`:600` — เติมจาก `rec.glueIps?.join(", ") ?? ""`)
  2. **ช่องใหม่ในฟอร์ม** แสดง **เฉพาะเมื่อ `recType === "NS"`** วางต่อจากช่อง Record Value
     (บริเวณบรรทัด 2109):
     - `Label`: `Glue IP ของ Nameserver (ไม่บังคับ)`
     - `Input` + `Button` (variant `outline`) เรียงในแถวเดียว (`flex gap-2`)
       ปุ่มข้อความ `ค้นหา IP อัตโนมัติ`; ระหว่างค้นหาเปลี่ยนเป็น `กำลังค้นหา...` และ `disabled`
     - ปุ่ม `type="button"` (สำคัญ — ห้ามให้ submit ฟอร์ม), `onClick` เรียก
       `dnsServerService.resolveNameserver(recValue.trim())` แล้ว `setRecGlueIps(ips.join(", "))`
       ถ้า error → `setRecError(...)` ข้อความไทยที่อธิบายว่าค้นหาไม่สำเร็จ
       ถ้า `recValue` ว่าง → แจ้งให้กรอกชื่อ nameserver ก่อน (ไม่ยิง API)
     - helper text ใต้ช่อง: อธิบายว่ากรอกเองได้/หลาย IP คั่นด้วยจุลภาค/สูงสุด 4 IP
       และ **ถ้าเว้นว่าง = ประกาศ NS record อย่างเดียว ไม่ส่งต่อคำถามจริง**
  3. **Apex guard ฝั่ง UI**: ถ้า Host Name เป็น `@`/ว่าง/เท่าชื่อโซน ให้ `disabled` ช่อง glue IP
     + ปุ่ม และแสดงข้อความสั้น ๆ ว่าใช้กับ apex ไม่ได้ ให้ใช้ Forward Zone แทน
     (backend ยังเป็นด่านตัดสินตาม T-06)
  4. **validation ใน `handleSaveRecord`** (ต่อจากบล็อก NS เดิม บรรทัด 656-673):
     - แปลง `recGlueIps` → array ด้วย `.split(",").map(s => s.trim()).filter(Boolean)`
       (ต้อง trim ฝั่ง client เพราะ backend reject ช่องว่างขอบโดยเจตนา)
     - ถ้ามีมากกว่า 4 → error
     - ตรวจแต่ละค่าเป็น IPv4/IPv6 ที่ใช้ได้ (ใช้ `isValidIp` เดิมถ้าครอบคลุม v6 —
       ถ้าไม่ครอบคลุม ให้เพิ่ม regex v6 แบบง่ายในไฟล์นี้ **โดยไม่เข้มกว่า backend**)
     - ถ้า type ไม่ใช่ NS ให้ **ไม่ส่ง** `glueIps` ไปเลย (backend reject ถ้ามีค่า)
     - ใส่ `glueIps` ลง payload เฉพาะกรณี NS
  5. **ตาราง record** (บรรทัด 1231-1233): สำหรับ record ชนิด NS ที่มี `glueIps`
     ให้ต่อท้ายค่า value ด้วยข้อความ muted เล็ก ๆ เช่น `→ 203.0.113.53` (ใช้
     `text-muted-foreground`, ไม่เพิ่มคอลัมน์ใหม่เพื่อไม่ให้ layout พัง)
  6. **info note** (บรรทัด 1300): แก้บูลเล็ต `NS` เดิมที่บอกว่า "ไม่ได้ส่งต่อ (delegate) จริง"
     ให้สะท้อนพฤติกรรมใหม่:
     - ถ้าไม่ระบุ Glue IP → ประกาศ NS record อย่างเดียว (เหมือนเดิม)
     - ถ้าระบุ Glue IP → PiGate จะส่งต่อคำถามทั้งหมดใต้ชื่อนั้นไปยัง nameserver ปลายทางจริง
       (delegation แบบ forwarding ผ่าน dnsmasq ไม่ใช่ referral ตามโปรโตคอลแบบ bind9)
     - ระบุด้วยว่าใช้กับ apex ของโซนไม่ได้
- acceptance:
  - `cd frontend && yarn build` และ `yarn lint` ผ่าน
  - ไม่มีคลาสสี hardcode / `shadow-*` / `backdrop-blur-*` เพิ่มเข้ามา
  - ไม่มี dependency ใหม่ (ถ้าต้องเพิ่ม shadcn component ใหม่ ให้ใช้
    `npx shadcn@latest add <component>` ใน `frontend/` เท่านั้น — ห้าม yarn dlx)
- depends_on: ["T-08"]

### T-10 — tests: model (validation ของ glue IP)
- layer: model
- files: `backend/internal/model/dns_validate_test.go`
- instruction:
  เพิ่มเคสใน `TestValidateDNSRecord` (คงเคสเดิมทั้งหมดไว้):
  - `NS + GlueIPs:["203.0.113.53"]` → ไม่ error
  - `NS + GlueIPs:["203.0.113.53","2001:db8::53"]` → ไม่ error (v4+v6)
  - `NS + GlueIPs:["192.168.1.53"]` → **ไม่ error** (private ตั้งใจอนุญาต)
  - `NS + GlueIPs:["127.0.0.1"]` → error (loopback)
  - `NS + GlueIPs:["0.0.0.0"]` / `["224.0.0.1"]` → error
  - `NS + GlueIPs:["1.2.3.4\nserver=/evil/6.6.6.6"]` → error (injection)
  - `NS + GlueIPs:[" 1.2.3.4"]` → error (ช่องว่างขอบ)
  - `NS + GlueIPs:["1.2.3.4","1.2.3.4"]` → error (ซ้ำ)
  - `NS + GlueIPs` 5 ตัว → error (เกิน `DNSNSGlueMaxIPs`)
  - `A + GlueIPs:["1.2.3.4"]` → error (glue เฉพาะ NS)
  - `NS` ที่ไม่มี GlueIPs → ไม่ error (no regression)
- acceptance: `cd backend && go test ./internal/model/...` ผ่านทั้งหมด
- depends_on: ["T-02"]

### T-11 — tests: kernel generator
- layer: kernel
- files: `backend/internal/kernel/dns_server_test.go`
- instruction:
  เพิ่ม `TestBuildDNSConfig_NSDelegation` (ไฟล์นี้เป็น `//go:build linux` อยู่แล้ว)
  ครอบคลุม:
  1. NS ที่ subdomain + glue 1 IP → มีทั้ง `dns-rr=sub.example.local,2,...`
     **และ** `server=/sub.example.local/203.0.113.53`
  2. glue หลาย IP (v4+v6) → ได้ `server=` ครบทุกบรรทัด, ไม่ซ้ำ
  3. NS สองรายการชื่อเดียวกัน glue ซ้ำกันบางตัว → บรรทัด `server=` ไม่ซ้ำ
  4. **apex + glue** → มี `dns-rr=example.local,2,...` แต่ **ไม่มี** `server=/example.local/`
     (regression guard ของ §2.5 — ข้อนี้สำคัญที่สุดในไฟล์นี้)
  5. glue IP ที่ไม่ถูกต้อง (`"1.2.3.4\nserver=/evil/6.6.6.6"`, `"127.0.0.1"`) →
     ไม่มีสตริง `evil` และไม่มี `server=/…/127.0.0.1` ใน output
  6. glue host-record:
     - target `ns1.example.local` (อยู่ในโซนแม่ นอกชื่อที่ delegate) → มี
       `host-record=ns1.example.local,<ip>`
     - target `ns1.sub.example.local` (อยู่ใต้ชื่อที่ delegate) → **ไม่มี** `host-record=` ของชื่อนั้น
     - target `rohin.ns.cloudflare.com` (นอกโซน) → **ไม่มี** `host-record=` ของชื่อนั้น
       (ข้อนี้สำคัญ: ห้าม hijack ชื่อสาธารณะบน resolver ของทั้งบ้าน)
     - โซนที่มี A record ชื่อ `ns1` อยู่แล้ว → **ไม่** emit glue host-record ทับ
  7. **no-regression**: NS ที่ไม่มี glue → output byte-for-byte เท่ากับผลของ config
     เดิม (เทียบกับสตริงที่ประกอบเองในเทสต์ แบบเดียวกับ
     `TestBuildDNSConfig_QueryLogByteIdentical`)
- acceptance: `cd backend && go test ./internal/kernel/...` ผ่าน
- depends_on: ["T-04"]

### T-12 — tests: db migration + repository round-trip
- layer: db
- files: `backend/internal/db/dns_records_glue_migration_test.go` (ไฟล์ใหม่)
- instruction:
  ยึดรูปแบบจาก `db/dns_server_settings_migration_test.go`:
  1. `TestMigrationAddsGlueIPsToLegacyDNSRecords` — สร้างตาราง `dns_records`
     แบบเก่า (มี `'NS'` ใน CHECK แต่ไม่มี `glue_ips`) + seed 1 แถว → `migrate(rawDB)` →
     อ่านผ่าน `NewRepository(...).GetDNSRecordsByZone(...)` ต้องได้แถวเดิมครบและ
     `GlueIPs == []string{}` (ไม่ใช่ `nil`, ไม่ใช่ `[""]`) → รัน `migrate` ซ้ำต้องไม่ error
  2. `TestMigrationFromPreNSDNSRecords` — สร้างตารางแบบเก่ากว่านั้น
     (CHECK ยังไม่มี `'NS'`) → `migrate` ต้องผ่านทั้ง rebuild เดิม **และ** ALTER ใหม่
     ตามลำดับ แล้วต้อง insert record ชนิด NS พร้อม glue ได้
  3. `TestDNSRecordGlueRoundTrip` — `InitDB(":memory:")` → Create/Get/Update/Get
     record NS ที่มี glue 2 IP → ค่าตรงกันทุกครั้ง และการ Update ให้ glue ว่าง
     ต้องอ่านกลับได้ `[]string{}`
- acceptance: `cd backend && go test ./internal/db/...` ผ่าน
- depends_on: ["T-03"]

### T-13 — tests: service resolver (stub, ไม่ยิง DNS จริง)
- layer: service
- files: `backend/internal/service/dns_ns_lookup_test.go` (ไฟล์ใหม่)
- instruction:
  stub `resolveNSHostIPs` ด้วย `t.Cleanup` คืนค่าเดิม (รูปแบบเดียวกับ
  `stubLookupIP` ที่ `kernel/policy_chain_test.go:263`) — **ห้ามให้เทสต์ยิง DNS จริง**
  ครอบคลุม:
  - ชื่อถูกต้อง → คืน IP ที่ dedupe แล้ว, v4 มาก่อน v6, ไม่เกิน `DNSNSGlueMaxIPs`
  - ชื่อผิดรูป (`""`, `"ns1..x"`, `"ns1\nx"`) → `ErrNSLookupInvalidName` และ
    **stub ต้องไม่ถูกเรียกเลย** (ใช้ตัวนับใน stub ยืนยัน)
  - resolver คืนลิสต์ว่าง → `ErrNSLookupNotFound`
  - resolver คืน error → error ถูกส่งต่อ (ไม่ panic)
  - ยิงติด ๆ กันเกิน burst → `ErrNSLookupRateLimited`
- acceptance: `cd backend && go test ./internal/service/...` ผ่าน
- depends_on: ["T-05"]

### T-14 — tests: api (validation ผ่าน HTTP + endpoint ใหม่)
- layer: api
- files: `backend/internal/api/dns_validation_test.go`
- instruction:
  ต่อยอดจาก `TestDNSAndDHCPInjectionRejected` (มีเคส NS อยู่แล้วที่บรรทัด 50-60):
  - POST record NS + `glueIps:["1.2.3.4\nserver=/evil/6.6.6.6"]` → 400
  - POST record NS ที่ **apex** (`name:"@"`) + `glueIps:["203.0.113.53"]` → 400
  - POST record NS ที่ subdomain (`name:"sub"`) + `glueIps:["203.0.113.53"]` → **ไม่ใช่** 400
  - POST record A + `glueIps:["1.2.3.4"]` → 400
  - `GET /api/dns/resolve-ns?name=ns1%0Aevil` → 400
  (ถ้า test fixture ปัจจุบันไม่มีโซนจริงใน repo ให้ seed โซนตามที่ไฟล์เทสต์นี้ทำอยู่
  — อย่าเปลี่ยนโครงสร้าง helper `post` เดิม)
- acceptance: `cd backend && go test ./internal/api/...` ผ่าน
- depends_on: ["T-06"]

---

## 5. ลำดับการทำงาน

- สายหลัก (ต้องเรียง): **T-01 → T-02 → T-04**
- ขนานกับสายหลักได้: **T-03** (หลัง T-01), **T-05** (หลัง T-01), **T-07**, **T-08**
- หลังจากนั้น: **T-06** (ต้องมี T-02 + T-05), **T-09** (หลัง T-08)
- ชุดเทสต์ท้ายสุด: T-10 (หลัง T-02), T-11 (หลัง T-04), T-12 (หลัง T-03),
  T-13 (หลัง T-05), T-14 (หลัง T-06)
- ทุก Task ทำบน feature branch เดียวกัน **`feat/dns-ns-delegation`** (แตกจาก main
  หลัง PR 162 merge แล้ว; ถ้ายังไม่ merge ให้แตกจาก `feat/dns-ns-record`) แล้วเปิด PR
  — ห้าม push โค้ดขึ้น main; ไฟล์แผนนี้ใน `docs/` push main ได้
- **ห้ามทดสอบทีละ Task** — ทำครบทุก Task ก่อน แล้วให้ ai-qa รันเกณฑ์ท้ายแผนรอบเดียว

---

## 6. ข้อควรระวัง (Cautions) — อ่านก่อนเขียนโค้ดทุกครั้ง

1. **Apex คือกับดักที่ทำโซนพังทั้งโซน** (§2.5) — ต้องมี guard ครบ 3 ชั้น
   (UI / handler / generator) และมีเทสต์ยืนยัน (T-11 ข้อ 4)
2. **`host-record=` ห้าม emit ให้ชื่อนอกโซน** — จะกลายเป็นการ hijack ชื่อสาธารณะ
   บน resolver ของทั้งเครือข่าย
3. **ห้ามให้ค่าที่ validate ไม่ผ่านหลุดลงไฟล์ config เด็ดขาด** — `dnsmasq --test`
   จับ directive ที่ "ถูก syntax แต่ผิดเจตนา" ไม่ได้ (เหมือนกรณี hex ของ `dns-rr` เดิม)
4. **byte-for-byte no-regression** — ผู้ที่ไม่ได้ใช้ฟีเจอร์นี้ต้องได้ config เดิมทุกไบต์
   (filter ก่อน แล้วค่อยเช็ค `len()` — pattern เดียวกับ blocked domains/blocklists)
5. **ห้าม `TrimSpace` ค่า IP ในชั้น validator** — ค่าไปลงไฟล์แบบ verbatim
   ต้อง reject ไม่ใช่ตัดทิ้งเงียบ ๆ
6. **ห้ามใช้ `isGloballyRoutable` กับ glue IP** — private IP เป็น use case ที่ถูกต้อง
   (แต่ต้องเขียน comment อธิบายไว้ ไม่งั้นรีวิวรอบหน้าจะคิดว่าลืม)
7. **ห้ามเพิ่ม `exec.Command`** และห้ามเพิ่ม dependency ใหม่ทั้ง Go และ npm/yarn
8. **`backend/internal/api/dist/openapi.yaml` เป็น build artifact** — แก้เฉพาะ
   `docs/openapi.yaml` + `frontend/public/openapi.yaml`
9. **ไม่ต้องแก้ `kernel/mock.go`** — `MockDNSServerManager.ApplyZones` ไม่ render record
   (ยืนยันจากโค้ดแล้ว ไม่ใช่การละเว้นกฎ real/mock คู่กัน)
10. TTL ของ record ยังคงเป็นข้อมูลใน DB เท่านั้น (dnsmasq ใช้ `local-ttl` ทั่วไป)
    — ไม่มีอะไรเปลี่ยนในแผนนี้

---

## 7. จุดที่ต้องให้เจ้าของโปรเจกต์ตัดสิน (ถามก่อนเริ่ม T-01 ถ้าไม่เห็นด้วย)

| # | ประเด็น | ข้อเสนอของแผนนี้ | ทางเลือกอื่น |
|---|---|---|---|
| 1 | NS ที่ apex + glue IP | **ปฏิเสธ (400)** และแนะนำให้ใช้ Forward Zone | อนุญาตแต่เตือน (เสี่ยงทำโซนพัง — ไม่แนะนำ) |
| 2 | เก็บ glue ยังไง | คอลัมน์ `glue_ips TEXT` (comma-joined) ใน `dns_records` | ตารางแยก `dns_record_glue` (โค้ดเยอะกว่ามาก) |
| 3 | จำนวน glue IP สูงสุด | 4 | ปรับเป็น 2 หรือ 8 ได้ (ค่าคงที่ตัวเดียว) |
| 4 | resolver ที่ใช้ค้นหา IP | resolver ของตัวเครื่อง (`/etc/resolv.conf` ผ่าน `net.Resolver{PreferGo:true}`) | ยิงตรงไป upstream ที่ตั้งใน DNS Server settings (ซับซ้อนกว่า แต่ไม่โดน `local=` ของโซนตัวเองบัง) |
| 5 | คง `dns-rr=` ไว้คู่กับ `server=` | **คงไว้** (dig NS ยังเห็น NS record) | เอาออกเมื่อมี glue (จะ regress ฟีเจอร์ PR 162) |
| 6 | รองรับพอร์ตของ NS (`ip#5353`) | **ไม่รองรับ** (bare IP เท่านั้น ตรงกับกติกา upstream servers) | รองรับภายหลังถ้าจำเป็น |

---

## 8. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... ผ่านโดยไม่มี error/warning ใหม่",
    "cd backend && go vet ./... ไม่มี finding ใหม่",
    "cd backend && go test ./... ผ่านทั้งหมด (model/kernel/db/service/api)",
    "cd frontend && yarn build และ yarn lint ผ่าน",
    "รัน backend ด้วย -mock=true: สร้างโซน authoritative แล้วเพิ่ม record NS ที่ subdomain พร้อม glue IP ผ่าน UI ได้สำเร็จ (200) และค่า glueIps อ่านกลับมาถูกต้องหลังรีเฟรชหน้า",
    "record NS ที่ apex (@ หรือเว้นว่าง) + glue IP ถูกปฏิเสธด้วย 400 ทั้งตอน POST และ PUT พร้อมข้อความไทยที่แนะนำให้ใช้ Forward Zone",
    "record ชนิดอื่น (A/AAAA/CNAME/MX/TXT/PTR) ที่ส่ง glueIps มาด้วย → 400",
    "glue IP ที่มี newline / ช่องว่างขอบ / loopback / 0.0.0.0 / multicast / ซ้ำ / เกิน 4 ตัว → 400 ทุกกรณี",
    "GET /api/dns/resolve-ns?name=<ชื่อถูกต้อง> คืน 200 พร้อม ips[]; ชื่อผิดรูป/มี newline → 400; ชื่อที่ resolve ไม่ได้ → 404; ยิงรัว ๆ เกิน burst → 429",
    "ปุ่ม 'ค้นหา IP อัตโนมัติ' ใน UI เติมค่า IP ลงช่อง glue ได้จริง, ระหว่างค้นหาปุ่ม disabled, และ error แสดงเป็นข้อความไทยโดยไม่ทำให้ฟอร์ม submit",
    "buildDNSConfig ของโซนที่ไม่มี NS record หรือมี NS แต่ไม่มี glue → output เหมือนก่อนแก้ทุกไบต์ (no regression กับ A/AAAA/CNAME/MX/TXT/PTR และกับ NS publish-only เดิม)",
    "buildDNSConfig ของ NS ที่ subdomain + glue → มีทั้ง dns-rr= เดิม และ server=/<fqdn>/<ip> ครบทุก IP โดยไม่ซ้ำ",
    "glue host-record= ถูก emit เฉพาะกรณี target อยู่ในโซนแม่แต่ไม่อยู่ใต้ชื่อที่ delegate และยังไม่มี A/AAAA ชื่อนั้น — target นอกโซน (เช่น *.ns.cloudflare.com) ต้องไม่มี host-record= เด็ดขาด",
    "อัปเกรดจาก DB เดิม: migrate() รันซ้ำได้โดยไม่ error, record เดิมทั้งหมดยังอยู่ครบ และอ่านกลับมาได้ glueIps = [] (ทดสอบทั้ง DB ที่มี 'NS' แล้ว และ DB ที่ยังไม่มี)",
    "export config แล้ว import กลับ: record NS พร้อม glue IP กลับมาครบถ้วน และ backup รุ่นเก่าที่ไม่มีฟิลด์ glueIps ยัง import ได้ปกติ",
    "info note ในหน้า DNS Server และ openapi.yaml ทั้งสองสำเนา (docs/ + frontend/public/) อธิบายพฤติกรรม delegation ใหม่ตรงกัน และไม่มีข้อความเก่าที่บอกว่า 'ไม่ได้ delegate จริง' หลงเหลือ",
    "(บน Pi จริง) apply DNS แล้ว dnsmasq --test ผ่าน, dnsmasq restart สำเร็จ",
    "(บน Pi จริง — ข้อพิสูจน์หลักของแผน) โซน authoritative <zone> + NS record 'sub' + glue IP ของ nameserver จริง: `dig NS sub.<zone> @<pi>` ตอบชื่อ nameserver ที่ตั้งไว้, และ `dig A <อะไรก็ได้>.sub.<zone> @<pi>` ได้คำตอบจาก nameserver ปลายทางจริง (ไม่ใช่ NXDOMAIN จาก local=/<zone>/)",
    "(บน Pi จริง) ชื่ออื่นในโซนแม่ที่ไม่เกี่ยวกับ delegation ยังตอบจาก record ในเครื่องเหมือนเดิม (server=/sub/ ไม่กลืนทั้งโซน)",
    "ไม่มีการเพิ่ม exec.Command ใหม่, ไม่มี dependency ใหม่ (go.mod/package.json ไม่เปลี่ยน), ไม่มีการแก้ kernel/mock.go, ไม่มีการแก้ backend/internal/api/dist/openapi.yaml ด้วยมือ"
  ]
}
```
