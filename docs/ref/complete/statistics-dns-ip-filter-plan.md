# Statistics → DNS: ใส่ IP ในช่อง filter โดเมน เพื่อดูว่า IP นั้นเป็นของโดเมนอะไรบ้าง

> เอกสารแผนงานสำหรับคำขอของเจ้าของ repo บน branch `feat/statistics-dns-page` (ต่อยอดจาก PR 127)
>
> วันที่เขียน: 2026-08-06 · Branch อ้างอิง: `feat/statistics-dns-page` @ `fe3cb3e`
> ผู้เขียน: ai-tech-lead (สำรวจโค้ดจริงทั้ง backend/frontend ก่อนวางแผน)

## 0. เป้าหมายและขอบเขต

**คำขอต้นทาง (เจ้าของ repo):** _"ถ้าผมอยากใช้ช่อง filter ชื่อโดเมนสามารถใส่ ip เพื่อดูว่าเป็นของโดเมนอะไรบ้างได้ไหม
ข้อสังเกตจากการที่มีสถานะ ip ซ้ำในชื่อโดเมนอื่นๆ"_

**เป้าหมาย (สิ่งที่ผู้ใช้จะเห็น)**

1. ที่หน้า `/statistics/dns` ตาราง **Top Domains** ช่อง filter เดิม (ค้นหาโดเมน/ประเภท) รับ **IP address** ได้ด้วย
2. เมื่อพิมพ์ IP ครบถ้วนถูกต้อง → ตารางเปลี่ยนเป็น "โดเมนทั้งหมดที่เคย resolve ไปยัง IP นี้"
   (**ไม่ใช่** แค่กรองแถวเดิม — ดู §2.1 ว่าทำไมกรองฝั่ง frontend ล้วนให้คำตอบผิด)
3. ถ้า IP นั้นถูกใช้ร่วมกันหลายโดเมน (CDN/shared hosting) ต้องเห็น **ครบทุกโดเมน** พร้อมคำเตือนว่าปริมาณข้อมูลถูกนับซ้ำ
4. คลิกแถวโดเมนแล้วเข้าหน้า domain drill-down เดิมได้ตามปกติ
5. ที่หน้า domain drill-down ตาราง "IP ที่ได้จากการ resolve" คลิกแถว IP → เด้งกลับมาหน้า DNS ในโหมด IP filter ของ IP นั้น
   (ตอบข้อสังเกต "IP ซ้ำในโดเมนอื่น" ได้ในคลิกเดียว)

**นอกขอบเขต (จะไม่ทำ — กันแผนบวม)**

- ตาราง **Top Source Hosts** ไม่เปลี่ยนพฤติกรรมเลย (ช่อง filter ของมันค้น IP/hostname/domain ของ client อยู่แล้ว)
- ไม่ทำหน้าใหม่ `/statistics/dns/ip/:ip` — ใช้โหมด filter บนหน้าเดิม + URL param `?ip=` แทน (linkable/refresh ได้เหมือนกัน แต่ไฟล์น้อยกว่า)
- ไม่แตะหน้า Statistics → Traffic, ไม่แตะ `dnsReverseCache` (index คนละตัว — ดู §1)
- ไม่รองรับการพิมพ์ **บางส่วน** ของ IP แล้วค้นหาแบบ substring (เช่น `142.250.`) — ดูเหตุผลใน §2.3
- ไม่รองรับ CIDR / range
- ไม่เพิ่ม dependency ใหม่ทั้ง Go และ npm

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันเขียน)

| ส่วน | สถานะจริง | ไฟล์:บรรทัด (~) |
|---|---|---|
| ช่อง filter ตาราง Top Domains | `useTextFilter(rows, query, [d.domain, d.queryType])` — **client-side ล้วน** บนแถวที่โหลดมาแล้ว | `frontend/src/components/statistics/DnsStatsShared.tsx:153-159` |
| แถว Top Domains มีข้อมูล IP ไหม | **ไม่มี** — `DNSDomainStat` มีแค่ `ipCount` (จำนวน) กับ `sharedIps` (bool) ไม่มีรายชื่อ IP | `backend/internal/model/statistics.go:194-227` · `frontend/src/services/dnsStatisticsService.ts:47-75` |
| จำนวนแถวที่ส่งมา | ตัดที่ `dnsStatsTopN = 50` เสมอ (`TotalDomains` บอกจำนวนจริงที่มากกว่า) | `backend/internal/service/dns_query_stats.go:52` · `service/statistics_dns.go:130` |
| index domain→IP | `dnsDomainIPs` (RAM-only, TTL, cap `maxDomains`=1000 × `maxIPsPerDomain`=16) มี `byDomain`, `ipRefs` (ip→จำนวนโดเมนที่อ้างถึง = ที่มาของ flag `shared`) | `backend/internal/service/dns_domain_ips.go:35-50` |
| ความสามารถ "IP → โดเมน" ที่มีอยู่ | **ยังไม่มี**: `IPsFor(domain)`, `StatsFor([]domain)`, `Snapshot()` (ip→domain **ตัวเดียว** last-writer-wins — ใช้ตอบคำถามนี้ไม่ได้ เพราะยุบ IP ที่ซ้ำหลายโดเมนเหลือโดเมนเดียว ดู doc comment ที่ `:269-278`) | `service/dns_domain_ips.go:187,240,279` |
| `dnsReverseCache` | index ip→domain **ล่าสุด** (last-writer-wins) — ก็ตอบ "มีโดเมนอะไรบ้าง" ไม่ได้เช่นกัน | `service/dns_reverse_cache.go` |
| endpoint DNS statistics | 3 เส้น: `/api/statistics/dns`, `/dns/domain`, `/dns/client` ทั้งหมด `authRoute` | `backend/internal/api/router.go:52-54` |
| การ validate input ของ handler | `domain` → `model.NormalizeQueryDomain`, `client`/`ip` → `netip.ParseAddr` แล้ว re-serialize `addr.String()`; ห้าม echo ค่าดิบกลับ | `api/handlers.go:490-540, 600-617` |
| `window` | whitelist ผ่าน `statsWindowParam` (7 ค่า) fallback `1h` | `api/handlers.go:418-424` |
| กติกา locking ของ service ชั้นนี้ | ต้อง `s.dns.mu.RUnlock()` **ก่อน** แตะ `domainIPs`/`traffic`/`reverseCache`; 1 request = ไม่เกิน 1 `GetTrafficBreakdown*` + 1 `hostLookup` | header comment `service/statistics_dns.go:26-34` |
| openapi | 2 ไฟล์ต้องเหมือนกันทุก byte: paths `/statistics/dns*` อยู่ที่บรรทัด 534/583/645, schema `DNSDomainStat`:4925, `DNSDomainIP`:5008 | `docs/openapi.yaml` · `frontend/public/openapi.yaml` |
| frontend test runner | **ไม่มี** (มีแค่ dev/build/lint/preview) → เกณฑ์ฝั่ง frontend คือ `yarn build` + `yarn lint` | `frontend/package.json` |

**สรุปหนึ่งบรรทัด:** ข้อมูลที่จะตอบคำถามนี้มีอยู่ครบใน `dnsDomainIPs` แล้ว แต่ **ยังไม่มีทางอ่านแบบ ip→[]domain**
และแถวที่ frontend มีอยู่ก็ตัดที่ top-50 + ไม่มีรายชื่อ IP → งานจริงคือเพิ่ม reverse lookup ที่ backend + endpoint ใหม่ 1 เส้น + โหมด IP บน UI เดิม

## 2. แนวทางเทคนิค

### 2.1 ทำไม **ไม่** ทำเป็น filter ฝั่ง frontend ล้วน

| ทาง | ปัญหา |
|---|---|
| ส่ง `ips: string[]` มากับทุกแถว `DNSDomainStat` แล้วกรองใน browser | (ก) payload โตขึ้น ~50×16 string ทุก 10 วินาที (ทุก refresh), (ข) **ยังตอบผิดอยู่ดี** — โดเมนที่ resolve ไป IP นั้นอาจไม่ติด top-50, (ค) โดเมนที่ "ไม่มี query ในหน้าต่างเวลานี้เลย" จะไม่มีแถวให้กรองตั้งแต่แรก ทั้งที่ index ยังจำ mapping ไว้ (TTL ยาวกว่า window ได้) |
| **endpoint ใหม่ ip→domains (เลือกทางนี้)** | ตอบจาก index เต็ม ไม่ติดข้อจำกัด top-50/window, payload เท่าที่ใช้จริง, input ผ่าน `netip.ParseAddr` เหมือนเส้นอื่น |

### 2.2 backend: เปลี่ยน `ipRefs` เป็น reverse index จริง

ปัจจุบัน `ipRefs map[string]int` เก็บแค่ "จำนวน" โดเมนที่อ้าง IP นั้น เปลี่ยนเป็น
`ipDomains map[string]map[string]struct{}` (ip → set ของ domain) แล้ว

- `Shared` เดิมที่เช็ค `d.ipRefs[ip] > 1` → `len(d.ipDomains[ip]) > 1` (ความหมายเท่าเดิมเป๊ะ)
- `DomainsForIP(ip)` กลายเป็น O(จำนวนโดเมนของ IP นั้น) — **ไม่ต้องสแกนทั้ง index**
- `Put()` ยังคง O(1) (map insert เพิ่ม 1 ครั้ง) — ห้ามทำให้ hot path ของ `WatchDNSLog` ช้าลง

> ทางเลือกที่ตัดทิ้ง: คง `ipRefs` ไว้แล้วให้ `DomainsForIP` สแกน `byDomain` ทั้งก้อน — worst case ตาม cap สูงสุดที่ config รับได้คือ 20000×64 ≈ 1.28M entry ต่อการเรียก 1 ครั้ง **ใต้ lock เดียวกับ `Put`** → บล็อก DNS hot path ยาวเกินรับได้ จึงเลือกทำ reverse index แทน
>
> RAM ที่เพิ่ม: จาก `int` เป็น map เล็ก ๆ ต่อ IP — worst case ค่า default 1000×16 = 16,000 entry เท่าเดิม โตขึ้นระดับหลัก MB ไม่เกินนี้ (คงย่อหน้า "RAM budget" ใน doc comment ให้ตรงความจริงหลังแก้)

### 2.3 กติกาการตรวจว่า "นี่คือ IP ไม่ใช่ชื่อโดเมน" (frontend)

สถานะของข้อความในช่อง filter มี 3 แบบ:

| สถานะ | เงื่อนไข | พฤติกรรม |
|---|---|---|
| `domain` | ค่าอื่น ๆ ทั้งหมด | กรองแบบเดิม (`useTextFilter` บน domain/queryType) |
| `ip-partial` | ตัวอักษรมีแต่ `0-9` และ `.` (v4) หรือ `0-9a-f` และ `:` (v6) แต่ยัง parse ไม่ครบ | **กรองแบบเดิมต่อไป** + ขึ้นข้อความใบ้ใต้ช่อง "พิมพ์ IP ให้ครบเพื่อค้นหาโดเมนที่ resolve ไปยัง IP นี้" (ห้ามล้างตารางให้ว่างระหว่างพิมพ์) |
| `ip` | parse เป็น IPv4/IPv6 สมบูรณ์ | เข้าโหมด IP: ยิง endpoint ใหม่ แล้วเอาผลมาแทนแถวในตาราง |

เหตุผลที่ไม่ทำ substring match บน IP: ต้องสแกน index ทั้งก้อนต่อ 1 keystroke และให้ผลกำกวม (`1.1` ตรงกับ `1.1.1.1` และ `10.0.1.1`)
ส่วนแบบ "ครบแล้วค่อยยิง" ตรงกับกติกา validate ของ backend (`netip.ParseAddr`) พอดี ไม่ต้องมี logic ค้นหาแบบใหม่ที่ backend เลย

### 2.4 semantics ของตัวเลขในโหมด IP (ต้องนิยามให้ชัด ห้ามให้ developer เดา)

แถวที่ส่งกลับใช้ `DNSDomainStat` **ตัวเดิม** (คอลัมน์เท่าเดิม ตารางเดิมใช้ซ้ำได้) โดย

- `count` / `percent` = จำนวน query ของโดเมนนั้นในหน้าต่างเวลา / เทียบ `totalQueries` ทั้งหน้าต่าง (**ตรงกับหน้า overview เป๊ะ**)
- `clients`, `ipCount`, `sharedIps` = ค่า system-wide ของโดเมนนั้น (เหมือน overview)
- `bytes/bytesUp/bytesDown` = ปริมาณ join ของ **ทั้งโดเมน** (ผลรวมทุก IP ของโดเมน) เหมือน overview
- `bytesPercent` = เทียบ **ผลรวมของแถวที่ match IP นี้เท่านั้น** (`matchedBytes`) ไม่ใช่ `domainBytes` ทั้งหน้าต่าง —
  ต่างจาก overview โดยตั้งใจ (คำนวณ denominator ทั้งหน้าต่างต้อง join ทุกโดเมน = งานเท่า endpoint overview ทั้งเส้น โดยไม่ได้ช่วยตอบคำถามนี้) → **UI ต้องเขียนกำกับไว้ว่า % Vol ในโหมดนี้เทียบเฉพาะกลุ่มโดเมนที่ใช้ IP นี้**
- โดเมนที่ index จำ mapping ไว้แต่ **ไม่มี query ในหน้าต่างเวลานี้** ต้องแสดงด้วย (`count=0, percent=0`) — เพราะคำถาม "IP นี้เป็นของโดเมนอะไร" ไม่ผูกกับหน้าต่างเวลา
  (ระวัง: `rankTopDomains()` ตัดแถวที่ `count == 0` ทิ้ง `dns_query_stats.go:330` → **ห้ามใช้ `rankTopDomains` สร้างแถวชุดนี้**)

## 3. Task list (ให้ ai-developer ทำเรียงตามลำดับ)

> ห้าม commit เว้นแต่เจ้าของสั่ง · ทำงานบน branch `feat/statistics-dns-page` (มี PR อยู่แล้ว) หรือ branch ย่อยตามที่เจ้าของกำหนด
> **ห้ามทดสอบทีละ task** — ทำครบทุก task แล้วให้ ai-qa ทดสอบรวมตาม §5 ครั้งเดียว

```json
{
  "task_id": "T-01",
  "title": "dnsDomainIPs: เปลี่ยน ipRefs เป็น reverse index + เพิ่ม DomainsForIP/IPsForMany",
  "layer": "service",
  "files": ["backend/internal/service/dns_domain_ips.go", "backend/internal/service/dns_domain_ips_test.go"],
  "instruction": "1) เปลี่ยนฟิลด์ `ipRefs map[string]int` เป็น `ipDomains map[string]map[string]struct{}` (ip -> set ของ domain ที่อ้างถึง IP นั้น) แก้ทุกจุดที่ใช้: newDNSDomainIPs (:81), Put (:178 `d.ipRefs[ip]++` -> เพิ่ม domain ลง set), IPsFor (:208 `Shared: d.ipRefs[ip] > 1` -> `len(d.ipDomains[ip]) > 1`), StatsFor (:259), Snapshot (:287-288 ใช้ len(d.ipDomains) เป็น capacity hint), Clear (:316), decRefLocked (:372 -> เปลี่ยนชื่อเป็น decRefLocked(ip, domain) ลบ domain ออกจาก set แล้วลบ key ทิ้งเมื่อ set ว่าง) และผู้เรียก decRef ทั้ง 2 จุด (evictExpiredLocked :343, evictExpiredInDomainLocked :359 — ทั้งคู่มี domain อยู่ในมือแล้ว). ความหมายของ Shared ต้องเท่าเดิมเป๊ะ. 2) เพิ่ม `type ipDomainEntry struct { Domain string; LastSeen time.Time }` และ method `DomainsForIP(ip string) []ipDomainEntry`: คืน nil ถ้า ip == \"\"; ถือ d.mu.Lock(); สำหรับทุก domain ใน d.ipDomains[ip] ให้ evictExpiredInDomainLocked ก่อน แล้วอ่าน lastSeen จาก d.byDomain[domain][ip] (ข้ามโดเมนที่หมดอายุไปแล้ว); เรียงผลลัพธ์ LastSeen desc, tie-break Domain asc. ห้ามสแกน byDomain ทั้งก้อน. 3) เพิ่ม `IPsForMany(domains []string) map[string][]domainIPEntry` — เหมือน IPsFor แต่ทำหลายโดเมนใน lock เดียว (เหตุผลเดียวกับ StatsFor doc comment ที่ :226-239) โดเมนที่ไม่มีใน index ให้ไม่ใส่ใน map. 4) อัปเดต doc comment ของ struct (ย่อหน้า RAM budget :29-34) ให้ตรงกับโครงสร้างใหม่ และอธิบายว่า reverse index นี้มีไว้ตอบ ip->domains ให้ endpoint ใหม่. 5) แก้เทสต์เดิมที่อ้าง d.ipRefs โดยตรง (dns_domain_ips_test.go:98,108,210,278) ให้ใช้ ipDomains + เพิ่มเทสต์ใหม่: DomainsForIP กับ IP ที่ 2 โดเมนใช้ร่วมกัน (ต้องได้ครบ 2), IP ที่ไม่รู้จัก (nil), entry ที่หมด TTL แล้ว (ไม่ถูกคืนและถูกเก็บกวาด), IPsForMany หลายโดเมนพร้อมกัน. ห้ามสร้าง goroutine/ticker ใหม่ ห้าม log ชื่อโดเมน ห้ามทำให้ Put เกิน O(1).",
  "acceptance": [
    "go build ./... และ go test ./internal/service/... ผ่าน",
    "ไม่มีคำว่า ipRefs หลงเหลือในทั้ง .go และ _test.go",
    "Put ยังคงเป็น map operation จำนวนคงที่ (ไม่มี loop ตามขนาด index)",
    "DomainsForIP ไม่มีการวนลูปทั้ง d.byDomain"
  ],
  "depends_on": []
}
```

```json
{
  "task_id": "T-02",
  "title": "DTO: model.DNSIPDomain / model.DNSIPDomains",
  "layer": "model",
  "files": ["backend/internal/model/statistics.go"],
  "instruction": "เพิ่ม 2 struct ต่อท้ายกลุ่ม DNS DTO เดิม (ใกล้ DNSDomainIP :233-257) พร้อม doc comment สไตล์เดียวกับเพื่อนบ้าน (อธิบาย denominator ของทุก percent และ caveat ของ join ให้ครบ): (1) `type DNSIPDomain struct { DNSDomainStat; LastSeen string `json:\"lastSeen\"` }` — LastSeen = RFC3339 UTC ของ DNS answer ล่าสุดที่ผูก domain นี้กับ IP ที่ค้น (ไม่ผูกกับ window เหมือน DNSDomainIP.LastSeen). (2) `type DNSIPDomains struct` ฟิลด์: IP, Window, Enabled, Hostname, TotalQueries (uint64, ทั้ง window — เป็น denominator ของ Percent ในแต่ละแถว), Truncated, IPsTruncated, Domains []DNSIPDomain (ห้าม nil ใช้ empty slice), DomainCount int, Shared bool (DomainCount > 1), MatchedBytes/MatchedBytesUp/MatchedBytesDown uint64 (ผลรวมของแถว = denominator ของ BytesPercent ในแถว — ระบุในคอมเมนต์ว่าต่างจาก DNSQueryStatistics.DomainBytes โดยตั้งใจ), IPBytes/IPBytesUp/IPBytesDown uint64 (ของ IP ที่ค้นเอง), Series []BandwidthPoint (ห้าม nil), ObservedBytes, Accuracy, GeneratedAt. คอมเมนต์ต้องย้ำว่า mapping domain<->IP นี้ display-only และห้ามใช้กับ firewall/policy/routing/QoS เด็ดขาด (เหมือน DNSDomainIP.LastSeen :250-256).",
  "acceptance": ["go build ./... ผ่าน", "ทุกฟิลด์มี json tag แบบ camelCase ตรงสไตล์ไฟล์เดิม", "มีคอมเมนต์ระบุ denominator ของ Percent และ BytesPercent ชัดเจน"],
  "depends_on": []
}
```

```json
{
  "task_id": "T-03",
  "title": "service: GetDNSIPDomains(window, ip)",
  "layer": "service",
  "files": ["backend/internal/service/statistics_dns.go", "backend/internal/service/statistics_dns_test.go"],
  "instruction": "เพิ่ม method `func (s *StatisticsService) GetDNSIPDomains(window, ip string) model.DNSIPDomains` ต่อท้าย GetDNSClientDomains โดยยึด pattern ของ 3 method เดิมในไฟล์นี้เป๊ะ: (1) normalizeStatsWindow(window). (2) s.dns.mu.RLock(); ถ้า !s.dns.enabled ให้ RUnlock แล้วคืน payload ที่ Enabled=false, Domains/Series เป็น empty slice, ห้ามแตะ domainIPs/traffic เลย. (3) วน s.dnsWindowBuckets(window) เก็บ domainTotals, typeByDomain, domainClients (set client ต่อ domain), totalQueries และ truncated (b.pairCount >= s.dns.maxPairs || len(b.clientCount) >= s.dns.maxClients) แบบเดียวกับ GetDNSQueryStatistics :71-93. (4) RUnlock ก่อนแตะอย่างอื่นเสมอ (กติกา Caution 3 ใน header ไฟล์). (5) matches := s.dns.domainIPs.DomainsForIP(ip); ipsTruncated := s.dns.domainIPs.Truncated(). (6) ipsByDomain := s.dns.domainIPs.IPsForMany(<ชื่อโดเมนทั้งหมดใน matches>) — เรียกครั้งเดียว. (7) เรียก breakdown := s.traffic.GetTrafficBreakdownForDests(window, []string{ip}) **ครั้งเดียว** และ leaseByIP, resByIP := s.traffic.hostLookup() **ครั้งเดียว** แล้ว hostname, _ := hostnameFor(ip, leaseByIP, resByIP). (8) สร้างแถวจาก matches (ห้ามใช้ rankTopDomains — มันตัดแถวที่ count==0 ทิ้ง แต่แถว count==0 ต้องแสดง): แต่ละแถวใส่ Domain, QueryType จาก typeByDomain (ว่างได้), Count/Percent จาก domainTotals + percentOf(count, totalQueries), Clients=len(domainClients[d]), IPCount=len(ipsByDomain[d]), SharedIPs=มี entry ใดใน ipsByDomain[d] ที่ Shared, Bytes/Up/Down = ผลรวม breakdown.Dests[e.IP] ของทุก IP ในโดเมนนั้น, LastSeen=UTC RFC3339. (9) MatchedBytes* = ผลรวมทุกแถว แล้วเติม BytesPercent = percentOf(row.Bytes, MatchedBytes). (10) เรียงแถว: Bytes desc -> Count desc -> Domain asc (deterministic). (11) IPBytes* จาก breakdown.Dests[ip]; Series = breakdown.DestSeries ถ้า nil ให้ zero-fill โดย copy Ts จาก breakdown.Series (เหมือน :375-386); ObservedBytes/Accuracy จาก breakdown. (12) DomainCount=len(rows), Shared=DomainCount>1. เพิ่มเทสต์ใน statistics_dns_test.go ตามสไตล์ไฟล์เดิม: IP ที่ 2 โดเมนใช้ร่วมกัน (ได้ 2 แถว, Shared=true, ผลรวม BytesPercent ~100), IP ที่ไม่มีใครรู้จัก (Domains ว่างแต่ไม่ nil, Enabled=true), โดเมนที่มี mapping แต่ไม่มี query ในหน้าต่าง (ต้องมีแถว count=0), และกรณี query logging ปิด (Enabled=false ไม่แตะ traffic).",
  "acceptance": [
    "go build ./... และ go test ./internal/service/... ผ่าน",
    "ทั้ง method มี GetTrafficBreakdown* ไม่เกิน 1 ครั้ง และ hostLookup ไม่เกิน 1 ครั้ง",
    "ไม่มีการเรียก domainIPs/traffic/reverseCache ขณะยังถือ s.dns.mu",
    "ไม่ใช้ rankTopDomains กับแถวชุดนี้ และแถว count==0 ไม่ถูกตัดทิ้ง",
    "ทุก slice ใน response เป็น empty slice ไม่ใช่ nil"
  ],
  "depends_on": ["T-01", "T-02"]
}
```

```json
{
  "task_id": "T-04",
  "title": "api: GET /api/statistics/dns/ip (handler + route)  🔒 sensitive review",
  "layer": "api",
  "files": ["backend/internal/api/handlers.go", "backend/internal/api/router.go", "backend/internal/api/handlers_test.go"],
  "instruction": "เพิ่ม `HandleGetDNSIPDomains` ต่อจาก HandleGetDNSClientDomains (handlers.go ~:540) โดยลอกกติกา validate ของ HandleGetTrafficHostDetail (:600-617) มาเป๊ะ: window := statsWindowParam(r); raw := r.URL.Query().Get(\"ip\"); ถ้าว่าง หรือ netip.ParseAddr ไม่ผ่าน -> writeError 400 ข้อความ generic \"invalid ip\" (**ห้าม echo ค่าดิบที่ client ส่งมากลับไปใน response เด็ดขาด**) และ **ห้ามเรียก service**; ผ่านแล้วใช้ addr.String() เป็นค่าที่ส่งเข้า service (canonical form เดียวกับ key ของ index). ห้ามรับพารามิเตอร์อื่นเพิ่ม (ไม่มี sort/limit — เรียง/กรองทำที่ browser). เขียน doc comment เหนือ handler สไตล์เดียวกับเพื่อนบ้าน ระบุว่า mapping นี้ display-only, poison ได้จาก LAN client, ห้ามใช้กับ firewall/policy/routing/QoS. ที่ router.go ต่อจากบรรทัด :54 เพิ่ม `authRoute(\"GET /api/statistics/dns/ip\", s.HandleGetDNSIPDomains)` (authRoute เท่านั้น — ระดับความอ่อนไหวเท่ากับอีก 3 เส้น ไม่ต้อง superAdminRoute, และเป็น GET จึงไม่ถูก DisableEditMiddleware บล็อก ซึ่งถูกต้องแล้ว). เพิ่มเทสต์ใน handlers_test.go: ip ว่าง -> 400, ip เพี้ยน (เช่น \"1.2.3\", \"abc\", \"192.168.1.1; ls\") -> 400 และ body ไม่มีค่าดิบนั้นอยู่เลย, IPv6 แบบไม่ canonical (\"2001:DB8::1\") -> 200 และ field ip ใน response เป็นรูป canonical, window เพี้ยน -> fallback 1h ไม่ใช่ 400.",
  "acceptance": [
    "go build ./... และ go test ./internal/api/... ผ่าน",
    "ค่าดิบจาก client ไม่เคยถูกส่งต่อเข้า service และไม่เคยปรากฏใน response body",
    "route ใหม่อยู่ใต้ authRoute และเป็น GET เท่านั้น"
  ],
  "depends_on": ["T-03"]
}
```

```json
{
  "task_id": "T-05",
  "title": "openapi: path /statistics/dns/ip + schema DNSIPDomains/DNSIPDomain (2 ไฟล์ต้องเท่ากันทุก byte)",
  "layer": "api",
  "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
  "instruction": "เพิ่ม path `/statistics/dns/ip` ต่อจาก `/statistics/dns/client` (~บรรทัด 645 ในทั้ง 2 ไฟล์) — GET, tag Statistics, parameter `window` (enum 7 ค่าเหมือนเส้นอื่น) + `ip` (required, string, ตัวอย่างเป็น IPv4), response 200 -> $ref DNSIPDomains, 400 (Missing or invalid `ip`), 401 -> Unauthorized. เพิ่ม schema `DNSIPDomains` และ `DNSIPDomain` ในกลุ่ม schemas ใกล้ DNSDomainIP (~:5008) โดย field/description ต้องตรงกับ Go DTO ใน T-02 ทุกตัว รวมทั้งประโยคที่อธิบายว่า bytesPercent ใช้ denominator = matchedBytes (ไม่ใช่ domainBytes). แก้ทั้ง 2 ไฟล์ให้เนื้อหาเหมือนกันทุก byte (ห้ามแก้ไฟล์เดียว, ห้ามแก้ backend/internal/api/dist/openapi.yaml ซึ่งเป็น build output).",
  "acceptance": ["diff docs/openapi.yaml frontend/public/openapi.yaml ไม่มีความต่าง", "ทุก field ในสคีมาตรงกับ json tag ของ Go DTO"],
  "depends_on": ["T-02", "T-04"]
}
```

```json
{
  "task_id": "T-06",
  "title": "frontend service: type + getIPDomains() + mock implementation",
  "layer": "frontend",
  "files": ["frontend/src/services/dnsStatisticsService.ts"],
  "instruction": "1) เพิ่ม `export interface DNSIPDomain extends DNSDomainStat { lastSeen: string }` และ `export interface DNSIPDomains { ... }` ให้ตรงกับ Go DTO ทุกฟิลด์ พร้อมคอมเมนต์อธิบาย denominator เหมือนสไตล์ไฟล์เดิม. 2) เพิ่ม method `getIPDomains: async (ip: string, window: StatsWindow = \"1h\"): Promise<DNSIPDomains>` — real path: fetch `${API_BASE_URL}/statistics/dns/ip?ip=${encodeURIComponent(ip)}&window=${window}`; ถ้า response.status === 400 ให้ throw error ที่ข้อความสื่อว่า 'IP ไม่ถูกต้อง' (หน้าเพจจะเอาไปแสดงเป็น inline message ไม่ใช่หน้าแดง). 3) mock path (IS_MOCK_MODE): สร้าง reverse map จาก `mockDomainIPs` ที่มีอยู่แล้ว (:249-265) — IP 64.233.166.127 ถูกใช้ทั้ง www.youtube.com และ googlevideo.com จึงเป็นเคสทดสอบ shared IP ที่ดีอยู่แล้ว — คำนวณแถวโดย reuse mockDomainTotals/mockDomainIpRows/mockPairs และ mockWindowScale(window) ให้ค่าออกมาสอดคล้องกับ getDNSStatistics (count/percent เทียบ totalQueries ทั้ง window เหมือนกัน), bytesPercent เทียบผลรวมของแถวที่ match, series ใช้ mockBandwidthSeries, hostname จาก mockHostnames, ipsTruncated/truncated=false, accuracy 'near-exact'. IP ที่ไม่มีใน mock -> domains: [] (ไม่ throw).",
  "acceptance": ["yarn build ผ่าน (tsc -b)", "type ตรงกับ Go DTO ทุกฟิลด์", "mock mode ค้น 64.233.166.127 แล้วได้ 2 โดเมน"],
  "depends_on": ["T-02"]
}
```

```json
{
  "task_id": "T-07",
  "title": "frontend lib: ตัวตรวจสถานะข้อความ filter (domain / ip-partial / ip)",
  "layer": "frontend",
  "files": ["frontend/src/lib/ipQuery.ts"],
  "instruction": "ไฟล์ใหม่ (สไตล์เดียวกับ src/lib/statsWindow.ts — pure functions ไม่มี React). export `type IpQueryKind = \"domain\" | \"ip-partial\" | \"ip\"` และ `export function classifyIpQuery(raw: string): { kind: IpQueryKind; ip: string }` โดย ip = ค่า normalize (trim + lowercase) เมื่อ kind === \"ip\" ไม่งั้นเป็น \"\". กติกา: (ก) IPv4 สมบูรณ์ = 4 กลุ่มคั่นด้วยจุด แต่ละกลุ่มเป็นเลข 0-255 และห้าม leading zero ('01') เพราะ netip.ParseAddr ฝั่ง Go ก็ปฏิเสธ — ต้องตรงกันไม่งั้นผู้ใช้จะเจอ 400. (ข) IPv6 สมบูรณ์ = มีเฉพาะ [0-9a-f:] (บวกท้ายแบบ IPv4-mapped ได้) และผ่านการตรวจจำนวนกลุ่ม/`::` แบบมาตรฐาน (เขียน validator สั้น ๆ เอง ห้ามเพิ่ม dependency). (ค) ip-partial = ประกอบด้วยเฉพาะ [0-9.] และมีอย่างน้อย 1 ตัวเลข (v4 ที่ยังไม่ครบ) หรือมี ':' และประกอบด้วยเฉพาะ [0-9a-f:.] (v6 ที่ยังไม่ครบ) แต่ไม่ผ่านข้อ (ก)/(ข). (ง) นอกนั้นเป็น domain. ต้องเขียนคอมเมนต์กำกับว่าฝั่ง backend validate ซ้ำด้วย netip.ParseAddr เสมอ — ตัวนี้เป็นแค่ UX guard ไม่ใช่ security boundary.",
  "acceptance": [
    "yarn build + yarn lint ผ่าน",
    "classifyIpQuery: '192.168.1.10' -> ip, '192.168.' -> ip-partial, '192.168.1.256' -> ip-partial(ไม่ใช่ ip), '2001:db8::1' -> ip, 'youtube' -> domain, '' -> domain"
  ],
  "depends_on": []
}
```

```json
{
  "task_id": "T-08",
  "title": "DomainStatsTable: รับ query จากภายนอกได้ (controlled) + slot สำหรับข้อความใบ้/แบนเนอร์",
  "layer": "frontend",
  "files": ["frontend/src/components/statistics/DnsStatsShared.tsx"],
  "instruction": "แก้ `DomainStatsTable` (:144-225) ให้รับ props ใหม่แบบ optional ทั้งหมด เพื่อไม่กระทบผู้เรียกเดิมทั้ง 2 จุด (pages/StatisticsDns.tsx:158, pages/StatisticsDnsClient.tsx:165): `query?: string`, `onQueryChange?: (v: string) => void` (ถ้าไม่ส่งมาให้ใช้ useState ภายในเหมือนเดิม — uncontrolled), `placeholder?: string`, `filterDisabled?: boolean` (โหมด IP: ปิดการกรองข้อความภายในเพราะ rows ถูกกรองมาจาก backend แล้ว), `hint?: React.ReactNode` (แสดงใต้ช่องค้นหา), `banner?: React.ReactNode` (แสดงเหนือตาราง), `footerNote?: React.ReactNode` (แทนบรรทัด 'แสดง N จาก M' เมื่อส่งมา). เมื่อ filterDisabled=true ให้ข้าม useTextFilter (ใช้ rows ตรง ๆ) แต่ยังคง useSortableRows ไว้. เปลี่ยน placeholder เริ่มต้นเป็น 'ค้นหาโดเมน, ประเภท หรือใส่ IP...'. ห้ามเปลี่ยนคอลัมน์/DOM ส่วนอื่น ห้ามใส่สี hardcode (ใช้ text-muted-foreground / text-warning ตามที่มีอยู่) ห้ามใช้ shadow-*/backdrop-blur-*. type ของ rows ให้กว้างขึ้นเป็น `DNSDomainStat[]` เหมือนเดิม (DNSIPDomain extends DNSDomainStat จึงส่งเข้าได้อยู่แล้ว).",
  "acceptance": [
    "yarn build + yarn lint ผ่าน",
    "หน้า StatisticsDnsClient (ผู้เรียกที่ไม่ส่ง prop ใหม่) render เหมือนเดิมทุกอย่าง",
    "ไม่มี hardcoded palette class / shadow / backdrop-blur เพิ่มเข้ามา"
  ],
  "depends_on": []
}
```

```json
{
  "task_id": "T-09",
  "title": "หน้า Statistics → DNS: โหมด IP filter (URL param ?ip=)",
  "layer": "frontend",
  "files": ["frontend/src/pages/StatisticsDns.tsx"],
  "instruction": "ให้หน้านี้เป็นเจ้าของ state ของช่องค้นหาตาราง Top Domains: (1) เก็บข้อความ filter ใน state + sync กับ URL param `ip` ผ่าน useSearchParams แบบ { replace: true } (ยึด pattern useStatsWindow ใน DnsStatsShared.tsx:47-69) — เข้าหน้าโดยมี ?ip=... ต้องเข้าโหมด IP ทันที และ refresh หน้าแล้วยังอยู่โหมดเดิม. (2) ใช้ classifyIpQuery (T-07): kind='ip' -> เรียก dnsStatisticsService.getIPDomains(ip, window) โดย debounce ~300ms และ refresh ซ้ำทุก 10 วินาทีตาม REFRESH_INTERVAL_MS เดิม + โหลดใหม่เมื่อเปลี่ยน window; ต้องกันผลลัพธ์ค้าง (stale guard แบบเดียวกับ StatisticsDnsDomain.tsx:42-66); error จาก 400 ให้แสดงเป็นข้อความ inline ในการ์ด ไม่ใช่ทั้งหน้า. kind อื่น -> ล้าง ipData แล้วส่ง query ให้ DomainStatsTable กรองแบบเดิม. (3) เมื่ออยู่โหมด IP: หัวการ์ดเปลี่ยนเป็น 'โดเมนที่ resolve ไปยัง <ip>' (font-mono ที่ IP) + Badge บอกโหมด + ปุ่มล้าง; rows = ipData.domains; filterDisabled=true; footerNote = 'พบ N โดเมน · % Vol เทียบเฉพาะกลุ่มโดเมนที่ใช้ IP นี้ (M รวม)'. (4) banner: ถ้า ipData.shared -> กล่องเตือนสไตล์เดียวกับ DnsStatsTruncatedWarning (border-warning/20 bg-warning/10 text-warning + TriangleAlert) ข้อความ 'IP นี้ถูกใช้ร่วมกัน N โดเมน — ปริมาณข้อมูลของแต่ละโดเมนถูกนับซ้ำจาก IP เดียวกัน'; ถ้า ipData.ipsTruncated -> เตือนว่า index โดเมน→IP เต็ม ข้อมูลอาจไม่ครบ. (5) empty state (domains ว่าง): บอกว่าไม่พบโดเมนที่ resolve ไป IP นี้ (อาจหมดอายุ TTL หรือทราฟฟิกไม่ได้ผ่าน DNS ของอุปกรณ์นี้) พร้อมลิงก์ทางเลือก 2 ปุ่ม (Button asChild + NavLink): ดูเป็น client -> /statistics/dns/client/<ip>?window=..., ดูทราฟฟิกของ IP นี้ -> /statistics/traffic/host/<ip>?window=... (2 route นี้มีอยู่จริงแล้ว App.tsx:159,162). (6) hint ใต้ช่องค้นหาเมื่อ kind='ip-partial': 'พิมพ์ IP ให้ครบเพื่อค้นหาโดเมนที่ resolve ไปยัง IP นี้' และห้ามล้างตารางระหว่างพิมพ์. (7) การ์ดสถิติ 4 ใบด้านบน, ตาราง Top Source Hosts, DnsStatsPrivacyNote — คงเดิมทั้งหมด ห้ามแตะ. ห้ามใช้สี hardcode/shadow/backdrop-blur; รองรับ dark/light ผ่าน semantic variables.",
  "acceptance": [
    "yarn build + yarn lint ผ่าน",
    "พิมพ์ IP ครบ -> ตาราง Top Domains เปลี่ยนเป็นผลจาก endpoint ใหม่ และ URL มี ?ip=",
    "ลบข้อความออก -> กลับสู่ตารางเดิมและ ?ip= หายจาก URL",
    "โหมด IP ไม่กระทบตาราง Top Source Hosts และการ์ดสถิติด้านบน"
  ],
  "depends_on": ["T-06", "T-07", "T-08"]
}
```

```json
{
  "task_id": "T-10",
  "title": "cross-link: คลิกแถวในตาราง 'IP ที่ได้จากการ resolve' -> เปิดโหมด IP filter",
  "layer": "frontend",
  "files": ["frontend/src/components/statistics/DnsStatsShared.tsx", "frontend/src/pages/StatisticsDnsDomain.tsx"],
  "instruction": "เพิ่ม prop optional `onRowClick?: (ip: string) => void` ให้ `DomainIpTable` (DnsStatsShared.tsx:314-375) โดยใช้รูปแบบเดียวกับ DomainStatsTable/ClientStatsTable เป๊ะ (className cursor-pointer hover:bg-muted/50 เมื่อมี handler, title ภาษาไทยว่า 'คลิกเพื่อดูว่ามีโดเมนอื่นใช้ IP นี้อีกไหม'; ไม่ส่ง handler = พฤติกรรมเดิม). ที่ StatisticsDnsDomain.tsx:169 ส่ง onRowClick={(ip) => navigate(`/statistics/dns?window=${window_}&ip=${encodeURIComponent(ip)}`)}. ห้ามใส่ปุ่มซ้อนในเซลล์ (จะกลายเป็น nested click target — บทเรียนจาก statistics-dns-host-domain-label-plan.md §0).",
  "acceptance": ["yarn build + yarn lint ผ่าน", "คลิกแถว IP ที่มี badge shared แล้วไปหน้า DNS โหมด IP และเห็นโดเมนมากกว่า 1 รายการ"],
  "depends_on": ["T-09"]
}
```

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/statistics/dns/ip?ip=<addr>&window=<7 ค่า>` | `authRoute` (ทุก role ที่ล็อกอิน) — **เส้นใหม่** | คืนโดเมนทั้งหมดที่ index จำได้ว่า resolve ไปยัง IP นี้ + ปริมาณข้อมูล; `ip` ต้องผ่าน `netip.ParseAddr` ไม่งั้น 400; `window` เพี้ยน = fallback `1h`; GET จึงไม่ถูก `DisableEditMiddleware` บล็อก (ถูกต้องแล้ว — เป็นการอ่านล้วน) |
| GET | `/api/statistics/dns`, `/dns/domain`, `/dns/client` | เดิม | **ไม่เปลี่ยน shape ใด ๆ** ในแผนนี้ |

## 5. ข้อควรระวัง

1. **`Snapshot()` ตอบคำถามนี้ไม่ได้** — มันยุบเป็น ip→domain เดียว (last-writer-wins) ถ้า developer เผลอใช้ จะได้โดเมนเดียวเสมอ ซึ่งขัดกับหัวใจของคำขอ (IP ซ้ำหลายโดเมน) → ต้องใช้ `DomainsForIP` จาก T-01 เท่านั้น
2. **lock ordering** — `s.dns.mu` (ring) ห้ามถูกถือขณะเรียก `domainIPs` / `traffic` / `reverseCache` (แต่ละตัวมี mutex ของตัวเอง) ถ้าผิดกติกานี้จะเกิด lock inversion กับ hot path ของ `WatchDNSLog`; ทุก method ในไฟล์ `statistics_dns.go` ทำ `RUnlock()` ก่อนเสมอ — ทำตามให้เหมือน
3. **hot path ต้องไม่ช้าลง** — `dnsDomainIPs.Put` ถูกเรียกทุก DNS answer ที่ผ่านเครื่อง การเปลี่ยน `ipRefs` เป็น set ต้องยังเป็น O(1) และ **ห้าม** ทำ full scan ใต้ lock ในเส้นทางใด ๆ ที่ผู้ใช้กด refresh ได้ทุก 10 วินาที
4. **input validation คือจุดอ่อนไหว** (🔒) — `ip` ที่ผู้ใช้พิมพ์เดินทางจาก browser → query string → handler; ต้อง `netip.ParseAddr` + `addr.String()` ก่อนเข้า service เสมอ, ห้าม echo ค่าดิบกลับใน error body, ห้ามเอาไปประกอบ log/rule/ไฟล์ใด ๆ
5. **ห้ามให้ mapping นี้ไหลเข้าไปตัดสินใจอะไร** — domain↔IP มาจาก dnsmasq answer log ที่ LAN client ปั่นได้ (poison) เป็น display-only เท่านั้น ห้ามใช้กับ firewall/policy/routing/QoS (ย้ำในคอมเมนต์ทั้ง DTO และ handler)
6. **debounce + stale guard ฝั่ง frontend** — ถ้ายิงทุก keystroke จะได้ทั้งภาระที่ backend และผลลัพธ์สลับลำดับ (race) ต้อง debounce ~300ms และมี stale guard แบบเดียวกับหน้า drill-down เดิม
7. **ห้ามล้างตารางเป็นว่างระหว่างผู้ใช้พิมพ์ IP** — สถานะ `ip-partial` ต้องคงตารางเดิมไว้ ไม่งั้นผู้ใช้จะเห็นตารางกะพริบว่างทุกครั้งที่พิมพ์ตัวเลขตัวแรก
8. **กติกา leading zero ต้องตรงกับ Go** — `netip.ParseAddr` ปฏิเสธ `192.168.01.1` ถ้า frontend มองว่าเป็น IP สมบูรณ์แล้วยิงไป จะได้ 400 โดยไม่จำเป็น (T-07 ระบุไว้แล้ว)
9. **openapi 2 ไฟล์ต้องเท่ากันทุก byte** และห้ามแก้ `backend/internal/api/dist/openapi.yaml` (build output จาก `build.sh`)
10. **แถวที่ `count == 0` ต้องไม่หาย** — `rankTopDomains` ตัดทิ้ง ถ้า developer ใช้ helper นั้น โดเมนที่ยังจำ mapping ไว้แต่ไม่มี query ในหน้าต่างเวลาจะหายไป ทั้งที่เป็นคำตอบที่ถูกต้องของคำถาม "IP นี้เป็นของโดเมนอะไร"
11. **RAM/privacy** — ไม่มีการ persist อะไรเพิ่มลง SQLite (ตาม tech_stack_design.md §8) และเมื่อผู้ใช้ปิด query logging `Clear()` ต้องล้าง reverse index ใหม่ด้วย (T-01 ครอบคลุมแล้วผ่านการเคลียร์ `ipDomains`)
12. **ไม่ต้องทำ** — ไม่มี kernel capability ใหม่ (ไม่แตะ `interfaces.go`/`real_*.go`/`mock.go`), ไม่มี DB migration, ไม่ต้อง `InitApplyConfig()` (ไม่มี state ตอน boot), ไม่ต้องแก้ `install.sh`/Polkit/sudoers, ไม่ต้องแก้ README Feature Status (Statistics → DNS นับเป็น Completed อยู่แล้ว)

## 6. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ให้ ai-qa ทดสอบครั้งเดียวหลังครบทุก Task)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... ผ่าน และ go test ./... ผ่านทั้งหมด (ไม่มีเทสต์เดิมพัง โดยเฉพาะ dns_domain_ips_test.go / statistics_dns_test.go / handlers_test.go)",
    "cd frontend && yarn build (tsc -b) ผ่าน และ yarn lint ไม่มี error/warning ใหม่",
    "diff docs/openapi.yaml frontend/public/openapi.yaml — ไม่มีความต่าง และมี path /statistics/dns/ip + schema DNSIPDomains/DNSIPDomain ครบ",
    "mock mode (backend -mock=true + yarn dev พร้อม -allow-dev-cors): หน้า /statistics/dns พิมพ์ '64.233.166.127' ในช่อง filter ของ Top Domains -> ตารางเปลี่ยนเป็นโดเมนที่ใช้ IP นี้ ได้ 2 แถว (www.youtube.com, googlevideo.com) พร้อมแบนเนอร์เตือน shared IP และ URL มี ?ip=64.233.166.127",
    "พิมพ์ '142.250.80.46' (IP ของโดเมนเดียว) -> ได้ 1 แถว ไม่มีแบนเนอร์ shared",
    "พิมพ์ '10.9.9.9' (IP ที่ไม่มีใน index) -> ขึ้น empty state พร้อมปุ่มลิงก์ไป /statistics/dns/client/10.9.9.9 และ /statistics/traffic/host/10.9.9.9 (คลิกแล้วไปถึงหน้าจริง ไม่ 404)",
    "ระหว่างพิมพ์ '142.' หรือ '142.250' -> ตารางไม่ถูกล้างเป็นว่าง และมีข้อความใบ้ให้พิมพ์ IP ให้ครบ",
    "พิมพ์ชื่อโดเมน (เช่น 'youtube') -> ยังกรองแบบเดิมทุกประการ (ไม่มี network request ไป /statistics/dns/ip)",
    "ลบข้อความในช่อง filter -> กลับสู่ตาราง Top Domains เดิม และ ?ip= หายจาก URL; กด Back ของ browser ไม่ติดกับดัก history (ใช้ replace: true)",
    "เปลี่ยน window (7 ปุ่ม) ขณะอยู่โหมด IP -> ข้อมูลโหลดใหม่ตาม window และยังอยู่ในโหมด IP",
    "เข้าหน้า /statistics/dns?window=1h&ip=64.233.166.127 ตรง ๆ (refresh) -> เข้าโหมด IP ทันที",
    "หน้า domain drill-down: คลิกแถวในตาราง 'IP ที่ได้จากการ resolve' ที่มี badge shared -> ไปหน้า DNS โหมด IP และเห็นโดเมนที่ใช้ IP นั้นครบทุกโดเมน",
    "คลิกแถวโดเมนในโหมด IP -> เข้าหน้า /statistics/dns/domain/<domain> ได้ตามปกติ",
    "ตาราง Top Source Hosts, การ์ดสถิติ 4 ใบ, หน้า /statistics/dns/client/:client และหน้า Statistics -> Traffic ทั้งหมด แสดงผลเหมือนก่อนแก้ (ไม่มี regression)",
    "curl ตรง: GET /api/statistics/dns/ip โดยไม่ส่ง ip -> 400; ip='1.2.3' -> 400; ip='192.168.1.1; ls' -> 400 และ body ไม่มีสตริงนั้นสะท้อนกลับ; ip='2001:DB8::1' -> 200 และ field ip เป็น '2001:db8::1'; window='9h' -> 200 window='1h'",
    "เรียก endpoint ใหม่โดยไม่ล็อกอิน -> 401",
    "ปิด query logging (DNS Server > Settings) แล้วเรียก endpoint ใหม่ -> enabled=false, domains เป็น [] และไม่มีข้อมูล mapping หลงเหลือ",
    "grep ทั้ง repo: ไม่มี exec.Command ใหม่, ไม่มี dependency ใหม่ใน go.mod/package.json, ไม่มี class shadow-*/backdrop-blur-* หรือสี palette แบบ hardcode ใน diff ฝั่ง frontend",
    "diff ทั้งหมดจำกัดอยู่ในไฟล์ตาม task list เท่านั้น (11 ไฟล์แก้ + 1 ไฟล์ใหม่ frontend/src/lib/ipQuery.ts) — ไม่มีการแตะ kernel/, db/, install.sh, main.go"
  ]
}
```
