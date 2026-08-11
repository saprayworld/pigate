# Firewall Log Buffer: Capacity Usage + Oldest Entry — Work Plan

โจทย์ (GitHub issue #134):
1. หน้า **Statistics → Capacity** ให้เห็นว่า "พื้นที่ log ของ firewall" ถูกใช้ไปแล้วเท่าไหร่ (เช่น รับได้ 10,000 รายการ ใช้ไปแล้ว 5,490 รายการ)
2. หน้า **Log** (Forward Traffic / Local Traffic) ให้แสดง **เวลาของ log เก่าสุด** ที่ยังอยู่ในบัฟเฟอร์ จะได้รู้ว่าประวัติที่ระบบเก็บไว้ย้อนไปถึงเมื่อไหร่
3. (เพิ่มตามคำตอบเจ้าของโปรเจกต์) ความจุบัฟเฟอร์ต้อง **ปรับได้ผ่าน `pigate.conf`** ไม่ใช่ const ตายตัวอีกต่อไป

สถานะ: **ยังไม่เริ่ม** — รอ ai-developer ลงมือ T-00 → T-10 ตามลำดับ แล้วค่อยส่ง ai-qa ทดสอบรวมทีเดียว

## ข้อตัดสินจากเจ้าของโปรเจกต์ (ตอบแล้ว — ปิดประเด็น)

| ประเด็นที่เคยค้าง | คำตอบ | ผลต่อแผน |
|---|---|---|
| ความจุ log buffer ควรปรับได้ผ่าน `pigate.conf` หรือคง const ไว้? | **ให้ปรับได้ผ่าน `pigate.conf` ในงานนี้เลย** (ไม่ต้องแยกเป็นงานหลัง) | เพิ่ม **T-00** (config key ใหม่) และขยาย T-03/T-05/T-06/T-07 |
| ทำ banner เตือนตอนบัฟเฟอร์เริ่ม evict ไหม? | **ไม่ต้อง** — ใช้แค่ตัวเลข + สีสถานะ (truncated badge) ตามแผนเดิม | ไม่มี task เพิ่ม; ยืนยันว่า T-09/T-10 ห้ามใส่ banner/alert component |

ไม่มีประเด็นค้างที่ต้องให้เจ้าของโปรเจกต์ตัดสินเพิ่มแล้ว

## สภาพปัจจุบัน (ข้อเท็จจริงจากโค้ด ไม่ใช่การเดา)

- `backend/internal/logs/ringbuffer.go` มีเมธอด `Add / GetAll / Size() / LastMatchedByRule() / Capacity() / Clear / Subscribe` แล้ว — **มี `Size()` และ `Capacity()` อยู่แล้ว** แต่ยัง **ไม่มี** "เวลาของ entry เก่าสุด" และไม่มีตัวนับว่ามี entry ถูก evict ไปแล้วกี่รายการ
- `logs[]` ถูก append ตามลำดับเวลา (เก่า→ใหม่) และ evict หัวแถวเมื่อเต็ม ⇒ **entry เก่าสุดคือ `r.logs[0]`** ส่วน `GetAll()` คืนแบบ newest-first (copy ทั้งก้อน ~2.5 MB — ห้ามใช้เพื่ออ่านแค่ค่าเดียว)
- `Clear()` ใช้ `r.logs = r.logs[:0]` (ไม่คืน capacity ของ slice)
- ความจุคงที่ `trafficLogBufferCapacity = 10000` ประกาศเป็น const ใน `backend/cmd/pigate/main.go:46` และถูกใช้ที่ `main.go:114` (`logs.NewRingBuffer(trafficLogBufferCapacity)`) — **แผนนี้เปลี่ยนให้มาจาก config key ใหม่** (ดู T-00/T-05); comment ที่ `main.go:110-113` ยังบอกว่า "Capacity 500" ซึ่งล้าสมัยแล้ว ให้แก้พร้อมกัน
- `internal/config` (pure package) มี pattern ชัดเจนสำหรับ "คีย์ file-only ที่เป็น RAM-guard knob" อยู่แล้ว 10 คีย์ (`dns-stats-max-*`, `traffic-stats-max-*`, `deny-stats-max-*`, `ipinfo-enabled`): ประกาศ field ใน `Config` → ค่าใน `Defaults()` → const `key…` → ใส่ท้าย `orderedKeys` → `applyKey` (แปลงชนิด, **ไม่** เช็ค range) → range-sanity pass ใน `Resolve` (clamp + warning, ไม่ fatal) → `keyValue` สำหรับ `Write`
- `install.sh:334-341` เขียน `pigate.conf` เฉพาะ 4 คีย์ production (`mock`, `db`, `https-port`, `docker-compat`) เท่านั้น และ **ไม่แตะคีย์ tuning ใดๆ เลย** ⇒ **ไม่ต้องแก้ `install.sh`** ในงานนี้ (ตรวจแล้ว ยืนยัน)
- ไฟล์ที่ documented ทุกคีย์คือ `pigate.conf.example` (root ของ repo) + ย่อหน้า "Configuration File" ใน `README.md:185` (ปัจจุบันเขียนว่า "ten keys ... file-only" ⇒ ต้องกลายเป็น eleven)
- `api.Server` ถือ `logs *logs.RingBuffer` โดยตรง (`handlers.go:32`) และ handler อ่านตรงได้เลย (เช่น `HandleGetTrafficLogs` เรียก `s.logs.Capacity()`) ⇒ endpoint ใหม่ของหน้า Log **ไม่ต้องสร้าง service ใหม่**
- หน้า Capacity มีอยู่แล้วครบวงจร: `service/statistics_capacity.go` (ตาราง metadata + `GetCapacityStatistics`) → `api.HandleGetCapacityStatistics` → `GET /api/statistics/capacity` (`router.go:74`, authRoute) → `frontend/src/services/capacityService.ts` → `frontend/src/pages/StatisticsCapacity.tsx` โดยมี group `"firewall"` อยู่แล้ว (ตอนนี้มี 2 ring: denySources/denyPorts)
- **`StatisticsService` ยังไม่ถือ ring buffer** (`service/statistics.go:69`) — ต้องต่อเข้ามา และ `NewStatisticsService` มี 7 พารามิเตอร์แล้ว จึง **ห้ามเพิ่มพารามิเตอร์** ให้ใช้ setter แบบเดียวกับ `Server.SetPolicyStatsService` (`handlers.go:68`) ที่ main.go เรียกเพิ่มทีหลัง
- มีที่ hardcode เลข "10 rings" อยู่หลายจุด ต้องแก้ให้ครบเมื่อเพิ่ม ring ที่ 11:
  - `backend/internal/model/statistics.go:947` (comment "always exactly 10 entries")
  - `backend/internal/service/statistics_capacity.go:30,137`
  - `backend/internal/service/statistics_capacity_test.go:10,27,41,42`
  - `backend/internal/api/handlers_test.go:2148,2171,2182`
  - `docs/openapi.yaml:832` และ `frontend/public/openapi.yaml:832`
  - `frontend/src/services/capacityService.ts:48` (comment) + `mockRingSpecs`
  - `frontend/src/pages/StatisticsCapacity.tsx:26,203` ("ทั้ง 9 มิติ" — ข้อความเดิมก็ผิดอยู่แล้ว)
  - **ห้ามแก้ `backend/internal/api/dist/openapi.yaml`** — เป็น build artifact ที่ `build.sh` copy มาจาก `frontend/public/`
- หน้า Log ใช้คอมโพเนนต์ร่วมตัวเดียว `frontend/src/components/logs/TrafficLogPage.tsx` (ใช้โดย `pages/ForwardTraffic.tsx` และ `pages/LocalTraffic.tsx`) — แก้ที่เดียวได้ทั้งสองหน้า หัวการ์ดตอนนี้แสดงแค่ `{logs.length} entries` (บรรทัด 293-295) ซึ่งคือ "จำนวนแถวที่โหลดมาแสดง" **ไม่ใช่** จำนวนใน buffer
- `frontend/src/services/trafficLogService.ts` มี mock feed ของตัวเอง (`seedMockLogs`, cap 5000) ⇒ ข้อมูล usage ในโหมด mock ต้องสังเคราะห์ให้สอดคล้องกับ feed นั้น

## Design decisions

1. **ไม่ persist อะไรเพิ่ม** — ข้อมูลทั้งหมดอ่านสดจาก ring buffer ใน RAM (ตรงตาม tech_stack_design.md §8 เรื่องถนอม SD card)
2. **แยกเป็น 2 ทางส่งข้อมูล ไม่ยัดรวมกัน**
   - หน้า Capacity → เพิ่ม **ring ที่ 11** `firewall.logBuffer` ใน `/api/statistics/capacity` ที่มีอยู่แล้ว (ไม่เพิ่ม endpoint, หน้า Capacity ไม่ต้องยิง request เพิ่ม)
   - หน้า Log → เพิ่ม endpoint เล็กเฉพาะกิจ `GET /api/logs/traffic/usage` (payload ~5 ฟิลด์) เพราะหน้า Log ไม่ควรต้องดึง capacity ทั้ง 11 ring + series มาเพื่ออ่านค่าเดียว
3. **นับรวมทุก chain** — ring buffer เดียวเก็บ forward+input+output ปนกัน การนับ "ใช้ไปกี่รายการ" จึงเป็นเลขของทั้งบัฟเฟอร์ **ไม่ใช่ต่อหน้า** ⇒ UI ต้องเขียนกำกับให้ชัดว่าเป็นตัวเลขรวมทุก chain (กัน user เข้าใจผิดว่าเป็นตัวเลขของ Forward Traffic อย่างเดียว)
4. **เพิ่มตัวนับ `evicted`** ใน ring buffer (นับสะสมว่ามี entry เก่าถูกดันตกไปแล้วกี่รายการ นับตั้งแต่ boot/Clear ล่าสุด) — ทำให้ `truncated` ของ ring นี้เป็นสัญญาณจริง ("บัฟเฟอร์เต็มและเริ่มทิ้งของเก่าแล้ว") ไม่ใช่แค่เดาจาก `size == cap`; `Clear()` รีเซ็ตกลับเป็น 0
5. **`oldestEntry` เป็น string ตามที่เก็บ** (RFC3339 หรือ RFC3339Nano ปนกันได้ ตาม doc ของ `model.FirewallLog.Time`) — backend ไม่ normalize, ฝั่ง frontend ใช้ `new Date()` แสดงผลเวลาท้องถิ่น และต้องกันกรณี parse ไม่ผ่าน
6. **ความจุปรับได้ผ่าน config key ใหม่ `traffic-log-buffer-capacity`** (ตามคำตอบเจ้าของโปรเจกต์) — รายละเอียดการออกแบบคีย์อยู่ใน §"Config key ใหม่" ด้านล่าง
7. **มีผลเมื่อ restart เท่านั้น** — `logs.RingBuffer` กำหนดความจุตอน `NewRingBuffer()` และ ring ถูกสร้างก่อน service/subscriber ทั้งหมดใน `main.go` การเปลี่ยนความจุขณะรันจะต้อง re-allocate + จัดการ subscriber ที่ค้างอยู่ ซึ่งเกินขอบเขตและเสี่ยงเกินประโยชน์ ⇒ **คงรูปแบบ "apply config at startup" เหมือนคีย์อื่นทั้งหมดในโปรเจกต์** และต้องเขียนกำกับให้ชัดทั้งใน `pigate.conf.example`, README, openapi description และ tooltip/ข้อความบนหน้า Capacity ว่า *แก้ค่าแล้วต้อง `sudo systemctl restart pigate` ถึงมีผล*
8. **ไม่ทำ banner/alert เตือนตอน evict** (ตามคำตอบเจ้าของโปรเจกต์) — ใช้ตัวเลข + badge สถานะเท่านั้น
9. **ไม่แตะหน้า Event Logs** (`pages/EventLogs.tsx`) — คนละระบบ (system events, เก็บลง SQLite) ไม่อยู่ในขอบเขต issue นี้

## Config key ใหม่ (สรุปข้อกำหนด — ใช้อ้างอิงร่วมกันทุก task)

| หัวข้อ | ค่า |
|---|---|
| ชื่อคีย์ | `traffic-log-buffer-capacity` (kebab-case ตรงกับชื่อ const เดิม `trafficLogBufferCapacity`) |
| Field ใน `config.Config` | `TrafficLogBufferCapacity int` |
| ชนิด | int |
| Default | `10000` (ค่าเดิมของ const — พฤติกรรม out-of-the-box ต้องไม่เปลี่ยน) |
| ประเภท | **file-only** — ไม่ลงทะเบียน `flag.Int` ใน `main.go` (เป็น RAM-tuning knob ของ appliance เหมือน `deny-stats-max-*`/`traffic-stats-max-*` ไม่ใช่สวิตช์ที่ใช้ประจำวันบน CLI) |
| ช่วงที่ยอมรับ | **500 – 100000** (`minTrafficLogBufferCapacity` / `maxTrafficLogBufferCapacity`) |
| เหตุผลของช่วง | ที่ ~300-550 bytes/entry (ตัวเลขจาก comment เดิมใน `main.go`): ขั้นต่ำ 500 = อย่างน้อย 1 หน้าเต็มของหน้า Log (500 rows/page) และกันค่า 0/ติดลบ; เพดาน 100000 ≈ 30-55 MB worst case ซึ่งยังรับได้บน Pi 5 (ค่าที่สูงกว่านี้เสี่ยง OOM กับ service อื่นบนบอร์ด) |
| Validation | two-tier เหมือนคีย์ tuning อื่นทุกตัว: ค่าที่ **ไม่ใช่จำนวนเต็ม** = error fail-fast จาก `applyKey`; ค่าที่เป็นจำนวนเต็มแต่ **นอกช่วง** = clamp กลับเป็น default + warning ใน `Resolve` (**ห้าม fatal** — ค่าพิมพ์ผิดต้องไม่ทำให้ gateway boot ไม่ขึ้น) |
| ตำแหน่งใน `orderedKeys` | **ต่อท้ายสุด** ถัดจาก `deny-stats-max-ports` (เพื่อให้ไฟล์ config ที่ generate ไว้แล้ว diff นิ่ง) |
| capSource ของ ring `firewall.logBuffer` | สตริง `"traffic-log-buffer-capacity"` (ชี้คีย์จริงในไฟล์ config เหมือน ring อื่น — ไม่ต้องเขียนว่า compile-time constant อีกแล้ว) |
| การมีผล | ตอน startup เท่านั้น (ดู design decision 7) |

## Task list

> ทุก task ทำบน branch ใหม่ `feat/firewall-log-buffer-capacity` แตกจาก `main` และเข้า PR (ห้าม push โค้ดขึ้น main), **ห้าม commit เว้นแต่เจ้าของสั่ง**

| Task | Layer | ไฟล์หลัก | สรุป | depends_on |
|---|---|---|---|---|
| T-00 | config | `backend/internal/config/config.go` | คีย์ใหม่ `traffic-log-buffer-capacity` (default 10000, ช่วง 500-100000, clamp+warn) | — |
| T-01 | logs | `backend/internal/logs/ringbuffer.go` | `Usage()` + ตัวนับ `evicted` | — |
| T-02 | model | `backend/internal/model/statistics.go`, `backend/internal/model/types.go` | DTO `TrafficLogBufferUsage` + ฟิลด์ `oldestEntry` (optional) ใน `RingCapacity` | T-01 |
| T-03 | service | `backend/internal/service/statistics_capacity.go`, `statistics.go` | ring ที่ 11 `firewall.logBuffer` (capSource = `traffic-log-buffer-capacity`) + `SetLogBuffer()` | T-01, T-02 |
| T-04 | api | `backend/internal/api/handlers.go`, `router.go` | `GET /api/logs/traffic/usage` (authRoute) | T-01, T-02 |
| T-05 | wiring | `backend/cmd/pigate/main.go` | ใช้ `cfg.TrafficLogBufferCapacity` สร้าง ring + `statisticsService.SetLogBuffer(ringBuffer)` | T-00, T-03 |
| T-06 | test | `backend/internal/config/config_test.go`, `backend/internal/logs/ringbuffer_test.go`, `backend/internal/service/statistics_capacity_test.go`, `backend/internal/api/handlers_test.go` | เทสต์คีย์ใหม่ + `Usage()` + endpoint ใหม่ + 10→11 rings | T-00..T-05 |
| T-07 | docs | `pigate.conf.example`, `README.md`, `docs/openapi.yaml`, `frontend/public/openapi.yaml` | คีย์ใหม่ในไฟล์ตัวอย่าง/README + path & schema ใหม่, แก้ "10 rings" | T-00, T-03, T-04 |
| T-08 | frontend | `frontend/src/services/trafficLogService.ts`, `capacityService.ts` | API client + mock ทั้งสองฝั่ง | T-04, T-07 |
| T-09 | frontend | `frontend/src/components/logs/TrafficLogPage.tsx` | แถบสรุป "ใช้ไป X / <cap จาก API> · log เก่าสุด …" | T-08 |
| T-10 | frontend | `frontend/src/pages/StatisticsCapacity.tsx` | แสดง `oldestEntry` ในการ์ด + แก้ข้อความจำนวนมิติ | T-08 |

### T-00 (ใหม่) — config key `traffic-log-buffer-capacity`

```json
{
  "task_id": "T-00",
  "title": "เพิ่ม config key traffic-log-buffer-capacity ใน internal/config",
  "layer": "service",
  "files": ["backend/internal/config/config.go"],
  "instruction": "ใน backend/internal/config/config.go ให้เพิ่มคีย์ file-only ใหม่ชื่อ 'traffic-log-buffer-capacity' โดยเลียนแบบ pattern ของ deny-stats-max-sources/deny-stats-max-ports ทุกขั้นตอน ห้ามคิด pattern ใหม่: (1) เพิ่ม field `TrafficLogBufferCapacity int` ใน struct Config พร้อม comment ว่าเป็น file-only (ไม่มี CLI flag) และคุมความจุ ring buffer ของ traffic log (backend/internal/logs/ringbuffer.go ที่ main.go สร้าง) และเน้นว่ามีผลตอน startup เท่านั้น; (2) เพิ่มค่า 10000 ใน Defaults() พร้อม comment ว่าต้อง sync กับค่าเดิมของ const trafficLogBufferCapacity ใน cmd/pigate/main.go; (3) เพิ่ม const keyTrafficLogBufferCapacity = \"traffic-log-buffer-capacity\"; (4) เพิ่ม const minTrafficLogBufferCapacity = 500 และ maxTrafficLogBufferCapacity = 100000 พร้อม comment อธิบายที่มา (~300-550 bytes/entry, ขั้นต่ำ = 1 หน้าเต็มของหน้า Log 500 rows, เพดาน ≈ 30-55 MB worst case บน Pi 5); (5) ต่อท้าย orderedKeys เป็นตัวสุดท้าย (ถัดจาก keyDenyStatsMaxPorts) พร้อม comment อ้าง docs/ref/todo/firewall-log-buffer-capacity-plan.md T-00 ว่าเหตุใดจึงต่อท้ายแทนการเรียงตามตัวอักษร; (6) เพิ่ม case ใน applyKey ที่ทำแค่ strconv.Atoi + เก็บค่า พร้อม comment ว่า range check อยู่ใน Resolve ไม่ใช่ที่นี่; (7) เพิ่ม range-sanity pass ใน Resolve ต่อท้ายบล็อกของ DenyStatsMaxPorts: ถ้า cfg.TrafficLogBufferCapacity < minTrafficLogBufferCapacity || > maxTrafficLogBufferCapacity ให้ append warning รูปแบบเดียวกับคีย์อื่น ('traffic-log-buffer-capacity=%d out of range (%d..%d), using default %d') แล้ว clamp กลับเป็น defaults.TrafficLogBufferCapacity (ห้าม return error); (8) เพิ่ม case ใน keyValue คืน strconv.Itoa; (9) อัปเดต package doc comment ด้านบนไฟล์ ให้บอกว่าคีย์นี้ก็เป็น file-only แบบ two-tier validation เหมือนกลุ่ม *-max-* และเสริมว่าต่างจากคีย์อื่นตรงที่ค่านี้ถูกอ่านครั้งเดียวตอนสร้าง ring buffer ⇒ ต้อง restart service ถึงจะมีผล. ห้ามแก้ไฟล์อื่นใน task นี้ ห้ามลงทะเบียน flag ใน main.go",
  "acceptance": [
    "cd backend && go build ./... ผ่าน",
    "config.Defaults().TrafficLogBufferCapacity == 10000",
    "orderedKeys มี traffic-log-buffer-capacity เป็นตัวสุดท้าย และ Write ออกมาเป็นบรรทัดสุดท้ายของไฟล์",
    "Resolve กับค่า 'abc' คืน error, กับค่า 0/-1/100001 คืน warning + ค่าเป็น 10000, กับค่า 500 และ 100000 ผ่านโดยไม่มี warning",
    "ไม่มีการเพิ่ม flag ใหม่ใน cmd/pigate/main.go ใน task นี้"
  ],
  "depends_on": []
}
```

### T-05 (แก้ไข) — wiring ใน main.go

```json
{
  "task_id": "T-05",
  "title": "main.go: สร้าง ring buffer จาก config + ต่อ ring เข้า StatisticsService",
  "layer": "service",
  "files": ["backend/cmd/pigate/main.go"],
  "instruction": "ใน backend/cmd/pigate/main.go: (1) เปลี่ยน const trafficLogBufferCapacity (บรรทัด ~46) ให้เหลือบทบาทเป็นเพียง 'ค่าอ้างอิงของเอกสาร' หรือ **ลบทิ้ง** แล้วให้ความจุจริงมาจาก cfg.TrafficLogBufferCapacity (ค่าที่ผ่าน config.Resolve แล้ว) — เลือกทางลบทิ้งถ้าไม่มีที่อื่นอ้างถึง (ตรวจด้วย grep ก่อน; comment ใน internal/api/handlers.go:760 อ้างชื่อนี้แบบข้อความเฉยๆ ให้แก้ข้อความให้ชี้ไปที่คีย์ traffic-log-buffer-capacity แทน หากลบ const); (2) แก้บรรทัดสร้าง ring (~114) เป็น logs.NewRingBuffer(cfg.TrafficLogBufferCapacity) โดยต้องอยู่หลังจุดที่ cfg ถูก resolve แล้ว (ตรวจลำดับจริงในไฟล์ ห้ามย้ายบล็อกอื่น); (3) แก้ comment เหนือบรรทัดนั้นที่ยังเขียนว่า 'Capacity 500' ให้ตรงความจริง: ความจุมาจากคีย์ traffic-log-buffer-capacity (default 10000, ช่วง 500-100000) และ **มีผลเมื่อ restart เท่านั้น**; (4) เพิ่มการเรียก statisticsService.SetLogBuffer(ringBuffer) ในจุดเดียวกับที่มีการเรียก setter อื่นของ service หลังสร้าง service เสร็จ (เลียนแบบ SetPolicyStatsService). ห้ามเพิ่ม flag.Int สำหรับคีย์นี้ (เป็น file-only โดยเจตนา) ห้ามเปลี่ยนลำดับ startup sequence อื่น",
  "acceptance": [
    "cd backend && go build ./... && go vet ./... ผ่าน",
    "ไม่มีเลข 10000 ตายตัวเหลือใน main.go สำหรับความจุ ring (ค่ามาจาก cfg เท่านั้น)",
    "รัน ./pigate-backend -mock=true ด้วยไฟล์ config ที่มี traffic-log-buffer-capacity=1500 แล้ว GET /api/logs/traffic/usage คืน capacity=1500",
    "ไม่มี flag ใหม่ใน -h output"
  ],
  "depends_on": ["T-00", "T-03"]
}
```

### T-03 (แก้ไขเฉพาะจุด)

- `capacityMetaLogBuffer` ต้องใช้ `capSource: "traffic-log-buffer-capacity"` (ไม่ใช่ข้อความว่ามาจาก compile-time constant ตามที่แผนเดิมเขียนไว้)
- `entryBytes` ให้ใช้ค่าประมาณ **400** (ช่วงจริง 300-550 ตาม comment เดิมใน `main.go`) และเขียน comment กำกับที่มาไว้ในตาราง metadata เหมือน ring อื่น
- ค่า `cap` ของ ring นี้ต้องอ่านสดจาก `ringBuffer.Capacity()` (ซึ่งมาจาก config ผ่าน `main.go`) **ห้าม** import `internal/config` เข้ามาใน service layer

### T-06 (แก้ไข — ขยายขอบเขต) — เพิ่มเทสต์ของ config

- `backend/internal/config/config_test.go`: เพิ่ม `TestResolve_TrafficLogBufferCapacity` เลียนแบบ `TestResolve_DenyStatsMaxSourcesPorts` ครบ 4 subtest — defaults (10000, ไม่มี warning) / file override (เช่น 20000) / out-of-range clamp+warn (ทดสอบทั้ง `0`, `-1`, `499`, `100001`) / non-integer เป็น error
- เพิ่มค่าคีย์ใหม่ลงใน `TestWriteParseRoundTrip` (บรรทัด ~305-319 ตั้ง `cfg.TrafficLogBufferCapacity = 20000`) และตรวจว่า round-trip กลับมาเท่าเดิม
- ถ้ามีเทสต์ที่ assert จำนวน `KnownKeys()` หรือจำนวนบรรทัดของ `Write` ต้องอัปเดตด้วย (ให้ grep ก่อนแก้)
- ส่วนที่เหลือของ T-06 คงเดิม: เทสต์ `Usage()`/`evicted`/`Clear()` ใน `ringbuffer_test.go`, endpoint ใหม่ใน `handlers_test.go`, และแก้ 10→11 rings ในทั้ง `statistics_capacity_test.go` และ `handlers_test.go`

### T-07 (แก้ไข — ขยายขอบเขต) — เอกสารคีย์ใหม่

- `pigate.conf.example`: เพิ่มบล็อกท้ายไฟล์ (ต่อจาก `deny-stats-max-ports`) — คอมเมนต์อธิบายว่าเป็นความจุ ring buffer ของ traffic log (รวมทุก chain: forward/input/output), default `10000`, ช่วง 500-100000, ประมาณการ RAM ~300-550 B/entry, validation แบบ clamp+warn, และ **ประโยคเด่นว่าต้อง `sudo systemctl restart pigate` ถึงจะมีผล** (ต่างจากคีย์อื่นที่อ่านสดตอน startup เหมือนกันแต่ไม่มี UI แสดงค่า) แล้วปิดท้ายด้วยบรรทัด `traffic-log-buffer-capacity=10000`
- `README.md` ย่อหน้า "Configuration File": เปลี่ยน "ten keys" → "eleven keys" และเพิ่ม `traffic-log-buffer-capacity` เข้าไปในรายการ พร้อมประโยคสั้นๆ ว่าคุมความจุ log buffer ของหน้า Forward/Local Traffic + Statistics ▸ Capacity, default 10000, ช่วง 500-100000, ต้อง restart ถึงมีผล
- `docs/openapi.yaml` + `frontend/public/openapi.yaml`: ใน description ของ ring `firewall.logBuffer` และของ `GET /api/logs/traffic/usage` ให้ระบุว่าความจุมาจากคีย์ `traffic-log-buffer-capacity` ใน `pigate.conf` และเปลี่ยนได้เมื่อ restart เท่านั้น (**ห้ามแก้ `backend/internal/api/dist/openapi.yaml`**)
- **ไม่ต้องแก้ `install.sh`** — ตรวจแล้วว่า seed เฉพาะ `mock`/`db`/`https-port`/`docker-compat` ไม่เคย seed คีย์ tuning ใดเลย (คีย์ใหม่จะใช้ code default อัตโนมัติ); ถ้า ai-developer พบว่าโค้ดจริงต่างจากนี้ให้หยุดและรายงานกลับ แทนที่จะแก้เอง
- **ไม่ต้องแก้ `CLAUDE.md`** — คีย์นี้ไม่มี CLI flag จึงไม่เข้าเงื่อนไขบรรทัด "Other relevant main.go flags" (ถ้า ai-developer เห็นว่าจำเป็นจริง ให้เสนอ ไม่ใช่แก้เอง)

### T-09 / T-10 (แก้ไขเฉพาะจุด)

- **ห้าม hardcode `10,000`** ในข้อความ UI ทุกจุด — ต้องใช้ `capacity` ที่ได้จาก API เสมอ (เพราะผู้ใช้ตั้งค่าเองได้แล้ว) รวมถึง mock ใน `capacityService.ts`/`trafficLogService.ts` ที่ให้ใช้ค่าคงที่ของ mock เองได้แต่ต้องส่งผ่านฟิลด์ `capacity` ไม่ใช่ฝังในสตริง
- หน้า Capacity: ในการ์ด `firewall.logBuffer` ให้แสดง capSource (`traffic-log-buffer-capacity`) ตาม pattern ที่การ์ดอื่นแสดงอยู่แล้ว และเพิ่มข้อความ/tooltip สั้นๆ ว่าแก้ค่าในไฟล์ config แล้วต้อง restart service
- **ห้ามใส่ banner/alert เตือน evict** (เจ้าของโปรเจกต์ตัดสินแล้วว่าไม่เอา) — ใช้เพียงตัวเลข + badge `truncated`

รายละเอียด instruction/acceptance เต็มของ T-01, T-02, T-04, T-08 อยู่ในรายงานของ ai-tech-lead ที่ส่งให้ coordinator (ไม่เปลี่ยนจากเดิม)

## Final acceptance (ทดสอบรวมครั้งเดียวหลังทำครบ T-00–T-10)

1. `cd backend && go build ./... && go vet ./... && go test ./...` ผ่านทั้งหมด (ถ้าสภาพแวดล้อมมี gcc ให้รัน `-race` ด้วย)
2. `cd frontend && yarn build && yarn lint` ผ่าน ไม่มี TS error
3. รัน `./pigate-backend -mock=true`: `GET /api/statistics/capacity` คืน **11 rings** เรียงตามลำดับคงที่ โดยตัวสุดท้ายคือ `firewall.logBuffer` (group `firewall`, kind `flat`, `capSource` = `traffic-log-buffer-capacity`, `cap` = 10000, `current` = จำนวน entry จริงในบัฟเฟอร์, `currentPercent` สอดคล้องกัน)
4. `GET /api/logs/traffic/usage` คืน 200 พร้อม `{used, capacity, usedPercent, oldestEntry, newestEntry, evicted}` โดย `used` ตรงกับจำนวน entry ที่ `GET /api/logs/traffic?limit=1000` สื่อถึง และ `oldestEntry` เป็นเวลาก่อน `newestEntry` เสมอ
5. ทั้งสอง endpoint ต้อง **401** เมื่อเรียกโดยไม่มี session (เหมือน route อื่นใน `/api/statistics/*`)
6. หลังยิง `POST /api/dashboard/logs/clear`: `used = 0`, `evicted = 0`, `oldestEntry`/`newestEntry` เป็นค่าว่าง และหน้า Log แสดงผลว่างโดยไม่ crash
7. หน้า Forward Traffic และ Local Traffic แสดงแถบ "ใช้ไป X / <capacity จาก API> (Y%) · log เก่าสุด …" ถูกต้อง, ตัวเลขอัปเดตเองตาม poll, และหยุด/กลับมาอัปเดตสอดคล้องกับปุ่ม Pause/Resume
8. หน้า Statistics → Capacity แสดงการ์ด "Firewall Traffic Log Buffer" ในกลุ่ม Firewall พร้อมบรรทัดเวลา log เก่าสุด, capSource ชี้ที่ `traffic-log-buffer-capacity`, และ badge สถานะเปลี่ยนเป็นแดงเมื่อ `truncated=true` (ไม่มี banner/alert ใดๆ เพิ่ม)
9. **config key ใหม่ — ค่าปกติ**: สร้างไฟล์ config ที่มี `traffic-log-buffer-capacity=1200` แล้วรัน `./pigate-backend -mock=true -config=<file>`: `GET /api/logs/traffic/usage` คืน `capacity=1200` และ ring `firewall.logBuffer` ใน `/api/statistics/capacity` คืน `cap=1200` เท่ากัน; หน้า Log/Capacity แสดงเลข 1,200 (ไม่ใช่ 10,000 ที่ฝังไว้)
10. **config key ใหม่ — ค่านอกช่วง**: ตั้ง `traffic-log-buffer-capacity=0` (และทดสอบซ้ำด้วย `499`, `100001`) แล้วรันใหม่ → process **ต้อง boot ขึ้นได้ปกติ**, log มี warning "out of range ... using default 10000", และ capacity จริงเป็น 10000
11. **config key ใหม่ — ค่าผิดชนิด**: ตั้ง `traffic-log-buffer-capacity=abc` → process ออกด้วย error ชัดเจน (fail-fast) ตาม pattern เดียวกับ `port=abc`
12. **config key ใหม่ — ไม่มีคีย์ในไฟล์**: ไฟล์ config เดิมที่ไม่มีคีย์นี้เลย ต้องทำงานเหมือนเดิมทุกประการ (capacity = 10000) และไฟล์ที่ถูก auto-generate ใหม่ (ไม่ระบุ `-config`) ต้องมีบรรทัด `traffic-log-buffer-capacity=10000` เป็นบรรทัดสุดท้าย
13. เอกสารครบ: `pigate.conf.example` มีคีย์ใหม่พร้อมคำอธิบาย + ข้อความว่าต้อง restart, README ระบุ "eleven keys" และรวมคีย์ใหม่แล้ว, `docs/openapi.yaml` และ `frontend/public/openapi.yaml` ตรงกัน (diff เท่ากัน) และ `backend/internal/api/dist/openapi.yaml` **ไม่ถูกแก้**
14. `install.sh` ไม่มีการเปลี่ยนแปลง (คีย์ tuning ไม่เคยถูก seed) — ยืนยันด้วย `git diff --stat`
15. ทดสอบโหมด mock ของ frontend (`IS_MOCK_MODE`) ว่าไม่มีหน้าไหนพัง และตัวเลข mock สมเหตุสมผล
16. ตรวจสายตา: dark/light mode ปกติ, ไม่มี `shadow-*`/`backdrop-blur-*`, ไม่มี Tailwind color class ดิบ (ใช้ตัวแปรธีมเท่านั้น)
17. ไม่มีการเพิ่ม `exec.Command`, ไม่มีการเขียน SQLite เพิ่ม, ไม่มี goroutine/ticker ใหม่ฝั่ง backend, และไม่มี CLI flag ใหม่ใน `-h`
