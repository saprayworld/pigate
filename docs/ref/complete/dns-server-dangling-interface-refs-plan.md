# DNS Server Dangling Interface Refs — แก้ save-deadlock เมื่อ interface ใน settings ไม่มีจริง (Issue 46)

> แผนงานแก้บั๊ก: `dns_server_settings` ที่อ้างถึง interface ซึ่งหายไปจากระบบ
> (เช่น VLAN ที่ถูกลบ/parent หาย) ทำให้ PUT `/api/dns/settings` ตอบ 400 ทุกครั้ง
> และ UI ไม่มีทางถอดชื่อค้างออก (แก้ได้เฉพาะ sqlite3) — เปลี่ยนเป็นโมเดล
> **tolerate dangling refs**: validate เฉพาะชื่อที่*เพิ่มใหม่*, แสดงชื่อค้างใน UI
> พร้อม badge "Missing" ให้ผู้ใช้ติ๊กออกเองได้, และล้าง settings เมื่อผู้ใช้ลบ VLAN
> ผ่าน PiGate เอง — ตาม self-healing design principle (ดู Caution 1)
>
> เขียนเมื่อ: 2026-07-13 · Reference branch: `fix/dns-server-dangling-iface-refs`
> เกี่ยวข้อง: GitHub issue #46 (bug นี้), #48/#49 (enhancement ต่อยอด — นอก scope แผนนี้)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- DB มีชื่อ interface ค้าง (ไม่มีจริงใน kernel) → ผู้ใช้ยังบันทึก Listen Interfaces
  ได้ตามปกติ: **เพิ่ม/ถอด interface อื่นได้ และถอดชื่อค้างออกได้** — ไม่มี 400 deadlock
- ชื่อที่*เพิ่มใหม่*ยังถูก validate เข้มเหมือนเดิม (ชื่อไม่มีจริง = 400) — กัน typo/garbage
- หน้า DNS Server แสดงชื่อค้างเป็น chip พร้อม badge "Missing" (ติ๊กออกได้ ติ๊กกลับเข้าไม่ได้)
- ลบ VLAN ผ่าน PiGate (`DeleteVlanInterface`) → ชื่อนั้นถูกถอดออกจาก
  `dns_server_settings` ให้อัตโนมัติ (explicit user action ≠ auto-heal — ดู Caution 1)

**Out of scope (ตัดชัด):**
- Internal event bus + startup reconciliation → issue #48
- หน้าจัดการ offline interfaces ใน Interfaces page (show-hidden toggle) → issue #49
- เก็บกวาด `dhcp_configs` ที่อ้าง interface ที่ถูกลบ — DHCP config เป็น intent ก้อนใหญ่
  (pool/range/lease) ต่างจาก list ชื่อเฉย ๆ และ generation path ข้ามให้อยู่แล้ว
  (`kernel/dhcp_server.go:79`) — ถ้าจะทำควรเป็นส่วนหนึ่งของ issue #49
- การเปลี่ยนแปลงใด ๆ ใน kernel layer (ดู §1 — ทั้ง dnsmasq config gen และ nftables
  ทน dangling refs ได้อยู่แล้ว)

## 1. Current State (สำรวจโค้ดจริง 2026-07-13)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Handler validation | **บั๊ก — all-or-nothing**: ทุกชื่อใน payload ต้องมีจริงใน kernel มิฉะนั้น 400 ทั้ง request | `backend/internal/api/handlers.go:~2631-2661` (`HandleUpdateDNSServerSettings`; loop `valid[name]` `:~2647-2652`) |
| แหล่ง valid names | `GetDataLayerInterface` วนจาก **kernel links เท่านั้น** — แถว DB ที่ไม่มี kernel link ไม่โผล่ | `backend/internal/service/interface.go:~331-409` |
| Frontend checkbox list | สร้างจาก interface จริง filter `role === "LAN"` → ชื่อค้าง**ไม่มี checkbox ให้ติ๊กออก** | `frontend/src/pages/DnsServer.tsx:~105` (filter), render `:~462-489` |
| Frontend save | `handleToggleInterface` ส่ง**ทั้ง list** `selectedInterfaces` (รวมชื่อค้างที่โหลดมา) ทุกครั้ง → 400 ตลอด = deadlock | `DnsServer.tsx:~118-133` |
| ลบ VLAN | ลบ kernel link + แถว `interfaces` เท่านั้น — **ไม่ล้าง** `dns_server_settings` | `backend/internal/service/interface.go:~715-730` (`DeleteVlanInterface`) |
| Storage | ตาราง `dns_server_settings` แถวเดียว (id=1) เก็บ comma-joined string; Get/Set มีแล้ว | `backend/internal/db/repository.go:~2406-2423`, schema `connection.go:~332` |
| dnsmasq config gen | **ทนอยู่แล้ว** — `ApplyZones` รับ interfaces แต่ไม่ emit `interface=` ลง config เลย | `backend/internal/kernel/dns_server.go:~35` |
| nftables (port 53 rule) | **ทนอยู่แล้ว** — rule match `iifname` ชื่อไม่มีจริง = ไม่ match อะไร ไม่ error | `backend/internal/kernel/real_firewall.go:~987-1017` |
| Backup/Restore | Export รวม settings แล้ว; **import เขียนตรงไม่ validate** = ทนอยู่แล้ว (ถูกต้อง — restore ลงเครื่องที่ interface ต่างชุดต้องไม่ล้ม) | `service/backup.go:~109-113`, `db/backup_repo.go:~228-230` |
| Route/role | `PUT /api/dns/settings` เป็น `authRoute` (mutation ถูก `RoleReadOnlyMiddleware` + `DisableEditMiddleware` คุมแล้ว) — ไม่แตะ | `backend/internal/api/router.go:~127-128` |
| OpenAPI | PUT description เขียนว่า "unknown names are rejected" — ต้องอัปเดต semantics ใหม่ | `docs/openapi.yaml:~1893-1900` + `frontend/public/openapi.yaml` (sync กัน) |
| Tests | ยังไม่มี test แตะ `/dns/settings`; มี `TestDeleteVlanInterface` เป็น template ฝั่ง service | `api/handlers_test.go`, `service/interface_test.go:596` |

**สรุป:** งานจริงกระจุกอยู่ 3 จุดเล็ก ๆ — handler validation (backend), chip render +
missing badge (frontend), cleanup ใน `DeleteVlanInterface` — kernel/DB/backup ไม่ต้องแตะเลย

## 2. Technical Approach

**หลัก: validate เฉพาะ delta ที่เพิ่มใหม่** — handler โหลดชุดที่บันทึกไว้เดิมจาก
`repo.GetDNSServerInterfaces()` แล้วผ่อนเงื่อนไข: ชื่อที่อยู่ในชุดเดิมผ่านเสมอ
(ให้ "คงไว้/ถอดออก" ได้ทุกกรณี), ชื่อที่ไม่อยู่ในชุดเดิม (= เพิ่มใหม่) ต้องมีจริงใน kernel

```go
saved, err := s.repo.GetDNSServerInterfaces()      // ชุดที่บันทึกไว้เดิม
// ... build savedSet ...
for _, name := range input.Interfaces {
    if !valid[name] && !savedSet[name] {           // ใหม่ + ไม่มีจริง เท่านั้นที่ reject
        s.writeError(w, http.StatusBadRequest, ...)
        return
    }
}
```

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
- *Filter ชื่อค้างทิ้งเงียบ ๆ ฝั่ง backend* — config ผู้ใช้หายโดยไม่ได้ตัดสินใจ
  ขัด self-healing principle (ผู้ใช้ต้องรับรู้และเลือกเอง) และ interface ที่หายชั่วคราว
  (USB NIC/ไฟดับ) จะถูกลบถาวรทันทีที่มีใครกด save อย่างอื่น
- *ตัด validation ทิ้งทั้งหมด* — API client ยิงชื่อมั่ว/typo เข้า DB ได้ เสีย defense เดิมฟรี ๆ
- *Auto-delete ชื่อค้างตอน startup หรือใน netlink monitor* — ขัด principle ตรง ๆ
  (monitor/event ห้ามลบ user config — ดู Caution 1)

**Frontend:** คำนวณ `missingInterfaces = selectedInterfaces` ที่ไม่อยู่ใน
`availableInterfaces` แล้ว render เป็น chip แบบเดียวกับของจริง แต่ style เป็น
`border-destructive/40 bg-destructive/10` + shadcn `Badge` "Missing" — ติ๊กออกแล้ว
หายจาก list (ติ๊กกลับเข้าไม่ได้เพราะไม่อยู่ใน available) ใช้ semantic colors เท่านั้น
ตาม `docs/rules_of_work.md` (template: chip label เดิมที่ `DnsServer.tsx:~466-487`)

**Cleanup ตอนลบ VLAN:** ใน `DeleteVlanInterface` หลัง `repo.DeleteInterface` สำเร็จ →
Get/filter/Set `dns_server_settings` (ใช้ repo method เดิมสองตัว ไม่ต้องเพิ่ม SQL ใหม่)

## 3. Steps (เรียงชั้นใน → นอก)

### Step 1 — Backend: ผ่อน validation ใน handler
**File:** `backend/internal/api/handlers.go:~2631-2661` (`HandleUpdateDNSServerSettings`)
- โหลด `saved` จาก `s.repo.GetDNSServerInterfaces()` ก่อน loop validate
- เงื่อนไข reject ใหม่: `!valid[name] && !savedSet[name]` (ดู §2)
- อัปเดต comment หัว handler ให้ตรง semantics ใหม่ (ชื่อเดิมที่บันทึกไว้ grandfather ผ่าน)

> ไม่ต้องแตะ `SetDNSServerInterfaces`/schema — รูปแบบเก็บเดิมรองรับอยู่แล้ว
> และไม่ต้องแตะ `service/dns_server.go` — `ApplyZones` ไม่ใช้ interfaces ลง config

### Step 2 — Backend: ล้าง settings เมื่อลบ VLAN ผ่าน PiGate
**File:** `backend/internal/service/interface.go:~715-730` (`DeleteVlanInterface`)
- หลัง `s.repo.DeleteInterface(id)` สำเร็จ: `GetDNSServerInterfaces` → filter ชื่อที่ลบ →
  `SetDNSServerInterfaces` — ถ้าขั้นนี้ error ให้ log warning แต่**ไม่ fail ทั้ง request**
  (interface ลบไปแล้วจริง; ชื่อค้างที่เหลือระบบทนได้ตาม Step 1 อยู่ดี)

> ไม่ต้อง trigger firewall re-sync ตรงนี้ — nft rule port 53 ของ iface ที่หายเป็น no-op
> และจะถูก regenerate ในการ sync ครั้งถัดไปตามปกติ

### Step 3 — Backend: tests
**File:** `backend/internal/api/handlers_test.go` (ตาม pattern test เดิมในไฟล์) และ
`backend/internal/service/interface_test.go:~596` (ต่อจาก `TestDeleteVlanInterface`)
- Handler: (a) ชุดเดิมมีชื่อค้าง → PUT ที่คงชื่อค้างไว้ = 200, (b) PUT ถอดชื่อค้างออก = 200,
  (c) PUT เพิ่มชื่อใหม่ที่ไม่มีจริง = 400, (d) เพิ่มชื่อจริง = 200
- Service: ลบ VLAN แล้วชื่อหายจาก `dns_server_settings` (seed settings ก่อนลบ)

### Step 4 — Frontend: missing chip + badge
**File:** `frontend/src/pages/DnsServer.tsx:~462-489` (CardContent ของ Listen Interfaces)
- `const missing = selectedInterfaces.filter(n => !availableInterfaces.some(i => i.name === n))`
  (useMemo) — render ต่อท้าย chips ปกติ: checkbox checked + label ชื่อ + `<Badge>`
  "Missing" ด้วย semantic destructive colors (ดู §2); `onChange` ใช้
  `handleToggleInterface(name, false)` เดิมได้เลย
- เงื่อนไข empty-state เดิม (`availableInterfaces.length === 0`) ต้องไม่กลบ missing chips
  — เปลี่ยนเป็นเช็ค `availableInterfaces.length === 0 && missing.length === 0`

> ไม่ต้องแก้ `dnsServerService.ts` — payload/endpoint เดิม; mock mode (localStorage)
> ใช้ logic เดียวกันได้เพราะ missing คำนวณฝั่ง client จากข้อมูลสอง endpoint เดิม

### Step 5 — Docs
**Files:** `docs/openapi.yaml:~1893-1900` + `frontend/public/openapi.yaml` (จุดเดียวกัน — sync ทั้งคู่)
- แก้ description ของ PUT `/dns/settings`: ชื่อที่บันทึกไว้เดิมได้รับการยอมรับเสมอ
  (tolerate dangling refs); เฉพาะชื่อที่เพิ่มใหม่ถูก validate กับ interface จริง

## 4. Related API

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/dns/settings` | authRoute (ทุก role อ่านได้) | เดิม — คืน list รวมชื่อค้าง (frontend ใช้แยก missing เอง) |
| PUT | `/api/dns/settings` | authRoute (mutation → super_admin เท่านั้นผ่าน `RoleReadOnlyMiddleware`) | **semantics เปลี่ยน**: reject เฉพาะชื่อเพิ่มใหม่ที่ไม่มีจริง |

Route เดิมทั้งคู่ ไม่มี route ใหม่; `-disable-edit=true` block PUT อยู่แล้วผ่าน
`DisableEditMiddleware` — พฤติกรรมนี้ถูกต้องสำหรับงานนี้ ไม่ต้องแก้

## 5. Cautions

1. **ห้ามเปลี่ยนงานนี้เป็น auto-delete** — self-healing principle ของโปรเจกต์:
   monitor/event/startup ห้ามลบ user config อัตโนมัติ; ที่ Step 2 ลบได้เพราะเป็น
   *explicit user action* (ผู้ใช้สั่งลบ VLAN เอง) ไม่ใช่ระบบตัดสินใจแทน ถ้ามีใครเสนอ
   "ล้างชื่อค้างตอน boot ให้เลย" ระหว่างทำงาน → ปฏิเสธ แล้วชี้ไป issue #48/#49
2. **ลำดับใน handler สำคัญ**: ต้องโหลด `saved` **ก่อน** `SetDNSServerInterfaces`
   เขียนทับ — ถ้าเผลอโหลดหลังเขียน grandfather set จะกลายเป็น input เอง = validation
   เป็นรูพรุน (ทุกชื่อผ่านหมด)
3. **Empty-state ฝั่ง frontend กลบ missing chips ได้** (Step 4): เคส interface LAN
   หายหมดแต่มีชื่อค้าง — ถ้าไม่แก้เงื่อนไข ผู้ใช้จะเห็น "ไม่พบ Interface..." แล้วถอดชื่อค้าง
   ไม่ได้เหมือนเดิม ทั้งที่ backend ปลดล็อกแล้ว
4. **Role filter ก็ทำให้ "missing" ได้เหมือน link หาย**: interface ที่ยังอยู่แต่ถูกเปลี่ยน
   role เป็น WAN จะหลุดจาก `availableInterfaces` (filter LAN) → โผล่เป็น missing chip
   — พฤติกรรมนี้*ยอมรับได้* (ผู้ใช้เห็นและถอดออกได้ ตรง intent) แต่ backend validate
   ด้วย kernel ทั้งหมดไม่ใช่แค่ LAN → ติ๊กชื่อ WAN กลับเข้าไม่ได้จาก UI แต่ API ทำได้
   — คงไว้ตามเดิม ไม่ขยาย scope ไป enforce role ที่ backend ในงานนี้
5. **อย่า validate ใน import/restore path** — `db/backup_repo.go:~228` เขียนตรง
   โดยไม่เช็ค ซึ่ง*ถูกต้องแล้ว* (restore ข้ามเครื่อง/ข้ามชุด interface ต้องสำเร็จเสมอ
   แล้วให้ UI แสดง missing) — ห้าม "แก้ให้ครบ" โดยเพิ่ม validation ตรงนั้น
6. **ทดสอบบนเครื่องจริงปลอดภัย**: ฟีเจอร์นี้ไม่แตะ routing/firewall structure — เสี่ยง
   ต่ำเรื่อง lock-out; ทดสอบ mock mode ได้เกือบหมด (สร้าง VLAN → เลือกใน DNS Server
   → ลบ VLAN → ดู chip/ถอดออก) เหลือแค่ยืนยันบนบอร์ดจริงว่า dnsmasq/nftables
   ไม่สะดุดเมื่อ settings มีชื่อค้าง (ตาม §1 ควรเฉย ๆ อยู่แล้ว)

## 6. Summary Checklist (Definition of Done)

- [ ] `api/handlers.go` — grandfather validation ใน `HandleUpdateDNSServerSettings`
- [ ] `service/interface.go` — `DeleteVlanInterface` ถอดชื่อออกจาก `dns_server_settings`
- [ ] `api/handlers_test.go` — 4 เคส validate ใหม่ (คงชื่อค้าง/ถอด/เพิ่มปลอม/เพิ่มจริง)
- [ ] `service/interface_test.go` — ลบ VLAN แล้ว settings ถูกล้าง
- [ ] `pages/DnsServer.tsx` — missing chips + Badge (semantic colors) + แก้เงื่อนไข empty-state
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — อัปเดต PUT description (sync ทั้งคู่)
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] `yarn build` + `yarn lint` ผ่าน (ใน `frontend/`)
- [ ] ทดสอบ mock mode: seed ชื่อค้าง (sqlite3 หรือสร้าง VLAN แล้วลบนอก UI) → เห็น chip
      Missing → ติ๊กออก → save ผ่าน; เพิ่มชื่อจริงอื่นได้ระหว่างที่ยังมีชื่อค้าง
- [ ] ทดสอบ role: user read-only PUT ไม่ผ่าน (พฤติกรรมเดิม), `-disable-edit` block PUT
- [ ] ทดสอบบอร์ดจริง: settings มีชื่อค้าง → dnsmasq + firewall sync ทำงานปกติ
- [ ] เสร็จแล้วย้ายไฟล์นี้ไป `docs/ref/complete/` + ปิด issue #46
