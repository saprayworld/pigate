### 0. DNS System ไม่ใช่ DNS Server

### 1. Database Layer (รูปแบบ Model ของข้อมูล)

ข้อมูล DNS จะถูกแยกเป็น 2 ระดับ คือ **Global** และ **Interface-specific** ดังนั้น Data Model ควรออกแบบให้รองรับทั้งสองแบบ (ตัวอย่างรูปแบบ Struct/JSON):

```json
{
  "id": "uuid",
  "interface": "eth0",       // ระบุชื่อ Interface (ถ้าเป็น Global ให้ใช้คำว่า "global")
  "dns_servers": ["8.8.8.8", "1.1.1.1"], // รายชื่อ DNS IP
  "is_active": true,         // สถานะการเปิด/ปิดใช้งาน (เผื่อผู้ใช้ปิดชั่วคราวโดยไม่ต้องลบทิ้ง)
  "priority": 1,             // ลำดับความสำคัญ (กรณีอยากสลับอันดับ DNS)
  "created_at": "timestamp"
}

```

* **ประเภทของ DNS:** * `custom`: ผู้ใช้ตั้งค่าเอง (เก็บใน DB)
* `system`: ระบบได้รับมาจาก DHCP หรือ Network Manager (ไม่เก็บใน DB)



---

### 2. System Layer (การคุยกับ OS ระดับล่าง)

ส่วนนี้จะรับจบเรื่องการสั่งงานผ่าน D-Bus หรือ File I/O ล้วนๆ โดยไม่สนใจ Logic ฝั่งผู้ใช้

* **`GetSystemDNS()`**: คุยกับ D-Bus (`org.freedesktop.resolve1.Manager`) เพื่อดึงค่า DNS ปัจจุบันที่ Interface ใช้อยู่ (รวมถึงค่าที่ได้จาก DHCP)
* **`SetLinkDNS(interface, servers)`**: สั่งตั้งค่า DNS ราย Interface ผ่าน D-Bus (ใช้ `SetLinkDNS`)
* **`SetGlobalDNS(servers)`**: เขียนทับไฟล์ `/etc/systemd/resolved.conf`
* **`RestartResolved()`**: สั่ง Restart `systemd-resolved.service` ผ่าน D-Bus (ใช้เมื่อมีการแก้ Global)

---

### 3. Service Layer (ตัวกลางจัดการตรรกะทางธุรกิจ)

ทำหน้าที่ประสานงานระหว่าง Database และ System Layer

**User Section:**

* **`StartupApply()`**: ดึงข้อมูล `is_active=true` ทั้งหมดจาก Database เช็คสถานะ Interface และฉีดค่า DNS เข้าไปตอนบูทระบบ
* **`GetDatabaseDNS()`**: ดึงข้อมูลที่ผู้ใช้เซฟไว้จาก Database (เอาไว้แสดงในหน้าตั้งค่า)
* **`GetActiveDNS()`**: ฟังก์ชันนี้สำคัญมาก ทำหน้าที่ Merge ข้อมูลจาก DB และ `GetSystemDNS()` เข้าด้วยกัน เพื่อแสดงให้ผู้ใช้ดูว่า **"สรุปแล้วตอนนี้ระบบใช้ DNS ตัวไหนอยู่"** (พร้อมระบุสถานะ เช่น `Active`, `Pending Interface`, หรือ `Overridden by DHCP`)
* **`AddDNS()`, `UpdateDNS()`, `RemoveDNS()**`: จัดการข้อมูลใน Database ควบคู่กับการเรียก `ApplyDNS()`
* **`ApplyDNS(interface)`**: ตรวจสอบว่า Interface ที่ระบุมีการต่อเน็ตอยู่ไหม ถ้ามี ให้เรียกใช้ `SetLinkDNS` หรือ `SetGlobalDNS` ตามประเภทของข้อมูล

**System Section:**

* **`GetDHCPDNS()`**: ดึงข้อมูล DNS เฉพาะส่วนที่ได้จาก DHCP ล้วนๆ เผื่อต้องการแสดงให้ผู้ใช้รู้ว่า ISP แจก DNS อะไรมาให้บ้างก่อนที่ผู้ใช้จะ Override

---

### 4. Monitor Section (ดักจับ Event เครือข่าย)

คล้ายกับระบบ Routing เลยครับ เนื่องจาก `systemd-resolved` มักจะรีเซ็ตค่าตัวเองเวลามีการเชื่อมต่อเครือข่ายใหม่ (เช่น ถอดสาย LAN เสียบใหม่, ต่อ Wi-Fi ใหม่, รับ DHCP Lease ใหม่)

* **`NetworkEventMonitor`**: ใช้ Netlink หรือ D-Bus Signals ดักจับสถานะ Link Up/Down
* **`AutoRecoverDNS`**: เมื่อพบว่า Interface ไหนกลับมา "Up" ระบบจะต้องหน่วงเวลาเล็กน้อย (Debounce) เพื่อรอให้กระบวนการ DHCP ฝั่ง OS ทำงานเสร็จก่อน จากนั้นค่อยไปดึง Custom DNS จาก Database มายิงทับ (Override) อีกครั้ง เพื่อให้มั่นใจว่า DNS ของผู้ใช้จะไม่ถูก DHCP กลืนหายไป

---

### 📁 โครงสร้างโปรเจกต์ (Project Structure)

```text
pigate/
├── models/
│   └── dns.go          # Database Layer (Data Structure)
├── system/
│   └── dns.go          # System Layer (OS / D-Bus Interaction)
├── services/
│   └── dns/
│       └── service.go  # Service Layer (Business Logic)
└── monitor/
    └── eventbus.go     # Event Bus (Monitor Section สำหรับคุยข้าม Service)

```

---

### 1. Database Layer: `models/dns.go`

กำหนดโครงสร้างข้อมูลที่จะใช้รับส่งในระบบและบันทึกลง Database

```go
package models

import "time"

// DNSConfig เก็บการตั้งค่า DNS ของผู้ใช้
type DNSConfig struct {
	ID         string    `json:"id" db:"id"`
	Interface  string    `json:"interface" db:"interface"` // "global" หรือชื่อ interface เช่น "eth0"
	Servers    []string  `json:"servers" db:"servers"`     // ["8.8.8.8", "1.1.1.1"]
	IsActive   bool      `json:"is_active" db:"is_active"`
	IsGlobal   bool      `json:"is_global" db:"is_global"`
	CreatedAt  time.Time `json:"created_at" db:"created_at"`
}

```

---

### 2. Monitor Section: `monitor/eventbus.go`

ตัวกลางในการประกาศ Event เพื่อให้ Service ต่างๆ ไม่ต้องผูกติดกัน (Loose Coupling)

```go
package monitor

// NetworkEventType กำหนดประเภทของ Event
type NetworkEventType string

const (
	EventLinkUp   NetworkEventType = "LINK_UP"
	EventLinkDown NetworkEventType = "LINK_DOWN"
)

// NetworkEvent โครงสร้างข้อมูลที่ส่งไปใน Channel
type NetworkEvent struct {
	Type      NetworkEventType
	Interface string
}

// EventBus ตัวจัดการ Channel กองกลาง
type EventBus struct {
	NetworkEvents chan NetworkEvent
}

func NewEventBus() *EventBus {
	return &EventBus{
		NetworkEvents: make(chan NetworkEvent, 100), // Buffer กันค้าง
	}
}

```

---

### 3. System Layer: `system/dns.go`

จัดการกับ OS ผ่าน D-Bus หรือ File I/O โดยไม่สนใจตรรกะฝั่งผู้ใช้

```go
package system

import (
	"fmt"
	// นำเข้าไลบรารี D-Bus ตามที่เคยคุยกัน
)

// SystemDNSInterface กำหนดหน้าตาของคำสั่งที่ OS ต้องทำได้
type SystemDNSInterface interface {
	SetLinkDNS(ifaceName string, servers []string) error
	SetGlobalDNS(servers []string) error
	RestartResolved() error
}

type systemDNS struct {
	// ใส่ Connection D-Bus ไว้ที่นี่
}

func NewSystemDNS() SystemDNSInterface {
	return &systemDNS{}
}

func (s *systemDNS) SetLinkDNS(ifaceName string, servers []string) error {
	// TODO: ใส่โค้ด D-Bus ที่เคยคุยกันตรงนี้
	fmt.Printf("[System] Applying DNS %v to %s via D-Bus\n", servers, ifaceName)
	return nil
}

func (s *systemDNS) SetGlobalDNS(servers []string) error {
	// TODO: แก้ไฟล์ /etc/systemd/resolved.conf
	return nil
}

func (s *systemDNS) RestartResolved() error {
	// TODO: สั่ง RestartUnit ผ่าน D-Bus
	return nil
}

```

---

### 4. Service Layer: `services/dns/service.go`

ตัวกลางประสานงาน รับข้อมูลจาก Database เช็คสถานะ Interface แล้วสั่ง System

```go
package dns

import (
	"fmt"
	"pigate/models"
	"pigate/monitor"
	"pigate/system"
)

// จำลอง InterfaceService สมมติว่ามีฟังก์ชันเช็คสถานะ
type MockInterfaceService interface {
	IsInterfaceUp(ifaceName string) bool
}

type DNSService struct {
	sysDNS    system.SystemDNSInterface
	ifaceSvc  MockInterfaceService
	eventBus  *monitor.EventBus
	// repo   Repository (สำหรับต่อ Database)
}

func NewDNSService(sys system.SystemDNSInterface, iface MockInterfaceService, bus *monitor.EventBus) *DNSService {
	svc := &DNSService{
		sysDNS:   sys,
		ifaceSvc: iface,
		eventBus: bus,
	}
	
	// เริ่มฟังสัญญาณจากระบบเครือข่ายทันทีที่สร้าง Service
	go svc.listenToNetworkEvents()
	return svc
}

// AddDNS รับคำสั่งจาก User
func (s *DNSService) AddDNS(config models.DNSConfig) error {
	// 1. บันทึกลง Database (จำลอง)
	fmt.Println("[Service] Saved DNS to Database:", config.Interface)

	// 2. เรียก Apply
	return s.ApplyDNS(config)
}

// ApplyDNS จัดการตรรกะการตั้งค่า
func (s *DNSService) ApplyDNS(config models.DNSConfig) error {
	if !config.IsActive {
		return nil
	}

	if config.IsGlobal {
		s.sysDNS.SetGlobalDNS(config.Servers)
		s.sysDNS.RestartResolved()
		return nil
	}

	// เช็คกับ InterfaceService ก่อนว่า Interface นี้มีอยู่จริงและเปิดอยู่ไหม
	if !s.ifaceSvc.IsInterfaceUp(config.Interface) {
		fmt.Printf("[Service] Interface %s is DOWN. Saved in DB, waiting for event.\n", config.Interface)
		return nil
	}

	return s.sysDNS.SetLinkDNS(config.Interface, config.Servers)
}

// listenToNetworkEvents ดักจับ Event จาก EventBus
func (s *DNSService) listenToNetworkEvents() {
	for event := range s.eventBus.NetworkEvents {
		switch event.Type {
		case monitor.EventLinkUp:
			fmt.Printf("[Monitor] Detected %s is UP. Auto-recovering DNS...\n", event.Interface)
			// TODO: ดึงข้อมูล config.Interface นี้จาก Database
			// แล้วเอามายิงทับ (Override) DHCP ที่เพิ่งได้มา
			// s.ApplyDNS(dbConfig)
		}
	}
}

```

---

## Re-apply behavior & idempotency guard (issue #57)

`DNSService.ApplyDNSConfig` เป็น **idempotent + guarded**: เก็บ `lastSig` (signature ของ
`mode|primaryDNS|secondaryDNS|localDomain`) ไว้ใน struct (RAM, ไม่เขียน SQLite) ถ้า config ที่
เข้ามาเหมือนเดิม → **return ทันที ไม่เรียก `SetGlobalDNS` และไม่ restart `systemd-resolved`**
มี `sync.Mutex` กันการเรียกซ้อนจาก HTTP handler (`UpdateDNSConfig`) กับ event bus goroutine

**Event wiring (`cmd/pigate/main.go`):** DNS subscribe เฉพาะ `InterfaceAdded` (interface กลับมา
จริง) **ไม่ผูกกับ `LinkChanged`** — global DNS เป็น system-wide drop-in ไม่ขึ้นกับสถานะ up/running
ของลิงก์ใดลิงก์หนึ่ง ดังนั้น Wi-Fi scan/reconnect ที่ทำให้ลิงก์กระพริบ (`LinkChanged` รัว ๆ) จะ
**ไม่** trigger DNS re-apply อีก (routing ยังคง reconcile บน `LinkChanged`/`AddrRouteChanged`
ผ่าน subscriber `routing` ที่แยกออกมา)

**Global-only, ไม่มี per-link SetDNS:** static/global mode ใช้ resolved drop-in
(`/etc/systemd/resolved.conf.d/pigate.conf`) อย่างเดียว ไม่เรียก `resolve1.Link.SetDNS` ราย
interface อีก — เดิมมันต้องใช้สิทธิ์ Polkit ราย-link ที่ pigate user ไม่มี จึง fail
(`Permission denied`) ทุกครั้งอยู่แล้ว = resolution ที่ใช้งานได้จริงมาจาก global drop-in 100%
มาตลอด การลบ loop จึงเป็น cleanup ล้วน ไม่เปลี่ยนพฤติกรรมจริง (แต่เอา log noise + call ที่ fail ออก)

---

## Cross-reference: DNS Statistics (2026-07-30)

Not part of this doc's scope (client-side resolution) — the opt-in "Top Queried Domains" /
IP→domain enrichment feature lives entirely in the DNS **Server** (dnsmasq) path. See
`docs/ref/complete/dnsmasq-design.md` ("DNS Statistics — query logging moved to a file")
and `docs/ref/complete/statistics-dns-top-domain-plan.md` for the full design.

---

## Cross-reference: System DNS no longer drives DNS Server's upstream (2026-07-31)

`ApplyDNSConfig` above (§ "Re-apply behavior & idempotency guard") still governs the
device's own resolver (`systemd-resolved` global drop-in) exactly as described — **that
part is unchanged**. What changed is a caller one level up: `HandleUpdateDNSConfig`
(`api/handlers.go`) used to also call `dnsServerService.ApplyAll()` on every System DNS
save, which rewrote `pigate-dns.conf` and restarted dnsmasq as a side effect. That call
has been **removed** — editing System DNS now only affects the device itself, never the
DNS Server dnsmasq gives to LAN clients. DNS Server has its own independent upstream
resolver setting (`DNSServerSettings.UpstreamMode`/`UpstreamServers`); when left at the
default `"system"` mode it reads (not subscribes to) System DNS's current value, but only
at the moment "Apply DNS Zones" is pressed — never automatically on a System DNS save.
See `docs/ref/complete/dns-server-settings-tab-and-upstream-plan.md` and
`docs/ref/complete/dnsmasq-design.md` ("DNS Server upstream resolvers, separated from
System DNS") for the full design.

---

## Cross-reference: DNS Server "Blocked Domains" deny-list (2026-07-31)

Also not part of this doc's scope (client-side resolution) — like DNS Statistics above,
the deny-list lives entirely in the DNS **Server** (dnsmasq) path. Summary (see
`docs/ref/todo/dns-blocked-domains-plan.md` for the full design):

- **Schema**: a new standalone table `dns_blocked_domains` (`id`, `domain` UNIQUE
  COLLATE NOCASE, `mode` — `nxdomain`/`sinkhole`, `enabled`, `comment`, `created_at`) —
  additive, independent from `dns_zones`/`dns_records`.
- **Directive mapping**: each enabled entry is rendered as its own block at the end of
  `pigate-dns.conf`, after all zones. `nxdomain` (default) emits `server=/<domain>/`
  (dnsmasq answers NXDOMAIN for the domain and every subdomain, without forwarding
  upstream). `sinkhole` emits both `address=/<domain>/0.0.0.0` and
  `address=/<domain>/::` (answers a fixed address instead — useful when a client handles
  a fake address better than NXDOMAIN). There is no exact-only mode: blocking a domain
  always blocks its subdomains too, since dnsmasq matches most-specific domain.
  Generation is byte-for-byte unchanged from before this feature when the deny-list is
  empty or every entry is filtered out.
- **Collision with zones**: an entry whose name exactly matches an enabled `dns_zones`
  name is rejected at write time (handler) and skipped again at generation time
  (defense-in-depth) — blocking `pigate.local` or an active local zone name would break
  name resolution for the whole LAN.
- **Apply semantics**: like zones/records, CRUD on blocked domains only writes the DB;
  `pigate-dns.conf` is only regenerated (and dnsmasq restarted) when the user presses
  "Apply DNS Zones" (`POST /dns/apply`), same batching rationale as zone/record edits.

---

## Cross-reference: DNS Query Statistics — domain↔client drill-down (2026-07-31)

Also not part of this doc's scope (client-side resolution) — like DNS Statistics and the
Blocked Domains deny-list above, this lives entirely in the DNS **Server** (dnsmasq)
query-log path built on `DNSServerManager.WatchDNSLog`. Summary (see
`docs/ref/todo/dns-query-statistics-drilldown-plan.md` for the full design):

- **Scope expanded from the original DNS Statistics plan.** `statistics-dns-top-domain-plan.md`
  §0 explicitly ruled out per-client drill-down at the time ("ไม่ทำ per-client drill-down") for
  privacy/RAM reasons. The project owner asked to reopen that scope (2026-07-31); this plan
  implements it, and the "not part of this doc's scope" note in the earlier
  "Cross-reference: DNS Statistics (2026-07-30)" section above no longer means
  "drill-down doesn't exist" — it now points here for the drill-down design.
- **Ring is now keyed by (domain, client) pairs, not just domain.** `service/dns_query_stats.go`'s
  5-minute bucket stores `pairs map[string]map[string]uint64` (domain → client IP → count)
  instead of a bare `domainCount`. Per-domain totals (used by the unchanged "Top Queried
  Domains" card at `/logs/statistics`) are derived by summing every client under a domain, so
  the card and the new drill-down can never disagree.
- **New caps per bucket**: default 2400 pairs / 200 clients (replacing the old
  `maxTrackedDomains = 500`, and raised from an earlier 1200-pair default); RAM worst case
  scales with the cap across the 288-bucket/24h ring (typical home traffic sits far below
  it). `queries` (the total count) is never capped, only new distinct pairs/clients are —
  so totals stay accurate even once `truncated == true`. As of
  docs/ref/todo/dns-stats-tracking-limits-config-plan.md both caps are configurable via the
  file-only bootstrap keys `dns-stats-max-pairs` / `dns-stats-max-clients` in `pigate.conf`
  (no CLI flag by design); `defaultMaxTrackedDNSPairs`/`defaultMaxTrackedDNSClients` in
  `service/dns_query_stats.go` are now just the fallback defaults.
- **Unparseable client IP collapses to the reserved key `"unknown"`** instead of being
  dropped, so domain totals stay accurate; the UI shows it as "ไม่ทราบต้นทาง" but it is still
  drill-down-able like any other client.
- **New endpoints**: `GET /api/statistics/dns`, `GET /api/statistics/dns/domain`,
  `GET /api/statistics/dns/client` — all `authRoute` (any logged-in role, matching the
  privacy level already exposed by the existing `/api/statistics/traffic`), GET-only so
  `-disable-edit=true`/read-only roles can still view them.
- **Still RAM-only, still opt-in.** Same `DNSServerSettings.QueryLogging` switch as before;
  turning it off clears both the per-domain totals and the new per-client pairs immediately
  (`ClearDNSStats()`), and nothing is written to SQLite or disk — restarting `pigate.service`
  resets the ring to empty. No new `db/` schema; `dns_server_settings` is unchanged.
- **UI**: new "สถิติ" tab on the DNS Server page (two ranked tables — Domain Query Stats,
  Source Hosts — plus a Dialog for drill-down in either direction), alongside the existing
  Zones/Blocked Domains/Settings tabs.

---

## Cross-reference: DNS Server "Blocklists" (bulk import) (2026-08-23)

Also lives entirely in the DNS **Server** (dnsmasq) path, alongside the deny-list above —
this is a *separate* feature, not a replacement for it. Full design/rationale:
`docs/ref/todo/dns-blocklist-import-plan.md`. Summary:

### Two render mechanisms, selected per list by `blockMode`

Every list a user adds — subscribed URL or uploaded file — is assigned a `blockMode`
(`sinkhole` or `nxdomain`, reusing the exact same `model.DNSBlockModeSinkhole` /
`model.DNSBlockModeNXDomain` constants as the deny-list, not a new set) that decides which
dnsmasq mechanism renders it:

| `blockMode` | directive emitted in `pigate-dns.conf` | generated file | subdomains covered? | relative cost |
|---|---|---|---|---|
| `sinkhole` (**default for a new blocklist**) | `addn-hosts=/var/lib/pigate/blocklists/<id>.hosts` | `<id>.hosts` — plain hosts records, `0.0.0.0 <domain>` per line | **No** — hosts-file matching is exact-name only | cheapest: dnsmasq never parses `addn-hosts` during `--test`, only at process start, and looks entries up in a hash table at query time |
| `nxdomain` | `conf-file=/var/lib/pigate/blocklists/<id>.conf` | `<id>.conf` — `address=/<domain>/` (no IP) per line | **Yes** — dnsmasq's man page: `address=/<domain>/` with no address "returns a no-such-domain answer… for `<domain>` and all its subdomains", matched on complete labels | more expensive: `conf-file` is parsed **twice per Apply** (once during `dnsmasq --test`, once again at process start), see cost table below |

Why `nxdomain` uses `address=/<domain>/` and not `server=/<domain>/` (which the deny-list
uses and gives an equivalent per-domain result): dnsmasq's CHANGELOG shows the two code
paths scale differently at bulk sizes — 2.86 made domain lookup for `address=`-style
matching grow as O(log₂ n) instead of worse-than-linear, while a later release notes
`--server` option parsing itself can be O(n²) at "thousands of options". At 90k+ domains
that gap matters; at the deny-list's ≤1000-row scale it doesn't, so the deny-list is
deliberately left on `server=` unchanged.

Estimated cost of the two modes at 93,515 domains (StevenBlack unified list; see
plan §2.1.3 for the full derivation — **these are estimates from documentation, not yet
measured on real hardware**, see "Numbers still to be measured" below):

| | `sinkhole` (`addn-hosts`) | `nxdomain` (`conf-file` + `address=/d/`) |
|---|---|---|
| bytes/domain | ≈31 B (`0.0.0.0 <name>\n`) | ≈33 B (`address=/<name>/\n`) |
| generated file size | ≈2.9 MB | ≈3.1 MB (within 10% of sinkhole — *not* the "tens of MB" an earlier plan draft assumed for a 2-line-per-domain sinkhole render) |
| parsed by `dnsmasq --test`? | No (hosts files are read only at process start) | Yes — parsed once by `--test`, once again at start (2 parses per Apply) |
| lookup cost per query | hash table | O(log₂ n) since dnsmasq 2.86 (~17 steps at 93k) |
| safety net if the file goes missing | `--test` does **not** catch it (Caution 1) | `--test` **does** catch it, because `conf-file` is read while parsing options (Caution 16) |

One domain per line always — `address=/a.com/b.com/c.com/` batching is valid dnsmasq
syntax but the per-line read buffer size for config lines is undocumented, so batching is
deliberately not used until a real board confirms it's safe.

### Why the mode is a property of the *list*, not of a domain inside it

1. The source format (`<ip> <hostname>` hosts lines) carries no per-domain mode column, and
   the IP column is discarded entirely (see next section) — there's nothing to infer a
   per-domain mode from even if we wanted to.
2. The UI intentionally never renders a 93k-row domain table (a browser-hang risk), so
   there is no place for a user to toggle an individual domain's mode.
3. `addn-hosts` and `conf-file` are different file formats; making mode per-domain would
   force every list to always be split across both files, and the per-domain state would
   have to live somewhere — i.e. back in SQLite, which was explicitly rejected for this
   feature (see manifest section below).
4. Users who need per-domain mode control already have it via the pre-existing
   "Blocked Domains" deny-list tab (cap 1000), which also takes precedence over blocklists
   in the statistics classifier.

The accepted asymmetry (`sinkhole` never covers subdomains, `nxdomain` always does) is
intentional dnsmasq behavior, not a bug, and is called out directly in the Blocklists tab's
UI copy.

### Why files are re-rendered from scratch instead of forwarded as-is (anti-spoofing)

A public hosts file maps `<hostname> → <ip>` with an arbitrary IP column. If that column
were passed straight to `addn-hosts`, whoever controls the source (or a MITM, or whoever
uploaded the file) could point any domain at any IP — full DNS spoofing. The parser
therefore **always discards the original IP column** and re-renders every accepted domain
as `0.0.0.0 <domain>` in `<id>.hosts`, regardless of the list's `blockMode` — the same
parser feeds both render paths, so there's exactly one place enforcing this. `nxdomain`
mode is inherently even safer here since `address=/<domain>/` has no IP field to spoof at
all, but that's a side effect, not a substitute for the parser doing the stripping.

Both generated files (`<id>.hosts`, `<id>.conf`) are written atomically: `os.CreateTemp` in
the same directory → write → `Sync` → `Chmod(0644)` → `os.Rename`.

### `<id>.hosts` is canonical, `<id>.conf` is a derived file

Every list writes `<id>.hosts` regardless of its `blockMode` — it is the single canonical
store of the list's domain names. `<id>.conf` is written **only** for lists whose
`blockMode == nxdomain`, and is always regenerated from `<id>.hosts` (no re-fetch/re-upload
needed) whenever a list's mode is switched to `nxdomain`. Switching a list back to
`sinkhole` deletes its `<id>.conf`, so an orphaned derived file is never left on disk to be
loaded silently if the directory layout ever changes.

Consequences of this split (chosen so no other part of the feature needs a second domain
parser):
- The statistics index (below) always streams from `<id>.hosts`, for every list, no matter
  its current mode.
- Backup/restore only ships `<id>.hosts` in the backup payload — `<id>.conf` is
  regenerated on import, not carried.
- Mode switches work fully offline, which matters for `upload`-sourced lists that cannot be
  re-fetched.
- Cost: an `nxdomain` list uses roughly double the disk space of an equivalent `sinkhole`
  list (both files present, ≈3 MB + ≈3 MB at 93k domains) — accepted, and it's a one-time
  write per ingest/mode-switch, not per query, so it has no meaningful SD-card wear impact.

### dnsmasq version requirement for `nxdomain`

`nxdomain` mode at blocklist scale depends on dnsmasq ≥ 2.86 ("domain search rewrite" —
sub-linear lookup growth for `address=`-style domain matching; before 2.86, lookup time grew
faster than linear, which would visibly slow down DNS resolution for every client on the
LAN). The kernel layer runs `dnsmasq --version` once (fixed argument, no user input — same
pattern as the existing `--test` call, not a violation of the no-shell-exec-for-user-input
rule) and caches the result; the service layer refuses to **set** a list's mode to
`nxdomain` if the detected version is below 2.86, with a readable error surfaced in the UI.
If the version can't be parsed or detected at all, the check **fails open** (treats it as
supported) — the worst case of guessing wrong here is "slower", not "insecure", so failing
closed and locking working boards out of the feature was judged worse.

### Metadata: single JSON manifest, deliberately not SQLite

`/var/lib/pigate/blocklists/manifest.json` holds every list's metadata (`id`, `name`,
`sourceType`, `url`, `blockMode`, `enabled`, `domainCount`, `fileBytes`, `sha256` of the
rendered `.hosts` file, `lastFetchedAt`, `lastError`, `createdAt`) — `schemaVersion: 1`.
The domain names themselves are **never** held in the manifest, in SQLite, or as an
in-memory `[]string` beyond what it takes to stream-render `<id>.hosts`/`<id>.conf` — the
generated files on disk are the only place they live.

Why JSON instead of adding blocklist tables to the existing SQLite schema (project's usual
source of truth for config): the actual imported *content* (the domain list) already has to
live outside the DB as plain files dnsmasq loads directly, so the DB would only ever hold
small metadata rows pointing at those files — not enough benefit to justify a schema
migration, and it keeps blocklist import fully separable/deletable as one directory. Rules
that make this safe without a database:
- A single `sync.RWMutex` in the kernel-layer store covers the entire read‑modify‑write
  cycle (load → mutate → write), not just the write, so two concurrent refreshes can't
  clobber each other. No `flock` — there is exactly one pigate process writing this
  directory, the same single-writer assumption the SQLite layer already makes.
- Atomic write: marshal (indent 2, deterministic) → temp file in the same directory →
  `Sync` → `Rename`, same pattern as the `.hosts`/`.conf` files above.
- A missing manifest is treated as an empty `schemaVersion: 1` manifest, not an error; a
  `schemaVersion` newer than the binary understands is a hard **fail closed** (refuse to
  write, surface an error, never overwrite a newer file); a manifest that fails to parse is
  quarantined to `manifest.json.corrupt-<timestamp>` and a fresh empty manifest is started,
  so a single corrupt file can't kill the feature permanently.
- The manifest is loaded into RAM once at service construction and every read (e.g. `GET`
  the list) is served from that in-memory cache, never re-reading the file — consistent
  with the project's general SD-wear-avoidance stance for anything that doesn't have to hit
  disk on every read.
- All actual byte-level I/O (manifest bytes, `.hosts`/`.conf` bytes) lives in the kernel
  layer, both `real_*` and `mock` implementations — the service layer only does
  marshal/unmarshal, locking, and business rules, so `-mock=true` never touches the
  filesystem for this feature, matching every other kernel capability in the project.

### Statistics: mode-aware hash index, separate from the deny-list's index

The existing deny-list statistics matcher (`dnsBlockIndex`, `map[string]string`) is left
untouched and keeps serving only the ≤1000-row deny-list. A **separate** index
(`dnsBlocklistIndex`) backs blocklist statistics because the two features have different
match semantics and different scale:
- **Match rule depends on the matching list's `blockMode`**, not a single global rule:
  `sinkhole` lists are matched **exact-name only** (a hosts-file entry for
  `ads.example.com` does not make `sub.ads.example.com` match), `nxdomain` lists are matched
  **by suffix/label-boundary** (mirrors dnsmasq's own `address=/<domain>/` subdomain-covering
  behavior). Applying the wrong rule to either mode would make the Statistics page report
  blocks dnsmasq never performed, or miss ones it did.
- **RAM layout**: each domain contributes one 8-byte FNV-1a 64-bit hash to its list's
  sorted `[]uint64` — never the domain string itself. At the 500,000-domain ceiling
  (`DNSBlocklistMaxTotalDomains`) that's ≈4 MB, versus an estimated ≈30 MB for a
  `map[string]string` keyed by the domain text, which is the structure the deny-list uses
  at its much smaller (≤1000-row) scale. `blockMode` is stored once **per list** (at most
  `DNSBlocklistsMax` = 8 lists), not once per domain, so mode-awareness costs nothing extra
  at the per-domain level. At 500,000 entries the chance of any FNV-1a 64-bit hash
  collision is on the order of `500000² / 2⁶⁵ ≈ 7×10⁻⁹` (birthday bound) — accepted as
  negligible for a statistics feature (a false-positive "blocked" classification here has
  no security consequence, only a cosmetic one).
- **Feed path**: the deny-list index is fed straight from the DB
  (`DNSServerService.SetBlockedDomainsSink`). The blocklist index is instead fed by
  **streaming `<id>.hosts` files back off disk** through the kernel layer
  (`StatisticsService.SetBlocklists`) after a successful `ApplyZones`, never by holding one
  big in-memory `[]string` of the combined domain set.
- If the same domain appears in two lists using different modes, dnsmasq's own internal
  precedence between a hosts record and an `address=/d/` rule decides what it actually
  answers; the statistics classifier may then report a list/mode that doesn't exactly match
  what dnsmasq used. This does not affect security (either path is "blocked"), and is
  accepted as an edge case (the user created it deliberately by importing overlapping
  lists).

### Relationship to the pre-existing deny-list ("Blocked Domains" tab)

Blocklists (this section) and the deny-list (see the "Blocked Domains" cross-reference
above) are two independent features that coexist and both render into the same
`pigate-dns.conf`:

| | Deny-list ("Blocked Domains") | Blocklists (bulk import) |
|---|---|---|
| Storage | SQLite (`dns_blocked_domains`) | JSON manifest + generated files, no SQLite |
| Cap | 1000 domains | 500,000 domains total (8 lists max), 150,000 of which may be `nxdomain` |
| Entry method | Typed in one at a time via the UI | Subscribed URL (backend-fetched) or uploaded hosts file |
| Default `blockMode` | `nxdomain` | `sinkhole` (deliberately the opposite default — see cost table above; a doc comment on both the model and the UI calls this out so the two features aren't assumed to share a default) |
| Mode granularity | Per domain | Per list |
| Directive(s) emitted | `server=/<domain>/` (`nxdomain`) or `address=/<domain>/0.0.0.0` + `address=/<domain>/::` (`sinkhole`) | `conf-file=<id>.conf` with `address=/<domain>/` lines (`nxdomain`) or `addn-hosts=<id>.hosts` (`sinkhole`) |
| Statistics index | `dnsBlockIndex` (`map[string]string`) | `dnsBlocklistIndex` (sorted `[]uint64` per list) |
| Precedence in statistics classification | Takes priority over blocklists | Checked after the deny-list |

Neither feature was modified to accommodate the other — the deny-list's generator, cap, and
byte-for-byte config output when empty are all unchanged; blocklist rendering is strictly
additive at the end of `pigate-dns.conf`.

### Numbers still to be measured on real hardware

The cost table above (file sizes, RSS, Apply timing) is derived from documentation and
napkin math, not measured. Per Caution 1 and Caution 15 of the implementation plan
(`docs/ref/todo/dns-blocklist-import-plan.md` §5), the following still need to be measured
on an actual Raspberry Pi 5 board before being treated as fact (none of this environment's
containers/dev machines are a substitute):

- Wall-clock time of a full Apply (`dnsmasq --test` + restart) for a 93k-domain list in
  `sinkhole` mode vs. `nxdomain` mode.
- `dnsmasq` RSS after start with a 93k-domain list loaded, both modes.
- Whether `dnsmasq --test` genuinely misses a missing/unreadable `addn-hosts` target (as
  assumed in Caution 1) — if it doesn't, that safety-net gap needs to be treated as fixed,
  not just documented.
- Whether `DNSBlocklistMaxNXDomainDomains = 150000` needs to be lowered — the plan's own
  guidance is: if a full Apply of an `nxdomain` list at the cap takes longer than roughly
  5 seconds, the cap is too high and should be reduced, with the reasoning for the new
  number recorded here and in the constant's doc comment
  (`backend/internal/model/dns_blocklist.go`).
- Whether accessing PiGate's own web UI through a domain present in an active `nxdomain`
  blocklist can lock an admin out of the box (Caution 15) — plan for this before testing
  on a board that's the only way to reach the device.

Until these are measured, `DNSBlocklistMaxNXDomainDomains` stays at its current
plan-derived value of 150,000 and should not be assumed final.

## Cross-reference: NS delegation and dnsmasq's CNAME limitation (2026-09-02)

`docs/ref/todo/dns-ns-delegation-cname-fix-plan.md` adds a second **delegation mode**
(`DNSRecord.DelegationMode`, `"glue"` default / `"upstream"`) to the forwarding-based NS
delegation feature (`docs/ref/todo/dns-ns-delegation-plan.md`), to work around a
fundamental limitation of dnsmasq rather than a bug in PiGate's own code.

### dnsmasq cannot merge records from two different sources

dnsmasq is a **forwarder + cache**, not a recursive resolver: it relays a query to one
upstream and returns exactly what that upstream answered, without following a CNAME
chain itself. Simon Kelley (dnsmasq's author), dnsmasq-discuss, 2020q1 ("dnsmasq not
returning A record for a CNAME with a server= config"):

> "All the parts of an answer have to come from the same source ... a CNAME cannot
> point to records which comes from a different server."

Concretely: when an NS record delegates a subtree (`server=/sub.example.net/<glue-ip>`)
and the delegated nameserver answers a query under that subtree with a **CNAME pointing
outside the delegated zone** (out-of-bailiwick — e.g. a CDN alias like
`xxx.pages.dev`), dnsmasq relays that CNAME to the client as-is and does **not** go
fetch the target's A/AAAA itself, because that record would have to come from a
different server than the one that answered the CNAME. The client's stub resolver
doesn't chase the CNAME either, so it ends up with no usable address — even though
plain A/AAAA records under the same delegated subtree resolve correctly, because those
already arrive complete from the delegated nameserver's own answer.

### The fix: an `"upstream"` delegation mode, per NS record

Instead of forwarding straight to the glue IP, `DelegationMode: "upstream"` makes the
generator emit `server=/<fqdn>/#` for that NS record. dnsmasq's `#` is a documented
special server address meaning "use the standard servers" — it hands the subtree to
PiGate's normal upstream resolvers instead of the delegated nameserver directly. Those
upstreams are real recursive resolvers, so they return the CNAME chain **and** its
final A/AAAA in a single answer from a single source, which satisfies dnsmasq's
same-source rule above and the client gets a complete answer.

| mode | `glueIps` | Config emitted | CNAME-out-of-zone behavior |
|---|---|---|---|
| `""` / `glue` (default) | empty | `dns-rr=` only (publish-only) | n/a (no forwarding) |
| `""` / `glue` (default) | set | `dns-rr=` + `server=/<fqdn>/<ip>` per IP | Returns only the CNAME, no A/AAAA (dnsmasq limitation above) |
| `upstream` | empty | `dns-rr=` + `server=/<fqdn>/#` | Fixed — CNAME + A/AAAA in one answer |
| `upstream` | set | `dns-rr=` + `server=/<fqdn>/#` (glue IPs used only for the nameserver's own `host-record=`, not for forwarding) | Fixed |
| any mode, at the zone apex | – | never emits a `server=` line (apex guard, 3 layers: UI, API, generator) | n/a — delegating the whole zone must go through a Forward Zone instead |

Both modes are mutually exclusive **per name**: the generator never emits both
`server=/x/<ip>` and `server=/x/#` for the same fully-qualified name, because dnsmasq
would then pick one non-deterministically per query (or all of them with
`--all-servers`), which is worse than either mode alone.

### Known limitation that remains out of scope

`upstream` mode only works when the delegated name is actually resolvable from
PiGate's normal upstream resolvers (i.e. it has a real public DNS delegation).  A
delegation that exists purely inside PiGate, pointing at a nameserver reachable only on
the LAN, is not helped by this mode — dnsmasq's same-source rule still applies, and
there is currently no CNAME-chasing forwarder in PiGate to work around it (that would
require a new DNS listener component; deliberately out of scope, see plan §7 item 1).
