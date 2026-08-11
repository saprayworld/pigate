# Traffic Log: Rule Matched Name + IP Domain/Hostname — Work Plan

โจทย์: หน้า Log & Report > Forward Traffic และ Local Traffic ปัจจุบันไม่สามารถบอกได้ว่าแต่ละ log entry ตรงกับ firewall rule ข้อไหน และคอลัมน์ IP ก็ไม่มีข้อมูลกำกับว่าเป็นโดเมน/เครื่องอะไร ทั้งที่ระบบมีข้อมูลนี้อยู่แล้วบางส่วน (DNS reverse cache ที่ใช้ในหน้า Statistics)

สถานะ: วางแผนเสร็จ + ตรวจสอบทานกับโค้ดจริงบน main แล้ว (หลัง PR #128, PR #131 merge) รอ ai-developer เริ่มลงมือ ดู "การตรวจสอบซ้ำ" ท้ายเอกสารสำหรับรายละเอียดที่ปรับ

## สภาพปัจจุบัน (ข้อเท็จจริงจากโค้ด ณ วันที่วางแผน)

**เส้นทางข้อมูล log**
- nftables (`backend/internal/kernel/real_firewall.go`) → `expr.Log` ส่งเข้า NFLOG group 100 (forward) / 101 (input+output) ผ่าน `forwardLogExpr()` / `localLogExpr()`
- `backend/internal/kernel/real_traffic_log.go` → `parseNflogAttr()` แปลง event เป็น `model.FirewallLog` โดยอ่าน log prefix แล้ว map ผ่าน `nflogPrefixTable`
- `backend/cmd/pigate/main.go` (`stampAndPush`) ใส่ `ID`/`Time` แล้ว `ringBuffer.Add()` + `statisticsService.RecordFirewallLog()`
- API: `HandleGetTrafficLogs` (`/api/logs/traffic`), `HandleGetRecentLogs` (`/api/dashboard/logs`), SSE `HandleLogStream` — ทั้งหมดอยู่ใน `backend/internal/api/handlers.go`
- Frontend: `ForwardTraffic.tsx` / `LocalTraffic.tsx` เป็น wrapper บางๆ ของ `frontend/src/components/logs/TrafficLogPage.tsx` (ตารางจริงอยู่ที่นี่ไฟล์เดียว)

**ข้อ 1 — rule matched name: ไม่มีข้อมูลเลยในปัจจุบัน**
- `model.FirewallLog` ไม่มี field rule id/handle
- log prefix ปัจจุบันใช้ร่วมกันทั้งเชน (`"[PiGate] FWD ACCEPT: "` ฯลฯ) แยกไม่ออกว่ามาจากกฎข้อไหน
- NFLOG ไม่ส่ง rule handle/userdata มาที่ userspace → ช่องทางเดียวที่เป็นไปได้คือฝัง identifier ลงใน log prefix ต่อกฎ (เปลี่ยนแค่ค่า `Data` ของ `expr.Log` ไม่กระทบลำดับ/verdict/โครงสร้าง 4 section ตาม tech_stack_design.md §4.3)
- `model.PolicyRule` มี `Name` อยู่แล้ว

**ข้อ 2 — ip → domain: มี infrastructure พร้อมแล้ว**
- `backend/internal/service/dns_reverse_cache.go` = cache IP → domain (จากคำตอบ DNS ล่าสุดที่ dnsmasq/DNS server ของ PiGate ตอบให้ client) มี `LookupMany(ips)` อยู่แล้ว ใช้ในหน้า Statistics
- ข้อจำกัด: ทำงานเฉพาะเมื่อ DNS Query Logging เปิด และ client ใช้ DNS ของ PiGate; ปิด logging = cache ถูกล้างทันที (privacy policy เดิม) — ห้ามใช้ตัดสินใจ firewall/routing/QoS ใช้ display เท่านั้น
- ไม่ใช่ข้อมูลจาก ipinfo.io (นั่นคือ geo/org/ASN คนละเรื่อง) และไม่ใช่ PTR reverse DNS

## Design decisions (หลังคุยกับเจ้าของโปรเจกต์)

1. **RuleID เดินทางมากับ log prefix** ต่อกฎ (`r=<ruleID>` เช่น `"[PiGate] FWD ACCEPT: r=rule-1a2b3c4d "`) ผ่าน whitelist sanitizer `^[A-Za-z0-9_-]{1,32}$` เพราะ `ValidatePolicyRule` ไม่ตรวจ field ID และ backup import เขียน ID เข้ามาได้
2. **จุด log เชิงโครงสร้าง** (anti-spoof/not-local, default drop ของ input/forward) ใช้ token คงที่ `sys-*` แล้ว map เป็นชื่ออ่านง่าย
3. **RuleName เป็น snapshot-on-write** (ตัดสินใจโดยเจ้าของโปรเจกต์): resolve ruleId→ruleName ที่ `stampAndPush` ก่อนเขียนเข้า ring buffer แล้วไม่แก้ย้อนหลัง — กฎถูกลบ/เปลี่ยนชื่อภายหลัง entry เก่ายังคงชื่อเดิมไว้ หายเฉพาะตอน restart backend (in-memory ring buffer) เพื่อไม่ให้กระทบ NFLOG hot-path (ต้อง O(1) ไม่มี I/O) ใช้ atomic snapshot map ที่ refresh พื้นหลังทุก 5 วินาที + refresh ทันทีหลัง `SyncFirewallRules` สำเร็จ
4. **SrcDomain/DestDomain/SrcHostname/DestHostname เป็น enrichment ตอนอ่าน** (ที่ API layer) ไม่ snapshot ลง ring buffer เพื่อรักษา privacy contract เดิม (ปิด DNS Query Logging แล้วโดเมนต้องหายทันที)
5. **DHCP hostname fallback**: IP ฝั่ง LAN ที่ dnsReverseCache ไม่มีข้อมูล ให้ fallback ไปใช้ hostname จาก DHCP lease/reservation (ผ่าน TTL cache กัน query lease file ถี่เกินไป)
6. **Enrich เฉพาะหน้าที่ส่งออก** (≤1000 แถว) ไม่ enrich ทั้ง ring buffer; ช่องค้นหา (q) **ไม่รองรับ** ค้นด้วยชื่อกฎ/โดเมน/hostname (ตัดสินใจโดยเจ้าของโปรเจกต์ — ลด complexity/performance risk)
7. ทั้งหมดเป็น read-only enrichment ไม่มีการเขียน DB เพิ่ม ไม่มี exec, ไม่แตะ verdict/ลำดับกฎ nftables

## Task list

> ทุก task ทำบน feature branch เดียวกัน `feat/traffic-log-rule-and-domain`

| Task | Layer | ไฟล์หลัก | สรุป |
|---|---|---|---|
| T-01 | model | `backend/internal/model/types.go` | เพิ่ม field `RuleID/RuleName/SrcDomain/DestDomain/SrcHostname/DestHostname` ใน `FirewallLog` พร้อม doc comment แยก snapshot-on-write vs enrich-on-read |
| T-02 | kernel (SENSITIVE) | `backend/internal/kernel/real_firewall.go` | ฝัง `r=<token>` ต่อท้าย log prefix ต่อกฎ + sanitizer whitelist + fallback เมื่อ prefix ยาวเกิน 120 bytes + token คงที่สำหรับจุด log เชิงโครงสร้าง |
| T-03 | kernel | `backend/internal/kernel/real_traffic_log.go` | แกะ `r=<id>` จาก NFLOG prefix ใส่ `FirewallLog.RuleID` (sanitize ซ้ำฝั่งอ่าน) |
| T-04 | kernel | `backend/internal/kernel/mock.go` | ใส่ `RuleID` ตัวอย่างใน mock traffic log (ครอบคลุม resolve ได้ / resolve ไม่ได้ / ว่าง) |
| T-05 | service | `backend/internal/service/firewall_rule_names.go` | rule-name snapshot แบบ lock-free (`atomic.Pointer[map[string]string]`) + background refresher ทุก 5s + refresh ทันทีหลัง `SyncFirewallRules`; ตาราง system token |
| T-06 | service | `backend/internal/service/dns_query_stats.go` | เปิด `LookupDomains`/`LookupDomain` บน `StatisticsService` ห่อ `dnsReverseCache` ที่มีอยู่ |
| T-07 | service | `backend/internal/service/traffic_stats.go` | DHCP hostname fallback พร้อม TTL cache (`cachedHostLookup`, `LookupHostnames`/`LookupHostname`) |
| T-08 | service | `backend/cmd/pigate/main.go` | เรียก `StartRuleNameRefresher`; ใน `stampAndPush` resolve RuleID→RuleName ก่อน `ringBuffer.Add()` |
| T-09 | api | `backend/internal/api/handlers.go` | `enrichTrafficLogs()` เติม domain/hostname ตอนอ่าน (ไม่แตะ ruleName) ใช้ใน `HandleGetTrafficLogs`/`HandleGetRecentLogs`/`HandleLogStream` |
| T-10 | api/docs | `docs/openapi.yaml`, `frontend/public/openapi.yaml` | เพิ่ม field ใหม่ optional พร้อม description อธิบาย lifecycle จริง |
| T-11 | frontend | `frontend/src/services/trafficLogService.ts`, `dashboardService.ts`, `mockData.ts` | เพิ่ม type + mock samples ครอบคลุม 5 เคส |
| T-12 | frontend | `frontend/src/components/logs/TrafficLogPage.tsx` | คอลัมน์ Rule + ข้อความรอง (domain/hostname) ใต้ IP ในตาราง |
| T-13 | frontend | `frontend/src/pages/ForwardTraffic.tsx`, `LocalTraffic.tsx` | เพิ่มคำอธิบายข้อจำกัดในหน้า (snapshot ชื่อกฎ, โดเมน/hostname เป็นข้อมูลประกอบเท่านั้น) |

รายละเอียด instruction/acceptance ต่อ task แบบเต็ม อยู่ใน transcript การวางแผนของ ai-tech-lead (ไม่ได้ inline ไว้ในเอกสารนี้เพื่อความกระชับ — ai-developer รับ task ทีละตัวจาก ai-tech-lead/coordinator โดยตรง)

## Final acceptance (รวมท้ายแผน หลังทำครบทุก task)

- `cd backend && go build ./... && go test ./...` ผ่านทั้งหมด (โดยเฉพาะ `traffic_logs_test.go`, `policy_chain_test.go`, `statistics_test.go` เดิมต้องไม่พัง)
- `cd frontend && yarn build && yarn lint` ผ่าน; `bash build.sh` สร้าง single binary ได้
- โหมด `-mock=true`: เห็นคอลัมน์ Rule (มีชื่อ / ruleId แบบ muted / `-`) และโดเมน/hostname ใต้ IP ตาม mock
- `GET /api/logs/traffic` คืน field ใหม่แบบ `omitempty`, pagination/filter เดิมไม่เปลี่ยนพฤติกรรม
- ค้นด้วยชื่อกฎ/โดเมนต้อง **ไม่** เจอ (ยืนยันว่า q ไม่ถูกขยาย) แต่ค้นแบบเดิม (src/dest/port/proto/iface/reason) ยังทำงาน
- SSE ส่งแถวใหม่ที่มี ruleName/domain/hostname เหมือน snapshot fetch
- **Snapshot behavior (ข้อกำหนดหลัก)**: เปลี่ยนชื่อกฎ A → แถวเก่ายังแสดงชื่อเดิม, แถวใหม่แสดงชื่อใหม่; ลบกฎ A → แถวเก่ายังแสดงชื่อเดิมครบถ้วน; restart backend → ข้อมูลหายทั้งหมด (ยอมรับได้)
- กฎที่เพิ่งสร้างแล้วมีทราฟฟิก match ทันที ต้องแสดงชื่อได้ (พิสูจน์ refresh หลัง apply ruleset ทำงาน ไม่ต้องรอ ticker)
- โหมด real: nft ruleset จำนวน/ลำดับกฎเท่าเดิม, โครงสร้าง 4 section คงเดิม
- Security: rule ID ผิดรูปแบบ (space/quote/newline/ยาวเกิน/non-ASCII) ต้องไม่หลุดเข้า nft log prefix
- Privacy: ปิด DNS Query Logging → โดเมนหายทันทีทั้ง snapshot/SSE รวมแถวเก่า ขณะที่ ruleName/hostname ยังอยู่ครบ
- ไม่มี `exec.Command` / `net.LookupAddr` / HTTP call ใหม่ในทุก path ของแผนนี้
- Performance: buffer เต็มแล้วโหลด `/api/logs/traffic?limit=1000` response time ไม่แย่ลงมีนัยสำคัญ, ไม่มี "Dropped N log events" ถี่ขึ้น, lease file ไม่ถูกอ่านถี่ผิดปกติ (TTL cache ทำงาน)

## หมายเหตุ/ความเสี่ยงที่เหลือ

1. หน้าต่างเวลาสั้นๆ ที่ชื่อกฎอาจว่าง (ก่อน refresh รอบแรก) แทบไม่เกิดเพราะ refresh ทันทีหลัง apply ruleset — ai-developer ต้องตรวจว่าทุก mutation ของ policy จบที่ `SyncFirewallRules` จริง
2. RAM เพิ่มจาก snapshot ชื่อกฎ ~0.5MB worst case บน ring buffer 10,000 entries — รับได้
3. Domain/hostname resolve ตอนอ่านเสมอ (ไม่ snapshot) ตาม privacy contract — TTL ของ `dnsReverseCache` หมดอายุแล้ว log เก่าจะกลับไปแสดงแค่ IP (ตั้งใจ) มีคำอธิบายใน T-13 แล้ว
4. **Rebase note**: PR #128 (`feat/statistics-capacity-visibility`) และ PR #131 (`feat/statistics-host-ipinfo`) merged เข้า main หลังวางแผนนี้ แก้ไฟล์ที่ทับซ้อนกับแผนนี้ (`dns_reverse_cache.go`, `traffic_stats.go`, `dns_query_stats.go`, `statistics.go`, `handlers.go`, `router.go`, `main.go`) — ดูหัวข้อ "การตรวจสอบซ้ำ" ด้านล่างสำหรับรายละเอียดที่ปรับหลัง re-verify

## การตรวจสอบซ้ำกับ main ล่าสุด (โดย ai-tech-lead, หลัง PR #128/#131 merge)

สรุป: แผนยังใช้ได้เกือบทั้งฉบับ ไม่มี task ไหนต้องออกแบบใหม่ทั้งอัน — T-01/T-03/T-04/T-05/T-06/T-10/T-11/T-12/T-13 ตรงตามแผน มี 4 task ที่ต้องปรับ/ระบุเพิ่มก่อนให้ ai-developer เริ่ม:

- **T-02 (kernel, SENSITIVE)** — พบข้อเท็จจริงใหม่: `r.ID` ถูกฝังลง nft rule `UserData` อยู่แล้ว (`real_firewall.go:1056`, `userdata.AppendString(nil, userdata.TypeComment, r.ID)`) ใช้โดย Top Rules accounting (`real_traffic_account.go:189`) แต่ **NFLOG ก็ยังไม่ส่ง UserData มาที่ userspace** เหมือนเดิม — ข้อสรุปหลักของแผน (ต้องฝัง token ผ่าน log prefix) ยังถูกต้อง ไม่มีอะไรให้ reuse (sanitizer ของ T-02 เป็นตัวใหม่ทั้งหมด) แต่ต้องเพิ่ม acceptance: การเปลี่ยน log prefix ต้องไม่กระทบ `UserData`/จำนวน-ลำดับกฎ ไม่งั้น Top Rules counters จะพัง จุดอ้างอิงในโค้ดยังตรง: `forwardLogExpr`/`localLogExpr` (:653/:668), `buildChainRules(..., acceptLogPrefix, dropLogPrefix string, ...)` (:1043), `buildRuleExpressions(..., logPrefix string, ...)` (:1118-1129)
- **T-05 (service, ไฟล์ใหม่)** — จุดแทรก "refresh ทันทีหลัง sync สำเร็จ" ควรเกาะที่ `FirewallService.recordApply()` (firewall.go, เรียกจาก `defer` ต้นๆ ของ `SyncFirewallRules`) แทนที่จะแทรกท้าย `SyncFirewallRules` เอง — ครอบทุก return path ได้ปลอดภัยกว่า และตอบข้อกังวลข้อ 1 ด้านบนโดยตรง อย่าขยาย `NewFirewallService(repo, firewall, ifaceService)` (3 params) ใช้ setter/`StartRuleNameRefresher` แบบ additive แทน
- **T-06** — ยืนยันหลัง PR #128: `dns_reverse_cache.go` API เดิม (`Lookup`, `LookupMany`) **ไม่เปลี่ยน** (PR128 แค่เพิ่ม `Usage()` แยกต่างหาก) แต่ `LookupMany` ใช้ `mu.Lock()` (ไม่ใช่ RLock เพราะ lazy-evict) — ต้องเรียก **1 batch ต่อ 1 response เท่านั้น** ห้ามเรียกต่อแถว (ตรงกับ convention ที่ใช้อยู่แล้วใน `statistics.go`/`statistics_dns.go`/`statistics_traffic.go`)
- **T-07** — `hostLookup()` เลื่อนไปอยู่ `traffic_stats.go:1739` (เดิมอ้างอิง ~1605) signature ไม่เปลี่ยน แต่ตอนนี้ถูกเรียกจาก **9 จุด** ใน Statistics แล้วโดยไม่มี cache เลย (สร้างขึ้นทีละ request) **ตัดสินใจแล้ว (เจ้าของโปรเจกต์)**: T-07 สร้าง `cachedHostLookup` แยกเฉพาะสำหรับ traffic log ตามแผนเดิม (Option A) — ไม่แตะ/ไม่ใส่ cache ให้ 9 จุดเดิมใน Statistics เพื่อคุม scope ของ issue #132 ให้แคบ (การเพิ่ม cache ให้ `hostLookup()` เองเป็นการตัดสินใจแยกต่างหาก นอกขอบเขตนี้)
- **T-08** — `stampAndPush` ยังอยู่ที่ `main.go:428-437` รูปแบบเดิม (`entry.Time` → `entry.ID` → `ringBuffer.Add(entry)` → `statisticsService.RecordFirewallLog(entry)`) แทรก resolve ได้ตามแผน แต่ต้องระบุลำดับให้ชัด: `monitorCtx` ถูกสร้างที่ `main.go:416-417` (หลังประกอบ service ทั้งหมด) → `StartRuleNameRefresher(monitorCtx)` ต้องเรียก**หลัง** :417 และ**ก่อน**ที่ traffic-log watcher จะเริ่มที่ :443/:455 พร้อม prime snapshot แบบ synchronous ครั้งแรกก่อนเริ่ม watcher (ไม่งั้น log ชุดแรกสุดจะได้ ruleName ว่างเพราะยังไม่มี snapshot). ไม่ต้องแตะ `NewStatisticsService(...)` (ตอนนี้ 7 params) หรือ `ipInfoService` ที่ PR131 เพิ่มเข้ามาที่ :238-254
- **T-09** — `Server` struct มี `trafficStats`/`statistics` fields อยู่แล้ว (handlers.go:54-55) → **ไม่ต้องแก้ `NewServer` signature เลย** (เพิ่งถูก PR131 เพิ่ม `ipInfo` param ไป ยิ่งไม่ควรไปแตะซ้ำ) จุดอ้างอิงที่เลื่อน: `HandleGetRecentLogs` :750, `HandleGetTrafficLogs` :813 (`q` haystack ที่ :852 ยืนยันว่ายังเป็น src/dest/port/proto/iface/reason/chain ตามแผน ห้ามขยาย), `HandleLogStream` :2989 (marshal ที่ :3046). `router.go` ไม่ต้องเพิ่ม route ใหม่ (`/api/statistics/ipinfo` :67 และ `/api/statistics/capacity` :74 ของ PR อื่น ไม่ชนกับแผนนี้)
- **T-11** — แก้ path: mock อยู่ที่ `frontend/src/data-mockup/mockData.ts` (ไม่ใช่ `services/mockData.ts`) และเพิ่ม `frontend/src/pages/Dashboard.tsx` เป็นไฟล์ที่ต้องเช็คด้วย (บริโภค `FirewallLog` type เดียวกัน — ต้องยังคอมไพล์ผ่านหลังเพิ่ม field ใหม่แบบ optional)

ยืนยันแล้วว่า `model.ValidatePolicyRule` (`policy_rule_validate.go:13-38`) และ `Repository.CreatePolicy` (`repository.go:725-745`) ยังไม่ตรวจ field `ID` เลย — สมมติฐานที่ T-02 อ้างอิง (backup import เขียน ID ผิดรูปแบบเข้ามาได้ ต้องมี sanitizer ที่ kernel layer) ยังจริง 100%
