# dnsmasq Integration Design — PiGate

เอกสารนี้อธิบายแผนการพัฒนาระบบ **DHCP Server** และ **DNS Server** โดยใช้ `dnsmasq` เป็น backend daemon สำหรับ PiGate โดยแมปงานทุกอย่างให้ตรงกับโครงสร้างโปรเจคปัจจุบัน

---

## ⚠️ ข้อควรระวัง — DHCP Client vs DHCP Server

ระบบปัจจุบันมี **DHCP Client** อยู่แล้ว ต้องแยกให้ชัดเจนก่อนพัฒนา:

| | DHCP Client (มีอยู่แล้ว) | DHCP Server (สร้างใหม่) |
|---|---|---|
| **หน้าที่** | **ขอรับ IP** จาก upstream router บนพอร์ต WAN | **แจก IP** ให้อุปกรณ์ในวง LAN |
| **Service** | `dhcpcd` (dhcp client daemon) | `dnsmasq` (DHCP server daemon) |
| **โค้ดปัจจุบัน** | `service/dhcpcd.go` → `DhcpcdService` | ยังไม่มี → จะสร้างใหม่ |
| **Trigger** | netlink event (interface up/down) | ผู้ใช้ configure ผ่าน Web UI |
| **Interface** | WAN (เช่น wlan0, eth1) | LAN (เช่น eth0) |

> **🚫 ห้ามแตะโค้ดต่อไปนี้เด็ดขาด**: `service/dhcpcd.go`, `DhcpcdService`, `startDhcpcd()`, `stopDhcpcd()`, `HandleLinkUpdate()`, `SyncActiveInterfaces()` — ถ้าแก้จะทำให้ WAN ขอ IP ไม่ได้

---

## สถานะปัจจุบัน (As-Is)

| ส่วน | สถานะ | หมายเหตุ |
|---|---|---|
| `dhcp_config` table (SQLite) | ✅ มีแล้ว | แต่เป็น **single config** (1 row, id=1) ต่อ 1 interface เท่านั้น |
| `dhcp_reservations` table | ✅ มีแล้ว | |
| `kernel.DhcpManager` interface | ✅ มีแล้ว | `ApplyConfig`, `GetActiveLeases` — ใช้สำหรับ DHCP Server |
| `kernel.MockDhcp` | ✅ มีแล้ว | Mock อ่าน dnsmasq leases file ได้บางส่วน |
| DHCP API handlers + routes | ✅ มีแล้ว | GET/PUT config, reservations, leases, apply |
| `frontend/src/pages/DhcpServer.tsx` | ✅ มีแล้ว | UI พร้อมแล้ว แต่รองรับ **single interface** |
| `frontend/src/services/dhcpService.ts` | ✅ มีแล้ว | |
| `service/dhcpcd.go` → `DhcpcdService` | ✅ มีแล้ว — **อย่าแตะ** | DHCP **Client** สำหรับ WAN เท่านั้น |
| DNS (system resolver) `kernel/dns.go` | ✅ มีแล้ว | จัดการ `systemd-resolved` via D-Bus |
| `frontend/src/pages/DNS.tsx` | ✅ มีแล้ว | System DNS (upstream) ไม่ใช่ DNS Server |
| `RestartServiceViaDBus()` ใน `kernel/dns.go` | ✅ มีแล้ว | reuse ได้เลย |

---

## ภาพรวม Flow การทำงาน

```
Web UI (React)
    │  REST API (JWT Cookie Auth)
    ▼
Go Backend (Service Layer)
    │                         │
    ▼                         ▼
Database Layer           Kernel/System Layer
(SQLite)                      │                     │
  dhcp_configs (new)     เขียน config file     D-Bus: systemd
  dhcp_reservations      /etc/dnsmasq.d/       org.freedesktop.systemd1
  dns_zones (new)        pigate-dhcp.conf      → RestartUnit("dnsmasq")
  dns_records (new)      pigate-dns.conf       → ReloadUnit("dnsmasq") [SIGHUP]
  dhcp_leases (new)      pigate-base.conf
                              │
                              ▼ reload / SIGHUP
                         dnsmasq process
                              │
                              ▼ D-Bus Signal (subscribe)
                         uk.org.thekelleys.dnsmasq
                         → DhcpLeaseAdded
                         → DhcpLeaseDeleted
                         → DhcpLeaseUpdated
```

---

## ส่วนที่ 1 — DHCP Server

### 1.1 Database Layer — `backend/internal/db/connection.go`

ตาราง `dhcp_config` เดิมเป็น single-row (1 interface) → เพิ่มตารางใหม่สำหรับ multi-interface

```sql
CREATE TABLE IF NOT EXISTS dhcp_configs (
    id          TEXT PRIMARY KEY,
    interface   TEXT NOT NULL UNIQUE,
    enabled     INTEGER DEFAULT 1 CHECK(enabled IN (0, 1)),
    start_ip    TEXT NOT NULL,
    end_ip      TEXT NOT NULL,
    gateway     TEXT NOT NULL,
    netmask     TEXT NOT NULL,
    dns1        TEXT NOT NULL DEFAULT '8.8.8.8',
    dns2        TEXT NOT NULL DEFAULT '1.1.1.1',
    lease_time  INTEGER NOT NULL DEFAULT 86400,
    created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
    updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Cache lease จาก dnsmasq D-Bus events
CREATE TABLE IF NOT EXISTS dhcp_leases (
    mac_address TEXT NOT NULL PRIMARY KEY,
    ip_address  TEXT NOT NULL,
    hostname    TEXT,
    interface   TEXT,
    expires_at  DATETIME
);
```

> **Migration**: ใน `migrate()` ตรวจสอบ `dhcp_config` เดิม → migrate ข้อมูลไป `dhcp_configs` แล้ว drop ตารางเก่า (`GetDHCPConfig()` เดิมยังคงไว้เพื่อ backward compat กับ `HandleExportConfig`)

**`backend/internal/db/repository.go`** — เพิ่ม methods:

```go
func (r *Repository) GetDHCPConfigs() ([]model.DhcpConfig, error)
func (r *Repository) GetDHCPConfigByInterface(iface string) (*model.DhcpConfig, error)
func (r *Repository) CreateDHCPConfig(cfg model.DhcpConfig) error
func (r *Repository) UpdateDHCPConfigByID(cfg model.DhcpConfig) error
func (r *Repository) DeleteDHCPConfig(id string) error
func (r *Repository) ToggleDHCPConfig(id string) error
func (r *Repository) UpsertDHCPLease(lease model.ActiveDhcpLease) error
func (r *Repository) DeleteDHCPLease(macAddress string) error
func (r *Repository) GetDHCPLeases() ([]model.ActiveDhcpLease, error)
func (r *Repository) ClearDHCPLeases() error
```

---

### 1.2 Model Layer — `backend/internal/model/`

```go
// อัปเดต DhcpConfig — เพิ่ม ID field
type DhcpConfig struct {
    ID        string `json:"id"`
    Interface string `json:"interface"`
    Enabled   bool   `json:"enabled"`
    StartIP   string `json:"startIp"`
    EndIP     string `json:"endIp"`
    Gateway   string `json:"gateway"`
    Netmask   string `json:"netmask"`
    Dns1      string `json:"dns1"`
    Dns2      string `json:"dns2"`
    LeaseTime int    `json:"leaseTime"`
}

// อัปเดต ActiveDhcpLease — เพิ่ม Interface
type ActiveDhcpLease struct {
    ID         string `json:"id"`
    IPAddress  string `json:"ipAddress"`
    MacAddress string `json:"macAddress"`
    Hostname   string `json:"hostname"`
    Interface  string `json:"interface"`  // ใหม่
    ExpiresAt  string `json:"expiresAt"`
}
```

---

### 1.3 Kernel/System Layer

**`backend/internal/kernel/interfaces.go`** — อัปเดต `DhcpManager` interface:

```go
type DhcpManager interface {
    ApplyConfig(cfgs []model.DhcpConfig, reservations []model.DhcpReservation) error
    GetActiveLeases() ([]model.ActiveDhcpLease, error)
    ReloadConfig() error
}
```

**ไฟล์ที่ต้องสร้างใหม่:** `backend/internal/kernel/dhcp_server.go` — `RealDhcpManager`

```
ApplyConfig:
1. วนทุก config → ตรวจ /sys/class/net/<iface> หรือ netlink
   ถ้าไม่มี → skip (ตาม user_ref.md)
2. Render → /etc/dnsmasq.d/pigate-dhcp.conf:
   interface=eth0
   dhcp-range=eth0,192.168.1.100,192.168.1.200,24h
   dhcp-option=eth0,3,192.168.1.1
   dhcp-option=eth0,6,8.8.8.8,1.1.1.1
   dhcp-host=AA:BB:...,192.168.1.10,DeviceName
3. เรียก ReloadConfig() → RestartServiceViaDBus("dnsmasq.service")

WatchLeases(ctx, callback):
- subscribe D-Bus: uk.org.thekelleys.dnsmasq
- DhcpLeaseAdded/Deleted/Updated → เรียก callback
- ใช้ godbus/dbus เหมือน kernel/dns.go
```

**`backend/internal/kernel/mock.go`** — อัปเดต `MockDhcp` ให้รับ `[]model.DhcpConfig`, เพิ่ม `ReloadConfig() → no-op`

---

### 1.4 Service Layer — ไฟล์ที่ต้องสร้างใหม่: `backend/internal/service/dhcp_server.go`

> **สร้างใหม่ทั้งหมด — ไม่แตะ `service/dhcpcd.go` เดิมเลย**
> `dhcpcd.go` = DHCP Client สำหรับ WAN → ยังทำงานอยู่ปกติ

```go
// ชื่อ struct สื่อถึง "DHCP Server" ชัดเจน
type DhcpServerService struct { repo *db.Repository; manager kernel.DhcpManager }

func NewDhcpServerService(repo *db.Repository, manager kernel.DhcpManager) *DhcpServerService
func (s *DhcpServerService) ApplyAll() error                             // อ่าน DB → ApplyConfig
func (s *DhcpServerService) InitApplyConfig() error                      // startup
func (s *DhcpServerService) StartLeaseWatcher(ctx context.Context) error // D-Bus → upsert DB
```

---
### 1.5 API Layer

**`backend/internal/api/router.go`** — อัปเดต DHCP routes:

```go
authRoute("GET /api/dhcp/configs",              s.HandleGetDHCPConfigs)
authRoute("POST /api/dhcp/configs",             s.HandleCreateDHCPConfig)
authRoute("PUT /api/dhcp/configs/{id}",         s.HandleUpdateDHCPConfig)
authRoute("DELETE /api/dhcp/configs/{id}",      s.HandleDeleteDHCPConfig)
authRoute("POST /api/dhcp/configs/{id}/toggle", s.HandleToggleDHCPConfig)
authRoute("GET /api/dhcp/interfaces",           s.HandleGetAvailableInterfaces) // ใหม่
// reservations, leases, apply — คงเดิม ✅
```

**`backend/internal/api/handlers.go`** — แก้ไข DHCP section:
- `HandleGetDHCPConfig` → `HandleGetDHCPConfigs` (return `[]DhcpConfig`)
- `HandleUpdateDHCPConfig` → แยกเป็น Create + Update by id
- เพิ่ม `HandleDeleteDHCPConfig`, `HandleToggleDHCPConfig`, `HandleGetAvailableInterfaces`
- `HandleApplyDHCP` → เรียก `dhcpService.ApplyAll()`
- `HandleGetDHCPLeases` → อ่านจาก `repo.GetDHCPLeases()`

---

### 1.6 main.go — `backend/cmd/pigate/main.go`

```go
// สร้าง DhcpServerService ใหม่ — ทำงานควบคู่กับ dhcpcdService (ไม่แทนที่)
dhcpServerService := service.NewDhcpServerService(repo, dhcp)
if err := dhcpServerService.InitApplyConfig(); err != nil { ... }
if !*mockOS { go dhcpServerService.StartLeaseWatcher(monitorCtx) }

// dhcpcdService ยังคงอยู่และทำงานตามปกติ — ไม่แตะ
dhcpcdService := service.NewDhcpcdService(repo, ifaceService)  // ✅ คงเดิม
dhcpcdService.SyncActiveInterfaces()                            // ✅ คงเดิม
```

แก้ไข `HandleGetSystemServices` ใน `handlers.go`: เพิ่ม `"dnsmasq"` เข้าไปใน service list (ไม่ลบ entry เดิม)

---

### 1.7 Frontend

**`frontend/src/data-mockup/mockData.ts`**
- เพิ่ม `id` field ใน `DhcpConfig`, เพิ่ม `interface` field ใน `ActiveDhcpLease`
- เปลี่ยน `initialDhcpConfig` (single) → `initialDhcpConfigs` (array)

**`frontend/src/services/dhcpService.ts`**
- เพิ่ม `getConfigs()`, `createConfig()`, `deleteConfig()`, `toggleConfig()`, `getAvailableInterfaces()`
- อัปเดต `updateConfig(id, cfg)` ให้ใช้ path `/dhcp/configs/{id}`

**`frontend/src/pages/DhcpServer.tsx`**
- `state`: เปลี่ยน `config` (single) → `configs` (array)
- แสดง config แต่ละ interface เป็น Card พร้อม toggle ต่อแถว
- ปุ่ม "Add Interface Config" → Dialog dropdown เลือก interface จาก `/api/dhcp/interfaces`
  (แสดงเฉพาะ interface ที่มีอยู่จริงใน OS และยังไม่มี config)
- Active Leases table: เพิ่ม column `Interface`

---


## ส่วนที่ 2 — DNS Server (dnsmasq Authoritative/Forward Mode)

> **สำคัญ**: ส่วนนี้คือ **DNS Server** สำหรับจัดการ local zones เช่น `host1.pigate.local`
> ต่างจาก `DNS.tsx` เดิมซึ่งเป็น System DNS upstream resolver → **ไม่ต้องแก้ DNS.tsx เดิม**

### 2.1 Database Layer — `backend/internal/db/connection.go`

```sql
CREATE TABLE IF NOT EXISTS dns_zones (
    id               TEXT PRIMARY KEY,
    zone_name        TEXT NOT NULL UNIQUE,
    forward_to       TEXT,
    allowed_ips      TEXT,
    is_authoritative INTEGER DEFAULT 1 CHECK(is_authoritative IN (0, 1)),
    enabled          INTEGER DEFAULT 1 CHECK(enabled IN (0, 1)),
    created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE IF NOT EXISTS dns_records (
    id         TEXT PRIMARY KEY,
    zone_id    TEXT NOT NULL,
    name       TEXT NOT NULL,
    type       TEXT NOT NULL CHECK(type IN ('A','AAAA','CNAME','MX','TXT','PTR')),
    value      TEXT NOT NULL,
    ttl        INTEGER DEFAULT 300,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (zone_id) REFERENCES dns_zones(id) ON DELETE CASCADE
);
```

**`backend/internal/db/repository.go`** — เพิ่ม methods:

```go
func (r *Repository) GetDNSZones() ([]model.DNSZone, error)
func (r *Repository) GetDNSZoneByID(id string) (*model.DNSZone, error)
func (r *Repository) CreateDNSZone(zone model.DNSZone) error
func (r *Repository) UpdateDNSZone(zone model.DNSZone) error
func (r *Repository) DeleteDNSZone(id string) error
func (r *Repository) ToggleDNSZone(id string) error
func (r *Repository) GetDNSRecordsByZone(zoneID string) ([]model.DNSRecord, error)
func (r *Repository) GetDNSRecordByID(id string) (*model.DNSRecord, error)
func (r *Repository) CreateDNSRecord(record model.DNSRecord) error
func (r *Repository) UpdateDNSRecord(record model.DNSRecord) error
func (r *Repository) DeleteDNSRecord(id string) error
```

---

### 2.2 Model Layer — ไฟล์ที่ต้องสร้างใหม่: `backend/internal/model/dns_server.go`

```go
type DNSZone struct {
    ID string; ZoneName string; ForwardTo string; AllowedIPs string
    IsAuthoritative bool; Enabled bool; Records []DNSRecord
}
type DNSRecord struct {
    ID string; ZoneID string; Name string; Type string; Value string; TTL int
}
type DNSZoneInput struct {
    ZoneName string; ForwardTo string; AllowedIPs string
    IsAuthoritative bool; Enabled bool
}
type DNSRecordInput struct { Name string; Type string; Value string; TTL int }
```

---

### 2.3 Kernel/System Layer

**`backend/internal/kernel/interfaces.go`** — เพิ่ม interface:

```go
type DNSServerManager interface {
    ApplyZones(zones []model.DNSZone) error
    ClearCache() error
}
```

**ไฟล์ที่ต้องสร้างใหม่:** `backend/internal/kernel/dns_server.go` — `RealDNSServerManager`

```
ApplyZones:
1. Generate /etc/dnsmasq.d/pigate-dns.conf:
   # Authoritative zone
   auth-zone=pigate.local
   address=/host1.pigate.local/172.24.29.22
   cname=service1.pigate.local,host1.pigate.local
   # Forward zone
   server=/home.sapray.net/8.8.8.8
2. RestartServiceViaDBus("dnsmasq.service") จาก kernel/dns.go

ClearCache: D-Bus call uk.org.thekelleys.dnsmasq → ClearCache
```

dnsmasq directive mapping: A→`address=/fqdn/ip`, CNAME→`cname=fqdn,target`,
MX→`mx-host=zone,target,prio`, TXT→`txt-record=fqdn,"text"`, PTR→`ptr-record=rev,fqdn`

**`backend/internal/kernel/mock.go`** — เพิ่ม `MockDNSServerManager` (no-op)

---

### 2.4 Service Layer — ไฟล์ที่ต้องสร้างใหม่: `backend/internal/service/dns_server.go`

```go
type DNSServerService struct { repo *db.Repository; manager kernel.DNSServerManager }
func NewDNSServerService(repo *db.Repository, manager kernel.DNSServerManager) *DNSServerService
func (s *DNSServerService) ApplyAll() error
func (s *DNSServerService) InitApplyConfig() error
```

---

### 2.5 API Layer

**`backend/internal/api/router.go`** — เพิ่ม DNS Server routes:

```go
authRoute("GET /api/dns/zones",               s.HandleGetDNSZones)
authRoute("POST /api/dns/zones",              s.HandleCreateDNSZone)
authRoute("PUT /api/dns/zones/{id}",          s.HandleUpdateDNSZone)
authRoute("DELETE /api/dns/zones/{id}",       s.HandleDeleteDNSZone)
authRoute("POST /api/dns/zones/{id}/toggle",  s.HandleToggleDNSZone)
authRoute("GET /api/dns/zones/{id}/records",  s.HandleGetDNSRecords)
authRoute("POST /api/dns/zones/{id}/records", s.HandleCreateDNSRecord)
authRoute("PUT /api/dns/records/{id}",        s.HandleUpdateDNSRecord)
authRoute("DELETE /api/dns/records/{id}",     s.HandleDeleteDNSRecord)
authRoute("POST /api/dns/apply",              s.HandleApplyDNSServer)
```

**`backend/internal/api/handlers.go`** — เพิ่ม DNS Server handlers ทั้งหมด

---

### 2.6 main.go — `backend/cmd/pigate/main.go`

```go
var dnsServer kernel.DNSServerManager
if *mockOS { dnsServer = kernel.NewMockDNSServerManager() } else { dnsServer = kernel.NewRealDNSServerManager() }
dnsServerService := service.NewDNSServerService(repo, dnsServer)
if err := dnsServerService.InitApplyConfig(); err != nil { ... }
server := api.NewServer(..., dnsServerService)
```

---

### 2.7 Frontend

**ไฟล์ที่ต้องสร้างใหม่:** `frontend/src/pages/DnsServer.tsx`

```
UI:
1. Zone List Cards — zone name, type (auth/forward), enabled toggle
2. Zone Form Dialog — Zone Name, Type, Forward To IP, Allowed IPs
3. Records Table per zone — Name, Type dropdown, Value, TTL, Edit/Delete
   Validate: A = valid IP, CNAME = valid hostname
4. Apply button → POST /api/dns/apply
```

**ไฟล์ที่ต้องสร้างใหม่:** `frontend/src/services/dnsServerService.ts`

```typescript
export const dnsServerService = {
    getZones, createZone, updateZone, deleteZone, toggleZone,
    getRecords, createRecord, updateRecord, deleteRecord, apply
}
```

เพิ่ม types DNS ใน `frontend/src/data-mockup/mockData.ts`
เพิ่ม route navigation สำหรับ `DnsServer.tsx` (ไม่แก้ `DNS.tsx` เดิม)

---


## ส่วนที่ 3 — Shared Config (DHCP + DNS ทำงานร่วมกัน)

### 3.1 pigate-base.conf

**ไฟล์ที่ต้องสร้างใหม่:** `backend/internal/kernel/dnsmasq_base.go`

Generate `/etc/dnsmasq.d/pigate-base.conf` ก่อน apply ครั้งแรก:

```
# /etc/dnsmasq.d/pigate-base.conf — Generated by PiGate

# Domain (ดึงจาก system_dns_settings.local_domain)
domain=pigate.local

# เปิดให้ DHCP client hostname ขึ้นทะเบียนใน DNS อัตโนมัติ
# เชื่อมกับ Setting > "Share hostname with dhcp client" ใน user_ref.md
expand-hosts

# DHCP Authoritative
dhcp-authoritative
```

### 3.2 เชื่อมกับ Settings → Hostname

`system_dns_settings.local_domain` ที่ตั้งค่าใน Settings → DNS → Local Domain จะถูกใช้ใน:
- `pigate-base.conf`: `domain=<local_domain>`
- dnsmasq จะ append domain นี้ให้ DHCP hostname โดยอัตโนมัติ

---

## ส่วนที่ 4 — Checklist สรุป (ขั้นตอนทั้งหมดตั้งแต่ต้นจนจบ)

### เตรียมการ
- [ ] สำรองฐานข้อมูล SQLite ก่อนเริ่ม
- [ ] ตรวจสอบเครื่อง (dnsmasq ติดตั้งหรือยัง, พอร์ต 53 ว่างไหม)

### 🗄️ Database
- [ ] เพิ่มตาราง `dhcp_configs` + migration จาก `dhcp_config` เดิม
- [ ] เพิ่มตาราง `dhcp_leases`
- [ ] เพิ่ม repository methods: `GetDHCPConfigs`, `CreateDHCPConfig`, `UpdateDHCPConfigByID`, `DeleteDHCPConfig`, `ToggleDHCPConfig`
- [ ] เพิ่ม repository methods: `UpsertDHCPLease`, `DeleteDHCPLease`, `GetDHCPLeases`, `ClearDHCPLeases`
- [ ] เพิ่มตาราง `dns_zones`
- [ ] เพิ่มตาราง `dns_records`
- [ ] เพิ่ม repository methods: `GetDNSZones`, `CreateDNSZone`, `UpdateDNSZone`, `DeleteDNSZone`, `ToggleDNSZone`
- [ ] เพิ่ม repository methods: `GetDNSRecordsByZone`, `CreateDNSRecord`, `UpdateDNSRecord`, `DeleteDNSRecord`

### 🧠 Model
- [ ] อัปเดต `DhcpConfig` struct เพิ่ม `ID` field
- [ ] อัปเดต `ActiveDhcpLease` struct เพิ่ม `Interface` field
- [ ] สร้างไฟล์ `dns_server.go`: `DNSZone`, `DNSRecord`, `DNSZoneInput`, `DNSRecordInput`

### ⚙️ Kernel/System Layer
- [ ] อัปเดต `DhcpManager` interface ใน `interfaces.go`
- [ ] สร้าง `dhcp_server.go` — `RealDhcpManager` (config writer + interface check + SIGHUP + WatchLeases)
- [ ] อัปเดต `MockDhcp` ใน `mock.go`
- [ ] เพิ่ม `DNSServerManager` interface ใน `interfaces.go`
- [ ] สร้าง `dns_server.go` — `RealDNSServerManager` (zone config writer + ClearCache)
- [ ] สร้าง `MockDNSServerManager` ใน `mock.go`
- [ ] สร้าง `dnsmasq_base.go` — generate `pigate-base.conf`
- [ ] เพิ่มเช็ค interface ชนกับ WAN ก่อน apply DHCP Server config
- [ ] เพิ่ม validate config (`dnsmasq --test`) ก่อนสั่งรีสตาร์ท

### 🔧 Service Layer
- [ ] สร้าง `service/dhcp_server.go` — `DhcpServerService` (**ใหม่ทั้งหมด, ไม่แตะ `dhcpcd.go`**)
- [ ] สร้าง `service/dns_server.go` — `DNSServerService`

### 🌐 API Layer
- [ ] แก้ไข DHCP handlers ใน `handlers.go` (multi-interface)
- [ ] เพิ่ม DNS Server handlers ใน `handlers.go`
- [ ] อัปเดต DHCP routes ใน `router.go`
- [ ] เพิ่ม DNS Server routes ใน `router.go`

### 🚀 main.go
- [ ] เพิ่ม `dhcpServerService` ใหม่ (ไม่แตะ `dhcpcdService` เดิม)
- [ ] เพิ่ม `dnsServerService` + `InitApplyConfig()`
- [ ] เพิ่ม `StartLeaseWatcher` goroutine (non-mock mode)
- [ ] อัปเดต `NewServer()` signature ให้รับ `dhcpServerService` และ `dnsServerService`
- [ ] เพิ่ม `"dnsmasq"` ใน service list ของ `HandleGetSystemServices`

### 🔥 Firewall
- [ ] เปิดพอร์ต 53, 67, 68 บน interface ฝั่ง LAN

### 🖥️ Frontend
- [ ] `mockData.ts`: อัปเดต `DhcpConfig`, `ActiveDhcpLease`, เพิ่ม DNS types
- [ ] `dhcpService.ts`: อัปเดต methods (multi-config)
- [ ] `DhcpServer.tsx`: อัปเดต UI (multi-interface cards)
- [ ] `dnsServerService.ts`: สร้างใหม่
- [ ] `DnsServer.tsx`: สร้างหน้าใหม่ (ไม่แก้ `DNS.tsx` เดิม)
- [ ] เพิ่ม route navigation สำหรับ `DnsServer.tsx`

### 🧪 ทดสอบ
- [ ] ทดสอบ DHCP Server (IP range, lease คงอยู่, reservation, กัน WAN ชน)
- [ ] ทดสอบ DNS Server (ping ชื่อที่ตั้ง, กันชื่อซ้ำ, forward zone, กัน config ผิด)
- [ ] ทดสอบ Integration (รีสตาร์ทเครื่อง, จำลอง service ล่ม)
---

## ส่วนที่ 5 — Pre-check ก่อนเริ่มพัฒนา

ก่อนเริ่มเขียนโค้ดจริง ต้องเช็คสภาพเครื่องก่อน เพราะแผนนี้สมมติว่ามี `dnsmasq` ติดตั้งอยู่แล้ว แต่ยังไม่มีขั้นตอนตรวจสอบ

```bash
# 1. เช็คว่าติดตั้ง dnsmasq หรือยัง
dpkg -l | grep dnsmasq

# 2. เช็คว่า port 53 (DNS) ถูกโปรแกรมอื่นจองอยู่หรือไม่
sudo ss -tulnp | grep :53

# 3. เช็คสถานะ service dnsmasq
systemctl status dnsmasq
```

**ผลตรวจสอบจริงบนเครื่อง Pi ของโปรเจกต์นี้** (บันทึกไว้เป็นหลักฐานอ้างอิง):

```
udp   UNCONN 0      0                            127.0.0.54:53        0.0.0.0:*    users:(("systemd-resolve",pid=1422,fd=19))
udp   UNCONN 0      0                         127.0.0.53%lo:53        0.0.0.0:*    users:(("systemd-resolve",pid=1422,fd=17))
tcp   LISTEN 0      4096                      127.0.0.53%lo:53        0.0.0.0:*    users:(("systemd-resolve",pid=1422,fd=18))
tcp   LISTEN 0      4096                         127.0.0.54:53        0.0.0.0:*    users:(("systemd-resolve",pid=1422,fd=20))
```

**สรุปผล**: `systemd-resolved` จองพอร์ต 53 อยู่จริง แต่จองเฉพาะที่ IP loopback สองตัวคือ `127.0.0.53` และ `127.0.0.54` เท่านั้น (สังเกตจาก `%lo` คือผูกกับ interface `lo` เพียงตัวเดียว ไม่ใช่ `0.0.0.0` ที่แปลว่าเปิดกว้างทุก interface)

ผลนี้แปลว่า **ไม่ชนกัน** ตราบใดที่ dnsmasq ถูกตั้งค่าให้ฟังที่ IP ของวง LAN โดยตรง (เช่น `192.168.1.1:53`) ไม่ใช่ IP loopback

> **กฎที่ต้องใส่ไว้ใน config เสมอ**: ระบุ `interface=` และ `bind-interfaces` ชี้ไปที่ IP วง LAN โดยตรง ห้ามปล่อยให้ dnsmasq ฟังแบบเปิดกว้างทุกที่อยู่ เพื่อป้องกันไม่ให้ไปชนกับ systemd-resolved ในอนาคต แม้ผลตรวจตอนนี้จะไม่ชนก็ตาม
>
> ```
> # /etc/dnsmasq.d/pigate-base.conf
> interface=eth0
> bind-interfaces
> ```

---

## ส่วนที่ 6 — ป้องกันความขัดแย้งระหว่าง DHCP Client (WAN) กับ DHCP Server (LAN)

นี่คือความเสี่ยงที่สำคัญที่สุดของแผนนี้ ต้องมีการเช็คก่อน apply config ทุกครั้ง

**กฎที่ต้องบังคับใช้ในโค้ด**: interface ที่จะตั้งเป็น DHCP Server ต้อง**ไม่ใช่** interface เดียวกับที่ `dhcpcd.go` (DHCP Client ฝั่ง WAN) กำลังทำงานอยู่

```go
// เพิ่มใน ApplyConfig() ของ RealDhcpManager
// backend/internal/kernel/dhcp.go

func (m *RealDhcpManager) ApplyConfig(cfgs []model.DhcpConfig, reservations []model.DhcpReservation) error {
    activeWanIfaces := m.dhcpcdService.GetActiveInterfaces() // ต้อง expose method นี้จาก DhcpcdService

    for _, cfg := range cfgs {
        if contains(activeWanIfaces, cfg.Interface) {
            return fmt.Errorf("interface %s ถูกใช้เป็น WAN (DHCP Client) อยู่ ไม่สามารถตั้งเป็น DHCP Server ได้", cfg.Interface)
        }
    }
    // ... เขียน config ต่อ
}
```

**สถานการณ์ที่ต้องป้องกัน**: ถ้าตั้งผิดให้ interface ฝั่ง WAN กลายเป็น DHCP Server ไปด้วย เครื่องจะกลายเป็นทั้งคนขอ IP และคนแจก IP บนสายเดียวกัน ทำให้เน็ตทั้งวงหลุดพร้อมกัน

---

## ส่วนที่ 7 — สำรองฐานข้อมูลก่อน Migration

ตาราง `dhcp_config` เดิม (single-row) จะถูก migrate ไปตารางใหม่ `dhcp_configs` แล้ว drop ตารางเก่าทิ้ง ขั้นตอนนี้เสี่ยงเสียข้อมูล ต้องสำรองก่อนทุกครั้ง

```go
// backend/internal/db/connection.go
// เพิ่มก่อนเรียก migrate()

func backupDatabase(dbPath string) error {
    timestamp := time.Now().Format("20060102-150405")
    backupPath := fmt.Sprintf("%s.backup-%s", dbPath, timestamp)
    return copyFile(dbPath, backupPath)
}
```

**ขั้นตอนบังคับ**:
1. `backupDatabase()` ก่อนรัน migration ทุกครั้ง
2. รัน migration
3. ถ้า migration fail → log error และหยุดทันที ห้ามลบตารางเก่าจนกว่าจะยืนยันว่าข้อมูลใหม่ถูกต้อง
4. เก็บไฟล์ backup ไว้อย่างน้อย 7 วันก่อนลบทิ้ง

---

## ส่วนที่ 8 — ตรวจสอบ config ก่อนสั่งรีสตาร์ท (Validation)

ก่อนเขียนทับไฟล์ config จริงที่ dnsmasq ใช้งานอยู่ ต้องทดสอบ syntax ก่อน เพื่อไม่ให้ dnsmasq ล่มทั้งระบบเพราะ config ผิดแค่จุดเดียว

```go
// backend/internal/kernel/dhcp.go และ dns_server.go
// เพิ่มก่อนเรียก RestartServiceViaDBus()

func validateDnsmasqConfig(tmpConfigPath string) error {
    cmd := execCommand("dnsmasq", "--test", "--conf-file="+tmpConfigPath)
    output, err := cmd.CombinedOutput()
    if err != nil {
        return fmt.Errorf("dnsmasq config ไม่ถูกต้อง: %s", string(output))
    }
    return nil
}
```

**ขั้นตอน**:
1. เขียน config ลงไฟล์ชั่วคราวก่อน (ไม่ใช่ไฟล์จริงใน `/etc/dnsmasq.d/`)
2. รัน `validateDnsmasqConfig()` เช็ค syntax
3. ผ่าน → ค่อยเขียนทับไฟล์จริงแล้วสั่งรีสตาร์ท
4. ไม่ผ่าน → คืน error กลับไปหน้าเว็บ ไม่แตะไฟล์จริง

---

## ส่วนที่ 9 — Firewall Rules ที่ต้องเปิด

ระบบมี Firewall (nftables) อยู่แล้ว ต้องเปิดพอร์ตต่อไปนี้บน interface ฝั่ง LAN ไม่งั้น dnsmasq ทำงานถูกต้องแต่ใช้งานจริงไม่ได้

| Protocol | Port | ใช้สำหรับ |
|---|---|---|
| UDP/TCP | 53 | DNS query/response |
| UDP | 67 | DHCP server (รับ request จาก client) |
| UDP | 68 | DHCP client (ฝั่งอุปกรณ์ที่ขอ IP) |

**Checklist เพิ่ม**:
- [ ] เพิ่มกฎ firewall เปิดพอร์ต 53, 67, 68 เฉพาะ interface ฝั่ง LAN ที่เปิดใช้ DHCP/DNS Server
- [ ] ตรวจสอบว่ากฎนี้ผูกกับ interface ที่ถูก enable จริงเท่านั้น ไม่เปิดทิ้งไว้ทุก interface

---

## ส่วนที่ 10 — แผนการทดสอบ (Testing Checklist)

### ทดสอบ DHCP Server
- [ ] ต่ออุปกรณ์ทดสอบเข้า LAN แล้วตรวจว่าได้ IP อยู่ใน range ที่ตั้งไว้
- [ ] ปิด-เปิด config แล้วตรวจว่า lease เดิมยังอยู่ (ไม่หลุด)
- [ ] ทดสอบ reservation (จอง IP ตาม MAC) ว่าได้ IP ตรงตามที่ตั้งจริง
- [ ] ทดสอบตั้งค่า DHCP Server บน interface ที่เป็น WAN อยู่ → ต้อง reject พร้อม error message ชัดเจน

### ทดสอบ DNS Server
- [ ] ping ชื่อที่ตั้งไว้ (เช่น `camera.pigate.local`) จากเครื่องอื่นในวง LAN
- [ ] ทดสอบตั้งชื่อซ้ำในโซนเดียวกัน → ต้องแจ้ง error ไม่ใช่บันทึกซ้ำ
- [ ] ทดสอบ forward zone → ยิง query ไปยัง upstream DNS ที่ตั้งไว้ได้จริง
- [ ] ใส่ config ผิดรูปแบบ (เช่น IP ผิด) แล้วตรวจว่าระบบ reject ก่อนเขียนทับไฟล์จริง (ตามส่วนที่ 8)

### ทดสอบ Integration
- [ ] รีสตาร์ทเครื่อง Pi ทั้งเครื่อง แล้วตรวจว่า DHCP Server และ DNS Server กลับมาทำงานเองอัตโนมัติ
- [ ] จำลอง dnsmasq service ล่มกลางทาง แล้วตรวจว่าระบบแจ้งเตือนผ่านหน้า Dashboard

---

## ส่วนที่ 11 — Checklist สรุปเพิ่มเติม

### ความปลอดภัยและการป้องกันข้อมูลสูญหาย
- [ ] เพิ่ม `backupDatabase()` ก่อนรัน migration (ส่วนที่ 7)
- [ ] เพิ่มการเช็ค interface ชนกับ WAN ก่อน apply DHCP Server config (ส่วนที่ 6)
- [ ] เพิ่ม `validateDnsmasqConfig()` ก่อนเขียนทับไฟล์จริงทุกครั้ง (ส่วนที่ 8)

### Firewall
- [ ] เพิ่มกฎเปิดพอร์ต 53, 67, 68 บน interface ฝั่ง LAN (ส่วนที่ 9)

### Pre-check
- [ ] เขียนสคริปต์ตรวจสอบ dnsmasq ติดตั้งและพอร์ต 53 ว่างก่อนเริ่มใช้งานจริง (ส่วนที่ 5)

### Testing
- [ ] ทำตาม Testing Checklist ทั้งหมดในส่วนที่ 10 ก่อนปิดงาน

---
 
## ส่วนที่ 12 — รายชื่อไฟล์ทั้งหมด
 
### ไฟล์ที่ต้องแก้ไขของเดิม
 
| ไฟล์ | สิ่งที่ต้องทำ |
|---|---|
| `backend/internal/db/connection.go` | เพิ่มตาราง `dhcp_configs`, `dhcp_leases`, `dns_zones`, `dns_records` และเขียน migration ย้ายข้อมูลจากตารางเดิม |
| `backend/internal/db/repository.go` | เพิ่มฟังก์ชันอ่าน/เขียนข้อมูลของทั้ง 4 ตารางใหม่ |
| `backend/internal/model/*` | เพิ่ม field `ID` ใน `DhcpConfig` และ field `Interface` ใน `ActiveDhcpLease` |
| `backend/internal/kernel/interfaces.go` | เพิ่มเมธอดใหม่ใน `DhcpManager` และเพิ่ม interface ใหม่ `DNSServerManager` |
| `backend/internal/kernel/mock.go` | อัปเดต `MockDhcp` ให้รองรับหลาย config และเพิ่ม `MockDNSServerManager` ตัวใหม่ |
| `backend/internal/api/router.go` | เพิ่ม route สำหรับ DHCP Server แบบหลาย interface และ DNS Server ทั้งหมด |
| `backend/internal/api/handlers.go` | เพิ่มฟังก์ชันรับ-ส่งข้อมูลหน้าเว็บสำหรับทั้ง DHCP Server และ DNS Server |
| `backend/cmd/pigate/main.go` | เพิ่มการเรียกใช้ service ใหม่ตอนเปิดโปรแกรม โดยไม่แตะของเดิม |
| `frontend/src/data-mockup/mockData.ts` | เพิ่มข้อมูลตัวอย่างของ DHCP หลาย interface และ DNS |
| `frontend/src/services/dhcpService.ts` | เพิ่มฟังก์ชันเรียก API ใหม่สำหรับจัดการหลาย config |
| `frontend/src/pages/DhcpServer.tsx` | ปรับหน้าจอให้แสดงและจัดการได้หลาย interface พร้อมกัน |
 
### ไฟล์ที่ต้องสร้างใหม่
 
| ไฟล์ | สิ่งที่ต้องทำ |
|---|---|
| `backend/internal/model/dns_server.go` | โครงสร้างข้อมูล `DNSZone` และ `DNSRecord` |
| `backend/internal/kernel/dhcp_server.go` | ตัวเขียน config ไฟล์ dnsmasq และสั่งรีสตาร์ทผ่าน D-Bus สำหรับ DHCP |
| `backend/internal/kernel/dns_server.go` | ตัวเขียน config โซน DNS และสั่งรีสตาร์ทผ่าน D-Bus |
| `backend/internal/kernel/dnsmasq_base.go` | สร้างไฟล์ config พื้นฐานที่ทั้ง DHCP และ DNS ใช้ร่วมกัน |
| `backend/internal/service/dhcp_server.go` | เชื่อมฐานข้อมูลกับตัวจัดการ DHCP Server |
| `backend/internal/service/dns_server.go` | เชื่อมฐานข้อมูลกับตัวจัดการ DNS Server |
| `frontend/src/services/dnsServerService.ts` | ฟังก์ชันเรียก API ของ DNS Server จากหน้าเว็บ |
| `frontend/src/pages/DnsServer.tsx` | หน้าจอใหม่สำหรับจัดการโซน DNS และรายการชื่อ |
 
### ไฟล์ที่ห้ามแตะเด็ดขาด
 
| ไฟล์ | เหตุผล |
|---|---|
| `backend/internal/service/dhcpcd.go` | ระบบขอ IP ฝั่ง WAN เดิม ถ้าแก้จะทำให้เน็ตหลุด |
| `frontend/src/pages/DNS.tsx` | หน้าตั้งค่า DNS ต้นทางเดิม คนละหน้าที่กับ DNS Server ตัวใหม่ |
 
