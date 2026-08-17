# Service Object Popover — hover ชื่อ Service ในหน้า Policy แล้วเห็นรายละเอียด entries

> เอกสารแผนงานสำหรับฟีเจอร์: เดิมคอลัมน์ "Service / Port" ในตาราง Policy และการ์ด "Top Service"
> ใน RuleStatsDrawer แสดงเป็นชื่อ Service Object เปล่า ๆ ผู้ใช้ต้องเปิดหน้า `/policy/services`
> ไปหาเองว่าชื่อนั้นคือ protocol/port อะไรบ้าง ฟีเจอร์นี้ทำ hover popover แบบเดียวกับ
> Address Object popover ที่เพิ่งทำเสร็จ (PR #144) เพื่อให้เห็น entries ทั้งหมดในที่เดียว
>
> วันที่เขียน: 2026-08-17 · Branch อ้างอิง (ที่ใช้สำรวจ): `feat/reference-popover`
> **หมายเหตุ branch:** งานนี้จะ **แตก branch ใหม่จาก `main` หลังจาก PR #144 merge เข้า main แล้ว**
> เท่านั้น — ห้าม implement ต่อบน `feat/reference-popover` (component ฐานทั้งหมดที่แผนนี้พึ่งพา
> ยังอยู่ใน PR #144 ที่ยังไม่ merge)
> สถานะใน README Feature Status: ไม่มีแถวใหม่ (เป็น UX enhancement ของ Firewall/Policy ที่เสร็จแล้ว)

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็นจริง)**

1. hover badge ในคอลัมน์ "Service / Port" ของหน้า Policy (Firewall / Local-In / Local-Out)
   ค้าง 1 วินาที → popover เปิด แสดงชื่อ object + badge `System` (ถ้าเป็น system) +
   รายการ entries ทั้งหมดในรูป `TCP 443`, `UDP 1000-2000`, `ICMP` (แสดง 5 แรก + "และอีก N รายการ")
2. ท้าย popover มีปุ่ม "ดู Service Objects" ที่พาไป `/policy/services?q=<ชื่อ object>` และหน้านั้น
   ต้อง **กรองรายการตาม `q` ทันทีที่โหลด** (ปัจจุบันหน้ายังไม่รองรับ query param)
3. hover ชื่อ service ในการ์ด "Top Service" ของ RuleStatsDrawer แล้วได้ popover เดียวกัน
   (เฉพาะแถวที่ `serviceName` ตรงกับ Service Object จริงเท่านั้น)
4. ค่า `ALL` และชื่อที่หาไม่เจอใน state → **ไม่ผูก handler ใด ๆ** (badge เฉย ๆ เหมือนเดิม)

**เงื่อนไขเชิงเทคนิคที่ต้องเป็นจริง**

- popover นี้ **ไม่มี network request เลย ทั้ง 100%** — ข้อมูลมาจาก `serviceObjects` state ที่หน้า
  Policy โหลดอยู่แล้ว (`PolicyChainPage.tsx:450/455`) ห้ามเรียก `serviceObjectService.getAll()` เพิ่ม
  และห้าม `.find()` ต่อ hover (ใช้ `Map` + `useMemo` แบบ `addressByName`)
- **ไม่มี popover ระดับ 2** — entry ของ Service Object เป็นแค่ proto/port ไม่มี reference data ผูกอยู่
- ต้อง fallback ไป legacy `{protocol, port}` เสมอเมื่อ `entries` เป็น `undefined`/ว่าง
  (ข้อมูลเก่าใน localStorage / API เก่า)
- **ไม่มีงาน backend เลย**: ไม่มี endpoint ใหม่, ไม่มี DTO ใหม่, ไม่มี migration, ไม่แตะ
  `router.go`/`handlers.go`/`service/`/`kernel/`/`db/`/`main.go`/`install.sh` และ
  **ไม่ต้องแก้ `docs/openapi.yaml` / `frontend/public/openapi.yaml`** (ไม่มี API contract เปลี่ยน)

**นอกขอบเขต (ตัดชัดเจน — ยืนยันโดยเจ้าของโปรเจกต์)**

- ไม่ทำปุ่ม "ดู logs ของพอร์ตนี้" (`/logs/traffic?q=<port>`) — ตัวกรอง `q` เป็น free-text match
  ทั้งแถว เลข `443` จะไป match IP ที่มีเลข 443 ด้วย ผลลัพธ์กำกวมเกินกว่าจะปล่อยเป็นปุ่มลัด
- ไม่แสดง `refPolicies` (where-used) ใน popover — ให้สอดคล้องกับ `AddressObjectReferenceContent`
  ที่ก็ไม่แสดง (ถ้าจะเพิ่ม ต้องเพิ่มพร้อมกันทั้งสองตัวในแผนรอบถัดไป)
- ไม่ทำ Service Object "group" (อ้าง service object อื่นซ้อนกัน) — model ปัจจุบันไม่มีแนวคิดนี้
- ไม่แตะ logic การสร้าง/แก้/จัดลำดับ policy rule, firewall rule generation, และไม่แตะ
  `EndpointRow` markup เดิม (ห่อ wrapper รอบนอกเท่านั้น)
- ไม่ขยายไปหน้าอื่นนอก `PolicyChainPage.tsx` + `RuleStatsDrawer.tsx` (หน้า Statistics/Logs
  แสดงพอร์ตดิบ ไม่มีชื่อ Service Object ให้ hover)

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริงแล้ว ณ 2026-08-17 บน `feat/reference-popover`)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `ServiceObject` / `ServiceEntry` (FE) | **มีแล้ว** — `entries?: ServiceEntry[]` optional + legacy `protocol`/`port` deprecated mirror, `type: "system"\|"custom"`, `refPolicies: string[]` | `frontend/src/data-mockup/mockData.ts:249-271` |
| `ServiceObject` (BE) | **มีแล้ว** ตรงกับ FE field-per-field | `backend/internal/model/types.go:120-143` |
| Service **group** (อ้าง object อื่น) | **ไม่มีแนวคิดนี้ในระบบ** — ความหลากหลายอยู่ใน `entries[]` เท่านั้น | `model/types.go:120-143`, `mockData.ts:249-271` |
| `refPolicies` ถูกเติมจริงจาก backend | **มีแล้ว** (ไม่ใช้ในแผนนี้ ตามข้อตกลง Q3) | `backend/internal/db/repository.go:556-630` |
| normalize legacy → entries (FE) | **มีแล้ว** | `frontend/src/services/serviceObjectService.ts:14-25`; ตัวช่วยซ้ำในหน้า Services ที่ `frontend/src/pages/Services.tsx:62-66` |
| คอลัมน์ Service ในตาราง Policy | **เป็น badge เปล่า ยังไม่มี trigger** | `frontend/src/components/policy/PolicyChainPage.tsx:290-303` (`rule.service.map()`) |
| `AddressReferenceBadge` (แม่แบบ) | **มีแล้ว** | `PolicyChainPage.tsx:168-206` |
| `addressByName` useMemo (แม่แบบ) | **มีแล้ว** | `PolicyChainPage.tsx:471-475` |
| `serviceObjects` state + การโหลด | **มีแล้ว ครบ** (โหลดพร้อม address/interface อยู่แล้ว) | `PolicyChainPage.tsx:418`, โหลดที่ `:447-455` และ `:514-522`, useMemo `serviceOptions` ที่ `:494-500` |
| `SortableRowProps` (ต้องเพิ่ม prop) | **มี `addressByName` แล้ว ยังไม่มี `serviceByName`** | `PolicyChainPage.tsx:209-224` |
| `ReferenceHoverProvider` ครอบหน้า Policy | **มีแล้ว** | `PolicyChainPage.tsx:1342` (ปิด provider), provider เปิดที่ต้นฟังก์ชัน render |
| `ReferenceTrigger` (lazy content, level 1/2) | **มีแล้ว** | `frontend/src/components/reference/ReferenceTrigger.tsx:16-112` |
| `AddressObjectReferenceContent` (แม่แบบ content) | **มีแล้ว** | `frontend/src/components/reference/AddressObjectReferenceContent.tsx:50-117` |
| RuleStatsDrawer — provider แยกในระดับ Drawer content | **มีแล้ว** (พร้อมคอมเมนต์อธิบายเหตุผล portal/z-index) | `frontend/src/components/policy/RuleStatsDrawer.tsx:298-306` |
| RuleStatsDrawer — การ์ด Top Service | **มีแล้ว แต่ยังไม่มี popover** (`EndpointRow` ห่อ trigger เฉพาะเมื่อมี prop `ip` ซึ่งแถว service ไม่มี) | `RuleStatsDrawer.tsx:582-603`, `EndpointRow` ที่ `:75-140`, `serviceDisplayName` ที่ `:147-149` |
| `RuleStatsDrawerProps` | **ยังไม่มี `serviceObjects`** | `RuleStatsDrawer.tsx:42-54`; call site `PolicyChainPage.tsx:1332-1340` |
| `ServiceHit` (มี `serviceName`, อาจว่าง) | **มีแล้ว** | `frontend/src/services/policyEndpointsService.ts:38-46` |
| หน้า `/policy/services` รองรับ `?q=` | **ยังไม่มี** — `searchQuery` เป็น local state ล้วน | `frontend/src/pages/Services.tsx:128`, ใช้ใน filter ที่ `:183-196`, input ที่ `:406-407` |
| route `/policy/services` | **มีแล้ว** (ไม่มี route รายตัวของ service object) | `frontend/src/App.tsx:184` |
| pattern `useSearchParams` deep-link (แม่แบบ) | **มีแล้ว** | `frontend/src/pages/DnsServer.tsx:85-101` (`?tab=`) |
| `ServiceObjectReferenceContent` | **ยังไม่มี** | ต้องสร้างใหม่ใน `frontend/src/components/reference/` |

> **สรุป:** โครงสร้างพื้นฐานทั้งหมด (provider / trigger / hook / pattern content) มีครบจาก PR #144 แล้ว
> งานจริงเหลือแค่ **component content ใหม่ 1 ตัว + wiring 2 จุด + เปิดรับ `?q=` ที่หน้า Services**
> ไม่มีงาน backend, ไม่มี API ใหม่, ไม่มี migration, ไม่แตะ `install.sh`

---

## 2. แนวทางเทคนิค

**2.1 popover เป็น pure presentational ล้วน (ไม่มี fetch, ไม่มี state, ไม่มี level 2)**
Service Object ไม่มีสถิติ/reference data ผูกอยู่ (ไม่มี endpoint `/api/statistics/...` ที่คิดตามพอร์ต
และไม่มีหน้า statistics by port) ดังนั้น content นี้เป็นแค่การ render ข้อมูลที่ caller ถือในมืออยู่แล้ว
*ทางเลือกที่ตัดทิ้ง:* สร้าง endpoint `/api/statistics/reference/service` — ไม่มีข้อมูลต้นทางให้สรุปจริง
จึงเป็นการเพิ่ม API ที่ไม่มีเนื้อหา

**2.2 รับ prop เป็น structural subset ไม่ import `mockData.ts`**
ทำตาม `AddressObjectReferenceContent.tsx:9-25` เป๊ะ — ประกาศ interface ของตัวเองในไฟล์ content
(`ServiceObjectReferenceEntry` / `ServiceObjectReferenceObject`) เพื่อให้ไฟล์ reference ไม่ผูกกับ
โมดูล mock data ทั้งก้อน และให้ `PolicyChainPage`/`RuleStatsDrawer` ส่ง object ที่โหลดมาแล้วเข้าไปตรง ๆ
(TS จะยอมเพราะ `ServiceObject` เป็น superset)

**2.3 lookup ด้วย `Map` + `useMemo` เท่านั้น**
```tsx
// PolicyChainPage.tsx — วางถัดจาก addressByName (~:471-475)
const serviceByName = useMemo(() => {
  const m = new Map<string, ServiceObject>()
  serviceObjects.forEach((s) => m.set(s.name, s))
  return m
}, [serviceObjects])
```
*ทางเลือกที่ตัดทิ้ง:* `.find()` ต่อ hover (ตาราง policy หลายสิบแถว × badge หลายอันต่อแถว) และ
การ fetch ซ้ำใน component ลูก

**2.4 หน้า Services รับ `?q=` แบบ seed-then-sync ตามแม่แบบ `DnsServer.tsx:85-101`**
`searchQuery` ยังเป็น single source of truth ของตัวกรอง; param แค่ **seed ค่าเริ่มต้น** ตอน mount
แล้ว sync กลับด้วย `setSearchParams(..., { replace: true })` เมื่อผู้ใช้พิมพ์ (ไม่ push history ทุกตัวอักษร)
*ทางเลือกที่ตัดทิ้ง:* ทำ route ใหม่ `/policy/services/:name` — ต้องสร้างหน้า detail ใหม่ทั้งหน้าเพื่อ
แสดงข้อมูลที่ popover แสดงอยู่แล้ว ไม่คุ้ม

**2.5 RuleStatsDrawer: ส่ง `serviceObjects` เป็น prop ไม่ fetch เอง**
`PolicyChainPage` โหลด service objects อยู่แล้วและ refresh ตาม lifecycle ของหน้า → ส่งลงเป็น prop
(`serviceObjects: ServiceObject[]`, ให้ default `[]` เพื่อไม่ทำให้ call site อื่น ๆ พัง)
Drawer มี `ReferenceHoverProvider` ของตัวเองอยู่แล้วที่ `:298-306` **ห้ามถอด/ห้ามเพิ่มซ้อน**

**2.6 pattern/ไฟล์แม่แบบที่ต้องทำตาม**
- content: `frontend/src/components/reference/AddressObjectReferenceContent.tsx`
- badge + การตัดสินใจว่าจะผูก trigger ไหม: `PolicyChainPage.tsx:168-206` (`AddressReferenceBadge`)
- deep-link query param: `frontend/src/pages/DnsServer.tsx:85-101`
- wrapper รอบ row ใน drawer: `RuleStatsDrawer.tsx:108-121` (`EndpointRow` ที่ห่อด้วย `ReferenceTrigger`)

---

## 3. ขั้นตอนการทำ (เรียงตาม dependency)

**Step 1 — content component** · **ไฟล์ใหม่:** `frontend/src/components/reference/ServiceObjectReferenceContent.tsx`
- ประกาศ `ServiceObjectReferenceEntry { protocol: "TCP"|"UDP"|"TCP/UDP"|"ICMP"; port: string }` และ
  `ServiceObjectReferenceObject { name; type: "system"|"custom"; protocol; port; entries?: ... }`
  (สองฟิลด์ล่างคือ legacy mirror ใช้เป็น fallback เท่านั้น)
- `const entries = object.entries?.length ? object.entries : [{ protocol: object.protocol, port: object.port }]`
- แสดง 5 แรก (`MAX_VISIBLE_ENTRIES = 5` เหมือนแม่แบบ) + `และอีก N รายการ`
- แต่ละแถว: `<Badge>` protocol + ข้อความ port แบบ mono; **ICMP ที่ไม่มีพอร์ต** (`port` ว่างหรือ `"-"`)
  ให้แสดงคำว่า `ทุกประเภท` แทนตัวเลข ห้ามแสดง `ICMP -` ดิบ ๆ
- ปุ่มท้าย: `navigate(`/policy/services?q=${encodeURIComponent(object.name)}`)` ข้อความ "ดู Service Objects"
- `useNavigate` import จาก `"react-router"` เท่านั้น
> **ไม่ต้อง** ใส่ `ReferenceTrigger` ระดับ 2 ใด ๆ ในไฟล์นี้ และ **ไม่ต้อง** ยิง API — proto/port
> ไม่มี reference data ปลายทาง (ต่างจาก entry ของ Address Object ที่เป็น IP/FQDN)
> **ไม่ต้อง** แสดง `refPolicies` (ข้อตกลง Q3)

**Step 2 — Services page รองรับ `?q=`** · **แก้:** `frontend/src/pages/Services.tsx`
- seed `useState` ของ `searchQuery` (`:128`) ด้วย `searchParams.get("q") ?? ""`
- ทุกจุดที่ `setSearchQuery` (input ที่ `:406-407`) ให้เรียก helper ตัวเดียวที่ทั้ง `setSearchQuery`
  และ `setSearchParams` (`q` ว่าง = `params.delete("q")` ไม่ทิ้ง `?q=` เปล่าไว้) ด้วย `{ replace: true }`
- ตัวกรองที่ `:183-196` **ห้ามแก้ logic** — อ่านจาก `searchQuery` เหมือนเดิม
> **ไม่ต้อง** ทำ `?proto=` หรือ deep-link เปิด modal แก้ไข — นอกขอบเขต

**Step 3 — wiring หน้า Policy** · **แก้:** `frontend/src/components/policy/PolicyChainPage.tsx`
- เพิ่ม `serviceByName` useMemo ถัดจาก `addressByName` (~:475) ตาม §2.3
- เพิ่ม `ServiceReferenceBadge` ถัดจาก `AddressReferenceBadge` (~:206): `ALL` → badge เปล่า,
  หาไม่เจอใน map → badge เปล่า (ไม่ผูก handler), เจอ → `<ReferenceTrigger content={() =>
  <ServiceObjectReferenceContent object={obj} />}>` — **คง className badge เดิมทุกตัวอักษร** (`:294-298`)
- เพิ่ม `serviceByName` เข้า `SortableRowProps` (`:209-221`) และ signature (`:224`) แล้วใช้ใน
  คอลัมน์ Service (`:290-303`) แทน `<Badge>` ตรง ๆ; ส่ง prop ที่ call site ของ `SortableRow`
> **ไม่ต้อง** เพิ่ม `ReferenceHoverProvider` — หน้านี้มีแล้ว (`:1342`)
> **ไม่ต้อง** แตะ `serviceOptions` (`:494-500`) ที่ใช้กับ Combobox ในฟอร์ม — คนละเรื่อง

**Step 4 — wiring RuleStatsDrawer** · **แก้:** `frontend/src/components/policy/RuleStatsDrawer.tsx`
  **และ** `frontend/src/components/policy/PolicyChainPage.tsx` (call site `:1332-1340`)
- เพิ่ม `serviceObjects?: ServiceObject[]` เข้า `RuleStatsDrawerProps` (`:42-54`) default `[]`
  และส่งจาก `PolicyChainPage` (`serviceObjects={serviceObjects}`)
- สร้าง `Map` ชื่อ→object ด้วย `useMemo` ในตัว drawer (ไม่ `.find()` ต่อแถว)
- ขยาย `EndpointRow` (`:75-121`) ด้วย prop ใหม่ **optional** `serviceObject?: ServiceObjectReferenceObject`:
  ถ้ามี → ห่อ `nameBlock` ด้วย `ReferenceTrigger` + `ServiceObjectReferenceContent`;
  ลำดับความสำคัญ: มี `ip` ใช้ทางเดิม (source/dest) → ไม่มี `ip` แต่มี `serviceObject` ใช้ทางใหม่ →
  ไม่มีทั้งคู่ = plain (พฤติกรรมเดิมเป๊ะ)
- ในการ์ด Top Service (`:590-599`) ส่ง `serviceObject={serviceByName.get(hit.serviceName)}`
  (`hit.serviceName` ว่างได้ → `undefined` → ไม่มี popover ตามที่ควร)
> **ไม่ต้อง** เพิ่ม/ย้าย `ReferenceHoverProvider` — drawer มี instance ของตัวเองแล้ว (`:298-306`)
> และเหตุผล z-index/portal เขียนกำกับไว้ในคอมเมนต์ตรงนั้น **ห้ามลบคอมเมนต์นั้น**
> **ไม่ต้อง** แตะ `onViewLogs`/`viewLogsFor`/`serviceDisplayName` และ markup เดิมของ `EndpointRow`

**Step 5 — เอกสาร** · **แก้:** `docs/rules_of_work.md`
- เพิ่มบรรทัดเดียวในกติกา popover ที่ PR #144 เพิ่มไว้: content ที่ไม่มีข้อมูล runtime ปลายทาง
  (เช่น Service Object) เป็น presentational ล้วน ห้ามยิง API และห้ามมีระดับ 2
> **ไม่ต้อง** แก้ `docs/openapi.yaml` / `frontend/public/openapi.yaml` (ไม่มี endpoint เปลี่ยน)
> **ไม่ต้อง** แก้ README Feature Status (ไม่ใช่ฟีเจอร์ระดับ subsystem ใหม่)

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | ใหม่/เดิม | พฤติกรรม |
|---|---|---|---|---|
| GET | `/api/services` (ผ่าน `serviceObjectService.getAll()`) | `authRoute` | **เดิม ไม่แก้** | หน้า Policy เรียกอยู่แล้วตอนโหลด (`PolicyChainPage.tsx:450`) — แผนนี้ **ไม่เพิ่มการเรียกใหม่แม้แต่ครั้งเดียว** |

**ไม่มี endpoint ใหม่ ไม่มี DTO ใหม่ ไม่มี migration** — งานทั้งหมดอยู่ใน frontend
**โหมด `-disable-edit=true`:** ฟีเจอร์เป็น read-only ล้วน (hover + navigate) ใช้งานได้ปกติ ไม่ต้องเพิ่มเงื่อนไข
**Mock mode:** `serviceObjectService` มี localStorage path อยู่แล้ว (`serviceObjectService.ts:28+`)
ทดสอบได้ครบใน `-mock=true` โดยไม่ต้องมีบอร์ดจริง

---

## 5. ข้อควรระวัง

1. **`entries` เป็น optional จริง** — `mockData.ts:268` และ `model/types.go:142` มี `omitempty`
   ถ้าอ่าน `object.entries[0]` หรือ `.map()` ตรง ๆ จะ crash กับ record เก่าใน localStorage
   → บังคับ fallback `[{ protocol, port }]` ทุกจุด (แบบเดียวกับ `Services.tsx:62-66`)
2. **ICMP ไม่มีพอร์ต** — `RuleStatsDrawer.tsx:598` แสดงให้เห็นว่า port ของ ICMP มาเป็น `"-"` ได้
   ถ้าแสดงดิบจะได้ `ICMP -` ซึ่งอ่านแล้วเหมือนข้อมูลหาย → map เป็นข้อความ "ทุกประเภท"
3. **`hit.serviceName` ว่างได้** — `policyEndpointsService.ts:38-46` และ mock catalog ที่ `:133-139`
   จงใจมีเคส `serviceName: ""` (เช่น TCP/22 ที่ไม่ match object ใด) ถ้าเผลอ `Map.get("")` แล้วบังเอิญมี
   object ชื่อว่าง จะได้ popover ผิดตัว → guard `if (!hit.serviceName) return undefined` ก่อน lookup
4. **Combobox/Drawer/drag-reorder ในหน้า Policy** — `PolicyChainPage.tsx` มี `useComboboxAnchor` (~:616)
   และฟอร์มที่ใช้ Combobox (~:1093-1116) ซึ่ง `docs/rules_of_work.md` ระบุว่าไวต่อ focus/pointer blocker
   popover ที่ค้างทับจะทำให้ดรอปดาวน์คลิกไม่ได้ → ใช้ `ReferenceTrigger` ตัวเดิมเท่านั้น (มี
   `onOpenAutoFocus` prevent อยู่ที่ provider แล้ว) และต้องทดสอบ drag-reorder + เปิดฟอร์มแก้ rule ซ้ำทุกครั้ง
5. **z-index/portal ของ RuleStatsDrawer** — รอบก่อนพบว่า reuse provider ของหน้าแม่ทำให้ popover ไป
   render **หลัง** overlay ของ drawer (คอมเมนต์อธิบายอยู่ที่ `RuleStatsDrawer.tsx:300-306`)
   → ห้ามถอด provider ของ drawer และห้าม wrap ซ้อนอีกชั้น
6. **badge เดิมห้ามเปลี่ยนหน้าตา** — คอลัมน์ Service ใช้คลาส `border-primary/20 bg-primary/10
   text-primary` (`:294-298`) ซึ่งเป็น semantic variable อยู่แล้ว การห่อ `ReferenceTrigger` เพิ่ม
   `<span class="cursor-default">` รอบนอก (`ReferenceTrigger.tsx:78`) ระวังไม่ให้ layout
   `flex flex-wrap gap-1` (`:292`) เพี้ยน → ตรวจด้วยตาว่า badge ยังเรียงเหมือนเดิม
7. **`?q=` ต้องไม่ทำให้ history บวม** — ถ้า `setSearchParams` แบบ push ทุกตัวอักษรที่พิมพ์ ปุ่ม Back
   จะย้อนทีละตัวอักษร → ใช้ `{ replace: true }` ตามแม่แบบ `DnsServer.tsx:99`
8. **ค่า `q` มาจาก URL = untrusted input (display-only)** — ใช้เป็นค่าเริ่มต้นของ search filter
   เท่านั้น ห้ามนำไปประกอบ request/DOM โดยตรง; React escape ให้อยู่แล้วเมื่อ render เป็น text
9. **สไตล์** — ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามคลาสสีดิบ (`text-emerald-500` ฯลฯ),
   ต้องผ่านทั้ง dark/light, ใช้เฉพาะ `components/ui/` primitives
10. **ข้อจำกัดการทดสอบ** — ai-developer/ai-qa ไม่มี browser tool จึงตรวจ hover/timing/สี ได้จาก
    การอ่านโค้ดเท่านั้น ข้อที่ต้องใช้เบราว์เซอร์ให้ทำเครื่องหมายว่าเป็นงานของเจ้าของโปรเจกต์ก่อน merge
11. **ลำดับงานกับ PR #144** — ถ้า PR #144 ถูกแก้ระหว่าง review (เช่น เปลี่ยน signature ของ
    `ReferenceTrigger` หรือ `AddressObjectReferenceContent`) ต้อง **อ่านโค้ดบน main ใหม่** ก่อนเริ่ม
    Step 1 เลขบรรทัดในแผนนี้อ้างจาก branch `feat/reference-popover` และจะ drift ได้

---

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [x] ไม่มีงาน backend ในแผนนี้ — ยืนยันว่าไม่มีไฟล์ใน `backend/` ถูกแก้ (ตรวจด้วย `git diff --stat`)

**Frontend**
- [ ] `frontend/src/components/reference/ServiceObjectReferenceContent.tsx` — ไฟล์ใหม่, presentational ล้วน,
      fallback legacy, 5 แรก + "และอีก N รายการ", ICMP แสดง "ทุกประเภท", ปุ่มไป `/policy/services?q=<name>`
- [ ] `frontend/src/pages/Services.tsx` — seed `searchQuery` จาก `?q=` + sync กลับด้วย `{ replace: true }`,
      `q` ว่าง = ลบ param, logic filter เดิมไม่ถูกแก้
- [ ] `frontend/src/components/policy/PolicyChainPage.tsx` — `serviceByName` useMemo, `ServiceReferenceBadge`,
      `SortableRowProps` เพิ่ม prop, ส่ง `serviceObjects` ให้ `RuleStatsDrawer`
- [ ] `frontend/src/components/policy/RuleStatsDrawer.tsx` — prop `serviceObjects` (default `[]`),
      Map lookup, `EndpointRow` prop `serviceObject` แบบ optional, การ์ด Top Service ห่อ trigger
- [ ] ไม่มี `serviceObjectService.getAll()` เพิ่มใหม่ที่ไหนเลย (grep ยืนยัน) และไม่มี `.find()` ต่อ hover
- [ ] `yarn build` และ `yarn lint` ผ่าน

**เอกสาร**
- [ ] `docs/rules_of_work.md` เพิ่มกติกา popover แบบ presentational ล้วน
- [ ] ยืนยันว่า `docs/openapi.yaml` และ `frontend/public/openapi.yaml` **ไม่ถูกแก้** (ไม่มี API เปลี่ยน)

**Git**
- [ ] แตก branch ใหม่จาก `main` **หลัง PR #144 merge แล้ว** และเข้า main ผ่าน PR เท่านั้น

---

### Final Acceptance — ทดสอบรวมครั้งเดียวหลังทุก Step เสร็จ (mock mode: `./pigate-backend -mock=true -allow-dev-cors` + `yarn dev`)

```json
{
  "final_acceptance": [
    "yarn build และ yarn lint ผ่าน · git diff --stat ไม่มีไฟล์ใน backend/ และไม่มี openapi.yaml ทั้งสองไฟล์",
    "หน้า Policy ทั้ง 3 (Firewall, Local-In, Local-Out): hover badge ในคอลัมน์ Service ค้าง 1 วินาที แล้ว popover เปิด แสดงชื่อ object + entries ครบ",
    "Service Object ที่มี entries หลายรายการแสดงครบทุกแถว; เกิน 5 รายการแสดง 5 แรก + 'และอีก N รายการ'",
    "Service Object ที่เป็น system แสดง badge System; entry ICMP แสดง 'ทุกประเภท' ไม่ใช่ 'ICMP -'",
    "badge ค่า ALL และชื่อที่ไม่มีใน Service Objects ไม่เปิด popover เลย (ไม่มี cursor/behavior เปลี่ยน)",
    "กดปุ่ม 'ดู Service Objects' ใน popover แล้วไปที่ /policy/services?q=<name> และตารางถูกกรองเหลือ object นั้นทันทีตอนโหลด",
    "พิมพ์ในช่องค้นหาหน้า Services แล้ว URL อัปเดต q ตาม, ลบข้อความจนว่างแล้ว q หายไปจาก URL, กด Back ไม่ย้อนทีละตัวอักษร",
    "เปิด RuleStatsDrawer ของ rule ที่เปิด Log: hover ชื่อใน 'Top Service' ที่ตรงกับ Service Object จริงแล้ว popover เปิดและ 'ไม่ถูก drawer บัง'",
    "แถว Top Service ที่ serviceName ว่าง (แสดงเป็น PROTO/PORT ดิบ) ไม่เปิด popover และปุ่ม 'ดู log ของรายการนี้' ยังทำงานเหมือนเดิม",
    "popover ของ Source/Destination (Address Object) ยังทำงานครบเหมือนเดิมทุกอย่าง รวม popover ระดับ 2",
    "เปิด DevTools Network: hover badge Service กี่ครั้งก็ตาม ไม่มี HTTP request เกิดขึ้นเลยแม้แต่เส้นเดียว",
    "drag-reorder แถว, เปิด/ปิด Drawer, เปิดฟอร์มแก้ rule และคลิก Combobox Service ในฟอร์ม ทำงานปกติทุกอัน (popover ไม่ค้างทับ)",
    "ดูทั้ง dark และ light mode: badge เรียงเหมือนเดิม ไม่มีเงา/blur และไม่มีคลาสสีดิบ (grep ไฟล์ที่แก้ทั้งหมด)"
  ]
}
```
