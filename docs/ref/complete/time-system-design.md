# Time System Design — ตั้งค่าเวลา / โซนเวลา / การซิงค์เวลา (NTP)

> เอกสารออกแบบสำหรับฟีเจอร์ระบบเวลา ครอบคลุม 3 เรื่อง:
> 1. **Timezone** — ตั้งเขตเวลาของเครื่อง (IANA timezone เช่น `Asia/Bangkok`)
> 2. **NTP Sync** — เปิด/ปิดการซิงค์เวลาอัตโนมัติ + กำหนด NTP Server เอง
> 3. **Manual Time** — ตั้งวันที่/เวลาด้วยมือ (ใช้ได้เฉพาะตอนปิด NTP)

---

## 1. สถานะปัจจุบัน (Current State — ตรวจสอบแล้ว 2026-07-03)

ระบบเวลามี "โครง" อยู่แล้วครึ่งทาง แต่**ยังไม่มีการ apply ลง OS จริงเลย**:

| จุด | สถานะ |
|-----|--------|
| ตาราง `system_time_settings` (timezone, ntp_sync, ntp_server) | ✅ มีแล้ว (`connection.go:322`) แต่ seed ค่า timezone เป็น `'Asia/Bangkok (GMT+7:00)'` ซึ่ง**ไม่ใช่ชื่อ IANA ที่ถูกต้อง** (มี " (GMT+7:00)" ติดมาด้วย) |
| `model.SystemTimeSettings` | ✅ มีแล้ว (`types.go:221`) |
| Repository Get/UpdateSystemTimeSettings | ✅ มีแล้ว (`repository.go:1576`) |
| API `GET/PUT /api/system/time` | ⚠️ มีแล้ว (`handlers.go:1345`) แต่**อ่าน/เขียนแค่ DB — ไม่ได้สั่ง OS อะไรเลย** |
| Kernel layer สำหรับเวลา (`TimeManager`) | ❌ ยังไม่มี |
| Service layer (`TimeService` + InitApplyConfig) | ❌ ยังไม่มี |
| polkit สำหรับ `org.freedesktop.timedate1` | ❌ ยังไม่มี (install.sh มีแค่ systemd1.manage-units + hostname1) |
| Frontend Card "System Time & NTP" | ⚠️ มีแล้ว (`SettingsMaintenance.tsx:587`) แต่ timezone เป็น native `<select>` hardcode 3 ตัวเลือก (value ปน GMT suffix) และมีกล่องเหลือง "Backend Integration" โชว์คำสั่ง `timedatectl` ให้ผู้ใช้ไปรันเอง (บรรทัด ~676-693) = ร่องรอย mock |
| ตั้งเวลาด้วยมือ (manual time) | ❌ ยังไม่มีทั้ง backend และ UI |
| Export/Import config | ⚠️ รวม `systemSettings` (time) อยู่แล้ว แต่ import แค่เซฟ DB ไม่ apply ลง OS |

**pattern อ้างอิงที่เพิ่งทำเสร็จ**: ฟีเจอร์ Hostname (`kernel/real_hostname.go`,
`service/hostname.go`, polkit บล็อก hostname1 ใน install.sh) ใช้โครงเดียวกันได้เกือบทั้งหมด

---

## 2. แนวทางที่เลือก (Design Decision)

### 2.1 ทุกอย่างผ่าน D-Bus `org.freedesktop.timedate1` (systemd-timedated)

| งาน | D-Bus method | polkit action id |
|---|---|---|
| ตั้ง timezone | `SetTimezone(tz, false)` — timedated จัดการ `/etc/localtime` + `/etc/timezone` ให้เอง | `org.freedesktop.timedate1.set-timezone` |
| เปิด/ปิด NTP | `SetNTP(enable, false)` — start/stop + enable/disable systemd-timesyncd ให้เอง | `org.freedesktop.timedate1.set-ntp` |
| ตั้งเวลาด้วยมือ | `SetTime(usec, false, false)` (microseconds since epoch, absolute) | `org.freedesktop.timedate1.set-time` |
| อ่านสถานะ live | properties: `Timezone`, `NTP`, `NTPSynchronized`, `TimeUSec` | (อ่านได้ ไม่ต้องขอสิทธิ์) |

ห้ามใช้ `timedatectl` / `date` ผ่าน exec เด็ดขาด (กฎ no-exec ของโปรเจกต์)

### 2.2 NTP Server → drop-in ของ systemd-timesyncd ที่ pigate เป็นเจ้าของ

timedate1 **ไม่มี API สำหรับตั้ง NTP server** — ต้องใช้ config file ของ
systemd-timesyncd แนวทางเดียวกับ dhcpcd conf ในฟีเจอร์ hostname:

- install.sh สร้าง `/etc/systemd/timesyncd.conf.d/` และไฟล์
  `50-pigate.conf` ที่ `chown pigate` ไว้ล่วงหน้า
- pigate เขียนไฟล์นี้แบบ **atomic (temp + rename)** เนื้อหา:
  ```ini
  [Time]
  NTP=<server ที่ validate แล้ว>
  ```
- แล้ว restart `systemd-timesyncd.service` ผ่าน D-Bus
  (`RestartServiceViaDBus` — มี helper อยู่แล้วใน `kernel/dns.go`)
  → **ต้องเพิ่ม `systemd-timesyncd.service` ใน polkit allowlist** ของบล็อก
  `systemd1.manage-units` เดิมด้วย

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
- เขียน `/etc/systemd/timesyncd.conf` ตรง ๆ → root-owned เขียนไม่ได้
- ใช้ chrony/ntpd → Raspberry Pi OS ใช้ systemd-timesyncd เป็น default อยู่แล้ว
  ไม่เพิ่ม dependency ใหม่ตามหลักโปรเจกต์

### 2.3 โครงข้อมูล + การ migrate ค่าเก่า

- **ตาราง DB เดิมใช้ต่อได้ ไม่ต้องเพิ่มคอลัมน์** — แต่ต้อง **migrate ข้อมูล**:
  ค่า timezone แบบเก่า `"Asia/Bangkok (GMT+7:00)"` ต้อง normalize เป็น IANA ล้วน
  (`"Asia/Bangkok"`) โดยตัด suffix ` (GMT...)` ทิ้งใน migration ของ `connection.go`
  และแก้ seed ให้เป็น `'Asia/Bangkok'`
- **เวลา (manual time) ไม่เก็บลง DB เด็ดขาด** — เวลาเป็น runtime state
  (เก็บแล้วบูตมา apply กลับ = หายนะ) จึงแยกเป็น endpoint ของตัวเอง ไม่อยู่ใน settings
- `GET /api/system/time` ปรับให้คืน **config จาก DB + สถานะ live จาก kernel**
  (currentTime, ntpSynchronized) เพื่อให้ UI แสดงเวลาปัจจุบันและสถานะซิงค์ได้จริง

### 2.4 API surface

| Endpoint | งาน |
|---|---|
| `GET /api/system/time` | คืน `{timezone, ntpSync, ntpServer, status: {currentTime, ntpSynchronized}}` |
| `PUT /api/system/time` | บันทึก config → apply ลง OS (timezone → NTP server → NTP toggle ตามลำดับ) |
| `POST /api/system/time/manual` (ใหม่) | body `{datetime: "<RFC3339>"}` — ตั้งเวลาด้วยมือ อนุญาตเฉพาะเมื่อ ntpSync=false |

### 2.5 รายการ Timezone ฝั่ง UI

ใช้ `Intl.supportedValuesOf("timeZone")` ของ browser สร้างรายการ (400+ โซน)
+ คำนวณ GMT offset แสดงประกอบด้วย `Intl.DateTimeFormat` — ไม่ต้อง hardcode
และไม่ต้องมี endpoint เพิ่ม (browser ยุค React 19 รองรับหมดแล้ว ใส่ fallback
list สั้น ๆ เผื่อไว้พอ)

---

## 3. ขั้นตอนการทำงาน (Implementation Plan)

### Phase 1 — Model + Database migration

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/model/types.go` | (1) เพิ่ม struct `TimeStatus { CurrentTime string \`json:"currentTime"\`; NTPSynchronized bool \`json:"ntpSynchronized"\` }` (2) เพิ่ม field `Status *TimeStatus \`json:"status,omitempty"\`` ใน `SystemTimeSettings` (pointer — PUT ไม่ต้องส่งมา) |
| `backend/internal/db/connection.go` | (1) แก้ seed จาก `'Asia/Bangkok (GMT+7:00)'` → `'Asia/Bangkok'` (2) เพิ่ม data migration: `UPDATE system_time_settings SET timezone = substr(...)` หรืออ่านค่ามา strip suffix ` (` ด้วย Go แล้วเขียนกลับ — รันได้ซ้ำโดยไม่พัง (idempotent) |

### Phase 2 — Kernel layer

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/kernel/interfaces.go` | เพิ่ม interface `TimeManager { GetTimeStatus() (*model.TimeStatus, error); SetTimezone(tz string) error; SetNTP(enable bool) error; SetTime(t time.Time) error; SetNTPServer(server string) error }` |
| `backend/internal/kernel/real_timedate.go` (ไฟล์ใหม่) | implement ด้วย godbus ตาม pattern `real_hostname.go`: object `/org/freedesktop/timedate1` — `SetTimezone`/`SetNTP`/`SetTime` เรียก method ตามตาราง §2.1; `GetTimeStatus` อ่าน properties `NTPSynchronized` + `TimeUSec`; `SetNTPServer` เขียน `/etc/systemd/timesyncd.conf.d/50-pigate.conf` แบบ atomic (temp+rename) แล้ว `RestartServiceViaDBus("systemd-timesyncd.service")` — restart เฉพาะเมื่อ NTP เปิดอยู่ |
| `backend/internal/kernel/mock.go` | เพิ่ม `MockTimeManager` (เก็บ state ใน memory + log, `GetTimeStatus` คืน `time.Now()` + synced=true) — **ลืมแล้ว build พังทั้งโปรเจกต์** |

### Phase 3 — Service layer

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/service/timesync.go` (ไฟล์ใหม่) | สร้าง `TimeService` (deps: `repo`, `kernel.TimeManager`) เมธอด: (1) `Get()` — DB config + `GetTimeStatus()` (2) `Update(s)` — validate → เซฟ DB → apply ตามลำดับ: `SetTimezone` (ถ้าเปลี่ยน) → `SetNTPServer` (ถ้าเปลี่ยน) → `SetNTP` (3) `SetManualTime(t)` — reject ถ้า config ntpSync=true → `kernel.SetTime` (4) `InitApplyConfig()` — apply timezone/NTP server/NTP toggle จาก DB ตอน boot (**ห้ามแตะ SetTime ใน startup เด็ดขาด**) |
| Validation (ใน service) | (1) timezone: `time.LoadLocation(tz)` ต้องผ่าน + ห้ามมีช่องว่าง/อักขระนอก `[A-Za-z0-9_+/-]` (2) NTP server: อนุญาตหลายตัวคั่นช่องว่างได้ตาม timesyncd แต่ validate ทีละ token — hostname (RFC 1123) หรือ IP เท่านั้น ยาว ≤ 253, **ห้ามมี newline/`[`/`]`/`=` เด็ดขาด** (กัน injection directive ลงไฟล์ ini) (3) manual datetime: parse RFC3339 + ปฏิเสธปีต่ำกว่า 2020/เกิน 2100 กันพิมพ์ผิด |
| `backend/internal/service/timesync_test.go` (ไฟล์ใหม่) | เทสต์: validation ทุกข้อ (โดยเฉพาะ injection string ใน ntpServer), ลำดับการ apply, SetManualTime ถูก reject เมื่อ NTP on, InitApplyConfig ไม่เรียก SetTime (ใช้ tracker pattern เหมือน `routing_test.go`) |

### Phase 4 — API layer + main.go

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/api/handlers.go` | (1) แก้ `HandleGetSystemTime`/`HandleUpdateSystemTime` ให้เรียกผ่าน `timeService` แทน repo ตรง (2) เพิ่ม `HandleSetManualTime` (POST) (3) เพิ่ม field `timeService` ใน struct `Server` + พารามิเตอร์ `NewServer(...)` (4) `HandleImportConfig`: ส่วน SystemSettings ให้เรียก `timeService.Update` แทนเซฟ DB ตรง เพื่อให้ apply ลง OS ด้วย + normalize timezone format เก่าจาก backup รุ่นเก่า |
| `backend/internal/api/router.go` | เพิ่ม `authRoute("POST /api/system/time/manual", s.HandleSetManualTime)` (กลุ่มเดียวกับ `/api/system/time` บรรทัด ~103) |
| `backend/cmd/pigate/main.go` | (1) เลือก real/mock `TimeManager` ตาม flag `-mock` (2) สร้าง `TimeService` ส่งเข้า `NewServer` (3) เรียก `timeService.InitApplyConfig()` ช่วงต้นของ startup sequence (ก่อน interface/DNS — เวลาที่ถูกต้องช่วยให้ log และ TLS validation ของขั้นตอนถัดไปเพี้ยนน้อยที่สุด) |
| `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` | อัปเดต schema `SystemTimeSettings` (เพิ่ม `status`), เพิ่ม path `/system/time/manual` — แก้ทั้ง 2 ไฟล์ให้ตรงกัน |

### Phase 5 — install.sh

| จุด | สิ่งที่ทำ |
|---|---|
| STEP 3 (polkit) | เพิ่ม `addRule` บล็อกใหม่สำหรับ `org.freedesktop.timedate1.set-timezone` / `.set-ntp` / `.set-time` เมื่อ `subject.user == "pigate"` (วางถัดจากบล็อก hostname1 ที่มีอยู่แล้ว บรรทัด ~230) **และ** เพิ่ม `unit === "systemd-timesyncd.service"` ใน allowlist ของบล็อก `systemd1.manage-units` เดิม |
| STEP ใหม่ | `mkdir -p /etc/systemd/timesyncd.conf.d` + สร้างไฟล์ `50-pigate.conf` เปล่า `chown pigate:netdev` + `chmod 0644` + ตรวจว่ามี `systemd-timesyncd` ติดตั้งอยู่ (ถ้าไม่มีให้ log เตือน `apt-get install systemd-timesyncd`) |

### Phase 6 — Frontend

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `frontend/src/services/systemService.ts` | (1) ขยาย interface `SystemTimeSettings` เพิ่ม `status?: { currentTime: string; ntpSynchronized: boolean }` (2) เพิ่ม `setManualTime(datetime: string)` + mock (3) mock ของ `getTimeSettings` คืน timezone format ใหม่ (IANA ล้วน) |
| `frontend/src/pages/SettingsMaintenance.tsx` | (1) เปลี่ยน native `<select>` timezone (บรรทัด ~620) เป็น shadcn `Select` หรือ Combobox ค้นหาได้ populate จาก `Intl.supportedValuesOf("timeZone")` + แสดง GMT offset ต่อท้าย (คำนวณตอน render — ค่าที่ *เก็บ* ต้องเป็น IANA ล้วน) (2) แสดงสถานะ live: เวลาปัจจุบันของเครื่อง + badge "Synchronized/Not synced" จาก `status` (3) เมื่อปิด NTP → แสดงช่องตั้งวันที่/เวลาด้วยมือ (`<Input type="datetime-local">`) + ปุ่ม "Set Time" เรียก `setManualTime` (4) ลบกล่องเหลือง "Backend Integration" ส่วนคำสั่ง `timedatectl` (บรรทัด ~686-691) — เป็น mock artifact ที่ไม่จริงแล้ว (5) ใช้ semantic color variables + shadcn primitives เท่านั้น ตาม `rules_of_work.md` |
| ค่าเก่าใน localStorage (mock mode) | mock ของ `getTimeSettings` ควร normalize ค่า timezone เก่าที่ติด " (GMT..." ก่อนคืน |

### Phase 7 — ทดสอบ

1. `cd backend && go build ./... && go test ./...`
2. mock mode: `./pigate-backend -mock=true` → GET/PUT/manual ผ่าน UI, ค่า persist,
   migration รันซ้ำได้
3. `cd frontend && yarn build && yarn lint`
4. เครื่องจริง (รัน install.sh ซ้ำก่อน): (a) เปลี่ยน timezone แล้ว `timedatectl status`
   ต้องเห็นโซนใหม่ (b) ปิด NTP → ตั้งเวลาด้วยมือ → เวลาเปลี่ยนจริง (c) เปิด NTP +
   ตั้ง server เอง → ดู `/etc/systemd/timesyncd.conf.d/50-pigate.conf` + timesyncd
   restart + สถานะ synchronized กลับมา (d) reboot → InitApplyConfig apply โซน/NTP
   ถูกต้อง และ**เวลาไม่โดนย้อน** (e) ลอง import backup รุ่นเก่า (timezone format เก่า)

---

## 4. ข้อควรระวัง (Cautions)

1. **ห้าม exec เด็ดขาด** — ใช้ godbus กับ `org.freedesktop.timedate1` เท่านั้น
   ห้าม `timedatectl`/`date`/`hwclock` (กล่องเหลืองใน UI ปัจจุบันที่โชว์คำสั่งพวกนี้
   คือสิ่งที่ต้องลบทิ้ง ไม่ใช่สิ่งที่ต้องทำตาม)

2. **ห้าม persist เวลาแล้ว apply ตอนบูต** — `InitApplyConfig` ต้อง apply เฉพาะ
   timezone/NTP config **ห้ามเรียก `SetTime`** มิฉะนั้นทุกครั้งที่บูต เวลาเครื่องจะ
   โดนย้อนกลับไปค่าเก่าใน DB — **นี่คือ bug ร้ายแรงที่สุดที่ต้องกันไว้**

3. **การย้อน/กระโดดเวลามีผลข้างเคียงกว้าง** — session/token หมดอายุผิดเวลา,
   TLS validation พัง (เวลาย้อนก่อน cert ออก), DHCP lease timer และ log timestamp
   ใน ring buffer เพี้ยน — UI ต้องมีคำเตือนก่อนตั้งเวลาด้วยมือ และแนะนำให้ใช้ NTP
   เป็นหลัก

4. **`SetTime` จะถูก timedated ปฏิเสธถ้า NTP ยังเปิดอยู่** — ต้อง guard 2 ชั้น:
   service ปฏิเสธเมื่อ config ntpSync=true และ UI ซ่อนช่องตั้งเวลาเมื่อ toggle เปิด
   (อย่าพึ่ง error จาก D-Bus เพราะข้อความอ่านไม่รู้เรื่อง)

5. **กัน injection ลงไฟล์ drop-in** — `NTP=<user input>` เป็น user input บรรทัดเดียว
   ที่จะถูก root process (timesyncd) อ่าน ต้อง validate เข้มก่อนเขียนเสมอ:
   ห้าม newline, `[`, `]`, `=`, `#` — มิฉะนั้นผู้ใช้ (หรือ attacker ที่ยึด session ได้)
   ฉีด directive อื่นของ timesyncd ได้

6. **polkit เป็น 2 ส่วนแยกกัน** — timedate1 actions เป็นคนละ action id กับ
   `systemd1.manage-units` และการ restart timesyncd ต้องเพิ่ม unit ใน allowlist
   เดิมอีกจุด — ลืมจุดใดจุดหนึ่งจะเจอ `Access denied` เฉพาะบาง operation
   ทำให้ debug งง และ**เครื่องที่ติดตั้งไปแล้วต้องรัน install.sh ซ้ำ** (upgrade path
   เดียวกับฟีเจอร์ hostname — ควรรวมไว้ใน release note เดียวกัน)

7. **Migration ค่า timezone เก่า** — DB ที่ seed ไปแล้วเก็บ
   `"Asia/Bangkok (GMT+7:00)"` ซึ่ง `time.LoadLocation` และ timedated จะ reject
   ต้อง migrate ทั้ง (a) DB จริง (b) mock localStorage ฝั่ง frontend
   (c) ค่าใน backup file เก่าตอน import — ตัด suffix ` (` เป็นต้นไปแล้ว validate
   ถ้า validate ไม่ผ่านให้ fallback `Asia/Bangkok` พร้อม log

8. **การ validate timezone ฝั่ง Go พึ่ง tzdata ของระบบ** — บน Pi มี tzdata อยู่แล้ว
   แต่บนเครื่อง dev/container อาจไม่มี → พิจารณา `import _ "time/tzdata"` ใน
   `main.go` (embed ~450KB) เพื่อให้ validation เสถียรทุกสภาพแวดล้อม
   และรายการโซนที่ browser (`Intl`) เห็น อาจต่างเวอร์ชันกับ tzdata บนเครื่องเล็กน้อย
   — backend validate เป็นด่านสุดท้ายเสมอ

9. **สมมติว่าใช้ systemd-timesyncd เท่านั้น** — ถ้าเครื่องผู้ใช้ติดตั้ง chrony/ntpd
   ไว้ `SetNTP` ของ timedated จะไปคุมบริการนั้นแทน และ drop-in ของเราจะไม่มีผล
   → install.sh ควรตรวจและเตือน + ระบุใน README ว่ารองรับเฉพาะ timesyncd

10. **Raspberry Pi รุ่นเก่าไม่มี RTC battery** — Pi 5 มี RTC (ต่อถ่านแยก) แต่ถ้าไม่ต่อ
    หรือใช้บอร์ดอื่น เวลาที่ตั้งด้วยมือจะหายเมื่อถอดปลั๊ก (fake-hwclock ช่วยได้แค่
    ประมาณ) — ใส่คำอธิบายใน UI ว่าถ้าปิด NTP เวลาอาจเพี้ยนหลังไฟดับ

11. **แก้ interface ของ kernel ต้องแก้ mock ให้ครบ** — `TimeManager` ต้องมีทั้ง
    `real_timedate.go` และใน `mock.go` ไม่งั้น compile ไม่ผ่าน (main.go เลือกตาม
    flag `-mock`)

12. **โหมด `-disable-edit`** — PUT `/api/system/time` และ POST `/manual` ต้องโดน
    read-only middleware ครอบเหมือน endpoint อื่น (ใช้ `authRoute` เดิมครอบให้แล้ว
    แต่ยืนยันตอนเทสต์)

13. **อย่ารื้อกล่องเหลืองทั้งกล่องโดยไม่ดู** — กล่อง "Backend Integration" ใน
    SettingsMaintenance มีทั้งส่วนคำสั่งของ password และของ time ปนกัน
    ลบเฉพาะส่วน time (หรือถ้าจะลบทั้งกล่องให้ตัดสินใจแยกเพราะกระทบ scope password)

---

## 5. ลำดับการลงมือทำ (แนะนำ)

```
1. Phase 1  model + seed fix + data migration     → go build ผ่าน
2. Phase 2  kernel (TimeManager real + mock)      → go build ผ่าน
3. Phase 3  TimeService + validation + เทสต์      → go test ผ่าน
4. Phase 4  handlers + router + main.go + openapi (2 ไฟล์)
5. Phase 5  install.sh (polkit 2 จุด + drop-in dir)
6. Phase 6  frontend (systemService → Time card → ลบ mock artifact)
7. Phase 7  ทดสอบ mock mode → เครื่องจริง (รัน install.sh ซ้ำก่อน)
```
