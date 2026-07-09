# Semantic Status Colors Migration — กวาดล้าง hardcoded palette classes เป็นตัวแปร theme

> เอกสารแผนงานสำหรับงาน: แทนที่คลาสสี Tailwind แบบ hardcode (`text-amber-500`,
> `bg-red-500/10`, `dark:text-red-400` ฯลฯ) ที่ตกค้างอยู่ ~55 จุดใน 15 ไฟล์
> ด้วยตัวแปร semantic (`warning` / `destructive`) ให้ตรงกฎ
> `docs/rules_of_work.md` §1 ("No hardcoded Tailwind color classes") ทั้งโปรเจกต์
>
> วันที่เขียน: 2026-07-09 · Branch อ้างอิง: `feat/central-event-log`
> (ตัวแปร `--warning` ถูกเพิ่มใน branch นี้แล้ว — ดู §1)

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย:** ทุกสีที่สื่อ *สถานะ* (warning/error/danger) ในโค้ด frontend
ต้องอ้างผ่านตัวแปร theme ใน `src/index.css` เท่านั้น เพื่อให้:

- ปรับโทนสีทั้งแอปได้จากจุดเดียว และ dark/light สลับค่าอัตโนมัติ
  (เลิกใช้ `dark:text-amber-400` ซ้อนทับรายจุด)
- โค้ดใหม่มี pattern เดียวให้ลอก — หน้า `EventLogs.tsx` และบล็อก status
  helpers ของ `Dashboard.tsx` ถูก migrate แล้วใน branch นี้ ใช้เป็นต้นแบบ

**นอกขอบเขต:**
- **`components/tailwindIndicator.tsx`** — เครื่องมือ debug ของ dev
  (SizeIndicator/TailwindIndicator) ใช้สี palette 13 จุดเพื่อไล่ breakpoint
  โดยเจตนา ไม่ใช่ UI ผู้ใช้ — ไม่แตะ
- **สีในคอนฟิก recharts / ค่า hex ใน JS** — กฎครอบคลุมเฉพาะ Tailwind class;
  ชาร์ตใช้ `--chart-*` อยู่แล้ว งานนี้ไม่แตะ
- **เพิ่มตัวแปรสถานะอื่น (success/info)** — โปรเจกต์ใช้ `primary` (เขียว)
  แทน success อยู่แล้วทุกหน้า ไม่สร้างตัวแปรใหม่เกินจำเป็น

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดแล้ว ณ วันที่เขียน)

ตัวแปรใน `frontend/src/index.css`:

| ตัวแปร | สถานะ |
|---|---|
| `--warning` (+ mapping `--color-warning`) | **มีแล้ว** — เพิ่มใน branch `feat/central-event-log` (~บรรทัด 28, 86, 122); light ≈ amber-600 `oklch(0.666 0.179 58.32)`, dark ≈ amber-500 `oklch(0.769 0.188 70.08)` |
| `--warning-foreground` | **ยังไม่มี** — จำเป็นสำหรับปุ่มพื้นทึบ `bg-warning` (ดู Step 1) |
| `--destructive` | มีแล้วทั้งสองธีม (แดง; dark สว่างกว่า light อยู่แล้ว) |

จุดที่ hardcode ตกค้าง (grep `(text|bg|border|...)-(red|amber|...)-[0-9]`
เมื่อ 2026-07-09, ตัด tailwindIndicator ออกแล้ว) — เลขบรรทัดเป็นค่า ~:

| ไฟล์ | amber (→ warning) | red (→ destructive) | เคสพิเศษ |
|---|---|---|---|
| `pages/Interfaces.tsx` | 72, 89, 675, 683, 782, 801, 1119 | 73, 90, 687, 726, 786, 810 | signal-strength helper (72-90) ไล่ 3 ระดับ primary/amber/red |
| `pages/StaticRoutes.tsx` | 629, 633, 657 | 444-445, 638, 711 | — |
| `pages/SettingsMaintenance.tsx` | — | 1031-1032, 1078, 1083, 1111 | — |
| `pages/FirewallPolicy.tsx` | — | 223, 269, 746, 981 | 981 มี `dark:data-active:text-red-400` |
| `pages/QoS.tsx` | 386, 569, 778 | 603 | 386 ใช้ amber แยกทิศ ingress (categorical ไม่ใช่ status — ดู §5.4) |
| `pages/DhcpServer.tsx` | 512 | 618, 713 | 512 ปุ่มพื้นทึบ `bg-amber-500 text-neutral-950 hover:bg-amber-400` |
| `pages/DnsServer.tsx` | 437 | 587, 697 | 437 ปุ่มพื้นทึบแบบเดียวกับ DhcpServer 512 |
| `pages/Users.tsx` | 308 | 333, 363 | — |
| `pages/Services.tsx` | 386, 408 | 434 | — |
| `pages/Addresses.tsx` | 442 | 487 | — |
| `components/site-header.tsx` | 58, 73 | 57 | มี `dark:text-amber-400` ซ้อน |
| `components/nav-user.tsx` | 126 | 148, 155, 166 | 124 `text-indigo-400` (ไอคอน Moon), 148/155/166 ผสม `text-red-500` + `focus:text-destructive` ในบรรทัดเดียว |
| `components/power-control.tsx` | — | 40, 41 | — |
| `pages/ApiDocs.tsx` | 49 | — | ไอคอน Sun (ธีม toggle) |

ที่ migrate แล้ว (ต้นแบบ): `pages/EventLogs.tsx:~70-77` (SEVERITY_STYLE),
`pages/Dashboard.tsx:~234-264` (statusMeter/statusBadge/alertStyle)

สรุป: **งาน frontend ล้วน ไฟล์เดียวที่แก้เชิงโครงสร้างคือ `index.css`
(เพิ่ม 1 ตัวแปร) ที่เหลือเป็นการแทนที่คลาสแบบ mechanical ~55 จุด**
ไม่มีงาน backend / API / openapi.yaml เลย

---

## 2. แนวทางเทคนิค

ตารางแทนที่ (mapping เดียวใช้ทุกไฟล์):

| ของเดิม | แทนด้วย |
|---|---|
| `text-amber-500`, `text-amber-400` | `text-warning` |
| `bg-amber-500/10` · `border-amber-500/20` | `bg-warning/10` · `border-warning/20` |
| `bg-amber-500` + `text-neutral-950` + `hover:bg-amber-400` (ปุ่มทึบ) | `bg-warning` + `text-warning-foreground` + `hover:bg-warning/90` |
| `text-red-500`, `text-red-400` | `text-destructive` |
| `bg-red-500/10` · `border-red-500/20` · `bg-red-500` | `bg-destructive/10` · `border-destructive/20` · `bg-destructive` |
| `dark:text-amber-400`, `dark:text-red-400` (ทุก variant `dark:`) | **ลบทิ้ง** — ตัวแปรมีค่า dark ในตัวแล้ว |

- **ทำไมลบ `dark:` variant ได้:** `--warning`/`--destructive` ถูกประกาศทั้งใน
  `:root` และ `.dark` โดยค่า dark สว่างกว่าอยู่แล้ว (pattern เดียวกับที่
  shadcn ทำกับ `--destructive`) การคง `dark:text-red-400` ไว้คือการ
  override ที่ตัวแปรทำให้อยู่แล้ว
- **ทางเลือกที่ตัดทิ้ง:** (ก) สร้าง utility class `.status-warning` ใน CSS —
  ปฏิเสธเพราะโปรเจกต์ใช้ Tailwind class ตรง ๆ ทั้งโค้ด ไม่มี pattern นี้อยู่ก่อน
  (ข) เพิ่มตัวแปรครบชุด success/info — ปฏิเสธ เกินความต้องการจริง (§0)
- **Pattern ต้นแบบ:** `pages/EventLogs.tsx` `SEVERITY_STYLE` และ
  `pages/Dashboard.tsx` `statusBadge` — คลาสชุด
  `bg-warning/10 text-warning border-warning/20` คือรูปแบบมาตรฐานของ badge

---

## 3. ขั้นตอนการทำ (เรียงลำดับ + ไฟล์ที่ต้องแก้)

### Step 1 — เพิ่ม `--warning-foreground`
**ไฟล์:** `frontend/src/index.css`
- `@theme inline` (~บรรทัด 28): `--color-warning-foreground: var(--warning-foreground);`
- `:root` (~87) และ `.dark` (~123): `--warning-foreground: oklch(0.145 0 0);`
  (ตัวหนังสือเข้มบนพื้นเหลืองอ่านออกทั้งสองธีม — ค่าเดียวกับ `--foreground`
  ฝั่ง light ตรงกับ `text-neutral-950` ที่ปุ่มเดิมใช้)

### Step 2 — ปุ่ม Apply พื้นทึบ (เคสพิเศษเดียวที่ไม่ mechanical)
**ไฟล์:** `pages/DhcpServer.tsx:~512`, `pages/DnsServer.tsx:~437`

```
เดิม: bg-amber-500 font-semibold text-neutral-950 hover:bg-amber-400
ใหม่: bg-warning  font-semibold text-warning-foreground hover:bg-warning/90
```

### Step 3 — แทนที่ amber → warning ที่เหลือ (mechanical)
**ไฟล์:** `Interfaces.tsx`, `StaticRoutes.tsx`, `QoS.tsx`, `Users.tsx`,
`Services.tsx`, `Addresses.tsx`, `site-header.tsx` ตามตาราง §1 —
แทนตรงตามตาราง §2 แล้วลบ `dark:text-amber-400` ที่ `site-header.tsx:58, 73`

### Step 4 — แทนที่ red → destructive (mechanical)
**ไฟล์:** ทุกไฟล์คอลัมน์ red ในตาราง §1 — รวมจุดที่ผสมสองระบบใน
`nav-user.tsx:~148-166` (`text-red-500 focus:bg-destructive/10 ...
dark:text-red-400` → เหลือ `text-destructive focus:bg-destructive/10
focus:text-destructive` อย่างเดียว) และ `FirewallPolicy.tsx:~981`
(`data-active:text-red-500 dark:data-active:text-red-400` →
`data-active:text-destructive`)

### Step 5 — ไอคอนธีม toggle (ตัดสินใจ + จุดเล็ก)
**ไฟล์:** `nav-user.tsx:~124-126`, `ApiDocs.tsx:~49`
- ไอคอน Sun (`text-amber-500`) → `text-warning`; ไอคอน Moon
  (`text-indigo-400`) → `text-primary`
- เหตุผล: เป็นสีตกแต่งไม่ใช่สถานะ แต่กฎ §1 ของ rules_of_work ไม่มีข้อยกเว้น
  ให้ decorative — ใช้ semantic ที่ใกล้เคียงที่สุดเพื่อไม่เหลือ palette class
  ในโค้ด (ถ้าทีมอยากคงสีเดิมเป๊ะ ให้บันทึกข้อยกเว้นลง `rules_of_work.md`
  แทน — ห้ามปล่อยเงียบ)

### Step 6 — ตรวจกวาดซ้ำ
รัน grep เดิม (regex ใน §1) ต้องเหลือผลลัพธ์เฉพาะ `tailwindIndicator.tsx`
เท่านั้น แล้ว `yarn lint` + `yarn build`

> **ไม่ต้องทำ:** backend ทุกส่วน, `openapi.yaml` (ไม่มี API เปลี่ยน),
> `install.sh`, README Feature Status (ไม่ใช่ฟีเจอร์ผู้ใช้ — เป็น code quality)
> และ **ไม่ต้องแตะ `rules_of_work.md`** เว้นแต่เลือกทางข้อยกเว้นใน Step 5

---

## 4. API ที่เกี่ยวข้อง

ไม่มี — งานนี้เป็น CSS/className ฝั่ง frontend ล้วน ไม่แตะ route/handler/
`-disable-edit` ใด ๆ

---

## 5. ข้อควรระวัง

1. **สีฝั่ง light จะเปลี่ยนจริงเล็กน้อย** — ของเดิมใช้ amber-500
   (`oklch ~0.769`) ทั้งสองธีม แต่ `--warning` ฝั่ง light จงใจเข้มกว่า
   (≈ amber-600) เพื่อ contrast บนพื้นขาว → ธีม light จะเห็นเหลืองเข้มขึ้น
   ทุกจุดที่ migrate นี่คือ *พฤติกรรมที่ตั้งใจ* ไม่ใช่ regression —
   ต้องเปิดดูทุกหน้าที่แก้ทั้ง light และ dark ก่อนปิดงาน
2. **ห้าม find-replace ทั้งโปรเจกต์แบบหลับตา** — `tailwindIndicator.tsx`
   ต้องรอด (ดู §0) และคำว่า `red`/`amber` อาจโผล่ใน string อื่นที่ไม่ใช่
   className → แก้ทีละไฟล์ตามตาราง §1 แล้วใช้ grep §6 เป็นตัวตรวจ ไม่ใช่ตัวแก้
3. **จุดที่สีผสมสองระบบอยู่แล้ว** (`nav-user.tsx:148-166` ใช้ `text-red-500`
   คู่ `focus:text-destructive`) บ่งว่าเคยแก้ครึ่งเดียว — ถ้าแทนแล้วเหลือ
   คลาสซ้ำความหมาย (`text-destructive focus:text-destructive`) ให้ยุบเหลือ
   ตัวเดียว อย่าปล่อยคลาสขยะ
4. **`QoS.tsx:386` amber ไม่ได้แปลว่า warning** — ใช้แยกทิศ ingress/egress
   (categorical) การ map เป็น `text-warning` คงหน้าตาเดิมไว้ได้ แต่ถ้าอนาคต
   เปลี่ยนโทน `--warning` กราฟ QoS จะเปลี่ยนตาม — ยอมรับใน phase นี้
   (ทางเลือกคือย้ายไป `--chart-*` ซึ่งปัจจุบันเป็น grayscale จะทำให้แยกทิศ
   ยากขึ้น จึงไม่ทำ)
5. **`power-control.tsx` คือ overlay ตอนเครื่องกำลังดับ** — หลังแก้ต้องเทส
   flow reboot/shutdown ใน mock mode ให้เห็น overlay จริง (กดจากหน้า
   Settings & Maintenance) ไม่ใช่ดูแค่ static render
6. **การทดสอบ:** `yarn lint` + `yarn build` ต้องผ่าน; เปิด UI (mock mode)
   ไล่ดูหน้า: Interfaces (badge สถานะ + signal bar + ปุ่ม disconnect),
   StaticRoutes (badge system/warning + แถบเตือนแดง), FirewallPolicy
   (badge DROP + tab DROP), DHCP/DNS Server (ปุ่ม Apply เหลืองกะพริบ),
   Users/Services/Addresses (badge), QoS (กราฟ+ไอคอน ingress),
   SettingsMaintenance (dialog ยืนยัน reboot/shutdown), site-header
   (สี temp ตอน warning/critical — mock อุณหภูมิสูงถ้าทำได้), nav-user
   (เมนู logout แดง + toggle ธีม) — ทั้ง light และ dark ทุกหน้า

---

## 6. Checklist สรุป (Definition of Done)

- [ ] `index.css` — เพิ่ม `--warning-foreground` (`@theme inline` + `:root` + `.dark`)
- [ ] `pages/DhcpServer.tsx` + `pages/DnsServer.tsx` — ปุ่ม Apply พื้นทึบ → `bg-warning text-warning-foreground hover:bg-warning/90`
- [ ] `pages/Interfaces.tsx` — signalColor/SignalBar + badge + ปุ่ม (13 จุด)
- [ ] `pages/StaticRoutes.tsx` — badge + แถบเตือน (7 จุด)
- [ ] `pages/SettingsMaintenance.tsx` — dialog reboot/shutdown (5 จุด)
- [ ] `pages/FirewallPolicy.tsx` — action badge + tab DROP (4 จุด, ลบ `dark:` variant)
- [ ] `pages/QoS.tsx` — ingress icon/label + ปุ่มลบ (4 จุด)
- [ ] `pages/Users.tsx` / `pages/Services.tsx` / `pages/Addresses.tsx` — badge + ปุ่มลบ (3+3+2 จุด)
- [ ] `components/site-header.tsx` — สี temp (3 จุด, ลบ `dark:` variant)
- [ ] `components/nav-user.tsx` — เมนูแดง + ไอคอนธีม (5 จุด, ยุบคลาสซ้ำ)
- [ ] `components/power-control.tsx` — overlay shutdown (2 จุด)
- [ ] `pages/ApiDocs.tsx` — ไอคอน Sun (1 จุด)
- [ ] grep ตรวจซ้ำ: palette class เหลือเฉพาะ `tailwindIndicator.tsx`
- [ ] `yarn lint` + `yarn build` ผ่าน
- [ ] ไล่ดู UI ทุกหน้าที่แก้ ทั้ง light/dark ตามรายการ §5.6 (รวม overlay power)
