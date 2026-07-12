# Policy-based Source NAT — ย้าย NAT masquerade ไปไว้ที่ Firewall Policy (แบบ FortiGate)

> แผนงาน: เปลี่ยนกลไก source NAT จาก "auto-masquerade บน interface ที่ Role=WAN"
> ให้เป็น **toggle NAT ต่อ firewall policy** โดยมี option เดียวตอนนี้คือ
> "Use Outgoing Interface Address" (= nftables `masquerade`) — ที่เดียว ชัดเจน
> ตรงกับโมเดล FortiGate; `Role` เหลือเป็น metadata ล้วน
>
> เขียนเมื่อ: 2026-07-12 · Reference branch: `feat/policy-based-source-nat`
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

## 1. Current State (สำรวจโค้ดจริง 2026-07-12)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `PolicyRule` / `PolicyRuleInput` model | ไม่มี field `nat` | `model/types.go:101`, `:116` |
| NAT masquerade (postrouting) | มี แต่ผูกกับ Role=="WAN" — ต้องเปลี่ยนเป็น per-policy | `kernel/real_firewall.go:528-556` (loop `if strings.ToUpper(iface.Role)=="WAN"` → `&expr.Masq{}`) |
| Forward user rules gen | มี — ต้องแทรก `meta mark set` เมื่อ nat | `real_firewall.go:458` loop, `buildRuleExpressions:729` |
| `ApplyRules` signature | รับ `rules,ifaces,...` อยู่แล้ว → ไม่ต้องเพิ่มพารามิเตอร์ | `interfaces.go:11`, real `real_firewall.go:28`, mock `mock.go:30` |
| DB `firewall_policies` | ไม่มีคอลัมน์ `nat` | `db/connection.go:244-253` |
| Repo CRUD (อ่าน/เขียน policy) | SELECT/INSERT/UPDATE ระบุคอลัมน์ตรง ๆ — ต้องเพิ่ม nat | `db/repository.go:600,661,755,801` |
| Handler map input→model | ต้อง map `Nat` เพิ่ม | `api/handlers.go:944` (create), `:975` (update) |
| Frontend dialog/table | มี Switch (log/status) อยู่แล้ว — เพิ่ม NAT Switch + column | `pages/FirewallPolicy.tsx:233,243` |
| `policyService.ts` | ยังพบเป็น localStorage/mock (`import ...mockData`, `getLocalPolicies`) — **real API path ยังไม่ trace เต็ม** | `services/policyService.ts:1,8` |
| Backup/Restore | `BackupConfig.Policies` รวม policy อยู่แล้ว → nat จะไหลตามถ้าเพิ่มใน struct | `service/backup.go` (cfg.Policies) |
| OpenAPI policy schema | ต้องเพิ่ม `nat` | `docs/openapi.yaml` + `frontend/public/openapi.yaml` |
| `Role` ที่ใช้ที่อื่น (ไม่กระทบ) | ใช้ detect WAN link (DNS/traffic) — คงไว้ | `service/dns.go:39,97`, `service/system_status.go:152` |

สรุป: งานกระจุกที่ `real_firewall.go` (กลไก mark→masquerade) + DB migration (คอลัมน์ +
**data migration**) + เดินผ่าน model→repo→handler→openapi→frontend ปกติ; ไม่แตะ mock, ไม่แตะ interface signature

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

**ทางเลือกที่ตัดทิ้ง:**
1. *interface-level NAT toggle (แยกจาก Role)* — เคยเลือกแล้วเปลี่ยน: FortiGate เก็บ NAT ไว้ที่ policy ที่เดียว สะอาดกว่า ไม่มี NAT 2 แหล่งให้สับสน
2. *replicate saddr/daddr/service ลง postrouting โดยตรง* — ซ้ำ logic ของ `buildRuleExpressions` และ match service ใน SNAT แปลก; mark สะอาดกว่า
3. *ct mark แทน meta mark* — ไม่จำเป็น: ทุก forwarded packet ผ่าน forward→postrouting ต่อ packet อยู่แล้ว, per-packet meta mark เพียงพอ (masquerade บน established เป็น no-op)
4. *scope masquerade ด้วย oifname* — ไม่ต้อง: masquerade ใช้ oif addr อยู่แล้ว

**Pattern:** ตาม `real_firewall.go` เดิม (`expr.Meta`/`expr.Masq`/`expr.Cmp` มีใช้ในไฟล์แล้ว)

## 3. Steps (ชั้นในสุด → นอก)

### Step 1 — model
**File:** `backend/internal/model/types.go:101,116`
เพิ่ม `Nat bool \`json:"nat"\`` ทั้ง `PolicyRule` และ `PolicyRuleInput`

### Step 2 — kernel (หัวใจ)
**File:** `backend/internal/kernel/real_firewall.go`
- `buildRuleExpressions:729` — รับ/อ่านค่า `nat` ; เมื่อ `nat && action==ACCEPT` แทรก
  `meta mark set 0x1` **ก่อน** verdict accept (อยู่ในส่วน dynamic-accept ไม่กระทบ 4-section order)
- NAT block `:528-556` — ลบ loop `if Role=="WAN"` ทิ้ง, คง `natTable`/`natChain` (postrouting)
  ไว้ แล้วเพิ่ม rule เดียว `meta mark 0x1 → masquerade`

### Step 3 — mock
> **ไม่ต้องแก้:** `mock.go:30` `ApplyRules` เป็น log-only และ signature เดิม (nat เป็น field
> ของ `PolicyRule` ที่ส่งเข้ามาอยู่แล้ว) — mock ไม่แตะ OS จึงไม่มี NAT ให้ทำ

### Step 4 — DB migration + data migration
**File:** `backend/internal/db/connection.go` (ตาม pattern `ALTER TABLE ... ADD COLUMN` เช่น `:489`)
- `ALTER TABLE firewall_policies ADD COLUMN nat INTEGER DEFAULT 0 CHECK(nat IN (0,1))`
- **data migration (best-effort รักษาพฤติกรรมเดิม):** หลังคอลัมน์พร้อม —
  `UPDATE firewall_policies SET nat=1 WHERE action='ACCEPT' AND (out_interface IN (SELECT id/name ของ iface ที่ role='WAN') OR out_interface IN ('','ANY'))`
  (ต้องรัน **หลัง** ตาราง `network_interfaces` พร้อม — ดู Caution 1)

### Step 5 — repository
**File:** `backend/internal/db/repository.go:600,661,755,801`
เพิ่ม `nat` ใน SELECT (`GetPolicies`,`GetPolicyByID`), INSERT (`CreatePolicy`), UPDATE (`UpdatePolicy`)

### Step 6 — handler
**File:** `backend/internal/api/handlers.go:944,975`
map `input.Nat` → `rule.Nat` ทั้ง create/update

### Step 7 — OpenAPI (ทั้งสองไฟล์)
**File:** `docs/openapi.yaml` + `frontend/public/openapi.yaml`
เพิ่ม `nat: {type: boolean, description: "Source NAT (masquerade to outgoing interface address)"}` ใน `PolicyRule`/input schema

### Step 8 — frontend
**File:** `frontend/src/pages/FirewallPolicy.tsx` + type ใน `data-mockup/mockData.ts` + `services/policyService.ts`
- เพิ่ม `nat: boolean` ใน type `PolicyRule`
- dialog: `<Switch>` "NAT" (hint: Use Outgoing Interface Address) ; ตาราง: badge NAT
- อัปเดต mock/localStorage ให้มี field (และ verify real API path ถ้าใช้จริง)

### Step 9 — docs (optional)
**File:** `docs/ref/complete/` firewall design — จดว่าNAT เป็น policy-based แล้ว

## 4. Related API

| Method | Path | Role | เปลี่ยนแปลง |
|---|---|---|---|
| POST | `/api/firewall/policies` | authRoute (RoleReadOnly บล็อก non-super_admin, `-disable-edit` บล็อก) | รับ field `nat` |
| PUT | `/api/firewall/policies/{id}` | authRoute เดียวกัน | รับ field `nat` |
| GET | `/api/firewall/policies` | authRoute | คืน field `nat` |

ไม่มี route ใหม่ — เพิ่ม field ใน payload เดิม

## 5. Cautions

1. **[ใหญ่สุด] semantic change ตอน upgrade** — เดิม NAT อัตโนมัติทุก packet ออก WAN; ใหม่
   เฉพาะ policy ที่ `nat=1`. *เกิดอะไร:* ไม่ migrate = LAN ออกเน็ตไม่ได้ทันทีหลัง upgrade.
   *กัน:* data migration (Step 4) ตั้ง `nat=1` บน ACCEPT policy ที่ออก WAN + เคส `out_interface=ANY`
   ตั้งด้วย **พร้อม log warning + release note** ให้แอดมินรีวิว (ANY จะทำให้ traffic ออก LAN โดน
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

## 6. Summary Checklist (Definition of Done)

- [ ] `model/types.go` — `Nat bool` ใน `PolicyRule` + `PolicyRuleInput`
- [ ] `kernel/real_firewall.go` — `buildRuleExpressions` แทรก `meta mark set` เมื่อ nat+ACCEPT;
      NAT block เปลี่ยนจาก Role=WAN loop → rule เดียว `mark→masquerade`
- [ ] `db/connection.go` — migration `ADD COLUMN nat` + data migration (nat=1 บน policy ออก WAN/ANY)
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
