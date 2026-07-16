QoS System ที่ใช้อยู่ตอนนี้ (ไม่ใช่ในระบบที่กำลังทำ) โดยการรันคำสั่งนี้

sudo tc qdisc add dev eth0 root handle 1: htb default 10
sudo tc class add dev eth0 parent 1: classid 1:1 htb rate 50mbit ceil 50mbit
sudo tc filter add dev eth0 parent 1:0 protocol ip prio 1 u32 match ip src 172.24.25.0/24 flowid 1:1
sudo tc filter add dev eth0 parent 1:0 protocol ip prio 1 u32 match ip dst 172.24.25.0/24 flowid 1:1
sudo tc filter add dev eth0 parent 1:0 protocol ip prio 1 u32 match ip dst 0.0.0.0/0 flowid 1:1

บน Router RPI5 ตัวหลัก
มี Interface อยู่ 2 ขา คือ wlan0 กับ eth0
- ขา wlan0 ได้ปล่อยไปตามปกติ
- ขา eth0 จำกัดที่ขานี้แทนที่ความเร็ว 50Mbps

จากการทำผลคือได้ลิมิตความเร็วตอนดาวน์โหลดที่เครื่อง client ที่ 50Mbps แต่ตอนอัพโหลดไม่สามารถจำกัดได้เลย ได้ความเร็วเต็มความเร็วตลอด
ตอนนี้ก็ใช้งานได้แหละ เพราะไม่ค่อยได้อัพโหลดอะไรมาก

ที่ทำเช่นนี้เพราะจับกับเครือข่ายที่ใช้ร่วมกับคนอื่นจึงไม่อยากไปกินแบนด์วิธของคนอื่นเยอะเกินไป

ในส่วนนี้จะเป็นการอัพเดทโปรเจกต์ Pigate โดยการเพิ่มระบบ QoS เข้าไป

---

## IFB Ingress Capability (issue #53)

QoS ขา **Ingress** (จำกัดความเร็ว upload ของ client) ต้อง redirect ทราฟฟิกขาเข้าไปที่
virtual interface `IFB` ซึ่งอาศัย kernel module `ifb`. ขา **Egress** (download) ไม่ต้องใช้
IFB เพราะ shape ที่ physical interface โดยตรง.

### Probe ตอน startup

`NewRealQos()` (`backend/internal/kernel/real_qos.go`) จะ **probe ครั้งเดียว** ตอน construct
โดยลอง `netlink.LinkAdd` IFB link ชื่อ `pigate-ifb0` (≤15 ตัวอักษรตาม IFNAMSIZ และไม่ชน
แพทเทิร์น `ifb-<iface>` ที่ `ClearQosRules` ใช้ลบ):

- สำเร็จ หรือได้ `EEXIST` (link ค้างจาก crash รอบก่อน) → ถือว่า **รองรับ** แล้วลบ link ทิ้ง
  (kernel เรียก `request_module("rtnl-link-ifb")` auto-load module ให้เอง — ฝั่ง caller
  ไม่ต้องมี `CAP_SYS_MODULE`)
- ล้มเหลว → ถือว่า **ไม่รองรับ** + log warning

ผล cache ไว้ใน field `RealQos.ingressSupported`. ต้อง probe ที่ construct **เท่านั้น** ไม่ใช่
lazy ตอน request แรก เพราะการสร้าง/ลบ probe link ระหว่างที่ `netlink_monitor` รันอยู่จะยิง
`InterfaceAdded` ปลอมเข้า self-healing event bus (construct ที่ `main.go` เกิดก่อน
`netlinkMonitor.Start`).

> **หมายเหตุ:** โค้ดเดิมเคยเรียก `modprobe ifb` ผ่าน `execCommand` ก่อนสร้าง IFB link — ถูกลบออก
> แล้ว เพราะเป็น dead code (runtime ไม่มี `CAP_SYS_MODULE` จึง fail เสมอ) และขัด constraint
> "no shell execution" ของโปรเจกต์. การ load module จริงมาจาก `/etc/modules-load.d/pigate.conf`
> ตอน boot (ตั้งโดย `install.sh`) + kernel auto `request_module` ตอน `LinkAdd`.

### พฤติกรรม fail-safe (คงเดิมจาก commit `152a127`)

ถ้า `ingressSupported == false`, `ApplyQosRules` จะ **log + skip เฉพาะ ingress section** ของ
interface นั้น — egress ยัง apply ปกติ, sync ทั้งก้อนไม่ล้ม (ห้าม return error). เส้นทาง
skip เดิมที่ `LinkByName` ของ IFB link คงไว้เป็น safety net.

### Expose สู่ UI

field `ingressSupported` (bool) ไหลออกทาง `model.QosIfaceStatus` →
`GET /api/qos/status/{iface}` (route เดิม, additive/backward-compatible). Mock backend คืน
`true` เสมอ (dev workstation ถือว่ารองรับ).

หน้า QoS (`frontend/src/pages/QoS.tsx`) derive `ingressUnsupported` จาก
`Object.values(ifaceStatuses).some(s => s.ingressSupported === false)` (ใช้ `=== false`
เพื่อไม่ให้ status ที่โหลดไม่สำเร็จกลายเป็น false positive) แล้ว:
- แสดง banner เตือนใต้ส่วนหัวของหน้า
- disable ช่องกรอก Ingress Rate/Ceil ใน dialog (ค่าที่มีอยู่เดิมของกฎยังถูกเก็บ/ส่งกลับตามเดิม)
- ในตาราง rule ขีดฆ่าค่า Ingress Limit + ไอคอนเตือนเมื่อ rule นั้นมี ingress > 0

