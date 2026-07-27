# Kernel Capability Detection — ตรวจจับ subsystem ที่ใช้งานจริงไม่ได้ แล้วเตือนบน UI

> เอกสารแผนงานสำหรับฟีเจอร์: ตอนนี้รัน PiGate โหมด real บนเครื่องที่ไม่ใช่ Debian/Pi
> จริง (เช่น WSL) ฟีเจอร์ Firewall เรียก nftables ผ่าน Netlink แล้วล้มเหลว/ไม่มีผล
> แต่ **UI ไม่บอกอะไรเลย** ผู้ใช้เข้าใจว่ากฎถูก apply แล้ว
> งานนี้เพิ่มชั้น "capability probe" ในชั้น kernel + endpoint ใหม่ + banner บน UI
> โดยออกแบบเป็น registry ให้เพิ่ม subsystem อื่น (QoS/dnsmasq/resolved/Wi-Fi) ได้ทีหลัง
>
> วันที่เขียน: 2026-07-25 · ทวนสอบกับโค้ดจริงอีกครั้ง: 2026-07-27 (issue #94)
> Branch อ้างอิง: `main` (งานจริงทำบน `feat/kernel-capability-detection`)
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

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ 2026-07-25 · ทวนซ้ำ 2026-07-27 ทุกบรรทัดยังตรง)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| kernel interface สำหรับ health/capability | ❌ ยังไม่มีเลย | `kernel/interfaces.go` (มี 14 interface จบที่บรรทัด 210 ไม่มีตัวไหนเกี่ยวกับ availability) |
| firewall real | ⚠️ error ถูกโยนขึ้นไป แต่ถูกกลืนตอน startup | `kernel/real_firewall.go:41` `nftables.New()`, `:623` `conn.Flush()` → error |
| การกลืน error ตอน boot | ⚠️ แค่ `log.Printf` warning | `cmd/pigate/main.go:399` `firewallService.InitApplyConfig()` — ล้มเหลวแล้วบูตต่อเงียบ ๆ (และอีก 8 subsystem ที่ `main.go:308-407` เป็นแบบเดียวกันทั้งหมด → ดู T-16) |
| firewall service | ⚠️ ไม่เก็บสถานะ apply ล่าสุด | `service/firewall.go:120` `SyncFirewallRules` (มี 8 จุด return), `:169` `InitApplyConfig` |
| helper อ่านสถานะ systemd unit ผ่าน D-Bus | ✅ มีแล้ว ใช้ซ้ำได้ | `kernel/dbus_systemd.go:79` `GetUnitRuntimeState` (ไฟล์นี้มี `//go:build linux`) |
| pattern service ที่มี catalog + kernel manager | ✅ ใช้เป็นแม่แบบได้ | `service/system_service.go` (whitelist/policy อยู่ service, kernel policy-free) |
| endpoint กลุ่ม system | ✅ มี 13 เส้น ยังไม่มี capabilities | `api/router.go:151-173` |
| `Server` struct + `NewServer` | ✅ struct จบที่ `:52`, ฟังก์ชัน `:54-110`, param ตัวสุดท้าย `systemServiceSvc` `:80` | `api/handlers.go` |
| DTO ฝั่ง model | ❌ ยังไม่มี | `model/types.go:343` `ServiceRuntimeState`, `:516` `SystemInfo` (ของใหม่แยกไฟล์ ไม่แตะไฟล์นี้) |
| event log service | ✅ `Log()` ปลอดภัยแม้เรียกก่อน `Start()` (คิวใน RAM) | `service/event_log.go:53`, `:82` |
| frontend service | ❌ ยังไม่มี | `frontend/src/services/systemService.ts` (มี getServices/getHostname เป็นแม่แบบ) |
| frontend provider pattern | ✅ มี 2 ตัวใช้เป็นแม่แบบได้ | `components/HostnameProvider.tsx` (fetch ครั้งเดียว), `components/MetricsProvider.tsx` (SSE) — mount ที่ `layout/ShellLayout.tsx:16-17` |
| UI banner component | ✅ มี `Alert` shadcn แล้ว | `components/ui/alert.tsx` (`Alert`/`AlertTitle`/`AlertDescription`/`AlertAction` export ที่ `:76`) |
| หน้าที่ต้องใส่ banner | ❌ ยังไม่มี | `pages/FirewallPolicy.tsx` (component `:296`, `return (` ตัวหลัก `:606`), `pages/PortForwarding.tsx` (component `:75`), `pages/ForwardTraffic.tsx` (component `:90`), `pages/Dashboard.tsx:638` (`return (` → root คือ `<Tabs>`) |
| การกลืน error body ฝั่ง frontend | ⚠️ ทุก throw ใน `policyService.ts` ใช้แค่ `response.statusText` | `frontend/src/services/policyService.ts:39,61,90,125,145,167,189,207` (backend ส่ง `{"message": ...}` ที่มีสาเหตุจริงมาให้แล้วที่ `api/handlers.go:1272`) → ดู T-17 |

**สรุป:** ไม่มีอะไรของเดิมให้ต่อยอดเลยฝั่ง backend (สร้างใหม่ทั้ง interface/real/mock/
service/handler) แต่ได้ helper D-Bus และ pattern `SystemServiceService` มาใช้ซ้ำได้เต็ม ๆ
ฝั่ง frontend มี provider pattern + `Alert` พร้อมแล้ว งานคือ service + provider + banner

**ยืนยันว่าไม่ชนของเดิม:** `model/capability.go`, `kernel/real_capability.go`,
`service/system_capability.go`, `service/system_capability_test.go`,
`frontend/src/services/capabilityService.ts`, `frontend/src/hooks/capability-context.ts`,
`frontend/src/hooks/useCapabilities.ts`, `frontend/src/components/CapabilitiesProvider.tsx`,
`frontend/src/components/CapabilityBanner.tsx` — **ยังไม่มีไฟล์ไหนอยู่ในโปรเจกต์** (ตรวจ 2026-07-27)

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
- `nftables.New()` เฉย ๆ **ไม่พอ** — v0.3.0 (ยืนยันใน `backend/go.mod:8`) ไม่ dial netlink
  ถ้าไม่ได้ใส่ `AsLasting()` (ดู `conn.go:62-70`) จึงคืน nil error เสมอ ต้องมีการเรียกที่ยิง
  netlink จริงถึงจะรู้
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

> **หมายเหตุสำหรับ ai-developer:** ทำทุก task ให้ครบตามลำดับก่อน **แล้วค่อยทดสอบรวมทีเดียว**
> ตาม §7 Final Acceptance ท้ายเอกสาร — ไม่ต้องหยุดทดสอบทั้งระบบทีละ task
> ("เสร็จเมื่อ" ของแต่ละ task คือเกณฑ์ว่างานชิ้นนั้นจบแล้ว ไม่ใช่การทดสอบทั้งระบบ)

### T-01 · DTO ของ capability
**Layer:** model · **depends_on:** —
**ไฟล์ (ใหม่):** `backend/internal/model/capability.go`
- `CapabilityProbeResult{ID, Available, Degraded bool, Reason, Err string}` — ผลดิบจาก kernel
- `CapabilityStatus{ID, Name, Available, Degraded bool, Reason, Detail, CheckedAt string}` — DTO ที่ส่งออก (json camelCase)
- `SystemCapabilities{Mock bool, CheckedAt string, Capabilities []CapabilityStatus}`
- ค่า Reason ที่ใช้ได้ (ประกาศเป็น const ในไฟล์นี้): `ok`, `mock`, `not_supported`,
  `permission_denied`, `no_dbus`, `service_missing`, `service_inactive`, `apply_failed`, `probe_failed`
> ไม่ต้องแตะ `model/types.go` — ไฟล์ใหม่แยกชัดกว่า (เหมือน `model/dns_server.go`)

**เสร็จเมื่อ:** ไฟล์ใหม่คอมไพล์ผ่าน (`go build ./...`), มีครบ 3 struct + const ของ reason ทุกค่า, json tag เป็น camelCase

### T-02 · kernel interface
**Layer:** kernel · **depends_on:** T-01
**ไฟล์:** `backend/internal/kernel/interfaces.go` (ต่อท้ายหลังบรรทัด 210 ซึ่งเป็นจุดจบ `SystemServiceManager`)
- เพิ่ม `type CapabilityProber interface { ProbeAll() []model.CapabilityProbeResult }`
- comment ระบุชัดว่า implementation **ต้อง read-only** และห้าม block นาน (มี timeout ในตัว)
> **ห้าม** เพิ่ม method ลง `FirewallManager` เดิมเด็ดขาด — จะพัง fake ใน
> `service/firewall_test.go:11` `trackingFirewallManager` และ mock อีกหลายตัว

**เสร็จเมื่อ:** `go build ./...` ผ่าน และ interface เดิมทั้ง 14 ตัวไม่ถูกแก้แม้แต่บรรทัดเดียว

### T-03 · real prober (sensitive: netlink + D-Bus — ต้อง review เข้มเป็นพิเศษ)
**Layer:** kernel · **depends_on:** T-02
**ไฟล์ (ใหม่):** `backend/internal/kernel/real_capability.go`
- ใส่ `//go:build linux` **บังคับ** — ไฟล์นี้เรียก `GetUnitRuntimeState` ซึ่งอยู่ใน
  `dbus_systemd.go` ที่มี build tag `linux` อยู่แล้ว ถ้าไม่ใส่จะพังตอน build ข้าม platform
  (หมายเหตุ: `real_firewall.go` ไม่มี tag เพราะไม่พึ่ง D-Bus — อย่ายึดไฟล์นั้นเป็นแบบ)
- `RealCapabilityProber` + `NewRealCapabilityProber()`
- registry: `firewall` (nftables), `dbus` (system bus), `dnsmasq` (`dnsmasq.service`),
  `resolved` (`systemd-resolved.service`)
- helper `classifyNetlinkErr(err) (reason string)` ใช้ `errors.Is` กับ `unix.EOPNOTSUPP`,
  `unix.EPROTONOSUPPORT`, `unix.EAFNOSUPPORT`, `unix.ENOENT`, `unix.EPERM`, `unix.EACCES`
  (`golang.org/x/sys` v0.46.0 เป็น direct dependency อยู่แล้วใน `go.mod:14` — ไม่ต้องเพิ่ม dep ใหม่)
- ถ้า probe `dbus` ล้มเหลว → `dnsmasq`/`resolved` รายงาน `no_dbus` โดยไม่ต้องยิงซ้ำ
- `ProbeAll()` ทั้งชุดต้องจบภายใน timeout รวม (เช่น 3 วินาที) ตาม §5 ข้อ 7
> **ห้าม** เรียก `AddTable`/`FlushTable`/`Flush()`/`nflog.Open()` ในไฟล์นี้
> **ห้าม** เพิ่ม dependency ใหม่ใน `go.mod`

**เสร็จเมื่อ:** คอมไพล์ผ่านบน linux, `grep -n "AddTable\|FlushTable\|Flush()\|nflog.Open\|exec.Command" real_capability.go` ไม่เจออะไรเลย, `ProbeAll()` คืน result ครบ 4 id เสมอ (ไม่ว่า probe จะสำเร็จหรือไม่)

### T-04 · mock prober
**Layer:** kernel · **depends_on:** T-02
**ไฟล์:** `backend/internal/kernel/mock.go` (ต่อท้ายกลุ่ม `MockSystemServiceManager` ~บรรทัด 448-466)
- `MockCapabilityProber` + `NewMockCapabilityProber()` คืน available=true, Reason `mock` ครบทุก id เดียวกับ real
- ห้ามมี side effect ใด ๆ ต่อ OS

**เสร็จเมื่อ:** `var _ kernel.CapabilityProber = (*MockCapabilityProber)(nil)` compile ผ่าน และ id ที่คืนตรงกับ real ครบ 4 ตัว

### T-05 · service layer + catalog ภาษาไทย
**Layer:** service · **depends_on:** T-01, T-02
**ไฟล์ (ใหม่):** `backend/internal/service/system_capability.go`
- `SystemCapabilityService{prober, mock bool, cache, cachedAt, mu, applyHealth map[string]ApplyHealthReporter, eventLog}`
- catalog `id → DisplayName` + ฟังก์ชัน `detailFor(reason, err)` แปลงเป็นข้อความไทย เช่น
  `not_supported` → "เคอร์เนลของเครื่องนี้ไม่รองรับ nf_tables (พบบ่อยบน WSL/คอนเทนเนอร์)"
  `permission_denied` → "สิทธิ์ไม่พอ — ต้องรัน `sudo setcap cap_net_admin,cap_net_raw+ep`"
- `Get(force bool) model.SystemCapabilities` — ใช้ cache ถ้าอายุ < 30s
- `Refresh()` เรียกตอน startup + log + ยิง event log `system.capability_unavailable`
  ผ่าน `eventLog.Log(model.EventCategorySystem, "system.capability_unavailable",
  model.EventSeverityWarning, "", id, msg)` เฉพาะตัวที่ไม่ available
  (signature ยืนยันแล้วที่ `service/event_log.go:53`; ปลอดภัยแม้ถูกเรียกก่อน `Start()`)
- `RegisterApplyHealth(id string, r ApplyHealthReporter)` + interface
  `ApplyHealthReporter interface{ ApplyHealth() (ok bool, detail string, at time.Time) }`
- `eventLog` ต้อง nil-guard (เผื่อ test สร้างโดยไม่ส่ง event log)
> constructor รับ `prober` + `mock bool` + `eventLog` เท่านั้น — **ห้าม** ไปแก้ signature ของ
> `NewSystemStatusService` (จะพัง `service/system_status_test.go`)

**เสร็จเมื่อ:** `go build ./...` ผ่าน, ทุก reason จาก T-01 มีข้อความไทยครบ (ไม่มี default ว่าง), cache/registry ป้องกันด้วย `sync.RWMutex` ตามแบบ `system_status.go:43-53`

### T-06 · FirewallService เก็บผล apply ล่าสุด
**Layer:** service · **depends_on:** T-05
**ไฟล์:** `backend/internal/service/firewall.go` (struct `:14-18`, `SyncFirewallRules` `:120-166`)
- เพิ่มฟิลด์ `mu sync.RWMutex; lastApplyErr error; lastApplyAt time.Time` ใน `FirewallService`
  (struct ปัจจุบันมี 3 ฟิลด์ ไม่มี mutex เลย — `NewFirewallService` ใช้ named field literal จึงไม่ต้องแก้)
- **สำคัญ:** `SyncFirewallRules` มี **8 จุด return** (บรรทัด 123,128,133,138,144,154,159,163,165)
  → **ห้ามไล่แปะทีละจุด** ให้เปลี่ยนเป็น named return + `defer` บันทึกครั้งเดียว เช่น
  ```go
  func (s *FirewallService) SyncFirewallRules() (err error) {
      defer func() { s.recordApply(err) }()
      ...
  }
  ```
  แล้วเขียน `recordApply(err error)` เป็น method เล็ก ๆ ที่ล็อก `mu` ตั้ง `lastApplyErr/lastApplyAt`
- เพิ่ม method `ApplyHealth() (bool, string, time.Time)` ให้ตรง `ApplyHealthReporter`
  (ยังไม่เคยมี apply เลย → คืน `ok=true` + `at` เป็น zero time เพื่อไม่ให้ขึ้น banner ผิด)
> พฤติกรรมเดิมของ `SyncFirewallRules` ต้องไม่เปลี่ยน (ยัง return error เดิมทุกกรณี) — call site
> มี 12 จุด (`service/firewall.go:99,107,115,171`, `api/handlers.go:650,685,840,953,1171,1800,2944`,
> `service/firewall_test.go:102`) ห้ามแก้ signature ของ method

**เสร็จเมื่อ:** `go test ./internal/service/...` เดิมยังผ่าน, `ApplyHealth()` มีอยู่และ implement `ApplyHealthReporter` ได้ (`var _ ApplyHealthReporter = (*FirewallService)(nil)`)

### T-07 · wiring ใน main.go
**Layer:** service/wiring · **depends_on:** T-03, T-04, T-05, T-06
**ไฟล์:** `backend/cmd/pigate/main.go`
- ประกาศ `var capProber kernel.CapabilityProber` ในบล็อกประกาศ `:112-124`
- เลือก real/mock ในบล็อก if/else เดิม `:127-159` — **ใช้เงื่อนไขเดิมของบล็อกนั้น
  (`cfg.Mock || cfg.MockFromReal`) ห้ามเช็คแค่ `cfg.Mock`** เพราะโหมด mock-from-real
  ก็ไม่ได้เขียน kernel จริงเช่นกัน
- สร้าง `capabilityService := service.NewSystemCapabilityService(capProber, cfg.Mock || cfg.MockFromReal, eventLogService)`
  **หลัง** `eventLogService` ถูกสร้าง (`:185`)
- `capabilityService.RegisterApplyHealth("firewall", firewallService)`
- ส่งเข้า `api.NewServer(...)` เป็น parameter สุดท้าย (`:299` — ปัจจุบันตัวสุดท้ายคือ `systemServiceService`)
- เรียก `capabilityService.Refresh()` **หลัง** step 6.3 firewall apply (`:399-401`)
  เพื่อให้ผล apply ล่าสุดถูกนับรวมใน log/event ตั้งแต่บูต
> ไม่ต้องมี `InitApplyConfig()` — capability ไม่มี state ให้ apply ลง kernel

**เสร็จเมื่อ:** `go build ./...` ผ่าน, รัน `./pigate-backend -mock=true` แล้วเห็นบรรทัด log ของ Refresh ตอน boot โดยไม่มี panic

### T-08 · API handler + route
**Layer:** api · **depends_on:** T-05, T-07
**ไฟล์:** `backend/internal/api/handlers.go` (struct `Server` `:25-52`, `NewServer` param ตัวสุดท้าย `:80`,
field assign `:108`, วาง handler ใกล้ `HandleGetSystemInfo`) และ `backend/internal/api/router.go` (`:151` กลุ่ม system)
- `authRoute("GET /api/system/capabilities", s.HandleGetSystemCapabilities)` — GET อ่านอย่างเดียว
  ทุก role ที่ล็อกอินอ่านได้ (ไม่มีข้อมูล sensitive; เป็นข้อมูลสภาพแวดล้อมล้วน)
- handler รองรับ `?force=1`, **nil-guard**: ถ้า `s.capabilityService == nil` ตอบ 503 (กัน test ที่ส่ง nil)
- อัปเดต call site `api.NewServer(` ทั้ง 5 จุดใน `backend/internal/api/handlers_test.go`
  (บรรทัด 67, 368, 556, 669, 817) ส่ง `nil` ตัวสุดท้าย — ยืนยันแล้วว่าไม่มี call site อื่นนอกจาก `main.go:299`

**เสร็จเมื่อ:** `go build ./... && go vet ./...` ผ่าน และ `go test ./internal/api/...` เดิมยังผ่านทั้งหมด

### T-09 · unit tests ฝั่ง backend
**Layer:** service (test) · **depends_on:** T-05, T-06
**ไฟล์ (ใหม่):** `backend/internal/service/system_capability_test.go`
- fake prober → ตรวจ mapping reason→Detail, `Available=false` ถูกส่งออกถูกตัว
- ตรวจ cache TTL (probe ครั้งที่สองภายใน TTL ต้องไม่เรียก prober ซ้ำ) และ `force=true` ต้องเรียกซ้ำ
- ตรวจ merge apply health: probe ok + apply fail → `degraded=true, reason="apply_failed"`
- mock mode → available ทุกตัว + `Mock=true`
> ให้ TTL/clock ฉีดได้ (field ในโครงสร้าง หรือ ttl เป็นตัวแปรของ struct) เพื่อไม่ต้อง `time.Sleep(30s)` ใน test

**เสร็จเมื่อ:** `go test ./internal/service/ -run TestSystemCapability` ผ่าน และไม่มี test ไหนใช้ `time.Sleep` เกิน 100ms

### T-10 · เอกสาร API (sync สองไฟล์)
**Layer:** docs · **depends_on:** T-08
**ไฟล์:** `docs/openapi.yaml` (`/system/info` อยู่ `:2242`, `/system/time` อยู่ `:2260` → แทรกระหว่างสองอันนี้)
และ `frontend/public/openapi.yaml` (ตำแหน่งเดียวกัน)
- path `/system/capabilities` + schema `SystemCapabilities`/`CapabilityStatus` เนื้อหาต้องเหมือนกันเป๊ะทั้งสองไฟล์
> **ห้ามแก้** `backend/internal/api/dist/openapi.yaml` — เป็นผลลัพธ์ที่ `build.sh` copy มาจาก
> `frontend/dist` จะถูกเขียนทับอยู่แล้ว

**เสร็จเมื่อ:** `diff docs/openapi.yaml frontend/public/openapi.yaml` ไม่มีความต่าง (หรือต่างเท่าที่ต่างอยู่ก่อนแก้)

### T-11 · frontend API client
**Layer:** frontend · **depends_on:** T-08
**ไฟล์ (ใหม่):** `frontend/src/services/capabilityService.ts`
- `getCapabilities(force = false): Promise<SystemCapabilities>` ยิง `${API_BASE_URL}/system/capabilities`
- มี branch `IS_MOCK_MODE` คืน available ทุกตัว (ตามสไตล์ `systemService.ts`)
- ถ้า response ไม่ ok → โยน error โดยอ่าน `errBody.message` ก่อน fallback `statusText`
  (แบบเดียวกับ `systemService.ts:220-221`)

**เสร็จเมื่อ:** `yarn build` ผ่าน (type ตรงกับ DTO ของ T-01) และ `yarn lint` ไม่มี error ใหม่

### T-12 · provider + hook
**Layer:** frontend · **depends_on:** T-11
**ไฟล์ (ใหม่):** `frontend/src/hooks/capability-context.ts`, `frontend/src/hooks/useCapabilities.ts`,
`frontend/src/components/CapabilitiesProvider.tsx`
**ไฟล์ (แก้):** `frontend/src/components/layout/ShellLayout.tsx` (`:16-17` — ครอบ/ซ้อนกับ `HostnameProvider`/`MetricsProvider`)
- fetch ตอน mount + refetch ทุก 5 นาที + expose `refresh()` (เรียก `force=1`)
- fetch ล้มเหลว → ถือว่า "ไม่ทราบสถานะ" และ **ไม่แสดง banner** (ห้าม false alarm)
- hook `useCapability(id)` คืน `CapabilityStatus | undefined`
> ทำตามแม่แบบ `HostnameProvider.tsx` (context แยกไฟล์ + provider แยกไฟล์ ตาม react-refresh lint)

**เสร็จเมื่อ:** `yarn build && yarn lint` ผ่าน, provider mount แล้วหน้าเว็บยังทำงานปกติ, ไม่มี lint error เรื่อง react-refresh

### T-13 · component banner
**Layer:** frontend · **depends_on:** T-12
**ไฟล์ (ใหม่):** `frontend/src/components/CapabilityBanner.tsx`
- props `{ id: string }` → ไม่แสดงอะไรเลยถ้า available && !degraded (หรือยังไม่ทราบสถานะ)
- ใช้ `<Alert variant="destructive">` (unavailable) / `variant="default"` + สี warning (degraded)
- ห้าม hardcode สี palette (ใช้ `text-destructive`, `bg-warning/10` ฯลฯ ตาม `src/index.css`),
  ห้าม `shadow-*`/`backdrop-blur-*`, ต้องอ่านออกทั้ง dark/light
- มีปุ่มใน `<AlertAction>` เรียก `refresh()` ("ตรวจสอบอีกครั้ง")

**เสร็จเมื่อ:** `yarn build && yarn lint` ผ่าน และ `grep -E "shadow-|backdrop-blur-|text-(emerald|red|amber)-" CapabilityBanner.tsx` ไม่เจอ

### T-14 · ใส่ banner ในหน้าที่พึ่ง nftables
**Layer:** frontend · **depends_on:** T-13
**ไฟล์:** `frontend/src/pages/FirewallPolicy.tsx`, `pages/PortForwarding.tsx`, `pages/ForwardTraffic.tsx`
- วาง `<CapabilityBanner id="firewall" />` เป็น element แรกใน `return (` ของ **component หลัก**
  ของแต่ละหน้า — ระวัง: บรรทัดที่อ้างใน §1 (`FirewallPolicy.tsx:296`, `PortForwarding.tsx:75`,
  `ForwardTraffic.tsx:90`) คือบรรทัด `export default function ...` ไม่ใช่ `return (`
  (ของ FirewallPolicy `return (` ตัวหลักอยู่ `:606` ซึ่ง root เป็น `<div className="space-y-4">`
  ส่วนอีกสองหน้าให้ไล่หา `return (` ตัวสุดท้ายของ component หลักเอง)
- ห้ามวางใน sub-component (เช่น row/StatCard) ที่ประกาศไว้บนไฟล์เดียวกัน

**เสร็จเมื่อ:** `yarn build` ผ่าน และ banner ปรากฏบนสุดของทั้ง 3 หน้าเมื่อ capability ไม่ available

### T-15 · สรุปรวมบน Dashboard
**Layer:** frontend · **depends_on:** T-13
**ไฟล์:** `frontend/src/pages/Dashboard.tsx` (`return (` อยู่ `:638`, root คือ `<Tabs>`)
- **ระวังโครงสร้าง:** root ของ return เป็น `<Tabs>` ตัวเดียว การแทรก Alert "เหนือ `<Tabs>`"
  ต้องครอบ fragment `<>...</>` หรือ `<div className="space-y-4">` เพิ่ม
  **ห้ามใส่ไว้ใน `TabsContent` อันเดียว** เพราะ Dashboard มี 3 แท็บ (overview/compact/detailed)
  ผู้ใช้ที่เปิดแท็บอื่นจะไม่เห็นคำเตือนเลย
- แสดง Alert เดียวสรุปว่า "ฟีเจอร์ต่อไปนี้ใช้งานไม่ได้บนเครื่องนี้: Firewall, DNS Server"
  เฉพาะเมื่อมีอย่างน้อยหนึ่งตัวไม่ available (loop จาก array — subsystem ใหม่โผล่เองอัตโนมัติ)

**เสร็จเมื่อ:** `yarn build && yarn lint` ผ่าน และคำเตือนเห็นได้ทุกแท็บของ Dashboard

### T-16 · ทำให้ startup apply ที่ล้มเหลว "ไม่เงียบ" (แก้อาการ fail-open ตอนบูตเป็นการทั่วไป)
**Layer:** service/wiring · **depends_on:** T-07
**ไฟล์:** `backend/cmd/pigate/main.go` (`:307-407` ทุกบล็อก `if err := ...InitApplyConfig(); err != nil`)
- **เหตุผล:** ทุกวันนี้ startup apply ที่ล้มเหลวทั้ง 9 จุด (time `:308`, interfaces `:314`,
  routes `:320`, hostname `:366`, dhcp server `:374`, dns local zone `:388`, dns client `:393`,
  firewall `:399`, qos `:405`) แค่ `log.Printf("Warning: ...")` ลง journal — ผู้ใช้ที่ดูแต่ UI
  ไม่มีทางรู้เลยว่าบูตมาแล้ว config ยังไม่ถูก apply เข้า kernel ปัญหานี้ **กว้างกว่า WSL**
  (setcap หาย / dnsmasq ไม่ได้ติดตั้ง / interface หาย ก็เกิดได้บน Pi จริง)
- แก้แบบแคบที่สุด: ในทุกบล็อกที่ล้มเหลว ให้เพิ่ม 1 บรรทัด
  `eventLogService.Log(model.EventCategorySystem, "system.startup_apply_failed",
  model.EventSeverityWarning, "", "<subsystem>", err.Error())` โดย `<subsystem>` เป็น
  `time`/`interfaces`/`routes`/`hostname`/`dhcp_server`/`dns_server`/`dns`/`firewall`/`qos`
- ปลอดภัยแม้ event log ยัง `Start()` ไม่ถึง (`event_log.go:53` แค่ต่อคิวใน RAM, flush ทีหลัง)
- **ห้าม** เปลี่ยน `log.Printf` เดิมออก และ **ห้าม** เปลี่ยนพฤติกรรมเป็น `log.Fatal`
  (fail-open ตอนบูตเป็นการตัดสินใจเชิงออกแบบเดิม — ถ้าจะเปลี่ยนเป็น fail-closed ต้องให้เจ้าของโปรเจกต์ตัดสิน)

**เสร็จเมื่อ:** `go build ./...` ผ่าน, ทั้ง 9 จุดมี `eventLogService.Log(...)` คู่กับ `log.Printf` เดิม, ไม่มีจุดไหนเปลี่ยนเป็น fatal

### T-17 · frontend: เลิกทิ้ง error body ของ backend ใน policyService (งานเสริม ขอบเขตจำกัด 1 ไฟล์)
**Layer:** frontend · **depends_on:** — (ทำเมื่อไรก็ได้ แนะนำหลัง T-14)
**ไฟล์:** `frontend/src/services/policyService.ts` เท่านั้น
- **ปัญหา:** backend ส่งสาเหตุจริงมาให้แล้ว (`api/handlers.go:1272`
  `"OS Firewall update failed: " + err.Error()` ลง body `{"message": ...}`) แต่ frontend ทิ้งทั้งหมด
  เหลือแค่ `response.statusText` (= "Internal Server Error") ผู้ใช้จึงไม่เห็นสาเหตุ nftables ที่แท้จริง
  ทั้งที่ `FirewallPolicy.tsx:414` เอา message ไปแสดงใน alert อยู่แล้ว
- **ขอบเขต (กัน scope creep):** เพิ่ม helper ท้องถิ่นในไฟล์นี้ไฟล์เดียว ตามแบบที่โปรเจกต์ใช้อยู่
  (`services/userService.ts:68-71` `parseError`) แล้วใช้แทน `throw new Error(...statusText)`
  ทั้ง 8 จุด (`:39,61,90,125,145,167,189,207`)
- **ห้าม** แก้ service ไฟล์อื่น, ห้ามแก้ handler ฝั่ง backend, ห้ามเปลี่ยนข้อความ fallback
  เดิมให้หายไป (ยังคง `Failed to apply policy to kernel: ...` เป็น fallback เมื่อ body ว่าง)

**เสร็จเมื่อ:** `yarn build && yarn lint` ผ่าน และไม่มี `throw new Error(...statusText)` แบบดิบเหลือในไฟล์นี้

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
   (`ForwardNflogGroup`) ค้างอยู่แล้วตลอดอายุ process การ bind ซ้ำอาจแย่ง/แบ่ง packet ทำให้ log
   หายเป็นช่วง ๆ ถ้าจะทำสถานะ traffic log ให้เก็บ error ที่ `WatchForwardTraffic` คืนใน goroutine
   ที่ `main.go:337-346` แล้วป้อนเข้า `RegisterApplyHealth("trafficLog", ...)` แทน (เฟสถัดไป)
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
11. **`-mock-from-real` ต้องนับเป็น mock** — บล็อกเลือก kernel manager ใน `main.go:127`
    ใช้เงื่อนไข `cfg.Mock || cfg.MockFromReal` ถ้า capability ไปเช็คแค่ `cfg.Mock` จะได้ real prober
    คู่กับ mock firewall = รายงานผลที่ไม่สอดคล้องกับสิ่งที่ระบบทำจริง
12. **git:** ทุก task เป็นโค้ด → ต้องทำบน branch `feat/kernel-capability-detection` และเข้า PR
    (มีเฉพาะ T-10 ที่แตะ `docs/` แต่รวมอยู่ใน PR เดียวกัน) ห้าม commit เว้นแต่เจ้าของสั่ง

## 6. Checklist สรุป (Definition of Done ระดับไฟล์)

- [ ] `backend/internal/model/capability.go` (ใหม่)
- [ ] `backend/internal/kernel/interfaces.go` — `CapabilityProber`
- [ ] `backend/internal/kernel/real_capability.go` (ใหม่, `//go:build linux`, read-only, มี timeout)
- [ ] `backend/internal/kernel/mock.go` — `MockCapabilityProber`
- [ ] `backend/internal/service/system_capability.go` (ใหม่)
- [ ] `backend/internal/service/firewall.go` — `ApplyHealth()` + บันทึกผล apply ล่าสุด (named return + defer)
- [ ] `backend/cmd/pigate/main.go` — real/mock select + construct + register + Refresh ตอน boot + event log ตอน apply ล้มเหลว (T-16)
- [ ] `backend/internal/api/handlers.go` + `router.go` — handler + route + nil-guard
- [ ] `backend/internal/api/handlers_test.go` — แก้ call site `NewServer(` ทั้ง 5 จุด
- [ ] `backend/internal/service/system_capability_test.go` (ใหม่)
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync ตรงกัน, ไม่แตะ `api/dist/openapi.yaml`)
- [ ] `frontend/src/services/capabilityService.ts` (ใหม่)
- [ ] `frontend/src/hooks/capability-context.ts` + `useCapabilities.ts` + `components/CapabilitiesProvider.tsx` (ใหม่) + mount ใน `ShellLayout.tsx`
- [ ] `frontend/src/components/CapabilityBanner.tsx` (ใหม่)
- [ ] `FirewallPolicy.tsx` / `PortForwarding.tsx` / `ForwardTraffic.tsx` / `Dashboard.tsx` — ใส่ banner
- [ ] `frontend/src/services/policyService.ts` — parse error body (T-17)

## 7. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance — ทดสอบครั้งเดียวหลังทำครบ T-01..T-17)

**A. Build & unit test**
1. `cd backend && go build ./... && go vet ./... && go test ./...` ผ่านทั้งหมด (รวม test เดิมที่มีอยู่)
2. `cd frontend && yarn build && yarn lint` ผ่าน ไม่มี error ใหม่
3. `bash build.sh` สร้าง `./pigate` สำเร็จ

**B. Static / security review (ต้องตรวจด้วยตาเพิ่ม — งานนี้แตะ netlink + D-Bus)**
4. `grep -rn "exec.Command" backend/internal/kernel/real_capability.go backend/internal/service/system_capability.go` → ต้องไม่เจอ
5. `real_capability.go` ไม่มีการเรียก mutating API (`AddTable`, `DelTable`, `FlushTable`, `Flush()`, `nflog.Open`)
6. `go.mod`/`go.sum` ไม่มี dependency ใหม่
7. interface เดิมทั้ง 14 ตัวใน `kernel/interfaces.go` ไม่ถูกแก้ signature
8. endpoint ใหม่อยู่ใต้ `authRoute` (ไม่ใช่ public) และเป็น GET เท่านั้น

**C. Mock mode (`./pigate-backend -mock=true -port=8081`)**
9. `GET /api/system/capabilities` (หลังล็อกอิน) คืน `mock: true`, ทุก capability `available: true`, `reason: "mock"`
10. เปิดหน้า Firewall Policy / Port Forwarding / Forward Traffic / Dashboard → **ไม่มี banner ใด ๆ**
11. `-mock-from-real=true` ก็ต้องได้ `mock: true` เช่นกัน (ข้อควรระวังข้อ 11)

**D. WSL / เครื่องที่ไม่มี nftables (`-mock=false`)**
12. `GET /api/system/capabilities` → `firewall.available=false` พร้อม reason ที่สื่อสาเหตุจริง
    (`not_supported` หรือ `permission_denied` ไม่ใช่ `probe_failed` เปล่า ๆ)
13. หน้า Firewall Policy / Port Forwarding / Forward Traffic ขึ้น banner สีเตือน อ่านเข้าใจเป็นภาษาไทย
14. Dashboard ขึ้นสรุป "ฟีเจอร์ที่ใช้งานไม่ได้" และเห็นได้ **ทุกแท็บ** (overview/compact/detailed)
15. ปุ่ม "ตรวจสอบอีกครั้ง" ยิง `?force=1` แล้ว state อัปเดต (ตรวจใน Network tab)
16. หน้าเว็บส่วนอื่นยังใช้งานได้ปกติ ฟอร์ม/ปุ่มไม่ถูก disable (ข้อควรระวังข้อ 5)
17. ปิด backend แล้วเปิดหน้าเว็บใหม่ (fetch capability ล้มเหลว) → **ไม่มี banner "ใช้ไม่ได้" เด้ง** (ข้อควรระวังข้อ 6)
18. หน้า Event Log มี event `system.capability_unavailable` (severity warning) ตั้งแต่ตอนบูต
19. บูตด้วย config ที่ apply ไม่สำเร็จ → หน้า Event Log มี `system.startup_apply_failed` ของ subsystem ที่ล้ม (T-16)
20. กดปุ่ม Apply บนหน้า Firewall Policy ตอน nftables ใช้ไม่ได้ → alert แสดง **ข้อความสาเหตุจริงจาก backend**
    (ไม่ใช่ "Internal Server Error") (T-17) และ capability กลายเป็น `degraded/apply_failed` หลัง refresh (T-06)

**E. บน Raspberry Pi จริง (ต้องทำก่อน merge — false positive อันตรายกว่าไม่มีฟีเจอร์)**
21. ทุก capability = `available: true`, ไม่มี banner โผล่ในหน้าไหนเลย
22. กฎ firewall ยัง apply ได้ปกติ (`nft list ruleset` เห็นกฎครบเหมือนก่อนแก้), DHCP/DNS/QoS ทำงานเหมือนเดิม
23. ลบ capability ทิ้งชั่วคราว (`sudo setcap -r ./pigate`) แล้วรันใหม่ → ต้องได้ `permission_denied`
    ไม่ใช่ `not_supported` (แล้ว setcap คืนหลังทดสอบ)
24. เรียก `/api/system/capabilities` รัว ๆ 20 ครั้ง → ตอบเร็ว (cache ทำงาน), `top` ไม่เห็น CPU พุ่ง,
    ไม่มี goroutine/socket รั่ว (`ls /proc/$(pidof pigate)/fd | wc -l` คงที่ก่อน/หลัง)
