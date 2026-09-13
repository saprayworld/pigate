# nftables Sets Refactor (Phase 2) — Work Plan

สถานะ: **ยังไม่เริ่ม — เอกสารเตรียมการเท่านั้น** เจ้าของโปรเจกต์อนุมัติให้ "เปิด issue + commit แผนไว้ก่อน"
แต่ **ยังไม่มอบหมายให้ ai-developer ลงมือ** จนกว่าจะมีคำสั่งเพิ่มเติม
เอกสารนี้เป็น docs-only (push เข้า `main` ตรงได้ตาม CLAUDE.md git workflow) — เมื่อถึงเวลาลงมือจริง
ต้องแตก branch ใหม่ (`refactor/nftables-sets`) และเข้า PR เท่านั้น

ที่มา: ต่อยอดจาก Phase 1 (`fix/nft-netlink-enobufs`) ซึ่งแก้อาการ `no buffer space available` (ENOBUFS)
ตอน `RealFirewall.ApplyRules` ด้วยการขยาย netlink socket buffer + ใส่งบประมาณ rule รวม
Phase 1 เป็นการ **ยกเพดาน** ให้สูงพอใช้งาน แต่ไม่ได้แก้ที่ต้นตอว่า "ทำไม 1 policy rule ถึงกลายเป็น
nft rule หลักพันได้ตั้งแต่แรก" — Phase 2 คือการแก้ที่ต้นตอนั้น

---

## 1. สภาพปัจจุบัน (ข้อเท็จจริงจากโค้ด ไม่ใช่การเดา)

### 1.1 การขยายกฎแบบ cartesian

`backend/internal/kernel/real_firewall.go`

- `addUserChainRules` (บรรทัด ~1439-1574) วนซ้อนกัน 6 ชั้น:
  `source name → destination name → service name → srcCombo → destCombo → svcCombo`
  แล้วเรียก `buildRuleExpressions` ซึ่งยัง**ขยายต่ออีกชั้น**เป็น cartesian ของ
  `inInterfaces × outInterfaces` (บรรทัด ~1760-1819, D-1 Option A ของ
  `multi-interface-firewall-rule-plan.md`)
- ผลลัพธ์: 1 `model.PolicyRule` → **M × N × K × proto × (in × out)** nft rules
  (M = จำนวน entry ของ address object ต้นทาง, N = ปลายทาง, K = จำนวน entry ของ service object,
  proto = 2 เมื่อ service entry เป็น `TCP/UDP`, และ entry ชนิด `fqdn` ขยายเป็น 1 combo ต่อ 1 A record
  สูงสุด `maxFQDNResolvedIPs` = 8 — บรรทัด 72)
- มี guard 2 ชั้นคือ `maxExpandedRulesPerPolicy` (default 4096, ต่อ 1 policy rule) และ
  `maxTotalNftRules` (default 16384, รวมทั้ง ruleset — เพิ่มใน Phase 1)
- `addUserChainRules` ถูกเรียก **3 ครั้ง** ต่อ 1 apply (input บรรทัด ~608, forward ~717, output ~789)

### 1.2 การจับคู่ match แต่ละชนิดในวันนี้ (ทุกอย่างเป็น payload+cmp ตรง ๆ ไม่มี set เลย)

- IP: `buildIPMatchExpressions` (บรรทัด ~1111-1201) — `/32` = `Payload+Cmp`,
  subnet อื่น = `Payload+Bitwise+Cmp`, range = `Payload+Cmp(Gte)+Cmp(Lte)`
- FQDN: `ipMatchExprsForCombo` (บรรทัด ~1208-1225) — resolve ล่วงหน้าใน `addressCombos`
  แล้วกลายเป็น `Payload+Cmp` ต่อ 1 IP
- Port: อยู่ใน `buildRuleExpressions` (บรรทัด ~1693-1745) — เลขเดี่ยว = `Cmp(Eq)`,
  ช่วง = `Cmp(Gte)+Cmp(Lte)`
- Interface: `Meta{IIFNAME/OIFNAME} + Cmp(Eq, padInterfaceName(...))` (บรรทัด ~1767-1775)

### 1.3 สิ่งที่ผูกติดกับ "1 DB rule = หลาย nft rule" อยู่ในปัจจุบัน

- **per-rule counter** — `ruleUserData := userdata.AppendString(nil, userdata.TypeComment, r.ID)`
  (บรรทัด ~1470) ติดไปกับ nft rule ทุกตัวที่ขยายออกมา แล้ว
  `accumulateRuleCounters` (`real_traffic_account.go:184`) **บวกรวม** counter ของทุก nft rule
  ที่มี rule id เดียวกันกลับเป็นตัวเลขเดียวของ DB rule
- **rule-name log token** — `withRuleToken(logPrefix, r.ID)` (บรรทัด ~1530, helper บรรทัด 171-180)
  ฝัง `r=<id>` ลง NFLOG prefix ให้ `real_traffic_log.go` อ่านกลับ
- **FQDN snapshot** — `fqdnRecorder` (บรรทัด 123-160) บันทึก FQDN → resolved IPv4 ที่ apply
  รอบนี้ใช้จริง เพื่อให้ `service/fqdn_refresh.go` รู้ว่าต้อง retry อันไหน

### 1.4 API ของ `github.com/google/nftables@v0.3.0` ที่จะใช้ (ยืนยันจากซอร์สใน GOMODCACHE)

- `nftables.Set{Name, Table, Anonymous, Constant, Interval, KeyType, KeyByteOrder}` (`set.go:243-274`)
- `conn.AddSet(s *Set, vals []SetElement) error` (`set.go:493`) —
  **`set.go:500` บังคับว่า anonymous set ต้องเป็น constant เสมอ**
- `nftables.SetElement{Key, Val, IntervalEnd}` (`set.go:275-281`)
- datatype: `TypeIPAddr` (ipv4_addr), `TypeInetService`, `TypeInetProto`, `TypeIFName` (`set.go:80-114`)
- `MustConcatSetType(types ...SetDatatype)` / `ConcatSetType` (`set.go:199-233`) สำหรับ set แบบ concat
  (เช่น `proto . port`)
- `expr.Lookup{SourceRegister, SetName, SetID, Invert}` สำหรับ match กับ set
- **anonymous set ใช้ได้เฉพาะภายใน batch เดียวกัน** — ตรงกับดีไซน์ single-`Flush()` ของ
  `ApplyRules` พอดี (จึงไม่ต้องบริหาร lifecycle ของ named set ข้าม apply)

---

## 2. ปัญหาที่ต้นตอ

การเขียนกฎแบบ "1 nft rule ต่อ 1 ชุดค่าที่เป็นไปได้" ทำให้:

1. **จำนวน netlink message โตแบบคูณกัน** — เป็นสาเหตุรากของ ENOBUFS/EMSGSIZE ที่ Phase 1 แก้แบบยกเพดาน
2. **ประสิทธิภาพการ match ใน kernel แย่ลง** — nftables ประเมินกฎแบบ linear ต่อแพ็กเก็ต
   ในขณะที่ set lookup เป็น hash/rbtree (O(1)/O(log n))
3. **หน่วยความจำ kernel** — 1 rule มี overhead ของตัวเอง (trans object, expression list)
4. **เวลา apply** — `Flush()` ต้องอ่าน ack ทีละข้อความ (`nftables@v0.3.0/conn.go:266-276`)
   = 2 syscall ต่อ 1 rule
5. **เพดานที่ผู้ใช้มองไม่เห็น** — ผู้ใช้เห็นแค่ "3 policy rules" แต่ระบบสร้าง 8,000 nft rules

---

## 3. แนวทางที่เสนอ — แทน cartesian ด้วย nftables sets

เป้าหมาย: 1 `model.PolicyRule` → **1 nft rule ต่อ 1 (chain, in-iface, out-iface) ที่จำเป็น**
โดยเงื่อนไข address/service ทั้งหมดยุบเป็น set lookup

### 3.1 mapping ต่อชนิดข้อมูล

| สิ่งที่ match วันนี้ | หลัง refactor |
|---|---|
| address object M entries (subnet/range) | 1 anonymous set `Interval: true, Constant: true, KeyType: TypeIPAddr` + `expr.Lookup` — subnet และ range ใส่เป็น interval element ชุดเดียวกันได้ |
| address entry `/32` | element เดี่ยวใน set เดียวกัน (ไม่ต้องแยก path) |
| address entry `fqdn` | resolve เหมือนเดิม แล้วเติม IP ที่ได้เป็น element ใน set เดียวกัน (ไม่สร้าง rule เพิ่ม) |
| service object K entries | set แบบ concat `proto . dport` (`MustConcatSetType(TypeInetProto, TypeInetService)`) ครอบคลุมทั้ง TCP/UDP/ช่วงพอร์ตในชุดเดียว — ต้องใช้ `Interval: true` เพื่อรองรับช่วงพอร์ต |
| service entry `ICMP` | ICMP ไม่มี dport → ต้องแยกออกจาก concat set (ดู Caution 3) |
| `inInterfaces` / `outInterfaces` หลายตัว | set `KeyType: TypeIFName` + `expr.Lookup` แทน cartesian in × out |

### 3.2 ผลที่คาดหวัง

- policy ที่วันนี้ขยายเป็น `10 src × 10 dst × 5 svc × 2 proto × 3 iface = 3,000` rules
  จะเหลือ **1 rule + 3 anonymous sets** (รวม element ประมาณ 25 ตัว)
- `maxExpandedRulesPerPolicy` / `maxTotalNftRules` แทบไม่มีวันถูกแตะอีก (แต่ **ห้ามลบทิ้ง** —
  ยังเป็น defense-in-depth และ config key ที่ผู้ใช้อาจตั้งไว้แล้ว)

### 3.3 สิ่งที่ยังต้องขยายเป็นหลายกฎอยู่ (ยอมรับได้)

- ICMP + non-ICMP ใน service object เดียวกัน (โครงสร้าง header ต่างกัน)
- IPv4 vs IPv6 (ปัจจุบันรองรับแค่ IPv4 อยู่แล้ว — ดู `buildIPMatchExpressions` ที่ปฏิเสธ non-IPv4)
- forward chain ที่ต้องใส่ fwmark สำหรับ NAT (ยังเป็น expr เดิม ไม่กระทบ)

---

## 4. Scope ที่กระทบ (ต้องแก้/ต้องตรวจทุกจุด)

1. `backend/internal/kernel/real_firewall.go` — `addressCombos`, `serviceCombos`,
   `buildRuleExpressions`, `addUserChainRules`, `buildIPMatchExpressions`, `ipMatchExprsForCombo`
   (บางตัวอาจถูกแทนที่ทั้งหมด)
2. **per-rule counter** — เมื่อ 1 DB rule = 1 nft rule แล้ว `accumulateRuleCounters`
   (`real_traffic_account.go:184-200`) ยังทำงานได้ (การบวกรวมกลายเป็นการบวกค่าเดียว) แต่ต้อง
   **ทดสอบยืนยัน** ว่าตัวเลขบนหน้า Statistics ไม่เพี้ยน และ `UserData` ยังติดกับ rule เสมอ
3. **rule-name log token** — `withRuleToken` ยังใช้ได้เหมือนเดิม แต่ต้องตรวจว่า log prefix
   ยังผูกกับ rule ที่ถูกต้องเมื่อ 1 rule ครอบหลายเงื่อนไข
4. **FQDN** — `fqdnRecorder` / `ResolveFQDNIPv4` / `service/fqdn_refresh.go` ยังต้องได้ข้อมูลชุดเดิม
   (การเปลี่ยนไปเป็น set element ต้องไม่ทำให้ refresher เห็น "changed" ผิด ๆ —
   ดู `fqdn-retry-and-monitored-counters-plan.md` D-1/D-2 เรื่องการ sort ก่อน cap)
5. **`DumpRuleCounters`** — จำนวน rule ที่ dump กลับมาจะลดฮวบ ต้องตรวจว่า
   `service/traffic_stats.go` / หน้า Statistics ไม่มี logic ที่สมมติว่ามีหลาย nft rule ต่อ 1 DB rule
6. **`policy_chain_test.go`** และเทสต์อื่นในแพ็กเกจ kernel ที่นับจำนวน `NFT_MSG_NEWRULE`
7. **`docs/tech_stack_design.md` §4.3** — ตัวอย่าง ruleset ในเอกสารต้องอัปเดตให้สะท้อน set
8. **`docs/openapi.yaml`** — ไม่ควรกระทบ (ไม่มีการเปลี่ยน API contract) แต่ต้องยืนยัน

---

## 5. Cautions (ข้อห้าม/ข้อควรระวัง)

1. **ห้ามเปลี่ยนโครงสร้าง 4 ส่วนของ chain `input`** ตาม `docs/tech_stack_design.md` section 4.3
   (Sanity → Audit → Dynamic Accept 3a → User rules 3b → Final Drop) และ Admin Access
   ต้องอยู่ **ก่อน** user rules เสมอ — เป็นการรับประกันเชิงโครงสร้างว่ากฎที่ผู้ใช้เขียนผิด
   จะล็อกตัวเองออกจากหน้าเว็บ/SSH ไม่ได้
2. **ห้ามแตะ atomic single-`Flush()`** — anonymous set ต้องอยู่ใน batch เดียวกับ rule ที่อ้างถึงมัน
   อยู่แล้วโดยธรรมชาติ ห้ามแยก flush ด้วยเหตุผลใดก็ตาม
   (`pigate_nat` prerouting/postrouting ต้องอยู่ pass เดียวกัน — comment `real_firewall.go:825`)
3. **ICMP กับ concat set** — service object ที่ผสม ICMP กับ TCP/UDP ในตัวเดียวกันต้อง
   แตกเป็นอย่างน้อย 2 rules ห้ามพยายามยัดลง set เดียว
4. **semantics ต้องเท่าเดิมเป๊ะ** — nftables set แบบ interval มีพฤติกรรมขอบเขต (auto-merge,
   overlapping element) ที่ต่างจาก `Gte/Lte` ต้องมีเทสต์เทียบผลลัพธ์ก่อน/หลัง
   โดยเฉพาะ subnet ที่ซ้อนทับกัน และ range ที่คร่อมกัน
5. **`Anonymous` ต้องคู่กับ `Constant`** เสมอ (`set.go:500` จะคืน error ถ้าไม่ใช่)
6. **ห้ามใช้ `exec.Command` / `nft` CLI** เพื่อจัดการ set — ต้องผ่าน netlink เท่านั้น
   (hard constraint ของโปรเจกต์)
7. **ห้ามลบ `maxExpandedRulesPerPolicy` / `maxTotalNftRules`** — ต้องคงไว้เป็น safety net
   และเพื่อไม่ให้ pigate.conf ที่ผู้ใช้มีอยู่แล้วเกิด unknown-key warning
8. งานนี้แตะ firewall rule generation โดยตรง = **sensitive** ต้อง review เข้มเป็นพิเศษ

---

## 6. Task breakdown ระดับสูง (ยังไม่แตกละเอียด)

| id | หัวข้อ | layer | หมายเหตุ |
|---|---|---|---|
| P2-01 | เขียน helper สร้าง anonymous IP set จาก `[]model.AddressEntry` (subnet + range + fqdn ที่ resolve แล้ว) พร้อม unit test เทียบ semantics กับ `buildIPMatchExpressions` เดิม | kernel | pure function ทดสอบได้โดยไม่ต้องมี netlink |
| P2-02 | เขียน helper สร้าง concat set `proto . dport` จาก `[]model.ServiceEntry` + แยกกรณี ICMP ออก | kernel | |
| P2-03 | เขียน helper สร้าง ifname set จาก `[]string` interface | kernel | |
| P2-04 | เปลี่ยน `buildRuleExpressions` ให้คืน 1 ruleset ที่ใช้ `expr.Lookup` แทน cartesian (คง `UserData`/log token/fwmark/NAT/verdict เดิมทุกประการ) | kernel | **sensitive — review เข้ม** |
| P2-05 | เปลี่ยน `addUserChainRules` ให้สร้าง set ผ่าน `conn.AddSet` ในทรานแซกชันเดียวกันก่อน `AddRule` | kernel | **sensitive — review เข้ม** |
| P2-06 | ตรวจ/ปรับ `accumulateRuleCounters` + `service/traffic_stats.go` ให้ตัวเลข per-rule ถูกต้อง | kernel/service | |
| P2-07 | ตรวจ/ปรับ FQDN path (`fqdnRecorder`, `service/fqdn_refresh.go`) | kernel/service | |
| P2-08 | ปรับ/เพิ่มเทสต์ในแพ็กเกจ kernel (รวมเทสต์เทียบ before/after semantics และเทสต์ที่ยืนยันว่าจำนวน nft rule ลดลงจริง) | kernel | |
| P2-09 | อัปเดต `docs/tech_stack_design.md` §4.3 (ตัวอย่าง ruleset ที่มี set) และ CLAUDE.md ถ้าจำเป็น | docs | |
| P2-10 | ทดสอบบน Raspberry Pi 5 จริง: เทียบ `nft list ruleset` before/after, วัดจำนวน rule, วัดเวลา apply, ยืนยัน traffic ผ่าน/ถูกบล็อกเหมือนเดิม | qa | |

---

## 7. Final Acceptance ระดับสูง (จะแตกละเอียดตอนอนุมัติให้เริ่มงาน)

1. `go build ./...`, `go vet ./...`, `go test ./...` ผ่านทั้งหมด
2. ไม่มี `exec.Command` เพิ่ม, `conn.Flush()` ยังมีจุดเดียวใน `real_firewall.go`
3. โครงสร้าง 4 ส่วนของ chain `input` และตำแหน่ง Admin Access ก่อน user rules ไม่เปลี่ยน
4. ชุด policy เดิมที่เคยขยายเป็นหลักพัน rule ต้องเหลือหลักสิบ พร้อมหลักฐานจาก `nft list ruleset`
5. พฤติกรรมการ match เหมือนเดิมทุกกรณี (มีเทสต์เทียบ before/after ต่อ entry type:
   subnet, subnet ซ้อนกัน, /32, range, range คร่อมกัน, fqdn หลาย A record,
   service TCP/UDP/ICMP/ช่วงพอร์ต, หลาย interface)
6. ตัวเลข per-rule counter และชื่อกฎบนหน้า Traffic Log / Statistics ยังถูกต้อง
7. บน Pi 5 จริง: SSH/HTTP เดิมไม่หลุดระหว่าง apply, ping ผ่าน, NAT/port-forward ยังทำงาน
8. FQDN refresher ไม่เกิด reapply loop (ตรวจ log 30 นาที)

---

## 8. คำถามที่ต้องให้เจ้าของโปรเจกต์ตัดสินก่อนเริ่ม

1. **anonymous set vs named set** — anonymous ง่ายกว่ามาก (lifecycle ผูกกับ rule อัตโนมัติ)
   แต่ผู้ใช้จะไม่เห็นชื่อ address object ใน `nft list ruleset`
   ส่วน named set (ตั้งชื่อตาม address object) อ่านง่ายกว่ามากตอน debug
   แต่ต้องจัดการ lifecycle เอง (ลบ set ที่ไม่ถูกอ้างถึงแล้ว) — เลือกอันไหน?
2. **จะคง `maxExpandedRulesPerPolicy` ไว้ที่ 4096 หรือลดลง** เมื่อการขยายไม่จำเป็นอีกต่อไป?
3. **ยอมรับ behavior change เรื่อง auto-merge ของ interval set หรือไม่** (subnet ที่ซ้อนกัน
   จะถูกยุบรวมโดย kernel ทำให้ `nft list ruleset` แสดงไม่ตรงกับที่ผู้ใช้กรอก)
4. ต้องการ backward-compat flag (config key เปิด/ปิดโหมด set) เพื่อ rollback เร็วหรือไม่?

---

## 9. เอกสาร/ไฟล์อ้างอิง

- `docs/tech_stack_design.md` section 4.3 — โครงสร้างกฎ 4 ส่วน (ห้ามเปลี่ยนลำดับ)
- `docs/ref/complete/multi-value-address-service-objects-plan.md` — ที่มาของ cartesian expansion
- `docs/ref/todo/multi-interface-firewall-rule-plan.md` — ที่มาของ in × out expansion (D-1 Option A)
- `docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md` — สัญญาของ FQDN snapshot (D-1/D-2)
- `backend/internal/kernel/real_firewall.go` — โค้ดหลักที่จะถูกรื้อ
- `backend/internal/kernel/real_traffic_account.go` — ผู้บริโภคของ `UserData` per-rule counter
- Phase 1: branch `fix/nft-netlink-enobufs` (netlink socket buffer + `max-total-nft-rules`)
