# PiGate Wi-Fi Client Configuration & wpa_supplicant Working Instruction (WI)

เอกสารฉบับนี้อธิบายแนวทางการพัฒนาและแนวคิดในการจัดการการเชื่อมต่อ Wi-Fi Client (WAN/Backup Link) บนบอร์ด Raspberry Pi 5 ด้วยโปรแกรม **`wpa_supplicant`** ผ่านการควบคุมของ Go Backend ในโครงการ PiGate เพื่อความเสถียร ความปลอดภัย และป้องกันปัญหาข้อจำกัดของ NetworkManager (`nmcli`)

---

## 1. ทำไมถึงเลือก `wpa_supplicant` แทน `NetworkManager`

1. **หลีกเลี่ยงการแย่งชิงสิทธิ์ควบคุมการ์ดเครือข่าย (No Routing/IP Conflict)**: NetworkManager มักจะจัดการ IP/Routing และพยายามเขียนทับการตั้งค่าระดับล่างที่เราจัดการผ่าน Netlink Socket ด้วยตัวเราเอง
2. **หลีกเลี่ยงการรันคำสั่งเชลล์ (No Shell Command Wrapper)**: ไม่ใช้คำสั่ง `nmcli` ที่เสี่ยงต่อการโดน Command Injection และลดการพึ่งพาตัวแปรภายนอก
3. **ระบบน้ำหนักเบา (Minimalist)**: wpa_supplicant ทำงานที่ระดับ Link Layer (Layer 2) เท่านั้น และติดตั้งเป็นมาตรฐานในระบบ Linux แทบทุกชนิดโดยไม่สร้างภาระให้บอร์ด

---

## 2. ระบบ Auto-Failover และ Priority ในตัว

`wpa_supplicant` สามารถสลับสายไปยังคลื่นสำรองและสลับกลับ (Failover & Failback) ได้เองโดยกำหนดบล็อก `network` และลำดับความสำคัญ (`priority`) ในไฟล์คอนฟิกเดียวกัน:

ตัวอย่างไฟล์คอนฟิก `/etc/wpa_supplicant/wpa_supplicant-wlan0.conf`:
```wpa_supplicant
ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
update_config=1
country=TH

# คลื่นหลัก (SSID หลัก)
network={
    ssid="MyHome_WiFi"
    psk="primary-password-here"
    priority=10
}

# คลื่นสำรอง (Backup SSID)
network={
    ssid="Backup_WiFi"
    psk="backup-password-here"
    priority=5
}
```

* **เมื่อเกาะคลื่นหลักไม่ได้**: wpa_supplicant จะย้ายมาเกาะคลื่นสำรอง (priority ต่ำกว่า) อัตโนมัติ
* **เมื่อคลื่นหลักกลับมา**: wpa_supplicant จะดึงการเชื่อมต่อกลับมาใช้คลื่นหลัก (priority สูงกว่า) อัตโนมัติในทันที

---

## 3. กลไก wpa_supplicant Control Socket (UNIX Domain Socket)

แทนการเรียก subprocess ไปรันคำสั่งควบคุมผ่านเชลล์ ระบบ Go Backend จะใช้ **UNIX Domain Datagram Socket (`SOCK_DGRAM`)** เพื่อคุยกับ wpa_supplicant โดยตรง

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

> [!IMPORTANT]
> เนื่องจาก wpa_supplicant ใช้ **Datagram Socket** การเชื่อมต่อของ Go Backend จะต้องทำการ Bind Local Socket ไฟล์ชั่วคราวก่อนส่งเสมอ เพื่อรองรับการตอบกลับจากฝั่ง Server

### โครงสร้างโค้ด Go สำหรับเชื่อมต่อผ่าน Socket:

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

## 4. ข้อพิจารณาและแนวทางปฏิบัติในการติดตั้งระบบ (Working Instructions)

ในการพัฒนาฟังก์ชันนี้ ผู้พัฒนาต้องปฏิบัติตามแนวทางการเขียนโค้ดและการตั้งค่าความปลอดภัยของบอร์ดอย่างเคร่งครัด:

### 4.1 รองรับหลายพอร์ตเครือข่าย (Multi-Interface)
* ไฟล์คอนฟิกและ Service ของ wpa_supplicant ต้องแยกเป็นรายพอร์ต เช่น `wpa_supplicant-wlan0.conf` และใช้การเรียกงานผ่าน systemd template: `wpa_supplicant@wlan0.service`

### 4.2 ห้ามลืมบรรดทัด Header บังคับ
* ทุกครั้งที่เขียนไฟล์คอนฟิกขึ้นมาใหม่ ต้องประกอบด้วย 2 บรรทัดแรกนี้เสมอ:
  ```wpa_supplicant
  ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev
  update_config=1
  ```
  หากตกหล่น wpa_supplicant จะไม่สร้าง Control Socket ส่งผลให้ Go Backend สั่งงานผ่าน Socket ไม่ได้อีก

### 4.3 เขียนไฟล์แบบอะตอมมิก (Atomic File Writing)
* เพื่อป้องกันปัญหาไฟดับระหว่างบันทึกไฟล์ซึ่งจะส่งผลให้ไฟล์คอนฟิก Wi-Fi เสียหาย (Corrupted/Blank):
  1. เขียนลงไฟล์ชั่วคราวก่อน เช่น `/etc/wpa_supplicant/wpa_supplicant-wlan0.conf.tmp`
  2. ใช้ฟังก์ชัน `os.Rename` ย้ายทับเพื่อการันตีความเป็น Atomic Operation

### 4.4 แปลงรูปแบบความปลอดภัย Wi-Fi ให้ถูกต้อง
* **Open**:
  ```wpa_supplicant
  network={
      ssid="Cafe_Free_WiFi"
      key_mgmt=NONE
  }
  ```
* **WPA2-PSK**:
  ```wpa_supplicant
  network={
      ssid="MyHome_WiFi"
      psk="password1234"
      key_mgmt=WPA-PSK
  }
  ```
* **WPA3-SAE**:
  ```wpa_supplicant
  network={
      ssid="ModernRouter_WiFi"
      psk="password1234"
      key_mgmt=WPA-PSK SAE
      ieee80211w=2  # เปิดใช้งาน PMF สำหรับความปลอดภัย WPA3
  }
  ```

### 4.5 จัดการการเปิด-ปิด Service และ Socket Status
* เมื่อสั่งเปิดอินเทอร์เฟซ (Up) ต้องเช็กว่า `wpa_supplicant@wlan0.service` ทำงานอยู่หรือไม่ หากไม่ทำงานให้สั่ง start
* เมื่อสั่งปิดอินเทอร์เฟซ (Down) ควรหยุดระบบด้วย `systemctl stop wpa_supplicant@wlan0` เพื่อไม่ให้เปลืองพลังงานและตัดการใช้สัญญาณวิทยุ

---

## 5. การตรวจสอบความมั่นคงปลอดภัย (Security Guidelines)

1. **สิทธิ์ของไฟล์ (File Permissions)**: ไฟล์คอนฟิก `/etc/wpa_supplicant/wpa_supplicant-*.conf` มีการบันทึกรหัสผ่าน Wi-Fi ต้องจำกัดสิทธิ์ระดับ `0600` (`chmod 600` - อ่านเขียนเฉพาะเจ้าของไฟล์) เท่านั้น
2. **การล้างข้อมูลนำเข้า (Configuration Injection)**: ใน Go Backend ก่อนนำตัวแปร SSID หรือ Password ไปประกอบและเขียนลงไฟล์คอนฟิก จะต้องทำการลบหรือแปลงอักขระขึ้นบรรทัดใหม่ `\n` และเครื่องหมายฟันหนู `"` เพื่อความปลอดภัย
3. **จำกัดสิทธิ์โปรแกรม (Execution Capabilities)**: Go binary จะต้องถูกจำกัดให้ทำงานภายใต้สิทธิ์ผู้ใช้ทั่วไปและรับ Capabilities เฉพาะทาง เช่น `cap_net_admin` เท่านั้น และ User ของระบบต้องอยู่ในกลุ่ม `netdev` เพื่อให้เขียนอ่าน socket ได้ตามมาตรฐานของ Linux
