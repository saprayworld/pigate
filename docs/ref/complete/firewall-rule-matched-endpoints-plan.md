# Firewall Policy: IP / Service ที่ตรงกับกฎแต่ละข้อ (Matched Endpoints) — Work Plan

โจทย์จากเจ้าของโปรเจกต์: ในหน้ากฎไฟร์วอล ตรงข้อมูลการใช้งานกฎ (rule usage stats) อยากเห็นว่ากฎนั้น **มี IP Address / Port-Service อะไรบ้างที่โดนกฎนี้** เพื่อใช้ troubleshoot โดยแสดงเป็น IP หรือ "ชื่อที่ตั้งไว้" (alias/label ของ host หรือ IP group ถ้ามี) รวมทั้งชื่อ service/port ด้วย

สถานะ: **พัฒนาเสร็จสิ้นครบทุก Task (T-01 ถึง T-13)** — รอ ai-qa ตรวจสอบและรอผู้ใช้สร้าง PR เข้า `main` (ยังไม่ทราบเลข PR ณ ตอนที่อัปเดตเอกสารนี้ — ให้ผู้ merge เติมเลข PR ที่นี่หลัง PR ถูกสร้าง/merge จริง)

---

## 1. สภาพปัจจุบัน (ข้อเท็จจริงจากโค้ด ไม่ใช่การเดา)

- ฟีเจอร์ usage stats เดิม (PR #135) ประกอบด้วย
  - `model.PolicyRuleStat` / `PolicyRuleStats` (`backend/internal/model/types.go:700`)
  - `service/policy_stats.go` → `PolicyStatsService.GetPolicyRuleStats(chain)` merge 3 แหล่ง: nft counter snapshot (`TrafficStatsService.RuleCounterSnapshot`), ring buffer (`RingBuffer.LastMatchedByRule`), poll last-hit (`RuleLastHits`)
  - `GET /api/policies/stats` (`router.go:117`, authRoute) → `api.HandleGetPolicyStats` (`handlers.go:1619`) ต่อผ่าน setter `SetPolicyStatsService` (`handlers.go:68`) — **`NewServer` มี 30 พารามิเตอร์ ห้ามเพิ่ม**
  - Frontend: `services/policyStatsService.ts`, `components/policy/PolicyChainPage.tsx` (คอลัมน์ Usage), `components/policy/RuleStatsDrawer.tsx` (Drawer รายละเอียด — **จุดที่ฟีเจอร์นี้จะไปเพิ่ม**)
- **nftables counter บอกได้แค่ bytes/packets รวมต่อกฎ ไม่มีทางแตกเป็นราย IP/port ได้** ถ้าไม่รื้อไปทำ named set + per-element counter (แก้ `real_firewall.go` ซึ่งเป็น path ที่ sensitive ที่สุด และก็ยังได้แค่ element ที่เราตั้งไว้ ไม่ใช่ IP จริงที่วิ่งเข้ามา) ⇒ **แหล่งข้อมูลเดียวที่รู้ว่า "แพ็กเก็ตไหนโดนกฎไหน" คือ traffic log ring buffer**
- Ring buffer (`internal/logs/ringbuffer.go`) ปัจจุบันมี `Add / GetAll / Size / LastMatchedByRule / Capacity / Clear / Subscribe` — `LastMatchedByRule` เป็น precedent ที่ดี: สแกนทั้งบัฟเฟอร์ครั้งเดียวใต้ `RLock` โดยไม่ copy ทั้งก้อน
- `model.FirewallLog` (`types.go:385`) มีครบที่ต้องใช้: `Src / Dest / SrcPort / Port / Proto / InIface / OutIface / Action / Chain / RuleID / RuleName / Time` (+ `SrcDomain/DestDomain/SrcHostname/DestHostname` ที่ enrich ตอนอ่านเท่านั้น) — **ไม่มีขนาดแพ็กเก็ต** ⇒ นับได้แค่ "จำนวน log event" ไม่ใช่ bytes
- กฎของผู้ใช้ที่เปิด Log **ไม่ถูก rate-limit** (log ลง NFLOG ใน RAM ไม่ใช่ printk — `real_firewall.go:1060-1068`) แต่ log เกิดเฉพาะแพ็กเก็ตที่ **เปิด connection ใหม่** (`ct state established,related accept` อยู่ก่อนหน้า) และแพ็กเก็ตที่ถูก DROP ⇒ ตัวเลขที่ได้คือ "จำนวน connection/แพ็กเก็ตที่ถูกบันทึก" ไม่ใช่ทุกแพ็กเก็ต (ข้อความนี้เขียนไว้แล้วใน `pages/ForwardTraffic.tsx`)
- **กฎที่ไม่เปิด Log จะไม่มีข้อมูลนี้เลย** — ข้อจำกัดพื้นฐานของฟีเจอร์ ต้องบอกผู้ใช้ตรงๆ ใน UI พร้อมชี้ทางแก้ (เปิด Log ที่กฎนั้น)
- "ชื่อที่ตั้งไว้" ที่มีอยู่ในระบบตอนนี้มี 3 ชั้น และ **มีของพร้อมใช้หมดแล้ว**
  1. `model.AddressObject` (name + type `subnet`/`range`/`fqdn` + value) — คือ "IP group / alias" ที่ผู้ใช้ตั้งเอง; `PolicyRule.Source/Destination` เก็บเป็น **ชื่อ** ของ address object (ดู `real_firewall.go:106-108` ที่ทำ `addrsMap[a.Name]`)
  2. DHCP lease / hostname → `TrafficStatsService.LookupHostnames(ips)` (batch, มี cache)
  3. DNS reverse cache → `StatisticsService.LookupDomains(ips)` (batch)
  โดย (2)(3) คือชุดเดียวกับที่ `api.enrichTrafficLogs` (`handlers.go:781-843`) ใช้อยู่แล้ว — **ต้อง batch เท่านั้น ห้ามเรียกต่อแถว** (คอมเมนต์ในโค้ดระบุเหตุผลไว้ชัด)
- ชื่อ service จาก port: มีตัวจับคู่ canonical อยู่แล้ว `TrafficStatsService.categorize(proto uint8, dstPort uint16)` (`traffic_stats.go:1008`) + cache `categoryEntries()` TTL + `model.ParsePortSpec` — **ห้ามเขียน matcher ใหม่ซ้ำ** (คอมเมนต์ `real_firewall.go:767-773` สั่งไว้)
- `PolicyRule.Service` เก็บชื่อ service object แต่ **อาจมีส่วนต่อท้ายหลังช่องว่าง** (ดู `resolveService` ที่ split ด้วย `" "` แล้วลองใหม่) — ตอนเทียบ "service นี้มาจากกฎเองไหม" ต้องเผื่อ quirk นี้
- Mock: `kernel/mock.go MockTrafficLog` สุ่ม log ทุก 4 วินาที ด้วย `ruleID` ที่ hardcode (`rule-allow-dns`, `rule-allow-web`, …) ซึ่ง **ไม่ตรงกับ id จริงใน DB** ⇒ ในโหมด `-mock=true` แผงใหม่จะว่างเปล่าถ้าไม่แก้อะไร (T-07 แก้จุดนี้)
- **หน้า Traffic Log (สำหรับงาน deep-link T-12/T-13)** — ข้อเท็จจริงที่สำรวจแล้ว
  - route: `/logs/traffic` = Forward (`pages/ForwardTraffic.tsx`), `/logs/local` = Local (`pages/LocalTraffic.tsx`) ประกาศที่ `App.tsx:188-190`
  - ทั้งสองหน้าใช้คอมโพเนนต์เดียวกัน `components/logs/TrafficLogPage.tsx`; ฟิลเตอร์เป็น **state ล้วน ไม่มีการอ่าน URL param เลย**: `action` (`all|PASS|DROP`), `search` (debounce 400ms → `debouncedSearch`) และ chain มาจาก prop `chainParam` / `extraFilter` (เฉพาะหน้า Local มี dropdown `local|input|output` ซึ่ง state อยู่ใน `LocalTraffic.tsx:17`)
  - การเปลี่ยนฟิลเตอร์ทุกชนิดไหลผ่าน `refreshKey` = `${action}|${debouncedSearch}|${effectiveChain}|${clearNonce}` แล้วรีเซ็ต pagination ทั้งหมด
  - **ช่องค้นหา (`q`) ค้นได้เฉพาะ src/dest/srcPort/port/proto/inIface/outIface/reason/chain — ไม่รวมชื่อกฎ/rule id** ทั้งฝั่ง server (`handlers.go:931`) และ client mirror (`TrafficLogPage.tsx` `matchesFilter`) ซึ่งคอมเมนต์กำกับว่า **สองที่นี้ต้องตรงกันเสมอ (lockstep)** ⇒ การ deep-link ด้วย "ชื่อกฎ" จะยังไม่ทำงานถ้าไม่ขยาย haystack ก่อน (จึงต้องมี T-12 แยกออกมาเป็นงาน backend)
- ⚠️ **ความเสี่ยงเรื่องลำดับงาน**: `docs/ref/todo/firewall-log-buffer-capacity-plan.md` (Issue #134) ยังไม่ปรากฏใน working tree (ตรวจแล้ว: ยังไม่มี `traffic-log-buffer-capacity`, ยังไม่มี oldest/evicted ใน ringbuffer.go, capacity ยังเป็น 9 ring) แต่แผนนั้นจะแก้ `internal/logs/ringbuffer.go` เหมือนกัน ⇒ **แผนนี้ควรแตก branch หลัง #134 merge เข้า main แล้ว** เพื่อเลี่ยง conflict ในไฟล์เดียวกัน

---

## 2. Design decisions (เจ้าของโปรเจกต์ยืนยันครบแล้ว — ปิดประเด็น)

| ประเด็น | คำตอบเจ้าของโปรเจกต์ | ผลต่อแผน |
|---|---|---|
| แหล่งข้อมูล: สแกน ring buffer สด (A) หรือทำ aggregator แยก (B)? | **ใช้ Option A** | ข้อ 1 ด้านล่างคือข้อสรุปสุดท้าย ไม่ทำ B ในเฟสนี้ |
| ลำดับชื่อที่แสดงต่อ IP | **`addressName` (ที่ผู้ใช้ตั้ง) ชนะเสมอ → `domain` → `hostname`** | ข้อ 5 ด้านล่าง ยืนยันตามแผนตั้งต้น |
| ปุ่ม deep-link "ดู log ของกฎนี้" | **ต้องการ** | เพิ่ม T-12 (backend: ขยาย `q` ให้ค้นชื่อกฎได้) + T-13 (frontend: URL param + ปุ่ม) |

1. **แหล่งข้อมูล = สแกน ring buffer สดตอนมี request (Option A — ยืนยันแล้ว)** — ไม่เพิ่ม state ถาวร ไม่เพิ่ม goroutine ไม่แตะ hot path ของ NFLOG; ข้อแลกเปลี่ยนที่รับทราบร่วมกันแล้วคือหน้าต่างข้อมูลเท่ากับความจุบัฟเฟอร์ (กฎที่ทราฟฟิกน้อยอาจถูกกฎที่ทราฟฟิกเยอะดันตกออกไป) และผู้ใช้ขยายเองได้ผ่านความจุบัฟเฟอร์ใน `pigate.conf` (Issue #134)
   - *Phase 2 ถ้าจำเป็นภายหลัง*: aggregator ที่ป้อนจาก `stampAndPush` แบบเดียวกับ `StatisticsService.RecordFirewallLog` — **API shape ตาม §3 ใช้ต่อได้ ไม่ต้องแก้ frontend**
2. **หน่วยนับคือ "จำนวน log entry" ไม่ใช่ bytes** — NFLOG payload ที่ parse ไว้ไม่มีขนาดแพ็กเก็ต และการไปแกะ IP total length เพิ่มถือว่าเกินขอบเขต; bytes รวมต่อกฎมีอยู่แล้วในแผง stats เดิม
3. **Endpoint ใหม่แยกต่างหาก ต่อกฎ**: `GET /api/policies/{id}/endpoints?limit=N` — ไม่ยัดเข้า `/api/policies/stats` (payload ของหน้ารายการต้องเล็ก และการสแกน buffer ต่อกฎทุกกฎในทุก request แพงเกินจำเป็น) เปิด drawer ทีละกฎอยู่แล้ว
4. **สิทธิ์ = authRoute** เท่ากับ `/api/logs/traffic` และ `/api/policies/stats` (ข้อมูลชุดเดียวกับหน้า Traffic Log ที่ทุก role ที่ล็อกอินดูได้อยู่แล้ว) และเป็น GET ⇒ ใช้ได้ภายใต้ `-disable-edit=true`
5. **ลำดับชื่อที่แสดงต่อ IP (ยืนยันแล้ว)**: backend คืน `addressName` / `domain` / `hostname` แยกฟิลด์เสมอ (พร้อม `ip` ดิบ) แล้ว frontend แสดง **`addressName ?? domain ?? hostname`** เป็นบรรทัดบน และ IP ดิบเป็นบรรทัดล่างเสมอ — ชื่อ Address Object ที่ผู้ใช้ตั้งเองชนะโดเมนจาก DNS เสมอ (ต่างจากหน้า Traffic Log ที่ domain มาก่อน hostname และไม่รู้จัก address object เลย — ตั้งใจให้ต่าง เพราะโจทย์คือ "ชื่อที่ผู้ใช้ตั้งไว้")
6. **จับคู่ Address Object แบบ "แคบสุดชนะ"** (prefix ยาวสุด) และทำเฉพาะ type `subnet`/`range`; type `fqdn` ข้าม (ค่าเป็นชื่อโดเมน ไม่ใช่ช่วง IP — ตรงกับพฤติกรรม nftables ที่ resolve ตอน apply) พร้อมธง `fromRule=true` เมื่อ address object นั้นคือหนึ่งใน `Source`/`Destination` ของกฎนี้เอง (ตัวช่วย troubleshoot: "โดนเพราะ object ตัวไหนที่ฉันตั้ง")
7. **ไม่ persist อะไรเพิ่ม ไม่มีตาราง/มigration ใหม่** (tech_stack_design.md §8 — ถนอม SD card) และไม่มี `exec.Command`
8. **ไม่แตะ `real_firewall.go` เลย** — งานนี้เป็น read/aggregate ล้วน ห้ามเข้าไปยุ่งกับ path การสร้าง rule
9. **ล้าง log แล้วข้อมูลต้องหายทันที** — เป็นคุณสมบัติด้าน privacy ที่มีอยู่แล้วของ ring buffer ต้องคงไว้ (อีกเหตุผลที่เลือก A)
10. **Deep-link ใช้ช่องค้นหาเดิม (`q`) ไม่สร้างระบบ filter ใหม่** — ขยาย haystack ของ `q` ให้ครอบคลุม `ruleName`/`ruleId` (T-12) แล้ว deep-link ส่งค่าผ่าน URL param `?q=` (+ `?action=`, `?chain=` เฉพาะหน้า Local) ที่ TrafficLogPage อ่านมา seed state (T-13) — วิธีนี้ทำให้ทั้ง server filter และ client mirror ยังคงตรงกันเป๊ะตามคอมเมนต์ในโค้ด

---

## 3. รูปแบบข้อมูล (สัญญาที่ทุก task ยึดร่วมกัน)

```jsonc
// GET /api/policies/{id}/endpoints?limit=10
{
  "ruleId": "rule-xxxx",
  "ruleName": "Allow LAN to WAN",
  "chain": "forward",
  "logEnabled": true,          // rule.Log — false แปลว่าไม่มีทางมีข้อมูล
  "matchedEntries": 482,       // จำนวน log entry ของกฎนี้ที่ยังอยู่ในบัฟเฟอร์
  "uniqueSources": 12,         // นับก่อนตัด top-N
  "uniqueDestinations": 96,
  "uniqueServices": 7,
  "limit": 10,
  "truncated": true,           // มีมากกว่า limit ในอย่างน้อยหนึ่งหมวด
  "scannedEntries": 10000,     // ขนาดบัฟเฟอร์ที่สแกนจริง (ทุก chain รวมกัน)
  "bufferOldestAt": "2026-08-12T03:11:02.5Z", // เวลาของ entry เก่าสุดในบัฟเฟอร์ = ขอบหน้าต่างข้อมูล
  "sources":      [{ "ip": "192.168.1.120", "count": 210, "firstSeenAt": "...", "lastSeenAt": "...",
                     "addressName": "LAN_Clients", "hostname": "notebook-01", "domain": "", "fromRule": true }],
  "destinations": [{ "ip": "142.250.80.46",  "count": 88,  "firstSeenAt": "...", "lastSeenAt": "...",
                     "addressName": "", "hostname": "", "domain": "www.google.com", "fromRule": false }],
  "services":     [{ "proto": "TCP", "port": "443", "count": 301, "firstSeenAt": "...", "lastSeenAt": "...",
                     "serviceName": "HTTPS", "fromRule": true }]
}
```

- ทุกลิสต์เรียง `count` มาก→น้อย, tie-break ด้วย key (ip / proto+port) เพื่อความ deterministic
- `limit` default 10, min 1, max 50 (เกินช่วง → 400)
- ไม่พบ rule id → 404; rule ที่ Disabled ก็ยังตอบ 200 ได้ (อาจมี log เก่าค้างในบัฟเฟอร์) แต่ UI ต้องบอกว่าเป็นข้อมูลย้อนหลัง
- ICMP: `port` เป็น `"-"` ตามที่ NFLOG parser ใส่มา — ห้ามแปลงเป็น 0

---

## 4. Task list

| Task | Layer | ไฟล์หลัก | depends_on |
|---|---|---|---|
| T-01 | model | `backend/internal/model/types.go` | — |
| T-02 | logs | `backend/internal/logs/ringbuffer.go` (+ test) | T-01 |
| T-03 | service | `backend/internal/service/traffic_stats.go` | — |
| T-04 | service | `backend/internal/service/policy_endpoint_labels.go` (ใหม่, + test) | T-01 |
| T-05 | service | `backend/internal/service/policy_endpoints.go` (ใหม่, + test) | T-02, T-03, T-04 |
| T-06 | api | `backend/internal/api/handlers.go`, `router.go` (+ test) | T-05 |
| T-07 | kernel | `backend/internal/kernel/mock.go`, `backend/cmd/pigate/main.go` | T-06 |
| T-08 | docs | `docs/openapi.yaml`, `frontend/public/openapi.yaml` | T-06 |
| T-09 | frontend | `frontend/src/services/policyEndpointsService.ts` (ใหม่) | T-08 |
| T-10 | frontend | `frontend/src/components/policy/RuleStatsDrawer.tsx` | T-09 |
| T-12 | api | `backend/internal/api/handlers.go` (+ test), `docs/openapi.yaml`, `frontend/public/openapi.yaml` | — |
| T-13 | frontend | `frontend/src/components/logs/TrafficLogPage.tsx`, `pages/LocalTraffic.tsx`, `components/policy/RuleStatsDrawer.tsx` | T-10, T-12 |
| T-11 | docs | `docs/data/firewall.md` | T-13 |

> โจทย์ "T-12" จากเจ้าของโปรเจกต์ (ปุ่ม deep-link) ถูกแตกเป็น **2 task** เพราะมีทั้งส่วน backend และ frontend: T-12 = ขยายการค้นหาให้ครอบคลุมชื่อกฎ (ไม่งั้น deep-link จะกดแล้วได้ผลลัพธ์ว่าง), T-13 = URL param + ปุ่ม. T-11 (เอกสาร) ถูกเลื่อนไปเป็น task สุดท้าย

### T-01 — DTO ของ matched endpoints
- **instruction**: เพิ่ม struct ใหม่ใน `model/types.go` ต่อท้ายบล็อก `PolicyRuleStat`/`PolicyRuleStats`: `EndpointHit` (ip, count, firstSeenAt, lastSeenAt, addressName, hostname, domain, fromRule), `ServiceHit` (proto, port, count, firstSeenAt, lastSeenAt, serviceName, fromRule), `PolicyRuleEndpoints` ตามสัญญาใน §3 (json tag ตรงเป๊ะ) พร้อม doc comment อธิบายข้อจำกัด: มาจาก traffic log เท่านั้น / ต้องเปิด Log / นับเป็นจำนวน log entry ไม่ใช่ bytes / หน้าต่างข้อมูลเท่ากับบัฟเฟอร์
- **ห้าม**: แตะ `PolicyRule`, `PolicyRuleStat`, `FirewallLog` เดิม (ถูกใช้ใน create/update/backup export)
- **acceptance**: `go build ./...` ผ่าน; ไม่มี struct เดิมถูกแก้

### T-02 — สแกน ring buffer แบบครั้งเดียวต่อกฎ
- **instruction**: เพิ่ม `func (r *RingBuffer) AggregateByRule(ruleID string) RuleAggregate` ใน `internal/logs/ringbuffer.go` โดยยึด precedent ของ `LastMatchedByRule` (สแกนใต้ `RLock` ครั้งเดียว ไม่ copy ทั้งก้อน) คืน struct ในแพ็กเกจ `logs` ที่มี: `Sources map[string]Counted`, `Dests map[string]Counted`, `Services map[string]Counted` (key = `PROTO/PORT`), `Matched int`, `Scanned int`, `OldestTime string` โดย `Counted{Count int; FirstSeen, LastSeen string}` — ใช้ค่า `entry.Time` ตามที่เก็บ (string, RFC3339/RFC3339Nano ปนกันได้) ไม่ parse ในชั้นนี้ เพราะ buffer เรียงเวลาอยู่แล้ว: entry แรกที่เจอ = FirstSeen, entry สุดท้าย = LastSeen (สแกนจากเก่า→ใหม่)
- **ข้อควรระวัง**: ห้ามคืน slice ที่อ้างถึง `r.logs` ภายใน; ข้าม `Src`/`Dest` ที่เป็น `"-"`; ห้ามใส่ logic การจัดอันดับ/ตัด top-N/แปลชื่อ ในแพ็กเกจ `logs` (เป็นงานของ service layer)
- **acceptance**: มี unit test ใน `internal/logs/` ครอบคลุม: ไม่มี entry ที่ตรง (ค่าว่างทั้งหมด, ไม่ panic), นับซ้ำถูกต้อง, first/last seen ถูกด้าน, buffer ว่าง, entry ที่ `RuleID==""` ไม่ถูกนับ; `go test ./internal/logs/...` ผ่าน

### T-03 — ตัวจับคู่ port → ชื่อ Service Object (ใช้ของเดิม)
- **instruction**: เพิ่ม method ที่ export บน `TrafficStatsService` ใน `traffic_stats.go`: `ServiceNameFor(proto string, port string) string` ที่แปลง `"TCP"/"UDP"/"ICMP"/"ICMPv6"/"proto-N"` + port string → เรียก `categorize()` เดิม แล้วคืน `""` แทน `"Other"` เมื่อไม่ match
- **ห้าม**: แก้ `categorize` / `categoryEntries` / `buildCategoryEntries` เดิม (ใช้โดย Dashboard) และห้ามเขียน matcher port ใหม่ซ้ำ
- **acceptance**: `go test ./internal/service/...` เดิมยังผ่าน; มี test สั้นๆ ยืนยัน `"TCP"/"443"` → ชื่อ service object และ port ที่ไม่มีใครจับ → `""`

### T-04 — ตัวจับคู่ IP → ชื่อ Address Object (pure function + test)
- **instruction**: ไฟล์ใหม่ `service/policy_endpoint_labels.go` ใส่ฟังก์ชัน pure (ไม่แตะ DB/kernel): `type addrMatcher` ที่สร้างจาก `[]model.AddressObject` ครั้งเดียวต่อ request แล้ว `Match(ip string) (name string, ok bool)` — รองรับ `subnet` (มี/ไม่มี `/prefix`), `range` (`a-b` เทียบแบบ big-endian), ข้าม `fqdn` และค่า parse ไม่ผ่าน (ข้ามเงียบๆ ห้าม error ทั้ง request); เลือก **prefix แคบสุดชนะ** ถ้าเสมอให้เรียงชื่อ ascii; ใช้ `net/netip` (stdlib) ไม่เพิ่ม dependency; รองรับ IPv6 อย่างน้อยแบบไม่ panic
- **หมายเหตุ security**: เป็น input จากภายนอก (IP จาก log) + config ผู้ใช้ ⇒ ต้องกัน panic ทุกกรณี (ค่าพัง, ค่าว่าง, IPv4-mapped IPv6)
- **acceptance**: unit test ครอบคลุม subnet /32, /24, bare IP, range, ค่าพัง, fqdn, ตัวเลือกซ้อนกัน (เลือกแคบสุด), IPv6; `go test ./internal/service/...` ผ่าน

### T-05 — ประกอบผลลัพธ์ต่อกฎ
- **instruction**: ไฟล์ใหม่ `service/policy_endpoints.go` เพิ่ม method บน `PolicyStatsService` ที่มีอยู่แล้ว: `GetRuleEndpoints(ruleID string, limit int) (model.PolicyRuleEndpoints, error)` ทำตามลำดับ
  1. หา rule จาก `repo.GetPolicies()` — ไม่เจอคืน sentinel `ErrPolicyRuleNotFound` (ประกาศในไฟล์นี้) ให้ api แปลงเป็น 404
  2. `s.ringBuffer.AggregateByRule(rule.ID)` (nil-safe)
  3. ตัด top-N ตาม limit หลังจากนับ unique แล้ว, เรียง count desc + key asc
  4. enrich ชื่อ: `addrMatcher` (จาก `repo.GetAddresses()`), `s.trafficStats.LookupHostnames(ips)` และ domain ผ่าน field ใหม่ `domainLookup func([]string) map[string]string` ที่ set ด้วย setter `SetDomainLookup(...)` (**ห้ามเพิ่มพารามิเตอร์ให้ `NewPolicyStatsService`**) — ทั้งสอง lookup ต้องเรียก **ครั้งเดียวต่อ request แบบ batch** ด้วย IP ที่เหลือหลังตัด top-N แล้วเท่านั้น
  5. `serviceName` ผ่าน `trafficStats.ServiceNameFor` (T-03); `fromRule` ของ address = ชื่อ object อยู่ใน `rule.Source`/`rule.Destination`, ของ service = ชื่ออยู่ใน `rule.Service` (เทียบทั้งสตริงเต็มและส่วนก่อนช่องว่างแรก ตาม quirk ของ `resolveService`)
- **ห้าม**: เรียก kernel/nftables, เพิ่ม goroutine/ticker, เรียก `repo` ต่อแถว, ใช้ `GetAll()` ของ ring buffer
- **acceptance**: unit test (repo ในหน่วยความจำแบบเดียวกับ `policy_stats_test.go`) ครอบคลุม: rule ไม่มีจริง → sentinel error, กฎที่ไม่มี log → ลิสต์ว่างแต่ไม่ error, top-N + `truncated`, `fromRule` ถูกทั้งฝั่ง address และ service, dependency เป็น nil ทุกตัวแล้วไม่ panic; `go test -race ./internal/service/...` ผ่าน

### T-06 — API endpoint
- **instruction**: handler `HandleGetPolicyRuleEndpoints` ใน `api/handlers.go` + route `authRoute("GET /api/policies/{id}/endpoints", ...)` ใน `router.go` (วางต่อจาก `/api/policies/stats`) — validate `limit` (default 10, 1–50, ไม่ใช่ตัวเลข/นอกช่วง → 400), `s.policyStats == nil` → 503, sentinel not-found → 404, error อื่น → 500
- **ข้อควรระวัง (sensitive)**: `{id}` มาจากผู้ใช้ ใช้เป็น key เทียบใน map/DB เท่านั้น ห้ามนำไปประกอบ query string ดิบ; ห้ามใส่ข้อมูลกฎอื่นในผลลัพธ์; ยืนยันว่าไม่ชนกับ route `PUT/DELETE /api/policies/{id}` ที่มีอยู่
- **acceptance**: test ใน `api/` (แนวเดียวกับ `policy_stats_handler_test.go`): 200 + shape ถูก, 400 limit พัง, 404 id มั่ว, 401 ไม่มี session, 503 เมื่อไม่ได้ set service; `go test ./...` ผ่านทั้ง repo

### T-07 — ให้โหมด mock มีข้อมูลจริงให้ทดสอบ
- **instruction**: เพิ่ม hook แบบ opt-in บน `MockTrafficLog` เช่น `SetRuleIDProvider(func() []string)` — ถ้าไม่ set ให้พฤติกรรมเดิมทุกประการ; ถ้า set ให้สุ่ม ruleID จริงจากลิสต์นั้นแทน id hardcode เป็นบางส่วน (คงเคสเดิมไว้อย่างน้อย 1 อัน: id ที่ resolve ไม่ได้ และ 1 อัน: id ว่าง) แล้ว wiring ใน `cmd/pigate/main.go` เฉพาะ path mock ให้ provider อ่านรายการ id ของ policy rule จาก repo (อ่านครั้งเดียวตอน start ก็พอ)
- **ห้าม**: ให้แพ็กเกจ `kernel` import `db`; แก้ interface `TrafficLogManager`; กระทบ path ที่ไม่ใช่ mock
- **acceptance**: `./pigate-backend -mock=true` แล้วเปิด drawer ของกฎที่เปิด Log เห็นข้อมูลจริงภายใน ~1 นาที; `go build ./...` + test เดิมผ่าน

### T-08 — OpenAPI
- **instruction**: เพิ่ม path + schema ให้ตรง DTO ของ T-01 ทั้ง `docs/openapi.yaml` และ `frontend/public/openapi.yaml` (สองไฟล์ต้องเหมือนกัน) พร้อม description ระบุข้อจำกัด 4 ข้อ (ต้องเปิด Log / นับเป็น log entry ไม่ใช่ bytes / หน้าต่างเท่าบัฟเฟอร์และหายเมื่อ Clear / เห็นเฉพาะ connection ใหม่และแพ็กเก็ตที่ถูก DROP)
- **ห้าม**: แก้ `backend/internal/api/dist/openapi.yaml` (เป็น build artifact)
- **acceptance**: YAML ถูกต้อง, หน้า ApiDocs เรนเดอร์ได้, ชื่อฟิลด์ตรงกับ Go json tag ทุกตัว

### T-09 — Frontend API client
- **instruction**: ไฟล์ใหม่ `services/policyEndpointsService.ts` ประกาศ type ตรงกับ DTO + `getRuleEndpoints(ruleId, limit?)`; รองรับ `IS_MOCK_MODE` ด้วยข้อมูลสังเคราะห์แบบ deterministic ต่อ rule id (แนว `policyStatsService.ts`) ให้มีทั้งเคสมี addressName, มี domain, มี hostname, ไม่มีชื่อเลย, `truncated=true`, และเคส `logEnabled=false`
- **acceptance**: `yarn build` + `yarn lint` ผ่าน; ไม่มี `any`

### T-10 — UI ใน RuleStatsDrawer
- **instruction**: เพิ่มส่วน "Endpoints ที่ตรงกับกฎนี้" ใน `components/policy/RuleStatsDrawer.tsx` (drawer เดิม เปิดจากปุ่มในแถวของ `PolicyChainPage.tsx` — พยายามไม่แก้ `PolicyChainPage.tsx` เลย) ประกอบด้วย
  - fetch เมื่อ `open && rule` เท่านั้น, refresh ทุก 10 วินาทีระหว่างเปิด, ยกเลิก request/timer เมื่อปิด drawer หรือ unmount (ห้ามมี request ค้าง)
  - 3 กลุ่ม: Top Source / Top Destination / Top Service — แต่ละแถว: บรรทัดบน = ชื่อ (`addressName ?? domain ?? hostname` ตาม §2 ข้อ 5 หรือ `serviceName`), บรรทัดล่าง = IP ดิบ / `PROTO/PORT`, ขวา = จำนวนครั้ง + เวลาเห็นล่าสุดแบบ relative (`lib/relativeTime.ts` ที่มีอยู่แล้ว); badge เล็กเมื่อ `fromRule`
  - สถานะพิเศษ: `logEnabled=false` → ข้อความชี้ทางแก้ว่าให้เปิด Log ที่กฎนี้ (ไม่ใช่ error), ไม่มีข้อมูล → empty state, `truncated` → หมายเหตุว่าแสดงเฉพาะ top N, แสดง `bufferOldestAt` เป็น "ข้อมูลย้อนหลังถึง …"
  - เพิ่มข้อจำกัดข้อ 5-7 ต่อจากรายการข้อจำกัด 4 ข้อเดิมในกล่อง Info; ขยายความกว้าง drawer ได้ถ้าจำเป็น
- **ข้อบังคับสไตล์**: ใช้เฉพาะ `components/ui/*`, ห้ามสี hardcode (`text-emerald-500` ฯลฯ) ใช้ theme variable, ห้าม `shadow-*`/`backdrop-blur-*`, รองรับ dark/light, drawer นี้ไม่มี Combobox จึงคง modal behavior เดิม
- **acceptance**: `yarn build` + `yarn lint` ผ่าน; ตรวจด้วยตาในโหมด mock ทั้ง dark/light

### T-12 — ให้ช่องค้นหาของ Traffic Log ค้น "ชื่อกฎ/rule id" ได้ (backend, ต้องทำก่อน deep-link)
- **บริบท**: ตอนนี้ `q` ค้นได้เฉพาะ `src/dest/srcPort/port/proto/inIface/outIface/reason/chain` ทั้งที่ตารางมีคอลัมน์ Rule แสดงอยู่ ⇒ deep-link ด้วยชื่อกฎจะได้ผลลัพธ์ว่างถ้าไม่แก้ก่อน
- **instruction**: ใน `api/handlers.go` `HandleGetTrafficLogs` เพิ่ม `entry.RuleName` และ `entry.RuleID` เข้าไปใน haystack ของ `q` (บรรทัดที่ประกอบ `hay := strings.ToLower(...)`, ปัจจุบัน `handlers.go:931`) และอัปเดต doc comment ของ handler (บรรทัดอธิบายพารามิเตอร์ `q`) ให้ตรง; อัปเดต description ของพารามิเตอร์ `q` ใน `docs/openapi.yaml` + `frontend/public/openapi.yaml` ให้ตรงกัน
- **ห้าม**: เปลี่ยน semantic อื่นของ filter (action/chain/cursor) หรือเพิ่ม query param ใหม่; แก้ `backend/internal/api/dist/openapi.yaml`
- **acceptance**: มี test ใน `api/traffic_logs_test.go` (หรือไฟล์ test เดิมของ handler นี้) ยืนยันว่า `q=<ชื่อกฎ>` คืนเฉพาะแถวของกฎนั้น, `q=<rule id>` ก็ได้ และ query เดิม (IP/port/reason) ยังให้ผลเท่าเดิม; `go test ./...` ผ่าน

### T-13 — ปุ่ม deep-link "ดู log ของกฎนี้" + URL param ที่หน้า Traffic Log (frontend)
- **instruction (2 ฝั่ง)**
  1. **ฝั่งอ่าน param — `components/logs/TrafficLogPage.tsx`**: ใช้ `useSearchParams` **จาก `"react-router"` เท่านั้น** (ห้าม `react-router-dom`) อ่าน `q` และ `action` มา **seed ค่าเริ่มต้นของ state ครั้งเดียว** (lazy initializer ของ `useState`) เพื่อไม่ให้ทับสิ่งที่ผู้ใช้พิมพ์ทีหลัง; ค่า `action` ที่ไม่อยู่ใน `ACTION_OPTIONS` ให้ตกเป็น `"all"`; ไม่ต้องเขียนค่ากลับลง URL เมื่อผู้ใช้แก้ฟิลเตอร์ (กัน history เละ) — พฤติกรรมเดิมเมื่อไม่มี param ต้องเหมือนเดิมเป๊ะ รวมทั้ง debounce 400ms และการ reset pagination ผ่าน `refreshKey`
  2. **`pages/LocalTraffic.tsx`**: อ่าน `?chain=` (`local|input|output`, ค่าอื่น/ไม่มี → `"local"`) มาเป็นค่าเริ่มต้นของ state `chain` ที่ส่งเข้า `extraFilter` (หน้า Forward ไม่ต้องรับ param นี้ เพราะ chain ตายตัว)
  3. **`components/policy/RuleStatsDrawer.tsx`**: เพิ่มปุ่ม/ลิงก์ "ดู log ของกฎนี้" ที่หัวส่วน Endpoints → ไปที่ `/logs/traffic?q=<ชื่อกฎ>` เมื่อ `rule.chain === "forward"` และ `/logs/local?q=<ชื่อกฎ>&chain=<input|output>` เมื่อเป็น local-in/local-out (ใช้ `Link`/`useNavigate` จาก `"react-router"`, `encodeURIComponent` ทุกค่า, ปิด drawer เมื่อกด); ถ้าชื่อกฎว่างให้ใช้ `rule.id` แทน; เพิ่มลิงก์ระดับแถวด้วย: แถว IP → `?q=<ip>`, แถว service → `?q=<port>` (ICMP ที่ port เป็น `"-"` ให้ลิงก์ด้วย proto แทน)
  4. เขียนหมายเหตุสั้นๆ ใต้ปุ่มว่าเป็นการ **ค้นหาแบบ substring บนชื่อกฎ ณ ตอนที่บันทึก log** ⇒ ถ้ากฎถูกเปลี่ยนชื่อภายหลัง แถวเก่าจะไม่ตรง และชื่อที่เป็น substring ของกฎอื่นอาจติดมาด้วย
- **ห้าม**: สร้างระบบ filter ใหม่/เพิ่ม query param ฝั่ง backend; แก้ `matchesFilter` ให้ต่างจาก server filter (ถ้า T-12 เพิ่ม ruleName/ruleId ใน haystack ฝั่ง server แล้ว **ต้องเพิ่มใน `matchesFilter` ให้ตรงกันด้วย** — คอมเมนต์ในไฟล์กำกับ lockstep ไว้); ใช้สี hardcode/`shadow-*`/`backdrop-blur-*`
- **acceptance**: `yarn build` + `yarn lint` ผ่าน; เปิด `/logs/traffic?q=test&action=DROP` แล้วช่องค้นหา + dropdown verdict ถูก prefill และผลลัพธ์ถูกกรองจริง; `/logs/local?q=x&chain=input` prefill dropdown chain เป็น Local-In; เข้าหน้าเดิมโดยไม่มี param แล้วพฤติกรรมเหมือนเดิมทุกอย่าง; ปุ่มใน drawer พาไปหน้าที่ถูกต้องตาม chain ของกฎ

### T-11 — เอกสารอ้างอิง
- **instruction**: อัปเดต `docs/data/firewall.md` อธิบาย endpoint ใหม่ แหล่งข้อมูล ลำดับการเลือกชื่อ (§2 ข้อ 5) การค้นหาด้วยชื่อกฎที่เพิ่มใน T-12 และข้อจำกัดทั้งหมด; อัปเดตสถานะในหัวไฟล์แผนนี้เป็น "เสร็จสิ้น" พร้อมเลข PR
- **acceptance**: เอกสารตรงกับพฤติกรรมจริงของโค้ดที่ merge

---

## 5. Final acceptance (ทดสอบรวมครั้งเดียวหลังทำครบทุก Task)

```json
{
  "final_acceptance": [
    "cd backend && go build ./... && go vet ./... && go test -race ./... ผ่านทั้งหมด ไม่มี test เดิมพัง",
    "cd frontend && yarn build && yarn lint ผ่าน",
    "-mock=true: GET /api/policies/{id}/endpoints ของกฎที่เปิด Log คืน 200 และมีข้อมูลใน sources/destinations/services ภายใน ~1 นาทีหลัง start",
    "uniqueSources/uniqueDestinations/uniqueServices นับก่อนตัด top-N (มากกว่าหรือเท่ากับความยาวลิสต์เสมอ) และ truncated=true เมื่อมีหมวดใดเกิน limit",
    "limit=0 / limit=51 / limit=abc → 400, rule id ที่ไม่มีจริง → 404, ไม่มี session → 401, ไม่ได้ set service → 503",
    "กฎที่ปิด Log → 200 พร้อม logEnabled=false และลิสต์ว่าง (ไม่ใช่ error) และ UI แสดงข้อความชี้ทางแก้",
    "IP ที่อยู่ใน Address Object ที่ผู้ใช้ตั้งไว้ แสดงชื่อ object นั้นเสมอ (ชนะ domain/hostname) พร้อม badge fromRule เมื่อ object นั้นถูกอ้างในกฎเดียวกัน; IP ที่ไม่อยู่ใน object ใดแสดง domain ก่อน hostname; ไม่มีอะไรเลยก็แสดง IP ดิบอย่างเดียว",
    "port ที่ตรงกับ Service Object แสดงชื่อ service (เช่น 443 → HTTPS); port ที่ไม่ตรงแสดง PROTO/PORT ดิบ; ICMP ไม่ทำให้พัง",
    "กด Clear Logs แล้วข้อมูลในแผงหายทันทีในรอบ refresh ถัดไป โดยไม่ error",
    "เปิด drawer ค้างไว้ 1 นาที ตัวเลขอัปเดตเองโดยไม่กระพริบ/ไม่ reset scroll; ปิด drawer แล้วไม่มี network request หรือ timer ค้าง (ตรวจใน devtools)",
    "Dark/light mode ผ่านทั้งคู่, ไม่มีสี hardcode, ไม่มี shadow-*/backdrop-blur-*",
    "-disable-edit=true: endpoint ยังใช้งานได้ (เป็น GET)",
    "ไม่มี exec.Command ใหม่, ไม่มี SQLite migration/ตารางใหม่, ไม่มี goroutine/ticker ใหม่ในฝั่ง backend, ไม่มีการแก้ real_firewall.go, NewServer/NewPolicyStatsService signature ไม่เปลี่ยน",
    "ตรวจว่าไม่มีการเรียก LookupHostnames/LookupDomains/repo แบบต่อแถว (batch ครั้งเดียวต่อ request)",
    "GET /api/logs/traffic?q=<ชื่อกฎ> คืนเฉพาะแถวของกฎนั้น และ q=<rule id> ก็ได้ ส่วน q เดิม (IP/port/reason/interface/chain) ยังให้ผลเท่าเดิมทุกกรณี",
    "server filter กับ client mirror (matchesFilter ใน TrafficLogPage.tsx) ยังตรงกันเป๊ะ: เปิดหน้า Traffic Log ค้างไว้พร้อมตัวกรองชื่อกฎ แล้วแถวใหม่ที่มาทาง SSE ปรากฏเฉพาะแถวที่ตรงกฎนั้นจริง",
    "เปิด /logs/traffic?q=test&action=DROP แล้วช่องค้นหา + dropdown verdict ถูก prefill และผลลัพธ์ถูกกรองจริง; /logs/local?q=x&chain=input prefill dropdown chain เป็น Local-In; เข้าหน้าเดิมโดยไม่มี param แล้วพฤติกรรมเหมือนเดิมทุกอย่าง (รวมทั้ง debounce และการ reset pagination)",
    "ปุ่ม 'ดู log ของกฎนี้' ใน RuleStatsDrawer พาไป /logs/traffic สำหรับกฎ chain forward และ /logs/local?chain=input|output สำหรับ local-in/local-out พร้อมชื่อกฎใน q แล้วเห็นแถวของกฎนั้นจริง; ลิงก์ระดับแถว (IP / port) ก็กรองได้ถูกต้อง; ทุกค่าใน URL ถูก encodeURIComponent (ทดสอบชื่อกฎที่มีช่องว่างและอักขระพิเศษ)",
    "ทุก import ของ router ในไฟล์ที่แก้มาจาก \"react-router\" ไม่ใช่ \"react-router-dom\"",
    "รัน backend จริงบน Pi (ถ้าทำได้): เปิด drawer ของกฎที่มีทราฟฟิกจริง ตัวเลข count สอดคล้องกับสิ่งที่เห็นในหน้า Forward Traffic เมื่อกดปุ่ม deep-link ของกฎเดียวกัน"
  ]
}
```

---

## 6. ความเสี่ยง / หมายเหตุที่เหลือ

1. **หน้าต่างข้อมูลเท่ากับบัฟเฟอร์** (ผลของ Option A ที่ยืนยันแล้ว) — กฎที่ทราฟฟิกน้อยอาจถูกดันตกออกจากบัฟเฟอร์ก่อนผู้ใช้เปิดดู; ทางแก้ระยะสั้นคือเพิ่มความจุใน `pigate.conf` (Issue #134) ระยะยาวคือ Option B ในเฟสถัดไป
2. **ข้อจำกัด "ต้องเปิด Log ที่กฎ"** แก้ไม่ได้ด้วยวิธีอื่นนอกจากบังคับให้ทุกกฎ log (ผลข้างเคียงด้าน performance/privacy สูง) — แผนนี้เลือกบอกผู้ใช้ใน UI แทน
3. **Sequencing กับ Issue #134** — ทั้งสองแผนแก้ `internal/logs/ringbuffer.go` ⇒ ควรรอ #134 merge ก่อนแตก branch นี้
4. **T-12 แตะ path ที่ผู้ใช้ป้อน input** (`q` ของ traffic log) — เป็นการเทียบ substring ในหน่วยความจำล้วน ไม่มี SQL/exec เข้ามาเกี่ยว แต่ต้องรีวิวว่าไม่ได้เผลอเปลี่ยน semantic ของ filter อื่น และ **client mirror ต้องแก้ให้ตรงกันใน T-13** ไม่งั้นแถวจาก SSE จะรั่วข้ามตัวกรอง
5. **การ deep-link ด้วยชื่อกฎเป็น substring match บน snapshot-on-write** — กฎที่ถูกเปลี่ยนชื่อภายหลังจะหาแถวเก่าไม่เจอ และชื่อที่เป็น substring ของกฎอื่นจะติดมาด้วย (เขียนกำกับใน UI แล้วตาม T-13 ข้อ 4)
