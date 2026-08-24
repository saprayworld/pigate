# DNS Blocklist Import — subscribe URL / upload ไฟล์ hosts ขนาดใหญ่ (แท็บใหม่ใน DNS Server)

> เอกสารแผนงานสำหรับฟีเจอร์: เพิ่มความสามารถ import blocklist สาธารณะรูปแบบ hosts
> (เช่น StevenBlack/hosts ~93,515 โดเมน) เข้า dnsmasq ผ่านไฟล์แยกต่างหาก
> **โดยไม่ใช้ SQLite เลย ทั้งรายชื่อโดเมนและ metadata** (metadata อยู่ใน JSON manifest)
> — deny-list เดิม (`dns_blocked_domains`, cap 1000) ยังอยู่เหมือนเดิม ไม่ถูกแตะ
>
> วันที่เขียน: 2026-08-22
> **แก้ไขรอบที่ 3 (2026-08-22): เพิ่ม "เลือกโหมดบล็อกได้ต่อ list" (`sinkhole` / `nxdomain`)**
> ตามที่ owner สั่งเพิ่ม — ยกเลิกข้อจำกัด "sinkhole อย่างเดียว" ของ R2 เดิม
> (ส่วนที่ถูกแก้: §2.1, §2.2, §2.3, §2.6, §2.7, §5 ข้อ 16-20, R2/R9-R12,
> T-01, T-02, T-03, T-05, T-06, T-08, T-09, T-10, T-11, T-12, T-13, Final Acceptance)
> Branch อ้างอิง: `feat/statistics-reference-popover` · ทำงานจริงบน branch ใหม่ `feat/dns-blocklist-import`
> ที่มา: comment ใน `backend/internal/model/dns_validate.go:346-350` —
> "Bulk import of public blocklists is a future feature that would need a separate --conf-file, not this cap raised."

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็น)**
- แท็บใหม่ `Blocklists` ในหน้า `/network/dns-server`
- เพิ่มได้ 2 แบบ: **Subscribe URL** (backend fetch เอง, กด Refresh ซ้ำได้) และ **Upload file**
- แต่ละรายการมี: ชื่อ, ชนิด (url/upload), URL ต้นทาง, **โหมดบล็อก (NXDOMAIN / Sinkhole)**,
  enable/disable, จำนวนโดเมนหลัง parse, เวลา fetch ล่าสุด, ขนาดไฟล์, error ล่าสุด
- **เลือกโหมดบล็อกได้ต่อ list** แบบเดียวกับแท็บ Blocked Domains เดิม (ค่าเริ่มต้น = `sinkhole`
  ซึ่งต่างจาก deny-list เดิมที่ default เป็น `nxdomain` — เหตุผลอยู่ใน §2.1.4)
- กด **Apply DNS** (ปุ่มเดิมของหน้านี้) แล้ว dnsmasq เริ่มบล็อกจริง
- **หน้า Statistics > DNS แท็บ "Blocked Query" นับ hit จาก blocklist ได้ด้วย** พร้อมบอกว่าโดน list ไหน
  และโดนด้วยโหมดอะไร

**นอกขอบเขต (ตัดชัดเจน)**
- ไม่แตะ deny-list เดิม (`dns_blocked_domains`) ไม่ขยับ `DNSBlockedDomainsMax = 1000`
- ไม่มี allow-list / whitelist / regex exception
- ไม่มี per-client (per-IP/group) blocking
- **ไม่มีโหมดบล็อกราย "โดเมน" ภายใน list** — โหมดเป็นคุณสมบัติของ list เท่านั้น (เหตุผลใน §2.1.2)
- ไม่แสดงตารางรายชื่อโดเมน 93k แถวใน UI (จงใจ — browser ค้าง)
- auto-refresh ตามกำหนดเวลา = optional task ท้ายสุด (T-14) ทำทีหลัง ไม่อยู่ในเฟสนี้

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริงแล้ว ณ วันเขียน)

| ส่วน | สถานะ | ไฟล์:บรรทัด (โดยประมาณ) |
|---|---|---|
| dnsmasq config generator | มีแล้ว — pure function `buildDNSConfig(zones, interfaces, upstreams, queryLog, blocked)` | `backend/internal/kernel/dns_server.go:109-340` |
| ไฟล์ config ที่เขียน | `/etc/dnsmasq.d/pigate-dns.conf` (+ base `pigate-base.conf`) | `dns_server.go:22`, `dnsmasq_base.go:13` |
| validate ก่อน apply | `dnsmasq --test --conf-file=…` (`exec.Command` แบบ arg คงที่ ไม่มี user input) | `dns_server.go:27-34` |
| reload | `RestartServiceViaDBus("dnsmasq.service")` | `dns_server.go:342-344` |
| **deny-list เดิม: โหมด nxdomain** | emit `server=/<domain>/` (1 บรรทัด/โดเมน) | `dns_server.go:327-329` |
| **deny-list เดิม: โหมด sinkhole** | emit `address=/<domain>/0.0.0.0` + `address=/<domain>/::` (2 บรรทัด/โดเมน) | `dns_server.go:321-326` |
| ค่าคงที่โหมด | `DNSBlockModeNXDomain="nxdomain"`, `DNSBlockModeSinkhole="sinkhole"` (`""` = nxdomain) | `model/dns_validate.go:342-353` |
| สิทธิ์เขียน `/etc/dnsmasq.d` | มีแล้ว — `setfacl -m u:pigate:rwx` | `install.sh:143-148` |
| ไดเรกทอรีข้อมูล | `/var/lib/pigate` มีแล้ว `pigate:netdev` mode 775 | `install.sh:307-312` |
| service layer DNS Server | `ApplyAll()` + `SetBlockedDomainsSink` | `backend/internal/service/dns_server.go:45-104` |
| kernel interface | `DNSServerManager` (ApplyZones/ClearCache/WatchDNSLog) | `backend/internal/kernel/interfaces.go:167-187` |
| mock kernel | `MockDNSServerManager` (in-memory, `ApplyCount`) | `backend/internal/kernel/mock.go:405-427` |
| validator โดเมน | `ValidateBlockedDomain` (regex `reZoneName`, ≤253, ต้องมีจุด, ตรวจ mode) | `model/dns_validate.go:355-399` |
| deny-list matcher (สถิติ) | `dnsBlockIndex` — `map[string]string` (domain→mode), suffix-match ไต่ parent 16 ชั้น | `service/dns_block_index.go:35-122` |
| จุด classify สถิติ | `recordDomainQuery` เรียก `blockIndex.Empty()` → `Match()` ที่ record-time | `service/dns_query_stats.go:204-212` |
| sink ป้อน index | `StatisticsService.SetBlockedDomains` ← `SetBlockedDomainsSink` ← `ApplyAll` | `service/statistics.go:157,314-316`, `service/dns_server.go:45-101` |
| UI เลือกโหมดของ deny-list | shadcn `<Select>` 2 ตัวเลือก + badge ในตาราง | `frontend/src/pages/DnsServer.tsx:139,1112-1117,1742-1749` |
| HTTP client ขาออก | มีตัวเดียวในโปรเจกต์ — ipinfo.io + `isGloballyRoutable` | `service/ipinfo.go:34-239` |
| body limit ของ API | 1 MB ทั้งระบบ ยกเว้น path ใน `bodyLimitExemptPaths` | `api/middleware.go:351-373` |
| backup export | **typed JSON ไฟล์เดียว** (`model.BackupFile`), checksum sha256 คำนวณจาก marshalled `Config` | `service/backup.go:96-180`, `model/backup.go:9-93` |
| ไฟล์ config นอก DB ที่มีอยู่แล้ว | `/var/lib/pigate/pigate.conf` เขียนโดย `internal/config` (`Parse`/`Resolve`/`Write`) | `backend/internal/config/` |
| หน้า DNS Server (frontend) | shadcn `Tabs` 3 แท็บ + deep-link `?tab=` whitelist | `frontend/src/pages/DnsServer.tsx:83-101, 791-796` |
| **blocklist ทั้งฟีเจอร์** | **ยังไม่มีเลยทุกชั้น** | — |

**สรุป:** ฟีเจอร์ใหม่เต็มก้อน แต่มีต้นแบบครบทุก pattern ที่ต้องใช้ในโค้ดเดิมแล้ว

---

## 2. แนวทางเทคนิค

### 2.1 สองกลไก render ต่อหนึ่ง list เลือกด้วย `blockMode` (แก้ไขรอบที่ 3 — R9)

**ยกเลิกข้อจำกัด "sinkhole เท่านั้น" ของฉบับก่อน** — ผู้ใช้เลือกโหมดได้ต่อ list และ
`blockMode` เป็นตัวกำหนดว่าเราจะ render list นั้นด้วยกลไกไหนของ dnsmasq:

| `blockMode` | directive ที่ emit ลง `pigate-dns.conf` | ไฟล์ที่ generate | ครอบ subdomain? | ต้นทุน |
|---|---|---|---|---|
| `sinkhole` (**default ของ blocklist**) | `addn-hosts=/var/lib/pigate/blocklists/<id>.hosts` | `<id>.hosts` (`0.0.0.0 <domain>`) | **ไม่ครอบ** (exact-match) | ถูกที่สุด — dnsmasq เก็บเป็น hosts record |
| `nxdomain` | `conf-file=/var/lib/pigate/blocklists/<id>.conf` | `<id>.conf` (`address=/<domain>/` บรรทัดละโดเมน) | **ครอบ** (label-boundary) | แพงกว่า — parse ทุกครั้งที่ Apply (§2.1.3) |

```
# /etc/dnsmasq.d/pigate-dns.conf (ท้ายไฟล์ ต่อจาก deny-list เดิม)
# Blocklists (bulk import)
addn-hosts=/var/lib/pigate/blocklists/bl-3f9a2c.hosts   # blockMode=sinkhole
conf-file=/var/lib/pigate/blocklists/bl-7d1e04.conf     # blockMode=nxdomain
```

**หลักฐานจาก man page ของ dnsmasq (ยืนยันแล้ว ไม่ใช่การเดา):**
- `-A, --address=/<domain>[/<domain>...]/[<ipaddr>]` — *"one or more domains with no address returns a
  no-such-domain answer, so `--address=/example.com/` is equivalent to `--server=/example.com/` and
  **returns NXDOMAIN for example.com and all its subdomains**"*
- *"Matching of domains is normally done on complete labels, so /google.com/ matches google.com and
  www.google.com but NOT supergoogle.com"* (label-boundary เหมือน `dnsBlockIndex.Match` ของเราพอดี)
- `addn-hosts` = hosts file ธรรมดา → **map ชื่อ → IP เท่านั้น ทำ NXDOMAIN ไม่ได้โดยสิ้นเชิง**
  จึงเป็นเหตุผลที่โหมด nxdomain ต้องเปลี่ยนไปใช้ไฟล์ `conf-file=` แยก ตามที่ comment ใน
  `model/dns_validate.go:348-349` เดาไว้แต่แรกว่า "would need a separate --conf-file"

**ทำไมโหมด nxdomain ใช้ `address=/<domain>/` ไม่ใช่ `server=/<domain>/` (ทั้งที่ deny-list เดิมใช้ `server=`):**
man ระบุว่าสองอันนี้ให้ผลเหมือนกันเป๊ะ แต่ CHANGELOG ของ dnsmasq ต่างกันในเชิงสเกล
- 2.86: *"Major rewrite of the DNS server and domain search handling code… drastically improves
  performance and reduces memory foot-print when configuring large numbers of domains… **Lookup times
  now grow as log-to-base-2 of the number of domains**, rather than greater than linearly, as before"*
- 2.88: *"Optimise reading large numbers of --server options … **can cause long start times with
  thousands of --server options because the work needed is O(n^2)**"* — ปัญหานี้อยู่ที่เส้นทางของ
  `--server` (การ re-use server record ตอนอ่านซ้ำ) โดยเฉพาะ

→ ที่ระดับ 93k โดเมน `address=` เป็นเส้นทางที่ปลอดภัยกว่าอย่างมีนัยสำคัญ
**ห้ามเปลี่ยน deny-list เดิมมาใช้ `address=/d/`** — ที่ ≤1000 รายการไม่มีประโยชน์ และจะทำให้
golden test ของ `buildDNSConfig` พังทั้งชุดโดยไม่ได้อะไรกลับมา (ต้องคง byte-compat)

**ไม่ batch หลายโดเมนต่อบรรทัด:** syntax `address=/a.com/b.com/c.com/` ใช้ได้ตาม man
แต่ dnsmasq อ่านบรรทัด config ผ่านบัฟเฟอร์ขนาดจำกัด (`MAXDNAME`) และพฤติกรรมเมื่อบรรทัดยาวเกิน
**ไม่ได้ระบุไว้ในเอกสาร** (อาจถูกตัดเงียบ ๆ กลายเป็น directive เพี้ยน = อันตรายมากกับไฟล์ที่ dnsmasq โหลด)
→ **1 โดเมน/บรรทัดเสมอ** ถ้าวัดบนบอร์ดจริงแล้วเวลา parse ไม่ยอมรับได้ ค่อยพิจารณา batch
ในเฟสถัดไปพร้อมทดสอบขอบเขตบรรทัดจริง ๆ (อย่าเดา)

#### 2.1.1 `.hosts` เป็นแหล่งข้อมูลหลักเสมอ `.conf` เป็นไฟล์ derived

**กฎ:** ทุก list เขียนไฟล์ `<id>.hosts` เสมอ ไม่ว่าโหมดไหน (เป็น canonical store ของรายชื่อโดเมน)
ส่วน `<id>.conf` เขียนเพิ่ม **เฉพาะ** list ที่ `blockMode == nxdomain` และถูก **regenerate จาก `<id>.hosts`
โดยไม่ต้อง fetch ใหม่** เมื่อผู้ใช้สลับโหมด

ผลที่ตามมา (ตั้งใจให้เป็นแบบนี้ เพื่อไม่ต้องรื้อ task อื่น):
- T-06 (index สถิติ) ยัง stream จาก `<id>.hosts` เหมือนเดิมทุก list — ไม่ต้องเขียน parser ตัวที่สอง
- T-09 (backup) ยังพกเฉพาะ `<id>.hosts` เหมือนเดิม — `.conf` สร้างใหม่ได้จาก `.hosts` ตอน import
- สลับโหมดไม่ต้องต่อเน็ต ทำ offline ได้ (สำคัญกับ list แบบ upload ที่ re-fetch ไม่ได้)
- **ต้นทุน:** list โหมด nxdomain กินดิสก์ ~2 เท่า (≈3 MB + ≈3 MB ที่ 93k โดเมน) — ยอมรับได้
  และเป็นการเขียนครั้งเดียวต่อ ingest/สลับโหมด ไม่ใช่ต่อ query (SD wear ไม่มีนัยสำคัญ)

#### 2.1.2 ทำไมเลือกโหมดได้แค่ "ต่อ list" ไม่ใช่ "ต่อโดเมน"

1. **ต้นทางไม่มีข้อมูลนี้** — hosts format มีแค่ `<ip> <hostname>` ไม่มีคอลัมน์โหมด และเราทิ้ง
   คอลัมน์ IP ต้นฉบับทั้งหมดด้วยเหตุผลด้านความปลอดภัย (§2.2) จึงไม่มีทางอนุมานโหมดรายโดเมนจากไฟล์ได้
2. **ไม่มี UI ที่จะจัดการมันได้** — เราตัด "ตารางรายชื่อ 93k แถว" ออกจากขอบเขตตั้งแต่ต้น (§0)
   ถ้าไม่มีตารางก็ไม่มีที่ให้ผู้ใช้กดเปลี่ยนโหมดรายโดเมน
3. **จะทำให้กลไกทั้งสองปนกันในไฟล์เดียวไม่ได้** — `addn-hosts` กับ `conf-file` เป็นไฟล์คนละรูปแบบ
   ถ้าโหมดเป็นรายโดเมน ต้องแตกทุก list ออกเป็น 2 ไฟล์เสมอ + เก็บสถานะรายโดเมนที่ไหนสักที่
   (= กลับไปใช้ DB ซึ่ง owner ตัดสินใจไม่ใช้แล้วใน R1)
4. **ผู้ใช้ที่ต้องการโหมดต่างกันรายโดเมนมีทางออกอยู่แล้ว** — deny-list เดิม (แท็บ Blocked Domains,
   cap 1000) เลือกโหมดรายโดเมนได้ครบ และมีความสำคัญเหนือ blocklist อยู่แล้วในสถิติ

> ถ้าผู้ใช้อยากได้ทั้งสองโหมดจาก list เดียวกัน: subscribe URL เดิมซ้ำอีกรายการแล้วตั้งคนละโหมด
> ก็ทำได้ (เปลืองดิสก์/RAM 2 เท่า) — ไม่ต้องเขียนโค้ดพิเศษรองรับ

#### 2.1.3 ต้นทุนของโหมด nxdomain (ประเมินแล้ว — ตอบข้อกังวลของแผนเดิม)

แผนเดิมปฏิเสธ `address=/domain/` โดยอ้างว่า "93,515 โดเมน = ~187k directive → conf บวมเป็นสิบ MB"
**ตัวเลขนั้นมาจากการ render แบบ deny-list sinkhole (2 บรรทัด/โดเมน: `0.0.0.0` + `::`) และการยัดลงใน
`pigate-dns.conf` โดยตรง** ไม่ได้มาจากตัวกลไก `address=` เอง เมื่อ render เป็น NXDOMAIN
(ไม่มี IP เลย = 1 บรรทัด/โดเมน) ในไฟล์แยก ตัวเลขจริงเป็นดังนี้:

| ประเด็น | `sinkhole` (`addn-hosts`) | `nxdomain` (`conf-file` + `address=/d/`) |
|---|---|---|
| ไบต์/โดเมน | `0.0.0.0 ` + name + `\n` ≈ 31 B | `address=/` + name + `/\n` ≈ 33 B |
| ขนาดไฟล์ที่ 93k | ≈ 2.9 MB | ≈ 3.1 MB (**ต่างกัน <10% ไม่ใช่ "สิบ MB"**) |
| `pigate-dns.conf` เอง | +1 บรรทัด | +1 บรรทัด (ไฟล์หลักไม่บวมทั้งสองแบบ) |
| dnsmasq parse ตอนอ่าน config | **ไม่ parse ตอน `--test`** (hosts ถูกอ่านตอน start เท่านั้น) | **parse ทั้งไฟล์ 2 รอบต่อ Apply** (`--test` 1 + start 1) |
| lookup ต่อ query | hash table ของ cache | O(log₂ n) ตั้งแต่ 2.86 (~17 ขั้นที่ 93k) |
| RAM ของ dnsmasq (ประมาณการ) | ~10-20 MB ที่ 93k | ~10-25 MB ที่ 93k (2.86 ระบุว่า "reduces memory foot-print") |
| ความเสี่ยงตอนไฟล์หาย | `--test` **จับไม่ได้** (ข้อควรระวัง 1) | **`--test` จับได้** เพราะ `conf-file` ถูกอ่านตอน parse option |

**ข้อสรุป:** โหมด nxdomain แพงกว่าจริงแต่ "แพงกว่า" อยู่ที่ **เวลา parse ตอน Apply** เป็นหลัก
ไม่ใช่ขนาดไฟล์หรือ RAM อย่างที่แผนเดิมเข้าใจ → ยอมรับได้ แต่ต้อง (ก) ตั้งเพดานแยกให้ต่ำกว่า
โหมด sinkhole (§2.1.4) และ (ข) **วัดเวลา Apply จริงบนบอร์ดแล้วบันทึกตัวเลขลงเอกสาร** (Final Acceptance)
ตัวเลขทั้งหมดข้างบนเป็น *ประมาณการจากเอกสาร* ยังไม่ได้วัด — ห้ามอ้างเป็นข้อเท็จจริงจนกว่าจะวัด

#### 2.1.4 ค่า default และเพดานแยกต่อโหมด

- **default ของ list ใหม่ = `sinkhole`** (ต่างจาก deny-list เดิมที่ default = `nxdomain`)
  เหตุผล: ที่สเกล 93k+ โหมด sinkhole ถูกกว่ามากและ "ไม่ครอบ subdomain" คือพฤติกรรมที่ตรงกับ
  เจตนาของไฟล์ blocklist สาธารณะ (ซึ่งลิสต์ชื่อเต็มมาให้ครบอยู่แล้ว) — ต้อง**เขียน doc comment
  กำกับความต่างนี้ไว้ทั้งใน model และในหน้า UI** ไม่งั้นจะสับสนกับ deny-list
- `DNSBlocklistMaxTotalDomains = 500000` (รวมทุก list ทุกโหมด) — คงเดิม
- **`DNSBlocklistMaxNXDomainDomains = 150000` (ใหม่)** — เพดานรวมเฉพาะ list ที่ `blockMode=nxdomain`
  เพราะโดเมนกลุ่มนี้ต้องถูก parse 2 รอบทุกครั้งที่ Apply ตัวเลข 150k เลือกให้พอกับ
  "StevenBlack unified (93k) + list ย่อยอีกสักตัว" และ **ต้อง re-tune หลังวัดจริงบนบอร์ด**
  (บันทึกผลใน T-13) — เขียนเหตุผลนี้เป็น doc comment ข้างค่าคงที่

#### 2.1.5 ข้อกำหนดเวอร์ชัน dnsmasq

โหมด `nxdomain` ที่สเกลนี้พึ่ง "domain search rewrite" ของ **dnsmasq ≥ 2.86**
(ก่อนหน้านั้น lookup โตเร็วกว่าเชิงเส้น = DNS ทั้งบ้านช้าลงจริง)

| ระบบ | เวอร์ชัน dnsmasq | โหมด nxdomain ที่ 93k |
|---|---|---|
| Raspberry Pi OS / Debian bookworm (เป้าหมายหลัก, Pi 5 รองรับแค่ bookworm ขึ้นไป) | 2.89 | ผ่าน |
| Debian trixie | 2.90+ | ผ่าน |
| Debian bullseye (บอร์ดเก่า) | 2.85 | **ไม่ผ่าน** |

→ kernel layer ตรวจเวอร์ชันครั้งเดียวตอนเรียกใช้ครั้งแรกด้วย `dnsmasq --version` (arg คงที่
ไม่มี user input — pattern เดียวกับ `--test` ที่มีอยู่แล้ว ไม่ถือว่าละเมิดกฎ no-shell-exec)
แล้ว service ปฏิเสธการ**ตั้ง**โหมด nxdomain พร้อมข้อความที่อ่านรู้เรื่องถ้าเวอร์ชัน < 2.86
**parse ไม่ได้/ไม่รู้เวอร์ชัน = ถือว่ารองรับ** (fail-open เฉพาะกรณีนี้ เพราะการ fail-closed
จะทำให้บอร์ดที่ปกติดีใช้ฟีเจอร์ไม่ได้ และผลเสียสูงสุดคือ "ช้า" ไม่ใช่ "ไม่ปลอดภัย")

### 2.2 ห้ามส่งไฟล์ต้นฉบับให้ dnsmasq (security requirement)

ไฟล์ hosts สาธารณะ map ชื่อ → IP อะไรก็ได้ ถ้าชี้ `addn-hosts` ไปที่ไฟล์ดิบ คนที่คุมไฟล์ต้นทาง
(หรือ MITM หรือคนอัปโหลด) จะชี้โดเมนใดก็ได้ไปยัง IP ของตัวเองได้ = DNS spoofing เต็มรูปแบบ
→ parser **ทิ้งคอลัมน์ IP ต้นฉบับทั้งหมด** แล้ว render ใหม่เป็น `0.0.0.0 <domain>` ของเราเสมอ
(โหมด `nxdomain` ยิ่งเข้มกว่า เพราะ `address=/<domain>/` ไม่มีช่องให้ใส่ IP เลย —
แต่ **ห้าม** ใช้ข้อนี้เป็นข้ออ้างลดความเข้มของ parser: parser ตัวเดียวกันป้อนทั้งสองโหมด)

ไฟล์ที่ generate (`/var/lib/pigate/blocklists/<id>.hosts` และ `<id>.conf`) เขียนแบบ atomic:
`os.CreateTemp` ในไดเรกทอรีเดียวกัน → `Write` → `Sync` → `Chmod(0644)` → `os.Rename`

### 2.3 Metadata = JSON manifest ไฟล์เดียว (**ไม่ใช้ SQLite เลย — ตามที่ owner เลือก (R1)**)

**ตำแหน่ง:** `/var/lib/pigate/blocklists/manifest.json` (ไดเรกทอรีเดียวกับไฟล์ `.hosts`/`.conf`)

**Schema (schemaVersion = 1):**
```json
{
  "schemaVersion": 1,
  "updatedAt": "2026-08-22T10:00:00Z",
  "lists": [
    {
      "id": "bl-3f9a2c",
      "name": "StevenBlack unified",
      "sourceType": "url",
      "url": "https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts",
      "blockMode": "sinkhole",
      "enabled": true,
      "domainCount": 93412,
      "fileBytes": 1902344,
      "sha256": "9f86d081…",
      "lastFetchedAt": "2026-08-22T10:00:00Z",
      "lastError": "",
      "createdAt": "2026-08-20T08:11:00Z"
    }
  ]
}
```
- `sha256` คือ hash ของไฟล์ `.hosts` ที่เรา render แล้ว (ไม่ใช่ของไฟล์ต้นฉบับ และ**ไม่ใช่ของ `.conf`
  ซึ่งเป็นไฟล์ derived**) ใช้เทียบว่าเนื้อหาเปลี่ยนไหม
- `id` ต้องตรง `^bl-[a-z0-9]{1,32}$` และเป็นตัวเดียวกับชื่อไฟล์ `<id>.hosts` / `<id>.conf`
- **`blockMode`** (ใหม่ รอบที่ 3): `"sinkhole"` | `"nxdomain"` — ใช้ค่าคงที่
  `model.DNSBlockModeSinkhole` / `model.DNSBlockModeNXDomain` **ตัวเดียวกับ deny-list เดิม
  ห้ามสร้างค่าคงที่ชุดใหม่**; ค่าว่าง `""` ที่อ่านจาก manifest ให้ normalize เป็น `sinkhole`
  (**ไม่ใช่** `nxdomain` แบบ deny-list — ดู §2.1.4) และเขียนกลับเป็นค่าเต็มเสมอตอน persist
  ครั้งถัดไป; **ไม่ต้อง bump `schemaVersion`** เพราะ v1 ยังไม่เคยถูก ship ออกไปจริง

**กฎการเขียน/อ่าน (ต้องทำครบทุกข้อ):**
1. **Locking:** `sync.RWMutex` ตัวเดียวใน `blocklistStore` ครอบ read-modify-write ทั้งก้อน
   (โหลด → แก้ → เขียน) ไม่ใช่แค่ตอนเขียน — ไม่งั้น refresh สองคำขอพร้อมกันจะเขียนทับกัน
   *ไม่ใช้ flock:* มี pigate process เดียวที่เขียนไดเรกทอรีนี้ (SQLite ของโปรเจกต์ก็ตั้งสมมติฐาน
   single-writer เดียวกันอยู่แล้ว) — จดเหตุผลนี้เป็น doc comment ไว้ในไฟล์ store
2. **Atomic write:** marshal (indent 2 spaces, deterministic) → temp file ในไดเรกทอรีเดียวกัน →
   `Sync` → `Rename` — pattern เดียวกับไฟล์ `.hosts`/`.conf` ห้ามเขียนทับไฟล์เดิมตรง ๆ
3. **Versioning:** ไฟล์หาย = manifest ว่าง `schemaVersion:1` (ไม่ใช่ error);
   `schemaVersion` > ที่โค้ดรู้จัก = **fail closed** (ปฏิเสธการเขียน, คืน error ให้ UI แสดง,
   ห้ามเขียนทับไฟล์ของเวอร์ชันใหม่กว่า); JSON พังอ่านไม่ออก = rename ไฟล์เสียเป็น
   `manifest.json.corrupt-<ts>` แล้วเริ่ม manifest ใหม่ + log error (กันฟีเจอร์ตายถาวร)
4. **Cache in RAM:** โหลดครั้งเดียวตอนสร้าง service แล้วเก็บใน struct — การอ่านทุกครั้ง (GET list)
   อ่านจาก RAM ไม่แตะดิสก์ (ลด SD wear ตามหลักของโปรเจกต์)
5. **Layering:** ตัว I/O จริง (อ่าน/เขียน bytes ของ manifest และไฟล์ `.hosts`/`.conf`) อยู่ใน
   **kernel layer** ทั้งหมด (real + mock) — service ทำแค่ marshal/unmarshal + mutex + business rule
   ทำแบบนี้ `-mock=true` จะไม่แตะ filesystem จริงเลยโดยอัตโนมัติ (ข้อบังคับของโปรเจกต์)

### 2.4 Backup/Restore ต้องพก manifest + ไฟล์อัปโหลดไปด้วย

`service/backup.go` export เป็น **JSON ไฟล์เดียว** (`model.BackupFile`) ไม่ใช่ archive และ
`Meta.Checksum` คำนวณจาก marshalled `Config` → **ฟิลด์ใหม่ทุกตัวต้อง `omitempty`**
ไม่งั้น backup เก่าจะ re-marshal ได้ bytes ไม่เท่าเดิมและ checksum พัง (มี comment เตือนไว้แล้วที่
`model/backup.go:87-92`)

```go
// model/backup.go — BackupConfig เพิ่ม 2 ฟิลด์ (omitempty ทั้งคู่)
Blocklists     []DNSBlocklist            `json:"blocklists,omitempty"`     // = lists[] จาก manifest (มี blockMode ในตัว)
BlocklistFiles []DNSBlocklistFilePayload `json:"blocklistFiles,omitempty"` // เนื้อไฟล์ .hosts เท่านั้น
// DNSBlocklistFilePayload{ ID, Sha256, GzipBase64 string }
```

**กฎว่าไฟล์ไหนถูกใส่เข้า backup:**
- **พกเฉพาะ `.hosts`** — `.conf` ของโหมด nxdomain เป็นไฟล์ derived สร้างใหม่ได้จาก `.hosts`
  ตอน import (อย่าพกไปทั้งคู่ = backup บวมเปล่า ๆ 2 เท่า)
- `sourceType == "upload"` → **ใส่เสมอ** (ไฟล์นี้ re-fetch ไม่ได้ ถ้าไม่พกไปด้วยข้อมูลหายถาวร)
- `sourceType == "url"` → **ไม่ใส่** (กด Refresh เอาคืนได้ ไม่ต้องให้ backup บวม)
  ยกเว้นผู้ใช้ส่ง `?includeBlocklistFiles=1` ตอน export (ค่า default = ปิด)
- บีบอัด gzip แล้ว base64 (ไฟล์ 93k โดเมน ~2.9 MB → gzip ~500 KB → base64 ~700 KB)
- **cap รวม 8 MB**: ถ้าเกิน ให้ตัด payload ที่เหลือออกแล้วใส่ warning ใน `ImportResult`/export log
  (import handler จำกัด body ที่ 10 MB — `api/handlers.go:3378-3383`)
- Import: verify `sha256` ของไฟล์ที่ decode ได้ก่อนเขียนลงดิสก์เสมอ; list ที่ไม่มี payload ให้ตั้ง
  `domainCount=0`, `lastError="needs refresh after import"` → `ApplyZones` จะข้ามไปเอง (stat check)
  จนกว่าผู้ใช้จะกด Refresh

### 2.5 SSRF hardening ของ subscribe URL (SENSITIVE)

ต้นแบบ: `service/ipinfo.go` (client เฉพาะตัว, refuse redirect, `io.LimitReader`) + เพิ่ม:

| กลไก | รายละเอียด |
|---|---|
| scheme | **`https` เท่านั้น** (ยืนยันแล้ว R3) — `http://` ถูก reject ตั้งแต่ validate |
| port | อนุญาตเฉพาะไม่ระบุ port หรือ `:443` |
| **IP guard ที่ dialer** | `net.Dialer.Control` ตรวจ IP ปลายทางจริงที่กำลังจะเชื่อม → ไม่ผ่าน `isGloballyRoutable` (`ipinfo.go:57`) = error ปิด DNS-rebinding/TOCTOU และครอบทุก redirect hop อัตโนมัติ (R4: **ไม่อนุญาต URL ใน LAN/localhost โดยตั้งใจ**) |
| redirect | ไม่เกิน 3 hop และ re-validate scheme/port ทุก hop |
| ขนาด | `io.LimitReader(body, 16 MB+1)` เกิน = error ไม่ truncate |
| compression | `Transport.DisableCompression = true` กัน gzip bomb ที่พองเกิน limit |
| timeout | ctx 60s + `Client.Timeout` + `TLSHandshakeTimeout`/`ResponseHeaderTimeout` |
| header/proxy | ส่งแค่ `User-Agent: PiGate`, ไม่มี cookie/auth, `Transport.Proxy = nil` |
| logging | log แค่ host + status code ห้าม log URL เต็ม |

### 2.6 สถิติ: index ใหม่แยกต่างหาก `dnsBlocklistIndex` (ตอบคำถาม R2 ของ user)

**ตัดสินใจ: แยก index ใหม่ ไม่ merge เข้า `dnsBlockIndex` เดิม** เหตุผล:
1. **semantic ต่างกัน และตอนนี้ต่างกัน "ต่อ list" ด้วย** — deny-list = suffix-match ไต่ parent เสมอ,
   blocklist = **ขึ้นกับ `blockMode` ของ list นั้น**: โหมด `sinkhole` (`addn-hosts`) = **exact-match
   เท่านั้น**, โหมด `nxdomain` (`address=/d/`) = **suffix-match ไต่ parent** ถ้ายัดรวมกันใน map เดียว
   จะแยกสองพฤติกรรมนี้ไม่ได้ → **สถิติจะรายงานว่าบล็อกทั้งที่ dnsmasq ไม่ได้บล็อก** (โกหก)
2. **โครงสร้างข้อมูลต่างกันเพราะขนาดต่างกัน 500 เท่า** — deny-list ≤1000 แถวเก็บเป็น
   `map[string]string` ได้สบาย; blocklist ถึง 500k โดเมนต้องเก็บแบบประหยัด RAM สุดขีด (ดูด้านล่าง)
   และ **mode เก็บที่ระดับ list ไม่ใช่ระดับโดเมน** จึงไม่ต้องเก็บ value ต่อโดเมนเลย
3. lifecycle ต่างกัน (deny-list มาจาก DB, blocklist มาจากไฟล์ที่ apply แล้ว)

**โครงสร้างที่เลือก — sorted `[]uint64` ของ FNV-1a 64-bit + binary search:**

```go
// backend/internal/service/dns_blocklist_index.go (ไฟล์ใหม่)
type blocklistSnapshot struct {
    lists []blocklistEntry // ต่อ list หนึ่งชุด เพื่อบอกได้ว่าโดน list ไหน
}
type blocklistEntry struct {
    id, name string
    mode     string   // model.DNSBlockModeSinkhole | model.DNSBlockModeNXDomain
    hashes   []uint64 // sorted, 8 ไบต์ต่อโดเมน
}
type dnsBlocklistIndex struct{ snap atomic.Pointer[blocklistSnapshot] }
// คืน mode ของ list ที่แมตช์ ไม่ใช่ค่าคงที่ตายตัวอีกต่อไป
func (idx *dnsBlocklistIndex) Match(domain string) (listName, mode string, ok bool)
```

**อัลกอริทึมของ `Match` (สำคัญ — ต้องเลียนแบบ dnsmasq ให้ตรง):**
1. ระดับที่ 0 = ชื่อที่ถูก query เอง: ค้นทุก list (ทั้ง sinkhole และ nxdomain)
2. ระดับถัดไป = ตัด label ซ้ายทีละชั้น (สูงสุด 16 ชั้น, ใช้ `strings.IndexByte` ห้าม `strings.Split`,
   ห้าม `strings.HasSuffix` ดิบ — label boundary เท่านั้น): ค้น **เฉพาะ list ที่ `mode == nxdomain`**
3. คืนผลของระดับที่ตื้นที่สุดที่แมตช์ (= most-specific wins ตรงกับกฎ "More specific domains take
   precedence" ของ dnsmasq) ถ้าระดับเดียวกันแมตช์หลาย list ให้คืน list แรกตามลำดับใน manifest
4. ถ้าไม่มี list โหมด nxdomain เลย ให้ข้ามการไต่ parent ทั้งหมด (fast path — เคสปกติของผู้ใช้ส่วนใหญ่)

ต้นทุนกรณีแย่สุด: 16 ชั้น × 8 list × binary search ~19 ครั้ง ≈ 2,400 การเปรียบเทียบต่อ query event
(โดเมนจริงมี 3-4 label ไม่ใช่ 16 และ list ส่วนใหญ่เป็น sinkhole ที่ค้นชั้นเดียว) — ไม่มี allocation

**RAM (ประเด็นที่ user ให้ประหยัดที่สุด):**

| ทางเลือก | ต่อโดเมน | 93k โดเมน | 500k โดเมน |
|---|---|---|---|
| `map[string]string` (แบบ dnsBlockIndex เดิม) | ~55-65 B | ~6 MB | **~30 MB** |
| `map[string]struct{}` | ~45-50 B | ~4.5 MB | ~24 MB |
| **sorted `[]uint64` + binary search (เลือกอันนี้)** | **8 B** | **~0.75 MB** | **~4 MB** |

- การเพิ่ม `mode` ไม่กระทบตัวเลขข้างบนเลย เพราะเก็บที่ระดับ list (8 ตัว) ไม่ใช่ระดับโดเมน
- lookup = O(log n) ≈ 19 การเปรียบเทียบ ไม่มี allocation ต่อ query
- ไม่เก็บ string เลย → GC ไม่ต้องไล่ pointer 500k ตัว (ลด GC pressure ด้วย ไม่ใช่แค่ RAM)
- **โอกาสชนของ hash 64-bit ที่ 500k รายการ ≈ 7e-9** และถ้าชนจริง ผลคือ *สถิติ* รายงานว่าโดนบล็อก
  ทั้งที่ไม่โดนเท่านั้น **ไม่มีผลต่อการ resolve DNS จริง** (dnsmasq เป็นคนบล็อก ไม่ใช่ index นี้) —
  ต้องเขียน doc comment ระบุข้อนี้ไว้ชัดเจน
- สร้าง snapshot แบบ streaming (kernel ส่งโดเมนทีละบรรทัดผ่าน callback) → peak RAM = slice เท่านั้น
  ไม่เคยถือ `[]string` 500k ตัวพร้อมกัน; สร้างเสร็จ `sort.Slice` แล้ว swap ด้วย `atomic.Pointer`
- **stream จาก `<id>.hosts` เสมอทุกโหมด** (ไฟล์ canonical ตาม §2.1.1) — ห้ามเขียน parser ตัวที่สอง
  สำหรับ `.conf`

**การป้อนข้อมูล (ตามแบบ sink เดิม):** `DNSServerService.ApplyAll` เพิ่ม sink ตัวที่สอง
`SetBlocklistSink(func([]model.DNSBlocklist))` เรียก **หลัง `ApplyZones` สำเร็จเท่านั้น**
(เหตุผลเดียวกับ `SetBlockedDomainsSink` — สิ่งที่ยังไม่ถูก apply ต้องไม่ถูกนับว่า "กำลังบังคับใช้อยู่")
→ `StatisticsService.SetBlocklists` → อ่านไฟล์ผ่าน kernel แบบ streaming → สร้าง snapshot ใหม่

**จุด classify:** `service/dns_query_stats.go:208-212` เช็ค `blockIndex` (deny-list) ก่อนตามเดิม
ถ้าไม่แมตช์จึงเช็ค `blocklistIndex` โดยตั้ง `blockedRule = <ชื่อ list>` และ
**`blockedMode = mode ที่ index คืนมา`** (ไม่ hardcode sinkhole อีกต่อไป) → หน้า Statistics > DNS >
Blocked Query แสดงได้ทันทีโดยไม่ต้องแก้ model/API/frontend เลย (คอลัมน์ rule/mode มีอยู่แล้ว
และรับค่า `"nxdomain"`/`"sinkhole"` อยู่แล้วที่ `dnsStatisticsService.ts:85,297`)

### 2.7 Apply/reload flow

fetch/upload → parse → เขียนไฟล์ `<id>.hosts` → ถ้า `blockMode==nxdomain` เขียน `<id>.conf` ต่อ
(ถ้าเป็น sinkhole ให้ **ลบ** `<id>.conf` ที่อาจค้างอยู่จากโหมดเดิม) → อัปเดต manifest →
**ยังไม่ restart dnsmasq** → ผู้ใช้กด **Apply DNS** → `ApplyAll()` ส่ง `[]BlocklistRef{id, mode}`
ของ list ที่ enabled เข้า `ApplyZones` → kernel `os.Stat` ไฟล์ที่ตรงกับโหมดของแต่ละ list →
emit `addn-hosts=` หรือ `conf-file=` เฉพาะไฟล์ที่มีจริง → `dnsmasq --test` → restart dnsmasq →
sink ป้อน index สถิติ (พร้อม mode)

**สลับโหมดอย่างเดียว (ไม่ fetch ใหม่):** re-render จาก `<id>.hosts` ที่มีอยู่ → เขียน/ลบ `<id>.conf`
→ อัปเดต manifest → `setIsApplied(false)` ที่ UI → ผู้ใช้กด Apply เอง (ห้าม restart dnsmasq เอง)

---

## 3. Task List ฉบับสมบูรณ์ (สำหรับ ai-developer — ทำเรียงตาม depends_on)

```json
[
  {
    "task_id": "T-01",
    "title": "model: ค่าคงที่ + types + parser ไฟล์ hosts + validator + manifest schema + renderer ทั้งสองโหมด",
    "layer": "model",
    "files": ["backend/internal/model/dns_blocklist.go", "backend/internal/model/dns_blocklist_test.go", "backend/internal/model/dns_validate.go"],
    "instruction": "สร้างไฟล์ใหม่ backend/internal/model/dns_blocklist.go (pure Go ไม่มี I/O นอกจากรับ io.Reader):\n1) type DNSBlocklist {ID, Name, SourceType, URL string; BlockMode string `json:\"blockMode,omitempty\"`; Enabled bool; DomainCount int; FileBytes int64; Sha256, LastFetchedAt, LastError, CreatedAt string} json tag camelCase ตามสไตล์ model/dns_server.go:81-97 + DNSBlocklistInput{Name, URL, BlockMode string; Enabled bool}\n2) type BlocklistManifest {SchemaVersion int `json:\"schemaVersion\"`; UpdatedAt string `json:\"updatedAt\"`; Lists []DNSBlocklist `json:\"lists\"`} + const BlocklistManifestSchemaVersion = 1 + ValidateBlocklistManifest(m) error (schemaVersion ต้อง >0, id ไม่ซ้ำ, ทุก field ผ่าน validator ของตัวเอง)\n3) const พร้อม doc comment อธิบายเหตุผลของตัวเลขทุกตัว: DNSBlocklistSourceURL=\"url\", DNSBlocklistSourceUpload=\"upload\", DNSBlocklistsMax=8, DNSBlocklistMaxFileBytes=16<<20, DNSBlocklistMaxDomainsPerList=300000, DNSBlocklistMaxTotalDomains=500000, **DNSBlocklistMaxNXDomainDomains=150000** (เพดานรวมเฉพาะ list โหมด nxdomain — doc comment ต้องอธิบายว่าโดเมนกลุ่มนี้ถูก dnsmasq parse 2 รอบต่อ Apply (`--test` + start) ต่างจาก addn-hosts ที่ไม่ถูก parse ตอน --test เลย ดูแผน §2.1.3/§2.1.4 และตัวเลขนี้ต้อง re-tune หลังวัดจริงบนบอร์ด), DNSBlocklistNameMax=64, DNSBlocklistMaxLineBytes=512, DNSBlocklistSinkholeIP=\"0.0.0.0\", DNSBlocklistFetchTimeout=60*time.Second, DNSBlocklistDefaultBlockMode=DNSBlockModeSinkhole\n4) ValidateDNSBlocklistName(name), ValidateDNSBlocklistID(id) (^bl-[a-z0-9]{1,32}$), ValidateDNSBlocklistURL(rawURL): url.Parse, scheme ต้องเป็น https เท่านั้น (owner ยืนยัน https-only), host ไม่ว่าง, u.User != nil = reject, port ต้องว่างหรือ 443, len<=2048, ห้ามมี \\n \\r\n4b) **NormalizeBlocklistBlockMode(mode string) (string, error)**: \"\" -> DNSBlocklistDefaultBlockMode (= sinkhole), \"sinkhole\"/\"nxdomain\" -> คืนตามนั้น, อื่น ๆ = error. **ต้อง reuse ค่าคงที่ model.DNSBlockModeSinkhole/DNSBlockModeNXDomain ที่มีอยู่แล้วใน dns_validate.go:342-344 ห้ามประกาศค่าคงที่โหมดชุดใหม่** และเขียน doc comment เตือนชัด ๆ ว่า default ของ blocklist = sinkhole ซึ่ง **ตรงข้ามกับ ValidateBlockedDomain ของ deny-list ที่ default = nxdomain** พร้อมเหตุผลตามแผน §2.1.4 (ที่สเกล 93k+ sinkhole ถูกกว่ามาก และ blocklist สาธารณะลิสต์ชื่อเต็มมาให้ครบอยู่แล้วจึงไม่ต้องการ subdomain coverage)\n5) ValidateBlocklistDomain(domain string) error — refactor: ย้าย logic ตรวจชื่อโดเมนออกมาจาก ValidateBlockedDomain (model/dns_validate.go:360-383) มาเป็นฟังก์ชันนี้ แล้วให้ ValidateBlockedDomain เรียกใช้ต่อ เพื่อไม่ให้ logic แตกเป็นสองชุด\n6) ParseHostsBlocklist(r io.Reader, exclude map[string]bool) ([]string, BlocklistParseStat, error): bufio.Scanner buffer <= DNSBlocklistMaxLineBytes (บรรทัดยาวเกิน = ข้าม+นับ ไม่ใช่ error); ตัดทุกอย่างหลัง '#'; strings.Fields — ถ้า field แรก parse เป็น IP ได้ ให้ถือว่า field ที่เหลือ (<=16) คือชื่อโฮสต์ และ **ทิ้งค่า IP ต้นฉบับเสมอ** (ข้อบังคับด้านความปลอดภัย §2.2 — ใช้กับทั้งสองโหมด), ถ้า field เดียวและไม่ใช่ IP = domain-only list; ทิ้งชื่อ built-in: localhost, localhost.localdomain, local, localhost4, localhost6, ip6-localhost, ip6-loopback, ip6-localnet, ip6-mcastprefix, ip6-allnodes, ip6-allrouters, broadcasthost, 0.0.0.0; lower-case + ตัดจุดท้าย + ผ่าน ValidateBlocklistDomain; ทิ้งชื่อที่ตรงหรือเป็น subdomain ของคีย์ใน exclude (เทียบแบบ label boundary เหมือน dnsBlockIndex.Match ที่ service/dns_block_index.go:95-122 ห้ามใช้ strings.HasSuffix ดิบ); dedupe ด้วย map; เกิน DNSBlocklistMaxDomainsPerList = error; BlocklistParseStat{TotalLines, Accepted, SkippedComment, SkippedInvalid, SkippedExcluded, Duplicates}\n7) RenderHostsFile(id string, domains []string, generatedAt time.Time) []byte — header comment + '0.0.0.0 <domain>' บรรทัดละตัว, sort.Strings ให้ deterministic (unit test เทียบ byte ได้)\n7b) **RenderBlocklistConfFile(id string, domains []string, generatedAt time.Time) []byte** (ใหม่ รอบที่ 3) — ไฟล์ dnsmasq conf สำหรับโหมด nxdomain: header comment + 'address=/<domain>/' **หนึ่งโดเมนต่อบรรทัดเท่านั้น ห้าม batch หลายโดเมนใน directive เดียว** (แผน §2.1: dnsmasq อ่านบรรทัด config ผ่านบัฟเฟอร์ขนาดจำกัด MAXDNAME และพฤติกรรมเมื่อบรรทัดยาวเกินไม่ได้ระบุในเอกสาร — ห้ามเสี่ยงกับไฟล์ที่ dnsmasq โหลด), sort.Strings, deterministic. doc comment ต้องอ้าง man page ว่า `--address=/example.com/` ที่ไม่มี IP = NXDOMAIN สำหรับโดเมนนั้นและทุก subdomain และอธิบายว่าทำไมใช้ address= ไม่ใช่ server= (dnsmasq 2.86 domain-search rewrite ทำให้ address= lookup เป็น O(log2 n) ส่วนเส้นทาง --server มีปัญหา O(n^2) ตอน start ที่เพิ่งแก้ใน 2.88)\n8) ParseHostsFileDomains(r io.Reader, fn func(string) error) error — อ่านไฟล์ .hosts ที่ *เรา* generate กลับมาแบบ streaming (ใช้ตอนสร้าง index สถิติ T-06 และตอน re-render .conf ตอนสลับโหมด T-05) ห้าม return []string\nUnit test: ไฟล์ตัวอย่างสไตล์ StevenBlack (header comment, 127.0.0.1 localhost, ::1, 255.255.255.255 broadcasthost, 0.0.0.0 domain), input ที่ระบุ IP อื่น (1.2.3.4 bank.example.com) ต้องได้ output เป็น 0.0.0.0 หรือถูกทิ้ง, comment ท้ายบรรทัด, dedupe, exclude subdomain, บรรทัดยาวเกิน, unicode, **RenderBlocklistConfFile ให้ไฟล์ที่มี 1 บรรทัดต่อโดเมนและไม่มี IP โผล่ในไฟล์เลย (grep หา '0.0.0.0' ต้องไม่เจอ)**, **NormalizeBlocklistBlockMode ครบทุกเคสรวม \"\" -> sinkhole**",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/model/... ผ่าน", "test เดิมของ ValidateBlockedDomain ยังผ่านครบ (ไม่ regress) และ default mode ของ deny-list ยังเป็น nxdomain เหมือนเดิม"],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "kernel: ไฟล์ .hosts/.conf + manifest I/O + emit addn-hosts|conf-file + ตรวจเวอร์ชัน dnsmasq (real + mock)",
    "layer": "kernel",
    "files": ["backend/internal/kernel/interfaces.go", "backend/internal/kernel/dns_server.go", "backend/internal/kernel/dns_blocklist.go", "backend/internal/kernel/mock.go", "backend/internal/kernel/dns_server_test.go"],
    "instruction": "SENSITIVE: เขียนไฟล์ที่ dnsmasq จะโหลด (รวมถึงไฟล์ conf ที่ถูก include เข้าไปตรง ๆ) + ประกอบ path จาก id ภายนอก — review เข้มเรื่อง path traversal, การ inject directive และลำดับ write-before-reference\n1) interfaces.go:167-187 เพิ่มใน DNSServerManager พร้อม doc comment:\n   - WriteBlocklistFile(id string, content []byte) error            // <id>.hosts\n   - WriteBlocklistConfFile(id string, content []byte) error        // <id>.conf (โหมด nxdomain)\n   - RemoveBlocklistFile(id string) error                           // ลบทั้ง .hosts และ .conf, ไม่มีไฟล์ = ไม่ error\n   - RemoveBlocklistConfFile(id string) error                       // ลบเฉพาะ .conf (ใช้ตอนสลับกลับเป็น sinkhole)\n   - BlocklistFileInfo(id string) (size int64, exists bool)\n   - BlocklistConfFileInfo(id string) (size int64, exists bool)\n   - StreamBlocklistFile(id string, fn func(line string) error) error  // อ่าน .hosts กลับแบบ streaming สำหรับ index สถิติ/re-render\n   - ReadBlocklistManifest() ([]byte, error)   // ไฟล์หาย = คืน (nil, nil) ไม่ใช่ error\n   - WriteBlocklistManifest(content []byte) error\n   - QuarantineBlocklistManifest() error       // rename ไฟล์เสียเป็น manifest.json.corrupt-<unix>\n   - SupportsBulkNXDomain() bool               // ดูข้อ 7\n   และเปลี่ยน signature ApplyZones(zones, interfaces, upstreamServers, queryLog, blocked, blocklists []model.BlocklistRef) error โดย **BlocklistRef{ID, BlockMode string}** ประกาศไว้ใน model (ไม่ใช่ []string อย่างแผนรอบก่อน — kernel ต้องรู้โหมดถึงจะเลือก directive ถูก)\n2) ไฟล์ใหม่ kernel/dns_blocklist.go (//go:build linux) implement บน RealDNSServerManager:\n   - const blocklistDir=\"/var/lib/pigate/blocklists\", manifest = blocklistDir+\"/manifest.json\"; MkdirAll(dir,0755) ก่อนทุกการเขียน\n   - **validateBlocklistID(id)**: ^bl-[a-z0-9]{1,32}$ เท่านั้น ไม่ผ่าน = error ทันที ห้ามใช้ filepath.Clean แทน (id ถูกใช้ประกอบ path จริงของทั้ง .hosts และ .conf)\n   - atomicWrite(path, content): os.CreateTemp(dir,\".tmp-*\") → Write → Sync → Chmod(0644) → Rename; error กลางทางต้อง os.Remove temp เสมอ — ใช้ helper ตัวเดียวกันทั้ง .hosts, .conf และ manifest\n   - **ห้ามตั้งนามสกุลไฟล์ .conf ไว้ในไดเรกทอรีที่ dnsmasq สแกนอัตโนมัติ** — /var/lib/pigate/blocklists ไม่ใช่ conf-dir ของ dnsmasq (dnsmasq อ่านเฉพาะ /etc/dnsmasq.d) จึงปลอดภัย แต่ให้เขียน doc comment ย้ำไว้ว่าห้ามย้ายไดเรกทอรีนี้ไปอยู่ใต้ /etc/dnsmasq.d เด็ดขาด ไม่งั้นไฟล์ของ list ที่ปิดอยู่จะถูกโหลดเองเงียบ ๆ\n3) dns_server.go: buildDNSConfig รับพารามิเตอร์ใหม่ blocklistDirectives []string (บรรทัดสำเร็จรูปที่ผู้เรียก stat แล้วว่าไฟล์มีจริง เช่น \"addn-hosts=/var/lib/pigate/blocklists/bl-x.hosts\" หรือ \"conf-file=/var/lib/pigate/blocklists/bl-y.conf\") แล้ว emit ท้ายสุดต่อจาก deny-list (dns_server.go:331-337) ใต้หัวข้อ '# Blocklists (bulk import)' โดย **filter ก่อนแล้วค่อยเช็ค len()** ตาม pattern blockedLines เดิม เพื่อให้ output เมื่อไม่มี blocklist byte-for-byte เท่าเดิม\n4) ApplyZones: หลัง ensureDnsmasqBaseConfig ให้ loop blocklists []model.BlocklistRef → validateBlocklistID → normalize mode → เลือกไฟล์ตามโหมด (sinkhole=.hosts, nxdomain=.conf) → os.Stat → เก็บเฉพาะไฟล์ที่มีจริงและ size>0; ตัวที่หาย log.Printf warning แล้วข้าม (ห้าม emit directive ชี้ไฟล์ที่ไม่มี — ข้อควรระวัง 1 และ 16: conf-file ที่หาย = dnsmasq **ไม่ start** แน่นอน ต่างจาก addn-hosts ที่ยังไม่ยืนยัน)\n5) mock.go:405-427 — MockDNSServerManager: เพิ่ม blocklists map[string][]byte + blocklistConfs map[string][]byte + manifest []byte ใน struct (**in-memory ล้วน ห้ามแตะ filesystem จริงเด็ดขาด**), implement ทุกเมธอดใหม่, SupportsBulkNXDomain() คืน true เสมอ, แก้ signature ApplyZones\n6) dns_server_test.go: เพิ่มเคส list โหมด sinkhole ได้ addn-hosts=, โหมด nxdomain ได้ conf-file=, ปนกันสองโหมดได้ทั้งสองบรรทัดตามลำดับ, path ที่ไม่มีไฟล์ไม่ถูก emit, output ตอนไม่มี blocklist เท่ากับ golden เดิมทุก byte\n7) **ตรวจเวอร์ชัน dnsmasq (ใหม่ รอบที่ 3)**: SupportsBulkNXDomain() บน RealDNSServerManager รัน `exec.Command(\"dnsmasq\", \"--version\")` (arg คงที่ ไม่มี user input — pattern เดียวกับ validateDnsmasqConfig ที่ dns_server.go:28 จึงไม่ละเมิดกฎ no-shell-exec ของโปรเจกต์) แล้ว regexp จับ 'Dnsmasq version (\\\\d+)\\\\.(\\\\d+)' → true เมื่อ >= 2.86; **แคชผลไว้ด้วย sync.Once** (ห้ามยิงทุกครั้งที่ Apply); parse ไม่ได้/รันไม่ได้ = **คืน true (fail-open)** พร้อม log warning — เหตุผลตามแผน §2.1.5 (ผลเสียสูงสุดคือ 'ช้า' ไม่ใช่ 'ไม่ปลอดภัย' และ fail-closed จะทำให้บอร์ดปกติใช้ฟีเจอร์ไม่ได้)",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/kernel/... ผ่าน", "ไม่มี exec.Command ใหม่นอกจาก dnsmasq --version ที่ arg คงที่", "mock ไม่เขียนไฟล์ลงดิสก์จริงเลย", "golden test ของ config ที่ไม่มี blocklist ยังเท่าเดิมทุก byte"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-03",
    "title": "service: blocklistStore — JSON manifest + locking + versioning (แทนที่ตาราง SQLite)",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist_store.go", "backend/internal/service/dns_blocklist_store_test.go"],
    "instruction": "สร้าง backend/internal/service/dns_blocklist_store.go — ชั้นเก็บ metadata แทน SQLite ทั้งหมด (owner ตัดสินใจไม่ใช้ DB แม้แต่ metadata) ทำตาม §2.3 ของ docs/ref/todo/dns-blocklist-import-plan.md:\n1) type blocklistStore struct { mu sync.RWMutex; manager kernel.DNSServerManager; cache model.BlocklistManifest; loaded bool; readOnly bool }\n2) Load() error — manager.ReadBlocklistManifest(); nil/ว่าง = manifest ว่าง schemaVersion=1 (ไม่ใช่ error); json.Unmarshal พัง = เรียก manager.QuarantineBlocklistManifest() + log error + เริ่ม manifest ใหม่; schemaVersion > model.BlocklistManifestSchemaVersion = ตั้ง readOnly=true และคืน error ที่สื่อความหมาย (fail closed ห้ามเขียนทับไฟล์ของเวอร์ชันใหม่กว่า)\n2b) **ตอน Load ต้อง normalize BlockMode ของทุกรายการด้วย model.NormalizeBlocklistBlockMode** — ค่าว่าง (manifest ที่เขียนโดยรุ่นก่อนมีฟีเจอร์โหมด) กลายเป็น sinkhole; ค่าที่ไม่รู้จักให้ clamp เป็น sinkhole + log warning **ห้าม fail ทั้ง manifest เพราะ list เดียวโหมดเพี้ยน** (pattern เดียวกับ db/backup_repo.go:344-351 ที่ clamp mode ของ deny-list)\n3) เมธอด mutation ทุกตัว **ถือ write lock คลุมทั้ง read-modify-write** ไม่ใช่แค่ตอนเขียน: Add(l), Update(id, mutate func(*model.DNSBlocklist) error), Remove(id), Toggle(id), ReplaceAll(lists) (สำหรับ backup import) — ทุกตัวจบด้วย persistLocked(): ตั้ง UpdatedAt=time.Now().UTC().RFC3339 → json.MarshalIndent (deterministic, เรียง lists ตาม createdAt) → manager.WriteBlocklistManifest(); ถ้า readOnly=true ให้คืน error ทันทีไม่เขียน\n4) การอ่าน (List/Get) อ่านจาก cache ใน RAM ภายใต้ read lock — ห้ามแตะดิสก์ (ลด SD wear) และต้องคืน copy ของ slice ไม่ใช่ตัวอ้างอิงภายใน\n5) ห้ามใช้ flock — เขียน doc comment อธิบายว่า pigate เป็น single writer ตัวเดียวของไดเรกทอรีนี้ (สมมติฐานเดียวกับ SQLite ของโปรเจกต์)\n6) test ด้วย MockDNSServerManager: save/load round-trip (รวม blockMode), ไฟล์หาย, JSON เสีย -> quarantine + เริ่มใหม่, **manifest ที่ไม่มีคีย์ blockMode -> โหลดได้และกลายเป็น sinkhole**, **blockMode ขยะ -> clamp เป็น sinkhole ไม่ทำให้ทั้ง manifest ล้ม**, schemaVersion อนาคต -> readOnly + error, concurrent Add 50 goroutine แล้ว list ครบ (รันด้วย -race)",
    "acceptance": ["go build ./... ผ่าน", "go test -race ./internal/service/... ผ่าน", "ไม่มีการแตะ SQLite ในไฟล์นี้เลย"],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-04",
    "title": "service: HTTP fetcher พร้อม SSRF guard (https-only)",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist_fetch.go", "backend/internal/service/dns_blocklist_fetch_test.go"],
    "instruction": "SENSITIVE (SSRF) — review เข้มเป็นพิเศษ ทำตาม §2.5 ของแผนทุกข้อ ห้ามลดทอน\n1) type blocklistFetcher struct { client *http.Client }\n2) newBlocklistFetcher(): http.Client ของตัวเอง (ห้ามใช้ http.DefaultClient) — Timeout: model.DNSBlocklistFetchTimeout; Transport: &http.Transport{Proxy: nil, DisableCompression: true, TLSHandshakeTimeout: 10s, ResponseHeaderTimeout: 20s, DialContext: (&net.Dialer{Timeout: 10s, Control: blocklistDialControl}).DialContext}; CheckRedirect: <=3 hop และ re-validate ทุก hop ด้วย model.ValidateDNSBlocklistURL\n3) blocklistDialControl(network, address string, _ syscall.RawConn) error — net.SplitHostPort → netip.ParseAddr → ถ้า !isGloballyRoutable(ip) (มีอยู่แล้วที่ service/ipinfo.go:57) คืน error 'blocklist: refusing to connect to non-public address'. **นี่คือด่านหลักกัน SSRF/DNS-rebinding ห้ามลบ ห้ามแทนด้วยการเช็คก่อน resolve อย่างเดียว** และเป็นเหตุผลที่ subscribe จาก URL ใน LAN ทำไม่ได้โดยตั้งใจ\n4) Fetch(ctx, rawURL) ([]byte, error): validate URL ก่อน → GET พร้อม User-Agent 'PiGate' → status ต้อง 200 → io.ReadAll(io.LimitReader(body, model.DNSBlocklistMaxFileBytes+1)) → len เกิน Max = error (ห้าม truncate แล้วใช้ต่อ) → log แค่ host+status ห้าม log URL เต็ม\n5) test: แยก unit test ของ blocklistDialControl เอง (127.0.0.1, 192.168.1.1, 169.254.169.254, ::1, 100.64.0.1, 0.0.0.0 ต้องถูกปฏิเสธ; 1.1.1.1, 2606:4700::1111 ต้องผ่าน) + httptest ผ่าน constructor สำหรับ test ที่ไม่ตั้ง Control + test body เกิน limit = error + redirect เกิน 3 = error + http:// ถูก reject",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/service/... ผ่าน", "มี test ยืนยันว่า private IP ถูกปฏิเสธที่ชั้น dialer จริง"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-05",
    "title": "service: DNSBlocklistService (create/refresh/upload/delete/toggle/สลับโหมด) + ผูกเข้า ApplyAll",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist.go", "backend/internal/service/dns_server.go", "backend/internal/service/dns_blocklist_test.go"],
    "instruction": "1) ไฟล์ใหม่ service/dns_blocklist.go: type DNSBlocklistService struct { store *blocklistStore; manager kernel.DNSServerManager; fetcher *blocklistFetcher; repo *db.Repository /* ใช้อ่าน zones สำหรับ exclude set เท่านั้น ห้ามเขียน */ }\n2) เมธอด: List(), CreateFromURL(name,url,blockMode,enabled), CreateFromUpload(name, raw []byte, blockMode, enabled), Refresh(id), Delete(id), Toggle(id), UpdateInfo(id,name,url,blockMode,enabled)\n   - id สร้างด้วย crypto/rand prefix 'bl-' (pattern เดียวกับ randomID(\"blk-\") ใน api/handlers.go) ต้องผ่าน model.ValidateDNSBlocklistID\n   - **ทุกทางเข้าที่รับ blockMode ต้องผ่าน model.NormalizeBlocklistBlockMode ก่อนเสมอ** และถ้าผลลัพธ์เป็น nxdomain ต้องเช็ค 2 อย่าง: (ก) manager.SupportsBulkNXDomain() ถ้า false = คืน error ที่บอกผู้ใช้ตรง ๆ ว่า 'ต้องใช้ dnsmasq 2.86 ขึ้นไป' (ข) ผลรวมโดเมนของ list โหมด nxdomain ที่ enabled ทั้งหมด (รวมรายการที่กำลังจะตั้งค่านี้) ต้องไม่เกิน model.DNSBlocklistMaxNXDomainDomains\n   - CreateFromURL: เช็ค len(List()) < model.DNSBlocklistsMax → fetch+parse+เขียนไฟล์ **ก่อน** แล้วค่อย store.Add — ถ้า fetch/parse พลาด ไม่เพิ่มลง manifest เลย (ไม่ทิ้งแถวเสีย)\n   - Refresh: เฉพาะ sourceType==url; fetch พลาด = **คงไฟล์เดิมและ domainCount เดิมไว้** แล้วบันทึกแค่ lastError (ห้ามลบไฟล์ที่ใช้งานได้อยู่ทิ้ง)\n   - Delete: manager.RemoveBlocklistFile (ลบทั้ง .hosts และ .conf) ก่อน แล้ว store.Remove\n   - **UpdateInfo ที่เปลี่ยนเฉพาะ blockMode ต้องไม่ fetch ใหม่** — เรียก renderArtifacts(id, newMode) ที่อ่านโดเมนกลับจาก <id>.hosts ผ่าน manager.StreamBlocklistFile + model.ParseHostsFileDomains แบบ streaming แล้ว render/ลบ .conf ตามโหมดใหม่ (แผน §2.1.1/§2.7) — ต้องทำงานได้แม้ไม่มีเน็ตและกับ list แบบ upload\n3) ingest(id, raw []byte, mode string) กลาง: exclude set = ชื่อ zone ที่ enabled (repo.GetDNSZones) + 'pigate.local' + hostname ปัจจุบัน → model.ParseHostsBlocklist → เช็คผลรวมโดเมนของ list ที่ enabled ทั้งหมดไม่เกิน model.DNSBlocklistMaxTotalDomains **และเช็คเพดาน nxdomain แยกตามข้อ 2** → model.RenderHostsFile → manager.WriteBlocklistFile → **ถ้า mode==nxdomain: model.RenderBlocklistConfFile → manager.WriteBlocklistConfFile; ถ้า mode==sinkhole: manager.RemoveBlocklistConfFile (ลบของเก่าที่ค้างจากโหมดเดิม ห้ามปล่อยไฟล์ orphan ไว้)** → sha256 ของ content ของ .hosts (ไม่ใช่ .conf ซึ่งเป็น derived) → store.Update(meta)\n4) service/dns_server.go ApplyAll (บรรทัด 50-104): ดึง list จาก DNSBlocklistService (inject ผ่าน setter SetBlocklistProvider เพื่อไม่เปลี่ยน signature ของ NewDNSServerService — pattern เดียวกับ SetBlockedDomainsSink ที่บรรทัด 45-47), รวบรวม list ที่ Enabled && DomainCount>0 เป็น []model.BlocklistRef{ID, BlockMode} ส่งเข้า manager.ApplyZones(..., refs) และเพิ่ม SetBlocklistSink(fn func([]model.DNSBlocklist)) ที่ถูกเรียก **หลัง ApplyZones สำเร็จเท่านั้น** พร้อม recover() กัน panic เหมือน sink เดิมทุกประการ\n5) test ด้วย MockDNSServerManager + temp DB (สไตล์ service/dns_server_test.go): ingest แล้ว domainCount ถูก, **สร้างแบบไม่ระบุ mode ได้ sinkhole และไม่มีไฟล์ .conf ใน mock**, **สร้างแบบ nxdomain ได้ทั้ง .hosts และ .conf และเนื้อ .conf มี address=/ ครบทุกโดเมน**, **สลับ nxdomain->sinkhole แล้ว .conf หายไปจาก mock โดยไม่มีการเรียก fetcher เลย**, **สลับ sinkhole->nxdomain โดยไม่มีเน็ตแล้วได้ .conf ที่ถูกต้อง**, **เกิน DNSBlocklistMaxNXDomainDomains = error แต่ตั้งเป็น sinkhole แทนได้**, disable แล้ว ApplyAll ไม่ส่ง ref นั้น, delete แล้วไฟล์ทั้งสองใน mock หาย, refresh ที่ fetch fail ไม่ทำให้ไฟล์เดิมหาย, เกิน DNSBlocklistsMax = error",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/service/... ผ่าน", "ApplyAll ที่ไม่มี blocklist เลย ยังให้ config เดิมทุก byte", "สลับโหมดไม่มีการเรียก HTTP fetcher (พิสูจน์ด้วย fetcher ปลอมที่ fail ทุกครั้ง)"],
    "depends_on": ["T-03", "T-04"]
  },
  {
    "task_id": "T-06",
    "title": "service: dnsBlocklistIndex สำหรับหน้า Statistics > DNS (mode-aware, ประหยัด RAM)",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist_index.go", "backend/internal/service/dns_blocklist_index_test.go", "backend/internal/service/statistics.go", "backend/internal/service/dns_query_stats.go"],
    "instruction": "ทำตาม §2.6 ของแผน — **แยก index ใหม่ ห้าม merge เข้า dnsBlockIndex เดิม** (semantic ต่างกันและตอนนี้ต่างกันรายlist ด้วย: deny-list = suffix-match เสมอ, blocklist โหมด sinkhole = exact-match, blocklist โหมด nxdomain = suffix-match)\n1) ไฟล์ใหม่ service/dns_blocklist_index.go:\n   - type blocklistEntry struct { id, name, mode string; hashes []uint64 }  // sorted; mode = model.DNSBlockModeSinkhole|NXDomain\n   - type blocklistSnapshot struct { lists []blocklistEntry; domainCount int; hasNXDomain bool }\n   - type dnsBlocklistIndex struct { snap atomic.Pointer[blocklistSnapshot] }\n   - hash = FNV-1a 64-bit ของ domain ที่ lower-case แล้ว (hash/fnv จาก stdlib) — **เก็บแค่ hash 8 ไบต์ ห้ามเก็บ string** (500k โดเมน = ~4 MB แทน ~30 MB ถ้าใช้ map[string]string); mode เก็บที่ระดับ list ไม่ใช่ระดับโดเมน จึงไม่เพิ่ม RAM ต่อโดเมนเลย\n   - Empty() bool (fast path ก่อน Match ทุกครั้ง), DomainCount() int, **Match(domain string) (listName, mode string, ok bool)**\n   - **อัลกอริทึมของ Match (ต้องตรงกับ dnsmasq)**: ชั้นที่ 0 (ชื่อที่ query มาเอง) ค้นทุก list; ชั้นถัดไปตัด label ซ้ายทีละชั้น (สูงสุด 16, ใช้ strings.IndexByte ห้าม strings.Split ห้าม strings.HasSuffix ดิบ) และค้น **เฉพาะ list ที่ mode==nxdomain**; คืนผลของชั้นที่ตื้นที่สุดที่แมตช์ (most-specific wins ตามกฎ 'More specific domains take precedence' ของ dnsmasq) ชั้นเดียวกันแมตช์หลาย list = คืน list แรกตามลำดับ; ถ้า !snap.hasNXDomain ให้ข้ามการไต่ parent ทั้งหมด (fast path)\n   - ใช้ sort.SearchUint64s (หรือ sort.Search) ต่อ list (<=8 lists) — ไม่ allocate ต่อ query\n   - Set(entries []blocklistEntry) สร้าง snapshot ใหม่แล้ว atomic swap (ไม่ล็อกฝั่งอ่านเลย)\n   - doc comment ต้องระบุ: (ก) ทำไม sinkhole ต้อง exact-match (addn-hosts ไม่ครอบ subdomain) แต่ nxdomain ต้อง suffix-match (address=/d/ ครอบ subdomain ตาม man page) — **ถ้าทำสลับกัน สถิติจะโกหก** (ข) โอกาส hash ชนที่ 500k รายการ ~7e-9 และถ้าชนจะกระทบแค่ตัวเลขสถิติ **ไม่กระทบการ resolve DNS จริง** (ค) ห้าม log domain ออกมา (privacy เหมือน recordDomainQuery)\n2) statistics.go: เพิ่มฟิลด์ blocklistIndex ใน dnsQueryStats (ข้าง blockIndex บรรทัด ~144-150) + สร้างใน NewStatisticsService (~157) + เมธอด SetBlocklists(lists []model.DNSBlocklist) ที่สร้าง snapshot โดย **stream** โดเมนจากไฟล์ <id>.hosts ผ่าน kernel StreamBlocklistFile + model.ParseHostsFileDomains (ห้ามสร้าง []string 500k ตัว; **stream จาก .hosts ทุกโหมด ห้ามเขียน parser ของ .conf ตัวที่สอง**) และเก็บ mode จาก l.BlockMode หลัง normalize — StatisticsService ต้องรับ kernel.DNSServerManager เข้ามา (setter post-construction แบบเดียวกับ SetLogBuffer/SetBlockedStatsLimit ห้ามเปลี่ยน signature constructor)\n3) dns_query_stats.go:208-212: หลังเช็ค blockIndex แล้วไม่แมตช์ ให้เช็ค blocklistIndex ต่อ (นอก s.dns.mu เหมือนเดิม เพื่อไม่ให้เกิด lock-ordering inversion) ถ้าแมตช์ให้ blocked=true, blockedRule=<ชื่อ list>, **blockedMode=<mode ที่ Match คืนมา> (ห้าม hardcode sinkhole)** → หน้า Statistics แสดงได้เลยโดยไม่ต้องแก้ model/API/frontend (frontend รับ 'nxdomain'|'sinkhole' อยู่แล้วที่ dnsStatisticsService.ts:85,297)\n4) test: exact-match ทำงาน, **subdomain ของโดเมนใน list โหมด sinkhole ต้อง NOT ถูกนับว่า blocked**, **subdomain ของโดเมนใน list โหมด nxdomain ต้องถูกนับว่า blocked และคืน mode=nxdomain**, 'notexample.com' ไม่ใช่ subdomain ของ 'example.com' (label boundary), เมื่อ list โหมด sinkhole มี ads.example.com และ list โหมด nxdomain มี example.com แล้ว query ads.example.com ต้องคืน list ของ sinkhole (most-specific/ชั้นตื้นสุดชนะ), deny-list ชนะ blocklist เมื่อแมตช์ทั้งคู่, Set ใหม่ไม่ re-classify ประวัติเดิม, benchmark/วัด RAM ของ 100k โดเมนแล้วบันทึกตัวเลขจริงลง doc comment",
    "acceptance": ["go build ./... ผ่าน", "go test -race ./internal/service/... ผ่าน", "RAM ที่วัดได้จริงของ 100k โดเมนถูกบันทึกไว้ใน doc comment", "หน้า Statistics > DNS > Blocked Query แสดง hit จาก blocklist พร้อมชื่อ list และ mode ที่ตรงกับโหมดของ list นั้น"],
    "depends_on": ["T-05"]
  },
  {
    "task_id": "T-07",
    "title": "main.go: wiring service ใหม่ + sink สถิติ",
    "layer": "service",
    "files": ["backend/cmd/pigate/main.go"],
    "instruction": "1) สร้าง dnsBlocklistService ต่อจาก dnsServerService (main.go:258) แล้วเรียก Load() ของ store ทันที (error ให้ log warning ไม่ล้ม process — ฟีเจอร์นี้ห้ามทำให้บอร์ดบูตไม่ขึ้น)\n2) dnsServerService.SetBlocklistProvider(dnsBlocklistService) และ dnsServerService.SetBlocklistSink(statisticsService.SetBlocklists) — **ต้องอยู่ก่อน dnsServerService.InitApplyConfig() ที่บรรทัด 675** ด้วยเหตุผลเดียวกับ SetBlockedDomainsSink (main.go:329-335) คือให้ index ถูก prime ตั้งแต่บูต ไม่ใช่รอ Apply ครั้งแรก\n3) statisticsService ต้องได้ kernel.DNSServerManager ผ่าน setter เพื่ออ่านไฟล์ blocklist\n4) ส่ง dnsBlocklistService เข้า api.NewServer (main.go:484)\n5) **ไม่ต้อง** เพิ่ม InitApplyConfig ใหม่ — blocklist ถูก apply ผ่าน dnsServerService.InitApplyConfig() เดิมอยู่แล้ว (เขียน comment ระบุไว้)\n6) หมายเหตุใน comment: fetcher ยังทำ HTTPS จริงแม้รัน -mock=true (เป็น outbound request ธรรมดา ไม่ใช่การแตะ OS) แต่ไฟล์/manifest จะไปลง MockDNSServerManager ใน RAM ทั้งหมด และ MockDNSServerManager.SupportsBulkNXDomain() คืน true เสมอ จึงทดสอบโหมด nxdomain บนเครื่อง dev ได้",
    "acceptance": ["go build ./... ผ่าน", "./pigate-backend -mock=true รันขึ้นได้ ไม่ panic แม้ไม่มี manifest.json"],
    "depends_on": ["T-06"]
  },
  {
    "task_id": "T-08",
    "title": "api: handlers + routes สำหรับ blocklists",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go", "backend/internal/api/router.go", "backend/internal/api/middleware.go", "backend/internal/api/server.go"],
    "instruction": "SENSITIVE (input validation + upload):\n1) server.go: เพิ่มฟิลด์ dnsBlocklistService + พารามิเตอร์ใน NewServer\n2) handlers.go (ต่อจากกลุ่ม blocked domains ~4108-4200): HandleGetDNSBlocklists, HandleCreateDNSBlocklist, HandleUpdateDNSBlocklist, HandleDeleteDNSBlocklist, HandleToggleDNSBlocklist, HandleRefreshDNSBlocklist, HandleUploadDNSBlocklist\n2b) **รับฟิลด์ blockMode ในทั้ง create (JSON body), upload (query ?blockMode=) และ update** — validate ด้วย model.NormalizeBlocklistBlockMode **ที่ชั้น handler ด้วย ไม่ใช่พึ่ง service อย่างเดียว** (pattern เดียวกับ handlers.go:4135,4214 ที่ deny-list ทำอยู่); ค่าที่ไม่รู้จัก = 400 พร้อมข้อความบอกค่าที่อนุญาต; ไม่ส่งมา = ปล่อยว่างให้ service ใส่ default (sinkhole)\n3) Upload: รับ raw body (Content-Type text/plain) พร้อม ?name= — **ต้อง** ครอบด้วย http.MaxBytesReader(w, r.Body, model.DNSBlocklistMaxFileBytes) เองในตัว handler ตาม pattern HandleImportConfig (handlers.go:3377-3383) **และเพิ่ม '/api/dns/blocklists/upload' ลง bodyLimitExemptPaths (api/middleware.go:358)** — comment ในนั้นสั่งไว้ชัดว่า endpoint upload ใหญ่ทุกตัวต้องมาลงทะเบียน ถ้าลืมจะถูกตัดที่ 1 MB แบบเงียบ ๆ\n4) router.go เพิ่มกลุ่ม 8.3 ต่อจาก 8.2 (บรรทัด 212-218): GET ใช้ authRoute; **create/upload/refresh/update/delete/toggle ใช้ superAdminRoute แบบ explicit** — เหตุผล: endpoint กลุ่มนี้สั่งให้บอร์ดยิง HTTP ออกไปยัง URL ที่ผู้ใช้ระบุและเขียนไฟล์หลาย MB ลงดิสก์ จึงทำให้ชัดในโค้ดแบบเดียวกับ reboot/config-export แทนที่จะพึ่ง RoleReadOnlyMiddleware เพียงอย่างเดียว\n5) ทุก handler validate ก่อนเสมอ (model.ValidateDNSBlocklist*) และ **ห้ามส่ง error ดิบจาก fetcher กลับไปทั้งก้อน** — สรุปข้อความเอง ไม่ให้รั่ว internal path/URL; แต่ error 'ต้องใช้ dnsmasq 2.86 ขึ้นไป' และ 'เกินเพดานโดเมนของโหมด NXDOMAIN' ต้องส่งกลับให้ผู้ใช้อ่านรู้เรื่อง (ไม่ใช่ 500 เปล่า ๆ)",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/api/... ผ่าน", "-disable-edit=true บล็อก mutation ของเส้นใหม่ทั้งหมด", "role admin (ไม่ใช่ super_admin) เรียก GET ได้ แต่ POST/PUT/DELETE ไม่ได้", "blockMode ที่ไม่รู้จัก = 400 ไม่ใช่ 500"],
    "depends_on": ["T-07"]
  },
  {
    "task_id": "T-09",
    "title": "backup: export/import manifest + ไฟล์ blocklist (ไม่ผ่าน DB)",
    "layer": "service",
    "files": ["backend/internal/model/backup.go", "backend/internal/service/backup.go", "backend/internal/api/handlers.go"],
    "instruction": "ทำตาม §2.4 ของแผน — backup ของโปรเจกต์เป็น JSON ไฟล์เดียว (model.BackupFile) ไม่ใช่ archive และ Meta.Checksum คำนวณจาก marshalled Config:\n1) model/backup.go: เพิ่มใน BackupConfig — Blocklists []DNSBlocklist `json:\"blocklists,omitempty\"` และ BlocklistFiles []DNSBlocklistFilePayload `json:\"blocklistFiles,omitempty\"` (payload = {ID, Sha256, GzipBase64 string}) **ทั้งคู่ต้อง omitempty** ด้วยเหตุผล checksum-compatibility เดียวกับที่ comment ไว้แล้วที่ backup.go:87-92 (backup เก่าที่ไม่มีคีย์นี้ต้อง re-marshal ได้ bytes เท่าเดิม) — เขียน comment อ้างเหตุผลนี้ไว้ด้วย; **blockMode เดินทางไปกับ DNSBlocklist อยู่แล้ว (json:\"blockMode,omitempty\") จึงไม่ต้องเพิ่มฟิลด์ระดับ BackupConfig อีก**\n2) service/backup.go Export (~บรรทัด 96-180): ดึง lists จาก DNSBlocklistService (inject ผ่าน setter แบบ SetCounterStore ที่บรรทัด 49-51 เพื่อไม่แตะ NewBackupService signature ที่ยาวอยู่แล้ว); ใส่ payload ของไฟล์ **เฉพาะ <id>.hosts เท่านั้น ห้ามพก <id>.conf** (เป็นไฟล์ derived สร้างใหม่ได้ตอน import — แผน §2.1.1/§2.4) และ **เฉพาะ sourceType==upload เสมอ** (re-fetch ไม่ได้) ส่วน url ใส่ก็ต่อเมื่อ caller ขอ (flag includeBlocklistFiles); gzip แล้ว base64; **cap รวม 8 MB** เกินให้ตัดที่เหลือออก + เพิ่ม warning (import handler จำกัด body 10 MB ที่ handlers.go:3378-3383)\n3) service/backup.go Import: verify sha256 ของไฟล์ที่ decode ได้ **ก่อน** เขียนลงดิสก์เสมอ (ไฟล์นี้ dnsmasq จะโหลด) → เขียนผ่าน kernel WriteBlocklistFile → **normalize blockMode แล้วถ้าเป็น nxdomain ให้ re-render <id>.conf จากโดเมนที่เพิ่งเขียน (ใช้เส้นทางเดียวกับ renderArtifacts ของ T-05 ห้ามเขียนซ้ำ)** ; ถ้า SupportsBulkNXDomain()==false ให้ downgrade เป็น sinkhole + ใส่ warning ใน ImportResult (import ต้องไม่ล้มทั้งก้อนเพราะเรื่องนี้) → store.ReplaceAll(lists); list ที่ไม่มี payload ให้ตั้ง domainCount=0 + lastError='needs refresh after import' (ApplyZones จะข้ามเองเพราะ stat ไม่เจอไฟล์) ; นับ blocklists ลง ImportResult.Counts\n4) handlers.go: export endpoint รับ query ?includeBlocklistFiles=1 (default ปิด)\n5) test: export→import round-trip ของ upload list ได้ไฟล์กลับครบและ sha256 ตรง; **round-trip ของ list โหมด nxdomain ได้ .conf กลับมาโดยที่ backup ไม่ได้พก .conf ไปด้วย**; backup เก่า (ไม่มีคีย์ใหม่) ยัง import ผ่าน checksum ได้เหมือนเดิม (regression test ข้อนี้สำคัญที่สุด)",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/service/... ผ่าน", "backup ไฟล์เก่าที่ export ก่อนฟีเจอร์นี้ยัง import ได้ (checksum ไม่พัง)", "backup ไม่มี payload ของ .conf อยู่เลย"],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-10",
    "title": "เอกสาร API: openapi.yaml ทั้งสองไฟล์",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "เพิ่ม path กลุ่ม /api/dns/blocklists ทั้งหมด (GET/POST/PUT/DELETE/toggle/refresh/upload) + schema DNSBlocklist/DNSBlocklistInput (**รวมฟิลด์ blockMode: enum [sinkhole, nxdomain] default sinkhole**) และ query ?includeBlocklistFiles ของ config export ให้ **สองไฟล์ตรงกันเป๊ะ** ระบุใน description ว่า: เลือกโหมดได้ต่อ list ไม่ใช่ต่อโดเมน, โหมด sinkhole ใช้ addn-hosts ตอบ 0.0.0.0 และ **ไม่ครอบ subdomain**, โหมด nxdomain ใช้ conf-file ที่มี address=/<domain>/ และ **ครอบ subdomain** พร้อมต้องการ dnsmasq >= 2.86 และมีเพดานโดเมนแยกต่างหาก, metadata เก็บใน JSON manifest ไม่ใช่ DB, และ mutation ต้องเป็น super_admin",
    "acceptance": ["diff ของสองไฟล์ว่าง", "หน้า ApiDocs render ได้ไม่พัง"],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-11",
    "title": "frontend: API client + mock data",
    "layer": "frontend",
    "files": ["frontend/src/services/dnsServerService.ts", "frontend/src/data-mockup/mockData.ts"],
    "instruction": "1) mockData.ts: type DNSBlocklist (ตรงกับ Go model รวม **blockMode: \"nxdomain\" | \"sinkhole\"** ใช้ union แบบเดียวกับ BlockedDomain.mode ที่ mockData.ts:839), initialDNSBlocklists (2 รายการ: subscribe StevenBlack โหมด sinkhole + upload โหมด nxdomain เพื่อให้เห็นทั้งสองโหมดใน UI), const DNS_BLOCKLISTS_MAX=8, DNS_BLOCKLIST_MAX_FILE_MB=16, DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS=150000\n2) dnsServerService.ts: getBlocklists/createBlocklistFromUrl/uploadBlocklist/updateBlocklist/deleteBlocklist/toggleBlocklist/refreshBlocklist ตาม pattern เดิมทุกประการ (branch IS_MOCK_MODE ใช้ localStorage key ใหม่ pigate_dns_blocklists; โหมด mock ของ upload ให้ parse ไฟล์ฝั่ง browser คร่าว ๆ เพื่อให้ domainCount สมจริง) — ทุกตัวที่รับ/ส่ง blockMode ต้องส่งขึ้น backend ตามสัญญาใน T-08 (create = JSON body, upload = query ?blockMode=)\n3) uploadBlocklist ส่ง body เป็น raw text ให้ตรงกับที่ backend รับใน T-08 (Content-Type text/plain + ?name= + ?blockMode=)\n4) โหมด mock ให้ enforce เพดาน DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS แบบเดียวกับ backend เพื่อให้ทดสอบ error path ของ UI ได้โดยไม่ต้องมีบอร์ด",
    "acceptance": ["yarn build ผ่าน", "yarn lint ผ่าน", "โหมด mock เพิ่ม/ลบ/toggle/สลับโหมด ได้ และค่าคงอยู่หลัง reload"],
    "depends_on": ["T-10"]
  },
  {
    "task_id": "T-12",
    "title": "frontend: แท็บ Blocklists ในหน้า DNS Server (พร้อมตัวเลือกโหมดบล็อก)",
    "layer": "frontend",
    "files": ["frontend/src/pages/DnsServer.tsx"],
    "instruction": "1) เพิ่ม 'blocklists' ลง VALID_TABS (DnsServer.tsx:83) และ TabsTrigger ใหม่ (บรรทัด 791-796) วางระหว่าง Blocked Domains กับ Settings\n2) TabsContent: Card + Table (shadcn เท่านั้น) คอลัมน์ Name | Source (badge url/upload + host ของ URL) | **Mode (badge Sinkhole/NXDOMAIN — ใช้ pattern badge เดียวกับแท็บ Blocked Domains ที่ DnsServer.tsx:1112-1117 เพื่อให้หน้าตาสอดคล้องกัน)** | Domains (toLocaleString) | Last fetched | Status | Enabled (Switch) | Actions (Refresh/Edit/Delete) + แถบสรุปยอดรวมโดเมนเทียบกับ 500,000 **และยอดรวมเฉพาะโหมด NXDOMAIN เทียบกับ 150,000**\n3) ปุ่ม Add: Drawer/Dialog 2 โหมดแหล่งที่มา (Subscribe URL / Upload file) — โหมด URL บังคับ https:// และแจ้งชัดว่า URL ภายใน LAN ใช้ไม่ได้ (มาตรการความปลอดภัย); โหมด upload ใช้ <input type=file accept='.txt,.hosts,text/plain'> ตรวจขนาดฝั่ง browser ก่อนส่ง\n3b) **ในฟอร์ม Add/Edit เพิ่ม `<Select>` เลือกโหมดบล็อก ใช้ pattern เดียวกับ blkMode ของแท็บ Blocked Domains (DnsServer.tsx:139,1742-1749) แต่สลับลำดับ/ค่าเริ่มต้น**: SelectItem 'sinkhole' = 'Sinkhole (ค่าเริ่มต้น — ตอบ 0.0.0.0 / ::)' และ 'nxdomain' = 'NXDOMAIN (ตอบว่าไม่พบโดเมน — ครอบ subdomain ด้วย)'; state เริ่มต้นเป็น 'sinkhole' และ reset กลับเป็น 'sinkhole' ทุกครั้งที่ปิดฟอร์ม (เทียบกับบรรทัด 637 ที่ deny-list reset เป็น 'nxdomain')\n4) Alert ถาวรบนแท็บ (แทน Alert 'sinkhole-only' ของแผนรอบก่อน) อธิบายความต่างของสองโหมดให้ครบ: **Sinkhole** = ตอบ 0.0.0.0/:: และ **บล็อกเฉพาะชื่อที่อยู่ในไฟล์ ไม่ครอบ subdomain** (เร็วและเบาที่สุด เหมาะกับ list ขนาดใหญ่); **NXDOMAIN** = ตอบว่าไม่พบโดเมนและ **ครอบ subdomain ทั้งหมด** แต่ dnsmasq ต้อง parse ไฟล์ทุกครั้งที่กด Apply จึงมีเพดานโดเมนต่ำกว่าและทำให้ Apply ช้าลง + ต้องใช้ dnsmasq 2.86 ขึ้นไป; และย้ำว่า **โหมดตั้งได้ระดับ list ไม่ใช่รายโดเมน** ถ้าต้องการรายโดเมนให้ใช้แท็บ Blocked Domains (พร้อมลิงก์ไปแท็บนั้น)\n5) หลัง create/refresh/toggle/delete/**เปลี่ยนโหมด** ต้อง setIsApplied(false) เหมือน flow ของ blocked domains เดิม (การเปลี่ยนโหมดไม่ restart dnsmasq เอง ต้องกด Apply DNS)\n6) loading state ระหว่าง refresh/upload/สลับโหมด (ใช้เวลาหลายวินาที) และกันกดซ้ำ; error จาก backend เรื่อง 'dnsmasq เก่าเกินไป' และ 'เกินเพดานโหมด NXDOMAIN' ต้องแสดงเป็นข้อความอ่านรู้เรื่อง ไม่ใช่ toast ว่า error เฉย ๆ\n7) style: shadcn/ui เท่านั้น, สี semantic variable ห้าม hardcode palette, ห้าม shadow-*/backdrop-blur-*, รองรับ dark/light, ตาม docs/rules_of_work.md ให้ใช้ `<Drawer direction="right">` (Dialog สงวนไว้เฉพาะ confirmation) และเนื่องจากฟอร์มนี้ไม่มี Combobox จึงไม่ต้องใส่ modal={false}",
    "acceptance": ["yarn build + yarn lint ผ่าน", "แท็บใหม่ใช้งานครบทุก action ในโหมด mock รวมถึงสลับโหมดไป-กลับ", "deep-link ?tab=blocklists เปิดตรงแท็บ", "badge โหมดในตารางหน้าตาสอดคล้องกับแท็บ Blocked Domains"],
    "depends_on": ["T-11"]
  },
  {
    "task_id": "T-13",
    "title": "install.sh + เอกสาร subsystem",
    "layer": "service",
    "files": ["install.sh", "docs/ref/complete/dns-system-design.md", "README.md"],
    "instruction": "1) install.sh (ต่อจากบล็อก /var/lib/pigate บรรทัด 307-312): mkdir -p /var/lib/pigate/blocklists; chown pigate:netdev; chmod 755 — โค้ด Go ก็ MkdirAll เองได้ แต่ทำที่นี่เพื่อให้ ownership ถูกตั้งแต่แรกและเครื่องที่ติดตั้งไปแล้วได้ไดเรกทอรีตอนรัน install.sh ซ้ำ\n2) docs/ref/complete/dns-system-design.md: เพิ่มหัวข้อ Blocklists — **สองกลไก render ต่อโหมด (addn-hosts สำหรับ sinkhole / conf-file + address=/d/ สำหรับ nxdomain) พร้อมตารางเปรียบเทียบต้นทุนตามแผน §2.1.3**, ทำไมต้อง render ไฟล์เอง (ทิ้ง IP ต้นฉบับ), ทำไมโหมดเป็นระดับ list ไม่ใช่ระดับโดเมน, ทำไม .hosts เป็น canonical และ .conf เป็น derived, ข้อกำหนด dnsmasq >= 2.86, JSON manifest schema + เหตุผลที่ไม่ใช้ SQLite, การนับสถิติด้วย hash index แบบ mode-aware, ความสัมพันธ์กับ deny-list เดิม\n2b) **บันทึกตัวเลขที่วัดได้จริงบนบอร์ด** (จาก Final Acceptance): เวลา Apply ของ list 93k โหมด sinkhole vs nxdomain, RSS ของ dnsmasq ทั้งสองโหมด, และระบุว่าเพดาน DNSBlocklistMaxNXDomainDomains=150000 ควรคงไว้/ปรับเป็นเท่าไร พร้อมเหตุผล\n3) README Feature Status: เพิ่มบรรทัด DNS Blocklists (Completed both)",
    "acceptance": ["bash -n install.sh ผ่าน", "เอกสารอัปเดตครบ", "มีตัวเลขที่วัดจริงในเอกสาร ไม่ใช่ค่าประมาณจากแผน"],
    "depends_on": ["T-12"]
  },
  {
    "task_id": "T-14",
    "title": "(OPTIONAL — เฟสถัดไป ไม่อยู่ในรอบนี้) auto-refresh ตามกำหนดเวลา",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist.go", "backend/cmd/pigate/main.go"],
    "instruction": "ทำเฉพาะเมื่อ owner สั่งในรอบถัดไป: เพิ่มฟิลด์ refreshIntervalHours (0 = manual only) ใน manifest schema (bump BlocklistManifestSchemaVersion เป็น 2 พร้อมทางอ่านไฟล์ v1 ได้) แล้วสร้าง goroutine เดียวใน DNSBlocklistService.StartScheduler(ctx) tick ทุก 1 ชั่วโมง ตรวจ list ที่ถึงกำหนดแล้ว refresh ทีละตัวแบบ sequential (ห้ามยิงพร้อมกัน) และเรียก ApplyAll ครั้งเดียวท้ายรอบ **เฉพาะเมื่อ sha256 ของอย่างน้อยหนึ่ง list เปลี่ยน** (ไฟล์เหมือนเดิม = ห้าม restart dnsmasq เพราะกระทบ DHCP ด้วย) รับ ctx จาก main.go และจบ goroutine เมื่อ ctx cancel (สไตล์ netlink_monitor.go) ห้ามใช้ cron/shell",
    "acceptance": ["go build ./... ผ่าน", "goroutine หยุดเมื่อ ctx cancel", "sha256 ไม่เปลี่ยน = ไม่ restart dnsmasq"],
    "depends_on": ["T-13"]
  }
]
```

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/dns/blocklists` | `authRoute` | คืน metadata จาก manifest (ไม่มีรายชื่อโดเมน) รวม `blockMode` |
| POST | `/api/dns/blocklists` | `superAdminRoute` | สร้างจาก URL + fetch ทันที (รับ `blockMode`) |
| POST | `/api/dns/blocklists/upload` | `superAdminRoute` + อยู่ใน `bodyLimitExemptPaths` | อัปโหลดไฟล์ hosts (รับ `?blockMode=`) |
| PUT | `/api/dns/blocklists/{id}` | `superAdminRoute` | แก้ชื่อ/URL/enabled/**blockMode** (เปลี่ยนโหมดไม่ fetch ใหม่) |
| POST | `/api/dns/blocklists/{id}/refresh` | `superAdminRoute` | re-fetch (เฉพาะ sourceType=url) |
| POST | `/api/dns/blocklists/{id}/toggle` | `superAdminRoute` | เปิด/ปิด |
| DELETE | `/api/dns/blocklists/{id}` | `superAdminRoute` | ลบรายการ + ไฟล์ทั้ง `.hosts` และ `.conf` |
| GET | `/api/system/config/export?includeBlocklistFiles=1` | `superAdminRoute` (เดิม) | เพิ่ม query ใหม่ |
| POST | `/api/dns/apply` (เส้นเดิม) | เดิม | จุดเดียวที่ restart dnsmasq |

`-disable-edit=true` บล็อก mutation ทั้งหมดผ่าน `DisableEditMiddleware` — ถูกต้องตามที่ควรเป็น

---

## 5. ข้อควรระวัง (อะไรพัง / พังอย่างไร / กันอย่างไร)

1. **`addn-hosts` ชี้ไฟล์ที่ไม่มีอยู่ อาจทำให้ dnsmasq ไม่ start → DNS + DHCP ล่มทั้งบ้าน**
   (คลาสเดียวกับ issue #50) และ `dnsmasq --test` **จับไม่ได้** เพราะเช็คแค่ syntax
   → `os.Stat` ทุกไฟล์ก่อน emit (T-02) + เขียนไฟล์ให้เสร็จก่อนเขียน conf เสมอ +
   **ต้องทดสอบบนบอร์ดจริงว่าไฟล์หายทำให้ dnsmasq ตายจริงหรือแค่ warning** แล้วบันทึกผล (ยังไม่ได้ตรวจ)
2. **ห้ามส่งไฟล์ต้นฉบับให้ dnsmasq** — ไฟล์สาธารณะ map โดเมนไปยัง IP ใดก็ได้ = DNS spoofing
   → parser ทิ้งคอลัมน์ IP เสมอ render ใหม่ด้วย `0.0.0.0` (T-01)
3. **SSRF** → กันที่ dialer `Control` ด้วย `isGloballyRoutable` ครอบทุก redirect hop (T-04)
   ผลข้างเคียงที่ตั้งใจ: **subscribe จาก URL ใน LAN ทำไม่ได้** ต้องบอกในหน้า UI
4. **path traversal**: `id` ประกอบเป็น path ของไฟล์ที่ dnsmasq โหลด (ทั้ง `.hosts` และ `.conf`) →
   `validateBlocklistID` ในชั้น kernel เป็นด่านสุดท้าย ห้ามพึ่ง handler อย่างเดียว
5. **manifest.json คือ single point of failure ของฟีเจอร์นี้** (ไม่มี DB สำรอง) — ไฟล์เสีย =
   รายการหายหมด → กัน 3 ชั้น: atomic temp+rename (ไม่มีสถานะเขียนครึ่งทาง),
   quarantine ไฟล์เสียแล้วเริ่มใหม่แทนที่จะตายถาวร, และ backup พก manifest ไปด้วย (T-09)
   *ผลข้างเคียงที่ต้องยอมรับ:* ถ้า manifest หายแต่ไฟล์ `.hosts`/`.conf` ยังอยู่ ไฟล์เหล่านั้นจะกลายเป็น
   orphan → เพิ่ม log warning ตอน Load ว่าเจอไฟล์ที่ไม่มีใน manifest (ไม่ต้องลบอัตโนมัติ)
6. **race ของ manifest**: refresh สองคำขอพร้อมกันจะเขียนทับกันถ้าล็อกแค่ตอนเขียน →
   ต้องถือ write lock คลุม read-modify-write ทั้งก้อน และรัน `go test -race` (T-03)
7. **checksum ของ backup พัง** ถ้าฟิลด์ใหม่ใน `BackupConfig` ไม่ใส่ `omitempty` —
   backup เก่าทุกไฟล์จะ import ไม่ได้ทันที (comment เตือนไว้แล้วที่ `model/backup.go:87-92`) →
   ต้องมี regression test ด้วย backup เก่าจริง (T-09)
8. **body limit 1 MB**: ถ้าลืมเพิ่ม `/api/dns/blocklists/upload` ลง `bodyLimitExemptPaths`
   (`api/middleware.go:358`) upload 4.5 MB จะพังแบบสับสน
9. **สถิติต้องไม่โกหก**: `dnsBlocklistIndex` ต้อง **exact-match สำหรับ list โหมด sinkhole** และ
   **suffix-match สำหรับ list โหมด nxdomain** ถ้าทำสลับกันหรือใช้กฎเดียวกันทั้งสองโหมด สถิติจะรายงาน
   ตรงข้ามกับสิ่งที่ dnsmasq ทำจริง (T-06 ข้อ 1) และ index ต้องถูกป้อนหลัง `ApplyZones` สำเร็จเท่านั้น
   (เหตุผลเดียวกับ `dns_block_index.go:20-27`)
10. **RAM ของ index สถิติ**: เลือก sorted `[]uint64` (8 B/โดเมน ≈ 4 MB ที่ 500k) แทน
    `map[string]string` (~30 MB) — ต้องวัดจริงแล้วบันทึกตัวเลขไว้ใน doc comment (T-06)
11. **RAM/CPU ของ dnsmasq เอง**: 93k host record ≈ 10-20 MB RSS + start ช้าขึ้นเล็กน้อย —
    คุมด้วย `DNSBlocklistMaxTotalDomains` และแสดงยอดรวมใน UI
    **ไม่ต้อง** ปรับ `cache-size` (ไม่ได้ตั้งไว้เลย = default 150) เพราะ hosts record เป็นโครงสร้างแยกจาก DNS cache
12. **ชนกับ local zone**: โดเมนใน blocklist ที่ตรงกับ zone ที่ enabled จะทำให้ชื่อในบ้านตอบ 0.0.0.0
    (หรือ NXDOMAIN ในโหมดใหม่) → parser ตัดชื่อที่ตรง/เป็น subdomain ของ zone ที่ enabled +
    `pigate.local` + hostname ออกตอน ingest
    **ข้อจำกัดที่รู้ตัว:** สร้าง zone ใหม่ *หลัง* ingest แล้วการกรองจะไม่ย้อนหลัง ต้องกด Refresh (บอกใน UI)
    *หมายเหตุสำหรับโหมด nxdomain:* ถ้า blocklist มีโดเมน **แม่** ของ zone (เช่น block `example.com`
    ขณะที่ zone คือ `home.example.com`) dnsmasq ใช้กฎ "more specific wins" → zone ยังชนะ ไม่ต้องกรองเพิ่ม
    แต่ **ต้องยืนยันบนบอร์ดจริง** ก่อนเชื่อ (อยู่ใน Final Acceptance)
13. **restart dnsmasq = DHCP สะดุด** → ห้าม restart ตอน fetch/refresh/สลับโหมด; restart เฉพาะตอน Apply
14. **`expand-hosts` ใน base config** มีผลเฉพาะชื่อที่ไม่มีจุด — โดเมนใน blocklist มีจุดทุกตัว จึงไม่มีผลข้างเคียง
15. **ทดสอบบนบอร์ดจริงเสี่ยง lock ตัวเอง**: ถ้า list มีโดเมนที่ใช้เข้าหน้าเว็บ PiGate เอง อาจเข้าไม่ได้ →
    ทดสอบเฉพาะตอนเข้าถึงตัวเครื่องได้ และเตรียมทางถอย (ปิด list แล้ว Apply / ลบ
    `/etc/dnsmasq.d/pigate-dns.conf` + restart dnsmasq ด้วยมือ)
    **โหมด nxdomain เพิ่มความเสี่ยงนี้** เพราะครอบ subdomain ทั้งหมดของทุกชื่อในไฟล์
16. **`conf-file=` ที่ชี้ไฟล์หาย = dnsmasq ไม่ start แน่นอน** (ต่างจาก `addn-hosts` ที่ยังไม่ยืนยัน) —
    ข่าวดีคือ `dnsmasq --test` **จับได้** เพราะ `conf-file` ถูกอ่านตั้งแต่ตอน parse option
    → เท่ากับโหมด nxdomain มี safety net เพิ่มมาหนึ่งชั้น **แต่ยังต้อง `os.Stat` ก่อน emit อยู่ดี**
    (ถ้าไฟล์หายหลัง --test ผ่านก็ยังตายได้ และเราไม่อยากให้ Apply ล้มทั้งก้อนเพราะ list เดียวหาย)
17. **`--test` แพงขึ้นในโหมด nxdomain**: dnsmasq ต้อง parse ไฟล์ address= ทั้งไฟล์ตอน `--test` และอีกครั้ง
    ตอน start = **2 รอบต่อการกด Apply หนึ่งครั้ง** → คุมด้วย `DNSBlocklistMaxNXDomainDomains`
    และต้องวัดเวลาจริงบนบอร์ดแล้ว re-tune (T-13) ถ้าเกิน ~5 วินาทีให้ถือว่าเพดานสูงเกินไป
18. **ไฟล์ derived ค้าง (orphan `.conf`)**: สลับ nxdomain → sinkhole แล้วลืมลบ `<id>.conf`
    จะเหลือไฟล์ 3 MB ค้างบน SD และถ้าวันหนึ่งมีใครย้ายไดเรกทอรีนี้ไปใต้ `/etc/dnsmasq.d`
    ไฟล์ที่ควร "ปิดอยู่" จะถูกโหลดเงียบ ๆ → ingest/สลับโหมดต้องเรียก `RemoveBlocklistConfFile` เสมอ
    เมื่อโหมดใหม่เป็น sinkhole (T-05) และห้ามย้ายไดเรกทอรี (doc comment ใน T-02)
19. **โดเมนเดียวกันอยู่ในสอง list คนละโหมด**: ผลลัพธ์ที่ dnsmasq ตอบขึ้นกับ precedence ภายในระหว่าง
    hosts record กับ `address=/d/` ซึ่ง **ยังไม่ได้ยืนยัน** — ทั้งสองทางคือ "ถูกบล็อก" อยู่ดีจึงไม่กระทบ
    ความปลอดภัย แต่ *สถิติ* อาจรายงาน list/mode ที่ไม่ตรงกับสิ่งที่ dnsmasq ใช้จริง
    → ยอมรับได้ (เป็นเคสขอบที่ผู้ใช้ตั้งใจสร้างเอง) แต่ต้องเขียนไว้ใน doc comment ของ T-06
    และยืนยันพฤติกรรมจริงบนบอร์ดถ้ามีเวลา
20. **dnsmasq < 2.86**: lookup ของ `address=/d/` โตเร็วกว่าเชิงเส้น → เปิดโหมด nxdomain ที่ 93k
    บนบอร์ดเก่า (bullseye = 2.85) จะทำให้ DNS ทั้งบ้านช้าลงอย่างรู้สึกได้
    → `SupportsBulkNXDomain()` ปฏิเสธการตั้งโหมดนี้ (T-02/T-05) แต่ **fail-open เมื่ออ่านเวอร์ชันไม่ได้**
    เพราะผลเสียสูงสุดคือ "ช้า" ไม่ใช่ "ไม่ปลอดภัย"

---

## 6. คำตอบของ owner (ปิดประเด็นแล้ว)

| # | ประเด็น | มติ |
|---|---|---|
| R1 | ที่เก็บ metadata | **JSON manifest** `/var/lib/pigate/blocklists/manifest.json` — ไม่ใช้ SQLite เลย (§2.3) |
| R2 | ~~ข้อจำกัดของ `addn-hosts`~~ | ~~ยอมรับ sinkhole-only~~ → **ยกเลิกในรอบที่ 3** ดู R9-R12 ด้านล่าง (ยังต้องเก็บสถิติได้เหมือนเดิม → §2.6 / T-06) |
| R3 | scheme | **https เท่านั้น** |
| R4 | URL ใน LAN | **ไม่อนุญาต** (ตัดสินโดย Tech Lead) — เป็นด่านกัน SSRF หลัก ถ้าจะเปิดต้องเป็น config key แยกพร้อมคำเตือนในภายหลัง |
| R5 | limit | 16 MB/ไฟล์, 8 lists, 300k/list, 500k รวม (+ 150k เฉพาะโหมด nxdomain — R12) |
| R6 | auto-refresh | **manual refresh ก่อน** (ตัดสินโดย Tech Lead) — T-14 เป็น optional เฟสถัดไป ไม่อยู่ในรอบนี้ |
| R7 | สถิติ | ทำ (ย้ายจาก optional มาเป็น **T-06 บังคับ**) ออกแบบให้กิน RAM ~4 MB ที่ 500k โดเมน |
| R8 | route protection | **GET = `authRoute`, mutation ทุกตัว = `superAdminRoute` แบบ explicit** (ตัดสินโดย Tech Lead) — เพราะสั่งให้บอร์ดยิง HTTP ออกภายนอกและเขียนไฟล์หลาย MB จึงควรชัดในโค้ดแบบเดียวกับ reboot/config-export ไม่ใช่พึ่ง `RoleReadOnlyMiddleware` เท่านั้น |
| **R9** | **เลือกโหมดบล็อกได้ (owner สั่งเพิ่ม รอบที่ 3)** | **ทำได้** — `sinkhole` ใช้ `addn-hosts=<id>.hosts` เหมือนเดิม, `nxdomain` ใช้ `conf-file=<id>.conf` ที่ภายในเป็น `address=/<domain>/` บรรทัดละโดเมน (man page ยืนยันว่า `address=/d/` ที่ไม่มี IP = NXDOMAIN ของโดเมนนั้นและทุก subdomain) §2.1 |
| **R10** | **granularity ของโหมด** | **ต่อ list เท่านั้น ไม่ทำต่อโดเมน** (ตัดสินโดย Tech Lead) — เหตุผลทางเทคนิค 4 ข้อใน §2.1.2 สรุปสั้น: ไฟล์ต้นทางไม่มีข้อมูลโหมด, UI ไม่มีตารางรายโดเมน (นอกขอบเขตตั้งแต่ต้น), สองกลไกเป็นไฟล์คนละรูปแบบ, และการเก็บสถานะรายโดเมนต้องกลับไปใช้ DB ซึ่งขัดกับ R1 |
| **R11** | **ความไม่สมมาตรของ subdomain** | **ยอมรับและต้องบอกใน UI ให้ชัด** — โหมด sinkhole บล็อกเฉพาะชื่อในไฟล์, โหมด nxdomain ครอบ subdomain ด้วย (เป็นธรรมชาติของสองกลไก ไม่ใช่บั๊ก) ถ้า owner อยากให้สองโหมดครอบ subdomain เหมือนกัน ทางเลือกคือเปลี่ยน sinkhole ไปใช้ `address=/<domain>/#` (ครอบ subdomain, ตอบ 0.0.0.0+::) ซึ่ง **แลกมาด้วยการเสียข้อได้เปรียบด้าน parse cost ของ addn-hosts ไปทั้งหมด** — **ข้อเสนอ: ยังไม่ทำ** รอผลวัดจริงจาก T-13 ก่อนแล้วค่อยตัดสินใจ |
| **R12** | **เพดานของโหมด nxdomain** | **150,000 โดเมน** รวมทุก list ที่เป็นโหมดนี้ (ตัดสินโดย Tech Lead, ต้อง re-tune หลังวัดจริง) — เพราะโดเมนกลุ่มนี้ถูก parse 2 รอบต่อการ Apply หนึ่งครั้ง (`--test` + start) ต่างจาก addn-hosts ที่ `--test` ไม่แตะเลย §2.1.4 |
| **R13** | **เวอร์ชัน dnsmasq ขั้นต่ำของโหมด nxdomain** | **≥ 2.86** (bookworm = 2.89 ผ่านสบาย) ตรวจด้วย `dnsmasq --version` แคชด้วย `sync.Once`; อ่านเวอร์ชันไม่ได้ = **fail-open** §2.1.5 |

---

## 7. Final Acceptance — เกณฑ์ทดสอบรวมท้ายแผน (ทดสอบครั้งเดียวหลัง T-01..T-13 เสร็จครบ)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... && go test ./... && go test -race ./internal/service/... ผ่านทั้งหมด",
    "cd frontend && yarn build && yarn lint ผ่านทั้งหมด",
    "รัน -mock=true: เพิ่ม blocklist จาก URL จริง (https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts) สำเร็จ, domainCount ~90,000-95,000 และไม่มี localhost/broadcasthost/0.0.0.0 ปนอยู่",
    "รัน -mock=true: อัปโหลดไฟล์เดียวกันจาก browser ได้ domainCount เท่ากับวิธี subscribe",
    "unit test ยืนยันว่าไฟล์ .hosts ที่ generate ใช้ 0.0.0.0 เสมอ แม้ input ระบุ IP อื่น (1.2.3.4 bank.example.com)",
    "unit test ยืนยันว่าไฟล์ .conf ที่ generate มีเฉพาะบรรทัด 'address=/<domain>/' หนึ่งโดเมนต่อบรรทัด ไม่มี IP ใด ๆ ปรากฏในไฟล์ และไม่มีบรรทัดไหนยาวเกิน 512 ไบต์",
    "unit test ยืนยันว่า https://127.0.0.1, https://192.168.1.1, https://169.254.169.254, https://[::1] ถูกปฏิเสธที่ชั้น dialer; http:// และ port อื่นถูก reject ตั้งแต่ validate; body เกิน 16 MB และ redirect เกิน 3 hop = error",
    "manifest: ลบ manifest.json แล้ว restart -> ฟีเจอร์ยังทำงาน (manifest ว่าง ไม่ crash); ทำ manifest.json ให้ JSON เสีย -> ถูก quarantine เป็น manifest.json.corrupt-<ts> และเริ่มใหม่; ตั้ง schemaVersion=99 -> ระบบ fail closed ไม่เขียนทับ และ UI แสดง error",
    "manifest: รายการที่ไม่มีคีย์ blockMode หรือมีค่าขยะ -> โหลดได้และกลายเป็น sinkhole (ไม่ทำให้ทั้ง manifest ล้ม)",
    "manifest: ยิง refresh พร้อมกันหลายคำขอแล้วรายการไม่หาย/ไม่ซ้ำ (go test -race ผ่าน)",
    "buildDNSConfig เมื่อไม่มี blocklist ให้ output byte-for-byte เท่าก่อนมีฟีเจอร์นี้ (golden test) และไม่ emit directive สำหรับ id ที่ไฟล์ไม่มีจริง",
    "buildDNSConfig: list โหมด sinkhole ได้บรรทัด addn-hosts= เท่านั้น, list โหมด nxdomain ได้บรรทัด conf-file= เท่านั้น, มีทั้งสองโหมดพร้อมกันได้ครบทั้งสองบรรทัด",
    "สลับโหมดของ list ที่มีอยู่ (ทั้ง url และ upload) ทำได้โดยไม่ต้องต่อเน็ต: nxdomain -> sinkhole แล้วไฟล์ .conf ถูกลบทิ้งจริง, sinkhole -> nxdomain แล้วได้ .conf ที่มีโดเมนครบเท่ากับ domainCount",
    "toggle disable + Apply -> บรรทัดของ list นั้นหายจาก pigate-dns.conf; ลบ list -> ไฟล์ .hosts และ .conf ถูกลบทั้งคู่",
    "refresh ที่ fetch ล้มเหลว -> lastError ถูกบันทึก แต่ไฟล์เดิม/domainCount เดิม/โหมดเดิมยังอยู่ (การบล็อกไม่หลุด)",
    "เพดาน: ตั้ง list รวมเกิน 150,000 โดเมนเป็นโหมด nxdomain -> ถูกปฏิเสธพร้อมข้อความที่ผู้ใช้อ่านรู้เรื่อง แต่ตั้งเป็น sinkhole แทนได้สำเร็จ",
    "สถิติ: เปิด query logging แล้ว query โดเมนที่อยู่ใน blocklist -> หน้า Statistics > DNS > Blocked Query นับ hit และแสดงชื่อ list เป็น rule, mode ตรงกับโหมดของ list นั้น (sinkhole หรือ nxdomain)",
    "สถิติ: query **subdomain** ของโดเมนใน list โหมด sinkhole (ซึ่ง dnsmasq ไม่ได้บล็อก) ต้อง **ไม่** ถูกนับว่า blocked",
    "สถิติ: query **subdomain** ของโดเมนใน list โหมด nxdomain (ซึ่ง dnsmasq บล็อกจริง) **ต้อง** ถูกนับว่า blocked และ mode = nxdomain",
    "สถิติ: 'notexample.com' ไม่ถูกนับว่าเป็น subdomain ของ 'example.com' ในทุกโหมด (label boundary)",
    "สถิติ: deny-list เดิมยังแสดง rule/mode ของตัวเองถูกต้อง และชนะ blocklist เมื่อแมตช์ทั้งคู่",
    "สถิติ: วัด RSS ของ pigate ก่อน/หลังโหลด blocklist 93k -> เพิ่มไม่เกิน ~10 MB และตัวเลขจริงถูกบันทึกใน doc comment",
    "deny-list เดิม (Blocked Domains tab, cap 1000) ยังทำงานครบทั้ง nxdomain/sinkhole และ default ของมันยังเป็น nxdomain (ไม่ถูกกลืนโดย default ใหม่ของ blocklist)",
    "backup: export -> import กลับ ได้ metadata ครบรวม blockMode; list แบบ upload ได้ไฟล์กลับมาใช้งานได้ทันที (sha256 ตรง); list โหมด nxdomain ได้ไฟล์ .conf กลับมาโดยที่ backup ไม่ได้พก .conf ไปด้วย; list แบบ url ขึ้น needs refresh แล้ว Refresh กลับมาใช้ได้",
    "backup: ไฟล์ backup เก่าที่ export ก่อนฟีเจอร์นี้ยัง import ได้ (checksum ไม่พัง) — regression test",
    "-disable-edit=true: ทุก mutation ของ /api/dns/blocklists ถูกบล็อก; role admin (ไม่ใช่ super_admin) เรียก GET ได้แต่ mutation ไม่ได้",
    "docs/openapi.yaml กับ frontend/public/openapi.yaml diff ว่าง",
    "UI: แท็บ Blocklists รองรับ dark/light, ไม่มี shadow-*/backdrop-blur-*, ไม่มี hardcode palette, deep-link ?tab=blocklists ทำงาน, เลือกโหมดได้ทั้งตอน Add และ Edit, badge โหมดแสดงในตาราง, และมีคำอธิบายครบ: ความต่างของสองโหมด (subdomain coverage + ต้นทุน), โหมดเป็นระดับ list ไม่ใช่รายโดเมน, https-only/no-LAN",
    "[บนบอร์ดจริงเท่านั้น] apply blocklist 93k โดเมน **โหมด sinkhole** -> dnsmasq start ปกติ, DHCP ยังแจก lease ได้, dig ads.doubleclick.net ตอบ 0.0.0.0, dig sub.ads.doubleclick.net (ชื่อที่ไม่อยู่ในไฟล์) ยัง resolve ปกติ, dig โดเมนปกติยังตอบถูก, restart dnsmasq ไม่เกิน ~2 วินาที, RSS ของ dnsmasq เพิ่มไม่เกิน ~40 MB",
    "[บนบอร์ดจริงเท่านั้น] apply blocklist 93k โดเมน **โหมด nxdomain** -> dnsmasq start ปกติ, dig ads.doubleclick.net ตอบ NXDOMAIN, dig sub.ads.doubleclick.net **ก็ตอบ NXDOMAIN ด้วย**, dig โดเมนปกติยังตอบถูก, **จับเวลาทั้งขั้นตอน Apply (dnsmasq --test + restart) แล้วบันทึกตัวเลข** — ถ้าเกิน ~5 วินาที ให้ลดเพดาน DNSBlocklistMaxNXDomainDomains แล้วบันทึกเหตุผลใน T-13",
    "[บนบอร์ดจริงเท่านั้น] เทียบ RSS ของ dnsmasq ระหว่างโหมด sinkhole กับ nxdomain ที่จำนวนโดเมนเท่ากัน แล้วบันทึกผลลง docs/ref/complete/dns-system-design.md",
    "[บนบอร์ดจริงเท่านั้น] ยืนยันว่า local zone ยังชนะ blocklist โหมด nxdomain ที่เป็นโดเมนแม่ (block example.com + zone home.example.com -> ชื่อในบ้านยัง resolve ได้) ตามกฎ most-specific-wins ของ dnsmasq",
    "[บนบอร์ดจริงเท่านั้น] ยืนยันพฤติกรรมของ addn-hosts ที่ชี้ไฟล์หาย (dnsmasq ตาย หรือแค่ warning) และของ conf-file ที่ชี้ไฟล์หาย (คาดว่า dnsmasq ไม่ start และ --test จับได้) แล้วบันทึกผลลงข้อควรระวังข้อ 1 และ 16"
  ]
}
```
