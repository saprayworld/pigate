# Multi-Value Address & Service Objects — Work Plan

โจทย์จากเจ้าของโปรเจกต์: ปัจจุบัน **Address Object** และ **Service Object** 1 ชื่อ ผูกกับค่าได้เพียง **1 ค่า** เท่านั้น
(1 subnet/range/fqdn หรือ 1 protocol+port) ต้องการให้ 1 ชื่อเก็บได้ **หลายค่า** เช่น
`Office_Servers` = `192.168.10.0/24` + `10.0.0.5/32` + `172.16.5.100-172.16.5.120`,
`Web_Ports` = `TCP/80` + `TCP/443` + `TCP/8000-8010` + `UDP/443`
(เทียบเท่า address group / service group ของไฟร์วอลล์ทั่วไป)

สถานะ: **เสร็จแล้ว** — implement ครบ T-00A ถึง T-14 บน branch `feat/multi-value-address-service-objects`,
ai-qa ตรวจ Final Acceptance ผ่านทั้งหมดในรอบแรก (build/test/lint ของทั้ง backend และ frontend ผ่านหมด,
ทดสอบ end-to-end ผ่าน mock server ครบทุกข้อใน Final Acceptance รวมถึง idempotent InitDB, migration จาก
DB เดิม, การสร้าง/แก้ไข object หลายรายการผ่าน UI, การขยาย policy เป็นหลายกฎด้วย rule id เดียวกัน,
สถิติ per-rule รวมเป็นบรรทัดเดียว, matched-endpoint labeling, และ config key `max-object-entries`/
`max-expanded-rules-per-policy` ทำงานตาม two-tier validation) — มีเพียงหมายเหตุความเสี่ยงเล็กน้อยว่า
cap guard ใน `kernel/real_firewall.go` ไม่มี automated test เพราะต้องใช้ CAP_NET_ADMIN จริง (ไม่ใช่
regression ใหม่ เป็นข้อจำกัดเดิมของโค้ดเบส) ไม่ block การปิดงาน
(แผนนี้แก้ไฟล์ที่ PR #137 เป็นคนสร้าง/แก้ ได้แก่ `service/policy_endpoint_labels.go`, `service/policy_endpoints.go`,
`service/traffic_stats.go` ⇒ ถ้าเริ่มก่อน merge จะชน conflict แน่นอน)

ทุก task ทำบน branch ใหม่ `feat/multi-value-address-service-objects` แตกจาก `main` (หลัง #137 merge) และเข้า PR เท่านั้น
— **ห้าม push โค้ดขึ้น main, ห้าม commit เว้นแต่เจ้าของสั่ง**

---

## 1. สภาพปัจจุบัน (ข้อเท็จจริงจากโค้ด ไม่ใช่การเดา)

### 1.1 DB (source of truth)

`backend/internal/db/connection.go:240-256` — โครงตารางปัจจุบัน (ค่าเดียวต่อ 1 แถว):

```sql
CREATE TABLE IF NOT EXISTS address_objects (
    id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('subnet','range','fqdn')),
    value TEXT NOT NULL,
    system INTEGER DEFAULT 0 CHECK(system IN (0,1)), comment TEXT );

CREATE TABLE IF NOT EXISTS service_objects (
    id TEXT PRIMARY KEY, name TEXT UNIQUE NOT NULL,
    protocol TEXT NOT NULL CHECK(protocol IN ('TCP','UDP','TCP/UDP','ICMP')),
    port TEXT NOT NULL,
    type TEXT NOT NULL CHECK(type IN ('system','custom')), comment TEXT );
```

- ตารางเชื่อม `policy_addresses(policy_id, address_id, association_type)` และ `policy_services(policy_id, service_id)`
  อ้าง **address_id/service_id** (`ON DELETE RESTRICT`) ⇒ **policy อ้าง "ชื่อ object" ไม่ใช่ค่า** ⇒ การเพิ่มค่าใน object
  ไม่กระทบตาราง policy เลย (นี่คือเหตุผลว่าทำไม backward compatibility ของ policy ถึงได้มาฟรี — ดู Design decision 6)
- **รูปแบบ migration ของโปรเจกต์นี้ไม่ใช่ versioned migration**: `InitDB` รัน `CREATE TABLE IF NOT EXISTS` ทุกรอบ
  แล้วเติมคอลัมน์ใหม่ด้วยการอ่าน `SELECT sql FROM sqlite_master ... ` + `strings.Contains(...)` + `ALTER TABLE ADD COLUMN`
  (ตัวอย่าง `connection.go:181-227` metric / vlan_parent / vlan_id / prefer_5ghz) ⇒ **งานนี้ต้องเดินตาม pattern เดิม**
- Seed ค่าเริ่มต้น (`connection.go:911-940`): `address_objects` มี `addr-1 = ALL (subnet 0.0.0.0/0, system=1)`;
  `service_objects` มี `svc-1..svc-6 = ALL/HTTP/HTTPS/SSH/DNS/ICMP (type='system')` — object พวกนี้ **แก้/ลบไม่ได้**

### 1.2 Model / DTO

`backend/internal/model/types.go:69-103` — `AddressObject{ID,Name,Type,Value,System,RefPolicies}`,
`AddressObjectInput{Name,Type,Value,Comment}`, `ServiceObject{ID,Name,Protocol,Port,Type,RefPolicies}`,
`ServiceObjectInput{Name,Protocol,Port,Comment}`
`PolicyRule.Source/Destination/Service` เป็น `[]string` ของ **ชื่อ object** อยู่แล้ว (types.go:126-155)

### 1.3 Repository

`backend/internal/db/repository.go`
- `GetAddresses/GetAddressByID` (185-253), `CreateAddress/UpdateAddress/DeleteAddress/BulkDeleteAddresses` (316-416),
  `AddressNameExists` (418)
- `validateAddressObject` (276-314) — ตรวจ subnet ด้วย `net.ParseCIDR`, range ด้วย `START-END` + family match, fqdn ด้วย `isValidFQDN`
- `GetServices/GetServiceByID` (428-492), `CreateService/UpdateService/DeleteService` (546-594), `ServiceNameExists` (596)
- `validateServiceObject` (503-544) — ICMP ต้อง port `-`; อื่น ๆ เป็นเลขเดี่ยวหรือช่วง `start-end` ที่ 1..65535
- `savePolicyRelations` (854-902) map ชื่อ → id (มี fallback ตัดคำแรกของชื่อ service เช่น `"HTTP (TCP 80)"`)
- ลบ object ที่ถูกอ้างอิงไม่ได้ (`policy_addresses/policy_services` COUNT > 0)
- `NewRepository(db *sql.DB)` (28) + มี pattern setter หลังสร้างอยู่แล้ว: `SetMockMode` (38), `SetAllowEditSystemRoutes` (51)
  ⇒ ใช้ pattern เดียวกันสำหรับ config ใหม่ (ห้ามเพิ่มพารามิเตอร์ constructor)

### 1.4 Kernel / nftables (จุดสำคัญที่สุด)

`backend/internal/kernel/real_firewall.go`
- **ไม่มีการใช้ nftables named set / anonymous set เลยในโปรเจกต์นี้** (ยืนยันด้วย grep: ไม่พบ `expr.Lookup`, `AddSet`,
  `SetElements`, `nftables.Set{`) — ปัจจุบันใช้ payload+cmp ตรง ๆ
- `NewRealFirewall(dockerCompat bool)` (81) — constructor ตัวเดียว
- `addUserChainRules` (1093-1179) ทำ **cartesian expansion อยู่แล้ว**: `for src → for dest → for svc → for proto`
  แล้วเรียก `buildRuleExpressions` ⇒ 1 policy rule ใน DB กลายเป็นหลาย nft rule ได้อยู่แล้ววันนี้
- ทุก nft rule ที่แตกออกมาถูก **แท็กด้วย `UserData = ruleID` เดียวกัน** (1114) และ log prefix ฝัง rule token เดียวกัน (1154)
- `buildIPMatchExpressions(name, addrsMap, offset)` (929-1047) — เปิด `addrsMap[name]` แล้วอ่าน `addr.Type`/`addr.Value`
  **ค่าเดียว**; subnet /32 ใช้ Cmp เท่ากับ, subnet อื่นใช้ Bitwise+Cmp, range ใช้ Gte+Lte, fqdn ใช้ `net.LookupIP` แล้ว
  **match แค่ IP ตัวแรก**
- `resolveService(name, svcsMap)` (916-927) และการ match protocol/port อยู่ใน `buildRuleExpressions` (1181-1329)
  อ่าน `svc.Protocol`/`svc.Port` **ค่าเดียว** (`TCP/UDP` แตกเป็น 2 protocol ที่ 1136-1140)
- โครงสร้าง 4 section ของ chain (sanity → audit log → dynamic accept → final drop+log) อยู่คนละที่กับ
  `addUserChainRules` ⇒ **แผนนี้แตะเฉพาะ section 3 เท่านั้น ไม่แตะลำดับ section**
- `real_traffic_account.go:181-191` `accumulateRuleCounters` **บวกสะสม counter ของทุก nft rule ที่มี UserData เดียวกัน**
  ⇒ การแตกกฎเพิ่มขึ้นไม่ทำให้สถิติ per-rule เพี้ยน (ยืนยันแล้ว)
- `kernel/mock.go:33-34` `ApplyRules(rules, addrs, svcs, ...)` แค่เก็บ input ไว้ ไม่ parse ค่า address/service

### 1.5 Service layer

- `service/firewall.go:60` `NewFirewallService(repo, firewall, ifaceService)`; `197-204` ดึง `repo.GetAddresses()/GetServices()`
  ส่งเข้า kernel; `297-350` เป็น pass-through CRUD
- `service/traffic_stats.go:1131-1156` `buildCategoryEntries(svcs)` สร้าง lookup table จาก **1 (protocol, port) ต่อ 1 service**
  ใช้โดย `categorize()` (เลือกช่วงพอร์ตแคบสุด, tie-break ด้วยชื่อ) + cache TTL 60s
  (บรรทัดเลื่อนจาก PR #137 ที่แทรก `ServiceNameFor` ไว้ก่อนหน้าที่ 1053 — ใช้ชื่อฟังก์ชันค้นหาแทนเลขบรรทัดตายตัวถ้าไม่ตรง)
- **จาก PR #137** (ยังไม่ merge): `service/policy_endpoint_labels.go` มี `newAddrMatcher([]model.AddressObject)` ที่อ่าน
  `addr.Type`/`addr.Value` เฉพาะ `subnet`/`range` (ข้าม fqdn) แปลงเป็นช่วง IP แล้ว `Match(ip)` คืน **ชื่อ object ที่ช่วงแคบที่สุด**
  ที่ครอบ IP นั้น (tie-break ด้วยชื่อ), และ `service/policy_endpoints.go` ใช้ `trafficStats.ServiceNameFor(proto, port)`
  + `serviceNameReferencedByRule(name, rule.Service)` (match ด้วย **ชื่อ** ⇒ ไม่กระทบ)

### 1.6 Config (`backend/internal/config`)

- pure package: `Parse`/`Resolve`/`Write`, ลำดับความสำคัญ **code default < config file < CLI flag ที่ส่งจริง**
- มี pattern "คีย์ file-only ที่เป็น tuning knob" ครบชุดแล้ว (เช่น `deny-stats-max-sources`, `deny-stats-max-ports`,
  `traffic-log-buffer-capacity`) ขั้นตอนของ pattern: field ใน `Config` → ค่าใน `Defaults()` → const `key…` →
  const `min…/max…` → ต่อท้าย `orderedKeys` → `applyKey` (แปลงชนิดอย่างเดียว) → range-sanity ใน `Resolve`
  (**clamp + warning ไม่ fatal**) → `keyValue` สำหรับ `Write` → เพิ่มใน `pigate.conf.example` + นับจำนวนคีย์ใน `README.md`
- `install.sh` เขียนเฉพาะ 4 คีย์ production (`mock`, `db`, `https-port`, `docker-compat`) ⇒ **ไม่ต้องแก้ `install.sh`**

### 1.7 API + Backup

- `api/handlers.go:1998-2105` (address) และ `2111-2181` (service) map `Input → model` ตรง ๆ field ต่อ field
  (เลื่อนจาก PR #137 ~+55/+60 บรรทัด — ค้นด้วยชื่อฟังก์ชัน `HandleCreateAddress`/`HandleCreateService` ถ้าไม่ตรง)
- `docs/openapi.yaml` (+ สำเนา `frontend/public/openapi.yaml`) มี schema `AddressObject` (7157), `AddressObjectInput` (7189)
  และ `ServiceObject` (7279), `ServiceObjectInput` (7312) — **ห้ามแก้ `backend/internal/api/dist/openapi.yaml`** (build artifact)
- Backup: `model/backup.go:58-59` เก็บ `[]AddressObject`/`[]ServiceObject` ตรง ๆ;
  `db/backup_repo.go:93-134` wipe แล้ว INSERT กลับ (ข้าม system/predefined);
  `service/backup.go:650-687` `validateConfig` ตรวจว่า policy อ้าง object ที่มีในไฟล์;
  **`service/backup.go:772-779` `configChecksum` = sha256 ของ JSON ของ `BackupConfig` และ `decodeBackup` (582-591)
  ถือ checksum mismatch เป็น error ร้ายแรง** ⇒ ดู Caution 1 (กับดักสำคัญที่สุดของงานนี้)

### 1.8 Frontend

- `src/data-mockup/mockData.ts:151-158, 213-220` — type `AddressObject`/`ServiceObject` (ค่าเดียว) + mock seed
- `src/pages/Addresses.tsx` — ฟอร์มใน Drawer มี `formType` (select) + `formValue` (Input เดี่ยว), validate ด้วย
  `isValidCidr`/`isValidIpRange`/regex FQDN, ตาราง 1 คอลัมน์ "Details / Value", stat cards นับ subnet/range/fqdn, filter ตาม type
- `src/pages/Services.tsx` — `formProto` (select) + `formPort` (Input เดี่ยว), filter TCP/UDP/TCP-UDP/ICMP
- `src/services/addressService.ts`, `src/services/serviceObjectService.ts` — มี mock mode เก็บใน localStorage
  ⇒ **ข้อมูลเก่าใน localStorage ของ browser ก็ต้อง migrate ตอนอ่าน**
- `src/services/mockSync.ts:33-37` mark refPolicies โดย match ทั้ง `addr.name` และ `addr.value`
- `src/components/policy/PolicyChainPage.tsx:423-445` ใช้เฉพาะ **ชื่อ** object เป็น option ⇒ แทบไม่ต้องแก้

---

## 2. ข้อตัดสินจากเจ้าของโปรเจกต์ (ตอบครบแล้ว — ปิดประเด็น)

| # | ประเด็น | คำตอบที่ยืนยันแล้ว | ผลต่อแผน |
|---|---|---|---|
| D-1 | type/protocol อยู่ระดับ object หรือระดับรายการย่อย | **(ก) ระดับรายการย่อย** — 1 object ปน `subnet` + `range` (หรือ TCP + UDP คนละพอร์ต) ในชื่อเดียวได้ | ตารางลูกมีคอลัมน์ `type` / `protocol` ของตัวเอง; badge บนตารางแสดง `Mixed` เมื่อปนกัน; ตัวกรอง = "มีรายการชนิดนั้นอย่างน้อย 1" (T-00/T-02/T-03/T-06/T-12/T-13) |
| D-2 | วิธี generate nftables | **(ก) แตกเป็นหลาย nft rule (cartesian expansion แบบเดิม)** — ไม่ใช้ named/anonymous set | T-04 ขยาย loop เดิมเท่านั้น ห้ามแตะโครง 4 section; ทางเลือก set เก็บไว้เป็นแผนสำรองถ้า QA เจอปัญหาจำนวนกฎจริง |
| D-3 | เพดานจำนวนรายการ/จำนวนกฎ | **ค่าเริ่มต้น 64 รายการ/object และ 4096 nft rule/policy rule ยืนยันตามที่เสนอ แต่ต้อง "ปรับได้ผ่าน config file" ไม่ใช่ const hardcode** | เพิ่ม **2 config key ใหม่** ใน `internal/config` (ดู §2.1) แล้ว thread ค่าจาก `main.go` → repository/kernel; ค่า default เมื่อไม่ตั้งในไฟล์ = 64 / 4096 เท่าเดิม (T-00A ใหม่, T-00/T-03/T-04 ปรับตาม) |
| D-4 | คอลัมน์เดิม `address_objects.type/value`, `service_objects.protocol/port` | **เก็บไว้ก่อนแต่ถือเป็น compat layer "ชั่วคราว" ที่จะถูก deprecate/ลบใน major version ถัดไป** (ไม่ใช่ mirror ถาวร) | ต้องมีหมายเหตุ deprecation ชัดเจนใน (ก) comment ตรง migration/CREATE TABLE, (ข) comment บนฟิลด์ใน `model/types.go`, (ค) `docs/data/firewall.md`, (ง) `description: deprecated` ใน openapi ทั้งสองไฟล์ — พร้อมเหตุผล (SQLite รุ่นเก่าไม่รองรับ `DROP COLUMN` และคอลัมน์เป็น `NOT NULL` + มี CHECK ⇒ ลบทันทีเสี่ยง, และเก็บไว้เพื่อให้ downgrade binary กลับได้ในช่วงเปลี่ยนผ่าน) (T-01/T-02/T-10/T-14) |

### 2.1 Config key ใหม่ 2 คีย์ (ตาม D-3 — ใช้อ้างอิงร่วมกันทุก task)

| หัวข้อ | คีย์ที่ 1 | คีย์ที่ 2 |
|---|---|---|
| ชื่อคีย์ | `max-object-entries` | `max-expanded-rules-per-policy` |
| Field ใน `config.Config` | `MaxObjectEntries int` | `MaxExpandedRulesPerPolicy int` |
| Default (เมื่อไม่ตั้งในไฟล์) | **64** | **4096** |
| ประเภท | **file-only** (ไม่ลงทะเบียน `flag.Int` ใน `main.go`) เหมือน `deny-stats-max-*` / `traffic-log-buffer-capacity` | เช่นเดียวกัน |
| ช่วงที่ยอมรับ | `1 – 512` (`minMaxObjectEntries`/`maxMaxObjectEntries`) | `64 – 65536` (`minMaxExpandedRulesPerPolicy`/`maxMaxExpandedRulesPerPolicy`) |
| เหตุผลของช่วง | 1 = อย่างน้อยต้องมีค่าได้ 1 รายการ; 512 = เพดานที่ยัง render บนหน้าเว็บและ validate ได้ไหวบน Pi 5 | 64 = ต่ำกว่านี้ policy ปกติ (ALL×ALL×ALL) ก็ยังต้องแตกได้; 65536 = เพดานกัน RAM/เวลาโหลด nft ruleset บน Pi 5 |
| Validation | two-tier เหมือนคีย์ tuning อื่น: ไม่ใช่จำนวนเต็ม = error ตั้งแต่ `applyKey`; เป็นจำนวนเต็มแต่นอกช่วง = **clamp กลับ default + warning ใน `Resolve` (ห้าม fatal)** | เช่นเดียวกัน |
| ตำแหน่งใน `orderedKeys` | ต่อท้ายสุด (หลัง `traffic-log-buffer-capacity`) เพื่อให้ไฟล์ config ที่ generate ไว้แล้ว diff นิ่ง | ต่อท้ายถัดจาก `max-object-entries` |
| การมีผล | `max-object-entries` มีผลกับการ validate ตอน create/update object (มีผลทันทีต่อ request ถัดไปหลัง restart) | `max-expanded-rules-per-policy` มีผลตอน `ApplyRules` |
| การส่งค่าเข้า layer | `main.go` → `repo.SetObjectLimits(cfg.MaxObjectEntries)` (setter แบบเดียวกับ `SetMockMode`) | `main.go` → `kernel.RealFirewall.SetMaxExpandedRulesPerPolicy(cfg.MaxExpandedRulesPerPolicy)` (setter หลัง `NewRealFirewall`, **ห้ามเปลี่ยน signature constructor**) แล้วส่งลงเป็นพารามิเตอร์ของ `addUserChainRules` |

> ทั้งสองคีย์ **แก้แล้วต้อง `sudo systemctl restart pigate` ถึงมีผล** (อ่านครั้งเดียวตอน startup ตาม pattern "apply config at startup" ของโปรเจกต์)

---

## 3. Design decisions (ที่แผนนี้ยึด)

1. **ตารางลูก (child table) เป็น source of truth ของค่า** — ไม่ยัด list ลงคอลัมน์เดียวแบบ comma-separated
   (ตรวจ/มี CHECK ไม่ได้ + เสี่ยงตอน parse) และไม่เก็บ JSON blob (ค้น/validate ยาก)

   ```sql
   CREATE TABLE IF NOT EXISTS address_object_values (
       address_id TEXT NOT NULL,
       seq        INTEGER NOT NULL,               -- ลำดับแสดงผล เริ่มที่ 1
       type       TEXT NOT NULL CHECK(type IN ('subnet','range','fqdn')),
       value      TEXT NOT NULL,
       PRIMARY KEY (address_id, seq),
       FOREIGN KEY (address_id) REFERENCES address_objects(id) ON DELETE CASCADE );

   CREATE TABLE IF NOT EXISTS service_object_ports (
       service_id TEXT NOT NULL,
       seq        INTEGER NOT NULL,
       protocol   TEXT NOT NULL CHECK(protocol IN ('TCP','UDP','TCP/UDP','ICMP')),
       port       TEXT NOT NULL,
       PRIMARY KEY (service_id, seq),
       FOREIGN KEY (service_id) REFERENCES service_objects(id) ON DELETE CASCADE );
   ```

2. **คอลัมน์เดิม `type/value` และ `protocol/port` บนตารางแม่ = compat mirror ของรายการที่ 1 แบบ "ชั่วคราว" (D-4)**
   เขียนทุกครั้งที่ create/update แต่ไม่มีใครอ่านเพื่อ generate กฎอีกแล้ว
   เหตุผลที่ยังไม่ลบตอนนี้: (ก) คอลัมน์เป็น `NOT NULL` + มี CHECK และ SQLite รุ่นเก่าไม่รองรับ `DROP COLUMN`
   (ข) เผื่อ downgrade binary กลับรุ่นก่อนหน้าในช่วงเปลี่ยนผ่านโดยข้อมูลไม่หายทั้งก้อน
   ⇒ **ต้องมีหมายเหตุ deprecation ระบุว่าจะถูกลบใน major version ถัดไป** ทุกจุดตามตาราง D-4

3. **Migration แบบไม่ทำลายข้อมูล และ idempotent** — สร้างตารางลูกแล้ว backfill *เฉพาะ object ที่ยังไม่มีแถวลูก*
   (`INSERT ... SELECT ... WHERE NOT EXISTS`) ⇒ รันซ้ำกี่รอบก็ปลอดภัย และครอบคลุมทั้ง DB เก่าและ row ที่ seed ใหม่
   (ต้องรัน **หลัง** seed default objects)

4. **API เป็น additive ล้วน** — เพิ่มฟิลด์ `entries` (array) และคงฟิลด์เดิมไว้เป็น legacy mirror ของรายการที่ 1
   (มาร์ค `deprecated` ใน openapi) ⇒ client เก่ายังใช้ได้
   ฝั่ง Input: ถ้าส่ง `entries` มาใช้ `entries`; ถ้าไม่ส่งแต่มี `type/value` ให้แปลงเป็น 1 รายการ (legacy path)

5. **nftables**: ขยาย cartesian loop จาก "ต่อ object" เป็น "ต่อรายการใน object" — ทุก nft rule ที่แตกออกมายังคง
   `UserData = ruleID` และ log prefix เดิม ⇒ สถิติ per-rule / matched endpoints / traffic detail ทำงานเหมือนเดิม
   และ **โครงสร้าง 4 section ของ chain ไม่ถูกแตะ**

6. **ไม่แตะตาราง `policy_addresses`/`policy_services`** — policy อ้าง object ด้วย id/ชื่อ ⇒ backward compatible อัตโนมัติ

7. **object ระบบ (`ALL`, `HTTP`, `HTTPS`, `SSH`, `DNS`, `ICMP`) ยังคงแก้ไม่ได้ และมีรายการเดียวเหมือนเดิม**

8. **เพดานทั้งสองตัวมาจาก config file ไม่ใช่ const** (D-3) — โค้ดต้องมีเพียง *ค่า default* ที่ผูกกับ
   `config.Defaults()` และรับค่าจริงผ่านพารามิเตอร์/setter เท่านั้น

9. **ไม่เพิ่ม dependency ใหม่ ไม่ใช้ exec.Command** — ทุกอย่างยังอยู่บน netlink/`google/nftables` และ stdlib เดิม

---

## 4. Caution (จุดที่พังง่ายที่สุด — ต้องอ่านก่อนลงมือทุก task)

1. **กับดัก checksum ของ backup (ร้ายแรงสุด)** — `configChecksum` คำนวณ sha256 จาก `json.Marshal(BackupConfig)`
   และ `decodeBackup` ถือว่า mismatch = ไฟล์เสีย (error). ถ้าเพิ่มฟิลด์ `Entries` แบบไม่มี `omitempty`
   ไฟล์ backup **เก่า** จะ marshal ออกมาเป็น `"entries":null` เพิ่มเข้าไป ⇒ checksum ไม่ตรง ⇒ **import ไฟล์เก่าทุกไฟล์พังทันที**
   ⇒ (ก) ฟิลด์ใหม่ทุกตัวต้องเป็น `json:"entries,omitempty"` **และ** (ข) ห้าม normalize legacy→entries ก่อนจุดเช็ค checksum
   ใน `decodeBackup` (ต้องทำหลังจากนั้น เช่นใน `validateConfig`/restore) — ต้องมีเทสต์ regression ข้อนี้ใน T-07
2. **จำนวนกฎ nft ระเบิด** — src M รายการ × dest N × service K (× 2 ถ้า TCP/UDP) ⇒ M·N·K·2 กฎต่อ 1 policy rule
   ⇒ ต้องมีเพดาน **ที่มาจาก config** (`max-object-entries`, `max-expanded-rules-per-policy`) และเมื่อชนเพดานให้
   **log warning แล้วข้ามส่วนที่เกิน ไม่ใช่ทำให้ apply ทั้งชุดล้ม** (ตามพฤติกรรมเดิมที่ `addUserChainRules` log+`continue`)
3. **FQDN resolve ล้มเหลวรายรายการ** — ปัจจุบัน `buildIPMatchExpressions` คืน error แล้ว caller ข้ามทั้ง combination
   ⇒ เมื่อเป็น list ต้องข้าม **เฉพาะรายการนั้น** แต่ยังต้อง generate รายการอื่นของ object เดิมต่อ
4. **ห้ามเปลี่ยนลำดับ 4 section ของ chain** (sanity/drop → audit log → dynamic accept → final drop+log ตาม
   `docs/tech_stack_design.md` §4.3) — งานนี้เพิ่มจำนวนกฎใน section 3 เท่านั้น
5. **Validation ต้องเป็น fail-closed และมีที่เดียว** — ย้าย/ยกตรรกะจาก `repository.validateAddressObject` /
   `validateServiceObject` ไปเป็น `model.ValidateAddressEntry` / `model.ValidateServiceEntry` แล้วให้ทั้ง repository,
   handler และ backup import เรียกตัวเดียวกัน (path นี้ป้อนค่าเข้ากฎไฟร์วอลล์โดยตรง = sensitive, ต้อง review เข้ม)
6. **ตารางลูกต้องมี `ON DELETE CASCADE` แต่การลบ object ที่ถูก policy อ้างอิงยังต้องถูกบล็อกเหมือนเดิม**
   (`policy_addresses/policy_services` COUNT > 0 ⇒ error) — ห้าม CASCADE ทะลุไปลบความสัมพันธ์ของ policy
7. **`db/backup_repo.go` wipe order** — ตอนนี้ลบ `address_objects WHERE system = 0` โดยพึ่ง FK RESTRICT;
   ตารางลูกใช้ CASCADE จึงหายตามอัตโนมัติ แต่ต้อง **ตรวจว่าลำดับ wipe/insert ยังถูกและแถวลูกของ system object ไม่ถูกลบ**
8. **ห้ามแก้ `backend/internal/api/dist/openapi.yaml`** (build artifact ที่ `build.sh` copy จาก `frontend/public/`)
9. **localStorage ของโหมด mock (frontend)** — ข้อมูลเดิมไม่มีฟิลด์ `entries` ⇒ service ฝั่ง frontend ต้อง normalize ตอนอ่าน
10. **`mockSync.ts` match ด้วย `addr.value`** — ต้องเปลี่ยนเป็นวนทุก entry ไม่งั้น refPolicies ในโหมด mock เพี้ยน
11. **ห้ามยุ่งกับความหมายของชื่อซ้ำ** — `name UNIQUE` และ fallback ตัดคำแรกใน `savePolicyRelations`/`resolveService`
    (`"HTTP (TCP 80)"` → `HTTP`) ยังต้องทำงานเหมือนเดิม
12. **อย่างน้อย 1 รายการเสมอ** — object ที่มี `entries` ว่างต้องถูก reject ทั้งที่ repository และ handler
13. **รายการซ้ำภายใน object** — reject พร้อมระบุค่าที่ซ้ำ อย่าปล่อยให้ generate กฎซ้ำเงียบ ๆ
14. **PR #137**: `newAddrMatcher` เดิมสร้าง 1 ช่วงต่อ 1 object ⇒ ต้องเปลี่ยนเป็น **N ช่วงต่อ 1 object ทุกช่วงชี้ชื่อเดียวกัน**
    (ตรรกะ "แคบสุดชนะ + tie-break ตามชื่อ" ใช้ต่อได้), และ `buildCategoryEntries` ต้องสร้าง 1 entry ต่อ 1 (protocol, port)
15. **ห้าม hardcode เพดานเป็น const ในโค้ดที่ใช้งานจริง (D-3)** — const ที่อนุญาตมีเพียงค่า default/ขอบเขตใน
    `internal/config` เท่านั้น; layer อื่นต้องรับค่าผ่านพารามิเตอร์/setter
16. **เทสต์ที่จะพังแน่ ๆ ต้องอัปเดตด้วย**: `db/repository_test.go`, `service/firewall_test.go` (มี `addr.Value = ...` บรรทัด 317),
    `service/backup_test.go`, `kernel/policy_chain_test.go`, `api/handlers_test.go`, `config/config_test.go`

---

## 5. Task list

> ทำเรียงตามลำดับ **ไม่ต้องทดสอบทีละ task** — ทำครบ T-00A → T-14 แล้วค่อยส่ง ai-qa ทดสอบรวมรอบเดียวตาม §6

| Task | Layer | ไฟล์หลัก | สรุป | depends_on |
|---|---|---|---|---|
| T-00A | config | `config/config.go` | 2 config key ใหม่ (`max-object-entries`, `max-expanded-rules-per-policy`) | — |
| T-00 | model | `model/object_entry_validate.go` (ใหม่) | `AddressEntry`/`ServiceEntry` + validator กลาง (รับเพดานเป็นพารามิเตอร์) | T-00A |
| T-01 | model | `model/types.go` | เพิ่ม `Entries` (omitempty) + helper normalize + หมายเหตุ deprecation ฟิลด์เดิม | T-00 |
| T-02 | db | `db/connection.go` | 2 ตารางลูก + migration/backfill idempotent + comment deprecation | T-01 |
| T-03 | db | `db/repository.go` | CRUD entries + `SetObjectLimits()` รับค่าจาก config | T-02 |
| T-04 | kernel | `kernel/real_firewall.go` | expand entries → nft rules + guard จาก config | T-01, T-00A |
| T-05 | kernel | `kernel/mock.go`, `kernel/policy_chain_test.go` | mock + เทสต์ chain | T-04 |
| T-06 | api | `api/handlers.go` | รับ `entries` + legacy fallback | T-01, T-03 |
| T-07 | service | `service/backup.go`, `db/backup_repo.go` | export/import entries + กับดัก checksum | T-01, T-03 |
| T-08 | service | `service/traffic_stats.go`, `service/policy_endpoint_labels.go` | รองรับหลาย entry (กระทบ PR #137) | T-01 |
| T-08B | wiring | `cmd/pigate/main.go` | ส่งค่า config 2 คีย์เข้า repository/kernel | T-00A, T-03, T-04 |
| T-09 | test | `*_test.go` ฝั่ง backend | เทสต์ใหม่ + ซ่อมเทสต์เดิม | T-00A..T-08B |
| T-10 | docs | `docs/openapi.yaml`, `frontend/public/openapi.yaml`, `docs/data/firewall.md`, `pigate.conf.example`, `README.md` | schema + คีย์ config + หมายเหตุ deprecation | T-06, T-07, T-00A |
| T-11 | frontend | `data-mockup/mockData.ts`, `services/addressService.ts`, `services/serviceObjectService.ts`, `services/mockSync.ts` | type + API client + mock normalize | T-10 |
| T-12 | frontend | `pages/Addresses.tsx` | ฟอร์ม list เพิ่ม/ลบรายการ + ตาราง/สถิติ/ฟิลเตอร์ | T-11 |
| T-13 | frontend | `pages/Services.tsx` | ฟอร์ม list (protocol+port ต่อแถว) | T-11 |
| T-14 | docs | `README.md`, ไฟล์แผนนี้ | ปิดงาน + ย้ายแผนเข้า `complete/` | T-12, T-13 |

### T-00A (ใหม่ ตาม D-3) — config key ปรับเพดานได้

```json
{
  "task_id": "T-00A",
  "title": "เพิ่ม config key max-object-entries และ max-expanded-rules-per-policy",
  "layer": "service",
  "files": ["backend/internal/config/config.go"],
  "instruction": "ใน backend/internal/config/config.go ให้เพิ่มคีย์ file-only ใหม่ 2 คีย์ โดยเลียนแบบ pattern ของ deny-stats-max-sources/deny-stats-max-ports และ traffic-log-buffer-capacity ทุกขั้นตอน ห้ามคิด pattern ใหม่: (1) field `MaxObjectEntries int` และ `MaxExpandedRulesPerPolicy int` ใน struct Config พร้อม comment ว่าเป็น file-only (ไม่มี CLI flag), MaxObjectEntries = จำนวนรายการย่อยสูงสุดต่อ 1 Address/Service Object (บังคับใช้ตอน validate ใน db/repository.go) และ MaxExpandedRulesPerPolicy = เพดานจำนวน nft rule ที่ 1 policy rule แตกออกได้ (บังคับใช้ใน kernel/real_firewall.go addUserChainRules) และทั้งคู่มีผลเมื่อ restart เท่านั้น พร้อมอ้าง docs/ref/todo/multi-value-address-service-objects-plan.md §2.1; (2) ค่าใน Defaults(): MaxObjectEntries = 64, MaxExpandedRulesPerPolicy = 4096 พร้อม comment ว่าเป็นค่าที่เจ้าของโปรเจกต์ยืนยัน (D-3) และต้อง sync กับ model.DefaultMaxObjectEntries; (3) const keyMaxObjectEntries = \"max-object-entries\" และ keyMaxExpandedRulesPerPolicy = \"max-expanded-rules-per-policy\"; (4) const ขอบเขต minMaxObjectEntries = 1, maxMaxObjectEntries = 512, minMaxExpandedRulesPerPolicy = 64, maxMaxExpandedRulesPerPolicy = 65536 พร้อม comment อธิบายที่มาตาม §2.1; (5) ต่อท้าย orderedKeys ทั้งสองคีย์ (หลัง keyTrafficLogBufferCapacity) พร้อม comment ว่าเหตุใดจึงต่อท้ายแทนเรียงตามตัวอักษร (ให้ไฟล์ config เดิม diff นิ่ง); (6) case ใน applyKey ที่ทำแค่ strconv.Atoi + เก็บค่า พร้อม comment ว่า range check อยู่ใน Resolve; (7) range-sanity pass ใน Resolve ต่อท้ายบล็อกของ TrafficLogBufferCapacity: นอกช่วง ⇒ append warning ('max-object-entries=%d out of range (%d..%d), using default %d' และคู่เดียวกันของอีกคีย์) แล้ว clamp กลับ default ห้าม return error; (8) case ใน keyValue คืน strconv.Itoa; (9) อัปเดต package doc comment ให้ครอบคลุมคีย์ใหม่ทั้งสอง. ห้ามลงทะเบียน flag ใน main.go ห้ามแก้ไฟล์อื่นใน task นี้",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "config.Defaults().MaxObjectEntries == 64 และ MaxExpandedRulesPerPolicy == 4096",
    "orderedKeys มีสองคีย์ใหม่อยู่ท้ายสุด และ Write ออกมาเป็นสองบรรทัดสุดท้าย",
    "ค่า 'abc' ⇒ error; ค่า 0/-1/513 (คีย์แรก) และ 0/63/65537 (คีย์สอง) ⇒ warning + clamp เป็น default; ค่าขอบเขต (1,512,64,65536) ⇒ ผ่านไม่มี warning",
    "ไม่มี flag ใหม่ใน cmd/pigate/main.go ใน task นี้"
  ],
  "depends_on": []
}
```

### T-00 — model: entry types + validator กลาง

```json
{
  "task_id": "T-00",
  "title": "เพิ่ม AddressEntry/ServiceEntry + validator กลางใน model (เพดานเป็นพารามิเตอร์ ไม่ใช่ const ตายตัว)",
  "layer": "model",
  "files": ["backend/internal/model/object_entry_validate.go"],
  "instruction": "สร้างไฟล์ใหม่ backend/internal/model/object_entry_validate.go (สไตล์เดียวกับ model/port_forward_validate.go) ประกอบด้วย: (1) type AddressEntry struct { Type string `json:\"type\"`; Value string `json:\"value\"` } และ type ServiceEntry struct { Protocol string `json:\"protocol\"`; Port string `json:\"port\"` } พร้อม doc comment ว่า 1 object มีได้หลาย entry และ entry คือหน่วยที่ถูกแปลงเป็น nft match 1 ชุด และ type/protocol อยู่ระดับ entry (ปนกันได้ใน object เดียว ตาม D-1); (2) func ValidateAddressEntry(e AddressEntry) error — ยกตรรกะจาก db/repository.go validateAddressObject มาแบบ verbatim (subnet ใช้ net.ParseCIDR; range รูปแบบ START-END ตรวจ ParseIP ทั้งสองฝั่งและ family ต้องตรง; fqdn ใช้ตรรกะเดียวกับ isValidFQDN ที่ย้ายมาเป็น helper unexported ในไฟล์นี้) ห้ามเปลี่ยนกฎการตรวจ; (3) func ValidateServiceEntry(e ServiceEntry) error — ยกตรรกะจาก validateServiceObject (protocol ∈ TCP/UDP/TCP\\/UDP/ICMP, ICMP ⇒ port == \"-\", อื่น ๆ เลขเดี่ยวหรือ start-end ที่ 1..65535 และ start <= end) — **การตรวจรูปแบบ/ช่วงพอร์ตต้อง reuse `model.ParsePortSpec` (มีอยู่แล้วใน model/types.go:843 และถูกใช้อยู่แล้วใน service/traffic_stats.go) ห้ามเขียน parser พอร์ตซ้ำใหม่** (ตรงกับหลัก validate ที่เดียวของ Caution 5); (4) **ห้ามประกาศเพดานเป็น const ที่ใช้งานจริง** — ให้ประกาศเพียง `const DefaultMaxObjectEntries = 64` พร้อม comment ว่าเป็นค่า default ที่ต้อง sync กับ config.Defaults().MaxObjectEntries และค่าจริงมาจาก config key max-object-entries (D-3, plan §2.1); (5) func ValidateAddressEntries(es []AddressEntry, maxEntries int) error และ ValidateServiceEntries(es []ServiceEntry, maxEntries int) error — ต้องมีอย่างน้อย 1 รายการ, ไม่เกิน maxEntries (ถ้า maxEntries <= 0 ให้ fallback ใช้ DefaultMaxObjectEntries พร้อม comment), ทุกรายการผ่าน validator เดี่ยว, รายการซ้ำ (เทียบแบบ trim+lowercase ของ type|value และ protocol|port) คืน error ระบุค่าที่ซ้ำ. ห้ามแก้ไฟล์อื่นใน task นี้",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "มี AddressEntry, ServiceEntry, DefaultMaxObjectEntries=64 และ ValidateAddressEntries/ValidateServiceEntries ที่รับ maxEntries เป็นพารามิเตอร์",
    "ไม่มี const เพดานตัวใดถูกใช้เป็นค่าบังคับใช้จริงในไฟล์นี้ (ใช้ได้เฉพาะเป็น fallback default)",
    "กฎการ validate ตรงกับของเดิมใน db/repository.go ทุกกรณี",
    "ValidateServiceEntry เรียกใช้ model.ParsePortSpec แทนการเขียน parser พอร์ตซ้ำ"
  ],
  "depends_on": ["T-00A"]
}
```

### T-01 — model: ฟิลด์ Entries (additive, omitempty) + หมายเหตุ deprecation

```json
{
  "task_id": "T-01",
  "title": "เพิ่มฟิลด์ Entries ใน AddressObject/ServiceObject + Input + helper normalize + deprecation note",
  "layer": "model",
  "files": ["backend/internal/model/types.go"],
  "instruction": "ใน backend/internal/model/types.go: (1) เพิ่ม `Entries []AddressEntry `json:\"entries,omitempty\"`` ใน AddressObject และ AddressObjectInput, `Entries []ServiceEntry `json:\"entries,omitempty\"`` ใน ServiceObject และ ServiceObjectInput — **ต้องมี omitempty ทุกตัว** พร้อม comment อ้าง Caution 1 ของ docs/ref/todo/multi-value-address-service-objects-plan.md ว่าถ้าไม่มี omitempty ไฟล์ backup เก่าจะ checksum ไม่ตรงและ import ไม่ได้ ห้ามลบเด็ดขาด; (2) **หมายเหตุ deprecation ตาม D-4**: เขียน comment เหนือฟิลด์ Type/Value และ Protocol/Port ว่า 'Deprecated: compat mirror ของ Entries[0] เท่านั้น คงไว้ชั่วคราวเพื่อ client เก่าและเพื่อให้ downgrade binary กลับได้ จะถูกลบใน major version ถัดไป (SQLite รุ่นเก่าไม่รองรับ DROP COLUMN และคอลัมน์เป็น NOT NULL + มี CHECK) — ห้ามใช้ generate กฎไฟร์วอลล์'; (3) func NormalizeAddressObject(a *AddressObject) และ NormalizeServiceObject(s *ServiceObject): ถ้า Entries ว่างแต่ค่า legacy ไม่ว่าง ให้เติม Entries = 1 รายการจากค่า legacy; ถ้า Entries ไม่ว่าง ให้เขียนค่า legacy จาก Entries[0] ทับเสมอ; ต้อง idempotent และ nil-safe; (4) ฟังก์ชันคู่เดียวกันสำหรับ *AddressObjectInput / *ServiceObjectInput เพื่อให้ handler ใน T-06 เรียกได้. ห้ามแก้ signature/ชื่อฟิลด์เดิม ห้ามแตะ PolicyRule",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "ทุกฟิลด์ใหม่มี json omitempty",
    "json.Marshal ของ AddressObject ที่ Entries == nil ให้ผลลัพธ์ byte-identical กับก่อนแก้",
    "ฟิลด์ Type/Value/Protocol/Port มี comment ขึ้นต้นด้วย 'Deprecated:' และระบุว่าจะถูกลบใน major version ถัดไปพร้อมเหตุผล",
    "Normalize* เรียกซ้ำแล้วผลไม่เปลี่ยน (idempotent)"
  ],
  "depends_on": ["T-00"]
}
```

### T-02 — db: schema + migration ไม่ทำลายข้อมูลเดิม (+ deprecation note)

```json
{
  "task_id": "T-02",
  "title": "สร้างตาราง address_object_values / service_object_ports + backfill + หมายเหตุ deprecation คอลัมน์เดิม",
  "layer": "db",
  "files": ["backend/internal/db/connection.go"],
  "instruction": "ใน backend/internal/db/connection.go: (1) เพิ่ม CREATE TABLE IF NOT EXISTS สองตารางตาม §3 ข้อ 1 ของ docs/ref/todo/multi-value-address-service-objects-plan.md (PK (parent_id, seq), FK ... ON DELETE CASCADE, CHECK ของ type/protocol เหมือนตารางแม่) วางต่อจาก CREATE TABLE ของ address_objects/service_objects ใน slice queries เดิม; (2) **หมายเหตุ deprecation ตาม D-4**: เขียน comment block เหนือ CREATE ทั้งสองว่า ตารางลูกคือ source of truth ของค่า ส่วนคอลัมน์ address_objects.type/value และ service_objects.protocol/port เป็น 'compat layer ชั่วคราว' ที่ mirror แถว seq=1 เท่านั้น มีไว้เพื่อ (ก) รองรับ downgrade binary ระหว่างเปลี่ยนผ่าน (ข) เพราะ SQLite รุ่นเก่าไม่รองรับ DROP COLUMN และคอลัมน์เป็น NOT NULL + มี CHECK constraint — และระบุชัดว่า **มีแผนลบทิ้งใน major version ถัดไป** ห้ามเขียนโค้ดใหม่ที่อ่านคอลัมน์เหล่านี้เพื่อ generate กฎ; (3) backfill idempotent ที่ต้องรัน **หลัง** ขั้น seed default objects: INSERT INTO address_object_values (address_id, seq, type, value) SELECT id, 1, type, value FROM address_objects a WHERE NOT EXISTS (SELECT 1 FROM address_object_values v WHERE v.address_id = a.id) และคู่เดียวกันของ service_object_ports พร้อม comment ว่าเงื่อนไข NOT EXISTS ทำให้รันซ้ำ/หลัง downgrade-upgrade ปลอดภัยและข้อมูลผู้ใช้ไม่หาย; (4) log จำนวนแถวที่ backfill (RowsAffected) เมื่อ > 0 เพื่อยืนยันว่าเครื่องผู้ใช้ migrate แล้วจริง. ห้ามใช้ DROP COLUMN ห้ามแก้ CHECK ของตารางแม่ ห้ามแตะ policy_addresses/policy_services",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "DB เดิมของผู้ใช้: ทุก object ได้แถวลูก seq=1 ตรงกับค่าเดิม และคอลัมน์เดิมไม่ถูกแก้",
    "รัน InitDB ซ้ำสองรอบแล้วแถวลูกไม่เพิ่ม (idempotent)",
    "DB ใหม่ได้ addr-1 และ svc-1..svc-6 พร้อมแถวลูกครบ",
    "มี comment ระบุ deprecation ของคอลัมน์เดิมพร้อมเหตุผลและแผนลบใน major version ถัดไป"
  ],
  "depends_on": ["T-01"]
}
```

### T-03 — db: repository อ่าน/เขียน entries + รับเพดานจาก config

```json
{
  "task_id": "T-03",
  "title": "repository: CRUD หลายรายการ + SetObjectLimits รับค่าจาก config",
  "layer": "db",
  "files": ["backend/internal/db/repository.go"],
  "instruction": "ใน backend/internal/db/repository.go: (1) เพิ่ม field `maxObjectEntries int` ใน struct Repository (ค่าเริ่มต้นใน NewRepository = model.DefaultMaxObjectEntries) และ setter `func (r *Repository) SetObjectLimits(maxObjectEntries int)` (pattern เดียวกับ SetMockMode/SetAllowEditSystemRoutes ที่มีอยู่แล้ว) พร้อม comment ว่าใครเรียก (cmd/pigate/main.go ด้วย cfg.MaxObjectEntries จาก config key max-object-entries) และห้าม hardcode เพดานที่จุดใช้งาน; (2) GetAddresses/GetAddressByID/GetServices/GetServiceByID โหลด entries จากตารางลูก ORDER BY seq ASC ใส่ .Entries แล้วเซ็ตฟิลด์ legacy ผ่าน model.NormalizeAddressObject/NormalizeServiceObject; object ที่ไม่มีแถวลูก (กรณีขอบ) fallback เป็น 1 entry จากคอลัมน์เดิม ห้าม panic; ระวังประสิทธิภาพ — อย่ายิง query ซ้อนใน loop ที่ยังเปิด rows ค้าง ให้ดึงแถวลูกทีเดียวแล้ว group ใน memory และปิด rows ให้ถูก; (3) CreateAddress/UpdateAddress/CreateService/UpdateService ทำใน transaction เดียว: normalize → เรียก model.ValidateAddressEntries(entries, r.maxObjectEntries) / ValidateServiceEntries(...) (fail-closed) → INSERT/UPDATE ตารางแม่ด้วย mirror จาก entry แรก → DELETE แถวลูกเดิมทั้งหมด แล้ว INSERT ใหม่ seq 1..N; คงการตรวจ system lock เดิมทุกจุด; (4) validateAddressObject/validateServiceObject เดิมกลายเป็น wrapper ที่เรียก validator ใน model (ห้ามมีตรรกะ validate สองชุด) และลบ isValidFQDN/isValidPort ที่ซ้ำถ้าไม่มีใครใช้แล้ว; (5) Delete/BulkDelete คงการบล็อกเมื่อถูก policy อ้างอิงและเมื่อเป็น system เหมือนเดิม + comment ว่า CASCADE ครอบเฉพาะตารางลูกของค่า ไม่เกี่ยวกับ policy_addresses/policy_services ที่ยังเป็น RESTRICT; (6) savePolicyRelations ห้ามแก้พฤติกรรม",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "มี SetObjectLimits และไม่มีการอ้างเลข 64 แบบ hardcode ในโค้ดบังคับใช้",
    "สร้าง object 3 entries อ่านกลับได้ครบตามลำดับ seq และฟิลด์ legacy = entry แรก",
    "update จาก 3 เหลือ 1 entry แล้วแถวลูกส่วนเกินหายจริง",
    "entries ว่าง / เกินเพดานที่ตั้งไว้ / ซ้ำ / รูปแบบผิด ⇒ error และไม่มีแถวใดถูกเขียน",
    "ลบ object ที่ policy อ้างอิงยัง error เหมือนเดิม, system object ยัง update/delete ไม่ได้"
  ],
  "depends_on": ["T-02"]
}
```

### T-04 — kernel: generate nftables จากหลาย entry (**sensitive — review เข้มเป็นพิเศษ**)

```json
{
  "task_id": "T-04",
  "title": "real_firewall: expand ทุก entry + guard จำนวนกฎที่รับค่าจาก config",
  "layer": "kernel",
  "files": ["backend/internal/kernel/real_firewall.go"],
  "instruction": "ใน backend/internal/kernel/real_firewall.go — security-sensitive path ต้องรักษาพฤติกรรมเดิมทุกอย่างนอกจากการรองรับหลายค่า: (1) เปลี่ยน buildIPMatchExpressions ให้รับ model.AddressEntry แทนการเปิด addrsMap ด้วยชื่อ แล้วสร้าง exprs ชุดเดิมเป๊ะ (subnet /32 = Cmp เท่ากับ, subnet อื่น = Payload+Bitwise+Cmp, range = Gte+Lte, fqdn = LookupIP ใช้ IPv4 ตัวแรกเหมือนเดิม) ห้ามเปลี่ยนวิธี match; เพิ่ม helper แปลงชื่อ object → []AddressEntry (ไม่พบชื่อ = error เหมือนเดิม, \"ALL\"/ว่าง = ไม่มีเงื่อนไข IP เหมือนเดิม); (2) addUserChainRules loop เพิ่มเป็น: src object → ทุก src entry, dest object → ทุก dest entry, service object → ทุก service entry → ทุก protocol ของ entry นั้น (entry ที่ protocol == \"TCP/UDP\" ยังแตกเป็น TCP และ UDP); ทุก nft rule ที่แตกออกมาต้องใช้ ruleUserData (rule id) เดียวกันและ logPrefix ที่ผ่าน withRuleToken(r.ID) เดียวกัน; ห้ามแตะโครง 4 section และลำดับ rule ระหว่าง policy; (3) resolveService คืน object พร้อม entries (คง fallback ตัดคำแรกของชื่อ) และ buildRuleExpressions รับ service entry ที่ resolve แล้วแทนการอ่าน svc.Protocol/svc.Port ของทั้ง object; (4) error รายรายการ: entry ใด build ไม่ผ่าน (เช่น FQDN resolve ไม่ได้) ให้ log warning ระบุชื่อ object + ค่า entry แล้ว continue เฉพาะ entry นั้น entry อื่นของ object เดิมต้องยังถูก generate; (5) **guard เพดานต้องมาจาก config ไม่ใช่ const (D-3)**: เพิ่ม field `maxExpandedRulesPerPolicy int` ใน struct RealFirewall (ค่าเริ่มต้นใน NewRealFirewall = 4096 พร้อม comment ว่าเป็น default ที่ต้อง sync กับ config.Defaults().MaxExpandedRulesPerPolicy) + setter `SetMaxExpandedRulesPerPolicy(n int)` (**ห้ามเปลี่ยน signature ของ NewRealFirewall**) แล้วส่งค่าลงเป็นพารามิเตอร์ของ addUserChainRules; เมื่อการแตกกฎของ policy rule หนึ่งจะเกินเพดาน ให้หยุดแตกต่อและ log warning ระบุชื่อ/ไอดี rule + เพดานที่ใช้ + ชื่อคีย์ config (max-expanded-rules-per-policy) แต่ **ห้าม return error หรือทำให้ ApplyRules ทั้งชุดล้ม**; (6) doc comment สรุปสูตรจำนวนกฎ (M×N×K×proto) และอ้าง plan §2.1. ห้ามใช้ nftables set (D-2) ห้ามเพิ่ม dependency",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "src 3 entries × dest ALL × service 2 entries ⇒ จำนวน nft rule ตามสูตร และทุกกฎมี UserData = rule id เดียวกัน",
    "fqdn entry ที่ resolve ไม่ได้ ข้ามเฉพาะ entry นั้น",
    "เพดานมาจาก field/พารามิเตอร์ที่ตั้งค่าได้ ไม่ใช่ const ที่อ้างตรงในจุดตรวจ และ log warning ระบุชื่อคีย์ config",
    "section 1,2,4 ของทุก chain ไม่เปลี่ยนจากเดิม (diff เฉพาะ section 3)",
    "policy ที่ object มีค่าเดียว ให้ exprs เหมือนก่อนแก้ทุกประการ"
  ],
  "depends_on": ["T-01", "T-00A"]
}
```

### T-05 — kernel: mock + เทสต์ chain

```json
{
  "task_id": "T-05",
  "title": "ตรวจ/ปรับ mock kernel และเทสต์โครงสร้าง chain",
  "layer": "kernel",
  "files": ["backend/internal/kernel/mock.go", "backend/internal/kernel/policy_chain_test.go"],
  "instruction": "ตรวจ backend/internal/kernel/mock.go: ApplyRules ปัจจุบันเก็บ addrs/svcs ไว้เฉย ๆ ไม่ parse ค่า ⇒ ถ้าไม่มีจุดใดอ่าน .Value/.Port ให้เพิ่มเพียง comment ว่า mock ไม่ตีความ entries และของจริงอยู่ใน real_firewall.go; ถ้าพบจุดที่อ่านค่า object (เช่นการสังเคราะห์ traffic log) ให้ปรับให้วนทุก entry; ถ้า mock มี setter/field เทียบเท่า ให้เพิ่ม no-op SetMaxExpandedRulesPerPolicy เฉพาะเมื่อ interface บังคับเท่านั้น (ห้ามเพิ่มเมธอดใน FirewallManager interface ถ้าไม่จำเป็น — setter นี้ควรอยู่บน struct RealFirewall เท่านั้น และ main.go เรียกผ่าน type assertion หรือตอนสร้าง real). จากนั้นอัปเดต policy_chain_test.go ให้คอมไพล์และผ่านกับ signature ใหม่ โดย **ห้ามอ่อนข้อ assertion เดิม** เรื่องลำดับ section/กฎ ให้ปรับเฉพาะ fixture ให้ใช้ Entries",
  "acceptance": [
    "cd backend && go build ./... และ go test ./internal/kernel/... ผ่าน",
    "assertion เดิมเรื่องลำดับ section/กฎยังอยู่ครบ",
    "ไม่มีเมธอดใหม่ถูกยัดเข้า FirewallManager interface โดยไม่จำเป็น"
  ],
  "depends_on": ["T-04"]
}
```

### T-06 — api: handler รับ entries + legacy fallback

```json
{
  "task_id": "T-06",
  "title": "handlers: create/update address & service object แบบหลายรายการ",
  "layer": "api",
  "files": ["backend/internal/api/handlers.go"],
  "instruction": "ใน backend/internal/api/handlers.go: (1) HandleCreateAddress/HandleUpdateAddress/HandleCreateService/HandleUpdateService map input → model โดย: ถ้า input.Entries ไม่ว่างใช้ Entries; ถ้าว่างแต่มี Type/Value (Protocol/Port) ให้แปลงเป็น 1 entry (legacy path) ผ่าน helper จาก T-01; ไม่มีทั้งคู่ ⇒ 400 ข้อความชัดเจน; (2) response คืน object ที่มีทั้ง entries และฟิลด์ legacy (เรียก Normalize* ก่อน writeJSON); (3) logEvent เพิ่มจำนวนรายการ เช่น 'Address object \"X\" created (3 entries)' โดยคงชื่อ event เดิม; (4) ห้าม validate ซ้ำคนละกฎกับ repository — handler แค่ map และคืน error จาก repository เป็น 400 เหมือนเดิม (fail-closed); ห้าม hardcode เพดานจำนวน entry ใน handler (เพดานอยู่ที่ repository ซึ่งรับค่าจาก config). ห้ามแตะ router/สิทธิ์/middleware",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "POST /api/addresses ด้วย body เก่า {name,type,value} ยังทำงานได้ (สร้าง 1 entry)",
    "POST/PUT ด้วย {name, entries:[...]} สร้าง/แก้ได้หลายรายการ",
    "body ที่ไม่มีทั้ง entries และ type/value ⇒ 400",
    "response มีทั้ง entries และฟิลด์ legacy ที่ตรงกับ entry แรก",
    "ไม่มีเลขเพดานถูก hardcode ใน handler"
  ],
  "depends_on": ["T-01", "T-03"]
}
```

### T-07 — backup/restore (**ระวัง checksum**)

```json
{
  "task_id": "T-07",
  "title": "Backup export/import รองรับ entries โดยไฟล์เก่ายัง import ได้",
  "layer": "service",
  "files": ["backend/internal/service/backup.go", "backend/internal/db/backup_repo.go"],
  "instruction": "(1) db/backup_repo.go: restore ให้ INSERT ตารางแม่ (mirror จาก entry แรก) แล้ว INSERT แถวลูกทุก entry ตามลำดับ seq ใน transaction เดิม; object ในไฟล์ที่ไม่มี entries (ไฟล์เก่า) ให้แปลงค่า legacy เป็น 1 entry ก่อนเขียน; ตรวจลำดับ wipe เดิมว่ายังถูกเมื่อมีตารางลูกแบบ CASCADE และแถวลูกของ system object ต้องไม่ถูกลบ; (2) service/backup.go: normalize object จากไฟล์ (legacy → entries) **หลังจาก** ขั้นตรวจ checksum ใน decodeBackup เท่านั้น แล้วตรวจทุก entry ด้วย model.ValidateAddressEntries/ValidateServiceEntries แบบ fail-closed (ส่งเพดานจาก config ถ้ามีให้ใช้ ไม่งั้นใช้ DefaultMaxObjectEntries) — ไฟล์ที่มี entry เพี้ยนต้องถูก reject ทั้งไฟล์ก่อนเขียน DB; (3) **ห้าม** แก้ configChecksum หรือ normalize cfg ก่อนบรรทัดเทียบ checksum ใน decodeBackup — เขียน comment เตือนไว้ตรงนั้นพร้อมเหตุผล (Caution 1); (4) configCounts นับ object เท่าเดิม ถ้าจะเพิ่มให้เพิ่มเป็นคีย์ใหม่เท่านั้น",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "ไฟล์ backup จากรุ่นก่อนหน้า (ไม่มี entries) import ผ่าน checksum และได้ object 1 entry ต่อชื่อ",
    "round-trip export → import ได้ entries ครบตามลำดับ",
    "ไฟล์ที่มี entry เพี้ยนถูก reject ก่อนเขียน DB (ไม่มีข้อมูลถูกแก้บางส่วน)"
  ],
  "depends_on": ["T-01", "T-03"]
}
```

### T-08 — service: ผลกระทบต่อ PR #137 + traffic stats

```json
{
  "task_id": "T-08",
  "title": "addrMatcher และ service categorizer รองรับหลาย entry ต่อ 1 object",
  "layer": "service",
  "files": ["backend/internal/service/policy_endpoint_labels.go", "backend/internal/service/traffic_stats.go"],
  "instruction": "**ทำหลัง PR #137 merge เข้า main แล้วเท่านั้น**. (1) policy_endpoint_labels.go: newAddrMatcher ปัจจุบันสร้าง addrRange 1 ช่วงต่อ 1 AddressObject จาก addr.Type/addr.Value ⇒ เปลี่ยนเป็นวน addr.Entries สร้าง addrRange 1 ช่วงต่อ 1 entry โดยทุกช่วงชี้ชื่อ object เดียวกัน; entry fqdn ยังข้ามเหมือนเดิม; ตรรกะ Match (ช่วงแคบสุดชนะ, tie-break ตามชื่อ) และการ normalize IPv4-mapped IPv6 ห้ามแก้; เพิ่ม comment ว่า object เดียวมีหลายช่วงได้และนั่นตั้งใจ; (2) traffic_stats.go: buildCategoryEntries สร้าง categoryEntry 1 ตัวต่อ 1 service entry (ชื่อ = ชื่อ object) protocol/port จาก entry นั้น (TCP/UDP ⇒ {6,17}, ICMP ⇒ isICMP), entry ที่ port spec ผิดข้ามเฉพาะ entry นั้น; ตรรกะ categorize และ cache TTL ห้ามแก้; ตรวจ ServiceNameFor ให้ถูกเมื่อ object มีหลาย entry; (3) grep หาที่อื่นใน service/ ที่อ่าน .Value/.Port ของ object โดยตรง ถ้ามีให้ปรับให้วน entries",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "object ที่มี 2 subnet คนละก้อน: IP ในทั้งสองก้อนถูก label เป็นชื่อ object เดียวกัน",
    "service object TCP/80 + TCP/443: flow ทั้งสองพอร์ตถูกจัดเป็น category ชื่อเดียวกัน",
    "กรณี object ค่าเดียว ผลลัพธ์เหมือนก่อนแก้ทุกประการ"
  ],
  "depends_on": ["T-01"]
}
```

### T-08B (ใหม่ ตาม D-3) — wiring ค่า config เข้า layer

```json
{
  "task_id": "T-08B",
  "title": "main.go: ส่ง cfg.MaxObjectEntries / cfg.MaxExpandedRulesPerPolicy เข้า repository และ kernel",
  "layer": "service",
  "files": ["backend/cmd/pigate/main.go"],
  "instruction": "ใน backend/cmd/pigate/main.go หลังจาก config ถูก resolve และหลังสร้าง repository/kernel manager ตามลำดับ startup เดิม (ห้ามย้ายลำดับ startup): (1) เรียก repo.SetObjectLimits(cfg.MaxObjectEntries) ในกลุ่มเดียวกับ SetMockMode/SetAllowEditSystemRoutes ที่มีอยู่แล้ว; (2) เมื่อเลือกใช้ real firewall manager ให้เรียก SetMaxExpandedRulesPerPolicy(cfg.MaxExpandedRulesPerPolicy) กับอินสแตนซ์ RealFirewall (ถ้าโค้ดถือเป็น interface อยู่ ให้เซ็ตตอนสร้าง real ก่อน assign เข้า interface — ห้ามเพิ่มเมธอดใน FirewallManager interface เพื่อการนี้); โหมด mock ไม่ต้องเซ็ต; (3) เขียน comment สั้น ๆ ว่าค่าทั้งสองมาจาก config key max-object-entries / max-expanded-rules-per-policy และมีผลเมื่อ restart เท่านั้น (plan §2.1). ห้ามลงทะเบียน flag ใหม่ (คีย์เป็น file-only โดยเจตนา)",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "ตั้ง max-object-entries=2 ใน config แล้วสร้าง object 3 รายการ ⇒ ถูก reject",
    "ตั้ง max-expanded-rules-per-policy=64 แล้ว apply policy ที่แตกเกิน ⇒ มี warning ใน log และ service ไม่ล้ม",
    "ไม่มี flag ใหม่ใน main.go และลำดับ startup เดิมไม่ถูกย้าย"
  ],
  "depends_on": ["T-00A", "T-03", "T-04"]
}
```

### T-09 — เทสต์ฝั่ง backend

```json
{
  "task_id": "T-09",
  "title": "เทสต์ multi-entry + config key ใหม่ + ซ่อมเทสต์เดิมที่พัง",
  "layer": "service",
  "files": [
    "backend/internal/config/config_test.go",
    "backend/internal/db/repository_test.go",
    "backend/internal/service/firewall_test.go",
    "backend/internal/service/backup_test.go",
    "backend/internal/service/policy_endpoint_labels_test.go",
    "backend/internal/service/traffic_stats_test.go",
    "backend/internal/api/handlers_test.go"
  ],
  "instruction": "เพิ่มเทสต์ใหม่และซ่อมเทสต์เดิม: (1) config: default 64/4096, ค่านอกช่วง clamp+warning, ค่าไม่ใช่ตัวเลข = error, Write ออกมาครบสองคีย์; (2) repository: CRUD หลาย entry, ลำดับ seq, reject (ว่าง/เกินเพดานที่ตั้งผ่าน SetObjectLimits/ซ้ำ/ค่าเพี้ยน), system lock, ลบตอนถูกอ้างอิง; (3) migration/backfill: DB ที่ seed แบบเก่า → InitDB ⇒ ได้แถวลูก seq=1 ครบ และรันซ้ำไม่เพิ่มแถว; (4) api: POST/PUT ทั้ง body เก่าและใหม่ + กรณี 400; (5) backup: **เทสต์ regression checksum** — ไฟล์ v2 เก่าที่ไม่มีคีย์ entries ต้อง import ผ่าน (ล้มทันทีถ้าใครลบ omitempty) + round-trip; (6) policy_endpoint_labels/traffic_stats ตาม acceptance ของ T-08; (7) firewall_test.go บรรทัดที่ตั้ง addr.Value ตรง ๆ เปลี่ยนไปใช้ Entries. ห้ามลด assertion เดิมเพื่อให้ผ่าน",
  "acceptance": [
    "cd backend && go test ./... ผ่านทั้งหมด",
    "มีเทสต์ที่ล้มทันทีถ้า omitempty ของ entries ถูกลบ",
    "มีเทสต์ backfill idempotent",
    "มีเทสต์ของ config key ใหม่ทั้งสองครบทั้ง default/clamp/error"
  ],
  "depends_on": ["T-00A", "T-00", "T-01", "T-02", "T-03", "T-04", "T-05", "T-06", "T-07", "T-08", "T-08B"]
}
```

### T-10 — เอกสาร API / config / deprecation

```json
{
  "task_id": "T-10",
  "title": "openapi + docs/data/firewall.md + pigate.conf.example + README",
  "layer": "api",
  "files": [
    "docs/openapi.yaml",
    "frontend/public/openapi.yaml",
    "docs/data/firewall.md",
    "pigate.conf.example",
    "README.md"
  ],
  "instruction": "(1) openapi ทั้งสองไฟล์ (ห้ามแตะ backend/internal/api/dist/openapi.yaml): เพิ่ม schema AddressEntry {type, value} และ ServiceEntry {protocol, port} + property entries (array, minItems 1) ใน AddressObject/AddressObjectInput/ServiceObject/ServiceObjectInput; มาร์ค type/value และ protocol/port ด้วย `deprecated: true` พร้อม description ว่าเป็น compat mirror ของ entries[0] **ที่มีแผนลบใน major version ถัดไป** และใน Input จะถูกใช้เมื่อไม่ส่ง entries เท่านั้น; ระบุใน description ของ entries ว่าจำนวนสูงสุดคุมด้วยคีย์ max-object-entries ใน pigate.conf (default 64); เนื้อหาสองไฟล์ต้องตรงกัน 100%; (2) docs/data/firewall.md: อัปเดต schema ให้มี DDL ตารางลูกทั้งสอง + ย่อหน้า 'Deprecation note' อธิบายว่าคอลัมน์ address_objects.type/value และ service_objects.protocol/port เป็น compat layer ชั่วคราว จะถูกลบใน major version ถัดไป พร้อมเหตุผล (SQLite รุ่นเก่าไม่รองรับ DROP COLUMN, คอลัมน์ NOT NULL + CHECK, เผื่อ downgrade ระหว่างเปลี่ยนผ่าน); (3) pigate.conf.example: เพิ่มสองคีย์ใหม่พร้อม comment (ค่า default, ช่วงที่ยอมรับ, ต้อง restart ถึงมีผล); (4) README.md: อัปเดตจำนวนคีย์ file-only ในย่อหน้า Configuration File ให้ตรงกับของจริงหลังเพิ่มสองคีย์",
  "acceptance": [
    "openapi ทั้งสองไฟล์ parse ได้และ diff เท่ากับศูนย์",
    "ฟิลด์ legacy ถูกมาร์ค deprecated: true พร้อมข้อความว่าจะถูกลบใน major version ถัดไป",
    "docs/data/firewall.md มี DDL ตารางลูกและย่อหน้า Deprecation note",
    "pigate.conf.example มี max-object-entries และ max-expanded-rules-per-policy พร้อมคำอธิบาย",
    "README ระบุจำนวนคีย์ถูกต้อง และ backend/internal/api/dist/openapi.yaml ไม่ถูกแก้"
  ],
  "depends_on": ["T-06", "T-07", "T-00A"]
}
```

### T-11 — frontend: type + service client

```json
{
  "task_id": "T-11",
  "title": "frontend types/API client รองรับ entries (+ normalize ข้อมูลเก่าใน localStorage)",
  "layer": "frontend",
  "files": [
    "frontend/src/data-mockup/mockData.ts",
    "frontend/src/services/addressService.ts",
    "frontend/src/services/serviceObjectService.ts",
    "frontend/src/services/mockSync.ts"
  ],
  "instruction": "(1) mockData.ts: เพิ่ม type AddressEntry {type: 'subnet'|'range'|'fqdn'; value: string} และ ServiceEntry {protocol: 'TCP'|'UDP'|'TCP/UDP'|'ICMP'; port: string}; เพิ่ม entries ใน AddressObject/ServiceObject (คงฟิลด์ value/protocol/port เดิมพร้อม comment ว่า deprecated: mirror ของ entries[0] จะถูกลบใน major version ถัดไป) และอัปเดต seed mock ให้มีอย่างน้อย 1 object หลายรายการ; (2) addressService.ts / serviceObjectService.ts: helper normalize ที่ใช้ทั้งใน mock mode และหลัง fetch — ถ้าไม่มี entries ให้เติมจากค่า legacy (Caution 9) และตอน create/update ส่ง entries ขึ้น API พร้อม sync ค่า legacy ในข้อมูล mock; (3) mockSync.ts: การ mark refPolicies ที่ match addr.value ให้วนทุก entry (match ชื่อยังเป็นหลัก); propagateAddressRename/propagateServiceRename ห้ามเปลี่ยนพฤติกรรม",
  "acceptance": [
    "cd frontend && yarn build ผ่าน และ yarn lint ไม่มี error ใหม่",
    "โหลดหน้าโดย localStorage มีข้อมูลเก่า (ไม่มี entries) แล้วไม่ crash",
    "mock mode สร้าง/แก้ object หลายรายการแล้วค่าคงอยู่หลัง reload"
  ],
  "depends_on": ["T-10"]
}
```

### T-12 — frontend: หน้า Addresses (ฟอร์มแบบ list)

```json
{
  "task_id": "T-12",
  "title": "Addresses.tsx: ฟอร์มรายการ (เพิ่ม/ลบแถว) + ตาราง/สถิติ/ฟิลเตอร์",
  "layer": "frontend",
  "files": ["frontend/src/pages/Addresses.tsx"],
  "instruction": "(1) เปลี่ยนฟอร์มใน Drawer เป็นรายการ dynamic: แต่ละแถวมี select ประเภท (subnet/range/fqdn) + Input ค่า + ปุ่มลบแถว และมีปุ่ม 'เพิ่มรายการ'; ต้องมีอย่างน้อย 1 แถว (ลบแถวสุดท้ายไม่ได้); เพดานจำนวนแถวฝั่ง UI ให้ใช้ค่าคงที่ฝั่ง client ได้ **แต่ต้องรองรับกรณี backend ปฏิเสธเพราะเพดานจาก config ต่ำกว่า** โดยแสดง error จาก API ให้ผู้ใช้เห็นตรง ๆ (อย่าเงียบ); (2) validate ต่อแถวด้วย isValidCidr/isValidIpRange/regex FQDN เดิม แสดง error ระบุแถวที่ผิด และตรวจรายการซ้ำในฟอร์ม; (3) ตาราง: คอลัมน์ 'Details / Value' แสดงทุกรายการ (เกิน 3 ย่อเป็น '+N more'); คอลัมน์ Type แสดง badge เดิมถ้าชนิดเดียวกันทั้งหมด และ 'Mixed' เมื่อปนกัน (D-1); (4) stat cards + ตัวกรองตามชนิด เปลี่ยนความหมายเป็น 'object ที่มีรายการชนิดนั้นอย่างน้อย 1' พร้อมแก้ข้อความให้ผู้ใช้เข้าใจ; (5) ค้นหาต้องเจอจากค่าของทุกรายการ; (6) ยึดกฎ UI: ใช้เฉพาะ components/ui/*, ห้าม hardcode สี (ใช้ theme variable), ห้าม shadow-*/backdrop-blur-*, รองรับ dark/light; Drawer นี้ไม่มี Combobox จึงไม่ต้องใส่ modal={false}",
  "acceptance": [
    "cd frontend && yarn build ผ่าน, yarn lint ไม่มี error ใหม่",
    "เพิ่ม/ลบแถวได้, บันทึก object 3 รายการแล้วกลับมาแก้เห็นครบ 3 แถว",
    "แถวเดียวลบไม่ได้ และ error จาก backend เรื่องเพดานถูกแสดงให้ผู้ใช้เห็น",
    "badge แสดง 'Mixed' เมื่อชนิดปนกัน",
    "ไม่มี class สีดิบ/shadow/backdrop-blur เพิ่มเข้ามา และดูดีทั้ง dark/light"
  ],
  "depends_on": ["T-11"]
}
```

### T-13 — frontend: หน้า Services (ฟอร์มแบบ list)

```json
{
  "task_id": "T-13",
  "title": "Services.tsx: ฟอร์มรายการ protocol+port หลายแถว",
  "layer": "frontend",
  "files": ["frontend/src/pages/Services.tsx"],
  "instruction": "ทำแบบเดียวกับ T-12 แต่เป็น service object: แต่ละแถว = select protocol (TCP/UDP/TCP/UDP/ICMP) + Input port (เลขเดี่ยวหรือช่วง start-end; เลือก ICMP ⇒ ล็อกค่าเป็น '-' และ disable ช่อง port ตามพฤติกรรมเดิม) + ปุ่มลบแถว, มีปุ่มเพิ่มแถว, อย่างน้อย 1 แถว, และแสดง error จาก backend เมื่อชนเพดานจาก config; validate ต่อแถวด้วยกฎเดิมและตรวจรายการซ้ำ; ตารางแสดงทุกรายการ (เกิน 3 ย่อ '+N more') คอลัมน์ Protocol แสดง 'Mixed' เมื่อปนกัน; ตัวกรอง TCP/UDP/TCP-UDP/ICMP = 'มีรายการโปรโตคอลนั้นอย่างน้อย 1'; ค้นหาครอบคลุมทุกรายการ; ยึดกฎ UI เดียวกับ T-12",
  "acceptance": [
    "cd frontend && yarn build ผ่าน, yarn lint ไม่มี error ใหม่",
    "สร้าง service object ที่มี TCP/80 + TCP/443 + UDP/53 ได้ และแก้กลับมาเห็นครบ",
    "แถว ICMP บังคับ port เป็น '-' เหมือนเดิม",
    "badge แสดง 'Mixed' เมื่อโปรโตคอลปนกัน"
  ],
  "depends_on": ["T-11"]
}
```

### T-14 — เอกสารปิดงาน

```json
{
  "task_id": "T-14",
  "title": "อัปเดต README/สถานะฟีเจอร์ และย้ายแผนเข้า complete",
  "layer": "db",
  "files": ["README.md", "docs/ref/todo/multi-value-address-service-objects-plan.md"],
  "instruction": "อัปเดตข้อความที่กล่าวถึง Address/Service Object ให้ระบุว่ารองรับหลายค่าต่อ 1 ชื่อแล้ว และมีคีย์ config ใหม่สองตัว; ตรวจว่าหมายเหตุ deprecation ของคอลัมน์เดิม (D-4) ปรากฏครบทั้งใน model/types.go, db/connection.go, docs/data/firewall.md และ openapi ทั้งสองไฟล์; เมื่อ ai-qa ผ่านครบทุกข้อใน Final Acceptance แล้วจึงย้ายไฟล์แผนนี้ไป docs/ref/complete/ พร้อมเปลี่ยนบรรทัดสถานะด้านบนเป็น 'เสร็จแล้ว' และสรุปผลทดสอบสั้น ๆ. **ห้ามย้ายไฟล์ก่อน QA ผ่าน**",
  "acceptance": [
    "README ไม่มีข้อความที่ขัดกับความสามารถใหม่ และระบุคีย์ config ใหม่ครบ",
    "หมายเหตุ deprecation ปรากฏครบทั้ง 4 จุด",
    "ไฟล์แผนถูกย้ายเข้า docs/ref/complete/ หลัง QA ผ่านเท่านั้น"
  ],
  "depends_on": ["T-12", "T-13"]
}
```

---

## 6. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... และ go test ./... ผ่านทั้งหมด; cd frontend && yarn build + yarn lint ผ่าน (ไม่มี error ใหม่)",
    "อัปเกรดจาก DB เดิมของผู้ใช้ (object ค่าเดียว + policy ที่อ้างอยู่): ข้อมูลครบทุก object, ทุก object มี 1 entry ตรงค่าเดิม, policy ทุกข้อยัง apply ได้ ไม่มีข้อมูลหาย",
    "รัน InitDB ซ้ำหลายรอบบน DB เดียวกันแล้วแถวใน address_object_values/service_object_ports ไม่เพิ่มซ้ำ (idempotent)",
    "สร้าง address object 'Office_Servers' 3 รายการ (subnet + /32 + range) และ service object 'Web_Ports' (TCP/80 + TCP/443 + TCP/8000-8010) ผ่าน UI ได้ และแก้ไข/ลบรายการย่อยได้",
    "1 object ที่ปนชนิด (subnet + range) และ service object ที่ปนโปรโตคอล (TCP + UDP) ใช้งานได้จริงและ badge แสดง 'Mixed' (D-1)",
    "policy rule ที่ใช้ object ข้างต้น เมื่อ Apply แล้ว nft ruleset มีกฎครบทุกคู่ผสม, ทุกกฎมี rule id เดียวกันใน UserData, และโครงสร้าง 4 section ของทั้ง input/forward/output chain ยังเรียงเหมือนเดิม",
    "สถิติ per-rule (Top Rules / Rule usage) ของ policy ที่แตกเป็นหลายกฎ แสดงเป็นตัวเลขรวมของ rule เดียว ไม่แตกเป็นหลายบรรทัด",
    "ฟีเจอร์ matched endpoints (PR #137): IP ที่อยู่ในรายการใดก็ได้ของ object ถูก label เป็นชื่อ object นั้น และ traffic category ของ service object หลายพอร์ตรวมเป็นชื่อเดียว",
    "**config key ปรับได้จริง (D-3)**: ไม่ตั้งคีย์ในไฟล์ ⇒ ใช้ค่า default 64 / 4096; ตั้ง max-object-entries=2 ใน pigate.conf แล้ว restart ⇒ สร้าง object 3 รายการถูกปฏิเสธพร้อมข้อความชัดเจนบน UI; ตั้ง max-expanded-rules-per-policy=64 ⇒ policy ที่แตกเกินมี warning ใน log และ service ไม่ล้ม; ค่านอกช่วง/ค่าที่ไม่ใช่ตัวเลข ทำงานตาม two-tier validation (clamp+warn / error) และ gateway ยัง boot ขึ้น",
    "ไม่มีเลขเพดาน 64/4096 ถูก hardcode เป็น const ที่ใช้บังคับใช้จริงนอก internal/config (ยกเว้นค่า default ที่ระบุไว้ในแผน)",
    "pigate.conf.example และ README ระบุคีย์ใหม่ทั้งสองพร้อมช่วงค่าและข้อความว่าต้อง restart ถึงมีผล",
    "**หมายเหตุ deprecation (D-4) ครบทุกจุด**: comment ใน model/types.go (ขึ้นต้น 'Deprecated:'), comment ใน db/connection.go ตรง CREATE TABLE/migration, ย่อหน้าใน docs/data/firewall.md, และ `deprecated: true` + คำอธิบายใน openapi ทั้งสองไฟล์ — ทุกจุดต้องระบุว่าเป็น compat layer ชั่วคราวที่จะถูกลบใน major version ถัดไป พร้อมเหตุผล (SQLite ไม่รองรับ DROP COLUMN ง่าย ๆ, คอลัมน์ NOT NULL + CHECK, เผื่อ downgrade)",
    "ลบ object ที่ถูก policy อ้างอิงยังถูกบล็อก, object ระบบ (ALL/HTTP/HTTPS/SSH/DNS/ICMP) ยังแก้/ลบไม่ได้",
    "Validation fail-closed: entries ว่าง / เกินเพดาน / ซ้ำ / CIDR-range-port ผิดรูปแบบ ถูกปฏิเสธทั้งที่ UI, API และ backup import โดยไม่มีการเขียน DB บางส่วน",
    "Backup: ไฟล์ที่ export จากรุ่นก่อนหน้า import ได้ (checksum ผ่าน) และได้ object ค่าเดียวถูกต้อง; export ใหม่ → import กลับ ได้ entries ครบตามลำดับ",
    "API เดิมยังใช้ได้: POST/PUT /api/addresses และ /api/services ด้วย payload แบบเก่า {type,value} / {protocol,port} ยังสำเร็จ และ response มีทั้ง entries และฟิลด์ legacy ที่ตรงกับรายการแรก",
    "FQDN entry ที่ resolve ไม่ได้ ทำให้ข้ามเฉพาะรายการนั้น ระบบยัง apply กฎที่เหลือได้และมี log warning ระบุ object/ค่าชัดเจน",
    "หน้า Addresses/Services: ค้นหาเจอจากค่าในทุกรายการ, ตัวกรองและ stat cards สอดคล้องกับความหมายใหม่, ใช้งานได้ทั้ง dark/light และไม่มี shadow-*/backdrop-blur-*/สี hardcode เพิ่มเข้ามา",
    "โหมด mock (-mock=true) ใช้งานได้ครบทุกหน้าโดยไม่ crash แม้ localStorage มีข้อมูลเก่าที่ไม่มี entries",
    "ไม่มี exec.Command หรือ dependency ใหม่ถูกเพิ่ม และไม่มีการแก้ backend/internal/api/dist/openapi.yaml"
  ]
}
```

---

## 7. หมายเหตุการทำงานร่วมกับทีม

- ai-developer ทำ T-00A → T-14 ตามลำดับ dependency **ไม่ต้องหยุดทดสอบรวมระหว่างทาง** (แต่ต้อง `go build`/`yarn build` ผ่านทุก task)
- ทำครบทุก task แล้วจึงส่ง ai-qa ทดสอบตาม §6 รอบเดียว
- T-03, T-04, T-07 เป็น **security-sensitive** (input validation, การ generate กฎไฟร์วอลล์, import ไฟล์จากภายนอก)
  ⇒ review เข้มเป็นพิเศษ ห้ามผ่อนกฎ validate เพื่อให้เทสต์ผ่าน
- ถ้า QA แก้ครบ 3 รอบแล้วยังไม่ผ่าน ให้ส่ง error log กลับมาที่ tech lead เพื่อออกแบบแผนใหม่ (แผนสำรองที่เตรียมไว้คือ
  เปลี่ยน T-04 ไปใช้ nftables anonymous set ซึ่งลดจำนวนกฎเหลือ 1 ต่อคู่ผสม — ต้องขออนุมัติเจ้าของก่อนเพราะขัดกับ D-2)
