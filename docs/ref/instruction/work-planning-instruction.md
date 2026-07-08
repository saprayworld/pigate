# Instruction: วิธีเขียนแผนงาน (Work Plan) สำหรับโปรเจกต์ PiGate

> คู่มือมาตรฐานสำหรับ **คนและ AI** ในการเขียนเอกสารแผนงานลง `docs/ref/todo/`
> ก่อนลงมือทำฟีเจอร์/แก้ไขใด ๆ เป้าหมายคือให้แผนงานทุกชิ้น
> **ครอบคลุมผลกระทบทั้งระบบ** และมีโครงสร้างเดียวกัน อ่านต่อกันได้ทุกฉบับ
>
> ตัวอย่างแผนงานที่เขียนตามมาตรฐานนี้:
> `docs/ref/complete/power-control-device-plan.md`,
> `docs/ref/complete/sidebar-dynamic-hostname-plan.md`

---

## 0. หลักการสำคัญ 3 ข้อ (อ่านก่อนเริ่มทุกครั้ง)

1. **สำรวจโค้ดจริงก่อนเขียนแผนเสมอ — ห้ามเขียนจากความจำหรือจากเอกสาร**
   ตาราง Feature Status ใน README และเอกสารใน `docs/` อาจ drift จากโค้ดจริง
   (CLAUDE.md ก็เตือนไว้) สถานะทุกอย่างที่อ้างในแผนต้องมาจากการเปิดไฟล์ดู
   ณ วันที่เขียน เช่น กรณี Power Control: README บอกว่า "Mock" แต่พอสำรวจจริง
   พบว่า frontend เสร็จหมดแล้ว, route มีแล้ว, ขาดแค่ backend handler → ขอบเขตงาน
   จริงเล็กกว่าที่เอกสารบอกมาก ถ้าไม่สำรวจก่อนจะวางแผนทำงานซ้ำของที่มีอยู่

2. **แผนต้องระบุ "ที่ไหน" แบบชี้ได้จริง** — ทุกขั้นตอนต้องบอกไฟล์เต็ม path
   และเลขบรรทัดโดยประมาณ (เช่น `backend/internal/api/handlers.go:1656`)
   พร้อมโค้ดตัวอย่างสั้น ๆ เมื่อช่วยให้เห็นภาพ คนอ่านแผนต้องเปิดไฟล์ตามแล้ว
   เจอจุดที่พูดถึงทันที โดยไม่ต้องค้นเอง

3. **แผนที่ดีต้องบอกทั้ง "ทำอะไร" และ "ห้าม/ระวังอะไร"** — ส่วนข้อควรระวัง
   (ผลกระทบข้างเคียง, constraint ของโปรเจกต์, กับดักที่เจอระหว่างสำรวจ)
   มีค่าเท่ากับตัวขั้นตอน เพราะเป็นสิ่งที่ป้องกันความเสียหายจริง

---

## 1. เมื่อไหร่ต้องเขียนแผนงาน

- ฟีเจอร์ใหม่ หรือเปลี่ยนของ Mock → Real ทุกกรณี
- งานที่แตะมากกว่า 1 layer (frontend + backend, หรือ backend ข้าม layer)
- งานที่แตะจุดอ่อนไหว: auth/role, firewall rule generation, D-Bus/Netlink,
  `install.sh`/Polkit, การ migrate เครื่องที่ติดตั้งไปแล้ว
- งานเล็กมาก (แก้ typo, ปรับ style จุดเดียว) ไม่ต้องเขียนแผน — ใช้วิจารณญาณ
  แต่ถ้าลังเลให้เขียน

**ที่เก็บ:** `docs/ref/todo/<ชื่อฟีเจอร์แบบ kebab-case>-plan.md`
(ไฟล์ละหนึ่งงาน ชื่อไฟล์เป็นภาษาอังกฤษ เนื้อหาภาษาไทยปนศัพท์เทคนิคอังกฤษ)

---

## 2. ขั้นตอนการวางแผน (ทำตามลำดับ)

### Phase A — ตีโจทย์และขีดขอบเขต

ตอบคำถามเหล่านี้ให้ได้ก่อนแตะโค้ด:

- งานนี้ "สำเร็จ" หน้าตาเป็นอย่างไร (ผู้ใช้กดอะไร แล้วเกิดอะไรจริง)
- อะไร **อยู่นอกขอบเขต** — เขียนออกมาตรง ๆ เสมอ (สำคัญเท่ากับขอบเขต)
  เช่น แผน Power Control ตัด "Power-on ระยะไกล" และ "System Services panel"
  ออกชัดเจน เพื่อกันแผนบวม
- งานนี้ชนกับ constraint หลักของโปรเจกต์ข้อไหนบ้าง (ดู §4 checklist)

### Phase B — สำรวจสถานะปัจจุบันของโค้ด (สำคัญที่สุด)

ใช้ grep/ค้นหา keyword ของฟีเจอร์ให้ครบ **ทุกชั้น** แล้วเปิดไฟล์ที่เจอมาอ่านจริง
เส้นทางที่ต้องไล่เช็คสำหรับ PiGate (เรียงตาม flow ของ request):

| ลำดับ | ชั้น | ที่ต้องดู |
|---|---|---|
| 1 | Frontend UI | `frontend/src/pages/`, `frontend/src/components/`, `frontend/src/hooks/` — มี UI แล้วหรือยัง? เรียกจากกี่ที่? ใช้ hook ร่วมกันไหม? |
| 2 | Frontend API client | `frontend/src/services/*.ts` — เรียก endpoint ไหน method อะไร |
| 3 | Route + middleware | `backend/internal/api/router.go` — เส้นมีหรือยัง? ใช้ `authRoute` / `superAdminRoute`? (ดูผลของ `RoleReadOnlyMiddleware` ใน `middleware.go` ประกอบ) |
| 4 | Handler | `backend/internal/api/handlers.go` — เป็นของจริงหรือ stub ตอบ 200 เฉย ๆ |
| 5 | Service layer | `backend/internal/service/` — มี service รองรับหรือยัง |
| 6 | Kernel layer | `backend/internal/kernel/interfaces.go` — มี interface หรือยัง? มีครบทั้ง `real_*.go` และ `mock.go` ไหม |
| 7 | DB | `backend/internal/db/` — ต้องมีตาราง/migration ใหม่ไหม (จำไว้: runtime state ไม่ persist ลง SQLite — ลด SD card wear) |
| 8 | Wiring | `backend/cmd/pigate/main.go` — manager ถูกเลือก real/mock ตรงไหน, service ถูกสร้างและส่งเข้า `api.NewServer` ตรงไหน, ต้อง apply config ตอน boot ไหม |
| 9 | ติดตั้ง/สิทธิ์ | `install.sh` — Polkit rules, sudoers, systemd unit, user `pigate` — งานนี้ต้องการสิทธิ์อะไรเพิ่ม |
| 10 | เอกสาร/สัญญา | `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` (ต้อง sync กัน), README Feature Status, `docs/ref/*` ที่เกี่ยว |

ผลลัพธ์ของ Phase นี้คือ **ตาราง "สถานะปัจจุบัน"** ในแผน: แต่ละส่วนเสร็จแล้ว /
เป็น stub / ยังไม่มี พร้อมอ้างไฟล์:บรรทัด — ตารางนี้คือหลักฐานว่าสำรวจจริง
และเป็นตัวกำหนดว่าขั้นตอนในแผนมีอะไรบ้าง

### Phase C — เลือกแนวทางเทคนิค พร้อมเหตุผลและทางเลือกที่ตัดทิ้ง

- ระบุกลไกที่จะใช้ให้เฉพาะเจาะจง (เช่น D-Bus destination + method ไหน,
  Netlink อะไร, library ตัวไหนที่มีอยู่แล้วใน `go.sum`)
- เขียน **เหตุผลที่เลือก** และ **ทางเลือกอื่นที่พิจารณาแล้วตัดทิ้ง เพราะอะไร**
  (เช่น เลือก `login1.PowerOff` แทน `systemd1.StartUnit("poweroff.target")`
  เพราะ Polkit action แคบกว่าและสื่อความหมายตรงกว่า) — ส่วนนี้กันคนมาทำทีหลัง
  ย้อนถามหรือเปลี่ยนแนวทางโดยไม่รู้ว่าเคยคิดมาแล้ว
- แนวทางต้องผ่าน constraint ใน §4 ทุกข้อ ถ้าไม่ผ่านให้กลับไปคิดใหม่
  ไม่ใช่จดเป็นข้อยกเว้น
- ยึด pattern ที่มีอยู่แล้วในโค้ดเป็นแม่แบบ และอ้างชื่อไฟล์แม่แบบไว้ในแผน
  (เช่น "ทำตามสไตล์ `real_hostname.go`") เพื่อให้ของใหม่หน้าตาเหมือนของเดิม

### Phase D — เขียนขั้นตอนการทำ (Step-by-step)

- เรียงลำดับตาม **ทิศทาง dependency**: ชั้นในสุดก่อน → ออกมาชั้นนอก
  สำหรับ backend PiGate คือ: `kernel/interfaces.go` → `real_*.go` → `mock.go`
  → `service/` → `main.go` wiring → `api/handlers.go` → `router.go` →
  `install.sh` → เอกสาร → frontend (ถ้ามี)
- แต่ละ step ต้องมี: **ชื่อไฟล์ (ใหม่หรือแก้), ตำแหน่งโดยประมาณ,
  สิ่งที่ต้องทำ, โค้ดตัวอย่างถ้าช่วยให้ชัด**
- step ไหนเป็น optional/polish ให้ระบุว่า optional ชัด ๆ และวางไว้ท้ายสุด
- ระบุด้วยว่างานนี้ **ไม่ต้อง** ทำอะไรที่ pattern ปกติมักทำ พร้อมเหตุผล
  (เช่น "ไม่ต้อง `InitApplyConfig()` เพราะไม่มี state ให้ apply ตอน boot")
  เพื่อกันคนทำเผื่อเกินโดยไม่จำเป็น

### Phase E — วิเคราะห์ผลกระทบและเขียนข้อควรระวัง

ไล่ checklist ผลกระทบใน §5 ทีละข้อ ข้อไหนเกี่ยวให้เขียนลงแผนเป็นข้อ ๆ
โดยแต่ละข้อบอก: **อะไรจะพัง / พังอย่างไร / กันอย่างไร** ไม่ใช่แค่ "ระวัง X"
เฉย ๆ เช่น อย่าเขียนว่า "ระวังเรื่อง response" แต่เขียนว่า
"ถ้าเรียก D-Bus ใน handler ตรง ๆ logind อาจ stop service ก่อน response ถึง
browser → frontend เข้า branch error ทั้งที่สำเร็จ → แก้ด้วย `time.AfterFunc`"

กับดักที่ **ค้นพบระหว่างสำรวจ** (Phase B) ต้องถูกจดลงส่วนนี้เสมอ —
เช่นการเจอว่า Polkit rule เดิมมี catch-all `return polkit.Result.NO`
ซึ่งทำให้การเพิ่ม rule ผิดตำแหน่งล้มเหลวเงียบ ๆ ของแบบนี้หาไม่เจอจากเอกสาร
เจอได้จากการอ่านโค้ดเท่านั้น และมีค่ามากที่สุดในแผน

### Phase F — สรุปเป็น Checklist (Definition of Done)

- แปลงทุก step เป็น checkbox `- [ ]` หนึ่งบรรทัดต่อหนึ่งไฟล์/งาน
- ต้องมีข้อของ **การทดสอบ** เสมอ: ทดสอบใน mock mode อย่างไร,
  ทดสอบบนอุปกรณ์จริงอย่างไร (และเงื่อนไขความปลอดภัยในการทดสอบ),
  ทดสอบ role/สิทธิ์อย่างไร
- ต้องมีข้อของ **การอัปเดตเอกสาร**: openapi.yaml (สองไฟล์), README
  Feature Status, docs ที่เกี่ยวข้อง

---

## 3. โครงสร้างเอกสารแผนงาน (Template)

ทุกแผนใช้โครงนี้ ตัด section ที่ไม่เกี่ยวได้ แต่ห้ามสลับลำดับ:

```markdown
# <ชื่อฟีเจอร์> — <คำอธิบายสั้นหนึ่งบรรทัด>

> เอกสารแผนงานสำหรับฟีเจอร์: <ขยายความ 1-3 บรรทัด ว่าเปลี่ยนจากอะไรเป็นอะไร>
>
> วันที่เขียน: YYYY-MM-DD · Branch อ้างอิง: `<branch>`
> สถานะใน README Feature Status: <ค่าปัจจุบัน> → เป้าหมายคือ <ค่าใหม่>   (ถ้ามี)

## 0. เป้าหมายและขอบเขต
   - เป้าหมาย (พฤติกรรมที่ผู้ใช้เห็น + เงื่อนไขเชิงเทคนิคที่ต้องเป็นจริง)
   - **นอกขอบเขต:** ระบุชัดเจนเสมอ

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)
   - ตาราง: ส่วน | สถานะ (เสร็จแล้ว / stub / ยังไม่มี) พร้อมไฟล์:บรรทัด
   - ปิดท้ายด้วยสรุปหนึ่งบรรทัด ว่างานจริงกระจุกอยู่ตรงไหน

## 2. แนวทางเทคนิค
   - กลไกที่เลือก + โค้ดตัวอย่างสั้น ๆ
   - เหตุผลที่เลือก และทางเลือกที่ตัดทิ้งเพราะอะไร
   - pattern/ไฟล์แม่แบบในโค้ดเดิมที่ให้ทำตาม

## 3. ขั้นตอนการทำ (เรียงลำดับ + ไฟล์ที่ต้องแก้)
   - Step 1..N: หัวข้อ + **ไฟล์:** path (ระบุ "ไฟล์ใหม่" ถ้าสร้างใหม่)
   - สิ่งที่ "ไม่ต้องทำ" พร้อมเหตุผล ใส่เป็น blockquote ใน step ที่เกี่ยว

## 4. API ที่เกี่ยวข้อง
   - ตาราง: Method | Path | ใครเรียกได้ (role) | พฤติกรรม
   - ระบุว่าเส้นใหม่หรือเส้นเดิม และผลของโหมด -disable-edit

## 5. ข้อควรระวัง
   - ข้อละหนึ่งประเด็น: อะไรพัง / พังอย่างไร / กันอย่างไร
   - รวมข้อกำหนดการทดสอบที่มีเงื่อนไขความปลอดภัย

## 6. Checklist สรุป (Definition of Done)
   - checkbox ครบทุกไฟล์ + ทดสอบ + เอกสาร
```

---

## 4. Constraint ประจำโปรเจกต์ที่แผนทุกฉบับต้องเช็ค

แนวทางเทคนิค (Phase C) ต้องไม่ขัดข้อใดข้อหนึ่งต่อไปนี้
(รายละเอียดเต็มอยู่ใน `docs/tech_stack_design.md` และ `CLAUDE.md`):

- [ ] **ห้าม shell execution** (`exec.Command`) สำหรับงานที่มีทาง Netlink/D-Bus —
      นี่คือแนวป้องกัน command-injection หลักของโปรเจกต์
- [ ] **รันด้วย user `pigate` + capabilities** (`cap_net_admin,cap_net_raw`)
      ไม่ใช่ root — ถ้างานต้องการสิทธิ์นอกเหนือจากนี้ ทางออกคือ Polkit rule
      หรือ sudoers เฉพาะจุดใน `install.sh` ไม่ใช่การ assume root
- [ ] **kernel layer เท่านั้นที่แตะ OS** — เพิ่มความสามารถใหม่ต้องผ่าน
      interface ใน `interfaces.go` และ implement **ทั้ง real และ mock เสมอ**
- [ ] **mock mode ปลอดภัย 100%** — dev รัน `-mock=true` บนเครื่องทำงานจริง
      โค้ด mock ห้ามมี side effect ต่อระบบปฏิบัติการเด็ดขาด
- [ ] **ถนอม SD card** — runtime/ephemeral state อยู่ใน RAM (ring buffer,
      อ่านสดจาก kernel) ไม่เขียนถี่ ๆ ลง SQLite
- [ ] **firewall input chain 4 section** — ถ้าแตะ firewall rule generation
      ต้องรักษาลำดับ: sanity/drop → audit log → dynamic accept (+Docker compat)
      → final drop-and-log
- [ ] **Wi-Fi ผ่าน wpa_supplicant โดยตรง** (config file + control socket)
      ไม่ใช่ NetworkManager — อ่าน `docs/wifi_wpa_working_instruction.md` ก่อน
- [ ] **Frontend:** shadcn/ui เท่านั้น, สี semantic variables (ห้าม hardcode
      palette), flat design (ห้าม `shadow-*`/`backdrop-blur-*`), รองรับ
      dark/light, Dialog ที่มี portal component ใช้ `modal={false}`
      **เฉพาะเมื่อ Dialog นั้นมี input ฟิลด์แบบ Combobox เท่านั้น** —
      ดู `docs/rules_of_work.md`
- [ ] **dependency ใหม่** — หลีกเลี่ยง ถ้าจำเป็นให้เลือก stdlib /
      `golang.org/x` / โมดูลที่มีอยู่แล้วก่อนเสมอ

---

## 5. Checklist วิเคราะห์ผลกระทบ (Impact Analysis)

ไล่ตอบทุกข้อตอน Phase E — ข้อไหน "เกี่ยว" ต้องปรากฏในแผน
(ในขั้นตอนหรือในข้อควรระวัง) ข้อไหน "ไม่เกี่ยว" ไม่ต้องเขียนลงแผน:

**สิทธิ์และความปลอดภัย**
- [ ] Endpoint ใหม่/เดิมใช้ role ไหน — `authRoute` (POST ถูกบล็อกสำหรับ
      non-super_admin โดย `RoleReadOnlyMiddleware` อยู่แล้ว) หรือควรเป็น
      `superAdminRoute` แบบ explicit? มีข้อมูล sensitive รั่วผ่าน GET ได้ไหม?
- [ ] โหมด `-disable-edit=true` ควรบล็อกงานนี้ไหม (DisableEditMiddleware
      บล็อก mutation ทั้งระบบอยู่แล้ว — ยืนยันว่าพฤติกรรมนั้นถูกต้องกับงานนี้)
- [ ] Input จากผู้ใช้ถูก validate ที่ไหน — มีทางที่ค่าหลุดไปประกอบเป็นอะไร
      อันตราย (ไฟล์ config, D-Bus argument, nft rule) ไหม
- [ ] ต้องแก้ Polkit/sudoers ใน `install.sh` ไหม → ถ้าใช่:
      **เครื่องที่ติดตั้งไปแล้วต้อง migrate อย่างไร** (รัน install.sh ซ้ำ /
      แก้มือ) — ต้องจดลง release note

**สถาปัตยกรรม backend**
- [ ] แตะ routing/interface ไหม → `netlink_monitor.go` ต้องรับรู้หรือ
      reconcile ของใหม่นี้ไหม
- [ ] ต้อง apply state ตอน boot ไหม → ตามลำดับ startup ใน `main.go`
      (interfaces → routes → monitor → DHCP → DNS → firewall → QoS)
      ของใหม่ควรแทรกตรงไหน / หรือระบุว่าไม่ต้อง
- [ ] มี schema/migration ใหม่ใน `db/` ไหม → ข้อมูลเก่าของผู้ใช้จะเป็นอย่างไร
- [ ] Backup/Restore (`service/backup.go`, schema v2) ต้องรวม config
      ใหม่นี้ไหม
- [ ] ลำดับ timing ของ HTTP response กับ side effect — มีกรณีที่ side effect
      ฆ่า/บล็อก process ก่อน response ออกไหม (เช่น reboot, restart service
      ที่ pigate พึ่งพา)

**Frontend**
- [ ] UI จุดเดียวหรือหลายจุดเรียกใช้ของเดียวกัน — logic ควรอยู่ใน hook/
      service ที่แชร์กัน แก้ที่เดียว (เช่น `usePowerControl` ถูกใช้ทั้งใน
      Settings และ nav-user)
- [ ] Mock mode ฝั่ง frontend (`services/config.ts`, `mockSync.ts`)
      ต้องรองรับไหม
- [ ] มีสถานะพิเศษที่ backend หายไปชั่วคราว (reboot, restart service) —
      frontend ต้อง handle connection หลุด/poll กลับมาไหม

**เอกสารและ contract**
- [ ] `docs/openapi.yaml` และ `frontend/public/openapi.yaml` — sync ทั้งคู่
- [ ] README Feature Status ต้องอัปเดตไหม
- [ ] `docs/ref/*` design doc ของ subsystem ที่แตะ ต้องอัปเดตไหม

**การทดสอบ**
- [ ] ทดสอบใน mock mode ครอบคลุม flow ไหนได้บ้าง / อะไรทดสอบได้บนบอร์ดจริง
      เท่านั้น
- [ ] การทดสอบบนบอร์ดจริงมีความเสี่ยงล็อกตัวเองออกจากเครื่องไหม
      (network, firewall, power) → กำหนดเงื่อนไข เช่น "ทดสอบเฉพาะตอน
      เข้าถึงตัวเครื่องได้" และลำดับที่ปลอดภัย (เช่น reboot ก่อน shutdown)
- [ ] `go build ./...` + `go test ./...` และ `yarn build` + `yarn lint`
      ผ่านหลังแก้

---

## 6. ข้อกำหนดการเขียน (Style)

- **ภาษา:** เนื้อหาภาษาไทย ศัพท์เทคนิค/ชื่อไฟล์/โค้ดเป็นภาษาอังกฤษ
  หัวไฟล์เป็น `#` เดียว + blockquote สรุป + วันที่เขียน + branch อ้างอิง
- **อ้างอิงตำแหน่ง:** ใช้ path จาก root ของ repo + เลขบรรทัดโดยประมาณ
  พร้อมหมายเหตุ `~` ถ้าไม่เป๊ะ (เลขบรรทัดจะ drift ได้ — path กับชื่อ
  function สำคัญกว่า)
- **โค้ดตัวอย่าง:** ใส่เฉพาะที่ช่วยให้ step ชัดขึ้น สั้นที่สุดที่ยังสื่อ
  ไม่ต้องเขียน implementation เต็ม — แผนไม่ใช่ PR
- **ความยาว:** แผนควรจบใน ~150-250 บรรทัด ถ้ายาวกว่านั้นแปลว่างานใหญ่เกิน
  ควรแตกเป็นหลายแผน/หลายเฟส
- **ความซื่อสัตย์ของข้อมูล:** ทุก "สถานะปัจจุบัน" ต้องมาจากการเปิดไฟล์ดูจริง
  ณ วันเขียน ถ้าไม่ได้ตรวจส่วนไหนให้เขียนว่า "ยังไม่ได้ตรวจ" ห้ามเดา
- **การอัปเดตแผน:** ถ้าลงมือทำแล้วพบว่าแผนผิด/สถานการณ์เปลี่ยน ให้แก้ไฟล์แผน
  ให้ตรงความจริงด้วย (แผนใน `todo/` คือ living document จนกว่างานจะเสร็จ
  เมื่อเสร็จแล้วเนื้อหาเชิง design ที่ควรอยู่ยาวให้ย้าย/สรุปเข้า
  `docs/ref/<subsystem>-design.md` ตามเหมาะสม)
