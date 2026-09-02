# NS Delegation + CNAME — แก้บั๊ก "ส่งต่อไป NS แล้วเจอ CNAME ไม่คืน IP"

> เอกสารแผนงานสำหรับ **แก้บั๊ก**: NS delegation แบบ forwarding-based (PR 163, commit `29e25c0`)
> ส่งต่อคำถามไปยัง nameserver ปลายทางด้วย `server=/<fqdn>/<glue-ip>` ได้จริง แต่ถ้าปลายทาง
> ตอบกลับเป็น **CNAME ที่ชี้ออกนอกโซนที่ delegate** ผู้ใช้จะได้แค่ CNAME ไม่ได้ IP
> (A/AAAA ทำงานปกติ) แผนนี้เพิ่ม **โหมดการส่งต่อ (delegation mode)** ระดับ record
> เพื่อให้เลือกส่งต่อผ่าน upstream resolver ที่ทำ recursion ให้ครบทั้ง CNAME chain ได้
>
> วันที่เขียน: 2026-09-02 · Branch อ้างอิง: `fix/dns-ns-cname-resolution` (แตกจาก main `e763bbb`)
> สถานะใน README Feature Status: DNS Server = Completed → ไม่เปลี่ยน (เป็นการแก้บั๊ก)

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
- ผู้ใช้ที่ delegate โดเมนย่อยของโซน authoritative ออกไปยัง nameserver ภายนอก
  (เช่น Cloudflare) แล้วปลายทางตอบเป็น CNAME → **client ต้องได้ IP กลับมาจริง**
- พฤติกรรมเดิมทุกอย่าง (publish-only NS, glue-IP forwarding, A/AAAA/CNAME/MX/TXT/PTR
  ในโซน local) ต้องไม่ regress — โซนที่ไม่ได้ใช้ฟีเจอร์ใหม่ต้องได้ config **เหมือนเดิมทุกไบต์**
- ข้อจำกัดที่แก้ไม่ได้ ต้องถูกอธิบายให้ผู้ใช้เห็นใน UI + openapi (ไม่ปล่อยให้เดาเอง)

**นอกขอบเขต (ห้ามเสนอ/ห้ามทำในแผนนี้)**
- เปลี่ยน DNS engine (bind9/knot/PowerDNS/CoreDNS) หรือรัน service คู่ขนาน —
  เจ้าของโปรเจกต์ตัดออกไปแล้วตั้งแต่ `docs/ref/todo/dns-ns-delegation-plan.md`
- เขียน DNS resolver/forwarder ของ PiGate เอง (Go DNS listener ที่ไล่ CNAME chain) —
  ดู §7 ข้อ 1 เป็นทางเลือกที่ต้องให้เจ้าของโปรเจกต์ตัดสินแยกต่างหาก
- แก้เคส "CNAME record ในโซน local ที่ชี้ไปชื่อภายนอก" (เป็นข้อจำกัด dnsmasq คนละจุด
  ที่บันทึกไว้แล้วใน `docs/ref/complete/dns-server-fallback-fix-plan.md` ท้ายไฟล์)
- เพิ่ม dependency ใหม่ทั้ง Go และ yarn

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ / ไฟล์:บรรทัด |
|---|---|
| DNS engine | **dnsmasq เท่านั้น** — PiGate เขียน `/etc/dnsmasq.d/pigate-dns.conf` แล้ว restart ผ่าน D-Bus (`kernel/dns_server.go:36,523`) |
| Generator | `buildDNSConfig()` (pure) `backend/internal/kernel/dns_server.go:111`; NS case `:300-385` |
| บรรทัดที่เป็นต้นเหตุ | `sb.WriteString(fmt.Sprintf("server=/%s/%s\n", fullName, ip))` `kernel/dns_server.go:353` |
| Validator | `model.ValidateDNSRecord` `backend/internal/model/dns_validate.go:89`; NS case `:157-205` |
| Model | `model.DNSRecord` / `DNSRecordInput` `backend/internal/model/dns_server.go:13,39` (มี `GlueIPs`), const `DNSNSGlueMaxIPs` `:51` |
| DB | คอลัมน์ `glue_ips` — CREATE `db/connection.go:488`, migration ALTER `db/connection.go:193-213`, repo 4 จุด `db/repository.go:3369,3390,3405,3411` (+ helper `joinGlueIPs/splitGlueIPs` `:3344-3365`), restore `db/backup_repo.go:334` |
| API | apex guard `validateNSGlueAgainstZone` `api/handlers.go:3907`, เรียกที่ create `:3946` / update `:3996`; endpoint `GET /api/dns/resolve-ns` |
| Service | `DNSServerService.ResolveNameserver` `service/dns_ns_lookup.go:109` — ใช้ `net.Resolver{PreferGo:true}` เพื่อ **หา glue IP ตอนกรอกฟอร์ม** เท่านั้น ไม่เกี่ยวกับ query path |
| **การ parse DNS response** | **ไม่มีเลยในโค้ด PiGate** — `go.mod` ไม่มี `miekg/dns` และไม่มี `golang.org/x/net/dns/dnsmessage` (x/net เป็น indirect) PiGate ไม่เคยแตะแพ็กเก็ต DNS ของ client |
| Frontend | `pages/DnsServer.tsx` — glue state `:208-209`, ฟอร์ม `:2342-2382`, validate `:814-831`, payload `:834-842`, ตาราง `:1459-1461`, info note `:1529-1535`; type `data-mockup/mockData.ts:741-752`; client `services/dnsServerService.ts:723` |
| Mock kernel | `MockDNSServerManager.ApplyZones` ไม่ render record → **ไม่ต้องแก้ `kernel/mock.go`** |

**สรุปหนึ่งบรรทัด:** ไม่มีจุดใดใน Go ที่ "กรอง A/AAAA แล้วทิ้ง CNAME" — งานทั้งหมด
กระจุกอยู่ที่ **directive ที่ generator เขียนลง dnsmasq** (`kernel/dns_server.go:353`)
และการเพิ่ม field/DB/UI มาคุมมัน

---

## 2. Root cause (ยืนยันแล้ว — เป็นฐานของทั้งแผน)

### 2.1 ไม่ใช่บั๊กของโค้ด PiGate แต่เป็นข้อจำกัดเชิงสถาปัตยกรรมของ dnsmasq

dnsmasq เป็น **forwarder + cache** ไม่ใช่ recursive resolver — มันส่งคำถามออกไปแล้ว
ส่งคำตอบกลับมาตามที่ได้รับ ไม่ไล่ CNAME chain ต่อเอง คำยืนยันจาก Simon Kelley
(ผู้พัฒนา dnsmasq) ใน dnsmasq-discuss 2020q1 หัวข้อ *"dnsmasq not returning A record
for a CNAME with a server= config"*:

> "All the parts of an answer have to come from the same source ...
> a CNAME cannot point to records which comes from a different server."

และ man page ของ `--cname` ก็ระบุข้อจำกัดฝั่ง local record ในทำนองเดียวกัน:

> "There is a significant limitation on the target; it must be a DNS record which is
> known to dnsmasq and NOT a DNS record which comes from an upstream server."

### 2.2 ลำดับเหตุการณ์ที่ทำให้ "ได้ CNAME แต่ไม่ได้ IP"

1. โซน `example.net` เป็น authoritative → generator เขียน `local=/example.net/`
   (`kernel/dns_server.go:222`) ⇒ ทุกชื่อใต้โซนถูกตอบจากในเครื่อง/NXDOMAIN ไม่ออกไปข้างนอก
2. NS record `sub` + glue IP ⇒ `server=/sub.example.net/203.0.113.53`
   (`:353`) ชนะ `local=` เพราะ dnsmasq เลือก domain ที่ match ยาวที่สุด — ถึงตรงนี้ยังถูกต้อง
3. client ถาม `A www.sub.example.net` → dnsmasq forward ไป `203.0.113.53`
4. nameserver ปลายทางเป็น **authoritative-only** ไม่ recurse ให้:
   - ถ้า record เป็น A/AAAA → ตอบ A/AAAA ครบ ⇒ **ทำงานได้ (ตรงกับที่ผู้ใช้รายงาน)**
   - ถ้า record เป็น CNAME ที่ target อยู่ **นอก** โซนที่มันถือ (out-of-bailiwick เช่น
     `xxx.pages.dev`) → ตอบมาแค่ `CNAME` ไม่มี A/AAAA แนบมา
5. dnsmasq ส่งคำตอบนั้นต่อให้ client ตามเดิม และ **จะไม่ไปถาม A ของ target ต่อ**
   เพราะ target จะต้องมาจาก "แหล่งอื่น" ซึ่งขัดกฎในข้อ 2.1
6. stub resolver ของ client (glibc `getaddrinfo` ฯลฯ) ไม่ไล่ chain ให้ ⇒ **ไม่ได้ IP**

> **หมายเหตุ:** ถ้า CNAME target อยู่ *ในโซนเดียวกับที่ delegate* (in-bailiwick)
> nameserver ปลายทางจะแนบ A มาให้ในคำตอบเดียวอยู่แล้ว ⇒ เคสนั้นใช้งานได้ปกติวันนี้
> บั๊กเกิดเฉพาะ CNAME ที่ชี้ออกนอกโซนที่ delegate

### 2.3 ทางแก้ที่เลือก: โหมด `upstream` ⇒ `server=/<fqdn>/#`

dnsmasq man page (`--server`) มี special address `#`:

> "The special server address '#' means, 'use the standard servers', so
> `--server=/google.com/1.2.3.4` `--server=/www.google.com/#` will send queries for
> google.com and its subdomains to 1.2.3.4, except www.google.com (and its subdomains)
> which will be forwarded as usual."

⇒ `server=/sub.example.net/#` คือการ **เจาะรู `local=/example.net/` เฉพาะ subtree นั้น**
แล้วโยนให้ upstream resolver ปกติของเครื่อง (ที่ PiGate เขียนไว้เป็น `no-resolv` +
`server=<ip>` ที่ `kernel/dns_server.go:184-188`) จัดการ — upstream พวกนี้เป็น
**recursive resolver จริง** จึงไล่ CNAME chain ให้ครบและคืน CNAME + A/AAAA
มาใน **คำตอบเดียวจากแหล่งเดียว** ⇒ ผ่านกฎ §2.1 ⇒ client ได้ IP

**ข้อจำกัดที่ต้องบอกผู้ใช้ตรง ๆ:** โหมดนี้ใช้ได้ก็ต่อเมื่อชื่อที่ delegate
**resolve ได้จริงจาก upstream** (คือมี delegation อยู่ใน public DNS แล้ว) ซึ่งเป็นเคสหลัก
ของผู้ใช้ที่โดเมนแม่เป็นโดเมนจริงแต่ถูก `local=` ของ PiGate บังอยู่
ถ้า delegation มีอยู่แต่ใน PiGate และ nameserver อยู่ในวง LAN ล้วน ๆ โหมดนี้ช่วยไม่ได้
และ dnsmasq ก็ทำ CNAME ข้ามแหล่งไม่ได้อยู่ดี → ต้องไปทาง §7 ข้อ 1

### 2.4 ทางเลือกที่พิจารณาแล้ว "ตัดทิ้ง" (จดไว้กันคนมาถามซ้ำ)

| ทางเลือก | ทำไมถึงตัด |
|---|---|
| emit ทั้ง `server=/<x>/<glue-ip>` และ `server=/<x>/#` พร้อมกัน | dnsmasq มีหลาย server ต่อ domain จะเลือกส่งไปตัวใดตัวหนึ่ง (หรือทั้งหมดด้วย `--all-servers`) ⇒ คำตอบไม่ deterministic บางครั้งได้ CNAME เปล่า บางครั้งได้ IP — แย่กว่าเดิม |
| resolve CNAME ล่วงหน้าตอน generate config แล้ว emit `host-record=` | delegation เป็น **wildcard subtree** เราไม่รู้ล่วงหน้าว่า client จะถามชื่ออะไร; และ emit `host-record=` ให้ชื่อสาธารณะ = hijack ชื่อนั้นทั้ง LAN (Caution 2 ของแผนเดิม) |
| ใช้ query log (`kernel/dns_query_log.go`) จับ CNAME แล้วเขียน config ย้อนหลัง | race + เขียนไฟล์/restart dnsmasq ทุกครั้งที่เจอชื่อใหม่ = SD card wear + DNS สะดุด |
| เพิ่ม `miekg/dns` แล้วเขียน resolver เอง | dependency ใหม่ + งานใหญ่ระดับฟีเจอร์ ไม่ใช่ bugfix → ยกไป §7 ข้อ 1 ให้เจ้าของตัดสิน |

---

## 3. Data model ที่เลือก

**เพิ่มคอลัมน์ `delegation_mode TEXT NOT NULL DEFAULT ''` ใน `dns_records`**
(ตาม pattern ของ `glue_ips` เป๊ะ — ALTER แบบ additive ไม่แตะ CHECK constraint
จึงไม่ต้อง rebuild table)

- ค่าที่ยอมรับ: `""` (ค่าของทุกแถวเดิม = ตีความเป็น `glue` ⇒ พฤติกรรมเดิมทุกไบต์),
  `"glue"`, `"upstream"`
- **ห้าม** เอา `"#"` ไปยัดใน `glue_ips` แทนการเพิ่มคอลัมน์ (validator ของ glue เป็น
  `net.ParseIP` ล้วน การเจาะรูให้ค่าที่ไม่ใช่ IP ผ่านเข้าไฟล์ config คือความเสี่ยง injection)

**ตารางสรุปพฤติกรรมหลังแผนนี้**

| mode | GlueIPs | output ใน pigate-dns.conf |
|---|---|---|
| `""` / `glue` | ว่าง | `dns-rr=` อย่างเดียว (publish-only) — **เหมือนเดิมทุกไบต์** |
| `""` / `glue` | มี | `dns-rr=` + `server=/<fqdn>/<ip>` ต่อ IP (+ glue `host-record=`) — **เหมือนเดิมทุกไบต์** |
| `upstream` | ว่าง | `dns-rr=` + `server=/<fqdn>/#` |
| `upstream` | มี | `dns-rr=` + `server=/<fqdn>/#` (ไม่มีบรรทัด `server=` ของ glue IP) + glue `host-record=` ตามกติกาเดิม |
| ทุก mode ที่ apex | – | ไม่มี `server=` เด็ดขาด (guard 3 ชั้นเดิม) |

---

## 4. Task list

### T-01 — model: field + const + helper ตัดสินโหมด
- layer: model
- files: `backend/internal/model/dns_server.go`
- instruction:
  1. เพิ่มฟิลด์ใน `DNSRecord` (หลัง `GlueIPs`) และใน `DNSRecordInput`:
     ```go
     // DelegationMode selects HOW an NS record's subtree is forwarded.
     // "" (every pre-existing row) and "glue" behave identically to before:
     // forward to the GlueIPs, or publish-only when there are none.
     // "upstream" emits a single `server=/<fqdn>/#` instead, handing the
     // subtree to the box's normal upstream resolvers — the only way to get
     // a complete answer when the delegated nameserver replies with a CNAME
     // pointing outside its own zone (dnsmasq cannot merge records from two
     // different sources; see docs/ref/todo/dns-ns-delegation-cname-fix-plan.md §2).
     DelegationMode string `json:"delegationMode"`
     ```
  2. เพิ่มค่าคงที่ + helper ในไฟล์เดียวกัน:
     ```go
     const (
         DNSNSDelegationModeGlue     = "glue"
         DNSNSDelegationModeUpstream = "upstream"
     )

     // EffectiveNSDelegationMode normalizes DelegationMode to exactly one of
     // the two real modes. Called by BOTH ValidateDNSRecord and the generator
     // so the two can never disagree (same discipline as EncodeDNSNameHex).
     func EffectiveNSDelegationMode(r DNSRecord) string
     ```
     กติกา: `strings.ToLower(strings.TrimSpace(...))`; `""` → `glue`; `"upstream"` →
     `upstream`; อย่างอื่น → `glue` (generator ปลอดภัยไว้ก่อน ส่วน validator เป็นคนปฏิเสธ)
  ห้ามแก้ signature ฟังก์ชันเดิม, ห้ามเพิ่ม dependency
- acceptance: `cd backend && go build ./...` ผ่าน; JSON key เป็น `delegationMode`
- depends_on: []

### T-02 — model: validation (SENSITIVE — input validation)
- layer: model
- files: `backend/internal/model/dns_validate.go`
- instruction:
  1. ต่อจาก guard `glueIps is only allowed on NS records` (`:95-97`) เพิ่ม guard คู่กัน:
     ถ้า `strings.TrimSpace(r.DelegationMode) != ""` และ type ไม่ใช่ NS → error
  2. ใน `case "NS":` เพิ่มการตรวจค่า: ค่าที่ยอมรับคือ `""`, `"glue"`, `"upstream"`
     (เทียบแบบ **case-insensitive หลัง TrimSpace** — ต่างจาก glue IP ที่ห้าม trim
     เพราะค่านี้ **ไม่เคยถูก interpolate ลง config**: generator เขียนแค่ literal
     `#` หรือ IP ที่ผ่าน `net.ParseIP` แล้ว ให้เขียน comment กำกับเหตุผลนี้ไว้)
     ค่าอื่น → `fmt.Errorf("NS delegationMode %q is invalid (allowed: %q, %q)", ...)`
  3. **ไม่ต้อง** บังคับว่า `upstream` ต้องไม่มี glue IP — glue IP ในโหมดนี้ยังใช้สร้าง
     glue `host-record=` ให้ชื่อ nameserver ที่อยู่ในโซนแม่ได้ (และการบังคับจะทำให้
     ผู้ใช้สลับโหมดของ record เดิมไม่ได้ถ้าไม่ล้างช่องก่อน) — เขียน comment กำกับ
  4. อัปเดต doc comment ของ `ValidateDNSRecord` ให้พูดถึง `DelegationMode`
  **ห้าม** ตรวจ apex ในไฟล์นี้ (ไม่รู้จักชื่อโซน — ทำที่ handler = T-05)
- acceptance: build ผ่าน; record ที่ไม่ตั้ง `DelegationMode` ให้ผลเหมือนเดิมทุกกรณี
- depends_on: ["T-01"]

### T-03 — db: schema + migration + repository + backup restore
- layer: db
- files: `backend/internal/db/connection.go`, `backend/internal/db/repository.go`,
  `backend/internal/db/backup_repo.go`
- instruction:
  1. `connection.go:488` — เพิ่ม `delegation_mode TEXT NOT NULL DEFAULT ''`
     ใน `CREATE TABLE IF NOT EXISTS dns_records` ต่อจาก `glue_ips`
  2. `connection.go` — เพิ่มบล็อก migration **ต่อจากบล็อก `glue_ips` (`:213`) เท่านั้น**
     (DB เก่ามากจะไหลผ่าน rebuild `'NS'` → ALTER `glue_ips` → ALTER ตัวนี้ ตามลำดับ)
     - query `sqlite_master` ใหม่เป็นตัวแปรของตัวเอง (ห้าม reuse
       `sqlCreateDNSRecordsGlue` — ค่าเปลี่ยนไปแล้วหลัง ALTER ก่อนหน้า)
     - idempotency: `strings.Contains(sql, "delegation_mode")`
     - `ALTER TABLE dns_records ADD COLUMN delegation_mode TEXT NOT NULL DEFAULT ''`
     - `log.Println("[Migration] Added delegation_mode column to dns_records table")`
  3. `repository.go` — แก้ 4 จุด (`GetDNSRecordsByZone:3369`, `GetDNSRecordByID:3390`,
     `CreateDNSRecord:3405`, `UpdateDNSRecord:3411`) ให้ SELECT/INSERT/UPDATE
     คอลัมน์ `delegation_mode` (เก็บดิบ ๆ ไม่ต้อง normalize ในชั้น repo)
  4. `backup_repo.go:334` — เพิ่ม `delegation_mode` ใน INSERT ของ restore
     (backup รุ่นเก่าที่ไม่มีฟิลด์นี้ → `""` ⇒ พฤติกรรมเดิม)
  ห้ามเปลี่ยนลำดับ migration เดิม, ห้ามแตะ `DELETE FROM dns_records`
- acceptance: build ผ่าน; `InitDB(":memory:")` ได้ตารางที่มี `delegation_mode`;
  `migrate()` ซ้ำไม่ error; record เดิมอ่านกลับได้ `DelegationMode == ""`
- depends_on: ["T-01"]

### T-04 — kernel: generator (SENSITIVE — config generation)
- layer: kernel
- files: `backend/internal/kernel/dns_server.go`
- instruction:
  แก้เฉพาะภายใน `buildDNSConfig` (ต้องยังเป็น pure function ไม่มี I/O)
  **ห้ามแตะ `kernel/mock.go`** (mock ไม่ render record)
  1. **pre-pass ต่อโซน** (วางข้าง ๆ `existingAddrNames` `:238-250`) สร้าง
     `upstreamDelegatedNames map[string]bool` = set ของ `fullName` (lowercase)
     ของ NS record ที่ `model.EffectiveNSDelegationMode(rec) == upstream`
     และ **ไม่ใช่ apex** — ทำเป็น pre-pass เพราะต้อง deterministic ไม่ขึ้นกับลำดับ record
  2. ใน `case "NS":` หลังบรรทัด `dns-rr=` (`:317`) แทนที่บล็อก
     `if len(rec.GlueIPs) == 0 { continue }` (`:325-327`) ด้วย:
     ```
     mode := model.EffectiveNSDelegationMode(rec)
     if mode == glue && len(rec.GlueIPs) == 0 → continue        // publish-only (เหมือนเดิม)
     if strings.EqualFold(fullName, zoneName) → log.Printf skip + continue   // apex (เดิม)
     emit header "# NS delegation (forwarding-based)" ครั้งเดียวต่อโซน (ข้อความเดิม ห้ามแก้)
     if mode == upstream:
         dedupe ด้วย emittedDelegation[fullName+"|#"]
         sb.WriteString(fmt.Sprintf("server=/%s/#\n", fullName))
     else:
         if upstreamDelegatedNames[lowerFullName] {
             log.Printf(... "skipping glue server= lines: %q is already delegated
                            in upstream mode" ...); // ชนกันได้ผลไม่แน่นอน → upstream ชนะ
         } else {
             loop rec.GlueIPs ตามโค้ดเดิมทุกบรรทัด (:337-354)
         }
     // บล็อก glue host-record (:366-384) คงไว้เหมือนเดิม ทำงานทั้งสองโหมด
     ```
  3. เขียน comment เหนือบล็อกใหม่อธิบายว่า `#` = "use the standard servers"
     ของ dnsmasq, ทำไมโหมดนี้ถึงแก้ CNAME ได้ (อ้าง §2.1/§2.3 ของแผนนี้)
     และทำไมถึงไม่ emit ทั้งสองแบบพร้อมกัน (อ้าง §2.4)
  4. **ห้าม** เปลี่ยนข้อความ/ลำดับ/บรรทัดว่างของส่วนอื่นในไฟล์ config
- acceptance: build ผ่าน; โซนที่ไม่มี NS หรือ NS ที่ `DelegationMode == ""` →
  output เหมือนก่อนแก้ทุกไบต์; ไม่มี `exec.Command`/I/O ใหม่
- depends_on: ["T-02"]

### T-05 — api: plumb field + ขยาย apex guard (SENSITIVE)
- layer: api
- files: `backend/internal/api/handlers.go`
- instruction:
  1. `HandleCreateDNSRecord` (`:3930-3940`) และ `HandleUpdateDNSRecord` (`:3980-3990`) —
     ใส่ `DelegationMode: input.DelegationMode` ลง struct `model.DNSRecord` ที่ประกอบขึ้น
  2. เงื่อนไขที่เรียก apex guard ปัจจุบันคือ
     `strings.EqualFold(record.Type, "NS") && len(record.GlueIPs) > 0` (`:3946`, `:3996`)
     → **ต้องยิงเมื่อ record จะสร้างบรรทัด `server=` ด้วย** เปลี่ยนเป็น helper ตัวเดียว
     ที่ใช้ร่วมกันสองที่ เช่น
     ```go
     func nsRecordEmitsDelegation(r model.DNSRecord) bool {
         return strings.EqualFold(r.Type, "NS") &&
             (len(r.GlueIPs) > 0 ||
              model.EffectiveNSDelegationMode(r) == model.DNSNSDelegationModeUpstream)
     }
     ```
  3. `validateNSGlueAgainstZone` (`:3907`) — เปลี่ยน early-return บรรทัดแรก
     (`:3908`) ให้ใช้ `nsRecordEmitsDelegation` แทน แล้วปรับข้อความ error ให้ครอบคลุม
     ทั้งสองโหมด (ยังต้องแนะนำให้ใช้ **Forward Zone** ถ้าอยากส่งต่อทั้งโซน)
  **ห้าม** เพิ่ม endpoint ใหม่ในแผนนี้ (`GET /api/dns/resolve-ns` เดิมพอแล้ว)
- acceptance: build ผ่าน; POST NS `delegationMode:"upstream"` ที่ `@` → 400,
  ที่ subdomain → ไม่ใช่ 400; POST A record ที่ส่ง `delegationMode` มา → 400
- depends_on: ["T-02", "T-01"]

### T-06 — เอกสาร API + design doc
- layer: api
- files: `docs/openapi.yaml`, `frontend/public/openapi.yaml`,
  `docs/ref/complete/dns-system-design.md`
- instruction:
  1. ใน openapi **ทั้งสองไฟล์** (บริเวณ `glueIps` `docs/openapi.yaml:9077` และ `:9121`)
     เพิ่ม property คู่กัน:
     ```yaml
     delegationMode:
       type: string
       enum: ["", "glue", "upstream"]
       description: >-
         NS records only. "" / "glue" (default) forwards the delegated subtree
         straight to glueIps. "upstream" instead emits `server=/<fqdn>/#`, handing
         the subtree to the box's normal upstream resolvers - required when the
         delegated nameserver answers with a CNAME pointing outside its own zone,
         because dnsmasq cannot combine records coming from two different servers
         (the client would otherwise get the CNAME with no A/AAAA). Only works when
         the delegated name is resolvable by those upstreams. Not allowed on the
         zone apex, and not allowed on non-NS records - both return 400.
       example: "upstream"
     ```
     และแก้คำอธิบาย `value` ของ NS ให้พูดถึงสองโหมด
  2. `docs/ref/complete/dns-system-design.md` — เพิ่มหัวข้อสั้น ๆ "ข้อจำกัด CNAME
     ของ dnsmasq" อ้างคำพูดของ Simon Kelley (§2.1) + ตารางพฤติกรรมใน §3 ของแผนนี้
  **ห้ามแก้มือ** `backend/internal/api/dist/openapi.yaml` (build artifact จาก `build.sh`)
- acceptance: openapi ทั้งสองไฟล์เนื้อหาส่วนนี้ตรงกันเป๊ะ, YAML ยัง parse ได้
- depends_on: []

### T-07 — frontend: type + API client
- layer: frontend
- files: `frontend/src/data-mockup/mockData.ts`, `frontend/src/services/dnsServerService.ts`
- instruction:
  1. `mockData.ts:751` — เพิ่ม `delegationMode?: "glue" | "upstream"` ใน `interface DNSRecord`
     (optional เพื่อไม่ให้ mock data เดิมพัง) พร้อม comment สั้น ๆ
  2. `dnsServerService.ts` — `createRecord`/`updateRecord` ใช้
     `Omit<DNSRecord,"id"|"zoneId">` อยู่แล้ว จึงไม่ต้องแก้ signature
     แต่ต้องตรวจว่า branch `IS_MOCK_MODE` ที่ spread `{...record}` เก็บ
     `delegationMode` ไว้จริง (ถ้าไม่ ให้เพิ่ม)
- acceptance: `cd frontend && yarn build && yarn lint` ผ่าน, ไม่มี dependency ใหม่
- depends_on: []

### T-08 — frontend: UI เลือกโหมด + helper text + ตาราง + info note
- layer: frontend
- files: `frontend/src/pages/DnsServer.tsx`
- instruction:
  ยึด `docs/rules_of_work.md`: ใช้เฉพาะ shadcn ใน `components/ui/`,
  **ห้ามสี hardcode**, **ห้าม `shadow-*` / `backdrop-blur-*`**, รองรับ dark/light
  1. state ใหม่ `recDelegationMode` (`"glue" | "upstream"`, default `"glue"`)
     — reset ใน `openCreateRecModal` (`~:705`) และเติมจาก
     `rec.delegationMode ?? "glue"` ใน `openEditRecModal` (`~:716`)
  2. **`<select>` ใหม่** แสดงเฉพาะ `recType === "NS"` วาง **เหนือ** ช่อง Glue IP
     (ก่อนบรรทัด `:2342`) ใช้ `<select>` สไตล์เดียวกับ Record Type (`:2304-2313`)
     — ห้ามใช้ Combobox (จะบังคับให้ Dialog ต้อง `modal={false}`)
     - `glue` → "ส่งต่อไปยัง Nameserver ที่ระบุ (ใช้ Glue IP)"
     - `upstream` → "ส่งต่อผ่าน Upstream Resolver (รองรับ CNAME ข้ามโซน)"
     - `disabled` เมื่อ `isRecNameApex` (เหมือนช่อง glue)
  3. helper text ใต้ select (ใช้ `text-muted-foreground` ขนาดเดียวกับของเดิม `:2375`):
     - โหมด `glue`: ส่งคำถามตรงไปที่ nameserver ที่ระบุ — **ถ้าปลายทางตอบเป็น CNAME
       ที่ชี้ออกนอกโซน จะได้แค่ CNAME ไม่ได้ IP** (ข้อจำกัดของ dnsmasq)
     - โหมด `upstream`: ส่งให้ upstream resolver ของเครื่องเป็นคนหาให้ทั้งสาย
       รวม CNAME — **ใช้ได้เฉพาะเมื่อชื่อที่ delegate หาเจอจาก upstream จริง**
       และคำถามใต้ชื่อนี้จะออกอินเทอร์เน็ตตามปกติ (ผ่านการกรอง blocklist ด้วย)
  4. ช่อง Glue IP: เมื่อโหมดเป็น `upstream` **ยังแก้ไขได้** แต่แก้ helper text (`:2375-2379`)
     ให้บอกว่าในโหมดนี้ Glue IP ใช้แค่สร้าง A record ให้ชื่อ nameserver ไม่ได้ใช้ส่งต่อ
  5. `handleSaveRecord` — ใส่ `delegationMode` ลง payload เฉพาะเมื่อ `recType === "NS"`
     (ต่อจากบล็อก glue `:833-842`) type อื่น **ห้ามส่ง** (backend ตอบ 400)
  6. ตาราง record (`:1459-1461`) — ถ้า `rec.delegationMode === "upstream"`
     ให้แสดง `Badge variant="secondary"` เล็ก ๆ ข้อความ `upstream` ต่อท้ายค่า value
     (ห้ามเพิ่มคอลัมน์ใหม่)
  7. info note บูลเล็ต NS (`:1529-1535`) — เพิ่มประโยคอธิบายสองโหมด และเตือน
     ข้อจำกัด CNAME ของโหมด glue
- acceptance: `yarn build` + `yarn lint` ผ่าน; ไม่มีคลาสสี hardcode/`shadow-*`/
  `backdrop-blur-*` เพิ่ม; ไม่มี dependency ใหม่
- depends_on: ["T-07"]

### T-09 — tests: model
- layer: model
- files: `backend/internal/model/dns_validate_test.go`
- instruction: เพิ่มเคสใน `TestValidateDNSRecord` (คงเคสเดิมทั้งหมด) +
  เพิ่ม `TestEffectiveNSDelegationMode`
  - `NS` + `DelegationMode:""` → ไม่ error และ `EffectiveNSDelegationMode == "glue"`
  - `NS` + `"glue"` / `"upstream"` / `"UPSTREAM"` / `" upstream "` → ไม่ error
  - `NS` + `"recursive"` / `"#"` / `"upstream\nserver=/evil/6.6.6.6"` → error
  - `NS` + `"upstream"` ไม่มี GlueIPs → ไม่ error
  - `NS` + `"upstream"` + GlueIPs 1 ตัว → ไม่ error (ตั้งใจอนุญาต)
  - `A` + `DelegationMode:"upstream"` → error
- acceptance: `cd backend && go test ./internal/model/...` ผ่าน
- depends_on: ["T-02"]

### T-10 — tests: kernel generator (สำคัญที่สุดในแผน)
- layer: kernel
- files: `backend/internal/kernel/dns_server_test.go`
- instruction: เพิ่ม `TestBuildDNSConfig_NSDelegationUpstreamMode` (ไฟล์เป็น
  `//go:build linux` อยู่แล้ว) ครอบคลุม:
  1. NS `sub` + `DelegationMode:"upstream"` ไม่มี glue → มี `dns-rr=sub.example.local,2,...`
     **และ** `server=/sub.example.local/#` และ **ไม่มี** `server=/sub.example.local/<ip>` ใด ๆ
  2. `upstream` + glue 1 IP → มี `server=/sub.example.local/#`, **ไม่มี**
     `server=/sub.example.local/203.0.113.53`, แต่ยังมี glue
     `host-record=ns1.example.local,203.0.113.53` เมื่อ target อยู่ในโซนแม่
  3. `upstream` ที่ **apex** → มี `dns-rr=example.local,2,...` แต่ **ไม่มี**
     `server=/example.local/#` (regression guard ของ apex trap)
  4. NS ชื่อเดียวกันสองรายการ (glue + upstream ปนกัน, สลับลำดับใน slice ทั้งสองแบบ)
     → ได้ `server=/x/#` เท่านั้น ไม่มีบรรทัด glue ของชื่อนั้น **และผลเหมือนกันทั้งสองลำดับ**
  5. `DelegationMode` ค่าขยะที่หลุด DB มา (`"bogus"`) → ถือเป็น glue,
     ไม่มีสตริง `bogus` โผล่ใน output
  6. **no-regression byte-for-byte**: โซนเดียวกันที่ record ทุกตัวมี
     `DelegationMode:""` → output ตรงกับสตริงที่ประกอบเองในเทสต์เป๊ะ
     (รูปแบบเดียวกับ `TestBuildDNSConfig_QueryLogByteIdentical`)
- acceptance: `cd backend && go test ./internal/kernel/...` ผ่าน
- depends_on: ["T-04"]

### T-11 — tests: db migration + round-trip
- layer: db
- files: `backend/internal/db/dns_records_delegation_mode_migration_test.go` (ไฟล์ใหม่)
- instruction: ยึดรูปแบบจาก `db/dns_records_glue_migration_test.go`
  1. `TestMigrationAddsDelegationModeToLegacyDNSRecords` — สร้างตารางที่มี `glue_ips`
     แต่ไม่มี `delegation_mode` + seed 1 แถว → `migrate()` → อ่านผ่าน repo ต้องได้
     `DelegationMode == ""` และข้อมูลเดิมครบ → `migrate()` ซ้ำไม่ error
  2. `TestMigrationFromPreGlueDNSRecords` — ตารางเก่าที่ไม่มีทั้ง `glue_ips` และ
     `delegation_mode` (และเคสที่ CHECK ยังไม่มี `'NS'`) → migrate ต้องผ่านทั้ง 3 ขั้น
     ตามลำดับ แล้ว insert NS + glue + `delegation_mode='upstream'` ได้
  3. `TestDNSRecordDelegationModeRoundTrip` — `InitDB(":memory:")` →
     Create/Get/Update/Get record NS ที่มี `DelegationMode:"upstream"` ค่าตรงทุกรอบ
     และ update กลับเป็น `""` ต้องอ่านได้ `""`
- acceptance: `cd backend && go test ./internal/db/...` ผ่าน
- depends_on: ["T-03"]

### T-12 — tests: api
- layer: api
- files: `backend/internal/api/dns_validation_test.go`
- instruction: ต่อยอด `TestDNSAndDHCPInjectionRejected` (มีเคส NS/glue อยู่แล้ว `:64-91`)
  - POST NS `name:"sub"`, `delegationMode:"upstream"`, ไม่มี glueIps → **ไม่ใช่** 400
  - POST NS `name:"@"`, `delegationMode:"upstream"`, ไม่มี glueIps → **400** (apex guard
    ต้องยิงถึงแม้ไม่มี glue — นี่คือ regression guard หลักของ T-05)
  - POST NS `delegationMode:"upstream\nserver=/evil/6.6.6.6"` → 400
  - POST A record `delegationMode:"upstream"` → 400
  - PUT record เดิมให้เป็น `delegationMode:"upstream"` ที่ apex → 400
  อย่าเปลี่ยนโครงสร้าง helper `post` เดิม
- acceptance: `cd backend && go test ./internal/api/...` ผ่าน
- depends_on: ["T-05"]

---

## 5. ลำดับการทำงาน

- สายหลัก (ต้องเรียง): **T-01 → T-02 → T-04**
- ขนานได้: **T-03** (หลัง T-01), **T-06**, **T-07**
- หลังจากนั้น: **T-05** (หลัง T-01/T-02), **T-08** (หลัง T-07)
- ชุดเทสต์ท้ายสุด: T-09 (หลัง T-02), T-10 (หลัง T-04), T-11 (หลัง T-03), T-12 (หลัง T-05)
- ทุก Task ทำบน feature branch **`fix/dns-ns-cname-resolution`** แล้วเปิด PR —
  ห้าม push โค้ดขึ้น main (ไฟล์แผนนี้ใน `docs/` push main ได้)
- **ห้ามทดสอบทีละ Task** — ทำครบทุก Task ก่อน แล้วให้ ai-qa รัน §8 รอบเดียว

---

## 6. ข้อควรระวัง (Cautions) — อ่านก่อนเขียนโค้ดทุกครั้ง

1. **`server=/<zone>/#` ที่ apex = ทำโซนพังทั้งโซน** — ร้ายแรงกว่ากรณี glue เดิมด้วยซ้ำ
   (ทั้งโซนจะถูกโยนออก upstream และ `host-record=` ทุกตัวถูก override)
   ปัจจุบัน apex guard ที่ handler ยิงเฉพาะเมื่อ `len(GlueIPs) > 0` ⇒ **ถ้าลืมแก้
   เงื่อนไขใน T-05 โหมด upstream ที่ไม่มี glue จะหลุด guard ไปทั้งเส้น** —
   ต้องมี guard ครบ 3 ชั้น (UI T-08 ข้อ 2 / handler T-05 / generator T-04) และมีเทสต์
   ยืนยันทั้ง T-10 ข้อ 3 และ T-12
2. **ห้าม emit ทั้ง `server=/x/<ip>` และ `server=/x/#` ให้ชื่อเดียวกัน** — คำตอบจะไม่
   deterministic (บางครั้ง CNAME เปล่า บางครั้งได้ IP) แก้ด้วย pre-pass
   `upstreamDelegatedNames` (T-04 ข้อ 1) ไม่ใช่การพึ่งลำดับของ `zone.Records`
3. **byte-for-byte no-regression** — ผู้ที่ไม่ได้ใช้โหมดใหม่ต้องได้ config เดิมทุกไบต์
   รวมถึง **ข้อความ header `# NS delegation (forwarding-based)`** ที่ห้ามแก้
4. **`#` ต้องอยู่ร่วมกับ `no-resolv` ได้** — PiGate เขียน `no-resolv` + `server=<ip>`
   เมื่อมี upstream (`kernel/dns_server.go:179-190`) ⇒ "standard servers" คือ
   `server=` เปล่า ๆ เหล่านั้น **ต้องยืนยันบน Pi จริง** (§8) ถ้าพบว่าไม่ทำงาน
   ทางสำรอง: emit `server=/<fqdn>/<upstreamIP>` ต่อ IP จาก `validUpstreams`
   ที่คำนวณอยู่แล้วในฟังก์ชันเดียวกัน — แต่ทางสำรองนี้ **พังเมื่อ `validUpstreams` ว่าง**
   (ตกไปใช้ `/etc/resolv.conf`) จึงเลือก `#` เป็นทางหลัก
5. **โหมด upstream เปลี่ยนเส้นทางข้อมูล** — คำถามใต้ชื่อที่ delegate จะออกอินเทอร์เน็ต
   ผ่าน upstream แทนที่จะไปหา nameserver ที่ผู้ใช้ระบุ และจะ **ตกอยู่ใต้ blocklist/
   blocked domains** ของ PiGate ด้วย ต้องบอกไว้ใน helper text (T-08 ข้อ 3)
6. **ห้าม TrimSpace ค่า glue IP** (กติกาเดิม) แต่ `delegationMode` **trim ได้**
   เพราะไม่เคยถูก interpolate ลง config — ต้องเขียน comment แยกให้ชัด ไม่งั้นรีวิว
   รอบหน้าจะคิดว่าขัดกันเอง
7. **migration ต้องต่อท้ายบล็อก `glue_ips`** (`db/connection.go:213`) และ query
   `sqlite_master` ใหม่เป็นตัวแปรของตัวเอง — reuse ตัวแปรเดิมจะอ่านสคีมาเก่า
8. **`backend/internal/api/dist/openapi.yaml` เป็น build artifact** — แก้เฉพาะ
   `docs/openapi.yaml` + `frontend/public/openapi.yaml`
9. **ไม่ต้องแก้ `kernel/mock.go`** — `MockDNSServerManager.ApplyZones` ไม่ render record
10. **ห้ามเพิ่ม `exec.Command`** และห้ามเพิ่ม dependency ใหม่ (go.mod/package.json
    ต้องไม่เปลี่ยน) — โดยเฉพาะ **ห้ามเพิ่ม `miekg/dns`** ในแผนนี้
11. **การทดสอบบน Pi จริงมีความเสี่ยง DNS ล่มทั้งบ้าน** — ให้ทดสอบตอนเข้าถึงตัวเครื่องได้
    และเตรียม `sudo systemctl restart dnsmasq` / ลบไฟล์ `pigate-dns.conf` เป็นทางถอย

---

## 7. จุดที่ต้องให้เจ้าของโปรเจกต์ตัดสิน

| # | ประเด็น | ข้อเสนอของแผนนี้ | ทางเลือกอื่น |
|---|---|---|---|
| 1 | เคส "delegation มีอยู่แต่ใน PiGate + nameserver อยู่ใน LAN" ซึ่งโหมด `upstream` ช่วยไม่ได้ | **ยอมรับเป็นข้อจำกัด** อธิบายใน UI/docs (แผนนี้) | เขียน **Go CNAME-chasing forwarder** ฟังที่ `127.0.0.1#<port>` แล้วให้ dnsmasq ส่งต่อมาที่มัน (ไล่ chain เอง + splice คำตอบ) — ใช้ `golang.org/x/net/dns/dnsmessage` ที่มีอยู่ใน module graph แล้ว (indirect) แต่เป็น **ฟีเจอร์ใหญ่**: DNS listener ใหม่ = attack surface ใหม่, ต้องทำ UDP+TCP/EDNS0/truncation/timeout/loop guard/cache และต้องมีคู่ real/mock — ประเมิน ~400-600 บรรทัด + เทสต์ **ต้องขออนุมัติแยกเป็นแผนใหม่** |
| 2 | ชื่อโหมด | `glue` / `upstream` | `direct` / `recursive` (สื่อความหมายพอกัน แต่ `upstream` ตรงกับศัพท์ที่ใช้อยู่แล้วใน DNS Server settings) |
| 3 | `upstream` + มี Glue IP พร้อมกัน | **อนุญาต** (glue ใช้แค่ทำ `host-record=` ให้ชื่อ NS) | ปฏิเสธที่ API (mental model ง่ายกว่า แต่ผู้ใช้ต้องล้างช่องก่อนสลับโหมด) |
| 4 | ค่า default ของ record ใหม่ใน UI | `glue` (ตรงกับพฤติกรรมเดิม) | `upstream` (แก้บั๊กให้คนส่วนใหญ่ทันที แต่เปลี่ยนพฤติกรรมโดยผู้ใช้ไม่ได้เลือก) |
| 5 | เครื่องมือวินิจฉัยใน UI (ปุ่ม "ทดสอบ resolve ชื่อนี้" ที่โชว์ทั้ง CNAME chain) | **นอกขอบเขต** แผนนี้ | ทำเป็นแผนแยกภายหลัง (ช่วยผู้ใช้แยกแยะว่าติดที่ delegation หรือที่ CNAME) |

---

## 8. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... ผ่านโดยไม่มี error/warning ใหม่",
    "cd backend && go vet ./... ไม่มี finding ใหม่",
    "cd backend && go test ./... ผ่านทั้งหมด (model/kernel/db/service/api)",
    "cd frontend && yarn build และ yarn lint ผ่าน",
    "go.mod / go.sum / package.json / yarn.lock ไม่เปลี่ยน (ไม่มี dependency ใหม่) และไม่มี exec.Command เพิ่ม",
    "buildDNSConfig: โซนที่ record ทุกตัวมี delegationMode ว่าง (รวม NS publish-only และ NS+glue เดิม) ให้ output เหมือนก่อนแก้ทุกไบต์ รวมถึงข้อความ header '# NS delegation (forwarding-based)'",
    "buildDNSConfig: NS ที่ subdomain + delegationMode=upstream → มี dns-rr= เดิม และมี server=/<fqdn>/# หนึ่งบรรทัด และไม่มี server=/<fqdn>/<ip> ของ record นั้นเลย",
    "buildDNSConfig: NS ที่ apex + delegationMode=upstream (ไม่ว่าจะมี glue หรือไม่) → ไม่มี server=/<zoneName>/ ใด ๆ แต่ยังมี dns-rr= ของ apex",
    "buildDNSConfig: NS ชื่อเดียวกันที่มีทั้งโหมด glue และ upstream → ได้เฉพาะ server=/x/# และผลลัพธ์เหมือนกันไม่ว่าจะเรียงลำดับ record แบบใด",
    "รัน backend -mock=true: เพิ่ม/แก้ NS record พร้อมเลือกโหมดใน UI ได้ และค่า delegationMode อ่านกลับมาถูกต้องหลังรีเฟรชหน้า",
    "POST/PUT NS record delegationMode=upstream ที่ apex (@ หรือเว้นว่าง หรือชื่อเท่าชื่อโซน) → 400 พร้อมข้อความไทยที่แนะนำให้ใช้ Forward Zone (ทดสอบทั้งกรณีมี glue และไม่มี glue)",
    "record ชนิดอื่น (A/AAAA/CNAME/MX/TXT/PTR) ที่ส่ง delegationMode มาด้วย → 400; ค่า delegationMode ที่ไม่รู้จักหรือมี newline → 400",
    "อัปเกรดจาก DB เดิม: migrate() รันซ้ำได้โดยไม่ error, record เดิมทุกแถวยังอยู่ครบและอ่านกลับได้ delegationMode = \"\" (ทดสอบทั้ง DB ที่มี glue_ips แล้ว และ DB ที่ยังไม่มีทั้ง glue_ips และ 'NS' ใน CHECK)",
    "export config แล้ว import กลับ: NS record พร้อม delegationMode/glueIps กลับมาครบ และ backup รุ่นเก่าที่ไม่มีฟิลด์นี้ยัง import ได้ปกติ",
    "openapi ทั้งสองสำเนา (docs/ + frontend/public/) มี delegationMode เหมือนกันเป๊ะ, backend/internal/api/dist/openapi.yaml ไม่ถูกแก้ด้วยมือ, และ dns-system-design.md มีหัวข้อข้อจำกัด CNAME ของ dnsmasq",
    "(บน Pi จริง) apply DNS แล้ว dnsmasq --test ผ่าน และ dnsmasq restart สำเร็จ ทั้งกรณีมี server=/<fqdn>/# และไม่มี",
    "(บน Pi จริง — ข้อพิสูจน์หลักของแผน) โซน authoritative <zone> + NS record 'sub' โหมด upstream: `dig A <ชื่อที่ปลายทางตอบเป็น CNAME>.sub.<zone> @<pi>` ต้องได้ทั้งบรรทัด CNAME และบรรทัด A/AAAA ใน ANSWER section (เดิมได้แค่ CNAME)",
    "(บน Pi จริง) record ที่เป็น A/AAAA ใต้ชื่อที่ delegate ยังตอบถูกต้องเหมือนเดิมทั้งสองโหมด (ไม่ regress)",
    "(บน Pi จริง) ชื่ออื่นในโซนแม่ที่ไม่เกี่ยวกับ delegation ยังตอบจาก record ในเครื่องเหมือนเดิม — server=/sub/# ไม่กลืนทั้งโซน",
    "(บน Pi จริง) โหมด glue เดิมยังส่งคำถามตรงไปที่ glue IP จริง (ยืนยันด้วย `dig A <ชื่อ>.sub.<zone> @<pi>` เทียบกับ `dig A <ชื่อ>.sub.<zone> @<glue-ip>`) — และ **เป็นเรื่องปกติที่โหมดนี้จะยังคืนแค่ CNAME** ตามข้อจำกัดของ dnsmasq (§2.1) ห้ามรายงานเป็น failure",
    "(บน Pi จริง) ยืนยันว่า `#` ทำงานร่วมกับ no-resolv ได้จริง: ถ้าพบว่าไม่ทำงาน ให้หยุดและรายงานกลับหา ai-tech-lead ตาม Caution 4 (มีทางสำรองเตรียมไว้แล้ว) ห้ามแก้แนวทางเอง"
  ]
}
```
