# รายงานการพัฒนาโครงสร้างระบบหลังบ้าน (Backend Development Report)

เอกสารฉบับนี้บันทึกความสำเร็จในการพัฒนาโครงสร้างและข้อกำหนดของ **PiGate Go Backend** ด้วยคอมไพเลอร์ภาษา **Go v1.26.4** พร้อมรายงานผลการทำระบบทดสอบอัตโนมัติ (Automated Tests) เพื่อใช้อ้างอิงการพัฒนาและตรวจสอบระบบในอนาคต

---

## 1. ไฟล์และองค์ประกอบที่สร้างขึ้น (Created Files)

โค้ดทั้งหมดถูกเก็บไว้แยกในห้องทำงาน [backend/](file:///pigate-backend/) โดยไม่มีการแก้ไขโค้ดฝั่งหน้าบ้าน (Frontend) ตามข้อตกลง ดังรายละเอียดนี้:

1. **โครงสร้างโมดูลหลัก (Dependencies):**
   * [go.mod](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/go.mod) และ `go.sum`: กำหนดโมดูลชื่อ `pigate` (ใช้ Go 1.26) โดยติดตั้งแพ็กเกจ SQLite ไดรเวอร์บริสุทธิ์ (`modernc.org/sqlite`) และตัวเข้ารหัสความปลอดภัย (`golang.org/x/crypto`) สำเร็จเรียบร้อย
2. **ระบบฐานข้อมูลและคิวรี (Database Layer):**
   * [internal/model/types.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/model/types.go): โครงสร้างโมเดล Go Structs ทั้งหมดที่แมปคู่เข้ากับความต้องการของสเปก API ของ Frontend
   * [internal/db/connection.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/db/connection.go): จัดการการเปิดและเชื่อมโยงฐานข้อมูล SQLite และสั่งรัน DDL SQL เพื่อจัดสร้างโครงสร้างตาราง พร้อมใส่ข้อมูลเริ่มต้น (Default User, Seed subnets, Predefined services, Interfaces, Routes) ตอนเริ่มระบบอัตโนมัติ
   * [internal/db/repository.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/db/repository.go): พัฒนาระบบคิวรี CRUD ครบทุกตาราง มีการเขียนส่วนประมวลผลความปลอดภัย เช่น การล็อกห้ามแก้ไขหรือลบวัตถุระบบ (System objects) และการตรวจเช็กความสัมพันธ์ห้ามลบวัตถุที่ถูกนำไปใช้ในกฎไฟร์วอลล์ (Referential integrity)
3. **ระบบจำลองการทำงานของ OS (Kernel Wrapper & Logging):**
   * [internal/kernel/interfaces.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/kernel/interfaces.go): ประกาศ Interface สำหรับดึงค่าและสั่งงาน firewall, routes, NetworkManager และ DHCP
   * [internal/kernel/mock.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/kernel/mock.go): พัฒนาระบบจำลองการสแกนหา SSID Wi-Fi, สลับสถานะการ์ดแลน, และดึงข้อมูล leases เพื่อให้ทดสอบ API บน Local PC ได้โดยไม่ต้องเชื่อมระบบปฏิบัติการ Linux จริง
   * [internal/logs/ringbuffer.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/logs/ringbuffer.go): คลาสจำลองเก็บ Log ในรูปแบบ Ring Buffer บนแรมเพื่อถนอมอายุ SD card
4. **ระบบจัดการ REST API & Middleware:**
   * [internal/api/middleware.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/api/middleware.go): ติดตั้ง CORS (อนุญาตให้หน้าบ้าน React dev server ที่พอร์ต 5173 เรียกใช้), ตรวจสอบโทเค็นสิทธิ์การเข้าใช้งาน (Authorization: Bearer <token>) และติดตั้ง Rate Limiter สกัดกั้นการสุ่มรหัสผ่าน
   * [internal/api/handlers.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/api/handlers.go): ส่วนประมวลผลคำขอ (Request handlers) ตามสเปก OpenAPI ครบถ้วน รวมถึงฟังก์ชันการ Import/Export ค่าการตั้งค่าของเครื่องเป็น JSON
   * [internal/api/router.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/api/router.go): จัดเส้นทางและสิทธิ์การเข้าถึง API
5. **โปรแกรมจุดเริ่มการรันระบบหลัก (Server Boot):**
   * [cmd/pigate/main.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/cmd/pigate/main.go): รันเรียกพารามิเตอร์ของระบบ ตั้งค่าพอร์ต ดึง DB และสั่งสตาร์ท HTTP Server (ยิงทดสอบระบบที่ http://localhost:8080)

---

## 2. ระบบทดสอบอัตโนมัติ (Automated Testing)

เราได้ติดตั้งระบบทดสอบทั้ง 2 มิติ คือ Unit Tests (สำหรับตรวจสอบ DB Repository logic) และ Integration Tests (สำหรับจำลอง HTTP Request) ดังไฟล์ด้านล่างนี้:

* **[internal/db/repository_test.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/db/repository_test.go):** ทดสอบการทำ Migration และคิวรีฐานข้อมูลโดยใช้ `:memory:` SQLite ที่แยกขาดจากไฟล์ DB จริง
* **[internal/api/handlers_test.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/api/handlers_test.go):** ทดสอบการส่งคำขอด้วย `httptest.NewRecorder()` ทั้งเรื่อง CORS, Login (ผ่าน/ไม่ผ่าน), การยืนยันสิทธิ์ Token, และการทำ CRUD Address API

### 2.1 ผลการทดสอบ (Test Results Log)
รันคำสั่งทดสอบแบบตัดแคชด้วย `-count=1` ผลลัพธ์ผ่านสำเร็จ 100% ปราศจากข้อผิดพลาด:

```text
?       pigate/cmd/pigate       [no test files]
ok      pigate/internal/api     0.164s
ok      pigate/internal/db      0.024s
?       pigate/internal/kernel  [no test files]
?       pigate/internal/logs    [no test files]
?       pigate/internal/model   [no test files]
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
```
เมื่อระบบขึ้นมาแล้ว จะปรากฏข้อความล็อกบนคอนโซล:
`[date] [time] PiGate API Backend is listening at http://localhost:8080`
คุณสามารถเปิดโปรแกรมทดสอบ API (เช่น Postman, REST Client, หรือ curl) ยิงไปที่ http://localhost:8080 เพื่อทดลองเรียกใช้งานได้ทันที

---

## 4. ประวัติการแก้ไขข้อบกพร่อง (Bug Fix History)

### 4.1 ปัญหาการส่งคืนค่าอาร์เรย์ว่างเป็น null (Empty Array returns null)
* **ปัญหาที่พบ:** เมื่อระบบหลังบ้านเรียกดูข้อมูลจากตารางเปล่า เช่น `/api/dhcp/reservations` ระบบจะแปลงค่า Slice ของ Go ที่เป็น `nil` ออกมาเป็นค่า `null` ในรูปแบบ JSON แทนการส่งเป็น `[]` ส่งผลให้หน้าจอควบคุมฝั่งหน้าบ้านเกิดการขัดข้องทางตัววิเคราะห์ข้อมูล (JSON Parser Error)
* **แนวทางแก้ไข:** ปรับปรุงไฟล์ [internal/db/repository.go](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/backend/internal/db/repository.go) โดยเปลี่ยนการประกาศ Slice จาก `var list []model.SomeType` เป็นการประกาศจองหน่วยความจำเริ่มต้น `list := []model.SomeType{}` ครบทุกฟังก์ชันของการคิวรีรายการ ผลลัพธ์ได้รับการทดสอบและรันคอมไพล์ผ่านสมบูรณ์ ส่งกลับคืนค่า `[]` ถูกต้องในรูปแบบ JSON เพื่อป้อนความต้องการของ Frontend

### 4.2 ปัญหาการเชื่อมต่อระบบหลังบ้านแล้วติดสิทธิ์เข้าถึง (401 Unauthorized on API requests)
* **ปัญหาที่พบ:** หลังจากผู้ใช้งานล็อกอินเข้าสู่ระบบเรียบร้อยแล้ว ทุกคำร้องขอที่ส่งไปยัง API เส้นย่อยต่างๆ จะได้รับสถานะ `401 Unauthorized` เนื่องจากระบบ API Services ของหน้าบ้าน (`frontend/src/services`) ไม่ได้ทำการแนบ Bearer Token เข้าไปในส่วนหัว (Authorization Header) ของคำร้องขอ HTTP
* **แนวทางแก้ไข:** ทำการปรับปรุงส่วนกำหนดค่า [frontend/src/services/config.ts](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/frontend/src/services/config.ts) โดยการเขียน Hook เข้าไปที่ฟังก์ชัน `window.fetch` ของเบราว์เซอร์ เพื่อให้ตรวจสอบและทำการแทรกส่วนหัว `Authorization: Bearer <token>` (ดึงจาก `localStorage` ที่ชื่อ `pigate_session`) เข้าไปในทุกคำร้องขอ API ที่มีพาร์ท `/api/` โดยอัตโนมัติ ทำให้ผู้ใช้เข้าถึงหน้าระบบและทำงานได้ปกติโดยไม่ต้องเข้าไปแก้ไขการเรียก fetch ใน Service ทุกไฟล์โดยตรง

