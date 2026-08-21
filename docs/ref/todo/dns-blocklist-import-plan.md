# DNS Blocklist Import — subscribe URL / upload ไฟล์ hosts ขนาดใหญ่ (แท็บใหม่ใน DNS Server)

> เอกสารแผนงานสำหรับฟีเจอร์: เพิ่มความสามารถ import blocklist สาธารณะรูปแบบ hosts
> (เช่น StevenBlack/hosts ~93,515 โดเมน) เข้า dnsmasq ผ่านไฟล์แยกต่างหาก
> **โดยไม่ใช้ SQLite เลย ทั้งรายชื่อโดเมนและ metadata** (metadata อยู่ใน JSON manifest)
> — deny-list เดิม (`dns_blocked_domains`, cap 1000) ยังอยู่เหมือนเดิม ไม่ถูกแตะ
>
> วันที่เขียน: 2026-08-22 (แก้ไขรอบที่ 2 หลัง owner ตอบคำถามความเสี่ยง)
> Branch อ้างอิง: `feat/statistics-reference-popover` · ทำงานจริงบน branch ใหม่ `feat/dns-blocklist-import`
> ที่มา: comment ใน `backend/internal/model/dns_validate.go:346-350` —
> "Bulk import of public blocklists is a future feature that would need a separate --conf-file, not this cap raised."

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็น)**
- แท็บใหม่ `Blocklists` ในหน้า `/network/dns-server`
- เพิ่มได้ 2 แบบ: **Subscribe URL** (backend fetch เอง, กด Refresh ซ้ำได้) และ **Upload file**
- แต่ละรายการมี: ชื่อ, ชนิด (url/upload), URL ต้นทาง, enable/disable, จำนวนโดเมนหลัง parse,
  เวลา fetch ล่าสุด, ขนาดไฟล์, error ล่าสุด
- กด **Apply DNS** (ปุ่มเดิมของหน้านี้) แล้ว dnsmasq เริ่มบล็อกจริง
- **หน้า Statistics > DNS แท็บ "Blocked Query" นับ hit จาก blocklist ได้ด้วย** พร้อมบอกว่าโดน list ไหน

**นอกขอบเขต (ตัดชัดเจน)**
- ไม่แตะ deny-list เดิม (`dns_blocked_domains`) ไม่ขยับ `DNSBlockedDomainsMax = 1000`
- ไม่มี allow-list / whitelist / regex exception
- ไม่มี per-client (per-IP/group) blocking
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
| สิทธิ์เขียน `/etc/dnsmasq.d` | มีแล้ว — `setfacl -m u:pigate:rwx` | `install.sh:143-148` |
| ไดเรกทอรีข้อมูล | `/var/lib/pigate` มีแล้ว `pigate:netdev` mode 775 | `install.sh:307-312` |
| service layer DNS Server | `ApplyAll()` + `SetBlockedDomainsSink` | `backend/internal/service/dns_server.go:45-104` |
| kernel interface | `DNSServerManager` (ApplyZones/ClearCache/WatchDNSLog) | `backend/internal/kernel/interfaces.go:167-187` |
| mock kernel | `MockDNSServerManager` (in-memory, `ApplyCount`) | `backend/internal/kernel/mock.go:405-427` |
| validator โดเมน | `ValidateBlockedDomain` (regex `reZoneName`, ≤253, ต้องมีจุด) | `model/dns_validate.go:355-399` |
| deny-list matcher (สถิติ) | `dnsBlockIndex` — `map[string]string` (domain→mode), suffix-match ไต่ parent 16 ชั้น | `service/dns_block_index.go:35-122` |
| จุด classify สถิติ | `recordDomainQuery` เรียก `blockIndex.Empty()` → `Match()` ที่ record-time | `service/dns_query_stats.go:204-212` |
| sink ป้อน index | `StatisticsService.SetBlockedDomains` ← `SetBlockedDomainsSink` ← `ApplyAll` | `service/statistics.go:157,314-316`, `service/dns_server.go:45-101` |
| HTTP client ขาออก | มีตัวเดียวในโปรเจกต์ — ipinfo.io + `isGloballyRoutable` | `service/ipinfo.go:34-239` |
| body limit ของ API | 1 MB ทั้งระบบ ยกเว้น path ใน `bodyLimitExemptPaths` | `api/middleware.go:351-373` |
| backup export | **typed JSON ไฟล์เดียว** (`model.BackupFile`), checksum sha256 คำนวณจาก marshalled `Config` | `service/backup.go:96-180`, `model/backup.go:9-93` |
| ไฟล์ config นอก DB ที่มีอยู่แล้ว | `/var/lib/pigate/pigate.conf` เขียนโดย `internal/config` (`Parse`/`Resolve`/`Write`) | `backend/internal/config/` |
| หน้า DNS Server (frontend) | shadcn `Tabs` 3 แท็บ + deep-link `?tab=` whitelist | `frontend/src/pages/DnsServer.tsx:83-101, 791-796` |
| **blocklist ทั้งฟีเจอร์** | **ยังไม่มีเลยทุกชั้น** | — |

**สรุป:** ฟีเจอร์ใหม่เต็มก้อน แต่มีต้นแบบครบทุก pattern ที่ต้องใช้ในโค้ดเดิมแล้ว

---

## 2. แนวทางเทคนิค

### 2.1 ใช้ `addn-hosts=` ชี้ไฟล์ hosts ที่เรา generate เอง (ยืนยันแล้ว — R2)

```
# /etc/dnsmasq.d/pigate-dns.conf (ท้ายไฟล์ ต่อจาก deny-list เดิม)
# Blocklists (bulk import, hosts format)
addn-hosts=/var/lib/pigate/blocklists/bl-3f9a2c.hosts
```

ทำไมไม่ใช่ `address=/domain/`: 93,515 โดเมน = ~187k directive → `dnsmasq --test` ต้อง parse
ทุกครั้งที่ Apply (ช้ามากบน Pi) และ conf บวมเป็นสิบ MB ตรงกับที่ comment ใน `dns_validate.go:346` เตือน

**ข้อจำกัดที่ owner ยอมรับแล้ว:** sinkhole `0.0.0.0` เท่านั้น (เลือก NXDOMAIN ไม่ได้) และบล็อก
เฉพาะชื่อที่อยู่ในไฟล์ ไม่ครอบ subdomain — AAAA ของชื่อในไฟล์ dnsmasq ตอบ NODATA ให้เอง

### 2.2 ห้ามส่งไฟล์ต้นฉบับให้ dnsmasq (security requirement)

ไฟล์ hosts สาธารณะ map ชื่อ → IP อะไรก็ได้ ถ้าชี้ `addn-hosts` ไปที่ไฟล์ดิบ คนที่คุมไฟล์ต้นทาง
(หรือ MITM หรือคนอัปโหลด) จะชี้โดเมนใดก็ได้ไปยัง IP ของตัวเองได้ = DNS spoofing เต็มรูปแบบ
→ parser **ทิ้งคอลัมน์ IP ต้นฉบับทั้งหมด** แล้ว render ใหม่เป็น `0.0.0.0 <domain>` ของเราเสมอ

ไฟล์ที่ generate (`/var/lib/pigate/blocklists/<id>.hosts`) เขียนแบบ atomic:
`os.CreateTemp` ในไดเรกทอรีเดียวกัน → `Write` → `Sync` → `Chmod(0644)` → `os.Rename`

### 2.3 Metadata = JSON manifest ไฟล์เดียว (**ไม่ใช้ SQLite เลย — ตามที่ owner เลือก (R1)**)

**ตำแหน่ง:** `/var/lib/pigate/blocklists/manifest.json` (ไดเรกทอรีเดียวกับไฟล์ `.hosts`)

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
- `sha256` คือ hash ของไฟล์ `.hosts` ที่เรา render แล้ว (ไม่ใช่ของไฟล์ต้นฉบับ) ใช้เทียบว่าเนื้อหาเปลี่ยนไหม
- `id` ต้องตรง `^bl-[a-z0-9]{1,32}$` และเป็นตัวเดียวกับชื่อไฟล์ `<id>.hosts`

**กฎการเขียน/อ่าน (ต้องทำครบทุกข้อ):**
1. **Locking:** `sync.RWMutex` ตัวเดียวใน `blocklistStore` ครอบ read-modify-write ทั้งก้อน
   (โหลด → แก้ → เขียน) ไม่ใช่แค่ตอนเขียน — ไม่งั้น refresh สองคำขอพร้อมกันจะเขียนทับกัน
   *ไม่ใช้ flock:* มี pigate process เดียวที่เขียนไดเรกทอรีนี้ (SQLite ของโปรเจกต์ก็ตั้งสมมติฐาน
   single-writer เดียวกันอยู่แล้ว) — จดเหตุผลนี้เป็น doc comment ไว้ในไฟล์ store
2. **Atomic write:** marshal (indent 2 spaces, deterministic) → temp file ในไดเรกทอรีเดียวกัน →
   `Sync` → `Rename` — pattern เดียวกับไฟล์ `.hosts` ห้ามเขียนทับไฟล์เดิมตรง ๆ
3. **Versioning:** ไฟล์หาย = manifest ว่าง `schemaVersion:1` (ไม่ใช่ error);
   `schemaVersion` > ที่โค้ดรู้จัก = **fail closed** (ปฏิเสธการเขียน, คืน error ให้ UI แสดง,
   ห้ามเขียนทับไฟล์ของเวอร์ชันใหม่กว่า); JSON พังอ่านไม่ออก = rename ไฟล์เสียเป็น
   `manifest.json.corrupt-<ts>` แล้วเริ่ม manifest ใหม่ + log error (กันฟีเจอร์ตายถาวร)
4. **Cache in RAM:** โหลดครั้งเดียวตอนสร้าง service แล้วเก็บใน struct — การอ่านทุกครั้ง (GET list)
   อ่านจาก RAM ไม่แตะดิสก์ (ลด SD wear ตามหลักของโปรเจกต์)
5. **Layering:** ตัว I/O จริง (อ่าน/เขียน bytes ของ manifest และไฟล์ `.hosts`) อยู่ใน **kernel layer**
   ทั้งหมด (real + mock) — service ทำแค่ marshal/unmarshal + mutex + business rule
   ทำแบบนี้ `-mock=true` จะไม่แตะ filesystem จริงเลยโดยอัตโนมัติ (ข้อบังคับของโปรเจกต์)

### 2.4 Backup/Restore ต้องพก manifest + ไฟล์อัปโหลดไปด้วย

`service/backup.go` export เป็น **JSON ไฟล์เดียว** (`model.BackupFile`) ไม่ใช่ archive และ
`Meta.Checksum` คำนวณจาก marshalled `Config` → **ฟิลด์ใหม่ทุกตัวต้อง `omitempty`**
ไม่งั้น backup เก่าจะ re-marshal ได้ bytes ไม่เท่าเดิมและ checksum พัง (มี comment เตือนไว้แล้วที่
`model/backup.go:87-92`)

```go
// model/backup.go — BackupConfig เพิ่ม 2 ฟิลด์ (omitempty ทั้งคู่)
Blocklists     []DNSBlocklist            `json:"blocklists,omitempty"`     // = lists[] จาก manifest
BlocklistFiles []DNSBlocklistFilePayload `json:"blocklistFiles,omitempty"` // เนื้อไฟล์ .hosts
// DNSBlocklistFilePayload{ ID, Sha256, GzipBase64 string }
```

**กฎว่าไฟล์ไหนถูกใส่เข้า backup:**
- `sourceType == "upload"` → **ใส่เสมอ** (ไฟล์นี้ re-fetch ไม่ได้ ถ้าไม่พกไปด้วยข้อมูลหายถาวร)
- `sourceType == "url"` → **ไม่ใส่** (กด Refresh เอาคืนได้ ไม่ต้องให้ backup บวม)
  ยกเว้นผู้ใช้ส่ง `?includeBlocklistFiles=1` ตอน export (ค่า default = ปิด)
- บีบอัด gzip แล้ว base64 (ไฟล์ 93k โดเมน ~1.9 MB → gzip ~500 KB → base64 ~700 KB)
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
1. **semantic ต่างกัน** — deny-list = suffix-match ไต่ parent (`address=/x/` ครอบ subdomain),
   blocklist = **exact-match เท่านั้น** (`addn-hosts` ไม่ครอบ subdomain) ถ้ายัดรวมกัน
   `Match()` จะไต่ parent ให้ blocklist ด้วย → **สถิติจะรายงานว่าบล็อกทั้งที่ dnsmasq ไม่ได้บล็อก** (โกหก)
2. **โครงสร้างข้อมูลต่างกันเพราะขนาดต่างกัน 500 เท่า** — deny-list ≤1000 แถวเก็บเป็น
   `map[string]string` ได้สบายและต้องเก็บ mode/rule ไว้โชว์; blocklist ถึง 500k โดเมน
   ต้องเก็บแบบประหยัด RAM สุดขีด (ดูด้านล่าง) และ mode เป็น sinkhole เหมือนกันหมด ไม่ต้องเก็บ value
3. lifecycle ต่างกัน (deny-list มาจาก DB, blocklist มาจากไฟล์ที่ apply แล้ว)

**โครงสร้างที่เลือก — sorted `[]uint64` ของ FNV-1a 64-bit + binary search:**

```go
// backend/internal/service/dns_blocklist_index.go (ไฟล์ใหม่)
type blocklistSnapshot struct {
    lists []blocklistEntry // ต่อ list หนึ่งชุด เพื่อบอกได้ว่าโดน list ไหน
}
type blocklistEntry struct {
    id, name string
    hashes   []uint64 // sorted, 8 ไบต์ต่อโดเมน
}
type dnsBlocklistIndex struct{ snap atomic.Pointer[blocklistSnapshot] }
func (idx *dnsBlocklistIndex) Match(domain string) (listName string, ok bool)
```

**RAM (ประเด็นที่ user ให้ประหยัดที่สุด):**

| ทางเลือก | ต่อโดเมน | 93k โดเมน | 500k โดเมน |
|---|---|---|---|
| `map[string]string` (แบบ dnsBlockIndex เดิม) | ~55-65 B | ~6 MB | **~30 MB** |
| `map[string]struct{}` | ~45-50 B | ~4.5 MB | ~24 MB |
| **sorted `[]uint64` + binary search (เลือกอันนี้)** | **8 B** | **~0.75 MB** | **~4 MB** |

- lookup = O(log n) ≈ 19 การเปรียบเทียบ ไม่มี allocation ต่อ query
- ไม่เก็บ string เลย → GC ไม่ต้องไล่ pointer 500k ตัว (ลด GC pressure ด้วย ไม่ใช่แค่ RAM)
- **โอกาสชนของ hash 64-bit ที่ 500k รายการ ≈ 7e-9** และถ้าชนจริง ผลคือ *สถิติ* รายงานว่าโดนบล็อก
  ทั้งที่ไม่โดนเท่านั้น **ไม่มีผลต่อการ resolve DNS จริง** (dnsmasq เป็นคนบล็อก ไม่ใช่ index นี้) —
  ต้องเขียน doc comment ระบุข้อนี้ไว้ชัดเจน
- สร้าง snapshot แบบ streaming (kernel ส่งโดเมนทีละบรรทัดผ่าน callback) → peak RAM = slice เท่านั้น
  ไม่เคยถือ `[]string` 500k ตัวพร้อมกัน; สร้างเสร็จ `sort.Slice` แล้ว swap ด้วย `atomic.Pointer`

**การป้อนข้อมูล (ตามแบบ sink เดิม):** `DNSServerService.ApplyAll` เพิ่ม sink ตัวที่สอง
`SetBlocklistSink(func([]model.DNSBlocklist))` เรียก **หลัง `ApplyZones` สำเร็จเท่านั้น**
(เหตุผลเดียวกับ `SetBlockedDomainsSink` — สิ่งที่ยังไม่ถูก apply ต้องไม่ถูกนับว่า "กำลังบังคับใช้อยู่")
→ `StatisticsService.SetBlocklists` → อ่านไฟล์ผ่าน kernel แบบ streaming → สร้าง snapshot ใหม่

**จุด classify:** `service/dns_query_stats.go:208-212` เช็ค `blockIndex` (deny-list) ก่อนตามเดิม
ถ้าไม่แมตช์จึงเช็ค `blocklistIndex` โดยตั้ง `blockedRule = <ชื่อ list>`,
`blockedMode = model.DNSBlockModeSinkhole` → หน้า Statistics > DNS > Blocked Query แสดงได้ทันที
โดยไม่ต้องแก้ model/API/frontend เลย (คอลัมน์ rule/mode มีอยู่แล้ว)

### 2.7 Apply/reload flow

fetch/upload → parse → เขียนไฟล์ `.hosts` → อัปเดต manifest → **ยังไม่ restart dnsmasq**
→ ผู้ใช้กด **Apply DNS** → `ApplyAll()` ส่ง id ของ list ที่ enabled เข้า `ApplyZones` →
kernel `os.Stat` ทุกไฟล์ → emit `addn-hosts=` เฉพาะไฟล์ที่มีจริง → restart dnsmasq → sink ป้อน index สถิติ

---

## 3. Task List ฉบับสมบูรณ์ (สำหรับ ai-developer — ทำเรียงตาม depends_on)

```json
[
  {
    "task_id": "T-01",
    "title": "model: ค่าคงที่ + types + parser ไฟล์ hosts + validator + manifest schema",
    "layer": "model",
    "files": ["backend/internal/model/dns_blocklist.go", "backend/internal/model/dns_blocklist_test.go", "backend/internal/model/dns_validate.go"],
    "instruction": "สร้างไฟล์ใหม่ backend/internal/model/dns_blocklist.go (pure Go ไม่มี I/O นอกจากรับ io.Reader):\n1) type DNSBlocklist {ID, Name, SourceType, URL string; Enabled bool; DomainCount int; FileBytes int64; Sha256, LastFetchedAt, LastError, CreatedAt string} json tag camelCase ตามสไตล์ model/dns_server.go:81-97 + DNSBlocklistInput{Name, URL string; Enabled bool}\n2) type BlocklistManifest {SchemaVersion int `json:\"schemaVersion\"`; UpdatedAt string `json:\"updatedAt\"`; Lists []DNSBlocklist `json:\"lists\"`} + const BlocklistManifestSchemaVersion = 1 + ValidateBlocklistManifest(m) error (schemaVersion ต้อง >0, id ไม่ซ้ำ, ทุก field ผ่าน validator ของตัวเอง)\n3) const พร้อม doc comment อธิบายเหตุผลของตัวเลขทุกตัว: DNSBlocklistSourceURL=\"url\", DNSBlocklistSourceUpload=\"upload\", DNSBlocklistsMax=8, DNSBlocklistMaxFileBytes=16<<20, DNSBlocklistMaxDomainsPerList=300000, DNSBlocklistMaxTotalDomains=500000, DNSBlocklistNameMax=64, DNSBlocklistMaxLineBytes=512, DNSBlocklistSinkholeIP=\"0.0.0.0\", DNSBlocklistFetchTimeout=60*time.Second\n4) ValidateDNSBlocklistName(name), ValidateDNSBlocklistID(id) (^bl-[a-z0-9]{1,32}$), ValidateDNSBlocklistURL(rawURL): url.Parse, scheme ต้องเป็น https เท่านั้น (owner ยืนยัน https-only), host ไม่ว่าง, u.User != nil = reject, port ต้องว่างหรือ 443, len<=2048, ห้ามมี \\n \\r\n5) ValidateBlocklistDomain(domain string) error — refactor: ย้าย logic ตรวจชื่อโดเมนออกมาจาก ValidateBlockedDomain (model/dns_validate.go:360-383) มาเป็นฟังก์ชันนี้ แล้วให้ ValidateBlockedDomain เรียกใช้ต่อ เพื่อไม่ให้ logic แตกเป็นสองชุด\n6) ParseHostsBlocklist(r io.Reader, exclude map[string]bool) ([]string, BlocklistParseStat, error): bufio.Scanner buffer <= DNSBlocklistMaxLineBytes (บรรทัดยาวเกิน = ข้าม+นับ ไม่ใช่ error); ตัดทุกอย่างหลัง '#'; strings.Fields — ถ้า field แรก parse เป็น IP ได้ ให้ถือว่า field ที่เหลือ (<=16) คือชื่อโฮสต์ และ **ทิ้งค่า IP ต้นฉบับเสมอ** (ข้อบังคับด้านความปลอดภัย §2.2), ถ้า field เดียวและไม่ใช่ IP = domain-only list; ทิ้งชื่อ built-in: localhost, localhost.localdomain, local, localhost4, localhost6, ip6-localhost, ip6-loopback, ip6-localnet, ip6-mcastprefix, ip6-allnodes, ip6-allrouters, broadcasthost, 0.0.0.0; lower-case + ตัดจุดท้าย + ผ่าน ValidateBlocklistDomain; ทิ้งชื่อที่ตรงหรือเป็น subdomain ของคีย์ใน exclude (เทียบแบบ label boundary เหมือน dnsBlockIndex.Match ที่ service/dns_block_index.go:95-122 ห้ามใช้ strings.HasSuffix ดิบ); dedupe ด้วย map; เกิน DNSBlocklistMaxDomainsPerList = error; BlocklistParseStat{TotalLines, Accepted, SkippedComment, SkippedInvalid, SkippedExcluded, Duplicates}\n7) RenderHostsFile(id string, domains []string, generatedAt time.Time) []byte — header comment + '0.0.0.0 <domain>' บรรทัดละตัว, sort.Strings ให้ deterministic (unit test เทียบ byte ได้)\n8) ParseHostsFileDomains(r io.Reader, fn func(string) error) error — อ่านไฟล์ที่ *เรา* generate กลับมาแบบ streaming (ใช้ตอนสร้าง index สถิติ T-06) ห้าม return []string\nUnit test: ไฟล์ตัวอย่างสไตล์ StevenBlack (header comment, 127.0.0.1 localhost, ::1, 255.255.255.255 broadcasthost, 0.0.0.0 domain), input ที่ระบุ IP อื่น (1.2.3.4 bank.example.com) ต้องได้ output เป็น 0.0.0.0 หรือถูกทิ้ง, comment ท้ายบรรทัด, dedupe, exclude subdomain, บรรทัดยาวเกิน, unicode",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/model/... ผ่าน", "test เดิมของ ValidateBlockedDomain ยังผ่านครบ (ไม่ regress)"],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "kernel: ไฟล์ .hosts + manifest I/O + emit addn-hosts (real + mock)",
    "layer": "kernel",
    "files": ["backend/internal/kernel/interfaces.go", "backend/internal/kernel/dns_server.go", "backend/internal/kernel/dns_blocklist.go", "backend/internal/kernel/mock.go", "backend/internal/kernel/dns_server_test.go"],
    "instruction": "SENSITIVE: เขียนไฟล์ที่ dnsmasq จะโหลด + ประกอบ path จาก id ภายนอก — review เข้มเรื่อง path traversal และลำดับ write-before-reference\n1) interfaces.go:167-187 เพิ่มใน DNSServerManager พร้อม doc comment:\n   - WriteBlocklistFile(id string, content []byte) error\n   - RemoveBlocklistFile(id string) error   // ไม่มีไฟล์ = ไม่ error\n   - BlocklistFileInfo(id string) (size int64, exists bool)\n   - StreamBlocklistFile(id string, fn func(line string) error) error  // อ่านกลับแบบ streaming สำหรับ index สถิติ\n   - ReadBlocklistManifest() ([]byte, error)   // ไฟล์หาย = คืน (nil, nil) ไม่ใช่ error\n   - WriteBlocklistManifest(content []byte) error\n   - QuarantineBlocklistManifest() error       // rename ไฟล์เสียเป็น manifest.json.corrupt-<unix>\n   และเปลี่ยน signature ApplyZones(zones, interfaces, upstreamServers, queryLog, blocked, blocklistIDs []string) error\n2) ไฟล์ใหม่ kernel/dns_blocklist.go (//go:build linux) implement บน RealDNSServerManager:\n   - const blocklistDir=\"/var/lib/pigate/blocklists\", manifest = blocklistDir+\"/manifest.json\"; MkdirAll(dir,0755) ก่อนทุกการเขียน\n   - **validateBlocklistID(id)**: ^bl-[a-z0-9]{1,32}$ เท่านั้น ไม่ผ่าน = error ทันที ห้ามใช้ filepath.Clean แทน (id ถูกใช้ประกอบ path จริง)\n   - atomicWrite(path, content): os.CreateTemp(dir,\".tmp-*\") → Write → Sync → Chmod(0644) → Rename; error กลางทางต้อง os.Remove temp เสมอ — ใช้ helper ตัวเดียวกันทั้งไฟล์ .hosts และ manifest\n3) dns_server.go: buildDNSConfig รับพารามิเตอร์ใหม่ blocklistPaths []string (path ที่ stat แล้วว่ามีจริง — ผู้เรียกกรองมาให้) แล้ว emit ท้ายสุดต่อจาก deny-list (dns_server.go:331-337): '# Blocklists (bulk import, hosts format)\\naddn-hosts=<path>\\n' โดย **filter ก่อนแล้วค่อยเช็ค len()** ตาม pattern blockedLines เดิม เพื่อให้ output เมื่อไม่มี blocklist byte-for-byte เท่าเดิม\n4) ApplyZones: หลัง ensureDnsmasqBaseConfig ให้ loop blocklistIDs → validateBlocklistID → os.Stat → เก็บเฉพาะไฟล์ที่มีจริงและ size>0; ตัวที่หาย log.Printf warning แล้วข้าม (ห้าม emit addn-hosts ชี้ไฟล์ที่ไม่มี — Caution 1)\n5) mock.go:405-427 — MockDNSServerManager: เพิ่ม blocklists map[string][]byte + manifest []byte ใน struct (**in-memory ล้วน ห้ามแตะ filesystem จริงเด็ดขาด**), implement ทุกเมธอดใหม่, แก้ signature ApplyZones\n6) dns_server_test.go: เพิ่มเคส มี/ไม่มี blocklist, path ที่ไม่มีไฟล์ไม่ถูก emit, output ตอนไม่มี blocklist เท่ากับ golden เดิม",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/kernel/... ผ่าน", "ไม่มี exec.Command ใหม่", "mock ไม่เขียนไฟล์ลงดิสก์จริงเลย"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-03",
    "title": "service: blocklistStore — JSON manifest + locking + versioning (แทนที่ตาราง SQLite)",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist_store.go", "backend/internal/service/dns_blocklist_store_test.go"],
    "instruction": "สร้าง backend/internal/service/dns_blocklist_store.go — ชั้นเก็บ metadata แทน SQLite ทั้งหมด (owner ตัดสินใจไม่ใช้ DB แม้แต่ metadata) ทำตาม §2.3 ของ docs/ref/todo/dns-blocklist-import-plan.md:\n1) type blocklistStore struct { mu sync.RWMutex; manager kernel.DNSServerManager; cache model.BlocklistManifest; loaded bool; readOnly bool }\n2) Load() error — manager.ReadBlocklistManifest(); nil/ว่าง = manifest ว่าง schemaVersion=1 (ไม่ใช่ error); json.Unmarshal พัง = เรียก manager.QuarantineBlocklistManifest() + log error + เริ่ม manifest ใหม่; schemaVersion > model.BlocklistManifestSchemaVersion = ตั้ง readOnly=true และคืน error ที่สื่อความหมาย (fail closed ห้ามเขียนทับไฟล์ของเวอร์ชันใหม่กว่า)\n3) เมธอด mutation ทุกตัว **ถือ write lock คลุมทั้ง read-modify-write** ไม่ใช่แค่ตอนเขียน: Add(l), Update(id, mutate func(*model.DNSBlocklist) error), Remove(id), Toggle(id), ReplaceAll(lists) (สำหรับ backup import) — ทุกตัวจบด้วย persistLocked(): ตั้ง UpdatedAt=time.Now().UTC().RFC3339 → json.MarshalIndent (deterministic, เรียง lists ตาม createdAt) → manager.WriteBlocklistManifest(); ถ้า readOnly=true ให้คืน error ทันทีไม่เขียน\n4) การอ่าน (List/Get) อ่านจาก cache ใน RAM ภายใต้ read lock — ห้ามแตะดิสก์ (ลด SD wear) และต้องคืน copy ของ slice ไม่ใช่ตัวอ้างอิงภายใน\n5) ห้ามใช้ flock — เขียน doc comment อธิบายว่า pigate เป็น single writer ตัวเดียวของไดเรกทอรีนี้ (สมมติฐานเดียวกับ SQLite ของโปรเจกต์)\n6) test ด้วย MockDNSServerManager: save/load round-trip, ไฟล์หาย, JSON เสีย -> quarantine + เริ่มใหม่, schemaVersion อนาคต -> readOnly + error, concurrent Add 50 goroutine แล้ว list ครบ (รันด้วย -race)",
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
    "title": "service: DNSBlocklistService (create/refresh/upload/delete/toggle) + ผูกเข้า ApplyAll",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist.go", "backend/internal/service/dns_server.go", "backend/internal/service/dns_blocklist_test.go"],
    "instruction": "1) ไฟล์ใหม่ service/dns_blocklist.go: type DNSBlocklistService struct { store *blocklistStore; manager kernel.DNSServerManager; fetcher *blocklistFetcher; repo *db.Repository /* ใช้อ่าน zones สำหรับ exclude set เท่านั้น ห้ามเขียน */ }\n2) เมธอด: List(), CreateFromURL(name,url,enabled), CreateFromUpload(name, raw []byte, enabled), Refresh(id), Delete(id), Toggle(id), UpdateInfo(id,name,url,enabled)\n   - id สร้างด้วย crypto/rand prefix 'bl-' (pattern เดียวกับ randomID(\"blk-\") ใน api/handlers.go) ต้องผ่าน model.ValidateDNSBlocklistID\n   - CreateFromURL: เช็ค len(List()) < model.DNSBlocklistsMax → fetch+parse+เขียนไฟล์ **ก่อน** แล้วค่อย store.Add — ถ้า fetch/parse พลาด ไม่เพิ่มลง manifest เลย (ไม่ทิ้งแถวเสีย)\n   - Refresh: เฉพาะ sourceType==url; fetch พลาด = **คงไฟล์เดิมและ domainCount เดิมไว้** แล้วบันทึกแค่ lastError (ห้ามลบไฟล์ที่ใช้งานได้อยู่ทิ้ง)\n   - Delete: manager.RemoveBlocklistFile ก่อน แล้ว store.Remove\n3) ingest(id, raw []byte) กลาง: exclude set = ชื่อ zone ที่ enabled (repo.GetDNSZones) + 'pigate.local' + hostname ปัจจุบัน → model.ParseHostsBlocklist → เช็คผลรวมโดเมนของ list ที่ enabled ทั้งหมดไม่เกิน model.DNSBlocklistMaxTotalDomains → model.RenderHostsFile → manager.WriteBlocklistFile → sha256 ของ content → store.Update(meta)\n4) service/dns_server.go ApplyAll (บรรทัด 50-104): ดึง list จาก DNSBlocklistService (inject ผ่าน setter SetBlocklistProvider เพื่อไม่เปลี่ยน signature ของ NewDNSServerService — pattern เดียวกับ SetBlockedDomainsSink ที่บรรทัด 45-47), รวบรวม id ที่ Enabled && DomainCount>0 ส่งเข้า manager.ApplyZones(..., blocklistIDs) และเพิ่ม SetBlocklistSink(fn func([]model.DNSBlocklist)) ที่ถูกเรียก **หลัง ApplyZones สำเร็จเท่านั้น** พร้อม recover() กัน panic เหมือน sink เดิมทุกประการ\n5) test ด้วย MockDNSServerManager + temp DB (สไตล์ service/dns_server_test.go): ingest แล้ว domainCount ถูก, disable แล้ว ApplyAll ไม่ส่ง id นั้น, delete แล้วไฟล์ใน mock หาย, refresh ที่ fetch fail ไม่ทำให้ไฟล์เดิมหาย, เกิน DNSBlocklistsMax = error",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/service/... ผ่าน", "ApplyAll ที่ไม่มี blocklist เลย ยังให้ config เดิมทุก byte"],
    "depends_on": ["T-03", "T-04"]
  },
  {
    "task_id": "T-06",
    "title": "service: dnsBlocklistIndex สำหรับหน้า Statistics > DNS (exact-match, ประหยัด RAM)",
    "layer": "service",
    "files": ["backend/internal/service/dns_blocklist_index.go", "backend/internal/service/dns_blocklist_index_test.go", "backend/internal/service/statistics.go", "backend/internal/service/dns_query_stats.go"],
    "instruction": "ทำตาม §2.6 ของแผน — **แยก index ใหม่ ห้าม merge เข้า dnsBlockIndex เดิม** (semantic ต่างกัน: deny-list = suffix-match, blocklist = exact-match เท่านั้น ถ้ารวมกันสถิติจะรายงานว่าบล็อกทั้งที่ dnsmasq ไม่ได้บล็อก)\n1) ไฟล์ใหม่ service/dns_blocklist_index.go:\n   - type blocklistEntry struct { id, name string; hashes []uint64 }  // sorted\n   - type blocklistSnapshot struct { lists []blocklistEntry; domainCount int }\n   - type dnsBlocklistIndex struct { snap atomic.Pointer[blocklistSnapshot] }\n   - hash = FNV-1a 64-bit ของ domain ที่ lower-case แล้ว (hash/fnv จาก stdlib) — **เก็บแค่ hash 8 ไบต์ ห้ามเก็บ string** (500k โดเมน = ~4 MB แทน ~30 MB ถ้าใช้ map[string]string)\n   - Empty() bool (fast path ก่อน Match ทุกครั้ง), DomainCount() int, Match(domain string) (listName string, ok bool) ใช้ sort.SearchUint64s ต่อ list (<=8 lists) — ไม่ allocate ต่อ query\n   - Set(entries []blocklistEntry) สร้าง snapshot ใหม่แล้ว atomic swap (ไม่ล็อกฝั่งอ่านเลย)\n   - doc comment ต้องระบุ: (ก) exact-match เท่านั้นเพราะ addn-hosts ไม่ครอบ subdomain (ข) โอกาส hash ชนที่ 500k รายการ ~7e-9 และถ้าชนจะกระทบแค่ตัวเลขสถิติ **ไม่กระทบการ resolve DNS จริง** (ค) ห้าม log domain ออกมา (privacy เหมือน recordDomainQuery)\n2) statistics.go: เพิ่มฟิลด์ blocklistIndex ใน dnsQueryStats (ข้าง blockIndex บรรทัด ~144-150) + สร้างใน NewStatisticsService (~157) + เมธอด SetBlocklists(lists []model.DNSBlocklist) ที่สร้าง snapshot โดย **stream** โดเมนจากไฟล์ผ่าน kernel StreamBlocklistFile + model.ParseHostsFileDomains (ห้ามสร้าง []string 500k ตัว) — StatisticsService ต้องรับ kernel.DNSServerManager เข้ามา (setter post-construction แบบเดียวกับ SetLogBuffer/SetBlockedStatsLimit ห้ามเปลี่ยน signature constructor)\n3) dns_query_stats.go:208-212: หลังเช็ค blockIndex แล้วไม่แมตช์ ให้เช็ค blocklistIndex ต่อ (นอก s.dns.mu เหมือนเดิม เพื่อไม่ให้เกิด lock-ordering inversion) ถ้าแมตช์ให้ blocked=true, blockedRule=<ชื่อ list>, blockedMode=model.DNSBlockModeSinkhole → หน้า Statistics แสดงได้เลยโดยไม่ต้องแก้ model/API/frontend\n4) test: exact-match ทำงาน, subdomain ของโดเมนใน list **ต้องไม่** ถูกนับว่า blocked, deny-list ชนะ blocklist เมื่อแมตช์ทั้งคู่, Set ใหม่ไม่ re-classify ประวัติเดิม (พฤติกรรมเดิมของ record-time classification), benchmark/วัด RAM ของ 100k โดเมนแล้วบันทึกตัวเลขจริงลง doc comment",
    "acceptance": ["go build ./... ผ่าน", "go test -race ./internal/service/... ผ่าน", "RAM ที่วัดได้จริงของ 100k โดเมนถูกบันทึกไว้ใน doc comment", "หน้า Statistics > DNS > Blocked Query แสดง hit จาก blocklist พร้อมชื่อ list"],
    "depends_on": ["T-05"]
  },
  {
    "task_id": "T-07",
    "title": "main.go: wiring service ใหม่ + sink สถิติ",
    "layer": "service",
    "files": ["backend/cmd/pigate/main.go"],
    "instruction": "1) สร้าง dnsBlocklistService ต่อจาก dnsServerService (main.go:258) แล้วเรียก Load() ของ store ทันที (error ให้ log warning ไม่ล้ม process — ฟีเจอร์นี้ห้ามทำให้บอร์ดบูตไม่ขึ้น)\n2) dnsServerService.SetBlocklistProvider(dnsBlocklistService) และ dnsServerService.SetBlocklistSink(statisticsService.SetBlocklists) — **ต้องอยู่ก่อน dnsServerService.InitApplyConfig() ที่บรรทัด 675** ด้วยเหตุผลเดียวกับ SetBlockedDomainsSink (main.go:329-335) คือให้ index ถูก prime ตั้งแต่บูต ไม่ใช่รอ Apply ครั้งแรก\n3) statisticsService ต้องได้ kernel.DNSServerManager ผ่าน setter เพื่ออ่านไฟล์ blocklist\n4) ส่ง dnsBlocklistService เข้า api.NewServer (main.go:484)\n5) **ไม่ต้อง** เพิ่ม InitApplyConfig ใหม่ — blocklist ถูก apply ผ่าน dnsServerService.InitApplyConfig() เดิมอยู่แล้ว (เขียน comment ระบุไว้)\n6) หมายเหตุใน comment: fetcher ยังทำ HTTPS จริงแม้รัน -mock=true (เป็น outbound request ธรรมดา ไม่ใช่การแตะ OS) แต่ไฟล์/manifest จะไปลง MockDNSServerManager ใน RAM ทั้งหมด",
    "acceptance": ["go build ./... ผ่าน", "./pigate-backend -mock=true รันขึ้นได้ ไม่ panic แม้ไม่มี manifest.json"],
    "depends_on": ["T-06"]
  },
  {
    "task_id": "T-08",
    "title": "api: handlers + routes สำหรับ blocklists",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go", "backend/internal/api/router.go", "backend/internal/api/middleware.go", "backend/internal/api/server.go"],
    "instruction": "SENSITIVE (input validation + upload):\n1) server.go: เพิ่มฟิลด์ dnsBlocklistService + พารามิเตอร์ใน NewServer\n2) handlers.go (ต่อจากกลุ่ม blocked domains ~4108-4200): HandleGetDNSBlocklists, HandleCreateDNSBlocklist, HandleUpdateDNSBlocklist, HandleDeleteDNSBlocklist, HandleToggleDNSBlocklist, HandleRefreshDNSBlocklist, HandleUploadDNSBlocklist\n3) Upload: รับ raw body (Content-Type text/plain) พร้อม ?name= — **ต้อง** ครอบด้วย http.MaxBytesReader(w, r.Body, model.DNSBlocklistMaxFileBytes) เองในตัว handler ตาม pattern HandleImportConfig (handlers.go:3377-3383) **และเพิ่ม '/api/dns/blocklists/upload' ลง bodyLimitExemptPaths (api/middleware.go:358)** — comment ในนั้นสั่งไว้ชัดว่า endpoint upload ใหญ่ทุกตัวต้องมาลงทะเบียน ถ้าลืมจะถูกตัดที่ 1 MB แบบเงียบ ๆ\n4) router.go เพิ่มกลุ่ม 8.3 ต่อจาก 8.2 (บรรทัด 212-218): GET ใช้ authRoute; **create/upload/refresh/update/delete/toggle ใช้ superAdminRoute แบบ explicit** — เหตุผล: endpoint กลุ่มนี้สั่งให้บอร์ดยิง HTTP ออกไปยัง URL ที่ผู้ใช้ระบุและเขียนไฟล์หลาย MB ลงดิสก์ จึงทำให้ชัดในโค้ดแบบเดียวกับ reboot/config-export แทนที่จะพึ่ง RoleReadOnlyMiddleware เพียงอย่างเดียว\n5) ทุก handler validate ก่อนเสมอ (model.ValidateDNSBlocklist*) และ **ห้ามส่ง error ดิบจาก fetcher กลับไปทั้งก้อน** — สรุปข้อความเอง ไม่ให้รั่ว internal path/URL",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/api/... ผ่าน", "-disable-edit=true บล็อก mutation ของเส้นใหม่ทั้งหมด", "role admin (ไม่ใช่ super_admin) เรียก GET ได้ แต่ POST/PUT/DELETE ไม่ได้"],
    "depends_on": ["T-07"]
  },
  {
    "task_id": "T-09",
    "title": "backup: export/import manifest + ไฟล์ blocklist (ไม่ผ่าน DB)",
    "layer": "service",
    "files": ["backend/internal/model/backup.go", "backend/internal/service/backup.go", "backend/internal/api/handlers.go"],
    "instruction": "ทำตาม §2.4 ของแผน — backup ของโปรเจกต์เป็น JSON ไฟล์เดียว (model.BackupFile) ไม่ใช่ archive และ Meta.Checksum คำนวณจาก marshalled Config:\n1) model/backup.go: เพิ่มใน BackupConfig — Blocklists []DNSBlocklist `json:\"blocklists,omitempty\"` และ BlocklistFiles []DNSBlocklistFilePayload `json:\"blocklistFiles,omitempty\"` (payload = {ID, Sha256, GzipBase64 string}) **ทั้งคู่ต้อง omitempty** ด้วยเหตุผล checksum-compatibility เดียวกับที่ comment ไว้แล้วที่ backup.go:87-92 (backup เก่าที่ไม่มีคีย์นี้ต้อง re-marshal ได้ bytes เท่าเดิม) — เขียน comment อ้างเหตุผลนี้ไว้ด้วย\n2) service/backup.go Export (~บรรทัด 96-180): ดึง lists จาก DNSBlocklistService (inject ผ่าน setter แบบ SetCounterStore ที่บรรทัด 49-51 เพื่อไม่แตะ NewBackupService signature ที่ยาวอยู่แล้ว); ใส่ payload ของไฟล์ **เฉพาะ sourceType==upload เสมอ** (re-fetch ไม่ได้) ส่วน url ใส่ก็ต่อเมื่อ caller ขอ (flag includeBlocklistFiles); gzip แล้ว base64; **cap รวม 8 MB** เกินให้ตัดที่เหลือออก + เพิ่ม warning (import handler จำกัด body 10 MB ที่ handlers.go:3378-3383)\n3) service/backup.go Import: verify sha256 ของไฟล์ที่ decode ได้ **ก่อน** เขียนลงดิสก์เสมอ (ไฟล์นี้ dnsmasq จะโหลด) → เขียนผ่าน kernel WriteBlocklistFile → store.ReplaceAll(lists); list ที่ไม่มี payload ให้ตั้ง domainCount=0 + lastError='needs refresh after import' (ApplyZones จะข้ามเองเพราะ stat ไม่เจอไฟล์) ; นับ blocklists ลง ImportResult.Counts\n4) handlers.go: export endpoint รับ query ?includeBlocklistFiles=1 (default ปิด)\n5) test: export→import round-trip ของ upload list ได้ไฟล์กลับครบและ sha256 ตรง; backup เก่า (ไม่มีคีย์ใหม่) ยัง import ผ่าน checksum ได้เหมือนเดิม (regression test ข้อนี้สำคัญที่สุด)",
    "acceptance": ["go build ./... ผ่าน", "go test ./internal/service/... ผ่าน", "backup ไฟล์เก่าที่ export ก่อนฟีเจอร์นี้ยัง import ได้ (checksum ไม่พัง)"],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-10",
    "title": "เอกสาร API: openapi.yaml ทั้งสองไฟล์",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "เพิ่ม path กลุ่ม /api/dns/blocklists ทั้งหมด (GET/POST/PUT/DELETE/toggle/refresh/upload) + schema DNSBlocklist/DNSBlocklistInput และ query ?includeBlocklistFiles ของ config export ให้ **สองไฟล์ตรงกันเป๊ะ** ระบุใน description ว่า: บล็อกแบบ sinkhole 0.0.0.0 เท่านั้น, ไม่ครอบ subdomain, metadata เก็บใน JSON manifest ไม่ใช่ DB, และ mutation ต้องเป็น super_admin",
    "acceptance": ["diff ของสองไฟล์ว่าง", "หน้า ApiDocs render ได้ไม่พัง"],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-11",
    "title": "frontend: API client + mock data",
    "layer": "frontend",
    "files": ["frontend/src/services/dnsServerService.ts", "frontend/src/data-mockup/mockData.ts"],
    "instruction": "1) mockData.ts: type DNSBlocklist (ตรงกับ Go model), initialDNSBlocklists (2 รายการ: subscribe StevenBlack + upload), const DNS_BLOCKLISTS_MAX=8, DNS_BLOCKLIST_MAX_FILE_MB=16\n2) dnsServerService.ts: getBlocklists/createBlocklistFromUrl/uploadBlocklist/updateBlocklist/deleteBlocklist/toggleBlocklist/refreshBlocklist ตาม pattern เดิมทุกประการ (branch IS_MOCK_MODE ใช้ localStorage key ใหม่ pigate_dns_blocklists; โหมด mock ของ upload ให้ parse ไฟล์ฝั่ง browser คร่าว ๆ เพื่อให้ domainCount สมจริง)\n3) uploadBlocklist ส่ง body เป็น raw text ให้ตรงกับที่ backend รับใน T-08 (Content-Type text/plain + ?name=)",
    "acceptance": ["yarn build ผ่าน", "yarn lint ผ่าน", "โหมด mock เพิ่ม/ลบ/toggle ได้ และค่าคงอยู่หลัง reload"],
    "depends_on": ["T-10"]
  },
  {
    "task_id": "T-12",
    "title": "frontend: แท็บ Blocklists ในหน้า DNS Server",
    "layer": "frontend",
    "files": ["frontend/src/pages/DnsServer.tsx"],
    "instruction": "1) เพิ่ม 'blocklists' ลง VALID_TABS (DnsServer.tsx:83) และ TabsTrigger ใหม่ (บรรทัด 791-796) วางระหว่าง Blocked Domains กับ Settings\n2) TabsContent: Card + Table (shadcn เท่านั้น) คอลัมน์ Name | Source (badge url/upload + host ของ URL) | Domains (toLocaleString) | Last fetched | Status | Enabled (Switch) | Actions (Refresh/Edit/Delete) + แถบสรุปยอดรวมโดเมนเทียบกับ 500,000\n3) ปุ่ม Add: Drawer/Dialog 2 โหมด (Subscribe URL / Upload file) — โหมด URL บังคับ https:// และแจ้งชัดว่า URL ภายใน LAN ใช้ไม่ได้ (มาตรการความปลอดภัย); โหมด upload ใช้ <input type=file accept='.txt,.hosts,text/plain'> ตรวจขนาดฝั่ง browser ก่อนส่ง\n4) Alert ถาวรบนแท็บ: blocklists ทำงานแบบ sinkhole 0.0.0.0 เท่านั้น (เลือก NXDOMAIN ไม่ได้) และบล็อกเฉพาะชื่อที่อยู่ในไฟล์ ไม่ครอบ subdomain + ลิงก์ไปแท็บ Blocked Domains สำหรับเคสที่ต้องการ NXDOMAIN/subdomain\n5) หลัง create/refresh/toggle/delete ต้อง setIsApplied(false) เหมือน flow ของ blocked domains เดิม\n6) loading state ระหว่าง refresh/upload (ใช้เวลาหลายวินาที) และกันกดซ้ำ\n7) style: shadcn/ui เท่านั้น, สี semantic variable ห้าม hardcode palette, ห้าม shadow-*/backdrop-blur-*, รองรับ dark/light, ไม่มี Combobox ในฟอร์มนี้จึงใช้ Dialog แบบ default (ไม่ใส่ modal={false})",
    "acceptance": ["yarn build + yarn lint ผ่าน", "แท็บใหม่ใช้งานครบทุก action ในโหมด mock", "deep-link ?tab=blocklists เปิดตรงแท็บ"],
    "depends_on": ["T-11"]
  },
  {
    "task_id": "T-13",
    "title": "install.sh + เอกสาร subsystem",
    "layer": "service",
    "files": ["install.sh", "docs/ref/complete/dns-system-design.md", "README.md"],
    "instruction": "1) install.sh (ต่อจากบล็อก /var/lib/pigate บรรทัด 307-312): mkdir -p /var/lib/pigate/blocklists; chown pigate:netdev; chmod 755 — โค้ด Go ก็ MkdirAll เองได้ แต่ทำที่นี่เพื่อให้ ownership ถูกตั้งแต่แรกและเครื่องที่ติดตั้งไปแล้วได้ไดเรกทอรีตอนรัน install.sh ซ้ำ\n2) docs/ref/complete/dns-system-design.md: เพิ่มหัวข้อ Blocklists — addn-hosts, ทำไมต้อง render ไฟล์เอง (ทิ้ง IP ต้นฉบับ), ข้อจำกัด sinkhole-only/ไม่ครอบ subdomain, JSON manifest schema + เหตุผลที่ไม่ใช้ SQLite, การนับสถิติด้วย hash index, ความสัมพันธ์กับ deny-list เดิม\n3) README Feature Status: เพิ่มบรรทัด DNS Blocklists (Completed both)",
    "acceptance": ["bash -n install.sh ผ่าน", "เอกสารอัปเดตครบ"],
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
| GET | `/api/dns/blocklists` | `authRoute` | คืน metadata จาก manifest (ไม่มีรายชื่อโดเมน) |
| POST | `/api/dns/blocklists` | `superAdminRoute` | สร้างจาก URL + fetch ทันที |
| POST | `/api/dns/blocklists/upload` | `superAdminRoute` + อยู่ใน `bodyLimitExemptPaths` | อัปโหลดไฟล์ hosts |
| PUT | `/api/dns/blocklists/{id}` | `superAdminRoute` | แก้ชื่อ/URL/enabled |
| POST | `/api/dns/blocklists/{id}/refresh` | `superAdminRoute` | re-fetch (เฉพาะ sourceType=url) |
| POST | `/api/dns/blocklists/{id}/toggle` | `superAdminRoute` | เปิด/ปิด |
| DELETE | `/api/dns/blocklists/{id}` | `superAdminRoute` | ลบรายการ + ไฟล์ |
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
4. **path traversal**: `id` ประกอบเป็น path ของไฟล์ที่ dnsmasq โหลด → `validateBlocklistID`
   ในชั้น kernel เป็นด่านสุดท้าย ห้ามพึ่ง handler อย่างเดียว
5. **manifest.json คือ single point of failure ของฟีเจอร์นี้** (ไม่มี DB สำรอง) — ไฟล์เสีย =
   รายการหายหมด → กัน 3 ชั้น: atomic temp+rename (ไม่มีสถานะเขียนครึ่งทาง),
   quarantine ไฟล์เสียแล้วเริ่มใหม่แทนที่จะตายถาวร, และ backup พก manifest ไปด้วย (T-09)
   *ผลข้างเคียงที่ต้องยอมรับ:* ถ้า manifest หายแต่ไฟล์ `.hosts` ยังอยู่ ไฟล์เหล่านั้นจะกลายเป็น
   orphan → เพิ่ม log warning ตอน Load ว่าเจอไฟล์ที่ไม่มีใน manifest (ไม่ต้องลบอัตโนมัติ)
6. **race ของ manifest**: refresh สองคำขอพร้อมกันจะเขียนทับกันถ้าล็อกแค่ตอนเขียน →
   ต้องถือ write lock คลุม read-modify-write ทั้งก้อน และรัน `go test -race` (T-03)
7. **checksum ของ backup พัง** ถ้าฟิลด์ใหม่ใน `BackupConfig` ไม่ใส่ `omitempty` —
   backup เก่าทุกไฟล์จะ import ไม่ได้ทันที (comment เตือนไว้แล้วที่ `model/backup.go:87-92`) →
   ต้องมี regression test ด้วย backup เก่าจริง (T-09)
8. **body limit 1 MB**: ถ้าลืมเพิ่ม `/api/dns/blocklists/upload` ลง `bodyLimitExemptPaths`
   (`api/middleware.go:358`) upload 4.5 MB จะพังแบบสับสน
9. **สถิติต้องไม่โกหก**: `dnsBlocklistIndex` ต้อง exact-match เท่านั้น ถ้าเผลอไต่ parent domain
   แบบ deny-list สถิติจะรายงานว่าบล็อกทั้งที่ dnsmasq resolve ให้ปกติ (T-06 ข้อ 1)
   และ index ต้องถูกป้อนหลัง `ApplyZones` สำเร็จเท่านั้น (เหตุผลเดียวกับ `dns_block_index.go:20-27`)
10. **RAM ของ index สถิติ**: เลือก sorted `[]uint64` (8 B/โดเมน ≈ 4 MB ที่ 500k) แทน
    `map[string]string` (~30 MB) — ต้องวัดจริงแล้วบันทึกตัวเลขไว้ใน doc comment (T-06)
11. **RAM/CPU ของ dnsmasq เอง**: 93k host record ≈ 10-20 MB RSS + start ช้าขึ้นเล็กน้อย —
    คุมด้วย `DNSBlocklistMaxTotalDomains` และแสดงยอดรวมใน UI
    **ไม่ต้อง** ปรับ `cache-size` (ไม่ได้ตั้งไว้เลย = default 150) เพราะ hosts record เป็นโครงสร้างแยกจาก DNS cache
12. **ชนกับ local zone**: โดเมนใน blocklist ที่ตรงกับ zone ที่ enabled จะทำให้ชื่อในบ้านตอบ 0.0.0.0
    → parser ตัดชื่อที่ตรง/เป็น subdomain ของ zone ที่ enabled + `pigate.local` + hostname ออกตอน ingest
    **ข้อจำกัดที่รู้ตัว:** สร้าง zone ใหม่ *หลัง* ingest แล้วการกรองจะไม่ย้อนหลัง ต้องกด Refresh (บอกใน UI)
13. **restart dnsmasq = DHCP สะดุด** → ห้าม restart ตอน fetch/refresh; restart เฉพาะตอน Apply
14. **`expand-hosts` ใน base config** มีผลเฉพาะชื่อที่ไม่มีจุด — โดเมนใน blocklist มีจุดทุกตัว จึงไม่มีผลข้างเคียง
15. **ทดสอบบนบอร์ดจริงเสี่ยง lock ตัวเอง**: ถ้า list มีโดเมนที่ใช้เข้าหน้าเว็บ PiGate เอง อาจเข้าไม่ได้ →
    ทดสอบเฉพาะตอนเข้าถึงตัวเครื่องได้ และเตรียมทางถอย (ปิด list แล้ว Apply / ลบ
    `/etc/dnsmasq.d/pigate-dns.conf` + restart dnsmasq ด้วยมือ)

---

## 6. คำตอบของ owner (ปิดประเด็นแล้ว)

| # | ประเด็น | มติ |
|---|---|---|
| R1 | ที่เก็บ metadata | **JSON manifest** `/var/lib/pigate/blocklists/manifest.json` — ไม่ใช้ SQLite เลย (§2.3) |
| R2 | ข้อจำกัดของ `addn-hosts` | ยอมรับ sinkhole-only + ไม่ครอบ subdomain **และต้องเก็บสถิติได้** → §2.6 / T-06 |
| R3 | scheme | **https เท่านั้น** |
| R4 | URL ใน LAN | **ไม่อนุญาต** (ตัดสินโดย Tech Lead) — เป็นด่านกัน SSRF หลัก ถ้าจะเปิดต้องเป็น config key แยกพร้อมคำเตือนในภายหลัง |
| R5 | limit | 16 MB/ไฟล์, 8 lists, 300k/list, 500k รวม |
| R6 | auto-refresh | **manual refresh ก่อน** (ตัดสินโดย Tech Lead) — T-14 เป็น optional เฟสถัดไป ไม่อยู่ในรอบนี้ |
| R7 | สถิติ | ทำ (ย้ายจาก optional มาเป็น **T-06 บังคับ**) ออกแบบให้กิน RAM ~4 MB ที่ 500k โดเมน |
| R8 | route protection | **GET = `authRoute`, mutation ทุกตัว = `superAdminRoute` แบบ explicit** (ตัดสินโดย Tech Lead) — เพราะสั่งให้บอร์ดยิง HTTP ออกภายนอกและเขียนไฟล์หลาย MB จึงควรชัดในโค้ดแบบเดียวกับ reboot/config-export ไม่ใช่พึ่ง `RoleReadOnlyMiddleware` เท่านั้น |

---

## 7. Final Acceptance — เกณฑ์ทดสอบรวมท้ายแผน (ทดสอบครั้งเดียวหลัง T-01..T-13 เสร็จครบ)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... && go test ./... && go test -race ./internal/service/... ผ่านทั้งหมด",
    "cd frontend && yarn build && yarn lint ผ่านทั้งหมด",
    "รัน -mock=true: เพิ่ม blocklist จาก URL จริง (https://raw.githubusercontent.com/StevenBlack/hosts/master/hosts) สำเร็จ, domainCount ~90,000-95,000 และไม่มี localhost/broadcasthost/0.0.0.0 ปนอยู่",
    "รัน -mock=true: อัปโหลดไฟล์เดียวกันจาก browser ได้ domainCount เท่ากับวิธี subscribe",
    "unit test ยืนยันว่าไฟล์ที่ generate ใช้ 0.0.0.0 เสมอ แม้ input ระบุ IP อื่น (1.2.3.4 bank.example.com)",
    "unit test ยืนยันว่า https://127.0.0.1, https://192.168.1.1, https://169.254.169.254, https://[::1] ถูกปฏิเสธที่ชั้น dialer; http:// และ port อื่นถูก reject ตั้งแต่ validate; body เกิน 16 MB และ redirect เกิน 3 hop = error",
    "manifest: ลบ manifest.json แล้ว restart -> ฟีเจอร์ยังทำงาน (manifest ว่าง ไม่ crash); ทำ manifest.json ให้ JSON เสีย -> ถูก quarantine เป็น manifest.json.corrupt-<ts> และเริ่มใหม่; ตั้ง schemaVersion=99 -> ระบบ fail closed ไม่เขียนทับ และ UI แสดง error",
    "manifest: ยิง refresh พร้อมกันหลายคำขอแล้วรายการไม่หาย/ไม่ซ้ำ (go test -race ผ่าน)",
    "buildDNSConfig เมื่อไม่มี blocklist ให้ output byte-for-byte เท่าก่อนมีฟีเจอร์นี้ (golden test) และไม่ emit addn-hosts สำหรับ id ที่ไฟล์ไม่มีจริง",
    "toggle disable + Apply -> บรรทัด addn-hosts ของ list นั้นหายจาก pigate-dns.conf; ลบ list -> ไฟล์ .hosts ถูกลบ",
    "refresh ที่ fetch ล้มเหลว -> lastError ถูกบันทึก แต่ไฟล์เดิม/domainCount เดิมยังอยู่ (การบล็อกไม่หลุด)",
    "สถิติ: เปิด query logging แล้ว query โดเมนที่อยู่ใน blocklist -> หน้า Statistics > DNS > Blocked Query นับ hit และแสดงชื่อ list เป็น rule, mode = sinkhole",
    "สถิติ: query **subdomain** ของโดเมนที่อยู่ใน blocklist (ซึ่ง dnsmasq ไม่ได้บล็อก) ต้อง **ไม่** ถูกนับว่า blocked",
    "สถิติ: deny-list เดิมยังแสดง rule/mode ของตัวเองถูกต้อง และชนะ blocklist เมื่อแมตช์ทั้งคู่",
    "สถิติ: วัด RSS ของ pigate ก่อน/หลังโหลด blocklist 93k -> เพิ่มไม่เกิน ~10 MB และตัวเลขจริงถูกบันทึกใน doc comment",
    "deny-list เดิม (Blocked Domains tab, cap 1000) ยังทำงานครบทั้ง nxdomain/sinkhole",
    "backup: export -> import กลับ ได้ metadata ครบ; list แบบ upload ได้ไฟล์กลับมาใช้งานได้ทันที (sha256 ตรง); list แบบ url ขึ้น needs refresh แล้ว Refresh กลับมาใช้ได้",
    "backup: ไฟล์ backup เก่าที่ export ก่อนฟีเจอร์นี้ยัง import ได้ (checksum ไม่พัง) — regression test",
    "-disable-edit=true: ทุก mutation ของ /api/dns/blocklists ถูกบล็อก; role admin (ไม่ใช่ super_admin) เรียก GET ได้แต่ mutation ไม่ได้",
    "docs/openapi.yaml กับ frontend/public/openapi.yaml diff ว่าง",
    "UI: แท็บ Blocklists รองรับ dark/light, ไม่มี shadow-*/backdrop-blur-*, ไม่มี hardcode palette, deep-link ?tab=blocklists ทำงาน, มีคำเตือน sinkhole-only + no-subdomain + https-only/no-LAN ครบ",
    "[บนบอร์ดจริงเท่านั้น] apply blocklist 93k โดเมน -> dnsmasq start ปกติ, DHCP ยังแจก lease ได้, dig ads.doubleclick.net ตอบ 0.0.0.0, dig โดเมนปกติยังตอบถูก, restart dnsmasq ไม่เกิน ~2 วินาที, RSS ของ dnsmasq เพิ่มไม่เกิน ~40 MB",
    "[บนบอร์ดจริงเท่านั้น] ยืนยันพฤติกรรมของ addn-hosts ที่ชี้ไฟล์หาย (dnsmasq ตาย หรือแค่ warning) แล้วบันทึกผลลงข้อควรระวังข้อ 1"
  ]
}
```
