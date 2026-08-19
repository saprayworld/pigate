# Popover Reference Detail — hover ที่ IP/Domain แล้วเห็นสรุปข้อมูลอ้างอิงแบบ FortiGate

> เอกสารแผนงานสำหรับฟีเจอร์ใหม่: เดิมค่าที่เป็น IP/Domain ในหน้า Logs และ Policy เป็นข้อความตายตัว
> ผู้ใช้ต้องจำค่าแล้วไปพิมพ์ค้นเองในหน้า Statistics ฟีเจอร์นี้ทำให้ hover ค้าง 1 วินาทีแล้วเห็น
> popover สรุป (domain ที่เกี่ยวข้อง / IP ที่ resolve ได้ / Public IP Info) พร้อมปุ่มลัดไปหน้า drill-down
>
> วันที่เขียน: 2026-08-17 · Branch อ้างอิง: `feat/reference-popover`
> สถานะ: **เสร็จตามแผนทุก step · ผ่าน QA รอบ 2 (2026-08-17)** — เหลือให้เจ้าของโปรเจกต์ทดสอบ UX
> บนเบราว์เซอร์จริงก่อน merge (ดูข้อที่ยังไม่ติ๊กใน §6) และรับทราบ scope gap ของ Step 13

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็นจริง)**

1. hover ค่าที่เป็น IP ในตาราง → รอ 1 วินาที → popover เปิด แสดง hostname/ปริมาณ traffic,
   รายการ domain ที่เกี่ยวข้องสูงสุด 3 รายการ + `+N more`, และ Public IP Info (ASN/Org/Country)
   เมื่อเป็น IP สาธารณะ พร้อมปุ่มไป `/statistics/traffic/host/<ip>`
2. hover ค่าที่เป็น Domain → popover แสดง IP ที่ domain นี้ resolve ไปสูงสุด 3 รายการ + `+N more`
   พร้อมปุ่มไป `/statistics/dns/domain/<domain>`
3. แถวใน Logs Traffic ที่มีทั้ง IP และ Domain อยู่ด้วยกัน → **popover เดียว** รวมทั้งสองฝั่ง
4. UX: เปิดหลัง hover ค้าง 1 วินาที, ลากเมาส์จาก trigger เข้าไปใน popover แล้วไม่ปิด (hover-bridge),
   ออกจากทั้งสองส่วนจริง ๆ จึงปิด
5. หน้า Policy ที่แสดงเป็น **ชื่อ Address Object** → popover ระดับ 1 แสดง entries ทั้งหมด,
   hover entry ที่เป็น IP เดี่ยว/FQDN → popover ระดับ 2 คือ reference จริงของค่านั้น

**เงื่อนไขเชิงเทคนิคที่ต้องเป็นจริง**

- ทุก endpoint ใหม่ validate `ip`/`domain` ด้วย `netip.ParseAddr` / `model.NormalizeQueryDomain`
  และไม่เคย echo ค่าดิบกลับใน response body
- ตาราง log รองรับได้ถึง 5,000 แถว → ห้ามมี Radix Popover Root ต่อเซลล์
- hover รัว ๆ ต้องไม่ยิง request ซ้ำ (cache + in-flight dedupe ฝั่ง frontend)

**นอกขอบเขต (ตัดชัดเจน)**

- ไม่ทำ GeoIP offline database (MaxMind GeoLite2) — ใช้ proxy `ipinfo.io` เดิมที่มีอยู่แล้ว
- ไม่แก้/ไม่เพิ่มความสามารถของ ipinfo service เดิม (cache/rate-limit/config key คงเดิมทุกอย่าง)
- ไม่เพิ่มการเลือกช่วงเวลา (window) ใน popover — fix ที่ `1h`
- ไม่ทำ popover ลึกเกิน 2 ระดับ, ไม่ขยายรายการในตัว popover (`+N more` = navigate เท่านั้น)
- ไม่แตะ logic ของ firewall rule generation, การสร้าง/แก้/จัดลำดับ policy rule, SSE log stream
- ไม่มีการเขียนข้อมูลใหม่ลง SQLite และไม่มี migration

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริงแล้ว ณ 2026-08-17)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Route `/statistics/traffic/host/:ip` | **มีแล้ว** | `frontend/src/App.tsx:160` → `frontend/src/pages/StatisticsTrafficHost.tsx` |
| Route `/statistics/dns/domain/:domain`, `/statistics/dns/client/:client` | **มีแล้ว** | `frontend/src/App.tsx:162-163` |
| IP → domains (reverse index) | **มีแล้ว** | `GET /api/statistics/dns/ip` — `backend/internal/api/router.go:60`, handler `backend/internal/api/handlers.go:597`, service `backend/internal/service/statistics_dns.go:641`, index `backend/internal/service/dns_domain_ips.go:275` (`DomainsForIP`) |
| Domain → IPs (forward index) | **มีแล้ว** | `GET /api/statistics/dns/domain` — `handlers.go:523`, `statistics_dns.go:239`, `dns_domain_ips.go:235` (`IPsFor`) |
| Client → top domains | **มีแล้ว** | `GET /api/statistics/dns/client` — `router.go:54` |
| Public IP Info (ASN/Org/Country) | **มีแล้ว, ปิดโดยดีฟอลต์** | `GET /api/statistics/ipinfo` — `router.go:67`, `handlers.go:732`, `backend/internal/service/ipinfo.go`, cache `ipinfo_cache.go`, คีย์คอนฟิก `ipinfo-enabled` |
| guard IP สาธารณะ | **มีแล้ว** | `backend/internal/service/ipinfo.go:57-95` (`isGloballyRoutable`) ครอบคลุม CGNAT/TEST-NET/IPv4-mapped |
| FE client + การ์ด Public IP Info | **มีแล้ว** | `frontend/src/services/ipinfoService.ts` (cache 10 นาที, บรรทัด ~41-63), `frontend/src/components/statistics/PublicIpInfoCard.tsx` (state 4 แบบ) |
| shadcn Popover + `PopoverAnchor` | **มีแล้ว** | `frontend/src/components/ui/popover.tsx:40-44` |
| ตัวจำแนก IP vs Domain | **มีแล้ว (ยังไม่รองรับ CIDR)** | `frontend/src/lib/ipQuery.ts` → `classifyIpQuery()` |
| หน้า Logs Traffic (Forward + Local) | **component ร่วมตัวเดียว** | `frontend/src/components/logs/TrafficLogPage.tsx` — `IpCell` บรรทัด ~154-162 มี `ip`/`domain`/`hostname` ครบในเซลล์เดียวแล้ว, `MAX_ROWS = 5000` บรรทัด ~38 |
| หน้า Policy (Firewall/Local-In/Local-Out) | **component ร่วมตัวเดียว** | `frontend/src/components/policy/PolicyChainPage.tsx` — badge Source/Destination บรรทัด ~226-245, โหลด address objects แล้วที่ ~374/406, useMemo map ที่ ~425/433 |
| Address Object model | **มีแล้ว (มี legacy field)** | `frontend/src/data-mockup/mockData.ts:158-180` — `entries?: AddressEntry[]` เป็น optional, มีคู่ legacy `{type,value}` deprecated |
| deep-link เปิด DNS query logging | **มีแล้ว** | `frontend/src/pages/DnsServer.tsx:78` ระบุ `/network/dns-server?tab=settings`, `useSearchParams` ที่ ~85, `TabsTrigger value="settings"` ที่ ~795 |
| endpoint สรุปแบบเบาสำหรับ hover | **ยังไม่มี** | ต้องสร้างใหม่ (ดู §2) |
| hook hover-delay / popover ร่วม | **ยังไม่มี** | `frontend/src/hooks/` ไม่มี |

> **สรุป:** ข้อมูลดิบและ route ปลายทางมีครบหมดแล้ว งานจริงกระจุกอยู่ที่
> (ก) endpoint สรุปแบบเบา 2 เส้นสำหรับ hover และ (ข) ชั้น UI ที่ใช้ร่วมกันทั้งแอป
> ไม่มีงาน kernel layer, ไม่มี DB migration, ไม่แตะ `install.sh`

---

## 2. แนวทางเทคนิค

**2.1 สร้าง endpoint สรุปแบบเบา แทนการ reuse เส้นเดิม**
`/api/statistics/dns/ip` และ `/dns/domain` เดิมคืน `series[]` (สูงสุด 288 จุด) + `clients[]` + ทุก domain
ที่ match — หนักเกินไปสำหรับ hover ที่อาจเกิดหลายสิบครั้งต่อนาที จึงเพิ่ม
`GET /api/statistics/reference/ip` และ `/reference/domain` ที่คืนเฉพาะ 3 รายการแรก + จำนวนรวม
โดย **reuse index/aggregation เดิมทั้งหมด** (`dns_domain_ips.go`, `statistics_dns.go`) ห้าม
copy-paste logic — ถ้าจำเป็นให้ refactor ส่วนร่วมเป็น unexported helper
*ทางเลือกที่ตัดทิ้ง:* เรียกเส้นเดิมแล้วตัดข้อมูลที่ frontend (payload หนักโดยเปล่าประโยชน์)

**2.2 Public IP Info: reuse `/api/statistics/ipinfo` เดิม (คำตอบ Q1 = ก)**
มี guard/cache/rate-limit/ปิดโดยดีฟอลต์ครบแล้ว ยิงเฉพาะตอน popover เปิดจริงและ `scope=public` เท่านั้น
*ทางเลือกที่ตัดทิ้ง:* MaxMind GeoLite2 offline (DB ~70MB embed ไม่ได้จริง + ต้องมีกลไกอัปเดต + license)
และการ fetch ตรงจาก browser (ชน CSP/privacy และบังคับปิดฟีเจอร์ไม่ได้จริง)

**2.3 แยกโหมด public vs LAN ที่ backend (คำตอบ Q3)**
`/reference/ip` คืน field `scope` เป็น `"public" | "lan"` ตัดสินด้วย **`isGloballyRoutable()` เท่านั้น**
- `public` → `domains` = domain ที่ resolve มาเป็น IP นี้ (`DomainsForIP`) + ปุ่มเดียวไป traffic host
- `lan` → `domains` = top domains ที่เครื่องนี้ query (client ring) + ปุ่มเสริมไป `/statistics/dns/client/<ip>`
*ทางเลือกที่ตัดทิ้ง:* ให้ frontend ตัดสิน scope เอง (FE classifier เป็นแค่ UX guard ไม่ใช่ security boundary)
และการใช้ `isPrivateIP` ใน `statistics.go` (ครอบคลุมช่วงน้อยเกินไป — ดู §5 ข้อ 1)

**2.4 window fix ที่ `1h` (คำตอบ Q4)**
ประกาศ `const referenceWindow = "1h"` ในชั้น service; handler **ไม่เรียก `statsWindowParam`** และไม่รับ
param `window` เลย แต่ยังส่ง `window` กลับใน response เพื่อให้ UI แสดงป้ายช่วงเวลาโดยไม่ hardcode

**2.5 Popover instance เดียวต่อหน้า + virtual anchor**
`ReferenceHoverProvider` ถือ Popover Root แล้วผูก `<PopoverAnchor asChild>` กับ div ที่ position ตาม
`getBoundingClientRect()` ของ element ที่ hover — จำเป็นเพราะ `TrafficLogPage` มี `MAX_ROWS=5000`
รองรับ stack ได้สูงสุด 2 ระดับ (ระดับ 1 delay 1000ms, ระดับ 2 delay ~300ms) ตามคำตอบ Q2

**2.6 Pattern แม่แบบที่ต้องทำตาม**
- handler + validation: `backend/internal/api/handlers.go:523` (`HandleGetDNSDomainClients`) และ `:597` (`HandleGetDNSIPDomains`)
- DTO + doc comment เตือน display-only: `backend/internal/model/statistics.go`
- FE service + cache + mock: `frontend/src/services/ipinfoService.ts`, `frontend/src/services/dnsStatisticsService.ts`
- state 4 แบบของการ์ด ipinfo: `frontend/src/components/statistics/PublicIpInfoCard.tsx`

---

## 3. ขั้นตอนการทำ (เรียงตาม dependency: model → service → api → docs → frontend)

**Step 1 — DTO** · **ไฟล์ใหม่:** `backend/internal/model/statistics_reference.go`
`IPReferenceSummary` (ip, hostname, mac, domain, `scope` enum ประกาศเป็น const, `enabled`, found,
`domains []ReferenceDomainRef`, domainCount, bytes/up/down, window, generatedAt) และ
`DomainReferenceSummary` (domain, `enabled`, found, `ips []ReferenceIPRef`, ipCount, queryCount, clients,
bytes/up/down, sharedIPs, ipsTruncated, window, generatedAt) — slice ต้องไม่เป็น nil
> doc comment ต้องระบุว่า `domains` **เปลี่ยนความหมายตาม `scope`** และย้ำว่า mapping domain↔IP นี้
> display-only จาก dnsmasq answer log ที่ LAN client วางยาได้ ห้ามใช้กับ firewall/policy/routing/QoS

**Step 2 — Service** · **ไฟล์ใหม่:** `backend/internal/service/statistics_reference.go`
`GetIPReference(ip string, limit int)` และ `GetDomainReference(domain string, limit int)` —
ไม่รับ `window`, ใช้ `const referenceWindow = "1h"`; ตัดสิน scope ด้วย `isGloballyRoutable`;
`domainCount`/`ipCount` นับก่อนตัด limit; เรียงลำดับตรงกับหน้า drill-down เดิม; `enabled=false`
(query logging ปิด) → คืน slice ว่าง ไม่ error และไม่แตะ index
> **ไม่ต้อง** คำนวณ `series[]` / per-client join ใด ๆ — เป็นเหตุผลทั้งหมดที่แยกเส้นนี้ออกมา

**Step 3 — Handler + Route** *(sensitive)* · **แก้:** `backend/internal/api/handlers.go`, `backend/internal/api/router.go`
`authRoute("GET /api/statistics/reference/ip", ...)` และ `.../reference/domain`; validate ตามแม่แบบ §2.6;
`limit` ใช้ `clampQueryLimit(r, 3, 10)` (ไม่เคย 400); ignore param `window`/`scope` ที่ client ส่งมาแบบเงียบ

**Step 4 — Tests + OpenAPI** · **แก้:** `backend/internal/api/handlers_test.go`,
**ไฟล์ใหม่:** `backend/internal/service/statistics_reference_test.go`,
**แก้:** `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (ต้อง sync ตรงกันเป๊ะ)
เคสบังคับ: require auth · ip/domain ผิด = 400 และไม่ echo ค่าดิบ · `::ffff:192.168.1.1` ต้องได้ `scope="lan"`
· `?window=24h` แล้ว response ยังเป็น `1h` · `limit=999` = clamp · query logging ปิด = 200 + `enabled:false`

**Step 5 — Classifier** · **ไฟล์ใหม่:** `frontend/src/lib/referenceTarget.ts`
`classifyReferenceTarget()` คืน `ip | cidr | domain | none` โดย **reuse `classifyIpQuery` จาก `lib/ipQuery.ts`**
(ห้ามเขียน parser IP ใหม่); `/32` และ `/128` ตีเป็น `ip`; `ALL`/ชื่อ object → `none`

**Step 6 — FE service** · **ไฟล์ใหม่:** `frontend/src/services/referenceService.ts`
`getIpReference(ip, limit=3)` / `getDomainReference(domain, limit=3)` (ไม่มี param window),
type mirror Go แบบ field-per-field, cache TTL 60s + in-flight dedupe, `encodeURIComponent` ที่นี่ที่เดียว,
โหมด mock ต้อง reuse fixture จาก `dnsStatisticsService.ts` (ถ้าต้องแชร์ให้ย้ายไป module ร่วม ห้าม fixture ซ้ำซ้อน)

**Step 7 — Hook** · **ไฟล์ใหม่:** `frontend/src/hooks/useHoverPopover.ts`
open delay 1000ms / close grace ~200ms / hover-bridge / clear timer ทุกตัวตอน unmount /
`pointerType === "touch"` ห้ามเปิดด้วย hover / Escape และ scroll ปิดทันที / เปิดด้วย focus ไม่ต้องรอ delay

**Step 8 — Provider + Trigger** · **ไฟล์ใหม่:** `frontend/src/components/reference/ReferenceHoverProvider.tsx`,
`frontend/src/components/reference/ReferenceTrigger.tsx`, `frontend/src/hooks/reference-context.ts`
Popover Root เดียวต่อหน้า + virtual `PopoverAnchor`; stack สูงสุด 2 ระดับ (จำกัดในโค้ดชัดเจน ไม่ recursive);
`onOpenAutoFocus={(e) => e.preventDefault()}`; hover-bridge propagate ทั้ง stack; Escape ปิดทีละระดับ

**Step 9 — เนื้อหา popover** · **ไฟล์ใหม่ทั้งหมดใน** `frontend/src/components/reference/`:
`IpReferenceContent.tsx` (2 โหมดตาม `scope` จาก API เท่านั้น — LAN ห้ามยิง ipinfo เด็ดขาด),
`DomainReferenceContent.tsx`, `CombinedReferenceContent.tsx`,
`DnsLoggingDisabledNotice.tsx` (ใช้เมื่อ `enabled === false`: ข้อความ muted + `<Link>` จาก `react-router`
ไป `/network/dns-server?tab=settings` โดยส่วนอื่นของ popover ยังแสดงปกติ)
> ส่วน Public IP Info ให้แยก presentational ออกจาก `PublicIpInfoCard.tsx` มาใช้ร่วม **ห้าม copy ทั้งก้อน**

**Step 10 — Logs Traffic** · **แก้:** `frontend/src/components/logs/TrafficLogPage.tsx`
ห่อหน้าด้วย provider แล้วห่อ `IpCell` (~154-162) ด้วย `ReferenceTrigger`; แถวที่มีทั้ง ip และ domain
ใช้ `CombinedReferenceContent`
> **ห้ามแตะ** `usePaginatedLiveLogs`, `matchesFilter`, pause/clear, SSE และความกว้างคอลัมน์เดิม;
> state ของ popover ต้องอยู่ที่ provider ไม่ใช่ component ที่ถือ `logs`

**Step 11 — Address Object content** · **ไฟล์ใหม่:** `frontend/src/components/reference/AddressObjectReferenceContent.tsx`
แสดงชื่อ object + badge System + entries ทั้งหมด (fallback ไป legacy `{type,value}` เมื่อ `entries` เป็น undefined);
entry ที่เป็น `ip`/`domain` → ห่อ `ReferenceTrigger` เปิดระดับ 2 + ปุ่ม drill-down ต่อแถว;
subnet/range กว้าง → ข้อความเฉย ๆ ไม่ยิง API; entries > 5 → แสดง 5 แรก + "และอีก N รายการ" + ปุ่มไป `/policy/addresses`
> popover ระดับ 1 ต้อง **ไม่มี network request เลย** — ยิง API เฉพาะตอนเข้า entry ระดับ 2

**Step 12 — Policy pages** · **แก้:** `frontend/src/components/policy/PolicyChainPage.tsx`
ห่อ badge Source/Destination (~226-245); lookup ชื่อ → object จาก state เดิม (~374/406) ผ่าน
`Map` + `useMemo` แบบเดียวกับ ~425/433 (ห้ามยิง `addressService.getAll()` ใหม่, ห้าม `.find()` ต่อ hover);
เจอ object → `AddressObjectReferenceContent`, ไม่เจอแต่เป็น IP/FQDN ดิบ → content ตรง, `ALL`/`none` → ไม่ผูก handler;
เปิด Drawer/Dialog เมื่อไร ต้องปิด popover ทั้ง stack

**Step 13 — ขยายไปจุดที่เหลือ (สโคปหลัก ไม่ใช่ optional)** · **แก้:**
`frontend/src/components/policy/RuleStatsDrawer.tsx` (provider อยู่ที่ระดับ Drawer content เพราะเป็น portal — ระวัง z-index),
`frontend/src/components/statistics/DnsStatsShared.tsx`, `frontend/src/components/statistics/TrafficStatsShared.tsx`,
`frontend/src/components/statistics/HostCells.tsx` (`HostNameLines` ~47-103 มี domain+ip ในเซลล์เดียว → ใช้ Combined)
> ห้ามสร้าง component/hook/service ใหม่ใน step นี้ และห้ามแก้ markup/logic เดิมของ `HostNameLines`/`HostLabel`
> (มี comment กำกับว่าเป็น pure move) — เพิ่มเป็น wrapper รอบนอกเท่านั้น และ onClick drill-down เดิมต้องยังทำงานทันที
>
> **หมายเหตุหลังทำจริง (2026-08-17):** step นี้ครอบคลุมเฉพาะ 4 ไฟล์ข้างต้นตามที่แผนระบุ — การ์ด/หน้าอื่น
> ที่แสดง IP/domain เหมือนกัน (`components/statistics/TopHostsShareCard.tsx`, `TrafficTrendCard.tsx`,
> `pages/StatisticsOverview.tsx`, `pages/StatisticsTraffic.tsx`, `pages/Dashboard.tsx`) **ยังไม่มี popover**
> เป็น scope gap ที่ตั้งใจ ไม่ใช่บั๊ก ถ้าต้องการให้ครอบคลุมทั้งแอปต้องเปิดแผนรอบถัดไป
>
> **หมายเหตุเพิ่มเติม (2026-08-18):** ปิด scope gap ข้างต้นบางส่วนแล้ว — หน้า `pages/StatisticsOverview.tsx`,
> `pages/StatisticsTraffic.tsx`, `pages/StatisticsTrafficHost.tsx` และ `components/statistics/TopHostsShareCard.tsx`
> ถูก wire ด้วย `HostReferenceTrigger` (component ใหม่ที่ห่อ `ReferenceTrigger` + classify ip/domain ให้ในที่เดียว
> reuse pattern จาก `RuleStatsDrawer.tsx`) ในรอบนี้แล้ว — `TrafficTrendCard.tsx`/`pages/Dashboard.tsx` ยังไม่ได้ wire

**Step 14 — เอกสาร** · **แก้:** `docs/rules_of_work.md` (กติกา popover 2 ระดับ + delay มาตรฐาน + ห้าม Root ต่อแถว),
`README.md` (Feature Status ถ้าจำเป็น), `docs/tech_stack_design.md` หรือ `docs/ref/` (ย้ำ display-only)

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | ใหม่/เดิม | พฤติกรรม |
|---|---|---|---|---|
| GET | `/api/statistics/reference/ip?ip=<ip>&limit=<1-10>` | `authRoute` (ทุก role ที่ล็อกอิน) | **ใหม่** | คืนสรุป IP: `scope` (public/lan), hostname, bytes, `domains[]` ≤ limit (ดีฟอลต์ 3), `domainCount`, `enabled`, `window="1h"` เสมอ · `ip` ผิด = 400 ข้อความ generic ไม่ echo ค่าดิบ · `limit` นอกช่วง = clamp · param `window`/`scope` ถูก ignore |
| GET | `/api/statistics/reference/domain?domain=<d>&limit=<1-10>` | `authRoute` | **ใหม่** | คืนสรุป domain: `ips[]` ≤ limit, `ipCount`, `queryCount`, `sharedIPs`, `enabled`, `window="1h"` · `domain` ต้องผ่าน `model.NormalizeQueryDomain` ไม่ผ่าน = 400 |
| GET | `/api/statistics/ipinfo?ip=<ip>` | `authRoute` | เดิม (ไม่แก้) | ใช้ต่อจาก popover เฉพาะ `scope=public` · ปิดโดยดีฟอลต์ → 404 = "ฟีเจอร์ปิดอยู่", 429/502 = "ไม่พร้อมใช้งานชั่วคราว" |
| GET | `/api/statistics/dns/ip`, `/dns/domain`, `/dns/client`, `/traffic/host` | `authRoute` | เดิม (ไม่แก้) | ปลายทางของปุ่ม drill-down / `+N more` |

**โหมด `-disable-edit=true`:** ทั้งฟีเจอร์เป็น GET ล้วน `DisableEditMiddleware` บล็อกเฉพาะ mutation
จึงใช้งานได้ตามปกติในโหมด read-only — ถูกต้องแล้วสำหรับงานนี้ ไม่ต้องเพิ่มเงื่อนไขใด ๆ

---

## 5. ข้อควรระวัง

1. **`isGloballyRoutable` เท่านั้น ห้าม `isPrivateIP`** — `backend/internal/service/statistics.go` มี
   `isPrivateIP` ที่เป็น display-only และครอบคลุมน้อยเกินไป (ไม่กัน CGNAT/TEST-NET/IPv4-mapped)
   ถ้าเผลอใช้ตัวนั้นตัดสิน scope จะได้ `scope=public` สำหรับ IP ภายใน แล้ว popover จะส่ง IP ภายใน
   ออกไปหา ipinfo.io = ข้อมูล topology ภายในรั่ว → บังคับใช้ `isGloballyRoutable` ใน `ipinfo.go:57` เท่านั้น
   และมีเทสต์ `::ffff:192.168.1.1` เป็นด่านจับ
2. **`MAX_ROWS = 5000` ใน `TrafficLogPage.tsx`** — ถ้าห่อทุกเซลล์ด้วย `<Popover>` Root จะได้ Radix Root
   นับหมื่นตัว หน้าค้าง/หน่วงตอน stream log → บังคับใช้ provider + virtual `PopoverAnchor` (Step 8)
   และตรวจใน React DevTools ว่ามี Root ไม่เกิน 2 ตัว
3. **Combobox/Drawer ใน `PolicyChainPage.tsx`** — ไฟล์นี้มี `useComboboxAnchor` (~559) และ Combobox ในฟอร์ม
   (~1130+) ซึ่ง `docs/rules_of_work.md` ระบุว่าไวต่อ focus/pointer blocker; Popover ที่ trap focus หรือ
   ค้างทับ จะทำให้ดรอปดาวน์คลิกไม่ได้ → ต้อง `onOpenAutoFocus` preventDefault และปิด popover ทั้ง stack
   เมื่อ Drawer/Dialog เปิด แล้วทดสอบ drag-reorder + เปิดฟอร์มแก้ rule ทุกครั้ง
4. **hover storm** — ตาราง log 500+ แถวถ้าไม่มี cache จะยิง request รัวจนหน่วงและกิน rate-limit ของ
   ipinfo ทิ้งเปล่า → cache 60s + in-flight dedupe ใน `referenceService.ts` และยิง ipinfo **หลัง popover
   เปิดจริงเท่านั้น** ห้าม prefetch ตอนเริ่ม hover
5. **ค่าในคอลัมน์ Policy ไม่ใช่ IP** — เป็นชื่อ Address Object ที่ resolve เป็น entries หลายรายการ และ
   `entries` เป็น optional (มีคู่ legacy `{type,value}` — `mockData.ts:167-177`) ถ้าอ่าน `entries[0]` ตรง ๆ
   จะ crash กับข้อมูลเก่าใน localStorage → ต้อง fallback เสมอ
6. **ข้อมูล domain↔IP เป็น display-only และ poisonable** — LAN client query ชื่อที่ตัวเองคุมได้ก็ทำให้
   index เปลี่ยนได้ ห้ามนำค่าจาก popover ไปใช้ตัดสิน firewall/policy/routing/QoS ทุกกรณี (มี comment เตือน
   อยู่แล้วที่ `handlers.go:509-511` ให้คงระดับเดียวกันใน DTO ใหม่)
7. **ค่าดิบจาก client ห้ามสะท้อนกลับ** — ทั้งสอง handler ใหม่ต้องคืนข้อความ generic (`"invalid ip"` /
   `"invalid domain"`) และ re-serialize ด้วย `addr.String()` ก่อนส่งลง service เพื่อให้ IPv6 non-canonical
   ตกลง key เดียวกับ index
8. **popover ทับกับ SSE stream** — log ไหลเข้าเรื่อย ๆ ถ้า state popover อยู่ใน component ที่ re-render
   ตาม `logs` popover จะกระพริบ/ปิดเอง → เก็บ state ไว้ที่ provider เท่านั้น
9. **การทดสอบ** — ทดสอบได้ครบทุก flow ใน `-mock=true` (ไม่มีความเสี่ยงล็อกตัวเองออกจากเครื่อง เพราะเป็น
   GET/read-only ล้วน ไม่แตะ firewall/network) ยกเว้นข้อมูล ipinfo จริงที่ต้องเปิด `ipinfo-enabled` บนบอร์ดจริง
   · **ข้อจำกัดที่พบจริง (2026-08-17):** ai-developer และ ai-qa ไม่มี browser tool จึงตรวจพฤติกรรม hover/
   timing/สี ได้ด้วยการอ่านโค้ดเท่านั้น — ข้อทดสอบที่ต้องใช้เบราว์เซอร์จึงยังไม่ติ๊กใน §6 และเป็นงานของ
   เจ้าของโปรเจกต์ก่อน merge
10. **สไตล์** — ห้าม `shadow-*`/`backdrop-blur-*` และห้ามคลาสสีดิบในไฟล์ใหม่ทุกไฟล์, ต้องผ่านทั้ง dark/light,
    router import จาก `"react-router"` เท่านั้น (v8 ไม่มี `react-router-dom`)

---

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [x] `backend/internal/model/statistics_reference.go` — DTO ครบ + `scope` เป็น const enum + doc comment เตือน display-only
- [x] `backend/internal/service/statistics_reference.go` — `GetIPReference`/`GetDomainReference`, `referenceWindow="1h"`, reuse index เดิม ไม่มี series
- [x] `backend/internal/api/handlers.go` — 2 handler ใหม่ validate ครบ ไม่ echo ค่าดิบ
- [x] `backend/internal/api/router.go` — 2 `authRoute` ใหม่
- [x] `backend/internal/api/handlers_test.go` + `backend/internal/service/statistics_reference_test.go` — ครอบคลุม auth/400/`::ffff:192.168.1.1`/window fix/clamp/`enabled:false`
- [x] `go build ./...` และ `go test ./...` ผ่าน

**Frontend**
- [x] `frontend/src/lib/referenceTarget.ts` — reuse `classifyIpQuery`, รองรับ CIDR, `/32`,`/128` = ip
- [x] `frontend/src/services/referenceService.ts` — cache 60s + dedupe + mock 2 โหมด (public/lan) + ไม่ส่ง window
- [x] `frontend/src/hooks/useHoverPopover.ts` — delay 1s, hover-bridge, cleanup timer, touch/Escape/scroll
- [x] `frontend/src/components/reference/ReferenceHoverProvider.tsx` + `ReferenceTrigger.tsx` + `frontend/src/hooks/reference-context.ts` — Root เดียวต่อหน้า, stack ≤ 2 ระดับ
- [x] `frontend/src/components/reference/{IpReferenceContent,DomainReferenceContent,CombinedReferenceContent,DnsLoggingDisabledNotice}.tsx`
- [x] `frontend/src/components/reference/AddressObjectReferenceContent.tsx` — entries + fallback legacy + ไม่ยิง API ระดับ 1
- [x] `frontend/src/components/logs/TrafficLogPage.tsx` — wiring, ไม่แตะ SSE/pagination
- [x] `frontend/src/components/policy/PolicyChainPage.tsx` — wiring + Map lookup + ปิด popover เมื่อเปิด Drawer
- [x] `frontend/src/components/policy/RuleStatsDrawer.tsx`, `frontend/src/components/statistics/DnsStatsShared.tsx` — wiring ไม่มี component ใหม่ (`TrafficStatsShared.tsx`/`HostCells.tsx` เองไม่มี `ReferenceTrigger` — เดิมอ้างผิด แก้แล้ว; wiring จริงของหน้า DNS stats อยู่ใน `DnsStatsShared.tsx`; scope gap ที่เหลือ: `TrafficTrendCard.tsx`/`pages/Dashboard.tsx` ยังไม่ได้ wire — ดูหมายเหตุ Step 13)
- [x] `yarn build` และ `yarn lint` ผ่าน

**ทดสอบ (mock mode: `./pigate-backend -mock=true -allow-dev-cors` + `yarn dev`)**
- [ ] hover < 1s ไม่เปิด / ครบ 1s เปิด / ลากเข้า popover ไม่ปิด / ออกทั้งสองส่วนปิดใน ~200ms — *ต้องทดสอบบนเบราว์เซอร์จริง (ยังไม่ทำ)*
- [x] IP สาธารณะ: badge Internet + domains ≤ 3 + `+N more` + Public IP Info + ปุ่มไป `/statistics/traffic/host/<ip>` (ตรวจด้วย logic review)
- [x] IP LAN: badge LAN + top domains ที่ query + **2 ปุ่ม** (traffic host + `/statistics/dns/client/<ip>`) และ **ไม่มี request ไป `/api/statistics/ipinfo`** (ตรวจด้วย logic review — ควรยืนยันซ้ำใน DevTools)
- [x] Domain: resolved IPs ≤ 3 + `+N more` + ปุ่มไป `/statistics/dns/domain/<domain>` (ทดสอบ `www.youtube.com` ที่มีหลาย IP)
- [x] แถว Logs Traffic ที่มีทั้ง IP+Domain = popover เดียว (ไม่ใช่สองอัน) ทั้งหน้า Forward และ Local
- [x] Policy: hover ชื่อ Address Object → entries ครบ; hover entry IP/FQDN → popover ระดับ 2 เปิดโดยระดับ 1 ไม่ปิด; subnet กว้างไม่มีปุ่ม/ไม่ยิง API; `ALL` ไม่มี popover; entries > 5 แสดง 5 + "และอีก N รายการ"
- [ ] Policy: drag-reorder / เปิด-ปิด Drawer / Combobox ในฟอร์ม ทำงานปกติทั้ง Firewall, Local-In, Local-Out — *ต้องทดสอบบนเบราว์เซอร์จริง (ยังไม่ทำ)*
- [ ] RuleStatsDrawer: popover ไม่ถูก Drawer บัง (z-index) · ตาราง Statistics: click drill-down เดิมยังทำงานทันที — *ต้องทดสอบบนเบราว์เซอร์จริง (ยังไม่ทำ)*
- [x] ปิด DNS query logging → เห็นข้อความ muted + ลิงก์ที่กดแล้วเปิด `/network/dns-server?tab=settings` จริง โดยส่วนอื่นของ popover ยังแสดงปกติ
- [x] `+N more` และรายการในลิสต์ = navigate ไป drill-down ทุกจุด (ไม่ขยายในที่)
- [ ] hover 10 แถวที่มีค่าซ้ำ = 1 request ต่อค่า · ตาราง 2000+ แถว hover 20 ครั้ง ไม่กระตุก ไม่มี React warning · Popover Root ≤ 2 — *ต้องทดสอบบนเบราว์เซอร์จริง (ยังไม่ทำ)*
- [x] ยิง `?ip=not-an-ip`, `?ip=<script>`, `?domain=<script>` ได้ 400/401 และ body ไม่มีค่าที่ส่งเข้าไป
- [x] `?window=24h` แล้ว response `window` ยังเป็น `1h` และ popover แสดงป้าย "1 ชั่วโมงล่าสุด"
- [ ] dark/light ผ่านทั้งคู่ — *ต้องดูด้วยตาบนเบราว์เซอร์จริง (ยังไม่ทำ)* · [x] grep ไม่พบ `shadow-*`/`backdrop-blur-*`/คลาสสีดิบในไฟล์ใหม่

**เอกสาร**
- [x] `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` เพิ่ม 2 path ใหม่ + schema (รวม `scope` enum, `window` fix 1h) และ **เนื้อหาตรงกันทั้งสองไฟล์**
- [x] `docs/rules_of_work.md` เพิ่มกติกา popover (2 ระดับ, delay 1000/300ms, ห้าม Root ต่อแถวตาราง)
- [x] `README.md` Feature Status อัปเดต (ถ้าจำเป็น) · ย้ำ display-only ใน `docs/tech_stack_design.md`/`docs/ref/`
- [ ] โค้ดทั้งหมดอยู่บน `feat/reference-popover` และเข้า main ผ่าน PR เท่านั้น — *รอเจ้าของสั่ง commit/เปิด PR*
