# DNS Record Table Sort & Filter — จัดเรียง/กรองตาราง Record ในหน้า DNS Server

> แผนงานฟีเจอร์: ตาราง "DNS Records" ในแท็บ Zones & Records ของหน้า DNS Server
> ปัจจุบันแสดงตามลำดับที่ backend คืนมา (ไม่มี ORDER BY) และมีแค่ช่องค้นหาข้อความ
> เดียว → เปลี่ยนเป็น **จัดเรียงได้ทุกคอลัมน์ (ค่าเริ่มต้น name ASC)** + **มีตัวกรอง
> ประเภท record เพิ่มจากช่องค้นหาเดิม**
>
> เขียนเมื่อ: 2026-09-02 · อ้างอิง branch: `main` (หลัง merge PR #163 dns-ns-delegation)
> Status ใน README Feature Status: DNS Server (local zones) = Completed → ไม่เปลี่ยน (เป็น UI polish)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็น)**
1. หัวตาราง Record คอลัมน์ `Host Name` / `Type` / `Value` / `TTL` กดเพื่อจัดเรียงได้
   สลับ asc ↔ desc มีไอคอนบอกทิศทาง และ `aria-sort` ถูกต้อง
2. เปิดหน้ามาครั้งแรก และทุกครั้งที่สลับโซน → เรียงตาม **name ASC** เสมอ
3. มีตัวกรองเพิ่มจากช่องค้นหาเดิม: dropdown **Type** (All / A / AAAA / CNAME / MX /
   TXT / PTR / NS) ทำงานร่วมกับช่องค้นหาข้อความแบบ AND
4. ช่องค้นหาข้อความค้นหา glue IP ของ NS record ได้ด้วย (ของใหม่จาก PR #161–163)
5. เมื่อกรองแล้วไม่พบข้อมูล ต้องบอกว่าเป็นเพราะตัวกรอง และมีปุ่มล้างตัวกรอง

**นอกขอบเขต (ตั้งใจตัดออก)**
- ตาราง **Blocked Domains** และ **Blocklists** ในแท็บอื่น — คอมโพเนนต์ที่สร้างใน T-01
  ออกแบบให้ reuse ได้ แต่แผนนี้ไม่ไปแตะตารางเหล่านั้น
- ไม่ sync sort/filter ลง URL query param (หน้านี้ใช้ `?tab=` อยู่แล้ว — ไม่ขยายเพิ่ม)
- ไม่ทำ pagination, ไม่ทำ multi-column sort, ไม่จำ state ข้าม session
- **ไม่แตะ backend / ไม่แก้ openapi.yaml** (ดูเหตุผลใน §2)

## 1. สถานะโค้ดปัจจุบัน (สำรวจจริงวันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| ตาราง Record + หัวตาราง 5 คอลัมน์ | มีอยู่ ไม่มี sort affordance | `frontend/src/pages/DnsServer.tsx:~1278-1341` (TableHead ~1281-1285) |
| ช่องค้นหา record (ข้อความเดียว) | มีอยู่ ค้นหา name/type/value | `DnsServer.tsx:~1267-1276` + state `recordSearchQuery` `:~141` |
| ตัวกรอง/จัดเรียง record | **ยังไม่มี** — `filteredRecords` เป็นแค่ `.filter()` ไม่มี `.sort()` | `DnsServer.tsx:~453-461` |
| ฟิลด์ของ record | `id, zoneId, name, type, value, ttl, glueIps?: string[]` | `frontend/src/data-mockup/mockData.ts:~741-752` |
| ชนิด record ที่รองรับ | A, AAAA, CNAME, MX, TXT, PTR, NS (ใน `<select>` ดิบของ Drawer) | `DnsServer.tsx:~2153-2166` |
| การแสดง NS + glue IP ใน cell Value | มีอยู่ (`value -> ip1, ip2`) | `DnsServer.tsx:~1306-1311` |
| API ที่ดึงข้อมูล | `GET /api/dns/zones` คืน zone พร้อม `records` มาทั้งก้อนครั้งเดียว ไม่มี pagination | `frontend/src/services/dnsServerService.ts:~133-143` |
| ลำดับที่ backend คืนมา | **ไม่มี `ORDER BY`** → ลำดับไม่ถูกกำหนด (ตาม rowid) | `backend/internal/db/repository.go:~3368` `GetDNSRecordsByZone` |
| UI primitive ของตาราง | `Table/TableHeader/TableHead/...` เป็น shadcn ล้วน **ไม่มีตัว sortable** | `frontend/src/components/ui/table.tsx:66-77` |
| แบบอย่าง sort/filter ที่มีในโปรเจกต์ | `Addresses.tsx:189` sort คงที่ด้วย `localeCompare`; filter chips `Addresses.tsx:~459-476` | — |
| `@tanstack/react-table` | อยู่ใน `package.json:23` แต่ **grep แล้วไม่ถูก import ที่ไหนเลยใน `src/`** | — |
| mock mode ฝั่ง frontend | `dnsServerService` มีสาขา `IS_MOCK_MODE` อ่าน/เขียน localStorage | `dnsServerService.ts:~134-137` |

สรุป: งานทั้งหมดกระจุกอยู่ที่ **`frontend/src/pages/DnsServer.tsx` + คอมโพเนนต์ UI ใหม่ 1 ตัว**
ไม่ต้องแตะ backend/DB/kernel เลย

## 2. แนวทางเชิงเทคนิค

### 2.1 เลือก client-side sort/filter (ไม่เพิ่ม query param ที่ backend)

เหตุผล:
1. **ข้อมูลอยู่ในหน่วยความจำครบอยู่แล้ว** — `GET /api/dns/zones` คืน records ของทุกโซน
   มาในครั้งเดียว (`dnsServerService.ts:~133`) การ sort ฝั่ง backend จะไม่ลด payload เลย
2. **ขนาดข้อมูลเล็ก** — DNS zone ของ home/SMB gateway โดยทั่วไปมีระดับสิบถึงร้อย record
   ต่อโซน การ sort/filter ใน `useMemo` ถือว่าฟรีเมื่อเทียบกับ round-trip HTTP
3. **หน้าเว็บแก้ state แบบ optimistic อยู่แล้ว** — create/update/delete record เขียนกลับเข้า
   `zones` state ตรง ๆ (`DnsServer.tsx:~758-780`, `~665-673`) ถ้าให้ backend เป็นคน sort
   record ที่เพิ่งเพิ่มจะไปโผล่ท้ายตารางจนกว่าจะ refresh — client-side sort จัดตำแหน่งให้ทันที
4. **ไม่เพิ่ม attack surface** — ไม่มี query param ใหม่ที่ต้อง validate, ไม่ต้องแก้
   `docs/openapi.yaml` + `frontend/public/openapi.yaml`, ไม่แตะ handler/repository
   (โปรเจกต์นี้ถือ input validation เป็นงาน sensitive — ทางที่ดีที่สุดคือไม่สร้างช่องใหม่)
5. **mock mode ทำงานเหมือนกันทันที** — สาขา localStorage ไม่ต้องเลียนแบบ logic sort ใด ๆ

ทางเลือกที่พิจารณาแล้วปฏิเสธ:
- **เพิ่ม `?sort=&order=&type=` ที่ `GET /api/dns/zones`** — ปฏิเสธ เพราะข้อ 1/3/4 ข้างบน
  (ต้องแก้ทั้ง handler, repository, openapi 2 ไฟล์ และ mock path เพื่อประโยชน์ 0)
- **ใช้ `@tanstack/react-table`** — ปฏิเสธ ถึงแม้จะมีใน `package.json` แต่**ไม่ถูกใช้ที่ไหนเลย**
  ในโค้ดปัจจุบัน การเริ่มใช้ที่ตารางเดียวจะทำให้เกิดสองแพตเทิร์นในโปรเจกต์ที่มีตารางมือเขียน
  ~20 ตาราง (เรื่องจะเก็บหรือถอด dependency นี้เป็นการตัดสินใจของเจ้าของโปรเจกต์ ไม่ใช่ของแผนนี้)
- **ทำ sort ใน `ORDER BY` ที่ `GetDNSRecordsByZone`** — ทำไม่ผิด แต่ไม่ตอบโจทย์ interactive
  sort และไม่ครอบ optimistic update (ยังคงเป็นตัวเลือกแยกในอนาคตถ้าอยากได้ลำดับ default
  ที่เสถียรฝั่ง server; แผนนี้แก้ที่ต้นเหตุด้วย tie-breaker แทน — ดู T-02)

### 2.2 คอมโพเนนต์: เพิ่ม `SortableTableHead` ใน `components/ui/`

ตาม `docs/rules_of_work.md` §1.1 (ห้าม ad hoc component) ให้สร้างไฟล์ใหม่ใน
`frontend/src/components/ui/sortable-table-head.tsx` ที่ **ประกอบจาก primitive ที่มีอยู่**
(`TableHead` + `Button variant="ghost"`) ไม่แก้ `components/ui/table.tsx` โดยตรง
(ไฟล์นั้นเป็นของ shadcn generator — การ re-add component ในอนาคตจะทับทิ้ง)

รูปแบบ props (ให้ reuse กับตารางอื่นได้ในอนาคต):

```tsx
export type SortDirection = "asc" | "desc"
export type SortState<K extends string = string> = { key: K; direction: SortDirection }

<SortableTableHead sortKey="name" sortState={recordSort} onSort={handleRecordSort} className="w-[25%] ...">
  Host Name
</SortableTableHead>
```

## 3. Task list (ทำเรียงตามลำดับ — T-02/T-03 แก้ไฟล์เดียวกัน อย่าทำขนาน)

### T-01 — UI primitive: `SortableTableHead`
- **layer:** frontend (components/ui)
- **files:** `frontend/src/components/ui/sortable-table-head.tsx` *(ไฟล์ใหม่)*
- **instruction:**
  - export `SortDirection`, `SortState<K>` และคอมโพเนนต์ `SortableTableHead`
  - props: `extends React.ComponentProps<typeof TableHead>` + `sortKey: string`,
    `sortState: SortState`, `onSort: (key: string) => void`
  - render `<TableHead>` (import จาก `@/components/ui/table`) โดยส่ง `className` ที่รับมา
    ผ่านต่อด้วย `cn(...)` จาก `@/lib/utils` และตั้ง
    `aria-sort={active ? (dir === "asc" ? "ascending" : "descending") : "none"}`
  - ข้างในใช้ `Button` จาก `@/components/ui/button` `variant="ghost" size="sm"`
    `type="button"` `onClick={() => onSort(sortKey)}`
    className แนว `-ml-2 h-7 cursor-pointer gap-1 px-2 text-xs font-medium text-muted-foreground hover:text-foreground`
    (ห้ามใส่สี palette ดิบ / ห้าม `shadow-*` / `backdrop-blur-*`)
  - ไอคอน `lucide-react`: active+asc → `ArrowUp`, active+desc → `ArrowDown`,
    ไม่ active → `ArrowUpDown` ใส่ `opacity-50`; ขนาด `h-3.5 w-3.5`
  - ไม่มี state ภายใน (controlled ล้วน) และไม่รู้จัก DNS อะไรทั้งสิ้น (generic)
  - เขียน doc comment สั้น ๆ ว่าใช้แทน `TableHead` ได้ตรง ๆ และ consumer เป็นคนถือ
    `SortState` เอง
- **acceptance:**
  - `yarn build` (tsc) ผ่าน — ระวัง `noUnusedLocals: true` ใน `tsconfig.app.json:22`
  - ไม่มีสี hardcode / ไม่มี shadow/backdrop-blur / ใช้เฉพาะ theme variable
- **depends_on:** []

### T-02 — DnsServer: state + comparator + wire หัวตาราง (ฟีเจอร์ sort ครบในทาสก์เดียว)
- **layer:** frontend (pages)
- **files:** `frontend/src/pages/DnsServer.tsx`
- **instruction:**
  1. module scope (นอก component, ใกล้ ๆ constant เดิมบรรทัด ~83-88):
     `const recordCollator = new Intl.Collator(undefined, { numeric: true, sensitivity: "base" })`
     (สร้างครั้งเดียว; `numeric: true` ทำให้ `host2` มาก่อน `host10` และ IP อ่านง่ายขึ้น)
  2. เพิ่ม type + state ใกล้ ๆ `recordSearchQuery` (~141):
     ```ts
     type RecordSortKey = "name" | "type" | "value" | "ttl"
     const [recordSort, setRecordSort] = useState<SortState<RecordSortKey>>({ key: "name", direction: "asc" })
     ```
  3. `handleRecordSort(key: string)`: ถ้า `key === recordSort.key` → สลับ asc/desc,
     ถ้าไม่ใช่ → `{ key, direction: "asc" }` (ไม่มีสถานะที่สาม "ไม่เรียง")
  4. แก้ `useMemo` `filteredRecords` (~453-461) → เปลี่ยนชื่อเป็น `visibleRecords`
     และต่อท้าย `.sort()` **บน array ที่ได้จาก `.filter()`** (เป็นสำเนาอยู่แล้ว ห้าม sort
     `selectedZone.records` ตรง ๆ):
     - `name`/`type`/`value`: `recordCollator.compare(a.x, b.x)`
     - `ttl`: `a.ttl - b.ttl`
     - คูณด้วย `recordSort.direction === "asc" ? 1 : -1`
     - **tie-breaker เสมอ**: `|| recordCollator.compare(a.name, b.name) || recordCollator.compare(a.id, b.id)`
       เพราะ backend ไม่มี `ORDER BY` (repository.go:~3368) ลำดับดิบจึงไม่ถูกกำหนด
     - คอมเมนต์กำกับว่า sort คอลัมน์ Value ใช้ `rec.value` เท่านั้น ไม่รวม `glueIps`
       (glue เป็นข้อมูลประกอบการแสดงผล)
     - deps ของ `useMemo` ต้องมี `recordSort` ด้วย
  5. เปลี่ยนจุดที่ใช้ `filteredRecords` (~1289 และ ~1296) ให้เป็น `visibleRecords`
  6. แก้ `TableHeader` (~1281-1285): คอลัมน์ Host Name / Type / Value / TTL เปลี่ยนเป็น
     `<SortableTableHead sortKey="name|type|value|ttl" sortState={recordSort} onSort={handleRecordSort} className="<คลาสเดิมของคอลัมน์นั้น>">`
     — **คง `w-[25%] / w-[15%] / w-[40%] / w-[10%]` และคลาส text เดิมไว้ทุกตัว**
     คอลัมน์ปุ่ม action (ตัวสุดท้าย) ยังเป็น `TableHead` ธรรมดา
  7. อย่าแตะ cell Value ที่แสดง `value -> glueIps` (~1306-1311)
- **acceptance:**
  - `yarn build` + `yarn lint` ผ่าน (ไม่มีตัวแปรค้าง)
  - โหลดหน้ามาแล้วตารางเรียงตาม name ASC และหัวคอลัมน์ Host Name แสดงไอคอน `ArrowUp`
  - ตรงตาม instruction ทุกข้อ (ยังไม่ต้องทดสอบรวมทั้งฟีเจอร์)
- **depends_on:** ["T-01"]

### T-03 — DnsServer: ตัวกรอง Type + ขยายการค้นหา + empty state
- **layer:** frontend (pages)
- **files:** `frontend/src/pages/DnsServer.tsx`
- **instruction:**
  1. module scope: สร้างแหล่งข้อมูลชนิด record แหล่งเดียว
     ```ts
     const DNS_RECORD_TYPE_OPTIONS = [
       { value: "A", label: "A (Address)" }, { value: "AAAA", label: "AAAA (IPv6 Address)" },
       { value: "CNAME", label: "CNAME (Alias)" }, { value: "MX", label: "MX (Mail Exchange)" },
       { value: "TXT", label: "TXT (Text)" }, { value: "PTR", label: "PTR (Pointer)" },
       { value: "NS", label: "NS (Name Server)" },
     ] as const
     ```
     แล้ว **ใช้ list นี้ render `<option>` ใน `<select id="rec-type">` เดิม (~2159-2165) แทน
     การเขียนซ้ำ** เพื่อกันชนิดใหม่ในอนาคตหลุดจากตัวกรอง (ห้ามเปลี่ยน `<select>` ดิบตัวนั้น
     เป็น shadcn `Select` ในทาสก์นี้ — คนละเรื่อง เก็บ diff ให้เล็ก)
  2. state ใหม่: `const [recordTypeFilter, setRecordTypeFilter] = useState<string>("all")`
  3. ใน `visibleRecords` (T-02): คำนวณ `const q = recordSearchQuery.trim().toLowerCase()` ครั้งเดียว
     แล้วเงื่อนไข = `matchText && matchType` โดย
     - `matchText`: `q === ""` หรือ ตรงกับ `name` / `type` / `value` /
       **`(r.glueIps ?? []).some(ip => ip.toLowerCase().includes(q))`** (ของใหม่จาก NS delegation)
     - `matchType`: `recordTypeFilter === "all" || r.type === recordTypeFilter`
     - เพิ่ม `recordTypeFilter` เข้า deps
  4. UI แถวเครื่องมือ (~1267-1276): ครอบช่องค้นหาเดิมกับ dropdown ใหม่ด้วย
     `<div className="flex flex-col gap-2 sm:flex-row sm:items-center">` — ช่องค้นหา `sm:flex-1`
     (คง `Search` icon + `Input` เดิม), dropdown ใช้ **shadcn `Select`** ที่ import อยู่แล้วในไฟล์นี้
     (`SelectTrigger className="h-9 w-full text-xs sm:w-[170px]"`) รายการ = `All Types` +
     `DNS_RECORD_TYPE_OPTIONS` (แสดงเฉพาะรหัสชนิด เช่น `A`, `AAAA` ให้แถบสั้น)
  5. empty state (~1289-1294): แยกข้อความ 2 กรณี — ถ้ามี `q` หรือ `recordTypeFilter !== "all"`
     → `"ไม่พบระเบียน DNS ตามเงื่อนไขที่กรอง"` + ปุ่ม `Button variant="outline" size="sm"`
     ข้อความ `ล้างตัวกรอง` ที่ `setRecordSearchQuery("")` + `setRecordTypeFilter("all")`;
     ถ้าไม่มีตัวกรอง → ข้อความเดิม `"ยังไม่มีข้อมูลระเบียน DNS ในโซนนี้"`; คง `colSpan={5}`
  6. (ไม่บังคับ) เพิ่ม `Badge variant="secondary"` แสดง `visibleRecords.length` ข้าง
     CardTitle ให้ล้อกับการ์ด DNS Zones (~1133)
  7. **ไม่** รีเซ็ต sort/filter เมื่อสลับโซน (คงพฤติกรรมเดิมของช่องค้นหาที่ไม่รีเซ็ต) —
     ปุ่ม "ล้างตัวกรอง" ในข้อ 5 คือทางออกให้ผู้ใช้
- **acceptance:**
  - `yarn build` + `yarn lint` ผ่าน
  - ตัวเลือกชนิดใน dropdown ตัวกรองกับใน Drawer มาจาก list เดียวกัน (แก้ที่เดียว)
  - ตรงตาม instruction ทุกข้อ
- **depends_on:** ["T-02"]

### T-04 — docs: บันทึกกติกาหัวตารางที่จัดเรียงได้ (optional, ทำท้ายสุด)
- **layer:** docs
- **files:** `docs/rules_of_work.md`
- **instruction:** เพิ่มหัวข้อย่อยใน §1 (เช่น 1.5) ระบุว่า ตารางที่ต้องการ sort ให้ใช้
  `SortableTableHead` จาก `@/components/ui/sortable-table-head` เท่านั้น ห้ามผูก `onClick`
  กับ `TableHead` เอง, ให้ผู้ใช้งานถือ `SortState` เอง, และ default ของทุกตารางควรเป็น
  คอลัมน์ชื่อ ASC พร้อม tie-breaker ที่ deterministic
- **acceptance:** ข้อความภาษาไทยตามสไตล์เอกสารเดิม ไม่ขัดกับ §1.1
- **depends_on:** ["T-03"]

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | หมายเหตุ |
|---|---|---|---|
| GET | `/api/dns/zones` | authRoute (เดิม) | **ไม่เปลี่ยน signature/พฤติกรรม** — sort/filter ทำฝั่ง client ล้วน |

ไม่มี endpoint ใหม่ → **ไม่ต้องแก้ `docs/openapi.yaml` และ `frontend/public/openapi.yaml`**
และโหมด `-disable-edit=true` ไม่เกี่ยวข้อง (ฟีเจอร์นี้เป็นการอ่านล้วน ไม่มี mutation ใหม่)

## 5. ข้อควรระวัง

1. **`noUnusedLocals: true` (`tsconfig.app.json:22`)** — ถ้าแยกทาสก์ "ใส่ state sort" กับ
   "wire หัวตาราง" ออกจากกัน ทาสก์แรกจะ build ไม่ผ่านเพราะตัวแปรไม่ถูกใช้ →
   จึงรวมไว้ใน T-02 ทาสก์เดียวโดยเจตนา
2. **ห้ามเก็บ record ที่ sort แล้วไว้ใน `useState`** — create/update/delete แก้ `zones`
   แบบ optimistic (`DnsServer.tsx:~665-673`, `~758-780`) ถ้า cache ลง state จะค้างเป็นข้อมูลเก่า
   ทันทีที่ผู้ใช้เพิ่ม record; ต้องคำนวณผ่าน `useMemo` จาก `selectedZone` เท่านั้น
3. **ห้าม sort array ต้นฉบับในที่ (`in place`)** — `selectedZone.records` เป็น object เดียวกับที่อยู่ใน
   state `zones`; `Array.prototype.sort` แก้ของเดิม ถ้าเผลอเรียกบน `records` ตรง ๆ จะ mutate state
   (React จะไม่ re-render และลำดับใน state จะเพี้ยนถาวร) — ให้ sort ผลลัพธ์จาก `.filter()` เท่านั้น
4. **backend ไม่มี `ORDER BY`** (`repository.go:~3368`) → เมื่อค่าที่ใช้เรียงเท่ากัน ลำดับจะไม่แน่นอน
   ระหว่าง reload ต้องมี tie-breaker (`name` แล้ว `id`) ไม่งั้น QA จะเจออาการ "แถวสลับเอง"
5. **TTL ต้องเรียงแบบตัวเลข** — ถ้าเผลอใช้ `localeCompare` กับ `String(ttl)` จะได้
   `300 < 3600 < 86400` ผิดเป็น `300 < 86400 < 3600`; ให้ลบกันตรง ๆ
6. **NS glue IPs (ของใหม่จาก PR #161-163)** — cell Value แสดง `value -> ip1, ip2`
   (`~1306-1311`) ห้ามแก้โครง cell นี้; การค้นหาต้องครอบ `glueIps` ด้วย มิฉะนั้นผู้ใช้ค้น IP
   ของ nameserver แล้วไม่เจอทั้งที่เห็นอยู่บนจอ
7. **ตาราง Record ถูก render เฉพาะโซน `isAuthoritative`** (~1265) — สาขา Forward zone
   (~1343-1352) ห้ามแตะ และห้ามเผลอย้าย state ที่ใช้เฉพาะตารางไปคำนวณในสาขานั้น
8. **กติกาสไตล์ (`docs/rules_of_work.md` §2)** — ใช้เฉพาะ theme variable
   (`text-muted-foreground`, `text-foreground`, `bg-primary/10` ฯลฯ) ห้าม palette ดิบ
   เช่น `text-emerald-500`, ห้าม `shadow-*` / `backdrop-blur-*`, ต้องดูดีทั้ง dark และ light
9. **ความกว้างคอลัมน์** — คลาส `w-[25%]/w-[15%]/w-[40%]/w-[10%]` อยู่บน `TableHead`
   ถ้าย้ายมาไว้บนปุ่มข้างในแทน ตารางจะยุบ/กระตุกตอน sort; ต้องส่งผ่าน `className` ไปที่
   `TableHead` เหมือนเดิม
10. **Accessibility** — ปุ่ม sort ต้องเป็น `<button type="button">` (ผ่าน shadcn `Button`)
    เพื่อให้ Tab/Enter/Space ใช้ได้ และ `aria-sort` ต้องอยู่บน `<th>` ไม่ใช่บนปุ่ม
11. **git** — ทำบน branch `feat/dns-record-table-sort-filter` และเข้า main ผ่าน PR เท่านั้น
    (เป็นการแก้โค้ด frontend ไม่ใช่ docs-only) ห้าม commit เว้นแต่เจ้าของสั่ง

## 6. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ทำครั้งเดียวหลัง T-01..T-04 เสร็จครบ)

รันด้วย `cd backend && ./pigate-backend -port=8081 -db=/tmp/pigate-test.db -mock=true -allow-dev-cors`
คู่กับ `cd frontend && yarn dev` แล้วเข้า `/network/dns-server?tab=zones`

- [ ] `cd frontend && yarn build` และ `yarn lint` ผ่านทั้งคู่ ไม่มี error/warning ใหม่
- [ ] โหลดหน้าครั้งแรก: ตาราง Record เรียง **name ASC** และไอคอนบนคอลัมน์ Host Name เป็นลูกศรขึ้น
- [ ] สลับโซนซ้ายมือแล้วโซนใหม่ยังเรียงตามค่าที่เลือกอยู่ และไม่มีแถวค้างจากโซนก่อน
- [ ] คลิก Host Name สลับ ASC ↔ DESC ได้ ไอคอนเปลี่ยนตาม และ `aria-sort` บน `<th>` ถูกต้อง (ดูใน DevTools)
- [ ] คลิก Type / Value / TTL เรียงได้ทั้ง 2 ทิศ; คอลัมน์ที่ไม่ได้เลือกกลับเป็นไอคอนกลาง
- [ ] TTL เรียงแบบตัวเลขจริง (สร้าง record TTL 300 / 3600 / 86400 → ASC ต้องได้ 300, 3600, 86400)
- [ ] ชื่อแบบมีเลขเรียงแบบ natural (สร้าง `host2`, `host10` → ASC ต้องได้ `host2` ก่อน `host10`)
- [ ] record apex (`@`) ไม่ทำให้ sort พัง (ไม่มี exception, อยู่ตำแหน่งคงที่ทุกครั้งที่ reload)
- [ ] ตัวกรอง Type: เลือก `NS` เหลือเฉพาะ NS, เลือก `All Types` กลับมาครบ
- [ ] ค้นหาข้อความ + ตัวกรอง Type ทำงานร่วมกันแบบ AND
- [ ] NS record ที่มี glue IP: ยังแสดง `ns1.example.com -> 10.0.0.53` เหมือนเดิม และ**ค้นหาด้วย glue IP แล้วเจอ**
- [ ] เมื่อกรองจนไม่เหลือแถว: ขึ้นข้อความว่ากรองไม่พบ + ปุ่ม "ล้างตัวกรอง" กดแล้วรายการกลับมาครบและ dropdown กลับเป็น All
- [ ] ขณะเรียง DESC อยู่ → Add Record ใหม่ แถวใหม่ไปโผล่ตำแหน่งที่ถูกต้องทันทีโดยไม่ต้อง refresh
      และแถบ "Apply DNS Zones" ยังขึ้นตามปกติ
- [ ] แก้ไข (Edit) record ที่กำลังถูกกรองอยู่ → หลังบันทึกรายการยังคงถูกกรอง/เรียงถูกต้อง
- [ ] ลบ record → ไม่มีแถวค้าง ไม่มี error console
- [ ] สลับ dark / light mode: หัวตาราง ไอคอน และ dropdown อ่านออกชัดเจนทั้งสองโหมด
- [ ] `grep -nE "shadow-|backdrop-blur-|text-(emerald|red|blue|green|amber|slate)-[0-9]" frontend/src/pages/DnsServer.tsx frontend/src/components/ui/sortable-table-head.tsx` ไม่พบผลลัพธ์ในโค้ดที่เพิ่มใหม่
- [ ] คีย์บอร์ด: Tab ไปที่หัวคอลัมน์แล้วกด Enter/Space สั่ง sort ได้ และมี focus ring มองเห็น
- [ ] แท็บ Blocked Domains / Blocklists / Settings ยังทำงานเหมือนเดิม (ไม่มี regression จากการแก้ไฟล์เดียวกัน)
- [ ] ตรวจว่าไม่มีการแก้ไฟล์ backend หรือ openapi ใน diff (`git diff --stat` ควรเห็นเฉพาะ frontend + docs)
