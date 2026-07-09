# PiGate Project Status & Roadmap

เอกสารฉบับนี้เป็นรายงานสรุปสถานะล่าสุดของโปรเจกต์ **PiGate** (ระบบควบคุม Raspberry Pi Firewall/Gateway) ทั้งความคืบหน้า โครงสร้างระบบล่าสุด ปัญหาที่พบ และแผนงานในอนาคต

> อัปเดตล่าสุด: 2026-07-05 — แผนงานที่ทำเสร็จแล้วถูกเก็บไว้ที่ [docs/ref/complete/](ref/complete/) และงานที่ค้างอยู่ที่ [docs/ref/todo/](ref/todo/)

---

## 1. สิ่งที่ได้ดำเนินการเสร็จสิ้นแล้ว (Completed Work)

ส่วนนี้สรุปงานที่พัฒนาเสร็จสมบูรณ์และพร้อมใช้งานแล้ว โดยแบ่งตามส่วนประกอบหลักของระบบ:

### 1.1 หน้าต่างผู้ใช้งาน (Frontend SPA - React 19)
* **Shell Layout & Responsive Design [สำเร็จ]:**
  * พัฒนาโครงหน้าจอหลัก ([ShellLayout.tsx](file:///home/sapray/dev/pigate/frontend/src/components/layout/ShellLayout.tsx)) สไตล์ Dark Mode ระดับพรีเมียม สนับสนุนทั้งระบบ Dark/Light Theme (บันทึกลง LocalStorage)
  * ออกแบบตารางข้อมูลกฎความปลอดภัย (Firewall Policies) และพอร์ตเชื่อมต่อ (Interfaces) เป็นแบบ Responsive โดยใช้ `<div className="overflow-x-auto w-full">` ป้องกันการล้นบนจอโทรศัพท์มือถือ
* **Interactive Pages (9 หน้าหลัก) [สำเร็จ]:** พัฒนาหน้าจอและการทำงานจำลอง (Mock API/SSE) ครบถ้วน ได้แก่ Dashboard, Interfaces, Static Routes, DHCP, Firewall Policy, Addresses, Services, Settings, Login
* **Dashboard & Charts [สำเร็จ]:**
  * พัฒนากราฟข้อมูลทราฟฟิกเครือข่ายเรียลไทม์ (WAN Bandwidth) แบบ Dynamic Line Chart ด้วย Recharts (โทนสี Cyan & Indigo)
  * แสดง Badges สถานะตัวบอร์ดบอร์ด (CPU, RAM, Temp, Power) และตารางแสดงประวัติความปลอดภัย (Firewall Logs) ที่กรองและค้นหาได้
* **Firewall Policies UI [สำเร็จ]:**
  * ระบบจัดลำดับความสำคัญของกฎแบบลากวาง (Drag & Drop) ด้วย `@dnd-kit/core` ล็อคแกนแนวตั้ง
  * ฟอร์มเพิ่ม/แก้ไขกฎที่มี Multiple Selection Combobox แบบ Chips ดึงข้อมูล Address/Service objects และ Interface ขาเข้า-ออก ไดนามิก
  * แก้ไขปัญหาคลิกดรอปดาวน์ภายใน Dialog โดน Radix Blocker โดยตั้งค่า `modal={false}` บน `<Dialog>`
* **Network Objects & CRUD Manager [สำเร็จ]:**
  * พัฒนาหน้า Addresses และ Services รองรับการทำงาน CRUD
  * มีการล็อคสิทธิ์แก้ไข/ลบวัตถุระบบ (🔒 Predefined System Objects) และตรวจเช็คความปลอดภัยบล็อกการลบวัตถุที่ถูกนำไปใช้ในกฎ (mockSync.ts) พร้อมระบบเปลี่ยนชื่อลามเปลี่ยนในกฎทั้งหมด (Rename Propagation)
* **Custom UI Alerts [สำเร็จ]:** พัฒนาและติดตั้ง Custom AlertDialog ([AlertDialogProvider.tsx](file:///home/sapray/dev/pigate/frontend/src/components/AlertDialogProvider.tsx)) แทนการใช้ `alert()` และ `confirm()` ดั้งเดิมของเบราว์เซอร์
* **Strict Input Validation [สำเร็จ]:** ติดตั้งระบบตรวจสอบรูปแบบ IPv4, CIDR Subnet และ IP Range อย่างรัดกุม (ตรวจสอบ Octet ละเอียด 0-255) ในทุกหน้าอินพุต
* **ระบบสวิตช์ความปลอดภัยการแก้ไขเส้นทางระบบปฏิบัติการ (System Route Safety Switch) [สำเร็จ]:**
  * เพิ่มสวิตช์ความปลอดภัยควบคุมการแก้ไขเส้นทางระดับระบบ (`uiEditSystemRouteActive`) โดยแสดงผลเมื่อหลังบ้านอนุญาตสิทธิ์เท่านั้น
  * มีการเชื่อมหน้าต่างแจ้งเตือนยืนยัน (Confirm Dialog) ก่อนสลับโหมด พร้อมกู้คืนค่าสถานะเดิมหากผู้ใช้ยกเลิก
  * ปรับเปลี่ยนปุ่มยื่นยันเส้นทางดั้งเดิม (Apply Config) เป็นปุ่มโหลดข้อมูลซ้ำ (Refresh) เนื่องจากหลังบ้านทำการอัปเดตปรับใช้อัตโนมัติทันทีที่มีการแก้ไข

### 1.2 สถาปัตยกรรมและหลังบ้าน (Backend API & Database - Go v1.26.4)
* **Single Binary Embedded Server [สำเร็จ]:** เชื่อมฝังเว็บ React SPA (`dist/` folder) เข้าใน Go binary ด้วย `go:embed` ใน [embed.go](file:///home/sapray/dev/pigate/backend/internal/api/embed.go) รองรับ Client-side routing fallback (SPA fallback)
* **SQLite Database Layer [สำเร็จ]:** ออกแบบฐานข้อมูล SQLite ด้วย pure-Go client (`modernc.org/sqlite` แบบไม่มี CGO dependency) เพื่อเก็บข้อมูล Interfaces, Routes, DNS, DHCP, Addresses, Services, และ Policies
* **Service Layer Refactoring [สำเร็จ]:** แยกโครงสร้างตรรกะ DB และ Kernel พัฒนาเป็น Service Layer ได้แก่ [InterfaceService](file:///home/sapray/dev/pigate/backend/internal/service/interface.go) และ [RoutingService](file:///home/sapray/dev/pigate/backend/internal/service/routing.go) เพื่อไม่ให้ API handlers เรียกใช้ชั้นข้อมูลโดยตรง
* **CORS, Rate Limiting & Auth Middleware [สำเร็จ]:**
  * CORS Middleware ปรับสิทธิ์เฉพาะ Origin ที่ปลอดภัย (ป้องกัน CORS Credentials Mismatch)
  * ติดตั้งระบบรักษาความปลอดภัยจำกัดอัตราการขอเข้าถึง (Rate Limiting) ในหน้าล็อกอิน และ Middleware ตรวจสอบ Bearer Token
  * ระบบยืนยันสิทธิ์เซสชันย้อนกลับ `/api/auth/session` พร้อมระบบบังคับเปลี่ยนรหัสผ่านครั้งแรก (IsInitial check) บล็อกการเข้าถึง endpoint อื่นและบังคับนำไปสู่หน้า `/change-password`
* **REST API Endpoints ทั้งหมด (10 กลุ่ม) [สำเร็จ]:**
  * **Authentication:** POST /auth/login, POST /auth/logout, GET /auth/session
  * **Dashboard:** GET /dashboard/stats, GET /dashboard/performance, GET /dashboard/traffic, GET /dashboard/logs, POST /dashboard/logs/clear, GET /dashboard/logs/stream (SSE), GET /system/info
  * **Network Interfaces:** GET, PUT, PATCH, DELETE /interfaces, POST /interfaces/{id}/toggle, POST /interfaces/{id}/reset, GET /interfaces/{id}/scan, GET /interfaces/{id}/wifi-status
  * **Firewall Policies:** GET, POST /policies, PUT, DELETE /policies/{id}, PUT /policies/reorder, POST /policies/{id}/toggle-log, POST /policies/{id}/toggle-status, POST /policies/apply
  * **Address Objects:** GET, POST /addresses, PUT, DELETE /addresses/{id}, POST /addresses/bulk-delete
  * **Service Objects:** GET, POST /services, PUT, DELETE /services/{id}
  * **Static Routes:** GET, POST /routes, PUT, DELETE /routes/{id}, GET /routes/config, POST /routes/bulk-delete, POST /routes/{id}/toggle, POST /routes/apply
  * **DHCP Server:** GET, PUT /dhcp/config, GET, POST /dhcp/reservations, PUT, DELETE /dhcp/reservations/{id}, GET /dhcp/leases, POST /dhcp/apply
  * **System & Maintenance:** GET, PUT /system/time, GET, PUT /system/dns, PUT /system/password, GET /system/services, POST /system/services/{id}/restart, POST /system/reboot, POST /system/shutdown, GET /system/config/export, POST /system/config/import
  * **QoS Bandwidth Control:** GET, POST /qos/rules, GET, PUT, DELETE /qos/rules/{id}, POST /qos/rules/{id}/toggle, POST /qos/sync, GET /qos/status/{iface}, DELETE /qos/iface/{iface}
* **Automated Testing Suite [สำเร็จ]:** พัฒนาชุดทดสอบ Unit test (จำลอง SQLite) และ Integration test (จำลอง http client) บิวด์และทดสอบผ่านสำเร็จ 100%
* **การจำแนกประเภทและอัปเดตสคีมา Static Routing ใหม่ [สำเร็จ]:**
  * แยกประเภทความชัดเจนของ Static Route เป็น `custom` และ `customgateway`
  * อัปเดต SQLite CHECK constraint ให้ยอมรับประเภทเส้นทางแบบใหม่ และรัน Database Migration อัตโนมัติ
  * พัฒนาระบบคลี่คลายค่าและติดตาม Default Gateway โดยแปลง IP เกตเวย์ปัจจุบันเป็นคำเฉพาะ `"default"` ลงฐานข้อมูล SQLite และสลับกลับมาเป็นไอพีจริงแบบไดนามิกขณะอ่านค่าหรือใช้งาน

### 1.3 การทำงานเชื่อมต่อระบบปฏิบัติการ (Kernel Integration)
* **Real Network Interface Control (Netlink & wpa_supplicant) [สำเร็จ]:**
  * จัดการเปิด/ปิดอินเตอร์เฟส (`ToggleInterface`) ด้วย `github.com/vishvananda/netlink` สื่อสารผ่าน Netlink Socket โดยตรง ป้องกัน Command Injection
  * พัฒนา `ConfigureInterface` ใน [real_network.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_network.go) ล้างการตั้งค่าเก่า และลงทะเบียน IP, Netmask, Gateway บน Linux Interface
  * การบันทึกและเขียนไฟล์การเชื่อมต่อ `wpa_supplicant` แบบอะตอมมิก (Atomic writes) เพื่อความปลอดภัย
  * พัฒนาการค้นหาคลื่น Wi-Fi ด้วย Netlink (nl80211) สแกนผ่านไลบรารี `github.com/mdlayher/wifi` โดยตรง พร้อมระบบจัดลำดับเครือข่ายตามระดับความแรงสัญญาณ และกรองเฉพาะการ์ดแบบ wireless
  * ระบบตรวจสอบและแสดงผลข้อมูลสถานะเชื่อมต่อไร้สายจริง (SSID, State, BSSID, Active MAC, frequency, KeyMgmt, WiFi Gen) ผ่าน UNIX domain control socket ของ `wpa_supplicant` (`unixgram` socket)
  * รองรับระบบสุ่ม MAC Address (MAC Address Randomization) ผ่านกลไกตัวสุ่มดั้งเดิมของ `wpa_supplicant` (`preassoc_mac_addr=1` และ `mac_addr=1`)
* **Real Routing Table Control (Netlink) [สำเร็จ]:**
  * พัฒนา `RealRouting` ใน [real_routing.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_routing.go) เพิ่ม ลบ และปรับปรุงเส้นทางเครือข่ายบนตาราง Routing ของ Linux โดยตรงผ่าน Netlink
  * ปรับระดับการประมวลผลให้รับรอง Scope, Metrics, Protocol, IP แหล่งต้นทาง และระบบคัดแยก/ล็อกการแก้ไขเส้นทางของระบบ (System Routes)
* **Netlink Event Monitor & Routing Self-Healing [สำเร็จ]:**
  * พัฒนา `NetlinkMonitor` ใน [netlink_monitor.go](file:///home/sapray/dev/pigate/backend/internal/service/netlink_monitor.go) คอยดักฟังการแจ้งเตือนการเปลี่ยนแปลง Network Link, IP Address และ Route จาก Kernel
  * ติดตั้งระบบ Debouncer (500ms) เพื่อรวมกลุ่มเหตุการณ์ และสั่งกระตุ้นการประสานงาน (Reconcile) นำเอาคอนฟิก Static Route จาก SQLite ไปเขียนทับลง Kernel อัตโนมัติ ป้องกันการดริฟต์เครือข่ายภายนอก
  * ปรับปรุงสิทธิ์การใช้ Protocol ID `120` ใน [real_routing.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_routing.go) ให้ครอบคลุมประเภท `customgateway` ป้องกันการตรวจเช็คผิดพลาด
  * แก้ไขปัญหาลูปสัญญาณย้อนกลับ (Netlink Reconciliation loop) ในกรณีเปลี่ยน Metric (Priority) โดยตรวจสอบและลบเส้นทางเก่าออกก่อนผ่าน `RouteDel` จากนั้นจึงสั่งสร้างใหม่ด้วย `RouteAdd` แทนการสั่งเปลี่ยนทับค่าเดิม
* **QoS Bandwidth Control (HTB + IFB via tc Netlink) [สำเร็จ]:**
  * พัฒนา `QosService` และ `RealQosManager` สำหรับการควบคุมแบนด์วิดธ์ทั้ง Egress และ Ingress
  * ใช้ HTB (Hierarchical Token Bucket) สำหรับ Egress Shaping และ IFB (Intermediate Functional Block) สำหรับ Ingress Shaping
  * รองรับการจับคู่ตาม Source/Destination IP (CIDR)
  * REST API ครบ: GET/POST/PUT/DELETE /qos/rules, toggle, sync, status, clear
  * `SyncToKernel()` ทำงานแบบ idempotent (ล้างแล้วสร้างใหม่)
  * ดู kernel status ได้แบบ real-time ผ่าน `GET /api/qos/status/{iface}`
* **D-Bus Service Management [สำเร็จ]:**
  * เปลี่ยนจากการรัน `sudo systemctl` ผ่าน exec.Command ไปใช้ D-Bus (`github.com/godbus/dbus/v5`) โดยตรง
  * ฟังก์ชัน `IsServiceActiveViaDBus`, `StartServiceViaDBus`, `StopServiceViaDBus`, `RestartServiceViaDBus`
  * ใช้ใน: จัดการ `wpa_supplicant@<iface>` และ `systemd-resolved.service`
  * ลดความจำเป็นของ sudo ลงเหลือเพียง `dhcpcd` เท่านั้น
* **Real Firewall Rules Integration (nftables via Netlink) [สำเร็จ]:**
  * พัฒนา `RealFirewall` ใน [real_firewall.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_firewall.go) โดยใช้ `github.com/google/nftables` ในการคุยกับเคอร์เนลผ่าน Netlink Socket
  * สร้างตาราง `pigate` (inet family) ควบคุมกฎ Dynamic Rules ใน `forward` chain
  * สร้างโครงสร้างห่วงโซ่ตรวจเช็คความปลอดภัยขั้นพื้นฐาน (established/related accepts, invalid drops, broadcast drops, default drops)
  * ออกแบบและติดตั้งระบบแชร์อินเทอร์เน็ต NAT (Postrouting/Masquerading) บนการ์ดขาออกที่ถูกระบุว่าเป็น `WAN` (`Role = WAN`) โดยอัตโนมัติลงในตาราง `pigate_nat`
  * ออกแบบระบบป้องกัน IP Spoofing ผ่าน `pigate-not-local` พร้อมระบุขอบเขตความปลอดภัยจำกัดสิทธิ์ (Security Isolation) ของกฎผู้ใช้ให้ทำงานเฉพาะใน `forward` เท่านั้น (ป้องกันการบล็อกเว็บควบคุมหรือ SSH)
  * ระบบแอดมินจำกัดสิทธิ์เข้าถึง (Admin Access Rules) สำหรับจัดการสิทธิ์ PING, HTTP (80/2479), HTTPS (443), และ SSH (22) รายอินเตอร์เฟส
  * สนับสนุนโหมดหลบเลี่ยงความขัดแย้งของ Docker (Docker Compatibility bypass) ด้วย CLI flag `-docker-compat` เพื่อข้ามการบล็อกการ์ด `docker0` และบริดจ์เสมือน `br-*` อัตโนมัติ
* **Real DHCP Server (dnsmasq) [สำเร็จ]** (ดู [dnsmasq-design.md](ref/complete/dnsmasq-design.md)):
  * พัฒนา `RealDhcpManager` ใน [dhcp_server.go](file:///home/sapray/dev/pigate/backend/internal/kernel/dhcp_server.go) สร้างไฟล์คอนฟิก `/etc/dnsmasq.d/pigate-dhcp.conf` (พร้อม base config) จากข้อมูล SQLite รองรับหลายอินเตอร์เฟส, IP Pool, Lease Time และ Static Reservations
  * ตรวจสอบไวยากรณ์คอนฟิกก่อนใช้งานจริงด้วย `dnsmasq --test` บนไฟล์ชั่วคราว แล้วจึงเขียนทับและสั่ง restart `dnsmasq.service` ผ่าน systemd D-Bus
  * พัฒนาระบบเฝ้าดู DHCP Lease แบบเรียลไทม์ (Lease Watcher) ผ่าน D-Bus signal ของ dnsmasq (`uk.org.thekelleys.dnsmasq`) แสดงผล Active Leases บนหน้าเว็บ
* **Real DNS Server — Local Zones/FQDN (dnsmasq) [สำเร็จ]** (ดู [dnsmasq-design.md](ref/complete/dnsmasq-design.md)):
  * พัฒนา `RealDNSServerManager` ใน [dns_server.go](file:///home/sapray/dev/pigate/backend/internal/kernel/dns_server.go) สร้างไฟล์คอนฟิก `/etc/dnsmasq.d/pigate-dns.conf` รองรับ Local DNS Zone, A/CNAME Records และ Authoritative Zone
  * แยกการเลือกอินเตอร์เฟสรับฟัง (Listen Interfaces) ของ DNS Server ออกจาก DHCP Server อย่างอิสระ
* **DHCP Client Manager (dhcpcd) [สำเร็จ]:**
  * พัฒนา `DhcpcdManager` ใน [dhcpcd.go](file:///home/sapray/dev/pigate/backend/internal/kernel/dhcpcd.go) ควบคุมวงจรชีวิต `dhcpcd@<iface>.service` (WAN-side DHCP client) ผ่าน systemd D-Bus โดย `install.sh` สร้าง systemd template service ให้ dhcpcd รันเป็น root unit ของตัวเอง — ตัดความจำเป็นของ sudo ออกทั้งหมด

### 1.4 ระบบจัดการระดับระบบและบัญชีผู้ใช้ (System Management Features)
* **System Hostname [สำเร็จ]** (ดู [hostname-setting-design.md](ref/complete/hostname-setting-design.md)):
  * จัดการ Hostname (static + transient) ผ่าน `systemd-hostnamed` D-Bus ใน [real_hostname.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_hostname.go) พร้อมประยุกต์ใช้ค่าตอนบูตและ restart DHCP client บนอินเตอร์เฟสที่เกี่ยวข้อง
* **System Time / NTP [สำเร็จ]** (ดู [time-system-design.md](ref/complete/time-system-design.md)):
  * ตั้งค่า Timezone, เปิด/ปิด NTP, กำหนด NTP Server (drop-in ของ `systemd-timesyncd`) และตั้งเวลาแบบ Manual ผ่าน `systemd-timedated` D-Bus ใน [real_timedate.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_timedate.go) พร้อมฝัง IANA tzdata ในไบนารี (`time/tzdata`)
* **User System [สำเร็จ]** (ดู [user-system-design.md](ref/complete/user-system-design.md)):
  * ระบบจัดการผู้ใช้หลายบัญชี (สร้าง/แก้ไข/ลบ/เปิด-ปิด) พร้อมบทบาท `super_admin` / `admin_readonly`, Session-based Auth ตรวจสอบกับฐานข้อมูลรายคำขอ, Middleware ตรวจสิทธิ์ตามบทบาท, Rate Limiting หน้าล็อกอิน และบังคับเปลี่ยนรหัสผ่านครั้งแรก
* **Export/Import Configuration [สำเร็จ]** (ดู [export-import-system-design.md](ref/complete/export-import-system-design.md)):
  * Typed JSON Backup (schema v2) พร้อม SHA-256 integrity, เข้ารหัสด้วย Passphrase ได้ (AES-256-GCM + Argon2id), รองรับไฟล์ v1 เดิม
  * Import แบบ validate → snapshot → wipe & restore ใน transaction เดียว → re-apply ลงเคอร์เนลตามลำดับบูต จำกัดสิทธิ์ `super_admin` พร้อมกันการล็อกตัวเองออก (Actor Lock-out Guard)
* **Per-Interface Route Metric [สำเร็จ]** (ดู [interface-metric-design.md](ref/complete/interface-metric-design.md)):
  * กำหนดค่า Metric ของเส้นทางรายอินเตอร์เฟสเพื่อรองรับ Multi-WAN Failover

---

## 2. ปัญหาและประเด็นที่ต้องพิจารณาในปัจจุบัน (Current Issues & Limitations)

> [!NOTE]
> **ความปลอดภัยของระบบหลังบ้าน (Backend Security Hardening):**
> * ได้ทำการแก้ปัญหาความปลอดภัยร้ายแรงเรียบร้อยแล้ว เช่น การปิด backdoor การตรวจสอบสิทธิ์ Bcrypt ใน `HandleLogin` และแก้ไขสิทธิ์ CORS Credentials Origin เพื่อรักษาสมดุลความมั่นคงปลอดภัยตามมาตรฐาน Web API
> * ปิดบังการส่งรหัสผ่าน Wi-Fi ทุกรูปแบบกลับมาที่ API Response โดยใช้การ Masking เป็น `"••••••••"` ใน API ขาออก และกำหนดให้ข้ามการบันทึกทับรหัสผ่านหากฝั่งหน้าบ้านส่งค่า Masked เข้ามา (PATCH Method)

> [!IMPORTANT]
> **ข้อจำกัดในการทดสอบระดับ Kernel Integration (Kernel Mode Limitations):**
> * การเข้าถึงเพื่อจัดการ IP Address, Routing Table, และ nftables ผ่าน Netlink จะต้องใช้สิทธิ์ของระบบปฏิบัติการระดับสูง (`root` หรือการตั้งค่า Linux Capabilities ในแบบ `cap_net_admin,cap_net_raw+ep`)
> * ในระหว่างการพัฒนาบนเครื่องคอมพิวเตอร์ทั่วไป (Development PC) จะเปิดใช้งานโหมดจำลอง (`-mock=true`) เพื่อเก็บสเตตทำงานทั้งหมดไว้บน SQLite และ Memory เสมอ โดยข้อมูลแลน/Wi-Fi (eth0, wlan0) จะไม่ถูก Seeding ในโหมดจริงเพื่อป้องกันการทับซ้อนกับการ์ดจริงของเครื่องเซิร์ฟเวอร์
> * ฟีเจอร์ที่ยังเป็นการจำลอง (Mock) อยู่: **Firewall Rule Counters**, **Firewall Log Streaming** (อ่านจาก In-Memory Ring Buffer ที่ seed ข้อมูลตัวอย่าง ยังไม่ได้อ่าน Kernel Log จริง) และ **รายการ/รีสตาร์ท System Services** — ส่วน **Power Control (Reboot/Shutdown)** ทำงานจริงแล้วผ่าน `org.freedesktop.login1` D-Bus — ส่วนค่า **Dashboard System Status** (CPU/RAM/Temp/Storage/Uptime/OS/Bandwidth) เป็นข้อมูลจริงแล้ว (อ่านจาก `/proc`, `/sys`, `statfs`, netlink) เช่นเดียวกับ DHCP Lease Count และสถานะ Wi-Fi

> [!WARNING]
> **งานคุณภาพโค้ดที่ค้างอยู่ (ดู [docs/ref/todo/](ref/todo/)):**
> * **Frontend Lint ยังไม่ผ่าน** — `yarn lint` fail 141 ปัญหา (132 errors, 9 warnings) ใน 27 ไฟล์ มีแผนแก้ไขแล้วที่ [frontend-lint-check-plan.md](ref/todo/frontend-lint-check-plan.md)
> * **Systemd D-Bus Call ยังกระจายหลายไฟล์** — logic การเรียก `org.freedesktop.systemd1` ซ้ำกันใน `real_network.go` / `dns.go` มีแผน refactor รวมศูนย์ที่ [systemd-dbus-call-design.md](ref/todo/systemd-dbus-call-design.md)

---

## 3. แผนการดำเนินงานระยะถัดไป (Roadmap & Next Steps)

เพื่อให้โปรเจกต์บรรลุเป้าหมายการทำงานระดับ Kernel สมบูรณ์แบบ รายการและแผนการทำงานถัดไปถูกระบุไว้ดังนี้:

### 3.1 งานเชื่อมโยงระดับเคอร์เนลและระบบเครือข่ายเพิ่มเติม (Pending Kernel Integration Tasks)
* [ ] **พัฒนาระบบรักษาความปลอดภัยป้องกันการล็อกตัวเองออก (Fail-Safe Rollback / Test & Rollback):**
  * พัฒนากลไกรักษาความปลอดภัยในกรณีผู้ดูแลระบบตั้งกฎไฟร์วอลล์ผิดพลาดจนบล็อกพอร์ตเว็บควบคุมหรือ SSH ของตัว PiGate เอง
  * เมื่อสั่งยืนยัน Apply กฎใหม่ ระบบหลังบ้านจะตั้งเวลาถอยหลัง 30 วินาที หากเบราว์เซอร์ของผู้ใช้ไม่มีการส่งสัญญาณยอมรับ (Confirm/Keepalive) กลับมา ระบบจะดึงข้อมูลคอนฟิกชุดเก่าขึ้นมาบังคับรีสโตร์ทันที
* [ ] **พัฒนาระบบและหน้าจอสลับ Docker Compatibility บน Web UI:**
  * พัฒนา API และปุ่มสลับสถานะ (Settings Toggle) บนหน้าเว็บของแอดมิน เพื่อควบคุมสถานะการเปิด/ปิด Docker Compatibility (`-docker-compat`) โดยตรงผ่านฐานข้อมูล SQLite แทนการระบุผ่าน CLI flag ตอนสตาร์ทโปรแกรม
* [x] **เชื่อมโยงการประยุกต์ใช้คอนฟิกตอนบูตระบบ (Startup Apply Integration) [สำเร็จ]:**
  * `main.go` นำค่าคอนฟิกจาก SQLite ไปประยุกต์ใช้บนเคอร์เนลตอนบูตครบทุกระบบตามลำดับ: Time/NTP → Interfaces → Static Routes → Netlink Monitor → Hostname → DHCP Client Sync → DHCP Server → DNS Local Zones → DNS Settings → Firewall → QoS
* [x] **พัฒนาระบบ DHCP Server (Real DHCP Manager) [สำเร็จ]:**
  * สร้างคอนฟิก `dnsmasq` (`/etc/dnsmasq.d/pigate-dhcp.conf`) พร้อมตรวจสอบไวยากรณ์ (`dnsmasq --test`), restart ผ่าน systemd D-Bus และ Lease Watcher แบบเรียลไทม์ผ่าน D-Bus signal — ดูรายละเอียดที่ [dnsmasq-design.md](ref/complete/dnsmasq-design.md)
* [x] **พัฒนาระบบ DNS Server (Real DNS Manager) [สำเร็จ]:**
  * Local DNS Resolution / FQDN สำหรับ client ภายในเครือข่ายผ่าน `dnsmasq` (`/etc/dnsmasq.d/pigate-dns.conf`) รองรับ Zone/Record และ Authoritative Zone — ดูรายละเอียดที่ [dnsmasq-design.md](ref/complete/dnsmasq-design.md)
* [ ] **ติดตั้งระบบสถิติกฎไฟร์วอลล์จริง (Live Rule Counters Telemetry):**
  * เรียกใช้ข้อมูล Hit packet และ byte counters จาก `expr.Counter` บน nftables rules นำเสนอเป็นข้อมูล Telemetry เรียลไทม์ผ่าน API ไปแสดงผลในตารางกฎหน้าเว็บโดยไม่ต้องสั่งเขียนบันทึกลง SD card ของบอร์ด
* [x] **พัฒนาระบบล็อกทราฟฟิกที่วิ่งผ่าน (Forward Traffic Log) [สำเร็จ]:**
  * เปลี่ยนปลายทาง log ของ forward chain (PASS/DROP) จาก printk/dmesg → **NFLOG group 100** แล้วเปิด listener ฝั่ง Go ด้วย `github.com/florianl/go-nflog` (pure Go, ไม่มี CGO, ต่อยอด `mdlayher/netlink` ที่ pin อยู่แล้ว) — parse header IPv4/IPv6 + TCP/UDP เป็น `model.FirewallLog` เข้า In-Memory Ring Buffer (capacity 500) เท่านั้น **ไม่เขียน SQLite** (ถนอม SD card, ตาม §8 ของ tech_stack_design)
  * เลือกทาง NFLOG แทน `/dev/kmsg` (ต้องเพิ่ม `cap_syslog` + parse text ที่ไม่ stable) และแทน journald (ต้องใช้ CGO) — คง CAP_NET_ADMIN เดิม; callback ฝั่ง Go non-blocking + ทิ้ง event ส่วนเกินตอน burst; mock mode ใช้ generator ล้วน (ไม่เปิด socket)
  * หน้าใหม่ **Log & Report › Forward Traffic** (filter PASS/DROP, ค้น src/dest/port/reason, pause/resume polling 5 วิ, clear) + Dashboard Recent Logs กลายเป็นข้อมูลจริงจาก buffer เดียวกันโดยอัตโนมัติ; เส้น API ใหม่ `GET /api/logs/traffic`
  * _(ยังไม่ทำในเฟสนี้: log ของ input chain ยังยิงเข้า dmesg ตามเดิม, ไม่ทำ SSE/persist/remote syslog)_
* [x] **พัฒนาข้อมูล Dashboard จริง (Real Dashboard Metrics):** _(เสร็จแล้ว)_
  * แทนที่ค่าจำลอง Total Traffic In/Out และ CPU/RAM/Temperature/Storage/Uptime/OS ด้วยข้อมูลจริงจากระบบผ่าน `SystemStatsManager` (อ่าน `/proc/stat`, `/proc/meminfo`, `/proc/cpuinfo`, `/sys/class/thermal`, `/sys/.../cpufreq`, `statfs`, `/etc/os-release`, `/proc/uptime`, `/proc/device-tree/model` และ netlink interface counters — ไม่มี shell exec)
  * CPU usage คำนวณจาก background sampler (2 snapshots), traffic history เก็บใน RAM ring buffer (288 buckets × 5 นาที = 24 ชม., ไม่เขียน SQLite), ค่า optional (temp/freq/board) degrade เป็น `available:false`/omit บนเครื่องที่ไม่มี sysfs node (เช่น WSL/x86)
  * เส้น API: `GET /api/dashboard/performance` (อัปเกรด), `GET /api/system/info` (ใหม่), `GET /api/dashboard/traffic` (ใหม่), `GET /api/dashboard/stats` (traffic จริง)
* [ ] **พัฒนาระบบจัดการ System Services จริง:**
  * แทนที่รายการ Services จำลอง (`HandleGetSystemServices` / `HandleRestartService`) ด้วยการอ่านสถานะและสั่ง restart unit จริงผ่าน systemd D-Bus
* [x] **พัฒนาระบบ Power Control จริง (Reboot/Shutdown):**
  * เชื่อม `POST /api/system/reboot` และ `/api/system/shutdown` เข้ากับ `org.freedesktop.login1` (systemd-logind) ผ่าน D-Bus พร้อม Polkit rule แล้ว (super_admin เท่านั้น, หน่วง ~1 วินาทีก่อนสั่งจริงเพื่อ flush HTTP response, mock mode เป็น no-op)
* [x] **พัฒนาระบบ Central Event Log (audit trail) [สำเร็จ]:**
  * `EventLogService` เป็นจุดบันทึกเหตุการณ์กลางทั้งระบบ: login success/failed, password changed, user CRUD, network/firewall/route/DHCP/DNS changes, DHCP lease add/remove, config export/import และ reboot/shutdown/boot — persist ลง SQLite ข้าม reboot ด้วย async batch writer แบบถนอม SD card (flush ทุก 10 events / 10 วินาที, cap 10,000 แถว, synchronous flush ก่อนสั่ง power)
  * เส้น API: `GET /api/logs/events` (filter/paging, ทุก role), `POST /api/logs/events/clear` (super_admin เท่านั้น, ทิ้ง audit row `config.logs_cleared` เสมอ) + หน้า UI ใหม่ System › Event Logs — ดูรายละเอียดที่ [central-event-log-system-plan.md](ref/complete/central-event-log-system-plan.md)
  * เหลือเฉพาะการทดสอบคู่ event `system.reboot`/`system.boot` บนบอร์ดจริง

### 3.1.1 งานปรับปรุงคุณภาพโค้ด (Code Quality Tasks — ดูแผนใน [docs/ref/todo/](ref/todo/))
* [ ] **รวมศูนย์ Systemd D-Bus Call:** refactor ฟังก์ชัน `IsServiceActiveViaDBus` / `StartServiceViaDBus` / `StopServiceViaDBus` / `RestartServiceViaDBus` ที่ซ้ำกันใน `real_network.go` และ `dns.go` ไปไว้ไฟล์เดียว (`dbus_systemd.go`) — แผนที่ [systemd-dbus-call-design.md](ref/todo/systemd-dbus-call-design.md)
* [ ] **แก้ Frontend Lint ให้ผ่านทั้งหมด:** `yarn lint` ยัง fail 141 ปัญหา (132 errors, 9 warnings) ใน 27 ไฟล์ — แผนที่ [frontend-lint-check-plan.md](ref/todo/frontend-lint-check-plan.md)

### 3.2 งานตรวจสอบคุณภาพและการทดสอบบนฮาร์ดแวร์จริง (QA & Hardware Testing)
* [ ] **ทดสอบบนบอร์ด Raspberry Pi 5 จริง:**
  * ติดตั้งและทดสอบโปรแกรมภายใต้ระบบปฏิบัติการ Linux ของ Raspberry Pi 5
  * กำหนดสิทธิ์และความปลอดภัยระดับไฟล์รันไบนารี (`sudo setcap cap_net_admin,cap_net_raw+ep ./pigate`) เพื่อทดสอบการจัดการ Netlink/nftables ในโหมดไร้สิทธิ์ root (Non-root security verification)
* [ ] **ทำการจำลองโหลดเครือข่ายและสตรีมมิ่ง:** เพื่อวัดระดับความหน่วงของการประมวลผล (Latency) และปริมาณอัตราความเร็วข้อมูล (Throughput) เมื่อมีการใช้ Masquerading และกฎไฟร์วอลล์ระดับร้อยข้อ

### 3.3 ระบบติดตั้งและความปลอดภัย (Installation & Security)
* [x] **install.sh — Script ติดตั้งอัตโนมัติ [สำเร็จ]:**
  * สร้าง system user `pigate` (non-root) + กลุ่ม `netdev`
  * ตั้งค่า ACL สำหรับ `/etc/wpa_supplicant` และ `/etc/systemd/resolved.conf.d`
  * สร้าง polkit rule `/etc/polkit-1/rules.d/10-pigate-wpa.rules` — อนุญาต pigate จัดการ `wpa_supplicant@*` และ `systemd-resolved.service` ผ่าน D-Bus
  * sudoers เฉพาะ `/usr/sbin/dhcpcd` เท่านั้น
  * ตั้งค่า `setcap cap_net_admin,cap_net_raw+ep` บน binary
  * สร้าง systemd service พร้อม `AmbientCapabilities` และ `CapabilityBoundingSet`
