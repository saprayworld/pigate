# แผนงาน: Public IP Info card แทน Top peers (Statistics > Traffic > Host)

สถานะ: อนุมัติแล้ว พร้อมให้ ai-developer ลงมือ
Branch: `feat/statistics-host-ipinfo` (แตกจาก `main`)

## 1. โจทย์

บนหน้า `/statistics/traffic/host/:ip` เมื่อ drill เข้า IP สาธารณะ ให้แสดงการ์ดข้อมูล
Public IP (ip, hostname, city, region, country, org) จาก ipinfo.io **แทนที่การ์ด
"Top peers"** ส่วนกรณี LAN host ให้คง Top peers ไว้เหมือนเดิม

## 2. การตัดสินใจของเจ้าของโปรเจกต์ (ปิดประเด็นแล้ว)

| ประเด็น | ข้อสรุป |
|---|---|
| ใครเรียก ipinfo.io | **Backend proxy** (ไม่ใช่ frontend เรียกตรง) |
| API token | **ไม่ใช้ในเฟสนี้** — `curl https://ipinfo.io/<ip>` จาก rpi5 จริงได้ city/region/org ครบโดยไม่มี token |
| หน้า Settings ใส่ token | **ตัดออก** (เดิม T-12) |
| การเปิดใช้งาน | **default ปิด** เปิดด้วยการแก้ `/var/lib/pigate/pigate.conf` คีย์ `ipinfo-enabled=true` ด้วยมือ ไม่มี toggle ใน UI |
| พฤติกรรม UI | แสดงตามเงื่อนไข: public IP -> IP Info card, LAN host -> Top peers เหมือนเดิม |

## 3. เหตุผลที่เลือก Backend proxy (บันทึกไว้เพื่อการ review)

1. **CSP** — `backend/internal/api/middleware.go:395` ตั้ง `connect-src 'self'`
   เบราว์เซอร์จะบล็อก `fetch("https://ipinfo.io/…")` ทันที การทำฝั่ง frontend
   บังคับให้ต้องผ่อน CSP ซึ่งเปิดช่องทาง exfiltration ให้ XSS **ทั้งแอป**
   เพื่อฟีเจอร์แสดงผลอันเดียว — แพงเกินไป
2. **Privacy** — ฝั่ง frontend จะเปิดเผย IP ของเครื่องแอดมินให้ ipinfo.io ด้วย
   ฝั่ง backend เปิดเผยแค่ IP ของ gateway เครื่องเดียว
3. **Cache/rate limit** — หน้านี้ auto-refresh ทุก 10 วินาที cache ที่ gateway
   ใช้ร่วมกันทุก client ได้ hit rate สูงกว่าและคุม rate limit ได้จริง
4. **บังคับปิดได้จริง** — default OFF ที่ server เชื่อถือได้ ต่างจากการเชื่อ client

## 4. ข้อควรระวังสำคัญ (อ่านก่อนลงมือทุก task)

1. **นี่คือ outbound HTTP client ตัวแรกของ pigate daemon** — ตรวจแล้วทั้ง
   `backend/internal` ใช้ `net/http` เฉพาะฝั่ง server ไม่มี `http.Client` ออกนอกเลย
   ถือเป็นการเปลี่ยน threat model ของเดมอน ทุก task ที่มีเครื่องหมาย SENSITIVE
   ต้องผ่าน review เข้มเป็นพิเศษ
2. **ห้ามใช้ `exec.Command` / `curl`** เด็ดขาด ใช้ `net/http` เท่านั้น
3. **`isPrivateIP` เดิมใน `service/statistics.go:393` ครอบคลุมไม่พอ** สำหรับงานนี้
   (ขาด CGNAT 100.64/10, IPv6 ULA, multicast, TEST-NET) ห้ามนำมาใช้เป็น guard
   ของการยิงออกอินเทอร์เน็ต — ต้องเขียน `isGloballyRoutable` ใหม่ (T-02)
4. **ห้ามลบ/ย้ายตรรกะ `topPeers` useMemo** ออกจาก `ConversationTable`
   (`StatisticsTrafficHost.tsx` บรรทัด 125-139) คอมเมนต์บรรทัด 100-108 อธิบายว่า
   ตั้งใจให้ Top peers ผูกกับ `filtered` เพื่อให้เปลี่ยนตามช่องค้นหา
   งานนี้แค่เปลี่ยน "เงื่อนไขการ render" เท่านั้น
5. **RAM-only** ห้ามเก็บผล lookup ลง SQLite (tech_stack_design §8 SD card wear)
6. **`go.sum` ไม่มี `golang.org/x/sync`** — single-flight ต้องเขียนเอง ห้ามเพิ่ม dependency
7. `ipinfo-enabled` เป็น **file-only key** (ไม่มี CLI flag) ตาม pattern ที่มีอยู่แล้วของ
   `dns-stats-max-pairs` ฯลฯ — ไม่ต้องแตะชุด flag ใน `cmd/pigate/main.go`

## 5. Tasks

### T-01 — model: IPInfo DTO + ค่าคงที่
- layer: model
- files: `backend/internal/model/ipinfo.go` (ใหม่)
- depends_on: —

สร้าง struct `IPInfoLookup`:
`Ip` (json `ip`) และฟิลด์ `Hostname, City, Region, Country, CountryName, Org, Asn, AsName, Timezone, Loc`
โดย **ทุกฟิลด์ยกเว้น `Ip` ต้องมี `,omitempty`** เพราะ provider แต่ละเจ้า/แต่ละ tier
คืนไม่ครบเท่ากัน (ออกแบบให้ provider-agnostic เผื่อวันหนึ่ง ipinfo.io ตัด free tier
เหลือแค่ country+ASN แล้วต้องย้ายไป provider อื่น)
เพิ่ม `Source string` (`"cache"`/`"live"`) และ `CachedAt time.Time`

ค่าคงที่: `IPInfoCacheTTLDefault` (24h), `IPInfoNegativeTTL` (1h),
`IPInfoCacheMaxEntries` (1000), `IPInfoHTTPTimeout` (5s), `IPInfoMaxResponseBytes` (16*1024)

ห้ามเพิ่ม dependency ห้ามแตะไฟล์อื่น

acceptance:
- `go build ./...` ผ่าน
- ทุกฟิลด์มี json tag และมี omitempty ครบตามที่ระบุ

### T-02 — service: public-IP guard (SENSITIVE)
- layer: service
- files: `backend/internal/service/ipinfo.go`, `backend/internal/service/ipinfo_test.go`
- depends_on: T-01

เขียน `isGloballyRoutable(ip string) bool` ด้วย `net/netip` — return **false** สำหรับ:
`IsPrivate`, `IsLoopback`, `IsLinkLocalUnicast`, `IsLinkLocalMulticast`, `IsMulticast`,
`IsUnspecified`, `IsInterfaceLocalMulticast`, CGNAT `100.64.0.0/10`, `0.0.0.0/8`,
`169.254.0.0/16`, TEST-NET `192.0.2.0/24` + `198.51.100.0/24` + `203.0.113.0/24`,
`240.0.0.0/4`, IPv6 ULA `fc00::/7`, IPv6 doc `2001:db8::/32`
และต้อง **unmap IPv4-mapped IPv6 ก่อนตรวจ** (`addr.Unmap()`) มิฉะนั้น `::ffff:192.168.1.1`
จะเล็ดลอดออกไป

ห้ามใช้ `isPrivateIP` เดิม (ดู Caution 3)

acceptance:
- `go test ./internal/service/ -run TestIsGloballyRoutable` ผ่าน
- มีเคสเทสต์ครบทุกช่วง IP ที่ระบุข้างบน รวม IPv4-mapped IPv6

### T-03 — service: ipinfo cache (RAM-only)
- layer: service
- files: `backend/internal/service/ipinfo_cache.go` (+ เทสต์)
- depends_on: T-01

ทำตามแบบ `service/dns_reverse_cache.go` (`sync.RWMutex` + map +
`evictExpiredLocked`/`evictToSizeLocked`) เก็บ `model.IPInfoLookup` ต่อ normalized IP

ต้องมี:
- negative entry (lookup ล้มเหลว -> cache ไว้ `IPInfoNegativeTTL` กัน hammering)
- single-flight **เขียนเอง** (`map[string]chan struct{}`) ห้ามเพิ่ม dependency (Caution 6)
- **RAM เท่านั้น** ห้ามเรียก db/repository ในไฟล์นี้ (Caution 5)

acceptance:
- `go test ./internal/service/` ผ่าน
- มีเทสต์: TTL หมดอายุ, cap eviction, negative cache, single-flight (goroutine พร้อมกัน
  หลายตัว -> provider ถูกเรียกครั้งเดียว)
- grep ไฟล์นี้ไม่พบ `repo`/`db`/`sql`

### T-04 — service: IPInfoProvider + ipinfo.io client (SENSITIVE)
- layer: service
- files: `backend/internal/service/ipinfo.go`
- depends_on: T-02, T-03

ประกาศ `type IPInfoProvider interface { Lookup(ctx context.Context, ip string) (model.IPInfoLookup, error) }`
แล้ว implement `ipinfoIOProvider` + `mockIPInfoProvider` (in-memory, deterministic,
สำหรับโหมด `-mock`)

ข้อบังคับของ real provider:
1. URL ต้อง hardcode base `"https://ipinfo.io/"` ต่อด้วย ip ที่ผ่าน
   `netip.ParseAddr().String()` มาแล้วเท่านั้น + `/json` — **ห้ามต่อ string ดิบจาก client**
2. `http.Client` เฉพาะตัว มี `Timeout: model.IPInfoHTTPTimeout` และ
   `CheckRedirect` ที่ `return http.ErrUseLastResponse` (**ห้าม follow redirect** — กัน
   ถูก redirect ไปยัง internal address)
3. อ่าน body ผ่าน `io.LimitReader(resp.Body, model.IPInfoMaxResponseBytes)`
4. **ไม่มี token ในเฟสนี้** — ไม่ต้องส่ง `Authorization` header และไม่ต้องมี token
   field ใน config แต่ให้ constructor ของ provider รับพารามิเตอร์ `token string`
   ไว้เฉย ๆ (ปัจจุบันส่ง `""` เสมอ) เผื่ออนาคต ipinfo.io บังคับ token แล้วเติมได้
   โดยไม่ต้องรื้อ interface — **ห้าม implement UI/config ใด ๆ สำหรับ token ตอนนี้**
5. field ที่ provider ไม่ส่งมาให้เป็น `""` (omitempty จะตัดเองตอน marshal)

acceptance:
- `go build ./...` ผ่าน
- มี provider ทั้ง real และ mock
- `CheckRedirect` และ `io.LimitReader` มีอยู่จริงในโค้ด
- ไม่มี `Authorization` header ถูกส่งเมื่อ token เป็น `""`

### T-05 — service: IPInfoService (enabled flag + rate limit)
- layer: service
- files: `backend/internal/service/ipinfo.go` (+ เทสต์)
- depends_on: T-04

รวม guard + cache + provider เป็น `IPInfoService.Lookup(ctx, ip)` ตามลำดับ:
1. `!enabled` -> `ErrIPInfoDisabled`
2. `!isGloballyRoutable(ip)` -> `ErrIPInfoNotPublic` (**ไม่ยิงออกเน็ต**)
3. cache hit -> คืนพร้อม `Source = "cache"`
4. rate limiter ระดับ service (token bucket ~1 req/s, burst 5) เกิน -> `ErrIPInfoRateLimited`
5. single-flight เรียก provider -> เก็บ cache -> คืน `Source = "live"`

อ่าน `enabled` จาก constructor เท่านั้น ห้ามอ่าน env/ไฟล์เองในนี้

acceptance:
- unit test ครอบทั้ง 5 เส้นทางด้วย mock provider
- เทสต์ยืนยันว่า **disabled แล้ว provider ไม่ถูกเรียกเลย** (นับด้วย counter ใน mock)
- เทสต์ยืนยันว่า LAN IP ไม่ทำให้ provider ถูกเรียก

### T-06 — config: `ipinfo-enabled` (file-only key)
- layer: service
- files: `backend/internal/config/config.go`, `backend/internal/config/config_test.go`,
  `backend/cmd/pigate/main.go`
- depends_on: T-05

เพิ่มคีย์ `ipinfo-enabled` (bool, **default `false`**) เป็น **file-only key ไม่มี CLI flag**
ตาม pattern ที่มีอยู่แล้วของ `dns-stats-max-pairs`/`traffic-stats-max-hosts`:
เพิ่ม field `IPInfoEnabled bool` ใน `Config`, const `keyIPInfoEnabled = "ipinfo-enabled"`,
default ใน `Defaults()`, **ต่อท้าย `orderedKeys`** (ห้ามแทรกกลาง — จะทำให้ไฟล์ config
ที่ generate ไว้แล้ว diff เพี้ยน), และ case ใน parse/format switch ทั้งสองตัว

**ไม่มีคีย์ token** ในเฟสนี้ (ดู T-04 ข้อ 4)

wire `IPInfoService` ใน `backend/cmd/pigate/main.go` ตามลำดับ startup เดิม
(หลัง service construction) โดยเลือก mock provider เมื่ออยู่ในโหมด `-mock`

acceptance:
- `go test ./internal/config/` ผ่าน
- `./pigate-backend -mock=true` รันได้ปกติโดยไม่ต้องตั้งค่าเพิ่ม (ฟีเจอร์ปิดอยู่)
- ไฟล์ config ที่ generate ใหม่มีบรรทัด `ipinfo-enabled=false` ต่อท้าย

### T-07 — api: `GET /api/statistics/ipinfo` (SENSITIVE)
- layer: api
- files: `backend/internal/api/handlers.go`, `backend/internal/api/router.go`,
  `backend/internal/api/handlers_test.go`
- depends_on: T-05

เพิ่ม `HandleGetIPInfo` **ตามแบบ `HandleGetTrafficHostDetail` (handlers.go:645) เป๊ะ**:
query param `ip` ต้องผ่าน `netip.ParseAddr` มิฉะนั้น `400 "invalid ip"` และ
**ห้ามเรียก service**; ส่งต่อด้วย `addr.String()` ที่ normalize แล้ว

mapping error:
- `ErrIPInfoDisabled` -> **404** (ไม่ใช่ 403 เพื่อไม่บอกใบ้สถานะ config)
- `ErrIPInfoNotPublic` -> 400
- `ErrIPInfoRateLimited` -> 429
- provider ล้มเหลว/timeout -> 502 พร้อม message กลาง ๆ
  **ห้ามส่ง error ของ upstream ดิบกลับไป**

register ด้วย `authRoute` (ไม่ใช่ `superAdminRoute`) ใน `router.go` ต่อจากบรรทัด 60
พร้อมคอมเมนต์อ้างแผนนี้ — GET ล้วน `DisableEditMiddleware` จึงไม่บล็อก

acceptance:
- `go test ./internal/api/` ผ่าน
- มีเทสต์: ไม่ล็อกอิน -> 401, `ip` เพี้ยน -> 400, LAN ip -> 400, ฟีเจอร์ปิด -> 404

### T-08 — frontend: `ipinfoService.ts`
- layer: frontend
- files: `frontend/src/services/ipinfoService.ts` (ใหม่)
- depends_on: T-01

ตามแบบ `trafficStatisticsService.ts`: `export interface IpInfoLookup` ให้ตรงกับ
`model.IPInfoLookup` ทุกฟิลด์ (ทุกตัวยกเว้น `ip` เป็น optional)
+ `getIpInfo(ip)` เรียก `GET ${API_BASE_URL}/statistics/ipinfo?ip=${encodeURIComponent(ip)}`
(encode ที่นี่ที่เดียว — ห้าม double-encode)

**ต้องมี in-memory Map cache ระดับ module (TTL 10 นาที)** เพราะหน้าเพจ auto-refresh
ทุก 10 วินาที ห้ามให้ทุกรอบ refresh ยิง lookup ใหม่

เพิ่ม mock branch (`IS_MOCK_MODE`) คืนข้อมูลปลอมสำหรับ IP ใน `mockExtraDests`
(`142.250.196.14`, `8.8.8.8`, `2606:4700:4700::1111` ฯลฯ)

**404 ต้องแปลเป็นสถานะ "ฟีเจอร์ปิดอยู่" ไม่ใช่ error แดง**

acceptance:
- `yarn build` ผ่าน
- type ตรงกับ Go struct ทุกฟิลด์
- เรียกซ้ำ IP เดิมภายใน TTL ไม่ยิง fetch ใหม่

### T-09 — frontend: `PublicIpInfoCard`
- layer: frontend
- files: `frontend/src/components/statistics/PublicIpInfoCard.tsx` (ใหม่)
- depends_on: T-08

คอมโพเนนต์แสดง ip / hostname / city+region / country / org / asn เป็นแถว label-value

กฎสไตล์ (`docs/rules_of_work.md`): ใช้เฉพาะ `components/ui/*`,
ห้าม `shadow-*` / `backdrop-blur-*`, **ห้าม hardcode สี** (เช่น `text-emerald-500`)
ใช้ `text-primary` / `text-muted-foreground` / `bg-primary/10` เท่านั้น,
รองรับ dark + light

ต้องมี 4 state: loading (`Skeleton`), disabled (ข้อความบอกว่าปิดอยู่ เปิดได้ที่
`/var/lib/pigate/pigate.conf` คีย์ `ipinfo-enabled`), error (ข้อความเรียบ ๆ ไม่ throw),
success

**ฟิลด์ที่ backend ไม่ส่งมาต้องซ่อนแถวนั้นไปเลย ไม่ใช่แสดง "—"** เพราะ provider
อาจคืนไม่ครบ

ขนาดต้องแทน Top peers ได้พอดี (1 คอลัมน์ใน `grid xl:grid-cols-3`)

acceptance:
- `yarn build` + `yarn lint` ผ่าน
- grep ไฟล์นี้ไม่พบ `shadow-` / `backdrop-blur-` / สี palette ดิบ

### T-10 — frontend: wire เข้าหน้า Host
- layer: frontend
- files: `frontend/src/pages/StatisticsTrafficHost.tsx`
- depends_on: T-09

ใน `ConversationTable` (บล็อก grid บรรทัด ~159-191) เพิ่ม prop `hostIsPublic: boolean`
ส่งมาจากหน้าเพจด้วยค่า `data.private === false`
(**ห้ามคำนวณ private เองฝั่ง frontend** — ใช้ค่าจาก backend)

- `hostIsPublic === true` -> render `<PublicIpInfoCard ip={ip ของหน้านี้} />` แทนบล็อก Top peers
- `hostIsPublic === false` -> คงพฤติกรรมเดิมทุกอย่าง

**ห้ามลบ/ย้าย `topPeers` useMemo** (Caution 4) แค่เปลี่ยนเงื่อนไข render

อัปเดตเงื่อนไข `col-span` ของ `TrafficTrendCard` ให้ครอบคลุมกรณีใหม่
(มีการ์ดขวา = `xl:col-span-2`, ไม่มี = `xl:col-span-3`) — ปัจจุบันผูกกับ
`topPeers.length > 0` อย่างเดียว ซึ่งจะผิดเมื่อการ์ดขวาเป็น IP Info

อัปเดตคอมเมนต์บล็อกให้อ้างแผนนี้

acceptance:
- `yarn build` ผ่าน
- drill เข้า LAN host -> เห็น Top peers เหมือนเดิม
- drill เข้า public IP -> เห็นการ์ด IP Info แทน
- ค้นหาในช่อง filter แล้ว Top peers ยังอัปเดตตามเหมือนเดิม (กรณี LAN host)

### T-11 — docs: openapi + README
- layer: api
- files: `docs/openapi.yaml`, `frontend/public/openapi.yaml`, `README.md`
- depends_on: T-07

เพิ่ม path `/statistics/ipinfo` + schema `IPInfoLookup` ลง openapi.yaml
(**ทั้งสองไฟล์ ต้องตรงกัน**) ระบุ response 200/400/404/429/502 ให้ครบ
พร้อมคำอธิบายว่าเป็นฟีเจอร์ opt-in default OFF และข้อมูลถูกส่งไป third party

อัปเดตหมายเหตุ privacy ใน README + วิธีเปิดผ่าน config file

acceptance:
- openapi.yaml สองไฟล์เหมือนกัน
- หน้า ApiDocs เรนเดอร์ไม่พัง

## 6. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

ทดสอบครั้งเดียวหลัง T-01 ถึง T-11 เสร็จครบ:

1. `cd backend && go build ./... && go test ./...` ผ่านทั้งหมด
2. `cd frontend && yarn build && yarn lint` ผ่าน
3. `bash build.sh` สร้าง `./pigate` ได้สำเร็จ
4. โหมด default (`ipinfo-enabled=false`): หน้า Statistics > Traffic > Host ทำงาน
   เหมือนเดิม 100% และ **ไม่มี request ออกอินเทอร์เน็ตแม้แต่ครั้งเดียว**
5. ตั้ง `ipinfo-enabled=true` ใน config -> drill เข้า public IP -> การ์ด IP Info
   แสดงข้อมูลจริง (ip/hostname/city/region/country/org) แทน Top peers
6. drill เข้า LAN host (192.168.x.x) -> ยังเป็น Top peers เหมือนเดิม และ backend
   **ไม่ยิง request ออกไปเลย** สำหรับ IP นั้น
7. `/api/statistics/ipinfo?ip=192.168.1.10` -> 400,
   `?ip=notanip` -> 400, `?ip=100.64.0.1` -> 400, `?ip=::ffff:10.0.0.1` -> 400,
   ไม่ล็อกอิน -> 401, ฟีเจอร์ปิด -> 404
8. ปล่อยหน้าค้าง 2 นาที (auto-refresh ~12 รอบ) -> request ออก ipinfo.io
   **ไม่เกิน 1 ครั้งต่อ IP** (พิสูจน์ว่า cache ทั้งสองชั้นทำงาน)
9. ตัดอินเทอร์เน็ตของ gateway -> การ์ดขึ้น error state เรียบร้อย ไม่ loading ค้าง
   ไม่ทำให้หน้าพัง และตารางด้านล่างยังใช้งานได้ปกติ
10. **CSP ยังเป็น `connect-src 'self'` เหมือนเดิม** (`middleware_test.go` ไม่ถูกแก้)
11. `grep -rn "exec.Command" backend/internal/service/ipinfo*.go` ไม่พบผลลัพธ์
12. dark mode + light mode การ์ดใหม่แสดงผลถูกต้องทั้งคู่
13. โหมด `-mock=true` การ์ดใหม่ยังแสดงข้อมูลปลอมได้ (dev workflow ไม่พัง)

## 7. หมายเหตุความเสี่ยงระยะยาว

ipinfo.io ปรับโครงสร้าง free tier ในปี 2025 (IPinfo Lite = country + ASN เท่านั้น,
ส่วน city/region/org อยู่ใน legacy free 50k req/เดือน หรือแผน Basic)
ตอนนี้ `curl` จาก rpi5 ยังได้ครบโดยไม่ต้องมี token แต่ **อาจเปลี่ยนได้ทุกเมื่อ**
ด้วยเหตุนี้ทุกฟิลด์จึงเป็น optional/omitempty และมี `IPInfoProvider` interface คั่นไว้
ถ้าวันหนึ่งข้อมูลหาย ทางเลือกคือ (ก) เติม token, (ข) เปลี่ยน provider,
(ค) ใช้ MaxMind GeoLite2 แบบ offline `.mmdb` (ปลอดภัยที่สุด ไม่ต้องออกเน็ต
แต่ +1 dependency +ไฟล์ ~60MB +งานอัปเดต DB)
