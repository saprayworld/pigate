# PiGate Project Status & Roadmap

เอกสารฉบับนี้เป็นรายงานสรุปสถานะล่าสุดของโปรเจกต์ **PiGate** (ระบบควบคุม Raspberry Pi Firewall/Gateway) ทั้งความคืบหน้า ปัญหาที่พบ และแผนการทำงานในระยะถัดไป สำหรับผู้พัฒนาและผู้ดูแลระบบ

---

## 1. สิ่งที่ได้ดำเนินการเสร็จสิ้นแล้ว (Completed Work)

* **วิเคราะห์สถาปัตยกรรมและการเชื่อมโยงข้อมูล (`docs/`)**:
  * วิเคราะห์เอกสารระบบ [`tech_stack_design.md`](file:///home/sapray/dev/pigate/docs/tech_stack_design.md) เพื่อทำความเข้าใจรูปแบบสถาปัตยกรรม (Go Backend ทำหน้าที่เป็น API & Single Binary Server ทำงานร่วมกับ React SPA Frontend ผ่าน `go:embed`)
  * ตรวจสอบไฟล์แบบร่างโครงลวดหน้าเว็บ (Wireframe Sketches) ทั้งหมด 10 หน้าในโฟลเดอร์ [`docs/sketchs/frontend/`](file:///home/sapray/dev/pigate/docs/sketchs/frontend/)
  * วิเคราะห์ API Endpoints ใน [`docs/sketchs/api-endpoint/01-api-endpoints.html`](file:///home/sapray/dev/pigate/docs/sketchs/api-endpoint/01-api-endpoints.html) เพื่อกำหนดโครงสร้างโมเดลข้อมูลและการเชื่อมโยงผ่าน HTTP REST API และระบบ Real-time (Server-Sent Events)

* **ตรวจสอบความถูกต้องของระบบหน้าบ้าน (`frontend/`)**:
  * ตรวจสอบการเซ็ตอัปเทคโนโลยีหลัก (React 19, Vite, TypeScript, Tailwind CSS v4, และ `components.json` สำหรับ shadcn/ui)
  * ตั้งค่าและแมปโฟลเดอร์ด้วย Alias `@/` สำเร็จเรียบร้อย สามารถอ้างอิงนำเข้า components/lib ได้โดยไม่มีปัญหาทางเทคนิค

* **พัฒนาโครงหน้าจอหลักและระบบนำทาง (Shell Layout & Routing) [สำเร็จ]**:
  * ติดตั้งไลบรารีที่จำเป็น ได้แก่ `react-router-dom` สำหรับระบบนำทาง, `recharts` สำหรับวาดกราฟ และ `@dnd-kit` สำหรับระบบ Drag & Drop เรียบร้อย
  * พัฒนา [ShellLayout.tsx](file:///home/sapray/dev/pigate/frontend/src/components/layout/ShellLayout.tsx) เป็นโครงเลย์เอาต์หลักสไตล์ Dark Mode ระดับพรีเมียม ประกอบด้วย:
    * **Sidebar:** แสดงแบรนด์ 🛡️ PiGate และลิงก์เมนูแบ่งตามกลุ่ม Network, Policy, System
    * **Topbar:** แสดงสถานะทรัพยากรตัวบอร์ดสด (CPU: 15%, RAM: 42%, Temp: 48°C, Power: OK) ในรูปแบบ Badges และเมนูผู้ใช้งาน (Admin Dropdown)
  * ตั้งค่าระบบนำทางและ Route Guard ป้องกันสิทธิ์การเข้าใช้งาน ([App.tsx](file:///home/sapray/dev/pigate/frontend/src/App.tsx)) หากยังไม่ยืนยันตัวตนจะถูกส่งกลับหน้าล็อกอิน
  * พัฒนาหน้าจอจำลอง (Mock Pages) ครบถ้วนทั้ง 9 หน้า (Dashboard, Interfaces, Static Routes, DHCP, Firewall Policy, Addresses, Services, Settings, Login) รองรับการเปลี่ยนหน้าแบบ Single Page Application (SPA) สมบูรณ์
  * ทดสอบเรียกคำสั่งคอมไพล์ระบบจริงด้วย `yarn build` บิวด์ผ่านสำเร็จ 100% ปราศจากข้อผิดพลาด

* **พัฒนาและติดตั้งระบบการจัดการธีม (Theme Management - Dark/Light Mode) [สำเร็จ]**:
  * ออกแบบและสร้าง `ThemeProvider` เพื่อจัดการสถานะการสลับธีม และบันทึกข้อมูลลงใน `localStorage` (โดยเริ่มต้นที่ Dark Mode เพื่อรักษาความปลอดภัยทางสายตา)
  * ปรับโครงสร้างสีของเลย์เอาต์หลัก คาร์ดข้อมูล และคอนเทนเนอร์ในหน้าจอหลักทั้งหมด (Dashboard, Login, Interfaces ฯลฯ) ให้เปลี่ยนสีสอดคล้องตามธีมโดยอัตโนมัติ
  * ติดตั้งเมนูสลับ Dark Mode / Light Mode พร้อมไอคอน Sun/Moon และตัวเลือก Appearance เข้ากับ Dropdown เมนูผู้ใช้งานตรงหน้า Topbar สำเร็จ

* **พัฒนาหน้า Dashboard และการแสดงผลทรัพยากรระบบ [สำเร็จ]**:
  * พัฒนาหน้า [Dashboard.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/Dashboard.tsx) สไตล์ Dark Mode ระดับพรีเมียม
  * ติดตั้งกราฟเครือข่ายเรียลไทม์ (WAN Bandwidth) แบบขยับแอนิเมชันโดยใช้ `recharts` ด้วยโทนสี Cyan & Indigo
  * ออกแบบการจำลองดึงค่า CPU / RAM / Temp และเวลาการทำงานระบบ (Uptime) แบบเรียลไทม์ และตารางสรุปประวัติล่าสุด (Firewall Logs) ที่ค้นหาและกรองได้ พร้อมตัวจำลองสถานะ SSE

* **พัฒนาหน้าจอและระบบจัดการกฎไฟร์วอลล์ (Firewall Policies) [สำเร็จ]**:
  * พัฒนาหน้า [FirewallPolicy.tsx](file:///home/sapray/Dev/pigate/frontend/src/pages/FirewallPolicy.tsx) โดยใช้ UI components ของ shadcn/ui เป็นพื้นฐาน
  * ติดตั้งระบบการลากสลับลำดับความสำคัญของกฎความปลอดภัย (Drag & Drop ด้วย `@dnd-kit/core` และ `@dnd-kit/sortable` ล็อคแกนการลากแนวตั้ง)
  * พัฒนาและเพิ่มประสิทธิภาพโมดอลสำหรับการเพิ่ม (Create) และแก้ไข (Edit) กฎความปลอดภัย:
    * ปรับเพิ่มความกว้างการแสดงผลและปรับปรุงเลย์เอาต์จัดวางคอมโพเนนต์ให้มีความสวยงามและเป็นระเบียบมากยิ่งขึ้น (3 แถวหลัก)
    * ปรับปรุงฟิลด์กรอกข้อมูล **ต้นทาง (Source)**, **ปลายทาง (Destination)** และ **บริการ/พอร์ต (Service/Port)** ให้ใช้งาน Multiple Selection Combobox แบบ Chips ของ Base UI / shadcn/ui
    * ปรับปรุงวิธีการใช้งาน Combobox API ให้ถูกต้องตามคู่มืออ้างอิงของ shadcn (การใช้งาน `items` prop, `<ComboboxValue>` ด้วย render prop และ `<ComboboxList>` ด้วย children function)
    * แก้ไขและบันทึกแนวทางปัญหาคลิกเลือกตัวเลือก Combobox dropdown ภายใน Dialog โดนบล็อกอันเกิดจากกลไก Focus/Pointer Blocker ของ Radix UI Dialog โดยการพอร์ตเนื้อหาเข้าภายใต้ DOM Tree ของ DialogContent ด้วย container ref
  * ออกแบบระบบ Switch inline เพื่อเปิด/ปิด หรือสตรีมล็อกบนแถว และการจำลองนำไปปรับใช้จริงผ่านปุ่ม "Apply Settings" เข้าเคอร์เนล `nftables`

* **พัฒนาหน้าจอการจัดการวัตถุเครือข่ายและพอร์ต (Addresses & Services Objects) [สำเร็จ]**:
  * พัฒนาหน้า [Addresses.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/Addresses.tsx) สำหรับสร้างและควบคุม IP/Subnet, IP Range, หรือชื่อโดเมน FQDN
  * พัฒนาหน้า [Services.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/Services.tsx) สำหรับควบคุมรายชื่อพอร์ต TCP/UDP/ICMP
  * ทั้งสองหน้ารองรับการทำ CRUD ในตัว (เพิ่ม, แก้ไข, ลบ) และมีระบบความปลอดภัยล็อกไม่ให้ลบหรือแก้ไขวัตถุของระบบ (Predefined System Objects)
  * หน้า Addresses รองรับการเลือกกล่องเครื่องหมายเพื่อลบทีละหลายวัตถุพร้อมกัน (Bulk Delete) และหน้า Services มีกล่อง Preview คำสั่ง Named Set ที่จำลองการส่งไปประมวลผลบน Linux Kernel `nftables` จริงแบบเรียลไทม์

* **พัฒนาหน้าจอการตั้งค่าการ์ดเครือข่ายและระบบสุ่ม MAC Address (Network Interfaces) [สำเร็จ]**:
  * พัฒนาหน้า [Interfaces.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/Interfaces.tsx) ครอบคลุมการแสดงผล eth0 (Ethernet) และ wlan0 (Wireless)
  * ติดตั้งเครื่องมือสแกนหาคลื่น Wi-Fi (SSID Scanner) ที่มีระบบพรีวิวสัญญาณตามระดับความแรงช่องสัญญาณและความปลอดภัยของเครือข่าย
  * เพิ่มฟีเจอร์ความปลอดภัยขั้นสูง ได้แก่ **MAC Address Randomization** สำหรับการสุ่ม MAC Address เพื่อความปลอดภัย และ **LAA MAC Address** (กำหนด MAC เองแบบ Locally Administered) สำหรับการ์ด Wi-Fi พร้อมระบบตรวจสอบมาตรฐานความถูกต้อง LAA (หลักที่สองของ Byte แรก ต้องเป็น 2, 6, A, E) และตัวสลับสุ่ม MAC ใหม่เมื่อ Reconnect โดยอัตโนมัติ

* **พัฒนาหน้าจอและระบบจัดการเส้นทางแบบคงที่ (Static Routes) [สำเร็จ]**:
  * พัฒนาหน้า [StaticRoutes.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/StaticRoutes.tsx) สำหรับควบคุมตารางการกำหนดเส้นทางเครือข่ายย่อยต่าง ๆ
  * รองรับระบบ CRUD เต็มรูปแบบในการเพิ่ม แก้ไข และลบเส้นทาง (ยกเว้นเส้นทางของระบบปฏิบัติการ / System Routes ที่จะถูกล็อกเพื่อความปลอดภัย)
  * ออกแบบ statistics cards แสดงรายละเอียดเส้นทางทั้งหมด, เส้นทางที่เปิดใช้งาน, เส้นทางระบบ และเส้นทางที่กำหนดเอง
  * เพิ่มตัวเลือก Filter ในการค้นหาตามประเภทการจัดเส้นทาง (System/Custom), ค้นหาตามสถานะการใช้งาน (Active/Inactive) และกล่องค้นหาตามชื่อ/ข้อมูลเส้นทาง
  * มีระบบตรวจสอบความถูกต้องข้อมูล เช่น ตรวจสอบ CIDR format สำหรับเครือข่ายปลายทาง, ตรวจสอบรูปแบบ IP Gateway และค่า Metric
  * มีปุ่มจำลองการนำการเปลี่ยนแปลงไปปรับใช้จริงผ่าน "Apply Routing Config" สตรีมตรงเข้า Kernel ตารางเส้นทาง

* **พัฒนาหน้าจอและระบบจัดการ DHCP Server [สำเร็จ]**:
  * พัฒนาหน้าจัดการ DHCP Server ครอบคลุมการเปิด/ปิดบริการ ตั้งค่า IP Pool, Gateway, DNS, IP Range และ Lease Time
  * รองรับการทำ IP Reservations (จองไอพีตาม MAC) และมีตารางแสดงรายการ Active Leases ปัจจุบัน

* **พัฒนาหน้าจอการตั้งค่ารวมถึงการดูแลรักษา (Settings & Maintenance) [สำเร็จ]**:
  * พัฒนาหน้า [SettingsMaintenance.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/SettingsMaintenance.tsx) สไตล์ Sleek Dark Mode แบ่งออกเป็นแท็บ Setup Settings และ Maintenance
  * แท็บ Setup Settings: รองรับฟอร์มเปลี่ยนรหัสผ่านแอดมิน, ตั้งค่า Time Zone, และการตั้งค่าซิงค์เวลาอัตโนมัติ (NTP Server)
  * แท็บ Maintenance: จัดการปุ่มรีบูต (Reboot) พร้อม Overlay นับถอยหลังจำลอง, ปุ่มปิดระบบ (Shutdown) พร้อม Overlay หน้าจอระบบปิดที่กด Power On กลับมาได้, ฟังก์ชันดาวน์โหลดสำรองข้อมูลเป็นไฟล์ JSON และนำเข้าฟื้นคืนข้อมูล รวมถึงตารางสั่ง Restart บริการย่อย (`nftables`, `isc-dhcp-server`, `NetworkManager`)

---

## 2. ปัญหาและประเด็นที่ต้องพิจารณาในปัจจุบัน (Current Issues & Limitations)

* **การพัฒนาและเชื่อมโยงข้อมูลจริงของแต่ละหน้าจอ**:
  * หน้าจอ Login ปัจจุบันยังเป็นหน้าจอจำลอง (Mock Pages)
  * ต้องเริ่มเปลี่ยนตัวแปรข้อมูลและการทำงานของแต่ละหน้าให้เป็นฟังก์ชันใช้งานจริง
  * ขาดการเชื่อมต่อ API จริงกับฝั่ง Go Backend และระบบการอัปเดตแบบ Real-time ด้วย Server-Sent Events (SSE)

---

## 3. แผนการดำเนินงานระยะถัดไป (Roadmap & Next Steps)

* **สเตปที่ 1: ติดตั้งไลบรารีเพื่อเริ่มระบบนำทางและข้อมูลจำลอง** `[เสร็จสิ้น]`
* **สเตปที่ 2: พัฒนาเลย์เอาต์หลักของแอดมินพอร์ทัล (Shell Layout)** `[เสร็จสิ้น]`
* **สเตปที่ 3: พัฒนาหน้า Dashboard (`01-dashboard.html`)** `[เสร็จสิ้น]`
* **สเตปที่ 4: พัฒนาหน้าจอการตั้งค่าเครือข่ายและความปลอดภัย**:
  * จัดสร้างหน้า Firewall Policies พร้อมติดตั้งความสามารถในการลากจัดเรียงลำดับความสำคัญ (Drag & Drop ด้วย `@dnd-kit`) และปรับปรุงฟอร์มโมดอลให้ใช้งาน Multiple Selection Combobox แบบถูกต้องตามคู่มืออ้างอิงของ shadcn `[เสร็จสิ้น]`
  * พัฒนาหน้าจอการจัดการ Physical & Virtual Interfaces (eth0, wlan0) และระบบจำลองสำหรับคลิกแสกนหาคลื่น Wi-Fi (SSID Scanner) พร้อมระบบสุ่ม MAC Address (MAC Randomization / LAA) `[เสร็จสิ้น]`
  * พัฒนาหน้าจอจัดการที่อยู่ไอพี (Address Objects) และบริการพอร์ต (Service Objects) พร้อมระบบจำลองพรีวิว `nftables` `[เสร็จสิ้น]`
  * พัฒนาหน้าจอและระบบ Static Route สำเร็จเรียบร้อย `[เสร็จสิ้น]`
  * พัฒนาหน้าจอและระบบ DHCP Server สำเร็จเรียบร้อย `[เสร็จสิ้น]`
  * พัฒนาหน้าจอและระบบ Settings & Maintenance สำเร็จเรียบร้อย `[เสร็จสิ้น]`
  * ทยอยพัฒนาส่วนหน้าจออื่น ๆ ได้แก่ หน้าล็อกอินจริง `[กำลังดำเนินการถัดไป]`
* **สเตปที่ 5: จัดระเบียบการเรียกใช้ API และความปลอดภัย**:
  * เตรียมโมเดลการดึงข้อมูลจาก API ของหลังบ้าน และระบบสตรีม SSE (Server-Sent Events)
  * ตรวจสอบความปลอดภัยระดับเบื้องต้น เช่น การรับมือเมื่อเซสชันหมดอายุ, การกรองฟิลด์ข้อมูลนำเข้า (Sanitization) และการเข้ารหัสการสื่อสาร
