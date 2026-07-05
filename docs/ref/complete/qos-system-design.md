# QoS Module Design — PiGate

เอกสารนี้สรุปแนวทางการออกแบบและพัฒนาระบบ QoS (Quality of Service) ตาม Interface
สำหรับโปรเจค PiGate (Raspberry Pi Gateway Controller)

---

## 1. เครื่องมือที่ใช้

- **Netlink เท่านั้น** ผ่าน `vishvananda/netlink` ที่มีอยู่แล้วใน `go.mod` — ไม่ต้องเพิ่ม dependency ใหม่
- **ไม่ใช้ D-Bus** เพราะ NetworkManager ไม่มี QoS API
- ตรงกับ Tech Stack ของโปรเจคที่หลีกเลี่ยง Shell Command Injection อยู่แล้ว

---

## 2. การจำกัด Download vs Upload

มุมมองจาก Router (PiGate) ที่ต้องเข้าใจก่อน:

```
[ Client LAN ] ←────────────────────────→ [ Internet / WAN ]
                    eth0 (LAN-facing)

Client Download  =  Router ส่งออก eth0  →  EGRESS ของ eth0
Client Upload    =  Router รับเข้า eth0  →  INGRESS ของ eth0
```

| ทิศทาง | ทำได้? | วิธี |
|--------|--------|------|
| **Egress (Client Download)** | ✅ ตรงไปตรงมา | HTB Qdisc บน eth0 ได้เลย |
| **Ingress (Client Upload)** | ⚠️ ทำได้ แต่ต้องใช้ IFB | Redirect ingress → IFB device → HTB บน IFB |

### การตั้งค่า Upload/Download แยกกัน

ตั้งได้อิสระ โดย `0` = ไม่จำกัด:

| ต้องการ | EgressRateMbps (Download) | IngressRateMbps (Upload) |
|---------|--------------------------|--------------------------|
| Download 100 / Upload 50 | `100` | `50` |
| Download 50 / Upload ไม่จำกัด | `50` | `0` |
| Download ไม่จำกัด / Upload 20 | `0` | `20` |
| จำกัดทั้งคู่เท่ากัน 50 | `50` | `50` |

---

## 3. IFB Virtual Device

IFB (Intermediate Functional Block) คือ **virtual network interface จริงๆ** ที่ Kernel สร้างขึ้น
เพื่อใช้เป็นทางผ่านสำหรับ shape ingress traffic

```bash
$ ip link show
1: lo: <LOOPBACK,...>
2: eth0: <BROADCAST,...>
3: wlan0: <BROADCAST,...>
4: ifb0: <BROADCAST,...>   ← โผล่มาเมื่อ modprobe ifb
5: ifb1: <BROADCAST,...>
```

### การจัดการ IFB ในหน้า Interface

เนื่องจาก `ifb0` จะโผล่ในรายการ Interface ของระบบ ต้องจัดการดังนี้:

- **Filter `ifb*` ออกจากหน้า Interface** — ไม่แสดงให้ User งง และป้องกัน User ไป configure ผิด
- **แสดงสถานะ IFB ใน QoS Status page แทน** — Transparent และ User เห็นสถานะจริง

```go
// ใน service/interface.go
var systemVirtualInterfaces = []string{
    "ifb", // IFB devices สำหรับ QoS ingress shaping
    "lo",  // loopback
}
```

---

## 4. โครงสร้าง Layer

```
┌─────────────────────────────────────────┐
│         API Handler Layer               │
│  internal/api/qos.go                    │
│  9 REST routes (CRUD + sync + status)   │
└────────────────┬────────────────────────┘
                 ↓
┌────────────────────────────────────────┐
│          Service Layer                 │
│  internal/service/qos.go              │
│  QosService: CRUD + SyncToKernel()    │
│  - อ่าน/เขียน DB                      │
│  - เรียก kernel.QosManager            │
└────────────────┬───────────────────────┘
                 ↓
┌────────────────────────────────────────┐
│        Kernel Interface                │
│  internal/kernel/interfaces.go        │
│  QosManager interface                 │
└────────────────┬───────────────────────┘
                 ↓ (implements)
┌────────────────────────────────────────┐
│      Kernel Implementation             │
│  internal/kernel/real_qos.go          │
│  RealQos: vishvananda/netlink          │
│  ├── Egress:  HTB Qdisc บน eth0       │
│  └── Ingress: IFB redirect → HTB      │
└────────────────────────────────────────┘
```

---

## 5. ไฟล์ที่ต้องสร้าง / แก้ไข

| ไฟล์ | Action | หมายเหตุ |
|------|--------|---------|
| `internal/model/types.go` | แก้ไข | เพิ่ม `QosRule`, `QosRuleInput`, `QosIfaceStatus` |
| `internal/kernel/interfaces.go` | แก้ไข | เพิ่ม `QosManager` interface |
| `internal/kernel/real_qos.go` | 🆕 สร้างใหม่ | HTB Qdisc + IFB implementation |
| `internal/kernel/mock.go` | แก้ไข | เพิ่ม `MockQos` สำหรับ test |
| `internal/service/qos.go` | 🆕 สร้างใหม่ | `QosService` CRUD + sync |
| `internal/db/qos.go` | 🆕 สร้างใหม่ | Repository methods + DB Schema |
| `internal/api/qos.go` | 🆕 สร้างใหม่ | HTTP Handler 9 routes |
| `internal/service/interface.go` | แก้ไข | กรอง `ifb*` ออกจาก interface list |
| `cmd/main.go` | แก้ไข | Wire `QosService` เข้า dependency injection |

---

## 6. DB Schema

```sql
CREATE TABLE IF NOT EXISTS qos_rules (
    id                TEXT PRIMARY KEY,
    name              TEXT NOT NULL,
    interface         TEXT NOT NULL,             -- e.g. "eth0"
    match_src_ip      TEXT NOT NULL DEFAULT '',  -- CIDR e.g. "172.24.25.0/24"
    match_dst_ip      TEXT NOT NULL DEFAULT '',  -- CIDR e.g. "0.0.0.0/0"
    egress_rate_mbps  INTEGER NOT NULL DEFAULT 0, -- Client Download, 0 = unlimited
    egress_ceil_mbps  INTEGER NOT NULL DEFAULT 0,
    ingress_rate_mbps INTEGER NOT NULL DEFAULT 0, -- Client Upload via IFB, 0 = unlimited
    ingress_ceil_mbps INTEGER NOT NULL DEFAULT 0,
    priority          INTEGER NOT NULL DEFAULT 10,
    status            INTEGER NOT NULL DEFAULT 1, -- 1=enabled, 0=disabled
    description       TEXT NOT NULL DEFAULT '',
    created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
);
```

---

## 7. API Routes

| Method | Path | หน้าที่ |
|--------|------|---------|
| `GET` | `/api/qos/rules` | ดึง QoS rules ทั้งหมด |
| `POST` | `/api/qos/rules` | สร้าง rule ใหม่ |
| `GET` | `/api/qos/rules/:id` | ดึง rule รายการเดียว |
| `PUT` | `/api/qos/rules/:id` | อัพเดท rule |
| `DELETE` | `/api/qos/rules/:id` | ลบ rule |
| `PATCH` | `/api/qos/rules/:id/toggle` | toggle enabled/disabled |
| `POST` | `/api/qos/sync` | บังคับ sync ไป kernel |
| `GET` | `/api/qos/status/:iface` | ดูสถานะ qdisc จาก kernel จริง |
| `DELETE` | `/api/qos/iface/:iface` | ลบ QoS ทั้งหมดออกจาก interface |

---

## 8. Phase การพัฒนา

### Phase 1 — Egress Only (Client Download Limit)
- HTB Qdisc บน eth0 ตรงๆ
- ทดสอบง่าย ไม่ต้องพึ่ง IFB
- พร้อม Production ได้เร็ว
- ตรงกับ use case จาก `qos-system.md` ที่ใช้อยู่จริง

### Phase 2 — Ingress via IFB (Client Upload Limit)
- `modprobe ifb` → สร้าง IFB device per interface
- tc ingress qdisc → mirred redirect → HTB บน IFB
- ต้องทดสอบบน Raspberry Pi 5 จริงก่อน
- ต้องตรวจสอบ IFB kernel module: `modprobe ifb && echo "IFB supported"`

---

## 9. ข้อควรระวัง

- **Idempotency**: `ApplyQosRules()` จะ clear + re-apply ทุกครั้ง เหมือน pattern ของ `SyncFirewallRules()`
- **HTB vs TBF**: ใช้ **HTB** เพราะรองรับหลาย class และ `ceil` (burst) — TBF เหมาะแค่ single rate
- **Ingress Policing vs Shaping**: `ingress` qdisc แบบ native แค่ DROP แพ็กเก็ต ไม่มี buffer — IFB ถึงจำเป็น
- **สิทธิ์**: ต้องการ `CAP_NET_ADMIN` ซึ่ง PiGate มีอยู่แล้วจาก `setcap`
