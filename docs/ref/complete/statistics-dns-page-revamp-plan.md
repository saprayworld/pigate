# Statistics → DNS page revamp (domain-centric stats + volume + drill-down)

> **Status: implemented, QA passed.** All tasks T-01 through T-13 landed on
> branch `feat/statistics-dns-page`. Backend build/vet/test and frontend
> build/lint are clean; `bash build.sh` produces the binary. QA verified the
> plan's §5 Final Acceptance checklist live against `-mock=true` — the
> byte-join math, the `sum(series[].bytes) == totalBytes` invariant, input
> validation (400s with no input echo), auth (401 without session), and the
> disabled-logging empty-non-nil-slice contract all hold. Regression guard
> confirmed: `TopDomain`/`TopHost`/`TopConversation`/`TrafficStatistics` in
> `model/statistics.go` are byte-for-byte untouched, `statistics_test.go` has
> zero diff, and the Overview page's Top Queried Domains card is unchanged.
> Security-sensitive files (`dns_domain_ips.go`, `statistics_dns.go`, the DNS
> handlers in `handlers.go`) were reviewed clean — no nested locks, no I/O or
> domain-name logging on the hot path, unchanged validation contract, no new
> `exec.Command`, `internal/kernel/interfaces.go`/`internal/db` untouched.
> The shared-IP client-drilldown attribution edge case noted during
> development (§ "known flagged item" below) is intentional and covered by
> `TestGetDNSClientDomains_VolumeJoin` plus the `DnsVolumeInfoButton` UI
> disclaimer — not a bug.
>
> **One outstanding pre-merge action item:** `go test -race
> ./internal/service/...` could not be run anywhere in this pipeline (no C
> toolchain / `gcc` available in the dev/QA/tech-lead sandboxes). This was an
> explicit §5 Final Acceptance item — run it on a machine with a C toolchain
> (CI or the owner's real dev box) before merging.

> Goal: ยกระดับหน้า **Statistics → DNS** ให้เทียบชั้นกับ **Statistics → Traffic** — overview table ที่
> sort ได้ทุกคอลัมน์และมี Volume (Down/Up) + drill-down ที่บอกได้ว่าโดเมนหนึ่งถูก resolve เป็น IP อะไรบ้าง
> และ IP ไหนกินปริมาณข้อมูลมากที่สุด
>
> Written: 2026-08-04 · Planned branch (create AFTER owner approval): `feat/statistics-dns-page`
> Read first: `CLAUDE.md`, `docs/tech_stack_design.md` (§8 no-SQLite-for-runtime-state),
> `docs/rules_of_work.md` (frontend), `docs/ref/complete/statistics-traffic-page-plan.md`
> (the pattern we mirror), `docs/ref/complete/dns-query-statistics-drilldown-plan.md`
> (the ring this plan extends), `docs/ref/complete/statistics-dns-top-domain-plan.md`
> (reverse cache + privacy rules), `docs/ref/complete/dns-stats-tracking-limits-config-plan.md`
> (config-key pattern).

---

## 0. Current state (verified against the working tree, 2026-08-04)

| Piece | State today | Reference |
|---|---|---|
| DNS event source | dnsmasq query log tailed from **tmpfs** `/run/pigate/dnsmasq-queries.log`, parsed into `model.DNSLogEvent` (`DNSLogQuery{Domain,QueryType,ClientIP}` / `DNSLogAnswer{Domain,AnswerIP}`) | `kernel/dns_query_log.go`, `kernel/real_dns_query_log.go` |
| DNS aggregation | RAM ring 288 × 5-min buckets keyed by **(domain, client) pair** → `pairs[domain][clientIP] = count`, `clientCount`, `typeByDomain`, `queries`; caps `dns-stats-max-pairs` / `dns-stats-max-clients` | `service/dns_query_stats.go:65-192` |
| Answer handling | `DNSLogAnswer` → `dnsReverseCache.Put(ip, domain)` — **IP → domain only**, last-writer-wins, TTL+size capped, has an internal `multi` flag that is never exposed | `service/dns_reverse_cache.go` |
| Traffic volume | RAM ring 288 × 5-min buckets: `hostBytes` (src IP), `dstBytes` (dst IP), `convBytes` (`src\|dst\|proto\|dstPort`), `observed`; accessor `GetTrafficBreakdown(window)` / `GetTrafficBreakdownForIP(window, ip)` (adds `HostSeries`) | `service/traffic_stats.go:1206-1420` |
| DNS API | `GET /api/statistics/dns`, `/dns/domain?domain=`, `/dns/client?client=` — count/percent only, no bytes | `api/router.go:52-54`, `api/handlers.go:448-506` |
| DNS DTOs | `TopDomain`, `DNSClientStat`, `DNSQueryStatistics`, `DNSDomainDrilldown`, `DNSClientDrilldown` | `model/statistics.go:79-255` |
| DNS frontend | `pages/StatisticsDns.tsx` (2 static tables, no sort/filter), `StatisticsDnsDomain.tsx`, `StatisticsDnsClient.tsx`, shared `components/statistics/DnsStatsShared.tsx` (`DomainStatsTable`/`ClientStatsTable` — plain `<TableHead>`, no sorting) | — |
| Traffic frontend (pattern) | `pages/StatisticsTraffic.tsx` + `StatisticsTrafficHost.tsx`; reusable `useSortableRows` / `SortableHead` / `useTextFilter` / `TrafficFilterInput` / `TrafficStatCard` / `TruncatedWarning` / `AccuracyInfoButton` in `components/statistics/TrafficStatsShared.tsx`, `TrafficTrendCard`, `HostHeaderCard`, `HostLabel`, `fmtBytes`/`fmtRate` | — |

### 0.1 Data-sufficiency verdict (requirement by requirement)

| Requirement | Available today? | Gap / action |
|---|---|---|
| จำนวนครั้งที่โดเมนถูกค้นหา | ✅ `pairs[domain][*]` summed | none |
| จำนวน client ต่อ domain / query ต่อ client | ✅ `pairs` (both directions) | เพิ่มแค่ "distinct client count" ที่ derive ได้จาก map เดิม |
| 1 โดเมนมี IP อะไรบ้าง | ❌ **ไม่พอ** — มีแต่ `dnsReverseCache` (IP→domain, last-writer-wins, ไม่มี index ย้อนกลับ) โดเมนที่ตอบหลาย IP จะเห็นได้ไม่ครบ และ IP ที่ถูกใช้ร่วมหลายโดเมนจะทับกันเอง | **เพิ่ม forward index `domain → {ip: lastSeen}`** ใน service layer (T-02) เก็บจาก event `DNSLogAnswer` เดิม (ไม่ต้องแตะ kernel/parser) |
| เรียง IP ตาม volume | ⚠️ ครึ่งเดียว — byte ต่อ IP มีอยู่แล้วใน `TrafficBreakdown.Dests` แต่ไม่มีสะพานเชื่อม domain ↔ IP | join `domainIPs` (T-02) กับ `Dests` ตอน compose response (T-04) |
| Volume Down/Up ในตาราง overview | ⚠️ ต้อง join — per-domain = Σ `Dests[ip]`, per-client = `Hosts[clientIP]` | T-04 |
| Drill-down ครบเหมือน Traffic (time series ฯลฯ) | ⚠️ per-client series ใช้ `GetTrafficBreakdownForIP` ที่มีอยู่แล้วได้เลย; per-domain series ต้องรวมหลาย dst IP | เพิ่ม `GetTrafficBreakdownForDests(window, ips)` (T-03) |

**สรุป: ไม่ต้องเพิ่ม kernel capability ใหม่แม้แต่ตัวเดียว** — event `DNSLogAnswer` ที่มี `AnswerIP`
ถูก parse อยู่แล้ว (`kernel/dns_query_log.go:95-113`) แค่ปัจจุบันโยนทิ้งเข้า reverse cache ทางเดียว
ไฟล์ใต้ `internal/kernel/` ที่แผนนี้แตะมีไฟล์เดียวคือ `mock.go` (ข้อมูลจำลองให้ครอบคลุมเคสใหม่)
และ **ไม่มีการเพิ่ม method ใน `interfaces.go`** จึงไม่มี `real_*.go` ที่ต้องแก้ตาม

---

## 1. Design decisions (settled — developer ต้องไม่ตัดสินใจใหม่เอง)

### 1.1 Volume ต่อโดเมนเป็น "ค่าประมาณจากการ join" ไม่ใช่การนับจริงต่อโดเมน
ระบบไม่ได้ (และจะไม่) ผูก byte counter เข้ากับชื่อโดเมนที่ระดับ packet — byte มาจาก conntrack ต่อ **IP**
เท่านั้น การแสดง "volume ของโดเมน" จึงคือ **Σ byte ของ IP ที่โดเมนนั้นเคยถูกตอบ** ภายในหน้าต่างเวลานั้น
ผลที่ตามมาที่ต้องยอมรับและต้องบอกผู้ใช้ใน UI:
1. IP ที่ใช้ร่วมกันหลายโดเมน (CDN/cloud LB) จะถูกนับให้ **ทุกโดเมน** ที่อ้างถึงมัน → ผลรวมของทุกแถวมากกว่า
   traffic จริงได้ → ต้องมี flag `sharedIPs` ต่อโดเมน และ `shared` ต่อแถว IP
2. traffic ที่วิ่งไปยัง IP ตรง ๆ โดยไม่ผ่าน DNS จะไม่ถูกนับให้โดเมนใดเลย
3. mapping domain→IP เป็น "ความรู้ล่าสุด" (มี TTL) ไม่ได้ผูกกับ bucket เวลา — โดเมนที่ resolve ไว้เมื่อ 2 ชม.
   ก่อนแล้ว entry หมดอายุ จะทำให้ volume หายไปจากแถวนั้น
4. ห้ามนำตัวเลข/ mapping นี้ไปใช้ generate firewall rule, policy matching, routing หรือ QoS เด็ดขาด
   (กติกาเดิมของ reverse cache — `statistics-dns-top-domain-plan.md` §5 item 6) มันถูก poison ได้โดย
   LAN client ทุกเครื่อง

UI ต้องมีปุ่ม info (Popover แบบ `AccuracyInfoButton`) อธิบายข้อ 1-3 เป็นภาษาไทยในทั้งหน้า overview และ
drill-down

### 1.2 Per-client × per-domain volume คำนวณจาก `convBytes` (แม่นกว่า)
ใน drill-down (ทั้งฝั่ง domain และ client) เรารู้ทั้ง src และ dst จาก `convBytes` key (`src|dst|proto|port`)
จึงคำนวณ "byte ระหว่าง client X กับ IP ของโดเมน Y" ได้ตรง ๆ ไม่ต้องเดา — ใช้เส้นทางนี้เสมอสำหรับ
drill-down; ส่วนตาราง overview (network-wide) ใช้ `Dests`/`Hosts` ตามข้อ 1.1

### 1.3 Percent มีสองหน่วยและห้ามปนกัน
- `percent` = % ของ **จำนวน query** (ความหมายเดิม ห้ามเปลี่ยน)
- `bytesPercent` = % ของ **byte** เทียบกับ denominator ที่ระบุใน DTO ของแต่ละ endpoint
UI ต้องติดป้ายสองคอลัมน์นี้ให้ต่างกันชัดเจน (`% Query` กับ `% Vol`)

### 1.4 Filter/sort ทำฝั่ง client ทั้งหมด
เหมือนหน้า Traffic เป๊ะ ๆ — backend ไม่รับ sort key จาก client (input จาก client มีแค่ window whitelist,
domain ที่ผ่าน `model.NormalizeQueryDomain`, client IP ที่ผ่าน `netip.ParseAddr`, และ limit ที่ clamp)
ใช้ `useSortableRows`/`SortableHead`/`useTextFilter` จาก `TrafficStatsShared.tsx` ที่มีอยู่แล้ว ห้ามเขียน
กลไก sort ใหม่

### 1.5 Backward compatibility ของ DTO
- `model.TopDomain` และ `model.TrafficStatistics` (response ของ `/api/statistics/traffic` + การ์ด Top
  Queried Domains บนหน้า Overview) ต้อง **ไม่เปลี่ยนรูป JSON** — field ใหม่ทั้งหมดไปอยู่ใน struct ใหม่ที่
  **embed** `TopDomain` (`DNSDomainStat`) ซึ่งใช้เฉพาะ 3 endpoint ของ DNS
- `model.DNSClientStat` เพิ่ม field ได้ (additive) — frontend เดิมไม่พัง

### 1.6 ไม่มีอะไรลง SQLite
index domain→IP ใหม่เป็น RAM-only เหมือน ring อื่น ๆ ทั้งหมด (tech_stack_design §8, SD-card wear)
`SetDNSLoggingEnabled(false)` / `ClearDNSStats()` ต้องล้าง index ใหม่นี้ด้วย (privacy)

---

## 2. Architecture

```
kernel (ไม่แตะ, ยกเว้น mock.go)
  DNSServerManager.WatchDNSLog  ──> model.DNSLogEvent {Query|Answer}
                                        │
service                                 ▼
  StatisticsService.RecordDNSEvent  ─┬─ Query  -> dnsQueryStats.pairs ring   (เดิม)
                                     └─ Answer -┬-> dnsReverseCache (IP->domain, เดิม)
                                                └-> dnsDomainIPs  (domain->{IP:lastSeen})  ★ ใหม่ T-02
  TrafficStatsService
     GetTrafficBreakdown(window)             (เดิม: Hosts/Dests/Convs/Observed/Series)
     GetTrafficBreakdownForIP(window, ip)    (เดิม: + HostSeries)  -> client drill-down
     GetTrafficBreakdownForDests(window, ips)★ ใหม่ T-03           -> domain drill-down series
  statistics_dns.go ★ ใหม่ (compose)  <- join pairs ring × domainIPs × traffic breakdown
                                        │
api (route เดิม 3 เส้น, handler เดิม, ไม่มี validation ใหม่)
  GET /api/statistics/dns          -> DNSQueryStatistics   (+ bytes, + clients/domain)
  GET /api/statistics/dns/domain   -> DNSDomainDrilldown   (+ ips[], + bytes, + series)
  GET /api/statistics/dns/client   -> DNSClientDrilldown   (+ bytes/domain, + series)
                                        │
frontend
  services/dnsStatisticsService.ts (types + mock)
  components/statistics/DnsStatsShared.tsx (ตาราง sort ได้ + คอลัมน์ volume + ตาราง IP ใหม่)
  pages/StatisticsDns.tsx / StatisticsDnsDomain.tsx / StatisticsDnsClient.tsx
```

### 2.1 โครงสร้างข้อมูลใหม่ (T-02) — `service/dns_domain_ips.go`

```go
type dnsDomainIPs struct {
    mu       sync.RWMutex
    byDomain map[string]map[string]time.Time // domain -> ip -> lastSeen
    ipRefs   map[string]int                  // ip -> จำนวนโดเมนที่อ้างถึง (ใช้ตั้ง flag shared)
    ttl      time.Duration                   // ใช้ค่าเดียวกับ dnsReverseCache (SetLimits)
    maxDomains  int                          // config: dns-stats-max-domains (default 1000)
    maxIPsPerDomain int                      // config: dns-stats-max-ips-per-domain (default 16)
}
```
กติกา:
- `Put(domain, ip)` ต้อง O(1), ไม่มี I/O, ไม่ log ชื่อโดเมน (เรียกจาก read-loop ของ watcher เหมือน
  `recordDomainQuery`)
- โดเมนใหม่ถูกปฏิเสธเมื่อ `len(byDomain) >= maxDomains`; IP ใหม่ในโดเมนเดิมถูกปฏิเสธเมื่อชนกับ
  `maxIPsPerDomain` (แต่ IP เดิมยัง refresh `lastSeen` ได้เสมอ) — ตั้ง flag truncated ให้ response
- lazy-evict entry ที่เกิน TTL ตอนอ่าน + `sweepExpired()` เกาะ ticker เดิมของ reverse cache
  (ห้ามสร้าง goroutine/ticker ใหม่)
- **ห้ามถือ lock นี้พร้อมกับ `s.dns.mu` หรือ lock ของ reverse cache** — ทุก accessor ต้อง snapshot
  ออกมาก่อนแล้วค่อยไป join
- worst case RAM ≈ 1000 × 16 × ~64B ≈ 1 MB — ระบุไว้ใน comment

Accessor ที่ต้องมี: `IPsFor(domain) []domainIP{IP, LastSeen, Shared}`, `Snapshot() map[string]string`
(ip → domain สำหรับ invert ใช้ใน drill-down ของ client — โดเมนที่ชน IP กันให้เลือกตัวที่ `lastSeen`
ใหม่สุดเพื่อความ deterministic), `Clear()`, `SetLimits(ttlMinutes)`

### 2.2 DTO ใหม่/ที่แก้ (T-01) — `model/statistics.go`

```go
// ใหม่ — embed TopDomain เพื่อคง JSON เดิมของ topDomains ไว้ครบ
type DNSDomainStat struct {
    TopDomain               // domain, queryType, count, percent (percent = % ของ "จำนวน query")
    Clients      int        `json:"clients"`       // จำนวน client ที่ถามโดเมนนี้ (distinct)
    IPCount      int        `json:"ipCount"`       // จำนวน IP ที่รู้จักของโดเมนนี้
    SharedIPs    bool       `json:"sharedIps"`     // มี IP อย่างน้อย 1 ตัวที่ใช้ร่วมกับโดเมนอื่น
    Bytes        uint64     `json:"bytes"`         // volume โดยประมาณ (ดู §1.1)
    BytesUp      uint64     `json:"bytesUp"`       // flow-relative: Orig = client -> server
    BytesDown    uint64     `json:"bytesDown"`     // Reply = server -> client
    BytesPercent float64    `json:"bytesPercent"`  // % ของ byte (denominator ระบุใน DTO แม่)
}

// ใหม่ — หนึ่งแถวของตาราง "IP ของโดเมนนี้" (เรียง bytes desc, tie-break ip asc)
type DNSDomainIP struct {
    IP           string  `json:"ip"`
    Bytes        uint64  `json:"bytes"`
    BytesUp      uint64  `json:"bytesUp"`
    BytesDown    uint64  `json:"bytesDown"`
    BytesPercent float64 `json:"bytesPercent"` // % ของ TotalBytes ของโดเมนนี้
    Shared       bool    `json:"shared"`       // IP นี้ถูกอ้างโดยมากกว่า 1 โดเมน
    LastSeen     string  `json:"lastSeen"`     // RFC3339 UTC, ครั้งล่าสุดที่ DNS ตอบ IP นี้
}

// แก้ (additive) — ความหมายของ bytes ขึ้นกับ DTO แม่ ต้องเขียน comment กำกับให้ชัด
type DNSClientStat struct {
    IP, Hostname string
    Count        uint64
    Percent      float64
    Domains      int     `json:"domains"`      // จำนวนโดเมน distinct (0 เมื่อไม่มีความหมายในบริบทนั้น)
    Bytes, BytesUp, BytesDown uint64
    BytesPercent float64 `json:"bytesPercent"`
}
```
- `DNSQueryStatistics`: `TopDomains []DNSDomainStat` (แทน `[]TopDomain`), เพิ่ม `ObservedBytes`,
  `DomainBytes` (ผลรวม byte ที่ระบุโดเมนได้), `TotalDomains`, `TotalClients`, `Accuracy`
- `DNSDomainDrilldown`: เพิ่ม `IPs []DNSDomainIP`, `TotalBytes/Up/Down`, `SharedIPs`, `IPsTruncated`,
  `Series []BandwidthPoint`, `Accuracy`, `ObservedBytes`
- `DNSClientDrilldown`: `Domains []DNSDomainStat`, เพิ่ม `TotalBytes/Up/Down`, `Series`, `Accuracy`

### 2.3 Series
- domain drill-down: `Series` = ผลรวม `dstBytes` ของ IP set ต่อ bucket (T-03) — flow-relative
  (Orig=up, Reply=down) เหมือน `TrafficHostDetail.Series` ไม่ใช่ LAN-relative; ความยาวคงที่เท่าจำนวน
  bucket ของ window (3/6/12/36/72/144/288) และต้อง **zero-filled ไม่ใช่ nil** เมื่อไม่มีข้อมูล
  invariant: `sum(Series[].Bytes) == TotalBytes`
- client drill-down: ใช้ `GetTrafficBreakdownForIP(window, client).HostSeries` ที่มีอยู่แล้ว ห้ามเขียนใหม่

---

## 3. Tasks

```json
[
  {
    "task_id": "T-01",
    "title": "DTO: DNSDomainStat / DNSDomainIP + ขยาย DNSClientStat และ 3 response ของ DNS statistics",
    "layer": "model",
    "files": ["backend/internal/model/statistics.go"],
    "instruction": "เพิ่ม struct DNSDomainStat (embed model.TopDomain + Clients/IPCount/SharedIPs/Bytes/BytesUp/BytesDown/BytesPercent) และ DNSDomainIP ตามสเปกใน docs/ref/todo/statistics-dns-page-revamp-plan.md §2.2; เพิ่ม field Domains/Bytes/BytesUp/BytesDown/BytesPercent ลงใน DNSClientStat; เปลี่ยน DNSQueryStatistics.TopDomains เป็น []DNSDomainStat และ DNSClientDrilldown.Domains เป็น []DNSDomainStat; เพิ่ม field ใหม่ให้ DNSQueryStatistics (ObservedBytes, DomainBytes, TotalDomains, TotalClients, Accuracy), DNSDomainDrilldown (IPs, TotalBytes/Up/Down, SharedIPs, IPsTruncated, Series, Accuracy, ObservedBytes) และ DNSClientDrilldown (TotalBytes/Up/Down, Series, Accuracy). ห้ามแตะ struct TopDomain, TopHost, TopConversation, TrafficStatistics เด็ดขาด (ต้องคง JSON ของ /api/statistics/traffic ไว้ byte-for-byte). เขียน comment กำกับทุก field ใหม่ตามสไตล์ไฟล์นี้ โดยเฉพาะ (ก) bytes เป็นค่าประมาณจากการ join DNS answer กับ conntrack ต่อ IP ไม่ใช่การนับต่อโดเมน (ข) ความหมายของ DNSClientStat.Bytes/Domains ขึ้นกับ DTO แม่ (overview = ยอดรวมของ client ทั้งหมด, domain drill-down = เฉพาะ byte ที่คุยกับ IP ของโดเมนนั้น) (ค) percent = % ของ query count ส่วน bytesPercent = % ของ byte (ง) ห้ามนำ mapping domain->IP ไปใช้กับ firewall/policy/routing/QoS. ยังไม่ต้องแก้ service/api ใน task นี้ (จะคอมไพล์ไม่ผ่านชั่วคราวได้ถ้าจำเป็น แต่ให้พยายามให้ package model คอมไพล์ผ่านเดี่ยว ๆ)",
    "acceptance": ["`cd backend && go build ./internal/model/...` ผ่าน", "struct TopDomain/TrafficStatistics ไม่มีการเปลี่ยนแปลง (git diff ยืนยันได้)", "ทุก field ใหม่มี json tag แบบ lowerCamelCase และมี comment กำกับ"],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "Service: forward index domain -> resolved IPs (RAM-only, capped, TTL)",
    "layer": "service",
    "files": ["backend/internal/service/dns_domain_ips.go", "backend/internal/service/dns_query_stats.go"],
    "instruction": "สร้างไฟล์ใหม่ service/dns_domain_ips.go ตามสเปก §2.1 ของแผน: struct dnsDomainIPs { mu, byDomain map[string]map[string]time.Time, ipRefs map[string]int, ttl, maxDomains, maxIPsPerDomain } พร้อม newDNSDomainIPs(), Put(domain, ip), IPsFor(domain) (คืน slice เรียงตาม IP asc พร้อม lastSeen + shared), Snapshot() (คืน map ip->domain สำหรับ invert; ชน IP ให้เลือก lastSeen ใหม่สุด), Clear(), SetLimits(ttlMinutes, maxDomains, maxIPsPerDomain), sweepExpired(), Truncated() bool. กติกาบังคับ: Put ต้อง O(1) ไม่มี I/O ไม่ log ชื่อโดเมน (รันบน read-loop ของ WatchDNSLog); โดเมนใหม่ถูกปฏิเสธเมื่อถึง maxDomains, IP ใหม่ถูกปฏิเสธเมื่อถึง maxIPsPerDomain แต่ IP เดิม refresh lastSeen ได้เสมอ; entry เกิน TTL ถูก evict แบบ lazy ตอนอ่าน + sweepExpired; ipRefs ต้องถูกลด/ลบให้ถูกต้องทุกครั้งที่ evict ไม่งั้น flag shared จะเพี้ยน; ห้ามถือ lock นี้พร้อมกับ s.dns.mu หรือ lock ของ dnsReverseCache (snapshot ก่อนแล้วค่อย join). จากนั้นใน dns_query_stats.go: ฝัง *dnsDomainIPs ลงใน struct dnsQueryStats, สร้างใน NewStatisticsService, เรียก Put ใน RecordDNSEvent สาขา model.DNSLogAnswer (คู่กับ reverseCache.Put เดิม), ล้างใน ClearDNSStats(), และต่อ SetReverseCacheLimits ให้ส่ง ttl เดียวกันเข้า SetLimits ของ index ใหม่ด้วย. เขียน unit test ครอบ cap/TTL/shared-flag/clear",
    "acceptance": ["`go build ./...` และ `go test ./internal/service/... -run DomainIP` ผ่าน", "RecordDNSEvent ยังคงไม่มี I/O และไม่ log ชื่อโดเมน", "ปิด query logging แล้ว index ถูกล้างจริง (มี test)", "ไม่มี goroutine/ticker ใหม่ถูกสร้าง"],
    "depends_on": []
  },
  {
    "task_id": "T-03",
    "title": "Service: TrafficStatsService.GetTrafficBreakdownForDests(window, ips) สำหรับ series ของโดเมน",
    "layer": "service",
    "files": ["backend/internal/service/traffic_stats.go"],
    "instruction": "เพิ่ม method GetTrafficBreakdownForDests(window string, dstIPs []string) TrafficBreakdown โดย refactor getTrafficBreakdown ให้รับ dstSet map[string]struct{} เพิ่มอีกหนึ่งพารามิเตอร์ (nil = เหมือนเดิมทุกประการ ไม่มี cost เพิ่มสำหรับ caller เดิม 3 ราย). เมื่อ dstSet ไม่ว่าง ให้คำนวณ DestSeries []model.BandwidthPoint จาก b.dstBytes ของทุก bucket ใน RLock/loop เดียวกับ Series/Observed (ห้าม lock หรือ scan รอบสอง) โดยใช้ idx เดียวกันกับ Series และเก็บผลรวม DestTotals dirBytes; ทิศทางเป็น flow-relative (Orig=up=client->server, Reply=down) เหมือน HostSeries ไม่ใช่ LAN-relative; array ต้องยาวคงที่เท่าจำนวน bucket ของ window และ zero-filled แม้ไม่มีข้อมูล (ห้ามคืน nil). เพิ่ม field DestSeries []model.BandwidthPoint และ DestTotals dirBytes ลงใน TrafficBreakdown พร้อม comment ระบุ invariant sum(DestSeries[].Bytes) == DestTotals.Total(). ห้ามเปลี่ยนพฤติกรรมของ GetTrafficBreakdown/GetTrafficBreakdownForIP (test เดิมต้องผ่านหมดโดยไม่แก้)",
    "acceptance": ["`go test ./internal/service/...` ผ่านทั้งหมดโดยไม่ต้องแก้ test เดิม", "มี test ใหม่ยืนยัน sum(DestSeries)==DestTotals และความยาว series ตรงตาม window", "ไม่มี lock/scan เพิ่มรอบสองใน getTrafficBreakdown"],
    "depends_on": []
  },
  {
    "task_id": "T-04",
    "title": "Service: compose 3 response ของ DNS statistics ใหม่ (join query ring × domainIPs × traffic)",
    "layer": "service",
    "files": ["backend/internal/service/statistics_dns.go", "backend/internal/service/dns_query_stats.go"],
    "instruction": "ย้าย/เขียน GetDNSQueryStatistics, GetDNSDomainClients, GetDNSClientDomains มาไว้ในไฟล์ใหม่ statistics_dns.go (dns_query_stats.go เหลือเฉพาะ ring + hot path + helper rankTopDomains/rankDNSClients) แล้วขยายตามสเปก §2.2: (1) GetDNSQueryStatistics — snapshot ring เดิม (โครงลูป bucket/ truncated/ percent เดิมห้ามเปลี่ยนความหมาย) + นับ distinct client ต่อโดเมนและ distinct domain ต่อ client + เรียก s.traffic.GetTrafficBreakdown(window) หนึ่งครั้ง แล้ว join: bytes ของโดเมน = ผลรวม breakdown.Dests[ip] ของทุก IP ที่ domainIPs.IPsFor(domain) คืน, bytes ของ client = breakdown.Hosts[clientIP]; ตั้ง ObservedBytes=breakdown.Observed, DomainBytes=ผลรวม byte ที่ระบุโดเมนได้, Accuracy=breakdown.Accuracy; bytesPercent ของโดเมนใช้ denominator = DomainBytes, ของ client ใช้ ObservedBytes (เขียน comment กำกับ denominator ทั้งสองให้ชัด). (2) GetDNSDomainClients — คงลิสต์ clients เดิมไว้ แต่เพิ่ม IPs []DNSDomainIP โดยดึง IP จาก domainIPs.IPsFor(domain) แล้วเอา byte ต่อ IP จาก breakdown.Dests เรียง bytes desc / ip asc, ตั้ง Shared จาก index, TotalBytes/Up/Down = ผลรวมของ IPs, Series = s.traffic.GetTrafficBreakdownForDests(window, ips).DestSeries; ส่วน bytes ต่อ client ในตาราง clients ให้คำนวณจาก breakdown.Convs (src==client และ dst อยู่ใน IP set ของโดเมนนี้) ตาม §1.2 โดยใช้ parseConvKey ที่มีอยู่แล้วและข้าม key ที่ malformed; ถ้า IP set ว่างให้ข้ามการ scan Convs ทั้งหมด (fast path). (3) GetDNSClientDomains — Domains กลายเป็น []DNSDomainStat โดย count/percent มาจาก ring เดิม ส่วน bytes ต่อโดเมนคำนวณจาก breakdown.Convs ที่ src==client แล้ว map dst->domain ผ่าน domainIPs.Snapshot() (สแกน Convs รอบเดียวเท่านั้น), Series = s.traffic.GetTrafficBreakdownForIP(window, client).HostSeries, TotalBytes/Up/Down จาก breakdown เดียวกัน. กติกาบังคับ: เรียก GetTrafficBreakdown*/hostLookup อย่างละครั้งเดียวต่อ request; ไม่มี lock ซ้อน (snapshot ring -> ปล่อย lock -> ค่อย join); เมื่อ enabled=false ต้องคืน empty slice (ไม่ใช่ nil) และไม่แตะ traffic breakdown เลย; ทุก slice ที่ตอบกลับต้องไม่เป็น nil; truncated ต้องรวม flag จาก domainIPs ด้วย (field IPsTruncated). เขียน/ปรับ unit test ให้ครอบ join, denominator, shared IP, empty/disabled",
    "acceptance": ["`go build ./... && go test ./internal/service/...` ผ่าน", "response ของ /api/statistics/traffic (GetStatistics) ไม่เปลี่ยนรูปเลย — test เดิมของ statistics_test.go ผ่านโดยไม่แก้", "มี test ยืนยันว่าเมื่อ query logging ปิด ทุก endpoint คืน enabled=false + slice ว่าง และไม่เรียก traffic breakdown", "ไม่มีการเรียก GetTrafficBreakdown ซ้ำมากกว่าหนึ่งครั้งต่อ request"],
    "depends_on": ["T-01", "T-02", "T-03"]
  },
  {
    "task_id": "T-05",
    "title": "Config: dns-stats-max-domains / dns-stats-max-ips-per-domain (file-only keys)",
    "layer": "service",
    "files": ["backend/internal/config/config.go", "backend/internal/config/config_test.go", "backend/cmd/pigate/main.go"],
    "instruction": "เพิ่ม config key แบบ file-only สองตัวตามแพตเทิร์นเดิมของ dns-stats-max-pairs/dns-stats-max-clients เป๊ะ ๆ: dns-stats-max-domains (default 1000, ช่วงที่ยอมรับ 100..20000) และ dns-stats-max-ips-per-domain (default 16, ช่วง 2..64) — ค่านอกช่วงหรือ <=0 ตกกลับ default (ไม่ error), ค่าที่ parse ไม่ได้เป็น hard error เหมือนเดิม, ต้องอยู่ใน KnownKeys และถูกเขียนโดย config.Write. ส่งค่าลง NewStatisticsService (หรือ setter ที่มีอยู่) ใน cmd/pigate/main.go ให้ dnsDomainIPs ใช้ พร้อม comment ระบุ worst-case RAM = maxDomains x maxIPsPerDomain entries (ไม่คูณจำนวน bucket เพราะ index นี้ไม่ใช่ ring). อัปเดต config_test.go ให้ครอบ default/override/out-of-range/KnownKeys เหมือน 2 key เดิม",
    "acceptance": ["`go test ./internal/config/...` ผ่าน", "รัน `./pigate-backend -mock=true` แล้วไฟล์ config ที่ถูกสร้างมี 2 key ใหม่", "ค่านอกช่วงตกกลับ default โดยไม่ทำให้ boot ล้ม"],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-06",
    "title": "Kernel mock: ข้อมูล DNS answer จำลองให้ครอบเคส multi-IP และ shared IP",
    "layer": "kernel",
    "files": ["backend/internal/kernel/mock.go"],
    "instruction": "ขยาย mockDNSAnswerEvents ให้ (ก) www.youtube.com และ googlevideo.com มีหลาย IP ต่อโดเมน (อย่างน้อย 3 และ 2 ตามลำดับ) โดยเลือก IP ให้ตรงกับ dstIP ใน mockFlowTemplates อย่างน้อยส่วนหนึ่ง เพื่อให้ตาราง volume ใน -mock=true มีตัวเลขจริง (ข) มี IP อย่างน้อย 1 ตัวที่ถูกใช้ร่วมกันสองโดเมน เพื่อ exercise flag shared (ค) คงโดเมนที่ไม่มี answer ไว้อย่างน้อย 1 ตัว (line-apps.com) เพื่อ exercise เคส 'ไม่รู้จัก IP'. ปรับลูปส่ง answer ใน WatchDNSLog ให้ส่งครบทุก entry ตามรอบ ห้ามแตะ interfaces.go และห้ามเพิ่ม method ใหม่ (ไม่มี real_*.go ที่ต้องแก้ตาม) และห้ามให้ mock แตะ filesystem",
    "acceptance": ["`go build ./...` ผ่าน", "รัน -mock=true แล้ว /api/statistics/dns/domain?domain=www.youtube.com คืน ips หลายแถวและมีอย่างน้อยหนึ่งแถว shared=true", "mock ยังไม่แตะ filesystem"],
    "depends_on": []
  },
  {
    "task_id": "T-07",
    "title": "API + OpenAPI: อัปเดต handler comment และสัญญา API ของ 3 endpoint DNS",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go", "docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "ไม่เพิ่ม route ใหม่และไม่เปลี่ยน validation ใด ๆ (window whitelist, NormalizeQueryDomain, netip.ParseAddr + reserved 'unknown', clampQueryLimit ยังคงเดิมทุกตัว) — งานคือ (1) อัปเดต comment ของ HandleGetDNSQueryStatistics/HandleGetDNSDomainClients/HandleGetDNSClientDomains ให้สะท้อน field ใหม่และย้ำว่ายังไม่มี input ใหม่จาก client (โดยเฉพาะ: ไม่มี sort key จาก client, sort ทำฝั่ง browser) (2) อัปเดต schema ใน docs/openapi.yaml และ frontend/public/openapi.yaml ให้ตรงกับ DTO ใหม่ทุก field พร้อมคำอธิบายว่า bytes เป็นค่าประมาณ และ mapping domain->IP ห้ามใช้ตัดสินใจด้าน security (3) ถ้าจะเพิ่ม limit ให้ /dns/domain (จำนวนแถว IP) ให้ใช้ clampQueryLimit เดิมเท่านั้น อย่าเขียน parser ใหม่. ตรวจว่าไฟล์ openapi ทั้งสองชุดตรงกัน",
    "acceptance": ["`go build ./...` ผ่าน", "openapi.yaml สองไฟล์ตรงกันและ parse ได้ (หน้า ApiDocs render ไม่ error)", "ไม่มี route ใหม่และไม่มี query param ใหม่ที่ไม่ผ่าน validator เดิม"],
    "depends_on": ["T-04"]
  },
  {
    "task_id": "T-08",
    "title": "Frontend service: types + mock ของ dnsStatisticsService",
    "layer": "frontend",
    "files": ["frontend/src/services/dnsStatisticsService.ts"],
    "instruction": "อัปเดต interface ให้ตรงกับ DTO ใหม่ field-for-field (DNSDomainStat, DNSDomainIP, DNSClientStat ที่มี domains/bytes/bytesUp/bytesDown/bytesPercent, และ field ใหม่ของทั้ง 3 response รวม series: BandwidthPoint[] — import type BandwidthPoint จาก trafficStatisticsService เพื่อไม่ประกาศซ้ำ). ขยาย mock data ให้สอดคล้องกับ kernel/mock.go ที่ T-06 แก้ (โดเมนเดียวกัน, IP ชุดเดียวกัน, มี shared IP, มีโดเมนที่ไม่มี IP) และสังเคราะห์ bytes/series แบบ deterministic ตาม mockWindowScale เหมือนที่ทำกับ count อยู่แล้ว. ห้ามเปลี่ยน signature ของ 3 ฟังก์ชันเดิมและห้ามแตะ statisticsService.ts (การ์ด Top Queried Domains บนหน้า Overview ต้องไม่พัง)",
    "acceptance": ["`yarn build` ผ่าน (tsc -b ไม่มี error)", "mock mode แสดงข้อมูลครบทุกคอลัมน์ใหม่", "TopDomain type ที่ re-export ไว้ยังคงใช้ได้จากหน้า Overview"],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-09",
    "title": "Frontend shared: ตาราง DNS ที่ sort/filter ได้ + คอลัมน์ Volume + ตาราง Resolved IPs",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/DnsStatsShared.tsx"],
    "instruction": "ปรับ DomainStatsTable และ ClientStatsTable ให้ (ก) ใช้ SortableHead + useSortableRows + useTextFilter + TrafficFilterInput จาก components/statistics/TrafficStatsShared.tsx (ห้ามเขียนกลไก sort/filter ใหม่) โดย **ทุกคอลัมน์ sort ได้** (ข) เพิ่มคอลัมน์ Down / Up / Total (fmtBytes จาก lib/formatBytes) และ % Vol พร้อมป้ายชื่อที่แยกจาก % Query ให้ชัด (ค) DomainStatsTable เพิ่มคอลัมน์ Clients และ IPs (จำนวน) พร้อมไอคอนเตือนเมื่อ sharedIps=true (ง) ClientStatsTable เพิ่มคอลัมน์ Domains. เพิ่ม component ใหม่ DomainIpTable (แถว = DNSDomainIP, เรียงตาม bytes มาก->น้อยเป็นค่าเริ่มต้น, sort ได้ทุกคอลัมน์, badge 'shared' เมื่อ shared=true, แสดง lastSeen แบบเวลาไทย) และ DnsVolumeInfoButton (Popover อธิบายข้อจำกัดตาม §1.1 ของแผน: IP ที่ใช้ร่วมกันถูกนับซ้ำ, traffic ที่ไม่ผ่าน DNS ไม่ถูกนับ, mapping มี TTL). สไตล์ต้องตามกติกา docs/rules_of_work.md: ใช้เฉพาะ components/ui/*, ห้าม hardcode สีแบรนด์/สถานะ (ใช้ text-primary/text-warning/... จาก theme), ห้าม shadow-*/backdrop-blur-*, รองรับ dark/light",
    "acceptance": ["`yarn build` และ `yarn lint` ผ่าน", "ทุก header ของทั้ง 3 ตารางกดเรียงได้ทั้ง asc/desc", "ไม่มี class สีดิบ เช่น text-emerald-500 และไม่มี shadow-*/backdrop-blur-*"],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-10",
    "title": "Frontend: หน้า Statistics > DNS (overview) แบบเดียวกับหน้า Traffic",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsDns.tsx"],
    "instruction": "ปรับหน้า overview ให้มีโครงเดียวกับ pages/StatisticsTraffic.tsx: (ก) แถวการ์ดสถิติด้านบนด้วย TrafficStatCard — Total Queries (window), Domains/Clients ที่พบ, Volume ที่ระบุโดเมนได้ (พร้อม breakdown Down/Up) (ข) ปุ่ม DnsVolumeInfoButton ข้าง StatsWindowTabs (ค) ตาราง Top Domains และ Top Source Hosts ใช้ component ที่ปรับแล้วจาก T-09 พร้อมช่องค้นหาแต่ละตาราง (ง) คงเงื่อนไข empty-state เดิมเมื่อ enabled=false (ลิงก์ไปหน้า DNS Server > Settings) และ DnsStatsTruncatedWarning/DnsStatsPrivacyNote ไว้ครบ (จ) คง auto-refresh 10 วินาที + การกลืน error ของ background refresh ไว้เหมือนเดิม (ฉ) row click ยังไปหน้า drill-down เดิมพร้อม ?window=. ห้ามเปลี่ยน route/path และห้ามแตะหน้า StatisticsOverview.tsx",
    "acceptance": ["`yarn build` ผ่าน", "-mock=true: ตารางแสดง Volume/Clients/IPs และเรียงได้ทุกคอลัมน์", "ปิด query logging แล้วยังเห็น empty-state เดิมทุกประการ"],
    "depends_on": ["T-09"]
  },
  {
    "task_id": "T-11",
    "title": "Frontend: หน้า drill-down ของโดเมน (IP + volume + time series)",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsDnsDomain.tsx"],
    "instruction": "ขยายหน้า drill-down ของโดเมนให้ครบตามโครงของ pages/StatisticsTrafficHost.tsx: (ก) header คงปุ่มกลับ + ชื่อโดเมน + StatsWindowTabs + Refresh เดิม เพิ่ม DnsVolumeInfoButton (ข) แถวการ์ดสถิติ: Total Queries, Clients, Resolved IPs, Volume (Down/Up) (ค) TrafficTrendCard ด้วย data.series (ใช้ component เดิม ห้ามเขียนกราฟใหม่) (ง) ตารางใหม่ 'IP ที่ได้จากการ resolve' ด้วย DomainIpTable เรียงตาม volume มาก->น้อยเป็นค่าเริ่มต้น (จ) ตาราง clients เดิมแต่มีคอลัมน์ volume ตาม T-09 (ฉ) empty-state แยกกรณี 'ไม่มี IP ที่รู้จัก' กับ 'ไม่มี traffic' ให้ผู้ใช้เข้าใจว่าไม่ใช่ error (ช) คง logic isStale/isNewTarget ของ load() เดิมไว้ทั้งหมด (กัน race ตอนสลับโดเมน) และคง auto-refresh 10 วินาที",
    "acceptance": ["`yarn build` ผ่าน", "คลิกโดเมนจากหน้า overview แล้วเห็นตาราง IP เรียงตาม volume และกราฟ series", "สลับโดเมน/หน้าต่างเวลาเร็ว ๆ แล้วไม่มีข้อมูลของเป้าหมายเก่าค้าง"],
    "depends_on": ["T-09"]
  },
  {
    "task_id": "T-12",
    "title": "Frontend: หน้า drill-down ของ client (โดเมนที่ถาม + volume + series)",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsDnsClient.tsx"],
    "instruction": "ขยายหน้า drill-down ของ client ให้สมมาตรกับ T-11: การ์ดสถิติ (Total Queries, Domains, Volume Down/Up), TrafficTrendCard ด้วย data.series, ตารางโดเมนใช้ DomainStatsTable ที่ปรับแล้ว (มี volume + sort ทุกคอลัมน์ + ช่องค้นหา), รองรับ client = 'unknown' เหมือนเดิม (แสดง 'ไม่ทราบต้นทาง' และซ่อนการ์ด volume เพราะไม่มี IP ให้ join — ห้ามแสดงเลข 0 ที่ทำให้เข้าใจผิด), เพิ่ม DnsVolumeInfoButton, คง logic isStale/auto-refresh/ปุ่มกลับเดิมไว้ครบ",
    "acceptance": ["`yarn build` ผ่าน", "client ปกติเห็น volume+series, client 'unknown' ไม่แสดงการ์ด volume และไม่ error", "ทุกคอลัมน์ในตารางโดเมนเรียงได้"],
    "depends_on": ["T-09"]
  },
  {
    "task_id": "T-13",
    "title": "เอกสาร: อัปเดต README feature status + หมายเหตุ config ใหม่",
    "layer": "db",
    "files": ["README.md", "docs/ref/todo/statistics-dns-page-revamp-plan.md"],
    "instruction": "อัปเดตตาราง Feature Status ใน README ให้สะท้อนว่า Statistics > DNS มี volume/drill-down แล้ว, ระบุ config key ใหม่ 2 ตัวและค่าเริ่มต้น/worst-case RAM ไว้ในที่เดียวกับที่อธิบาย dns-stats-max-pairs, และทำเครื่องหมายว่าแผนนี้เสร็จแล้ว (ย้ายไฟล์แผนไป docs/ref/complete/ เมื่อ QA ผ่านครบ). งานนี้เป็น docs-only จึง push ขึ้น main ได้โดยตรงตามกติกาโปรเจกต์ แต่ให้ทำหลังสุด",
    "acceptance": ["README สะท้อนสถานะจริง", "config key ใหม่ถูกบันทึกไว้พร้อมค่าเริ่มต้น"],
    "depends_on": ["T-10", "T-11", "T-12", "T-07"]
  }
]
```

### ลำดับการทำงาน
- **ขนานได้ทันที (ไม่มี dependency):** T-01, T-02, T-03, T-06
- **หลัง backend core:** T-04 (ต้องรอ T-01/02/03) → T-05 (รอ T-02) → T-07 (รอ T-04)
- **Frontend:** T-08 (รอ T-01) → T-09 → T-10/T-11/T-12 (สามตัวนี้ขนานกันได้)
- **ปิดท้าย:** T-13

---

## 4. ความเสี่ยง / จุดที่ต้องระวัง

1. **SD card wear / persistence** — ห้ามมี migration หรือ table ใหม่ใน SQLite แม้แต่ column เดียว index
   domain→IP และทุกตัวเลขในแผนนี้เป็น RAM-only ทั้งหมด (tech_stack_design §8) restart แล้วเริ่มนับใหม่
   ถือเป็นพฤติกรรมที่ถูกต้อง
2. **RAM บน Pi** — index ใหม่ worst case ≈ maxDomains(1000) × maxIPsPerDomain(16) ≈ 16k entry (~1 MB)
   ไม่ใช่ ring จึงไม่คูณ 288 bucket; ต้องมี cap ทั้งสองชั้น + TTL + flag truncated ครบ ไม่งั้น LAN client
   ที่ยิง query ชื่อขยะรัว ๆ ทำให้หน่วยความจำโตไม่จำกัด
3. **Hot path** — `RecordDNSEvent` รันบน read-loop ของ log watcher: การเพิ่ม `Put` ต้อง O(1) ไม่มี I/O
   ไม่มี allocation หนัก ไม่ log ชื่อโดเมน และ **ห้ามถือ lock ซ้อน** (index lock ↔ ring lock ↔ reverse
   cache lock) — deadlock ที่นี่จะทำให้ทั้ง watcher ตาย
4. **ต้นทุนต่อ request ของการ scan `Convs`** — worst case = maxTrackedConversations (600) × bucket ของ
   window (สูงสุด 288) ต่อคำขอ และหน้าเว็บ refresh ทุก 10 วินาที ต้อง: scan รอบเดียว, ใช้ fast-path
   `strings.HasPrefix/Contains` แบบเดียวกับ `getTrafficBreakdown`, และ **ข้ามทั้งก้อนเมื่อโดเมนไม่มี IP
   ที่รู้จัก**; ถ้าวัดแล้วช้าให้ลดค่า default ของ limit ก่อน ไม่ใช่เพิ่ม cache ใหม่
5. **ความถูกต้องของตัวเลข volume** — เป็นค่าประมาณโดยธรรมชาติ (§1.1) ต้องมี disclaimer ทั้งใน DTO comment
   และใน UI; ห้ามนำเสนอเป็น "ปริมาณข้อมูลของโดเมน" แบบชี้ขาด ให้ใช้ถ้อยคำเช่น "ประมาณจาก IP ที่โดเมนนี้
   ถูก resolve"
6. **Privacy (sensitive)** — ข้อมูลนี้คือพฤติกรรมการใช้เน็ตรายบุคคล: ต้องยังคง opt-in ผ่าน
   `DNSServerSettings.QueryLogging`, ปิดแล้วล้าง index ใหม่ด้วย, endpoint ยังเป็น `authRoute` เท่านั้น,
   และห้าม log ชื่อโดเมน/IP ของ client ลง event log
7. **Security-sensitive review** — งานที่ต้อง review เข้มเป็นพิเศษ: T-02 (โครงสร้างข้อมูลที่รับ input จาก
   DNS ภายนอกโดยตรง — poison ได้), T-04 (join + denominator + การไม่ทำ lock ซ้อน), T-07 (สัญญา API/
   validation). ย้ำ: ห้าม `exec.Command`, ห้ามแตะ `interfaces.go`, ห้ามมี kernel capability ใหม่
8. **Backward compatibility** — `/api/statistics/traffic` และการ์ด Top Queried Domains บนหน้า Overview
   ต้องไม่เปลี่ยนเลย (test เดิมต้องผ่านโดยไม่ถูกแก้) นี่คือ regression guard ที่สำคัญที่สุดของแผนนี้
9. **`unknown` client bucket** — ยังต้องรองรับทุกจุด และห้ามพยายาม join volume ให้มัน (ไม่มี IP)
10. **git** — ทุกงานโค้ดทำบน branch `feat/statistics-dns-page` แล้วเข้า PR เท่านั้น (มีแต่ T-13 ที่เป็น
    docs-only ที่ push main ได้) ห้าม commit เว้นแต่เจ้าของสั่ง

---

## 5. Final Acceptance (ทดสอบรอบเดียวหลังทุก Task เสร็จครบ)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... && go test ./... ผ่านทั้งหมด (รวม go test -race ./internal/service/...)",
    "cd frontend && yarn build && yarn lint ผ่าน ไม่มี error/warning ใหม่",
    "bash build.sh สร้าง ./pigate ได้สำเร็จ",
    "GET /api/statistics/traffic (window ใด ๆ) คืน JSON รูปเดิมทุก field — เทียบ diff กับ response ก่อนแก้แล้วเหมือนเดิม byte-for-byte",
    "หน้า Overview การ์ด Top Queried Domains ยังแสดงผลเหมือนเดิมทุกประการ",
    "-mock=true: GET /api/statistics/dns คืน topDomains ที่มี count, clients, ipCount, bytes, bytesUp, bytesDown, bytesPercent ครบ และ topClients มี domains + bytes",
    "-mock=true: GET /api/statistics/dns/domain?domain=www.youtube.com คืน ips[] มากกว่า 1 แถว เรียงจาก bytes มากไปน้อย มีอย่างน้อย 1 แถว shared=true และ series ยาวเท่าจำนวน bucket ของ window",
    "invariant: sum(series[].bytes) == totalBytes ในทั้ง domain drill-down และ client drill-down",
    "GET /api/statistics/dns/client?client=unknown คืน 200 พร้อม domains[] และไม่มีการพยายาม join volume",
    "GET /api/statistics/dns/domain?domain=<ชื่อไม่ถูกต้อง เช่น '../etc' หรือสตริงยาว 300 ตัว> คืน 400 และไม่ echo ค่าที่ส่งมากลับใน body",
    "GET /api/statistics/dns/client?client=not-an-ip คืน 400; client=2001:DB8::1 (ตัวใหญ่) ให้ผลเดียวกับ 2001:db8::1",
    "ทุก endpoint ของ DNS statistics ยังต้องผ่าน auth (เรียกโดยไม่มี session แล้วได้ 401)",
    "ปิดสวิตช์ 'เก็บสถิติ DNS query' แล้ว: ทั้ง 3 endpoint คืน enabled=false + slice ว่าง, หน้าเว็บแสดง empty-state เดิม, และเปิดใหม่แล้วเริ่มนับใหม่จากศูนย์",
    "UI หน้า Statistics > DNS: ทุกคอลัมน์ของทั้งตาราง Top Domains และ Top Source Hosts กดเรียงได้ทั้ง asc/desc และมีคอลัมน์ Down/Up/Total",
    "UI drill-down โดเมน: มีการ์ดสถิติ, กราฟ series, ตาราง IP เรียงตาม volume, ตาราง clients ที่มี volume — คลิกข้ามไป client drill-down แล้วกลับได้โดย window ไม่หาย",
    "UI: ปุ่ม info อธิบายข้อจำกัดของตัวเลข volume ปรากฏทั้งหน้า overview และ drill-down",
    "UI: ทดสอบทั้ง dark และ light mode, ไม่มี hardcoded brand/status color, ไม่มี shadow-*/backdrop-blur-*",
    "grep ยืนยัน: ไม่มี exec.Command ใหม่, internal/kernel/interfaces.go ไม่ถูกแก้, ไม่มี migration/table ใหม่ใน internal/db",
    "รันโหมด real บนเครื่องจริง (หรือ -mock-from-real) แล้วเปิดหน้า DNS statistics ค้างไว้ 10 นาที: ไม่มี panic/deadlock, RSS ของ process ไม่โตผิดปกติ, log ไม่มีชื่อโดเมนของผู้ใช้หลุดออกมา"
  ]
}
```
