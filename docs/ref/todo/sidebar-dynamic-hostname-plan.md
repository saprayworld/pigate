# Sidebar Dynamic Hostname — แสดงชื่อเครื่องจริงใต้แบรนด์ PiGate

> เอกสารออกแบบสำหรับฟีเจอร์: ทำให้ brand header ของ sidebar
> (`app-sidebar.tsx`) แสดง **hostname จริงของอุปกรณ์** แทน string ที่ hardcode อยู่
>
> วันที่เขียน: 2026-07-07 · Branch อ้างอิง: `feat/frontend-ui-redesign`

---

## 0. เป้าหมายและขอบเขต

ระหว่าง redesign sidebar (ปรับ brand block ให้เป็นสไตล์ TeamSwitcher ของ
shadcn dashboard-01) ได้เพิ่มบรรทัดที่สองใต้ชื่อ "PiGate" เพื่อโชว์ชื่อเครื่อง:

```tsx
// frontend/src/components/app-sidebar.tsx (brand header block)
<div className="grid flex-1 text-left text-xs leading-tight">
  <span className="truncate text-sm font-bold tracking-wider">PiGate</span>
  <span className="truncate text-xs text-muted-foreground font-mono">
    home-000002.pigate.lan   {/* ⚠️ hardcode */}
  </span>
</div>
```

**ปัญหา:** ค่า `home-000002.pigate.lan` เป็น string ตายตัว แต่ hostname จริง
ผู้ใช้แก้ได้ที่หน้า **Settings & Maintenance → System Identity**
(`SettingsMaintenance.tsx`, ผ่าน `systemService.getHostname()` /
`updateHostname()`) ผลคือ:

- ตั้ง hostname ใหม่แล้ว sidebar ยังโชว์ค่าเดิม → ดูเหมือนของจริงแต่เป็น mock
- suffix `.lan` ก็เป็นสมมติ ยังไม่มี domain suffix จริงจาก backend

**ขอบเขตงานนี้ = ดึง hostname จริงมาแสดงใน sidebar** พร้อม fallback ระหว่างโหลด
และอัปเดตให้ตรงเมื่อผู้ใช้แก้ค่าในหน้า Settings

นอกขอบเขต (ไว้ทีหลัง): domain suffix / FQDN จริงจาก backend, การโชว์ mDNS name
(`<host>.local`), การ sync ข้าม tab

---

## 1. แหล่งข้อมูลที่มีอยู่แล้ว (ไม่ต้องทำ API ใหม่)

| สิ่งที่ต้องใช้ | แหล่ง | สถานะ |
|---|---|---|
| hostname ปัจจุบัน | `systemService.getHostname()` → `{ hostname, shareWithDhcp }` | ✅ มีแล้ว (`systemService.ts:253`) |
| endpoint จริง | `GET /api/system/hostname` | ✅ มีแล้ว (มี mock fallback ผ่าน LocalStorage) |

`getHostname()` คืน `SystemHostnameSettings` — ใช้ field `hostname` ตรง ๆ ได้เลย
ไม่ต้องแก้ backend หรือ service เพิ่ม

---

## 2. แนวทาง implement (frontend ล้วน)

### 2.1 ทางเลือก A — fetch ใน AppSidebar (เล็ก, เริ่มง่าย)

```tsx
const [hostname, setHostname] = useState("pigate")   // fallback ระหว่างโหลด

useEffect(() => {
  let alive = true
  systemService.getHostname()
    .then((s) => { if (alive && s.hostname) setHostname(s.hostname) })
    .catch(() => { /* คงค่า fallback */ })
  return () => { alive = false }
}, [])
```

แล้วแสดง `{hostname}` (จะใส่ suffix `.lan` หรือไม่ค่อยตัดสินใจ — แนะนำโชว์
hostname เปล่า ๆ จนกว่าจะมี domain จริงจาก backend)

- ✅ ง่าย เปลี่ยนไฟล์เดียว
- ⚠️ แก้ hostname ที่หน้า Settings แล้ว sidebar ไม่อัปเดตจนกว่าจะรีเฟรช/รีเมานต์

### 2.2 ทางเลือก B — shared context/store (sync ทั้งแอป) ✅ แนะนำ

สร้าง `HostnameContext` (หรือ zustand/store เล็ก ๆ) ที่:

- โหลด hostname ครั้งแรกตอน mount (เหมือน 2.1)
- expose `setHostname()` ให้หน้า Settings เรียกหลัง `updateHostname()` สำเร็จ
  เพื่อให้ sidebar อัปเดตทันที (optimistic)

- ✅ แก้ที่ Settings แล้ว sidebar เปลี่ยนตามทันที ไม่ต้องรีเฟรช
- ⚠️ งานมากกว่า A เล็กน้อย (เพิ่ม provider ที่ root + wire หน้า Settings)

---

## 3. จุดที่ต้องระวัง

- **Fallback ต้องไม่ว่าง** — ระหว่างรอ API หรือกรณี error ให้โชว์ `pigate`
  (หรือค่าคงที่) แทน ไม่ปล่อยบรรทัดว่างจน layout กระตุก
- **`truncate` มีอยู่แล้ว** — hostname ยาวจะถูกตัด `...` เมื่อ sidebar แคบ ไม่ต้องแก้เพิ่ม
- **โหมด collapsed (icon-only)** — บรรทัด hostname จะถูกซ่อนตาม layout เดิมอยู่แล้ว
- **suffix `.lan`** — อย่าฮาร์ดโค้ดต่อท้าย ถ้ายังไม่มี domain จริง ให้โชว์ hostname
  เปล่า ๆ ไปก่อน (กัน "ดูเหมือนจริงแต่ mock" ซ้ำรอยเดิม)

---

## 4. Checklist

- [x] เลือกแนวทาง A หรือ B (แนะนำ B ถ้าต้องการให้ Settings ↔ sidebar sync) → **เลือก B**
- [x] ลบ string hardcode `home-000002.pigate.lan` ออกจาก `app-sidebar.tsx`
- [x] ดึง hostname จริงจาก `systemService.getHostname()` + fallback `pigate`
- [x] (ถ้าเลือก B) wire หน้า Settings ให้ update ค่าใน context หลังบันทึกสำเร็จ
- [x] ตัดสินใจเรื่อง suffix `.lan` / FQDN → **โชว์ hostname เปล่า ๆ (ไม่ต่อ suffix)**

---

## 5. สรุปการ implement (2026-07-08 · branch `feat/sidebar-dynamic-hostname`)

เลือก **แนวทาง B** (shared context) ตามคำแนะนำ ทำตาม pattern เดียวกับ `ThemeProvider`:

| ไฟล์ | บทบาท |
|---|---|
| `frontend/src/hooks/hostname-context.ts` | `createContext` + type `HostnameProviderState` (`hostname`, `setHostname`) |
| `frontend/src/components/HostnameProvider.tsx` | โหลด hostname ครั้งแรกด้วย `systemService.getHostname()` (มี `alive` guard), fallback `"pigate"` |
| `frontend/src/hooks/useHostname.ts` | hook อ่าน context (throw ถ้าใช้นอก provider) |
| `frontend/src/components/layout/ShellLayout.tsx` | ครอบ `<HostnameProvider>` รอบ shell → sidebar + หน้า Settings แชร์ค่าเดียวกัน |
| `frontend/src/components/app-sidebar.tsx` | แทน hardcode ด้วย `{hostname}` จาก `useHostname()` |
| `frontend/src/pages/SettingsMaintenance.tsx` | หลัง `updateHostname()` สำเร็จ เรียก `setSharedHostname()` → sidebar อัปเดตทันที (optimistic) |

ผ่าน `yarn lint` และ `yarn build` เรียบร้อย
