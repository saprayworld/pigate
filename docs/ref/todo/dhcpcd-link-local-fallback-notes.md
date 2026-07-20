# dhcpcd Link-Local (169.254.x.x) Fallback Detection — หมายเหตุเบื้องต้น (issue #78)

> **สถานะ: หมายเหตุเบื้องต้น รอวางแผนเต็ม — ไม่ใช่แผนพร้อมลงมือทำ**
> จุดประสงค์ของไฟล์นี้คือเก็บผลสำรวจโค้ด + คำตัดสินใจของเจ้าของโปรเจกต์ไว้
> ไม่ให้บริบทหาย ก่อนจะเขียน work-plan เต็ม (Phase B ต้องสำรวจซ้ำ ณ วันเขียนแผนจริง
> เพราะงาน issue #76 ที่จะทำก่อนหน้าอาจแตะไฟล์ย่านเดียวกัน)
>
> เขียนเมื่อ: 2026-07-20 (อัปเดตเพิ่ม §6 ผลวิเคราะห์ log AP outage วันเดียวกัน)
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
