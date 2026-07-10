# Interface Alias as Primary Display — ใช้ alias สื่อสารกับผู้ใช้แบบ FortiGate

> แผนงาน: เปลี่ยนการแสดง interface ทุกหน้าให้ **alias เด่น** เป็น `"alias (name)"` ผ่าน helper กลาง
> (ยุบเหลือ `name` เดี่ยวเมื่อ alias == name หรือว่าง), บังคับ **alias ไม่ซ้ำ (case-insensitive)** และ
> **alias ห้ามชนกับ OS name ของ interface อื่น**, ย้าย validation รูปแบบ alias ไปฝั่ง server
> **คงการเก็บ OS name เป็น key อ้างอิงเดิมทุกที่** (firewall/dhcp/qos/routing/dns ไม่แตะ) และ event log คง name อย่างเดียว
>
> เขียนเมื่อ: 2026-07-10 · ตรวจทานกับโค้ดจริงหลัง merge #24 เข้า branch: 2026-07-10
> Reference branch: `main` (จะทำงานบน `feat/interface-alias-primary-display`)
> Issue: https://github.com/saprayworld/pigate/issues/25
> เกี่ยวเนื่องกับงาน VLAN (#20, PR #24) ที่เพิ่ง merge — VLAN มี default alias = name เช่นกัน

## 0. Goal and Scope

**Goal (พฤติกรรมที่ผู้ใช้เห็น):**
- ทุกหน้าที่ list/select interface (Firewall, DHCP Server, QoS, DNS Server, Static Routes, Interfaces, Dashboard)
  แสดง `alias (name)` เหมือนกันหมด เช่น `LAN_Internal (eth0)`, `VLAN_Guest (eth0.100)` — ถ้า alias == name หรือว่าง
  ให้โชว์ `name` เดี่ยว ๆ (ไม่ให้ได้ `eth0 (eth0)`)
- ตั้ง alias ซ้ำกัน (ตัวพิมพ์เล็ก/ใหญ่ถือว่าซ้ำ) หรือชนกับชื่อจริงของ interface อื่นไม่ได้ — ระบบปฏิเสธพร้อมข้อความชัดเจน
- กติกาถูก enforce **ฝั่ง server** (ไม่ใช่แค่ฟอร์ม edit หน้าเดียว) ครอบทั้ง PUT/PATCH/สร้าง VLAN/import

**เงื่อนไขทางเทคนิค:** ค่าที่เก็บใน DB และที่ dropdown ส่ง (`value`) ยังเป็น **OS name** เดิม — เปลี่ยนแค่ข้อความที่แสดง

**Out of scope:**
- ไม่เปลี่ยน reference model: firewall `in/out_interface`, dhcp/qos/route `interface`, dns server `interfaces`
  ยังเก็บ/ใช้ OS name (kernel ต้องใช้ชื่อจริง; alias เปลี่ยนได้จึงเป็น FK ไม่ได้) — เหมือน FortiGate ที่เก็บ internal name
- **Event log / audit message คง `iface.Name` อย่างเดียว** (เพื่อ traceability — ตัดสินใจแล้ว)
- ไม่ทำ rename-cascade, ไม่แตะ API response shape (alias มีใน `NetworkInterface` อยู่แล้ว)
- ไม่เปลี่ยนกฎรูปแบบ alias เดิม (`^[a-zA-Z0-9_]+$`) — แค่ย้ายที่ตรวจ

## 1. Current State (สำรวจโค้ดจริง ณ 2026-07-10)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| FE: label logic เขียน inline ซ้ำแต่ละหน้า ไม่สอดคล้อง (บาง alias-first บาง name-first) | ต้องรวม | ดูตารางล่าง |
| FE: helper กลางสำหรับ label ยังไม่มี (`lib/utils.ts` มีแค่ `isValidIp`) | ขาด | `frontend/src/lib/utils.ts` |
| FE: DhcpServer เก็บ interface เป็น `string[]` (ชื่อล้วน) ไม่มี object → โชว์ alias ไม่ได้ทันที | ต้องแก้ | `frontend/src/pages/DhcpServer.tsx:88,879` |
| FE: ตรวจ alias (regex + ซ้ำ case-insensitive ยกเว้นตัวเอง) เฉพาะฟอร์ม edit | บางส่วน | `frontend/src/pages/Interfaces.tsx:596-606` |
| BE: ไม่มี validation alias ฝั่ง server เลย — PUT ทับตรง ๆ (`iface.Alias = updates.Alias`) **client ที่ไม่ส่ง field alias จะได้ `""` ลง DB** | ขาด | `handlers.go:501` (PUT), `:693` (PATCH) |
| BE: `ApplyInterfaceConfig` มี caller แค่ PUT/PATCH สองจุด (ยืนยันแล้ว) → wire validate จุดเดียวครอบ | เสร็จ | `handlers.go:576,748` |
| BE: `CreateVlanInterface` ตั้ง default alias = name แต่ไม่เช็คซ้ำ | ต้องเพิ่ม | `backend/internal/service/interface.go:607` |
| DB: `alias TEXT NOT NULL` **ไม่ UNIQUE**, ไม่มี index | ขาด | `backend/internal/db/connection.go:360` |
| DB: มี pattern migration `ADD COLUMN` + `queries` CREATE ให้ลอก | เสร็จ | `connection.go:194-215` |
| Reference: firewall in/out → `iifname`/`oifname` เก็บ OS name | คงไว้ | `kernel/real_firewall.go:499` (`buildRuleExpressions`) |
| Reference: dhcp/qos/route/dns server เก็บ OS name | คงไว้ | หลายไฟล์ |

**FE label ที่ต้องรวมเป็น helper เดียว:**

| หน้า | ตอนนี้แสดง | อ้างอิง |
|---|---|---|
| FirewallPolicy | `alias (name)` alias-first แต่**ไม่ยุบ**เมื่อ alias==name (ได้ `eth0 (eth0)`) + มี logic ค่า `ALL` ที่ต้องคงไว้ | `FirewallPolicy.tsx:111-116` (`ifaceLabel`) |
| StaticRoutes | `name (alias\|\|role)` (name-first) | `StaticRoutes.tsx:807` |
| QoS | `NAME (alias\|\|role)` + `alias\|\|type` | `QoS.tsx:675,348` |
| DnsServer | `name (alias)` ยุบเมื่อเท่ากัน (ใกล้เป้าหมายสุด) | `DnsServer.tsx:483-485` |
| Interfaces (การ์ด) | name ตัวใหญ่ + `(alias)` เล็ก | `Interfaces.tsx` (Name cell) |
| Dashboard | `name` เดี่ยว | `Dashboard.tsx:537` |
| DhcpServer | `name` เดี่ยว (string[]) | `DhcpServer.tsx:879` |

**สรุป:** งานจริงคือ (1) helper กลาง + แทนที่ 7 จุด, (2) migration unique index + de-dup, (3) validation ฝั่ง server
3 กติกา (รูปแบบ/ซ้ำ/ชน-name-อื่น), (4) map error เป็น 400/409 ใน 3 handler + import — **ไม่แตะ security path**

## 2. Technical Approach

**Frontend — helper กลาง** `frontend/src/lib/ifaceLabel.ts` (ไฟล์ใหม่):
```ts
// รับ list มาเพื่อ resolve name -> alias; ยุบเมื่อ alias ว่าง/เท่ากับ name
export function formatIfaceLabel(name: string, ifaces: {name:string; alias?:string}[]): string {
  const f = ifaces.find(i => i.name === name)
  return f?.alias && f.alias !== f.name ? `${f.alias} (${f.name})` : name
}
```
แทนที่ logic inline ทั้ง 7 จุด (ยกโครงจาก `DnsServer.tsx:484` ที่ยุบ-เมื่อ-เท่ากัน มาเป็นมาตรฐาน)
**DhcpServer** ต้องโหลด interface objects เพิ่ม (มี `interfaceService.getAll()` แล้วในหน้าอื่น) แล้วส่งเข้า helper

**Backend — normalize + validation ฝั่ง service** เพิ่มใน `service/interface.go`:
```go
var ErrAliasConflict = errors.New("interface alias already in use")
var ErrAliasInvalid  = errors.New("invalid interface alias")
// normalize ก่อนเสมอ: TrimSpace แล้วถ้าว่าง → ใช้ name (เหมือน CreateVlanInterface :607-610 ทำอยู่แล้ว)
//   — สำคัญ: PUT เดิมปล่อย "" ลง DB ได้ ถ้า reject แทน normalize จะเป็น breaking change
// validateAlias: regex ^[A-Za-z0-9_]+$; ไม่ซ้ำ alias อื่น (repo, COLLATE NOCASE);
// ไม่ตรงกับ OS name ของ interface อื่น (kernel list, strings.EqualFold ให้ตรงกับ index NOCASE);
// ยกเว้นตัวเอง: selfID สำหรับ row ใน DB, selfName สำหรับ alias == ชื่อตัวเอง (ค่า default ต้องผ่านเสมอ)
func (s *InterfaceService) validateAlias(alias, selfID, selfName string) error { ... }
```
เรียกใน `ApplyInterfaceConfig` (ครอบ PUT/PATCH — ยืนยันแล้วว่าเป็น caller เพียงสองจุด `handlers.go:576,748`)
และ `CreateVlanInterface` (หลัง normalize เดิม ก่อน `CreateVlan`)
เทียบ alias↔alias ผ่าน repo (query DB), เทียบ alias↔name ผ่าน `GetKernelInterfaces()`

**DB — unique index (ไม่ใช่ table-rebuild)** ใน `connection.go` หลังบล็อก vlan migration (~214, ก่อน `queries :=` ~216):
```sql
-- pass 0: alias ว่าง → ตั้งเป็น name (PUT เดิมปล่อย '' ลง DB ได้ — หลายแถวว่างพร้อมกัน index จะ fail)
UPDATE network_interfaces SET alias = name WHERE TRIM(alias) = '';
-- pass 1 (ทำใน Go, ไม่ใช่ SQL ล้วน): แถวที่ alias ซ้ำ (NOCASE) นอกจากแถวแรก → ตั้งกลับเป็น name ของตัวเอง
--   (ค่า default; name UNIQUE จึงเกือบชัวร์) ถ้ายังชนอีก (มี row อื่น alias เป็นชื่อนั้นพอดี) วน _2, _3 ... จน unique
--   ทุกแถวที่ถูกแก้ log warning
CREATE UNIQUE INDEX IF NOT EXISTS idx_network_interfaces_alias
  ON network_interfaces(alias COLLATE NOCASE);
```

- **ทำไมทางนี้:** เก็บ OS name เป็น key เดิม = ตรงกับที่ kernel ต้องใช้ + ตรงกับที่ FortiGate ทำ (เก็บ internal name,
  โชว์ alias); index `COLLATE NOCASE` ให้ uniqueness ตรงกับ client เดิมที่เทียบ `.toLowerCase()`
- **ทางเลือกที่ตัดทิ้ง:**
  - เก็บ alias เป็น FK แล้ว resolve เป็น name ตอน apply — ตัด: rename cascade พัง policy/config ทั้งหมด, kernel ต้องใช้ name จริง
  - บังคับ unique เฉพาะ client — ตัด: import/API bypass ได้ ไม่ authoritative
  - `ALTER TABLE ... ADD UNIQUE` / table-rebuild — ตัด: SQLite ไม่รองรับ add constraint, unique index ง่ายและ idempotent กว่า
- **Template ที่ลอก:** sentinel error + handler switch ตามสไตล์ `ErrVlanExists/ErrVlanInvalid` (งาน #20),
  migration ตาม pattern คอลัมน์ `vlan_parent` (`connection.go:194-210`)

## 3. Steps (เรียงจากชั้นในสุดออกนอก)

### Step 1 — DB: normalize ว่าง + de-dup + unique index
**File:** `backend/internal/db/connection.go` (หลังบล็อก vlan migration ~214, ก่อน `queries :=` ~216)
- pass 0: `UPDATE ... SET alias = name WHERE TRIM(alias) = ''` (กันหลายแถวว่างชนกันเอง)
- pass 1 (Go): SELECT หา alias ซ้ำแบบ NOCASE → แถวที่ซ้ำ (นอกจากตัวแรก) ตั้งกลับเป็น `name` ของตัวเอง,
  ถ้ายังชน วนต่อท้าย `_2`, `_3`, ... จน unique — log warning ทุกแถวที่แก้
- `CREATE UNIQUE INDEX IF NOT EXISTS idx_network_interfaces_alias ON network_interfaces(alias COLLATE NOCASE)`
- CREATE TABLE ใหม่ (~360) ไม่ต้องแตะ (index สร้างแยก idempotent ใช้ได้ทั้ง new + existing)

### Step 2 — Repo: helper เช็ก alias ซ้ำ
**File:** `backend/internal/db/repository.go`
- `AliasExists(alias, excludeID string) (bool, error)` — `SELECT 1 ... WHERE alias = ? COLLATE NOCASE AND id != ?`

### Step 3 — Service: normalize + validateAlias + wire
**File:** `backend/internal/service/interface.go`
- เพิ่ม `ErrAliasConflict`, `ErrAliasInvalid` (ใกล้ `ErrVlan*` ที่ :19-20)
- ต้น `ApplyInterfaceConfig` (:416): normalize — `TrimSpace`; ถ้าว่าง → `iface.Alias = iface.Name`
  (PUT ที่ไม่ส่ง alias ต้องไม่พัง/ไม่ได้ `""`; PATCH ไม่กระทบเพราะแก้เฉพาะ key ที่ส่งมา)
- `validateAlias(alias, selfID, selfName)`: regex; `repo.AliasExists` NOCASE (409); loop kernel list —
  ถ้า `strings.EqualFold(alias, name-อื่น)` และ name นั้น != selfName → conflict (alias == ชื่อตัวเองต้องผ่านเสมอ)
- เรียกใน `ApplyInterfaceConfig` (หลัง normalize) และ `CreateVlanInterface` (หลัง normalize เดิมที่ :607-610, ก่อน CreateVlan)

### Step 4 — Handlers: map error
**File:** `backend/internal/api/handlers.go`
- `HandleUpdateInterface` (:482, err จาก Apply ที่ :576) + `HandlePatchInterface` (:633, err ที่ :748)
  + `HandleCreateVlan` (:601, เพิ่ม case ใน switch `ErrVlan*` เดิมที่ :611-614): ถ้า err เป็น
  `ErrAliasConflict` → 409, `ErrAliasInvalid` → 400 (ตาม pattern switch ของ vlan)
> Reset flow ไม่ต้องแก้: reset ตั้ง alias = name ซึ่ง unique อยู่แล้ว (name UNIQUE) และผ่านกติกา not-equal-other-name เสมอ

### Step 5 — Backup/Restore: กัน alias ซ้ำตอน import
**File:** `backend/internal/service/backup.go` (`resolveInterfaces`)
- backup อาจพก alias ว่าง/ซ้ำ/ชน name มา → ถ้าปล่อยไป unique index จะทำ transaction restore ล้มทั้งก้อน
- normalize + dedup ใน merged rows แบบเดียวกับ Step 1 (ว่าง → name; ซ้ำ → name ของตัวเอง + วน `_2`...)
  พร้อมเพิ่ม warning ก่อนส่งเข้า `RestoreConfig`

### Step 6 — FE helper กลาง + แทนที่ทุกจุด
**File (ใหม่):** `frontend/src/lib/ifaceLabel.ts` — `formatIfaceLabel(name, ifaces)`
**Files:** แทนที่ inline ที่ `FirewallPolicy.tsx:111-116` (คงเช็คค่า `ALL` ไว้ที่หน้า — helper ไม่ต้องรู้จัก),
`StaticRoutes.tsx:806-808`, `QoS.tsx:675,348`, `DnsServer.tsx:483-486`,
`Interfaces.tsx` (Name cell :790 + drawer title :1059 + VLAN parent dropdown :1613), `Dashboard.tsx:537`
**File:** `DhcpServer.tsx` — โหลด interface objects (เพิ่ม state จาก `interfaceService.getAll()`) แล้วใช้ helper
ที่ `:879`; `value`/`key` ของ option ยังเป็นชื่อจริงเหมือนเดิม

### Step 7 — FE Interfaces edit form: กติกาให้ครบ + รับ error server
**File:** `frontend/src/pages/Interfaces.tsx`
- เพิ่มเช็ค client: alias ห้ามตรง `name` ของ interface อื่น (เสริมจาก dup เดิม ~603) — และแสดง error 409/400 จาก server
- ฟอร์ม Create VLAN (drawer) เพิ่มเช็คเดียวกัน

### Step 8 — OpenAPI (sync สองไฟล์)
**Files:** `docs/openapi.yaml` + `frontend/public/openapi.yaml`
- เพิ่ม response `409` (alias conflict) ให้ PUT/PATCH `/interfaces/{id}` และ POST `/interfaces/vlan`
- อัปเดตคำอธิบาย field `alias` ใน schema `NetworkInterface` ว่า unique (case-insensitive) และห้ามชื่อชนกับ OS name อื่น

> **ไม่ต้องทำ:** ไม่แตะ `real_firewall.go`/kernel apply/route/dhcp/qos (reference ยังเป็น OS name),
> ไม่แตะ event log (คง name), ไม่แตะ `install.sh`/Polkit, ไม่เพิ่ม dependency, ไม่แตะ API response shape

## 4. Related API

| Method | Path | Role | พฤติกรรมที่เปลี่ยน | สถานะ |
|---|---|---|---|---|
| PUT/PATCH | `/api/interfaces/{id}` | super_admin | เพิ่ม validate alias → 409 (ซ้ำ/ชน name) / 400 (รูปแบบ) | แก้ไข |
| POST | `/api/interfaces/vlan` | super_admin | เพิ่ม validate alias เช่นเดียวกัน | แก้ไข |
| GET/toggle/reset | `/api/interfaces/...` | เดิม | ไม่เปลี่ยน (reset ตั้ง alias=name ปลอดภัย) | เดิม |

`-disable-edit=true`: PUT/PATCH/POST ถูก `DisableEditMiddleware` บล็อกอยู่แล้ว — ถูกต้อง ไม่ต้องแก้

## 5. Cautions

1. **Migration normalize+de-dup ต้องมาก่อนสร้าง index** — ข้อมูลเดิมอาจมีทั้ง alias ซ้ำ (user ตั้งเอง) **และ
   alias ว่างหลายแถว** (PUT เดิมทับด้วย `""` ได้เมื่อ client ไม่ส่ง field — `handlers.go:501`) ถ้ารัน
   `CREATE UNIQUE INDEX` เลย จะ error → **boot ล้มทั้งเครื่อง**. ป้องกัน: pass ว่าง→name แล้ว de-dup (→ own name,
   วน `_2`...) ก่อนเสมอ + log
2. **Case-sensitivity ต้องตรงกันทุกที่** — index เป็น `COLLATE NOCASE` แต่ถ้า `validateAlias` เทียบ case-sensitive
   → server ปล่อยผ่านแต่ index reject = 500 แทน 409. ป้องกัน: `AliasExists` ใช้ `COLLATE NOCASE` และ
   เช็ค alias↔name ใช้ `strings.EqualFold` ให้ตรงกับ index
2b. **PUT ที่ไม่ส่ง alias ต้องไม่กลายเป็น 400** — semantics เดิมของ PUT คือทับทั้ง object; client เดิม/script
   ที่ไม่ส่ง alias เคยผ่าน ถ้า validate ตรง ๆ จะ reject. ป้องกัน: normalize ว่าง→name (พฤติกรรม default
   เดียวกับ VLAN create) ไม่ใช่ reject; ระบุใน OpenAPI ด้วย
3. **alias == OS name ของ interface อื่น index จับไม่ได้** — unique index เทียบแค่ alias↔alias ไม่เทียบ alias↔name
   → label กำกวม `eth0 (eth1)`. ป้องกัน: เช็ค alias↔kernel-name แยกใน `validateAlias` (เทียบจาก `GetKernelInterfaces`
   จึงครอบ interface ที่ unmanaged/ไม่มีใน DB ด้วย)
4. **Import ที่มี alias ซ้ำ/ชน = restore ล้มทั้งก้อน** — `RestoreConfig` เป็น single transaction; unique index ทำให้
   INSERT/UPDATE ที่ซ้ำ fail แล้ว rollback ทั้งหมด. ป้องกัน: Step 5 dedup ใน `resolveInterfaces` ก่อน restore
5. **Reference ต้องไม่เผลอเปลี่ยนไปใช้ alias** — จุดที่เสี่ยงพลาดคือ FE dropdown: ต้องคง `value={iface.name}`
   เปลี่ยนแค่ข้อความใน `<option>`/`<SelectItem>`. ถ้าเผลอส่ง alias เป็น value → firewall/dhcp/qos พังเงียบ ๆ
   (nft `iifname LAN_Internal` ไม่ match อะไร). ป้องกัน: helper คืน "label" อย่างเดียว, ไม่ยุ่งกับ value; ทดสอบสร้าง
   policy แล้วดูว่า `in_interface` ที่เก็บยังเป็น `eth0`
6. **VLAN default alias = name** — VLAN ใหม่ (`eth0.100`) alias = name → helper ยุบเหลือ `eth0.100` จนกว่า user ตั้ง alias
   สื่อความ (เช่น `VLAN_Guest`) — พฤติกรรมถูกต้อง สอดคล้องงาน #20
7. **Frontend mock mode** — `interfaceService.update`/`createVlan` (mock branch, localStorage) ควรเช็ค alias ซ้ำ
   ให้สอดคล้อง เพื่อ demo/dev เห็นพฤติกรรมเดียวกับ real (ไม่งั้น mock ปล่อยผ่านแต่ real 409)

## 6. Summary Checklist (Definition of Done)

- [ ] `db/connection.go` — pass ว่าง→name + de-dup pass + `CREATE UNIQUE INDEX ... (alias COLLATE NOCASE)`
- [ ] `db/repository.go` — `AliasExists(alias, excludeID)` (NOCASE)
- [ ] `service/interface.go` — normalize (ว่าง→name) ใน `ApplyInterfaceConfig` + `ErrAliasConflict`/`ErrAliasInvalid` + `validateAlias(alias, selfID, selfName)` + wire ใน `ApplyInterfaceConfig`/`CreateVlanInterface`
- [ ] `api/handlers.go` — map error 409/400 ใน `HandleUpdateInterface`/`HandlePatchInterface`/`HandleCreateVlan`
- [ ] `service/backup.go` — dedup alias ใน `resolveInterfaces` ก่อน restore
- [ ] `frontend/src/lib/ifaceLabel.ts` — helper `formatIfaceLabel` (ยุบเมื่อ alias ว่าง/เท่ากับ name)
- [ ] แทนที่ label ทุกจุด: FirewallPolicy, StaticRoutes, QoS(×2), DnsServer, Interfaces, Dashboard, DhcpServer (+โหลด objects)
- [ ] `Interfaces.tsx` — เช็ค alias≠name-อื่น (client) + แสดง error 409/400 จาก server (ฟอร์ม edit + create vlan)
- [ ] `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` — เพิ่ม 409 + คำอธิบาย alias unique
- [ ] Test BE: `service/interface_test.go` — validateAlias (รูปแบบผิด/ซ้ำ NOCASE/ชน name อื่น/ยกเว้นตัวเอง/alias==ชื่อตัวเองผ่าน), migration de-dup
- [ ] Test BE: `api/handlers_test.go` — PUT/POST alias ซ้ำ → 409, รูปแบบผิด → 400, **PUT ไม่ส่ง alias → 200 และ alias = name (ไม่ใช่ `""`)**
- [ ] Test BE: `backup_test.go` — import ที่มี alias ซ้ำไม่ทำ restore ล้ม (ถูก dedup + warning)
- [ ] `go build ./...` + `go test ./...` ผ่าน; `yarn build` + `yarn lint` ผ่าน
- [ ] ทดสอบ mock: ตั้ง alias ซ้ำ→เตือน, ทุกหน้าโชว์ `alias (name)`, dropdown ยังส่ง OS name (สร้าง policy แล้ว `in_interface` เป็น `eth0`)
- [ ] ทดสอบ migration: เปิด DB เดิมที่มี alias ซ้ำ **และแถว alias ว่างหลายแถว** → boot ผ่าน, alias ถูกแก้ + มี warning ใน log
- [ ] (ไม่ต้องแตะ README Feature Status — ไม่ใช่ feature ใหม่ เป็น UX/integrity ปรับปรุง; ใส่หมายเหตุได้ถ้าต้องการ)
