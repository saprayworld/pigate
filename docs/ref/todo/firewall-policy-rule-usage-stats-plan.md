# Firewall Policy: Per-Rule Usage Statistics — Work Plan

โจทย์: หน้า Firewall Policy / Local-In Policy / Local-Out Policy (รายการกฎ) อยากให้เห็นสถิติการใช้งานของแต่ละกฎในรายการเลย (ใช้ไปเท่าไหร่ กฎไหนไม่เคยถูกใช้) และตอนกดดูรายละเอียดกฎ ให้เห็นเพิ่ม เช่น bytes/packets, % ของทราฟฟิกทั้งหมด, ใช้ล่าสุดเมื่อไหร่

สถานะ: **พร้อม implement** — PR #133 merge เข้า main แล้ว (commit `5105466`), verify แล้วว่า `model.FirewallLog.RuleID`/`RuleName` มีอยู่จริงที่ `backend/internal/model/types.go:411-412` ตรงกับที่แผนนี้อ้างอิง ให้แตก branch ใหม่จาก main (เช่น `feat/policy-rule-usage-stats`) แล้วเริ่มทำได้เลย

## Design decisions (ยืนยันกับเจ้าของโปรเจกต์แล้ว)

1. **Snapshot ไม่ persistent** — ไม่เขียนอะไรลง SQLite เพิ่ม เพราะ nftables counter รีเซ็ตเป็นศูนย์ทุกครั้งที่มีการ sync ruleset ใหม่ (`RealFirewall.ApplyRules` ทำ `conn.FlushTable(table)` แล้วสร้างกฎทั้งหมดใหม่ทุกครั้ง แม้แก้แค่กฎเดียว) — ตัวเลขทั้งหมดคือ "นับตั้งแต่ apply ล่าสุด" ไม่ใช่สะสมตลอดชีพกฎ ต้องมี UI note บอกชัดเจน
2. **% การใช้งานเทียบกับทุก chain รวมกัน** เสมอ (ไม่ใช่เทียบเฉพาะภายใน chain เดียวกัน) — ตัวเลขของกฎหนึ่งจะไม่เปลี่ยนไปตามหน้าที่เปิดดู
3. **"Last matched at" แบบ hybrid**: (ก) กฎที่เปิด Log → หาจาก ring buffer traffic log ที่มี `RuleID` อยู่แล้ว (PR #133) สแกนรอบเดียวสร้าง `map[ruleID]time` (ข) กฎที่ไม่เปิด Log → fallback เกาะกับ poller เดิมที่ dump nft counter ทุก 10 วินาทีอยู่แล้ว (`TrafficStatsService`) บันทึกเวลาที่ delta > 0 — **ยอมรับความคลาดเคลื่อน ±10 วินาที** สำหรับกลุ่มนี้ ไม่เพิ่ม poll loop ใหม่
4. **ไม่ทำ auto-detect ถ้ามีคน `nft flush` นอกระบบ PiGate** (จะทำให้ counter รีเซ็ตโดยที่ `countersSince` ไม่อัปเดตตาม) — ยังไม่ทำในเฟสนี้ ลด scope
5. **Endpoint สิทธิ์ระดับ authRoute** เหมือน `/api/statistics/*` (ทุก role ที่ล็อกอินอ่านได้ ไม่ใช่ superAdminRoute) เพราะข้อมูลเป็นแค่ rule id + byte count ความอ่อนไหวต่ำ
6. **กราฟ/time-series ย้อนหลัง** (bytes ต่อ 5 นาทีใน 1h/24h) เลื่อนไป phase 2 — เฟสนี้แสดงตัวเลขนิ่งพอ
7. **ไม่แยก endpoint ต่อกฎ** — endpoint เดียว `GET /api/policies/stats[?chain=]` ใช้ได้ทั้งหน้ารายการและ dialog รายละเอียด (payload เล็ก จำนวนกฎหลักสิบ)
8. **ห้ามยัด field สถิติเข้า `model.PolicyRule`** เพราะถูกใช้ใน create/update/backup-export ด้วย จะทำให้ import/export เพี้ยน — ใช้ DTO แยก

## สภาพปัจจุบัน (ข้อเท็จจริงจากโค้ด)

- หน้ารายการกฎมีที่เดียว: `frontend/src/components/policy/PolicyChainPage.tsx` (ใช้ร่วม 3 หน้า Forward/Local-In/Local-Out ผ่าน wrapper บางๆ) ตารางปัจจุบัน 12 คอลัมน์ (มี `colSpan={12}` และแถว "Implicit Deny" ท้ายตารางที่ต้องแก้ตามด้วย) — **ยังไม่มีหน้า/dialog รายละเอียดกฎเลย** ต้องสร้างใหม่
- Backend: `GET /api/policies` → `handlers.go:1582 HandleGetPolicies` → `service/firewall.go:68 GetPolicies(chain)`
- `model.PolicyRule` มี `Log bool`, `Status bool`, `Chain`, `Action` — ไม่มี field runtime ใดๆ
- nft counter อ่านจาก `kernel/real_traffic_account.go:151 DumpRuleCounters()` (รวม counter ของ nft rule ทุกตัวที่ขยายจากกฎเดียวกันให้แล้วผ่าน `UserData` = DB rule id)
- **กฎที่ Disabled (`Status=false`) ไม่ถูกสร้างใน nftables เลย → ไม่มี counter** ต้องแสดง "—" ไม่ใช่ 0
- **มี poller อยู่แล้ว**: `service/traffic_stats.go:651 poll()` ทุก 10 วินาที (`flowPollInterval`) → `processRuleCounters()` (บรรทัด 919) คำนวณ delta ต่อกฎ + เก็บ `ruleBaseline map[string]model.RuleCounter` (ค่า cumulative ล่าสุด, มี reset detection อยู่แล้ว) — **นี่คือจุด poll ของ "Last matched at" กลุ่มไม่มี Log โดยไม่ต้องเพิ่ม kernel call/goroutine ใหม่เลย**
- `model.TopRule`/`buildTopRules()` (Dashboard "Top Rules" card, `traffic_stats.go:1700`) ใช้ซ้ำได้แค่บางส่วน (ตัด topN, ตัดกฎ bytes=0 ทิ้งซึ่งเป็นข้อมูลที่เราต้องการที่สุด, เป็น windowed bucket ไม่ใช่ cumulative-since-sync) — **ไม่แก้ `buildTopRules` เดิม** (Dashboard ใช้อยู่) เขียน path ใหม่อ่านจาก `ruleBaseline` แทน แต่ใช้ `percentOf()`/`policyLookup()` เดิมซ้ำได้
- Ring buffer (`internal/logs/ringbuffer.go`) มีแค่ `Add/GetAll(copy ทั้งก้อน)/Capacity/Clear/Subscribe` — ยังไม่มี API สแกนแบบไม่ copy ต้องเพิ่มใหม่ (`GetAll()` ทั้ง 10,000 entry จะ alloc ~2.5MB ต่อ request รับไม่ได้)
- `FirewallService.lastApplyAt` ถูกเซ็ตแม้ apply ล้มเหลว → ต้องเพิ่ม field "last **successful** apply" แยกสำหรับใช้เป็น `countersSince`
- `api.Server`/`NewServer` มี 30 พารามิเตอร์ ถูกเรียกใน test 6 จุด — **ห้ามเพิ่มพารามิเตอร์** ใช้ setter แบบเดียวกับ `FirewallService.SetRuleNameResolver`
- Mock: `kernel/mock.go MockTrafficAccounting.DumpRuleCounters` ให้ทุกกฎมีค่า > 0 เสมอ ต้องปรับให้บางกฎเป็น 0 เพื่อทดสอบสถานะ "Unused" ได้ในโหมด `-mock=true`

## Task list

> ทุก task ทำบน branch ใหม่ที่แตกจาก `main` **หลัง PR #133 merge แล้ว** (เช่น `feat/policy-rule-usage-stats`)

| Task | Layer | ไฟล์หลัก | สรุป |
|---|---|---|---|
| T-01 | model | `backend/internal/model/types.go` | เพิ่ม DTO `PolicyRuleStat`/`PolicyRuleStats` (ไม่แตะ `PolicyRule`/`TopRule` เดิม) |
| T-02 | logs | `backend/internal/logs/ringbuffer.go` | เพิ่ม `LastMatchedByRule() map[string]string` สแกนรอบเดียวจากท้ายมาหน้า ไม่ copy ทั้งก้อน + `Size()`. `FirewallLog.Time` เป็น `string` (RFC3339 หรือ RFC3339Nano ปนกันในโค้ด/เทสต์) — ใช้ค่า string ตามที่มีตรงๆ เป็น `lastMatchedAt`, การเทียบ "ใหม่กว่ากัน" ระหว่าง log กับ counter (ทำใน T-05) ต้อง parse ด้วย `time.Parse` ที่ลองทั้งสอง layout |
| T-03 | service | `backend/internal/service/traffic_stats.go` | บันทึก `lastRuleHit` เมื่อ delta > 0 ใน `processRuleCounters` (ไม่เพิ่ม goroutine/kernel call) + accessor `RuleCounterSnapshot()`/`RuleLastHits()` (คืนสำเนาเสมอ, อ่านใต้ `ruleMu` ซึ่งเป็น `sync.Mutex` ธรรมดา ไม่ใช่ `RWMutex` — ใช้ `Lock()`/`Unlock()` เท่านั้น ห้ามใช้ `RLock`) |
| T-04 | service | `backend/internal/service/firewall.go` | เพิ่ม `lastApplyOKAt` + `LastAppliedAt()` — **แก้ให้น้อยที่สุด ไฟล์นี้เป็น security-sensitive (firewall rule generation path)** |
| T-05 | service | `backend/internal/service/policy_stats.go` (ใหม่) | `PolicyStatsService.GetPolicyRuleStats(chain)` merge 3 source (nft counter snapshot / ring buffer log / poll last-hit), คำนวณ %/Unused, sort by bytes |
| T-06 | api | `backend/internal/api/handlers.go`, `router.go` | Handler + route `GET /api/policies/stats` (authRoute) ผ่าน setter `SetPolicyStatsService` (ห้ามแก้ `NewServer` signature) |
| T-07 | api | `backend/cmd/pigate/main.go` | wiring `PolicyStatsService` หลัง `api.NewServer(...)` |
| T-08 | kernel | `backend/internal/kernel/mock.go` | mock `DumpRuleCounters` ให้บางกฎเป็น 0 (ทดสอบสถานะ Unused ในโหมด mock) |
| T-09 | docs | `docs/openapi.yaml`, `frontend/public/openapi.yaml` | เพิ่ม path/schema ให้ตรงกับ DTO ของ T-01 พร้อม description ข้อจำกัด 4 ข้อ |
| T-10 | frontend | `frontend/src/services/policyStatsService.ts` (ใหม่) | API client + mock-mode synthesis (deterministic ต่อ rule id, มีทั้ง unused/log-source/counter-source) |
| T-11 | frontend | `frontend/src/lib/relativeTime.ts` (ใหม่) | `fmtRelativeTime`/`fmtAbsoluteTime` (ไทย, ไม่พึ่ง dependency ใหม่) |
| T-12 | frontend | `frontend/src/components/policy/PolicyChainPage.tsx` | คอลัมน์ "Usage" ใหม่ แทรกหลังคอลัมน์ "Status" (13 คอลัมน์รวม) — ต้องแก้ **`colSpan={12}` ทั้ง 2 จุด** (empty-state และ no-result) เป็น 13, แก้แถว "Implicit Deny" (เป็น `<TableCell>` เรียงมือ ไม่ใช่ colSpan) โดยเพิ่ม cell "—" 1 ช่องให้ตรงตำแหน่งคอลัมน์ Usage ใหม่ + เพิ่ม StatCard "Unused Rules" ใบที่ 5 (grid ปัจจุบัน `grid-cols-2 lg:grid-cols-4` ต้องแก้เป็น `lg:grid-cols-5` ด้วย) + note ท้ายหน้า + poll ทุก 10s (ไม่กระพริบตาราง) |
| T-13 | frontend | `frontend/src/components/policy/RuleStatsDrawer.tsx` (ใหม่) | Drawer รายละเอียดสถิติต่อกฎ (read-only) เปิดจากปุ่มไอคอนใหม่ในแถว |
| T-14 | docs | `docs/data/firewall.md` | อัปเดตเอกสารอ้างอิงสั้นๆ อธิบาย endpoint/แหล่งข้อมูล/ข้อจำกัด |

รายละเอียด instruction/acceptance แบบเต็มต่อ task อยู่ใน transcript การวางแผนของ ai-tech-lead (ai-developer รับ task จาก coordinator โดยตรงตอนเริ่มงานจริง)

## Final acceptance (รวมท้ายแผน)

- `cd backend && go build ./... && go test -race ./...` ผ่านทั้งหมด (ไม่มี test เดิมพัง)
- `cd frontend && yarn build && yarn lint` ผ่าน
- `-mock=true`: `GET /api/policies/stats` คืน 200, `available=true`, มีทั้งกฎ unused และไม่ unused, ผลรวม bytes ทุกกฎ = totalBytes, % รวมกัน ≈ 100
- `?chain=input` กรองเฉพาะกฎ chain นั้น แต่ totalBytes/percent ยังอิงทุก chain เท่าเดิม
- `?chain=bogus` → 400; ไม่มี session → 401
- กฎ Disabled ไม่ปรากฏใน stats, UI แสดง "—" ไม่ใช่ 0/Unused
- กฎเปิด Log + มี traffic จริง → `lastMatchedSource="log"`; กฎไม่เปิด Log แต่ counter ขยับ → `lastMatchedSource="counter"` ภายใน ~10-20 วินาที
- Clear ring buffer แล้ว `lastMatchedAt` จาก log หายไปอย่างนุ่มนวล (fallback เป็น counter หรือแสดง "ไม่ทราบ") ไม่พัง
- กด Apply Settings แล้ว bytes/packets รีเซ็ตใกล้ 0, `countersSince` อัปเดต, note ในหน้าอัปเดตตาม
- ตาราง 3 หน้า (Firewall/Local-In/Local-Out) แสดงครบ 13 คอลัมน์ไม่เหลื่อม, แถว Implicit Deny เรียงตรง, drag-reorder ยังทำงาน
- Drawer รายละเอียดแสดงครบทุก field + ข้อจำกัด 4 ข้อ
- ตารางอัปเดตเองทุก ~10 วินาทีไม่กระพริบ/ไม่ reset scroll, ไม่มี network request ค้างหลังออกจากหน้า
- Dark/light mode ผ่าน, ไม่มีสี hardcode/`shadow-*`/`backdrop-blur-*`
- `-disable-edit=true`: endpoint ยังทำงาน (เป็น GET)
- ไม่มี `exec.Command`/SQLite migration/ตารางใหม่, ไม่เรียก `DumpRuleCounters` จาก HTTP handler โดยตรง, ไม่มี goroutine/ticker ใหม่

## หมายเหตุ/ความเสี่ยงที่เหลือ

1. ~~**Sequencing**: พึ่ง `model.FirewallLog.RuleID` จาก PR #133~~ — **Resolved**: #133 merge เข้า main แล้ว (`5105466`), verify แล้วว่า `RuleID`/`RuleName` อยู่ที่ `types.go:411-412` ตรงตามแผน
2. **T-04 เป็นจุด sensitive**: แก้ `service/firewall.go` (firewall rule generation path) ต้อง diff ให้น้อยที่สุด ห้ามแตะ `SyncFirewallRules`/`ApplyRules` logic เดิม
3. **T-03 เป็น hot path ของ poller ที่มี data-race caution เดิมอยู่แล้ว** — ต้องรัน `go test -race` ให้ผ่าน
4. **`countersSince` เพี้ยนได้ถ้ามีคน `nft flush` นอกระบบ** (ตัดสินใจแล้วว่ายังไม่ทำ auto-detect ในเฟสนี้ — ทราบความเสี่ยงนี้ไว้)
5. Ring buffer จำกัด 10,000 entries ไม่ persist — กฎที่ log match ไม่บ่อยอาจถูกดันหลุดจาก buffer ก่อนเห็น "last matched" (fallback เป็น poll-based หรือแสดง "ไม่ทราบ")
