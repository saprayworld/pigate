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
  * พัฒนาหน้า [FirewallPolicy.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/FirewallPolicy.tsx) โดยใช้ UI components ของ shadcn/ui เป็นพื้นฐาน
  * ติดตั้งระบบการลากสลับลำดับความสำคัญของกฎความปลอดภัย (Drag & Drop ด้วย `@dnd-kit/core` และ `@dnd-kit/sortable` ล็อคแกนการลากแนวตั้ง)
  * พัฒนาและเพิ่มประสิทธิภาพโมดอลสำหรับการเพิ่ม (Create) และแก้ไข (Edit) กฎความปลอดภัย:
    * ปรับเพิ่มความกว้างการแสดงผลและปรับปรุงเลย์เอาต์จัดวางคอมโพเนนต์ให้มีความสวยงามและเป็นระเบียบมากยิ่งขึ้น (3 แถวหลัก)
    * เพิ่มฟิลด์เลือก **การ์ดขาเข้า (In Interface)** และ **การ์ดขาออก (Out Interface)** ดึงรายชื่อการ์ดเครือข่ายที่มีในระบบแบบไดนามิกผ่าน `interfaceService` พร้อมทั้งแสดงผลคอลัมน์ In/Out, Badges และแสดงชื่อ **Alias** ของการ์ดเครือข่ายใต้ Badge ในตารางนโยบายอย่างชัดเจนและสวยงาม
    * ปรับปรุงฟิลด์กรอกข้อมูล **ต้นทาง (Source)**, **ปลายทาง (Destination)** และ **บริการ/พอร์ต (Service/Port)** ให้ใช้งาน Multiple Selection Combobox แบบ Chips ดึงข้อมูลตัวเลือกจาก Addresses และ Services Mock Database จริงที่ผู้ใช้บันทึก
    * ปรับปรุงวิธีการใช้งาน Combobox API ให้ถูกต้องตามคู่มืออ้างอิงของ shadcn (การใช้งาน `items` prop, `<ComboboxValue>` ด้วย render prop และ `<ComboboxList>` ด้วย children function)
    * แก้ไขและบันทึกแนวทางปัญหาคลิกเลือกตัวเลือก Combobox dropdown ภายใน Dialog โดนบล็อกอันเกิดจากกลไก Focus/Pointer Blocker ของ Radix UI Dialog โดยการกำหนด `modal={false}` บน Dialog
  * ออกแบบระบบ Switch inline เพื่อเปิด/ปิด หรือสตรีมล็อกบนแถว และการจำลองนำไปปรับใช้จริงผ่านปุ่ม "Apply Settings" เข้าเคอร์เนล `nftables`

* **พัฒนาหน้าจอการจัดการวัตถุเครือข่ายและพอร์ต (Addresses & Services Objects) [สำเร็จ]**:
  * พัฒนาหน้า [Addresses.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/Addresses.tsx) สำหรับสร้างและควบคุม IP/Subnet, IP Range, หรือชื่อโดเมน FQDN
  * พัฒนาหน้า [Services.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/Services.tsx) สำหรับควบคุมรายชื่อพอร์ต TCP/UDP/ICMP
  * ทั้งสองหน้ารองรับการทำ CRUD ในตัว (เพิ่ม, แก้ไข, ลบ) และมีระบบความปลอดภัยล็อกไม่ให้ลบหรือแก้ไขวัตถุของระบบ (Predefined System Objects เช่น `ALL` หรือ `HTTP`) พร้อมทั้งแสดงไอคอน Lock 🔒 ในแถวนั้น ๆ
  * หน้า Addresses รองรับการเลือกกล่องเครื่องหมายเพื่อลบทีละหลายวัตถุพร้อมกัน (Bulk Delete) และหน้า Services มีกล่อง Preview คำสั่ง Named Set ที่จำลองการส่งไปประมวลผลบน Linux Kernel `nftables` จริงแบบเรียลไทม์
  * พัฒนาระบบเชื่อมโยงความสัมพันธ์และส่งต่อข้อมูลจำลอง (Mock Data Synchronization - [mockSync.ts](file:///home/sapray/dev/pigate/frontend/src/services/mockSync.ts)):
    * คำนวณหาค่า `refPolicies` สำหรับแสดงรายการกฎไฟร์วอลล์ที่อ้างอิงถึงวัตถุที่อยู่หรือวัตถุบริการนั้น ๆ สดแบบเรียลไทม์
    * บล็อกความสามารถในการลบ Address หรือ Service ใด ๆ ตราบใดที่ยังถูกกฎ Firewall อ้างอิงการใช้งานอยู่
    * รองรับการส่งต่อการแก้ไขชื่อ (Rename Propagation): เมื่อเปลี่ยนชื่อวัตถุ เช่น `LAN_Network` -> `LAN_Internal` ระบบจะตามไปค้นหาและเปลี่ยนชื่อในกฎ Firewall Policy ทุกกฎที่ใช้วัตถุนั้นให้อัตโนมัติใน Mock LocalStorage Database

* **พัฒนาหน้าจอการตั้งค่าการ์ดเครือข่ายและระบบสุ่ม MAC Address (Network Interfaces) [สำเร็จ]**:
  * พัฒนาหน้า [Interfaces.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/Interfaces.tsx) ครอบคลุมการแสดงผล eth0 (Ethernet) และ wlan0 (Wireless)
  * เพิ่มการสลับประเภท/หน้าที่พอร์ต (**Port Role**) ได้แก่ **LAN** และ **WAN** ในหน้าแก้ไข และแสดงผลเป็นสัญลักษณ์สีแยกแยะชัดเจนในตารางการ์ดเครือข่าย
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
  * พัฒนาหน้า [SettingsMaintenance.tsx](file:///home/sapray/dev/pigate/frontend/src/pages/SettingsMaintenance.tsx) สไตล์ Premium Design (รองรับ Dark/Light Mode) แบ่งออกเป็นแท็บ Setup Settings และ Maintenance
  * แท็บ Setup Settings: รองรับฟอร์มเปลี่ยนรหัสผ่านแอดมิน, ตั้งค่า Time Zone, และการตั้งค่าซิงค์เวลาอัตโนมัติ (NTP Server)
  * แท็บ Maintenance: จัดการปุ่มรีบูต (Reboot) พร้อม Overlay นับถอยหลังจำลอง, ปุ่มปิดระบบ (Shutdown) พร้อม Overlay หน้าจอระบบปิดที่กด Power On กลับมาได้, ฟังก์ชันดาวน์โหลดสำรองข้อมูลเป็นไฟล์ JSON และนำเข้าฟื้นคืนข้อมูล รวมถึงตารางสั่ง Restart บริการย่อย (`nftables`, `isc-dhcp-server`, `NetworkManager`)

* **พัฒนาโครงสร้าง Service API Layer และเชื่อมต่อครบทุกหน้าจอ (Service Layer & Pages Integration) [สำเร็จ]**:
  * ออกแบบระบบสวิตช์ควบคุม `IS_MOCK_MODE` ป้องกันสเตตหายเมื่อรีเฟรชหน้าเว็บด้วยระบบ LocalStorage Mocking ผ่าน [config.ts](file:///home/sapray/dev/pigate/frontend/src/services/config.ts)
  * พัฒนารายการ Service ครบทั้ง 9 บริการหลัก ได้แก่ `addressService`, `serviceObjectService`, `policyService`, `staticRouteService`, `dhcpService`, `interfaceService`, `systemService`, `dashboardService`, และ `authService`
  * ปรับแก้หน้าจอ UI ทุกหน้า (`Addresses`, `Services`, `FirewallPolicy`, `StaticRoutes`, `DhcpServer`, `Interfaces`, `SettingsMaintenance`, `Dashboard`, `Login`) ให้ดึงข้อมูลและทำรายการแบบ Asynchronous ผ่าน Service API Layer ทั้งหมด
  * ตรวจสอบโค้ดบิวด์ระดับ Production ด้วย `yarn build` ผ่าน 100% ไม่มีข้อผิดพลาดของ TypeScript หรือ Syntax Warnings

* **พัฒนา Go API Backend & ระบบทดสอบอัตโนมัติ (Go Backend & Automated Testing) [สำเร็จ]**:
  * พัฒนาโครงสร้างตัวควบคุมหลังบ้านหลัก (Go v1.26.4) สื่อสารกับฐานข้อมูล SQLite (`modernc.org/sqlite` แบบไม่มี CGO) และจำลองคำสั่งจัดการไฟร์วอลล์/การเชื่อมต่อระดับ Kernel
  * ติดตั้งระบบรักษาความปลอดภัยระดับตัวรับส่ง API ได้แก่ CORS (อนุญาตหน้าจอพอร์ต 5173), Middleware ตรวจสอบโทเค็น (Bearer token auth) และ Rate Limiting ป้องกันการสุ่มล็อกอิน
  * เชื่อมต่อหน้าจอ React Frontend เข้ากับ Go API จริงที่พอร์ต `8081` สำเร็จลุล่วง ข้อมูลสามารถรับส่งได้จริงและจัดเก็บลงฐานข้อมูล `pigate.db`
  * พัฒนาและติดตั้งชุดทดสอบอัตโนมัติ (Automated Testing) ครบถ้วนทั้ง Unit tests (ทดสอบคิวรีฐานข้อมูลจำลอง) และ Integration tests (จำลองยิง HTTP ตรวจสอบ JSON payload และ Auth validation) ซึ่งผ่านการรันคอมไพล์และทดสอบสำเร็จ 100%
  * แก้ไขประเด็นสำคัญระหว่างการเชื่อมต่อระบบจริง (Integration Fixes):
    * **ระบบสิทธิ์โทเค็น (Bearer Token Injection):** ติดตั้งระบบ Hook สกัดกั้นการดึงข้อมูล `window.fetch` ของเบราว์เซอร์ เพื่อส่ง Bearer Token ที่ดึงจาก LocalSession ไปยัง API Endpoint ขาเข้าอัตโนมัติ ป้องกันปัญหา 401 Unauthorized ในการดึงข้อมูลของระบบ
    * **การจัดส่งค่าอาร์เรย์ว่าง (Empty Array Serialization):** ปรับจูนฝั่ง API หลังบ้านไม่ให้คืนค่า Slice ว่างเป็น `null` บน JSON แต่ให้คืนค่าเป็น `[]` เพื่อไม่ให้ตัวประมวลผล JSON ในฝั่ง React เกิดการ Error
    * **ฟีเจอร์จำลองการทำงานจากระบบจริง (Mock from Real Data Mode) [สำเร็จ]:** พัฒนาแฟล็ก `-mock-from-real` สำหรับการดึงข้อมูลการตั้งค่าจริงบนเครื่องคอมพิวเตอร์แม่ข่าย Linux เข้าสู่ฐานข้อมูล SQLite ตอนเริ่มระบบครั้งแรก (ซิงค์ Network Interfaces, Static Routes และ DNS จาก `/etc/resolv.conf`) โดยไม่บันทึกแก้ไขกลับลงตัวเครื่องจริงพร้อมระบบฉีดการ์ดจำลอง `wlan0` อัตโนมัติในฝั่งหน้าบ้าน
    * **อัปเดตสเปก API เอกสารสากล (OpenAPI Specification) [สำเร็จ]:** อัปเดตสเปก API ทั้งหมดเพิ่มเส้นทาง `/system/dns` และ Schemas `DNSConfig`, `DNSConfigInput`, `DynamicDNSServer` ในคู่มือตัวระบบ `docs/openapi.yaml` และหน้าบ้าน `frontend/public/openapi.yaml`
    * **ฟีเจอร์จำกัดสิทธิ์แก้ไขข้อมูลจำลอง (Disable Edit Mode) [สำเร็จ]:** เพิ่มแฟล็ก `-disable-edit` เพื่อเปิดใช้งานโหมดอ่านอย่างเดียว (Read-Only) ในฝั่งหลังบ้าน ป้องกันการทำ CRUD เพื่อความปลอดภัยในบางสภาวะแวดล้อม
    * **ระบบสแกนคลื่นไร้สายที่รัดกุม (Wireless Scan Validation) [สำเร็จ]:** เพิ่มการกรองและยืนยันประเภทของการ์ดเชื่อมต่อเครือข่ายก่อนทำการค้นหาสัญญาณ Wi-Fi (Wi-Fi Scan) เพื่อบังคับให้ทำรายการเฉพาะการ์ดที่ระบุประเภทเป็น `wireless` เท่านั้น
    * **ระบบจัดการ DNS เชิงลึกแบบรวมศูนย์ (Centralized DNS Management) [สำเร็จ]:** เพิ่มการรองรับ API การตั้งค่าเซิร์ฟเวอร์ DNS ทั้งแบบคงที่และแบบรับไดนามิก พร้อมทั้งการเชื่อมโยงระบบ Local Domain Name
    * **การฝังหน้าจอ React Frontend เข้ากับ Go Backend (Frontend Embedding) [สำเร็จ]:** พัฒนาการฝังไฟล์หน้าบ้าน (`dist/`) เข้าไปใน Go backend binary ผ่าน `go:embed` ใน [internal/api/embed.go](file:///home/sapray/dev/pigate/backend/internal/api/embed.go) ส่งผลให้ตัวแอปพลิเคชันทำงานเป็น Single Binary ที่สามารถเสิร์ฟหน้าจอผู้ใช้งานได้ด้วยตัวเองและยังคงรองรับ Routing แบบ Client-side (SPA fallback)
    * **ระบบจัดการและลบอินเตอร์เฟสจำลอง (Interface CRUD & DB Order Fix) [สำเร็จ]:** เพิ่มระบบ API สำหรับ Delete/Reset การตั้งค่าการ์ดเครือข่ายจำลอง เพื่อความยืดหยุ่นในการทดสอบ และปรับแก้อันดับอาร์กิวเมนต์คิวรีในการซิงค์ข้อมูลลงฐานข้อมูลให้ถูกต้อง
    * **การตั้งค่ารายละเอียดอินเตอร์เฟสเครือข่ายจริงผ่าน Netlink (Netlink IP Configuration) [สำเร็จ]:** พัฒนา `ConfigureInterface` ใน [internal/kernel/real_network.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_network.go) ให้ทำการล้างค่า IP/DHCP เก่า (ยกเลิก dhclient/dhcpcd) และลงทะเบียนการตั้งค่า static IP, netmask, default gateway บนการ์ดเครือข่ายลินุกซ์ด้วย Netlink
    * **การทำความสะอาดโครงสร้างเครือข่ายล้าสมัย (Network Struct Cleanup) [สำเร็จ]:** ทำการถอนฟิลด์ `dns1` และ `dns2` ที่ไม่ใช้งานออกจากการตั้งค่าการ์ดเครือข่าย เพื่อไปใช้ระบบจัดส่ง DNS แบบรวมศูนย์อย่างสมบูรณ์
    * **การสร้างเอกสารอ้างอิงสำหรับผู้พัฒนา (Developer Portal generation) [สำเร็จ]:** สร้างเอกสารอ้างอิงสำหรับผู้พัฒนาทั้งรูปแบบ HTML และ Markdown ในโฟลเดอร์ `docs/` เพื่อสรุปแนวทางความปลอดภัย กฎไฟร์วอลล์ และตาราง DHCP/Routing
    * **ระบบยืนยันสิทธิ์เซสชันและการบังคับเปลี่ยนรหัสผ่านครั้งแรก (Active Session Verification & Force Password Change Enforcement) [สำเร็จ]:**
      * ฝั่งหลังบ้าน: เพิ่ม API Endpoint `/api/auth/session` สำหรับยืนยันความมีอยู่และหมดอายุของเซสชันที่เชื่อมต่อ และใน `AuthMiddleware` มีการตรวจสอบสิทธิ์รหัสผ่านแรกเริ่ม (`IsInitial` ใน DB) เพื่อจำกัดสิทธิ์ (ส่งกลับ StatusForbidden 403 พร้อม payload `mustChangePassword`) ทุก Endpoint ยกเว้นเปลี่ยนรหัสผ่าน ออกจากระบบ และตรวจสอบเซสชัน โดยผู้ใช้หลักเปลี่ยนชื่ออ้างอิงจาก "admin" เป็น "pigate"
      * ฝั่งหน้าบ้าน: พัฒนาการตรวจสอบเซสชันบน Backend ทุกครั้งที่เริ่มดาวน์โหลด/เมานต์หน้าเว็บเพื่อป้องกัน Session Bypass พร้อมระบบ Route Guard และจัดทำหน้าจอเตือนเปลี่ยนรหัสผ่านบังคับ `/change-password` (ForceChangePassword.tsx)
    * **การคัดแยกการ Seeding ข้อมูล Mock/Real (Database Seeding Isolation & Frontend Proxy Config) [สำเร็จ]:**
      * เพิ่มแฟล็ก `mockOS` จากหน้าหลักส่งเข้าฟังก์ชัน `InitDB` เพื่อป้องกันการ Seed ข้อมูลพอร์ตแลน/Wi-Fi จำลอง (eth0, wlan0) ลงฐานข้อมูล SQLite ในโหมด Production (`-mock=false`) ทำให้ระบบรวบรวมข้อมูลจากการ์ดจริงบนบอร์ดเท่านั้น
      * กำหนดสิทธิ์ตั้งค่าการทำ Proxy พาร์ท `/api` ไปยัง `http://localhost:2479` ใน `vite.config.ts` ของหน้าบ้าน ทำให้การพัฒนาเชื่อมต่อ API บน React dev server ทำได้สะดวก
    * **การป้องกันความปลอดภัยฐานข้อมูลและย้ายตำแหน่งไบนารีระบบ (Gitignore database bypass & Binary root placement) [สำเร็จ]:**
      * เพิ่มการละเว้นไฟล์ข้อมูล SQLite (`*.db`, `*.db-shm`, `*.db-wal`) และไบนารีรัน `pigate` เข้าไปใน `.gitignore` เพื่อความมั่นคงปลอดภัย
      * ปรับแต่งสคริปต์คอมไพล์ระบบ `build.sh` ให้คัดลอกไฟล์รันไบนารีมาที่ตำแหน่งรูทโฟลเดอร์ในชื่อ `pigate` เพื่อรันงานได้ง่ายขึ้น


* **แก้ไขข้อเสนอแนะความสำคัญสูง (Priority High Recommendations) จากผลการรีวิวหน้าบ้าน [สำเร็จ]**:
  * **แทนที่ Native Dialogs:** พัฒนาและติดตั้ง [AlertDialogProvider.tsx](file:///home/sapray/dev/pigate/frontend/src/components/AlertDialogProvider.tsx) เพื่อใช้ Custom AlertDialog ของ shadcn/ui ครอบคลุมการเตือนและการยืนยันคำสั่งทั้งหมด แทนการเรียกใช้ `alert()` และ `confirm()` ดั้งเดิมของเบราว์เซอร์
  * **ระบบตรวจสอบค่า IP Address (Strict Validation):** อัปเดตและติดตั้ง Regex/Logic ตรวจสอบความถูกต้องของ IPv4, CIDR, และ IP Range โดยเช็กค่า Octet ละเอียด 0-255 ใน [utils.ts](file:///home/sapray/dev/pigate/frontend/src/lib/utils.ts) และนำไปใช้ตรวจสอบความมั่นคงปลอดภัยของอินพุตในทุกหน้ารวมถึง Static Routes, DHCP Server, Addresses และ Interfaces
  * **ตารางแบบ Responsive:** ครอบตารางแสดงข้อมูลกฎความปลอดภัย (Firewall Policies) และตารางพอร์ตเชื่อมต่อ (Interfaces) ด้วย `<div className="overflow-x-auto w-full">` ป้องกันเนื้อหาล้น (overflow) เมื่อแสดงผลบนหน้าจอขนาดเล็ก/สมาร์ทโฟน

---

## 2. ปัญหาและประเด็นที่ต้องพิจารณาในปัจจุบัน (Current Issues & Limitations)

> [!CAUTION]
> ### 🔴 ประเด็นความมั่นคงปลอดภัยระดับวิกฤต (Security Vulnerabilities - MUST FIX)
> จากการตรวจสอบซอร์สโค้ดและสถาปัตยกรรมระบบหลังบ้าน พบจุดอ่อนร้ายแรง 2 ประเด็นที่จะต้องได้รับการแก้ไขก่อนเผยแพร่สู่การใช้งานจริง:
> 
> 1. **Auth Bypass Backdoor (CWE-259 & CWE-287 - Critical)**: โค้ดตรวจสอบสิทธิ์การล็อกอินในฟังก์ชัน `HandleLogin` ([handlers.go](file:///home/sapray/dev/pigate/backend/internal/api/handlers.go#L92-L98)) ยินยอมให้ข้ามการตรวจสอบ Bcrypt ได้หากระบุบัญชี `pigate:pigate` (ใช้เป็นรหัสผ่านเริ่มต้นสำหรับการจำลอง) ซึ่งสามารถถูกใช้เจาะระบบได้แม้จะเปลี่ยนรหัสผ่านหลักใน SQLite ไปแล้ว
> 2. **CORS Mismatch with Credentials (CWE-346 - Medium)**: ใน [middleware.go](file:///home/sapray/dev/pigate/backend/internal/api/middleware.go#L50-L60) มีการตั้งค่า `Access-Control-Allow-Origin: "*"` ควบคู่กับการใช้ Credentials ซึ่งผิดข้อกำหนดความปลอดภัยของเบราว์เซอร์ยุคใหม่ และอาจทำให้การสื่อสารข้าม Origin ถูกบล็อก

> [!NOTE]
> **สถานะปัจจุบันพร้อมทดสอบจำลองแล้ว (Mock OS Interface Verified):**
> ระบบฐานข้อมูล SQLite, ส่วนควบคุม REST API และสิทธิ์โทเค็นได้รับการทดสอบร่วมกันกับหน้าจอ UI จริงบนเบราว์เซอร์เรียบร้อยแล้ว ปัจจุบันยังไม่พบปัญหาขัดข้องในฝั่งการทำงานจำลอง (Mock OS Mode) ส่วนแผนงานระยะถัดไปคือการเริ่มเตรียมระบบการรันงานระดับ Kernel จริงบน Linux Host (บอร์ด Raspberry Pi 5) เมื่ออุปกรณ์และสิทธิ์ Cap_Net_Admin พร้อมใช้งาน

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
  * ทยอยพัฒนาส่วนหน้าจออื่น ๆ ได้แก่ หน้าล็อกอินจริง `[เสร็จสิ้น]`
* **สเตปที่ 5: จัดระเบียบการเรียกใช้ API และความปลอดภัย**:
  * พัฒนาโครงสร้าง Service API Layer รองรับ LocalStorage Mocking และ Go API Swappable `[เสร็จสิ้น]`
  * แก้ไขข้อเสนอแนะความสำคัญสูง (Priority High) จากผลการรีวิวหน้าบ้าน (ระบบ Custom Alert/Confirm, Strict IP Validation 0-255, Responsive Tables) `[เสร็จสิ้น]`
  * ทดสอบการทำงานของปุ่ม ฟังก์ชัน CRUD และ UI ต่างๆ บนเบราว์เซอร์จริง (Runtime Manual Verification & UI Validation) `[เสร็จสิ้น]`
  * ตรวจสอบความปลอดภัยระดับเบื้องต้น เช่น การรับมือเมื่อเซสชันหมดอายุ, การกรองฟิลด์ข้อมูลนำเข้า (Sanitization) และการเข้ารหัสการสื่อสาร `[เสร็จสิ้น]`
  * เชื่อมต่อ API จริงกับฝั่ง Go Backend และระบบการอัปเดตแบบ Real-time ด้วย Server-Sent Events (SSE) เมื่อฝั่ง API พร้อมใช้งาน `[เสร็จสิ้น]`
  * เพิ่มเติมฟีเจอร์รันระบบหลังบ้านโดยดึงข้อมูลจริงจาก Kernel แต่อัปเดตลงเฉพาะฐานข้อมูล (-mock-from-real) พร้อมระบบจำกัดสิทธิ์อ่านอย่างเดียว (-disable-edit), ระบบ DNS และการตรวจสอบ Wifi Scan พร้อมปรับปรุง API Specs `[เสร็จสิ้น]`
  * พัฒนาระบบตรวจสอบความถูกต้องของเซสชัน (Active Session Verification) และระบบบังคับเปลี่ยนรหัสผ่านแรกเริ่มเพื่อความปลอดภัย (Forced Password Change Enforcement) ทั้งฝั่งหลังบ้านและหน้าบ้าน `[เสร็จสิ้น]`
  * คัดแยกการ Seed ข้อมูลเครือข่ายจำลองตามโหมดใช้งาน และตั้งค่า Proxy การพัฒนาฝั่งหน้าบ้าน `[เสร็จสิ้น]`
  * ปรับแต่งสคริปต์คอมไพล์ build.sh และการละเว้นไฟล์ข้อมูลสำคัญลง Gitignore `[เสร็จสิ้น]`

* **พัฒนา Kernel Integration ระยะที่ 1 — Real NetworkManager ผ่าน Netlink Socket [สำเร็จ]**:
  * สร้างไฟล์ [internal/kernel/real_network.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_network.go) implement `RealNetwork struct` ตาม `NetworkManager` interface สำหรับรันบน Linux production จริง
  * `ToggleInterface` ใช้ `github.com/vishvananda/netlink` — `netlink.LinkSetUp/Down()` สื่อสารกับ kernel ผ่าน Netlink Socket โดยตรง ไม่ผ่าน shell command (ป้องกัน Command Injection ตามข้อ 4.1 ใน tech_stack_design.md)
  * `ScanWifi` ใช้ `iw dev scan` (primary) และ `nmcli` (fallback) โดยไม่ต้องการ root
  * อัปเดต [cmd/pigate/main.go](file:///home/sapray/dev/pigate/backend/cmd/pigate/main.go) ให้ production path (`--mock=false`) ใช้ `kernel.NewRealNetwork()` แทน MockNetwork
  * เพิ่มเติมระบบตรวจจับ Netlink Subtypes (เช่น loopback, bridge, vlan ฯลฯ) เพื่อแสดงผลใน UI หน้าบ้าน
  * เพิ่มปุ่ม Refresh ในหน้า Interfaces และระบบคงการตั้งค่า (Config retention) ของการ์ดเครือข่ายไว้แม้ว่าจะ offline/sync
  * ทดสอบ compile ผ่าน 100% ด้วย `go build ./...`
  * ทดสอบรันจริงด้วย `./pigate-backend -port=8081 -mock=false` ผ่านสำเร็จ (ต้องการ `cap_net_admin` บน RPi5)

* **สเตปที่ 6: Kernel Integration ระยะที่ 2 — Real Firewall & Routing (TODO)**:
  * **[สำเร็จ]** พัฒนา `RealRouting` ใน [internal/kernel/real_routing.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_routing.go) โดยใช้ `netlink.RouteAdd/Del/Replace` ในการเชื่อมกับ Linux Routing Table โดยตรง ไม่ผ่าน shell command พร้อมรองรับรายละเอียด Scope, Metric, Protocol, Src IP และระบบจัดเรียงความสำคัญของ Kernel Route
  * **[TODO]** สร้าง `RealFirewall` ใช้ `github.com/google/nftables` แทน MockFirewall
  * **[TODO]** ทดสอบบน Raspberry Pi 5 จริงพร้อม `sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend`

* **สเตปที่ 7: การแก้ไขและลดระดับช่องโหว่ทางความปลอดภัย (Security Hardening - MUST FIX)**:
  * **[TODO]** ลบหรือปิดกั้นช่องโหว่การข้าม Bcrypt (HandleLogin Backdoor) ในไฟล์ `handlers.go`
  * **[TODO]** ปรับปรุงโครงสร้างของ CORS Origin ใน `middleware.go` ไม่ให้ตั้งค่า Wildcard `*` ปะปนกับ Credentials `true` เพื่อรักษาระเบียบความถูกต้องตามมาตรฐาน Web API


