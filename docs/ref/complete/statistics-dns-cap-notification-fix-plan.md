# Statistics → DNS: แก้ notification "cap เต็ม" ที่ขึ้นผิด + ทำ `dns-stats-max-ips-per-domain` ให้ default = 32

> เอกสารแผนงานสำหรับคำขอของเจ้าของ repo บน branch `feat/statistics-capacity-visibility` (ต่อยอดจาก PR 128)
>
> วันที่เขียน: 2026-08-09 · Branch อ้างอิง: `feat/statistics-capacity-visibility` @ `469676b`
> ผู้เขียน: ai-tech-lead (สำรวจโค้ดจริงทั้ง backend/frontend ก่อนวางแผน — ทุกข้อใน §1/§2 อ้างไฟล์:บรรทัดจริง)

## 0. เป้าหมายและขอบเขต

**อาการที่เจ้าของ repo แจ้ง:** หน้า `Statistics → DNS` ขึ้น notification ว่า "cap เต็ม / ข้อมูลอาจไม่ครบ"
ทั้งที่เปิดหน้า `Statistics → Capacity` แล้ว **ไม่มีแถวไหนเต็มเลย** และสงสัยว่าเกี่ยวกับ limit 16 IP/domain ที่ hardcode อยู่

**สรุป root cause (ยืนยันจากโค้ดแล้ว — รายละเอียดเต็มใน §2):** ผู้ใช้เข้าใจถูกครึ่งหนึ่ง
เลข 16 คือ `maxIPsPerDomain` (จำนวน IP ที่จำได้ **ต่อ 1 โดเมน** ใน index `domain → resolved IP`) และ**มี config key อยู่แล้ว**
(`dns-stats-max-ips-per-domain`) เพียงแต่ default = 16 — แต่ **ตัว notification ที่ขึ้นผิดไม่ได้เกิดจากค่า 16 โดยตรง**
มันเกิดจาก **3 บั๊กเชิง logic** ที่ทำให้ "โดเมนเดียวชน cap 16 ครั้งเดียวตั้งแต่เปิดเครื่อง" กลายเป็นคำเตือนถาวรทั้งหน้า
และหน้า Capacity **ไม่มีทางแสดงว่าเต็ม** ได้เลยแม้จะเต็มจริง

**เป้าหมาย (สิ่งที่ผู้ใช้จะเห็นหลังแผนนี้เสร็จ)**

1. คำเตือนบนหน้า DNS ขึ้น **เฉพาะเมื่อของจริงที่มันพูดถึงเต็มจริง** และดับเองเมื่อสถานการณ์คลี่คลาย (ไม่ latch ถาวรจนกว่าจะ restart)
2. ข้อความคำเตือนตรงกับสาเหตุ — แยก "ring คู่ (โดเมน, เครื่อง) เต็ม" ออกจาก "index โดเมน→IP เต็ม" (ตอนนี้ปนกันอยู่)
3. หน้า domain drill-down เตือน `ipsTruncated` **เฉพาะโดเมนที่กำลังดูอยู่ชน cap จริง** (ตรงกับ doc comment ของ field ตัวเอง)
4. เมื่อหน้า DNS เตือน → หน้า Capacity ต้องเห็นสาเหตุตรงกัน (มีแถวที่ขึ้นสีแดง/100%) ไม่ขัดกันเองอีก
5. default `dns-stats-max-ips-per-domain` = **32** (ปรับได้จาก config file เหมือนเดิม)

**นอกขอบเขต (จะไม่ทำ — กันแผนบวม)**

- ไม่เพิ่ม CLI flag ใหม่ — key นี้เป็น **file-only** โดยเจตนาเหมือนพี่น้องอีก 8 ตัว (`config.go:101-109`, README §Configuration File)
- ไม่ย้าย cap พวกนี้ไปเป็น setting ใน UI/SQLite (ยังเป็น bootstrap config เหมือนเดิม)
- ไม่แตะ `dnsReverseCache`, ไม่แตะ ring ของ traffic/firewall, ไม่แตะหน้า Statistics → Traffic
- ไม่แก้ตรรกะ `truncated` ของ ring คู่ (โดเมน, เครื่อง) — อันนั้นทำงานถูกอยู่แล้ว
- ไม่เพิ่ม goroutine/ticker ใหม่ (ข้อห้ามเดิมของ subsystem นี้ — `dns_domain_ips.go:446-454`)
- ไม่เพิ่ม dependency ใหม่ทั้ง Go และ npm

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันเขียน)

| ส่วน | สถานะจริง | ไฟล์:บรรทัด |
|---|---|---|
| เลข 16 อยู่ที่ไหนบ้าง | **2 จุดเท่านั้น** — `defaultMaxIPsPerDomain = 16` (fallback ของ service) และ `Defaults().DNSStatsMaxIPsPerDomain: 16` (ค่าจริงที่ใช้บูต) | `service/dns_domain_ips.go:89` · `config/config.go:156` |
| config key มีอยู่แล้วไหม | **มีแล้ว** `dns-stats-max-ips-per-domain` (file-only, ช่วงที่รับ 2–64, out-of-range → clamp + warn ไม่ fail) | `config/config.go:197, 238-239, 418-423` |
| wiring เข้า service | ครบแล้ว: `main.go:534` เรียก `SetDomainIPsLimits(ttl, cfg.DNSStatsMaxDomains, cfg.DNSStatsMaxIPsPerDomain)` | `cmd/pigate/main.go:104, 534` · `service/dns_query_stats.go:239-241` |
| ที่มาของ flag "เต็ม" | field เดียว `dnsDomainIPs.truncated bool` — **latch ถาวร** ตั้งแต่ Put ตัวแรกที่ถูกปฏิเสธ | `service/dns_domain_ips.go:52-58, 178, 193, 430-434` |
| latch ตัวนี้ถูก set จากอะไรบ้าง | **2 เหตุการณ์คนละเรื่องปนกัน**: (ก) โดเมนใหม่ถูกปฏิเสธเพราะครบ `maxDomains` (1000) (ข) IP ใหม่ของโดเมน **ใดโดเมนหนึ่ง** ถูกปฏิเสธเพราะครบ `maxIPsPerDomain` (16) | `dns_domain_ips.go:177-180` และ `:192-195` |
| ใครอ่าน latch ตัวนี้ | 4 จุด: หน้า DNS หลัก, client drill-down, domain drill-down, IP mode | `service/statistics_dns.go:117, 293, 491, 673` |
| หน้า DNS หลักแสดงยังไง | `stats.truncated` → `<DnsStatsTruncatedWarning/>` ข้อความว่า **"จำนวนคู่ (โดเมน, เครื่อง) ... เกินขีดจำกัด"** | `pages/StatisticsDns.tsx:239` · `components/statistics/DnsStatsShared.tsx:471-478` |
| หน้า Capacity อ่าน `ring.truncated` ไหม | **ไม่อ่านเลย** — `ringStatus()` ดูแค่ `fullBuckets` กับ `peakPercent`, และ flat ring มี `fullBuckets = 0` เสมอ | `lib/capacityStatus.ts:18-22` · `service/statistics_capacity.go:198-216` |
| ring `dns.domainIps` นับหน่วยอะไร | นับเป็น **จำนวนโดเมน** (`current/cap` = domains/maxDomains) — cap ต่อโดเมน (`maxIPsPerDomain`) **ถูกโยนทิ้ง** (`_ = domainIPsMaxIPsPerDomain`) | `service/statistics_capacity.go:133, 144, 146-153` |
| ไฟล์ที่เขียนค่า 16 ไว้เป็นเอกสาร | `pigate.conf.example:153`, `README.md:185`, comment ใน `statistics_capacity.go:107` | — |
| `install.sh` seed key นี้ไหม | **ไม่** — เขียนแค่ 4 key (`mock`, `db`, `https-port`, `docker-compat`) → เครื่องที่ติดตั้งจริงจะได้ default ใหม่อัตโนมัติ | `install.sh:333-345` |
| frontend test runner | ไม่มี → เกณฑ์ฝั่ง frontend คือ `yarn build` + `yarn lint` | `frontend/package.json` |

## 2. Root cause (3 บั๊ก ไม่ใช่บั๊กเดียว)

### 2.1 บั๊ก A — latch เดียวถูกใช้ตอบคำถามคนละคำถาม + latch ถาวร

`dns_domain_ips.go:166-204` (`Put`) ตั้ง `d.truncated = true` ทั้งกรณีชน `maxDomains` (บรรทัด 178) และกรณีชน `maxIPsPerDomain` (บรรทัด 193)
แล้ว `Truncated()` (`:430`) คืนค่าเดียวออกไปให้ทุก endpoint

ผลที่เกิดจริงกับผู้ใช้: บ้านที่มี CDN สักโดเมน (`*.googlevideo.com`, `*.akamaized.net`, `*.fbcdn.net` — โดเมนพวกนี้ตอบ A record หมุนเวียนเกิน 16 IP
ภายในไม่กี่นาที) จะ**ชน cap 16 ตั้งแต่ไม่กี่นาทีแรกหลังบูต** → latch ติดถาวร → ทุกหน้า DNS ขึ้นคำเตือนตลอดไป
ทั้งที่ index โดยรวมใช้ไปแค่ ~30–50 โดเมนจาก 1000

ซ้ำร้าย latch นี้ **ไม่มีมิติเวลา** (ไม่ผูกกับ `window` ที่ผู้ใช้เลือก) และ **ไม่เคยถูกล้าง** —
`Clear()` (`:438`) ล้างได้ทางเดียวคือผู้ใช้ปิด query logging ส่วน `SetLimits()` (`:124-140`)
**ไม่ได้ reset ให้ทั้งที่ doc comment บรรทัด 56 เขียนว่า "sticky until Clear/SetLimits"** — code กับ comment ไม่ตรงกัน (บั๊กย่อย A2)
แปลว่าต่อให้ผู้ใช้ขึ้น cap เป็น 64 แล้ว restart-less คำเตือนก็ยังค้าง

### 2.2 บั๊ก B — เอา flag ของ index หนึ่ง ไปตอบเป็นคำเตือนของอีก subsystem หนึ่ง

`statistics_dns.go:111-119`

```go
if b.pairCount >= s.dns.maxPairs || len(b.clientCount) >= s.dns.maxClients {
    truncated = true
}
...
if s.dns.domainIPs.Truncated() {   // <-- บรรทัด 117: OR รวมเข้ามาในตัวแปรเดียวกัน
    truncated = true
}
```

`truncated` ตัวนี้ไหลออกไปเป็น `DNSQueryStatistics.Truncated` ซึ่ง frontend เอาไปเรนเดอร์
`DnsStatsTruncatedWarning` ที่เขียนว่า _"อันดับอาจไม่ครบ เนื่องจากจำนวนคู่ (โดเมน, เครื่อง) ในช่วงเวลานี้เกินขีดจำกัดการติดตาม"_
(`DnsStatsShared.tsx:471-478`) — **ข้อความพูดถึง ring คู่ (โดเมน, เครื่อง) ซึ่งอาจว่างอยู่ที่ 5%**
ส่วนสาเหตุจริงคือ index โดเมน→IP คนละตัวกัน ผู้ใช้จึงไปหาที่หน้า Capacity แล้วไม่เจออะไรเต็ม → ตรงกับอาการที่แจ้งมาเป๊ะ
(จุดเดียวกันซ้ำอีกที่ `:491-493` สำหรับ client drill-down)

และที่ `statistics_dns.go:293` `GetDNSDomainDetail` ใช้ latch **ระดับ index ทั้งก้อน** มาตอบ `IPsTruncated` ของ **โดเมนเดียว**
ทั้งที่ doc comment ของ field เขียนไว้ชัดว่า _"hit its per-domain IP cap while building IPs above"_
(`model/statistics.go:535-540`) → เปิดโดเมนที่มี IP เดียว ก็ยังเตือนว่ารายการ IP ไม่ครบ

### 2.3 บั๊ก C — หน้า Capacity ไม่มีทางยืนยันสาเหตุนี้ได้เลย (ต้นตอของคำว่า "ขัดกันเอง")

1. `flatRing()` (`statistics_capacity.go:198-216`) ส่ง `Truncated` ออกมาก็จริง แต่ frontend `ringStatus()`
   (`lib/capacityStatus.ts:18-22`) **ไม่เคยอ่าน field นี้** — ดูแค่ `fullBuckets > 0` (flat ring = 0 เสมอ) และ `peakPercent`
2. `dns.domainIps` รายงาน `current/cap` เป็นหน่วย **โดเมน** เท่านั้น (`:144`) ส่วน `maxIPsPerDomain` ถูกโยนทิ้งที่ `:153`
   → ต่อให้ 200 โดเมนชน cap 16 พร้อมกัน ring นี้ก็ยังโชว์ `200 / 1000 = 20%` **สีเขียว**

ดังนั้นหน้า Capacity **ไม่สามารถ** แสดงว่า "cap ต่อโดเมนเต็ม" ได้เลยไม่ว่ากรณีใด ๆ — นี่คือเหตุผลที่ผู้ใช้เห็นคำเตือนแล้วไปตรวจไม่พบ

### 2.4 เลข 16 เกี่ยวยังไง

16 ไม่ได้ "ผิด" ในเชิง logic แต่มัน **ต่ำเกินไปสำหรับโลกจริง** — CDN/anycast สมัยใหม่ตอบ A record หมุน 8–40 IP ต่อโดเมน
ค่า 16 จึงทำให้ "ชน cap" กลายเป็นเหตุการณ์ปกติที่เกิดทุกวัน แล้วบั๊ก A/B/C ข้างบนก็ขยายมันเป็นคำเตือนถาวร
การขึ้นเป็น 32 ลดความถี่ของการชน cap ลงมาก (RAM worst case 1000 × 32 × ~55B ≈ 1.8 MB ยังอยู่ในงบเดิม "~1-2 MB")
แต่ **ต่อให้ขึ้นเป็น 64 ก็ยังไม่แก้บั๊ก A/B/C** — ต้องทำทั้งสองอย่าง

## 3. แนวทางเทคนิค

### 3.1 แยก latch เป็น 2 ตัว + ผูกกับ TTL แทนการ latch ถาวร

แทนที่ `truncated bool` ด้วย 2 timestamp:

```go
domainCapHitAt time.Time // ครั้งล่าสุดที่ "โดเมนใหม่" ถูกปฏิเสธเพราะครบ maxDomains
ipCapHitAt     time.Time // ครั้งล่าสุดที่ "IP ใหม่ของโดเมนหนึ่ง" ถูกปฏิเสธเพราะครบ maxIPsPerDomain
```

- `IndexTruncated() bool` = `!domainCapHitAt.IsZero() && time.Since(domainCapHitAt) <= ttl`
- `IPCapHitRecently() bool` = เช่นเดียวกันกับ `ipCapHitAt`
- **เหตุผลที่ใช้ TTL เป็นหน้าต่างหมดอายุ**: ทั้ง index อายุตาม `ttl` อยู่แล้ว (`evictExpiredLocked`) — ถ้าไม่มีการปฏิเสธใหม่เลยตลอด 1 TTL
  แปลว่าข้อมูลที่ "หายไป" นั้นหมดอายุไปเองแล้ว คำเตือนจึงควรดับ เป็นกติกาเดียวกับตัว index เอง ไม่ต้องมี ticker
- `SetLimits()` และ `Clear()` ต้อง **zero ทั้งสอง field** (แก้บั๊ก A2 ให้ code ตรงกับ comment เดิม)

### 3.2 "โดเมนนี้เต็มไหม" ให้คำนวณตอนอ่าน ไม่ต้องเก็บ state

`GetDNSDomainDetail` มี `ips := s.dns.domainIPs.IPsFor(domain)` อยู่แล้ว (`statistics_dns.go:292`)
และมี `caps()` ที่คืน `(maxDomains, maxIPsPerDomain)` อยู่แล้ว (`dns_domain_ips.go:147-151`) →

```go
_, maxIPs := s.dns.domainIPs.caps()
ipsTruncated := len(ips) >= maxIPs        // ตรงกับ doc comment ของ field เป๊ะ
```

**ไม่ต้องเพิ่ม map ใหม่ ไม่ต้องเพิ่ม lock รอบใหม่** (ทางเลือกที่ตัดทิ้ง: เก็บ `map[domain]struct{}` ของโดเมนที่เคยชน cap —
เปลือง RAM, ต้องดูแล lifecycle ตอน evict, และตอบผิดหลัง TTL ผ่านไป)

เส้นทาง client drill-down ที่ใช้ `StatsFor()` ก็ใช้กติกาเดียวกันได้จาก `domainIPStat.Count >= maxIPs`

### 3.3 แยก field ใน DTO — ไม่เอาไป OR รวมกับ `truncated` ของ ring คู่

เพิ่ม field ใหม่ `domainIndexTruncated` (bool) ใน `DNSQueryStatistics` และ `DNSClientDrilldown`
แล้ว **ถอด** การ OR ที่ `statistics_dns.go:117` และ `:491` ออก → `truncated` กลับไปหมายถึง ring คู่ (โดเมน, เครื่อง) ล้วน ๆ ตามชื่อ
เป็นการเปลี่ยนแบบ additive (field ใหม่) ไม่ทำ API เดิมพัง

### 3.4 หน้า Capacity ต้องยืนยันสาเหตุได้

1. เพิ่ม ring ที่ 10: `dns.domainIpsPerDomain` (flat) — `current` = จำนวน IP มากที่สุดที่โดเมนเดียวถืออยู่,
   `cap` = `maxIPsPerDomain`, `truncated` = มีโดเมนที่ `len(ips) >= maxIPsPerDomain` อย่างน้อย 1 โดเมน
   → เวลาชน cap หน้านี้จะโชว์ `32 / 32 = 100%` สีแดง ตรงกับคำเตือนหน้า DNS
2. `ringStatus()` ฝั่ง frontend ต้องคืน `"danger"` เมื่อ `ring.truncated === true` ด้วย (ปัจจุบันเมิน field นี้ทั้ง 9 ring)

> **ทางเลือกที่ให้เจ้าของ repo ตัดสิน (ดู §6):** ถ้าอยากคุมขอบเขต PR ให้เล็ก ตัด T-06/T-08b/T-10b (ring ที่ 10) ออกได้
> — บั๊กที่ผู้ใช้เจอจะหายด้วย T-03/T-04 อยู่แล้ว แต่หน้า Capacity จะยังยืนยัน "cap ต่อโดเมนเต็ม" ไม่ได้ (ข้อ 2.3 ยังค้าง)

### 3.5 ค่า default ใหม่

| ที่ | เดิม | ใหม่ |
|---|---|---|
| `config/config.go:156` `Defaults().DNSStatsMaxIPsPerDomain` | 16 | **32** |
| `service/dns_domain_ips.go:89` `defaultMaxIPsPerDomain` (fallback) | 16 | **32** |
| ช่วงที่รับ `minDNSStatsMaxIPsPerDomain..max` (`config.go:238-239`) | 2..64 | **ไม่เปลี่ยน** (32 อยู่ในช่วงอยู่แล้ว) |
| `pigate.conf.example:153` · `README.md:185` · comment `statistics_capacity.go:105-109` | 16 | **32** |

## 4. Tasks

```json
[
  {
    "task_id": "T-01",
    "title": "config: เปลี่ยน default dns-stats-max-ips-per-domain เป็น 32",
    "layer": "backend",
    "files": ["backend/internal/config/config.go", "backend/internal/config/config_test.go"],
    "instruction": "แก้ backend/internal/config/config.go บรรทัด 156: DNSStatsMaxIPsPerDomain: 16 -> 32 และอัปเดต comment บรรทัด 152-154 ('Keep in sync with ... dns_domain_ips.go') ให้ยังถูกต้อง. ห้ามแตะ minDNSStatsMaxIPsPerDomain/maxDNSStatsMaxIPsPerDomain (บรรทัด 238-239, ยังคง 2..64) และห้ามเพิ่ม CLI flag ใด ๆ (key นี้เป็น file-only โดยเจตนา ดู comment บรรทัด 101-109). แก้ comment บรรทัด 230-234 ที่ยกตัวอย่าง '1000 x 16 = 16000 entries, ~1 MB' ให้เป็น '1000 x 32 = 32000 entries, ~2 MB'. จากนั้นอัปเดต backend/internal/config/config_test.go ทุกจุดที่ assert ค่า 16 ให้เป็น 32 (บรรทัด ~233-234, ~269-270, ~283-284, ~297-298) โดยไม่เปลี่ยนความหมายของเคส (เคส out-of-range ยังต้อง fallback เป็น default ซึ่งตอนนี้คือ 32); เคสบรรทัด 239/247 ที่ set ไฟล์เป็น '32' ให้เปลี่ยนเป็นค่าอื่นที่ไม่ใช่ default เช่น '40' เพื่อไม่ให้เทสต์ผ่านโดยบังเอิญเพราะบังเอิญเท่ากับ default ใหม่",
    "acceptance": [
      "cd backend && go build ./... ผ่าน",
      "config.Defaults().DNSStatsMaxIPsPerDomain == 32",
      "ไม่มี flag ใหม่ใน cmd/pigate/main.go",
      "go test ./internal/config/... ผ่าน"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "service: เปลี่ยน fallback default ของ dnsDomainIPs เป็น 32",
    "layer": "service",
    "files": ["backend/internal/service/dns_domain_ips.go"],
    "instruction": "แก้ backend/internal/service/dns_domain_ips.go บรรทัด 89: defaultMaxIPsPerDomain = 16 -> 32 (ค่านี้เป็น fallback เมื่อ caller ส่งค่า <=0 เท่านั้น; ค่าที่ใช้จริงมาจาก config ผ่าน SetDomainIPsLimits — comment บรรทัด 82-87 อธิบายไว้แล้ว ให้คงไว้และปรับตัวเลขให้ตรง). อัปเดต RAM-budget comment ที่ header บรรทัด 30-36 จาก 'maxIPsPerDomain (default 16) ~= 16,000 entries / ~1-2 MB' เป็นตัวเลขที่ default ใหม่ (1000 x 32 = 32,000 entries, ~2-4 MB รวม ipDomains). ห้ามแก้ dnsDomainIPsPerDomainMin/Max (2/64)",
    "acceptance": [
      "cd backend && go build ./... ผ่าน",
      "defaultMaxIPsPerDomain == 32 และ comment ทุกจุดในไฟล์ไม่เหลือเลข 16 ที่หมายถึง default นี้"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-03",
    "title": "service (แกนของบั๊ก): แยก truncation latch ของ dnsDomainIPs เป็น 2 ตัว + ผูกกับ TTL + reset ใน SetLimits",
    "layer": "service",
    "files": ["backend/internal/service/dns_domain_ips.go"],
    "instruction": "แก้ backend/internal/service/dns_domain_ips.go ดังนี้ (นี่คือแกนของการแก้บั๊ก อ่าน docs/ref/todo/statistics-dns-cap-notification-fix-plan.md §2.1/§3.1 ก่อนลงมือ):\n1) ลบ field `truncated bool` (บรรทัด 52-58) แล้วใส่แทนด้วยสอง field: `domainCapHitAt time.Time` (ครั้งล่าสุดที่โดเมนใหม่ถูกปฏิเสธเพราะครบ maxDomains) และ `ipCapHitAt time.Time` (ครั้งล่าสุดที่ IP ใหม่ของโดเมนหนึ่งถูกปฏิเสธเพราะครบ maxIPsPerDomain) พร้อม doc comment อธิบายว่าทำไมต้องแยกและทำไมหมดอายุตาม ttl (แทน latch ถาวร)\n2) ใน Put(): บรรทัด ~178 ที่เดิม `d.truncated = true` (เคสชน maxDomains) -> `d.domainCapHitAt = now`; บรรทัด ~193 (เคสชน maxIPsPerDomain) -> `d.ipCapHitAt = now`. ห้ามเพิ่ม allocation/I/O ใด ๆ ใน Put (hot path ของ WatchDNSLog)\n3) แทน `Truncated()` (บรรทัด 430-434) ด้วยสองเมธอด read-only (RLock เท่านั้น ห้าม evict): `IndexTruncated() bool` = `!d.domainCapHitAt.IsZero() && time.Since(d.domainCapHitAt) <= d.ttl` และ `IPCapHitRecently() bool` = แบบเดียวกันกับ ipCapHitAt\n4) SetLimits() (บรรทัด 124-140): เพิ่มการ zero ทั้งสอง field (doc comment เดิมบรรทัด 56 อ้างว่า sticky until Clear/SetLimits แต่ code ไม่เคยทำ — นี่คือการแก้ให้ตรง) และ Clear() (บรรทัด 438-444) ก็ zero ทั้งสองเช่นกัน\n5) Usage() (บรรทัด 420-424): เปลี่ยน signature เป็น `Usage() (domains, maxDomains, maxIPsPerDomain, maxIPsUsed, domainsAtIPCap int, indexTruncated bool)` โดย maxIPsUsed = จำนวน IP มากสุดที่โดเมนเดียวถืออยู่ และ domainsAtIPCap = จำนวนโดเมนที่ len(ips) >= maxIPsPerDomain — คำนวณด้วยการวน d.byDomain ใต้ RLock เดียว (O(domains) <= 20000, ไม่ evict, ไม่ alloc map ใหม่) พร้อม doc comment ระบุว่ายอมรับ cost นี้เพราะถูกเรียกเฉพาะ endpoint /statistics/capacity\n6) ห้ามเพิ่ม goroutine/ticker ใด ๆ",
    "acceptance": [
      "cd backend && go build ./... ผ่าน (ยกเว้น call site ที่ T-04/T-06 จะแก้ ให้แก้ให้คอมไพล์ผ่านด้วยการปรับ call site ตาม T-04/T-06 หากจำเป็น)",
      "ไม่มี field/เมธอดชื่อ truncated/Truncated() เหลืออยู่ใน dns_domain_ips.go",
      "Put() ยังไม่มี I/O, ไม่ log ชื่อโดเมน, และไม่มี allocation เพิ่ม",
      "Usage() ใช้ RLock อย่างเดียวและไม่เรียก evict* ใด ๆ"
    ],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-04",
    "title": "service: เลิก OR flag ของ index โดเมน→IP เข้ากับ truncated ของ ring คู่ + ทำ ipsTruncated ให้เป็นรายโดเมนจริง",
    "layer": "service",
    "files": ["backend/internal/service/statistics_dns.go"],
    "instruction": "แก้ backend/internal/service/statistics_dns.go ตาม §2.2/§3.2/§3.3 ของแผน:\n1) GetDNSQueryStatistics: ลบบล็อก `if s.dns.domainIPs.Truncated() { truncated = true }` (บรรทัด 117-119) ออก แล้วเซ็ต field ใหม่ `DomainIndexTruncated: s.dns.domainIPs.IndexTruncated()` ใน struct ผลลัพธ์แทน (field เพิ่มใน T-05). ห้ามแตะเงื่อนไข pairCount/clientCount บรรทัด 111-113\n2) GetDNSClientDomains: ลบบล็อกเดียวกันที่บรรทัด 491-493 แล้วเซ็ต DomainIndexTruncated บน DNSClientDrilldown แทน\n3) GetDNSDomainDetail: เปลี่ยนบรรทัด 293 จาก `ipsTruncated := s.dns.domainIPs.Truncated()` เป็นการคำนวณรายโดเมน: `_, maxIPs := s.dns.domainIPs.caps()` แล้ว `ipsTruncated := maxIPs > 0 && len(ips) >= maxIPs` (ips คือผลจาก IPsFor บรรทัด 292) — ตรงกับ doc comment ของ model.DNSDomainDrilldown.IPsTruncated ที่มีอยู่แล้ว\n4) GetDNSIPDomains: บรรทัด 673 เปลี่ยนเป็น `ipsTruncated := s.dns.domainIPs.IndexTruncated() || <มีโดเมนใดในผลลัพธ์ที่จำนวน IP >= maxIPs>` (ใช้ข้อมูล IPsForMany ที่ฟังก์ชันนี้เรียกอยู่แล้ว อย่าเรียก index ซ้ำ)\n5) ห้ามละเมิดกติกา locking ของไฟล์นี้ (header comment บรรทัด 26-34): ต้องปล่อย s.dns.mu.RUnlock() ก่อนแตะ domainIPs เสมอ และ 1 request = ไม่เกิน 1 GetTrafficBreakdown* + 1 hostLookup — การแก้นี้ต้องไม่เพิ่มการเรียกทั้งสองอย่าง",
    "acceptance": [
      "cd backend && go build ./... ผ่าน",
      "ไม่มีการเรียก domainIPs.Truncated() เหลือในไฟล์",
      "จำนวนการเรียก GetTrafficBreakdown*/hostLookup ต่อฟังก์ชันเท่าเดิมทุกฟังก์ชัน",
      "ทุกจุดที่แตะ domainIPs ยังอยู่หลัง s.dns.mu.RUnlock()"
    ],
    "depends_on": ["T-03", "T-05"]
  },
  {
    "task_id": "T-05",
    "title": "model: เพิ่ม field domainIndexTruncated + ปรับ doc comment ของ ipsTruncated ให้ตรงกับพฤติกรรมใหม่",
    "layer": "model",
    "files": ["backend/internal/model/statistics.go"],
    "instruction": "แก้ backend/internal/model/statistics.go:\n1) เพิ่ม field `DomainIndexTruncated bool `json:\"domainIndexTruncated\"`` ให้ DNSQueryStatistics และ DNSClientDrilldown พร้อม doc comment ที่ระบุชัดว่า: true เมื่อ index โดเมน→IP ปฏิเสธ 'โดเมนใหม่' เพราะครบ dns-stats-max-domains ภายใน 1 ช่วง TTL ล่าสุด — เป็นคนละสัญญาณกับ Truncated (ซึ่งหมายถึง ring คู่ (โดเมน, เครื่อง) เท่านั้น)\n2) แก้ doc comment ของ DNSIPDomains.IPsTruncated (บรรทัด 342-348) ให้ตรงกับ logic ใหม่ใน T-04 ข้อ 4 (index-level หมดอายุตาม TTL หรือมีโดเมนในผลลัพธ์ที่ชน per-domain cap)\n3) doc comment ของ DNSDomainDrilldown.IPsTruncated (บรรทัด 535-540) เนื้อความถูกต้องอยู่แล้ว — เพิ่มประโยคย้ำว่าเป็นค่าที่คำนวณจากโดเมนนี้เท่านั้น ไม่ใช่สถานะทั้ง index\n4) แก้ doc comment ของ RingCapacity.Truncated (บรรทัด 851-858) ให้สะท้อนว่า frontend ใช้ field นี้จัดสถานะสีแล้ว (ดู T-10)",
    "acceptance": [
      "cd backend && go build ./... ผ่าน",
      "field ใหม่มี json tag ตรงกับที่ frontend/openapi จะใช้ (domainIndexTruncated)"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-06",
    "title": "service: เพิ่ม ring ที่ 10 (dns.domainIpsPerDomain) ในหน้า Capacity",
    "layer": "service",
    "files": ["backend/internal/service/statistics_capacity.go"],
    "instruction": "แก้ backend/internal/service/statistics_capacity.go ตาม §3.4:\n1) เพิ่ม capacityRingMeta ใหม่ `capacityMetaDNSDomainIPsPerDomain` = {id: \"dns.domainIpsPerDomain\", group: \"dns\", label: \"DNS resolved IPs ต่อโดเมน (สูงสุด)\", kind: \"flat\", capSource: \"dns-stats-max-ips-per-domain\", entryBytes: 55}\n2) ปรับ GetCapacityStatistics ให้รับค่าจาก Usage() signature ใหม่ของ T-03 (domains, maxDomains, maxIPsPerDomain, maxIPsUsed, domainsAtIPCap, indexTruncated) แล้ว append ring ใหม่ต่อท้าย rings (เป็นตัวที่ 10, ต่อจาก dns.domainIps): flatRing(meta, maxIPsUsed, maxIPsPerDomain, domainsAtIPCap > 0)\n3) ลบบรรทัด `_ = domainIPsMaxIPsPerDomain` (บรรทัด 153) และ comment บรรทัด 146-152 ที่อธิบายว่าทำไมไม่มีแถวนี้ — แทนที่ด้วย comment ใหม่ที่อธิบายว่า ring ที่ 10 คือแถวนั้น\n4) แก้ capSource ของ dns.domainIps (บรรทัด 101) จาก \"dns-stats-max-domains / dns-stats-max-ips-per-domain\" เป็น \"dns-stats-max-domains\" เพียงอย่างเดียว (cap ต่อโดเมนย้ายไปอยู่ ring ใหม่แล้ว)\n5) แก้ตัวเลขใน comment บรรทัด 105-109 ให้อิง default ใหม่ 32 (≈ 32 x 55B + overhead ≈ 2000B/domain) และปรับ entryBytes ของ capacityMetaDNSDomainIPs จาก 1000 เป็น 2000\n6) แก้ header comment ของไฟล์ (บรรทัด 9-26) ที่เขียนว่า '9 rings' ให้เป็น 10",
    "acceptance": [
      "cd backend && go build ./... ผ่าน",
      "GetCapacityStatistics คืน 10 ring ตามลำดับคงที่ โดยตัวที่ 10 คือ dns.domainIpsPerDomain",
      "ยังไม่มีการ mutate โครงสร้างใด ๆ ในไฟล์นี้ (read-only ตาม header comment เดิม)"
    ],
    "depends_on": ["T-03"]
  },
  {
    "task_id": "T-07",
    "title": "openapi: อัปเดตทั้ง 2 ไฟล์ให้ตรงกันทุก byte",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "อัปเดต docs/openapi.yaml และ frontend/public/openapi.yaml (สองไฟล์ต้องเหมือนกันทุก byte — คัดลอกไฟล์เดียวไปทับอีกไฟล์หลังแก้เสร็จ):\n1) เพิ่ม property `domainIndexTruncated` (boolean, required) ใน schema ของ DNSQueryStatistics และ DNSClientDrilldown พร้อม description ตรงกับ doc comment ใน T-05\n2) แก้ description ของ ipsTruncated ทั้งใน DNSIPDomains (บรรทัด ~5312-5318) และ DNSDomainDrilldown (บรรทัด ~5609-5615) ให้ตรงกับพฤติกรรมใหม่\n3) แก้ทุกจุดที่เขียนว่า 'exactly 9 rings' / 'Always exactly 9 entries' / รายการ id ของ ring (บรรทัด ~775-778, ~813-814, ~817, ~5906-5913) ให้เป็น 10 ring และเพิ่ม dns.domainIpsPerDomain ท้ายรายการ (เป็น flat ring อีกตัวที่ไม่มี series)",
    "acceptance": [
      "diff docs/openapi.yaml frontend/public/openapi.yaml ต้องไม่มีความต่าง",
      "ไม่มีข้อความ 'exactly 9 rings' หรือ 'Always exactly 9 entries' หลงเหลือ",
      "YAML ยัง parse ได้ (เปิดหน้า ApiDocs ได้หลัง build)"
    ],
    "depends_on": ["T-05", "T-06"]
  },
  {
    "task_id": "T-08",
    "title": "frontend services: เพิ่ม type ใหม่ + mock ring ที่ 10",
    "layer": "frontend",
    "files": ["frontend/src/services/dnsStatisticsService.ts", "frontend/src/services/capacityService.ts"],
    "instruction": "1) frontend/src/services/dnsStatisticsService.ts: เพิ่ม `domainIndexTruncated: boolean` ให้ interface DNSQueryStatistics (บรรทัด ~131-145) และ DNSClientDrilldown (บรรทัด ~198-210) พร้อมคอมเมนต์สั้น ๆ ว่าต่างจาก truncated อย่างไร; เพิ่มค่า false ใน mock/fallback object ทุกตัวที่สร้าง DNSQueryStatistics/DNSClientDrilldown (บรรทัด ~569, ~729)\n2) frontend/src/services/capacityService.ts: เพิ่ม spec ของ ring ที่ 10 ต่อท้าย array (บรรทัด ~78-86): { id: \"dns.domainIpsPerDomain\", group: \"dns\", label: \"DNS resolved IPs ต่อโดเมน (สูงสุด)\", kind: \"flat\", capSource: \"dns-stats-max-ips-per-domain\", entryBytes: 55, cap: 32, currentPercent: 50, peakPercent: 50, truncated: false } และตรวจว่า type CapacityRingGroup/RingCapacity ไม่ต้องแก้ (id เป็น string อยู่แล้ว — ถ้าเป็น union literal ให้เพิ่ม id ใหม่เข้าไปด้วย); อัปเดต comment ใด ๆ ที่ระบุ '9 rings'",
    "acceptance": [
      "cd frontend && yarn build ผ่าน",
      "cd frontend && yarn lint ไม่มี error ใหม่"
    ],
    "depends_on": ["T-05", "T-06"]
  },
  {
    "task_id": "T-09",
    "title": "frontend: แยกคำเตือนหน้า Statistics → DNS ให้ตรงสาเหตุ",
    "layer": "frontend",
    "files": ["frontend/src/pages/StatisticsDns.tsx", "frontend/src/components/statistics/DnsStatsShared.tsx"],
    "instruction": "1) frontend/src/components/statistics/DnsStatsShared.tsx: เพิ่ม component ใหม่ `DnsDomainIndexTruncatedWarning` ข้าง ๆ DnsStatsTruncatedWarning (บรรทัด 471-478) รูปแบบ/สีเดียวกัน (border-warning/20 bg-warning/10 text-warning + TriangleAlert) ข้อความ: 'ดัชนีโดเมน→IP เก็บโดเมนครบขีดจำกัดแล้ว (dns-stats-max-domains) — โดเมนใหม่บางส่วนอาจไม่ถูกจับคู่กับปริมาณข้อมูล' ห้ามใช้ Tailwind palette class ตรง ๆ และห้ามใส่ shadow-*/backdrop-blur-* (docs/rules_of_work.md)\n2) frontend/src/pages/StatisticsDns.tsx บรรทัด 239: คงบรรทัดเดิม `{stats?.truncated && <DnsStatsTruncatedWarning />}` ไว้ (ตอนนี้มันหมายถึง ring คู่จริง ๆ แล้ว) และเพิ่มบรรทัดถัดไป `{stats?.domainIndexTruncated && <DnsDomainIndexTruncatedWarning />}`\n3) แก้ข้อความ banner ในโหมด IP (บรรทัด 320-325) ให้ชัดขึ้นว่าเป็นข้อจำกัดของดัชนี ไม่ใช่ของตารางนี้ เช่น 'ดัชนีโดเมน→IP เคยเต็มขีดจำกัดในช่วงที่ผ่านมา — รายชื่อโดเมนของ IP นี้อาจไม่ครบ'\n4) ห้ามแตะ Top Source Hosts / DnsQueryTrendCard / stat cards",
    "acceptance": [
      "cd frontend && yarn build ผ่าน และ yarn lint ไม่มี error ใหม่",
      "ไม่มี hardcoded palette class (เช่น text-amber-500) และไม่มี shadow-*/backdrop-blur-* ในโค้ดที่เพิ่ม",
      "ทั้ง dark และ light mode ใช้ตัวแปรสีเชิงความหมายเดิม"
    ],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-10",
    "title": "frontend: ให้สถานะสีของ ring อ่าน truncated ด้วย",
    "layer": "frontend",
    "files": ["frontend/src/lib/capacityStatus.ts"],
    "instruction": "แก้ frontend/src/lib/capacityStatus.ts ฟังก์ชัน ringStatus (บรรทัด 18-22): เพิ่มเงื่อนไขแรกสุด `if (ring.truncated) return \"danger\"` แล้วตามด้วยเงื่อนไขเดิม (fullBuckets > 0 || peakPercent >= 90 -> danger; peakPercent >= 70 -> warn; ok). อัปเดต doc comment บรรทัด 13-17 ให้อธิบายว่าทำไม truncated ต้องเป็น danger ทันที (flat ring ไม่มี fullBuckets จึงไม่มีทางแดงได้เลยก่อนหน้านี้ — ดู statistics-dns-cap-notification-fix-plan.md §2.3). ห้ามแก้ ringStatusClasses",
    "acceptance": [
      "cd frontend && yarn build ผ่าน และ yarn lint ไม่มี error ใหม่",
      "flat ring ที่ truncated=true ให้สถานะ danger ทั้งใน CapacityIndicator และหน้า StatisticsCapacity (ทั้งสองไฟล์ import ringStatus ตัวเดียวกัน ไม่ต้องแก้เพิ่ม)"
    ],
    "depends_on": ["T-08"]
  },
  {
    "task_id": "T-11",
    "title": "backend tests: ปรับ/เพิ่มเทสต์ให้ครอบคลุมพฤติกรรมใหม่",
    "layer": "backend",
    "files": [
      "backend/internal/service/dns_domain_ips_test.go",
      "backend/internal/service/statistics_capacity_test.go",
      "backend/internal/service/statistics_dns_test.go",
      "backend/internal/api/handlers_test.go"
    ],
    "instruction": "1) dns_domain_ips_test.go: แทนที่ทุกการเรียก d.Truncated() (บรรทัด 26, 42, 71, 202, 215) ด้วย IndexTruncated()/IPCapHitRecently() ตามความหมายของเคสนั้น ๆ (เคสชน maxDomains -> IndexTruncated, เคสชน maxIPsPerDomain -> IPCapHitRecently) และเพิ่มเทสต์ใหม่ 3 เคส: (ก) ชน per-domain IP cap แล้ว IndexTruncated() ต้องยังเป็น false (นี่คือบั๊กที่แผนนี้แก้), (ข) SetLimits() ต้อง reset ทั้งสอง latch, (ค) Usage() คืน maxIPsUsed/domainsAtIPCap ถูกต้องเมื่อมีโดเมนหนึ่งชน cap และอีกโดเมนไม่ชน\n2) statistics_capacity_test.go: แก้ทุกจุดที่ assert 9 ring (บรรทัด 10, 25, 34, 39-40) ให้เป็น 10 และเพิ่ม dns.domainIpsPerDomain ต่อท้าย wantIDs\n3) statistics_dns_test.go: เพิ่มเทสต์ที่พิสูจน์ว่าโดเมน A ชน per-domain cap แล้ว GetDNSQueryStatistics().Truncated ต้องยังเป็น false (ตราบใดที่ ring คู่ยังไม่เต็ม) และ GetDNSDomainDetail(domain B ที่มี IP เดียว).IPsTruncated ต้องเป็น false\n4) handlers_test.go: แก้ assert '9 rings' (บรรทัด 2057, 2080, 2090-2091) เป็น 10",
    "acceptance": [
      "cd backend && go test ./... ผ่านทั้งหมด",
      "เทสต์ใหม่ล้มถ้าย้อนโค้ดกลับไปเป็น latch ตัวเดียวแบบเดิม (พิสูจน์ว่าเทสต์จับบั๊กจริง)"
    ],
    "depends_on": ["T-03", "T-04", "T-06"]
  },
  {
    "task_id": "T-12",
    "title": "docs/config sample: sync ค่า 16 -> 32 ทุกที่ที่เป็นเอกสาร",
    "layer": "docs",
    "files": ["pigate.conf.example", "README.md"],
    "instruction": "1) pigate.conf.example บรรทัด 151-153: dns-stats-max-ips-per-domain=16 -> 32 และปรับคอมเมนต์อธิบาย RAM ให้ตรง (1000 x 32 ≈ 32,000 entries ≈ ~2 MB)\n2) README.md บรรทัด 185: แก้ '(default `16`, accepted range 2–64)' -> '(default `32`, accepted range 2–64)' และแก้ตัวเลข '≈1 MB at the defaults' -> '≈2 MB at the defaults'\n3) เพิ่มประโยคเตือน 1 บรรทัดใน README ตรงหัวข้อเดียวกันว่า: เครื่องที่ไฟล์ /var/lib/pigate/pigate.conf ถูกสร้างอัตโนมัติโดยตัว binary (ไม่ใช่โดย install.sh) จะมีบรรทัด dns-stats-max-ips-per-domain=16 ค้างอยู่ ต้องลบ/แก้บรรทัดนั้นเองจึงจะได้ค่า default ใหม่ (install.sh เขียนแค่ 4 key จึงไม่ได้รับผลกระทบ)\n4) ห้ามแตะ install.sh (ไม่ต้อง seed key นี้)",
    "acceptance": [
      "ไม่เหลือเลข 16 ที่หมายถึง dns-stats-max-ips-per-domain ในทั้ง repo (grep ตรวจได้)",
      "README/pigate.conf.example อ่านแล้วสอดคล้องกับ config.Defaults() จริง"
    ],
    "depends_on": ["T-01"]
  }
]
```

## 5. ข้อควรระวัง (Cautions)

1. **Put() คือ hot path** ของ `WatchDNSLog` — ห้ามใส่ log, alloc, หรืออะไรที่ไม่ใช่ O(1) เพิ่มใน T-03
2. **ห้าม log ชื่อโดเมน/IP ของ client** ทุกกรณี (privacy — ข้อมูลนี้เป็นพฤติกรรมการใช้อินเทอร์เน็ตของคนในบ้าน)
3. **กติกา locking ของ `statistics_dns.go`** (header comment บรรทัด 26-34): ปล่อย `s.dns.mu.RUnlock()` ก่อนแตะ `domainIPs`/`traffic` เสมอ,
   1 request ห้ามเกิน 1 `GetTrafficBreakdown*` + 1 `hostLookup` — T-04 ต้องไม่ทำให้ตัวเลขนี้เพิ่ม
4. **`Usage()` ต้องไม่ evict** (`statistics_capacity.go` header: "viewing capacity must not change it") — T-03 ข้อ 5 ต้องใช้ RLock อย่างเดียว
5. **openapi 2 ไฟล์ต้องเหมือนกันทุก byte** — วิธีที่ปลอดภัยคือแก้ `docs/openapi.yaml` แล้ว `cp` ไปทับ `frontend/public/openapi.yaml`
6. **index นี้ห้ามใช้ตัดสินใจเชิงความปลอดภัย** (`dns_domain_ips.go:24-28`) — ข้อมูลมาจาก answer log ที่ client ในบ้านชี้นำได้
   งานนี้เป็นแค่ display-only enrichment เท่านั้น ห้ามมีโค้ดใหม่ที่เอา `domainIPs` ไปสร้าง firewall rule/route/QoS
7. **การเปลี่ยน default ไม่มีผลกับเครื่องที่มี key นี้ในไฟล์ config อยู่แล้ว** — precedence คือ code default < config file < CLI flag
   (`config.Resolve`) ตรวจข้อนี้ตอนทดสอบด้วย (ดู Final Acceptance ข้อ 8)
8. **งาน frontend ต้องอยู่บน feature branch เดิม `feat/statistics-capacity-visibility` และเข้า main ผ่าน PR เท่านั้น** —
   ห้าม push โค้ดขึ้น main ตรง ๆ (เอกสารแผนนี้ push main ได้)

## 6. เรื่องที่ต้องให้เจ้าของ repo ตัดสิน

| # | ประเด็น | ทางเลือก | ข้อเสนอของ ai-tech-lead |
|---|---|---|---|
| 1 | ring ที่ 10 บนหน้า Capacity (T-06/T-08/T-10) | (ก) ทำ — หน้า Capacity ยืนยันสาเหตุได้ แต่กระทบ openapi 2 ไฟล์ + เทสต์ที่ assert '9 rings' 2 ที่ · (ข) ไม่ทำ — PR เล็กลง แต่ปัญหา "เตือนแล้วไปหาไม่เจอ" ยังเหลือครึ่งหนึ่ง | **(ก) ทำ** — ต้นเรื่องของบั๊กนี้คือผู้ใช้หาสาเหตุจากหน้า Capacity ไม่เจอ |
| 2 | หน้าต่างหมดอายุของคำเตือน | (ก) ผูกกับ TTL ของ index (เสนอ) · (ข) fix 15 นาที · (ค) latch ถาวรเหมือนเดิม | **(ก)** — ใช้กติกาเดียวกับที่ index ลืมข้อมูล ไม่ต้องมีค่าคงที่ใหม่ให้จูน |
| 3 | ค่า default 32 พอไหม | 32 (เสนอ) / 48 / 64 | **32** ตามที่สั่ง — RAM ~2 MB, ปรับขึ้นได้ถึง 64 จาก config โดยไม่ต้อง rebuild |
| 4 | ควร log warning ตอนบูตไหมว่า config file มี key ค้างค่าเก่า | ทำ / ไม่ทำ | **ไม่ทำในแผนนี้** — `config.Resolve` ไม่มีแนวคิด "ค่านี้เป็นค่าเก่า" การเพิ่มจะทำให้ semantics ของ config เพี้ยน; ใช้หมายเหตุใน README (T-12) แทน |

## 7. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

> ทดสอบ **ครั้งเดียวหลัง T-01..T-12 เสร็จครบ** ไม่ต้องทดสอบทีละ task

```json
{
  "final_acceptance": [
    "cd backend && go build ./... และ go test ./... ผ่านทั้งหมด (ไม่มี test ใดถูกลบทิ้งเพื่อให้ผ่าน)",
    "cd frontend && yarn build และ yarn lint ผ่าน ไม่มี error ใหม่",
    "bash build.sh สร้าง ./pigate ได้สำเร็จ",
    "diff docs/openapi.yaml frontend/public/openapi.yaml ไม่มีความต่าง",
    "รัน ./pigate -mock=true -port=8081 -db=/tmp/t.db แล้ว log แสดง 'DNS Stats Max Domains/IPs-per-domain: 1000 / 32'",
    "GET /api/statistics/capacity คืน rings ครบ 10 ตัว ลำดับคงที่ ลงท้ายด้วย dns.domainIpsPerDomain ที่มี capSource='dns-stats-max-ips-per-domain' และ cap=32",
    "GET /api/statistics/dns คืน field ใหม่ domainIndexTruncated และ field truncated ไม่เปลี่ยนค่าตามสถานะของ index โดเมน→IP อีกต่อไป",
    "สร้างไฟล์ config ที่มี dns-stats-max-ips-per-domain=8 แล้วรันด้วย -config=ไฟล์นั้น -> log แสดง '... / 8' (พิสูจน์ว่า config file ยังชนะ code default); เปลี่ยนเป็น 99 -> ขึ้น warning 'out of range (2..64), using default 32' และใช้ 32",
    "สถานการณ์บั๊กเดิม (ทดสอบด้วย unit test หรือ mock DNS log): ป้อน answer event ให้โดเมนเดียวเกิน 32 IP แล้ว (ก) GET /api/statistics/dns -> truncated=false, domainIndexTruncated=false (ข) GET /api/statistics/dns/domain?domain=<โดเมนนั้น> -> ipsTruncated=true (ค) GET /api/statistics/dns/domain?domain=<โดเมนอื่นที่มี IP เดียว> -> ipsTruncated=false (ง) GET /api/statistics/capacity -> ring dns.domainIpsPerDomain มี current=32, cap=32, truncated=true ส่วน dns.domainIps ยังเป็นเปอร์เซ็นต์ต่ำตามจำนวนโดเมนจริง",
    "หน้า UI: เปิด /statistics/dns ในสถานการณ์ข้างบน -> ไม่มี banner 'จำนวนคู่ (โดเมน, เครื่อง) ... เกินขีดจำกัด' ขึ้นมาโดยไม่มีเหตุ; pill Capacity ของกลุ่ม dns ขึ้นสีแดง และกด 'ดูเพิ่มเติม' แล้วหน้า Capacity แสดงแถว 'DNS resolved IPs ต่อโดเมน (สูงสุด)' เป็นสีแดง 100% (คำเตือนกับหลักฐานตรงกัน ไม่ขัดกันเองอีก)",
    "ทำให้ index ชน dns-stats-max-domains (ป้อนโดเมนใหม่เกิน cap) -> GET /api/statistics/dns มี domainIndexTruncated=true และหน้า UI ขึ้น banner ใหม่ (ข้อความเรื่องดัชนีโดเมน→IP) แยกจาก banner เดิม",
    "คำเตือนดับเองได้: หลังหยุดป้อนเหตุการณ์ที่ชน cap นานกว่า TTL (ตั้ง TTL สั้นในเทสต์) -> domainIndexTruncated กลับเป็น false โดยไม่ต้อง restart",
    "เรียก SetDomainIPsLimits ด้วย cap ที่สูงขึ้น -> latch ถูก reset ทันที (domainIndexTruncated=false)",
    "UI ทั้งหมดที่แก้ผ่านทั้ง dark และ light mode, ไม่มี hardcoded palette class, ไม่มี shadow-*/backdrop-blur-*",
    "grep ทั้ง repo แล้วไม่เหลือค่า 16 ที่หมายถึง dns-stats-max-ips-per-domain (code, comment, README, pigate.conf.example, openapi)",
    "ไม่มี exec.Command ใหม่, ไม่มี dependency ใหม่ (go.mod/yarn.lock ไม่เปลี่ยน), ไม่มี goroutine/ticker ใหม่",
    "งานทั้งหมดอยู่บน branch feat/statistics-capacity-visibility (ไม่มี commit ตรงเข้า main สำหรับโค้ด)"
  ]
}
```
