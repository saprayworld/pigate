# User System Design — ระบบจัดการผู้ใช้ (สร้าง/แก้ไข/ลบ/ปิด-เปิดใช้งาน + Role)

> เอกสารออกแบบสำหรับระบบจัดการผู้ใช้ของ PiGate:
> - จัดการผู้ใช้ได้เต็มรูปแบบ: **สร้าง / แก้ไข / ลบ / ปิดใช้งาน / เปิดใช้งาน**
> - ผู้ใช้มี 2 role หลัก:
>   - **`super_admin`** — ทำได้ทุกอย่าง รวมถึงจัดการผู้ใช้คนอื่น
>   - **`admin_readonly`** — ดูได้ทุกหน้า แต่แก้ไขอะไรไม่ได้เลย (block ทุก mutation)
> - Frontend เป็น **เมนูใหม่ในกลุ่ม System** (เช่น `/system/users`)
>   แยกหน้าเป็นของตัวเอง **ไม่รวมกับ Settings & Maintenance**

---

## 1. สถานะปัจจุบัน (Current State — ตรวจสอบแล้ว 2026-07-03)

README ระบุ User System = Mock/Mock ตรวจโค้ดแล้วมี auth พื้นฐานแบบ **ผู้ใช้เดียว
hardcode "pigate"** ฝังอยู่หลายจุด:

| จุด | สถานะ |
|-----|--------|
| ตาราง `users` | ✅ มีแล้ว (`connection.go:195`) แต่มีแค่ `id, username, password_hash, is_initial, created_at` — **ไม่มี `role` ไม่มี `status`** seed ผู้ใช้เดียว "pigate" |
| `model.User` | ✅ มีแล้ว (`types.go:6`) — `PasswordHash` ติด `json:"-"` แล้ว (ดี) แต่ไม่มี Role/Status |
| Repository | ⚠️ มีแค่ `GetUserByUsername` + `ChangePassword` — ไม่มี List/Create/Update/Delete |
| Login/Logout/Session | ✅ ทำงานจริง: bcrypt + rate limit + cookie/Bearer, session เก็บใน memory (`middleware.go` map token→username) |
| `AuthMiddleware` | 🐛 **hardcode**: ฉีด context user = `"pigate"` เสมอ (`middleware.go:92`) และเช็ค is_initial โดย query `"pigate"` ตายตัว — ไม่สนว่า login ด้วยใคร |
| `HandleChangePassword` | 🐛 **hardcode** `GetUserByUsername("pigate")` (`handlers.go:1430`) |
| `HandleCheckSession` | 🐛 fallback username = `"pigate"` (`handlers.go:185`) |
| Role-based authorization | ❌ ไม่มีเลย (มีแต่ `DisableEditMiddleware` จาก flag `-disable-edit` ซึ่งเป็น global read-only คนละเรื่องกับ role) |
| API `/api/users/*` | ❌ ยังไม่มี |
| Frontend | ⚠️ มี `Login.tsx`, `ForceChangePassword.tsx`, `authService.ts` แล้ว แต่ไม่มีหน้า Users, ไม่มี role ใน state; เมนูกลุ่ม "System" ใน `ShellLayout.tsx:126` มีรายการเดียว (Settings & Maintenance) |

**นัยสำคัญ**: ก่อนเพิ่ม multi-user ต้อง "ถอน hardcode pigate" ใน 3 จุดข้างบนก่อน
ไม่งั้นทุก session จะกลายเป็น pigate หมดไม่ว่า login ด้วยใคร

---

## 2. แนวทางที่เลือก (Design Decision)

### 2.1 Schema

เพิ่ม 2 คอลัมน์ในตาราง `users` (migration แบบ `strings.Contains` pattern เดิม):

```sql
role   TEXT NOT NULL DEFAULT 'super_admin'
       CHECK(role IN ('super_admin', 'admin_readonly'))
status TEXT NOT NULL DEFAULT 'active'
       CHECK(status IN ('active', 'disabled'))
```

- ผู้ใช้เดิม ("pigate") ได้ default `super_admin` + `active` จาก migration อัตโนมัติ
- ใช้ค่า role เป็น string ตรง ๆ (ไม่ทำตาราง roles แยก) — มีแค่ 2 ค่า fix ตาม requirement

### 2.2 การผูก session กับผู้ใช้จริง (แก้ hardcode)

`AuthMiddleware` ใหม่ ต่อ request:
1. token → username จาก session map (มีอยู่แล้ว แค่เอามาใช้จริง)
2. `GetUserByUsername(username)` → ถ้าไม่พบ (ถูกลบ) หรือ `status = disabled`
   → ตอบ 401 + `RemoveSession(token)` ทันที
3. ฉีด context: username + role (เพิ่ม `RoleContextKey`)
4. เช็ค `is_initial` ของ **ผู้ใช้คนนั้น** (ไม่ใช่ pigate) — บังคับเปลี่ยนรหัสผ่านครั้งแรก
   ใช้กลไก `mustChangePassword` เดิมได้เลย

**ข้อดีของการ query DB ทุก request**: ปิดใช้งาน/ลบ/ลด role ผู้ใช้แล้ว**มีผลทันที**
กับ session ที่ค้างอยู่ โดยไม่ต้องทำระบบ purge session (SQLite in-process เร็วพอ
และ middleware เดิมก็ query อยู่แล้วเพื่อเช็ค is_initial)

### 2.3 การบังคับ role

- **`admin_readonly`**: middleware ใหม่ `RoleReadOnlyMiddleware` (โครงเดียวกับ
  `DisableEditMiddleware`) — block `POST/PUT/PATCH/DELETE` ทั้งหมดเมื่อ role จาก
  context เป็น `admin_readonly` **ยกเว้น**: `/api/auth/logout` และ
  `PUT /api/system/password` (เปลี่ยนรหัสผ่านตัวเองได้) — ตอบ 403 พร้อมข้อความชัดเจน
- **`/api/users/*` ทุก endpoint (รวม GET)**: จำกัดเฉพาะ `super_admin` ผ่าน wrapper
  `superAdminRoute` ใน router.go — readonly ไม่ควรเห็นแม้แต่รายชื่อบัญชี
- flag `-disable-edit` (global read-only) **คงไว้แยกต่างหาก** ทำงานซ้อนกันได้

### 2.4 Business rules (guard rails)

| กติกา | เหตุผล |
|---|---|
| ห้ามลบ/ปิดใช้งาน/ลด role **ตัวเอง** | กัน lock ตัวเองออกกลางอากาศ |
| ห้ามลบ/ปิดใช้งาน/ลด role จนไม่เหลือ `super_admin` ที่ `active` แม้แต่คนเดียว | กันระบบไร้คนคุมถาวร (ตรวจใน service ก่อนทุก mutation) |
| `username` แก้ไขไม่ได้หลังสร้าง (key อ้างอิง session) | เลี่ยงปัญหา session map ผูกกับ username |
| สร้างผู้ใช้ใหม่ → `is_initial = 1` เสมอ | บังคับเปลี่ยนรหัสผ่านครั้งแรก (กลไกเดิมรองรับแล้ว) |
| super_admin ตั้งรหัสผ่านใหม่ให้คนอื่นได้โดยไม่ต้องรู้รหัสเดิม (reset) และ set `is_initial = 1` กลับ | ใช้กรณีลืมรหัสผ่าน |
| Validation: username `^[a-zA-Z0-9_]{3,32}$`, password ≥ 8 ตัว | สอดคล้อง UI เดิม |

### 2.5 API surface

| Endpoint | สิทธิ์ | งาน |
|---|---|---|
| `GET /api/users` | super_admin | รายชื่อทั้งหมด (ไม่มี hash) |
| `POST /api/users` | super_admin | สร้าง `{username, password, role}` |
| `PUT /api/users/{id}` | super_admin | แก้ `{role, password?}` (password = reset) |
| `DELETE /api/users/{id}` | super_admin | ลบ |
| `POST /api/users/{id}/toggle` | super_admin | สลับ active ↔ disabled |
| `GET /api/auth/session` (เดิม) | ทุกคน | **เพิ่ม `role` ใน response** ให้ frontend ปรับ UI |

Login ของผู้ใช้ที่ `disabled` → 401 ข้อความ "บัญชีนี้ถูกปิดใช้งาน" (ตัดสินใจแล้วว่า
ยอมบอกตรง ๆ เพราะเป็นกล่อง admin ภายใน ไม่ใช่ public service)

### 2.6 Frontend

- หน้าใหม่ `pages/Users.tsx` ที่ route `/system/users` + เมนู "User Management"
  ในกลุ่ม System ของ `ShellLayout.tsx` (**ไม่ยุ่งกับ SettingsMaintenance.tsx**)
- เก็บ `role` ไว้ใน auth state (จาก login/checkSession):
  - `admin_readonly` → ซ่อนเมนู User Management + guard route + (เฟสถัดไป)
    ค่อยไล่ disable ปุ่ม Save ตามหน้า — ระหว่างนี้พึ่ง 403 จาก backend เป็นด่านจริง
- หน้า Users: ตาราง (username, role badge, status badge, created) + Dialog
  สร้าง/แก้ไข + confirm ก่อนลบ/ปิดใช้งาน — ใช้ shadcn/ui + semantic colors ตาม
  `rules_of_work.md` (Dialog ที่มี Select ต้องใช้ `modal={false}`)

---

## 3. ขั้นตอนการทำงาน (Implementation Plan)

### Phase 1 — Model + Database

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/model/types.go` | (1) เพิ่ม `Role string \`json:"role"\`` และ `Status string \`json:"status"\`` ใน `User` (2) เพิ่ม struct request: `CreateUserRequest {Username, Password, Role}`, `UpdateUserRequest {Role string; Password *string}` (3) เพิ่ม `Role` ใน `LoginResponse` |
| `backend/internal/db/connection.go` | (1) migration เพิ่มคอลัมน์ `role`, `status` ใน `users` (pattern `strings.Contains(sql, "role")` — **ระวัง**: ใช้คำที่ unique เช่นตรวจ `"admin_readonly"` หรือชื่อคอลัมน์เต็มใน CHECK แทน เพราะคำสั้น ๆ อาจชนกับ substring อื่น) (2) แก้ `CREATE TABLE IF NOT EXISTS users` ให้มี 2 คอลัมน์ใหม่ + CHECK (3) seed "pigate" เดิมให้ระบุ role/status ชัดเจน |
| `backend/internal/db/repository.go` | เพิ่ม `GetUsers()`, `GetUserByID(id)`, `CreateUser(u)`, `UpdateUser(u)`, `DeleteUser(id)`, `SetUserStatus(id, status)`, `CountActiveSuperAdmins()` + อัปเดต `GetUserByUsername` ให้ scan คอลัมน์ใหม่ด้วย |

### Phase 2 — Service layer (business rules)

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/service/user.go` (ไฟล์ใหม่) | สร้าง `UserService` (dep: `repo`) รวม logic ทั้งหมด: (1) `List()` (2) `Create(req)` — validate username/password/role, เช็คซ้ำ, bcrypt cost 10, `is_initial=1` (3) `Update(actorUsername, id, req)` — guard: ห้ามลดตัวเอง, ห้ามทำให้ super_admin active เหลือ 0; ถ้ามี password → hash ใหม่ + `is_initial=1` (4) `Delete(actorUsername, id)` — guard เดียวกัน + ห้ามลบตัวเอง (5) `Toggle(actorUsername, id)` — guard เดียวกัน + ห้ามปิดตัวเอง หมายเหตุ: **ไม่มี kernel layer** เพราะฟีเจอร์นี้จบใน DB ล้วน ไม่แตะ OS |
| `backend/internal/service/user_test.go` (ไฟล์ใหม่) | เทสต์ guard rails ครบทุกข้อ: ลบตัวเอง, ปิดตัวเอง, ลด role ตัวเอง, super_admin คนสุดท้าย, username ซ้ำ, validation, toggle ไป-กลับ |

### Phase 3 — Auth core rework (ถอน hardcode "pigate")

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `backend/internal/api/middleware.go` | (1) เพิ่ม `RoleContextKey` (2) `AuthMiddleware`: ดึง username จริงจาก session map → query user → reject ถ้า nil/disabled (+RemoveSession) → ฉีด username+role ลง context → เช็ค `is_initial` ของ user นั้น (แทน "pigate") (3) เพิ่ม `RoleReadOnlyMiddleware`: block mutation เมื่อ role=admin_readonly ยกเว้น `/api/auth/logout`, `/api/system/password` |
| `backend/internal/api/handlers.go` | (1) `HandleLogin`: reject `status=disabled` + ใส่ `Role` ใน response (2) `HandleCheckSession`: ตัด fallback "pigate", คืน `role` ด้วย (3) `HandleChangePassword`: ใช้ username จาก context แทน hardcode (4) เพิ่ม handlers: `HandleGetUsers`, `HandleCreateUser`, `HandleUpdateUser`, `HandleDeleteUser`, `HandleToggleUser` — ส่ง actor จาก context เข้า service |
| `backend/internal/api/router.go` | (1) ครอบ `authRoute` ทั้งหมดด้วย `RoleReadOnlyMiddleware` (หลัง AuthMiddleware) (2) เพิ่ม wrapper `superAdminRoute` (เช็ค role จาก context = super_admin ไม่งั้น 403) แล้ว register 5 เส้นทาง `/api/users` (3) ระวังลำดับ middleware: RateLimit → Auth → Role |
| `backend/cmd/pigate/main.go` | สร้าง `UserService` ส่งเข้า `NewServer(...)` |
| `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` | เพิ่ม paths `/users`, `/users/{id}`, `/users/{id}/toggle` + schema `User` (ไม่มี passwordHash), `CreateUserRequest`, `UpdateUserRequest` + อัปเดต `LoginResponse`/session ให้มี role — แก้ 2 ไฟล์ให้ตรงกัน |

### Phase 4 — Frontend: service + auth state

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `frontend/src/services/userService.ts` (ไฟล์ใหม่) | `getAll/create/update/delete/toggle` + mock mode ผ่าน localStorage (pattern เดียวกับ `interfaceService.ts`) — mock seed: pigate (super_admin) + viewer (admin_readonly) |
| `frontend/src/services/authService.ts` | (1) `login`/`checkSession` รับ-เก็บ `role` (เช่น localStorage `pigate_role` คู่กับ session token) (2) mock mode คืน role ด้วย |
| `frontend/src/App.tsx` | (1) เพิ่ม route `/system/users` ใต้ ProtectedRoute (2) เพิ่ม guard: ถ้า role ≠ super_admin เข้า `/system/users` → redirect `/dashboard` |

### Phase 5 — Frontend: UI

| ไฟล์ | สิ่งที่ทำ |
|---|---|
| `frontend/src/pages/Users.tsx` (ไฟล์ใหม่) | หน้า User Management: (1) ตารางรายชื่อ — role badge (super_admin = สี primary, admin_readonly = muted), status badge (Active/Disabled), created date (2) ปุ่ม Add User → Dialog `modal={false}` (มี Select role): username / password / confirm / role (3) แก้ไข → Dialog เดียวกัน: role + reset password (เว้นว่าง = ไม่เปลี่ยน — pattern เดียวกับ Wi-Fi password ใน Interfaces.tsx) (4) Switch เปิด/ปิดใช้งาน + ปุ่มลบ พร้อม `confirm()` จาก AlertDialogProvider (5) แถวของ**ตัวเอง**: ซ่อน/disable ปุ่มลบ, toggle, และเปลี่ยน role (สะท้อน guard ฝั่ง backend) (6) validation ฝั่ง UI ก่อนส่ง + แสดง error จาก backend |
| `frontend/src/components/layout/ShellLayout.tsx` | (1) เพิ่ม `{ path: "/system/users", label: "User Management", icon: Users }` ในกลุ่ม System (2) เพิ่ม case ใน `getPageTitle` (3) ซ่อนเมนูนี้เมื่อ role ≠ super_admin (4) แสดง username + role จริงใน dropdown มุมขวาบน (ปัจจุบัน hardcode) |
| `frontend/src/pages/Login.tsx` / `ForceChangePassword.tsx` | ตรวจให้ flow รองรับ role (เก็บ role หลัง login) — force-change ทำงานกับผู้ใช้ใหม่ทุกคนผ่าน `is_initial` กลไกเดิม |

### Phase 6 — ทดสอบ + เอกสาร

1. `cd backend && go build ./... && go test ./...` (เน้น `user_test.go` + เทสต์
   middleware ใน `handlers_test.go` เดิมต้องไม่พังจากการแก้ AuthMiddleware)
2. mock mode: สร้าง/แก้/ลบ/ปิด-เปิด ผ่าน UI, ลอง login ด้วย user ใหม่ →
   โดนบังคับเปลี่ยนรหัส, login ด้วย readonly → แก้อะไรไม่ได้แต่เปลี่ยนรหัสตัวเองได้
3. `cd frontend && yarn build && yarn lint`
4. เครื่องจริง: ผู้ใช้เดิม "pigate" ยัง login ได้หลัง migrate (ต้องได้ role
   super_admin อัตโนมัติ), ทดสอบ disable แล้ว session ที่เปิดค้างโดนดีดทันที
5. อัปเดตตาราง Feature Status ใน `README.md` (User System: Mock → In Progress/Completed)

---

## 4. ข้อควรระวัง (Cautions)

1. **จุดที่พังง่ายที่สุดคือ migration + ผู้ใช้เดิม** — เครื่องที่ใช้อยู่มี user
   "pigate" (บาง เครื่อง is_initial=0 แล้ว) migration ต้องทำให้เขาเป็น
   `super_admin/active` เสมอ มิฉะนั้น**อัปเกรดแล้ว lock out ทั้งระบบ** —
   เขียนเทสต์ migration กับ DB ที่มีข้อมูลเก่าโดยเฉพาะ

2. **การถอน hardcode "pigate" กระทบ flow เดิม 3 จุดพร้อมกัน** (AuthMiddleware,
   HandleChangePassword, HandleCheckSession) — ทำเป็น Phase แยก (Phase 3) และรัน
   เทสต์เดิมทั้งหมดก่อนต่อยอด เพราะ `handlers_test.go` เดิมอาจ assume พฤติกรรม
   single-user

3. **ห้ามให้ระบบไร้ super_admin ที่ active** — guard ต้องอยู่ที่ **service layer
   จุดเดียว** และเช็คแบบ "ผลลัพธ์หลัง mutation" (`CountActiveSuperAdmins()` โดย
   ไม่นับคนที่กำลังถูกลบ/ปิด/ลด role) อย่ากระจาย logic ไปอยู่ใน handler หรือ UI
   เพราะจะหลุดเคสซ้อน เช่น "ลด role ตัวเองพร้อมกับเป็นคนสุดท้าย"

4. **ผลของ disable/delete ต้องมีผลกับ session ค้างทันที** — design นี้ให้
   AuthMiddleware query DB ทุก request จึงได้ฟรี แต่**อย่า cache user ใน memory**
   (เช่น เก็บ role ไว้ใน session map ตอน login) ไม่งั้นปิดใช้งานแล้ว session เก่า
   ยังทำงานต่อจน token หมดอายุ

5. **RoleReadOnlyMiddleware ต้อง fail-closed** — ถ้า role ใน context ว่าง/ไม่รู้จัก
   ให้ถือเป็น readonly (block mutation) ไม่ใช่ปล่อยผ่าน และรายการยกเว้น
   (`/api/system/password`, `/api/auth/logout`) ให้เทียบ path แบบเป๊ะ ๆ
   อย่าใช้ prefix match

6. **อย่าคืน `password_hash` ออก API เด็ดขาด** — `json:"-"` ใน model ช่วยอยู่แล้ว
   แต่ระวัง handler ที่ marshal ผ่าน `map[string]interface{}` หรือ struct ใหม่
   และอย่า log struct User ทั้งก้อน (`log.Printf("%v", user)` จะพ่น hash ลง log)

7. **การซ่อนปุ่มใน UI ไม่ใช่ security** — ด่านจริงคือ middleware ฝั่ง backend
   frontend เป็นแค่ UX; ทุก endpoint mutation ต้องโดน `RoleReadOnlyMiddleware`
   ครอบผ่าน `authRoute` กลางจุดเดียว อย่า register route ใดข้าม wrapper นี้

8. **ลำดับ middleware สำคัญ** — Role check ต้องมาหลัง Auth (ต้องรู้ role ก่อน)
   และ `DisableEditMiddleware` (flag `-disable-edit`) เป็น global คนละชั้น
   ต้องทดสอบว่าซ้อนกันแล้วไม่ตีกัน (readonly role + disable-edit พร้อมกัน)

9. **Session ยังเป็น in-memory** — restart backend = ทุกคนหลุด (พฤติกรรมเดิม
   ยอมรับได้) แต่ token ของ user ที่ถูกลบจะค้างใน map จนกว่า restart —
   middleware reject ให้อยู่แล้ว (GetUserByUsername คืน nil) เป็นแค่ memory ค้าง
   เล็กน้อย ถ้าจะเก็บกวาดให้เพิ่ม `RemoveSessionsForUser(username)` ตอน delete

10. **Rate limiter ของ login ใช้ร่วมทุก username ต่อ IP** — เพิ่มผู้ใช้หลายคนแล้ว
    พฤติกรรมเดิมยังพอใช้ได้ แต่ระวังว่า brute force ข้าม username ยังโดนคุมด้วย
    IP เดียวกัน — ไม่ต้องแก้ในงานนี้ แค่รับรู้

11. **มาตรฐาน UI ตาม `rules_of_work.md`** — Dialog ที่มี Select (เลือก role)
    ต้อง `modal={false}`, ห้าม shadow-*/backdrop-blur, ใช้ semantic colors,
    รองรับ dark/light — และหน้า Users ต้องสร้างจาก shadcn/ui primitives เท่านั้น

12. **โหมด mock ฝั่ง frontend** — `authService.ts` mock ปัจจุบัน hardcode
    `pigate/pigate` — ต้องขยายให้ mock login ผูกกับ userService mock data
    (เช่น viewer/viewer เป็น readonly) เพื่อทดสอบ role-based UI ได้โดยไม่ต้องมี backend

13. **ไม่มี kernel layer ในงานนี้** — จบใน DB + middleware ล้วน จึง**ไม่ต้องแก้
    `kernel/interfaces.go` / `mock.go`** และไม่ต้องแตะ install.sh — ถ้าพบว่า
    กำลังจะแก้ไฟล์พวกนี้แปลว่าหลุด scope

---

## 5. ลำดับการลงมือทำ (แนะนำ)

```
1. Phase 1  model + migration + repository       → go build ผ่าน + เทสต์ migration DB เก่า
2. Phase 2  UserService + guard rails + เทสต์    → go test ผ่าน
3. Phase 3  auth rework (ถอน hardcode) + role middleware + /api/users + openapi
            → รันเทสต์เดิมทั้งหมด ต้องไม่พัง
4. Phase 4  frontend service + auth state + route guard
5. Phase 5  หน้า Users.tsx + เมนู System ใน ShellLayout
6. Phase 6  ทดสอบ mock mode → เครื่องจริง (เช็คผู้ใช้เดิมหลัง migrate) → อัปเดต README
```
