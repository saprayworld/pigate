# Traffic Log Pagination + หน้า Local Traffic — cursor-based infinite scroll และ log ของ chain input/output

> เอกสารแผนงานสำหรับ 2 เรื่องที่ต้องทำรวมเป็นแผนเดียว (แชร์ดีไซน์/โค้ดชุดเดียวกัน):
>
> 1. **Cursor-based infinite scroll** สำหรับหน้า Forward Traffic เดิม (ตอนนี้ดึงทีเดียว 200 แถว
>    จบ ไม่มี pagination) → โหลดหน้าแรก 500 แถว แล้วเลื่อนถึงล่างสุดค่อยขอ "แถวที่เก่ากว่า cursor"
>    ต่อไปเรื่อยๆ พร้อมขยาย ring buffer ให้ใหญ่ขึ้นมาก
> 2. **หน้าใหม่ "Local Traffic"** แสดง log ของ chain `input` + `output` รวมกันในหน้าเดียว
>    (มีฟิลเตอร์ input only / output only / ทั้งคู่) ใช้กลไก pagination/SSE ชุดเดียวกับข้อ 1
>    → ต้องย้าย log ของ input/output จาก printk (journald → SD card) ไปเข้า **NFLOG** ก่อน
>
> วันที่เขียน: 2026-07-28 · Branch อ้างอิง: `main` (หลัง PR #99 merge) ·
> งานจริงทำบน `feat/traffic-log-pagination-local-traffic`
> README Feature Status: Firewall/Log = Completed → ยังคง Completed (ขยายขอบเขตของเดิม)
>
> **แผนนี้เป็น "แผนแยก" ที่ `docs/ref/todo/input-output-chain-firewall-plan.md` §0 (นอกขอบเขต)
> และ §5 ข้อ 6 ระบุไว้ว่าให้ทำทีหลัง** — เนื้อหาส่วนที่แผนเดิมห้ามไว้ (ห้ามส่ง log input/output
> เข้า NFLOG group 100 เพราะ parser ฮาร์ดโค้ด reason เป็น forward) ถูกปลดล็อกในแผนนี้ด้วยการ
> แก้ parser + เพิ่มฟิลด์ `Chain` ตามที่แผนเดิมสั่งไว้ ไม่ใช่การขัดคำสั่งเดิม (ดู §6 ข้อ 1)

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย (สิ่งที่ผู้ใช้เห็น):**

- หน้า **Forward Traffic** (`/logs/traffic`) โหลด 500 แถวแรก, เลื่อนลงถึงล่างสุดแล้วโหลด
  500 แถวถัดไปอัตโนมัติ (infinite scroll) จนหมด buffer แล้วขึ้นข้อความ "หมดแล้ว";
  ของใหม่ยัง prepend ด้านบนแบบ real-time ผ่าน SSE เหมือนเดิม
- เมนู "Log & Report" มีรายการใหม่ **Local Traffic** (`/logs/local`) แสดง log ของทราฟฟิก
  ที่ *ปลายทางเป็นตัวบอร์ดเอง* (chain `input`) และที่ *บอร์ดส่งออกเอง* (chain `output`)
  รวมกันในตารางเดียว มี dropdown เลือก `All local / Local-In only / Local-Out only`
  พร้อม infinite scroll + real-time แบบเดียวกันเป๊ะ (โค้ดใช้ร่วมกัน ไม่ copy-paste)
- ตารางมีคอลัมน์ **Chain** เพิ่ม (FWD / INP / OUT) เพื่ออ่านง่ายเมื่อหลาย chain ปนกัน
- ring buffer ใหญ่ขึ้นจาก 500 → **10,000 รายการ** (ดูการคำนวณ RAM ใน §2.2)

**เงื่อนไขทางเทคนิค:**

- log ของ input/output ต้อง **ไม่เขียน SD card อีกต่อไป** (ย้ายจาก printk → NFLOG,
  สอดคล้อง `docs/tech_stack_design.md` §8 มากกว่าเดิม)
- ห้ามแตะลำดับ/โครงสร้าง 4 ส่วนของ input chain และห้ามแตะ verdict ของกฎใดๆ
  (งานนี้แตะเฉพาะ *ปลายทางของ log* ไม่ใช่ *ผลการตัดสินแพ็กเก็ต*)
- ห้ามใส่ `limit` ในกฎที่มี verdict อยู่ด้วย (Caution 5 ของแผนเดิม — ยังบังคับใช้ ดู §6 ข้อ 2)
- API เดิม `GET /api/logs/traffic` ต้อง backward compatible (client ที่ไม่ส่ง `chain`/cursor
  ยังได้ผลลัพธ์เดิม = ทุก chain ใหม่สุดก่อน) — response ยังเป็น array เปล่าเหมือนเดิม
- pagination ต้องกรอง (action/q/chain) **ทั้งก้อนก่อน** แล้วค่อยตัด cursor/limit เสมอ

**นอกขอบเขต (จงใจตัดออก):**

- ไม่ persist log ลง SQLite ทุกกรณี (ขัด §8 ของ tech_stack_design) — ยังเป็น RAM-only
- ไม่ทำ virtualized table (react-window ฯลฯ) — ใช้เพดานจำนวนแถวฝั่ง client แทน (§2.7)
- ไม่ทำ export log เป็นไฟล์ / ไม่ทำ filter ตามช่วงเวลา / ไม่ทำ search แบบ regex
- ไม่ทำ flag/config สำหรับปรับ capacity ของ ring buffer (ใช้ค่าคงที่ในโค้ด; ถ้าอนาคต
  ต้องการปรับได้ค่อยเพิ่มเป็น key ใน `internal/config` แยกแผน)
- ไม่แก้บั๊ก IPv6 protocol-offset ของ matcher เดิม และไม่แตะ Admin Access hardcoded ports
  (ข้อ 7 ของแผนเดิม — ยังเป็น issue แยก)

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ 2026-07-28, หลัง PR #99)

| ส่วน | สถานะ | อ้างอิง (ยืนยันด้วย grep ชื่อฟังก์ชัน ไม่ใช่เลขบรรทัด) |
|---|---|---|
| `model.FirewallLog` | ❌ ไม่มีฟิลด์ `Chain` (10 ฟิลด์ string) | `model/types.go:379-391` |
| ring buffer capacity | ⚠️ 500 (คอมเมนต์บอกเองว่า "ไม่กี่ร้อย KB") | `cmd/pigate/main.go:97` `logs.NewRingBuffer(500)` |
| `RingBuffer` | ✅ ใช้ได้ตามเดิม (Add/GetAll newest-first/Clear/Subscribe) | `internal/logs/ringbuffer.go` |
| NFLOG group ของ forward | ✅ `ForwardNflogGroup = 100`, snaplen 64 | `kernel/real_traffic_log.go:22-34` |
| parser NFLOG | ❌ ฮาร์ดโค้ด reason `"Blocked/Allowed (forward)"`, ไม่รู้จัก chain | `kernel/real_traffic_log.go` → `parseNflogAttr` |
| interface kernel | ⚠️ มีแค่ `WatchForwardTraffic` | `kernel/interfaces.go:23-32` (`TrafficLogManager`) |
| log ของ forward chain | ✅ ไป NFLOG 100 ผ่าน `forwardLogExpr` | `kernel/real_firewall.go` → `forwardLogExpr` |
| log ของ input chain | ❌ printk ทุกจุด (`expr.Log` + `NFTA_LOG_PREFIX` เปล่า) → journald → **SD card** | `real_firewall.go`: notLocal drop (มี limit), AUDIT, docker-compat accept, `addAdminAccessRules`, `addDNSServerAccessRules`, final drop |
| log ของ output chain | ❌ printk ผ่าน `addUserChainRules` prefix `"[PiGate] OUT ..."` | `real_firewall.go` → `addUserChainRules` |
| กฎ log ของ user rule ใน input/output | ⚠️ ถูก **แยกเป็น 2 กฎ** (log+limit / counter+verdict) เพราะ printk | `real_firewall.go` → `buildRuleExpressions` ท้ายฟังก์ชัน |
| เทสต์ที่ผูกกับพฤติกรรมนั้น | ⚠️ ต้องแก้ | `kernel/policy_chain_test.go` → `TestBuildRuleExpressions_LogSplitOnInputOutput` |
| `HandleGetTrafficLogs` | ⚠️ มี action/q/limit แต่ไม่มี cursor/chain; limit ถูก cap ด้วย `len(all)` | `api/handlers.go` → `HandleGetTrafficLogs` |
| `HandleGetRecentLogs` | ❌ คืน `GetAll()` ทั้งก้อนไม่มี limit → ถ้าขยาย buffer เป็น 10k จะส่ง JSON ~2.5MB ทุกครั้งที่เปิด Dashboard/reconnect | `api/handlers.go` → `HandleGetRecentLogs` |
| SSE stream | ✅ ใช้ได้ตามเดิม (`data:` = FirewallLog 1 ตัว, event `clear`) | `api/handlers.go` → `HandleLogStream` |
| timestamp | ⚠️ `time.RFC3339` = ความละเอียดระดับ **วินาที** → cursor แบบ time มีโอกาสชนกันเยอะ | `cmd/pigate/main.go` ในคอลแบ็กของ `WatchForwardTraffic` |
| `useLiveLogs` | ⚠️ snapshot **แทนที่ทั้งลิสต์** และ cap `MAX_LOGS = 500` | `frontend/src/hooks/useLiveLogs.ts` |
| หน้า Forward Traffic | ⚠️ `FETCH_LIMIT = 200`, render ทุกแถวในตารางเดียว | `frontend/src/pages/ForwardTraffic.tsx:41` |
| `trafficLogService` | ⚠️ query มีแค่ action/q/limit + mock feed | `frontend/src/services/trafficLogService.ts` |
| Dashboard Recent Logs | ⚠️ ใช้ ring buffer เดียวกัน → จะเห็น entry ของ input/output ด้วยหลังแผนนี้ | `frontend/src/pages/Dashboard.tsx:620-623` |
| openapi | ⚠️ ต้องแก้ 2 ไฟล์ (`docs/openapi.yaml`, `frontend/public/openapi.yaml`); `backend/internal/api/dist/openapi.yaml` เป็นผลลัพธ์ build **ห้ามแก้มือ** | `docs/openapi.yaml:496-538`, `:3366-3410` |

---

## 2. แนวทางเทคนิค

### 2.1 ring buffer เดียวรวมทุก chain (ไม่แยก buffer ต่อ chain)

**เลือก: buffer เดียว** (`logs.RingBuffer` ตัวเดิม) + ฟิลด์ `Chain` ในแต่ละ entry
แล้วกรองตอน query

เหตุผล:

- API/pagination/SSE ใช้โค้ดชุดเดียวได้ทั้ง Forward Traffic และ Local Traffic
  ต่างกันแค่ค่า `chain` ที่ส่งไป — ถ้าแยก buffer ต้องเขียน k-way merge ตอน query
  แล้วต้องนิยาม cursor ข้าม buffer (cursor ต้องกลายเป็นชุด id ต่อ buffer) = ซับซ้อนขึ้นมาก
- SSE stream มีเส้นเดียว (`/api/dashboard/logs/stream`) และ subscriber ผูกกับ buffer
  ถ้าแยก buffer ต้องมี stream ที่ 2 หรือ multiplex — งานเพิ่มโดยไม่ได้อะไรกลับมา
- ลำดับเวลาข้าม chain ถูกต้องโดยธรรมชาติ (insert order เดียวกัน) ไม่ต้อง sort ใหม่
- `Clear` ทำงานครั้งเดียวครอบคลุมทุกหน้า (พฤติกรรมเดิมที่ผู้ใช้คุ้นอยู่แล้ว)

**ทางเลือกที่ตัดทิ้ง — แยก buffer ต่อ chain:** ข้อดีเดียวคือทราฟฟิก forward ปริมาณมาก
จะไม่เบียด entry ของ input/output ให้หลุด buffer เร็ว → แก้ด้วยวิธีที่ถูกกว่าคือ
(ก) ขยาย capacity เป็น 10,000 และ (ข) ใส่ rate limit ให้กฎ log ที่ปริมาณสูงและไม่ระบุตัวตน
(final drop ของ input) ตาม §2.5 ทำให้ปัญหาการเบียดกันอยู่ในระดับที่ยอมรับได้
(หน้านี้ประกาศตัวเองว่าเป็น "ตัวอย่างล่าสุด" ไม่ใช่บันทึกครบถ้วนอยู่แล้ว)

### 2.2 capacity ใหม่ = 10,000 (ยืนยันการคำนวณ RAM ซ้ำแล้ว)

โครงสร้าง `model.FirewallLog` หลังเพิ่ม `Chain` = **11 ฟิลด์ string**

- header ของ string ใน Go = 16 byte (ptr+len) × 11 = **176 byte/entry** (ส่วนนี้อยู่ใน
  slice ของ ring buffer ตรงๆ)
- ข้อมูลจริงหลัง header: `id` uuid 36B, `time` RFC3339Nano ~30B, `src`/`dest` ≤15B (IPv4),
  `port` ≤5B, ที่เหลือ (`action`/`proto`/`chain`/`reason`/`inIface`/`outIface`) ส่วนใหญ่เป็น
  **string literal ในโค้ด** ("DROP", "TCP", "input", "Blocked (input)") ซึ่งใช้ backing array
  ร่วมกันทั้งโปรแกรม → ไม่ alloc ต่อ entry; `inIface`/`outIface` มาจาก cache ของ
  `ifaceNameResolver` → ก็ใช้ string เดิมซ้ำเช่นกัน
- ที่ alloc จริงต่อ entry ≈ id(48) + time(32) + src(16) + dest(16) + port(16) ตาม size class
  ของ Go allocator ≈ **128 byte** (+ overhead ของ heap ~10-20%)

รวม ≈ **300-350 byte/entry ในทางปฏิบัติ**, ตีกรอบบนแบบระมัดระวัง (ทุกฟิลด์ alloc จริง,
IPv6 ยาว, reason จาก `fmt.Sprintf`) ≈ **550 byte/entry**

| capacity | RAM (typical ~330B) | RAM (worst ~550B) | JSON ทั้ง buffer |
|---|---|---|---|
| 500 (ปัจจุบัน) | ~0.17 MB | ~0.27 MB | ~0.12 MB |
| 10,000 (**เลือก**) | ~3.3 MB | ~5.5 MB | ~2.5 MB |
| 20,000 | ~6.6 MB | ~11 MB | ~5 MB |

เลือก **10,000**: บน RPi5 (4-8 GB) กิน RAM ระดับ 3-6 MB = ต่ำกว่า 0.2% ปลอดภัยมาก
ให้ผู้ใช้เลื่อนดูได้ 20 หน้า (500/หน้า) ครอบคลุมช่วงเวลาย้อนหลังที่มีประโยชน์จริง
และยัง **ไม่ใหญ่พอที่จะทำให้ latency ของ `GetAll()` เป็นปัญหา** (copy 10k struct ≈ 1.8 MB
memcpy ต่อ 1 request — ระดับหลักมิลลิวินาที; ดู §6 ข้อ 7 สำหรับข้อจำกัดของวิธีนี้)
ไม่เลือก 20,000+ เพราะกำไรที่ได้ต่ำ แต่ค่า `GetAll()` ต่อ request เพิ่มเป็นเท่าตัว

> ผลข้างเคียงที่ต้องแก้พร้อมกัน: `HandleGetRecentLogs` ปัจจุบันคืน `GetAll()` ทั้งก้อน
> → ต้องใส่ `limit` (default 100, cap 500) ไม่งั้น Dashboard จะดาวน์โหลด ~2.5 MB
> ทุกครั้งที่เปิดหน้า/SSE reconnect (ดู T-08 และ §6 ข้อ 5)

### 2.3 NFLOG group ใหม่ 101 สำหรับ input+output (ไม่ใช้ group 100 ร่วมกับ forward)

ปัจจุบัน forward ใช้ `ForwardNflogGroup = 100` (`real_traffic_log.go`) — group อื่นยังไม่ถูกใช้
ในโปรเจกต์เลย (grep แล้วมีที่เดียว) → กำหนดเพิ่ม `LocalNflogGroup uint16 = 101`

**เลือกแยก group** ทั้งที่ chain ระบุได้จาก prefix อยู่แล้ว เพราะ:

- **การแยกคิวไม่ให้ท่วมกัน**: watcher แต่ละตัวมี channel ขนาด `trafficLogChanSize = 256`
  และ drop เมื่อเต็ม ถ้าใช้ group เดียว ทราฟฟิก forward ที่ไหลแรงจะดัน event ของ
  input/output (ซึ่งจำนวนน้อยกว่าและ "มีค่า" กว่าในเชิง diagnostic เช่น ใครยิงเข้าตัวบอร์ด)
  ตกหล่นไปด้วย — แยก group = แยก socket = แยกคิว = แยกตัวนับ drop
- ปรับ snaplen/บัฟเฟอร์ของฝั่ง local ได้อิสระในอนาคต โดยไม่กระทบ forward
- แก้ปัญหาได้ตรงจุดโดยไม่ทำให้ ring buffer ต้องแยก (คนละเรื่องกับ §2.1)

ต้นทุน: เพิ่ม goroutine + netlink socket อีก 1 ตัว (ไม่ต้องขอสิทธิ์เพิ่ม ใช้ `cap_net_admin` เดิม)

**ทางเลือกที่ตัดทิ้ง — ใช้ group 100 ร่วมกัน:** โค้ดน้อยกว่าจริง (ไม่ต้องแก้ interface)
แต่เสียการแยกคิวข้างต้น และทำให้ tuning ในอนาคตผูกติดกัน

โครงสร้างโค้ด: refactor `RealTrafficLog.WatchForwardTraffic` เป็น private
`watchGroup(ctx, group uint16, cb)` แล้วให้ทั้ง `WatchForwardTraffic` และ
`WatchLocalTraffic` (ใหม่) เรียกใช้ — ไม่ copy-paste loop เดิม

### 2.4 ฟิลด์ `Chain` + reason แยกตาม prefix

- `model.FirewallLog` เพิ่ม `Chain string \`json:"chain"\`` ค่าที่เป็นไปได้
  `forward | input | output` (ใช้ค่าคงที่ `model.PolicyChainForward/Input/Output` ที่มีอยู่แล้ว
  จาก PR #99 — ห้ามสร้างชุดค่าคงที่ซ้ำ)
- `parseNflogAttr` ต้องอ่าน prefix แล้วแยก 2 มิติ:

| prefix ที่โผล่จริงในโค้ด | chain | action | reason |
|---|---|---|---|
| `[PiGate] FWD ACCEPT: ` | forward | PASS | Allowed (forward) |
| `[PiGate] FWD DROP  : ` | forward | DROP | Blocked (forward) |
| `[PiGate] INP ACCEPT: ` | input | PASS | Allowed (local-in) |
| `[PiGate] INP DROP  : ` / `[PiGate]  INP DROP  : ` (มีช่องว่างเกิน 1 ตัวในกฎ notLocal) | input | DROP | Blocked (local-in) |
| `[PiGate] OUT ACCEPT: ` | output | PASS | Allowed (local-out) |
| `[PiGate] OUT DROP  : ` | output | DROP | Blocked (local-out) |
| ไม่มี prefix / ไม่รู้จัก | ตาม group ที่รับมา (100→forward, 101→input) | PASS | Logged |

  → ต้อง `strings.Fields()`/`strings.Contains` แบบทนช่องว่างซ้ำ **ห้ามเทียบ prefix ตรงตัว**
  (มีจุดที่เขียน `"[PiGate]  INP DROP  : "` สองช่องว่างอยู่จริง — ดู §6 ข้อ 3)
- `Chain` ต้องมาจาก prefix เป็นหลัก และ fallback เป็น chain ประจำ group เมื่อ prefix หาย
  (`parseNflogAttr` จึงต้องรับพารามิเตอร์ `defaultChain`)

### 2.5 ย้าย log ของ input/output จาก printk → NFLOG 101 (ตัด printk ทิ้ง ยกเว้น AUDIT)

**ตัด printk ทิ้งทั้งหมด** สำหรับ log ที่ผู้ใช้ควรเห็นในหน้า Local Traffic — ไม่เก็บทั้งคู่
(printk + NFLOG) เพราะจุดประสงค์หลักของการย้ายคือ **หยุดเขียน journald ลง SD card**
(tech_stack_design §8) การเก็บ printk ไว้คู่กันเท่ากับไม่ได้แก้อะไรเลย ส่วนการ debug
ยังทำได้ด้วยหน้า Local Traffic เอง (ซึ่งดีกว่า `journalctl -k` อยู่แล้ว: มีฟิลเตอร์/ค้นหา)

**ยกเว้น 1 จุด: กฎ AUDIT (`[PiGate] INP AUDIT : `) ให้คงเป็น printk** และ**เพิ่ม rate limit**
ให้มัน (ปัจจุบันไม่มี limit เลย = แหล่งเขียน SD card ต่อเนื่องที่ใหญ่ที่สุดในไฟล์)
เหตุผลที่ไม่ย้ายเข้า NFLOG: กฎนี้ log **ทุกแพ็กเก็ต**ที่ผ่าน section 1 มาถึง section 2
โดยไม่มี verdict — ถ้าส่งเข้า ring buffer ทุก entry จะถูกบันทึกซ้ำ 2 ครั้ง (AUDIT + ACCEPT/DROP)
และหน้า Local Traffic จะจมไปกับ entry ที่ไม่มีผลการตัดสิน มันเป็นจุด tap สำหรับ debug
ระดับ kernel ไม่ใช่ event ที่ผู้ใช้ต้องเห็น

ตารางการแปลงต่อจุด (ทุกจุดอยู่ใน `real_firewall.go`):

| จุด | เดิม | ใหม่ | หมายเหตุ |
|---|---|---|---|
| notLocal chain rule 3.4 (anti-spoof drop) | printk + `limit 3/min burst 10` | **NFLOG 101 + คง limit เดิม** | กฎ log ล้วน ไม่มี verdict → ใส่ limit ได้ปลอดภัย |
| section 2 AUDIT | printk ไม่มี limit | **printk + เพิ่ม `limit 3/min burst 10`** | ไม่ย้าย (เหตุผลด้านบน) กฎ log ล้วน ใส่ limit ได้ |
| docker-compat accept (input) ×2 | printk + verdict ในกฎเดียว | **NFLOG 101 + verdict เดิม** | สลับเฉพาะ expr.Log ห้ามแตะ verdict |
| `addAdminAccessRules` (ทุกเคส) | printk + verdict | **NFLOG 101 + verdict เดิม** | จุดสำคัญ: ทำให้เห็นว่าใครเข้าหน้าเว็บ/SSH |
| `addDNSServerAccessRules` | printk + verdict | **NFLOG 101 + verdict เดิม** | |
| section 4 final drop log (input) | printk ไม่มี limit | **NFLOG 101 + เพิ่ม `limit 10/second burst 20`** | กฎ log ล้วน (drop มาจาก chain policy) ปริมาณสูงสุดในระบบ (noise จาก WAN) → ใส่ limit กัน ring buffer ถูกท่วม |
| `buildRuleExpressions` สาขา input/output | แยก 2 กฎ (log+limit / counter+verdict) | **รวมเหลือกฎเดียว** `match + counter + NFLOG + verdict` เหมือนสาขา forward | ดู §2.6 |

### 2.6 ยุบการแยกกฎ log/verdict ของ input/output (Caution 5 ของแผนเดิมหมดเงื่อนไข)

เหตุผลที่แผนเดิมต้องแยกเป็น 2 กฎคือ *"log ของ input/output ไปที่ printk จึงต้องมี `limit`
และกฎที่มี `limit` จะพก verdict ไม่ได้"* — เมื่อย้ายไป NFLOG แล้ว **ไม่ต้องมี `limit` อีก**
(NFLOG ไม่แตะ SD card, overflow ถูก drop ที่ socket/channel โดยไม่กระทบแพ็กเก็ต)
จึงยุบเหลือกฎเดียวได้เหมือนสาขา forward ผลพลอยได้: counter ตรงกับกฎที่ตัดสินจริง 1:1
และจำนวนกฎใน chain ลดลงเกือบครึ่ง

**หลักการที่ยังคงบังคับใช้ 100%: ห้ามใส่ `limit` ในกฎที่มี verdict** — กฎที่เหลือ `limit`
หลังงานนี้มีแค่กฎ log ล้วน 3 จุด (notLocal 3.4, AUDIT, final drop) ซึ่งไม่มี verdict
→ ต้องมีเทสต์ยืนยันเงื่อนไขนี้แบบทั่วไป ไม่ใช่ยืนยันการแยกกฎแบบเดิม (ดู T-05)

### 2.7 Cursor-based pagination (สัญญา API + กติกาฝั่ง client)

```
GET /api/logs/traffic?chain=&action=&q=&limit=500&beforeId=<id>&beforeTime=<rfc3339nano>
```

- **กรองก่อน ตัดทีหลังเสมอ**: อ่าน `GetAll()` (newest-first) → กรอง chain/action/q ทั้งก้อน
  → หาตำแหน่ง cursor ในผลลัพธ์ที่กรองแล้ว → ตัดเอาแถวถัดจาก cursor `limit` แถว
- **cursor = (`beforeId`, `beforeTime`) ของแถวล่างสุดที่ client มีแล้ว ภายใต้ฟิลเตอร์ปัจจุบัน**
  ไม่ใช่ offset ตัวเลข เพราะ ring buffer ถูก prepend ตลอดเวลาจาก NFLOG → offset เพี้ยนทันที
- **fallback**: ถ้าหา `beforeId` ในลิสต์ที่กรองแล้วไม่เจอ (แถวนั้นถูก evict ออกจาก buffer
  ระหว่างที่ผู้ใช้เลื่อนช้า) → ตัดด้วยเวลาแทน: เอาเฉพาะแถวที่ `time < beforeTime`
  (ถ้า `beforeTime` ว่าง/parse ไม่ผ่าน → คืน "หมดแล้ว" คือ array ว่าง ไม่ใช่คืนหน้าแรกซ้ำ)
- **สัญญาณจบ**: คืนน้อยกว่า `limit` ที่ขอ = ไม่มีของเก่ากว่านี้แล้ว (ไม่เพิ่ม field ใน response
  เพื่อคง backward compatibility ของ array เปล่า)
- `limit`: default 100 (เดิม), cap **1000** (กันขอทีเดียวทั้ง buffer); frontend ใช้ 500
- `chain`: `forward` | `input` | `output` | `local` (= input+output) | ว่าง (= ทุก chain)
  — validate ค่าที่ไม่รู้จักด้วยการ **ปฏิเสธเป็น 400** ไม่ใช่เงียบๆ คืนทุก chain

**เปลี่ยนฟิลเตอร์ = reset ทุกอย่าง**: ทิ้ง cursor + ล้างแถวที่สะสมไว้ + fetch หน้าแรกใหม่
(ผูกกับ `refreshKey` ที่มีอยู่แล้วใน `useLiveLogs`)

**การ merge กับ SSE (สำคัญ)**: hook เดิม `loadSnapshot` **แทนที่ทั้งลิสต์** — ถ้าคงพฤติกรรมนี้
ผู้ใช้ที่เลื่อนไป 5 หน้าแล้ว SSE reconnect (ทุก error/หลุด) จะโดนตัดกลับเหลือหน้าแรกทันที
→ hook ใหม่ต้อง **merge หน้าแรกเข้ากับของเดิมโดย dedupe ด้วย `id`** และคงแถวเก่าที่โหลดมาแล้วไว้
(รายละเอียดใน T-12)

**เพดานฝั่ง client**: `MAX_LOGS = 500` ปัจจุบันจะตัดหน้าที่โหลดเพิ่มทิ้งทันที
→ ทำให้เป็น option `maxRows` (Dashboard คงใช้ 500, หน้า traffic ใช้ **5,000**)
เลือก 5,000 เพราะ 500×10 หน้าพอสำหรับการไล่ดูจริง และ DOM ~5,000 แถว × 9 คอลัมน์
ยังอยู่ในวิสัยที่ React จัดการได้โดยไม่ต้องทำ virtualization (นอกขอบเขต)

### 2.8 โครงหน้าเว็บ: แตกเป็น component ใช้ร่วม

`ForwardTraffic.tsx` ปัจจุบัน = header + note + filter bar + table + clear/pause
→ แตกเป็น `components/logs/TrafficLogPage.tsx` (รับ props: title/คำอธิบาย/icon/
`chainFilter`/ตัวเลือก dropdown เพิ่มเติม) แล้วให้ `ForwardTraffic.tsx` และ
`LocalTraffic.tsx` เป็น wrapper บางๆ — รูปแบบเดียวกับที่ PR #99 ทำกับ
`components/policy/PolicyChainPage.tsx` (Firewall/Local-In/Local-Out ใช้ตัวเดียวกัน)

---

## 3. ขั้นตอนการทำ (เรียงตาม dependency: model → kernel → main → api → docs → frontend)

### T-01 · เพิ่มฟิลด์ `Chain` ใน `model.FirewallLog`

- **layer**: model · **files**: `backend/internal/model/types.go`
- **instruction**: เพิ่มฟิลด์ `Chain string \`json:"chain"\`` ต่อท้าย struct `FirewallLog`
  พร้อมคอมเมนต์ว่าใช้ค่าคงที่ `PolicyChainForward/Input/Output` ที่มีอยู่แล้วในไฟล์นี้
  (ห้ามประกาศค่าคงที่ชุดใหม่) ค่าว่างหมายถึง "ไม่ทราบ" และฝั่ง UI ต้องรับได้
- **acceptance**: `go build ./...` ผ่าน; ไม่มีการเปลี่ยนฟิลด์เดิม
- **depends_on**: —

### T-02 · kernel: NFLOG group ใหม่ + ย้าย log ของ input/output ออกจาก printk (**sensitive — review เข้ม**)

- **layer**: kernel · **files**: `backend/internal/kernel/real_traffic_log.go`,
  `backend/internal/kernel/real_firewall.go`
- **instruction**:
  1. ใน `real_traffic_log.go` เพิ่มค่าคงที่ `LocalNflogGroup uint16 = 101`
     (+ ใช้ snaplen เดิม `ForwardNflogSnaplen`) พร้อมคอมเมนต์อธิบายเหตุผลที่แยก group
  2. ใน `real_firewall.go` เพิ่ม `localLogExpr(logPrefix string) *expr.Log` คู่แฝดของ
     `forwardLogExpr` แต่ `Group: LocalNflogGroup`
  3. แทนที่ `&expr.Log{Key: uint32(1 << unix.NFTA_LOG_PREFIX), ...}` ด้วย `localLogExpr(...)`
     ทุกจุดของ chain input/output **ยกเว้นกฎ AUDIT** ตามตารางใน §2.5
  4. กฎ AUDIT: คง printk ไว้ แต่เพิ่ม `&expr.Limit{Type: LimitTypePkts, Rate: 3,
     Unit: LimitTimeMinute, Burst: 10}` **ก่อน** expr.Log (กฎนี้ไม่มี verdict → ปลอดภัย)
  5. กฎ final drop log ของ input (section 4): เพิ่ม `&expr.Limit{... Rate: 10,
     Unit: LimitTimeSecond, Burst: 20}` ก่อน `localLogExpr(...)` (กฎนี้ไม่มี verdict เช่นกัน
     — verdict มาจาก chain policy drop **ห้ามเผลอเติม verdict ลงในกฎนี้**)
  6. ใน `buildRuleExpressions` สาขา input/output: ยุบการแยก 2 กฎเหลือกฎเดียว
     `exprs + Counter + (localLogExpr เมื่อ logEnabled) + verdictExpr()` (ไม่มี `limit`)
     ห้ามใส่ fwmark/NAT ในสาขานี้เหมือนเดิม
- **ข้อห้าม**: ห้ามแก้ลำดับกฎ, ห้ามแก้ verdict/policy ของ chain ใดๆ, ห้ามแตะสาขา forward
- **acceptance**: `go build ./...` ผ่าน; grep แล้วไม่มี `expr.Log` แบบ printk เหลือใน
  chain input/output นอกจากกฎ AUDIT; ทุกกฎที่มี `expr.Limit` ต้องไม่มี `expr.Verdict`
- **depends_on**: T-01

### T-03 · kernel: parser แยก chain/reason ตาม prefix + `WatchLocalTraffic`

- **layer**: kernel · **files**: `backend/internal/kernel/real_traffic_log.go`,
  `backend/internal/kernel/interfaces.go`
- **instruction**:
  1. refactor loop เดิมใน `WatchForwardTraffic` เป็น private
     `watchGroup(ctx context.Context, group uint16, defaultChain string, cb func(model.FirewallLog)) error`
     (คงกลไก channel `trafficLogChanSize` + drop counter + errFunc เดิมทุกอย่าง แต่ข้อความ log
     ต้องบอก group/chain ที่กำลังฟัง)
  2. `WatchForwardTraffic` เรียก `watchGroup(ctx, ForwardNflogGroup, model.PolicyChainForward, cb)`
  3. เพิ่ม `WatchLocalTraffic(ctx, cb)` → `watchGroup(ctx, LocalNflogGroup, model.PolicyChainInput, cb)`
  4. `parseNflogAttr(attr, resolveIface, defaultChain)` ตั้ง `entry.Chain` + `Action` + `Reason`
     ตามตารางใน §2.4 โดย normalize prefix ด้วยการตัดช่องว่างซ้ำ (เช่น `strings.Fields`)
     ห้ามเทียบสตริงเต็มรูปแบบ
  5. `interfaces.go`: เพิ่ม `WatchLocalTraffic` ใน `TrafficLogManager` + อัปเดตคอมเมนต์ของ
     interface ว่าครอบคลุม 3 chain แล้ว
- **acceptance**: `go build ./...` ผ่าน; ฟังก์ชัน `parseNflogAttr` คืน `Chain` ถูกต้องสำหรับ
  prefix ทั้ง 6 แบบใน §2.4
- **depends_on**: T-01, T-02

### T-04 · kernel: mock รองรับ chain ทั้ง 3

- **layer**: kernel · **files**: `backend/internal/kernel/mock.go`
- **instruction**: `MockTrafficLog` ต้อง implement `WatchLocalTraffic` ด้วย — สังเคราะห์
  event ของ chain `input` (เช่น admin เข้าหน้าเว็บ/ping/ยิงพอร์ตแปลกจาก WAN) และ `output`
  (เช่น DNS/NTP ขาออก) ทุก ~4 วินาที และ `WatchForwardTraffic` เดิมต้องเซ็ต
  `Chain: model.PolicyChainForward` ให้ทุก entry; reason ใช้ข้อความชุดเดียวกับ §2.4
- **acceptance**: `-mock=true` แล้วมี entry ครบทั้ง 3 chain ไหลเข้า ring buffer
- **depends_on**: T-03

### T-05 · kernel: แก้/เพิ่มเทสต์ให้ตรงพฤติกรรมใหม่

- **layer**: kernel · **files**: `backend/internal/kernel/policy_chain_test.go`,
  `backend/internal/kernel/real_traffic_log_test.go`
- **instruction**:
  1. แทน `TestBuildRuleExpressions_LogSplitOnInputOutput` ด้วย
     `TestBuildRuleExpressions_InputOutputSingleRuleWithNflog`: ยืนยันว่ากฎของ input/output
     ที่เปิด log เป็น **กฎเดียว** มี `expr.Log` ที่ `Group == LocalNflogGroup`, มี `Counter`,
     มี `Verdict` และ **ไม่มี** `expr.Limit`
  2. เพิ่มเทสต์กติกาความปลอดภัยแบบทั่วไป: สำหรับทุก ruleSet ที่ `buildRuleExpressions` คืน
     ถ้ามี `expr.Limit` ต้องไม่มี `expr.Verdict` ในกฎเดียวกัน (แทนที่การเช็กเชิงโครงสร้างเดิม)
  3. `real_traffic_log_test.go`: เพิ่มเคส prefix `INP ACCEPT` / `INP DROP` (ทั้งแบบช่องว่างเดียว
     และสองช่องว่าง) / `OUT ACCEPT` / `OUT DROP` / prefix หาย → ยืนยัน `Chain`+`Action`+`Reason`
- **acceptance**: `cd backend && go test ./internal/kernel/...` ผ่าน
- **depends_on**: T-03

### T-06 · logs: ไม่แก้ตรรกะ ring buffer แต่เพิ่ม `Capacity()`

- **layer**: service/logs · **files**: `backend/internal/logs/ringbuffer.go`
- **instruction**: เพิ่มเมธอด `Capacity() int` (อ่านอย่างเดียว, ใช้ RLock) เพื่อให้ชั้น API
  cap ค่า `limit` ได้โดยไม่ต้องฮาร์ดโค้ดตัวเลขซ้ำ ห้ามแก้ Add/GetAll/Clear/Subscribe
- **acceptance**: `go test ./internal/logs/...` เดิมยังผ่าน
- **depends_on**: —

### T-07 · main: capacity 10,000 + start watcher ตัวที่สอง + timestamp RFC3339Nano

- **layer**: service · **files**: `backend/cmd/pigate/main.go`
- **instruction**:
  1. ประกาศค่าคงที่ระดับไฟล์ `trafficLogBufferCapacity = 10000` พร้อมคอมเมนต์อ้างการคำนวณ
     RAM (~3-5 MB) แล้วใช้แทนเลข 500
  2. เปลี่ยน `time.RFC3339` → `time.RFC3339Nano` ในการ stamp เวลา (ต้องทำเพื่อให้ cursor
     แบบ time ไม่ชนกันในวินาทีเดียวกัน — ดู §6 ข้อ 4)
  3. แยก callback stamp (`stampAndPush`) ให้ใช้ร่วมกัน แล้ว `go` เพิ่มอีก 1 ตัวเรียก
     `trafficLog.WatchLocalTraffic(monitorCtx, stampAndPush)` โดยล็อกข้อความ warning
     แยกจาก forward ให้ชัด (NFLOG group ไหนล้ม ต้องรู้)
- **acceptance**: `go build ./...` ผ่าน; รัน `-mock=true` แล้ว log แจ้ง watcher 2 ตัว
- **depends_on**: T-03, T-04

### T-08 · api: cursor + chain filter ใน `/logs/traffic` และ limit ใน `/dashboard/logs`

- **layer**: api · **files**: `backend/internal/api/handlers.go`
- **instruction**:
  1. `HandleGetTrafficLogs`: อ่าน `chain`, `action`, `q`, `limit`, `beforeId`, `beforeTime`
     - validate `chain` ∈ {"", forward, input, output, local} ไม่ตรง → 400
     - `limit` default 100, ถ้า ≤0 หรือ parse ไม่ผ่านใช้ default, cap ที่ `min(1000, s.logs.Capacity())`
     - **กรองทั้งก้อนก่อน**: วนทั้ง `GetAll()` สร้าง `matched []model.FirewallLog` เต็มชุด
       (ห้าม break ที่ limit ในลูปกรอง)
     - ถ้ามี `beforeId`: หา index ใน `matched` → ตัดเอา `matched[idx+1:]`
       ถ้าไม่เจอและมี `beforeTime` (parse ด้วย `time.Parse(time.RFC3339Nano, …)` โดยรองรับ
       RFC3339 ธรรมดาด้วย) → เอาเฉพาะรายการที่เวลา < beforeTime
       ถ้าไม่เจอทั้งคู่ → คืน `[]` (array ว่าง ไม่ใช่ null)
     - ตัด `limit` แถวแรกของผลลัพธ์แล้วคืน array (คงรูปแบบเดิม)
  2. `HandleGetRecentLogs`: เพิ่ม `limit` (default 100, cap 500) แล้ว slice จาก `GetAll()`
     — ป้องกัน payload หลาย MB หลังขยาย buffer
  3. คอมเมนต์อธิบายกฎ "กรองก่อน–ตัด cursor ทีหลัง" และเหตุผลที่ไม่ใช้ offset ให้ครบ
- **ข้อห้าม**: ห้ามคืน `null` แทน array ว่าง (frontend `.map` จะพัง), ห้าม cap `limit`
  ด้วย `len(all)` แบบเดิมจนได้ 0 เมื่อ buffer ว่าง
- **acceptance**: `go build ./...` ผ่าน; เรียกด้วย query เดิม (ไม่มี cursor/chain) ได้ผลเหมือนเดิม
- **depends_on**: T-01, T-06

### T-09 · api: เทสต์ handler ของ pagination/filter

- **layer**: api · **files**: `backend/internal/api/handlers_test.go` (หรือไฟล์เทสต์ใหม่
  `traffic_logs_test.go` ในแพ็กเกจเดียวกัน)
- **instruction**: เขียนเทสต์ยัด entry ปลอมลง `logs.NewRingBuffer(...)` แล้วยืนยัน
  - กรอง `chain=input` / `chain=local` / ไม่ส่ง chain ได้ผลตรง
  - `limit=2` + ไล่ cursor ทีละหน้า → ได้ครบทุกแถวไม่ซ้ำไม่ขาด และหน้าสุดท้ายสั้นกว่า limit
  - cursor ที่ id ไม่มีใน buffer แต่มี `beforeTime` → fallback ตัดด้วยเวลาถูกต้อง
  - `q` + `action` ทำงานร่วมกับ cursor (กรองก่อนตัด): ยืนยันว่าแถวที่ตรงเงื่อนไขซึ่งอยู่ลึกกว่า
    500 แถวแรกยังถูกคืน
  - `chain=bogus` → 400
- **acceptance**: `cd backend && go test ./internal/api/...` ผ่าน
- **depends_on**: T-08

### T-10 · เอกสาร API (sync 2 ไฟล์)

- **layer**: docs · **files**: `docs/openapi.yaml`, `frontend/public/openapi.yaml`
- **instruction**: อัปเดตให้ตรงกันทั้งสองไฟล์ (ห้ามแก้ `backend/internal/api/dist/openapi.yaml`
  ซึ่งเป็นผลลัพธ์ของ `build.sh`)
  - schema `FirewallLog`: เพิ่ม `chain` (enum forward/input/output) ใน properties + required,
    แก้ตัวอย่าง `time` จาก `"14:31:02"` เป็น RFC3339Nano จริง
  - path `/logs/traffic`: เพิ่มพารามิเตอร์ `chain`, `beforeId`, `beforeTime`, อธิบาย
    กติกา "คืนน้อยกว่า limit = หมดแล้ว", แก้ summary/description ให้ครอบคลุม 3 chain
  - path `/dashboard/logs`: เพิ่มพารามิเตอร์ `limit`
- **acceptance**: YAML ทั้งสองไฟล์เหมือนกัน (diff ว่าง) และ parse ได้
- **depends_on**: T-08

### T-11 · frontend: service layer

- **layer**: frontend · **files**: `frontend/src/services/trafficLogService.ts`,
  `frontend/src/services/dashboardService.ts`
- **instruction**:
  - `TrafficLog` เพิ่ม `chain: "forward" | "input" | "output"`; `SSELogEntry` ใน
    dashboardService เพิ่ม `chain?: string` (optional เพื่อไม่ให้ Dashboard พัง)
  - `TrafficLogQuery` เพิ่ม `chain?`, `beforeId?`, `beforeTime?`
  - โหมด mock: ให้ mock feed สร้าง entry ครบ 3 chain, รองรับการกรอง `chain`
    (`local` = input+output) และ cursor แบบเดียวกับ backend (กรองก่อน→ตัด cursor→ตัด limit)
    เพื่อให้ทดสอบ infinite scroll บน `-mock` ได้จริง; เพิ่มจำนวน seed ให้พอเลื่อนหลายหน้า
- **acceptance**: `yarn build` ผ่าน (tsc)
- **depends_on**: T-10

### T-12 · frontend: hook pagination + แก้ `useLiveLogs` ไม่ให้ล้างหน้าที่โหลดแล้ว

- **layer**: frontend · **files**: `frontend/src/hooks/useLiveLogs.ts`,
  `frontend/src/hooks/usePaginatedLiveLogs.ts` (ใหม่)
- **instruction**:
  - `useLiveLogs`: เพิ่ม option `maxRows` (default 500 คงพฤติกรรม Dashboard เดิม) แทนค่าคงที่
    `MAX_LOGS` — **ห้ามเปลี่ยนพฤติกรรมที่ Dashboard ใช้อยู่**
  - สร้าง `usePaginatedLiveLogs<T>` ใหม่ (ใช้ภายในเรียก SSE ตัวเดิมผ่าน
    `dashboardService.connectSSELogs`) คืน `{ logs, isLoading, isLoadingMore, hasMore, loadMore }`
    กติกา:
    1. mount/`refreshKey` เปลี่ยน → ล้างลิสต์+cursor แล้ว fetch หน้าแรก (`pageSize`)
    2. `loadMore()` ใช้ `id`/`time` ของ **แถวสุดท้ายในลิสต์ปัจจุบัน** เป็น cursor,
       กันเรียกซ้อน (ref guard) และหยุดเมื่อ `hasMore === false`
    3. ผลลัพธ์ที่ได้ **ต่อท้าย** พร้อม dedupe ด้วย `id`; ได้น้อยกว่า `pageSize` → `hasMore=false`
    4. SSE `onLog` → prepend + dedupe (เหมือนเดิม) และ **ต้องไม่แตะ cursor**
    5. SSE `onOpen`/`onError` → refetch เฉพาะ**หน้าแรก** แล้ว **merge เข้าหัวลิสต์** (dedupe by id,
       ห้าม replace ทั้งลิสต์) เพื่อไม่ให้หน้าที่โหลดมาแล้วหายตอน reconnect
    6. `onClear` → ล้างลิสต์ + reset cursor + `hasMore=true`
    7. เพดาน `maxRows` (5,000): ตัด**ท้าย**ลิสต์ทิ้งเมื่อเกิน และเมื่อถูกตัด ต้องตั้ง
       `hasMore=true` ไว้เหมือนเดิม (cursor คำนวณจากแถวสุดท้ายที่เหลืออยู่เสมอ)
- **acceptance**: `yarn build` + `yarn lint` ผ่าน; Dashboard ยังใช้ `useLiveLogs` ได้เหมือนเดิม
- **depends_on**: T-11

### T-13 · frontend: แตก `ForwardTraffic.tsx` เป็น `TrafficLogPage` ใช้ซ้ำ

- **layer**: frontend · **files**: `frontend/src/components/logs/TrafficLogPage.tsx` (ใหม่),
  `frontend/src/pages/ForwardTraffic.tsx`
- **instruction**:
  - ย้าย UI ทั้งหมด (header/note/filter bar/table/pause/clear) ไป `TrafficLogPage`
    รับ props: `title`, `description`, `icon`, `noteContent`, `chainParam`
    (`"forward" | "local"`), `extraFilter?` (dropdown เพิ่มของหน้า Local Traffic)
  - เพิ่มคอลัมน์ **Chain** (badge FWD/INP/OUT ผ่านตัวแปรธีมเท่านั้น — ห้ามใช้สีดิบ
    ตาม `docs/rules_of_work.md`) แสดงเสมอ
  - เปลี่ยนไปใช้ `usePaginatedLiveLogs` (pageSize **500**, maxRows 5,000) และเพิ่ม
    sentinel div ท้ายตาราง + `IntersectionObserver` เรียก `loadMore()`
    (มีปุ่ม "Load more" สำรองไว้ด้วย เผื่อ observer ไม่ทำงานในบางเบราว์เซอร์/เลย์เอาต์)
  - แสดงสถานะท้ายตาราง: กำลังโหลดเพิ่ม / "แสดง N รายการ · ไม่มีข้อมูลเก่ากว่านี้แล้ว"
  - `matchesFilter` ฝั่ง client ต้อง **mirror ฟิลเตอร์ฝั่ง server ให้ครบ รวม `chain` ด้วย**
    (ไม่งั้น entry ของ input จะโผล่ในหน้า Forward Traffic ผ่าน SSE — ดู §6 ข้อ 6)
  - `ForwardTraffic.tsx` เหลือเป็น wrapper บางๆ (`chainParam="forward"`)
- **acceptance**: `yarn build` + `yarn lint` ผ่าน; หน้า Forward Traffic ทำงานเหมือนเดิม + เลื่อนโหลดต่อได้
- **depends_on**: T-12

### T-14 · frontend: หน้าใหม่ Local Traffic + route/เมนู/title

- **layer**: frontend · **files**: `frontend/src/pages/LocalTraffic.tsx` (ใหม่),
  `frontend/src/App.tsx`, `frontend/src/components/app-sidebar.tsx`,
  `frontend/src/components/site-header.tsx`
- **instruction**:
  - `LocalTraffic.tsx` = wrapper ของ `TrafficLogPage` (`chainParam="local"`) พร้อม dropdown
    `All local / Local-In only / Local-Out only` → map เป็น `chain=local|input|output`
    (การเปลี่ยนค่านี้ต้อง reset pagination ผ่าน `refreshKey` เหมือนฟิลเตอร์อื่น)
  - คำอธิบายหัวหน้า: ทราฟฟิกที่ "ปลายทางเป็นตัวบอร์ดเอง" (Local-In) และ "บอร์ดส่งออกเอง"
    (Local-Out) + หมายเหตุว่า connection ที่ established แล้วจะไม่ปรากฏ และ log จะเห็นเฉพาะ
    กฎที่เปิด Log ไว้ (ยกเว้นกฎโครงสร้างที่ log เสมอ) — ใช้โทนเดียวกับหน้า Forward Traffic
  - `App.tsx`: `<Route path="local" element={<LocalTraffic />} />` ใต้ `logs`
  - `app-sidebar.tsx`: เพิ่ม `{ path: "/logs/local", label: "Local Traffic", icon: ... }`
    ต่อจาก Forward Traffic (ใช้ icon จาก lucide ที่มีอยู่ เช่น `ShieldAlert`/`ArrowDownUp`)
  - `site-header.tsx`: เพิ่ม title mapping `"/logs/local": "Local Traffic"`
- **acceptance**: `yarn build` + `yarn lint` ผ่าน; เข้าหน้าใหม่จากเมนูได้ หัวหน้าเว็บแสดงชื่อถูก
- **depends_on**: T-13

### T-15 · เอกสารสถาปัตยกรรม

- **layer**: docs · **files**: `docs/tech_stack_design.md`, `README.md`
- **instruction**:
  - §8: เพิ่มว่า log ของ chain input/output ย้ายจาก printk/journald มาเข้า NFLOG group 101
    → ring buffer ในแรม (ลดการเขียน SD card ลงอีก) และระบุ capacity ใหม่ 10,000
  - §9 หัวข้อ "Forward Traffic Log": เปลี่ยนเป็นครอบคลุม 3 chain, ระบุ group 100 = forward,
    101 = input/output และกฎ AUDIT ที่ยังอยู่บน printk แบบ rate-limited
  - README Feature Status: เพิ่มบรรทัด/หมายเหตุหน้า Local Traffic (Completed ทั้ง FE/BE)
- **acceptance**: เอกสารตรงกับโค้ดที่ทำจริงใน T-02/T-03/T-07
- **depends_on**: T-14

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | สิทธิ์ | เปลี่ยนแปลง |
|---|---|---|---|
| GET | `/api/logs/traffic` | ทุก role ที่ล็อกอิน | **เพิ่ม** `chain`, `beforeId`, `beforeTime`; `limit` cap 1000; กรองก่อนตัด cursor |
| GET | `/api/dashboard/logs` | ทุก role | **เพิ่ม** `limit` (default 100, cap 500) |
| POST | `/api/dashboard/logs/clear` | ทุก role (ปุ่มโชว์เฉพาะ super_admin) | ไม่เปลี่ยน (ล้างทั้ง buffer = ทุก chain) |
| GET | `/api/dashboard/logs/stream` | ทุก role | ไม่เปลี่ยนสัญญา แต่ payload มีฟิลด์ `chain` เพิ่ม |

- ไม่มี endpoint ใหม่ และไม่มี route ใหม่ใน `router.go` (หน้า Local Traffic ใช้เส้นเดิม
  ต่างกันแค่ query `chain=local`) → ลดพื้นที่ผิวของ API ที่ต้อง review
- `-disable-edit=true`: ทุกเส้นในตารางนี้เป็น GET ยกเว้น clear ที่ถูก `DisableEditMiddleware`
  บล็อกอยู่แล้ว — ไม่ต้องแก้อะไร
- client เก่าที่ไม่ส่ง `chain`/cursor ยังทำงานได้เหมือนเดิมทุกเส้น

---

## 5. ผลกระทบต่อ SD card (สรุปเชิงตัวเลข)

| เหตุการณ์ | ก่อน | หลัง |
|---|---|---|
| แพ็กเก็ตถูก DROP ที่ท้าย input chain (noise จาก WAN, ยิงพอร์ตสแกน) | printk **ทุกแพ็กเก็ต** → journald → เขียน SD card | NFLOG (RAM) + จำกัด 10/วินาที → **ไม่เขียน SD card** |
| แพ็กเก็ตเข้าหน้าเว็บ/SSH (Admin Access accept) | printk ทุกแพ็กเก็ตแรกของ connection | NFLOG (RAM) |
| กฎผู้ใช้ใน input/output ที่เปิด Log | printk (จำกัด 3/นาที) | NFLOG (RAM) ไม่จำกัด |
| กฎ AUDIT ของ input | printk **ทุกแพ็กเก็ต ไม่จำกัด** | printk **จำกัด 3/นาที** (ลดลงมาก) |

→ งานนี้ทำให้สอดคล้องกับ `tech_stack_design.md` §8 **มากกว่าเดิมอย่างมีนัยสำคัญ**
ไม่มีเส้นทางใดที่เพิ่มการเขียนดิสก์

---

## 6. ข้อควรระวัง

1. **แผนนี้กลับด้าน Caution 6 ของแผนเดิมอย่างตั้งใจ (ไม่ใช่ขัดกัน)** —
   `input-output-chain-firewall-plan.md` §5 ข้อ 6 ห้ามส่ง log ของ input/output เข้า NFLOG
   *group 100* เพราะ parser ฮาร์ดโค้ด reason เป็น forward และหน้า Forward Traffic จะปนกัน
   แผนนี้ (ก) ใช้ **group 101 ไม่ใช่ 100** และ (ข) แก้ parser ให้แยก prefix `INP/OUT/FWD`
   ตามที่แผนเดิมสั่งไว้ตรงๆ และ (ค) หน้า Forward Traffic กรอง `chain=forward` เสมอ
   → **T-03 กับ T-13 ต้องทำครบทั้งคู่ก่อน merge** ถ้าทำแค่ T-02 (ย้าย log) แล้วข้าม parser
   จะเกิดผลเสียตรงตามที่แผนเดิมเตือนไว้ทันที
2. **ห้ามใส่ `limit` ในกฎที่มี verdict เด็ดขาด** (Caution 5 ของแผนเดิม ยังบังคับใช้ตลอดกาล) —
   ใน nftables กฎ `limit ... drop` จะ drop เฉพาะแพ็กเก็ตที่ผ่านตัวจำกัดอัตรา ที่เหลือหลุดไป
   กฎถัดไป = กฎความปลอดภัยรั่ว งานนี้เพิ่ม `limit` 2 จุด (AUDIT, final drop ของ input)
   ซึ่ง**ทั้งคู่เป็นกฎ log ล้วนที่ไม่มี verdict** — ถ้ามีใครเผลอเติม verdict ลงในกฎเดียวกัน
   ภายหลัง = ช่องโหว่ทันที → ต้องมีเทสต์เชิงกติกาใน T-05 ไม่ใช่แค่เทสต์เคสเฉพาะ
3. **prefix มีช่องว่างไม่สม่ำเสมอ** — มีจุดหนึ่งเขียน `"[PiGate]  INP DROP  : "` (สองช่องว่าง
   หลัง `[PiGate]`) ต่างจากที่อื่น ถ้า parser เทียบสตริงเต็มรูปแบบจะพลาดเงียบๆ กลายเป็น
   chain ผิด → ต้อง normalize ด้วย `strings.Fields` หรือเทียบ token `INP`/`OUT`/`FWD`
   และต้องมีเทสต์เคสของรูปแบบสองช่องว่างนี้โดยเฉพาะ
4. **`time.RFC3339` ละเอียดแค่วินาที → cursor fallback เพี้ยน** — บนทราฟฟิกหนัก entry
   หลายสิบตัวมี `time` เท่ากันเป๊ะ ถ้า fallback ตัดด้วย `time < beforeTime` จะข้ามพี่น้อง
   ในวินาทีเดียวกันทิ้งทั้งชุด → **ต้องเปลี่ยนเป็น `RFC3339Nano` ใน T-07** และ fallback
   นี้ทำงานเฉพาะกรณี id ถูก evict เท่านั้น (เส้นทางหลักคือหา id เจอ ซึ่งแม่นยำ 100%)
   *ทางเลือกที่แข็งแรงกว่า (ไม่ทำในแผนนี้): ให้ ring buffer แจก `seq uint64` แบบ monotonic
   แล้วใช้ `seq` เป็น cursor — ตัดปัญหาเวลาซ้ำ/evict ทิ้งทั้งหมด แต่ต้องเพิ่มฟิลด์ใน
   wire format และเจ้าของโปรเจกต์ตกลงสัญญา (id, time) ไว้แล้ว → บันทึกไว้เป็นทางเลือก
   สำหรับ hardening รอบหน้า*
5. **Dashboard จะดาวน์โหลดทั้ง buffer ถ้าลืมใส่ `limit` ให้ `/dashboard/logs`** —
   หลังขยาย capacity 20 เท่า payload กลายเป็น ~2.5 MB ต่อการเปิดหน้า/reconnect หนึ่งครั้ง
   บน Wi-Fi ของ RPi5 นี่คือ regression ที่ผู้ใช้รู้สึกได้ → T-08 ข้อ 2 ห้ามข้าม
6. **entry ของ input/output จะโผล่ในหน้า Forward Traffic ผ่าน SSE ถ้าลืมกรอง `chain` ฝั่ง client** —
   SSE ส่งทุก entry ของ buffer เดียวกันให้ทุกหน้า snapshot ถูกกรองที่ server แต่ event สดกรอง
   ที่ client เท่านั้น (`matchesFilter`) → ต้องเพิ่มเงื่อนไข chain เข้าไปใน `matchesFilter`
   ให้ mirror ฝั่ง server เป๊ะ (เป็นบั๊กประเภทเดียวกับ Caution 8 ของแผน forward-traffic เดิม)
7. **Dashboard "Recent Logs" จะเห็น entry ของ input/output ด้วย (พฤติกรรมเปลี่ยน — ตั้งใจ)** —
   วิดเจ็ตนี้อ่าน buffer เดียวกันโดยไม่กรอง chain ตั้งใจปล่อยไว้เพราะ event แบบ
   "ถูกยิงเข้าตัวบอร์ดแล้วโดน DROP" มีค่ากับผู้ดูแลมากกว่า forward เสียอีก และข้อความ
   `reason` ระบุ "(local-in)/(local-out)" ชัดเจนอยู่แล้ว → **ต้องแจ้งเจ้าของโปรเจกต์ตอนทดสอบ**
   ถ้าไม่ต้องการ ให้เติม `chain=forward` ที่ `dashboardService.getRecentLogs` เป็นการแก้บรรทัดเดียว
8. **`GetAll()` copy ทั้ง buffer ทุก request** — ที่ capacity 10,000 = ~1.8 MB memcpy + กด
   RLock ต่อ 1 คำขอ ยอมรับได้สำหรับการใช้งานจริง (ไม่กี่ request/นาที) แต่ **ห้าม**
   ทำ polling ถี่ๆ บน endpoint นี้ และห้ามเรียก `GetAll()` ในลูป/ต่อ 1 entry
   (ถ้าอนาคตพบว่าเป็นคอขวด ค่อยเพิ่มเมธอด `Filter(pred, cursor, limit)` ที่ทำงานใต้ RLock
   โดยไม่ copy — เป็น optimization แยก ไม่ทำในแผนนี้)
9. **เพดาน 5,000 แถวใน DOM** — ตารางไม่มี virtualization ถ้าผู้ใช้กดเลื่อนรัวจนครบ 10 หน้า
   หน้าจะเริ่มหน่วงบนเครื่องช้า → ต้องมีเพดาน `maxRows` จริง (T-12 ข้อ 7) และตัด **ท้าย**
   ลิสต์ ไม่ใช่หัว (หัวคือของใหม่ที่ผู้ใช้ต้องการเห็นที่สุด)
10. **NFLOG group 101 ต้องไม่ถูกใช้โดยโปรแกรมอื่นบนบอร์ด** — ถ้าเครื่องผู้ใช้มี
    `ulogd`/`suricata` ที่ผูก group 101 อยู่ ทั้งสองฝั่งจะได้ event ปนกัน (kernel ส่งให้
    ทุก listener ของ group นั้น) → ระหว่างทดสอบบนบอร์ดจริงให้ตรวจ
    `ss -f netlink | grep nflog` / ไม่มี ulogd รันอยู่ ก่อนสรุปว่า log ผิดปกติ
11. **ทดสอบบนบอร์ดจริง = ยังมีความเสี่ยงล็อกตัวเอง** แม้แผนนี้ไม่แตะ verdict — เพราะ T-02
    แก้ไฟล์เดียวกับที่สร้างกฎทุกข้อ พลาดนิดเดียว (เช่นวาง `localLogExpr` ผิดตำแหน่ง หรือ
    ทำ `expr.Verdict` หายจากกฎ Admin Access) = เข้าเว็บ/SSH ไม่ได้ → ทดสอบเมื่อเข้าถึงบอร์ด
    ทางกายภาพได้เท่านั้น และเปิด SSH session ค้างไว้อีกหน้าต่างเสมอ
12. **`install.sh` ไม่ต้องแก้** — ใช้ `cap_net_admin` เดิม ไม่มี Polkit/sudoers เพิ่ม
    (NFLOG group ใหม่ไม่ต้องขอสิทธิ์เพิ่ม) → อัปเกรดด้วยการเปลี่ยนไบนารีอย่างเดียว
13. **`netlink_monitor.go` ไม่เกี่ยว** — ดูแล route/interface ไม่ยุ่งกับ nftables/NFLOG
    และลำดับ startup ใน `main.go` ไม่เปลี่ยน (แค่เพิ่ม goroutine watcher อีกตัวข้าง ๆ ตัวเดิม)
14. **`backend/internal/api/dist/openapi.yaml` เป็นไฟล์ที่ `build.sh` copy มา** — ห้ามแก้มือ
    ให้แก้ `docs/openapi.yaml` + `frontend/public/openapi.yaml` แล้ว build ใหม่

---

## 7. Checklist สรุป (Definition of Done)

- [ ] `backend/internal/model/types.go` — `FirewallLog.Chain`
- [ ] `backend/internal/kernel/real_traffic_log.go` — `LocalNflogGroup=101`, `watchGroup`,
      `WatchLocalTraffic`, `parseNflogAttr` แยก chain/reason ตาม prefix
- [ ] `backend/internal/kernel/interfaces.go` — `TrafficLogManager.WatchLocalTraffic`
- [ ] `backend/internal/kernel/real_firewall.go` — `localLogExpr`, ย้าย printk→NFLOG ทุกจุด
      ของ input/output (ยกเว้น AUDIT), เพิ่ม limit ให้ AUDIT + final drop, ยุบ log split
- [ ] `backend/internal/kernel/mock.go` — mock ครบ 3 chain + `WatchLocalTraffic`
- [ ] `backend/internal/kernel/policy_chain_test.go` + `real_traffic_log_test.go` — เทสต์ใหม่
- [ ] `backend/internal/logs/ringbuffer.go` — `Capacity()`
- [ ] `backend/cmd/pigate/main.go` — capacity 10,000, RFC3339Nano, watcher ตัวที่สอง
- [ ] `backend/internal/api/handlers.go` — cursor+chain ใน `/logs/traffic`, `limit` ใน `/dashboard/logs`
- [ ] `backend/internal/api/handlers_test.go` (หรือไฟล์ใหม่) — เทสต์ pagination/filter
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` (ตรงกันทั้งสองไฟล์)
- [ ] `frontend/src/services/trafficLogService.ts` + `dashboardService.ts` — chain/cursor + mock
- [ ] `frontend/src/hooks/useLiveLogs.ts` (`maxRows`) + `usePaginatedLiveLogs.ts` (ใหม่)
- [ ] `frontend/src/components/logs/TrafficLogPage.tsx` (ใหม่) + `pages/ForwardTraffic.tsx` (wrapper)
- [ ] `frontend/src/pages/LocalTraffic.tsx` (ใหม่) + `App.tsx` + `app-sidebar.tsx` + `site-header.tsx`
- [ ] `docs/tech_stack_design.md` §8/§9 + README Feature Status
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน

**ทดสอบรวมท้ายแผน (ทำครั้งเดียวหลังทุก Task เสร็จ):**

- [ ] **mock mode** (`-mock=true`): หน้า Forward Traffic โหลดหน้าแรก 500 แถว, เลื่อนถึงล่าง
      แล้วโหลดเพิ่มอัตโนมัติ, ไม่มีแถวซ้ำ/ข้าม, สุดท้ายขึ้น "ไม่มีข้อมูลเก่ากว่านี้แล้ว"
- [ ] mock mode: หน้า Local Traffic แสดงทั้ง input/output, dropdown สลับ input only/output only/
      ทั้งคู่ ทำงานถูก และการสลับ **reset** ลิสต์+cursor แล้วโหลดหน้าแรกใหม่
- [ ] ระหว่างเลื่อนอยู่หน้าที่ 3-4 มี entry ใหม่ไหลเข้าทาง SSE → **prepend ด้านบน** โดยแถวที่
      โหลดมาแล้วไม่หาย และไม่มี id ซ้ำ
- [ ] กด Pause → SSE หยุด แต่ยัง Load more ได้; กด Resume → ต่อสตรีมและแถวเดิมยังอยู่
- [ ] ตัดเน็ต/รีสตาร์ต backend ให้ SSE reconnect ระหว่างเลื่อนอยู่หน้าลึกๆ → หน้าแรกถูก merge
      เข้าหัว ไม่ล้างแถวเก่าที่โหลดไว้
- [ ] ใส่คำค้นที่ match เฉพาะ entry เก่ามาก (อยู่ลึกกว่า 500 แถวแรก) → ต้องเจอ
      (พิสูจน์ว่า "กรองก่อน–ตัดทีหลัง" ถูกต้อง)
- [ ] Clear log → ทุกแท็บที่เปิดอยู่ (Dashboard/Forward/Local) ล้างพร้อมกันและ pagination reset
- [ ] `curl '…/api/logs/traffic?limit=3'` ซ้ำๆ ด้วย cursor ที่ได้ → ไล่ครบทุกแถว, ส่ง
      `beforeId` มั่ว + `beforeTime` จริง → fallback ถูกต้อง, ส่ง `chain=bogus` → 400
- [ ] client เก่า (`GET /api/logs/traffic?action=DROP&limit=200` ไม่มี chain/cursor) ยังได้ผลลัพธ์
- [ ] **บอร์ดจริง** (เข้าถึงทางกายภาพได้ + SSH ค้างไว้อีกหน้าต่าง): apply กฎแล้ว
      หน้าเว็บ/SSH ยังเข้าได้ตามปกติ (ยืนยันว่า verdict ไม่หายจากกฎ Admin Access)
- [ ] บอร์ดจริง: `journalctl -k -f` **ไม่มี** บรรทัด `[PiGate] INP ACCEPT/DROP` และ
      `[PiGate] OUT ...` ไหลอีกต่อไป (เหลือได้เฉพาะ `INP AUDIT` แบบจำกัดอัตรา)
- [ ] บอร์ดจริง: ping/ssh/เปิดหน้าเว็บเข้าบอร์ด → เห็น entry chain `input` ในหน้า Local Traffic
      แบบเรียลไทม์; ทราฟฟิกขาออกของบอร์ด (เช่น DNS/NTP) ที่มีกฎเปิด Log → เห็น chain `output`
- [ ] บอร์ดจริง: ทราฟฟิก LAN↔WAN ยังขึ้นในหน้า Forward Traffic เหมือนเดิม **และไม่มี entry
      ของ input/output ปนเข้ามา** (ทั้งจาก snapshot และจาก SSE)
- [ ] บอร์ดจริง: `ps_mem`/`/proc/<pid>/status` — RSS ของ pigate เพิ่มไม่เกิน ~10 MB
      หลัง buffer เต็ม 10,000 รายการ
