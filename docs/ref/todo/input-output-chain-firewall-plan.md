# Input/Output Chain Firewall — ขยาย Firewall Policy จาก forward-only เป็น 3 chain

> เอกสารแผนงานสำหรับฟีเจอร์: ปัจจุบันกฎที่ผู้ใช้สร้างในหน้า Firewall Policy ถูก generate
> ลง `forward` chain เท่านั้น ส่วน `input` chain เป็นโครงตายตัวที่ขับด้วย
> `NetworkInterface.AdminAccess` + DHCP/DNS server ifaces และ **ยังไม่มี `output` chain เลย**
> งานนี้เพิ่มฟิลด์ `chain` (forward | input | output) ให้ PolicyRule เดิม แล้ว generate
> กฎของผู้ใช้ลง input/output ด้วย โดยรักษาโครงสร้าง 4 ส่วนของ input chain ไว้ครบ
> ฝั่งหน้าบ้านแยกเป็น **3 หน้าเมนูคนละหน้ากัน** (ไม่ใช่ Tabs ในหน้าเดียว): Firewall Policy
> (forward, เดิม) / **Local-In Policy** (input, ใหม่) / **Local-Out Policy** (output, ใหม่)
> — ชื่อยืมศัพท์จาก FortiGate ที่โปรเจกต์นี้ใช้โทนเดียวกันอยู่แล้ว (เช่น Port Forwarding
> ที่คอมเมนต์โค้ดระบุตรงๆ ว่า "FortiGate VIP style")
>
> วันที่เขียน: 2026-07-25 · Branch อ้างอิง: `main` (งานจริงทำบน `feat/input-output-chain-firewall`)
> README Feature Status: Firewall = Completed → ยังคง Completed (ขยายขอบเขตฟีเจอร์เดิม)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็น):** เมนู "Policy & Objects" มี 2 รายการใหม่ต่อจาก Firewall Policy —
**Local-In Policy** (`/policy/local-in`) และ **Local-Out Policy** (`/policy/local-out`)
สร้าง–แก้–ลบ–ลากจัดลำดับกฎได้แบบเดียวกับหน้า Firewall Policy เดิมทุกประการ (คนละหน้า ไม่ใช่ Tab
ในหน้าเดียว) แล้วกด Apply Settings ครั้งเดียวจากหน้าไหนก็ได้ (apply ทั้ง 3 chain พร้อมกันเสมอ)
กฎของหน้า Local-In ถูก generate ลง `input` chain (ส่วนที่ 3 ต่อจากกฎ Admin Access เดิม)
และกฎของหน้า Local-Out ลง `output` chain ที่สร้างใหม่ หน้า Firewall Policy เดิมยังคงคุม
เฉพาะ `forward` chain เหมือนเดิมทุกประการ (ผู้ใช้เดิมไม่เห็นความเปลี่ยนแปลงใดๆ)

**เงื่อนไขทางเทคนิค:**
- `input` chain ต้องคงลำดับ 4 ส่วนเดิม (sanity → audit → dynamic accept → final drop log)
- กฎ Admin Access (HTTP/HTTPS/SSH/PING ต่ออินเทอร์เฟซ) ต้องอยู่ **ก่อน** กฎ input ของผู้ใช้เสมอ
  → กฎที่ผู้ใช้สร้างไม่สามารถปิดทางเข้าหน้าเว็บ/SSH ของตัวเองได้เชิงโครงสร้าง
- `output` chain ใช้ `policy accept` เท่านั้น (default-allow + deny เฉพาะที่ผู้ใช้เพิ่ม)
- ไม่แก้ signature ของ `FirewallManager.ApplyRules` (กัน mock/fake ใน test พังทั้งชุด)

**นอกขอบเขต (จงใจตัดออก):**
- **Strict egress filtering** (`output` policy drop) — ต้องมีกฎ allow ให้ DNS/NTP/DHCP client/
  apt/dnsmasq upstream ครบก่อน ไม่งั้นบอร์ดตัดขาตัวเอง เป็น scope decision ของเจ้าของโปรเจกต์
- ไม่ทำ "safe apply + auto-rollback ถ้าไม่กด confirm ใน N วินาที" (เสนอเป็นแผนแยกในข้อ 5)
- ไม่ย้าย log ของ input/output เข้า NFLOG/หน้า Forward Traffic (เหตุผลใน §5 ข้อ 6) — ถ้าต้องการ
  หน้า log รวม (Local-In/Local-Out/Forward Traffic) ให้เขียนเป็นเอกสารแผนแยกต่างหาก **หลัง**
  แผนนี้เสร็จ (ต้องแก้ `real_traffic_log.go` ให้ parser แยก reason ตาม prefix `INP/OUT/FWD`)
- ไม่แตะกฎ Admin Access, DHCP/DNS server accept, docker compat และ NAT/port-forward เดิม
- ไม่ทำ input/output chain สำหรับ IPv6 เพิ่มเติม (table เป็น `inet` อยู่แล้ว แต่ตัว matcher
  address object รองรับเฉพาะ IPv4 เหมือนเดิม)

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ 2026-07-25)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| model `PolicyRule` | ❌ ไม่มีแนวคิด chain/direction เลย | `model/types.go:100-128` (มี InInterface/OutInterface/Nat/Status/Priority) |
| ตาราง `firewall_policies` | ❌ ไม่มีคอลัมน์ chain | `db/connection.go:258-267` |
| แม่แบบ migration เพิ่มคอลัมน์ | ✅ มีให้ลอก (คอลัมน์ `nat`) | `db/connection.go:569-598` + test `db/nat_migration_test.go` |
| repository CRUD | ⚠️ select/insert ทีละคอลัมน์ ต้องแก้ทุกจุด | `db/repository.go:606,668,725,782,1042` |
| `SaveAllPolicies` (reorder) | ⚠️ เขียน `priority = idx+1` แบบ global ไม่รู้จัก chain | `db/repository.go:1042-1056` |
| service | ✅ ส่ง `rules` ทั้งก้อนเข้า kernel อยู่แล้ว | `service/firewall.go:120-166` (`SyncFirewallRules`) |
| kernel interface | ✅ ไม่ต้องแก้ signature | `kernel/interfaces.go:11-21` |
| input chain (real) | ⚠️ hardcode ครบ 4 ส่วน ไม่มีกฎจาก DB ที่ผู้ใช้แก้ได้ | `real_firewall.go:127-393` (sanity `:138`, audit `:324`, dynamic `:336`, drop log `:383`) |
| กฎ input ที่ขับด้วย DB ตอนนี้ | ⚠️ มาจาก `iface.AdminAccess` + dhcp/dns ifaces เท่านั้น | `real_firewall.go:376-381`, `addAdminAccessRules:1149`, `addDNSServerAccessRules:1250` |
| พอร์ตหน้าเว็บใน Admin Access | ⚠️ hardcode 80/2479 (`:1183`), 443 (`:1206`), 22 (`:1226`) ไม่ได้อ่านจาก `cfg.Port/HTTPSPort` | เทียบ `config/config.go:38,47` |
| forward chain (real) | ✅ วน `rules` สร้าง expr แล้ว ใช้ซ้ำได้ | `real_firewall.go:485-544`, `buildRuleExpressions:976` |
| **output chain** | ❌ **ไม่มี base chain เลย** → ขาออกไม่ถูกกรอง (kernel default accept) | ทั้งไฟล์ `real_firewall.go` ไม่มี `ChainHookOutput` |
| mock firewall | ⚠️ log เฉย ๆ ยังไม่รู้จัก chain | `kernel/mock.go:30-71` |
| fake ใน unit test | ⚠️ ผูกกับ signature `ApplyRules` เดิม | `service/firewall_test.go:21-38` |
| API handlers | ⚠️ ประกอบ struct ทีละฟิลด์ (ฟิลด์ใหม่หายเงียบถ้าลืม) | `api/handlers.go:1042-1054` (create), `:1080-1092` (update) |
| routes | ✅ ครบ 7 เส้น ยังไม่มี chain param | `api/router.go:78-86` |
| backup export/import | ⚠️ export `GetPolicies` / import insert ทีละคอลัมน์ | `service/backup.go:97,657-677`, `db/backup_repo.go:137-148` |
| frontend type + service | ⚠️ ไม่มี chain, reorder ส่งทั้ง list | `data-mockup/mockData.ts:98-110`, `services/policyService.ts:45-64` |
| frontend page | ⚠️ ตารางเดียว + แถว Implicit Deny ตายตัว | `pages/FirewallPolicy.tsx:296` (form state `:433-442`, ตาราง `:737`, implicit deny `:754`) |
| openapi | ⚠️ schema ไม่มี chain (สองไฟล์) | `docs/openapi.yaml:3548,3604` + `frontend/public/openapi.yaml` |

**สรุป:** งานหนักอยู่ที่ `real_firewall.go` (เพิ่ม section 3b + output chain ใหม่) และ
`repository.go`/migration ส่วนที่เหลือเป็นการ "พาฟิลด์ `chain` เดินทาง" ให้ครบทุกชั้น —
จุดที่ฟิลด์หายเงียบได้คือ handler `:1042/:1080` และ `backup_repo.go:140`

## 2. แนวทางเทคนิค

### 2.1 Data model: เพิ่มคอลัมน์ `chain` ในตารางเดิม (ไม่สร้างตารางใหม่)

```sql
ALTER TABLE firewall_policies ADD COLUMN chain TEXT NOT NULL DEFAULT 'forward'
  CHECK(chain IN ('forward', 'input', 'output'));
```

**ทำไมใช้ตารางเดิม:** semantics เหมือนกัน 100% (source/destination/service/action/log/status/
priority/drag-drop) และ junction table `policy_addresses`/`policy_services` รวมถึงการนับ
`RefPolicies` ของ address/service object (`repository.go:205,446`) join ผ่าน `firewall_policies`
อยู่แล้ว — ถ้าแยกตารางต้อง duplicate junction + CRUD + backup + UI ทั้งชุด
**ทางเลือกที่ตัดทิ้ง:** ตาราง `input_policies`/`output_policies` แยก — ต้องแก้ 3 จุดทุกครั้งที่
เพิ่มฟิลด์ และ address object จะนับ reference ไม่ครบ (ลบ object ที่ถูกใช้อยู่ได้ = ช่องโหว่)

ฟิลด์ที่ไม่ได้ใช้ในบาง chain: `input` ไม่ใช้ `OutInterface`/`Nat`, `output` ไม่ใช้
`InInterface`/`Nat` → validate ที่ชั้น service (บังคับเป็น `"ALL"`/`false`) และ generator
เพิกเฉยอยู่แล้ว ไม่ต้องเพิ่มคอลัมน์ใหม่

### 2.2 ตำแหน่งกฎใน input chain (รักษาโครง 4 ส่วน)

```
Section 1  sanity/drop checks                    (เดิม ไม่แตะ)
Section 2  log prefix "[PiGate] INP AUDIT : "     (เดิม ไม่แตะ)
Section 3a docker compat + AdminAccess + DNS srv  (เดิม ไม่แตะ)   ← accept ก่อนเสมอ
Section 3b **กฎ input ของผู้ใช้จาก DB (ใหม่)**                     ← เพิ่มตรงนี้
Section 4  log prefix "[PiGate] INP DROP  : " + policy drop (เดิม)
```

**ทำไมกฎผู้ใช้อยู่หลัง Admin Access:** nftables ตัดสินแบบ first-match — เมื่อ accept ของ
Admin Access มาก่อน กฎ DROP ที่ผู้ใช้เขียนผิดจะ **ไม่มีทาง** ปิดหน้าเว็บ/SSH ของตัวเอง
ถ้าอยากจำกัด SSH ให้เฉพาะบางซับเน็ต ให้ปิด SSH ใน Admin Access ของอินเทอร์เฟซนั้น
(หน้า Interfaces) แล้วเขียนกฎ input ACCEPT แบบแคบแทน — ต้องอธิบายไว้ใน UI
**ทางเลือกที่ตัดทิ้ง:** เอากฎผู้ใช้ขึ้นก่อนแบบ FortiGate — ได้ความยืดหยุ่นแลกกับความเสี่ยง
ล็อกตัวเองออกจากอุปกรณ์ ซึ่งเป็น footgun ที่แก้ไม่ได้ถ้าไม่มีจอ/คีย์บอร์ดต่อบอร์ด

### 2.3 output chain (ใหม่, default-allow)

```nftables
chain output {
    type filter hook output priority filter; policy accept;
    ct state established,related accept      # กันไม่ให้กฎ deny ตัด session ที่กำลังคุยอยู่
    oifname "lo" accept
    meta nfproto ipv6 counter drop            # ปิด IPv6 ไว้ก่อน ให้เข้าชุดเดียวกับ input/forward (ดู 2.3.1)
    # --- กฎ output ของผู้ใช้จาก DB (ACCEPT/DROP) ---
    # ไม่มี final drop log — policy accept
}
```
`ct state established,related accept` เป็นด่านกันตาย: reply ของ session หน้าเว็บที่
กำลังเปิดอยู่จะไม่ถูกกฎ DROP ของผู้ใช้ตัดทิ้ง (ผู้ใช้ยังกลับเข้าไปลบกฎที่ผิดได้)
ไม่ใส่ `ct state invalid drop` ในขา output — แพ็กเก็ตที่ระบบสร้างเองบางชนิดถูก mark
invalid ได้ในบางเคส เสี่ยงกว่าประโยชน์ที่ได้

#### 2.3.1 ทำไมต้องมี `meta nfproto ipv6 drop` ใน output chain

Table `pigate` เป็น family `inet` (ครอบทั้ง IPv4/IPv6 — `real_firewall.go:59`) แต่ทุกกฎที่
เช็ก protocol ในไฟล์นี้ใช้ `expr.Payload{Offset: 9}` ซึ่งเป็นตำแหน่ง field "Protocol" ของ
**header IPv4 เท่านั้น** (header IPv6 ตำแหน่งเดียวกันตกอยู่กลาง Source Address, Next Header
ตัวจริงอยู่ offset 6) ผลคือกฎ TCP/UDP/ICMP ทุกตัวใน Section 1/3 ของ `input`/`forward`
**ไม่ match แพ็กเก็ต IPv6 เลย** แล้วไหลลงไปโดน `policy drop` ท้าย chain แทน — `input`/`forward`
จึง fail-closed สำหรับ IPv6 อยู่แล้วโดยบังเอิญ (ไม่ใช่ของตั้งใจ, เป็นบั๊กเดิมนอกขอบเขตงานนี้)
ส่วนที่ OS level `install.sh:483` ก็คอมเมนต์ `net.ipv6.conf.all.forwarding` ทิ้งไว้ (เปิดแค่
`net.ipv4.ip_forward` บรรทัด 478) จึงไม่ route IPv6 ข้าม interface อยู่แล้วเช่นกัน

ถ้า `output` chain ใหม่ (policy accept) ไม่มีบรรทัดนี้ จะกลายเป็น chain เดียวที่ "เปิด" IPv6
โดยไม่ตั้งใจ: ผู้ใช้เขียนกฎ DROP เพื่อบล็อก output ไม่ได้เลยเพราะ address object รองรับ
เฉพาะ IPv4 (ข้อ 32-33) ดังนั้น traffic ขาออก IPv6 ใดๆ (ถ้าบอร์ดมี IPv6 address ในอนาคต) จะ
หลุดออกไปแบบไม่มีใครกรองได้ — เพิ่ม `meta nfproto ipv6 counter drop` ไว้หลังกฎกันตายสองบรรทัด
แต่ก่อนกฎผู้ใช้ ให้ทั้ง 3 chain มีพฤติกรรมต่อ IPv6 สอดคล้องกัน (fail-closed เหมือนกันหมด)
ไม่ใช่การแก้บั๊ก offset — เป็นแค่ safety line เฉพาะ chain ใหม่ที่เพิ่งสร้าง

### 2.4 ใช้ generator เดิมซ้ำ

`buildRuleExpressions` (`real_firewall.go:976`) ใช้ได้ทั้ง 3 chain — เพิ่มพารามิเตอร์
`chain string` เพื่อ (1) เลือก log prefix `FWD/INP/OUT` (2) ปิดการใส่ fwmark 0x1 เมื่อไม่ใช่
forward (`:1126-1137`) (3) ข้าม iifname/oifname ที่ไม่ meaningful เขียน test ครอบด้วย
pattern เดียวกับ `kernel/port_forward_test.go` (ตรวจ expr ล้วน ไม่ต้องมี kernel จริง)

## 3. ขั้นตอนการทำ (เรียงตาม dependency: model → db → service → kernel → api → docs → frontend)

### T-01 · เพิ่มฟิลด์ `Chain` ใน model + ค่าคงที่ + validator
**ไฟล์:** `backend/internal/model/types.go` (~บรรทัด 100-128)
- `Chain string \`json:"chain"\`` ใน `PolicyRule` และ `PolicyRuleInput`
- const `PolicyChainForward = "forward"`, `PolicyChainInput`, `PolicyChainOutput`
- `func NormalizePolicyChain(c string) string` — ค่าว่าง → `"forward"` (backward compat)
- `func ValidatePolicyRule(p PolicyRule) error` — chain ต้องอยู่ใน 3 ค่า; `input` ต้องมี
  `OutInterface ∈ {"", "ALL"}` และ `Nat == false`; `output` ต้องมี `InInterface ∈ {"", "ALL"}`
  และ `Nat == false` (สไตล์เดียวกับ `ValidatePortForward`)

### T-02 · Schema + migration
**ไฟล์:** `backend/internal/db/connection.go` (CREATE ~258, migration block ~569-598)
- เติม `chain TEXT NOT NULL DEFAULT 'forward' CHECK(chain IN ('forward','input','output'))` ใน CREATE TABLE
- migration แบบเดียวกับ `nat`: อ่าน `sqlite_master` แล้ว `ALTER TABLE ... ADD COLUMN` เมื่อยังไม่มี
- **ตรวจด้วย token ที่ไม่ชนกัน** เช่น `strings.Contains(sqlCreatePolicies, "'output'")`
  ไม่ใช่คำว่า `chain` (คำนี้อาจโผล่ในคอมเมนต์/ชื่ออื่นในอนาคต) — แถวเดิมได้ `forward` อัตโนมัติ

### T-03 · Repository รองรับ chain
**ไฟล์:** `backend/internal/db/repository.go`
- `GetPolicies` (`:606`) / `GetPolicyByID` (`:668`): select `chain` เพิ่ม, เปลี่ยนเป็น
  `ORDER BY chain ASC, priority ASC` (กันลำดับกำกวมเมื่อ priority ซ้ำข้าม chain)
- `CreatePolicy` (`:725`) / `UpdatePolicy` (`:782`): insert/update คอลัมน์ `chain`
  (normalize ค่าว่าง → forward) + เรียก `model.ValidatePolicyRule`
- **`SaveAllPolicies` → `SaveChainOrder(chain string, ids []string)`** (`:1042`):
  `UPDATE ... SET priority = ? WHERE id = ? AND chain = ?` และ error ถ้ามี id ไหน
  ไม่อยู่ใน chain นั้น (กัน client ส่งข้าม chain มาสลับลำดับให้กัน)
- `GetPolicies` ยังคืนทุก chain (kernel ต้องใช้ครบ) — การกรองทำที่ service/handler

### T-04 · Backup export/import
**ไฟล์:** `backend/internal/db/backup_repo.go:137-148`, `backend/internal/service/backup.go:657-677`
- insert `chain` ในคำสั่ง restore; ไฟล์ backup เก่าไม่มีฟิลด์นี้ → normalize เป็น `forward`
- `validateConfig` เรียก `model.ValidatePolicyRule` ต่อ policy (fail-closed ทั้งไฟล์)

### T-05 · Service: validate + ฟิลเตอร์ตาม chain
**ไฟล์:** `backend/internal/service/firewall.go` (~34-77)
- `GetPolicies(chain string)` (ค่าว่าง = ทุก chain) หรือเพิ่ม `GetPoliciesByChain`
- `CreatePolicy/UpdatePolicy` normalize + validate ก่อนลง repo
- `ReorderPolicies(chain string, policies []model.PolicyRule)`
- `SyncFirewallRules` (`:120`) **ไม่ต้องแก้** — ส่ง rules ทั้งก้อนเหมือนเดิม

### T-06 · Kernel: กฎ input ของผู้ใช้ (sensitive — ต้อง review เข้ม)
**ไฟล์:** `backend/internal/kernel/real_firewall.go` (แทรกหลัง `addDNSServerAccessRules` ~`:381`)
- วนกฎที่ `Status && Chain == "input"` สร้าง expr ด้วย `buildRuleExpressions(..., chain)`
- log prefix: `"[PiGate] INP ACCEPT: "` / `"[PiGate] INP DROP  : "` (printk เหมือนของเดิมในนี้)
- **ห้าม** ใส่ fwmark/NAT ในขา input; **ห้าม** ย้ายหรือแทรกก่อน section 3a
- กฎที่เปิด log ให้ออกเป็น **สองกฎ**: กฎ `limit ... log` (ไม่มี verdict) แล้วตามด้วยกฎ
  `counter <verdict>` — เหตุผลใน §5 ข้อ 5

### T-07 · Kernel: output chain ใหม่ (sensitive — ต้อง review เข้ม)
**ไฟล์:** `backend/internal/kernel/real_firewall.go` (เพิ่มบล็อกใหม่ก่อนหัวข้อ 6 NAT ~`:555`)
- `conn.AddChain(&nftables.Chain{Name: "output", Hooknum: ChainHookOutput, Priority: ChainPriorityFilter, Policy: &policyAccept})`
  — **ประกาศตัวแปร `policyAccept` แยก** อย่าใช้ `&policyDrop` ที่มีอยู่ (`:128`) ซ้ำ
- กฎคงที่: `ct state established,related accept`, `oifname "lo" accept`,
  `meta nfproto ipv6 counter drop` (ดูเหตุผลใน 2.3.1 — ปิด IPv6 ให้เข้าชุดเดียวกับ input/forward
  ที่ fail-closed อยู่แล้วโดยบังเอิญจากบั๊ก offset เดิม)
- ตามด้วยกฎ `Chain == "output"` ของผู้ใช้ (prefix `"[PiGate] OUT ..."`) — ไม่มี final drop log
- ทุกอย่างอยู่ในทรานแซกชัน `conn.Flush()` เดิม (`:623`) ตารางถูก flush/สร้างใหม่ทั้งก้อนอยู่แล้ว

### T-08 · Mock firewall
**ไฟล์:** `backend/internal/kernel/mock.go:30-71`
- log แยกกลุ่มตาม chain (`[MockFirewall] input: N rule(s)` ฯลฯ) เพื่อให้ dev เห็นผลใน mock mode
- ไม่มี side effect ต่อ OS (เหมือนเดิม)

### T-09 · Unit test ชั้น kernel
**ไฟล์ (ใหม่):** `backend/internal/kernel/policy_chain_test.go` (แม่แบบ `port_forward_test.go`)
- กฎ chain=input/output ต้อง **ไม่มี** `expr.Meta{MetaKeyMARK}` (ไม่มี fwmark) แม้ `Nat=true`
- log prefix ถูกต้องต่อ chain, กฎที่เปิด log ต้องได้ 2 rule (limit+log แล้วค่อย verdict)
- ยืนยันว่า `output` chain มีกฎ `meta nfproto ipv6 drop` อยู่จริงก่อนกฎผู้ใช้ทุกข้อ (caution 13)

### T-10 · API handlers + routes
**ไฟล์:** `backend/internal/api/handlers.go:1021-1136`, `backend/internal/api/router.go:78-86`
- `HandleGetPolicies`: รองรับ `?chain=input` (ค่าว่าง = ทุก chain, ค่าที่ไม่รู้จัก = 400)
- `HandleCreatePolicy` (`:1042`) / `HandleUpdatePolicy` (`:1080`): **เพิ่ม `Chain:` ในการประกอบ
  struct ทั้งสองจุด** — นี่คือ whitelist gotcha เดียวกับ interface PATCH (`handlers.go:713`)
  ฟิลด์ใหม่ที่ไม่ได้ใส่จะหายเงียบโดยไม่มี error
- update: ถ้า `input.Chain == ""` ให้ใช้ `existing.Chain` **ห้าม** fallback เป็น `forward`
  (ไม่งั้น client เก่ายิง PUT แล้วกฎ input กลายเป็นกฎ forward เงียบ ๆ)
- `HandleReorderPolicies` (`:1119`): body เพิ่ม `chain` + ตรวจว่าทุก id อยู่ใน chain นั้น
- ไม่เพิ่ม route ใหม่ — ทางเลือกที่ตัดทิ้ง: `/api/input-policies` (ต้อง duplicate 7 handler + openapi)

### T-11 · เอกสาร API (sync สองไฟล์)
**ไฟล์:** `docs/openapi.yaml:3548,3604` และ `frontend/public/openapi.yaml`
- เพิ่ม `chain` (enum forward/input/output, default forward) ใน `PolicyRule`/`PolicyRuleInput`,
  query param `chain` ของ `GET /policies`, ฟิลด์ `chain` ใน body ของ `/policies/reorder`
- เนื้อหาสองไฟล์ต้องตรงกันเป๊ะ

### T-12 · Frontend type + service
**ไฟล์:** `frontend/src/data-mockup/mockData.ts:98-110`, `frontend/src/services/policyService.ts`
- `chain: "forward" | "input" | "output"` ใน `PolicyRule`
- `getAll(chain?)`, `saveAll(chain, policies)` (ส่ง `{ chain, policies }`) — ใช้ซ้ำได้ทั้ง 3 หน้า
  ในเมนู (Firewall Policy / Local-In / Local-Out ตาม T-13/T-14)
- โหมด mock (localStorage): แถวเก่าที่ไม่มี `chain` ให้เติม `"forward"` ตอนอ่าน

### T-13 · Frontend: แตก `FirewallPolicy.tsx` เป็น component ใช้ซ้ำ
**ไฟล์ (ใหม่):** `frontend/src/components/policy/PolicyChainPage.tsx`
**ไฟล์ (แก้):** `frontend/src/pages/FirewallPolicy.tsx:296` (ย้าย logic ออก)
- ตัดสินใจแยก **3 หน้าเมนูจริง** (Firewall Policy / Local-In Policy / Local-Out Policy) ไม่ใช่
  Tabs ในหน้าเดียว (เปลี่ยนจากดราฟต์แรกตามที่ผู้ใช้ต้องการ — ศัพท์ "Local-In"/"Local-Out"
  ยืมจาก FortiGate ให้เข้าธีมเดิมของโปรเจกต์) แต่ทั้ง 3 หน้าเหมือนกัน ~95% (table, drag-reorder,
  drawer form, apply) ต่างแค่ chain + field visibility + ข้อความ — ถ้า copy-paste ตรงๆ เป็น 3
  ไฟล์แยกอิสระ บั๊กที่แก้หน้าเดียวจะไม่ถูกแก้อีก 2 หน้า → ย้าย logic ทั้งหมดของหน้าปัจจุบันเข้า
  `PolicyChainPage.tsx` รับ prop `chain: "forward" | "input" | "output"` + `pageTitle` +
  `pageDescription` แล้วให้ 3 ไฟล์ `pages/*.tsx` เป็น wrapper บางๆ เรียก component นี้ (ดู T-14)
- ฟอร์ม: ซ่อน Out Interface + NAT เมื่อ `chain === "input"`, ซ่อน In Interface + NAT เมื่อ
  `chain === "output"` (`:878-914`, `:1018-1035`)
- แถวท้ายตาราง (`:754`): forward/input = "Implicit Deny (DROP)", output = "Implicit Allow (ACCEPT)"
- `chain === "input"` แสดง `<Alert>` อธิบายว่า Admin Access (หน้า Interfaces) ถูกประเมินก่อนเสมอ
  `chain === "output"` แสดง `<Alert>` อธิบายว่า default คืออนุญาตทั้งหมด (ยกเว้น IPv6 ดู 2.3.1)
- ใช้ตัวแปรสีเชิงความหมายเท่านั้น ห้าม `shadow-*`/`backdrop-blur-*`; Drawer เดิมมี Combobox
  อยู่แล้ว — คงรูปแบบ portal ที่ `drawerContentRef` ไว้ตามเดิม (`:799-813`)

### T-14 · Frontend: หน้าใหม่ Local-In/Local-Out + routing + เมนู
**ไฟล์ (ใหม่):** `frontend/src/pages/LocalInPolicy.tsx`, `frontend/src/pages/LocalOutPolicy.tsx`
**ไฟล์ (แก้):** `frontend/src/pages/FirewallPolicy.tsx` (เหลือแค่ wrapper), `frontend/src/App.tsx:13,157`,
`frontend/src/components/app-sidebar.tsx:63-68`, `frontend/src/components/site-header.tsx:18-20`
- `FirewallPolicy.tsx` → `<PolicyChainPage chain="forward" pageTitle="Firewall Policy" .../>`
- `LocalInPolicy.tsx` (ใหม่) → `<PolicyChainPage chain="input" pageTitle="Local-In Policy" .../>`
- `LocalOutPolicy.tsx` (ใหม่) → `<PolicyChainPage chain="output" pageTitle="Local-Out Policy" .../>`
- Route ใหม่ 2 เส้นใน `App.tsx`: `/policy/local-in`, `/policy/local-out` (import + `<Route>`)
- Sidebar (`app-sidebar.tsx` group "Policy & Objects"): เพิ่ม 2 item ต่อจาก "Firewall Policy"
  เดิม — เลือกไอคอน lucide ที่สื่อถึงทราฟฟิกเข้า/ออกตัวอุปกรณ์เอง (เช่น `LogIn`/`LogOut`)
  ให้ทีม frontend ตัดสินใจตอน implement
- `site-header.tsx` `TITLES` map: เพิ่ม `"/policy/local-in": "Local-In Policy"`,
  `"/policy/local-out": "Local-Out Policy"`

### T-15 · Backend test + เอกสารออกแบบ
**ไฟล์:** `backend/internal/db/chain_migration_test.go` (ใหม่, ลอก `nat_migration_test.go`),
`backend/internal/service/firewall_test.go` (เพิ่มเคส chain), `docs/tech_stack_design.md` §4.3, `README.md`
- migration test: DB เก่าที่ไม่มีคอลัมน์ → หลัง `InitDB` ทุกแถวต้องเป็น `forward`
- อัปเดตตัวอย่าง nftables ใน §4.3 ให้มี section 3b + chain `output` และคำอธิบายลำดับ

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/policies?chain=` (เดิม + param ใหม่) | ทุก role ที่ล็อกอิน | ไม่ใส่ = ทุก chain (backward compat) |
| POST | `/api/policies` (เดิม) | super_admin (`RoleReadOnlyMiddleware` บล็อก readonly) | body เพิ่ม `chain`, ค่าว่าง = forward |
| PUT | `/api/policies/{id}` (เดิม) | super_admin | ค่าว่าง = คง chain เดิมของแถวนั้น |
| PUT | `/api/policies/reorder` (เดิม) | super_admin | body เพิ่ม `chain`; id ข้าม chain = 400 |
| POST | `/api/policies/apply` (เดิม) | super_admin | สร้างทั้ง 3 chain ในทรานแซกชันเดียว |

- `-disable-edit=true`: `DisableEditMiddleware` บล็อก mutation ทั้งหมดอยู่แล้ว — ถูกต้องตามที่ควรเป็น
- Client เก่า (ไม่ส่ง `chain`) ยังทำงานได้เหมือนเดิมทุกเส้น

## 5. ข้อควรระวัง

1. **ฟิลด์ใหม่หายเงียบที่ handler** — `handlers.go:1042` และ `:1080` ประกอบ `model.PolicyRule`
   ทีละฟิลด์ ถ้าลืมใส่ `Chain:` จะ compile ผ่าน ไม่มี error แต่กฎ input ทุกข้อกลายเป็น forward
   (บั๊กประเภทเดียวกับ whitelist ของ interface PATCH `:713`) → ต้องมีเทสต์ระดับ handler ยืนยัน
2. **PUT ที่ไม่ส่ง chain ต้องไม่ย้าย chain** — ถ้า fallback เป็น `forward` กฎ DROP ที่เคย
   ปกป้องตัวบอร์ดจะย้ายไปขวางทราฟฟิกที่วิ่งผ่านบอร์ดแทน (เปลี่ยนพฤติกรรมเครือข่ายทั้งวง)
   → ป้องกันด้วยการอ่าน `existing.Chain` ที่ handler อยู่แล้ว (`:1068`)
3. **priority ซ้ำข้าม chain** — หลังเปลี่ยน reorder ให้ scope ตาม chain แต่ละ chain จะมี
   priority 1..N ของตัวเอง ถ้าที่ไหนยัง `ORDER BY priority` เฉย ๆ ลำดับจะสลับแบบสุ่มเมื่อค่าซ้ำ
   → บังคับ `ORDER BY chain, priority` ใน `GetPolicies` และห้าม generator พึ่งลำดับ global
4. **ห้ามแก้ signature `FirewallManager.ApplyRules`** — `service/firewall_test.go:21` และ
   `kernel/mock.go:30` implement ตามนั้น การเพิ่มพารามิเตอร์ทำให้ test พังทั้งชุดโดยไม่จำเป็น
   (ฟิลด์ `Chain` เดินทางไปกับ `[]model.PolicyRule` อยู่แล้ว)
5. **กับดัก `limit` + verdict ในกฎเดียว** — ใน nftables กฎ `limit rate X log ... drop`
   จะ **drop เฉพาะแพ็กเก็ตที่ผ่านตัวจำกัดอัตรา** ที่เหลือหลุดไปกฎถัดไป = กฎความปลอดภัยรั่ว
   → ต้องแยกเป็นสองกฎเสมอ (กฎ log ที่มี limit ไม่มี verdict, แล้วกฎ verdict มี counter)
   เหตุผลที่ต้องมี limit: log ของ input/output ไปที่ printk → journald → เขียนลง SD card
   (ขัดหัวข้อ 8 ของ tech_stack_design) ต่างจาก forward ที่ส่งเข้า NFLOG ring buffer ในแรม
6. **อย่าส่ง log ของ input/output เข้า NFLOG group 100** — `parseNflogAttr`
   (`real_traffic_log.go:154-174`) ฮาร์ดโค้ด reason เป็น "Blocked/Allowed (forward)" และหน้า
   Forward Traffic ตั้งใจแสดงเฉพาะทราฟฟิกที่วิ่งผ่านบอร์ด → log จะปนและ label ผิด
   ถ้าจะทำหน้า log รวมทีหลัง ต้องแก้ parser ให้ดู prefix `INP/OUT/FWD` ก่อน (แผนแยก)
7. **Admin Access ใช้พอร์ตฮาร์ดโค้ด** — `addAdminAccessRules` เปิด 80/2479/443/22 โดยไม่อ่าน
   `cfg.Port`/`cfg.HTTPSPort` (`config.go:38,47`) ถ้าผู้ใช้เปลี่ยนพอร์ตหน้าเว็บในไฟล์ config
   จะไม่มีกฎ accept ให้พอร์ตนั้น และตอนนี้ยัง "รอด" เพราะ input chain ไม่มีกฎผู้ใช้มาแย่งลำดับ
   → **ไม่แก้ในแผนนี้** แต่ต้องบันทึกเป็น issue แยก เพราะเป็นเหตุล็อกตัวเองที่มีอยู่ก่อนแล้ว
8. **`output` ต้องเป็น policy accept เท่านั้นในเฟสนี้** — ถ้าเผลอใช้ตัวแปร `policyDrop` เดิม
   ซ้ำ (`real_firewall.go:128`) บอร์ดจะตัดขาตัวเองทันทีที่ apply (DNS/NTP/dhcpcd/หน้าเว็บดับ
   พร้อมกัน) และกู้ได้ทางจอ+คีย์บอร์ดเท่านั้น → ตรวจในรีวิว + มีเทสต์ยืนยันค่า Policy
9. **ทดสอบบนบอร์ดจริงมีความเสี่ยงล็อกตัวเอง** — ทดสอบเมื่อ *เข้าถึงบอร์ดทางกายภาพได้เท่านั้น*
   ลำดับปลอดภัย: กฎ input ACCEPT → input DROP แคบ ๆ (เช่นบล็อก ping จากอินเทอร์เฟซเดียว) →
   output DROP แคบ ๆ → ค่อยทดสอบกฎกว้าง และเปิด SSH session ค้างไว้อีกหน้าต่างเสมอ
10. **netlink monitor ไม่เกี่ยว** — `netlink_monitor.go` ดูแล route/interface ไม่ยุ่งกับ nftables
    และลำดับ boot ใน `main.go:397-400` ไม่ต้องเปลี่ยน (ยังเป็น `InitApplyConfig` ตัวเดิม)
11. **`install.sh` ไม่ต้องแก้** — ไม่มีสิทธิ์ใหม่ (ใช้ `cap_net_admin` เดิม) และไม่มี Polkit/sudoers
    เพิ่ม → อุปกรณ์ที่ติดตั้งไปแล้วอัปเกรดได้ด้วยการเปลี่ยนไบนารี ไม่ต้องรัน install ซ้ำ
12. **Backup ไฟล์เก่า** — ไม่มีฟิลด์ `chain` ต้อง normalize เป็น `forward` ตอน import
    ไม่ใช่ปล่อยค่าว่างลง DB (CHECK constraint จะทำให้ restore ล้มทั้งก้อน)
13. **IPv6 ใน `output` chain ใหม่ต้อง fail-closed เหมือน input/forward** — `input`/`forward`
    บล็อก IPv6 อยู่แล้วโดยบังเอิญ (บั๊ก offset การเช็ก protocol ที่อ่าน byte ผิดตำแหน่งสำหรับ
    header IPv6 ดู 2.3.1) แต่ `output` เป็น chain ใหม่ล้วนที่ตั้ง policy accept — ถ้าลืมใส่
    `meta nfproto ipv6 counter drop` จะกลายเป็นรูเดียวที่ปล่อย IPv6 ออกแบบไม่มีใครกรองได้เลย
    (address object ผู้ใช้รองรับเฉพาะ IPv4 จึงเขียนกฎมาบล็อกเองไม่ได้) → ต้องมีเทสต์ยืนยันว่า
    output chain มีกฎนี้อยู่จริง (`policy_chain_test.go`, T-09) ไม่ใช่แค่ตรวจ policy accept
    เฉยๆ ไม่ใช่การแก้บั๊ก offset เดิม — นั่นเป็น issue แยก (ดูข้อ 7)

## 6. Checklist สรุป (Definition of Done)

- [ ] `backend/internal/model/types.go` — `Chain` + const + `ValidatePolicyRule`
- [ ] `backend/internal/db/connection.go` — คอลัมน์ `chain` ใน CREATE + migration (ตรวจด้วย token `'output'`)
- [ ] `backend/internal/db/repository.go` — CRUD + `ORDER BY chain, priority` + reorder scoped ตาม chain
- [ ] `backend/internal/db/backup_repo.go` + `service/backup.go` — export/import/validate ฟิลด์ `chain`
- [ ] `backend/internal/service/firewall.go` — normalize/validate + ฟิลเตอร์ตาม chain
- [ ] `backend/internal/kernel/real_firewall.go` — section 3b (input) + chain `output` (policy accept,
      รวม `meta nfproto ipv6 counter drop` ตาม caution 13)
- [ ] `backend/internal/kernel/mock.go` — log แยกตาม chain
- [ ] `backend/internal/kernel/policy_chain_test.go` (ใหม่) + `db/chain_migration_test.go` (ใหม่)
- [ ] `backend/internal/api/handlers.go` + `router.go` — `chain` ใน create/update/reorder/query
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` (ตรงกันทั้งสองไฟล์)
- [ ] `frontend/src/data-mockup/mockData.ts` + `services/policyService.ts`
- [ ] `frontend/src/components/policy/PolicyChainPage.tsx` (ใหม่) — ฟอร์มตามบริบท + แถวท้ายตาราง + คำเตือน
- [ ] `frontend/src/pages/FirewallPolicy.tsx` (wrapper) + `LocalInPolicy.tsx`/`LocalOutPolicy.tsx` (ใหม่)
- [ ] `frontend/src/App.tsx` + `app-sidebar.tsx` + `site-header.tsx` — route/เมนู/title 2 รายการใหม่
- [ ] `docs/tech_stack_design.md` §4.3 + README (ถ้าจำเป็น)
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน

**ทดสอบรวมท้ายแผน (ทำครั้งเดียวหลังทุก Task เสร็จ):**
- [ ] DB เดิมที่มีกฎ forward อยู่ก่อน: อัปเกรดแล้วกฎทั้งหมดยังเป็น `forward`, ใช้งานได้เหมือนเดิม
- [ ] mock mode (`-mock=true`): สร้าง/แก้/ลบ/ลากจัดลำดับได้ครบทั้ง 3 หน้า (Firewall Policy /
      Local-In Policy / Local-Out Policy), log ของ MockFirewall แสดงจำนวนกฎแยกตาม chain ถูกต้อง
- [ ] Export backup → ล้าง DB → Import กลับ: `chain` ของทุกกฎตรงเดิม; import ไฟล์ backup เก่า
      (ไม่มีฟิลด์ `chain`) ต้องสำเร็จและได้ `forward`
- [ ] client เก่า/คำสั่ง curl ที่ไม่ส่ง `chain`: POST ได้ forward, PUT ไม่ย้าย chain ของกฎเดิม
- [ ] role `admin_readonly`: GET ได้ทุก chain, POST/PUT/DELETE ได้ 403; `-disable-edit=true` บล็อก mutation
- [ ] **บนบอร์ดจริง (เจ้าของโปรเจกต์ทดสอบเอง, ต้องเข้าถึงบอร์ดทางกายภาพ):**
      `nft list ruleset` เห็น input chain เรียง sanity → audit → AdminAccess → กฎผู้ใช้ → drop log,
      เห็น chain `output` เป็น `policy accept`; กฎ input DROP ที่ทับพอร์ตหน้าเว็บ **ต้องไม่ทำให้
      หลุดจากหน้าเว็บ**; กฎ output DROP แคบ ๆ ทำงานจริงและ session หน้าเว็บที่เปิดค้างไม่หลุด;
      counter ของกฎใหม่เพิ่มขึ้นตามทราฟฟิกจริง; `journalctl -k` ไม่ถูก log ท่วมเมื่อเปิด log บนกฎที่โดนยิงถี่
