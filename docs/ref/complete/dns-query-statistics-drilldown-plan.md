# DNS Query Statistics (แท็บใหม่ในหน้า DNS Server) — 2 ตารางหลัก + Drill-down 2 ทาง

> เอกสารแผนงานสำหรับฟีเจอร์: เพิ่มแท็บ **"สถิติ (Statistics)"** ในหน้า DNS Server แสดง
> (1) ตารางโดเมนที่ถูกค้นหามากที่สุด (2) ตาราง Source Host ที่ค้นหามากที่สุด
> และ **drill-down 2 ทาง** — คลิกโดเมน → เห็นว่าเครื่องไหนถามบ้าง / คลิกเครื่อง → เห็นว่าถามโดเมนอะไรบ้าง
> ต่อยอดจากท่อ DNS query log ที่ทำเสร็จแล้วใน PR #109 (`statistics-dns-top-domain-plan.md`)
> ซึ่ง **ตัด per-client drill-down ออกจากขอบเขตไว้ตอนนั้น** — แผนนี้คือการเปิดขอบเขตส่วนนั้นตามที่เจ้าของโปรเจกต์สั่ง
>
> วันที่เขียน: 2026-07-31 · Branch อ้างอิง: `feat/dns-blocked-domains` (เจ้าของยืนยันให้ทำต่อบน branch นี้)
> README Feature Status: "DNS Server" / "Statistics" ยัง Completed เหมือนเดิม (เพิ่มแท็บในหน้าเดิม)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็น)**
- แท็บใหม่ "สถิติ" ในหน้า DNS Server (`/dns-server`) มีตัวเลือก window `1h | 24h` เหมือนหน้า Statistics
- **ตาราง A — Domain Query Stats**: โดเมน / ชนิด query / จำนวนครั้ง / % เรียงมาก→น้อย
- **ตาราง B — Source Hosts**: IP + hostname (จาก DHCP lease/reservation) / จำนวนครั้ง / % เรียงมาก→น้อย
- คลิกแถวในตาราง A → Dialog แสดง **รายชื่อ Source Host ที่ถามโดเมนนั้น + จำนวนครั้ง**
- คลิกแถวในตาราง B → Dialog แสดง **รายชื่อโดเมนที่เครื่องนั้นถาม + จำนวนครั้ง**
- ทั้งหมดยังอยู่ใต้สวิตช์ opt-in เดิม (`DNSServerSettings.QueryLogging`) — ปิดอยู่ = แท็บแสดง empty state
  พร้อมชี้ทางไปเปิดที่แท็บ Settings; ปิดสวิตช์ = ข้อมูลถูกล้างทันที (พฤติกรรมเดิมของ `ClearDNSStats`)
- ทำงานได้ทั้ง `-mock=true` (ข้อมูลสังเคราะห์) และของจริง

**นอกขอบเขต (ตัดชัดเจน)**
- **ไม่ persist สถิติลง SQLite** — reboot/restart pigate แล้วสถิติเริ่มนับใหม่ (เหตุผลใน §2.2)
- **ไม่ทำกราฟ timeline / export CSV / ค้นหาข้ามหน้า** — ตารางอันดับ + drill-down เท่านั้น
- **ไม่แตะ kernel layer เลย** — `DNSServerManager.WatchDNSLog` ส่ง `ClientIP` มาให้อยู่แล้ว
  (`model.DNSLogEvent.ClientIP`, parser ทำ `netip.ParseAddr` ให้แล้วที่ `kernel/dns_query_log.go`)
  แก้เฉพาะ **ข้อมูลสังเคราะห์ใน `mock.go`** ให้หลากหลายพอจะเห็น drill-down
- **ไม่เปลี่ยนพฤติกรรมการ์ดเดิมในหน้า `/logs/statistics`** — "Top Queried Domains",
  "Top Destinations", "Top Conversations" ต้องได้ผลลัพธ์เท่าเดิม
- **ไม่นำโดเมน/คู่ (โดเมน,เครื่อง) ไปสร้าง rule/policy ใด ๆ** — display-only เหมือนเดิม
  (`statistics-dns-top-domain-plan.md` §5 ข้อ 6)
- ไม่แตะ dnsmasq config, ไม่แตะ `install.sh`, ไม่เพิ่ม dependency

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-07-31)

| ส่วน | สถานะ | ที่อยู่ |
|---|---|---|
| ท่ออ่าน DNS log | **เสร็จแล้ว** — `WatchDNSLog(ctx, cb)` ทั้ง real และ mock | `kernel/interfaces.go`, `kernel/real_dns_query_log.go`, `kernel/mock.go:446` |
| event ที่ส่งขึ้นมา | **มี `ClientIP` อยู่แล้ว** (sanitize/parse แล้วที่ parser) — ปัจจุบัน **ถูกทิ้ง ไม่มีใครใช้** | `model/dns_server.go` (`DNSLogEvent`), `service/dns_query_stats.go:57-66` |
| ring ปัจจุบัน | bucket 5 นาที × 288, `domainCount map[string]uint64` + `typeByDomain` + `queries` — **ไม่มีมิติของ client** | `service/dns_query_stats.go:31-105` |
| cap ปัจจุบัน | `maxTrackedDomains = 500` ต่อ bucket | `service/dns_query_stats.go:28` |
| สวิตช์ opt-in | `dnsQueryStats.enabled` ตั้งจาก `SetDNSLoggingEnabled` (handler + main.go ตอนบูต) | `service/dns_query_stats.go:111-128` |
| การล้างข้อมูล | `ClearDNSStats()` ล้าง ring + reverse cache เมื่อปิดสวิตช์ | `service/dns_query_stats.go:123` |
| จุดประกอบ response เดิม | `GetStatistics(window)` เรียก `domainSnapshot` + `buildTopDomains` | `service/statistics.go:167-212, dns_query_stats.go:138-199` |
| hostname enrichment | `s.traffic.hostLookup()` → `hostnameFor(ip, leaseByIP, resByIP)` ใช้ได้ทันที | `service/statistics.go:173`, `traffic_stats.go` |
| API สถิติเดิม | `authRoute("GET /api/statistics/traffic", s.HandleGetStatistics)` (whitelist window) | `api/router.go:42`, `api/handlers.go:412-425` |
| หน้า DNS Server | `<Tabs defaultValue="zones">` มี 3 แท็บ: zones / blocked / settings | `frontend/src/pages/DnsServer.tsx:764-769` |
| pattern ตาราง + Dialog | แท็บ Blocked Domains (Table + search + Badge + Dialog) ลอกได้ทั้งชุด | `DnsServer.tsx:1027-1158` |
| frontend service | `dnsServerService` (มี mock branch ทุก method), `statisticsService` (DTO ตรงกับ Go) | `services/dnsServerService.ts`, `services/statisticsService.ts` |
| DB | `dns_server_settings` มีแค่คอลัมน์ config — **ไม่มี (และต้องไม่มี) ตารางเก็บ query** | `db/connection.go`, `db/repository.go` |

**สรุป:** งานจริงกระจุกที่ **service layer ก้อนเดียว** (เพิ่มมิติ client เข้า ring เดิม) + endpoint ใหม่ 3 เส้น
+ แท็บใหม่ฝั่ง frontend — **ไม่ต้องแตะ kernel, DB, main.go, install.sh เลย**

## 2. แนวทางเทคนิค

### 2.1 เปลี่ยน ring เดิมให้เก็บ "คู่ (domain, client)" แทนที่จะเพิ่ม ring ตัวที่สอง

```go
// service/dns_query_stats.go — โครง bucket ใหม่
type domainBucket struct {
    ts           string
    pairs        map[string]map[string]uint64 // domain -> clientIP -> count   (แหล่งความจริงเดียว)
    pairCount    int                          // จำนวน (domain,client) ที่ถูก track ใน bucket นี้
    clientCount  map[string]uint64            // ดัชนีช่วยจัดอันดับตาราง B (derive ได้ แต่เก็บไว้ให้ O(clients))
    typeByDomain map[string]string
    queries      uint64
}

const (
    maxTrackedDNSPairs   = 1200 // ต่อ bucket (แทน maxTrackedDomains = 500 เดิม)
    maxTrackedDNSClients = 200  // ต่อ bucket
)
```

- **ยอดต่อโดเมน = ผลรวมของ client ทุกตัวใต้โดเมนนั้น** → ตาราง A, การ์ด "Top Queried Domains" เดิม
  และ drill-down ใช้ **ตัวเลขชุดเดียวกัน** ผลรวมของ drill-down จึงเท่ากับยอดในตารางเสมอ
  (ถ้าแยกเป็น 2 ring ตัวเลขจะเพี้ยนกันตอนชน cap แล้วผู้ใช้จะเชื่อไม่ได้ว่าอันไหนถูก)
- **drill-down by-domain** = อ่าน `pairs[domain]` ของทุก bucket ใน window → O(clients)
- **drill-down by-client** = สแกน `pairs` ทั้ง bucket (เกิดเฉพาะตอนผู้ใช้คลิก ไม่ใช่ทุก request) —
  worst case ~345k iteration ต่อ request (1200 × 288) ระดับมิลลิวินาที ยอมรับได้
- **client ว่าง** (parser parse IP ไม่ผ่าน) → ใช้ค่าคงที่ `dnsUnknownClient = "unknown"` ไม่ทิ้ง event
  (ไม่งั้นยอดรวมของโดเมนจะหายไปเงียบ ๆ) และ endpoint drill-down ต้องรับค่านี้เป็น argument ที่ถูกต้อง
- **RAM worst case:** 1200 คู่ × 288 bucket ≈ 345k entry × ~120 B ≈ **~40 MB** (ค่าจริงในบ้าน ~200
  คู่/bucket ≈ 7 MB) — เทียบกับ deny ring ที่ shipped ไปแล้ว 800 entry/bucket × 288 จึงเป็นระดับเดียวกัน
  cap ทั้งสองตัวเป็น **ค่าคงที่ในโค้ด ไม่เปิดให้ผู้ใช้ปรับ** (ตามธรรมเนียมของ ring อื่นในไฟล์เดียวกัน)
- `dnsTruncated` เปลี่ยนความหมายเป็น "ชน cap ของคู่หรือของ client" — เขียน comment กำกับให้ชัด

**ทางเลือกที่ตัดทิ้ง**
- *เก็บ `domainCount` เดิมไว้ + เพิ่ม pair ring ใหม่* — เปลือง RAM ซ้ำซ้อน และตัวเลข 2 ที่ไม่ตรงกันตอนชน cap
- *key เดียวแบบ `"domain|client"`* — สแกน by-domain ต้องวนทั้ง map และ prefix-match เสี่ยงพลาดเมื่อโดเมนมี `|`
  (แม้ sanitizer จะกันไว้แล้วก็ไม่ควรพึ่ง)
- *ลด bucket เหลือ 15 นาที × 96 เพื่อประหยัด RAM* — จะทำให้ window "1h" ของแท็บนี้ไม่ตรงกับหน้า Statistics
  (12 bucket × 5 นาที) และเกิดคำถามว่าทำไมตัวเลขไม่ตรงกัน

### 2.2 ทำไมไม่ persist ลง SQLite

- ข้อกำหนดถนอม SD card (`tech_stack_design.md` §8): DNS query เป็น event ความถี่สูง (~50/s ตอน peak)
  การเขียนลง SQLite ทุกช่วงเวลาคือ write amplification ตรง ๆ
- ข้อมูลนี้คือ **ประวัติการใช้อินเทอร์เน็ตของทุกคนในบ้าน** — เก็บลงดิสก์ = ของที่หลุดได้ถาวร
  แผนเดิมตัดสินใจไว้แล้วว่า "reboot แล้วหายหมด" คือ feature ไม่ใช่ข้อจำกัด (`statistics-dns-top-domain-plan.md` §5 ข้อ 1)
- ผลที่ผู้ใช้ต้องรับรู้: **restart pigate = สถิติเริ่มนับใหม่** → ต้องเขียนบอกไว้ในแท็บ (T-07)

### 2.3 สิทธิ์การเข้าถึง — ตัดสินแล้ว: ทางเลือก A (`authRoute`) เจ้าของยืนยัน 2026-07-31

ข้อมูล "เครื่องไหนถามโดเมนอะไร" อ่อนไหวกว่ายอดรวมต่อโดเมนที่ shipped ไปแล้วอย่างมีนัยสำคัญ

| ทางเลือก | ผล |
|---|---|
| **A (แผนนี้ตั้งค่าไว้)** `authRoute` | ผู้ล็อกอินทุก role ดูได้ สอดคล้องกับ `/api/statistics/traffic` เดิมที่โชว์ทั้งโดเมนและ IP ต้นทางอยู่แล้ว |
| **B** `superAdminRoute` | read-only admin เห็นแท็บนี้ไม่ได้เลย (401/403) — ต้องซ่อนแท็บฝั่ง UI ตาม role ด้วย |

ผู้วางแผนแนะนำ **A** เพราะข้อมูลระดับเดียวกันเปิดให้ `authRoute` อยู่แล้ว การทำ B อย่างเดียวโดยไม่ปิด
`/api/statistics/traffic` ด้วยจะเป็นความปลอดภัยเชิงพิธีกรรม — **ตัดสินแล้ว: ทางเลือก A เจ้าของยืนยัน
2026-07-31** ทั้ง 3 endpoint ใหม่ใช้ `authRoute` ตามแผนเดิม ไม่ต้องแก้ T-03/T-07 เพิ่ม

## 3. ขั้นตอนการทำ (เรียงตาม dependency)

### T-01 — DTO
**ไฟล์:** `backend/internal/model/statistics.go` (แก้ ท้ายไฟล์)
```go
// DNSClientStat — หนึ่งแถวของตาราง Source Host
type DNSClientStat struct {
    IP       string  `json:"ip"`
    Hostname string  `json:"hostname"` // จาก DHCP lease/reservation, ว่างได้
    Count    uint64  `json:"count"`
    Percent  float64 `json:"percent"`
}

// DNSQueryStatistics — response ของ GET /api/statistics/dns
type DNSQueryStatistics struct {
    Window       string          `json:"window"`
    Enabled      bool            `json:"enabled"`
    TotalQueries uint64          `json:"totalQueries"`
    Truncated    bool            `json:"truncated"`
    TopDomains   []TopDomain     `json:"topDomains"`  // reuse DTO เดิม
    TopClients   []DNSClientStat `json:"topClients"`
    GeneratedAt  string          `json:"generatedAt"`
}

// DNSDomainDrilldown — response ของ GET /api/statistics/dns/domain
type DNSDomainDrilldown struct {
    Domain string; Window string; Enabled bool; TotalQueries uint64 // ยอดของ "โดเมนนี้" ใน window
    Truncated bool; Clients []DNSClientStat; GeneratedAt string
}

// DNSClientDrilldown — response ของ GET /api/statistics/dns/client
type DNSClientDrilldown struct {
    Client string; Hostname string; Window string; Enabled bool; TotalQueries uint64
    Truncated bool; Domains []TopDomain; GeneratedAt string
}
```
> **ไม่ต้อง**สร้าง DTO ใหม่สำหรับแถวโดเมน — `model.TopDomain` (domain/queryType/count/percent) ตรงอยู่แล้ว
> และ **ห้ามแก้ `TrafficStatistics`** (การ์ดเดิมต้องนิ่ง)

**acceptance:** `go build ./...` ผ่าน, ไม่มีฟิลด์เดิมถูกแก้/ลบ

### T-02 — service: เพิ่มมิติ client เข้า ring + snapshot/builder ใหม่ 🔒 (หัวใจของแผน)
**ไฟล์:** `backend/internal/service/dns_query_stats.go` (แก้), `service/statistics.go` (แก้เล็กน้อย)
1. เปลี่ยน `domainBucket` ตาม §2.1 (`pairs`, `pairCount`, `clientCount`), เพิ่ม const
   `maxTrackedDNSPairs = 1200`, `maxTrackedDNSClients = 200`, `dnsUnknownClient = "unknown"`,
   `dnsStatsTopN = 50` (ตารางเต็มหน้าเว็บ ไม่ใช่การ์ด 10 แถว), ลบ `maxTrackedDomains` ที่ไม่ใช้แล้ว
2. `recordDomainQuery(domain, qtype, client string)` — ยังต้อง **O(1), ไม่มี I/O, ไม่ log โดเมน**
   (รันบน read loop ของ watcher): client ว่าง → `dnsUnknownClient`; สร้าง inner map เมื่อจำเป็น;
   นับ pair ใหม่ได้ต่อเมื่อ `pairCount < maxTrackedDNSPairs` (คู่ที่มีอยู่แล้วนับต่อได้เสมอ);
   `clientCount` ใช้กติกาเดียวกันกับ `maxTrackedDNSClients`; `queries++` **ไม่ติด cap** (ยอดรวมต้องจริงเสมอ)
3. `RecordDNSEvent` ส่ง `ev.ClientIP` ต่อลงมา (เปลี่ยนแค่การเรียก)
4. `domainSnapshot(window)` — คืน `totals map[string]uint64` เหมือนเดิม แต่ derive จาก `pairs`
   (ลายเซ็นเดิม/ผู้เรียกเดิมใน `statistics.go` ไม่ต้องแก้) → การ์ดเดิมทำงานเท่าเดิม
5. เพิ่ม 3 เมธอดใหม่ (public, ใช้ `windowBuckets` ตัวช่วยร่วมกัน อย่า copy logic เลือก bucket ซ้ำ):
   - `GetDNSQueryStatistics(window string) model.DNSQueryStatistics`
   - `GetDNSDomainClients(window, domain string) model.DNSDomainDrilldown`
   - `GetDNSClientDomains(window, client string) model.DNSClientDrilldown`
   ทั้งสามอ่านใต้ `dns.mu.RLock()` ก้อนเดียว, เติม hostname ด้วย `s.traffic.hostLookup()` +
   `hostnameFor(...)` (pattern เดียวกับ `buildTopDeniedSources`), sort deterministic
   (count desc → key asc), ตัดที่ `dnsStatsTopN`
6. **percent:** ตาราง A/B คิดจาก `totalQueries` ของ window; drill-down คิดจาก **ยอดของโดเมน/เครื่องนั้น**
   (ไม่ใช่ยอดรวมทั้ง window) — เขียน comment กำกับ ไม่งั้นคนอ่านตัวเลขผิด
7. `enabled == false` → ทั้งสามเมธอดคืน struct ที่ list ว่าง + `Enabled:false` (ไม่ error)
> **ไม่ต้อง**แตะ `main.go` — `RecordDNSEvent` ลายเซ็นเดิม watcher เดิมทำงานต่อได้ทันที

**acceptance:** `go build ./...` ผ่าน; hot path ยังไม่มี I/O/log; `GetStatistics` เดิมยังคืนค่าครบทุกฟิลด์

### T-03 — API: handler + route 🔒 (จุดรับ input จาก client)
**ไฟล์:** `backend/internal/api/handlers.go` (ต่อจาก `HandleGetStatistics` ~`:425`), `api/router.go` (~`:42`)
```go
authRoute("GET /api/statistics/dns",        s.HandleGetDNSQueryStatistics)
authRoute("GET /api/statistics/dns/domain", s.HandleGetDNSDomainClients)
authRoute("GET /api/statistics/dns/client", s.HandleGetDNSClientDomains)
```
- `window` — whitelist `{"1h","24h"}` เท่านั้น ค่าอื่น/ว่าง → `"1h"` (ลอก `HandleGetStatistics:420`)
- 🔒 `domain` — บังคับมี; normalize `strings.ToLower` + ตัด `.` ท้าย; ยาว ≤ 253;
  อนุญาตเฉพาะ `a-z 0-9 . - _ *`; ไม่ผ่าน → `400` พร้อมข้อความกลาง ๆ **ห้าม echo ค่าที่ผู้ใช้ส่งมากลับไปใน error**
  (กันสะท้อน payload) — ใช้ helper ใหม่ใน `model` (เช่น `model.NormalizeQueryDomain(s) (string, bool)`)
  เพื่อให้ backend/เทสต์ใช้กติกาชุดเดียวกับ sanitizer ของ parser
- 🔒 `client` — บังคับมี; ต้อง `netip.ParseAddr` ผ่าน **หรือ** เท่ากับ `"unknown"` พอดี; ไม่ผ่าน → `400`
  (เก็บค่าที่ normalize แล้ว `addr.String()` ไปค้น ไม่ใช่ string ดิบ — ให้ตรงกับ key ที่ ring เก็บ)
- ทั้งสามเป็น GET → ไม่โดน `DisableEditMiddleware` (ถูกต้อง: อ่านอย่างเดียว) และยังโดน `AuthMiddleware`
- ไม่พบโดเมน/เครื่องใน window → `200` พร้อม list ว่าง (ไม่ใช่ 404 — เป็นเรื่องปกติของ window ที่เลื่อนไป)

**acceptance:** `go build ./... && go test ./internal/api/...` ผ่าน

### T-04 — mock: ข้อมูลสังเคราะห์ให้เห็น drill-down จริง
**ไฟล์:** `backend/internal/kernel/mock.go` (`mockDNSQueryEvents` ~`:412`)
- ปัจจุบัน 1 โดเมนผูกกับ 1 client → drill-down จะมีแถวเดียวตลอด **ไม่เห็นของจริง**
- แก้เป็น **matrix**: อย่างน้อย 3 client (`192.168.1.101/102/105`) × 4-5 โดเมน โดยให้
  `www.youtube.com` ถูกถามจาก 3 เครื่อง (weight ต่างกัน), `netflix.com` จาก 2 เครื่อง,
  และเหลือ 1 โดเมนที่มีเครื่องเดียวไว้ทดสอบเคสแถวเดียว
- คง `mockDNSAnswerEvents` และ interval เดิมไว้ **ห้ามแตะ filesystem**
**acceptance:** `-mock=true` แล้ว drill-down ทั้ง 2 ทางมีมากกว่า 1 แถวในบางรายการ

### T-05 — เทสต์ backend
**ไฟล์:** `service/dns_query_stats_test.go` (ใหม่/ขยายจาก `statistics_test.go`), `api/handlers_test.go` (แก้)
1. นับคู่ถูกต้อง: 3 เครื่อง × 2 โดเมน → ตาราง A/B อันดับถูก, `Σ drill-down == ยอดในตาราง`
2. `ClientIP == ""` → ไปลงที่ `"unknown"` และ drill-down ด้วยคีย์ `"unknown"` เจอ
3. cap: ยิง 5,000 คู่ไม่ซ้ำ → `pairCount` ไม่เกิน 1200/bucket, ไม่ panic, `truncated == true`,
   `totalQueries` ยัง **นับครบทุก event** (ไม่ถูก cap ตัด)
4. window: 24h ≥ 1h เสมอ; bucket เก่ากว่า 1h ไม่โผล่ใน 1h
5. percent: ตาราง A/B รวมไม่เกิน 100; drill-down คิดจากฐานของตัวเอง (โดเมนที่มี client เดียว → 100%)
6. สวิตช์ปิด → ทั้ง 3 เมธอดคืน `Enabled:false` + list ว่าง; `ClearDNSStats()` → ว่างทันที
7. **regression:** ลำดับ/ตัวเลขของ `GetStatistics().TopDomains` เท่ากับก่อนแก้ (เทสต์เดิมต้องผ่านโดยไม่แก้ค่าคาดหวัง)
8. 🔒 handler: `domain` ที่มี `<script>`, newline, `\x00`, ช่องว่าง, ยาว 300 ตัวอักษร → `400`;
   `client=notanip` → `400`; `client=unknown` → `200`; ไม่ส่ง param → `400`; `window=evil` → fallback `1h`
9. `go test -race ./internal/service/...` โดยยิง `RecordDNSEvent` พร้อม `GetDNSQueryStatistics` +
   `GetDNSClientDomains` พร้อมกัน
**acceptance:** `cd backend && go test -race ./...` ผ่าน

### T-06 — API contract
**ไฟล์:** `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (ต้อง diff ตรงกันเป๊ะ)
- เพิ่ม 3 path + schema `DNSQueryStatistics`, `DNSClientStat`, `DNSDomainDrilldown`, `DNSClientDrilldown`
- ระบุ enum ของ `window`, บังคับ `domain`/`client`, และหมายเหตุว่า **ข้อมูลเป็น RAM-only หายเมื่อ restart**
  และ **ใช้เพื่อแสดงผลเท่านั้น**
**acceptance:** สองไฟล์เหมือนกัน, หน้า ApiDocs เรนเดอร์ไม่ error

### T-07 — frontend: service client + แท็บใหม่ (2 ตาราง)
**ไฟล์:** `frontend/src/services/dnsStatisticsService.ts` (**ไฟล์ใหม่**), `frontend/src/pages/DnsServer.tsx` (แก้)
1. service ใหม่: type ตรงกับ Go 1:1 + 3 method (`getDNSStatistics`, `getDomainClients`, `getClientDomains`)
   พร้อม **mock branch** ที่ใช้ matrix ชุดเดียวกับ `kernel/mock.go` (T-04) — วางไว้ไฟล์แยกจาก
   `statisticsService.ts` เพราะคนละหน้า/คนละ endpoint (ลอกโครง `IS_MOCK_MODE` ของไฟล์นั้นมาได้เลย)
2. `DnsServer.tsx`: เพิ่ม `<TabsTrigger value="stats">สถิติ</TabsTrigger>` (วางหลัง "Blocked Domains")
   และ `<TabsContent value="stats">` ประกอบด้วย
   - แถวบน: ปุ่มสลับ window `1h / 24h` + ปุ่ม Refresh + เวลา `generatedAt`
   - 2 `<Card>` วางคู่กัน (`grid lg:grid-cols-2`) แต่ละใบเป็น `<Table>` ตาม pattern แท็บ Blocked
     (`DnsServer.tsx:1059-1130`) — แถวคลิกได้ (`cursor-pointer hover:bg-muted/50`, มี `title` บอกว่าคลิกเพื่อดูรายละเอียด)
   - `enabled === false` → empty state "ยังไม่ได้เปิดการเก็บสถิติ DNS" + ปุ่มพาไปแท็บ Settings
     (ต้องเปลี่ยน `<Tabs defaultValue="zones">` เป็น controlled `value/onValueChange` เพื่อสลับแท็บได้)
   - `truncated === true` → Badge เตือนว่าอันดับอาจไม่ครบ
   - บรรทัดหมายเหตุ: ข้อมูลเก็บใน RAM **restart แล้วเริ่มนับใหม่** + เป็นข้อมูลส่วนบุคคลของคนในบ้าน
3. โดเมนแสดงเป็น text node ธรรมดา (React escape ให้) **ห้าม `dangerouslySetInnerHTML`**,
   `font-mono text-xs` + `truncate` + `title` เต็ม ๆ
4. กฎสไตล์: `components/ui/*` เท่านั้น, ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามสีดิบ, dark/light ครบ
**acceptance:** `yarn build && yarn lint` ผ่าน; ตารางทั้งสองมีข้อมูลใน mock mode

### T-08 — frontend: Dialog drill-down 2 ทาง
**ไฟล์:** `frontend/src/pages/DnsServer.tsx` (แก้ ต่อจาก T-07)
- state เดียว: `drilldown: { kind: "domain" | "client"; key: string } | null` → `<Dialog open={!!drilldown}>`
  ใช้ **modal ปกติ** (ไม่มี Combobox ในนี้ → ห้ามใส่ `modal={false}` ตาม `rules_of_work.md`)
- หัวข้อ Dialog: `"Source Hosts ที่ค้นหา <domain>"` / `"โดเมนที่ <ip> (<hostname>) ค้นหา"`
- เนื้อใน: loading spinner → `<Table>` (คอลัมน์ตามชนิด) + จำนวนครั้ง + % + empty state
- โหลดข้อมูลตอนเปิดเท่านั้น (ไม่ prefetch ทุกแถว), เปลี่ยน window ขณะเปิดอยู่ → refetch
- `"unknown"` ในตาราง B แสดงเป็น "ไม่ทราบต้นทาง" แต่ยังคลิก drill-down ได้
**acceptance:** `yarn build && yarn lint` ผ่าน; คลิกได้ทั้ง 2 ทิศทางใน mock mode

### T-09 (ท้ายสุด) — เอกสาร
**ไฟล์:** `docs/ref/complete/dns-system-design.md` (หรือเอกสาร DNS ที่เหมาะสม)
- บันทึกว่า ring ของ DNS statistics เก็บเป็นคู่ (domain, client) แล้ว, cap ใหม่, ข้อจำกัด RAM-only,
  และว่า per-client drill-down **ไม่ใช่ "นอกขอบเขต" อีกต่อไป** (แก้ข้อความในแผนเดิมที่ระบุว่าไม่ทำ)
- README Feature Status: ไม่ต้องแก้ (ยัง Completed ทั้งคู่)

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | พฤติกรรม |
|---|---|---|---|
| GET | `/api/statistics/dns?window=1h\|24h` | `authRoute` (ดู §2.3) | **เส้นใหม่** — 2 ตารางหลัก |
| GET | `/api/statistics/dns/domain?domain=<d>&window=` | `authRoute` | **เส้นใหม่** — client ที่ถามโดเมนนั้น; `domain` ไม่ผ่าน validate → 400 |
| GET | `/api/statistics/dns/client?client=<ip\|unknown>&window=` | `authRoute` | **เส้นใหม่** — โดเมนที่เครื่องนั้นถาม; `client` ไม่ผ่าน validate → 400 |
| GET | `/api/statistics/traffic` | `authRoute` | **เส้นเดิม ไม่เปลี่ยน response** (regression guard) |

ทั้งหมดเป็น GET → `-disable-edit=true` และ role read-only **ดูได้ตามปกติ** (ตัดสินแล้ว: ทางเลือก A ใน §2.3)

## 5. ข้อควรระวัง

1. 🔒 **ความเป็นส่วนตัวยกระดับขึ้นจริง** — เดิมเห็นแค่ "โดเมนไหนถูกถามกี่ครั้ง" ตอนนี้เห็น
   "ใครถามอะไร" = ประวัติการใช้เน็ตรายบุคคล → ต้องคงกติกาเดิมครบ: opt-in, ปิดสวิตช์แล้วล้างทันที,
   ไม่ลงดิสก์, ไม่ log โดเมนออกทาง `log.Printf` และ **ต้องมีข้อความบอกผู้ใช้ในแท็บ** (T-07)
2. **hot path ต้องไม่ช้าลง** — `recordDomainQuery` รันบน read loop ของ watcher: การเพิ่ม inner map
   ต้องไม่ทำ allocation เกินจำเป็น (สร้าง inner map เฉพาะตอนเจอโดเมนใหม่) และ **ห้ามเรียก
   `hostLookup()`/`hostnameFor()` ในนี้เด็ดขาด** — เติม hostname ตอนประกอบ response เท่านั้น
3. **RAM โตขึ้นจริง** (§2.1) — cap คือหลักประกันเดียว: `pairCount` ต้องถูกนับ/ตรวจก่อนเพิ่มคู่ใหม่เสมอ
   ทั้งชั้นนอกและชั้นใน (พลาดที่ inner map = cap ไม่มีผล) และต้องมีเทสต์ยิง 5,000 คู่ล็อกไว้
4. **regression ของการ์ดเดิม** — `domainSnapshot` เปลี่ยนวิธีคำนวณภายใน ถ้า derive ผิด
   "Top Queried Domains" ในหน้า `/logs/statistics` จะเพี้ยนเงียบ ๆ → เทสต์เดิมต้องผ่าน **โดยไม่แก้ค่าคาดหวัง**
5. 🔒 **`domain`/`client` เป็น input จาก client** — ใช้เป็น map key เท่านั้น (ไม่ประกอบไฟล์/คำสั่ง/SQL)
   แต่ยังต้อง validate เพื่อกัน (ก) ค่ายาวผิดปกติ (ข) การสะท้อน payload กลับใน error message
   (ค) ค่าที่ normalize ไม่ตรงกับ key ที่เก็บ → ค้นไม่เจอทั้งที่มีข้อมูล (เคส IPv6 เขียนย่อ/ยาว)
6. **`unknown` เป็นค่าสงวน** — ถ้าอนาคตมี client ที่ hostname ชื่อ "unknown" จะสับสน แต่ key คือ **IP**
   ไม่ใช่ hostname จึงชนกันไม่ได้ (IP ไม่มีทาง parse ได้เป็น "unknown") — เขียน comment กำกับไว้
7. **ตัวเลขไม่ตรงกันตอนชน cap** — เมื่อ `truncated == true` ผลรวม drill-down อาจน้อยกว่ายอดในตาราง
   (คู่ใหม่ถูกปฏิเสธแต่ `queries` ยังนับ) → UI ต้องโชว์ Badge เตือน ไม่ใช่ปล่อยให้ผู้ใช้งง
8. **window ต้องนิยามเดียวกับหน้า Statistics** (12 bucket ล่าสุด = 1h) ไม่งั้นผู้ใช้เทียบตัวเลข 2 หน้าแล้วไม่ตรง
9. **แท็บใหม่ห้ามยิง API ตอนยังไม่ถูกเปิด** — `<TabsContent>` ของ Radix render เนื้อหาเมื่อ active
   แต่ถ้า fetch ไว้ที่ระดับหน้า จะยิงทุกครั้งที่เข้าหน้า DNS Server → โหลด fetch ไว้ใน component ของแท็บ
   หรือ guard ด้วย `activeTab === "stats"`; และ **อย่าตั้ง auto-refresh ถี่กว่า 10 วินาที**
10. **หน้า DnsServer.tsx ยาวมากอยู่แล้ว (~1,700 บรรทัด)** — แท็บสถิติควรแยกเป็น component ย่อย
    (เช่น `frontend/src/components/dns/DnsStatisticsTab.tsx`) เพื่อไม่ให้ไฟล์บวมจนแก้ยาก
    แต่ยังใช้ `components/ui/*` เหมือนเดิม
11. **`-mock=true` ต้องปลอดภัย 100%** — T-04 แก้แค่ข้อมูลในหน่วยความจำ ห้ามแตะ `/run` หรือไฟล์ใด ๆ
12. **ห้ามเพิ่มตาราง/คอลัมน์ใน `db/`** — `git diff` ของ `backend/internal/db/` ต้องว่างเปล่าในแผนนี้

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [ ] T-01 DTO ใหม่ 4 ตัวใน `model/statistics.go` (ไม่แก้ `TrafficStatistics`)
- [ ] T-02 `service/dns_query_stats.go`: bucket แบบคู่ + cap ใหม่ + 3 เมธอด + `domainSnapshot` derive 🔒
- [ ] T-03 `api/handlers.go` + `api/router.go`: 3 endpoint + validate `window`/`domain`/`client` 🔒
- [ ] T-04 `kernel/mock.go`: matrix โดเมน × client
- [ ] T-05 เทสต์ 9 กลุ่ม + `go test -race ./...`

**Frontend / Docs**
- [ ] T-06 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync)
- [ ] T-07 `services/dnsStatisticsService.ts` (ใหม่) + แท็บ "สถิติ" + 2 ตาราง (+ component ย่อยตามข้อ 10)
- [ ] T-08 Dialog drill-down 2 ทาง
- [ ] T-09 อัปเดต `docs/ref/complete/dns-system-design.md`

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ)**
- [ ] `cd backend && go build ./... && go test -race ./...` ผ่าน; `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] `-mock=true -allow-dev-cors`: แท็บ "สถิติ" มีข้อมูลทั้ง 2 ตาราง และตัวเลขขยับทุก ~2 วินาที
- [ ] mock: คลิกโดเมนที่มีหลายเครื่อง → เห็น >1 แถว; คลิกเครื่อง → เห็นโดเมนที่เครื่องนั้นถามครบ
- [ ] mock: **ผลรวมจำนวนครั้งใน drill-down == ตัวเลขในตารางหลัก** ของรายการนั้น (ทั้ง 2 ทิศทาง)
- [ ] mock: สลับ window 1h ↔ 24h → 24h ≥ 1h เสมอ, % ไม่เกิน 100
- [ ] mock: ไม่มีการสร้าง/แตะไฟล์ใน `/run` (`ls /run/pigate` บนเครื่อง dev)
- [ ] หน้า `/logs/statistics` เดิม: การ์ด "Top Queried Domains"/"Top Destinations"/"Top Conversations"
      แสดงผลเหมือนก่อนแผนนี้ทุกประการ
- [ ] real device: ปิดสวิตช์ query logging → แท็บแสดง empty state + ปุ่มพาไปแท็บ Settings ทำงาน
- [ ] real device: เปิดสวิตช์ แล้วเปิดเว็บจากมือถือ → ภายใน ~10 วินาที โดเมนขึ้นในตาราง A
      **และ** IP/ชื่อเครื่องของมือถือขึ้นในตาราง B และ drill-down ทั้ง 2 ทางชี้ถึงกันถูกต้อง
- [ ] real device: ปิดสวิตช์ → ทั้ง 2 ตารางว่างทันที (ข้อมูลถูกล้าง ไม่ค้าง)
- [ ] real device: restart `pigate.service` → สถิติเริ่มนับใหม่ (ยืนยันว่าไม่มีอะไรลงดิสก์),
      `sqlite3 pigate.db .tables` ไม่มีตารางใหม่
- [ ] 🔒 ยิง API ตรง ๆ: `?domain=<script>alert(1)</script>`, domain ยาว 300 ตัวอักษร, `?client=notanip`,
      ไม่ส่ง param → **400 ทุกกรณี** และ error message ไม่สะท้อนค่าที่ส่งไป; `?client=unknown` → 200
- [ ] 🔒 logout แล้วเรียกทั้ง 3 endpoint → 401
- [ ] `-disable-edit=true` / role read-only → ยังดูแท็บได้ตามปกติ (ตัดสินแล้ว: ทางเลือก A ใน §2.3, ไม่ใช่ 403)
- [ ] โหลด DNS หนัก (~1,000 query จากหลายเครื่อง) → หน้าเว็บยังตอบเร็ว, RSS ของ pigate ไม่โตต่อเนื่อง
      หลังปล่อยรัน ≥ 1 ชั่วโมง
- [ ] 🔒 `git diff --stat`: ไม่มีการเปลี่ยนแปลงใน `backend/internal/db/`, ไม่มี `exec.Command` ใหม่,
      ไม่แตะ `kernel/real_*.go`
- [ ] ทุกอย่างอยู่บน branch `feat/dns-blocked-domains` และเข้า `main` ผ่าน PR เท่านั้น
