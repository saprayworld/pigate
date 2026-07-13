# Polkit Rule User Scope Fix — จำกัด `10-pigate-system.rules` ให้มีผลเฉพาะ user `pigate`

> แผนงานแก้บั๊กความปลอดภัย/ความเสถียรใน `install.sh` STEP 3: polkit rule ที่ติดตั้ง
> ปัจจุบันปิดท้ายด้วย catch-all `return polkit.Result.NO` ซึ่ง**ปฏิเสธทุก polkit action
> ของทุก user บนเครื่อง** (ไม่ใช่แค่ pigate) — เช่น user ธรรมดาเรียก `systemctl` /
> mount USB / จัดการ NetworkManager จะโดน deny ทันทีโดยไม่มี auth prompt
> เป้าหมาย: rule ต้อง "ไม่ออกความเห็น" (NOT_HANDLED) กับ user อื่นทั้งหมด
> โดยพฤติกรรมของ pigate เองคงเดิมทุกอย่าง (allowlist + deny-by-default)
>
> เขียนเมื่อ: 2026-07-13 · Reference branch: `fix/polkit-rule-scope`

## 0. Goal and Scope

**Goal (เมื่อเสร็จ):**
- Request จาก user อื่น (root, pi, user desktop, service accounts) **ไม่ถูก rule นี้
  ตัดสินเลย** — ตกไปให้ rule อื่น/default ของ distro ทำงานตามปกติ (auth prompt,
  implicit policy ฯลฯ)
- Request จาก `pigate` พฤติกรรม**เหมือนเดิมทุกประการ**: manage-units เฉพาะ unit ใน
  allowlist = YES, unit อื่น = NO; hostname1/timedate1/login1 action ในลิสต์ = YES;
  action อื่นทั้งหมดของ pigate = NO (least privilege เดิม)
- กัน exception ใน rule: `action.lookup("unit")` อาจเป็น `undefined` (เช่น
  daemon-reload) → ต้อง guard ก่อนเรียก `.indexOf` (exception ทำให้ polkit ทิ้งผล
  ของ rule ทั้งฟังก์ชัน)

**Out of scope (ตัดออกชัดเจน):**
- ไม่แตะรายการ allowlist (unit/action) — ชุดปัจจุบันตรงกับที่ backend ใช้จริงแล้ว (ดู §1)
- ไม่แตะ `docs/setup_guide.md` — ไฟล์นั้นเป็น dev note ของไฟล์คนละตัว
  (`10-pigate-wpa.rules`) ซึ่ง scoped ถูกอยู่แล้ว (ไม่มี catch-all `NO`)
- ไม่ทำ auto-migration ให้เครื่องที่ติดตั้งไปแล้ว — บันทึกวิธี migrate ไว้ใน Cautions
  (รัน `install.sh` ซ้ำ ซึ่ง idempotent อยู่แล้ว)

## 1. Current State (สำรวจโค้ดจริง 2026-07-13)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Polkit rule ใน install.sh | **บั๊ก — catch-all NO ทั้งระบบ** | `install.sh:231-278` — heredoc เขียน `/etc/polkit-1/rules.d/10-pigate-system.rules`; `else { return polkit.Result.NO; }` ที่ `:274-276` ผูกกับ if ก้อนที่สอง จึงจับ request ของ**ทุก user**ที่ไม่เข้าเงื่อนไข |
| โครงสร้าง rule ปัจจุบัน | if(manage-units && pigate){...} ตามด้วย if(hostname1/timedate1/login1 && pigate){YES} else {**NO**} | `install.sh:232-277` |
| จุดที่ backend ใช้ manage-units | Start/Stop/RestartUnit | `backend/internal/kernel/dbus_systemd.go:69-79` — เรียก unit: `wpa_supplicant@*`, `dhcpcd@*`, `systemd-resolved`, `systemd-timesyncd`, `dnsmasq`, (pigate.service จาก UI restart) — ตรง allowlist `install.sh:240-245` ครบ |
| hostname1 | `SetStaticHostname` | `backend/internal/kernel/real_hostname.go:57` — rule มี set-static-hostname + set-hostname (`install.sh:262-263`) |
| timedate1 | `SetTimezone`/`SetNTP`/`SetTime` | `backend/internal/kernel/real_timedate.go:80-…` — rule มีครบ 3 action (`install.sh:264-266`) |
| login1 | `Reboot`/`PowerOff` (+`-multiple-sessions`) | `backend/internal/kernel/interfaces.go:178-183` (PowerManager) — rule มีครบ 4 action (`install.sh:267-270`) |
| `action.lookup("unit")` ไม่มี guard | เสี่ยง exception | `install.sh:235,240` — ถ้า manage-units call ไม่มี field `unit` (เช่น daemon-reload) `unit.indexOf` โยน TypeError → polkit log error และถือว่า rule ไม่ให้ผล |
| จุด restart polkit หลังเขียนไฟล์ | มีแล้ว ใช้ต่อได้ | `install.sh:282` — `systemctl restart polkit` |
| เอกสารที่กล่าวถึง catch-all นี้ | บันทึกเป็น trap ไว้แล้ว (ยืนยันว่าพฤติกรรมนี้มีจริง) | `docs/ref/complete/power-control-device-plan.md:215` และ `docs/ref/instruction/work-planning-instruction-eng.md:135-137` |
| backend / frontend / db / openapi | ไม่เกี่ยว | งานจบใน heredoc ของ `install.sh` ไฟล์เดียว — ไม่มี Go/TS code เปลี่ยน |

สรุป: แก้จุดเดียวคือ heredoc ใน `install.sh` STEP 3 — โครงสร้าง rule ใหม่ต้องเริ่มด้วย
guard `subject.user != "pigate"` แล้วค่อยตัดสินของ pigate แบบ deny-by-default เหมือนเดิม

## 2. Technical Approach

**กลไกที่เลือก: user guard ขึ้นต้นฟังก์ชัน + คง deny-by-default เฉพาะ pigate**

```js
polkit.addRule(function(action, subject) {
    if (subject.user != "pigate") {
        return polkit.Result.NOT_HANDLED;   // user อื่น: ไม่ออกความเห็นเด็ดขาด
    }
    if (action.id == "org.freedesktop.systemd1.manage-units") {
        var unit = action.lookup("unit");
        if (!unit) { return polkit.Result.NO; }        // กัน TypeError
        if (unit.indexOf("wpa_supplicant@") === 0 || /* ...allowlist เดิม... */) {
            return polkit.Result.YES;
        }
        return polkit.Result.NO;
    }
    if (/* hostname1/timedate1/login1 action list เดิม */) {
        return polkit.Result.YES;
    }
    return polkit.Result.NO;   // action อื่นของ pigate: ปฏิเสธ (least privilege เดิม)
});
```

- `polkit.Result.NOT_HANDLED` = rule นี้ไม่ให้ผล → polkit ไปประเมิน rule ถัดไป /
  implicit policy ของ action นั้นตามปกติ — คือพฤติกรรม "เหมือนไม่มีไฟล์นี้อยู่"
  สำหรับ user อื่น
- guard อยู่**บรรทัดแรกสุด**ของฟังก์ชัน เพื่อให้ทุกสาขาที่เหลือการันตีว่าเป็น pigate
  เท่านั้น — คนมาเติม rule ภายหลังจะไม่มีทางพลาดไปตัดสิน user อื่นอีก

**ทางเลือกที่พิจารณาแล้วตัดทิ้ง:**
1. *`return;` (undefined) แทน `NOT_HANDLED`* — ผลเหมือนกัน แต่ `NOT_HANDLED`
   สื่อเจตนาชัดกว่าและกันคนอ่านเข้าใจผิดว่าลืม return
2. *ลบ catch-all `NO` ทิ้งเฉยๆ (ให้ตกท้ายฟังก์ชันเป็น undefined)* — ตัดทิ้ง:
   จะเสีย deny-by-default ของ **pigate เอง** ไปด้วย — pigate ที่ถูก compromise
   จะได้ default ของ distro (บาง action เป็น `yes` สำหรับ active session) แทนที่จะ
   โดนปฏิเสธ; ต้องคง `NO` ไว้แต่เฉพาะหลัง guard
3. *แยกเป็น 2 ไฟล์ rule (allowlist / deny)* — ตัดทิ้ง: เพิ่มความซับซ้อนของ ordering
   ระหว่างไฟล์โดยไม่ได้อะไรเพิ่ม — guard ในไฟล์เดียวอ่านง่ายกว่า

**Pattern ที่ยึด:** โครง heredoc เดิมใน `install.sh` STEP 3 (คง comment ภาษาไทย
สไตล์เดิม อธิบายเหตุผลของ guard + อ้างเหตุการณ์ global-deny ที่แก้)

## 3. Steps

### Step 1 — เขียน rule ใหม่ใน heredoc
**File:** `install.sh:231-278` (STEP 3)
แทนที่เนื้อหา heredoc ทั้งก้อนด้วยโครงตาม §2:
1. guard `subject.user != "pigate"` → `NOT_HANDLED` (พร้อม comment ว่าห้ามคืน NO
   สำหรับ user อื่นเด็ดขาด — เคยทำให้ polkit ทั้งเครื่อง deny มาแล้ว)
2. บล็อก manage-units: เพิ่ม guard `if (!unit) return NO;` ก่อน `.indexOf`;
   allowlist unit **ชุดเดิมเป๊ะ** (`install.sh:240-245`)
3. บล็อก hostname1/timedate1/login1: action list **ชุดเดิมเป๊ะ** (`install.sh:262-270`)
4. ปิดท้าย `return polkit.Result.NO;` — deny-by-default เฉพาะ pigate
> **สิ่งที่ไม่ต้องทำ:** ไม่แก้ `systemctl restart polkit` (`:282`) และ log รอบๆ —
> ใช้ของเดิม; ไม่เพิ่ม unit/action ใหม่ใดๆ ใน allowlist

### Step 2 — ตรวจ syntax
- `bash -n install.sh` (ตรวจ shell)
- แยกเนื้อ JS ใน heredoc ไปผ่าน `node --check` (มี node v22 ใน dev env) —
  ตรวจ parse error ของ JavaScript ก่อนถึงเครื่องจริง

### Step 3 — ทดสอบบนอุปกรณ์จริง (ดูเงื่อนไขความปลอดภัยใน Cautions 4)
รัน `install.sh` ซ้ำ (idempotent — heredoc `cat >` ทับไฟล์เดิม + restart polkit) แล้ว:
- ฝั่ง pigate: ผ่าน UI จริง — save Wi-Fi (restart `wpa_supplicant@`), restart dnsmasq
  (หน้า DHCP/DNS), ตั้ง hostname, ตั้ง timezone → ต้องสำเร็จทุกอัน
- ฝั่ง user อื่น: login เป็น user ธรรมดา (เช่น `pi`) แล้ว
  `busctl call org.freedesktop.login1 /org/freedesktop/login1 org.freedesktop.login1.Manager CanReboot`
  หรือ `pkcheck --action-id org.freedesktop.systemd1.manage-units --process $$` →
  ต้องได้พฤติกรรม default ของ distro (เช่น `auth_admin`/`yes`) **ไม่ใช่ deny เงียบ**
- `journalctl -u polkit -n 50` ต้องไม่มี JS error จากไฟล์ rule

## 4. Related API

| Method | Path | Role | การเปลี่ยนแปลง |
|---|---|---|---|
| — | — | — | ไม่มี API เปลี่ยน — งานอยู่ที่ polkit rule บน host เท่านั้น; ทุกฟีเจอร์ที่พึ่ง D-Bus (Wi-Fi, DNS, DHCP restart, hostname, time, power) ต้อง regression เท่าเดิม |

`-disable-edit` mode: ไม่เกี่ยว — polkit ตัดสินที่ระดับ OS ไม่ใช่ HTTP layer

## 5. Cautions

1. **ห้ามให้สาขาใดของ rule ตอบ NO/YES กับ user อื่นเด็ดขาด** — polkit ประเมิน rule
   ตามลำดับชื่อไฟล์ และ rule แรกที่คืนค่า (ไม่ใช่ NOT_HANDLED/undefined) จะ**จบการ
   ตัดสินทันที**; ไฟล์นี้ชื่อ `10-*` มาก่อน default ทั้งหมด → ความผิดพลาดแบบเดิม
   กลายเป็น global deny ทั้งเครื่อง (อาการ: user ธรรมดา mount USB/`systemctl` ไม่ได้
   โดยไม่มี prompt; root ไม่เห็นอาการเพราะ caller root ส่วนใหญ่ไม่ผ่าน polkit) →
   ป้องกันด้วย guard บรรทัดแรก + comment เตือนในไฟล์
2. **อย่าเผลอทำ pigate หลวมขึ้น** — การลบ catch-all ต้องแทนด้วย `NO` ที่อยู่**หลัง**
   guard เสมอ; ถ้าปล่อยตกท้ายฟังก์ชันเป็น undefined pigate จะได้ default ของ distro
   ซึ่งบาง action อนุญาต active session → least-privilege ที่ตั้งใจไว้หายเงียบๆ
3. **Exception ใน JS = rule ทั้งไฟล์ไม่ให้ผล** — `action.lookup("unit")` เป็น
   `undefined` ได้ (manage-units บาง call เช่น daemon-reload/reexec ไม่มี unit แนบ)
   แล้ว `.indexOf` จะโยน TypeError; polkit จะ log error และข้าม rule → พฤติกรรม
   เพี้ยนแบบตามไม่ทัน → guard `if (!unit) return NO;` และดู `journalctl -u polkit`
   ตอนทดสอบ (Step 3)
4. **การทดสอบบนเครื่องจริงมีความเสี่ยงล็อกฟีเจอร์ ไม่ใช่ล็อกเครื่อง** — ถ้า rule ใหม่
   พัง pigate จะสั่ง restart service ผ่าน D-Bus ไม่ได้ (Wi-Fi/DNS/hostname/time/power
   จาก UI ล้มเหลว) แต่ SSH/root ยังเข้าได้ปกติ → ทดสอบเมื่อเข้าถึงเครื่องทาง SSH ได้
   และแก้กลับได้ด้วยการเขียนไฟล์ rule เดิมทับ + `systemctl restart polkit`
5. **เครื่องที่ติดตั้งไปแล้วต้อง migrate เอง** — ไฟล์ rule เก่าค้างอยู่จนกว่าจะรัน
   `install.sh` ซ้ำ (ปลอดภัย: script idempotent, `cat >` ทับแล้ว restart polkit) หรือ
   แก้ไฟล์ `/etc/polkit-1/rules.d/10-pigate-system.rules` ตรงๆ — ต้องเขียนบอกใน
   PR/release note
6. **restart polkit จำเป็นเสมอหลังแก้ไฟล์** — polkitd cache rule ตอน start;
   `install.sh:282` ทำให้อยู่แล้ว แต่คน migrate มือต้องไม่ลืม

## 6. Summary Checklist (Definition of Done)

- [ ] `install.sh` STEP 3 — rule ใหม่: guard user → allowlist เดิม → deny-by-default
      เฉพาะ pigate + guard `!unit` + comment เตือน global-deny
- [ ] `bash -n install.sh` ผ่าน + JS ใน heredoc ผ่าน `node --check`
- [ ] ทดสอบเครื่องจริง (มี SSH access): pigate ใช้ Wi-Fi/DNS restart/hostname/time/power
      ผ่าน UI ได้ครบ; user อื่นได้ default ของ distro (ไม่ deny เงียบ);
      `journalctl -u polkit` ไม่มี JS error
- [ ] PR note: เครื่องที่ติดตั้งแล้วให้รัน `install.sh` ซ้ำ (หรือแทนไฟล์ rule + restart polkit)
- [ ] ไม่มี openapi/README ต้องแก้ (ไม่ใช่ API contract — จดเหตุผลไว้ที่นี่);
      `docs/setup_guide.md` ไม่แตะ (ไฟล์ dev คนละตัว, scoped ถูกแล้ว)
- [ ] เสร็จแล้วย้ายแผนนี้ไป `docs/ref/complete/`
