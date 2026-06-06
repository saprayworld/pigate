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

### 1.3 การจัดการ Portal Components ภายใน Dialog/Modal (Portal inside Dialog Rules)
* **ข้อกำหนด:** หากมีการใช้งานคอมโพเนนต์ที่เป็น Portal (เช่น Combobox, Select, Dropdown หรือ Popover ของ Base UI / Radix UI) **ภายใน Dialog หรือ Modal ของโครงการ** อาจเกิดปัญหาคลิกดรอปดาวน์แล้วโดนบล็อกเนื่องจาก Focus/Pointer Blocker ของ Radix Dialog มองว่าเป็นกิจกรรมนอกขอบเขต (Interact Outside)
* **แนวทางปฏิบัติ:**
  * ห้ามเรนเดอร์ Combobox แบบไม่ใช้ Portal (เช่น ปลดแท็ก Portal ออกตรงๆ) เพราะจะขัดกับกฎ Context ของไลบรารีและอาจเกิด Error: `Base UI: <Combobox.Portal> is missing.`
  * ให้ใช้วิธีส่งผ่าน container ref โดยสร้าง `useRef` ผูกเข้ากับ `<DialogContent>` เช่น:
    ```tsx
    const dialogContentRef = useRef<HTMLDivElement | null>(null)
    ...
    <DialogContent ref={dialogContentRef}>
    ```
  * จากนั้นให้ตั้งค่า `container={dialogContentRef}` ให้กับคอมโพเนนต์ Content ของ Portal นั้น ๆ (เช่น `<ComboboxContent container={dialogContentRef}>`) เพื่อย้าย Portal เข้ามาเรนเดอร์ใต้ DOM Tree ของ Dialog Content วิธีนี้จะทำให้คลิกเลือกรายการได้สมบูรณ์และถูกต้อง
* **เหตุผล:** เพื่อรองรับการทำงานร่วมกันระหว่างไลบรารีคอมโพเนนต์ต่างค่าย (Radix UI และ Base UI) ได้อย่างเสถียรและปลอดภัย

---

## 2. กฎการเลือกใช้สไตล์และโทนสี (Styling & Theme Rules)

### 2.1 Sleek Dark Mode (ระบบธีมหลัก)
* หน้าต่างควบคุมระบบถูกออกแบบมาเป็น **ธีมสีเข้ม (Sleek Dark Mode)** ตั้งแต่เริ่มต้น เพื่อให้สอดคล้องกับระบบเครือข่ายและความปลอดภัยระดับสูง
* **สีพื้นหลังหลัก (Background):** ใช้โทนสีดำ-เทาเข้ม (`bg-neutral-950` หรือ `bg-neutral-900`)
* **สีสถานะการใช้งาน (Active/Highlight):** 
  * ใช้สีเขียวมรกตหลักของระบบผ่านตัวแปรสี (`text-primary` / `bg-primary`) เพื่อแสดงสถานะที่เป็นปกติ (เช่น Active, Allowed, Power OK)
  * ใช้สีฟ้าคราม (`text-cyan-400`) หรือ สีน้ำเงินอินดิโก (`text-indigo-400`) สำหรับกราฟหรือแถบข้อมูลเครือข่าย
  * ใช้สีส้ม/เหลือง (`text-amber-400`) สำหรับค่าระดับกลาง/คำเตือน (เช่น อุณหภูมิบอร์ด)
  * ใช้สีแดง (`text-red-400` / `bg-red-500`) สำหรับสถานะการบล็อก (Blocked) หรือ ข้อผิดพลาด (Errors)

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
  * ทุกการปรับปรุงชุดตัวแปรของธีมใน [`src/index.css`](file:///home/sapray/Dev/pigate/frontend/src/index.css) จะต้องประกาศตัวแปรเงาและเบลอของ Tailwind เป็น `none` และ `0px` เสมอ (เพื่อไม่ให้ปลั๊กอิน UI ภายนอกสร้างเงาและเบลอขึ้นมา)
* **เหตุผล:** เพื่อคงสไตล์หน้าต่างควบคุมที่สะอาดตา คมชัด เป็นระเบียบ เรียบง่ายสไตล์มินิมอลและประหยัดการประมวลผลบนการ์ดจอ/อุปกรณ์ Client

---

## 3. การจัดการแพ็กเกจทั่วไป (Package Management)

* การเพิ่ม Dependencies หรือ Libraries ทั่วไปของระบบ (เช่น แพ็กเกจช่วยเหลือ, ไลบรารีฟังก์ชัน) ให้ทำผ่าน **Yarn** เป็นหลัก เพื่อรักษาโครงสร้างของ `yarn.lock`
  ```bash
  yarn add <ชื่อแพ็กเกจ>
  ```
