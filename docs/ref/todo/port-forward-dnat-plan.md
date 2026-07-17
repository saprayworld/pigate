# Port Forwarding / DNAT (Virtual IP) — เปิด service ใน LAN ออกสู่ภายนอกผ่าน DNAT

> แผนงาน: เพิ่มความสามารถ **DNAT / port-forward** — ยิงเข้ามาที่ (interface/IP + port
> ภายนอก) แล้ว PiGate แปลงปลายทางไปเครื่องใน LAN (internal IP:port) แบบ FortiGate VIP
> เพิ่ม **prerouting NAT chain** ใน `pigate_nat` + resource/หน้า "Port Forwarding" ใหม่
> + auto-gen forward-accept rule กันกับดัก "DNAT ถูกแต่ firewall drop"
>
> เขียนเมื่อ: 2026-07-12 · ตรวจทานกับโค้ดจริงล่าสุด: 2026-07-17 · Reference branch: `feat/port-forward-dnat`
> **Depends on:** `policy-based-source-nat-plan.md` — ✅ **merged แล้ว** (PR #59, ย้ายไป
> `docs/ref/complete/` แล้ว) → dependency ปลดล็อก เริ่มงานนี้ได้เลย

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- หน้า "Port Forwarding" เพิ่ม entry: `{name, inInterface (external), externalPort(range),
  protocol (tcp/udp), internalIP, internalPort, enabled}`
- ยิงจากภายนอกเข้า `<external iface addr>:<externalPort>` → DNAT ไป `internalIP:internalPort`
- traffic ที่ DNAT แล้ว **ทะลุ forward chain ได้** (auto-gen forward-accept คู่กัน)
- return traffic กลับถูก un-DNAT อัตโนมัติ (conntrack)

**Out of scope (ตัดชัด):**
- **hairpin / NAT-loopback** (internal client → external addr → internal server, ต้อง SNAT เพิ่ม) → เฟสหลัง
- DNAT แบบ 1:1 (map ทั้ง IP), load-balance หลาย backend, **port-range translation ทุกแบบ**
  (range รองรับเฉพาะ keep-port — ดู Caution 9)
- IPv6 (pigate_nat family ip); source-restricted DNAT (จำกัด src) — ค่อยเพิ่มทีหลัง

## 1. Current State (ตรวจทานโค้ดจริง 2026-07-17 หลัง PR #59 merge)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `pigate_nat` table | มี (จาก Plan 1/PR #59) — **postrouting** chain + masquerade-on-fwmark-0x1 (policy NAT toggle) | `kernel/real_firewall.go:535-557` |
| prerouting NAT chain (DNAT) | **ยังไม่มี** — ต้องเพิ่ม (Type NAT, Hook Prerouting, Priority Dstnat) | — |
| forward chain (จุดวาง auto-accept) | ct est/rel accept → ct invalid drop → docker-compat bypass → **user rules (`:458`)** → final drop+NFLOG (`:519`) | `real_firewall.go:395-527` |
| Port-forward model/DTO | ยังไม่มี | — |
| DB table port_forwards | ยังไม่มี | `db/connection.go` (ตาม pattern `firewall_policies:244`) |
| Repository CRUD | ยังไม่มี | `db/repository.go` |
| Service layer | ยังไม่มี — ปัจจุบัน firewall apply รวมที่ `service/firewall.go:118` (`ApplyRules`) | — |
| `FirewallManager.ApplyRules` | รับ rules/ifaces/addrs/svcs/dhcpServerIfaces/dnsServerIfaces — DNAT ต้องส่ง port-forwards เข้าไปด้วย (ขยาย signature) | `interfaces.go:12`, real `:28`, mock `:30` |
| Route/handler | ยังไม่มี — ตาม pattern policy handler; **convention route จริงเป็นแบบ flat** (`/api/policies` ไม่มี `/api/firewall/...` prefix) | `api/handlers.go:1012` (`HandleCreatePolicy`), `api/router.go:67-74` |
| Frontend หน้า/‌service | ยังไม่มี — ตาม pattern `pages/FirewallPolicy.tsx` + `services/policyService.ts`; เมนูที่ `components/app-sidebar.tsx:62` | — |
| Backup/Restore | `BackupConfig` (schema v2) ยังไม่รวม port-forwards; checksum recompute จาก typed struct — ดู Caution 8 | `model/backup.go:55`, `service/backup.go:516-570` |
| OpenAPI | ยังไม่มี endpoint | ทั้งสองไฟล์ |

สรุป: เป็น **feature ใหม่เกือบทั้งหมด** (model→db→repo→service→handler→route→frontend) +
เพิ่ม prerouting chain ใน kernel; NAT-table ที่ Plan 1 refactor เสร็จแล้ว ใช้ต่อได้ทันที

## 2. Technical Approach

**DNAT อยู่ที่ prerouting (คนละ hook กับ SNAT):**

```go
// เพิ่มใน pigate_nat: prerouting chain
dnatChain := conn.AddChain(&nftables.Chain{
    Name: "prerouting", Table: natTable, Type: nftables.ChainTypeNAT,
    Hooknum: nftables.ChainHookPrerouting, Priority: nftables.ChainPriorityNATDest,
})
// ต่อ port-forward entry:
//   iifname==<ext> && fib daddr type local && <proto> dport==<extPort> → dnat to internalIP:internalPort
//   expr.Meta{IIFNAME} + Cmp, expr.Fib{FlagDADDR,ResultADDRTYPE} + Cmp(RTN_LOCAL),
//   expr.Payload(proto+dport) + Cmp, expr.Immediate(ip+port), expr.NAT{Type: DNAT}
```

- **ต้องมี `fib daddr type local`** — ถ้า match แค่ iifname+dport จะ DNAT traffic ที่แค่
  *transit ผ่าน* ext iface (dst เป็นเครื่องอื่น ไม่ใช่ PiGate) ด้วย ซึ่งผิด; เช็ค daddr เป็น
  local address ก่อนจึงแปลง (pattern `expr.Fib` มีใช้แล้วใน `real_firewall.go:76-78`)

- หลัง DNAT ปลายทางถูกเขียนเป็น `internalIP` → routing ส่งออก LAN → **ผ่าน forward(filter) chain**
  → ต้องมี forward-accept ให้ dst ที่แปลงแล้ว มิฉะนั้นโดน final-drop
- **auto-gen forward-accept:** ตอน apply ให้สร้าง rule ใน forward chain `iif==ext, ip daddr==internalIP,
  <proto> dport==internalPort → accept` คู่กับ DNAT entry (ผู้ใช้ไม่ต้องเพิ่ม policy เอง)
  — วาง **หลัง docker-compat bypass, ก่อน user policy rules** (จุดแทรกจริง = ก่อน
  "User rules in forward" ที่ `real_firewall.go:458`; design decision: entry ที่ enabled
  ต้องทะลุเสมอ ไม่ให้ user DROP rule กว้าง ๆ shadow; อยากปิดให้ปิดที่ตัว entry)
  — ใส่ `expr.Counter{}` ทุก rule (ดู hit count ผ่าน `nft list` ได้; สอดคล้องเหตุผลที่เลือก
  per-entry accept แทน `ct status dnat`); v1 ไม่ใส่ NFLOG log ใน auto-accept
  (ไม่มี field `Log` ใน model — เพิ่มทีหลังได้ตาม pattern `forwardLogExpr`)
- **ไม่ติด fwmark 0x1** บน auto-accept → ไม่โดน masquerade ของ policy-SNAT (PR #59) →
  internal server เห็น source IP จริงของ client ภายนอก (ตามที่ควรเป็นสำหรับ DNAT;
  และเพราะ auto-accept มาก่อน user rules เสมอ NAT toggle ของ user policy จะไม่ทับ flow นี้)
- return: conntrack un-DNAT ให้เอง (PiGate เป็น gateway ขากลับวิ่งผ่านอยู่แล้ว)

**การส่งข้อมูลเข้า kernel:** ขยาย `ApplyRules` ให้รับ `[]PortForward` เพิ่ม (แก้ interface + real +
mock) — เลือกทางนี้แทน method แยกเพราะ DNAT/forward-accept ต้อง build ในรอบ apply เดียวกับ nat table
(flush ครั้งเดียว) ป้องกัน table ครึ่ง ๆ

**ทางเลือกที่ตัดทิ้ง:**
1. *method `ApplyPortForwards` แยก* — ต้อง flush/สร้าง pigate_nat ซ้ำ เสี่ยง race กับ SNAT rules; รวมใน ApplyRules เดียวสะอาดกว่า
2. *ให้ผู้ใช้เพิ่ม forward-accept เอง* — กับดักคลาสสิก (DNAT ถูกแต่ทะลุไม่ได้); auto-gen ดีกว่า
3. *ทำ DNAT เป็น toggle บน firewall rule (เหมือน SNAT)* — DNAT เป็น mapping ขาเข้า คนละ concept
   กับ match+action; แยกเป็น resource ชัดกว่า (ตรง FortiGate VIP)
4. *`ct status dnat accept` rule เดียวใน forward แทน per-entry accept* — สั้นกว่า แต่ accept
   flow ที่ถูก DNAT จาก**ทุกแหล่ง** (รวม docker published ports) และไม่มี counter/log ต่อ entry;
   per-entry accept แคบกว่าและ debug ง่ายกว่า

**Pattern:** model/db/repo/handler ตาม `firewall_policies`; kernel ตาม NAT block ใน `real_firewall.go`

## 3. Steps (ชั้นในสุด → นอก)

### Step 1 — model
**File:** `backend/internal/model/types.go` — struct `PortForward` + `PortForwardInput`
(`ID,Name,InInterface,ExternalPort,Protocol,InternalIP,InternalPort,Status`)
- `ExternalPort`: single (`"8080"`) หรือ range (`"8000-8010"`); range ใช้ได้เฉพาะแบบ
  keep-port (`InternalPort` ว่าง) — ดู Caution 9

### Step 2 — kernel interface + real + mock
**File:** `kernel/interfaces.go`, `real_firewall.go`, `mock.go`
- ขยาย `ApplyRules(...)` เพิ่มพารามิเตอร์ `portForwards []model.PortForward`
- real: เพิ่ม prerouting chain + DNAT rules (มี `fib daddr type local` — §2) + auto-gen
  forward-accept (วางก่อน user rules, ก่อน final drop — §2)
- mock: อัปเดต signature + log-only

### Step 3 — DB
**File:** `db/connection.go` — `CREATE TABLE port_forwards (...)`; `db/repository.go` — CRUD
(Get/Create/Update/Delete/GetByID)

### Step 4 — service
**File:** `service/firewall.go:118` — ดึง port-forwards ส่งเข้า `ApplyRules`

### Step 5 — handler + route
**File:** `api/handlers.go` + `api/router.go` — `GET/POST/PUT/DELETE /api/port-forwards`
(**path แบบ flat** ตาม convention จริงของ router เช่น `/api/policies` — ไม่ใช้
`/api/firewall/...` prefix; authRoute; mutation โดน RoleReadOnly + DisableEdit)

### Step 6 — backup/restore
**File:** `model/backup.go` + `service/backup.go` — เพิ่ม `PortForwards []PortForward`
ใน `BackupConfig` โดย **ต้องใช้ tag `json:"portForwards,omitempty"`** (ตาม pattern field
`Users` — ดู Caution 8, ไม่ต้อง bump schema version) + export/`RestoreConfig`

### Step 7 — OpenAPI (สองไฟล์) + frontend
**File:** `docs/openapi.yaml`(+public), `pages/PortForwarding.tsx` (ใหม่, ตาม FirewallPolicy),
`services/portForwardService.ts`, เพิ่มเมนูใน `components/app-sidebar.tsx` (กลุ่มเดียวกับ
Firewall Policy)

> **ไม่ต้องทำ:** netlink_monitor ไม่เกี่ยว (NAT ไม่ใช่ route/interface event); ไม่มี boot-apply
> แยก (ไหลผ่าน firewall apply เดิม)

## 4. Related API

| Method | Path | Role | หมายเหตุ |
|---|---|---|---|
| GET | `/api/port-forwards` | authRoute | list |
| POST/PUT/DELETE | `/api/port-forwards[/{id}]` | authRoute (RoleReadOnly + `-disable-edit` บล็อก mutation) | route ใหม่ทั้งหมด — path flat ตาม convention `/api/policies` |

## 5. Cautions

1. **[กับดักคลาสสิก] DNAT ถูกแต่ firewall drop** — หลัง DNAT dst=internal → ต้องผ่าน forward chain.
   *เกิดอะไร:* ไม่มี forward-accept = port-forward "ดูเหมือนตั้งถูก" แต่ยิงไม่ทะลุ. *กัน:* auto-gen
   forward-accept คู่ทุก entry (Step 2), วาง**ก่อน user policy rules** (กัน user DROP กว้าง ๆ
   shadow — ดู §2) และก่อน final-drop-log ของ forward chain
2. **firewall 4-section order** — forward-accept ที่ auto-gen ต้องอยู่ใน section dynamic-accept
   ไม่ทำลายลำดับ sanity→log→accept→drop (CLAUDE.md)
3. **prerouting priority** — ใช้ `ChainPriorityNATDest` (DNAT ก่อน routing); ถ้าใส่ priority ผิดจะ
   DNAT ไม่ทันก่อนตัดสิน route
4. **input validation → nft args** — `InternalIP`/port/proto เข้าไปสร้าง nft rule ผ่าน netlink
   (ไม่ใช่ string exec) แต่ต้อง validate: IP ถูก, port 1-65535, proto ∈ {tcp,udp} กัน rule พัง/สับสน
   (บทเรียน dnsmasq-input-validation)
5. **ต่อยอด NAT table ของ Plan 1 (merged แล้ว — PR #59)** — prerouting chain ต้อง add ใน
   **รอบ apply/flush เดียวกัน** กับ postrouting เดิม (`real_firewall.go:535-557` ทำ
   `FlushTable(natTable)` แล้วสร้างใหม่ทุกครั้ง) — ห้ามแยก method ที่ flush ซ้ำ ไม่งั้น
   DNAT/SNAT ลบกันเอง
6. **conntrack return path** — ทำงานเมื่อขากลับผ่าน PiGate (เป็น gateway). ถ้า internal host มี
   gateway อื่น → return ไม่ถูก un-DNAT (จดใน docs; นอกเหนือการควบคุมของ PiGate)
7. **hairpin ไม่รองรับใน v1** — internal client เรียก external addr จะไม่กลับเข้ามา (ต้อง SNAT เพิ่ม)
   → จดใน docs/UI ว่าให้ใช้ internal IP ตรงจาก LAN
8. **Backup checksum ของไฟล์เก่าต้องไม่พัง** — importer (`service/backup.go:559-568`)
   ตรวจ checksum โดย re-marshal `BackupConfig` ที่ decode แล้ว; ถ้าเพิ่ม field ใหม่แบบ
   ไม่มี `omitempty` ไฟล์ v2 เก่า (ไม่มี key) จะถูก marshal เพิ่ม `"portForwards":null`
   → checksum mismatch → import ถูกปฏิเสธทั้งไฟล์. *กัน:* ใช้
   `json:"portForwards,omitempty"` (nil/empty slice ถูก omit เหมือนเดิม — pattern เดียวกับ
   field `Users` ที่ `model/backup.go:69`); ไม่ต้อง bump `CurrentBackupSchemaVersion`
9. **port range กับ google/nftables** — DNAT พร้อมแปลง port range 1:1 (`8000-8010 → 9000-9010`)
   สร้างด้วย expr ตรง ๆ ไม่ได้ (ต้องใช้ map). *กัน:* v1 รองรับ 2 แบบเท่านั้น —
   (a) single port → `dnat to internalIP:internalPort`; (b) range แบบ keep-port
   (`InternalPort` ว่าง) → `dnat to internalIP` ไม่ระบุ port (conntrack คง port เดิม).
   validate: range + InternalPort ไม่ว่าง = reject
10. **entry ชนกัน** — 2 entry ที่ (InInterface, Protocol, ExternalPort) ทับซ้อนกัน rule แรกชนะ
    เงียบ ๆ สับสน. *กัน:* validate uniqueness/overlap ตอน create/update (รวมเคส range คร่อม
    single port) — reject พร้อม error ชี้ entry ที่ชน

## 6. Summary Checklist (Definition of Done)

- [ ] `model/types.go` — `PortForward` + `PortForwardInput` + validation helper
      (IP/port/proto + range↔InternalPort + uniqueness/overlap — Caution 4, 9, 10)
- [ ] `kernel/interfaces.go` + `real_firewall.go` + `mock.go` — `ApplyRules` รับ port-forwards;
      real เพิ่ม prerouting DNAT (`fib daddr type local`) + auto forward-accept (ก่อน user rules);
      mock log-only
- [ ] `db/connection.go` — `CREATE TABLE port_forwards`; `db/repository.go` — CRUD
- [ ] `service/firewall.go` — ดึง+ส่ง port-forwards เข้า ApplyRules
- [ ] `api/handlers.go` + `router.go` — 4 endpoint ใหม่
- [ ] `service/backup.go` — รวม PortForwards ใน export/import
- [ ] `go build ./...` + `go test ./...` ผ่าน (test: DNAT rule ถูก + auto forward-accept ถูก + validation)
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — endpoint ใหม่ (sync)
- [ ] frontend: หน้า PortForwarding + service + เมนู; `yarn build`+`yarn lint` ผ่าน
- [ ] ทดสอบเครื่องจริง: มี **LAB** ยิงจากภายนอกเข้า ext-addr:port → เข้าเครื่องใน LAN ได้;
      ปิด entry → เข้าไม่ได้; ตรวจ `nft list table ip pigate_nat` เห็น prerouting DNAT
- [ ] README Feature Status: เพิ่ม Port Forwarding
- [ ] เสร็จแล้วย้ายแผนไป `docs/ref/complete/`
