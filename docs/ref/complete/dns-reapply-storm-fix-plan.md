# DNS Re-apply Storm Fix — หยุด restart systemd-resolved รัวตอน Wi-Fi flap + เลิกยัด DNS ราย link

> แผนงานแก้ปัญหา `DNSService` re-apply DNS config ซ้ำทุก netlink event โดยเฉพาะตอน Wi-Fi
> scan/reconnect (ลิงก์กระพริบ `up=true running=false` รัว ๆ) ทำให้ `systemd-resolved` ถูกสั่ง
> restart ตลอด → DNS ใช้ไม่ได้เป็นช่วง ๆ ทั้งที่ config **ไม่เคยเปลี่ยน**
>
> เปลี่ยน 3 จุด: (1) DNS ไม่ผูกกับ event `LinkChanged`/`AddrRouteChanged` อีก, (2) เพิ่ม
> idempotency guard จำ signature ล่าสุด — เหมือนเดิมไม่ restart, (3) global/static mode เลิก
> loop `SetLinkDNS` ราย WAN link (ซ้ำซ้อน + เป็นต้นตอ `Permission denied`)
>
> เขียน: 2026-07-16 · Issue: #57 · Reference branch: `fix/dns-reapply-storm`
> Status ใน README Feature Status: DNS (client) = Completed (ไม่เปลี่ยน — เป็น bug fix/robustness)

## 0. Goal และ Scope

**Goal (พฤติกรรมที่ผู้ใช้เห็น):**
- Wi-Fi scan/reconnect หรือลิงก์ WAN กระพริบ → **ไม่มี** log `RestartUnit: systemd-resolved`
  ซ้ำ ๆ อีก, DNS resolution ไม่ขาดช่วง
- DNS config ที่ไม่เปลี่ยน (เคสปกติ) → apply ครั้งเดียวตอน startup/ตอนผู้ใช้กด Save เท่านั้น
  หลังจากนั้น "จำสถานะไว้" ถ้าเหมือนเดิมไม่ทำซ้ำ
- ไม่มี log `Failed to set link DNS ... Permission denied` และ `interface ... not found` อีก
  เพราะเลิกยัด DNS ราย link แล้วใช้ global drop-in อย่างเดียว

**เงื่อนไขเชิงเทคนิค:**
- ยังคงหลัก self-healing: ถ้า interface หาย-กลับมาจริง (`InterfaceAdded`) DNS ยัง re-apply ได้
  (แต่ผ่าน idempotency guard → ถ้าไม่เปลี่ยนก็ไม่ restart)
- ไม่มี `exec.Command` เพิ่ม, ไม่แตะ kernel interface (guard อยู่ที่ service layer)

**Out of scope:**
- แก้ปัญหา interface ค้างใน DB (`wlx0cef1548ff2b` ที่ไม่มีจริงแล้ว) — เป็นคนละเรื่อง (DB cleanup),
  หลังเลิก loop ราย link แล้ว log not-found จะหายไปเองอยู่ดี
- เพิ่ม Polkit rule ให้ `resolve1.Link.SetDNS` (ถูก reject — ดู §2)
- เปลี่ยนพฤติกรรม routing reconcile (`ReconcileKernelRoutingTable`) — คงเดิม แยกออกจาก DNS
- per-link DNS override แบบต่าง server ต่อ WAN — ระบบตั้งใจใช้ global config อยู่แล้ว

## 1. Current State (สำรวจโค้ดจริง ณ วันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Subscriber ผูก DNS กับ event | DNS + routing รวมใน subscriber เดียว ฟัง `AddrRouteChanged`+`LinkChanged` (Debounced 500ms) เรียก `ApplyDNSConfig()` ทุกครั้ง | `backend/cmd/pigate/main.go:184-194` |
| NetlinkMonitor สร้าง LinkChanged | ลิงก์ที่ index รู้จักแล้วมี flag เปลี่ยน → publish `LinkChanged` (Wi-Fi flap เข้าทางนี้รัว ๆ) | `backend/internal/service/netlink_monitor.go:157-160` |
| Debounce window | 500ms coalesce ต่อ iface — แต่ flap มาห่าง ~2s จึง flush แยกทุกครั้ง | `backend/internal/service/event_bus.go:114,206-217` |
| `ApplyDNSConfig` | **ไม่มี** guard เทียบ config เดิม — apply เต็มทุกครั้ง | `backend/internal/service/dns.go:70-123` |
| static mode: loop ราย link | `SetGlobalDNS` + วน WAN ทุกตัวเรียก `SetLinkDNS` (ต้นตอ Permission denied) | `backend/internal/service/dns.go:96-103` |
| `SetGlobalDNS` (real) | เขียน drop-in `/etc/systemd/resolved.conf.d/pigate.conf` แล้ว `RestartServiceViaDBus` **ทุกครั้งไม่มีเงื่อนไข** | `backend/internal/kernel/dns.go:159,202-214` |
| `SetLinkDNS` (real) | เรียก D-Bus `resolve1.Link.SetDNS` — ต้องมีสิทธิ์ polkit ราย link ที่ pigate ไม่มี → error | `backend/internal/kernel/dns.go:122-127` |
| DNSService struct | มีแค่ `repo` + `dnsMgr` ไม่มี field เก็บ state ล่าสุด | `backend/internal/service/dns.go:11-21` |
| Startup apply | เรียก `ApplyDNSConfig()` ตอน boot (ต้องยัง apply จริงครั้งแรกเสมอ) | `backend/cmd/pigate/main.go:335` |
| Mock DNS | `MockDNSManager` (in-memory ไม่มี side effect) — **ไม่ต้องแตะ** | `backend/internal/kernel/dns.go:219-252` |
| GetDNSConfig (read) | ยังใช้ `GetLinkDNS` โชว์ DynamicDNS ราย WAN — read-only ไม่กระทบ | `backend/internal/service/dns.go:24-54` |

สรุป: bug กระจุกที่ (1) DNS subscribe `LinkChanged` โดยไม่จำเป็น (`main.go:184`), (2) ไม่มี
idempotency guard (`dns.go` service), (3) loop `SetLinkDNS` ที่ fail อยู่แล้วและซ้ำซ้อนกับ global
drop-in (`dns.go:96-103`). ไม่ต้องแตะ kernel interface / mock / db / openapi / frontend.

## 2. Technical Approach

**จุดที่ 1 — แยก DNS ออกจาก event ที่กระพริบ (`main.go`):**
แยก subscriber เดียวเป็นสองตัว
- `"routing"` — คงฟัง `AddrRouteChanged`+`LinkChanged` เรียกเฉพาะ `ReconcileKernelRoutingTable()`
  (routing มีเหตุผลต้อง reconcile ตอน route/link เปลี่ยนจริง)
- `"dns"` — ฟัง **เฉพาะ `InterfaceAdded`** (interface กลับมาจริง ๆ เท่านั้น) Debounced
  → DNS global config ไม่ได้ขึ้นกับสถานะ flap ของลิงก์เลย

**จุดที่ 2 — idempotency guard ใน `DNSService` (service layer, ไม่แตะ kernel):**
เก็บ signature ล่าสุดใน struct แล้วเทียบก่อนลงมือ

```go
type DNSService struct {
    repo    *db.Repository
    dnsMgr  kernel.DNSManager
    mu      sync.Mutex
    lastSig string // signature ของ config ที่ apply สำเร็จล่าสุด
}

func (s *DNSService) ApplyDNSConfig() error {
    cfg, err := s.repo.GetDNSConfig() // อ่านก่อนเพื่อสร้าง signature
    ...
    sig := fmt.Sprintf("%s|%s|%s|%s", cfg.Mode, cfg.PrimaryDNS, cfg.SecondaryDNS, cfg.LocalDomain)
    s.mu.Lock(); defer s.mu.Unlock()
    if sig == s.lastSig {
        return nil // ไม่เปลี่ยน → ไม่ restart resolved
    }
    ... // ทำงานจริง (SetGlobalDNS ฯลฯ)
    s.lastSig = sig
    return nil
}
```

- เก็บใน RAM (ไม่เขียน SQLite — รักษา SD card); ครั้งแรกตอน boot `lastSig` ว่าง → apply จริงเสมอ
- ต้องมี `sync.Mutex` เพราะ `ApplyDNSConfig` ถูกเรียกได้ทั้งจาก HTTP handler (`UpdateDNSConfig`) และ
  จาก bus goroutine (debounced flush) พร้อมกัน

**จุดที่ 3 — เลิก loop `SetLinkDNS` ราย link ใน static mode (`dns.go:96-103`):**
ลบ loop ทิ้ง ใช้ `SetGlobalDNS` (drop-in) อย่างเดียว

- **argument ความปลอดภัย:** per-link `SetLinkDNS` **fail ด้วย Permission denied อยู่แล้วทุกครั้ง**
  → DNS ที่ใช้งานได้ตอนนี้มาจาก global drop-in 100% อยู่แล้ว → การลบ loop จึงไม่เปลี่ยน
  พฤติกรรม resolution จริง แค่เอา call ที่ fail + noise ออก

**ทางเลือกที่ reject:**
- *เพิ่ม Polkit rule ให้ `resolve1.Link.SetDNS`* — ให้สิทธิ์กว้างขึ้นเพื่อทำงานที่ซ้ำซ้อนกับ global
  drop-in อยู่แล้ว ผิดหลัก least-privilege และไม่แก้ต้นเหตุ (ยัง restart รัวอยู่ดีถ้าไม่มี guard)
- *เพิ่ม debounce window ให้ยาวขึ้น (เช่น 5s)* — แค่ยืดปัญหา ไม่หายจริง flap ที่ยาวกว่า window
  ยัง restart อยู่ดี และไปกระทบ subscriber อื่นที่ต้องการ 500ms
- *เอา DNS ออกจาก bus ทั้งหมด (พึ่ง guard + startup อย่างเดียว)* — เสีย self-heal เคส resolved
  ถูก reset ตอน interface กลับมา; subscribe `InterfaceAdded` + guard ให้ทั้ง self-heal และถูก

**Pattern ต้นแบบ:** การ subscribe แยก label ต่อ service มีอยู่แล้วใน `main.go:167-227`
(interface/dhcpcd/dhcp-server/qos แต่ละตัวแยก subscriber ของตัวเอง)

## 3. Steps (เรียงจาก layer ในสุดออกนอก)

### Step 1 — `DNSService`: เพิ่ม idempotency guard
**File:** `backend/internal/service/dns.go`
- `:11-21` — เพิ่ม field `mu sync.Mutex` + `lastSig string` ใน struct (import `sync`)
- `:70` — ต้น `ApplyDNSConfig` สร้าง `sig` จาก cfg แล้ว lock+เทียบ `lastSig`; เท่ากัน → `return nil`,
  ต่างกัน → ทำงานเดิมจนจบแล้วเซ็ต `s.lastSig = sig`
- **หมายเหตุ:** `UpdateDNSConfig` (`:57-67`) เขียน DB แล้วเรียก `ApplyDNSConfig` — signature จะต่าง
  จึง apply เสมอเมื่อผู้ใช้กด Save (ถูกต้อง)

### Step 2 — `DNSService`: เลิก loop SetLinkDNS ราย link (static mode)
**File:** `backend/internal/service/dns.go:96-103`
- ลบ block `for _, iface := range interfaces { if iface.Role == "WAN" { ... SetLinkDNS ... } }`
  ในสาขา `cfg.Mode == "static"` ออก — เหลือแค่ `SetGlobalDNS`

> **ยังต้องเก็บ** ตัวแปร `interfaces` ไว้ให้สาขา `else` (dynamic mode ยังวน `RevertLinkDNS`
> ราย WAN อยู่ที่ `:112-119`) — ตรวจว่าไม่มี unused variable หลังลบ

### Step 3 — `main.go`: แยก DNS subscriber ให้ฟังเฉพาะ InterfaceAdded
**File:** `backend/cmd/pigate/main.go:184-194`
- แยก subscriber `"routing-dns"` เดิมเป็น 2 ตัว:
  - `"routing"` : kinds `{AddrRouteChanged, LinkChanged}` Debounced → เรียกเฉพาะ
    `routingService.ReconcileKernelRoutingTable()`
  - `"dns"` : kinds `{InterfaceAdded}` Debounced → เรียก `dnsService.ApplyDNSConfig()`

> **ไม่ต้อง** แตะ `netlink_monitor.go`, `event_bus.go`, `interfaces.go`, `mock.go`, `db/`,
> `install.sh`, openapi, frontend — เป็นการปรับ wiring + service logic ล้วน

## 4. Related API

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| PUT | `/api/dns` (ผ่าน `UpdateDNSConfig`) | superAdmin (mutation) | **เดิม** — ยัง apply ทุกครั้งที่ Save เพราะ signature ต่าง; ไม่กระทบ contract |
| GET | `/api/dns` (`GetDNSConfig`) | authRoute | **เดิม** — ยังโชว์ DynamicDNS ราย WAN (read `GetLinkDNS`) ไม่กระทบ |

- ไม่มี route ใหม่ / ไม่มี field ใหม่ → **ไม่ต้องแก้ openapi ทั้งสองไฟล์**
- `-disable-edit=true`: GET ไม่ถูก block, PUT ถูก block อยู่แล้ว — ไม่เกี่ยวกับการแก้นี้

## 5. Cautions

1. **guard ต้องกันหลาย goroutine พร้อมกัน** — `ApplyDNSConfig` เรียกได้จาก HTTP handler และ bus
   flush พร้อมกัน ถ้าไม่มี mutex การเทียบ/เซ็ต `lastSig` จะ race → อาจ skip ผิดจังหวะหรือ apply
   ซ้อน ป้องกัน: `sync.Mutex` ครอบตั้งแต่เทียบ signature จนเซ็ตค่าใหม่
2. **ครั้งแรกตอน boot ต้อง apply จริงเสมอ** — `lastSig` เริ่มว่าง ต้องไม่เท่ากับ signature จริง
   (mode ไม่เคยเป็น "" เพราะ validate เป็น "wan"/"static") → startup apply ที่ `main.go:335`
   ทำงานปกติ ไม่ถูก guard บล็อกผิด
3. **การลบ loop SetLinkDNS ต้องยืนยันว่า DNS ยังใช้ได้** — เพราะ argument คือ "per-link fail อยู่แล้ว
   → global drop-in ทำงานจริงคนเดียว" ต้องทดสอบบนบอร์ดจริงว่า resolution ยังถูกต้องหลังลบ
   (`resolvectl status` เห็น Global DNS = ค่าที่ตั้ง, `resolvectl query <host>` ผ่าน)
4. **guard ตาม signature ของ DB config เท่านั้น** — ไม่ได้ตรวจว่าไฟล์ drop-in ถูกลบโดยคนภายนอก
   ถ้ามีใครลบ `pigate.conf` มือ ระบบจะไม่ re-apply จนกว่าจะ restart/แก้ config ยอมรับได้เพราะ
   startup apply ครอบเคส reboot; ถ้าต้องการเข้มกว่านี้ค่อยทำ issue แยก (out of scope)
5. **routing reconcile ต้องไม่ถูกลบพลาด** — ตอนแยก subscriber ต้องคง `ReconcileKernelRoutingTable`
   ให้ยังฟัง `LinkChanged`+`AddrRouteChanged` เหมือนเดิม (แค่แยก DNS ออก) ไม่งั้น self-healing
   ของ routing (issue #48) ถอยหลัง
6. **ทดสอบ regression ของ self-heal DNS** — ถอดสาย/ดับ WAN แล้วเสียบกลับ (`InterfaceAdded` จริง) →
   DNS ต้อง re-apply ครั้งเดียว (ถ้า config ไม่เปลี่ยนและ resolved ยังตั้งอยู่ guard จะ skip — ยอมรับได้
   เพราะ drop-in ยังอยู่); ต่างจาก flap (`LinkChanged`) ที่ต้อง **ไม่** ทำอะไรเลย

## 6. Summary Checklist (Definition of Done)

- [x] `backend/internal/service/dns.go` — เพิ่ม `sync.Mutex` + `lastSig` + guard ใน `ApplyDNSConfig`
- [x] `backend/internal/service/dns.go` — ลบ loop `SetLinkDNS` ในสาขา static (คง `interfaces` ให้ else ใช้)
- [x] `backend/cmd/pigate/main.go` — แยก subscriber `routing` (LinkChanged/AddrRouteChanged) กับ
      `dns` (InterfaceAdded เท่านั้น)
- [x] Test: `cd backend && go build ./... && go test ./...` ผ่าน (ปรับ `dns_test.go`: ลบ assert per-link,
      เพิ่ม assert guard/`setGlobalCalls`); `go vet` ผ่าน
- [x] Test (mock, workstation): `-mock=true` PUT `/api/system/dns` config ใหม่ → apply; PUT ค่าเดิมซ้ำ →
      log `DNS config unchanged, skipping re-apply (no resolved restart)` (ยืนยันผ่าน full HTTP stack)
- [ ] Test (บอร์ดจริง): Wi-Fi scan/reconnect → log **ไม่มี** `RestartUnit: systemd-resolved` ซ้ำ ๆ,
      **ไม่มี** `Permission denied`/`not found`; `resolvectl status` global DNS ถูกต้อง, `resolvectl query` ผ่าน
- [ ] Test (บอร์ดจริง regression): ถอด-เสียบ WAN จริง → routing ยัง self-heal, DNS ยังถูกต้อง
- [ ] Test (role): read-only login → GET `/api/dns` ยังอ่านได้ตามเดิม
- [x] ย้ายแผนไป `docs/ref/complete/` + สรุป design ลง `docs/ref/complete/dns-system-design.md`
      (เหลือเฉพาะ board-only tests ด้านบนที่รอ deploy)
