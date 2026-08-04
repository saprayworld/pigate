# แผนแก้ Dependabot alerts (npm / frontend) 15 รายการ

> เอกสารแผนงานสำหรับปิด Dependabot alerts ทั้ง 15 รายการใน `frontend/yarn.lock`
> วันที่เขียน: 2026-08-04 · อ้างอิงโค้ด: `main` @ `6a893d5` · Branch ที่จะใช้: `fix/dependabot-npm-2026-08`
> **สถานะ: รอเจ้าของโปรเจกต์ตัดสิน D-1 และ D-2 ก่อนเริ่ม T-04 / T-06** (T-01…T-03 เริ่มได้เลย)
>
> ทุกอย่างในแผนนี้เป็นการเปลี่ยน dependency ของ **frontend เท่านั้น** — ไม่มี Go/backend alert
> (ยืนยันจาก query จริง: ทั้ง 15 alert เป็น npm ecosystem, `backend/go.mod`/`go.sum` ไม่มี alert)

## 0. คำถามที่ต้องให้เจ้าของโปรเจกต์ตัดสิน (ล็อกก่อนลงมือ)

| # | คำถาม | ทางเลือก | ข้อเสนอของ tech lead |
|---|---|---|---|
| D-1 | react-router จะไปเวอร์ชันไหน | (ก) `7.18.2` — ปิด 4/5 alert, ไม่ต้องแก้โค้ดเลย / (ข) `8.3.0` — ปิด **5/5** แต่ต้องเลิกใช้แพ็กเกจ `react-router-dom` และแก้ import 16 ไฟล์ | **(ข) 8.3.0** — breaking change มีแค่ "ย้าย import path" และ prerequisite (react 19.2.7, vite 8) ในโปรเจกต์ครบแล้ว |
| D-2 | `@hono/node-server` (alert 12) แก้ยังไง | (ก) ลบ `shadcn` ออกจาก devDependencies (CLAUDE.md ระบุอยู่แล้วว่าให้เพิ่ม component ด้วย `npx shadcn@latest add`) / (ข) ใส่ `resolutions` บังคับ `^2.0.5` ทับใน `@modelcontextprotocol/sdk` / (ค) dismiss alert (ช่องโหว่เป็น path traversal เฉพาะ Windows, เป็น dev CLI ที่ไม่เคยรันเป็นเซิร์ฟเวอร์) | **(ก) ลบ `shadcn` devDep** — ปิด alert 12 ได้จริง ลด dep tree ลงมาก และตรงกับวิธีทำงานที่ใช้อยู่แล้ว ถ้าเจ้าของยังอยากได้ shadcn MCP server ในเครื่อง ให้เลือก (ค) แทน — ห้ามเลือก (ข) เพราะเป็นการ force major bump เข้าไปในไส้ของ MCP SDK |

**นอกขอบเขต (ห้าม ai-developer ทำ)**

- ห้ามแตะ `backend/` ทุกไฟล์ (ไม่มี Go alert)
- ห้ามแก้ตรรกะ/UI ของหน้าใดๆ — งานนี้เปลี่ยนได้แค่ `package.json`, `yarn.lock`, และ **บรรทัด import** ของ react-router
- ห้ามรัน `yarn upgrade` แบบ global (ไม่ระบุชื่อแพ็กเกจ) เพราะจะดัน dep ทั้งโปรเจกต์พร้อมกันจนหาสาเหตุ regression ไม่เจอ
- ห้าม `npm install` (โปรเจกต์ใช้ Yarn v1)
- ห้าม commit / เปิด PR เอง

## 1. ผลสำรวจ (ณ วันเขียนแผน)

โปรเจกต์ใช้ `frontend/package.json` + `frontend/yarn.lock` (lockfile v1 format, Yarn v1) ตามที่ CLAUDE.md ระบุ
ไม่มี field `resolutions` อยู่ในตอนนี้ และไม่มี GitHub Actions workflow (build ทำในเครื่อง)

### 1.1 ที่มาของแต่ละแพ็กเกจที่ติด alert

| Alert | แพ็กเกจ | เวอร์ชันใน lock | direct/transitive | สายที่ดึงเข้ามา | prod runtime? | ช่วง semver ในสายเดิมครอบเวอร์ชันที่แพตช์แล้วไหม |
|---|---|---|---|---|---|---|
| 21,20,19,18,17 | `react-router` | 7.17.0 | transitive (แต่ pin ตรงจาก `react-router-dom@7.17.0`) | `react-router-dom` (direct dep) | **ใช่** (routing ทั้งแอป) | ต้องขยับ direct dep เอง |
| 23 | `postcss` | 8.5.15 และ 8.5.16 (สองก้อน) | transitive | `vite@8.0.16` (`^8.5.15`), `shadcn@4.13.0` (`^8.5.6`) | ไม่ — build-time | **ครอบ** (latest 8.5.25 ≥ 8.5.18) |
| 22 | `brace-expansion` | 5.0.7 | transitive | `minimatch@10` ← `eslint`, `typescript-eslint`, `@ts-morph/common`, **และ `@swagger-api/apidom-reference`** | บางส่วน (apidom = ไส้ของ swagger-ui) | **ครอบ** (`^5.0.5` → 5.0.8) |
| 16 | `fast-uri` | 3.1.3 | transitive | `ajv@8` ← `@modelcontextprotocol/sdk`, `conf` (← `@dotenvx/dotenvx`) → ทั้งคู่มาจาก `shadcn` (dev) | ไม่ | **ครอบ** (`^3.0.1` → 3.1.4) |
| 24,25 | `ip-address` | 10.2.0 | transitive | `express-rate-limit@8` ← `@modelcontextprotocol/sdk` ← `shadcn` (dev) | ไม่ | **ครอบ** (`^10.2.0` → 10.2.2) |
| 12 | `@hono/node-server` | 1.19.14 | transitive | `@modelcontextprotocol/sdk@1.29.0` (`^1.19.9`) ← `shadcn` (dev) | ไม่ | **ไม่ครอบ** — ต้องข้ามไป 2.x (ดู D-2) |
| 15 | `dompurify` | 3.4.11 | transitive | `swagger-ui-react` (direct, `^3.4.11`) | **ใช่** (หน้า `/api-docs`) | ครอบ แต่ทางที่ถูกคือดัน parent (ดู 1.2) |
| 13,14 | `immutable` | 3.8.3 | transitive | `swagger-ui-react` (`^3.x.x`) | **ใช่** | **ไม่ครอบ** ในเวอร์ชัน swagger ปัจจุบัน → ต้องดัน parent |
| 11 | `js-yaml` | 4.2.0 (pin `=4.2.0`) + 4.3.0 (ปลอดภัยแล้ว จาก `cosmiconfig`) | transitive | `swagger-ui-react` pin `js-yaml "=4.2.0"` | **ใช่** | **ไม่ครอบ** (pin ตรงเป๊ะ) → ต้องดัน parent |

### 1.2 กุญแจสำคัญ: `swagger-ui-react` ตัวเดียวปิด 4 alert

`swagger-ui-react@5.32.12` (latest) ประกาศ dep เป็น `immutable: "^4.3.9"`, `js-yaml: "=4.3.0"`, `dompurify: "^3.4.12"` — ตรงกับเวอร์ชันที่แพตช์ทั้งสามตัวพอดี
และ range ใน `package.json` ปัจจุบันคือ `"swagger-ui-react": "^5.32.6"` → **อนุญาต 5.32.12 อยู่แล้ว** จึงไม่ต้องแตะ `package.json` แค่ re-resolve lockfile

> หมายเหตุความเสี่ยง: swagger-ui ข้าม `immutable` 3 → 4 ในไส้ตัวเอง (พร้อม `redux-immutable`, `react-immutable-proptypes`) เป็น major bump ของ runtime หน้า `/api-docs` → ต้องเปิดหน้าดูจริงตอนทดสอบรวม

### 1.3 react-router: ต้องไปถึงไหนถึงจะปิดครบ

เวอร์ชันปัจจุบัน **7.17.0** ติดครบทั้ง 5 alert:

- alert 21 / 20 / 19 / 17 → ช่วง `<7.18.0` → **7.18.2 (`version-7` tag) ปิดได้**
- alert 18 (RSC Mode CSRF Bypass) → ช่วง `>=7.12.0 <8.3.0` → **ต้อง 8.3.0 เท่านั้น** (npm dist-tag `latest` = `8.3.0` มีอยู่จริงแล้ว)

ความเสี่ยงจริงของ alert ที่ยังค้างถ้าเลือก 7.18.2: RSC/SSR mode ไม่ได้ถูกใช้ในโปรเจกต์นี้เลย (SPA + `BrowserRouter` ล้วน) แต่ **Dependabot จะไม่ปิด alert 18 ให้** จนกว่าจะขึ้น 8.3.0 หรือ dismiss เอง

**สิ่งที่ v8 พังเทียบกับ v7 (เท่าที่กระทบโปรเจกต์นี้):**

1. แพ็กเกจ `react-router-dom` **ถูกลบใน v8** (npm `latest` ของ `react-router-dom` ค้างที่ 7.18.2) → ทุกอย่างต้อง import จาก `react-router`, ส่วน `RouterProvider`/`HydratedRouter` ย้ายไป `react-router/dom` (**โปรเจกต์นี้ไม่ได้ใช้ทั้งสองตัว** — ใช้ `BrowserRouter` แบบ declarative)
2. ESM-only (ไม่มี CJS build แล้ว) — Vite 8 รับได้
3. baseline ใหม่: React ≥ 19.2.7 (lock มี **19.2.7** พอดี), Vite ≥ 7 (มี **8.0.16**), Node ≥ 22.22
4. future flags / `middleware` / `meta.loaderData` / server adapter — **ไม่เกี่ยว** เพราะไม่ได้ใช้ Framework Mode หรือ data router

API ของ react-router ที่โค้ดใช้จริง (16 ไฟล์): `BrowserRouter`, `Routes`, `Route`, `Navigate`, `Outlet`, `Link`, `NavLink`, `useNavigate`, `useLocation`, `useParams`, `useSearchParams` — **ทั้งหมด export จาก `react-router` ตั้งแต่ v7 แล้ว** จึงเป็นการเปลี่ยน specifier ล้วนๆ ไม่มี rename API

### 1.4 กลยุทธ์สำหรับ transitive deps: ทำไมไม่ใช้ `resolutions`

- แพ็กเกจ 4 ตัว (`postcss`, `brace-expansion`, `fast-uri`, `ip-address`) มี range เดิม **ครอบเวอร์ชันที่แพตช์แล้ว** → แค่ให้ Yarn re-resolve ก็จบ ไม่ต้องบังคับ
- วิธี re-resolve ที่เชื่อถือได้ที่สุดใน Yarn v1: **ลบเฉพาะ block ของแพ็กเกจนั้นออกจาก `yarn.lock` แล้วรัน `yarn install`** (`yarn upgrade <pkg>` ของ Yarn v1 มีพฤติกรรมไม่แน่นอนกับ dep ที่ไม่ได้อยู่ใน `package.json` และบางกรณีจะไปเพิ่ม entry ใน `package.json` ให้เอง ซึ่งไม่ต้องการ)
- `resolutions` เป็นหนี้ทางเทคนิค: ต้องมานั่งรื้อทีหลังเมื่อ parent อัปเดต และ Yarn v1 ไม่เตือนเมื่อมันล้าสมัย → **ใช้เฉพาะเมื่อไม่มีทางอื่นจริงๆ** ซึ่งในแผนนี้คือ "ไม่ใช้เลย"

## 2. Task list

```json
[
  {
    "task_id": "T-01",
    "title": "เตรียม branch + บันทึก baseline",
    "layer": "frontend",
    "files": [],
    "instruction": "สร้าง branch fix/dependabot-npm-2026-08 จาก main. ที่ frontend/ รัน `yarn install` ให้ node_modules ตรงกับ yarn.lock ปัจจุบัน แล้วรัน `yarn build` และ `yarn lint` เก็บผลไว้เป็น baseline (ต้องเขียวก่อนเริ่มแก้ ถ้าไม่เขียวให้หยุดและรายงานกลับทันที ห้ามแก้ต่อ). บันทึกเวอร์ชัน node/yarn ที่ใช้ (`node -v`, `yarn -v`) — ถ้า node < 22.22 ให้รายงานกลับก่อนทำ T-04 เพราะ react-router v8 ตั้ง baseline ไว้ที่ Node 22.22. ห้ามแก้ไฟล์ใดๆ ใน task นี้",
    "acceptance": [
      "อยู่บน branch fix/dependabot-npm-2026-08",
      "yarn build และ yarn lint ผ่านบนโค้ดเดิม (บันทึกผลไว้)",
      "รายงานเวอร์ชัน node/yarn"
    ],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "ดัน swagger-ui-react ให้ re-resolve เป็น 5.32.12 (ปิด alert 11, 13, 14, 15)",
    "layer": "frontend",
    "files": ["frontend/yarn.lock"],
    "instruction": "ห้ามแก้ frontend/package.json (range ^5.32.6 ครอบ 5.32.12 อยู่แล้ว). ใน frontend/yarn.lock ให้ลบ block ของ `swagger-ui-react@^5.32.6` ออก พร้อมกับ block ของลูกที่ต้องขยับตาม: `dompurify@^3.4.11`, `immutable@^3.x.x`, `js-yaml@=4.2.0` (อย่าลบ block `js-yaml@^4.1.0, js-yaml@^4.2.0` ที่เป็น 4.3.0 อยู่แล้ว) จากนั้นรัน `yarn install` ที่ frontend/. เสร็จแล้วตรวจ yarn.lock ว่า swagger-ui-react >= 5.32.12, immutable >= 4.3.9, js-yaml ทุก entry >= 4.3.0, dompurify >= 3.4.12 และไม่มี entry immutable 3.x เหลืออยู่. ถ้ามี immutable 3.x ค้าง ให้ระบุว่า block ไหนดึงมันเข้ามาแล้วรายงานกลับ ห้ามใส่ resolutions เอง",
    "acceptance": [
      "yarn.lock: swagger-ui-react >= 5.32.12, dompurify >= 3.4.12, immutable >= 4.3.9, js-yaml ทุก entry >= 4.3.0",
      "package.json ไม่ถูกแก้",
      "yarn install จบโดยไม่มี error"
    ],
    "depends_on": ["T-01"]
  },
  {
    "task_id": "T-03",
    "title": "Re-resolve transitive: postcss / brace-expansion / fast-uri / ip-address (ปิด alert 16, 22, 23, 24, 25)",
    "layer": "frontend",
    "files": ["frontend/yarn.lock"],
    "instruction": "ใน frontend/yarn.lock ให้ลบ block เหล่านี้ทั้งหมด: `postcss@^8.5.15`, `postcss@^8.5.6`, `brace-expansion@^5.0.5`, `fast-uri@^3.0.1`, `ip-address@^10.2.0` แล้วรัน `yarn install` ที่ frontend/. ห้ามแก้ package.json และห้ามเพิ่ม field resolutions. หลังติดตั้งให้ยืนยันในyarn.lock ว่า postcss ทุก entry >= 8.5.18, brace-expansion >= 5.0.8, fast-uri >= 3.1.4, ip-address >= 10.2.2. ถ้าตัวใดตัวหนึ่ง re-resolve แล้วยังต่ำกว่าเวอร์ชันที่แพตช์ ให้หยุดและรายงานพร้อมชื่อ parent ที่ล็อกช่วงไว้ ห้ามแก้เกินขอบเขต",
    "acceptance": [
      "yarn.lock: postcss ทุก entry >= 8.5.18, brace-expansion >= 5.0.8, fast-uri >= 3.1.4, ip-address >= 10.2.2",
      "package.json ไม่ถูกแก้ และไม่มี field resolutions ถูกเพิ่ม",
      "yarn install จบโดยไม่มี error"
    ],
    "depends_on": ["T-02"]
  },
  {
    "task_id": "T-04",
    "title": "เปลี่ยน dependency react-router-dom -> react-router ^8.3.0 (ทำเมื่อเจ้าของเลือก D-1 = ข)",
    "layer": "frontend",
    "files": ["frontend/package.json", "frontend/yarn.lock"],
    "instruction": "ที่ frontend/ รัน `yarn remove react-router-dom` แล้ว `yarn add react-router@^8.3.0` (Yarn v1 เท่านั้น ห้าม npm). ตรวจว่า package.json dependencies มี react-router ^8.3.0 และไม่มี react-router-dom เหลือ. ถ้า yarn รายงาน peer dependency warning ให้คัดลอกข้อความมารายงานทั้งหมด (คาดว่า react >= 19.2.7 ซึ่ง lock มีอยู่แล้ว). ห้ามแก้ไฟล์ใน src/ ใน task นี้ — TypeScript จะพังชั่วคราว ซึ่งถูกต้อง แล้วไปแก้ใน T-05. ถ้าเจ้าของเลือก D-1 = (ก) ให้ทำแทนด้วย `yarn upgrade react-router-dom@^7.18.2` และข้าม T-05 ไปเลย",
    "acceptance": [
      "package.json: มี react-router ^8.3.0 (หรือ react-router-dom ^7.18.2 ถ้าเลือกทางเลือก ก) และไม่มีแพ็กเกจอีกตัวค้าง",
      "yarn.lock: react-router >= 8.3.0",
      "บันทึก peer dependency warning ทั้งหมดที่ yarn พ่นออกมา"
    ],
    "depends_on": ["T-03"]
  },
  {
    "task_id": "T-05",
    "title": "แก้ import react-router-dom -> react-router ทั้ง 16 ไฟล์",
    "layer": "frontend",
    "files": [
      "frontend/src/App.tsx",
      "frontend/src/components/layout/ShellLayout.tsx",
      "frontend/src/components/site-header.tsx",
      "frontend/src/components/app-sidebar.tsx",
      "frontend/src/components/nav-user.tsx",
      "frontend/src/components/statistics/DnsStatsShared.tsx",
      "frontend/src/pages/ApiDocs.tsx",
      "frontend/src/pages/DnsServer.tsx",
      "frontend/src/pages/ForceChangePassword.tsx",
      "frontend/src/pages/Login.tsx",
      "frontend/src/pages/StatisticsOverview.tsx",
      "frontend/src/pages/StatisticsTraffic.tsx",
      "frontend/src/pages/StatisticsTrafficHost.tsx",
      "frontend/src/pages/StatisticsDns.tsx",
      "frontend/src/pages/StatisticsDnsDomain.tsx",
      "frontend/src/pages/StatisticsDnsClient.tsx"
    ],
    "instruction": "เปลี่ยนเฉพาะ specifier ของ import: from \"react-router-dom\" -> from \"react-router\" (ทั้งแบบ double quote และ single quote ที่ ApiDocs.tsx ใช้). ห้ามเปลี่ยนรายชื่อ named import, ห้ามจัดรูปแบบโค้ดใหม่, ห้ามแตะบรรทัดอื่นนอกจากบรรทัด import. รายชื่อ API ที่ใช้ (BrowserRouter, Routes, Route, Navigate, Outlet, Link, NavLink, useNavigate, useLocation, useParams, useSearchParams) ทั้งหมด export จาก react-router โดยตรง ไม่มีตัวไหนต้องย้ายไป react-router/dom เพราะโปรเจกต์ไม่ได้ใช้ RouterProvider/HydratedRouter. เสร็จแล้วรัน `grep -rn \"react-router-dom\" frontend/src` ต้องไม่เหลือผลลัพธ์ และรัน `yarn build` ให้ผ่าน",
    "acceptance": [
      "ไม่มีสตริง react-router-dom เหลือใน frontend/src",
      "yarn build (tsc -b && vite build) ผ่าน",
      "diff ของแต่ละไฟล์มีแค่บรรทัด import เท่านั้น"
    ],
    "depends_on": ["T-04"]
  },
  {
    "task_id": "T-06",
    "title": "จัดการ @hono/node-server (alert 12) ตามที่เจ้าของเลือกใน D-2",
    "layer": "frontend",
    "files": ["frontend/package.json", "frontend/yarn.lock"],
    "instruction": "ทำตามคำตัดสิน D-2 เท่านั้น ห้ามเลือกเอง. ถ้า (ก): รัน `yarn remove shadcn` ที่ frontend/ แล้วยืนยันว่า yarn.lock ไม่เหลือ block ของ shadcn, @modelcontextprotocol/sdk, @hono/node-server, express-rate-limit, ip-address, @dotenvx/dotenvx และรัน yarn build/lint ให้ผ่าน (shadcn เป็น CLI ไม่ได้ถูก import ในโค้ด — ยืนยันด้วย grep ก่อนลบ). ถ้า (ค) dismiss: ไม่ต้องแก้โค้ด ให้รายงานกลับว่าเจ้าของต้องกด dismiss alert 12 บน GitHub ด้วยเหตุผล 'dev CLI only, Windows-only path traversal, ไม่ได้รันเป็นเซิร์ฟเวอร์'. ห้ามใส่ resolutions บังคับ @hono/node-server 2.x",
    "acceptance": [
      "ถ้า (ก): grep ยืนยันว่าไม่มีโค้ดใน frontend/src import shadcn, package.json ไม่มี shadcn, yarn.lock ไม่มี @hono/node-server",
      "ถ้า (ค): ไม่มีไฟล์ถูกแก้ และมีข้อความรายงานให้เจ้าของ dismiss",
      "ไม่มีการเพิ่ม field resolutions"
    ],
    "depends_on": ["T-05"]
  },
  {
    "task_id": "T-07",
    "title": "อัปเดตเอกสารเมื่อ dependency เปลี่ยนสายตา",
    "layer": "frontend",
    "files": ["CLAUDE.md", "README.md"],
    "instruction": "ทำเฉพาะเมื่อ T-04 เลือกทาง (ข) และ/หรือ T-06 เลือกทาง (ก). แก้เอกสารเท่าที่จำเป็น: (1) ถ้าใช้ react-router v8 ให้เพิ่มโน้ตสั้นๆ ในส่วน frontend ของ CLAUDE.md ว่า import ทั้งหมดต้องมาจาก react-router (แพ็กเกจ react-router-dom ถูกลบใน v8) (2) ถ้าลบ shadcn ออกจาก devDependencies ให้ปรับประโยคเดิมที่บอกวิธีเพิ่ม component ให้ชัดว่าใช้ `npx shadcn@latest add <component>` โดยไม่มี shadcn ติดตั้งในโปรเจกต์. ห้ามแก้เนื้อหาส่วนอื่นของเอกสาร",
    "acceptance": [
      "เอกสารสะท้อนสถานะ dependency ใหม่ ไม่มีคำแนะนำที่ขัดกับ package.json ที่แก้แล้ว",
      "diff จำกัดอยู่ที่ย่อหน้าที่เกี่ยวข้องเท่านั้น"
    ],
    "depends_on": ["T-06"]
  }
]
```

## 3. เกณฑ์ทดสอบรวมท้ายแผน (Final Acceptance) — รันครั้งเดียวหลัง T-01…T-07 เสร็จครบ

```json
{
  "final_acceptance": [
    "cd frontend && yarn install --frozen-lockfile จบโดยไม่มี error และไม่แก้ yarn.lock เพิ่ม",
    "cd frontend && yarn build (tsc -b && vite build) ผ่าน ไม่มี TS error และไม่มี warning ใหม่เกี่ยวกับ module resolution",
    "cd frontend && yarn lint ผ่าน ไม่มี error ใหม่เทียบกับ baseline ใน T-01",
    "bash build.sh สร้าง ./pigate ได้สำเร็จ (frontend dist ถูก copy เข้า backend/internal/api/dist)",
    "grep ยืนยันเวอร์ชันใน frontend/yarn.lock: react-router >= 8.3.0 (หรือ 7.18.2 ถ้าเจ้าของเลือก D-1 ก), postcss ทุก entry >= 8.5.18, brace-expansion >= 5.0.8, fast-uri >= 3.1.4, ip-address >= 10.2.2, dompurify >= 3.4.12, immutable >= 4.3.9 และไม่มี immutable 3.x, js-yaml ทุก entry >= 4.3.0, ไม่มี @hono/node-server (ถ้าเลือก D-2 ก)",
    "grep -rn 'react-router-dom' frontend/ (ยกเว้น yarn.lock ที่ต้องไม่มีอยู่แล้ว) ต้องไม่เจอผลลัพธ์",
    "รัน backend แบบ mock (./pigate-backend -mock=true -allow-dev-cors) + yarn dev แล้วทดสอบ routing ด้วยมือ: login -> redirect ไป /dashboard, sidebar NavLink ทุกเมนู (network/policy/logs/system/statistics) เปลี่ยนหน้าได้และ active state ถูกต้อง",
    "ทดสอบ route ที่มี param: /statistics/traffic/host/:ip, /statistics/dns/domain/:domain, /statistics/dns/client/:client เข้าได้ทั้งจากการคลิก drill-down และการพิมพ์ URL ตรง (useParams อ่านค่าได้ ไม่ 404)",
    "ทดสอบ query string: หน้า DnsServer และ DnsStatsShared ใช้ useSearchParams แล้วค่ายังอ่าน/เขียนได้ถูกต้อง",
    "ทดสอบ guard: เข้า /dashboard โดยไม่ล็อกอิน -> เด้งไป /login, เข้า /system/users ด้วย role ที่ไม่ใช่ super_admin -> เด้งไป /dashboard, URL มั่วๆ -> เด้งเข้า / ตาม catch-all",
    "เปิดหน้า /api-docs แล้ว SwaggerUI เรนเดอร์ /openapi.yaml ได้ครบ ไม่มี error ใน console (จุดเสี่ยงสูงสุด: swagger-ui ข้าม immutable 3 -> 4)",
    "ทดสอบ dark/light mode ทั้งหน้า /api-docs และหน้าหลัก ยังไม่พัง",
    "หลังเจ้าของ merge PR แล้ว: Dependabot alerts บน repo saprayworld/pigate ปิดครบ 15 รายการ (หรือเหลือเฉพาะรายการที่เจ้าของตัดสินใจ dismiss ตาม D-2)"
  ]
}
```

## 4. ความเสี่ยงที่ ai-developer ต้องระวังเป็นพิเศษ

1. **react-router v8 = major bump** — จุดที่พังจริงมีแค่ import specifier แต่ถ้าเผลอแก้ named import หรือย้ายบางตัวไป `react-router/dom` จะพังทั้งแอป → แก้เฉพาะสตริง `"react-router-dom"` เท่านั้น
2. **v8 เป็น ESM-only + baseline Node 22.22** — ถ้าเครื่อง dev/host ที่ build จริงใช้ Node เก่ากว่านั้น `vite build` อาจพังแบบสับสน → เช็คใน T-01 ก่อน
3. **swagger-ui-react ข้าม immutable 3 → 4** — เป็น major bump ในไส้ของหน้า `/api-docs` (ซึ่งเป็น route สาธารณะ ไม่มี auth guard) ต้องเปิดดูจริงในเบราว์เซอร์ ไม่ใช่แค่ build ผ่าน
4. **อย่าใช้ `resolutions`** — ทุกเคสในแผนนี้แก้ได้ด้วยการ re-resolve หรือดัน parent; `resolutions` จะกลายเป็นหนี้ที่ Yarn v1 ไม่เตือนเมื่อล้าสมัย
5. **อย่ารัน `yarn upgrade` เปล่าๆ** — จะดัน dep ทั้งโปรเจกต์ (radix, tailwind 4, recharts, vite) พร้อมกัน แล้วแยกไม่ออกว่า regression มาจากอะไร
6. **`yarn.lock` แก้ด้วยมือได้เฉพาะ "ลบทั้ง block"** — ห้ามแก้เลข version/integrity ด้วยมือเด็ดขาด (integrity ไม่ตรงจะพังตอน CI/install สะอาด)
7. งานนี้ไม่แตะ auth/firewall/kernel แต่ **`react-router` เป็นส่วนหนึ่งของ route guard** (`ProtectedRoute`, `SuperAdminRoute`) → ให้ถือว่าการทดสอบ guard เป็นข้อบังคับ (การบังคับสิทธิ์จริงอยู่ที่ backend middleware อยู่แล้ว แต่ห้ามให้ UX guard พัง)
8. โค้ดทั้งหมดอยู่บน branch `fix/dependabot-npm-2026-08` และเข้า main ผ่าน PR เท่านั้น — ai-developer/ai-qa ไม่ commit เอง
