# Wi-Fi Scan "trigger สำเร็จแต่ผลว่าง" บน on-board wlan0 — rev.5

> Work plan (bug fix) สำหรับ GitHub issue #88: กด Scan บน on-board `wlan0` แล้วไม่เจอเครือข่าย
>
> เขียนเมื่อ: 2026-07-23 (rev.5 — หลังได้ hardware journal ชี้ขาดจาก T-07 + owner ยืนยัน design)
> สืบเนื่องจาก: `docs/ref/todo/wifi-scan-down-interface-plan.md` (rev.2, **merge แล้ว** — T-01..T-04)
> Reference branch: `fix/wifi-scan-reliability` · README Feature Status: Interfaces = Completed (bug fix ไม่เปลี่ยน scope)

---

## 0. บัญชีสมมติฐานที่ตกไปแล้ว (Ruled-out ledger — เก็บไว้ตามธรรมเนียมเอกสาร)

| rev | สมมติฐาน | สถานะ | หลักฐานที่หักล้าง |
|-----|----------|-------|-------------------|
| rev.2 | single trigger + ไม่ทน EBUSY + ไม่ poll → cold/busy cache คืนว่าง | **แก้แล้วบางส่วน** (T-01..T-04 merge) แต่ **ไม่พอ** | journal ใหม่: trigger สำเร็จ (c.Scan=nil) ไม่มี EBUSY แต่ยังว่าง |
| rev.3 | `c.Scan=nil` เพราะไปคว้า NEW_SCAN_RESULTS **ของ wpa_supplicant** ที่สแกนชนกัน | **ตกไป** | `wpa_supplicant@wlan0` = inactive(dead) → ไม่มีใครสแกนชน |
| rev.4 H-A | regdomain ไม่ถูกตั้ง (world "00") → active scan ถูก no-IR จำกัด | **ตกไป (ชี้ขาด)** | T-07 log: `regdomain at scan time: TH` **ทั้งเคสที่พังและเคสที่ได้** — regdomain ถูกต้องตั้งแต่แรก |
| rev.4 H-B | AccessPoints คืน BSS จริงแต่ `mapWifiScanResults` ทิ้งหมด (parse SSID พัง) | **ตกไป** | T-07 log เคสพัง = `0 BSS, 0 usable` (cache ว่างดิบ ไม่ใช่ parse ทิ้ง); เคสได้ = `11 BSS, 9 usable` (parse ทำงานถูก) |

> **T-07 (diagnostic-only) ทำเสร็จ + QA ผ่าน + เก็บ journal จริงแล้ว** — เป็น task ที่ยังคุณค่า:
> มันตัด H-A/H-B ออกอย่างชี้ขาดและชี้ทางไปยัง root cause จริงใน rev.5

---

## 1. หลักฐานชี้ขาดจาก T-07 (3 การทดสอบบน RPi5 จริง)

**Log 1 — wlan0, ไม่เคยมีไฟล์ wpa config, wpa ไม่รัน → สแกนพัง:**
```
regdomain at scan time: TH
triggering scan on wlan0
scan trigger on wlan0 returned: <nil>
poll 0-5: AccessPoints returned 0 BSS, 0 usable (non-hidden)
wlan0 scan timed out with no networks found
```

**Log 2 — wlan0 เดิม แต่ผู้ใช้เขียน wpa config (SSID ปลอม "12366998" ที่ไม่ match ของจริง) แล้วเปิด interface → wpa_supplicant รัน+attach แต่ไม่ associate → สแกนได้:**
```
regdomain at scan time: TH
triggering scan on wlan0
scan trigger on wlan0 returned: <nil>
poll 0: AccessPoints returned 11 BSS, 9 usable (non-hidden)
wlan0 found 9 network(s) after 0 poll(s)
```

**Log 3 — USB dongle (`wlx0cef1548ff2b`) เทียบ, ได้เสมอไม่ว่ามี wpa หรือไม่:**
```
regdomain at scan time: TH
poll 0: AccessPoints returned 44 BSS, 32 usable (non-hidden)
```

**ตัวแปรเดียวที่ต่างระหว่าง Log 1 (พัง) กับ Log 2 (ได้)** บน interface เดียวกัน โค้ด scan เดียวกัน
regdomain เดียวกัน คือ: **มี `wpa_supplicant` attach อยู่กับ wlan0 หรือไม่**. SSID ในคอนฟิกเป็นของปลอม
ที่ไม่มีจริงในละแวก → **ไม่ได้เกี่ยวกับการ associate สำเร็จ แค่ "wpa attach อยู่" ก็พอ**

---

## 2. Root Cause (rev.5)

> **บนชิป Wi-Fi on-board ของบอร์ด (แทบแน่ใจว่าเป็น Broadcom/Cypress `brcmfmac` over SDIO บน RPi5 —
> FullMAC), kernel/firmware จะเริ่มคืนผล scan ผ่าน nl80211 (`GET_SCAN` dump ของ PiGate) ก็ต่อเมื่อมี
> `wpa_supplicant` attach อยู่กับ interface นั้นเท่านั้น — โดยไม่ต้อง associate สำเร็จ และคอนฟิกจะเป็น
> SSID ปลอมก็ได้. ถ้าไม่มี wpa attach, PiGate ยิง `TRIGGER_SCAN` แล้ว scan "จบสมบูรณ์" (NEW_SCAN_RESULTS
> ยิง, `c.Scan` คืน nil) แต่ BSS cache ว่างดิบ (0 BSS). ชิป USB dongle (SoftMAC, คนละ driver) ไม่มี
> ข้อจำกัดนี้ — สแกนได้เองเสมอ.**

### 2.1 ทำไม wlan0 ถึงไม่มี wpa attach (สาย causal ที่ยืนยันจากโค้ด)

1. ผู้ใช้ยังไม่เคย "Save network" บน wlan0 → **ไม่มีไฟล์** `/etc/wpa_supplicant/wpa_supplicant-wlan0.conf`
   (`ConfigureWifi` เป็นที่เดียวที่เขียนไฟล์นี้ — `real_network.go:129`)
2. ตอน toggle UP, `ToggleInterface` เรียก `StartServiceViaDBus("wpa_supplicant@wlan0.service")`
   (`real_network.go:86`) — แต่ template `wpa_supplicant@.service` มาตรฐานต้องมี
   `-c /etc/wpa_supplicant/wpa_supplicant-wlan0.conf`; **ไม่มีไฟล์ = wpa exit ทันที = service ล้ม**
3. error ของ start ถูก **กลืนเงียบ** (`_ = StartServiceViaDBus(...)`, `real_network.go:86`) → ไม่มีใครรู้
4. ผลลัพธ์: interface UP อยู่ แต่ **ไม่มี wpa attach** → ชิป brcmfmac ไม่คืนผล scan → Log 1

> สรุป: บั๊กเกิดจาก **ช่องว่างของ lifecycle** — PiGate ผูกการ start wpa ไว้กับ "ผู้ใช้ save network แล้ว"
> (มีไฟล์คอนฟิก) เท่านั้น. wireless interface ที่ UP แต่ยังไม่ save network เลยจึงไม่มี wpa attach และ
> **สแกนไม่ได้บนชิป on-board** (แต่สแกนได้บน USB dongle — ปิดบังบั๊กนี้มานาน)

### 2.2 กลไกระดับ nl80211 (ความเชื่อมั่น: กลาง — บอกตรง ๆ ว่ายืนยัน primitive เป๊ะ ๆ ไม่ได้)

**ยืนยันเชิงประจักษ์ (สูง):** "wpa attach → สแกนได้; ไม่ attach → สแกนไม่ได้" — พิสูจน์ตรงจาก Log 1 vs 2

**ยืนยัน primitive ที่แน่ชัด (ทำไม่ได้จาก source ที่มีในมือ):** ผม**ไม่มี** source ของ brcmfmac driver
หรือ `driver_nl80211` ของ wpa_supplicant ในโปรเจกต์นี้ให้อ้าง และเอกสาร PiGate
(`wifi_wpa_working_instruction.md`, `tech_stack_design.md`) **ไม่ได้บันทึก quirk นี้ไว้เลย**. ผู้สมัคร
ที่เป็นไปได้ (จากความรู้ทั่วไปเรื่อง brcmfmac บน RPi — **ระบุระดับความเชื่อมั่น: กลาง/ต่ำ, ยังไม่ยืนยัน**):
- `wpa_supplicant` ตอน driver init ยิง **`NL80211_CMD_SET_INTERFACE`** ตั้ง iftype = STATION อย่างชัดเจน
  → บน brcmfmac สั่ง firmware reconfigure vif ให้อยู่สถานะพร้อม escan (น่าจะเป็นตัวหลักที่สุด)
- **`NL80211_CMD_REGISTER_FRAME`** (ลงทะเบียนรับ probe-resp/mgmt frame) — FullMAC firmware บางตัว
  ต้องมีสิ่งนี้ก่อนจึงจะ surface ผล scan นอกบริบท association
- ปิด power-save / ปลุกชิปจาก deep-sleep, หรือ vendor iovar เฉพาะ brcmfmac ที่มีแต่ driver backend รู้

> เพราะ **ไม่รู้ primitive ที่แน่ชัด** การพยายามให้ PiGate "เลียนแบบสิ่งที่ wpa ทำ" ผ่าน nl80211 ตรง ๆ
> (SET_INTERFACE/REGISTER_FRAME เอง) จึงเป็น **การเดาที่มีความเสี่ยงสูง** และ library ที่ pin อยู่
> (`mdlayher/wifi`) เป็น read-mostly ไม่ expose การตั้ง iftype/register-frame. → แนวทางที่ปลอดภัยกว่า
> คือ **ใช้ wpa_supplicant ทำหน้าที่นั้น** (ซึ่งพิสูจน์แล้วว่าได้) แทนการ reimplement เอง

---

## 3. ประเมินไอเดีย "throwaway config" ของผู้ใช้ vs ทางเลือกที่สะอาดกว่า

ไอเดียผู้ใช้: ก่อนสแกน interface on-board ที่ยังไม่มีคอนฟิก → เขียนคอนฟิก **ชั่วคราว** ที่มี SSID ปลอม
→ start wpa แค่พอสแกน → **ลบ/คืนคอนฟิกเดิม** หลังเสร็จ. ประเมินตาม 4 ข้อกังวลของ coordinator:

1. **กลไกยังไม่เข้าใจถ่องแท้** — จริง (ดู §2.2). แต่ข้อเท็จจริงเชิงประจักษ์ "wpa attach = สแกนได้"
   หนักแน่นพอจะต่อยอด **ถ้า** ทำให้เงื่อนไข deterministic (รอ wpa ถึงสถานะพร้อมก่อนสแกน) ไม่ใช่ "start แล้วหวัง"
2. **เสี่ยงทับคอนฟิกจริง** — **นี่คือจุดตายของไอเดีย throwaway**: การเขียน-แล้ว-ลบ/คืน รอบคอนฟิกจริง
   เสี่ยงข้อมูลหายถ้า crash/ไฟดับ/แข่งกัน (ตรงข้ามหลัก atomic write ใน WI §4.3). **หลีกเลี่ยงได้ทั้งหมด**
   ถ้าไม่ต้องลบ/คืนอะไรเลย
3. **ขัดหลักออกแบบ** (rev.2 §0.3 + CLAUDE.md: wpa เป็นเจ้าของ lifecycle, PiGate ห้ามสร้าง side effect
   รอบ wpa แค่เพื่อให้ scan ได้) — การ **start/stop wpa ทุกครั้งที่สแกน** เป็น side effect ต่อ scan ชัด ๆ
   → ผิดหลัก
4. **ไม่ deterministic** — ไอเดีย throwaway ต้องรอ wpa boot ทุกครั้ง เพิ่ม latency + จังหวะไม่แน่นอน

### 3.1 ทางเลือกที่ owner อนุมัติ — "minimal always-present config" (มี placeholder network block)

> **กรอบคิดของ owner (ยึดตามนี้):** *wpa_supplicant คือ process ที่ทำหน้าที่ attach กับชิปอยู่แล้ว —
> T-10 แค่ทำให้มัน **start ได้เสมอ** (โดยมีไฟล์ config ให้มันเจอ) แทนที่จะ start ก็ต่อเมื่อผู้ใช้ save
> network จริงเท่านั้น.* ปิดช่องว่าง lifecycle ที่ต้นเหตุ (§2.1): ทำให้ **ทุก wireless interface ที่ UP
> มีไฟล์ wpa config เสมอ** เพื่อ wpa start+attach ได้บน UP โดยไม่ต้องรอผู้ใช้ save network — แล้ว
> "wireless UP ⟹ wpa attached ⟹ สแกนได้" เป็น invariant

ทำไมดีกว่า throwaway ทุกข้อ:
- **ไม่มีการลบ/คืนคอนฟิก** → ตัดความเสี่ยงทับข้อมูล (ข้อ 2) ทิ้งทั้งหมด. เขียน **เฉพาะเมื่อไฟล์ยังไม่มี**
  (never overwrite) → คอนฟิกจริงของผู้ใช้ปลอดภัย 100%
- **ไม่ start/stop wpa ต่อการสแกน** → wpa attach ครั้งเดียวตอน UP (ยั่งยืน) ไม่ใช่ side effect ต่อ scan
  → สอดคล้องหลัก "wpa เป็นเจ้าของ interface" **มากกว่า** สภาพปัจจุบัน (ข้อ 3)
- เมื่อผู้ใช้ save network จริง `ConfigureWifi` เขียนทับ atomic + RECONFIGURE ตามเดิม → minimal config
  เป็นแค่ bootstrap ไม่ชนกัน (placeholder ถูกแทนที่ทั้งไฟล์ ไม่อยู่ร่วมกับ network จริง)

**ต้องมี placeholder network block — ยืนยันจาก hardware แล้ว (ไม่ใช่ contingency อีกต่อไป):**
owner ทดสอบบนเครื่องจริงแล้วว่า **config แบบ header-only (ไม่มี network block) ใช้ไม่ได้ — สแกนยังว่าง**.
wpa ต้องมี network block ที่ **enabled** อย่างน้อยหนึ่งอันจึงจะเข้าสถานะ attached ที่ปลดล็อกผลสแกนได้.
ดังนั้น T-10 กำหนด **placeholder network block เป็น baseline design ตายตัว** — รูปแบบที่ปลอดภัยกำหนดใน §3.2

### 3.2 รูปร่างของ placeholder network block ที่ปลอดภัย (spec ตายตัวสำหรับ T-10)

เงื่อนไขความปลอดภัย: (ก) **enabled** (ต้องขับให้ wpa เข้าสถานะ scanning — ยืนยันแล้วว่าจำเป็น),
(ข) **associate ไม่ได้จริงในทางปฏิบัติ** แม้มี AP ชื่อชนในละแวก, (ค) syntax ที่ wpa_supplicant รับได้
ไม่ error, (ง) ไม่โผล่เป็น "เครือข่ายที่ดูจริง" ใน log/UI, (จ) format ตาม convention `wpa.go`

**สเปกที่เลือก (ใช้อันนี้):**
```
network={
    ssid="<สุ่ม 32 hex ตอนสร้างไฟล์>"
    key_mgmt=WPA-PSK
    psk="<สุ่ม 63 อักขระตอนสร้างไฟล์>"
    priority=0
}
```
เหตุผลเชิงความปลอดภัย (สำคัญ — reviewer ต้องตรวจ):
- **ทำไม `key_mgmt=WPA-PSK` + psk (ไม่ใช่ `key_mgmt=NONE`)**: open (NONE) จะ **associate สำเร็จ** ถ้ามี
  open AP ที่ชื่อ SSID ชนกัน → เสี่ยง auto-join AP ของคนอื่น (นี่คือช่องโหว่ของ config ที่ owner ใช้ทดสอบ).
  WPA-PSK บังคับ 4-way handshake → แม้มี AP ชื่อชน (open หรือ WPA) ก็ handshake ไม่ผ่าน → **associate
  ไม่ได้เด็ดขาด** เหลือแค่ "scanning ค้าง" ตามที่ต้องการ
- **ทำไม SSID + psk ต้อง "สุ่มตอนสร้างไฟล์" ไม่ hardcode**: repo เป็น open-source — ถ้า hardcode ทั้ง
  SSID และ psk ผู้ไม่หวังดีตั้ง AP ชื่อ+psk ตรงตามค่าคงที่ได้ → wpa จะ join. สุ่มต่อเครื่อง (crypto/rand)
  ทำให้เดา/ชนไม่ได้. psk ยาว 8–63 อักขระตามข้อบังคับ WPA (ใช้ 63 เพื่อความปลอดภัย)
- **ทำไม *ไม่* ใช้ `disabled=1`**: `disabled=1` = network ที่ wpa ไม่พยายามต่อ → ถ้าทุก network ถูก
  disable ก็เท่ากับ "ไม่มี enabled network" ซึ่ง **เทียบเท่า header-only ที่ owner พิสูจน์แล้วว่าใช้ไม่ได้**
  → มีความเสี่ยงสูงที่ wpa จะไม่เข้าสถานะ scanning. จึงเลือก **enabled + associate-ไม่ได้-ด้วย WPA/สุ่ม**
  แทนการพึ่ง disabled (ปลอดภัยเท่ากันเรื่อง join แต่ไม่เสี่ยงเรื่อง "ไม่ scan")
- **ไม่รั่วข้อมูล / ไม่ดูเป็นเครือข่ายจริง**: ค่าเป็น hex/สุ่มล้วน ไม่สื่อความ; ไม่มี secret ของผู้ใช้;
  ไฟล์ 0600. Placeholder เป็น config สำหรับ *client association* ไม่ใช่ผล scan → ไม่โผล่ในกล่องผลสแกน UI

---

## 4. Tasks (rev.5)

```json
[
  {
    "task_id": "T-08a",
    "title": "[RETIRED — ตกไป] PiGate ตั้ง regdomain เองผ่าน SetRegulatoryRegion",
    "layer": "kernel",
    "files": ["backend/internal/kernel/real_network.go"],
    "instruction": "ห้ามทำ. T-07 พิสูจน์แล้วว่า regdomain = 'TH' อยู่แล้วทั้งเคสที่พังและเคสที่ได้ → regdomain ไม่ใช่ต้นเหตุ. เก็บ task นี้ไว้ในเอกสารเป็นบันทึกประวัติเท่านั้น.",
    "acceptance": ["ไม่ implement — retired"],
    "depends_on": ["T-07"],
    "status": "ruled-out"
  },
  {
    "task_id": "T-08b",
    "title": "[RETIRED — ตกไป] แก้ parse/กรอง SSID ใน mapWifiScanResults",
    "layer": "kernel",
    "files": ["backend/internal/kernel/real_network.go"],
    "instruction": "ห้ามทำ. T-07 เคสที่ได้แสดง '11 BSS, 9 usable' → mapWifiScanResults ทำงานถูกต้อง; เคสที่พังคือ cache ว่างดิบ (0 BSS) ไม่ใช่ parse ทิ้ง. เก็บไว้เป็นบันทึกประวัติ.",
    "acceptance": ["ไม่ implement — retired"],
    "depends_on": ["T-07"],
    "status": "ruled-out"
  },
  {
    "task_id": "T-10",
    "title": "[CORE][SENSITIVE] รับประกัน minimal wpa_supplicant config (มี placeholder network block) เสมอสำหรับ wireless interface ที่ UP",
    "layer": "kernel",
    "files": ["backend/internal/kernel/wpa.go", "backend/internal/kernel/real_network.go"],
    "instruction": "ปิดช่องว่าง lifecycle (§2.1): ทำให้ wireless interface ที่ UP มีไฟล์ wpa config เสมอ เพื่อ wpa_supplicant@<iface> start+attach ได้ (จำเป็นสำหรับชิป on-board brcmfmac ให้คืนผล scan). กรอบคิด owner: wpa คือ process ที่ attach อยู่แล้ว — งานนี้แค่ 'ให้ไฟล์ config มันเจอ' เพื่อให้มัน start ได้เสมอ ไม่ใช่รอผู้ใช้ save network ก่อน. **owner ยืนยันบน hardware แล้วว่า config header-only (ไม่มี network block) ใช้ไม่ได้ — ต้องมี placeholder network block ที่ enabled. นี่คือ design ตายตัว ไม่ต้อง hardware round-trip อีก.** (1) ใน wpa.go: refactor แยก helper writeWpaHeader ที่เขียน 6 บรรทัด header ที่ GenerateWpaConfig ใช้อยู่ (ctrl_interface=DIR=/var/run/wpa_supplicant GROUP=netdev / update_config=1 / country=TH / ap_scan=1 / autoscan=periodic:10 / disable_scan_offload=1) เพื่อไม่ hardcode 'TH'/directive ซ้ำสองที่; ให้ GenerateWpaConfig เดิมเรียก helper นี้ (พฤติกรรมเดิมไม่เปลี่ยน). (2) เพิ่ม GenerateMinimalWpaConfig() คืน header (จาก helper) + **placeholder network block ตาม spec §3.2 ของแผน**: ssid = สุ่ม 32 hex (crypto/rand), key_mgmt=WPA-PSK, psk = สุ่ม 63 อักขระ (crypto/rand, ช่วงอักขระ ASCII ปลอดภัยสำหรับ psk เช่น hex หรือ base64-url), priority=0. **ห้ามใช้ key_mgmt=NONE (เสี่ยง auto-join AP ชื่อชน) และห้ามใช้ disabled=1 (เท่ากับ header-only ที่พิสูจน์แล้วว่าไม่ scan).** ห้าม hardcode ssid/psk (repo เป็น public — ต้องสุ่มต่อเครื่อง). ใช้ SanitizeWpaInput ไม่จำเป็นเพราะค่ามาจาก rng ที่คุมเอง แต่ให้ generate เป็นชุดอักขระที่ปลอดภัยอยู่แล้ว (ไม่มี \\n หรือ \" ). (3) เพิ่ม EnsureWpaConfig(name): ถ้าไฟล์ /etc/wpa_supplicant/wpa_supplicant-<name>.conf **ยังไม่มี** → เขียน GenerateMinimalWpaConfig() แบบ atomic (เขียน temp .tmp แล้ว os.Rename, perms 0600 ตาม WI §4.3/§5.1); ถ้า **มีอยู่แล้ว ห้ามแตะ/ห้ามอ่าน/ห้ามเขียนทับเด็ดขาด** (คอนฟิกจริงของผู้ใช้ปลอดภัย 100%). validate ชื่อ interface แบบเดียวกับ ConfigureWifi (real_network.go:116-123) กัน path traversal. **ห้ามลบ/คืนไฟล์ที่ไหนเลย — ไม่ใช่ throwaway.** (4) ใน ToggleInterface UP path (real_network.go:63-90) สำหรับ isWireless: เรียก EnsureWpaConfig(name) **ก่อน** StartServiceViaDBus. คง guard เดิม: ห้าม start wpa/เขียน config ตอน interface DOWN, ห้ามเปิด interface เป็น side effect. (5) ห้ามแตะ mock.go/signature/frontend. **SENSITIVE (เขียนไฟล์ wpa config บนระบบจริง + start service + กระทบ Wi-Fi lifecycle) — reviewer ยืนยัน: (ก) never-overwrite/never-read/never-delete ไฟล์ที่มีอยู่, (ข) atomic write + 0600, (ค) interface-name validated กัน traversal, (ง) placeholder ตรง spec §3.2 — key_mgmt=WPA-PSK, ssid+psk สุ่มจาก crypto/rand ต่อเครื่อง (ไม่ hardcode), ไม่ใช่ NONE, ไม่ใช่ disabled=1, associate ไม่ได้จริง, (จ) psk ยาว 8-63 ตามข้อบังคับ WPA และไม่มีอักขระที่ทำ config เสีย, (ฉ) ไม่มี secret ของผู้ใช้ในไฟล์ placeholder, (ช) ไม่มี side effect เปิด interface หรือ start wpa ตอน down.**",
    "acceptance": [
      "go build + go vet + go test ./... ผ่าน",
      "อ่านโค้ดยืนยัน: EnsureWpaConfig เขียนเฉพาะเมื่อไฟล์ไม่มี, atomic (temp+rename), 0600, ไม่เคยอ่าน/เขียนทับ/ลบไฟล์ที่มีอยู่",
      "placeholder network block ตรง spec §3.2: enabled, key_mgmt=WPA-PSK, ssid+psk สุ่มจาก crypto/rand (ไม่ hardcode), ไม่มี key_mgmt=NONE, ไม่มี disabled=1",
      "header มาจาก helper เดียว (writeWpaHeader) — ไม่มี 'TH'/directive hardcode ซ้ำสองที่; GenerateWpaConfig เดิมพฤติกรรมไม่เปลี่ยน",
      "ToggleInterface UP (wireless) เรียก EnsureWpaConfig ก่อน start; DOWN ไม่ start/ไม่เขียน; ไม่มี side effect เปิด interface",
      "HARDWARE: บน RPi5 — wlan0 ที่ไม่เคย save network, toggle UP แล้วกด Scan (cold) → เห็นเครือข่าย (repro หลักหาย); ไม่ associate เข้า placeholder ที่ไหน"
    ],
    "depends_on": ["T-07"]
  },
  {
    "task_id": "T-09",
    "title": "[SENSITIVE] เลิกกลืน error ตอน start/stop wpa_supplicant ใน ToggleInterface — log-only (ไม่ propagate)",
    "layer": "kernel",
    "files": ["backend/internal/kernel/real_network.go"],
    "instruction": "แก้ real_network.go:86 (`_ = StartServiceViaDBus(serviceName)`) และจุดกลืน error ทำนองเดียวกัน (StopServiceViaDBus ที่ 99) ให้ตรวจและ **log** error (เช่น 'failed to start %s via D-Bus: %v'). **owner ยืนยัน: log อย่างเดียว ไม่ propagate error กลับ caller/UI** — ToggleInterface ยังคง return nil หลัง block นี้เหมือนเดิม (ไม่เปลี่ยน error contract/handler/UX). สำคัญขึ้นเมื่อรวมกับ T-10: ถ้า EnsureWpaConfig หรือ start ล้ม ต้องเห็นใน journal ไม่เงียบ (นี่คือเหตุที่ Log 1 ไม่มี wpa โดยไม่มีสัญญาณเตือน). อย่าเปลี่ยนลำดับ LinkSetUp/EnsureWpaConfig/start, อย่าเพิ่ม side effect เปิด interface. **SENSITIVE: reviewer ยืนยัน (ก) ยังคง log-only ไม่ทำ toggle ที่เคยสำเร็จให้ล้ม, (ข) log ช่วยเห็นว่าทำไม wpa ตาย, (ค) ไม่มี side effect เปิด interface.**",
    "acceptance": [
      "go build + go vet ผ่าน",
      "error ของ StartServiceViaDBus/StopServiceViaDBus ไม่ถูกกลืนเงียบ (มี log)",
      "ToggleInterface ยัง return nil เหมือนเดิม (log-only, ไม่ propagate) — ไม่เปลี่ยนพฤติกรรม toggle เคสปกติ",
      "ไม่มี side effect เปิด interface"
    ],
    "depends_on": []
  }
]
```

---

## 4.1 QA finding — deferred as follow-up (owner decision 2026-07-23)

QA รอบ T-10/T-09 ผ่าน (PASS) แต่พบจุดที่ควรบันทึกไว้: **TOCTOU race แคบมากใน `EnsureWpaConfig`** —
`os.Stat` แล้ว `os.WriteFile`+`os.Rename` ไม่ได้ล็อกป้องกัน concurrent request ในทางทฤษฎีถ้ามี
`ToggleInterface` (trigger `EnsureWpaConfig`) กับ `ConfigureWifi` (save network จริง) ชนกันพอดีในช่วง
เสี้ยววินาทีบน interface เดียวกัน อาจมีโอกาสที่ placeholder เขียนทับ config จริงที่เพิ่งบันทึกไป
(ไม่เคยมี race นี้มาก่อนเพราะไม่เคยมีโค้ดไหนเขียนไฟล์นี้แบบ check-then-write มาก่อน T-10)

**Owner ตัดสิน: ไม่ต้องแก้ก่อน merge รอบนี้ — เก็บเป็น follow-up แยก** (โอกาสเกิดจริงต่ำมาก ต้องมี 2
request ชนกันพอดี ไม่ใช่ flow ปกติของ UI) ทางแก้ที่แนะนำไว้สำหรับ follow-up ในอนาคต: per-interface
mutex คร่อม `ToggleInterface`/`ConfigureWifi`, หรือใช้ `renameat2` + `RENAME_NOREPLACE`
(`golang.org/x/sys/unix`) แทน stat-then-rename เพื่อปิดช่องที่ระดับ kernel

---

## 5. Final Acceptance (ทดสอบรวมหลังทำ T-10 + T-09 เสร็จ)

```json
{
  "final_acceptance": [
    "บน RPi5 จริง (non-mock): wlan0 ที่ **ไม่เคย save network** → toggle UP → กด Scan ครั้งเดียวบน cold cache (ไม่ต้องมีคอนฟิกจริง/ไม่ต้องรัน wavemon ก่อน) → เห็นเครือข่าย (repro หลักของ #88 หาย)",
    "ยืนยันจาก journal ว่า EnsureWpaConfig เขียน minimal config (เมื่อไม่มีไฟล์) และ wpa_supplicant@wlan0 attach สำเร็จก่อนสแกน",
    "placeholder ไม่ associate เข้าที่ไหนเลย (ยืนยันว่า WPA-PSK+สุ่ม กัน auto-join ได้จริง แม้มี AP ชื่อชน)",
    "ผู้ใช้ที่มีคอนฟิก Wi-Fi จริงอยู่แล้ว → toggle UP → คอนฟิกจริง **ไม่ถูกแตะ/อ่าน/ทับ** (ยืนยัน never-overwrite) และเชื่อมต่อได้ตามเดิม",
    "toggle wlan0 OFF/ON ซ้ำ → ยังสแกนเห็น (ไม่ regress) ; wpa ถูก stop ตอน DOWN ตามเดิม",
    "USB dongle ยังสแกนเห็นครบเหมือนเดิม (ไม่ regress)",
    "กด Scan บน wlan0 ที่ down → 409 actionable (T-01 ไม่ regress) ; ไม่มี AP → 'ไม่พบเครือข่าย' (T-03) ; spam/2-interface → singleflight (T-04) ไม่ regress",
    "T-09: ถ้า EnsureWpaConfig/start wpa ล้ม → journal เห็น error ชัด ไม่เงียบ (แต่ ToggleInterface ยัง return nil)",
    "ไม่มีการลบ/คืนไฟล์คอนฟิกที่ไหน (ยืนยันว่าไม่ใช่ throwaway-write-then-delete)",
    "go build + go vet + go test ./... (รวม -race บน internal/kernel) ผ่าน"
  ]
}
```

## 5.1 ยืนยันบน RPi5 จริงแล้ว (2026-07-23)

- wlan0 ไม่เคย save network → toggle UP → journal: `EnsureWpaConfig: no config existed for wlan0;
  wrote placeholder config to unlock scanning` → wpa_supplicant start สำเร็จผ่าน D-Bus
- กด Scan ครั้งเดียวบน cold cache (ไม่ต้องรัน wavemon/เขียนคอนฟิกมือก่อน) → เจอ **10 เครือข่าย** —
  repro หลักของ #88 หายแล้ว
- USB dongle (`wlx0cef1548ff2b`) ยังสแกนเจอ 31 เครือข่ายตามปกติ — ไม่ regress
- ผู้ใช้มีคอนฟิก Wi-Fi จริงที่เคย save ไว้ก่อนหน้า (SSID จริง) → หลัง toggle/scan รอบนี้ยืนยันแล้วว่า
  **ไฟล์ไม่ถูกแตะ/ทับ** — never-overwrite ทำงานถูกต้อง

Final acceptance ผ่านครบตามหลักฐานจริง — พร้อม merge

---

## 6. Decision for owner — ตัดสินแล้ว (ยึดตามนี้)

1. **อนุมัติ:** ทำ T-10 แนวทาง "minimal always-present config" (แทน throwaway). กรอบ owner: wpa คือ
   process ที่ attach อยู่แล้ว — T-10 แค่ทำให้มัน start ได้เสมอโดยมีไฟล์ config ให้มันเจอ
2. **ยืนยันจาก hardware:** header-only (ไม่มี network block) **ใช้ไม่ได้** → T-10 ใช้ **placeholder
   network block เป็น baseline ตายตัว** (ดู spec §3.2). ไม่ต้อง hardware round-trip เรื่องนี้อีก
3. **T-09:** ยืนยัน **log-only** ไม่ propagate error → doc/T-09 อัปเดตตามแล้ว
4. **(บันทึกไว้ นอก scope bug นี้)** country code `TH` ยัง hardcode — เป็นประเด็น compliance ระยะยาว
   ว่าควรทำ setting country ไหม; ไม่แก้ในแผนนี้
5. **behavior change ที่รับแล้ว:** wireless UP ⟹ wpa attach เสมอ (แม้ยังไม่ save network) — wpa idle
   ใช้พลังงานเล็กน้อย, radio ถูก wpa manage

---

## 7. Sensitive / Review Points

- **T-10 = จุดเสี่ยงหลัก (เขียน wpa config บนระบบจริง + start service):** never-overwrite/never-delete,
  atomic+0600, validate ชื่อ interface, **placeholder ตาม spec §3.2** (WPA-PSK + ssid/psk สุ่มจาก
  crypto/rand ต่อเครื่อง, ไม่ใช่ NONE, ไม่ใช่ disabled=1, associate ไม่ได้จริง), ไม่มี secret,
  ไม่ auto-join, ไม่มี side effect เปิด interface
- **T-09 (wpa lifecycle):** log-only, อย่าทำ toggle ที่เคยสำเร็จให้ล้ม, ไม่เปลี่ยน error contract
- **mechanism ระดับ nl80211 ยังไม่ยืนยัน primitive เป๊ะ (§2.2, ความเชื่อมั่นกลาง/ต่ำ)** — fix พึ่ง
  หลักฐานเชิงประจักษ์ "wpa attach = สแกนได้" ไม่ได้พึ่งการเดา primitive; ถ้าอนาคตยืนยัน primitive ได้
  ค่อยพิจารณาทำ nl80211-only (ให้ PiGate ทำเองโดยไม่พึ่ง wpa) เป็น optimization แยก
- rev.3 (wpa race), rev.4 H-A (regdomain), H-B (parse) **ตกไปแล้ว** — อย่านำกลับมาโดยไม่มีหลักฐานใหม่
- version pin `mdlayher/wifi v0.8.0` — read-mostly, ไม่ expose SET_INTERFACE/REGISTER_FRAME → อีกเหตุผล
  ที่เลือกพึ่ง wpa แทน reimplement
- T-01/T-03/T-04 (rev.2) + T-07 (diagnostic) เสร็จแล้ว — rev.5 ไม่แตะ
