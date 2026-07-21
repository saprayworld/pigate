# dhcpcd Link-Local (169.254.x.x) Fallback Detection — หมายเหตุเบื้องต้น (issue #78)

> **สถานะ: หมายเหตุเบื้องต้น — มี work-plan เต็มแล้วที่ `dhcpcd-link-local-fallback-plan.md`**
> จุดประสงค์ของไฟล์นี้คือเก็บผลสำรวจโค้ด + คำตัดสินใจของเจ้าของโปรเจกต์ + หลักฐาน log จริงไว้
> ไม่ให้บริบทหาย — ไฟล์แผนเต็มอ้างอิงกลับมาที่นี่สำหรับที่มาของ decision/evidence
>
> เขียนเมื่อ: 2026-07-20 (อัปเดตเพิ่ม §6 ผลวิเคราะห์ log AP outage วันเดียวกัน,
> §7 ผลวิเคราะห์ log จริงเพิ่มเติม 2026-07-21 ระหว่างทดสอบ PR #79 — เจอเคส "ค้างที่
> 169.254 จริง" ที่ยังไม่เคยมีหลักฐานตรงมาก่อนหน้านี้)
> อ้างอิง: GitHub issue #78

## 1. สรุปปัญหา

บางครั้ง dhcpcd ขอ IP จาก DHCP server ไม่สำเร็จ และ interface ได้ APIPA/link-local
`169.254.x.x/16` มาแทน (log ใน issue เห็น pattern retry ซ้ำทุก ~4 นาทีแต่ไม่หาย)
ระบบปัจจุบัน**ไม่มีกลไกตรวจจับ/แก้ไข**สถานการณ์นี้เลย — interface ค้างจนกว่าผู้ใช้แก้เอง

## 2. คำตัดสินใจของเจ้าของโปรเจกต์ (2026-07-20 — ผูกพันตอนวางแผนจริง)

1. **เกณฑ์เวลา/จำนวนรอบ** ("เช็คทุก ~1 นาที", "รอนับ 2-3 รอบก่อนลงมือ") ต้องเป็น
   **ค่าที่ user ปรับได้จริงผ่านระบบ config ของโปรเจกต์** — ไม่ใช่แค่ const ในโค้ดแบบ
   `stopSettleDelay` ตอนวางแผนจริงต้องเลือกกลไก (ดู §4) แล้วระบุเหตุผล
2. **ขยาย scope จาก issue เดิม:** interface DHCP mode ที่**ไม่มี IPv4 เลย**ค้างนาน
   ก็เข้าข่าย "dhcpcd ขอ IP ไม่ได้" เหมือนกัน ต้องถูกตรวจ/แก้ด้วย
3. **ต้องมี backoff/เพดานการ restart** — ถ้า restart แล้วยังได้ 169.254 ซ้ำ
   ห้ามวน restart ทุกไม่กี่นาทีตลอดไป และเหตุการณ์ควรลง event log ให้ผู้ใช้เห็น

ข้อยกเว้นสำคัญ (ยืนยันหนักแน่นขึ้นด้วยผลทดสอบจริงใน §6):
- **Interface ที่ carrier ยังไม่พร้อม (`!(isUp && isRunning)`) ต้องถูก skip
  โดยสมบูรณ์** — ไม่นับ strike, ไม่ restart, ไม่ลบ address ใด ๆ (Wi-Fi ที่ยัง
  ไม่ associate / สาย LAN หลุด อยู่ในกลุ่มนี้ทั้งหมด) ดูเหตุผลเชิงพฤติกรรมจริงใน §6

พฤติกรรมแก้ไขตาม issue เดิม (ยังยืนตาม):
- มี IPv4 อื่นอยู่ร่วมบน interface → **ลบเฉพาะ 169.254 ทิ้ง**
- มีแค่ 169.254 เดี่ยว ๆ (หรือไม่มี IP เลย ตาม decision 2) → **restart dhcpcd
  ของ interface นั้น** (ขอ lease ใหม่ — ไม่แตะ config ผู้ใช้)

## 3. ผลสำรวจโค้ด ณ 2026-07-20 (ต้อง re-verify ตอนวางแผนจริง)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Address event จาก kernel | มีแต่ log + publish `AddrRouteChanged` — ไม่เก็บว่า addr ไหนคือ 169.254 / เห็นเมื่อไร | `service/netlink_monitor.go:112-119` |
| `NetEvent` struct | ไม่มี field address เลย (มีแค่ Kind/Name/Up/Running) | `service/event_bus.go:51-56` |
| Periodic health poller | **ไม่มี** — ticker ที่มีเป็น telemetry/log-flush/sweeper ทั้งหมด | `system_status.go:140,176,189`, `event_log.go:84`, `api/session.go:227`, `api/middleware.go:296` |
| Restart dhcpcd ราย interface | ✅ มีครบ real+mock ใช้ซ้ำได้เลย | `kernel/interfaces.go:119-121` `RestartDhcpcd`, `kernel/dhcpcd.go:73`, `kernel/mock.go:390` |
| ลบ address เดี่ยวจาก interface | ❌ ไม่มี method บน `NetworkManager` — `netlink.AddrDel` ใช้ภายใน `ConfigureInterface` เท่านั้น | `kernel/interfaces.go:35-51`, `kernel/real_network.go:222` |
| อ่าน address ปัจจุบันของ interface | ยังไม่สำรวจละเอียด (read path ของ interfaces น่าจะมี AddrList อยู่แล้ว — เช็คตอนวางแผน) | `kernel/real_network.go` |
| Concurrency pattern ต้นแบบ | mutex + pendingStops + settle timer จากงาน issue #75 | `service/dhcpcd.go:20-49`, Caution 3 ใน comment `:74-79,232-236` |
| Bus pause ตอน backup import | ตัวตรวจใหม่ต้องเคารพ (ห้าม restart ระหว่าง import) | `service/event_bus.go:184-193`, `netlink_monitor.go:214-221` |
| wpa_supplicant scan cadence | `autoscan=periodic:10` (สแกนทุก 10s ระหว่างรอ reconnect) | `kernel/wpa.go:68-69` |
| Routing reconcile ต่อ `LinkChanged` | full `ApplyRoutes` + `enforceInterfaceMetrics` ทุกครั้ง (debounced 500ms) | `cmd/pigate/main.go:213-220`, `service/routing.go:412-435` |

## 4. กลไก config สำหรับเกณฑ์ที่ปรับได้ — candidates (ยังไม่ตัดสิน)

- **(a) `pigate.conf` / `-config` flag** (`internal/config` — Parse/Resolve/Write,
  key ตรงกับชื่อ flag): ระดับ operator แก้ไฟล์เอง ไม่มี UI, ไม่ติดไป backup,
  เพิ่ม key ง่าย แต่ปัจจุบัน struct `Config` เป็น bootstrap flags ล้วน
  (`config/config.go:38-49`) — เกณฑ์ self-heal อาจไม่เข้าพวก
- **(b) ตารางตั้งค่าใน DB + UI** ตาม pattern single-row settings ที่มีอยู่
  (`system_time_settings`, `system_hostname_settings`, `dns_server_settings` —
  `db/connection.go:357-376`): user ปรับจาก UI ได้จริง, ติดไป backup/restore
  (schema v2), แต่งานใหญ่กว่า (migration + handlers + openapi ทั้งสองไฟล์ + หน้า UI)
- ความหมายของ "configurable value ผ่านระบบ config ที่มีอยู่" ของเจ้าของโน้มไปทาง
  user ปรับได้ → (b) น่าจะตรง intent กว่า แต่ให้ยืนยัน scope UI กับเจ้าของตอนวางแผนจริง
  (เช่น อาจเริ่ม (b) แบบ backend+API ก่อน แล้ว UI เป็น phase ถัดไป)

## 5. ประเด็นออกแบบที่ต้องเก็บตอนวางแผนเต็ม

- **Eligibility gate ต้องมาก่อนการตรวจ IP เสมอ:** ทุก tick ต้องอ่าน live link flags
  ก่อน — interface เข้าเกณฑ์ตรวจก็ต่อเมื่อ `isUp && isRunning` (และเป็น DHCP mode
  ใน DB ณ ตอนนั้น) เท่านั้น; ไม่เข้าเกณฑ์ = **skip โดยสมบูรณ์ + reset strike counter**
  ของ interface นั้น (ไม่ใช่ pause การนับ) — เหตุผลดู §6.2/§6.3
- **State machine ต่อ interface:** นับรอบที่ "เข้าเกณฑ์ ∧ สภาพผิดปกติ" (มี 169.254
  หรือไม่มี IPv4) ติดต่อกันครบ N ก่อนลงมือ — กัน false positive ทั้งช่วง 169.254
  โผล่ชั่วคราวระหว่างรอ DHCP จริง และช่วงหลัง reconnect ที่เพิ่งได้ RUNNING แต่ lease
  ยังไม่มา (พิจารณา guard เสริม "เวลาตั้งแต่เข้าสถานะ running" ด้วย)
- **Trigger แบบไหน:** ต้องเป็น periodic ticker เป็นแกนหลัก — ผลทดสอบ §6 ยืนยันว่า
  เคส "ไม่มี IP เลย" **ไม่มี Addr event ใด ๆ ให้เกาะ** (event-driven ล้วนตรวจเคสนี้
  ไม่ได้); Addr event ใช้ได้อย่างมากเป็นตัวเสริมสำหรับเคส 169.254 โผล่
- **จุดเสี่ยง concurrency ระดับเดียวกับงาน #75:** การ restart จากตัวตรวจต้องแชร์
  critical section กับ `DhcpcdService.mu`/`pendingStops` (ห้าม race กับ deferred stop /
  event-path start), เช็ค DB ทุก tick ว่า interface ยังเป็น DHCP mode, เคารพ bus Pause
- **Kernel capability ใหม่ต้องครบ real+mock** (กติกา CLAUDE.md): เช่น
  `DeleteAddress(iface, cidr)` หรือ method เฉพาะทางลบ link-local
- **Backoff:** เพดาน/ช่วงถอยหลังเมื่อ restart ซ้ำไม่สำเร็จ + ลง event log ทุกครั้งที่ลงมือ
- **ลำดับงาน:** ทำหลัง issue #76 (แผน `usb-wifi-startup-race-plan.md`) — โค้ดย่านเดียวกัน
  และแก้ #76 ก่อนช่วยให้ทดสอบ #78 บนบอร์ดจริงสะอาดขึ้น

## 6. ผลวิเคราะห์ log ทดสอบจริง: ปิด AP ค้าง ~5-6 นาที (2026-07-20, `wlx4086cbb56030`)

เจ้าของทดสอบปิด AP ทิ้งไว้ (12:22:03-12:27:29) บนบอร์ดจริงหลังแก้ issue #75 แล้ว —
ข้อมูลนี้มีผลตรงต่อการออกแบบ #78:

### 6.1 พฤติกรรมที่เห็น + คำอธิบายจากโค้ด
- **Full down→up cycle ทุก ~65 วินาที** (`up=false running=false` แล้วกลับ
  `up=true running=false` ภายใน ~1-2s): เป็น retry cadence ของ wpa_supplicant/driver
  เอง — pigate ไม่ได้สั่ง (`ToggleInterface` ไม่ถูกเรียกใน path นี้เลย) ตลอดช่วง
  ไม่เคยมี `running=true` (associate ไม่ได้จริงเพราะ AP ปิด)
- **Event "duplicate, suppressed" ทุก ~11 วินาที** สอดคล้องกับ `autoscan=periodic:10`
  ที่ pigate เขียนลง wpa config (`kernel/wpa.go:69`) — RTM_NEWLINK ซ้ำจากรอบสแกน
  ถูก dedupe ของ monitor กลืนหมด ไม่เกิด action ใด ๆ (กลไก dedupe ทำงานถูกต้อง)
- **ตลอด 5-6 นาที ไม่มี Address event แม้แต่ครั้งเดียว** — interface ไม่เคยได้ IP
  ตั้งแต่ต้น (คนละอาการกับเคส 169.254 ค้างใน issue เดิม แต่เข้า decision 2 ใน §2)

### 6.2 ยืนยัน: กลไก deferred-stop จาก #75 ทำงานถูกต้องภายใต้ AP outage ยาว
ทุก cycle: down → `scheduleOrResetStopLocked` (settle 2s, log "Deferring stop") →
up กลับมาภายใน window → `cancelPendingStopLocked` + เข้า branch
"Wi-Fi UP but not running (waiting for connection)" (`dhcpcd.go:151-171`) —
**ไม่มี `StopDhcpcd`/`StartDhcpcd` เกิดขึ้นเลยตลอดการทดสอบ** ตรงตาม design

### 6.3 นัยต่อ #78 — เน้นย้ำ eligibility gate
- เคสนี้คือ "Wi-Fi up-but-not-running ค้างระดับนาที+ โดยมี down/up blip แทรกทุก ~65s"
  — health-checker **ต้อง skip interface สถานะนี้โดยสมบูรณ์** (ไม่ใช่แค่ข้ามชั่วคราว
  แล้วนับ strike ต่อ): ไม่มี carrier → restart dhcpcd ไร้ประโยชน์แน่นอน และสถานะ
  dhcpcd process ระหว่าง outage **ไม่แน่นอน** — ถ้า blip สั้นกว่า settle window ตลอด
  (เคสในการทดสอบนี้) dhcpcd ตัวเดิมยังรันรอ carrier อยู่ แต่ถ้ามี down ค้าง >2s
  ครั้งใดครั้งหนึ่ง dhcpcd จะถูก stop ไปแล้ว (branch up-not-running ไม่ start กลับ,
  `dhcpcd.go:164-166`) → checker ห้าม assume ว่ามี process ให้ restart เสมอ
- **จังหวะ tick ~60s อาจ alias กับ cycle ~65s:** บาง tick จะไปตกช่วง down-blip พอดี
  → gate ที่อ่าน live flags ณ ตอน tick จัดการเคสนี้ให้เอง (อ่านได้ down/not-running
  → skip) ข้อสำคัญคือห้ามออกแบบให้ "ไม่มี IP" อย่างเดียวเป็นเงื่อนไขนับ strike
  โดยไม่ดู flags — ไม่งั้นช่วง reconnect ทั้งหมดจะถูกนับผิดจน false-trigger
- หลัง AP กลับมา: จะมีหน้าต่าง "up && running แต่ lease ยังไม่มา" — กติกานับ N รอบ
  ติดต่อกัน (+guard เวลาตั้งแต่ running) ต้องครอบเคสนี้ไม่ให้ insta-restart

### 6.4 ข้อสังเกตแยก (นอก scope #78 — บันทึกไว้เฉย ๆ)
- **Routing reconcile รันเต็มทุก ~65s ระหว่าง outage:** `LinkChanged` แต่ละ cycle
  ปลุก subscriber "routing" (debounced) → `ReconcileKernelRoutingTable` →
  `ApplyRoutes` + `enforceInterfaceMetrics` เต็มชุด (`routing.go:412-435`) ทั้งที่
  ไม่มีอะไรเปลี่ยนจริง — idempotent จึงไม่พัง แต่เป็น noise/งาน netlink ฟรีเป็นระยะ
  ระหว่าง Wi-Fi outage ยาว ๆ; ถ้าจะ optimize (เช่น short-circuit เมื่อ state ไม่ต่าง)
  ควรเป็น issue แยก ไม่ผูกกับ #78

## 7. ผลวิเคราะห์ log จริงเพิ่มเติม: ค้างที่ 169.254 จริง (2026-07-21, `log-pigate-003.log`)

ระหว่างทดสอบ real-hardware ของ PR #79 (แก้ issue #76 + udev-rename race) บน
`wlx0cef1548ff2b` เจอเคส **"ค้างที่ 169.254 จริง"** ที่ต่างจาก §6 (ซึ่งเป็นเคส "ไม่มี IP
เลยเพราะ AP ปิด") — นี่คือหลักฐานตรงเคสแรกของปัญหา #78 เอง ไม่ใช่แค่การอนุมานจาก issue เดิม

### 7.1 ลำดับเหตุการณ์ (สองรอบ pigate restart ติดกัน, SSID หลัก `ppook02` เป็น Open network)
- รอบแรก (pid 3875): associate สำเร็จ 19:28:39 → ได้ `169.254.129.156/16` ที่ 19:28:49 →
  pigate ถูก stop ที่ 19:29:51 **ขณะยังค้างอยู่ที่ 169.254** (~62 วินาทีในหน้าต่างที่สังเกตได้)
- รอบสอง (pid 4503, restart ใหม่): associate สำเร็จ 19:30:54 (SSID เดิม `ppook02`) →
  ได้ `169.254.35.202/16` ที่ 19:31:05 → **ที่อยู่เดิมถูก kernel re-assert ซ้ำทุก ~11-14
  วินาที** (19:31:39, 19:31:50, 19:32:04, 19:32:16) โดยไม่เคยขยับไปเป็น DHCP lease จริงเลย
  ตลอด ~89 วินาที จนกระทั่งมีคนสั่ง `ToggleInterface up=false` แล้ว `up=true` ด้วยมือที่
  19:32:34/37 (ไม่มี log `[Self-heal]` นำหน้า — เป็นการสั่งจากภายนอก ไม่ใช่กลไกอัตโนมัติใด ๆ
  ที่มีอยู่ในระบบปัจจุบัน) หลัง toggle ใหม่ wpa_supplicant ไป associate SSID สำรอง `5.0G`
  (WPA2) แทน แล้วได้ IP จริง `192.168.1.171/24` ที่ 19:32:55

### 7.2 นัยสำคัญต่อการออกแบบ #78

1. **ยืนยันคาดการณ์ใน notes/§2.2 ว่า address 169.254 ไม่หายเองแม้ reconnect เต็มรูปแบบ**
   — การ restart ทั้ง pigate service (ซึ่งวิ่ง self-heal → `ConfigureWifi` →
   `ToggleInterface` ครบชุด ไม่ใช่แค่ restart dhcpcd เฉย ๆ) เกิดขึ้นสองรอบติดกันบน SSID
   เดิม (`ppook02`) และได้ 169.254 ซ้ำทั้งสองรอบ — เคสนี้ปัญหาน่าจะอยู่ฝั่ง AP/DHCP-server
   ของ SSID นั้นเอง ไม่ใช่ที่ dhcpcd/wpa_supplicant client
2. **ข้อสังเกตใหม่ที่ plan ยังไม่ได้ตอบ:** สิ่งที่ทำให้หลุดจากอาการค้างจริง ๆ ในการทดสอบนี้
   คือการ toggle interface (stop/start wpa_supplicant ทั้งชุด) ที่บังเอิญทำให้
   wpa_supplicant เลือก associate SSID **สำรอง** (`5.0G`) แทนตัวหลัก — ซึ่ง "restart
   dhcpcd" ตามที่ออกแบบไว้ใน `dhcpcd-link-local-fallback-plan.md` §2.2/T-08
   (`DhcpcdService.RestartForHealthCheck`) เบากว่านี้มาก (ขอ lease ใหม่เฉย ๆ ไม่แตะ
   wpa_supplicant/การเลือก SSID เลย) — มีความเป็นไปได้ที่ "restart dhcpcd" อย่างเดียวจะ
   **ไม่พอ**สำหรับเคสที่ปัญหาผูกกับ SSID เฉพาะตัวแบบนี้ เพราะ Wi-Fi จะยังคง associate
   SSID เดิมที่มีปัญหาอยู่ดี แล้วขอ lease ใหม่ก็มีโอกาสได้ 169.254 ซ้ำอีก — **นี่ไม่ใช่
   เหตุผลให้ขยาย scope ไปแตะ Wi-Fi failover/SSID-switching logic** (ยังคงอยู่นอก scope
   ตาม §0 ของแผนเต็มเหมือนเดิม) แต่เป็นเหตุผลว่าทำไม backoff+เพดาน+event log (decision 3,
   Caution ใน plan) ถึงสำคัญมาก — เพราะในเคสแบบนี้ checker จะ "ลงมือแล้วไม่หาย" ซ้ำ ๆ
   จนชนเพดาน แล้วต้องพึ่งคนมาแก้เอง (ตรงกับที่เกิดขึ้นจริงในการทดสอบนี้) ไม่ใช่รับประกันว่า
   จะแก้ได้ทุกกรณี — เอกสาร/log event ตอนชนเพดานจึงต้องสื่อสารให้ผู้ใช้เข้าใจว่าอาจต้องเช็ค
   ฝั่ง AP/SSID เอง ไม่ใช่แค่ "restart แล้วจบ"
3. **Retry cadence ที่วัดได้จริงระหว่างค้าง (~11-14s)** ต่างจาก cadence ของ AP-outage
   ใน §6 (~65s) — เป็นคนละปรากฏการณ์กัน (อันนี้คือ kernel/dhcpcd ยืนยันที่อยู่เดิมซ้ำ
   ระหว่างที่ carrier ยังพร้อมและ associate อยู่แล้ว ไม่ใช่ down/up cycle) แต่ที่อยู่ยังคง
   "ค้างนิ่ง" ระหว่าง cadence นี้ (ไม่ toggle up/down) จึงไม่เข้าเงื่อนไข eligibility-gate
   aliasing แบบ §6.3 — ยืนยันว่า design เดิม (`isUp && isRunning` ต้องผ่านก่อนเสมอ, อ่าน
   address list สดทุก tick) จับเคสนี้ได้ตรงไปตรงมา ไม่ต้อง guard เพิ่ม
4. **ยังไม่มีหลักฐานจริงของตัว checker เอง** (ยังไม่ implement) — หน้าต่างที่สังเกตได้
   (~89 วินาทีค้างก่อนมีคนแก้ด้วยมือ) สั้นกว่า threshold default `3 strikes × 60s +
   30s min-running` (~210s) เล็กน้อย จึงยังไม่มีข้อมูลจริงว่า checker ตามค่า default จะ
   ทันจับหรือไม่ในเคสนี้พอดี — เป็นเหตุผลเสริมให้ตรวจสอบตัวเลข default อีกครั้งก่อน merge
   ตามที่ Caution 7 ของ plan ระบุไว้แล้ว
5. **RAM-only state ของ checker (T-09) จะ reset ทุกครั้งที่ pigate restart** — ระหว่าง
   การทดสอบนี้ pigate ถูก restart เอง 3 ครั้งภายใน ~10 นาที (เพื่อทดสอบ #76/#79) ถ้า
   checker ทำงานอยู่จริง `restartsSinceRecover`/เพดานจะ reset ทุกรอบ restart ไปด้วย —
   ในสภาพใช้งานจริง pigate ไม่ควร restart ถี่ขนาดนี้ตามปกติ จึงไม่ใช่ความเสี่ยงเชิง
   production แต่ควรบันทึกไว้เป็นข้อจำกัดที่ทราบ (known limitation) ไม่ใช่ต้องแก้ตอนนี้
