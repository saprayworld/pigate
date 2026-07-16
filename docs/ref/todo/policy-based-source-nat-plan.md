# Policy-based Source NAT — ย้าย NAT masquerade ไปไว้ที่ Firewall Policy (แบบ FortiGate)

> แผนงาน: เปลี่ยนกลไก source NAT จาก "auto-masquerade บน interface ที่ Role=WAN"
> ให้เป็น **toggle NAT ต่อ firewall policy** โดยมี option เดียวตอนนี้คือ
> "Use Outgoing Interface Address" (= nftables `masquerade`) — ที่เดียว ชัดเจน
> ตรงกับโมเดล FortiGate; `Role` เหลือเป็น metadata ล้วน
>
> เขียนเมื่อ: 2026-07-12 · ตรวจทานกับโค้ดจริงล่าสุด: 2026-07-16 (main @ `1d22ba1`) ·
> Reference branch: `feat/policy-based-source-nat` (ยังไม่ได้สร้าง — งานยังไม่เริ่ม)
> ต่อยอด: NAT masquerade ปัจจุบันอยู่ที่ `real_firewall.go` (postrouting, Role=WAN)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- firewall rule dialog มี toggle **"NAT"** — เปิดแล้ว traffic ที่ match rule นั้นถูก
  masquerade ออก egress interface (Use Outgoing Interface Address)
- ครอบทั้ง LAN→WAN และ **LAN-to-LAN NAT** (ออก interface ไหนก็ masquerade ตาม oif)
- **ลบ auto-masquerade ที่ผูกกับ Role=="WAN" ออกทั้งหมด** — NAT มาจาก policy ที่เดียว
- อุปกรณ์ที่ upgrade มาต้องไม่หลุด internet (migration ตั้ง nat ให้ policy เดิมที่ออก WAN)

**Out of scope (ตัดชัด):**
- **DNAT / port-forward** → แผนแยก `port-forward-dnat-plan.md` (ต่อจากแผนนี้)
- **SNAT ไป IP/pool เจาะจง** (`snat to <ip>`) — เฟสหลัง โครงรองรับแต่ยังไม่ทำ
- **interface-level NAT toggle** — เคยพิจารณาแล้วเปลี่ยนมาเป็น policy-only (ดู §2)
- IPv6 NAT (pigate_nat เป็น family ip); hairpin/NAT-loopback

## 1. Current State (สำรวจ 2026-07-12 · ตรวจทานเลขบรรทัดใหม่ 2026-07-16)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `PolicyRule` / `PolicyRuleInput` model | ไม่มี field `nat` | `model/types.go:101`, `:116` |
| NAT masquerade (postrouting) | มี แต่ผูกกับ Role=="WAN" — ต้องเปลี่ยนเป็น per-policy | `kernel/real_firewall.go:528-556` (loop `if strings.ToUpper(iface.Role)=="WAN"` → `&expr.Masq{}` ที่ `:543-556`) |
| Forward user rules gen | มี — ต้องแทรก `meta mark set` เมื่อ nat | loop `real_firewall.go:459`, call site `:498`, `buildRuleExpressions:729` (verdict ต่อท้ายที่ `:878-883`) |
| `ApplyRules` signature | รับ `rules,ifaces,...` อยู่แล้ว → ไม่ต้องเพิ่มพารามิเตอร์ | `interfaces.go:12`, real `real_firewall.go:28`, mock `mock.go:30` |
| Service layer (`firewall.go`) | pass-through `model.PolicyRule` ทั้ง Create/Update/Apply → **ไม่ต้องแก้** | `service/firewall.go:44,49,118` |
| DB `firewall_policies` | ไม่มีคอลัมน์ `nat` | `db/connection.go:244-253` |
| Repo CRUD (อ่าน/เขียน policy) | SELECT/INSERT/UPDATE ระบุคอลัมน์ตรง ๆ — ต้องเพิ่ม nat | `db/repository.go:607` (GetPolicies), `:667` (GetPolicyByID), `:762` (INSERT), `:808` (UPDATE) |
| Toggle endpoints (log/status) | มี `POST /api/policies/{id}/toggle-log`,`/toggle-status` (`UPDATE ... SET x = NOT x`) — NAT ใช้ dialog Switch จึง**ไม่ต้องเพิ่ม** `toggle-nat`; response เป็น `PolicyRule` เต็ม → `nat` ไหลตามเอง | `api/router.go:72-73`, `db/repository.go:917,922` |
| Handler map input→model | ต้อง map `Nat` เพิ่ม | `api/handlers.go:1024` (create), `:1061` (update) |
| Frontend dialog/table | มี Switch (log/status) อยู่แล้ว — เพิ่ม NAT Switch + column | `pages/FirewallPolicy.tsx:233,243` |
| `policyService.ts` | มีครบทั้ง mock (localStorage) และ real API (`fetch ${API_BASE_URL}/policies`); create/update ส่ง rule ทั้ง object → เพิ่ม `nat` ใน type แล้วไหลผ่านเอง | `services/policyService.ts:37,82,117` |
| Backup/Restore | `BackupConfig.Policies` ใช้ `model.PolicyRule` ตรง ๆ (ทั้ง v1→v2 map และ restore loop) → nat ไหลตามเมื่อเพิ่มใน struct | `service/backup.go:97,583,599,637` |
| OpenAPI policy schema | ต้องเพิ่ม `nat` | `docs/openapi.yaml:3219` (PolicyRule), `:3271` (PolicyRuleInput) + `frontend/public/openapi.yaml` |
| `Role` ที่ใช้ที่อื่น (ไม่กระทบ) | ใช้ detect WAN link (DNS/traffic) — คงไว้ | `service/dns.go:51,134`, `service/system_status.go:207-214` |

สรุป: งานกระจุกที่ `real_firewall.go` (กลไก mark→masquerade) + DB migration (คอลัมน์ +
**data migration**) + เดินผ่าน model→repo→handler→openapi→frontend ปกติ; ไม่แตะ mock, ไม่แตะ interface signature, ไม่แตะ service layer

## 2. Technical Approach

**กลไก: mark-in-forward → masquerade-by-mark-in-postrouting**

Netfilter ประมวลผล forwarded packet ตามลำดับ `prerouting → forward(filter) → postrouting(nat)`
→ mark ที่เซ็ตใน forward chain มองเห็นใน postrouting เสมอ

```go
// ใน forward chain: rule ที่ nat=true && action=ACCEPT — แทรกก่อน verdict
&expr.Immediate{Register: 1, Data: []byte{0x01, 0, 0, 0}},
&expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1}, // meta mark set 0x1

// ใน pigate_nat postrouting: rule เดียว แทน loop Role=WAN เดิม
&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{0x01, 0, 0, 0}},
&expr.Masq{}, // masquerade = ใช้ IP ของ egress interface = "Use Outgoing Interface Address"
```

- `expr.Masq{}` ใช้ address ของ oif โดยนิยาม → ตรงกับ option "Use Outgoing Interface Address" พอดี
- mark namespace **ว่างสนิท** (grep แล้วไม่มี fwmark/QoS ใช้ — QoS ใช้ U32 filter) จึงไม่ชน
- mark ข้าม table/family ได้: `pigate` เป็น family **inet** ส่วน `pigate_nat` เป็น family **ip**
  แต่ meta mark เป็น metadata บน skb (ตัว packet) ไม่ผูกกับ table → เซ็ตใน inet เห็นใน ip ได้
- nat chain (postrouting type NAT) ประเมินเฉพาะ **packet แรกของ flow** — packet ถัดไป conntrack
  ทำ NAT ให้เองโดยไม่ผ่าน chain; forward chain เห็น packet แรกเสมอ → mark ทันเวลาแน่นอน

**ทางเลือกที่ตัดทิ้ง:**
1. *interface-level NAT toggle (แยกจาก Role)* — เคยเลือกแล้วเปลี่ยน: FortiGate เก็บ NAT ไว้ที่ policy ที่เดียว สะอาดกว่า ไม่มี NAT 2 แหล่งให้สับสน
2. *replicate saddr/daddr/service ลง postrouting โดยตรง* — ซ้ำ logic ของ `buildRuleExpressions` และ match service ใน SNAT แปลก; mark สะอาดกว่า
3. *ct mark แทน meta mark* — ไม่จำเป็น: NAT decision เกิดที่ packet แรกของ flow เท่านั้น
   (nat chain ไม่เห็น packet ถัดไป — ดูข้อสังเกตด้านบน) จึงไม่ต้องพก mark ข้าม packet ด้วย ct mark
4. *scope masquerade ด้วย oifname* — ไม่ต้อง: masquerade ใช้ oif addr อยู่แล้ว

**Pattern:** ตาม `real_firewall.go` เดิม (`expr.Meta`/`expr.Masq`/`expr.Cmp` มีใช้ในไฟล์แล้ว)

## 3. Steps (ชั้นในสุด → นอก)

### Step 1 — model
**File:** `backend/internal/model/types.go:101,116`
เพิ่ม `Nat bool \`json:"nat"\`` ทั้ง `PolicyRule` และ `PolicyRuleInput`

### Step 2 — kernel (หัวใจ)
**File:** `backend/internal/kernel/real_firewall.go`
- `buildRuleExpressions:729` — เพิ่มพารามิเตอร์ `nat bool` ใน signature (`:729-738`) และส่ง
  `r.Nat` จาก call site `:498`; เมื่อ `nat && action==ACCEPT` แทรก `meta mark set 0x1`
  ระหว่าง step 7 (log, `:874`) กับ step 8 (verdict, `:878`) — คือ**ก่อน** verdict accept
  (อยู่ในส่วน dynamic-accept ไม่กระทบ 4-section order)
- NAT block `:528-556` — ลบ loop `if Role=="WAN"` ทิ้ง (`:543-556`), คง `natTable`/`natChain`
  (postrouting) ไว้ แล้วเพิ่ม rule เดียว `meta mark 0x1 → masquerade`

### Step 3 — mock
> **ไม่ต้องแก้:** `mock.go:30` `ApplyRules` เป็น log-only และ signature เดิม (nat เป็น field
> ของ `PolicyRule` ที่ส่งเข้ามาอยู่แล้ว) — mock ไม่แตะ OS จึงไม่มี NAT ให้ทำ

### Step 4 — DB migration + data migration
**File:** `backend/internal/db/connection.go` (ตาม pattern ตรวจ DDL จาก sqlite_master แล้ว `ALTER TABLE ... ADD COLUMN` เช่น `:488-493`)
- `ALTER TABLE firewall_policies ADD COLUMN nat INTEGER DEFAULT 0 CHECK(nat IN (0,1))`
- **data migration (best-effort รักษาพฤติกรรมเดิม):**
  ```sql
  UPDATE firewall_policies SET nat=1 WHERE action='ACCEPT' AND (
    out_interface IN (SELECT name FROM network_interfaces WHERE role='WAN')
    OR out_interface IN ('','ALL'))
  ```
  - `out_interface` เก็บ**ชื่อ** interface (kernel เทียบด้วย `padInterfaceName` — `real_firewall.go:750`)
    ไม่ใช่ id; ค่า any คือ `'ALL'` (frontend default, `FirewallPolicy.tsx:420`) **ไม่ใช่ 'ANY'**
  - ต้องรันใน **guard branch เดียวกับ ADD COLUMN** (one-shot ตอนคอลัมน์เพิ่งถูกเพิ่ม) —
    ถ้ารันทุก boot จะ flip ค่า nat=0 ที่แอดมินตั้งใจแก้ กลับเป็น 1
  - ลำดับปลอดภัยอยู่แล้ว: CREATE TABLE ทั้งหมดมาก่อน block migration ใน `connection.go`
    → อ่าน `network_interfaces` ได้; DB ใหม่ทั้งสองตารางว่าง → UPDATE เป็น no-op

### Step 5 — repository
**File:** `backend/internal/db/repository.go:607,667,762,808`
เพิ่ม `nat` ใน SELECT + `Scan` (`GetPolicies:607`, `GetPolicyByID:667`), INSERT (`CreatePolicy:762`
— ปัจจุบัน 8 คอลัมน์), UPDATE (`UpdatePolicy:808` — ปัจจุบัน 6 setter)

### Step 6 — handler
**File:** `backend/internal/api/handlers.go:1024,1061`
map `input.Nat` → `rule.Nat` ทั้ง create (`HandleCreatePolicy:1012`) / update (`HandleUpdatePolicy:1047`)

### Step 7 — OpenAPI (ทั้งสองไฟล์)
**File:** `docs/openapi.yaml:3219,3271` + `frontend/public/openapi.yaml` (sync กัน)
เพิ่ม `nat: {type: boolean, description: "Source NAT (masquerade to outgoing interface address)"}`
ใน `PolicyRule`/`PolicyRuleInput` — **ไม่ใส่ใน `required`** (client เก่า/payload เก่าที่ไม่ส่ง nat
ยัง valid; Go zero-value = false)

### Step 8 — frontend
**File:** `frontend/src/pages/FirewallPolicy.tsx` + type ใน `data-mockup/mockData.ts:98` + `services/policyService.ts`
- เพิ่ม `nat: boolean` ใน type `PolicyRule`
- dialog: `<Switch>` "NAT" (hint: Use Outgoing Interface Address) ; ตาราง: badge NAT
- อัปเดต mock/localStorage ให้มี field (และ verify real API path ถ้าใช้จริง)

### Step 9 — docs (optional)
**File:** `docs/ref/complete/` firewall design — จดว่าNAT เป็น policy-based แล้ว

## 4. Related API

| Method | Path | Role | เปลี่ยนแปลง |
|---|---|---|---|
| POST | `/api/policies` | authRoute (RoleReadOnly บล็อก non-super_admin, `-disable-edit` บล็อก) | รับ field `nat` |
| PUT | `/api/policies/{id}` | authRoute เดียวกัน | รับ field `nat` |
| GET | `/api/policies` | authRoute | คืน field `nat` |
| POST | `/api/policies/{id}/toggle-log`, `/toggle-status` | authRoute เดียวกัน | **ไม่แก้** — response คืน `PolicyRule` เต็ม `nat` ไหลตามเอง |

(path จริงตาม `api/router.go:67-74` คือ `/api/policies` — ฉบับก่อนเขียนผิดเป็น `/api/firewall/policies`)
ไม่มี route ใหม่ — เพิ่ม field ใน payload เดิม; ไม่เพิ่ม `toggle-nat` เพราะ NAT switch อยู่ใน dialog
(save ผ่าน PUT ปกติ) ไม่ใช่ switch คาตาราง

## 5. Cautions

1. **[ใหญ่สุด] semantic change ตอน upgrade** — เดิม NAT อัตโนมัติทุก packet ออก WAN; ใหม่
   เฉพาะ policy ที่ `nat=1`. *เกิดอะไร:* ไม่ migrate = LAN ออกเน็ตไม่ได้ทันทีหลัง upgrade.
   *กัน:* data migration (Step 4) ตั้ง `nat=1` บน ACCEPT policy ที่ออก WAN + เคส `out_interface='ALL'`
   ตั้งด้วย **พร้อม log warning + release note** ให้แอดมินรีวิว (ALL จะทำให้ traffic ออก LAN โดน
   NAT ด้วยซึ่งเดิมไม่โดน). migration ต้องรัน **หลัง** `network_interfaces` เพื่ออ่าน role ได้
2. **ลำดับ hook** — mark เซ็ตใน forward, match ใน postrouting; ถูกเพราะ `forward` มาก่อน
   `postrouting`. *กัน:* อย่าเผลอเซ็ต mark ใน chain อื่น (input/prerouting ของ filter)
3. **NAT เฉพาะ ACCEPT** — *เกิดอะไร:* ติ๊ก NAT บน rule DROP ไร้ความหมาย. *กัน:* ตอน gen ข้าม
   mark ถ้า `action!=ACCEPT` (และ frontend disable NAT switch เมื่อเลือก DROP)
4. **mark bit สงวน** — ใช้ `0x1` เดี่ยว ๆ; ถ้าอนาคตมี mark อื่นให้ใช้ bitmask เฉพาะ กันชน
5. **firewall 4-section order** — การแทรก mark อยู่ใน section "dynamic accept" ของ forward chain
   ไม่ขยับ sanity/log/drop; ต้องคงลำดับเดิม (CLAUDE.md)
6. **Backup/Restore (schema v2)** — *เกิดอะไร:* ถ้า `BackupConfig` mapping ไม่รวม `nat` → export/import
   แล้ว nat หาย. *กัน:* ตรวจว่า `service/backup.go` map policy รวม field `nat` (PolicyRule มี field
   แล้วมักไหลตาม แต่ยืนยัน) + validate ไม่ต้องพิเศษ
7. **IPv4-only** — `pigate_nat` family ip → NAT เฉพาะ IPv4 (จดใน docs/release note)
8. **role/`-disable-edit`** — mutation policy โดน `RoleReadOnlyMiddleware` + `DisableEditMiddleware`
   อยู่แล้ว ไม่ต้องเพิ่ม
9. **docker-compat accept ไม่ถูก mark** — traffic ที่ accept ผ่าน docker-compat rules
   (`iif docker0`/`br-*`, `real_firewall.go:430-455`) ไม่ผ่าน user rule → ไม่มี mark → PiGate
   ไม่ masquerade ให้ (เดิม Role=WAN NAT ให้ทุกอย่างรวม container). *ปกติไม่กระทบ:* Docker ตั้ง
   MASQUERADE ของ bridge subnet เองใน iptables-nft. *กัน:* จดใน release note; ถ้าผู้ใช้ปิด
   `iptables:true` ของ Docker ต้องสร้าง ACCEPT+NAT policy เอง

## 6. Summary Checklist (Definition of Done)

- [ ] `model/types.go` — `Nat bool` ใน `PolicyRule` + `PolicyRuleInput`
- [ ] `kernel/real_firewall.go` — `buildRuleExpressions` แทรก `meta mark set` เมื่อ nat+ACCEPT;
      NAT block เปลี่ยนจาก Role=WAN loop → rule เดียว `mark→masquerade`
- [ ] `db/connection.go` — migration `ADD COLUMN nat` + data migration (nat=1 บน ACCEPT policy
      ออก WAN/'ALL'/'' — one-shot ใน guard branch เดียวกับ ADD COLUMN)
- [ ] `db/repository.go` — nat ใน SELECT/INSERT/UPDATE (4 จุด)
- [ ] `api/handlers.go` — map `Nat` create/update
- [ ] `go build ./...` + `go test ./...` ผ่าน (เพิ่ม test: rule nat=true → มี mark+masquerade; migration ตั้ง nat ถูก)
- [ ] `service/backup.go` — ยืนยัน export/import รวม `nat`
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — เพิ่ม `nat` (sync)
- [ ] frontend: NAT Switch ใน dialog + badge ในตาราง + type/mock; `yarn build`+`yarn lint` ผ่าน
- [ ] ทดสอบเครื่องจริง: policy LAN→WAN nat=on → client ออกเน็ตได้ (masquerade); LAN-to-LAN nat=on →
      ปลายทางเห็น src เป็น IP PiGate; nat=off → ไม่ NAT; `nft list table ip pigate_nat` เห็น mark→masq
- [ ] ทดสอบ upgrade: DB เดิม (policy ออก WAN) → หลัง migrate `nat=1` และเน็ตไม่หลุด
- [ ] release note: โมเดล NAT เปลี่ยนเป็น policy-based + IPv4-only
- [ ] เสร็จแล้วย้ายแผนไป `docs/ref/complete/`
