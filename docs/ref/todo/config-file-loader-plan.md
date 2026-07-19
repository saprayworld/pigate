# Config File Loader — โหลด runtime config จากไฟล์ `key=value` (issue #68)

> แผนงานเพิ่มการโหลด runtime configuration จากไฟล์ `key=value` เพื่อให้ปรับค่า
> (mock, db, https-port, docker-compat ฯลฯ) ได้โดยไม่ต้องแก้ systemd unit — ปัจจุบัน
> ค่าเหล่านี้ตั้งได้ทางเดียวคือ CLI flag ใน `ExecStart` ของ `pigate.service`
> เพิ่มแพ็กเกจใหม่ `internal/config` (pure, testable) + flag `-config` + auto-write
> ไฟล์ default ครั้งแรก โดย **flag ที่ระบุจริงยังชนะไฟล์เสมอ** (unit เดิมไม่พัง)
>
> เขียนเมื่อ: 2026-07-19 · Reference branch: `main` (แยกงานที่ `feat/config-file-loader`)
> อ้างอิง: issue #68 · CLAUDE.md (no new deps, stdlib เท่านั้น; code change → feature branch → PR)

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย:** binary อ่าน config จากไฟล์ `key=value`
  1. `-config=/path` → ใช้ไฟล์นั้น (ไม่มีไฟล์ → **error ชัด fail fast**)
  2. ไม่ระบุ `-config` → default path `/var/lib/pigate/pigate.conf`
  3. default path ไม่มีไฟล์ → ใช้ code default แล้ว **เขียนไฟล์ default ลงไป** (เขียนไม่สำเร็จ → warn แล้วรันต่อ)
- **Precedence (ต่ำ→สูง):** `code default` < `ไฟล์ config` < `CLI flag ที่ถูกระบุจริง`
  ใช้ `flag.Visit` แยกเฉพาะ flag ที่ผู้ใช้ตั้งจริง เพื่อให้ flag ใน unit เดิม (`-mock=false -db=... -https-port=443`)
  ยังชนะไฟล์เสมอ → พฤติกรรมของ install เดิมไม่เปลี่ยน
- **"เสร็จ" คือ:** รัน `pigate -config=x.conf` โหลดค่าจากไฟล์; รันเปล่าบนเครื่องที่ไม่มีไฟล์ default →
  เขียน `pigate.conf` แล้วบูตด้วย code default; ใส่ flag ทับค่าไฟล์ได้จริง; malformed int/bool ในไฟล์ → process ตายพร้อม error ชัด
- **Out of scope:** ย้าย flag ทุกตัวใน unit เข้าไฟล์ (ทำแค่ production set), hot-reload/reload-on-SIGHUP,
  การเก็บ secret ในไฟล์, การ validate เชิงความหมายของค่า (เช่น port range) — คงพฤติกรรม parse เดิมของ flag

## 1. สภาพโค้ดปัจจุบัน (สำรวจ ณ วันเขียน 2026-07-19)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| แพ็กเกจ `internal/config` | ❌ **ยังไม่มี** (ยืนยันด้วย `ls internal/`: api/db/kernel/logs/model/service เท่านั้น) | — |
| การลงทะเบียน flag ทั้งหมด | ✅ อยู่ใน `main()` ตรง ๆ 13 ตัว | `backend/cmd/pigate/main.go:37-50` |
| ค่า default ปัจจุบัน (ต้อง 1:1 กับ `Defaults()`) | ✅ ยืนยันจากโค้ดจริง (docker-compat=false แล้ว) | ตารางด้านล่าง |
| การใช้ flag pointer | ✅ dereference กระจายทั้งไฟล์ (`*mockOS`, `*dbPath`, `*httpsPort` ฯลฯ) | `main.go:51-455` |
| `-v` early return ก่อนทำงานอื่น | ✅ มี | `main.go:51-54` |
| systemd `ExecStart` | ✅ `-mock=false -db=/var/lib/pigate/pigate.db -https-port=443` | `install.sh:546` |
| การสร้าง `/var/lib/pigate` (dir + chown pigate:netdev, 775) | ✅ มีแล้ว | `install.sh:307-312` |
| ตัวอย่าง install เขียนไฟล์ config-owned-by-pigate | ✅ pattern มีอยู่ (`dhcpcd.conf`, `50-pigate.conf`) | `install.sh:317-324, 364-371` |

**ค่า default ที่ยืนยันจาก `main.go:37-49` (คือสิ่งที่ `Defaults()` ต้องคืน):**

| key (=ชื่อ flag) | ชนิด | default |
|---|---|---|
| `port` | int | `2479` |
| `db` | string | `pigate.db` |
| `mock` | bool | `true` |
| `mock-from-real` | bool | `false` |
| `disable-edit` | bool | `false` |
| `allow-edit-system-routes` | bool | `false` |
| `enable-edit-system-route` | bool | `false` |
| `prioritize-kernel-routes` | bool | `false` |
| `docker-compat` | bool | `false` ← default false แล้ว (PR #69 merged) |
| `https-port` | int | `0` |
| `tls-dir` | string | `""` |
| `allow-dev-cors` | bool | `false` |

> **ยกเว้นไม่ map เข้าไฟล์:** `-config` (ตัวชี้ path เอง) และ `-v` (version, early-return) — รวมเป็น 12 คีย์ในไฟล์

**สรุป:** งานกระจุกที่ **backend layer เดียว** — แพ็กเกจ `internal/config` ใหม่ (pure logic, เทสต์ครบ) +
refactor `main.go` ให้ใช้ `cfg.*` แทน flag pointer + แก้ `install.sh` เขียนไฟล์ production ไม่แตะ service/kernel/db/frontend/openapi

## 2. แนวทางเทคนิค

แพ็กเกจใหม่ `backend/internal/config` แยกเป็น 4 ฟังก์ชันบริสุทธิ์ (I/O ผ่าน `io.Reader`/`io.Writer` — เทสต์ได้ไม่แตะไฟล์จริง)
ยึด stdlib ล้วน (`bufio`, `strings`, `strconv`, `fmt`, `io`) — **ไม่มี dependency ใหม่** ตาม CLAUDE.md

```go
type Config struct {
    Port int; DBPath string; Mock bool; MockFromReal bool; DisableEdit bool
    AllowEditSystemRoutes bool; EnableEditSystemRoute bool; PrioritizeKernelRoutes bool
    DockerCompat bool; HTTPSPort int; TLSDir string; AllowDevCORS bool
}
func Defaults() Config                                   // 1:1 กับตารางข้างบน
func Parse(r io.Reader) (map[string]string, error)       // pure syntax: trim, ข้าม ว่าง/#, split '=' ตัวแรก
func Resolve(def Config, fileVals, explicit map[string]string) (Config, []string, error)
func Write(w io.Writer, cfg Config) error                // key=value + header comment
```

- **`Parse`** = syntax ล้วน: อ่านทีละบรรทัด, `TrimSpace`, ข้ามบรรทัดว่าง/ขึ้นต้น `#`, split ที่ `=` **ตัวแรก**
  (`strings.SplitN(line,"=",2)`), trim key+value, ไม่รองรับ quote/inline-comment/escape — คืน `map[string]string` ดิบ
  บรรทัดที่ไม่มี `=` → error (malformed line)
- **`Resolve`** = semantic + precedence: เริ่มจาก `def` → apply `fileVals` → apply `explicit` (flag) ทับ
  - แต่ละคีย์ map เข้า field ตามชนิด: bool→`strconv.ParseBool`, int→`strconv.Atoi`, string→ใช้ตรง (ค่าว่างถูกต้อง เช่น `tls-dir=`)
  - **malformed int/bool → return error (fail fast)** — ทั้งจากไฟล์และจาก flag (แต่ flag ผ่าน `flag` มาแล้วจึงถูกชนิดเสมอ)
  - **unknown key → เก็บลง `[]string` warnings แล้วข้าม** (ไม่ error) — main.go เอาไป `log.Printf`
  > **หมายเหตุออกแบบ:** เลือกคืน `([]string, error)` แทนให้ `Resolve` เรียก `log` เอง เพื่อคง purity/เทสต์ได้
  > (decision เดิมเขียน `(Config, error)` — เพิ่ม `[]string` เป็นการปรับเล็กเพื่อ testability; ถ้าไม่อยากแตะ signature
  > ให้ย้าย unknown-key detection ไป helper `KnownKeys()` แล้ว log ใน main แทน)
- **`Write`** = header comment (สื่อ "แก้ไฟล์นี้ได้; ค่าจาก flag จะชนะค่านี้") + ทุกคีย์ `key=value` เรียงคงที่
  ต้อง round-trip กับ `Parse` ได้ (Write→Parse→Resolve = ค่าเดิม)
- **`main.go` flow ใหม่** (แทน `main.go:36-66`):
  1. ลงทะเบียน flag เดิมทั้ง 13 ตัว + เพิ่ม `configPath := flag.String("config", "", "...")` → `flag.Parse()`
  2. `-v` → print version + return **ก่อน** ทำงานไฟล์ config (อย่าให้แค่เช็คเวอร์ชันไปสร้าง `/var/lib/pigate/pigate.conf`)
  3. หา path: ถ้า `*configPath != ""` ใช้เลย (ไม่มีไฟล์ → `log.Fatalf`); ไม่งั้น default `/var/lib/pigate/pigate.conf`
  4. อ่านไฟล์: มี → `Parse`; ไม่มี **ที่ default path** → เขียนไฟล์ default (`Write`, perm 0644; fail → warn ต่อ), `fileVals=nil`
  5. `explicit := map[string]string{}`; `flag.Visit(func(f){ explicit[f.Name]=f.Value.String() })` — เก็บเฉพาะที่ตั้งจริง
     (ตัด `config`/`v` ออกจาก map)
  6. `cfg, warns, err := config.Resolve(config.Defaults(), fileVals, explicit)`; err → `log.Fatalf`; warns → log ทีละอัน
  7. แทน `*port`→`cfg.Port`, `*mockOS`→`cfg.Mock`, ... ทุกจุดในไฟล์
- **Pattern แม่แบบ:** สไตล์ pure+test ล้อ `internal/model/dns_validate.go` (ฟังก์ชันบริสุทธิ์ + `_test.go` คู่กัน);
  การเขียนไฟล์ atomic ไม่จำเป็น (เขียนครั้งเดียวตอน bootstrap ไฟล์ยังไม่มี) — เขียนตรงด้วย `os.WriteFile(path, buf, 0644)`
- **ทางเลือกที่ปฏิเสธ:**
  - **flag ชนะ ทำด้วยการ apply ไฟล์ *ก่อน* `flag.Parse` ผ่าน `flag.Set`** — ปฏิเสธ เพราะแยก "flag ที่ผู้ใช้ตั้งจริง"
    ออกจาก "default" ไม่ได้หลัง `Set` (ทุกตัวกลายเป็น visited) → พังหลัก precedence; `flag.Visit` หลัง Parse ชัดกว่า
  - **ใช้ไลบรารี viper/koanf** — ปฏิเสธ ขัด CLAUDE.md (no new deps); รูปแบบ `key=value` แบนพอทำด้วย stdlib
  - **รูปแบบ TOML/YAML/JSON** — ปฏิเสธ เกินจำเป็น, ต้อง dep ใหม่ (YAML) หรือ verbose (JSON ไม่มี comment); `key=value` 1:1 กับชื่อ flag เรียนรู้ง่ายสุด

## 3. ขั้นตอน (เรียง inner-layer-first)

### Step 1 — สร้างแพ็กเกจ config (core)
**File:** `backend/internal/config/config.go` (**ไฟล์ใหม่**)
`Config` struct + `Defaults()` (ค่า 1:1 กับตาราง §1) + `Parse` + `Resolve` + `Write` + `KnownKeys()` (map ชื่อ flag → ชนิด)
ใช้ stdlib เท่านั้น จัด key ตามลำดับคงที่ (`Write` เรียงเดิมทุกครั้งเพื่อ diff ไฟล์ได้)

### Step 2 — unit test ของแพ็กเกจ (mock-safe 100%)
**File:** `backend/internal/config/config_test.go` (**ไฟล์ใหม่**)
- `TestParse`: `key=value`/คอมเมนต์ `#`/บรรทัดว่าง/`=` ใน value (`SplitN`)/`tls-dir=` ค่าว่างผ่าน/บรรทัดไม่มี `=` → error
- `TestResolve`: default ล้วน; ไฟล์ override default; flag override ไฟล์; **flag ชนะทั้งที่ไฟล์ก็ตั้ง**;
  unknown key → อยู่ใน warns ไม่ error; `mock=notabool`/`port=abc` → error (fail fast)
- `TestWriteParseRoundTrip`: `Write(Defaults())` → `Parse` → `Resolve` = `Defaults()`

### Step 3 — refactor main.go ใช้ config
**File:** `backend/cmd/pigate/main.go` (แก้ไข `main.go:36-66` + แทน pointer ทั้งไฟล์)
เพิ่ม `-config` flag, ลำดับ flow §2 (ข้อ 2-7), แทน `*xxx` → `cfg.Xxx` ทุกจุด
> **ไม่ต้องมี `InitApplyConfig()`** — config นี้อ่านครั้งเดียวตอน bootstrap ก่อนสร้าง service ทั้งหมด
> ไม่ใช่ state ใน DB ที่ต้อง apply เข้า kernel; ไม่แตะ startup apply order เดิม

### Step 4 — install.sh เขียนไฟล์ production + ปรับ ExecStart
**File:** `install.sh` (แก้ไข ต่อจาก STEP 4 ราว `:312`, และ ExecStart `:546`)
- เพิ่ม block: ถ้า `/var/lib/pigate/pigate.conf` **ยังไม่มี** → เขียนค่า production:
  ```
  # Managed by PiGate installer. แก้ไขค่าได้ที่ไฟล์นี้; ค่าจาก CLI flag จะชนะค่าในไฟล์
  mock=false
  db=/var/lib/pigate/pigate.db
  https-port=443
  docker-compat=false
  ```
  แล้ว `chown pigate:netdev` + `chmod 0644` (ล้อ `dhcpcd.conf` `:322-323`)
- **ExecStart:** ลดเหลือ `ExecStart=/usr/local/bin/pigate -config=/var/lib/pigate/pigate.conf`
  (ค่ามาจากไฟล์แทน flag ในบรรทัด — ดูข้อควรระวังเรื่อง mock ด้านล่างก่อนตัดสินใจลดจริง)

### Step 5 (optional) — เอกสาร
**File:** `docs/setup_guide.md` / README — อธิบายไฟล์ `pigate.conf`, precedence, และวิธีปรับค่าโดยไม่แก้ unit
(ไม่แตะ `docs/openapi.yaml` / `frontend/public/openapi.yaml` — ไม่มี API/endpoint ใหม่)

## 4. API ที่เกี่ยวข้อง

ไม่มี — งานนี้เป็น bootstrap/CLI ล้วน ไม่เพิ่ม/แก้ HTTP route, ไม่มี handler, ไม่แตะ role/middleware,
ไม่แตะ `-disable-edit` (ค่านี้เองกลายเป็นคีย์ในไฟล์ได้ แต่พฤติกรรม middleware ไม่เปลี่ยน) — จึง **ไม่ต้อง sync openapi**

## 5. ข้อควรระวัง (Cautions)

- **[สำคัญสุด] mock default = `true` → เสี่ยง production บูต mock ถ้าไฟล์ไม่มี:** ถ้าลด `ExecStart`
  เหลือ `-config=...` แต่ไฟล์ `pigate.conf` ยังไม่ถูกสร้าง → binary จะ **auto-write ไฟล์ default ที่มี `mock=true`**
  (code default) แล้วบูตในโหมด mock บนอุปกรณ์จริง (ไม่แตะ kernel/firewall เลย = ระบบเน็ตเวิร์กพัง).
  **ป้องกัน:** install.sh ต้องเขียน `pigate.conf` (mock=false) ใน Step 4 **ก่อน** `systemctl enable/start`
  (STEP 7 `:562-572`) เสมอ — ลำดับใน install.sh ปัจจุบัน STEP 4 (`:305`) มาก่อน STEP 7 อยู่แล้ว ✔
  ถ้ากังวลเรื่อง regression ให้ **คง `-mock=false` ไว้ใน ExecStart เป็น belt-and-suspenders** (flag ชนะไฟล์อยู่แล้ว
  จึงไม่ขัดกัน) แล้วย้ายเฉพาะค่าที่ไม่อันตรายเข้าไฟล์ — ผู้ตัดสินใจเลือกได้ตอนทำ Step 4
- **`-config=<path>` ที่ไฟล์ไม่มี ต้อง fail ชัด ไม่ auto-create:** auto-create ทำเฉพาะ **default path** เท่านั้น
  ถ้าผู้ใช้ระบุ path เองแล้วพิมพ์ผิด → `log.Fatalf("config file %q not found", path)` (แยกกรณี "ระบุเอง" กับ "default")
  ด้วยการเช็ค `*configPath != ""` ก่อน; อย่าใช้ `os.IsNotExist` แล้วเขียนไฟล์ในทั้งสองกรณี
- **`flag.Visit` ต้องเรียกหลัง `flag.Parse` และเก็บเฉพาะที่ตั้งจริง:** ถ้าเผลอใช้ `flag.VisitAll` จะได้ทุก flag
  (รวม default) → ทุกอย่างกลายเป็น "explicit" → ไฟล์ config ไม่มีผลเลย พังหลัก precedence ทั้งหมด
- **`-v` ต้อง return ก่อนแตะไฟล์ config:** ไม่งั้นแค่รัน `pigate -v` บนเครื่อง prod ก็จะไปสร้าง/เขียน
  `/var/lib/pigate/pigate.conf` โดยไม่ตั้งใจ (side effect ที่ไม่ควรมี)
- **เขียนไฟล์ default ไม่สำเร็จ = warn ต่อ ไม่ fail:** บน dev workstation ไม่มี `/var/lib/pigate/` → เขียนล้มเหลว
  ปกติ; ต้อง `log.Printf(warn)` แล้วรันด้วย code default ต่อ (dev ใช้ `-mock=true` เป็น default อยู่แล้ว สะดวก)
  — ห้าม `log.Fatalf` ตรงนี้
- **malformed int/bool ในไฟล์ = fail fast (Fatalf):** ต่างจาก unknown key (warn+ข้าม) — ค่าที่ parse ไม่ได้
  แปลว่าผู้ใช้ตั้งใจตั้งแต่พิมพ์ผิด ปล่อยผ่านด้วย default เงียบ ๆ จะ debug ยากกว่า; ให้ตายพร้อม error ที่ระบุ key+value
- **migration ของอุปกรณ์ที่ติดตั้งแล้ว:** เครื่องเดิมที่ upgrade ด้วย install.sh — ถ้า unit ใหม่ ExecStart เปลี่ยน
  ต้องมั่นใจว่า `pigate.conf` ถูกเขียน (idempotent create-if-missing) **ก่อน** `systemctl start`; เครื่องที่เคยแก้
  `ExecStart` เองไว้จะถูก install.sh เขียนทับ unit — ระบุใน release note ว่า "ตั้งค่าเพิ่มเติมย้ายไป `pigate.conf`"
- **ไม่แตะ backup/restore, netlink monitor, startup apply order, migration DB:** config นี้ไม่ได้อยู่ใน SQLite
  (เป็น bootstrap param) → schema เดิมไม่เปลี่ยน, `service/backup.go` ไม่ต้องรวม, `netlink_monitor.go` ไม่เกี่ยว
- **ไม่มี SD-card write เพิ่มระหว่างรัน:** ไฟล์ config เขียน **ครั้งเดียว** ตอน bootstrap เมื่อไฟล์ยังไม่มีเท่านั้น
  ไม่ใช่ write ซ้ำ ๆ ระหว่างทำงาน → สอดคล้องนโยบายถนอม SD card
- **ทดสอบทั้งหมด mock-safe:** `Parse/Resolve/Write` เป็น pure + integration ชี้ default path ไป temp dir
  (`t.TempDir()`) → ไม่ต้องมีบอร์ดจริง ไม่เสี่ยงล็อกตัวเอง

## 6. Checklist (Definition of Done)

- [ ] `backend/internal/config/config.go` — `Config`/`Defaults`/`Parse`/`Resolve`/`Write`/`KnownKeys` (stdlib ล้วน)
- [ ] `backend/internal/config/config_test.go` — Parse/Resolve/Write round-trip + เคส unknown-key/malformed/precedence
- [ ] `backend/cmd/pigate/main.go` — เพิ่ม `-config`, flow bootstrap (path/read/write/Visit/Resolve), แทน `*ptr`→`cfg.*` ทุกจุด, `-v` return ก่อนแตะไฟล์
- [ ] `install.sh` — เขียน `/var/lib/pigate/pigate.conf` (mock=false,db,https-port=443,docker-compat=false, 0644, chown pigate:netdev) create-if-missing; ปรับ/พิจารณา ExecStart
- [ ] (optional) `docs/setup_guide.md`/README — อธิบายไฟล์ config + precedence
- [ ] `cd backend && go build ./... && go vet ./... && go test ./...` เขียว
- [ ] ทดสอบ mock: รันเปล่าใน temp dir → เขียน `pigate.conf` + บูต mock=true; แก้ไฟล์ `mock=false` แล้วรัน → ค่าเปลี่ยน; `-mock=true` ทับไฟล์ที่ตั้ง false → mock=true (flag ชนะ)
- [ ] ทดสอบ error: `-config=/does/not/exist` → fail ชัด; ไฟล์มี `port=abc` → fail fast; ไฟล์มี `unknownkey=1` → warn+รันต่อ
- [ ] ไม่แตะ `docs/openapi.yaml` / `frontend/public/openapi.yaml` (ไม่มี API ใหม่)
- [ ] แยก branch `feat/config-file-loader` → PR เข้า main (code change ห้าม push ตรง)
