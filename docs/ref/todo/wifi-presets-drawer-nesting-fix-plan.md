# Wi-Fi Presets — แก้ Dialog ซ้อน Drawer ผิดพลาด (Drawer-in-Drawer)

> แผนแก้บั๊ก UI ของฟีเจอร์ Wi-Fi Presets (`feat/wifi-presets`, PR #73): Dialog ที่เปิดจากภายใน
> Edit Interface Drawer ชนกับ Drawer เดิม (คนละ overlay library คุม focus-trap/body-lock เอง)
> ทำให้ Combobox กดไม่ติดและหน้าค้างหลังปิด — แปลงเป็น **nested Drawer** (vaul `NestedRoot`) แทน
> Dialog ทุกจุดที่ถูกเปิดซ้อนบน Drawer อื่น ส่วน alert/confirm box ที่ไม่ได้ซ้อนยังคงเป็น Dialog เดิม
>
> วันที่เขียน: 2026-07-22 · Branch อ้างอิง: `feat/wifi-presets`
> Status ใน README Feature Status: Wi-Fi Presets = Completed (ไม่เปลี่ยน — นี่คือ bug fix ของฟีเจอร์ที่ทำเสร็จแล้ว ไม่ใช่ mock→real)

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย (พฤติกรรมที่ผู้ใช้เห็น):** ในหน้า Interfaces เมื่อเปิด Edit Drawer ของ WLAN interface แล้วกด
  "โหลดจาก Saved Network" หรือ "บันทึกเป็น Preset" ต้องได้ Drawer ใบที่สองเปิดซ้อนทับ Edit Drawer แบบ
  nested (ไม่ใช่ Dialog) — Combobox ใน "โหลดจาก Saved Network" ต้องกด dropdown แล้วเลือกรายการได้ปกติ,
  ปิด Drawer ที่ซ้อนอยู่แล้วต้องไม่ค้าง/กดหน้าจอไม่ได้ และต้องไม่ปิด Edit Drawer ที่เป็นแม่ไปด้วย
- **เงื่อนไขเชิงเทคนิค:** Dialog เก็บไว้ใช้เฉพาะกรณี alert/confirm ที่ไม่ได้ถูกเปิดซ้อนบน overlay อื่น
  (ตามที่ owner ล็อกไว้) — ฟอร์ม/picker ที่ถูกเปิดจากภายใน Drawer อีกใบ ต้องเป็น Drawer เสมอ
- **นอกขอบเขต:**
  - ไม่แตะ global confirm/alert (`AlertDialogProvider.tsx`) — คงเป็น `Dialog modal={true}` ตามเดิม เพราะ
    ไม่เคยถูกเปิดซ้อนบน Drawer อื่น (เป็น alert box แท้ ๆ ตามที่ owner ต้องการ)
  - ไม่แตะ backend/API ใด ๆ — เป็นบั๊ก UI overlay ล้วน ๆ ไม่มี endpoint เปลี่ยน
  - ไม่เปลี่ยน business logic ของฟอร์ม (validation, write-only password, one-way sync, primary/backup
    slot) — ย้ายแค่ overlay type
  - ไม่ทำ `AlertDialog` primitive ใหม่ (ยังไม่มีติดตั้งใน `components.json`/`components/ui/` และไม่จำเป็น
    เพราะ `AlertDialogProvider.tsx` ทำหน้าที่นี้อยู่แล้วด้วย `Dialog`)

## 1. Current State (สำรวจโค้ดจริง ณ วันที่เขียน)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Edit Interface Drawer (parent) | มีอยู่แล้ว, ไม่แตะ | `frontend/src/pages/Interfaces.tsx:1407-1963` — `<Drawer direction="right" open={isEditOpen} onOpenChange={setIsEditOpen}>` |
| ปุ่ม "โหลดจาก Saved Network" (trigger) | มีอยู่แล้ว, ไม่แตะ | `Interfaces.tsx:1576` เรียก `openApplyPresetDialog` (บรรทัด 529) ซึ่งอยู่ภายใน Edit Drawer เสมอ |
| ปุ่ม "บันทึกเป็น Preset" (trigger) | มีอยู่แล้ว, ไม่แตะ | `Interfaces.tsx:1588` เรียก `openSaveCurrentAsPresetDialog` (บรรทัด 453) ซึ่งอยู่ภายใน Edit Drawer เสมอ |
| Preset Create/Edit — **ต้องแก้** | ปัจจุบันเป็น `Dialog` (บรรทัด 1965-1966 มี comment เก่าอ้างว่า "ไม่มี Combobox เลยใช้ modal ปกติได้" — comment นี้ไม่ผิด แต่พลาดเคส "ถูกเปิดจากใน Edit Drawer" ตอน Save-as-Preset) | `Interfaces.tsx:1967-2094` — `<Dialog open={isPresetDialogOpen} onOpenChange={setIsPresetDialogOpen}>` ใช้ instance เดียวทั้ง 3 ทาง: panel "New Preset" (บรรทัด 1311, `openCreatePresetDialog` 427), panel row "edit" (บรรทัด 1370, `openEditPresetDialog` 438), และ "บันทึกเป็น Preset" ในกรณีที่ 2 ทางแรก Edit Drawer ปิดอยู่ แต่ทางที่ 3 Edit Drawer เปิดอยู่ → **Dialog-in-Drawer** |
| Apply-from-Saved-Network — **ต้องแก้** | ปัจจุบันเป็น `Dialog modal={false}` (comment บรรทัด 2096-2098 อธิบายเหตุผลเดิมของ `modal={false}` ตรงตาม `docs/rules_of_work.md` แต่ไม่ได้แก้ปัญหาการซ้อนกับ Drawer) | `Interfaces.tsx:2099-2193` มี `Combobox`/`ComboboxContent` ที่บรรทัด 2122-2142 ถูกเปิดจากภายใน Edit Drawer เสมอ (trigger เดียวที่ 1576) → **Dialog-in-Drawer เสมอ** |
| Global confirm/alert — ไม่แตะ | เป็น `Dialog modal={true}` ระดับ app-root ไม่เคยถูกเปิดซ้อนบน overlay อื่น | `frontend/src/components/AlertDialogProvider.tsx:56-93`, เรียกผ่าน `handleDeletePreset` (`Interfaces.tsx:504`) และ error `alert()` ทั่วไฟล์ |
| `DrawerNested` wrapper — **ยังไม่มี** | `components/ui/drawer.tsx` export แค่ `Root`/`Trigger`/`Portal`/`Close`/`Overlay`/`Content`/`Header`/`Footer`/`Title`/`Description` (บรรทัด 118-129) ไม่มี wrapper ของ `NestedRoot` | `frontend/src/components/ui/drawer.tsx` |
| vaul รองรับ nested drawer อยู่แล้ว (dependency มีของพร้อมใช้) | มี `NestedRoot` export ใน package ที่ติดตั้งอยู่ v1.1.2 | `frontend/node_modules/vaul/dist/index.d.mts:123,128,141` — ไม่ต้องเพิ่ม dependency ใหม่ |
| Pattern ต้นแบบ Combobox-in-Drawer ที่ทำถูกอยู่แล้วในโปรเจกต์ | ใช้งานจริง ไม่มีปัญหา (ไม่ใช้ `modal={false}` เลย เพราะทั้งคู่เป็น vaul Drawer, ไม่มี Dialog มาชน) | `frontend/src/pages/FirewallPolicy.tsx:423-430` (`useComboboxAnchor()`, `drawerContentRef`), `:800-811` (`onEscapeKeyDown` guard เช็ค `[data-slot="combobox-content"]`), `:864,943,981` (`<ComboboxContent anchor={...} container={drawerContentRef} data-vaul-no-drag>`) |
| `ComboboxContent` รองรับ `container`/`anchor` prop | รองรับอยู่แล้ว ไม่ต้องแก้ | `frontend/src/components/ui/combobox.tsx:95-105` (`Portal container={container}` + `anchor={anchor}`) |
| README Feature Status | "Wi-Fi Presets (Saved Networks) = Completed/Completed" อยู่แล้ว ไม่ต้องแก้ | `README.md:95` |

สรุป: งานจริงกระจุกอยู่ที่ `frontend/src/pages/Interfaces.tsx` 2 บล็อก (บรรทัด 1967-2094 และ 2099-2193) บวกเพิ่ม wrapper 1 ตัวใน `drawer.tsx` — ไม่มีฝั่ง backend เกี่ยวข้อง

## 2. Technical Approach

**กลไกที่เลือก:** ใช้ vaul's `Drawer.NestedRoot` (มีอยู่แล้วใน dependency, ไม่ต้องเพิ่ม package ใหม่) แทน
Radix `Dialog` สำหรับทั้ง 2 overlay ที่ถูกเปิดจากภายใน Edit Drawer เสมอ `NestedRoot` ถูกออกแบบมาให้
coordinate กับ parent `Drawer.Root` โดยตรง (scale parent ย่อกลับ, จัดการ body/focus-trap ใน library
เดียวกันทั้งหมด) จึงไม่มี race ระหว่าง 2 overlay library แบบที่เกิดกับ Dialog-in-Drawer

```tsx
// components/ui/drawer.tsx — เพิ่ม wrapper ใหม่ (ตาม pattern ของ Drawer เดิม)
function DrawerNested({
  ...props
}: React.ComponentProps<typeof DrawerPrimitive.NestedRoot>) {
  return <DrawerPrimitive.NestedRoot data-slot="drawer-nested" {...props} />
}
```

ข้อสำคัญ (พบระหว่างสำรวจ `node_modules/vaul`): `NestedRoot` จะ throw
`"Drawer.NestedRoot must be placed in another drawer"` ถ้าไม่ได้ render อยู่ใต้ context ของ parent
`<Drawer.Root>` จริง ๆ — เพราะฉะนั้น JSX ของทั้ง 2 overlay ที่แก้ต้อง**ย้ายเข้าไปอยู่ใน subtree ของ
`<DrawerContent>` ของ Edit Drawer** (บรรทัด 1407-1963) ไม่ใช่วางเป็น sibling ระดับ page เหมือน Dialog เดิม

สำหรับ Combobox ใน Apply-from-Saved-Network ให้ใช้ pattern เดียวกับที่ `FirewallPolicy.tsx` ทำสำเร็จแล้ว
(portal `ComboboxContent` เข้าไปอยู่ใน `DrawerContent` เดียวกันผ่าน `container` ref + `anchor` จาก
`useComboboxAnchor()` + guard `onEscapeKeyDown` ไม่ให้ Escape หลุดไปปิด Drawer ทั้งที่ Combobox popup ยัง
เปิดอยู่) วิธีนี้ทำให้**ไม่ต้องพึ่ง `modal={false}` อีกต่อไป** เพราะไม่มี Dialog ให้ conflict กับ Drawer แล้ว

**ทางเลือกที่พิจารณาแล้วปฏิเสธ:**
- **คง `Dialog modal={false}` ไว้แล้วพยายามแก้ z-index/pointer-events เอง** — ปฏิเสธ เพราะเป็นการแก้ปลายเหตุ
  (patch เฉพาะอาการ) ทั้งที่ต้นเหตุคือ 2 overlay library คุม body state คนละชุดกัน โอกาสเกิด edge case ใหม่สูง
  และขัดกับที่ owner สั่งชัดเจนแล้วว่าอยากได้ Drawer-in-Drawer
- **สร้าง `AlertDialog` primitive ใหม่แยกจาก Dialog สำหรับ confirm/alert** — ปฏิเสธ (นอกขอบเขต):
  `AlertDialogProvider.tsx` ที่มีอยู่ทำหน้าที่ alert/confirm ได้ครบแล้วด้วย `Dialog` เดิม ไม่ได้ถูกเปิดซ้อน
  บน Drawer จึงไม่มีปัญหาต้องแก้ — เพิ่ม primitive ใหม่จะเป็นการขยายงานเกินโจทย์บั๊กนี้
- **รวม Preset Create/Edit กับ Save-as-Preset ให้เป็น instance เดียวโดยใช้ `Drawer` ธรรมดา (ไม่แยก
  nested)** — ปฏิเสธ เพราะทางที่เปิดจาก panel (Edit Drawer ปิดอยู่) กับทางที่เปิดจากใน Edit Drawer
  (Save-as-Preset) มีบริบท parent ต่างกัน — ถ้าใช้ `NestedRoot` เป็น instance เดียวแล้วเปิดตอน Edit Drawer
  ปิดอยู่จะพัง (ไม่มี parent ให้ nest) จึงต้องแยก 2 instance ตาม §3 ด้านล่าง

**Template ในโค้ดที่ให้ยึดตาม:** `frontend/src/pages/FirewallPolicy.tsx` สำหรับวิธีวาง Combobox ใน Drawer
โดยไม่ใช้ `modal={false}`; `components/ui/drawer.tsx` สำหรับ style ของ wrapper ตัวใหม่ (ให้หน้าตาเหมือน
`Drawer`/`DrawerContent` เดิมทุกอย่าง มีแค่ primitive ข้างในเปลี่ยน)

## 3. Steps (เรียงตาม dependency, ในไป-นอก)

### Step 1 — เพิ่ม `DrawerNested` wrapper
**File:** `frontend/src/components/ui/drawer.tsx` (แก้ไฟล์เดิม)
เพิ่มฟังก์ชัน `DrawerNested` ตามโค้ดตัวอย่างใน §2 (ห่อ `DrawerPrimitive.NestedRoot`, `data-slot="drawer-nested"`)
แล้ว export เพิ่มในบรรทัด 118-129 ไม่แตะ export/behavior เดิมตัวอื่น

### Step 2 — แยกฟอร์ม Preset ออกมาใช้ซ้ำ (ยังไม่เปลี่ยน overlay type)
**File:** `frontend/src/pages/Interfaces.tsx:1979-2091` (ส่วน `<form onSubmit={handleSavePreset}>` ถึง footer)
แยกเนื้อฟอร์ม (name/SSID/security/mac-mode/password + `presetFormError` Alert + ปุ่ม Cancel/Save) ออกเป็น
component ย่อยในไฟล์เดียวกัน เช่น `PresetFormFields` รับ props เป็น state/setter ที่มีอยู่แล้วทั้งหมด
(`presetFormName/SSID/Security/MacMode/Password`, `editingPreset`, `presetSubmitting`, `presetFormError`,
`handleSavePreset`) เพื่อให้เรียกใช้ได้ทั้งจาก Drawer หน้า page และ nested Drawer ใน Step 4 — ยังคงเรียก
ใน `<Dialog>` เดิมไปก่อนในสเต็ปนี้ (pure refactor ไม่เปลี่ยนพฤติกรรม เพื่อแยก concern ให้ diff ของ step ถัดไป
อ่านง่าย)

### Step 3 — แปลง Preset Create/Edit (ทางที่เปิดจาก panel ตรง ๆ) เป็น Drawer หน้า page
**File:** `frontend/src/pages/Interfaces.tsx:1967-2094`
เปลี่ยน `<Dialog open={isPresetDialogOpen} onOpenChange={setIsPresetDialogOpen}>` เป็น
`<Drawer direction="right" open={isPresetDialogOpen && !isEditOpen} onOpenChange={setIsPresetDialogOpen}>`
ใช้ `DrawerContent`/`DrawerHeader`/`DrawerTitle`/`DrawerDescription` + `PresetFormFields` จาก Step 2 เงื่อนไข
`&& !isEditOpen` กันไม่ให้ instance นี้โผล่ตอนถูกเปิดจากใน Edit Drawer (นั่นคืองานของ Step 4) ปุ่ม
trigger เดิม (`openCreatePresetDialog`, `openEditPresetDialog`) ไม่ต้องแก้ เพราะยัง set
`isPresetDialogOpen` ตัวเดิม

> ไม่ต้องใช้ `DrawerNested` ในสเต็ปนี้ เพราะ instance นี้ไม่เคยถูกเปิดซ้อนบน Drawer อื่น (`isEditOpen`
> เป็น false เสมอตอนเปิดทางนี้) — `Drawer` ธรรมดาก็พอ

### Step 4 — เพิ่ม nested Drawer "บันทึกเป็น Preset" ในกรณีเปิดจาก Edit Drawer
**File:** `frontend/src/pages/Interfaces.tsx` — วางโค้ดใหม่ **ภายใน** `<DrawerContent>` ของ Edit Drawer
(ที่ไหนสักที่ก่อนบรรทัด 1962 ที่ `</DrawerContent>` ปิด ไม่ใช่วางถัดจากบรรทัด 1963 แบบ Dialog เดิม)
```tsx
<DrawerNested direction="right" open={isPresetDialogOpen && isEditOpen} onOpenChange={setIsPresetDialogOpen}>
  <DrawerContent>
    {/* PresetFormFields จาก Step 2 */}
  </DrawerContent>
</DrawerNested>
```
เงื่อนไข `&& isEditOpen` คู่กับ Step 3 ทำให้ 2 instance ไม่เปิดพร้อมกันเด็ดขาด (ปุ่ม trigger
`openSaveCurrentAsPresetDialog` เข้าถึงได้เฉพาะตอน Edit Drawer เปิดอยู่เท่านั้น) ปุ่ม trigger เดิม
(บรรทัด 1588) ไม่ต้องแก้ ตรวจสอบว่าปิด nested Drawer นี้แล้ว (`handleSavePreset` เรียก
`setIsPresetDialogOpen(false)`) ไม่ทำให้ Edit Drawer (`isEditOpen`) ปิดตามไปด้วย

> ทางเลือกที่ตรงไปกว่านี้ (ใน §2) คือแยก boolean คนละตัวสำหรับ nested instance แทนการ gate ด้วย
> `isEditOpen` ร่วมกับ state เดิม — ถ้าตอนแก้จริงเจอว่า gate แบบนี้อ่านยาก ให้แยก state ใหม่ได้ แต่ต้องแก้
> `handleSavePreset` ให้ปิดทั้งคู่

### Step 5 — แปลง Apply-from-Saved-Network เป็น nested Drawer + ย้าย Combobox ไป container เดียวกัน
**File:** `frontend/src/pages/Interfaces.tsx:2099-2193` — ย้ายเข้าไปอยู่ใน subtree ของ Edit Drawer
เหมือน Step 4 (เพราะ trigger เดียว บรรทัด 1576 อยู่ในนั้นเสมอ)
```tsx
<DrawerNested direction="right" open={isApplyPresetOpen} onOpenChange={setIsApplyPresetOpen}>
  <DrawerContent ref={applyDrawerContentRef} onEscapeKeyDown={(e) => {
    if (document.querySelector('[data-slot="combobox-content"]')) e.preventDefault()
  }}>
    {/* ...header/description/error alert เดิม... */}
    <Combobox items={presetComboboxItems} value={applyPresetSelection} onValueChange={...}>
      <ComboboxInput ... />
      <ComboboxContent anchor={applyPresetAnchor} container={applyDrawerContentRef} data-vaul-no-drag ...>
        {/* เดิม */}
      </ComboboxContent>
    </Combobox>
    {/* ...primary/backup toggle + footer เดิม... */}
  </DrawerContent>
</DrawerNested>
```
ต้องเพิ่ม `const applyDrawerContentRef = useRef<HTMLDivElement | null>(null)` และ
`const applyPresetAnchor = useComboboxAnchor()` (import `useComboboxAnchor` จาก
`@/components/ui/combobox` เพิ่ม — ดู `FirewallPolicy.tsx:423-430`) ลบ `modal={false}` ออกทั้งหมด
(ไม่มี Dialog เหลือให้ prop นี้ใช้แล้ว) คง `applyPresetError`, `applyPresetSelection`,
`applyPresetSlot`, `applyPresetSubmitting`, `handleApplyPreset` (รวม post-apply flow ที่เรียก
`loadData` แล้ว `openEditDialog(updatedIface)` เพื่อ repopulate ฟอร์ม Edit Drawer) และ `useEffect`
ที่บรรทัด 654-662 (reset `isApplyPresetOpen` เมื่อ `isEditOpen` เป็น false) ไว้ทั้งหมดโดยไม่แก้ logic

> จุดนี้คือจุดที่ bug รายงานมาโดยตรง (Combobox กดไม่ติด) ต้องทดสอบมือละเอียดสุดตาม DoD ด้านล่าง

### Step 6 — ล้าง import/comment ที่ไม่ใช้แล้ว
**File:** `frontend/src/pages/Interfaces.tsx` (import block บรรทัด 51-58)
ลบ import `Dialog, DialogContent, DialogDescription, DialogFooter, DialogHeader, DialogTitle` **เฉพาะถ้า**
ไม่มีจุดไหนในไฟล์นี้เรียกใช้แล้ว (ตรวจก่อนลบ — `AlertDialogProvider.tsx` เป็นไฟล์แยก ไม่กระทบ) แก้
comment เก่าที่บรรทัด 1965-1966 และ 2096-2098 (อ้างถึง Dialog/`modal={false}`) ให้ตรงกับโครงสร้าง nested
Drawer ใหม่

> ไม่ต้องแตะ `AlertDialogProvider.tsx` — ยังคงเป็น `Dialog modal={true}` ตามเดิม (out of scope ตาม §0)

## 4. Related API

ไม่มี — งานนี้เป็น frontend overlay refactor ล้วน ๆ ไม่มี route/handler/service/DB เปลี่ยนแปลง
(`docs/openapi.yaml` และ `frontend/public/openapi.yaml` ไม่ต้องแก้)

## 5. Cautions

- **`NestedRoot` ต้องอยู่ใต้ parent `<Drawer.Root>` context จริง ๆ** — ถ้าใครในอนาคตย้ายโค้ด Step 4/5
  ออกไปวางเป็น sibling ระดับ page (เหมือนที่ Dialog เดิมเคยอยู่) จะได้ error
  `"Drawer.NestedRoot must be placed in another drawer"` ทันทีตอน runtime ป้องกันด้วยการคง JSX ไว้ใน
  subtree ของ `<DrawerContent>` ของ Edit Drawer เท่านั้น
- **Gate ด้วย `isEditOpen` ทำให้ Step 3 กับ Step 4 ใช้ boolean เดียวกัน (`isPresetDialogOpen`) แต่คนละ
  instance** — ถ้าแก้ `handleSavePreset` ในอนาคตแล้วลืมว่ามี 2 จุดเรียก `setIsPresetDialogOpen`
  (จาก 2 instance ที่ mount ต่างกันตาม `isEditOpen`) อาจงงว่าปิดยังไงไม่ยอมปิด — คอมเมนต์ในโค้ดควรระบุ
  ความสัมพันธ์นี้ชัดเจน (ดูหมายเหตุท้าย Step 4)
- **Escape key ambiguity เดิมที่เป็นต้นเหตุบั๊ก** — ถ้า guard `onEscapeKeyDown` ใน Step 5 ไม่ได้ check
  `[data-slot="combobox-content"]` ก่อน `preventDefault()` (เช่น สลับลำดับเงื่อนไข) Escape ครั้งแรกจะปิด
  nested Drawer ทั้งใบทั้งที่ผู้ใช้แค่อยากปิด dropdown ของ Combobox — ต้องทดสอบมือตามลำดับ: เปิด dropdown
  → Escape (ปิดแค่ dropdown) → Escape อีกที (ปิด nested Drawer) → Edit Drawer (parent) ต้องยังเปิดอยู่
- **Testing:** บั๊กนี้เป็น overlay stacking/focus-trap/pointer-event ซึ่ง **automated test (`yarn build`,
  `yarn lint`) จับไม่ได้** ต้องรัน `yarn dev` แล้วคลิกทดสอบจริงในเบราว์เซอร์ทั้ง flow "บันทึกเป็น Preset"
  และ "โหลดจาก Saved Network" (รวม Combobox dropdown คลิกได้จริง, ปิด nested Drawer ไม่ทำให้หน้าค้าง,
  ปิด nested ไม่ลาม parent) ทั้ง dark และ light mode — ทดสอบใน mock mode ได้ครบ ไม่ต้องใช้ real board
  (ไม่มีการเรียก D-Bus/Netlink ใหม่ในงานนี้)
- **Style ตาม `docs/rules_of_work.md`:** ทุก Drawer ใหม่ต้องใช้ theme variable เดิม (ไม่ hardcode สี),
  flat design (ไม่มี `shadow-*`/`backdrop-blur-*` ใหม่), รองรับ dark/light ทั้งคู่ — เทียบกับ Edit Drawer
  เดิม (1407-1963) และ VLAN Drawer เดิม (2197 เป็นต้นไป) เป็นตัวอย้างสไตล์ที่ถูกต้องอยู่แล้ว

## 6. Summary Checklist (Definition of Done)

- [ ] `frontend/src/components/ui/drawer.tsx` — เพิ่ม `DrawerNested` wrapper + export
- [ ] `Interfaces.tsx` — แยก `PresetFormFields` (หรือเทียบเท่า) ใช้ซ้ำได้ทั้ง 2 ที่
- [ ] `Interfaces.tsx:1967-2094` — Preset Create/Edit (ทาง panel) เป็น `Drawer` หน้า page, gate `!isEditOpen`
- [ ] `Interfaces.tsx` — เพิ่ม nested `DrawerNested` "บันทึกเป็น Preset" ในกรณี Save-as-Preset, gate `isEditOpen`
- [ ] `Interfaces.tsx:2099-2193` — Apply-from-Saved-Network เป็น nested `DrawerNested`, Combobox portal เข้า
      `container` เดียวกัน (`useComboboxAnchor` + `onEscapeKeyDown` guard), ลบ `modal={false}`
- [ ] ลบ import Dialog ที่ไม่ใช้แล้ว + อัปเดต comment เก่าใน `Interfaces.tsx`
- [ ] `cd frontend && yarn build` ผ่าน (ไม่มี type error/unused import)
- [ ] `cd frontend && yarn lint` ผ่าน ไม่มี warning ใหม่
- [ ] ทดสอบมือ (mock mode, `yarn dev`): New/Edit preset จาก panel เปิดเป็น Drawer, สร้าง/แก้ไข/ลบสำเร็จ
- [ ] ทดสอบมือ: "บันทึกเป็น Preset" จากใน Edit Drawer เปิด nested Drawer ซ้อนถูกต้อง, ปิดแล้วหน้าไม่ค้าง,
      Edit Drawer แม่ยังเปิดอยู่
- [ ] ทดสอบมือ: "โหลดจาก Saved Network" เปิด nested Drawer, Combobox dropdown กดเลือกได้จริง, Apply แล้ว
      ฟอร์ม Edit Drawer refresh ค่าที่ apply ไปถูกต้อง, nested Drawer ปิดเอง
- [ ] ทดสอบมือ: Escape key ปิด Combobox popup ก่อน แล้วค่อยปิด nested Drawer ในกด Escape ครั้งถัดไป
- [ ] ทดสอบมือ: dark mode และ light mode ทั้งคู่ ไม่มีสี hardcode/shadow ใหม่หลุดเข้ามา
- [ ] ไม่ต้องอัปเดต README/openapi (ไม่มี API/status เปลี่ยน)
