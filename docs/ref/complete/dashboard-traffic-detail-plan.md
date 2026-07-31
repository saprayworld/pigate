# Dashboard Traffic Detail — Protocol Breakdown, Top Talkers, Top Rules by Traffic

> Work plan สำหรับฟีเจอร์: เพิ่มการ์ดวิเคราะห์ traffic 3 ใบในแท็บ **Detailed** ของหน้า
> Dashboard — (1) Protocol Breakdown (สัดส่วน traffic แยกตามหมวด service), (2) Top
> Talkers (host ที่ใช้ bandwidth มากสุด), (3) Top Rules by Traffic (policy rule ที่มี
> traffic ผ่านมากสุด) โดยเก็บข้อมูลจาก **conntrack ผ่าน netlink** และ **nftables
> per-rule counter** ที่มีอยู่แล้วใน kernel — ไม่ใช้ exec/shell และไม่เขียนลง SQLite
>
> เขียนเมื่อ: 2026-07-28 · Reference branch: `main` (งานจริงทำบน `feat/dashboard-traffic-detail`)
> README Feature Status: Dashboard = Partial → หลังงานนี้ยังคง Partial (Power Control /
> System Services ยัง mock) แต่ traffic analytics จะเป็นข้อมูลจริงทั้งหมด

## 0. Goal and Scope

**Goal (สิ่งที่ผู้ใช้เห็น):** เปิด Dashboard → แท็บ Detailed แล้วเห็นเพิ่ม 3 การ์ด

1. **Protocol Breakdown** — stacked/segmented bar + legend แสดงสัดส่วน traffic ตาม
   หมวด service (เช่น HTTPS 62% · DNS 8% · VoIP 5% · Other 25%) โดย **หมวดมาจาก
   Service Objects ที่ผู้ใช้นิยามไว้เอง** (หน้า Addresses/Services) — flow ไหนไม่ match
   Service Object ใดเลย → "Other"
2. **Top Talkers** — รายชื่อ host ที่ใช้ bandwidth สูงสุด (เช่น `NAS-Server 18.2 GB`)
   พร้อม progress bar เทียบสัดส่วน โดยชื่อมาจาก DHCP lease hostname (fallback = IP)
3. **Top Rules by Traffic** (bonus) — policy rule ที่มี traffic วิ่งผ่านมากสุด อ่านจาก
   `counter` ที่ nftables นับให้อยู่แล้วในทุก rule

**เงื่อนไขทางเทคนิค:**
- แหล่งข้อมูลหลัก = **conntrack dump ผ่าน netlink** (`vishvananda/netlink` ที่เป็น
  dependency อยู่แล้ว) — **ไม่เพิ่ม dependency ใหม่** และ **ไม่แตะ chain structure ของ
  `real_firewall.go`** (ยกเว้นการติด UserData tag ให้ rule ในข้อ 3 ซึ่งเป็น additive ล้วน)
- เก็บสถิติใน **RAM ring buffer** แบบเดียวกับ `TrafficBucket` — ไม่ persist ลง SQLite
  (SD card write cycles, `tech_stack_design.md` §8)
- ทำงานได้ทั้ง real และ mock (`-mock=true` ต้องมีข้อมูลสังเคราะห์ให้เห็นการ์ดครบ)
- ตัวเลขเป็น **"ประมาณการ"** โดยเจตนา (ดู §2.4 accuracy estimate) — UI ต้องสื่อสารข้อนี้

**Out of scope (จงใจตัดออก):**
- ❌ **Phase 2 — nftables named counters / dynamic set ต่อ IP** (ความแม่นยำระดับ kernel)
  → ดู §7 "Future / Out of scope"
- ❌ conntrack **DESTROY event listener** (netlink multicast `NFNLGRP_CONNTRACK_DESTROY`)
  เพื่อไม่ให้พลาด byte ของ flow ที่ตายระหว่าง poll — เจ้าของโปรเจกต์ยอมรับตัวเลข
  ประมาณการใน Phase 1 แล้ว (ต้องเพิ่ม dep `ti-mo/conntrack` หรือเขียน CTA parser เอง)
- ❌ ไม่ persist ประวัติ traffic ต่อ host ลง DB (ไม่มี historical report ข้าม reboot)
- ❌ ไม่ทำ per-host **live rate** (Mbps ปัจจุบัน) — Phase 1 แสดงเป็น **ยอดสะสมในหน้าต่าง
  เวลา** (1h / 24h) เท่านั้น
- ❌ ไม่ทำ deep packet inspection / SNI / application fingerprinting — จำแนกด้วย port
  ตาม Service Objects เท่านั้น
- ❌ ไม่แตะ QoS/tc (HTB class stats เป็นคนละแหล่ง ไม่ครอบคลุมทุก host)

## 1. Current State (สำรวจโค้ด ณ 2026-07-28)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| แท็บ Detailed | ⚠️ มีแค่การ์ดเดิมจัดเรียงใหม่ ไม่มีข้อมูลใหม่ | `frontend/src/pages/Dashboard.tsx:699-714` (`StatGrid`/`BandwidthCard`/`SystemInfoCard`/`InterfacesCard`/`AlertsCard`) |
| Bandwidth (รวม) | ✅ ของจริงแล้ว (ไม่ใช่ simulated ตามที่ README บอก) | `service/system_status.go:228` `collectTraffic()` → `kernel/real_system_stats.go:302` `GetNetCounters()` (netlink link stats, เฉพาะ iface role=WAN) |
| Ring-buffer pattern ใน RAM | ✅ มีแม่แบบครบ | `service/system_status.go:20-30` (`trafficBucketSpan` 5m × `trafficBucketMax` 288 = 24h), `:272` `addToBucketLocked` |
| **byte ต่อ host** | ❌ **ไม่มีเลย** | ไม่มีที่ไหนในโค้ดนับ byte แยกตาม IP |
| **byte ต่อ protocol** | ❌ **ไม่มีเลย** | — |
| NFLOG traffic log | ⚠️ มี src/dst/dport/proto **แต่ไม่มีจำนวน byte** และถูก sample หนัก | `kernel/real_traffic_log.go:243` `parsePacketHeader` · rate limit `real_firewall.go:399` (`10/second burst 20`), `:112`/`:330` (`3/minute`) · channel 256 ทิ้ง event ตอน burst (`real_traffic_log.go:43`) → **ใช้คำนวณสัดส่วน byte ไม่ได้** |
| nftables per-rule counter | ⚠️ **มี `&expr.Counter{}` ติดทุก rule ใน kernel แล้ว แต่ไม่เคยอ่านกลับ** | `kernel/real_firewall.go:790, 868, 1041, 1287, 1313` |
| การผูก nft rule ↔ DB rule id | ❌ ไม่มี (ไม่ได้ set `UserData`) | `real_firewall.go:1047-1132` — 1 DB rule ขยายเป็นหลาย nft rule (src×dest×service) |
| conntrack | ✅ ใช้งานอยู่แน่นอน (มี ct state rules) | `real_firewall.go:138,150,414,426,525` (`expr.Ct{Key: CtKeySTATE}`) |
| `vishvananda/netlink` conntrack API | ✅ **มีอยู่ใน go.mod แล้ว** ให้ byte/packet ต่อ flow | `go.mod:11` (v1.3.1) · `ConntrackTableList(table, family)` → `ConntrackFlow{Forward, Reverse IPTuple}` · `IPTuple{Bytes, Packets, SrcIP, DstIP, SrcPort, DstPort, Protocol}` |
| IP → hostname | ✅ พร้อมใช้ | `kernel.DhcpManager.GetActiveLeases()` (`kernel/interfaces.go:86`) → `model.ActiveDhcpLease{IPAddress, MacAddress, Hostname}` (`model/types.go:314`) |
| Service Objects (สำหรับจัดหมวด) | ✅ มีใน DB + parser port | `model/types.go:83` `ServiceObject{Protocol, Port}` · parser `kernel/real_firewall.go:710` `parsePortSpec` (**unexported อยู่ใน package kernel**) |
| Capability probe pattern | ✅ มีแม่แบบครบ | `kernel/real_capability.go:34` `ProbeAll` · catalog `service/system_capability.go:25-28` · reason codes `model/capability.go:8-41` |
| sysctl ตอนติดตั้ง | ✅ มีไฟล์ที่ต้องแก้อยู่แล้ว | `install.sh:471-497` เขียน `/etc/sysctl.d/99-pigate.conf` + `sysctl --system` |
| `nf_conntrack_acct` | ❌ ยังไม่ได้เปิด (default ปิด → `Bytes` จะเป็น 0) | ต้องเพิ่มใน `install.sh` |
| Dashboard API routes | ✅ มี pattern | `api/router.go:37-43` · handler ตัวอย่าง `api/handlers.go:388` `HandleGetTrafficHistory` |
| startup wiring | ✅ มี pattern | `cmd/pigate/main.go:120-171` (เลือก real/mock), `:193` (สร้าง service), `:355` `monitorCtx`, `:409` เริ่ม sampler |

**สรุป:** UI/API/ring-buffer/capability/hostname-mapping มีแม่แบบครบหมด — งานจริงคือ
**สร้าง data pipeline ใหม่ 1 เส้น** (kernel conntrack collector → service aggregator →
API → 3 การ์ด) + **1 การแก้ `real_firewall.go` แบบ additive** (ติด UserData ให้ rule
เพื่อผูก counter กลับเข้ากับ DB rule)

## 2. Technical Approach

### 2.1 กลไกหลัก — conntrack polling (Protocol Breakdown + Top Talkers)

kernel layer เพิ่ม interface ใหม่ `TrafficAccountingManager` ที่มี 2 เมธอด:

```go
type TrafficAccountingManager interface {
    // DumpFlows คืน snapshot ของ conntrack table (IPv4 + IPv6) ณ ขณะนั้น
    DumpFlows() ([]model.FlowSample, error)
    // DumpRuleCounters คืน bytes/packets ต่อ DB policy-rule id (อ่านจาก nftables)
    DumpRuleCounters() (map[string]model.RuleCounter, error)
}
```

`real_traffic_account.go` เรียก `netlink.ConntrackTableList(netlink.ConntrackTable, unix.AF_INET)`
และ `AF_INET6` แล้วแปลงเป็น `model.FlowSample` โดย:
- **`SrcIP` = `flow.Forward.SrcIP`** — เป็น IP ต้นทางฝั่ง LAN **ก่อน NAT** (masquerade
  เกิดที่ postrouting หลัง conntrack จับ original tuple แล้ว) จึงใช้เป็น key ของ
  Top Talkers ได้ตรงตัว
- **`DstPort` = `flow.Forward.DstPort`, `Proto` = `flow.Forward.Protocol`** → ใช้จัดหมวด
- **`Bytes` = `flow.Forward.Bytes + flow.Reverse.Bytes`** (นับทั้งขาไป-ขากลับ)
- **`Key`** = hash ของ (family, proto, srcIP, srcPort, dstIP, dstPort, `flow.TimeStart`)
  — ใส่ `TimeStart` ด้วยเพื่อไม่ให้ flow ใหม่ที่บังเอิญใช้ 5-tuple ซ้ำกับ flow เก่าถูกมองว่า
  เป็นตัวเดิม (จะทำให้ delta ติดลบ/หาย)

service layer (`service/traffic_stats.go`) รัน goroutine poll ทุก **10 วินาที** (เท่ากับ
`trafficPollInterval` เดิม) แล้ว:
1. `DumpFlows()` → เทียบกับ snapshot รอบก่อน (`map[key]bytes`) → คิด **delta** (คลैมป์ที่ 0)
2. บวก delta เข้า 2 aggregator: `byHost[srcIP] += delta` และ `byCategory[cat] += delta`
3. เก็บลง bucket 5 นาที × 288 (24h) แบบเดียวกับ `addToBucketLocked` และ prune host ที่
   ไม่มี traffic ในหน้าต่างเวลา
4. **poll แรกหลัง start = seed baseline เท่านั้น ไม่นับ delta** (กัน flow ที่ established
   มาก่อนโปรเซสเริ่ม โยนยอดสะสมทั้งชีวิตเข้ามาเป็น spike ที่ t0)

### 2.2 การจัดหมวด protocol ด้วย Service Objects

**ผู้ใช้ตัดสินใจแล้ว:** ใช้ Service Objects เป็นตัวจัดหมวดหลัก ไม่ hardcode หมวดในโค้ด

- โหลด `repo.GetServiceObjects()` มา cache ไว้ (refresh ทุก ~60s หรือทุกครั้งที่ poll)
- แปลงเป็นตาราง lookup: `(protoNum, portStart, portEnd) → ServiceObject.Name`
- flow หนึ่งจับคู่โดยดู `Proto` + `DstPort` ของ original direction
- **match หลายตัว** → เลือกตัวที่ **ช่วง port แคบที่สุด** (specific ชนะ range กว้าง);
  เสมอกันให้เรียงตามชื่อเพื่อให้ deterministic
- **ไม่ match เลย** → หมวด `"Other"` (ตามที่เจ้าของกำหนด)
- ICMP/ICMPv6 หรือ proto อื่นที่ไม่มี port → match กับ ServiceObject ที่ `Protocol=="ICMP"`
  ถ้ามี ไม่งั้น `"Other"`

**การใช้ `parsePortSpec` ซ้ำ:** ตัวแปลง `"8000-8010"` มีอยู่แล้วที่
`kernel/real_firewall.go:710` แต่ **unexported และอยู่คนละ layer** → ให้ย้าย logic ไปไว้ที่
`model` (เช่น `model.ParsePortSpec`) แล้วให้ `kernel.parsePortSpec` เรียกต่อ (diff ใน
ไฟล์ sensitive แค่ 3 บรรทัด) — **ห้ามเขียน parser ตัวที่สองขึ้นมาใหม่** เพราะจะทำให้
กฎการแปลง port ของ firewall กับของ dashboard เพี้ยนจากกันได้ในอนาคต

### 2.3 Top Rules by Traffic — อ่าน counter ที่มีอยู่แล้ว

ทุก rule ที่ `real_firewall.go` สร้างมี `&expr.Counter{}` ติดอยู่แล้ว (บรรทัด 790, 868,
1041, 1287, 1313) แต่ **อ่านกลับมาแล้วไม่รู้ว่า rule ไหนคือ rule ไหนใน DB** เพราะ 1 DB
rule ขยายเป็นหลาย nft rule (loop src × dest × service ที่ `:1058-1132`)

**วิธีผูก:** ตอนสร้าง rule ให้ใส่ `Rule.UserData` เป็น comment ที่บรรจุ DB rule id
(ใช้ helper `github.com/google/nftables/userdata` ที่มากับ dependency เดิม) แล้วขา read
ใช้ `conn.GetRules(table, chain)` → decode UserData → `sum(counter.Bytes)` ต่อ rule id

- counter ของ nftables เป็น **ยอดสะสมตั้งแต่ rule ถูกสร้าง** → service ต้องเก็บ baseline
  แล้วรายงานเป็น **delta ในหน้าต่างเวลา** เหมือน conntrack
- **ทุกครั้งที่ `ApplyRules` ถูกเรียก rule จะถูกสร้างใหม่ทั้งชุด → counter รีเซ็ต** →
  ต้องตรวจจับ (ค่าใหม่ < baseline) แล้ว reset baseline แทนที่จะได้ delta ติดลบ
- นี่เป็นข้อมูล **ที่แม่นยำ 100%** (kernel นับเอง) ต่างจาก 2 การ์ดแรก — UI ควรสื่อสาร
  ต่างกัน (การ์ดนี้ไม่ต้องติดป้าย "ประมาณการ")

### 2.4 Accuracy estimate — ตัวเลขจาก conntrack polling แม่นแค่ไหน

**ประเมิน: จับได้ประมาณ 95–99% ของ byte ที่วิ่งผ่านเครื่องจริง ในสภาพบ้าน/ออฟฟิศเล็ก
ทั่วไป และ "สัดส่วน" (%) ที่การ์ดทั้งสองใบแสดง จะคลาดเคลื่อนน้อยกว่านั้นอีก (ราว 1–3%)**

เหตุผลที่รองรับตัวเลขนี้:

1. **conntrack entry ไม่ได้หายทันทีที่ connection จบ** — นี่คือเหตุผลหลัก timeout ของ
   kernel โดย default: TCP `TIME_WAIT` ≈ 120s, TCP `CLOSE` ≈ 10s, TCP `ESTABLISHED`
   5 วัน, UDP unreplied 30s / assured 120s, ICMP 30s. ที่ poll interval 10 วินาที
   **flow เกือบทั้งหมดจะถูกมองเห็นอย่างน้อย 1 ครั้งหลังจากมันจบไปแล้ว** (ตอนนั้น
   `Bytes` เป็นยอดสุดท้ายที่สมบูรณ์) — ไม่ใช่ "flow สั้น = พลาดแน่นอน" อย่างที่มักเข้าใจกัน
2. **ปริมาณ byte กระจุกอยู่ที่ flow ยาว** — traffic ของบ้าน/ออฟฟิศเล็กถูกครองด้วย
   video streaming, download, cloud backup, VPN tunnel ซึ่งมีอายุเป็นนาทีถึงชั่วโมง =
   หลายสิบเท่าของ poll interval จึงถูกนับครบทุก delta ส่วน flow สั้นจริงๆ (DNS query
   ~100–200 bytes, HTTP request เดี่ยว ไม่กี่ KB) มีสัดส่วน byte เล็กมากแม้จะมีจำนวนมาก
3. **ช่องที่พลาดจริงมี 3 กรณี** และล้วนเป็นส่วนน้อย:
   (ก) flow ที่เกิดและถูกลบออกจากตารางภายในหน้าต่าง 10 วินาทีเดียว (ต้องเป็น TCP ที่โดน
   RST แล้ว timeout `CLOSE` 10s พอดี — พบไม่บ่อย)
   (ข) conntrack table เต็มจน kernel evict entry ก่อนเวลา (`nf_conntrack_max` default
   65536+ ที่ RAM 8GB — บ้านทั่วไปใช้หลักร้อยถึงพัน flow ไม่ถึงเพดาน)
   (ค) traffic ที่ไม่ถูก conntrack จับเลย (NOTRACK, non-IP เช่น ARP, บาง multicast)
4. **ความคลาดเคลื่อนหลักคือ "เลื่อนช่อง" ไม่ใช่ "หายไป"** — byte ที่วิ่งช่วงท้าย window
   จะถูกนับใน bucket ถัดไป มีผลกับกราฟราย 5 นาที แต่ **ไม่มีผล**กับยอดรวม 1h/24h ซึ่งเป็น
   สิ่งที่ 2 การ์ดนี้แสดง
5. **การจำแนกหมวด/เจ้าของ byte ที่มองเห็นแล้วนั้นแม่นยำ 100%** (kernel นับให้ต่อ flow
   และ tuple บอก src/port ตรงๆ) → **error ที่เหลือกระทบ "ตัวเลขสัมบูรณ์ (GB)" มากกว่า
   "สัดส่วน (%)"** เพราะ byte ที่พลาดกระจายตัวคล้ายกันทุกหมวด/ทุก host
6. **กรณีที่ความแม่นยำตกลงชัดเจน:** โดน port-scan/DDoS/P2P ที่สร้าง flow สั้นนับหมื่นต่อ
   นาที → conntrack table โดนกดดัน evict เร็ว ความแม่นอาจตกไปที่ ~80–90% และ cost ของ
   dump สูงขึ้น — เป็นสถานการณ์ผิดปกติที่ผู้ใช้ควรเห็นจาก Alerts อยู่แล้ว

**วิธี cross-check ในตัวระบบเอง:** เรามี ground truth อยู่แล้วคือ per-interface byte
counter (`GetNetCounters` ที่ BandwidthCard ใช้) → API ควรคืน `observedBytes` (ยอดที่
conntrack เห็น) มาด้วย เพื่อให้ frontend แสดง Top Talkers เป็น **% ของยอดที่สังเกตได้**
ไม่ใช่แกล้งทำเป็นว่าผลรวมเท่ากับ WAN total; และผู้พัฒนา/ผู้ใช้เปรียบเทียบสองตัวเลขนี้เอง
ได้ว่าห่างกันกี่ % ในสภาพแวดล้อมจริง

**Alternatives ที่พิจารณาแล้วตัดทิ้ง (Phase 1):**
- *(ตัด) คำนวณจาก NFLOG ring buffer ที่มีอยู่* — ไม่มี byte, ถูก rate-limit `10/second`
  และ `3/minute` โดยตั้งใจ, log เฉพาะ rule ที่เปิด log, traffic established ถูก accept ที่
  ct-state rule โดยไม่ผ่าน rule ที่ log → สัดส่วนจะเพี้ยนหนักและ **แสดงเป็น GB ไม่ได้เลย**
- *(ตัด) tc/HTB class stats* — ครอบคลุมเฉพาะ host ที่มี QoS rule และ `GetIfaceQosStatus`
  (`model/types.go:640`) ไม่ได้อ่าน byte stats อยู่แล้ว
- *(ตัด Phase 1) nftables named counters / dynamic set* — แม่น 100% แต่ต้องแทรก rule เข้า
  chain ที่มีโครงสร้างบังคับ 4 ส่วน (งาน security-sensitive) และยังต้อง spike ยืนยันว่า
  `google/nftables v0.3.0` รองรับ set element ที่มี counter → ยกไป §7

## 3. Steps (เรียงจาก inner → outer)

> ไม่ต้องทำ: ❌ db migration (ไม่ persist อะไรลง SQLite) · ❌ chain structure ใหม่ใน
> `real_firewall.go` (แก้แค่ติด UserData ใน Step 7 เท่านั้น) · ❌ Polkit/sudoers เพิ่ม
> (conntrack dump ใช้ `CAP_NET_ADMIN` ที่มีอยู่แล้ว)

**Step 0 (T-00) — Spike ยืนยันบนเครื่องทดสอบจริง (ไม่ merge)**
- บน Pi จริง: `sudo sysctl net.netfilter.nf_conntrack_acct=1` แล้วเขียนโปรแกรมสั้นๆ เรียก
  `netlink.ConntrackTableList` ตรวจว่า `flow.Forward.Bytes > 0` และ `SrcIP` เป็น IP ฝั่ง
  LAN จริง (ก่อน NAT)
- วัด: จำนวน flow ทั่วไป, เวลา/allocation ต่อ dump 1 ครั้ง (คาดหลัก ms)
- ยืนยันว่า `google/nftables` `conn.GetRules` คืน `expr.Counter` ที่มีค่า `Bytes` จริง
- **ผลลัพธ์ที่ต้องได้:** ตัวเลขจริงเพื่อยืนยัน/ปรับ §2.4 และเลือก poll interval สุดท้าย
- ⚠️ ไม่ commit โค้ด spike เข้า repo

**Step 1 (T-01) — Model DTO**
**File:** `backend/internal/model/types.go` (วางถัดจาก `TrafficHistory:560`)
- `FlowSample{Key string, SrcIP, DstIP string, Proto uint8, DstPort uint16, Bytes uint64}`
- `RuleCounter{Bytes, Packets uint64}`
- `TrafficCategorySlice{Name string, Bytes uint64, Percent float64}`
- `TopTalker{IP, Hostname, MAC string, Bytes uint64, Percent float64}`
- `TopRule{RuleID, Name, Chain, Action string, Bytes, Packets uint64, Percent float64}`
- `TrafficDetail{Window string, ObservedBytes uint64, Estimated bool, Categories []TrafficCategorySlice, TopTalkers []TopTalker, TopRules []TopRule, GeneratedAt string}`
- เพิ่ม `model.ParsePortSpec(spec string) (start, end int, err error)` (ย้ายจาก
  `kernel/real_firewall.go:710`)

**Step 2 (T-02) — Kernel interface**
**File:** `backend/internal/kernel/interfaces.go` (วางถัดจาก `TrafficLogManager:32`)
- ประกาศ `TrafficAccountingManager` ตาม §2.1 พร้อม doc comment อธิบายว่าเป็น read-only,
  ต้องการ `nf_conntrack_acct=1`, และ error ต้อง degrade ไม่ทำให้ระบบล่ม

**Step 3 (T-03) — Real implementation (conntrack) — 🔒 sensitive review**
**File (ใหม่):** `backend/internal/kernel/real_traffic_account.go`
- `DumpFlows()`: `ConntrackTableList(ConntrackTable, unix.AF_INET)` + `AF_INET6`,
  แปลงเป็น `[]model.FlowSample` ตาม mapping ใน §2.1
- ถ้า family ใด error ให้ log แล้วไปต่อ (IPv6 อาจไม่มีในบางเครื่อง) — คืน error เฉพาะเมื่อ
  ล้มทั้งสอง family
- cap จำนวน flow ที่ประมวลผลต่อ dump (เช่น 50,000) กัน memory spike ตอนโดน scan
- ⚠️ **review เข้ม:** เปิด netlink socket, ต้องปิด/ไม่ leak fd, ไม่ block นาน, ไม่ panic
  บน payload ผิดรูป

**Step 4 (T-04) — Real implementation (nftables rule counters) — 🔒 sensitive review**
**File:** `backend/internal/kernel/real_traffic_account.go` (เมธอด `DumpRuleCounters`)
- `conn.GetRules(&nftables.Table{Name:"pigate",...}, chain)` สำหรับ chain `input`
  (`real_firewall.go:129`), `forward` (`:406`), `output` (`:517`)
- decode `Rule.UserData` (ดู Step 7) → DB rule id; รวม `expr.Counter.Bytes/Packets`
  ของทุก nft rule ที่มี id เดียวกัน
- rule ที่ไม่มี UserData (structural rule เช่น ct state / final drop) → ข้าม

**Step 5 (T-05) — Mock implementation**
**File:** `backend/internal/kernel/mock.go` (วางถัดจาก `MockTrafficLog`)
- `MockTrafficAccounting` สังเคราะห์ flow ~20–40 เส้นจาก IP ชุดเดียวกับ
  `MockDhcp.GetActiveLeases()` (`mock.go:322`) เพื่อให้ Top Talkers มีชื่อเครื่องโชว์จริง
- byte เพิ่มขึ้นแบบ monotonic ทุกครั้งที่ถูกเรียก (ให้ delta > 0 → การ์ดขยับได้ใน dev)
- port ที่ใช้ควรครอบคลุม 443/53/80/5060 + port แปลกๆ เพื่อทดสอบทั้งหมวดที่ match และ "Other"
- `DumpRuleCounters` คืน map สังเคราะห์ตาม rule id ที่มีใน DB

**Step 6 (T-06) — Capability probe**
**Files:** `backend/internal/kernel/real_capability.go` (ถัดจาก `probeFirewall:77`),
`backend/internal/service/system_capability.go:25-28` (catalog),
`backend/internal/model/capability.go` (ถ้าต้องเพิ่ม reason code)
- probe id ใหม่ `"conntrack"` ชื่อแสดงผล `"Traffic Accounting (conntrack)"`
- ตรรกะ: dump ได้ไหม → `not_supported`/`permission_denied`; dump ได้แต่ **ทุก flow มี
  `Bytes==0`** → `Degraded=true` พร้อมข้อความไทยว่าให้เปิด `net.netfilter.nf_conntrack_acct=1`
  (ผู้ใช้จะเห็น Alert บนหัว Dashboard ที่ `Dashboard.tsx:648` อยู่แล้ว)
- mock prober (`mock.go:495`) ต้องคืน id นี้ด้วย ไม่งั้น catalog จะไม่ครบ

**Step 7 (T-07) — ติด UserData ให้ rule เพื่อผูกกับ DB rule id — 🔒 sensitive review**
**File:** `backend/internal/kernel/real_firewall.go:1058-1132` (`applyUserRules` loop)
- ใช้ `userdata.AppendString(nil, userdata.TypeComment, r.ID)` แล้ว set `Rule.UserData`
  ตอน `AddRule` ทั้งใน forward exprs (`:1287`) และ local exprs (`:1313`)
- **additive เท่านั้น**: ห้ามเปลี่ยนลำดับ/เงื่อนไข/expr อื่นใดของ rule — โครงสร้าง 4 ส่วน
  ของ input chain ต้องเหมือนเดิมทุกประการ
- เพิ่ม unit test ที่ยืนยันว่า ruleset ที่ generate ออกมา (จำนวน/ลำดับ rule) ไม่เปลี่ยน
  เทียบกับ test เดิมใน `kernel/policy_chain_test.go`

**Step 8 (T-08) — Service aggregator**
**File (ใหม่):** `backend/internal/service/traffic_stats.go`
- `TrafficStatsService{ acct kernel.TrafficAccountingManager, repo *db.Repository, dhcp kernel.DhcpManager }`
- const: `flowPollInterval = 10 * time.Second`, bucket 5m × 288 (ลอกจาก
  `system_status.go:20-30`), `maxTrackedHosts = 500`, `maxTrackedFlows = 50000`
- `Start(ctx)` → goroutine poll (ลอกโครง `runTrafficCollector` `system_status.go:188`)
- delta logic + seed-only ในรอบแรก (§2.1 ข้อ 4) + prune host/flow ที่หายไป
- categorization ตาม §2.2 (cache Service Objects, refresh ทุก ~60s)
- rule counters: baseline + ตรวจ reset (ค่าลดลง → ตั้ง baseline ใหม่ ไม่คิด delta ติดลบ)
- `GetTrafficDetail(window string) model.TrafficDetail` — รองรับ `1h` (default) และ `24h`,
  enrich hostname จาก `dhcp.GetActiveLeases()` + DHCP reservation ใน DB (fallback = IP),
  จัดอันดับ + ตัด top N (เช่น 10) + คำนวณ `Percent` จาก `ObservedBytes`
- ทุกอย่างอยู่ใน RAM ภายใต้ `sync.RWMutex` — **ห้ามเขียน SQLite**

**Step 9 (T-09) — API handler + route**
**Files:** `backend/internal/api/handlers.go` (ถัดจาก `HandleGetTrafficHistory:388`),
`backend/internal/api/router.go:40`
```go
authRoute("GET /api/dashboard/traffic-detail", s.HandleGetTrafficDetail)
```
- อ่าน query `window` → whitelist `{"1h","24h"}` เท่านั้น (ค่าอื่น → 400 หรือ fallback
  เป็น `1h`) · **ห้าม** ส่งค่าดิบจาก client ต่อเข้า service
- คืน `model.TrafficDetail` เป็น JSON ผ่าน `s.writeJSON`
- เป็น GET/read-only → ใช้ `authRoute` (ไม่ใช่ superAdmin), `-disable-edit` ไม่กระทบ

**Step 10 (T-10) — main.go wiring**
**File:** `backend/cmd/pigate/main.go`
- ประกาศ `var trafficAcct kernel.TrafficAccountingManager` ที่ `:132` แล้ว assign
  `kernel.NewMockTrafficAccounting()` (`:151`) / `kernel.NewRealTrafficAccounting()` (`:168`)
- สร้าง service ที่ `:193` ถัดจาก `systemStatusService`
- `trafficStatsService.Start(monitorCtx)` ในบล็อกเดียวกับ sampler อื่น (~`:409`)
- ส่งเข้า `api.NewServer(...)`

**Step 11 (T-11) — Frontend service client**
**File:** `frontend/src/services/dashboardService.ts` (ถัดจาก `getTrafficHistory:219`)
- type `TrafficDetail`/`TopTalker`/`TrafficCategorySlice`/`TopRule` ตรงกับ DTO
- `getTrafficDetail(window: "1h" | "24h" = "1h")` → `GET ${API_BASE_URL}/dashboard/traffic-detail?window=…`
- **mock branch** (`IS_MOCK_MODE`) สร้างข้อมูลสมจริงตามภาพตัวอย่าง (HTTPS/DNS/VoIP/Other
  + NAS-Server/iPhone/MacBook/TV) เพื่อให้ dev workstation เห็นการ์ดครบ

**Step 12 (T-12) — Frontend cards**
**File:** `frontend/src/pages/Dashboard.tsx`
- คอมโพเนนต์ใหม่ 3 ตัว วางถัดจาก `AlertsCard:565`:
  `ProtocolBreakdownCard` (segmented bar + legend), `TopTalkersCard` (list + progress bar),
  `TopRulesCard` (list + progress bar)
- ดึงข้อมูลด้วย `usePoll(() => dashboardService.getTrafficDetail(window), 60_000, refreshKey)`
  (`usePoll` มีอยู่แล้วที่ `:65`)
- ใส่ใน `TabsContent value="detailed"` (`:700-714`) เท่านั้น — **ไม่แตะ overview/compact**
- ป้าย "ประมาณการ" (badge/tooltip) บน 2 การ์ดแรก, การ์ด Top Rules ไม่ต้อง (แม่นยำ 100%)
- **styling:** ต้องประกอบจาก `components/ui/*` เท่านั้น, สีจาก theme variable
  (`bg-primary/10`, `text-muted-foreground`, …) **ห้าม hardcode palette** เช่น
  `text-emerald-500`, ห้าม `shadow-*`/`backdrop-blur-*`, ต้องดูดีทั้ง dark/light
  (`docs/rules_of_work.md`)
- สีของ segment ในกราฟ: ใช้ชุด chart color ที่ประกาศใน `src/index.css` (ถ้ายังไม่มีให้
  เพิ่ม `--chart-1..5` เป็น theme variable — ห้าม inline hex ในคอมโพเนนต์)

**Step 13 (T-13) — install.sh: เปิด `nf_conntrack_acct`**
**File:** `install.sh:470-497`
- เพิ่มบรรทัดใน heredoc ที่เขียน `/etc/sysctl.d/99-pigate.conf`:
  ```
  # Conntrack byte/packet accounting — ต้องเปิดเพื่อให้ Dashboard นับ traffic
  # ต่อ host/protocol ได้ (ค่า default ของ kernel = 0 ทำให้ byte counter เป็น 0 ทั้งหมด)
  net.netfilter.nf_conntrack_acct = 1
  ```
- ⚠️ `net.netfilter.*` จะ set ไม่ได้ถ้าโมดูล `nf_conntrack` ยังไม่โหลด → เพิ่ม
  `nf_conntrack` เข้า `/etc/modules-load.d/pigate.conf` (บล็อกเดียวกับ `8021q`, `ifb`
  ที่ `install.sh:499-505`) เพื่อให้ `systemd-modules-load` โหลดก่อน `systemd-sysctl`
- อัปเดตข้อความสรุปท้ายสคริปต์ (`install.sh:613-614`) ให้ระบุค่าที่เพิ่ม
- เป็น update-safe: ผู้ใช้เดิมรัน `install.sh` ซ้ำแล้วได้ค่าใหม่

**Step 14 (T-14) — Docs / API contract**
- `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (sync ทั้งคู่): เพิ่ม path
  `/dashboard/traffic-detail` + schema `TrafficDetail`
- `docs/tech_stack_design.md`: เพิ่มหัวข้อย่อยอธิบายว่า traffic analytics มาจาก
  conntrack polling (ประมาณการ) + nftables counter (แม่นยำ) และเหตุผลที่ไม่ persist
- README Feature Status: ระบุว่า Dashboard traffic analytics เป็นข้อมูลจริงแล้ว

## 4. Related API

| Method | Path | Role | Behavior |
|---|---|---|---|
| GET | `/api/dashboard/traffic-detail?window=1h\|24h` | authRoute (ทุก logged-in role) | **route ใหม่** · คืน `TrafficDetail` (categories + topTalkers + topRules + observedBytes + estimated) · `window` ที่ไม่อยู่ใน whitelist → 400 |
| GET | `/api/dashboard/traffic` | authRoute | เดิม ไม่เปลี่ยน — ยังเป็น ground truth ของ byte รวม ใช้ cross-check ได้ (§2.4) |
| GET | `/api/system/capabilities` | authRoute | เดิม แต่ **มี id ใหม่ `conntrack`** เพิ่มเข้ามาใน list |
| GET | `/api/dhcp/leases` | authRoute | เดิม ไม่เปลี่ยน — service เรียกใช้ภายในเพื่อ map IP → hostname |

- **`-disable-edit` mode:** ไม่กระทบ (GET ล้วน, `DisableEditMiddleware` บล็อกเฉพาะ mutation)
- **`-mock` mode:** ทำงานครบ ใช้ `MockTrafficAccounting` — ไม่แตะ OS เลย
- **`-mock-from-real`:** ถือเป็น mock เหมือน subsystem อื่น (ดู `main.go:137`)

## 5. Cautions

1. **`nf_conntrack_acct` ปิดอยู่ = `Bytes` เป็น 0 ทั้งตาราง (ฟีเจอร์เงียบสนิท).** kernel
   ไม่ error แค่คืน 0 → ผู้ใช้จะเห็นการ์ดว่างโดยไม่รู้สาเหตุ. **กัน:** capability probe
   (Step 6) ต้องแยกกรณี "dump ได้แต่ byte เป็น 0 ทั้งหมด" ออกมาเป็น `Degraded` พร้อม
   ข้อความไทยที่บอกวิธีแก้ตรงๆ + `install.sh` ตั้งค่าให้ตั้งแต่ติดตั้ง (Step 13).

2. **counter ของ nftables รีเซ็ตทุกครั้งที่ `ApplyRules`.** ผู้ใช้แก้ policy หนึ่งครั้ง =
   rule ทั้งชุดถูกสร้างใหม่ → counter กลับเป็น 0. ถ้าคิด delta ตรงๆ จะได้ค่าติดลบ/
   underflow ของ `uint64` (ตัวเลขมหาศาล). **กัน:** ตรวจ `new < baseline` → ถือว่า reset
   แล้วตั้ง baseline ใหม่ นับ delta = `new` (ลอกแนวเดียวกับ `collectTraffic` ที่ clamp
   ค่าติดลบไว้แล้วที่ `system_status.go:252-259`).

3. **flow key ที่ใช้ 5-tuple อย่างเดียวจะชนกัน.** NAT ทำให้ port ถูก reuse บ่อย — flow
   ใหม่ที่ tuple ซ้ำกับ flow เก่าที่เพิ่งตาย จะถูกมองว่าเป็นตัวเดิมและได้ delta ติดลบ
   (หรือ byte หาย). **กัน:** ใส่ `flow.TimeStart` เข้าไปใน key ด้วย (§2.1) และ clamp
   delta ที่ 0 เสมอ.

4. **poll แรกทำให้เกิด spike ปลอม.** flow ที่ established มาก่อนโปรเซสเริ่ม มี `Bytes`
   สะสมมาทั้งชีวิต ถ้านับเป็น delta ของรอบแรกจะได้ยอดพุ่งผิดจริงตอน restart. **กัน:**
   รอบแรก seed baseline อย่างเดียว ไม่บวกเข้า bucket (§2.1 ข้อ 4) — เหมือนที่
   `SystemStatusService.Start` prime `lastCounters` ไว้ก่อน (`system_status.go:95-101`).

5. **memory โตไม่จำกัดตอนโดน scan/P2P.** map ของ flow key และ map ของ host อาจโตถึง
   หลักแสน entry. **กัน:** cap `maxTrackedFlows` ตอน dump (Step 3), cap
   `maxTrackedHosts` (เก็บ top-N + bucket "อื่นๆ"), และ prune entry ที่ไม่ปรากฏใน dump
   2–3 รอบติดกัน. ต้องมี unit test ที่ยิง flow จำนวนมากแล้วยืนยันว่า map ไม่โตเกิน cap.

6. **cost ของ conntrack dump.** เป็น netlink dump ที่ allocate ต่อ flow; ที่ 10 วินาที
   และหลักพัน flow ถือว่าถูกมากบน Pi 5 แต่ต้องไม่ลด interval ลงต่ำกว่านี้โดยไม่วัดผลก่อน
   (T-00) และ **ห้ามเรียก `DumpFlows()` จาก request handler** (ต้องเป็น background
   goroutine + cache เท่านั้น เหมือนกฎเดียวกับ CPU sampler ที่ `system_status.go:15-19`).

7. **แก้ `real_firewall.go` = งาน security-sensitive.** Step 7 ต้อง **additive ล้วน**:
   ห้ามขยับลำดับ rule, ห้ามแตะโครงสร้าง 4 ส่วนของ input chain (sanity → audit → dynamic
   accept → final drop) ตาม `tech_stack_design.md` §4.3. **กัน:** diff ต้องมีเฉพาะการ
   set `UserData` + test เดิมใน `kernel/policy_chain_test.go` ต้องผ่านโดยไม่แก้ expectation
   ของลำดับ/จำนวน rule; ให้ผู้ review อ่าน diff บรรทัดต่อบรรทัด.

8. **อย่าให้ผู้ใช้เข้าใจว่า Top Talkers รวมกันได้เท่ากับ WAN total.** ตัวเลขจาก conntrack
   เป็นประมาณการ (§2.4) และไม่ครอบคลุม traffic ที่ไม่ถูก conntrack จับ. **กัน:** คืน
   `observedBytes` + `estimated: true` จาก API แล้วให้ UI คิด % จาก `observedBytes`
   พร้อมป้าย "ประมาณการ" — **ห้าม**เอา `%` ไปคูณกับยอด WAN จาก `/dashboard/traffic`.

9. **Service Objects เปลี่ยนได้ตลอดเวลา.** ผู้ใช้เพิ่ม/ลบ service object ระหว่างที่
   aggregator ทำงานอยู่ → หมวดเก่าที่สะสมไว้จะค้างอยู่ใน bucket. **กัน:** ยอมรับว่า
   bucket เก่ายังใช้ชื่อหมวดตอนที่นับ (self-healing principle: ไม่ลบข้อมูลผู้ใช้ทิ้งเอง)
   และ refresh cache ทุก ~60s; หมวดที่ไม่มี service object แล้วให้ยังแสดงต่อจนหลุด window.

10. **IPv6 อาจไม่มีบนเครื่อง.** `ConntrackTableList(..., AF_INET6)` อาจ error บนเครื่องที่
    ปิด IPv6 (installer เลือก IPv4-only เป็นค่าเริ่มต้น — `install.sh:480-483`). **กัน:**
    error ของ family ใด family หนึ่งต้อง log แล้วไปต่อ ไม่ทำให้ dump ทั้งก้อนล้ม.

11. **หน่วยเวลาและ window.** bucket เป็น 5 นาที → `window=1h` = 12 bucket ล่าสุด,
    `24h` = ทั้ง ring. หลัง reboot จะมี bucket ไม่ครบ (เหมือน BandwidthCard วันนี้) —
    frontend ต้องรับสภาพนี้ได้ ไม่ใช่แสดง 0% หรือ error.

12. **อย่าเผลอเขียนลง SQLite.** ทุกอย่างใน `traffic_stats.go` ต้องอยู่ใน RAM เท่านั้น
    (SD card write cycles) — เหมือน log ring buffer และ traffic bucket เดิม; ยอมรับว่า
    ข้อมูลหายเมื่อ reboot.

13. **`parsePortSpec` ห้ามมีสองชุด.** ถ้า service เขียน parser ของตัวเอง กฎการตีความ port
    ของ firewall กับของ dashboard จะ drift จากกันเมื่อมีคนแก้ข้างเดียว. **กัน:** ย้ายไป
    `model.ParsePortSpec` แล้วให้ `kernel/real_firewall.go:710` delegate (Step 1).

## 6. Summary Checklist (Definition of Done)

**Spike**
- [ ] T-00 ยืนยันบน Pi จริง: `nf_conntrack_acct=1` → `ConntrackTableList` คืน `Bytes>0`,
      `SrcIP` เป็น IP ก่อน NAT, วัดจำนวน flow/เวลา dump, ยืนยัน `GetRules` คืน counter

**Backend — model / kernel**
- [ ] T-01 DTO ใหม่ใน `model/types.go` + `model.ParsePortSpec` (kernel delegate)
- [ ] T-02 `TrafficAccountingManager` ใน `kernel/interfaces.go`
- [ ] T-03 `real_traffic_account.go` `DumpFlows()` (IPv4+IPv6, cap, degrade รายตระกูล) 🔒
- [ ] T-04 `DumpRuleCounters()` อ่าน nftables counter + decode UserData 🔒
- [ ] T-05 `MockTrafficAccounting` ใน `kernel/mock.go` (สอดคล้องกับ mock DHCP leases)
- [ ] T-06 capability `conntrack` (real + mock prober + catalog + ข้อความไทย)
- [ ] T-07 ติด `UserData` ให้ user rule ใน `real_firewall.go` (additive ล้วน) 🔒

**Backend — service / api / wiring**
- [ ] T-08 `service/traffic_stats.go` (poll 10s, delta+clamp, seed รอบแรก, bucket 5m×288,
      categorization ตาม Service Objects → "Other", rule-counter baseline+reset detect,
      hostname enrich, cap/prune) + unit test (delta, reset, cap, categorization)
- [ ] T-09 `HandleGetTrafficDetail` + route `GET /api/dashboard/traffic-detail` (whitelist `window`)
- [ ] T-10 wiring ใน `cmd/pigate/main.go` (real/mock select + `Start(monitorCtx)`)
- [ ] `cd backend && go build ./... && go test ./...` ผ่าน

**Frontend**
- [ ] T-11 `getTrafficDetail` + types ใน `dashboardService.ts` (+ mock branch สมจริง)
- [ ] T-12 `ProtocolBreakdownCard` / `TopTalkersCard` / `TopRulesCard` ในแท็บ Detailed
      เท่านั้น, ใช้ `components/ui/*`, theme variable ล้วน, flat design, dark+light,
      ป้าย "ประมาณการ" บน 2 การ์ดแรก
- [ ] `cd frontend && yarn build && yarn lint` ผ่าน

**Ops / Docs**
- [ ] T-13 `install.sh`: `net.netfilter.nf_conntrack_acct = 1` ใน `/etc/sysctl.d/99-pigate.conf`
      + `nf_conntrack` ใน `/etc/modules-load.d/pigate.conf` + อัปเดตข้อความสรุปท้ายสคริปต์
- [ ] T-14 `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (sync), หัวข้อใหม่ใน
      `docs/tech_stack_design.md`, README Feature Status

**Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก task เสร็จ)**
- [ ] mock: `-mock=true -allow-dev-cors` → แท็บ Detailed แสดงครบ 3 การ์ด มีข้อมูล ไม่ error,
      Top Talkers แสดง **ชื่อเครื่อง** (ไม่ใช่ IP เปล่า), มีหมวด "Other" ปรากฏ
- [ ] real device: หลังรัน `install.sh` ใหม่ → `sysctl net.netfilter.nf_conntrack_acct` = 1
- [ ] real device: ดาวน์โหลดไฟล์ใหญ่จากเครื่องหนึ่งใน LAN → เครื่องนั้นขึ้นเป็นอันดับ 1 ใน
      Top Talkers ภายใน ~1 นาที และหมวดของ traffic ตรงกับ Service Object ที่ผู้ใช้นิยาม
- [ ] real device: ยอดรวม `observedBytes` (1h) อยู่ในระดับใกล้เคียงกับพื้นที่ใต้กราฟ
      BandwidthCard ช่วงเดียวกัน (ไม่ต่างกันหลายเท่า) — ใช้ยืนยัน §2.4
- [ ] real device: แก้ policy rule 1 ข้อ (trigger `ApplyRules`) → Top Rules ไม่แสดงตัวเลข
      ผิดปกติ/ติดลบ/ค่ามหาศาลจาก underflow
- [ ] เครื่องที่ยังไม่เปิด `nf_conntrack_acct` → เห็น Alert capability degraded พร้อม
      ข้อความบอกวิธีแก้ ไม่ใช่การ์ดว่างเปล่าเงียบๆ
- [ ] ปล่อยรัน ≥ 1 ชั่วโมงบนเครื่องจริง → memory ของโปรเซสไม่โตต่อเนื่อง (ตรวจ prune/cap)
- [ ] `-disable-edit=true` และ role read-only ยังเปิดดูการ์ดได้ (GET ล้วน)
- [ ] ตรวจ diff ของ `real_firewall.go` แบบบรรทัดต่อบรรทัด: มีเฉพาะการ set `UserData`
      และการ delegate `parsePortSpec` — ลำดับ/จำนวน rule ไม่เปลี่ยน 🔒

## 7. Future / Out of scope — Phase 2: nftables named counters (draft สำหรับเปิด issue)

**ปัญหาที่ยังเหลือหลัง Phase 1:** ตัวเลข Protocol Breakdown / Top Talkers เป็น
**ประมาณการ** (~95–99%, ดู §2.4) และตกลงชัดเจนในสภาวะผิดปกติ (scan/P2P ที่สร้าง flow
สั้นจำนวนมาก จน conntrack evict เร็ว) เพราะเป็นการ poll snapshot ไม่ใช่การนับที่ต้นทาง

**แนวทาง Phase 2:** ให้ **kernel นับให้แทน** ผ่าน nftables

1. *Protocol Breakdown:* สร้าง **named counter object** ชุดเล็ก (ตามจำนวนหมวดที่ผู้ใช้มี
   หรือ top-N หมวด) แล้วเพิ่ม rule ใน forward chain ที่ match dport ของแต่ละหมวดแล้ว
   `counter name <obj>` (ไม่เปลี่ยน verdict) — อ่านค่าผ่าน `conn.GetObjects()`
2. *Top Talkers:* ใช้ **dynamic set / meter** keyed ด้วย `ip saddr` ที่มี stateful
   counter ต่อ element (`update @tally { ip saddr counter }`) แล้วอ่านผ่าน
   `conn.GetSetElements()`

**ข้อดี:** แม่นยำระดับ byte 100%, ไม่พลาด flow สั้น, cost ย้ายไปอยู่ใน kernel fast path
(ถูกกว่าการ dump ตารางเป็นรอบๆ), ไม่ต้องพึ่ง `nf_conntrack_acct`

**ความเสี่ยง / สิ่งที่ต้องตรวจสอบก่อน:**
- ต้อง **แทรก rule เข้า chain ที่มีโครงสร้างบังคับ 4 ส่วน** (`tech_stack_design.md` §4.3)
  → เป็นงาน security-sensitive ที่ต้อง review เข้มและมี test ครอบคลุมลำดับ rule
- ต้อง spike ยืนยันก่อนว่า **`google/nftables v0.3.0` รองรับ set element ที่มี counter
  expression และ named counter object** จริง (ยังไม่ได้ยืนยันในการสำรวจครั้งนี้) — ถ้าไม่
  รองรับ ต้องอัปเดต/แทนที่ dependency ซึ่งขัดกับหลัก "dependency น้อยและ pin ไว้"
- dynamic set ต่อ IP มี **ขนาดไม่จำกัด**ถ้าไม่ตั้ง `size`/timeout → ต้องออกแบบ GC ใน kernel
- ต้องคิดว่า counter จะรีเซ็ตทุกครั้งที่ `ApplyRules` เหมือนกัน (ปัญหาเดียวกับ §5 ข้อ 2)
  หรือจะใช้ named object ที่คงอยู่ข้ามการ apply

**ทางเลือกกลางที่ควรพิจารณาคู่กัน:** conntrack **DESTROY event listener**
(netlink multicast `NFNLGRP_CONNTRACK_DESTROY`) ซึ่งจะเก็บ byte สุดท้ายของทุก flow ตอนมัน
ถูกลบ ทำให้ความแม่นเข้าใกล้ 100% โดย **ไม่ต้องแตะ firewall chain เลย** — ต้นทุนคือเพิ่ม
dependency `ti-mo/conntrack` หรือเขียน CTA attribute parser เองบน `mdlayher/netlink`
(มีเป็น indirect dependency อยู่แล้ว) ซึ่งอาจคุ้มกว่าและเสี่ยงน้อยกว่า Phase 2 แบบเต็ม
