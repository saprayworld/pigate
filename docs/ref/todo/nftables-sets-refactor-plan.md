# nftables Sets Refactor (Phase 1 + Phase 2) — Work Plan

> แผนงานสำหรับ issue #168: เปลี่ยนการขยายกฎ firewall แบบ cartesian (1 policy → หลักพัน nft rules)
> เป็น **anonymous nftables sets** + ทำ Phase 1 (ขยาย netlink socket buffer + งบประมาณ rule ทั้ง ruleset)
> ใน PR เดียวกัน พร้อม config key สำหรับ rollback กลับ path เดิม
>
> เขียน/ปรับปรุง: 2026-10-01 · Reference branch: `refactor/nftables-sets` (checkout แล้ว)
> สถานะ: **implement ครบ T-01..T-11 + ผ่าน QA บน WSL แล้ว — รอเจ้าของทดสอบบน Raspberry Pi 5 จริง** (ดู PR)
> งานนี้แตะ firewall rule generation + Netlink โดยตรง = **sensitive ทุก task ใน kernel layer ต้อง review เข้ม**

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
1. 1 `model.PolicyRule` → nft rule ไม่เกิน **2 ข้อต่อ chain** (1 ข้อในกรณีทั่วไป; 2 ข้อเมื่อ service ผสม
   "proto อย่างเดียว" กับ "proto+port") โดยเงื่อนไขหลายค่ายุบเป็น anonymous set + `expr.Lookup`
2. **semantics การ match เท่าเดิมทุกแพ็กเก็ต** (รวมพฤติกรรมบังเอิญกับ IPv6 ใน inet table — ดูข้อ 3.3)
   ต่างกันได้เฉพาะการแสดงผลของ `nft list ruleset`
3. ค่าเดียว (singleton) ต้องได้ expr **byte-identical** กับวันนี้ — กฎส่วนใหญ่และเทสต์เดิมไม่เปลี่ยน
4. Phase 1: socket buffer ของ nftables conn ใหญ่พอ + งบประมาณ `max-total-nft-rules` (reject ก่อน `Flush`)
5. `nft-use-sets=false` = path cartesian เดิมทุกไบต์ (rollback ได้ทันทีโดยไม่ต้อง build ใหม่)

**นอกขอบเขต**
- Named sets (ตั้งชื่อตาม address object) — อาจเป็น Phase 3 ในอนาคต
- IPv6 policy matching (วันนี้รองรับแค่ IPv4 — ห้ามเปลี่ยนพฤติกรรม IPv6 ในงานนี้)
- Frontend: ตัวประมาณ `ruleExpansionEstimate` ใน `frontend/src/components/policy/PolicyChainPage.tsx:~861`
  ยังคำนวณแบบ cartesian — ไม่แก้ในงานนี้ (บันทึกเป็น follow-up ข้อ 8 Open-3)
- ไม่เปลี่ยน API contract (`docs/openapi.yaml` ไม่กระทบ), ไม่มี migration DB, ไม่แตะ `mock.go`
  (ไม่มี method ใหม่ใน `FirewallManager` — เป็น setter บน `*RealFirewall` แบบเดียวกับ `SetMaxExpandedRulesPerPolicy`)
- ทดสอบบน Pi 5 จริง = เจ้าของทำเอง

---

## 1. สภาพปัจจุบัน (สำรวจโค้ดจริง 2026-10-01)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| cartesian src×dst×svc×combos | มีอยู่ | `backend/internal/kernel/real_firewall.go:~1439-1574` `addUserChainRules` (6 ชั้น loop) |
| in×out interface cartesian | มีอยู่ | `real_firewall.go:~1760-1819` ใน `buildRuleExpressions`; `normalizeIfaceMatchList` `:~1584` |
| address → combos (+FQDN resolve/record) | มีอยู่ | `addressCombos` `:~1272`, `ipMatchExprsForCombo` `:~1208`, `buildIPMatchExpressions` `:~1111` |
| service → combos (TCP/UDP แตก 2) | มีอยู่ | `serviceCombos` `:~1350`; ตรรกะ port อยู่ใน `buildRuleExpressions` `:~1667-1746` |
| per-policy cap | มีอยู่ (default 4096, truncate+log, ไม่ fail apply) | field `:~196`, setter `:~235`, `config.go:~291/366/758/959/1075`, `main.go:~227` |
| **Phase 1: `max-total-nft-rules`** | **ไม่มี** (draft เดิมเขียนผิดว่าทำแล้ว) | grep ทั้ง repo ไม่พบ; ไม่มี branch `fix/nft-netlink-enobufs` |
| **Phase 1: `SO_RCVBUFFORCE` / `WithSockOptions`** | **ไม่มี** | `ApplyRules` ใช้ `nftables.New()` เปล่า `:~255` |
| single `conn.Flush()` | มีอยู่ | `:~860` (ห้ามแยก) |
| per-rule counter (UserData) | มีอยู่ | `:~1470`; ผู้ใช้ `real_traffic_account.go:~184` `accumulateRuleCounters` (บวกรวมตาม id) |
| log token / fqdnRecorder | มีอยู่ | `withRuleToken :~171`, `fqdnRecorder :~123`, snapshot หลัง Flush สำเร็จ `:~867` |
| boot apply ล้ม | แค่ warning + event log | `cmd/pigate/main.go:~826` (ไม่มี pigate table ⇒ ไม่มีการกรองเลย — ดู Caution 9) |
| `-mock-from-real` | ใช้ MockFirewall เหมือน `-mock` | `main.go:~157` ⇒ key ใหม่ไม่มีผลใน mock |
| validation input | ports ผ่าน `model.ParsePortSpec` (1..65535, s≤e); interface ≤8/ทิศ, dedupe, ALL collapse | `model/object_entry_validate.go:~109`, `model/types.go:~368/1213`, `model/policy_rule_validate.go` |
| เทสต์เดิม | `policy_chain_test.go` ใช้ `nftables.WithTestDial` นับ `NFT_MSG_NEWRULE` (`:~548`) | ต้องยังผ่าน |

**API `google/nftables@v0.3.0` ที่ยืนยันจากซอร์ส** (`$(go env GOMODCACHE)/github.com/google/nftables@v0.3.0`):
- `Set{Anonymous, Constant, Interval, Concatenation, KeyType, KeyByteOrder}` `set.go:245`; `AddSet` `set.go:493`
  — anonymous ต้อง constant (`:500`), ถ้า `ID==0` จะจอง `allocSetID` (global) และตั้งชื่อ `"__set%d"` (`:504-512`)
  ⇒ **`expr.Lookup` ต้องสร้างหลัง `AddSet` คืนค่า** (ใช้ `set.Name`/`set.ID` ที่ถูกเติมแล้ว)
- `SetElement{Key, KeyEnd, IntervalEnd}` `set.go:275`; `KeyEnd` → `NFTA_SET_ELEM_KEY_END` (`:413`)
- `Concatenation: true` → ส่ง `NFTA_SET_DESC_CONCAT` (field len 1,2) (`:586-614`) — จำเป็นให้ kernel เลือก pipapo
- `MustConcatSetType(TypeInetProto, TypeInetService)` → Bytes = 4+4 = 8 (`:210-230`)
- `WithSockOptions(opts ...SockOption)` `conn.go:111`, `type SockOption func(*netlink.Conn) error` `conn.go:53`
  — ถูกเรียกทุกครั้งที่ dial (ทุก `Flush`) และ **ถ้าคืน error ⇒ `Flush` ล้มทั้ง apply** (`conn.go:315-319`)
- `mdlayher/netlink@v1.11.2` `Conn.SyscallConn()` `conn.go:578`, `SetReadBuffer/SetWriteBuffer` `:503/514`
- `Flush` ส่งทั้ง batch ใน `sendmsg` เดียว แล้วอ่าน ack **ทีละ message** (`conn.go:262-276`) ⇒ ack ทุกข้อความต้องพอดี rcvbuf

สรุป: งานจริงกระจุกที่ `real_firewall.go` (+ไฟล์ใหม่ในแพ็กเกจ kernel), `config.go`, `main.go` และเอกสาร

---

## 2. ปัญหาที่ต้นตอ (ย่อ)

cartesian ทำให้จำนวน netlink message โตแบบคูณ; kernel ประมวลผล batch แล้ว queue ack 1 ตัว/ข้อความ
ลง rcvbuf (default ~208 KB, truesize ~0.5-1 KB/ack) ⇒ ENOBUFS ตั้งแต่หลักร้อย-พันกฎ, sndbuf เล็กกว่า batch
⇒ EMSGSIZE, match แบบ linear ช้า, และผู้ใช้มองไม่เห็นว่า "3 policy = 8,000 rules"

---

## 3. การออกแบบทางเทคนิค

### 3.1 Phase 1a — socket buffer (ไฟล์ใหม่ `backend/internal/kernel/real_firewall_batch.go`)

- `const nftSockBufBytes = 32 << 20` (kernel คูณ 2 ให้เอง ⇒ accounting ~64 MiB; ใช้จริงเฉพาะตอนมี ack ค้าง)
- `func nftSocketBufferOption(c *netlink.Conn) error` — **best-effort, คืน `nil` เสมอ**:
  1. `c.SyscallConn()` → `rc.Control(fd → unix.SetsockoptInt(fd, SOL_SOCKET, SO_RCVBUFFORCE, n)` แล้ว `SO_SNDBUFFORCE)`
     (ต้อง `CAP_NET_ADMIN` ใน init userns — binary มี)
  2. ถ้าข้อ 1 ล้ม (EPERM ใน `unshare -rn`, หรือ nltest ไม่รองรับ) → `c.SetReadBuffer(n)` / `c.SetWriteBuffer(n)`
     (ถูก cap ด้วย `net.core.rmem_max/wmem_max`)
  3. ล้มอีก → log ครั้งเดียว (`sync.Once`) แล้วคืน `nil` — **ห้ามทำให้ dial ล้ม**
- `var newFirewallConn = func() (*nftables.Conn, error) { return nftables.New(nftables.WithSockOptions(nftSocketBufferOption)) }`
  (package var ให้ unit test แทนด้วย `WithTestDial` ได้) — `ApplyRules :~255` เปลี่ยนมาเรียกตัวนี้
- ไม่ใช้ `exec.Command`/sysctl; ไม่แก้ `install.sh`

### 3.2 Phase 1b — งบประมาณทั้ง ruleset `max-total-nft-rules`

- ในไฟล์เดียวกัน: `type nftBatch interface { AddRule(*nftables.Rule) *nftables.Rule; AddSet(*nftables.Set, []nftables.SetElement) error }`
  (`*nftables.Conn` satisfy อยู่แล้ว) + `type countingBatch struct{ inner nftBatch; rules, sets, elems int }`
- `ApplyRules`: `cb := &countingBatch{inner: conn}` แล้ว **ทุก** `conn.AddRule(` ในตัว `ApplyRules` เปลี่ยนเป็น `cb.AddRule(`
  (`AddTable/AddChain/FlushTable` ยังเรียก `conn`) — helper `addAdminAccessRules`, `addDNSServerAccessRules`,
  `addUserChainRules`, `addUserChainRulesSets` เปลี่ยน param `conn *nftables.Conn` → `b nftBatch`
- **นับอะไร**: ทุก `AddRule` ใน batch = โครงสร้าง (not-local/input/forward/output) + user rules 3 chain +
  port-forward accept + `pigate_nat` postrouting/prerouting DNAT. set ไม่นับเป็น rule (นับแยกไว้ log)
- **จุดตรวจ**: หลัง `AddRule/AddSet` ครบทั้งหมด **ก่อน** `conn.Flush()` `:~860` บรรทัดเดียว:
  `if cb.rules > rf.maxTotalNftRules { log; return fmt.Errorf("%w: ...", ErrNftRuleBudgetExceeded) }`
  — ไม่เรียก `Flush` ⇒ ไม่มีข้อความใดถึง kernel (conn แบบ non-lasting ส่งเฉพาะตอน Flush) ⇒ ruleset เดิมคงอยู่;
  `rf.fqdnData` ไม่ถูกแทน (อยู่หลัง Flush อยู่แล้ว). ห้ามแยก Flush
- log สรุปทุก apply สำเร็จ: `mode=sets|legacy rules=N sets=S elements=E`

### 3.3 Phase 2 — โมเดล semantics ของ set mode

ข้อเท็จจริง: cartesian (src names × dst names × svc names × entries × in × out) ≡
`iif∈I ∧ oif∈O ∧ src∈∪S ∧ dst∈∪D ∧ svc∈∪V` (verdict/log/nat เดียวกันทุก combo) ดังนั้นแต่ละ "มิติ" ยุบเป็น 1 ชุดค่า:

- **atom** = หน่วย match 1 ตัว (address: 1 combo จาก `addressCombos` ที่ parse ได้; service: 1 combo จาก `serviceCombos`)
- `"ALL"`/`""` ที่ใดก็ได้ใน list ของมิติ address/service ⇒ มิตินั้น "ไม่มีเงื่อนไข" (ตรงกับ combo `hasFilter=false` วันนี้)
- interface ใช้ `normalizeIfaceMatchList` เดิม (ALL ถูก collapse โดย `model.NormalizePolicyRuleInterfaces` แล้ว)
- ชื่อ object ไม่รู้จัก → skip เฉพาะชื่อนั้น + log ข้อความเดิม; entry parse ไม่ได้ → skip เฉพาะ atom + log
- มิติใด **ไม่มี atom และไม่มี ALL** ⇒ **ไม่ emit rule ของ policy นั้นใน chain นั้น** (วันนี้ = 0 combos = 0 rules)
  ห้าม emit set ว่าง ห้าม degrade เป็น "ไม่มีเงื่อนไข" (fail closed)
- dedupe atom ที่เหมือนกันทุกประการ (key = `type|value|resolvedIP` / `proto|port`)
- **singleton**: มิติที่มี atom หลัง dedupe = 1 ⇒ ใช้ expr เดิม (`ipMatchExprsForCombo`, service expr เดิม,
  `Meta+Cmp(padInterfaceName)`) byte-identical; ≥2 ⇒ set lookup (ตัดสินก่อน merge)
- range ที่ start>end (วันนี้ = Gte/Lte ที่ไม่ match อะไร) ⇒ ตัดทิ้งก่อนนับ singleton (ผล match เท่ากัน: ไม่มีแพ็กเก็ตใด match)
- **คง payload load เดิมทุกจุด**: IP = network header offset 12/16 len 4; proto = network header offset 9 len 1
  (**ห้ามเปลี่ยนเป็น `meta l4proto`** — จะเปลี่ยนพฤติกรรมกับ IPv6 ใน inet table ที่วันนี้ fail-closed โดยบังเอิญ,
  ดู comment `real_firewall.go:~772`); dport = transport header offset 2 len 2
- **service split** (ต้องแยกเพราะ port load บน transport header ให้ผลต่างกับ fragment/ICMP):
  - `anyPort` atom = ICMP ทุกกรณี, หรือ TCP/UDP ที่ `strings.TrimSpace(port)` เป็น `""`, `"-"`, `"1-65535"` (ตรงตัวอักษร)
    หรือ split `"-"` ได้ ≥3 ส่วน (quirk เดิม `:~1695-1744` fall-through = proto อย่างเดียว — **mirror ไม่ normalize**)
  - port atom = 1 ส่วน → `strconv.Atoi(parts[0])`, 2 ส่วน → `Atoi(TrimSpace)` ทั้งคู่ (เช่น `"1 - 65535"` = port atom [1,65535] ไม่ใช่ anyPort)
  - ค่านอก 0..65535 (DB เสีย) → skip atom + log (วันนี้ byte-truncate; เป็นความต่างเฉพาะข้อมูลที่ validation ไม่ยอมให้เข้า DB อยู่แล้ว)
  - กลุ่ม P = proto ของ anyPort atoms; กลุ่ม Q = port atoms ที่ proto ∉ P (ถ้า proto อยู่ใน P แล้ว port atom ซ้ำซ้อน — union เท่าเดิม), merge ต่อ proto
  - variant A (ถ้า P≠∅): `|P|=1` → `Payload(nh,9,1,reg1)+Cmp(Eq,[p])`; `|P|≥2` → `Payload(nh,9,1,reg1)+Lookup(proto set)`
  - variant B (ถ้า Q≠∅): Q มี 1 element หลัง merge → `Payload proto + Cmp` + `dportMatchExprs` รูปแบบเดิม (Eq หรือ Gte/Lte);
    ≥2 → `Payload(nh,9,1,DestRegister:1)` + `Payload(th,2,2,DestRegister:9)` + `Lookup{SourceRegister:1}` บน concat set
  - service singleton (atom รวม =1) ⇒ ใช้ expr เดิมทั้งก้อน ไม่ผ่าน P/Q
- FQDN: เรียก `addressCombos` สำหรับทุกชื่อในทั้ง 3 list ของทุก rule ที่ enabled (resolve 1 ครั้ง/ชื่อ/มิติ/rule แทนที่
  วันนี้ resolve dest ซ้ำต่อ src name) ⇒ key ใน snapshot เป็น **superset** ของวันนี้ (วันนี้ถ้า src ไม่รู้จักทั้งหมด dest ไม่ถูก resolve)
  — ยอมรับได้: refresher แค่ monitor เพิ่ม ไม่เกิด loop (`service/fqdn_refresh.go:~111-149`)

### 3.4 Netlink encoding ต่อชนิด set (ทุกตัว `Anonymous:true, Constant:true, Table: pigate(inet)`)

| set | flags/KeyType | element | lookup |
|---|---|---|---|
| IPv4 (src/dst) | `Interval:true`, `KeyType:TypeIPAddr` (4B), `KeyByteOrder:BigEndian` → rbtree | จาก interval ที่ merge แล้วเรียง: ถ้า `iv[0].start≠0` ใส่ `{Key:0.0.0.0, IntervalEnd:true}` นำหน้า (แบบที่ `nft` CLI ส่ง: `element 00000000 : 1 [end]`); ทุก iv: `{Key:be32(start)}` และถ้า `end≠0xFFFFFFFF` ตามด้วย `{Key:be32(end+1), IntervalEnd:true}` (end ของ space ไม่มี end element) | `Payload(nh,off,4,reg1)` + `Lookup{SourceRegister:1, SetName, SetID}` |
| proto . dport | `Interval:true, Concatenation:true`, `KeyType: MustConcatSetType(TypeInetProto, TypeInetService)` (8B) → pipapo | 1 element/interval: `Key=[p,0,0,0, sHi,sLo,0,0]`, `KeyEnd=[p,0,0,0, eHi,eLo,0,0]` (ส่ง KeyEnd เสมอแม้ s=e; ไม่มี IntervalEnd element) | `Payload(nh,9,1,reg1)` + `Payload(th,2,2,reg9)` + `Lookup{SourceRegister:1}` (reg1=NFT_REG32_00, reg9=NFT_REG32_01 = 4 ไบต์ถัดไป; payload len<4 ถูก zero-pad ใน register) |
| proto only | `KeyType:TypeInetProto` (1B), ไม่ interval → hash | `{Key:[]byte{p}}` | `Payload(nh,9,1,reg1)` + `Lookup` |
| iifname/oifname | `KeyType:TypeIFName` (16B), ไม่ interval → hash | `{Key:padInterfaceName(n)}` | `Meta{IIFNAME|OIFNAME, reg1}` + `Lookup` |

กฎที่ kernel บังคับ/ต้องระวัง (เราทำให้ครบใน pure Go ก่อน `AddSet`):
- **interval ห้ามซ้อน** (rbtree/pipapo คืน EEXIST/ENOTEMPTY; `nft` CLI merge ใน userspace แต่ google/nftables ไม่ทำ)
  ⇒ `mergeIntervals`: sort ตาม start, รวมเมื่อ `next.start ≤ cur.end+1` (รวม **adjacent** ด้วย เพื่อไม่ให้ end-element
  กับ start-element key ชนกัน), คำนวณด้วย `uint64` กัน overflow ที่ `0xFFFFFFFF`/65535
- element ซ้ำใน hash set ⇒ dedupe ก่อนเสมอ
- **anonymous set bind ได้กับ lookup เดียว** (kernel `nf_tables_bind_set` → EBUSY) ⇒ ทุก nft rule สร้าง set ของตัวเองใหม่
  ทุกครั้ง (variant A/B ห้ามแชร์ src/dst/iif set; src กับ dst ห้ามใช้ set เดียวกันแม้ค่าตรงกัน)
- ลำดับ message: `AddSet` (NEWSET+NEWSETELEM) ต้องมาก่อน `AddRule` ที่อ้างถึง; lookup ใช้ชื่อ `"__set%d"` ซึ่ง kernel
  หาไม่เจอด้วยชื่อแล้ว fallback เป็น `SET_ID` (พฤติกรรมเดียวกับ nft)
- `FlushTable` (DELRULE ทั้ง table `:~285`) ปลด binding ⇒ anonymous set เก่าถูกทำลายเองใน batch เดียวกัน (ต้องพิสูจน์ใน T-09 ว่าไม่ leak)
- google/nftables ส่ง `NFTA_SET_DESC` 2 ครั้งสำหรับ constant+concat (size แล้ว concat) — kernel ใช้ตัวหลัง; และ
  `NFTA_SET_ELEM_FLAGS` มี `NLA_F_NESTED` — **ยังไม่ยืนยันกับ kernel จริง** ⇒ T-09 ต้องพิสูจน์
  (fallback ถ้า concat ถูกปฏิเสธ: Cmp proto + rbtree `TypeInetService` interval set ต่อ proto — แจ้ง tech lead ก่อนเปลี่ยน)

### 3.5 การประกอบ rule (ลำดับ expr เหมือน legacy ทุกประการ)

`[iif] [oif] [src] [dst] [svc-variant] [counter] [log?] [fwmark ถ้า forward&&nat&&ACCEPT] [verdict]`
— chain scoping เดิม (input ล้าง out, output ล้าง in), `UserData = userdata.AppendString(nil, TypeComment, r.ID)`
ทุก rule, log prefix = `withRuleToken(prefix, r.ID)` คำนวณครั้งเดียว/policy. จำนวน rule ต่อ policy/chain:
service ALL หรือ singleton ⇒ 1; มิฉะนั้น `(P≠∅)+(Q≠∅)` ∈ {1,2}

### 3.6 เพดานใน set mode (ตัดสินแล้ว — D-6)

- `maxExpandedRulesPerPolicy` ยังนับ **nft rule ที่ emit ต่อ policy** (≤2 โดยโครงสร้าง ⇒ แทบไม่ถูกแตะ) และ
- **เพิ่ม defense-in-depth**: จำนวน atom (หลัง dedupe, ก่อน merge) ของมิติใดมิติหนึ่ง > `maxExpandedRulesPerPolicy`
  ⇒ **skip ทั้ง policy ใน chain นั้น** + log warning ระบุชื่อ key (ไม่ truncate set — set ที่ถูกตัดครึ่งคือ semantics
  ที่ผู้ใช้มองไม่เห็น) — ไม่คืน error, ไม่ทำให้ `ApplyRules` ล้ม (สัญญาเดิมของ cap นี้)
- `maxTotalNftRules` ครอบทั้ง ruleset ทั้งสองโหมด (ข้อ 3.2)

### 3.7 Rollback flag และ config (mirror `max-expanded-rules-per-policy` ทุกจุด = file-only key)

| key | Config field | default | ช่วง / validation | setter |
|---|---|---|---|---|
| `nft-use-sets` | `NFTUseSets bool` | `true` | `strconv.ParseBool` ผิด = fail-fast error (เหมือน `ipinfo-enabled`) | `(*RealFirewall).SetUseNFTSets(bool)` |
| `max-total-nft-rules` | `MaxTotalNFTRules int` | `16384` | 1024..65536 clamp+warn (ไม่ใช่ตัวเลข = error) | `(*RealFirewall).SetMaxTotalNFTRules(n)` (n≤0 ignore) |

`orderedKeys` ต่อท้ายหลัง `keyDNSStatsMaxBlockedDomains` (`config.go:~585`); `NewRealFirewall` default ต้องตรง
`config.Defaults()` (`useSets:true, maxTotalNftRules:16384`). `install.sh` **ไม่ต้องแก้** (seed แค่ 4 key `:~349`, ที่เหลือใช้ default)

### 3.8 ฟังก์ชันที่เพิ่ม/เปลี่ยน

- `real_firewall_batch.go` (ใหม่): `nftBatch`, `countingBatch`, `nftSocketBufferOption`, `newFirewallConn`,
  `ErrNftRuleBudgetExceeded`, `checkRuleBudget(count, max int) error`
- `real_firewall_sets.go` (ใหม่, pure ส่วนใหญ่): `type interval struct{start,end uint32}`, `mergeIntervals`,
  `addrComboInterval(c addrCombo) (iv interval, nonEmpty bool, err error)` (mirror parsing ของ `buildIPMatchExpressions`),
  `svcComboAtom(vc svcCombo) (svcAtom, nonEmpty bool, err error)` (mirror `buildRuleExpressions :~1667-1746`),
  `encodeIPv4IntervalElems`, `encodeProtoPortElems`, `encodeIfnameElems`, `encodeProtoElems`,
  `newAnonIPv4Set/newAnonProtoPortSet/newAnonProtoSet/newAnonIfnameSet(table)`, `collectAddrDim`, `collectSvcDim`,
  `buildSetModeRules` (คืน list ของ rule spec: exprs-builder + sets ที่ต้อง AddSet), `addUserChainRulesSets(...) int`
- `real_firewall.go`: แยก helper แบบ **mechanical move ห้ามเปลี่ยน logic** จาก `buildRuleExpressions`:
  `svcComboMatchExprs(vc svcCombo) ([]expr.Any, error)` (บล็อก "5. Service / Protocol") และ
  `userRuleSuffixExprs(chain, action string, logEnabled, nat bool, logPrefix string) []expr.Any`;
  `addUserChainRules` คืน `int` (จำนวนที่ emit); `ApplyRules` dispatch `if rf.useSets {…Sets} else {legacy}` ทั้ง 3 จุด
  (`:~608/717/789`); fields + setters ใหม่ใน `RealFirewall`

---

## 4. Scope ไฟล์

`backend/internal/kernel/{real_firewall.go, real_firewall_batch.go(ใหม่), real_firewall_sets.go(ใหม่),
real_firewall_sets_test.go(ใหม่), real_firewall_golden_test.go(ใหม่)+testdata/, real_firewall_netns_test.go(ใหม่), policy_chain_test.go}`,
`backend/internal/config/{config.go, config_test.go}`, `backend/cmd/pigate/main.go`, `pigate.conf.example`, `README.md`,
`CLAUDE.md`, `docs/data/firewall.md`, `docs/tech_stack_design.md` §4.3 — ไม่แตะ: `mock.go`, `interfaces.go`, service, api, db, frontend, openapi

---

## 5. Cautions

1. **โครงสร้าง 4 ส่วนของ `input` + Admin Access ก่อน user rules ห้ามเปลี่ยน** — dispatch แทนที่เฉพาะบรรทัดเรียก
   `addUserChainRules` เดิม ณ ตำแหน่งเดิม ถ้าย้าย ⇒ user DROP อาจ shadow Admin Access ⇒ ล็อกตัวเองออก
2. **Single `Flush()` ห้ามแยก** — anonymous set ใช้ได้เฉพาะใน batch เดียว และ `pigate_nat` ต้องอยู่ pass เดียวกัน (`:~825`)
3. **SockOption คืน error = firewall ไม่ถูก apply เลย** (`conn.go:315`) ⇒ `nftSocketBufferOption` ต้องคืน `nil` เสมอ + มีเทสต์ด้วย nltest conn
4. **anonymous set single-bind** — แชร์ set ข้าม 2 rules ⇒ kernel EBUSY ⇒ ทั้ง apply ล้ม ⇒ สร้าง set ใหม่ต่อ rule เสมอ
5. **interval ซ้อน/ติดกัน** ⇒ EEXIST/ENOTEMPTY ทั้ง batch ⇒ merge + dedupe ใน Go ก่อน `AddSet` เสมอ (รวม per-proto ใน concat)
6. **อย่า normalize ค่าที่ legacy ไม่ normalize** (`"1 - 65535"`, ≥3 ส่วน, subnet ไม่มี `/`) — ความต่างเล็กน้อยใน
   การจัดกลุ่ม anyPort vs port ⇒ fragment/ICMP match ต่างกัน ⇒ ต้อง mirror ทีละ branch + table test
7. **ห้ามเปลี่ยน proto load เป็น `meta l4proto`** และห้ามเพิ่ม `meta nfproto ipv4` — เปลี่ยนพฤติกรรม IPv6 (ข้อ 3.3)
8. **fail closed**: มิติว่าง ⇒ ไม่มี rule; ห้าม emit set ว่าง (หรือกฎไม่มีเงื่อนไข) — DROP ว่างไม่เป็นไร แต่ ACCEPT ไม่มีเงื่อนไข = เปิดหมด
9. **Budget reject ตอน boot** — `InitApplyConfig` ล้มแค่ warning (`main.go:~826`) และ kernel ยังไม่มี pigate table
   ⇒ บอร์ดไม่มีการกรองจนกว่าจะ apply สำเร็จ (เหมือน apply ล้มเหตุอื่นวันนี้) — ในโหมด sets แทบเป็นไปไม่ได้ (ต้อง >16k rules);
   ความเสี่ยงอยู่ที่ `nft-use-sets=false` เท่านั้น (ข้อ 8 Open-1)
10. **ENOBUFS ≠ rollback แน่นอน** — kernel commit batch ก่อน queue ack; ack หล่นเพราะ rcvbuf เต็ม ⇒ `Flush` อาจคืน error
    ทั้งที่ ruleset ใหม่ถูก commit แล้ว (ยังไม่ยืนยัน) ⇒ อย่าเขียน logic ที่เชื่อว่า error = ruleset เดิม; buffer ใหญ่ + sets ลดโอกาส
11. **`allocSetID` เป็น global ไม่มี atomic** — ใช้ได้เพราะ `FirewallService.applyMu` serialize การ apply; เทสต์ที่เรียก `AddSet`
    ห้าม `t.Parallel()` (race detector)
12. **Integration test ห้ามรันนอก netns** — มันแทน table จริงได้ ⇒ gate ด้วย env + ตรวจว่า `net.Interfaces()` มีแค่ `lo` ไม่งั้น `t.Fatal`
13. **config key ใหม่ถูก pin เมื่อ binary auto-generate pigate.conf** (`config.Write`) — ค่า default ในโค้ดเปลี่ยนภายหลังจะไม่มีผลกับไฟล์ที่เขียนแล้ว (README มีตัวอย่างเดิม)
14. **mock mode 100% ไม่กระทบ** — key ใหม่ใช้แค่ `*RealFirewall`; ห้ามเพิ่ม method ใน `FirewallManager`
15. ห้าม `exec.Command`/`nft` CLI ในทุกไฟล์รวมเทสต์; ห้าม dependency ใหม่ (ใช้ `mdlayher/netlink`, `x/sys/unix`, `vishvananda/netlink` ที่มีอยู่)
16. golden test (T-01) ต้องสร้างจากโค้ด **ก่อนแก้** — ถ้าสร้างหลังแก้ มันจะยืนยันโค้ดใหม่แทนโค้ดเก่า
17. **`nft list ruleset` อาจโชว์ anonymous `oifname`/`iifname` set (2+ interfaces) เป็น `{ "", "" }`** —
    cosmetic bug ของ `google/nftables` v0.3.0 (ยังอยู่ใน upstream `main` ปัจจุบัน), ไม่ใช่บั๊กโค้ดนี้: `AddSet`
    (`set.go:~624`) ใส่ userdata `NFTNL_UDATA_SET_KEYBYTEORDER=2` (big-endian) ให้ทุก set ที่ `Anonymous||Constant`
    เสมอ ไม่สนว่า key type จริงคืออะไร แต่ datatype `string` ที่ `iifname`/`oifname` ใช้ ประกาศไว้ใน nftables เองว่า
    `byteorder = BYTEORDER_HOST_ENDIAN` (`src/datatype.c`) — พอ `nft` เจอ hint ไม่ตรงกับที่ datatype บอก มันจะกลับไบต์
    ก่อนพิมพ์ ชื่อ interface ที่ pad เป็น 16 ไบต์ (`padInterfaceName`) เลยโชว์เป็น string ว่าง ไบต์จริงที่ส่งผ่าน
    netlink ไปให้ kernel ใช้ match (`makeElemList`) ไม่ได้ถูกกลับ ⇒ **match ใน kernel ถูกต้องปกติ มีผลแค่การแสดงผล**
    (เคสเดียวกับ [google/nftables#225](https://github.com/google/nftables/issues/225) ที่เคยรายงานกับ `ipv4_addr`)
    ยืนยันด้วยการดู packet/byte counter ของ rule นั้นเพิ่มขึ้นจริงเมื่อมี traffic ผ่าน ถ้าอยากได้ `nft list` ที่อ่านง่าย
    ให้ตั้ง `nft-use-sets=false` (ข้อ 3.7) กลับไปใช้ legacy cartesian builder ซึ่งไม่ผ่าน anonymous set เลย

---

## 6. Tasks (ai-developer ทำตามลำดับ; ไม่ทดสอบรวมทีละ task — QA รอบเดียวท้ายแผน)

```json
[
{"task_id":"T-01","title":"Golden snapshot ของ legacy path (ก่อนแก้โค้ดใดๆ)","layer":"kernel",
 "files":["backend/internal/kernel/real_firewall_golden_test.go","backend/internal/kernel/testdata/legacy_user_rules.golden"],
 "instruction":"Test-only. Build a fixed matrix of policies (singleton subnet/range//32/fqdn via stubbed lookupIP; multi-entry objects; TCP/UDP; ICMP; port ranges; ports '', '-', '1-65535', '1 - 65535'; multi in/out interfaces; ALL; chains forward/input/output; log on/off; nat on/off; ACCEPT/DROP). Run CURRENT addUserChainRules through nftables.New(WithTestDial) capturing msg.Data of every NFT_MSG_NEWRULE, hex-encode per policy into the golden file. Add a -update flag to regenerate. Generate it NOW, before T-03..T-07 touch real_firewall.go.",
 "acceptance":["golden file exists under testdata/ (dev does not git commit), test passes on unmodified code","deterministic across 3 runs"],"depends_on":[]},
{"task_id":"T-02","title":"config: nft-use-sets + max-total-nft-rules","layer":"config",
 "files":["backend/internal/config/config.go","backend/internal/config/config_test.go"],
 "instruction":"Add file-only keys per plan 3.7, mirroring max-expanded-rules-per-policy exactly (Config field+doc, Defaults, key const, orderedKeys appended after keyDNSStatsMaxBlockedDomains, applyKey, keyValue, Resolve range pass with min 1024/max 65536 clamp+warn). No CLI flag.",
 "acceptance":["Defaults: NFTUseSets=true, MaxTotalNFTRules=16384","file 'nft-use-sets=false' -> false; 'nft-use-sets=maybe' -> Resolve error","max-total-nft-rules 100 / 70000 -> default + 1 warning; 1024 and 65536 accepted; 'abc' -> error","Write->Parse->Resolve round-trip equal; KnownKeys contains both","existing config tests green"],"depends_on":[]},
{"task_id":"T-03","title":"Phase 1: batch wrapper, socket buffers, ruleset budget, setters","layer":"kernel","sensitive":true,
 "files":["backend/internal/kernel/real_firewall_batch.go","backend/internal/kernel/real_firewall.go"],
 "instruction":"Implement plan 3.1/3.2: nftBatch, countingBatch, nftSocketBufferOption (always returns nil), newFirewallConn package var used by ApplyRules, ErrNftRuleBudgetExceeded + checkRuleBudget, budget check immediately before the single conn.Flush(). Replace every conn.AddRule inside ApplyRules with cb.AddRule; change addAdminAccessRules/addDNSServerAccessRules/addUserChainRules param to nftBatch; addUserChainRules returns emitted count (no behavior change). Add RealFirewall fields useSets(default true)/maxTotalNftRules(16384) + SetUseNFTSets/SetMaxTotalNFTRules. Success summary log line.",
 "acceptance":["go build ./... ; T-01 golden still passes byte-for-byte","exactly one conn.Flush() in real_firewall.go","unit: nftSocketBufferOption on an nltest conn returns nil","unit (newFirewallConn swapped to WithTestDial): ApplyRules with SetMaxTotalNFTRules(1024) and >1024 generated rules returns errors.Is(ErrNftRuleBudgetExceeded) and TestDial func is never invoked; under budget -> Flush invoked once","countingBatch counts rules across input/forward/output/not-local/nat"],"depends_on":["T-01"]},
{"task_id":"T-04","title":"Pure set helpers: intervals, atoms, encoders","layer":"kernel","sensitive":true,
 "files":["backend/internal/kernel/real_firewall_sets.go","backend/internal/kernel/real_firewall_sets_test.go"],
 "instruction":"Implement plan 3.3/3.4/3.8 pure parts: interval, mergeIntervals (uint64 math, merge overlap+adjacent), addrComboInterval (mirror buildIPMatchExpressions parsing incl. missing '/' => /32, host bits masked, IPv6 => error, range TrimSpace, start>end => nonEmpty=false), svcComboAtom (mirror buildRuleExpressions branch by branch incl. quirks; out-of-range => error), encodeIPv4IntervalElems (leading 0.0.0.0 end element iff first start!=0; no end element when end==0xFFFFFFFF), encodeProtoPortElems (Key/KeyEnd 8 bytes padded), encodeIfnameElems, encodeProtoElems, newAnon*Set constructors (Anonymous+Constant always; Interval only IPv4/concat; Concatenation only concat). No netlink.",
 "acceptance":["merge: overlap, containment, adjacency (x..y,y+1..z => one), duplicates, unsorted, [0,max], end at 255.255.255.255, ports 65535","encode 10.0.0.0/8 => [0.0.0.0 end, 10.0.0.0, 11.0.0.0 end]; range starting 0.0.0.0 => no leading elem; 0.0.0.0/0 => single {0.0.0.0}; /32 x => {x},{x+1 end}","concat TCP 80-90 => Key 06000000 00500000 KeyEnd 06000000 005a0000","svcComboAtom table: ''/'-'/'1-65535' anyPort; ' 80 '=>80; '1 - 65535' => port range; '80-90-100' anyPort; 'abc' err; '70000' err; ICMP+port anyPort; 'SCTP' err; '90-80' nonEmpty=false","addrComboInterval table mirrors legacy for every case in T-01 matrix"],"depends_on":[]},
{"task_id":"T-05","title":"Mechanical extraction of service/suffix expr helpers","layer":"kernel","sensitive":true,
 "files":["backend/internal/kernel/real_firewall.go"],
 "instruction":"Move (no logic change) the 'Service / Protocol' block of buildRuleExpressions into svcComboMatchExprs(vc) and the counter/log/fwmark/verdict tail into userRuleSuffixExprs(chain, action, logEnabled, nat, logPrefix); buildRuleExpressions calls them. Same append order, same error messages.",
 "acceptance":["T-01 golden passes byte-for-byte","policy_chain_test.go unchanged and green"],"depends_on":["T-03"]},
{"task_id":"T-06","title":"Set-mode rule builder addUserChainRulesSets + dispatch","layer":"kernel","sensitive":true,
 "files":["backend/internal/kernel/real_firewall_sets.go","backend/internal/kernel/real_firewall.go"],
 "instruction":"Implement plan 3.3/3.5/3.6: per enabled rule of chainName: NormalizePolicyRuleInterfaces, UserData, default empty lists to ALL, collectAddrDim(src), collectAddrDim(dst), collectSvcDim(svc) always all three (reuse addressCombos/serviceCombos + same skip log formats), fail-closed on empty dimension, atom cap => skip policy + warning naming max-expanded-rules-per-policy, build variants, for EACH emitted rule create fresh sets via b.AddSet then build Lookup from set.Name/set.ID, then b.AddRule. Singletons reuse ipMatchExprsForCombo / svcComboMatchExprs / Meta+Cmp. Expr order and suffix via userRuleSuffixExprs. ApplyRules: dispatch on rf.useSets at the three existing call sites only.",
 "acceptance":["go build ./... ; go vet ./...","no exec.Command; single Flush","nft-use-sets=false path produces T-01 golden bytes"],"depends_on":["T-04","T-05"]},
{"task_id":"T-07","title":"Unit tests for set mode (pure, WithTestDial / fake nftBatch)","layer":"kernel",
 "files":["backend/internal/kernel/real_firewall_sets_test.go","backend/internal/kernel/policy_chain_test.go"],
 "instruction":"Use a recording fake nftBatch (assigns incremental set IDs like AddSet) and/or WithTestDial. No t.Parallel for AddSet tests.",
 "acceptance":["all-singleton policies: set mode NEWRULE bytes == T-01 golden entries, 0 sets","src 3 entries + dst 2 + 2 TCP ports + 3 in-ifaces (forward): 1 rule, 4 sets (ifname,ipv4,ipv4,concat) vs legacy count >= 36","ICMP + TCP 80 + UDP 53: 2 rules; rule A proto Cmp [1]; rule B concat 2 elems; no set ID referenced by more than one Lookup across the whole batch","TCP any + TCP 80: 1 rule proto Cmp [6], no concat set","src [ALL, LAN]: no src match exprs","src all unknown / only IPv6 entries / only unresolvable FQDN: 0 rules for that policy, other policies emitted; fqdnRec has FQDN key with empty value","overlapping 10.0.0.0/8 + 10.1.0.0/16 => one interval","FQDN 3 IPs + subnet dest => 1 rule, merged elems; fqdnRec keys equal legacy run for same input","input chain: no oifname; output: no iifname; fwmark only forward+nat+ACCEPT","every rule has UserData=r.ID and log prefix with r=<id> when Log","atom cap: cap=64 with 65 src entries => policy skipped, warning logged, next policy emitted","every AddSet: Anonymous&&Constant; Lookup after its AddSet in message order","dispatch: SetUseNFTSets(false) => golden bytes"],"depends_on":["T-06"]},
{"task_id":"T-08","title":"main.go wiring","layer":"wiring",
 "files":["backend/cmd/pigate/main.go"],
 "instruction":"After realFw.SetMaxExpandedRulesPerPolicy (~227): realFw.SetUseNFTSets(cfg.NFTUseSets); realFw.SetMaxTotalNFTRules(cfg.MaxTotalNFTRules); one startup log line with both values. Mock branch untouched.",
 "acceptance":["go build ./...","-mock=true boots and Apply works unchanged"],"depends_on":["T-02","T-03"]},
{"task_id":"T-09","title":"Opt-in netns integration test (real kernel via unshare -rn)","layer":"kernel","sensitive":true,
 "files":["backend/internal/kernel/real_firewall_netns_test.go"],
 "instruction":"Skip unless PIGATE_NFT_NETNS_TEST=1; t.Fatal unless net.Interfaces() == [lo]. Run: go test -c -o $SCRATCH/kernel.test ./internal/kernel && PIGATE_NFT_NETNS_TEST=1 unshare -rn $SCRATCH/kernel.test -test.run TestNetns -test.v. Subtests: (1) acceptance: test table inet pigate_it, output-hook chain, representative policies (Log=false) via addUserChainRulesSets, Flush ok, GetRules count == emitted, GetSets count == AddSet calls, GetSetElements compared as multiset to encoders (log kernel-returned elements); (2) apply 3x with FlushTable => set count stable (no leak); (3) behavioral: lo up + addrs 10.1.0.1/32,10.2.0.5/32,192.168.50.7/32 via vishvananda/netlink, tables pigate_legacy (addUserChainRules) and pigate_sets (Sets) both hook output with same ACCEPT policies; per vector (src,dst,udp/tcp,port) send 1 packet, per-policy counter delta via GetRules+accumulateRuleCounters must be equal in both tables; (4) full rf.ApplyRules smoke with log/nat: ENOENT/EOPNOTSUPP => t.Skip with message (missing module), any other error => t.Fatal; then SetMaxTotalNFTRules(1024)+oversized legacy input => ErrNftRuleBudgetExceeded and previous table still listed; FORCE->fallback path exercised without error. No exec.Command.",
 "acceptance":["compiles and skips under plain go test ./...","under unshare -rn on WSL2 5.15: subtests 1-3 pass (or report exact kernel errno for tech lead)"],"depends_on":["T-06"]},
{"task_id":"T-10","title":"Config docs: pigate.conf.example, README, CLAUDE.md, docs/data/firewall.md","layer":"docs",
 "files":["pigate.conf.example","README.md","CLAUDE.md","docs/data/firewall.md"],
 "instruction":"Document both file-only keys (default, range, restart-required, rollback meaning of nft-use-sets=false, reject-on-exceed of max-total-nft-rules). README Configuration File paragraph: add the two keys (drop the stale 'thirteen' count wording). CLAUDE.md config paragraph: one sentence naming the two file-only firewall keys. firewall.md ~114: sets mode note.",
 "acceptance":["key names/defaults identical to config.go"],"depends_on":["T-02"]},
{"task_id":"T-11","title":"docs/tech_stack_design.md §4.3","layer":"docs",
 "files":["docs/tech_stack_design.md"],
 "instruction":"Update example ruleset (~107-171) to show a user rule with anonymous sets (iifname { }, ip saddr { interval }, ip protocol . th dport { }) and the ICMP/port 2-rule split; fix stale 3b text (log is NFLOG single rule now); rewrite item 5 (~192) which says sets are not used; mention max-total-nft-rules and nft-use-sets. Keep 4-section order text.",
 "acceptance":["no claim of cartesian as current default","4-section order unchanged"],"depends_on":["T-06"]}
]
```

---

## 7. Final Acceptance (QA รอบเดียวหลังทุก task)

```json
{"final_acceptance":[
 "cd backend && go build ./... && go vet ./... && go test ./... ผ่าน (รวม -race สำหรับ ./internal/kernel/...)",
 "grep: ไม่มี exec.Command ใหม่; conn.Flush() มีจุดเดียวใน real_firewall.go; ไม่มี dependency ใหม่ใน go.mod",
 "golden: nft-use-sets=false ให้ NEWRULE bytes ตรง legacy golden ทุก policy; set mode singleton-only policies ตรง golden",
 "ลำดับ input chain 4 ส่วน + Admin Access/DNS accept ก่อน user rules ไม่เปลี่ยน (ตรวจลำดับ AddRule ใน ApplyRules ด้วย WithTestDial)",
 "rule reduction: policy 10 src × 10 dst × 5 svc(TCP/UDP) × 3 iface: legacy >= 3000 NEWRULE, set mode <= 2 NEWRULE",
 "semantics: netns behavioral test (T-09 subtest 3) ผ่าน — counter delta ต่อ policy เท่ากันทุก vector: subnet, subnet ซ้อน, /32, range, range คร่อม/ติดกัน, range ถึง 255.255.255.255, fqdn หลาย A record (stub), TCP/UDP/ICMP/ช่วงพอร์ต, TCP any + TCP port, หลาย interface",
 "kernel acceptance: T-09 subtest 1-2 ผ่านบน WSL2 (ไม่มี EINVAL/EEXIST/ENOTEMPTY/EBUSY) และไม่มี anonymous set leak หลัง apply 3 ครั้ง",
 "fail-closed: มิติว่าง ⇒ 0 rule สำหรับ policy นั้น; ไม่มี set ว่างใน batch",
 "budget: เกิน max-total-nft-rules ⇒ ErrNftRuleBudgetExceeded, ไม่ Flush, ruleset เดิมคงอยู่ (netns), FQDN snapshot ไม่เปลี่ยน",
 "socket option ไม่เคยทำให้ apply ล้ม (nltest + userns fallback)",
 "config: key ใหม่ทั้งสอง parse/clamp/round-trip ถูก; -mock=true boot + apply ปกติ; mock.go/interfaces.go ไม่ถูกแก้",
 "counter/log: accumulateRuleCounters บน GetRules ของ set-mode ruleset (netns) คืนค่าตาม rule id ถูก; log prefix มี r=<id>",
 "docs: pigate.conf.example/README/CLAUDE.md/firewall.md/tech_stack_design.md §4.3 อัปเดตตรงโค้ด",
 "(เจ้าของ, Pi 5 จริง — มี physical access) nft list ruleset before/after, จำนวน rule, เวลา apply, SSH/HTTP ไม่หลุด, ping, NAT/port-forward, Statistics counter, FQDN refresher ไม่ loop 30 นาที, rollback nft-use-sets=false + restart ได้ผล"
]}
```

---

## 8. การตัดสินใจของเจ้าของโปรเจกต์ (final) และประเด็นเปิด

- **D-1** Anonymous sets (`Anonymous+Constant`) ใน batch `Flush()` เดียว; named sets นอกขอบเขต (Phase 3 ได้)
- **D-2** คง `maxExpandedRulesPerPolicy` default 4096 (ห้ามลบ)
- **D-3** interval ซ้อนต้อง merge เองใน pure Go (IPv4 และ port ต่อ proto ใน concat) — semantics เท่าเดิม ต่างแค่การแสดงผล
- **D-4** Rollback: `nft-use-sets` (bool, default true); false = path cartesian เดิมทุกไบต์
- **D-5** Phase 1 ทำใน PR นี้: socket buffer (`WithSockOptions` + FORCE, fallback ธรรมดา) + `max-total-nft-rules`
  (default 16384, reject ก่อน Flush, kernel คง ruleset เดิม)
- **D-6** (tech lead ตัดสินตามที่มอบหมาย) set mode: cap นับ nft rule ต่อ policy + atom ต่อมิติ; เกิน ⇒ skip policy + warning
- **D-7** (tech lead) key ใหม่ทั้งสองเป็น **file-only** (mirror `max-expanded-rules-per-policy` ซึ่งไม่มี CLI flag) —
  หลีกเลี่ยงกับดัก "flag ใน systemd unit ทับไฟล์เงียบๆ"
- **D-8** (tech lead) singleton ตัดสินจาก atom หลัง dedupe ก่อน merge; IPv4 set ส่ง leading zero end element แบบ nft CLI

**ประเด็นเปิด — เจ้าของตัดสินแล้ว (2026-10-01): ทำตามข้อแนะนำทุกข้อ**
- **Open-1** Budget reject ตอน boot ⇒ ไม่มี firewall (Caution 9). แนะนำ: ยอมรับใน PR นี้ (มี event log + ApplyHealth แสดง error)
  แล้วเปิด issue แยกสำหรับ "boot lockdown ruleset" ถ้าต้องการ
- **Open-2** D-6 ทางเลือก: skip policy (แนะนำ) / truncate แบบเดิม / reject ทั้ง apply — skip DROP policy = ไม่ block ตามที่ตั้งใจ
  (แต่ต้องมี >4096 atom ต่อมิติ แทบเป็นไปไม่ได้เพราะ `max-object-entries` 64)
- **Open-3** Frontend estimate ยังแสดงตัวเลข cartesian + คำเตือน cap — follow-up issue (backend ไม่ expose โหมด)
- **Open-4** ต้องการ CLI flag `-nft-use-sets` เพิ่มหรือไม่ (D-7 เลือก file-only)

ผลการตัดสิน: Open-1 ยอมรับใน PR นี้ + issue แยก (boot lockdown) · Open-2 skip policy + warning ·
Open-3 follow-up issue แยก · Open-4 file-only (ไม่มี CLI flag)

---

## 9. อ้างอิง

- `docs/tech_stack_design.md` §4.3 — โครงสร้าง 4 ส่วน
- `docs/ref/complete/multi-value-address-service-objects-plan.md` — ที่มาของ cartesian + pattern config key
- `docs/ref/todo/multi-interface-firewall-rule-plan.md` — in×out (D-1 Option A)
- `docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md` — สัญญา FQDN snapshot (D-1/D-2)
- `backend/internal/kernel/real_firewall.go`, `real_traffic_account.go`, `backend/internal/config/config.go`
- google/nftables v0.3.0 `set.go`, `conn.go`; mdlayher/netlink v1.11.2 `conn.go`
