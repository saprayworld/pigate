# Statistics — แยก byte เป็น Upload/Download ต่อ host (Issue #107)

> แผนงาน follow-up ของ `docs/ref/complete/statistics-page-plan.md` §0 ("นอกขอบเขต" ข้อ 2):
> ปัจจุบัน `flowsToSamples` บวก `Forward.Bytes + Reverse.Bytes` ทิ้งที่
> `kernel/real_traffic_account.go:95` ทำให้ทุกการ์ดของหน้า Statistics โชว์ได้แค่ byte รวม
> แผนนี้พา **ทิศทาง (orig/reply) ของ conntrack** ไหลจาก kernel → service → API → UI
> โดยรักษา invariant "ห้ามนับซ้ำ" ของ Phase 2 (PR #103) ไว้ครบทุกข้อ
>
> วันที่เขียน: 2026-08-01 · Branch ที่จะใช้: `feat/statistics-updown-bytes` (ตั้งต้นจาก `main`)
> README Feature Status: Statistics ยัง Completed เหมือนเดิม (แผนนี้เพิ่มมิติข้อมูล ไม่เพิ่มฟีเจอร์ใหม่)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
- หน้า `/logs/statistics` แสดง **Upload / Download แยกกัน** ต่อแถวใน Top Source Hosts,
  Top Destinations และ Top Conversations (ยังคงจัดอันดับด้วย byte รวมเหมือนเดิม)
- นิยามทิศทางชัดเจนและ "สัมพันธ์กับ IP ของแถวนั้น": `bytesUp` = byte ที่ IP ในแถวนั้น
  **ส่ง** ออกไป, `bytesDown` = byte ที่มัน **รับ** เข้ามา — ทำให้แถวใน Top Destinations
  (ซึ่ง IP อยู่ฝั่ง dst) ต้องสลับทิศจาก conntrack orig/reply
- ทุกแถวต้องคง invariant `bytesUp + bytesDown == bytes` เสมอ และผลรวมทั้งหมด
  ยังต้อง ≤ `observedBytes` (ไม่พองขึ้น = ไม่นับซ้ำ)
- ทำงานได้ทั้ง `-mock=true` และของจริง, ไม่เพิ่ม kernel capability ใหม่, ไม่แตะ
  `real_firewall.go` / `interfaces.go` (ลายเซ็น interface ไม่เปลี่ยน)

**นอกขอบเขต (ตัดชัดเจน)**
- **การ์ด Dashboard แท็บ Detailed** (`model.TopTalker`, `GetTrafficDetail`) — ไม่แตะ response
  เดิมเลย ยังเป็น byte รวมก้อนเดียว (ถ้าอยากได้ค่อยทำเป็นงานแยก, ดู T-12 optional)
- **Top Rules by Traffic** — มาจาก nftables rule counter ซึ่งไม่มีมิติทิศทาง (counter ต่อ rule
  ไม่ใช่ต่อ flow) → แยก up/down ไม่ได้ ห้ามพยายามเดา
- **Top Denied** — หน่วยเป็นจำนวน event ของ NFLOG ไม่ใช่ byte → ไม่เกี่ยวกับแผนนี้
- ไม่ persist อะไรลง SQLite เพิ่ม (ไม่มี migration ใด ๆ — SD card wear, `tech_stack_design.md` §8)
- ไม่เปลี่ยน bucket span/retention (5 นาที × 288) และไม่เพิ่ม endpoint ใหม่

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-08-01)

| ส่วน | สถานะ | อ้างอิง (file:line โดยประมาณ) |
|---|---|---|
| conntrack poll | มี `f.Forward.Bytes` / `f.Reverse.Bytes` แยกอยู่แล้ว แต่ **บวกทิ้ง** เป็น `bytes` ก้อนเดียว | `kernel/real_traffic_account.go:95` (`flowsToSamples`) |
| conntrack DESTROY event | parse `CTA_COUNTERS_ORIG`/`CTA_COUNTERS_REPLY` แยกกันอยู่แล้ว (`origBytes`, `replyBytes`) แต่ **บวกทิ้ง** ตอนสร้าง FlowSample | `kernel/real_conntrack_events.go:216-234` |
| DTO ชั้นล่าง | `model.FlowSample` มีฟิลด์เดียว `Bytes uint64` | `model/types.go:607-622` |
| kernel interface | `TrafficAccountingManager` 3 เมธอด — **ลายเซ็นไม่ต้องเปลี่ยน** (เปลี่ยนแค่เนื้อใน FlowSample) | `kernel/interfaces.go:41-73` |
| mock | `MockTrafficAccounting.DumpFlows`/`WatchFlowEnd` สังเคราะห์ byte รวมทางเดียว ไม่มีมิติทิศทาง | `kernel/mock.go:939-966`, `:1003-1033` |
| baseline ของ poll | `flowSampleState{bytes, misses}` — baseline ก้อนเดียวต่อ key | `service/traffic_stats.go:81-84`, `processFlows :407-492` |
| baseline ของ event | `onFlowEnd` เครดิต `f.Bytes - st.bytes` แล้ว `delete(flowState, key)` ทันที | `service/traffic_stats.go:345-367` |
| bucket ring | `hostBytes` / `catBytes` / `dstBytes` / `convBytes` เป็น `map[string]uint64` ทั้งหมด | `service/traffic_stats.go:89-105`, `addBucket :669-705` |
| accessor ของหน้า Statistics | `GetTrafficBreakdown` คืน `Hosts/Dests/Convs map[string]uint64` | `service/traffic_stats.go:837-903` |
| ประกอบ response | `buildTopHosts` / `buildTopConversations` (percent = `percentOf(bytes, observed)`) | `service/statistics.go:238-315` |
| DTO ของ API | `model.TopHost{Bytes}`, `model.TopConversation{Bytes}` | `model/statistics.go:14-51` |
| handler/route | `HandleGetStatistics` + `authRoute("GET /api/statistics/traffic")` — **ไม่ต้องแก้** | `api/handlers.go:413-425`, `api/router.go:42` |
| openapi | schema `TopHost` (:3993), `TopConversation` (:4040) มีทั้ง 2 ไฟล์ที่ต้อง sync | `docs/openapi.yaml`, `frontend/public/openapi.yaml` |
| frontend client | `TopHost.bytes` / `TopConversation.bytes` + mock branch สังเคราะห์เอง | `frontend/src/services/statisticsService.ts:7-56`, `:119-142` |
| frontend UI | `TopHostsCard` แสดง `fmtBytes(h.bytes) · h.percent%`; ตาราง Conversations มีคอลัมน์ Bytes เดียว | `frontend/src/pages/StatisticsOverview.tsx:133-166`, `:168-227` |
| test ที่คุม invariant Phase 2 | `TestTrafficStats_OnFlowEnd_*` 3 เคส + `TestGetTrafficBreakdown_OnFlowEnd*` 2 เคส + race test | `service/traffic_stats_test.go:186-256`, `:437-487`, `:321-360` |
| DB / migration | ไม่มีตารางที่เกี่ยวข้องเลย (RAM ล้วน) | — |

**สรุป:** ข้อมูลทิศทาง**มีอยู่แล้วทั้งสองเส้นทาง** (poll และ event) แค่ถูกบวกทิ้งที่บรรทัดเดียว
ต่อเส้นทาง งานจริงทั้งหมดกระจุกที่ `service/traffic_stats.go` ซึ่งเป็นที่อยู่ของ invariant
"ห้ามนับซ้ำ" ของ Phase 2 → นี่คือจุดเสี่ยงเดียวของแผนนี้ (ดู §5)

## 2. แนวทางเทคนิค

### 2.1 นิยาม invariant ของ Phase 2 ที่ต้องรักษา (อ่านจากโค้ด + PR #103)

1. **key parity** — poll กับ event ต้องได้ key เดียวกันเป๊ะผ่าน `flowKeyFromParts`
   (`real_traffic_account.go:137`, ใช้ร่วมกับ `real_conntrack_events.go:227`) ถ้าไม่ตรง
   event จะถูกมองเป็น flow ใหม่แล้วเครดิตเต็มก้อนทับของ poll → นับซ้ำเงียบ ๆ
2. **credit-delta-then-delete** — `onFlowEnd` เครดิตเฉพาะส่วนต่างเหนือ baseline แล้ว
   **ลบ key ออกจาก `flowState` ทันที** เพื่อไม่ให้ poll รอบถัดไปเห็นซ้ำ (`traffic_stats.go:345-356`)
3. **clamp ≥ 0** — flow โตทางเดียว ค่าติดลบ = key ชน/counter reset ไม่ใช่ค่าจริง → ปัดเป็น 0
4. **seed ไม่นับ** — poll ครั้งแรกสุด (`flowSeeded == false`) แค่ตั้ง baseline ไม่เครดิต
5. **delta เดียว → addBucket ครั้งเดียว** — host/cat/dst/conv ต้องใช้ `d` ตัวเดียวกันในการเรียก
   `addBucket` ครั้งเดียว (statistics-page-plan Caution 2, `traffic_stats.go:453-467`)

แผนนี้ **ไม่เปลี่ยนโครงข้างบนเลย** เปลี่ยนแค่ "หน่วยของ delta" จาก scalar เป็นคู่ (orig, reply)
ทุกกฎยังใช้เหมือนเดิมแต่ **บังคับใช้ต่อทิศทางอย่างอิสระ**

### 2.2 ทางที่เลือก — เปลี่ยน `FlowSample.Bytes` เป็นคู่ `BytesOrig`/`BytesReply`

```go
// model/types.go
type FlowSample struct {
    Key, SrcIP, DstIP string
    Proto   uint8
    DstPort uint16
    // BytesOrig = ทิศ SrcIP -> DstIP (conntrack Forward / CTA_COUNTERS_ORIG)
    // BytesReply = ทิศ DstIP -> SrcIP (Reverse / CTA_COUNTERS_REPLY)
    BytesOrig, BytesReply uint64
}
func (f FlowSample) TotalBytes() uint64 { return f.BytesOrig + f.BytesReply }
```

**เหตุผลที่ลบ `Bytes` ทิ้ง แทนที่จะ "เพิ่ม 2 ฟิลด์แล้วเก็บ Bytes ไว้"**
- ถ้าเก็บ `Bytes` ไว้คู่กัน จะมี "แหล่งความจริง 2 ที่" ที่ drift ได้ (producer ตัวใดตัวหนึ่ง
  ลืมเซ็ต แล้ว consumer อ่านคนละฟิลด์ = นับหาย/นับซ้ำแบบเงียบ) — เป็นความเสี่ยง
  ประเภทเดียวกับที่ Phase 2 พยายามกำจัด
- การลบฟิลด์ทำให้ **compiler ชี้ call site ให้ครบทุกจุด** (producer 3 จุด: real poll, real event,
  mock; consumer 2 จุด: `processFlows`, `onFlowEnd`; test literal ~34 จุดใน 2 ไฟล์) →
  ไม่มีทางลืมจุดใดจุดหนึ่งแบบเงียบ ๆ
- `FlowSample` เป็น struct ภายใน ไม่เคย marshal เป็น JSON ออก API → ไม่มี contract ภายนอกแตก

### 2.3 ชั้น service — baseline และ bucket เป็นคู่

```go
type flowSampleState struct { orig, reply uint64; misses int }   // เดิม: bytes, misses

type dirBytes struct{ Orig, Reply uint64 }                       // ใช้แทน uint64 ในทุก map ของ bucket
func (d dirBytes) Total() uint64 { return d.Orig + d.Reply }
```
- `hostBytes` / `dstBytes` / `convBytes` เปลี่ยนเป็น `map[string]dirBytes`
- `catBytes` (Protocol Breakdown ของ Dashboard) **คงเป็น `map[string]uint64` เหมือนเดิม** —
  การ์ดนั้นอยู่นอกขอบเขต §0 แก้แล้วเสี่ยง regression ฟรี ๆ
- `mergeUint64Map` เดิมยังใช้กับ `catBytes` ต่อ; เพิ่ม `mergeDirMap(dst, src, capAt)` ที่คัดลอก
  ตรรกะ cap เดิม **เป๊ะ ๆ** (key ใหม่ถูกทิ้งเมื่อชนเพดาน, key เดิมสะสมต่อ)

### 2.4 ทางเลือกที่ตัดทิ้ง

- **เพิ่ม map คู่ขนาน (`hostUpBytes`/`hostDownBytes`)** — จำนวน map ต่อ bucket จาก 5 เป็น 8,
  ต้องเรียก merge เพิ่มอีก 3 ครั้ง และ "ยอดรวม" กับ "ยอดแยก" กลายเป็นคนละ map ที่ drift ได้
  → ขัดกับ invariant ข้อ 5 โดยตรง
- **เก็บ `Total` ไว้ในตัว struct `dirBytes` เป็นฟิลด์ที่ 3** — ค่าซ้ำซ้อนที่ผิดเพี้ยนได้
  → ให้เป็น method คำนวณสดเสมอ
- **แยก up/down ที่ nftables ด้วย named counter ต่อทิศทาง** — ต้องแตะ `real_firewall.go`
  (ไฟล์อ่อนไหวที่สุด) ทั้งที่ conntrack ให้ข้อมูลนี้ฟรีอยู่แล้ว → ตัดทิ้งด้วยเหตุผลเดียวกับ
  `traffic-accounting-accuracy-phase2-plan.md` §2.2

### 2.5 นิยามทิศทางที่ชั้น presentation (จุดที่คนพลาดง่ายที่สุด)

bucket เก็บแบบ **flow-relative เสมอ** (orig = src→dst) ส่วนการแปลงเป็น up/down ทำที่
`service/statistics.go` ที่เดียว:

| การ์ด | key ของ map | `bytesUp` (ส่งออกจาก IP ของแถว) | `bytesDown` |
|---|---|---|---|
| Top Source Hosts | SrcIP | `Orig` | `Reply` |
| Top Destinations | DstIP | `Reply` ← **สลับ** | `Orig` |
| Top Conversations | 4-tuple (มอง srcIP เป็นเจ้าของแถว) | `Orig` | `Reply` |

## 3. ขั้นตอนการทำ (เรียงตาม dependency: model → kernel → service → api/docs → frontend)

### T-01 — เปลี่ยน `model.FlowSample` เป็นคู่ทิศทาง
**ไฟล์:** `backend/internal/model/types.go` (~:607-622)
แทน `Bytes uint64` ด้วย `BytesOrig`/`BytesReply` + method `TotalBytes()` และอัปเดต doc comment
ให้ระบุว่า orig = SrcIP→DstIP เสมอ (pre-NAT ตาม Forward tuple)
**acceptance:** ไฟล์นี้แก้เสร็จ (ทั้ง repo จะ build ไม่ผ่านจนกว่า T-02..T-05 เสร็จ — ปกติ)

### T-02 — poll path 🔒 (netlink, review เข้ม)
**ไฟล์:** `backend/internal/kernel/real_traffic_account.go` (`flowsToSamples` ~:83-106)
เลิกบวก `bytes := f.Forward.Bytes + f.Reverse.Bytes` → ส่ง `BytesOrig: f.Forward.Bytes`,
`BytesReply: f.Reverse.Bytes` ตรง ๆ (ตัวแปร `bytes` หายไป) — ห้ามแตะ `flowKey`/`flowKeyFromParts`
แม้แต่ตัวอักษรเดียว (invariant ข้อ 1)

### T-03 — event path 🔒 (netlink, review เข้ม)
**ไฟล์:** `backend/internal/kernel/real_conntrack_events.go` (~:216-234)
`origBytes`/`replyBytes` ถูก parse แยกอยู่แล้ว → ใส่ลงฟิลด์ใหม่ตรง ๆ แทน `origBytes + replyBytes`
คงพฤติกรรม "acct ปิด = 0 ทั้งคู่ ไม่ใช่ error" ไว้เหมือนเดิม

### T-04 — mock (ห้ามแตะ OS)
**ไฟล์:** `backend/internal/kernel/mock.go` (`DumpFlows` ~:939, `WatchFlowEnd` ~:1003,
`mockFlowTemplate` ~:888)
เพิ่มฟิลด์ `upRatio float64` ให้ template แล้วแบ่ง byte เป็น 2 ทิศแบบ **ไม่สมมาตร**
(เช่น HTTPS/HTTP วิดีโอ ~0.08, DNS ~0.45, VoIP ~0.5) เพื่อให้ dev เห็นว่า UI แยกทิศได้จริง
> ห้ามเพิ่ม timer/socket/ไฟล์ใด ๆ — ยังเป็น pure function ของ `time.Since(m.start)` เหมือนเดิม

### T-05 — ชั้น service: baseline + bucket เป็นคู่ 🔒 (จุดเสี่ยง double-count ทั้งหมดของแผนนี้)
**ไฟล์:** `backend/internal/service/traffic_stats.go`
1. `flowSampleState` (~:81) → `{orig, reply uint64; misses int}`
2. เพิ่ม type `dirBytes` + `mergeDirMap` (ข้าง `mergeUint64Map` ~:736) และเปลี่ยน
   `hostBytes`/`dstBytes`/`convBytes` ใน `trafficDetailBucket` (~:89) เป็น `map[string]dirBytes`
   (`catBytes`/`ruleBytes` คงเดิม)
3. `processFlows` (~:407) — คำนวณ delta **แยกทิศ** โดยใช้กฎเดิมทุกข้อ:
   ```go
   dOrig := clampDelta(f.BytesOrig, st.orig)   // int64 subtract แล้ว clamp ที่ 0
   dReply := clampDelta(f.BytesReply, st.reply)
   st.orig, st.reply, st.misses = f.BytesOrig, f.BytesReply, 0
   if firstPoll || dOrig+dReply == 0 { continue }
   d := dOrig + dReply            // ตัวเดียวกันนี้เท่านั้นที่ไหลเข้า observed/catDeltas
   ```
   `observed += d`, `catDeltas[...] += d` (คงเป็น uint64), ส่วน host/dst/conv รับ
   `dirBytes{dOrig, dReply}` — **ห้ามคำนวณ delta ชุดที่สองเด็ดขาด** (invariant ข้อ 5)
4. `onFlowEnd` (~:345) — ตรรกะเดิมเป๊ะ แต่ทำสองทิศ: มี baseline → เครดิต
   `f.BytesOrig-st.orig` และ `f.BytesReply-st.reply` (clamp แยกกัน) แล้ว `delete` key;
   ไม่มี baseline → เครดิตเต็มทั้งสองทิศ; `delta == 0` ทั้งคู่ → return ก่อนเรียก `addBucket`
   (invariant ข้อ 2 ต้องยังลบ key ก่อนออกทุกกรณีที่เจอ baseline)
5. `addBucket` (~:669) — เปลี่ยน signature ของ host/dst/conv เป็น `map[string]dirBytes`
   และเรียก `mergeDirMap` ด้วย cap ตัวเดิม (`maxTrackedHosts`/`maxTrackedDests`/
   `maxTrackedConversations`) — ค่าคงที่ห้ามเปลี่ยน
6. `GetTrafficDetail` (~:772) — **ห้ามเปลี่ยน response**: รวมด้วย `v.Total()` ให้ได้ตัวเลข
   `TopTalker.Bytes` เท่าเดิมทุกประการ (ยังต้องอ่านทั้งก้อนใต้ `s.mu.RLock()` ตามคำเตือน ~:754-771)
7. `GetTrafficBreakdown` (~:851) — `Hosts/Dests/Convs` เปลี่ยนเป็น `map[string]dirBytes`,
   `Observed`/`Truncated`/`Accuracy` เหมือนเดิม

### T-06 — DTO ของ API
**ไฟล์:** `backend/internal/model/statistics.go` (`TopHost` ~:14, `TopConversation` ~:35)
เพิ่ม `BytesUp uint64 \`json:"bytesUp"\`` และ `BytesDown uint64 \`json:"bytesDown"\`` (additive,
`bytes` เดิมคงอยู่ = up+down) พร้อม comment ระบุว่า "สัมพันธ์กับ IP ของแถวนี้" และเตือนว่า
Top Destinations สลับทิศแล้ว

### T-07 — ประกอบ response + สลับทิศให้ Top Destinations
**ไฟล์:** `backend/internal/service/statistics.go` (`buildTopHosts` ~:238,
`buildTopConversations` ~:270, `GetStatistics` ~:187)
- `buildTopHosts(totals map[string]dirBytes, ..., flip bool)` — `flip=false` สำหรับ
  `TopSources`, `flip=true` สำหรับ `TopDestinations` (ดูตาราง §2.5) และตั้ง
  `Bytes = v.Total()` เสมอ เพื่อให้ **การจัดอันดับและ `percent` ไม่เปลี่ยนจากของเดิมเลย**
- `buildTopConversations` — `BytesUp = Orig`, `BytesDown = Reply`
> **ไม่ต้อง** แก้ `api/handlers.go` / `router.go` / `NewServer` — response เป็น additive
> และ `window` ยัง whitelist ที่เดิม (`handlers.go:413-425`) ไม่มี input ใหม่จาก client

### T-08 — regression test ของ invariant (งานสำคัญที่สุดของแผนนี้)
**ไฟล์:** `backend/internal/service/traffic_stats_test.go`, `.../statistics_test.go`
- อัปเดต literal `Bytes:` เดิม (~34 จุดใน 2 ไฟล์) เป็น `BytesOrig`/`BytesReply` โดย
  **แบ่งค่าเดิมแบบไม่สมมาตร** (เช่น 1000 → 200/800) เพื่อให้ทุกเคสเดิมยังตรวจยอดรวมได้เท่าเดิม
  และจับสลับทิศผิดได้ด้วย
- เคสใหม่ที่ต้องมี:
  1. poll เห็น (200,800) → event ปิดที่ (300,1200) → รวมต้องได้ 1500 **ไม่ใช่ 3500**
     และ up=300 / down=1200 (ต่อยอด `TestTrafficStats_OnFlowEnd_CreditsOnlyDeltaAboveBaseline`)
  2. event ของ key ที่ poll ไม่เคยเห็น → เครดิตเต็มทั้งสองทิศ
  3. หลัง prune แล้ว event มาถึง → ไม่ underflow ทั้งสองทิศ
  4. **ทิศเดียวถอยหลัง**: orig ลด/reply เพิ่ม → orig clamp เป็น 0, reply นับปกติ, ไม่มี underflow
  5. **property**: ทุกแถวของ 3 การ์ด `bytesUp + bytesDown == bytes` และ
     `Σ bytes ของ TopSources ≤ observedBytes`
  6. **สลับทิศ**: flow LAN→internet ก้อนเดียว → ใน TopSources เป็น up, ใน TopDestinations
     ต้องกลายเป็น down (ล็อกตาราง §2.5 ไม่ให้กลับด้านโดยไม่ตั้งใจ)
  7. `-race` เดิม (`TestTrafficStats_GetTrafficDetailNoRaceWithPoll` ~:321) ต้องยังผ่าน
**acceptance:** `cd backend && go build ./... && go test -race ./...` ผ่านทั้งหมด

### T-09 — API contract
**ไฟล์:** `docs/openapi.yaml` (`TopHost` ~:3993, `TopConversation` ~:4040) **และ**
`frontend/public/openapi.yaml` (ต้องเหมือนกันเป๊ะ)
เพิ่ม `bytesUp`/`bytesDown` ใน `properties` + `required` พร้อม description ว่าเป็นค่าเทียบกับ
IP ของแถวนั้น และ `bytesUp + bytesDown == bytes` เสมอ
> **ไม่ต้อง** แก้ `backend/internal/api/dist/openapi.yaml` (build artifact ที่ `build.sh` ทับให้)

### T-10 — frontend API client + mock branch
**ไฟล์:** `frontend/src/services/statisticsService.ts` (~:7-56 types, ~:119-142 mock)
เพิ่ม `bytesUp`/`bytesDown` ใน `TopHost`/`TopConversation` และใน mock generator ให้แบ่งสัดส่วน
ไม่สมมาตร (สอดคล้องกับ `upRatio` ของ T-04) — ถ้าลืม mock branch tsc จะพัง

### T-11 — UI
**ไฟล์:** `frontend/src/pages/StatisticsOverview.tsx` (`TopHostsCard` ~:133,
`TopConversationsCard` ~:168)
- การ์ด Top Source/Destination: ใต้บรรทัดยอดรวมเพิ่มบรรทัดย่อย `↑ up · ↓ down` (ใช้ไอคอน
  `ArrowUp`/`ArrowDown` จาก lucide-react หรือสัญลักษณ์ข้อความ) — แถบ `HostBar` ยังใช้ percent
  ของยอดรวมเหมือนเดิม
- ตาราง Conversations: แยกคอลัมน์ Bytes เป็น `Up` / `Down` (คง `Total` ไว้ด้วย)
- **กฎสไตล์บังคับ**: ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามสีดิบ (`text-emerald-500`) ให้ใช้
  `text-primary`/`text-muted-foreground`, ตัวเลข/IP ใช้ `font-mono`, ต้องดูดีทั้ง dark/light
  และไม่ล้นบนจอแคบ (ตารางอยู่ใน `overflow-x-auto` อยู่แล้ว)

### T-12 (optional, ท้ายสุด) — เอกสาร
`docs/ref/complete/statistics-page-plan.md` เพิ่มบรรทัดว่าข้อ "นอกขอบเขต" เรื่อง up/down
ถูกทำแล้วในแผนนี้; `docs/tech_stack_design.md` ระบุว่า traffic accounting เก็บ orig/reply
แยกกันตั้งแต่ชั้น kernel

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | พฤติกรรม |
|---|---|---|---|
| GET | `/api/statistics/traffic?window=1h\|24h` | **เส้นเดิม** `authRoute` (ทุก role ที่ล็อกอิน) | เพิ่มฟิลด์ `bytesUp`/`bytesDown` ใน `topSources`/`topDestinations`/`topConversations` เท่านั้น (additive, ไม่ลบ/ไม่เปลี่ยนความหมายฟิลด์เดิม) |
| GET | `/api/dashboard/traffic-detail?window=1h\|24h` | **เส้นเดิม** `authRoute` | **response ต้องเหมือนเดิมทุก byte** (regression guard) |

ไม่มี endpoint ใหม่, ไม่มี mutation → ต้องใช้งานได้ปกติทั้งใน `-disable-edit=true` และ role
read-only (ถือเป็นเกณฑ์ทดสอบ) และไม่มี input ใหม่จาก client เข้าสู่ระบบเลย

## 5. ข้อควรระวัง

1. 🔒 **นับซ้ำ (invariant หลักของ Phase 2)** — ถ้า `onFlowEnd` เครดิตเต็มก้อนทั้งที่ poll เคยนับ
   ไปแล้ว ตัวเลขจะพองเกือบเท่าตัว **ป้องกัน:** เก็บโครง "เครดิตส่วนต่าง → ลบ key ทันที" ไว้
   เหมือนเดิมทุกบรรทัด เปลี่ยนแค่จาก 1 ตัวเลขเป็น 2 ตัวเลข, ห้ามแตะ `flowKeyFromParts`
   และต้องมีเทสต์ T-08 ข้อ 1 เป็นตัวล็อก
2. **baseline หลุดทิศเดียว** — ถ้าอัปเดต `st.orig` แต่ลืม `st.reply` (หรือกลับกัน) ทิศที่ลืมจะ
   ถูกนับซ้ำทุก poll ทีละก้อนสะสม (แย่กว่าข้อ 1 เพราะซ้ำทุก ๆ 10 วินาที)
   **ป้องกัน:** อัปเดตทั้งสองฟิลด์ในบรรทัดเดียวกัน (`st.orig, st.reply = ...`) และเทสต์ T-08
   ข้อ 4 ที่ขยับทีละทิศ
3. **การ clamp เปลี่ยนความหมายเล็กน้อย** — เดิม clamp ที่ผลรวม ตอนนี้ clamp ต่อทิศ กรณี
   counter ทิศหนึ่งถอยหลัง (key ชน/NAT port reuse) ตัวเลขรวมอาจต่างจากเดิมเล็กน้อย
   **ป้องกัน:** เขียนกำกับใน doc comment ของ `processFlows` ว่าเป็นพฤติกรรมที่ตั้งใจ
   (per-direction clamp ถูกต้องกว่า เพราะ counter monotonic แยกทิศ) และเทสต์ T-08 ข้อ 4
   ล็อกค่าไว้ ไม่ให้คนหลังคิดว่าเป็นบั๊ก
4. **ทิศกลับด้านของ Top Destinations** — ถ้าไม่ flip ผู้ใช้จะเห็น "server ปลายทางอัปโหลด
   4 GB" ซึ่งกลับหัวจากความจริง **ป้องกัน:** flip ที่ `buildTopHosts` ที่เดียว (ห้าม flip ใน
   bucket) + เทสต์ T-08 ข้อ 6
5. **ความหมายของ SrcIP ตอน flow มาจากภายนอก** — conntrack Forward tuple ทำให้ flow ที่
   ริเริ่มจากอินเทอร์เน็ต (เช่น port forward เข้าเซิร์ฟเวอร์ใน LAN) มี "source" เป็น IP
   อินเทอร์เน็ต → `bytesUp` ของแถวนั้นคือ byte ที่ **โลกภายนอกส่งเข้ามา** ไม่ใช่ upload ของบ้าน
   **ป้องกัน:** UI ใช้ badge `LAN`/`Internet` ที่มีอยู่แล้ว (`private`) และ tooltip อธิบายว่า
   ทิศเทียบกับ IP ของแถว ไม่ใช่เทียบกับ WAN
6. **`GetTrafficDetail` ต้องไม่ regress** — Dashboard แท็บ Detailed ใช้ bucket ring ก้อนเดียวกัน
   **ป้องกัน:** สรุปด้วย `.Total()` และมีเทสต์เดิม (`api/handlers_test.go` ~:925 window
   whitelist + service test) ต้องผ่านโดยไม่แก้ค่าที่คาดหวัง
7. **concurrent map / RLock** — `trafficDetailBucket` มีแต่ map (reference type) การอ่านต้องอยู่ใต้
   `RLock` ตลอดช่วง aggregate ตามคำเตือนยาวที่ `traffic_stats.go:754-771` การเปลี่ยน value type
   ไม่ทำให้ปลอดภัยขึ้นเลย **ป้องกัน:** ห้าม copy struct ออกมาแล้ววนทีหลัง, ต้องรัน `-race`
8. **RAM** — value ของ 3 map โตจาก 8 → 16 ไบต์ต่อ key (worst case ~+2-3 MB ที่ cap เต็ม
   ทุก bucket) ยังอยู่ในงบเดิมของ statistics-page-plan Caution 5 **ป้องกัน:** ห้ามขยับค่า cap
   ใด ๆ ในแผนนี้ และวัด RSS หลังรัน ≥1 ชม.
9. **`nf_conntrack_acct=0`** — ถ้า sysctl ปิด ทั้งสองทิศจะเป็น 0 (ไม่ใช่ error) เหมือนเดิม
   **ป้องกัน:** อย่าตีความ 0/0 เป็นความผิดพลาด, capability probe `conntrack` เดิมยังเป็นตัวบอก
   ผู้ใช้อยู่แล้ว — ไม่ต้องแก้ `real_capability.go`
10. **mock ต้องปลอดภัย 100%** — dev รัน `-mock=true` บน WSL ที่ไม่มี conntrack
    **ป้องกัน:** T-04 ห้ามเพิ่ม socket/ไฟล์/`exec.Command` ใด ๆ
11. **frontend mock branch ต้องอัปเดตพร้อม type** — ถ้าเพิ่มฟิลด์ใน interface แต่ลืม mock
    `yarn build` (tsc) จะพังหรือหน้าโชว์ 0 ทุกแถวในโหมด mock
12. **ห้าม persist** — ตลอดแผนนี้ `git diff --stat` ต้องไม่มี `backend/internal/db/`
    และไม่มี migration ใหม่ (SD card wear)
13. 🔒 **หลักฐานว่าไม่หลุดขอบเขต** — `git diff --stat` ต้องไม่มี `kernel/real_firewall.go`
    และไม่มี `kernel/interfaces.go` (ลายเซ็นไม่เปลี่ยน) ถ้ามีแปลว่าหลุด §0
14. **ทดสอบบนบอร์ดจริงปลอดภัย** — แผนนี้อ่านอย่างเดียว ไม่แตะ firewall/routing จึงไม่มีความ
    เสี่ยงล็อกตัวเองออกจากเครื่อง แต่ยังควรทดสอบตอนเข้าถึงเครื่องได้

## 6. Checklist สรุป (Definition of Done)

**Model / Kernel**
- [ ] T-01 `model/types.go`: `FlowSample.BytesOrig/BytesReply` + `TotalBytes()`
- [ ] T-02 `kernel/real_traffic_account.go` `flowsToSamples` 🔒 (ห้ามแตะ flowKey*)
- [ ] T-03 `kernel/real_conntrack_events.go` `safeParseConntrackDestroy` 🔒
- [ ] T-04 `kernel/mock.go` `DumpFlows`/`WatchFlowEnd` + `upRatio` (ไม่มี side effect)

**Service / API**
- [ ] T-05 `service/traffic_stats.go`: `flowSampleState` คู่, `dirBytes`+`mergeDirMap`,
      `processFlows`, `onFlowEnd`, `addBucket`, `GetTrafficBreakdown`, `GetTrafficDetail` คงเดิม 🔒
- [ ] T-06 `model/statistics.go`: `bytesUp`/`bytesDown` (additive)
- [ ] T-07 `service/statistics.go`: `buildTopHosts(flip)` + `buildTopConversations`
- [ ] T-08 test ครบ 7 เคส + อัปเดต literal เดิม; `cd backend && go build ./... && go test -race ./...`

**Docs / Frontend**
- [ ] T-09 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (diff ตรงกันเป๊ะ)
- [ ] T-10 `frontend/src/services/statisticsService.ts` (type + mock branch)
- [ ] T-11 `frontend/src/pages/StatisticsOverview.tsx` (up/down ทั้ง 3 การ์ด)
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] T-12 (optional) เอกสาร

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ)**
- [ ] `-mock=true -allow-dev-cors`: `/logs/statistics` แสดง up/down แยกทุกการ์ด ตัวเลขขยับ
      ทุก refresh และ **up/down ไม่เท่ากันเป๊ะ** (พิสูจน์ว่าไม่ได้หาร 2 หรือใช้ค่าเดียวกัน)
- [ ] ทุกแถวของ 3 การ์ด: `bytesUp + bytesDown == bytes` (ตรวจจาก JSON ดิบด้วย `curl`)
- [ ] Σ `bytes` ของ TopSources ≤ `observedBytes` และ `percent` ไม่เกิน 100
- [ ] สลับ window 1h ↔ 24h แล้วค่าเปลี่ยนสมเหตุผล (24h ≥ 1h เสมอ)
- [ ] Dashboard แท็บ Detailed (Protocol Breakdown / Top Talkers / Top Rules / Active Sessions)
      ให้ตัวเลขและ JSON เหมือนก่อนแก้ทุกฟิลด์ — ไม่มี regression
- [ ] real device: ดาวน์โหลดไฟล์ใหญ่ ~1 GB ลงเครื่องใน LAN 1 ครั้ง → เครื่องนั้นขึ้นอันดับ 1
      ใน Top Sources โดย **download ≫ upload** และยอดรวมไม่โตเป็น 2 เท่าของขนาดไฟล์
      (ยืนยัน invariant ไม่นับซ้ำ)
- [ ] real device: อัปโหลดไฟล์ใหญ่ออกจากเครื่องใน LAN → ทิศกลับด้านอย่างถูกต้อง
- [ ] real device: ปลายทางเดียวกันนั้นใน Top Destinations ต้องแสดง **ทิศตรงข้าม** กับแถวใน
      Top Sources (ยืนยันการ flip ของ §2.5)
- [ ] real device: `nmap` ยิงจากภายนอก → flow สั้นจำนวนมากยังถูกนับผ่าน event path,
      ไม่มี panic, `truncated` ขึ้นป้ายเตือน, CPU ไม่ค้าง
- [ ] ปล่อยรัน ≥ 1 ชั่วโมง → RSS ไม่โตต่อเนื่อง
- [ ] `-disable-edit=true` และ role read-only ยังเปิดหน้านี้ได้ครบ; logout แล้วเรียก
      `/api/statistics/traffic` ตรง ๆ → 401
- [ ] `go test -race ./...` และ `yarn build && yarn lint` ผ่านทั้งคู่
- [ ] 🔒 `git diff --stat` ไม่มี `real_firewall.go`, ไม่มี `kernel/interfaces.go`, ไม่มี `db/`
- [ ] ทุกอย่างอยู่บน branch `feat/statistics-updown-bytes` และเข้า main ผ่าน PR เท่านั้น
      (PR body ใส่ `Closes #107`)
