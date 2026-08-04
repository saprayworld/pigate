# Statistics → Traffic → Host: การ์ด header ของเครื่องที่เลือก + Accuracy เป็นปุ่มไอคอน Popover

> เอกสารแผนงานสำหรับหน้า `/statistics/traffic/host/:ip` (ไฟล์ `frontend/src/pages/StatisticsTrafficHost.tsx`)
> (A) เปลี่ยนหัวหน้าเพจเป็น **การ์ด host header** ตามภาพตัวอย่างของเจ้าของโปรเจกต์
> (ไอคอนวงกลม + IP ตัวใหญ่ + badge สถานะ + แถว metadata + ชุดควบคุมชิดขวา/ตกลงแถวล่างเมื่อจอเล็ก)
> (B) เปลี่ยน badge ข้อความ "ใกล้เคียงจริง / ประมาณการ" เป็น **ปุ่มไอคอน `i` + Popover**
>
> วันที่เขียน: 2026-08-04 · แก้ไขครั้งที่ 1: 2026-08-04 (เจ้าของตอบ D-1…D-4 แล้ว — ล็อกขอบเขต)
> อ้างอิงโค้ด: `main` @ `443b4a2`
>
> **สถานะ: ล็อกแล้ว พร้อมให้ ai-developer ลงมือ T-01…T-06 ตามลำดับ (ไม่มีทางเลือกค้าง)**
>
> **Branch ที่ใช้: `feat/statistics-traffic-host-header` (แตกจาก `main`)**
> กติกาเดิม: ห้าม push `main` (โค้ด), ห้าม commit เว้นแต่เจ้าของสั่ง, เข้า PR เท่านั้น

## 0. การตัดสินใจของเจ้าของโปรเจกต์ (ล็อกแล้ว ห้ามเปิดใหม่)

| # | คำถาม | คำตอบที่ล็อก |
|---|---|---|
| D-1 | ฟิลด์ "Group" ในภาพตัวอย่าง | **ตัดทิ้ง** — แถว metadata แสดงเฉพาะ Hostname / MAC / Domain / Local-Internet ที่มีข้อมูลจริงเท่านั้น **ไม่แตะ backend** ไม่เพิ่มฟิลด์ `iface` |
| D-2 | badge "Active" | **เตรียม UI เฉย ๆ** — มีแค่ `active?: boolean` ในโครง type ฝั่ง frontend (+ mock เซ็ตให้เห็น layout) โหมดจริงยังไม่มี logic เช็ค **ไม่แตะ backend** |
| D-3 | ชุดควบคุมฝั่งขวาของการ์ด | **`[ปุ่มไอคอน i (accuracy popover)]` + `[Time Window selector 15m…24h]` + `[Refresh]`** ทั้งสามตัวย้ายเข้าไปอยู่ในการ์ด (เดิม window selector อยู่บนแถวหัวเพจ) · จอเล็กทั้งชุดตกลงแถวล่างสุดของการ์ด · **ไม่มีปุ่ม Block Host / Export PCAP** |
| D-4 | ขอบเขตการเปลี่ยน badge → ปุ่มไอคอน | **เปลี่ยนที่คอมโพเนนต์กลาง** `AccuracyBadge` ใน `TrafficStatsShared.tsx` → กระทบ Overview + Traffic + TrafficHost พร้อมกัน · **ไม่แตะ Dashboard** (มีก๊อปปี้ของตัวเอง) |

**นอกขอบเขต (ห้าม ai-developer ทำ)**
- ไม่แตะ backend เลยในแผนนี้ (ไม่มี Go, ไม่มี openapi, ไม่มี migration, ไม่มี kernel layer)
- ไม่เพิ่มปุ่ม/ฟีเจอร์ Block Host, Export PCAP หรือ action ใด ๆ ที่ระบบยังไม่มี
- ไม่แตะ Dashboard, `dashboardService.ts`, หน้า DNS ทั้ง 3
- ไม่แตะ logic การคำนวณ bytes/rate/series/percent ทั้งของจริงและ mock
- ไม่เปลี่ยนพฤติกรรมของ `?window=` / `?role=` / auto-refresh 10 วินาที / สถานะ `found=false` / `TruncatedWarning`

## 1. สถานะปัจจุบัน (สำรวจแล้ว ก่อนวางแผน)

| ส่วน | สถานะจริง | ไฟล์:บรรทัด (~) |
|---|---|---|
| หัวหน้าเพจ drill-down | **ไม่มีการ์ด** — เป็น `div.flex` ธรรมดา: ปุ่ม "กลับไปหน้า Traffic" + `<h1>` (hostname หรือ IP) + `<span>` IP ตัวเล็ก + badge domain + badge LAN/Internet | `StatisticsTrafficHost.tsx:358-406` |
| ชุดควบคุมมุมขวาบน (ปัจจุบัน) | `AccuracyBadge` (391) → `StatsWindowTabs` (392) → ปุ่ม `Refresh` (393-404) อยู่ใน `div.flex flex-wrap items-center gap-2` เดียวกัน | `StatisticsTrafficHost.tsx:390-405` |
| Time Window selector | คอมโพเนนต์ `StatsWindowTabs` (segmented control 7 ปุ่ม 15m/30m/1H/3H/6H/12H/24H จาก commit `8bed53b`) — ตัว state คือ `useStatsWindow()` ที่อ่าน/เขียน `?window=` ใน URL, หน้านี้เรียกที่บรรทัด 271 (`const [window_, setWindow] = useStatsWindow()`) แล้วเรนเดอร์ที่บรรทัด 392 | `components/statistics/DnsStatsShared.tsx:33-95` |
| ปุ่ม action อื่นในหน้านี้ | **ไม่มีเลย** — ไม่มี block host / export / add rule ที่ไหนในหน้า Statistics ทั้ง 6 หน้า | ตรวจทั้ง `pages/Statistics*.tsx` |
| ฟิลด์ที่ backend ส่งมาจริงสำหรับ host นี้ | `ip`, `hostname`, `mac`, `domain`, `private`, `window`, `accuracy`, `truncated`, `limit`, `found`, `totalBytes*`, `percentOfObserved`, `observedBytes`, `asSource`, `asDestination`, `series`, `generatedAt`, `currentRateBps*`, `rateSampledAt` | `model/statistics.go:345-410` · `services/trafficStatisticsService.ts:76-120` |
| ที่มาของ `hostname`/`mac` | `hostnameFor()` — DHCP active lease ก่อน แล้วค่อย DHCP reservation; ถ้าไม่เจอ hostname = IP และ mac = `""` | `service/traffic_stats.go:1532-1543` |
| **ไม่มีฟิลด์ group / VLAN** | `TrafficHostDetail` ไม่มี group/vlan/zone/interface · VLAN ในระบบมีแค่ระดับ interface (`model/types.go:202-205`) ไม่ผูกกับ IP → **D-1 ตัดทิ้ง** | — |
| **ไม่มีฟิลด์ active/online** | ไม่มีการเช็ค reachability ที่ใดใน traffic stats → **D-2 เตรียม UI เท่านั้น** | — |
| `AccuracyBadge` | คอมโพเนนต์กลางใน `TrafficStatsShared.tsx:166-179` ใช้ **3 หน้า** (Overview:413, Traffic:241, TrafficHost:391) · **Dashboard มีก๊อปปี้ของตัวเอง** (`Dashboard.tsx:792-805` ใช้ที่ 817/865) | — |
| ข้อความอธิบาย accuracy | **ยังไม่เคยมีใน UI** — มีแต่คอมเมนต์ในโค้ด (`Dashboard.tsx:786-791`): `estimated` = มีแต่ poll conntrack ทุก ~10 วิ, `near-exact` = มี DESTROY event listener ด้วย | — |
| `components/ui/popover.tsx` | **ยังไม่มี** ต้องเพิ่ม (โปรเจกต์ติดตั้ง `radix-ui` unified package v1.5 แล้ว — `tooltip.tsx` import จาก `"radix-ui"`) | `frontend/package.json:25` |
| mock mode | `getHostDetail` โหมด mock ประกอบข้อมูลเองครบทุกฟิลด์ → ฟิลด์ใหม่ต้องเติมใน mock ด้วย | `trafficStatisticsService.ts:323-434` |

**สรุปหนึ่งบรรทัด:** งานทั้งหมดเป็น **frontend ล้วน** — ย้ายของที่มีอยู่แล้ว (title/badges/accuracy/window selector/refresh)
เข้าไปจัดใหม่ในการ์ดเดียว + เพิ่ม Popover หนึ่งตัว ไม่มีข้อมูลใหม่จาก backend

## 2. โครงหน้าใหม่ (สรุปให้เห็นภาพก่อนลงมือ)

```
[ปุ่ม ← กลับไปหน้า Traffic]                          ← แถวบน (นอกการ์ด) เหลือแค่ปุ่มเดียว

┌─ Card ────────────────────────────────────────────────────────────────────┐
│ (◻)  172.24.25.24  [Active]* [Local|Internet] [domain]*   [i] [15m…24H] [⟳]│
│      Hostname: nas-01 · MAC: a1:b2:… · Domain: example.com                 │
└───────────────────────────────────────────────────────────────────────────┘
        * แสดงแบบมีเงื่อนไข: Active เมื่อ private && active===true, domain เมื่อมีค่า

จอเล็ก (< sm): ชุด [i] [window] [⟳] ตกลงมาเป็นแถวล่างสุดของการ์ด ชิดขวา
```

## 3. Task list (ล็อกแล้ว — ทำตามลำดับ T-01 → T-06)

```json
[
  {
    "task_id": "T-01",
    "title": "เพิ่ม shadcn Popover เข้า components/ui",
    "layer": "frontend",
    "files": ["frontend/src/components/ui/popover.tsx"],
    "instruction": "รันใน frontend/: `npx shadcn@latest add popover` (ห้ามใช้ yarn dlx — Yarn v1 ไม่มี). ตรวจไฟล์ที่ได้: (1) ต้อง import จาก \"radix-ui\" แบบเดียวกับ components/ui/tooltip.tsx (`import { Popover as PopoverPrimitive } from \"radix-ui\"`) ไม่ใช่ @radix-ui/react-popover — ถ้า generator ออกมาเป็นแบบหลังให้แก้ import และห้ามเพิ่ม dependency ใหม่ลง package.json (2) ลบคลาส shadow-* / backdrop-blur-* ที่ติดมากับ PopoverContent ให้หมด ตาม docs/rules_of_work.md (flat design only) เหลือ border + bg-popover + text-popover-foreground (3) ห้ามใส่สีพาเลตต์ดิบ ใช้ตัวแปรธีมเท่านั้น (4) ห้ามแก้ไฟล์อื่นใน components/ui",
    "acceptance": [
      "มีไฟล์ frontend/src/components/ui/popover.tsx export Popover / PopoverTrigger / PopoverContent (+ PopoverAnchor ถ้ามี)",
      "ไม่มีคำว่า shadow- หรือ backdrop-blur- ในไฟล์",
      "package.json ไม่มี dependency ใหม่เพิ่ม",
      "`yarn build` ผ่าน"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "แปลง AccuracyBadge เป็นปุ่มไอคอน i + Popover (คอมโพเนนต์กลาง)",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/TrafficStatsShared.tsx"],
    "instruction": "ใน TrafficStatsShared.tsx แทนที่ AccuracyBadge (บรรทัด ~163-179) ด้วยคอมโพเนนต์ใหม่ชื่อ AccuracyInfoButton (prop เดิม `{ accuracy?: \"estimated\" | \"near-exact\" }`) ที่เรนเดอร์เป็น <Button variant=\"outline\" size=\"sm\"> ครอบไอคอน lucide `Info` เท่านั้น (ไม่มีข้อความ) ความสูงเท่ากับปุ่ม Refresh ข้าง ๆ (size=\"sm\" + ไอคอน size-4 + cursor-pointer + aria-label=\"รายละเอียดความแม่นยำของข้อมูล\") ห่อด้วย <Popover><PopoverTrigger asChild>…</PopoverTrigger><PopoverContent align=\"end\" className=\"w-80 space-y-2 text-xs\">…</PopoverContent></Popover>. เนื้อใน Popover: บรรทัดแรกเป็นหัวข้อสถานะปัจจุบัน — near-exact แสดง 'ใกล้เคียงจริง' (text-primary), estimated แสดง 'ประมาณการ' (text-muted-foreground) — ตามด้วยคำอธิบาย 2 ย่อหน้า text-muted-foreground: (ก) 'ใกล้เคียงจริง = ระบบนับไบต์จากทั้ง event ตอน conntrack ปิด flow (DESTROY) และการ poll ทุก ~10 วินาที จึงเก็บ flow สั้น ๆ ได้เกือบครบ' (ข) 'ประมาณการ = ขณะนี้นับจากการ poll conntrack ทุก ~10 วินาทีอย่างเดียว flow ที่เกิดและจบภายในช่วง poll เดียวกันอาจตกหล่นได้' ปิดท้ายด้วย 'ข้อมูลทั้งหมดเก็บใน RAM เท่านั้น เริ่มนับใหม่ทุกครั้งที่ restart pigate'. ห้าม hardcode สี Tailwind ดิบ (ใช้ text-primary / text-muted-foreground / border-border) ห้าม shadow-*/backdrop-blur-*. ลบ export AccuracyBadge ทิ้ง (ผู้เรียกจะแก้ใน T-03) ห้ามทิ้ง export ที่ไม่มีคนใช้. ห้ามแตะ Dashboard.tsx ซึ่งมีก๊อปปี้ของตัวเอง",
    "acceptance": [
      "TrafficStatsShared.tsx export AccuracyInfoButton และไม่มี AccuracyBadge เหลืออยู่",
      "ใช้ Popover/PopoverTrigger/PopoverContent จาก @/components/ui/popover และ Button จาก @/components/ui/button เท่านั้น",
      "ไม่มีคลาสสีดิบ (emerald/red/…); ไม่มี shadow-*/backdrop-blur-*",
      "ไฟล์คอมไพล์ผ่าน (ผู้เรียกจะถูกแก้ใน T-03)"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-03",
    "title": "อัปเดตผู้เรียก AccuracyBadge ทั้ง 3 หน้า",
    "layer": "frontend",
    "files": [
      "frontend/src/pages/StatisticsOverview.tsx",
      "frontend/src/pages/StatisticsTraffic.tsx",
      "frontend/src/pages/StatisticsTrafficHost.tsx"
    ],
    "instruction": "เปลี่ยน import และจุดใช้งาน `<AccuracyBadge accuracy={…} />` เป็น `<AccuracyInfoButton accuracy={…} />` ทั้ง 3 หน้า โดยคงลำดับใน control bar เดิมไว้เป๊ะ (ปุ่มข้อมูล → StatsWindowTabs → Refresh) ตาม docs/ref/todo/statistics-window-granularity-plan.md. สำหรับ StatisticsTrafficHost.tsx แก้แค่ชื่อคอมโพเนนต์ในขั้นนี้ (การย้ายเข้าการ์ดทำที่ T-06). ห้ามแตะ Dashboard.tsx",
    "acceptance": [
      "ไม่มีการอ้างอิง AccuracyBadge เหลือใน frontend/src ยกเว้นก๊อปปี้ภายใน Dashboard.tsx ที่ไม่ถูกแตะ",
      "`yarn build` และ `yarn lint` ผ่าน",
      "ลำดับ control bar ของ Overview และ Traffic ยังเป็น ปุ่มข้อมูล → ตัวเลือกช่วงเวลา → Refresh"
    ],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-04",
    "title": "เพิ่มฟิลด์ active (optional) ใน type + mock ของ TrafficHostDetail",
    "layer": "frontend",
    "files": ["frontend/src/services/trafficStatisticsService.ts"],
    "instruction": "ใน interface TrafficHostDetail เพิ่มฟิลด์ `active?: boolean` พร้อมคอมเมนต์ชัดเจนว่า: backend ยังไม่ส่งฟิลด์นี้มา (เตรียม UI ไว้ตามแผน statistics-traffic-host-header-plan.md D-2) ความหมายที่ตั้งใจคือ 'IP นี้เป็น LAN host ที่ยังใช้งานอยู่' และมีความหมายเฉพาะเมื่อ private === true — UI ต้องแสดง badge Active ก็ต่อเมื่อ `private && active === true` ห้ามตีความ undefined ว่า active. ในสาขา IS_MOCK_MODE ของ getHostDetail ส่ง `active: mockIsPrivate(ip) ? true : undefined` เพื่อให้เห็น layout ตอน yarn dev. ห้ามแตะ logic การคำนวณ bytes/percent/series ใด ๆ และห้ามแตะ backend",
    "acceptance": [
      "TrafficHostDetail มี active?: boolean พร้อมคอมเมนต์อธิบายที่มา/ข้อจำกัด",
      "mock mode ส่ง active = true เฉพาะ IP ที่ private",
      "โหมดจริง (fetch) ไม่ถูกแก้พฤติกรรมใด ๆ",
      "`yarn build` ผ่าน"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-05",
    "title": "สร้างคอมโพเนนต์ HostHeaderCard (รวม i + window selector + refresh)",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/HostHeaderCard.tsx"],
    "instruction": "สร้างคอมโพเนนต์ใหม่ pure presentational (ห้ามมี fetch / useNavigate / useSearchParams ภายใน — ตามแนวเดียวกับ TrafficStatsShared.tsx) signature: `HostHeaderCard({ ip, detail, isLoading, window, onWindowChange, onRefresh }: { ip: string; detail: TrafficHostDetail | null; isLoading: boolean; window: StatsWindow; onWindowChange: (w: StatsWindow) => void; onRefresh: () => void })` — รับ ip แยกจาก detail เพราะ detail เป็น null ได้ตอนโหลด. โครงสร้าง: <Card><CardContent className=\"flex flex-wrap items-start justify-between gap-4 pt-4\"> โดย\n(1) กลุ่มซ้าย (`flex min-w-0 items-start gap-3`): <div className=\"flex size-10 shrink-0 items-center justify-center rounded-lg bg-primary/10 text-primary\"> ไอคอน lucide `Monitor` (ใช้ `Globe` แทนเมื่อ detail?.private === false) — รูปแบบเดียวกับหัวหน้า StatisticsTraffic.tsx:230-232 — ตามด้วยคอลัมน์ข้อความ: บรรทัดแรก IP ตัวใหญ่ `truncate font-mono text-xl font-bold tracking-tight` + badge สถานะ: <Badge variant=\"outline\" className=\"border-primary/30 text-primary\">Active</Badge> เฉพาะเมื่อ `detail?.private && detail?.active === true`, badge Local/Internet (ยกโค้ดเดิมจาก StatisticsTrafficHost.tsx:376-387 มา ไม่เปลี่ยนคลาส) และ badge domain ถ้ามี (ยกจาก 371-375). บรรทัดที่สอง: แถว metadata `text-xs text-muted-foreground` ประกอบจาก **เฉพาะฟิลด์ที่มีค่าจริง** คั่นด้วย ' · ' ได้แก่ `Hostname: {detail.hostname}` (ข้ามเมื่อ hostname === ip หรือว่าง), `MAC: {detail.mac}` (ข้ามเมื่อว่าง, ค่าเป็น font-mono), `Domain: {detail.domain}` (ข้ามเมื่อว่าง) — ถ้าไม่เหลือรายการใดเลยแสดงข้อความ muted 'ไม่มีข้อมูลอุปกรณ์ (ไม่พบ DHCP lease/reservation ของ IP นี้)'.\n(2) กลุ่มขวา = ชุดควบคุมทั้งหมดของหน้านี้ (D-3): <div className=\"flex w-full flex-wrap items-center justify-end gap-2 sm:w-auto\"> เรียงตามลำดับ **<AccuracyInfoButton accuracy={detail?.accuracy} /> → <StatsWindowTabs value={window} onChange={onWindowChange} /> → ปุ่ม Refresh** (ยกปุ่ม Refresh มาจาก StatisticsTrafficHost.tsx:393-404 ทั้งก้อน รวม disabled/animate-spin/aria-label/`hidden sm:inline`) — ลำดับนี้ต้องตรงกับ control bar มาตรฐานของหน้า Statistics อื่น (docs/ref/todo/statistics-window-granularity-plan.md). StatsWindowTabs import จาก @/components/statistics/DnsStatsShared และตัวมันมี wrapper overflow-x-auto อยู่แล้ว — ห้ามแก้ไฟล์ DnsStatsShared.tsx และห้ามใส่ overflow ซ้อนจนเกิด scrollbar ซ้อน; ให้ wrapper ฝั่งการ์ดใช้ `min-w-0` เพื่อให้มันย่อได้บนจอเล็ก.\n(3) ตอน `isLoading && !detail` ให้ IP ยังแสดงจาก prop ip ตามปกติ และแถว metadata แสดง <Skeleton className=\"h-3 w-48\" /> แทน.\nข้อบังคับ: ใช้เฉพาะคอมโพเนนต์จาก @/components/ui (Card/CardContent/Badge/Button/Skeleton) + AccuracyInfoButton + StatsWindowTabs, ห้ามสี Tailwind ดิบ, ห้าม shadow-*/backdrop-blur-*, ต้องอ่านออกทั้ง dark/light, ข้อความยาวต้อง truncate/min-w-0 ไม่ล้นการ์ด",
    "acceptance": [
      "มีไฟล์ HostHeaderCard.tsx ที่ไม่มี fetch/useNavigate/useSearchParams อยู่ภายใน",
      "ชุดควบคุมในการ์ดเรียง i → window selector → Refresh ครบ 3 ตัว",
      "badge Active แสดงเฉพาะเงื่อนไข private && active === true",
      "แถว metadata ข้ามฟิลด์ว่างโดยไม่ทิ้ง ' · ' ค้างหัว/ท้าย",
      "ไม่มีสีดิบ/shadow/backdrop-blur; `yarn build` + `yarn lint` ผ่าน"
    ],
    "depends_on": ["T-02", "T-04"]
  },
  {
    "task_id": "T-06",
    "title": "นำ HostHeaderCard ไปใช้ในหน้า drill-down แทนหัวเดิม + ย้าย window selector เข้าการ์ด",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsTrafficHost.tsx"],
    "instruction": "แทนที่บล็อกหัวเดิม (บรรทัด ~358-406) ด้วย: (1) แถวบนเหลือเพียงปุ่ม 'กลับไปหน้า Traffic' ของเดิม (ไม่แก้ลิงก์/ไอคอน/ข้อความ) ครอบด้วย div ธรรมดา (2) ถัดมา <HostHeaderCard ip={ip} detail={data} isLoading={isLoading} window={window_} onWindowChange={setWindow} onRefresh={() => load(ip, window_, true, () => false, false)} />. ลบออกจากหน้าให้หมด: <h1> + <span> IP ตัวเล็ก + badge domain + badge LAN/Internet + <AccuracyInfoButton> + <StatsWindowTabs> + ปุ่ม Refresh (ทั้งหมดย้ายไปอยู่ในการ์ดแล้ว) พร้อม import ที่ไม่ได้ใช้แล้ว (Badge, RefreshCw ฯลฯ — ต้องไม่เหลือ unused import ให้ lint ฟ้อง). **ห้ามลบ/แก้** `const [window_, setWindow] = useStatsWindow()` (บรรทัด 271) และ **ห้ามลบตัวแปร title** ที่ยังถูกใช้ในย่อหน้าหมายเหตุใต้ stat cards (~458). ห้ามแก้ logic โหลดข้อมูล/auto-refresh/role/window/สถานะ found=false/skeleton/TruncatedWarning. ตรวจว่าเมื่อ data === null (กำลังโหลด หรือ error) การ์ดยังเรนเดอร์ได้โดยไม่ throw และ window selector ยังกดเปลี่ยนช่วงเวลาได้ตามปกติ",
    "acceptance": [
      "หน้าแสดงการ์ด host header ตาม layout ใหม่ ไม่มีหัวเดิมหลงเหลือ และไม่มี unused import",
      "ปุ่ม Refresh / ปุ่มข้อมูล accuracy / ตัวเลือกช่วงเวลา ทำงานเหมือนเดิมทุกประการ (ตัวเลือกช่วงเวลายังเขียน ?window= ลง URL แบบ replace)",
      "ไม่มีการเปลี่ยนแปลง logic การ fetch/auto-refresh/URL params",
      "`yarn build` + `yarn lint` ผ่าน"
    ],
    "depends_on": ["T-05", "T-03"]
  }
]
```

## 4. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

> ทดสอบครั้งเดียวหลัง T-01…T-06 เสร็จครบ (ไม่ต้องทดสอบทีละ task)

```json
{
  "final_acceptance": [
    "`cd frontend && yarn build && yarn lint` ผ่านทั้งคู่ ไม่มี warning ใหม่",
    "`bash build.sh` ผ่าน (single binary build ไม่พัง)",
    "`cd backend && go build ./... && go test ./...` ยังผ่าน (แผนนี้ไม่ควรแตะ backend เลย — git diff ต้องไม่มีไฟล์ใน backend/)",
    "เปิด /statistics/traffic/host/<ip> โหมด mock: การ์ด header แสดงไอคอนวงกลม + IP ตัวใหญ่ + badge สถานะ + แถว metadata (Hostname/MAC/Domain เฉพาะที่มีค่า) ครบตาม layout §2",
    "ชุดควบคุมในการ์ดมีครบ 3 ตัวเรียง [i] → [15m 30m 1H 3H 6H 12H 24H] → [Refresh] ชิดขวาบนจอใหญ่",
    "กดปุ่มช่วงเวลาจากในการ์ด: URL ?window= เปลี่ยน, ข้อมูล/กราฟรีโหลดตามช่วงใหม่, กด Back ของเบราว์เซอร์แล้วออกจากหน้าไปเลย (ไม่ไล่ทีละ window — พฤติกรรม replace เดิม)",
    "เครื่อง LAN (private=true) โหมด mock เห็น badge Active; ปลายทางอินเทอร์เน็ต (private=false) ไม่เห็น badge Active และไอคอนเปลี่ยนเป็น Globe",
    "โหมดจริง (backend -mock=false): ไม่มี badge Active โผล่ และหน้าไม่ error",
    "IP ที่ไม่มี DHCP lease (hostname === ip, mac ว่าง) แสดงข้อความ fallback ไม่มี ' · ' ค้างหัว/ท้ายแถว",
    "ปุ่มไอคอน i: คลิกแล้ว Popover เปิด แสดงสถานะปัจจุบัน (ใกล้เคียงจริง/ประมาณการ) + คำอธิบาย; คลิกนอก/กด Esc ปิดได้; ใช้คีย์บอร์ด (Tab + Enter) ได้; ปุ่มสูงเท่าปุ่ม Refresh",
    "หน้า Statistics Overview และ Statistics Traffic เปลี่ยนเป็นปุ่มไอคอน i แล้วเช่นกัน และลำดับยังเป็น ปุ่มข้อมูล → ช่วงเวลา → Refresh; หน้า Dashboard ยังเป็น badge เดิม (ไม่ถูกแตะ)",
    "Responsive ที่ ~375px และ ~768px: ชุดควบคุมตกลงแถวล่างสุดของการ์ดและชิดขวา, segmented control 7 ปุ่มเลื่อนแนวนอนได้ในตัวมันเอง, ไม่มี horizontal scroll ของทั้งหน้า, IP/hostname ยาว ๆ ถูก truncate ไม่ดันการ์ดล้น",
    "Dark mode และ Light mode อ่านออกทั้งคู่ (IP, badge, metadata, Popover, segmented control)",
    "grep ยืนยัน: ไม่มี shadow-*, backdrop-blur- และไม่มีคลาสสีดิบ (emerald/red/green/blue-…) ในไฟล์ที่แผนนี้แตะ",
    "ฟังก์ชันเดิมไม่ regress: ปุ่มกลับไปหน้า Traffic, แท็บ Source/Destination (?role=), ตารางเรียง/ค้นหา, กราฟ, drill-down ไป peer, auto-refresh 10 วินาที, สถานะ found=false, TruncatedWarning ทำงานเหมือนเดิมทุกข้อ"
  ]
}
```

## 5. ข้อควรระวังสำหรับ ai-developer

1. **ห้ามคิดฟิลด์ขึ้นเอง** — `group`, `vlan`, `zone`, `online`, `lastSeen`, `iface` ไม่มีอยู่จริงใน payload ห้ามใส่ (D-1 ตัดทิ้งแล้ว)
2. `detail` เป็น `null` ได้เสมอในช่วงโหลด/เออเรอร์ — ทุกจุดใน HostHeaderCard ต้อง optional-chain
3. ฟิลด์ `mac` เป็น `""` (ไม่ใช่ undefined) เมื่อไม่รู้จัก — เช็คด้วยค่าว่าง ไม่ใช่ `!= null`
4. `hostname` fallback เป็น IP เอง (ดู `hostnameFor`) — อย่าแสดง "Hostname: 192.168.1.5" ซ้ำกับ IP ตัวใหญ่
5. window selector เป็นของ **ที่ย้ายมา ไม่ใช่ของใหม่** — state ยังอยู่ที่หน้า (`useStatsWindow()`) การ์ดรับผ่าน prop เท่านั้น ห้ามให้การ์ดไปอ่าน/เขียน URL เอง
6. ห้ามแก้ `components/statistics/DnsStatsShared.tsx` (ใช้ร่วมกับหน้า DNS อีก 3 หน้า)
7. Popover ในหน้านี้ไม่มี Combobox เกี่ยวข้อง จึงไม่ต้องแตะเรื่อง `modal={false}` แต่ห้ามซ้อน Popover ไว้ใน Tooltip
8. งานนี้ไม่แตะ auth/firewall/netlink/D-Bus และไม่แตะ backend เลย — ถ้าพบว่าต้องแตะ แปลว่าออกนอกขอบเขต ให้หยุดแล้วรายงานกลับ
9. ทำงานบน branch `feat/statistics-traffic-host-header` เท่านั้น ห้าม commit เว้นแต่เจ้าของสั่ง
