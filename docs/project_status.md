# PiGate Project Status & Roadmap

เอกสารฉบับนี้เป็นรายงานสรุปสถานะล่าสุดของโปรเจกต์ **PiGate** (ระบบควบคุม Raspberry Pi Firewall/Gateway) ทั้งความคืบหน้า โครงสร้างระบบล่าสุด ปัญหาที่พบ และแผนงานในอนาคต

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
* **Real Firewall Rules Integration (nftables via Netlink) [สำเร็จ]:**
  * พัฒนา `RealFirewall` ใน [real_firewall.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_firewall.go) โดยใช้ `github.com/google/nftables` ในการคุยกับเคอร์เนลผ่าน Netlink Socket
  * สร้างตาราง `pigate` (inet family) ควบคุมกฎ Dynamic Rules ใน `forward` chain
  * สร้างโครงสร้างห่วงโซ่ตรวจเช็คความปลอดภัยขั้นพื้นฐาน (established/related accepts, invalid drops, broadcast drops, default drops)
  * ออกแบบและติดตั้งระบบแชร์อินเทอร์เน็ต NAT (Postrouting/Masquerading) บนการ์ดขาออกที่ถูกระบุว่าเป็น `WAN` (`Role = WAN`) โดยอัตโนมัติลงในตาราง `pigate_nat`
  * ออกแบบระบบป้องกัน IP Spoofing ผ่าน `pigate-not-local` พร้อมระบุขอบเขตความปลอดภัยจำกัดสิทธิ์ (Security Isolation) ของกฎผู้ใช้ให้ทำงานเฉพาะใน `forward` เท่านั้น (ป้องกันการบล็อกเว็บควบคุมหรือ SSH)
  * ระบบแอดมินจำกัดสิทธิ์เข้าถึง (Admin Access Rules) สำหรับจัดการสิทธิ์ PING, HTTP (80/2479), HTTPS (443), และ SSH (22) รายอินเตอร์เฟส
  * สนับสนุนโหมดหลบเลี่ยงความขัดแย้งของ Docker (Docker Compatibility bypass) ด้วย CLI flag `-docker-compat` เพื่อข้ามการบล็อกการ์ด `docker0` และบริดจ์เสมือน `br-*` อัตโนมัติ

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
> * ฟีเจอร์ DNS Server, DHCP Server, Counter และ Log Streaming ปัจจุบันได้รับการจำลองพฤติกรรมบน UI และจัดเก็บข้อมูลลง SQLite เรียบร้อย แต่รอการพัฒนาตัวเชื่อมแกนหลักระบบปฏิบัติการ (Real OS integration write) ในอนาคต

---

## 3. แผนการดำเนินงานระยะถัดไป (Roadmap & Next Steps)

เพื่อให้โปรเจกต์บรรลุเป้าหมายการทำงานระดับ Kernel สมบูรณ์แบบ รายการและแผนการทำงานถัดไปถูกระบุไว้ดังนี้:

### 3.1 งานเชื่อมโยงระดับเคอร์เนลและระบบเครือข่ายเพิ่มเติม (Pending Kernel Integration Tasks)
* [ ] **พัฒนาระบบรักษาความปลอดภัยป้องกันการล็อกตัวเองออก (Fail-Safe Rollback / Test & Rollback):**
  * พัฒนากลไกรักษาความปลอดภัยในกรณีผู้ดูแลระบบตั้งกฎไฟร์วอลล์ผิดพลาดจนบล็อกพอร์ตเว็บควบคุมหรือ SSH ของตัว PiGate เอง
  * เมื่อสั่งยืนยัน Apply กฎใหม่ ระบบหลังบ้านจะตั้งเวลาถอยหลัง 30 วินาที หากเบราว์เซอร์ของผู้ใช้ไม่มีการส่งสัญญาณยอมรับ (Confirm/Keepalive) กลับมา ระบบจะดึงข้อมูลคอนฟิกชุดเก่าขึ้นมาบังคับรีสโตร์ทันที
* [ ] **พัฒนาระบบและหน้าจอสลับ Docker Compatibility บน Web UI:**
  * พัฒนา API และปุ่มสลับสถานะ (Settings Toggle) บนหน้าเว็บของแอดมิน เพื่อควบคุมสถานะการเปิด/ปิด Docker Compatibility (`-docker-compat`) โดยตรงผ่านฐานข้อมูล SQLite แทนการระบุผ่าน CLI flag ตอนสตาร์ทโปรแกรม
* [ ] **เชื่อมโยงการประยุกต์ใช้คอนฟิกตอนบูตระบบ (Startup Apply Integration):**
  * ยกเลิกการคอมเมนต์ในไฟล์ `main.go` เพื่อเปิดใช้งานกระบวนการนำเอาค่าคอนฟิกของ Firewall (nftables), DHCP Server และ DNS Settings ล่าสุดจากฐานข้อมูล SQLite ไปลงทะเบียนติดตั้งบนเคอร์เนลโดยอัตโนมัติขณะตัวเซิร์ฟเวอร์หลังบ้านกำลังบูตสตาร์ท
* [ ] **พัฒนาระบบ DHCP Server (Real DHCP Manager):**
  * พัฒนาระดับชั้นเขียนคอนฟิก (Real Config Writer) เพื่อจัดส่งไฟล์และคำสั่งสตาร์ทบริการให้กับ `dnsmasq` หรือ `isc-dhcp-server` บน Linux Host
* [ ] **พัฒนาระบบ DNS Server (Real DNS Manager):**
  * พัฒนาระดับชั้นจัดการแก้ไขไฟล์ `/etc/resolv.conf` หรือระบบจัดการ Local DNS Resolution บนเซิร์ฟเวอร์จริง
* [ ] **ติดตั้งระบบสถิติกฎไฟร์วอลล์จริง (Live Rule Counters Telemetry):**
  * เรียกใช้ข้อมูล Hit packet และ byte counters จาก `expr.Counter` บน nftables rules นำเสนอเป็นข้อมูล Telemetry เรียลไทม์ผ่าน API ไปแสดงผลในตารางกฎหน้าเว็บโดยไม่ต้องสั่งเขียนบันทึกลง SD card ของบอร์ด
* [ ] **พัฒนาระบบล็อกความปลอดภัยสตรีมสด (Firewall Logs Stream):**
  * พัฒนาตัวอ่านข้อมูล Kernel Logs (`/dev/kmsg` หรือ Journald) ที่มีข้อความ Prefix เป็น `[PiGate]` นำเข้าสู่ In-Memory Ring Buffer และจัดส่งเป็นสตรีมข้อมูลสด (Server-Sent Events) ไปแสดงบนหน้า Dashboard ของแอดมิน

### 3.2 งานตรวจสอบคุณภาพและการทดสอบบนฮาร์ดแวร์จริง (QA & Hardware Testing)
* [ ] **ทดสอบบนบอร์ด Raspberry Pi 5 จริง:**
  * ติดตั้งและทดสอบโปรแกรมภายใต้ระบบปฏิบัติการ Linux ของ Raspberry Pi 5
  * กำหนดสิทธิ์และความปลอดภัยระดับไฟล์รันไบนารี (`sudo setcap cap_net_admin,cap_net_raw+ep ./pigate`) เพื่อทดสอบการจัดการ Netlink/nftables ในโหมดไร้สิทธิ์ root (Non-root security verification)
* [ ] **ทำการจำลองโหลดเครือข่ายและสตรีมมิ่ง:** เพื่อวัดระดับความหน่วงของการประมวลผล (Latency) และปริมาณอัตราความเร็วข้อมูล (Throughput) เมื่อมีการใช้ Masquerading และกฎไฟร์วอลล์ระดับร้อยข้อ
