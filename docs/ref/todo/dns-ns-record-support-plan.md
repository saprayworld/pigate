# แผนงาน: รองรับ DNS Record ชนิด NS (DNS Server / local zones)

สถานะ: TODO — สำหรับ ai-developer ทำทีละ Task, ทดสอบรวมครั้งเดียวท้ายแผน

## 1. บริบทจากการสำรวจโค้ด (ของจริง ณ ปัจจุบัน)

- Record type ที่รองรับตอนนี้: **A, AAAA, CNAME, MX, TXT, PTR** (ตรงกันทั้ง
  `model.ValidateDNSRecord` (`backend/internal/model/dns_validate.go:84`),
  generator `buildDNSConfig` (`backend/internal/kernel/dns_server.go:236`),
  dropdown ฝั่ง frontend (`frontend/src/pages/DnsServer.tsx:2055`) และ
  `openapi.yaml`)
- DNS Server จริง = **dnsmasq** เขียนไฟล์ `/etc/dnsmasq.d/pigate-dns.conf`
  จาก `buildDNSConfig` (pure function) แล้ว `dnsmasq --test` ก่อน แล้ว restart
  ผ่าน D-Bus
- Zone แบบ authoritative ใช้ `local=/<zone>/` + directive ต่อ record
  (`host-record=`, `cname=`, `mx-host=`, `txt-record=`, `ptr-record=`)
- **DB ไม่ต้อง migrate**: `dns_records(id, zone_id, name, type, value, ttl)`
  เก็บ type/value เป็น TEXT ทั่วไป (`db/connection.go:423`)
- **Kernel interface ไม่ต้องเพิ่ม method**: `DNSServerManager.ApplyZones`
  รับ `[]model.DNSZone` อยู่แล้ว และ `MockDNSServerManager.ApplyZones`
  (`kernel/mock.go:433`) แค่ log/นับ ไม่ render record → **mock ไม่ต้องแก้**
  (ตรวจสอบยืนยันแล้ว ไม่ใช่การละเลยกฎ real/mock คู่กัน)

## 2. ข้อจำกัดทางเทคนิคที่ต้องรู้ก่อน (สำคัญ — เจ้าของโปรเจกต์ควรรับทราบ)

dnsmasq **ไม่มี directive `ns-record`**. ทางเดียวที่ปล่อย NS record ได้คือ
directive ทั่วไป:

```
dns-rr=<fqdn>,<type-number>,<hex-rdata>
```

NS = type **2**, rdata = ชื่อโดเมนแบบ uncompressed wire format
(ทุก label นำหน้าด้วย byte ความยาว ปิดท้ายด้วย `00`)
เช่น `ns1.example.com` → `03 6e 73 31 07 65 78 61 6d 70 6c 65 03 63 6f 6d 00`

ผลที่ตามมา (ต้องเขียนกำกับใน UI/เอกสาร):

1. dnsmasq จะ **ตอบ NS record นี้เมื่อมีคนถาม type NS ตรง ๆ** เท่านั้น
   — มันจะ **ไม่ทำ delegation/referral จริง** และไม่สร้าง glue A record ให้
2. `local=/<zone>/` ยังทำให้ชื่อลูกใต้ delegation ตอบ NXDOMAIN เหมือนเดิม
   (ไม่ได้ถูกส่งต่อไป nameserver ที่ระบุใน NS)
3. `dns-rr` ไม่มีช่อง TTL ต่อ record — dnsmasq ใช้ `local-ttl` ทั่วไป
   (ฟิลด์ TTL ในฟอร์มจึงเป็นข้อมูลใน DB เท่านั้น เหมือนที่ record ชนิดอื่นเป็นอยู่แล้ว)

**จุดตัดสินใจสำหรับเจ้าของโปรเจกต์**: ถ้าต้องการให้ delegation "ทำงานจริง"
(query ใต้ sub-zone ถูกส่งต่อไป nameserver นั้น) ต้องเพิ่ม
`server=/<sub.zone>/<ip ของ NS>` ด้วย ซึ่งต้องมี field IP เพิ่ม (glue) และเป็น
scope ที่ใหญ่กว่าโจทย์นี้ — **แผนนี้ทำเฉพาะ "publish NS record" (ข้อ 1)**
ถ้าเจ้าของต้องการ delegation จริง ให้สั่งก่อนเริ่ม T-01

## 3. Task list

### T-01 — model: ตัวเข้ารหัสชื่อโดเมนเป็น wire-format hex
- layer: model
- files: `backend/internal/model/dns_wire.go` (ไฟล์ใหม่)
- instruction:
  สร้าง pure helper (ไม่มี I/O, ไม่มี build tag เพื่อให้ test ได้ทุก platform):
  ```go
  // EncodeDNSNameHex แปลงชื่อโดเมนเป็น RDATA แบบ uncompressed wire format
  // แล้วคืนค่าเป็น hex string ตัวพิมพ์เล็กไม่มีตัวคั่น สำหรับ dnsmasq dns-rr=
  func EncodeDNSNameHex(name string) (string, error)
  ```
  กติกา: ตัด trailing dot ออกก่อน; แยก label ด้วย `.`; **reject** เมื่อ
  ชื่อว่าง / มี label ว่าง (จุดซ้อน/ขึ้นต้นด้วยจุด) / label ยาว >63 /
  ชื่อรวม >253 / มีตัวอักษรนอก `[A-Za-z0-9-]` ต่อ label / label ขึ้นต้นหรือ
  ลงท้ายด้วย `-`; normalize เป็นตัวพิมพ์เล็กก่อนเข้ารหัส; ผลลัพธ์ปิดท้ายด้วย `00`
  เขียน doc comment อ้างอิงว่าใช้กับ `dns-rr=<fqdn>,2,<hex>` และห้ามใช้ name
  compression (offset ไม่ทราบตอน generate)
- acceptance:
  - `go build ./...` ผ่าน
  - ฟังก์ชันเป็น pure, ไม่มี import ที่ไม่ใช่ stdlib
  - `EncodeDNSNameHex("ns1.example.com")` = `"036e7331076578616d706c6503636f6d00"`
- depends_on: []

### T-02 — model: validation ของ NS record
- layer: model
- files: `backend/internal/model/dns_validate.go`
- instruction:
  เพิ่ม `case "NS":` ใน `ValidateDNSRecord` **ก่อน** `default:` โดยให้ตรงกับสิ่งที่
  generator จะเขียนจริงเป๊ะ ๆ (ห้ามเข้มกว่า generator):
  - `value := strings.TrimSpace(r.Value)`, ตัด trailing dot
  - ว่าง → error `"NS record value must not be empty"`
  - เรียก `EncodeDNSNameHex(target)` ถ้า error ให้ห่อเป็น
    `fmt.Errorf("NS record value %q is not a valid nameserver name: %w", r.Value, err)`
    (การเรียก encoder ตรงนี้คือหลักประกันว่า validator กับ generator ไม่มีวันเห็นต่างกัน
    และปิดช่องขึ้นบรรทัดใหม่/อักขระควบคุมโดยอัตโนมัติ)
  - อัปเดต doc comment ของ `ValidateDNSRecord` ให้ระบุ NS ด้วย
  งานนี้อยู่ในกลุ่ม **input validation → sensitive** ต้อง review เข้ม:
  ค่านี้ถูก interpolate ลงไฟล์ config ของ dnsmasq โดยตรง
- acceptance:
  - `go build ./...` ผ่าน
  - `NS` ไม่ตกไป `default` (`unsupported DNS record type`) อีกต่อไป
  - ค่าอย่าง `"ns1\ndns-rr=x,2,00"` ถูก reject
- depends_on: ["T-01"]

### T-03 — kernel: generate directive `dns-rr` สำหรับ NS
- layer: kernel
- files: `backend/internal/kernel/dns_server.go`
- instruction:
  ใน `buildDNSConfig` switch ของ `strings.ToUpper(rec.Type)` เพิ่ม `case "NS":`
  - normalize target แบบเดียวกับ CNAME: `strings.TrimSuffix(strings.TrimSpace(rec.Value), ".")`
    ถ้าไม่มี `.` ให้เติม `.<zoneName>` (ชื่อสั้น = ชื่อในโซนนี้)
  - `hex, err := model.EncodeDNSNameHex(target)`; ถ้า err ให้
    `log.Printf("[DNS Server] Skipping invalid NS record ...")` แล้ว `continue`
    (ห้ามเขียนบรรทัดที่เข้ารหัสไม่ผ่านลงไฟล์เด็ดขาด — dnsmasq --test จับ hex
    ที่ผิดความหมายไม่ได้)
  - เขียน `sb.WriteString(fmt.Sprintf("dns-rr=%s,2,%s\n", fullName, hex))`
  - ใส่ comment สั้น ๆ เหนือ case อธิบายว่า dnsmasq ไม่มี ns-record จึงใช้
    dns-rr type 2 + uncompressed wire format และ dnsmasq จะไม่ทำ delegation จริง
  ห้ามแตะลำดับ/ข้อความส่วนอื่นของไฟล์ config (ต้องคง byte-for-byte เดิมเมื่อไม่มี NS record)
  งานนี้เป็น **sensitive (config generation)** ต้อง review เข้ม
- acceptance:
  - `go build ./...` ผ่าน
  - โซนที่ไม่มี record NS → output เท่าเดิมทุกไบต์
  - ไม่มีการแก้ `kernel/mock.go` (mock ไม่ render record — ยืนยันแล้ว)
- depends_on: ["T-02"]

### T-04 — เอกสาร API (openapi)
- layer: api
- files: `docs/openapi.yaml`, `frontend/public/openapi.yaml`
- instruction:
  ในสคีมา DNS record (บริเวณ `docs/openapi.yaml:9034`) แก้คำอธิบาย `type` เป็น
  `One of A, AAAA, CNAME, MX, TXT, PTR, NS.` และเพิ่มคำอธิบาย value ของ NS
  (`NS=ชื่อ nameserver, ชื่อสั้นจะถูกต่อท้ายด้วยชื่อโซน; ปล่อยผ่านเป็น dnsmasq dns-rr type 2`)
  แก้ **ทั้งสองไฟล์ให้เนื้อหาเหมือนกันเป๊ะ** (frontend/public เป็นสำเนาที่หน้า ApiDocs ใช้)
- acceptance: ทั้งสองไฟล์มีเนื้อหาส่วนนี้ตรงกัน, YAML ยังถูกต้อง
- depends_on: []

### T-05 — frontend: dropdown + placeholder + validation ในฟอร์ม record
- layer: frontend
- files: `frontend/src/pages/DnsServer.tsx`
- instruction:
  1. เพิ่ม `<option value="NS">NS (Name Server)</option>` ต่อท้ายรายการ
     (บริเวณบรรทัด 2060)
  2. `placeholder` ของช่อง Record Value (บรรทัด ~2075): เพิ่มกรณี
     `recType === "NS"` → `"เช่น ns1.example.com"`
  3. เพิ่ม validation ใน `handleSaveRecord` (บริเวณบรรทัด ~650 ต่อจากเช็ค A):
     ถ้า `recType === "NS"` ให้ตรวจรูปแบบชื่อโดเมนด้วย regex ฝั่ง client
     (ตัด trailing dot ก่อน; label เป็น `[A-Za-z0-9-]` ยาว 1–63 ไม่ขึ้น/ลงท้ายด้วย `-`;
     ห้ามจุดซ้อน; ความยาวรวม ≤253) ถ้าไม่ผ่านให้ตั้ง
     `setRecError("สำหรับระเบียนประเภท NS ค่าของระเบียนต้องเป็นชื่อ nameserver ที่ถูกต้อง เช่น ns1.example.com")`
     แล้ว `setIsSaving(false); return` — กติกาต้องสอดคล้องกับ T-01/T-02
     (client validation เป็นเพียง UX; backend ยังเป็นด่านตัดสิน)
  4. เพิ่มบูลเล็ตอธิบายใน info note (บริเวณบรรทัด 1280) ต่อจาก TXT:
     `NS`: ระบุ nameserver ของโดเมน/โดเมนย่อย พร้อมหมายเหตุสั้น ๆ ว่า PiGate
     จะประกาศ NS record ให้เท่านั้น ไม่ได้ส่งต่อ (delegate) คำถามใต้โดเมนนั้นจริง
  ห้ามใส่สีแบบ hardcode / shadow / backdrop-blur (ดู `docs/rules_of_work.md`)
  และต้องรองรับทั้ง dark/light ตามคลาสเดิมของฟอร์ม
- acceptance:
  - `yarn build` และ `yarn lint` ผ่าน
  - ไม่มีการเพิ่ม dependency ใหม่
- depends_on: []

### T-06 — tests: model (encoder + validation)
- layer: model
- files: `backend/internal/model/dns_wire_test.go` (ใหม่),
  `backend/internal/model/dns_validate_test.go`
- instruction:
  - `dns_wire_test.go`: table test ของ `EncodeDNSNameHex` —
    เคสถูก (`ns1.example.com`, `ns1`, `NS1.Example.COM` ต้องได้ hex ตัวพิมพ์เล็กเดียวกับ
    ตัวพิมพ์เล็ก, ชื่อมี trailing dot) และเคสผิด (ว่าง, `..`, `.a`, `a..b`,
    label ยาว 64, ชื่อรวมยาวเกิน 253, `ns_1`, `-ns`, `ns-`, `"ns1\nx"`)
  - `dns_validate_test.go`: เพิ่มเคสใน `TestValidateDNSRecord` —
    `{"NS valid fqdn", DNSRecord{Name:"@", Type:"NS", Value:"ns1.example.com"}, false}`,
    `{"NS valid short", ... Value:"ns1"}, false}`,
    `{"NS trailing dot", ... Value:"ns1.example.com."}, false}`,
    `{"NS empty", ... Value:""}, true}`,
    `{"NS injection", ... Value:"ns1\ndns-rr=x,2,00"}, true}`,
    `{"NS bad label", ... Value:"ns1..example.com"}, true}`
    และคง `{"unsupported type" ... "SRV" ...}` ไว้เหมือนเดิม
- acceptance: `cd backend && go test ./internal/model/...` ผ่านทั้งหมด
- depends_on: ["T-02"]

### T-07 — tests: kernel generator
- layer: kernel
- files: `backend/internal/kernel/dns_server_test.go`
- instruction:
  เพิ่ม `TestBuildDNSConfig_NSRecord` (ไฟล์นี้เป็น `//go:build linux` อยู่แล้ว)
  ครอบคลุม:
  1. NS ที่ apex (`Name: "@"`) ในโซน `example.local` → config มี
     `dns-rr=example.local,2,036e73310765786d...` (คำนวณ hex จาก
     `model.EncodeDNSNameHex` ในตัว test เอง เพื่อไม่ hardcode ผิด — แต่ให้
     assert ด้วยว่า prefix เป็น `dns-rr=example.local,2,`)
  2. NS ที่ subdomain + target ชื่อสั้น (`Value: "ns1"`) → target ถูกต่อเป็น
     `ns1.example.local` ก่อนเข้ารหัส
  3. NS ที่ค่าผิด (`Value: "ns1\ndns-rr=evil"`) → **ไม่มี** สตริง `evil` และไม่มี
     บรรทัด `dns-rr=` ใด ๆ ใน output
  4. โซนที่มีเฉพาะ record A/CNAME → output ไม่มี `dns-rr=` เลย (no-regression)
- acceptance: `cd backend && go test ./internal/kernel/...` ผ่าน
- depends_on: ["T-03"]

### T-08 — tests: api handler (end-to-end validation ผ่าน HTTP)
- layer: api
- files: `backend/internal/api/dns_validation_test.go`
- instruction:
  ใน `TestDNSAndDHCPInjectionRejected` เพิ่ม 2 เคสต่อจากเคส A record:
  - POST `/api/dns/zones/zone-test/records` ด้วย
    `{Name:"@", Type:"NS", Value:"ns1\ndns-rr=evil"}` → ต้องได้ 400
  - POST ด้วย `{Name:"@", Type:"NS", Value:"ns1.example.com"}` → ต้อง **ไม่ใช่** 400
- acceptance: `cd backend && go test ./internal/api/...` ผ่าน
- depends_on: ["T-02"]

## 4. ลำดับการทำงาน

- สายหลัก (ต้องเรียง): T-01 → T-02 → T-03
- ทำขนานได้ตลอดเวลา: T-04, T-05
- หลังสายหลักเสร็จ: T-06 (หลัง T-02), T-07 (หลัง T-03), T-08 (หลัง T-02)
- ทุก Task ทำบน feature branch เดียวกัน `feat/dns-ns-record` แล้วเปิด PR
  (ห้าม push โค้ดขึ้น main; ไฟล์แผนนี้ใน `docs/` push main ได้)
- **ห้ามทดสอบทีละ Task** — ทำครบทุก Task ก่อน แล้วให้ ai-qa รันเกณฑ์ท้ายแผนรอบเดียว

## 5. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... ผ่านโดยไม่มี error/warning ใหม่",
    "cd backend && go test ./... ผ่านทั้งหมด (รวม model/kernel/api/service)",
    "cd frontend && yarn build และ yarn lint ผ่าน",
    "รัน backend ด้วย -mock=true แล้วสร้าง zone authoritative + record type NS ผ่าน UI ได้สำเร็จ (ได้ 201/200 ไม่ใช่ 400)",
    "ส่ง record NS ที่มี newline หรือชื่อผิดรูป (เช่น 'ns1\\ndns-rr=evil', 'ns1..x', ค่าว่าง) ผ่าน API → ได้ 400 ทุกกรณี",
    "buildDNSConfig ของโซนที่ไม่มี record NS ให้ผลลัพธ์เหมือนก่อนแก้ทุกไบต์ (no regression กับ A/AAAA/CNAME/MX/TXT/PTR)",
    "record type เดิมทั้ง 6 ชนิดยังสร้าง/แก้ไข/ลบ ผ่าน UI ได้ตามปกติ",
    "dropdown ในฟอร์ม Record มีตัวเลือก NS, placeholder เปลี่ยนตามชนิด, และกรอกค่าผิดแล้วขึ้นข้อความ error ภาษาไทยโดยไม่ยิง API",
    "info note และ openapi.yaml ทั้งสองสำเนา (docs/ + frontend/public/) ระบุ NS ตรงกัน",
    "(ถ้ามีเครื่อง Pi จริง) apply DNS แล้ว dnsmasq --test ผ่าน, dnsmasq restart สำเร็จ และ `dig NS <zone> @<pi>` ตอบค่า nameserver ที่ตั้งไว้",
    "ไม่มีการเพิ่ม exec.Command ใหม่, ไม่มี dependency ใหม่, ไม่มีการแก้ schema/migration ของ DB"
  ]
}
```
