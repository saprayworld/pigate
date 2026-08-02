# Statistics → Traffic: เพิ่มกราฟ Bandwidth — reuse คอมโพเนนต์จากหน้า Overview

> เอกสารแผนงานสำหรับฟีเจอร์: เพิ่มกราฟ Bandwidth เข้าไปในหน้า Statistics → Traffic ทั้งหน้า list
> (`/statistics/traffic` — ใช้ `BandwidthTrendCard` + `TopHostsShareCard` แบบ **ทั้งเครือข่าย**
> เหมือนหน้า Overview) และหน้า drill-down ต่อ IP (`/statistics/traffic/host/:ip` — ใช้
> `BandwidthTrendCard` แบบ **per-IP จริง**) โดยใช้คอมโพเนนต์เดิม ไม่สร้างกราฟใหม่
>
> วันที่เขียน: 2026-08-02 · แก้ไขครั้งที่ 2: 2026-08-02 (owner เลือก D-1 ตัวเลือก B = per-IP)
> Branch อ้างอิง: `feat/statistics-traffic-page` (ต่อยอดจาก PR 121 — ไม่สร้าง branch ใหม่)
>
> **สถานะ: โค้ดครบทุก Task (T-01…T-08) แล้ว ณ 2026-08-02** — ai-developer ทำครบ, ai-qa ตรวจ PASS
> ไม่มี loop แก้, build/vet/test/lint ผ่านทั้งหมด, openapi สองไฟล์ diff เท่ากันเป๊ะ
> **เหลือเฉพาะ manual browser check ของเจ้าของโปรเจกต์ก่อนเปิด PR** (ดู §7 ข้อที่ยังไม่ติ๊ก)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**

1. หน้า `/statistics/traffic` — เพิ่มแถวบนสุด: `BandwidthTrendCard` (2/3) + `TopHostsShareCard` (1/3)
   หน้าตาและ **ความหมายของข้อมูลเหมือนหน้า Overview ทุกประการ** คือ bandwidth ของทั้งเครือข่าย
2. หน้า `/statistics/traffic/host/:ip` — เพิ่ม **เฉพาะ** `BandwidthTrendCard` ไปอยู่แถวเดียวกับ
   panel "Top peers" ที่มีอยู่แล้ว สัดส่วน Bandwidth = 2/3, Top peers = 1/3, responsive (จอเล็ก stack)
   และกราฟต้องเป็น **bandwidth ของ IP ที่กำลังดูเท่านั้น (per-IP)** ไม่ใช่ของทั้งเครือข่าย
3. ข้อมูลกราฟทั้งสองหน้าต้องมาจาก **request เดิมของแต่ละหน้า** (ไม่ยิง API เพิ่มรอบที่สอง)
4. ตัวเลขบนกราฟ per-IP ต้อง **ไม่ขัดกับการ์ดสรุปด้านบนของหน้าเดียวกัน**:
   `sum(series[].bytes) == totalBytes`, `sum(series[].bytesUp) == totalBytesUp`,
   `sum(series[].bytesDown) == totalBytesDown` แบบเป๊ะ (by construction ไม่ใช่ by luck)

**นอกขอบเขต (ระบุชัด)**

- ไม่แตะหน้า `/statistics` (Overview), ไม่แตะ endpoint `/api/statistics/traffic`, ไม่แตะ Dashboard
- ไม่แตะ kernel layer, ไม่มี route ใหม่, ไม่มี migration, ไม่แตะ auth/firewall/D-Bus/Netlink
- ไม่เพิ่ม `TopHostsShareCard` ในหน้า drill-down (owner ระบุชัดว่าเอาเฉพาะ Bandwidth)
- ไม่ทำ per-IP series ในหน้า list (`/statistics/traffic`) — หน้านั้นคงเป็นทั้งเครือข่ายตามโจทย์เดิม
- ไม่เปลี่ยนความหมาย/ค่าของ `TrafficStatistics.series` และ `TotalBytes*` ที่มีอยู่เดิม

## 1. สถานะปัจจุบัน (สำรวจ ณ วันเขียนแผน 2026-08-02 — ก่อนลงมือ)

| ส่วน | สถานะ | ไฟล์:บรรทัด (~) |
|---|---|---|
| `BandwidthTrendCard` | **เสร็จแล้ว + reusable** — รับ props `{series, window, className}` ไม่ fetch เอง แต่ subtitle ตอนนั้น hardcode ว่า "Up/Down นับตามทิศทางเข้า-ออกเครือข่าย LAN" (ผิดสำหรับกราฟ per-IP) | `frontend/src/components/statistics/BandwidthTrendCard.tsx:31, 76-79` |
| `TopHostsShareCard` | **เสร็จแล้ว + reusable** — รับ `{hosts, observedBytes, className}` ผู้เรียกต้อง slice top 5 มาเอง | `frontend/src/components/statistics/TopHostsShareCard.tsx:22` |
| หน้า Overview ใช้งาน 2 การ์ดนี้ | เสร็จแล้ว — `grid lg:grid-cols-3`, chart `lg:col-span-2` | `frontend/src/pages/StatisticsOverview.tsx:463-472` |
| series ระดับเครือข่าย | มีแล้ว — `TrafficBreakdown.Series` คำนวณในลูป/RLock เดียวกับ `Observed` | `backend/internal/service/traffic_stats.go:1018, 1039-1054, 1096-1107` |
| **series ระดับ IP** | **ยังไม่มีเลย** — ไม่มีทั้งใน service, model, API | — |
| ข้อมูลดิบต่อบัคเก็ตที่ใช้ทำ per-IP ได้ | มีครบ — `trafficDetailBucket.convBytes` (key = `src\|dst\|proto\|port`, value = `dirBytes{Orig,Reply}`) และ `hostBytes`/`dstBytes` | `traffic_stats.go:110-139, 863-865` |
| `TotalBytes/Up/Down` ของ drill-down | คำนวณจาก `breakdown.Convs` (map รวมทั้ง window) ก่อนตัด limit | `backend/internal/service/statistics_traffic.go:110-153, 224-226` |
| หน้า Traffic list | เสร็จแล้ว — ยิง `GET /api/statistics/traffic/hosts` ทุก 10 วิ, grid `xl:grid-cols-2` 2 ตาราง | `frontend/src/pages/StatisticsTraffic.tsx:203-235` |
| หน้า Traffic drill-down | เสร็จแล้ว — panel "Top peers" อยู่ **ข้างใน** `ConversationTable` เหนือช่องค้นหา และคำนวณจากแถวที่ผ่าน text filter | `frontend/src/pages/StatisticsTrafficHost.tsx:76-124` |
| `series` ใน 2 endpoint ของหน้า Traffic | **ยังไม่มี** ทั้งคู่ | `backend/internal/model/statistics.go:247, 297` |
| Mock ฝั่ง frontend ของหน้า Traffic | มี แต่ยังไม่มี series (`mockBandwidthSeries` เป็น private อยู่ใน `statisticsService.ts`) | `frontend/src/services/trafficStatisticsService.ts:229-351`, `statisticsService.ts:198` |

**สรุปหนึ่งบรรทัด:** หน้า list ได้ของฟรี (แค่ส่ง `breakdown.Series` ที่คำนวณทิ้งอยู่แล้วออกไป)
ส่วนหน้า drill-down เป็นงานจริง: ต้องสร้าง per-IP series ใหม่จาก `convBytes` รายบัคเก็ต

## 2. แนวทางเทคนิค

### 2.1 หน้า list (network-wide) — เกือบฟรี

`GetTrafficTopHosts` เรียก `s.traffic.GetTrafficBreakdown(window)` อยู่แล้ว (`statistics_traffic.go:60`)
และ `breakdown.Series` ถูกคำนวณไว้แล้วแต่ถูกทิ้ง → แค่ `Series: breakdown.Series` ต้นทุน = 0
(ทางเลือกที่ตัดทิ้ง: ให้ frontend ยิง `/api/statistics/traffic` เพิ่มอีกเส้นทุก 10 วิ — เส้นนั้นคำนวณ
conversations/denied/DNS ที่หน้านี้ไม่ใช้ และเป็นคนละ snapshot กับตัวเลขที่หน้าแสดงอยู่)

### 2.2 หน้า drill-down (per-IP) — 2 การตัดสินใจที่ล็อกไว้แล้ว ห้ามเปิดใหม่

**ตัดสินใจ 1 — แหล่งข้อมูล: ใช้ `b.convBytes` รายบัคเก็ต (ไม่ใช่ `hostBytes`/`dstBytes`)**

เหตุผล: `TotalBytes/TotalBytesUp/TotalBytesDown` ของ response นี้คำนวณจาก `breakdown.Convs`
(`statistics_traffic.go:110-153`) ถ้า series มาจาก `hostBytes`/`dstBytes` ตัวเลขสองชุดจะไม่ตรงกัน
เมื่อชน per-bucket tracking cap (`mergeDirMap` ตัด key ใหม่ทิ้งคนละจังหวะกันในแต่ละ map,
`traffic_stats.go:899`) → ผู้ใช้เห็น "Total = 3.2 GB" แต่ผลรวมกราฟได้ 2.9 GB โดยไม่มีคำอธิบาย
การอ่านจาก `convBytes` ซึ่งเป็น map เดียวกับที่ `TotalBytes` ใช้ ทำให้ invariant เป็นจริง
**by construction** (map เดียวกัน, ชุดบัคเก็ตเดียวกัน, กฎ carry index เดียวกัน)
ต้นทุนที่แลกมา: ต้องวนอ่าน `convBytes` เพิ่ม 1 รอบต่อบัคเก็ต (ดู §5 ข้อ 8 เรื่อง performance)

**ตัดสินใจ 2 — ทิศทาง up/down: ใช้ flow-relative (Orig = up, Reply = down) เหมือนทั้งหน้า
ห้ามสร้าง convention ที่สามของโปรเจกต์ (IP-relative)**

โปรเจกต์นี้มี 2 convention อยู่แล้ว: flow-relative (`TopHost`/`TopConversation`/`TrafficHostDetail.TotalBytesUp/Down`)
และ LAN-relative (`TrafficStatistics.series`) ถ้ากราฟ per-IP ใช้ IP-relative (up = ที่ IP นี้ส่งออก)
จะขัดกับ **ทุกตัวเลขที่อยู่บนหน้าเดียวกัน**: การ์ดสรุป Up/Down ด้านบน (`StatisticsTrafficHost.tsx:380-391`)
และคอลัมน์ Up/Down ในตารางทั้งสองแท็บ ซึ่งเป็น flow-relative ทั้งหมด
→ เลือก flow-relative แล้วได้ทั้งความสอดคล้องและ invariant ครบ 3 ตัว (bytes/up/down)
และความหมายก็ยังอ่านรู้เรื่องทั้งสองบทบาท ตามเหตุผลที่ `buildTopHosts` เขียนไว้แล้ว
("down คือ traffic ฝั่ง reply ที่โหลดหนัก" — `statistics.go:245-248`): ดู LAN client → Download = ที่เครื่องโหลดมา,
ดู IP ปลายทางบนอินเทอร์เน็ต → Download = ที่ LAN โหลดมาจากปลายทางนั้น

> **ผลที่ตามมาซึ่งต้องเขียนกำกับใน UI:** กราฟบนหน้า drill-down เป็น flow-relative
> แต่กราฟบนหน้า list/Overview เป็น LAN-relative → ทั้งสองที่ต้องมี subtitle บอกนิยามของตัวเอง (T-06/T-08)

### 2.3 กลไก: ขยาย `GetTrafficBreakdown` ด้วย focus IP (ไม่ใช่ method ใหม่ที่ล็อกอีกรอบ)

```go
// traffic_stats.go
type TrafficBreakdown struct {
    …เดิม…
    // HostSeries เป็น nil เสมอเมื่อเรียกผ่าน GetTrafficBreakdown (ไม่มี focus IP)
    HostSeries []model.BandwidthPoint
}

func (s *TrafficStatsService) GetTrafficBreakdown(window string) TrafficBreakdown {
    return s.getTrafficBreakdown(window, "")
}
func (s *TrafficStatsService) GetTrafficBreakdownForIP(window, ip string) TrafficBreakdown {
    return s.getTrafficBreakdown(window, ip)
}
```

เหตุผลที่ **ไม่** ทำเป็น method แยกที่วนบัคเก็ตเอง: จะกลายเป็นการ `s.mu.RLock()` รอบที่สอง
คนละ snapshot กับ `TotalBytes` → invariant พังทันทีที่มี traffic เข้ามาระหว่างสองการ lock
(เป็นความผิดพลาดแบบเดียวกับที่แผน bandwidth-chart เดิมเคยแก้ไว้ — `traffic_stats.go:1039-1044`)

## 3. ขั้นตอนการทำ (Task ย่อย เรียงตาม dependency) — ทำครบแล้วทั้งหมด

### T-01 — เพิ่มฟิลด์ `Series` ลง DTO ทั้งสองตัว (ความหมายต่างกัน ต้องเขียนคอมเมนต์ให้ชัด) ✅
- **layer:** model · **depends_on:** —
- **ไฟล์:** `backend/internal/model/statistics.go`
- **instruction:** เพิ่ม `Series []BandwidthPoint` (json `series`) ในทั้งสอง struct วางก่อน `GeneratedAt`
  พร้อมคอมเมนต์แยกความหมาย: `TrafficTopHosts.Series` = ทั้งเครือข่าย/LAN-relative/`sum==ObservedBytes`
  ส่วน `TrafficHostDetail.Series` = ของ IP นี้เท่านั้น/flow-relative/invariant 3 ค่า/ยาวคงที่ 12-288
  แม้ `Found == false` (array ศูนย์ ไม่ใช่ nil)
- **ผลจริง:** เสร็จแล้ว — `model/statistics.go:269` (TrafficTopHosts) และ `:359` (TrafficHostDetail)

### T-02 — per-IP series ใน service (งานหลักของแผนนี้) ✅
- **layer:** service · **depends_on:** T-01
- **ไฟล์:** `backend/internal/service/traffic_stats.go`, `backend/internal/service/statistics_traffic.go`
- **instruction (สรุป):** `HostSeries` ใน `TrafficBreakdown` + `getTrafficBreakdown(window, focusIP)`
  + wrapper `GetTrafficBreakdown`/`GetTrafficBreakdownForIP` + ลูป `convBytes` ที่ `idx`/RLock เดิม
  (ไม่ใช่ else-if สำหรับแถว loopback) + helper กลาง `parseConvKey` ที่ `GetTrafficHostDetail`
  ใช้ร่วมกัน + `GetTrafficHostDetail` ต้องใช้ `breakdown.HostSeries` ห้ามใช้ `breakdown.Series`
- **ผลจริง:** เสร็จแล้ว — `traffic_stats.go:1028` (`HostSeries`), `:1044/:1055` (wrapper),
  `:1064` (`getTrafficBreakdown`), `:1176-1199` (ลูป per-IP + fast path prefix/needle ก่อน parse),
  `statistics_traffic.go:59` (`parseConvKey`), `:125` (`GetTrafficBreakdownForIP`),
  `:259` (`Series: breakdown.HostSeries`)

### T-03 — เทสต์ backend ✅
- **layer:** service (test) · **depends_on:** T-02 · **ไฟล์:** `backend/internal/service/statistics_traffic_test.go`
- **instruction:** 6 เคส — SeriesMatchesTotals / SeriesLengthFixed / SeriesIsPerIPNotNetworkWide /
  SeriesZeroFilledWhenNotFound / TopHosts_SeriesIsNetworkWide / Breakdown_HostSeriesNilWithoutFocusIP
- **ผลจริง:** เสร็จแล้วครบ 6 เคส (`:403` ดักกรณี `breakdown.Series` รั่วเข้ามา, `:478` regression
  ของ wrapper) · `go test ./...` ผ่านทั้ง repo

### T-04 — อัปเดต OpenAPI ทั้งสองไฟล์ ✅
- **layer:** docs · **ไฟล์:** `docs/openapi.yaml`, `frontend/public/openapi.yaml`
- **instruction:** เพิ่ม `series` (+ ใส่ใน `required`) ทั้ง `TrafficTopHosts` (window-wide, LAN-relative)
  และ `TrafficHostDetail` (per-IP, flow-relative, invariant 3 ค่า, ยาวคงที่แม้ `found=false`)
- **ผลจริง:** เสร็จแล้ว · `diff` สองไฟล์เท่ากันเป๊ะ

### T-05 — Type + mock ฝั่ง frontend service ✅
- **layer:** frontend (service) · **ไฟล์:** `frontend/src/services/{statisticsService,trafficStatisticsService}.ts`
- **instruction:** export `mockBandwidthSeries(window, exactTotal, upRatio = 0.15)` (ย้าย ×1.12 ไปที่
  callsite ของ Overview), re-export `BandwidthPoint`, `series` ใน 2 interface, mock list ใช้
  `observedBytes`, mock drill-down ใช้ `totalBytes` + สัดส่วน `totalUp/totalBytes`
- **ผลจริง:** เสร็จแล้ว — `statisticsService.ts:208` (signature ใหม่), `:311` (callsite ×1.12),
  `trafficStatisticsService.ts:256` (list) และ `:361` (per-IP)

### T-06 — `BandwidthTrendCard` ใช้ในบริบท per-IP และพื้นที่แคบได้ ✅
- **layer:** frontend (component) · **ไฟล์:** `frontend/src/components/statistics/BandwidthTrendCard.tsx`
- **instruction:** prop `subtitle?: string` ที่ **แทนที่** บรรทัด subtitle เดิมทั้งบรรทัด (ไม่ใช่ต่อท้าย
  เพราะข้อความเดิมระบุ LAN-relative ซึ่งผิดสำหรับ per-IP) + `flex-wrap gap-x-4 gap-y-1` ที่ `CardHeader`
- **ผลจริง:** เสร็จแล้ว — `:35/:46` (prop), `:85` (`subtitle ?? "…LAN"` — ค่า default เดิมไม่เปลี่ยน)

### T-07 — หน้า Traffic list: แถว Bandwidth + Top 5 Hosts (network-wide) ✅
- **layer:** frontend (page) · **ไฟล์:** `frontend/src/pages/StatisticsTraffic.tsx`
- **instruction:** `grid lg:grid-cols-3` + chart `lg:col-span-2` + `data.sources.slice(0,5)`
  (ห้าม sort/normalize ใหม่), ไม่ส่ง `subtitle`, render นอกเงื่อนไข empty, อัปเดต skeleton
- **ผลจริง:** เสร็จแล้ว — `:249-250`

### T-08 — หน้า drill-down: Bandwidth per-IP (2/3) + Top peers (1/3) ✅
- **layer:** frontend (page) · **ไฟล์:** `frontend/src/pages/StatisticsTrafficHost.tsx`
- **instruction:** ส่ง `series` เข้า `ConversationTable`, `grid grid-cols-1 gap-4 xl:grid-cols-3`,
  chart `xl:col-span-2` (หรือ `xl:col-span-3` เมื่อไม่มี peers), `subtitle` บอกว่าเป็นเฉพาะ IP นี้ +
  นิยาม Up/Down แบบ flow-relative, ไม่ย้าย `topPeers` ออกจาก `ConversationTable`
- **ผลจริง:** เสร็จแล้ว — `:122-124`

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | เปลี่ยนอะไร |
|---|---|---|---|
| GET | `/api/statistics/traffic/hosts` | เส้นเดิม (`authRoute`) | **เพิ่ม `series`** = ทั้งเครือข่าย, LAN-relative (additive) |
| GET | `/api/statistics/traffic/host` | เส้นเดิม (`authRoute`) | **เพิ่ม `series`** = เฉพาะ IP ที่ query, flow-relative (additive) |
| GET | `/api/statistics/traffic` | เส้นเดิม | **ไม่แตะ** — response เหมือนเดิม byte-for-byte |

- ไม่มี route ใหม่, `api/handlers.go` ไม่ต้องแก้ (handler แค่ encode struct ที่ service คืนมา)
- ทั้งสองเส้นเป็น GET → `-disable-edit=true` / `RoleReadOnlyMiddleware` ไม่กระทบ
- `ip` ที่รับเข้ามาถูก `netip.ParseAddr` + normalize ที่ handler อยู่แล้วก่อนถึง service
  (`statistics_traffic.go:84-89`) — งานนี้ไม่ได้เพิ่มเส้นทางที่ทำให้ค่า `ip` ดิบถูกใช้เป็น key โดยไม่ normalize

## 5. ข้อควรระวัง (ทั้งหมดนี้เจอจากการอ่านโค้ดจริง)

1. **`series` ไม่เคยอยู่ใน endpoint ของหน้า Traffic มาก่อน** — `BandwidthTrendCard` ป้องกันด้วย
   `series ?? []` → **ห้ามลบ** เพราะเป็นตัวกัน frontend build เก่า/ใหม่คุยข้ามเวอร์ชันไม่ให้ crash
2. **สอง `series` คนละความหมายในโปรเจกต์เดียวกัน** — `TrafficTopHosts.series` (ทั้งเครือข่าย,
   LAN-relative) กับ `TrafficHostDetail.series` (per-IP, flow-relative) ถ้าก็อปโค้ดข้ามหน้าโดยไม่อ่าน
   จะได้กราฟที่ตัวเลขถูกแต่ป้ายผิด → กันด้วยคอมเมนต์ใน model, description ใน openapi,
   `subtitle` ที่ต้อง override และเทสต์ข้อ 3/5 ของ T-03
3. **ห้ามเอา per-IP series ไปคำนวณจาก `hostBytes`/`dstBytes`** — จะไม่ตรงกับ `TotalBytes` เมื่อชน
   tracking cap (§2.2 ตัดสินใจ 1) อาการคือ "Total ในการ์ด ≠ ผลรวมกราฟ" ซึ่งหาสาเหตุยากมาก
4. **ห้ามเรียก breakdown สองรอบ** — `TotalBytes` กับ series ต้องมาจาก snapshot/RLock เดียว
   ไม่งั้น traffic ที่เข้ามาระหว่างสอง lock จะทำให้ invariant พังแบบสุ่ม (เจอยากตอนเทสต์ เจอตอน production)
5. **`topPeers` คำนวณจากแถวที่ผ่าน text filter** — ถ้าย้าย panel ออกไปไว้ระดับ page เพื่อจัด layout
   จะเสียพฤติกรรม "Top peers ขยับตามคำค้น" (regression เงียบ) → จึงวางกราฟ **ข้างใน `ConversationTable`**
6. **Breakpoint**: หน้า drill-down อยู่ใน `<Card>` + `TabsContent` และ sidebar กว้าง 16rem
   ทำให้ที่ 1024px ช่องกราฟเหลือ ~450px → ใช้ `xl:grid-cols-3` (ไม่ใช่ `lg` แบบหน้า Overview)
   + ทำ header ให้ wrap ได้ (T-06)
7. **Tabs ของ Radix unmount tab ที่ไม่ active** → กราฟ remount ตอนสลับ Source/Destination
   ยอมรับได้ และทั้งสองแท็บใช้ series ชุดเดียวกัน (per-IP series รวมทั้งสองทิศทางเท่ากับ `TotalBytes`)
8. **Performance ของลูปใหม่**: วน `convBytes` เพิ่ม 1 รอบต่อบัคเก็ตใน RLock เดิม → ต้องกรองด้วย
   prefix/needle ก่อนแล้วค่อย `parseConvKey` ที่ alloc (implementation จริงทำตามนี้แล้ว — `traffic_stats.go:1178`)
9. **`found == true` แต่กราฟว่างได้** — IP ที่โผล่เฉพาะใน `Hosts`/`Dests` แต่ไม่มีแถว conversation
   จะได้ series ศูนย์ทั้งเส้น → การ์ดโชว์ "กำลังเก็บข้อมูล traffic…" ซึ่งถูกต้องแล้ว
10. **ขนาด payload**: window 24h เพิ่ม ~288 จุด (~20-25KB) ต่อ request เท่ากับที่หน้า Overview ทำอยู่แล้ว
11. **Mock ฝั่ง frontend ต้องคง invariant** — list `sum(series)===observedBytes`,
    drill-down `sum(series)===totalBytes` (up/down คลาดจากการปัดเศษได้เฉพาะใน mock)
12. **Regression ที่ต้องกัน**: หน้า Overview, `/api/statistics/traffic`, `GetTrafficDetail` (Dashboard)
    และ `TotalBytes*` เดิมต้องไม่เปลี่ยน — จุดเสี่ยงคือ rename ฟังก์ชัน + refactor `parseConvKey`
    → เทสต์ข้อ 6 ของ T-03 คือด่านกัน (ผ่านแล้ว)
13. **สไตล์ (`docs/rules_of_work.md`)**: ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามสี palette ดิบ,
    รองรับ dark/light · งานนี้ไม่มี Dialog/Combobox จึงไม่เกี่ยวกับกฎ `modal={false}`
14. **Git**: ทำต่อบน `feat/statistics-traffic-page` เดิม เข้า PR ไป `main` ห้าม push โค้ดขึ้น main ตรง ๆ
    และห้าม commit จนกว่าเจ้าของโปรเจกต์จะสั่ง

## 6. Checklist สรุป (Definition of Done)

- [x] T-01 `backend/internal/model/statistics.go` — `Series` ใน `TrafficTopHosts` (network-wide) + `TrafficHostDetail` (per-IP) พร้อมคอมเมนต์แยกความหมาย
- [x] T-02a `backend/internal/service/traffic_stats.go` — `HostSeries` + `getTrafficBreakdown(window, focusIP)` + wrapper 2 ตัว + ลูป `convBytes` (fast path ก่อน parse)
- [x] T-02b `backend/internal/service/statistics_traffic.go` — helper `parseConvKey` ใช้ร่วมกัน 2 ที่, `GetTrafficTopHosts` ใช้ `breakdown.Series`, `GetTrafficHostDetail` ใช้ `GetTrafficBreakdownForIP` + `breakdown.HostSeries`
- [x] T-03 `backend/internal/service/statistics_traffic_test.go` — เทสต์ 6 ตัวตามรายการ
- [x] T-04 `docs/openapi.yaml` + `frontend/public/openapi.yaml` — เพิ่ม `series` ใน 2 schema (diff เท่ากันเป๊ะ)
- [x] T-05 `frontend/src/services/{statisticsService,trafficStatisticsService}.ts` — type + mock series (network-wide / per-IP)
- [x] T-06 `frontend/src/components/statistics/BandwidthTrendCard.tsx` — prop `subtitle` (override) + header wrap
- [x] T-07 `frontend/src/pages/StatisticsTraffic.tsx` — แถว Bandwidth (2/3) + Top 5 Hosts (1/3) + skeleton
- [x] T-08 `frontend/src/pages/StatisticsTrafficHost.tsx` — Bandwidth per-IP (2/3) คู่กับ Top peers (1/3)
- [x] `cd backend && go build ./... && go vet ./... && go test ./...` ผ่าน
- [x] `cd frontend && yarn build && yarn lint` ผ่าน
- [x] README Feature Status: ไม่ต้องแก้ (Statistics ไม่ได้เป็นรายการแยกในตาราง และไม่มี Mock→Real)
- [ ] **manual browser check โดยเจ้าของโปรเจกต์** (ยังค้าง — ดู §7 ข้อ 3, 6, 7, 10, 11, 13, 14)
- [ ] เปิด PR จาก `feat/statistics-traffic-page` → `main` (หลัง manual check ผ่าน)
- [ ] เมื่อ merge แล้ว: ย้ายไฟล์แผนนี้ไป `docs/ref/complete/` ตามธรรมเนียมโปรเจกต์

## 7. เกณฑ์ทดสอบรวมท้ายแผน (สถานะ ณ 2026-08-02)

- [x] 1. `go build ./... && go vet ./... && go test ./...` ผ่านทั้งหมด (เทสต์เดิมไม่พัง)
- [x] 2. `yarn build && yarn lint` ผ่าน ไม่มี error/warning ใหม่
- [ ] 3. `/statistics/traffic` (mock): เห็นการ์ด "Bandwidth · 1 ชม. ล่าสุด" 2/3 และ "Top 5 Hosts by Usage" 1/3 เหนือตาราง — *ตรวจจากโครง JSX แล้ว รอยืนยันด้วยตา*
- [x] 4. สลับ window 1h ↔ 24h: series ผูกกับ `window_` ทั้งสองหน้า (โครงโค้ด + เทสต์ length 12/288)
- [x] 5. `%` ของ Top 5 Hosts บนหน้า Traffic ใช้ตัวหาร `observedBytes` เดียวกับ Overview (`data.sources.slice(0,5)` ไม่ re-normalize)
- [ ] 6. `/statistics/traffic/host/:ip`: กราฟซ้าย 2/3 + Top peers ขวา 1/3 พร้อม subtitle per-IP — *รอยืนยันด้วยตา*
- [ ] 7. **ตรวจ per-IP จริงบนหน้าจอ**: กราฟของ IP ที่ traffic น้อย/มาก ต้องต่างกัน และต่างจากหน้า list — *ยืนยันเชิงตรรกะด้วยเทสต์ `SeriesIsPerIPNotNetworkWide` แล้ว รอยืนยันด้วยตา*
- [x] 8. invariant `sum(bytes)==totalBytes`, `sum(up)==totalBytesUp`, `sum(down)==totalBytesDown` — เทสต์ `SeriesMatchesTotals` ผ่าน
- [x] 9. `found=false` → `series` เป็น array ศูนย์ความยาวเต็ม ไม่ nil — เทสต์ `SeriesZeroFilledWhenNotFound` ผ่าน
- [ ] 10. responsive ต่ำกว่า `xl` ลงถึง ~375px: stack แนวตั้ง ไม่มี horizontal scroll, header wrap ไม่ทับกัน — *รอ manual*
- [ ] 11. หน้า drill-down: Top peers ยังขยับตามคำค้น + สลับแท็บแล้วกราฟยังถูก — *โครงโค้ดคงเดิม (`topPeers` ยังอยู่ใน `ConversationTable`) รอยืนยันด้วยตา*
- [x] 12. หน้า Overview ไม่เปลี่ยน — `subtitle` เป็น optional (default เดิม), callsite ×1.12 ย้ายมาให้ค่าเท่าเดิม, เทสต์ regression ผ่าน
- [ ] 13. dark mode + light mode อ่านออกทั้งสองโหมด — *รอ manual*
- [ ] 14. Network tab: แต่ละหน้ายิง API เส้นเดียวต่อรอบ poll — *ตรวจจากโค้ดแล้ว (หน้า Traffic ไม่ import `statisticsService`) รอยืนยันด้วย DevTools*

> **หมายเหตุจาก ai-qa:** ข้อที่ยังไม่ติ๊กคือข้อที่ต้องเปิดเบราว์เซอร์จริง ซึ่ง agent ทำไม่ได้
> (endpoint อยู่หลัง auth และไม่มี credential ให้ใช้ในสภาพแวดล้อมของ agent) — ไม่ใช่ข้อบกพร่องของโค้ด
> วิธีทดสอบ: `cd backend && go build -o pigate-backend ./cmd/pigate && ./pigate-backend -mock=true -allow-dev-cors`
> คู่กับ `cd frontend && yarn dev` แล้วเปิด `/statistics/traffic` และ `/statistics/traffic/host/192.168.1.102`

## 8. บันทึกการตัดสินใจ (D-1 — ปิดแล้ว)

**D-1: กราฟ Bandwidth บนหน้า drill-down ควรเป็นของทั้งเครือข่ายหรือ per-IP?**
→ **ตัดสินใจแล้วเมื่อ 2026-08-02 โดยเจ้าของโปรเจกต์: เลือกตัวเลือก B = per-IP จริง**
(ตัวเลือก A "ทั้งเครือข่าย + ป้ายกำกับ" ถูกตัดทิ้ง)

ผลที่ตามมาถูกดูดเข้ามาในแผนฉบับนี้และลงโค้ดแล้ว:

- แหล่งข้อมูล = `convBytes` รายบัคเก็ต (map เดียวกับที่ `TotalBytes` ใช้) — §2.2 ตัดสินใจ 1
- ทิศทาง up/down = flow-relative เหมือนทั้งหน้า ไม่สร้าง convention ที่สาม — §2.2 ตัดสินใจ 2
- กลไก = `getTrafficBreakdown(window, focusIP)` + `HostSeries` ใน snapshot/RLock เดียวกัน — §2.3
- หน้า list (`/statistics/traffic`) ไม่ได้รับผลกระทบ ยังเป็น network-wide ตามโจทย์เดิม — T-07
