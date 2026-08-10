# Statistics → DNS: แก้ตามรีวิว PR 127 (ยุบคอลัมน์ Down/Up เป็น "Traffic" + แก้บั๊ก Clients/IPs = 0)

> เอกสารแผนงานสำหรับ 3 คอมเมนต์รีวิวของเจ้าของ repo บน PR 127
> (`feat/statistics-dns-page` — domain-centric DNS statistics with volume + drill-down)
>
> วันที่เขียน: 2026-08-06 · อ้างอิงโค้ด: `feat/statistics-dns-page` @ `088f127`
> ผู้เขียน: ai-tech-lead (สำรวจโค้ดจริงก่อนวางแผน ไม่ได้เดาชื่อไฟล์/ฟิลด์)
>
> **ขอบเขต: หน้า Statistics → DNS ทั้ง 3 หน้าเท่านั้น** — ห้ามแตะหน้า Statistics → Traffic
> (`pages/StatisticsTraffic.tsx`, `pages/StatisticsTrafficHost.tsx`) ซึ่ง *เป็น* สถิติทราฟฟิกจริง
> และต้องคงคอลัมน์ Down / Up / Total แยกไว้เหมือนเดิม

## 0. คอมเมนต์รีวิวที่ต้องแก้ (ต้นทาง)

| # | คอมเมนต์ | สรุปเชิงเทคนิค |
|---|---|---|
| R-1 | ไม่ต้องแสดง Down/Up แยก และ Vol % ของ Down/Up — นี่คือสถิติ DNS ไม่ใช่ Traffic | ลบคอลัมน์ `Down` และ `Up` ออกจากทุกตารางในหน้า DNS |
| R-2 | เปลี่ยนชื่อ "Total" → "Traffic" และแสดง 2 บรรทัดในเซลล์เดียว (บน = Down, ล่าง = Up), sort ตามผลรวม | คอลัมน์เดียว label `Traffic`, `sortKey="bytes"` (backend การันตี `bytesUp + bytesDown == bytes`) |
| R-3 | บั๊ก: หน้า client drill-down แสดง Clients/IPs = 0 ทั้งที่ overview และ domain drill-down แสดงปกติ | **ไม่ใช่บั๊ก render — เป็นสัญญา DTO ฝั่ง backend ที่จงใจไม่ใส่ค่า** (รายละเอียด §2) |

### D-1 คำถามที่ต้องให้เจ้าของโปรเจกต์ยืนยัน (ไม่บล็อกการเริ่มงาน — ค่า default ระบุไว้แล้ว)

1. **คอลัมน์ `% Vol` เก็บไว้ไหม** — ปัจจุบันมี `bytesPercent` เพียงตัวเดียวต่อแถว ซึ่งเป็น
   "% ของ *ผลรวม* bytes" ไม่ใช่ % แยก Down/Up (ไม่เคยมี % แยกทิศทางในโค้ด) ผมตีความว่า R-1
   หมายถึง "ตัดคอลัมน์ Down/Up และ % ที่ผูกกับทิศทาง" → **default ของแผนนี้คือคงคอลัมน์
   `% Vol` ไว้** (มันคือ % ของ Traffic รวม) ถ้าเจ้าของต้องการตัดทิ้งด้วย บอกได้ ลบง่ายมาก (1 บรรทัด/ตาราง)
2. **การ์ด `TrafficStatCard` (Volume) ที่มี breakdown Down/Up 2 บรรทัด** — คนละอย่างกับตาราง
   และรูปแบบ 2 บรรทัดตรงกับที่ R-2 ต้องการอยู่แล้ว → **default: ไม่แตะ** (แค่ทบทวนชื่อ label ใน T-07)
3. **บั๊กแฝดของ R-3**: หน้า *domain* drill-down ตาราง "Source Hosts" มีคอลัมน์ `Domains`
   ที่ backend จงใจปล่อยเป็น 0 เหมือนกัน (`statistics_dns.go:325-326`) เจ้าของไม่ได้พูดถึง
   แต่เป็นอาการเดียวกันเป๊ะ → แผนนี้ใส่ไว้เป็น **T-06 (optional)** ให้เจ้าของสั่งทำ/ไม่ทำ

**อัปเดต 2026-08-06 — เจ้าของ repo ยืนยันแล้วทั้ง 2 ข้อ**: (1) คงคอลัมน์ `% Vol` ไว้ตาม default
ของแผนนี้ ไม่ตัดทิ้ง (2) T-06 ให้ทำในรอบนี้เลย ไม่ใช่ optional อีกต่อไป — เปลี่ยนสถานะจาก
"optional / รอเจ้าของสั่ง" เป็น mandatory เหมือน T-01 ถึง T-05/T-07 ถึง T-10 ทุกงาน (T-01…T-10)
ถูก implement ครบแล้วโดย ai-developer ในรอบเดียวกัน ผลตรวจสอบ: `go build`/`go vet`/
`go test ./... -race` (backend) และ `yarn build`/`yarn lint` (frontend) ผ่านทั้งหมด

## 1. สถานะปัจจุบัน (สำรวจแล้ว)

| ส่วน | สถานะ | ไฟล์:บรรทัด (~) |
|---|---|---|
| ตารางทั้ง 3 ของหน้า DNS | `DomainStatsTable`, `ClientStatsTable`, `DomainIpTable` อยู่ในไฟล์เดียว ใช้ร่วมกัน 3 หน้า | `frontend/src/components/statistics/DnsStatsShared.tsx:118-351` |
| คอลัมน์ Down/Up/Total ที่ต้องยุบ | DomainStats `:144-146` + cell `:186-188`; ClientStats `:229-231` + cell `:265-267`; DomainIp `:307-309` + cell `:326-328` | `DnsStatsShared.tsx` |
| hook sort/filter | `useSortableRows` / `SortableHead` / `useTextFilter` — sort ตาม `keyof T` เท่านั้น (ไม่มี custom accessor) | `frontend/src/components/statistics/TrafficStatsShared.tsx:26-98,138-149` |
| ค่า `bytes` = up+down หรือไม่ | ใช่ — สัญญาระบุชัดใน DTO ทั้ง Go และ TS (`bytesUp + bytesDown == bytes` เสมอ) | `services/dnsStatisticsService.ts:56-70,92-94` |
| หน้า overview | ใช้ทั้ง DomainStatsTable + ClientStatsTable | `frontend/src/pages/StatisticsDns.tsx:158,171` |
| หน้า domain drill-down | ใช้ DomainIpTable + ClientStatsTable | `frontend/src/pages/StatisticsDnsDomain.tsx:164,172` |
| หน้า client drill-down | ใช้ DomainStatsTable (คอลัมน์ Clients/IPs อยู่ที่นี่) | `frontend/src/pages/StatisticsDnsClient.tsx:165` |
| หน้า Traffic (ห้ามแตะ) | มีคอลัมน์ Down/Up/Total ของตัวเอง — ยังต้องคงไว้ | `pages/StatisticsTraffic.tsx:83-85`, `pages/StatisticsTrafficHost.tsx:213-215` |

## 2. Root cause ของ R-3 (วิเคราะห์จากโค้ดจริง ไม่ใช่การเดา)

**อาการ**: `/statistics/dns/client/:client` ตาราง "โดเมนที่เครื่องนี้ค้นหา" คอลัมน์ `Clients` และ
`IPs` เป็น 0 ทุกแถว ขณะที่ตารางเดียวกัน (component เดียวกัน) บนหน้า overview แสดงค่าปกติ

**สาเหตุจริง**: ไม่ได้อยู่ที่ frontend เลย — `DomainStatsTable` อ่าน `d.clients` / `d.ipCount`
ตรงๆ (`DnsStatsShared.tsx:173,181`) ค่ามันเป็น 0 เพราะ **backend ส่ง 0 มาโดยเจตนา**:

- `backend/internal/service/statistics_dns.go:471-488` — `GetDNSClientDomains` สร้าง
  `model.DNSDomainStat` โดยใส่เฉพาะ `TopDomain` + `Bytes/BytesUp/BytesDown/BytesPercent`
  แล้วมีคอมเมนต์กำกับว่า *"Clients/IPCount/SharedIPs are not meaningful in a
  single-client drill-down row — left at zero value"*
- เส้นทาง `client == dnsUnknownClient` (บรรทัด `:420-435`) ยิ่งชัด: `DNSDomainStat{TopDomain: td}` เปล่าๆ
- สัญญานี้ถูกเขียนล็อกไว้ 3 ที่: `model/statistics.go:406-412`, `docs/openapi.yaml:5327-5336`,
  และคอมเมนต์ TS `services/dnsStatisticsService.ts:168-172`

กล่าวคือ ตอนออกแบบ PR 127 ตีความว่า "ในบริบทเครื่องเดียว ตัวเลขนี้ไม่มีความหมาย" แต่ UI กลับยัง
render คอลัมน์เดิมอยู่ → ผู้ใช้เห็น "0" ซึ่งอ่านเป็น "ไม่มีข้อมูล" (ผิด) แทนที่จะเป็น "ไม่เกี่ยวข้อง"

**ทางเลือกการแก้ (เลือกทางที่ 2)**

| ทาง | วิธี | ข้อดี | ข้อเสีย |
|---|---|---|---|
| 1 | frontend ซ่อน 2 คอลัมน์นี้เมื่ออยู่ในหน้า client (prop `variant`) | แก้จุดเดียว ไม่แตะ backend | เจ้าของบอกว่า "ควรมีข้อมูล" → ซ่อนไม่ตอบโจทย์ |
| **2 (เลือก)** | backend เติมค่าที่ *มีความหมายจริง* ให้ทั้ง 2 ฟิลด์ | ตรงตามที่เจ้าของคาดหวัง, ตารางเดียวกันความหมายเดียวกันทุกหน้า | ต้องแก้ service + model doc + openapi + mock frontend |

**ความหมายที่ล็อกไว้สำหรับหน้า client drill-down** (ต้องเขียนคอมเมนต์กำกับให้ชัดทุกที่):

- `ipCount` / `sharedIps` = **จำนวน IP ที่โดเมนนั้นรู้จักใน forward index (ทั้งระบบ)** —
  ความหมายเดียวกับหน้า overview เป๊ะ ไม่ขึ้นกับ client
- `clients` = **จำนวน client ที่ถามโดเมนนั้นในช่วงเวลานี้ (ทั้งระบบ)** — ความหมายเดียวกับ overview
  (ไม่ใช่ "1" ที่ไร้ประโยชน์) ตอบคำถาม "โดเมนนี้มีเครื่องอื่นในบ้านใช้ด้วยกี่เครื่อง"

**ข้อมูลดิบมีครบแล้ว ไม่ต้องเพิ่มแหล่งข้อมูลใหม่**:

- `clients`: ลูป ring ใน `GetDNSClientDomains` (`:392-405`) วน `for domain, clients := range b.pairs`
  อยู่แล้ว → เก็บ set ของ client ต่อ domain ระหว่างทางได้เลย (วิธีเดียวกับ
  `GetDNSQueryStatistics:66-84` ซึ่งทำ `domainClients` อยู่แล้ว)
- `ipCount/sharedIps`: `s.dns.domainIPs` มีข้อมูลครบ แต่ **ห้ามใช้ `Snapshot()` ที่เรียกอยู่แล้ว
  มาคำนวณ** เพราะ `Snapshot()` เป็น map `ip -> domain` ที่ตัด IP ที่ shared ทิ้งเหลือโดเมนเดียว
  (`dns_domain_ips.go:225-244`) → นับ IP ขาด และหา `shared` ไม่ได้ ต้องเพิ่ม method ใหม่ (T-03)

## 3. Task list

### T-01 — เพิ่ม `DnsTrafficCell` (เซลล์ 2 บรรทัด Down/Up) ใน DnsStatsShared

```json
{
  "task_id": "T-01",
  "title": "เพิ่ม component เซลล์ Traffic แบบ 2 บรรทัด (Down บน / Up ล่าง)",
  "layer": "frontend",
  "files": ["frontend/src/components/statistics/DnsStatsShared.tsx"],
  "instruction": "เพิ่ม export component เล็ก ๆ ชื่อ DnsTrafficCell({ down, up }: { down: number; up: number }) ในไฟล์นี้ (วางไว้ก่อน DomainStatsTable ~บรรทัด 110) render เป็น <div className=\"flex flex-col items-end leading-tight\"> 2 บรรทัด: บรรทัดบน = ค่า down ใช้ fmtBytes(down) สี text-primary, บรรทัดล่าง = ค่า up ใช้ fmtBytes(up) สี text-muted-foreground ทั้งคู่ font-mono text-xs ใส่ ArrowDown/ArrowUp จาก lucide-react ขนาด size-3 นำหน้าแต่ละบรรทัด (import เพิ่มจาก lucide-react ที่ import อยู่แล้วบรรทัด 3) และใส่ title/aria-label ที่อ่านออกได้ เช่น title={`Down ${fmtBytes(down)} · Up ${fmtBytes(up)}`}. ห้ามใช้ hardcoded palette class (เช่น text-emerald-500) ต้องใช้ theme variable ตาม docs/rules_of_work.md, ห้ามใส่ shadow-*/backdrop-blur-*. ยังไม่ต้องแก้ตารางใด ๆ ในงานนี้",
  "acceptance": [
    "มี export function DnsTrafficCell ในไฟล์ และ tsc คอมไพล์ผ่าน (yarn build)",
    "ใช้เฉพาะ theme color variable ไม่มี hardcoded palette / shadow / backdrop-blur",
    "ยังไม่มีตารางไหนเรียกใช้ (จะเรียกใน T-02) — ไม่มี lint error unused ถ้า export ไว้"
  ],
  "depends_on": []
}
```

### T-02 — ยุบคอลัมน์ Down/Up/Total → "Traffic" ในทั้ง 3 ตาราง

```json
{
  "task_id": "T-02",
  "title": "แทนที่คอลัมน์ Down / Up / Total ด้วยคอลัมน์เดียวชื่อ Traffic ในทุกตารางของหน้า DNS",
  "layer": "frontend",
  "files": ["frontend/src/components/statistics/DnsStatsShared.tsx"],
  "instruction": "ในไฟล์นี้ แก้ 3 ตาราง (DomainStatsTable ~118-201, ClientStatsTable ~205-280, DomainIpTable ~288-351) ดังนี้ ทุกตารางทำเหมือนกัน: (1) ลบ SortableHead ของ label=\"Down\" (sortKey bytesDown) และ label=\"Up\" (sortKey bytesUp) ทิ้ง (2) เปลี่ยน SortableHead label=\"Total\" เป็น label=\"Traffic\" คง sortKey=\"bytes\" ไว้เหมือนเดิม (bytes = bytesUp + bytesDown เป็นสัญญาที่ backend การันตี จึงเท่ากับ sort ตามผลรวมตามที่รีวิวขอ) เปลี่ยน className จาก w-24 เป็น w-28 และเพิ่ม title/tooltip อธิบายว่า 'บรรทัดบน = Down, บรรทัดล่าง = Up, เรียงตามผลรวม' — ถ้า SortableHead ไม่รับ prop title ห้ามแก้ signature ของ SortableHead (มันแชร์กับหน้า Traffic) ให้ใส่คำอธิบายนี้ไว้ที่ข้อความใต้ตาราง/คอมเมนต์แทน (3) ในส่วน TableBody ลบ TableCell ของ bytesDown และ bytesUp ทิ้ง แล้วเปลี่ยน TableCell ของ bytes เป็น <TableCell className=\"py-3 text-right\"><DnsTrafficCell down={x.bytesDown} up={x.bytesUp} /></TableCell> (4) ปรับ colSpan ของแถว empty state ให้ตรงจำนวนคอลัมน์ใหม่: DomainStatsTable 10 -> 8, ClientStatsTable 8 -> 6, DomainIpTable 7 -> 5 (5) คงคอลัมน์ % Vol (bytesPercent) ไว้ทุกตาราง (ดู D-1 ข้อ 1) (6) ถ้า fmtBytes ไม่ถูกใช้แล้วในไฟล์นี้ ให้เอา import ออกให้ lint ผ่าน. ห้ามแตะไฟล์ pages/StatisticsTraffic.tsx และ pages/StatisticsTrafficHost.tsx เด็ดขาด — สองหน้านั้นต้องคง Down/Up/Total แยกไว้",
  "acceptance": [
    "ทั้ง 3 ตารางไม่มี header ชื่อ Down หรือ Up เหลืออยู่แล้ว และมี header ชื่อ Traffic แทน Total",
    "จำนวน <SortableHead>+<TableHead> ต่อแถว header ตรงกับ colSpan ของ empty state ทุกตาราง (8 / 6 / 5)",
    "yarn build (tsc -b && vite build) และ yarn lint ผ่าน ไม่มี unused import",
    "grep แล้วไฟล์ pages/StatisticsTraffic.tsx, pages/StatisticsTrafficHost.tsx ไม่มี diff"
  ],
  "depends_on": ["T-01"]
}
```

### T-03 — [backend] เพิ่ม method นับ IP ต่อโดเมนแบบล็อกครั้งเดียวใน forward index

```json
{
  "task_id": "T-03",
  "title": "เพิ่ม dnsDomainIPs.StatsFor(domains []string) สำหรับดึง (ipCount, shared) หลายโดเมนในล็อกเดียว",
  "layer": "service",
  "files": ["backend/internal/service/dns_domain_ips.go"],
  "instruction": "เพิ่ม type และ method ใหม่ในไฟล์นี้: type domainIPStat struct { Count int; Shared bool } และ func (d *dnsDomainIPs) StatsFor(domains []string) map[string]domainIPStat. พฤติกรรมต้องสอดคล้องกับ IPsFor (บรรทัด 187-213) ทุกประการ: ถือ write lock ครั้งเดียวตลอดการทำงาน (เพราะมี lazy TTL eviction), วนเฉพาะโดเมนที่ส่งเข้ามา, เรียก evictExpiredInDomainLocked ต่อโดเมนก่อนนับ, Count = จำนวน IP ที่ยังไม่หมดอายุของโดเมนนั้น, Shared = true เมื่อมี IP อย่างน้อยหนึ่งตัวที่ d.ipRefs[ip] > 1, โดเมนที่ไม่มีใน index ให้ไม่ต้องใส่ key (ผู้เรียกอ่านค่า zero ได้เอง). ห้ามใช้ Snapshot() มาคำนวณแทน เพราะ Snapshot ยุบ IP ที่ shared ให้เหลือโดเมนเดียว (ดู doc comment บรรทัด 215-224) จะนับขาด. ห้ามเพิ่ม goroutine/ticker ใหม่ (ข้อห้ามเดิมของไฟล์นี้) เขียน doc comment อธิบายว่าเพิ่มมาทำไม อ้างอิงแผนนี้ และเพิ่ม unit test ใน backend/internal/service (ไฟล์ test ที่มีอยู่ของ dns domain ips หรือสร้างใหม่ตามที่มี) ครอบเคส: โดเมนไม่รู้จัก, โดเมนมี IP ปกติ, โดเมนที่มี IP shared กับโดเมนอื่น, entry หมดอายุ TTL",
  "acceptance": [
    "go build ./... และ go vet ./... ผ่าน",
    "StatsFor ถือ lock ครั้งเดียว ไม่เรียก IPsFor วนต่อโดเมน",
    "มี unit test ครอบ 4 เคสข้างต้นและ go test ./internal/service/... ผ่าน"
  ],
  "depends_on": []
}
```

### T-04 — [backend] เติม Clients / IPCount / SharedIPs ใน GetDNSClientDomains (แก้ R-3)

```json
{
  "task_id": "T-04",
  "title": "GetDNSClientDomains ต้องส่ง clients/ipCount/sharedIps ที่มีความหมายจริง แทนค่า 0",
  "layer": "service",
  "files": ["backend/internal/service/statistics_dns.go"],
  "instruction": "แก้ GetDNSClientDomains (บรรทัด ~370-505) ให้เติม 3 ฟิลด์ที่ตอนนี้ปล่อยเป็น zero value: (1) ในลูป ring บรรทัด ~392-405 ที่วน for domain, clients := range b.pairs อยู่แล้ว ให้สร้าง domainClients map[string]map[string]struct{} เก็บ set ของ client ทุกตัวต่อ domain (union ข้าม bucket) — ทำแบบเดียวกับ GetDNSQueryStatistics บรรทัด 66-84 เป๊ะ ๆ และหลังจบลูปให้เก็บไว้เฉพาะ domain ที่อยู่ใน domainTotals เพื่อไม่ให้ map โตเกินจำเป็น ห้ามเพิ่มการ lock ใหม่ ห้ามยืด critical section ของ s.dns.mu ออกไปเกินลูปเดิม (locking discipline ตามคอมเมนต์หัวไฟล์บรรทัด 25-34: ปล่อย s.dns.mu ก่อนแตะ domainIPs/traffic เสมอ) (2) หลังปล่อย s.dns.mu แล้ว เรียก s.dns.domainIPs.StatsFor(<รายชื่อโดเมนใน baseDomains>) หนึ่งครั้ง (method ใหม่จาก T-03) — เรียกครั้งเดียวเท่านั้น ห้ามเรียกต่อโดเมน (3) ใส่ค่าลง model.DNSDomainStat ทั้งใน loop ปกติ (บรรทัด ~471-488) และใน early-return path ของ client == dnsUnknownClient (บรรทัด ~420-435): Clients = len(domainClients[domain]), IPCount = stat.Count, SharedIPs = stat.Shared — เส้นทาง unknown ก็ต้องเติมด้วย เพราะ 3 ฟิลด์นี้ไม่ต้องพึ่ง conntrack join เลย แต่ยังคงข้อห้ามเดิมไว้เคร่งครัด: เส้นทาง unknown ห้ามเรียก GetTrafficBreakdown* / hostLookup ใด ๆ (4) แก้คอมเมนต์เก่าที่บอกว่า 'not meaningful ... left at zero value' ให้ตรงกับความหมายใหม่: ipCount/sharedIps = IP ที่โดเมนนั้นรู้จักทั้งระบบ, clients = จำนวน client ทั้งระบบที่ถามโดเมนนั้นในช่วงเวลานี้ (ไม่ขึ้นกับ client ที่กำลัง drill-down) (5) ห้ามเปลี่ยนความหมาย/denominator ของ bytes/bytesPercent เดิมแม้แต่นิดเดียว",
  "acceptance": [
    "go build ./... , go vet ./... ผ่าน",
    "จำนวนการเรียก GetTrafficBreakdown*/hostLookup ต่อ request ยังคงเป็น 1 (หรือ 0 สำหรับ unknown) ตามกฎในหัวไฟล์",
    "s.dns.mu ไม่ถูกถือไว้ระหว่างเรียก domainIPs.StatsFor",
    "ค่าที่ส่งออกของ Bytes/BytesUp/BytesDown/BytesPercent ไม่เปลี่ยน"
  ],
  "depends_on": ["T-03"]
}
```

### T-05 — [backend] อัปเดต test ของ statistics_dns ให้ครอบสัญญาใหม่

```json
{
  "task_id": "T-05",
  "title": "เพิ่ม/แก้ unit test ยืนยันว่า client drill-down ส่ง clients/ipCount/sharedIps ถูกต้อง",
  "layer": "service",
  "files": ["backend/internal/service/statistics_dns_test.go"],
  "instruction": "อัปเดต test ในไฟล์นี้ (ดูเคสเดิมบรรทัด ~104-130, ~240-302) ให้: (1) เพิ่ม assertion ในเทสต์ client drill-down ว่าแถว domains[] มี Clients เท่ากับจำนวน client ทั้งระบบที่ถามโดเมนนั้นในช่วงเวลานั้น และ IPCount/SharedIPs ตรงกับ forward index ที่ seed ไว้ (ใช้ fixture เดิมของไฟล์ ห้ามสร้าง fixture ใหม่ถ้าเลี่ยงได้) (2) แก้เทสต์เคส client=unknown (บรรทัด ~298-302) ให้ยืนยันว่า Bytes ยังเป็น 0 เหมือนเดิม แต่ Clients/IPCount ต้องไม่ใช่ 0 แล้วเมื่อโดเมนนั้นมีข้อมูลจริง (3) เทสต์เดิมที่ยืนยันว่า overview/domain drill-down ทำงานถูกต้องห้ามแก้ค่าคาดหวัง ต้องยังผ่านเหมือนเดิม",
  "acceptance": [
    "go test ./internal/... -race ผ่านทั้งหมด",
    "มี assertion ใหม่ที่จะ fail ถ้าย้อนโค้ด T-04 กลับ (ทดสอบด้วยการอ่าน ไม่ต้อง revert จริง)"
  ],
  "depends_on": ["T-04"]
}
```

### T-06 — [optional / รอเจ้าของสั่ง] เติม Domains ในแถว client ของ domain drill-down

```json
{
  "task_id": "T-06",
  "title": "(optional) แก้บั๊กแฝดชนิดเดียวกัน: คอลัมน์ Domains = 0 ในตาราง Source Hosts ของหน้า domain",
  "layer": "service",
  "files": ["backend/internal/service/statistics_dns.go", "backend/internal/model/statistics.go", "docs/openapi.yaml", "frontend/public/openapi.yaml", "frontend/src/services/dnsStatisticsService.ts"],
  "instruction": "ทำงานนี้ต่อเมื่อเจ้าของโปรเจกต์สั่งเท่านั้น (ดู D-1 ข้อ 3) — แก้ GetDNSDomainClients (statistics_dns.go ~313-327) ที่จงใจปล่อย clients[i].Domains = 0 ให้เติมจำนวนโดเมนที่แต่ละ client ถามในช่วงเวลานี้ (ทั้งระบบ) โดย derive จากลูป ring เดิม (~223-231) แบบเดียวกับ T-04 ห้ามเพิ่มแหล่งข้อมูลใหม่ ห้ามยืด critical section แล้วอัปเดตคอมเมนต์สัญญาใน model/statistics.go, openapi ทั้ง 2 ไฟล์ และ TS type ให้ตรงกัน ถ้าไม่ทำงานนี้ ทางเลือกสำรองคือให้ frontend ซ่อนคอลัมน์ Domains ในหน้า domain drill-down ไปเลย (อย่าปล่อยให้แสดง 0 ค้างไว้)",
  "acceptance": [
    "ถ้าทำ: go build/vet/test ผ่าน และคอลัมน์ Domains บนหน้า domain drill-down แสดงค่าที่ไม่ใช่ 0 เมื่อมีข้อมูล",
    "ถ้าไม่ทำ: ต้องมีบันทึกในเอกสารแผนนี้ว่าเจ้าของเลือกไม่ทำ"
  ],
  "depends_on": ["T-04"]
}
```

### T-07 — [frontend] อัปเดต mock mode + doc comment ของ TS types ให้ตรงสัญญาใหม่

```json
{
  "task_id": "T-07",
  "title": "mock mode ของ getClientDomains ต้องคืน clients/ipCount/sharedIps เหมือน backend จริง",
  "layer": "frontend",
  "files": ["frontend/src/services/dnsStatisticsService.ts"],
  "instruction": "แก้ getClientDomains ในโหมด IS_MOCK_MODE (บรรทัด ~521-583): ตอนนี้ hardcode clients: 0, ipCount: 0, sharedIps: false (บรรทัด ~552-554) ให้เปลี่ยนเป็นค่าที่คำนวณจาก mock data เดิมในไฟล์นี้ — clients = จำนวน client ที่ถามโดเมนนั้นใน mockPairs (นับทั้งระบบ ไม่ใช่เฉพาะ target), ipCount = mockDomainIpRows(domain, scale).length, sharedIps = แถวใดแถวหนึ่งมี shared=true — ใช้ helper ที่มีอยู่แล้ว (mockDomainIpRows, mockIpRefCount, mockPairs) ห้ามสร้าง mock dataset ชุดใหม่. พร้อมกันนี้แก้ doc comment ของ DNSClientDrilldown.domains (บรรทัด ~168-172) และ DNSDomainStat.clients/ipCount (บรรทัด ~48-58) ให้ระบุความหมายใหม่ (ทั้งระบบ ไม่ขึ้นกับ client ที่ drill-down) ห้ามให้ TS type drift จาก Go struct",
  "acceptance": [
    "รันด้วย mock mode แล้วหน้า /statistics/dns/client/192.168.1.101 แสดง Clients/IPs ไม่เป็น 0 สำหรับ www.youtube.com (mockPairs มี 3 clients, mockDomainIPs มี 3 IPs และมี shared 1 ตัว)",
    "yarn build + yarn lint ผ่าน",
    "doc comment ตรงกับ Go struct หลัง T-04"
  ],
  "depends_on": ["T-04"]
}
```

### T-08 — อัปเดต API contract (openapi 2 ไฟล์) + doc comment ของ Go model

```json
{
  "task_id": "T-08",
  "title": "แก้สัญญา 'always 0/false here' ใน openapi + model comment ให้ตรงพฤติกรรมใหม่",
  "layer": "api",
  "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml", "backend/internal/model/statistics.go"],
  "instruction": "หลัง T-04 สัญญาเดิมที่เขียนว่า clients/ipCount/sharedIps เป็น 0/false เสมอในบริบท client drill-down ไม่จริงอีกต่อไป ให้แก้ให้ตรงกันทั้ง 3 จุด: (1) docs/openapi.yaml บรรทัด ~5327-5336 (DNSClientDrilldown.domains) เขียนใหม่ว่า clients = จำนวน client ทั้งระบบที่ถามโดเมนนั้นในช่วงเวลานี้, ipCount/sharedIps = ข้อมูลของโดเมนใน forward index (ไม่ขึ้นกับ client) ส่วน bytes/bytesUp/bytesDown ยังคงเป็นเฉพาะไบต์ระหว่าง client นี้กับ IP ของโดเมนนั้นเหมือนเดิม (2) frontend/public/openapi.yaml ต้องแก้ให้ diff เท่ากับ docs/openapi.yaml เป๊ะ (เป็นสำเนา) (3) backend/internal/model/statistics.go บรรทัด ~406-412 (DNSClientDrilldown.Domains) แก้คอมเมนต์ให้ตรงกัน. ห้ามเปลี่ยนชื่อฟิลด์/รูปร่าง JSON ใด ๆ — งานนี้แก้เฉพาะคำอธิบาย",
  "acceptance": [
    "docs/openapi.yaml กับ frontend/public/openapi.yaml เหมือนกันทุก byte",
    "ไม่มีฟิลด์ใดถูกเพิ่ม/ลบ/เปลี่ยนชื่อใน schema (diff เป็น description ล้วน)",
    "ไม่มีข้อความ 'always 0/false' ในบริบท client drill-down หลงเหลือทั้งใน openapi และ Go comment"
  ],
  "depends_on": ["T-04"]
}
```

### T-09 — ตรวจข้อความประกอบ UI ที่อ้างถึงคอลัมน์ Up/Down ที่ถูกลบไปแล้ว

```json
{
  "task_id": "T-09",
  "title": "แก้ subtitle/tooltip ที่อ้างถึง 'คอลัมน์ Up/Down ในตารางด้านล่าง' ให้ตรงกับ UI ใหม่",
  "layer": "frontend",
  "files": ["frontend/src/pages/StatisticsDnsClient.tsx", "frontend/src/pages/StatisticsDnsDomain.tsx", "frontend/src/pages/StatisticsDns.tsx", "frontend/src/components/statistics/DnsStatsShared.tsx"],
  "instruction": "หลัง T-02 ข้อความบางจุดอ้างถึงคอลัมน์ที่ไม่มีแล้ว: StatisticsDnsClient.tsx บรรทัด ~159 subtitle ของ TrafficTrendCard เขียนว่า 'เหมือนคอลัมน์ Up/Down ในตารางด้านล่าง' ให้แก้เป็นสำนวนที่ตรงกับคอลัมน์ Traffic แบบ 2 บรรทัด (เช่น 'ตรงกับบรรทัด Down/Up ในคอลัมน์ Traffic ของตารางด้านล่าง') และไล่ตรวจ subtitle/tooltip/คอมเมนต์หัว component ในอีก 3 ไฟล์ว่ามีจุดไหนอ้างคอลัมน์ Down/Up/Total อีกหรือไม่ (grep คำว่า Down, Up, Total ในไฟล์กลุ่ม DNS เท่านั้น) แล้วแก้ให้สอดคล้อง พร้อมอัปเดตคอมเมนต์หัว DomainStatsTable/ClientStatsTable/DomainIpTable ที่บรรยายคอลัมน์เดิม. ห้ามแก้ข้อความในหน้า Traffic",
  "acceptance": [
    "ไม่มีข้อความ UI ในหน้า DNS ที่อ้างถึงคอลัมน์ Down/Up/Total ที่ถูกลบไปแล้ว",
    "yarn build + yarn lint ผ่าน"
  ],
  "depends_on": ["T-02"]
}
```

### T-10 — อัปเดตเอกสารแผนเดิมให้สอดคล้อง (docs only)

```json
{
  "task_id": "T-10",
  "title": "บันทึกการเปลี่ยนสัญญาไว้ในเอกสารแผนเดิมของ PR 127",
  "layer": "db",
  "files": ["docs/ref/complete/statistics-dns-page-revamp-plan.md", "docs/ref/todo/statistics-dns-review-fixes-plan.md"],
  "instruction": "เพิ่มหมายเหตุสั้น ๆ ท้ายเอกสาร docs/ref/complete/statistics-dns-page-revamp-plan.md ว่าข้อกำหนดเดิม 'client drill-down: clients/ipCount/sharedIps = 0 เสมอ' ถูกยกเลิกโดยแผน docs/ref/todo/statistics-dns-review-fixes-plan.md (รีวิว PR 127) และคอลัมน์ Down/Up ในตารางหน้า DNS ถูกยุบเป็นคอลัมน์ Traffic แบบ 2 บรรทัด พร้อมอัปเดตสถานะในแผนฉบับนี้ ห้ามอ้างอิงด้วย #N (ใช้ 'PR 127' / 'ข้อ N' ตามข้อตกลงใน CLAUDE.md)",
  "acceptance": [
    "เอกสารเดิมมีหมายเหตุชี้มาที่แผนนี้ และไม่มีการใช้ #N อ้างหัวข้อในเอกสาร"
  ],
  "depends_on": ["T-08"]
}
```

## 4. ข้อควรระวังสำหรับ ai-developer

1. **ห้ามแตะหน้า Statistics → Traffic** — `pages/StatisticsTraffic.tsx` / `pages/StatisticsTrafficHost.tsx`
   ใช้ `SortableHead` ตัวเดียวกัน แต่คอลัมน์ Down/Up/Total ของมันถูกต้องอยู่แล้วตามเจตนาของรีวิว
   (นั่นคือสถิติ traffic จริง) ห้ามแก้ signature ของ `SortableHead`/`useSortableRows` ให้กระทบสองหน้านั้น
2. **sort ของคอลัมน์ Traffic ใช้ `sortKey="bytes"` เท่านั้น** — ห้ามเขียน sort accessor ใหม่
   `bytes == bytesUp + bytesDown` เป็นสัญญาที่ backend การันตีไว้แล้ว (ทั้ง Go DTO และ TS type)
3. **Locking discipline ของ `statistics_dns.go` เป็นเรื่องซีเรียส** — ห้ามถือ `s.dns.mu` ขณะเรียก
   `domainIPs.*` หรือ `traffic.*` (คอมเมนต์หัวไฟล์บรรทัด 25-34) และห้ามเพิ่มจำนวนการเรียก
   `GetTrafficBreakdown*` / `hostLookup` ต่อ request
4. **`Snapshot()` ใช้แทน `StatsFor()` ไม่ได้** — มันยุบ IP ที่ shared เหลือโดเมนเดียว จะนับ IP ขาด
5. **เส้นทาง `client == "unknown"` ห้าม join conntrack** — เติมได้เฉพาะ 3 ฟิลด์จาก ring/forward index
6. **ข้อมูลนี้เป็น PII ของคนในบ้าน** — ห้าม log ชื่อโดเมน/IP เพิ่มเติมในโค้ดที่แก้ และห้ามนำ
   domain→IP index ไปใช้กับ firewall/policy/routing/QoS เด็ดขาด (คำเตือนใน `dns_domain_ips.go:24-28`)
7. **openapi 2 ไฟล์ต้องเท่ากันเป๊ะ** (`docs/openapi.yaml` ↔ `frontend/public/openapi.yaml`)
8. git: ทำงานต่อบน branch `feat/statistics-dns-page` (เป็น PR 127 อยู่แล้ว) **ห้าม commit เอง**
   เว้นแต่เจ้าของสั่ง

## 5. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ให้ ai-qa ทดสอบครั้งเดียวหลัง T-01…T-09 เสร็จครบ)

```json
{
  "final_acceptance": [
    "Build เขียวทั้งหมด: cd backend && go build ./... && go vet ./... && go test ./... -race ; cd frontend && yarn build && yarn lint ; และ bash build.sh สำเร็จ",
    "R-1: หน้า /statistics/dns (Top Domains + Top Source Hosts), /statistics/dns/domain/:domain (Resolved IPs + Source Hosts) และ /statistics/dns/client/:client ไม่มีคอลัมน์ Down หรือ Up แยกเหลืออยู่เลยแม้แต่ตารางเดียว",
    "R-2: ทุกตารางในหน้า DNS มีคอลัมน์ชื่อ Traffic (ไม่ใช่ Total) แสดง 2 บรรทัดในเซลล์เดียว บรรทัดบนเป็นค่า Down บรรทัดล่างเป็นค่า Up และค่าที่แสดงตรงกับ bytesDown/bytesUp ของ API response",
    "R-2: คลิก header Traffic แล้วเรียงตามผลรวม (bytes) ได้ทั้ง asc/desc — ตรวจโดยเทียบว่าแถวบนสุดมี bytesDown+bytesUp มากที่สุด (desc) และตัวชี้ทิศทางสลับถูกต้อง",
    "R-3: เปิด /statistics/dns/client/<ip ที่มีข้อมูล> แล้วคอลัมน์ Clients และ IPs ไม่เป็น 0 สำหรับโดเมนที่หน้า overview แสดงค่าไม่เป็น 0 และค่าที่ได้ตรงกันเป๊ะกับค่าของโดเมนเดียวกันบนหน้า overview",
    "R-3: ค่า Clients/IPs ที่หน้า client drill-down ต้องเท่ากับที่หน้า overview ทุกโดเมนที่ปรากฏทั้งสองหน้า (ทดสอบทั้ง mock mode และ backend -mock=true)",
    "R-3 edge case: /statistics/dns/client/unknown ยังเปิดได้ปกติ ไม่ error, การ์ด Volume และกราฟ trend ยังถูกซ่อนไว้เหมือนเดิม, แถวโดเมนแสดง Traffic เป็น 0 B / 0 B แต่ Clients/IPs แสดงค่าจริง",
    "โดเมนที่ยังไม่มี IP ใน forward index (เช่น netflix.com ใน mock) ต้องแสดง IPs = 0 ได้โดยไม่ถือเป็นบั๊ก และไอคอนเตือน shared IP ยังทำงานถูกต้องในคอลัมน์ IPs",
    "ไม่มี regression บนหน้า Statistics → Traffic: /statistics/traffic และ /statistics/traffic/host/:ip ยังมีคอลัมน์ Down / Up / Total แยกครบเหมือนเดิม (git diff ต้องไม่แตะ 2 ไฟล์นี้)",
    "สลับช่วงเวลาครบทั้ง 7 ปุ่ม (15m/30m/1h/3h/6h/12h/24h) บนทั้ง 3 หน้า ไม่มี error ใน console และ auto-refresh 10 วิยังทำงาน",
    "Dark mode + light mode: เซลล์ Traffic 2 บรรทัดอ่านออกทั้งสองโหมด ไม่มี hardcoded palette class / shadow-* / backdrop-blur-* ที่เพิ่มใหม่",
    "API contract: docs/openapi.yaml กับ frontend/public/openapi.yaml เหมือนกันทุก byte และไม่มี field ใดถูกเพิ่ม/ลบ/เปลี่ยนชื่อในสคีมา (diff เป็นคำอธิบายล้วน)",
    "Security/perf: git diff ของ backend ไม่มี exec.Command ใหม่, ไม่มี goroutine/ticker ใหม่, ไม่มีการ log ชื่อโดเมนหรือ IP เพิ่ม, และ GetDNSClientDomains ยังเรียก GetTrafficBreakdown*/hostLookup อย่างละไม่เกิน 1 ครั้งต่อ request (0 ครั้งสำหรับ client=unknown)"
  ]
}
```
