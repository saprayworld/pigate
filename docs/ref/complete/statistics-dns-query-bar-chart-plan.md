# Statistics → DNS (overview): กราฟแท่งจำนวน DNS query ตามช่วงเวลา

> เอกสารแผนงานสำหรับคำขอของเจ้าของ repo บน branch `feat/statistics-dns-page` (ต่อยอดจาก PR 127)
>
> วันที่เขียน: 2026-08-06 · Branch อ้างอิง: `feat/statistics-dns-page` @ `fe3cb3e`
> ผู้เขียน: ai-tech-lead (สำรวจโค้ดจริงทั้ง backend/frontend ก่อนวางแผน)
>
> บริบทงานก่อนหน้า: `docs/ref/complete/statistics-dns-page-revamp-plan.md`, `docs/ref/todo/statistics-dns-ip-filter-plan.md`

## 0. เป้าหมายและขอบเขต

**คำขอต้นทาง (เจ้าของ repo):** _"เพิ่มกราฟแท่งเพื่อแสดงจำนวนครั้งในการ Query ตามช่วงเวลาในแต่ละ bucket เป็นแบบแท่ง ที่หน้าแรกของระบบสถิติ DNS"_

**เป้าหมาย (สิ่งที่ผู้ใช้จะเห็น)**

1. ที่หน้า `/statistics/dns` (หน้า overview เท่านั้น) มีการ์ดกราฟ **แท่ง** ใหม่ 1 ใบ เต็มความกว้าง วางใต้แถว stat card 4 ใบ เหนือตาราง 2 คอลัมน์
2. แกน X = เวลา (bucket ละ 5 นาที ตาม ring เดิม), แกน Y = **จำนวนครั้งของ DNS query** (ไม่ใช่ bytes)
3. จำนวนแท่งเปลี่ยนตาม time window ที่เลือกอยู่แล้ว (`StatsWindowTabs`): 3 / 6 / 12 / 36 / 72 / 144 / 288 แท่ง สำหรับ 15m/30m/1h/3h/6h/12h/24h
4. ผลรวมความสูงของทุกแท่ง = `totalQueries` ของ stat card ใบแรกในหน้าเดียวกัน **เป๊ะ** (invariant เดียวกับ `sum(series[].bytes) == observedBytes` ของฝั่ง traffic)
5. ยังไม่เปิดเก็บสถิติ DNS (`enabled=false`) → ไม่แสดงการ์ดนี้เลย (เหมือน stat card ปัจจุบัน); เปิดแล้วแต่ยังไม่มี query → แสดงการ์ดพร้อมข้อความ "กำลังเก็บข้อมูล…"
6. โหมด IP filter (`?ip=`) **ไม่กระทบ** กราฟนี้ — กราฟเป็นข้อมูลระดับ window ทั้งระบบเสมอ เหมือน stat card 4 ใบ

**นอกขอบเขต (จะไม่ทำ — กันแผนบวม)**

- ไม่เพิ่มกราฟนี้ในหน้า drill-down (`/statistics/dns/domain/:domain`, `/statistics/dns/client/:client`) — ทั้งสองหน้ามี `TrafficTrendCard` (bytes) อยู่แล้ว ถ้าเจ้าของอยากได้ query-count ในหน้าเหล่านั้นด้วย ค่อยทำเป็นแผนถัดไป (§6 ทางเลือก A)
- ไม่เพิ่ม endpoint ใหม่ — ขยาย `GET /api/statistics/dns` เดิม (เหตุผล §2.1)
- ไม่แตะ ring / hot path (`recordDomainQuery`) — ข้อมูลที่ต้องใช้มีครบอยู่แล้ว (§1)
- ไม่แยกนับ query ตาม domain / client / query type ในกราฟ (stacked bar) — แท่งเดียวต่อ bucket = ยอดรวม (เหตุผล §2.4)
- ไม่เพิ่ม dependency ใหม่ทั้ง Go และ npm (recharts มีอยู่แล้ว)
- ไม่ persist อะไรลง SQLite เพิ่ม (RAM-only ตามหลักการ SD card wear, CLAUDE.md / tech_stack_design.md §8)

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันเขียน)

| คำถาม | คำตอบจริงจากโค้ด | ไฟล์:บรรทัด (~) |
|---|---|---|
| backend มี time-bucketed query count อยู่แล้วไหม | **มีครบแล้ว** — `domainBucket.queries uint64` นับทุก event โดยไม่ถูก gate ด้วย cap ใด ๆ และ `domainBucket.ts` คือ RFC3339 ของ `time.Now().Truncate(5m)` | `backend/internal/service/dns_query_stats.go:65-87, 140, 169` |
| ring ยาวเท่าไร | 288 buckets × 5 นาที = 24h (`domainBucketSpan`/`domainBucketMax`) เท่ากับ ring ของ traffic/deny เป๊ะ | `dns_query_stats.go:19-27` |
| การเลือก bucket ตาม window | `dnsWindowBuckets(window)` = `lastNBuckets(s.dns.buckets, statsWindowBucketCount(window))` | `dns_query_stats.go:248-250` |
| bucket size logic ต้องออกแบบใหม่ไหม | **ไม่ต้อง** — `statsWindowBuckets` (15m:3 … 24h:288) เป็น single source of truth อยู่แล้ว และมีคำสั่งห้ามสร้าง map/switch ที่สองในโค้ดเบสไว้ชัดเจน | `backend/internal/service/traffic_stats.go:90-134` |
| endpoint overview | `GET /api/statistics/dns` → `HandleGetDNSQueryStatistics` → `StatisticsService.GetDNSQueryStatistics(window)` (มี loop วน bucket อยู่แล้วที่ `:71-93`) | `api/router.go:52` · `api/handlers.go:462-465` · `service/statistics_dns.go:40-204` |
| มี time-series pattern ให้ลอกไหม | มี — `getTrafficBreakdown` สร้างแกนเวลาแบบ fixed-length + zero-fill + carry rule ไว้ครบ (`axisStart`/`axisEnd`/`idx` clamp) | `service/traffic_stats.go:1314-1330, 1403-1414` |
| DTO ของจุดบนกราฟ | `model.BandwidthPoint{ts,bytes,bytesUp,bytesDown}` — **ใช้ซ้ำไม่ได้** เพราะฟิลด์เป็น bytes ล้วน (จะต้องยัด count ลง `bytes` = ตั้งใจทำให้อ่านผิด) | `model/statistics.go:109-124` |
| frontend มี chart library ไหม | `recharts` ใช้อยู่แล้วใน `Dashboard.tsx`, `StatisticsOverview.tsx`, `TrafficTrendCard.tsx` (มีแต่ `LineChart` — **ยังไม่มี `BarChart` ที่ไหนเลยในโปรเจกต์**) | `frontend/src/components/statistics/TrafficTrendCard.tsx:1-10` |
| หน้า overview มี window selector แล้วไหม | มี — `useStatsWindow()` + `<StatsWindowTabs>` sync กับ `?window=` อยู่แล้ว | `frontend/src/pages/StatisticsDns.tsx:48, 174` |
| layout ปัจจุบันของหน้า | header → stat cards 4 ใบ (`:190-204`) → truncated warning (`:206`) → grid 2 คอลัมน์ (Top Domains / Top Source Hosts, `:236-349`) → privacy note | `pages/StatisticsDns.tsx` |
| mock mode | `dnsStatisticsService.getDNSStatistics` มี branch `IS_MOCK_MODE` ที่ต้องสร้างข้อมูลเองครบทุกฟิลด์ และมี `mockBandwidthSeries(window, exactTotal, upRatio)` ที่การันตี sum เป๊ะให้ใช้ซ้ำได้ | `services/dnsStatisticsService.ts:459-546` · `services/statisticsService.ts:219-255` |
| openapi | 2 ไฟล์ต้องเหมือนกันทุก byte: `docs/openapi.yaml` และ `frontend/public/openapi.yaml` (path `/statistics/dns` ~บรรทัด 534, schema `DNSQueryStatistics`/`DNSDomainStat` ~4900+) | — |
| frontend test runner | **ไม่มี** → เกณฑ์ฝั่ง frontend คือ `yarn build` + `yarn lint` | `frontend/package.json` |

**สรุปหนึ่งบรรทัด:** ข้อมูลที่ต้องใช้ (`bucket.queries` + `bucket.ts`) **มีอยู่ครบแล้วใน loop ที่ `GetDNSQueryStatistics` วนอยู่ทุกวันนี้**
งานจริงคือ (ก) เพิ่ม DTO จุดกราฟ 1 ตัว + ฟิลด์ 1 ฟิลด์ในคำตอบเดิม (ข) plot ค่าลงแกนเวลาใน loop เดิม โดยไม่เพิ่มการ lock/สแกนใหม่ (ค) เขียน component กราฟแท่งใหม่ 1 ตัวฝั่ง frontend

## 2. แนวทางเทคนิค (ตัดสินใจแล้ว — developer ไม่ต้องเดา)

### 2.1 ทำไมขยาย `/api/statistics/dns` เดิม ไม่ทำ endpoint ใหม่

| ทาง | ผล |
|---|---|
| endpoint ใหม่ `/statistics/dns/series` | ต้องวน ring รอบที่สอง + จับ `s.dns.mu` รอบที่สอง ทุก 10 วินาที เพื่อข้อมูลที่ loop เดิมมีอยู่ในมือแล้ว, หน้าเดียวยิง 2 request, มีโอกาส `totalQueries` ของ card กับผลรวมกราฟไม่ตรงกัน (คนละ snapshot) |
| **ขยาย response เดิม (เลือกทางนี้)** | 0 request เพิ่ม, 0 lock เพิ่ม, invariant `sum(querySeries[].count) == totalQueries` จริงเสมอเพราะมาจาก loop เดียวกัน, additive JSON change (client เก่าไม่พัง) |

payload ที่เพิ่ม: `{"ts":"2026-08-06T10:05:00+07:00","count":123}` ≈ 45 bytes/จุด → 1h = 12 จุด ≈ 0.5 KB, 24h = 288 จุด ≈ 13 KB ต่อ poll (ทุก 10 วินาที) — ยอมรับได้บน LAN และไม่แตะดิสก์เลย

### 2.2 โครงสร้างข้อมูล (backend)

```go
// model/statistics.go — DTO ใหม่ วางถัดจาก BandwidthPoint
type DNSQueryPoint struct {
    Ts    string `json:"ts"`    // RFC3339 เวลาเริ่ม bucket (device local time + offset) เหมือน BandwidthPoint.Ts
    Count uint64 `json:"count"` // จำนวน DNS query ใน bucket นี้
}

// model/statistics.go — ฟิลด์ใหม่ใน DNSQueryStatistics
QuerySeries []DNSQueryPoint `json:"querySeries"`
```

กติกาที่ต้องเขียนไว้ใน doc comment และต้องเป็นจริง:

- ความยาวคงที่ = `statsWindowBucketCount(window)` เสมอ (3/6/12/36/72/144/288) — zero-fill, **ห้ามเป็น nil**
- เรียงเก่า → ใหม่, จุดสุดท้ายคือ bucket ปัจจุบันที่ยัง "ไม่ปิด" (ค่าจะยังไต่ขึ้นในรอบ refresh ถัดไป — ต้องเขียนกำกับใน UI)
- **invariant: `sum(QuerySeries[].Count) == TotalQueries`** — นี่คือกติกาที่สำคัญที่สุดของงานนี้ (เหตุผลเดียวกับ §6 item 1 ของ traffic plan) จึงต้องใช้ **carry rule** เดียวกับ `getTrafficBreakdown`: bucket ที่เก่ากว่าแกน → index 0, ใหม่กว่าแกน → index n-1, `ts` parse ไม่ผ่าน → index 0
- `enabled == false` → `QuerySeries` เป็น `[]model.DNSQueryPoint{}` (empty ไม่ใช่ nil, ตามธรรมเนียม early-return เดิมที่ `statistics_dns.go:50-56`)

### 2.3 จุดวางโค้ดและกติกา locking

plot ลงแกน **ใน loop เดิม** ที่ `statistics_dns.go:71-93` (`for _, b := range s.dnsWindowBuckets(window)`) โดย

1. คำนวณแกน (`n`, `axisStart`, สร้าง slice + เติม `Ts`) **ก่อน** `s.dns.mu.RLock()` ที่ `:43` — เหตุผลเดียวกับ comment ที่ `traffic_stats.go:1314-1323`: ถ้าคำนวณแกนใต้ lock คนละครั้งกับที่วน bucket จะเกิด race ที่ทำให้ invariant พังแบบเงียบ ๆ
2. ในลูป: `idx := statsSeriesIndex(b.ts, axisStart, n); series[idx].Count += b.queries`
3. **ห้าม** เพิ่มการเรียก `GetTrafficBreakdown*`/`hostLookup`/`domainIPs` ใด ๆ (กติกา 1 request = 1 call ที่ header comment `statistics_dns.go:26-34`) — งานนี้ไม่ต้องใช้ traffic เลย
4. **ห้าม** แตะ `recordDomainQuery` (hot path จาก `WatchDNSLog`)

### 2.4 ทำไมแท่งเดียวต่อ bucket (ไม่ stacked ตาม domain/type)

- stacked ต่อ domain ที่ 288 buckets × top-N domain = payload และงาน aggregate โตขึ้นหลายสิบเท่า เพื่อกราฟที่อ่านไม่ออกบนจอเดียว
- ยอดรวมต่อ bucket ตอบคำถามที่เจ้าของถามพอดี ("จำนวนครั้งในการ query ตามช่วงเวลา")
- ถ้าอยากได้ breakdown ทีหลัง → ทำที่หน้า drill-down (มี scope แคบอยู่แล้ว) ดีกว่ายัดในหน้า overview

### 2.5 การไม่สร้าง logic แกนเวลาซ้ำซ้อน

`traffic_stats.go:90-100` ประกาศกติกาไว้ชัดว่า "อย่าสร้าง map/switch ที่สองที่ไหนอีก ให้เรียก `normalizeStatsWindow`/`statsWindowBucketCount`" — งานนี้จึงต้อง **แยกโค้ดคำนวณแกนออกมาเป็น helper กลาง** แล้วให้ทั้ง `getTrafficBreakdown` (ของเดิม) และ DNS (ของใหม่) เรียกใช้ตัวเดียวกัน (T-01) แทนที่จะ copy-paste สูตร `axisStart`/`idx clamp` เป็นชุดที่สอง

> ข้อควรระวังของ T-01: `getTrafficBreakdown` คือหัวใจของ invariant `sum(Series[].Bytes) == Observed` — การ refactor นี้ต้องเป็น **pure refactor** (พฤติกรรมเท่าเดิมทุก byte) และมีเทสต์เดิมใน `traffic_stats_test.go` เป็นตัวคุม ถ้าเทสต์แดงแม้แต่ตัวเดียวให้ถอย T-01 กลับแล้วแจ้งกลับมา (ห้ามแก้เทสต์ให้ผ่าน)

### 2.6 frontend: component ใหม่ ไม่ดัดแปลง `TrafficTrendCard`

`TrafficTrendCard` ผูกกับ bytes ทั้งตัว (2 เส้น up/down, toggle Bytes/Speed, `fmtBytes`/`fmtRate`, subtitle เรื่องทิศทาง LAN) — การยัด mode "count" เข้าไปจะทำให้ component ที่ 4 หน้าใช้ร่วมกันเปราะขึ้นโดยไม่ได้ใช้ซ้ำอะไรจริง ๆ นอกจากสี/ธีม
จึงสร้าง `DnsQueryTrendCard.tsx` ใหม่ โดย **ลอก pattern** (ไม่ลอกโค้ดทั้งก้อน) เหล่านี้จาก `TrafficTrendCard`:

- `useTheme()` → สี grid/axis (`rgba(...)` 2 ชุด light/dark) และ `var(--primary)` สำหรับแท่ง — **ห้าม** hardcode สีแบรนด์เป็น class เช่น `bg-emerald-500` (`docs/rules_of_work.md`)
- `tickInterval = Math.max(0, Math.ceil(data.length / 10) - 1)` (ให้ label ~8-12 อันทุก window)
- `isAnimationActive={false}` (สำคัญที่ 288 แท่ง + refresh ทุก 10 วิ)
- label แกน X: `toLocaleTimeString(undefined, {hour:"2-digit",minute:"2-digit",hour12:false})`
- ห้ามมี `shadow-*` / `backdrop-blur-*` (flat design rule)

## 3. Task list (ให้ ai-developer ทำเรียงตามลำดับ)

> ทำงานบน branch `feat/statistics-dns-page` (มี PR 127 อยู่แล้ว) · **ห้าม commit เว้นแต่เจ้าของสั่ง**
> **ห้ามทดสอบทีละ task** — ทำครบทุก task แล้วให้ ai-qa ทดสอบรวมตาม §5 ครั้งเดียว

```json
{
  "task_id": "T-01",
  "title": "แยก helper แกนเวลา (statsSeriesAxis/statsSeriesIndex) ออกจาก getTrafficBreakdown — pure refactor",
  "layer": "service",
  "files": ["backend/internal/service/traffic_stats.go"],
  "instruction": "เพิ่มฟังก์ชัน 2 ตัวใน traffic_stats.go วางถัดจาก lastNBuckets (~:144) เพื่อให้สูตรแกนเวลาของ statistics ทุก ring มีที่มาที่เดียว (ดูกติกาที่ comment :90-100): (1) `func statsSeriesAxis(window string) (axisStart time.Time, n int)` — n = statsWindowBucketCount(window); axisEnd = time.Now().Truncate(trafficDetailBucketSpan); axisStart = axisEnd.Add(-time.Duration(n-1) * trafficDetailBucketSpan). (2) `func statsSeriesIndex(ts string, axisStart time.Time, n int) int` — parse ts ด้วย time.RFC3339; parse ไม่ผ่านคืน 0; มิฉะนั้น idx = int(bt.Sub(axisStart) / trafficDetailBucketSpan) แล้ว clamp เป็น [0, n-1] (carry rule เดิมเป๊ะ). เขียน doc comment อธิบายว่า carry rule นี้คือสิ่งที่ทำให้ sum(series) == total เป็นจริงแม้ window นับเป็นจำนวน bucket ไม่ใช่ช่วงเวลา (อ้าง comment เดิมที่ :1395-1402). จากนั้นแก้ getTrafficBreakdown ให้เรียก helper ทั้งสอง แทนโค้ดคำนวณเดิมที่ :1324-1330 และ :1403-1411 — โดยยังคงลำดับเดิมทุกอย่าง: คำนวณแกน BEFORE s.mu.RLock และใช้ n ตัวเดียวกันทั้งกับแกนและ lastNBuckets. ห้ามเปลี่ยนพฤติกรรม/ผลลัพธ์ใด ๆ ห้ามแก้ไฟล์เทสต์ ห้ามเปลี่ยน signature ของ method สาธารณะ. ถ้าเทสต์เดิมแดงแม้แต่ตัวเดียว ให้ถอยการแก้ไฟล์นี้ทั้งหมดแล้วรายงานกลับ (ห้ามแก้เทสต์ให้ผ่าน).",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "go test ./internal/service/... ผ่านทั้งหมดโดยไม่มีการแก้ไฟล์ _test.go แม้แต่บรรทัดเดียว",
    "getTrafficBreakdown ไม่มีสูตร axisStart/idx clamp เขียนซ้ำอีกแล้ว (เรียก helper อย่างเดียว)",
    "statsSeriesAxis ยังถูกเรียกก่อน s.mu.RLock() เหมือนเดิม"
  ],
  "depends_on": []
}
```

```json
{
  "task_id": "T-02",
  "title": "model: เพิ่ม DNSQueryPoint + ฟิลด์ QuerySeries ใน DNSQueryStatistics",
  "layer": "model",
  "files": ["backend/internal/model/statistics.go"],
  "instruction": "1) เพิ่ม `type DNSQueryPoint struct { Ts string `json:\"ts\"`; Count uint64 `json:\"count\"` }` วางถัดจาก BandwidthPoint (~:124) พร้อม doc comment ที่ระบุชัดว่า: นี่คือ 'จำนวนครั้งของ DNS query' ไม่ใช่ bytes (จงใจไม่ใช้ BandwidthPoint ซ้ำเพื่อไม่ให้เกิดการอ่านค่าผิดชนิด), Ts เป็น RFC3339 เวลาเริ่ม bucket 5 นาที ในเวลาท้องถิ่นของอุปกรณ์ (มี offset) เหมือน BandwidthPoint.Ts, ข้อมูล RAM-only จาก ring ใน service/dns_query_stats.go ไม่เคย persist. 2) เพิ่มฟิลด์ `QuerySeries []DNSQueryPoint `json:\"querySeries\"`` ใน DNSQueryStatistics (~:430-477) วางถัดจาก TotalQueries พร้อม doc comment ที่ระบุครบ 4 ข้อ: (ก) ความยาวคงที่เท่ากับจำนวน bucket ของ window (3/6/12/36/72/144/288) zero-filled ไม่เคยเป็น nil, (ข) เรียงเก่า->ใหม่ จุดสุดท้ายคือ bucket ปัจจุบันที่ยังเก็บไม่ครบ 5 นาที, (ค) invariant sum(QuerySeries[].Count) == TotalQueries เสมอ (carry rule เดียวกับ TrafficStatistics.Series), (ง) เมื่อ Enabled=false จะเป็น slice ว่าง. เป็นการเปลี่ยน JSON แบบ additive เฉพาะ endpoint นี้ ห้ามแตะ DTO อื่น.",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "ไม่มีการแก้ไข struct อื่นนอกจาก DNSQueryStatistics",
    "doc comment ครบทั้ง 4 ข้อตาม instruction"
  ],
  "depends_on": []
}
```

```json
{
  "task_id": "T-03",
  "title": "service: สร้าง QuerySeries ใน GetDNSQueryStatistics โดยใช้ loop/lock เดิม",
  "layer": "service",
  "files": ["backend/internal/service/statistics_dns.go"],
  "instruction": "แก้ GetDNSQueryStatistics (:40-204) เท่านั้น: 1) หลังบรรทัด normalizeStatsWindow (:41) และ **ก่อน** s.dns.mu.RLock() (:43) ให้เรียก `axisStart, n := statsSeriesAxis(window)` แล้วสร้าง `querySeries := make([]model.DNSQueryPoint, n)` เติม Ts ของทุกจุดด้วย axisStart.Add(time.Duration(i)*trafficDetailBucketSpan).Format(time.RFC3339) พร้อม comment อ้างเหตุผลว่าทำไมต้องคำนวณก่อนจับ lock (เหมือน traffic_stats.go:1314-1323 — กัน race ที่ทำให้ invariant พังเงียบ ๆ). 2) ใน early-return ตอน !enabled (:50-56) ให้ใส่ `QuerySeries: []model.DNSQueryPoint{}` (empty ไม่ใช่ nil, ไม่ใช่ตัวที่สร้างไว้ข้างบน) — ห้ามเปิดเผยข้อมูลใด ๆ ตอนปิดสวิตช์. 3) ใน loop เดิม `for _, b := range s.dnsWindowBuckets(window)` (:71-93) เพิ่ม 2 บรรทัด: `idx := statsSeriesIndex(b.ts, axisStart, n)` และ `querySeries[idx].Count += b.queries` — วางไว้ตำแหน่งเดียวกับที่ `totalQueries += b.queries` (:89) เพื่อให้เห็นชัดว่าเป็นค่าเดียวกันคนละมุมมอง และเขียน comment ระบุ invariant sum == TotalQueries. 4) ใส่ `QuerySeries: querySeries` ใน return ก้อนสุดท้าย (:190-203). ห้ามเพิ่มการเรียก GetTrafficBreakdown*/hostLookup/domainIPs/reverseCache ใด ๆ, ห้ามวน ring รอบที่สอง, ห้ามจับ lock เพิ่ม, ห้ามแตะเมธอดอื่นในไฟล์นี้ (GetDNSDomainClients/GetDNSClientDomains/GetDNSIPDomains) และห้ามแตะ dns_query_stats.go.",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "GetDNSQueryStatistics ยังมี s.dns.mu.RLock()/RUnlock() คู่เดียวเหมือนเดิม และยังมีการเรียก GetTrafficBreakdown 1 ครั้ง + hostLookup 1 ครั้งเท่าเดิม",
    "มีการวน s.dnsWindowBuckets(window) เพียงลูปเดียวในเมธอดนี้",
    "ไม่มีการแก้เมธอดอื่นในไฟล์"
  ],
  "depends_on": ["T-01", "T-02"]
}
```

```json
{
  "task_id": "T-04",
  "title": "backend tests: invariant ของ QuerySeries",
  "layer": "service",
  "files": ["backend/internal/service/statistics_dns_test.go"],
  "instruction": "เพิ่มเทสต์ใหม่ในไฟล์เดิม (ดูวิธี setup service/ป้อน event ของเทสต์ที่มีอยู่ เช่น :231 ที่เช็คความยาว Series ของ drill-down เป็นตัวอย่าง) ครอบคลุม: 1) ความยาว QuerySeries == statsWindowBucketCount(window) สำหรับอย่างน้อย 3 window (15m, 1h, 24h). 2) sum(QuerySeries[].Count) == TotalQueries หลังป้อน RecordDNSEvent หลายรายการ. 3) เรียงเก่า->ใหม่: ts ของทุกจุดเพิ่มขึ้นทีละ 5 นาที และจุดสุดท้ายคือ bucket ปัจจุบัน. 4) enabled=false -> QuerySeries เป็น slice ว่าง (len 0) และต้อง **ไม่ใช่ nil** (เช็ค `q.QuerySeries == nil` ต้องเป็น false เพื่อกัน `null` ใน JSON). 5) กรณีไม่มี query เลยแต่เปิดสวิตช์ -> ความยาวยังเต็มตาม window และทุก Count == 0. 6) carry rule: ยัด bucket ที่ ts เก่ากว่าแกน (แก้ s.dns.buckets โดยตรงในเทสต์แบบเดียวกับที่เทสต์เดิมทำ ถ้าเทสต์เดิมทำได้) แล้วยืนยันว่า Count ไปรวมที่ index 0 และ sum ยังเท่ากับ TotalQueries. ห้ามแก้เทสต์เดิมที่มีอยู่.",
  "acceptance": [
    "cd backend && go test ./internal/service/... ผ่านทั้งหมด",
    "เทสต์ใหม่ครอบคลุมครบทั้ง 6 ข้อ",
    "ไม่มีการแก้/ลบเทสต์เดิม"
  ],
  "depends_on": ["T-03"]
}
```

```json
{
  "task_id": "T-05",
  "title": "openapi + doc comment ของ handler",
  "layer": "api",
  "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml", "backend/internal/api/handlers.go"],
  "instruction": "1) เพิ่ม schema `DNSQueryPoint` (properties: ts เป็น string format date-time, count เป็น integer) และเพิ่ม property `querySeries` (type array, items $ref DNSQueryPoint) ใน schema `DNSQueryStatistics` พร้อม description ภาษาเดียวกับของเดิมในไฟล์ ที่ระบุ: ความยาวคงที่ตาม window, zero-filled ไม่เคยเป็น null, เรียงเก่า->ใหม่, invariant sum(count) == totalQueries, และเป็น slice ว่างเมื่อ enabled=false. 2) แก้ description ของ path GET /statistics/dns (~บรรทัด 534) เพิ่มประโยคว่าตอนนี้คืน time series จำนวน query ต่อ bucket 5 นาทีมาด้วย. 3) **สำคัญ: docs/openapi.yaml และ frontend/public/openapi.yaml ต้องเหมือนกันทุก byte** — แก้ไฟล์แรกแล้วคัดลอกทับไฟล์ที่สอง แล้วยืนยันด้วย diff ว่าไม่ต่างกัน (ห้ามแก้ backend/internal/api/dist/openapi.yaml และ frontend/dist/ — เป็น build artifact). 4) เติมย่อหน้าเดียวใน doc comment ของ HandleGetDNSQueryStatistics (handlers.go:448-461) ว่า response มี querySeries เพิ่ม โดยมาจาก ring RAM-only ตัวเดิม ไม่มี input ใหม่จาก client (ยังรับแค่ window ที่ whitelist แล้ว) และห้ามแก้โค้ดใน handler.",
  "acceptance": [
    "diff docs/openapi.yaml frontend/public/openapi.yaml ไม่มีความต่าง",
    "yaml ทั้งสองไฟล์ parse ได้ (เปิดหน้า ApiDocs ได้ / yaml lint ผ่าน)",
    "โค้ดใน handlers.go ไม่ถูกแก้ (เฉพาะ comment)",
    "ไม่มีการแก้ไฟล์ใน dist/"
  ],
  "depends_on": ["T-02"]
}
```

```json
{
  "task_id": "T-06",
  "title": "frontend service: type DNSQueryPoint + querySeries + mock",
  "layer": "frontend",
  "files": ["frontend/src/services/dnsStatisticsService.ts"],
  "instruction": "1) เพิ่ม `export interface DNSQueryPoint { ts: string; count: number }` พร้อม comment ที่ mirror doc comment ของ Go struct (จำนวนครั้ง ไม่ใช่ bytes, RAM-only, sum == totalQueries) วางไว้ใกล้ ๆ DNSDomainStat. 2) เพิ่มฟิลด์ `querySeries: DNSQueryPoint[]` ใน interface DNSQueryStatistics (:119-141) พร้อม comment ระบุ invariant + ความยาวคงที่ตาม window. 3) ใน branch IS_MOCK_MODE ของ getDNSStatistics (:459-546) ให้สร้าง querySeries โดย **ใช้ mockBandwidthSeries ซ้ำ** (import อยู่แล้วที่ :2): `const querySeries = mockBandwidthSeries(window, totalQueries).series.map((p) => ({ ts: p.ts, count: p.bytes }))` — เขียน comment อธิบายว่าใช้ shape generator ตัวเดียวกับ series อื่นเพื่อได้ทั้งความยาวตาม window, ช่องว่าง zero-fill, และ invariant ผลรวมเป๊ะ = totalQueries ฟรี ๆ (ค่า bytes ในที่นี้ถูกใช้เป็น 'จำนวนครั้ง' โดยตั้งใจ เฉพาะใน mock เท่านั้น) แล้วใส่ลง return object. ห้ามแตะ mock ของ endpoint อื่นในไฟล์นี้.",
  "acceptance": [
    "cd frontend && yarn build ผ่าน (tsc -b ไม่มี error)",
    "yarn lint ผ่าน",
    "mock: sum(querySeries[].count) === totalQueries และ length ตรงกับ STATS_WINDOWS[].points ของ window นั้น"
  ],
  "depends_on": ["T-02"]
}
```

```json
{
  "task_id": "T-07",
  "title": "frontend: component ใหม่ DnsQueryTrendCard (recharts BarChart)",
  "layer": "frontend",
  "files": ["frontend/src/components/statistics/DnsQueryTrendCard.tsx"],
  "instruction": "สร้าง component ใหม่ `export function DnsQueryTrendCard({ series, window, className }: { series: DNSQueryPoint[] | undefined; window: StatsWindow; className?: string })` โดยลอกโครง/สไตล์จาก components/statistics/TrafficTrendCard.tsx (อ่านให้ครบก่อนเขียน) แต่เปลี่ยนเป็นกราฟแท่งและหน่วยจำนวนครั้ง: 1) ใช้ Card/CardHeader/CardTitle/CardContent จาก @/components/ui/card, สีเส้น grid/axis จาก useTheme() แบบเดียวกับ TrafficTrendCard (:51-56) ห้าม hardcode สีแบรนด์เป็น class เช่น text-emerald-500, สีแท่งใช้ `var(--primary)`. 2) recharts: ResponsiveContainer > BarChart (data) > CartesianGrid strokeDasharray='3 3' vertical={false} > XAxis dataKey='time' interval={tickInterval} > YAxis (allowDecimals={false}, tickFormatter ใช้ toLocaleString(), width 48) > Tooltip (contentStyle เดียวกับ TrafficTrendCard :183-189, formatter คืน [`${Number(value).toLocaleString()} ครั้ง / 5 นาที`, 'Queries']) > Bar dataKey='count' name='Queries' fill='var(--primary)' radius={[2,2,0,0]} isAnimationActive={false}. 3) data = series.map(p => ({ time: label จาก new Date(p.ts).toLocaleTimeString(undefined,{hour:'2-digit',minute:'2-digit',hour12:false}) หรือ p.ts ดิบเมื่อ parse ไม่ได้, count: p.count })) ใน useMemo. 4) tickInterval = Math.max(0, Math.ceil(data.length / 10) - 1) เหมือนเดิม. 5) หัวการ์ด: title `DNS Queries · ${statsWindowLongLabel(window)}` (import จาก @/lib/statsWindow) + คำอธิบายบรรทัดเล็ก 'จำนวน query ต่อช่วง 5 นาที · แท่งสุดท้ายยังเก็บข้อมูลไม่ครบช่วง · เก็บใน RAM เท่านั้น'. 6) empty state: ถ้าไม่มีจุดไหน count > 0 ให้แสดงข้อความ 'ยังไม่มี DNS query ในช่วงเวลานี้' กลางกล่องสูงเท่ากัน (h-56 เหมือน TrafficTrendCard :156). 7) ห้ามใช้ shadow-*/backdrop-blur-*, ต้องดูดีทั้ง light/dark, ห้ามเพิ่ม npm package, ห้ามแก้ TrafficTrendCard.",
  "acceptance": [
    "cd frontend && yarn build + yarn lint ผ่าน",
    "ไฟล์ใหม่ไม่มี class สีแบบ hardcode (emerald/blue/... ) และไม่มี shadow-*/backdrop-blur-*",
    "ไม่มีการแก้ TrafficTrendCard.tsx",
    "ไม่มี dependency ใหม่ใน package.json"
  ],
  "depends_on": ["T-06"]
}
```

```json
{
  "task_id": "T-08",
  "title": "frontend: วางกราฟลงหน้า Statistics DNS overview",
  "layer": "frontend",
  "files": ["frontend/src/pages/StatisticsDns.tsx"],
  "instruction": "import DnsQueryTrendCard แล้ววางไว้ **หลังแถว stat card 4 ใบ (:190-204) และก่อน {stats?.truncated && <DnsStatsTruncatedWarning />} (:206)** โดย render เฉพาะเมื่อ `stats && stats.enabled` (เงื่อนไขเดียวกับ stat cards) ส่งพร็อพ `series={stats.querySeries}` และ `window={window_}`. ห้ามเปลี่ยน logic การโหลดข้อมูล/interval/URL param ใด ๆ, ห้ามให้โหมด IP filter (ipMode) มีผลกับการแสดงกราฟนี้ (กราฟเป็นข้อมูลระดับ window ทั้งระบบเสมอ เหมือน stat cards), ห้ามยิง request เพิ่ม — ใช้ข้อมูลจาก stats ที่โหลดอยู่แล้ว. อัปเดต comment หัวไฟล์ (:27-42) เพิ่ม 1-2 บรรทัดว่าหน้านี้มีกราฟแท่งจำนวน query ต่อ bucket แล้ว พร้อมอ้างไฟล์แผนนี้.",
  "acceptance": [
    "cd frontend && yarn build + yarn lint ผ่าน",
    "จำนวน fetch/useEffect ในไฟล์เท่าเดิม (ไม่มี request ใหม่)",
    "กราฟอยู่ระหว่างแถว stat card กับ truncated warning และแสดงเมื่อ enabled=true เท่านั้น"
  ],
  "depends_on": ["T-06", "T-07"]
}
```

## 4. ข้อควรระวัง (ให้ ai-qa ใช้ตรวจด้วย)

1. **invariant ผลรวม**: `sum(querySeries[].count)` ต้องเท่ากับ `totalQueries` ที่การ์ดใบแรกโชว์ **ทุก window** — ถ้าไม่ตรงแปลว่า carry rule หรือแกนเวลาผิด
2. **ห้ามคำนวณแกนหลังจับ lock** — เป็นบั๊ก race ที่ traffic plan revision 2 เคยแก้มาแล้ว อย่าให้กลับมาอีก
3. **ห้ามเพิ่ม lock/loop/HTTP request** — งานนี้ต้องมีต้นทุน runtime ~0 (เพิ่มแค่ n≤288 ครั้งของการบวก uint64)
4. **ห้ามแตะ hot path** `recordDomainQuery` / `RecordDNSEvent` (รันบน read loop ของ kernel layer)
5. **ห้าม persist** อะไรลง SQLite — ข้อมูลนี้ RAM-only ตามหลักการ SD card wear
6. **privacy**: เมื่อ `enabled=false` ห้ามคืนข้อมูลเวลา/จำนวนใด ๆ (ต้องเป็น slice ว่าง) และห้าม log ชื่อโดเมน
7. **openapi 2 ไฟล์ต้องเท่ากันทุก byte** (docs/ + frontend/public/) และห้ามแก้ dist/
8. `querySeries` เป็นฟิลด์ใหม่ — client เก่าที่ไม่รู้จักต้องไม่พัง (additive เท่านั้น ห้ามลบ/เปลี่ยนชนิดฟิลด์เดิม)
9. งานนี้ **ไม่ใช่งาน security-sensitive** (ไม่แตะ auth/firewall/D-Bus/Netlink/input ใหม่) — input เดียวคือ `window` ที่ whitelist อยู่แล้ว ห้าม developer เพิ่ม query param ใหม่ใด ๆ
10. ที่ 24h กราฟจะมี 288 แท่ง — ต้องยืนยันด้วยตาว่ายังอ่านได้และไม่กระตุก (นี่คือเหตุผลที่บังคับ `isAnimationActive={false}`)

## 5. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ทดสอบครั้งเดียวหลังทำครบทุก task)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... ผ่าน และ go test ./... ผ่านทั้งหมด (รวมเทสต์เดิมของ traffic ที่ไม่ถูกแก้)",
    "cd frontend && yarn build ผ่าน และ yarn lint ไม่มี error ใหม่",
    "รัน ./pigate-backend -mock=true แล้วเรียก GET /api/statistics/dns?window=1h: มี querySeries ความยาว 12, ts เรียงเก่า->ใหม่ห่างกันจุดละ 5 นาที, และ sum(count) == totalQueries",
    "เรียกซ้ำด้วย window=15m / 24h ได้ความยาว 3 / 288 ตามลำดับ และ invariant ผลรวมยังจริงทุกครั้ง",
    "ปิดสวิตช์เก็บสถิติ DNS แล้วเรียก endpoint เดิม: querySeries เป็น [] (ไม่ใช่ null) และไม่มีข้อมูลอื่นรั่วออกมา",
    "หน้า /statistics/dns (mock mode): เห็นการ์ดกราฟแท่งใหม่อยู่ใต้ stat card 4 ใบ และเหนือคำเตือน/ตาราง 2 คอลัมน์",
    "กดสลับ window ที่ StatsWindowTabs: จำนวนแท่งเปลี่ยนตาม (3/6/12/36/72/144/288) และ URL ?window= ยังทำงานเหมือนเดิม",
    "พิมพ์ IP ลงช่อง filter ของ Top Domains จนเข้าโหมด IP: กราฟแท่ง **ไม่เปลี่ยน** (ยังเป็นข้อมูลระดับ window ทั้งระบบ) และ Top Domains ยังสลับเป็นผลของ IP ได้ตามเดิม",
    "ยังไม่เปิดเก็บสถิติ DNS: ไม่มีการ์ดกราฟแสดงเลย (เห็นเฉพาะ empty state เดิม 'ยังไม่ได้เปิดการเก็บสถิติ DNS')",
    "เปิดสถิติแต่ยังไม่มี query: การ์ดแสดงพร้อมข้อความ 'ยังไม่มี DNS query ในช่วงเวลานี้' ไม่ใช่กราฟเปล่า/หน้าแตก",
    "hover แท่งใด ๆ: tooltip บอกจำนวนครั้ง + '/ 5 นาที' และเวลาแกน X อ่านออก (ไม่ทับกัน) ทั้งที่ 15m และ 24h",
    "สลับ dark/light: กราฟอ่านได้ทั้งสองโหมด, ไม่มีเงา/blur, สีแท่งมาจากตัวแปรธีม",
    "diff docs/openapi.yaml frontend/public/openapi.yaml = ไม่ต่างกัน และหน้า ApiDocs เปิดได้ปกติ",
    "git diff: ไม่มีการแก้ไฟล์ใน backend/internal/api/dist/ หรือ frontend/dist/, ไม่มี package ใหม่ใน package.json/go.mod, ไม่มีการแก้ไฟล์ _test.go เดิม",
    "หน้า /statistics/dns/domain/:domain และ /statistics/dns/client/:client ยังทำงานเหมือนเดิมทุกอย่าง (regression guard — ไม่ควรมีอะไรเปลี่ยน)"
  ]
}
```

## 6. ทางเลือกที่เสนอให้เจ้าของตัดสิน (ไม่รวมในแผนนี้)

| # | ทางเลือก | ข้อดี | ข้อเสีย |
|---|---|---|---|
| A | เพิ่มกราฟแท่งจำนวน query ในหน้า drill-down ของ domain/client ด้วย | เทียบ "จำนวนครั้ง" กับ "ปริมาณ bytes" ในหน้าเดียวกันได้ | ต้องเพิ่ม querySeries ให้อีก 2-3 endpoint (งานเพิ่ม ~1 เท่าตัวของแผนนี้) |
| B | รวมแท่งเป็นราย 15 นาที/ราย 1 ชม. เมื่อเลือก window ยาว (12h/24h) แทน 288 แท่ง | อ่านง่ายขึ้นมากที่ window ยาว | เพิ่ม logic aggregate ที่ frontend และทำให้ "1 แท่ง = 1 bucket" ไม่จริงอีกต่อไป (เจ้าของขอ "แต่ละ bucket" มาตรง ๆ) |
| C | ซ้อนแท่งแยกตาม query type (A/AAAA/PTR/…) | เห็นสัดส่วนชนิด query | ring ปัจจุบันเก็บ `typeByDomain` (ชนิดล่าสุดต่อโดเมน) ไม่ได้เก็บ count แยกตามชนิด → ต้องแก้โครงสร้าง bucket + RAM เพิ่ม |

แผนนี้เลือกทำเฉพาะสิ่งที่เจ้าของขอตรง ๆ (หน้า overview, 1 แท่ง = 1 bucket) — ถ้าต้องการ A/B/C ให้สั่งเพิ่มเป็นแผนถัดไป
