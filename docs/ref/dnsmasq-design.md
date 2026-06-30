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

**ไฟล์ที่ต้องสร้างใหม่:** `backend/internal/kernel/dhcp.go` — `RealDhcpManager`

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

## ส่วนที่ 4 — Checklist สรุป

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
- [ ] สร้าง `dhcp.go` — `RealDhcpManager` (config writer + interface check + SIGHUP + WatchLeases)
- [ ] อัปเดต `MockDhcp` ใน `mock.go`
- [ ] เพิ่ม `DNSServerManager` interface ใน `interfaces.go`
- [ ] สร้าง `dns_server.go` — `RealDNSServerManager` (zone config writer + ClearCache)
- [ ] สร้าง `MockDNSServerManager` ใน `mock.go`
- [ ] สร้าง `dnsmasq_base.go` — generate `pigate-base.conf`

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

### 🖥️ Frontend
- [ ] `mockData.ts`: อัปเดต `DhcpConfig`, `ActiveDhcpLease`, เพิ่ม DNS types
- [ ] `dhcpService.ts`: อัปเดต methods (multi-config)
- [ ] `DhcpServer.tsx`: อัปเดต UI (multi-interface cards)
- [ ] `dnsServerService.ts`: สร้างใหม่
- [ ] `DnsServer.tsx`: สร้างหน้าใหม่ (ไม่แก้ `DNS.tsx` เดิม)
- [ ] เพิ่ม route navigation สำหรับ `DnsServer.tsx`

