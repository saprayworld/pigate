# Wi-Fi Scan ไม่คืนเครือข่ายบน on-board wlan0 — ทำให้ scan complete จริงบน cold cache

> Work plan สำหรับ **bug fix**: กด Scan บน on-board `wlan0` ของ RPi5 แล้ว UI ไม่ขึ้นเครือข่ายเลย
> ทั้งที่ interface up อยู่ ในขณะที่ USB dongle สแกนได้ปกติ และเครื่องมือภายนอก (`wavemon`)
> ก็สแกนเจอ
>
> เขียนเมื่อ: 2026-07-22 · แก้ไข: 2026-07-22 (rev.2 หลังได้ repro ใหม่)
> Reference branch: `main` (งานจริงทำบน `fix/wifi-scan-reliability`)
> README Feature Status: Interfaces = Completed (นี่คือ bug fix ไม่เปลี่ยน scope)

## 0. Root Cause (rev.2 — ยืนยันจาก source ของ library แล้ว)

### 0.0 หลักฐาน repro ใหม่จากผู้ใช้ (RPi5, on-board wlan0, hardware จริง)

1. toggle interface **UP** ใน PiGate → กด Scan → **ว่างเปล่า**
2. รัน `wavemon` (external) → เห็นเครือข่ายครบ
3. กลับมากด Scan ใน PiGate → **คราวนี้เห็นเครือข่าย**
4. toggle **OFF แล้ว ON** ใหม่ → กด Scan → **ว่างอีก** (พังเหมือนเดิม)

สรุปจาก repro: อาการ **ไม่ใช่แค่ "interface down"** — แม้ up แล้ว scan แรกบน BSS cache ที่เย็น
(cold) ก็คืนว่าง. มันจะได้ผลก็ต่อเมื่อมี agent อื่น (wavemon) ไป **warm kernel BSS cache**
มาก่อน; พอ toggle ใหม่ cache ถูก flush → พังอีก. แปลว่า `ScanWifi` ของ PiGate จริงๆ
**พึ่ง cache ที่อุ่นอยู่แล้ว** ไม่ได้ทำ+รอ scan ของตัวเองให้เสร็จอย่างเชื่อถือได้

### 0.1 กลไกที่ยืนยันแล้ว (อ้าง `github.com/mdlayher/wifi@v0.8.0`)

version pin: `backend/go.mod` → `github.com/mdlayher/wifi v0.8.0` (ตรวจ source ที่
`$(go env GOMODCACHE)/github.com/mdlayher/wifi@v0.8.0/client_linux.go`)

**ข้อเท็จจริง A — `AccessPoints` = dump cache เฉยๆ (ยืนยัน):**
`AccessPoints(ifi)` (client_linux.go:193-206) เรียก `c.get(NL80211_CMD_GET_SCAN, netlink.Dump, …)`
→ `parseGetScanResult` (client_linux.go:546). **มันไม่ trigger อะไร** แค่ dump รายการ BSS
ที่ kernel cache อยู่ ณ ตอนนั้น → ถ้า cache เย็นก็คืน `[]`, ถ้าอุ่น (wavemon เพิ่งสแกน) ก็คืนครบ
**นี่คือสาเหตุตรงๆ ที่ wavemon ทำให้ PiGate "ใช้ได้"**

**ข้อเท็จจริง B — `Scan` คืน `EBUSY` เร็วเมื่อมี scan อื่นวิ่งอยู่ (ยืนยัน):**
docstring ของ `Scan` (client_linux.go:267-268) เขียนชัด: *"If a scan is already in progress,
this function will return a syscall.EBUSY error."* กลไก: `Scan` (261-358) join multicast group
**ก่อน** ส่ง TRIGGER_SCAN (297-307 ก่อน 350 → **ไม่มี join-after-trigger race**) แล้วส่ง req
ด้วย `Request|Acknowledge` (348-350) และรอ `listenNewScanResults` (499). ถ้า kernel ปฏิเสธ
ด้วย EBUSY มันมาเป็น error ACK → `conn.Receive()` คืน err (501-503) → `Scan` คืน EBUSY
**ทันที (เร็ว ไม่ค้าง)** → อธิบายว่าทำไมผู้ใช้เห็น "ว่างเร็ว" ไม่ใช่ค้าง

**ข้อเท็จจริง C — library block ถูกต้องเมื่อสำเร็จ:** เมื่อ TRIGGER สำเร็จ `listenNewScanResults`
รอ `NL80211_CMD_NEW_SCAN_RESULTS` ของ ifiIndex ที่ถูกต้อง (519-535) จริง — **ปัญหาจึงอยู่ที่
วิธีที่ PiGate เรียกใช้ ไม่ใช่ race ใน library**

**ข้อเท็จจริง D — trigger ชนกับ wpa_supplicant ของ PiGate เอง:** ตอน toggle interface **UP**
`RealNetwork.ToggleInterface` (real_network.go:56-84) **start `wpa_supplicant@wlanN.service`**
ผ่าน D-Bus. wpa_supplicant พอ start แล้วจะ **สแกนต่อเนื่อง** เพื่อหา network → ตอนผู้ใช้กด Scan
ใน PiGate มันไปชนกับ scan ของ wpa → `c.Scan` คืน EBUSY. toggle off/on = restart wpa (สแกนใหม่)
+ flush cache → ตรงกับ repro ข้อ 1 และ 4 เป๊ะ. wavemon (persistent) ยิงจนสำเร็จหนึ่งรอบ
→ warm cache → PiGate `AccessPoints` dump ได้ → ตรงกับ repro ข้อ 2 และ 3

**ข้อเท็จจริง E — โค้ด PiGate ทำ single-shot ไม่มี retry/timeout:**
`RealNetwork.ScanWifi` (real_network.go:401-495) เรียก `c.Scan` **ครั้งเดียว** แล้วอ่าน
`c.AccessPoints` **ทันทีครั้งเดียว** ด้วย `context.TODO()` (real_network.go:425 → ไม่มี deadline)
**ไม่มี** poll/retry และ **ไม่ทน** EBUSY → cold/busy cache = ว่าง; ได้ผลเฉพาะตอน cache อุ่นเอง

### 0.2 สรุป root cause (reframe)

> PiGate ปฏิบัติกับ `AccessPoints` (ซึ่งเป็นแค่ **cache dump**) เหมือนว่ามันคืนผล scan สดๆ และทำ
> **trigger-แล้วอ่านทันทีครั้งเดียว โดยไม่รอ ไม่ retry และไม่ทน EBUSY** (ซึ่งเกิดบ่อยมากเพราะ
> wpa_supplicant ที่ PiGate start ตอน up สแกนอยู่). บน cache ที่เย็นหรือกำลัง busy จึงคืนว่าง/error
> — ใช้ได้เฉพาะตอนมี agent อื่นเพิ่ง warm kernel BSS cache ไว้

ความเชื่อมั่น: **สูง** ต่อข้อเท็จจริง A/B/C/E (อ่านจาก source โดยตรง). ข้อ D (EBUSY จาก
wpa_supplicant) เป็นสาเหตุ "ว่างเร็ว" ที่น่าจะเป็นที่สุดตาม docstring + ลำดับ start service —
แต่ **fix เดียวกันครอบคลุมทุก micro-mechanism** (EBUSY, cache ยังไม่ commit, scan ใช้เวลานาน
บน iface ที่ยังไม่ associate)

## 0.3 สิ่งที่ **จงใจไม่ทำ** (ต้องให้เจ้าของตัดสิน)

- **ไม่ auto bring-up interface เพื่อสแกน** — โครงการมีหลักการ "ห้ามเปิด interface เป็น side effect"
  (real_network.go:160-170 "Save-silently-enables bug"). กรณี down ยังคง guard + แจ้ง error (T-01)
- **ไม่ยิง TRIGGER_SCAN ซ้ำๆ ใน loop** — จะไปกวน wpa_supplicant ตอน associate และทำให้ EBUSY
  หนักขึ้น. ออกแบบให้ trigger ครั้งเดียวแล้ว poll เฉพาะ cache (GET_SCAN อ่านอย่างเดียว ปลอดภัย)

## 1. Technical Approach (rev.2)

**หัวใจใหม่:** เขียน `ScanWifi` ให้เป็น **trigger-once-then-poll** ที่ทน EBUSY และมี timeout —
ไม่พึ่ง cache อุ่นจากภายนอกอีก. คงงาน down-guard (T-01) และ frontend-surfacing (T-03) ไว้

กลไก `ScanWifi` ใหม่:
1. `ctx, cancel := context.WithTimeout(context.Background(), ~10s)` แทน `context.TODO()`
2. `c.Scan(ctx, ifi)` **หนึ่งครั้ง**:
   - `syscall.EBUSY` → **ไม่ fatal** (มี scan อื่น เช่น wpa_supplicant วิ่งอยู่แล้ว) → ข้ามไป poll
   - `syscall.ENETDOWN` → map เป็น "interface is down; bring it up before scanning" (defense-in-depth คู่ T-01)
   - `syscall.ERFKILL`/มีคำว่า "rf-kill" → map เป็น "radio blocked by rfkill"
   - `nil` → สำเร็จ ผลควรสด
   - error อื่น → เก็บไว้ (best-effort ไป poll ต่อ; คืน error นั้นหากสุดท้ายว่าง)
3. **Poll** `c.AccessPoints(ifi)` เป็นช่วงสั้นๆ (เช่นทุก ~1s จนถึง deadline) จนกว่าจะได้ผล
   non-empty แล้วคืน — เปิดโอกาสให้ scan ที่กำลังวิ่ง (ของเราหรือ wpa) เติม cache เสร็จ
4. หมดเวลายังว่าง → คืน `[]` (ไม่ใช่ error — อาจไม่มี AP จริงๆ; กรณี down แยกไว้ที่ T-01 แล้ว)
5. **ยิง TRIGGER_SCAN ไม่เกินหนึ่งครั้ง**; loop มีแต่ GET_SCAN (read-only) เท่านั้น

ไม่แตะ: interface state จริง, netlink_monitor, wpa_supplicant lifecycle, DB, และ **mock** —
interface `ScanWifi` signature ไม่เปลี่ยน → ไม่ต้องแก้ `kernel/mock.go` (mock คืน static list เดิม)

## 2. Tasks

```json
[
  {
    "task_id": "T-01",
    "title": "Guard สแกนเฉพาะ interface ที่ up — คืน 409 ที่ actionable",
    "layer": "api",
    "files": ["backend/internal/api/handlers.go"],
    "instruction": "ใน HandleScanWifi (~handlers.go:883) หลังเช็ค iface.Type != \"wireless\" และก่อนเรียก s.network.ScanWifi ให้เพิ่ม guard read-only: ถ้า iface.Status != \"up\" ให้ s.writeError(w, http.StatusConflict, \"Interface must be brought up before scanning for Wi-Fi networks.\") แล้ว return. ห้ามเปลี่ยน iface.Status หรือเรียกฟังก์ชันที่ bring-up interface ใดๆ. อย่าแตะ rejectIfOffline. ใช้ 409 ให้ตรง pattern rejectIfOffline. sensitive เล็กน้อย: เปลี่ยน response contract — ตรวจว่าข้อความไม่รั่วข้อมูลภายในและ interface ที่ up ยังทำงานปกติ.",
    "acceptance": ["go build ผ่าน", "scan บน interface ที่ status=down/offline ได้ 409 body {\"message\":...} ไม่ถึง kernel", "status=up ยังเรียก ScanWifi ตามเดิม"],
    "depends_on": []
  },
  {
    "task_id": "T-02",
    "title": "[CORE] เขียน ScanWifi ใหม่: trigger-once + poll cache + timeout + ทน EBUSY",
    "layer": "kernel",
    "files": ["backend/internal/kernel/real_network.go"],
    "instruction": "เขียน RealNetwork.ScanWifi (real_network.go:401-495) ใหม่ให้ scan เสร็จจริงบน cold cache แทนการพึ่ง cache อุ่นจากภายนอก. ขั้นตอน: (1) แทน ctx := context.TODO() ด้วย ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second); defer cancel(). (2) เรียก c.Scan(ctx, ifi) หนึ่งครั้ง แล้วแยกจัดการ error: ถ้า errors.Is(err, syscall.EBUSY) → log แล้ว *ไปต่อ* (ถือว่ามี scan อื่น เช่น wpa_supplicant วิ่งอยู่); ถ้า errors.Is(err, syscall.ENETDOWN) → return error 'interface %s is down; bring it up before scanning'; ถ้า errors.Is(err, syscall.ERFKILL) หรือ strings.Contains(strings.ToLower(err.Error()), \"rf-kill\") → return 'radio for %s is blocked by rfkill'; ถ้า err อื่นที่ไม่ใช่ nil → log แล้ว *ไปต่อ* poll (best-effort) แต่เก็บ err ไว้คืนหากสุดท้ายว่าง; ถ้า nil → ไปต่อ. (3) POLL: loop อ่าน c.AccessPoints(ifi) ทุก ~1s จนกว่าจะ len(bssList) > 0 (นับเฉพาะ SSID ไม่ว่าง) หรือ ctx หมดเวลา; ใช้ ticker/select กับ ctx.Done() — อย่า busy-loop. คืนทันทีที่ได้ผล. (4) map ผล BSS→WifiScanResult ด้วย logic เดิม (signal/channel/band/RSN) — ห้ามเปลี่ยน. (5) หมดเวลายังว่าง: ถ้าเคยเจอ ENETDOWN/ERFKILL คืน error นั้น, ไม่งั้นคืน ([]model.WifiScanResult{}, nil). ข้อบังคับ: ยิง c.Scan/TRIGGER_SCAN ไม่เกินหนึ่งครั้ง (loop มีแต่ AccessPoints/GET_SCAN read-only) เพื่อไม่กวน wpa_supplicant associate. เพิ่ม log.Printf ที่จุด trigger, EBUSY, timeout-empty. ไม่เปลี่ยน signature interface (ไม่แตะ mock.go). imports: เช็ค/เพิ่ม syscall, time, errors. **SENSITIVE: Netlink + timeout + interaction กับ wpa_supplicant — ต้อง review ว่า (ก) ทุก wait bounded ด้วย ctx ไม่มีทางค้างถาวร, (ข) ไม่ re-trigger ซ้ำ, (ค) error mapping ไม่กลืน error ประเภทอื่นเงียบๆ, (ง) 10s ไม่สั้นเกินสำหรับ active scan บน iface ที่ยังไม่ associate.**",
    "acceptance": ["go build + go vet ผ่าน", "อ่านโค้ดแล้วยืนยันว่า TRIGGER_SCAN ยิงครั้งเดียว, loop เป็น GET_SCAN, ทุก path bounded ด้วย ctx timeout", "EBUSY ไม่ทำให้ล้ม (ไป poll ต่อ)", "ENETDOWN/ERFKILL คืนข้อความอ่านออกและมี log"],
    "depends_on": []
  },
  {
    "task_id": "T-03",
    "title": "Frontend: อ่านข้อความจริงจาก body + แยก empty-ok จาก error",
    "layer": "frontend",
    "files": ["frontend/src/services/interfaceService.ts", "frontend/src/pages/Interfaces.tsx"],
    "instruction": "(1) interfaceService.ts scanWifi (~บรรทัด 225): เมื่อ !response.ok ให้ลอง await response.json() อ่าน field message มา throw (fallback statusText/ข้อความ default ถ้า parse ไม่ได้) — ปัจจุบันใช้ response.statusText อย่างเดียวซึ่งว่างบน HTTP/2 ทำให้ผู้ใช้เห็นสาเหตุกำกวม. (2) Interfaces.tsx handleWifiScan (~บรรทัด 722): เก็บ error ลง state ใหม่ (scanError) แล้วในกล่องผลลัพธ์ (showScanResults ~บรรทัด 1786) แสดงเป็น 3 กรณี: กำลังสแกน / มี scanError → แสดงข้อความสาเหตุ (text-destructive) / สแกนเสร็จแต่ scanResults ว่าง → empty-state 'ไม่พบเครือข่าย Wi-Fi' (text-muted-foreground). เคลียร์ scanError ทุกครั้งที่เริ่ม scan ใหม่. ตาม rules_of_work.md: ห้าม hardcode สีดิบ, flat ไม่มี shadow. ไม่กระทบ IS_MOCK_MODE branch.",
    "acceptance": ["yarn build + yarn lint ผ่าน", "409/500 พร้อม {message} → กล่องแสดงข้อความจาก body ไม่ใช่ 'Internal Server Error'/ค่าว่าง", "200 พร้อม [] → แสดง 'ไม่พบเครือข่าย' (empty-ok) ไม่ใช่ error", "mock mode สแกนคืนผลปกติ"],
    "depends_on": ["T-01", "T-02"]
  }
]
```

## 3. Final Acceptance (ทดสอบรวมครั้งเดียวหลังทำครบทุก Task)

```json
{
  "final_acceptance": [
    "บน RPi5 จริง (non-mock): toggle on-board wlan0 UP → กด Scan ครั้งเดียวบน COLD cache (ไม่ต้องรัน wavemon ก่อน) → เห็นเครือข่าย (นี่คือ repro หลักที่ต้องหาย)",
    "toggle wlan0 OFF แล้ว ON ใหม่ (flush cache) → กด Scan → ยังเห็นเครือข่าย (ไม่ regress กลับไปว่าง)",
    "กด Scan บน USB dongle ที่ใช้งานอยู่ → ยังเห็นเครือข่ายครบเหมือนเดิม",
    "กด Scan บน wlan0 ที่ยัง down → ได้ข้อความชัดเจนว่าต้องเปิด interface ก่อน (409) ไม่ใช่กล่องว่างเงียบ",
    "อยู่ในที่ไม่มี AP จริง → กด Scan → แสดง 'ไม่พบเครือข่าย' (empty-ok) ไม่ใช่ error กำกวม",
    "scan ใช้เวลาไม่เกิน ~10s แล้วคืนเสมอ ไม่ค้าง goroutine (ยืนยัน timeout ทำงาน)",
    "การกด Scan ระหว่าง wpa_supplicant กำลัง associate ไม่ทำให้ connection ที่กำลังต่อหลุด (ยืนยันว่าไม่ re-trigger รัวๆ)",
    "go test ./... และ yarn build/lint ผ่านทั้งหมด"
  ]
}
```

## 4. Sensitive / Review Points

- **T-02 = จุดเสี่ยงหลัก (Netlink + timeout + wpa_supplicant)**: review ว่า (1) ยิง TRIGGER_SCAN
  ครั้งเดียว, loop เป็น GET_SCAN read-only, (2) ทุก wait ผูกกับ ctx timeout — ไม่มี path ค้างถาวร
  (แก้ latent bug ของ context.TODO() ด้วย), (3) EBUSY ถือเป็น non-fatal ถูกต้อง, (4) error
  mapping (errors.Is) ไม่กลืน error อื่นเงียบๆ, (5) 10s พอสำหรับ active scan บน iface unassociated
- **การกด Scan ไม่ควรรบกวนการ associate ของ wpa_supplicant** — เหตุผลที่ห้าม re-trigger
- **T-01 เปลี่ยน response contract** (500→409 ตอน down) — ตรวจว่าไม่มี client อื่นพึ่ง 500
- **ทั้งหมดไม่แตะ interface state จริง / netlink_monitor / wpa_supplicant lifecycle / mock** โดยเจตนา
- version library pin: `mdlayher/wifi v0.8.0` — พฤติกรรมที่วิเคราะห์ผูกกับเวอร์ชันนี้; ถ้า bump
  version ต้องทวนใหม่

## 5. Follow-up (เจ้าของตัดสิน — นอก scope แผนนี้)

- ปุ่ม **"Bring up & Scan"** แบบ explicit consent (bring-up ชั่วคราว → scan → คืนสถานะเดิม)
  สำหรับ UX "สแกนก่อนเปิด" — sensitive (ต้องออกแบบร่วมกับ netlink_monitor), เปิดแผนแยกเมื่อสั่ง
- ถ้าอนาคตต้องการ real-time scan progress ผูกกับ wifi-status stream ที่มีอยู่ (`wifi-status-stream-plan.md`)
