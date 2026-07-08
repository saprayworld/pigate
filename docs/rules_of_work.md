# PiGate Frontend - Rules of Work & Work Instructions (WI)

เอกสารฉบับนี้สรุปกฎเกณฑ์และแนวทางการพัฒนาหน้าบ้าน (Frontend Development Guidelines) สำหรับระบบ **PiGate** เพื่อรักษาความเป็นระบบระเบียบ ความสวยงามสม่ำเสมอ และความง่ายในการพัฒนาระบบร่วมกัน

---

## 1. กฎการใช้งาน UI Components

### 1.1 ใช้ shadcn/ui เป็นคอมโพเนนต์พื้นฐาน (Base Components)
* **ข้อกำหนด:** องค์ประกอบ UI ทุกชิ้นในระบบ (เช่น ปุ่ม, กล่องกรอกข้อมูล, ป้ายสถานะ, เมนูตัวเลือก, กล่องหน้าต่างแจ้งเตือน) **ต้องพัฒนาขึ้นโดยใช้คอมโพเนนต์ของ shadcn/ui เป็นตัวหลัก** 
* **โฟลเดอร์เก็บโค้ด:** คอมโพเนนต์เหล่านั้นจะเก็บอยู่ที่โฟลเดอร์ [`src/components/ui/`](file:///home/sapray/dev/pigate/frontend/src/components/ui/)
* **เหตุผล:** เพื่อป้องกันการสไตล์ที่ซ้ำซ้อน รักษาระบบดีไซน์ (Design System) ให้เหมือนกันทั่วทั้งโปรเจกต์ และลดขนาด Bundle ของโปรแกรม

### 1.2 การติดตั้ง Component ของ shadcn เพิ่มเติม
* **ข้อกำหนด:** หากต้องการใช้งาน Component ใหม่ของ shadcn ที่ยังไม่มีในโปรเจกต์ (เช่น `dialog`, `select`, `table`) **ต้องใช้คำสั่งผ่าน `npx` แทน `yarn`**
* **รูปแบบคำสั่ง (รันที่โฟลเดอร์ `frontend/`):**
  ```bash
  npx shadcn@latest add <ชื่อคอมโพเนนต์>
  ```
* **เหตุผลสำคัญ:** ตัวจัดการแพ็กเกจของโครงการในปัจจุบันใช้ Yarn รุ่น 1 (Yarn v1) ซึ่ง**ไม่รองรับคำสั่ง `yarn dlx`** การพยายามรันติดตั้งผ่าน `yarn dlx` จะล้มเหลว ดังนั้นจึงกำหนดให้ใช้ `npx` เป็นมาตรฐานหลักแทน

### 1.3 การเลือกใช้ Drawer กับ Dialog และการจัดการ Portal Components ข้างใน

#### 1.3.1 Drawer สำหรับฟอร์มจัดการข้อมูล, Dialog สำหรับการยืนยัน
* **ข้อกำหนด:** ฟอร์ม **เพิ่ม / แก้ไข / ตั้งค่า** ข้อมูลทุกหน้า (create / edit / config) **ต้องเปิดเป็น `<Drawer direction="right">`** (vaul, เลื่อนเข้าจากขวาของจอ) ไม่ใช่ `<Dialog>` กลางจอ เพื่อ UX ที่สม่ำเสมอและได้พื้นที่แนวตั้งเต็มจอสำหรับฟอร์มยาว
* **สงวน `<Dialog>` ไว้สำหรับ "การยืนยัน (confirmation)" เท่านั้น** เช่น `useAlert().alert/confirm` (`AlertDialogProvider.tsx`) และ dialog ยืนยัน Reboot/Shutdown ใน `SettingsMaintenance.tsx` — อย่าเปลี่ยนพวกนี้เป็น Drawer
* **รูปแบบมาตรฐาน** (คุมด้วย `open`/`onOpenChange` state เดิม ไม่ต้องใช้ `DrawerTrigger`):
  ```tsx
  <Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
    <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
      <DrawerHeader className="border-b border-border/50">
        <DrawerTitle className="text-base font-semibold">…</DrawerTitle>
      </DrawerHeader>
      <div className="flex-1 overflow-y-auto p-4">
        <form onSubmit={handleSave} className="space-y-4 text-sm">…</form>
      </div>
    </DrawerContent>
  </Drawer>
  ```
  * **การ override ความกว้าง ต้องใช้ variant prefix เต็ม** `data-[vaul-drawer-direction=right]:sm:max-w-[NNNpx]` เท่านั้น — ถ้าใส่ `sm:max-w-[NNNpx]` เฉย ๆ จะโดน selector ค่า default (`…direction=right]:sm:max-w-sm`) ทับแบบเงียบ ๆ เพราะ specificity สูงกว่า
  * **ต้องห่อ `<form>` ด้วย `<div className="flex-1 overflow-y-auto p-4">`** เพราะ `DrawerContent` เป็น flex-col เต็มความสูงจอ ไม่มี padding/scroll ในตัวเหมือน `DialogContent` เดิม — ถ้าไม่ห่อ ฟอร์มยาวจะล้นจอและปุ่ม Save หลุดออกนอกจอ
  * ปุ่ม Cancel/Save คงไว้ท้าย `<form>` (ปุ่ม `type="submit"` ต้องอยู่ใน form) ไม่ต้องย้ายไป `DrawerFooter`

#### 1.3.2 กติกา Combobox inside overlay (portal เข้า DrawerContent — ไม่ใช้ `modal={false}` แล้ว)
* vaul Drawer สร้างอยู่บน Radix Dialog จึงมี Focus/Pointer Blocker — หากมี **input ฟิลด์แบบ Combobox** (Portal component ของ Base UI) อยู่ข้างในแล้ว popup ถูก portal ไปที่ `<body>` (ค่า default) การคลิกดรอปดาวน์จะโดนบล็อกเพราะ overlay มองว่าเป็น Interact Outside
* **แนวทางปฏิบัติ (อัปเดต 2026-07-08 — แทนกติกา `modal={false}` เดิม):**
  * **portal popup ของ Combobox เข้าไปใน DrawerContent** แทนการปิด modal: ผูก ref กับ `<DrawerContent ref={drawerContentRef}>` แล้วส่ง `container={drawerContentRef}` ให้ `<ComboboxContent>` (prop นี้ส่งต่อไปยัง Base UI `Portal`) — popup จะอยู่ใน subtree ของ Dialog จึงไม่โดนทั้ง pointer blocker และ outside-click dismiss โดยคง modal behavior เต็ม (overlay/scroll-lock/คลิกนอกเพื่อปิด)
  * ใส่ `data-vaul-no-drag` บน `<ComboboxContent>` กันการลาก popup ไปโดน drag ของ vaul
  * ใส่ `onEscapeKeyDown` guard บน `DrawerContent`: ถ้ามี `[data-slot="combobox-content"]` เปิดอยู่ ให้ `e.preventDefault()` — ไม่งั้นกด Esc ขณะ popup เปิดจะปิดทั้ง Drawer (Base UI กับ Radix ไม่ประสาน escape-layer กัน)
  * ตัวอย่างจริง: `FirewallPolicy.tsx` (Combobox chips ×3) — ทดสอบจริงแล้ว (2026-07-08) ผ่านทั้งคลิกเลือก/พิมพ์กรอง/ลบ chip/Esc/คลิกนอก
  * Drawer/Dialog ที่ข้างในมีเพียง **native `<select>` หรือ shadcn `Select` (Radix) ปกติ ไม่ต้องทำอะไรพิเศษ** — ทดสอบจริงแล้ว (2026-07-08) ว่า dropdown เหล่านี้คลิกได้ตามปกติภายใต้ modal ค่า default
* **เหตุผล:** คงพฤติกรรม modal (คลิกนอก/Esc/scroll-lock) ของ overlay ไว้ครบทุกหน้า แทนที่จะปิด pointer blocker ทั้งใบด้วย `modal={false}` แบบเดิม

---

## 2. กฎการเลือกใช้สไตล์และโทนสี (Styling & Theme Rules)

### 2.1 ระบบธีมสีเข้มและสีสว่าง (Dark & Light Mode Support)
* หน้าต่างควบคุมระบบถูกออกแบบมารองรับทั้ง **ธีมสีเข้ม (Dark Mode)** และ **ธีมสีสว่าง (Light Mode)** อย่างเต็มรูปแบบ เพื่อตอบโจทย์ความยืดหยุ่นในการใช้งาน โดยมีลักษณะการใช้งานสีดังนี้:
* **สีพื้นหลังหลัก (Background):**
  * สำหรับ **Dark Mode:** ใช้โทนสีดำ-เทาเข้ม (`bg-neutral-950`, `bg-neutral-900` หรือตามตัวแปรระบบ `var(--background)`)
  * สำหรับ **Light Mode:** ใช้โทนสีขาว-เทาอ่อน (`bg-white`, `bg-neutral-50` หรือตามตัวแปรระบบ `var(--background)`)
* **สีสถานะการใช้งาน (Active/Highlight):** จะต้องปรับใช้ให้มองเห็นได้ชัดเจนและมีสีสันสอดรับกับแต่ละธีม:
  * ใช้สีเขียวมรกตหลักของระบบผ่านตัวแปรสี (`text-primary` / `bg-primary`) เพื่อแสดงสถานะที่เป็นปกติ (เช่น Active, Allowed, Power OK)
  * ใช้สีฟ้าคราม (`text-cyan-400` ใน Dark Mode / `text-cyan-600` ใน Light Mode) หรือ สีน้ำเงินอินดิโก (`text-indigo-400` / `text-indigo-600`) สำหรับกราฟหรือแถบข้อมูลเครือข่าย
  * ใช้สีส้ม/เหลือง (`text-amber-500` ใน Dark Mode / `text-amber-600` ใน Light Mode) สำหรับค่าระดับกลาง/คำเตือน (เช่น อุณหภูมิบอร์ด)
  * ใช้สีแดง (`text-red-500` / `bg-red-500` หรือ `text-red-600` ตามแต่ละธีม) สำหรับสถานะการบล็อก (Blocked) หรือ ข้อผิดพลาด (Errors)

### 2.2 หลีกเลี่ยงการ Hardcode สีหลัก (Avoiding Hardcoded Colors)
* **ข้อกำหนด:** ห้ามเขียน Class สีเขียวของ Tailwind ตรงๆ ลงในโค้ด (เช่น `text-emerald-500`, `bg-emerald-500`, `border-emerald-500/20`) เพื่อให้สามารถควบคุมสไตล์สีหลักได้จากศูนย์กลาง
* **แนวทางปฏิบัติ:** ให้เรียกใช้ผ่านตัวแปรสีหลักของระบบตามที่ประกาศไว้ใน [`src/index.css`](file:///home/sapray/Dev/pigate/frontend/src/index.css) เสมอ เช่น:
  * ใช้ `text-primary` แทน `text-emerald-500` หรือ `dark:text-emerald-400`
  * ใช้ `bg-primary/10` หรือ `border-primary/20` แทน `bg-emerald-500/10` หรือ `border-emerald-500/20`
  * ใช้ `bg-primary` และ `text-primary-foreground` สำหรับปุ่มกดหลัก (Primary Button)
* **เหตุผล:** เพื่อรองรับการปรับแต่งและเปลี่ยนสไตล์สีหลัก (Rebranding) ของระบบได้ง่ายจากศูนย์กลางจุดเดียวในอนาคต

### 2.3 กฎการออกแบบสไตล์ Flat Design (Flat Style & Effect Rules)
* **ข้อกำหนด:** ระบบถูกออกแบบมาเป็น **Flat Premium Design** ห้ามใช้เงา (Shadow) หรือเอฟเฟกต์เบลอ (Blur) ในตัวควบคุมเลย์เอาต์และการ์ดแสดงผลทั้งหมด
* **แนวทางปฏิบัติ:**
  * หลีกเลี่ยงการใส่คลาสเงา เช่น `shadow`, `shadow-sm`, `shadow-md`, `shadow-lg`, `shadow-2xl`, `shadow-xs` ลงในส่วนประกอบ UI
  * หลีกเลี่ยงการใช้คลาสเบลอพื้นหลัง เช่น `backdrop-blur`, `backdrop-blur-sm`, `backdrop-blur-md`
  * ทุกการปรับปรุงชุดตัวแปรของธีมใน [`src/index.css`](file:///home/sapray/dev/pigate/frontend/src/index.css) จะต้องประกาศตัวแปรเงาและเบลอของ Tailwind เป็น `none` และ `0px` เสมอ (เพื่อไม่ให้ปลั๊กอิน UI ภายนอกสร้างเงาและเบลอขึ้นมา)
* **เหตุผล:** เพื่อคงสไตล์หน้าต่างควบคุมที่สะอาดตา คมชัด เป็นระเบียบ เรียบง่ายสไตล์มินิมอลและประหยัดการประมวลผลบนการ์ดจอ/อุปกรณ์ Client

---

## 3. การจัดการแพ็กเกจทั่วไป (Package Management)

* การเพิ่ม Dependencies หรือ Libraries ทั่วไปของระบบ (เช่น แพ็กเกจช่วยเหลือ, ไลบรารีฟังก์ชัน) ให้ทำผ่าน **Yarn** เป็นหลัก เพื่อรักษาโครงสร้างของ `yarn.lock`
  ```bash
  yarn add <ชื่อแพ็กเกจ>
  ```

---

## 4. กฎการจัดการการเชื่อมต่อ Wi-Fi และเครือข่ายไร้สาย (Wi-Fi Configuration Rules)

* **ข้อกำหนดการทำงาน**: การตั้งค่าการเชื่อมต่อ Wi-Fi Client บน Linux Host จะต้องใช้ `wpa_supplicant` เป็นเครื่องมือหลัก และหลีกเลี่ยงการติดตั้งหรือใช้งาน `NetworkManager` (`nmcli`) เพื่อลดปัญหาขัดแย้งเชิงระบบ (Conflict)
* **แนวทางปฏิบัติ**:
  * โค้ดหลังบ้านที่แก้ไขการตั้งค่า Wi-Fi จะต้องเขียนบันทึกไฟล์คอนฟิกรายพอร์ต เช่น `/etc/wpa_supplicant/wpa_supplicant-wlan0.conf` จากนั้นส่งคำสั่ง `RECONFIGURE` ผ่าน UNIX Domain Socket (`unixgram`) ของ `wpa_supplicant` แทนการเรียกคำสั่งภายนอกผ่าน subprocesses
  * การบันทึกไฟล์คอนฟิก Wi-Fi ต้องทำแบบอะตอมมิก (Atomic write) เสมอเพื่อความมั่นคงปลอดภัย
  * ศึกษารายละเอียดแนวทางการเขียนโค้ดและข้อควรระวังความปลอดภัยในการพัฒนา Wi-Fi Client เพิ่มเติมที่คู่มือ [wifi_wpa_working_instruction.md](file:///home/sapray/Sapray/gemini/rpi5-firewall-frontend/docs/wifi_wpa_working_instruction.md)

