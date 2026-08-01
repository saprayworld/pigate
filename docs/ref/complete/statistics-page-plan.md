# Statistics Page — Top Source / Top Destination / Top Conversation

> เอกสารแผนงานสำหรับหน้าใหม่ "Statistics" (Top Source, Top Destination, Top
> Conversation, Top Denied) โดย **ไม่เพิ่ม kernel capability ใหม่แม้แต่ตัวเดียว** —
> ข้อมูลทุกอย่างมาจากท่อที่ PR #101/#103/#105 วางไว้แล้ว (conntrack poll +
> DESTROY event) และจาก NFLOG ring buffer ที่มีอยู่
>
> วันที่เขียน: 2026-07-30 · Branch ที่จะใช้: `feat/statistics-page` (ตั้งต้นจาก `main`)
> README Feature Status: เพิ่มแถวใหม่ "Statistics" (Frontend/Backend = Completed เมื่อจบแผนนี้)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
- หน้าใหม่ `/logs/statistics` แสดงอันดับ (top-N) ของทราฟฟิกในหน้าต่างเวลา 1h/24h:
  Top Source Hosts, Top Destinations, Top Conversations (src → dst:port),
  Top Denied Sources/Ports
- ใช้ endpoint เดียว `GET /api/statistics/traffic?window=1h|24h` (อ่านอย่างเดียว)
- ทำงานได้ทั้งโหมด `-mock=true` และของจริง โดยไม่ต้องแก้ `kernel/interfaces.go`
  และ **ไม่แตะ `real_firewall.go` เลย**

**นอกขอบเขต (ตัดชัดเจน)**
- **Top Queried Domains (DNS)** — สำรวจแล้ว: ไม่มีท่อเก็บ query log ของ dnsmasq ในระบบ
  (`kernel/dnsmasq_base.go` ไม่ได้อ่าน log และ `DNSServerManager` มีแค่
  `ApplyZones`/`ClearCache`) จะทำได้ต้องเปิด `log-queries` แล้วอ่านไฟล์/journal เพิ่ม
  → เป็นงานคนละก้อน ให้เป็น Phase ถัดไปถ้าเจ้าของโปรเจกต์ต้องการ
- **แยก byte เป็น upload/download ต่อ host** — ทำได้ (conntrack มี Forward/Reverse
  แยกกันอยู่แล้ว แต่ `flowsToSamples` บวกรวมทิ้งที่ `real_traffic_account.go:95`)
  แต่ต้องแก้ baseline ของ `onFlowEnd` เป็น 2 ชุด ซึ่งเสี่ยงต่อ invariant "ห้ามนับซ้ำ"
  ของ Phase 2 → **เลื่อนไป Phase ถัดไป** — ทำแล้วใน
  `docs/ref/todo/statistics-split-upload-download-bytes-plan.md` (Issue #107)
- **SrcPort ใน `model.FlowSample`** — ไม่มีในโครงสร้างปัจจุบัน จึงไม่ทำ session row
  แบบ 5-tuple เต็ม ใช้ 4-tuple (src, dst, proto, dstPort) แทน ซึ่งเป็นสิ่งที่ผู้ใช้
  อยากเห็นจริง (src port เป็น ephemeral ไม่มีความหมายในการจัดอันดับ)
- ไม่ persist อะไรลง SQLite เพิ่มเลย (SD card wear — `tech_stack_design.md` §8)
- ไม่แก้การ์ดเดิมในแท็บ Detailed ของ Dashboard (Top Talkers เดิมยังอยู่เหมือนเดิม)

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-07-30)

| ข้อมูลที่มี | มีอะไรบ้าง | อยู่ที่ไหน | พอทำ Top อะไรได้ |
|---|---|---|---|
| conntrack flow (poll ทุก 10s) | `Key`, `SrcIP`, **`DstIP`**, `Proto`, `DstPort`, `Bytes` | `model/types.go:615`, `kernel/real_traffic_account.go:83-106` | Source ✅, Destination ✅ (ข้อมูลมีแต่ **service ทิ้ง**), Conversation ✅ |
| conntrack DESTROY event | `model.FlowSample` ชุดเดียวกัน | `kernel/real_conntrack_events.go`, `service/traffic_stats.go:323 onFlowEnd` | เติมความแม่นให้ทั้ง 3 อันข้างบน |
| aggregation ที่มีอยู่ | bucket ring 5 นาที × 288 (24h) เก็บ `hostBytes` / `catBytes` / `ruleBytes` | `service/traffic_stats.go:77`, `:632 addBucket` | มีแค่ **hostBytes (= Top Source)**; ไม่มี dst, ไม่มี conversation |
| nftables rule counter (แม่นยำจริง) | bytes/packets ต่อ DB rule id | `real_traffic_account.go:151` | Top Rules (มีแล้วบน Dashboard) |
| NFLOG log ring (10,000 entries) | `Action`, `Src`, `Dest`, `Port`, `Proto`, `Chain`, `InIface/OutIface`, `Time` | `model/types.go:385`, `logs/ringbuffer.go`, ป้อนที่ `cmd/pigate/main.go:390 stampAndPush` | Top Denied Source / Denied Port (นับ **จำนวน event** ไม่ใช่ byte) |
| session count | total/max จาก /proc + per-proto จาก dump ล่าสุด | `service/traffic_stats.go:205 sampleSessions` | จำนวน session รวม (มีบน Dashboard แล้ว) |
| DHCP lease/reservation | IP → hostname/MAC | `service/traffic_stats.go:854 hostLookup`, `:874 hostnameFor` | ใส่ชื่ออุปกรณ์ให้ทุกการ์ดได้ (ฟังก์ชันอยู่ package `service` เดียวกัน เรียกซ้ำได้เลย) |

**สรุปการประเมิน**
1. **Top Source — ทำได้ทันที** ของมีครบ (`hostBytes`) แค่ยกออกมาแสดงในหน้าใหม่
2. **Top Destination — ทำได้ ต้องเพิ่มโค้ดนิดเดียว** `FlowSample.DstIP` ถูกส่งมาถึง
   service แล้วแต่ `processFlows` (`traffic_stats.go:381`) ไม่เคยใช้ → เพิ่ม map
   `dstBytes` ใน bucket
3. **Top Conversation — ทำได้ ต้องเพิ่ม aggregation ใหม่** ปัจจุบัน `flowState` เก็บแค่
   `bytes`/`misses` ต่อ key ที่เป็น **hash** จึงย้อนกลับเป็น tuple ไม่ได้ → ต้อง
   aggregate จาก `FlowSample` ตรง ๆ ตอน `processFlows`/`onFlowEnd` เข้า map ใหม่
   `convBytes` (key = `src|dst|proto|dstPort`)
4. **Top Denied — ทำได้ แต่เป็นตัวเลข "สุ่มตัวอย่าง"** rule ที่ log ถูกใส่
   `expr.Limit` ไว้ทั้งหมด (3 pkt/min ที่ `real_firewall.go:331`, **10 pkt/s** ที่ตัว
   final drop log `:400`) → ใช้จัดอันดับได้ ใช้เป็นจำนวนแพ็กเก็ตจริงไม่ได้
   **ต้องมีป้ายกำกับใน UI** และห้ามลด limit เพื่อความแม่น (จะกลายเป็นช่องทาง log flood)
5. **ไม่ต้องเพิ่ม kernel capability ใหม่เลย** → ไม่มีไฟล์ `real_*.go` / `mock.go`
   คู่ใหม่ ไม่มีการเพิ่ม syscall/socket ใหม่ → ความเสี่ยงด้านความปลอดภัยของแผนนี้ต่ำ
   (งาน sensitive มีแค่จุดเดียว: input `window` ที่ handler — ดู §5 ข้อ 1)

## 2. สถาปัตยกรรมที่เลือก

```
NFLOG watcher (main.go stampAndPush) ──► StatisticsService.RecordFirewallLog()  [deny ring, RAM]
                                                     │
conntrack poll / DESTROY event ──► TrafficStatsService (bucket ring เดิม + dst/conv ใหม่)
                                                     │
                                    StatisticsService.GetStatistics(window)  ◄── DHCP lease/reservation
                                                     │
                                  GET /api/statistics/traffic?window=1h|24h
                                                     │
                                        frontend/src/pages/Statistics.tsx
```

**เหตุผลของการแบ่งไฟล์**
- `service/traffic_stats.go` ยาว ~900 บรรทัดแล้ว → เพิ่มเฉพาะ *แหล่งข้อมูล*
  (2 map ใน bucket + 1 accessor) ส่วน *การประกอบร่าง* ไปอยู่ไฟล์ใหม่
  `service/statistics.go`
- deny ring แยกเป็นของ `StatisticsService` เอง เพราะมาจากคนละแหล่ง (NFLOG) และ
  หน่วยคนละอย่าง (จำนวน event ไม่ใช่ byte) — ไม่ควรปนใน bucket เดียวกับ byte
- **ไม่ใช้ `RingBuffer.GetAll()` มานับตอนมี request** เพราะ ring เก็บแค่ 10,000 entry
  (`main.go:46`) ตอนโดนสแกนจะครอบคลุมแค่ไม่กี่นาที และการ copy 10k struct ทุก
  request เป็นภาระเปล่า ๆ → ใช้ hook นับตอน event เข้ามาแทน (O(1) ต่อ event)

**endpoint เดียว vs หลาย endpoint:** เลือก endpoint เดียวแบบเดียวกับ
`/api/dashboard/traffic-detail` เพราะหน้าเดียวโหลดครั้งเดียว ทุกการ์ดใช้ window
เดียวกัน และเปอร์เซ็นต์ทุกใบต้องคิดจาก `observedBytes` ก้อนเดียวกันจึงจะไม่ขัดกันเอง

## 3. ขั้นตอนการทำ (เรียงตาม dependency)

### T-01 — DTO ของหน้า Statistics
**ไฟล์ใหม่:** `backend/internal/model/statistics.go`
สร้าง struct (แยกไฟล์ ไม่ยัดใน `types.go` ที่ยาวมากแล้ว):
```go
type TopHost struct {          // ใช้ทั้ง Source และ Destination
    IP, Hostname, MAC string
    Bytes   uint64
    Percent float64
    Private bool               // RFC1918 / link-local / ULA → UI แยกสี LAN vs Internet
}
type TopConversation struct {
    SrcIP, SrcHostname, DstIP, DstHostname string
    Proto   string             // "TCP"/"UDP"/"ICMP"/"IP:<n>"
    DstPort uint16
    Bytes   uint64
    Percent float64
}
type TopDeniedSource struct { IP, Hostname string; Count uint64; Percent float64 }
type TopDeniedPort   struct { Proto string; Port string; Count uint64; Percent float64 }
type TrafficStatistics struct {
    Window           string   // "1h" | "24h"
    ObservedBytes    uint64
    Accuracy         string   // "estimated" | "near-exact" (ค่าเดียวกับ TrafficDetail)
    TopSources       []TopHost
    TopDestinations  []TopHost
    TopConversations []TopConversation
    DeniedSources    []TopDeniedSource
    DeniedPorts      []TopDeniedPort
    DeniedSampled    bool     // true เสมอ — nftables log limit (ดู §1 ข้อ 4)
    DeniedEvents     uint64   // จำนวน event ที่นับได้จริงในหน้าต่างนี้
    Truncated        bool     // true เมื่อ bucket ใดชนเพดาน cap
    GeneratedAt      string
}
```
ทุกฟิลด์ต้องมี json tag เป็น camelCase ตามแบบ `types.go`
**acceptance:** `go build ./...` ผ่าน, ไม่มีการแก้ struct เดิม

### T-02 — เก็บ Destination + Conversation ในบัคเก็ตเดิม
**ไฟล์:** `backend/internal/service/traffic_stats.go`
1. เพิ่ม const: `maxTrackedDests = 300`, `maxTrackedConversations = 200`,
   `statsTopN = 10` (คงค่า `topN` เดิมไว้ ห้ามแก้ เพราะการ์ด Dashboard ใช้อยู่)
2. `trafficDetailBucket` (`:77`) เพิ่ม `dstBytes map[string]uint64` และ
   `convBytes map[string]uint64` (key = `srcIP|dstIP|proto|dstPort`)
3. `addBucket` (`:632`) รับ 2 map เพิ่ม แล้ว merge ด้วย `mergeUint64Map` พร้อม cap
   ข้างบน (ตัว `mergeUint64Map` `:669` รองรับ capAt อยู่แล้ว ไม่ต้องแก้)
4. `processFlows` (`:381`) — ในลูปเดิม จุดที่คำนวณ `d := uint64(delta)` แล้ว
   **ใช้ `d` ตัวเดิมนั้น** เติมทั้ง `dstDeltas[f.DstIP]` และ
   `convDeltas[convKey(f)]` (ห้ามคำนวณ delta ใหม่แยก — ดู §5 ข้อ 2)
5. `onFlowEnd` (`:323`) — ใช้ `delta` ตัวเดียวกับที่ให้ hostDeltas เติม dst/conv ด้วย
   ใน `addBucket` **ครั้งเดียว** เหมือนเดิม
6. เพิ่ม accessor ใหม่ (อ่านอย่างเดียว, **ต้องรวมยอดใต้ `s.mu.RLock()` ทั้งก้อน**
   ตามคำเตือนที่ `:687-704`):
   ```go
   type TrafficBreakdown struct {
       Hosts, Dests, Convs map[string]uint64
       Observed  uint64
       Truncated bool
       Accuracy  string
   }
   func (s *TrafficStatsService) GetTrafficBreakdown(window string) TrafficBreakdown
   ```
   `Truncated` = true เมื่อ bucket ใดใน window มี `len(hostBytes) >= maxTrackedHosts`
   หรือ dst/conv ชนเพดานของตัวเอง
7. `GetTrafficDetail` (`:705`) **ห้ามเปลี่ยน response** — การ์ด Dashboard เดิมต้องเหมือนเดิมเป๊ะ
**acceptance:** `go build ./...` ผ่าน; `GetTrafficDetail` ยัง return ฟิลด์เดิมครบ

### T-03 — StatisticsService (ไฟล์ใหม่)
**ไฟล์ใหม่:** `backend/internal/service/statistics.go`
- struct ถือ `*TrafficStatsService`, `*db.Repository`, `kernel.DhcpManager`
- deny ring: bucket 5 นาที × 288 เหมือนกัน แต่เก็บ
  `srcCount map[string]uint64` (cap 500) และ `portCount map[string]uint64`
  (key = `"TCP/443"`, cap 300) — RAM ล้วน
- `RecordFirewallLog(entry model.FirewallLog)` — เรียกจาก NFLOG hook:
  นับเฉพาะ `entry.Action == "DROP"`, ต้อง **ไม่ block และเร็วมาก** (mutex + map
  increment เท่านั้น ห้าม I/O ห้าม log ต่อ event)
- `GetStatistics(window string) model.TrafficStatistics` — เรียก
  `GetTrafficBreakdown(window)` + รวม deny bucket ของ window เดียวกัน + เติมชื่อ
  ด้วย `hostnameFor()` (`traffic_stats.go:874`, package เดียวกัน) + คิด `%` ด้วย
  `percentOf()` (`:843`) + จัดเรียง top-N แบบ deterministic (bytes desc, ตามด้วย key asc
  เพื่อกัน flaky test เหมือน `buildTopTalkers`)
- helper `isPrivateIP(string) bool` ใช้ `net/netip` ของ stdlib
  (`IsPrivate()/IsLoopback()/IsLinkLocalUnicast()`) — ห้ามเขียน regex เอง
**acceptance:** `go build ./...` ผ่าน; ไม่มี call ไปยัง kernel layer โดยตรงจาก service นี้
นอกจากผ่าน `TrafficStatsService`/`DhcpManager` ที่มีอยู่

### T-04 — unit test
**ไฟล์ใหม่:** `backend/internal/service/statistics_test.go`
(+ เพิ่มเคสใน `backend/internal/service/traffic_stats_test.go`)
เคสบังคับ:
1. poll 2 รอบด้วย flow เดิม → `TopSources`/`TopDestinations`/`TopConversations`
   ได้ยอด **delta** ไม่ใช่ยอดสะสม และผลรวมของทั้งสามอันเท่ากับ `ObservedBytes`
2. `onFlowEnd` หลัง poll เห็นแล้ว → dst/conv **ไม่นับซ้ำ** (ยอดรวม = ค่าสุดท้ายของ flow)
3. flow ที่เกิด+ตายระหว่าง tick (มีแต่ event) → ขึ้นครบทั้ง 3 การ์ด
4. ชน cap: ยิง flow 1000 conversation → ไม่ panic, `Truncated == true`,
   จำนวน key ต่อ bucket ไม่เกินเพดาน
5. `RecordFirewallLog`: นับเฉพาะ DROP, จัดอันดับถูก, `PASS` ไม่ถูกนับ
6. `go test -race ./internal/service/...` ผ่าน โดยมี goroutine เรียก
   `poll()` + `RecordFirewallLog()` + `GetStatistics()` พร้อมกัน
**acceptance:** `cd backend && go test -race ./internal/service/...` ผ่าน

### T-05 — API handler + route 🔒 (จุด input validation เดียวของแผนนี้)
**ไฟล์:** `backend/internal/api/handlers.go`, `backend/internal/api/router.go`
- เพิ่มฟิลด์ `statistics *service.StatisticsService` ใน `Server` (`handlers.go:31-53`)
  และพารามิเตอร์ท้ายสุดของ `NewServer` (`:62-114`)
- `HandleGetStatistics`: whitelist `window` แบบ **ลอกจาก `HandleGetTrafficDetail`
  (`:401-407`) เป๊ะ ๆ** — ค่าอื่นทั้งหมด (รวมค่าว่าง) ตกเป็น `"1h"` ห้ามส่งสตริงดิบ
  จาก client เข้า service
- `router.go` (`:37-44` กลุ่ม dashboard) เพิ่ม
  `authRoute("GET /api/statistics/traffic", s.HandleGetStatistics)`
  — **ต้องเป็น `authRoute`** (ไม่ใช่ public, ไม่ใช่ superAdmin) และเป็น GET
  จึงไม่โดน `DisableEditMiddleware`/`RoleReadOnlyMiddleware`
- **ต้องแก้ call site ของ `NewServer` ในเทสต์ด้วย 5 จุด**:
  `backend/internal/api/handlers_test.go` บรรทัด ~68, ~369, ~557, ~670, ~863
**acceptance:** `cd backend && go build ./... && go test ./internal/api/...` ผ่าน

### T-06 — wiring ใน main.go
**ไฟล์:** `backend/cmd/pigate/main.go`
1. หลัง `trafficStatsService := ...` (`:212`) สร้าง
   `statisticsService := service.NewStatisticsService(trafficStatsService, repo, dhcp)`
2. ส่งเข้า `api.NewServer(...)` (`:344`) เป็นพารามิเตอร์ท้ายสุด
3. ใน `stampAndPush` (`:390`) เพิ่ม `statisticsService.RecordFirewallLog(entry)`
   **หลัง** `ringBuffer.Add(entry)` — closure นี้ถูกเรียกจาก NFLOG read loop
   จึงต้องเบาและห้าม panic (ดู §5 ข้อ 4)
> **ไม่ต้อง** `InitApplyConfig()` และไม่ต้องเพิ่ม goroutine ใหม่ — service นี้ไม่มี
> state ที่ต้อง apply ลง kernel และไม่มี ticker ของตัวเอง (อาศัย ticker เดิมของ
> `TrafficStatsService`) จึงไม่กระทบลำดับ boot
**acceptance:** `-mock=true` แล้ว `curl /api/statistics/traffic` ได้ JSON ที่ตัวเลขขยับ

### T-07 — API contract
**ไฟล์:** `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (ต้องเหมือนกันเป๊ะ)
เพิ่ม path `/statistics/traffic` + schema `TrafficStatistics`/`TopHost`/
`TopConversation`/`TopDeniedSource`/`TopDeniedPort` พร้อมคำอธิบายว่า deny เป็น
**ค่าสุ่มตัวอย่างเพราะ nftables log rate limit**
> **ไม่ต้อง** แก้ `backend/internal/api/dist/openapi.yaml` (build artifact ที่ `build.sh` ทับให้)
**acceptance:** ทั้งสองไฟล์ diff ตรงกัน, หน้า ApiDocs เรนเดอร์ไม่ error

### T-08 — frontend API client
**ไฟล์ใหม่:** `frontend/src/services/statisticsService.ts`
- ลอกโครงจาก `frontend/src/services/dashboardService.ts` (type + fetch + **mock branch**)
- type ตรงกับ T-01 ทุกฟิลด์; mock branch สังเคราะห์ข้อมูลที่ใช้ IP ชุดเดียวกับ
  `kernel/mock.go:800 mockFlowTemplates` (192.168.1.101/102/105) เพื่อให้ dev เห็นชื่อ
  อุปกรณ์เหมือนของจริง
**acceptance:** `yarn build` (tsc) ผ่าน

### T-09 — หน้า Statistics
**ไฟล์ใหม่:** `frontend/src/pages/Statistics.tsx`
- ตัวเลือก window (1h/24h) + auto refresh ~10s + ปุ่ม refresh
- การ์ด: Top Source Hosts / Top Destinations / Top Conversations / Top Denied
  (Sources + Ports) — ใช้ `components/ui/{card,table,badge,tabs,select,skeleton}` เท่านั้น
- ป้าย `accuracy` ("ประมาณการ"/"ใกล้เคียงจริง") ขับด้วยฟิลด์จาก API (อย่า hardcode
  ซ้ำรอย bug เดิมที่ `Dashboard.tsx`), ป้าย "สุ่มตัวอย่าง" บนการ์ด Denied เสมอ,
  ป้ายเตือนเมื่อ `truncated == true`
- Empty state เมื่อยังไม่มีข้อมูล (เพิ่งบูต / conntrack ใช้ไม่ได้) — ห้ามโชว์ตาราง
  เปล่าเฉย ๆ
- **กฎสไตล์บังคับ** (`docs/rules_of_work.md`): ห้าม `shadow-*`/`backdrop-blur-*`,
  ห้ามสีดิบแบบ `text-emerald-500` ให้ใช้ตัวแปรธีม, ต้องดูดีทั้ง dark/light,
  IP/MAC ใช้ `font-mono`
**acceptance:** `yarn build && yarn lint` ผ่าน, ดูได้ทั้ง 2 ธีม

### T-10 — route + sidebar
**ไฟล์:** `frontend/src/App.tsx` (กลุ่ม `logs` `:169-172`),
`frontend/src/components/app-sidebar.tsx` (กลุ่ม "Log & Report" `:76-82`)
เพิ่ม `<Route path="statistics" element={<Statistics />} />` และเมนู
`{ path: "/logs/statistics", label: "Statistics", icon: BarChart3 }` (import icon
จาก lucide-react ตามแบบเดิม)
**acceptance:** คลิกจาก sidebar เข้าหน้าได้, active state ถูกต้อง

### T-11 (optional) — Top Destination Ports by bytes
เพิ่ม map `portBytes` (cap 200) ในบัคเก็ต + การ์ดที่ 5 — ใช้กลไกเดียวกับ T-02 ทั้งหมด
ทำก็ต่อเมื่อ T-01..T-10 ผ่านหมดแล้ว

### T-12 (optional, ท้ายสุด) — เอกสาร
`README.md` Feature Status เพิ่มแถว Statistics; `docs/tech_stack_design.md` ระบุว่า
สถิติทั้งหมดเป็น RAM-only aggregation ไม่ลง SQLite

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | หมายเหตุ |
|---|---|---|---|
| GET | `/api/statistics/traffic?window=1h\|24h` | `authRoute` (ทุก role ที่ล็อกอิน) | เส้นใหม่, อ่านอย่างเดียว, `window` whitelist ที่ handler |

ไม่มี endpoint ที่เขียน/แก้ไขอะไรในแผนนี้ → ต้องใช้งานได้ปกติทั้งในโหมด
`-disable-edit=true` และ role read-only (ถือเป็นเกณฑ์ทดสอบ)

## 5. ข้อควรระวัง

1. 🔒 **input `window`** — จุด input จาก client จุดเดียวของแผนนี้ ต้อง whitelist
   ที่ handler ก่อนถึง service เท่านั้น (ลอก `HandleGetTrafficDetail`) ห้ามเอาไป
   ประกอบเป็น key/query/สตริงอะไรทั้งสิ้น
2. **ห้ามนับซ้ำ / ห้ามคำนวณ delta ซ้ำ** — dst/conv ต้องใช้ `delta` **ตัวเดียวกัน**
   กับที่ host ใช้ ในการเรียก `addBucket` ครั้งเดียวกัน ถ้าแยกคำนวณเองจะเพี้ยนทันที
   ที่ event มาชนกับ poll (invariant ของ Phase 2, `traffic_stats.go:302-322`)
3. **หน่วยของ Denied ไม่ใช่ byte และไม่ใช่จำนวนแพ็กเก็ตจริง** — log rule มี
   `expr.Limit` (10 pkt/s ที่ `real_firewall.go:400`, 3 pkt/min ที่ `:331`)
   ห้ามเอาไปรวมเปอร์เซ็นต์กับ `observedBytes` เด็ดขาด และห้ามแก้ limit เพื่อให้
   สถิติแม่นขึ้น (จะกลายเป็นช่องทางให้ผู้โจมตีทำ log flood)
4. **`stampAndPush` อยู่บน NFLOG read loop** — `RecordFirewallLog` ต้อง O(1),
   ไม่ log, ไม่ I/O, ไม่ block; ถ้า mutex ตัวนี้ช้าจะทำให้ log ตกทั้งระบบ
5. **RAM** — เพิ่ม 2-4 map ต่อบัคเก็ต × 288 บัคเก็ต ต้อง cap ทุกอันตามตัวเลขใน T-02/T-03
   (worst case ~20-25 MB) และต้องยืนยันด้วยการรัน ≥1 ชม. แล้ววัด RSS
6. **concurrent map** — `trafficDetailBucket` มีแต่ map (reference type) การอ่านต้อง
   อยู่ใต้ `RLock` ตลอดช่วง aggregate ตามคำเตือนยาวที่ `traffic_stats.go:687-704`
   ห้าม copy struct ออกมาแล้วค่อยวน
7. **ความหมายของ SrcIP** — conntrack Forward tuple: flow ขาออกจาก LAN จะได้ IP
   ในบ้าน (pre-NAT ✅) แต่ flow ที่ริเริ่มจากภายนอกจะได้ IP อินเทอร์เน็ตเป็น "source"
   → ต้องมีฟิลด์ `Private` และ UI ต้องบอกผู้ใช้ได้ว่าแถวไหนคือเครื่องใน LAN
8. **conversation cardinality** — เครื่องที่โดน/ทำ port scan สร้าง conversation ได้
   หลักหมื่นต่อนาที เพดาน `maxTrackedConversations` คือด่านสุดท้าย และต้องมี
   unit test ข้อ 4 ของ T-04 ล็อกพฤติกรรมนี้
9. **ห้าม persist** — ห้ามเพิ่ม table/migration ใด ๆ ใน `db/` ตลอดแผนนี้
   (ตรวจตอน review: `git diff --stat` ต้องไม่มี `backend/internal/db/`)
10. 🔒 **หลักฐานว่าไม่แตะ firewall/kernel** — `git diff --stat` ต้องไม่มี
    `kernel/real_firewall.go`, `kernel/interfaces.go`, `kernel/real_*.go` ปรากฏเลย
    ถ้ามีแปลว่าหลุดขอบเขต §0
11. **mock ต้องพร้อมใช้** — `-mock=true` ต้องเห็นข้อมูลครบทุกการ์ด (mock flow มี
    dst IP อยู่แล้ว, mock NFLOG มี DROP อยู่แล้ว) และ frontend mock branch ต้อง
    อัปเดตพร้อมกัน ไม่งั้น tsc พังหรือหน้าเปล่า
12. **ไม่ทำ regression กับ Dashboard** — `GetTrafficDetail` ต้องคืน JSON เหมือนเดิม
    ทุกฟิลด์ (มี test เดิมที่ `api/handlers_test.go:~817` คุมอยู่)

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [ ] T-01 `model/statistics.go` (ไฟล์ใหม่)
- [ ] T-02 `service/traffic_stats.go`: dstBytes/convBytes + cap + `GetTrafficBreakdown`
- [ ] T-03 `service/statistics.go` (ไฟล์ใหม่): deny ring + `RecordFirewallLog` + `GetStatistics`
- [ ] T-04 `service/statistics_test.go` ครบ 6 เคส + `-race`
- [ ] T-05 `api/handlers.go` + `api/router.go` + แก้ call site เทสต์ 5 จุด 🔒
- [ ] T-06 `cmd/pigate/main.go` wiring + hook ใน `stampAndPush`
- [ ] `cd backend && go build ./... && go test -race ./...` ผ่าน

**Frontend / Docs**
- [ ] T-07 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync)
- [ ] T-08 `frontend/src/services/statisticsService.ts` (+ mock branch)
- [ ] T-09 `frontend/src/pages/Statistics.tsx`
- [ ] T-10 `App.tsx` + `app-sidebar.tsx`
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] T-11 / T-12 (optional)

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ)**
- [ ] `-mock=true -allow-dev-cors`: เข้า `/logs/statistics` เห็นครบ 4 การ์ด ตัวเลข
      ขยับทุกครั้งที่ refresh, ชื่ออุปกรณ์ (iPhone-13/Android-SmartTV/iPad-Pro)
      แสดงแทน IP ในการ์ด Top Source
- [ ] สลับ window 1h ↔ 24h แล้วค่าเปลี่ยนอย่างสมเหตุสมผล (24h ≥ 1h เสมอ)
- [ ] ผลรวม bytes ของ Top Sources ทั้งหมด ≤ `observedBytes` และ % ไม่เกิน 100
- [ ] Dashboard แท็บ Detailed เดิม (Protocol Breakdown / Top Talkers / Top Rules /
      Active Sessions) ยังทำงานเหมือนเดิมทุกประการ — ไม่มี regression
- [ ] real device: ดาวน์โหลดไฟล์ใหญ่จากเครื่องใน LAN 1 ครั้ง → เครื่องนั้นขึ้นเป็น
      อันดับ 1 ใน Top Sources และปลายทางจริงขึ้นใน Top Destinations,
      ยอดไม่โตเป็น 2 เท่าของขนาดไฟล์ (ยืนยันว่าไม่นับซ้ำ)
- [ ] real device: `nmap` ยิงมาจากเครื่องนอก → IP นั้นขึ้นใน Top Denied Sources และ
      การ์ดแสดงป้าย "สุ่มตัวอย่าง"
- [ ] real device: ระหว่างสแกนหนัก → CPU ไม่ค้าง, ไม่มี panic, `truncated` ขึ้นป้ายเตือน
- [ ] ปล่อยรัน ≥ 1 ชั่วโมง → RSS ไม่โตต่อเนื่อง (bucket ring + cap ทำงาน)
- [ ] `-disable-edit=true` และ role read-only ยังเปิดหน้านี้ได้ครบ
- [ ] logout แล้วเรียก `/api/statistics/traffic` ตรง ๆ → 401
- [ ] `go test -race ./...` และ `yarn build && yarn lint` ผ่านทั้งคู่
- [ ] 🔒 `git diff --stat` ไม่มี `kernel/`, ไม่มี `db/`, ไม่มี migration ใหม่
- [ ] ทุกอย่างอยู่บน branch `feat/statistics-page` และเข้า main ผ่าน PR เท่านั้น
