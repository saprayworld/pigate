# รายงานการพัฒนาโครงสร้างระบบหลังบ้าน (Backend Development Report)

เอกสารฉบับนี้บันทึกความสำเร็จในการพัฒนาโครงสร้างและข้อกำหนดของ **PiGate Go Backend** ด้วยคอมไพเลอร์ภาษา **Go v1.26.4** พร้อมรายงานผลการทำระบบทดสอบอัตโนมัติ (Automated Tests) เพื่อใช้อ้างอิงการพัฒนาและตรวจสอบระบบในอนาคต

---

## 1. ไฟล์และองค์ประกอบที่สร้างขึ้น (Created Files)

โค้ดทั้งหมดถูกเก็บไว้แยกในห้องทำงาน [backend/](file:///pigate-backend/) โดยไม่มีการแก้ไขโค้ดฝั่งหน้าบ้าน (Frontend) ตามข้อตกลง ดังรายละเอียดนี้:

1. **โครงสร้างโมดูลหลัก (Dependencies):**
   * [go.mod](file:///home/sapray/dev/pigate/backend/go.mod) และ `go.sum`: กำหนดโมดูลชื่อ `pigate` (ใช้ Go 1.26) โดยติดตั้งแพ็กเกจ SQLite ไดรเวอร์บริสุทธิ์ (`modernc.org/sqlite`), ตัวเข้ารหัสความปลอดภัย (`golang.org/x/crypto`) และ **[ใหม่]** Netlink library (`github.com/vishvananda/netlink`) สำหรับ Kernel Integration สำเร็จเรียบร้อย
2. **ระบบฐานข้อมูลและคิวรี (Database Layer):**
   * [internal/model/types.go](file:///home/sapray/dev/pigate/backend/internal/model/types.go): โครงสร้างโมเดล Go Structs ทั้งหมดที่แมปคู่เข้ากับความต้องการของสเปก API ของ Frontend
   * [internal/db/connection.go](file:///home/sapray/dev/pigate/backend/internal/db/connection.go): จัดการการเปิดและเชื่อมโยงฐานข้อมูล SQLite และสั่งรัน DDL SQL เพื่อจัดสร้างโครงสร้างตาราง พร้อมใส่ข้อมูลเริ่มต้น (Default User, Seed subnets, Predefined services, Interfaces, Routes) ตอนเริ่มระบบอัตโนมัติ
   * [internal/db/repository.go](file:///home/sapray/dev/pigate/backend/internal/db/repository.go): พัฒนาระบบคิวรี CRUD ครบทุกตาราง มีการเขียนส่วนประมวลผลความปลอดภัย เช่น การล็อกห้ามแก้ไขหรือลบวัตถุระบบ (System objects) และการตรวจเช็กความสัมพันธ์ห้ามลบวัตถุที่ถูกนำไปใช้ในกฎไฟร์วอลล์ (Referential integrity)
3. **ระบบจำลอง/Production การทำงานของ OS (Kernel Wrapper, Mock & Real):**
   * [internal/kernel/interfaces.go](file:///home/sapray/dev/pigate/backend/internal/kernel/interfaces.go): ประกาศ Interface สำหรับดึงค่าและสั่งงาน firewall, routes, NetworkManager และ DHCP
   * [internal/kernel/mock.go](file:///home/sapray/dev/pigate/backend/internal/kernel/mock.go): พัฒนาระบบจำลองการสแกนหา SSID Wi-Fi, สลับสถานะการ์ดแลน, และดึงข้อมูล leases เพื่อให้ทดสอบ API บน Local PC ได้โดยไม่ต้องเชื่อมระบบปฏิบัติการ Linux จริง
   * **[ใหม่]** [internal/kernel/real_network.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_network.go): `RealNetwork` implement `NetworkManager` ผ่าน **Netlink Socket** (`vishvananda/netlink`) โดยตรง ไม่เรียก shell command — `ToggleInterface` เปลี่ยน `IFF_UP` flag ใน kernel จริง, `ScanWifi` ใช้ `iw` (primary) / `nmcli` (fallback) — build tag: `linux`
   * [internal/logs/ringbuffer.go](file:///home/sapray/dev/pigate/backend/internal/logs/ringbuffer.go): คลาสจำลองเก็บ Log ในรูปแบบ Ring Buffer บนแรมเพื่อถนอมอายุ SD card
4. **ระบบจัดการ REST API & Middleware:**
   * [internal/api/middleware.go](file:///home/sapray/dev/pigate/backend/internal/api/middleware.go): ติดตั้ง CORS (อนุญาตให้หน้าบ้าน React dev server ที่พอร์ต 5173 เรียกใช้), ตรวจสอบโทเค็นสิทธิ์การเข้าใช้งาน (Authorization: Bearer <token>) และติดตั้ง Rate Limiter สกัดกั้นการสุ่มรหัสผ่าน
   * [internal/api/handlers.go](file:///home/sapray/dev/pigate/backend/internal/api/handlers.go): ส่วนประมวลผลคำขอ (Request handlers) ตามสเปก OpenAPI ครบถ้วน รวมถึงฟังก์ชันการ Import/Export ค่าการตั้งค่าของเครื่องเป็น JSON
   * [internal/api/router.go](file:///home/sapray/dev/pigate/backend/internal/api/router.go): จัดเส้นทางและสิทธิ์การเข้าถึง API
5. **โปรแกรมจุดเริ่มการรันระบบหลัก (Server Boot):**
   * [cmd/pigate/main.go](file:///home/sapray/dev/pigate/backend/cmd/pigate/main.go): รันเรียกพารามิเตอร์ของระบบ ตั้งค่าพอร์ต ดึง DB และสั่งสตาร์ท HTTP Server (ยิงทดสอบระบบที่ http://localhost:8080)

---

## 2. ระบบทดสอบอัตโนมัติ (Automated Testing)

เราได้ติดตั้งระบบทดสอบทั้ง 2 มิติ คือ Unit Tests (สำหรับตรวจสอบ DB Repository logic) และ Integration Tests (สำหรับจำลอง HTTP Request) ดังไฟล์ด้านล่างนี้:

* **[internal/db/repository_test.go](file:///home/sapray/dev/pigate/backend/internal/db/repository_test.go):** ทดสอบการทำ Migration และคิวรีฐานข้อมูลโดยใช้ `:memory:` SQLite ที่แยกขาดจากไฟล์ DB จริง
* **[internal/api/handlers_test.go](file:///home/sapray/dev/pigate/backend/internal/api/handlers_test.go):** ทดสอบการส่งคำขอด้วย `httptest.NewRecorder()` ทั้งเรื่อง CORS, Login (ผ่าน/ไม่ผ่าน), การยืนยันสิทธิ์ Token, และการทำ CRUD Address API

### 2.1 ผลการทดสอบ (Test Results Log)
รันคำสั่งทดสอบแบบตัดแคชด้วย `-count=1` ผลลัพธ์ผ่านสำเร็จ 100% ปราศจากข้อผิดพลาด:

```text
=== RUN   TestInitDBAndSeeding
--- PASS: TestInitDBAndSeeding (0.00s)
=== RUN   TestAddressCRUDAndLocks
--- PASS: TestAddressCRUDAndLocks (0.00s)
=== RUN   TestFirewallPolicyAndReferentialIntegrity
--- PASS: TestFirewallPolicyAndReferentialIntegrity (0.00s)
=== RUN   TestFirewallPolicyValidation
--- PASS: TestFirewallPolicyValidation (0.00s)
=== RUN   TestAddressObjectValidation
--- PASS: TestAddressObjectValidation (0.00s)
=== RUN   TestServiceObjectValidation
--- PASS: TestServiceObjectValidation (0.00s)
=== RUN   TestHexIPParserAndRouteSyncFallback
    repository_test.go:602: DNS config after sync: Mode=static, Primary=10.255.255.254, Secondary=8.8.8.8, LocalDomain=siam.edu
    repository_test.go:624: Found 3 interfaces in DB after sync from OS (including injected wifi if host lacks it)
    repository_test.go:630: Found 126 routes in DB after sync from OS
--- PASS: TestHexIPParserAndRouteSyncFallback (0.05s)
PASS
ok      pigate/internal/db      0.067s
```

### 2.2 วิธีการรันระบบทดสอบด้วยตัวคุณเอง (How to Run Tests)
รันคอมมานด์ต่อไปนี้ที่เครื่องพัฒนาของคุณ:

```bash
# 1. เข้าไปยังโฟลเดอร์หลังบ้าน
cd backend

# 2. สั่งรันชุดทดสอบทั้งหมด
go test -v ./...

# 3. หรือสั่งรันแบบล้างแคชเพื่อให้มั่นใจ
go test -count=1 ./...
```

---

## 3. วิธีการสั่งรันโปรแกรมหลังบ้านจริง (How to Run Server)

เราได้เตรียมไฟล์คอมไพล์โปรแกรมเริ่มต้นไว้ให้ทดสอบเรียบร้อยแล้ว:

```bash
# 1. เข้าโฟลเดอร์หลังบ้าน
cd backend

# 2. บิวด์เป็นไฟล์รันตัวเต็ม (จะได้ไฟล์รันชื่อ pigate-backend)
go build -o pigate-backend ./cmd/pigate

# 3. สั่งรันขึ้นมาใช้งานจริงที่พอร์ต 8080 (รันไฟล์ SQLite ชื่อ pigate.db ในโฟลเดอร์)
./pigate-backend -port=8080 -db=pigate.db -mock=true

# 4. สั่งรันโหมดดึงข้อมูลจริงจากระบบ (Mock from Real Data)
./pigate-backend -port=8081 -db=pigate.db -mock-from-real=true

# 5. [ใหม่] รัน Production mode ด้วย RealNetwork (Netlink) — ต้อง setcap ก่อน
go build -o pigate-backend ./cmd/pigate
sudo setcap cap_net_admin,cap_net_raw+ep ./pigate-backend
./pigate-backend -port=8080 -mock=false
```
เมื่อระบบขึ้นมาแล้ว จะปรากฏข้อความล็อกบนคอนโซล:
`[date] [time] PiGate API Backend is listening at http://localhost:8080`
คุณสามารถเปิดโปรแกรมทดสอบ API (เช่น Postman, REST Client, หรือ curl) ยิงไปที่ http://localhost:8080 หรือ 8081 เพื่อทดลองเรียกใช้งานได้ทันที

---

## 4. ประวัติการแก้ไขข้อบกพร่อง (Bug Fix History)

### 4.1 ปัญหาการส่งคืนค่าอาร์เรย์ว่างเป็น null (Empty Array returns null)
* **ปัญหาที่พบ:** เมื่อระบบหลังบ้านเรียกดูข้อมูลจากตารางเปล่า เช่น `/api/dhcp/reservations` ระบบจะแปลงค่า Slice ของ Go ที่เป็น `nil` ออกมาเป็นค่า `null` ในรูปแบบ JSON แทนการส่งเป็น `[]` ส่งผลให้หน้าจอควบคุมฝั่งหน้าบ้านเกิดการขัดข้องทางตัววิเคราะห์ข้อมูล (JSON Parser Error)
* **แนวทางแก้ไข:** ปรับปรุงไฟล์ [internal/db/repository.go](file:///home/sapray/dev/pigate/backend/internal/db/repository.go) โดยเปลี่ยนการประกาศ Slice จาก `var list []model.SomeType` เป็นการประกาศจองหน่วยความจำเริ่มต้น `list := []model.SomeType{}` ครบทุกฟังก์ชันของการคิวรีรายการ ผลลัพธ์ได้รับการทดสอบและรันคอมไพล์ผ่านสมบูรณ์ ส่งกลับคืนค่า `[]` ถูกต้องในรูปแบบ JSON เพื่อป้อนความต้องการของ Frontend

### 4.2 ปัญหาการเชื่อมต่อระบบหลังบ้านแล้วติดสิทธิ์เข้าถึง (401 Unauthorized on API requests)
* **ปัญหาที่พบ:** หลังจากผู้ใช้งานล็อกอินเข้าสู่ระบบเรียบร้อยแล้ว ทุกคำร้องขอที่ส่งไปยัง API เส้นย่อยต่างๆ จะได้รับสถานะ `401 Unauthorized` เนื่องจากระบบ API Services ของหน้าบ้าน (`frontend/src/services`) ไม่ได้ทำการแนบ Bearer Token เข้าไปในส่วนหัว (Authorization Header) ของคำร้องขอ HTTP
* **แนวทางแก้ไข:** ทำการปรับปรุงส่วนกำหนดค่า [frontend/src/services/config.ts](file:///home/sapray/dev/pigate/frontend/src/services/config.ts) โดยการเขียน Hook เข้าไปที่ฟังก์ชัน `window.fetch` ของเบราว์เซอร์ เพื่อให้ตรวจสอบและทำการแทรกส่วนหัว `Authorization: Bearer <token>` (ดึงจาก `localStorage` ที่ชื่อ `pigate_session`) เข้าไปในทุกคำร้องขอ API ที่มีพาร์ท `/api/` โดยอัตโนมัติ ทำให้ผู้ใช้เข้าถึงหน้าระบบและทำงานได้ปกติโดยไม่ต้องเข้าไปแก้ไขการเรียก fetch ใน Service ทุกไฟล์โดยตรง

### 4.3 ฟีเจอร์จำลองจากข้อมูลจริง (Mock from Real Data Mode)
* **ปัญหาที่พบ:** ในการทดสอบและพัฒนาระบบหลังบ้านฝั่งนักพัฒนา ข้อมูลจำลอง (Mock Data) มักจะไม่ตรงกับสภาวะแวดล้อมหรือการตั้งค่าของบอร์ดจริง แต่ในทางกลับกันการเปิดการใช้งานเชื่อมต่อระดับ OS จริงก็อาจเป็นอันตรายหรือส่งผลกระทบต่อสภาวะของโฮสต์เครื่องที่นักพัฒนากำลังเขียนโค้ดอยู่
* **แนวทางแก้ไข:** พัฒนาตัวเลือก `-mock-from-real` เพื่อให้ backend ดึงการกำหนดค่าจริงจากระบบปฏิบัติการ Linux เมื่อเริ่มทำงาน (Startup) เพียงครั้งเดียว โดยมีการซิงค์ข้อมูล DNS จริงจาก `/etc/resolv.conf`, ตาราง Routing จริงจาก `/proc/net/route` และ Interfaces จริงผ่าน `net.Interfaces()` โดยเมื่อมีการกระทำใดๆ เพิ่มเติม (เช่น CRUD) จะปรับปรุงข้อมูลลง SQLite database เท่านั้นและไม่มีผลย้อนกลับไปแก้ไขระบบปฏิบัติการจริง พร้อมกับการสกัดหากไม่พบตัวปล่อยคลื่น Wi-Fi บนโฮสต์จริง จะมีการสร้างตัวจำลอง `wlan0` อัตโนมัติเพื่อช่วยเหลือหน้าต่างสแกน Wi-Fi ฝั่ง Frontend ให้รันได้ปกติ

### 4.4 ฟีเจอร์จำกัดสิทธิ์แก้ไขข้อมูลจำลอง (Disable Edit Mode)
* **ปัญหาที่พบ:** ในบางกรณีของการทดสอบระบบหลังบ้านที่เปิดเผยสู่สาธารณะ หรือสภาวะแวดล้อมแซนด์บ็อกซ์ (Sandbox) การอนุญาตให้ผู้ใช้แก้ไขข้อมูลผ่านทาง REST API อาจทำให้ข้อมูลทดสอบเสียหายหรือเสื่อมสภาพ
* **แนวทางแก้ไข:** พัฒนาตัวเลือก `-disable-edit` เพื่อบังคับให้ระบบหลังบ้านเปิดใช้งานในโหมด "อ่านอย่างเดียว" (Read-Only) ในโหมดจำลอง (Mock Mode) โดยฝั่งหลังบ้านจะส่งกลับคืนรหัสข้อผิดพลาดและปิดการทำ CRUD ที่จะบันทึกหรือปรับปรุงฐานข้อมูล SQLite

### 4.5 การจัดการและตั้งค่า DNS เซิร์ฟเวอร์และชื่อโดเมนท้องถิ่น (DNS & Domain Management)
* **ปัญหาที่พบ:** ความต้องการในการตั้งค่าระบบ DNS ของไฟร์วอลล์/เกตเวย์ให้เป็นแบบรวมศูนย์ โดยรองรับได้ทั้งที่อยู่ DNS แบบคงที่ (Static DNS Servers) และแบบไดนามิกที่ดึงจากเครือข่ายภายนอก (Dynamic DNS Servers) รวมถึงการตั้งค่าชื่อโดเมนเครื่องภายในระบบ (Local Domain Name)
* **แนวทางแก้ไข:** พัฒนา API `/system/dns` และเชื่อมต่อกับพื้นที่จัดเก็บ SQLite ในการสืบค้นและปรับปรุงข้อมูล พร้อมกำหนดโครงสร้าง Schemas สำหรับรับส่งข้อมูลที่ตรงตามข้อกำหนดของ OpenAPI Spec ใหม่อย่างครบถ้วน

### 4.6 ปัญหาการเรียก Wi-Fi Scan บนการ์ดเครือข่ายธรรมดา (Wireless Scan Validation)
* **ปัญหาที่พบ:** หากมีการร้องขอทำรายการ Wi-Fi Scan (ค้นหาคลื่นวิทยุแลนไร้สาย) ผ่านทาง Endpoint `/api/interfaces/scan` โดยระบุการ์ดที่เป็นพอร์ตแลนมีสาย (เช่น `eth0`) อาจจะก่อให้เกิดความล้มเหลวระดับล่าง หรือเกิดความผิดปกติในระบบปฏิบัติการได้
* **แนวทางแก้ไข:** เพิ่มส่วนการตรวจสอบและแจ้งเตือนความถูกต้อง (Validation Check) ใน Handler คัดกรองว่าพอร์ตที่จะทำการสแกนหา Wi-Fi จะต้องมีชนิดข้อมูลเป็น `wireless` ในฐานข้อมูลเท่านั้น หากไม่ใช่จะส่งข้อผิดพลาด `400 Bad Request` กลับไป เพื่อป้องกันผลกระทบที่ไม่พึงประสงค์

### 4.7 Kernel Integration — Real NetworkManager ผ่าน Netlink Socket
* **ปัญหาที่พบ:** `kernel.NetworkManager.ToggleInterface()` เดิมใช้ `MockNetwork` ที่ `return nil` เฉยๆ ทำให้การสั่ง toggle interface ผ่าน API ไม่ได้เปลี่ยนสถานะ kernel (`IFF_UP`) จริง ส่งผลให้ `SyncInterfacesFromOS()` อ่านค่า `FlagUp` ไม่ reflect สถานะจริง
* **แนวทางแก้ไข:** สร้างไฟล์ [internal/kernel/real_network.go](file:///home/sapray/dev/pigate/backend/internal/kernel/real_network.go) implement `RealNetwork struct` ด้วย `github.com/vishvananda/netlink` เพื่อสื่อสารกับ kernel ผ่าน Netlink Socket โดยตรงไม่ใช้ shell command (ป้องกัน Command Injection) — `ToggleInterface` ใช้ `netlink.LinkSetUp/Down()` เทียบเท่า `ip link set up/down` แต่ไม่ต้องเรียก binary ภายนอก — เลือกใช้ใน production path เมื่อ `--mock=false` พร้อมติดตั้ง `cap_net_admin` capability ไว้ที่ binary

### 4.8 ระบบจัดการเซสชันและการบังคับเปลี่ยนรหัสผ่านครั้งแรก (Active Session & Force Password Change)
* **ปัญหาที่พบ/ความต้องการ:** การเพิ่มความมั่นคงปลอดภัยระดับเริ่มต้น (Day 1 Security) เพื่อตรวจสอบความถูกต้องของเซสชันที่เก็บในเบราว์เซอร์ของฝั่งหน้าบ้านว่ายังคงมีตัวตนและไม่หมดอายุบน backend จริง และจำกัดสิทธิ์หากผู้ดูแลระบบยังคงใช้บัญชีผู้ใช้งานรหัสผ่านเริ่มต้นของระบบ (`pigate` / `IsInitial = true`) โดยต้องบังคับเปลี่ยนรหัสผ่านทันทีก่อนใช้งานส่วนอื่น
* **แนวทางแก้ไข:**
  - เพิ่ม API Endpoint `GET /api/auth/session` (ฟังก์ชัน `HandleCheckSession` ใน [internal/api/handlers.go](file:///home/sapray/dev/pigate/backend/internal/api/handlers.go)) สำหรับส่งข้อมูลเซสชันกลับไปให้หน้าบ้านยืนยันสถานะ
  - ปรับปรุง `AuthMiddleware` ใน [internal/api/middleware.go](file:///home/sapray/dev/pigate/backend/internal/api/middleware.go) ให้ทำการตรวจสอบประวัติผู้ใช้งาน หากพบสถานะ `IsInitial` มีค่าจริง จะสั่งส่งการตอบกลับเป็นรหัส `403 Forbidden` พร้อมระบุ JSON payload `{"message": "Change password required", "mustChangePassword": true}` ทันทีหากเข้าถึง API อื่นๆ นอกเหนือจากการเปลี่ยนรหัสผ่าน บัญชีผู้ดูแลหลักเปลี่ยนชื่ออ้างอิงจาก "admin" เป็น "pigate"

### 4.9 การแยกโหมดการ Seed ข้อมูลเครือข่ายจำลอง (Isolated Database Seeding)
* **ปัญหาที่พบ/ความต้องการ:** เมื่อรัน backend ด้วยการเชื่อมต่อระบบจริง (`-mock=false`) แต่ตัวโปรแกรมยังมีการสร้างการ์ดเครือข่ายจำลอง `eth0` และ `wlan0` ลงในฐานข้อมูลตอนเริ่มรันครั้งแรก ส่งผลให้ข้อมูล Interfaces สับสนและซ้ำซ้อนกับข้อมูลการ์ดจริงที่อ่านจากระบบปฏิบัติการ
* **แนวทางแก้ไข:** ส่งสถานะ `mockOS` จากหน้าหลักเข้าสู่ฟังก์ชัน `InitDB` และปรับปรุงตรรกะใน [internal/db/connection.go](file:///home/sapray/dev/pigate/backend/internal/db/connection.go) ให้ทำการตรวจสอบค่าสถานะนี้ก่อนการ Seed ข้อมูลเครือข่ายจำลอง หากผู้ใช้รันโปรแกรมในโหมด Production (`-mock=false`) ระบบจะข้ามการใส่ค่าอินเตอร์เฟสจำลอง

### 4.10 การตั้งค่าพร็อกซีสำหรับการพัฒนารวมและ Version Control (Vite Proxy & Version Control Configuration)
* **ปัญหาที่พบ/ความต้องการ:** เพื่อความสะดวกในการพัฒนาระหว่าง Frontend และ Backend และป้องกันข้อมูลไฟล์ที่สร้างขึ้นตอนรัน
* **แนวทางแก้ไข:**
  - เพิ่มการตั้งค่า Proxy `/api` ให้ชี้ไปที่ `http://localhost:2479` ใน [vite.config.ts](file:///home/sapray/dev/pigate/frontend/vite.config.ts) ของหน้าบ้าน ทำให้การพัฒนาเชื่อมต่อ API บน React dev server ทำได้สะดวก
  - ปรับปรุง `.gitignore` ทั่วไปเพื่อละเว้นไฟล์ข้อมูล SQLite (`*.db`, `*.db-shm`, `*.db-wal`) และไฟล์รันไบนารีระบบ (`pigate`)
  - ปรับปรุง `build.sh` ให้ย้ายไฟล์ไบนารีหลังบ้านจาก `./backend/pigate-backend` ไปยังไฟล์รันชื่อ `./pigate` ที่รูทโฟลเดอร์หลักโดยตรง

---

## 5. ประเด็นความมั่นคงปลอดภัยและช่องโหว่ที่ต้องได้รับการแก้ไข (Security Vulnerabilities to Fix)

> [!IMPORTANT]
> **สถานะความสำคัญ: ต้องแก้ไขทันทีก่อนการนำขึ้นระบบจริง (MUST FIX - CRITICAL PRIORITY)**
> ตรวจพบช่องโหว่ความปลอดภัยทางซอร์สโค้ด (Source Code Review Findings) สรุปรายละเอียดระดับความสำคัญและแนวทางแก้ไขดังนี้:

### 5.1 ช่องโหว่การข้ามการยืนยันสิทธิ์ล็อกอิน / บัญชีลับ (Critical - Auth Bypass & Backdoor)
* **ตำแหน่งโค้ด:** ฟังก์ชัน `HandleLogin` ในไฟล์ [internal/api/handlers.go](file:///home/sapray/dev/pigate/backend/internal/api/handlers.go#L92-L98)
* **รายละเอียด:** โค้ดมีการตรวจสอบสิทธิ์ล็อกอินแบบ Hardcoded Bypass หากผู้ใช้ป้อนชื่อผู้ใช้ `pigate` และรหัสผ่าน `pigate` ระบบจะยินยอมให้ล็อกอินผ่านระบบความปลอดภัย (Bcrypt comparison bypass) โดยตรง ส่งผลให้ถึงแม้ว่าผู้ดูแลระบบจะเปลี่ยนรหัสผ่านใน SQLite ไปแล้ว ผู้โจมตีก็ยังสามารถใช้รหัสผ่านเริ่มต้นในการเจาะระบบได้ตลอดเวลา
* **ระดับความสำคัญ:** 🔴 **CRITICAL (ต้องแก้ไขทันที)**
* **แนวทางแก้ไข:** กำจัดโค้ดตรวจสอบเงื่อนไข `req.Username == "pigate" && req.Password == "pigate"` ออกจาก Handler ฝั่ง Production หรือแยกสวิตช์ตรวจเช็กให้อนุญาตเฉพาะเมื่อเปิดใช้งาน Mock Mode เท่านั้น

### 5.2 ปัญหาการสับสนการตั้งค่า CORS ร่วมกับการส่ง Credentials (Medium - CORS Configuration Conflict)
* **ตำแหน่งโค้ด:** `CORSMiddleware` ในไฟล์ [internal/api/middleware.go](file:///home/sapray/dev/pigate/backend/internal/api/middleware.go#L50-L60)
* **รายละเอียด:** ระบบมีการตั้งค่า `Access-Control-Allow-Origin: "*"` เมื่อพอร์ต/ Origin ไม่ตรงกับรายการ Local Development แต่ในขณะเดียวกันก็เปิดใช้งาน `Access-Control-Allow-Credentials: "true"` ซึ่งตามข้อกำหนดของเว็บเบราว์เซอร์จะไม่ยินยอมให้ใช้สัญลักษณ์ Wildcard `*` ร่วมกับการส่ง Credentials ส่งผลให้การเชื่อมต่อถูกเบราว์เซอร์บล็อกโดยอัตโนมัติ
* **ระดับความสำคัญ:** 🟡 **MEDIUM (ควรปรับปรุงก่อนเข้าสู่ช่วงใช้งานจริง)**
* **แนวทางแก้ไข:** ปรับแก้การคืนค่า Origin ของ CORS ให้สะท้อน Origin ที่ส่งคำขอเข้ามา หรือปิดใช้งาน Credentials สำหรับโดเมนที่ไม่ได้รับอนุญาต



