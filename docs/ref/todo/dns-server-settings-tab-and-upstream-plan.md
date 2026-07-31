# DNS Server — แท็บ Settings + แยก Upstream Resolver ออกจาก System DNS

> เอกสารแผนงานสำหรับ 2 คอมเมนต์ของเจ้าของโปรเจกต์ใน issue #106 ที่ตกลงว่าทำรวมรอบเดียว
> ต่อยอดบน branch/PR เดิม (`feat/statistics-dns-top-domain`, PR #109 — ไม่แยก branch ใหม่):
> 1. หน้า DNS Server จัดกลุ่มการตั้งค่าเป็น **แท็บ "Settings"** แยกจาก Zones/Records
> 2. DNS Server (dnsmasq ที่ให้บริการ client) มี **upstream resolver ของตัวเอง** ตั้งค่าได้ในแท็บ Settings
>    และ **ตัด path ที่ System DNS เขียน/สั่ง restart dnsmasq ออกให้หมด**
>
> วันที่เขียน: 2026-07-31 (แก้ไขรอบที่ 2 วันเดียวกัน: ตัด System DNS → dnsmasq write path ทิ้งทั้งหมด
> และยืนยันขอบเขต 3 ข้อ) · Branch อ้างอิง: `feat/statistics-dns-top-domain`
> README Feature Status: "DNS Server (local zones)" ยังเป็น Completed เหมือนเดิม (ขยายการตั้งค่าในหน้าเดิม)

## ✅ สถานะ: implement เสร็จแล้ว รอ commit/PR (อัปเดต 2026-07-31)

- **T-01 – T-13 เสร็จครบทุกข้อ** (tech-lead ตรวจเทียบโค้ดจริงทีละ task แล้ว — ดู §3 และ §6)
- **T-14 (design doc) — SKIP ตามที่เจ้าของโปรเจกต์อนุมัติ** เพราะเป็น optional
  (เนื้อหาเชิง design ยังอยู่ครบในไฟล์แผนนี้ ถ้าอยากย้ายเข้า `docs/ref/complete/*` ทำภายหลังได้)
- QA: ตรวจ 2 รอบ (รอบแรกพบช่องว่างด้าน test coverage ของ T-08 ไม่ใช่บั๊ก functional, รอบสองผ่านครบ)
  build / test / vet / lint ผ่านทั้ง backend และ frontend
- **ยังไม่ commit** — โค้ดอยู่ใน working tree ของ branch `feat/statistics-dns-top-domain`
- **เหลือเฉพาะรายการ real-device ใน Final Acceptance** ที่ต้องทำตอน deploy ลงบอร์ดจริง
  (ทำบนเครื่อง dev ไม่ได้) — **ไม่บล็อกการ commit/อัปเดต PR #109** ดูรายการ 🧪 ใน §6

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
- หน้า `/dns-server` แบ่งเป็น 2 แท็บ: **"Zones & Records"** (ค่าเริ่มต้น) และ **"Settings"**
  โดยแท็บ Settings รวม Listen Interfaces + Upstream Resolvers (ใหม่) + DNS Statistics ไว้ที่เดียว
  ปุ่ม **Apply DNS Zones / Clear Cache** ย้ายขึ้นแถบหัวหน้าจอ (เหนือแท็บ) ให้กดได้จากทั้งสองแท็บ
- `DNSServerSettings` มีฟิลด์ใหม่ 2 ตัว: `upstreamMode` (`"system"` | `"custom"`) และ `upstreamServers[]`
  (**สูงสุด 4 รายการ, รับเฉพาะ IP เปล่า** — ยืนยันแล้วกับเจ้าของโปรเจกต์)
  - `system` (ค่าเริ่มต้น) — ตอน **generate config** จะ *อ่าน* ค่าจาก System DNS มาใช้เป็น `server=`
    (static = primary/secondary, wan = DNS ที่ WAN link ได้จาก DHCP) เหมือนพฤติกรรมเดิม
  - `custom` — dnsmasq forward ไป **เฉพาะ** IP ที่ผู้ใช้กรอกในหน้า DNS Server เท่านั้น
- **ตัด path "System DNS เขียนค่าลง dnsmasq" ออกทั้งหมด** — การบันทึกหน้า System DNS (`PUT /api/system/dns`)
  จะ **ไม่** เขียน `pigate-dns.conf` และ **ไม่** restart dnsmasq อีกต่อไปในทุกโหมด
  (System DNS = ค่าของตัวเครื่อง/systemd-resolved เท่านั้น) — เหลือความสัมพันธ์เดียวคือ **การอ่านค่า
  ตอนที่ DNS Server generate config ของตัวเองในโหมด `system`** (ผลข้างเคียง: §5 ข้อ 2)
- 🔒 ปิดช่องโหว่ config-file injection ของ System DNS ที่เจอตอนสำรวจ (T-09)
- System DNS (หน้า `/dns`, systemd-resolved) ยังทำหน้าที่เดิมทุกประการสำหรับตัวเครื่องเอง
- ทำงานได้ทั้ง `-mock=true` และของจริง, DB เก่าอัปเกรดเองได้, Backup/Restore ครอบคลุมค่าใหม่

**นอกขอบเขต (ตัดชัดเจน)**
- **ไม่รองรับ `IP#port` / ชื่อโฮสต์ / DoT / DoH** — รับเฉพาะ IPv4/IPv6 ล้วน
  `server=/zone/target` แบบ per-zone ยังเป็นของฟีเจอร์ Forward Zone เดิม
- **ไม่ทำ health check / failover / latency probe ของ upstream** — dnsmasq จัดการเอง
- **ไม่แตะการทำงานของ System DNS ที่มีต่อ systemd-resolved** นอกจากการ validate input (T-09)
- **ไม่แตะ DHCP option 6** (`dhcp-option=<iface>,6,<dns>`) — ค่านั้นมาจาก `dhcp_configs.dns1/dns2`
- **ไม่ย้าย/เปลี่ยนโครง Zones & Records** — แค่ย้ายทั้งก้อนไปอยู่ใต้ `<TabsContent>`
- ไม่เพิ่ม endpoint ใหม่ ไม่เพิ่มตาราง DB ใหม่ ไม่เพิ่ม dependency ใหม่ ไม่เพิ่ม kernel capability ใหม่

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-07-31 — ก่อนลงมือ)

| ส่วน | สถานะ | ที่อยู่ |
|---|---|---|
| หน้า DNS Server | **ไม่มีแท็บเลย** — เรียงการ์ดลงมา: Listen Interfaces (มีปุ่ม Clear Cache/Apply ใน CardHeader) → DNS Statistics → grid Zones/Records → Dialogs | `frontend/src/pages/DnsServer.tsx:499-603`, `:605-695`, `:697-…` |
| `Tabs` ของ shadcn | มีแล้ว ใช้อยู่ 4 หน้า (Dashboard/Statistics/Policy/SettingsMaintenance) | `components/ui/tabs.tsx`, `Dashboard.tsx:1009-1066` |
| `RadioGroup` | **ไม่มีใน `components/ui/`** → ใช้ `Select` (มีแล้ว) สำหรับเลือกโหมด | `components/ui/select.tsx` |
| upstream ของ DNS Server | **ผูกกับ System DNS 100%** — `resolveUpstreams()` เรียก `DNSService.GetDNSConfig()` แล้วแปลงเป็น `server=` | `service/dns_server.go:49, :61-91` |
| การเขียน `server=` ลง config | `buildDNSConfig()` เขียน `no-resolv` + `server=<ip>` ต่อรายการ — **ไม่ validate IP** (แค่ TrimSpace) | `kernel/dns_server.go:142-153` |
| **จุดที่ System DNS สั่งเขียน dnsmasq (ต้องตัด)** | `HandleUpdateDNSConfig` เรียก `dnsServerService.ApplyAll()` ทุกครั้งหลังบันทึก System DNS = เขียน `pigate-dns.conf` + **restart dnsmasq (DNS+DHCP สะดุด)** | `api/handlers.go:2311-2317` |
| จุดอื่นที่อาจสงสัย — ตรวจแล้ว **ไม่มี** | self-heal bus subscriber `"dns"` เรียกแค่ `dnsService.ApplyDNSConfig()` (resolved เท่านั้น); startup/restore เรียก `dnsServerService.InitApplyConfig()` แยกต่างหากอยู่แล้ว | `cmd/pigate/main.go:283-296, :524-529`, `service/backup.go:513-514` |
| `SyncDNSFromOS()` | **dead path — ไม่ถูกเรียกจาก production เลย** (มีแต่ใน `repository_test.go:598`) | `db/repository.go:2456-2515` |
| 🔒 validate System DNS input | **ไม่มีเลยฝั่ง backend** → `primaryDns` ที่มี `\n` ไหลไปโผล่ใน `pigate-dns.conf` และ `resolved.conf.d/pigate.conf` ได้ | `api/handlers.go:2295-2310`, `db/repository.go:2173-2180`, `kernel/dns.go:178-186` |
| `sanitizeUpstreams` | ตัดค่าว่าง/ซ้ำ/loopback แต่ **ค่าที่ parse ไม่ผ่านจะถูกเก็บไว้** | `service/dns_server.go:96-111` |
| ตาราง `dns_server_settings` | คอลัมน์: `interfaces`, `query_logging`, `dns_cache_ttl_minutes`, `dns_cache_max_entries` | `db/connection.go:359-362`, migration ต่อคอลัมน์ `:510-533` |
| handler PUT settings | validate → เขียน DB → แยกว่าอะไร restart (interfaces/queryLogging → `ApplyAll()`, TTL/cap → `SetReverseCacheLimits`) | `api/handlers.go:3195-3266` |
| kernel interface | `ApplyZones(zones, interfaces, upstreamServers []string, queryLog bool)` — **รับ upstream เป็นพารามิเตอร์อยู่แล้ว** | `kernel/interfaces.go`, `kernel/mock.go:395-399` |

**สรุป:** kernel ไม่ต้องแก้ signature เลย และ **จุดที่ System DNS เขียน dnsmasq มีจุดเดียวจริง ๆ คือ
`api/handlers.go:2315`**

## 2. แนวทางเทคนิค

```
PUT /api/system/dns (System DNS) ──► systemd-resolved เท่านั้น   ✂️ ตัดสาย ApplyAll() ทิ้ง (T-06)
                                     (ไม่เขียน pigate-dns.conf / ไม่ restart dnsmasq อีกต่อไป)

DNSServerSettings.upstreamMode        ← ใช้ตอน DNS Server generate config ของตัวเองเท่านั้น
      ├── "system"  (default)  ─► resolveUpstreams(): *อ่าน* DNSService.GetDNSConfig()
      └── "custom"             ─► resolveUpstreams(): settings.UpstreamServers (≤ 4 IP)
                                              │
                                    sanitizeUpstreams() (ตัดว่าง/ซ้ำ/loopback/parse ไม่ผ่าน)
                                              ▼
                       kernel.ApplyZones(zones, ifaces, upstreams, queryLog)  ← signature เดิม ไม่แก้
                                              ▼
                          buildDNSConfig: no-resolv + server=<ip> (validate ซ้ำอีกชั้น 🔒)
```

**เหตุผลที่เลือกโหมด `system`/`custom` แทนการแยกขาด**
1. **backward compatible 100%** — เครื่องที่ติดตั้งไปแล้วได้ `upstream_mode='system'` จาก DEFAULT ของ
   migration → ไฟล์ config ที่ generate ออกมาเหมือนเดิมทุกไบต์ ไม่มีใครเน็ตหลุดตอนอัปเกรด
2. **โหมด `wan` ของ System DNS ยังมีประโยชน์** — ISP เปลี่ยน DNS ทาง DHCP แล้วได้ค่าใหม่เมื่อ generate รอบถัดไป
3. ผู้ใช้ที่ต้องการ "แยกจริง" (ตัว Pi ใช้ ISP DNS แต่เครื่องลูกใช้ 1.1.1.1) เลือก `custom` ได้ทันที

**เหตุผลที่ตัด `ApplyAll()` ออกจาก path ของ System DNS ทั้งหมด (ไม่ใช่แค่ทำเป็นเงื่อนไข)**
- เจ้าของโปรเจกต์สั่งชัดเจนว่า System DNS **ต้องไม่มีทางเขียนอิทธิพลลง dnsmasq config เอง**
- ผลข้างเคียงเชิงบวก: เดิมการกดบันทึกหน้า System DNS = **restart dnsmasq ทุกครั้ง** ทำให้ **DHCP ของทั้งบ้าน
  สะดุดด้วย** (process เดียวกัน) ทั้งที่เนื้อไฟล์ config บ่อยครั้งไม่เปลี่ยนเลย
- ราคาที่ต้องยอมรับ: โหมด `system` ค่าใหม่ของ System DNS มีผลกับ dnsmasq **เมื่อ config ถูก generate
  รอบถัดไป** (กด "Apply DNS Zones", แก้ค่าอื่นในหน้า DNS Server, boot/restore) → ต้องบอกใน UI (T-12/T-13)

**ทางเลือกที่ตัดทิ้ง**
- **ทำ `ApplyAll()` แบบมีเงื่อนไข (เรียกเฉพาะตอน mode == system)** — เจ้าของสั่งให้ตัดทั้งหมด
- **แยกขาดเสมอ (copy ค่า System DNS ลงคอลัมน์ใหม่ตอน migrate)** — เสียพฤติกรรม `wan` อัตโนมัติ
- **เก็บ upstream เป็นตารางใหม่ `dns_upstreams`** — เกินจำเป็น
- **แยกหน้าใหม่ "DNS Server Settings"** — เจ้าของสั่งให้เป็นแท็บในหน้าเดิม

## 3. ขั้นตอนการทำ (เรียงตาม dependency) — ทำครบแล้วทุกข้อ

### ✅ T-01 — model: ฟิลด์ใหม่ + ค่าคงที่ 🔒
**ไฟล์:** `backend/internal/model/dns_server.go`, `backend/internal/model/dns_validate.go`
- `DNSServerSettings` เพิ่ม `UpstreamMode string` / `UpstreamServers []string`
- const กลาง: `DNSUpstreamModeSystem`, `DNSUpstreamModeCustom`, `DNSUpstreamMaxServers = 4`
- ขยาย `ValidateDNSServerSettings` 🔒: mode ว่าง = `system`; mode อื่น → error; `custom` ต้องมี 1–4 รายการ;
  ตรวจค่าดิบด้วย `net.ParseIP` (ไม่ TrimSpace ก่อน); ปฏิเสธ loopback และค่าซ้ำ;
  mode `system` เก็บ `UpstreamServers` ได้แต่ไม่ใช้

### ✅ T-02 — DB: 2 คอลัมน์ใหม่ + repo
**ไฟล์:** `backend/internal/db/connection.go` (`:533-546`), `backend/internal/db/repository.go` (`:2689-2760`)
- migration ต่อคอลัมน์ (เช็ก `sqlite_master`): `upstream_mode TEXT NOT NULL DEFAULT 'system'`,
  `upstream_servers TEXT NOT NULL DEFAULT ''` — ใช้เส้นทางเดียวกับคอลัมน์ DNS Statistics ของ PR #109
  (DB ใหม่ก็ได้คอลัมน์จาก migration block เดียวกัน จึงไม่ต้องแก้ `CREATE TABLE` — ตรงกับ pattern เดิม)
- `GetDNSServerSettings()` อ่าน 2 คอลัมน์ + clamp: mode ไม่รู้จัก → `system` + warning,
  IP ที่ parse ไม่ผ่าน → ตัดทิ้ง + warning; `SetDNSServerSettings()` รับ 2 พารามิเตอร์เพิ่ม

### ✅ T-03 — service: เลือกแหล่ง upstream
**ไฟล์:** `backend/internal/service/dns_server.go` (`:49`, `:61-128`)
- `resolveUpstreams(settings)` — `custom` → ใช้ `settings.UpstreamServers` (ไม่แตะ `DNSService` เลย),
  `system` → ตรรกะเดิมทั้งดุ้น; 🔒 `sanitizeUpstreams` ทิ้งค่าที่ `net.ParseIP` คืน nil แล้ว

### ✅ T-04 — kernel: validate `server=` ซ้ำอีกชั้น 🔒
**ไฟล์:** `backend/internal/kernel/dns_server.go` (`:136-174`)
- กรอง IP ก่อน แล้วค่อยเช็ก `len()` → **ไม่มีทางได้ `no-resolv` เปล่า**;
  ข้อความคอมเมนต์ในไฟล์ config คงเดิมเพื่อรักษาการันตี byte-identical ของโหมด `system`

### ✅ T-05 — API handler ของ DNS Server settings 🔒
**ไฟล์:** `backend/internal/api/handlers.go` (`:3271-3274`)
- `upstreamChanged` ถูกรวมเข้าเงื่อนไข restart เดียวกับ interfaces/queryLogging; TTL/cap ยังไม่ restart

### ✅ T-06 — ตัด path "System DNS เขียน/restart dnsmasq" ออกให้หมด
**ไฟล์:** `backend/internal/api/handlers.go` (`:2319-2329`)
- ลบ `dnsServerService.ApplyAll()` ออกจาก `HandleUpdateDNSConfig` เหลือคอมเมนต์ห้ามเติมกลับ
- ตรวจแล้ว: จุดเรียก `ApplyAll`/`InitApplyConfig` เหลือ 4 จุดที่เป็นของ DNS Server เองเท่านั้น
  (`handlers.go:3155` = POST /api/dns/apply, `handlers.go:3274` = PUT settings,
  `service/dns_server.go:133` = boot, `service/backup.go:514` = restore)

### ✅ T-07 — Backup/Restore
**ไฟล์:** `backend/internal/db/backup_repo.go` (`:272-292`)
- restore เขียน 2 คอลัมน์ใหม่ + ไฟล์รุ่นเก่า → `system` + ว่าง; กรอง IP จากไฟล์ backup ด้วย `net.ParseIP`

### ✅ T-08 — เทสต์ backend
- `service/dns_server_test.go` (ใหม่): `TestResolveUpstreams_SystemMode_Static`, `_CustomMode`,
  `_DoesNotTouchApplyCount`, `TestApplyAll_UsesResolvedUpstreams`
- `db/dns_server_settings_migration_test.go`: `TestMigrationAddsUpstreamColumnsToLegacyDNSServerSettings`,
  `TestGetDNSServerSettings_ClampsUnknownUpstreamMode`
- `kernel/dns_server_test.go`: `TestBuildDNSConfig_UpstreamValidation` (injection + `no-resolv` เปล่า),
  `TestBuildDNSConfig_QueryLogByteIdentical`
- `api/handlers_test.go`: upstream เปลี่ยน → ApplyCount +1; TTL/cap-only → ไม่ขยับ; ค่าเพี้ยน → 400 + ไม่ apply;
  **`TestHandleUpdateDNSConfig_NeverCallsApplyZones` (T-06 regression guard, ทดสอบทั้ง 2 โหมด)**
- `model/dns_validate_test.go`: `TestValidateDNSConfigInput` + เคสของ upstream; `service/backup_test.go` ครอบคลุม restore

### ✅ T-09 — validate System DNS input 🔒
**ไฟล์:** `backend/internal/model/dns_validate.go` (`:309-…` `ValidateDNSConfigInput`),
`backend/internal/api/handlers.go` (`:2306-2312` เรียกหลังเติม default `localDomain`)

### ✅ T-10 — API contract (2 ไฟล์ sync แล้ว)
**ไฟล์:** `docs/openapi.yaml` (`:2595-2597`, `:4886-4913`) และ `frontend/public/openapi.yaml`
- `upstreamMode` (`enum`, `default: system`), `upstreamServers` (`maxItems: 4`) + หมายเหตุว่า
  System DNS ไม่กระทบ dnsmasq แล้ว (จำนวน occurrence ตรงกัน 7:7)

### ✅ T-11 — Frontend: type + service
**ไฟล์:** `frontend/src/data-mockup/mockData.ts`, `frontend/src/services/dnsServerService.ts`

### ✅ T-12 — Frontend: แท็บ + การ์ด Upstream
**ไฟล์:** `frontend/src/pages/DnsServer.tsx`
- แถบหัวหน้าจอ (Clear Cache / Apply DNS Zones) `:610-640` → `<Tabs defaultValue="zones">` `:654`
  (แท็บ "Zones & Records" / "Settings"), **Dialog/Drawer ทั้งหมดอยู่นอก `<Tabs>` `:1195-…`**
- การ์ด Upstream Resolvers: `Select` โหมด, ช่อง IP สูงสุด 4, validate ฝั่ง client + แสดง 400 จาก backend,
  ข้อความเตือน "กด Apply DNS Zones เพื่อให้ค่าใหม่มีผล" `:1033`
- ตรวจแล้วไม่มี `shadow-*`/`backdrop-blur-*`/สี palette ดิบในไฟล์นี้

### ✅ T-13 — Frontend: หมายเหตุในหน้า System DNS
**ไฟล์:** `frontend/src/pages/DNS.tsx` (`:133-134`)

### ⏭️ T-14 (optional) — เอกสาร design doc — **SKIP ตามที่เจ้าของโปรเจกต์อนุมัติ**
เนื้อหา design ยังอยู่ในไฟล์แผนนี้ครบ; ถ้าจะทำภายหลังคือย้ายสรุปเข้า
`docs/ref/complete/dns-system-design.md` และ `dnsmasq-design.md`

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | หมายเหตุ |
|---|---|---|---|
| GET | `/api/dns/settings` | `authRoute` | เส้นเดิม เพิ่ม `upstreamMode`, `upstreamServers` |
| PUT | `/api/dns/settings` | `authRoute` (mutation → `DisableEditMiddleware`/read-only role) | validate IP/โหมด → 400; เปลี่ยน upstream/interfaces/queryLogging = เขียน config + restart dnsmasq; TTL/cap ไม่ restart |
| POST | `/api/dns/apply` | `authRoute` | เส้นเดิม — **จุดเดียวที่ผู้ใช้สั่ง regenerate `pigate-dns.conf` ด้วยมือ** |
| PUT | `/api/system/dns` (System DNS) | `authRoute` | **ไม่แตะ dnsmasq อีกต่อไป** (ตัด `ApplyAll()` ทิ้ง) + validate input (T-09) → 400 |

## 5. ข้อควรระวัง

1. 🔒 **ค่า upstream ถูก interpolate ลงไฟล์ config ตรง ๆ** (`server=%s`) → กัน 3 ชั้น: handler validate (400),
   repo กรองตอนอ่าน, `buildDNSConfig` กรองตอนเขียน (T-01/T-02/T-04) เพราะ config import/restore
   เขียน DB ได้โดยไม่ผ่าน handler
2. **ผลข้างเคียงของการตัด trigger (ต้องสื่อสารให้ผู้ใช้รู้)** — โหมด `system`: แก้ System DNS แล้ว
   `pigate-dns.conf` ยังเป็นค่าเก่าจนกว่าจะ generate รอบถัดไป → มีข้อความบอกทั้งหน้า DNS Server และหน้า System DNS
3. **ห้ามใครเติม trigger กลับ** — `TestHandleUpdateDNSConfig_NeverCallsApplyZones` ล็อกไว้แล้ว
4. **`no-resolv` เปล่า = DNS ตายทั้งบ้าน** — กรองก่อนแล้วค่อยเช็ก `len()` (T-04)
5. **loopback = forwarding loop** — โหมด custom ตอบ 400 พร้อมเหตุผล ไม่ใช่ตัดทิ้งเงียบ
6. **การเปลี่ยน upstream restart dnsmasq = DHCP สะดุดด้วย** — TTL/cap ต้องไม่ trigger `ApplyAll` (regression PR #109)
7. **DB เก่าอัปเกรดเองได้** — ALTER ต่อคอลัมน์เท่านั้น, DEFAULT `'system'` เพื่อให้ config เหมือนเดิมทุกไบต์
8. **ไฟล์ backup รุ่นเก่าไม่มีฟิลด์** → ต้องได้ `system` ไม่ใช่ `""`
9. **สลับโหมดกลับไป `system` ต้องไม่ลบรายการที่ผู้ใช้เคยกรอก**
10. **Dialog/Drawer ต้องอยู่นอก `<Tabs>`** — Radix unmount `TabsContent` ที่ไม่ active
11. **ปุ่ม Apply DNS Zones อยู่นอกแท็บ** — สำคัญขึ้นมากหลังตัด trigger (ข้อ 2)
12. **โหมด `wan` + `system`** — ค่า upstream ขยับตาม DHCP ของ ISP **ตอน generate เท่านั้น** ห้าม cache ลง DB
13. **`SyncDNSFromOS()` เป็น dead path** — ห้ามฟื้นมาใช้ (เขียน `system_dns_settings` โดยไม่ผ่าน validator ใหม่)
14. **mock mode**: ไม่มี kernel capability ใหม่ → `MockDNSServerManager` ไม่ต้องแก้

## 6. Checklist สรุป (Definition of Done)

**Backend**
- [x] T-01 `model/dns_server.go` + `model/dns_validate.go` (2 ฟิลด์ + const + validator) 🔒
- [x] T-02 `db/connection.go` migration 2 คอลัมน์ + `db/repository.go` (อ่าน/เขียน + clamp)
- [x] T-03 `service/dns_server.go` (`resolveUpstreams(settings)`, `sanitizeUpstreams` ทิ้ง IP เพี้ยน)
- [x] T-04 `kernel/dns_server.go` validate `server=` + กัน `no-resolv` เปล่า 🔒
- [x] T-05 `api/handlers.go` PUT `/api/dns/settings` (validate + เงื่อนไข restart) 🔒
- [x] T-06 `api/handlers.go` **ลบ `ApplyAll()` ออกจาก path ของ System DNS** + ยืนยันเหลือ 4 จุดเรียกที่ถูกต้อง
- [x] T-07 `db/backup_repo.go` + `service/backup.go`
- [x] T-08 เทสต์ครบทุกกลุ่ม (รวม regression guard ของ T-06) + `go test -race ./...` ผ่าน
- [x] T-09 validate System DNS input 🔒

**Frontend / Docs**
- [x] T-10 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync)
- [x] T-11 `data-mockup/mockData.ts` + `services/dnsServerService.ts`
- [x] T-12 `pages/DnsServer.tsx` (Tabs + ย้ายปุ่ม Apply/Clear Cache + การ์ด Upstream + ข้อความเตือน)
- [x] T-13 `pages/DNS.tsx` หมายเหตุว่า System DNS มีผลกับตัวเครื่องเท่านั้น
- [ ] ⏭️ T-14 (optional) design doc — **SKIP ตามที่เจ้าของโปรเจกต์อนุมัติ**

**Final Acceptance** — ✅ = ตรวจแล้วบนเครื่อง dev / mock / เทสต์อัตโนมัติ ·
🧪 = **ต้องทำบนบอร์ดจริงตอน deploy (ไม่บล็อกการ commit/PR)**
- [x] `cd backend && go build ./... && go test -race ./...` ผ่าน; `cd frontend && yarn build && yarn lint` ผ่าน
- [x] `-mock=true -allow-dev-cors`: หน้า DNS Server มี 2 แท็บ ("Zones & Records" เป็นค่าเริ่มต้น),
      สลับแท็บแล้ว state ไม่หาย, dialog zone/record ไม่พัง, ปุ่ม Apply/Clear Cache กดได้จากทั้งสองแท็บ
- [x] `-mock=true`: บันทึก upstream custom แล้วรีเฟรช → ค่ายังอยู่; สลับกลับ system → รายการเดิมไม่ถูกล้าง;
      ครบ 4 รายการแล้วปุ่มเพิ่ม disable
- [x] migration: DB ที่ยังไม่มี 2 คอลัมน์ → เปิดได้ ได้ `system` + ว่าง (เทสต์อัตโนมัติครอบคลุม)
- [x] `buildDNSConfig` โหมด `system` ให้ผลลัพธ์ byte-identical กับก่อนแผนนี้ (เทสต์ล็อกไว้)
- [x] 🔒 ค่า IP เพี้ยน/`#port`/hostname/loopback/ซ้ำ/เกิน 4 → 400 และ DB ไม่เปลี่ยน (เทสต์ handler + model)
- [x] 🔒 `PUT /api/system/dns` ด้วยค่าที่มี `\n` → 400 (เทสต์ T-09)
- [x] เปลี่ยนเฉพาะ TTL/จำนวน mapping → ไม่มีการเรียก `ApplyZones` (regression PR #109, เทสต์ล็อกไว้)
- [x] `PUT /api/system/dns` ไม่ทำให้ `ApplyCount` ขยับในทั้ง 2 โหมด (regression guard ของ T-06)
- [ ] 🧪 real device: แก้ค่าในหน้า **System DNS** แล้วกดบันทึก → `resolvectl status` ของตัวเครื่องเปลี่ยน
      แต่ **`pigate-dns.conf` ไม่ถูกเขียนใหม่ และ dnsmasq ไม่ถูก restart**
      (`systemctl show dnsmasq -p ExecMainStartTimestamp` ไม่เปลี่ยน, `stat -c %y` ของไฟล์ config ไม่เปลี่ยน,
      ping/DHCP ของเครื่องลูกไม่สะดุด) — ทดสอบทั้งโหมด `system` และ `custom`
- [ ] 🧪 real device: โหมด `system` + แก้ System DNS แล้วกด **"Apply DNS Zones"** → `pigate-dns.conf`
      อัปเดตเป็นค่าใหม่ (ยืนยันว่า path การ "อ่าน" ยังทำงาน)
- [ ] 🧪 real device: สลับเป็น custom = `1.1.1.1`,`8.8.8.8` → `pigate-dns.conf` มี `no-resolv` + 2 บรรทัด
      `server=`, `dnsmasq --test` ไม่ error, `dig @<pigate-ip> example.com` ตอบถูก
- [ ] 🧪 real device: ตัว Pi กับเครื่องลูกใช้ upstream ต่างกันจริง
- [ ] 🧪 real device: อัปเกรดจาก DB เดิมของเครื่องที่ใช้งานอยู่ → `pigate-dns.conf` เหมือนก่อนอัปเกรดทุกไบต์
- [ ] 🧪 Backup ตอนโหมด custom → Restore แล้วค่ากลับมาถูกและมีผลจริง; restore ไฟล์รุ่นเก่า → `system` + ว่าง
- [ ] 🧪 `-disable-edit=true` / role read-only: ดูค่า upstream ได้ แต่บันทึกไม่ได้ (403); logout → 401
- [x] dark/light mode ของแท็บ + การ์ดใหม่ + ข้อความในหน้า System DNS ดูปกติ,
      ไม่มี `shadow-*`/`backdrop-blur-*`/สี palette ดิบ
- [x] ไม่มีตารางใหม่ใน `db/` (มีแค่ 2 คอลัมน์ใน `dns_server_settings`), ไม่มี `exec.Command` ใหม่,
      ไม่มีการแก้ signature ของ `DNSServerManager`
- [ ] ทุกอย่างอยู่บน branch `feat/statistics-dns-top-domain` และเข้า main ผ่าน PR #109 เท่านั้น (รอ commit/push)
