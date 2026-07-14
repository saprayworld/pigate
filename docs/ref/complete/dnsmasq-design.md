# dnsmasq Integration Design — PiGate

เอกสารนี้อธิบายแผนการพัฒนาระบบ **DHCP Server** และ **DNS Server** โดยใช้ `dnsmasq` เป็น backend daemon สำหรับ PiGate โดยแมปงานทุกอย่างให้ตรงกับโครงสร้างโปรเจคปัจจุบัน

> 📌 **อัปเดตสถานะล่าสุด**: ดู [ส่วนที่ 13 — Progress Update](#ส่วนที่-13--progress-update-2026-07-02) ท้ายเอกสารสำหรับสถานะการพัฒนาจริง เทียบกับแผนในเอกสารนี้ รวมถึงสิ่งที่ทำเพิ่มนอกแผนและช่องว่างที่ยังเหลืออยู่

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

## สถานะปัจจุบัน (As-Is ณ ตอนเริ่มแผน)

> หมายเหตุ: ตารางนี้คือภาพ **ก่อนเริ่มพัฒนา**. สำหรับสถานะล่าสุดหลังพัฒนาเสร็จ ดู [ส่วนที่ 13 — Progress Update](#ส่วนที่-13--progress-update-2026-07-02)

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
- [x] สำรองฐานข้อมูล SQLite ก่อนเริ่ม
- [x] ตรวจสอบเครื่อง (dnsmasq ติดตั้งหรือยัง, พอร์ต 53 ว่างไหม) — ผลตรวจบันทึกไว้ในส่วนที่ 5

### 🗄️ Database
- [x] เพิ่มตาราง `dhcp_configs` + migration จาก `dhcp_config` เดิม
- [x] เพิ่มตาราง `dhcp_leases`
- [x] เพิ่ม repository methods: `GetDHCPConfigs`, `CreateDHCPConfig`, `UpdateDHCPConfigByID`, `DeleteDHCPConfig`, `ToggleDHCPConfig`
- [x] เพิ่ม repository methods: `UpsertDHCPLease`, `DeleteDHCPLease`, `GetDHCPLeases`, `ClearDHCPLeases`
- [x] เพิ่มตาราง `dns_zones`
- [x] เพิ่มตาราง `dns_records`
- [x] เพิ่ม repository methods: `GetDNSZones`, `CreateDNSZone`, `UpdateDNSZone`, `DeleteDNSZone`, `ToggleDNSZone`
- [x] เพิ่ม repository methods: `GetDNSRecordsByZone`, `CreateDNSRecord`, `UpdateDNSRecord`, `DeleteDNSRecord`
- [x] *(เพิ่มเติมนอกแผน)* เพิ่มตาราง `dns_server_settings` + `GetDNSServerInterfaces`/`SetDNSServerInterfaces` — ดูส่วนที่ 13

### 🧠 Model
- [x] อัปเดต `DhcpConfig` struct เพิ่ม `ID` field
- [x] อัปเดต `ActiveDhcpLease` struct เพิ่ม `Interface` field
- [x] สร้างไฟล์ `dns_server.go`: `DNSZone`, `DNSRecord`, `DNSZoneInput`, `DNSRecordInput`
- [x] *(เพิ่มเติมนอกแผน)* `model.DNSServerSettings{Interfaces []string}`

### ⚙️ Kernel/System Layer
- [x] อัปเดต `DhcpManager` interface ใน `interfaces.go`
- [x] สร้าง `dhcp_server.go` — `RealDhcpManager` (config writer + interface check + SIGHUP + WatchLeases)
- [x] อัปเดต `MockDhcp` ใน `mock.go`
- [x] เพิ่ม `DNSServerManager` interface ใน `interfaces.go`
- [x] สร้าง `dns_server.go` — `RealDNSServerManager` (zone config writer + ClearCache)
- [x] สร้าง `MockDNSServerManager` ใน `mock.go`
- [x] สร้าง base config generator (`ensureBaseConfig()` ใน `dhcp_server.go` แทนไฟล์แยก `dnsmasq_base.go`)
- [x] เพิ่มเช็ค interface ชนกับ WAN ก่อน apply DHCP Server config (อยู่ใน `DhcpServerService.ApplyAll()`)
- [x] เพิ่ม validate config (`dnsmasq --test`) ก่อนสั่งรีสตาร์ท (ทั้ง DHCP และ DNS Server)

### 🔧 Service Layer
- [x] สร้าง `service/dhcp_server.go` — `DhcpServerService` (**ใหม่ทั้งหมด, ไม่แตะ `dhcpcd.go`**)
- [x] สร้าง `service/dns_server.go` — `DNSServerService`

### 🌐 API Layer
- [x] แก้ไข DHCP handlers ใน `handlers.go` (multi-interface)
- [x] เพิ่ม DNS Server handlers ใน `handlers.go`
- [x] อัปเดต DHCP routes ใน `router.go`
- [x] เพิ่ม DNS Server routes ใน `router.go`
- [x] *(เพิ่มเติมนอกแผน)* `GET/PUT /api/dns/settings` — ดูส่วนที่ 13

### 🚀 main.go
- [x] เพิ่ม `dhcpServerService` ใหม่ (ไม่แตะ `dhcpcdService` เดิม)
- [x] เพิ่ม `dnsServerService` + `InitApplyConfig()`
- [x] เพิ่ม `StartLeaseWatcher` goroutine (non-mock mode)
- [x] อัปเดต `NewServer()` signature ให้รับ `dhcpServerService` และ `dnsServerService`
- [x] เพิ่ม `"dnsmasq"` ใน service list ของ `HandleGetSystemServices`

### 🔥 Firewall
- [x] เปิดพอร์ต 53 (TCP/UDP) และ 67 (UDP) เฉพาะ interface ที่เปิดใช้ DNS Server / DHCP Server จริงเท่านั้น — ดูรายละเอียดในส่วนที่ 13
- [x] แก้กฎ baseline drop พอร์ต 67 ใน `real_firewall.go` ที่เคย drop แบบไม่มีเงื่อนไข interface ให้เพิ่ม accept-per-interface นำหน้า (พอร์ต 68 ยังคง drop ทุก interface ตามเดิม — ไม่เกี่ยวกับ DHCP Server)

### 🖥️ Frontend
- [x] `mockData.ts`: อัปเดต `DhcpConfig`, `ActiveDhcpLease`, เพิ่ม DNS types
- [x] `dhcpService.ts`: อัปเดต methods (multi-config)
- [x] `DhcpServer.tsx`: อัปเดต UI (multi-interface cards)
- [x] `dnsServerService.ts`: สร้างใหม่
- [x] `DnsServer.tsx`: สร้างหน้าใหม่ (ไม่แก้ `DNS.tsx` เดิม)
- [x] เพิ่ม route navigation สำหรับ `DnsServer.tsx`
- [x] *(เพิ่มเติมนอกแผน)* UI เลือก Listen Interfaces ของ DNS Server จาก Interface Service — ดูส่วนที่ 13

### 🧪 ทดสอบ
- [ ] ทดสอบ DHCP Server (IP range, lease คงอยู่, reservation, กัน WAN ชน) — ยังไม่ได้ทดสอบบนฮาร์ดแวร์จริง
- [ ] ทดสอบ DNS Server (ping ชื่อที่ตั้ง, กันชื่อซ้ำ, forward zone, กัน config ผิด) — ยังไม่ได้ทดสอบบนฮาร์ดแวร์จริง
- [ ] ทดสอบ Integration (รีสตาร์ทเครื่อง, จำลอง service ล่ม) — ยังไม่ได้ทดสอบบนฮาร์ดแวร์จริง
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

> **กฎที่ต้องใส่ไว้ใน config เสมอ**: ระบุ `interface=` (จาก DNS Server Listen Interfaces) เพื่อไม่ให้ dnsmasq ฟังแบบเปิดกว้างทุกที่อยู่ กันชนกับ systemd-resolved — แต่ใช้ `bind-dynamic` แทน `bind-interfaces` (ดูหมายเหตุด้านล่าง)
>
> ```
> # /etc/dnsmasq.d/pigate-base.conf
> bind-dynamic
> # /etc/dnsmasq.d/pigate-dns.conf
> interface=eth0
> ```

### หมายเหตุ: `bind-dynamic` แทน `bind-interfaces` (issue #50, implemented 2026-07-14)

เดิม `pigate-base.conf` ใช้ `bind-interfaces` และการ listen ของ DNS Server ขี่บรรทัด
`interface=` จาก `pigate-dhcp.conf` — ทำให้ **ปิด DHCP Server ทุก interface = DNS Server
ใบ้ทั้งระบบ** และถ้ามีชื่อ interface ที่ไม่มีจริง (เช่น VLAN ที่ parent หาย) อยู่ใน config
ภายใต้ `bind-interfaces` dnsmasq จะ **ล้มทั้งโปรเซส (รวม DHCP)** — `dnsmasq --test`
จับไม่ได้เพราะเช็คแค่ syntax

การแก้ (พิสูจน์บน LAB VM แล้ว):
- base config เปลี่ยนเป็น `bind-dynamic` — ทน `interface=` ที่ชื่อไม่มีจริงได้ (dnsmasq
  จะ bind ให้เองเมื่อ interface โผล่มา โดยไม่ต้อง restart/re-apply = self-healing)
- `pigate-dns.conf` emit `interface=<name>` ต่อ Listen Interface ที่ตั้งในหน้า DNS Server
  โดย**ไม่ skip** ชื่อที่ไม่มีจริง (ต่างจากฝั่ง DHCP ที่ยัง skip อยู่ ลด regression) —
  ทุกชื่อผ่าน `model.ValidateInterfaceName` ก่อน emit (defense-in-depth กัน directive
  injection จาก import path)
- base config ถูก (re)write โดยทั้ง `RealDhcpManager.ApplyConfig` และ
  `RealDNSServerManager.ApplyZones` ผ่าน helper ร่วม `ensureDnsmasqBaseConfig()` เสมอ
  ก่อนเขียน per-role config — ปิด upgrade/ordering trap ที่ base เก่ายังเป็น
  `bind-interfaces` ตอน binary ใหม่ apply DNS
- **ความปลอดภัยมาจาก `bind-dynamic` ไม่ใช่จาก validation** — `dnsmasq --test` จับ
  unknown interface ไม่ได้

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
- [x] เพิ่มกฎ firewall เปิดพอร์ต 53 (TCP/UDP) และ 67 (UDP) เฉพาะ interface ฝั่ง LAN ที่เปิดใช้ DHCP/DNS Server (พอร์ต 68 ไม่ต้องเปิด — DHCP Server เป็นฝั่ง server ไม่ใช่ client บน interface นั้น)
- [x] ตรวจสอบว่ากฎนี้ผูกกับ interface ที่ถูก enable จริงเท่านั้น ไม่เปิดทิ้งไว้ทุก interface — ดูส่วนที่ 13

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

---

## ส่วนที่ 13 — Progress Update (2026-07-02)

สรุปสถานะการพัฒนาจริง เทียบกับแผนในเอกสารนี้ ตามการตรวจโค้ดล่าสุด

### ✅ ทำครบตามแผนแล้ว
Database / Model / Kernel / Service / API / main.go / Frontend ทั้งหมดตาม checklist ในส่วนที่ 4 — โค้ด backend คอมไพล์ผ่าน (`go build`, `go vet`, `go test ./...`) และ frontend ผ่าน `tsc --noEmit` + `yarn build` ทั้งหมด โดยไม่ได้แตะ `service/dhcpcd.go` หรือ `frontend/src/pages/DNS.tsx` เลยตามกฎที่กำหนดไว้

### 🆕 สิ่งที่ทำเพิ่มนอกแผนเดิม

**1. แยก DNS Server Listen Interface ออกจาก DHCP Server**

แผนเดิม (ส่วนที่ 2.3/2.4) ให้ `DNSServerService.ApplyAll()` ดึงรายชื่อ interface สำหรับ `auth-server=` directive มาจาก DHCP configs ที่ enabled อยู่ ซึ่งทำให้ DNS Server ผูกติดกับสถานะของ DHCP Server โดยไม่จำเป็น จึงปรับให้เป็นค่าตั้งค่าอิสระของตัวเอง:

- DB: เพิ่มตาราง `dns_server_settings` (single-row, `id=1`) เก็บรายชื่อ interface แบบ comma-separated
- Repository: `GetDNSServerInterfaces()` / `SetDNSServerInterfaces()`
- Model: `DNSServerSettings{Interfaces []string}`
- API: `GET /api/dns/settings`, `PUT /api/dns/settings` — ฝั่ง `PUT` validate ชื่อ interface กับ `interfaceService.GetDataLayerInterface()` (Interface Service จริง) ก่อนบันทึกเสมอ
- Service: `DNSServerService.ApplyAll()` อ่าน interfaces จาก `repo.GetDNSServerInterfaces()` แทนการ derive จาก `dhcp_configs` (ลบ fallback `eth0` แบบ hardcode ทิ้งด้วย)
- Frontend: การ์ด "DNS Server Listen Interfaces" ใน `DnsServer.tsx` ให้ผู้ใช้ติ๊กเลือก interface จริง (role `LAN`) จาก `interfaceService.getAll()` — ไม่ผูกกับ DHCP Server อีกต่อไป

**2. แก้บั๊ก: nil slice → JSON `null` ทำให้หน้า DHCP Server crash**

พบว่า backend คืน Go slice ที่ไม่ได้ initialize (`nil`) ในหลายจุด ซึ่ง JSON-encode เป็น `null` แทนที่จะเป็น `[]` และฝั่ง frontend เรียก `.length`/`.filter` ต่อจากผลลัพธ์โดยไม่มีการเช็คป้องกัน — จุดที่ร้ายแรงที่สุดคือ `HandleGetAvailableInterfaces` ที่จะคืน `null` เมื่อไม่เหลือ LAN interface ว่าง (เช่น ตั้ง DHCP Server ครบทุก interface แล้ว) ทำให้หน้าเว็บทั้งหน้า crash ตอนโหลด เพราะ `availableInterfaces.length` ถูกอ่านตรง ๆ ใน JSX

แก้โดย:
- Backend: `HandleGetAvailableInterfaces`, `HandleGetDHCPConfigs`, `HandleGetDHCPLeases` คืน `[]` เสมอเมื่อไม่มีข้อมูล
- Backend: `RealDhcpManager.GetActiveLeases()` (`kernel/dhcp_server.go`) initialize slice เป็น `[]model.ActiveDhcpLease{}` แทน `nil`
- Frontend: `DhcpServer.tsx` เพิ่ม `|| []` กันไว้อีกชั้นตอนรับผลลัพธ์จาก API (`loadDhcpData`, `openCreateConfigModal`)

**3. แก้บั๊ก: แก้ไข DNS Zone แล้ว DNS Records ฝั่งขวาหายไป ต้องโหลดหน้าใหม่**

ต้นตออยู่ที่ `HandleUpdateDNSZone` (`api/handlers.go`) — ดึง `existing` (ซึ่งมี `Records` ครบ) มาเช็คว่า zone มีอยู่จริง แต่ตอนสร้าง struct `zone` เพื่อ save และส่งกลับเป็น response กลับไม่ได้ copy `existing.Records` มาด้วย ทำให้ response มี `"records": null` เสมอ (เพราะ field `Records` ใน model ไม่มี `omitempty`) ฝั่ง frontend ที่ `{ ...z, ...updated }` จึงเอา `null` ไปทับ records เดิมในหน้าจอทันที

แก้โดย:
- Backend: เพิ่ม `Records: existing.Records` ใน struct `zone` ก่อน save/ส่งกลับใน `HandleUpdateDNSZone`
- Frontend: `handleSaveZone` ใน `DnsServer.tsx` merge แบบ `{ ...z, ...updated, records: z.records }` เพื่อไม่ให้พึ่งพา records จาก response ของ endpoint ที่แก้แค่ zone metadata

### ⚠️ ช่องว่างที่ยังเหลืออยู่ (ยังไม่ได้แก้)

**Firewall (ส่วนที่ 9) — ความเสี่ยงสำคัญที่สุดที่ยังไม่ได้แก้**

ยังไม่มีการเพิ่มกฎเปิดพอร์ต 53/67/68 บน LAN interface ตามแผน และที่ร้ายแรงกว่านั้นคือใน `backend/internal/kernel/real_firewall.go:186-199` มี baseline rule เดิม:

```go
// udp dport { 137, 138, 67, 68 } drop
for _, port := range []uint16{137, 138, 67, 68} {
    ...
    &expr.Verdict{Kind: expr.VerdictDrop},
}
```

กฎนี้ drop พอร์ต UDP 67/68 แบบไม่มีเงื่อนไข interface ซึ่งจะบล็อก DHCP Server ตัวใหม่ไม่ให้ทำงานได้จริงถ้า apply firewall config นี้ทับ interface ที่เปิด DHCP Server ไว้ — **ต้องแก้ก่อนขึ้นใช้งานจริง** โดยอาจต้อง exclude LAN interface ที่ enable DHCP Server ออกจากกฎ drop นี้ หรือย้ายกฎนี้ไปอยู่หลัง accept rule ของ DHCP Server

**Testing (ส่วนที่ 10) — ยังไม่ได้ทดสอบบนฮาร์ดแวร์จริง**

ทั้งหมดในส่วนที่ 10 ยังเป็น manual testing ที่ต้องทำบน Pi จริง (ต่ออุปกรณ์รับ IP, ping ชื่อ DNS, ทดสอบ interface ชน WAN, restart integration) — โค้ดผ่านแค่ build/test อัตโนมัติเท่านั้น

### 🆕 Firewall (ส่วนที่ 9) — แก้ไขแล้ว (2026-07-02)

แก้ปัญหาที่บันทึกไว้ข้างต้นแล้ว:

- `kernel.FirewallManager.ApplyRules` (`interfaces.go`) เพิ่มพารามิเตอร์ `dhcpServerIfaces []string`, `dnsServerIfaces []string` — ทั้ง `RealFirewall` และ `MockFirewall` อัปเดตตาม
- `real_firewall.go`: เพิ่มกฎ `udp dport 67 iifname <X> accept` ต่อ interface ใน `dhcpServerIfaces` **ก่อน** กฎ drop เดิม (`udp dport {137,138,67,68} drop`) เพราะ nftables ประมวลผลกฎจากบนลงล่างแบบ first-match-wins — กฎ accept ที่แทรกก่อนจะทำให้ traffic ของ interface ที่ได้รับอนุญาตหลุดออกจาก chain ก่อนไปโดนกฎ drop เดิม ส่วน interface อื่นที่ไม่ได้เปิด DHCP Server ยังโดน drop เหมือนเดิม (ป้องกัน rogue DHCP)
- เพิ่มฟังก์ชันใหม่ `addDNSServerAccessRules()` เปิด TCP+UDP port 53 ต่อ interface ใน `dnsServerIfaces` (เดิมไม่มีกฎอะไรเลยสำหรับพอร์ต 53 — ตกไปโดน default DROP policy ท้าย chain)
- `FirewallService.SyncFirewallRules()` (`service/firewall.go`) โหลด enabled DHCP configs (`repo.GetDHCPConfigs()` กรอง `Enabled`) และ `repo.GetDNSServerInterfaces()` มาส่งต่อให้ `ApplyRules`
- **Runtime auto-resync**: `HandleApplyDHCP` และ `HandleApplyDNSServer` (`api/handlers.go`) เรียก `firewallService.SyncFirewallRules()` ต่อท้ายหลัง apply สำเร็จ — เดิมถ้า enable DHCP/DNS Server บน interface ใหม่ผ่านหน้าเว็บ จะไม่มีอะไรไปเปิด firewall ให้จนกว่าจะรีสตาร์ทเครื่อง (เพราะ `firewallService.InitApplyConfig()` รันแค่ตอน startup) ตอนนี้เปิดพอร์ตทันทีที่กด Apply
- พอร์ต 68 **ยังคง** ถูก drop ทุก interface เหมือนเดิม — DHCP Server เป็นฝั่ง server บน LAN ไม่จำเป็นต้องรับพอร์ต 68 เข้ามา
- **ยังไม่ได้แก้ (นอก scope ของรอบนี้)**: มีข้อสังเกตว่ากฎ drop พอร์ต 68 แบบไม่มีเงื่อนไขนี้อาจกระทบ DHCP **Client** ฝั่ง WAN (`dhcpcd`) ด้วย เพราะ conntrack ปกติไม่รู้จัก broadcast DHCP reply ว่าเป็น "related" กับ request เดิม แต่ประเด็นนี้อยู่นอกเอกสารฉบับนี้และแตะพื้นที่ที่ถูกกำหนดห้ามแตะ (`dhcpcd.go`) จึงพักไว้เป็น follow-up แยกที่ต้องทดสอบบนฮาร์ดแวร์จริงก่อนสรุป
- Backend ผ่าน `go build ./...`, `go vet ./...`, `go test ./...` ทั้งหมด — ยังไม่ได้ทดสอบบนฮาร์ดแวร์จริง (เช่น `nft list ruleset` ยืนยันลำดับกฎ, ต่ออุปกรณ์รับ lease จริง, `dig` ไปยัง DNS Server)


### 🔒 Input Validation — กัน config injection (2026-07-12, issue #36)

Security review finding 7 (Medium): ค่าที่ผู้ใช้กรอก (DNS zone name, record name/value,
forward-zone `forwardTo`, DHCP reservation `deviceName`/MAC/IP) เดิมถูกเขียนลง
`pigate-dns.conf` / `pigate-dhcp.conf` ด้วย `fmt.Sprintf` โดยมีแค่ `TrimSpace` — ซึ่ง
**ไม่ตัด newline กลางสตริง** ค่าอย่าง `1.2.3.4\naddress=/evil/6.6.6.6` ฉีด directive ใหม่เข้า
ไฟล์ได้ และ `dnsmasq --test` จับไม่ได้ (บรรทัดที่ฉีดเป็น config ที่ valid)

**การแก้ (whitelist, reject-not-strip) — validate 3 ชั้น:**

- **`backend/internal/model/dns_validate.go`** (ใหม่) — `ValidateDNSZone`, `ValidateDNSRecord`,
  `ValidateReservation`/`ValidateReservationName`: pure function ใน `model` (ไม่มี dep,
  เรียกได้ทุก layer). regex full-match `^...$` — ใน Go `$` = ปลายข้อความ จึง reject `\n`/`\r`
  โดยอัตโนมัติ. per-type: A=IPv4 (`net.ParseIP`+`To4`), AAAA=IPv6, CNAME/PTR=FQDN charset,
  MX=`"<pref> <target>"`/`"<target>"`, TXT≤255 ไม่มี `"`; zone name `^[a-zA-Z0-9.-]+$`;
  reservation name อนุญาต space (writer แปลงเป็น `-`) แต่ reject อักขระคุม; MAC/IP validate ด้วย
- **Handler layer** (`api/handlers.go`) — 6 handler (DNS zone/record create+update, DHCP
  reservation create+update) เรียก validator หลัง decode → ตอบ `400` พร้อม message ชัด
- **Import path** (`service/backup.go` → `validateConfig`) — import เขียน DB ตรงข้าม handler
  จึง validate ทุก DNS/DHCP entry **ก่อน** single-txn restore; ถ้ามีตัวใดพัง reject ทั้ง import
  (fail-closed, DB ไม่เปลี่ยน)
- **Generation-time defense-in-depth** (`kernel/dns_server.go`, `dhcp_server.go`) — loop
  ApplyZones/ApplyConfig ถ้า zone/record/reservation ตัวใด fail validate → **skip + log**
  (ไม่ทำทั้ง apply พังเพราะ 1 entry เสีย) กันค่าที่หลุด DB เก่าไปถึงไฟล์

Mock backend ไม่แตะ (mock DNS/DHCP ไม่เขียนไฟล์ dnsmasq จึงไม่มีผิวฉีด). ทดสอบ:
`model/dns_validate_test.go` (per-type valid/invalid + injection ตรงๆ),
`api/dns_validation_test.go` (handler 400), `service/backup_test.go`
(`TestImportRejectsDnsmasqInjection` — import ถูก reject, DB ไม่เปลี่ยน)
