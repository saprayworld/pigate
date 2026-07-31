# Statistics — Top Queried Domain (DNS) + IP→Domain Enrichment

> แผนงานสำหรับ issue #106 (ต่อจาก PR #108 ที่ทำหน้า Statistics เสร็จแล้ว) โดยเปิด `log-queries`
> ของ dnsmasq ให้เขียน log ลง **tmpfs (`/run`)** แล้วให้ pigate tail + parse + aggregate
> เป็น RAM-only 2 อย่างพร้อมกัน:
> 1. **การ์ดใหม่ "Top Queried Domains"** (จากบรรทัด `query[...]`)
> 2. **เติมชื่อโดเมนให้ IP ปลายทาง** ในการ์ด **Top Destinations / Top Conversations** ที่มีอยู่แล้ว
>    (จากบรรทัด `reply/cached ... is <IP>`) — แนวเดียวกับที่ FortiGate ทำในฐานะ DNS proxy ในเส้นทาง
>
> เป็น **kernel capability ใหม่ 1 ตัว** (`DNSServerManager.WatchDNSLog`) จึงต้องมีทั้ง real และ mock
>
> วันที่เขียน: 2026-07-30 (แก้ไข 2 รอบวันเดียวกันตามที่เจ้าของโปรเจกต์สั่ง: เพิ่มขอบเขต IP→Domain,
> และทำ TTL/ขนาด cache ให้ตั้งค่าได้จาก UI)
> Branch ที่จะใช้: `feat/statistics-dns-top-domain` (ตั้งต้นจาก `main`)
> README Feature Status: แถว "Statistics" ยังเป็น Completed เหมือนเดิม (เพิ่มการ์ด+คอลัมน์ในหน้าเดิม)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
- การ์ดใหม่ "Top Queried Domains" ในหน้า `/logs/statistics` ใช้ window 1h/24h เดียวกับการ์ดอื่น
- **การ์ด Top Destinations / Top Conversations เดิมแสดงชื่อโดเมนของ IP ปลายทาง** เมื่อรู้จัก
  (เช่น `142.250.80.46` → `www.youtube.com`) และแสดง IP เปล่า ๆ เหมือนเดิมเมื่อไม่รู้จัก
- ใช้ endpoint เดิม `GET /api/statistics/traffic?window=1h|24h` (เพิ่มฟิลด์ใน response ไม่เพิ่มเส้นใหม่)
- **ปิดเป็นค่าเริ่มต้น (opt-in)** — ผู้ใช้ต้องเปิดสวิตช์ "เก็บสถิติ DNS query" ในหน้า DNS Server ก่อน
  (เหตุผลด้านความเป็นส่วนตัว ดู §5 ข้อ 1) และสวิตช์ตัวเดียวคุมทั้ง 2 ฟีเจอร์
- **TTL และขนาดสูงสุดของ reverse cache ปรับได้จากหน้า DNS Server** (ไม่ใช่ค่าคงที่ในโค้ด)
  โดยค่าเริ่มต้นที่ส่งมอบคือ 60 นาที / 4096 entry และ **ปรับแล้วมีผลทันทีโดยไม่ต้อง restart
  อะไรเลย** (ดู §2.1 และ §5 ข้อ 18)
- **ห้ามทำให้การ์ดเดิมพัง** — Top Destination/Conversation ต้องทำงานได้เหมือนเดิมทุกประการเมื่อ
  สวิตช์ปิด (ข้อมูลมาจาก conntrack ไม่ได้มาจาก DNS — ดู §5 ข้อ 12)
- ทำงานได้ทั้ง `-mock=true` (มี log สังเคราะห์ทั้ง query และ reply) และของจริง

**นอกขอบเขต (ตัดชัดเจน)**
- **ไม่ทำ per-client drill-down** (ดูว่าเครื่องไหนถามโดเมนอะไร) — เก็บแค่ยอดรวมต่อโดเมน
  เพราะ (ก) เป็นข้อมูลอ่อนไหวมาก (ข) ต้องเก็บ set ต่อ bucket = RAM บาน → เป็นงานคนละก้อน
- **ไม่ทำ DNS blocking / blocklist / policy ตามโดเมน** — แผนนี้อ่านอย่างเดียว
  ชื่อโดเมนที่ได้ **ใช้เพื่อ "แสดงผล" เท่านั้น** ห้ามนำไปสร้าง rule/lookup ย้อนเข้า firewall (§5 ข้อ 6)
- **ไม่ parse TTL จริงของ DNS record** — บรรทัด log ไม่มี TTL → ใช้ TTL ของ cache เอง (ตั้งค่าได้, §2.1)
- **ไม่ทำให้ TTL/cap เป็น flag ของ `main.go`/ไฟล์ config** — เหตุผลใน §2.1
- **ไม่อ่าน systemd journal** — go.mod ไม่มี library อ่าน journal (sd-journal ต้อง cgo) และห้าม exec
- **ไม่เก็บ log ลง SD card / SQLite เด็ดขาด** (tech_stack_design.md §8)
- ไม่แตะ nftables/NFLOG/conntrack, ไม่แตะการ์ด Dashboard "Detailed" (`TopTalker` เป็นคนละ DTO)

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-07-30)

| ส่วน | สถานะ | ที่อยู่ |
|---|---|---|
| `DNSServerManager` | มีแค่ `ApplyZones` / `ClearCache` — **ไม่มีท่ออ่าน DNS log** | `kernel/interfaces.go:154-157` |
| dnsmasq base config | เขียน directive process-global (`domain`, `bind-dynamic`, `enable-dbus`) — **ไม่มี log-*** | `kernel/dnsmasq_base.go:43-49` |
| dnsmasq DNS config | `buildDNSConfig()` เป็น pure function เขียน `/etc/dnsmasq.d/pigate-dns.conf` แล้ว restart ผ่าน D-Bus | `kernel/dns_server.go:35-69, :215-222` |
| D-Bus ของ dnsmasq | มีแต่ signal `DhcpLease*` — **ไม่มี signal ของ DNS query/reply** | `kernel/dnsmasq_base.go:37-42` |
| mock DNS server | `MockDNSServerManager` เป็น no-op ล้วน | `kernel/mock.go:382-396` |
| DB ของ DNS Server | ตาราง `dns_server_settings` มีคอลัมน์เดียว (`interfaces`) | `db/connection.go:359-362`, `db/repository.go:2663-2681` |
| **precedent ของ "ค่าตัวเลขที่ผู้ใช้ตั้งเองได้"** | `dhcp_configs.lease_time INTEGER NOT NULL DEFAULT 86400` — เก็บใน DB, แก้จากหน้า DHCP Server, validate ที่ `model/` | `db/connection.go:322`, `model/types.go:299`, `model/dns_validate.go:182 ValidateDhcpConfig` |
| **precedent ของ flag/ไฟล์ config** | `internal/config.Config` มีแต่ค่าระดับ ops/boot (port, db path, mock, docker-compat, tls dir) — **ไม่มีค่าที่เป็น "การตั้งค่าฟีเจอร์ให้ผู้ใช้ปรับ" เลยสักตัว** | `internal/config/config.go:38-49` |
| Top Destinations | ใช้ `buildTopHosts()` ตัวเดียวกับ Top Sources — **ชื่อมาจาก DHCP lease เท่านั้น** IP ภายนอกจึงโชว์ IP เปล่า | `service/statistics.go:185-211`, `traffic_stats.go:1015 hostnameFor` |
| Top Conversations | `dstHostname` ก็มาจาก `hostnameFor()` เหมือนกัน → IP ปลายทางภายนอกโชว์ IP เปล่า | `service/statistics.go:216-260` |
| รูปแบบ IP ปลายทาง | มาจาก `net.IP.String()` ของ netlink (canonical form) | `kernel/real_traffic_account.go:98-99` |
| DTO ที่ต้องเติมฟิลด์ | `TopHost` (ใช้ทั้ง Source/Destination), `TopConversation` | `model/statistics.go:14-41` |
| Statistics pipeline | `StatisticsService` + deny ring RAM-only + `GetStatistics(window)` | `service/statistics.go:59-179` |
| หน้า UI Statistics | 4 การ์ด; Top Destinations ใช้ `HostLabel`, Conversations เป็น `<Table>` 5 คอลัมน์ | `Statistics.tsx:95-200, :273-406` |
| หน้า DNS Server | การ์ด "Listen Interfaces" + ปุ่ม Apply, มี `<Switch>` ใช้อยู่แล้ว **ไม่มีการแยกหน้า "restart-required" กับ "live-tunable"** | `DnsServer.tsx:22, :418-447, :586, :816` |
| สิทธิ์ที่มีอยู่แล้ว | `/etc/dnsmasq.d` มี ACL ให้ pigate เขียนได้, `/run/pigate` ถูกสร้างโดย `RuntimeDirectory=pigate` | `install.sh:146-148, :308-312, :586-587` |

**สรุป:** งานจริงกระจุกที่ **kernel layer (ท่ออ่าน log ใหม่)** + ring/cache ใน RAM 2 ก้อน
ส่วน API/หน้าเว็บเป็นการต่อยอดของเดิม และ **ไม่ต้องขอสิทธิ์ใหม่ใน install.sh เลย**

## 2. แนวทางเทคนิคที่เลือก

```
dnsmasq (log-queries) ──เขียน──► /run/pigate/dnsmasq-queries.log   [tmpfs = RAM ไม่กิน SD]
                                        │ tail ทุก 1s (os.File + offset, ไม่มี dep ใหม่)
        kernel: RealDNSServerManager.WatchDNSLog(ctx, cb) ──► model.DNSLogEvent
                        ├── Kind="query"  ─► StatisticsService.RecordDNSEvent ─► domain ring (5m × 288)
                        └── Kind="answer" ─► DNS reverse cache  (IP → domain, TTL/cap ตั้งค่าได้)
                                                        │ อ่านตอนประกอบ response
        GET /api/statistics/traffic ─► topDomains[]  +  topDestinations[].domain
                                                     +  topConversations[].dstDomain
```

**directive ที่จะเพิ่ม** (เมื่อผู้ใช้เปิดสวิตช์เท่านั้น) ลงใน `pigate-dns.conf`:
```
log-queries
log-facility=/run/pigate/dnsmasq-queries.log
log-async=25
```

**รูปแบบบรรทัดที่ต้อง parse** (ตอน log ลงไฟล์ prefix คือ "เดือน วัน เวลา dnsmasq[pid]: "):
```
Jul 30 12:00:01 dnsmasq[1234]: query[A] www.example.com from 192.168.1.101   ← Kind=query
Jul 30 12:00:01 dnsmasq[1234]: forwarded www.example.com to 8.8.8.8          ← ทิ้ง
Jul 30 12:00:01 dnsmasq[1234]: reply www.example.com is <CNAME>              ← ทิ้ง (ไม่ใช่ IP)
Jul 30 12:00:01 dnsmasq[1234]: reply cdn.example.net is 93.184.216.34        ← Kind=answer
Jul 30 12:00:01 dnsmasq[1234]: cached www.example.com is 93.184.216.34       ← Kind=answer
Jul 30 12:00:01 dnsmasq[1234]: reply example.com is NXDOMAIN / NODATA-IPv6   ← ทิ้ง (ไม่ใช่ IP)
Jul 30 12:00:01 dnsmasq[1234]: config pigate.local is 192.168.1.1            ← Kind=answer (ยอมรับได้)
```
**กติกา parser (ไม่ต้องใช้ regex เลย)** — ตัด prefix `dnsmasq[pid]: ` ออกก่อน แล้ว:
- ขึ้นต้นด้วย `query[` → เหตุการณ์ query (domain = field ถัดไป, client = field หลัง `from`)
- มิฉะนั้น ถ้าแยก field แล้วได้ `<source> <domain> is <value>` (field[2] == "is") **และ
  `netip.ParseAddr(value)` ผ่าน** → เหตุการณ์ answer; ถ้า parse ไม่ผ่าน (`<CNAME>`, `NXDOMAIN`,
  `NODATA-IPv4/6`, `<REDACTED>`) → **ทิ้งทั้งบรรทัด**
- ไม่ต้องแยกกรณี `reply` / `cached` / `config` / `/etc/hosts` — เงื่อนไข "field[2]==is และ value เป็น IP"
  ครอบคลุมทั้งหมดด้วย branch เดียว จึง **ไม่ต้องเพิ่ม regex ตัวที่สอง**
- หนึ่ง query ที่มีหลาย A record จะได้หลายบรรทัด `is <IP>` → เก็บทุกบรรทัด (หลาย IP ชี้โดเมนเดียวกันได้)

**เหตุผลของการออกแบบ reverse cache**
- **key ต้อง normalize ด้วย `netip.ParseAddr(v).String()`** ก่อนเก็บ เพื่อให้ตรงกับรูปแบบ IP ที่
  conntrack ส่งมา (`net.IP.String()` — สำคัญมากกับ IPv6 ที่เขียนได้หลายแบบ) ไม่งั้น lookup พลาดหมด
- **TTL นับจากครั้งล่าสุดที่เห็น** (เห็นซ้ำ = ต่ออายุ) — สั้นพอที่ IP ของ CDN/cloud LB
  ที่ถูกนำกลับมาใช้กับโดเมนอื่นจะไม่ค้างนาน แต่ยาวพอให้ window 1h มีชื่อครบ
  (window 24h จะมีบางแถวไม่มีชื่อ = ยอมรับได้ ดีกว่าติดป้ายผิด)
- **cap จำนวน entry** — เต็มเมื่อไร: ลบตัวหมดอายุก่อน ถ้ายังเต็มให้ลบตัวที่ `lastSeen`
  เก่าสุด (LRU) — กัน RAM บานจากโดเมน/IP ที่ผู้โจมตีสร้างขึ้นได้ไม่จำกัด
- **IP เดิมชี้โดเมนใหม่ = ทับของเดิม (last writer wins)** พร้อม flag ว่าเคยเห็นหลายชื่อ
  → UI แสดงชื่อล่าสุดเท่านั้น และ §5 ข้อ 8/9 อธิบายว่าทำไมถึงไม่ใช่ค่าที่เชื่อถือได้ 100%
- **CNAME chain**: dnsmasq ผูก IP ไว้กับ **ชื่อปลายทางของ chain** (เช่น `*.googlevideo.com`)
  ไม่ใช่ชื่อที่ผู้ใช้พิมพ์ — เราจะ **แสดงตามที่ dnsmasq บอกเท่านั้น ห้ามเดา/ห้ามแต่งชื่อขึ้นเอง**

### 2.1 TTL / ขนาด cache: เก็บที่ไหน (ตัดสินใจแล้ว)

**เก็บใน DB ตาราง `dns_server_settings` แถวเดียวกับ `query_logging` และแก้จากหน้า DNS Server**
— ไม่ใช่ flag ของ `main.go`/ไฟล์ `pigate.conf`

| ค่า | ชื่อฟิลด์ (Go / JSON / คอลัมน์ DB) | ค่าเริ่มต้น | ขอบเขตที่ยอมรับ |
|---|---|---|---|
| อายุของ mapping | `DNSCacheTTLMinutes` / `dnsCacheTtlMinutes` / `dns_cache_ttl_minutes` | **60** | **1–1440** นาที (1 นาที – 24 ชม.) |
| จำนวน entry สูงสุด | `DNSCacheMaxEntries` / `dnsCacheMaxEntries` / `dns_cache_max_entries` | **4096** | **128–65536** |

**เหตุผลที่เลือก DB + UI (ไม่ใช่ flag/ไฟล์ config)**
1. **มี precedent ตรง ๆ อยู่แล้ว** — `dhcp_configs.lease_time` เป็นค่าตัวเลขที่ผู้ใช้ตั้งเองได้
   เก็บใน DB, แก้จากหน้าเว็บ, validate ที่ `model/` (`db/connection.go:322`,
   `model/dns_validate.go:182`) นี่คือรูปแบบมาตรฐานของ "ค่าปรับแต่งฟีเจอร์" ในโปรเจกต์นี้
2. **`internal/config` เป็นค่าระดับ ops/boot ล้วน** (port, db path, mock, tls dir, docker-compat)
   — ไม่มีสักตัวที่เป็นการตั้งค่าฟีเจอร์ให้ผู้ใช้ปรับ (`config.go:38-49`) การยัดค่านี้ลงไปจะ
   ทำให้ผู้ใช้ต้อง ssh เข้าเครื่อง + แก้ไฟล์ + **restart pigate** เพื่อปรับค่าที่เป็นแค่พารามิเตอร์
   ของ map ในหน่วยความจำ ซึ่งเกินจำเป็นและขัดกับข้อ 4 ข้างล่าง
3. **อยู่ที่เดียวกับ `query_logging`** — ทั้งสามค่าเป็นเรื่องเดียวกัน (ฟีเจอร์สถิติ DNS)
   ผู้ดูแลไม่ต้องตามหาสองที่ และสำรวจแล้วโปรเจกต์นี้ **ไม่มี** ธรรมเนียมแยก
   "ค่าที่ต้อง restart" กับ "ค่าที่ปรับสด" ออกคนละหน้า (หน้า DNS Server/DHCP Server ปนกันอยู่แล้ว)
4. 🎁 **ปรับแล้วมีผลทันที ไม่ต้อง restart อะไรเลย** — ต่างจากสวิตช์ `query_logging` ที่ต้องเขียน
   config + restart dnsmasq เพราะสองค่านี้เป็นพารามิเตอร์ของ map ฝั่ง Go ล้วน ๆ
   (`ApplyZones` ไม่ต้องถูกเรียกด้วยซ้ำ — ดู T-10) จึงเป็นข้อดีที่ควรเขียนบอกผู้ใช้ใต้ช่องกรอก
5. **Backup/Restore ได้ฟรี** — อยู่ในตารางที่ `backup_repo.go` จัดการอยู่แล้ว

**เพดานคือของจริง ไม่ใช่พิธีกรรม:** 1 entry ≈ key IP (~40 B) + domain (≤253 B แต่ปกติ ~30 B) +
struct/overhead ของ map ≈ **~200 B** → ที่ค่า default 4096 ≈ **< 1 MB**,
ที่เพดานสูงสุด 65536 ≈ **~13 MB** (worst case) ซึ่งยังอยู่ในงบของบอร์ด Pi 5 และ **ยังคงเป็นค่าที่
มีขอบเขตชัดเจน** ตามข้อกำหนด "ห้ามโตไม่จำกัด" ของ T-08 — ผู้ดูแลจะตั้งเป็น 0/ติดลบ/ล้านไม่ได้

**ทางเลือกที่ตัดทิ้ง**
- **flag `-dns-cache-ttl` / คีย์ในไฟล์ config** — ตามเหตุผลข้อ 2/4 ข้างบน (ต้อง restart, คนละที่กับ
  สวิตช์ที่คุมฟีเจอร์เดียวกัน, ไม่เข้ากับ precedent ของ `lease_time`)
- **ปล่อยเป็น const ตามเดิม** — เจ้าของโปรเจกต์สั่งให้ปรับได้ (รอบแก้ไขที่ 2)
- **ตารางใหม่สำหรับ settings ของสถิติ** — เกินจำเป็น, `dns_server_settings` เป็นแถวเดี่ยว (id=1) อยู่แล้ว

**ทางเลือกอื่นที่ตัดทิ้ง (คงเดิม)**
- **tmpfs `/run/pigate/`** — dir นี้ systemd สร้างให้อยู่แล้ว (owner `pigate`) ไม่ต้องขอสิทธิ์เพิ่ม
  และเป็น RAM ล้วน → ไม่ขัดข้อกำหนดถนอม SD card
- **ให้ pigate สร้างไฟล์ log ล่วงหน้าเอง** (0640) ก่อน restart dnsmasq → ไฟล์เป็นของ `pigate`
  จึง `truncate` เองได้ (ถ้าปล่อยให้ root สร้าง จะ truncate ไม่ได้ และการ unlink แทนจะทำให้
  dnsmasq เขียนลง inode ที่ถูกลบ = RAM รั่วเงียบ ๆ)
- **ตัด systemd journal / FIFO / directive ใน `pigate-base.conf`** — เหตุผล: ไม่มี reader ที่ไม่ใช้ cgo,
  FIFO ที่ยังไม่มีผู้อ่านให้ ENXIO ทำ dnsmasq ล้มทั้ง DNS+DHCP, และ base ถูกเขียนทับโดย DHCP path
  (`ensureDnsmasqBaseConfig()` ไม่มีพารามิเตอร์) จะทำให้ directive หายเงียบ ๆ
- **ตัดการทำ reverse DNS (PTR) เอง** — ช้า, สร้าง traffic เอง, และให้ชื่อ infra ที่ไม่มีความหมาย
- **แบบแผนที่ให้ลอก:** `DhcpManager.WatchLeases` (watcher ที่ block จนกว่า ctx จบ),
  `service/statistics.go` deny ring, `model/dns_validate.go` (validator), `dhcp_configs.lease_time` (ค่าปรับได้)

## 3. ขั้นตอนการทำ (เรียงตาม dependency)

### T-01 — DTO + validator
**ไฟล์:** `model/statistics.go` (แก้), `model/dns_server.go` (แก้), `model/dns_validate.go` (แก้)
- `TopHost` เพิ่ม `Domain string \`json:"domain"\`` — **ชื่อโดเมนของ IP นี้ตามที่ DNS ตอบ**
  ค่าว่าง = ไม่รู้จัก (ห้ามใส่ IP ซ้ำลงไปเพื่อให้ไม่ว่าง)
- `TopConversation` เพิ่ม `DstDomain string \`json:"dstDomain"\`` (ฝั่ง source ไม่ต้อง — เป็น LAN)
- เพิ่ม `type TopDomain struct { Domain string; QueryType string; Count uint64; Percent float64 }`
- `TrafficStatistics` เพิ่ม `TopDomains []TopDomain`, `DNSQueries uint64`,
  `DNSLoggingEnabled bool`, `DNSTruncated bool`
- เพิ่ม event ที่ kernel ส่งขึ้นมา (ไว้ใน `dns_server.go`):
  ```go
  const DNSLogQuery, DNSLogAnswer = "query", "answer"
  type DNSLogEvent struct {
      Kind      string // DNSLogQuery | DNSLogAnswer
      Domain    string
      QueryType string // เฉพาะ query ("A"/"AAAA"/"HTTPS"/...)
      ClientIP  string // เฉพาะ query
      AnswerIP  string // เฉพาะ answer (normalize แล้ว)
  }
  ```
- `DNSServerSettings` เพิ่ม 3 ฟิลด์:
  ```go
  QueryLogging       bool `json:"queryLogging"`
  DNSCacheTTLMinutes int  `json:"dnsCacheTtlMinutes"` // default 60, 1..1440
  DNSCacheMaxEntries int  `json:"dnsCacheMaxEntries"` // default 4096, 128..65536
  ```
- 🔒 `func ValidateDNSServerSettings(s DNSServerSettings) error` ใน `dns_validate.go`
  (ลอกสไตล์ `ValidateDhcpConfig` `:182`): ค่านอกช่วง/0/ติดลบ → error ที่อ่านรู้เรื่อง
  ประกาศ const กลางไว้ที่ `model` ให้ backend/เทสต์/frontend อ้างชุดเดียวกัน:
  `DNSCacheTTLDefault=60`, `DNSCacheTTLMin=1`, `DNSCacheTTLMax=1440`,
  `DNSCacheEntriesDefault=4096`, `DNSCacheEntriesMin=128`, `DNSCacheEntriesMax=65536`
**acceptance:** `go build ./...` ผ่าน, ฟิลด์เดิมทุกตัวคงเดิม (เพิ่มอย่างเดียว)

### T-02 — DB: สวิตช์ + ค่าปรับแต่ง cache
**ไฟล์:** `db/connection.go`, `db/repository.go`, `db/backup_repo.go`, `service/backup.go`
1. migration แบบเดียวกับ `subtype` ที่ `connection.go:157-167` — เช็กทีละคอลัมน์ว่ามีหรือยัง
   (DB ที่ติดตั้งไปแล้วต้องอัปเกรดได้เอง ไม่ต้อง restore):
   ```sql
   ALTER TABLE dns_server_settings ADD COLUMN query_logging         INTEGER NOT NULL DEFAULT 0;
   ALTER TABLE dns_server_settings ADD COLUMN dns_cache_ttl_minutes INTEGER NOT NULL DEFAULT 60;
   ALTER TABLE dns_server_settings ADD COLUMN dns_cache_max_entries INTEGER NOT NULL DEFAULT 4096;
   ```
2. `repository.go:2663-2681` เพิ่ม `GetDNSServerSettings()` / `SetDNSServerSettings()`
   (คงของเดิม `Get/SetDNSServerInterfaces` ไว้ ไม่ลบ) — **ตอนอ่าน ถ้าค่าใน DB หลุดช่วง
   (เช่น ถูกแก้ด้วยมือ/ไฟล์ backup แปลก ๆ) ให้ clamp กลับเป็น default พร้อม log warning
   ไม่ใช่คืนค่าที่ทำให้ RAM บาน**
3. backup/restore: `service/backup.go:116` ใส่ทั้ง 3 ค่า; `db/backup_repo.go:258-262`
   restore คอลัมน์ใหม่ — ไฟล์ backup รุ่นเก่าไม่มีฟิลด์ (0/หายไป) → **ใช้ default 60/4096
   ไม่ใช่ 0** (0 = ปิด cache ทั้งใบโดยไม่ได้ตั้งใจ)
**acceptance:** `go build ./...` ผ่าน; เปิด DB เก่าแล้วไม่ error และได้ค่า default ครบ

### T-03 — kernel interface 🔒
**ไฟล์:** `kernel/interfaces.go` (`:151-157`)
```go
type DNSServerManager interface {
    ApplyZones(zones []model.DNSZone, interfaces []string, upstreamServers []string, queryLog bool) error
    ClearCache() error
    // WatchDNSLog สตรีมทั้ง query และ answer (reply/cached ที่ค่าเป็น IP) จาก dnsmasq query log
    // Blocking จนกว่า ctx จบ; cb ต้องคืนค่าเร็ว (อยู่บน read loop)
    // ไฟล์ log ไม่มี/สวิตช์ปิดอยู่ = ไม่ใช่ error ให้รอเงียบ ๆ
    WatchDNSLog(ctx context.Context, cb func(model.DNSLogEvent)) error
}
```
> `ApplyZones` รับเพิ่มแค่ `queryLog` — **TTL/cap ไม่ต้องส่งลงมาถึง kernel เลย** เพราะเป็น
> พารามิเตอร์ของ cache ฝั่ง service ล้วน ๆ (ไม่มีผลต่อไฟล์ config ของ dnsmasq)
> และเลือก **stream เดียว 2 Kind** แทน 2 method เพราะมาจากไฟล์เดียว/ลูปอ่านเดียว
**acceptance:** `go build ./...` จะยัง error ที่ implementation (คาดหวัง) → จบเมื่อ T-04..T-06 เสร็จ

### T-04 — parser + ค่าคงที่ (ไฟล์ใหม่ ไม่มี build tag เพื่อให้เทสต์ได้ทุก OS)
**ไฟล์ใหม่:** `backend/internal/kernel/dns_query_log.go`
- `const DNSQueryLogPath = "/run/pigate/dnsmasq-queries.log"`, `maxQueryLogBytes = 8 << 20`,
  `queryLogPollInterval = 1 * time.Second`, `maxQueryLogReadPerTick = 2 << 20`
  (ชุดนี้เป็นค่าคงที่ระดับ kernel/ops ไม่เปิดให้ผู้ใช้ปรับ — คนละเรื่องกับ TTL/cap ใน §2.1)
- `func parseDNSLogLine(line string) (model.DNSLogEvent, bool)` — ทำทั้ง 2 branch ตามกติกาใน §2
- 🔒 `func sanitizeDomain(s string) (string, bool)` — โดเมนเป็น **input ที่ไม่น่าเชื่อถือทั้ง 2 ทาง**
  (ทั้งชื่อที่ client ถาม และชื่อใน CNAME chain ที่เซิร์ฟเวอร์ปลายทางเป็นคนกำหนด): lower-case,
  ตัด `.` ท้าย, ยาว ≤ 253, อนุญาตเฉพาะ `a-z 0-9 . - _ *` (ครอบคลุม punycode `xn--`)
  เจออักขระอื่น (ควบคุม, `<`, `"`, newline, ช่องว่าง) → **ทิ้งทั้งบรรทัด** ห้าม escape เอง
- `AnswerIP` ต้องผ่าน `netip.ParseAddr` แล้วเก็บ `addr.String()` (normalize ให้ตรงกับ conntrack)
- `ClientIP` ตรวจด้วย `netip.ParseAddr` เช่นกัน parse ไม่ผ่านให้เก็บ "" (ไม่ทิ้งทั้งบรรทัด)
**acceptance:** `go build ./...` ผ่าน; ทุกฟังก์ชันเป็น pure ไม่มี I/O

### T-05 — real implementation 🔒
**ไฟล์:** `kernel/dns_server.go` (แก้), ไฟล์ใหม่ `kernel/real_dns_query_log.go` (`//go:build linux`)
1. `buildDNSConfig(...)` รับ `queryLog bool` เพิ่ม — เมื่อ true ให้ append 3 directive ตาม §2
   (path เป็นค่าคงที่ในโค้ด **ห้ามรับจาก input ใด ๆ**)
2. `ApplyZones`: ถ้า `queryLog == true` → สร้างไฟล์ log ล่วงหน้า
   (`os.OpenFile(DNSQueryLogPath, O_CREATE|O_WRONLY|O_APPEND, 0640)` แล้วปิดทันที) ก่อนเขียน config;
   ถ้า `false` → `os.Remove(DNSQueryLogPath)` (ลบประวัติทิ้งทันที §5 ข้อ 1)
   ทั้งสองกรณีพลาดให้ log warning **ไม่ล้ม ApplyZones** (DNS ต้องขึ้นก่อนเสมอ)
3. `WatchDNSLog`: loop ทุก `queryLogPollInterval`
   - เปิดไฟล์ (ไม่มี = รอ tick ถัดไปเงียบ ๆ ห้าม log ทุกวินาที), อ่านจาก offset ที่จำไว้
   - `size < offset` (โดน truncate / dnsmasq เปิดไฟล์ใหม่) → reset offset = 0
   - ค้างเกิน `maxQueryLogReadPerTick` → กระโดดไปท้ายไฟล์ + ตั้ง flag "dropped"
   - อ่านทีละบรรทัดด้วย `bufio.Scanner` (จำกัด buffer), บรรทัดที่ค้างครึ่ง ๆ เก็บต่อ tick หน้า
   - `size > maxQueryLogBytes` → `os.Truncate(path, 0)` (dnsmasq เปิดแบบ `O_APPEND` เขียนต่อได้เอง
     — **ต้องยืนยันบนเครื่องจริง** §5 ข้อ 5)
   - `cb(event)` ต้องเร็ว ไม่ block และ **ห้าม log โดเมนลง log ของ pigate เอง** (§5 ข้อ 2)
**acceptance:** `go build ./...` ผ่าน, ไม่มี `exec.Command` ใหม่ (ของเดิม `dnsmasq --test` คงไว้)

### T-06 — mock implementation
**ไฟล์:** `kernel/mock.go` (`:382-396`)
- `ApplyZones(..., queryLog bool)` — แค่ log ค่าที่ได้รับ (no-op เหมือนเดิม)
- `WatchDNSLog` — ticker ~2s สังเคราะห์ **ทั้ง query และ answer**:
  - query: โดเมนคงที่ (`youtube.com`, `googlevideo.com`, `netflix.com`, `line-apps.com`,
    `cdn.jsdelivr.net`) จาก client 192.168.1.101/102/105 ความถี่ไม่เท่ากัน
  - answer: **map ให้ตรงกับ dstIP ใน `mockFlowTemplates`** (`142.250.80.46` → `youtube.com`,
    `173.194.76.94` → `googlevideo.com`, `151.101.1.69` → `cdn.jsdelivr.net` และเว้น
    `8.8.8.8`/`203.0.113.55` ไว้ไม่ map เพื่อทดสอบเคส "ไม่รู้จัก")
- **ห้ามแตะ filesystem ของเครื่อง dev เด็ดขาด**
**acceptance:** `go build ./...` ผ่าน; `-mock=true` ไม่มีการเปิดไฟล์ใด ๆ

### T-07 — service: domain ring (Top Queried Domains)
**ไฟล์ใหม่:** `backend/internal/service/dns_query_stats.go`; **แก้:** `service/statistics.go`, `service/dns_server.go`
- ring ใหม่ในโครงเดียวกับ deny ring: bucket 5 นาที × 288, `domainCount map[string]uint64`
  (cap `maxTrackedDomains = 500`/bucket — ค่าคงที่ ไม่เปิดให้ปรับ), `typeByDomain`, `queries uint64`
- `func (s *StatisticsService) RecordDNSEvent(ev model.DNSLogEvent)` — จุดรับเดียวจาก watcher:
  `Kind == query` → domain ring; `Kind == answer` → ส่งต่อ reverse cache (T-08)
  ต้องเป็น mutex + map increment เท่านั้น (ห้าม I/O ห้าม log ต่อ event)
- `SetDNSLoggingEnabled(bool)` + `ClearDNSStats()` (ปิดสวิตช์ → ล้าง **ทั้ง ring และ reverse cache**)
- `GetStatistics()` เติม `TopDomains` (percent คิดจาก **จำนวน query ใน window** ไม่ใช่ `observedBytes`),
  `DNSQueries`, `DNSLoggingEnabled`, `DNSTruncated`; sort deterministic (count desc → domain asc)
- `service/dns_server.go`: `ApplyAll()` อ่าน `QueryLogging` จาก repo ส่งต่อให้ `ApplyZones`
  (**ไม่ต้องส่ง TTL/cap** — ไม่เกี่ยวกับ dnsmasq)
**acceptance:** `go build ./...` ผ่าน; service นี้ยังไม่เรียก kernel ตรงนอกจากผ่าน manager เดิม

### T-08 — service: DNS reverse cache (IP → domain) 🔒 **[TTL/cap ปรับได้ตอนรัน]**
**ไฟล์ใหม่:** `backend/internal/service/dns_reverse_cache.go`
```go
type dnsReverseCache struct {
    mu      sync.RWMutex
    entries map[string]dnsReverseEntry // key = IP ที่ normalize แล้ว
    ttl     time.Duration              // ปรับได้ตอนรัน (จาก DB) — ไม่ใช่ const
    maxSize int                        // ปรับได้ตอนรัน (จาก DB) — ไม่ใช่ const
}
type dnsReverseEntry struct{ domain string; lastSeen time.Time; multi bool }
```
- `SetLimits(ttlMinutes, maxEntries int)` — **จุดที่ทำให้ค่าปรับได้แบบไม่ต้อง restart**:
  1. clamp ด้วย const จาก `model` (T-01) อีกชั้นหนึ่ง (defense-in-depth ต่อให้ handler พลาด)
  2. เขียนค่าใหม่ใต้ `mu.Lock()`
  3. **ถ้า `maxEntries` ใหม่เล็กลง ต้อง evict ทันทีให้เหลือไม่เกินค่าใหม่** (ลบตัวหมดอายุก่อน
     แล้วไล่ลบตาม `lastSeen` เก่าสุด) — ไม่ใช่รอให้ค่อย ๆ หดเอง ไม่งั้นการ "ลดเพดาน" ไม่มีผลจริง
  4. TTL ที่สั้นลงมีผลทันทีเองโดยไม่ต้องทำอะไร เพราะ `Lookup` ตัดสินหมดอายุจาก `ttl` ปัจจุบัน
- `Put(ip, domain string)` — O(1): normalize key แล้วเขียน; ถ้าเดิมมีชื่ออื่นให้ทับและตั้ง `multi=true`;
  ถ้า `len(entries) >= maxSize` ให้ลบตัวหมดอายุก่อน แล้วจึงลบตัว `lastSeen` เก่าสุด (LRU)
- `Lookup(ip string) string` — คืน "" เมื่อไม่พบ **หรือหมดอายุแล้ว** (ห้ามคืน IP แทน)
- `LookupMany(ips []string) map[string]string` — ใช้ตอนประกอบ response ครั้งเดียวต่อ request
  (RLock ก้อนเดียว ไม่ล็อกทีละแถว)
- `Clear()` — เรียกจาก `ClearDNSStats()` ตอนผู้ใช้ปิดสวิตช์
- กวาดตัวหมดอายุแบบ lazy ใน `Lookup` + กวาดเป็นรอบ (~5 นาที) ใน goroutine เดิมของ watcher
  (ห้ามสร้าง ticker ใหม่ทั้ง service เพื่อเรื่องนี้)
- `StatisticsService` เปิด `SetReverseCacheLimits(ttlMinutes, maxEntries int)` ให้ handler/main เรียก
**เชื่อมเข้า `service/statistics.go`:**
- `buildTopHosts()` (`:185-211`) รับ map `ipDomain` เพิ่ม แล้วเซ็ต `TopHost.Domain`
  (ใช้กับ **ทั้ง** TopSources และ TopDestinations)
- `buildTopConversations()` (`:216-260`) เซ็ต `TopConversation.DstDomain` จาก map เดียวกัน
- ทั้งสองจุด: ไม่พบ → **ปล่อยว่าง ห้ามใส่ IP หรือชื่ออื่นแทน**; `hostnameFor()` เดิมทำงานเหมือนเดิมทุกประการ
**acceptance:** `go build ./...` ผ่าน; `GetStatistics` ยังคืนฟิลด์เดิมครบเมื่อ cache ว่าง

### T-09 — wiring ใน main.go
**ไฟล์:** `backend/cmd/pigate/main.go` (ราว `:218-223` และหลัง watcher ตัวอื่น)
- หลังสร้าง `statisticsService`: อ่าน `repo.GetDNSServerSettings()` ครั้งเดียวแล้วเรียก
  `SetDNSLoggingEnabled(...)` + `SetReverseCacheLimits(...)` (อ่านพลาด → ใช้ default 60/4096
  แล้ว log warning ห้ามทำให้บูตล้ม)
- เพิ่ม goroutine: `go dnsServerManager.WatchDNSLog(ctx, statisticsService.RecordDNSEvent)`
  แบบเดียวกับ watcher NFLOG/lease (error = log ครั้งเดียว)
> **ไม่ต้อง** เพิ่ม `InitApplyConfig` ใหม่ — `DNSServerService.InitApplyConfig()` เดิมจะพา
> directive ใหม่ลง dnsmasq ให้เองตอนบูต
**acceptance:** `-mock=true` แล้ว `curl /api/statistics/traffic` เห็นทั้ง `topDomains` และ
`topDestinations[].domain` มีค่า

### T-10 — API + handler 🔒 (จุดรับ input ตัวเลขจาก client)
**ไฟล์:** `backend/internal/api/handlers.go` (`:3166-3230`)
- `HandleGetDNSServerSettings` คืนทั้ง 3 ฟิลด์ใหม่ (พร้อมค่า default เมื่อ DB ยังไม่เคยตั้ง)
- 🔒 `HandleUpdateDNSServerSettings`:
  1. เรียก `model.ValidateDNSServerSettings(input)` **ก่อนบันทึก** — นอกช่วง → `400` พร้อม
     ข้อความบอกช่วงที่รับได้ (ห้าม clamp เงียบ ๆ ที่ชั้น API ผู้ใช้ต้องรู้ว่าค่าไม่ถูกรับ)
  2. **แยกให้ชัดว่าอะไรต้อง restart dnsmasq**: อ่านค่าเดิมก่อนเขียน (มี pattern grandfather
     ของ interfaces อยู่แล้วที่ `:3201-3212`) แล้ว
     - `interfaces` หรือ `queryLogging` เปลี่ยน → `dnsServerService.ApplyAll()` (เขียน config +
       restart dnsmasq) และถ้า `queryLogging` เปลี่ยน ให้เรียก
       `SetDNSLoggingEnabled()` / `ClearDNSStats()` ตามค่าใหม่
     - **เปลี่ยนแค่ `dnsCacheTtlMinutes`/`dnsCacheMaxEntries` → เรียกแค่
       `statisticsService.SetReverseCacheLimits(...)` ห้ามเรียก `ApplyAll()`**
       (ไม่มีการเขียน config, ไม่มีการ restart dnsmasq, DNS/DHCP ไม่สะดุดเลย — §5 ข้อ 18)
- **ไม่เพิ่ม endpoint ใหม่** — `GET /api/statistics/traffic` เดิมส่งฟิลด์เพิ่มเท่านั้น
- PUT นี้เป็น mutation → ต้องยังโดน `DisableEditMiddleware`/read-only role ตามปกติ
**acceptance:** `go build ./... && go test ./internal/api/...` ผ่าน

### T-11 — เทสต์ backend
**ไฟล์ใหม่:** `kernel/dns_query_log_test.go`, `service/dns_reverse_cache_test.go`;
**แก้:** `service/statistics_test.go`, `kernel/dns_server_test.go`, `api/handlers_test.go`,
เทสต์ของ `model/` (validator)
เคสบังคับ:
1. `parseDNSLogLine`: แยก query ถูก; แยก `reply/cached/config ... is <IP>` เป็น answer ถูก;
   คืน false กับ `forwarded`, `is <CNAME>`, `is NXDOMAIN`, `is NODATA-IPv6`, บรรทัด DHCP, ขยะ
2. answer ที่เป็น IPv6 เขียนแบบย่อ/ยาว → key ที่ได้ตรงกับ `net.IP.String()` ของ conntrack
3. 🔒 `sanitizeDomain` ทิ้งบรรทัดที่มี `<script>`, newline, `\x00`, ช่องว่าง, โดเมน > 253 ตัวอักษร
   — ทดสอบทั้งฝั่ง query และฝั่ง **CNAME/reply**
4. reverse cache: หมดอายุตาม TTL แล้ว `Lookup` คืน ""; เห็นซ้ำแล้วต่ออายุ;
   ใส่เกิน cap → ขนาดไม่เกิน cap และไม่ panic; IP เดิมโดเมนใหม่ → ได้ชื่อล่าสุด + `multi`
5. 🔒 **`SetLimits`**: (ก) ลด `maxEntries` จาก 4096 → 128 ขณะมี 4096 entry → ขนาดลดเหลือ ≤ 128
   **ทันที** (ไม่ต้องรอ Put ครั้งถัดไป) (ข) ลด TTL แล้ว entry เก่ากลายเป็นหมดอายุทันทีที่ Lookup
   (ค) ส่งค่านอกช่วง/0/ติดลบเข้ามา → ถูก clamp เป็น default/ขอบเขต ไม่ทำให้ cache ปิดหรือโตเกิน
6. 🔒 `ValidateDNSServerSettings`: 0, -1, 1441, 127, 65537 → error; 1, 60, 1440, 128, 4096, 65536 → ผ่าน
7. handler: PUT ที่เปลี่ยนเฉพาะ TTL/cap → **ไม่มีการเรียก `ApplyZones`** (ใช้ mock manager นับจำนวนครั้ง);
   PUT ที่เปลี่ยน `queryLogging` → มีการเรียก `ApplyZones` 1 ครั้ง; PUT ค่านอกช่วง → 400 และ DB ไม่เปลี่ยน
8. `RecordDNSEvent`: query จัดอันดับถูก, percent คิดจากจำนวน query, ผลรวม ≤ 100%
9. ชน cap ของ ring: 5,000 โดเมนไม่ซ้ำ → ไม่ panic, key/bucket ไม่เกินเพดาน, `dnsTruncated == true`
10. `ClearDNSStats()` → `topDomains` ว่าง **และ** `topDestinations[].domain` กลับเป็นว่างทันที
11. **regression:** cache ว่าง/สวิตช์ปิด → `TopDestinations`/`TopConversations` เหมือนผลลัพธ์เดิมเป๊ะ
    (bytes/percent/hostname เดิมทั้งหมด, `domain` = "")
12. `buildDNSConfig(..., queryLog=false)` → ไฟล์ config เหมือนเดิม **ทุกไบต์**
13. migration: เปิด DB ที่ยังไม่มี 3 คอลัมน์ → อ่านได้และได้ค่า default 0/60/4096;
    restore backup รุ่นเก่า → ได้ 60/4096 ไม่ใช่ 0
14. `go test -race ./internal/service/...` โดยมี goroutine ยิง `RecordDNSEvent` + `GetStatistics`
    + **`SetLimits`** พร้อมกัน (การปรับค่าตอนรันต้องไม่ race กับ reader)
**acceptance:** `cd backend && go test -race ./...` ผ่าน

### T-12 — API contract
**ไฟล์:** `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (ต้องตรงกันเป๊ะ)
เพิ่ม schema `TopDomain`, ฟิลด์ `domain` ใน `TopHost`, `dstDomain` ใน `TopConversation`,
ฟิลด์ใหม่ใน `TrafficStatistics`, และใน `DNSServerSettings` เพิ่ม `queryLogging`,
`dnsCacheTtlMinutes` (min 1 / max 1440 / default 60), `dnsCacheMaxEntries` (min 128 / max 65536 /
default 4096) — ใส่ `minimum`/`maximum`/`default` ใน schema จริง ๆ ไม่ใช่เขียนแค่ใน description
พร้อมหมายเหตุว่า `domain` เป็น **ค่าที่ DNS ตอบมา ใช้เพื่อแสดงผลเท่านั้น (อาจว่างหรือล้าสมัยได้)**
และว่าการแก้ 2 ค่านี้ **ไม่ restart dnsmasq**
**acceptance:** สองไฟล์ diff ตรงกัน, หน้า ApiDocs เรนเดอร์ไม่ error

### T-13 — frontend
**ไฟล์:** `frontend/src/services/statisticsService.ts`, `frontend/src/pages/Statistics.tsx`,
`frontend/src/services/dnsServerService.ts`, `frontend/src/pages/DnsServer.tsx`
1. type: `TopHost.domain`, `TopConversation.dstDomain`, `TopDomain`, ฟิลด์ใหม่ของ
   `TrafficStatistics`, และ `DNSServerSettings` 3 ฟิลด์ + **mock branch** (ใช้ IP↔โดเมนชุดเดียวกับ
   `kernel/mock.go` T-06 และเว้นบางแถวไม่มีโดเมนเพื่อให้เห็น fallback)
2. การ์ดที่ 5 "Top Queried Domains" — layout เดียวกับ `TopHostsCard`;
   `dnsLoggingEnabled === false` → empty state "ยังไม่ได้เปิดการเก็บสถิติ DNS — เปิดได้ที่หน้า
   DNS Server"; `dnsTruncated` → ป้ายเตือน
3. **การ์ดเดิม 2 ใบ:**
   - `HostLabel` (`Statistics.tsx:95-113`): มี `host.domain` → แสดงโดเมนเป็นบรรทัดหลัก IP เป็น
     `font-mono` ตัวเล็กข้าง ๆ; ไม่มี → **หน้าตาเหมือนเดิมทุกประการ**
   - ตาราง Conversations (`:180-185`): คอลัมน์ Destination แสดง `dstDomain` เหนือ IP เมื่อมี
   - แสดงเป็น text node ธรรมดา (React escape ให้เอง) **ห้าม `dangerouslySetInnerHTML`**,
     truncate ด้วย CSS, ใส่ข้อความสั้น ๆ ว่าชื่อโดเมน "มาจากที่ DNS ตอบ อาจไม่ตรงกับที่เห็นในเบราว์เซอร์"
4. **หน้า DNS Server — การ์ดใหม่ "สถิติ DNS (DNS Statistics)"** รวม 3 ค่าไว้ที่เดียว:
   - `<Switch>` "เก็บสถิติ DNS query (Top Queried Domains + ชื่อโดเมนของ IP ปลายทาง)"
     + คำเตือนว่าจะเห็นรายชื่อโดเมนที่เครื่องในบ้านถาม และ **การเปิด/ปิดจะ restart dnsmasq**
   - `<Input type="number">` "อายุของ mapping IP→โดเมน (นาที)" — min 1 max 1440 default 60
   - `<Input type="number">` "จำนวน mapping สูงสุด" — min 128 max 65536 default 4096
   - ข้อความใต้สองช่องนี้: **"ปรับแล้วมีผลทันที ไม่ต้อง restart บริการใด ๆ"** และคำอธิบายสั้น ๆ ว่า
     ค่าสูงขึ้น = ชื่อโดเมนครบขึ้นแต่ใช้ RAM มากขึ้น / ค่ายาวขึ้น = เสี่ยงเห็นชื่อเก่าที่ IP ถูกใช้ซ้ำแล้ว
   - validate ฝั่ง client ตามช่วงเดียวกัน (กันกดผิด) แต่ **ยังต้องพึ่ง 400 จาก backend เป็นตัวจริง**
     และแสดง error จาก backend ถ้าถูกปฏิเสธ
5. กฎสไตล์: ใช้ `components/ui/*` เท่านั้น, ห้าม `shadow-*`/`backdrop-blur-*`, ห้ามสีดิบ,
   ต้องดูดีทั้ง dark/light
**acceptance:** `yarn build && yarn lint` ผ่าน

### T-14 (optional, ท้ายสุด) — install.sh + เอกสาร
- `install.sh:586` เพิ่ม `RuntimeDirectoryPreserve=yes` ใน unit (§5 ข้อ 4) + release note ว่า
  เครื่องที่ติดตั้งไปแล้วต้องรัน `install.sh` ซ้ำ (หรือแก้ unit เอง) ถึงจะมีผล
- `docs/ref/complete/dnsmasq-design.md` + `docs/ref/dns-system-design.md`: บันทึกว่า log-facility
  ถูกย้ายไปไฟล์ (มีผลกับ `journalctl -u dnsmasq` — §5 ข้อ 7), มี reverse cache ระดับ RAM
  และค่า TTL/cap อยู่ใน `dns_server_settings` (ไม่ใช่ไฟล์ config ของ pigate)

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | หมายเหตุ |
|---|---|---|---|
| GET | `/api/statistics/traffic?window=1h\|24h` | `authRoute` | เส้นเดิม เพิ่ม `topDomains`/`dnsQueries`/`dnsLoggingEnabled`/`dnsTruncated` + `topDestinations[].domain` + `topConversations[].dstDomain` |
| GET | `/api/dns/settings` | `authRoute` | เพิ่ม `queryLogging`, `dnsCacheTtlMinutes`, `dnsCacheMaxEntries` |
| PUT | `/api/dns/settings` | `authRoute` (mutation → โดน DisableEdit/read-only ตามปกติ) | validate ช่วงค่า → 400 ถ้าเกิน; เปลี่ยน `interfaces`/`queryLogging` = apply + restart dnsmasq, เปลี่ยนแค่ TTL/cap = **ไม่ restart อะไรเลย** |

## 5. ข้อควรระวัง

1. 🔒 **ความเป็นส่วนตัว** — ทั้งรายชื่อโดเมนและ mapping IP→domain คือประวัติการใช้อินเทอร์เน็ต
   ของทุกคนในบ้าน จึงต้อง (ก) ปิดเป็นค่าเริ่มต้น (ข) เปิดได้เฉพาะผู้ล็อกอินที่แก้ไขได้
   (ค) ปิดสวิตช์แล้ว **ลบไฟล์ log + ล้าง ring + ล้าง reverse cache ทันที**
   (ง) ไฟล์ log 0640 (จ) ไม่มีอะไรลงดิสก์ถาวร reboot แล้วหายหมด
2. 🔒 **โดเมนคือ input ที่ไม่น่าเชื่อถือ — ทั้งขาถามและขาตอบ** ชื่อใน CNAME/reply ถูกกำหนดโดย
   เซิร์ฟเวอร์ปลายทางซึ่งอาจถูกยึด → sanitize ที่ **จุดเข้า (parser)** เสมอ ไม่ใช่ที่ UI
   และ **ห้าม log โดเมนออกทาง `log.Printf` ต่อ event**
3. **ปริมาณ log** — ~50 query/s ตอน peak × 3-5 บรรทัด/query ≈ 20 KB/s บน tmpfs
   ต้องมี `log-async=25`, เพดาน truncate 8 MB, เพดานอ่านต่อ tick — เกินแล้ว **ยอมทิ้งข้อมูล** + ตั้ง flag
4. **`/run/pigate` ถูกลบตอน pigate หยุด** (`RuntimeDirectory`) → dnsmasq เขียนลง inode ที่ถูก unlink
   ป้องกันด้วย `RuntimeDirectoryPreserve=yes` (T-14) และ/หรืออาศัย `ApplyZones` ตอนบูตที่ restart dnsmasq
5. **สมมติฐาน `O_APPEND` ของ dnsmasq** — ต้องยืนยันบนเครื่องจริงว่าหลัง `truncate` log ยังเดินต่อ
6. 🔒 **ชื่อโดเมนใช้เพื่อ "แสดงผล" เท่านั้น** — ห้ามนำ `Domain`/`DstDomain` หรือ reverse cache
   ไปใช้สร้าง nft rule, จับคู่ policy, ตัดสินใจ routing/QoS หรือ lookup ย้อนกลับเข้า firewall
   จุดใช้งานที่อนุญาตมีแค่ `buildTopHosts`/`buildTopConversations` → JSON → UI
7. **log ของ dnsmasq จะหายไปจาก journal** เมื่อเปิด `log-facility=<file>` (รวม DHCPACK)
   → เขียนไว้ในเอกสารและใต้สวิตช์
8. 🔒 **mapping IP→domain "ถูกป้อน" ได้** — เครื่องใน LAN query โดเมนของตัวเองที่ตอบ IP อะไรก็ได้
   เช่นทำให้ `8.8.8.8` ขึ้นชื่อ `evil.example.com` → ถือเป็น "ข้อมูลตามที่ DNS ตอบ" ไม่ใช่ข้อเท็จจริง,
   UI ต้องแสดง IP ควบคู่เสมอ และห้ามใช้ตัดสินใจอะไร (ข้อ 6)
9. **IP ซ้ำ/หมุนเวียน (CDN, cloud LB)** — IP เดียวใช้กับหลายโดเมนได้ → TTL + last-writer-wins
   + flag `multi`; ป้ายชื่ออาจไม่ตรงในบางแถว **เป็นพฤติกรรมที่ยอมรับ ไม่ใช่บั๊ก**
   และ **การตั้ง TTL ยาว ๆ ทำให้เพี้ยนมากขึ้น ไม่ใช่ดีขึ้น** — ต้องเขียนเตือนใต้ช่องกรอก (T-13)
10. **CNAME chain ให้ชื่อปลายทาง** (`*.googlevideo.com` แทน `www.youtube.com`) — แสดงตามที่
    dnsmasq บอก **ห้ามเดาชื่อต้นทางเอง**
11. **ลูกค้าที่ไม่ได้ใช้ DNS ของเรา** (DoH/DoT, ตั้ง 8.8.8.8 เอง) จะไม่มีชื่อโดเมนเลย — ต้องไม่ทำให้
    UI ดูเหมือนพัง (แสดง IP เหมือนเดิม)
12. **ห้ามทำให้ฟีเจอร์ที่ส่งไปแล้วพึ่งพา DNS log** — Top Destination/Conversation มาจาก conntrack
    ต้องแสดงผลครบเหมือนเดิมเมื่อสวิตช์ปิด/cache ว่าง/dnsmasq ไม่ทำงาน; `domain` เป็นฟิลด์
    **optional ที่ว่างได้เสมอ** และมีเทสต์ regression ล็อกไว้ (T-11 ข้อ 11)
13. **key ของ cache ต้อง normalize** ด้วย `netip` ให้ตรงกับ `net.IP.String()` ที่ conntrack ส่งมา
    (`real_traffic_account.go:98-99`) โดยเฉพาะ IPv6
14. **mock ต้องปลอดภัย 100%** — `MockDNSServerManager.WatchDNSLog` ห้ามแตะ `/run` หรือไฟล์ใด ๆ
15. **โหมด real บนเครื่องที่ไม่มี dnsmasq** (WSL) — watcher ต้องวนรอเงียบ ๆ ไม่ spam log
16. **ห้าม persist สถิติ** — `dns_server_settings` เพิ่มได้แค่ **ค่าตั้งค่า 3 ตัว** (config)
    ห้ามมีตารางเก็บ query/โดเมน/mapping (`git diff` ของ `db/` ต้องมีแค่ 3 คอลัมน์นี้)
17. 🔒 **ค่าที่ผู้ใช้ตั้งได้ต้องไม่ทำลายหลักประกัน "RAM มีขอบเขต"** — ต้อง validate ที่ handler (400)
    **และ** clamp ซ้ำทั้งตอนอ่านจาก DB (T-02) และใน `SetLimits` (T-08) เพราะ DB แก้ด้วยมือได้
    และไฟล์ backup ก็เป็น input จากภายนอกเช่นกัน; ค่า 0/ติดลบต้องไม่มีทางไปถึง cache
    (0 = ปิด cache เงียบ ๆ, ติดลบ = พฤติกรรมไม่นิยาม) เพดานสูงสุด 65536 ≈ ~13 MB worst case
18. 🎁 **แยกให้ชัดว่าอะไร restart อะไรไม่ restart** — `queryLogging`/`interfaces` ต้องเขียน config +
    restart dnsmasq (DNS **และ DHCP** สะดุด 1-2 วินาที เพราะเป็น process เดียวกัน)
    แต่ TTL/cap **ห้าม** เรียก `ApplyAll()` เด็ดขาด ไม่งั้นการเลื่อนตัวเลขเล่น ๆ ในหน้าเว็บจะทำ
    เน็ตบ้านสะดุดทุกครั้ง — มีเทสต์นับจำนวนครั้งที่ `ApplyZones` ถูกเรียกล็อกไว้ (T-11 ข้อ 7)
19. **ลดเพดานต้องมีผลทันที** — `SetLimits` ที่ลด `maxEntries` ต้อง evict ให้เหลือตามค่าใหม่ในที่เดียวกัน
    ไม่ใช่รอ `Put` ครั้งถัดไป (ผู้ใช้ลดเพราะอยากได้ RAM คืน "เดี๋ยวนี้")
20. **ไม่ทำ regression กับ DNS Server เดิม** — `ApplyZones` เปลี่ยน signature: ต้องแก้ real, mock,
    service และเทสต์ `kernel/dns_server_test.go` ให้ครบ และเมื่อ `queryLog=false`
    ไฟล์ config ที่ได้ต้อง **เหมือนเดิมทุกไบต์** (T-11 ข้อ 12)

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [ ] T-01 DTO (`domain`/`dstDomain`, `DNSLogEvent`, 3 ฟิลด์ของ `DNSServerSettings`) +
      `ValidateDNSServerSettings` + const ขอบเขตกลาง
- [ ] T-02 migration 3 คอลัมน์ (`query_logging`, `dns_cache_ttl_minutes`, `dns_cache_max_entries`)
      + repo (clamp ตอนอ่าน) + backup/restore (ไฟล์เก่า → default ไม่ใช่ 0)
- [ ] T-03 `kernel/interfaces.go`: `ApplyZones(+queryLog)`, `WatchDNSLog` 🔒
- [ ] T-04 `kernel/dns_query_log.go` parser (query+answer) + sanitizer 🔒
- [ ] T-05 `kernel/dns_server.go` + `kernel/real_dns_query_log.go` 🔒
- [ ] T-06 `kernel/mock.go` (signature + สังเคราะห์ query **และ** answer ให้ตรง mockFlowTemplates)
- [ ] T-07 `service/dns_query_stats.go` + `service/statistics.go` + `service/dns_server.go`
- [ ] T-08 `service/dns_reverse_cache.go` (TTL/cap ปรับได้ + `SetLimits` + evict ทันทีเมื่อลดเพดาน)
      + ต่อเข้า `buildTopHosts`/`buildTopConversations` 🔒
- [ ] T-09 wiring ใน `cmd/pigate/main.go` (โหลดค่าจาก DB ตอนบูต)
- [ ] T-10 `api/handlers.go` (validate 400 + แยก restart/ไม่ restart) 🔒
- [ ] T-11 เทสต์ 14 เคส (รวม `SetLimits`, validator, "ไม่เรียก ApplyZones", regression การ์ดเดิม)
      + `go test -race ./...`

**Frontend / Docs**
- [ ] T-12 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync, มี min/max/default จริง)
- [ ] T-13 `statisticsService.ts` + `Statistics.tsx` (การ์ดใหม่ + ชื่อโดเมนในการ์ดเดิม 2 ใบ)
      + `dnsServerService.ts` + `DnsServer.tsx` (การ์ด "สถิติ DNS": สวิตช์ + 2 ช่องตัวเลข)
- [ ] T-14 (optional) `install.sh` `RuntimeDirectoryPreserve=yes` + เอกสาร

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ)**
- [ ] `-mock=true -allow-dev-cors`: เห็นการ์ด Top Queried Domains มีข้อมูลขยับ **และ**
      Top Destinations/Conversations แสดงชื่อโดเมนของ IP ที่ mock map ไว้ ส่วน IP ที่ไม่ได้ map
      ยังโชว์ IP เปล่าเหมือนเดิม
- [ ] `-mock=true`: ไม่มีการสร้าง/แตะไฟล์ใน `/run` เลย (`ls /run/pigate` บนเครื่อง dev)
- [ ] real device: สวิตช์ **ปิด** (ค่าเริ่มต้น) → ไม่มีไฟล์ log, `pigate-dns.conf` ไม่มีบรรทัด `log-*`,
      การ์ดใหม่แสดง "ยังไม่ได้เปิด", **การ์ด Top Destination/Conversation ทำงานเหมือนก่อนแผนนี้ทุกประการ**
- [ ] real device: หน้า DNS Server แสดงค่าเริ่มต้น **60 นาที / 4096 entry** ตั้งแต่ครั้งแรก
      (ทั้งบนเครื่องติดตั้งใหม่และเครื่องที่อัปเกรดจาก DB เก่า)
- [ ] real device: เปิดสวิตช์ → dnsmasq restart แล้วยังจ่าย DNS/DHCP ปกติ, ไฟล์ log เป็นของ
      `pigate` mode 0640, `dnsmasq --test` ไม่ error
- [ ] real device: เปิดเว็บจากมือถือในบ้าน → โดเมนขึ้นใน Top Queried Domains ภายใน ~10s
      **และ** IP ปลายทางของ flow นั้นใน Top Destinations ขึ้นชื่อโดเมน
- [ ] 🎁 real device: **แก้ TTL/จำนวน mapping แล้วกดบันทึก → ping/เปิดเว็บระหว่างนั้นไม่หลุดแม้แต่
      แพ็กเก็ตเดียว, `systemctl show dnsmasq -p ExecMainStartTimestamp` ไม่เปลี่ยน (ไม่ได้ restart),
      และค่าใหม่มีผลทันทีโดยไม่ต้อง restart pigate**
- [ ] real device: ตั้ง TTL = 1 นาที → รอ 2 นาทีแล้วชื่อโดเมนของ IP ที่ไม่ถูกถามซ้ำหายไปจริง
- [ ] real device: ลดจำนวน mapping สูงสุดจาก 4096 → 128 ขณะมีข้อมูลเต็ม → RSS ลดลง/ไม่ค้าง
      และหน้าเว็บยังทำงานปกติ
- [ ] 🔒 ส่งค่านอกช่วงผ่าน API ตรง ๆ (`0`, `-5`, `99999`) → ได้ 400 พร้อมข้อความบอกช่วง
      และค่าใน DB ไม่ถูกเปลี่ยน
- [ ] real device: เครื่องที่ตั้ง DNS เป็น 8.8.8.8 เอง → ปลายทางไม่มีชื่อโดเมน แต่การ์ดยังปกติ
- [ ] real device: สลับ window 1h ↔ 24h → 24h ≥ 1h เสมอ; % ไม่เกิน 100; แถวเก่าใน 24h
      ที่ mapping หมดอายุแล้วโชว์ IP เปล่าได้ (ไม่ใช่บั๊ก)
- [ ] real device: ปิดสวิตช์ → ไฟล์ log ถูกลบ, การ์ดใหม่กลับเป็น "ยังไม่ได้เปิด",
      **ชื่อโดเมนในการ์ดเดิมหายไปทันที (cache ถูกล้าง) แต่ตัวเลข bytes/percent ไม่เปลี่ยน**
- [ ] 🔒 ยิงโดเมนแปลก ๆ (`dig "<script>alert(1)</script>.example.com"`, โดเมนยาว 300 ตัวอักษร)
      และตั้งโดเมนทดสอบให้ตอบ CNAME ที่ชื่อมีอักขระแปลก → ไม่ปรากฏใน UI (ถูกทิ้งที่ parser),
      ไม่มีอะไรแปลกใน log ของ pigate, ไม่มี panic, เลย์เอาต์ไม่พัง
- [ ] โหลด DNS หนัก (~1,000 query) → ไฟล์ log ไม่เกิน 8 MB (ถูก truncate), CPU ไม่พุ่งค้าง,
      สถิติเดินต่อหลัง truncate, ขนาด reverse cache ไม่เกินค่าที่ตั้งไว้
- [ ] ปล่อยรัน ≥ 1 ชั่วโมงโดยเปิดสวิตช์ → RSS ไม่โตต่อเนื่อง, `df -h /run` ไม่โตต่อเนื่อง
- [ ] restart `pigate.service` ระหว่างเปิดสวิตช์ → กลับมาแล้วสถิติเดินต่อ และ **ค่า TTL/cap
      ที่ตั้งไว้ยังอยู่** (โหลดจาก DB ตอนบูต)
- [ ] `-disable-edit=true` / role read-only: **ดู** การ์ดและชื่อโดเมนได้ แต่สลับสวิตช์/แก้ตัวเลขไม่ได้ (403)
- [ ] logout แล้วเรียก `/api/statistics/traffic` และ `/api/dns/settings` ตรง ๆ → 401
- [ ] Backup → Restore ไฟล์ที่มีค่า custom (เช่น 30 นาที / 8192) แล้วค่ากลับมาถูกและมีผลจริง;
      restore ไฟล์รุ่นเก่า (ไม่มีฟิลด์) → ได้ default 60/4096 (ไม่ใช่ 0) และไม่พัง
- [ ] `cd backend && go build ./... && go test -race ./...` ผ่าน; `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] 🔒 `git diff --stat`: ไม่มี `kernel/real_firewall.go`, ไม่มีตารางใหม่ใน `db/`
      (มีได้แค่ 3 คอลัมน์ใน `dns_server_settings`), ไม่มี `exec.Command` ใหม่,
      **ไม่มีคีย์ใหม่ใน `internal/config`** (ยืนยันว่าไม่ได้ทำเป็น flag)
- [ ] ทุกอย่างอยู่บน branch `feat/statistics-dns-top-domain` และเข้า main ผ่าน PR เท่านั้น
