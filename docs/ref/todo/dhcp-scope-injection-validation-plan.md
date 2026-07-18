# DHCP Scope Injection Validation — ปิดช่องฉีดค่าตั้งค่า DHCP (Finding #11)

> แผนงานปิดช่องโหว่ระดับกลาง Finding #11 จากรายงานรีวิวความปลอดภัย 17 ก.ค. 2026:
> ค่าตั้งค่าขอบเขต DHCP (`DhcpConfig`: interface, start/end IP, gateway, netmask, DNS1/2)
> ถูกเขียนลง `pigate-dhcp.conf` โดย **ไม่มีการ validate** เลย ทำให้แทรก newline เพื่อ
> ฉีด directive ของ dnsmasq ได้ (เช่น command execution ผ่าน dnsmasq option) — ตรงข้ามกับ
> ฝั่ง reservation ที่มี `ValidateReservation` ครบทั้ง 3 จุดแล้ว งานนี้เติมช่อง `DhcpConfig`
> ให้เท่ากัน
>
> เขียนเมื่อ: 2026-07-18 · Reference branch: `main` (แยก `fix/dhcp-scope-injection`)
> อ้างอิง: `docs/review-guide.md` §5–7, artifact รายงานรีวิว Finding #11

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย:** ทุกค่าใน `DhcpConfig` ที่ถูกเขียนลงไฟล์ dnsmasq ต้องผ่าน whitelist validator
  ที่ REJECT (ไม่ strip) เหมือน `ValidateReservation` — ครบทั้ง **3 จุด** ตามที่รายงานระบุ:
  1. จุดรับข้อมูล (handlers: create/update DHCP config)
  2. จุดนำเข้า backup (`service/backup.go` → `validateBackupConfig`)
  3. จุดสร้างไฟล์ (defense-in-depth ใน `kernel/dhcp_server.go` → `ApplyConfig`)
- **"เสร็จ" คือ:** ยิง `POST/PUT /api/dhcp/configs` ด้วย `startIp` ที่ฝัง `\n` แล้ว interface
  ตอบ 400; import backup ที่มี DhcpConfig อันตรายถูก reject ทั้งไฟล์; และแม้เผลอมีค่าอันตราย
  หลุดถึง `ApplyConfig` ก็ถูกข้าม/ปฏิเสธ ไม่เขียนลงไฟล์จริง
- **Out of scope:**
  - ไม่แตะ logic ฝั่ง reservation (ครบแล้ว)
  - ไม่เพิ่ม validation เชิง "ความสมเหตุสมผลของ subnet" (เช่น start < end, IP อยู่ใน subnet
    ของ netmask) — เป็นเรื่อง UX/correctness ไม่ใช่ช่องโหว่ injection; แยกงานทีหลังได้
  - ไม่แตะ frontend logic (form เดิมส่งค่าปกติอยู่แล้ว) — validation ฝั่ง client เป็น
    optional polish ท้ายแผน
  - Finding #12 (อัปเกรด Go ≥ 1.26.5) เป็นคนละงาน

## 1. สภาพโค้ดปัจจุบัน (สำรวจ ณ วันเขียน 2026-07-18)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Validator ของ reservation | ✅ มีครบ (REJECT model) | `model/dns_validate.go:173` `ValidateReservation`, `:50` `ValidateInterfaceName` |
| Validator ของ `DhcpConfig` | ❌ **ไม่มีเลย** | ยืนยันด้วย grep: ไม่พบ `ValidateDhcpConfig` ทั้ง repo |
| Handler create config | ❌ decode → `repo.CreateDHCPConfig` ตรง ไม่ validate | `api/handlers.go:1798` `HandleCreateDHCPConfig` |
| Handler update config (body) | ❌ ไม่ validate | `api/handlers.go:1648` `HandleUpdateDHCPConfig` |
| Handler update config (by ID) | ❌ ไม่ validate | `api/handlers.go:1814` `HandleUpdateDHCPConfigByID` |
| Handler reservation (เทียบ) | ✅ เรียก `ValidateReservation` แล้ว | `api/handlers.go:1692`, `:1727` |
| Backup import — reservations | ✅ validate ใน loop | `service/backup.go:686` |
| Backup import — **DhcpConfigs** | ❌ **ไม่มี loop validate** | `service/backup.go:672-690` (มีแต่ DnsZone/Record/Reservation) |
| Kernel `ApplyConfig` — configs | ❌ เขียน `cfg.StartIP/EndIP/Gateway/DNS1/DNS2/Interface` ลง sb ตรง | `kernel/dhcp_server.go:61-82` |
| Kernel `ApplyConfig` — reservations | ✅ มี `ValidateReservation` defense-in-depth | `kernel/dhcp_server.go:92` |
| Mock `ApplyConfig` | ✅ no-op ปลอดภัย (return nil) | `kernel/mock.go:182` |
| โครงสร้าง `DhcpConfig` | Interface/StartIP/EndIP/Gateway/Netmask/DNS1/DNS2 string + LeaseTime int | `model/types.go:261-272` |
| Netmask ที่ frontend ส่ง | dotted-decimal เช่น `255.255.255.0` (ไม่ใช่ CIDR) | `frontend/src/pages/DhcpServer.tsx:949` placeholder |

**สรุป:** งานกระจุกที่ **backend อย่างเดียว** — เพิ่ม validator 1 ตัวใน `model` แล้ว wire เข้า
3 จุด (handlers ×3, backup ×1, kernel ×1) ให้ pattern ตรงกับ reservation ที่มีอยู่แล้ว 100%

## 2. แนวทางเทคนิค

เพิ่มฟังก์ชัน `model.ValidateDhcpConfig(cfg DhcpConfig) error` ใน `model/dns_validate.go`
(ไฟล์เดียวกับ validator ตัวอื่น — pure, ไม่มี import cycle, ทุก layer เรียกได้) โดย:

```go
// ValidateDhcpConfig validates every DhcpConfig field written to pigate-dhcp.conf.
// Interface, start/end IP are always written; gateway/netmask/dns are optional.
func ValidateDhcpConfig(cfg DhcpConfig) error {
    if err := ValidateInterfaceName(cfg.Interface); err != nil { // reuse ตัวเดิม
        return err
    }
    for _, f := range []struct{ name, val string; required bool }{
        {"startIp", cfg.StartIP, true},
        {"endIp", cfg.EndIP, true},
        {"gateway", cfg.Gateway, false},
        {"netmask", cfg.Netmask, false},
        {"dns1", cfg.DNS1, false},
        {"dns2", cfg.DNS2, false},
    } {
        v := strings.TrimSpace(f.val)
        if v == "" {
            if f.required { return fmt.Errorf("dhcp %s must not be empty", f.name) }
            continue
        }
        if net.ParseIP(v) == nil { // net.ParseIP ปฏิเสธ \n/\r โดยธรรมชาติ
            return fmt.Errorf("dhcp %s %q is not a valid IP address", f.name, f.val)
        }
    }
    return nil
}
```

- **ทำไม `net.ParseIP`:** ทุกฟิลด์ที่เขียนลงไฟล์เป็น IP ล้วน (รวม netmask ที่เป็น dotted-decimal
  `255.255.255.0` — `net.ParseIP` ผ่าน) และ `net.ParseIP` reject อักขระควบคุม/ช่องว่าง/newline
  อยู่แล้ว จึงกัน injection ได้ตรงจุด สอดคล้องกับที่ `ValidateReservation:178` ใช้ `net.ParseIP`
  กับ reserved IP อยู่แล้ว
- **ทำไม reuse `ValidateInterfaceName`:** interface ถูกเขียนทั้งบรรทัด `interface=` และ
  `dhcp-range=<iface>,...` — เป็น validator เดียวกับที่ DNS server/reservation ใช้ (regex
  `reInterfaceName`, `dns_validate.go:42`) กันชื่อที่มี newline/ยาวเกิน IFNAMSIZ
- **Netmask:** สังเกตว่า `ApplyConfig` **ไม่ได้เขียน Netmask ลงไฟล์** (ใช้แค่ใน
  `findInterfaceForIP`, `service/dhcp_server.go:96`) แต่ยัง validate ไว้เพื่อความสม่ำเสมอ/กัน
  ค่าเพี้ยนไปพังการ map subnet — ระบุใน caution ว่าไม่ใช่ฟิลด์ injection โดยตรง
- **Pattern ที่ยึดเป็นแม่แบบ:** ล้อ `ValidateReservation` + การ wire ของ reservation ทั้ง 3 จุด
  ทุกประการ (handler → 400, backup → fail-closed ทั้งไฟล์, kernel → skip+log)
- **ทางเลือกที่ปฏิเสธ:** (1) strip อักขระอันตรายแทน reject — ปฏิเสธ เพราะ header ของ
  `dns_validate.go:16-22` ระบุ policy ทั้งไฟล์ว่า REJECT ไม่ strip เพื่อ feedback ชัด
  (2) validate เฉพาะใน handler — ปฏิเสธ เพราะ backup import เขียน DB ตรง bypass handler และ
  รายงานสั่งให้ครบทั้ง 3 จุด (defense-in-depth)

## 3. ขั้นตอน (เรียง inner-layer-first)

### Step 1 — เพิ่ม validator ใน model
**File:** `backend/internal/model/dns_validate.go` (แก้ไข, ต่อท้ายราว `:183`)
เพิ่ม `ValidateDhcpConfig` ตามโค้ดใน §2 ใช้ `net`/`strings`/`fmt` ที่ import อยู่แล้ว

### Step 2 — เพิ่ม unit test ของ validator
**File:** `backend/internal/model/dns_validate_test.go` (แก้ไข, ล้อ `TestValidateReservation` `:98`)
เคสอย่างน้อย: ค่า valid ครบ; `startIp` ฝัง `"192.168.1.10\naddress=/evil/6.6.6.6"` → error;
interface ฝัง newline → error; gateway/dns ว่าง → ผ่าน; startIp ว่าง → error; netmask dotted ผ่าน

### Step 3 — defense-in-depth ใน kernel generation
**File:** `backend/internal/kernel/dhcp_server.go` (แก้ไข ใน `ApplyConfig` ราว `:50-59`)
ในลูป `for _, cfg := range cfgs` หลังเช็ค `!cfg.Enabled` ให้เพิ่ม (ล้อ reservation `:92`):

```go
if err := model.ValidateDhcpConfig(cfg); err != nil {
    log.Printf("[DHCP Server] Skipping invalid DHCP config (iface=%q start=%q end=%q): %v",
        cfg.Interface, cfg.StartIP, cfg.EndIP, err)
    continue
}
```

> ไม่ต้องแก้ mock.go — `MockDhcp.ApplyConfig` เป็น no-op (`mock.go:183`) ปลอดภัยอยู่แล้ว

### Step 4 — validate จุดนำเข้า backup
**File:** `backend/internal/service/backup.go` (แก้ไขใน `validateBackupConfig` ราว `:686`)
เพิ่ม loop ก่อน/หลัง loop reservation:

```go
for _, c := range cfg.DhcpConfigs {
    if err := model.ValidateDhcpConfig(c); err != nil {
        return fmt.Errorf("dhcp config %q: %w", c.Interface, err)
    }
}
```

### Step 5 — validate จุดรับข้อมูล (handlers ×3)
**File:** `backend/internal/api/handlers.go`
หลัง decode และก่อนเรียก repo ให้ใส่ (คืน 400 เหมือน reservation `:1692`):
```go
if err := model.ValidateDhcpConfig(cfg); err != nil {
    s.writeError(w, http.StatusBadRequest, err.Error()); return
}
```
- `HandleUpdateDHCPConfig` ราว `:1653` (ก่อน `repo.UpdateDHCPConfig`)
- `HandleCreateDHCPConfig` ราว `:1804` (ก่อน `repo.CreateDHCPConfig`)
- `HandleUpdateDHCPConfigByID` ราว `:1821` (หลัง `cfg.ID = id`, ก่อน `repo.UpdateDHCPConfigByID`)

### Step 6 (optional) — mirror validation ฝั่ง frontend
**File:** `frontend/src/pages/DhcpServer.tsx` — เพิ่มเช็ครูปแบบ IP ก่อน submit เพื่อ UX
(error ชัดก่อนยิง API) ไม่ใช่ชั้นความปลอดภัยจริง — backend เป็นตัวบังคับ

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรมหลังแก้ |
|---|---|---|---|
| POST | `/api/dhcp/configs` | super_admin (RoleReadOnly บล็อก non-super) | ค่าอันตราย → **400** (เดิม 200) |
| PUT | `/api/dhcp/configs/{id}` | super_admin | ค่าอันตราย → **400** |
| PUT | `/api/dhcp/config` (body) | super_admin | ค่าอันตราย → **400** |
| POST | `/api/config/import` | super_admin | DhcpConfig อันตราย → reject ทั้งไฟล์ (atomic) |

route ทั้งหมด **มีอยู่แล้ว** ไม่เพิ่มใหม่; `-disable-edit=true` บล็อก mutation เหล่านี้ทั้งหมด
อยู่แล้วผ่าน `DisableEditMiddleware` — ไม่ต้องแก้ contract/openapi (response 400 เป็น validation
error ทั่วไป ไม่ใช่ schema ใหม่)

## 5. ข้อควรระวัง (Cautions)

- **fail-closed ของ backup ต้องมาก่อนการเขียน DB:** `validateBackupConfig` ถูกเรียกก่อน
  transaction import (atomic) — ถ้าเผลอย้าย loop ไปหลังเขียน จะเขียนค่าอันตรายลง DB ไปแล้ว
  ก่อน reject → ต้องคง loop ไว้ในฟังก์ชัน validate เดิม (`:672-690`) เท่านั้น
- **kernel ใช้ `continue` ไม่ใช่ error:** ให้ข้าม config ที่ไม่ผ่าน (เหมือน reservation) ไม่ให้
  ทั้ง `ApplyConfig` ล้ม — เพราะ handler/backup ปิดต้นทางแล้ว ชั้นนี้เป็น defense-in-depth
  การ fail ทั้งก้อนจะทำให้ config ดี ๆ อันอื่นไม่ถูก apply ตามไปด้วย
- **Netmask ไม่ใช่ฟิลด์ injection โดยตรง** (ไม่ถูกเขียนลงไฟล์ dnsmasq, `dhcp_server.go` ไม่มี
  บรรทัด netmask) แต่ validate ไว้กันค่าเพี้ยนทำ `findInterfaceForIP` map subnet ผิด → lease
  ถูกผูก interface ผิด ระบุไว้ว่าเป็น hygiene ไม่ใช่ security-critical
- **อย่าเข้มกว่าที่ writer รับ:** gateway/dns/netmask เป็น optional — ถ้า required หมดจะ reject
  config เดิมที่ตั้ง DNS/gateway ว่างไว้โดยชอบ (writer เขียน dhcp-option เฉพาะเมื่อ non-empty,
  `dhcp_server.go:71,75`) ให้ตรง: ว่าง = ผ่าน, มีค่า = ต้องเป็น IP
- **ทดสอบ mock ปลอดภัย 100%:** ทุกเคสทดสอบผ่าน `-mock=true` บน workstation ได้ (mock
  ApplyConfig no-op ไม่แตะ dnsmasq จริง) — ไม่ต้องมีบอร์ดจริง; การทดสอบไม่เสี่ยงล็อกตัวเอง
- **ของเดิมใน DB ที่อาจมีค่าไม่ผ่าน validator:** ถ้าผู้ใช้เคยบันทึก config แปลก ๆ ไว้ก่อนแก้
  ครั้งนี้ `ApplyConfig` จะเริ่ม skip มัน (log เตือน) — พฤติกรรมนี้ถูกต้อง (ค่านั้นอันตราย/ผิด
  รูปอยู่แล้ว) แต่ควรระบุใน commit ว่า config ที่ IP ผิดรูปจะไม่ถูก apply อีก

## 6. Checklist (Definition of Done)

- [x] `model/dns_validate.go` — เพิ่ม `ValidateDhcpConfig`
- [x] `model/dns_validate_test.go` — เพิ่มเทสต์ `TestValidateDhcpConfig` (รวมเคส newline injection)
- [x] `kernel/dhcp_server.go` — `continue` เมื่อ config ไม่ผ่าน ใน `ApplyConfig`
- [x] `service/backup.go` — loop validate `cfg.DhcpConfigs` ใน `validateBackupConfig`
- [x] `api/handlers.go` — validate ใน create/update/update-by-id (คืน 400) ×3
- [ ] (optional) `frontend/DhcpServer.tsx` — เช็ค IP ฝั่ง client ก่อน submit (ข้ามไว้ — backend บังคับพอ)
- [x] `cd backend && go build ./... && go test ./...` ผ่าน (+ `go vet ./...` สะอาด)
- [x] ทดสอบ mock (handler-level): `POST /api/dhcp/configs` ด้วย `startIp` ฝัง `\n` → 400 (`TestDNSAndDHCPInjectionRejected`)
- [x] ทดสอบ mock: import backup ที่มี DhcpConfig อันตราย → reject ทั้งไฟล์ (`TestImportRejectsDhcpConfigInjection`)
- [x] ทดสอบ role: non-super_admin โดน 403 เหมือนเดิม (ไม่เปลี่ยน route/middleware)
- [x] ไม่ต้องแก้ `docs/openapi.yaml` / `frontend/public/openapi.yaml` (ไม่มี schema ใหม่)
- [ ] อัปเดต `docs/review-guide.md` ว่า Finding #11 ปิดแล้ว (ถ้าต้องการ track)
- [ ] แยก branch `fix/dhcp-scope-injection` → PR เข้า main (code change ห้าม push ตรง)
