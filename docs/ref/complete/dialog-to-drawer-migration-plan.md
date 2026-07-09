# Dialog → Drawer Migration — เปลี่ยนฟอร์ม เพิ่ม/แก้ไข/ตั้งค่า จาก Dialog เป็น Drawer (shadcn/vaul)

> แผนงานสำหรับฟีเจอร์: เปลี่ยน overlay ของ **ฟอร์มจัดการข้อมูล** (create / edit /
> config) ทุกหน้า จาก `<Dialog>` (Radix, กลางจอ) ไปเป็น `<Drawer direction="right">`
> (vaul, side panel เลื่อนจากขวา) เพื่อ UX ที่สม่ำเสมอและได้พื้นที่แนวตั้งเต็มจอ
> ส่วน Dialog ที่เป็น **การยืนยัน (confirm)** คงเป็น Dialog ตามเดิม
>
> วันที่เขียน: 2026-07-08 · Branch อ้างอิง: `main` (แนะนำแตก branch `feat/dialog-to-drawer`)
> งานนี้เป็น frontend-only — ไม่มีการแก้ backend / API / openapi.yaml

---

## 0. เป้าหมายและขอบเขต

**เป้าหมาย:** ผู้ใช้กดปุ่ม เพิ่ม/แก้ไข/ตั้งค่า ในหน้าใดก็ตาม แล้วฟอร์มเปิดเป็น
Drawer เลื่อนเข้าจากขวาของจอ (desktop และ mobile) พฤติกรรมฟอร์มทุกอย่างเดิม:
validation, submit, error alert, ปุ่ม Cancel/Save, การ reset state ตอนปิด

**Out of scope (ต้องไม่แตะ):**
- `AlertDialogProvider.tsx` (global `useAlert().alert/confirm`) — เป็น confirmation กลางจอ คงเป็น Dialog
- Dialog ยืนยัน Reboot/Shutdown ใน `SettingsMaintenance.tsx:1074` และ `:1107` — เป็น confirm ไม่ใช่ฟอร์มข้อมูล คงเป็น Dialog
- ไม่ redesign เนื้อในฟอร์ม (ไม่เปลี่ยน native `<select>` เป็น shadcn Select, ไม่เปลี่ยน `space-y-*` เป็น `FieldGroup`) — สลับเฉพาะเปลือก overlay เท่านั้น
- ไม่แตะ `components/ui/sheet.tsx` (ใช้ภายใน sidebar mobile อยู่แล้ว)

---

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง ณ วันที่เขียน)

โครงสร้างพื้นฐานพร้อมแล้ว: `frontend/src/components/ui/drawer.tsx` มีอยู่แล้ว
(vaul `^1.1.2` อยู่ใน `package.json` แล้ว) รองรับ `direction="right"` ครบ
(`DrawerContent` มี style ของทุกทิศ) **แต่ยังไม่มีไฟล์ไหนใช้เลย** — ไม่ต้อง
`npx shadcn add` เพิ่ม

Dialog ฟอร์มที่ต้องแปลง มี **11 จุด ใน 9 หน้า** (ทั้งหมดใส่ `modal={false}` อยู่):

| # | ไฟล์ : บรรทัด (~) | ฟอร์ม | ความกว้างเดิม | ของข้างในที่ต้องระวัง |
|---|---|---|---|---|
| 1 | `frontend/src/pages/Addresses.tsx:517-617` | สร้าง/แก้ไข Address Object | 500px | native select ×1 |
| 2 | `frontend/src/pages/Services.tsx:454-557` | สร้าง/แก้ไข Service Object | 500px | native select ×1 |
| 3 | `frontend/src/pages/Users.tsx:380-502` | สร้าง/แก้ไข User | 480px | native select ×1 (role) |
| 4 | `frontend/src/pages/StaticRoutes.tsx:738-962` | สร้าง/แก้ไข Route | 500px | native select ×3 |
| 5 | `frontend/src/pages/QoS.tsx:629-851` | สร้าง/แก้ไข QoS Rule | 500px | native select ×1 |
| 6 | `frontend/src/pages/DnsServer.tsx:750-850` | สร้าง/แก้ไข Zone | 450px | — |
| 7 | `frontend/src/pages/DnsServer.tsx:853-973` | สร้าง/แก้ไข Record | 450px | native select ×1 |
| 8 | `frontend/src/pages/DhcpServer.tsx:843-1027` | ตั้งค่า DHCP ต่อ interface | 500px | native select ×1 |
| 9 | `frontend/src/pages/DhcpServer.tsx:1030-1116` | เพิ่ม/แก้ไข IP Reservation | 450px | — |
| 10 | `frontend/src/pages/Interfaces.tsx:902-1413` | Edit Interface (ฟอร์มใหญ่สุด ~500 บรรทัด) | 920px, `max-h-[90vh] overflow-y-auto` | shadcn `Select` (Radix portal) |
| 11 | `frontend/src/pages/FirewallPolicy.tsx:774-1009` | สร้าง/แก้ไข Firewall Rule | 960px | **base-ui Combobox chips ×3** (`useComboboxAnchor`), native select ×2 |

สิ่งที่พบเพิ่มจากการสำรวจ:
- `dialogContentRef` (`useRef` ส่งเข้า `DialogContent ref={...}`) มีใน 5 หน้า
  (Addresses:120, Services:115, StaticRoutes:107, DhcpServer:126, FirewallPolicy:412)
  — **เป็น dead code**: ไม่มีใคร consume ref นี้แล้ว (คอมเมนต์บอกว่าเคยใช้
  container-portal ให้ Combobox แต่ปัจจุบัน ComboboxContent ใช้ `anchor=` แทน)
  → ลบทิ้งระหว่าง migrate ได้เลย
- `docs/rules_of_work.md` §1.3 เขียนกติกา `modal={false}` ไว้ว่าใช้ *เฉพาะ Dialog
  ที่มี Combobox* แต่โค้ดจริงใส่ `modal={false}` ทุกฟอร์ม (เช่น `Users.tsx:378`
  คอมเมนต์อ้างว่าจำเป็นสำหรับ native select) — เอกสารกับโค้ด drift กันอยู่
  งานนี้ต้องอัปเดต §1.3 ให้ครอบคลุม Drawer ด้วย

สรุป: งานจริงกระจุกอยู่ที่การสลับ component ใน 9 ไฟล์ page + อัปเดต
`rules_of_work.md` — ไม่มีงาน backend

---

## 2. แนวทางเทคนิค

ใช้ `Drawer` (vaul) ที่มีอยู่แล้ว โดยกำหนด `direction="right"` ทุกจุด:

```tsx
import {
  Drawer, DrawerContent, DrawerHeader, DrawerTitle,
} from "@/components/ui/drawer"

<Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
  <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
    <DrawerHeader className="border-b border-border/50">
      <DrawerTitle className="text-base font-semibold">
        {editing ? "แก้ไข..." : "สร้าง...ใหม่"}
      </DrawerTitle>
    </DrawerHeader>
    <div className="flex-1 overflow-y-auto p-4">
      <form onSubmit={handleSave} className="space-y-4 text-sm">
        {/* เนื้อฟอร์มเดิม ยกมาทั้งก้อน ไม่แก้ */}
      </form>
    </div>
  </DrawerContent>
</Drawer>
```

**เหตุผลที่เลือก / ทางเลือกที่ตัดทิ้ง:**
- **Drawer (vaul) ตามโจทย์ผู้ใช้** — ได้ swipe-to-dismiss และเป็น component
  ที่โปรเจกต์ติดตั้งไว้แล้วแต่ยังไม่ได้ใช้ (ไม่เพิ่ม dependency ใหม่)
- ~~Sheet (Radix)~~ — หน้าตาเหมือนกัน (side panel) แต่โจทย์ระบุ Drawer และ
  Sheet เป็น Radix Dialog ตัวเดิมซึ่งมีปัญหา portal/Combobox แบบเดียวกับที่เจออยู่
- ~~direction="bottom" (ค่า default ของ shadcn)~~ — bottom sheet เหมาะกับ
  mobile action สั้น ๆ ไม่เหมาะกับฟอร์มยาวหลายฟิลด์อย่าง Interfaces/Firewall
- **การส่งต่อ prop `modal`:** คงพฤติกรรมเดิมของแต่ละจุดไว้ก่อน (ทุกฟอร์มตอนนี้
  `modal={false}`) — behavior parity ก่อน แล้วค่อยถอดเป็นรายจุดใน Step 7
  **ผลทดสอบจริงจากเจ้าของโปรเจกต์ (2026-07-08):** ลองใส่ Drawer แล้ว
  ถอด `modal={false}` ได้ทุกหน้า (native select / Radix Select ใช้งานได้ปกติ)
  **ยกเว้น `FirewallPolicy.tsx` ที่เดียว** ที่ใช้ Combobox chips —
  หน้านั้นต้องคง `modal={false}` ไว้ ซึ่งตรงกับเจตนาเดิมของ
  `rules_of_work.md` §1.3 พอดี

**ไฟล์แม่แบบ:** ทำหน้า **`Addresses.tsx` เป็น pilot ก่อน** (ฟอร์มเล็กสุด ครบทุก
องค์ประกอบ: header/error alert/native select/footer) ให้ pattern นิ่งแล้วค่อย
ทาบไปหน้าอื่นทีละไฟล์

---

## 3. ขั้นตอน (เรียงตามลำดับความเสี่ยง ง่าย → ยาก)

### Step 1 — Pilot: `Addresses.tsx`
**ไฟล์:** `frontend/src/pages/Addresses.tsx:517`
- เปลี่ยน import จาก `ui/dialog` เป็น `ui/drawer` (Dialog→Drawer,
  DialogContent→DrawerContent, DialogHeader→DrawerHeader, DialogTitle→DrawerTitle)
- ห่อ `<form>` ด้วย `<div className="flex-1 overflow-y-auto p-4">` เพราะ
  DrawerContent เป็น flex-col เต็มความสูงจอ ไม่มี padding/scroll ในตัวแบบ
  DialogContent (ของเดิม `p-6` อยู่บน DialogContent — ย้าย padding ลง wrapper)
- ปุ่ม Cancel/Save **คงไว้ท้าย `<form>` ตามเดิม** ไม่ย้ายไป `DrawerFooter`
  (ปุ่ม `type="submit"` ต้องอยู่ใน form; ถ้าจะใช้ DrawerFooter ต้องเพิ่ม
  `form=` attribute — ไม่คุ้ม)
- ลบ `dialogContentRef` (dead code) ที่ `Addresses.tsx:120`
> ไม่ต้องใช้ `DrawerTrigger` — ทุกหน้าคุมด้วย `open`/`onOpenChange` state เดิมอยู่แล้ว
> และไม่ต้องใช้ `DrawerDescription` เว้นแต่หน้าที่มีอยู่เดิม

### Step 2 — หน้าฟอร์มเดี่ยวขนาดกลาง
**ไฟล์:** `Services.tsx:454`, `Users.tsx:380`, `StaticRoutes.tsx:738`, `QoS.tsx:629`
- ทาบ pattern จาก Step 1 ตรง ๆ (ลบ `dialogContentRef` ใน Services:115,
  StaticRoutes:107 ด้วย)

### Step 3 — หน้าที่มี 2 Drawer
**ไฟล์:** `DnsServer.tsx:750, 853` และ `DhcpServer.tsx:843, 1030`
- แต่ละหน้ามี 2 dialog แยก state กัน (`isZoneModalOpen`/`isRecModalOpen`,
  `isConfigModalOpen`/`isResModalOpen`) — แปลงทีละตัว เปิดได้ทีละตัวอยู่แล้ว
  ไม่มีเคสซ้อนกัน (ลบ `dialogContentRef` ใน DhcpServer:126)

### Step 4 — `Interfaces.tsx` (ฟอร์มใหญ่)
**ไฟล์:** `frontend/src/pages/Interfaces.tsx:902`
- ของเดิมกว้าง 920px → ใช้ `data-[vaul-drawer-direction=right]:sm:max-w-[920px]`
  (ดูข้อควรระวังเรื่อง width override ใน §5)
- `max-h-[90vh] overflow-y-auto` บน DialogContent เดิม → เอาออก ใช้ wrapper
  `flex-1 overflow-y-auto` แทน (drawer สูงเต็มจออยู่แล้ว ฟอร์มยาวจะได้พื้นที่เพิ่ม)
- ข้างในใช้ shadcn `Select` (Radix portal) — คง `modal={false}` แล้วต้องเทสต์
  ว่า dropdown คลิกได้

### Step 5 — `FirewallPolicy.tsx` (เสี่ยงสุด ทำท้ายสุด)
**ไฟล์:** `frontend/src/pages/FirewallPolicy.tsx:774`
- กว้าง 960px → override width แบบเดียวกับ Step 4
- มี base-ui Combobox chips ×3 ที่ยึดตำแหน่งด้วย `useComboboxAnchor`
  (`:407-409`) — popup portal ไป body แล้ว position ตาม anchor ต้องเทสต์ว่า
  ตำแหน่ง popup ตรงหลัง drawer เลื่อนเข้าเสร็จ และคลิกเลือก/ลบ chip ได้
- ลบ `dialogContentRef` (`:411-412`) และคอมเมนต์ที่อ้างถึง

### Step 6 — อัปเดตเอกสาร
**ไฟล์:** `docs/rules_of_work.md` §1.3 (~บรรทัด 22-31)
- เพิ่มกติกาใหม่: ฟอร์มจัดการข้อมูลใช้ `Drawer direction="right"`;
  Dialog สงวนไว้สำหรับ confirmation (`useAlert`, power confirm) เท่านั้น
- ปรับข้อความ `modal={false}` ให้พูดถึง Drawer ด้วย (vaul อยู่บน Radix Dialog
  จึงใช้กติกาเดียวกัน) และแก้ให้ตรงกับพฤติกรรมจริงที่ใช้กับ native select ด้วย

### Step 7 (ทำหลังทุกหน้า migrate เสถียรแล้ว — แยกคอมมิต)
- ถอด `modal={false}` ออกจาก Drawer **ทุกหน้า ยกเว้น `FirewallPolicy.tsx`**
  (ผลทดสอบจริงยืนยันแล้วว่าใช้ได้ — ดู §2) แล้วเทสต์ native select /
  Radix Select ซ้ำอีกรอบเพื่อยืนยัน

> **สิ่งที่ไม่ต้องทำ:** ไม่ต้อง `npx shadcn@latest add drawer` (มีแล้ว), ไม่แตะ
> `ui/dialog.tsx` (AlertDialogProvider + power confirm ยังใช้), ไม่มีงาน state
> management เพิ่ม (ทุก Drawer ใช้ `open`/`onOpenChange` state ตัวเดิมของหน้า)

---

## 4. Related API

ไม่มี — งานนี้ไม่แตะ endpoint, `openapi.yaml` (ทั้งสองไฟล์), router, middleware
และไม่กระทบโหมด `-disable-edit` (การ block mutation อยู่ฝั่ง backend เหมือนเดิม)

---

## 5. ข้อควรระวัง

1. **Width override บน DrawerContent ต้องใช้ variant prefix เต็ม** —
   ค่า default ของทิศขวาคือ `data-[vaul-drawer-direction=right]:sm:max-w-sm`
   (`ui/drawer.tsx:57`) ถ้าส่ง `sm:max-w-[920px]` เฉย ๆ selector ของ default
   จะ specificity สูงกว่า (มี attribute selector) → **ความกว้างไม่เปลี่ยน
   แบบเงียบ ๆ** ต้องส่งเป็น
   `data-[vaul-drawer-direction=right]:sm:max-w-[920px]` ให้ tailwind-merge
   ใน `cn()` แทนที่ class เดิมตรง ๆ
2. **vaul คือ Radix Dialog + gesture** — ผลทดสอบจริง (ดู §2): ใน Drawer
   ถอด `modal={false}` ได้ทุกหน้า ยกเว้น `FirewallPolicy.tsx` (Combobox chips)
   ที่ pointer blocker ยังบล็อกการคลิก popup อยู่ → หน้านั้น**ต้องคง
   `modal={false}` เสมอ** ส่วนหน้าอื่นให้ถอดใน Step 7 แยกคอมมิตจากการ
   migrate — ถ้าถอดพร้อมกันแล้วพังจะแยกไม่ออกว่าอะไรทำพัง
3. **DrawerContent ไม่มี padding/scroll ในตัว** — DialogContent เดิมมี `p-6`
   และ (บางจุด) `overflow-y-auto` ถ้าย้ายฟอร์มมาโดยไม่ห่อ
   `<div className="flex-1 overflow-y-auto p-4">` ฟอร์มยาว ๆ
   (Interfaces, FirewallPolicy, QoS) จะ **ล้นจอโดย scroll ไม่ได้** และปุ่ม Save
   จะหลุดจอ
4. **FirewallPolicy Combobox positioning** — popup ยึดตำแหน่งกับ anchor ผ่าน
   base-ui ระหว่าง drawer กำลัง animate เข้า (transform เลื่อน) ตำแหน่ง anchor
   ยังวิ่งอยู่ ถ้าผู้ใช้เปิด combobox ทันที popup อาจลอยผิดที่ → เทสต์จริง;
   ถ้าเพี้ยนให้พิจารณาปิด drag gesture หรือคงหน้านี้เป็น Dialog ไว้ก่อนแล้วแยก
   เป็นงานถัดไป (อย่าฝืนแก้ ui/drawer.tsx กลางทาง)
5. **swipe-to-dismiss ของ vaul** — ลากขวาเพื่อปิดได้ ถ้าฟอร์มกรอกค้างอยู่จะหาย
   เหมือนการกด Esc/คลิกนอกของเดิม (ของเดิมก็ปิดได้ไม่มี dirty-check) —
   พฤติกรรมเทียบเท่า ไม่ต้องเพิ่ม dirty-check ในงานนี้ แต่ระวังอย่าให้ area
   ที่ scroll ฟอร์มไปชนกับ drag gesture (vaul จัดการ scrollable ข้างในให้แล้ว
   แต่ต้องเทสต์ฟอร์มยาวบนจอสัมผัส/mobile)
6. **กติกา frontend เดิมยังบังคับ** — flat design (drawer.tsx ปัจจุบันไม่มี
   `shadow-*` อยู่แล้ว อย่าเพิ่ม), semantic color เท่านั้น, เทสต์ dark/light
   ทั้งคู่, ห้ามแก้สี/typography ผ่าน className บน DrawerContent
7. **การเทสต์ปลอดภัย 100% ใน mock mode** — ทุกหน้าเทสต์ได้ด้วย
   `./pigate-backend -mock=true` + `yarn dev` ไม่ต้องแตะบอร์ดจริง เพราะเปลี่ยน
   เฉพาะเปลือก UI; ข้อยกเว้นเดียวคือควรกดใช้งานจริงบนจอมือถือ/แท็บเล็ต
   (viewport แคบ) เพื่อดู drawer กว้าง `w-3/4` บน mobile

---

## 6. Checklist สรุป (Definition of Done)

- [ ] `Addresses.tsx` — Drawer pilot + ลบ `dialogContentRef`
- [ ] `Services.tsx` — Drawer + ลบ `dialogContentRef`
- [ ] `Users.tsx` — Drawer
- [ ] `StaticRoutes.tsx` — Drawer + ลบ `dialogContentRef`
- [ ] `QoS.tsx` — Drawer
- [ ] `DnsServer.tsx` — Drawer ×2 (Zone, Record)
- [ ] `DhcpServer.tsx` — Drawer ×2 (Config, Reservation) + ลบ `dialogContentRef`
- [ ] `Interfaces.tsx` — Drawer กว้าง 920px + ย้าย scroll เข้า wrapper
- [ ] `FirewallPolicy.tsx` — Drawer กว้าง 960px + เทสต์ Combobox chips ครบ 3 ช่อง
- [ ] ทุกไฟล์: import จาก `ui/dialog` ที่ไม่ใช้แล้วถูกลบ (ESLint จับ unused import)
- [ ] `SettingsMaintenance.tsx` + `AlertDialogProvider.tsx` **ไม่ถูกแตะ** (ยัง Dialog)
- [ ] `docs/rules_of_work.md` §1.3 อัปเดตกติกา Drawer/Dialog
- [ ] `yarn build` + `yarn lint` ผ่าน
- [ ] เทสต์ mock mode ครบทุกหน้า: เปิด/ปิด drawer, กรอกฟอร์ม, submit สำเร็จ,
      error alert แสดง, native select / Radix Select / Combobox คลิกได้
- [ ] เทสต์ dark + light mode และ viewport มือถือ (drawer `w-3/4`)
- [ ] Step 7 (แยกคอมมิต): ถอด `modal={false}` ทุกหน้า ยกเว้น `FirewallPolicy.tsx`
      + เทสต์ select ทุกแบบซ้ำ
