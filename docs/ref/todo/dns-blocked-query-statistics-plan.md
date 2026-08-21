# Blocked Domain Query Statistics — สถิติ query ที่ถูก deny-list บล็อก

> เอกสารแผนงานสำหรับฟีเจอร์ "Blocked Domain Query" บนหน้า Statistics > DNS: ระบบมี
> deny-list (block domain) ผ่าน dnsmasq อยู่แล้ว (`backend/internal/kernel/dns_server.go`,
> `docs/ref/todo/dns-blocked-domains-plan.md`) แต่ไม่เคยเก็บสถิติว่า query ไหนถูก block เลย
> ฟีเจอร์นี้เพิ่มการ classify query ที่มีอยู่แล้วใน ring เดิม (`dns_query_stats.go`) ว่า
> "ถูกบล็อกหรือไม่" โดยไม่สร้างแหล่งข้อมูลใหม่
>
> วันที่เขียน: 2026-08-22 · Branch อ้างอิง: `feat/dns-blocked-query-statistics`
> สถานะ: **implement ครบ T-01 ถึง T-15 แล้ว รอ ai-qa ตรวจ**

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**

1. หน้า Statistics > DNS แสดงสถิติ query ที่ถูกบล็อก: stat card, donut chart
   (Allowed vs Blocked), และตาราง Top Blocked Domains / Top Blocked Clients แยกเป็น
   sub-tab "Blocked Query"
2. ตัวเลข Blocked ต้องสอดคล้องกับตาราง Top Domains/Top Clients เดิม (badge "Blocked" บนแถวที่
   ถูกบล็อก) และ drill-down (domain/client/IP-filter) ทั้ง 3 endpoint เดิม
3. ไม่แตะ SQLite, ไม่เพิ่ม endpoint ใหม่, ไม่เพิ่ม query param ใหม่ — ขยาย response เดิมแบบ additive

**เงื่อนไขเชิงเทคนิคที่ต้องเป็นจริง**

- Classify แบบ **record-time**: index ที่ใช้ classify ต้องป้อนจาก list ที่ apply เข้า dnsmasq
  จริงแล้ว (`DNSServerService.ApplyAll` หลัง `ApplyZones` สำเร็จ) ไม่ใช่จาก DB ตรง ๆ — ถ้า apply
  fail ห้าม update index
- ห้าม re-classify query เก่าเมื่อ deny-list เปลี่ยน — บันทึกไว้ ณ เวลาที่เกิด query เท่านั้น
- Reuse ring buffer เดิม `dnsQueryStats.buckets[].pairs[domain][client]` ที่มี matrix
  (domain × client) ครบอยู่แล้ว — ห้ามสร้าง counting structure ใหม่ซ้ำซ้อน, เพิ่มแค่ "ป้ายกำกับ"
  ว่าโดเมนไหนถูกจัดเป็น blocked ในแต่ละ bucket (`blockedQueries uint64`, `blockedInfo
  map[string]blockedMeta`)
- RAM-only, ไม่เขียน SQLite (tech_stack_design.md §8 — ประหยัด SD card)
- Cap ใหม่ `dns-stats-max-blocked-domains` (default 1000, ตัดสินใจจากเดิมที่เสนอ 300 — ผู้ใช้
  เลือก 1000 เพื่อรองรับ blocklist สาธารณะขนาดใหญ่ในอนาคต) file-only ตาม pattern เดียวกับ
  `dns-stats-max-pairs`
- ตัวเลขรวม `blockedQueries` นับทุก event ไม่ผ่าน cap ใด ๆ ส่วนตาราง Top Blocked
  Domains/Clients derive จาก ring ที่มี cap (`dns-stats-max-pairs` เดิม + cap ใหม่) — เมื่อ ring
  เต็ม ยอดรวมของแถวอาจน้อยกว่ายอดจริง ซึ่งยอมรับได้ โดยแสดง banner เตือน (`blockedTruncated`)
  แทนการบังคับตัวเลขให้ตรงกันแบบผิด ๆ

**นอกขอบเขต (ตัดชัดเจน)**

- ไม่แก้ logic การสร้าง/ลบ/แก้ deny-list entry เอง (`dns_blocked_domains` table, CRUD endpoint
  เดิม) — ฟีเจอร์นี้แค่ "อ่าน" list ที่มีอยู่แล้วมา classify
- ไม่ทำ retroactive re-classification เมื่อแก้ deny-list (เจตนา, ดู §0 record-time)
- ไม่เพิ่ม endpoint ใหม่ ไม่เพิ่ม query param ใหม่บน endpoint เดิม
- ไม่รับประกันว่า query "ถูกบล็อกจริง 100%" ที่ปลายทาง client — สะท้อนแค่ว่า domain ที่ถาม
  match deny-list ที่ apply แล้ว ณ เวลานั้น ไม่รวม upstream DoH, client-side cache, หรือ client
  ใช้ resolver อื่น

---

## 1. ทางเลือกที่พิจารณาและปฏิเสธ

| ทางเลือก | เหตุผลที่ปฏิเสธ |
|---|---|
| Re-classify ทุกครั้งที่อ่าน response (query-time, ไม่ใช่ record-time) โดยเทียบกับ deny-list ปัจจุบัน | ทำให้ query เก่าที่ query ตอน deny-list ยังไม่มี rule กลายเป็น "blocked" ย้อนหลัง — เข้าใจผิดว่าระบบเคย block จริงตอนนั้น ขัดกับหลัก audit trail |
| สร้าง ring/bucket ใหม่แยกสำหรับ blocked query โดยเฉพาะ | ซ้ำซ้อนกับ `pairs[domain][client]` ที่มีข้อมูลครบอยู่แล้ว (matrix เดียวกัน) เสีย RAM เพิ่มโดยไม่จำเป็น และเสี่ยงให้ตัวเลขสองแหล่งไม่ตรงกัน |
| อ่าน deny-list ตรงจาก DB ทุกครั้งที่ query event เข้ามา (query-time DB lookup) | ผิดกับ "no I/O on hot path" contract ของ `RecordDNSEvent`/`recordDomainQuery` (DB read ทุก query จะทำให้ query-log watcher stall) |
| Cap `dns-stats-max-blocked-domains` = 300 (ข้อเสนอเดิม) | เจ้าของโปรเจกต์เลือก 1000 แทน เพื่อรองรับ blocklist สาธารณะขนาดใหญ่ในอนาคต (bulk import ยังไม่ทำในเฟสนี้ แต่เผื่อ headroom ไว้) |
| Endpoint ใหม่ `/api/statistics/dns/blocked` แยกจาก `/api/statistics/dns` | ขัดกับเป้าหมาย "ขยาย response เดิมแบบ additive" — เพิ่ม endpoint ใหม่จะทำให้ frontend ต้อง fetch 2 รอบ/มี 2 loading state โดยไม่จำเป็น ข้อมูลก็มาจาก ring เดียวกันอยู่แล้ว |
| ใช้ `blockIndex` ปัจจุบัน (query-time) ในการ set `blocked` field ตอน compose response (`GetDNSQueryStatistics` ฯลฯ) แทนการอ่านจาก `blockedInfo` ที่บันทึกไว้ใน bucket | จะทำให้ badge/field เปลี่ยนไปมาตามเวลาที่อ่าน (ไม่ record-time) — ขัดกับ §0 โดยตรง จึงต้องอ่านจาก `blockedInfo` ที่บันทึกไว้ ณ เวลา query เท่านั้น |

---

## 2. สถาปัตยกรรม

### 2.1 Backend

```
DNSServerService.ApplyAll()
  -> manager.ApplyZones(...)   [เดิม]
  -> (สำเร็จเท่านั้น) blockedDomainsSink(blocked)   [ใหม่ T-08]
       = StatisticsService.SetBlockedDomains
       -> dnsQueryStats.blockIndex.Set(rules)        [ใหม่ T-01/T-03]

kernel.DNSServerManager.WatchDNSLog -> StatisticsService.RecordDNSEvent (เดิม)
  -> recordDomainQuery(domain, qtype, client)
       1. blockIndex.Empty()/Match(domain)  — เช็คก่อนถือ s.dns.mu (lock ordering, T-03)
       2. s.dns.mu.Lock()
       3. b.queries++ (เดิม, ไม่เปลี่ยน)
       4. ถ้า matched: b.blockedQueries++ (uncapped), b.blockedInfo[domain] = {rule, mode}
          (capped ที่ dns-stats-max-blocked-domains ต่อ bucket)
       5. b.pairs[domain][client]++ ฯลฯ (เดิมทุกบรรทัด ไม่แตะ)

GetDNSQueryStatistics / GetDNSDomainClients / GetDNSClientDomains / GetDNSIPDomains
  (statistics_dns.go)
  -> เดินลูป bucket เดิม (ที่ RLock อยู่แล้ว) เพิ่มการ union b.blockedInfo เข้า
     windowBlockedInfo map[string]blockedMeta (ล่าสุดชนะ)
  -> เติม Blocked/BlockedRule/BlockedMode ให้แถว DNSDomainStat ทุกตัว, และ
     GetDNSQueryStatistics เพิ่มสร้าง topBlockedDomains/topBlockedClients จาก
     blockedDomainTotals/blockedClientTotals ที่สะสมจาก b.pairs เฉพาะ domain ที่อยู่ใน
     blockedInfo ของแต่ละ bucket (rankBlockedDomains/rankBlockedClients, dns_query_stats.go)
```

`dnsBlockIndex` (`internal/service/dns_block_index.go`) เป็น matcher แยกจาก ring หลัก:
`map[domain lower-case]mode`, mimick semantics ของ dnsmasq `address=/domain/` (ครอบทุก
subdomain) โดยเช็ค exact match ก่อน แล้ววนตัด label ทีละชั้น (สูงสุด 16 ชั้น, ใช้
`strings.IndexByte` ไม่ใช้ `strings.Split` เพื่อลด allocation) — ไม่ log domain ที่ query
(privacy) และไม่ I/O เลย

### 2.2 Frontend

`dnsStatisticsService.ts` ขยาย `DNSQueryStatistics`/`DNSDomainStat`/`DNSDomainDrilldown`
แบบ additive + type ใหม่ `DNSBlockedDomainStat`/`DNSBlockedClientStat` mirror ตาม
`docs/openapi.yaml` เป๊ะ — mock mode เพิ่ม fixture deny-list คงที่
(`doubleclick.net`/`googlesyndication.com`) จับคู่กับ 2 entry ใหม่ใน `mockPairs` ที่ตรงกับ
`kernel/mock.go`'s `mockDNSQueryEvents` (T-14)

`StatisticsDns.tsx`: stat-card แถวเดิมขยายเป็น `lg:grid-cols-5` (เพิ่ม "Blocked Queries"),
แถวกราฟใหม่ `lg:grid-cols-3` (`DnsQueryTrendCard` 2/3 + `DnsBlockedDonutCard` 1/3), เนื้อหา
เดิม (Top Domains/Top Source Hosts/IP-filter) ย้ายเข้า shadcn `<Tabs>` แท็บ "Query ปกติ"
ส่วนแท็บ "Blocked Query" ใหม่มี 2 ตาราง (`DnsBlockedStatsShared.tsx`) sync กับ `?tab=` —
พิมพ์ IP ครบขณะอยู่แท็บ blocked จะสลับกลับ "queries" อัตโนมัติ (คำนวณระหว่าง render ตาม
React's "adjusting state" pattern ไม่ใช่ `useEffect`, เพื่อเลี่ยง cascading-render lint error)

---

## 3. Task list (T-01 ถึง T-15)

Dependency order: `T-01,T-02,T-04,T-07,T-09,T-14,T-15` ทำคู่ขนานได้ก่อน → `T-03` → `T-08` →
`T-05` → `T-06,T-10,T-11` → `T-12` → `T-13`

| Task | ไฟล์หลัก | สรุป |
|---|---|---|
| T-01 | `backend/internal/service/dns_block_index.go` | `dnsBlockIndex` matcher (Set/Empty/Match) |
| T-02 | `backend/internal/service/dns_block_index_test.go` | เทสต์ exact/subdomain/invalid/concurrency |
| T-03 | `backend/internal/service/dns_query_stats.go` | `blockedMeta`, `domainBucket.blockedQueries/blockedInfo`, `recordDomainQuery` lock-ordering, `SetBlockedDomains`/`SetBlockedStatsLimit` |
| T-04 | `backend/internal/model/statistics.go` | DTO fields ใหม่ทั้งหมด (additive) |
| T-05 | `backend/internal/service/statistics_dns.go` + `dns_query_stats.go` | compose blocked-side ใน 4 endpoint composer, `rankBlockedDomains`/`rankBlockedClients` |
| T-06 | `backend/internal/service/dns_blocked_query_stats_test.go` | matrix/subdomain/badge/invariant/cap/empty/no-retroactive tests |
| T-07 | `backend/internal/config/config.go` (+test) | `dns-stats-max-blocked-domains`, default 1000 |
| T-08 | `dns_server.go`, `main.go`, `statistics.go` | `SetBlockedDomainsSink`, wiring ก่อน `InitApplyConfig()` |
| T-09 | `dns_query_stats.go` (bugfix) | ข้าม AnswerIP ที่ unspecified (sinkhole 0.0.0.0/::) ไม่ให้ปนเปื้อน reverse cache |
| T-10 | `backend/internal/api/handlers.go` (+test) | doc comment, ไม่มี query param ใหม่ |
| T-11 | `docs/openapi.yaml` + `frontend/public/openapi.yaml` | schema ใหม่ + field ใหม่ทุกจุด |
| T-12 | `frontend/src/services/dnsStatisticsService.ts` | type + mock fixture |
| T-13 | `DnsBlockedStatsShared.tsx`, `DnsBlockedDonutCard.tsx`, `DnsStatsShared.tsx`, `DnsQueryTrendCard.tsx`, `TrafficStatsShared.tsx`, `StatisticsDns.tsx` | UI ทั้งหมด |
| T-14 | `backend/internal/kernel/mock.go` | เพิ่ม ad-domain mock query event 2 รายการ |
| T-15 | เอกสารนี้ | บันทึกแผน |

---

## 4. Final acceptance

- `cd backend && go build ./... && go test ./...` ผ่านหมด (ไม่มี gcc ในสภาพแวดล้อม dev
  ที่ implement ฟีเจอร์นี้ — **`go test -race` ไม่ได้ถูกรันจริงในรอบนี้**, ต้องให้ CI/เครื่องที่มี
  gcc รันยืนยันอีกครั้งก่อน merge)
- `cd frontend && yarn build && yarn lint` ผ่าน ไม่มี error/warning ใหม่
- matrix ตรงกันทุกทิศ: Top Blocked Domains ของ D == ผลรวม client rows ใน drill-down ของ D ==
  ผลรวมแถว D จาก drill-down ของแต่ละ client (คุ้มครองด้วย
  `TestStatisticsService_BlockedQuery_MatrixAndDrilldown`)
- Badge "Blocked" ปรากฏถูกต้องใน Top Domains (tab queries), drill-down, IP-filter mode
- Tabs: URL เปลี่ยนเป็น `?tab=blocked`, พิมพ์ IP ครบขณะอยู่แท็บ blocked -> เด้งกลับ queries
- Donut: สัดส่วนตรงกับ `blockedPercent`, empty state เมื่อ `blockedQueries===0` หรือ
  `totalQueries===0`, dark/light อ่านออก (สีผ่าน ChartConfig เท่านั้น ไม่มี hex ตรงกลาง)
- ring เต็ม (ทดสอบด้วย `dns-stats-max-blocked-domains` ต่ำ ๆ) -> `blockedQueries` ยังถูกต้อง,
  ผลรวมแถวน้อยกว่า, มี banner เตือน (`blockedTruncated`)
- deny-list ว่าง -> response เหมือนก่อนมีฟีเจอร์ทุกประการ (คุ้มครองด้วย
  `TestStatisticsService_BlockedQuery_EmptyDenyListUnchanged`), ปิด DNS query logging -> blocked
  ทุกอย่างเป็น 0/empty
- ไม่มี `exec.Command` หรือเขียนสถิติลง SQLite เพิ่มเข้ามาเลย
- ไม่มี hardcoded color class/`shadow-*`/`backdrop-blur-*` ใหม่
- `docs/openapi.yaml` และ `frontend/public/openapi.yaml` ตรงกัน (`diff` แล้วไม่มีผลต่าง) และตรง
  กับ Go struct ทุก field

---

## 5. จุดที่ไม่แน่ใจ/decision ที่ตัดสินใจเองระหว่าง implement

1. **`go test -race` รันไม่ได้ในสภาพแวดล้อม dev ที่ใช้ implement** (ไม่มี `gcc`/C compiler ติดตั้ง
   — `CGO_ENABLED=1` ต้องใช้ cgo สำหรับ `-race`) — รันแค่ `go test ./...` ธรรมดาผ่านหมด แต่
   **QA/CI ต้องรัน `-race` ยืนยันอีกครั้ง** ก่อน merge จริง โดยเฉพาะจุด lock-ordering ใน
   `recordDomainQuery` (เช็ค `blockIndex` ก่อนถือ `s.dns.mu`) และ `dnsBlockIndex`'s
   `sync.RWMutex`
2. **empty state ของแท็บ "Blocked Query"**: spec บอกให้ลิงก์ไปหน้า DNS Server > Blocked Domains
   "เมื่อยังไม่มี deny-list" แต่ backend response ไม่มี field บอกว่า deny-list ว่างหรือมีแต่ไม่ match
   เลย — ใช้เกณฑ์ `totalBlockedDomains === 0` แทน (ครอบคลุมทั้งสองกรณี: ไม่มี deny-list เลย, หรือ
   มี deny-list แต่ไม่มี query ไหน match ในช่วงเวลานี้) ซึ่งอาจแสดงข้อความ "ยังไม่มี query ที่ถูก
   บล็อก" ทั้งที่จริง ๆ มี deny-list อยู่แล้วแค่ไม่มีคน query โดเมนนั้นในช่วงนี้ — ยอมรับได้เพราะ
   ข้อความ/ปุ่มลิงก์ยังถูกต้องและเป็นประโยชน์ทั้งสองกรณี
3. **`DnsQueryTrendCard`'s stacked bar เมื่อมี `blockedSeries`**: เปลี่ยนจาก single `Bar` เป็น
   stacked `allowed`+`blocked` two-bar เมื่อ prop ถูกส่งมา — คำนวณ `allowed = count -
   blockedCount` ที่ frontend (ไม่ได้มาจาก backend โดยตรง) เพราะ backend ส่งแค่ `querySeries`
   (all) กับ `blockedSeries` (blocked only) ไม่ได้ส่ง `allowedSeries` แยก — สอดคล้องกับ
   invariant `blockedQueries` รวมอยู่ใน `totalQueries` อยู่แล้ว
4. **Mock deny-list ใน frontend เป็นค่าคงที่เสมอ** (`mockBlockedDomains` ใน
   `dnsStatisticsService.ts`) ไม่ได้ผูกกับ mock ของ `dnsServerService`'s blocked-domains CRUD
   endpoint (ถ้ามี) — เพียงพอสำหรับ exercise UI ในโหมด `-mock`/`yarn dev` แต่ไม่ใช่ full
   end-to-end mock ของ "ผู้ใช้เพิ่ม deny-list เองแล้วเห็นผลทันที" (ฟีเจอร์นั้นมีอยู่แล้วจาก
   `dns-blocked-domains-plan.md` และไม่ได้อยู่ใน scope ของแผนนี้)
