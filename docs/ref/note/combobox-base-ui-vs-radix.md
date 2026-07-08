# Note: Combobox เป็น Base UI ไม่ใช่ Radix — และ shadcn ตั้งใจให้เป็นแบบนั้น

> บันทึกผลการตรวจสอบ (2026-07-08) ก่อนตัดสินใจว่าจะ "เปลี่ยน Combobox เป็น Radix" หรือไม่
> สรุปสั้น: **shadcn ไม่มี Combobox เวอร์ชัน Radix** — แม้ base ของโปรเจกต์เป็น `radix`
> ตัว Combobox ก็ยังถูกสร้างบน Base UI โดยเจตนา เพราะ Radix ไม่มี primitive `Combobox`

---

## 1. ที่มาของคำถาม

ระหว่างงาน [Dialog → Drawer migration](../todo/dialog-to-drawer-migration-plan.md)
พบว่า `FirewallPolicy.tsx` เป็นหน้าเดียวที่ต้องคง `modal={false}` ไว้ เพราะ popup
ของ Combobox (chips ×3) โดน pointer blocker ของ vaul/Radix Dialog บล็อก

แนวคิดที่อยากลอง: ถ้าเปลี่ยน Combobox ให้เป็น Radix (ตระกูลเดียวกับ Drawer/Dialog)
pointer blocker น่าจะไม่ตีกัน → ถอด `modal={false}` ออกจาก FirewallPolicy ได้

## 2. สถานะ primitive ของ UI components ปัจจุบัน

| Primitive package | ใช้ใน component |
|---|---|
| `radix-ui` (unscoped, v1.5.0) | `dialog`, `select`, `dropdown-menu`, `popover`*, `tooltip`, `switch`, `checkbox`, `tabs`, `alert-dialog` ฯลฯ |
| `@base-ui/react` (v1.5.0) | **`combobox` (ตัวเดียว)** |
| `vaul` (v1.1.2) | `drawer` (สร้างบน Radix Dialog อีกที) |

หมายเหตุ: โปรเจกต์ใช้ `radix-ui` แบบ **unscoped package เดียว** (รูปแบบใหม่ของ shadcn)
ไม่ใช่ `@radix-ui/react-*` แบบ scoped เดิม — import เป็น `import { Dialog as DialogPrimitive } from "radix-ui"`

(*`popover.tsx` ยังไม่ถูกสร้างในโปรเจกต์ ณ ตอนเขียน — radix Popover มากับ `radix-ui` แต่ยังไม่มีไฟล์ ui/popover.tsx)

## 3. หลักฐาน: shadcn Combobox = Base UI แม้ base เป็น radix

Project config (จาก `npx shadcn@latest info`): `"base": "radix"`, `"style": "radix-vega"`

รัน `npx shadcn@latest view @shadcn/combobox` (เคารพ base radix ของโปรเจกต์) ได้:

```json
{
  "name": "combobox",
  "dependencies": ["@base-ui/react"],          // ← ยังเป็น Base UI
  "registryDependencies": ["button", "input-group"],
  "meta": { "links": {
    "docs": "https://ui.shadcn.com/docs/components/radix/combobox",
    "api":  "https://base-ui.com/react/components/combobox"   // ← API doc ชี้ไป base-ui.com
  } }
}
```

- ไฟล์ registry ของ shadcn (`registry/radix-vega/ui/combobox.tsx`) **เหมือนกับ
  `frontend/src/components/ui/combobox.tsx` ในรีโปแทบ 100%** ต่างแค่ไอคอน
  (registry ใช้ `IconPlaceholder`, ในรีโป swap เป็น lucide `ChevronDownIcon/XIcon/CheckIcon` แล้ว)
- `npx shadcn@latest docs combobox` → field `api` = `https://base-ui.com/react/components/combobox`

**สรุปข้อ 3:** Combobox ที่เป็น Base UI ตอนนี้ = ตัว canonical ของ shadcn อยู่แล้ว
ไม่ใช่ของแปลกปลอมที่ควร "แก้ให้เป็น Radix"

## 4. เหตุผลเชิงเทคนิค

Radix UI **ไม่มี** primitive ชื่อ Combobox (มีแค่ Select / Popover / DropdownMenu /
NavigationMenu) การทำ combobox แบบ multi-select + chips + typeahead filter + keyboard
navigation + empty state บน Radix ต้องประกอบเองจาก Popover + จัดการ state ทั้งหมดเอง
(หรือใช้ `cmdk` ซึ่งเป็น dep ใหม่ และ multi-select chips ก็ยังต้องเขียนเอง)

Base UI Combobox ให้ความสามารถพวกนี้มาสำเร็จรูป — จึงเป็นเหตุผลที่ shadcn เลือกใช้
Base UI เฉพาะ component นี้

## 5. API surface ที่ FirewallPolicy ใช้อยู่ (ถ้าจะ reimplement ต้องรองรับครบ)

ไฟล์เดียวที่ใช้ Combobox: `frontend/src/pages/FirewallPolicy.tsx` (3 ช่อง: source / destination / service)

- `<Combobox multiple required value={string[]} onValueChange items={string[]}>` — multi-select controlled
- `<ComboboxChips ref={anchor}>` + `useComboboxAnchor()` — chips container + anchor สำหรับ positioning popup
- `<ComboboxValue>{(values) => ...}</ComboboxValue>` — render prop คืน array ค่าที่เลือก
- `<ComboboxChip>` + ChipRemove (ปุ่มลบในตัว)
- `<ComboboxChipsInput>` — ช่องพิมพ์กรอง inline ในกล่อง chips
- `<ComboboxContent anchor={anchor}>` → `<ComboboxEmpty>` + `<ComboboxList>{(opt) => <ComboboxItem>}</ComboboxList>`
- ต้องการ: พิมพ์แล้วกรอง items, keyboard nav, highlighted item, empty state "ไม่พบข้อมูล"

## 6. ทางเลือกสำหรับงานต่อ (ยังไม่ตัดสิน)

**Option A — คง Base UI ไว้ (แนะนำ):**
ไม่เปลี่ยน combobox (เป็น shadcn-canonical อยู่แล้ว) และคง `modal={false}` ที่
FirewallPolicy ไว้ตามเดิม → จบ ไม่มีความเสี่ยง Base UI เป็น dep ที่ shadcn ตั้งใจให้ติด
ไม่ใช่ของแปลกปลอม

**Option B — hand-build multiselect บน Radix Popover:**
reimplement chips + typeahead filter + keyboard/highlight/empty เองทั้งหมดบน Radix
Popover เพื่อตัด dep `@base-ui/react` + ถอด `modal={false}`
- ข้อดี: primitive เป็น Radix ล้วน, ถอด modal={false} ได้, ตัด 1 dependency
- ข้อเสีย: custom code เยอะ, **ไม่ใช่ shadcn-canonical (ขัดกติกา rules_of_work §1.1)**,
  ต้องดูแล a11y/keyboard/filter เอง, เสี่ยง regression ในหน้า firewall (security-adjacent)

**Option C — ใช้ Radix Popover + cmdk:**
เพิ่ม dep `cmdk` (well-known, shadcn ใช้เป็น building block ของ Command) + สร้าง
popover.tsx/command.tsx แล้วประกอบ multiselect เอง
- ข้อดี: filtering/keyboard/a11y ผ่าน cmdk ที่ทดสอบมาแล้ว
- ข้อเสีย: เพิ่ม dependency ใหม่ (ขัดกติกา minimal-deps ของ CLAUDE.md), chips multi-select
  ยังต้องเขียน wiring เอง, ก็ยังไม่ใช่ shadcn-canonical combobox

## 7. ข้อสังเกตปิดท้าย

- ถ้าเป้าหมายจริงคือ "ถอด `modal={false}` ที่ FirewallPolicy" — การเปลี่ยน primitive
  เป็นวิธีที่แพงและเสี่ยง ควรลองทางที่ถูกกว่าก่อน เช่น ปรับ positioning/portal ของ
  popup ให้ไม่ชน pointer blocker หรือคง `modal={false}` ไว้ (มันคือกติกาที่ถูกต้อง
  ตาม rules_of_work §1.3 อยู่แล้ว — Drawer/Dialog ที่มี Combobox ให้ใส่ `modal={false}`)
- อย่าลบ `input-group.tsx` ถ้าเลิกใช้ Combobox — เช็คก่อน (ตอนนี้ `input-group` ถูกใช้
  โดย combobox เท่านั้น)

---

_อ้างอิงคำสั่งที่ใช้ตรวจสอบ: `npx shadcn@latest info`, `npx shadcn@latest docs combobox`,
`npx shadcn@latest view @shadcn/combobox`_
