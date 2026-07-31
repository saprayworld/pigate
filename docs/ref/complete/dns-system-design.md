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
- **New caps per bucket**: `maxTrackedDNSPairs = 1200` and `maxTrackedDNSClients = 200`
  (replacing the old `maxTrackedDomains = 500`); RAM worst case is ~40 MB across the
  288-bucket/24h ring (typical home traffic ~7 MB). `queries` (the total count) is never
  capped, only new distinct pairs/clients are — so totals stay accurate even once
  `truncated == true`.
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
