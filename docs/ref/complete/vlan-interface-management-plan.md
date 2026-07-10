# VLAN Interface Management — สร้าง/ลบ/จัดการ VLAN จากหน้า Interfaces

> แผนงานสำหรับ issue #20: ปัจจุบันหน้า Interfaces จัดการได้เฉพาะ interface ที่มีอยู่แล้วใน kernel
> หากต้องการ VLAN ต้องไปพิมพ์ `ip link add ...` เองนอกระบบ และ VLAN ที่สร้างจะหายเมื่อ reboot
> แผนนี้เพิ่มการสร้าง/ลบ VLAN (802.1Q) ผ่าน UI ด้วย Netlink และทำให้ VLAN ถูกสร้างคืนอัตโนมัติตอน boot
>
> เขียนเมื่อ: 2026-07-10 · Reference branch: `main` (จะทำงานบน `feat/vlan-interface-management`)
> Issue: https://github.com/saprayworld/pigate/issues/20

## 0. Goal and Scope

**Goal (พฤติกรรมที่ผู้ใช้เห็น):**
- หน้า Interfaces มีปุ่ม "Create VLAN" → dialog เลือก parent interface (ethernet), กรอก VLAN ID (1–4094),
  alias, role, addressing mode → กด Create แล้ว VLAN (`eth0.100`) ปรากฏเป็นการ์ดใหม่ทันที
- VLAN ที่สร้างผ่าน pigate จัดการต่อได้เหมือน interface ปกติ (edit IP/role/adminAccess, toggle up/down)
  และมีปุ่มลบ (ลบทั้ง kernel link และ config ใน DB)
- Reboot แล้ว VLAN ถูกสร้างคืนจาก DB พร้อม config เดิม (แก้ปัญหาหลักของ issue)
- Backup/Restore ครอบคลุม VLAN (restore บนเครื่องใหม่แล้ว VLAN ถูกสร้างขึ้นได้เอง)

**เงื่อนไขทางเทคนิค:** สร้าง/ลบ link ผ่าน `vishvananda/netlink` เท่านั้น (ห้าม `exec.Command`),
implement ทั้ง real และ mock, ชื่อ VLAN ตายตัวเป็น `<parent>.<id>`

**Out of scope:**
- QinQ / nested VLAN (VLAN บน VLAN), VLAN protocol 802.1ad
- VLAN บน wireless interface (wpa client mode ไม่รองรับ tagged frame ที่เชื่อถือได้ — ปฏิเสธตั้งแต่ validation)
- Bridge / bonding / interface เสมือนชนิดอื่น (ค่อยทำแผนแยกถ้าต้องการ)
- การแก้ไข VLAN ID / parent หลังสร้าง (ให้ลบแล้วสร้างใหม่แทน)
- ตรวจ/เตือน config ค้าง (DHCP server, QoS, firewall rule) ที่อ้างถึง VLAN ที่ถูกลบ — บันทึกไว้ใน Cautions เท่านั้น

## 1. Current State (สำรวจโค้ดจริง ณ 2026-07-10)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Model: `Subtype` field มีอยู่แล้ว (ค่า "vlan" ได้) แต่ไม่มี vlanParent/vlanId | บางส่วน | `backend/internal/model/types.go:134` |
| Kernel: ตรวจจับ subtype vlan ได้แล้วผ่าน `link.Type()` | เสร็จ | `backend/internal/db/repository.go:1637` (`GetDeviceType`) |
| Kernel: `NetworkManager` ไม่มี method สร้าง/ลบ link | ขาด | `backend/internal/kernel/interfaces.go:34-43` |
| Service: `GetKernelInterfaces` แสดง VLAN ที่มีอยู่แล้วได้ (unmanaged) | เสร็จ | `backend/internal/service/interface.go:214-288` |
| Service: boot-apply **ข้าม** DB row ที่ไม่มีใน kernel → VLAN ไม่ถูกสร้างคืน | ขาด (จุดสำคัญ) | `backend/internal/service/interface.go:46-49` |
| DB: ตาราง `network_interfaces` ไม่มีคอลัมน์ vlan_parent/vlan_id | ขาด | `backend/internal/db/connection.go:336-368` |
| DB: มี pattern migration แบบ `ALTER TABLE ADD COLUMN` ให้ลอกแล้ว | เสร็จ | `backend/internal/db/connection.go:157-192` (subtype/metric) |
| API: มี GET/PUT/PATCH/toggle/reset/DELETE แต่ไม่มี endpoint สร้าง interface | ขาด | `backend/internal/api/router.go:56-63` |
| API: `HandleDeleteInterface` ลบได้เฉพาะ status "offline" และลบแค่ DB row | ต้องแก้ | `backend/internal/api/handlers.go:809-828` |
| Backup: `resolveInterfaces` **ทิ้ง** interface ใน backup ที่ไม่มีบนเครื่อง | ต้องแก้ | `backend/internal/service/backup.go:212-219,276` |
| Frontend: icon + subtype badge ของ vlan มีแล้ว แต่ไม่มี UI สร้าง/ลบ | บางส่วน | `frontend/src/pages/Interfaces.tsx:139-140` |
| Frontend: `interfaceService` ไม่มี `createVlan` | ขาด | `frontend/src/services/interfaceService.ts:46-260` |
| Deps: `vishvananda/netlink v1.3.1` มี `netlink.Vlan` อยู่แล้ว | เสร็จ | `backend/go.mod:11` |
| install.sh: `cap_net_admin` ครอบคลุม RTM_NEWLINK/DELLINK อยู่แล้ว | ไม่ต้องแก้ | — |

**สรุป:** โครงแสดงผล (subtype, icon, unmanaged badge) มีครบแล้ว งานจริงกระจุกอยู่ที่
(1) kernel method สร้าง/ลบ VLAN, (2) จุด recreate ตอน boot ใน `InitApplyConfigurationAtStartup`,
(3) endpoint POST ใหม่ + แก้เงื่อนไข DELETE, (4) dialog สร้าง VLAN ใน frontend

## 2. Technical Approach

ใช้ `netlink.Vlan` จาก `vishvananda/netlink` (มีใน go.sum แล้ว, ไม่เพิ่ม dependency):

```go
parent, _ := netlink.LinkByName("eth0")
vlan := &netlink.Vlan{
    LinkAttrs:    netlink.LinkAttrs{Name: "eth0.100", ParentIndex: parent.Attrs().Index},
    VlanId:       100,
    VlanProtocol: netlink.VLAN_PROTOCOL_8021Q,
}
err := netlink.LinkAdd(vlan)   // ลบ: netlink.LinkDel(vlan)
```

- **ทำไมเลือกทางนี้:** เทียบเท่า `ip link add link eth0 name eth0.100 type vlan id 100` แต่เป็น
  Netlink syscall ตรง ไม่มี shell → สอดคล้อง constraint หลักของโปรเจกต์ และใช้ capability
  `cap_net_admin` ที่ binary มีอยู่แล้ว ไม่ต้องแตะ install.sh/Polkit
- **ทางเลือกที่ตัดทิ้ง:**
  - เขียนไฟล์ config ให้ dhcpcd/systemd-networkd สร้าง VLAN ตอน boot — ตัดเพราะผูกกับ network
    stack ภายนอก, pigate มี pattern "DB คือ source of truth + apply ตอน boot" อยู่แล้ว
    (`InitApplyConfigurationAtStartup`) ใช้ pattern เดิมสม่ำเสมอกว่า
  - `exec.Command("ip", ...)` — ขัด constraint no-shell โดยตรง
- **ชื่อ VLAN บังคับเป็น `<parent>.<vlanId>`** (เช่น `eth0.100`) — เป็น convention มาตรฐาน Linux,
  ทำให้ตรวจซ้ำง่าย และ `GetDeviceType` ตรวจ subtype ได้เองโดยไม่ต้องเก็บ state เพิ่ม
- **Template ในโค้ดเดิมที่ลอก:** การเพิ่ม method ใน `NetworkManager` ทั้ง real+mock ตามสไตล์
  `ToggleInterface` (`real_network.go:38`, `mock.go:67`), migration ตาม pattern คอลัมน์ `metric`
  (`connection.go:181-192`), handler สร้าง object ตามสไตล์ `HandleCreatePolicy` (`handlers.go:867`)

**การคงอยู่หลัง reboot:** เพิ่มขั้น "recreate missing VLANs" ใน `InitApplyConfigurationAtStartup`
ก่อน loop เดิม — อ่าน DB row ที่ `subtype == "vlan"` และไม่มีใน kernel → `CreateVlan(parent, id)`
แล้วค่อยเข้า loop apply config ตามปกติ วิธีนี้ได้ Backup/Restore ฟรีด้วย เพราะ
`BackupService.reapply()` (`backup.go:402`) เรียกฟังก์ชันเดียวกันเป็น step "interfaces"

## 3. Steps (เรียงจากชั้นในสุดออกนอก)

### Step 1 — Model: เพิ่ม field VLAN
**File:** `backend/internal/model/types.go` (~บรรทัด 134, ใน `NetworkInterface`)
- เพิ่ม `VlanParent *string \`json:"vlanParent,omitempty"\`` และ `VlanID *int \`json:"vlanId,omitempty"\``
- เพิ่ม struct input ใหม่ `CreateVlanInput { Parent string; VlanID int; Alias, Role, AddressingMode, IP, Netmask, Gateway string; AdminAccess []string }`

### Step 2 — DB: migration + CRUD
**File:** `backend/internal/db/connection.go` (~บรรทัด 181 ต่อจาก migration `metric`)
- เพิ่ม `ALTER TABLE network_interfaces ADD COLUMN vlan_parent TEXT` และ `ADD COLUMN vlan_id INTEGER`
  ตาม pattern เดิม (ตรวจจาก `sqlite_master` ก่อน) และเพิ่ม 2 คอลัมน์นี้ใน `CREATE TABLE IF NOT EXISTS` (~บรรทัด 336)

**File:** `backend/internal/db/repository.go`
- `GetInterfacesFromDB` (~1650): เพิ่ม 2 คอลัมน์ใน SELECT + Scan
- `UpdateInterface` (~1727): เพิ่ม 2 คอลัมน์ใน branch INSERT (~1757) — branch UPDATE ไม่ต้องใส่
  เพราะ vlan_parent/vlan_id เป็น immutable หลังสร้าง (ระบุเหตุผลนี้เป็น comment)

### Step 3 — Kernel interface: เพิ่ม method
**File:** `backend/internal/kernel/interfaces.go` (~บรรทัด 34, ใน `NetworkManager`)
```go
// CreateVlan creates an 802.1Q VLAN sub-interface named "<parent>.<vlanID>".
CreateVlan(parent string, vlanID int) error
// DeleteVlan removes a VLAN link previously created on this host. It must
// refuse to delete a link whose type is not "vlan".
DeleteVlan(name string) error
```

### Step 4 — Real implementation
**File:** `backend/internal/kernel/real_network.go` (ต่อท้าย `ConfigureInterface`, ~บรรทัด 290)
- `CreateVlan`: `LinkByName(parent)` → สร้าง `netlink.Vlan` ตามโค้ดใน §2 → `LinkAdd`
  แปล error `EEXIST` เป็นข้อความอ่านง่าย ("vlan already exists")
- `DeleteVlan`: `LinkByName(name)` → **ตรวจ `link.Type() == "vlan"` ก่อนเสมอ** แล้วค่อย `LinkDel`
  (กันการยิง DELETE มาลบ eth0/wlan0 ทิ้ง — นี่คือ guard ชั้น kernel ไม่พึ่ง handler อย่างเดียว)

### Step 5 — Mock implementation
**File:** `backend/internal/kernel/mock.go` (~บรรทัด 67, ใน `MockNetwork`)
- ทั้งสอง method เป็น log-only no-op (ห้ามแตะ OS) — VLAN ที่สร้างใน mock mode จะโผล่ใน UI เอง
  เพราะ `GetKernelInterfaces` mock branch ผนวก DB row ที่ไม่อยู่ใน list อยู่แล้ว (`service/interface.go:197-209`)

### Step 6 — Service layer
**File:** `backend/internal/service/interface.go`
- `CreateVlanInterface(input model.CreateVlanInput) (*model.NetworkInterface, error)` (ฟังก์ชันใหม่):
  1. validate: vlanID 1–4094; parent ต้องอยู่ใน kernel, `Type == "ethernet"` และ `Subtype != "vlan"`;
     ชื่อ `parent.id` ต้องไม่ซ้ำทั้งใน kernel และ DB (ซ้ำ → error ให้ handler ตอบ 409)
  2. `s.network.CreateVlan(parent, vlanID)` → `s.network.ToggleInterface(name, true)`
  3. บันทึก DB ผ่าน `s.repo.UpdateInterface` (upsert-insert) โดยตั้ง `Subtype: "vlan"`,
     `Type: "ethernet"`, VlanParent/VlanID, status "up" แล้วเรียก `ApplyInterfaceConfig` เพื่อ apply IP/mode
- `DeleteVlanInterface(id string) error` (ฟังก์ชันใหม่): โหลด iface, ตรวจ `Subtype == "vlan"`,
  `s.network.DeleteVlan(name)` → `s.repo.DeleteInterface(id)`
- `InitApplyConfigurationAtStartup` (~บรรทัด 44 ก่อน loop): เพิ่ม block recreate —
  ```go
  for _, iface := range ifaces {
      if iface.Subtype == "vlan" && !kernelMap[iface.Name] && iface.VlanParent != nil && iface.VlanID != nil {
          if err := s.network.CreateVlan(*iface.VlanParent, *iface.VlanID); err != nil { /* log warn, skip */ }
          else { kernelMap[iface.Name] = true }
      }
  }
  ```
  (parent ไม่อยู่ → log warning แล้วข้าม เหมือน pattern เดิมบรรทัด 47)

### Step 7 — Backup/Restore
**File:** `backend/internal/service/backup.go` (`resolveInterfaces` ~บรรทัด 276)
- แถวที่ `Subtype == "vlan"` ให้**คงไว้**แม้ไม่มีชื่อนั้นบนเครื่อง (แทนที่จะ skip+warn)
  เพราะ `reapply()` → `InitApplyConfigurationAtStartup` จะสร้าง link ให้เองจาก Step 6
  แต่ยังต้องตรวจว่า **parent** ของมันมีอยู่บนเครื่อง ไม่มีก็ skip+warn ตามเดิม

### Step 8 — API handler + route
**File:** `backend/internal/api/handlers.go`
- `HandleCreateVlan` (ใหม่, วางใกล้ `HandleUpdateInterface` ~บรรทัด 482): decode `CreateVlanInput`
  → `interfaceService.CreateVlanInterface` → เรียก `s.syncFirewallRules()` (interface ใหม่มี
  adminAccess ต้องเข้า nftables เหมือน flow ที่บรรทัด 587-592) → `s.logEvent(...)` → 201 + JSON
- `HandleDeleteInterface` (~บรรทัด 809): เพิ่ม branch — ถ้า `iface.Subtype == "vlan"` เรียก
  `DeleteVlanInterface` ได้เลย (ไม่ติดเงื่อนไข "offline" เพราะ VLAN ลบตอน up ได้และเป็นทางเดียว
  ที่จะเอา link ออก) แล้ว `syncFirewallRules()` + logEvent; ถ้าไม่ใช่ vlan → พฤติกรรมเดิมทุกอย่าง

**File:** `backend/internal/api/router.go` (~บรรทัด 56)
- `authRoute("POST /api/interfaces/vlan", s.HandleCreateVlan)` — ใช้ `authRoute` เพราะ
  `RoleReadOnlyMiddleware` บล็อก POST จาก role viewer อยู่แล้ว และ `DisableEditMiddleware`
  บล็อกใน `-disable-edit` mode อัตโนมัติ (สอดคล้อง PUT/DELETE เดิมของ interfaces)

### Step 9 — OpenAPI (สองไฟล์ต้อง sync กัน)
**Files:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` (~ใกล้ path `/interfaces/{id}` :479)
- เพิ่ม `POST /interfaces/vlan` (request/response schema + 400/403/409) และเพิ่ม field
  `vlanParent`, `vlanId` ใน schema NetworkInterface; อัปเดตคำอธิบาย DELETE ว่า vlan ลบได้ตอน up

### Step 10 — Frontend service + types
**File:** `frontend/src/data-mockup/mockData.ts` (~บรรทัด 251): เพิ่ม `vlanParent?: string; vlanId?: number`
**File:** `frontend/src/services/interfaceService.ts`: เพิ่ม `createVlan(input)` — mock branch สร้าง
object ลง localStorage (id `iface-<parent>.<id>`, subtype "vlan"), real branch `POST /interfaces/vlan`

### Step 11 — Frontend UI
**File:** `frontend/src/pages/Interfaces.tsx`
- ปุ่ม "Create VLAN" ที่ header (ใช้ shadcn `Button` + `Dialog` เดิมของหน้า; ใน dialog ใช้
  `Select` เลือก parent — เป็น Select ธรรมดาไม่ใช่ Combobox จึง**ไม่ต้อง** `modal={false}`)
- ฟอร์ม: parent (เฉพาะ ethernet ที่ subtype ไม่ใช่ vlan), VLAN ID (1–4094), alias, role,
  addressing mode + IP/netmask/gateway (reuse ชิ้นฟอร์ม static เดิม), adminAccess
- การ์ด interface ที่ `subtype === "vlan"` และ `managed === true`: เพิ่มปุ่ม Delete (confirm ผ่าน
  `useAlert().confirm` เดิม) → `interfaceService.delete(id)`
- สีทั้งหมดใช้ semantic variables, ไม่มี shadow/blur ตาม `docs/rules_of_work.md`

> **สิ่งที่ไม่ต้องทำ:** ไม่แตะ `main.go` (ใช้ startup step ของ interfaces เดิม), ไม่แตะ `install.sh`
> (cap_net_admin พอ), ไม่เพิ่ม dependency, ไม่ต้องมี kernel method "list VLANs"
> (`GetKernelInterfaces` + `GetDeviceType` เห็นอยู่แล้ว)

## 4. Related API

| Method | Path | Role | พฤติกรรม | สถานะ |
|---|---|---|---|---|
| POST | `/api/interfaces/vlan` | super_admin (authRoute + RoleReadOnly บล็อก viewer) | สร้าง VLAN link + DB row, sync firewall, 201 | **ใหม่** |
| DELETE | `/api/interfaces/{id}` | super_admin | เดิม: ลบ DB row เฉพาะ offline; เพิ่ม: subtype vlan → ลบ kernel link + DB row ได้ตอน up | แก้ไข |
| GET | `/api/interfaces` | ทุก role | ได้ field `vlanParent`/`vlanId` เพิ่ม (additive, ไม่ breaking) | แก้ไข |
| PUT/PATCH/toggle/reset | `/api/interfaces/{id}` | super_admin | ใช้กับ VLAN ได้เหมือน interface ปกติ ไม่แก้โค้ด | เดิม |

`-disable-edit=true`: POST/DELETE ถูก `DisableEditMiddleware` บล็อกอัตโนมัติ — ถูกต้องตามที่ควรเป็น

## 5. Cautions

1. **Boot-apply ข้าม row ที่ไม่มีใน kernel** (`service/interface.go:46-49`) — ถ้าลืม Step 6 block
   recreate, VLAN จะบันทึกลง DB ได้แต่หายทุกครั้งที่ reboot = ไม่แก้ปัญหาของ issue เลย
   ป้องกัน: เขียน test ว่า `InitApplyConfigurationAtStartup` เรียก `CreateVlan` เมื่อ row vlan ไม่อยู่ใน kernel
2. **Restore ทิ้ง VLAN เงียบ ๆ** — `resolveInterfaces` (`backup.go:276`) skip interface ที่ไม่มีบน
   เครื่อง ถ้าไม่ exempt subtype vlan, restore บนบอร์ดใหม่จะได้แค่ warning และ VLAN หาย
   ป้องกัน: Step 7 + test restore ที่มี vlan row
3. **DELETE เดิมกันไว้เฉพาะ offline** (`handlers.go:817-820`) — ถ้า reuse โดยไม่แยก branch จะลบ
   VLAN ที่ up ไม่ได้เลย; กลับกัน ถ้าแยก branch แล้วไม่ตรวจ `Subtype == "vlan"` ทั้งใน handler
   และใน `DeleteVlan` (kernel) จะเปิดทางลบ interface กายภาพ → หลุดการเชื่อมต่อทั้งเครื่อง
   ป้องกัน: ตรวจสองชั้น (service ตรวจ subtype จาก DB/kernel, `DeleteVlan` ตรวจ `link.Type()`)
4. **ลบ VLAN ที่ตัวเองใช้เชื่อมต่ออยู่ = lock ตัวเองออก** — เหมือน toggle down interface ที่ใช้งาน
   ป้องกัน: frontend ใส่ confirm dialog ข้อความเตือนชัดเจน; ทดสอบบนบอร์ดจริงเฉพาะเมื่อเข้าถึง
   เครื่องทางกายภาพได้ และทดสอบผ่าน interface อื่นที่ไม่ใช่ VLAN ที่กำลังลบ
5. **Parent down/ถูกถอด** — kernel จะพา VLAN down ตาม (และลบ link ถ้า parent หายไปจริง เช่น USB NIC)
   `GetDataLayerInterface` วนจาก kernel list เท่านั้น → row ใน DB จะไม่แสดงใน UI จนกว่า parent กลับมา
   (boot ครั้งถัดไปหรือ restart pigate จะสร้างคืน) ยอมรับพฤติกรรมนี้ได้ แต่ต้อง log warning ตอน
   recreate ไม่สำเร็จ ไม่ใช่ fail ทั้ง startup
6. **ชื่อ VLAN มีจุด (`eth0.100`)** — validation ของ `ConfigureWifi` (`real_network.go:107-114`)
   ปฏิเสธ "." แต่ไม่กระทบเพราะ VLAN เป็น ethernet เท่านั้น; unit `dhcpcd@eth0.100.service` เป็นชื่อ
   ที่ systemd ยอมรับ; ห้ามผ่อน validation ของ wifi เพื่อ VLAN เด็ดขาด
7. **Netlink monitor / dhcpcd** — `LinkAdd` จะยิง LinkUpdate event ให้ `netlink_monitor.go:117`
   → `dhcpcdService.HandleLinkUpdate` จัดการ start dhcpcd ให้เองถ้า mode dhcp ไม่ต้องเขียนเพิ่ม
   แต่ต้องทดสอบ flow "สร้าง VLAN แบบ dhcp" ว่า dhcpcd ติดจริง
8. **VLAN ID ต้อง 1–4094 และ input เป็น int เท่านั้น** — ค่า id/parent ประกอบเป็นชื่อ link
   (ไม่มี shell จึงไม่มี injection) แต่ parent ต้อง validate ว่ามีจริงใน kernel ก่อนเสมอ
   เพื่อกัน DB row ขยะที่สร้าง link ไม่ได้
9. **config ค้างหลังลบ VLAN** — DHCP server/QoS/firewall rule ที่อ้าง `eth0.100` จะยังอยู่ใน DB
   (out of scope ที่จะ cascade) → พฤติกรรมเดียวกับการถอด USB NIC ปัจจุบัน บันทึกไว้เฉย ๆ
   ไม่ทำอะไรเพิ่มในรอบนี้
10. **ทดสอบ mock ก่อนเสมอ** — `-mock=true` ครอบ flow สร้าง/แสดง/ลบใน UI ได้ครบ (ผ่าน DB-append
    ใน mock branch); สิ่งที่ต้องทดสอบบนบอร์ดจริงเท่านั้น: LinkAdd/LinkDel จริง, dhcpcd บน VLAN,
    tagged traffic ผ่าน switch ที่ตั้ง trunk แล้ว

## 6. Summary Checklist (Definition of Done)

- [ ] `model/types.go` — เพิ่ม VlanParent/VlanID + `CreateVlanInput`
- [ ] `db/connection.go` — migration `vlan_parent`/`vlan_id` + CREATE TABLE ใหม่
- [ ] `db/repository.go` — SELECT/Scan/INSERT ครอบสองคอลัมน์ใหม่
- [ ] `kernel/interfaces.go` — เพิ่ม `CreateVlan`/`DeleteVlan` ใน `NetworkManager`
- [ ] `kernel/real_network.go` — implement จริงด้วย `netlink.Vlan` + guard `link.Type()=="vlan"` ตอนลบ
- [ ] `kernel/mock.go` — implement mock (log-only, ไม่มี side effect)
- [ ] `service/interface.go` — `CreateVlanInterface`, `DeleteVlanInterface`, recreate block ใน startup
- [ ] `service/backup.go` — exempt vlan row ใน `resolveInterfaces` (ตรวจ parent แทน)
- [ ] `api/handlers.go` — `HandleCreateVlan` + branch vlan ใน `HandleDeleteInterface` (+syncFirewallRules, logEvent)
- [ ] `api/router.go` — `POST /api/interfaces/vlan`
- [ ] `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` — sync ทั้งสองไฟล์
- [ ] `frontend/src/data-mockup/mockData.ts` — type + mock create รองรับ vlan
- [ ] `frontend/src/services/interfaceService.ts` — `createVlan` (mock + real)
- [ ] `frontend/src/pages/Interfaces.tsx` — Create VLAN dialog + Delete บนการ์ด vlan
- [ ] Test: `service/interface_test.go` — สร้าง/ลบ/startup-recreate/validation (id ผิด, parent เป็น wireless, ชื่อซ้ำ)
- [ ] Test: `api/handlers_test.go` — POST 201/400/409, DELETE vlan ตอน up สำเร็จ, DELETE interface กายภาพยังติด offline-guard, viewer โดน 403
- [ ] Test: backup export→restore ที่มี vlan row ไม่โดน drop
- [ ] `go build ./...` + `go test ./...` ผ่าน; `yarn build` + `yarn lint` ผ่าน
- [ ] ทดสอบ mock mode: สร้าง → การ์ดโผล่ → edit → ลบ → refresh แล้วสถานะถูกต้อง
- [ ] ทดสอบบอร์ดจริง (มี physical access เท่านั้น): สร้าง `eth0.100` → `ip -d link show eth0.100` เห็น vlan id → reboot → VLAN กลับมาพร้อม config → ลบแล้ว link หายจริง
- [ ] อัปเดต README Feature Status (แถว Interfaces เพิ่มหมายเหตุ VLAN) + คอมเมนต์สรุปแนวทางลง issue #20
