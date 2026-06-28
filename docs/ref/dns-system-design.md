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
