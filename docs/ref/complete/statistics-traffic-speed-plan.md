# Statistics → Traffic: เพิ่ม "ความเร็ว" (rate / throughput) ต่อยอดจาก PR 121

> เอกสารแผนงานสำหรับฟีเจอร์: เพิ่มมุมมอง **ความเร็ว** ให้หน้า Statistics → Traffic
> 2 ส่วน คือ (A) ปุ่มสลับหน่วยบนกราฟ Bandwidth เดิม bytes ↔ speed และ
> (C) **ความเร็วปัจจุบันต่อ IP** (คอลัมน์ในตาราง Top hosts + การ์ดในหน้า drill-down)
>
> วันที่เขียน: 2026-08-03 · แก้ไขครั้งที่ 1: 2026-08-03 (เจ้าของตอบ D-1/D-2/D-3 แล้ว — ล็อกขอบเขต)
> แก้ไขครั้งที่ 2: 2026-08-03 (ai-developer ทำครบ + ai-qa ตรวจผ่าน — บันทึกสถานะปิดงาน)
> อ้างอิงโค้ด: `main` @ `12a61bc` (หลัง merge PR 121) · Branch: `feat/statistics-traffic-speed`
>
> **สถานะ: โค้ดครบทุก Task (T-01…T-10) และ ai-qa ตรวจ PASS แล้ว ณ 2026-08-03**
> build / vet / `go test ./... -race` / lint / `build.sh` เขียวทั้งหมด, openapi สองไฟล์ diff เท่ากันเป๊ะ,
> manual E2E บน mock server ผ่าน, ไม่พบบั๊ก severity สูง/กลาง, ไม่มี loop แก้
> มี known-limitation 1 ข้อที่ **ไม่บล็อก** (ดู §7)
> **เหลือเฉพาะ: เจ้าของโปรเจกต์ตรวจบนเบราว์เซอร์จริง แล้วสั่ง commit + เปิด PR**
> (ai-tech-lead/ai-developer/ai-qa ไม่ commit เอง ตามข้อตกลงของโปรเจกต์)

## 0. การตัดสินใจของเจ้าของโปรเจกต์ (ล็อกแล้ว ห้ามเปิดใหม่)

| # | คำถาม | คำตอบที่ล็อก |
|---|---|---|
| D-1 | ขอบเขต | **A + C** — rate ย้อนหลังบนกราฟเดิม + ความเร็วสดต่อ IP |
| D-2 | หน่วย | **Mbps (bit/วินาที, ฐาน 1000)** ทุกจุดที่แสดงความเร็ว ห้ามผสมหน่วย byte |
| D-3 | UX กราฟเดิม | **ปุ่ม toggle สลับ bytes ↔ speed** โดย **default = bytes เหมือนเดิมทุกประการ** |

**นอกขอบเขต (ระบุชัด — ห้าม ai-developer ทำ)**

- **ไม่ทำแนวทาง B** (ความเร็วสดระดับ WAN จาก netlink counters / `SystemMetrics` / SSE) —
  เจ้าของไม่เลือก ห้ามแตะ `service/system_status.go`, `model/types.go`, `dashboardService.ts`,
  หน้า Dashboard และ SSE metrics stream แม้แต่บรรทัดเดียวในแผนนี้
- ไม่แตะ kernel layer (`kernel/interfaces.go`, `real_*.go`, `mock.go`) — ข้อมูลที่ต้องใช้มีครบแล้ว
- ไม่เพิ่ม endpoint/route/query param ใหม่ ไม่มี migration ไม่เพิ่ม config key ใหม่
- ไม่เปลี่ยนรูปร่าง/ค่าของ response `/api/statistics/traffic` (หน้า Overview) และไม่แตะหน้า Overview
- ไม่เพิ่มความถี่ poll ของ conntrack (คงที่ 10 วิ — ดู §6 ข้อ 8)
- ไม่ทำ "พีคของช่วง" (peak Mbps ต่อ bucket) — เก็บไว้เป็นงานอนาคต

## 1. สถานะปัจจุบัน (สำรวจ ณ วันเขียนแผน — ก่อนลงมือ)

| ส่วน | สถานะปัจจุบัน | ไฟล์:บรรทัด (~) |
|---|---|---|
| แหล่งข้อมูลกราฟหน้า Traffic | conntrack `DumpFlows` poll ทุก **10 วิ** สะสม delta ลง bucket ring **5 นาที** × 288 (24 ชม.) ใน RAM | `backend/internal/service/traffic_stats.go:27-30, 481-508` |
| หน่วยของกราฟตอนนี้ | **bytes สะสมต่อบัคเก็ต 5 นาที** — subtitle เขียนไว้ตรงๆ ว่า "ยอดต่อ 5 นาที (ไม่ใช่ความเร็ว)" | `frontend/src/components/statistics/BandwidthTrendCard.tsx:85, 134` |
| series network-wide (LAN-relative) | มีแล้ว `TrafficBreakdown.Series` คำนวณในลูป/RLock เดียวกับ `Observed` | `traffic_stats.go:1075-1090, 1150-1169` |
| series ต่อ IP (flow-relative) | มีแล้ว `TrafficBreakdown.HostSeries` จาก `b.convBytes` | `traffic_stats.go:1171-1199` |
| DTO ที่ส่งออก | `model.BandwidthPoint{ts,bytes,bytesUp,bytesDown}` — **ไม่มีฟิลด์ rate/bps ใดๆ** | `backend/internal/model/statistics.go:101-110` |
| delta ต่อ poll (วัตถุดิบของ "ความเร็วสด") | คำนวณอยู่แล้วทุก 10 วิ (`hostDeltas/dstDeltas/convDeltas`) แต่ **ถูกโยนเข้า bucket แล้วทิ้ง** ไม่มีใครเก็บไว้ | `traffic_stats.go:481-508, 819-856` |
| delta จาก flow-end event | `onFlowEnd` บวกเข้า bucket นอกจังหวะ tick ได้ | `traffic_stats.go:436-475` |
| ความถี่ refresh ของหน้า | list และ drill-down ยิง API ใหม่ทุก **10 วิ** อยู่แล้ว (มี `setInterval` แล้ว) | `pages/StatisticsTraffic.tsx:30,169`, `pages/StatisticsTrafficHost.tsx:36,278` |
| `TotalBytes/Up/Down` ของ drill-down | คำนวณจาก `breakdown.Convs` นับทั้งฝั่ง src และ dst, แถว `src==dst` นับสองครั้ง (ไม่ใช่ else-if) | `backend/internal/service/statistics_traffic.go:139-177` |
| formatter ฝั่ง frontend | มีแค่ `fmtBytes` (ฐาน 1024) — **ยังไม่มี formatter หน่วยความเร็ว** | `frontend/src/lib/formatBytes.ts:8-14` |
| kernel layer | `DumpFlows()` มีครบทั้ง real และ mock — **ไม่ต้องแก้อะไร** | `backend/internal/kernel/interfaces.go` |

**สรุปหนึ่งบรรทัด:** A เป็นงาน frontend ล้วน (คณิตศาสตร์บน series เดิม) ส่วน C คือ "เก็บ delta
ของ poll รอบล่าสุดที่ทุกวันนี้คำนวณแล้วทิ้ง" ไว้เป็น snapshot แล้วหารด้วยเวลาจริง

**ข้อจำกัดที่ต้องยอมรับ:** bucket ring เก็บแค่ผลรวมต่อ 5 นาที ไม่มี timestamp ราย sample →
ความละเอียดย้อนหลังสูงสุด = 5 นาที ส่วน "ความเร็วตอนนี้" = ค่าเฉลี่ยของช่วง poll ล่าสุด (~10 วิ)
ไม่ใช่ค่า ณ วินาทีนี้จริงๆ — ต้องเขียนกำกับใน UI ทุกจุด

## 2. แนวทางเทคนิค

### 2.1 A — rate จาก series เดิม (frontend ล้วน ไม่มี API เปลี่ยน)

```
rate_bps(point) = point.bytes * 8 / span_seconds
span_seconds    = 300                                   // ทุกจุด ยกเว้นจุดสุดท้าย
span_last       = clamp(now_sec - ts_last_sec, 30, 300) // บัคเก็ตที่ยังเปิดอยู่
```

เหตุผลของ `span_last`: จุดสุดท้ายของ series คือบัคเก็ตที่ยังสะสมไม่ครบ 5 นาที ถ้าหารด้วย 300
กราฟความเร็วจะร่วงที่จุดสุดท้าย **ทุกครั้งที่รีเฟรช** (ผู้ใช้จะเข้าใจผิดว่าเน็ตตก) ส่วน `clamp`
ขั้นต่ำ 30 วิ กันกรณีเพิ่งข้ามขอบบัคเก็ตแล้วหารด้วยเลขน้อยจนความเร็วพุ่งเป็นค่าเวอร์

ค่า `bytesUp/bytesDown` ของแต่ละจุดใช้สูตรเดียวกัน (คูณ 8 หารด้วย span เดียวกัน) — เส้น
Download/Upload ในโหมด speed ต้องมาจากจุดเดียวกับโหมด bytes เสมอ ห้ามเปลี่ยนที่มาของข้อมูล

### 2.2 C — ความเร็วปัจจุบันต่อ IP (service + model + frontend)

**หลักการ: rate accumulator ที่หมุนทุก poll tick**

1. `TrafficStatsService` เพิ่ม accumulator 3 map (by-src, by-dst, by-conv) ชนิด `dirBytes`
   เหมือนที่ bucket ใช้
2. **ทั้ง `poll()` และ `onFlowEnd()`** ต้องบวก delta ชุดเดียวกับที่ส่งเข้า `addBucket` เข้า
   accumulator ด้วย — ถ้าบวกเฉพาะ `poll()` flow ที่เกิดและตายระหว่าง tick จะหายจากความเร็ว
   **ห้ามคำนวณ delta รอบสอง** เด็ดขาด — invariant เดิมของไฟล์นี้คือมี delta ชุดเดียวต่อ flow
   ต่อ tick (`traffic_stats.go` Caution 1/2)
3. ทุก tick ของ `poll()` ให้ **หมุน** accumulator → `lastRate` พร้อมบันทึก `elapsed` จริง
   (`now - lastRotateAt`, ไม่ hardcode 10 วิ) และเวลา `at` แล้วเคลียร์ accumulator
4. `rate_bps = delta_bytes * 8 / elapsed_seconds`
5. **การหมุนต้องเกิดก่อน early-return ของ `poll()`** (`traffic_stats.go:502-506` — บรรทัดที่
   `return` เมื่อ tick นั้นเงียบสนิท) ไม่งั้นตอนไม่มีทราฟฟิก ความเร็วจะ **ค้างค่าเดิมตลอดไป**
   แทนที่จะกลับเป็น 0
6. หน่วยความจำเพิ่ม = ขนาดเท่า 1 บัคเก็ต คุมด้วย cap เดิม (`traffic-stats-max-hosts/-dests/
   -conversations`) → ไม่ต้องเพิ่ม config key ใหม่ และไม่เขียนลง SQLite (RAM-only เหมือนเดิม)

**ทำไมต้องมี map by-conv ด้วย ทั้งที่มี by-src/by-dst แล้ว**

หน้า drill-down คิด `TotalBytes` จาก `breakdown.Convs` ด้วยกติกา "นับทั้งฝั่ง src และ dst,
แถว `src==dst` นับสองครั้ง" (`statistics_traffic.go:139-177`) ถ้าความเร็วของ IP นั้นคำนวณจาก
by-src/by-dst จะได้คนละกติกา → ตัวเลข "ความเร็ว" กับ "ยอดรวม" บนหน้าเดียวกันจะขัดกันเอง
ใช้ by-conv จึงสอดคล้องกันโดยโครงสร้าง (by construction) ไม่ใช่โดยบังเอิญ

**สัญญาของฟิลด์ใหม่** — additive + `omitempty` ทุกตัว เพื่อให้ payload ของ
`/api/statistics/traffic` (หน้า Overview ใช้ `TopHost` ตัวเดียวกัน) ไม่เปลี่ยนรูปแม้แต่ไบต์เดียว
เมื่อไม่ได้เติมค่า (ข้อผูกพัน §1.7 ของแผน `statistics-traffic-page-plan.md`)

## 3. Task list (ทำครบทุก Task แล้ว — ai-qa ตรวจ PASS)

> Branch: `feat/statistics-traffic-speed` (โค้ดทุกอย่างต้องอยู่บน feature branch แล้วเข้า PR
> เท่านั้น ห้าม push main, ห้าม commit เว้นแต่เจ้าของสั่ง)
> **ห้ามทดสอบทีละ Task** — ทำให้ครบ T-01…T-10 ก่อน แล้วค่อยทดสอบรวมทีเดียวตาม §5

| Task | สถานะ |
|---|---|
| T-01 … T-10 | **DONE ทั้งหมด** (ai-developer) · **QA PASS** (ai-qa, ไม่มี loop แก้) |

```json
[
  {
    "task_id": "T-01",
    "title": "เพิ่ม formatter ความเร็ว (Mbps) ที่ frontend ใช้ร่วมกัน",
    "layer": "frontend",
    "files": ["frontend/src/lib/formatBytes.ts"],
    "instruction": "เพิ่ม export function fmtRate(bitsPerSec: number): string ในไฟล์เดิม โดย (1) ห้ามแก้ fmtBytes เดิมแม้แต่ตัวอักษรเดียว (มีคอมเมนต์กำกับไว้ว่าเป็น pure move ห้ามเปลี่ยนสูตร) (2) หน่วยเป็น bit/วินาที ฐาน 1000 (ไม่ใช่ 1024): bps, Kbps, Mbps, Gbps ตามมาตรฐานเครือข่าย (3) ทศนิยม 1 ตำแหน่งเมื่อค่า < 100 และ 0 ตำแหน่งเมื่อ >= 100 หรืออยู่หน่วย bps (4) ค่า <= 0 หรือ NaN คืน '0 bps' (5) ใส่ doc comment อ้างอิง docs/ref/todo/statistics-traffic-speed-plan.md T-01 และระบุว่าอินพุตเป็น bit/วินาทีเสมอ (backend ส่ง bps มาให้แล้ว ห้ามให้ผู้เรียกคูณ 8 เอง)",
    "acceptance": [
      "yarn build (tsc -b) ผ่าน",
      "fmtBytes เดิมไม่ถูกแก้",
      "fmtRate(1_500_000) === '1.5 Mbps', fmtRate(0) === '0 bps', fmtRate(999) === '999 bps'"
    ],
    "depends_on": [],
    "status": "done"
  },
  {
    "task_id": "T-02",
    "title": "BandwidthTrendCard: ปุ่ม toggle สลับ bytes ↔ speed (default = bytes)",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/BandwidthTrendCard.tsx"],
    "instruction": "เพิ่ม state ภายในการ์ด mode: 'bytes' | 'speed' โดย default = 'bytes' และในโหมด bytes ต้อง render เหมือนเดิมทุกพิกเซล (ห้าม regression กับหน้า Overview / Traffic list / drill-down ที่ใช้การ์ดนี้ร่วมกัน). ปุ่มสลับต้องใช้คอมโพเนนต์จาก @/components/ui เท่านั้น (เช่น ToggleGroup หรือ Button ขนาดเล็ก) ห้ามสร้าง UI เอง ตาม docs/rules_of_work.md ข้อ 1.1 และวางไว้แถวหัวการ์ดคู่กับ legend Download/Upload. โหมด speed: แปลงทุกจุดเป็น bit/วินาที ตามสูตรใน docs/ref/todo/statistics-traffic-speed-plan.md §2.1 คือ bytes*8/span โดย span = 300 วินาที ยกเว้นจุดสุดท้ายใช้ clamp(now - ts_last, 30, 300) — แปลงทั้ง download (bytesDown), upload (bytesUp) และ total ด้วย span เดียวกัน. YAxis tickFormatter และ Tooltip formatter ในโหมด speed ใช้ fmtRate (ข้อความ tooltip ต้องไม่มีคำว่า '/ 5 นาที' ในโหมดนี้). subtitle: โหมด bytes ใช้ข้อความเดิม/prop subtitle เดิมทุกประการ ส่วนโหมด speed ให้แสดงข้อความที่ระบุชัดว่าเป็นค่าเฉลี่ยต่อช่วง 5 นาที ไม่ใช่ค่าพีค และจุดล่าสุดยังเก็บไม่ครบช่วง แต่ยังต้องคง prop subtitle ที่ผู้เรียก (หน้า per-IP) ส่งมาให้เห็นในโหมด bytes เหมือนเดิม. เงื่อนไข hasSignal / empty state เดิมต้องทำงานเหมือนเดิมทั้งสองโหมด. ห้ามใช้คลาสสีดิบของ Tailwind ห้าม shadow-* / backdrop-blur-* ต้องรองรับ dark/light",
    "acceptance": [
      "yarn build และ yarn lint ผ่าน ไม่มี warning ใหม่",
      "โหมด bytes ให้ผลเหมือนก่อนแก้ทุกประการ (ค่า, ข้อความ, สี, layout)",
      "โหมด speed แสดงหน่วย Mbps/Kbps ผ่าน fmtRate ทั้งแกน Y และ tooltip",
      "ปุ่ม toggle มาจาก components/ui ไม่ใช่ element ที่สร้างเอง"
    ],
    "depends_on": ["T-01"],
    "status": "done"
  },
  {
    "task_id": "T-03",
    "title": "service: rate accumulator + snapshot ความเร็วรอบล่าสุด",
    "layer": "service",
    "files": ["backend/internal/service/traffic_stats.go"],
    "instruction": "เพิ่มใน TrafficStatsService: rateMu sync.RWMutex, rateAcc (3 map: byHost, byDst, byConv ชนิด map[string]dirBytes — key รูปแบบเดียวกับ bucket ทุกประการ รวมถึง convKey), lastRate (3 map เดียวกัน), lastRateElapsed time.Duration, lastRateAt time.Time, lastRotateAt time.Time. (1) poll(): หลังคำนวณ hostDeltas/dstDeltas/convDeltas แล้ว ให้บวก delta ชุดเดิมนั้น (ห้ามคำนวณ delta ใหม่รอบสอง — invariant Caution 1/2 ของไฟล์นี้) เข้า rateAcc ภายใต้ rateMu โดยใช้กติกา cap เดียวกับ mergeDirMap (s.maxTrackedHosts / maxTrackedDests / maxTrackedConversations) (2) onFlowEnd(): บวก delta ของมันเข้า rateAcc ด้วยเช่นกัน มิฉะนั้น flow ที่เกิด-ตายระหว่าง tick จะหายจากความเร็ว (3) ทุก tick ของ poll() ให้หมุน rateAcc -> lastRate พร้อมบันทึก elapsed = now - lastRotateAt (เวลาจริง ห้าม hardcode flowPollInterval) แล้วเคลียร์ rateAcc — การหมุนนี้ต้องเกิดก่อน early-return กรณี tick เงียบ (บรรทัดราว 502-506) เพื่อให้ความเร็วตกกลับเป็น 0 ได้จริง ห้ามค้างค่าเดิม (4) เพิ่ม exported method CurrentRates() ที่คืน snapshot ของ lastRate เป็น map ที่ 'คัดลอกใหม่' ทั้ง 3 ชุด พร้อมเวลา at — ห้ามคืน map ภายในตรงๆ เพราะ poller เขียนทับตลอดเวลา (บั๊กประเภทเดียวกับที่คอมเมนต์ยาวเหนือ GetTrafficDetail เตือนไว้) และให้แปลงเป็น bit/วินาทีให้เรียบร้อยที่ชั้นนี้ (bytes*8/elapsed_seconds, กรณี elapsed <= 0 ให้คืนค่า 0 ทั้งหมดและ at เป็น zero time) (5) เขียน doc comment อ้าง docs/ref/todo/statistics-traffic-speed-plan.md §2.2 อธิบายว่าเป็น RAM-only ไม่เขียน SQLite และเป็นค่าเฉลี่ยของช่วง poll ล่าสุด ไม่ใช่ค่า ณ วินาทีนี้ (6) ห้ามเปลี่ยน flowPollInterval, ห้ามแตะ GetTrafficDetail/GetTrafficBreakdown/getTrafficBreakdown, ห้ามเรียก kernel เพิ่ม",
    "acceptance": [
      "cd backend && go build ./... && go vet ./... ผ่าน",
      "go test ./internal/service/... -race ผ่าน (เทสต์เดิมทั้งหมดยังเขียว)",
      "ไม่มี kernel call ใหม่ และไม่มีการเขียน SQLite เพิ่ม",
      "CurrentRates() คืน map ที่คัดลอกใหม่ ไม่ใช่ reference ของ state ภายใน"
    ],
    "depends_on": [],
    "status": "done"
  },
  {
    "task_id": "T-04",
    "title": "model: ฟิลด์ความเร็วแบบ additive (omitempty)",
    "layer": "model",
    "files": ["backend/internal/model/statistics.go"],
    "instruction": "เพิ่มฟิลด์ใหม่ทั้งหมดเป็น additive + json omitempty ห้ามแก้/ห้ามย้ายฟิลด์เดิม: (1) TopHost: RateBpsUp uint64 json:\"rateBpsUp,omitempty\", RateBpsDown uint64 json:\"rateBpsDown,omitempty\" (2) TrafficTopHosts: RateSampledAt string json:\"rateSampledAt,omitempty\" (3) TrafficHostDetail: CurrentRateBpsUp, CurrentRateBpsDown uint64 (omitempty) และ RateSampledAt string (omitempty). doc comment ของทุกฟิลด์ต้องระบุ 4 เรื่อง: หน่วยเป็น bit/วินาที (bps) เสมอ, เป็นค่าเฉลี่ยของช่วง poll ล่าสุด ~10 วินาที ไม่ใช่ค่า ณ วินาทีนี้, มาจาก conntrack จึงมีความแม่นระดับเดียวกับฟิลด์ Accuracy ของ struct เดียวกัน, ทิศทาง up/down เป็น flow-relative แบบเดียวกับ BytesUp/BytesDown ของ struct นั้น (ไม่ใช่ LAN-relative แบบ BandwidthPoint). ระบุด้วยว่า omitempty มีไว้เพื่อให้ response /api/statistics/traffic (หน้า Overview ซึ่งใช้ TopHost ตัวเดียวกันแต่ไม่เติมค่า) ไม่เปลี่ยนรูปร่าง",
    "acceptance": [
      "go build ./... ผ่าน",
      "ฟิลด์เดิมทุกตัวไม่ถูกแก้ชื่อ/ชนิด/tag",
      "ฟิลด์ใหม่ทุกตัวมี omitempty"
    ],
    "depends_on": [],
    "status": "done"
  },
  {
    "task_id": "T-05",
    "title": "service: เติมค่าความเร็วลง 2 endpoint ของหน้า Traffic",
    "layer": "service",
    "files": ["backend/internal/service/statistics_traffic.go"],
    "instruction": "(1) GetTrafficTopHosts: เรียก s.traffic.CurrentRates() เพียงครั้งเดียวต่อ request แล้วเติม RateBpsUp/RateBpsDown ให้แถว Sources จาก map by-src และแถว Destinations จาก map by-dst (คีย์คือ IP ของแถวนั้น) พร้อมตั้ง RateSampledAt จาก at ที่ snapshot คืนมา (format RFC3339 UTC เหมือน GeneratedAt) (2) GetTrafficHostDetail: คำนวณ CurrentRateBpsUp/Down ของ IP ที่กำลังดู จาก map by-conv โดยใช้ parseConvKey และกติกาเดียวกับที่คำนวณ TotalBytes ทุกประการ คือบวกเมื่อ srcIP == ip และบวกอีกครั้งเมื่อ dstIP == ip (ห้ามเปลี่ยนเป็น else-if — แถว src==dst ต้องนับสองครั้งให้ตรงกับ TotalBytes) โดย Up = Orig, Down = Reply เหมือนแถวในตาราง (3) กรณียังไม่เคยหมุน rate เลย (เพิ่งบูต) หรือ at เป็น zero time ให้ค่าความเร็วเป็น 0 และ RateSampledAt เป็นสตริงว่าง (4) ห้ามเรียก GetTrafficBreakdown/GetTrafficBreakdownForIP เพิ่มรอบที่สอง ห้ามเพิ่มล็อกของตัวเอง ห้ามเรียก kernel (5) ห้ามแตะ GetStatistics หรือเส้นทางของหน้า Overview",
    "acceptance": [
      "go build ./... && go vet ./... ผ่าน",
      "ไม่มีการเรียก breakdown หรือ CurrentRates ซ้ำรอบสองใน request เดียว",
      "ค่าความเร็วเป็น 0 และ rateSampledAt ว่าง เมื่อยังไม่มี sample",
      "โค้ดที่คำนวณ TotalBytes เดิมไม่ถูกแก้"
    ],
    "depends_on": ["T-03", "T-04"],
    "status": "done"
  },
  {
    "task_id": "T-06",
    "title": "test: ครอบ rate accumulator และการเติมค่าในสอง endpoint",
    "layer": "service",
    "files": [
      "backend/internal/service/traffic_stats_test.go",
      "backend/internal/service/statistics_traffic_test.go"
    ],
    "instruction": "เพิ่มเทสต์ใหม่ (ห้ามแก้ความหมายของเทสต์เดิม): (1) หมุน accumulator แล้ว rate ที่ได้ ~= delta_bytes*8/elapsed (ออกแบบให้ควบคุม/inject เวลาได้ หรือเช็คเป็นช่วงค่าแทนค่าเป๊ะ) (2) tick ถัดไปที่ไม่มีทราฟฟิกเลย ทำให้ความเร็วกลับเป็น 0 ไม่ค้างค่าเดิม (เคสสำคัญที่สุดของแผนนี้) (3) delta ที่มาจาก onFlowEnd ถูกนับรวมในความเร็วด้วย (4) ความเร็วต่อ IP ของ drill-down ใช้กติกาเดียวกับ TotalBytes รวมกรณี src==dst ที่ต้องนับสองครั้ง (5) เทสต์ -race ที่อ่าน CurrentRates() ขนานกับ poll() แบบเดียวกับ TestTrafficStats_GetTrafficDetailNoRaceWithPoll (6) marshal response ของหน้า Overview (/api/statistics/traffic) เป็น JSON แล้วยืนยันว่าไม่มีคีย์ rateBpsUp/rateBpsDown/rateSampledAt โผล่ (omitempty ทำงานจริง)",
    "acceptance": [
      "go test ./... -race ผ่านทั้ง repo ไม่มี race report",
      "เทสต์เดิมไม่ถูกแก้ความหมาย",
      "มีเทสต์ครบทั้ง 6 ข้อในคำสั่ง"
    ],
    "depends_on": ["T-05"],
    "status": "done"
  },
  {
    "task_id": "T-07",
    "title": "openapi: บันทึกฟิลด์ใหม่ในสองไฟล์ให้ตรงกันเป๊ะ",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "เพิ่ม property ใหม่ให้ schema TopHost (rateBpsUp, rateBpsDown), TrafficTopHosts (rateSampledAt) และ TrafficHostDetail (currentRateBpsUp, currentRateBpsDown, rateSampledAt) โดย description ต้องระบุ: หน่วยเป็น bit/วินาที (bps), เป็นค่าเฉลี่ยของช่วง poll ล่าสุดราว 10 วินาที ไม่ใช่ค่า ณ วินาทีนี้, เป็นค่าประมาณจาก conntrack ระดับเดียวกับ accuracy, ทิศทาง up/down เป็น flow-relative, และฟิลด์อาจไม่ปรากฏเลยเมื่อยังไม่มี sample (omitempty). ห้ามเพิ่ม path ใหม่ ห้ามแก้ path/parameter เดิม ห้ามแตะ schema ของ TrafficStatistics. แก้สองไฟล์ให้เนื้อหาเหมือนกันเป๊ะ (backend/internal/api/dist/openapi.yaml เป็นผลลัพธ์จาก build.sh ห้ามแก้ด้วยมือ)",
    "acceptance": [
      "diff docs/openapi.yaml frontend/public/openapi.yaml ไม่มีความต่าง",
      "ไม่มี path หรือ query parameter ใหม่",
      "ไฟล์ยังเป็น YAML ที่ parse ได้ (หน้า ApiDocs เปิดได้)"
    ],
    "depends_on": ["T-04"],
    "status": "done"
  },
  {
    "task_id": "T-08",
    "title": "frontend service: type + mock data ของฟิลด์ความเร็ว",
    "layer": "frontend",
    "files": ["frontend/src/services/trafficStatisticsService.ts"],
    "instruction": "(1) เพิ่มฟิลด์ใหม่เป็น optional ทุกตัวใน type TopHost (rateBpsUp?: number, rateBpsDown?: number), TrafficTopHosts (rateSampledAt?: string) และ TrafficHostDetail (currentRateBpsUp?: number, currentRateBpsDown?: number, rateSampledAt?: string) ให้ตรงกับ openapi ที่ T-07 เขียนไว้ — optional เพราะ backend ใช้ omitempty และ build frontend เก่า/ใหม่ต้องคุยกับ backend คนละเวอร์ชันได้ (2) อัปเดต mock generator ในไฟล์เดียวกันให้ใส่ค่าความเร็วที่สมเหตุสมผลและสอดคล้องคร่าวๆ กับ bytes ที่ mock อยู่แล้ว (เช่น คิดจาก bytes ของแถวนั้นหารด้วยช่วงเวลา) พร้อม rateSampledAt เป็นเวลาปัจจุบัน เพื่อให้ทดสอบหน้าด้วย -mock=true ได้จริง (3) ห้ามแก้ฟิลด์/พฤติกรรมเดิมของ service นี้",
    "acceptance": [
      "tsc -b / yarn build ผ่าน",
      "ฟิลด์ใหม่เป็น optional ทุกตัว",
      "โหมด mock แสดงค่าความเร็วได้ ไม่ใช่ undefined ทุกแถว"
    ],
    "depends_on": ["T-07"],
    "status": "done"
  },
  {
    "task_id": "T-09",
    "title": "หน้า Traffic list: คอลัมน์ Speed ในสองตาราง",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsTraffic.tsx"],
    "instruction": "เพิ่มคอลัมน์ 'Speed' ในตาราง Top Sources และ Top Destinations แสดง Down/Up ด้วย fmtRate (จาก rateBpsDown / rateBpsUp) โดย (1) ใช้คอมโพเนนต์ตาราง/badge เดิมจาก components/ui เท่านั้น จัดวางให้ล้อกับคอลัมน์ Up/Down ที่มีอยู่ (2) แถวที่ไม่มีค่า (undefined) ให้แสดง '—' ไม่ใช่ 0 เพื่อแยก 'ไม่มีข้อมูล' ออกจาก 'เงียบจริง' (3) หัวคอลัมน์หรือ tooltip ต้องบอกว่าเป็นค่าเฉลี่ยประมาณ 10 วินาทีล่าสุด และเป็นค่าประมาณจาก conntrack (4) ห้ามเพิ่ม setInterval / fetch ใหม่ — หน้านี้รีเฟรชทุก 10 วิอยู่แล้วและฟิลด์ใหม่ติดมากับ response เดิม (5) ห้ามใช้คลาสสีดิบของ Tailwind ห้าม shadow-*/backdrop-blur-* ต้องอ่านออกทั้ง dark/light และไม่พังบนจอแคบ (ถ้าจำเป็นให้ซ่อนคอลัมน์บน breakpoint เล็กด้วยคลาส responsive)",
    "acceptance": [
      "yarn build และ yarn lint ผ่าน",
      "ไม่มี polling/fetch เพิ่ม",
      "แสดง '—' เมื่อไม่มีค่า",
      "ตารางยังใช้งานได้บนจอแคบ และรองรับ dark/light"
    ],
    "depends_on": ["T-08"],
    "status": "done"
  },
  {
    "task_id": "T-10",
    "title": "หน้า drill-down: การ์ด 'ความเร็วตอนนี้' ของ IP",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsTrafficHost.tsx"],
    "instruction": "เพิ่มการ์ดสรุปความเร็วปัจจุบันของ IP ที่กำลังดู (Down และ Up) เข้าไปในแถวการ์ด Total/Down/Up ที่มีอยู่แล้ว โดย (1) ใช้ fmtRate และ Card จาก components/ui แบบเดียวกับการ์ดเดิม (2) ป้ายกำกับต้องแยกให้ชัดว่าเป็นความเร็วเฉลี่ย ~10 วิล่าสุด ต่างจากการ์ด Total/Down/Up ที่เป็นยอดสะสมทั้ง window — ผู้ใช้ต้องไม่สับสนระหว่างสองหน่วย (3) เมื่อ found === false หรือค่าเป็น undefined ให้แสดง '—' (4) ห้ามแก้ความหมาย/ค่าในการ์ด Total/Down/Up เดิม ห้ามเพิ่ม fetch หรือ setInterval ใหม่ (5) ห้ามใช้คลาสสีดิบของ Tailwind ห้าม shadow-*/backdrop-blur-* รองรับ dark/light",
    "acceptance": [
      "yarn build และ yarn lint ผ่าน",
      "การ์ดเดิมไม่เปลี่ยนความหมาย",
      "แสดง '—' เมื่อไม่มีข้อมูล",
      "รองรับ dark/light"
    ],
    "depends_on": ["T-08"],
    "status": "done"
  }
]
```

## 4. ระดับการ review ของแต่ละ Task

- **ไม่มี Task ใดเป็นงาน sensitive** ตามนิยามของ CLAUDE.md — แผนนี้ไม่แตะ auth, การ generate
  firewall rule, D-Bus/Netlink write, input validation ของ policy และไม่เพิ่ม query parameter ใหม่
- Task ที่ต้อง review ละเอียดกว่าเพื่อน (แม้ไม่ sensitive) คือ **T-03** — เป็นงาน concurrency
  บน goroutine ที่รันตลอดเวลา จุดตายคือ (ก) การหมุนต้องอยู่ก่อน early-return (ข) `CurrentRates()`
  ต้องคัดลอก map (ค) ห้ามคำนวณ delta รอบสอง

## 5. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ai-qa ตรวจครบแล้ว ผลลัพธ์: PASS)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... && go vet ./... && go test ./... -race — ผ่านทั้งหมด ไม่มี race report",
    "cd frontend && yarn build && yarn lint — ผ่าน ไม่มี error/warning ใหม่",
    "bash build.sh สำเร็จ ได้ไบนารีเดียว ./pigate",
    "diff docs/openapi.yaml frontend/public/openapi.yaml — ไม่มีความต่าง",
    "GET /api/statistics/traffic (หน้า Overview) — JSON เหมือนก่อนแก้ทุกฟิลด์ ต้องไม่มีคีย์ rateBpsUp/rateBpsDown/rateSampledAt โผล่ขึ้นมา",
    "รัน backend ด้วย -mock=true: หน้า /statistics/traffic โหลดได้ มีคอลัมน์ Speed ในทั้งสองตาราง แสดงหน่วย Mbps/Kbps และแสดง '—' เมื่อไม่มีค่า",
    "กราฟ BandwidthTrendCard สลับ bytes <-> speed ได้ทั้ง window 1h และ 24h และ default เปิดมาเป็นโหมด bytes เสมอ",
    "โหมด bytes ของกราฟแสดงผลเหมือนก่อนแก้ทุกประการ ทั้งหน้า Overview, Traffic list และ drill-down (รวม subtitle ของ per-IP ที่ส่งผ่าน prop)",
    "โหมด speed: จุดสุดท้ายของกราฟไม่ร่วงลงผิดปกติเมื่อเพิ่งข้ามขอบบัคเก็ต (พิสูจน์สูตร span_last)",
    "หน้า /statistics/traffic/host/:ip แสดงการ์ดความเร็วปัจจุบัน (Down/Up) พร้อมป้ายกำกับที่แยกจากยอดสะสมชัดเจน และแสดง '—' เมื่อ found=false",
    "ทดสอบกับ mock-from-real หรือเครื่องจริง: ระหว่างดาวน์โหลดไฟล์ใหญ่ ค่า Speed ของ IP นั้นขึ้นภายใน ~20 วินาที และตกกลับใกล้ 0 ภายใน ~20 วินาทีหลังดาวน์โหลดจบ (พิสูจน์ว่า accumulator หมุนแม้ tick เงียบ ไม่ค้างค่า)",
    "ค่า currentRateBps* ของหน้า drill-down ไม่ขัดกับผลรวมความเร็วของแถวใน asSource/asDestination อย่างมีนัยสำคัญ และใช้กติกานับเดียวกับ totalBytes (เคส src==dst นับสองครั้ง)",
    "ทั้ง dark และ light mode แสดงผลถูกต้อง ไม่มี shadow-*/backdrop-blur-* และไม่มีคลาสสีดิบของ Tailwind ที่เพิ่มใหม่",
    "ตรวจ diff ทั้ง PR: ไม่มี exec.Command ใหม่, ไม่มี method ใหม่ใน kernel/interfaces.go, ไม่แตะ real_*.go/mock.go, ไม่แตะ system_status.go/Dashboard/SSE, ไม่มี route ใหม่, ไม่มี migration, ไม่มี config key ใหม่",
    "โค้ดทั้งหมดอยู่บน branch feat/statistics-traffic-speed และเปิดเป็น PR (ไม่มีการ push main)"
  ]
}
```

**หมายเหตุการทดสอบ:** ข้อที่ต้องใช้เครื่องจริง/`-mock-from-real` (การขึ้น-ตกของ Speed ระหว่าง
ดาวน์โหลดจริง) และการตรวจด้วยตาบนเบราว์เซอร์จริง เป็นส่วนที่ **เจ้าของโปรเจกต์ควรยืนยันเอง**
ก่อนเปิด PR — ai-qa ตรวจผ่าน mock server และเทสต์อัตโนมัติแล้ว

## 6. ความเสี่ยง / ข้อควรระวัง (ส่งต่อให้ ai-developer และ ai-qa อ่านก่อนเริ่ม)

1. **bit vs byte สลับกัน = ผิด 8 เท่า** และผู้ใช้จับได้ยาก — กติกาคือ **backend ส่ง bps เสมอ**
   (แปลงหน่วยที่ `CurrentRates()` จุดเดียว) และ frontend แปลงหน่วยแสดงผลที่ `fmtRate` จุดเดียว
   ยกเว้นโหมด speed ของกราฟ (T-02) ที่ต้องคูณ 8 เองเพราะข้อมูลต้นทางเป็น bytes
2. **บัคเก็ตล่าสุดยังเก็บไม่ครบ 5 นาที** — ถ้าหารด้วย 300 ตรงๆ กราฟความเร็วจะร่วงที่จุดสุดท้าย
   ทุกครั้ง ผู้ใช้จะเข้าใจผิดว่าเน็ตตก ต้องทำตามสูตร `span_last` ใน §2.1
3. **ความเร็วต้องตกกลับเป็น 0 ได้** — จุดพลาดที่ง่ายที่สุดของแผนนี้คือหมุน accumulator หลัง
   early-return ของ `poll()` แล้วความเร็วค้างค่าเดิมตลอดกาล (มีเทสต์บังคับใน T-06 ข้อ 2)
4. **conntrack เป็นค่าประมาณ** — flow ที่รายงานตอน DESTROY จะทิ้งไบต์ทั้งก้อนลงช่วงปัจจุบัน
   (พฤติกรรมที่บันทึกไว้แล้วใน Caution 7 ของแผนก่อนหน้า) ในมุมมองความเร็วจะเห็นเป็น
   **สไปก์ปลอม** ชัดกว่ามุมมอง bytes มาก — จึงต้องคง `AccuracyBadge` และข้อความกำกับไว้เสมอ
5. **window 24 ชม.** เฉลี่ยต่อ 5 นาที จะกลบพีคจนดูเหมือนความเร็วต่ำผิดปกติ — ต้องมีข้อความ
   กำกับใต้กราฟในโหมด speed (การแสดงค่าพีคเป็นงานอนาคต ไม่อยู่ในแผนนี้)
6. **Race**: `CurrentRates()` ต้องคัดลอก map ออกมาใต้ล็อก ห้ามคืน map ภายใน — เป็นบั๊กประเภท
   เดียวกับที่ `GetTrafficDetail` เขียนคอมเมนต์เตือนไว้ยาว บังคับด้วยเทสต์ `-race` (T-06 ข้อ 5)
7. **ห้ามทำให้ response ของหน้า Overview เปลี่ยน** — `TopHost` ใช้ร่วมกันสองหน้า ป้องกันด้วย
   `omitempty` + เทสต์ marshal (T-06 ข้อ 6)
8. **ห้ามเร่ง poll conntrack ให้ถี่กว่า 10 วิ** เพื่อไล่ให้ "เรียลไทม์กว่านี้" — dump ได้ถึง 50k flow
   ต่อรอบ บน Pi 5 คือการแลก CPU กับความสวยของกราฟ ถ้าอนาคตต้องการเรียลไทม์ระดับวินาทีจริงๆ
   ให้เปิดแผนใหม่ที่ใช้ netlink interface counters (แนวทางที่ถูกตัดออกจากแผนนี้) ไม่ใช่เร่ง conntrack
9. เอกสารข้างเคียงที่พบว่าไม่ตรงโค้ดแล้ว (ไม่อยู่ในขอบเขตแผนนี้ แต่ควรแก้แยก): README ระบุว่า
   Dashboard traffic เป็น "simulated" ทั้งที่ `collectTraffic()` อ่าน netlink counters จริงมานานแล้ว

## 7. Known limitations (บันทึกไว้หลัง QA — ยอมรับได้ ไม่บล็อกการเปิด PR)

1. **แยกไม่ออกระหว่าง "ไม่มีข้อมูลความเร็ว" กับ "ความเร็ว 0 จริง"** (พบโดย ai-qa, severity ต่ำ)
   - สาเหตุ: ฟิลด์ rate เป็น `uint64` + `omitempty` → ค่า 0 จะถูกตัดออกจาก JSON เหมือนกับ
     กรณีที่ยังไม่มี sample เลย ฝั่ง frontend จึงเห็นเป็น `undefined` ทั้งสองกรณี
   - ผลที่ผู้ใช้เห็น: แสดง `—` ทั้งสองกรณี ซึ่ง **ยังสื่อความหมายถูกต้อง** ("ตอนนี้ไม่มีทราฟฟิก")
     จึงไม่ถือเป็นบั๊กและไม่ต้องแก้ในรอบนี้
   - ทางออกถ้าอนาคตต้องแยกให้ได้จริง (เช่น อยากโชว์ `0 bps` ตัวเลขจริงตอนเงียบ):
     เปลี่ยนเป็น pointer (`*uint64`) หรือถอด `omitempty` ออก **แต่ต้องระวังว่าทั้งสองทาง
     จะทำให้ response ของหน้า Overview (`/api/statistics/traffic` ซึ่งใช้ `TopHost` ร่วมกัน)
     มีคีย์เพิ่มขึ้นมา** — ขัดกับข้อผูกพัน §1.7 ของแผน `statistics-traffic-page-plan.md`
     ถ้าจะทำต้องแยก DTO ของหน้า Traffic ออกจาก `TopHost` ก่อน = งานใหญ่กว่าที่ได้ประโยชน์
2. **ความละเอียดของ "ความเร็วตอนนี้" = ~10 วินาที** (คาบ poll ของ conntrack) ไม่ใช่ค่า ณ
   วินาทีนี้ — เป็นข้อจำกัดโดยการออกแบบตาม §1/§6 ข้อ 8 มี label กำกับใน UI แล้ว
3. **กราฟโหมด speed เป็นค่าเฉลี่ยต่อ 5 นาที** ไม่ใช่ค่าพีค — window 24 ชม. จะกลบพีคชัดเจน
   (ฟีเจอร์ "peak ต่อ bucket" ถูกกันไว้เป็นงานอนาคตตั้งแต่ §0)
