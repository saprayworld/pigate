# Statistics -> Firewall Page — Work Plan

โจทย์: เพิ่มหน้า **Statistics > Firewall** ที่รวมสถิติไฟร์วอลล์ที่มีอยู่แล้วใน RAM (nftables rule counter ผ่าน `TrafficAccountingManager.DumpRuleCounters`, bucket ring ใน `traffic_stats.go`, deny ring ใน `statistics.go`, `RingBuffer` ใน `logs/ringbuffer.go`, `PolicyStatsService`) มาไว้ที่หน้าเดียว โดย **ไม่เพิ่ม method ใหม่ใน `kernel.FirewallManager`/`TrafficAccountingManager`, ไม่แตะ `real_*.go`/`mock.go`, ไม่เพิ่มตาราง SQLite, ไม่เขียนดิสก์เพิ่ม, ไม่เพิ่ม goroutine/ticker ใหม่** — ทุกอย่างทำในชั้น service/api/frontend เท่านั้น

สถานะ: **implement แล้ว** — ดูหัวข้อ "สิ่งที่ทำจริง" ท้ายไฟล์ (T-12)

## ข้อตัดสินจากเจ้าของโปรเจกต์ (ตอบแล้ว — ปิดประเด็น)

| ประเด็น | คำตอบ | ผลต่อแผน |
|---|---|---|
| หน้านี้ควรมีตาราง log บล็อกล่าสุดเองไหม หรือแค่ลิงก์ออกไป Logs > Traffic | **ต้องมีตาราง "Recent Blocked Events" อยู่ในหน้านี้ด้วย** (ไม่ใช่แค่ลิงก์ออก) | เพิ่มขอบเขตข้อ 7 (ตาราง) + คง endpoint field `recentBlockedEvents` + ลิงก์ "ดู log แบบเต็ม" ไป `/logs/traffic` (กรอง DROP) ควบคู่กัน |
| ตำแหน่งเมนู Firewall ใน sidebar | **อยู่ต่อจาก Traffic** (Overview / Traffic / Firewall / DNS / Capacity) | T-11 |
| `blockedBytes`/`blockedPackets` ไม่ครบ (ไม่รวม default-drop) จะโชว์ต่อไหม | **โชว์ต่อ พร้อม tooltip อธิบายชัดเจน** ว่านับเฉพาะ traffic ที่ match user rule ไม่รวม default-drop | ต้องมี tooltip/คำอธิบายทุกจุดที่โชว์ตัวเลขนี้ (T-10, model doc comment T-02) |

ไม่มีประเด็นค้างที่ต้องให้เจ้าของโปรเจกต์ตัดสินเพิ่มแล้ว

## สภาพปัจจุบัน (ข้อเท็จจริงจากโค้ด)

- `TrafficStatsService` (`backend/internal/service/traffic_stats.go`) เก็บ bucket ring `s.buckets` (5 นาที/bucket, 288 bucket = 24 ชม.) แต่ละ bucket มี `ruleBytes map[string]model.RuleCounter` (bytes/packets ต่อ DB rule id จาก nft counter) — `GetTrafficDetail`/`buildTopRules` อ่านค่านี้อยู่แล้วสำหรับ Dashboard "Detailed" tab
- `StatisticsService` (`backend/internal/service/statistics.go`) เก็บ "deny ring" ของตัวเอง (`s.denyBuckets`) แยกจาก bucket ring ข้างบน — นับ event จาก NFLOG (`RecordFirewallLog`, เฉพาะ Action=="DROP") ต่อ source IP และ ต่อ "proto/port" ต่อ bucket 5 นาทีเดียวกัน ใช้แล้วโดย `/api/statistics/traffic`'s `DeniedSources`/`DeniedPorts`
- `logs.RingBuffer` (`backend/internal/logs/ringbuffer.go`) เก็บ log ทุก chain (forward/input/output) ปนกัน มี `GetAll()` (คืนสำเนาทั้งหมด, newest-first), `LastMatchedByRule()` (scan ครั้งเดียว คืน last-matched ต่อ rule id ที่มี Log เปิด) — `StatisticsService.logBuffer` (ต่อผ่าน `SetLogBuffer`, wiring เดิมมีอยู่แล้ว) ชี้ไปที่ instance เดียวกับที่ `main.go` ใช้ทั่วระบบ
- `PolicyStatsService.GetPolicyRuleStats` (`policy_stats.go`) เป็นตัวอย่าง pattern การรวม 3 แหล่ง (`RuleCounterSnapshot`, `LastMatchedByRule`, `RuleLastHits`) เข้าเป็นแถวต่อ rule พร้อม `Unused`/`LastMatchedAt`/`LastMatchedSource` — ใช้เป็นแม่แบบให้ `GetFirewallStatistics` โดยตรง (ต่าง location เพราะ plan กำหนดให้ methods ใหม่อยู่ที่ `StatisticsService` ไม่ใช่ `PolicyStatsService`)
- `normalizeStatsWindow`/`statsWindowBucketCount`/`statsSeriesAxis`/`statsSeriesIndex`/`lastNBuckets` (`traffic_stats.go`) เป็น helper กลางของทุก endpoint ที่ต้อง window/series แล้ว — ต้องนำมาใช้ซ้ำ ห้ามสร้างชุดใหม่
- `model.TopDeniedSource`/`TopDeniedPort` (`model/statistics.go`) มีอยู่แล้ว ใช้ซ้ำได้เลย ไม่ประกาศซ้ำ
- Frontend มีชุด component ใช้ซ้ำได้ครบใน `components/statistics/`: `TrafficStatCard`, `SortableHead`, `TrafficFilterInput`, `TrafficEmptyState`, `TruncatedWarning`, `CapacityIndicator`, `HostCells.HostLabel`, `ReferenceHoverProvider`/`HostReferenceTrigger`, `TrafficTrendCard` — และ `useStatsWindow`/`StatsWindowTabs` จาก `DnsStatsShared.tsx`, `STATS_WINDOWS` จาก `lib/statsWindow.ts`
- Sidebar ปัจจุบัน (`app-sidebar.tsx`): Overview / Traffic / DNS / Capacity — Firewall เป็นเมนูใหม่ที่ต้องแทรกต่อจาก Traffic
- `docs/openapi.yaml` และ `frontend/public/openapi.yaml` ต้องเนื้อหาตรงกันเสมอ ห้ามแก้ `backend/internal/api/dist/openapi.yaml`/`frontend/dist/` (build artifact)

## Design decisions

1. **สองแหล่งข้อมูล คนละหน่วย ห้ามผสม** — `acceptedBytes`/`acceptedPackets`/`blockedBytes`/`blockedPackets`/ทุกอย่างใน `trend`/`chains`/`rules` มาจาก nft rule counter (แม่นยำ 100% แต่ครอบคลุมเฉพาะ traffic ที่ match user rule ที่ผู้ใช้สร้างเอง ไม่รวม traffic ที่โดน default-drop ของ section 4 ใน input/forward chain — ดู `docs/tech_stack_design.md` §4.3) ส่วน `blockedEvents`/`denyTrend`/`blockedSources`/`blockedPorts`/`recentBlockedEvents` มาจาก NFLOG event count (รวม default-drop ด้วย) — ห้ามคำนวณ percent ข้ามสองแหล่งนี้เด็ดขาด
2. **ไม่ copy bucket ring ออกมาก่อนวน** — ทุก accessor ใหม่ (`GetFirewallRuleBreakdown`, `denySeries`) อ่าน+รวมผลใต้ `s.mu.RLock()` เดียวตลอด (เหมือน `GetTrafficDetail`/`denyCapacity` เดิม) กัน data race กับ poller goroutine
3. **ไม่แตะ endpoint/ฟังก์ชันเดิมที่มีอยู่แล้วโดยไม่จำเป็น** — `GetTrafficDetail`/`GetTrafficBreakdown`/`addBucket`/`denySnapshot`/`denyCapacity`/`RecordFirewallLog` ต้องพฤติกรรมเดิมทุกไบต์ ยกเว้นการเพิ่ม `limit` parameter ให้ `buildTopDeniedSources`/`buildTopDeniedPorts` (มีผู้เรียกเดิมใน `statistics.go`'s `GetStatistics` ที่ต้อง "ส่ง `statsTopN` เพื่อผลลัพธ์เท่าเดิมทุกไบต์" ตามที่ระบุใน T-05)
4. **Rule ที่ถูกลบ/ปิดใช้งานระหว่าง window** — แสดงเป็นแถว "(deleted rule)" แทนการ panic หรือข้ามเงียบๆ (ตรงกับ Risk ข้อ 5 ของแผนต้นฉบับ)
5. **Recent Blocked Events อ่านจาก accessor ที่มีอยู่แล้ว (`logs.RingBuffer.GetAll()`) ไม่เขียน logic scan ใหม่** — filter เฉพาะ `Action == "DROP"` แล้วตัดที่ `limit` แถวแรก (buffer คืนค่า newest-first อยู่แล้ว)
6. **`CountersSince` ต้องมาจาก `FirewallService.LastAppliedAt()`** — `StatisticsService` เดิมไม่ถือ `*FirewallService` จึงต้องเพิ่ม field + setter (`SetFirewallService`) แบบเดียวกับ `SetLogBuffer` เดิม (ห้ามเปลี่ยน signature ของ `NewStatisticsService`)
7. **Limit เดียวคุมทั้ง 4 ลิสต์** (`rules`/`blockedSources`/`blockedPorts`/`recentBlockedEvents`) — default 100, clamp 1-500 ที่ทั้งชั้น handler (ห้ามเชื่อ client) และชั้น service (defense-in-depth)

## Task list

> ทำบน branch `feat/statistics-firewall-page` (แตกจาก `main` แล้ว) ตามลำดับ T-01 → T-12, ห้าม commit เว้นแต่เจ้าของสั่ง

| Task | Layer | ไฟล์หลัก | สรุป | depends_on |
|---|---|---|---|---|
| T-01 | docs | `docs/ref/todo/statistics-firewall-page-plan.md` | เอกสารแผนนี้ | — |
| T-02 | model | `backend/internal/model/firewall_stats.go` | DTO: `FirewallStatistics`, `FirewallTrendPoint`, `FirewallDenyPoint`, `FirewallChainStat`, `FirewallRuleStatRow`, `FirewallBlockedEvent` | T-01 |
| T-03 | service | `backend/internal/service/traffic_stats.go` | `GetFirewallRuleBreakdown(window, actionByRule)` — per-rule totals + accept/drop trend จาก bucket ring เดิม | T-02 |
| T-04 | service | `backend/internal/service/statistics_firewall.go` (ใหม่) | `denySeries(window)` — deny ring series (event count) | T-02 |
| T-05 | service | `backend/internal/service/statistics_firewall.go` | `GetFirewallStatistics(window, limit)` — ประกอบ response ทั้งหมด | T-03, T-04 |
| T-06 | api | `handlers.go`, `router.go`, `cmd/pigate/main.go` | `HandleGetFirewallStatistics`, route `GET /api/statistics/firewall` (authRoute), wiring `SetFirewallService` | T-05 |
| T-07 | test | `statistics_firewall_test.go`, `handlers_test.go` | unit test + race test | T-06 |
| T-08 | docs | `docs/openapi.yaml`, `frontend/public/openapi.yaml` | เพิ่ม path/schema | T-06 |
| T-09 | frontend | `frontend/src/services/firewallStatisticsService.ts` | type + mock data | T-08 |
| T-10 | frontend | `frontend/src/pages/StatisticsFirewall.tsx` | หน้าเว็บเต็ม | T-09 |
| T-11 | frontend | `App.tsx`, `app-sidebar.tsx`, `site-header.tsx` | route + sidebar + header title | T-10 |
| T-12 | docs | `README.md`, เอกสารนี้ | อัปเดต Feature Status + สรุปผล implement | T-11 |

## Scope หน้า `/statistics/firewall`

Endpoint: `GET /api/statistics/firewall?window=<15m..24h>&limit=<1..500>` (authRoute เดียวกับ `/api/statistics/*` อื่น)

1. Summary cards — Accepted bytes/packets, Blocked bytes/packets (พร้อม tooltip อธิบายไม่รวม default-drop), Blocked events (จาก NFLOG), จำนวน rule ที่เปิดใช้/unused, `countersSince`
2. Firewall Traffic Trend (bytes) — กราฟ 2 เส้น accept vs drop ต่อ bucket 5 นาที (`trend`)
3. Blocked Events Trend (events) — กราฟแยกใบจาก deny ring (`denyTrend`) คนละหน่วย ห้ามรวมแกนกับข้อ 2
4. Chain Breakdown — ตารางแยก forward/input/output × accept/drop (`chains`)
5. Top Rules by Traffic — rule name, chain, action, bytes, packets, %, last matched, badge Unused, คลิกไปหน้า policy ของ chain นั้น (`rules`)
6. Top Blocked Sources / Top Blocked Ports — full list (limit 100 default, max 500) พร้อม hostname enrichment (`blockedSources`/`blockedPorts`)
7. **Recent Blocked Events table** (เพิ่มตามคำตอบเจ้าของโปรเจกต์) — timestamp, source IP, port/proto, chain/rule, ลิงก์ "ดู log แบบเต็ม" ไป `/logs/traffic` (กรอง DROP) (`recentBlockedEvents`)
8. Capacity + คำเตือน — `CapacityIndicator` ของ ring `firewall.logBuffer`/`denySources`/`denyPorts` (จาก `/api/statistics/capacity` เดิม, ไม่มี endpoint ใหม่) + `TruncatedWarning` เมื่อ `truncated`/`available==false`

เมนู sidebar ใต้ Statistics: Overview / Traffic / **Firewall** / DNS / Capacity

## Risks ที่ต้องระวัง

1. `blockedBytes` ไม่ครบตามที่ผู้ใช้คาด (ไม่รวม default drop) — มี tooltip ชัดเจนทุกจุด ห้ามคำนวณ % ข้ามหน่วยกับ `blockedEvents`
2. nft counter รีเซ็ตทุกครั้งที่ Apply Settings — กราฟอิง bucket ring (delta ผ่าน reset-detection ใน `processRuleCounters` อยู่แล้ว) ไม่ใช่ `RuleCounterSnapshot` ตรงๆ
3. Data race กับ poller — accessor ใหม่อ่าน+รวมผลใต้ RLock เดียว ห้าม copy slice ออกมาก่อน มีเทสต์ `-race`
4. Performance — endpoint ใหม่ห้ามเรียก kernel เลย (ห้าม `DumpRuleCounters` ใน handler) วน bucket ring ครั้งเดียวต่อ request
5. Rule ถูกลบ/เปลี่ยนชื่อระหว่าง window — ไม่ panic ไม่แสดงชื่อผิด (แสดง "(deleted rule)")
6. Mock ต้องสมจริง ห้ามแก้ `mock_traffic_accounting_test.go` หรือพฤติกรรม mock เดิม
7. Deny ring มี cap ต่อ bucket — โชว์ `truncated`/`CapacityIndicator` ให้ถูกต้อง
8. ห้าม regress `/api/statistics/traffic` ที่ใช้ `buildTopDeniedSources`/`Ports` อยู่ — ต้อง pass `statsTopN` ที่ caller เดิมหลังเพิ่ม `limit` parameter
9. T-06 เป็น task เดียวที่แตะ input validation ชั้น api — ต้องระวังเป็นพิเศษเรื่อง injection/validation (window whitelist, limit clamp)

## สิ่งที่ทำจริง / ข้อจำกัดที่พบระหว่าง implement (T-12)

- ทุก task T-01–T-11 ทำเสร็จตามแผนข้างต้น ไม่มีการเบี่ยงเบนเชิงสถาปัตยกรรมจากแผน
- `buildTopDeniedSources`/`buildTopDeniedPorts` (`statistics.go`) เพิ่ม parameter `limit` ตามแผน และ caller เดิมใน `GetStatistics` ถูกแก้ให้ส่ง `statsTopN` เพื่อคง behavior เดิมทุกไบต์ (ตรวจแล้วด้วย unit test เดิมของ `/api/statistics/traffic`)
- `StatisticsService` เพิ่ม field `firewall *FirewallService` + setter `SetFirewallService` ใน `statistics.go` (ไม่แตะ `denySnapshot`/`denyCapacity`/`RecordFirewallLog`) — ต่อสายใน `main.go` ทันทีหลัง `SetLogBuffer`
- "Top Rules by Traffic" (`rules`) และ Recent Blocked Events/Blocked Sources/Ports ใช้ `limit` เดียวกัน (การตัดสินใจเชิง implementation: เอกสารต้นฉบับระบุ limit ชัดเจนเฉพาะ blockedSources/Ports แต่ไม่ได้กำหนดจำนวนแถวของ `rules` — เลือกใช้ limit เดียวกันเพื่อความเรียบง่ายของ endpoint แทนที่จะเพิ่ม constant ที่สอง)
- Recent Blocked Events ลิงก์ "ดู log แบบเต็ม" ไปที่ `/logs/traffic?action=DROP` แบบ static ตามคำสั่งเดิม (ไม่ได้แยกไป `/logs/local` ตาม chain ของแต่ละ entry แม้ entry จะมี field `chain` ก็ตาม — เป็นไปตามข้อความ scope ข้อ 7 ที่ระบุปลายทางเดียว)
