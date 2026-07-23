# DHCP Domain Option — เพิ่มการแจก Domain name (DHCP option 15) ต่อ scope

> แผนงานสำหรับ issue #83: เพิ่มฟิลด์ **Domain** ให้ DHCP Server scope หนึ่ง ๆ
> เพื่อให้ dnsmasq แจก DHCP option 15 (domain name) ไปยัง client — ปัจจุบัน
> ฟอร์มมีแค่ IP Range / Subnet / Gateway / Lease Time / DNS Servers
>
> เขียนเมื่อ: 2026-07-22 · Reference branch: `main` (งานจริงทำบน `feat/dhcp-domain-option`)
> README Feature Status: DHCP Server (dnsmasq) = Completed → คงเดิม (เพิ่มฟิลด์ในฟีเจอร์ที่เสร็จแล้ว)

## 0. Goal และ Scope

- **Goal (ผู้ใช้เห็นอะไร):** ในหน้า DHCP Server เมื่อสร้าง/แก้ไข scope จะมีช่อง
  "Domain" (optional) ผู้ใช้กรอกเช่น `home.lan` แล้ว dnsmasq จะแจก option 15
  ให้ client ในซับเน็ตนั้น (client ใช้เป็น domain search / FQDN suffix) ค่าว่าง =
  ปิดฟีเจอร์ (ไม่ emit directive) ค่าถูก persist ลง DB และรอดผ่าน backup/restore
- **เงื่อนไขทางเทคนิค:** ค่า Domain ถูกเขียน verbatim ลง `pigate-dhcp.conf` จึงต้อง
  ผ่าน validation แบบ whitelist (กัน newline/space injection) เหมือน field DHCP อื่น
- **Out of scope:**
  - ไม่ทำ per-reservation domain / per-host FQDN (option 15 ระดับ scope พอ)
  - ไม่ยุ่งกับ `domain=` แบบ global ของ dnsmasq (ซึ่งไปผูกกับ DNS Server local
    resolution) — ใช้ `dhcp-option=<iface>,15,<domain>` ต่อ scope ให้สอดคล้อง
    กับ pattern ที่มีอยู่ (gateway=opt3, dns=opt6)
  - ไม่แตะ DHCP Client (dhcpcd) — issue นี้เกี่ยวกับฝั่ง Server เท่านั้น

## 1. Current State (สำรวจโค้ด ณ วันเขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Model `DhcpConfig` struct | มี 10 field, ไม่มี domain | `backend/internal/model/types.go:262` |
| Validation `ValidateDhcpConfig` | validate iface + start/end/gw/dns1/dns2 (reject verbatim) | `backend/internal/model/dns_validate.go:182` |
| Regex domain-shaped ที่ reuse ได้ | `reZoneName = ^[a-zA-Z0-9.-]+$` (ใช้กับ DNS zone) | `backend/internal/model/dns_validate.go:30` |
| DB schema `dhcp_configs` (CREATE) | ไม่มี column domain | `backend/internal/db/connection.go:311` |
| DB migration pattern (ADD COLUMN idempotent) | ใช้ `SELECT sql FROM sqlite_master ... strings.Contains` | `backend/internal/db/connection.go:157-227` |
| DB seed default row | ไม่มี domain (จะ default '') | `backend/internal/db/connection.go:868` |
| Repository CRUD (6 statement มี column list ตรง ๆ) | ต้องเติม domain ทุกจุด | `backend/internal/db/repository.go:1339,1356,1363,1384,1407,1419` |
| Kernel real `ApplyConfig` (emit opt 3/6) | จุดที่ต้อง emit opt 15 | `backend/internal/kernel/dhcp_server.go:79-89` |
| Kernel mock `ApplyConfig` | เรียก `ValidateDhcpConfig` เท่านั้น ไม่ emit config | `backend/internal/kernel/mock.go:198` |
| API handler create/update | decode **ทั้ง struct** (`json.Decode(&cfg)`) → ไม่ผ่าน merge whitelist | `backend/internal/api/handlers.go:1817,1839` |
| Backup/restore | ใช้ `[]DhcpConfig` ตรง ๆ | `backend/internal/model/backup.go:61` |
| Frontend TS type `DhcpConfig` | ไม่มี domain | `frontend/src/data-mockup/mockData.ts:476` |
| Frontend form (state + save + dialog + card) | ไม่มี domain | `frontend/src/pages/DhcpServer.tsx:112-114,225-234,263-326,955-1002,590-607` |
| Frontend service `dhcpService.ts` | ส่งทั้ง object ผ่าน `JSON.stringify(config)` | `frontend/src/services/dhcpService.ts:113,136` |
| OpenAPI schema `DhcpConfig` | ไม่มี domain (2 ไฟล์) | `docs/openapi.yaml:4198`, `frontend/public/openapi.yaml` |

**สรุป:** งานกระจายบาง ๆ ทุกชั้นแต่ตรงไปตรงมา จุดที่พลาดง่ายคือ (ก) repository มี 6
statement ที่ระบุ column list ด้วยมือ ต้องแก้ให้ครบ ไม่งั้น scan/insert เพี้ยน และ
(ข) **whitelist gotcha ของ interface PATCH ไม่ applicable ที่นี่** — DHCP handler
decode ทั้ง struct จึงเพิ่ม field ใน model แล้วรับค่าอัตโนมัติ (ระบุไว้กัน developer
ไปเติม whitelist ผิดที่)

## 2. Technical Approach

- **กลไก emit:** ต่อท้ายบล็อก opt 3/opt 6 ใน `RealDhcpManager.ApplyConfig` ด้วย
  ```go
  if cfg.Domain != "" {
      sb.WriteString(fmt.Sprintf("dhcp-option=%s,15,%s\n", cfg.Interface, cfg.Domain))
  }
  ```
  เลือกรูปแบบนี้เพราะ **ตรงกับ pattern เดิมเป๊ะ** (opt 3 gateway, opt 6 dns ใช้
  `dhcp-option=<iface>,<n>,<value>`) → interface-scoped เหมือนกัน, ผ่าน
  `dnsmasq --test` ด้วยกลไกเดียวกัน
- **ทางเลือกที่ปฏิเสธ:** `domain=<name>,<subnet>` ระดับ global — ปฏิเสธเพราะไปผูก
  กับ local DNS resolution ของ dnsmasq (นอก scope), และไม่ per-interface ตาม
  โครงสร้าง config ปัจจุบัน
- **Validation:** เพิ่ม branch ใน `ValidateDhcpConfig` — Domain optional (ว่าง = ผ่าน),
  เมื่อมีค่าใช้ `reZoneName.MatchString` (reuse regex เดิม, full-match anchored จึง
  reject `\n`/`\r`/space โดยธรรมชาติ) + cap ความยาว `<= 253` (RFC 1035) ปฏิเสธ
  ค่าที่ **ไม่ trim** (interpolate verbatim) — เหมือน field IP ที่มีอยู่
- **Template ที่ยึด:** ตาม `ValidateDhcpConfig` เดิม (loop field) และ style การ emit
  opt 3/6 ใน `dhcp_server.go`

## 3. Steps (เรียงจากชั้นในออกนอก)

### T-01 — Model: เพิ่ม field Domain
**File:** `backend/internal/model/types.go` (~262, struct `DhcpConfig`)
เพิ่ม `Domain string \`json:"domain"\`` ต่อท้าย (ก่อน/หลัง DNS2 ก็ได้ ให้ JSON key = `domain`)

### T-02 — Validation
**File:** `backend/internal/model/dns_validate.go` (`ValidateDhcpConfig` ~182)
หลัง loop field IP เพิ่ม:
```go
if cfg.Domain != "" {
    if len(cfg.Domain) > 253 {
        return fmt.Errorf("dhcp domain %q exceeds 253 characters", cfg.Domain)
    }
    if !reZoneName.MatchString(cfg.Domain) {
        return fmt.Errorf("dhcp domain %q contains invalid characters (allowed: letters, digits, '.', '-')", cfg.Domain)
    }
}
```
> ไม่ต้อง trim ก่อนเช็ค — เจตนาให้ค่ามี edge whitespace แล้ว fail (เขียน verbatim ลง config)

### T-03 — DB schema + migration + repository
**File:** `backend/internal/db/connection.go`
- CREATE TABLE `dhcp_configs` (~311): เพิ่มบรรทัด `domain TEXT NOT NULL DEFAULT '',`
- เพิ่ม migration idempotent (ตาม pattern บรรทัด 157-227) ก่อน/หลังบล็อก create:
  ```go
  var sqlDhcpDomain string
  err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='dhcp_configs'").Scan(&sqlDhcpDomain)
  if err == nil && !strings.Contains(sqlDhcpDomain, "domain") {
      if _, err = db.Exec("ALTER TABLE dhcp_configs ADD COLUMN domain TEXT NOT NULL DEFAULT ''"); err != nil {
          return fmt.Errorf("failed to add domain column: %w", err)
      }
  }
  ```
- seed default (~868) ไม่ต้องแก้ (default '' พอ)

**File:** `backend/internal/db/repository.go` — เติม `domain` ให้ครบ **6 จุด**:
- `GetDHCPConfig` SELECT+Scan (~1339,1342)
- `UpdateDHCPConfig` UPDATE (~1355-1358)
- `GetDHCPConfigs` SELECT+Scan (~1363,1373)
- `GetDHCPConfigByInterface` SELECT+Scan (~1384,1387)
- `CreateDHCPConfig` INSERT column list + VALUES + args (~1406-1409) — เพิ่ม `?` ให้ครบ
- `UpdateDHCPConfigByID` UPDATE (~1418-1421)
> ระวัง: จำนวน `?` ใน INSERT ต้องตรงกับ column ที่เพิ่ม — ตรวจนับ

### T-04 — Kernel real emit (sensitive: เขียน config file)
**File:** `backend/internal/kernel/dhcp_server.go` (~89 หลังบล็อก DNS opt 6)
เพิ่มการ emit `dhcp-option=<iface>,15,<domain>` เมื่อ `cfg.Domain != ""`
> `ValidateDhcpConfig` ถูกเรียกอยู่แล้วบรรทัด 58 (defense-in-depth) ครอบ Domain ให้
> อัตโนมัติเมื่อ T-02 เสร็จ — ไม่ต้องเช็คซ้ำในไฟล์นี้ นอกจาก guard `!= ""`

### T-05 — Kernel mock
**File:** `backend/internal/kernel/mock.go` (`MockDhcp.ApplyConfig` ~198)
> **ไม่ต้องแก้โค้ด** — mock เรียก `ValidateDhcpConfig` อยู่แล้วและไม่ emit config
> จริง เมื่อ T-02 เสร็จ mock จะ validate Domain ให้เอง ระบุไว้กัน over-build

### T-06 — API handler
**File:** `backend/internal/api/handlers.go` (create ~1817 / update ~1839)
> **ไม่ต้องแก้** — decode ทั้ง struct + เรียก `ValidateDhcpConfig` แล้ว จึงรับ+validate
> `domain` อัตโนมัติ **ไม่มี merge whitelist ให้ไปเติม** (whitelist gotcha เป็นของ
> interface PATCH/PUT เท่านั้น) — ระบุชัดเพื่อไม่ให้ developer หลง

### T-07 — Frontend type + form + service
**File:** `frontend/src/data-mockup/mockData.ts`
- interface `DhcpConfig` (~476): เพิ่ม `domain: string`
- `initialDhcpConfig` / `initialDhcpConfigs`: เพิ่ม `domain: ""` ทุก object (เลี่ยง TS error)

**File:** `frontend/src/pages/DhcpServer.tsx`
- state: เพิ่ม `const [formDomain, setFormDomain] = useState("")` (~114)
- `openCreateConfigModal` (~215): `setFormDomain("")`
- `openEditConfigModal` (~232): `setFormDomain(cfg.domain || "")`
- `handleSaveConfig` (~289): เพิ่ม validation ฝั่ง UI — ถ้า `formDomain` ไม่ว่างและไม่ match
  `^[a-zA-Z0-9.-]+$` → `setConfigError("Domain ไม่ถูกต้อง")` (สร้าง `isValidDomain`
  ใน `frontend/src/lib/utils.ts` คู่กับ `isValidIp`)
- payload (~316): เพิ่ม `domain: formDomain.trim()`
- dialog: เพิ่ม `<Input>` ช่อง Domain ต่อจากบล็อก DNS/Lease (~985-1002) — ใช้ shadcn
  `<Label>/<Input>` เดิม, semantic color, flat, ระบุ optional
- card list (~590-607): แสดง `{cfg.domain || "—"}`

**File:** `frontend/src/services/dhcpService.ts`
> **ไม่ต้องแก้** — ส่งทั้ง object `JSON.stringify(config)` อยู่แล้ว

### T-08 — Docs / OpenAPI
- `docs/openapi.yaml` (~4210 properties ของ `DhcpConfig`): เพิ่ม `domain` (type string,
  optional, example `home.lan`) — **ไม่ใส่ใน required**
- `frontend/public/openapi.yaml`: sync เนื้อหาเดียวกัน
- `docs/ref/dhcp-service-design.md`: เพิ่มหมายเหตุว่า scope รองรับ option 15 (domain)

### T-09 — Tests
**File:** `backend/internal/model/dns_validate_test.go`
เพิ่ม case ให้ `ValidateDhcpConfig`: domain ว่าง (ผ่าน), domain ปกติ `home.lan` (ผ่าน),
domain มี `\n`/space (fail), domain ยาว >253 (fail)

## 4. Related API

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| POST | `/dhcp/configs` | super_admin (มี) | รับ `domain` เพิ่มใน body — route เดิม ไม่เปลี่ยน |
| PUT | `/dhcp/configs/{id}` | super_admin (มี) | อัปเดต `domain` — route เดิม |
| GET | `/dhcp/configs`, `/dhcp/config` | authRoute (มี) | ตอบ `domain` เพิ่ม |
| POST | `/dhcp/apply` | super_admin (มี) | ทริกเกอร์ emit opt 15 |

`-disable-edit=true` block mutation ทั้งหมดผ่าน `DisableEditMiddleware` อยู่แล้ว — ถูกต้อง

## 5. Cautions

1. **Repository column-list ไม่ครบ → data corruption:** repository ระบุ column ด้วยมือ
   6 จุด ถ้าเติม `domain` ใน SELECT แต่ลืม Scan (หรือกลับกัน) จะ scan ค่าเลื่อน/ error
   ทุก request ป้องกันด้วยการแก้ทั้งคู่พร้อมกันต่อ statement + `go test ./internal/db/...`
2. **Injection ผ่าน config file:** Domain เขียน verbatim ลง `pigate-dhcp.conf` — ถ้า
   validation หลุด (เช่นเผลอ trim แล้ว accept) ค่า `foo\ndhcp-option=...` จะ inject
   directive `dnsmasq --test` จับไม่ได้เพราะบรรทัดที่ inject valid เอง → กันด้วย
   `reZoneName` full-match (T-02) ซึ่ง reject newline โดยธรรมชาติ **เป็นงาน sensitive
   ต้อง review เข้ม** (แตะ dnsmasq config generation)
3. **`dnsmasq --test` fail = ทั้ง scope ตกทั้งไฟล์:** ถ้า emit opt 15 ผิดรูป การ apply
   จะ fail ทั้งไฟล์ (ไม่ใช่แค่ scope เดียว) → ยึด format `dhcp-option=<iface>,15,<v>`
   ตาม opt 3/6 เป๊ะ และเทสต์ apply จริงหนึ่งรอบ
4. **Migration ของเครื่องที่ติดตั้งแล้ว:** เครื่องเก่ามี `dhcp_configs` โดยไม่มี column
   → migration idempotent (T-03) เติมให้ default `''` ไม่กระทบ row เดิม, ไม่ต้อง re-run
   `install.sh` (ไม่แตะ Polkit/sudoers)
5. **Backup/restore:** `[]DhcpConfig` เป็น struct ตรง จึงรวม `domain` อัตโนมัติเมื่อ T-01
   เสร็จ — restore config เก่า (ไม่มี key domain) จะได้ `""` ถูกต้อง ไม่ต้องบั๊ม schema version
6. **Netlink monitor / boot-apply:** ไม่กระทบ — DHCP apply ที่ boot ผ่าน
   `DhcpServerService.InitApplyConfig` อยู่แล้ว จะ emit opt 15 ให้เองเมื่อ T-04 เสร็จ
7. **Frontend mock mode:** `dhcpService` เก็บลง localStorage เป็น object ตรง จึงรองรับ
   `domain` อัตโนมัติ — แค่ต้องมี `domain: ""` ใน `initialDhcpConfig(s)` (T-07) กัน
   TS/undefined

## 6. Summary Checklist (Definition of Done)

- [ ] T-01 model `DhcpConfig.Domain`
- [ ] T-02 `ValidateDhcpConfig` รองรับ Domain (empty ok / charset / len<=253)
- [ ] T-03 CREATE TABLE + migration ADD COLUMN + repository 6 statements
- [ ] T-04 real emit `dhcp-option=<iface>,15,<domain>`
- [ ] T-05 mock — ยืนยันว่าไม่ต้องแก้ (validate ครอบให้แล้ว)
- [ ] T-06 handler — ยืนยันว่าไม่ต้องแก้ (ไม่มี whitelist)
- [ ] T-07 frontend type + state + form + card + `isValidDomain` util
- [ ] T-08 openapi (2 ไฟล์) + dhcp-service-design.md
- [ ] T-09 unit test validation
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน

---

## Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก T เสร็จ)

```json
{
  "final_acceptance": [
    "go build ./... และ go test ./... ผ่านทั้งหมด; yarn build + yarn lint ผ่าน",
    "รัน -mock=true: สร้าง scope ใหม่พร้อม Domain 'home.lan' → save/reload หน้าเว็บแล้วค่า domain ยังอยู่ (persist ลง DB), แก้ scope ลบ domain เป็นค่าว่าง → save ได้",
    "รัน -mock=true: กรอก domain ที่มี space หรือ newline หรือยาว >253 → ฟอร์มขึ้น error และ backend ตอบ 400 (ValidateDhcpConfig reject)",
    "โหมด real (บนบอร์ดทดสอบ/มี dnsmasq): เปิด scope ที่มี Domain แล้วกด Apply → /etc/dnsmasq.d/pigate-dhcp.conf มีบรรทัด 'dhcp-option=<iface>,15,home.lan', ผ่าน dnsmasq --test, dnsmasq reload สำเร็จ; client ที่ขอ lease ได้รับ DHCP option 15 = home.lan",
    "scope ที่ Domain ว่าง → ไฟล์ config ไม่มีบรรทัด option 15 (ไม่ regress พฤติกรรมเดิม)",
    "Backup แล้ว Restore config → ค่า domain กลับมาครบ; restore ไฟล์ backup เก่า (ไม่มี key domain) ได้ค่า '' ไม่ error",
    "openapi.yaml ทั้งสองไฟล์มี property domain ตรงกัน และหน้า ApiDocs แสดงถูกต้อง"
  ]
}
```
