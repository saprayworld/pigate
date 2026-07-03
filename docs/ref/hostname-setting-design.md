# Hostname Setting Design — ตั้งค่า Hostname + Share hostname with DHCP

> เอกสารออกแบบ (ฉบับปรับปรุง — เขียนทับแผนเดิมที่ล้าสมัย) สำหรับฟีเจอร์:
> 1. **Hostname** — กำหนด/แก้ไข hostname ของเครื่อง PiGate
> 2. **Share hostname with DHCP** — Toggle ให้ dhcpcd (ฝั่ง DHCP *Client* ของ PiGate)
>    ส่ง hostname ไปบอก Router ฝั่ง WAN ผ่าน DHCP Option 12
>    (ไม่เกี่ยวกับ DHCP Server ที่แจก IP ให้เครื่องลูกในวง LAN)

---

## 0. เหตุผลที่ต้องออกแบบใหม่ (ทำไมแผนเดิมใช้ไม่ได้)

ตรวจสอบโค้ดปัจจุบัน (2026-07-03) แล้ว **ยังไม่มีการ implement ใด ๆ**:
ไม่มี endpoint `/api/system/hostname`, ไม่มีตาราง `system_hostname_settings`,
ไม่มี UI, และ `Dashboard.tsx:679` ยัง hardcode `"PiGate-RPI5"` อยู่

แผนเดิมมีปัญหาที่ทำตามไม่ได้แล้ว:

1. **ใช้ shell exec** — `execCommand("sudo", "dhcpcd", "--reconfigure")` และ
   `hostnamectl set-hostname` ขัดกับกฎหลักของโปรเจกต์ (No shell execution —
   ต้องใช้ Netlink/D-Bus เท่านั้น ดู `docs/tech_stack_design.md`)
2. **เขียนไฟล์ root-owned โดยตรง** — `/etc/hostname` และ `/etc/dhcpcd.conf`
   เป็นของ root แต่ pigate รันเป็น user ธรรมดา (capability-only) เขียนไม่ได้
3. **สถาปัตยกรรมเปลี่ยนไปแล้ว** — ปัจจุบัน dhcpcd รันเป็น `dhcpcd@<iface>.service`
   (systemd template, root ของตัวเอง) ควบคุมผ่าน D-Bus + polkit
   (`kernel/dhcpcd.go`, install.sh STEP 2.2/3) ไม่ใช่ให้ pigate เรียก dhcpcd ตรง ๆ
4. **วาง logic ผิดชั้น** — แผนเดิมให้ `service/dhcpcd.go` แตะไฟล์ OS ตรง ๆ
   ซึ่งผิด layering (การแตะ OS ต้องอยู่ใน `kernel/` เท่านั้น)

---

## 1. สถานะปัจจุบัน (Current State — ตรวจสอบแล้ว)

| จุด | สถานะ |
|-----|--------|
| Backend API `/api/system/hostname` | ❌ ยังไม่มี (`router.go` มีแค่ time/dns/password/services/reboot/…) |
| ตาราง `system_hostname_settings` | ❌ ยังไม่มี |
| `systemService.ts` — get/updateHostname | ❌ ยังไม่มี |
| `SettingsMaintenance.tsx` — Card Hostname | ❌ ยังไม่มี (มี Card Password + Time/NTP ใน tab "settings") |
| `Dashboard.tsx` แสดง Hostname | ⚠️ hardcode `"PiGate-RPI5"` (บรรทัด ~679) |
| กลไก dhcpcd | ✅ มีแล้ว: `dhcpcd@.service` (`ExecStart=dhcpcd -B -q %I`) สั่ง start/stop ผ่าน D-Bus (`kernel/dhcpcd.go` → `StartServiceViaDBus`/`StopServiceViaDBus`) มี polkit rule อนุญาต prefix `dhcpcd@` แล้ว |
| D-Bus helpers ใน kernel | ✅ มี `StartServiceViaDBus`, `StopServiceViaDBus` (`real_network.go`), `RestartServiceViaDBus` (`dns.go`) |

---

## 2. แนวทางที่เลือก (Design Decision)

### 2.1 ตั้ง Hostname → ใช้ D-Bus `org.freedesktop.hostname1` (systemd-hostnamed)

- เรียก method `SetStaticHostname(name, false)` (และ `SetHostname` สำหรับ transient
  เพื่อให้ kernel hostname เปลี่ยนทันที) บน `org.freedesktop.hostname1`
- **ข้อดี**: hostnamed เป็นคนเขียน `/etc/hostname` ให้เอง (atomic, ถูกต้องตาม distro)
  pigate ไม่ต้องแตะไฟล์ root เลย และเป็น D-Bus ล้วน ตามกฎโปรเจกต์
- **ต้องเพิ่ม polkit rule** ใน install.sh: action id
  `org.freedesktop.hostname1.set-static-hostname` และ
  `org.freedesktop.hostname1.set-hostname` สำหรับ `subject.user == "pigate"`
  — เป็น **addRule บล็อกใหม่** แยกจากบล็อกเดิม เพราะบล็อกเดิมดักเฉพาะ
  `org.freedesktop.systemd1.manage-units`

### 2.2 Share hostname with DHCP → ไฟล์ config dhcpcd ที่ pigate เป็นเจ้าของ

หลักการของ dhcpcd: จะส่ง DHCP Option 12 ก็ต่อเมื่อมี directive `hostname`
ในไฟล์ config (ถ้าไม่มี = ไม่ส่ง)

- แก้ `dhcpcd@.service` (ใน install.sh) ให้ชี้ config ที่ pigate จัดการได้:
  `ExecStart=dhcpcd -B -q -f /var/lib/pigate/dhcpcd.conf %I`
  (`/var/lib/pigate` เป็นของ `pigate:netdev` อยู่แล้ว — install.sh STEP 4)
- pigate เขียนไฟล์นี้แบบ **atomic (temp + rename)** ตาม pattern เดียวกับ
  wpa_supplicant conf (`docs/wifi_wpa_working_instruction.md`) เนื้อไฟล์มีแค่
  บรรทัดคงที่จาก whitelist เท่านั้น:
  - toggle **เปิด** → มีบรรทัด `hostname` (dhcpcd จะอ่าน hostname ปัจจุบันของระบบไปส่งเอง)
  - toggle **ปิด** → ไม่มีบรรทัดนี้ (ไฟล์ว่าง/มีแต่ comment)
- จากนั้น restart `dhcpcd@<iface>` เฉพาะ interface ที่ mode = dhcp ผ่าน
  `DhcpcdManager` (D-Bus) เพื่อให้ค่ามีผล

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
- แก้ `/etc/dhcpcd.conf` ตรง ๆ → ไฟล์ root-owned, ต้องเพิ่ม sudoers/chown = ขยาย attack surface
- `dhcpcd --reconfigure` ผ่าน exec → ผิดกฎ no-exec และ pigate ไม่มีสิทธิ์คุย control socket ของ dhcpcd ที่รันเป็น root อยู่แล้ว

### 2.3 Source of truth + Startup

- ค่า config เก็บใน SQLite (`system_hostname_settings`, แถวเดียว id=1)
- ตอน seed ครั้งแรก **อ่าน hostname จริงจาก OS** (`os.Hostname()`) มาใส่ DB
  — ห้าม hardcode "PiGate-RPI5"
- ตาม pattern "apply config at startup": เพิ่ม `HostnameService.InitApplyConfig()`
  เรียกใน `main.go` **ก่อน** `dhcpcdService.SyncActiveInterfaces()` เพื่อให้
  DHCP request แรกส่งชื่อที่ถูกต้อง

---

## 3. ขั้นตอนการทำงาน (Implementation Plan)

### Phase 1 — Model + Database

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/model/types.go` | เพิ่ม struct `SystemHostnameSettings { Hostname string \`json:"hostname"\`; ShareWithDhcp bool \`json:"shareWithDhcp"\` }` |
| `backend/internal/db/connection.go` | (1) เพิ่ม `CREATE TABLE IF NOT EXISTS system_hostname_settings (id INTEGER PRIMARY KEY CHECK(id = 1), hostname TEXT NOT NULL, share_with_dhcp INTEGER DEFAULT 0 CHECK(share_with_dhcp IN (0,1)))` ในกลุ่ม `queries` (2) เพิ่ม seed block ตาม pattern ของ `system_time_settings`: ถ้า COUNT = 0 → INSERT โดยใช้ `os.Hostname()` เป็นค่าเริ่มต้น, share_with_dhcp = 0 |
| `backend/internal/db/repository.go` | เพิ่ม `GetHostnameSettings() (*model.SystemHostnameSettings, error)` และ `UpdateHostnameSettings(s model.SystemHostnameSettings) error` (pattern เดียวกับ `GetSystemTimeSettings`/`UpdateSystemTimeSettings` บรรทัด ~1576) |

### Phase 2 — Kernel layer

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/kernel/interfaces.go` | (1) เพิ่ม interface ใหม่ `HostnameManager { GetHostname() (string, error); SetHostname(name string) error }` (2) ขยาย `DhcpcdManager` เพิ่ม `SetShareHostname(share bool) error` (เขียน config) และ `RestartDhcpcd(ifaceName string) error` |
| `backend/internal/kernel/real_hostname.go` (ไฟล์ใหม่) | implement `RealHostnameManager` ด้วย godbus: connect system bus → object `/org/freedesktop/hostname1` → call `SetStaticHostname(name, false)` + `SetHostname(name, false)`; `GetHostname` อ่าน property `StaticHostname` (หรือ fallback `os.Hostname()`) |
| `backend/internal/kernel/dhcpcd.go` | (1) `SetShareHostname`: เขียน `/var/lib/pigate/dhcpcd.conf` แบบ atomic — เนื้อหา fixed เท่านั้น (header comment + บรรทัด `hostname` เมื่อ share=true) **ห้าม interpolate ข้อมูลจากผู้ใช้ลงไฟล์เด็ดขาด** (2) `RestartDhcpcd`: เรียก `RestartServiceViaDBus(dhcpcdUnitName(iface))` (helper มีอยู่แล้วใน `dns.go`) |
| `backend/internal/kernel/mock.go` | เพิ่ม `MockHostnameManager` (เก็บค่าใน memory + log) และ mock ของเมธอดใหม่ใน DhcpcdManager — **ถ้าลืม build พังทั้งโปรเจกต์** |

### Phase 3 — Service layer

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/service/hostname.go` (ไฟล์ใหม่) | สร้าง `HostnameService` (deps: `repo`, `kernel.HostnameManager`, `kernel.DhcpcdManager`, `*InterfaceService`) มีเมธอด: (1) `Get()` — คืนค่าจาก DB (ถ้า DB ว่างให้ fallback อ่านจาก kernel) (2) `Update(s)` — validate → เซฟ DB → `SetHostname` ผ่าน D-Bus → ถ้าค่า share เปลี่ยนหรือ hostname เปลี่ยนขณะ share=on → `SetShareHostname` + restart `dhcpcd@` เฉพาะ interface ที่ `AddressingMode == "dhcp"` และ status up (3) `InitApplyConfig()` — apply hostname + share config จาก DB ตอน boot |
| `backend/internal/service/hostname_test.go` (ไฟล์ใหม่) | เทสต์ validation, การเรียก kernel (ใช้ mock/tracker pattern เหมือน `routing_test.go`), เคส share toggle |
| Validation (ใน service) | RFC 1123 label: `^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`, ยาว ≤ 63, ห้ามว่าง — ทำที่ service layer เพื่อให้ครอบทั้ง API และ import config |

### Phase 4 — API layer + main.go

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/api/handlers.go` | เพิ่ม `HandleGetHostname` (GET) และ `HandleUpdateHostname` (PUT — decode, เรียก `hostnameService.Update`, คืน 400 พร้อมข้อความไทยเมื่อ validate ไม่ผ่าน) + เพิ่ม field `hostnameService` ใน struct `Server` + `NewServer(...)` |
| `backend/internal/api/router.go` | `authRoute("GET /api/system/hostname", ...)`, `authRoute("PUT /api/system/hostname", ...)` (วางกลุ่มเดียวกับ `/api/system/time` บรรทัด ~103) |
| `backend/internal/api/handlers.go` (export/import) | `HandleExportConfig`: เพิ่ม hostname settings ใน backup JSON; `HandleImportConfig`: รับ field ใหม่แบบ optional (backup เก่าไม่มี field นี้ต้องไม่พัง — ใช้ pointer + nil check) |
| `backend/cmd/pigate/main.go` | (1) เลือก real/mock `HostnameManager` ตาม flag `-mock` (2) สร้าง `HostnameService` (3) เรียก `hostnameService.InitApplyConfig()` **ก่อน** `dhcpcdService.SyncActiveInterfaces()` (บรรทัด ~131) |
| `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` | เพิ่ม path `/system/hostname` (GET/PUT) + schema `SystemHostnameSettings` — ต้องแก้ทั้ง 2 ไฟล์ให้ตรงกัน |

### Phase 5 — install.sh (ต้องทำก่อนทดสอบบนเครื่องจริง)

| จุด | สิ่งที่ทำ |
|---|---|
| STEP 2.2 (dhcpcd@.service) | เปลี่ยน `ExecStart=${DHCPCD_BIN} -B -q %I` → `ExecStart=${DHCPCD_BIN} -B -q -f /var/lib/pigate/dhcpcd.conf %I` |
| STEP 3 (polkit) | เพิ่ม `polkit.addRule` บล็อกใหม่: อนุญาต action `org.freedesktop.hostname1.set-static-hostname` และ `org.freedesktop.hostname1.set-hostname` เมื่อ `subject.user == "pigate"` |
| STEP 4 (directories) | สร้างไฟล์ baseline `/var/lib/pigate/dhcpcd.conf` (ว่าง/มีแต่ comment) พร้อม `chown pigate:netdev` + `chmod 0644` ถ้ายังไม่มี |

### Phase 6 — Frontend

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `frontend/src/services/systemService.ts` | เพิ่ม interface `SystemHostnameSettings`, `getHostname()`, `updateHostname()` + mock ผ่าน localStorage (pattern เดียวกับ `getTimeSettings`/`updateTimeSettings` ที่มีอยู่) |
| `frontend/src/pages/SettingsMaintenance.tsx` | เพิ่ม Card "System Identity" ใน tab `settings` (วางก่อน Card Time/NTP): Input hostname (font-mono) + คำอธิบาย rule ตัวอักษร, Switch "Share hostname with DHCP (ส่งชื่อเครื่องไปบอก Router ฝั่ง WAN)", ปุ่ม Save + feedback state (pattern เดียวกับ `timeFeedback`) — ใช้ shadcn/ui primitives และ semantic color variables เท่านั้น (ห้าม shadow-*, ห้าม hardcode สี) |
| `frontend/src/pages/Dashboard.tsx` | แทน hardcode `"PiGate-RPI5"` (บรรทัด ~679): เพิ่ม state + `useEffect` เรียก `systemService.getHostname()` ตอน mount แล้ว render ค่าจริง (fallback เป็นค่าเดิมถ้า fetch fail) |
| Validation ฝั่ง UI | regex เดียวกับ backend + แจ้ง error ภาษาไทยก่อนยิง API |

### Phase 7 — ทดสอบ

1. `cd backend && go build ./... && go test ./...`
2. mock mode: `./pigate-backend -mock=true` → ทดสอบ GET/PUT ผ่าน UI + ค่า persist ใน DB
3. `cd frontend && yarn build && yarn lint`
4. เครื่องจริง (Pi): รัน install.sh ใหม่ → ตรวจ (a) `hostnamectl status` เห็นชื่อใหม่หลัง Save (b) เปิด toggle แล้วดูใน Router ว่าเห็นชื่อเครื่อง (c) ปิด toggle → renew lease แล้วชื่อหาย (d) reboot แล้ว hostname ยังถูกต้อง (InitApplyConfig)

---

## 4. ข้อควรระวัง (Cautions)

1. **ห้าม exec เด็ดขาด** — ห้ามใช้ `hostnamectl`, `sudo dhcpcd` ใด ๆ ทั้งสิ้น
   ทุกอย่างผ่าน godbus (`org.freedesktop.hostname1` + `org.freedesktop.systemd1`)
   ตามกฎใน CLAUDE.md / tech_stack_design.md

2. **polkit ของ hostname1 เป็นคนละ action กับ systemd1** — rule เดิมใน
   `10-pigate-system.rules` ดักเฉพาะ `org.freedesktop.systemd1.manage-units`
   ถ้าไม่เพิ่มบล็อกใหม่ การเรียก `SetStaticHostname` จะโดน `Access denied`
   และ**เครื่องที่ติดตั้งไปแล้วต้องรัน install.sh ซ้ำ** (หรือแก้ polkit + unit เอง)
   — ควรบันทึกเรื่อง upgrade path นี้ใน README/release note

3. **การย้าย dhcpcd ไปใช้ `-f /var/lib/pigate/dhcpcd.conf` เปลี่ยน default behavior**
   — ปัจจุบัน unit อ่าน `/etc/dhcpcd.conf` ของ distro ซึ่งมักมีบรรทัด `hostname`,
   `duid`, `persistent`, `option rapid_commit` ฯลฯ อยู่แล้ว เมื่อสลับมาใช้ไฟล์ของเรา
   ค่า default พวกนี้จะหายไปทั้งหมด:
   - แปลว่า *พฤติกรรมวันนี้อาจส่ง hostname อยู่แล้ว* — หลังเปลี่ยนจะ "ปิดโดย default"
     ตามค่า DB ซึ่งตรงกับ design แต่ให้ตระหนักว่าพฤติกรรมผู้ใช้เดิมอาจเปลี่ยน
   - ต้องตัดสินใจว่า baseline ในไฟล์ควรมี directive อะไรบ้าง (แนะนำเริ่ม minimal
     แล้วทดสอบว่า lease/DNS ยังปกติ) — **ทดสอบ DHCP บนเครื่องจริงหลังเปลี่ยน unit เสมอ**

4. **ความปลอดภัยของไฟล์ config ที่ root (dhcpcd) อ่านแต่ pigate เขียนได้** —
   ลดความเสี่ยงโดย: เนื้อไฟล์ต้องเป็น **บรรทัดคงที่จาก whitelist เท่านั้น**
   ห้ามเอา string จากผู้ใช้ (รวมถึงตัว hostname เอง) ไป interpolate ลงไฟล์
   — directive `hostname` เปล่า ๆ ก็สั่งให้ dhcpcd ส่งชื่อระบบปัจจุบันได้อยู่แล้ว
   จึงไม่มีช่อง injection

5. **hostnamed ไม่แก้ `/etc/hosts`** — ถ้าเครื่องมีบรรทัด `127.0.1.1 <ชื่อเก่า>`
   จะค้างชื่อเก่าไว้ อาจทำให้ `sudo` ช้า/มี warning "unable to resolve host"
   ทางแก้ที่ไม่ต้องเขียนไฟล์ root: แนะนำใน install.sh ให้ตรวจว่า
   `/etc/nsswitch.conf` มี `myhostname` ใน line `hosts:` (nss-myhostname ของ systemd
   resolve ชื่อตัวเองได้โดยไม่พึ่ง /etc/hosts) — อย่างน้อยต้องบันทึกเป็น
   known limitation ในเอกสาร

6. **Restart dhcpcd@ = renew lease = เน็ต WAN สะดุดชั่วครู่** — restart เฉพาะเมื่อ
   ค่าที่มีผลจริงเปลี่ยน (share toggle เปลี่ยน หรือ hostname เปลี่ยนขณะ share=on)
   อย่า restart ทุกครั้งที่กด Save และควรเตือนผู้ใช้ใน UI ว่าการเปลี่ยนค่านี้
   อาจทำให้การเชื่อมต่อ WAN หลุดชั่วขณะ

7. **Validation ต้องอยู่ที่ service layer** — เพราะมีทางเข้า 2 ทาง (PUT ตรง +
   import config) ใช้ RFC 1123: `^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`
   ยาว ≤ 63 ตัว ห้ามว่าง — ฝั่ง hostnamed เองก็ validate อีกชั้นซึ่งดี
   แต่ error จาก D-Bus อ่านไม่รู้เรื่อง ให้ดัก error เองก่อนเพื่อข้อความไทยที่ชัดเจน

8. **แก้ interface ของ kernel ต้องแก้ mock ให้ครบ** — `HostnameManager` ใหม่ +
   เมธอดใหม่ใน `DhcpcdManager` ต้องมีทั้ง real และ mock ไม่งั้น compile ไม่ผ่าน
   (main.go เลือก impl ตาม flag `-mock`)

9. **Seed ค่าเริ่มต้นจากเครื่องจริง ไม่ hardcode** — ใช้ `os.Hostname()` ตอน seed
   ครั้งแรก มิฉะนั้นบูตครั้งแรกหลังอัปเกรด `InitApplyConfig` จะไปเปลี่ยนชื่อเครื่อง
   ผู้ใช้เป็น "PiGate-RPI5" โดยไม่ได้ตั้งใจ — **นี่คือ bug ร้ายแรงที่สุดที่ต้องกันไว้**

10. **Import config เก่า** — backup ที่ export ก่อนฟีเจอร์นี้ไม่มี field hostname
    ต้องใช้ pointer + nil check ใน `HandleImportConfig` (pattern เดียวกับ
    `SystemSettings *model.SystemTimeSettings` ที่มีอยู่) เพื่อไม่ให้ import พังหรือ
    reset hostname ทิ้ง

11. **โหมด `-disable-edit`** — PUT `/api/system/hostname` ต้องผ่าน middleware
    read-only เหมือน endpoint แก้ไขอื่น ๆ (ใช้ `authRoute` pattern เดิมก็ครอบให้แล้ว
    แต่ให้ยืนยันตอนเทสต์)

12. **Dashboard ยังเป็นฟีเจอร์ Mock** — แก้เฉพาะบรรทัด hostname ให้ดึงค่าจริง
    อย่าไปรื้อส่วนอื่นของ System Information card ที่ยัง hardcode (Firmware, OS Base)
    เพราะอยู่นอก scope งานนี้

---

## 5. ลำดับการลงมือทำ (แนะนำ)

```
1. Phase 1  model + DB + repository            → go build ผ่าน
2. Phase 2  kernel (interfaces + real + mock)  → go build ผ่าน
3. Phase 3  HostnameService + เทสต์            → go test ผ่าน
4. Phase 4  handlers + router + main.go + openapi (2 ไฟล์)
5. Phase 5  install.sh (polkit + unit + baseline conf)
6. Phase 6  frontend (systemService → Settings card → Dashboard)
7. Phase 7  ทดสอบ mock mode
8. Phase 8  ทดสอบเครื่องจริง (รัน install.sh ซ้ำก่อน) **ส่วนนี้ผู้ใช้จะทดสอบเอง หลังทำเสร็จให้แนะนำวิธีการด้วย**
```
