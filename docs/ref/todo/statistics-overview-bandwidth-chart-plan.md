# Statistics Overview — กราฟ Bandwidth ตามช่วงเวลา + การ์ด Top 5 Hosts

> แผนงานฟีเจอร์: เพิ่มแถวบนสุดของหน้า `/statistics/overview` เป็น 2 องค์ประกอบในแถวเดียวกัน
> — กราฟ **Bandwidth over time** (กว้าง 2/3) และการ์ด **Top 5 Hosts by usage** (กว้าง 1/3)
> โดย **ไม่สร้าง data pipeline ใหม่**: ใช้ bucket ring ของ `TrafficStatsService` ที่มีอยู่แล้ว
> (5 นาที × 288 = 24 ชม.) ซึ่งหลัง PR #117 เก็บทิศทาง orig/reply แยกอยู่แล้ว
>
> วันที่เขียน: 2026-08-01 · Branch: `feat/statistics-overview-charts` (ตั้งต้นจาก `main`)
> README Feature Status: Statistics ยัง Completed เหมือนเดิม (เพิ่มการนำเสนอ ไม่ใช่ subsystem ใหม่)
>
> **แก้ไขครั้งที่ 1 (2026-08-01):** เจ้าของโปรเจกต์ตอบคำถามค้างครบทั้ง 5 ข้อแล้ว —
> ข้อสรุปถูก lock ไว้ที่ §7 และแทรกกลับเข้า §2.3 / T-03 / T-04 / T-06 / T-07 / §5 / §6
> จุดที่เปลี่ยนจากร่างแรกมากที่สุดคือ **24h ใช้ bucket ดิบ 5 นาที 288 จุด** (ไม่ยุบรายชั่วโมง)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
- แถวบนสุดของ Overview: กราฟเส้น bandwidth ตามเวลา (`lg:col-span-2`) + การ์ด Top 5 Hosts
  (`lg:col-span-1`) ในกริด `lg:grid-cols-3`; จอแคบ stack เป็น 1 คอลัมน์ตามแพตเทิร์นเดิมของหน้า
- กราฟผูกกับ `StatsWindowSelect` (1h/24h) เดิม และ auto-refresh 10 วินาทีเดิม
  (ไม่เพิ่ม polling/interval ใหม่)
- ตัวเลขในกราฟต้อง **สอดคล้องกับหน้าเดียวกัน**: ผลรวมทุกจุดของ series ต้องเท่ากับ
  `observedBytes` ของ window นั้นเป๊ะ (ห้ามใช้แหล่งข้อมูลคนละชุดกับการ์ดอื่นในหน้า)
- การ์ด Top 5 Hosts เลียนแบบการ์ด **Protocol Breakdown** ของ Dashboard แท็บ Detailed
  (`Dashboard.tsx:813-859`): แถบ stacked bar เดียวด้านบน + legend list มีจุดสี/ชื่อ/`bytes · percent`
- ไม่มี endpoint ใหม่, ไม่มี kernel capability ใหม่, ไม่มี migration, ไม่ persist อะไรเพิ่ม

**นอกขอบเขต (ตัดชัดเจน)**
- **ไม่ใช้ `/api/dashboard/traffic` (`TrafficHistory`)** เป็นแหล่งข้อมูลของกราฟนี้ — ดูเหตุผลที่ §2.4
- ไม่แตะการ์ด Dashboard ใด ๆ ยกเว้นการย้ายค่าคงที่ `CHART_BG_CLASSES` ออกไปเป็น shared (T-08)
- ไม่แตะ Top Denied / Top Queried Domains / Top Conversations
- ไม่เพิ่ม window ใหม่ (เช่น 7d) — ring buffer เก็บได้แค่ 24 ชม. และเป็น RAM ล้วนตามเจตนาเดิม
- ไม่ทำ drill-down จากกราฟ/การ์ดใหม่ (คลิกแล้วเจาะดูรายเครื่อง) — งานแยกถ้าต้องการ
- **ไม่ downsample ที่ API** (มติข้อ 5) — ถ้าจำเป็นต้องลดจุดเพื่อความลื่น ให้ทำที่คอมโพเนนต์
  กราฟฝั่ง frontend เท่านั้น (ดู T-07 / §5 ข้อ 13)

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-08-01)

| ส่วน | สถานะ | อ้างอิง (file:line โดยประมาณ) |
|---|---|---|
| bucket ring 5 นาที × 288 | **มีแล้ว** — `trafficDetailBucket{ts, hostBytes, catBytes, ruleBytes, dstBytes, convBytes, observed}` | `service/traffic_stats.go:89-111`, const `:27-30` |
| ทิศทางต่อ host/dst/conv | **มีแล้ว** `dirBytes{Orig,Reply}` หลัง PR #117 | `service/traffic_stats.go:113-120` |
| ยอดรวมต่อ bucket | มีแต่ `observed uint64` — **ยังไม่มีมิติทิศทาง** | `service/traffic_stats.go:94`, `addBucket :717,730,741` |
| accessor ของหน้า Statistics | `GetTrafficBreakdown(window)` คืน map รวมทั้ง window (ยุบ bucket ทิ้ง) — **ไม่มี series ตามเวลา** | `service/traffic_stats.go:918-...` |
| การเลือก window | slice ท้าย 12 buckets สำหรับ 1h, ทั้ง ring สำหรับ 24h | `traffic_stats.go:847-855`, `:930-939` |
| ประกอบ response | `GetStatistics` | `service/statistics.go:187-232` |
| helper ที่ใช้ซ้ำได้ | `isPrivateIP` `service/statistics.go:402`, `percentOf` `traffic_stats.go:1060` | — |
| DTO | `model.TrafficStatistics` (ไม่มี series), `model.TopHost` มี `bytesUp/bytesDown` แล้ว | `model/statistics.go:98-143`, `:14-40` |
| handler / route | `HandleGetStatistics` + `authRoute("GET /api/statistics/traffic")` — **ไม่ต้องแก้** | `api/handlers.go:420`, `api/router.go:42` |
| gzip/compression ของ API | **ไม่มี middleware บีบอัดใด ๆ** — JSON ออกไปดิบ (เกี่ยวกับขนาด payload ของ series) | grep `gzip`/`Content-Encoding` ใน `backend/internal/api/` = ไม่พบ |
| openapi | `TopHost` :3993, `TrafficStatistics` :4187 — เลขบรรทัดตรงกันทั้ง `docs/` และ `frontend/public/` | ทั้ง 2 ไฟล์ |
| frontend client | `TrafficStatistics` + mock branch สังเคราะห์ครบ | `frontend/src/services/statisticsService.ts:70-98`, `:202-249` |
| หน้า Overview | กริดเดียว `grid-cols-1 lg:grid-cols-2` มี 5 การ์ด, poll 10 วิ | `frontend/src/pages/StatisticsOverview.tsx:499-522`, `:30`, `:426-430` |
| การ์ดต้นแบบ Protocol Breakdown | `ProtocolBreakdownCard` + `CHART_BG_CLASSES` (`bg-chart-1..5`) | `frontend/src/pages/Dashboard.tsx:786`, `:813-859` |
| แพตเทิร์นกราฟที่มีอยู่ | `BandwidthCard` ใช้ recharts ตรง ๆ (`LineChart`/`ResponsiveContainer`) + `useTheme` คุมสี grid/axis; `Card className="lg:col-span-2"` อยู่แล้ว | `Dashboard.tsx:360-439`, `aggregateHourly :220-239` |
| ตัวแปรสี chart | `--chart-1..5` เป็น grayscale ทั้ง dark/light | `frontend/src/index.css:93-97`, `:130-134` |
| recharts | มีใน dependency แล้ว (`components/ui/chart.tsx` + Dashboard) — **ไม่ต้องเพิ่ม dep** | — |

**สรุป:** ข้อมูลที่ต้องใช้อยู่ครบใน bucket ring แล้ว งานแบ็กเอนด์เหลือแค่ (ก) ให้ยอดรวมต่อ bucket
มีมิติทิศทาง และ (ข) เพิ่ม accessor ที่คืนค่า "ต่อ bucket" แทนที่จะยุบรวม ส่วน Top 5 Hosts
**ไม่ต้องแตะแบ็กเอนด์เลย** (ใช้ `topSources` ที่มีอยู่ ตัด 5 แถวแรก)

## 2. แนวทางเทคนิค

### 2.1 DTO ใหม่ (additive บน endpoint เดิม)

```go
// model/statistics.go
type BandwidthPoint struct {
    Ts        string `json:"ts"`        // RFC3339 จุดเริ่ม bucket (device local time เหมือน TrafficBucket)
    Bytes     uint64 `json:"bytes"`     // = BytesUp + BytesDown เสมอ
    BytesUp   uint64 `json:"bytesUp"`
    BytesDown uint64 `json:"bytesDown"`
}
// เพิ่มใน TrafficStatistics: Series []BandwidthPoint `json:"series"`
```

### 2.2 ทิศทางระดับ bucket — LAN-relative ไม่ใช่ flow-relative

`dirBytes` ที่มีอยู่เป็น **flow-relative** (Orig = src→dst) ถ้าเอามารวมเป็นยอดของทั้งเครือข่ายตรง ๆ
flow ที่ริเริ่มจากอินเทอร์เน็ต (port forward, สแกน) จะสลับด้าน ทำให้กราฟรวม "Upload/Download"
มีความหมายปนกัน → แปลงเป็น LAN-relative **ตอนคำนวณ delta ที่เดียว** โดยดูจาก IP:

| src / dst | up (ออกจาก LAN) | down (เข้า LAN) |
|---|---|---|
| private → public | Orig | Reply |
| public → private | Reply | Orig |
| private→private / public→public | Orig | Reply (คงเดิม, บันทึกไว้ใน doc comment) |

ผลการจำแนกคำนวณ **ครั้งเดียวตอนสร้าง `flowSampleState`** แล้วเก็บเป็น `lanFlip bool` ในนั้น
(ไม่ parse IP ซ้ำทุก poll — ที่ 50k flows × ทุก 10 วิ บน Pi 5 คุ้มค่ามาก)

โครงสร้างที่เปลี่ยน: `trafficDetailBucket.observed` จาก `uint64` → `dirBytes`
(คงหลัก "แหล่งความจริงเดียว" ของ §2.3 ในแผน PR #117: ยอดรวมเป็น method `Total()` ไม่ใช่ฟิลด์ที่ 3)

### 2.3 ความละเอียดของ series — bucket ดิบทั้งสอง window (มติข้อ 5)

- `1h` → **12 จุด** × 5 นาที
- `24h` → **288 จุด** × 5 นาที (bucket ดิบทั้ง ring — **ไม่ยุบรายชั่วโมง**)
- **เติมช่องว่างด้วยศูนย์**: ring จะไม่มี bucket เลยในช่วงที่เงียบสนิท (`poll()` return ก่อน
  `addBucket` ที่ `:414-418`) → ต้อง pad ให้แกนเวลาต่อเนื่อง ไม่งั้นกราฟจะบีบเวลาให้เพี้ยน
  ผลคือ series **มีความยาวคงที่เสมอ** (12 หรือ 288) ไม่ว่าเครื่องจะเพิ่งบูตหรือเงียบมาทั้งคืน
- **ผลต่อขนาด payload (ยอมรับแล้วโดยเจ้าของโปรเจกต์):** 288 จุด × ~85-95 ไบต์ JSON
  ≈ **24-27 KB ต่อการ refresh 1 ครั้ง** และ API ไม่มี gzip (ดู §1) → หน้าที่เปิดค้างไว้ใช้
  ~2.5 KB/s ≈ ~9 MB/ชม. ต่อ 1 แท็บ ถือว่ารับได้สำหรับ admin UI บน LAN ที่มีผู้ใช้ไม่กี่คน
  (ให้เป็นตัวเลขที่ QA วัดจริงในเกณฑ์ทดสอบรวม §6)

### 2.4 ทางเลือกที่ตัดทิ้ง

- **ใช้ `/api/dashboard/traffic` (`TrafficHistory`, WAN link counters) แทน** — ไม่ต้องแก้แบ็กเอนด์เลย
  แต่คนละแหล่งข้อมูลกับทุกการ์ดในหน้า: นับ traffic ของตัวเราเตอร์เองด้วย, ไม่นับ LAN↔LAN,
  เป็นค่า exact ขณะที่หน้านี้เป็น conntrack estimate → ผู้ใช้เอากราฟไปเทียบกับ Top Hosts
  ในหน้าเดียวกันแล้วตัวเลขไม่ตรง = บั๊กที่อธิบายไม่ได้ **ตัดทิ้ง**
- **endpoint ใหม่ `/api/statistics/bandwidth`** — เพิ่ม auth surface + fetch รอบที่สองที่ต้อง sync
  window/refresh กับของเดิมเอง โดยไม่ได้อะไรเพิ่ม (ข้อมูลมาจาก ring เดียวกัน)
- **ยุบ 24h เป็นรายชั่วโมง 24 จุด** — payload เล็กกว่า ~10 เท่า แต่กลบ burst สั้น ๆ
  (การดาวน์โหลดก้อนใหญ่ 10 นาทีจะถูกเฉลี่ยหายไปในชั่วโมงนั้น) **เจ้าของโปรเจกต์เลือกความละเอียด
  แทนขนาด payload** (มติข้อ 5) — ทางเลือกนี้ถูกตัดอย่างเป็นทางการแล้ว
- **เพิ่มฟิลด์ `observedUp/observedDown` คู่กับ `observed` เดิม** — แหล่งความจริง 2 ที่ที่ drift ได้
  (เหตุผลเดียวกับ §2.3 ของแผน PR #117) → เปลี่ยน type ของฟิลด์เดิมให้ compiler ไล่ call site แทน
- **คำนวณ Top 5 ที่แบ็กเอนด์เป็น endpoint/ฟิลด์ใหม่** — `topSources` มี 10 แถวเรียงแล้ว
  ตัด 5 ที่ frontend พอ ไม่ต้องเพิ่มอะไร

## 3. ขั้นตอนการทำ (inner layer → outer)

### T-01 — DTO ของ API
**ไฟล์:** `backend/internal/model/statistics.go` (~:98-143)
เพิ่ม `BandwidthPoint` (§2.1) + ฟิลด์ `Series []BandwidthPoint \`json:"series"\`` ใน
`TrafficStatistics` พร้อม doc comment ระบุ: LAN-relative, `bytesUp+bytesDown == bytes`,
`Σ series[].bytes == observedBytes`, ความละเอียดคงที่ 5 นาที และความยาวคงที่ (12 / 288)
**acceptance:** ไฟล์นี้แก้เสร็จ, `go build ./...` ยังผ่าน (additive ล้วน)

### T-02 — ยอดรวมต่อ bucket มีมิติทิศทาง 🔒 (แตะ path นับ byte — review เข้ม)
**ไฟล์:** `backend/internal/service/traffic_stats.go`
1. `flowSampleState` (~:81) เพิ่ม `lanFlip bool` — ตั้งค่า **ครั้งเดียว** ตอนสร้าง state ใหม่
   (~:460) ด้วย `isPrivateIP(f.SrcIP)`/`isPrivateIP(f.DstIP)` ตามตาราง §2.2
2. `trafficDetailBucket.observed` (~:94) `uint64` → `dirBytes`
3. `processFlows` (~:427) เปลี่ยน return จาก `uint64` เป็น `dirBytes`: สะสม
   `up += st.lanFlip ? dReply : dOrig` และ down กลับกัน — **ห้ามคำนวณ delta ชุดที่สอง**
   `d := dOrig + dReply` ตัวเดิมยังคงเป็นตัวเดียวที่ไหลเข้า `catDeltas` (invariant ข้อ 5 ของ PR #117)
4. `poll()` (~:393-419): local `var observed uint64` → `dirBytes`, เงื่อนไข quiet-tick
   (~:414) ใช้ `observed.Total() == 0`
5. `onFlowEnd` (~:386): ส่ง `dirBytes` ที่ flip ตาม `st.lanFlip` เช่นกัน
   (**ระวัง:** จุดนี้ `delete(flowState, key)` ไปแล้ว → ต้องอ่าน `lanFlip` ก่อนลบ)
6. `addBucket` (~:717,730,741) รับ `observed dirBytes`, บวกทีละทิศ
7. `GetTrafficDetail` (~:873) และ `GetTrafficBreakdown` (~:959) ใช้ `b.observed.Total()`
   — **response ทั้งสองต้องเหมือนเดิมทุก byte**
> ห้ามแตะ `flowKey`/`flowKeyFromParts`, `mergeDirMap`, ค่าคงที่ cap ใด ๆ

### T-03 — accessor ของ series (ความละเอียด 5 นาทีทั้งสอง window)
**ไฟล์:** `backend/internal/service/traffic_stats.go` (ฟังก์ชันใหม่ ต่อท้าย `GetTrafficBreakdown`)
```go
func (s *TrafficStatsService) GetBandwidthSeries(window string) []model.BandwidthPoint
```
- validate window แบบเดียวกับพี่น้องอีกสองเมธอด (`!= "24h"` → `"1h"`)
- จำนวนจุด: `trafficWindow1hBuckets` (12) เมื่อ 1h, `trafficDetailBucketMax` (288) เมื่อ 24h
  — **ใช้ค่าคงที่เดิม ห้ามฮาร์ดโค้ดเลข** เพื่อให้ series ยาวเท่ากับ window ที่ ring เก็บได้จริงเสมอ
- อ่าน ring ทั้งก้อนใต้ `s.mu.RLock()` **ครั้งเดียว** (คำเตือนยาว ~:821-834) แล้วสร้าง
  `map[ts]dirBytes` จากเฉพาะ bucket ที่อยู่ในช่วง
- **zero-fill:** ไล่ทีละ `trafficDetailBucketSpan` ถอยหลังจาก `time.Now().Truncate(span)`
  ครบจำนวนจุดที่กำหนด แล้ว lookup จาก map (ไม่เจอ = จุดค่า 0 ทั้งสามฟิลด์) → ผลลัพธ์
  **เรียงเก่า→ใหม่เสมอ** และความยาวคงที่
- **ไม่มีการยุบ/เฉลี่ย/downsample ใด ๆ ในเมธอดนี้** (มติข้อ 5) — 1 bucket = 1 จุด
- allocate ด้วย `make([]model.BandwidthPoint, 0, n)` ครั้งเดียว (สร้างทุก 10 วิต่อ client)

**ไฟล์:** `backend/internal/service/statistics.go` (`GetStatistics` ~:214-231)
เพิ่ม `Series: s.traffic.GetBandwidthSeries(window)` ใน struct literal
> **ไม่ต้อง**แก้ `api/handlers.go` / `router.go` — response เป็น additive และไม่มี input ใหม่จาก client

### T-04 — regression test ของ invariant 🔒
**ไฟล์:** `backend/internal/service/traffic_stats_test.go`, `statistics_test.go`
- แก้ call site เดิมของ `addBucket` 2 จุด (`traffic_stats_test.go:562`, `:575`) ให้ส่ง `dirBytes`
- เคสใหม่:
  1. `Σ series[].bytes == observedBytes` ทั้ง 1h และ 24h (property test)
  2. ทุกจุด `bytesUp + bytesDown == bytes`
  3. **ความยาวคงที่**: `len(series) == 12` เมื่อ 1h และ `== 288` เมื่อ 24h **แม้ ring จะว่างเปล่า**
     (ทุกจุดเป็น 0) และ `ts` เรียงจากเก่าไปใหม่ ห่างกันช่องละ 5 นาทีเป๊ะ ไม่มีค่าซ้ำ
  4. **zero-fill กลางช่วง**: ใส่ bucket 2 ก้อนที่ห่างกัน แล้วจุดระหว่างกลางต้องเป็น 0
     (ไม่ใช่หายไปหรือถูกเลื่อนตำแหน่ง)
  5. LAN-relative: flow public→private ต้องนับเป็น **down** ไม่ใช่ up (ล็อกตาราง §2.2)
  6. `GetTrafficDetail`/`GetTrafficBreakdown` คืนค่าเท่าเดิม (regression guard ของ T-02)
- `go test -race ./...` (มี `TestTrafficStats_GetTrafficDetailNoRaceWithPoll` เดิมคุมอยู่)
**acceptance:** `cd backend && go build ./... && go test -race ./...` ผ่านหมด

### T-05 — API contract
**ไฟล์:** `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (ต้องเหมือนกันเป๊ะ)
เพิ่ม schema `BandwidthPoint` และฟิลด์ `series` ใน `TrafficStatistics` (~:4187 `properties` +
`required`) พร้อม description ตาม T-01 และระบุชัดว่า **1h = 12 จุด, 24h = 288 จุด, ช่องละ 5 นาที,
zero-filled, เรียงเก่า→ใหม่**
> ไม่ต้องแก้ `backend/internal/api/dist/openapi.yaml` (build artifact)

### T-06 — frontend API client + mock branch
**ไฟล์:** `frontend/src/services/statisticsService.ts` (types ~:70-98, mock ~:205-242)
เพิ่ม `interface BandwidthPoint` + `series: BandwidthPoint[]` และ mock generator ที่สร้าง
**12 จุด (1h) / 288 จุด (24h)** ย้อนหลังช่องละ 5 นาที ให้ **มีรูปทรง** (เช่น sine + ค่าคงที่ต่อ
index ไม่ใช่แบนราบ และมีช่วงค่า 0 คั่นบ้าง เพื่อทดสอบ zero-fill/แกนเวลา) โดย
`Σ bytes` ของ series **เท่ากับ `observedBytes` ของ mock เป๊ะ** (คำนวณ `observedBytes` จาก series
หรือปรับ scale ให้ลงตัว — อย่าปล่อยให้สองค่านี้ขัดกันในโหมด mock)
ratio up/down ให้สอดคล้องกับ `mockHosts.upRatio` เดิม (download นำ)
> mock 288 จุดต้องเป็น pure function ไม่มี timer/side effect (เหมือน generator อื่นในไฟล์นี้)

### T-07 — คอมโพเนนต์กราฟ (ไฟล์ใหม่)
**ไฟล์ใหม่:** `frontend/src/components/statistics/BandwidthTrendCard.tsx`
ลอกโครง `Dashboard.tsx BandwidthCard` (~:360-439): `useTheme` คุมสี grid/axis,
`ResponsiveContainer` + `LineChart`, เส้น Download = `var(--primary)` / Upload = สี muted,
legend สองจุดบน `CardHeader`, สูง `h-56`
- **ปรับจูนสำหรับ 288 จุด (มติข้อ 5)** — ทั้งหมดเป็นการตั้งค่า recharts ล้วน ไม่ใช่การลดข้อมูล:
  - `dot={false}` + `isAnimationActive={false}` (ของ Dashboard มีอยู่แล้ว — **ห้ามถอดออก**;
    ที่ 288 จุด × 2 เส้น การเปิด animation/dot คือสาเหตุอันดับหนึ่งของอาการหน่วงทุก 10 วิ)
  - `activeDot={{ r: 4, strokeWidth: 0 }}` คงไว้ (วาดเฉพาะจุดที่ hover ไม่กระทบ perf)
  - `XAxis`: กำหนด `interval` ให้แสดง tick ห่าง ๆ (เป้าหมาย ~8-12 label ต่อกราฟ เช่น ทุก 2 ชม.
    เมื่อ 24h = ทุกจุดที่ 24) — ปล่อย auto จะได้ label ทับกันจนอ่านไม่ออกบนการ์ดกว้าง 2/3
  - `type="monotone"` คงไว้ได้; ถ้า QA วัดแล้วพบเฟรมตก ให้ลองเปลี่ยนเป็น `type="linear"` ก่อน
    เป็นอันดับแรก (ถูกกว่า) — **ห้ามแก้ด้วยการลดจำนวนจุดที่ API**
  - memoize ข้อมูลที่แปลงแล้วด้วย `useMemo` โดยผูกกับ `stats.generatedAt`/`window` เพื่อไม่ให้
    map array 288 ช่องใหม่ทุกครั้งที่ React re-render ด้วยเหตุอื่น
  - **ถ้าจำเป็นจริง ๆ** ค่อยเพิ่ม downsample **ในคอมโพเนนต์นี้ที่เดียว** (เช่นรวมทีละ 2 จุด
    เมื่อความกว้างจอ < 640px) และต้องเขียน comment กำกับว่า API ยังส่งความละเอียดเต็ม
- แกน X: label `HH:mm` (แปลงจาก `ts` ด้วย `new Date()` — device local time)
- แกน Y/tooltip: ใช้ `fmtBytes` แบบ adaptive **ห้าม hardcode หน่วย "G"** (bucket 5 นาที
  บนเครือข่ายบ้านมักอยู่ระดับ MB — ของ Dashboard hardcode ไว้เพราะยุบรายชั่วโมง)
  tooltip ต้องบอกด้วยว่าเป็นยอด "ต่อ 5 นาที" ไม่ใช่อัตราเร็ว (bps)
- empty state เมื่อ `series.length === 0` หรือทุกจุดเป็น 0: "กำลังเก็บข้อมูล traffic…"

### T-08 — ค่าคงที่สี chart ที่ใช้ร่วม + การ์ด Top 5 Hosts
**ไฟล์ใหม่:** `frontend/src/lib/chartColors.ts` — ย้าย `CHART_BG_CLASSES`
(`["bg-chart-1"..."bg-chart-5"]`) ออกจาก `Dashboard.tsx:786` มาไว้ที่นี่ แล้ว import กลับใน
`Dashboard.tsx` (แก้บรรทัดเดียว ไม่แตะ logic การ์ดเดิม)
**ไฟล์ใหม่:** `frontend/src/components/statistics/TopHostsShareCard.tsx`
เลียนแบบ `ProtocolBreakdownCard` (`Dashboard.tsx:813-859`) ทุกจุด: stacked bar `h-2.5` +
`ul` legend มีจุดสี `h-2.5 w-2.5 rounded-full` / ชื่อ truncate / `fmtBytes · percent%`
- input = `topSources.slice(0, 5)` — **ไม่กรอง LAN/Internet** (มติข้อ 4) และ **เรียงตาม byte รวม
  ตามที่ API ส่งมา ห้ามจัดอันดับใหม่** (มติข้อ 2)
- แถวใช้ `hostname` (fallback `ip`) + badge LAN/Internet แบบเดียวกับ `HostLabel` เดิม
- **percent ใช้ `TopHost.percent` จาก API ตรง ๆ** (เทียบ `observedBytes` ทั้ง window) —
  **ห้าม normalize ให้ 5 แถวรวมเป็น 100%** (มติข้อ 3) และปิดท้าย stacked bar ด้วย segment
  **"Other"** สี `bg-muted` ความกว้าง `100 - Σ percent` เมื่อผลรวม < 100 พร้อม legend แถวสุดท้าย
  ว่า "อื่น ๆ" (ใช้ `observedBytes - Σ bytes` เป็นตัวเลข)
- บรรทัดรองแสดง `↑up · ↓down` แบบ `UpDownLine` เดิมได้ (ถ้าไม่ทำให้แน่นเกินไป)
**กฎสไตล์บังคับ:** ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามสีดิบ (`text-emerald-500`),
ใช้ `bg-chart-*`/`bg-muted`/`text-primary`/`text-muted-foreground`, ต้องดูดีทั้ง dark/light

### T-09 — วางเลย์เอาต์ในหน้า Overview
**ไฟล์:** `frontend/src/pages/StatisticsOverview.tsx` (~:479-523)
- แทรกกริดใหม่ **เหนือ** กริดเดิม: `grid grid-cols-1 gap-4 lg:grid-cols-3` →
  `<BandwidthTrendCard className="lg:col-span-2" …/>` + `<TopHostsShareCard />` (1 คอลัมน์)
- กริดเดิมของการ์ดที่เหลือคง `lg:grid-cols-2` ไว้เหมือนเดิม
- แก้ skeleton (~:479-491) ให้มีบล็อกกราฟด้วย และปรับ `hasData` (~:432-439) ให้ไม่ซ่อนกราฟทิ้ง
  โดยไม่ตั้งใจ (กราฟมี empty state ของตัวเองแล้ว)
- อัปเดตข้อความอธิบายใต้หัวข้อ (~:451-453) ให้พูดถึงกราฟ/Top 5

### T-10 (optional, ท้ายสุด) — เอกสาร
`docs/ref/complete/statistics-page-plan.md` เพิ่มบรรทัดว่ามีมิติ time-series แล้ว;
ถ้าจำเป็นค่อยแตะ README Feature Status (ปกติไม่ต้อง — Statistics ยัง Completed)

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | พฤติกรรม |
|---|---|---|---|
| GET | `/api/statistics/traffic?window=1h\|24h` | **เส้นเดิม** `authRoute` (ทุก role ที่ล็อกอิน) | เพิ่มฟิลด์ `series[]` (additive, 12 หรือ 288 จุด) — ฟิลด์เดิมทุกตัวความหมายไม่เปลี่ยน |
| GET | `/api/dashboard/traffic-detail?window=…` | **เส้นเดิม** `authRoute` | **ต้องเหมือนเดิมทุก byte** (regression guard ของ T-02) |
| GET | `/api/dashboard/traffic` | เส้นเดิม | ไม่แตะ ไม่ใช้ในแผนนี้ (§2.4) |

ไม่มี endpoint ใหม่ ไม่มี mutation → ต้องใช้งานได้ปกติทั้งใน `-disable-edit=true` และ role
read-only, และไม่มี input ใหม่จาก client เข้าสู่ระบบ

## 5. ข้อควรระวัง

1. 🔒 **T-02 อยู่บน path นับ byte ของ Phase 2** — ถ้า `lanFlip` ถูกคำนวณใหม่ทุก poll แล้ว
   ผลไม่คงที่ (เช่น NAT ทำให้ IP เปลี่ยนมุมมอง) ทิศของ bucket จะสวิงไปมา
   **ป้องกัน:** คำนวณครั้งเดียวตอนสร้าง `flowSampleState` และห้ามแก้ภายหลัง + เทสต์ T-04 ข้อ 5
2. **`onFlowEnd` ลบ key ทิ้งทันที** (invariant ข้อ 2 ของ PR #117) — ถ้าอ่าน `lanFlip` หลัง `delete`
   จะได้ค่า zero-value = ทิศผิดเงียบ ๆ **ป้องกัน:** อ่านค่าเก็บใส่ local ก่อนลบ + เทสต์
3. **series ต้องรวมได้เท่า `observedBytes`** — ถ้า zero-fill พลาดขอบ window (นับเกิน/ขาดไป 1 ช่อง)
   ผู้ใช้จะเห็นกราฟไม่ตรงกับตัวเลขใต้หน้า **ป้องกัน:** เทสต์ T-04 ข้อ 1 เป็น property test
4. **แกนเวลาโกหกได้เมื่อไม่ zero-fill** — ring ไม่สร้าง bucket ในช่วงที่เงียบ (`poll()` ~:414)
   กราฟจะลากเส้นข้ามช่องว่างเหมือนไม่มีอะไรเกิดขึ้น **ป้องกัน:** pad ที่แบ็กเอนด์ (T-03)
5. **หลังบูตใหม่ ring ว่าง** — 24h จะเป็นเส้นศูนย์ยาว 288 จุดเกือบทั้งกราฟ เป็นพฤติกรรมที่ถูกต้อง
   (RAM-only, `tech_stack_design.md` §8) **ป้องกัน:** empty state/คำอธิบายให้ชัด ไม่ใช่ทำให้ดูเหมือน error
6. **concurrent map** — `trafficDetailBucket` มีแต่ map การอ่านต้องอยู่ใต้ `RLock` ตลอดช่วง
   aggregate **ป้องกัน:** `GetBandwidthSeries` ห้าม copy slice ออกมาวนทีหลัง, ต้องรัน `-race`
7. **ห้าม persist / ห้าม migration** — `git diff --stat` ต้องไม่มี `backend/internal/db/`
8. **RAM** — เปลี่ยน `observed` เป็น dirBytes = +8 ไบต์/bucket × 288 ≈ ไม่มีนัยสำคัญ
9. **frontend mock branch ต้องอัปเดตพร้อม type** — ลืมแล้ว `yarn build` (tsc) พัง หรือหน้ากราฟ
   ว่างในโหมด mock (Caution เดิมข้อ 11 ของแผน PR #117)
10. **การย้าย `CHART_BG_CLASSES`** (T-08) แตะ `Dashboard.tsx` ด้วย — ห้ามเปลี่ยนค่า/ลำดับสี
    ไม่งั้นการ์ด Protocol Breakdown เปลี่ยนสีโดยไม่ได้ตั้งใจ
11. **`lg:col-span-2` ต้องอยู่บนตัว `<Card>`** ไม่ใช่ wrapper `<div>` (ตามแพตเทิร์น
    `Dashboard.tsx:369`) ไม่งั้น grid item ซ้อนกันแล้วความสูงเพี้ยน
12. **payload 24-27 KB ทุก 10 วินาที (มติข้อ 5)** — ไม่มี gzip ที่ API (§1) และหน้านี้ poll ทุก
    10 วิแม้แท็บจะอยู่เบื้องหลัง **ผลกระทบที่ต้องเฝ้า:** แท็บที่เปิดค้างหลายวันบน Wi-Fi อ่อน ๆ
    หรือหลายผู้ใช้พร้อมกันจะเห็น traffic ของตัว PiGate เองโผล่ในสถิติของตัวเอง (เล็กน้อยแต่มีจริง)
    **ป้องกัน/ทางออกถ้าเป็นปัญหา:** วัดจริงในเกณฑ์ §6 ก่อน; ถ้าหนักไปค่อยเสนอเจ้าของโปรเจกต์
    ทีหลังเป็นงานแยก (gzip middleware หรือ `series` แบบ opt-in ด้วย query param) —
    **ห้ามแก้ด้วยการลดความละเอียดเองโดยพลการ** เพราะขัดมติข้อ 5
13. **recharts กับ 288 จุด** — เปิด animation/dot หรือ re-render array ใหม่ทุกครั้ง จะกระตุกทุกรอบ
    refresh บนเครื่องช้า **ป้องกัน:** ตั้งค่าตาม T-07 (`dot={false}`, `isAnimationActive={false}`,
    `XAxis interval`, `useMemo`) และถ้ายังไม่พอ ให้ downsample **ที่คอมโพเนนต์เท่านั้น**
14. **ทดสอบบนบอร์ดจริงปลอดภัย** — แผนนี้อ่านอย่างเดียว ไม่แตะ firewall/routing/interfaces
    ไม่มีความเสี่ยงล็อกตัวเองออกจากเครื่อง

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [ ] T-01 `model/statistics.go`: `BandwidthPoint` + `TrafficStatistics.Series`
- [ ] T-02 `service/traffic_stats.go`: `lanFlip`, `bucket.observed → dirBytes`, `processFlows`,
      `poll`, `onFlowEnd`, `addBucket`, `GetTrafficDetail`/`GetTrafficBreakdown` คงเดิม 🔒
- [ ] T-03 `GetBandwidthSeries` (12/288 จุด, zero-fill, ไม่ downsample) + wire ใน `GetStatistics`
- [ ] T-04 เทสต์ 6 เคส + แก้ call site `addBucket` 2 จุด; `go build ./... && go test -race ./...`

**Docs / Frontend**
- [ ] T-05 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (diff ตรงกันเป๊ะ)
- [ ] T-06 `services/statisticsService.ts` (type + mock series 12/288 จุด)
- [ ] T-07 `components/statistics/BandwidthTrendCard.tsx` (ไฟล์ใหม่, ปรับจูน 288 จุด)
- [ ] T-08 `lib/chartColors.ts` + `components/statistics/TopHostsShareCard.tsx` (ไฟล์ใหม่)
- [ ] T-09 `pages/StatisticsOverview.tsx` เลย์เอาต์ 2/3 + 1/3
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] T-10 (optional) เอกสาร

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ — skill `verify`)**
- [ ] `-mock=true`: `/statistics/overview` แสดงแถวบน = กราฟ 2/3 + การ์ด Top 5 1/3 ในแถวเดียวกัน
      บนจอกว้าง (≥1024px) และ stack เป็นคอลัมน์เดียวที่ ~768px โดยไม่มี overflow แนวนอน
- [ ] กราฟมีทั้งเส้น Download/Upload ที่ **ไม่ทับกันเป๊ะ**, แกน X เรียงเวลาถูกต้องและ label
      ไม่ทับกัน (~8-12 label), tooltip แสดงหน่วยที่อ่านได้ (KB/MB/GB ตามขนาด ไม่ใช่ "0.00G" ทุกจุด)
- [ ] สลับ window 1h ↔ 24h: **จำนวนจุด 12 ↔ 288** (นับจาก JSON) และยอดรวม 24h ≥ 1h เสมอ
- [ ] ตรวจ JSON ดิบด้วย `curl`: `Σ series[].bytes == observedBytes`, ทุกจุด
      `bytesUp + bytesDown == bytes`, `ts` ห่างกันช่องละ 5 นาทีเป๊ะไม่มีช่องหาย/ซ้ำ
- [ ] **วัดขนาด/ความลื่นของ 24h (มติข้อ 5):** `curl -s … | wc -c` ของ window 24h ≈ 24-27 KB
      และเปิดหน้าค้างที่ 24h แล้วสลับแท็บ/scroll ต้องไม่กระตุกชัดเจนบนเครื่องทดสอบ
      (ถ้ากระตุก ให้แก้ตาม §5 ข้อ 13 เท่านั้น — ห้ามลดความละเอียดที่ API)
- [ ] การ์ด Top 5: มี ≤5 แถว เรียงตาม byte รวมตรงกับ 5 แถวแรกของ Top Source Hosts เป๊ะ,
      **ไม่กรอง LAN/Internet** (badge ยังอยู่), percent ตรงกับการ์ด Top Source Hosts ทุกแถว
      (ไม่ถูก normalize), มี segment "Other" สีเทาปิดท้ายเมื่อผลรวม < 100%
- [ ] หน้าตา/สัดส่วน/legend ของการ์ด Top 5 ตรงกับ Protocol Breakdown ของ Dashboard
- [ ] dark/light สลับแล้วอ่านออกทั้งคู่, ไม่มี `shadow-*`/`backdrop-blur-*`/สีดิบใน diff
- [ ] Dashboard (แท็บ Overview + Detailed) ไม่มี regression — Protocol Breakdown สีเดิม,
      `/api/dashboard/traffic-detail` JSON เหมือนก่อนแก้ทุกฟิลด์
- [ ] real device: ดาวน์โหลดไฟล์ใหญ่ → กราฟขึ้นยอดใน bucket 5 นาทีนั้นอย่างคมชัด (เห็นเป็น spike
      ไม่ถูกเฉลี่ยหาย) โดย **Download ≫ Upload** และยอดรวมไม่โตเป็น 2 เท่าของขนาดไฟล์
- [ ] real device: ปล่อยรัน ≥1 ชม. → RSS ไม่โตต่อเนื่อง, กราฟ 24h มี 288 จุดต่อเนื่องไม่มีช่องหาย
- [ ] `-disable-edit=true` + role read-only ยังเปิดหน้านี้ได้ครบ; logout แล้วเรียก
      `/api/statistics/traffic` ตรง ๆ → 401
- [ ] `go test -race ./...` และ `yarn build && yarn lint` ผ่านทั้งคู่
- [ ] ทุกอย่างอยู่บน branch `feat/statistics-overview-charts` และเข้า main ผ่าน PR เท่านั้น

## 7. มติที่เจ้าของโปรเจกต์ตัดสินแล้ว (lock 2026-08-01 — ห้ามเปลี่ยนเองระหว่างทำ)

| # | ประเด็น | มติ | ผลต่อแผน |
|---|---|---|---|
| 1 | กราฟแยก Upload/Download หรือเส้นเดียว | **แยก 2 เส้น** (LAN-relative ตาม §2.2) | T-02 อยู่ครบตามเดิม 🔒 |
| 2 | เกณฑ์จัดอันดับ Top 5 Hosts | **byte รวม (up+down)** เท่ากับ Top Source Hosts | T-08 ใช้ `topSources` ตามลำดับที่ API ส่งมา ห้ามเรียงใหม่ |
| 3 | ตัวหารของ percent ในการ์ด Top 5 | **เทียบ `observedBytes` ทั้ง window + segment "Other"** ห้าม normalize เป็น 100% | T-08 ใช้ `TopHost.percent` ตรง ๆ + แถบ `bg-muted` ส่วนที่เหลือ |
| 4 | กรองเฉพาะเครื่องใน LAN ไหม | **ไม่กรอง** — แสดงทั้ง LAN และ Internet พร้อม badge เดิม | T-08 ไม่มี filter |
| 5 | ความละเอียดของ window 24h | **bucket ดิบ 5 นาที 288 จุด** (เลือกความละเอียดแทนขนาด payload) | §2.3, T-03, T-04 ข้อ 3, T-06, T-07 (ปรับจูน recharts), §5 ข้อ 12-13, เกณฑ์วัดขนาด/ความลื่นใน §6 |

> ถ้าระหว่างทำพบว่ามติข้อ 5 ทำให้หน้ากระตุกจนใช้งานไม่ได้จริง **ห้ามลดความละเอียดที่ API เอง** —
> ให้แก้ตาม §5 ข้อ 13 (ปรับ recharts / downsample ในคอมโพเนนต์) ก่อน แล้วรายงานตัวเลขที่วัดได้
> กลับไปให้เจ้าของโปรเจกต์ตัดสินใจอีกครั้ง
