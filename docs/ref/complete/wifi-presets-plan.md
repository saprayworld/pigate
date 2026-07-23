# Wi-Fi Saved-Networks (Preset) Library — คลังเครือข่าย Wi-Fi ที่บันทึกไว้ (issue #66)

> เอกสารแผนงานสำหรับฟีเจอร์ "คลังเครือข่ายที่บันทึกไว้" (saved networks / presets)
> เก็บชุด SSID + password + security + mac_mode เป็นชื่อ preset แล้วนำไปเติมลง
> ช่อง primary/backup ของ WLAN interface ได้เร็ว ๆ (คล้าย "known networks" ในมือถือ)
> ปัจจุบัน credential ถูกเก็บ inline อยู่ในแต่ละ interface (primary+backup เท่านั้น)
> ยังไม่มีคลังกลางให้ใช้ซ้ำข้าม interface
>
> วันที่เขียน: 2026-07-19 · Branch อ้างอิง: `main` (แยกงานที่ `feat/wifi-presets`)
> อ้างอิง: issue #66 · CLAUDE.md · `docs/wifi_wpa_working_instruction.md` · `docs/ref/instruction/work-planning-instruction.md`

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย (พฤติกรรมที่ผู้ใช้เห็น):**
  - ผู้ใช้สร้าง/แก้ไข/ลบ preset (ชื่อ, SSID, security, password, mac_mode) ได้จาก UI
  - ในหน้า Interfaces (WLAN) ผู้ใช้เลือก preset จาก dropdown แล้วกด "Apply" → ระบบ
    เติมค่าลงช่อง **primary** หรือ **backup** ของ interface นั้น แล้ว apply เข้า kernel จริง
  - **password ไม่มีวันวิ่งผ่าน browser** — ทั้งตอนอ่าน preset และตอน apply
- **เงื่อนไขเชิงเทคนิคที่ต้องเป็นจริง (Decision ที่ owner ล็อกแล้วในคอมเมนต์ issue):**
  - password เป็น **write-only**: เก็บ plaintext ใน DB (เหมือน interface ปัจจุบัน) แต่
    **ไม่เคยส่งกลับ frontend** — GET คืนแค่ boolean `hasPassword`
  - การ apply ทำ **server-side เท่านั้น** ผ่าน `POST /api/wifi-presets/{id}/apply
    { interfaceId, slot }` — backend อ่าน password จาก DB เองแล้วเรียก `ConfigureWifi(...)`
    **ยกเลิก** ทางเลือกที่ frontend ดึง password มายัด payload ของ PATCH interface
- **นอกขอบเขต (เขียนชัดเพื่อกันแผนบวม):**
  - ไม่เพิ่ม kernel capability ใหม่ — ใช้ `ConfigureWifi(...)` เดิมผ่าน `ApplyInterfaceConfig`
  - ไม่มี auto-connect / priority / weight logic ระหว่าง presets (priority ยังมาจาก slot
    primary=10 / backup=5 เดิมใน `GenerateWpaConfig`)
  - ไม่รองรับ WPA-Enterprise (WPA-EAP / 802.1X) ในรอบแรก — เฉพาะ Open/WPA2/WPA3/mixed
  - ไม่ทำ auto-sync ย้อนกลับ (แก้ preset แล้วไม่ push อัตโนมัติเข้าทุก interface ที่เคยใช้ preset นั้น —
    preset เป็นแค่ "แม่แบบตอน apply"; หลัง apply แล้ว interface มี copy ของตัวเอง)

## 0.1 การตัดสินใจเพิ่มเติมจาก owner (ล็อกแล้ว 2026-07-19)

- **Auth:** ทุก endpoint ของ `/api/wifi-presets` (list/create/update/delete **และ** `/apply`)
  ใช้ **`superAdminRoute`** แบบ explicit ทั้งชุด — ไม่ใช่ `authRoute` — เพราะเกี่ยวกับ
  Wi-Fi credential ทั้งหมด จำกัดที่ super_admin เท่านั้น
- **Sync:** ยืนยัน **One-way** — preset เป็นแค่แม่แบบตอน apply; แก้ preset ภายหลัง
  ไม่กระทบ interface ที่เคย apply ไปแล้ว (ไม่ทำ two-way sync)

## 1. สถานะปัจจุบัน (สำรวจโค้ดจริง ณ 2026-07-19)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| ตาราง `wifi_presets` / model / validator | ❌ ยังไม่มี | — |
| แม่แบบ Wi-Fi config → wpa_supplicant | ✅ มี (`ConfigureWifi` → `GenerateWpaConfig`) | `kernel/wpa.go:53`, interface `ConfigureWifi` |
| Sanitizer กัน config injection | ✅ มี `SanitizeWpaInput` (ตัด `\n \r "`) | `kernel/wpa.go:19` |
| Interface wifi fields (primary+backup) | ✅ มีครบใน model + DB | `model/types.go:180-191`, `db/connection.go` (`network_interfaces`) |
| Password masking ตอน GET interface | ✅ `maskInterfacePasswords` แทนด้วย `••••••••` | `api/handlers.go:115-121` |
| Skip-mask pattern ตอน PUT/PATCH (`!= "••••••••"`) | ✅ มี ใช้ซ้ำได้ | `api/handlers.go:561-595, 780-795` |
| Apply wifi → DB + kernel (persist+reapply) | ✅ `ApplyInterfaceConfig` (persist + `ConfigureWifi`) | `service/interface.go:591-635` |
| CRUD DB pattern (validate→exec, system-lock) | ✅ ล้อ address_objects ได้ | `db/repository.go:316-370` |
| Migration pattern (CREATE TABLE IF NOT EXISTS ใน slice) | ✅ | `db/connection.go:215-455` |
| Backup export/import + fail-closed validate | ✅ `BackupConfig` + `validateConfig` + `RestoreConfig` | `model/backup.go:55-76`, `service/backup.go:626-697`, `db/backup_repo.go:77-295` |
| Route + role middleware (`authRoute`/`superAdminRoute`, DisableEdit) | ✅ | `api/router.go:56-65, 180` |
| Frontend interface service (mock+real) | ✅ แม่แบบ | `frontend/src/services/interfaceService.ts` |
| Frontend wifi form + masked password | ✅ `formWifiPassword`, placeholder `••••••••` | `frontend/src/pages/Interfaces.tsx:188, 701, 1357` |

**สรุป:** ทุก primitive ที่ต้องใช้มีครบแล้ว งานคือ **เพิ่มตารางใหม่ + CRUD + endpoint `/apply`
ที่ประกอบ preset เข้ากับ `ApplyInterfaceConfig` เดิม + รวมเข้า backup + UI** — ไม่แตะ kernel layer เลย

## 2. แนวทางเทคนิค

### 2.1 Data model
ตารางใหม่ `wifi_presets` (config = persist ลง SQLite ปกติ ไม่ใช่ runtime state):

| column | type | หมายเหตุ |
|---|---|---|
| `id` | TEXT PRIMARY KEY | uuid ฝั่ง handler เหมือน address/route |
| `name` | TEXT UNIQUE NOT NULL | ชื่อที่ผู้ใช้ตั้ง |
| `ssid` | TEXT NOT NULL | |
| `security` | TEXT NOT NULL DEFAULT 'WPA2' | enum เดียวกับ interface |
| `password` | TEXT DEFAULT '' | plaintext (เหมือน `network_interfaces.wifi_password`) — **write-only** |
| `mac_mode` | TEXT DEFAULT '' | `''`/`hardware`/`randomized`/`laa` (optional) |
| `created_at` / `updated_at` | DATETIME DEFAULT CURRENT_TIMESTAMP | |

`model.WifiPreset` (ใน `model/types.go` หรือไฟล์ใหม่ `model/wifi_preset.go`):
```go
type WifiPreset struct {
    ID          string `json:"id"`
    Name        string `json:"name"`
    SSID        string `json:"ssid"`
    Security    string `json:"security"`
    Password    string `json:"password,omitempty"` // write-only: ยัดเข้า struct ตอนรับ, ล้างก่อนส่งออก
    MacMode     string `json:"macMode,omitempty"`
    HasPassword bool   `json:"hasPassword"`        // read-only flag แทน password ตอน GET
}
```
> **กติกา password:** handler ต้อง `SanitizeSecret`/ล้าง `Password=""` + set `HasPassword`
> ก่อน `writeJSON` เสมอ (ทำ helper `sanitizePresetForRead`) — ห้ามพึ่ง `omitempty` อย่างเดียว
> เพราะ plaintext ที่ไม่ว่างจะหลุดออก JSON

### 2.2 Validator (SENSITIVE — pure, testable)
`model.ValidateWifiPreset(p) error` ล้อสไตล์ `model/dns_validate.go` (pure + `_test.go` คู่กัน):
- `name` trim แล้วต้องไม่ว่าง; `ssid` trim แล้วต้องไม่ว่าง
- `security ∈ {Open, WPA2, WPA2-PSK, WPA3, WPA2/WPA3}` (ตรงกับ switch ใน `writeNetworkBlock`)
- `mac_mode ∈ {"", hardware, randomized, laa}`
- **anti-injection:** ปฏิเสธถ้า `SanitizeWpaInput(ssid) != ssid` หรือ `SanitizeWpaInput(password) != password`
  (มี `\n`/`\r`/`"`) — ป้องกันตั้งแต่ชั้น validate ไม่ให้ค่าหลุดไปประกอบ config wpa_supplicant
  > sanitizer เป็นแนวป้องกันหลักอยู่แล้ว แต่ที่นี่เลือก **reject** แทน silent-strip เพื่อให้ผู้ใช้รู้ตัว
  > และไม่เก็บค่าที่ต่างจากที่พิมพ์ลง DB

### 2.3 /apply flow (SENSITIVE — password server-side เท่านั้น)
`POST /api/wifi-presets/{id}/apply` body `{ "interfaceId": "...", "slot": "primary"|"backup" }`:
1. โหลด preset (พร้อม password) จาก repo ด้วย `{id}`
2. โหลด interface ด้วย `interfaceId` (`repo.GetInterfaceByID`) — 404 ถ้าไม่พบ
3. เช็ค `iface.Type == "wireless"` — 400 ถ้าไม่ใช่
4. เติมค่าตาม slot:
   - `primary` → `WifiSSID`, `WifiPassword`, `WifiSecurity` (+ `MacMode` ถ้า preset มี)
   - `backup` → `BackupSSID`, `BackupWifiPassword`, `BackupWifiSecurity`
5. เรียก `interfaceService.ApplyInterfaceConfig(iface)` — persist ลง DB + reapply เข้า kernel
   ผ่าน `ConfigureWifi` เดิม (จุดเดียวที่ password ถูกใช้ ทั้งหมดอยู่ backend)
6. คืน interface ที่ **ผ่าน `maskInterfacePasswords`** แล้ว (password เป็น `••••••••`)

**ที่อยู่ของ logic:** สร้าง `service/wifi_preset.go` → `WifiPresetService` ถือ `repo` +
reference ไป `*InterfaceService` (สำหรับ `ApplyInterfaceConfig`) — เคารพ layering api→service→db/kernel
(CRUD ก็ผ่าน service ตัวนี้ ไม่ให้ handler เรียก repo ตรง เพื่อความสม่ำเสมอ)

### 2.4 ทางเลือกที่ตัดทิ้ง
- **frontend ยัด password ลง PATCH interface** — ตัดทิ้งตาม decision owner (password วิ่งผ่าน browser)
- **เพิ่ม kernel interface method ใหม่** — ไม่จำเป็น เพราะ preset แค่เตรียมค่าให้ `ConfigureWifi` เดิม
  (ตาม CLAUDE.md: ไม่เพิ่ม capability ที่ไม่ต้องมี real+mock)
- **เก็บ password แบบ hash/encrypt** — ตัดทิ้ง เพราะ wpa_supplicant ต้องใช้ PSK plaintext; ให้สอดคล้อง
  กับพฤติกรรม `network_interfaces.wifi_password` ที่มีอยู่ (masking + no-return เป็นแนวป้องกันที่เลือก)

## 3. ขั้นตอนการทำ (เรียง inner-layer-first + ไฟล์ที่แตะ)

### Step 1 — migration + model + validator + unit test
- **`db/connection.go`** (แก้): เพิ่ม `CREATE TABLE IF NOT EXISTS wifi_presets (...)` ใน `queries` slice
  (~`:215-455`) — ตารางใหม่ล้วน ไม่ต้อง ALTER/backfill
- **`model/wifi_preset.go`** (ไฟล์ใหม่): struct `WifiPreset` + helper `sanitizePresetForRead`
- **`model/wifi_preset_validate.go`** (ไฟล์ใหม่): `ValidateWifiPreset` (§2.2)
- **`model/wifi_preset_validate_test.go`** (ไฟล์ใหม่): เคส name/ssid ว่าง, security/mac_mode นอก enum,
  ssid/password มี `\n`/`"` → error; เคสถูกต้องผ่าน

### Step 2 — repository CRUD
- **`db/wifi_preset_repo.go`** (ไฟล์ใหม่ — แยกจาก repository.go ให้อ่านง่าย):
  `GetWifiPresets` (คืน password ด้วยสำหรับ service; **การ mask ทำที่ handler**),
  `GetWifiPresetByID`, `CreateWifiPreset`, `UpdateWifiPreset`, `DeleteWifiPreset`,
  `WifiPresetNameExists` — ล้อ pattern `db/repository.go:316-370` (validate→exec, unique-name check)
- **`db/wifi_preset_repo_test.go`** (ไฟล์ใหม่): CRUD + unique-name (`:memory:` mock-safe)

### Step 3 — service layer (รวม /apply)
- **`service/wifi_preset.go`** (ไฟล์ใหม่): `WifiPresetService{repo, ifaceService}` + CRUD passthrough
  + `ApplyPresetToInterface(presetID, interfaceID, slot)` (§2.3)
- **`service/wifi_preset_test.go`** (ไฟล์ใหม่): apply primary/backup เติม field ถูกช่อง;
  interface ไม่ใช่ wireless → error; slot ไม่ถูกต้อง → error (รันบน mock network manager)

### Step 4 — wiring
- **`cmd/pigate/main.go`** (แก้): สร้าง `WifiPresetService` (ส่ง `interfaceService` เข้าไป) แล้วส่งเข้า
  `api.NewServer` เพิ่ม field
  > **ไม่ต้อง** `InitApplyConfig()` — preset ไม่มี state ที่ต้อง push เข้า kernel ตอน boot
  > (interface ถือ copy ของตัวเองหลัง apply อยู่แล้ว) ไม่แตะ startup apply order เดิม

### Step 5 — api handlers + routes (SENSITIVE)
- **`api/handlers.go`** (แก้): `HandleGetWifiPresets` (ผ่าน `sanitizePresetForRead`),
  `HandleCreateWifiPreset`, `HandleUpdateWifiPreset` (ใช้ skip-mask pattern `!= "••••••••"`
  แบบเดียวกับ interface ที่ `:584`), `HandleDeleteWifiPreset`, `HandleApplyWifiPreset`
- **`api/router.go`** (แก้ ~`:65` ต่อจากบล็อก interfaces): ลงทะเบียน 5 เส้น (ดู §4)
- **`api/handlers_test.go`** (แก้/เพิ่ม): ยืนยัน GET **ไม่มี** password ใน body (มีแค่ `hasPassword`);
  apply เติมค่าเข้า interface จริง (mock)

### Step 6 — backup export/import + validate (fail-closed)
- **`model/backup.go`** (แก้): เพิ่ม `Presets []WifiPreset \`json:"presets,omitempty"\`` ใน `BackupConfig`
  > **ต้อง `omitempty`** ด้วยเหตุผลเดียวกับ `PortForwards` (`:70-75`): importer verify checksum ด้วยการ
  > re-marshal → ถ้าไม่ omitempty ไฟล์ v2 เก่าจะได้ `"presets":null` แล้ว checksum พังทั้งหมด
- **`service/backup.go`** (แก้): เพิ่ม gather `cfg.Presets` ใน export (~`:100`), เพิ่ม fail-closed
  `ValidateWifiPreset` loop ใน `validateConfig` (~`:626-697`), เพิ่ม count
- **`db/backup_repo.go`** (แก้): wipe `DELETE FROM wifi_presets` + re-insert loop ใน `RestoreConfig`
  (`GetWifiPresets` ถูกเรียกใน export path — password ถูกเก็บใน backup plaintext เหมือน `wifi_password`
  ของ interface ที่ทำอยู่แล้วที่ `:349`)
- ทดสอบ round-trip export→import ต้องคง preset ครบ + checksum ผ่าน

### Step 7 — เอกสาร contract
- **`docs/openapi.yaml`** และ **`frontend/public/openapi.yaml`** (แก้ทั้งคู่ ต้อง sync): เพิ่ม schema
  `WifiPreset` (password `writeOnly: true`, มี `hasPassword`) + 5 paths
- **README Feature Status** (แก้): เพิ่มแถว "Wi-Fi Saved Networks" (Completed หลังเสร็จ)

### Step 8 — frontend service + UI
- **`frontend/src/services/wifiPresetService.ts`** (ไฟล์ใหม่): `getAll/create/update/remove/apply`
  รองรับ mock (localStorage) + real ล้อ `interfaceService.ts`; **type ไม่มี field password ตอนอ่าน**
  (มีแค่ `hasPassword`), `apply(presetId, {interfaceId, slot})`
- **`frontend/src/data-mockup/mockData.ts`** (แก้): seed presets ตัวอย่าง (ไม่มี plaintext password จริง)
- **UI**: จัดการ preset (list/create/edit/delete) + ใน dialog wifi ของ `Interfaces.tsx` เพิ่ม
  Combobox "Apply from saved network" + ปุ่มเลือก slot → เรียก `apply` (ไม่แตะ password)
  > Dialog ที่มี Combobox ต้องใช้ `<Dialog modal={false}>` (rules_of_work.md); ใช้ shadcn/ui เท่านั้น,
  > สี semantic, ห้าม `shadow-*`/hardcode palette, รองรับ dark/light

## 4. API ที่เกี่ยวข้อง

| Method | Path | Role | พฤติกรรม |
|---|---|---|---|
| GET | `/api/wifi-presets` | authRoute | list — **ไม่มี password**, มี `hasPassword` |
| POST | `/api/wifi-presets` | authRoute¹ | create |
| PUT | `/api/wifi-presets/{id}` | authRoute¹ | update (skip password ถ้าเป็น `••••••••`) |
| DELETE | `/api/wifi-presets/{id}` | authRoute¹ | delete |
| POST | `/api/wifi-presets/{id}/apply` | authRoute¹ | **SENSITIVE** apply server-side ลง slot ของ interface |

¹ `authRoute` + `RoleReadOnlyMiddleware` บล็อก mutation (POST/PUT/DELETE) สำหรับ non-super_admin อยู่แล้ว
  เหมือนทุก mutation ของ interface — **ดูจุดตัดสินใจ §5** ว่าจะยกระดับ `/apply` เป็น `superAdminRoute` แบบ explicit หรือไม่
  ทุกเส้น mutation ผ่าน `DisableEditMiddleware` (โหมด `-disable-edit=true` บล็อกทั้งหมด — ถูกต้องกับงานนี้)

## 5. ข้อควรระวัง (Cautions)

- **[สำคัญสุด] password รั่วออก GET:** ถ้าลืมล้าง `Password` ก่อน `writeJSON` plaintext จะหลุด
  → บังคับผ่าน `sanitizePresetForRead` ทุก read path (list + response ของ create/update/apply) และ
  เขียน test ที่ assert `body` **ไม่มี** key `password`/ค่า plaintext เด็ดขาด (ทำเป็น Definition of Done)
- **[SENSITIVE] anti-injection ของ SSID/password:** ค่าจากผู้ใช้จะไปประกอบไฟล์ `wpa_supplicant-*.conf`
  ผ่าน `GenerateWpaConfig` — ถ้าปล่อย `\n`/`"` เข้าไปได้ = config injection (แทรก network block/ตัวเลือกอื่น)
  → validate ที่ `ValidateWifiPreset` (reject) **และ** ยังคงมี `SanitizeWpaInput` ชั้น kernel เป็น defense-in-depth
  (อย่าถอด sanitizer ออกโดยคิดว่า validate ครอบแล้ว)
- **[SENSITIVE] /apply ต้องผ่าน review เข้ม:** endpoint นี้อ่าน secret จาก DB แล้ว mutate live wifi ของ
  interface — ตรวจ: (ก) `slot` ถูก whitelist เป็น `primary`/`backup` เท่านั้น, (ข) 404 เมื่อ preset/interface
  ไม่พบ, (ค) 400 เมื่อ interface ไม่ใช่ wireless, (ง) ไม่มี path ที่ interfaceId ผู้ใช้กำหนดทำให้แก้ของที่ไม่ควร
- **checksum ของ backup พังถ้าลืม omitempty:** `Presets` ต้อง `json:"presets,omitempty"` มิฉะนั้น
  ไฟล์ v2 เดิม (ไม่มี key นี้) จะ verify checksum ไม่ผ่าน (ดู `model/backup.go:70-75`) — ไม่ต้อง bump schema version
- **restore ต้อง wipe ก่อน insert:** เพิ่ม `DELETE FROM wifi_presets` ใน `wipes` slice ของ `RestoreConfig`
  (ลำดับ FK-safe — ตารางนี้ไม่มี FK ไปใคร ใส่ที่ไหนก็ได้) ไม่งั้น restore จะได้ preset ซ้ำ/ชนกับ unique name
- **unique name ชน:** create/update ต้องเช็ค `WifiPresetNameExists` แล้วคืน 409 (ล้อ address `AddressNameExists`)
  มิฉะนั้น INSERT จะ error ดิบจาก UNIQUE constraint (500 แทน 409 ที่สื่อความ)
- **preset ไม่ใช่ live-link ของ interface:** แก้ preset **ไม่** เปลี่ยน interface ที่เคย apply ไปแล้ว
  (ตั้งใจ — อยู่นอกขอบเขต) ระบุใน UI ให้ชัดว่าเป็น "แม่แบบตอน apply" กัน user เข้าใจผิดว่าเป็นการ sync
- **ทดสอบ mock-safe 100%:** CRUD/validate/apply ทดสอบบน `-mock=true` ได้ (mock network manager ไม่แตะ OS);
  การยืนยันว่า wpa_supplicant reconfigure จริงต้องทำบนบอร์ดจริง — ทดสอบเฉพาะตอนเข้าถึงตัวเครื่องได้
  และ **ระวัง apply preset ผิดลง WLAN ที่เป็นทางเข้า admin จะหลุดการเชื่อมต่อ** (apply บน interface ที่ไม่ใช่ทางเข้าก่อน)
- **ไม่แตะ netlink_monitor / startup apply order / kernel layer:** ยืนยันแล้วว่าไม่มี state ที่ต้อง reconcile
  หรือ apply ตอน boot — งานอยู่ใน db/model/service/api/frontend เท่านั้น

## 6. Checklist สรุป (Definition of Done)

- [ ] `db/connection.go` — `CREATE TABLE IF NOT EXISTS wifi_presets`
- [ ] `model/wifi_preset.go` + `model/wifi_preset_validate.go` (+ `_test.go`) — struct, `sanitizePresetForRead`, `ValidateWifiPreset`
- [ ] `db/wifi_preset_repo.go` (+ `_test.go`) — CRUD + unique-name (mock-safe `:memory:`)
- [ ] `service/wifi_preset.go` (+ `_test.go`) — CRUD + `ApplyPresetToInterface` (primary/backup)
- [ ] `cmd/pigate/main.go` — สร้าง service + ส่งเข้า `api.NewServer`
- [ ] `api/handlers.go` + `api/router.go` (+ test) — 5 handlers/routes; GET ไม่มี password; skip-mask ตอน update
- [ ] `model/backup.go` + `service/backup.go` + `db/backup_repo.go` — export/validate(fail-closed)/restore + round-trip test
- [ ] `docs/openapi.yaml` **และ** `frontend/public/openapi.yaml` — schema `WifiPreset` (`writeOnly` password) + 5 paths (sync)
- [ ] README Feature Status — เพิ่มแถว Wi-Fi Saved Networks
- [ ] `frontend/src/services/wifiPresetService.ts` (+ mockData seed) — mock+real, type ไม่มี password ตอนอ่าน
- [ ] `frontend/src/pages/Interfaces.tsx` (หรือ page/section ใหม่) — จัดการ preset + Combobox apply (Dialog `modal={false}`)
- [ ] `cd backend && go build ./... && go vet ./... && go test ./...` เขียว
- [ ] `cd frontend && yarn build && yarn lint` เขียว
- [ ] ทดสอบ mock: create/edit/delete preset; apply → interface เปลี่ยนค่า; GET ไม่คืน password
- [ ] ทดสอบ role: non-super_admin โดน block mutation; `-disable-edit=true` block ทุก mutation
- [ ] (บอร์ดจริง เฉพาะตอนเข้าถึงเครื่องได้) apply preset → wpa_supplicant reconfigure เชื่อมต่อได้จริง
- [ ] แยก branch `feat/wifi-presets` → PR เข้า main (code change ห้าม push ตรง)
