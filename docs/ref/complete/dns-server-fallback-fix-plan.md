# แผนแก้ไข DNS Server (dnsmasq) — Fallback Upstream ไม่ทำงาน + Multi-Zone ใช้ไม่ได้

> อ้างอิง GitHub Issues: [#7](https://github.com/saprayworld/pigate/issues/7) (fallback upstream ไม่ทำงาน), [#5](https://github.com/saprayworld/pigate/issues/5) (ใช้ zone มากกว่า 1 zone ไม่ได้)
>
> เอกสารนี้เป็นแผนงานก่อนเริ่มเขียนโค้ดจริง — สรุปจากการวิเคราะห์โค้ด + ผลทดสอบจริงบนเครื่อง (`dig`/`resolvectl`/journal log) ที่ยืนยัน root cause แล้ว ดูรายละเอียดการวินิจฉัยใน conversation ที่เกี่ยวข้อง สรุปสั้นๆ ไว้ในหัวข้อ "สาเหตุที่ยืนยันแล้ว" ด้านล่าง

---

## สาเหตุที่ยืนยันแล้ว (จากผลทดสอบจริงบนเครื่อง)

1. **ไม่มี upstream DNS ให้ dnsmasq forward เลย** — `dig @<pigate-ip> google.com` คืน `status: REFUSED` พร้อม `WARNING: recursion requested but not available`
2. Debian/Ubuntu package รัน dnsmasq ด้วย `-r /run/dnsmasq/resolv.conf` (**ไม่ใช่** `/etc/resolv.conf` ตามที่เข้าใจกันทั่วไป) และไฟล์นี้ถูกเติมข้อมูลผ่าน hook `start-resolvconf` ซึ่ง **fail** บนเครื่องนี้ (`Failed to set DNS configuration: Link lo is loopback device`) ทำให้ dnsmasq ไม่มี upstream ใดๆ เลย
3. เมื่อไม่มี upstream และมี `auth-zone=`/`auth-server=` (โค้ดปัจจุบันใน `RealDNSServerManager.ApplyZones`) dnsmasq จะเข้าสู่โหมด **authoritative-only** ซึ่ง**ปิด recursion ทั้งระบบ** ไม่ใช่แค่ปิดเฉพาะ zone ที่ประกาศไว้ — เป็นสาเหตุที่ query โดเมนภายนอกทั้งหมด (ไม่ใช่แค่โดเมนที่ชนกับ zone) โดน REFUSED
4. ทดสอบยืนยันแล้วว่า config ที่ใช้ **`no-resolv` + `server=<ip>` (explicit)** ร่วมกับเปลี่ยนจาก `auth-zone=`/`auth-server=` เป็น **`local=/<zone>/`** ทำให้ทั้ง fallback และ local zone resolution ทำงานถูกต้อง

ข้อสรุปสาเหตุของ Issue #5 (multi-zone ใช้ไม่ได้): มีความเป็นไปได้สูงว่ามาจากกลไก `auth-zone`/`auth-server` เดียวกันนี้ (auth-server ผูกกับ authoritative nameserver identity แบบ global ไม่ใช่ per-zone) — การเปลี่ยนไปใช้ `local=/<zone>/` ต่อ zone (ซึ่งเป็น directive ที่ dnsmasq ออกแบบมาให้ใช้ซ้ำได้หลายครั้งอยู่แล้ว ไม่มีข้อจำกัดเรื่องจำนวน) ควรแก้ปัญหานี้ไปด้วยในตัว แต่ **ต้องทดสอบยืนยันบนเครื่องจริงอีกครั้งหลังแก้** เพราะยังไม่เคยทดสอบ multi-zone กับ `local=` โดยตรง (ผลทดสอบที่มีตอนนี้เทสแค่ 2 zone แบบ ad-hoc ในไฟล์ทดลอง ยังไม่ผ่าน flow เต็มของ backend)

---

## ขอบเขตงาน (ตามที่ตกลงกัน)

1. เพิ่ม `IGNORE_RESOLVCONF=yes` ใน `install.sh` (ตัด dependency กับกลไก resolvconf ที่พังอยู่ ไม่มีผลกับ `systemd-resolved`/System DNS)
2. เขียน `no-resolv` + `server=<ip>` ใน `pigate-dns.conf` โดย **ดึงค่าจาก System DNS (`system_dns_settings` ผ่าน `DNSService.GetDNSConfig()`) ตรงๆ** — ไม่เพิ่ม field ใหม่ซ้ำซ้อน
3. เปลี่ยนการ generate authoritative zone จาก `auth-zone=`/`auth-server=` เป็น `local=/<zone>/` — แก้ทั้ง Issue #7 และ #5 ในคราวเดียว
4. Normalize target ของ `cname=` ให้เป็น FQDN อัตโนมัติเมื่อผู้ใช้กรอกชื่อสั้น (บั๊กที่พบเพิ่มเติมระหว่างวิเคราะห์ — ยืนยันแล้วจากผลทดสอบจริงว่า CNAME target ต้องเป็น FQDN ถึงจะ resolve ต่อได้)

**สรุปไฟล์ที่ต้องแก้ทั้งหมด**:

| ไฟล์ | สิ่งที่แก้ |
|---|---|
| `install.sh` | เพิ่ม `IGNORE_RESOLVCONF=yes` + restart dnsmasq (ขั้นตอนที่ 1) |
| `backend/internal/kernel/dns_server.go` | `no-resolv`/`server=`, `local=/zone/`, CNAME normalization (ขั้นตอนที่ 2, 5) |
| `backend/internal/kernel/interfaces.go` | signature `DNSServerManager.ApplyZones` (ขั้นตอนที่ 3) |
| `backend/internal/kernel/mock.go` | signature `MockDNSServerManager.ApplyZones` (ขั้นตอนที่ 3) |
| `backend/internal/service/dns_server.go` | inject `DNSService`, รวบรวม upstream servers (ขั้นตอนที่ 4) |
| `backend/cmd/pigate/main.go` | wiring constructor ใหม่ (ขั้นตอนที่ 4) |
| `backend/internal/api/handlers.go` | regenerate dnsmasq conf หลังแก้ System DNS (ขั้นตอนที่ 4.3) |

---

## ขั้นตอนการทำงาน (ตามลำดับที่แนะนำ)

### ขั้นตอนที่ 1 — `install.sh`: ตั้งค่า `IGNORE_RESOLVCONF=yes`

**ไฟล์**: `install.sh` (แก้ในสเต็ป 2.1 ที่ติดตั้ง dnsmasq + ตั้งค่า ACL อยู่แล้ว บรรทัดประมาณ 133-148)

```bash
# เพิ่มหลังขั้นตอนติดตั้ง dnsmasq
if [ -f /etc/default/dnsmasq ]; then
    if grep -q "^IGNORE_RESOLVCONF=" /etc/default/dnsmasq; then
        sed -i 's/^IGNORE_RESOLVCONF=.*/IGNORE_RESOLVCONF=yes/' /etc/default/dnsmasq
    elif grep -q "^#IGNORE_RESOLVCONF=" /etc/default/dnsmasq; then
        sed -i 's/^#IGNORE_RESOLVCONF=.*/IGNORE_RESOLVCONF=yes/' /etc/default/dnsmasq
    else
        echo "IGNORE_RESOLVCONF=yes" >> /etc/default/dnsmasq
    fi
else
    echo "IGNORE_RESOLVCONF=yes" > /etc/default/dnsmasq
fi
```

**ข้อควรระวัง**:
- ต้อง idempotent — รัน `install.sh` ซ้ำได้โดยไม่เพิ่มบรรทัดซ้ำ (เช็คก่อนด้วย `grep`)
- ไฟล์นี้เป็นคนละไฟล์กับ `/etc/dnsmasq.d/*.conf` ที่ PiGate generate — ห้ามใส่ `IGNORE_RESOLVCONF` ปนใน `pigate-*.conf` เพราะ syntax นี้เป็น env var ของ init/systemd-helper script ไม่ใช่ dnsmasq directive
- **`install.sh` ปัจจุบันไม่มีการ restart dnsmasq เองเลย** (ตรวจโค้ดแล้ว — สคริปต์ start เฉพาะ `pigate.service` ท้ายสคริปต์ แล้ว dnsmasq ค่อยถูก restart ทางอ้อมตอน pigate boot ผ่าน `InitApplyConfig()` → `RestartServiceViaDBus("dnsmasq.service")`) — เพื่อไม่ให้พึ่งพา side effect นี้ ให้เพิ่ม `systemctl restart dnsmasq` ต่อท้าย block ข้างบนไปเลย (env var มีผลเฉพาะตอน service (re)start)
- กรณีติดตั้งครั้งแรกแล้วผู้ใช้เลือกไม่ start pigate ทันที ก็ยังได้ผลจาก restart ตรงนี้
- **ห้ามแตะ `systemd-resolved`** — เป็นคนละกลไกกับที่ปิดตรงนี้ ยังต้องรันอยู่เพื่อ System DNS (`kernel/dns.go`)

---

### ขั้นตอนที่ 2 — Kernel Layer: `backend/internal/kernel/dns_server.go`

แก้ `RealDNSServerManager.ApplyZones`:

1. **เปลี่ยน signature** เพิ่ม parameter `upstreamServers []string`:
   ```go
   func (m *RealDNSServerManager) ApplyZones(zones []model.DNSZone, interfaces []string, upstreamServers []string) error
   ```

2. **เขียน `no-resolv` + `server=` เฉพาะเมื่อมี upstream จริง** (สำคัญ — ห้ามเขียน `no-resolv` โดยไม่มี `server=` เลยแม้แต่บรรทัดเดียว เพราะจะทำให้ dnsmasq ไม่มี upstream อะไรเลย แย่กว่าเดิม):
   ```go
   if len(upstreamServers) > 0 {
       sb.WriteString("no-resolv\n")
       for _, ip := range upstreamServers {
           ip = strings.TrimSpace(ip)
           if ip == "" {
               continue
           }
           sb.WriteString(fmt.Sprintf("server=%s\n", ip))
       }
       sb.WriteString("\n")
   }
   ```

3. **เปลี่ยน authoritative zone block** จาก:
   ```go
   sb.WriteString(fmt.Sprintf("auth-zone=%s\n", zoneName))
   if len(interfaces) > 0 {
       nsName := fmt.Sprintf("ns.%s", zoneName)
       sb.WriteString(fmt.Sprintf("auth-server=%s,%s\n", nsName, strings.Join(interfaces, ",")))
   }
   ```
   เป็น:
   ```go
   sb.WriteString(fmt.Sprintf("local=/%s/\n", zoneName))
   ```
   **ห้ามเขียน `domain=<zoneName>` ต่อ zone** — `domain=` เป็น directive ระดับ global (ตั้งได้ครั้งเดียว) ปัจจุบันถูกเขียนไว้แล้วใน `pigate-base.conf` โดย `RealDhcpManager.ensureBaseConfig()` (`kernel/dhcp_server.go` — **hardcode เป็น `domain=pigate.local`** ไม่ได้ดึงจาก `system_dns_settings.local_domain`; ความไม่สอดคล้องนี้เป็นประเด็นแยกต่างหาก ดูหัวข้อ "พบเพิ่มเติมนอกขอบเขต") ถ้าเขียน `domain=` ซ้ำต่อ zone จะเกิดหลายบรรทัดชนกัน ซึ่งเป็นความเสี่ยงหลักของ multi-zone bug (#5) — `local=/zone/` เพียงอย่างเดียวเพียงพอสำหรับทำให้ zone นั้นเป็น "local-only" (ตอบจาก `host-record=`/`cname=` ที่มี, คืน NXDOMAIN ถ้าไม่เจอ, ไม่ forward ออกไปนอก)
   - Records (`host-record=`, `mx-host=`, `txt-record=`, `ptr-record=`) เขียนเหมือนเดิมทุกอย่าง ยกเว้น `cname=` ที่ต้อง normalize target (ขั้นตอนที่ 5)
   - Forward zone (`server=/%s/%s`) ไม่ต้องแก้ — อยู่ร่วมกับ `server=<ip>` แบบ global ได้ (dnsmasq เลือกตัวที่ match เฉพาะเจาะจงกว่าก่อน)

4. **`interfaces` parameter** (เดิมใช้แค่กับ `auth-server=`) จะไม่ถูกใช้แล้วในเวอร์ชันนี้ — เก็บ parameter ไว้เผื่ออนาคต (เช่น emit `listen-address=` เพื่อ bind เฉพาะ interface ของ DNS Server ที่ยังเป็นช่องว่างอยู่ตอนนี้ ดูหัวข้อ "พบเพิ่มเติมนอกขอบเขต" ด้านล่าง) แต่ **ไม่ต้องแก้ในรอบนี้** เพราะนอกขอบเขตที่ตกลงกันไว้

**ข้อควรระวัง**:
- `no-resolv` มีผล global ต่อ dnsmasq ทั้ง process (รวมถึงส่วน DHCP ใน `pigate-dhcp.conf` ด้วย) — ไม่เป็นปัญหาเพราะฝั่ง DHCP ไม่ได้พึ่ง resolv.conf แต่ให้รู้ไว้ว่า directive นี้ไม่ได้จำกัดอยู่แค่ไฟล์ที่มันถูกเขียน
- โค้ดเดิมมี flow validate ด้วย `dnsmasq --test` ก่อนเขียนไฟล์จริงอยู่แล้ว — คงไว้เหมือนเดิม ห้ามข้าม

---

### ขั้นตอนที่ 3 — อัปเดต Interface + Mock ให้ signature ตรงกัน

**ไฟล์**:
- `backend/internal/kernel/interfaces.go` (บรรทัด ~83) — แก้ `DNSServerManager.ApplyZones(zones []model.DNSZone, interfaces []string, upstreamServers []string) error`
- `backend/internal/kernel/mock.go` (บรรทัด ~314) — แก้ `MockDNSServerManager.ApplyZones` ให้ signature ตรงกัน และ log ค่า `upstreamServers` ออกมาด้วย เพื่อให้ทดสอบผ่าน mock mode บน WSL แล้วเห็นว่า service layer ส่งค่าอะไรลงมา

**ข้อควรระวัง**: ตามกติกา CLAUDE.md — เพิ่ม/แก้ method ใน interface ต้องแก้ทั้ง real และ mock เสมอ ไม่งั้น build ไม่ผ่าน (mock ไม่มี build tag linux ใช้ตอน dev)

---

### ขั้นตอนที่ 4 — Service Layer: `backend/internal/service/dns_server.go` + wiring

ปัจจุบัน `DNSServerService` มีแค่ `repo` + `manager` — ยังเข้าถึง System DNS logic ไม่ได้ ต้อง inject `*DNSService` เพิ่ม (ห้ามอ่าน `system_dns_settings` จาก repo ตรงๆ เอง เพราะกรณี `mode=wan` ค่า upstream จริงอยู่ที่ per-link DNS ของ systemd-resolved ซึ่งมีแต่ `DNSService.GetDNSConfig()` ที่รวบรวมให้ — ตรงกับขอบเขตข้อ 2 "ไม่เพิ่ม field ใหม่ซ้ำซ้อน")

1. **แก้ constructor + struct**:
   ```go
   type DNSServerService struct {
       repo       *db.Repository
       manager    kernel.DNSServerManager
       dnsService *DNSService
   }

   func NewDNSServerService(repo *db.Repository, manager kernel.DNSServerManager, dnsService *DNSService) *DNSServerService
   ```

2. **เพิ่ม helper รวบรวม upstream** แล้วเรียกใน `ApplyAll()` ก่อนส่งเข้า `manager.ApplyZones(...)`:
   ```go
   func (s *DNSServerService) resolveUpstreams() []string {
       cfg, err := s.dnsService.GetDNSConfig()
       if err != nil {
           log.Printf("[DNSServerService] Warning: cannot read system DNS config: %v", err)
           return nil
       }
       var servers []string
       if cfg.Mode == "static" {
           if cfg.PrimaryDNS != "" {
               servers = append(servers, cfg.PrimaryDNS)
           }
           if cfg.SecondaryDNS != "" {
               servers = append(servers, cfg.SecondaryDNS)
           }
       } else { // mode == "wan": ใช้ DNS ที่ WAN link ได้จาก DHCP (DynamicDNS)
           for _, d := range cfg.DynamicDNS {
               servers = append(servers, d.DNSServers...)
           }
       }
       // dedupe + ตัด loopback (127.0.0.0/8, ::1) กัน query loop
       return sanitizeUpstreams(servers)
   }
   ```

3. **Regenerate เมื่อ System DNS เปลี่ยน** — ไม่งั้น `server=` ใน `pigate-dns.conf` จะค้างค่าเก่า: ใน `api/handlers.go` `HandleUpdateDNSConfig` (บรรทัด ~1483) หลัง `s.dnsService.UpdateDNSConfig(input)` สำเร็จ ให้เรียก `s.dnsServerService.ApplyAll()` ต่อ (ถ้า error ให้ log warning พอ อย่า fail ทั้ง request เพราะ System DNS apply สำเร็จไปแล้ว) — handler struct มี `dnsServerService` อยู่แล้ว ไม่ต้อง wiring เพิ่ม

4. **`backend/cmd/pigate/main.go`** (บรรทัด ~118): แก้เป็น `service.NewDNSServerService(repo, dnsServer, dnsService)` — ลำดับสร้างไม่มีปัญหา เพราะ `dnsService` ถูกสร้างก่อน (บรรทัด ~115) และไม่มี dependency ย้อนกลับ (ไม่เกิด circular)

**ข้อควรระวัง**:
- **ห้ามเขียน `no-resolv` เมื่อ list ว่าง** — logic นี้อยู่ฝั่ง kernel แล้ว (ขั้นตอนที่ 2.2) แต่ service ต้องไม่ส่ง element ว่าง/ซ้ำลงไป
- **กรอง loopback ออกเสมอ** — ถ้า upstream เป็น `127.0.0.53` (stub ของ systemd-resolved) หรือ IP ของตัวเครื่องเอง อาจเกิด forwarding loop ระหว่าง dnsmasq ↔ resolved ได้
- **ตอน boot ในโหมด `wan`**: `dnsServerService.InitApplyConfig()` (main.go ~189) รันตามลำดับ startup — ณ จุดนั้น WAN อาจยังไม่ได้ DNS จาก DHCP ทำให้ upstream list ว่าง → config จะไม่มี `no-resolv`/`server=` (ปลอดภัย ไม่แย่กว่าพฤติกรรมเดิม) แต่จะยังไม่ fallback จนกว่าจะมีการ `ApplyAll()` รอบถัดไป — ยอมรับข้อจำกัดนี้ในรอบนี้ และบันทึกไว้ในหัวข้อ "พบเพิ่มเติมนอกขอบเขต" (ทางแก้ระยะยาวคือให้ `netlink_monitor.go` หรือ event จาก resolved trigger regenerate)
- อย่าลืมว่า `GetDNSConfig()` ในโหมด `wan` ไป query systemd-resolved สดๆ — บน mock mode ค่าที่ได้มาจาก `MockDNSManager` ตรวจให้แน่ใจว่า mock คืนค่าอะไรสักอย่างเพื่อให้เทส flow ได้

---

### ขั้นตอนที่ 5 — CNAME target normalization (ขอบเขตข้อ 4)

**ไฟล์**: `backend/internal/kernel/dns_server.go` (จุด generate `cname=` ใน `ApplyZones`)

กติกา normalize: ถ้า target ที่ผู้ใช้กรอก**ไม่มีจุด (`.`) เลย** ให้ถือเป็นชื่อสั้นภายใน zone แล้วต่อท้ายด้วย zone name อัตโนมัติ; ถ้ามีจุดอยู่แล้วถือว่าเป็น FQDN ใช้ตามนั้น (ตัด trailing dot ทิ้งถ้ามี เพราะ dnsmasq ไม่ใช้ trailing dot):

```go
case "CNAME":
    target := strings.TrimSuffix(strings.TrimSpace(rec.Value), ".")
    if target != "" && !strings.Contains(target, ".") {
        target = fmt.Sprintf("%s.%s", target, zoneName)
    }
    sb.WriteString(fmt.Sprintf("cname=%s,%s\n", fullName, target))
```

**ข้อควรระวัง**:
- Normalize เฉพาะตอน generate config — **ห้ามแก้ค่าใน DB** ผู้ใช้เห็นค่าที่ตัวเองกรอกเสมอ (ถ้าอยากช่วยผู้ใช้เพิ่ม ค่อยทำ validation/hint ฝั่ง frontend เป็นงานแยก)
- CNAME target ที่ชี้ออกนอก zone ที่เป็น `local=/zone/` เดียวกัน: dnsmasq resolve `cname=` ได้เฉพาะ target ที่ dnsmasq รู้จักเอง (host-record/DHCP lease) — target ภายนอกจะไม่ทำงาน เป็นข้อจำกัดของ dnsmasq เอง ควรระบุใน docs/UI ภายหลัง

---

### ขั้นตอนที่ 6 — Build + ทดสอบ

1. **บน WSL (dev)**:
   ```bash
   cd backend && go build ./... && go test ./...
   ./pigate-backend -port=8081 -db=pigate.db -mock=true
   ```
   - สร้าง zone ≥ 2 zone ผ่าน UI/API แล้วดู log ของ `MockDNSServerManager.ApplyZones` ว่า upstream ถูกส่งลงมาถูกต้องทั้งโหมด `static` และ `wan`
   - แก้ System DNS ผ่าน `PUT /api/system/dns` แล้วยืนยันว่า `ApplyZones` ถูกเรียกซ้ำ (ขั้นตอนที่ 4.3)
2. **บนเครื่องจริง** (ผู้ใช้เป็นคน upload/deploy เอง ตาม workflow ของโปรเจกต์):
   - รัน `install.sh` ซ้ำ → เช็ค `/etc/default/dnsmasq` มี `IGNORE_RESOLVCONF=yes` บรรทัดเดียว
   - เช็คไฟล์ `/etc/dnsmasq.d/pigate-dns.conf` ที่ generate ใหม่: มี `no-resolv`, `server=<ip>` ตาม System DNS, `local=/<zone>/` ต่อ zone, ไม่มี `auth-zone`/`auth-server`
   - `dig @<pigate-ip> google.com` → ต้องได้ `NOERROR` (Issue #7)
   - สร้าง **2+ zones** พร้อม record แล้ว `dig` ชื่อในแต่ละ zone → ต้อง resolve ครบทุก zone (Issue #5) และชื่อที่ไม่มีใน zone ต้องได้ `NXDOMAIN` ไม่ใช่ forward ออกไป
   - ทดสอบ CNAME แบบกรอกชื่อสั้น → ต้อง resolve ต่อไปยัง A record ได้
   - `systemctl status dnsmasq` + journal ต้องไม่มี error `start-resolvconf`

---

## พบเพิ่มเติมนอกขอบเขต (บันทึกไว้ ไม่ทำในรอบนี้)

1. **dnsmasq ไม่ bind เฉพาะ interface ของ DNS Server** — `interfaces` ที่เลือกใน UI เดิมถูกใช้แค่กับ `auth-server=` พอเลิกใช้แล้ว parameter นี้ไม่มีผลอะไรเลย ควร emit `interface=`/`listen-address=` ในอนาคต (ระวังชนกับ `bind-interfaces` ใน `pigate-base.conf` และ interface ฝั่ง DHCP)
2. **`domain=pigate.local` ใน `pigate-base.conf` เป็นค่า hardcode** (`kernel/dhcp_server.go` → `ensureBaseConfig()`) ไม่ sync กับ `system_dns_settings.local_domain` — ควรดึงค่าจริงมาเขียนในอนาคต
3. **โหมด `wan`: upstream เปลี่ยนแล้ว `pigate-dns.conf` ไม่ regenerate อัตโนมัติ** — ถ้า ISP เปลี่ยน DNS ผ่าน DHCP ค่าที่ freeze ไว้ในไฟล์จะค้าง ควรให้ `netlink_monitor.go` หรือ hook ฝั่ง dhcpcd trigger `DNSServerService.ApplyAll()` ในอนาคต
