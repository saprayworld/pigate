# Statistics: control bar ใหม่ + ขยายช่วงเวลาเป็น 7 ระดับ (15m/30m/1H/3H/6H/12H/24H)

> เอกสารแผนงานสำหรับ: (A) จัดลำดับ "ชุดควบคุมข้อมูล" ด้านบนของหน้า Statistics ทั้ง 6 หน้า
> ให้เป็น Badge → ปุ่มช่วงเวลาแบบ segmented control → ปุ่ม Refresh (จอเล็กเหลือแค่ไอคอน)
> และ (B) ขยายช่วงเวลาจาก 2 ค่า (1h/24h) เป็น **7 ค่า: 15m, 30m, 1h, 3h, 6h, 12h, 24h**
> ทั้ง frontend และ backend
>
> วันที่เขียน: 2026-08-03 · แก้ไขครั้งที่ 1: 2026-08-03 (เจ้าของตอบ D-1…D-4 + ระบุ branch แล้ว — ล็อกขอบเขต)
> แก้ไขครั้งที่ 2: 2026-08-03 (ai-developer ทำครบ T-01…T-14 + ai-qa ตรวจ PASS — บันทึกสถานะปิดงาน)
> อ้างอิงโค้ด: `main` @ `12a61bc` + งานความเร็วที่อยู่บน branch `feat/statistics-traffic-speed`
>
> **Branch ที่ใช้: `feat/statistics-traffic-speed` (branch เดิม ทำต่อบนนั้นเลย)**
> เจ้าของสั่งชัดเจนว่า **ห้ามแตก branch ใหม่** และห้ามแตกจาก `main` — เพื่อเลี่ยง merge conflict
> กับงาน speed (`TrafficTrendCard.tsx`, คอลัมน์ Speed, `fmtRate`, `CurrentRates()`) ที่ยังไม่ merge
> ยังคงกติกาเดิม: ห้าม push `main` (โค้ด), ห้าม commit เว้นแต่เจ้าของสั่ง, เข้า PR เท่านั้น
>
> **สถานะ: โค้ดครบทุก Task (T-01…T-14) และ ai-qa ตรวจ PASS แล้ว ณ 2026-08-03**
> build / vet / `go test ./... -race` / lint / `build.sh` เขียวทั้งหมด · openapi สองไฟล์ diff เท่ากันเป๊ะ ·
> ทดสอบด้วย curl จริงครบทั้ง 7 window ยืนยัน `sum(series) == observed` ทุกกรณี ·
> เคส case-sensitivity/malformed input ผ่าน · responsive 3 ขนาดจอผ่าน · dark/light ผ่าน ·
> ฟีเจอร์ speed เดิมไม่ regress · ไม่พบบั๊ก severity สูง/กลาง · ไม่มี loop แก้
> มี known-limitation ที่ **ไม่บล็อก** (ดู §7)
> **เหลือเฉพาะ: เจ้าของโปรเจกต์ตรวจบนเบราว์เซอร์จริง แล้วสั่ง commit + เปิด PR**
> (ai-tech-lead/ai-developer/ai-qa ไม่ commit เอง ตามข้อตกลงของโปรเจกต์)

## 0. การตัดสินใจของเจ้าของโปรเจกต์ (ล็อกแล้ว ห้ามเปิดใหม่)

| # | คำถาม | คำตอบที่ล็อก |
|---|---|---|
| D-1 | ความละเอียดของกราฟที่ช่วงสั้น | **ยอมรับบัคเก็ต 5 นาทีคงที่** — 15m = กราฟ 3 จุด, 30m = 6 จุด, จุดท้ายยังเก็บไม่ครบช่วง **ห้ามเพิ่ม ring ละเอียด 1 นาที** และห้ามแก้ปัญหา "กราฟหยาบ/ตกท้าย" ด้วยการตัดจุดสุดท้ายทิ้ง |
| D-2 | `/api/dashboard/traffic-detail` ใช้ `?window=` ตัวเดียวกัน | **ให้รองรับ 7 ค่าด้วย** ผ่าน helper ตัวเดียวกันทั้งไฟล์ `handlers.go` แต่ **ห้ามแตะหน้า Dashboard** — UI ของ Dashboard ยังมี 2 ปุ่มเดิม (`dashboardService.ts` ก็ไม่แตะ) |
| D-3 | ค่า default เมื่อไม่ส่ง `window` มา | **`1h`** เหมือนเดิม และค่าที่ไม่รู้จักยัง fallback เป็น `1h` แบบเงียบ ๆ (ห้ามตอบ 400) |
| D-4 | ข้อความบนปุ่ม vs ค่าจริง | label บนปุ่มคือ `15m, 30m, 1H, 3H, 6H, 12H, 24H` (ตัว H ใหญ่) แต่ **ค่าที่ส่งขึ้น URL/API ต้องตัวเล็กเสมอ** (`15m, 30m, 1h, 3h, 6h, 12h, 24h`) — และต้องมี **เทสต์บังคับแยก label/value** ตาม T-07 ข้อ 6 |

**นอกขอบเขต (ห้าม ai-developer ทำ)**

- ไม่แตก branch ใหม่ — ทำงานบน `feat/statistics-traffic-speed` เท่านั้น
- ไม่เพิ่ม ring ใหม่ ไม่เปลี่ยน `trafficDetailBucketSpan` (5 นาที) ไม่เปลี่ยน `trafficDetailBucketMax`/`denyBucketMax`/`domainBucketMax` (288)
- ไม่เพิ่ม endpoint/route ใหม่ ไม่เพิ่ม config key ใหม่ ไม่มี migration ไม่แตะ kernel layer
- ไม่แตะหน้า Dashboard, `frontend/src/services/dashboardService.ts`, `system_status.go`, SSE metrics
- ไม่แตะโค้ดส่วนความเร็ว (rate accumulator / `CurrentRates()` / `applyRates` / คอลัมน์ Speed) ที่มาจากแผน `statistics-traffic-speed-plan.md`
- ไม่เปลี่ยนความหมาย/ชื่อฟิลด์เดิมของ response ใด ๆ (เพิ่มได้เฉพาะ "ค่าที่เป็นไปได้" ของ `window`)
- ไม่เปลี่ยนพฤติกรรมของ `window=1h` และ `window=24h` แม้แต่ไบต์เดียว (regression guard หลักของแผนนี้)

## 1. สถานะปัจจุบัน (สำรวจแล้ว ณ วันเขียนแผน — ก่อนลงมือ)

| ส่วน | สถานะ | ไฟล์:บรรทัด (~) |
|---|---|---|
| ค่าคงที่ window ฝั่ง service | `trafficWindow1h = "1h"`, `trafficWindow24h = "24h"`, `trafficWindow1hBuckets = 12` | `backend/internal/service/traffic_stats.go:72-76` |
| ring ของ traffic | 5 นาที × **288 บัคเก็ต = 24 ชม.** ตัดท้ายด้วย `s.buckets[len-288:]` | `traffic_stats.go:29-30, 1006-1007` |
| ring ของ deny (NFLOG) | `denyBucketSpan = 5m`, `denyBucketMax = 288` | `statistics.go:33-34` |
| ring ของ DNS | `domainBucketSpan = 5m`, `domainBucketMax = 288` | `dns_query_stats.go:22-23` |
| จุดที่เทียบ window แบบ hardcode (service) | `GetTrafficDetail` 1092-1094, 1103-1108 · `getTrafficBreakdown` 1218-1220, 1234-1237, 1273-1281 | `traffic_stats.go` |
| " (ต่อ) | `denySnapshot` 159-167 · `GetStatistics` 188-190 | `statistics.go` |
| " (ต่อ) | `dnsWindowBuckets` 228-235 · `GetDNSQueryStatistics` 304-305 · `GetDNSDomainClients` 373-374 · `GetDNSClientDomains` 432-433 | `dns_query_stats.go` |
| " (ต่อ) | `GetTrafficTopHosts` 76-78 · `GetTrafficHostDetail` 150-152 | `statistics_traffic.go` |
| whitelist ฝั่ง API | `if window != "24h" { window = "1h" }` ซ้ำ 3 ที่ (`HandleGetTrafficDetail` 406-409, `HandleGetStatistics` 421-424, `dnsStatsWindow` 433-438) | `backend/internal/api/handlers.go` |
| kernel layer | **ไม่มี** จุดใดอิง 1h/24h (`kernel/dhcp_server.go:72` `"24h"` คือ lease time ของ dnsmasq คนละเรื่อง — ห้ามแตะ) | — |
| model | คอมเมนต์ `// "1h" \| "24h"` และ "12 for 1h, 288 for 24h" หลายจุด | `model/statistics.go:128,142,200,262,282,384-385` · `model/types.go:683` |
| openapi | `enum: [1h, 24h]` + คำอธิบาย ซ้ำ **7 จุด** ในไฟล์ละชุด (บรรทัด 363, 401, 433, 497, 549, 598, 648) | `docs/openapi.yaml`, `frontend/public/openapi.yaml` |
| type ฝั่ง frontend | `StatsWindow = "1h" \| "24h"` + `useStatsWindow()` (อ่านจาก `?window=`) + `StatsWindowSelect` (`<Select>` กว้าง `w-28`) | `frontend/src/components/statistics/DnsStatsShared.tsx:29-74` |
| union `"1h" \| "24h"` กระจายในเซอร์วิส | `statisticsService.ts:94,218,307` · `trafficStatisticsService.ts:18,81,238,280,318` · `dnsStatisticsService.ts:23,34,45,91,148,193` · `dashboardService.ts:156,365` (ไฟล์สุดท้าย **ห้ามแตะ**) | — |
| mock helper ที่ผูกกับ 2 ค่า | `mockWindowSeconds()` (`24h→86400 : 3600`), `mockBandwidthSeries()` (`n = 24h?288:12`), `scale = window==="24h"?18:1` (โผล่ 6 ที่) | ตามไฟล์ข้างบน |
| label กราฟ | `windowLabel = statsWindow === "24h" ? "24 ชม. ล่าสุด" : "1 ชม. ล่าสุด"` | `components/statistics/TrafficTrendCard.tsx:114` |
| control bar ปัจจุบันของทั้ง 6 หน้า | ลำดับคือ **Select → Badge → Refresh(มี label เสมอ)** ครอบด้วย `div.flex items-center gap-2` | `StatisticsOverview.tsx:412-419`, `StatisticsTraffic.tsx:240-246`, `StatisticsTrafficHost.tsx:390-…`, `StatisticsDns.tsx:81-93`, `StatisticsDnsDomain.tsx:94-…`, `StatisticsDnsClient.tsx:101-…` |
| หมายเหตุ | 3 หน้า DNS **ไม่มี** `AccuracyBadge` (มีเฉพาะ Overview/Traffic/TrafficHost) → ลำดับใหม่ต้องไม่พังเมื่อไม่มี badge | — |
| ต้นแบบ segmented control | `<div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">` + `<button>` toggle ด้วย `bg-primary text-primary-foreground` / `text-muted-foreground hover:bg-muted hover:text-foreground` | `pages/Interfaces.tsx:1635-1656` |
| breakpoint มาตรฐานของโปรเจกต์สำหรับซ่อน label ปุ่ม | `hidden sm:inline` (ใช้อยู่แล้วใน `pages/ApiDocs.tsx:51`) | — |

**สรุปหนึ่งบรรทัด:** ทั้งสาม ring เก็บ 5 นาที × 288 = 24 ชม. เท่ากันหมด ดังนั้นทุก window ใหม่คือ
"เอา N บัคเก็ตท้ายสุด" โดย `N = นาทีของ window / 5` (15m→3, 30m→6, 1h→12, 3h→36, 6h→72, 12h→144,
24h→288 = ทั้ง ring พอดี) — **ไม่ต้องเพิ่ม storage, ไม่ต้องเพิ่ม poll, ไม่ต้อง migration**
งานจริงคือ "ถอด hardcode 2 ค่า ออกมาเป็นตารางเดียว" แล้วไล่แก้ทุกจุดที่อ้างถึงมัน

## 2. แนวทางเทคนิค (การตัดสินใจเชิงออกแบบ)

### 2.1 backend รับ window เป็น string enum เหมือนเดิม (ไม่เปลี่ยนเป็น int นาที)

**ตัดสินใจ: คงเป็น string enum 7 ค่า** เหตุผล:

1. ทุกชั้นเก็บ `Window string` ลง response อยู่แล้ว (`model.TrafficStatistics.Window` ฯลฯ) —
   เปลี่ยนเป็น int = เปลี่ยนรูปร่าง response ทุก endpoint = แตะหน้าเว็บทุกหน้าโดยไม่ได้อะไรเพิ่ม
2. URL query `?window=1h` เป็น contract ที่หน้า drill-down/ปุ่ม Back พึ่งพาอยู่ (`useStatsWindow`)
   และมี bookmark/ลิงก์เก่าที่ผู้ใช้อาจเปิดค้างไว้ — string enum ทำให้ค่าเก่ายังใช้ได้เป๊ะ
3. int นาทีเปิดช่องให้ค่าอิสระ (เช่น `?window=7`) ที่ต้อง validate/clamp เพิ่ม และทำให้ series
   ยาวไม่คงที่ ซึ่งขัดกับสัญญา "Series มีความยาวคงที่ต่อ window" ที่ model/openapi เขียนไว้ชัด
4. enum แคบ = พื้นที่อินพุตแคบ = ปลอดภัยกว่าโดยธรรมชาติ (แม้ endpoint นี้จะไม่ใช่ sensitive)

**รูปแบบที่ให้ implement (จุดเดียวใน `traffic_stats.go` แล้วให้ทุกไฟล์เรียกใช้):**

```go
// statsWindowBuckets: ตารางเดียวที่กำหนดว่า window ไหน = กี่บัคเก็ต 5 นาที
// (ห้ามคำนวณ N ซ้ำที่อื่น ห้ามมี map ชุดที่สอง)
var statsWindowBuckets = map[string]int{
    "15m": 3, "30m": 6, "1h": 12, "3h": 36, "6h": 72, "12h": 144, "24h": 288,
}

func normalizeStatsWindow(w string) string   // ไม่รู้จัก -> "1h" (D-3)
func statsWindowBucketCount(w string) int    // normalize ให้ในตัว
func lastNBuckets[T any](ring []T, n int) []T // ตัดท้าย n ตัว ป้องกัน n > len
```

- `trafficWindow1h` / `trafficWindow24h` / `trafficWindow1hBuckets` **ให้คงไว้** (เทสต์เดิมหลายสิบจุด
  อ้างถึง) แต่ให้ค่าตรงกับตารางเดียวกันเสมอ จะได้ไม่มีทางเพี้ยน
- **จุดสำคัญที่ต้องพิสูจน์ว่าไม่ regress:** วันนี้สาขา 24h เขียนว่า `windowBuckets = s.buckets`
  (ทั้ง ring) ไม่ใช่ "288 ตัวท้าย" — ทั้งสองอย่างให้ผลเท่ากันเพราะ ring ถูกตัดที่ 288 อยู่แล้ว
  (`traffic_stats.go:1006`) การเปลี่ยนมาใช้ `lastNBuckets(ring, 288)` จึงเป็น no-op เชิงพฤติกรรม
  — ให้ ai-developer เขียนคอมเมนต์กำกับข้อนี้ไว้ตรงจุด และมีเทสต์ยืนยัน (T-07)

### 2.2 สิ่งที่ต้องตรวจว่าไม่พังเมื่อ N เล็กลง

| ของ | ผลกระทบ | สรุป |
|---|---|---|
| `Observed` (ตัวหาร percent) | รวมเฉพาะบัคเก็ตใน window อยู่แล้ว → เล็กลงตาม window | ถูกต้องโดยอัตโนมัติ ไม่ต้องแก้ |
| `Series` / `HostSeries` | ความยาว = N; แกนเวลาสร้างจาก N เหมือนเดิม; กติกา carry-to-edge ยังคงเดิม → `sum(Series[].Bytes) == Observed` ยังจริง | ไม่ต้องแก้สูตร |
| `Truncated` | เช็ก cap รายบัคเก็ตภายใน window → window สั้นลงยิ่งมีโอกาส truncate น้อยลง | ไม่ต้องแก้ |
| `Accuracy` | มาจาก `eventsActive` ไม่เกี่ยวกับ window | ไม่ต้องแก้ |
| `DeniedEvents`, `DNSQueries` | รวมเฉพาะบัคเก็ตใน window | ไม่ต้องแก้ |
| ความเร็ว (`rateBps*`) | เป็น snapshot ของ poll ล่าสุด **ไม่ผูกกับ window เลย** | ไม่ต้องแก้ — และห้ามไปผูกกับ window |
| จุดสุดท้ายของ series ที่ยังเก็บไม่ครบ 5 นาที | ที่ 15m คิดเป็น 1 ใน 3 ของกราฟ → กราฟ "ตกท้าย" เห็นชัดกว่าเดิมมาก | เจ้าของยอมรับแล้ว (D-1); สูตร `lastBucketSpanSeconds` เดิมรองรับอยู่แล้วในโหมด speed — ห้ามแก้ด้วยการตัดจุดสุดท้ายทิ้ง (§6 ข้อ 2) |

### 2.3 frontend: ย้าย type/ตาราง window ออกจาก component ไปที่ `lib/`

ปัจจุบัน `StatsWindow` อยู่ใน `components/statistics/DnsStatsShared.tsx` แต่ **service layer**
(`statisticsService.ts` ฯลฯ) ต้องใช้ union เดียวกัน — ให้ service import type จาก component ถือเป็น
layering ที่ผิดทาง จึงกำหนดให้:

- สร้าง `frontend/src/lib/statsWindow.ts` (pure, ไม่มี JSX/ไม่มี React) เป็น **แหล่งความจริงเดียว**
  ของ `StatsWindow` type, ลำดับปุ่ม, label, จำนวนนาที, จำนวนจุดของ series และ mock scale
- `DnsStatsShared.tsx` `re-export type { StatsWindow }` ต่อไป เพื่อให้ import เดิมของ 6 หน้าไม่พัง

### 2.4 UI: segmented control 7 ปุ่ม + Refresh แบบไอคอนบนจอเล็ก

- โครง markup ลอกจาก Addressing Mode (`Interfaces.tsx:1635-1656`) แต่ **บีบขนาดลง** เพราะ 7 ปุ่ม
  ไม่ใช่ 2: `px-2 py-1 text-[11px]` (ของเดิม `px-4 py-1.5 text-xs`)
- ห่อด้วย `max-w-full overflow-x-auto` เพื่อให้จอแคบมากเลื่อนแนวนอนได้แทนที่จะดันปุ่ม Refresh ตกขอบ
  และเติม `flex-wrap` ให้แถบควบคุมของทุกหน้า เพื่อให้ตกบรรทัดใหม่ได้อย่างสวยงามก่อนถึงขั้นต้องเลื่อน
- ปุ่ม Refresh: `<span className="hidden sm:inline">Refresh</span>` (breakpoint `sm` — เป็น pattern
  ที่โปรเจกต์ใช้อยู่แล้วใน `ApiDocs.tsx:51`) พร้อม `title="Refresh"` + `aria-label="Refresh"`
  เพื่อไม่ให้ปุ่มไร้ชื่อสำหรับ screen reader ตอนซ่อน label
- a11y ของ segmented control: `role="group"` ที่กล่องนอก + `aria-pressed={active}` ที่ปุ่มแต่ละอัน
- สี: ใช้เฉพาะตัวแปรธีม (`bg-primary`, `text-primary-foreground`, `bg-muted`, `border-border`,
  `text-muted-foreground`) — ห้าม `shadow-*` / `backdrop-blur-*` / คลาสสีดิบ (CLAUDE.md + rules_of_work.md)

## 3. Task list (ทำครบทุก Task แล้ว — ai-qa ตรวจ PASS)

> **Branch: `feat/statistics-traffic-speed` (branch เดิม ห้ามแตกใหม่ ห้าม rebase/reset)**
> ห้าม push `main`, ห้าม commit เว้นแต่เจ้าของสั่ง, งานเข้า PR เท่านั้น
> **ห้ามทดสอบทีละ Task** — ทำ T-01…T-14 ให้ครบก่อน แล้วค่อยทดสอบรวมทีเดียวตาม §5

| Task | สถานะ |
|---|---|
| T-01 … T-14 | **DONE ทั้งหมด** (ai-developer) · **QA PASS** (ai-qa, ไม่มี loop แก้) |

```json
[
  {
    "task_id": "T-01",
    "title": "service: ตารางกลางของ window + helper เลือกบัคเก็ต",
    "layer": "service",
    "files": ["backend/internal/service/traffic_stats.go"],
    "instruction": "สร้าง 'แหล่งความจริงเดียว' ของ window ในไฟล์นี้ ตาม docs/ref/todo/statistics-window-granularity-plan.md §2.1: (1) เพิ่มตาราง statsWindowBuckets ที่ map window -> จำนวนบัคเก็ต 5 นาที คือ 15m:3, 30m:6, 1h:12, 3h:36, 6h:72, 12h:144, 24h:288 พร้อมคอมเมนต์ว่าค่ามาจาก นาที/trafficDetailBucketSpan และทุก ring ในโปรเจกต์ (traffic 288 / deny 288 / dns 288) เก็บย้อนหลัง 24 ชม. เท่ากันหมด จึงไม่ต้องเพิ่ม storage ใด ๆ (2) เพิ่ม normalizeStatsWindow(w string) string ที่คืน '1h' เมื่อไม่รู้จัก (แผน §0 D-3 — พฤติกรรม fallback เดิมทุกประการ ห้าม error) และ statsWindowBucketCount(w string) int ที่ normalize ให้ในตัว (3) เพิ่ม generic helper lastNBuckets[T any](ring []T, n int) []T ที่ตัดท้าย n ตัวและกัน n > len(ring) — จะได้ไม่ต้องเขียน if len < n ซ้ำ 5 ที่ (4) คง const trafficWindow1h/trafficWindow24h/trafficWindow1hBuckets ไว้ (เทสต์เดิมหลายสิบจุดอ้างถึง) แต่ให้ trafficWindow1hBuckets มีค่าตรงกับตารางเสมอ (5) แก้ GetTrafficDetail และ getTrafficBreakdown ให้เลิก hardcode: ใช้ normalizeStatsWindow แทน if window != trafficWindow24h, ใช้ statsWindowBucketCount(window) เป็นทั้ง n ของแกนเวลา series และจำนวนบัคเก็ตที่เลือกผ่าน lastNBuckets — n ต้องมาจากการเรียกครั้งเดียวและเป็นตัวแปรเดียวกันทั้งสองที่เสมอ (invariant sum(Series[].Bytes) == Observed พังเงียบ ๆ ทันทีถ้าไม่ตรงกัน) (6) สาขา 24h เดิมใช้ทั้ง ring (windowBuckets = s.buckets) การเปลี่ยนมาเป็น lastNBuckets(s.buckets, 288) เป็น no-op เพราะ ring ถูกตัดที่ trafficDetailBucketMax อยู่แล้วที่บรรทัด ~1006 — ให้เขียนคอมเมนต์อธิบายและผูกสองค่านี้เข้าหากันไว้ตรงจุด (7) ห้ามเปลี่ยน trafficDetailBucketSpan/trafficDetailBucketMax, ห้ามแตะ poll()/addBucket/rate accumulator/CurrentRates(), ห้ามเรียก kernel เพิ่ม, ห้ามเปลี่ยนพฤติกรรมของ window '1h' และ '24h' แม้แต่นิดเดียว",
    "acceptance": [
      "cd backend && go build ./... && go vet ./... ผ่าน",
      "go test ./internal/service/... ผ่านทั้งหมดโดยไม่ต้องแก้เทสต์เดิม",
      "ไม่มีการเทียบสตริง \"1h\"/\"24h\" หลงเหลือใน GetTrafficDetail/getTrafficBreakdown",
      "n ที่ใช้สร้างแกน series กับจำนวนบัคเก็ตที่เลือก มาจากการเรียก statsWindowBucketCount ครั้งเดียวกัน",
      "โค้ดส่วนความเร็ว (rate accumulator / CurrentRates) ไม่ถูกแก้แม้แต่บรรทัดเดียว"
    ],
    "depends_on": [],
    "status": "done"
  },
  {
    "task_id": "T-02",
    "title": "service: deny ring (statistics.go) ใช้ helper กลาง",
    "layer": "service",
    "files": ["backend/internal/service/statistics.go"],
    "instruction": "แก้ denySnapshot (บรรทัด ~159-167) และ GetStatistics (~188-190) ให้ใช้ normalizeStatsWindow / statsWindowBucketCount / lastNBuckets จาก T-01 แทน if window == trafficWindow1h { ... } else { ทั้ง ring } และ if window != trafficWindow24h { window = trafficWindow1h }. อัปเดตคอมเมนต์เหนือ denySnapshot ที่เขียนว่า 'trafficWindow1hBuckets trailing buckets for 1h, the whole ring for 24h' ให้ตรงกับกติกาใหม่ (N บัคเก็ตท้ายสุดตามตารางกลาง). ห้ามเปลี่ยนโครงสร้าง response, ห้ามเปลี่ยน denyBucketSpan/denyBucketMax หรือ cap ใด ๆ, ห้ามแตะ RecordFirewallLog (hot path ที่ต้อง O(1) ไม่มี I/O)",
    "acceptance": [
      "go build ./... && go vet ./... ผ่าน",
      "เทสต์เดิมของ statistics_test.go ผ่านโดยไม่ต้องแก้ความหมาย",
      "ไม่เหลือ literal \"1h\"/\"24h\" ในไฟล์นี้นอกจากในคอมเมนต์"
    ],
    "depends_on": ["T-01"],
    "status": "done"
  },
  {
    "task_id": "T-03",
    "title": "service: DNS ring (dns_query_stats.go) ใช้ helper กลาง",
    "layer": "service",
    "files": ["backend/internal/service/dns_query_stats.go"],
    "instruction": "แก้ 4 จุด: dnsWindowBuckets (~227-236) ให้เลือก lastNBuckets(s.dns.buckets, statsWindowBucketCount(window)) แทนกติกา '1h -> 12 ตัวท้าย, อย่างอื่น -> ทั้ง ring' และ GetDNSQueryStatistics (~304), GetDNSDomainClients (~373), GetDNSClientDomains (~432) ให้ใช้ normalizeStatsWindow แทน if window != trafficWindow24h. ระวัง: ทั้งสามเมธอดมี early-return path ตอน dns.enabled == false ที่ใส่ Window ลง response ด้วย — ต้องเป็นค่าที่ normalize แล้วเหมือนกันทั้งสองเส้นทาง. ห้ามแตะ RecordDNSEvent/recordDomainQuery (hot path, ต้อง O(1) ไม่มี I/O ไม่ log ชื่อโดเมน), ห้ามเปลี่ยน domainBucketSpan/domainBucketMax หรือ cap maxPairs/maxClients",
    "acceptance": [
      "go build ./... && go vet ./... ผ่าน",
      "go test ./internal/service/... ผ่าน รวม TestStatisticsService_DNSDrilldown_WindowSelection เดิม",
      "เส้นทาง disabled และเส้นทางปกติคืนค่า window ที่ normalize แล้วเหมือนกัน"
    ],
    "depends_on": ["T-01"],
    "status": "done"
  },
  {
    "task_id": "T-04",
    "title": "service: สอง endpoint ของหน้า Traffic ใช้ helper กลาง",
    "layer": "service",
    "files": ["backend/internal/service/statistics_traffic.go"],
    "instruction": "แก้ GetTrafficTopHosts (~76-78) และ GetTrafficHostDetail (~150-152) ให้ใช้ normalizeStatsWindow แทน if window != trafficWindow24h. ห้ามแตะส่วนความเร็ว (CurrentRates/applyRates/currentRateUp/currentRateDown/rateSampledAt) เด็ดขาด — ความเร็วเป็น snapshot ของ poll ล่าสุด (~10 วิ) ไม่ผูกกับ window และต้องไม่ถูกทำให้ผูกกับ window, ห้ามเรียก breakdown ซ้ำรอบสอง, ห้ามแก้กติกาการนับ TotalBytes (เคส src==dst นับสองครั้ง)",
    "acceptance": [
      "go build ./... && go vet ./... ผ่าน",
      "go test ./internal/service/... ผ่าน",
      "โค้ดส่วน rate ไม่ถูกแก้แม้แต่บรรทัดเดียว"
    ],
    "depends_on": ["T-01"],
    "status": "done"
  },
  {
    "task_id": "T-05",
    "title": "api: รวม whitelist ของ ?window= ให้เหลือ helper เดียว (ครอบ traffic-detail ด้วย)",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go"],
    "instruction": "ตอนนี้มี whitelist ซ้ำ 3 ชุด (HandleGetTrafficDetail ~406-409, HandleGetStatistics ~421-424, dnsStatsWindow ~433-438) ให้ยุบเหลือ helper เดียว เช่น statsWindowParam(r *http.Request) string ที่อ่าน r.URL.Query().Get(\"window\") แล้วผ่าน whitelist 7 ค่า {15m,30m,1h,3h,6h,12h,24h} โดยค่าที่ไม่รู้จัก/ว่าง -> \"1h\" (แผน §0 D-3: fallback เงียบ ๆ ห้ามตอบ 400 เพราะจะทำให้ลิงก์/bookmark เก่าพัง). ให้ทุก handler ที่รับ window ใช้ helper ตัวนี้ทั้งหมด 7 จุด: /api/dashboard/traffic-detail, /statistics/traffic, /statistics/dns, /statistics/dns/domain, /statistics/dns/client, /statistics/traffic/hosts, /statistics/traffic/host. ตาม §0 D-2 เจ้าของยืนยันแล้วว่า traffic-detail ต้องรับ 7 ค่าเช่นกัน (UI ของ Dashboard ยังส่งแค่ 1h/24h — ห้ามแตะหน้า Dashboard และ dashboardService.ts). สำคัญ: whitelist ต้องอยู่ที่ชั้น api ต่อไป และชั้น service ยังต้อง normalize ซ้ำแบบ defense-in-depth เหมือนเดิม ห้ามถอดออกฝั่งใดฝั่งหนึ่ง. ห้ามเปลี่ยน signature ของ handler, ห้ามเพิ่ม query param ใหม่, ห้ามแตะ middleware/auth/rate limit",
    "acceptance": [
      "go build ./... && go vet ./... ผ่าน",
      "go test ./internal/api/... ผ่าน (รวมเทสต์เดิมที่คาดว่า window=evil / 99h -> 1h)",
      "ไม่มี if window != \"24h\" หลงเหลือใน handlers.go",
      "handler ทั้ง 7 จุดเรียก helper ตัวเดียวกัน"
    ],
    "depends_on": ["T-01"],
    "status": "done"
  },
  {
    "task_id": "T-06",
    "title": "model: อัปเดตคอมเมนต์/สัญญาของฟิลด์ window และความยาว series",
    "layer": "model",
    "files": ["backend/internal/model/statistics.go", "backend/internal/model/types.go"],
    "instruction": "แก้เฉพาะคอมเมนต์ ห้ามแก้ชื่อ/ชนิด/json tag ของฟิลด์ใด ๆ: (1) ทุกที่ที่เขียน // \"1h\" | \"24h\" (statistics.go:128,200,262 และ types.go:683) เปลี่ยนเป็นรายการ 7 ค่า (2) ทุกที่ที่เขียน 'Fixed length (12 for \"1h\", 288 for \"24h\")' (statistics.go:142,282,384-385) เปลี่ยนเป็นกติกาทั่วไปว่า ความยาว = จำนวนบัคเก็ต 5 นาทีของ window (15m=3, 30m=6, 1h=12, 3h=36, 6h=72, 12h=144, 24h=288) และยังคงสัญญาเดิมว่า sum(Series[].Bytes) == ObservedBytes เสมอ (3) ระบุเพิ่มว่า window ที่ไม่รู้จักจะ fallback เป็น 1h ไม่ใช่ error",
    "acceptance": [
      "go build ./... ผ่าน",
      "diff ของ task นี้เป็นคอมเมนต์ล้วน ไม่มีการเปลี่ยนโค้ด",
      "ไม่มีคอมเมนต์ที่ยังบอกว่ามีแค่ 2 ค่า หลงเหลือในสองไฟล์นี้"
    ],
    "depends_on": [],
    "status": "done"
  },
  {
    "task_id": "T-07",
    "title": "test: ครอบ 7 window ทั้ง service และ api + บังคับแยก label/value",
    "layer": "service",
    "files": [
      "backend/internal/service/traffic_stats_test.go",
      "backend/internal/service/statistics_traffic_test.go",
      "backend/internal/service/dns_query_stats_test.go",
      "backend/internal/api/handlers_test.go"
    ],
    "instruction": "เพิ่มเทสต์ใหม่ ห้ามแก้ความหมายของเทสต์เดิม: (1) table test ยืนยันความยาว Series/HostSeries ของทั้ง 7 window = 3,6,12,36,72,144,288 ตามลำดับ ทั้งจาก GetTrafficBreakdown และ GetTrafficHostDetail (2) เทสต์ regression ว่าเมื่อยัด bucket ชุดเดียวกัน ผลของ window '1h' และ '24h' เท่ากับก่อนแก้ทุกฟิลด์ที่สังเกตได้ (Observed, len(Series), sum(Series), Truncated) — นี่คือ guard หลักของแผน (3) เทสต์ว่า sum(Series[].Bytes) == Observed จริงสำหรับทุก window รวมกรณี ring มีข้อมูลมากกว่าความยาว window (เติม 288 บัคเก็ตแล้วขอ 15m ต้องได้เฉพาะ 3 บัคเก็ตท้าย ไม่ใช่รวมทั้ง ring มา carry ไว้ที่จุดแรก) (4) เทสต์ว่า ring ที่มีข้อมูลน้อยกว่า N (เพิ่งบูต มี 2 บัคเก็ต แล้วขอ 24h) ไม่ panic และคืน series ความยาว N ที่ zero-fill (5) เทสต์ฝั่ง DNS ว่า dnsWindowBuckets เลือกจำนวนบัคเก็ตถูกต้องทั้ง 7 ค่า (6) **เทสต์บังคับแยก label/value ตามที่เจ้าของยืนยันใน §0 D-4**: ฝั่ง api ยิง ?window= ครบทั้ง 7 ค่าตัวเล็กแล้ว response.window ต้องตรงกันเป๊ะ และค่าแปลกปลอมทุกตัว ('', 'evil', '99h', '2h', '1H', '24H', '15M', ' 1h', '../etc/passwd', สตริงยาว 10KB) ต้องตกกลับเป็น '1h' พร้อมสถานะ 200 — ย้ำว่า '1H'/'24H'/'15M' ตัวใหญ่ซึ่งเป็น label บนปุ่ม ต้องถือเป็นค่าที่ไม่รู้จัก ห้าม normalize เป็นตัวเล็กโดยอัตโนมัติ (ถ้า normalize ให้ บั๊ก 'ปุ่ม 3H active แต่ได้ข้อมูล 1 ชม.' จะถูกกลบจนไม่มีทางจับได้) (7) เทสต์ -race เดิมที่อ่านขนานกับ poll() ให้เพิ่ม window ใหม่เข้าไปในลูปด้วย",
    "acceptance": [
      "go test ./... -race ผ่านทั้ง repo ไม่มี race report",
      "เทสต์เดิมไม่ถูกแก้ความหมาย",
      "มีเทสต์ครบทั้ง 7 ข้อในคำสั่ง โดยเฉพาะข้อ 6 ที่ยืนยันว่า '1H'/'24H'/'15M' -> '1h'"
    ],
    "depends_on": ["T-02", "T-03", "T-04", "T-05"],
    "status": "done"
  },
  {
    "task_id": "T-08",
    "title": "openapi: ขยาย enum ของ window ทั้ง 7 จุด ในสองไฟล์ให้ตรงกันเป๊ะ",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "แก้ parameter ชื่อ window ทุกจุด (บรรทัดราว 363, 401, 433, 497, 549, 598, 648 ของแต่ละไฟล์ — รวม /api/dashboard/traffic-detail ที่บรรทัด 363 ตาม §0 D-2) จาก enum: [1h, 24h] เป็น enum: [15m, 30m, 1h, 3h, 6h, 12h, 24h] โดย default: 1h เหมือนเดิม และแก้ description ให้เขียนว่าค่าใดนอกชุดนี้ (รวมทั้งไม่ส่งมา และรวมตัวพิมพ์ใหญ่เช่น 1H) จะ fallback เป็น 1h ไม่ใช่ error. นอกจากนี้ให้แก้ description ของ schema ที่พูดถึงความยาว series ('12 points for window=1h, 288 points for window=24h' — ราวบรรทัด 4428, 4536, 4752 ของ docs/openapi.yaml) ให้เป็นกติกาทั่วไป: จำนวนจุด = จำนวนบัคเก็ต 5 นาทีของ window (3/6/12/36/72/144/288). ห้ามเพิ่ม path ใหม่ ห้ามเพิ่ม/ลบ parameter ห้ามแก้ schema ฟิลด์อื่น. ต้องแก้สองไฟล์ให้เนื้อหาเหมือนกันเป๊ะ (backend/internal/api/dist/openapi.yaml เป็นผลลัพธ์จาก build.sh ห้ามแก้ด้วยมือ)",
    "acceptance": [
      "diff docs/openapi.yaml frontend/public/openapi.yaml ไม่มีความต่าง",
      "ไม่มี enum: [1h, 24h] หลงเหลือในสองไฟล์",
      "ไฟล์ยังเป็น YAML ที่ parse ได้ (หน้า ApiDocs เปิดได้)"
    ],
    "depends_on": ["T-05"],
    "status": "done"
  },
  {
    "task_id": "T-09",
    "title": "frontend: โมดูลกลางของ window (lib/statsWindow.ts)",
    "layer": "frontend",
    "files": ["frontend/src/lib/statsWindow.ts"],
    "instruction": "สร้างไฟล์ใหม่ (pure TypeScript ห้ามมี JSX/React/DOM) เป็นแหล่งความจริงเดียวของช่วงเวลาตาม docs/ref/todo/statistics-window-granularity-plan.md §2.3: (1) export type StatsWindow = \"15m\" | \"30m\" | \"1h\" | \"3h\" | \"6h\" | \"12h\" | \"24h\" (2) export const STATS_WINDOWS: readonly { value: StatsWindow; label: string; minutes: number; points: number }[] เรียงตามลำดับที่แสดงบนปุ่ม โดย label ต้องเป็น '15m','30m','1H','3H','6H','12H','24H' ตามที่เจ้าของกำหนด (§0 D-4: ตัว H ใหญ่เฉพาะ label เท่านั้น — value ต้องตัวเล็กเสมอเพราะถูกส่งขึ้น API/URL และ backend ถือว่า '1H' เป็นค่าที่ไม่รู้จัก), minutes = 15,30,60,180,360,720,1440 และ points = minutes/5 (3) export function parseStatsWindow(raw: string | null | undefined): StatsWindow คืน '1h' เมื่อค่าไม่อยู่ในชุด (ตรงกับ fallback ของ backend และต้องไม่ toLowerCase ให้ เพื่อให้พฤติกรรมสองฝั่งตรงกันเป๊ะ) (4) export function statsWindowLongLabel(w: StatsWindow): string สำหรับหัวกราฟ โดย '1h' ต้องได้ '1 ชม. ล่าสุด' และ '24h' ต้องได้ '24 ชม. ล่าสุด' เป๊ะเท่าข้อความเดิมใน TrafficTrendCard.tsx:114 (regression guard) ส่วนค่าใหม่ใช้รูปแบบเดียวกัน ('15 นาทีล่าสุด', '30 นาทีล่าสุด', '3 ชม. ล่าสุด', '6 ชม. ล่าสุด', '12 ชม. ล่าสุด') (5) export function statsWindowSeconds(w) = minutes*60 และ export function mockWindowScale(w): number สำหรับ mock generator โดยต้องคง 1h -> 1 และ 24h -> 18 เท่าค่าเดิมเป๊ะ ส่วนค่าใหม่ใช้ 15m -> 0.3, 30m -> 0.6, 3h -> 3, 6h -> 5, 12h -> 10 (6) doc comment ต้องระบุว่าทุกค่าอิงบัคเก็ต 5 นาทีของ backend และต้องตรงกับตาราง statsWindowBuckets ใน backend/internal/service/traffic_stats.go เสมอ",
    "acceptance": [
      "tsc -b / yarn build ผ่าน",
      "ไฟล์ไม่ import อะไรจาก react / components / services",
      "parseStatsWindow(null) === '1h' และ parseStatsWindow('1H') === '1h' (เพราะไม่รู้จัก ไม่ใช่เพราะแปลงตัวพิมพ์)"
    ],
    "depends_on": [],
    "status": "done"
  },
  {
    "task_id": "T-10",
    "title": "frontend: segmented control แทน <Select> + hook อ่าน URL รองรับ 7 ค่า",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/DnsStatsShared.tsx"],
    "instruction": "(1) ลบ type StatsWindow ที่ประกาศในไฟล์นี้ แล้ว re-export type จาก @/lib/statsWindow แทน (export type { StatsWindow }) เพื่อให้ import เดิมของทั้ง 6 หน้าไม่พัง (2) useStatsWindow(): ใช้ parseStatsWindow(searchParams.get('window')) แทนกติกา raw === '24h' ? '24h' : '1h' ส่วนที่เหลือ (setSearchParams แบบ replace: true) ห้ามเปลี่ยน — ถ้าเปลี่ยนเป็น push การกดเล่นปุ่ม 7 ปุ่มจะยัดประวัติเบราว์เซอร์จนปุ่ม Back ใช้ไม่ได้ (3) เพิ่มคอมโพเนนต์ใหม่ StatsWindowTabs({value, onChange}) เป็น segmented control ที่ map จาก STATS_WINDOWS ตามสไตล์ Addressing Mode ใน pages/Interfaces.tsx บรรทัด ~1635-1656 คือกล่องนอก 'flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5' และปุ่มแต่ละอันเป็น <button type=\"button\"> ที่ active ใช้ 'bg-primary text-primary-foreground' ส่วน inactive ใช้ 'text-muted-foreground hover:bg-muted hover:text-foreground' แต่ **บีบขนาดลง** เป็น 'px-2 py-1 text-[11px] font-medium rounded-md cursor-pointer transition' เพราะมี 7 ปุ่มไม่ใช่ 2 (4) responsive: ห่อกล่องด้วย 'max-w-full overflow-x-auto' เพื่อให้จอแคบเลื่อนแนวนอนได้แทนที่จะดันปุ่ม Refresh ตกขอบ (5) a11y: กล่องนอกใส่ role=\"group\" aria-label=\"ช่วงเวลา\" และปุ่มใส่ aria-pressed={active} (6) onChange ต้องส่ง item.value (ตัวเล็ก) เสมอ ห้ามส่ง item.label (7) ห้ามใช้คลาสสีดิบของ Tailwind ห้าม shadow-* / backdrop-blur-* ต้องอ่านออกทั้ง dark และ light (8) ลบ StatsWindowSelect เดิมทิ้ง พร้อม import ของ Select ที่ไม่ได้ใช้แล้ว — ทั้ง 6 หน้าจะถูกแก้ให้ใช้ StatsWindowTabs ใน T-13/T-14 (9) ห้ามแตะ DomainStatsTable/ClientStatsTable/DnsStatsPrivacyNote/DnsStatsTruncatedWarning ในไฟล์เดียวกัน",
    "acceptance": [
      "yarn build และ yarn lint ผ่าน ไม่มี import ที่ไม่ได้ใช้ค้าง",
      "ไม่มี <Select> เหลือในไฟล์นี้",
      "ปุ่มทั้ง 7 มาจากการ map STATS_WINDOWS ไม่ใช่เขียนซ้ำ 7 ชุด และ onChange ส่งค่าตัวเล็ก",
      "รองรับ dark/light และไม่มีคลาสสีดิบ/shadow/backdrop-blur"
    ],
    "depends_on": ["T-09"],
    "status": "done"
  },
  {
    "task_id": "T-11",
    "title": "frontend services: ขยาย type และ mock generator ให้รองรับ 7 ค่า",
    "layer": "frontend",
    "files": [
      "frontend/src/services/statisticsService.ts",
      "frontend/src/services/trafficStatisticsService.ts",
      "frontend/src/services/dnsStatisticsService.ts"
    ],
    "instruction": "(1) แทนที่ union \"1h\" | \"24h\" ทุกจุดในสามไฟล์ (statisticsService.ts:94,218,307 · trafficStatisticsService.ts:18,81,238,280,318 · dnsStatisticsService.ts:23,34,45,91,148,193) ด้วย type StatsWindow ที่ import จาก @/lib/statsWindow — ค่า default ของพารามิเตอร์ยังเป็น '1h' เหมือนเดิมทุกฟังก์ชัน (2) mockWindowSeconds ใน trafficStatisticsService.ts ให้เรียก statsWindowSeconds จาก lib แทน (ค่า 1h=3600, 24h=86400 ต้องเท่าเดิมเป๊ะ) (3) mockBandwidthSeries ใน statisticsService.ts: n ต้องมาจาก STATS_WINDOWS (points) แทน window === '24h' ? 288 : 12 — ตรวจให้แน่ใจว่าตรรกะกระจายค่าและการบังคับให้ sum == exactTotal ยังถูกเมื่อ n เล็กสุดคือ 3 (อย่าหาร/mod ด้วยค่าที่อาจกลายเป็น 0) (4) ทุกที่ที่เขียน const scale = window === '24h' ? 18 : 1 (6 จุดในสามไฟล์) ให้เรียก mockWindowScale(window) แทน โดยผลของ 1h/24h ต้องเท่าค่าเดิมเป๊ะ และเนื่องจาก scale ใหม่เป็นทศนิยมได้ (15m=0.3) ต้องใส่ Math.max(1, Math.round(...)) ตรงจุดที่ค่านั้นถูกใช้เป็นจำนวนแถว/จำนวนเต็ม ไม่งั้น mock จะได้ 0 แถวที่ window สั้น และ QA จะเข้าใจผิดว่า backend พัง (5) **ห้ามแตะ dashboardService.ts** (หน้า Dashboard อยู่นอกขอบเขตตาม §0 D-2 และยังส่งแค่ 1h/24h) (6) ห้ามเปลี่ยนรูปร่าง response type อื่น ๆ ห้ามเปลี่ยน endpoint/query param ห้ามแตะฟิลด์ความเร็ว (rateBps*)",
    "acceptance": [
      "yarn build (tsc -b) ผ่าน",
      "ไม่มี union \"1h\" | \"24h\" หลงเหลือในสามไฟล์นี้",
      "โหมด mock ที่ window=15m ยังได้ข้อมูลไม่ว่าง (ไม่ใช่ 0 แถว, series ยาว 3 จุด)",
      "ผลของ mock ที่ window=1h และ 24h เหมือนก่อนแก้",
      "dashboardService.ts ไม่ถูกแก้"
    ],
    "depends_on": ["T-09"],
    "status": "done"
  },
  {
    "task_id": "T-12",
    "title": "frontend: หัวกราฟ TrafficTrendCard รองรับ 7 label",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/TrafficTrendCard.tsx"],
    "instruction": "แทนที่ const windowLabel = statsWindow === '24h' ? '24 ชม. ล่าสุด' : '1 ชม. ล่าสุด' (บรรทัด ~114) ด้วย statsWindowLongLabel(statsWindow) จาก @/lib/statsWindow และเปลี่ยน import type StatsWindow ให้มาจาก @/lib/statsWindow. ข้อความของ 1h/24h ต้องออกมาเหมือนเดิมทุกตัวอักษร. ตรวจ tickInterval (Math.max(0, Math.ceil(data.length/10)-1)) ว่ายังให้ผลสมเหตุสมผลเมื่อ data.length = 3 (ควรได้ 0 = แสดงทุกจุด) และอัปเดตคอมเมนต์ที่เขียนว่า '12 points for 1h, 288 for 24h' ให้ครอบคลุมช่วงใหม่. ห้ามแตะตรรกะ mode bytes/speed, สูตร span_last / lastBucketSpanSeconds, สี, legend หรือ layout ของการ์ด (เป็นงานของแผน statistics-traffic-speed-plan.md ที่เสร็จแล้ว)",
    "acceptance": [
      "yarn build และ yarn lint ผ่าน",
      "หัวการ์ดที่ window=1h/24h แสดงข้อความเหมือนเดิมเป๊ะ",
      "ที่ window=15m กราฟแสดง 3 จุดโดยแกน X อ่านออก ไม่ error",
      "ตรรกะโหมด speed ไม่ถูกแก้"
    ],
    "depends_on": ["T-09"],
    "status": "done"
  },
  {
    "task_id": "T-13",
    "title": "control bar ใหม่: 3 หน้าฝั่ง Traffic/Overview",
    "layer": "frontend",
    "files": [
      "frontend/src/pages/StatisticsOverview.tsx",
      "frontend/src/pages/StatisticsTraffic.tsx",
      "frontend/src/pages/StatisticsTrafficHost.tsx"
    ],
    "instruction": "ในทั้งสามหน้า ให้จัดชุดควบคุมด้านบน (div ที่ครอบ StatsWindowSelect + AccuracyBadge + ปุ่ม Refresh — Overview ~412-419, Traffic ~240-246, TrafficHost ~390-…) ใหม่ตามลำดับที่เจ้าของกำหนด: (1) AccuracyBadge ก่อน โดยคงเงื่อนไขการแสดงเดิมทุกประการ (เช่น {stats && <AccuracyBadge .../>}) (2) ตามด้วย <StatsWindowTabs value={window_} onChange={setWindow} /> แทน StatsWindowSelect เดิม (3) ปิดท้ายด้วยปุ่ม Refresh เดิม โดยเปลี่ยนข้อความ 'Refresh' เป็น <span className=\"hidden sm:inline\">Refresh</span> และเพิ่ม aria-label=\"Refresh\" + title=\"Refresh\" ที่ปุ่ม (ไอคอน RefreshCw, สถานะ animate-spin และ disabled เดิมห้ามเปลี่ยน) (4) เพิ่ม flex-wrap ให้ div ที่ครอบชุดควบคุม (ปัจจุบัน 'flex items-center gap-2') เพื่อให้ตกบรรทัดได้บนจอแคบ (5) เปลี่ยน type ของพารามิเตอร์ที่ยังเขียนว่า win: \"1h\" | \"24h\" (Overview:360, Traffic:49,180, TrafficHost:278) ให้เป็น StatsWindow จาก @/lib/statsWindow (6) ห้ามเปลี่ยนตรรกะ load/polling/useEffect dependency, ห้ามเพิ่ม setInterval หรือ fetch ใหม่, ห้ามแตะตาราง/การ์ด/คอลัมน์ Speed/การ์ดความเร็วปัจจุบัน (7) ห้ามใช้คลาสสีดิบ ห้าม shadow-*/backdrop-blur-* ต้องรองรับ dark/light",
    "acceptance": [
      "yarn build และ yarn lint ผ่าน ไม่มี import ที่ไม่ได้ใช้ค้าง",
      "ลำดับบนจอกว้างคือ Badge -> ปุ่มช่วงเวลา -> Refresh(มี label)",
      "บนจอเล็กกว่า sm ปุ่ม Refresh เหลือแต่ไอคอน และไม่มีองค์ประกอบใดล้นออกนอกจอ",
      "ไม่มีการเปลี่ยนพฤติกรรมการโหลดข้อมูล/ความถี่ refresh และคอลัมน์ Speed ยังทำงานเหมือนเดิม"
    ],
    "depends_on": ["T-10", "T-11", "T-12"],
    "status": "done"
  },
  {
    "task_id": "T-14",
    "title": "control bar ใหม่: 3 หน้าฝั่ง DNS",
    "layer": "frontend",
    "files": [
      "frontend/src/pages/StatisticsDns.tsx",
      "frontend/src/pages/StatisticsDnsDomain.tsx",
      "frontend/src/pages/StatisticsDnsClient.tsx"
    ],
    "instruction": "ทำแบบเดียวกับ T-13 กับสามหน้านี้ (StatisticsDns ~81-93, StatisticsDnsDomain ~94-…, StatisticsDnsClient ~101-…) แต่ระวังว่า **สามหน้านี้ไม่มี AccuracyBadge** — ลำดับจึงเป็น ปุ่มช่วงเวลา -> Refresh และ layout ต้องไม่มีช่องว่าง/placeholder ค้างจากตำแหน่ง badge ที่ไม่มีจริง (ห้ามใส่ badge ใหม่เข้าไปเอง ไม่อยู่ในขอบเขต). เปลี่ยน StatsWindowSelect -> StatsWindowTabs, Refresh label เป็น <span className=\"hidden sm:inline\">Refresh</span> + aria-label/title, เติม flex-wrap ให้ div ที่ครอบ, และเปลี่ยน type win: \"1h\" | \"24h\" (Dns:34, DnsDomain:34, DnsClient:37) เป็น StatsWindow. ห้ามแตะตรรกะ load/drilldown/การ navigate กลับ (ปุ่ม Back ต้องพา window เดิมกลับไปได้เหมือนเดิม) และห้ามแตะ DnsStatsPrivacyNote/TruncatedWarning",
    "acceptance": [
      "yarn build และ yarn lint ผ่าน",
      "ทั้งสามหน้ามีปุ่มช่วงเวลา 7 ปุ่มและ Refresh ที่ซ่อน label บนจอเล็ก",
      "คลิกแถวเพื่อ drill-down แล้วกลับ ยังคง window เดิมใน URL",
      "รองรับ dark/light"
    ],
    "depends_on": ["T-10", "T-11", "T-12"],
    "status": "done"
  }
]
```

## 4. ระดับการ review ของแต่ละ Task

- **ไม่มี Task ใดเป็นงาน sensitive** ตามนิยาม CLAUDE.md — ไม่แตะ auth, การ generate firewall rule,
  D-Bus/Netlink และไม่เพิ่ม input surface ใหม่ (ยังเป็น query param เดิมที่ whitelist แบบ enum ปิด)
- Task ที่ต้อง review ละเอียดกว่าเพื่อน:
  - **T-01** — หัวใจของแผน ถ้า n ของแกน series กับ n ของการเลือกบัคเก็ตหลุดจากกัน invariant
    `sum(Series[].Bytes) == Observed` จะพังแบบเงียบ ๆ (กราฟกับตัวเลขสรุปไม่ตรงกัน)
  - **T-05** — ด่าน validate อินพุตจากผู้ใช้ ต้องคง fallback (ไม่ error) และห้ามถอด
    defense-in-depth ที่ชั้น service ออก
  - **T-07 ข้อ 6** — เทสต์ที่เจ้าของสั่งให้มีโดยเฉพาะ (label ตัวใหญ่ต้องไม่ถูก normalize)

## 5. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ai-qa ตรวจครบแล้ว ผลลัพธ์: PASS)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... && go vet ./... && go test ./... -race — ผ่านทั้งหมด ไม่มี race report",
    "cd frontend && yarn build && yarn lint — ผ่าน ไม่มี error/warning ใหม่",
    "bash build.sh สำเร็จ ได้ไบนารีเดียว ./pigate",
    "diff docs/openapi.yaml frontend/public/openapi.yaml — ไม่มีความต่าง และไม่มี enum: [1h, 24h] หลงเหลือ",
    "grep ทั้ง repo: ไม่มีการเทียบ window กับ \"1h\"/\"24h\" แบบ hardcode หลงเหลือใน backend/internal/{service,api} (ยกเว้นตารางกลางใน traffic_stats.go และ kernel/dhcp_server.go:72 ที่เป็น lease time คนละเรื่อง) และไม่มี union \"1h\" | \"24h\" หลงเหลือใน frontend/src ยกเว้น dashboardService.ts ที่ห้ามแตะ",
    "regression 1h/24h: ยิง GET /api/statistics/traffic?window=1h และ ?window=24h ด้วยข้อมูล mock ชุดเดียวกัน — จำนวนจุดของ series (12/288), observedBytes, truncated และรูปร่าง JSON เหมือนก่อนแก้ทุกฟิลด์",
    "ทุก endpoint ที่รับ window (/api/statistics/traffic, /statistics/traffic/hosts, /statistics/traffic/host, /statistics/dns, /statistics/dns/domain, /statistics/dns/client, /api/dashboard/traffic-detail) ตอบ 200 และคืน window ตรงกับที่ส่งไป ครบทั้ง 7 ค่า",
    "ค่าที่ไม่รู้จัก ('', 'evil', '99h', '2h', '1H', '24H', '15M', ' 1h', '../etc/passwd', สตริงยาว 10KB) ทุก endpoint ตอบ 200 พร้อม window='1h' ไม่ 400 ไม่ 500 ไม่ panic",
    "sum(series[].bytes) == observedBytes สำหรับทุก window ทั้ง 7 ค่า และ len(series) = 3/6/12/36/72/144/288 ตามลำดับ (ตรวจทั้ง /statistics/traffic และ /statistics/traffic/host)",
    "เพิ่งบูต (ring ยังมีไม่ถึง N บัคเก็ต) แล้วขอ window=24h — ไม่ panic, series ยาว 288 และ zero-fill",
    "รัน backend ด้วย -mock=true แล้วเปิดครบทั้ง 6 หน้า (/statistics, /statistics/traffic, /statistics/traffic/host/:ip, /statistics/dns, /statistics/dns/domain?domain=…, /statistics/dns/client?client=…): ทุกหน้ามีปุ่มช่วงเวลา 7 ปุ่มเรียง 15m,30m,1H,3H,6H,12H,24H และกดแล้วข้อมูลเปลี่ยนจริง",
    "ลำดับ control bar ถูกต้อง: หน้าที่มี badge = Badge -> ปุ่มช่วงเวลา -> Refresh; หน้า DNS ที่ไม่มี badge = ปุ่มช่วงเวลา -> Refresh โดยไม่มีช่องว่างค้าง",
    "ค่าที่ส่งขึ้น URL/API เป็นตัวเล็กเสมอ ('1h' ไม่ใช่ '1H') แม้ปุ่มจะแสดงเป็น 1H — ตรวจจาก address bar และ Network tab ของเบราว์เซอร์",
    "กดปุ่มช่วงเวลาแล้ว ?window= ใน URL เปลี่ยนแบบ replace (ปุ่ม Back ของเบราว์เซอร์ไม่ถูกยัดประวัติทีละคลิก) และ deep link ด้วย ?window=6h เปิดมาแล้วปุ่ม 6H active",
    "คลิกแถวเพื่อ drill-down จากทุกหน้า list แล้วกด Back — window ที่เลือกไว้ยังคงเดิมทุกกรณี รวมค่าใหม่ (เช่น 30m)",
    "responsive: ที่ความกว้าง 1280 / 768 / 390 px ทุกหน้า ปุ่ม Refresh ไม่ถูกดันตกขอบ, ที่ < sm ปุ่ม Refresh เหลือแต่ไอคอน, และแถบ 7 ปุ่มเลื่อนแนวนอนได้หรือขึ้นบรรทัดใหม่แทนที่จะล้นจอ",
    "ทั้ง dark และ light mode แสดงผลถูกต้อง ปุ่มที่ active อ่านออกชัดทั้งสองธีม ไม่มี shadow-*/backdrop-blur-* และไม่มีคลาสสีดิบของ Tailwind ที่เพิ่มใหม่",
    "หัวการ์ดกราฟแสดงข้อความช่วงเวลาถูกต้องทั้ง 7 ค่า และข้อความของ 1h/24h เหมือนก่อนแก้ทุกตัวอักษร",
    "ฟีเจอร์ความเร็วจากแผนก่อนหน้ายังทำงานครบ: ปุ่ม toggle bytes/speed, คอลัมน์ Speed ในสองตาราง และการ์ดความเร็วปัจจุบันของหน้า drill-down ไม่ regress",
    "หน้า Dashboard (แท็บ Detailed) ยังทำงานเหมือนเดิมทุกประการ — ไม่มีปุ่มช่วงเวลาใหม่โผล่ที่นั่น และ dashboardService.ts ไม่ถูกแก้",
    "ตรวจ diff ทั้ง PR: ไม่มี exec.Command ใหม่, ไม่แตะ kernel/ (interfaces.go, real_*.go, mock.go), ไม่แตะ system_status.go/Dashboard/SSE, ไม่มี route ใหม่, ไม่มี migration, ไม่มี config key ใหม่, ไม่มีการเปลี่ยน bucket span/max",
    "โค้ดทั้งหมดอยู่บน branch feat/statistics-traffic-speed (branch เดิม ไม่มีการสร้าง branch ใหม่ ไม่มีการ push main)"
  ]
}
```

**หมายเหตุการทดสอบ:** ai-qa ตรวจครบทุกข้อข้างต้นแล้ว **ผ่านทั้งหมด** (รวมการยิง curl จริงครบ 7
window, เคส case-sensitivity/malformed input, responsive 3 ขนาดจอ และ dark/light) — ส่วนที่เหลือคือ
**เจ้าของโปรเจกต์ตรวจด้วยตาบนเบราว์เซอร์จริง** ก่อนสั่ง commit/เปิด PR

## 6. ความเสี่ยง / ข้อควรระวัง (ai-developer และ ai-qa อ่านก่อนเริ่ม)

1. **จุดตายเดียวของแผน: n ต้องมาจากที่เดียว** — ใน `getTrafficBreakdown` ค่า `n` ถูกใช้สองที่
   (สร้างแกนเวลาของ `series` ก่อนล็อก และเลือกจำนวนบัคเก็ตหลังล็อก) ถ้าเผลอคำนวณคนละทาง
   ตัวเลขบนกราฟกับ `observedBytes` จะไม่ตรงกันโดยไม่มี error ให้เห็น — ต้องเรียก
   `statsWindowBucketCount` ครั้งเดียวแล้วใช้ตัวแปรเดียวกันทั้งสองจุด
2. **15m = 3 จุด และหนึ่งในสามจุดนั้นยังเก็บไม่ครบ 5 นาที** — เจ้าของยอมรับแล้ว (§0 D-1)
   นี่คือพฤติกรรมที่ถูกต้องตามข้อมูล ไม่ใช่บั๊ก และ **ห้ามแก้ด้วยการตัดจุดสุดท้ายทิ้ง**
   เพราะจะทำให้ `sum(Series) == Observed` พัง
3. **label กับ value ห้ามปนกัน** — ปุ่มเป็น `1H/3H/…` (ตัวใหญ่) แต่ backend รู้จักเฉพาะตัวเล็ก
   ถ้าเผลอส่ง `1H` ขึ้น API จะ fallback เป็น `1h` เงียบ ๆ ผู้ใช้จะเห็นปุ่ม `3H` active แต่ได้ข้อมูล
   1 ชั่วโมง — บั๊กที่จับได้ยากมาก **ห้ามแก้ด้วยการ `toLowerCase()` ให้ที่ backend** เพราะจะกลบ
   บั๊กจนเทสต์จับไม่ได้ ให้พึ่งเทสต์ T-07 ข้อ 6 แทน (เจ้าของสั่งไว้ชัดใน D-4)
4. **mock scale เป็นทศนิยมได้แล้ว** (15m = 0.3) จุดที่เอา scale ไปคูณเป็นจำนวนแถว/จำนวน entry
   ต้อง `Math.max(1, Math.round(...))` ไม่งั้นหน้า mock จะว่างเปล่าที่ window สั้น และ ai-qa
   จะเข้าใจผิดว่า backend พัง
5. **`window=24h` เดิมใช้ "ทั้ง ring" ไม่ใช่ "288 ตัวท้าย"** — เท่ากันเพราะ ring ถูกตัดที่ 288 อยู่แล้ว
   แต่ถ้าอนาคตมีใครขยาย `trafficDetailBucketMax` โดยไม่ขยายตาราง window ความหมายจะเปลี่ยนทันที
   ให้เขียนคอมเมนต์ผูกสองค่านี้เข้าหากันไว้ใน T-01
6. **สามหน้า DNS ไม่มี AccuracyBadge** — อย่า "จัดลำดับ" ด้วยการใส่ placeholder ว่างไว้ตำแหน่ง badge
7. **ห้ามผูกความเร็ว (rateBps*) เข้ากับ window** — มันคือ snapshot ของ conntrack poll ล่าสุด (~10 วิ)
   ผู้ใช้อาจคาดหวังว่า "เลือก 15m แล้วความเร็วต้องเป็นค่าเฉลี่ย 15 นาที" ซึ่งไม่ใช่สิ่งที่ระบบนี้ทำ
   (เป็นแผนแยกในอนาคต) ป้ายกำกับเดิมที่บอกว่าเป็นค่าเฉลี่ย ~10 วินาที ต้องคงไว้ทุกจุด
8. **`useStatsWindow` ใช้ `replace: true`** — ห้ามเปลี่ยนเป็น push ไม่งั้นการกดเล่นปุ่ม 7 ปุ่ม
   จะยัดประวัติเบราว์เซอร์จนปุ่ม Back ใช้งานไม่ได้
9. **ทำงานบน branch `feat/statistics-traffic-speed` เดิมเท่านั้น** (คำสั่งเจ้าของ) — ห้ามแตก branch
   ใหม่ ห้าม rebase/reset ทับงาน speed ที่ยังไม่ merge และก่อนเริ่มให้ยืนยันว่าไฟล์
   `frontend/src/components/statistics/TrafficTrendCard.tsx` กับ `fmtRate` ใน `lib/formatBytes.ts`
   มีอยู่จริงใน working tree (ถ้าไม่มี แปลว่าอยู่ผิด branch — ให้หยุดแล้วแจ้งกลับ ห้ามเดา)

## 7. Known limitations (บันทึกไว้หลัง QA — ยอมรับได้ ไม่บล็อกการเปิด PR)

1. **ที่ `window=15m` ป้ายกำกับแกน X ของจุดสุดท้ายถูกตัดเล็กน้อยที่ขอบขวาของกราฟ**
   (พบโดย ai-qa, severity ต่ำ, **ไม่บล็อก**)
   - สาเหตุ: เป็นพฤติกรรมการวาด tick ของ recharts เอง (tick ตัวสุดท้ายชนขอบ `ResponsiveContainer`)
     — **ไม่ใช่ regression จากแผนนี้** window ที่มีจุดน้อย (3 จุด) ทำให้ระยะห่างระหว่าง tick กว้างขึ้น
     จนเห็นผลชัด ส่วนที่ 12/288 จุดเดิมก็มีพฤติกรรมเดียวกันแต่มองแทบไม่ออก
   - ผลที่ผู้ใช้เห็น: ตัวเลขนาทีของจุดขวาสุดอาจถูกครอบตัดเล็กน้อย ยังอ่านค่าเวลาได้จาก tooltip ตามปกติ
   - ทางออกถ้าอนาคตอยากเก็บให้เรียบร้อย (งานแยก ไม่อยู่ในแผนนี้): ปรับ `margin.right` ของ
     `LineChart` ใน `TrafficTrendCard.tsx` หรือใส่ `padding={{ right: … }}` ให้ `XAxis`
     — ต้องระวังว่าการ์ดนี้ใช้ร่วมกัน 3 หน้า (Overview / Traffic list / drill-down) และมีสองโหมด
     (bytes/speed) จึงต้องตรวจ regression ครบทุกคู่ก่อนแก้
2. **ความละเอียดต่ำสุดยังเป็น 5 นาที** ตามการตัดสินใจ D-1 — `15m` จึงมี 3 จุดและ `30m` มี 6 จุด
   ถ้าอนาคตต้องการกราฟช่วงสั้นที่ละเอียดกว่านี้ ต้องเปิดแผนใหม่ที่เพิ่ม ring ละเอียด (เช่น 1 นาที)
   ไม่ใช่ปรับตาราง `statsWindowBuckets` ให้ต่ำกว่า 1 บัคเก็ต
3. **หน้า Dashboard (แท็บ Detailed) ยังมีปุ่มช่วงเวลาแค่ 2 ค่า** ทั้งที่ backend รับได้ 7 ค่าแล้ว
   (ตั้งใจตาม D-2) — ถ้าเจ้าของอยากให้ Dashboard ใช้ 7 ปุ่มด้วย เป็นงานเล็กแยกต่างหาก:
   เปลี่ยน `dashboardService.ts` ให้ใช้ `StatsWindow` แล้ววาง `StatsWindowTabs` แทนปุ่มเดิม
