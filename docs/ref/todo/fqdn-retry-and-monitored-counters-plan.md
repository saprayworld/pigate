# แผนงาน: FQDN Re-resolve Retry + Persisted Monitored Rule Counters (issue #141)

สถานะ: รอเจ้าของโปรเจกต์อนุมัติ → ส่งให้ ai-developer ทำทีละ Task → ทดสอบรวมทีเดียวโดย ai-qa

Branch: `feat/fqdn-retry-and-monitored-counters` (ห้าม commit ลง main; โค้ดทั้งหมดเข้า PR)

---

## 0. ขอบเขต

แผนนี้รวม 2 เรื่องจาก issue #141 ไว้ในชุดเดียว เพราะทั้งคู่แตะ "จังหวะก่อน/หลัง ApplyRules":

1. **FQDN Retry** — ถ้า DNS/Internet ยังไม่พร้อมตอนบูต FQDN address object จะ resolve ไม่ได้และถูกข้ามถาวร
   จนกว่าจะมีคน trigger `SyncFirewallRules()` ใหม่ → เพิ่ม background ticker ที่ re-resolve และสั่ง reapply
   **เฉพาะเมื่อผลลัพธ์เปลี่ยน**
2. **Persisted Monitored Counters** — counter ต่อกฎอยู่บน RAM และรีเซ็ตทุกครั้งที่ ApplyRules →
   เพิ่ม opt-in "Monitor" ต่อกฎ ที่สะสม running total ลง SQLite และไม่หายเมื่อ reapply

นอกขอบเขต (ยืนยันแล้วใน issue): time-series/กราฟประวัติ, retention policy, incremental nft rule patching

---

## 1. สภาพปัจจุบัน (จากการสำรวจโค้ดจริง)

| จุด | ไฟล์ | สรุป |
|---|---|---|
| resolve FQDN | `backend/internal/kernel/real_firewall.go` `addressCombos()` (บรรทัด ~1142-1186) | เรียก `lookupIP` (var ชี้ `net.LookupIP`) ตอน ApplyRules เท่านั้น, fail = log warning + ข้าม entry, cap `maxFQDNResolvedIPs=8` โดยเอา "8 ตัวแรกที่ DNS ตอบ" |
| apply ทั้งชุด | `backend/internal/service/firewall.go` `SyncFirewallRules()` | โหลด DB ทุกอย่าง → `s.firewall.ApplyRules(...)` → `recordApply()` (defer). **ไม่มี mutex กันเรียกซ้อน** |
| counter ต่อกฎ | `backend/internal/service/traffic_stats.go` `processRuleCounters()` (บรรทัด 921-962) | poll ทุก `flowPollInterval = 10s`, เก็บ `ruleBaseline` (RAM) = ค่าสะสมตั้งแต่ apply ล่าสุด, detect reset ด้วย `cur < base` |
| แสดงผล | `backend/internal/service/policy_stats.go` → `GET /api/policies/stats` → `frontend/src/services/policyStatsService.ts` → `RuleStatsDrawer.tsx` | Drawer แสดง bytes/packets/percent/lastMatchedAt + ข้อความข้อจำกัด 7 ข้อ |
| pattern ticker อ้างอิง | `backend/internal/service/dhcp_health_checker.go` | `Start(ctx)` → goroutine + `time.Ticker`, guard 3 ชั้น: `repo.IsMockMode()` → `settings.Enabled` → `bus.IsPaused()` |
| DB | `backend/internal/db/connection.go` (`firewall_policies` ~บรรทัด 288, migration ADD COLUMN pattern ~บรรทัด 654-700) | migration ใช้วิธีอ่าน `sqlite_master.sql` แล้วเช็คว่ามีคอลัมน์หรือยัง ก่อน `ALTER TABLE` |
| config file-only key | `backend/internal/config/config.go` | มี pattern ครบ: struct field → `Defaults()` → `key…` const → `orderedKeys` → `applyKey()` → `valueFor()` → range clamp ใน `Resolve()` |

---

## 2. การตัดสินใจเชิงออกแบบ (ตอบข้อที่ยังไม่ล็อกใน issue)

### D-1 — ticker ต้อง "re-resolve แล้วเทียบ" ไม่ใช่ "reapply ทุกรอบ"

ถ้า ticker สั่ง `SyncFirewallRules()` ทุกรอบแบบไม่มีเงื่อนไข จะได้ผลข้างเคียงหนัก: flush ทั้ง table,
รีเซ็ต nft counter ทุกกฎ, สร้าง log noise — และไปตีกับเรื่องที่ 2 โดยตรง
ดังนั้น ticker จะ:

1. อ่าน **snapshot ของผลลัพธ์ FQDN ที่ ApplyRules ครั้งล่าสุดใช้จริง** จาก kernel layer
2. re-resolve FQDN ชุดเดียวกันด้วย helper ตัวเดียวกัน (sort + cap เหมือนกันเป๊ะ)
3. ถ้าชุด IP ต่างจากเดิม (รวมกรณี "เดิมว่าง → ตอนนี้ resolve ได้" = เคสบูต) → เรียก `SyncFirewallRules()` ครั้งเดียว
4. ถ้าเหมือนเดิม → ไม่แตะ kernel เลย (ต้นทุน = DNS query ไม่กี่ครั้ง ซึ่ง dnsmasq cache ไว้อยู่แล้ว)

ทำไมต้องเอา snapshot จาก kernel แทนที่จะให้ service จำเอง: snapshot จาก kernel คือ ground truth ว่า
"กฎที่อยู่ในเคอร์เนลตอนนี้ใช้ IP อะไร" — จับเคสบูตล้มเหลวได้ตรงๆ โดยไม่ต้องเดา และจำกัดเฉพาะ FQDN ที่
ถูกอ้างโดยกฎที่ enable จริงเท่านั้น (ไม่ยิง DNS ให้ object ที่ไม่มีใครใช้)

### D-2 — ต้อง sort ผลลัพธ์ก่อน cap 8 ตัว (จำเป็น ไม่ใช่ของแถม)

ปัจจุบันตัด "8 ตัวแรกที่ DNS ตอบ" ซึ่ง DNS round-robin สลับลำดับทุกครั้ง → ถ้าไม่ sort ก่อน compare
โดเมนที่มี A record > 8 ตัวจะถูกมองว่า "เปลี่ยน" แทบทุกรอบ และจะ reapply ไม่หยุด
จึงต้องเปลี่ยนเป็น **sort ascending ตามค่า IP แล้วค่อยตัด 8 ตัวแรก** ทั้งใน path ที่สร้างกฎและ path ที่เทียบ
(ผลข้างเคียงที่ยอมรับ: โดเมน > 8 A record จะ match ชุด IP ที่ต่ำสุด 8 ตัวแทน "8 ตัวแรกที่ DNS ส่งมา" —
เป็นพฤติกรรมที่ deterministic ขึ้น และเป็นเงื่อนไขที่ทำให้ change-detection ใช้งานได้จริง)

### D-3 — ค่า interval ที่เสนอ (พร้อมเหตุผล)

| key (file-only, ไม่มี CLI flag) | default | ช่วงที่ยอมรับ | เหตุผล |
|---|---|---|---|
| `fqdn-refresh-enabled` | `true` | bool | kill switch เผื่อ FQDN object ทำงานผิดปกติในสนามจริง |
| `fqdn-refresh-retry-interval-seconds` | `30` | 10..3600 | ใช้เมื่อ **ยังมี FQDN ที่ resolve ไม่ได้** (เคสบูต/เน็ตหลุด) — กู้คืนภายใน ≤30 วิ หลัง DNS พร้อม ต้นทุนแค่ DNS query ที่ dnsmasq cache อยู่แล้ว |
| `fqdn-refresh-interval-seconds` | `300` | 60..86400 | steady state ที่ทุก FQDN resolve ได้หมด — 5 นาทีตรงกับ TTL ที่พบบ่อยของ A record ทั่วไป และจำกัดจำนวน reapply สูงสุดที่ 12 ครั้ง/ชม. แม้ IP หมุนทุกรอบ (จริงๆ จะน้อยกว่านั้นมาก เพราะ reapply เฉพาะตอน "เปลี่ยนจริง") |
| `monitored-counter-flush-interval-seconds` | `300` | 30..86400 | worst case ไฟดับ = เสียข้อมูล 5 นาที; เขียน SQLite ≤ 288 transaction/วัน และ **ข้ามการเขียนทั้งหมดเมื่อ delta = 0** จึงแทบไม่กิน write cycle ของ SD |

Back-off ทำแบบ 2 ระดับ (ไม่ใช่ exponential เต็มรูป) เพราะสถานะที่มีความหมายจริงมีแค่สองแบบ:
"ยังมีตัวที่ resolve ไม่ได้" (ต้องถี่) กับ "ครบหมดแล้ว" (ต้องห่าง) — exponential เพิ่มความซับซ้อน/
เคสทดสอบโดยไม่ได้ประโยชน์เพิ่ม

### D-4 — จุด "flush counter ก่อน ApplyRules" อยู่ที่ service layer ไม่ใช่ `real_firewall.go`

issue เขียนว่า "ก่อน ApplyRules flush" ซึ่งตีความตรงตัวคือแก้ `real_firewall.go` — **แผนนี้เลือกวางฮุกไว้ที่
`FirewallService.SyncFirewallRules()` บรรทัดก่อนเรียก `s.firewall.ApplyRules(...)` แทน** เพราะ:

- semantics เหมือนกันเป๊ะ (นับ counter ครบก่อนเคอร์เนลถูก flush) แต่ **ไม่ต้องแตะ `real_firewall.go` เลย**
  สำหรับเรื่องที่ 2 → ลดความเสี่ยง merge conflict ตามที่ issue กังวล (บทเรียนจาก PR #140)
- ถูกต้องตาม layering: kernel layer ห้ามรู้จัก repository/SQLite
- mock backend ได้พฤติกรรมเดียวกันฟรี

การ flush นั้นจะ **บังคับ poll counter หนึ่งครั้งทันที** ก่อน drain เพื่อเก็บเศษ ≤10 วิ ที่ poller ปกติยังไม่เห็น

### D-5 — วิธีสะสมค่าที่ "ไม่หาย"

ใช้ delta-accumulation ไม่ใช่ snapshot ค่าดิบ:

```
poller(10s) → processRuleCounters() คำนวณ delta ต่อกฎอยู่แล้ว
            → (ใหม่) บวก delta เข้า pendingDeltas (RAM, key = rule id)
PolicyCounterStore.Flush() → drain pendingDeltas → กรองเฉพาะกฎที่ monitored
                           → บวกเข้ายอดสะสม (RAM) → เขียน SQLite 1 transaction
```

จังหวะเขียน 2 แบบตาม issue: (ก) `FlushBeforeApply()` เรียกจาก `SyncFirewallRules()` (ข) ticker 5 นาที

หลัง ApplyRules สำเร็จ ต้องเรียก `ZeroRuleBaselines()` เพื่อ set baseline ทุก id เป็น 0 —
ไม่งั้นถ้าทราฟฟิกแรงจน counter ใหม่โตเกินค่าเก่าก่อน poll รอบถัดไป การ detect reset (`cur < base`)
จะไม่ทำงานและนับขาด

### D-6 — semantics ของ Monitor (ยืนยันตาม issue)

- เปิด Monitor → สร้างแถวใน `policy_rule_counters` (bytes/packets = 0, `started_at` = now) แล้วสะสมไปเรื่อยๆ
- ไม่รีเซ็ตอัตโนมัติทุกกรณี (reapply / restart pigate / reboot ก็ไม่รีเซ็ต)
- ผู้ใช้กด "รีเซ็ตค่า" → zero ยอด + `started_at` = now → **ต้องมี confirm dialog**
- ปิด Monitor → ลบแถวทิ้ง (ข้อมูลหายถาวร) → **ต้องมี confirm dialog**
- ลบกฎ → แถวหายตาม `ON DELETE CASCADE`

---

## 3. Task list

> ทุก Task ทำบน branch `feat/fqdn-retry-and-monitored-counters` เท่านั้น
> **ห้าม commit ระหว่างทางถ้าเจ้าของยังไม่สั่ง** และ **ไม่ต้องทดสอบทีละ Task** — ทำครบทุก Task ก่อน แล้วค่อยเทสรวม

```json
[
  {
    "task_id": "T-00",
    "title": "สร้าง feature branch",
    "layer": "repo",
    "files": [],
    "instruction": "สร้าง branch ใหม่จาก main ชื่อ feat/fqdn-retry-and-monitored-counters แล้วทำงานทุก Task ถัดไปบน branch นี้ ห้าม push/commit ลง main",
    "acceptance": ["อยู่บน branch feat/fqdn-retry-and-monitored-counters", "working tree สะอาดก่อนเริ่ม T-01"],
    "depends_on": []
  },
  {
    "task_id": "T-01",
    "title": "เพิ่ม config key 4 ตัวสำหรับ FQDN refresher และ counter flush",
    "layer": "service",
    "files": ["backend/internal/config/config.go", "backend/internal/config/config_test.go"],
    "instruction": "เพิ่ม field ใหม่ใน Config: FQDNRefreshEnabled bool, FQDNRefreshIntervalSeconds int, FQDNRefreshRetryIntervalSeconds int, MonitoredCounterFlushIntervalSeconds int พร้อม key file-only (ไม่มี CLI flag) ชื่อ fqdn-refresh-enabled / fqdn-refresh-interval-seconds / fqdn-refresh-retry-interval-seconds / monitored-counter-flush-interval-seconds ทำครบทุกจุดตาม pattern ของ max-expanded-rules-per-policy: struct field + doc comment, Defaults() (true/300/30/300), const key, ต่อท้าย orderedKeys (ห้ามแทรกกลาง เพื่อให้ pigate.conf ที่ generate ไว้แล้ว diff สะอาด), applyKey(), valueFor() และ range clamp+warn ใน Resolve() ตามช่วง 60..86400 / 10..3600 / 30..86400 (bool ไม่ต้อง clamp) เพิ่ม unit test ให้ครอบคลุมทั้ง parse ค่าถูก, ค่านอกช่วงถูก clamp พร้อม warning, และ Write() แล้วอ่านกลับได้ค่าเดิม",
    "acceptance": ["go build ผ่าน", "go test ./internal/config/... ผ่าน", "KnownKeys() มี 4 key ใหม่และอยู่ท้ายลิสต์"],
    "depends_on": ["T-00"]
  },
  {
    "task_id": "T-02",
    "title": "kernel: exported FQDN resolve helper (sort+cap) + snapshot ผลลัพธ์ที่ ApplyRules ใช้จริง",
    "layer": "kernel",
    "files": ["backend/internal/kernel/real_firewall.go", "backend/internal/kernel/interfaces.go", "backend/internal/kernel/mock.go", "backend/internal/kernel/real_firewall_test.go"],
    "instruction": "งาน SENSITIVE (แตะ path สร้าง firewall rule) ต้อง review เข้มเป็นพิเศษ และห้ามเปลี่ยนลำดับ/จำนวน/verdict ของ nft rule ที่มีอยู่เดิม (tech_stack_design.md §4.3 โครง 4 section ของ input chain ห้ามขยับ)\n\n1) เพิ่มฟังก์ชัน exported ใน kernel package: ResolveFQDNIPv4(fqdn string) ([]net.IP, error) ที่รวม logic เดิมของ addressCombos ไว้ที่เดียว = เรียก lookupIP → กรองเฉพาะ To4() != nil → **sort ascending ตาม bytes ของ IP** → ตัดที่ maxFQDNResolvedIPs (เขียน doc comment อธิบายว่าทำไมต้อง sort ก่อน cap อ้าง D-2 ของแผนนี้)\n2) แก้ addressCombos ให้เรียก ResolveFQDNIPv4 แทน logic เดิม (log warning ข้อความเดิมทั้งหมดต้องคงไว้)\n3) เพิ่ม type fqdnRecorder (map[string][]string + mutex) ส่งผ่านเป็นพารามิเตอร์เพิ่มจาก ApplyRules → addUserChainRules → addressCombos แบบ nil-safe บันทึก key = ค่า FQDN, value = ลิสต์ IP เป็น string ที่ใช้จริง (**บันทึก key ด้วยแม้ resolve ไม่สำเร็จ โดยให้ value เป็น slice ว่าง** — นี่คือกลไกที่ทำให้ retry ตอนบูตทำงาน)\n4) เก็บ recorder ลง RealFirewall (mutex guard) หลัง conn.Flush() สำเร็จเท่านั้น\n5) เพิ่ม method ใน FirewallManager interface: FQDNResolutions() map[string][]string (คืน copy) implement ทั้ง RealFirewall และ MockFirewall (mock คืน map ว่างก็พอ พร้อม comment ว่า FQDNRefresher ถูกปิดใน mock mode อยู่แล้ว)\n6) unit test: ResolveFQDNIPv4 sort/cap ถูกต้อง (แทน lookupIP ด้วย stub), และ addressCombos ยังคืน combo เท่าเดิมสำหรับ subnet/range",
    "acceptance": ["go build ผ่าน", "go test ./internal/kernel/... ผ่านทั้งหมด (รวมเทสเดิมที่มีอยู่)", "FirewallManager มี FQDNResolutions() และมีทั้งใน real_*.go และ mock.go", "ไม่มีการเปลี่ยนลำดับ/จำนวน nft rule ในเทสเดิม"],
    "depends_on": ["T-00"]
  },
  {
    "task_id": "T-03",
    "title": "service: serialize การ apply + hook ก่อน/หลัง ApplyRules ใน FirewallService",
    "layer": "service",
    "files": ["backend/internal/service/firewall.go"],
    "instruction": "งาน SENSITIVE (firewall apply path)\n1) เพิ่ม applyMu sync.Mutex (แยกจาก s.mu เดิมที่ guard timestamp เท่านั้น — ห้ามใช้ตัวเดียวกัน เสี่ยง deadlock) ล็อกครอบ body ของ SyncFirewallRules ทั้งก้อน เพื่อกันการ flush nft table ซ้อนกันเมื่อ ticker ใหม่ทำงานพร้อมกับผู้ใช้กด Apply (ระวัง defer recordApply ที่ใช้ s.mu อยู่แล้ว ต้องไม่ล็อกซ้อนกันจนค้าง)\n2) เพิ่ม optional setter SetPolicyCounterStore(*PolicyCounterStore) และ SetTrafficStats(*TrafficStatsService) แบบ additive-setter pattern เดียวกับ SetRuleNameResolver (ห้ามเปลี่ยน signature ของ NewFirewallService)\n3) ใน SyncFirewallRules: ก่อนบรรทัด s.firewall.ApplyRules(...) ให้เรียก counterStore.FlushBeforeApply() ถ้าไม่ nil (error แค่ log ห้ามทำให้ apply ล้ม); หลัง ApplyRules คืน nil สำเร็จ ให้เรียก trafficStats.ZeroRuleBaselines() ถ้าไม่ nil\n4) เขียน doc comment อธิบายลำดับ flush-before / zero-after อ้าง D-4/D-5 ของแผนนี้",
    "acceptance": ["go build ผ่าน", "go vet ผ่าน", "ไม่มี lock ซ้อนระหว่าง applyMu กับ s.mu", "โค้ดเดิมที่เรียก NewFirewallService ไม่ต้องแก้"],
    "depends_on": ["T-08", "T-09"]
  },
  {
    "task_id": "T-04",
    "title": "service: FQDNRefresher background ticker",
    "layer": "service",
    "files": ["backend/internal/service/fqdn_refresh.go", "backend/internal/service/fqdn_refresh_test.go"],
    "instruction": "สร้างไฟล์ใหม่ FQDNRefresher เลียนแบบโครงของ dhcp_health_checker.go\n- NewFQDNRefresher(repo *db.Repository, firewall *FirewallService, fwKernel kernel.FirewallManager, bus *NetEventBus, eventLog *EventLogService, enabled bool, steadyInterval, retryInterval time.Duration) และ Start(ctx) ที่ spawn goroutine เดียว\n- ในแต่ละ tick guard ตามลำดับนี้เท่านั้น: !enabled → return; repo.IsMockMode() → return; bus.IsPaused() → return (กัน race กับการ import backup)\n- ตรรกะ tick: อ่าน snapshot := fwKernel.FQDNResolutions(); ถ้า len == 0 ไม่ต้องทำอะไร; วนทุก key เรียก kernel.ResolveFQDNIPv4 (ตัวเดียวกับ T-02) แปลงเป็น []string เทียบแบบ order-sensitive กับค่าใน snapshot; ถ้ามีอย่างน้อยหนึ่ง FQDN ที่ต่าง → เรียก firewall.SyncFirewallRules() หนึ่งครั้ง (ครั้งเดียวต่อ tick ไม่ว่าจะต่างกี่โดเมน) และ log ผ่าน EventLogService (category firewall, severity info, ระบุชื่อโดเมนกับ IP เก่า/ใหม่); ถ้า resolve error ให้ถือว่าผลลัพธ์เป็น slice ว่าง (= ยังไม่พร้อม) ห้าม panic ห้าม return ทั้ง tick\n- back-off: ถ้าหลังจบ tick ยังมีโดเมนที่ผลลัพธ์ว่าง → ใช้ retryInterval, ถ้าครบทุกโดเมนแล้ว → ใช้ steadyInterval; เปลี่ยน cadence ด้วย ticker.Reset() เฉพาะเมื่อค่าต่างจากเดิม (log ตอนสลับโหมด) เหมือน pattern ของ DhcpHealthChecker\n- แยกส่วนตัดสินใจเป็นฟังก์ชัน pure (เช่น diffResolutions(old, new map[string][]string) (changed []string, anyUnresolved bool)) เพื่อให้ unit test ได้โดยไม่ต้องมี kernel/DB\n- unit test ครอบคลุม: (1) เดิมว่าง→resolve ได้ = changed (เคสบูต) (2) เหมือนเดิม = ไม่ changed (3) ลำดับ/จำนวน IP ต่าง = changed (4) resolve ไม่ได้ = anyUnresolved true และไม่ changed",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run FQDN ผ่าน", "ไม่มีการเรียก SyncFirewallRules ในรอบที่ผลลัพธ์ไม่เปลี่ยน (พิสูจน์ด้วยเทสของฟังก์ชัน pure)"],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-05",
    "title": "db: migration คอลัมน์ monitored + ตาราง policy_rule_counters",
    "layer": "db",
    "files": ["backend/internal/db/connection.go"],
    "instruction": "1) เพิ่ม CREATE TABLE IF NOT EXISTS policy_rule_counters (policy_id TEXT PRIMARY KEY, bytes INTEGER NOT NULL DEFAULT 0, packets INTEGER NOT NULL DEFAULT 0, started_at TEXT NOT NULL, updated_at TEXT NOT NULL, FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE) ไว้ในลิสต์ CREATE TABLE ถัดจาก policy_services\n2) เพิ่มคอลัมน์ monitored ให้ firewall_policies ทั้งใน CREATE TABLE ตั้งต้น (monitored INTEGER NOT NULL DEFAULT 0 CHECK(monitored IN (0,1))) และเพิ่ม migration แบบเดียวกับที่ทำกับคอลัมน์ nat/chain คือ SELECT sql FROM sqlite_master WHERE name='firewall_policies' แล้วเช็คว่า string มีคำว่า monitored หรือยัง ถ้ายังจึง ALTER TABLE ADD COLUMN (ห้าม backfill ค่าอื่นนอกจาก 0)\n3) ห้ามใส่ retention/cleanup job ใดๆ",
    "acceptance": ["go build ผ่าน", "รัน backend ด้วย DB เดิมที่มีอยู่แล้วไม่ error (idempotent migration)", "รัน 2 ครั้งติดกันไม่ error"],
    "depends_on": ["T-00"]
  },
  {
    "task_id": "T-06",
    "title": "model: Monitored flag + type สำหรับ counter ที่ persist",
    "layer": "model",
    "files": ["backend/internal/model/types.go"],
    "instruction": "1) เพิ่ม Monitored bool `json:\"monitored\"` ใน PolicyRule และ PolicyRuleInput\n2) เพิ่ม type MonitoredCounter struct { RuleID string; Bytes uint64; Packets uint64; StartedAt string; UpdatedAt string } (StartedAt/UpdatedAt เป็น RFC3339 UTC)\n3) เพิ่มใน PolicyRuleStat: Monitored bool, MonitoredBytes uint64, MonitoredPackets uint64, MonitoredSince string (omitempty ตามความเหมาะสมของ field ที่มีอยู่)\n4) ห้ามแก้ ValidatePolicyRule ให้ปฏิเสธค่า monitored (เป็น flag อิสระ ไม่มีเงื่อนไขข้ามกับ chain/action)",
    "acceptance": ["go build ผ่าน", "go test ./internal/model/... ผ่าน"],
    "depends_on": ["T-00"]
  },
  {
    "task_id": "T-07",
    "title": "db repository: อ่าน/เขียน monitored + CRUD ของ policy_rule_counters",
    "layer": "db",
    "files": ["backend/internal/db/repository.go"],
    "instruction": "1) เพิ่มคอลัมน์ monitored ในทุก SELECT/INSERT/UPDATE ของ firewall_policies: GetPolicies, GetPolicyByID, CreatePolicy, UpdatePolicy (แปลง bool<->int แบบเดียวกับ log/nat/status)\n2) เพิ่ม method ใหม่:\n   - GetMonitoredPolicyIDs() (map[string]bool, error)\n   - SetPolicyMonitored(id string, monitored bool) error — ทำใน transaction เดียว: UPDATE flag; ถ้า monitored=true ให้ INSERT OR IGNORE แถวใน policy_rule_counters (bytes/packets=0, started_at=updated_at=now UTC RFC3339); ถ้า false ให้ DELETE แถวนั้น (ข้อมูลหายถาวรตามที่ออกแบบไว้)\n   - GetPolicyRuleCounters() ([]model.MonitoredCounter, error)\n   - AddPolicyRuleCounterDeltas(deltas map[string]model.RuleCounter) error — บวกสะสมใน transaction เดียว (UPDATE policy_rule_counters SET bytes = bytes + ?, packets = packets + ?, updated_at = ? WHERE policy_id = ?) ข้าม id ที่ไม่มีแถว และ **return ทันทีโดยไม่เปิด transaction ถ้า deltas ว่างหรือทุกค่าเป็น 0** (ประหยัด write cycle ของ SD)\n   - ResetPolicyRuleCounter(id string) error — set bytes/packets = 0, started_at = updated_at = now\n3) ระวัง SQLite INTEGER เป็น signed 64-bit: แปลง uint64 -> int64 ตอนเขียน และตอนอ่านถ้าเจอค่าติดลบให้ถือเป็น 0 พร้อม log warning",
    "acceptance": ["go build ผ่าน", "go test ./internal/db/... ผ่าน", "ทุก query ใช้ parameter binding (ห้ามต่อ string SQL เอง)"],
    "depends_on": ["T-05", "T-06"]
  },
  {
    "task_id": "T-08",
    "title": "traffic_stats: pending delta accumulator + drain + poll-once + zero-baselines",
    "layer": "service",
    "files": ["backend/internal/service/traffic_stats.go", "backend/internal/service/traffic_stats_test.go"],
    "instruction": "แก้เท่าที่จำเป็น ห้ามเปลี่ยนพฤติกรรมของ RuleCounterSnapshot/RuleLastHits/GetTrafficDetail เดิม\n1) เพิ่ม field pendingRuleDeltas map[string]model.RuleCounter (guard ด้วย ruleMu ตัวเดิม) ใน processRuleCounters เมื่อคำนวณ delta ได้ (ทั้ง branch ปกติและ branch reset) ให้บวกเข้า pendingRuleDeltas ด้วย\n2) เพิ่ม DrainRuleDeltas() map[string]model.RuleCounter — คืนค่าที่สะสมไว้แล้วเคลียร์ map (มีผู้บริโภคเพียงรายเดียวคือ PolicyCounterStore)\n3) เพิ่ม PollRuleCountersOnce() error — เรียก s.acct.DumpRuleCounters() แล้ว feed เข้า processRuleCounters ทันที (ไม่แตะ bucket/history/rate) ใช้ตอน FlushBeforeApply เพื่อเก็บเศษ <=10 วินาทีก่อนเคอร์เนลถูก flush; ต้องปลอดภัยเมื่อ acct เป็น mock\n4) เพิ่ม ZeroRuleBaselines() — set ทุก entry ใน ruleBaseline เป็น {0,0} (คง key ไว้, คง ruleSeeded = true) เขียน doc comment ว่าเรียกหลัง ApplyRules สำเร็จเท่านั้น และเหตุผลคือกันเคส counter ใหม่โตเกินค่าเก่าจนตรวจ reset ไม่เจอ\n5) unit test: delta ถูกสะสมเข้า pending, drain แล้วว่าง, ZeroRuleBaselines ทำให้ poll ถัดไปนับ cur เต็มจำนวนเป็น delta",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run TrafficStats ผ่าน", "ไม่มี data race (go test -race ในไฟล์นี้ผ่าน)"],
    "depends_on": ["T-06"]
  },
  {
    "task_id": "T-09",
    "title": "service: PolicyCounterStore (สะสม + persist + reset)",
    "layer": "service",
    "files": ["backend/internal/service/policy_counter_store.go", "backend/internal/service/policy_counter_store_test.go"],
    "instruction": "สร้าง service ใหม่ PolicyCounterStore(repo *db.Repository, trafficStats *TrafficStatsService, flushInterval time.Duration)\n- Load() — อ่านยอดสะสมจาก DB เข้าแคช RAM ตอน startup\n- Start(ctx) — ticker ตาม flushInterval เรียก Flush() (ข้าม tick เมื่อ repo.IsMockMode())\n- Flush() error — drain pending จาก trafficStats → กรองเฉพาะ rule id ที่ monitored (repo.GetMonitoredPolicyIDs()) → บวกเข้าแคช RAM → repo.AddPolicyRuleCounterDeltas(...) ; **ถ้าไม่มี delta ที่ไม่เป็นศูนย์ ให้ return ทันทีโดยไม่เขียน DB** ; delta ของกฎที่ไม่ได้ monitored ให้ทิ้ง\n- FlushBeforeApply() error — เรียก trafficStats.PollRuleCountersOnce() ก่อน แล้วจึง Flush() (error ของ poll แค่ log แล้วไป Flush ต่อ ห้ามทำให้ apply ล้ม)\n- Totals() map[string]model.MonitoredCounter — คืน copy สำหรับ PolicyStatsService\n- SetMonitored(id string, on bool) error — เรียก repo.SetPolicyMonitored + อัปเดต/ลบแคช RAM (ตอนเปิดให้ Flush() ก่อน เพื่อไม่ให้ delta ค้างจากช่วงก่อนหน้าถูกนับเข้ายอดใหม่)\n- ResetRule(id string) error — Flush() ก่อน (ทิ้ง delta ค้าง) แล้ว repo.ResetPolicyRuleCounter + zero แคช RAM\n- Reload() error — alias ของ Load() ใช้หลัง import backup\n- ทุก method ต้อง goroutine-safe (mutex ของตัวเอง) และไม่เรียก kernel โดยตรง\n- unit test: สะสมข้ามการ ApplyRules จำลอง (drain สองรอบ) ได้ยอดรวมถูก, กฎที่ไม่ monitored ไม่ถูกเขียน, delta ศูนย์ไม่เขียน DB",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run PolicyCounter ผ่าน", "ไม่มี import ของ kernel package ในไฟล์นี้"],
    "depends_on": ["T-07", "T-08"]
  },
  {
    "task_id": "T-10",
    "title": "service: PolicyStatsService รวมค่ายอดสะสมที่ persist เข้า response",
    "layer": "service",
    "files": ["backend/internal/service/policy_stats.go"],
    "instruction": "เพิ่ม optional setter SetCounterStore(*PolicyCounterStore) (additive pattern เดียวกับ SetDomainLookup ห้ามแก้ signature ของ NewPolicyStatsService) แล้วใน GetPolicyRuleStats เติม Monitored/MonitoredBytes/MonitoredPackets/MonitoredSince ของแต่ละแถวจาก store.Totals() + r.Monitored ; ถ้า store เป็น nil ให้ค่า monitored เป็น false/0 ทั้งหมด ; **ห้ามเอา monitored bytes ไปคิดใน TotalBytes/Percent** (สองตัวเลขนี้คนละความหมายกัน — percent ยังเป็นสัดส่วนของทราฟฟิกตั้งแต่ apply ล่าสุดเหมือนเดิม)",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run PolicyStats ผ่าน", "percent/totalBytes ให้ค่าเท่าเดิมเมื่อไม่มีกฎไหน monitored"],
    "depends_on": ["T-09"]
  },
  {
    "task_id": "T-11",
    "title": "api: endpoint toggle-monitor และ reset counter",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go", "backend/internal/api/router.go", "backend/internal/api/policy_stats_handler_test.go"],
    "instruction": "งาน SENSITIVE (เพิ่ม route ที่เปลี่ยน state)\n1) เพิ่ม HandleTogglePolicyMonitor (POST /api/policies/{id}/toggle-monitor) และ HandleResetPolicyMonitorCounter (POST /api/policies/{id}/monitor/reset) ใช้ authRoute เหมือน toggle-log/toggle-status (ไม่ใช่ superAdminRoute) — ทั้งคู่เป็น POST จึงถูก RoleReadOnlyMiddleware/-disable-edit บล็อกโดยอัตโนมัติอยู่แล้ว\n2) validate id ตาม pattern เดิมของ handler กลุ่ม policies, คืน 404 เมื่อไม่พบกฎ, 503 เมื่อ counter store ยังไม่ถูก wire (เลียนแบบ HandleGetPolicyStats ที่จัดการกรณี service เป็น nil)\n3) response คืน PolicyRule ที่อัปเดตแล้ว (toggle) และ 200 พร้อม body ยืนยัน (reset)\n4) เพิ่ม handler test: no session -> 401, id ไม่มีจริง -> 404, mock mode toggle แล้ว GET /api/policies/stats สะท้อนค่า monitored",
    "acceptance": ["go build ผ่าน", "go test ./internal/api/... ผ่าน", "route ใหม่อยู่ในกลุ่ม 4. Firewall Policies ของ router.go พร้อม comment อ้างแผนนี้"],
    "depends_on": ["T-10"]
  },
  {
    "task_id": "T-12",
    "title": "backup: พา monitored ผ่าน export/import และ reload ยอดสะสมหลัง import",
    "layer": "service",
    "files": ["backend/internal/service/backup.go", "backend/internal/service/backup_test.go"],
    "instruction": "1) ตรวจว่า export/import ของ policies พา field monitored ไปด้วย (ถ้า marshal ทั้ง struct อยู่แล้วก็ไม่ต้องแก้ แต่ต้องยืนยันด้วยเทส) — **ห้าม export ตาราง policy_rule_counters** (เป็น runtime data ไม่ใช่ config)\n2) หลัง import สำเร็จ ให้เรียก PolicyCounterStore.Reload() (ผ่าน optional setter แบบเดิม) เพื่อให้แคช RAM ตรงกับ DB ชุดใหม่\n3) เทส: import ไฟล์ backup เวอร์ชันเก่าที่ไม่มี field monitored ต้องผ่านและได้ค่า false (backward compatible)",
    "acceptance": ["go build ผ่าน", "go test ./internal/service/... -run Backup ผ่าน", "backup ไฟล์เก่า import ได้ไม่ error"],
    "depends_on": ["T-09"]
  },
  {
    "task_id": "T-13",
    "title": "main.go: wiring ทั้งหมด (ลำดับ startup)",
    "layer": "service",
    "files": ["backend/cmd/pigate/main.go"],
    "instruction": "1) สร้าง policyCounterStore := service.NewPolicyCounterStore(repo, trafficStatsService, time.Duration(cfg.MonitoredCounterFlushIntervalSeconds)*time.Second) หลัง trafficStatsService ถูกสร้าง แล้วเรียก .Load() (error แค่ log warning ห้ามทำให้ boot ล้ม)\n2) firewallService.SetPolicyCounterStore(...) และ .SetTrafficStats(trafficStatsService) ; policyStatsService.SetCounterStore(...) ; backupService (ถ้ามี setter) รับ store ไปเรียก Reload\n3) policyCounterStore.Start(monitorCtx) วางไว้กลุ่มเดียวกับ trafficStatsService.Start(monitorCtx)\n4) สร้างและ Start FQDNRefresher **หลัง** firewallService.InitApplyConfig() และหลัง netlinkMonitor.Start(...) — วางถัดจาก dhcpHealthChecker.Start(monitorCtx) พร้อม log บรรทัดแนะนำตัวแบบเดียวกัน\n5) ทุกอย่างใช้ monitorCtx เพื่อให้ปิดตอน shutdown",
    "acceptance": ["go build ผ่าน", "รัน ./pigate-backend -mock=true แล้ว log ลำดับ startup ครบและไม่มี panic", "FQDNRefresher ไม่ทำงานใน mock mode"],
    "depends_on": ["T-01", "T-03", "T-04", "T-11", "T-12"]
  },
  {
    "task_id": "T-14",
    "title": "docs: openapi.yaml สำหรับ endpoint/field ใหม่",
    "layer": "api",
    "files": ["docs/openapi.yaml", "frontend/public/openapi.yaml"],
    "instruction": "เพิ่ม POST /policies/{id}/toggle-monitor และ POST /policies/{id}/monitor/reset, เพิ่ม field monitored ใน schema ของ PolicyRule/PolicyRuleInput, เพิ่ม monitored/monitoredBytes/monitoredPackets/monitoredSince ใน PolicyRuleStat พร้อมคำอธิบายว่ายอดนี้สะสมข้าม Apply/รีสตาร์ท และหายเมื่อผู้ใช้กดรีเซ็ตหรือปิด Monitor เท่านั้น ; ถ้า frontend/public/openapi.yaml เป็นไฟล์สำเนา ให้ sync ให้ตรงกัน (ไฟล์ใน backend/internal/api/dist/ เป็น build artifact ไม่ต้องแก้มือ)",
    "acceptance": ["YAML parse ผ่าน (ไม่มี syntax error)", "ทุก endpoint/field ใหม่มีเอกสาร"],
    "depends_on": ["T-11"]
  },
  {
    "task_id": "T-15",
    "title": "frontend services: type + mock สำหรับ monitored",
    "layer": "frontend",
    "files": ["frontend/src/data-mockup/mockData.ts", "frontend/src/services/policyService.ts", "frontend/src/services/policyStatsService.ts"],
    "instruction": "1) เพิ่ม monitored: boolean ใน interface PolicyRule (mockData.ts)\n2) policyService: เพิ่ม toggleMonitor(id) และ resetMonitorCounter(id) ยิงไปที่ POST /policies/{id}/toggle-monitor และ /policies/{id}/monitor/reset พร้อม branch mock mode ที่แก้ค่าใน mock store (ตาม pattern ของ toggleLog เดิม)\n3) policyStatsService: เพิ่ม monitored/monitoredBytes/monitoredPackets/monitoredSince ใน PolicyRuleStat และให้ buildMockStats สังเคราะห์ค่าแบบ deterministic (กฎที่ monitored=true ให้ยอดสะสมมากกว่ายอด since-apply เสมอ เพื่อให้เห็นความต่างชัดเจนตอน dev)",
    "acceptance": ["yarn build ผ่าน (tsc -b)", "yarn lint ผ่าน", "mock mode ใช้งานได้โดยไม่ต้องมี backend"],
    "depends_on": ["T-00"]
  },
  {
    "task_id": "T-16",
    "title": "frontend: Switch Monitor + ยอดสะสม + ปุ่มรีเซ็ต ใน RuleStatsDrawer",
    "layer": "frontend",
    "files": ["frontend/src/components/policy/RuleStatsDrawer.tsx", "frontend/src/components/policy/PolicyChainPage.tsx"],
    "instruction": "1) ใน RuleStatsDrawer เพิ่มบล็อกใหม่ (คั่นด้วย border-t เหมือนบล็อกอื่น) ชื่อ 'เก็บสถิติสะสม (Monitor)' ประกอบด้วย: shadcn Switch (import จาก @/components/ui/switch เหมือนที่ PolicyChainPage ใช้), ข้อความอธิบายสั้นๆ ว่าเปิดแล้วยอดจะสะสมต่อเนื่องไม่รีเซ็ตตอน Apply, และเมื่อเปิดอยู่ให้แสดง Bytes/Packets สะสม (fmtBytes/toLocaleString) + 'เก็บมาตั้งแต่' (fmtAbsoluteTime ของ monitoredSince) + ปุ่ม 'รีเซ็ตค่า' (variant outline size sm)\n2) การปิด Switch และการกดรีเซ็ต **ต้องผ่าน confirm() จาก useAlert ทั้งคู่** โดยข้อความต้องบอกชัดว่าข้อมูลจะหายถาวรและกู้คืนไม่ได้ (ปิด Monitor = ลบยอดสะสมทิ้ง)\n3) เพิ่ม prop onChanged?: () => void ให้ Drawer เรียกหลัง toggle/reset สำเร็จ และให้ PolicyChainPage ส่ง callback ที่ refetch policies + stats ทันที (ไม่ต้องรอ poll 10 วิ)\n4) เพิ่มข้อจำกัดข้อใหม่ในกล่อง 'ข้อจำกัดของข้อมูลสถิตินี้' อธิบายว่ายอดในหมวด Monitor เป็นคนละชุดกับตัวเลข 'ตั้งแต่ Apply ล่าสุด' ด้านบน และอาจคลาดเคลื่อนได้สูงสุดตามรอบบันทึก (~5 นาที) หากไฟดับกะทันหัน\n5) ต้องเคารพ docs/rules_of_work.md: ห้าม hardcode สี tailwind แบรนด์/สถานะ (ใช้ text-primary, text-destructive, text-muted-foreground), ห้าม shadow-*/backdrop-blur-*, รองรับ dark/light, ใช้เฉพาะ components/ui ; Drawer ตัวนี้ไม่มี Combobox จึงคง modal behavior เดิมไว้\n6) ถ้าเป็นผู้ใช้ role อ่านอย่างเดียว/โหมด -disable-edit ให้ disable Switch และปุ่มรีเซ็ตตาม pattern ที่หน้านี้ใช้อยู่แล้ว",
    "acceptance": ["yarn build ผ่าน", "yarn lint ผ่าน", "ไม่มี class สีดิบ/shadow/backdrop-blur เพิ่มเข้ามา", "การปิด Monitor และการรีเซ็ตมี confirm dialog ทั้งคู่"],
    "depends_on": ["T-15"]
  },
  {
    "task_id": "T-17",
    "title": "build รวมทั้งระบบ",
    "layer": "repo",
    "files": ["build.sh"],
    "instruction": "รัน bash build.sh ให้ผ่านตลอดสาย (frontend build → copy dist → backend build) เพื่อยืนยันว่า embed ไม่พัง และไม่มี type/compile error ค้างจาก Task ก่อนหน้า",
    "acceptance": ["bash build.sh สำเร็จ ได้ไบนารี ./pigate", "cd backend && go test ./... ผ่านทั้งหมด"],
    "depends_on": ["T-13", "T-14", "T-16"]
  }
]
```

---

## 4. ข้อควรระวัง (Cautions)

1. **ห้ามขยับโครง 4 section ของ input chain** ใน `real_firewall.go` (sanity → audit → dynamic accepts → final drop) แผนนี้แตะเฉพาะ resolve helper + recorder เท่านั้น ไม่เพิ่ม/ลด/สลับ rule
2. **sort ก่อน cap เป็นเงื่อนไขบังคับ** ไม่ใช่ทางเลือก — ถ้าลืม จะเกิด reapply loop ทุก tick กับโดเมนที่มี A record > 8 ตัว
3. **applyMu ต้องแยกจาก s.mu** ของ FirewallService (s.mu ถูกจับใน `recordApply` ที่รันผ่าน defer ภายใน SyncFirewallRules)
4. **ห้ามให้ FlushBeforeApply ทำให้ apply ล้ม** — firewall ต้อง apply ได้เสมอแม้ SQLite มีปัญหา
5. **FQDNRefresher ต้องเช็ค `bus.IsPaused()`** ไม่งั้นจะไป reapply ชนกับการ import backup
6. **ห้ามเขียน DB เมื่อ delta เป็นศูนย์** (SD card write cycle, tech_stack_design.md §8)
7. `monitored` เป็น config → ต้องไป export/import ด้วย; ส่วนตัวเลข counter เป็น runtime → **ห้าม** export
8. เลข monitored bytes ต้องไม่ถูกนำไปคำนวณ `percent`/`totalBytes` ของ response เดิม (คนละหน่วยเวลา)
9. เอกสาร openapi ต้องอัปเดตพร้อมโค้ด ไม่ปล่อยให้ drift

---

## 5. Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก Task เสร็จ — สำหรับ ai-qa)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... ผ่าน และ go vet ./... ไม่มี error",
    "cd backend && go test ./... ผ่านทั้งหมด (รวมเทสเดิมทั้งหมดที่มีอยู่ก่อนหน้า ต้องไม่มีตัวไหน fail/skip เพิ่ม)",
    "cd backend && go test -race ./internal/service/... ผ่าน (ไม่มี data race จาก ticker ใหม่ 2 ตัว)",
    "cd frontend && yarn build และ yarn lint ผ่าน",
    "bash build.sh สำเร็จ ได้ไบนารี ./pigate",
    "รัน ./pigate-backend -mock=true -db=<temp>.db แล้ว: บูตผ่านไม่ panic, log แสดงว่า PolicyCounterStore start และ FQDNRefresher ถูกข้าม/ไม่ทำงานใน mock mode, GET /api/policies/stats คืน 200 พร้อม field monitored/monitoredBytes/monitoredPackets ครบ",
    "รันด้วย DB ไฟล์เดิมที่สร้างจาก main (ก่อน merge) แล้ว migration ผ่าน: firewall_policies มีคอลัมน์ monitored (default 0) และมีตาราง policy_rule_counters; รันซ้ำอีกรอบไม่ error (idempotent)",
    "FQDN retry — จำลองเคสบูตไม่มี DNS: ตั้ง address object ชนิด fqdn ที่ใช้ในกฎที่ enable, ทำให้ resolve ไม่ได้ตอน apply แรก (เช่นชี้ resolver ไปที่ที่ไม่ตอบ) จากนั้นคืน DNS ให้ปกติ → ภายใน ~30 วินาที ต้องเห็น log/event ว่า FQDN เปลี่ยนและมีการ SyncFirewallRules อัตโนมัติหนึ่งครั้ง และกฎในเคอร์เนลมี IP ของโดเมนนั้นจริง",
    "FQDN steady state — เมื่อ FQDN ทุกตัว resolve ได้และผลลัพธ์ไม่เปลี่ยน ต้องไม่มี SyncFirewallRules เกิดขึ้นเลยตลอด 10 นาที (ตรวจจาก log ของ RealFirewall/countersSince ที่ต้องไม่ขยับ) และ cadence สลับเป็น 300 วินาทีตามที่ออกแบบ",
    "FQDN หลาย A record — โดเมนที่มี A record มากกว่า 8 ตัว ต้องไม่ทำให้เกิด reapply ซ้ำๆ ทุกรอบ (พิสูจน์ว่า sort+cap ทำงาน)",
    "Monitor persist ข้าม Apply — เปิด Monitor ที่กฎหนึ่ง, สร้างทราฟฟิกให้กฎนั้น, กด Apply Settings → ยอดสะสมในหมวด Monitor ต้อง 'ไม่' กลับไปเป็น 0 และต้อง 'เพิ่มขึ้นแบบต่อเนื่อง' (ตัวเลข 'ตั้งแต่ Apply ล่าสุด' ด้านบนยังรีเซ็ตเป็น 0 ตามเดิมได้ ถือว่าถูกต้อง)",
    "Monitor persist ข้ามการรีสตาร์ท — restart process แล้วยอดสะสมยังอยู่ครบ (ค่าจาก SQLite) และ 'เก็บมาตั้งแต่' ไม่เปลี่ยน",
    "Monitor ไม่นับกฎที่ปิด Monitor — กฎที่ monitored=false ต้องไม่มีแถวใน policy_rule_counters และไม่มียอดแสดง",
    "รีเซ็ตค่า — กดปุ่มรีเซ็ตต้องขึ้น confirm dialog ก่อนเสมอ; กดยืนยันแล้วยอดกลับเป็น 0 และ 'เก็บมาตั้งแต่' อัปเดตเป็นเวลาปัจจุบัน; กดยกเลิกแล้วข้อมูลต้องไม่เปลี่ยน",
    "ปิด Monitor — ต้องขึ้น confirm dialog ที่บอกชัดว่าข้อมูลจะหายถาวร; ยืนยันแล้วแถวใน policy_rule_counters ถูกลบและ UI ไม่แสดงยอดอีก",
    "ลบกฎที่ monitored อยู่ → แถวใน policy_rule_counters หายตาม (FK cascade) ไม่มีแถวกำพร้าค้าง",
    "SD card write — ในสภาวะไม่มีทราฟฟิกเข้ากฎที่ monitored เลย ต้องไม่มี write transaction ลง policy_rule_counters เกิดขึ้นตลอดหลายรอบ flush",
    "Export/Import — export config ได้ไฟล์ที่มี field monitored ของแต่ละกฎ, import ไฟล์ backup 'รุ่นเก่า' ที่ไม่มี field นี้ต้องผ่านโดยได้ค่า false, และหลัง import ยอดสะสมใน UI ตรงกับ DB (ไม่ค้างค่าเก่าในแคช RAM)",
    "สิทธิ์ — ผู้ใช้ role admin_readonly หรือโหมด -disable-edit ต้องถูกปฏิเสธ (403) ที่ POST /api/policies/{id}/toggle-monitor และ /monitor/reset และ UI ต้อง disable ปุ่ม/สวิตช์",
    "ไม่มี session → เรียก endpoint ใหม่ทั้งสองได้ 401",
    "Regression firewall — หลังทุกอย่างทำงาน ตรวจ nft ruleset ว่ายังคงโครง 4 section ของ input chain เดิม, admin access ยังมาก่อน user rule, port-forward/NAT ยังทำงาน, และไม่มีจำนวนกฎเพิ่มขึ้นจาก baseline ก่อน merge (นอกเหนือจากที่ FQDN resolve ได้เพิ่ม)",
    "Regression UI — หน้า Firewall Policy ทั้ง 3 chain ยังใช้งานได้ครบ (สร้าง/แก้/ลบ/reorder/toggle log/status), Drawer สถิติแสดงข้อมูลเดิมครบทุกช่อง, ทั้ง dark และ light mode"
  ]
}
```

---

## 6. เรื่องที่ต้องให้เจ้าของโปรเจกต์ตัดสิน (ก่อนเริ่ม T-01)

1. **ค่า default ของ interval** — เสนอ 30 วินาที (retry) / 300 วินาที (steady) / 300 วินาที (flush counter) ตามเหตุผลใน D-3 ยืนยันหรือปรับ?
2. **จุดวางฮุก flush counter** — issue ระบุ `real_firewall.go` แต่แผนเสนอย้ายไป `FirewallService.SyncFirewallRules()` (D-4) เพื่อลด merge conflict และรักษา layering ยืนยันไหม?
3. **การ sort ผลลัพธ์ FQDN ก่อน cap 8 ตัว** (D-2) เปลี่ยนพฤติกรรมเดิมเล็กน้อยสำหรับโดเมนที่มี A record > 8 ตัว — ยอมรับได้ไหม (ถ้าไม่ ต้องเพิ่ม cap เป็นเทียบแบบ set-based ซึ่งซับซ้อนกว่า)
4. **สิทธิ์ของ endpoint ใหม่** — เสนอ `authRoute` (ระดับเดียวกับ toggle-log) ไม่ใช่ `superAdminRoute` ยืนยันไหม?
