# แผนงาน: Persisted Rule Endpoints (ส่วนขยายของ PR #142 / issue #141)

สถานะ: **เจ้าของโปรเจกต์อนุมัติแล้ว (2026-08-14)** → ส่งให้ ai-developer ทำทีละ Task → ทดสอบรวมทีเดียวโดย ai-qa

Branch: `feat/fqdn-retry-and-monitored-counters` (branch เดิมของ PR #142 — **ห้ามเปิด branch ใหม่**,
ห้าม commit ลง main; โค้ดทั้งหมดเข้า PR เดิม)

ข้อตัดสินใจที่อนุมัติแล้วทั้งหมด (เดิมอยู่ในหัวข้อ 7 "รอเจ้าของตัดสิน"):

1. **cap = `1000` แถวต่อ (กฎ, direction)** — เจ้าของเลือกค่านี้เอง (ข้อเสนอเดิมของแผนคือ 200) จึงเป็นค่า
   default ของ `monitored-endpoints-max-per-rule` และเป็นฐานของตารางงบประมาณในหัวข้อ 5 ที่คำนวณใหม่แล้ว
2. **ผูกกับ Switch "Monitor" เดิม ไม่สร้างสวิตช์ใหม่** — ตามข้อเสนอ E-D2
3. **คงข้อจำกัด "กฎต้องเปิด Log ก่อนถึงจะมีข้อมูล Endpoints"** พร้อมปุ่มลัดเปิด Log ใน Drawer
   (ไม่เปิดให้อัตโนมัติ) — ตามข้อเสนอ E-D7
4. **เริ่มนับจากศูนย์ตอนเปิด Monitor ครั้งแรก ไม่ backfill จาก ring buffer** — ตามข้อเสนอ E-D6/O-4 เดิม
   (ไม่มี Task backfill ในแผนนี้)

เอกสารที่ต้องอ่านคู่กัน: `docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md` (issue #141 — pattern
`PolicyCounterStore` / epoch guard / config key / migration ที่แผนนี้ต่อยอดทั้งหมด) และ
`docs/ref/todo/firewall-rule-matched-endpoints-plan.md` (ที่มาของฟีเจอร์ Endpoints ปัจจุบัน)

**ทำไมแยกไฟล์แทนที่จะต่อท้ายแผน #141**: แผน #141 ถูกอนุมัติและปิดไปแล้ว (มี "บันทึกการอนุมัติ" ท้ายไฟล์)
และ ai-qa ใช้ Final Acceptance ของไฟล์นั้นเป็นเกณฑ์ทดสอบชุดที่หนึ่งอยู่ การเติมหัวข้อใหม่เข้าไปจะทำให้ QA
แยกไม่ออกว่าต้องทดสอบชุดไหน แผนนี้จึงเป็นไฟล์แยกที่มี Task list/Final Acceptance ของตัวเอง แต่ลงโค้ดใน
**branch และ PR เดียวกัน**

---

## 0. ขอบเขต

ทำให้ข้อมูล "Endpoints ที่ตรงกับกฎนี้" (ตาราง Top Source / Top Destination / Top Service ใน
`RuleStatsDrawer.tsx`) **ไม่หายเมื่อ reapply / restart / ล้าง Traffic Log** โดยเก็บลง SQLite แบบเดียวกับ
Monitor counter ของ #141

ในขอบเขต:

1. เก็บ **ทุก unique key ที่เคยเห็น** (ไม่ใช่ top-N ชั่วคราว) ต่อกฎ พร้อม `count` / `first_seen_at` / `last_seen_at`
2. นับเป็น **จำนวนครั้ง (count of log entries)** เหมือนกลไกเดิมทุกประการ — ไม่ใช่ bytes/packets ต่อ IP
   (เจ้าของตัดตัวเลือก nftables named set/meter ต่อ IP ออกไปแล้ว)
3. **cap ต่อกฎ + LRU eviction** กันกฎที่หันหน้าออกอินเทอร์เน็ต (port-forward / WAN input) โดน scan
   จนแถวโตไม่หยุด
4. config key ใหม่สำหรับ cap ตาม pattern file-only key ของ #141

นอกขอบเขต (ยืนยัน): time-series/กราฟย้อนหลังต่อ IP, retention ตามเวลา (เช่นลบของเก่ากว่า 30 วัน),
per-IP bytes/packets, GeoIP/threat intel, การ export ข้อมูลชุดนี้ไปกับ backup, การ backfill ข้อมูลย้อนหลัง
จาก ring buffer ตอนเปิด Monitor

---

## 1. สภาพปัจจุบัน (จากการสำรวจโค้ดจริงบน branch นี้)

| จุด | ไฟล์ | สรุป |
|---|---|---|
| ผลิต log entry | `backend/cmd/pigate/main.go` `stampAndPush` (บรรทัด ~521-535) | closure เดียวที่ NFLOG watcher **ทั้งสองกลุ่ม** (forward group 100 / local group 101) เรียก: stamp `ID`/`Time`/`RuleName` → `ringBuffer.Add(entry)` → `statisticsService.RecordFirewallLog(entry)` |
| pattern hook แบบ O(1) ที่มีอยู่แล้ว | `backend/internal/service/statistics.go` `RecordFirewallLog` (บรรทัด 161-200) | รันบน NFLOG read loop โดยตรง จึงบังคับว่า **O(1), ไม่ block, ไม่ทำ I/O, ไม่ panic**; ใช้ admission cap แบบ `if _, exists := m[k]; exists \|\| len(m) < max { m[k]++ }` (บรรทัด 193-196) |
| เก็บ endpoints ปัจจุบัน | `backend/internal/logs/ringbuffer.go` `AggregateByRule` (บรรทัด 178-224) | สแกน ring buffer ทั้งก้อนต่อ 1 request, tally `Sources`/`Dests`/`Services` (key = `"PROTO/PORT"`), ข้าม `Src/Dest == "-"` และ `RuleID == ""` |
| อ่าน/จัดอันดับ | `backend/internal/service/policy_endpoints.go` `GetRuleEndpoints` | เรียก `AggregateByRule` ครั้งเดียว → `topEndpointCounts` (sort count desc, key asc, ตัดที่ `limit` 1..50 default 10) → resolve ชื่อครั้งเดียวแบบ batch (`newAddrMatcher` / `LookupHostnames` / `domainLookup`) |
| resolve ชื่อ IP | `backend/internal/service/policy_endpoint_labels.go` | `addrMatcher` เลือก Address Object ที่ range แคบที่สุด, pure function, สร้างใหม่ทุก request |
| route | `backend/internal/api/router.go` บรรทัด 124-128 | `authRoute("GET /api/policies/{id}/endpoints")` — **แผนนี้ไม่เพิ่ม route ใหม่เลย** ใช้ endpoint เดิม เปลี่ยนแค่แหล่งข้อมูล + เพิ่ม field |
| pattern persist ของ #141 | `backend/internal/service/policy_counter_store.go` | `Load/Reload/Start(ctx)/Flush/FlushBeforeApply/Totals/SetMonitored/ResetRule`, ข้าม tick เมื่อ `repo.IsMockMode()`, **ไม่เขียน DB เมื่อ delta เป็นศูนย์** |
| ตาราง counter ของ #141 | `backend/internal/db/connection.go` บรรทัด 318-325 | `policy_rule_counters(policy_id PK, bytes, packets, started_at, updated_at)` + FK `ON DELETE CASCADE` ไป `firewall_policies` |
| repo ของ #141 | `backend/internal/db/repository.go` บรรทัด 1111-1274 | `GetMonitoredPolicyIDs` / `SetPolicyMonitored` (transaction เดียว: UPDATE flag + INSERT OR IGNORE / DELETE row) / `GetPolicyRuleCounters` / `AddPolicyRuleCounterDeltas` / `ResetPolicyRuleCounter` |
| UI | `frontend/src/components/policy/RuleStatsDrawer.tsx` | มีบล็อก "เก็บสถิติสะสม (Monitor)" (Switch + ยอดสะสม + ปุ่มรีเซ็ต, confirm ทั้งตอนปิดและตอนรีเซ็ต) และบล็อก "Endpoints ที่ตรงกับกฎนี้" (refetch ทุก 10 วิขณะเปิด) แยกกันอยู่แล้ว + กล่อง "ข้อจำกัด" 8 ข้อ |
| config file-only key | `backend/internal/config/config.go` | pattern ครบ 6 จุด: struct field → `Defaults()` → const `key…` → `orderedKeys` (**ต่อท้ายเสมอ**) → `applyKey()` → `valueFor()` + range clamp+warn ใน `Resolve()` |

ข้อสรุปสำคัญจากการสำรวจ: **ข้อมูล endpoints มีแหล่งเดียวคือ NFLOG** — nft counter ต่อกฎให้แค่
bytes/packets รวม ไม่มีการแยกราย IP ดังนั้นทุกอย่างในแผนนี้ต้องพึ่ง log entry ที่ไหลผ่าน `stampAndPush`

---

## 2. การตัดสินใจเชิงออกแบบ (อนุมัติแล้วทั้งหมด)

### E-D1 — จุด hook: `stampAndPush` ใน main.go (recorder แบบ O(1) ใน RAM) ไม่ใช่การสแกน ring buffer ตามรอบ

ทางเลือกที่พิจารณา:

| ทางเลือก | ผล |
|---|---|
| (ก) hook ที่ `stampAndPush` แล้วสะสมใน RAM → flush ลง SQLite ตามรอบ | **เลือกอันนี้** — นับได้ทุก entry ที่เข้ามาจริง ไม่มีทางนับซ้ำ/นับหาย, O(1) ต่อ event, ใช้ pattern เดียวกับ `StatisticsService.RecordFirewallLog` ที่พิสูจน์แล้วในโปรดักชัน |
| (ข) ให้ ticker เรียก `AggregateByRule` ทุก N วินาทีแล้ว diff กับค่าที่เก็บไว้ | ตกไป — ring buffer evict ของเก่าตลอดเวลา ทำให้ diff **นับหาย** เมื่อทราฟฟิกหนาแน่น (buffer หมุนเร็วกว่ารอบ diff) และ **นับซ้ำ** เมื่อ log ถูก `Clear()` ระหว่างรอบ ต้องเก็บ snapshot ทั้งก้อนไว้เทียบ = เปลืองทั้ง RAM และ CPU |
| (ค) เขียนลง SQLite ตรงๆ ทุก log entry | ตกไปทันที — เขียน SD card ทุกแพ็กเก็ตที่ถูก log ขัด `tech_stack_design.md` §8 อย่างรุนแรง และ block NFLOG read loop |

ข้อบังคับของ recorder (เหมือน `RecordFirewallLog` เป๊ะ เพราะรันบน read loop เดียวกัน):
**O(1) ต่อ event, ไม่ทำ I/O, ไม่ query DB, ไม่ block, ไม่ panic** → รายชื่อกฎที่ต้องเก็บ (monitored set)
ต้องอยู่ใน RAM และถูกรีเฟรชโดย `PolicyCounterStore` (ตอน toggle และตอน flush) **ห้าม** เรียก
`repo.GetMonitoredPolicyIDs()` จากใน recorder เด็ดขาด

### E-D2 — ผูกกับ Switch "Monitor" เดิม ไม่สร้าง Switch ใหม่ (อนุมัติแล้ว)

**ใช้ Switch "Monitor" ตัวเดิมคุมทั้ง counter สะสมและ endpoints สะสม** เหตุผล:

1. **โมเดลความคิดเดียวกัน** — ผู้ใช้เข้าใจว่า "เปิด Monitor = เก็บสถิติของกฎนี้แบบถาวร" การมีสองสวิตช์ที่
   คำอธิบายใกล้เคียงกันมาก ("เก็บสถิติสะสม" กับ "เก็บ Endpoints ถาวร") บนหน้าจอเดียวกันสร้างความสับสน
   มากกว่าประโยชน์ที่ได้
2. **semantics พร้อมใช้อยู่แล้วและอนุมัติไปแล้ว** (D-6 ของแผน #141): เปิด → สร้างแถว + `started_at`,
   ปิด → ลบข้อมูลถาวร (มี confirm), รีเซ็ต → ศูนย์ + `started_at` ใหม่ (มี confirm), ลบกฎ → CASCADE
   แผนนี้แค่ขยายให้ครอบคลุมตาราง endpoints ด้วย ไม่มี semantics ใหม่ให้ผู้ใช้ต้องเรียนรู้เพิ่ม
3. **จุดบังคับเดียว** — `PolicyCounterStore` มี `GetMonitoredPolicyIDs()` เป็น source of truth อยู่แล้ว
   recorder ใช้ set เดียวกัน ไม่ต้อง query เพิ่ม ไม่ต้องมีคอลัมน์ที่สองที่อาจไม่ sync กัน
4. **คุมต้นทุนได้ตรงจุด** — opt-in ต่อกฎคือขอบเขตที่จำกัดจำนวนแถวจริงๆ (cap ต่อกฎเป็นด่านที่สอง)

trade-off ที่เจ้าของยอมรับแล้ว: ผู้ใช้ที่อยากได้แค่ bytes/packets สะสมจะได้ตาราง IP มาด้วย — ต้นทุนสูงสุด
ถูก cap ไว้แล้ว (≤ cap × 3 แถวต่อกฎ, ดู E-D4 และตารางงบประมาณหัวข้อ 5)

### E-D3 — Schema: ตารางเดียวครอบทั้ง src / dst / svc

```sql
CREATE TABLE IF NOT EXISTS policy_rule_endpoints (
    policy_id     TEXT NOT NULL,
    direction     TEXT NOT NULL CHECK(direction IN ('src', 'dst', 'svc')),
    endpoint_key  TEXT NOT NULL,   -- IP literal สำหรับ src/dst, "PROTO/PORT" สำหรับ svc
    count         INTEGER NOT NULL DEFAULT 0,
    first_seen_at TEXT NOT NULL,   -- RFC3339 UTC
    last_seen_at  TEXT NOT NULL,   -- RFC3339 UTC (ใช้เป็นคีย์ LRU)
    PRIMARY KEY (policy_id, direction, endpoint_key),
    FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_policy_rule_endpoints_lru
    ON policy_rule_endpoints(policy_id, direction, last_seen_at);
```

เหตุผลที่รวมเป็นตารางเดียวแทน 3 ตาราง: migration ชุดเดียว, flush ได้ใน transaction เดียว, routine
eviction เขียนครั้งเดียวใช้ได้ทั้งสามหมวด

หมายเหตุ:
- ตั้งชื่อคอลัมน์ `endpoint_key` ไม่ใช่ `key` เพื่อเลี่ยงความกำกวมกับคำสงวนในภาษา SQL อื่นๆ ที่อาจมาอ่าน
  ไฟล์ DB นี้ในอนาคต
- **ไม่มีคอลัมน์ `started_at` ในตารางนี้** — ใช้ `policy_rule_counters.started_at` ของกฎเดียวกันเป็น
  "เก็บมาตั้งแต่" ร่วมกัน เพราะทั้งคู่ผูกกับ Switch Monitor ตัวเดียวกันและเริ่ม/รีเซ็ตพร้อมกันเสมอ
- เพิ่มคอลัมน์ `endpoints_evicted INTEGER NOT NULL DEFAULT 0` ใน **`policy_rule_counters`** (ตารางที่
  #141 เพิ่งสร้าง) เพื่อนับสะสมว่าเคย evict ทิ้งไปกี่แถว — เป็นสัญญาณเดียวที่บอกผู้ใช้ได้ว่า "กฎนี้เจอ IP
  แปลกหน้าเยอะจนล้น cap" (ซึ่งคือเคส scan ที่เจ้าของกังวล) ต้องเพิ่มทั้งใน `CREATE TABLE` ตั้งต้นและ
  migration `ALTER TABLE ADD COLUMN` แบบเช็ค `sqlite_master.sql` ตาม pattern เดิม เพราะมีเครื่อง dev
  ที่สร้าง DB จาก branch นี้ไปแล้วก่อนหน้า

### E-D4 — cap + LRU eviction (cap = 1000 ต่อ (กฎ, direction) — เจ้าของกำหนด)

**cap เป็น "ต่อ (กฎ, direction)" ไม่ใช่ต่อกฎรวม** — เหตุผล: กฎ port-forward ตัวเดียวอาจมี source
เป็นพันแต่มี destination แค่ 1-2 และ service แค่ไม่กี่ตัว ถ้าใช้ cap รวม source ที่โดน scan จะกิน
โควตาจนเบียด dst/svc (ซึ่งเป็นข้อมูลที่มีประโยชน์กว่ามาก) หายไปหมด

| ค่า | ที่อนุมัติ | หมายเหตุ |
|---|---|---|
| default | **1000 แถว ต่อ (กฎ, direction)** | เจ้าของเลือกเอง (ข้อเสนอเดิมของแผนคือ 200) → worst case ต่อกฎ = 3 × 1000 = 3,000 แถว ดูตัวเลขทรัพยากรจริงในหัวข้อ 5 |
| ช่วงที่ยอมรับ | 20..5000 | 20 = โหมดประหยัดสุดสำหรับบอร์ดที่ SD เล็ก/เก่า, 5000 = เพดานที่ยัง flush จบใน transaction เดียวได้เร็ว |
| kill switch | `monitored-endpoints-enabled` default `true` | ปิดทั้งฟีเจอร์ได้โดยไม่ต้องไล่ปิด Monitor ทีละกฎ (pattern เดียวกับ `fqdn-refresh-enabled`) |

**นโยบาย eviction: LRU ตาม `last_seen_at` น้อยสุดก่อน** (tie-break: `count` น้อยกว่าไปก่อน แล้ว
`endpoint_key` น้อยกว่าไปก่อน เพื่อความ deterministic ในเทส)

ทำไม LRU ถึงตอบโจทย์เคส scan พอดี: IP ของ scanner มักเห็นครั้งเดียวแล้วหายไป `last_seen_at` จึงเก่าเร็ว
และถูกเขี่ยออกก่อน ส่วนโฮสต์จริงที่คุยกับกฎนี้เรื่อยๆ จะมี `last_seen_at` สดใหม่ตลอดจึงรอด — ตรงกับ
เจตนา "เก็บคนที่ยังคุยอยู่ ทิ้งคนที่ผ่านมาแล้วผ่านไป"

**จังหวะ evict: ตอน flush เท่านั้น ไม่ใช่ตอนรับ event** (ตอนรับ event ห้ามทำ I/O) และ**เฉพาะคู่
(กฎ, direction) ที่มีการแตะในรอบ flush นั้นและมีแถวเกิน cap เท่านั้น** เพื่อไม่ให้เกิด DELETE
โดยไม่จำเป็น (SD write cycle)

**ด่านที่ศูนย์ — admission cap ฝั่ง RAM**: recorder จำกัด key ต่อ (กฎ, direction) ที่ค่า cap เดียวกัน
(1000) โดยใช้ pattern เดิมของ `statistics.go` คือ *key ที่มีอยู่แล้วนับต่อได้เสมอ ส่วน key ใหม่รับเฉพาะเมื่อ
ยังไม่เต็ม* → RAM สูงสุดต่อกฎถูกล็อกไว้ที่ 3 × 1000 key ระหว่างสอง flush และเป็นตัวจำกัดจำนวน UPSERT
สูงสุดต่อรอบ flush ไปในตัว ผลข้างเคียงที่ยอมรับ: ในช่วง scan หนัก IP ใหม่บางส่วนจะถูกทิ้งตั้งแต่ RAM
(ไม่ได้เข้าไปแข่ง LRU) — ยอมรับได้เพราะข้อมูลชุดนี้เป็นเครื่องมือ troubleshooting ไม่ใช่หลักฐานเชิง
นิติวิทยาศาสตร์ และต้องเขียนไว้ในกล่อง "ข้อจำกัด" บน UI

### E-D5 — จังหวะ flush: เกาะรอบเดียวกับ `PolicyCounterStore` ไม่เพิ่ม ticker ใหม่

ใช้ ticker เดิมของ `PolicyCounterStore` (`monitored-counter-flush-interval-seconds`, default 300 วิ)
และ flush endpoints ภายใน `Flush()` ตัวเดียวกัน เหตุผล: ไม่เพิ่ม goroutine, ไม่เพิ่มจำนวนจังหวะที่แตะ SD,
และทำให้ "ยอด counter" กับ "ยอด endpoints" ของกฎเดียวกันเป็น snapshot ที่เวลาใกล้กันเสมอ

กฎเหล็กเดิมยังใช้: **ไม่มีอะไรจะเขียน = ไม่เปิด transaction เลย** (Caution 6 ของแผน #141)

`FlushBeforeApply()` จะ flush endpoints ไปด้วยโดยปริยาย (โค้ดเส้นทางเดียว) ซึ่งไม่จำเป็นเชิงตรรกะ
(nft flush ไม่กระทบข้อมูลที่มาจาก NFLOG) แต่ไม่มีผลเสียและทำให้มี code path เดียวให้ดูแล

### E-D6 — เส้นทางอ่าน: กฎที่ monitored อ่านจาก DB, กฎที่ไม่ monitored อ่านจาก ring buffer เหมือนเดิม (ห้ามบวกกัน)

**ห้ามรวมสองแหล่งด้วยการบวก count เด็ดขาด** — entry ที่อยู่ใน ring buffer ตอนนี้ ถูก recorder นับเข้า
ยอด persist ไปแล้วทุกตัว การบวกกัน = นับซ้ำ

ตรรกะที่ใช้ใน `GetRuleEndpoints`:

```
ถ้า rule.Monitored และ endpoints-enabled:
    source = "persisted"
    ยอด = (แถวใน policy_rule_endpoints)  +  (pending ที่ยังไม่ flush ใน recorder ของกฎนี้)
    collectingSince = policy_rule_counters.started_at ของกฎนี้
ไม่งั้น:
    source = "buffer"          // พฤติกรรมเดิมทุกประการ ไม่เปลี่ยน
    ยอด = AggregateByRule(...)
```

การบวก pending จาก RAM เข้าไปด้วยสำคัญมาก: ไม่งั้นผู้ใช้ที่เพิ่งเปิด Monitor จะเห็นตารางว่างเปล่าไปอีก
5 นาทีจนกว่าจะถึงรอบ flush แรก (คนจะคิดว่าฟีเจอร์พัง)

**เริ่มนับจากศูนย์ ไม่ backfill (อนุมัติแล้ว)**: ตอนเปิด Monitor ครั้งแรก ระบบ **ไม่** ดึงข้อมูลย้อนหลังจาก
ring buffer มา seed ลง DB ผลที่ผู้ใช้จะเห็นคือตารางเริ่มนับจากศูนย์ ณ วินาทีที่เปิด (ข้อมูลย้อนหลังที่ยังอยู่
ใน Traffic Log จะไม่ปรากฏในโหมด persisted) เหตุผลที่ไม่ backfill: `count` จะกลายเป็นค่าผสมสองยุค และ
`first_seen_at` จะมาจากข้อมูลที่บางส่วนถูก evict ออกจาก buffer ไปแล้ว = ตัวเลขที่อธิบายให้ผู้ใช้เข้าใจไม่ได้
UI ต้องสื่อสารเรื่องนี้ผ่านบรรทัด "เก็บมาตั้งแต่ {collectingSince}" ให้ชัด (ดู E-12)

การจัดอันดับ/ตัด top-N ยังใช้กติกาเดิม (count desc, key asc, `limit` 1..50 default 10) และ
**query จาก DB ต้อง `ORDER BY count DESC, endpoint_key ASC LIMIT ?` ในตัว SQL** ไม่ใช่ดึงทั้ง 3,000 แถว
มาเรียงใน Go (drawer refetch ทุก 10 วินาทีขณะเปิดอยู่ — ต้องเบา) พร้อมอีกหนึ่ง query แบบ
`GROUP BY direction` เพื่อได้ `uniqueSources/uniqueDestinations/uniqueServices` = 4 query เล็กต่อ request
ทั้งหมดเป็นการ **อ่าน** ไม่แตะ write cycle ของ SD

field ใหม่ใน response (`model.PolicyRuleEndpoints`):

| field | ความหมาย |
|---|---|
| `source` | `"persisted"` หรือ `"buffer"` — ให้ UI ติดป้ายบอกผู้ใช้ตรงๆ ว่าตัวเลขนี้มาจากไหน |
| `collectingSince` | RFC3339 UTC, มีค่าเฉพาะ `source == "persisted"` |
| `capped` | true เมื่อ direction ใดก็ตามมีแถวเท่ากับ cap (กำลัง evict อยู่) |
| `evicted` | ค่าสะสมจาก `policy_rule_counters.endpoints_evicted` |
| `maxPerDirection` | ค่า cap ที่มีผลอยู่ เพื่อให้ UI แสดงข้อความได้โดยไม่ต้อง hardcode |

field เดิมทุกตัวคงความหมายเดิม; `bufferOldestAt`/`scannedEntries` จะเป็นค่าว่าง/0 เมื่อ
`source == "persisted"` (ไม่ได้สแกน buffer) — ระบุใน openapi ให้ชัด

### E-D7 — ข้อจำกัด "ต้องเปิด Log ที่กฎนั้นก่อน" ยังคงอยู่ (อนุมัติแล้ว ไม่ยกเลิก)

ทางเลือกเดียวที่จะได้ข้อมูลราย IP โดยไม่พึ่ง log คือสร้าง nftables named set / meter ต่อกฎต่อ IP ซึ่ง
**เจ้าของตัดออกไปแล้ว** (และมันจะเปลี่ยนโครงสร้าง ruleset ที่ `tech_stack_design.md` §4.3 ล็อกไว้)
ดังนั้น:

- `logEnabled == false` → ตารางว่าง ไม่ใช่ error (เหมือนเดิม)
- **สิ่งที่ต้องปรับปรุงคือความชัดเจนของ UI**: เมื่อกฎเปิด Monitor แต่ยังไม่เปิด Log ต้องขึ้นคำเตือนใน
  บล็อก Endpoints ว่า "เปิด Monitor แล้วแต่ยังไม่ได้เปิด Log จึงยังไม่มีข้อมูล Endpoints ให้เก็บ"
  พร้อมปุ่มลัด "เปิด Log ให้กฎนี้" ที่เรียก `policyService.toggleLog(id)` (endpoint ที่มีอยู่แล้ว) —
  ไม่เปิด Log ให้อัตโนมัติ เพราะการเปิด Log เปลี่ยน nft ruleset จริงและเพิ่มภาระ NFLOG ซึ่งเป็นการ
  ตัดสินใจที่ผู้ใช้ควรกดเอง

### E-D8 — semantics ครบทุกกรณี (ต่อยอด D-6 ของแผน #141)

| เหตุการณ์ | ผลกับ endpoints ที่ persist |
|---|---|
| เปิด Monitor | เริ่มเก็บทันที (`started_at` = now ร่วมกับ counter), **ไม่ย้อนหลังเอาของใน buffer มาใส่** (E-D6) |
| ปิด Monitor | **ลบแถว `policy_rule_endpoints` ของกฎนั้นทิ้งใน transaction เดียวกับที่ลบแถว counter** (FK cascade ผูกกับ `firewall_policies` ไม่ใช่กับ `policy_rule_counters` จึงไม่ทำงานเอง — ต้อง DELETE ตรงๆ) + ล้าง pending ใน RAM ของกฎนั้น; ข้อความ confirm เดิมต้องแก้ให้ครอบคลุมว่า "ยอดสะสมและรายการ IP/Service ทั้งหมดจะถูกลบถาวร" |
| กดรีเซ็ตค่า | ลบแถว endpoints ทั้งหมดของกฎนั้น + zero counter + `started_at` = now + `endpoints_evicted` = 0 + ล้าง pending ใน RAM; ข้อความ confirm ต้องบอกด้วยว่ารายการ Endpoints จะถูกล้างด้วย |
| ลบกฎ | หายตาม FK `ON DELETE CASCADE` ทั้งสองตาราง |
| ปิดใช้งานกฎ (status=false) | **ไม่ลบข้อมูล** — กฎแค่ไม่ถูกสร้างใน nft จึงไม่มี entry ใหม่เข้ามาเอง |
| แก้ไขกฎ (เปลี่ยน source/dest/service) | ไม่ลบข้อมูล — id เดิม ยอดเดิมเดินต่อ (ตรงกับพฤติกรรมของ counter สะสมใน #141) |
| ล้าง Traffic Log | **ไม่กระทบเลย** — นี่คือเป้าหมายหลักของแผนนี้ |
| reapply / restart / reboot | ไม่กระทบ (เสียได้อย่างมาก 1 รอบ flush ≈ 5 นาที ถ้าไฟดับกะทันหัน) |
| import backup | `policy_rule_endpoints` เป็น runtime data → **ห้าม export**; หลัง import ต้องเรียก `Reload()` และ **ล้าง pending + monitored set ใน recorder** ให้ตรงกับ DB ชุดใหม่ |
| mock mode | ไม่เขียน DB (ข้าม flush เหมือน `PolicyCounterStore.run` ที่ทำอยู่แล้ว) |

### E-D9 — config key ใหม่ 2 ตัว (file-only ไม่มี CLI flag)

| key | default | ช่วง | ความหมาย |
|---|---|---|---|
| `monitored-endpoints-enabled` | `true` | bool | kill switch ทั้งฟีเจอร์ (ปิดแล้ว recorder ไม่นับ, ไม่เขียน DB, และ `GetRuleEndpoints` กลับไปใช้ ring buffer ล้วนเหมือนก่อนแผนนี้) |
| `monitored-endpoints-max-per-rule` | **`1000`** | 20..5000 | cap แถวต่อ (กฎ, direction) ทั้งฝั่ง RAM และฝั่ง DB |

ทั้งคู่ **ต่อท้าย `orderedKeys`** (ห้ามแทรกกลาง — `pigate.conf` ที่ generate ไว้แล้วต้อง diff สะอาด)

---

## 3. Task list

> ทุก Task ทำบน branch `feat/fqdn-retry-and-monitored-counters` (branch เดิม ห้ามเปิดใหม่)
> **ห้าม commit ถ้าเจ้าของยังไม่สั่ง** และ **ไม่ต้องทดสอบทีละ Task** — ทำครบทุก Task ก่อน แล้วค่อยเทสรวม
> ตามหัวข้อ 6

```json
[
  {
    "task_id": "E-01",
    "title": "config: เพิ่ม key monitored-endpoints-enabled / monitored-endpoints-max-per-rule",
    "layer": "service",
    "files": ["backend/internal/config/config.go", "backend/internal/config/config_test.go"],
    "instruction": "เพิ่ม field MonitoredEndpointsEnabled bool และ MonitoredEndpointsMaxPerRule int ใน Config พร้อม key file-only (ไม่มี CLI flag) ชื่อ monitored-endpoints-enabled / monitored-endpoints-max-per-rule ทำครบทุกจุดตาม pattern ของ monitored-counter-flush-interval-seconds ที่เพิ่งเพิ่มใน PR นี้: struct field + doc comment (อ้าง docs/ref/todo/persisted-rule-endpoints-plan.md E-D9), Defaults() = true / **1000**, const key, ต่อท้าย orderedKeys ให้อยู่หลัง monitored-counter-flush-interval-seconds (ห้ามแทรกกลาง), applyKey(), valueFor() และ range clamp+warn ใน Resolve() ช่วง 20..5000 (bool ไม่ต้อง clamp) เพิ่ม unit test: parse ค่าถูก, ค่านอกช่วงถูก clamp กลับเป็น 1000 พร้อม warning, Write() แล้วอ่านกลับได้ค่าเดิม, KnownKeys() มี 2 key ใหม่และอยู่ท้ายลิสต์",
    "acceptance": ["go build ผ่าน", "go test ./internal/config/... ผ่าน", "Defaults().MonitoredEndpointsMaxPerRule == 1000", "KnownKeys() มี 2 key ใหม่ต่อท้ายลิสต์"],
    "depends_on": []
  },
  {
    "task_id": "E-02",
    "title": "db: ตาราง policy_rule_endpoints + index LRU + คอลัมน์ endpoints_evicted",
    "layer": "db",
    "files": ["backend/internal/db/connection.go"],
    "instruction": "1) เพิ่ม CREATE TABLE IF NOT EXISTS policy_rule_endpoints (policy_id TEXT NOT NULL, direction TEXT NOT NULL CHECK(direction IN ('src','dst','svc')), endpoint_key TEXT NOT NULL, count INTEGER NOT NULL DEFAULT 0, first_seen_at TEXT NOT NULL, last_seen_at TEXT NOT NULL, PRIMARY KEY (policy_id, direction, endpoint_key), FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE) วางถัดจาก policy_rule_counters (บรรทัด ~318-325)\n2) เพิ่ม CREATE INDEX IF NOT EXISTS idx_policy_rule_endpoints_lru ON policy_rule_endpoints(policy_id, direction, last_seen_at) ในกลุ่ม CREATE INDEX ที่มีอยู่\n3) เพิ่มคอลัมน์ endpoints_evicted INTEGER NOT NULL DEFAULT 0 ให้ policy_rule_counters ทั้งใน CREATE TABLE ตั้งต้น และเพิ่ม migration แบบเช็ค SELECT sql FROM sqlite_master WHERE name='policy_rule_counters' ว่ามีคำว่า endpoints_evicted แล้วหรือยัง ก่อน ALTER TABLE ADD COLUMN (จำเป็นเพราะมีเครื่อง dev ที่สร้าง DB จาก branch นี้ไปก่อนแล้ว)\n4) ห้ามใส่ retention/cleanup job ตามเวลาใดๆ ทั้งสิ้น (นอกขอบเขต)",
    "acceptance": ["go build ผ่าน", "รัน InitDB กับ DB เดิมที่มีตาราง policy_rule_counters อยู่แล้วไม่ error", "รัน InitDB 2 ครั้งติดกันไม่ error (idempotent)"],
    "depends_on": []
  },
  {
    "task_id": "E-03",
    "title": "model: type สำหรับ endpoint ที่ persist + field ใหม่ใน PolicyRuleEndpoints",
    "layer": "model",
    "files": ["backend/internal/model/types.go"],
    "instruction": "1) เพิ่ม type PersistedEndpoint struct { RuleID string; Direction string; Key string; Count int; FirstSeenAt string; LastSeenAt string } (Direction เป็น 'src'|'dst'|'svc'; FirstSeenAt/LastSeenAt เป็น RFC3339 UTC)\n2) เพิ่ม const EndpointDirectionSrc/Dst/Svc = \"src\"/\"dst\"/\"svc\" ไว้ใช้ร่วมกันทุกเลเยอร์ ห้ามเขียน literal กระจัดกระจาย\n3) เพิ่ม field ใน PolicyRuleEndpoints: Source string `json:\"source\"` (\"persisted\"|\"buffer\"), CollectingSince string `json:\"collectingSince,omitempty\"`, Capped bool `json:\"capped\"`, Evicted int `json:\"evicted\"`, MaxPerDirection int `json:\"maxPerDirection\"` พร้อม doc comment อธิบายความหมายและระบุว่าเมื่อ Source==\"persisted\" ค่า ScannedEntries/BufferOldestAt จะไม่มีความหมาย และข้อมูลเริ่มนับจากศูนย์ตอนเปิด Monitor (ไม่มี backfill)\n4) เพิ่ม EndpointsEvicted uint64 ใน MonitoredCounter (สะท้อนคอลัมน์ใหม่ของ E-02)",
    "acceptance": ["go build ผ่าน", "go test ./internal/model/... ผ่าน", "ไม่มีการเปลี่ยนชื่อ/ชนิดของ field เดิมใน PolicyRuleEndpoints"],
    "depends_on": []
  },
  {
    "task_id": "E-04",
    "title": "db repository: CRUD + LRU eviction ของ policy_rule_endpoints",
    "layer": "db",
    "files": ["backend/internal/db/repository.go", "backend/internal/db/repository_test.go"],
    "instruction": "เพิ่ม method ใหม่ (ทุก query ต้องใช้ parameter binding ห้ามต่อ string SQL เอง):\n1) AddPolicyEndpointDeltas(deltas []model.PersistedEndpoint, maxPerDirection int) (evicted map[string]int, err error) — ใน transaction เดียว: (ก) ต่อ 1 รายการทำ UPSERT: INSERT INTO policy_rule_endpoints (...) VALUES (...) ON CONFLICT(policy_id, direction, endpoint_key) DO UPDATE SET count = count + excluded.count, last_seen_at = excluded.last_seen_at (ห้ามแตะ first_seen_at ตอน update) (ข) หลัง upsert ครบ ให้ทำ eviction เฉพาะคู่ (policy_id, direction) ที่ถูกแตะในรอบนี้เท่านั้น โดย SELECT COUNT(*) ก่อน ถ้า <= maxPerDirection ให้ข้ามไปเลย (ห้ามสั่ง DELETE โดยไม่จำเป็น — SD write cycle) ถ้าเกิน จึง DELETE FROM policy_rule_endpoints WHERE policy_id=? AND direction=? AND endpoint_key IN (SELECT endpoint_key FROM policy_rule_endpoints WHERE policy_id=? AND direction=? ORDER BY last_seen_at ASC, count ASC, endpoint_key ASC LIMIT ?) โดย LIMIT = จำนวนที่เกิน แล้วสะสมจำนวนที่ลบลง evicted[policy_id] (ค) UPDATE policy_rule_counters SET endpoints_evicted = endpoints_evicted + ? สำหรับ policy ที่มีการ evict\n   **return ทันทีโดยไม่เปิด transaction ถ้า deltas ว่าง** ; ข้าม policy_id ที่ไม่มีอยู่จริง (FK error) โดยไม่ให้ทั้ง transaction ล้ม — วิธีที่ต้องใช้คือให้ผู้เรียกกรอง id ด้วย GetMonitoredPolicyIDs() มาก่อน (ทำใน E-06) และใน repo ให้ตรวจ error ของแต่ละ statement แล้ว log+ข้ามเฉพาะ FK constraint error\n   หมายเหตุประสิทธิภาพ: cap default = 1000 ต่อ (กฎ, direction) จึงมี UPSERT ได้สูงสุด ~3,000 แถวต่อกฎต่อรอบ flush — ต้องใช้ tx.Prepare ครั้งเดียวแล้ว Exec ซ้ำ (ห้าม Prepare ในลูป) และวนตามลำดับ sort เพื่อความ deterministic\n2) GetTopPolicyEndpoints(policyID string, direction string, limit int) ([]model.PersistedEndpoint, error) — SELECT ... ORDER BY count DESC, endpoint_key ASC LIMIT ? (เรียงใน SQL ห้ามดึงทั้งหมดมาเรียงใน Go)\n3) CountPolicyEndpoints(policyID string) (map[string]int, error) — SELECT direction, COUNT(*) ... GROUP BY direction\n4) DeletePolicyEndpoints(policyID string) error — DELETE ทั้งกฎ\n5) แก้ SetPolicyMonitored: ตอน monitored=false ให้ DELETE FROM policy_rule_endpoints WHERE policy_id=? **ใน transaction เดียวกัน** กับที่ลบแถว counter (E-D8)\n6) แก้ ResetPolicyRuleCounter: ให้ทำใน transaction เดียวกัน = zero counter + started_at/updated_at=now + endpoints_evicted=0 + DELETE FROM policy_rule_endpoints WHERE policy_id=?\n7) แก้ GetPolicyRuleCounters ให้ SELECT endpoints_evicted มาด้วย (clamp ค่าติดลบเป็น 0 พร้อม log warning เหมือน bytes/packets)\nเพิ่มเทสครอบคลุม: upsert สะสม count ถูกต้องและ first_seen_at ไม่ถูกเขียนทับ, eviction ตัดตัวที่ last_seen_at เก่าสุดจริงและเหลือพอดี cap, ไม่เกิด DELETE เมื่อยังไม่เต็ม, ปิด monitored แล้วแถว endpoints หายหมด, reset แล้วแถวหายหมดและ endpoints_evicted กลับเป็น 0, ลบ policy แล้ว cascade หายทั้งสองตาราง",
    "acceptance": ["go build ผ่าน", "go test ./internal/db/... ผ่าน", "ทุก query ใช้ parameter binding", "มีเทสพิสูจน์ลำดับ eviction แบบ LRU", "ไม่มีการเรียก Prepare ภายในลูป"],
    "depends_on": ["E-02", "E-03"]
  },
  {
    "task_id": "E-05",
    "title": "service: PolicyEndpointRecorder (RAM hook แบบ O(1) บน NFLOG read loop)",
    "layer": "service",
    "files": ["backend/internal/service/policy_endpoint_recorder.go", "backend/internal/service/policy_endpoint_recorder_test.go"],
    "instruction": "งาน SENSITIVE ในแง่ performance: โค้ดนี้รันบน NFLOG read loop โดยตรง (main.go stampAndPush) จึง **ห้าม block, ห้ามทำ I/O, ห้าม query DB, ห้าม panic** และต้องเป็น O(1) ต่อ event — ลอกข้อกำหนดและโครงจาก StatisticsService.RecordFirewallLog (statistics.go บรรทัด 161-200) ซึ่งเป็น hook พี่น้องกันบน closure เดียวกัน\n\nสร้าง type PolicyEndpointRecorder:\n- NewPolicyEndpointRecorder(enabled bool, maxPerDirection int) — maxPerDirection <= 0 ให้ fallback เป็นค่าคงที่ defaultMaxEndpointsPerDirection = 1000 (defense-in-depth เหมือน defaultMaxTrackedDenySources และต้องตรงกับ config.Defaults())\n- SetMonitoredRules(ids map[string]bool) — แทนที่ set ทั้งก้อนใต้ mutex (ผู้เรียกคือ PolicyCounterStore ใน E-06) และ **ลบ pending ของกฎที่หลุดออกจาก set ทิ้ง**\n- Record(entry model.FirewallLog) — return ทันทีถ้า !enabled, entry.RuleID == \"\" หรือ ruleID ไม่อยู่ใน monitored set; ไม่งั้น tally 3 หมวดใต้ mutex เดียว: src (ข้าม \"\" และ \"-\"), dst (ข้าม \"\" และ \"-\"), svc = entry.Proto + \"/\" + entry.Port (ข้ามเมื่อ Proto == \"\") — กติกาการข้ามต้องตรงกับ logs.RingBuffer.AggregateByRule เป๊ะ เพื่อให้ตัวเลขสองแหล่งเทียบกันได้\n- admission cap: ใช้ pattern `if _, exists := m[k]; exists || len(m) < maxPerDirection { ... }` ต่อ (rule, direction) — key เดิมนับต่อได้เสมอ key ใหม่รับเฉพาะตอนยังไม่เต็ม (ที่ default = 1000 key ต่อหมวด)\n- เก็บ count + firstSeen + lastSeen (ใช้ entry.Time ตามที่ stampAndPush stamp มาแล้ว ห้ามเรียก time.Now() ต่อ event)\n- Drain() []model.PersistedEndpoint — คืนทุก pending แล้วเคลียร์ map (ผู้บริโภครายเดียวคือ PolicyCounterStore)\n- Pending(ruleID string) map[string]map[string]model.PersistedEndpoint (หรือรูปแบบที่อ่านง่ายเทียบเท่า) — คืน copy ของ pending เฉพาะกฎเดียว ใช้โดย GetRuleEndpoints ใน E-07 เพื่อบวกยอดที่ยังไม่ flush ห้ามเคลียร์อะไร\n- ClearRule(ruleID string) — ทิ้ง pending ของกฎเดียว (ใช้ตอนปิด Monitor/รีเซ็ต)\n- Reset() — ทิ้ง pending ทั้งหมด (ใช้หลัง import backup)\nunit test: นับถูกต้องทั้ง 3 หมวด, กฎที่ไม่ได้ monitored ไม่ถูกนับเลย, admission cap ทำงาน (key ใหม่เกิน cap ถูกทิ้งแต่ key เดิมยังนับต่อ), Drain แล้วว่าง, ข้าม \"-\"/\"\" ตรงกับ AggregateByRule, และ -race ผ่านเมื่อ Record/Drain ทำงานพร้อมกัน",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run EndpointRecorder ผ่าน", "go test -race ผ่านสำหรับไฟล์นี้", "ไม่มี import ของ db/kernel ในไฟล์นี้ และไม่มีการเรียก time.Now() ใน Record"],
    "depends_on": ["E-03"]
  },
  {
    "task_id": "E-06",
    "title": "service: PolicyCounterStore flush endpoints ในรอบเดียวกัน + sync monitored set",
    "layer": "service",
    "files": ["backend/internal/service/policy_counter_store.go", "backend/internal/service/policy_counter_store_test.go"],
    "instruction": "ต่อยอดจากโค้ดที่ทำใน issue #141 (ห้ามเปลี่ยนพฤติกรรมเดิมของ counter สะสม)\n1) เพิ่ม field recorder *PolicyEndpointRecorder และ maxEndpointsPerDirection int พร้อม optional setter SetEndpointRecorder(r *PolicyEndpointRecorder, maxPerDirection int) (additive-setter pattern เดียวกับที่ PR นี้ใช้อยู่ ห้ามเปลี่ยน signature ของ NewPolicyCounterStore)\n2) ใน Flush(): หลังจัดการ counter เดิมเสร็จ ให้ (ก) เรียก recorder.Drain() (ข) กรองเฉพาะ rule id ที่อยู่ใน monitored set ที่เพิ่งอ่านมาแล้วในเมธอดเดียวกัน — **ห้ามเรียก GetMonitoredPolicyIDs ซ้ำรอบสอง** ให้ refactor ให้อ่านครั้งเดียวใช้ทั้งสองงาน (ค) ถ้ามีรายการเหลือ ให้เรียก repo.AddPolicyEndpointDeltas(deltas, s.maxEndpointsPerDirection) (ง) **ถ้าไม่มีรายการเลย ห้ามแตะ DB** (ห) หลังจบ ให้เรียก recorder.SetMonitoredRules(monitored) เพื่อให้ recorder รู้จักกฎที่ถูกเปิด/ปิด Monitor ระหว่างรอบ\n   สำคัญ: การกรองด้วย monitored set นี้คือด่านที่กันไม่ให้ delta ของกฎที่ถูกลบไปแล้วชน FK — อย่าข้าม\n3) SetMonitored(id, on): หลังเรียก repo.SetPolicyMonitored สำเร็จ ให้ recorder.SetMonitoredRules(...) ด้วยค่าใหม่ (หรืออัปเดต set เฉพาะ id นั้น) และเมื่อ on == false ให้ recorder.ClearRule(id) ; เมื่อ on == true **ห้าม seed/backfill ข้อมูลจาก ring buffer** (E-D6 — เริ่มนับจากศูนย์)\n4) ResetRule(id): หลัง repo.ResetPolicyRuleCounter สำเร็จ ให้ recorder.ClearRule(id) ด้วย (ยอด pending ที่ค้างต้องไม่ไหลกลับเข้ามาหลังรีเซ็ต)\n5) Load()/Reload(): เติม EndpointsEvicted เข้าแคช และใน Reload ให้เรียก recorder.Reset() + SetMonitoredRules(...) ตามสถานะ DB ใหม่หลัง import backup\n6) เพิ่ม EndpointsEvictedFor(id string) int (อ่านจากแคช) ให้ E-07 ใช้\nเทส: endpoints ถูก flush เฉพาะกฎที่ monitored, ไม่มี delta = ไม่เขียน DB, ปิด Monitor แล้ว pending ถูกทิ้ง, Reload แล้ว recorder ถูกล้าง, เปิด Monitor แล้วไม่มีการ seed ข้อมูลย้อนหลัง",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run PolicyCounter ผ่าน", "GetMonitoredPolicyIDs ถูกเรียกไม่เกิน 1 ครั้งต่อ Flush", "พฤติกรรมเดิมของ counter สะสมไม่เปลี่ยน (เทสเดิมของ #141 ยังผ่านทั้งหมด)"],
    "depends_on": ["E-04", "E-05"]
  },
  {
    "task_id": "E-07",
    "title": "service: GetRuleEndpoints อ่านจากข้อมูลที่ persist เมื่อกฎ monitored",
    "layer": "service",
    "files": ["backend/internal/service/policy_endpoints.go", "backend/internal/service/policy_endpoints_test.go"],
    "instruction": "1) เพิ่ม optional setter SetEndpointStore(store *PolicyCounterStore, recorder *PolicyEndpointRecorder, enabled bool, maxPerDirection int) บน PolicyStatsService (additive pattern เดียวกับ SetDomainLookup/SetCounterStore ห้ามแก้ signature ของ NewPolicyStatsService)\n2) ใน GetRuleEndpoints: หลังหา rule เจอแล้ว ให้แตกเป็นสองเส้นทางตาม E-D6 ของแผน\n   - ถ้า enabled && rule.Monitored && repo != nil: source=\"persisted\" — ดึง top-N ของแต่ละ direction ด้วย repo.GetTopPolicyEndpoints(rule.ID, dir, limit), ดึงจำนวนรวมด้วย repo.CountPolicyEndpoints(rule.ID) มาใส่ UniqueSources/UniqueDestinations/UniqueServices, **บวก pending จาก recorder.Pending(rule.ID) เข้าไปก่อนจัดอันดับ** แล้วค่อย sort/ตัด top-N ด้วย topEndpointCounts ตัวเดิม (แปลง pending+DB ให้เป็น map[string]logs.Counted เพื่อ reuse โค้ดจัดอันดับเดิม ห้ามเขียน sort ซ้ำสอง), set CollectingSince จากยอด counter store (started_at), Capped = มี direction ใดที่ count == maxPerDirection, Evicted = store.EndpointsEvictedFor(rule.ID), MaxPerDirection = ค่า cap (default 1000), ScannedEntries = 0 และ BufferOldestAt = \"\"\n   - ไม่งั้น: source=\"buffer\" — พฤติกรรมเดิมทุกบรรทัด ห้ามเปลี่ยน\n3) การ resolve ชื่อ (addrMatcher / LookupHostnames / domainLookup) และการคำนวณ Truncated/FromRule ต้องใช้โค้ดเส้นเดียวกันทั้งสองเส้นทาง — refactor ให้ส่วนที่แตกต่างมีแค่ \"ที่มาของ map นับ\" เท่านั้น เพื่อไม่ให้พฤติกรรมสองโหมดหลุดจากกันในอนาคต\n4) MatchedEntries เมื่อ source=\"persisted\" ให้ใช้ผลรวม count ของหมวด src (สอดคล้องกับความหมายเดิมที่สุดเท่าที่ทำได้) และเขียน doc comment กำกับว่ามันเป็นค่าประมาณเมื่อมาจากข้อมูล persist\nเทส: กฎ monitored ได้ source=persisted และตัวเลขรวม DB+pending ถูกต้อง, กฎไม่ monitored ได้ผลเท่าเดิมทุกประการ (regression), enabled=false บังคับให้กลับไปใช้ buffer, Capped/Evicted/MaxPerDirection ถูกต้อง",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run Endpoints ผ่าน (รวมเทสเดิมทั้งหมดของ firewall-rule-matched-endpoints)", "เทสเดิมของโหมด buffer ไม่ต้องแก้แม้แต่ไฟล์เดียว"],
    "depends_on": ["E-06"]
  },
  {
    "task_id": "E-08",
    "title": "main.go: wiring recorder เข้า stampAndPush และ store",
    "layer": "service",
    "files": ["backend/cmd/pigate/main.go"],
    "instruction": "1) สร้าง endpointRecorder := service.NewPolicyEndpointRecorder(cfg.MonitoredEndpointsEnabled, cfg.MonitoredEndpointsMaxPerRule) **ก่อน** บล็อก stampAndPush (บรรทัด ~521)\n2) ใน stampAndPush เพิ่มบรรทัด endpointRecorder.Record(entry) **ต่อท้ายสุด** ถัดจาก statisticsService.RecordFirewallLog(entry) พร้อม comment อธิบายข้อกำหนด O(1)/non-blocking/panic-free เหมือน comment ที่มีอยู่ (ห้ามวางไว้ก่อน ringBuffer.Add)\n3) policyCounterStore.SetEndpointRecorder(endpointRecorder, cfg.MonitoredEndpointsMaxPerRule) และ policyStatsService.SetEndpointStore(policyCounterStore, endpointRecorder, cfg.MonitoredEndpointsEnabled, cfg.MonitoredEndpointsMaxPerRule) วางกลุ่มเดียวกับ setter อื่นๆ ของสองตัวนี้\n4) หลัง policyCounterStore.Load() สำเร็จ ให้ prime monitored set ของ recorder หนึ่งครั้ง (อ่าน repo.GetMonitoredPolicyIDs()) เพื่อไม่ให้ช่วง 5 นาทีแรกหลังบูตนับไม่ครบ — error แค่ log warning ห้ามทำให้ boot ล้ม\n5) ห้ามเพิ่ม goroutine/ticker ใหม่ (ใช้รอบ flush เดิมของ policyCounterStore)",
    "acceptance": ["go build ผ่าน", "รัน ./pigate-backend -mock=true แล้วบูตผ่านไม่ panic", "ไม่มี ticker/goroutine ใหม่เพิ่มใน main.go"],
    "depends_on": ["E-01", "E-07"]
  },
  {
    "task_id": "E-09",
    "title": "backup: ยืนยันว่า policy_rule_endpoints ไม่ถูก export และ recorder ถูกล้างหลัง import",
    "layer": "service",
    "files": ["backend/internal/service/backup.go", "backend/internal/service/backup_test.go"],
    "instruction": "1) ตรวจและยืนยันด้วยเทสว่าไฟล์ export **ไม่มี** ข้อมูลจาก policy_rule_endpoints และ policy_rule_counters (runtime data ทั้งคู่ ตาม Caution 7 ของแผน #141)\n2) จุดที่เรียก PolicyCounterStore.Reload() หลัง import สำเร็จ (ทำไว้แล้วใน T-12 ของ #141) ต้องล้าง recorder ด้วย — ตรวจว่า Reload() ที่แก้ใน E-06 ทำครบ ถ้ายังไม่ครบให้แก้ที่ E-06 ไม่ใช่ที่นี่\n3) เทส: import แล้ว pending เก่าของกฎที่หายไปจาก DB ชุดใหม่ต้องไม่ถูกเขียนลง DB ในรอบ flush ถัดไป",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run Backup ผ่าน", "ไฟล์ export ไม่มี key ที่เกี่ยวกับ endpoints/counters"],
    "depends_on": ["E-06"]
  },
  {
    "task_id": "E-10",
    "title": "docs: openapi สำหรับ field ใหม่ของ /policies/{id}/endpoints",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "เพิ่ม field source / collectingSince / capped / evicted / maxPerDirection ใน schema ของ PolicyRuleEndpoints พร้อมคำอธิบายว่า: source=persisted หมายถึงข้อมูลมาจาก SQLite (ไม่หายเมื่อ reapply/restart/ล้าง Traffic Log) และเกิดขึ้นเฉพาะกฎที่เปิด Monitor, source=buffer คือพฤติกรรมเดิมที่อ่านจาก ring buffer, เมื่อ source=persisted ค่า scannedEntries/bufferOldestAt ไม่มีความหมาย, ข้อมูลเริ่มนับจากศูนย์ตอนเปิด Monitor (ไม่มี backfill ย้อนหลัง), และ capped=true หมายถึงถึงเพดาน maxPerDirection (default 1000 ต่อหมวด) แล้วและระบบกำลัง evict รายการที่ไม่ถูกพบนานที่สุดออก ; ต้องระบุด้วยว่ายังคงต้องเปิด Log ที่กฎถึงจะมีข้อมูล (E-D7) ; sync สองไฟล์ให้ตรงกัน (backend/internal/api/dist/openapi.yaml เป็น build artifact ไม่ต้องแก้มือ) ; **ไม่มี endpoint ใหม่ในแผนนี้**",
    "acceptance": ["YAML parse ผ่าน", "ทุก field ใหม่มีเอกสารและตรงกับ model/types.go"],
    "depends_on": ["E-07"]
  },
  {
    "task_id": "E-11",
    "title": "frontend service: type + mock ของ field ใหม่",
    "layer": "frontend",
    "files": ["frontend/src/services/policyEndpointsService.ts"],
    "instruction": "1) เพิ่ม source: \"persisted\" | \"buffer\", collectingSince?: string, capped: boolean, evicted: number, maxPerDirection: number ใน interface PolicyRuleEndpoints และอัปเดตคอมเมนต์ข้อจำกัดหัวไฟล์ (ข้อ 3 เดิมที่บอกว่า 'ล้าง Traffic Log แล้วข้อมูลหาย' ต้องแก้ให้ระบุว่าใช้กับ source=buffer เท่านั้น)\n2) buildMockEndpoints: ให้กฎที่ rule.monitored === true คืน source=\"persisted\" พร้อม collectingSince/evicted/capped และ maxPerDirection = 1000 แบบ deterministic (เช่นกฎที่ hash ลงตัวบางค่าให้ capped=true พร้อม evicted > 0 เพื่อให้ทดสอบ UI เตือนได้) และกฎอื่นคืน source=\"buffer\" เหมือนเดิม\n3) ห้ามเปลี่ยน signature ของ getRuleEndpoints",
    "acceptance": ["yarn build ผ่าน (tsc -b)", "yarn lint ผ่าน", "mock mode ใช้งานได้โดยไม่ต้องมี backend และเห็นทั้งสองโหมด"],
    "depends_on": []
  },
  {
    "task_id": "E-12",
    "title": "frontend: RuleStatsDrawer แสดงที่มาของข้อมูล Endpoints + คำเตือน cap/Log",
    "layer": "frontend",
    "files": ["frontend/src/components/policy/RuleStatsDrawer.tsx"],
    "instruction": "แก้เฉพาะบล็อก 'Endpoints ที่ตรงกับกฎนี้' และกล่อง 'ข้อจำกัดของข้อมูลสถิตินี้' เท่านั้น (บล็อก Monitor และบล็อกอื่นห้ามแตะ ยกเว้นข้อความ confirm ตามข้อ 6)\n1) ใต้หัวข้อบล็อก ให้แสดง Badge บอกที่มา: source=\"persisted\" → <Badge variant=\"outline\">เก็บถาวร</Badge> พร้อมข้อความ 'เก็บมาตั้งแต่ {fmtAbsoluteTime(collectingSince)} — ไม่หายเมื่อ Apply/รีสตาร์ท/ล้าง Traffic Log' ; source=\"buffer\" → Badge 'จาก Traffic Log' พร้อมข้อความเดิมเรื่องย้อนหลังถึง bufferOldestAt\n2) เมื่อ source=\"buffer\" และ rule.monitored === false ให้แสดงบรรทัดชวนเปิด: 'เปิด Monitor ด้านบนเพื่อเก็บรายการนี้แบบถาวร (จะเริ่มนับใหม่จากศูนย์)' — ต้องบอกให้ชัดว่าเริ่มจากศูนย์ ไม่ยกข้อมูลเก่ามา (E-D6)\n3) เมื่อ rule.monitored === true แต่ endpoints.logEnabled === false ให้แสดงกล่องเตือน (border-border bg-muted/50 เหมือนกล่อง empty state เดิม) ว่า 'เปิด Monitor แล้วแต่กฎนี้ยังไม่ได้เปิด Log จึงยังไม่มีข้อมูล Endpoints ให้เก็บ' พร้อมปุ่ม variant=\"outline\" size=\"sm\" 'เปิด Log ให้กฎนี้' ที่เรียก policyService.toggleLog(rule.id) แล้ว onChanged?.() — error ให้แจ้งผ่าน alert() ของ useAlert และไม่เปลี่ยน state ใน UI (D-7 ของแผน #141: หน้านี้ไม่มี role gate ฝั่ง frontend)\n4) เมื่อ capped === true ให้แสดงข้อความ text-muted-foreground: 'เก็บได้สูงสุด {maxPerDirection} รายการต่อหมวด — รายการที่ไม่ถูกพบนานที่สุดจะถูกลบออกก่อน (ลบไปแล้ว {evicted} รายการ)' โดยต้องอ่านค่าจาก field ที่ API ส่งมา **ห้าม hardcode เลข 1000 ในโค้ด frontend**\n5) เพิ่มข้อจำกัดข้อใหม่ในกล่อง 'ข้อจำกัดของข้อมูลสถิตินี้' (ต่อจากข้อ 8 เดิม): ข้อ 9 อธิบายว่าเมื่อเปิด Monitor ข้อมูล Endpoints จะถูกเก็บถาวรภายใต้เพดานต่อหมวดและใช้กติกา 'ลบตัวที่ไม่ถูกพบนานที่สุดก่อน' และเริ่มนับจากศูนย์ตอนเปิด Monitor, ข้อ 10 อธิบายว่ายังต้องเปิด Log ที่กฎถึงจะมีข้อมูล (ข้อจำกัดของ NFLOG ไม่ใช่บั๊ก) และปิด Monitor/กดรีเซ็ตจะลบรายการเหล่านี้ทิ้งด้วย\n6) แก้ข้อความ confirm ของ 'ปิด Monitor' และ 'รีเซ็ตค่า' ให้ครอบคลุมว่ารายการ Endpoints ที่เก็บไว้จะถูกลบด้วย\n7) เคารพ docs/rules_of_work.md: ห้าม hardcode สีแบรนด์/สถานะ (ใช้ text-primary/text-destructive/text-muted-foreground), ห้าม shadow-*/backdrop-blur-*, รองรับ dark/light, ใช้เฉพาะ components/ui ; Drawer นี้ไม่มี Combobox จึงคง modal behavior เดิม",
    "acceptance": ["yarn build ผ่าน", "yarn lint ผ่าน", "ไม่มี class สีดิบ/shadow/backdrop-blur เพิ่มเข้ามา", "ไม่มีเลข cap hardcode ในไฟล์ frontend", "ทั้งสองโหมด (persisted/buffer) แสดงผลถูกต้องใน mock mode ทั้ง dark และ light"],
    "depends_on": ["E-11"]
  },
  {
    "task_id": "E-13",
    "title": "build รวมทั้งระบบ",
    "layer": "repo",
    "files": ["build.sh"],
    "instruction": "รัน bash build.sh ให้ผ่านตลอดสาย (frontend build → copy dist → backend build) เพื่อยืนยันว่า embed ไม่พังและไม่มี type/compile error ค้าง",
    "acceptance": ["bash build.sh สำเร็จ ได้ไบนารี ./pigate", "cd backend && go test ./... ผ่านทั้งหมด"],
    "depends_on": ["E-08", "E-09", "E-10", "E-12"]
  }
]
```

---

## 4. ข้อควรระวัง (Cautions)

1. **`Record()` รันบน NFLOG read loop** — ห้าม I/O, ห้าม query DB, ห้าม `time.Now()` ต่อ event, ห้าม
   panic; ถ้าพลาดจะทำให้การ log แพ็กเก็ตทั้งระบบสะดุด (บทเรียนเดียวกับ Caution 4 ของ statistics-page-plan)
2. **ห้ามบวก count จาก ring buffer เข้ากับค่าที่ persist** — จะนับซ้ำทันที (E-D6)
3. **FK cascade ไม่ช่วยตอนปิด Monitor** — FK ผูกกับ `firewall_policies` ไม่ใช่ `policy_rule_counters`
   ต้อง `DELETE FROM policy_rule_endpoints` เองใน transaction เดียวกัน
4. **ห้ามเปิด transaction เมื่อไม่มีอะไรจะเขียน** และ **ห้ามสั่ง DELETE eviction เมื่อยังไม่เกิน cap**
   (SD card write cycle, `tech_stack_design.md` §8)
5. **eviction ต้องจำกัดเฉพาะคู่ (กฎ, direction) ที่ถูกแตะในรอบ flush นั้น** ห้ามไล่ทั้งตาราง
6. **กรอง delta ด้วย `GetMonitoredPolicyIDs()` ก่อนเขียนเสมอ** — กฎที่ถูกลบไปแล้วจะทำให้ FK constraint
   ล้มทั้ง transaction ถ้าปล่อยหลุดเข้าไป
7. **`first_seen_at` ต้องไม่ถูกเขียนทับตอน UPSERT** (ใช้ `DO UPDATE SET` เฉพาะ count/last_seen_at)
8. **กติกาการข้ามค่า `"-"`/`""` ต้องตรงกับ `AggregateByRule` เป๊ะ** ไม่งั้นตัวเลขสองโหมดจะเทียบกันไม่ได้
   และผู้ใช้จะเห็นตัวเลขกระโดดตอนเปิด Monitor
9. **ห้ามเพิ่ม route ใหม่** — แผนนี้ใช้ `GET /api/policies/{id}/endpoints` เดิม เปลี่ยนแค่แหล่งข้อมูลและ
   เพิ่ม field (ลดพื้นที่ security review และไม่ต้องแตะ auth)
10. **ห้ามแตะ `real_firewall.go` / โครง 4 section ของ input chain** — แผนนี้ไม่ยุ่งกับการสร้าง nft rule เลย
11. **โหมด buffer ต้อง regression เป็นศูนย์** — กฎที่ไม่ได้เปิด Monitor ต้องได้ผลลัพธ์เท่าเดิมทุก field
    และเทสเดิมของ `firewall-rule-matched-endpoints-plan` ต้องผ่านโดยไม่ต้องแก้
12. **openapi ต้องอัปเดตพร้อมโค้ด** ไม่ปล่อยให้ drift
13. **cap = 1000 ทำให้ transaction ต่อรอบ flush ใหญ่ขึ้นกว่าที่แผนเสนอไว้เดิม 5 เท่า** — ต้อง
    `tx.Prepare` ครั้งเดียวนอกลูป (E-04) และห้ามมี query รายแถวเพิ่มเติมนอกเหนือจาก UPSERT
    (เช่นห้าม SELECT ตรวจว่ามีแถวอยู่ก่อนแล้วค่อย INSERT — ใช้ UPSERT อย่างเดียว)
14. **ห้าม hardcode เลข 1000 นอก `config.Defaults()` และค่า fallback ของ recorder** — ทุกเลเยอร์อื่น
    (repo/service/frontend) ต้องรับค่ามาจาก config เท่านั้น

---

## 5. งบประมาณทรัพยากรที่ cap = 1000 (คำนวณใหม่ตามค่าที่เจ้าของเลือก)

**สมมติฐานขนาดต่อแถว**: `policy_id` (UUID 36 ตัวอักษร) + `direction` (3) + `endpoint_key` (IPv4 ~15 /
IPv6 ได้ถึง 39) + `count` + `first_seen_at`/`last_seen_at` (RFC3339 อย่างละ 20) ≈ **~110 bytes ของข้อมูล
ดิบ** เมื่อรวม overhead ของ b-tree + PRIMARY KEY index + index LRU ให้ประเมินเป็น **~200 bytes ต่อแถว
บนดิสก์**

| สถานการณ์ | แถวใน DB | ขนาดบนดิสก์ (โดยประมาณ) |
|---|---|---|
| ไม่มีกฎไหนเปิด Monitor | 0 | 0 |
| 1 กฎ LAN ปกติ (โฮสต์ ~30, ปลายทางไม่กี่สิบ, service ไม่กี่ตัว) | ~60-80 | ~15 KB |
| 1 กฎ เต็ม cap ทั้ง 3 หมวด (worst case ทางทฤษฎี) | 3 × 1000 = **3,000** | **~600 KB** |
| 5 กฎที่เปิด Monitor และเต็ม cap หมด | 15,000 | ~3 MB |
| **20 กฎที่เปิด Monitor และเต็ม cap หมด (worst case ที่เจ้าของถาม)** | **60,000** | **~12 MB** |

**RAM ของ recorder** (pending ระหว่างสอง flush, admission cap = 1000 ต่อหมวด): map entry แต่ละตัว
(string key + struct ที่มี count + 2 timestamp string) ≈ ~180-200 bytes → **~600 KB ต่อกฎ worst case**
และ **~12 MB ที่ 20 กฎ worst case** — บน Raspberry Pi 5 (4/8 GB) ถือว่าเล็ก แต่ต้องรู้ว่ามีอยู่
(เทียบเคียง: ring buffer ของ traffic log ที่ 10,000 entry กินราว 2.5 MB)

**ปริมาณเขียน SD ต่อรอบ flush (ทุก 5 นาที)**:

| สถานการณ์ | จำนวน UPSERT ต่อรอบ | DELETE ต่อรอบ |
|---|---|---|
| ไม่มีทราฟฟิกเข้ากฎที่ monitored | **0 (ไม่เปิด transaction เลย)** | 0 |
| กฎ LAN ปกติที่มีทราฟฟิก | สิบต้นๆ ถึงร้อยต้นๆ | 0 (ยังไม่ถึง cap) |
| กฎ WAN โดน scan ต่อเนื่อง 1 กฎ | ≤ 3,000 (ถูกจำกัดด้วย admission cap ฝั่ง RAM) | เท่าจำนวนที่ล้น cap |
| 20 กฎ worst case พร้อมกัน | ≤ 60,000 ใน 1 transaction / 5 นาที | ตามที่ล้น |

**หมายเหตุที่ต้องเข้าใจตรงกัน**: จำนวน UPSERT ต่อรอบไม่ได้ถูกจำกัดด้วย "จำนวนแถวที่เก็บ" แต่ถูกจำกัดด้วย
"จำนวน key ที่ recorder รับไว้ในหน้าต่าง 5 นาทีนั้น" ซึ่ง admission cap ล็อกไว้ที่ 3,000 ต่อกฎพอดี —
นี่คือเหตุผลที่ admission cap ฝั่ง RAM ต้องเท่ากับ cap ฝั่ง DB (ถ้าปล่อยฟรีจะกลายเป็นการเขียนหลักหมื่นแถว
ต่อรอบต่อกฎเดียวตอนโดน scan) worst case 20 กฎที่ 60,000 UPSERT/5 นาที เป็นเคสที่ไม่สมจริงในการใช้งานจริง
(ต้องมีกฎที่หันหน้าออกเน็ต 20 กฎที่เปิด Monitor ครบและโดน scan พร้อมกันตลอดเวลา) แต่ถ้าผู้ดูแลเจอสภาพนี้
สามารถลด `monitored-endpoints-max-per-rule` ลงได้ทันทีจากไฟล์ config โดยไม่ต้อง rebuild

---

## 6. Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก Task เสร็จ — สำหรับ ai-qa)

> ทดสอบชุดนี้ **เพิ่มเติมจาก** Final Acceptance ของ `fqdn-retry-and-monitored-counters-plan.md`
> ซึ่งต้องยังผ่านครบทุกข้อด้วย

```json
{
  "final_acceptance": [
    "cd backend && go build ./... ผ่าน, go vet ./... ไม่มี error",
    "cd backend && go test ./... ผ่านทั้งหมด (รวมเทสเดิมทั้งหมด ไม่มีตัวไหน fail/skip เพิ่ม)",
    "cd backend && go test -race ./internal/service/... ผ่าน (recorder ถูกเรียกจาก NFLOG loop ขณะ flush ทำงาน)",
    "cd frontend && yarn build และ yarn lint ผ่าน; bash build.sh สำเร็จได้ไบนารี ./pigate",
    "ค่า default — pigate.conf ที่ generate ใหม่ต้องมี monitored-endpoints-enabled=true และ monitored-endpoints-max-per-rule=1000 ต่อท้ายไฟล์ และค่านอกช่วง (เช่น 1 หรือ 99999) ต้องถูก clamp กลับเป็น 1000 พร้อม warning ใน log",
    "Migration — รันด้วย DB ไฟล์เดิมที่สร้างจาก branch นี้ก่อนแผนนี้ (มี policy_rule_counters แล้วแต่ยังไม่มี endpoints_evicted): ต้องผ่าน, ได้ตาราง policy_rule_endpoints + index + คอลัมน์ endpoints_evicted; รันซ้ำอีกรอบไม่ error",
    "Regression โหมด buffer — กฎที่ปิด Monitor: GET /api/policies/{id}/endpoints ต้องคืน source=\"buffer\" และตัวเลข/ลำดับทุก field เท่ากับก่อน merge แผนนี้ทุกประการ",
    "เก็บได้จริง — เปิด Monitor + เปิด Log ที่กฎหนึ่ง, ยิงทราฟฟิกจากหลาย source IP, รอครบรอบ flush → ตาราง policy_rule_endpoints มีแถวของ IP เหล่านั้นพร้อม count/first_seen_at/last_seen_at ถูกต้อง",
    "เริ่มจากศูนย์ ไม่ backfill — ก่อนเปิด Monitor ให้มีข้อมูลของกฎนั้นค้างอยู่ใน Traffic Log ก่อน แล้วจึงเปิด Monitor: ตาราง policy_rule_endpoints ต้อง 'ไม่' มีแถวจากข้อมูลย้อนหลังชุดนั้น และ UI ต้องเริ่มนับจากศูนย์พร้อมแสดง 'เก็บมาตั้งแต่' เป็นเวลาที่เพิ่งเปิด",
    "เห็นทันทีไม่ต้องรอ flush — ทันทีหลังเปิด Monitor แล้วมีทราฟฟิกเข้ากฎ (ยังไม่ถึงรอบ flush 5 นาที) Drawer ต้องแสดงรายการแล้ว (ยอด pending ใน RAM ถูกบวกเข้าไป) และเมื่อ flush แล้วตัวเลขต้องไม่กระโดด/ไม่นับซ้ำ",
    "ไม่หายเมื่อ reapply — กด Apply Settings แล้วรายการ Endpoints และตัวเลข count ต้องคงอยู่ครบ",
    "ไม่หายเมื่อ restart — restart process แล้วรายการยังอยู่ครบ (มาจาก SQLite) และ collectingSince ไม่เปลี่ยน",
    "ไม่หายเมื่อล้าง Traffic Log — กดล้าง Traffic Log แล้วบล็อก Endpoints ของกฎที่ monitored ต้องยังแสดงข้อมูลเดิมครบ (นี่คือเป้าหมายหลักของแผนนี้) ส่วนกฎที่ไม่ monitored ต้องว่างเปล่าตามพฤติกรรมเดิม",
    "cap + LRU (ทดสอบด้วยค่าต่ำเพื่อความเร็ว) — ตั้ง monitored-endpoints-max-per-rule=20 ในไฟล์ config, ยิงทราฟฟิกจาก source IP ที่ไม่ซ้ำกันเกิน 20 ตัว โดยให้มี IP ชุดหนึ่ง 'ยังคุยอยู่เรื่อยๆ' → หลัง flush ต้องเหลือแถวไม่เกิน 20 ต่อ direction, IP ที่ยังคุยอยู่ต้องยังอยู่ครบ, IP ที่เห็นครั้งเดียวนานแล้วถูกลบก่อน, endpoints_evicted เพิ่มขึ้นตามจำนวนที่ลบจริง และ UI ขึ้นข้อความเตือนว่าถึงเพดานพร้อมแสดงเลข 20 (ไม่ใช่เลข hardcode)",
    "cap ค่า default — ตั้งกลับเป็น 1000 (default) แล้วยืนยันว่า UI แสดง 'เก็บได้สูงสุด 1000 รายการต่อหมวด' และ maxPerDirection ใน response = 1000",
    "SD write — ในสภาวะไม่มีทราฟฟิกเข้ากฎที่ monitored เลย ต้องไม่มี transaction เขียน policy_rule_endpoints เกิดขึ้นตลอดหลายรอบ flush; และในรอบที่แถวยังไม่ถึง cap ต้องไม่มีคำสั่ง DELETE เกิดขึ้นเลย",
    "ปริมาณเขียนถูกจำกัดจริง — ยิง source IP ที่ไม่ซ้ำกันจำนวนมาก (มากกว่า cap หลายเท่า) ภายในหนึ่งรอบ flush แล้วยืนยันว่าจำนวนแถวที่ถูกเขียน/มีอยู่ต่อ (กฎ, direction) ไม่เกิน cap (admission cap ฝั่ง RAM ทำงาน)",
    "ต้องเปิด Log ก่อน — กฎที่เปิด Monitor แต่ปิด Log: ไม่มีแถวถูกเขียนลง DB เลย และ UI ขึ้นกล่องเตือนพร้อมปุ่ม 'เปิด Log ให้กฎนี้' ที่กดแล้วเปิด Log ได้จริงและเริ่มเก็บข้อมูลหลังจากนั้น",
    "ปิด Monitor — confirm dialog ต้องบอกว่ารายการ Endpoints จะถูกลบด้วย; ยืนยันแล้วแถวใน policy_rule_endpoints ของกฎนั้นหายหมดใน transaction เดียวกับแถว counter และ Drawer กลับไปแสดงโหมด buffer",
    "รีเซ็ตค่า — confirm dialog ครอบคลุม Endpoints; ยืนยันแล้วทั้ง counter และรายการ Endpoints ถูกล้าง, endpoints_evicted กลับเป็น 0, collectingSince อัปเดตเป็นเวลาปัจจุบัน, และ pending ที่ค้างใน RAM ต้องไม่ไหลกลับเข้ามาในรอบ flush ถัดไป",
    "ลบกฎที่ monitored — แถวทั้งใน policy_rule_counters และ policy_rule_endpoints หายตาม FK cascade ไม่มีแถวกำพร้า และรอบ flush ถัดไปต้องไม่เกิด FK error ใน log แม้จะมี pending ของกฎนั้นค้างอยู่",
    "kill switch — ตั้ง monitored-endpoints-enabled=false แล้วรีสตาร์ท: ไม่มีการเขียน policy_rule_endpoints เลย และทุกกฎ (รวมกฎที่ monitored) คืน source=\"buffer\"",
    "Export/Import — ไฟล์ export ต้องไม่มีข้อมูล endpoints/counters; หลัง import ยอดที่แสดงตรงกับ DB ชุดใหม่ (ไม่มีค่าค้างจากแคช/pending ของ DB ชุดเก่า)",
    "mock mode — ./pigate-backend -mock=true บูตผ่านไม่ panic, ไม่มีการเขียนตารางนี้, และหน้า Drawer ใน mock frontend แสดงได้ทั้งสองโหมด",
    "สิทธิ์ — GET /api/policies/{id}/endpoints ยังต้องการ session (401 เมื่อไม่มี) และ role admin_readonly ยังอ่านได้ตามเดิม (เป็น GET)",
    "Performance — ขณะมีทราฟฟิกหนาแน่นเข้ากฎที่ monitored ต้องไม่มีอาการ log ตกหล่นผิดปกติ/หน่วง (เทียบกับ baseline ก่อน merge) และเปิด Drawer ค้างไว้ 5 นาที (refetch ทุก 10 วิ) ต้องไม่ทำให้ CPU พุ่งผิดปกติ แม้กฎนั้นจะมีแถวเต็ม cap 1000 ทั้ง 3 หมวด",
    "Regression UI — หน้า Firewall Policy ใช้งานได้ครบ (สร้าง/แก้/ลบ/reorder/toggle log/status/monitor/reset), Drawer แสดงทุกบล็อกเดิมครบ, ทั้ง dark และ light mode"
  ]
}
```

---

## 7. บันทึกการอนุมัติ

เจ้าของโปรเจกต์อนุมัติเมื่อ 2026-08-14 ครบทั้ง 4 ข้อที่แผนฉบับร่างค้างไว้:
**cap = 1000 ต่อ (กฎ, direction)** (เลือกเองแทนข้อเสนอ 200 ของแผน),
**ผูกกับ Switch Monitor เดิม**, **คงข้อจำกัดว่ากฎต้องเปิด Log ก่อน** (พร้อมปุ่มลัดใน UI),
และ **เริ่มนับจากศูนย์ ไม่ backfill จาก ring buffer**
แผนฉบับนี้คือฉบับที่ใช้สั่งงานจริง ไม่มีข้อค้างรอการตัดสินใจแล้ว
