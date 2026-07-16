# QoS IFB Ingress Capability — เตือนที่ UI + skip ingress เมื่อ kernel ไม่มี IFB module

> แผนงานสำหรับ issue #53: ตรวจว่า kernel รองรับ IFB module หรือไม่ (probe ครั้งเดียวตอน startup)
> แล้ว expose สถานะเป็น field `ingressSupported` ใน QoS status API เพื่อให้หน้า QoS
> แสดง banner เตือน + disable ช่องกรอก ingress rate เมื่อไม่รองรับ — คงพฤติกรรม fail-safe
> (apply egress ต่อ, skip ingress) ตามที่ commit `152a127` ทำไว้แล้ว
>
> เขียน: 2026-07-16 · Reference branch: `feat/qos-ifb-ingress-capability`
> Status ใน README Feature Status: QoS = Completed (ไม่เปลี่ยน — งานนี้เป็น enhancement)

## 0. Goal และ Scope

**Goal (พฤติกรรมที่ผู้ใช้เห็น):**
- บอร์ดที่ kernel ไม่มี `ifb` module: เปิดหน้า QoS แล้วเห็น banner เตือนว่า
  *"kernel นี้ไม่มี IFB module — รองรับเฉพาะ QoS ขา egress เท่านั้น, ingress rule จะไม่ถูก apply"*
  และช่องกรอก Ingress Rate/Ceil ใน dialog ถูก disable พร้อมข้อความอธิบาย
- Backend รู้สถานะนี้จากการ probe จริงครั้งเดียวตอน startup ไม่ใช่เดาจาก log
- พฤติกรรม apply คงเดิมแบบ fail-safe: sync ไม่ล้ม, egress apply ปกติ, ingress ถูก skip + log

**เงื่อนไขเชิงเทคนิค:**
- ตรวจ capability ด้วย netlink ล้วน ๆ (ไม่มี exec.Command เพิ่ม — และถือโอกาส **ลบ**
  `execCommand("modprobe", "ifb")` เดิมที่เป็น dead code ออก, ดู Caution 4)
- ไม่แก้ interface `QosManager` (ไม่ต้อง — ดู Step 2)

**Out of scope:**
- Event log entry (`qos.ingress_unsupported`) ลง EventLogs — สื่อสารผ่าน banner ที่หน้า QoS พอ
  (kernel layer ไม่มีสิทธิ์เรียก logEvent ของ api layer; ถ้าอยากได้ค่อยทำเป็น issue แยก)
- การ auto-install/แนะนำติดตั้ง module จาก UI
- endpoint `/api/qos/capabilities` แยกต่างหาก (ถูก reject — ดู §2)
- mock mode จำลองสถานะ "ไม่รองรับ" ได้จาก UI (ทดสอบ banner ด้วยการ hardcode ชั่วคราวระหว่าง dev พอ)

## 1. Current State (สำรวจโค้ดจริง ณ วันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `model.QosIfaceStatus` | มีแล้ว แต่ไม่มี field capability | `backend/internal/model/types.go:~549` (Interface/HasQdisc/Classes) |
| `QosManager` interface | มี 3 method (Apply/Clear/GetIfaceQosStatus) ไม่มี capability | `backend/internal/kernel/interfaces.go:84-96` |
| Real: skip ingress เมื่อ IFB ไม่มี | ทำแล้ว (หลัง `152a127`) — LinkByName fail → log warning + `continue` | `backend/internal/kernel/real_qos.go:145-149` |
| Real: `modprobe ifb` | **dead code** — `execCommand("modprobe","ifb")` fail เสมอตอน runtime เพราะ binary ไม่มี CAP_SYS_MODULE (install.sh ยืนยันใน comment) | `real_qos.go:125`, `install.sh:~483` |
| Real: GetIfaceQosStatus | อ่าน qdisc/class จาก physical + IFB link, ไม่รายงาน capability | `real_qos.go:266-338` |
| Mock: GetIfaceQosStatus | คืน `HasQdisc:false, Classes:[]` | `backend/internal/kernel/mock.go:316-324` |
| Service | `GetIfaceStatus` เป็น passthrough ไป kernel | `backend/internal/service/qos.go:120-122` |
| Handler + Route | `GET /api/qos/status/{iface}` เป็น `authRoute` มีอยู่แล้ว | `backend/internal/api/handlers.go:~2404`, `router.go:161` |
| Wiring | `NewRealQos()` ที่ `main.go:124` — **ก่อน** `netlinkMonitor.Start` ที่ `main.go:357` | `backend/cmd/pigate/main.go` |
| install.sh | preload `ifb` ผ่าน `/etc/modules-load.d/pigate.conf` + modprobe ตอน install แล้ว | `install.sh:479-500` — **ไม่ต้องแก้** |
| Frontend type + mock | `QosIfaceStatus` ไม่มี `ingressSupported`; mock branch สร้าง status เอง | `frontend/src/services/qosService.ts:25-29, 179-208` |
| Frontend หน้า QoS | โหลด status ทุก interface อยู่แล้ว (`getIfaceStatus` ใน loadData/initialLoad) แต่ไม่มี banner; ช่อง Ingress Rate/Ceil ใน dialog ที่ `~785` | `frontend/src/pages/QoS.tsx:89-101, 120-132, 70-71, ~785` |
| Alert component | มีแค่ variant `default` / `destructive` — ไม่มี `warning` | `frontend/src/components/ui/alert.tsx:9-13` |
| openapi (ทั้ง 2 ไฟล์) | schema `QosIfaceStatus` ยังไม่มี field ใหม่ | `docs/openapi.yaml:4220`, `frontend/public/openapi.yaml:4220` |
| docs/ref/qos-system.md | ตรวจแล้ว — **ไม่มี** เนื้อหา ingress/IFB เลย | grep ifb/ingress = 0 match |

สรุป: โครง fail-safe ฝั่ง apply เสร็จแล้วจาก `152a127` — งานจริงกระจุกอยู่ที่ (1) probe + cache
capability ใน `RealQos`, (2) เพิ่ม field ใน model/status/openapi, (3) banner + disable input ฝั่ง QoS.tsx

## 2. Technical Approach

**การตรวจ (probe):** ใน `NewRealQos()` ทำ probe ครั้งเดียวแล้ว cache ผลใน struct:

```go
type RealQos struct{ ingressSupported bool }

func NewRealQos() *RealQos {
    q := &RealQos{}
    la := netlink.NewLinkAttrs()
    la.Name = "pigate-ifb0" // ≤15 ตัวอักษร (IFNAMSIZ) และไม่ชนแพทเทิร์น "ifb-<iface>"
    probe := &netlink.Ifb{LinkAttrs: la}
    err := netlink.LinkAdd(probe)
    if err == nil || errors.Is(err, os.ErrExist) {
        q.ingressSupported = true
        if l, e := netlink.LinkByName(la.Name); e == nil { _ = netlink.LinkDel(l) }
    } else {
        log.Printf("[RealQos] IFB module unavailable, ingress shaping disabled: %v", err)
    }
    return q
}
```

- `netlink.LinkAdd` ของ link kind `ifb` จะทำให้ kernel เรียก `request_module("rtnl-link-ifb")`
  ให้เอง (ฝั่ง kernel รันเป็น root ไม่ต้องมี CAP_SYS_MODULE ฝั่ง caller) — probe จึงเป็นตัวชี้วัด
  ที่ตรงความจริงที่สุด: สำเร็จ = ingress ใช้ได้จริง
- **ทางเลือกที่ reject:**
  - *stat `/sys/module/ifb`* — false negative เมื่อ `ifb.ko` มีบนดิสก์แต่ยังไม่ load
    (LinkAdd จะ auto-load ได้อยู่ดี) → รายงาน "ไม่รองรับ" ทั้งที่ใช้ได้
  - *lazy probe (sync.Once ตอนถูกเรียกครั้งแรก)* — จะสร้าง/ลบ link ระหว่างที่
    `netlink_monitor` รันอยู่ → ยิง `InterfaceAdded` ปลอมเข้า self-healing bus
    (probe ตอน construct ที่ `main.go:124` เกิดก่อน monitor start ที่ `:357` จึงไม่มีปัญหานี้)
  - *endpoint `GET /api/qos/capabilities` แยก* — ต้องเพิ่ม route + handler + openapi path
    เพื่อ bool ตัวเดียว ทั้งที่ frontend โหลด `getIfaceStatus` ทุก interface อยู่แล้ว → piggyback
    บน `QosIfaceStatus` ดีกว่า

**การ expose:** เพิ่ม `IngressSupported bool \`json:"ingressSupported"\`` ใน `model.QosIfaceStatus`
— `RealQos.GetIfaceQosStatus` ใส่ค่าจาก cache, `MockQos` ใส่ `true` ตายตัว
ไม่ต้องแตะ interface/service/handler เลยเพราะเป็นแค่ field ใหม่ใน struct ที่ไหลผ่าน passthrough เดิม

**Pattern ต้นแบบ:** สไตล์ skip+log tolerance ที่มีอยู่แล้วใน `real_qos.go:62-71` และ `:140-149`

## 3. Steps (เรียงจาก layer ในสุดออกนอก)

### Step 1 — เพิ่ม field ใน model
**File:** `backend/internal/model/types.go` (~549)
เพิ่ม `IngressSupported bool \`json:"ingressSupported"\`` ใน `QosIfaceStatus`

### Step 2 — RealQos: probe + ใช้ cache + ลบ modprobe
**File:** `backend/internal/kernel/real_qos.go`
- `:21-25` — เพิ่ม field `ingressSupported bool` + probe ใน `NewRealQos()` (โค้ดตาม §2)
- `:122-127` — **ลบ** `execCommand("modprobe", "ifb")` ทิ้ง แล้วเปลี่ยนหัว section 4 เป็น
  guard: `if len(enabledIngressRules) > 0 && !q.ingressSupported { log warning; ข้าม section }`
  (ทางลัดก่อนถึง LinkAdd/LinkByName — เส้นทาง skip เดิมที่ `:145-149` คงไว้เป็น safety net)
- `:266+` — `GetIfaceQosStatus` ใส่ `IngressSupported: q.ingressSupported` ตอนสร้าง status

> **ไม่ต้อง** เพิ่ม method ใหม่ใน `interfaces.go` — capability ไหลออกทาง `QosIfaceStatus`
> อยู่แล้ว การไม่แตะ interface ทำให้ fake QosManager ใน test เดิม (`service/qos_test.go`)
> ไม่ต้องแก้ตาม

### Step 3 — MockQos
**File:** `backend/internal/kernel/mock.go` (~316)
`GetIfaceQosStatus` เพิ่ม `IngressSupported: true` (mock = dev workstation ให้ถือว่ารองรับ)

> **ไม่ต้อง** แก้ `service/qos.go`, `handlers.go`, `router.go`, `main.go`, `install.sh` —
> passthrough เดิมพา field ใหม่ออกไปเอง และ install.sh preload ifb ครบแล้ว (`:479-500`)

### Step 4 — openapi ทั้งสองไฟล์
**Files:** `docs/openapi.yaml:4220` และ `frontend/public/openapi.yaml:4220` (ต้อง sync กัน)
เพิ่ม property `ingressSupported` (type boolean + description ว่าหมายถึง kernel มี IFB module)
ใน schema `QosIfaceStatus`

### Step 5 — Frontend service
**File:** `frontend/src/services/qosService.ts`
- `:25-29` — เพิ่ม `ingressSupported: boolean` ใน interface `QosIfaceStatus`
- `:203-207` — mock branch ของ `getIfaceStatus` ใส่ `ingressSupported: true`

### Step 6 — Frontend หน้า QoS: banner + disable input
**File:** `frontend/src/pages/QoS.tsx`
- derive จาก state เดิม: `const ingressUnsupported = Object.values(ifaceStatuses).some(s => s.ingressSupported === false)`
  (ใช้ `=== false` เพื่อไม่ให้ status ที่ fetch ไม่สำเร็จ/ยังไม่โหลดกลายเป็น false positive)
- Banner ใต้ header ของหน้า (~330): ใช้ `<Alert>` variant `default` + สีเตือนผ่าน semantic
  class (`text-warning` — มีใช้แล้วใน `QoS.tsx:387`) — **ห้าม** hardcode palette class;
  ไอคอน `AlertCircle` import ไว้แล้ว (`:9`); ข้อความตามที่ issue ระบุ
- Dialog (~785): ช่อง Ingress Rate/Ceil เพิ่ม `disabled={ingressUnsupported}` + ข้อความ
  หมายเหตุใต้ช่องเมื่อ disable (ค่าเดิมของ rule ที่แก้ไขยังแสดง/ถูกส่งกลับตามเดิม — ดู Caution 6)
- ตาราง rule (optional, ทำท้ายสุด): เซลล์ Ingress Limit แสดงไอคอน/สีเตือนเมื่อ
  `ingressUnsupported && rule.ingressRateMbps > 0` เพื่อชี้ว่า rule นั้น ingress ไม่มีผลจริง

### Step 7 — docs
**File:** `docs/ref/qos-system.md` — เพิ่มหัวข้อ IFB ingress capability: probe ตอน startup,
field `ingressSupported`, พฤติกรรม fail-safe และเงื่อนไข kernel (ตอนนี้ doc ไม่มีเนื้อหา ingress เลย)

## 4. Related API

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/qos/status/{iface}` | `authRoute` (ทุก role อ่านได้) | **route เดิม** — response เพิ่ม field `ingressSupported` (additive, backward compatible) |

- ไม่มี route ใหม่ / ไม่มี mutation ใหม่ → `-disable-edit=true` ไม่เกี่ยว (GET ไม่ถูก block อยู่แล้ว)
- ไม่มี input ใหม่จากผู้ใช้ → ไม่มี validation เพิ่ม

## 5. Cautions

1. **ตำแหน่ง probe ต้องอยู่ใน `NewRealQos()` เท่านั้น** — ถ้าย้ายไป lazy (ตอน request แรก)
   การสร้าง/ลบ probe link จะเกิดระหว่าง `netlink_monitor` รัน → publish `InterfaceAdded` ปลอม
   เข้า self-healing bus (`netlink_monitor.go:156`) ให้ subscriber ทำ reconcile ฟรี ๆ
   ป้องกัน: probe ที่ construct (`main.go:124`) ซึ่งเกิดก่อน `netlinkMonitor.Start` (`main.go:357`)
2. **ชื่อ probe link จำกัด 15 ตัวอักษร (IFNAMSIZ)** — เช่น `pigate-ifb-probe` ยาว 16 ตัว
   จะ fail ด้วย error คนละความหมายกับ "ไม่มี module" → รายงาน capability ผิด
   ป้องกัน: ใช้ `pigate-ifb0` (11 ตัว) และต้องไม่ใช้แพทเทิร์น `ifb-<iface>` เพราะ
   `ClearQosRules` (`real_qos.go:252`) ลบ link ตามแพทเทิร์นนั้น
3. **probe LinkAdd อาจได้ EEXIST** (link ค้างจาก crash รอบก่อน) — ต้องตีความเป็น "รองรับ"
   แล้วลบทิ้ง ไม่ใช่ "ไม่รองรับ" (ดูโค้ด §2: `errors.Is(err, os.ErrExist)`)
4. **การลบ `modprobe ifb` ปลอดภัย** — มันเป็น dead code: runtime ไม่มี CAP_SYS_MODULE จึง fail
   ทุกครั้งอยู่แล้ว (comment ใน `install.sh:~483` ยืนยัน) การ load จริงมาจาก
   `/etc/modules-load.d/pigate.conf` ตอน boot + kernel auto `request_module` ตอน LinkAdd
   และการลบยังสอดคล้อง constraint "no shell execution" ของโปรเจกต์ด้วย
5. **ห้ามเปลี่ยน semantics ของ fail-safe** — guard ใหม่ใน `ApplyQosRules` ต้อง `log + ข้าม
   ingress section` เท่านั้น ห้าม return error (ไม่งั้น sync ทั้งก้อนล้ม ผิดข้อ 3 ของ issue
   และถอยหลังจาก `152a127`)
6. **Disable input แล้วค่าเดิมต้องไม่หาย** — ตอน edit rule ที่มี ingress > 0 อยู่แล้ว
   `resetForm(rule)` เติมค่าใน state (`QoS.tsx:153-154`) และ input ที่ `disabled` ยังส่งค่า
   จาก state ตอน submit → rule เดิมไม่ถูก reset เป็น 0 เงียบ ๆ — ต้องทดสอบ case นี้จริง
7. **Banner ต้อง derive แบบกันพลาด** — `GET /api/qos/status/{iface}` ตอบ 500 เมื่อ interface
   หาไม่เจอ (`handlers.go:~2404` + `real_qos.go:267-270`) และ frontend แค่ console.error
   (`QoS.tsx:96-98`) → statusMap อาจว่าง/ไม่ครบ ใช้ `some(s => s.ingressSupported === false)`
   เพื่อให้ "ไม่มีข้อมูล" = ไม่เตือน (ไม่ false positive)
8. **สไตล์ banner** — Alert มีแค่ variant `default`/`destructive` (`alert.tsx:9-13`);
   โทนเตือนให้ใช้ semantic vars (`text-warning`, `bg-warning/10`) ห้าม `text-amber-*`,
   ห้าม shadow/backdrop-blur และต้องดูรู้เรื่องทั้ง dark/light
9. **การทดสอบบนบอร์ดจริงไม่เสี่ยง lockout** — QoS ไม่แตะ SSH/firewall/routing แต่เงื่อนไข
   "kernel ไม่มี ifb" จำลองบนบอร์ดที่มี module ไม่ได้ง่าย ๆ (rmmod ไม่พอ เพราะ LinkAdd จะ
   auto-load กลับ) → ทดสอบขา "รองรับ" บนบอร์ดจริง, ขา "ไม่รองรับ" ทดสอบด้วยการ hardcode
   `ingressSupported=false` ชั่วคราว หรือย้าย `ifb.ko` ออกจาก `/lib/modules` ชั่วคราว
   (ต้องย้ายกลับ + `depmod` เสร็จแล้ว)

## 6. Summary Checklist (Definition of Done)

- [ ] `backend/internal/model/types.go` — เพิ่ม `IngressSupported` ใน `QosIfaceStatus`
- [ ] `backend/internal/kernel/real_qos.go` — probe ใน `NewRealQos()` + guard ใน
      `ApplyQosRules` + ลบ `modprobe` + ใส่ field ใน `GetIfaceQosStatus`
- [ ] `backend/internal/kernel/mock.go` — `IngressSupported: true`
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — schema `QosIfaceStatus` (sync ทั้งคู่)
- [ ] `frontend/src/services/qosService.ts` — type + mock branch
- [ ] `frontend/src/pages/QoS.tsx` — banner + disable ingress inputs (+ optional: ไอคอนเตือนในตาราง)
- [ ] `docs/ref/qos-system.md` — หัวข้อ IFB ingress capability
- [ ] Test: `cd backend && go build ./... && go test ./...` ผ่าน
- [ ] Test: `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] Test (mock, workstation): `-mock=true` → status ทุก interface มี `ingressSupported: true`,
      ไม่มี banner, ช่อง ingress ใช้ได้ปกติ
- [ ] Test (จำลองไม่รองรับ): hardcode `false` ชั่วคราว → banner แสดง, ช่อง ingress ถูก disable,
      edit rule เดิมที่มี ingress > 0 แล้ว save → ค่า ingress ไม่ถูกล้างเป็น 0 (Caution 6)
- [ ] Test (บอร์ดจริง มี ifb): boot แล้ว log ไม่มี warning IFB, ingress shaping ยังทำงาน
      (`tc -s qdisc show dev ifb-<iface>` เทียบก่อน/หลัง), ไม่มี link `pigate-ifb0` ค้าง
- [ ] Test (role): login ด้วย role read-only → เห็น banner ได้ (GET ไม่ถูก block)
- [ ] อัปเดตแผนนี้ถ้าหน้างานต่างจากที่เขียน แล้วย้ายไป `docs/ref/complete/` เมื่อเสร็จ
