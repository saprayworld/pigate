# Service Object Popover — hover ชื่อ Service ในหน้า Policy แล้วเห็นรายละเอียด entries

> เอกสารแผนงานสำหรับฟีเจอร์: เดิมคอลัมน์ "Service / Port" ในตาราง Policy และการ์ด "Top Service"
> ใน RuleStatsDrawer แสดงเป็นชื่อ Service Object เปล่า ๆ ผู้ใช้ต้องเปิดหน้า `/policy/services`
> ไปหาเองว่าชื่อนั้นคือ protocol/port อะไรบ้าง ฟีเจอร์นี้ทำ hover popover แบบเดียวกับ
> Address Object popover ที่เพิ่งทำเสร็จ (PR #144) เพื่อให้เห็น entries ทั้งหมดในที่เดียว
>
> วันที่เขียน: 2026-08-17 · **ทวนเลขบรรทัด/สถานะโค้ดใหม่ทั้งฉบับ: 2026-08-18**
> **ขยายขอบเขต 2026-08-18 (ตัดสินใจโดยเจ้าของโปรเจกต์):** ปุ่ม "ดู Address Objects" ในฝั่ง Address
> ต้องส่ง `?q=` และหน้า `/policy/addresses` ต้องรองรับ `?q=` **ในรอบนี้ด้วย** เพื่อให้สองปุ่มสมมาตรกัน
> (เดิมแผนระบุว่าไม่แก้ฝั่ง Address — ข้อนั้นถูกยกเลิกแล้ว ดู Step 2B)
> Branch อ้างอิง (ที่ใช้สำรวจ): `main` หลัง merge PR #144
> (`6253581 Merge pull request #144 from saprayworld/feat/reference-popover`)
> **หมายเหตุ branch:** PR #144 **merge เข้า `main` เรียบร้อยแล้ว** → component ฐานทั้งหมดที่แผนนี้พึ่งพา
> (`ReferenceHoverProvider` / `ReferenceTrigger` / `AddressObjectReferenceContent`) อยู่บน `main` แล้ว
> **สถานะ: พร้อมแตก branch ใหม่จาก `main` และเริ่ม Step 1 ได้ทันที**
> (ชื่อ branch ที่แนะนำ: `feat/service-reference-popover`)
> สถานะใน README Feature Status: ไม่มีแถวใหม่ (เป็น UX enhancement ของ Firewall/Policy ที่เสร็จแล้ว)

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็นจริง)**

1. hover badge ในคอลัมน์ "Service / Port" ของหน้า Policy (Firewall / Local-In / Local-Out)
   ค้าง 1 วินาที → popover เปิด แสดงชื่อ object + badge `System` (ถ้าเป็น system) +
   รายการ entries ทั้งหมดในรูป `TCP 443`, `UDP 1000-2000`, `ICMP` (แสดง 5 แรก + "และอีก N รายการ")
2. ท้าย popover มีปุ่ม "ดู Service Objects" ที่พาไป `/policy/services?q=<ชื่อ object>` และหน้านั้น
   ต้อง **กรองรายการตาม `q` ทันทีที่โหลด** (ปัจจุบันหน้ายังไม่รองรับ query param)
3. **(ขอบเขตเพิ่ม 2026-08-18)** ปุ่ม "ดู Address Objects" ใน `AddressObjectReferenceContent`
   ต้องพาไป `/policy/addresses?q=<ชื่อ object>` (ปัจจุบันไป `/policy/addresses` เปล่า ๆ) และหน้า
   `/policy/addresses` ต้องรองรับ `?q=` แบบเดียวกับหน้า Services เป๊ะ — **สองปุ่มต้องทำงานสมมาตรกัน**
4. hover ชื่อ service ในการ์ด "Top Service" ของ RuleStatsDrawer แล้วได้ popover เดียวกัน
   (เฉพาะแถวที่ `serviceName` ตรงกับ Service Object จริงเท่านั้น)
5. ค่า `ALL` และชื่อที่หาไม่เจอใน state → **ไม่ผูก handler ใด ๆ** (badge เฉย ๆ เหมือนเดิม)

**เงื่อนไขเชิงเทคนิคที่ต้องเป็นจริง**

- popover นี้ **ไม่มี network request เลย ทั้ง 100%** — ข้อมูลมาจาก `serviceObjects` state ที่หน้า
  Policy โหลดอยู่แล้ว (`PolicyChainPage.tsx:486` และ `:553`) ห้ามเรียก `serviceObjectService.getAll()`
  เพิ่ม และห้าม `.find()` ต่อ hover (ใช้ `Map` + `useMemo` แบบ `addressByName`)
- **ไม่มี popover ระดับ 2** — entry ของ Service Object เป็นแค่ proto/port ไม่มี reference data ผูกอยู่
  (popover ระดับ 2 ของฝั่ง Address ที่มีอยู่แล้ว **ห้ามแตะ**)
- ต้อง fallback ไป legacy `{protocol, port}` เสมอเมื่อ `entries` เป็น `undefined`/ว่าง
  (ข้อมูลเก่าใน localStorage / API เก่า)
- ทั้งสองหน้า (Services, Addresses) ต้องใช้รูปแบบ `?q=` เดียวกัน: seed ตอน mount → sync กลับด้วย
  `setSearchParams(..., { replace: true })` → `q` ว่าง = ลบ param ทิ้ง (**ห้าม push history ทุกตัวอักษร**)
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
- ในหน้า Addresses **ไม่ทำ** `?type=` (deep-link ตัวกรองชนิด entry) และไม่ทำ deep-link เปิด modal
  แก้ไข — งานฝั่ง Address จำกัดที่ `?q=` อย่างเดียวเท่านั้น

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริงแล้ว · ทวนซ้ำ 2026-08-18 บน `main` หลัง merge PR #144)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `ServiceObject` / `ServiceEntry` (FE) | **มีแล้ว** — `entries?: ServiceEntry[]` optional + legacy `protocol`/`port` deprecated mirror, `type: "system"\|"custom"`, `refPolicies: string[]` | `frontend/src/data-mockup/mockData.ts:257-283` (`ServiceEntry` ที่ `:261-264`, `ServiceObject` ที่ `:267-283`, `entries?` ที่ `:280`) |
| `ServiceObject` (BE) | **มีแล้ว** ตรงกับ FE field-per-field | `backend/internal/model/types.go:120-143` (`Entries` ที่ `:142`) |
| Service **group** (อ้าง object อื่น) | **ไม่มีแนวคิดนี้ในระบบ** — ความหลากหลายอยู่ใน `entries[]` เท่านั้น | `model/types.go:120-143`, `mockData.ts:267-283` |
| `refPolicies` ถูกเติมจริงจาก backend | **มีแล้ว** (ไม่ใช้ในแผนนี้ ตามข้อตกลง Q3) | `backend/internal/db/repository.go:561-614` (`GetServices`), `:615-658` (`GetServiceByID`) |
| normalize legacy → entries (FE) | **มีแล้ว** | `frontend/src/services/serviceObjectService.ts:14-25` (`normalizeService`); ตัวช่วยซ้ำในหน้า Services ที่ `frontend/src/pages/Services.tsx:62-66` (`serviceEntries`, คอมเมนต์ที่ `:58-61`) |
| คอลัมน์ Service ในตาราง Policy | **เป็น badge เปล่า ยังไม่มี trigger** | `frontend/src/components/policy/PolicyChainPage.tsx:326-339` (`rule.service.map()` ที่ `:329`) |
| `AddressReferenceBadge` (แม่แบบ) | **มีแล้ว** | `PolicyChainPage.tsx:196-234` (ตัวฟังก์ชัน `:204-234`) |
| `addressByName` useMemo (แม่แบบ) | **มีแล้ว** | `PolicyChainPage.tsx:507-511` (คอมเมนต์อธิบายที่ `:501-506`) |
| `serviceObjects` state + การโหลด | **มีแล้ว ครบ** (โหลดพร้อม address/interface อยู่แล้ว) | `PolicyChainPage.tsx:454`, โหลดที่ `:483-491` (`loadPolicies`) และ `:550-558` (initial `useEffect`), useMemo `serviceOptions` ที่ `:530-536` |
| `SortableRowProps` (ต้องเพิ่ม prop) | **มี `addressByName` แล้ว ยังไม่มี `serviceByName`** | `PolicyChainPage.tsx:237-249` (signature `:252`, call site `:1021-1036` — ส่ง `addressByName` ที่ `:1028`) |
| `ReferenceHoverProvider` ครอบหน้า Policy | **มีแล้ว** | `PolicyChainPage.tsx:864` (เปิด provider พร้อม `closeWhen={isModalOpen \|\| isStatsDrawerOpen}`), ปิดที่ `:1452` |
| `ReferenceTrigger` (lazy content, level 1/2) | **มีแล้ว** | `frontend/src/components/reference/ReferenceTrigger.tsx:16-112` |
| `AddressObjectReferenceContent` (แม่แบบ content) | **มีแล้ว** | `frontend/src/components/reference/AddressObjectReferenceContent.tsx:50-117` (interface ท้องถิ่นที่ `:9-25`) |
| ปุ่ม "ดู Address Objects" ในแม่แบบ | **navigate ไป `/policy/addresses` เปล่า ๆ ยังไม่มี `?q=`** → **ต้องแก้ใน Step 2B** | `AddressObjectReferenceContent.tsx:110-114` (`onClick={() => navigate("/policy/addresses")}` ที่ `:111`) |
| ผู้ใช้ `AddressObjectReferenceContent` (call site) | **มีที่เดียวเท่านั้น** — `AddressReferenceBadge` ในหน้า Policy (RuleStatsDrawer ใช้ `IpReferenceContent`/`CombinedReferenceContent` คนละตัว จึง **ไม่กระทบ**) | import ที่ `PolicyChainPage.tsx:44`, ใช้ที่ `:215`; drawer ใช้ `RuleStatsDrawer.tsx:29-31`, `:111-118` |
| หน้า `/policy/addresses` รองรับ `?q=` | **ยังไม่มี** (ยืนยัน 2026-08-18: ไฟล์ `Addresses.tsx` **ไม่มี import จาก `react-router` เลยแม้แต่บรรทัดเดียว**) — `searchQuery` เป็น local state ล้วน | `frontend/src/pages/Addresses.tsx:90` (`useState("")`), ใช้ใน filter ที่ `:155-167` (match ชื่อ/ค่า entry/`refPolicies` ที่ `:158-161`), input ที่ `:417-426` (`value` ที่ `:421`, `onChange` ที่ `:422` — **เป็น `setSearchQuery` จุดเดียวในไฟล์**) |
| route `/policy/addresses` | **มีแล้ว** (`<Route path="addresses" element={<Addresses />} />` ใต้ parent `policy`) | `frontend/src/App.tsx:183` |
| หน้า `/policy/services` รองรับ `?q=` | **ยังไม่มี** (ยืนยันอีกครั้ง 2026-08-18: ไม่มี `useSearchParams` ในไฟล์เลย) — `searchQuery` เป็น local state ล้วน | `frontend/src/pages/Services.tsx:128`, ใช้ใน filter ที่ `:183-196`, input ที่ `:406-407` |
| route `/policy/services` | **มีแล้ว** (`<Route path="services" …>` ใต้ parent `policy`; ไม่มี route รายตัวของ service object) | `frontend/src/App.tsx:184` |
| pattern `useSearchParams` deep-link (แม่แบบ) | **มีแล้ว** | `frontend/src/pages/DnsServer.tsx:85-101` (`?tab=`, `{ replace: true }` ที่ `:99`) |
| RuleStatsDrawer — provider แยกในระดับ Drawer content | **มีแล้ว** (พร้อมคอมเมนต์อธิบายเหตุผล portal/z-index) | `frontend/src/components/policy/RuleStatsDrawer.tsx:300-307` (คอมเมนต์ `:300-306`, `<ReferenceHoverProvider>` ที่ `:307`, ปิดที่ `:643`) |
| RuleStatsDrawer — การ์ด Top Service | **มีแล้ว แต่ยังไม่มี popover** (`EndpointRow` ห่อ trigger เฉพาะเมื่อมี prop `ip` ซึ่งแถว service ไม่มี) | `RuleStatsDrawer.tsx:582-603`, `EndpointRow` ที่ `:75-141`, `serviceDisplayName` ที่ `:147-149` |
| `RuleStatsDrawerProps` | **ยังไม่มี `serviceObjects`** | `RuleStatsDrawer.tsx:42-54`; call site `PolicyChainPage.tsx:1442-1450` |
| `ServiceHit` (มี `serviceName`, อาจว่าง) | **มีแล้ว** | `frontend/src/services/policyEndpointsService.ts:38-46` |
| `ServiceObjectReferenceContent` | **ยังไม่มี** (ยืนยัน 2026-08-18: โฟลเดอร์ `components/reference/` มี 7 ไฟล์ — Address/Ip/Domain/Combined/Trigger/Provider/DnsLoggingDisabledNotice เท่านั้น) | ต้องสร้างใหม่ใน `frontend/src/components/reference/` |

> **สรุป:** โครงสร้างพื้นฐานทั้งหมด (provider / trigger / hook / pattern content) อยู่บน `main` ครบแล้ว
> งานจริงเหลือแค่ **component content ใหม่ 1 ตัว + wiring 2 จุด + เปิดรับ `?q=` ที่หน้า Services
> และ (ขอบเขตเพิ่ม) แก้ปุ่มฝั่ง Address ให้ส่ง `?q=` + เปิดรับ `?q=` ที่หน้า Addresses**
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
> ข้อต่างที่ต้องรู้: `AddressObjectReferenceObject` ใช้ฟิลด์ `system: boolean` เป็นตัวบอกว่าเป็น system
> (`AddressObjectReferenceContent.tsx:19`, ใช้ที่ `:62`) แต่ `ServiceObject` ของ FE ใช้
> `type: "system" | "custom"` (`mockData.ts:281`) — **ห้ามลอก `system: boolean` มาตรง ๆ**
> ให้เช็ก `object.type === "system"` แทน (และระวังชื่อฟิลด์ `type` ในฝั่ง Address คือชนิด entry
> คนละความหมายกัน)

**2.3 lookup ด้วย `Map` + `useMemo` เท่านั้น**
```tsx
// PolicyChainPage.tsx — วางถัดจาก addressByName (~:507-511)
const serviceByName = useMemo(() => {
  const m = new Map<string, ServiceObject>()
  serviceObjects.forEach((s) => m.set(s.name, s))
  return m
}, [serviceObjects])
```
*ทางเลือกที่ตัดทิ้ง:* `.find()` ต่อ hover (ตาราง policy หลายสิบแถว × badge หลายอันต่อแถว) และ
การ fetch ซ้ำใน component ลูก

**2.4 หน้า Services *และ* หน้า Addresses รับ `?q=` แบบ seed-then-sync ตามแม่แบบ `DnsServer.tsx:85-101`**
`searchQuery` ยังเป็น single source of truth ของตัวกรอง; param แค่ **seed ค่าเริ่มต้น** ตอน mount
แล้ว sync กลับด้วย `setSearchParams(..., { replace: true })` เมื่อผู้ใช้พิมพ์ (ไม่ push history ทุกตัวอักษร)
ทั้งสองหน้าต้องเขียนด้วย pattern เดียวกันคำต่อคำ (helper ตัวเดียวชื่อเดียวกันในแต่ละไฟล์ เช่น
`updateSearchQuery`) เพื่อให้ diff อ่านง่ายและพฤติกรรมสมมาตร
> *ทางเลือกที่ตัดทิ้ง:* (ก) ทำ route ใหม่ `/policy/services/:name` — ต้องสร้างหน้า detail ใหม่ทั้งหน้าเพื่อ
> แสดงข้อมูลที่ popover แสดงอยู่แล้ว ไม่คุ้ม
> (ข) ดึง helper `?q=` ไปไว้เป็น custom hook กลาง (`useQueryParamSearch`) — เป็น refactor ที่กระทบ
> ไฟล์เพิ่มโดยไม่จำเป็นในรอบนี้ (มีผู้ใช้แค่ 2 หน้า) ถ้าอนาคตมีหน้าที่ 3 ค่อยยกขึ้นเป็น hook

**2.5 RuleStatsDrawer: ส่ง `serviceObjects` เป็น prop ไม่ fetch เอง**
`PolicyChainPage` โหลด service objects อยู่แล้วและ refresh ตาม lifecycle ของหน้า → ส่งลงเป็น prop
(`serviceObjects: ServiceObject[]`, ให้ default `[]` เพื่อไม่ทำให้ call site อื่น ๆ พัง)
Drawer มี `ReferenceHoverProvider` ของตัวเองอยู่แล้วที่ `:300-307` **ห้ามถอด/ห้ามเพิ่มซ้อน**

**2.6 pattern/ไฟล์แม่แบบที่ต้องทำตาม**
- content: `frontend/src/components/reference/AddressObjectReferenceContent.tsx` (`:50-117`)
- badge + การตัดสินใจว่าจะผูก trigger ไหม: `PolicyChainPage.tsx:196-234` (`AddressReferenceBadge`)
- deep-link query param: `frontend/src/pages/DnsServer.tsx:85-101`
- wrapper รอบ row ใน drawer: `RuleStatsDrawer.tsx:108-121` (`EndpointRow` ที่ห่อด้วย `ReferenceTrigger`)

---

## 3. ขั้นตอนการทำ (เรียงตาม dependency)

**Step 1 — content component** · **ไฟล์ใหม่:** `frontend/src/components/reference/ServiceObjectReferenceContent.tsx`
- ประกาศ `ServiceObjectReferenceEntry { protocol: "TCP"|"UDP"|"TCP/UDP"|"ICMP"; port: string }` และ
  `ServiceObjectReferenceObject { name; type: "system"|"custom"; protocol; port; entries?: ... }`
  (สองฟิลด์ `protocol`/`port` คือ legacy mirror ใช้เป็น fallback เท่านั้น; `type` ที่นี่คือ system/custom
  ตาม `ServiceObject` — ไม่ใช่ `system: boolean` แบบฝั่ง Address ดู §2.2)
- `const entries = object.entries?.length ? object.entries : [{ protocol: object.protocol, port: object.port }]`
- แสดง 5 แรก (`MAX_VISIBLE_ENTRIES = 5` เหมือนแม่แบบ `AddressObjectReferenceContent.tsx:27`) + `และอีก N รายการ`
- แต่ละแถว: `<Badge>` protocol + ข้อความ port แบบ mono; **ICMP ที่ไม่มีพอร์ต** (`port` ว่างหรือ `"-"`)
  ให้แสดงคำว่า `ทุกประเภท` แทนตัวเลข ห้ามแสดง `ICMP -` ดิบ ๆ
- ปุ่มท้าย: `navigate(`/policy/services?q=${encodeURIComponent(object.name)}`)` ข้อความ "ดู Service Objects"
- `useNavigate` import จาก `"react-router"` เท่านั้น
> **ไม่ต้อง** ใส่ `ReferenceTrigger` ระดับ 2 ใด ๆ ในไฟล์นี้ และ **ไม่ต้อง** ยิง API — proto/port
> ไม่มี reference data ปลายทาง (ต่างจาก entry ของ Address Object ที่เป็น IP/FQDN)
> **ไม่ต้อง** แสดง `refPolicies` (ข้อตกลง Q3)
> ปุ่มของฝั่ง Address จะถูกแก้ให้ส่ง `?q=` เหมือนกันใน **Step 2B** → รูปแบบ URL ของปุ่มทั้งสองฝั่ง
> ต้องเป็นแบบเดียวกันเป๊ะ (`?q=${encodeURIComponent(object.name)}`) อย่าเขียนคนละสไตล์

**Step 2 — Services page รองรับ `?q=`** · **แก้:** `frontend/src/pages/Services.tsx`
- seed `useState` ของ `searchQuery` (`:128`) ด้วย `searchParams.get("q") ?? ""`
- ทุกจุดที่ `setSearchQuery` (input ที่ `:404-410`, `onChange` ที่ `:407`) ให้เรียก helper ตัวเดียวที่ทั้ง
  `setSearchQuery` และ `setSearchParams` (`q` ว่าง = `params.delete("q")` ไม่ทิ้ง `?q=` เปล่าไว้)
  ด้วย `{ replace: true }`
- ตัวกรองที่ `:183-196` **ห้ามแก้ logic** — อ่านจาก `searchQuery` เหมือนเดิม
> **ไม่ต้อง** ทำ `?proto=` หรือ deep-link เปิด modal แก้ไข — นอกขอบเขต

**Step 2B — ทำฝั่ง Address ให้สมมาตร (ขอบเขตเพิ่ม 2026-08-18 โดยเจ้าของโปรเจกต์)**
· **แก้ 2 ไฟล์:** `frontend/src/components/reference/AddressObjectReferenceContent.tsx`
และ `frontend/src/pages/Addresses.tsx`

(a) **ปุ่มใน `AddressObjectReferenceContent.tsx`** (`:110-114`)
- เปลี่ยน `onClick={() => navigate("/policy/addresses")}` (`:111`) เป็น
  ``navigate(`/policy/addresses?q=${encodeURIComponent(object.name)}`)``
- **ห้ามแตะอย่างอื่นในไฟล์นี้เลย** — โดยเฉพาะ `ReferenceTrigger` ระดับ 2 (`:85-100`),
  fallback legacy entries (`:52-53`) และคอมเมนต์อธิบายที่ `:40-49` (เป็นบันทึกเหตุผลจาก PR #144)
- ข้อความปุ่ม ("ดู Address Objects"), `size`/`variant`/className (`:111`) คงเดิมทุกตัวอักษร
- **ผลกระทบ call site:** `AddressObjectReferenceContent` ถูกใช้ **ที่เดียวเท่านั้น** คือ
  `AddressReferenceBadge` ใน `PolicyChainPage.tsx:215` (import ที่ `:44`) — RuleStatsDrawer ใช้
  `IpReferenceContent`/`CombinedReferenceContent` (`RuleStatsDrawer.tsx:29-31`, `:111-118`) ไม่ใช่ตัวนี้
  → การเปลี่ยนปลายทาง navigate **ไม่กระทบ drawer และไม่กระทบหน้าอื่น** แต่ให้ ai-developer
  `grep -rn "AddressObjectReferenceContent" frontend/src` ยืนยันซ้ำก่อนแก้ (เผื่อมี PR อื่น merge แทรก)

(b) **หน้า `Addresses.tsx` รองรับ `?q=`** — ทำตาม pattern เดียวกับ Step 2 เป๊ะ (แม่แบบ `DnsServer.tsx:85-101`)
- ไฟล์นี้ **ยังไม่มี import จาก `"react-router"` เลย** → ต้องเพิ่ม `import { useSearchParams } from "react-router"`
  (ห้าม `react-router-dom` เด็ดขาด ตาม CLAUDE.md)
- seed `useState` ของ `searchQuery` (`:90`) ด้วย `searchParams.get("q") ?? ""`
- จุด `setSearchQuery` ในไฟล์นี้มี **จุดเดียว** คือ `onChange` ของ Input (`:422`, ตัว Input ที่ `:417-426`)
  → เปลี่ยนไปเรียก helper ที่ทั้ง `setSearchQuery` และ `setSearchParams` (`q` ว่าง = `params.delete("q")`)
  ด้วย `{ replace: true }`
- ตัวกรอง `filteredAddresses` (`:155-167`) **ห้ามแก้ logic** — ยังอ่าน `searchQuery` เหมือนเดิม
  (มัน match ทั้ง `name`, ค่า entry และ `refPolicies` ที่ `:158-161` → การค้นด้วยชื่อ object จาก popover
  จะเจอแน่นอน แต่ **อาจได้แถวอื่นติดมาด้วย** ถ้าชื่อนั้นไปตรงกับ `refPolicies`/ค่า entry ของ object อื่น
  ซึ่งเป็นพฤติกรรมที่ยอมรับได้ เหมือนหน้า Services — **ห้ามเปลี่ยนเป็น exact-match**)
- `selectedTypeFilter` (`:91`) และ selection state (`:94`) **ห้ามแตะ**
> **ไม่ต้อง** ทำ `?type=` และ **ไม่ต้อง** deep-link เปิด modal แก้ไข address
> **ไม่ต้อง** แตะการ์ดสถิติด้านบน (`:145-152`) ที่นับจาก `addresses` ทั้งหมด ไม่ใช่ผลกรอง

**Step 3 — wiring หน้า Policy** · **แก้:** `frontend/src/components/policy/PolicyChainPage.tsx`
- เพิ่ม `serviceByName` useMemo ถัดจาก `addressByName` (~:511) ตาม §2.3
- เพิ่ม `ServiceReferenceBadge` ถัดจาก `AddressReferenceBadge` (~:234): `ALL` → badge เปล่า,
  หาไม่เจอใน map → badge เปล่า (ไม่ผูก handler), เจอ → `<ReferenceTrigger content={() =>
  <ServiceObjectReferenceContent object={obj} />}>` — **คง className badge เดิมทุกตัวอักษร** (`:330-336`)
- เพิ่ม `serviceByName` เข้า `SortableRowProps` (`:237-249`) และ signature (`:252`) แล้วใช้ใน
  คอลัมน์ Service (`:326-339`) แทน `<Badge>` ตรง ๆ; ส่ง prop ที่ call site ของ `SortableRow` (`:1021-1036`,
  วางถัดจาก `addressByName={addressByName}` ที่ `:1028`)
> **ไม่ต้อง** เพิ่ม `ReferenceHoverProvider` — หน้านี้มีแล้ว (เปิด `:864` / ปิด `:1452`)
> **ไม่ต้อง** แตะ `serviceOptions` (`:530-536`) ที่ใช้กับ Combobox ในฟอร์ม — คนละเรื่อง
> **ไม่ต้อง** แตะ `AddressReferenceBadge` (`:196-234`) — Step 2B แก้เฉพาะข้างในไฟล์ content เท่านั้น

**Step 4 — wiring RuleStatsDrawer** · **แก้:** `frontend/src/components/policy/RuleStatsDrawer.tsx`
  **และ** `frontend/src/components/policy/PolicyChainPage.tsx` (call site `:1442-1450`)
- เพิ่ม `serviceObjects?: ServiceObject[]` เข้า `RuleStatsDrawerProps` (`:42-54`) default `[]`
  และส่งจาก `PolicyChainPage` (`serviceObjects={serviceObjects}`)
- สร้าง `Map` ชื่อ→object ด้วย `useMemo` ในตัว drawer (ไม่ `.find()` ต่อแถว)
- ขยาย `EndpointRow` (`:75-141`, ตัว wrapper `ip ? … : nameBlock` ที่ `:108-121`) ด้วย prop ใหม่
  **optional** `serviceObject?: ServiceObjectReferenceObject`:
  ถ้ามี → ห่อ `nameBlock` (`:94-106`) ด้วย `ReferenceTrigger` + `ServiceObjectReferenceContent`;
  ลำดับความสำคัญ: มี `ip` ใช้ทางเดิม (source/dest) → ไม่มี `ip` แต่มี `serviceObject` ใช้ทางใหม่ →
  ไม่มีทั้งคู่ = plain (พฤติกรรมเดิมเป๊ะ)
- ในการ์ด Top Service (`:591-599`) ส่ง `serviceObject={serviceByName.get(hit.serviceName)}`
  (`hit.serviceName` ว่างได้ → `undefined` → ไม่มี popover ตามที่ควร)
> **ไม่ต้อง** เพิ่ม/ย้าย `ReferenceHoverProvider` — drawer มี instance ของตัวเองแล้ว (`:300-307`)
> และเหตุผล z-index/portal เขียนกำกับไว้ในคอมเมนต์ตรงนั้น (`:300-306`) **ห้ามลบคอมเมนต์นั้น**
> **ไม่ต้อง** แตะ `onViewLogs`/`viewLogsFor`/`serviceDisplayName` (`:147-149`) และ markup เดิมของ `EndpointRow`

**Step 5 — เอกสาร** · **แก้:** `docs/rules_of_work.md`
- เพิ่มบรรทัดเดียวในกติกา popover ที่ PR #144 เพิ่มไว้: content ที่ไม่มีข้อมูล runtime ปลายทาง
  (เช่น Service Object) เป็น presentational ล้วน ห้ามยิง API และห้ามมีระดับ 2
- เพิ่มอีกบรรทัด: ปุ่ม "ดู … Objects" ท้าย popover ทุกตัว **ต้อง** ส่ง `?q=<ชื่อ object>` และหน้า
  ปลายทางต้อง seed ตัวกรองจาก `?q=` (แบบ `{ replace: true }`) — เป็นกติกากลาง ไม่ใช่ทางเลือกรายหน้า
> **ไม่ต้อง** แก้ `docs/openapi.yaml` / `frontend/public/openapi.yaml` (ไม่มี endpoint เปลี่ยน)
> **ไม่ต้อง** แก้ README Feature Status (ไม่ใช่ฟีเจอร์ระดับ subsystem ใหม่)

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | ใหม่/เดิม | พฤติกรรม |
|---|---|---|---|---|
| GET | `/api/services` (ผ่าน `serviceObjectService.getAll()`) | `authRoute` | **เดิม ไม่แก้** | หน้า Policy เรียกอยู่แล้วตอนโหลด (`PolicyChainPage.tsx:486` ใน `loadPolicies` และ `:553` ใน initial `useEffect`) — แผนนี้ **ไม่เพิ่มการเรียกใหม่แม้แต่ครั้งเดียว** |
| GET | `/api/addresses` (ผ่าน `addressObjectService`) | `authRoute` | **เดิม ไม่แก้** | หน้า Addresses โหลดอยู่แล้วตอน mount — Step 2B แตะแค่ query param/ตัวกรองฝั่ง client ไม่แตะการเรียก API |

**ไม่มี endpoint ใหม่ ไม่มี DTO ใหม่ ไม่มี migration** — งานทั้งหมดอยู่ใน frontend
**โหมด `-disable-edit=true`:** ฟีเจอร์เป็น read-only ล้วน (hover + navigate) ใช้งานได้ปกติ ไม่ต้องเพิ่มเงื่อนไข
**Mock mode:** `serviceObjectService` มี localStorage path อยู่แล้ว (`serviceObjectService.ts:28+`
— `getLocalServices()` ที่ `:28` normalize ทุก record ด้วย `normalizeService`)
ทดสอบได้ครบใน `-mock=true` โดยไม่ต้องมีบอร์ดจริง

---

## 5. ข้อควรระวัง

1. **`entries` เป็น optional จริง** — `mockData.ts:280` และ `model/types.go:142` มี `omitempty`
   ถ้าอ่าน `object.entries[0]` หรือ `.map()` ตรง ๆ จะ crash กับ record เก่าใน localStorage
   → บังคับ fallback `[{ protocol, port }]` ทุกจุด (แบบเดียวกับ `Services.tsx:62-66`)
2. **ICMP ไม่มีพอร์ต** — `RuleStatsDrawer.tsx:598` แสดงให้เห็นว่า port ของ ICMP มาเป็น `"-"` ได้
   (`hit.proto === "ICMP" && hit.port === "-"`) ถ้าแสดงดิบจะได้ `ICMP -` ซึ่งอ่านแล้วเหมือนข้อมูลหาย
   → map เป็นข้อความ "ทุกประเภท"
3. **`hit.serviceName` ว่างได้** — `policyEndpointsService.ts:38-46` และ mock catalog ที่ `:133-139`
   จงใจมีเคส `serviceName: ""` (TCP/22 ที่ `:137` และ ICMP ที่ `:138`) ถ้าเผลอ `Map.get("")` แล้วบังเอิญมี
   object ชื่อว่าง จะได้ popover ผิดตัว → guard `if (!hit.serviceName) return undefined` ก่อน lookup
4. **Combobox/Drawer/drag-reorder ในหน้า Policy** — `PolicyChainPage.tsx` มี `useComboboxAnchor` (~:650-654,
   `serviceAnchor` ที่ `:652`) และฟอร์มที่ใช้ Combobox Service (~:1154-1185) ซึ่ง `docs/rules_of_work.md`
   ระบุว่าไวต่อ focus/pointer blocker popover ที่ค้างทับจะทำให้ดรอปดาวน์คลิกไม่ได้ → ใช้ `ReferenceTrigger`
   ตัวเดิมเท่านั้น (provider ของหน้านี้มี `closeWhen={isModalOpen || isStatsDrawerOpen}` ที่ `:864`
   ปิด popover ให้อัตโนมัติเมื่อเปิด modal/drawer) และต้องทดสอบ drag-reorder + เปิดฟอร์มแก้ rule ซ้ำทุกครั้ง
5. **z-index/portal ของ RuleStatsDrawer** — รอบก่อนพบว่า reuse provider ของหน้าแม่ทำให้ popover ไป
   render **หลัง** overlay ของ drawer (คอมเมนต์อธิบายอยู่ที่ `RuleStatsDrawer.tsx:300-306`)
   → ห้ามถอด provider ของ drawer (`:307`) และห้าม wrap ซ้อนอีกชั้น
6. **badge เดิมห้ามเปลี่ยนหน้าตา** — คอลัมน์ Service ใช้คลาส `border-primary/20 bg-primary/10
   text-primary` (`:333`) ซึ่งเป็น semantic variable อยู่แล้ว การห่อ `ReferenceTrigger` เพิ่ม
   `<span class="cursor-default">` รอบนอก (`ReferenceTrigger.tsx:78`) ระวังไม่ให้ layout
   `flex flex-wrap gap-1` (`:328`) เพี้ยน → ตรวจด้วยตาว่า badge ยังเรียงเหมือนเดิม
7. **`?q=` ต้องไม่ทำให้ history บวม** — ถ้า `setSearchParams` แบบ push ทุกตัวอักษรที่พิมพ์ ปุ่ม Back
   จะย้อนทีละตัวอักษร → ใช้ `{ replace: true }` ตามแม่แบบ `DnsServer.tsx:99` **ทั้งหน้า Services
   และหน้า Addresses**
8. **ค่า `q` มาจาก URL = untrusted input (display-only)** — ใช้เป็นค่าเริ่มต้นของ search filter
   เท่านั้น ห้ามนำไปประกอบ request/DOM โดยตรง; React escape ให้อยู่แล้วเมื่อ render เป็น text
   (ใช้เกณฑ์นี้กับหน้า Addresses ด้วย — ค่า `q` เข้าไปเป็น `searchQuery` เฉย ๆ ไม่มี regex/`dangerouslySetInnerHTML`)
9. **(ขอบเขตเพิ่ม) การเปลี่ยนปลายทางปุ่มฝั่ง Address เป็น behavior change ที่ผู้ใช้เห็น** —
   เดิมกดแล้วเห็น "รายการ address ทั้งหมด" ตอนนี้จะเห็น "รายการที่ถูกกรองแล้ว" ผู้ใช้ที่ชินของเดิมอาจ
   คิดว่า object อื่นหายไป → ต้องมั่นใจว่าล้างช่องค้นหาแล้วรายการกลับมาครบและ `q` หลุดออกจาก URL
   (เกณฑ์นี้อยู่ใน Final Acceptance) · ตรวจ call site ด้วย grep ก่อนแก้ (Step 2B(a)) —
   ปัจจุบันมีจุดเดียวคือ `PolicyChainPage.tsx:215`
10. **ตัวกรองของหน้า Addresses กว้างกว่าที่คิด** — `filteredAddresses` match ทั้ง `name`, ค่าของ entry
    และ `refPolicies` (`Addresses.tsx:158-161`) ดังนั้น `?q=<ชื่อ object>` อาจคืนมากกว่า 1 แถวได้
    เป็นเรื่องปกติ **ห้ามแก้ตัวกรองให้เป็น exact-match** เพื่อ "ให้ผลสวย" — จะทำให้ช่องค้นหาปกติเสีย
11. **สไตล์** — ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามคลาสสีดิบ (`text-emerald-500` ฯลฯ),
    ต้องผ่านทั้ง dark/light, ใช้เฉพาะ `components/ui/` primitives
12. **ข้อจำกัดการทดสอบ** — ai-developer/ai-qa ไม่มี browser tool จึงตรวจ hover/timing/สี ได้จาก
    การอ่านโค้ดเท่านั้น ข้อที่ต้องใช้เบราว์เซอร์ให้ทำเครื่องหมายว่าเป็นงานของเจ้าของโปรเจกต์ก่อน merge
13. **เลขบรรทัดในแผนนี้ทวนกับ `main` แล้วเมื่อ 2026-08-18 (หลัง PR #144 merge)** — แต่ยังเลื่อนได้อีก
    ถ้ามี PR อื่น merge เข้า `main` ก่อนเริ่มงาน (เช่นสาย statistics ที่กำลังทำอยู่) → ก่อนเริ่ม Step 1
    ให้ `git pull` แล้ว **ยืนยัน symbol ด้วย grep** (`AddressReferenceBadge`, `addressByName`,
    `EndpointRow`, `RuleStatsDrawerProps`, `filteredAddresses`, `AddressObjectReferenceContent`)
    ไม่ใช่เชื่อเลขบรรทัดตรง ๆ

---

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [x] ไม่มีงาน backend ในแผนนี้ — ยืนยันว่าไม่มีไฟล์ใน `backend/` ถูกแก้ (ตรวจด้วย `git diff --stat`)

**Frontend**
- [ ] `frontend/src/components/reference/ServiceObjectReferenceContent.tsx` — ไฟล์ใหม่, presentational ล้วน,
      fallback legacy, 5 แรก + "และอีก N รายการ", ICMP แสดง "ทุกประเภท", ปุ่มไป `/policy/services?q=<name>`
- [ ] `frontend/src/pages/Services.tsx` — seed `searchQuery` จาก `?q=` + sync กลับด้วย `{ replace: true }`,
      `q` ว่าง = ลบ param, logic filter เดิมไม่ถูกแก้
- [ ] `frontend/src/components/reference/AddressObjectReferenceContent.tsx` — ปุ่ม (`:111`) เปลี่ยนเป็น
      `/policy/addresses?q=${encodeURIComponent(object.name)}`; **ส่วนอื่นของไฟล์ (level-2 trigger,
      fallback, คอมเมนต์ `:40-49`) ไม่ถูกแตะ** (ตรวจด้วย `git diff` ว่าเป็น 1 บรรทัด)
- [ ] `frontend/src/pages/Addresses.tsx` — เพิ่ม `useSearchParams` จาก `"react-router"`, seed `searchQuery`
      (`:90`) จาก `?q=`, `onChange` (`:422`) sync กลับด้วย `{ replace: true }`, `q` ว่าง = ลบ param,
      `filteredAddresses` (`:155-167`) และ `selectedTypeFilter` ไม่ถูกแก้
- [ ] `frontend/src/components/policy/PolicyChainPage.tsx` — `serviceByName` useMemo, `ServiceReferenceBadge`,
      `SortableRowProps` เพิ่ม prop, ส่ง `serviceObjects` ให้ `RuleStatsDrawer`
- [ ] `frontend/src/components/policy/RuleStatsDrawer.tsx` — prop `serviceObjects` (default `[]`),
      Map lookup, `EndpointRow` prop `serviceObject` แบบ optional, การ์ด Top Service ห่อ trigger
- [ ] grep ยืนยัน: ไม่มี `react-router-dom` โผล่ในไฟล์ที่แก้, ไม่มี `serviceObjectService.getAll()`
      /`addressObjectService.getAll()` เพิ่มใหม่ที่ไหนเลย และไม่มี `.find()` ต่อ hover
- [ ] grep ยืนยัน: `setSearchParams` ทุกจุดในสองหน้าใช้ `{ replace: true }` ครบ (ไม่มีจุดไหนหลุดเป็น push)
- [ ] `yarn build` และ `yarn lint` ผ่าน

**เอกสาร**
- [ ] `docs/rules_of_work.md` เพิ่มกติกา popover แบบ presentational ล้วน + กติกาปุ่มท้าย popover ต้องส่ง `?q=`
- [ ] ยืนยันว่า `docs/openapi.yaml` และ `frontend/public/openapi.yaml` **ไม่ถูกแก้** (ไม่มี API เปลี่ยน)

**Git**
- [x] PR #144 merge เข้า `main` แล้ว (`6253581`) — เงื่อนไขเริ่มงานครบ
- [ ] แตก branch ใหม่จาก `main` ล่าสุด (แนะนำ `feat/service-reference-popover`) และเข้า main ผ่าน PR เท่านั้น
- [ ] ระบุในคำอธิบาย PR ว่ารอบนี้ **เปลี่ยนพฤติกรรมปุ่มของ Address popover ด้วย** (ขอบเขตเพิ่มตามที่
      เจ้าของโปรเจกต์ตัดสินใจ) เพื่อให้ reviewer ไม่แปลกใจว่าทำไม diff แตะไฟล์ของ PR #144

---

### Final Acceptance — ทดสอบรวมครั้งเดียวหลังทุก Step เสร็จ (mock mode: `./pigate-backend -mock=true -allow-dev-cors` + `yarn dev`)

```json
{
  "final_acceptance": [
    "yarn build และ yarn lint ผ่าน · git diff --stat ไม่มีไฟล์ใน backend/ และไม่มี openapi.yaml ทั้งสองไฟล์",
    "หน้า Policy ทั้ง 3 (Firewall, Local-In, Local-Out): hover badge ในคอลัมน์ Service ค้าง 1 วินาที แล้ว popover เปิด แสดงชื่อ object + entries ครบ",
    "Service Object ที่มี entries หลายรายการแสดงครบทุกแถว; เกิน 5 รายการแสดง 5 แรก + 'และอีก N รายการ'",
    "Service Object ที่เป็น system (type === \"system\") แสดง badge System; entry ICMP แสดง 'ทุกประเภท' ไม่ใช่ 'ICMP -'",
    "badge ค่า ALL และชื่อที่ไม่มีใน Service Objects ไม่เปิด popover เลย (ไม่มี cursor/behavior เปลี่ยน)",
    "กดปุ่ม 'ดู Service Objects' ใน popover แล้วไปที่ /policy/services?q=<name> และตารางถูกกรองเหลือ object นั้นทันทีตอนโหลด",
    "พิมพ์ในช่องค้นหาหน้า Services แล้ว URL อัปเดต q ตาม, ลบข้อความจนว่างแล้ว q หายไปจาก URL, กด Back ไม่ย้อนทีละตัวอักษร",
    "กดปุ่ม 'ดู Address Objects' ใน popover ของ Source/Destination แล้วไปที่ /policy/addresses?q=<name> (ไม่ใช่ /policy/addresses เปล่า) และตารางหน้า Addresses ถูกกรองตามชื่อนั้นทันทีตอนโหลด",
    "พิมพ์ในช่องค้นหาหน้า Addresses แล้ว URL อัปเดต q ตาม, ลบข้อความจนว่างแล้ว q หายไปจาก URL และรายการ address กลับมาครบทุกตัว, กด Back ไม่ย้อนทีละตัวอักษร",
    "เปิด /policy/addresses ตรง ๆ (ไม่มี ?q=) แล้วรายการแสดงครบเหมือนเดิม; ตัวกรองชนิด (all/subnet/range/fqdn) และ checkbox เลือกหลายรายการยังทำงานปกติร่วมกับ ?q=",
    "เปิด RuleStatsDrawer ของ rule ที่เปิด Log: hover ชื่อใน 'Top Service' ที่ตรงกับ Service Object จริงแล้ว popover เปิดและ 'ไม่ถูก drawer บัง'",
    "แถว Top Service ที่ serviceName ว่าง (แสดงเป็น PROTO/PORT ดิบ) ไม่เปิด popover และปุ่ม 'ดู log ของรายการนี้' ยังทำงานเหมือนเดิม",
    "popover ของ Source/Destination (Address Object) ยังทำงานครบเหมือนเดิมทุกอย่าง รวม popover ระดับ 2 (hover entry ที่เป็น IP/FQDN) — diff ของ AddressObjectReferenceContent.tsx มีแค่บรรทัดปุ่ม navigate เท่านั้น",
    "เปิด DevTools Network: hover badge Service กี่ครั้งก็ตาม ไม่มี HTTP request เกิดขึ้นเลยแม้แต่เส้นเดียว",
    "drag-reorder แถว, เปิด/ปิด Drawer, เปิดฟอร์มแก้ rule และคลิก Combobox Service ในฟอร์ม ทำงานปกติทุกอัน (popover ไม่ค้างทับ)",
    "ดูทั้ง dark และ light mode: badge เรียงเหมือนเดิม ไม่มีเงา/blur และไม่มีคลาสสีดิบ (grep ไฟล์ที่แก้ทั้งหมด)"
  ]
}
```
