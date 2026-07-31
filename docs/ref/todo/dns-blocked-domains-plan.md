# DNS Server: Block Domain Query (deny-list tab) — แผนงาน issue #110

> เพิ่มแท็บใหม่ "Blocked Domains" ในหน้า DNS Server สำหรับจัดการ deny-list ของโดเมนที่ต้องการบล็อก
> โดยมีตารางฐานข้อมูลของตัวเอง (ไม่ยัดใส่ `dns_zones`) และ generate directive ของ dnsmasq เพิ่มเข้าไปใน
> `pigate-dns.conf` แบบ additive — ของเดิม (Zones & Records, Settings/upstream separation จาก #109) ต้องไม่พัง
>
> เขียนเมื่อ: 2026-07-31 · Reference branch: `main` (งานจริงต้องอยู่บน `feat/dns-blocked-domains`)
> README Feature Status: DNS Server (local zones) = Completed → คงเดิม (เพิ่ม capability ย่อย)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย:** ผู้ใช้เปิดหน้า DNS Server → แท็บ "Blocked Domains" → กด Add แล้วกรอกโดเมน (เช่น `ads.example.com`)
เลือกโหมดบล็อก → บันทึก → กด "Apply DNS Zones" → dnsmasq ตอบ NXDOMAIN (หรือ 0.0.0.0) ให้โดเมนนั้นและ
subdomain ทั้งหมด โดย zone/record เดิมและ upstream resolver เดิมทำงานเหมือนเดิมทุกประการ

**เงื่อนไขทางเทคนิคที่ต้องเป็นจริง:**
- เมื่อ deny-list ว่าง ผลลัพธ์ของ `buildDNSConfig` ต้อง **byte-for-byte เหมือนเดิม** (กันเครื่องที่ติดตั้งอยู่แล้ว regress)
- ค่าที่ผู้ใช้กรอกถูก whitelist-validate ก่อนถูกเขียนลงไฟล์ config (dnsmasq เป็น directive-per-line → newline injection)
- ทุก path ที่เขียน DB ได้ (handler, backup import) ต้องผ่าน validator ตัวเดียวกัน

**นอกขอบเขต (Out of scope):**
- ไม่ย้าย/ลบ empty-zone workaround เดิมโดยอัตโนมัติ — ฟีเจอร์นี้ **additive อย่างเดียว** (เหตุผลใน §2)
- ไม่ทำ bulk import จากไฟล์ blocklist สาธารณะ (hosts/AdBlock list), ไม่ทำ scheduled update, ไม่ทำ per-client policy
- ไม่ทำสถิติ "โดเมนที่ถูกบล็อกบ่อยสุด" (ต่อยอดจาก statistics DNS ภายหลังได้)
- ไม่แตะ System DNS (systemd-resolved) — บล็อกมีผลกับ client ที่ใช้ dnsmasq ของ PiGate เป็น resolver เท่านั้น

## 1. สภาพปัจจุบัน (สำรวจโค้ดจริง ณ 2026-07-31)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Frontend page + Tabs (`zones` / `settings`) | มีแล้ว 2 แท็บ | `frontend/src/pages/DnsServer.tsx:654-658`, TabsContent zones `:660`, settings `:916` |
| Frontend API client | มีแล้ว (zones/records/settings/apply/clearCache) | `frontend/src/services/dnsServerService.ts:44-323` |
| Frontend types + mock storage | มีแล้ว (localStorage `pigate_dns_zones`) | `frontend/src/data-mockup/mockData.ts:655-736`, service `:4-42` |
| Route + middleware | มีแล้ว ใช้ `authRoute` ทั้งหมด | `backend/internal/api/router.go:137-150` |
| Handler zones/records/settings | มีแล้ว | `backend/internal/api/handlers.go:2949-3289` |
| Service layer | `DNSServerService.ApplyAll()` อ่าน zones+settings แล้วเรียก kernel | `backend/internal/service/dns_server.go:29-59` |
| Kernel interface | `DNSServerManager.ApplyZones(zones, interfaces, upstreamServers, queryLog)` | `backend/internal/kernel/interfaces.go:154-170` |
| Kernel real | `buildDNSConfig` เป็น pure function เขียน `local=/zone/`, `server=/zone/fwd`, `no-resolv` | `backend/internal/kernel/dns_server.go:36-266` |
| Kernel mock | `MockDNSServerManager.ApplyZones` นับ ApplyCount | `backend/internal/kernel/mock.go:387-404` |
| DB schema | `dns_zones` / `dns_records` / `dns_server_settings` — **ยังไม่มีตาราง deny-list** | `backend/internal/db/connection.go:336-362` |
| DB migration pattern | ALTER แบบตรวจ `sqlite_master` | `connection.go:509-548` |
| Repository | CRUD zones/records/settings | `backend/internal/db/repository.go:2521-2761` |
| Validation | `ValidateDNSZone` / `ValidateDNSRecord` / `reZoneName` | `backend/internal/model/dns_validate.go:27-152` |
| Backup/Restore | export `DnsZones`+`DnsServerSettings`, wipe+restore, validate | `service/backup.go:109-116, 700-709, 732-752`, `db/backup_repo.go:99-100, 223-296` |
| openapi | มี `/dns/*` + schema DNSZone/DNSServerSettings | `docs/openapi.yaml:2132-2380, 4707-4900` (+ `frontend/public/openapi.yaml` ต้อง sync) |

**สรุป:** ทุก layer มีรูปแบบพร้อมให้ทำตามหมดแล้ว งานจริงคือ "เพิ่มตารางใหม่ + ทางเดินของข้อมูลชุดใหม่ตั้งแต่ DB → kernel generator → UI" ไม่มีอะไรต้องรื้อ

## 2. แนวทางทางเทคนิค

**directive ที่เลือก** — ต่อ 1 entry ที่ `enabled = 1`:

| mode | directive ที่ emit | ผลลัพธ์ |
|---|---|---|
| `nxdomain` (ค่าเริ่มต้น) | `server=/<domain>/` | dnsmasq ตอบ NXDOMAIN ให้โดเมนนั้น + subdomain ทั้งหมด ไม่ forward ไป upstream |
| `sinkhole` | `address=/<domain>/0.0.0.0` และ `address=/<domain>/::` | ตอบ 0.0.0.0 / :: (เหมาะกับ ad-block ที่ browser ไม่ชอบ NXDOMAIN) |

เหตุผลที่เลือก `server=/<domain>/` เป็นค่าเริ่มต้น: มัน**คือ directive เดียวกับที่ empty-zone workaround เดิมสร้างอยู่แล้ว**
(`local=/zone/` ที่ `dns_server.go:206` — `local` เป็น synonym ของ `server`) พฤติกรรมจึงเหมือนที่ผู้ใช้คุ้นเคยเป๊ะ
ๆ และไม่ต้องเดาว่า dnsmasq เวอร์ชันบนเครื่องรองรับรูปแบบ `address=/domain/` แบบไม่มี IP หรือไม่

**ทางเลือกที่พิจารณาแล้วไม่เอา:**
- `address=/<domain>/` (ไม่ใส่ IP) — พฤติกรรมต่างกันไปตามเวอร์ชัน dnsmasq, เสี่ยงกว่าโดยไม่ได้อะไรเพิ่ม
- ใช้ `--conf-file` แยกไฟล์ `pigate-blocklist.conf` — ต้องจัดการ ordering/ลบไฟล์เพิ่มอีกไฟล์ ทั้งที่ generator เดิมเป็น
  pure function เขียนไฟล์เดียวอยู่แล้ว; แยกไฟล์ค่อยทำตอนรองรับ blocklist ขนาดหลักแสนรายการ
- บล็อกที่ชั้น firewall (nftables) — บล็อกได้แค่ IP ไม่ใช่ชื่อโดเมน และไปแตะ 4-section input chain โดยไม่จำเป็น

**ตำแหน่งใน config:** เขียนเป็นบล็อกใหม่ **ท้ายไฟล์** หลัง zone ทั้งหมด และ emit เฉพาะเมื่อมี entry ที่ enabled อย่างน้อย 1 รายการ
→ ถ้า deny-list ว่าง ผลลัพธ์เท่าเดิม byte-for-byte (dnsmasq ไม่สนใจลำดับของ directive กลุ่มนี้ ใช้กฎ most-specific domain match)

**การชนกับ zone เดิม:** dnsmasq จับคู่โดเมนแบบ specific-ที่สุดชนะ ดังนั้น block `ads.example.com` ยังชนะ zone `example.com` ได้
แต่ถ้าชื่อ**ตรงกันเป๊ะ**กับ zone ที่ enabled อยู่ → ไม่นิยาม พฤติกรรมกำกวม จึง (ก) handler ปฏิเสธ 400 ตอน create/update
และ (ข) generator ข้าม entry นั้นพร้อม log warning (defense-in-depth แบบเดียวกับที่ทำกับ zone/record ที่ invalid)

**Migration path ของ empty-zone workaround:** ไม่ย้ายอัตโนมัติ เพราะ zone ที่ไม่มี record อาจถูกสร้างด้วยเจตนาอื่น
(local zone ที่กำลังจะใส่ record) การลบทิ้งให้ผู้ใช้ = data loss ที่ย้อนไม่ได้ → ฟีเจอร์ใหม่เป็น additive ล้วน และให้ UI
แสดงคำแนะนำแบบ static ว่า "ถ้าเคยสร้าง zone เปล่าเพื่อบล็อก ให้ย้ายมาที่แท็บนี้แล้วลบ zone เปล่าเองได้"

**Template ที่ให้ทำตาม:** เดิน pattern ของ upstream-resolver feature ทั้งชุด — `docs/ref/complete/dns-server-settings-tab-and-upstream-plan.md`
(model → migration → repo → kernel → service → handler → backup → openapi → frontend)

## 3. ขั้นตอน (เรียงตาม dependency: inner layer ก่อน)

> ทำครบทุก Task ก่อน แล้วค่อยทดสอบรวมทีเดียวตาม §6

### T-01 · model: struct + constants + validator
- **layer:** model · **files:** `backend/internal/model/dns_server.go`, `backend/internal/model/dns_validate.go`
- **instruction:** เพิ่ม `BlockedDomain{ID, Domain, Mode, Enabled, Comment string, CreatedAt string}` และ
  `BlockedDomainInput{Domain, Mode, Enabled, Comment}` (json camelCase) ใน `dns_server.go`;
  ใน `dns_validate.go` เพิ่ม const `DNSBlockModeNXDomain = "nxdomain"`, `DNSBlockModeSinkhole = "sinkhole"`,
  `DNSBlockedDomainsMax = 1000`, `DNSBlockedCommentMax = 128` และฟังก์ชัน `ValidateBlockedDomain(b BlockedDomain) error`
  ที่: ไม่ trim (reject ถ้ามี whitespace ขอบ), ต้อง match `reZoneName` ที่มีอยู่แล้ว, ยาว ≤ 253, ต้องมีอย่างน้อย 1 จุด,
  ห้ามขึ้น/ลงท้ายด้วย `.` หรือ `-`, mode ต้องเป็น 1 ใน 2 ค่า (ว่าง = nxdomain), comment ห้ามมี `\n`/`\r` และยาว ≤ 128
- **acceptance:** `go build ./...` ผ่าน; ฟังก์ชัน/const ถูก export ครบตามชื่อข้างต้น; ไม่แก้ validator ตัวอื่น
- **depends_on:** —

### T-02 · db: ตารางใหม่ + repository CRUD
- **layer:** db · **files:** `backend/internal/db/connection.go`, `backend/internal/db/repository.go`
- **instruction:** ใน `connection.go` เพิ่ม `CREATE TABLE IF NOT EXISTS dns_blocked_domains (id TEXT PRIMARY KEY,
  domain TEXT NOT NULL UNIQUE COLLATE NOCASE, mode TEXT NOT NULL DEFAULT 'nxdomain' CHECK(mode IN ('nxdomain','sinkhole')),
  enabled INTEGER NOT NULL DEFAULT 1 CHECK(enabled IN (0,1)), comment TEXT NOT NULL DEFAULT '',
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP)` ต่อท้ายกลุ่ม DNS (~`connection.go:362`) — เป็นตารางใหม่ล้วน
  จึง **ไม่ต้องเขียน ALTER migration** และไม่ต้อง seed row ใด ๆ;
  ใน `repository.go` (ต่อท้ายส่วน DNS SERVER ~`:2761`) เพิ่ม `GetBlockedDomains() ([]model.BlockedDomain, error)`
  (เรียงตาม domain ASC), `GetBlockedDomainByID`, `CountBlockedDomains() (int, error)`,
  `CreateBlockedDomain`, `UpdateBlockedDomain`, `DeleteBlockedDomain`, `ToggleBlockedDomain` — ใช้ prepared arg เท่านั้น
- **acceptance:** `go build ./...` ผ่าน; ทุก query ใช้ `?` placeholder; ไม่มีการแก้ตาราง `dns_zones`/`dns_records`
- **depends_on:** T-01

### T-03 · kernel: interface + real generator + mock
- **layer:** kernel · **files:** `backend/internal/kernel/interfaces.go`, `backend/internal/kernel/dns_server.go`, `backend/internal/kernel/mock.go`
- **instruction:** เปลี่ยน signature เป็น `ApplyZones(zones []model.DNSZone, interfaces []string, upstreamServers []string, queryLog bool, blocked []model.BlockedDomain) error`
  (ต่อท้ายพารามิเตอร์ ตาม precedent ที่ feature ก่อน ๆ ทำมา) พร้อมอัปเดต doc comment;
  ใน `buildDNSConfig` (pure function) เพิ่มบล็อกสุดท้ายของไฟล์: วน `blocked`, ข้ามตัวที่ `!Enabled`,
  ข้ามตัวที่ `model.ValidateBlockedDomain` ไม่ผ่าน (log warning — defense-in-depth ตัวสุดท้ายเหมือน zone/record),
  ข้ามตัวที่ชื่อ**ตรงกับ** zone ที่ enabled ใน `zones` (log warning), แล้ว emit ตามตาราง §2;
  เขียน header comment `# Blocked domains (deny-list)` **เฉพาะเมื่อมีอย่างน้อย 1 บรรทัดที่จะ emit จริง**
  (คำนวณรายการที่ผ่าน filter ก่อน แล้วค่อยเช็ค len — pattern เดียวกับ `validUpstreams` ที่ `dns_server.go:151-174`);
  แก้ `MockDNSServerManager.ApplyZones` ให้รับพารามิเตอร์ใหม่ + log จำนวน blocked (ห้ามแตะไฟล์ระบบใด ๆ)
- **acceptance:** `go build ./...` ผ่านทั้ง real และ mock; ถ้า `blocked` ว่างหรือทุกตัวถูกข้าม ผลลัพธ์ string เท่ากับก่อนแก้ทุกตัวอักษร
- **depends_on:** T-01
- **sensitive:** ✅ ค่าจากผู้ใช้ถูก interpolate ลง config file — ต้อง review เข้ม

### T-04 · service: ส่ง deny-list เข้า kernel
- **layer:** service · **files:** `backend/internal/service/dns_server.go`
- **instruction:** ใน `ApplyAll()` (~`:29`) อ่าน `s.repo.GetBlockedDomains()` (error → คืน error แบบเดียวกับ zones)
  แล้วส่งเป็นพารามิเตอร์ที่ 5 ของ `s.manager.ApplyZones(...)`; ไม่ต้องสร้าง service ใหม่ ไม่ต้องแก้ `main.go`
  (boot path เรียก `InitApplyConfig()` → `ApplyAll()` อยู่แล้ว)
- **acceptance:** `go build ./...` ผ่าน; ไม่มีการเพิ่ม field ใน struct `DNSServerService`
- **depends_on:** T-02, T-03

### T-05 · api: handlers + routes
- **layer:** api · **files:** `backend/internal/api/handlers.go`, `backend/internal/api/router.go`
- **instruction:** เพิ่ม handler ต่อท้ายส่วน DNS SERVER (~`handlers.go:3176`):
  `HandleGetBlockedDomains` (คืน `[]` ไม่ใช่ null), `HandleCreateBlockedDomain` (gen id ด้วย `randomID("blk-")`,
  normalize domain เป็น lower-case + ตัด trailing dot **ก่อน** validate, เช็ค `CountBlockedDomains() >= model.DNSBlockedDomainsMax` → 400,
  ชนกับชื่อ zone ที่มีอยู่ → 400 พร้อมข้อความอธิบาย, UNIQUE ชน → 400 "domain นี้มีอยู่แล้ว"),
  `HandleUpdateBlockedDomain`, `HandleDeleteBlockedDomain`, `HandleToggleBlockedDomain`;
  ทุกตัว `s.logEvent(r, model.EventCategoryDns, "dns.blocked_domain_{created,updated,deleted,toggled}", ...)`;
  **CRUD เขียน DB อย่างเดียว ไม่เรียก ApplyAll** (ให้ผู้ใช้กด Apply DNS Zones เอง เหมือน zone/record — กัน dnsmasq restart รัว ๆ);
  ใน `router.go` (~`:146`) เพิ่ม `authRoute` 5 เส้นตาม §4
- **acceptance:** `go build ./...` ผ่าน; ใช้ `authRoute` (ไม่ใช่ `superAdminRoute`); ไม่มี handler ไหนเรียก `dnsServerService.ApplyAll()`
- **depends_on:** T-02
- **sensitive:** ✅ input validation boundary

### T-06 · backup/restore
- **layer:** db+service+model · **files:** `backend/internal/model/backup.go`, `backend/internal/service/backup.go`, `backend/internal/db/backup_repo.go`
- **instruction:** ใน `BackupConfig` เพิ่ม `BlockedDomains []BlockedDomain \`json:"blockedDomains,omitempty"\`` —
  **ต้องมี `omitempty`** ด้วยเหตุผลเดียวกับ `PortForwards`/`Presets` (`backup.go:70-86`): importer verify checksum ด้วยการ
  re-marshal struct ถ้าไม่ใส่ omitempty ไฟล์ backup เก่าจะกลายเป็น `"blockedDomains":null` → checksum mismatch → import พังทั้งไฟล์;
  export ใน `service/backup.go` (~`:109`); `validateConfig` (~`:700`) วน `model.ValidateBlockedDomain` แบบ fail-closed;
  `configCounts` (~`:737`) เพิ่มคีย์ `blockedDomains`; ใน `backup_repo.go` เพิ่ม `"DELETE FROM dns_blocked_domains"` ในลิสต์ wipe (~`:99`)
  และ INSERT ใน transaction ถัดจาก DNS zones (~`:243`) พร้อม clamp mode ที่ไม่รู้จักเป็น `nxdomain`
- **acceptance:** `go build ./...` ผ่าน; field เป็น `omitempty`; ไม่ bump `CurrentBackupSchemaVersion`
- **depends_on:** T-01, T-02

### T-07 · backend tests
- **layer:** backend tests · **files:** `backend/internal/kernel/dns_server_test.go`, `backend/internal/model/dns_validate_test.go`, `backend/internal/api/dns_validation_test.go`
- **instruction:** เพิ่มเทสต์: (1) `buildDNSConfig` — deny-list ว่าง → output ไม่มีคำว่า `Blocked` และเท่ากับ baseline เดิม,
  nxdomain → มี `server=/ads.example.com/`, sinkhole → มีทั้ง `0.0.0.0` และ `::`, entry ที่ `enabled=false` ไม่ถูก emit,
  domain ที่มี `\n` ฝังอยู่ต้องไม่ผลิต directive แปลกปลอมบรรทัดใหม่, entry ที่ชนชื่อ zone ที่ enabled ถูกข้าม;
  (2) `ValidateBlockedDomain` — เคสผ่าน/ไม่ผ่าน รวม newline/space/`#`/โดเมนไม่มีจุด/ยาวเกิน;
  (3) handler — POST domain ที่มี newline → 400 และ DB ไม่มีแถวใหม่ (ทำตามสไตล์ `dns_validation_test.go:36-49`)
- **acceptance:** `cd backend && go test ./...` ผ่านทั้งหมด (รวมเทสต์เดิมที่ signature เปลี่ยน)
- **depends_on:** T-03, T-05

### T-08 · openapi (2 ไฟล์)
- **layer:** docs · **files:** `docs/openapi.yaml`, `frontend/public/openapi.yaml`
- **instruction:** เพิ่ม path `/dns/blocked-domains`, `/dns/blocked-domains/{id}`, `/dns/blocked-domains/{id}/toggle`
  (~ต่อจาก `/dns/records/{id}` บรรทัด 2281-2328) และ schema `BlockedDomain`/`BlockedDomainInput` (~ต่อจาก 4841)
  พร้อมเพิ่ม `blockedDomains` ในตัวอย่าง BackupConfig ถ้ามี; **สองไฟล์ต้องเนื้อหาตรงกัน**
- **acceptance:** ทั้งสองไฟล์ parse เป็น YAML ได้และมี path/schema ครบเหมือนกัน
- **depends_on:** T-05

### T-09 · frontend: types + API client + mock mode
- **layer:** frontend · **files:** `frontend/src/data-mockup/mockData.ts`, `frontend/src/services/dnsServerService.ts`
- **instruction:** เพิ่ม `export interface BlockedDomain { id, domain, mode: "nxdomain" | "sinkhole", enabled, comment }`,
  `initialBlockedDomains: BlockedDomain[] = []`, const `DNS_BLOCKED_DOMAINS_MAX = 1000` (ต้องตรงกับ backend);
  ใน `dnsServerService.ts` เพิ่ม `getBlockedDomains/createBlockedDomain/updateBlockedDomain/deleteBlockedDomain/toggleBlockedDomain`
  ตาม pattern เดิมทุกประการ รวมสาขา `IS_MOCK_MODE` ที่เก็บใน localStorage key `pigate_dns_blocked_domains`
- **acceptance:** `cd frontend && yarn build` ผ่าน; mock branch ไม่ยิง network เลย
- **depends_on:** T-08

### T-10 · frontend: แท็บ Blocked Domains
- **layer:** frontend · **files:** `frontend/src/pages/DnsServer.tsx`
- **instruction:** เพิ่ม `<TabsTrigger value="blocked">Blocked Domains</TabsTrigger>` (~`:657`) และ `<TabsContent value="blocked">`
  ใหม่ (วางระหว่าง zones กับ settings): Card + ช่องค้นหา + ปุ่ม Add + `<Table>` คอลัมน์ Domain / Mode (Badge) /
  Comment / Enabled (`<Switch>`) / actions (Edit, Delete พร้อม `confirm`); ฟอร์ม add/edit ใช้ `<Drawer direction="right">`
  แบบเดียวกับ Zone modal (`:1200`) — ไม่มี Combobox จึงไม่ต้องใช้ `modal={false}`; ทุก mutation ต้อง `setIsApplied(false)`
  เพื่อให้ปุ่ม "Apply DNS Zones" เด้ง; โหลดข้อมูลใน `initialLoad` Promise.all เดิม (~`:139`);
  ใส่ Alert/Info note อธิบาย (ก) บล็อกครอบ subdomain ทั้งหมดโดยอัตโนมัติ (ข) ต่างของ nxdomain vs sinkhole
  (ค) ถ้าเคยสร้าง "zone เปล่า" เพื่อบล็อก ให้ย้ายมาที่นี่แล้วลบ zone เปล่าได้เอง;
  ใช้เฉพาะ shadcn/ui + semantic color variables, flat design, รองรับ dark/light
- **acceptance:** `yarn build` + `yarn lint` ผ่าน; ไม่มี `shadow-*`/`backdrop-blur-*`/สีดิบ (`text-emerald-*` ฯลฯ)
- **depends_on:** T-09

### T-11 · เอกสาร
- **layer:** docs · **files:** `README.md`, `docs/ref/complete/dns-system-design.md`
- **instruction:** เพิ่มบรรทัดสั้น ๆ ใน Feature Status ว่า DNS Server รองรับ deny-list แล้ว และเพิ่มหัวข้อย่อย
  "Blocked Domains" ใน design doc อธิบาย schema + directive mapping ตาม §2
- **acceptance:** ข้อความตรงกับสิ่งที่ implement จริง ไม่มีการอ้าง `#N` เพื่ออ้างหัวข้อในเอกสาร
- **depends_on:** T-10

### T-12 (optional, ทำท้ายสุด) · ตัวช่วยแนะนำ empty-zone เดิม
- **layer:** frontend · **files:** `frontend/src/pages/DnsServer.tsx`
- **instruction:** ในแท็บ Blocked Domains แสดงรายการ zone ที่ `isAuthoritative && enabled && records.length === 0`
  เป็นข้อความแนะนำอ่านอย่างเดียว ("โซนเหล่านี้อาจถูกสร้างไว้เพื่อบล็อก — พิจารณาย้ายมาที่แท็บนี้")
  **ห้ามมีปุ่มแปลง/ลบอัตโนมัติ** (ความเสี่ยง data loss)
- **acceptance:** เป็น read-only ล้วน ไม่มี mutation ใด ๆ
- **depends_on:** T-10

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใหม่/เดิม | Role | พฤติกรรม |
|---|---|---|---|---|
| GET | `/api/dns/blocked-domains` | ใหม่ | authRoute (ทุก role อ่านได้) | คืน list เรียงตาม domain |
| POST | `/api/dns/blocked-domains` | ใหม่ | authRoute (non-super_admin ถูก `RoleReadOnlyMiddleware` บล็อก) | สร้าง entry, เขียน DB อย่างเดียว |
| PUT | `/api/dns/blocked-domains/{id}` | ใหม่ | เหมือน POST | แก้ไข domain/mode/comment |
| DELETE | `/api/dns/blocked-domains/{id}` | ใหม่ | เหมือน POST | ลบ |
| POST | `/api/dns/blocked-domains/{id}/toggle` | ใหม่ | เหมือน POST | สลับ enabled |
| POST | `/api/dns/apply` | เดิม | เดิม | เขียน `pigate-dns.conf` + restart dnsmasq (ตอนนี้รวม deny-list ด้วย) |

`-disable-edit=true` บล็อก mutation ทั้งหมดผ่าน `DisableEditMiddleware` อยู่แล้ว — ถูกต้องตามที่ควรเป็นสำหรับฟีเจอร์นี้

## 5. ข้อควรระวัง (Cautions)

1. **Config injection** — `domain`/`comment` ถูก interpolate ลง `pigate-dns.conf` ตรง ๆ; newline 1 ตัวใน domain =
   inject directive ได้ทั้งบรรทัด และ `dnsmasq --test` จับไม่ได้ (บรรทัดที่ inject เป็น config ที่ valid)
   **ป้องกัน:** validate 3 ชั้นเหมือนของเดิม — handler (400), backup import (fail-closed), generator (ข้าม+log)
2. **Byte-for-byte regression ของเครื่องที่ติดตั้งอยู่แล้ว** — ถ้า emit header/บรรทัดว่างของ section ใหม่ทั้งที่ deny-list ว่าง
   config ของทุกเครื่องจะเปลี่ยน (แม้พฤติกรรมเท่าเดิม) ทำให้ diff/verify ตอน debug เสียเวลา
   **ป้องกัน:** filter ก่อน แล้วค่อยเช็ค `len()>0` ค่อยเขียน header + มี unit test ยืนยัน
3. **บล็อกโดเมนของตัวเอง = ตัดขาบ้านตัวเอง** — ถ้าผู้ใช้บล็อก `pigate.local` (ค่า `domain=` ใน `pigate-base.conf:44`)
   หรือชื่อ zone ที่ใช้งานอยู่ ชื่อเครื่องภายในบ้านจะ resolve ไม่ได้ทั้งหมด
   **ป้องกัน:** handler ปฏิเสธชื่อที่ตรงกับ zone ที่ enabled และ generator ข้ามให้อีกชั้น; UI เตือนว่าบล็อกครอบ subdomain
4. **บล็อกครอบ subdomain โดยอัตโนมัติ (ไม่มีโหมด exact-only)** — `server=/example.com/` บล็อก `www.example.com` ด้วย
   ผู้ใช้ที่คิดว่าบล็อกเฉพาะชื่อเดียวจะงง **ป้องกัน:** เขียนไว้ชัดใน UI note + openapi description
5. **Checksum ของ backup เก่า** — เพิ่ม field ใน `BackupConfig` โดยลืม `omitempty` ทำให้ไฟล์ backup เดิม import ไม่ได้
   ทั้งไฟล์ (checksum mismatch) — ดู `model/backup.go:70-79` **ป้องกัน:** `omitempty` + เทสต์ import ไฟล์เก่า
6. **dnsmasq restart กระเทือน DHCP ด้วย** — dnsmasq instance เดียวทำทั้ง DNS/DHCP; ถ้า CRUD เรียก `ApplyAll` ทุกครั้ง
   การเพิ่ม 20 โดเมนจะ restart 20 ครั้ง = LAN สะดุดยาว **ป้องกัน:** CRUD เขียน DB อย่างเดียว, apply ครั้งเดียวตอนกดปุ่ม
7. **จำนวน entry ไม่จำกัด** — deny-list หลักหมื่นทำให้ไฟล์ config ใหญ่, `dnsmasq --test` ช้า, UI table ค้าง
   **ป้องกัน:** cap `DNSBlockedDomainsMax = 1000` ตรวจที่ handler (bulk blocklist เป็นงานเฟสถัดไปที่ต้องแยกไฟล์ config)
8. **signature ของ `ApplyZones` ยาวขึ้นเป็น 5 พารามิเตอร์** (bool คั่นกลางระหว่าง slice) — เรียกผิดตำแหน่งแล้ว compile ผ่านยาก
   แต่ก็อ่านยากขึ้นเรื่อย ๆ **หมายเหตุ:** ยอมรับในรอบนี้เพื่อ diff เล็ก; ถ้ารอบหน้ามีพารามิเตอร์เพิ่มอีก ให้ refactor เป็น
   struct `model.DNSServerRenderInput` แยกเป็น PR ของตัวเอง
9. **ไม่ต้องแตะ netlink monitor / main.go / install.sh** — ฟีเจอร์นี้ไม่ยุ่งกับ routing/interface, ไม่ต้องการสิทธิ์ใหม่
   (เขียนไฟล์ `/etc/dnsmasq.d/` + restart dnsmasq ผ่าน D-Bus มี Polkit rule อยู่แล้ว)
10. **ทดสอบบนบอร์ดจริง** ต้องทำตอนมี access ทางอื่นนอกจากผ่าน PiGate เอง และ **ห้ามใช้โดเมนของ UI/SSH ตัวเอง**
    เป็นตัวทดลองบล็อก; ใช้โดเมนสาธารณะที่ไม่กระทบ เช่น `ads-test.example.com`

## 6. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ทดสอบครั้งเดียวหลังทุก Task เสร็จ)

- [ ] `cd backend && go build ./... && go test ./...` ผ่านทั้งหมด
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] Mock mode (`-mock=true -allow-dev-cors`): เพิ่ม/แก้/ลบ/toggle blocked domain ได้ครบ, ปุ่ม Apply DNS Zones เด้งทุกครั้งที่แก้,
      log ของ MockDNSServer แสดงจำนวน blocked ถูกต้อง, และ frontend mock mode (ไม่มี backend) ก็ CRUD ได้จาก localStorage
- [ ] Unit test ยืนยัน: deny-list ว่าง → `buildDNSConfig` output เท่าเดิมทุกตัวอักษร
- [ ] nxdomain entry → config มี `server=/<domain>/` เพียงบรรทัดเดียว; sinkhole → มีทั้ง `address=/<d>/0.0.0.0` และ `address=/<d>/::`
- [ ] entry ที่ `enabled=false` และ entry ที่ชนชื่อ zone ที่ enabled ไม่ปรากฏใน config
- [ ] POST domain ที่มี `\n`, ช่องว่าง, `#`, ไม่มีจุด, ยาวเกิน 253 → 400 ทุกเคส และ DB ไม่มีแถวใหม่
- [ ] POST domain ซ้ำ (ต่างตัวพิมพ์ใหญ่เล็ก) → 400 ไม่สร้างซ้ำ; เกิน 1000 รายการ → 400
- [ ] Export backup → มี key `blockedDomains`; import ไฟล์นั้นกลับได้; **import ไฟล์ backup เก่า (ที่ไม่มี key นี้) ยังผ่าน checksum**
- [ ] Import ไฟล์ที่ถูกแก้ให้ blockedDomains มี newline ฝัง → ปฏิเสธทั้งไฟล์ ไม่มีการเขียน DB
- [ ] Role: user ที่ไม่ใช่ super_admin เรียก GET ได้ แต่ POST/PUT/DELETE/toggle ถูกบล็อก; `-disable-edit=true` บล็อก mutation ทั้งหมด
- [ ] Zones & Records เดิมและ Settings/upstream (system/custom) ยังทำงานครบ ไม่มี regression
- [ ] บนบอร์ดจริง: กด Apply แล้ว `dnsmasq --test` ผ่าน, `dig ads-test.example.com @<pigate-ip>` ได้ NXDOMAIN (หรือ 0.0.0.0 ตามโหมด),
      ขณะที่ `dig <host>.pigate.local @<pigate-ip>` และการ resolve เว็บทั่วไปยังทำงานปกติ และ DHCP ยังจ่าย lease ได้
- [ ] `docs/openapi.yaml` กับ `frontend/public/openapi.yaml` เนื้อหาตรงกัน
