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
>
> **แก้ไขครั้งที่ 2 (2026-08-01 — ทวนกับโค้ดจริงอีกรอบก่อนส่งเข้า pipeline developer/QA):**
> 1. แก้เลขบรรทัดที่คลาดเคลื่อนใน §1 / T-02 / T-03 / T-05 / T-06
>    (`isPrivateIP` :402→**:397**, `model.TopHost` :14-40→**:14-37**,
>    `model.TrafficStatistics` :98-143→**:96-140**, openapi `TrafficStatistics` :4187→**:4185**,
>    `statisticsService.ts` types :70-98→**:68-96**, mock branch :202-249→**:203-247**)
>    — เลขบรรทัดอื่นทั้งหมดใน §1 ทวนแล้วตรงกับโค้ดปัจจุบัน
> 2. **แก้ 3 จุดเชิงโครงสร้างที่ร่างเดิมมองข้าม** (รายละเอียด §2.5 และ §5 ข้อ 15-18):
>    - (ก) **race ของ invariant** — ถ้า `GetBandwidthSeries` จับ `RLock` คนละครั้งกับ
>      `GetTrafficBreakdown` แล้ว `poll()` แทรกกลาง `Σ series ≠ observedBytes` ได้จริง
>      → ย้ายไปคำนวณ **ใน RLock เดียวกัน** ของ `GetTrafficBreakdown` (T-03 เขียนใหม่)
>    - (ข) **window เดิมเป็นแบบนับ bucket ไม่ใช่ตามเวลา** (`traffic_stats.go:847-855`, `:930-939`
>      เอา "12 ก้อนท้าย"/"ทั้ง ring") ถ้ามีช่วงเงียบจน `poll()` ไม่สร้าง bucket ช่วงเวลาที่ ring
>      ครอบคลุมจะกว้างกว่า window → bucket ที่หลุดออกนอกแกนเวลาทำให้ผลรวมไม่ตรง
>      → เพิ่มกฎ **carry เข้าจุดริมสุด** (T-03) + เทสต์ใหม่ (T-04 ข้อ 7)
>    - (ค) **helper ฝั่ง frontend ยัง import ไม่ได้** — `fmtBytes` (`StatisticsOverview.tsx:32`),
>      `UpDownLine` (`:96`), `HostLabel` (`:111`) เป็น local ทั้งหมด และ `Dashboard.tsx:103` มี
>      `fmtBytes` ของตัวเองอีกชุด → T-08 ถูกแตกเป็น **T-08A (แยก shared)** / **T-08B (การ์ด)**
>      และ **T-07 ต้องรอ T-08A**
> 3. เพิ่ม §8 คำถามใหม่ 2 ข้อ (ข้อ 6-7) ให้เจ้าของโปรเจกต์ตัดสิน — **ไม่แตะมติ §7 เดิม**

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
- ไม่แตะการ์ด Dashboard ใด ๆ ยกเว้น (ก) ย้ายค่าคงที่ `CHART_BG_CLASSES` ออกไปเป็น shared และ
  (ข) ให้ `Dashboard.tsx` ใช้ `fmtBytes` ตัวกลางแทนสำเนาของตัวเอง (ทั้งคู่อยู่ใน T-08A
  และต้องไม่เปลี่ยนผลลัพธ์ที่แสดงแม้แต่ตัวอักษรเดียว)
- ไม่แตะ Top Denied / Top Queried Domains / Top Conversations
- ไม่เพิ่ม window ใหม่ (เช่น 7d) — ring buffer เก็บได้แค่ 24 ชม. และเป็น RAM ล้วนตามเจตนาเดิม
- ไม่ทำ drill-down จากกราฟ/การ์ดใหม่ (คลิกแล้วเจาะดูรายเครื่อง) — งานแยกถ้าต้องการ
- **ไม่ downsample ที่ API** (มติข้อ 5) — ถ้าจำเป็นต้องลดจุดเพื่อความลื่น ให้ทำที่คอมโพเนนต์
  กราฟฝั่ง frontend เท่านั้น (ดู T-07 / §5 ข้อ 13)
- **ไม่แก้ semantics ของ `TopHost.bytesUp/bytesDown`** (flow-relative ตามที่ PR #117 ล็อกไว้)
  แม้กราฟใหม่จะเป็น LAN-relative — ความต่างนี้ถูกบันทึกไว้เป็นคำถามข้อ 7 ที่ §8

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-08-01, ทวนซ้ำรอบที่ 2)

| ส่วน | สถานะ | อ้างอิง (file:line ตรวจแล้วกับโค้ดปัจจุบัน) |
|---|---|---|
| bucket ring 5 นาที × 288 | **มีแล้ว** — `trafficDetailBucket{ts, hostBytes, catBytes, ruleBytes, dstBytes, convBytes, observed}` | `service/traffic_stats.go:89-111`, const `:27-30` |
| ทิศทางต่อ host/dst/conv | **มีแล้ว** `dirBytes{Orig,Reply}` + `Total()` หลัง PR #117 | `service/traffic_stats.go:113-120` |
| doc comment ที่ล็อกว่า bucket เก็บ flow-relative เท่านั้น | **มีอยู่จริง** — `hostBytes/dstBytes/convBytes store flow-relative direction … never up/down` | `service/traffic_stats.go:104-108` (T-02 ต้องอัปเดตข้อความนี้) |
| ยอดรวมต่อ bucket | มีแต่ `observed uint64` — **ยังไม่มีมิติทิศทาง** | `service/traffic_stats.go:94`, `addBucket :717,730,741` |
| accessor ของหน้า Statistics | `GetTrafficBreakdown(window)` คืน map รวมทั้ง window (ยุบ bucket ทิ้ง) — **ไม่มี series ตามเวลา** | `service/traffic_stats.go:918-979`, struct `TrafficBreakdown :904-909` |
| ผู้เรียก `GetTrafficBreakdown` | **มีที่เดียวใน production** (`statistics.go:192`) + เทสต์อ่านฟิลด์ → **เพิ่มฟิลด์ในสตรักต์นี้ปลอดภัย (additive)** | `service/statistics.go:192`, `traffic_stats_test.go:213,239,277,301,471,504,525,581` |
| การเลือก window | **นับก้อน ไม่ใช่ตามเวลา**: slice ท้าย `trafficWindow1hBuckets` ก้อนสำหรับ 1h, ทั้ง ring สำหรับ 24h | `traffic_stats.go:847-855`, `:930-939` |
| ประกอบ response | `GetStatistics` (struct literal `:214-231`) | `service/statistics.go:187-232` |
| helper ที่ใช้ซ้ำได้ | `isPrivateIP` **`service/statistics.go:397`** (ใช้ `netip` — private/loopback/link-local), `percentOf` `traffic_stats.go:1060` | — |
| flow key | hash ของ (family, proto, srcIP, srcPort, dstIP, dstPort) → **key เดียวกัน = คู่ IP เดิมเสมอ** (สำคัญกับ `lanFlip`) | `kernel/real_traffic_account.go:112-142` |
| DTO | `model.TrafficStatistics` (ไม่มี series), `model.TopHost` มี `bytesUp/bytesDown` แล้ว | **`model/statistics.go:96-140`**, **`:14-37`** |
| handler / route | `HandleGetStatistics` + `authRoute("GET /api/statistics/traffic")` — **ไม่ต้องแก้** | `api/handlers.go:420`, `api/router.go:42` |
| gzip/compression ของ API | **ไม่มี middleware บีบอัดใด ๆ** — JSON ออกไปดิบ (เกี่ยวกับขนาด payload ของ series) | grep `gzip`/`Content-Encoding` ใน `backend/internal/api/` = ไม่พบ (ทวนซ้ำแล้ว) |
| openapi | `TopHost` :3993, `TrafficStatistics` **:4185** — เลขบรรทัดตรงกันทั้ง `docs/` และ `frontend/public/` | ทั้ง 2 ไฟล์ |
| frontend client | `TrafficStatistics` **:68-96** + mock branch **:203-247** (`observedBytes` = Σ `topSources[].bytes`, `:209`) | `frontend/src/services/statisticsService.ts` |
| หน้า Overview | กริดเดียว `grid-cols-1 lg:grid-cols-2` มี 5 การ์ด, poll 10 วิ, เรนเดอร์เป็น **ternary ซ้อน** (`isLoading&&!stats` → skeleton, `stats&&!hasData` → การ์ด empty อันเดียว, `stats` → กริด) | `StatisticsOverview.tsx:499-522`, `:30`, `:426-430`, `hasData :432-439`, skeleton `:479-491` |
| helper ฝั่งหน้า Overview | **ไม่ export**: `fmtBytes :32`, `UpDownLine :96`, `HostLabel :111`, `AccuracyBadge :43` | `StatisticsOverview.tsx` |
| การ์ดต้นแบบ Protocol Breakdown | `ProtocolBreakdownCard` + `CHART_BG_CLASSES` (`bg-chart-1..5`) | `frontend/src/pages/Dashboard.tsx:786`, `:813-859` |
| `fmtBytes` ของ Dashboard | **สำเนาคนละตัว** กับของ Overview (สูตรเดียวกัน) | `Dashboard.tsx:103` |
| แพตเทิร์นกราฟที่มีอยู่ | `BandwidthCard` ใช้ recharts ตรง ๆ (`LineChart`/`ResponsiveContainer`) + `useTheme` คุมสี grid/axis; `Card className="lg:col-span-2"` ที่ `:369`; `dot={false}`+`isAnimationActive={false}` อยู่แล้ว (`:418-431`); Y axis hardcode `"G"` ที่ `:399`/`:410` | `Dashboard.tsx:360-439`, `aggregateHourly :220-239` |
| ตัวแปรสี chart | `--chart-1..5` เป็น grayscale ทั้ง dark/light | `frontend/src/index.css:93-97`, `:130-134` |
| recharts | มีใน dependency แล้ว (`components/ui/chart.tsx` + Dashboard) — **ไม่ต้องเพิ่ม dep** | — |
| โฟลเดอร์ปลายทางของคอมโพเนนต์ใหม่ | มีแล้ว มีไฟล์เดียว `DnsStatsShared.tsx` (export `useStatsWindow`, `StatsWindowSelect`) | `frontend/src/components/statistics/` |

**สรุป:** ข้อมูลที่ต้องใช้อยู่ครบใน bucket ring แล้ว งานแบ็กเอนด์เหลือแค่ (ก) ให้ยอดรวมต่อ bucket
มีมิติทิศทาง และ (ข) ให้ `GetTrafficBreakdown` คืนค่า "ต่อ bucket" เพิ่มมาใน RLock เดิม ส่วน Top 5 Hosts
**ไม่ต้องแตะแบ็กเอนด์เลย** (ใช้ `topSources` ที่มีอยู่ ตัด 5 แถวแรก) แต่ฝั่ง frontend ต้องแยก helper
ที่ยังเป็น local ออกมาก่อน (T-08A)

## 2. แนวทางเทคนิค

### 2.1 DTO ใหม่ (additive บน endpoint เดิม)

```go
// model/statistics.go
type BandwidthPoint struct {
    Ts        string `json:"ts"`        // RFC3339 จุดเริ่ม bucket (device local time เหมือน bucket.ts)
    Bytes     uint64 `json:"bytes"`     // = BytesUp + BytesDown เสมอ
    BytesUp   uint64 `json:"bytesUp"`
    BytesDown uint64 `json:"bytesDown"`
}
// เพิ่มใน TrafficStatistics: Series []BandwidthPoint `json:"series"`
```

> **หมายเหตุเรื่องเวลา:** `addBucket` ใช้ `now.Truncate(trafficDetailBucketSpan).Format(time.RFC3339)`
> ด้วย `time.Now()` (local time พร้อม offset) — `GeneratedAt` ของ response เป็น UTC คนละแบบ
> **จงใจไม่แก้ทั้งคู่** ให้ `Ts` ใช้ local เหมือน bucket เดิม เพราะ RFC3339 มี offset ติดมาอยู่แล้ว
> `new Date(ts)` ฝั่งเบราว์เซอร์จึงตีความถูกทั้งสองแบบ

### 2.2 ทิศทางระดับ bucket — LAN-relative ไม่ใช่ flow-relative

`dirBytes` ที่มีอยู่เป็น **flow-relative** (Orig = src→dst) ถ้าเอามารวมเป็นยอดของทั้งเครือข่ายตรง ๆ
flow ที่ริเริ่มจากอินเทอร์เน็ต (port forward, สแกน) จะสลับด้าน ทำให้กราฟรวม "Upload/Download"
มีความหมายปนกัน → แปลงเป็น LAN-relative **ตอนคำนวณ delta ที่เดียว** โดยดูจาก IP:

| src / dst | up (ออกจาก LAN) | down (เข้า LAN) |
|---|---|---|
| private → public | Orig | Reply |
| public → private | Reply | Orig |
| private→private / public→public | Orig | Reply (คงเดิม, บันทึกไว้ใน doc comment) |

การจำแนกต้องเป็น **ฟังก์ชันบริสุทธิ์ตัวเดียว** ที่ใช้ร่วมกันทั้งสองเส้นทาง:

```go
// lanFlipFor รายงานว่าต้องสลับ Orig/Reply เป็น up/down หรือไม่ (ตาราง §2.2)
func lanFlipFor(srcIP, dstIP string) bool { return !isPrivateIP(srcIP) && isPrivateIP(dstIP) }
```

- ใน `processFlows`: เรียก **ครั้งเดียวตอนสร้าง `flowSampleState`** แล้วเก็บผลเป็น `lanFlip bool`
  ในสตรักต์นั้น (ไม่ parse IP ซ้ำทุก poll — ที่ 50k flows × ทุก 10 วิ บน Pi 5 คุ้มค่ามาก)
- ใน `onFlowEnd`: เรียก `lanFlipFor(f.SrcIP, f.DstIP)` **ตรง ๆ จาก `f`** ไม่ต้องไปอ่านจาก state
  ที่กำลังจะถูก `delete` (เส้นทางนี้เป็น event ไม่ใช่ hot loop) — และผลลัพธ์เท่ากันเสมอ เพราะ
  `flowKey` ถูกแฮชจากคู่ IP/พอร์ตเดียวกัน (`kernel/real_traffic_account.go:137-142`) ดังนั้น
  key เดียวกัน = คู่ IP เดิมเสมอ **การจำแนกจึงคงที่ตลอดอายุ flow โดยโครงสร้าง**

โครงสร้างที่เปลี่ยน: `trafficDetailBucket.observed` จาก `uint64` → `dirBytes`
(คงหลัก "แหล่งความจริงเดียว" ของ §2.3 ในแผน PR #117: ยอดรวมเป็น method `Total()` ไม่ใช่ฟิลด์ที่ 3)

> ⚠️ **ขัดกับ doc comment เดิมที่ `traffic_stats.go:104-108`** ซึ่งเขียนล็อกไว้ว่า bucket
> "เก็บ flow-relative เท่านั้น ไม่เคยเก็บ up/down; การ flip เกิดที่ presentation layer เท่านั้น"
> — แผนนี้ทำให้ `observed` เป็นข้อยกเว้นที่จงใจ **T-02 จึงต้องแก้ข้อความ comment นั้นให้ตรงกัน**
> (ระบุว่า hostBytes/dstBytes/convBytes ยัง flow-relative เหมือนเดิม แต่ `observed` เป็น
> LAN-relative เพราะเป็นยอดรวมของทั้งเครือข่ายที่ไม่มี "เจ้าของแถว" ให้อ้างอิงทิศทาง)
> ถ้าไม่แก้ คนอ่านโค้ดคนถัดไปจะเจอ comment ที่โกหก = บั๊กรอเกิด

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

### 2.5 ความสอดคล้องของ `Σ series == observedBytes` (เพิ่มในการแก้ไขครั้งที่ 2)

ร่างแรกให้ series เป็นเมธอดแยก (`GetBandwidthSeries`) ที่จับ `RLock` ของตัวเอง และเลือก bucket
"ตามเวลา" ขณะที่ `GetTrafficBreakdown` เลือก "ตามจำนวนก้อน" ซึ่งพังได้ 2 ทาง:

1. **Race กับ `poll()`** — `GetStatistics` เรียกสองเมธอดห่างกันเสี้ยววินาที ถ้า `poll()` (ทุก 10 วิ)
   หรือ `onFlowEnd` (event, ถี่กว่านั้นมาก) แทรกกลาง `observedBytes` กับ `Σ series` จะไม่เท่ากัน
   แบบสุ่ม — เกิดถี่กว่าที่คิด เพราะทุก client poll ทุก 10 วิพอดีกับ cadence ของ poller
2. **Window คนละนิยาม** — `GetTrafficBreakdown("1h")` = "12 ก้อนท้ายสุด" ไม่ใช่ "60 นาทีที่แล้ว"
   ถ้าเครื่องเงียบเป็นช่วง ๆ 12 ก้อนนั้นอาจกินเวลาจริง 3 ชั่วโมง เช่นเดียวกับ 24h ที่ใช้ทั้ง ring
   (288 ก้อนอาจกินเวลาหลายวัน) → bucket ที่อยู่นอกแกนเวลา 12/288 ช่องจะถูกนับใน `observedBytes`
   แต่หายไปจาก series

**ทางแก้ที่แผนนี้บังคับ (ทำให้ invariant เป็นจริง "โดยโครงสร้าง" ไม่ใช่ "โดยบังเอิญ"):**

- คำนวณ series **ในลูปเดิมและใต้ `RLock` เดิม** ของ `GetTrafficBreakdown` จาก `windowBuckets`
  **ชุดเดียวกันเป๊ะ** กับที่บวกเข้า `observed` → ตัดปัญหา (1) ทิ้งทั้งหมด
- ทุก bucket ใน `windowBuckets` ต้องลงจุดใดจุดหนึ่งเสมอ: คำนวณ index จากเวลา ถ้าหลุดขอบให้
  **carry เข้าจุดริมสุด** (เก่ากว่าแกน → จุดแรก, ใหม่กว่าแกน เช่นนาฬิกาถูก NTP ดึงถอยหลัง → จุดสุดท้าย)
  → ตัดปัญหา (2) ทิ้ง โดยแลกกับ "ยอดกองที่จุดซ้ายสุด" ในกรณีหายากที่ ring กว้างกว่า window
  (บันทึกเป็นคำถามข้อ 6 ที่ §8 — ถ้าเจ้าของโปรเจกต์อยากได้พฤติกรรมอื่นค่อยเปลี่ยน)

### 2.4 ทางเลือกที่ตัดทิ้ง

- **ใช้ `/api/dashboard/traffic` (`TrafficHistory`, WAN link counters) แทน** — ไม่ต้องแก้แบ็กเอนด์เลย
  แต่คนละแหล่งข้อมูลกับทุกการ์ดในหน้า: นับ traffic ของตัวเราเตอร์เองด้วย, ไม่นับ LAN↔LAN,
  เป็นค่า exact ขณะที่หน้านี้เป็น conntrack estimate → ผู้ใช้เอากราฟไปเทียบกับ Top Hosts
  ในหน้าเดียวกันแล้วตัวเลขไม่ตรง = บั๊กที่อธิบายไม่ได้ **ตัดทิ้ง**
- **endpoint ใหม่ `/api/statistics/bandwidth`** — เพิ่ม auth surface + fetch รอบที่สองที่ต้อง sync
  window/refresh กับของเดิมเอง โดยไม่ได้อะไรเพิ่ม (ข้อมูลมาจาก ring เดียวกัน) และยังทำให้
  invariant §2.5 ข้อ 1 พังถาวร (คนละ request = คนละ snapshot)
- **เมธอด `GetBandwidthSeries(window)` แยกที่จับ RLock ของตัวเอง** — สะอาดกว่าในแง่ชื่อ แต่เปิดช่อง
  race ตาม §2.5 ข้อ 1 **ตัดทิ้งในการแก้ไขครั้งที่ 2**
- **ยุบ 24h เป็นรายชั่วโมง 24 จุด** — payload เล็กกว่า ~10 เท่า แต่กลบ burst สั้น ๆ
  (การดาวน์โหลดก้อนใหญ่ 10 นาทีจะถูกเฉลี่ยหายไปในชั่วโมงนั้น) **เจ้าของโปรเจกต์เลือกความละเอียด
  แทนขนาด payload** (มติข้อ 5) — ทางเลือกนี้ถูกตัดอย่างเป็นทางการแล้ว
- **เพิ่มฟิลด์ `observedUp/observedDown` คู่กับ `observed` เดิม** — แหล่งความจริง 2 ที่ที่ drift ได้
  (เหตุผลเดียวกับ §2.3 ของแผน PR #117) → เปลี่ยน type ของฟิลด์เดิมให้ compiler ไล่ call site แทน
- **คำนวณ Top 5 ที่แบ็กเอนด์เป็น endpoint/ฟิลด์ใหม่** — `topSources` มี 10 แถวเรียงแล้ว
  ตัด 5 ที่ frontend พอ ไม่ต้องเพิ่มอะไร

## 3. ขั้นตอนการทำ (inner layer → outer)

> **ลำดับ dependency ที่บังคับ:** T-01 → T-02 → T-03 → T-04 → T-05 → T-06 → **T-08A** → T-07 →
> T-08B → T-09 → T-10 (T-07/T-08B ต้องใช้ `fmtBytes`/`HostLabel`/`UpDownLine` ที่ T-08A แยกออกมา)

### T-01 — DTO ของ API
**ไฟล์:** `backend/internal/model/statistics.go` (`TrafficStatistics` อยู่ที่ **:96-140**)
เพิ่ม `BandwidthPoint` (§2.1) + ฟิลด์ `Series []BandwidthPoint \`json:"series"\`` ใน
`TrafficStatistics` พร้อม doc comment ระบุ: LAN-relative, `bytesUp+bytesDown == bytes`,
`Σ series[].bytes == observedBytes` (รวมกฎ carry ตาม §2.5), ความละเอียดคงที่ 5 นาที
และความยาวคงที่ (12 / 288), เรียงเก่า→ใหม่
**acceptance:** ไฟล์นี้แก้เสร็จ, `cd backend && go build ./...` ยังผ่าน (additive ล้วน)

### T-02 — ยอดรวมต่อ bucket มีมิติทิศทาง 🔒 (แตะ path นับ byte — review เข้ม)
**ไฟล์:** `backend/internal/service/traffic_stats.go`
1. เพิ่มฟังก์ชัน `lanFlipFor(srcIP, dstIP string) bool` (§2.2) — ฟังก์ชันเดียว ใช้ทั้งสองเส้นทาง
   ห้ามเขียน logic ซ้ำ และห้ามเขียน regex/prefix-matching เอง (ใช้ `isPrivateIP`
   ที่ `statistics.go:397` ซึ่งอยู่แพ็กเกจเดียวกัน)
2. `flowSampleState` (**:81-84**) เพิ่ม `lanFlip bool` — ตั้งค่า **ครั้งเดียว** ตอนสร้าง state ใหม่
   (**:460** `st = &flowSampleState{}`) ด้วย `lanFlipFor(f.SrcIP, f.DstIP)` **ห้ามแก้ค่านี้ภายหลัง**
3. `trafficDetailBucket.observed` (**:94**) `uint64` → `dirBytes`
4. `processFlows` (**:427**) เปลี่ยน return จาก `uint64` เป็น `dirBytes`: หลังคำนวณ
   `d := dOrig + dReply` (**:490**) ให้สะสม `observed.Orig/.Reply` โดย
   up = `st.lanFlip ? dReply : dOrig`, down = อีกด้าน — **ห้ามคำนวณ delta ชุดที่สอง**
   `d` ตัวเดิมยังคงเป็นตัวเดียวที่ไหลเข้า `catDeltas` (invariant ข้อ 5 ของ PR #117)
   และผลรวมต้องเท่ากับของเดิมเสมอ (`observed.Total()` == observed เดิมทุกกรณี)
   > call site ของ `processFlows` ในเทสต์ (`traffic_stats_test.go:434,561,574`) **ไม่ได้ใช้ค่าที่
   > return** → เปลี่ยน return type ไม่พังเทสต์เดิม
5. `poll()` (**:393-420**): local `var observed uint64` (**:399**) → `dirBytes`, เงื่อนไข quiet-tick
   (**:414**) ใช้ `observed.Total() == 0` (ส่วนที่เหลือของเงื่อนไขคงเดิมทุกตัว)
6. `onFlowEnd` (**:360-387**): บรรทัด `s.addBucket(...)` (**:386**) ส่ง `dirBytes` ที่ flip ตาม
   `lanFlipFor(f.SrcIP, f.DstIP)` — **เรียกจาก `f` ตรง ๆ ไม่ต้องอ่าน `st.lanFlip`** (§2.2)
   จึงไม่มีปัญหาลำดับกับ `delete(s.flowState, f.Key)` ที่ **:370** และครอบคลุมเคส "ไม่มี baseline"
   (**:371-374**) ที่ไม่มี state ให้อ่านอยู่แล้วโดยอัตโนมัติ
7. `addBucket` (**:717** signature, **:730** merge, **:741** สร้างใหม่) รับ `observed dirBytes`,
   บวกทีละทิศ (`b.observed.Orig += observed.Orig` และ Reply เช่นกัน)
8. `GetTrafficDetail` (**:873**) และ `GetTrafficBreakdown` (**:959**) ใช้ `b.observed.Total()`
   — **response ทั้งสองต้องเหมือนเดิมทุก byte**
9. **อัปเดต doc comment `:104-108`** ให้ตรงกับความจริงใหม่ (ดูกล่องเตือนท้าย §2.2)
> ห้ามแตะ `flowKey`/`flowKeyFromParts`, `mergeDirMap`, `mergeUint64Map`, ค่าคงที่ cap ใด ๆ
> และห้ามเปลี่ยนตรรกะ clamp ต่อทิศ (**:472-481**)

**acceptance:** `go build ./...` ผ่าน, `go test ./...` เดิมยังเขียวหมด (ยังไม่ต้องเพิ่มเทสต์ใหม่ — T-04)

### T-03 — series ต่อ bucket (คำนวณใน RLock เดียวกับ observed)
**ไฟล์:** `backend/internal/service/traffic_stats.go`
1. เพิ่มฟิลด์ `Series []model.BandwidthPoint` ใน `TrafficBreakdown` (**:904-909**) —
   สตรักต์ภายใน ไม่ถูก serialize ตรง ๆ และมีผู้เรียก production แค่ที่เดียว จึงเป็น additive ที่ปลอดภัย
2. ใน `GetTrafficBreakdown` (**:918-979**) หลัง validate window แล้ว **ก่อนเข้า `s.mu.RLock()`**
   ให้กำหนด:
   ```go
   n := trafficWindow1hBuckets              // 12 เมื่อ 1h
   if window == trafficWindow24h { n = trafficDetailBucketMax }   // 288 เมื่อ 24h
   axisEnd := time.Now().Truncate(trafficDetailBucketSpan)        // จุดสุดท้าย = bucket ปัจจุบัน
   axisStart := axisEnd.Add(-time.Duration(n-1) * trafficDetailBucketSpan)
   ```
   **ใช้ค่าคงที่เดิมเท่านั้น ห้ามฮาร์ดโค้ด 12/288/5m**
3. สร้าง `series := make([]model.BandwidthPoint, n)` เติม `Ts` ล่วงหน้าทุกช่อง
   (`axisStart.Add(i*span).Format(time.RFC3339)` — **local time เหมือน `addBucket:718`**)
   ค่า byte เริ่มต้นเป็น 0 ทั้งหมด → ได้ zero-fill และความยาวคงที่ฟรี
4. **ในลูป `for _, b := range windowBuckets` ที่มีอยู่แล้ว** (**:940-963**, ใต้ `RLock` เดิม)
   เพิ่มการลงจุด: parse `b.ts` (`time.Parse(time.RFC3339, b.ts)`) แล้ว
   `idx := int(bt.Sub(axisStart) / trafficDetailBucketSpan)`; **clamp**: `idx < 0 → 0`,
   `idx >= n → n-1`, parse error → `0` (พร้อม comment อธิบายว่านี่คือกฎ carry ของ §2.5
   ที่ทำให้ `Σ series == Observed` เป็นจริงเสมอ แม้ ring จะกินเวลากว้างกว่า window)
   จากนั้นบวก `b.observed.Orig/.Reply` ลง `series[idx].BytesUp/.BytesDown` และ
   `series[idx].Bytes += b.observed.Total()`
   > **ห้าม**คัดลอก `s.buckets` ออกไปวนนอก lock (คำเตือนยาว **:821-834**) — ที่นี่แค่บวกเลข
   > scalar เพิ่มในลูปเดิม จึงไม่เพิ่มความเสี่ยง race ใด ๆ
5. คืน `Series: series` ใน struct literal (**:971-978**)
6. **ไม่มีการยุบ/เฉลี่ย/downsample ใด ๆ** (มติข้อ 5) — 1 bucket = 1 จุด
7. ห้ามแก้ตรรกะเลือก `windowBuckets` (**:930-939**) และห้ามแตะ `GetTrafficDetail`

**ไฟล์:** `backend/internal/service/statistics.go` (`GetStatistics` struct literal **:214-231**)
เพิ่ม `Series: breakdown.Series,` (ใช้ค่าจาก `breakdown` ที่ดึงมาแล้วที่ **:192** — **ห้ามเรียก
เมธอดใหม่อีกรอบ** ไม่งั้น race ตาม §2.5 กลับมา)
> **ไม่ต้อง**แก้ `api/handlers.go` / `router.go` — response เป็น additive และไม่มี input ใหม่จาก client

**acceptance:** `go build ./...` ผ่าน; `curl` window ไหนก็ได้เห็น `series` ยาว 12/288

### T-04 — regression test ของ invariant 🔒
**ไฟล์:** `backend/internal/service/traffic_stats_test.go`, `statistics_test.go`
- แก้ call site เดิมของ `addBucket` 2 จุด (`traffic_stats_test.go:562`, `:575`) ให้ส่ง `dirBytes`
  (ปัจจุบันส่ง `0`) — call site ของ `processFlows` ไม่ต้องแก้ (ไม่ได้ใช้ค่า return)
- เคสใหม่:
  1. `Σ series[].bytes == observedBytes` ทั้ง 1h และ 24h (property test)
  2. ทุกจุด `bytesUp + bytesDown == bytes`
  3. **ความยาวคงที่**: `len(series) == 12` เมื่อ 1h และ `== 288` เมื่อ 24h **แม้ ring จะว่างเปล่า**
     (ทุกจุดเป็น 0) และ `ts` เรียงจากเก่าไปใหม่ ห่างกันช่องละ 5 นาทีเป๊ะ ไม่มีค่าซ้ำ
  4. **zero-fill กลางช่วง**: ใส่ bucket 2 ก้อนที่ห่างกัน แล้วจุดระหว่างกลางต้องเป็น 0
     (ไม่ใช่หายไปหรือถูกเลื่อนตำแหน่ง)
  5. LAN-relative: flow public→private ต้องนับเป็น **down** ไม่ใช่ up (ล็อกตาราง §2.2)
     — ทดสอบทั้งทาง `poll()/processFlows` และทาง `onFlowEnd` (เส้นทางที่ไม่มี state)
  6. `GetTrafficDetail`/`GetTrafficBreakdown` คืนค่าเท่าเดิม (regression guard ของ T-02):
     `Hosts/Dests/Convs/Observed/Truncated` ต้องไม่เปลี่ยนค่าสำหรับชุด input เดิม
  7. **กฎ carry (§2.5):** ยัด bucket ที่ ts เก่ากว่าแกนเวลา (เช่น `-30h` สำหรับ 24h หรือ
     `-3h` สำหรับ 1h) เข้า ring แล้ว `Σ series == observedBytes` ต้องยังเป็นจริง และไบต์ก้อนนั้น
     ต้องไปโผล่ที่ `series[0]` (ไม่หายไปเฉย ๆ)
  8. **1h เป็นเซ็ตย่อยของ 24h:** สำหรับ ring ชุดเดียวกัน `Σ series(1h) <= Σ series(24h)`
     และ `observedBytes(1h) <= observedBytes(24h)`
- `go test -race ./...` (มี `TestTrafficStats_GetTrafficDetailNoRaceWithPoll` เดิมคุมอยู่)
**acceptance:** `cd backend && go build ./... && go test -race ./...` ผ่านหมด

### T-05 — API contract
**ไฟล์:** `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (ต้องเหมือนกันเป๊ะ)
เพิ่ม schema `BandwidthPoint` และฟิลด์ `series` ใน `TrafficStatistics` (**:4185** `properties` +
`required`) พร้อม description ตาม T-01 และระบุชัดว่า **1h = 12 จุด, 24h = 288 จุด, ช่องละ 5 นาที,
zero-filled, เรียงเก่า→ใหม่, up/down เป็น LAN-relative (ต่างจาก `TopHost.bytesUp/bytesDown`
ซึ่งเป็น flow-relative — ระบุไว้ให้ชัดใน description เพื่อไม่ให้ client เข้าใจผิด)**
> ไม่ต้องแก้ `backend/internal/api/dist/openapi.yaml` (build artifact)
**acceptance:** `diff docs/openapi.yaml frontend/public/openapi.yaml` = ไม่มีความต่าง

### T-06 — frontend API client + mock branch
**ไฟล์:** `frontend/src/services/statisticsService.ts` (types **:68-96**, mock branch **:203-247**)
1. เพิ่ม `export interface BandwidthPoint { ts: string; bytes: number; bytesUp: number; bytesDown: number }`
   + `series: BandwidthPoint[];` ใน `TrafficStatistics`
2. mock generator (pure function, ไม่มี timer/side effect เหมือน generator อื่นในไฟล์นี้) สร้าง
   **12 จุด (1h) / 288 จุด (24h)** ย้อนหลังช่องละ 5 นาทีจากเวลาปัจจุบัน ให้ **มีรูปทรง**
   (เช่น sine + ค่าคงที่ต่อ index ไม่ใช่แบนราบ และมีช่วงค่า 0 คั่นบ้าง เพื่อทดสอบ zero-fill/แกนเวลา)
   ratio up/down ให้ download นำเหมือน `mockHosts.upRatio` เดิม
3. **ทำให้ `observedBytes` ของ mock มาจาก series** แทนที่จะเป็น `Σ topSources[].bytes` เดิม
   (**:209**) เพื่อรักษา invariant เดียวกับของจริง; และให้ **`Σ series > Σ topSources[].bytes`
   เล็กน้อย (เช่น +10-15%)** เพื่อให้การ์ด Top 5 มีส่วน "Other" ให้เห็นในโหมด mock
   > ⚠️ **จุดที่ร่างแรกมองข้าม:** ปัจจุบัน `mockTopHosts` (**:143**) คำนวณ `percent` เทียบกับ
   > ผลรวมของแถวตัวเอง → percent รวมได้ 100% พอดี ทำให้ segment "Other" (มติข้อ 3)
   > **ไม่มีทางปรากฏในโหมด mock เลย** และเกณฑ์ทดสอบ §6 ข้อนั้นจะทดสอบไม่ได้
   > **ต้องแก้:** ให้ `mockTopHosts` รับ `observedBytes` เป็นตัวหารของ `percent`
   > (พฤติกรรมเดียวกับ backend จริงที่ใช้ `percentOf(bytes, observed)`) — เป็นการแก้เฉพาะ
   > mock branch ไม่กระทบโค้ดจริง แต่ **จะทำให้ percent ในการ์ด Top Source Hosts/Top Destinations
   > ของโหมด mock เปลี่ยนไปเล็กน้อย ซึ่งเป็นการเปลี่ยนให้ "ตรงกับของจริงมากขึ้น" ไม่ใช่ regression**
   > (ระบุไว้ในรายงาน PR ด้วย)
**acceptance:** `yarn build` (tsc) ผ่าน; เปิดโหมด mock แล้ว `series` มีข้อมูลครบ 12/288 จุด

### T-08A — แยก helper/ค่าคงที่ที่ต้องใช้ร่วม (ต้องทำก่อน T-07 และ T-08B)
**ไฟล์ใหม่:** `frontend/src/lib/chartColors.ts` — ย้าย `CHART_BG_CLASSES`
(`["bg-chart-1"..."bg-chart-5"]`) ออกจาก `Dashboard.tsx:786` มาไว้ที่นี่ แล้ว import กลับใน
`Dashboard.tsx` (**ห้ามเปลี่ยนค่า/ลำดับสี**)
**ไฟล์ใหม่:** `frontend/src/lib/formatBytes.ts` — ย้ายฟังก์ชัน `fmtBytes` (สูตรเดียวกันเป๊ะกับ
`StatisticsOverview.tsx:32-38` และ `Dashboard.tsx:103`) มาเป็น export กลาง แล้วให้ **ทั้งสองไฟล์**
import ตัวนี้แทนสำเนาเดิม
**ไฟล์ใหม่:** `frontend/src/components/statistics/HostCells.tsx` — ย้าย `UpDownLine`
(`StatisticsOverview.tsx:91-109`) และ `HostLabel` (`:111-…`) มาเป็นคอมโพเนนต์ export
แล้ว `StatisticsOverview.tsx` import กลับ
> **เหตุผล:** ทั้งสามตัวเป็น local ของหน้า/ไม่ export ถ้าให้คอมโพเนนต์ใหม่ import จาก
> `pages/StatisticsOverview.tsx` จะเกิด **circular import** (หน้า import การ์ด, การ์ด import หน้า)
> ซึ่งพังยากและดีบักยาก
> **กฎเหล็กของ task นี้: ย้ายอย่างเดียว ห้ามแก้ตรรกะ/มาร์กอัป/สูตรแม้แต่ตัวอักษรเดียว** — หน้าจอ
> Dashboard และ Statistics Overview ต้องแสดงผลเหมือนเดิม 100%
**acceptance:** `yarn build && yarn lint` ผ่าน, `git diff` ของ `Dashboard.tsx`/`StatisticsOverview.tsx`
มีแต่บรรทัด import และการลบโค้ดที่ย้ายออก

### T-07 — คอมโพเนนต์กราฟ (ไฟล์ใหม่) — ต้องทำหลัง T-08A
**ไฟล์ใหม่:** `frontend/src/components/statistics/BandwidthTrendCard.tsx`
ลอกโครง `Dashboard.tsx BandwidthCard` (**:360-439**): `useTheme` คุมสี grid/axis,
`ResponsiveContainer` + `LineChart`, เส้น Download = `var(--primary)` / Upload = สี muted
(**:365-366**), legend สองจุดบน `CardHeader` (**:372-381**), สูง `h-56` (**:384**)
- props: `series: BandwidthPoint[] | undefined`, `window: StatsWindow`, `className?: string`
  — **อ่านค่าแบบป้องกันไว้ก่อน (`series ?? []`)** เผื่อ JS ที่ถูกแคชไว้เจอ backend เวอร์ชันเก่า
  หรือกลับกัน (ไม่ให้หน้าขาวทั้งหน้าเพราะ `undefined.map`)
- **ปรับจูนสำหรับ 288 จุด (มติข้อ 5)** — ทั้งหมดเป็นการตั้งค่า recharts ล้วน ไม่ใช่การลดข้อมูล:
  - `dot={false}` + `isAnimationActive={false}` (ของ Dashboard มีอยู่แล้วที่ **:418-431** —
    **ห้ามถอดออก**; ที่ 288 จุด × 2 เส้น การเปิด animation/dot คือสาเหตุอันดับหนึ่งของอาการหน่วงทุก 10 วิ)
  - `activeDot={{ r: 4, strokeWidth: 0 }}` คงไว้ (วาดเฉพาะจุดที่ hover ไม่กระทบ perf)
  - `XAxis`: กำหนด `interval` ให้แสดง tick ห่าง ๆ (เป้าหมาย ~8-12 label ต่อกราฟ เช่น ทุก 2 ชม.
    เมื่อ 24h = ทุกจุดที่ 24) — ปล่อย auto จะได้ label ทับกันจนอ่านไม่ออกบนการ์ดกว้าง 2/3
  - `type="monotone"` คงไว้ได้; ถ้า QA วัดแล้วพบเฟรมตก ให้ลองเปลี่ยนเป็น `type="linear"` ก่อน
    เป็นอันดับแรก (ถูกกว่า) — **ห้ามแก้ด้วยการลดจำนวนจุดที่ API**
  - memoize ข้อมูลที่แปลงแล้วด้วย `useMemo` โดยผูกกับ `series` (อ้างอิงอ็อบเจกต์ใหม่ทุก fetch อยู่แล้ว)
    เพื่อไม่ให้ map array 288 ช่องใหม่ทุกครั้งที่ React re-render ด้วยเหตุอื่น
  - **ถ้าจำเป็นจริง ๆ** ค่อยเพิ่ม downsample **ในคอมโพเนนต์นี้ที่เดียว** (เช่นรวมทีละ 2 จุด
    เมื่อความกว้างจอ < 640px) และต้องเขียน comment กำกับว่า API ยังส่งความละเอียดเต็ม
- แกน X: label `HH:mm` (แปลงจาก `ts` ด้วย `new Date()` — device local time)
- แกน Y/tooltip: ใช้ `fmtBytes` จาก `lib/formatBytes.ts` (T-08A) **ห้าม hardcode หน่วย "G"**
  แบบ `Dashboard.tsx:399,410` (bucket 5 นาทีบนเครือข่ายบ้านมักอยู่ระดับ MB — ของ Dashboard
  hardcode ไว้ได้เพราะยุบรายชั่วโมงและใช้หน่วย GB)
  tooltip ต้องบอกด้วยว่าเป็นยอด "ต่อ 5 นาที" ไม่ใช่อัตราเร็ว (bps)
- หัวการ์ดต้องสะท้อน window ที่เลือก (เช่น "Bandwidth · 1 ชม. ล่าสุด" / "· 24 ชม. ล่าสุด")
  ห้าม hardcode "last 24h" แบบ `Dashboard.tsx:371`
- empty state เมื่อ `series.length === 0` หรือทุกจุดเป็น 0: "กำลังเก็บข้อมูล traffic…"
  (ข้อความไทยให้เข้าชุดกับการ์ดอื่นในหน้านี้)
- **กฎสไตล์บังคับ:** ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามสีดิบ, dark/light ต้องอ่านออกทั้งคู่

### T-08B — การ์ด Top 5 Hosts (ไฟล์ใหม่) — ต้องทำหลัง T-08A
**ไฟล์ใหม่:** `frontend/src/components/statistics/TopHostsShareCard.tsx`
เลียนแบบ `ProtocolBreakdownCard` (`Dashboard.tsx:813-859`) ทุกจุด: stacked bar `h-2.5` +
`ul` legend มีจุดสี `h-2.5 w-2.5 rounded-full` / ชื่อ truncate / `fmtBytes · percent%`
- props: `hosts: TopHost[]`, `observedBytes: number`
- input = `topSources.slice(0, 5)` — **ไม่กรอง LAN/Internet** (มติข้อ 4) และ **เรียงตาม byte รวม
  ตามที่ API ส่งมา ห้ามจัดอันดับใหม่** (มติข้อ 2)
- แถวใช้ `HostLabel` จาก `components/statistics/HostCells.tsx` (T-08A) เพื่อให้ badge LAN/Internet
  และการแสดง domain ตรงกับการ์ด Top Source Hosts เป๊ะ
- **percent ใช้ `TopHost.percent` จาก API ตรง ๆ** (เทียบ `observedBytes` ทั้ง window) —
  **ห้าม normalize ให้ 5 แถวรวมเป็น 100%** (มติข้อ 3) และปิดท้าย stacked bar ด้วย segment
  **"Other"** สี `bg-muted` ความกว้าง `Math.max(0, 100 - Σ percent)` พร้อม legend แถวสุดท้าย
  ว่า "อื่น ๆ" (ตัวเลขไบต์ใช้ `Math.max(0, observedBytes - Σ bytes)`)
  > **ระวัง edge case:** `Σ percent` อาจเกิน 100 เล็กน้อยจากการปัดเศษ (`percentOf` ปัดทศนิยม 1 ตำแหน่ง)
  > และ `observedBytes` อาจเป็น 0 ตอน ring ว่าง → ต้อง clamp ทั้งความกว้างและตัวเลขไม่ให้ติดลบ
  > และแสดง empty state แทนเมื่อ `hosts.length === 0`
- บรรทัดรองแสดง `↑up · ↓down` ด้วย `UpDownLine` (T-08A) ได้ ถ้าไม่ทำให้แน่นเกินไป
  — **แต่ต้องรู้ว่าค่านี้เป็น flow-relative** ต่างจากกราฟที่เป็น LAN-relative (§8 ข้อ 7)
**กฎสไตล์บังคับ:** ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามสีดิบ (`text-emerald-500`),
ใช้ `bg-chart-*`/`bg-muted`/`text-primary`/`text-muted-foreground`, ต้องดูดีทั้ง dark/light

### T-09 — วางเลย์เอาต์ในหน้า Overview
**ไฟล์:** `frontend/src/pages/StatisticsOverview.tsx` (**:479-523**)
- **สำคัญ:** โครงปัจจุบันเป็น ternary ซ้อน 3 ชั้น และสาขา `stats && !hasData` (**:492-497**)
  จะ **แทนที่ทั้งกริด** ด้วยการ์ด empty ใบเดียว → ถ้าวางแถวใหม่ไว้ในสาขาสุดท้าย กราฟจะหายไป
  ทั้งแถบตอนที่ยังไม่มี top-N (ซึ่งเป็นตอนที่ผู้ใช้อยากเห็นกราฟกำลังเก็บข้อมูลที่สุด)
  **ให้เรนเดอร์แถวใหม่ไว้ "นอก" ternary** (เงื่อนไขแค่ `stats !== null`) เหนือบล็อกเดิม:
  `<div className="grid grid-cols-1 gap-4 lg:grid-cols-3">` →
  `<BandwidthTrendCard className="lg:col-span-2" series={stats.series} window={window_} />` +
  `<TopHostsShareCard hosts={stats.topSources.slice(0,5)} observedBytes={stats.observedBytes} />`
- `lg:col-span-2` ต้องไปอยู่บนตัว `<Card>` ในคอมโพเนนต์ (ผ่าน `cn(className)`) ไม่ใช่ wrapper `<div>`
  (แพตเทิร์นเดียวกับ `Dashboard.tsx:369`)
- กริดเดิมของการ์ดที่เหลือคง `lg:grid-cols-2` ไว้เหมือนเดิม และ **ไม่แก้ค่า `hasData` (:432-439)**
  (ไม่ต้องแก้แล้ว เพราะแถวใหม่ไม่ได้อยู่ใต้เงื่อนไขนั้น — ทางเลือกนี้ diff เล็กกว่าและเสี่ยงน้อยกว่า
  การไปแก้ตรรกะ `hasData` ที่การ์ดอื่นใช้ร่วม)
- แก้ skeleton (**:479-491**) ให้มีบล็อกกราฟ 2/3 + การ์ด 1/3 ด้านบนด้วย เพื่อไม่ให้เลย์เอาต์
  กระโดดตอนโหลดเสร็จ
- อัปเดตข้อความอธิบายใต้หัวข้อ (**:451-453**) ให้พูดถึงกราฟ/Top 5
**acceptance:** `yarn build && yarn lint` ผ่าน, เปิดหน้าแล้วเห็นแถวใหม่ทั้งกรณีมี/ไม่มีข้อมูล

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
   ผลไม่คงที่ ทิศของ bucket จะสวิงไปมา
   **ป้องกัน:** คำนวณครั้งเดียวตอนสร้าง `flowSampleState` และห้ามแก้ภายหลัง + เทสต์ T-04 ข้อ 5
   (หมายเหตุ: `flowKey` แฮชจากคู่ IP อยู่แล้ว จึงไม่มีทางที่ key เดิมจะเปลี่ยนคู่ IP กลางคัน)
2. **`onFlowEnd` ลบ key ทิ้งทันที** (invariant ข้อ 2 ของ PR #117) และยังมีสาขา "ไม่เคยเห็น flow นี้"
   (`:371-374`) ที่ **ไม่มี state ให้อ่านเลย** → ถ้าไปดึง `lanFlip` จาก state จะได้ zero-value =
   ทิศผิดเงียบ ๆ **ป้องกัน:** ใช้ `lanFlipFor(f.SrcIP, f.DstIP)` จาก `f` ตรง ๆ ตาม T-02 ข้อ 6 + เทสต์
3. **series ต้องรวมได้เท่า `observedBytes`** — ถ้าคำนวณแยก lock หรือ zero-fill พลาดขอบ window
   ผู้ใช้จะเห็นกราฟไม่ตรงกับตัวเลขใต้หน้า **ป้องกัน:** คำนวณในลูป/lock เดียวกัน (§2.5) +
   property test T-04 ข้อ 1 และ 7
4. **แกนเวลาโกหกได้เมื่อไม่ zero-fill** — ring ไม่สร้าง bucket ในช่วงที่เงียบ (`poll()` **:414**)
   กราฟจะลากเส้นข้ามช่องว่างเหมือนไม่มีอะไรเกิดขึ้น **ป้องกัน:** pad ที่แบ็กเอนด์ (T-03)
5. **หลังบูตใหม่ ring ว่าง** — 24h จะเป็นเส้นศูนย์ยาว 288 จุดเกือบทั้งกราฟ เป็นพฤติกรรมที่ถูกต้อง
   (RAM-only, `tech_stack_design.md` §8) **ป้องกัน:** empty state/คำอธิบายให้ชัด ไม่ใช่ทำให้ดูเหมือน error
6. **concurrent map** — `trafficDetailBucket` มีแต่ map การอ่านต้องอยู่ใต้ `RLock` ตลอดช่วง
   aggregate **ป้องกัน:** ห้าม copy `s.buckets` ออกมาวนทีหลัง (คำเตือน **:821-834**), ต้องรัน `-race`
7. **ห้าม persist / ห้าม migration** — `git diff --stat` ต้องไม่มี `backend/internal/db/`
8. **RAM** — เปลี่ยน `observed` เป็น dirBytes = +8 ไบต์/bucket × 288 ≈ ไม่มีนัยสำคัญ
9. **frontend mock branch ต้องอัปเดตพร้อม type** — ลืมแล้ว `yarn build` (tsc) พัง หรือหน้ากราฟ
   ว่างในโหมด mock (Caution เดิมข้อ 11 ของแผน PR #117)
10. **การย้าย `CHART_BG_CLASSES`/`fmtBytes`** (T-08A) แตะ `Dashboard.tsx` ด้วย — ห้ามเปลี่ยน
    ค่า/ลำดับสี หรือสูตรปัดเศษของ `fmtBytes` ไม่งั้นการ์ด Protocol Breakdown/ตัวเลขทั้ง Dashboard
    เปลี่ยนโดยไม่ได้ตั้งใจ (ทั้งสองไฟล์มีสำเนา `fmtBytes` ที่ **เหมือนกันเป๊ะอยู่แล้ว** — ตรวจซ้ำ
    ก่อนย้าย ถ้าไม่เหมือนห้ามรวมเป็นตัวเดียวแล้วรายงานกลับมา)
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
15. **(ใหม่) doc comment ที่ `traffic_stats.go:104-108` จะกลายเป็นคำโกหกทันทีที่ T-02 ผ่าน**
    ถ้าไม่แก้ **ป้องกัน:** T-02 ข้อ 9 บังคับให้แก้ในคอมมิตเดียวกัน — reviewer ต้องเช็กข้อนี้
16. **(ใหม่) นิยาม window ไม่ใช่ "ตามเวลา"** — `1h` = 12 ก้อนท้ายสุด, `24h` = ทั้ง ring
    (`:847-855`, `:930-939`) ทั้งคู่กินเวลาจริงได้กว้างกว่าชื่อของมันเมื่อมีช่วงเงียบ
    **ห้ามแก้ตรรกะนี้ในแผนนี้** (จะเปลี่ยนตัวเลขของทุกการ์ดที่มีอยู่) — ให้ series ปรับตัวเข้าหามันแทน
    ด้วยกฎ carry (§2.5) **ป้องกัน:** เทสต์ T-04 ข้อ 7 + คำถามข้อ 6 ที่ §8
17. **(ใหม่) การ์ด Top 5 ในโหมด mock จะไม่มี "Other" ให้เห็น** ถ้าไม่แก้ `mockTopHosts` ตาม T-06
    ข้อ 3 → QA จะเช็กเกณฑ์ §6 ข้อนั้นไม่ได้เลย **ป้องกัน:** ทำ T-06 ข้อ 3 ให้ครบ
18. **(ใหม่) ความหมาย up/down สองแบบในหน้าเดียวกัน** — กราฟ = LAN-relative,
    `TopHost.bytesUp/bytesDown` (บรรทัด `↑ ↓` ในการ์ด Top 5) = flow-relative ตาม
    `model/statistics.go:20-26` → แถวที่เป็น "source ฝั่งอินเทอร์เน็ต" จะอ่านสวนทางกับกราฟ
    **ป้องกันในแผนนี้:** เขียน tooltip/คำอธิบายกราฟให้ชัดว่า "Upload = ออกจาก LAN,
    Download = เข้าสู่ LAN" และ **ห้ามแก้ semantics ของ `TopHost`** (เป็นคำถามข้อ 7 ที่ §8)

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [ ] T-01 `model/statistics.go`: `BandwidthPoint` + `TrafficStatistics.Series`
- [ ] T-02 `service/traffic_stats.go`: `lanFlipFor`, `flowSampleState.lanFlip`,
      `bucket.observed → dirBytes`, `processFlows`, `poll`, `onFlowEnd`, `addBucket`,
      `GetTrafficDetail`/`GetTrafficBreakdown` คงเดิม, **แก้ doc comment :104-108** 🔒
- [ ] T-03 `TrafficBreakdown.Series` (12/288 จุด, zero-fill, carry, ไม่ downsample,
      **ใน RLock เดียวกัน**) + wire `Series: breakdown.Series` ใน `GetStatistics`
- [ ] T-04 เทสต์ 8 เคส + แก้ call site `addBucket` 2 จุด; `go build ./... && go test -race ./...`

**Docs / Frontend**
- [ ] T-05 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (diff ตรงกันเป๊ะ)
- [ ] T-06 `services/statisticsService.ts` (type + mock series 12/288 จุด + percent เทียบ
      `observedBytes` เพื่อให้ "Other" ทดสอบได้)
- [ ] T-08A `lib/chartColors.ts`, `lib/formatBytes.ts`, `components/statistics/HostCells.tsx`
      (ย้ายอย่างเดียว ห้ามเปลี่ยนพฤติกรรม) — **ต้องเสร็จก่อน T-07/T-08B**
- [ ] T-07 `components/statistics/BandwidthTrendCard.tsx` (ไฟล์ใหม่, ปรับจูน 288 จุด)
- [ ] T-08B `components/statistics/TopHostsShareCard.tsx` (ไฟล์ใหม่)
- [ ] T-09 `pages/StatisticsOverview.tsx` เลย์เอาต์ 2/3 + 1/3 (แถวใหม่อยู่นอก ternary `hasData`)
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] T-10 (optional) เอกสาร

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ — skill `verify`)**
- [ ] `-mock=true`: `/statistics/overview` แสดงแถวบน = กราฟ 2/3 + การ์ด Top 5 1/3 ในแถวเดียวกัน
      บนจอกว้าง (≥1024px) และ stack เป็นคอลัมน์เดียวที่ ~768px โดยไม่มี overflow แนวนอน
- [ ] กราฟมีทั้งเส้น Download/Upload ที่ **ไม่ทับกันเป๊ะ**, แกน X เรียงเวลาถูกต้องและ label
      ไม่ทับกัน (~8-12 label), tooltip แสดงหน่วยที่อ่านได้ (KB/MB/GB ตามขนาด ไม่ใช่ "0.00G" ทุกจุด)
      และหัวการ์ดเปลี่ยนตาม window ที่เลือก
- [ ] สลับ window 1h ↔ 24h: **จำนวนจุด 12 ↔ 288** (นับจาก JSON) และยอดรวม 24h ≥ 1h เสมอ
- [ ] ตรวจ JSON ดิบด้วย `curl`: `Σ series[].bytes == observedBytes`, ทุกจุด
      `bytesUp + bytesDown == bytes`, `ts` ห่างกันช่องละ 5 นาทีเป๊ะไม่มีช่องหาย/ซ้ำ
      (ทำซ้ำ 3 ครั้งติดขณะมี traffic วิ่งจริง เพื่อจับ race ตาม §5 ข้อ 3)
- [ ] **วัดขนาด/ความลื่นของ 24h (มติข้อ 5):** `curl -s … | wc -c` ของ window 24h ≈ 24-27 KB
      และเปิดหน้าค้างที่ 24h แล้วสลับแท็บ/scroll ต้องไม่กระตุกชัดเจนบนเครื่องทดสอบ
      (ถ้ากระตุก ให้แก้ตาม §5 ข้อ 13 เท่านั้น — ห้ามลดความละเอียดที่ API)
- [ ] การ์ด Top 5: มี ≤5 แถว เรียงตาม byte รวมตรงกับ 5 แถวแรกของ Top Source Hosts เป๊ะ,
      **ไม่กรอง LAN/Internet** (badge ยังอยู่), percent ตรงกับการ์ด Top Source Hosts ทุกแถว
      (ไม่ถูก normalize), มี segment "Other" สีเทาปิดท้ายเมื่อผลรวม < 100%
- [ ] หน้าตา/สัดส่วน/legend ของการ์ด Top 5 ตรงกับ Protocol Breakdown ของ Dashboard
- [ ] เคสว่าง: ring ว่าง/หลังบูตใหม่ → กราฟยังเรนเดอร์ (empty state ของตัวเอง) และ **ไม่ถูก
      การ์ด empty-state ของหน้าซ่อนทิ้ง**; การ์ด Top 5 แสดง empty state ไม่ crash
      (`observedBytes = 0`)
- [ ] dark/light สลับแล้วอ่านออกทั้งคู่, ไม่มี `shadow-*`/`backdrop-blur-*`/สีดิบใน diff
- [ ] Dashboard (แท็บ Overview + Detailed) ไม่มี regression — Protocol Breakdown สีเดิม,
      ตัวเลขทุกใบเหมือนเดิมหลังย้าย `fmtBytes` (T-08A),
      `/api/dashboard/traffic-detail` JSON เหมือนก่อนแก้ทุกฟิลด์
- [ ] Statistics Overview เดิม (Top Sources/Destinations/Conversations/Denied/Domains)
      ไม่มี regression หลังย้าย `HostLabel`/`UpDownLine` ออกไปเป็น shared
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

> **หมายเหตุการทวนแผนรอบที่ 2:** มติทั้ง 5 ข้อข้างต้นทวนกับโค้ดจริงแล้ว **ทำได้ทั้งหมด ไม่มีข้อไหน
> ขัดกับสถาปัตยกรรมปัจจุบัน** — มติข้อ 3 มีเงื่อนไขเพิ่มว่าโหมด mock ต้องปรับตาม T-06 ข้อ 3
> ไม่งั้นทดสอบส่วน "Other" ไม่ได้ (ไม่ได้เปลี่ยนมติ แค่เพิ่มงานให้ทดสอบได้)

## 8. คำถามใหม่ที่พบตอนทวนแผนรอบที่ 2 — **มติแล้ว (ยืนยันโดยเจ้าของโปรเจกต์ 2026-08-01)**

> เจ้าของโปรเจกต์ยืนยัน default ทั้งสองข้อตามที่แผนเสนอ — **lock แล้ว ห้ามเปลี่ยนเองระหว่างทำ**
> เช่นเดียวกับมติ §7

| # | ประเด็น | ทางเลือก | **มติ** |
|---|---|---|---|
| 6 | เมื่อ ring กินเวลากว้างกว่า window (มีช่วงเงียบนาน) ควรทำอย่างไรกับ bucket ที่หลุดแกนเวลา | (ก) **carry เข้าจุดซ้ายสุด** — ผลรวมตรงกับ `observedBytes` เสมอ แต่จุดแรกอาจดูสูงผิดปกติ · (ข) ตัดทิ้งจาก series — กราฟสวย แต่ `Σ series < observedBytes` (ขัดเป้าหมาย §0) · (ค) เปลี่ยนนิยาม window ของทั้งระบบเป็น "ตามเวลา" — ถูกต้องที่สุด แต่ **เปลี่ยนตัวเลขของทุกการ์ดที่มีอยู่** = งานแยกที่ต้องรีวิวใหม่ทั้งหน้า | **(ก) carry เข้าจุดริมสุด** — รักษาความสอดคล้องของตัวเลขในหน้าเดียวกันตามเป้าหมาย §0 และเป็นเคสที่เกิดยากบนเครื่องที่มี traffic จริง |
| 7 | ในหน้าเดียวกันมี up/down สองนิยาม (กราฟ = LAN-relative, `TopHost.bytesUp/Down` = flow-relative ตาม PR #117) | (ก) ปล่อยไว้ + อธิบายด้วยข้อความ/tooltip · (ข) เปลี่ยน `TopHost` ให้เป็น LAN-relative ด้วย — สอดคล้องกันทั้งหน้า แต่ **เป็น breaking change ของ API ที่เพิ่ง lock ไปใน PR #117** และกระทบทั้ง Top Sources/Destinations/Conversations | **(ก) ปล่อยไว้ + tooltip อธิบาย** — แผนนี้ไม่แตะ semantics เดิม (นอกขอบเขต §0) และเขียนคำอธิบายกำกับกราฟแทน (§5 ข้อ 18) |
