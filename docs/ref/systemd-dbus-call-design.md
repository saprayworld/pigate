# Systemd D-Bus Call — รวมศูนย์ (Design)

## ปัญหาปัจจุบัน

โค้ดที่คุยกับ `org.freedesktop.systemd1` (systemd Manager) ผ่าน D-Bus เพื่อสั่ง
start/stop/restart/is-active ของ unit ต่างๆ กระจายอยู่คนละไฟล์ และมี logic
ซ้ำกันเกือบทุกฟังก์ชัน (เปิด `dbus.SystemBus()` → หา object
`/org/freedesktop/systemd1` → เรียก method บน interface `Manager` → เก็บ
`jobPath`):

| ไฟล์ | ฟังก์ชัน | ใช้ทำอะไร |
|---|---|---|
| `real_network.go` | `IsServiceActiveViaDBus(serviceName string) bool` | เช็คว่า unit active หรือไม่ (ใช้กับ wpa_supplicant units) |
| `real_network.go` | `StartServiceViaDBus(serviceName string) error` | สั่ง start unit (wpa_supplicant units) |
| `real_network.go` | `StopServiceViaDBus(serviceName string) error` | สั่ง stop unit (wpa_supplicant units) |
| `dns.go` | `RestartServiceViaDBus(serviceName string) error` | สั่ง restart unit (`systemd-resolved.service`) — pattern เดียวกับ 3 ตัวบนเป๊ะ แค่เรียก `RestartUnit` |

ไฟล์อื่นที่เรียกใช้ฟังก์ชันเหล่านี้ (ไม่มี logic D-Bus ของตัวเอง แค่เรียกชื่อฟังก์ชันตรงๆ
เพราะอยู่ package `kernel` เดียวกัน):

- `dhcpcd.go` → เรียก `StartServiceViaDBus` / `StopServiceViaDBus` สำหรับ `dhcpcd@<iface>.service`
- `dhcp_server.go` → เรียก `RestartServiceViaDBus("dnsmasq.service")`
- `dns_server.go` → เรียก `RestartServiceViaDBus("dnsmasq.service")`

**นอกขอบเขตของงานนี้** (คุยกับ D-Bus service คนละตัว ไม่ใช่ `org.freedesktop.systemd1`
Manager ดังนั้นไม่ได้ใช้ pattern เดียวกัน จะไม่ย้าย/รวมในรอบนี้):

- `dns.go` — `GetLinkDNS` / `SetLinkDNS` / `RevertLinkDNS` เปิด `dbus.SystemBus()`
  เองเพื่อคุยกับ `org.freedesktop.resolve1` (Link DNS properties)
- `dhcp_server.go` — `WatchLeases` เปิด `dbus.SystemBus()` เองเพื่อ subscribe
  signal จาก `uk.org.thekelleys.dnsmasq`

ยืนยันแล้วด้วย `grep -rn "kernel\.\(IsServiceActiveViaDBus\|StartServiceViaDBus\|StopServiceViaDBus\|RestartServiceViaDBus\)" backend/`
ว่าไม่มี package ไหนนอก `kernel` เรียกฟังก์ชันกลุ่มนี้ตรงๆ — ใช้ภายใน `kernel`
package เท่านั้น (เรียกผ่าน interface `DhcpManager` / `DNSManager` /
`DNSServerManager` เป็นต้น)

## เป้าหมาย

1. รวม logic การเรียก `org.freedesktop.systemd1` Manager (start/stop/restart/
   is-active unit) ไว้ที่ไฟล์เดียว ลด duplication
2. ไม่เปลี่ยนพฤติกรรมการทำงาน (pure refactor) — ชื่อฟังก์ชัน, signature, log
   message คงเดิม เพื่อลดความเสี่ยงและ diff ที่ต้อง review
3. ไม่แตะ `resolve1` และ `dnsmasq` signal watcher ในรอบนี้ (คนละ concern)

## ไฟล์ใหม่ที่จะสร้าง

**`backend/internal/kernel/dbus_systemd.go`**

```go
//go:build linux

package kernel

import (
    "fmt"
    "log"

    "github.com/godbus/dbus/v5"
)

const (
    systemd1Dest    = "org.freedesktop.systemd1"
    systemd1Path    = "/org/freedesktop/systemd1"
    systemd1Manager = "org.freedesktop.systemd1.Manager"
    systemd1Unit    = "org.freedesktop.systemd1.Unit"
)

// callSystemdUnitMethod เรียก method บน org.freedesktop.systemd1.Manager
// (StartUnit / StopUnit / RestartUnit) แบบเดียวกันทั้งหมด ต่างกันแค่ชื่อ method
// และ log prefix ของผู้เรียก
func callSystemdUnitMethod(logPrefix, method, serviceName string) error {
    log.Printf("[%s] Attempting to %s: %s", logPrefix, method, serviceName)

    conn, err := dbus.SystemBus()
    if err != nil {
        log.Printf("[%s] Failed to connect to system bus: %v", logPrefix, err)
        return fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
    }

    obj := conn.Object(systemd1Dest, dbus.ObjectPath(systemd1Path))
    var jobPath dbus.ObjectPath
    err = obj.Call(systemd1Manager+"."+method, 0, serviceName, "replace").Store(&jobPath)
    if err != nil {
        log.Printf("[%s] Failed to call %s for %s: %v", logPrefix, method, serviceName, err)
        return fmt.Errorf("D-Bus call %s failed for %s: %w", method, serviceName, err)
    }

    log.Printf("[%s] %s job queued successfully. Job Path: %s", logPrefix, method, jobPath)
    return nil
}

// IsServiceActiveViaDBus เช็กว่า systemd unit กำลังทำงานอยู่หรือไม่ (แทน systemctl is-active)
func IsServiceActiveViaDBus(serviceName string) bool {
    conn, err := dbus.SystemBus()
    if err != nil {
        log.Printf("[D-Bus] Failed to connect to system bus: %v", err)
        return false
    }

    obj := conn.Object(systemd1Dest, dbus.ObjectPath(systemd1Path))
    var unitPath dbus.ObjectPath
    if err := obj.Call(systemd1Manager+".GetUnit", 0, serviceName).Store(&unitPath); err != nil {
        return false
    }

    unitObj := conn.Object(systemd1Dest, unitPath)
    variant, err := unitObj.GetProperty(systemd1Unit + ".ActiveState")
    if err != nil {
        return false
    }

    state, ok := variant.Value().(string)
    return ok && state == "active"
}

// StartServiceViaDBus สั่งรัน systemd unit (แทน systemctl start)
func StartServiceViaDBus(serviceName string) error {
    return callSystemdUnitMethod("D-Bus", "StartUnit", serviceName)
}

// StopServiceViaDBus สั่งหยุด systemd unit (แทน systemctl stop)
func StopServiceViaDBus(serviceName string) error {
    return callSystemdUnitMethod("D-Bus", "StopUnit", serviceName)
}

// RestartServiceViaDBus สั่งรีสตาร์ท systemd unit (แทน systemctl restart)
func RestartServiceViaDBus(serviceName string) error {
    return callSystemdUnitMethod("D-Bus", "RestartUnit", serviceName)
}
```

หมายเหตุการออกแบบ:

- คง**ชื่อฟังก์ชันเดิมทุกตัว** (`IsServiceActiveViaDBus`, `StartServiceViaDBus`,
  `StopServiceViaDBus`, `RestartServiceViaDBus`) เพื่อให้ **ไม่ต้องแก้ call site
  ในไฟล์อื่นเลยสักบรรทัด** (`dhcpcd.go`, `dhcp_server.go`, `dns_server.go`,
  `real_network.go` ที่เหลือ) — เพราะยังอยู่ package `kernel` เดียวกัน
- log prefix เดิมของแต่ละจุดเรียก (`[RealDhcpcd]`, `[RealNetwork-D-Bus]`,
  `[D-Bus]`) เป็น prefix ของ**ผู้เรียก** ไม่ใช่ของฟังก์ชัน D-Bus เอง จึงยังคง
  แสดงอยู่ครบใน log บรรทัดที่ `dhcpcd.go`/`real_network.go` เรียกใช้ ไม่ได้หายไป
  ส่วน log *ภายใน* ฟังก์ชัน D-Bus เอง (ที่เคย prefix ต่างกันเป็น
  `[RealNetwork-D-Bus]` กับ `[D-Bus]`) จะถูกรวมเป็น `[D-Bus]` อันเดียว — ถือเป็น
  behavior change เล็กน้อยเฉพาะข้อความ log (ไม่กระทบ logic) ควรระบุใน commit
  message
- ไม่ unexport ฟังก์ชันแม้จะเช็คแล้วว่าไม่มีใครนอก package เรียกใช้ — เก็บไว้เป็น
  follow-up แยกต่างหาก (ดูหัวข้อ "นอกขอบเขต" ด้านล่าง) เพื่อให้ PR นี้เป็น pure
  move + dedupe ไม่ปนกับการเปลี่ยน API visibility

## ไฟล์ที่ต้องแก้ไข

1. **สร้างใหม่** `backend/internal/kernel/dbus_systemd.go` — ตามโค้ดด้านบน

2. **แก้ไข** `backend/internal/kernel/real_network.go`
   - ลบฟังก์ชัน `IsServiceActiveViaDBus`, `StartServiceViaDBus`,
     `StopServiceViaDBus` ทั้ง 3 ตัว (บรรทัด ~471-543 รวม comment header
     `// D-Bus based management functions...` ด้านบนด้วย)
   - เช็คว่า `github.com/godbus/dbus/v5` ยังถูกใช้ที่อื่นในไฟล์นี้หรือไม่
     (ปัจจุบันใช้เฉพาะใน 3 ฟังก์ชันที่ลบ) — ถ้าไม่เหลือการใช้งานแล้วต้องลบ
     import ออกด้วย ไม่งั้น `go build` จะ error `imported and not used`
   - จุดที่เรียกใช้ฟังก์ชัน (บรรทัด ~72, 78, 91, 149, 173) **ไม่ต้องแก้**
     เพราะชื่อฟังก์ชันเดิม อยู่ package เดียวกัน

3. **แก้ไข** `backend/internal/kernel/dns.go`
   - ลบฟังก์ชัน `RestartServiceViaDBus` (บรรทัด ~255-284)
   - **ห้ามลบ** import `github.com/godbus/dbus/v5` — ไฟล์นี้ยังใช้ต่อใน
     `GetLinkDNS` / `SetLinkDNS` / `RevertLinkDNS` (คุยกับ `resolve1`)
   - จุดเรียกใช้ `RestartServiceViaDBus("systemd-resolved.service")`
     (บรรทัด ~172, ~213) ไม่ต้องแก้

4. **ไม่ต้องแก้** `dhcpcd.go`, `dhcp_server.go`, `dns_server.go` —
   เรียกชื่อฟังก์ชันเดิม ไม่มีอะไรเปลี่ยน

## ขั้นตอนการทำ (step-by-step)

1. สร้างไฟล์ `backend/internal/kernel/dbus_systemd.go` ตาม draft ด้านบน
2. เปิด `real_network.go` ลบ 3 ฟังก์ชัน + comment header ของ section
   "D-Bus based management functions" ออก
3. รัน `grep -n "dbus\." backend/internal/kernel/real_network.go` — ถ้าไม่มีผลลัพธ์
   เหลือ ให้ลบ `"github.com/godbus/dbus/v5"` ออกจาก import block ของไฟล์นี้
4. เปิด `dns.go` ลบฟังก์ชัน `RestartServiceViaDBus` ออก (เหลือ import `dbus`
   ไว้เหมือนเดิมเพราะยังใช้กับ `resolve1`)
5. รัน `cd backend && go build ./...` — ต้อง build ผ่านโดยไม่มี unused-import
   หรือ undefined-symbol error
6. รัน `go vet ./...`
7. รัน `go test ./...` (โดยเฉพาะ `internal/service/...` และ
   `internal/kernel/...`) — ควรผ่านเหมือนเดิมทั้งหมด เพราะเป็น refactor ที่ไม่
   เปลี่ยน behavior ทาง logic (นอกจาก log prefix ตามที่ระบุไว้ข้างบน)
8. Diff review: เช็คว่าไม่มีไฟล์ไหนถูกแก้เกินขอบเขตที่ระบุไว้ (เฉพาะ
   `dbus_systemd.go` ใหม่, `real_network.go`, `dns.go`)

> การทดสอบจริงบนอุปกรณ์ (Raspberry Pi) เป็นขั้นตอนที่ผู้ใช้ deploy เองหลัง
> build — ไม่ได้อยู่ในสโคปของ step นี้

## นอกขอบเขต / งานต่อยอดที่เป็นไปได้ในอนาคต

- รวม `resolve1` D-Bus calls (`GetLinkDNS`/`SetLinkDNS`/`RevertLinkDNS`) เป็น
  helper กลางแยกไฟล์ เช่น `dbus_resolve.go` (คนละ D-Bus service, คนละ pattern
  จาก systemd1 Manager จึงไม่รวมไฟล์เดียวกับ commit นี้)
- ทำ shared `systemBus()` helper (เปิด/แชร์ `*dbus.Conn` เดียวแทนการเรียก
  `dbus.SystemBus()` กระจายทุกจุด) ให้ `dhcp_server.go` (`WatchLeases`) และ
  ฟังก์ชันใน `dbus_systemd.go`/`dns.go` ใช้ร่วมกัน — หมายเหตุ: `dbus.SystemBus()`
  ของ library `godbus/dbus` cache connection เป็น singleton ต่อ process อยู่แล้ว
  ภายใน (ใช้ `sync.Once`) ดังนั้นการเรียกซ้ำหลายจุดไม่ได้เปิด socket ใหม่จริงๆ
  — ประโยชน์ของการรวมจะเป็นเรื่อง code organization/testability มากกว่า
  performance
- พิจารณา unexport ฟังก์ชันกลุ่มนี้ (เช่น `startServiceViaDBus`) เนื่องจากใช้
  เฉพาะภายใน package `kernel` เพื่อบีบขอบเขต visibility ให้ชัดเจนขึ้น — แยกเป็น
  PR ต่างหากเพราะกระทบ call site หลายไฟล์
