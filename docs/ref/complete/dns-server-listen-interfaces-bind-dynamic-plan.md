# DNS Server Listen Interfaces + bind-dynamic — ให้ DNS Server ฟังตาม Listen Interfaces จริง ไม่ขี่ config ของ DHCP (Issue 50)

> แผนงานแก้บั๊ก: `pigate-dns.conf` ไม่เคย emit `interface=` — การ listen ของ DNS Server
> ขี่บรรทัด `interface=` จาก `pigate-dhcp.conf` ทั้งหมด ทำให้ **ปิด DHCP Server หมด =
> DNS Server ใบ้ทั้งระบบ** แก้โดย (1) เปลี่ยน base config `bind-interfaces` →
> `bind-dynamic` (2) emit `interface=<name>` ตาม Listen Interfaces ลง `pigate-dns.conf`
> (3) DNS apply path ต้อง ensure base config ด้วย (4) ปรับ wording frontend
> Auth → Local zone ให้ตรง backend
>
> เขียนเมื่อ: 2026-07-13 · Reference branch: `feat/dns-server-listen-interfaces`
> เกี่ยวข้อง: GitHub issue #50 (bug นี้) · **ทำหลังงาน #46 merge** (แตะ `DnsServer.tsx` ชุดเดียวกัน)

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- ตั้ง Listen Interfaces ในหน้า DNS Server → dnsmasq ฟัง DNS บน interface เหล่านั้นจริง
  **โดยไม่ต้องเปิด DHCP Server เลยแม้แต่ interface เดียว**
- Interface ใน Listen Interfaces ที่หายไปจากระบบ → dnsmasq ยังรันปกติ (ไม่ fatal)
  และเมื่อ interface กลับมา dnsmasq bind ให้เอง**โดยไม่ต้อง restart/re-apply**
  (self-healing ระดับ dnsmasq — พิสูจน์บน LAB VM แล้ว 2026-07-13, หลักฐานใน issue #50)
- DHCP Server ทำงานเหมือนเดิมทุกประการภายใต้ `bind-dynamic`
- หน้า DNS Server เปลี่ยน wording "Auth/Authoritative" → "Local zone" ตรงกับ backend
  ที่ใช้ `local=/zone/` แล้ว (API field `isAuthoritative` **คงเดิม** — ไม่ break contract)

**Out of scope (ตัดชัด):**
- `listen-address=<ip>` (เจาะจง IP) — refinement เฟสหลังเมื่อมี event bus (issue #48);
  เหตุผลที่ไม่ทำตอนนี้: config stale เมื่อ IP เปลี่ยน + ต้อง resolve ชื่อ→IP สดตอน gen + boot race
- ถอด skip-if-missing ของฝั่ง DHCP (`dhcp_server.go:~78`) แม้ bind-dynamic จะทนได้แล้ว —
  พฤติกรรม DHCP เดิมคงไว้ก่อน ลดตัวแปร regression (ค่อยพิจารณาใน issue #48)
- การหยุด dnsmasq เมื่อไม่มี interface config เลย / except-interface บน WAN — ดู Caution 6
- เปลี่ยนชื่อ field `isAuthoritative` ใน API/DB/model — แตะเฉพาะ display text

## 1. Current State (สำรวจโค้ดจริง 2026-07-13)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| `ApplyZones` รับ `interfaces` | **ใช้แค่ log — ไม่ emit ลง config เลย** | `backend/internal/kernel/dns_server.go:~35-36`; config gen `:~38-150` ไม่มี `interface=` |
| การ listen จริงของ DNS | ขี่ `interface=` จาก DHCP config เท่านั้น | `backend/internal/kernel/dhcp_server.go:~84` |
| Base config | `bind-interfaces` — เขียนทับ**ทุกครั้ง**ที่ DHCP apply แต่เรียกจาก DHCP `ApplyConfig` เท่านั้น; **DNS apply path ไม่ ensure base เลย** | `dhcp_server.go:~32-49` (`ensureBaseConfig`), call site `:~65` |
| ผลของ `bind-interfaces` + ชื่อไม่มีจริง | **fatal** — `unknown interface` / `FAILED to start up`; `dnsmasq --test` จับไม่ได้ (เช็คแค่ syntax) | พิสูจน์บน LAB VM (issue #50) |
| Config write + validate + restart | temp file → `dnsmasq --test` → write จริง → restart ผ่าน D-Bus (`RestartServiceViaDBus`) — pattern ใช้ต่อได้เลย | `dns_server.go:~150-175` |
| Mock | `MockDNSServerManager.ApplyZones` log อย่างเดียว — ปลอดภัยอยู่แล้ว | `backend/internal/kernel/mock.go:~331-334` |
| Wiring | main เลือก real/mock อยู่แล้ว — ไม่ต้องแก้ | `backend/cmd/pigate/main.go:~94-126` |
| Handler apply | `POST /api/dns/apply` → `ApplyAll` → `ApplyZones` + `SyncFirewallRules` — โครงเดิมพอ | `backend/internal/api/handlers.go:~2595-2607` |
| Firewall port 53 | เปิดตาม Listen Interfaces อยู่แล้ว — ไม่ต้องแก้ | `backend/internal/kernel/real_firewall.go:~987-1017` |
| Validator ชื่อ interface | **ยังไม่มี**ใน `model` (มีแต่ zone/record/reservation) — ต้องเพิ่ม (defense-in-depth จุด gen) | `backend/internal/model/dns_validate.go:42,64,138,153` |
| Import path | restore เขียนชื่อ interface ลง DB **ตรง ๆ ไม่ validate** — เหตุผลที่ต้องมี defense-in-depth ที่ gen | `backend/internal/db/backup_repo.go:~228-230` |
| Frontend wording | ยังใช้ "Auth"/"Authoritative"/"NS/auth-server" หลายจุด | `frontend/src/pages/DnsServer.tsx:~419` (description), `~543-551` (badge Auth/Fwd), `~615-633`, `~781` (dialog toggle) |
| OpenAPI | zone schema/description ยังไม่กล่าวถึง local-zone semantics — เช็คตอนแก้ | `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync กัน) |

**สรุป:** งานกระจุกที่ `kernel/` (base config ร่วม + emit `interface=`) และ wording ใน
`DnsServer.tsx` — service/db/handler/firewall/wiring ไม่ต้องแตะเลย

## 2. Technical Approach

**กลไก:** เปลี่ยน base config เป็น `bind-dynamic` แล้ว emit `interface=<name>` ต่อ
Listen Interface ลง `pigate-dns.conf` — ผ่านการพิสูจน์ 4 ข้อบน LAB VM แล้ว (issue #50):
ชื่อไม่มีจริงไม่ fatal / interface มาทีหลัง bind เอง / ถอดออกไม่ตาย / ไม่ชน systemd-resolved

```
# pigate-base.conf (ใหม่)          # pigate-dns.conf (เพิ่มส่วนนี้)
bind-dynamic                        # DNS listen interfaces (from DNS Server settings)
                                    interface=eth0.301
                                    interface=wlan1
```

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
- *`listen-address=<ip>`* — เจาะจงกว่า แต่ stale เมื่อ IP เปลี่ยน (ต้อง regen config ไม่ใช่แค่
  restart), ต้อง resolve IP สดตอน gen (interface โหมด dhcp ไม่มี IP ใน DB), และมี boot race
  → เลื่อนไปเฟส event bus (#48)
- *คง `bind-interfaces` + emit เฉพาะ interface ที่มีจริง (skip แบบ DHCP)* — ใช้ได้แต่ไม่
  self-heal: VLAN กลับมาต้องรอคนกด apply; และ address ใหม่บน interface เดิมไม่ถูก bind
  จนกว่าจะ restart — `bind-dynamic` แก้ทั้งสองอย่างฟรี
- *emit ทั้ง interface= และ listen-address=* — ซ้ำซ้อน เพิ่ม failure mode โดยไม่ได้อะไรเพิ่ม

**Defense-in-depth:** เพิ่ม `ValidateInterfaceName` ใน `model/dns_validate.go`
(`^[A-Za-z0-9._-]{1,15}$` — ตาม IFNAMSIZ) เรียกที่จุด gen ก่อน emit ทุกชื่อ
(skip + log ถ้าไม่ผ่าน) — จำเป็นเพราะ import path เขียน DB ตรงโดยไม่ validate
(pattern เดียวกับ `ValidateDNSZone` ที่ `dns_server.go:~74`)

**Template:** โครง write-validate-restart เดิมใน `ApplyZones` (`dns_server.go:~150-175`)
และ style ของ `ensureBaseConfig` เดิม

## 3. Steps (เรียงชั้นใน → นอก)

### Step 1 — kernel: แยก base config เป็น helper ร่วม + เปลี่ยนเป็น bind-dynamic
**File:** `backend/internal/kernel/dnsmasq_base.go` (**ไฟล์ใหม่**) + `dhcp_server.go:~32-49,~65`
- ย้ายเนื้อ `ensureBaseConfig` ไปเป็น package-level `ensureDnsmasqBaseConfig()` ในไฟล์ใหม่
  เปลี่ยน `bind-interfaces` → `bind-dynamic` (คง `domain`/`expand-hosts`/`dhcp-authoritative`)
- `RealDhcpManager.ApplyConfig` เรียก helper ใหม่ (ลบ method เดิม)

### Step 2 — kernel: DNS apply ensure base + emit interface=
**File:** `backend/internal/kernel/dns_server.go:~35-60`
- ต้นทาง `ApplyZones`: เรียก `ensureDnsmasqBaseConfig()` ก่อนเสมอ (ปิด upgrade/ordering
  trap — ดู Caution 1)
- หลังส่วน upstream servers: emit `interface=<name>` ต่อชื่อใน `interfaces` โดย**ไม่ skip
  ชื่อที่ไม่มีจริง** (bind-dynamic ทนได้ = self-healing) แต่ผ่าน `ValidateInterfaceName`
  ก่อนทุกชื่อ (skip + log ถ้าไม่ผ่าน)

### Step 3 — model: validator + test
**Files:** `backend/internal/model/dns_validate.go` (+ `dns_validate_test.go` ถ้ามีไฟล์ test เดิม — ยังไม่ได้เช็คว่ามี)
- เพิ่ม `ValidateInterfaceName(name string) error` ตาม §2 + test case (ชื่อปกติ/ยาวเกิน/มี newline/ว่าง)

> **ไม่ต้องแก้** `interfaces.go` (signature `ApplyZones` รับ `interfaces` อยู่แล้ว),
> `mock.go` (log อยู่แล้ว ปลอดภัย), `service/dns_server.go` (ส่ง interfaces อยู่แล้ว),
> `main.go`, `install.sh` (ไม่มี permission ใหม่ — เขียนไฟล์ `/etc/dnsmasq.d` ทำอยู่แล้ว)

### Step 4 — kernel tests (ทำได้เท่าที่โครงเอื้อ)
**File:** `backend/internal/kernel/dns_server_test.go` (ตรวจก่อนว่ามีไฟล์เดิมไหม)
- ถ้า config-builder ยัง inline ใน `ApplyZones` (เขียน `/etc/dnsmasq.d` ตรง) →
  แยกส่วนประกอบ string เป็น pure function `buildDNSConfig(zones, interfaces, upstreams) string`
  แล้ว test ว่า: มี `interface=` ครบ, ชื่อ invalid ถูก skip, ไม่มี interfaces = ไม่มีบรรทัด interface=

### Step 5 — frontend: wording Auth → Local zone
**File:** `frontend/src/pages/DnsServer.tsx:~419,~543-551,~615-633,~781`
- Badge "Auth"→"Local", "Fwd" คงไว้; ข้อความอธิบาย "โซน Authoritative..." → "Local zone
  (ตอบจาก records ในระบบ, ชื่อที่ไม่รู้จักตอบ NXDOMAIN)"; description `:~419` เลิกพูดถึง
  "NS/auth-server" → อธิบายว่าเป็น interface ที่ dnsmasq ฟัง DNS
- **ไม่แตะ** state/field `isAuthoritative` และ `mockData.ts` structure (API contract เดิม)

### Step 6 — docs
**Files:** `docs/openapi.yaml` + `frontend/public/openapi.yaml` (sync ทั้งคู่), `docs/ref/complete/dnsmasq-design.md`
- openapi: อัปเดต description ของ zone (`isAuthoritative` = local zone semantics) และ
  `/dns/settings` (Listen Interfaces มีผลต่อการ listen จริงแล้ว ไม่ใช่แค่ firewall)
- dnsmasq-design: บันทึก bind-dynamic + เหตุผล (ย้ายเนื้อจากแผนนี้ตอนปิดงาน)

## 4. Related API

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| POST | `/api/dns/apply` | authRoute (mutation → super_admin) | เดิม — แต่ตอนนี้เขียน base config + `interface=` ด้วย |
| PUT | `/api/dns/settings` | authRoute (mutation → super_admin) | เดิม (semantics ตามงาน #46) — apply แล้วมีผล listen จริง |

ไม่มี route ใหม่; `-disable-edit` block mutations อยู่แล้ว — ถูกต้องสำหรับงานนี้

## 5. Cautions

1. **Upgrade/ordering trap (สำคัญสุด)**: เครื่องที่ติดตั้งอยู่แล้วมี base config เป็น
   `bind-interfaces` — ถ้า binary ใหม่ apply DNS (เขียน `interface=` ที่อาจมีชื่อค้าง)
   โดย base ยังเก่า → dnsmasq **ล้มทั้งตัว รวม DHCP** (พิสูจน์แล้วใน LAB) → ป้องกันโดย
   Step 2 ให้ `ApplyZones` เรียก `ensureDnsmasqBaseConfig()` ก่อน**เสมอ** ทุก apply
   (boot ก็ปลอดภัยเพราะลำดับ startup: DHCP `InitApplyConfig` (เขียน base ใหม่) → DNS)
2. **อย่าหวังพึ่ง `dnsmasq --test`**: มัน**จับ unknown interface ไม่ได้** (`syntax check
   OK` แล้วค่อยล้มตอน start จริง — หลักฐานใน issue #50) — ความปลอดภัยมาจาก bind-dynamic
   ไม่ใช่จาก validation ก่อนเขียน
3. **Import path bypass**: ชื่อ interface จาก restore เข้า DB โดยไม่ validate
   (`backup_repo.go:~228`) — ชื่อที่มี newline สามารถ inject directive ลง config ได้
   ถ้าไม่มี `ValidateInterfaceName` ที่จุด gen (Step 2/3) — ห้ามตัด validator ออก
   ด้วยเหตุผลว่า "handler เช็คแล้ว"
4. **bind-dynamic กระทบ dnsmasq ทั้งโปรเซส (รวม DHCP)**: LAB รอบแรกยังไม่ได้ทดสอบ
   DHCP lease renew — ตอน implement เสร็จต้องทดสอบบนบอร์ด/VM จริง: client ต่อใหม่
   ได้ lease ปกติ + `cat /var/lib/misc/dnsmasq.leases` มี entry ใหม่ ก่อน merge
5. **ห้าม skip ชื่อที่ไม่มีจริงตอน emit** (ต่างจากฝั่ง DHCP): การ skip = ต้องมีคน re-apply
   ตอน interface กลับมา — ขัด self-healing principle; bind-dynamic ออกแบบมาให้ค้างชื่อ
   ไว้ใน config ได้ปลอดภัย ส่วนฝั่ง DHCP **คง skip เดิมไว้** (ลด regression — ดู Out of scope)
6. **ไม่มี `interface=` เลยทั้งสองไฟล์ = dnsmasq ฟังทุก interface (รวม WAN)**: เป็น
   พฤติกรรม dnsmasq เดิมที่มีอยู่แล้วก่อนงานนี้ — วันนี้ถูกกันโดย firewall input chain
   (default drop, เปิด 53 เฉพาะ interface ที่ตั้ง) จึง*ยอมรับได้เท่าเดิม* แต่บันทึกไว้:
   ถ้าอนาคตอยาก hard-guard เพิ่ม ให้พิจารณา `except-interface=<WAN>` เป็นงานแยก
7. **ทำหลัง #46 merge เท่านั้น**: แตะ `DnsServer.tsx` ชุดเดียวกัน (#46 เพิ่ม missing
   chips ใน CardContent เดียวกับ description ที่ Step 5 แก้) — เริ่มก่อน = conflict แน่นอน
8. **ทดสอบบนเครื่องจริง**: mock mode ครอบได้แค่ flow UI/handler (mock `ApplyZones`
   ไม่เขียนไฟล์) — ผล config จริง + bind-dynamic ต้องดูบน VM/บอร์ด; ความเสี่ยง lock-out
   ต่ำ (ไม่แตะ routing/ssh) แต่ DNS/DHCP ของ LAN จะสะดุดช่วง restart dnsmasq — ทดสอบ
   นอกเวลาที่มีคนใช้เครือข่าย LAB

## 6. Summary Checklist (Definition of Done)

- [ ] `kernel/dnsmasq_base.go` (ใหม่) — `ensureDnsmasqBaseConfig()` + `bind-dynamic`
- [ ] `kernel/dhcp_server.go` — ใช้ helper ร่วม (ลบ `ensureBaseConfig` เดิม)
- [ ] `kernel/dns_server.go` — ensure base ใน `ApplyZones` + emit `interface=` ผ่าน validator
- [ ] `model/dns_validate.go` — `ValidateInterfaceName` + tests
- [ ] `kernel/dns_server_test.go` — test config builder (แยก pure function ถ้าจำเป็น)
- [ ] `pages/DnsServer.tsx` — wording Auth → Local zone (field เดิมคงไว้)
- [ ] `docs/openapi.yaml` + `frontend/public/openapi.yaml` — sync description
- [ ] `docs/ref/complete/dnsmasq-design.md` — บันทึก bind-dynamic design ตอนปิดงาน
- [ ] `go build ./...` + `go test ./...` ผ่าน (ใน `backend/`)
- [ ] `yarn build` + `yarn lint` ผ่าน (ใน `frontend/`)
- [ ] ทดสอบ VM/บอร์ดจริง: ปิด DHCP ทุก interface → ตั้ง Listen Interfaces → `dig` ตอบ
- [ ] ทดสอบ VM/บอร์ดจริง: Listen Interface ที่ไม่มีจริง → dnsmasq active; สร้าง interface
      กลับมา → bind เอง ไม่ต้อง re-apply
- [ ] **Regression DHCP ภายใต้ bind-dynamic**: client renew lease ได้ปกติ (Caution 4)
- [ ] ทดสอบ upgrade path: เครื่องที่ base เก่า (`bind-interfaces`) → apply DNS จาก binary
      ใหม่ → base ถูกเขียนใหม่ก่อน ไม่มีช่วง dnsmasq ล้ม (Caution 1)
- [ ] เสร็จแล้วย้ายไฟล์นี้ไป `docs/ref/complete/` + ปิด issue #50
