# แผนงาน: Multi-Interface Firewall Rule (1 กฎ เลือกได้หลาย In/Out Interface)

สถานะ: **เจ้าของโปรเจกต์อนุมัติแล้ว (2026-08-15)** → ส่งให้ ai-developer ทำทีละ Task (T-01..T-14)
→ ทดสอบรวมทีเดียวโดย ai-qa ตามหัวข้อ 6

Branch: `feat/multi-interface-firewall-rule` (โค้ดทั้งหมดเข้า PR ใหม่ ห้าม push main;
main รับได้เฉพาะไฟล์ docs)

ข้อตัดสินใจที่อนุมัติแล้วทั้งหมด (รายละเอียด + เหตุผลอยู่หัวข้อ 7):

1. **D-1 = Option A** — แตกกฎย่อยแบบ cartesian (1 nft rule ต่อ 1 คู่ in × out) ไม่ใช้ anonymous set
2. **D-2 = config key ใหม่ `max-policy-interfaces-per-direction` ค่าเริ่มต้น 8** (เจ้าของสั่งให้ปรับได้
   ผ่าน config file แทนค่าคงที่ตายตัว) → เพิ่ม Task ใหม่ T-02 สำหรับชั้น config โดยเฉพาะ
3. **D-3 = คงคอลัมน์/field scalar เดิม** เป็น mirror ของสมาชิกตัวแรก (deprecated แต่ไม่ลบ)
4. **D-4 = UI แสดงประมาณการจำนวนกฎย่อย + เตือนเมื่อบวม** → เพิ่ม Task ใหม่ T-14
5. **D-5 = validate ชื่ออินเทอร์เฟซแบบ warn อย่างเดียว** (ตรวจ "รูปแบบชื่อ" แบบ hard-fail,
   แต่ "มีอยู่จริงบนเครื่องหรือไม่" แค่ log warning ไม่บล็อก)

เอกสารที่ต้องอ่านคู่กัน:
- `docs/tech_stack_design.md` §4.3 — โครงสร้าง 4 ส่วนของ input chain (ห้ามสลับลำดับ)
- `docs/ref/todo/input-output-chain-firewall-plan.md` — ที่มาของ field `chain` + กติกา
  in/out interface ต่อ chain
- `docs/ref/todo/multi-value-address-service-objects-plan.md` — **pattern หลักที่แผนนี้ลอกมาทั้งดุ้น**
  (legacy scalar column + ตารางลูก + `omitempty` + backfill idempotent + config key
  `max-object-entries` + `Repository.SetObjectLimits`)
- `docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md` — cap การขยายกฎ
  (`max-expanded-rules-per-policy`) ที่แผนนี้ไปเพิ่มตัวคูณให้

---

## 0. ขอบเขต

ทำให้ 1 PolicyRule ระบุ **In Interface ได้หลายตัว และ Out Interface ได้หลายตัว** (เช่น
in = `eth0`,`wlan0` / out = `eth1`) แทนที่จะเลือกได้ตัวเดียวหรือ `ALL` อย่างทุกวันนี้ ครบทุกชั้น:
model → config → db → service → kernel → api → frontend + backup/restore + openapi

ในขอบเขต:
1. เก็บรายการอินเทอร์เฟซหลายตัวต่อกฎ ต่อทิศทาง (in/out) ลง SQLite แบบ backward-compatible
2. สร้าง nftables rule ให้ถูกต้องสำหรับหลายอินเทอร์เฟซ โดย **ไม่แตะโครงสร้าง 4 ส่วน** ของ input chain
3. Validate ชื่ออินเทอร์เฟซทุกตัว (ปัจจุบัน **ไม่ validate เลย** — ดูหัวข้อ 1 ข้อสังเกตความปลอดภัย)
4. เพดานจำนวนอินเทอร์เฟซต่อทิศทางปรับได้ผ่าน config file (default 8)
5. UI เลือกหลายอินเทอร์เฟซด้วย Combobox chips ตัวเดียวกับที่ Source/Destination/Service ใช้อยู่แล้ว
   + บอกประมาณการจำนวนกฎย่อยที่จะเกิดขึ้น
6. Backup/Restore + OpenAPI + mock mode สอดคล้องกัน

นอกขอบเขต (ยืนยัน):
- Port Forward (`PortForward.InInterface`) ยังเป็นตัวเดียวเหมือนเดิม
- "Interface Group / Zone object" (ออบเจ็กต์กลุ่มอินเทอร์เฟซที่นำกลับมาใช้ซ้ำได้แบบ FortiGate zone) —
  เป็นฟีเจอร์คนละตัว ถ้าจะทำควรทำต่อยอดหลังแผนนี้จบ
- การใช้ nftables anonymous set / named set (ตัดออกตาม D-1 — เก็บเป็น optimization รอบหน้า)
- QoS / DHCP / DNS ที่อ้างอิงอินเทอร์เฟซ ไม่แตะ

---

## 1. สภาพปัจจุบัน (จากการอ่านโค้ดจริง)

| จุด | ไฟล์ / บรรทัด | สรุป |
|---|---|---|
| struct | `backend/internal/model/types.go:266-312` | `PolicyRule` / `PolicyRuleInput` มี `InInterface string` + `OutInterface string` (ตัวเดียว) ส่วน Source/Destination/Service เป็น `[]string` อยู่แล้ว |
| validate | `backend/internal/model/policy_rule_validate.go` | ตรวจแค่ว่า chain=input ต้องไม่มี outInterface และ chain=output ต้องไม่มี inInterface (ยอมรับ `""` หรือ `"ALL"`) — **ไม่มีการตรวจรูปแบบชื่ออินเทอร์เฟซเลย** |
| schema | `backend/internal/db/connection.go:288-299` | `firewall_policies(… in_interface TEXT NOT NULL, out_interface TEXT NOT NULL, …)` |
| pattern multi-value ที่มีอยู่ | `connection.go:258-286` + `1033-1061` | `address_object_values` / `service_object_ports` = ตารางลูก `(parent_id, seq, …)` + backfill `WHERE NOT EXISTS` (idempotent) + คอลัมน์เดิมกลายเป็น mirror ของ entry ที่ 1 |
| pattern cap ที่ปรับได้ | `config/config.go:167-177, 263-266, 328-332, 408-422, 499-501, 677-687, 862-875, 974-977` + `db/repository.go:27-52, 348, 650` + `model/object_entry_validate.go:30-36` + `cmd/pigate/main.go:126-130` | `max-object-entries`: const default ใน model → field ใน `config.Config` → `Defaults()` → const key → `orderedKeys` (**ต่อท้ายเสมอ**) → `applyKey()` → `valueFor()` → range clamp+warn ใน `Resolve()` → `repo.SetObjectLimits(cfg.…)` ที่ main.go → validate ที่ repository ด้วยค่าที่ inject มา |
| repo | `backend/internal/db/repository.go:759-1032` | `GetPolicies`/`GetPolicyByID` scan สองคอลัมน์นี้ตรง ๆ, `CreatePolicy`/`UpdatePolicy` เขียนตรง ๆ ใน transaction เดียวกับ `savePolicyRelations` |
| service | `backend/internal/service/firewall.go:132-147` | `CreatePolicy`/`UpdatePolicy` = normalize chain → `model.ValidatePolicyRule` → repo (ไม่แตะอินเทอร์เฟซ) |
| kernel: จุดใช้จริง | `backend/internal/kernel/real_firewall.go:1556-1588` | `buildRuleExpressions` รับ `inInterface, outInterface string` → ถ้าไม่ใช่ `""`/`"ALL"` ก็ต่อ `expr.Meta{IIFNAME}` + `expr.Cmp{Eq, padInterfaceName(name)}` (และ OIFNAME) |
| kernel: ตัวขยายกฎ | `real_firewall.go:1439-1554` | `addUserChainRules` ทำ cartesian อยู่แล้วบน (source entry × dest entry × service entry × proto) และนับ `expandedCount` เทียบ `maxExpandedRulesPerPolicy` แล้ว `break srcLoop` เมื่อชนเพดาน |
| kernel: การผูก counter | `real_firewall.go:1462` + `real_traffic_account.go:146-190` | ทุก nft rule ที่ขยายจาก DB rule เดียวกันถูกแท็ก `UserData = comment(r.ID)` และ traffic account **รวมผลของทุก expansion ที่ id เดียวกัน** → การเพิ่มจำนวน nft rule ต่อ 1 DB rule ไม่ทำสถิติเพี้ยน |
| kernel: จุดเรียกต่อ chain | `real_firewall.go:608, 717, 789` | `addUserChainRules` ถูกเรียกครั้งเดียวต่อ chain ที่ตำแหน่งคงที่ (input = section 3 ก่อน final drop log) — ตราบใดที่ยังเรียกจุดเดิม โครงสร้าง 4 ส่วนไม่ถูกกระทบ |
| mock | `backend/internal/kernel/mock.go:72-79` | log บรรทัดเดียวต่อกฎ พิมพ์ `In: %s, Out: %s` |
| api | `backend/internal/api/handlers.go:1738-1827` | map `input.InInterface`/`OutInterface` เข้าตรง ๆ; PUT มี pattern "field ที่ client เก่าไม่ส่งต้องไม่ถูกล้าง" อยู่แล้ว (ดู `chain`, `Monitored`) |
| backup | `backend/internal/db/backup_repo.go:173-212` | restore เขียน `in_interface/out_interface` ตรง ๆ; **checksum ถูกตรวจโดย re-marshal ก่อน normalize** → field ใหม่ต้อง `omitempty` เสมอ |
| frontend type | `frontend/src/data-mockup/mockData.ts:117-134` | `inInterface: string` / `outInterface: string` |
| frontend form | `frontend/src/components/policy/PolicyChainPage.tsx:1073-1117` | `<select>` HTML ธรรมดา 2 อัน + `interfaceOptions` (`["ALL", ...ifaces]`, บรรทัด 447-453) |
| frontend chips ที่มีอยู่ | `PolicyChainPage.tsx:1119-1194`, `1035-1069` | `<Combobox multiple>` + `ComboboxChips/Chip/ChipsInput/Content/List/Item` + `useComboboxAnchor()` + `container={drawerContentRef}` — **นี่คือ pattern ที่ multi-interface ต้องใช้ซ้ำ** |
| frontend table | `PolicyChainPage.tsx:211-223, 868-869` | คอลัมน์ In/Out เป็น Badge เดี่ยว (`ifaceLabel`) |
| frontend service | `frontend/src/services/policyService.ts:11-13` | มี `normalizeChain()` เป็นแบบอย่างของการ normalize ข้อมูลเก่าใน localStorage |
| openapi | `docs/openapi.yaml:6762-6802, 7225-7256` | `inInterface`/`outInterface` เป็น required string ทั้งใน `PolicyRule` และ `PolicyRuleInput` |

**ข้อสังเกตความปลอดภัยที่เจอระหว่างสำรวจ (ต้องแก้ในแผนนี้):** ชื่ออินเทอร์เฟซของ policy
ไม่เคยถูก validate เลย และ `padInterfaceName` (real_firewall.go:905) `copy()` ลง buffer 16 ไบต์ —
ชื่อยาวเกิน 15 ตัวอักษรจะถูก **ตัดเงียบ ๆ** แล้วกลายเป็นกฎที่แมตช์อินเทอร์เฟซอื่นที่ prefix ตรงกัน
(กฎที่ผู้ใช้ตั้งใจให้แคบ กลายเป็นกว้างกว่าที่คิด) ปัจจุบันมี `model.ValidateInterfaceName`
(`dns_validate.go:50`, จำกัด 15 ตัวอักษร + whitelist) ใช้อยู่แล้วในฝั่ง DNS/PortForward —
แผนนี้จะบังคับใช้กับ policy ด้วย (T-01)

---

## 2. การออกแบบ

### 2.1 Model (`model/types.go`)

เพิ่ม field แบบ **additive** ตาม pattern `AddressObject.Entries` (D-3: scalar เดิมอยู่ต่อ):

```go
// PolicyRule
InInterface  string   `json:"inInterface"`            // Deprecated: mirror ของ InInterfaces[0]
OutInterface string   `json:"outInterface"`           // Deprecated: mirror ของ OutInterfaces[0]
InInterfaces  []string `json:"inInterfaces,omitempty"`  // แหล่งความจริงใหม่
OutInterfaces []string `json:"outInterfaces,omitempty"`
```

- **`omitempty` บังคับ** — เพราะ `BackupService` re-marshal ไฟล์ backup เพื่อตรวจ checksum
  *ก่อน* normalize (backup.go `decodeBackup` Caution 1) ไฟล์เก่าที่ไม่มีคีย์นี้ต้อง marshal
  ออกมาได้ไบต์เดิมเป๊ะ
- ฟังก์ชัน `NormalizePolicyRuleInterfaces(p *PolicyRule)` (+ `…Input`) นิยาม idempotent:
  1. ถ้า `InInterfaces` ว่าง → เติมจาก scalar `InInterface` (ถ้า scalar ว่าง → `["ALL"]`)
  2. trim ช่องว่างทุกตัว, ตัดตัวว่างทิ้ง, **ตัดตัวซ้ำ** (case-sensitive ตาม Linux)
  3. ถ้ามี `"ALL"` อยู่ในลิสต์ → ยุบเหลือ `["ALL"]` (ALL กินทุกอย่าง ไม่ต้องเก็บตัวอื่น)
  4. เขียน scalar กลับ = `InInterfaces[0]` เสมอ (ลิสต์เป็นแหล่งความจริงเมื่อมีค่า)

### 2.2 Validation + เพดานที่ปรับได้ (D-2, D-5)

แบ่งเป็นสองระดับ ตาม pattern `max-object-entries` เป๊ะ ๆ:

1. **`ValidatePolicyRule(p PolicyRule) error`** (signature เดิม ไม่เปลี่ยน) — ตรวจสิ่งที่ไม่ขึ้นกับ config:
   - กติกา chain เดิม (input ห้ามมี out, output ห้ามมี in, nat=false) แต่ตรวจจาก "ลิสต์"
   - ทุกชื่อที่ไม่ใช่ `"ALL"` ต้องผ่าน `model.ValidateInterfaceName` (≤15 ตัว + whitelist) — **hard fail**
   - ห้ามซ้ำในลิสต์เดียวกัน
2. **`ValidatePolicyInterfaces(names []string, maxPerDirection int) error`** (ฟังก์ชันใหม่ ลอกจาก
   `ValidateAddressEntries`) — ตรวจ "จำนวน" เทียบเพดานที่ inject มา, ถ้า `maxPerDirection <= 0`
   ให้ fallback เป็น `model.DefaultMaxPolicyInterfacesPerDirection = 8`
   เรียกจาก **repository เท่านั้น** (จุดเดียวที่ถือค่า config — เหมือน `r.maxObjectEntries`)
   เพื่อไม่ให้ service กับ repo บังคับเพดานคนละค่ากันเมื่อผู้ใช้แก้ config

3. **D-5 (มีอยู่จริงบนเครื่องไหม)**: `FirewallService.CreatePolicy/UpdatePolicy` เทียบชื่อกับ
   `ifaceService.GetDataLayerInterface()` แล้ว **log warning อย่างเดียว ไม่ block** (wlan0/bridge
   อาจยังไม่ขึ้นตอน boot หรือผู้ใช้ตั้งค่าล่วงหน้า)

**Config key ใหม่ (D-2)** — `max-policy-interfaces-per-direction`, file-only (ไม่มี CLI flag),
default 8, ช่วงที่ยอมรับ 1..64 (นอกช่วง → clamp กลับ default + warn) ต้องแตะครบ 6 จุดใน
`config/config.go` + wiring 1 จุดใน `main.go` (`repo.SetPolicyInterfaceLimit(cfg.…)`)

### 2.3 DB (`db/connection.go`, `db/repository.go`, `db/backup_repo.go`)

ตารางลูกใหม่ (ลอกจาก `address_object_values`):

```sql
CREATE TABLE IF NOT EXISTS policy_interfaces (
    policy_id TEXT NOT NULL,
    direction TEXT NOT NULL CHECK(direction IN ('in', 'out')),
    seq       INTEGER NOT NULL,
    name      TEXT NOT NULL,
    PRIMARY KEY (policy_id, direction, seq),
    FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE
);
```

- คอลัมน์ `in_interface` / `out_interface` **คงไว้** (D-3) เป็น mirror ของ seq 1
- Migration backfill idempotent (รันทุก boot ปลอดภัย):

```sql
INSERT INTO policy_interfaces (policy_id, direction, seq, name)
SELECT p.id, 'in', 1, CASE WHEN TRIM(p.in_interface) = '' THEN 'ALL' ELSE p.in_interface END
FROM firewall_policies p
WHERE NOT EXISTS (SELECT 1 FROM policy_interfaces i WHERE i.policy_id = p.id AND i.direction = 'in');
-- และชุดเดียวกันสำหรับ 'out'
```

- `GetPolicies` โหลดตารางลูกทั้งก้อนครั้งเดียวแล้ว map ตาม policy_id (**ห้าม query ในลูป** —
  ทำแบบ `loadServiceEntries` ที่ query ทีเดียวทั้งตาราง)
- `CreatePolicy`/`UpdatePolicy` เขียนคอลัมน์ mirror + `replacePolicyInterfaces(tx, id, dir, names)`
  ใน transaction เดิม

### 2.4 Kernel (`kernel/real_firewall.go`, `kernel/mock.go`) — D-1 Option A

`buildRuleExpressions` เปลี่ยนพารามิเตอร์เป็น `inIfaces, outIfaces []string` แล้วคืน
`[][]expr.Any` ที่ครอบคลุมทุกคู่ (in × out) — สร้าง nft rule 1 ตัวต่อ 1 คู่ ซ้อนอยู่ในลูป
cartesian ที่มีอยู่แล้ว

เหตุผลที่เลือก A (อนุมัติแล้ว):
- ไม่แตะโครงสร้าง 4 ส่วน: rule ทุกตัวยังถูก append ณ จุดเดิมของ chain และเรียงติดกัน
- ใช้เครื่องมือเดิมได้ทั้งชุด: `UserData` แท็ก DB rule id (traffic account รวมให้อยู่แล้ว),
  log prefix + `withRuleToken`, และเพดาน `maxExpandedRulesPerPolicy` คุมการระเบิดของกฎอัตโนมัติ
- `buildRuleExpressions` ยังเป็น pure function ทดสอบได้โดยไม่ต้องต่อ netlink
  (เทสต์ `kernel/policy_chain_test.go`, `real_firewall_test.go` พึ่งจุดนี้อยู่)

กติกาการแมตช์ (ต้องเหมือนเดิมเป๊ะสำหรับกฎเก่า):
- ลิสต์ `["ALL"]` หรือว่าง → **ไม่ใส่ expr iifname/oifname เลย** (เหมือนเดิม)
- ลิสต์ 1 ตัว → ได้ nft rule ชุดเดียวหน้าตาเหมือนก่อนแก้ทุกไบต์
- chain=input บังคับ `outIfaces` เป็น "ทุกตัว", chain=output บังคับ `inIfaces` เป็น "ทุกตัว"
  (ตรรกะ `effIn/effOut` เดิม ย้ายมาทำกับลิสต์)

`mock.go`: พิมพ์ `In: [eth0 wlan0], Out: [ALL]` (ใช้ลิสต์หลัง normalize) — mock ไม่ต้องขยายกฎ

### 2.5 API

- `PolicyRuleInput` รับได้ทั้งสองแบบ:
  - client ใหม่ส่ง `inInterfaces: ["eth0","wlan0"]`
  - client เก่าส่ง `inInterface: "eth0"` → normalize เป็น `["eth0"]`
  - ส่งมาทั้งคู่ → **ลิสต์ชนะ** (ตาม pattern Normalize ของ Address/Service)
- response `PolicyRule` ส่งกลับ **ทั้งสอง field** เสมอ: ลิสต์เต็ม + scalar = ตัวแรก
- PUT: ถ้า body ไม่มีทั้ง `inInterfaces` และ `inInterface` (ไม่ใช่ค่าว่าง — คือ **ไม่มีคีย์เลย**)
  ให้คงค่าเดิมของกฎ ตาม pattern `chain`/`Monitored` ที่ handler ทำอยู่แล้ว

### 2.6 Frontend

- `mockData.ts`: `inInterfaces?: string[]` / `outInterfaces?: string[]` (optional) + คง scalar เดิม
- `policyService.ts`: เพิ่ม `normalizeInterfaces()` คู่กับ `normalizeChain()`; create/update ส่งทั้งลิสต์และ scalar
- `PolicyChainPage.tsx`:
  - ฟอร์ม: แทน `<select>` 2 อัน ด้วย `<Combobox multiple>` + `ComboboxChips` ชุดเดียวกับ
    Source/Destination (เพิ่ม `useComboboxAnchor()` 2 ตัว + `container={drawerContentRef}`)
  - ตรรกะ ALL: เลือก `ALL` → เคลียร์ตัวอื่น; เลือกตัวอื่นขณะมี `ALL` → เอา `ALL` ออก; ว่าง → ส่ง `["ALL"]`
  - ตาราง: คอลัมน์ In/Out เรนเดอร์ Badge หลายตัวแบบเดียวกับคอลัมน์ Source
  - ค้นหา: เพิ่มชื่ออินเทอร์เฟซเข้า `filteredRules`
  - **D-4**: ใต้ฟอร์มแสดงประมาณการจำนวนกฎย่อย = `|in| × |out| × |src| × |dst| × |svc|`
    (นับ `ALL`/ว่าง = 1) เป็นข้อความ `text-muted-foreground` และเปลี่ยนเป็นโทน
    `text-destructive` + ข้อความเตือนเมื่อเกิน 100
  - สไตล์ตาม `docs/rules_of_work.md`: ใช้ตัวแปรสีธีมเท่านั้น, ห้าม `shadow-*`/`backdrop-blur-*`,
    รองรับทั้ง dark/light

---

## 3. รายการ Task (ให้ ai-developer ทำ **ทีละ Task ตามลำดับ ยังไม่ต้องทดสอบระหว่างทาง**)

```json
[
  {
    "task_id": "T-01",
    "title": "model: เพิ่ม InInterfaces/OutInterfaces + Normalize + Validate",
    "layer": "model",
    "files": ["backend/internal/model/types.go", "backend/internal/model/policy_rule_validate.go"],
    "instruction": "ใน model/types.go เพิ่ม field `InInterfaces []string `json:\"inInterfaces,omitempty\"`` และ `OutInterfaces []string `json:\"outInterfaces,omitempty\"`` ให้ทั้ง PolicyRule และ PolicyRuleInput โดยคง InInterface/OutInterface (string) ไว้เป็น compat mirror ของสมาชิกตัวแรก (D-3) พร้อม doc comment แบบเดียวกับ AddressObject.Protocol/Port ที่ระบุว่า Deprecated และห้ามใช้ generate firewall rule. omitempty ห้ามลบเด็ดขาด (เหตุผล: backup checksum ตรวจโดย re-marshal ก่อน normalize). เพิ่มฟังก์ชัน NormalizePolicyRuleInterfaces(p *PolicyRule) และ NormalizePolicyRuleInputInterfaces(p *PolicyRuleInput) ที่ nil-safe + idempotent ตามกติกาหัวข้อ 2.1 (เติมจาก scalar เมื่อลิสต์ว่าง, trim, ตัดค่าว่าง, ตัดซ้ำ, มี ALL ให้ยุบเหลือ [\"ALL\"], เขียน scalar กลับจากสมาชิกตัวแรกเสมอ). เพิ่ม const DefaultMaxPolicyInterfacesPerDirection = 8 พร้อม comment ว่าต้อง sync กับ config.Defaults().MaxPolicyInterfacesPerDirection และเป็นเพียง fallback เมื่อ caller ไม่ inject ค่า (ลอกถ้อยคำจาก model/object_entry_validate.go:30-36). ใน policy_rule_validate.go: (ก) ValidatePolicyRule คง signature เดิม แต่เปลี่ยนไปตรวจจากลิสต์ — chain=input ต้องมี OutInterfaces เป็น []/[\"ALL\"], chain=output ต้องมี InInterfaces เป็น []/[\"ALL\"] (ข้อความ error คงความหมายเดิม), ทุกชื่อที่ไม่ใช่ ALL ต้องผ่าน ValidateInterfaceName, ห้ามซ้ำ; (ข) เพิ่มฟังก์ชันใหม่ ValidatePolicyInterfaces(names []string, maxPerDirection int) error ที่ตรวจเฉพาะ 'จำนวน' และ fallback เป็น DefaultMaxPolicyInterfacesPerDirection เมื่อ maxPerDirection <= 0 (ลอกโครงจาก ValidateAddressEntries). ห้ามแตะกติกา nat/chain เดิม",
    "acceptance": ["go build ./... ผ่าน", "ValidatePolicyRule ยังคงปฏิเสธ input+outInterface และ output+inInterface เหมือนเดิม", "Normalize เป็น idempotent (เรียกซ้ำได้ผลเดิม)", "ValidatePolicyInterfaces(names, 0) ใช้เพดาน 8"],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "config: เพิ่ม key max-policy-interfaces-per-direction (default 8) + wiring ที่ main.go",
    "layer": "service",
    "files": ["backend/internal/config/config.go", "backend/cmd/pigate/main.go", "backend/internal/db/repository.go"],
    "instruction": "เพิ่ม config key file-only ใหม่ตาม pattern max-object-entries ให้ครบ 6 จุดใน config/config.go: (1) field MaxPolicyInterfacesPerDirection int ใน Config พร้อม doc comment ว่าเป็น file-only ไม่มี CLI flag และบังคับใช้ตอน validate ใน db/repository.go; (2) Defaults() = 8 พร้อม comment ว่าต้อง sync กับ model.DefaultMaxPolicyInterfacesPerDirection; (3) const keyMaxPolicyInterfacesPerDirection = \"max-policy-interfaces-per-direction\"; (4) ต่อท้าย orderedKeys **ที่ท้ายสุดเท่านั้น** (ห้ามแทรกกลาง เพราะกระทบลำดับไฟล์ config ที่ติดตั้งไปแล้ว); (5) case ใน applyKey() (parse int, ค่าผิดรูปแบบให้ทำแบบเดียวกับ key int อื่น); (6) case ใน valueFor() + range clamp+warn ใน Resolve() ด้วย const minMaxPolicyInterfacesPerDirection = 1, maxMaxPolicyInterfacesPerDirection = 64. จากนั้นใน db/repository.go เพิ่ม field maxPolicyInterfacesPerDirection (ค่าเริ่มต้น model.DefaultMaxPolicyInterfacesPerDirection ใน NewRepository) + setter เพิ่มเติมแบบ additive `func (r *Repository) SetPolicyInterfaceLimit(n int)` (ห้ามเปลี่ยน signature ของ SetObjectLimits เดิม) และใน cmd/pigate/main.go เรียก repo.SetPolicyInterfaceLimit(cfg.MaxPolicyInterfacesPerDirection) ถัดจากบรรทัด repo.SetObjectLimits(cfg.MaxObjectEntries) (บรรทัด ~130) พร้อม comment สั้น ๆ ว่าอ่านครั้งเดียวตอน startup",
    "acceptance": ["go build ./... ผ่าน", "config.Resolve คืนค่า 8 เมื่อไม่ตั้งคีย์ และ clamp+warn เมื่อใส่ 0 หรือ 999", "config.Write เขียนคีย์ใหม่ต่อท้ายไฟล์ ไม่สลับลำดับคีย์เดิม"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-03",
    "title": "db: ตาราง policy_interfaces + migration backfill idempotent",
    "layer": "db",
    "files": ["backend/internal/db/connection.go"],
    "instruction": "เพิ่ม CREATE TABLE IF NOT EXISTS policy_interfaces ตามสคีมาในหัวข้อ 2.3 ต่อท้ายกลุ่ม CREATE TABLE ของ firewall_policies/policy_addresses/policy_services และเพิ่ม backfill สองคำสั่ง (direction 'in' และ 'out') ในโซนเดียวกับ backfill address_object_values/service_object_ports (ราวบรรทัด 1033-1061) โดยใช้ WHERE NOT EXISTS ให้ idempotent, แปลงค่าว่าง/ช่องว่างเป็น 'ALL', และ log จำนวนแถวที่ backfill เมื่อ > 0. ห้ามลบหรือแก้คอลัมน์ in_interface/out_interface เดิม ห้ามแทรกกลางลำดับ migration อื่น",
    "acceptance": ["เปิด DB เก่าที่มี policy อยู่แล้ว ได้แถว policy_interfaces ครบ 2 แถวต่อกฎ", "รัน InitDB ซ้ำไม่เพิ่มแถวซ้ำ", "DB ใหม่เอี่ยมสร้างตารางได้ไม่ error"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-04",
    "title": "db: repository อ่าน/เขียน policy_interfaces + บังคับเพดานจาก config",
    "layer": "db",
    "files": ["backend/internal/db/repository.go"],
    "instruction": "ใน GetPolicies โหลด policy_interfaces ทั้งตารางด้วย query เดียว (pattern เดียวกับ loadServiceEntries บรรทัด 512) แล้ว map เข้าแต่ละกฎ — ห้ามยิง query ต่อกฎในลูป. ใน GetPolicyByID โหลดเฉพาะ policy_id นั้น. หลังโหลดเสร็จเรียก model.NormalizePolicyRuleInterfaces เสมอ. เพิ่ม helper replacePolicyInterfaces(tx, policyID, direction, names) (DELETE แล้ว INSERT seq เริ่ม 1) และเรียกใน CreatePolicy/UpdatePolicy ภายใน transaction เดิม พร้อมเขียนคอลัมน์ mirror in_interface/out_interface จากสมาชิกตัวแรก. CreatePolicy/UpdatePolicy ต้อง: normalize → ValidatePolicyRule → model.ValidatePolicyInterfaces(p.InInterfaces, r.maxPolicyInterfacesPerDirection) และตัวเดียวกันสำหรับ OutInterfaces (จุดเดียวในระบบที่บังคับเพดานจาก config — ห้ามไปฮาร์ดโค้ดเพดานที่ชั้นอื่น)",
    "acceptance": ["สร้างกฎที่มี 3 in-interface แล้วอ่านกลับมาได้ครบ 3 ตัว ลำดับคงเดิม", "อัปเดตกฎให้เหลือ 1 ตัว แถวลูกส่วนเกินถูกลบ", "กฎเก่าที่ backfill มาอ่านได้ค่าเดิมทุกประการ", "ส่ง 9 อินเทอร์เฟซขณะเพดาน 8 → error"],
    "depends_on": ["T-02", "T-03"]
  },
  {
    "task_id": "T-05",
    "title": "db/service: backup export/restore รองรับลิสต์อินเทอร์เฟซ",
    "layer": "db",
    "files": ["backend/internal/db/backup_repo.go", "backend/internal/service/backup.go"],
    "instruction": "ใน RestoreConfig (backup_repo.go ส่วน --- 4. Firewall policies) ให้เรียก model.NormalizePolicyRuleInterfaces กับ policy ที่ถอดมาก่อนเขียน แล้วเขียนทั้งคอลัมน์ mirror และแถว policy_interfaces (ใช้ helper จาก T-04). เพิ่ม \"DELETE FROM policy_interfaces\" ลงรายการ wipes ก่อน DELETE FROM firewall_policies (แม้มี CASCADE ก็ใส่ให้ชัดตาม pattern เดิมของ policy_services/policy_addresses). ตรวจใน backup.go ว่า normalization ทั้งหมดยังเกิด **หลัง** การตรวจ checksum เท่านั้น — ห้ามย้ายลำดับ",
    "acceptance": ["import ไฟล์ backup เก่า (ไม่มีคีย์ inInterfaces) ผ่าน checksum และได้กฎ interface เดียวเหมือนเดิม", "export แล้ว import ไฟล์ที่มีหลายอินเทอร์เฟซ ได้ค่าเท่าเดิมครบ"],
    "depends_on": ["T-04"]
  },
  {
    "task_id": "T-06",
    "title": "kernel: buildRuleExpressions/addUserChainRules ขยายเป็นหลายอินเทอร์เฟซ (D-1 Option A)",
    "layer": "kernel",
    "files": ["backend/internal/kernel/real_firewall.go"],
    "instruction": "งาน sensitive: ต้อง review เข้มเป็นพิเศษ (แตะ firewall rule generation โดยตรง). เปลี่ยนพารามิเตอร์ buildRuleExpressions จาก inInterface, outInterface string เป็น inIfaces, outIfaces []string. ตรรกะ: ทำสำเนาลิสต์ตาม chain เหมือน effIn/effOut เดิม (chain=input ล้าง out, chain=output ล้าง in); ลิสต์ว่างหรือ [\"ALL\"] ให้แทนด้วย sentinel ตัวเดียวที่แปลว่า 'ไม่ใส่ expr' เพื่อให้ยังได้กฎ 1 ชุดเหมือนเดิมทุกไบต์; จากนั้นวน for ทุกคู่ (in, out) สร้าง expr ชุดเดิม (Meta IIFNAME/OIFNAME + Cmp padInterfaceName) แล้วต่อด้วย src/dest/svc/verdict เดิม คืนเป็นหลายชุดใน [][]expr.Any เรียงลำดับ in เป็นลูปนอก out เป็นลูปใน. ห้ามเปลี่ยนลำดับ expr ภายในกฎ ห้ามเปลี่ยนตำแหน่งที่ addUserChainRules ถูกเรียกในแต่ละ chain (โครงสร้าง 4 ส่วนของ input chain ต้องเหมือนเดิม). ใน addUserChainRules ส่ง r.InInterfaces/r.OutInterfaces (เรียก model.NormalizePolicyRuleInterfaces ต่อกฎก่อนใช้ กันกรณีถูกเรียกด้วยข้อมูลที่ยังไม่ normalize) และให้ทุก nft rule ที่ได้ยังถูกนับใน expandedCount เทียบ maxExpandedRulesPerPolicy เหมือนเดิม (ห้าม bypass เพดาน) พร้อมคง UserData = r.ID ทุกตัว. ห้ามใช้ anonymous set/named set (D-1 ตัดออกแล้ว)",
    "acceptance": ["กฎที่มีอินเทอร์เฟซตัวเดียวหรือ ALL ให้ผลลัพธ์ expr เหมือนก่อนแก้ทุกประการ", "กฎ in=[eth0,wlan0], out=[eth1] ได้ 2 ชุด expr ต่อ 1 combo ของ src/dst/svc", "เพดาน maxExpandedRulesPerPolicy ยังตัดได้จริงเมื่อเกิน", "go build ./... ผ่าน"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-07",
    "title": "kernel: mock.go log ลิสต์อินเทอร์เฟซ",
    "layer": "kernel",
    "files": ["backend/internal/kernel/mock.go"],
    "instruction": "ปรับบรรทัด log ต่อกฎ (บรรทัด ~77-78) ให้พิมพ์ In/Out เป็นลิสต์หลัง model.NormalizePolicyRuleInterfaces แทน scalar เดิม เช่น \"In: [eth0 wlan0], Out: [ALL]\". ห้ามเปลี่ยน signature ของ ApplyRules และห้ามเพิ่มพฤติกรรมอื่น",
    "acceptance": ["รัน -mock=true แล้ว log แสดงอินเทอร์เฟซครบทุกตัวของกฎ"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-08",
    "title": "service: normalize ก่อน validate + warn เมื่ออินเทอร์เฟซยังไม่มีบนเครื่อง (D-5)",
    "layer": "service",
    "files": ["backend/internal/service/firewall.go"],
    "instruction": "ใน CreatePolicy/UpdatePolicy เรียก model.NormalizePolicyRuleInterfaces(&rule) ต่อจาก NormalizePolicyChain และก่อน ValidatePolicyRule เพื่อให้ทุกทางเข้าได้ข้อมูลที่ normalize แล้วเสมอ (fail-closed). จากนั้นตาม D-5: เทียบชื่ออินเทอร์เฟซทุกตัว (ยกเว้น ALL) กับ s.ifaceService.GetDataLayerInterface() แล้ว **log warning อย่างเดียว** เมื่อไม่พบ (ข้อความต้องบอกชื่อกฎ + ชื่ออินเทอร์เฟซ + ว่ากฎยังถูกบันทึกและจะมีผลเมื่ออินเทอร์เฟซขึ้น) — ห้าม return error และห้ามให้ error จาก GetDataLayerInterface ทำให้บันทึกกฎไม่ได้ (ถ้า query ล้มเหลว ให้ข้ามการเช็คไปเงียบ ๆ พร้อม log). ไม่ต้องบังคับเพดานจำนวนที่ชั้นนี้ (repository ทำแล้วใน T-04)",
    "acceptance": ["สร้างกฎผ่าน service ด้วย scalar อย่างเดียว ได้ลิสต์ที่ normalize แล้วลง DB", "ตั้งกฎกับอินเทอร์เฟซที่ยังไม่มีบนเครื่อง บันทึกได้ + มี warning ใน log", "ชื่ออินเทอร์เฟซผิดรูปแบบยังถูกปฏิเสธ"],
    "depends_on": ["T-04", "T-01"]
  },
  {
    "task_id": "T-09",
    "title": "api: handler รับ/ส่งทั้งลิสต์และ scalar (backward compatible)",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go"],
    "instruction": "งาน sensitive (input validation). ใน HandleCreatePolicy map input.InInterfaces/OutInterfaces เข้ากฎ พร้อม scalar เดิม แล้วเรียก model.NormalizePolicyRuleInputInterfaces ก่อนประกอบ model.PolicyRule. ใน HandleUpdatePolicy ต้องคงพฤติกรรม 'client เก่าที่ไม่ส่ง field ต้องไม่ล้างค่าเดิม' แบบเดียวกับ chain/Monitored: ถ้า request ไม่มีทั้งคีย์ inInterfaces และ inInterface ให้ใช้ค่าเดิมจาก existing (ทำได้โดย decode body เป็น map[string]json.RawMessage เพื่อเช็คการมีอยู่ของคีย์ หรือใช้ pointer field เฉพาะสองตัวนี้ — เลือกวิธีที่กระทบโค้ดน้อยที่สุดและอธิบายเหตุผลไว้ใน comment). response ต้องส่งกลับทั้งลิสต์และ scalar เสมอ",
    "acceptance": ["POST ด้วย {\"inInterface\":\"eth0\"} ได้กฎที่ inInterfaces=[\"eth0\"]", "POST ด้วย {\"inInterfaces\":[\"eth0\",\"wlan0\"]} ได้ครบสองตัว และ inInterface=\"eth0\"", "PUT ที่ไม่ส่งคีย์อินเทอร์เฟซเลย ไม่ทำให้ค่าเดิมหาย", "ส่งชื่ออินเทอร์เฟซผิดรูปแบบ/เกินเพดาน ได้ 400 พร้อมข้อความบอกเหตุ"],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-10",
    "title": "backend tests: unit test ครอบ normalize/validate/config/expansion/migration",
    "layer": "service",
    "files": ["backend/internal/model/policy_rule_validate_test.go", "backend/internal/config/config_test.go", "backend/internal/kernel/policy_chain_test.go", "backend/internal/kernel/real_firewall_test.go", "backend/internal/db/repository_test.go"],
    "instruction": "เพิ่มเทสต์ (ไม่ต้องรันชุดใหญ่เอง QA จะรันรวมท้ายแผน): (1) NormalizePolicyRuleInterfaces ครบเคส (ว่าง/scalar/ALL ปน/ซ้ำ/idempotent); (2) ValidatePolicyRule เคสชื่อยาวเกิน 15, อักขระต้องห้าม, ซ้ำ, chain input/output + ValidatePolicyInterfaces เคสเกินเพดานและเคส maxPerDirection<=0; (3) config: default 8, parse จากไฟล์, clamp+warn นอกช่วง 1..64, Write ต่อท้ายไม่สลับลำดับคีย์เดิม; (4) buildRuleExpressions: กฎ 1 อินเทอร์เฟซให้ expr เท่าเดิม + กฎหลายอินเทอร์เฟซได้จำนวนชุดถูกต้องและมี Cmp ตรงชื่อแต่ละตัว; (5) migration backfill ของ policy_interfaces ทั้งเคส DB เก่ามีข้อมูลและเคสรันซ้ำ (ลอกโครงจาก chain_migration_test.go / repository_test.go บรรทัด ~1339)",
    "acceptance": ["ไฟล์เทสต์คอมไพล์ผ่านและครอบทุกหัวข้อข้างต้น"],
    "depends_on": ["T-06", "T-09"]
  },
  {
    "task_id": "T-11",
    "title": "docs: openapi.yaml + เอกสารออกแบบ + คีย์ config ใหม่",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml", "docs/tech_stack_design.md", "CLAUDE.md"],
    "instruction": "เพิ่ม inInterfaces/outInterfaces (array of string) ใน schema PolicyRule และ PolicyRuleInput พร้อม description ที่บอกชัดว่า: ลิสต์เป็นแหล่งความจริง, scalar เป็น mirror ของสมาชิกตัวแรกไว้ให้ client เก่า, ส่งมาทั้งคู่ให้ลิสต์ชนะ, [\"ALL\"]/ว่าง = ทุกอินเทอร์เฟซ, เพดานต่อทิศทางมาจากคีย์ config max-policy-interfaces-per-direction (default 8), ชื่อยาวได้ไม่เกิน 15 ตัวอักษร. คง inInterface/outInterface ไว้ใน required เดิม และซิงก์ frontend/public/openapi.yaml ให้ตรงกัน. ใน tech_stack_design.md เพิ่มหมายเหตุใต้หัวข้อ firewall ว่ากฎหนึ่งข้อขยายเป็น nft rule ต่อคู่ (in × out) และยังอยู่ใต้เพดาน max-expanded-rules-per-policy. ใน CLAUDE.md เพิ่มคีย์ config ใหม่ในย่อหน้า Config file ถ้ามีการไล่รายชื่อคีย์",
    "acceptance": ["openapi.yaml สองไฟล์ตรงกันและ parse ได้", "ไม่มีการลบ field เดิมออกจาก schema"],
    "depends_on": ["T-09"]
  },
  {
    "task_id": "T-12",
    "title": "frontend: type + policyService รองรับลิสต์",
    "layer": "frontend",
    "files": ["frontend/src/data-mockup/mockData.ts", "frontend/src/services/policyService.ts"],
    "instruction": "ใน mockData.ts เพิ่ม inInterfaces?: string[] และ outInterfaces?: string[] ใน interface PolicyRule (optional เพื่อไม่พังข้อมูลเก่าใน localStorage) พร้อมคอมเมนต์ว่า scalar เดิมเป็น mirror ของตัวแรก. ใน policyService.ts เพิ่ม normalizeInterfaces(rules) คู่กับ normalizeChain เพื่อเติมลิสต์จาก scalar (และเติม [\"ALL\"] เมื่อว่าง) ให้ข้อมูลเก่า แล้วเรียกใน getLocalPolicies; create/update ส่ง payload ที่มีทั้งลิสต์และ scalar",
    "acceptance": ["yarn build ผ่าน", "mock mode ที่มีข้อมูลเก่าใน localStorage แสดงผลได้ไม่ crash"],
    "depends_on": ["T-11"]
  },
  {
    "task_id": "T-13",
    "title": "frontend: PolicyChainPage multi-select + ตาราง + ค้นหา",
    "layer": "frontend",
    "files": ["frontend/src/components/policy/PolicyChainPage.tsx"],
    "instruction": "แทน <select> สองอันของ In/Out Interface (บรรทัด ~1073-1117) ด้วย <Combobox multiple> + ComboboxChips/ComboboxChip/ComboboxChipsInput/ComboboxContent/ComboboxList/ComboboxItem แบบเดียวกับ Source/Destination (บรรทัด 1119-1194) รวมถึงเพิ่ม useComboboxAnchor() อีกสองตัวและใช้ container={drawerContentRef} เหมือนกัน. state เปลี่ยนเป็น formInInterfaces/formOutInterfaces: string[]. ตรรกะ ALL: เลือก ALL ให้เคลียร์ตัวอื่น, เลือกตัวอื่นให้ถอด ALL, ว่าง = [\"ALL\"] ตอน submit. openCreateModal/openEditModal เซ็ตค่าจาก rule.inInterfaces ?? [rule.inInterface || \"ALL\"]. คอลัมน์ In/Out ในตาราง (บรรทัด 211-223) เรนเดอร์ Badge หลายตัวใน div flex flex-wrap gap-1 แบบคอลัมน์ Source และเพิ่มชื่ออินเทอร์เฟซเข้าเงื่อนไข filteredRules. ยึดกฎสไตล์ docs/rules_of_work.md: ใช้ตัวแปรสีธีมเท่านั้น ห้าม shadow-*/backdrop-blur-* และรองรับทั้ง dark/light",
    "acceptance": ["yarn build + yarn lint ผ่าน", "เลือกได้หลายอินเทอร์เฟซและบันทึกแล้วกลับมาแก้ไขเห็นค่าครบ", "หน้า Local-In ยังซ่อนช่อง Out และ Local-Out ยังซ่อนช่อง In เหมือนเดิม"],
    "depends_on": ["T-12"]
  },
  {
    "task_id": "T-14",
    "title": "frontend: แสดงประมาณการจำนวนกฎย่อย + เตือนเมื่อบวม (D-4)",
    "layer": "frontend",
    "files": ["frontend/src/components/policy/PolicyChainPage.tsx"],
    "instruction": "ใน Drawer ฟอร์มสร้าง/แก้ไขกฎ เพิ่มบรรทัดสรุปใต้ช่องเลือกอินเทอร์เฟซ (หรือเหนือปุ่มบันทึก) ที่คำนวณ useMemo จากค่าในฟอร์มปัจจุบัน: estimate = |in| × |out| × |src| × |dst| × |svc| โดยนับ ALL/ว่างเป็น 1 พร้อมข้อความไทยประมาณ 'กฎนี้จะถูกแปลงเป็นกฎย่อยในเคอร์เนลประมาณ N ข้อ'. สไตล์ปกติใช้ text-muted-foreground; เมื่อ N > 100 ให้เปลี่ยนเป็นโทน text-destructive พร้อมข้อความเสริมว่าอาจชนเพดานการขยายกฎของระบบและควรลดจำนวนตัวเลือก. ห้ามบล็อกการบันทึก (เป็นแค่คำเตือน) ห้ามยิง API เพิ่ม ห้ามฮาร์ดโค้ดสีแบรนด์ตรง ๆ และห้ามใช้ shadow-*/backdrop-blur-*",
    "acceptance": ["yarn build + yarn lint ผ่าน", "เลือก in 2 ตัว out 2 ตัว src/dst/svc อย่างละ 1 แสดง 4", "เกิน 100 เปลี่ยนโทนเป็นเตือนแต่ยังกดบันทึกได้"],
    "depends_on": ["T-13"]
  }
]
```

---

## 4. ลำดับ dependency (สรุป)

```
T-01 ──┬─ T-02 ─ T-04 ─┬─ T-05
       │   (config)    └─ T-08 ─ T-09 ─┬─ T-10 (tests)
       ├─ T-03 ─(รวมเข้า T-04)          └─ T-11 ─ T-12 ─ T-13 ─ T-14
       ├─ T-06 (kernel) ───────────────┘
       └─ T-07 (mock)
```

ลำดับลงมือที่แนะนำ: **T-01 → T-02 → T-03 → T-04 → T-05 → T-06 → T-07 → T-08 → T-09 → T-10 →
T-11 → T-12 → T-13 → T-14**

---

## 5. ข้อควรระวัง (Cautions)

1. **โครงสร้าง 4 ส่วนของ input chain ห้ามขยับ** — `addUserChainRules` ต้องถูกเรียกที่บรรทัดเดิม
   (หลัง Admin Access/DNS accept, ก่อน final drop log) กฎที่ขยายเพิ่มขึ้นทั้งหมดต้องอยู่ในบล็อกนั้น
2. **การคูณจำนวนกฎ** — กฎเดิมขยายเป็น M(src) × N(dst) × K(svc) × proto อยู่แล้ว แผนนี้เพิ่มตัวคูณ
   |in| × |out| เข้าไปอีก ต้องนับทุกตัวใน `expandedCount` เพื่อให้เพดาน
   `max-expanded-rules-per-policy` ยังคุมได้ ห้าม bypass
3. **กฎเดิมต้องได้ byte-identical expr** — เคส 1 อินเทอร์เฟซ/ALL ต้องให้ผลเหมือนก่อนแก้เป๊ะ
   (มีเทสต์เทียบ expr อยู่แล้วใน `policy_chain_test.go`)
4. **PUT ต้องไม่ล้างค่าเดิม** — client เก่าที่ PUT โดยไม่ส่งคีย์อินเทอร์เฟซต้องไม่ทำให้กฎกลายเป็น
   ALL เงียบ ๆ ซึ่งจะ **เปิดกว้างกว่าที่ผู้ใช้ตั้งใจ** = ปัญหาความปลอดภัย
5. **`omitempty` ห้ามหาย** — ไม่งั้นไฟล์ backup เก่าจะ checksum ไม่ผ่านและ import ไม่ได้
6. **ชื่ออินเทอร์เฟซยาวเกิน 15** จะถูก `padInterfaceName` ตัดเงียบ → ต้อง validate ก่อนเสมอ
   (bug เงียบที่มีอยู่ก่อนแล้ว แผนนี้ปิดให้)
7. **เพดานต้องมาจาก config จุดเดียว** — บังคับที่ repository ด้วย `r.maxPolicyInterfacesPerDirection`
   เท่านั้น ห้ามฮาร์ดโค้ด 8 ที่ service/api/kernel (ค่า const ใน model เป็นแค่ fallback)
8. **`orderedKeys` ต้องต่อท้ายเท่านั้น** — การแทรกกลางทำให้ไฟล์ config ที่ติดตั้งแล้วสลับลำดับ
9. **ห้ามใช้ `exec.Command`** — ทุกอย่างยังผ่าน Netlink ผ่าน `google/nftables` เหมือนเดิม
10. **ลำดับ migration** — เพิ่ม backfill ต่อท้าย ห้ามแทรกกลางลำดับเดิม และต้อง idempotent
11. **mock/real ต้อง parity** — ทุก field ใหม่ต้องสะท้อนใน `mock.go` ด้วย (แม้จะเป็นแค่ log)
12. **counter/สถิติ** — อย่าลืมแท็ก `UserData` ทุก expansion; ถ้าลืม กฎ multi-interface จะโชว์
    usage ต่ำกว่าจริง
13. **D-5 ต้องไม่กลายเป็น hard block** — การเช็ค "อินเทอร์เฟซมีจริงไหม" ต้องไม่ทำให้บันทึกกฎไม่ได้
    แม้ `GetDataLayerInterface()` จะ error

---

## 6. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ai-qa ทดสอบครั้งเดียวหลังทุก Task เสร็จ)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... และ go test ./... ผ่านทั้งหมด",
    "cd frontend && yarn build และ yarn lint ผ่าน",
    "bash build.sh สร้าง ./pigate ได้สำเร็จ",
    "อัปเกรดจาก DB เดิมที่มีกฎ interface เดียว: หลัง start ครั้งแรก กฎทุกข้อยังมีค่า In/Out เท่าเดิมทั้งใน UI และใน DB (คอลัมน์ mirror + แถว policy_interfaces ครบ 2 แถวต่อกฎ) และ restart ซ้ำไม่เกิดแถวซ้ำ",
    "ไฟล์ config เดิมที่ไม่มีคีย์ max-policy-interfaces-per-direction ยังบูตได้ และใช้ค่า default 8; ตั้งคีย์เป็น 2 แล้วรีสตาร์ต จะบันทึกกฎที่มี 3 อินเทอร์เฟซไม่ได้; ตั้งเป็น 0 หรือ 999 ระบบ clamp กลับ 8 พร้อม warning ใน log",
    "โหมด -mock=true: สร้างกฎใหม่ in=[eth0,wlan0] out=[ALL] บันทึกได้ และ log ของ MockFirewall แสดงอินเทอร์เฟซครบทุกตัว",
    "โหมด real (บนเครื่องที่มี CAP_NET_ADMIN): apply กฎ in=[eth0,wlan0] แล้ว nft list ruleset แสดงกฎแยกรายอินเทอร์เฟซครบ อยู่ในตำแหน่งเดิมของ chain (input: หลัง Admin Access/DNS accept และก่อน final drop log) และลำดับ 4 ส่วนไม่เปลี่ยน",
    "กฎที่ตั้ง ALL หรือมีอินเทอร์เฟซเดียว ให้ ruleset ผลลัพธ์เหมือนก่อนมีฟีเจอร์นี้ (diff เฉพาะกฎที่ตั้งหลายอินเทอร์เฟซเท่านั้น)",
    "API: POST /api/policies ด้วย payload แบบเก่า (inInterface เดี่ยว) ยังสร้างกฎได้ปกติ และ response มี inInterfaces=[ค่านั้น]",
    "API: PUT /api/policies/{id} โดยไม่ส่งคีย์อินเทอร์เฟซเลย ไม่ทำให้อินเทอร์เฟซเดิมของกฎเปลี่ยนเป็น ALL",
    "API: ส่งชื่ออินเทอร์เฟซยาว 20 ตัวอักษร / มีอักขระแปลก / เกินเพดานที่ตั้งไว้ ได้ HTTP 400 พร้อมข้อความบอกเหตุ และไม่มีอะไรถูกเขียนลง DB",
    "ตั้งกฎกับอินเทอร์เฟซที่ยังไม่มีอยู่จริงบนเครื่อง (เช่น br-lan ที่ยังไม่สร้าง) บันทึกได้สำเร็จ และมี warning ใน log ไม่ใช่ error (D-5)",
    "Chain rule เดิมยังถูกบังคับ: Local-In Policy ตั้ง outInterfaces ไม่ได้, Local-Out Policy ตั้ง inInterfaces ไม่ได้",
    "Backup/Restore: import ไฟล์ backup ที่ export ก่อนมีฟีเจอร์นี้ ผ่าน checksum และได้กฎเหมือนเดิม; export→import ไฟล์ที่มีกฎ multi-interface ได้ค่าครบทุกตัว",
    "UI: หน้า Firewall Policy / Local-In / Local-Out เลือกหลายอินเทอร์เฟซได้ผ่าน Combobox chips, เลือก ALL แล้วตัวอื่นถูกเคลียร์, ตารางแสดง Badge ครบทุกตัว, ค้นหาด้วยชื่ออินเทอร์เฟซเจอกฎ, ใช้ได้ทั้ง dark/light และไม่มี shadow-*/backdrop-blur-* ใหม่",
    "UI: บรรทัดประมาณการจำนวนกฎย่อยแสดงค่าถูกต้องตามตัวเลือกในฟอร์ม และเปลี่ยนเป็นโทนเตือนเมื่อเกิน 100 โดยยังกดบันทึกได้ (D-4)",
    "สถิติ Usage ต่อกฎ (และ Monitor counter ถ้าเปิด) ของกฎ multi-interface แสดงยอดรวมของทุกอินเทอร์เฟซ ไม่ใช่แค่ตัวแรก",
    "โหมด -disable-edit=true ยังปฏิเสธการแก้ไขกฎเหมือนเดิม"
  ]
}
```

---

## 7. บันทึกการตัดสินใจ (ปิดครบทุกข้อแล้ว)

| id | ประเด็น | ผลการตัดสิน | เหตุผล / ผลต่อแผน |
|---|---|---|---|
| D-1 | วิธี match หลายอินเทอร์เฟซใน nftables | **Option A — แตกกฎย่อยแบบ cartesian (1 nft rule ต่อคู่ in × out)** | ใช้กลไกเดิมทั้งหมด (UserData/counter/เพดาน/pure function ที่เทสต์ได้) ความเสี่ยงต่ำสุด; anonymous set (`iifname { … }`) ตัดออกจากขอบเขต เก็บเป็น optimization รอบหน้าถ้ากฎบวมจริง → T-06 |
| D-2 | เพดานจำนวนอินเทอร์เฟซต่อทิศทาง | **config key `max-policy-interfaces-per-direction` default 8** (ช่วง 1..64, file-only ไม่มี CLI flag) | เจ้าของต้องการให้ปรับได้ ไม่ใช่ค่าคงที่ → เพิ่ม T-02 (config + setter + wiring) และ T-04 บังคับใช้ที่ repository จุดเดียว |
| D-3 | คอลัมน์/field scalar เดิม | **คงไว้เป็น mirror (deprecated)** | ตรงกับ pattern Address/Service, downgrade binary กลับได้, client เก่าไม่พัง → T-01/T-03/T-04 |
| D-4 | UI เตือนการบวมของกฎ | **แสดงประมาณการจำนวนกฎย่อย + เปลี่ยนโทนเตือนเมื่อเกิน 100 แต่ไม่บล็อกการบันทึก** | ให้ผู้ใช้เห็นผลกระทบก่อนกด Apply → T-14 |
| D-5 | ตรวจว่าอินเทอร์เฟซมีอยู่จริงบนเครื่องไหม | **warn อย่างเดียว ไม่บล็อก** (แต่ "รูปแบบชื่อ" ยัง hard-fail) | wlan0/bridge อาจยังไม่ขึ้นตอน boot และผู้ใช้ต้องตั้งค่าล่วงหน้าได้ → T-08 |

---

## 8. บันทึกการอนุมัติ

- **2026-08-15** — เจ้าของโปรเจกต์อนุมัติแผนและเคาะ D-1..D-5 ครบทุกข้อตามตารางหัวข้อ 7
  (D-1/D-2 เจ้าของเลือกเอง; D-3/D-4/D-5 อนุมัติตามข้อเสนอของ Tech Lead)
  → แผนพร้อมส่งให้ ai-developer ลงมือ T-01..T-14 ตามลำดับ แล้วส่ง ai-qa ทดสอบรวมตามหัวข้อ 6
