# Traffic Accounting Accuracy Phase 2 — ปิดช่องว่างของ flow ที่ตายระหว่าง poll

> เอกสารแผนงาน follow-up ของ dashboard-traffic-detail (PR #101): Protocol Breakdown /
> Top Talkers ปัจจุบันเป็น **ประมาณการ** เพราะ poll conntrack ทุก 10s แล้วคิด delta —
> flow ที่เกิดและตายในช่วงระหว่าง tick จะถูกนับไม่ครบ Phase 2 เพิ่ม **listener ของ
> conntrack DESTROY event** มาเก็บ byte ก้อนสุดท้ายของทุก flow ตอนถูกลบ (hybrid poll +
> event) โดย **ไม่แตะ firewall rule generation เลย**
>
> วันที่เขียน: 2026-07-29 · Branch อ้างอิง: `feat/dashboard-traffic-detail` (ตั้งต้นจาก `main`)
> README Feature Status: Dashboard backend = Partial → ยังคง Partial (แผนนี้เพิ่มความแม่นยำ ไม่เพิ่มฟีเจอร์ใหม่)

## 0. เป้าหมายและขอบเขต

**เป้าหมาย**
- ตัวเลข Protocol Breakdown / Top Talkers ครอบคลุม flow ที่อายุสั้นกว่า poll interval
  (scan, DNS burst, P2P) → ความคลาดเคลื่อนลดจาก ~95–99% (และแย่กว่านั้นตอนมี scan)
  เหลือ "ใกล้เคียงจริง" ในทางปฏิบัติ
- ผู้ใช้เห็นผลผ่านป้ายบนการ์ด: เมื่อ listener ทำงานอยู่ ป้าย "ประมาณการ" เปลี่ยนเป็น
  "ใกล้เคียงจริง"; ถ้า listener ใช้ไม่ได้ (kernel ไม่รองรับ/ไม่มีสิทธิ์) ระบบ **degrade
  กลับไป poll อย่างเดียวเงียบ ๆ** และแจ้งใน System Capabilities panel
- ห้ามมี regression: ไม่นับซ้ำ (double count), memory ไม่โต, ไม่ทำให้ process ตาย
  เมื่อ event ท่วม

**นอกขอบเขต (ตัดชัดเจน)**
- **Top Rules by Traffic** — แม่นยำอยู่แล้ว (kernel rule counter) ไม่แตะ
- **แนวทาง nftables named counter / dynamic set** — ประเมินแล้วใน §2 และ **ตัดออกจาก
  Phase นี้** (เก็บไว้เป็น Phase 3 ถ้า hybrid ยังไม่พอ)
- ไม่เพิ่มการ์ด/หน้าจอใหม่, ไม่ persist flow history ลง SQLite, ไม่แตะ
  `real_firewall.go` แม้แต่บรรทัดเดียว
- ไม่เปลี่ยน bucket span/retention (5 นาที × 288) และไม่เปลี่ยน endpoint เดิม

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง 2026-07-29)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Frontend การ์ด 3 ใบ (แท็บ Detailed) | เสร็จแล้ว — `EstimatedBadge` **hardcode** ไว้บน 2 การ์ดแรก ไม่ได้อ่านจากฟิลด์ API | `frontend/src/pages/Dashboard.tsx:~612, ~624, ~672, ~777` |
| Frontend API client + mock branch | เสร็จแล้ว (มี `estimated: true` แบบคงที่ใน mock) | `frontend/src/services/dashboardService.ts:~125, ~281-332` |
| Route + handler | เสร็จแล้ว — `authRoute("GET /api/dashboard/traffic-detail")`, whitelist `window` | `backend/internal/api/router.go:~41`, `handlers.go:~401` |
| Service poller | เสร็จแล้ว — poll 10s, delta+clamp, seed, prune, bucket ring ใน RAM | `backend/internal/service/traffic_stats.go:~152 poll, ~184 processFlows, ~398 addBucket, ~471 GetTrafficDetail` |
| Kernel interface | มี `TrafficAccountingManager` แค่ 2 เมธอด (`DumpFlows`, `DumpRuleCounters`) — **ยังไม่มี** ช่องทาง event | `backend/internal/kernel/interfaces.go:~53` |
| Kernel real | `RealTrafficAccounting` (poll อย่างเดียว), `flowKey()` แฮชรวม `TimeStart` | `backend/internal/kernel/real_traffic_account.go:~43, ~77, ~120` |
| Kernel mock | `MockTrafficAccounting` สังเคราะห์ flow จาก template, ไม่เปิด socket จริง | `backend/internal/kernel/mock.go:~788, ~811` |
| Pattern netlink listener ที่มีอยู่แล้ว | `RealTrafficLog` (NFLOG): read loop + bounded chan 256 + drop counter + ctx cancel | `backend/internal/kernel/real_traffic_log.go:~22-60` |
| Wiring | `trafficAcct` เลือก real/mock, `Start(monitorCtx)` หลัง netlink monitor | `backend/cmd/pigate/main.go:~135, ~186, ~212, ~437` |
| Capability probe | มี id `conntrack` (ตรวจ dump ได้ + `nf_conntrack_acct`) — **ยังไม่มี** การตรวจ ct event | `real_capability.go:~35, ~155`, `service/system_capability.go:~29`, `mock.go:~496` |
| install.sh | ตั้ง `net.netfilter.nf_conntrack_acct=1` + preload `nf_conntrack` แล้ว — **ยังไม่มี** `nf_conntrack_events` / `nf_conntrack_timestamp` | `install.sh:~470-529, ~628` |
| DB / migration | ไม่มีตารางเกี่ยวข้อง (state อยู่ใน RAM ล้วน) | — |
| openapi | schema `TrafficDetail` มีครบทั้ง 2 ไฟล์ที่ต้อง sync | `docs/openapi.yaml:~343, ~3545`, `frontend/public/openapi.yaml` (เนื้อหาเดียวกัน) |

**สรุป:** งานจริงกระจุกที่ kernel layer (ไฟล์ listener ใหม่ + 1 เมธอดใน interface) และ
การรับ event เข้ามาผสมกับ baseline เดิมใน `traffic_stats.go` ส่วน frontend/API แก้แค่
ฟิลด์เดียวเพื่อเลิก hardcode ป้าย "ประมาณการ"

## 2. แนวทางเทคนิค

### 2.1 ทางที่เลือก — hybrid: poll เดิม + conntrack DESTROY listener

subscribe netlink multicast group `NFNLGRP_CONNTRACK_DESTROY` แล้วดึง byte ก้อนสุดท้าย
ของทุก flow ตอน kernel ลบมันทิ้ง แล้ว **เครดิตเฉพาะส่วนต่างจาก baseline ที่ poll เห็น
ล่าสุด** เข้า bucket เดียวกัน

```go
// kernel: ไม่ต้องเพิ่ม dependency ใด ๆ — ทุกชิ้นมาจาก vishvananda/netlink ที่เป็น direct dep อยู่แล้ว
s, err := nl.Subscribe(unix.NETLINK_NETFILTER, unix.NFNLGRP_CONNTRACK_DESTROY) // nl/nl_linux.go:791
msgs, _, err := s.Receive()                                                    // nl/nl_linux.go:878
attrs, _ := nl.ParseRouteAttr(msg.Data[nfgenmsgLen:])                          // nl/nl_linux.go:1038
// เดิน CTA_TUPLE_ORIG / CTA_COUNTERS_ORIG / CTA_COUNTERS_REPLY เอง (nl/conntrack_linux.go:114,120,153,176)
```

**เหตุผลที่เลือก (ยืนยัน draft เดิมของ §7 ใน Phase 1 พร้อมหลักฐานใหม่)**
1. **ไม่เพิ่ม dependency เลย** — draft เดิมเข้าใจว่าต้องใช้ `ti-mo/conntrack` หรือเขียนบน
   `mdlayher/netlink` (indirect) แต่สำรวจแล้วพบว่า `github.com/vishvananda/netlink/nl`
   (direct dep, `go.mod:11`) export ครบ: `Subscribe`, `NetlinkSocket.Receive`,
   `ParseRouteAttr`, `DeserializeNfgenmsg` และค่าคงที่ `CTA_*` ส่วน
   `unix.NFNLGRP_CONNTRACK_DESTROY` มีใน `golang.org/x/sys/unix` แล้ว
   ต้นทุนที่เหลือคือ parser ของ CTA attribute ~120 บรรทัด เพราะ `parseRawData`
   ของ library เป็น unexported (`conntrack_linux.go:624`)
2. **ไม่แตะ firewall chain** → ไม่ชนโครงสร้างบังคับ 4 ส่วน (`tech_stack_design.md` §4.3)
   ความเสี่ยงเชิงความปลอดภัยต่ำกว่ามาก
3. มี **แม่แบบในโค้ดเดิมอยู่แล้ว**: `kernel/real_traffic_log.go` (NFLOG listener) —
   ทำตามสไตล์นั้นทั้ง read loop, bounded channel, drop counter, ctx cancel

**ทำไมยังต้องมี poll อยู่:** DESTROY event มาถึงตอน flow ตายเท่านั้น การดาวน์โหลดยาว 3
ชั่วโมงจะไม่รายงานอะไรเลยจนจบ แล้วโยนก้อนใหญ่เข้าบักเก็ตเดียว → การ์ดจะดูนิ่งผิดจริง
poll จึงยังเป็นแหล่งของ "traffic ที่กำลังไหล" และ event เป็นตัวเก็บเศษท้ายที่ poll พลาด

### 2.2 ทางเลือกที่ตัดทิ้ง — nftables named counter + dynamic set

สำรวจ `google/nftables v0.3.0` แล้วพบว่า **library รองรับจริง** (จบข้อสงสัยของ spike ใน
Phase 1 §7): `CounterObj` (`counter.go:23`), `AddObj/GetObjects/ResetObjects`
(`obj.go:103, 193, 207`), `expr.Objref` (`expr/objref.go:25`), `expr.Dynset.Exprs`
(`expr/dynset.go:43`) และ `SetElement.Counter` ที่ `GetSetElements` ถอดค่าให้
(`set.go:292, 342`) — แต่ยังตัดออกจาก Phase นี้เพราะ:

- ต้องแทรก rule ลง chain ที่มีโครงสร้างบังคับ 4 ส่วน → เป็นการแก้ `real_firewall.go`
  ซึ่งเป็นไฟล์อ่อนไหวที่สุดของโปรเจกต์ แลกกับผลลัพธ์ที่ hybrid ก็ได้ใกล้เคียงกัน
- `ApplyRules` เรียก `conn.FlushTable(table)` (`real_firewall.go:~64`) ซึ่ง marshal เป็น
  `NFT_MSG_DELRULE` (`table.go:100-112`) — ลบเฉพาะ rule ไม่ลบ object แต่ `AddObj` ส่ง
  flag `Create` โดยไม่มี `Excl` (`obj.go:~130`) ทำให้พฤติกรรมของการ "add ซ้ำ" ต่อ
  counter object ที่มีอยู่ (คงค่าเดิม vs ถูกรีเซ็ต) **ขึ้นกับเวอร์ชัน kernel** และยัง
  ไม่ได้ยืนยันบนบอร์ดจริง → ความเสี่ยงตัวเลขเพี้ยนทุกครั้งที่ผู้ใช้แก้ policy
- dynamic set ต่อ IP ต้องออกแบบ `size` + timeout GC เอง และจำนวน rule จะโตตามจำนวน
  Service Object ของผู้ใช้

> ถ้าอนาคตยังต้องการความแม่น 100% ระดับ byte ให้เปิดเป็น Phase 3 แยก และ **ต้องระบุใน
> แผนนั้นว่าเป็นงาน security-sensitive: review เข้มเป็นพิเศษ + test ยืนยันลำดับ/จำนวน
> rule ในทั้ง 3 chain ไม่เปลี่ยน** (มี `kernel/policy_chain_test.go` เป็นฐานอยู่แล้ว)

## 3. ขั้นตอนการทำ (เรียงจากชั้นในออกนอก)

### T-01 — Spike ยืนยันบนบอร์ดจริงก่อนเขียนโค้ด (ไม่มีไฟล์แก้)
ตรวจ 3 อย่างบนอุปกรณ์ทดสอบแล้วบันทึกผลกลับมาในแผนนี้:
`sysctl net.netfilter.nf_conntrack_events`, `sysctl net.netfilter.nf_conntrack_timestamp`,
และว่า `conntrack -E -e DESTROY` (ถ้ามีเครื่องมือ) เห็น event จริงไหม
ถ้า kernel build ไม่มี `CONFIG_NF_CONNTRACK_EVENTS` → แผนนี้ทำต่อไม่ได้ ต้องกลับไปคุยกัน

### T-02 — เพิ่มฟิลด์ความแม่นยำใน DTO
**ไฟล์:** `backend/internal/model/types.go` (`TrafficDetail`, ~:631)
เพิ่ม `Accuracy string \`json:"accuracy"\`` ค่าที่ใช้: `"estimated"` (poll อย่างเดียว) /
`"near-exact"` (poll + event) — คง `Estimated bool` ไว้เพื่อความเข้ากันได้ย้อนหลัง

### T-03 — เพิ่มเมธอดใน kernel interface
**ไฟล์:** `backend/internal/kernel/interfaces.go` (~:53)
```go
// WatchFlowEnd streams the final byte count of every conntrack flow at teardown.
// Blocking; returns when ctx is done or the subscription fails.
WatchFlowEnd(ctx context.Context, cb func(model.FlowSample)) error
```
ลอกลายเซ็นจาก `TrafficLogManager.WatchForwardTraffic` (~:32) เพื่อให้หน้าตาเหมือนของเดิม

### T-04 — implementation จริง 🔒 (งานหลัก, security-sensitive: netlink)
**ไฟล์ใหม่:** `backend/internal/kernel/real_conntrack_events.go` (build tag `//go:build linux`
เหมือน `real_traffic_account.go:1`)
- `nl.Subscribe(unix.NETLINK_NETFILTER, unix.NFNLGRP_CONNTRACK_DESTROY)` → read loop
  `Receive()` → ข้าม nfgenmsg 4 ไบต์ → `nl.ParseRouteAttr` → เดิน `CTA_TUPLE_ORIG`
  (`CTA_IP_V4_SRC/DST`, `CTA_IP_V6_SRC/DST`, `CTA_PROTO_NUM`, `CTA_PROTO_DST_PORT`) และ
  `CTA_COUNTERS_ORIG` + `CTA_COUNTERS_REPLY` (bytes) → คืนเป็น `model.FlowSample`
- **ต้องใช้ตัวสร้าง key ตัวเดียวกับ poll** — refactor `flowKey()` ใน
  `real_traffic_account.go:~120` เป็น `flowKeyFromParts(family, proto, srcIP, srcPort,
  dstIP, dstPort, timeStart)` แล้วให้ทั้งสองทางเรียกฟังก์ชันเดียวกัน (ดู Caution 2)
- ปิด socket เมื่อ ctx done, `recover()` รอบ parser, ไม่ block บน callback ช้า
  (bounded chan ตามแบบ `trafficLogChanSize = 256`, นับ drop แล้ว log เป็นระยะ)

### T-05 — mock (ห้ามแตะ OS)
**ไฟล์:** `backend/internal/kernel/mock.go` (`MockTrafficAccounting`, ~:811)
`WatchFlowEnd` สังเคราะห์ event เป็นระยะ (เช่นทุก ~7s) จาก `mockFlowTemplates` (~:788)
โดยใช้ IP เดิมเพื่อให้ Top Talkers ใน dev ขยับ — **ห้ามเปิด socket/อ่าน /proc ใด ๆ** และ
ต้องคืนค่าเมื่อ ctx ถูกยกเลิก

### T-06 — รับ event เข้าบัญชีเดียวกับ poll
**ไฟล์:** `backend/internal/service/traffic_stats.go`
- เพิ่ม `StartFlowEndWatcher(ctx)` (goroutine แยกจาก `run`) เรียก `acct.WatchFlowEnd`
  ถ้า error → log แล้ว **ปล่อยให้ระบบทำงานแบบ poll-only** (ตั้ง flag `eventsActive=false`)
- callback `onFlowEnd(f)`: ใต้ `flowMu` — ถ้ามี baseline ของ key นี้ เครดิต
  `f.Bytes - st.bytes` (clamp ≥ 0) แล้ว `delete(s.flowState, key)`; ถ้าไม่มี baseline
  (flow เกิด+ตายในช่วงระหว่าง tick — เคสที่แผนนี้มาแก้) เครดิต `f.Bytes` เต็ม
- เอา delta เข้า bucket ผ่านเส้นทางเดิม (`addBucket`) และผ่าน `categorize()` เดิม
- `GetTrafficDetail` (~:471) เซ็ต `Accuracy` ตาม `eventsActive`

### T-07 — unit test
**ไฟล์:** `backend/internal/service/traffic_stats_test.go`
เพิ่มเคส: (ก) poll เห็น 1000 → event ปิดที่ 1500 → รวมต้องได้ 1500 ไม่ใช่ 2500,
(ข) event ของ key ที่ไม่เคยเห็น → นับเต็ม, (ค) event หลัง flow ถูก prune ไปแล้ว →
ไม่ทำให้ติดลบ, (ง) `-race` ระหว่าง `poll()` + `onFlowEnd()` + `GetTrafficDetail()`
(ต่อยอด `TestTrafficStats_GetTrafficDetailNoRaceWithPoll` ~:225)

### T-08 — wiring
**ไฟล์:** `backend/cmd/pigate/main.go` (~:437 ถัดจาก `trafficStatsService.Start(monitorCtx)`)
เรียก `trafficStatsService.StartFlowEndWatcher(monitorCtx)` ใช้ `monitorCtx` ตัวเดิมเพื่อ
ให้ socket ถูกปิดตอน shutdown
> **ไม่ต้อง** ทำ `InitApplyConfig()` — งานนี้ไม่มี state ที่ต้อง apply ลง kernel และไม่
> เกี่ยวกับลำดับ boot (interfaces → routes → monitor → DHCP → DNS → firewall → QoS)

### T-09 — capability probe
**ไฟล์:** `backend/internal/kernel/real_capability.go` (~:35, ~:155),
`backend/internal/kernel/mock.go` (~:496), `backend/internal/service/system_capability.go` (~:29)
เพิ่ม id `conntrack-events` (ชื่อแสดงผล เช่น "Conntrack Event Stream") + reason ใหม่
`events_unavailable` ใน `backend/internal/model/capability.go` (~:41) โดยรายงานเป็น
`Degraded` ไม่ใช่ `Available=false` (ระบบยังทำงานได้ด้วย poll)

### T-10 — install.sh
**ไฟล์:** `install.sh` (บล็อก sysctl ~:479-501 และข้อความสรุป ~:628)
เพิ่มลง `/etc/sysctl.d/99-pigate.conf`: `net.netfilter.nf_conntrack_events = 1` และ
(ถ้า T-01 พบว่า poll ฝั่งหนึ่งมี TimeStart จริง) `net.netfilter.nf_conntrack_timestamp = 1`
พร้อมคอมเมนต์ภาษาไทยตามสไตล์บล็อกเดิม + อัปเดตบรรทัดสรุปท้ายสคริปต์
> **ไม่ต้อง** แก้ Polkit/sudoers — multicast group นี้ใช้ `cap_net_admin` ที่ binary มีอยู่แล้ว

### T-11 — API contract + frontend
**ไฟล์:** `docs/openapi.yaml` (~:3545) **และ** `frontend/public/openapi.yaml` (ต้องเหมือนกันเป๊ะ),
`frontend/src/services/dashboardService.ts` (~:125 interface, ~:281-332 mock branch),
`frontend/src/pages/Dashboard.tsx` (~:612 `EstimatedBadge`, ~:624/~:672)
เพิ่มฟิลด์ `accuracy` และเปลี่ยนป้ายให้ขับด้วยค่านั้น: `estimated` → "ประมาณการ",
`near-exact` → "ใกล้เคียงจริง" ใช้ `components/ui/badge` เดิม, สี semantic variable เท่านั้น,
flat design, ต้องดูดีทั้ง dark/light
> **ไม่ต้อง** แก้ `backend/internal/api/dist/openapi.yaml` — เป็น build artifact ที่
> `build.sh` คัดลอกมาจาก `frontend/public/` แก้มือแล้วจะถูกทับ

### T-12 (optional, ท้ายสุด) — เอกสาร
`docs/tech_stack_design.md` เพิ่มหัวข้อสั้น ๆ ว่า traffic accounting เป็น hybrid
poll+event และ `README.md` Feature Status ระบุว่า Dashboard traffic เป็น "near-exact"

## 4. API ที่เกี่ยวข้อง

| Method | Path | ใครเรียกได้ | พฤติกรรม |
|---|---|---|---|
| GET | `/api/dashboard/traffic-detail?window=1h\|24h` | **เส้นเดิม** `authRoute` (ทุก role ที่ล็อกอิน) | เพิ่มฟิลด์ `accuracy` ใน response เท่านั้น |
| GET | `/api/system/capabilities` | **เส้นเดิม** `authRoute` | เพิ่มรายการ `conntrack-events` |

ทั้งสองเส้นเป็น GET ล้วน → `DisableEditMiddleware` (`-disable-edit=true`) และ
`RoleReadOnlyMiddleware` ไม่บล็อก และ **ต้องยังดูได้ปกติในโหมด read-only** (ถือเป็นเกณฑ์ทดสอบ)
ไม่มี input จากผู้ใช้ไหลเข้าสู่ netlink/nft rule ในแผนนี้ (`window` ถูก whitelist ที่
`handlers.go:~401` อยู่แล้ว)

## 5. ข้อควรระวัง

1. **นับซ้ำ (double count)** — ถ้า callback เครดิต `f.Bytes` เต็มทั้งที่ poll เคยนับ
   1000 ไปแล้ว ตัวเลข Top Talkers จะพองเกือบเท่าตัว → ต้องเครดิต **ส่วนต่างจาก
   baseline** เสมอ แล้วลบ key ออกจาก `flowState` ทันที (ป้องกัน poll รอบถัดไปมาเจอซ้ำ)
   และต้องมี unit test T-07 (ก) เป็นตัวล็อกพฤติกรรมนี้
2. **key ระหว่าง poll กับ event ต้องตรงกัน 100%** — `flowKey()`
   (`real_traffic_account.go:~120`) แฮชรวม `TimeStart` ซึ่งมีค่าจริงก็ต่อเมื่อ
   `net.netfilter.nf_conntrack_timestamp=1`; ถ้าฝั่ง event ไม่ถอด `CTA_TIMESTAMP` แต่
   ฝั่ง poll มีค่า → key ไม่ตรง → ระบบจะเข้าใจว่าเป็น flow ใหม่แล้ว **นับซ้ำทั้งก้อน**
   → ป้องกันด้วย (ก) ใช้ `flowKeyFromParts` ตัวเดียวกันทั้งสองทาง และ (ข) ผลของ T-01
   ต้องระบุชัดว่า sysctl นี้เปิดหรือปิด และเลือกให้สอดคล้องกันทั้งสองฝั่ง
3. **event flood** — เครื่องโดน port scan สร้าง DESTROY event ได้หลักหมื่น/วินาที
   ถ้า callback ทำงานหนักหรือ channel ไม่มีขอบเขต จะกิน CPU/RAM จนหน้าเว็บหน่วง →
   ใช้ bounded channel + drop counter แบบ `real_traffic_log.go` (`trafficLogChanSize`
   = 256) และเพิ่มเพดานจำนวน event ที่ประมวลผลต่อวินาที ทิ้งส่วนเกินโดย log สรุปเป็นระยะ
   (การ์ดนี้เป็นภาพรวม ไม่ใช่บันทึกครบถ้วน)
4. **ENOBUFS** — เมื่อ kernel ส่งเร็วกว่าที่อ่าน `Receive()` จะคืน `ENOBUFS` และ
   **ห้ามถือเป็น fatal จนออกจาก goroutine** ไม่งั้นความแม่นจะหายไปเงียบ ๆ ตั้งแต่ครั้ง
   แรกที่ traffic แรง → ต้อง log + นับ แล้ววนอ่านต่อ (พิจารณาเพิ่ม `SO_RCVBUF`)
5. **payload ผิดรูป/สั้นกว่าที่คาด** — parser ที่เขียนเองเป็นโค้ดอ่าน byte ดิบ ถ้า
   index เกินขอบเขตจะ panic ทั้ง process → ตรวจความยาวทุก attribute ก่อน slice และครอบ
   `recover()` ในลูป เหมือน `safeConntrackList` (`real_traffic_account.go:~77`)
6. **kernel/สิทธิ์ไม่รองรับ** — บอร์ดที่ build kernel ไม่มี `CONFIG_NF_CONNTRACK_EVENTS`
   หรือรันโดยไม่ได้ `setcap cap_net_admin` จะ `Subscribe` ไม่ผ่าน → **ห้ามทำให้ startup
   ล้ม** ต้อง degrade เป็น poll-only + ขึ้น capability degraded (T-09) ให้ผู้ใช้เห็นสาเหตุ
7. **เวลาของ byte จะเลื่อนบักเก็ต** — byte ที่ไหลเมื่อ 4 นาทีก่อนจะถูกบันทึกตอน flow
   ตาย ทำให้ตกในบักเก็ตปัจจุบัน ไม่ใช่บักเก็ตที่มันไหลจริง → ยอดรวมของหน้าต่าง 1h ยัง
   ถูก แต่รูปร่างรายบักเก็ตเลื่อนเล็กน้อย ต้องเขียนกำกับไว้ใน doc comment ของ
   `traffic_stats.go` เพื่อกันคนหลังเข้าใจว่าเป็นบั๊ก
8. **fd/goroutine leak ตอน shutdown** — ถ้า watcher ไม่ผูกกับ `monitorCtx` ตัวเดียวกับ
   ของเดิม (`main.go:~437`) socket จะค้างเมื่อ service ปิด → ผูก ctx และปิด socket ใน
   `defer` เสมอ
9. **mock ต้องไม่แตะระบบจริง** — dev รัน `-mock=true` บนเครื่องตัวเอง (บ่อยครั้งบน WSL
   ที่ไม่มี conntrack) `MockTrafficAccounting.WatchFlowEnd` ต้องเป็น timer ล้วน ๆ
10. **frontend mock branch ต้องอัปเดตพร้อมกัน** — ถ้าเพิ่ม `accuracy` ใน type แต่ไม่
    เพิ่มใน mock branch (`dashboardService.ts:~323`) TypeScript จะ build ไม่ผ่าน หรือ
    ป้ายจะหายไปเฉย ๆ ในโหมด mock
11. **เครื่องที่ติดตั้งไปแล้วต้อง migrate** — sysctl ใหม่ใน T-10 ไม่มีผลจนกว่าจะรัน
    `install.sh` ซ้ำ (หรือแก้ `/etc/sysctl.d/99-pigate.conf` เอง + `sysctl --system`) →
    ต้องเขียนลง release note ของ PR
12. **SD card** — ห้ามเพิ่มการเขียน SQLite ใด ๆ จากเส้นทาง event (ปริมาณสูงมาก) ทุกอย่าง
    อยู่ใน RAM ring เหมือนเดิม
13. **ทดสอบบนบอร์ดจริงอย่างปลอดภัย** — แผนนี้ไม่แตะ firewall/routing จึงไม่มีความเสี่ยง
    ล็อกตัวเองออกจากเครื่อง แต่ให้ทดสอบตอนเข้าถึงเครื่องได้อยู่ดี เพราะ T-10 แตะ sysctl
    ระดับ host และควรรีบูตหนึ่งครั้งเพื่อยืนยันว่าค่าคงอยู่ข้าม boot
14. **หลักฐานว่าไม่แตะ firewall** 🔒 — ตอนรีวิว PR ให้ตรวจว่า `git diff --stat` **ไม่มี**
    `backend/internal/kernel/real_firewall.go` ปรากฏเลย ถ้ามี แปลว่าหลุดขอบเขต §0

## 6. Checklist สรุป (Definition of Done)

**Spike / kernel**
- [ ] T-01 บันทึกผล `nf_conntrack_events` / `nf_conntrack_timestamp` จากบอร์ดจริงลงแผนนี้
- [ ] T-02 `model/types.go`: ฟิลด์ `accuracy`
- [ ] T-03 `kernel/interfaces.go`: `WatchFlowEnd`
- [ ] T-04 `kernel/real_conntrack_events.go` (ไฟล์ใหม่) + refactor `flowKeyFromParts` 🔒
- [ ] T-05 `kernel/mock.go`: `WatchFlowEnd` แบบ timer ล้วน ไม่มี side effect

**Service / wiring**
- [ ] T-06 `service/traffic_stats.go`: `StartFlowEndWatcher` + `onFlowEnd` (เครดิตส่วนต่าง)
- [ ] T-07 unit test ครบ 4 เคส รวม `go test -race ./internal/service/...`
- [ ] T-08 `cmd/pigate/main.go` wiring ด้วย `monitorCtx`
- [ ] T-09 capability `conntrack-events` (real + mock + catalog + reason)
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน

**Ops / Frontend / Docs**
- [ ] T-10 `install.sh` sysctl ใหม่ + ข้อความสรุป + release note เรื่อง migrate
- [ ] T-11 `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync), `dashboardService.ts`
      (type + mock), `Dashboard.tsx` ป้ายขับด้วย `accuracy`
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน
- [ ] T-12 (optional) `tech_stack_design.md` + README Feature Status

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ)**
- [ ] mock (`-mock=true -allow-dev-cors`): แท็บ Detailed ครบ 3 การ์ด ตัวเลขขยับ,
      ป้ายบน 2 การ์ดแรกแสดงตาม `accuracy` ไม่ใช่ค่า hardcode
- [ ] real device: `sysctl net.netfilter.nf_conntrack_events` = 1 หลังรัน `install.sh` ใหม่ และคงอยู่หลังรีบูต
- [ ] real device: ดาวน์โหลดไฟล์ใหญ่ 1 ครั้งจากเครื่องใน LAN → ยอดของเครื่องนั้นใน Top
      Talkers **ไม่เพิ่มขึ้นเป็นสองเท่า** ของขนาดไฟล์ (ยืนยันว่าไม่ double count)
- [ ] real device: ยิง `nmap -sT` ใส่ปลายทางในอินเทอร์เน็ต (flow สั้นจำนวนมาก) → traffic
      โผล่ในการ์ดจริง ต่างจากพฤติกรรม Phase 1 ที่หายไปเกือบหมด
- [ ] real device: ระหว่าง scan หนัก → CPU ของ process ไม่พุ่งค้าง, log มีบรรทัดสรุป
      drop/ENOBUFS แทนการค้างหรือ crash
- [ ] ปล่อยรัน ≥ 1 ชั่วโมง → RSS ไม่โตต่อเนื่อง (ยืนยัน prune + ลบ key ตอน event)
- [ ] ปิด/ไม่รองรับ ct event (เช่นทดสอบบน WSL หรือถอด cap) → ระบบยังขึ้นการ์ดได้ด้วย
      poll, ป้ายเป็น "ประมาณการ", System Capabilities แสดง `conntrack-events` degraded
- [ ] `-disable-edit=true` และ role read-only ยังเปิดดูการ์ดได้ครบ
- [ ] `git diff --stat` ไม่มี `real_firewall.go` 🔒
