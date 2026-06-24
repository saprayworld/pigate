# Network Interface — บันทึกการทำงานของระบบ

เอกสารนี้อธิบายกระบวนการทำงานของระบบ **Network Interface** ใน PiGate ตั้งแต่ต้นทาง (OS) ไปจนถึงปลายทาง (Frontend) เพื่อเป็นข้อมูลอ้างอิงสำหรับนักพัฒนา

---

## 1. Data Model (`NetworkInterface`)

ไฟล์: `backend/internal/model/types.go`

```go
type NetworkInterface struct {
    ID                   string   // รหัสเฉพาะ (เช่น "iface-1", "iface-os-2")
    Name                 string   // ชื่อ OS (เช่น "eth0", "wlan0")
    Alias                string   // ชื่อแสดงผล (เช่น "LAN_Internal")
    Role                 string   // "LAN" หรือ "WAN"
    Type                 string   // "ethernet" หรือ "wireless"
    AddressingMode       string   // "static" หรือ "dhcp"
    IP                   string   // IPv4 address
    Netmask              string   // CIDR prefix (เช่น "24")
    Gateway              string   // Default gateway IP
    MacAddress           string   // MAC address (อาจเป็น hardware หรือ randomized)
    AdminAccess          []string // ["PING", "HTTP", "HTTPS", "SSH"]
    Status               string   // "up" หรือ "down"
    Speed                string   // เช่น "1000 Mbps", "1 Gbps", "unknown"

    // Wireless-only fields (optional/nullable)
    ConnectedSSID        *string
    WifiPassword         *string
    WifiSecurity         *string
    MacMode              *string  // "hardware", "randomized", "laa"
    RealMacAddress       *string
    RandomizedMac        *string
    LaaMacAddress        *string
    RandomizeOnReconnect *bool
    FailoverEnabled      *bool
    BackupSSID           *string
    BackupWifiPassword   *string
    IPCheckTimeout       *int
    PrimaryMaxRetries    *int
    FailoverCooldown     *int
}
```

---

## 2. Database Schema (`network_interfaces`)

ไฟล์: `backend/internal/db/connection.go`

```sql
CREATE TABLE IF NOT EXISTS network_interfaces (
    id                      TEXT PRIMARY KEY,
    name                    TEXT UNIQUE NOT NULL,
    alias                   TEXT NOT NULL,
    role                    TEXT NOT NULL CHECK(role IN ('LAN', 'WAN')),
    type                    TEXT NOT NULL CHECK(type IN ('ethernet', 'wireless')),
    addressing_mode         TEXT NOT NULL CHECK(addressing_mode IN ('dhcp', 'static')),
    ip                      TEXT NOT NULL,
    netmask                 TEXT NOT NULL,
    gateway                 TEXT NOT NULL,
    mac_address             TEXT NOT NULL,
    admin_access            TEXT NOT NULL,  -- comma separated: "PING,HTTP,SSH"
    status                  TEXT NOT NULL CHECK(status IN ('up', 'down')),
    speed                   TEXT NOT NULL,
    -- Wireless-specific optional fields
    connected_ssid          TEXT,
    wifi_password           TEXT,
    wifi_security           TEXT,
    mac_mode                TEXT CHECK(mac_mode IN ('hardware', 'randomized', 'laa')),
    real_mac_address        TEXT,
    randomized_mac          TEXT,
    laa_mac_address         TEXT,
    randomize_on_reconnect  INTEGER DEFAULT 0,
    failover_enabled        INTEGER DEFAULT 0,
    backup_ssid             TEXT,
    backup_wifi_password    TEXT,
    ip_check_timeout        INTEGER,
    primary_max_retries     INTEGER,
    failover_cooldown       INTEGER
);
```

### Seed data เริ่มต้น (ถ้า DB ว่าง)

| ID | Name | Role | Type | Mode |
|---|---|---|---|---|
| `iface-1` | `eth0` | LAN | ethernet | static, 192.168.1.1/24 |
| `iface-2` | `wlan0` | WAN | wireless | dhcp, 10.0.0.45/24 |

---

## 3. การ Sync ข้อมูลจาก OS

ไฟล์: `backend/internal/db/repository.go` — ฟังก์ชัน `SyncInterfacesFromOS()`

### เมื่อไหร่ถูกเรียก

ทุกครั้งที่มีการเรียก `GET /api/interfaces` (ถ้าไม่อยู่ใน mock-only mode)

```go
func (r *Repository) GetInterfaces() ([]model.NetworkInterface, error) {
    if !r.mockMode {
        _ = r.SyncInterfacesFromOS()
    }
    // ... query DB ...
}
```

### Logic ของ SyncInterfacesFromOS

1. เรียก `net.Interfaces()` เพื่อดึง list interface จาก kernel
2. Skip `loopback` (flag `FlagLoopback`) และ interface ชื่อขึ้นต้นด้วย `lo`
3. ดึง IP/Netmask จาก `net.Interface.Addrs()` — เลือกเฉพาะ IPv4
4. อ่าน MAC address จาก `net.Interface.HardwareAddr`
5. ตรวจ status จาก flag `FlagUp`

#### กรณี interface **มีอยู่แล้วใน DB**
→ `UPDATE` เฉพาะ **dynamic fields** เท่านั้น: `ip, netmask, mac_address, status`  
→ **ไม่แตะ** `addressing_mode` และค่าที่ผู้ใช้ตั้งไว้ (เพื่อรักษาการ configure ของ user)

#### กรณี interface **ยังไม่มีใน DB** (ใหม่)
→ `INSERT` ใหม่โดยอ่านค่าจาก OS จริง ผ่าน helper functions:

| Field | Helper Function | แหล่งข้อมูล |
|---|---|---|
| `addressing_mode` | `detectAddressingMode()` | ตรวจ DHCP lease/PID files |
| `gateway` | `getGatewayForInterface()` | `/proc/net/route` |
| `speed` | `getInterfaceSpeed()` | `/sys/class/net/<iface>/speed` |
| `admin_access` | (inline logic) | LAN=`"PING,HTTP,SSH"`, WAN=`"PING"` |
| `role` | (inline logic) | `eth0`/`*wan*` → WAN, อื่นๆ → LAN |
| `type` | (inline logic) | ชื่อขึ้นต้น `w` → wireless, อื่นๆ → ethernet |

---

## 4. Helper Functions (OS Detection)

### `detectAddressingMode(ifaceName, ifaceIndex)`

ตรวจว่า interface ใช้ DHCP หรือ Static โดยตรวจไฟล์ตามลำดับ:

1. **dhclient PID files**: `/run/dhclient-<iface>.pid`, `/run/dhclient.<iface>.pid`, `/var/run/...`
2. **dhclient lease files**: `/var/lib/dhcp/dhclient.<iface>.leases`, `/var/lib/dhclient/...`
3. **dhcpcd lease files**: `/var/lib/dhcpcd/<iface>.lease`, `/var/lib/dhcpcd5/...`
4. **systemd-networkd**: `/run/systemd/netif/leases/<ifaceIndex>`
5. **NetworkManager**: `/var/lib/NetworkManager/dhclient-<iface>.conf`, `/run/NetworkManager/...`
6. **Fallback**: คืนค่า `"static"`

### `getGatewayForInterface(ifaceName)`

- อ่าน `/proc/net/route`
- กรอง row ที่ `Iface == ifaceName` AND `Destination == 00000000` (default route)
- แปลง Gateway hex little-endian เป็น IP string
- ถ้า gateway เป็น `0.0.0.0` คืนค่า `""`

### `getInterfaceSpeed(ifaceName)`

- อ่าน `/sys/class/net/<iface>/speed` (หน่วย Mbps)
- แปลงเป็น string: `"1 Gbps"` (≥1000), `"100 Mbps"`, หรือ `"unknown"`

---

## 5. REST API Endpoints

ไฟล์: `backend/internal/api/router.go`, `handlers.go`

ทุก endpoint ต้องผ่าน JWT/Session authentication ก่อน

| Method | Path | Handler | หน้าที่ |
|---|---|---|---|
| `GET` | `/api/interfaces` | `HandleGetInterfaces` | ดึง list ทั้งหมด (trigger OS sync) |
| `PUT` | `/api/interfaces/{id}` | `HandleUpdateInterface` | แก้ไขค่า interface |
| `POST` | `/api/interfaces/{id}/toggle` | `HandleToggleInterface` | เปิด/ปิด interface (OS + DB) |
| `GET` | `/api/interfaces/{id}/scan` | `HandleScanWifi` | สแกน Wi-Fi SSID (wireless เท่านั้น) |

### `HandleUpdateInterface` — Fields ที่ update ได้

`alias`, `role`, `addressingMode`, `ip`, `netmask`, `gateway`, `macAddress`, `adminAccess`, `status`, `macMode`, `laaMacAddress`, `randomizeOnReconnect`, `backupSsid`, `backupWifiPassword`

> **หมายเหตุ**: `name` และ `type` **ไม่สามารถแก้ไขได้** ผ่าน API (ผูกกับ OS)

### `HandleToggleInterface` — ลำดับการทำงาน

1. ดึง interface จาก DB
2. คำนวณ `nextStatus` (`up` ↔ `down`)
3. เรียก `kernel.NetworkManager.ToggleInterface(name, isUp)` → OS action
4. อัปเดต DB ผ่าน `repo.ToggleInterfaceStatus()`
5. คืน interface ที่อัปเดตแล้ว

---

## 6. Mock Mode

ระบบรองรับ 3 โหมดผ่าน CLI flags:

| Flag | `mockMode` | `mockFromReal` | พฤติกรรม |
|---|---|---|---|
| (default) | `true` | `false` | ใช้ seed data ใน DB เท่านั้น ไม่ sync OS |
| `-mock-from-real` | `false` | `true` | Sync จาก OS จริง + inject mock wireless ถ้าไม่มี |
| (production) | `false` | `false` | Sync จาก OS จริงเต็มรูปแบบ |

### Mock Wireless Injection

ถ้า `mockFromReal == true` และไม่มี `wireless` interface ใน DB → ระบบจะ inject `wlan0` mock:

```
ID: iface-mock-wlan0
IP: 10.0.0.45, Role: WAN
SSID: MyHome_5G, Security: WPA2-PSK
MAC Mode: randomized
```

---

## 7. ข้อควรระวัง

1. **`addressing_mode` ใน DB** — ค่านี้เป็น "ค่าที่ผู้ใช้กำหนด" ไม่ใช่ real-time จาก OS หากผู้ใช้บันทึก `static` ไว้ใน DB แล้ว OS ถูกเปลี่ยนเป็น DHCP → ค่าใน DB จะยังแสดง `static` จนกว่าจะ UPDATE ผ่าน API

2. **`SyncInterfacesFromOS` ไม่ update `addressing_mode` ในกรณี UPDATE** — เจตนาเพื่อรักษาการตั้งค่าของผู้ใช้ ดู comment ในโค้ด:
   ```
   // addressing_mode and other user-configured fields are intentionally preserved
   ```

3. **`speed` อาจเป็น `"unknown"`** — สำหรับ wireless interface, `/sys/class/net/wlan0/speed` อาจ return ค่า `-1` หรือ error เพราะความเร็วขึ้นอยู่กับ connection ณ ขณะนั้น

4. **`net.FlagUp` ≠ Administratively Down** — `FlagUp` อ่านจาก kernel IFF_UP flag เท่านั้น การรัน `nmcli connection down <name>` อาจไม่เปลี่ยน IFF_UP เพราะ NM deactivate เฉพาะ connection profile ไม่ได้สั่ง `ip link set down` เสมอไป → สถานะอาจไม่ sync ถ้าใช้ `nmcli` โดยตรง ควรใช้ API `POST /toggle` แทน

5. **Production mode ต้องการ Linux Capabilities** — `RealNetwork` (netlink) ต้องการ `cap_net_admin` บน binary ก่อนใช้งาน:
   ```bash
   sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
   ```

---

## 8. Kernel Integration (Production)

ไฟล์: `backend/internal/kernel/real_network.go` (build tag: `linux`)

`RealNetwork` implement `NetworkManager` interface โดยสื่อสารกับ kernel ผ่าน **Netlink Socket** โดยตรง (ไม่ใช้ shell command) ต้องการ `github.com/vishvananda/netlink`

### `ToggleInterface(name string, up bool)`

```go
link, _ := netlink.LinkByName(name)
netlink.LinkSetUp(link)   // ≡ ip link set eth0 up
netlink.LinkSetDown(link) // ≡ ip link set eth0 down
```

→ เปลี่ยน `IFF_UP` ใน kernel โดยตรง → `SyncInterfacesFromOS()` จะ reflect ค่า `net.FlagUp` ถูกต้องในครั้งถัดไป

### `ScanWifi(name string)`

1. **Primary**: `iw dev <name> scan` — parse BSS blocks
2. **Fallback**: `nmcli --terse --fields SSID,SIGNAL,SECURITY,CHAN,FREQ dev wifi list`

### Production Deployment

```bash
go build -o pigate-backend ./cmd/pigate
sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
./pigate-backend -port=8080 -mock=false
```

---

---

## 9. Wi-Fi Client Management & wpa_supplicant Integration

ระบบควบคุม Wi-Fi ของ PiGate หลีกเลี่ยงการใช้งาน `NetworkManager` (`nmcli`) เพื่อลด Overhead ของระบบปฏิบัติการ และป้องกันสิทธิ์การแย่งชิงควบคุมอินเทอร์เฟซ (IP/Routing Conflict) โดยใช้ **`wpa_supplicant`** ซึ่งเป็นมาตรฐานน้ำหนักเบาและเชื่อมโยงการทำงานในระดับ Link Layer (Layer 2) แทน

### 9.1 Auto-Failover & Priority ใน wpa_supplicant

`wpa_supplicant` สามารถจัดการการสลับสายไปยังคลื่นสำรองและสลับกลับ (Failover & Failback) ได้ในตัวเองโดยการระบุบล็อก `network` หลายตัวและใช้ระบบความสำคัญ (`priority`) ในไฟล์คอนฟิก `/etc/wpa_supplicant/wpa_supplicant-wlan0.conf`:

```wpa_supplicant
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
country=TH

# เครือข่ายหลัก (SSID หลัก)
network={
    ssid="MyHome_WiFi"
    psk="primary-password-here"
    priority=10
}

# เครือข่ายสำรอง (Backup SSID)
network={
    ssid="Backup_WiFi"
    psk="backup-password-here"
    priority=5
}
```

* **การสลับคลื่น**: หากสัญญาณคลื่นหลักหายไป ระบบจะสลับมาเชื่อมคลื่นสำรองอัตโนมัติ
* **การดึงข้อมูลกลับ**: เมื่อคลื่นที่มี `priority` สูงกว่ากลับมาส่งสัญญาณ ระบบจะย้ายกลับไปเชื่อมต่อคลื่นหลักให้อัตโนมัติ

---

### 9.2 wpa_supplicant Control Socket Mechanism

แทนการเรียกใช้ Subprocess ด้วยเชลล์คำสั่ง `wpa_cli` ทางระบบ Go Backend สื่อสารผ่าน **UNIX Domain Datagram Socket (`SOCK_DGRAM`)** ของ `wpa_supplicant` โดยตรง

```text
  Go Backend (Client)                                   wpa_supplicant (Server)
┌─────────────────────────────────┐                   ┌───────────────────────────────┐
│ Local Temp Socket:              │                   │ Master Control Socket:        │
│ `/run/pigate/wpa_ctrl_12345`    │                   │ `/var/run/wpa_supplicant/wlan0`│
│                                 │                   │                               │
│      1. Bind Local Temp Socket ─┼───────────────────┼─► [Listening]                 │
│      2. Send Command Datagram  ─┼───────────────────┼──► (เช่น "PING")               │
│                                 │                   │                               │
│      3. Wait and Receive ◄──────┼───────────────────┼─── Send Response ("PONG")     │
│                                 │                   │                               │
│      4. Unlink/Delete Temp File │                   │                               │
└─────────────────────────────────┘                   └───────────────────────────────┘
```

#### การสื่อสารระดับต่ำ (Low-level Go Socket Code Pattern):

```go
package kernel

import (
	"fmt"
	"net"
	"os"
	"time"
)

// SendWpaCommand ส่งคำสั่งตรงไปยัง wpa_supplicant socket (เช่น "RECONFIGURE" เพื่อโหลดคอนฟิกใหม่)
func SendWpaCommand(ifaceName string, command string) (string, error) {
	destAddr := fmt.Sprintf("/var/run/wpa_supplicant/%s", ifaceName)
	localAddr := fmt.Sprintf("/run/pigate/wpa_ctrl_%d", os.Getpid())
	
	_ = os.Remove(localAddr)

	lAddr, err := net.ResolveUnixAddr("unixgram", localAddr)
	if err != nil {
		return "", err
	}
	rAddr, err := net.ResolveUnixAddr("unixgram", destAddr)
	if err != nil {
		return "", err
	}

	conn, err := net.DialUnix("unixgram", lAddr, rAddr)
	if err != nil {
		return "", fmt.Errorf("failed to dial wpa_supplicant socket: %w", err)
	}
	defer func() {
		conn.Close()
		os.Remove(localAddr)
	}()

	conn.SetDeadline(time.Now().Add(2 * time.Second))

	_, err = conn.Write([]byte(command))
	if err != nil {
		return "", fmt.Errorf("failed to send command: %w", err)
	}

	buf := make([]byte, 2048)
	n, err := conn.Read(buf)
	if err != nil {
		return "", fmt.Errorf("failed to read response: %w", err)
	}

	return string(buf[:n]), nil
}
```

---

### 9.3 ข้อควรระวังความปลอดภัย (Security Controls)

1. **สิทธิ์การอ่านรหัสผ่าน (File Permissions)**: ไฟล์คอนฟิก `/etc/wpa_supplicant/wpa_supplicant-wlan0.conf` เก็บความลับของรหัสผ่าน จึงต้องเขียนไฟล์ด้วยสิทธิ์จำกัดระดับ `0600` (`chmod 600` - อ่านเขียนเฉพาะเจ้าของไฟล์) เท่านั้น
2. **การป้องกัน Configuration Injection**: ก่อนเขียนข้อมูล SSID และ Password ลงในไฟล์คอนฟิก Go Backend ต้องทำความสะอาดอักขระพิเศษ เช่น `"` และ `\n` เพื่อป้องกันการบิดเบือนรูปแบบบล็อกเน็ตเวิร์ก
3. **การเข้าถึง Socket**: โปรแกรมที่รันต้องอยู่ในกลุ่ม `netdev` เพื่อมีสิทธิ์เขียนไปยัง `/var/run/wpa_supplicant/` socket

---

## 10. ไฟล์ที่เกี่ยวข้อง

| ไฟล์ | หน้าที่ |
|---|---|
| [`backend/internal/model/types.go`](../../../backend/internal/model/types.go) | Data model struct ของ NetworkInterface |
| [`backend/internal/db/connection.go`](../../../backend/internal/db/connection.go) | DB schema + seed data |
| [`backend/internal/db/repository.go`](../../../backend/internal/db/repository.go) | CRUD + OS sync logic (SyncInterfacesFromOS, detectAddressingMode ฯลฯ) |
| [`backend/internal/api/handlers.go`](../../../backend/internal/api/handlers.go) | HTTP handlers สำหรับ interface endpoints |
| [`backend/internal/api/router.go`](../../../backend/internal/api/router.go) | Route mapping |
| [`backend/internal/kernel/interfaces.go`](../../../backend/internal/kernel/interfaces.go) | Interface abstractions (NetworkManager, FirewallManager ฯลฯ) |
| [`backend/internal/kernel/mock.go`](../../../backend/internal/kernel/mock.go) | Mock implementations สำหรับ test/dev |
| [`backend/internal/kernel/real_network.go`](../../../backend/internal/kernel/real_network.go) | **[ใหม่]** Real NetworkManager ผ่าน netlink (Linux production) |
