# Frontend Lint Check — Fix Plan

เอกสารแผนงานแก้ lint error ทั้งหมดของ frontend ให้ `yarn lint` ผ่าน (exit 0)
(สถานะ: TODO / วางแผน — รัน lint จริงเมื่อ 2026-07-04 บน branch `main`)

---

## 1. สถานะปัจจุบัน

รัน `cd frontend && yarn lint` แล้ว **fail**: **141 ปัญหา (132 errors, 9 warnings) กระจายใน 27 ไฟล์**

ESLint config (`frontend/eslint.config.js`) เป็น flat config เปิด preset: `js.recommended` + `tseslint.recommended` + `reactHooks.flat.recommended` (v6 — มี rule ใหม่ที่เข้มขึ้น) + `reactRefresh.vite`

### สรุปตามกฎ (rule)

| Rule | จำนวน | ระดับ | ลักษณะปัญหา |
|---|---|---|---|
| `@typescript-eslint/no-explicit-any` | 84 | error | ~68 จุดเป็น `catch (err: any)` ใน pages; ~16 จุดเป็น `any` จริงใน services (`config.ts`, `mockSync.ts`, `systemService.ts`, `Login.tsx`) |
| `@typescript-eslint/no-unused-vars` | 18 | error | `catch (e)` / `catch (err)` ที่ไม่ใช้ตัวแปร |
| `react-hooks/set-state-in-effect` | 14 | error | pattern `useEffect(() => { loadX() }, [])` ที่ `loadX` นิยามนอก effect และเรียก setState |
| `react-hooks/exhaustive-deps` | 9 | warning | dependency ขาด (`loadX`, `isRouteActionDisabled`) |
| `react-refresh/only-export-components` | 6 | error | ไฟล์ export ทั้ง component + non-component (shadcn variants, provider hooks) |
| `no-empty` | 5 | error | `catch` block ว่างเปล่า |
| `react-hooks/static-components` | 2 | error | `SidebarContent` นิยาม component ระหว่าง render (`ShellLayout.tsx`) |
| `no-useless-escape` | 2 | error | escape `\/` เกินจำเป็นใน regex (`mockSync.ts`) |
| `prefer-const` | 1 | error | `let newService` ที่ไม่ reassign (`mockSync.ts`) — autofix ได้ |

### สรุปตามไฟล์ (เรียงจากมากไปน้อย)

| ไฟล์ | ปัญหา | ประเภทหลัก |
|---|---|---|
| `src/pages/Dashboard.tsx` | 13 | unused-vars 6, no-empty 4, set-state-in-effect 2, any 1 |
| `src/pages/DhcpServer.tsx` | 11 | any 9, set-state-in-effect 1, deps 1 |
| `src/pages/DnsServer.tsx` | 11 | any 9, set-state-in-effect 1, deps 1 |
| `src/pages/SettingsMaintenance.tsx` | 11 | any 10, set-state-in-effect 1 |
| `src/services/mockSync.ts` | 11 | any 8, useless-escape 2, prefer-const 1 |
| `src/pages/FirewallPolicy.tsx` | 9 | any 7, set-state-in-effect 1, deps 1 |
| `src/pages/Interfaces.tsx` | 8 | any 6, set-state-in-effect 2 |
| `src/pages/QoS.tsx` | 8 | any 6, set-state-in-effect 1, deps 1 |
| `src/pages/StaticRoutes.tsx` | 8 | any 5, deps 2, set-state-in-effect 1 |
| `src/pages/Services.tsx` | 7 | any 5, set-state-in-effect 1, deps 1 |
| `src/pages/Addresses.tsx` | 6 | any 4, set-state-in-effect 1, deps 1 |
| `src/pages/Users.tsx` | 6 | any 4, set-state-in-effect 1, deps 1 |
| `src/services/systemService.ts` | 6 | unused-vars 3, any 3 |
| `src/components/layout/ShellLayout.tsx` | 3 | static-components 2, unused-vars 1 |
| `src/pages/DNS.tsx` | 3 | any 2, set-state-in-effect 1 |
| `src/services/config.ts` | 3 | any 3 |
| `src/services/dashboardService.ts` | 3 | unused-vars 2, no-empty 1 |
| `src/services/dhcpService.ts` | 3 | unused-vars 3 |
| `src/pages/ForceChangePassword.tsx` | 2 | any 1, unused-vars 1 |
| `src/services/dnsServerService.ts` | 2 | unused-vars 2 |
| `src/components/AlertDialogProvider.tsx` | 1 | only-export-components |
| `src/components/ThemeProvider.tsx` | 1 | only-export-components |
| `src/components/ui/badge.tsx` | 1 | only-export-components |
| `src/components/ui/button.tsx` | 1 | only-export-components |
| `src/components/ui/combobox.tsx` | 1 | only-export-components |
| `src/components/ui/tabs.tsx` | 1 | only-export-components |
| `src/pages/Login.tsx` | 1 | any 1 |

## 2. หลักการแก้ (ตัดสินใจเชิงนโยบายก่อนลงมือ)

1. **แก้โค้ดเป็นหลัก ไม่ปิด rule** — ยกเว้น `react-refresh/only-export-components` สำหรับ `components/ui/**` เท่านั้น (เป็นโค้ด shadcn stock ที่ trigger rule นี้เป็นปกติ และ `docs/rules_of_work.md` ให้คงไฟล์เหล่านี้ตาม convention ของ shadcn — การแยกไฟล์ variants ออกจะทำให้ diff กับ upstream ตอน `npx shadcn add` ยุ่งขึ้น)
2. **`catch (err: any)` → `catch (err)` + helper กลาง** — สร้าง `getErrorMessage(err: unknown): string` ที่เดียว แล้ว sweep ทุก page ให้เรียก helper แทน `err.message` เพื่อไม่ต้องเดา type ใน 68 จุด
3. **`set-state-in-effect` แก้ด้วย pattern ที่พิสูจน์แล้วในโค้ดเบสเอง** — `Dashboard.tsx:118-128` (`fetchHostname`) นิยาม async function *ภายใน* effect แล้ว setState หลัง `await` → **ไม่โดน flag** ขณะที่ `fetchStats` (นิยามนอก effect, โครงเหมือนกัน) โดน flag — ใช้ pattern นี้เป็นแม่แบบ
4. ทำเป็น commit เล็ก ๆ แยกตามประเภท (mechanical ก่อน → พฤติกรรมทีหลัง) เพื่อให้ review/bisect ง่าย

## 3. แผนงานเป็นขั้นตอน

### Phase 1 — Mechanical fixes (ไม่กระทบพฤติกรรม, เสี่ยงต่ำสุด)

**1.1 `catch` ที่ไม่ใช้ตัวแปร + block ว่าง** (`no-unused-vars` 18 + `no-empty` 5)
- เปลี่ยน `catch (e) {}` / `catch (err) { }` → `catch { /* ignore: <เหตุผลสั้น ๆ> */ }` (optional catch binding แก้ unused-vars; comment ใน block แก้ no-empty ในตัว)
- ไฟล์: `src/pages/Dashboard.tsx` (6 จุด: บรรทัด ~106, 138, 171, 233, 260, 335), `src/services/dashboardService.ts` (~41, 61), `src/services/dhcpService.ts` (~25, 43, 61), `src/services/dnsServerService.ts` (~15, 32), `src/services/systemService.ts` (~64, 101, 132), `src/pages/ForceChangePassword.tsx` (~50), `src/components/layout/ShellLayout.tsx` (~68)
- จุดที่ catch แล้ว "กลืนเงียบ" ใน polling loop ของ Dashboard เป็นความตั้งใจ (กัน error spam ตอน backend ไม่ตอบ) — ใส่ comment อธิบาย อย่าเปลี่ยนเป็น throw/alert

**1.2 `mockSync.ts` เรื่องเล็ก** (`no-useless-escape` 2 + `prefer-const` 1)
- บรรทัด ~65, ~158: ลบ `\` หน้า `/` ใน regex (ใน JS regex literal, `/` ใน character class ไม่ต้อง escape) — ห้ามเปลี่ยนความหมาย regex ตรวจด้วยการเทียบ pattern เดิม/ใหม่ใน console ก่อน
- บรรทัด ~172: `let newService` → `const` (จุดเดียวที่ `--fix` แก้ให้ได้ — จะรัน `npx eslint src/services/mockSync.ts --fix` เฉพาะไฟล์นี้ก็ได้ **อย่ารัน `--fix` ทั้งโปรเจค**)

### Phase 2 — กวาด `catch (err: any)` (~68 จุด, มี recipe เดียว)

**2.1 สร้าง `src/lib/errors.ts` (ใหม่)**
```ts
export function getErrorMessage(err: unknown): string {
  if (err instanceof Error) return err.message
  if (typeof err === "string") return err
  return String(err)
}
```

**2.2 Sweep ทุก page**: `catch (err: any)` → `catch (err)` แล้วแทน `err.message || err` / `err.message` ด้วย `getErrorMessage(err)`
- ไฟล์ (จำนวนจุด): `SettingsMaintenance.tsx` (10), `DhcpServer.tsx` (9), `DnsServer.tsx` (9), `FirewallPolicy.tsx` (7), `Interfaces.tsx` (6), `QoS.tsx` (6), `StaticRoutes.tsx` (5), `Services.tsx` (5), `Addresses.tsx` (4), `Users.tsx` (4), `DNS.tsx` (2), `Dashboard.tsx` (1), `Login.tsx` (1), `ForceChangePassword.tsx` (1)
- ระวังจุดที่โค้ดเดิมอ่าน property อื่นนอกจาก `.message` (เช่น `err.status`, `err.response`) — ถ้ามี ให้ narrow type ด้วย `instanceof` / type guard เฉพาะจุด อย่า cast มั่ว

### Phase 3 — `any` จริงใน services (~16 จุด, ต้องคิด type)

**3.1 `src/services/config.ts`** (3 จุด ~บรรทัด 13, 22, 48) — เป็น utility fetch/mock-guard: เปลี่ยน `any` เป็น generic `<T>` หรือ `unknown` แล้วให้ caller ระบุ type

**3.2 `src/services/mockSync.ts`** (8 จุด) — payload ของ mock sync: ประกาศ interface ของ entity ที่ sync (หรือ import type จาก service ที่มีอยู่แล้ว เช่น `AddressObject`, `ServiceObject`) แทน `any`; ถ้า shape ไม่แน่จริง ใช้ `unknown` + narrow

**3.3 `src/services/systemService.ts`** (3 จุด ~บรรทัด 355, 396, 451) — `exportConfig(): Promise<any>` / `importConfig(configData: any)`:
- **ประสานกับแผน Export/Import** (`docs/ref/todo/export-import-system-design.md` Phase 3 ข้อ 12 กำหนดให้พิมพ์ type `BackupFile`/`ImportResult` อยู่แล้ว) — ถ้าจะทำระบบ Export/Import ต่อเร็ว ๆ นี้ ให้พิมพ์ type ชั่วคราวเป็น `unknown` + type ขั้นต่ำ (`Record<string, unknown>`) ไปก่อนเพื่อให้ lint ผ่าน แล้วค่อยแทนด้วย `BackupFile` จริงตอนทำ schema v2 — **อย่าออกแบบ type ละเอียดซ้อนกับงานนั้น**

### Phase 4 — React hooks refactor (14 errors + 9 warnings, กระทบพฤติกรรมได้ — ระวังสุด)

**4.1 `react-hooks/set-state-in-effect` (14 จุด)** — pattern เดียวกันเกือบทุก page:
```
useEffect(() => { loadX() }, [])        // loadX นิยามนอก effect + setState
```
ตำแหน่ง: `Addresses.tsx:78`, `DNS.tsx:50`, `Dashboard.tsx:110`, `Dashboard.tsx:246`, `DhcpServer.tsx:116`, `DnsServer.tsx:112`, `FirewallPolicy.tsx:339`, `Interfaces.tsx:203`, `Interfaces.tsx:382`, `QoS.tsx:107`, `Services.tsx:75`, `SettingsMaintenance.tsx:133`, `StaticRoutes.tsx:105`, `Users.tsx:79`

Recipe (ตามแม่แบบ `fetchHostname` ใน Dashboard ที่ lint ผ่าน):
- ตั้ง initial state ให้ถูกตั้งแต่ `useState` (เช่น `useState(true)` สำหรับ `isLoading`) เพื่อไม่ต้องเรียก `setIsLoading(true)` แบบ sync ตอน mount
- ใน effect นิยาม async function *ข้างใน* แล้วเรียก: fetch → setState หลัง `await` เท่านั้น
- `loadX` ตัวเดิม**เก็บไว้**สำหรับปุ่ม refresh / หลัง save (เรียกจาก event handler ไม่โดน rule นี้) — effect กับ handler เรียก service ชุดเดียวกัน ยอมให้ซ้ำกันเล็กน้อยได้
- กรณี polling (`Dashboard.tsx:110` มี `setInterval(fetchStats, 10000)`) — ย้ายนิยาม `fetchStats` เข้าไปใน effect ทั้งก้อน interval ยังทำงานเหมือนเดิม

**4.2 `react-hooks/exhaustive-deps` (9 warnings)**
- ส่วนใหญ่คือ effect ที่ขาด `loadX` ใน deps — **จะหายเองเมื่อทำ 4.1** (function ย้ายเข้า effect แล้วไม่มี external dep)
- `StaticRoutes.tsx:191` (`useMemo` ขาด `isRouteActionDisabled`): ห่อ `isRouteActionDisabled` ด้วย `useCallback` แล้วใส่ใน deps ของ `useMemo` — **ห้าม**แค่ยัด function ดิบเข้า deps (identity เปลี่ยนทุก render = memo ไร้ผล)

**4.3 `react-hooks/static-components` (`ShellLayout.tsx:211, 227`)**
- `SidebarContent` เป็น arrow component ที่นิยามใน render แล้วใช้ 2 ที่ (desktop sidebar + mobile panel) — ทุก re-render ได้ component type ใหม่ → React unmount/remount ทั้ง subtree
- แก้โดยเปลี่ยนจาก component เป็น **JSX element ธรรมดา**: `const sidebarContent = (<div>...</div>)` แล้ว render `{sidebarContent}` ทั้ง 2 จุด (ไม่ต้องแตกไฟล์/ส่ง props เพราะมันปิด state ของ ShellLayout อยู่)
- **ทดสอบ mobile menu เปิด-ปิดหลังแก้** — จุดนี้เป็น UI หลักทุกหน้า

### Phase 5 — `react-refresh/only-export-components` (6 จุด)

**5.1 `eslint.config.js`** — เพิ่ม override ปิด rule เฉพาะ shadcn primitives:
```js
{
  files: ['src/components/ui/**/*.tsx'],
  rules: { 'react-refresh/only-export-components': 'off' },
},
```
ครอบคลุม `badge.tsx`, `button.tsx`, `combobox.tsx`, `tabs.tsx` (export `xxxVariants` cva ตาม convention ของ shadcn — คงไฟล์ stock ไว้)

**5.2 Providers 2 ไฟล์ — แยก hook ออกเป็นไฟล์ใหม่** (แก้จริง ไม่ปิด rule เพราะไม่ใช่โค้ด vendored และได้ HMR ที่ถูกต้องคืนมา):
- `src/components/ThemeProvider.tsx` → ย้าย `useTheme` ไป `src/hooks/useTheme.ts` (สร้างโฟลเดอร์ `src/hooks/` ใหม่)
- `src/components/AlertDialogProvider.tsx` → ย้าย `useAlert` ไป `src/hooks/useAlert.ts`
- ต้องย้าย context object ไปไฟล์กลาง (เช่น `src/hooks/theme-context.ts`) เพื่อให้ provider กับ hook import ร่วมกันโดยไม่เกิด circular import
- **อัปเดต import ทุกไฟล์ที่ใช้** — มี ~15 ไฟล์ที่อ้าง `useTheme`/`useAlert` (grep ก่อนแก้: `grep -rln "useTheme\|useAlert" src`)

### Phase 6 — Verify (Definition of Done)

1. `cd frontend && yarn lint` → **exit 0, 0 errors 0 warnings**
2. `cd frontend && yarn build` → ผ่าน (`tsc -b` จับ type ใหม่จาก Phase 2-3)
3. Smoke test บน dev (`yarn dev` + backend `-mock=true`) ไล่ทุกหน้า โดยเฉพาะ:
   - ทุก page โหลดข้อมูลขึ้นปกติ + ปุ่ม refresh ยังทำงาน (Phase 4.1 แตะ initial-load ทุกหน้า)
   - Dashboard: stats/performance ยัง poll ต่อเนื่อง (interval ไม่ตาย ไม่ double-fire)
   - Sidebar mobile เปิด-ปิด, สลับ dark/light theme, เรียก AlertDialog สักจุด (Phase 4.3 + 5.2)
   - Error path: ปิด backend แล้วกด action → ข้อความ error ยังอ่านรู้เรื่อง (Phase 2 helper)

## 4. ข้อควรระวัง (Cautions)

1. **ห้ามรัน `eslint --fix` ทั้งโปรเจค** — autofix ได้จริงแค่ 1 จุด (`prefer-const`) ที่เหลือถ้า plugin พยายาม fix hooks อาจเปลี่ยนพฤติกรรม ให้แก้มือทีละกลุ่ม
2. **อย่าแก้ `exhaustive-deps` ด้วยการยัด dep เพิ่มอย่างเดียว** — `loadX` นิยามใหม่ทุก render ถ้าใส่ใน deps ตรง ๆ จะได้ effect วิ่งทุก render (แย่กว่าเดิม เสี่ยง infinite loop กับ setState) ต้อง restructure ตาม Phase 4.1 หรือ `useCallback`
3. **Polling ใน Dashboard มี `setInterval` 2 ชุด (10s / 3s)** — ตอน refactor เข้า effect ต้องคง cleanup (`clearInterval`) ครบ มิฉะนั้น interval รั่วซ้อนกันทุกครั้งที่ remount
4. **การกลืน error เงียบ ๆ ใน polling เป็นความตั้งใจ** — อย่า "แก้ lint" โดยเพิ่ม alert/throw ใน catch ของ fetch ที่วิ่งซ้ำอัตโนมัติ เดี๋ยว UI เด้ง error รัว ๆ เวลา backend restart
5. **อย่าแตะ logic ของ `mockSync.ts` เกิน type/regex** — mock mode เป็นเครื่องมือทดสอบ UI หลักบน dev (ตาม workflow ของโปรเจคที่ dev บน WSL ไม่มี kernel จริง)
6. **`systemService.ts` type ของ export/import ผูกกับแผน Export/Import** — ทำขั้นต่ำให้ lint ผ่าน (unknown) พอ ปล่อยให้แผนนั้นเป็นเจ้าของ `BackupFile`/`ImportResult` ตัวจริง เพื่อไม่ทำงานซ้ำ/ขัดกัน
7. **ไฟล์ `components/ui/**` เป็น shadcn stock** — แก้ผ่าน eslint override ไม่แก้เนื้อไฟล์ เพื่อให้ `npx shadcn@latest add <component>` ครั้งหน้า diff สะอาด
8. **แยก commit ตาม Phase** — Phase 1-2 (mechanical) รวมกันได้, Phase 4 ควรแยกเป็น commit ต่อกลุ่มหน้า เพราะเป็นจุดเดียวที่เสี่ยงต่อพฤติกรรม runtime; ห้าม commit จนกว่าผู้ใช้สั่ง (ตาม convention โปรเจค)
9. **rule `react-hooks/set-state-in-effect` เป็นของใหม่ใน react-hooks v6** — ถ้าเจอเคสที่ restructure แล้วยังโดน flag แบบ false positive จริง ๆ ให้ใช้ `// eslint-disable-next-line react-hooks/set-state-in-effect` พร้อม comment เหตุผล **รายบรรทัด** เท่านั้น ห้ามปิดทั้งไฟล์/ทั้งโปรเจค
10. **Dark/light + theme variables** — งานนี้ไม่ควรแตะ className ใด ๆ ถ้าพบว่าต้องแก้ JSX (เช่น ShellLayout) ให้ copy className เดิมเป๊ะ ๆ ตาม `docs/rules_of_work.md`

## 5. ประมาณการขนาดงาน

| Phase | ไฟล์ที่แตะ | ความเสี่ยง |
|---|---|---|
| 1 | ~8 ไฟล์ | ต่ำมาก (mechanical) |
| 2 | ~15 ไฟล์ + `lib/errors.ts` ใหม่ | ต่ำ (pattern เดียว) |
| 3 | 3 ไฟล์ services | กลาง (ต้องเข้าใจ shape ข้อมูล) |
| 4 | ~14 pages + `ShellLayout.tsx` | **สูงสุด** (แตะ initial-load ทุกหน้า) |
| 5 | `eslint.config.js` + 2 providers + ~15 imports | กลาง (ย้ายไฟล์/import) |
| 6 | — | verification |
