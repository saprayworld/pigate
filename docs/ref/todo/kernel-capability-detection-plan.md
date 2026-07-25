# Kernel Capability Detection — ตรวจจับ subsystem ที่ใช้งานจริงไม่ได้ แล้วเตือนบน UI

> เอกสารแผนงานสำหรับฟีเจอร์: ตอนนี้รัน PiGate โหมด real บนเครื่องที่ไม่ใช่ Debian/Pi
> จริง (เช่น WSL) ฟีเจอร์ Firewall เรียก nftables ผ่าน Netlink แล้วล้มเหลว/ไม่มีผล
> แต่ **UI ไม่บอกอะไรเลย** ผู้ใช้เข้าใจว่ากฎถูก apply แล้ว
> งานนี้เพิ่มชั้น "capability probe" ในชั้น kernel + endpoint ใหม่ + banner บน UI
> โดยออกแบบเป็น registry ให้เพิ่ม subsystem อื่น (QoS/dnsmasq/resolved/Wi-Fi) ได้ทีหลัง
>
> วันที่เขียน: 2026-07-25 · Branch อ้างอิง: `main` (งานจริงทำบน `feat/kernel-capability-detection`)
> README Feature Status: ไม่เปลี่ยนสถานะฟีเจอร์เดิม (นี่คือ observability layer ใหม่)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็น):** เปิดหน้า Firewall Policy / Port Forwarding /
Forward Traffic บนเครื่องที่ nftables ใช้ไม่ได้ → เห็น banner สีเตือนด้านบนหน้าว่า
"ระบบ Firewall (nftables) ใช้งานไม่ได้บนเครื่องนี้ — <เหตุผล>" และหน้า Dashboard
เห็นสรุปรวมว่ามี subsystem ไหนใช้ไม่ได้บ้าง โดย **ยังใช้หน้าเว็บส่วนอื่นได้ปกติ**
(ไม่ block การแก้ config — DB ยังเป็น source of truth และเมื่อย้ายไปรันบน Pi จริง
ค่าจะถูก apply ให้เอง)

**เงื่อนไขทางเทคนิค:**
- การ probe ทั้งหมดอยู่ในชั้น `kernel/` เท่านั้น ผ่าน interface ใหม่ + มีทั้ง real และ mock
- **ห้าม `exec.Command`** — probe ด้วย netlink (nftables list) และ D-Bus (systemd GetUnit) เท่านั้น
- probe ต้อง **read-only 100%** ห้ามสร้าง/ลบ table, chain, qdisc หรือ bind NFLOG group
- mock mode รายงาน available เสมอ (ไม่มี banner กวนตอน dev)

**นอกขอบเขต (จงใจตัดออก):**
- ไม่ disable/ซ่อนปุ่มหรือฟอร์มเมื่อ capability ใช้ไม่ได้ — แค่ "เตือน" (ตัดสินใจข้อ 5 ใน §5)
- ไม่ทำ global banner ค้างทุกหน้าใน `ShellLayout` — ใช้ banner ต่อหน้า + สรุปบน Dashboard
- ไม่ probe NFLOG (traffic log) ในเฟสนี้ — เหตุผลใน §5 ข้อ 4 (เสี่ยงแย่ง group กับ watcher)
- ไม่ probe Wi-Fi/wpa_supplicant และ QoS/tc ในเฟสนี้ — registry รองรับไว้แล้ว เพิ่มทีหลังได้
- ไม่เก็บผล probe ลง SQLite (runtime state, ถนอม SD card)

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ 2026-07-25)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| kernel interface สำหรับ health/capability | ❌ ยังไม่มีเลย | `kernel/interfaces.go` (มี 14 interface ไม่มีตัวไหนเกี่ยวกับ availability) |
| firewall real | ⚠️ error ถูกโยนขึ้นไป แต่ถูกกลืนตอน startup | `kernel/real_firewall.go:41` `nftables.New()`, `:623` `conn.Flush()` → error |
| การกลืน error ตอน boot | ⚠️ แค่ `log.Printf` warning | `cmd/pigate/main.go:399` `firewallService.InitApplyConfig()` — ล้มเหลวแล้วบูตต่อเงียบ ๆ |
| firewall service | ⚠️ ไม่เก็บสถานะ apply ล่าสุด | `service/firewall.go:120` `SyncFirewallRules`, `:169` `InitApplyConfig` |
| helper อ่านสถานะ systemd unit ผ่าน D-Bus | ✅ มีแล้ว ใช้ซ้ำได้ | `kernel/dbus_systemd.go:79` `GetUnitRuntimeState` (แยก NoSuchUnit ออกจาก error จริงแล้ว) |
| pattern service ที่มี catalog + kernel manager | ✅ ใช้เป็นแม่แบบได้ | `service/system_service.go` (whitelist/policy อยู่ service, kernel policy-free) |
| endpoint กลุ่ม system | ✅ มี 13 เส้น ยังไม่มี capabilities | `api/router.go:151-173` |
| DTO ฝั่ง model | ❌ ยังไม่มี | `model/types.go:343` `ServiceRuntimeState`, `:516` `SystemInfo` (ที่ที่ควรวางของใหม่ใกล้ ๆ) |
| frontend service | ❌ ยังไม่มี | `frontend/src/services/systemService.ts` (มี getServices/getHostname เป็นแม่แบบ) |
| frontend provider pattern | ✅ มี 2 ตัวใช้เป็นแม่แบบได้ | `components/HostnameProvider.tsx` (fetch ครั้งเดียว), `components/MetricsProvider.tsx` (SSE) — mount ที่ `layout/ShellLayout.tsx:16-18` |
| UI banner component | ✅ มี `Alert` shadcn แล้ว | `components/ui/alert.tsx` (variant `default`/`destructive`, มี `AlertAction`) |
| หน้าที่ต้องใส่ banner | ❌ ยังไม่มี | `pages/FirewallPolicy.tsx:296`, `pages/PortForwarding.tsx:75`, `pages/ForwardTraffic.tsx:90`, `pages/Dashboard.tsx:638` |

**สรุป:** ไม่มีอะไรของเดิมให้ต่อยอดเลยฝั่ง backend (สร้างใหม่ทั้ง interface/real/mock/
service/handler) แต่ได้ helper D-Bus และ pattern `SystemServiceService` มาใช้ซ้ำได้เต็ม ๆ
ฝั่ง frontend มี provider pattern + `Alert` พร้อมแล้ว งานคือ service + provider + banner

## 2. แนวทางเทคนิค

### 2.1 กลไก probe (read-only ทั้งหมด)

```go
// nftables: ดัมพ์รายการ table (read-only) — ล้มเหลว = subsystem ใช้ไม่ได้จริง
c, err := nftables.New()               // ห้ามใส่ AsLasting() จะค้าง socket ไว้
_, err = c.ListTablesOfFamily(nftables.TableFamilyINet)
// classify errno: EOPNOTSUPP/EPROTONOSUPPORT/EAFNOSUPPORT/ENOENT -> "not_supported"
//                 EPERM/EACCES                                    -> "permission_denied"
//                 อื่น ๆ                                          -> "probe_failed"

// D-Bus/systemd: ใช้ helper เดิม ไม่เขียนใหม่
st, err := kernel.GetUnitRuntimeState("dnsmasq.service")   // dbus_systemd.go:79
// err != nil (ต่อ system bus ไม่ได้ เช่น WSL ไม่มี systemd) -> "no_dbus"
// !st.Loaded                                              -> "service_missing"
// st.ActiveState != "active"                              -> "service_inactive" (degraded)
```

**ทำไมเลือก `ListTablesOfFamily` แทนวิธีอื่น:**
- `nftables.New()` เฉย ๆ **ไม่พอ** — v0.3.0 ไม่ dial netlink ถ้าไม่ได้ใส่ `AsLasting()`
  (ดู `conn.go:62-70`) จึงคืน nil error เสมอ ต้องมีการเรียกที่ยิง netlink จริงถึงจะรู้
- ทางเลือกที่ตัดทิ้ง: อ่าน `/proc/net/nf_tables*` หรือ `/sys/module/nf_tables` — พึ่งพา
  procfs layout ที่ไม่ใช่ contract และไม่ครอบคลุมกรณีสิทธิ์ไม่พอ (setcap หาย)
- ทางเลือกที่ตัดทิ้ง: สร้าง table ทดสอบแล้วลบทิ้ง — mutate ระบบจริง ผิดหลัก "probe ต้อง read-only"

### 2.2 สองแหล่งความจริง: probe + last apply error

probe บอกได้แค่ว่า "netlink ตอบ nf_tables ไหม" **ไม่ได้พิสูจน์ว่ากฎชุดจริง apply ได้**
(บาง kernel มี `nf_tables` แต่ขาด expression module เช่น `nft_fib`, `nft_log`, nat chain)
จึงต้องมีช่องทางที่สองคือ **ผลลัพธ์ apply ครั้งล่าสุดจริง ๆ**:

- `FirewallService` เก็บ `lastApplyErr` / `lastApplyAt` ทุกครั้งที่ `SyncFirewallRules` จบ
- `SystemCapabilityService` มี `RegisterApplyHealth(id, reporter)` ให้ subsystem ใด ๆ
  ส่งสถานะ apply ล่าสุดเข้ามา (ต่อไป QoS/DNS ใช้กลไกเดียวกันได้)
- กติกา merge: probe fail ชนะทุกอย่าง; ถ้า probe ผ่านแต่ apply ล่าสุดล้มเหลว →
  `available=true, degraded=true, reason="apply_failed"` พร้อมข้อความ error จริง

### 2.3 Registry ให้ขยายได้

`RealCapabilityProber` ถือ slice ของ `{ID, probeFn}` — เพิ่ม subsystem ใหม่ = เพิ่ม 1 entry
+ 1 ฟังก์ชัน ไม่ต้องแตะ service/api/frontend เลย (frontend render จาก array ที่ backend ส่งมา)
ชื่อ/ข้อความภาษาไทยอยู่ **ชั้น service** (kernel policy-free) ตามแม่แบบ `system_service.go`

### 2.4 Cache + cadence

probe ทุก request = ยิง netlink + D-Bus ทุกครั้ง (Dashboard poll อยู่แล้ว) →
`SystemCapabilityService` cache ผลไว้ใน RAM + TTL 30 วินาที, `?force=1` บังคับ probe ใหม่
(สำหรับปุ่ม "ตรวจสอบใหม่") **ไม่สร้าง goroutine background เพิ่ม** — capability เปลี่ยนยาก
poll ตาม request พอ และไม่กิน CPU ตอนไม่มีใครเปิดหน้าเว็บ

## 3. ขั้นตอนการทำ (เรียงตาม dependency: model → kernel → service → main → api → docs → frontend)

### T-01 · DTO ของ capability
**ไฟล์ (ใหม่):** `backend/internal/model/capability.go`
- `CapabilityProbeResult{ID, Available, Degraded bool, Reason, Err string}` — ผลดิบจาก kernel
- `CapabilityStatus{ID, Name, Available, Degraded bool, Reason, Detail, CheckedAt string}` — DTO ที่ส่งออก (json camelCase)
- `SystemCapabilities{Mock bool, CheckedAt string, Capabilities []CapabilityStatus}`
- ค่า Reason ที่ใช้ได้: `ok`, `mock`, `not_supported`, `permission_denied`, `no_dbus`,
  `service_missing`, `service_inactive`, `apply_failed`, `probe_failed`
> ไม่ต้องแตะ `model/types.go` — ไฟล์ใหม่แยกชัดกว่า (เหมือน `model/dns_server.go`)

### T-02 · kernel interface
**ไฟล์:** `backend/internal/kernel/interfaces.go` (ต่อท้าย ~บรรทัด 211)
- เพิ่ม `type CapabilityProber interface { ProbeAll() []model.CapabilityProbeResult }`
- comment ระบุชัดว่า implementation **ต้อง read-only** และห้าม block นาน (มี timeout ในตัว)
> **ห้าม** เพิ่ม method ลง `FirewallManager` เดิมเด็ดขาด — จะพัง fake ใน
> `service/firewall_test.go:11` `trackingFirewallManager` และ mock อีกหลายตัว

### T-03 · real prober (sensitive: netlink + D-Bus)
**ไฟล์ (ใหม่):** `backend/internal/kernel/real_capability.go` (ใส่ `//go:build linux` ตามพี่น้อง `real_*.go`)
- `RealCapabilityProber` + `NewRealCapabilityProber()`
- registry: `firewall` (nftables), `dbus` (system bus), `dnsmasq` (`dnsmasq.service`),
  `resolved` (`systemd-resolved.service`)
- helper `classifyNetlinkErr(err) (reason string)` ใช้ `errors.Is` กับ `unix.EOPNOTSUPP`,
  `unix.EPROTONOSUPPORT`, `unix.EAFNOSUPPORT`, `unix.ENOENT`, `unix.EPERM`, `unix.EACCES`
- ถ้า probe `dbus` ล้มเหลว → `dnsmasq`/`resolved` รายงาน `no_dbus` โดยไม่ต้องยิงซ้ำ
> **ห้าม** เรียก `AddTable`/`FlushTable`/`Flush()`/`nflog.Open()` ในไฟล์นี้

### T-04 · mock prober
**ไฟล์:** `backend/internal/kernel/mock.go` (ต่อท้าย ~บรรทัด 455 กลุ่ม `NewMockSystemServiceManager`)
- `MockCapabilityProber` คืน available=true, Reason `mock` ครบทุก id เดียวกับ real
- ห้ามมี side effect ใด ๆ ต่อ OS

### T-05 · service layer + catalog ภาษาไทย
**ไฟล์ (ใหม่):** `backend/internal/service/system_capability.go`
- `SystemCapabilityService{prober, mock bool, cache, cachedAt, mu, applyHealth map[string]ApplyHealthReporter, eventLog}`
- catalog `id → DisplayName` + ฟังก์ชัน `detailFor(reason, err)` แปลงเป็นข้อความไทย เช่น
  `not_supported` → "เคอร์เนลของเครื่องนี้ไม่รองรับ nf_tables (พบบ่อยบน WSL/คอนเทนเนอร์)"
  `permission_denied` → "สิทธิ์ไม่พอ — ต้องรัน `sudo setcap cap_net_admin,cap_net_raw+ep`"
- `Get(force bool) model.SystemCapabilities` — ใช้ cache ถ้าอายุ < 30s
- `Refresh()` เรียกตอน startup + log + ยิง event log `system.capability_unavailable`
  (`EventCategorySystem`, `EventSeverityWarning`) เฉพาะตัวที่ไม่ available
- `RegisterApplyHealth(id string, r ApplyHealthReporter)` + interface
  `ApplyHealthReporter interface{ ApplyHealth() (ok bool, detail string, at time.Time) }`
> constructor รับ `prober` + `mock bool` + `eventLog` เท่านั้น — **ห้าม** ไปแก้ signature ของ
> `NewSystemStatusService` (จะพัง `service/system_status_test.go:59`)

### T-06 · FirewallService เก็บผล apply ล่าสุด
**ไฟล์:** `backend/internal/service/firewall.go` (`SyncFirewallRules` ~บรรทัด 120)
- เพิ่มฟิลด์ `mu sync.RWMutex; lastApplyErr error; lastApplyAt time.Time`
- ตั้งค่าทั้ง success/failure ก่อน return ของ `SyncFirewallRules`
- เพิ่ม method `ApplyHealth() (bool, string, time.Time)` ให้ตรง `ApplyHealthReporter`
> พฤติกรรมเดิมของ `SyncFirewallRules` ต้องไม่เปลี่ยน (ยัง return error เดิมทุกกรณี)

### T-07 · wiring ใน main.go
**ไฟล์:** `backend/cmd/pigate/main.go`
- ประกาศ `var capProber kernel.CapabilityProber` ในบล็อก ~บรรทัด 112-124
- เลือก real/mock ในบล็อก if/else เดิม (~127-159)
- สร้าง `capabilityService := service.NewSystemCapabilityService(capProber, cfg.Mock, eventLogService)`
  **หลัง** `eventLogService` ถูกสร้าง (~บรรทัด 185)
- `capabilityService.RegisterApplyHealth("firewall", firewallService)`
- ส่งเข้า `api.NewServer(...)` เป็น parameter สุดท้าย (~บรรทัด 299)
- เรียก `capabilityService.Refresh()` **หลัง** step 6.3 firewall apply (~บรรทัด 401)
  เพื่อให้ผล apply ล่าสุดถูกนับรวมใน log/event ตั้งแต่บูต
> ไม่ต้องมี `InitApplyConfig()` — capability ไม่มี state ให้ apply ลง kernel

### T-08 · API handler + route
**ไฟล์:** `backend/internal/api/handlers.go` (เพิ่มฟิลด์ struct ~51, param ~80, handler ใกล้ `HandleGetSystemInfo:363`)
และ `backend/internal/api/router.go` (~บรรทัด 151 กลุ่ม system)
- `authRoute("GET /api/system/capabilities", s.HandleGetSystemCapabilities)` — GET อ่านอย่างเดียว
  ทุก role ที่ล็อกอินอ่านได้ (ไม่มีข้อมูล sensitive; เป็นข้อมูลสภาพแวดล้อมล้วน)
- handler รองรับ `?force=1`, **nil-guard**: ถ้า `s.capabilityService == nil` ตอบ 503 (กัน test ที่ส่ง nil)
- อัปเดต call site `api.NewServer(` ทั้ง 5 จุดใน `backend/internal/api/handlers_test.go`
  (บรรทัด 67, 368, 556, 669, 817) ส่ง `nil` ตัวสุดท้าย

### T-09 · unit tests ฝั่ง backend
**ไฟล์ (ใหม่):** `backend/internal/service/system_capability_test.go`
- fake prober → ตรวจ mapping reason→Detail, `Available=false` ถูกส่งออกถูกตัว
- ตรวจ cache TTL (probe ครั้งที่สองภายใน TTL ต้องไม่เรียก prober ซ้ำ) และ `force=true` ต้องเรียกซ้ำ
- ตรวจ merge apply health: probe ok + apply fail → `degraded=true, reason="apply_failed"`
- mock mode → available ทุกตัว + `Mock=true`

### T-10 · เอกสาร API (sync สองไฟล์)
**ไฟล์:** `docs/openapi.yaml` (~บรรทัด 2242 ก่อน `/system/time`) และ `frontend/public/openapi.yaml`
- path `/system/capabilities` + schema `SystemCapabilities`/`CapabilityStatus` เนื้อหาต้องเหมือนกันเป๊ะทั้งสองไฟล์

### T-11 · frontend API client
**ไฟล์ (ใหม่):** `frontend/src/services/capabilityService.ts`
- `getCapabilities(force = false): Promise<SystemCapabilities>` ยิง `${API_BASE_URL}/system/capabilities`
- มี branch `IS_MOCK_MODE` คืน available ทุกตัว (ตามสไตล์ `systemService.ts`)

### T-12 · provider + hook
**ไฟล์ (ใหม่):** `frontend/src/hooks/capability-context.ts`, `frontend/src/hooks/useCapabilities.ts`,
`frontend/src/components/CapabilitiesProvider.tsx`
**ไฟล์ (แก้):** `frontend/src/components/layout/ShellLayout.tsx` (~16-18 ครอบใน `HostnameProvider`)
- fetch ตอน mount + refetch ทุก 5 นาที + expose `refresh()` (เรียก `force=1`)
- fetch ล้มเหลว → ถือว่า "ไม่ทราบสถานะ" และ **ไม่แสดง banner** (ห้าม false alarm)
- hook `useCapability(id)` คืน `CapabilityStatus | undefined`
> ทำตามแม่แบบ `HostnameProvider.tsx` — context + provider แยกไฟล์ตาม pattern เดิม (react-refresh lint)

### T-13 · component banner
**ไฟล์ (ใหม่):** `frontend/src/components/CapabilityBanner.tsx`
- props `{ id: string }` → ไม่แสดงอะไรเลยถ้า available && !degraded
- ใช้ `<Alert variant="destructive">` (unavailable) / `variant="default"` + สี `text-warning` (degraded)
- ห้าม hardcode สี palette (ใช้ `text-destructive`, `bg-warning/10` ฯลฯ), ห้าม `shadow-*`/`backdrop-blur-*`
- มีปุ่มใน `<AlertAction>` เรียก `refresh()` ("ตรวจสอบอีกครั้ง")

### T-14 · ใส่ banner ในหน้าที่พึ่ง nftables
**ไฟล์:** `frontend/src/pages/FirewallPolicy.tsx:296`, `pages/PortForwarding.tsx:75`, `pages/ForwardTraffic.tsx:90`
- วาง `<CapabilityBanner id="firewall" />` เป็น element แรกใน return ของแต่ละหน้า

### T-15 · สรุปรวมบน Dashboard
**ไฟล์:** `frontend/src/pages/Dashboard.tsx` (~บรรทัด 638 ใน return, เหนือ `<Tabs>` หรือแถวแรกใน `TabsContent`)
- แสดง Alert เดียวสรุปว่า "ฟีเจอร์ต่อไปนี้ใช้งานไม่ได้บนเครื่องนี้: Firewall, DNS Server"
  เฉพาะเมื่อมีอย่างน้อยหนึ่งตัวไม่ available (loop จาก array — subsystem ใหม่โผล่เองอัตโนมัติ)

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/system/capabilities` (ใหม่) | ทุก role ที่ล็อกอิน (`authRoute`) | คืนสถานะ capability ทุกตัว + เหตุผล; `?force=1` บังคับ probe ใหม่ข้าม cache |

- **`-disable-edit=true`**: ไม่กระทบ — เป็น GET, `DisableEditMiddleware` บล็อกเฉพาะ mutation
- ไม่แก้ endpoint เดิม `/api/system/info` — คนละ concern (identity vs health) และ Dashboard poll ถี่กว่า

## 5. ข้อควรระวัง

1. **ห้ามเพิ่ม method ลง interface เดิมใน `interfaces.go`** — `service/firewall_test.go:11`
   `trackingFirewallManager` และ mock ตัวอื่นจะ compile ไม่ผ่านทันที ใช้ interface ใหม่แยกตัวเท่านั้น
2. **`nftables.New()` ไม่ใช่การ probe** — v0.3.0 คืน `*Conn` โดยไม่ dial ถ้าไม่ใส่ `AsLasting()`
   (`conn.go:62-70`) ถ้า dev เขียนแค่ `New()` แล้วเช็ค err จะได้ "available" เสมอแม้บน WSL
   ต้องมีการเรียกที่ยิง netlink จริง (`ListTablesOfFamily`)
3. **probe ผ่าน ≠ ใช้งานได้จริง** — WSL kernel รุ่นใหม่มี `nf_tables` แต่อาจขาด expression
   module (`nft_fib`, `nft_log`, nat chain) ทำให้ `Flush()` ล้มเหลวทั้งที่ list table ได้
   จึงต้องมี T-06 (last apply error) คู่กันเสมอ — ถ้าตัด T-06 ทิ้ง อาการเดิมของผู้ใช้จะยังไม่หาย
4. **ห้าม probe NFLOG ด้วยการ `nflog.Open()`** — `real_traffic_log.go:94` มี watcher bind group 100
   ค้างอยู่แล้วตลอดอายุ process การ bind ซ้ำอาจแย่ง/แบ่ง packet ทำให้ log หายเป็นช่วง ๆ
   ถ้าจะทำสถานะ traffic log ให้เก็บ error ที่ `WatchForwardTraffic` คืนใน goroutine
   ที่ `main.go:337-346` แล้วป้อนเข้า `RegisterApplyHealth("trafficLog", ...)` แทน
5. **ห้าม disable ปุ่ม/ฟอร์มตาม capability** — DB คือ source of truth ผู้ใช้ต้องแก้ config
   ล่วงหน้าบนเครื่อง dev แล้วยกไปรันบน Pi ได้ ถ้า disable UI จะแก้อะไรไม่ได้เลยบน WSL
   (ถ้าเจ้าของโปรเจกต์อยากได้แบบ block ต้องตัดสินใจเพิ่ม — เป็น scope decision ไม่ใช่ของ dev)
6. **fetch capability ล้มเหลว = เงียบ** — ห้ามเด้ง banner "ใช้ไม่ได้" ตอน API ยิงไม่ผ่าน
   (เช่น backend เพิ่ง restart) จะกลายเป็น false alarm ที่ผู้ใช้เชื่อถือไม่ได้
7. **probe ต้องไม่ทำให้ handler ค้าง** — `dbus.SystemBus()` บนเครื่องที่ไม่มี D-Bus จะ error เร็ว
   แต่ถ้ามี socket ที่ไม่มีคนตอบอาจค้างได้ ให้ probe ทั้งชุดรันภายใต้ timeout รวม (เช่น 3 วินาที)
   แล้วตัวที่ไม่ทันตอบรายงาน `probe_failed`
8. **สิทธิ์:** ไม่ต้องแก้ `install.sh` — probe ใช้สิทธิ์เท่าที่ `cap_net_admin` มีอยู่แล้ว และ
   D-Bus read property ไม่ต้องมี Polkit rule เพิ่ม (Polkit คุมเฉพาะ method ที่ mutate)
9. **cache ต้อง thread-safe** — handler ถูกเรียกพร้อมกันได้หลาย request ใช้ `sync.RWMutex`
   แบบเดียวกับ `SystemStatusService` (`system_status.go:43-53`)
10. **ทดสอบบนบอร์ดจริง (Pi) ต้องได้ available ครบ** — ถ้าบน Pi จริงขึ้น banner แดง แปลว่า
    probe classify errno ผิด (false positive) อันตรายกว่าไม่มี banner เพราะทำให้ผู้ใช้ไม่เชื่อระบบ

## 6. Checklist สรุป (Definition of Done)

- [ ] `backend/internal/model/capability.go` (ใหม่)
- [ ] `backend/internal/kernel/interfaces.go` — `CapabilityProber`
- [ ] `backend/internal/kernel/real_capability.go` (ใหม่, read-only, มี timeout)
- [ ] `backend/internal/kernel/mock.go` — `MockCapabilityProber`
- [ ] `backend/internal/service/system_capability.go` (ใหม่)
- [ ] `backend/internal/service/firewall.go` — `ApplyHealth()` + บันทึกผล apply ล่าสุด
- [ ] `backend/cmd/pigate/main.go` — real/mock select + construct + register + Refresh ตอน boot
- [ ] `backend/internal/api/handlers.go` + `router.go` — handler + route + nil-guard
- [ ] `backend/internal/api/handlers_test.go` — แก้ call site `NewServer(` ทั้ง 5 จุด
- [ ] `backend/internal/service/system_capability_test.go` (ใหม่)
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync ตรงกัน)
- [ ] `frontend/src/services/capabilityService.ts` (ใหม่)
- [ ] `frontend/src/hooks/capability-context.ts` + `useCapabilities.ts` + `components/CapabilitiesProvider.tsx` (ใหม่) + mount ใน `ShellLayout.tsx`
- [ ] `frontend/src/components/CapabilityBanner.tsx` (ใหม่)
- [ ] `FirewallPolicy.tsx` / `PortForwarding.tsx` / `ForwardTraffic.tsx` / `Dashboard.tsx` — ใส่ banner
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] ทดสอบ mock mode (`-mock=true`): ไม่มี banner ทุกหน้า, endpoint คืน `mock: true`
- [ ] ทดสอบ WSL real mode (`-mock=false`): หน้า Firewall/Port Forwarding/Forward Traffic ขึ้น banner
      พร้อมเหตุผลอ่านรู้เรื่อง, Dashboard ขึ้นสรุป, event log มี `system.capability_unavailable`
- [ ] ทดสอบบน Pi จริง: ทุก capability = available, ไม่มี banner, กฎ firewall ยัง apply ปกติ
- [ ] ทดสอบเคส setcap หาย (บน Pi/WSL): ต้องได้ `permission_denied` ไม่ใช่ `not_supported`
