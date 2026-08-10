# Statistics — Capacity Visibility (RAM ring/index usage vs cap, 9 dimensions)

> เอกสารแผนงานสำหรับฟีเจอร์: เพิ่ม visibility ให้ผู้ใช้เห็น current usage เทียบ cap ของ
> in-memory tracking structure ทั้ง 9 มิติ (traffic hosts/dests/conversations ring,
> firewall deny sources/ports ring, DNS pairs/clients ring, DNS reverse cache, DNS
> domain→IP forward index) ที่ปัจจุบันมีแค่ boolean `truncated` (after-the-fact, บอกว่า
> "ตอนนี้ตกหล่นแล้ว") — เปลี่ยนเป็น early-warning ที่เห็น % ใช้งานได้ก่อนเต็ม
>
> วันที่เขียน: 2026-08-06 · Branch อ้างอิง: `feat/statistics-capacity-visibility`
> อ้างอิง GitHub issue #123
> อ่านก่อน: `CLAUDE.md`, `docs/tech_stack_design.md` §8 (no-SQLite-for-runtime-state),
> `docs/ref/complete/statistics-dns-page-revamp-plan.md` (ring/index ที่งานนี้อ่านต่อ),
> `docs/ref/todo/dns-stats-tracking-limits-config-plan.md` (config-key pattern),
> `docs/ref/todo/statistics-traffic-page-plan.md` §1.6 (traffic-stats-max-* pattern)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย:**
- ผู้ใช้เปิดหน้า Statistics (Overview/Traffic/DNS) แล้วเห็น pill เล็ก ๆ ข้าง window
  tabs บอก % การใช้งานของ ring/index ที่ตึงที่สุดในกลุ่มที่หน้านั้นเกี่ยวข้อง พร้อมสี
  เตือน (warn ≥70%, danger ≥90% หรือมี bucket เต็มแล้ว)
- คลิก pill แล้วไปหน้า `/statistics/capacity` เห็นรายละเอียดครบทั้ง 9 ring: current/cap,
  peak%, จำนวน bucket ที่เคยเต็ม, ที่มาของ cap (ชื่อ config key), กราฟต่อ bucket
  (ring แบบ `bucket`) หรือแถบเดียว (index แบบ `flat`), และ "RAM โดยประมาณ" ที่ระบุชัดว่า
  เป็นค่าประมาณ ไม่ใช่ค่าที่วัดจริง
- Endpoint ใหม่ `GET /api/statistics/capacity` คืนครบ 9 ring เสมอ ลำดับคงที่ ไม่มี PII

**นอกขอบเขต (ตัดทิ้งชัดเจน):**
- ไม่ทำ alert/notification เชิงรุก (push, email) — หน้านี้เป็น pull-based dashboard
  เท่านั้น ผู้ใช้ต้องเข้ามาดูเอง
- ไม่เปลี่ยนพฤติกรรม eviction/cap ของ ring ใด ๆ ทั้ง 9 ตัว — งานนี้เป็น read-only
  observability ล้วน ไม่แตะ write-path
- ไม่เพิ่ม SQLite table/migration ใด ๆ (ตาม tech_stack_design §8) — ทุกอย่างคำนวณสด
  ตอนมี request
- ไม่ทำ historical retention ของตัวเลข capacity เอง — `Series` ที่ส่งกลับมาใช้แกนเวลา
  เดียวกับของเดิม (มาจาก ts ที่ ring บันทึกไว้แล้ว) ไม่ใช่ ring ใหม่
- T-14 (ย้าย deny-ring cap เป็น config key) เป็นส่วนหนึ่งของแผนนี้ที่ผู้ใช้อนุมัติแล้ว
  ไม่ใช่ scope creep — ทำเพราะ `capSource` ของอีก 7 ring ต้องชี้ config key จริงได้
  ทั้งหมด มีแค่ deny ring 2 ตัวที่ยังเป็น const ฝังโค้ด

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Traffic bucket ring (hosts/dests/conversations) | มี cap เป็น field ต่อ service (`maxTrackedHosts/Dests/Conversations`) + `Truncated bool` เท่านั้น ไม่มี per-bucket count reader ที่ export | `backend/internal/service/traffic_stats.go:285-293`, `:1490` (truncated check) |
| Firewall deny ring (sources/ports) | cap เป็น **const** (`maxTrackedDenySources=500`, `maxTrackedDenyPorts=300`) ไม่ใช่ config key, มีแค่ `denySnapshot` คืน totals+truncated | `backend/internal/service/statistics.go:42-44`, `:152-174` |
| DNS pairs/clients ring | cap เป็น field ของ `dnsQueryStats` (`maxPairs/maxClients`) มาจาก config `dns-stats-max-pairs/-clients` แล้ว | `backend/internal/service/dns_query_stats.go:100-108`, `backend/internal/config/config.go:160-163` |
| DNS reverse cache | มี `ttl`/`maxSize` เป็น field, ไม่มี accessor อ่านขนาดปัจจุบันแบบไม่มี side effect (ทุก accessor ที่มีอยู่ทำ eviction) | `backend/internal/service/dns_reverse_cache.go:22-27` |
| DNS domain→IP forward index | มี `caps()` (คืน max) และ `Truncated()` อยู่แล้ว แต่ไม่มี accessor อ่าน `len(byDomain)` ปัจจุบัน | `backend/internal/service/dns_domain_ips.go:142-151`, `:412-420` |
| model DTO สำหรับ capacity | ยังไม่มี | — |
| composer/handler/route สำหรับ capacity | ยังไม่มี | — |
| Frontend service/component/หน้า capacity | ยังไม่มี | — |
| Config key `deny-stats-max-sources/-ports` | ยังไม่มี (ยัง hardcode เป็น const) | `backend/internal/service/statistics.go:42-44` |
| `statsSeriesAxis`/`statsSeriesIndex`/`percentOf`/`normalizeStatsWindow` (helper ที่จะ reuse) | มีอยู่แล้ว ใช้ร่วมกันทั้งโปรเจกต์ | `backend/internal/service/traffic_stats.go:117-185`, `:1594` |

สรุป: งานจริงกระจุกอยู่ที่การเพิ่ม **read-only reader** ต่อ ring/index ที่มีอยู่แล้วทั้ง 5
โครงสร้าง (T-02..T-05) แล้ว compose เป็น response เดียว (T-06) — ไม่มี ring/index ใหม่
ไม่มี kernel capability ใหม่ ไม่มี DB ใหม่

## 2. แนวทางเทคนิค

- **Reader แยกจาก writer เสมอ**: ทุก accessor ใหม่ (`CapacityUsage`, `denyCapacity`,
  `dnsRingCapacity`, `Usage()` x2) เป็น read-only, ใช้ `RLock` (หรือ `Lock` เฉพาะ
  `dnsDomainIPs.Usage()` ที่ต้อง evict ก่อนนับ — แต่ตาม instruction T-05 **ห้าม
  evict** ในนี้ ดังนั้นใช้ `RLock` อ่าน `len(byDomain)` ตรง ๆ แม้จะรวม entry ที่หมดอายุ
  แล้วแต่ยังไม่ถูก sweep (เป็น upper bound ตามที่ comment อธิบาย)
- **Composer เดียว ประกอบ 9 ring ตามลำดับคงที่** (`statistics_capacity.go`) — ไม่ผูก
  ลำดับกับ iteration ของ map ใด ๆ, ประกาศเป็น slice literal ตรง ๆ
- **Metadata (id/group/label/capSource/entryBytes) เป็น literal ในไฟล์เดียว** —
  ไม่กระจายไปหลายไฟล์ ป้องกัน drift
- **ทางเลือกที่ตัดทิ้ง**: เคยพิจารณาให้แต่ละ ring คืนค่า capacity ของตัวเองปนกับ response
  เดิม (เช่น เพิ่ม field ใน `TrafficStatistics`) แต่ตัดทิ้งเพราะ (ก) ผิด layer — response
  เดิมมี regression-guard ห้ามเปลี่ยนรูป (ข) capacity ไม่ผูกกับ window ที่ผู้ใช้เลือกดู
  โดยตรง (RAM ที่ใช้จริงมาจากทั้ง ring ไม่ใช่แค่หน้าต่างเวลาที่กำลังดู) ควรเป็น endpoint
  แยก
- **Pattern แม่แบบที่ทำตาม**: `denySnapshot` (statistics.go) สำหรับโครง
  RLock→เลือก bucket ย้อนหลัง→คำนวณ axis ก่อน lock (traffic_stats.go's
  `statsSeriesAxis`/`statsSeriesIndex`), `SetReverseCacheLimits`/`SetDomainIPsLimits`
  (dns_query_stats.go) สำหรับ pattern การ wire ค่า config เข้า service, config.go's
  `traffic-stats-max-hosts` สำหรับ pattern เพิ่ม config key แบบ file-only

## 3. ขั้นตอนการทำ (T-00 ถึง T-14 — ดู task list เต็มในข้อความสั่งงานต้นทาง)

ลำดับ dependency: T-00 → T-01 → {T-02, T-03, T-04, T-05 ขนานกัน} → T-06 → T-07 →
{T-08, T-09} → T-10 → T-11 → T-12 → T-13 → T-14 (ปิดท้ายตามที่ผู้ใช้กำหนด แม้ตัว config
key จะถูก "ใช้" ตั้งแต่ T-06 ก็ตาม — T-06 อ้าง capSource ของ deny ring แบบ hardcode
string ไปก่อน แล้ว T-14 มาเปลี่ยนตัว service field ทีหลังโดยไม่ต้องแก้ T-06 อีกครั้ง
เพราะ capSource เป็นแค่ string ที่ไม่ผูก type กับ const)

- **T-01** `backend/internal/model/statistics.go` — เพิ่ม `CapacityPoint`,
  `RingCapacity`, `CapacityStatistics` ต่อท้ายไฟล์
- **T-02** `backend/internal/service/traffic_stats.go` — เพิ่ม
  `CapacityUsage(window string)` อ่าน per-bucket len ของ hostBytes/dstBytes/convBytes
- **T-03** `backend/internal/service/statistics.go` — เพิ่ม `denyCapacity(window string)`
  (unexported) อ่าน per-bucket len ของ srcCount/portCount — **ห้ามแก้ `denySnapshot`**
- **T-04** `backend/internal/service/dns_query_stats.go` — เพิ่ม
  `dnsRingCapacity(window string)` (unexported) อ่าน pairCount/len(clientCount) ต่อ
  bucket + สถานะ `enabled`
- **T-05** `backend/internal/service/dns_reverse_cache.go` +
  `backend/internal/service/dns_domain_ips.go` — เพิ่ม `Usage()` ทั้งคู่ (ไม่มี eviction)
- **T-06** ไฟล์ใหม่ `backend/internal/service/statistics_capacity.go` — composer
  `GetCapacityStatistics(window string, withSeries bool) model.CapacityStatistics`
- **T-07** `backend/internal/api/handlers.go` + `router.go` — handler +
  `GET /api/statistics/capacity` (authRoute)
- **T-08** `backend/internal/service/statistics_capacity_test.go` (ใหม่) +
  `backend/internal/api/handlers_test.go` (แก้เพิ่ม)
- **T-09** `docs/openapi.yaml` + `frontend/public/openapi.yaml`
- **T-10** ไฟล์ใหม่ `frontend/src/services/capacityService.ts`
- **T-11** ไฟล์ใหม่ `frontend/src/components/statistics/CapacityIndicator.tsx`
- **T-12** `frontend/src/pages/StatisticsOverview.tsx` /
  `StatisticsTraffic.tsx` / `StatisticsDns.tsx` — ติด indicator
- **T-13** ไฟล์ใหม่ `frontend/src/pages/StatisticsCapacity.tsx` + แก้ `App.tsx` +
  `app-sidebar.tsx`
- **T-14** `backend/internal/config/config.go` + `statistics.go` + `main.go` —
  `deny-stats-max-sources`/`-ports` เป็น config key จริง (default 500/300)

> **ไม่ต้องทำ**: ไม่ต้อง `InitApplyConfig()` ใหม่ (ไม่มี state ให้ apply ตอน boot —
> capacity คำนวณสดทุก request), ไม่ต้องแก้ `netlink_monitor.go` (ไม่แตะ
> routing/interface), ไม่ต้องมี ticker/goroutine ใหม่ (คำนวณตอนมี HTTP request เท่านั้น)

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/statistics/capacity` | `authRoute` (ทุก role อ่านได้ — response มีแค่ตัวเลขนับ ไม่มี IP/domain/hostname) | คืน `model.CapacityStatistics`: 9 ring ตามลำดับคงที่, `window` ผ่าน whitelist เดิม (`statsWindowParam`), query param `series` (`"1"`/`"true"` → true, อื่น ๆ → false เสมอ ไม่ error) |

โหมด `-disable-edit=true` ไม่เกี่ยว (endpoint นี้เป็น GET ล้วน ไม่มี mutation อยู่แล้ว)

## 5. ข้อควรระวัง

1. **Privacy/PII** — response ต้องไม่มี domain/IP/hostname เลยแม้แต่ field เดียว
   (ตัวเลขนับล้วน) เพราะ role คือ `authRoute` ไม่ใช่ `superAdminRoute` — ถ้ามีการเผลอใส่
   ตัวอย่าง top domain/IP ลงไปเพื่อ debug จะกลายเป็น privacy leak ให้ทุก role เห็น ต้อง
   grep เช็คก่อนส่งงาน
2. **Lock ซ้อน** — `dnsRingCapacity`/`denyCapacity`/`CapacityUsage` ต้องเป็น
   RLock-แล้ว-ปล่อย เหมือน `denySnapshot`/`domainSnapshot` เดิม ห้ามถือ lock ของ ring
   หนึ่งพร้อมกับอีก ring ระหว่าง compose ใน T-06 (ต่างจาก statistics_dns.go ที่ join
   ข้าม service ได้เพราะปล่อย lock ก่อนแล้ว — ที่นี่ไม่มีการ join ข้าม ring เลย ยิ่งง่ายกว่า)
3. **RAM estimate เข้าใจผิดง่าย** — `EstimatedBytes` ต้องคูณจาก entries ที่อยู่ใน RAM
   "ทั้ง ring" (24h เต็ม) ไม่ใช่แค่ window ที่ผู้ใช้กำลังดู (เช่นเลือกดู "15m" แต่ ring เก็บ
   24h เสมอ) — ต้อง comment เตือนจุดนี้ให้ชัดใน `statistics_capacity.go` ไม่งั้นคนอ่าน
   โค้ดจะงงว่าทำไม EstimatedBytes ไม่ผูกกับ `window`/`Series`
4. **Deny ring cap เปลี่ยนจาก const เป็น field (T-14)** — ทุกจุดที่เคยอ้าง
   `maxTrackedDenySources`/`maxTrackedDenyPorts` เป็น const (`RecordFirewallLog`,
   `denySnapshot`, และ `denyCapacity` ใหม่จาก T-03) ต้องเปลี่ยนเป็น `s.<field>`
   ทั้งหมดในการแก้ครั้งเดียว ไม่งั้น build พังหรือ (แย่กว่า) ใช้ค่าไม่ตรงกันระหว่างจุด
5. **Backward compatibility ของ config file เก่า** — ไฟล์ config ที่ผลิตไปแล้วไม่มี
   `deny-stats-max-sources/-ports` ต้องยัง resolve ได้ปกติด้วยค่า default 500/300 เป๊ะ
   (Resolve เริ่มจาก `defaults` เสมอ, key ที่ไม่มีในไฟล์ไม่ทำให้ error) — เพิ่ม key ต่อท้าย
   `orderedKeys` เท่านั้น ห้ามแทรกกลาง (ไม่งั้น diff ของไฟล์ config ที่ generate ไว้แล้ว
   จะเปลี่ยนตำแหน่งทุกบรรทัดถัดไป)
6. **regression guard** — ห้ามแก้ `denySnapshot`, `GetStatistics`, `GetDNSQueryStatistics`,
   `GetTrafficBreakdown`/`GetTrafficBreakdownForIP`/`GetTrafficBreakdownForDests` แม้แต่
   บรรทัดเดียว (นอกจากเปลี่ยน const→field ตาม T-14 ซึ่งเป็นการเปลี่ยนที่ไม่กระทบ behavior/
   JSON shape) — `/api/statistics/traffic` และ `/api/statistics/dns` ต้องคืน payload
   เดิมทุกประการ
7. **Frontend semantic color** — ห้าม hardcode `text-amber-500` ฯลฯ ต้องใช้
   `text-warning`/`text-destructive`/`text-muted-foreground` ตาม `docs/rules_of_work.md`,
   ห้าม `shadow-*`/`backdrop-blur-*`
8. **ทดสอบ**: mock mode (`-mock=true`) ครอบคลุมได้ทั้งหมด (ไม่มีอะไรต้องใช้ hardware จริง)
   — ring/index ทั้ง 9 ตัวเป็น RAM-only มาตั้งแต่เดิม ทดสอบยิง firewall log event/DNS
   query event ปลอมผ่าน service ตรง ๆ ใน unit test ได้ ไม่ต้องพึ่ง real kernel

## 6. Checklist สรุป (Definition of Done)

- [ ] T-00 เอกสารแผนงานนี้
- [ ] T-01 `model/statistics.go` — 3 struct ใหม่ต่อท้ายไฟล์
- [ ] T-02 `traffic_stats.go` — `CapacityUsage`
- [ ] T-03 `statistics.go` — `denyCapacity` (ไม่แก้ `denySnapshot`)
- [ ] T-04 `dns_query_stats.go` — `dnsRingCapacity` (ไม่แตะ hot path)
- [ ] T-05 `dns_reverse_cache.go` + `dns_domain_ips.go` — `Usage()` x2 (read-only)
- [ ] T-06 `statistics_capacity.go` ใหม่ — `GetCapacityStatistics`
- [ ] T-07 `handlers.go` + `router.go` — endpoint ใหม่ (authRoute, ไม่ echo input)
- [ ] T-08 unit test backend (service + api)
- [ ] T-09 `docs/openapi.yaml` + `frontend/public/openapi.yaml` sync กัน
- [ ] T-10 `capacityService.ts` (types + mock ครบสีทุกสถานะ)
- [ ] T-11 `CapacityIndicator.tsx` (Badge เท่านั้น, semantic color, dark/light)
- [ ] T-12 ติด indicator 3 หน้า (poll เดียวกับของเดิม, fail แล้วไม่พังหน้า)
- [ ] T-13 `StatisticsCapacity.tsx` + route + sidebar menu
- [ ] T-14 `deny-stats-max-sources/-ports` เป็น config key จริง (backward compatible)
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน (test เดิมทุกไฟล์ไม่มี diff)
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน ไม่มี warning ใหม่
- [ ] `bash build.sh` ได้ `./pigate` ไฟล์เดียว
- [ ] grep ยืนยัน: ไม่มี `exec.Command`/ticker/goroutine/SQLite migration ใหม่จากงานนี้
- [ ] `/api/statistics/traffic` และ `/api/statistics/dns` คืน payload เดิมทุกประการ
- [ ] response ของ endpoint ใหม่ไม่มี domain/IP/hostname แม้แต่ field เดียว
