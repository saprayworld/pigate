# Statistics → DNS: คอลัมน์ "Host" แสดง domain/hostname บรรทัดบน + IP บรรทัดล่าง (ให้เหมือนหน้า Traffic)

> เอกสารแผนงานสำหรับคำขอเพิ่มเติมของเจ้าของ repo บน branch `feat/statistics-dns-page` (PR 127 ยังไม่ merge)
>
> วันที่เขียน: 2026-08-06 · อ้างอิงโค้ด: `feat/statistics-dns-page` @ `3c7be8d`
> ผู้เขียน: ai-tech-lead (สำรวจโค้ดจริงก่อนวางแผน — ไม่ได้เดาชื่อไฟล์/ฟิลด์)
>
> **ขอบเขต**: เฉพาะคอลัมน์ `Host` ของ `ClientStatsTable` ในหน้า Statistics → DNS (overview + domain drill-down)
> และการเพิ่มฟิลด์ `domain` ให้ DTO `DNSClientStat` เท่านั้น
> **ห้ามแตะ** หน้า Statistics → Traffic (`pages/StatisticsTraffic.tsx`, `pages/StatisticsTrafficHost.tsx`,
> `components/statistics/TopHostsShareCard.tsx`) — งานนี้ต้อง "ยืม" ของจากหน้า Traffic โดย DOM/พฤติกรรมของหน้า Traffic
> ต้องเหมือนเดิมทุกตัวอักษร

## 0. คำขอต้นทาง (เจ้าของ repo)

ปรับคอลัมน์ "Host" ในตารางหน้า Statistics → DNS ให้เหมือนหน้า Statistics → Traffic:

1. บรรทัดบน = Hostname หรือ Domain
2. ถ้ามี domain ให้ domain ชนะ hostname
3. บรรทัดล่าง = IP

**สิ่งที่ไม่ได้ขอ (จะไม่ทำในแผนนี้ — กัน over-engineering)**

- Badge `LAN` / `Internet` ที่ `HostLabel` มีอยู่ (แถวในตารางนี้เป็น LAN client ทั้งหมด badge จะเป็น "LAN" แทบทุกแถว — ไม่มีข้อมูลเพิ่ม กินความกว้างเปล่า)
- ปุ่ม/ลิงก์คลิกได้ในเซลล์ (ทั้งแถวคลิกได้อยู่แล้วผ่าน `onRowClick` — ถ้าใส่ `<button>` ซ้อนเข้าไปจะกลายเป็น nested click target)
- หัวข้อ/หัวเรื่องหน้า client drill-down (`StatisticsDnsClient.tsx` ใช้ `DNSClientDrilldown.hostname`) — คนละที่ ไม่ได้ขอ

## 1. สถานะปัจจุบัน (สำรวจแล้ว)

| ส่วน | สถานะจริง | ไฟล์:บรรทัด (~) |
|---|---|---|
| เซลล์ Host หน้า DNS | render เอง inline: `c.hostname \|\| c.ip` + IP ต่อท้ายแบบ `ml-1.5` (บรรทัดเดียวกัน ไม่ใช่ 2 บรรทัด) + เคสพิเศษ `ip === "unknown"` → "ไม่ทราบต้นทาง" | `frontend/src/components/statistics/DnsStatsShared.tsx:271-282` |
| component ที่หน้า Traffic ใช้ | `HostLabel({host: TopHost, onClick?})` — มี 2 branch (มี domain / ไม่มี domain) + `<Badge>` LAN/Internet | `frontend/src/components/statistics/HostCells.tsx:40-105` |
| ผู้ใช้ `HostLabel` ปัจจุบัน | `pages/StatisticsTraffic.tsx:138`, `pages/StatisticsOverview.tsx:95`, `components/statistics/TopHostsShareCard.tsx:75` (ทั้ง 3 จุดห้ามเปลี่ยนผลลัพธ์) | — |
| type `DNSClientStat` (TS) | มี `ip`, `hostname`, `count`, `percent`, `domains` (จำนวนโดเมน — คนละเรื่องกับ domain เดี่ยว), `bytes*` — **ไม่มี `domain` และไม่มี `private`** | `frontend/src/services/dnsStatisticsService.ts:80-108` |
| DTO `DNSClientStat` (Go) | เหมือนกันเป๊ะ ไม่มี `Domain` | `backend/internal/model/statistics.go:262-295` |
| แหล่งที่มาของ `TopHost.Domain` | `s.dns.reverseCache.LookupMany(ips)` → `buildTopHosts(..., ipDomain, ...)` | `service/statistics_traffic.go:89,239` · `service/statistics.go:240-259` |
| `dnsReverseCache` | index **ip → domain ล่าสุดที่ DNS ตอบ** (last-writer-wins) มี `sync.RWMutex` ของตัวเอง, TTL + cap ปรับได้ live, `LookupMany` = ล็อกก้อนเดียวต่อ batch, expired เก็บกวาดแบบ lazy | `service/dns_reverse_cache.go:22-140` |
| ความสัมพันธ์กับ `dns_domain_ips.go` | **คนละ index กัน** — reverseCache = ip→domain (สำหรับ enrich แถว), domainIPs = domain→[]ip (forward, ใช้คำนวณ volume ใน PR 127) ทั้งคู่กินจาก `model.DNSLogAnswer` event เดียวกันและแชร์ค่า TTL ผ่าน `SetReverseCacheLimits` | `service/dns_query_stats.go:118-126,220-240` |
| จุดที่สร้างแถว `DNSClientStat` | `rankDNSClients()` (ใส่แค่ IP/Hostname/Count/Percent) แล้ว decorate ต่อใน 2 ที่ | `service/dns_query_stats.go:355-379` · `service/statistics_dns.go:162-175, 339-355` |
| Hostname เมื่อไม่รู้จัก | `hostnameFor()` **คืนค่า `ip` เอง** (ไม่ใช่ค่าว่าง) → คอมเมนต์ใน DTO ที่เขียนว่า "empty when unknown" ไม่ตรงกับพฤติกรรมจริง | `service/traffic_stats.go:1599-1610` |
| openapi | schema `DNSClientStat` อยู่บรรทัดเดียวกันทั้ง 2 ไฟล์ (สำเนาตรงกัน) | `docs/openapi.yaml:4827-…` · `frontend/public/openapi.yaml:4827-…` |
| frontend test | ไม่มี test runner ใน `frontend/package.json` (มีแค่ dev/build/lint/preview) → เกณฑ์ฝั่ง frontend คือ `yarn build` + `yarn lint` | `frontend/package.json:6-11` |

### 1.1 บั๊กเล็กที่เจอระหว่างสำรวจ (จะหายไปเองเมื่อทำแผนนี้)

`DnsStatsShared.tsx:276-279` render `{c.hostname || c.ip}` แล้วต่อด้วย `{c.hostname && <span>{c.ip}</span>}`
แต่ backend จริง `hostnameFor()` คืน **`ip`** เมื่อไม่รู้จัก hostname → เงื่อนไข `c.hostname` เป็นจริงเสมอ →
เครื่องที่ไม่มี DHCP lease จะแสดง **IP ซ้ำสองครั้งในแถวเดียว** (`192.168.1.7  192.168.1.7`)
`HostLabel` ฝั่ง Traffic กันเคสนี้ไว้แล้วด้วย `host.hostname !== host.ip` (`HostCells.tsx:90`) —
การย้ายมาใช้ logic เดียวกันจึงแก้บั๊กนี้ไปในตัว (อย่าเขียนเงื่อนไขเดิมกลับเข้าไป)

## 2. การประเมิน: คุ้มไหมที่จะเพิ่ม `domain` ใน `DNSClientStat` — **คุ้ม ทำ**

**ต้นทุนฝั่ง runtime = 1 lock acquisition ต่อ request** (`reverseCache.LookupMany` เหนือรายชื่อ IP ที่ผ่านการตัด
top-N แล้ว ≤ `dnsStatsTopN` = 50 รายการ — `dns_query_stats.go:52`) ไม่มีการเรียกฟังก์ชันหนักเพิ่ม
(ไม่แตะ conntrack, ไม่เพิ่ม `GetTrafficBreakdown*`, ไม่เพิ่ม `hostLookup`) และเป็นแค่ map lookup ใน RAM
เทียบกับสิ่งที่ endpoint นี้ทำอยู่แล้ว (สแกน ring ทุก bucket + join conntrack) ถือว่าไม่มีนัยสำคัญ

**ข้อจำกัดด้าน locking (ต้องทำตามเป๊ะ — หลักการเดียวกับ PR 127 §Caution 3)**

- `reverseCache` มี mutex ของตัวเอง แยกจาก `s.dns.mu` (ring lock) → **ห้ามเรียก `LookupMany` ขณะถือ `s.dns.mu`**
  ต้องเรียกหลัง `s.dns.mu.RUnlock()` เท่านั้น (ตำแหน่งเดียวกับที่โค้ดปัจจุบันเรียก `domainIPs` / traffic breakdown)
- **1 request = อย่างมาก 1 ครั้ง `LookupMany`** (batch เดียว) ห้ามเรียก `Lookup` ทีละแถว

**ค่าที่ผู้ใช้จะเห็นจริง (ต้องเข้าใจตรงกัน ไม่ใช่บั๊ก)**: reverseCache ถูกเติมจาก DNS **answer** →
IP ที่มี domain ส่วนใหญ่คือ IP ปลายทางบนอินเทอร์เน็ต ส่วนแถวในตารางนี้คือ **client IP ใน LAN** ที่ยิง query
ดังนั้นโดยปกติ `domain` จะว่าง และ UI จะ fallback ไปแสดง hostname เหมือนเดิม — จะมีค่าเฉพาะเมื่อ DNS ตอบชื่อ
ที่ชี้กลับมายัง IP ใน LAN (เช่น local zone ของ DNS Server page เช่น `nas.home.lan → 192.168.1.50`)
**นี่คือพฤติกรรมเดียวกับตาราง "Top Source Hosts" ของหน้า Traffic เป๊ะ ๆ** ซึ่งตรงกับคำขอ "ให้เหมือนหน้า Traffic"

**ไม่เพิ่มฟิลด์ `private`** — ใช้เฉพาะ Badge LAN/Internet ที่ไม่ได้ขอ (ดู §0) การเพิ่มจะลาก DTO/openapi/mock
บานปลายโดยไม่มีผู้ใช้

### 2.1 ทางเลือกฝั่ง frontend (เลือกทางที่ 2)

| ทาง | วิธี | ข้อดี | ข้อเสีย |
|---|---|---|---|
| 1 | เรียก `HostLabel` ตรง ๆ จาก `ClientStatsTable` | ไม่ต้องแตะ `HostCells.tsx` | type ไม่ผ่าน (`HostLabel` รับ `TopHost` ซึ่งต้องมี `mac/bytes/percent/bytesUp/bytesDown/private` ที่ `DNSClientStat` ไม่มี) + ได้ Badge ที่ไม่ได้ขอติดมาด้วย |
| **2 (เลือก)** | แยกส่วนใน `HostLabel` ที่เป็น "2 บรรทัด domain/hostname + ip" ออกเป็น `HostNameLines` (prop เป็น structural subset `{ip, hostname, domain}`) แล้วให้ `HostLabel` ประกอบ `HostNameLines` + `<Badge>` เหมือนเดิม ส่วน `ClientStatsTable` เรียก `HostNameLines` ล้วน ๆ | ได้ตรงตามที่ขอพอดี (ไม่มี badge), หน้า Traffic ได้ DOM เดิมเป๊ะ, logic เดียวไม่ drift 2 ที่ | ต้องแตะไฟล์ที่หน้า Traffic ใช้ → ต้องเป็น **pure refactor** ห้ามเปลี่ยน markup/class ใด ๆ |
| 3 | copy markup ไปวางใน `DnsStatsShared.tsx` | ไม่แตะไฟล์ของ Traffic เลย | logic ซ้ำ 2 ที่ drift แน่นอน (เคย move ไฟล์นี้ออกมาเพื่อเลี่ยงเรื่องนี้อยู่แล้ว — `HostCells.tsx:7-12`) |

## 3. Task list

```json
[
  {
    "task_id": "T-01",
    "title": "เพิ่มฟิลด์ Domain ให้ DTO model.DNSClientStat",
    "layer": "model",
    "files": ["backend/internal/model/statistics.go"],
    "instruction": "ใน struct model.DNSClientStat (~บรรทัด 262-295) เพิ่มฟิลด์ `Domain string `json:\"domain\"`` ต่อจาก Hostname (ไม่ใส่ omitempty — ต้องมี key เสมอ ค่าว่างเมื่อไม่รู้จัก เหมือน TopHost.Domain ที่ statistics.go:46-50) พร้อมคอมเมนต์กำกับให้ครบ 4 ประเด็น: (1) เป็นชื่อโดเมนที่ dnsmasq ตอบล่าสุดสำหรับ IP นี้ มาจาก dnsReverseCache ตัวเดียวกับ TopHost.Domain (2) display-only ว่างเมื่อไม่รู้จัก/หมด TTL (3) ห้ามใช้กับการสร้าง firewall rule / policy matching / routing / QoS เพราะ LAN client poison ได้ (4) ปกติจะว่างสำหรับแถวในตารางนี้เพราะเป็น client IP ใน LAN — จะมีค่าก็ต่อเมื่อ DNS ตอบชื่อที่ชี้กลับมายัง IP ใน LAN (local zone) พร้อมกันนี้ให้แก้คอมเมนต์เดิมของฟิลด์ Hostname ที่เขียนว่า 'empty when unknown' ให้ตรงกับพฤติกรรมจริงของ hostnameFor() (service/traffic_stats.go:1599-1610) ซึ่ง fallback เป็นค่า IP ไม่ใช่ค่าว่าง ห้ามแก้ logic ใด ๆ ใน task นี้ (แก้เฉพาะ struct + คอมเมนต์)",
    "acceptance": [
      "`cd backend && go build ./...` ผ่าน",
      "struct DNSClientStat มีฟิลด์ Domain พร้อม json tag `domain` และคอมเมนต์ครบ 4 ประเด็น",
      "คอมเมนต์ฟิลด์ Hostname ระบุพฤติกรรม fallback เป็น IP ตรงกับ hostnameFor()",
      "ไม่มีไฟล์อื่นถูกแก้"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "เติมค่า Domain ให้ทุกแถว DNSClientStat จาก reverseCache (1 LookupMany ต่อ request)",
    "layer": "service",
    "files": ["backend/internal/service/statistics_dns.go"],
    "instruction": "งานนี้เป็น sensitive review (แตะ locking discipline ของ statistics pipeline) — อ่านคอมเมนต์หัวไฟล์ statistics_dns.go บรรทัด 25-34 ก่อนเริ่ม แก้ 2 จุด: (A) GetDNSQueryStatistics — หลังได้ topClients จาก rankDNSClients (~บรรทัด 162) ให้รวบรวม IP ของ topClients ทุกแถวเป็น []string แล้วเรียก `s.dns.reverseCache.LookupMany(ips)` **หนึ่งครั้ง** จากนั้นในลูป decorate เดิม (~163-175) เติม `topClients[i].Domain = ipDomain[ip]` (B) GetDNSDomainClients — ทำแบบเดียวกันกับ slice `clients` หลัง rankDNSClients (~บรรทัด 339) และเติมในลูป decorate เดิม (~340-355) ข้อบังคับ: (1) ห้ามเรียก LookupMany ขณะถือ s.dns.mu — ทั้งสองจุดอยู่หลัง s.dns.mu.RUnlock() อยู่แล้ว ห้ามย้ายขึ้นไปก่อนหน้า (2) ห้ามเรียก reverseCache.Lookup ทีละแถว (3) ห้ามเพิ่มจำนวนครั้งการเรียก GetTrafficBreakdown*/hostLookup ที่มีอยู่ (ยังต้องอย่างละ 1 ครั้งต่อ request ตามกฎ T-04 ของแผน statistics-dns-page-revamp) (4) เก็บ batch จาก IP ของแถวที่ผ่าน top-N แล้วเท่านั้น ไม่ใช่ทุก client ใน window (5) ห้ามแก้ signature ของ rankDNSClients (6) ห้ามเพิ่ม log/print ใด ๆ — ข้อมูลนี้เป็น PII การใช้งานของคนในบ้าน (7) เคส client เป็นค่า 'unknown' (dnsUnknownClient) ต้องไม่พัง — LookupMany จะคืนค่าว่างให้เอง ห้ามใส่เงื่อนไขพิเศษเพิ่ม (8) ไม่ต้องแตะ GetDNSClientDomains (คืน DNSClientDrilldown ไม่ใช่ DNSClientStat) ใส่คอมเมนต์สั้น ๆ อธิบายว่าทำไม batch ถึงอยู่ตำแหน่งนี้ (locking) และอ้างเอกสารแผนนี้",
    "acceptance": [
      "`cd backend && go build ./... && go vet ./...` ผ่าน",
      "grep แล้วมี `reverseCache.LookupMany` ใน statistics_dns.go อย่างละ 1 ครั้งใน 2 ฟังก์ชัน และไม่มี `reverseCache.Lookup(` แบบทีละแถว",
      "จำนวนการเรียก GetTrafficBreakdown/GetTrafficBreakdownForDests/hostLookup ในแต่ละฟังก์ชันเท่าเดิม",
      "การเรียก LookupMany ทั้งสองจุดอยู่หลังบรรทัด s.dns.mu.RUnlock()",
      "ไม่มี log/fmt.Print ใหม่"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-03",
    "title": "unit test: Domain ถูกเติมจาก reverseCache และว่างเมื่อไม่รู้จัก",
    "layer": "service",
    "files": ["backend/internal/service/statistics_dns_test.go"],
    "instruction": "เพิ่ม test ใหม่ (อย่าแก้ test เดิม) ชื่อประมาณ TestDNSClientStat_DomainFromReverseCache โดยใช้ fixture เดิม seedDNSVolumeFixture(t) (บรรทัด 38-75) เป็นฐาน แล้วบันทึก answer event เพิ่มที่ map ชื่อโดเมนกลับมายัง IP ของ client ใน LAN เช่น `s.RecordDNSEvent(model.DNSLogEvent{Kind: model.DNSLogAnswer, Domain: \"nas.home.lan\", AnswerIP: \"192.168.1.10\"})` (RecordDNSEvent เขียนลง reverseCache ที่ dns_query_stats.go:118-126) ยืนยัน 3 ข้อ: (1) GetDNSQueryStatistics(\"1h\").TopClients แถวของ 192.168.1.10 มี Domain == \"nas.home.lan\" (2) แถวของ 192.168.1.11 ซึ่งไม่มี answer ชี้กลับมา มี Domain == \"\" (ห้าม fallback เป็น IP) (3) GetDNSDomainClients(\"1h\", \"b.example.com\").Clients ก็ได้ค่า Domain แบบเดียวกัน (ทั้งสอง client อยู่ในโดเมนนี้) หมายเหตุ: การเพิ่ม answer ที่ชี้ไป 192.168.1.10 จะทำให้ forward index มีโดเมน nas.home.lan เพิ่มมาด้วย — ห้ามแก้ค่า assert ของ test เดิมเพื่อให้ผ่าน ถ้าชนกันให้ใช้ StatisticsService ตัวใหม่แยกจาก fixture เดิมภายใน test นี้เท่านั้น",
    "acceptance": [
      "`cd backend && go test ./internal/service/... -run TestDNSClientStat_DomainFromReverseCache -race` ผ่าน",
      "`cd backend && go test ./... -race` ผ่านทั้งหมด (test เดิมไม่ถูกแก้ค่า assert)",
      "test ครอบคลุมทั้งเคสมี domain, ไม่มี domain (ค่าว่าง ไม่ใช่ IP) และ endpoint domain drill-down"
    ],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-04",
    "title": "openapi: เพิ่ม property domain ให้ schema DNSClientStat (2 ไฟล์ ต้องตรงกันเป๊ะ)",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "แก้ทั้ง 2 ไฟล์ให้เนื้อหาตรงกัน byte-for-byte (ปัจจุบัน schema DNSClientStat อยู่ที่บรรทัด 4827 ของทั้งคู่): (1) เพิ่ม `domain` เข้าไปใน list `required` ต่อจาก hostname (backend ส่ง key นี้เสมอ ค่าว่างเมื่อไม่รู้จัก) (2) เพิ่ม property `domain: {type: string, description: ...}` ต่อจาก hostname โดย description ต้องระบุว่าเป็นชื่อโดเมนที่ dnsmasq ตอบล่าสุดสำหรับ IP นี้ (แหล่งเดียวกับ TopHost.domain), display-only, ห้ามใช้ตัดสินใจด้าน firewall/policy/routing, และปกติจะว่างเพราะแถวนี้เป็น client IP ใน LAN — มีค่าเฉพาะเมื่อ DNS ตอบชื่อที่ชี้กลับมายัง IP ใน LAN (local zone) พร้อม example เป็นชื่อโดเมนภายในเช่น nas.home.lan (3) ปรับ description ของ property `hostname` ให้ตรงพฤติกรรมจริง (fallback เป็นค่า IP เมื่อไม่มี DHCP lease/reservation ไม่ใช่ค่าว่าง) ห้ามแก้ schema อื่น",
    "acceptance": [
      "`diff docs/openapi.yaml frontend/public/openapi.yaml` ไม่มีความต่าง",
      "schema DNSClientStat มี domain ทั้งใน required และ properties",
      "ไฟล์ยัง parse เป็น YAML ได้ (เปิดหน้า ApiDocs แล้วไม่ error)",
      "ไม่มี schema อื่นถูกแก้"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-05",
    "title": "frontend service: เพิ่ม domain ใน type DNSClientStat + เติมค่าใน mock mode",
    "layer": "frontend",
    "files": ["frontend/src/services/dnsStatisticsService.ts"],
    "instruction": "(1) เพิ่มฟิลด์ `domain: string` (required ไม่ใช่ optional — backend ส่งเสมอ) ต่อจาก hostname ใน interface DNSClientStat (~บรรทัด 80-108) พร้อมคอมเมนต์ที่สื่อความหมายเดียวกับ Go DTO ใน T-01 (มาจาก reverse lookup ip→domain ล่าสุดที่ DNS ตอบ, display-only, ห้ามใช้ตัดสินใจใด ๆ, ปกติว่างเพราะเป็น client IP ใน LAN) (2) เพิ่ม const map ระดับไฟล์ชื่อ mockClientDomains ถัดจาก mockHostnames (~บรรทัด 203-207) ที่มี **entry เดียว** (เช่น \"192.168.1.102\": \"smarttv.home.lan\") พร้อมคอมเมนต์ว่ามีไว้ให้ mock mode ได้ทดลอง path 'domain ชนะ hostname' ส่วน client อื่นตั้งใจปล่อยว่างเพื่อสะท้อนของจริงที่ client ใน LAN มักไม่มี reverse entry (3) เติม `domain: mockClientDomains[ip] ?? \"\"` ให้ทุกจุดที่ mock สร้าง object DNSClientStat — มี 2 จุด: getDNSStatistics (topClients ~บรรทัด 412-427) และ getDomainClients (clients ~บรรทัด 480-501) ข้อห้าม: ห้ามแก้ค่า hostname/ตรรกะ byte/percent ใด ๆ, ห้ามแตะ kernel/mock.go ฝั่ง backend (การเพิ่ม answer event ที่ชี้ไป IP ใน LAN จะไปโผล่ในตาราง Top Domains/volume ของ mock ด้วย — ผลข้างเคียงเกินขอบเขต), ห้ามเพิ่ม console.log",
    "acceptance": [
      "`cd frontend && yarn build` ผ่าน (tsc ไม่ฟ้องว่ามี object DNSClientStat ที่ขาด domain)",
      "interface DNSClientStat มี domain: string พร้อมคอมเมนต์",
      "ทั้ง 2 จุดที่สร้าง DNSClientStat ใน mock ใส่ค่า domain แล้ว",
      "ไม่มีการแก้ค่า hostname/bytes/percent เดิม และไม่มี console.log ใหม่"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-06",
    "title": "แยก HostNameLines ออกจาก HostLabel (pure refactor — DOM หน้า Traffic ต้องเหมือนเดิมเป๊ะ)",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/HostCells.tsx"],
    "instruction": "refactor ล้วน ห้ามเปลี่ยนผลลัพธ์ที่ render ของ HostLabel แม้แต่ class เดียว: (1) export component ใหม่ `HostNameLines({ host, onClick }: { host: { ip: string; hostname: string; domain: string }; onClick?: () => void })` — สังเกตว่า prop type เป็น structural subset ไม่ใช่ TopHost เพื่อให้ทั้ง TopHost และ DNSClientStat ส่งเข้ามาได้ (2) ย้าย markup ส่วน `<span className=\"min-w-0\">…</span>` ของทั้ง 2 branch ใน HostLabel (branch มี domain: บรรทัด 48-62, branch ไม่มี domain: บรรทัด 77-93) มาไว้ใน HostNameLines โดยคง class/attribute/ลำดับ/tooltip ทุกตัวอักษร กติกาเดิมยังอยู่: มี domain → domain บรรทัดบน + ip บรรทัดล่างเสมอ, ไม่มี domain → hostname บรรทัดบน + แสดง ip บรรทัดล่างเฉพาะเมื่อ hostname !== ip, มี onClick → บรรทัดบนเป็น <button> ไม่มี onClick → เป็น <span> (3) เพิ่ม fallback ป้องกันเคส hostname เป็นค่าว่าง: บรรทัดบนของ branch ไม่มี domain ให้ใช้ `host.hostname || host.ip` (backend จริง fallback เป็น IP อยู่แล้ว แต่ mock frontend ส่งค่าว่างได้) พร้อมคอมเมนต์อธิบาย — นี่คือความต่างเชิงพฤติกรรมจุดเดียวที่อนุญาต และมีผลเฉพาะเคสที่ปัจจุบัน render เป็นบรรทัดว่าง (4) ให้ HostLabel เรียก HostNameLines แล้วห่อด้วย <span className=\"flex min-w-0 items-center gap-2\"> + <Badge> เหมือนเดิมทั้ง 2 branch (จะยุบเหลือ branch เดียวก็ได้ถ้า DOM ออกมาเหมือนเดิมเป๊ะ) (5) HostLabel ยังต้อง export ด้วย signature เดิม (host: TopHost, onClick?) — ห้ามแก้ไฟล์ที่เรียกใช้ (StatisticsTraffic.tsx, StatisticsOverview.tsx, TopHostsShareCard.tsx) (6) อัปเดตคอมเมนต์หัวไฟล์ให้ครอบคลุม component ใหม่",
    "acceptance": [
      "`cd frontend && yarn build && yarn lint` ผ่าน",
      "HostCells.tsx export ทั้ง UpDownLine, HostLabel (signature เดิม) และ HostNameLines",
      "ไม่มีไฟล์อื่นถูกแก้ใน task นี้",
      "diff ของ markup ที่ HostLabel render ต่างจากเดิมเฉพาะ fallback hostname||ip เท่านั้น (class/tooltip/ลำดับ badge เหมือนเดิมทุกตัว)"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-07",
    "title": "ClientStatsTable: ใช้ HostNameLines ในคอลัมน์ Host + ให้ filter ค้น domain ได้",
    "layer": "frontend",
    "files": ["frontend/src/components/statistics/DnsStatsShared.tsx"],
    "instruction": "แก้เฉพาะ ClientStatsTable (~บรรทัด 228-301) ห้ามแตะ DomainStatsTable/DomainIpTable: (1) import { HostNameLines } from \"@/components/statistics/HostCells\" (2) แทนที่เนื้อในของ TableCell คอลัมน์ Host (~บรรทัด 271-282) ด้วย <HostNameLines host={c} /> โดย **ไม่ส่ง onClick** (ทั้งแถวคลิกได้อยู่แล้วผ่าน onRowClick — ห้ามสร้าง nested button) (3) คงเคสพิเศษ c.ip === \"unknown\" ที่แสดง <span className=\"text-muted-foreground\">ไม่ทราบต้นทาง</span> ไว้เหมือนเดิมทุกประการ (4) คง className ของ TableCell เดิมไว้ (max-w-[220px] truncate py-3 ...) และปรับ title tooltip ให้สะท้อนสิ่งที่แสดงจริง: ถ้ามี c.domain ให้ขึ้นต้นด้วย domain แล้วตามด้วย (ip) ถ้าไม่มีให้ใช้ hostname/ip แบบเดิม (5) เพิ่ม (c) => c.domain เข้าไปใน accessor list ของ useTextFilter (~บรรทัด 238) เพื่อให้ค้นหาสิ่งที่ตาเห็นได้ และปรับ placeholder ของ TrafficFilterInput (~บรรทัด 243) จาก \"ค้นหา IP, hostname...\" เป็นข้อความที่รวม domain ด้วย (6) **ห้ามเปลี่ยน sortKey ของหัวคอลัมน์ Host** — ยังเป็น \"hostname\" เพราะ useSortableRows รองรับแค่ keyof T ไม่มี custom accessor (ดู TrafficStatsShared.tsx:26-98) การทำ sort แบบ domain-first ต้องแก้ hook กลางซึ่งกระทบหน้า Traffic → นอกขอบเขต (7) ห้ามเพิ่ม Badge LAN/Internet (8) อัปเดตคอมเมนต์เหนือ ClientStatsTable ให้ระบุว่าคอลัมน์ Host ใช้ component ร่วมกับหน้า Traffic แล้ว และอ้างเอกสารแผนนี้",
    "acceptance": [
      "`cd frontend && yarn build && yarn lint` ผ่าน",
      "คอลัมน์ Host ของ ClientStatsTable render ผ่าน HostNameLines โดยไม่ส่ง onClick และไม่มี Badge",
      "เคส ip === \"unknown\" ยังแสดง 'ไม่ทราบต้นทาง' เหมือนเดิม",
      "useTextFilter ของตารางนี้มี accessor ครบ ip/hostname/domain และ placeholder สอดคล้อง",
      "sortKey ของหัวคอลัมน์ Host ยังเป็น \"hostname\" และไม่มีการแก้ไฟล์ TrafficStatsShared.tsx"
    ],
    "depends_on": ["T-05", "T-06"]
  }
]
```

## 4. ข้อควรระวังรวม (ให้ ai-developer อ่านก่อนทุก task)

1. **Privacy/PII** — ข้อมูลนี้คือพฤติกรรมการใช้อินเทอร์เน็ตของคนในบ้าน **ห้ามเพิ่ม log/print/console.log ทุกชนิด**
   ในทุก task และห้ามเขียนลง SQLite (ทุกอย่างต้องอยู่ใน RAM ตาม tech_stack_design.md §8)
2. **Trust boundary** — `reverseCache` ถูก poison ได้โดย LAN client ใด ๆ (แค่ query ชื่อที่ตัวเองคุม)
   ค่า `domain` จึงเป็น **display-only** เท่านั้น ห้ามนำไปใช้ใน firewall rule, policy matching, routing, QoS
   หรือเป็น key ตัดสินใจใด ๆ — ต้องมีคอมเมนต์กำกับทุกที่ที่ประกาศฟิลด์นี้ (T-01, T-04, T-05)
3. **Locking** — ห้ามถือ `s.dns.mu` ข้ามไปเรียก `reverseCache` / `domainIPs` / traffic breakdown (กฎเดิมจาก PR 127)
4. **ห้ามเพิ่ม cost ต่อ request** — จำนวนครั้งของ `GetTrafficBreakdown*`, `hostLookup`, `domainIPs.*`
   ต้องเท่าเดิม ส่วน `reverseCache.LookupMany` เพิ่มได้ไม่เกิน 1 ครั้ง/request
5. **หน้า Traffic ห้ามเปลี่ยนพฤติกรรม** — T-06 เป็น pure refactor; ถ้าเผลอเปลี่ยน class/badge/tooltip ของ
   `HostLabel` ถือว่าไม่ผ่าน
6. **git** — ทำงานต่อบน branch `feat/statistics-dns-page` (PR 127 เดิม) ห้าม push main และห้าม commit
   จนกว่าเจ้าของ repo จะสั่ง

## 5. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ทดสอบครั้งเดียวหลัง T-01…T-07 เสร็จครบ)

```json
{
  "final_acceptance": [
    "backend: `cd backend && go build ./... && go vet ./... && go test ./... -race` ผ่านทั้งหมด",
    "frontend: `cd frontend && yarn build && yarn lint` ผ่าน ไม่มี warning ใหม่",
    "`diff docs/openapi.yaml frontend/public/openapi.yaml` ไม่มีความต่าง และ schema DNSClientStat มี domain ทั้งใน required และ properties",
    "รัน backend `-mock=true -allow-dev-cors` + `yarn dev`: หน้า Statistics → DNS ตาราง 'Top Source Hosts' คอลัมน์ Host แสดง 2 บรรทัด — บรรทัดบนเป็น hostname (หรือ domain ถ้ามี) บรรทัดล่างเป็น IP ตัวเล็ก font-mono สี muted",
    "โหมด mock ของ frontend (IS_MOCK_MODE): แถวของ client ที่ mockClientDomains กำหนดไว้ แสดง 'domain' บรรทัดบน (ไม่ใช่ hostname) พิสูจน์ว่า domain ชนะ hostname ส่วนแถวอื่นยังแสดง hostname ตามเดิม",
    "แถวที่ไม่มีทั้ง hostname และ domain (backend จริงคืน hostname == ip) แสดง IP เพียงครั้งเดียว ไม่ซ้ำสองบรรทัด (แก้บั๊กใน §1.1)",
    "แถว ip === 'unknown' ยังแสดง 'ไม่ทราบต้นทาง' เหมือนเดิม และคลิกแล้วยังไปหน้า client drill-down ได้ตามเดิม",
    "คลิกที่แถว (รวมถึงคลิกตรงข้อความ Host) ยังนำทางไปหน้า drill-down ได้ 1 ครั้ง ไม่มี nested button / ไม่มี navigate ซ้อน",
    "ตารางเดียวกันบนหน้า domain drill-down (Statistics → DNS → เลือกโดเมน) แสดงผลรูปแบบเดียวกันเป๊ะกับหน้า overview",
    "ช่องค้นหาของตาราง 'Top Source Hosts' ค้นด้วย domain ที่แสดงอยู่แล้วเจอแถวนั้น และยังค้นด้วย ip/hostname ได้เหมือนเดิม",
    "หน้า Statistics → Traffic (Top Source Hosts / Top Destinations), Statistics (Overview) และการ์ด TopHostsShareCard แสดงผลเหมือนเดิมทุกประการ รวมถึง Badge LAN/Internet และการคลิกชื่อ host",
    "ตาราง DNS ตัวอื่น (Top Domains, Resolved IPs) ไม่เปลี่ยนแปลง ทั้งคอลัมน์และการเรียงลำดับ",
    "dark mode + light mode อ่านออกทั้งคู่ และไม่มี class shadow-*/backdrop-blur-* หรือสี palette ดิบ (เช่น text-emerald-500) เพิ่มเข้ามา",
    "grep ทั้ง diff แล้วไม่มี log/console.log/fmt.Print ใหม่ที่แตะค่า domain/hostname/ip",
    "diff ทั้งหมดอยู่ใน 7 ไฟล์ตาม task list เท่านั้น (model/statistics.go, service/statistics_dns.go, service/statistics_dns_test.go, docs/openapi.yaml, frontend/public/openapi.yaml, services/dnsStatisticsService.ts, components/statistics/HostCells.tsx, components/statistics/DnsStatsShared.tsx) — ไม่มีการแก้ kernel/mock.go หรือไฟล์หน้า Traffic"
  ]
}
```

## 6. หมายเหตุถึงเจ้าของ repo (ตัดสินใจได้ ไม่บล็อกการเริ่มงาน)

1. **ค่า `domain` ของ client ใน LAN ปกติจะว่าง** — reverse cache เก็บ "IP ปลายทาง → ชื่อที่ DNS ตอบ"
   ส่วนแถวในตารางนี้เป็น IP ต้นทางใน LAN จะมีชื่อก็ต่อเมื่อมี local zone/DNS ตอบชี้กลับมาในบ้าน
   ผลลัพธ์ที่เห็นบ่อยที่สุดจึงยังเป็น hostname เหมือนเดิม — **ตรงกับพฤติกรรมตาราง Top Source Hosts ของหน้า Traffic
   ทุกประการ** (ซึ่งคือสิ่งที่สั่งมา) ถ้าที่ต้องการจริง ๆ คือ "อยากเห็นชื่อโดเมนของเครื่องในบ้าน" ต้องเป็นงานคนละก้อน
   (เติม reverse cache จาก DHCP hostname + local domain) — บอกได้ ผมจะวางแผนแยกให้
2. **การเรียงลำดับคอลัมน์ Host** ยังเรียงตาม hostname ไม่ใช่ domain (hook `useSortableRows` รองรับแค่ key ตรง ๆ)
   ถ้าอยากให้เรียงตามสิ่งที่ตาเห็น ต้องแก้ hook กลางที่หน้า Traffic ใช้ร่วม → ขอสั่งเป็นงานถัดไปแยกต่างหาก
3. **หัวหน้า client drill-down** (`StatisticsDnsClient.tsx`) ยังแสดงเฉพาะ hostname เพราะใช้ DTO คนละตัว
   (`DNSClientDrilldown.hostname`) ไม่ได้อยู่ในคำขอ 3 ข้อ — ถ้าต้องการให้เหมือนกันด้วย เพิ่มได้อีก 1 task เล็ก ๆ
