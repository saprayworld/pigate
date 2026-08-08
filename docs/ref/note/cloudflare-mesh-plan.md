# Cloudflare Mesh (Site-to-Site) — เชื่อม PiGate หลาย site เข้าหากันผ่าน Cloudflare

> เอกสารแผนงานสำหรับฟีเจอร์: ทำให้ PiGate สองเครื่อง (หรือ PiGate กับ site อื่น)
> คุยกันแบบ **site-to-site / mesh** ผ่านเครือข่ายของ Cloudflare — แต่ละ site
> ประกาศ LAN CIDR ของตัวเอง แล้ว Cloudflare route ข้าม site ให้ ต่างจากฟีเจอร์
> `cloudflare-tunnel-plan.md` ที่เป็น remote-access tunnel ขาเดียว
>
> วันที่เขียน: 2026-08-08 (อัปเดต 2026-08-08 — เคาะสถาปัตยกรรม deployment แล้ว)
> · Branch อ้างอิง: `main` (จะแยก branch ใหม่ก่อนเริ่มโค้ด)
> สถานะใน README Feature Status: ไม่มีแถวนี้ → เป้าหมายคือเพิ่มแถวใหม่ "Cloudflare Mesh"
> เอกสารที่ต้องอ่านคู่กัน: `docs/ref/todo/cloudflare-tunnel-plan.md` (ฟีเจอร์คู่ขนาน),
> `docs/tech_stack_design.md` §4.3 (โครง input/forward chain), `CLAUDE.md`
>
> **การตัดสินใจสำคัญที่เคาะแล้ว (คุยกับเจ้าของโปรเจกต์ 2026-08-08):** mesh
> connector รันบน **อุปกรณ์แยกจาก PiGate Gateway หลักเสมอ** เป็นสถาปัตยกรรม
> baseline (ไม่ใช่แค่ทางเลือกใน decision B อีกต่อไป) เหตุผลคือ WARP client
> hijack DNS ของ host ไปหา Cloudflare Gateway ซึ่งชนตรงกับ dnsmasq ที่ Gateway
> หลักรันอยู่ และการรวมสอง service ที่กระทบ routing/firewall/DNS ไว้เครื่องเดียว
> เพิ่ม blast radius ถ้าอย่างใดอย่างหนึ่งพัง — ดู §0.4 และ §2.3 (B) ที่ปรับปรุงแล้ว

---

## 0. เป้าหมายและขอบเขต

### 0.1 เป้าหมาย

- super_admin เข้าหน้า `/system/cloudflare-mesh` → ใส่ **connector token** ของ
  site นี้, ประกาศ **LAN CIDR ของ site ตัวเอง** (advertised routes), และบันทึก
  **CIDR ของ site ปลายทาง** (remote sites) → PiGate:
  1. คุม service ของ mesh node บนเครื่อง (start/stop/restart/status) ผ่าน D-Bus systemd
  2. เตรียม host prerequisite ให้ถูกต้อง (IP forwarding, MTU/MSS, rp_filter)
  3. **สร้าง/ตรวจ firewall policy สำหรับ traffic ข้าม site** (forward chain ของ
     PiGate เป็น default drop — ถ้าไม่ทำขั้นนี้ mesh จะ "ต่อติดแต่ใช้งานไม่ได้")
  4. ตรวจ **CIDR overlap** ระหว่าง LAN ของตัวเอง / static route เดิม / remote site
     แล้วเตือนก่อนบันทึก
  5. แสดงสถานะ + บันทึก event log ทุก mutation, และ apply desired state ตอน boot
- ผลลัพธ์ที่วัดได้: host ใน LAN ของ site A ping/เข้าถึง host ใน LAN ของ site B ได้
  ตาม policy ที่กำหนด โดยไม่ต้องเปิด port ขาเข้าจากอินเทอร์เน็ตที่ทั้งสองฝั่ง

### 0.2 ความสัมพันธ์กับ `cloudflare-tunnel-plan.md`

> **ข้อค้นพบสำคัญ (ต้องอ่านก่อนตัดสินใจอะไร):** Cloudflare Mesh คือชื่อใหม่ของ
> **WARP Connector** และมันไม่ได้ใช้ `cloudflared` — มันใช้ **WARP client**
> (แพ็กเกจ `cloudflare-warp`, daemon `warp-svc.service`, CLI `warp-cli`)
> ดังนั้น Mesh **ไม่ใช่ "เปิด flag เพิ่ม" บน tunnel เดิม** แต่เป็นคนละ binary
> คนละ service คนละ token คนละ subsystem

| ประเด็น | Cloudflare Tunnel (แผนเดิม) | Cloudflare Mesh (แผนนี้) |
|---|---|---|
| Binary / service | `cloudflared` / `cloudflared.service` | `cloudflare-warp` / `warp-svc.service` |
| ทิศทาง | outbound-only, remote access เข้ามาที่ service ที่ประกาศไว้ | bidirectional, route ทั้ง subnet ข้าม site |
| Token | Tunnel token (env `TUNNEL_TOKEN`) | Connector token (ลงทะเบียนกับ warp-svc) |
| กระทบ routing table ของเครื่อง | ไม่ | **ใช่** (สร้าง virtual iface + route ของ mesh/remote CIDR) |
| กระทบ firewall posture | เปิดทางเข้าเฉพาะ service ที่ประกาศ | **เปิดทาง L3 ระหว่าง LAN สอง site** (กว้างกว่ามาก) |
| กระทบ DNS ของเครื่อง | ไม่ | **ใช่** (WARP redirect DNS ของ host ไป Gateway — ชนกับ dnsmasq ของ PiGate) |

**สิ่งที่ใช้ร่วมกันได้จริง:** เฉพาะ *pattern* — `dbus_systemd.go`, single-row
settings table, token validation/masking, `superAdminRoute`, polkit allowlist,
โครงหน้า frontend และ backup/restore เท่านั้น **ไม่ใช่โค้ดเดียวกัน**

**ทั้งสองฟีเจอร์รันพร้อมกันบนเครื่องเดียวได้** แต่ Cloudflare ระบุว่าต้องใช้
Split Tunnel กันไม่ให้ traffic ของ tunnel วิ่งเข้า mesh (ดู §5 ข้อควรระวัง 9)

### 0.3 นอกขอบเขต (ชัดเจน)

- **ไม่ทำการตั้งค่าฝั่ง Cloudflare ให้** — การสร้าง mesh node, ประกาศ CIDR route
  ในแดชบอร์ด, ตั้ง Split Tunnel (include `100.96.0.0/12` + CIDR ของแต่ละ site),
  เปิด "Traffic and DNS mode" ทั้งหมดทำที่ Cloudflare Zero Trust dashboard
  PiGate เก็บค่า CIDR เหล่านี้ไว้เพื่อ **ทำฝั่ง host** (route/firewall/validate)
  และเพื่อแสดงผล ไม่ใช่เพื่อ push ขึ้น Cloudflare
- **ไม่เรียก Cloudflare API / ไม่ทำ OAuth login** (เหมือนแผน tunnel)
- **ไม่ดาวน์โหลด/ติดตั้งแพ็กเกจ `cloudflare-warp` ให้อัตโนมัติ** — ผู้ใช้ติดตั้งเอง
  ตามคู่มือ Cloudflare, `install.sh` แค่ตรวจและเตือน
- **ไม่ทำ mesh ด้วยโปรโตคอลอื่น** (WireGuard/Tailscale/IPsec) ในแผนนี้ — ถ้าเจ้าของ
  โปรเจกต์เลือกทางนั้นภายหลัง ต้องเขียนแผนใหม่ (ดู §2.2 ทางเลือกที่ตัดทิ้ง)
- **ไม่ทำ HA/failover ระหว่างหลาย mesh node ใน site เดียว** (Cloudflare รองรับ แต่
  เกินขอบเขตรอบแรก)
- ไม่เพิ่ม metric/traffic ของ mesh เข้า Dashboard (รอบแรกแสดงแค่ status)

### 0.4 สถาปัตยกรรม deployment (resolved — แยกอุปกรณ์เป็น baseline)

**เหตุผลที่เลือกทำผ่าน Cloudflare แทน WireGuard ล้วนๆ:** โปรเจกต์นี้ตั้งใจ
ออกแบบให้เป็น homelab ที่ประหยัดที่สุด — WireGuard เองไม่มี NAT
traversal/rendezvous ในตัว ถ้าทั้งสอง site อยู่หลัง NAT/CGNAT (เคสปกติของ
home internet) ต้องมีฝั่งใดฝั่งหนึ่งเปิด public endpoint หรือไม่ก็ต้องมี relay
กลางที่มี public IP แน่นอน (= ต้องเช่า VPS มีค่าใช้จ่ายรายเดือน) ส่วน Cloudflare
Mesh ให้ edge network ของ Cloudflare เป็นตัวกลางฟรี ทั้งสอง site แค่ outbound
connection ออกไปหา Cloudflare ไม่ต้องเปิด public port เลย นี่คือเหตุผลที่แผนนี้
ยังคงเลือกกลไก Cloudflare Mesh ต่อไป แม้จะมีความซับซ้อนเรื่อง DNS/deployment
มากกว่า tunnel เดี่ยว (ดู §2.2 ที่ยังคงบันทึก WireGuard ไว้เป็นแผนสำรอง)

**สถาปัตยกรรม baseline ที่เลือก:** mesh connector (`warp-svc`) รันบน
**อุปกรณ์แยก** (เช่น Pi ตัวที่สอง/Pi Zero ราคาถูก หรือ VM เล็กๆ) ที่วางอยู่
**หลัง** PiGate Gateway หลักในเครือข่ายเดียวกัน (เป็นแค่ host อีกตัวหนึ่งใน LAN)
ไม่ใช่ลงบนตัว PiGate Gateway หลักเอง:

```
        Site A                                          Site B
  LAN 192.168.10.0/24                              LAN 192.168.20.0/24
        │                                                    │
  [PiGate A — Gateway หลัก]                         [PiGate B — Gateway หลัก]
  routing / firewall / dnsmasq / DHCP               routing / firewall / dnsmasq / DHCP
        │ (route ปกติไปหา mesh node                          │
        │  เหมือน host อื่นใน LAN)                            │
        │                                                    │
  [Mesh Node A — อุปกรณ์แยก]  ←── Cloudflare edge ──→  [Mesh Node B — อุปกรณ์แยก]
   warp-svc.service (token site A)                    warp-svc.service (token site B)
```

**ผลต่อขอบเขตแผนนี้:** PiGate Gateway หลัก **ไม่ต้องรู้จัก Cloudflare/WARP เลย**
— แค่ต้อง route traffic ไปหา mesh node ตามปกติ (เหมือน route ไปหาเซิร์ฟเวอร์
ตัวหนึ่งใน LAN) แล้วเปิด forward-chain policy ให้ผ่าน คำถามคือ **ฟีเจอร์นี้ควร
เป็น subsystem ใน PiGate เอง (สำหรับติดตั้งบน mesh node ที่แยกออกไป) หรือทำเป็น
แค่คู่มือ/สคริปต์ config แยกที่ไม่ใช่ส่วนหนึ่งของ PiGate binary?** — ดูเพิ่มเติมที่
decision B (ปรับปรุงแล้ว) ใน §2.3 ซึ่งกระทบรูปร่างของ Step 1-13 ทั้งหมด

**การอยู่ร่วมเครื่องเดียวกับ Gateway หลัก (B1 เดิม) ถูกลดสถานะเป็น "ทางเลือก
อนาคต"** ไม่ใช่ baseline ของแผนนี้อีกต่อไป — จะพิจารณาใหม่ก็ต่อเมื่อ Step 0
พิสูจน์ด้วยข้อมูลจริงว่าอยู่ร่วมกับ dnsmasq/routing ของ Gateway หลักได้โดยไม่
กระทบผู้ใช้เดิม ไม่ใช่สมมติฐานตั้งต้น

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

| ส่วน | สถานะ | ไฟล์:บรรทัด |
|---|---|---|
| D-Bus systemd helper (start/stop/restart/สถานะ unit ใดก็ได้) | มีครบ ใช้ซ้ำได้ทันที | `backend/internal/kernel/dbus_systemd.go:79-126` |
| Mapping ActiveState/LoadState → running/stopped/failed/unavailable | มีแล้ว | `backend/internal/service/system_service.go:101-113` |
| Polkit allowlist unit ของ pigate (deny-by-default + guard `subject.user != "pigate"`) | มีแล้ว ต้อง**เพิ่ม `warp-svc.service`** | `install.sh:231-295` (allowlist ที่ `:259-266`) |
| `net.ipv4.ip_forward = 1` + `rp_filter` persist ข้าม reboot | **มีแล้ว** (`rp_filter=2` = loose ซึ่งพอดีกับ asymmetric routing ของ mesh) | `install.sh:446-514` |
| `net.ipv6.conf.all.forwarding` / `accept_ra=2` (Mesh ต้องการ) | **ยังไม่มี** ต้องเพิ่ม | `install.sh:479-508` |
| Static route CRUD + apply ลง kernel จริง (netlink) | มีครบ (DB → `ApplyRoutes` reconcile) | `backend/internal/service/routing.go:391-440`, `backend/internal/kernel/real_routing.go:34-220` |
| Marker route ของ PiGate = **proto 120** — `ApplyRoutes` ลบเฉพาะ route ที่เป็น proto 120 หรือ route ที่มี gateway เมื่อเปิด `allowEditSystemRoutes` | มีแล้ว (สำคัญมากกับ mesh: route ที่ WARP สร้างเองเป็น device route ไม่มี gw → **ไม่ควร**โดนลบ แต่ **ต้องพิสูจน์ใน Step 0**) | `real_routing.go:87-147` |
| Netlink monitor เฝ้า route/link/addr แล้ว publish event → routing reconcile (debounce) | มีแล้ว | `backend/internal/service/netlink_monitor.go:107-156`, `routing.go:391-440` |
| Forward chain **default drop** + user policy rules + final drop-log | มีแล้ว → cross-site traffic ต้องมี policy ถึงจะผ่าน | `backend/internal/kernel/real_firewall.go:405-506` |
| Policy rule มี `InInterface`/`OutInterface`/`Source`/`Destination`/`Nat` | มีแล้ว → ใช้เขียนกฎ mesh↔LAN ได้เลย | `backend/internal/model/types.go:126-155` |
| Source NAT แบบ policy-driven (fwmark 0x1 → masquerade) | มีแล้ว | `real_firewall.go:571-601` |
| MSS clamping / ตั้ง MTU ของ interface ผ่าน UI | **ยังไม่มี** (Mesh แนะนำ MTU 1280 / MSS 1240) | grep ไม่พบ |
| Interface list มาจาก netlink (virtual iface ใหม่จะโผล่เอง) | มีแล้ว — ต้องยืนยันว่า iface ของ WARP แสดงผลถูกต้องและไม่ถูก "จัดการ" โดยพลาด | `backend/internal/kernel/real_network.go`, `service/interface.go` |
| Capability probe framework (`firewall`, `dbus`, `dnsmasq`, `resolved`, `conntrack`…) | มีแล้ว เพิ่ม id ใหม่ได้ | `backend/internal/service/system_capability.go:25-30` |
| Pattern เก็บ secret ใน SQLite (plaintext + mask ที่ handler) | มีแล้ว | `backend/internal/db/wifi_preset_repo.go:12-17`, `service/backup.go:136` |
| Pattern single-row settings table | มีแล้ว | `backend/internal/db/connection.go:383-407` |
| Route/handler แบบ super_admin only (รวม GET) | มีแล้ว | `backend/internal/api/router.go:87-96` |
| Sidebar group "System" + `SuperAdminRoute` | มีแล้ว | `frontend/src/components/app-sidebar.tsx:158-166`, `frontend/src/App.tsx:193-204` |
| DNS server ของ PiGate (dnsmasq) รันบนเครื่องเดียวกัน | มีแล้ว — **จุดชนกับ Mesh โดยตรง** (WARP redirect DNS ของ host) | `backend/internal/kernel/dns_server.go`, `service/dns_server.go` |
| Cloudflare Mesh / WARP (backend/frontend/install.sh) | **ยังไม่มีอะไรเลย** | grep ไม่พบ `warp`/`mesh` |

**สรุป:** ชิ้นส่วน "คุม systemd ผ่าน D-Bus + settings table + route CRUD +
firewall policy + NAT" มีครบแล้วทั้งหมด งานใหม่จริงๆ ของแผนนี้คือ
(ก) subsystem ใหม่ที่คุม `warp-svc` (ข) การ **validate/ประสาน CIDR** ระหว่าง mesh
กับ routing/firewall ที่มีอยู่ และ (ค) การพิสูจน์ข้อจำกัดฝั่ง OS ใน Step 0
ซึ่งเป็นความไม่แน่นอนที่ใหญ่ที่สุดของแผนนี้

---

## 2. แนวทางเทคนิค

### 2.1 กลไกที่เลือก: Cloudflare Mesh (WARP Connector) บนอุปกรณ์แยก + PiGate Gateway หลักเป็นแค่ "ผู้ route ไปหา mesh node"

> อัปเดตตาม §0.4 — mesh node เป็นอุปกรณ์แยกจาก Gateway หลักเสมอในแผนนี้

```
        Site A                                          Site B
  LAN 192.168.10.0/24                              LAN 192.168.20.0/24
        │                                                    │
  [PiGate A — Gateway หลัก, eth1/LAN]                [PiGate B — Gateway หลัก, eth1/LAN]
   nftables forward chain (default drop)              nftables forward chain (default drop)
   + policy: LAN↔mesh-node-A                           + policy: LAN↔mesh-node-B
        │ (route ปกติไปหา mesh node ใน LAN เดียวกัน)              │
        │                                                    │
  [Mesh Node A — อุปกรณ์แยก, mesh iface 100.96.x.x] ←Cloudflare edge→ [Mesh Node B — อุปกรณ์แยก]
   warp-svc.service (token ของ site A)              warp-svc.service (token ของ site B)
```

หน้าที่ที่ **Cloudflare dashboard** ทำ (ไม่ใช่ PiGate): สร้าง mesh node ต่อ site,
ประกาศ CIDR route ของแต่ละ site, ตั้ง Split Tunnel (Include mode) ให้มี
`100.96.0.0/12` + CIDR ของ site ปลายทาง, เปิดโหมด Traffic and DNS

หน้าที่ที่ **PiGate** ทำ (ขอบเขตของแผนนี้ — รันบน **mesh node ที่แยกออกไป**
ตาม §0.4, ไม่ใช่บน Gateway หลัก):
1. lifecycle ของ `warp-svc.service` ผ่าน D-Bus (start/stop/restart/status/loaded)
2. ตรวจ prerequisite ฝั่ง host แบบ read-only ผ่าน `/proc/sys` (`ip_forward`,
   `ipv6 forwarding`, `rp_filter`) แล้วรายงานในหน้า UI + capability probe
3. เก็บ "โครงร่าง mesh" ใน DB: token, advertised CIDR ของ site นี้, รายการ remote site
4. **ตรวจ CIDR overlap** กับ interface address, static route ใน DB และ remote site
   อื่นๆ → ปฏิเสธ/เตือนก่อนบันทึก
5. **generate/แนะนำ firewall policy** สำหรับ traffic ข้าม site (ไม่ auto-apply เงียบๆ
   — ดู §2.3 จุดตัดสินใจ D) — policy นี้จะไปสร้างจริงที่ **Gateway หลัก** (ไม่ใช่
   บน mesh node เอง) เพราะ forward chain default-drop อยู่ที่ Gateway หลัก
   ดู decision E ด้านล่างว่ากลไกส่ง policy ข้ามสองอุปกรณ์นี้จะทำอย่างไร
6. event log + desired state ตอน boot (`InitApplyConfig`)

### 2.2 ทางเลือกที่พิจารณาแล้วตัดทิ้ง

- **ตัดทิ้ง: ใช้ `cloudflared` + private network routing ทำ site-to-site**
  `cloudflared` เป็น connector ขาเดียว (ingress จาก Cloudflare → LAN) ตัวเริ่ม
  traffic ต้องเป็น WARP client เสมอ ดังนั้นสอง site ที่มีแต่ `cloudflared`
  จะคุยกันเองไม่ได้ — มันคือ user-to-site ซึ่ง `cloudflare-tunnel-plan.md`
  ครอบคลุมอยู่แล้ว ไม่ใช่ mesh
- **ตัดทิ้ง: WARP-to-WARP ล้วน** — เชื่อม *เครื่อง* ถึงเครื่อง ไม่ประกาศทั้ง subnet
  ไม่ตอบโจทย์ "LAN ทั้งฝั่งคุยกับ LAN อีกฝั่ง"
- **ตัดทิ้ง: Magic WAN (GRE/IPsec + Anycast)** — เป็นแพ็กเกจระดับ enterprise
  ไม่เหมาะกับ Raspberry Pi ของผู้ใช้ทั่วไป และต้องมี public static IP
- **ตัดทิ้ง (รอบนี้): WireGuard/Tailscale เป็น backend ทางเลือก** — แก้ปัญหา
  DNS/exec ได้หมดและคุมผ่าน netlink ได้ตรงๆ (wireguard เป็น netlink device จริง)
  แต่ผิดโจทย์ที่เจ้าของตั้งไว้ว่า "ผ่าน Cloudflare" → บันทึกไว้เป็น **แผนสำรอง**
  ถ้า Step 0 พบว่า Mesh อยู่ร่วมกับ dnsmasq/routing ของ PiGate ไม่ได้จริง
- **ตัดทิ้ง: implement client ของ IPC socket ของ `warp-svc` เอง** — โปรโตคอลไม่มี
  เอกสารสาธารณะ เปลี่ยนได้ทุกเวอร์ชัน = หนี้ทางเทคนิคที่ควบคุมไม่ได้

### 2.3 จุดตัดสินใจที่ต้องให้เจ้าของโปรเจกต์เคาะ **ก่อน** เริ่ม Step 1

> ทุกข้อในนี้เปลี่ยนรูปร่างของ Step 3-6 อย่างมีนัยสำคัญ อย่าเริ่มโค้ดจนกว่าจะสรุป

**A. การลงทะเบียน token (enrollment) — ข้อจำกัดที่ชนกับกฎ "ห้าม exec" โดยตรง**
`warp-cli connector new <TOKEN>` เป็นทางเดียวที่มีเอกสารรองรับสำหรับผูก
mesh node กับ Cloudflare และมันคุยกับ `warp-svc` ผ่าน Unix socket ภายในที่ไม่มี
D-Bus / Netlink API ให้ใช้ ทางเลือก:
- **A1 (แนะนำ): PiGate ไม่แตะ enrollment เลย** — ผู้ดูแลรัน `warp-cli connector new`
  บนเครื่องเองครั้งเดียว (SSH) PiGate ทำเฉพาะ lifecycle + host networking +
  สถานะ + policy UI จะมี checklist บอกคำสั่งที่ต้องรันและตรวจว่า "ลงทะเบียนแล้ว
  หรือยัง" จากสถานะ service/interface → **รักษากฎห้าม exec ได้ 100%** และ token
  ไม่ต้องถูกเก็บใน DB ของ PiGate เลย (ลด attack surface ลงมาก)
- **A2: อนุญาต `exec.Command("warp-cli", "connector", "new", token)` เป็นข้อยกเว้น
  เฉพาะจุด** (argv ไม่ผ่าน shell, path binary hardcode, token ผ่าน validation
  เข้ม) — ได้ UX ที่ดีกว่า (กรอก token ในหน้าเว็บแล้วจบ) แต่ **ต้องเป็นการตัดสินใจ
  ของเจ้าของโปรเจกต์อย่างชัดเจน** และต้องบันทึกเป็นข้อยกเว้นใน
  `docs/tech_stack_design.md` พร้อมเหตุผล (ไม่มี Netlink/D-Bus ทางเลือก)
- **A3: เขียน config/token file ให้ warp-svc อ่านเอง** — ต้องยืนยันใน Step 0 ว่า
  ทำได้จริงหรือไม่ (ถ้าได้ นี่คือทางที่ตรงกับ pattern `cloudflared.env` ของแผน
  tunnel ที่สุด)

**B. DNS ชนกัน — ✅ RESOLVED (2026-08-08): เลือก B2 เป็น baseline**
เอกสาร Cloudflare เตือนตรงๆ ว่าอย่าติดตั้ง mesh node บนเครื่องที่รัน DNS
service เพราะ WARP จะ redirect DNS query ของ host ไป Gateway ซึ่งชนกับ dnsmasq
ของ PiGate (ฟีเจอร์ DNS Server + DHCP ที่ Completed อยู่แล้ว)
- ~~B1: ยอมรับความเสี่ยง รันบนเครื่องเดียวกับ Gateway หลัก~~ — **ไม่ใช้เป็น
  baseline อีกต่อไป** (เหตุผลตาม §0.4: ลด blast radius, เลี่ยงชน dnsmasq)
  เก็บไว้เป็น **ทางเลือกอนาคต** ถ้า Step 0 พิสูจน์ว่าอยู่ร่วมกันได้จริงโดยไม่กระทบ
  ผู้ใช้เดิม
- **B2 (เลือกเป็น baseline): mesh node เป็นอุปกรณ์แยกในซับเน็ตเดียวกัน**
  ตามที่ Cloudflare แนะนำเอง → PiGate Gateway หลัก **ไม่ต้องรู้จัก
  Cloudflare/WARP เลย** ลดบทบาทเหลือแค่ "จัดการ route/firewall ไปหา mesh node
  ตัวนั้นเหมือน host อื่นใน LAN" — ส่วน "PiGate" ที่ทำ lifecycle/status/CIDR ใน
  §2.1 ข้อ 1-6 รันอยู่บน **mesh node** (คนละ instance จาก Gateway หลัก) ดู
  decision E ที่เพิ่มใหม่ด้านล่างสำหรับผลกระทบต่อ Step 6/8/13
- ~~B3: ตัด DNS Server ออกอัตโนมัติเมื่อเปิด mesh~~ — ไม่จำเป็นแล้วเพราะ B2
  แยกอุปกรณ์อยู่แล้ว DNS Server ของ Gateway หลักไม่ถูกแตะเลย

**C. ขอบเขต "ประกาศ CIDR"** — PiGate เก็บ CIDR ไว้เฉยๆ เพื่อ validate/สร้าง policy
(ตามแผนนี้) หรือจะต้อง sync ขึ้น Cloudflare ด้วย (= ต้องใช้ Cloudflare API +
API token = ขัดกับ "นอกขอบเขต" ของแผน tunnel เดิม) → แผนนี้ตั้งต้นที่ **เก็บไว้ใช้
ฝั่ง host เท่านั้น**

**D. Firewall policy: auto-create หรือ suggest** — traffic ข้าม site จะโดน forward
chain default drop ทันทีที่ **Gateway หลัก** (ไม่ใช่ที่ mesh node) ทางเลือก:
(D1) สร้าง policy ให้อัตโนมัติเมื่อเปิด mesh, (D2) แสดงปุ่ม "สร้าง policy ที่
แนะนำ" ให้ผู้ใช้กดยืนยัน แล้วไปแก้ต่อในหน้า Firewall Policy ได้ตามปกติ → **แผนนี้
เสนอ D2** (การเปิดทาง L3 ข้าม LAN ต้องเป็นเจตนาที่ผู้ใช้กดยืนยัน ไม่ใช่ผลข้างเคียง)
**อัปเดตตาม B2:** เพราะ mesh node กับ Gateway หลักเป็นคนละอุปกรณ์/คนละ PiGate
instance กันแล้ว ปุ่ม "สร้าง policy ที่แนะนำ" ใน UI ของ mesh node **เรียกไม่ถึง**
firewall service ของ Gateway หลักโดยตรงอีกต่อไป — ดู decision E

**E. (เพิ่มใหม่ตาม B2) mesh node กับ Gateway หลักเป็นคนละอุปกรณ์แล้ว การ
"แนะนำ policy" ข้ามอุปกรณ์ทำอย่างไร?**
เดิมแผนออกแบบให้ `SuggestedPolicies()`/`ApplySuggestedPolicies()` เรียก
firewall service ในโปรเซสเดียวกัน (Step 6) แต่ตอนนี้ mesh node กับ Gateway
หลักเป็นคนละเครื่อง คนละ PiGate instance กัน (หรืออาจไม่มี PiGate อยู่บน mesh
node เลยก็ได้) ทางเลือก:
- **E1 (แนะนำ, ง่ายที่สุด, ไม่มี cross-device API):** หน้า UI ของ mesh node
  แสดง "ข้อความ/ตัวอย่าง policy ที่ต้องไปสร้างเอง" (source = mesh CIDR,
  destination = remote CIDR, interface = mesh iface) ให้ผู้ดูแล **ไปสร้างเองใน
  หน้า Firewall Policy ของ Gateway หลัก** (คนละ URL/คนละเครื่อง) ไม่มีการเรียก
  API ข้ามเครื่อง ไม่มี trust relationship ใหม่ระหว่างสอง PiGate instance ที่ต้อง
  ดูแล security เพิ่ม → ยกเลิก endpoint `/suggested-policies/apply` (Step 8/9)
  เหลือแค่ `/suggested-policies` (GET, แสดงข้อความ/ตัวอย่างเท่านั้น)
- **E2:** ทำ cross-device API call (mesh node เรียก Gateway หลักผ่าน token ใหม่)
  — เพิ่ม attack surface และ auth model ใหม่ทั้งหมด ไม่คุ้มกับประโยชน์ที่ได้
  (ตัดทิ้ง)
- **E3:** mesh node ไม่ต้องมี PiGate/UI เลย เป็นแค่สคริปต์ + `warp-cli` ตรงๆ ตาม
  คู่มือ Cloudflare, PiGate เกี่ยวข้องแค่ฝั่ง Gateway หลัก (รับ CIDR ของ mesh node
  มาเป็น "remote site" ธรรมดาแล้วสร้าง policy) — **ลดขอบเขตงานลงมาก** เพราะ
  Step 1-2, 5-6, 12-13 ทั้งหมดยุบเหลือแค่ "จัดการ remote-site CIDR + policy" ที่
  ฝั่ง Gateway หลักอย่างเดียว ไม่ต้องมี kernel manager คุม `warp-svc` เลย
  ควรพิจารณาจริงจังเพราะตัดความซับซ้อน/ความเสี่ยงของแผนทั้งฉบับลงมาก

> **ยังไม่เคาะ E — ต้องตัดสินใจก่อน Step 1** เพราะ E3 เปลี่ยนขนาดงานทั้งแผนอย่าง
> มาก (จาก "subsystem เต็มรูปแบบ" เหลือ "จัดการ remote CIDR + policy เท่านั้น")
> ในขณะที่ E1 ยังคง scope เดิมของ Step 1-15 ไว้ทั้งหมดแต่รันบนอุปกรณ์แยก

---

## 3. ขั้นตอนการทำ (เรียงตาม dependency)

> **Step 0 เป็น blocking** — ห้ามเริ่ม Step 1 ก่อนได้ผลจริงจากบอร์ด เพราะ Step 3-6
> ขึ้นกับข้อเท็จจริงที่ยังไม่ยืนยัน (ชื่อ interface, พฤติกรรม route, DNS, enrollment)

### Step 0 — Spike/PoC บนบอร์ดจริง (ไม่แตะโค้ดโปรเจกต์) — **SENSITIVE, blocking**
**ไฟล์:** `docs/ref/cloudflare-mesh-findings.md` (ใหม่ — บันทึกผล ไม่ใช่ผลลัพธ์โค้ด)

> **หมายเหตุ (หลังเคาะ B=B2 ใน §2.3):** ทดสอบข้อ 1-3, 6-9 ด้านล่างยังจำเป็นเสมอ
> ไม่ว่าจะเลือก E1 หรือ E3 เพราะเป็นข้อเท็จจริงของ WARP client เอง แต่ข้อ 4-5
> (netlink monitor ping-pong / dnsmasq) จะ**เกี่ยวเฉพาะกรณีเลือก E1** (มี PiGate
> เต็มรูปแบบรันอยู่บน mesh node เอง) — ถ้าเลือก E3 (mesh node ไม่มี PiGate เลย)
> ข้อ 4-5 ไม่เกี่ยวเลยเพราะ routing/dnsmasq ของ PiGate ไม่เคยอยู่บนเครื่องเดียวกับ
> WARP ตั้งแต่ต้น — ยังทดสอบทิ้งไว้เผื่อพิจารณา B1 (อยู่ร่วมเครื่องกับ Gateway
> หลัก) เป็นทางเลือกอนาคตตาม §0.4

ต้องตอบให้ครบทุกข้อ (ทำบน Pi ทดสอบ ไม่ใช่เครื่อง production):
1. แพ็กเกจ `cloudflare-warp` ติดตั้งบน Raspberry Pi OS **arm64** ได้จริงไหม
   (arm 32-bit ไม่รองรับ), เวอร์ชันไหน, unit ชื่อ `warp-svc.service` จริงไหม
2. หลัง `warp-cli connector new <token>` + connect: **ชื่อ network interface**
   ที่โผล่คืออะไร, IP ที่ได้อยู่ใน `100.96.0.0/12` ไหม
3. `ip route show` — WARP ใส่ route อะไรลง routing table บ้าง, **proto อะไร**,
   มี gateway ไหม → เอาไปเทียบกับเงื่อนไขลบ route ของ `real_routing.go:126-147`
   (ต้องพิสูจน์ว่า `ApplyRoutes` ของ PiGate **ไม่ลบ** route ของ WARP ทิ้ง)
4. เปิด PiGate (โหมด real) ค้างไว้พร้อมกัน แล้วดูว่า netlink monitor +
   routing reconcile ทำให้เกิด route ping-pong หรือไม่ (ดู log `[Routing]`)
5. **dnsmasq ยังทำงานได้ไหม** — client ใน LAN query DNS ผ่าน PiGate ได้ไหม,
   dnsmasq ยิง upstream ออกได้ไหม, `/etc/resolv.conf` ของ host ถูกแก้ไหม
6. สถานะ mesh อ่านได้แบบ **ไม่ต้อง exec** หรือไม่ (D-Bus property ของ unit,
   ไฟล์ state/log ใต้ `/var/log/cloudflare-warp/`, การมีอยู่ของ interface)
   → ผลข้อนี้ตัดสินหน้าตาของ `MeshManager.Status()` ใน Step 3
7. มีทางตั้ง token แบบ config file (ตัวเลือก A3) ไหม
8. connectivity จริงระหว่างสอง site: ping ข้าม LAN, MTU ที่ใช้ได้จริง (1280?),
   ต้อง MSS clamp ไหม, ต้องมี masquerade เพิ่มจากที่ WARP ทำเองไหม
9. `warp-svc` ต้อง start ด้วย polkit ของ user `pigate` ได้ (ทดสอบหลังเพิ่ม allowlist)

**เสร็จเมื่อ:** เอกสาร findings ตอบครบ 9 ข้อ + เจ้าของโปรเจกต์เคาะจุดตัดสินใจ
A/B/C/D ใน §2.3 แล้ว (ถ้าข้อ 3/4/5 ผลออกมาว่าชนกันจนแก้ไม่ได้ → หยุดแผนนี้แล้ว
กลับไปพิจารณาแผนสำรอง WireGuard ตาม §2.2)

### Step 1 — Model + validation (SENSITIVE)
**ไฟล์:** `backend/internal/model/cloudflare_mesh.go` (ใหม่),
`backend/internal/model/cloudflare_mesh_test.go` (ใหม่)
- `MeshSettings{Enabled, Token, LocalCIDRs []string, MTU int, UpdatedAt}`
- `MeshSite{ID, Name, CIDR, Description, Status bool}` (remote site หนึ่งแถวต่อ CIDR)
- `MeshStatus{Enabled, HasToken, TokenHint, Status, UnitLoaded, PackageInstalled,
  MeshInterface, MeshIP, IPForwarding, IPv6Forwarding, RPFilter, Warnings []string}`
- `ValidateMeshToken(token)` — ใช้กติกาเดียวกับ `ValidateTunnelToken` (ปฏิเสธ
  `\n`/`\r`/`\x00`/control char, whitelist charset, ความยาวมีขอบเขต) **แม้เลือก
  ตัวเลือก A1** ก็ยังต้อง validate เพราะ token จะถูกแสดง/เก็บ
- `ValidateSiteCIDR(cidr)` — ต้องเป็น CIDR ที่ parse ได้, ต้องเป็น network address
  (ไม่ใช่ host address), ปฏิเสธ `0.0.0.0/0`, ปฏิเสธ CIDR ที่คร่อม `100.64.0.0/10`
  (CGNAT ของ WARP) และ `100.96.0.0/12` (mesh range), เตือนถ้าไม่ใช่ RFC1918
- `OverlapsCIDR(a, b string) bool` + `FindOverlaps(target string, others []string)`
  — ฟังก์ชันบริสุทธิ์ ใช้ทั้งฝั่ง service และเทสต์
- `TokenHint()` เหมือนแผน tunnel (4 ตัวท้าย), ห้ามมีฟังก์ชันใด log token
- unit test: overlap ตรงตัว/ครอบกัน/คนละ subnet, host address, CGNAT range,
  token ที่มี newline

### Step 2 — DB migration + repository
**ไฟล์:** `backend/internal/db/connection.go` (แก้, ต่อท้ายกลุ่ม migration
~`:383-407`), `backend/internal/db/cloudflare_mesh_repo.go` (ใหม่),
`backend/internal/db/cloudflare_mesh_migration_test.go` (ใหม่)
- `cloudflare_mesh_settings` — single row `CHECK(id=1)`: `enabled`, `token`,
  `local_cidrs` (TEXT, comma-separated หรือ JSON — เลือกให้ตรงกับ pattern เดิมใน repo),
  `mtu`, `updated_at` + seed `INSERT OR IGNORE`
- `cloudflare_mesh_sites` — `id TEXT PRIMARY KEY`, `name`, `cidr`, `description`,
  `status INTEGER`, `UNIQUE(cidr)`
- Repo: `GetMeshSettings/UpdateMeshSettings` (token ว่าง = คงค่าเดิม ตาม
  `wifi_preset_repo.go:67-89`), `ClearMeshSettings`, `GetMeshSites/CreateMeshSite/
  UpdateMeshSite/DeleteMeshSite`
- migration test ตาม `timezone_migration_test.go`: DB เก่า migrate ขึ้นได้ + มีแถว id=1

### Step 3 — `MeshManager` interface
**ไฟล์:** `backend/internal/kernel/interfaces.go` (แก้, ต่อท้าย)
- `Start()/Stop()/Restart() error`, `Status() (model.ServiceRuntimeState, error)`
  (ทั้งหมด delegate ไป `dbus_systemd.go` ห้ามเขียน D-Bus ใหม่)
- `PackageInstalled() bool` (`os.Stat` ของ binary path เท่านั้น ห้าม exec)
- `MeshInterface() (name string, ip string, ok bool)` — หา iface ที่มี IP ใน
  `100.96.0.0/12` ผ่าน netlink (ยืนยันชื่อจริงจาก Step 0; อย่า hardcode ชื่อ
  ถ้าหลีกเลี่ยงได้ ให้ค้นจาก address range แทน)
- `HostForwarding() (v4 bool, v6 bool, rpFilter int, err error)` — อ่าน `/proc/sys`
  แบบ read-only
- (เฉพาะเมื่อเลือก A2/A3) `Enroll(token string) error` / `WriteToken(token) error`
  พร้อม doc comment ระบุข้อยกเว้นและเหตุผล
- doc comment: ชื่อ unit เป็นค่าคงที่ภายใน ห้ามรับจาก client, ห้าม log token

### Step 4 — `RealMeshManager` (SENSITIVE)
**ไฟล์:** `backend/internal/kernel/real_cloudflare_mesh.go` (ใหม่, `//go:build linux`)
- `const meshUnit = "warp-svc.service"` + binary path จาก Step 0
- netlink query สำหรับ `MeshInterface()` (ใช้ `vishvananda/netlink` ที่มีอยู่แล้ว)
- `HostForwarding()` อ่าน `/proc/sys/net/ipv4/ip_forward`,
  `/proc/sys/net/ipv6/conf/all/forwarding`, `/proc/sys/net/ipv4/conf/all/rp_filter`
  — **read-only เท่านั้น ห้ามเขียน** (การเขียน sysctl เป็นงานของ `install.sh`)
- ถ้าเลือก A2: `exec.Command` เฉพาะจุดนี้จุดเดียว, argv คงที่, path absolute,
  token ผ่าน `ValidateMeshToken` ซ้ำ, ห้าม log token, ห้ามผ่าน `sh -c`
  → **ต้องมี review เข้มเป็นพิเศษ + comment อ้างการตัดสินใจของเจ้าของโปรเจกต์**

### Step 5 — `MockMeshManager`
**ไฟล์:** `backend/internal/kernel/mock.go` (แก้, ต่อท้ายตาม pattern
`MockSystemServiceManager` ~`:588-606`)
- state ในหน่วยความจำล้วน (`enrolled`, `running`), `PackageInstalled()` = true,
  `MeshInterface()` คืนค่าจำลอง (เช่น `mesh0`, `100.96.0.5`),
  `HostForwarding()` คืน `true,true,2`
- **ห้ามเขียนไฟล์จริง ห้าม D-Bus จริง ห้าม exec** (dev รัน `-mock=true` บนเครื่องตัวเอง)

### Step 6 — `CloudflareMeshService` (SENSITIVE)
**ไฟล์:** `backend/internal/service/cloudflare_mesh.go` (ใหม่),
`backend/internal/service/cloudflare_mesh_test.go` (ใหม่)
- ต้องรับ `*db.Repository`, `kernel.MeshManager`, `*RoutingService` (อ่าน static
  route มาตรวจ overlap), `*InterfaceService` (อ่าน address ของ interface),
  `*EventLogService`
- `GetStatus()` — รวมสถานะ service + interface + sysctl + `TokenHint` (ห้ามคืน
  token เต็ม) + `Warnings[]` ที่คำนวณจาก: package ไม่ได้ติดตั้ง, unit ไม่ loaded,
  ip_forward=0, CIDR overlap, ไม่มี firewall policy ที่อนุญาต mesh iface,
  dnsmasq เปิดอยู่พร้อมกับ mesh (ตามผลตัดสินใจ B)
- `ValidateTopology()` — ตรวจ overlap ครบทุกคู่: local CIDR ↔ address ของ
  interface จริง ↔ static route ใน DB ↔ remote site ทุกอัน ↔ `100.64.0.0/10`
  → คืนรายการ conflict อธิบายเป็นข้อความ
- `SetToken/Enroll`, `Start/Stop/Restart` (เขียน `enabled` ลง DB ก่อนสั่ง kernel),
  `Remove()`, CRUD ของ remote site (ทุก mutation เรียก `ValidateTopology` ก่อน)
- `SuggestedPolicies()` — คืน **draft** policy rule (ยังไม่บันทึก) สำหรับ
  LAN→mesh และ mesh→LAN ตามตัวเลือก D2, `ApplySuggestedPolicies()` สร้างจริงผ่าน
  service ของ firewall ที่มีอยู่ (ไม่เขียน nftables ตรง)
- sentinel errors: `ErrMeshNotInstalled`, `ErrMeshNoToken`, `ErrMeshCIDROverlap`,
  `ErrMeshForwardingDisabled`
- `InitApplyConfig()` — enabled → Start (best-effort, error แค่ log warn ห้ามทำ boot ล้ม)
- ทุก mutation ลง `EventLogService` (category `system`, action `cloudflare-mesh-<op>`)
  โดยระบุ CIDR ที่เปลี่ยน (**ต้องมี audit trail ว่าใครเปิดทางเข้า LAN เมื่อไร**)
  และห้ามมี token ใน message
- unit test: overlap ทุกรูปแบบ, sentinel error, desired state ตอน boot

### Step 7 — Wiring
**ไฟล์:** `backend/cmd/pigate/main.go` (แก้)
- ประกาศ `meshMgr kernel.MeshManager` ในกลุ่มเดียวกับ `systemServiceMgr` (~`:139`),
  assign mock/real (~`:159`/`:190`)
- สร้าง `cloudflareMeshService` ใกล้ `systemServiceService` (~`:214`) → ส่งเข้า `api.NewServer`
- เรียก `InitApplyConfig()` **ท้ายสุด** ของ apply chain (หลัง firewall/QoS/tunnel)
  เพราะ mesh ต้องการ interface/route/firewall พร้อมก่อน

### Step 8 — API handlers (SENSITIVE)
**ไฟล์:** `backend/internal/api/cloudflare_mesh_handlers.go` (ใหม่),
`backend/internal/api/server.go` (แก้)
- GET status (ห้ามมี token เต็ม), PUT settings (token ว่าง=คงเดิม, local CIDRs, MTU),
  POST start/stop/restart, DELETE (ปิด+ล้าง), CRUD `/sites`,
  GET `/suggested-policies`, POST `/suggested-policies/apply`
- map sentinel error → 400 (overlap/ไม่มี token/ไม่ได้ติดตั้ง/forwarding ปิด),
  500 (D-Bus/kernel error); error message ห้ามสะท้อน token, ห้าม log body

### Step 9 — Routes (super_admin only ทุกเส้น รวม GET)
**ไฟล์:** `backend/internal/api/router.go` (แก้, กลุ่ม `/api/system` ~`:178-197`)
- `GET/PUT/DELETE /api/system/cloudflare-mesh`,
  `POST /api/system/cloudflare-mesh/{start,stop,restart}`,
  `GET/POST /api/system/cloudflare-mesh/sites`, `PUT/DELETE .../sites/{id}`,
  `GET /api/system/cloudflare-mesh/suggested-policies`,
  `POST /api/system/cloudflare-mesh/suggested-policies/apply`
- ทุกเส้นใช้ `superAdminRoute` (เหมือน wifi-presets `:87-96`) เพราะทั้ง credential
  และการเปิดเส้นทาง L3 ข้าม LAN

### Step 10 — install.sh: sysctl + polkit + ตรวจแพ็กเกจ (SENSITIVE)
**ไฟล์:** `install.sh` (แก้)
- บล็อก sysctl (`:479-508`): เพิ่ม `net.ipv6.conf.all.forwarding = 1` และ
  `net.ipv6.conf.all.accept_ra = 2` (ต้องยืนยันใน Step 0 ว่าจำเป็นจริงกับการใช้งาน
  ของเรา และ **ประเมินผลข้างเคียงต่อ IPv6 posture ของเครื่องก่อน** — ถ้าไม่ได้ใช้
  IPv6 อาจตั้งเป็น optional/comment ไว้)
- polkit (`:259-266`): เพิ่ม `unit === "warp-svc.service"` เข้า allowlist
  **ต้องอยู่ใน if ของ `manage-units` ก่อนบรรทัด `return NO` เท่านั้น
  ห้ามแตะ guard `subject.user != "pigate"`**
- STEP ใหม่: ตรวจว่ามีแพ็กเกจ `cloudflare-warp` หรือไม่ → ไม่มีให้ `log_warn`
  แล้วทำงานต่อ (**ห้าม `exit 1`** เพราะเป็นฟีเจอร์ optional) + เตือนถ้า
  architecture ไม่ใช่ arm64/x86_64
- ข้อความสรุปท้ายสคริปต์ + หมายเหตุว่าเครื่องที่ติดตั้งแล้วต้องรัน `install.sh` ซ้ำ

### Step 11 — Capability probe + (ทางเลือก) MSS clamp
**ไฟล์:** `backend/internal/service/system_capability.go` (แก้ `:25-30`),
`backend/internal/kernel/real_capability.go` (แก้),
`backend/internal/kernel/real_firewall.go` (แก้ เฉพาะกรณีทำ MSS clamp)
- เพิ่ม capability id `mesh` (แพ็กเกจติดตั้งแล้ว + unit loaded + ip_forward เปิด)
- MSS clamping (`tcp flags syn / tcp option maxseg size set rt mtu` หรือค่าคงที่
  ตามผล Step 0) — **ทำเฉพาะเมื่อ Step 0 พิสูจน์ว่าจำเป็น** และต้องแทรกใน forward
  chain โดย **ไม่ทำลายลำดับ 4 ส่วนของ chain** ตาม `tech_stack_design.md` §4.3
  ถ้าไม่จำเป็น ให้ตัดออกและบันทึกเหตุผลในเอกสาร

### Step 12 — Frontend API client
**ไฟล์:** `frontend/src/services/cloudflareMeshService.ts` (ใหม่)
- ตาม pattern `systemService.ts` / `staticRouteService.ts`; export type
  `MeshStatus`, `MeshSite`, `SuggestedPolicy` + ฟังก์ชันครบทุก endpoint ของ Step 9
- ห้ามเก็บ token ลง `localStorage`/`sessionStorage`

### Step 13 — Frontend: หน้าใหม่ + route + sidebar
**ไฟล์:** `frontend/src/pages/CloudflareMesh.tsx` (ใหม่, โครงตาม `DnsServer.tsx`),
`frontend/src/App.tsx` (แก้ `:193-204`),
`frontend/src/components/app-sidebar.tsx` (แก้ `:158-166`)
- โครงหน้า: (1) Card สถานะ (Badge running/stopped/failed/unavailable, mesh iface +
  mesh IP), (2) Card Prerequisites เป็น checklist (แพ็กเกจ, unit, ip_forward,
  enrollment, Split Tunnel ที่ต้องตั้งใน Cloudflare) พร้อมคำสั่ง/ลิงก์คู่มือ,
  (3) Card "This site" (local CIDRs + MTU), (4) ตาราง Remote sites (CRUD),
  (5) Card Firewall — แสดง suggested policy + ปุ่ม "สร้าง policy ที่แนะนำ"
  พร้อม AlertDialog อธิบายว่ากำลังเปิดทางให้ LAN อีกฝั่งเข้ามา,
  (6) **Alert เตือนถาวร** เรื่อง security posture + เรื่อง DNS (ตามผลตัดสินใจ B)
- แสดง conflict/overlap เป็น error inline ที่ฟอร์ม ไม่ใช่แค่ toast
- poll สถานะ ~5s ขณะอยู่หน้านี้ (clear ตอน unmount) + refetch หลังทุก action
- route: `<Route path="cloudflare-mesh" element={<SuperAdminRoute><CloudflareMesh /></SuperAdminRoute>} />`
- sidebar: `isSuperAdmin ? [{path:"/system/cloudflare-mesh", label:"Cloudflare Mesh", icon: Waypoints|Share2}] : []`
- import router จาก `"react-router"` เท่านั้น, flat design (ห้าม `shadow-*`/
  `backdrop-blur-*`), ใช้ semantic color เท่านั้น, dark/light ครบ, ห้าม
  `console.log` token, Dialog ที่มี Combobox ต้อง `modal={false}`

### Step 14 — Backup/Restore
**ไฟล์:** `backend/internal/service/backup.go` (แก้), `backend/internal/model/backup.go` (แก้)
- เพิ่ม `cloudflare_mesh_settings` + `cloudflare_mesh_sites` เข้า `BackupConfig`
  ตาม pattern `:88`/`:136`
- **ข้อควรระวังพิเศษ:** ไฟล์ backup ที่ไม่ใส่ passphrase จะมี token + แผนผัง
  network ของทุก site (ข้อมูล reconnaissance ชั้นดี) → ต้องเตือนใน UI export
- ตอน import: ต้องไม่ auto-start mesh ทันที (import config ไม่ควรเปิดเส้นทางเข้า
  LAN โดยอัตโนมัติ) — restore เป็น `enabled=false` เสมอ แล้วให้ผู้ใช้กดเปิดเอง
  (**ต่างจากแผน tunnel โดยตั้งใจ** เพราะผลกระทบกว้างกว่ามาก)

### Step 15 — เอกสาร
**ไฟล์:** `docs/openapi.yaml`, `frontend/public/openapi.yaml`, `README.md`,
`docs/ref/cloudflare-mesh-design.md` (ใหม่), `docs/tech_stack_design.md` (แก้
เฉพาะกรณีเลือก A2 — ต้องบันทึกข้อยกเว้น exec)
- เพิ่ม endpoint ทั้งหมดให้ sync กันทั้งสองไฟล์ openapi (response ห้ามมี token เต็ม)
- README Feature Status เพิ่มแถว Cloudflare Mesh + ระบุว่าเป็น Beta/optional
- design doc: สถาปัตยกรรมที่เลือก, ความต่างจาก Cloudflare Tunnel, สิ่งที่ต้องตั้งใน
  Cloudflare dashboard (พร้อม checklist), ข้อจำกัด DNS/arm64, ขั้นตอน migrate
  เครื่องที่ติดตั้งแล้ว, วิธี rollback (stop + ลบ policy + reboot)

---

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/system/cloudflare-mesh` | super_admin | สถานะ + prerequisites + warnings (ไม่มี token เต็ม) |
| PUT | `/api/system/cloudflare-mesh` | super_admin | token (ว่าง=คงเดิม), local CIDRs, MTU |
| POST | `/api/system/cloudflare-mesh/{start,stop,restart}` | super_admin | สั่ง `warp-svc` ผ่าน D-Bus |
| DELETE | `/api/system/cloudflare-mesh` | super_admin | stop + ล้างค่า (ไม่ลบ policy ให้อัตโนมัติ — แจ้งผู้ใช้แทน) |
| GET/POST | `/api/system/cloudflare-mesh/sites` | super_admin | อ่าน/เพิ่ม remote site (validate overlap) |
| PUT/DELETE | `/api/system/cloudflare-mesh/sites/{id}` | super_admin | แก้/ลบ remote site |
| GET | `/api/system/cloudflare-mesh/suggested-policies` | super_admin | draft policy สำหรับ LAN↔mesh (ยังไม่บันทึก) |
| POST | `/api/system/cloudflare-mesh/suggested-policies/apply` | super_admin | สร้าง policy จริงผ่าน firewall service |

ทุกเส้น mutation ถูก `DisableEditMiddleware` บล็อกในโหมด `-disable-edit=true` อยู่แล้ว

---

## 5. ข้อควรระวัง

1. **นี่คือฟีเจอร์ที่เปลี่ยน security posture มากที่สุดเท่าที่ PiGate เคยมี** —
   tunnel เดี่ยวเปิดทางเข้าถึงเฉพาะ service ที่ประกาศ แต่ mesh เปิด **routing
   ระดับ L3 ระหว่าง LAN สองแห่ง** ช่องโหว่/เครื่องติดมัลแวร์ที่ site หนึ่งจะไปถึง
   อีก site ได้ทันทีถ้า policy หลวม → UI ต้องมี Alert ถาวร, ต้อง log ทุก mutation
   พร้อม CIDR, และ **default ต้องเป็น deny** (ไม่ auto-apply policy ตามตัวเลือก D2)
2. **CIDR overlap คือบั๊กที่เจ็บที่สุดและเงียบที่สุด** — ถ้าสอง site ใช้
   `192.168.1.0/24` เหมือนกัน routing จะพังแบบคาดเดาไม่ได้ (traffic วิ่งไป local
   แทน remote) → ต้อง validate ครบทุกคู่ใน Step 6 และปฏิเสธการบันทึก ไม่ใช่แค่เตือน
   รวมถึงต้องกัน overlap กับ `100.64.0.0/10` (CGNAT) และ `100.96.0.0/12` (mesh)
3. **ชนกับ static route ที่มีอยู่เดิม** — ผู้ใช้อาจมี static route ไปยัง CIDR ของ
   site ปลายทางอยู่แล้ว (ผ่าน VPN/ลิงก์อื่น) → ต้องตรวจกับ `static_routes` ใน DB
   และแสดงว่าเส้นไหนจะถูก override โดย metric ใด
4. **`ApplyRoutes` ของ PiGate อาจลบ route ของ WARP** — `real_routing.go:126-147`
   ลบ route ที่ proto=120 ทิ้งเสมอ และลบ route ที่มี gateway เมื่อเปิด
   `allow-edit-system-routes` → **ต้องพิสูจน์ใน Step 0** ว่า route ของ WARP ไม่เข้า
   เงื่อนไขไหนเลย ถ้าเข้า ต้องเพิ่ม guard "ห้ามแตะ route ของ mesh interface"
   อย่างชัดเจน (และเขียนเทสต์กันการ regress)
5. **Netlink monitor + reconcile อาจเกิด ping-pong** — mesh สร้าง/ลบ route เอง →
   monitor เห็น route event → routing reconcile → อาจไปแก้ของ mesh → วนซ้ำ
   ต้องดู log `[Routing]` ตอน Step 0 และพิจารณา ignore-list ตาม interface
6. **DNS ชนกับ dnsmasq** — Cloudflare เตือนตรงๆ ว่าอย่าลง mesh node บนเครื่องที่รัน
   DNS service เพราะ WARP redirect DNS ของ host ไป Gateway → ต้องเคาะตัวเลือก B
   ใน §2.3 ก่อน และไม่ว่าเลือกทางไหน UI ต้องบอกผู้ใช้อย่างชัดเจน
7. **MTU/MSS** — encapsulation ทำให้ packet ใหญ่เกินแล้วหายเงียบ (อาการคลาสสิก:
   ping ผ่าน แต่ HTTPS/SMB ค้าง) → ต้องทดสอบด้วย packet ใหญ่ ไม่ใช่แค่ ping
8. **rp_filter** — ปัจจุบัน `install.sh:455` ตั้ง `RP_FILTER=2` (loose) ซึ่งพอดีกับ
   asymmetric routing ของ mesh **ห้ามเปลี่ยนเป็น 1** ระหว่างทำฟีเจอร์นี้
9. **รัน tunnel + mesh พร้อมกัน** — Cloudflare ระบุว่าต้องใช้ Split Tunnel กันไม่ให้
   traffic ของ `cloudflared` วิ่งเข้า mesh ไม่งั้นเกิด routing loop → เอกสาร
   design doc ต้องเขียนขั้นตอนนี้ไว้ชัด และ UI ควรเตือนเมื่อเปิดทั้งสองฟีเจอร์
10. **ห้าม exec (กฎหลักของโปรเจกต์)** — ถ้าเลือก A2 ต้องเป็นการยกเว้นที่เจ้าของ
    โปรเจกต์อนุมัติเป็นลายลักษณ์อักษรในเอกสาร, จำกัดที่ฟังก์ชันเดียว, argv คงที่,
    ไม่ผ่าน shell, validate token ซ้ำ และต้องผ่าน review เข้มเป็นพิเศษ
11. **Token ต้องไม่รั่ว/ไม่ถูก log** — เหมือนแผน tunnel: ห้ามใส่ใน argv ที่เห็นจาก
    `ps` ถ้าเลี่ยงได้, ห้าม log, ห้ามส่งกลับ client, ไฟล์ที่เกี่ยวข้องต้อง 0600
12. **Mock mode ต้องไม่มี side effect** — `MockMeshManager` ห้ามเขียนไฟล์/เรียก
    D-Bus/exec (dev รัน `-mock=true` บนเครื่องทำงานจริง)
13. **arm 32-bit ไม่รองรับ** — Pi รุ่นเก่า/OS 32-bit ใช้ฟีเจอร์นี้ไม่ได้ ต้องตรวจ
    architecture แล้วแสดงผลให้ชัด ไม่ใช่ปล่อยให้ start ล้มแบบไม่มีคำอธิบาย
14. **เครื่องที่ติดตั้งไปแล้วต้องรัน `install.sh` ซ้ำ** (polkit + sysctl ใหม่)
    ไม่งั้น start จะล้มด้วย polkit denied — ต้องเขียนใน release note และ UI ต้อง
    แสดง "unavailable" อย่างชัดเจนแทน error ดิบ
15. **ทดสอบตอนเข้าถึงเครื่องได้ทางกายภาพเท่านั้น** — ฟีเจอร์นี้แตะ routing table
    ของเครื่องจริง มีโอกาสตัดการเชื่อมต่อของตัวเอง (ต่างจากแผน tunnel ที่ความเสี่ยงต่ำ)

---

## 6. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance)

ทดสอบ **ครั้งเดียวหลังทำครบทุก Step** (ไม่ต้องทดสอบทีละ Step):

**A. ทดสอบใน mock mode (`-mock=true`) — ครอบคลุมได้ทั้งหมดโดยไม่แตะ OS**
- [ ] CRUD remote site ครบ flow + validation error แสดงถูกจุด
- [ ] CIDR overlap ถูกปฏิเสธทุกกรณี (ตรงตัว / ครอบกัน / ชน LAN ตัวเอง / ชน static
      route ใน DB / ชน `100.64.0.0/10`)
- [ ] token ที่มี newline/control char/charset ผิด ถูกปฏิเสธที่ทั้ง model และ kernel
- [ ] GET status ไม่มี token เต็มในทุก response (ตรวจด้วย DevTools/curl)
- [ ] role ที่ไม่ใช่ super_admin ได้ 403 ทุกเส้น (รวม GET)
- [ ] `-disable-edit=true` บล็อกทุก mutation
- [ ] ปุ่ม "สร้าง policy ที่แนะนำ" สร้าง policy ที่เห็นได้จริงในหน้า Firewall Policy
      และ **ไม่ถูกสร้างเองโดยไม่กด**
- [ ] backup/restore: export/import แล้วค่ากลับมาครบ และ mesh กลับมาเป็น
      `enabled=false` เสมอ
- [ ] ไม่มี side effect ต่อ OS (ไม่มีไฟล์ใหม่/ไม่มี D-Bus call/ไม่มี process ใหม่)

**B. ทดสอบบนบอร์ดจริง 2 เครื่อง (ต้องเข้าถึงเครื่องได้ทางกายภาพ)**
- [ ] `install.sh` รอบใหม่: polkit ยอมให้ `pigate` คุม `warp-svc.service` ได้ และ
      **ไม่ทำ polkit ของ user อื่นพัง** (ทดสอบ `systemctl`/mount ของ user ปกติ)
- [ ] start/stop/restart จาก UI มีผลจริง สถานะตรงกับ `systemctl status`
- [ ] host ใน LAN ของ site A เข้าถึง host ใน LAN ของ site B ได้ตาม policy
      และ **ถูก drop เมื่อยังไม่มี policy** (พิสูจน์ว่า default deny ทำงาน)
- [ ] traffic ที่ผ่าน/ถูก drop ปรากฏใน Forward Traffic log ตามปกติ
- [ ] ทดสอบ payload ใหญ่ (HTTPS/SCP/SMB) ไม่ค้าง (MTU/MSS ถูกต้อง)
- [ ] routing table ไม่เกิด ping-pong: ปล่อยไว้ 30 นาที + toggle link แล้ว route ของ
      mesh ยังอยู่ครบ (ตรวจ log `[Routing]`)
- [ ] DNS Server (dnsmasq) และ DHCP ยังทำงานปกติ (หรือพฤติกรรมตรงกับตัวเลือก B ที่เลือก)
- [ ] reboot แล้ว desired state กลับมาตรงเดิม (`InitApplyConfig`)
- [ ] stop mesh แล้ว routing/DNS/firewall ของเครื่องกลับสู่สภาพเดิม (rollback ได้)
- [ ] Event log บันทึกครบทุก mutation พร้อม CIDR และ **ไม่มี token**

**C. ทั่วไป**
- [ ] `go build ./...` + `go test ./...` ผ่าน
- [ ] `yarn build` + `yarn lint` ผ่าน
- [ ] grep ยืนยันไม่มี log/echo ของ token ในทุกไฟล์ที่แก้/สร้าง
- [ ] grep ยืนยันไม่มี `exec.Command` ใหม่ (หรือมีเฉพาะจุดที่เจ้าของอนุมัติตาม A2
      พร้อม comment อ้างอิงการตัดสินใจ)

---

## 7. Checklist สรุป (Definition of Done)

- [ ] **Step 0**: spike บนบอร์ดจริง + `docs/ref/cloudflare-mesh-findings.md` ตอบครบ 9 ข้อ
- [x] **B (deployment topology)**: เคาะแล้ว 2026-08-08 — mesh node แยกอุปกรณ์จาก
      Gateway หลักเป็น baseline (ดู §0.4, §2.3-B)
- [ ] **จุดตัดสินใจ A/C/D/E ใน §2.3 ยังเหลือให้เจ้าของโปรเจกต์เคาะก่อนเริ่ม Step 1**
      (E สำคัญที่สุด — กำหนดว่าแผนทั้งฉบับยังเป็น subsystem เต็มรูปแบบ (E1) หรือ
      ย่อเหลือแค่ "จัดการ remote CIDR + policy ที่ Gateway หลัก" (E3))
- [ ] Step 1: `model/cloudflare_mesh.go` + test (token + CIDR + overlap)
- [ ] Step 2: DB migration (`cloudflare_mesh_settings`, `cloudflare_mesh_sites`) + repo + migration test
- [ ] Step 3: `MeshManager` interface ใน `kernel/interfaces.go`
- [ ] Step 4: `RealMeshManager` (`kernel/real_cloudflare_mesh.go`)
- [ ] Step 5: `MockMeshManager` ใน `kernel/mock.go` (ไม่มี side effect)
- [ ] Step 6: `service/cloudflare_mesh.go` + test (topology validation, sentinel errors, event log)
- [ ] Step 7: wiring ใน `cmd/pigate/main.go`
- [ ] Step 8: API handlers `api/cloudflare_mesh_handlers.go`
- [ ] Step 9: routes ใน `api/router.go` (`superAdminRoute` ทุกเส้น)
- [ ] Step 10: `install.sh` (sysctl IPv6 forwarding, polkit `warp-svc.service`, ตรวจแพ็กเกจ/arch)
- [ ] Step 11: capability probe `mesh` (+ MSS clamp เฉพาะถ้า Step 0 บอกว่าจำเป็น)
- [ ] Step 12: `frontend/src/services/cloudflareMeshService.ts`
- [ ] Step 13: `frontend/src/pages/CloudflareMesh.tsx` + route + sidebar
- [ ] Step 14: backup/restore (restore เป็น disabled เสมอ)
- [ ] Step 15: openapi (2 ไฟล์) + README + `docs/ref/cloudflare-mesh-design.md`
- [ ] เกณฑ์ทดสอบรวมท้ายแผน §6 ผ่านครบทั้ง A / B / C
