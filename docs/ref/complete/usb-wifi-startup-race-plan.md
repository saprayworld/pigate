# USB Wi-Fi Startup Race — self-heal interface ที่ enumerate ไม่ทันตอน boot (issue #76)

> แผนงานแก้บั๊ก: หลัง restart ทั้งบอร์ด USB Wi-Fi (เช่น `wlx0cef1548ff2b`) ที่ kernel
> enumerate ช้า จะถูก startup apply ข้าม ("does not exist in kernel. Skipping.") แล้วถูก
> `seedKnownLinks()` นับเป็น "known" ก่อนเคยได้ event ทำให้ไม่มีวันเกิด `InterfaceAdded`
> → self-heal ไม่ทำงาน → config ไม่ถูก apply จนกว่าจะ restart service เอง
>
> เขียนเมื่อ: 2026-07-20 · Reference branch: `main` (แยก `fix/usb-wifi-startup-race`)
> อ้างอิง: GitHub issue #76 (ต่อยอด design จาก issue #48 self-healing event bus)
>
> **อัปเดต 2026-07-21 (PR #79 hold):** ระหว่างทดสอบ real-hardware ของ PR #79 บน branch
> `fix/usb-wifi-startup-race` (ซึ่งมี T-01–T-05 ของ #76 อยู่แล้ว) พบบั๊กที่สอง **คนละ root
> cause** จาก #76 แต่กระทบเป้าหมายหลักของ PR #79 โดยตรง (USB Wi-Fi ที่ผ่าน udev rename
> ไม่ได้ config apply เหมือนกัน) — ดู §1.1 (finding) และ §2.1 (แนวทาง) และ Task T-06/T-07
> ใหม่ท้าย §3
>
> **อัปเดต 2026-07-22 (ปิดงาน):** T-06/T-07 เสร็จแล้ว, PR #79 merged เข้า `main`
> (commit `4e2168d`), Final Acceptance §6 ยืนยันครบทุกข้อบนบอร์ดจริงแล้ว — งานนี้ปิดแล้ว

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย:** reboot บอร์ดที่มี USB Wi-Fi ตั้งค่าไว้ใน DB แล้ว interface นั้นต้องกลับมา
  ใช้งานได้เอง (Wi-Fi associate + IP + dhcpcd/routing/firewall-related re-apply ครบ)
  โดย**ไม่ต้อง restart pigate service** ไม่ว่า interface จะ enumerate ก่อน, ระหว่าง,
  หรือหลังช่วง startup apply ของ pigate **และไม่ว่า kernel จะสร้าง interface นั้นด้วยชื่อ
  default ก่อนแล้วโดน udev rename เป็นชื่อ MAC-based เกือบทันทีหรือไม่ก็ตาม** (เพิ่มเข้ามา
  จาก finding 2026-07-21 — เดิมแผนนี้ครอบคลุมแค่ "enumerate ช้า", ตอนนี้ครอบคลุม "enumerate
  แล้วแต่ชื่อยังไม่ settle" ด้วย)
- **เงื่อนไขทางเทคนิค:** interface ที่ startup apply "ข้ามไป" เพราะยังไม่อยู่ใน kernel
  ต้องได้ event `InterfaceAdded` (จริงหรือ synthetic) เสมอเมื่อมันโผล่มา — ปิดหน้าต่าง race
  ระหว่าง `InitApplyConfigurationAtStartup()` (main.go step 6.1) กับ `seedKnownLinks()`
  (ใน `NetlinkMonitor.Start()`, step 6.5); **และ** `InterfaceAdded` ที่ publish ออกไปต้องเป็น
  ชื่อที่ "settle" แล้ว (ชื่อที่ DB/self-heal subscriber อื่น ๆ จะ match เจอจริง) ไม่ใช่ชื่อ
  ephemeral ที่ kernel ตั้งให้ก่อน udev rename
- **Out of scope:**
  - ไม่ทำ periodic health checker / ตรวจ 169.254 (นั่นคือ issue #78 — ดู
    `docs/ref/todo/dhcpcd-link-local-fallback-notes.md`)
  - ไม่เปลี่ยน design "self-heal หนักเฉพาะ `InterfaceAdded`, ไม่ฟัง `LinkChanged`"
    (กัน re-apply storm ตาม issue #48)
  - ไม่แตะ frontend / API / DB schema / install.sh ใด ๆ
  - ไม่เพิ่ม "rename interface ผ่าน UI" หรือ OS-level rename ใด ๆ — ยืนยันแล้วว่าโปรเจกต์นี้
    ไม่มี feature ให้ผู้ใช้สั่ง rename ชื่อ interface ระดับ OS (มีแค่ "alias" ซึ่งเป็น field
    ใน DB สำหรับแสดงผล ไม่กระทบชื่อ netlink จริง) — ดู §1.1 ว่าทำไมเรื่องนี้ทำให้ scope
    ของบั๊กที่สองแคบกว่าที่คิดตอนแรก

## 1. สภาพโค้ดปัจจุบัน (สำรวจ ณ 2026-07-20)

| ส่วน | สถานะ | อ้างอิง |
|---|---|---|
| Startup apply ข้าม iface ที่ไม่อยู่ใน kernel | log "Skipping." แล้วทิ้งเลย **ไม่จำว่าข้ามตัวไหน** | `service/interface.go:~76-82` |
| Signature ของ `InitApplyConfigurationAtStartup` | `func() error` — backup restore เรียกผ่าน `step(...)` ที่รับ `func() error` **ห้ามเปลี่ยน signature** | `service/interface.go:46`, `service/backup.go:482` |
| ลำดับ startup ใน main.go | 6.1 apply interfaces → 6.2-6.4 (routes/DHCP/DNS/firewall/QoS ใช้เวลาหลายวินาที) → 6.5 `netlinkMonitor.Start()` เป็นตัวสุดท้าย (ตั้งใจ, issue #48) | `cmd/pigate/main.go:~295-398` |
| `seedKnownLinks()` | เรียก `netlink.LinkList()` **ณ เวลา Start()** — link ที่โผล่ระหว่าง 6.1→6.5 ถูก seed เป็น known ทันที | `service/netlink_monitor.go:~102,193-209` |
| `handleLinkUpdate` | publish `InterfaceAdded` เฉพาะ index ที่ไม่เคยเห็น; index ที่ถูก seed แล้วได้แค่ `LinkChanged` ตลอดไป | `service/netlink_monitor.go:~168-186` |
| Self-heal subscribers | interface/dns/dhcp-server/qos key ที่ `InterfaceAdded` เท่านั้น; dhcpcd ฟัง `LinkChanged` ด้วยแต่ช่วยไม่ได้ (Wi-Fi ไม่มี config → ไม่มีวัน RUNNING) | `cmd/pigate/main.go:~190-252` |
| `ReapplyInterfaceByName` | มีครบแล้ว: ignore iface ที่ไม่ managed, recreate child VLAN, เรียก `applyOneInterface` (รวม ConfigureWifi) | `service/interface.go:~171-210` |
| `NetEvent` | มี field `Up`/`Running` ให้ synthetic event ใช้ได้ | `service/event_bus.go:51-56` |
| Mock mode | `Start()` return ก่อนถึง seed (ไม่ subscribe อะไร) | `service/netlink_monitor.go:56-59` |
| เทสต์เดิม | `handleLinkUpdate` ทดสอบแบบ inject `known` map ได้โดยไม่แตะ kernel จริง — ใช้เป็นแม่แบบ | `service/netlink_monitor_test.go:71-154` |

**สรุป root cause (บั๊กแรก, #76):** USB Wi-Fi enumerate เสร็จ**ในหน้าต่าง** 6.1→6.5 → ถูกข้าม
ตอน apply และถูก seed เป็น known → event แรกที่ได้จริงเป็น `LinkChanged` ไม่ใช่
`InterfaceAdded` → self-heal ไม่ยิง งานกระจุกที่ **backend service layer 2 ไฟล์ + main.go**
เท่านั้น

> **สถานะ ณ 2026-07-21:** T-01–T-05 ด้านล่าง (แก้บั๊กแรกนี้) **ถูก implement ครบแล้ว** บน
> branch `fix/usb-wifi-startup-race` — ยืนยันจากการอ่านโค้ดจริง: `linkState.settling`
> ยังไม่มีในโค้ดปัจจุบัน (จะถูกเพิ่มโดย T-06), แต่ `StartupSkippedInterfaces()`
> (`interface.go:105-116`), `Start(ctx, missedAtStartup)` + `publishMissedStartupLinks`
> (`netlink_monitor.go:64-228`), และการต่อสายใน main.go มีอยู่ครบตามที่ T-01/T-03/T-05
> ระบุ พร้อมเทสต์ T-02/T-04 ครบ

### 1.1 การค้นพบเพิ่มเติมจาก real-hardware testing (2026-07-21) — บั๊กที่สอง คนละสาเหตุ กระทบเป้าหมายเดียวกัน

**Evidence (log จริงบน workstation, service restart ไม่ใช่ full reboot):**

```
[NetlinkMonitor] Interface added: iface="wlan0" index=13 up=... running=...
[NetlinkMonitor] Link changed: iface="wlx4086cbb56030" index=13 up=... running=...
[Self-heal] Interface "wlan0" returned; re-applying its configuration
[ApplyInterface] ReapplyInterfaceByName(wlan0): not a managed interface, ignoring
```

(เห็น "does not exist in kernel. Skipping." ตอน boot ด้วย ยืนยันว่า USB Wi-Fi หลุดออกจาก
kernel ระหว่าง service restart จริง แม้เป็นแค่ restart service ไม่ใช่ reboot ทั้งบอร์ด)

**Root cause:** kernel สร้าง USB Wi-Fi adapter (`wlx4086cbb56030`) ด้วยชื่อ default ก่อน
(`wlan0`) แล้ว udev rename เป็นชื่อ MAC-based บน **netlink index เดิม** (13) เกือบทันที —
`handleLinkUpdate` (`netlink_monitor.go:160-198`, ยืนยันจากการอ่านโค้ดจริง) ตีความ
`RTM_NEWLINK` ครั้งที่สองบน index ที่เคยเห็นแล้ว (`seen == true`) เป็น "flag/attribute
change" เสมอ (มีคอมเมนต์ในโค้ดเองตรง ๆ ว่า `// keep state current (a rename also arrives
as NEWLINK)`) → publish เป็น `LinkChanged` ไม่ใช่ `InterfaceAdded` — **พฤติกรรมนี้มีมาก่อน
#76 และไม่ได้ถูกแตะโดย T-01–T-05 เลย** (T-01–T-05 แก้เฉพาะกรณี "enumerate ช้าจนถูก seed
เป็น known ตั้งแต่แรก" ไม่ใช่กรณี "enumerate ทันแต่ชื่อยังไม่ settle")

ผลคือ: `InterfaceAdded` ตัวแรกและตัวเดียวที่เกิดขึ้นจริง publish ออกไปพร้อมชื่อ ephemeral
(`wlan0`) ไม่ใช่ชื่อสุดท้าย (`wlx4086cbb56030`) — subscriber `"interface"`
(`ReapplyInterfaceByName`, `service/interface.go:202-241`, lookup ด้วย exact-name match
กับ DB ที่ `:210-215`) หา `"wlan0"` ใน DB ไม่เจอ (DB เก็บ `wlx4086cbb56030` เพราะ user
ตั้งค่าจริงด้วยชื่อ MAC-based) → log `"not a managed interface, ignoring"` → **ไม่มีการ
เรียก `ConfigureWifi`/apply IP ใด ๆ เลยสำหรับชื่อจริง**

**ทำไม log ดูเหมือนใช้งานได้ (Wi-Fi associate + ได้ IP):** เพราะ
`wpa_supplicant@wlx4086cbb56030.service` มี config ไฟล์ค้างอยู่บนดิสก์จาก session ก่อนหน้า
(แค่ service restart ไม่ใช่ full reboot) แล้ว systemd unit auto-start เองจาก device
presence — pigate self-heal ไม่ได้มีส่วนเกี่ยวข้องเลย บนเครื่องที่เพิ่ง provision ใหม่
(ไม่เคยมี wpa_supplicant config ไฟล์นี้มาก่อน หรือ SSID/password ถูกเปลี่ยนใน DB) จุดนี้จะ
ล้มเหลวแบบเงียบ ๆ ไม่มี IP เลย — Final Acceptance ข้อใหม่ (§6) ต้องทดสอบเคสนี้โดยเฉพาะ
เพื่อไม่ให้ผลทดสอบหลอกแบบเดียวกันอีก

**ทำไมสมมติฐานเดิมของ §2 ใช้ไม่ได้กับเคสนี้:** §2 เดิมเขียนไว้ว่า "ตัวที่ถูกข้ามแต่ยังไม่
โผล่ ไม่ต้องทำอะไร — เมื่อโผล่ทีหลังจะได้ `InterfaceAdded` จริงตามกลไกเดิมอยู่แล้ว" —
สมมติฐานนี้อาศัย "กลไกเดิม" ของ `handleLinkUpdate` ว่าจะ publish `InterfaceAdded` ให้เองเมื่อ
index เป็นของใหม่ ซึ่ง**เป็นจริงเฉพาะตอนที่ยังไม่เคยเห็น index นั้นมาก่อนเท่านั้น** ถ้า USB
Wi-Fi enumerate เร็วพอที่จะได้ `InterfaceAdded` จริง (ไม่ทันโดน seed เป็น known แบบ #76)
แต่ยังโดน udev rename ตามมาติด ๆ บน index เดียวกัน มันจะไปชน "กลไกเดิม" ที่เพิ่งอธิบายว่า
เป็นบั๊ก ไม่ใช่ตามกลไกที่ §2 สันนิษฐานไว้ — **เป้าหมายหลักของ PR #79 ("reboot บอร์ดที่มี USB
Wi-Fi ตั้งค่าไว้ใน DB แล้ว interface นั้นต้องกลับมาใช้งานได้เองเสมอ") ยังไม่ครบจริงในเคสนี้
แม้ T-01–T-05 จะเสร็จแล้ว**

**ยืนยันแล้วว่าไม่กระทบ (ไม่ต้องแก้):**
- `seedKnownLinks()` (`netlink_monitor.go:233-249`) — ดึงจาก `netlink.LinkList()` สด
  ณ เวลา `Start()` ซึ่งอ่านค่า **ปัจจุบัน** ของ kernel ตรง ๆ ถ้า udev rename เสร็จก่อนถึง
  `Start()` (ซึ่งเป็นกรณีทั่วไปเพราะ `Start()` มาหลัง step 6.1-6.4 ที่กินเวลาหลายวินาที)
  ชื่อที่ seed จะเป็นชื่อ final อยู่แล้ว — synthetic path ของ T-03 (#76) ไม่ชนบั๊กนี้ใน
  เคสทั่วไป (แต่มี residual race แคบมากที่ T-06 ปิดให้ด้วย ดู §2.1 และ Caution 8)
- Subscriber อื่นที่ฟัง `InterfaceAdded` แบบ global ไม่ filter ด้วยชื่อ — `dns`
  (`main.go:228-235`), `dhcp-server` (`:239-244`), `qos` (`:247-252`), `event-log`
  (`:256-268`) — reapply/log แบบไม่ผูกกับชื่อ interface เฉพาะเจาะจง ดังนั้นทำงานถูกต้อง
  แม้ event มาพร้อมชื่อ ephemeral ผิด ไม่ต้องแก้
- `dhcpcd` subscriber (`main.go:203-208`, `dhcpcd.go:182-211` `HandleLinkEvent`) —
  **มี exact-name filter เหมือนกัน** (`if iface.Name == name` ที่ `dhcpcd.go:192`) จึงพลาด
  event แรก (`InterfaceAdded("wlan0")`) เหมือน `ReapplyInterfaceByName` เป๊ะ **แต่**
  เพราะ dhcpcd subscribe ทั้ง `InterfaceAdded` **และ** `LinkChanged` (ไม่เหมือน
  `"interface"` subscriber ที่ฟังแค่ `InterfaceAdded`) มันจึงได้โอกาสที่สองจาก event
  `LinkChanged("wlx4086cbb56030")` ที่ตามมา (ชื่อถูกต้องแล้ว) แล้ว match กับ DB เจอ
  ตัดสินใจ start/stop dhcpcd ถูกต้องในที่สุด — **นี่คือเหตุผลจริงที่ dhcpcd ทำงานได้แม้เจอ
  บั๊กเดียวกัน ไม่ใช่เพราะมันไม่ filter ด้วยชื่อ (แก้ finding เดิมของผู้ประสานงานให้แม่นยำ
  ขึ้น — `HandleLinkEvent` filter ด้วยชื่อเหมือนกัน จุดต่างคือมันฟัง `LinkChanged` ด้วย จึง
  ได้โอกาสที่สอง)** — ผลคือ dhcpcd ไม่ต้องแก้อะไรเพิ่ม เพราะเมื่อ T-06 ทำให้ event รอบ rename
  กลายเป็น `InterfaceAdded` แทน `LinkChanged` มันยังอยู่ใน `[]NetEventKind` ที่ dhcpcd
  subscribe อยู่ดี (ทั้งคู่อยู่ใน `{InterfaceAdded, LinkChanged, InterfaceRemoved}`)
- ไม่มี OS-level "rename interface" feature ในโปรเจกต์นี้เลย (grep แล้ว: มีแต่ `os.Rename`
  สำหรับ atomic file write ใน `kernel/real_network.go:140` และ `kernel/dhcpcd.go:64` — คนละ
  เรื่องกับ netlink rename) — เพราะฉะนั้น "rename ที่ user สั่งเองผ่าน UI" ที่ต้องระวังไม่ให้
  ชน **ไม่มีอยู่จริงในโค้ด** ลด scope ของปัญหา "แยกแยะ genuine rename" ลงมาก (ดู §2.1 ว่า
  ทำไมยังออกแบบให้ทนทานต่อ manual `ip link` rename แบบ hypothetical อยู่ดี แม้ไม่มี UI ก็ตาม)

## 2. แนวทางเทคนิค

**ให้ `InitApplyConfigurationAtStartup` จดรายชื่อ interface ที่ถูกข้าม แล้วตอน
`NetlinkMonitor.Start()` หลัง seed เสร็จ ยิง synthetic `InterfaceAdded` ให้เฉพาะตัวที่
"ถูกข้าม ∧ ตอนนี้อยู่ใน seed แล้ว"** (ตัวที่ถูกข้ามแต่ยังไม่โผล่ ไม่ต้องทำอะไร —
เมื่อโผล่ทีหลังจะได้ `InterfaceAdded` จริงตามกลไกเดิมอยู่แล้ว)

```go
// netlink_monitor.go — เรียกใน goroutine ของ Start() ทันทีหลัง seedKnownLinks()
func (m *NetlinkMonitor) publishMissedStartupLinks(known map[int]linkState, missed []string) {
    for _, name := range missed {
        for _, st := range known {
            if st.name == name {
                log.Printf("[NetlinkMonitor] Interface %q appeared during the startup window; publishing synthetic InterfaceAdded (issue #76)", name)
                m.bus.Publish(NetEvent{Kind: InterfaceAdded, Name: name, Up: st.up, Running: st.running})
                break
            }
        }
    }
}
```

- **ทำไมแนวนี้:** แม่นยำ — ยิงเฉพาะตัวที่พิสูจน์แล้วว่า "config ยังไม่เคยถูก apply"
  จึงไม่มีทางเกิด re-apply storm ตอน boot ปกติ (คงหลักการ issue #48); event วิ่งผ่าน
  bus ปกติ ทำให้ subscriber ทุกตัว (interface reapply, dhcpcd, dns, dhcp-server, qos,
  event-log) ได้รับเหมือน interface มาช้าจริง ๆ — ครบทั้ง Wi-Fi associate, dhcp-range,
  qos โดยไม่ต้องเขียน path พิเศษ; ผิวสัมผัสเล็ก (3 ไฟล์) และ unit-test ได้โดยไม่แตะ
  kernel จริง (inject `known` map ตามแม่แบบเทสต์เดิม)
- **ทางเลือกที่ปฏิเสธ:**
  1. *ย้ายจุด seed ไปก่อน 6.1* — ไม่พอ: event ที่เกิดระหว่าง snapshot กับ subscribe
     ไม่ถูกส่ง (subscribe เกิดใน Start) link ที่โผล่ในหน้าต่างจะเงียบจนกว่า flag
     จะเปลี่ยนครั้งถัดไป ซึ่งอาจไม่มาอีกนาน — ยังพลาดเหมือนเดิม
  2. *diff snapshot kernel สองจุด (ก่อน 6.1 vs ตอน Start) แล้วยิง event ให้ index ใหม่* —
     กว้างเกิน: link ที่โผล่หลัง snapshot แต่ก่อน 6.1 อ่าน kernel จะถูก apply ไปแล้ว
     และโดนยิงซ้ำ (double ConfigureWifi → reassociate ฟรี) ทั้ง coupling เรื่องจังหวะ
     snapshot กับ 6.1 ก็เปราะกว่า skip-list ที่เป็นข้อเท็จจริงตรง ๆ ว่า "ตัวไหนไม่ถูก apply"
  3. *ให้ self-heal ฟัง `LinkChanged` ด้วย* — ปฏิเสธเด็ดขาด: ย้อน design issue #48
     (ลิงก์กระพริบ = re-apply storm)
  4. *periodic reconcile poller* — ใหญ่เกินโจทย์ deterministic นี้ และซ้อนทางกับงาน #78
- **แม่แบบโค้ดที่ยึด:** โครงเทสต์และการแยก logic ให้ inject ได้ของ
  `netlink_monitor_test.go` (`known` map เป็น parameter), สไตล์ log/Publish ของ
  `handleLinkUpdate`

### 2.1 แนวทางเทคนิคเพิ่มเติมสำหรับบั๊กที่สอง (T-06/T-07) — udev rename race

**เลือก: เพิ่ม "settling window" หนึ่งช็อตต่อ index ใน `linkState`** — เมื่อ index ไหน
publish `InterfaceAdded` ไปแล้ว (ไม่ว่าจะจาก event จริงครั้งแรก หรือ synthetic ของ T-03)
ให้ mark `settling = true`; ถ้า **event ถัดไปสำหรับ index เดียวกัน** (event แรกที่ตามมา ไม่
ว่าจะเป็นอะไรก็ตาม) เป็น rename (ชื่อเปลี่ยน) ให้ publish เป็น `InterfaceAdded` (ด้วยชื่อ/flag
ใหม่) แทน `LinkChanged` — ส่วน event ถัดไปแบบอื่น (flag เปลี่ยนแต่ชื่อเดิม, duplicate) ก็
consume settling window เหมือนกัน (กลายเป็น false ถาวร) แม้จะไม่ได้ trigger การ publish
พิเศษก็ตาม

```go
// linkState เพิ่ม field ใหม่
type linkState struct {
    name    string
    up      bool
    running bool
    // settling is true for exactly one event-window immediately after this index's
    // entry was first created via InterfaceAdded (real or synthetic) in this monitor's
    // lifetime. It is consumed (cleared) by the very next RTM_NEWLINK processed for the
    // index, whatever kind it turns out to be. See handleLinkUpdate's rename-during-
    // settling branch (PR #79 follow-up, USB Wi-Fi udev rename race).
    settling bool
}

// เทียบเฉพาะ attribute ที่มีความหมาย — ห้ามใช้ `prev == newState` ตรง ๆ อีกต่อไป เพราะ
// settling ของ prev/newState มักไม่เท่ากัน จะทำให้ duplicate-suppression เดิมพังเงียบ ๆ
func (a linkState) sameAttrs(b linkState) bool {
    return a.name == b.name && a.up == b.up && a.running == b.running
}
```

- **ทำไมแนวนี้:** ตรงจุด — จับ pattern จริงของ udev rename (ตาม log จริงใน §1.1: rename
  เป็น *event ถัดไปทันที* หลัง creation แทบทุกครั้ง เพราะ udev rename ก่อนที่ daemon ไหนจะ
  ทันไปแตะ interface) โดยไม่ใช้ wall-clock timing เลย (ไม่ fragile ต่อ scheduling jitter);
  bound ความเสี่ยงชัดเจน — index หนึ่ง ๆ ได้ "โบนัส" `InterfaceAdded` พิเศษได้แค่ครั้งเดียว
  และ**เฉพาะตอนที่ยังไม่เคยมี event อื่นคั่นเลยเท่านั้น** ดังนั้น rename ของ interface ที่
  settle แล้ว (มี event อื่นผ่านมาก่อนหน้านั้น ไม่ว่าจะนานแค่ไหน) ยังคงเป็น `LinkChanged`
  ตามเดิมเป๊ะ — ตอบโจทย์ "ต้องแยก genuine rename ของ interface ที่รู้จักอยู่แล้วออกจาก
  ephemeral name ตอน enumerate ครั้งแรก" แม้โปรเจกต์นี้จะไม่มี UI สั่ง rename จริงก็ตาม
  (§1.1) — ยังออกแบบให้ทนทานต่อ manual `ip link set name` ที่ทำนอก pigate ไว้ด้วยเผื่ออนาคต
- **ที่ตั้งของการแก้:** ทั้งหมดอยู่ใน `netlink_monitor.go` เท่านั้น — **ไม่แก้**
  `interface.go` (`ReapplyInterfaceByName`), `dhcpcd.go`, หรือ `main.go` เลย เพราะเมื่อ
  monitor รับประกันว่า `InterfaceAdded` ที่ publish ออกไปเป็นชื่อ settled แล้วเสมอ
  subscriber ทุกตัวที่ filter ด้วย exact-name (ทั้ง `ReapplyInterfaceByName` และ
  `dhcpcd.HandleLinkEvent`) ก็ถูกต้องอยู่แล้วโดยไม่ต้องแตะ — ตรงกับหน้าที่เดิมของไฟล์นี้
  ตามที่ doc comment บนสุดของไฟล์บอกไว้แล้ว ("distinguishing a genuinely new interface
  from a mere flag change")
- **ทางเลือกที่ปฏิเสธ:**
  1. *Timestamp-based (ถ้า rename เกิดภายในไม่กี่ร้อย ms หลัง InterfaceAdded ให้ republish)*
     — ปฏิเสธ: fragile ต่อ scheduling/system load, ไม่มี "ค่าที่ถูกต้อง" ตายตัวสำหรับ
     threshold, และ debug ยาก (พฤติกรรม flaky ขึ้นกับจังหวะเครื่อง) — settling-window
     แบบ event-based ให้ผลลัพธ์ deterministic เหมือนเดิมทุก run
  2. *เก็บ flag "ยังไม่เคย reapply สำเร็จ" แล้วให้ service layer อ่าน* — ปฏิเสธ: ต้อง
     coupling `netlink_monitor.go` กับผลลัพธ์ของ `ReapplyInterfaceByName` (ack channel
     ย้อนกลับ) ซึ่งขัด design เดิมที่ตั้งใจให้ monitor เป็น "thin translator" ไม่รู้จัก
     business logic ของ subscriber ใด ๆ (ตาม doc comment บนสุดของไฟล์) — เพิ่ม coupling
     โดยไม่จำเป็น ในเมื่อ settling-window แก้ได้จบใน layer เดียว
  3. *แก้ที่ `ReapplyInterfaceByName` ให้ fallback เทียบ MAC address หรือ index แทนชื่อ* —
     ปฏิเสธ: (ก) `NetEvent` ไม่มี field MAC ตอนนี้ ต้องเพิ่ม field ใหม่ใน `event_bus.go`
     ขยาย blast radius ไปทุก publisher/subscriber โดยไม่จำเป็น (ข) แก้เฉพาะจุดเดียวจะไม่
     ครอบคลุม `dhcpcd.HandleLinkEvent` ที่ filter ด้วยชื่อเหมือนกัน (ต้องแก้ซ้ำสองที่ เสี่ยง
     behavior ไม่ตรงกัน) (ค) การแก้ที่ต้นตอ (monitor layer) แก้ปัญหาให้ subscriber ทุกตัว
     ทั้งปัจจุบันและอนาคตพร้อมกันในจุดเดียว ตรงกับหน้าที่ที่ไฟล์นี้ถูกออกแบบมาให้ทำอยู่แล้ว
  4. *ให้ settling คงอยู่ตราบใดที่ชื่อยังไม่เปลี่ยน (ทนต่อ flag-only event คั่นก่อน rename)*
     — พิจารณาแล้วปฏิเสธ: ทำให้ index ที่ไม่เคยถูก rename เลยตั้งแต่สร้างมีค่า
     `settling=true` ค้างอยู่ตลอดไปจนกว่าจะมี rename ครั้งแรก (อาจเป็นวัน/สัปดาห์ให้หลัง)
     ซึ่งขัดเป้าหมายที่ต้องการแยก "ephemeral ตอน enumerate" ออกจาก "genuine rename ของ
     interface ที่ settle แล้ว" — ยอมรับ residual race แคบ ๆ แทน (มี flag-only event คั่น
     ก่อน rename จริง) และบันทึกไว้เป็น Caution 8 แทนที่จะแก้ให้ครบทุกกรณี
- **แม่แบบโค้ดที่ยึด:** โครง `handleLinkUpdate`/`linkState` เดิมเป๊ะ (เพิ่ม field ใหม่และ
  branch ใหม่เข้าไปในโครงเดิม ไม่ปรับ architecture); โครงเทสต์ `netlink_monitor_test.go`
  (`newLinkUpdate`, `collectEvents`, `expectEvent`)

## 3. ขั้นตอน (เรียง inner-layer-first — ทำครบทุก Task ก่อน แล้วค่อยทดสอบรวมตาม §6)

### Task T-01 — จดรายชื่อ interface ที่ถูกข้ามตอน startup apply
**File:** `backend/internal/service/interface.go` (แก้ไข)
- เพิ่ม field ใน `InterfaceService` (`:33-36`): `startupSkippedMu sync.Mutex` +
  `startupSkipped []string`
- ในลูป `:76-82`: บรรทัดที่ log "Skipping." ให้ append ชื่อเข้า slice (เก็บผ่าน mutex
  ตอนจบฟังก์ชัน — เขียนทับทั้ง slice ทุกครั้งที่ฟังก์ชันรัน ไม่ append ข้ามรอบ)
- เพิ่ม accessor `StartupSkippedInterfaces() []string` คืน **copy** ภายใต้ mutex
- **ห้าม**เปลี่ยน signature `InitApplyConfigurationAtStartup() error` (backup.go:482
  ต้องการ `func() error`)
- **เสร็จเมื่อ:** คอมไพล์ผ่าน; ฟังก์ชันเดิมพฤติกรรมไม่เปลี่ยนนอกจากการจดรายชื่อ
- **สถานะ:** ✅ ทำเสร็จแล้ว (ยืนยันจากการอ่านโค้ดจริง 2026-07-21 — `interface.go:38-45,
  85-116`)

### Task T-02 — เทสต์การจดรายชื่อ
**File:** `backend/internal/service/interface_test.go` (แก้ไข — ล้อ `TestInitApplyConfigurationAtStartup:77`)
- เคส: DB มี iface ที่ไม่อยู่ใน kernel → ชื่อโผล่ใน `StartupSkippedInterfaces()`;
  iface ที่อยู่ใน kernel → ไม่โผล่; เรียก Init ซ้ำ → slice ถูก reset ไม่สะสม
- **เสร็จเมื่อ:** `go test ./internal/service/... -run TestInitApply` ผ่าน
- **สถานะ:** ✅ ทำเสร็จแล้ว (ยืนยันจากการอ่านโค้ดจริง 2026-07-21)

### Task T-03 — synthetic InterfaceAdded ใน NetlinkMonitor
**File:** `backend/internal/service/netlink_monitor.go` (แก้ไข)
- เปลี่ยน signature เป็น `Start(ctx context.Context, missedAtStartup []string)`
  (call site เดียวคือ main.go — BackupService ใช้แค่ Pause/Resume ไม่กระทบ)
- เพิ่ม `publishMissedStartupLinks(known, missed)` ตาม §2 และเรียกใน goroutine
  ของ `Start()` **ทันทีหลัง** `seedKnownLinks()` (`:~102`) ก่อนเข้า select loop
- mock mode: return ก่อนถึง goroutine อยู่แล้ว (`:56-59`) — synthetic path ไม่รันใน mock
- **เสร็จเมื่อ:** คอมไพล์ผ่าน; log message ระบุชัดว่าเป็น synthetic (มีคำว่า startup window)
- **สถานะ:** ✅ ทำเสร็จแล้ว (ยืนยันจากการอ่านโค้ดจริง 2026-07-21 — `netlink_monitor.go:64-
  228`) — **T-06 ด้านล่างจะแก้ไฟล์นี้ต่อ ต้องทำหลัง T-06/T-07 เสร็จเท่านั้นถึงจะถือว่า
  `netlink_monitor.go` นิ่งพร้อม merge**

### Task T-04 — เทสต์ synthetic event
**File:** `backend/internal/service/netlink_monitor_test.go` (แก้ไข — ใช้ helper `collectEvents`/`expectEvent` เดิม)
- เคส: missed name อยู่ใน known → ได้ `InterfaceAdded` พร้อม Up/Running ตรงกับ seed;
  missed name ไม่อยู่ใน known → ไม่มี event; missed ว่าง → ไม่มี event;
  หลัง synthetic แล้ว RTM_NEWLINK จริงของ index นั้นตามมา → ยังถูก dedupe เป็น
  duplicate/LinkChanged ปกติ (known มีอยู่แล้ว ไม่เกิด `InterfaceAdded` ซ้ำ)
- **เสร็จเมื่อ:** `go test ./internal/service/... -run TestNetlinkMonitor` ผ่าน (รวม `-race`)
- **สถานะ:** ✅ ทำเสร็จแล้ว (ยืนยันจากการอ่านโค้ดจริง 2026-07-21 — `netlink_monitor_test.go:
  156-241`) — **T-07 ด้านล่างจะแก้ไฟล์นี้ต่อ** (แก้เทสต์เดิม 1 ตัวที่ขัดกับพฤติกรรมใหม่ +
  เพิ่มเคสใหม่)

### Task T-05 — ต่อสายใน main.go
**File:** `backend/cmd/pigate/main.go` (แก้ไข `:~398`)
- `netlinkMonitor.Start(monitorCtx, ifaceService.StartupSkippedInterfaces())`
- **ห้ามย้ายตำแหน่ง** step 6.5 — synthetic event ต้องยิงหลังทุก startup apply เสร็จ
  และหลัง subscriber ทุกตัวถูก register แล้วเท่านั้น
- **เสร็จเมื่อ:** `go build ./...` + `go test ./...` ผ่านทั้ง repo
- **สถานะ:** ✅ ทำเสร็จแล้ว (ยืนยันจากการอ่านโค้ดจริง 2026-07-21)

> **สิ่งที่ไม่ต้องทำ (T-01–T-05):** ไม่แตะ `event_bus.go`/subscribers (synthetic event ใช้
> contract เดิมเป๊ะ), ไม่มี kernel capability ใหม่ (ไม่ต้องแก้ `interfaces.go`/`real_*.go`/
> `mock.go`), ไม่มี route/openapi/frontend/migration/install.sh — เพราะทั้งหมดเป็นการเดิน
> สายข้อมูลภายใน service layer ที่มีอยู่แล้ว

### Task T-06 — settling-window classification สำหรับ udev rename race (บั๊กที่สอง)
**File:** `backend/internal/service/netlink_monitor.go` (แก้ไข — sensitive, ดู Caution 9)
- เพิ่ม field `settling bool` ใน `linkState` (`:38-42`) พร้อม doc comment ตาม §2.1
- เพิ่มเมธอด `func (a linkState) sameAttrs(b linkState) bool` เทียบเฉพาะ
  `name`/`up`/`running` **แทนที่** การเทียบ `prev == newState` ที่ `:189` ทั้งหมด —
  **ข้อควรระวังสำคัญที่ยืนยันแล้วจากการอ่านโค้ด (ไม่ใช่แค่ทฤษฎี):** ถ้ายังปล่อยให้มีจุดไหน
  เทียบ `linkState` สอง ค่าด้วย `==` ตรง ๆ หลังเพิ่ม field `settling` เข้าไปแล้ว
  duplicate-NEWLINK suppression เดิม (`TestNetlinkMonitor_DuplicateNewlinkSuppressed`)
  จะพังเงียบ ๆ (settling ของ prev/newState มักไม่เท่ากัน ทำให้ไม่เคย suppress ได้อีกเลย)
  ตรวจด้วย `grep -n "== newState\|newState ==" internal/service/netlink_monitor.go`
  ต้องไม่เจอผลลัพธ์เหลืออยู่
- ใน branch `!seen` (`:180-184`): set `newState.settling = true` ก่อนเขียนลง `known[idx]`
- เพิ่ม branch ใหม่ **ระหว่าง** duplicate-check (`sameAttrs`) กับ `LinkChanged` เดิม: ถ้า
  `attrs != nil && prev.name != newState.name && prev.settling` → publish
  `InterfaceAdded` (ไม่ใช่ `LinkChanged`) ด้วยชื่อ/flags ใหม่ แล้วเขียน
  `known[idx] = newState` (settling เป็น `false` โดย default ของ struct literal ใหม่)
  — คอมเมนต์อธิบายเหตุผลตาม §2.1 พร้อมอ้างอิง log จริงจาก §1.1
- branch duplicate (`sameAttrs` true) และ branch `LinkChanged` เดิม (ชื่อไม่เปลี่ยนแต่ flag
  เปลี่ยน, หรือ `attrs == nil`) ต้อง reset `settling` เป็น `false` เสมอเมื่อเขียน
  `known[idx]` ใหม่ — เพราะนี่คือ "event ถัดไปหลัง InterfaceAdded" ไม่ว่าจะเป็นอะไรก็ตาม
  ปิด settling window — **ข้อควรระวัง (ยืนยันจากการอ่านโค้ดจริง 2026-07-21):** branch
  duplicate ปัจจุบัน (`sameAttrs` true) แค่ `log` แล้ว `return` ทันที **ไม่เขียน**
  `known[idx]` เลย นี่คือจุดที่ต้อง**เพิ่มการเขียนเข้าไปใหม่** ไม่ใช่แค่เปลี่ยนตัวเปรียบเทียบ
  จาก `==` เป็น `sameAttrs()` — ต้องเปลี่ยน branch นี้ให้เขียน
  `known[idx] = linkState{name: newState.name, up: newState.up, running: newState.running, settling: false}`
  ก่อน `return` เสมอ มิฉะนั้น `prev.settling` ที่ค้างเป็น `true` มาจากตอน `!seen` จะทำให้
  rename ที่ตามมาไป match เงื่อนไข `prev.settling` ผิด (จะได้ `InterfaceAdded` ทั้งที่ควร
  เป็น `LinkChanged`) — ดู `TestNetlinkMonitor_DuplicateThenRenameIsLinkChanged` ใน T-07
  ที่เขียนไว้ป้องกันเคสนี้โดยตรง
- แก้ `publishMissedStartupLinks` (`:218-228`): เปลี่ยน loop ด้านในจาก
  `for _, st := range known` เป็น `for idx, st := range known` แล้ว set
  `st.settling = true; known[idx] = st` ก่อน publish — **จำเป็นต้องทำ** (ไม่ใช่ optional):
  ปิดเคส compound race ที่ synthetic `InterfaceAdded` ของ T-03 (#76) กับ udev rename
  เกิดคาบเกี่ยวกัน และ T-07 มีเทสต์
  `TestNetlinkMonitor_PublishMissedStartupLinks_ThenRenameIsInterfaceAdded` ที่ **บังคับ**
  ให้พฤติกรรมนี้ผ่าน — ถ้าข้ามขั้นตอนนี้ เทสต์ดังกล่าวจะแดง (แก้ไขจากร่างเดิมที่เขียนว่า
  "defense-in-depth ไม่ใช่โจทย์บังคับ" ซึ่งขัดกับเกณฑ์ผ่านของ T-07 เอง — ตรวจพบจาก
  tech-lead review 2026-07-21)
- **เสร็จเมื่อ:** คอมไพล์ผ่าน; `go vet` สะอาด; grep ยืนยันไม่มีจุดไหนเทียบ `linkState`
  ด้วย `==` ตรง ๆ เหลืออยู่แล้ว
- **depends_on:** T-03 (แก้ไฟล์เดียวกัน ต้องทำหลัง state ของ T-03 นิ่งแล้ว)

### Task T-07 — เทสต์ settling window + แก้เทสต์เดิมที่ขัดกับพฤติกรรมใหม่
**File:** `backend/internal/service/netlink_monitor_test.go` (แก้ไข)
- **แก้ `TestNetlinkMonitor_RenameSameFlagsPublishes` (`:108-128`):** เปลี่ยนความคาดหวัง
  event ที่สองจาก `LinkChanged` เป็น `InterfaceAdded` (ชื่อยังเป็น `"eth1"` เหมือนเดิม) —
  เทสต์นี้คือ shape เดียวกับบั๊กจริงใน §1.1 เป๊ะ (`InterfaceAdded` ตามด้วย rename ทันทีบน
  index เดิม) เดิมมันเขียนขึ้นมา encode พฤติกรรมเก่า (ซึ่งคือบั๊กที่กำลังแก้) ไว้โดยไม่ตั้งใจ
  — อัปเดต docstring ของเทสต์ให้ตรงกับพฤติกรรมใหม่ด้วย (ไม่ใช่แค่แก้ assertion)
- เพิ่มเทสต์ใหม่ `TestNetlinkMonitor_RenameAfterSettledIsLinkChanged`: จำลอง "genuine
  rename ของ interface ที่ settle แล้ว" — `InterfaceAdded(eth0)` → `LinkChanged` หนึ่งครั้ง
  (flag เปลี่ยนแต่ชื่อเดิม `eth0`, consume settling) → rename เป็น `eth1` → ต้องได้
  `LinkChanged` (**ไม่ใช่** `InterfaceAdded`) ยืนยันว่า settling ถูก consume ไปแล้วตั้งแต่
  event ที่สอง ไม่ได้ค้างอยู่ตลอดไป — ครอบคลุมโจทย์ "ต้องไม่ trigger InterfaceAdded ปลอมให้
  rename ของ interface ที่รู้จักอยู่แล้ว"
- เพิ่มเทสต์ใหม่ `TestNetlinkMonitor_DuplicateThenRenameIsLinkChanged`: index ใหม่ →
  `InterfaceAdded` → duplicate NEWLINK เป๊ะ (suppressed, ไม่มี event) → rename ตามมา
  ครั้งที่สาม → ต้องได้ `LinkChanged` (ยืนยันว่า duplicate ก็ consume settling เหมือนกัน
  แม้จะไม่ publish อะไรออกมาก็ตาม)
- เพิ่มเทสต์ใหม่ `TestNetlinkMonitor_PublishMissedStartupLinks_ThenRenameIsInterfaceAdded`:
  ต่อยอดจาก `TestNetlinkMonitor_PublishMissedStartupLinks_ThenRealNewlinkIsDeduped` เดิม
  แต่ event ถัดไปหลัง synthetic publish เป็น **rename** (ชื่อเปลี่ยน) แทน flag-only change
  → ต้องได้ `InterfaceAdded` ด้วยชื่อใหม่ (คุม compound race ของ T-03+T-06 ตาม §1.1)
- รัน `go test ./internal/service/... -run TestNetlinkMonitor -race` ต้องผ่านทั้งหมด
  รวมเทสต์เดิมที่ไม่ถูกแก้ (`DuplicateNewlinkSuppressed`, `FlagChangePublishes`,
  `DellinkThenNewlinkIsInterfaceAdded`, `PublishMissedStartupLinks_*` เดิม 3 เคสแรก)
- **เสร็จเมื่อ:** ตามที่ระบุข้างต้นครบ, `go test ./... -race` เขียวทั้ง repo
- **depends_on:** T-06

> **สิ่งที่ไม่ต้องทำ (T-06/T-07):** ไม่แก้ `interface.go` (`ReapplyInterfaceByName` ไม่ต้อง
> เปลี่ยน — ถูกต้องอยู่แล้วเมื่อได้ชื่อ settled จาก monitor), ไม่แก้ `dhcpcd.go` (เหตุผลตาม
> §1.1 — subscribe ทั้ง `InterfaceAdded`/`LinkChanged` อยู่แล้ว ทั้งสอง kind ยังอยู่ใน
> `[]NetEventKind` เดิมของมันหลัง T-06), ไม่แก้ `main.go`/`event_bus.go` (ไม่มี
> `NetEventKind` ใหม่, ไม่มี field ใหม่ใน `NetEvent`), ไม่มี route/openapi/frontend/
> install.sh — การแก้ทั้งหมดจำกัดอยู่ใน `netlink_monitor.go` ไฟล์เดียว (+ เทสต์ของมัน)

## 4. API ที่เกี่ยวข้อง

ไม่มี — ไม่มี route ใหม่/เปลี่ยน ไม่กระทบ `-disable-edit` (ไม่มี mutation ผ่าน HTTP;
เป็น internal self-heal ล้วน) — ครอบคลุมทั้ง T-01–T-05 และ T-06/T-07

## 5. ข้อควรระวัง (Cautions)

1. **ห้ามยิง synthetic ให้ iface ที่ apply สำเร็จแล้ว** — ถ้าเผลอยิงทุกตัวใน DB
   ตอน Start จะเกิด boot re-apply storm (ConfigureWifi ซ้ำ = Wi-Fi reassociate,
   dhcp-server ApplyAll ซ้ำ ฯลฯ) ย้อนบั๊ก issue #48 → เงื่อนไขยิงต้องเป็น
   "อยู่ใน skip-list ∧ อยู่ใน seed" เท่านั้น (T-01 คือแหล่งความจริงของ skip-list)
2. **Flags ของ synthetic event ต้องมาจาก seed จริง** (`st.up/st.running`) —
   ถ้า hardcode `Up:true,Running:true` dhcpcd subscriber จะสั่ง start dhcpcd ให้
   Wi-Fi ที่ยังไม่ associate ผิดลำดับ (ต้องปล่อยให้รอ RUNNING จาก `LinkChanged`
   จริงหลัง wpa associate ตาม logic `dhcpcd.go:93-96`)
3. **Backup restore เรียก `InitApplyConfigurationAtStartup` ซ้ำ** (`backup.go:482`)
   ระหว่างที่ monitor รันไปแล้ว → skip-list ถูกเขียนทับจาก goroutine ของ HTTP handler
   ขณะไม่มีใครอ่านแล้ว (Start อ่านครั้งเดียว) — ไม่มีผลเชิงพฤติกรรม แต่ accessor/การเขียน
   ต้องอยู่ใต้ mutex ให้ `go test -race` สะอาด
4. **Residual race ที่ยอมรับ (T-01–T-05):** link ที่ apply สำเร็จตอน 6.1 แล้วถูกถอด+เสียบใหม่
   *ภายใน* หน้าต่าง 6.1→6.5 พอดี จะถูก seed เป็น known โดย config อาจหาย —
   ไม่อยู่ใน skip-list จึงไม่ได้ synthetic event เคสนี้แคบมาก (ต้อง replug ในไม่กี่วินาที
   ของ boot) และแนว periodic checker ของ #78 จะช่วยรับในอนาคต — บันทึกไว้ ไม่แก้รอบนี้
5. **ทดสอบบนบอร์ดจริงต้องมี physical access** — งานนี้แตะพฤติกรรม boot ของ
   interface/WAN; ถ้า self-heal ทำงานผิดอาจเสีย network ทางเข้าเครื่อง ให้ทดสอบเมื่อ
   เข้าถึงหน้าเครื่อง/จอ-คีย์บอร์ดได้เท่านั้น (mock mode ครอบคลุมได้แค่ unit-level
   เพราะ `Start()` ไม่ subscribe ใน mock — เทสต์ synthetic path ต้องเรียก helper ตรง)
6. **งานนี้แตะย่าน self-heal/netlink → sensitive** ตามนโยบายโปรเจกต์ — PR ต้องผ่าน
   review เข้ม โดยเฉพาะเงื่อนไขการยิง event (ข้อ 1) และ lock ordering (ข้อ 3)
7. **`linkState` struct equality pitfall (T-06, ยืนยันจากการอ่านโค้ดจริง — ไม่ใช่ทฤษฎี):**
   `handleLinkUpdate` เดิมเทียบ `prev == newState` ตรง ๆ ที่ `:189` เพื่อ suppress
   duplicate NEWLINK — การเพิ่ม field `settling` เข้าไปใน `linkState` โดยไม่เปลี่ยนจุดนี้
   จะทำให้ dedupe เดิมพังเงียบ ๆ ทันที (settling ของสอง state มักไม่เท่ากัน) ทุก NEWLINK
   ที่ควรถูก suppress จะกลาย publish `LinkChanged` รัวแทน (ย้อนบั๊กที่
   `TestNetlinkMonitor_DuplicateNewlinkSuppressed` เขียนไว้ป้องกัน) — **ต้อง**เปลี่ยนไปใช้
   `sameAttrs()` ที่เทียบเฉพาะ `name`/`up`/`running` แทนทุกจุดที่เคยเทียบด้วย `==`
8. **Bounded one-shot risk ที่ยอมรับ (T-06):** settling window ถูก consume โดย "event
   ถัดไป" เพียงครั้งเดียว — ถ้ามี flag-only event คั่นระหว่าง creation กับ rename จริง
   (เช่น kernel ส่ง flag transition ก่อน udev ทัน rename) rename ที่ตามมาจะไม่ได้รับการ
   ปฏิบัติพิเศษอีก (ตกกลับไปเป็น `LinkChanged` เหมือนเดิม → กลับไปเจอบั๊กเดิม) — ยอมรับ
   ความเสี่ยงนี้เพราะ udev rename ตามหลักฐาน log จริง (§1.1) เกิดเป็น event ที่สองติดกัน
   เสมอ ไม่มี flag event คั่น และการออกแบบให้ทนทานกว่านี้ (settling ค้างจนกว่าจะมี rename
   ครั้งแรกไม่จำกัดเวลา) จะไปกระทบกรณี genuine rename ของ interface ที่ settle แล้วนานแล้ว
   แทน (ตัวเลือกที่ 4 ใน §2.1 ที่ถูกปฏิเสธ) — ถ้าพบบนบอร์ดจริงว่าเกิดขึ้นบ่อย ให้กลับมา
   ทบทวนแนวทางนี้ใหม่ ไม่ใช่ขยาย window แบบเดา
9. **Regression ที่ต้องแก้พร้อมกัน (T-07):** `TestNetlinkMonitor_RenameSameFlagsPublishes`
   ที่มีอยู่แล้วในโค้ดปัจจุบัน **assert พฤติกรรมเดิม (บั๊ก) ไว้ตรง ๆ** — ต้องแก้เทสต์นี้เป็น
   ส่วนหนึ่งของ T-07 ไม่ใช่แค่เพิ่มเทสต์ใหม่เฉย ๆ มิฉะนั้น T-06 จะทำให้เทสต์เดิมแดง
10. **ไม่ต้องแตะ `interface.go`/`dhcpcd.go`/`main.go` สำหรับ T-06/T-07** (ดู §2.1 และ
    blockquote ท้าย T-07) — ถ้า ai-developer พบว่าอยากแก้ไฟล์เหล่านี้เพิ่มระหว่างทำ ให้
    กลับมาทบทวนกับแผนนี้ก่อน เพราะสถาปัตยกรรมที่เลือกไว้ตั้งใจให้ fix จบที่
    `netlink_monitor.go` ไฟล์เดียว
11. **บอร์ดที่เพิ่ง provision ใหม่คือเงื่อนไขทดสอบที่แท้จริง สำหรับบั๊กที่สอง** (§1.1) —
    ถ้าทดสอบบนบอร์ดที่มี `wpa_supplicant@<iface>.service` config ไฟล์ค้างอยู่แล้วจาก
    session ก่อนหน้า จะเห็น Wi-Fi associate ได้ "ดูเหมือนใช้งานได้" แม้ bug ยังไม่ถูกแก้จริง
    (systemd auto-start จาก device presence ไม่เกี่ยวกับ pigate self-heal เลย) — Final
    Acceptance (§6) ต้องระบุเงื่อนไขทดสอบที่ตัด false positive นี้ออกอย่างชัดเจน

## 6. Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก Task เสร็จ — สำหรับ ai-qa)

- [x] `cd backend && go build ./... && go vet ./... && go test -race ./...` เขียวทั้งหมด
- [x] Unit: เทสต์ใหม่ T-02/T-04/T-07 ผ่านครบทุกเคสที่ระบุ (รวม `-race`)
- [x] เทสต์เดิมทั้งหมดของ `interface_test.go`, `netlink_monitor_test.go`,
      `dhcpcd_test.go`, `backup_test.go` ยังผ่าน (ไม่มี regression จาก signature ใหม่ของ
      T-03 และการเปลี่ยน assertion ของ `TestNetlinkMonitor_RenameSameFlagsPublishes`
      ตาม T-07 ต้องเป็นการเปลี่ยนที่ตั้งใจ ไม่ใช่ผลข้างเคียงที่ไม่ได้ตรวจสอบ)
- [x] grep ยืนยันไม่มีจุดไหนใน `netlink_monitor.go` เทียบ `linkState` ด้วย `==` ตรง ๆ
      เหลืออยู่ (Caution 7)
- [x] บอร์ดจริง (physical access), เคส #76 เดิม: ตั้งค่า USB Wi-Fi ใน DB → reboot ทั้งบอร์ด →
      log ต้องเห็นลำดับ "Skipping." → "publishing synthetic InterfaceAdded" →
      "[Self-heal] Interface ... returned" → Wi-Fi associate + ได้ IP ใช้งานได้
      **โดยไม่ restart pigate service**
- [x] บอร์ดจริง, เคส #76 เดิม: interface ที่ enumerate ทัน (เช่น `wlan1`/`eth0`) พฤติกรรมเดิม
      ทุกอย่าง — **ไม่มี** synthetic event ให้ตัวที่ apply ปกติ (เช็ค log ว่าไม่มี
      InterfaceAdded ซ้ำ)
- [x] บอร์ดจริง, เคส #76 เดิม: reboot ปกติ (ไม่มี USB Wi-Fi ช้า) → ไม่มี re-apply เพิ่มเติม/
      พายุ event ใน log
- [x] **บอร์ดจริงที่เพิ่ง provision ใหม่ (สำคัญมาก — ดู Caution 11)**: ลบ/ไม่มี
      `wpa_supplicant@<iface>.service` config ไฟล์ค้างจาก session ก่อนหน้า (หรือทดสอบด้วย
      SSID/password ที่เพิ่งเปลี่ยนใน DB ให้ต่างจากไฟล์เดิมบนดิสก์) → ต่อ USB Wi-Fi ที่ต้อง
      ผ่าน udev rename (ชื่อ default → ชื่อ MAC-based) → restart service/reboot → log ต้อง
      เห็นลำดับ "Interface added: iface=<default-name>" ตามด้วย branch ใหม่ของ T-06 (ไม่ใช่
      "Link changed:") ที่ publish `InterfaceAdded` ด้วยชื่อ MAC-based สุดท้าย →
      "[Self-heal] Interface <final-name> returned" → `ConfigureWifi` ถูกเรียกจริง (เช็คจาก
      log `[ApplyInterface] Configuring Wi-Fi for interface <final-name>`) → Wi-Fi
      associate + ได้ IP **โดยไม่พึ่ง wpa_supplicant config ไฟล์เดิมที่ค้างอยู่เลย**
- [x] บอร์ดจริง: interface ที่ไม่เคยโดน rename เลย (ethernet ปกติ) ยังคงได้ `InterfaceAdded`
      ครั้งเดียวตามชื่อจริง ไม่มีการ publish ซ้ำซ้อนจาก T-06 (regression check)
- [x] บอร์ดจริง (ถ้าจำลองได้): เคสผสม USB Wi-Fi enumerate ช้าจน miss startup window (#76)
      **และ** โดน udev rename (บั๊กที่สอง) พร้อมกัน → ต้องได้ `InterfaceAdded` เพียงครั้งเดียว
      ด้วยชื่อ final ที่ถูกต้อง ไม่ apply ซ้ำสองรอบ
- [x] Backup import ระหว่างระบบรัน → restore สำเร็จเหมือนเดิม (path `backup.go:482`
      ไม่พังจากการเปลี่ยนแปลง) และไม่มี synthetic event ยิงหลัง restore
- [x] Code บน branch `fix/usb-wifi-startup-race` → PR เข้า `main` (ห้าม push ตรง) — PR นี้
      รวม T-01–T-07 ทั้งหมด (ไม่แยก PR สำหรับบั๊กที่สอง เพราะเป็น branch เดียวกันตามที่ถูก
      hold ไว้) — merged: PR #79, commit `4e2168d`

## 7. Checklist (Definition of Done)

- [x] T-01 `service/interface.go` — skip-list + accessor (signature เดิม)
- [x] T-02 `service/interface_test.go` — เทสต์ skip-list
- [x] T-03 `service/netlink_monitor.go` — `Start(ctx, missed)` + `publishMissedStartupLinks`
- [x] T-04 `service/netlink_monitor_test.go` — เทสต์ synthetic event + dedupe หลัง synthetic
- [x] T-05 `cmd/pigate/main.go` — ต่อสาย Start
- [x] T-06 `service/netlink_monitor.go` — settling window + `sameAttrs` + fix
      `publishMissedStartupLinks` (บั๊กที่สอง, udev rename race)
- [x] T-07 `service/netlink_monitor_test.go` — แก้เทสต์เดิมที่ผิด + เทสต์ settling ใหม่
- [x] Final Acceptance §6 ครบทุกข้อ (รวมข้อใหม่สำหรับบั๊กที่สอง) — ยืนยันทดสอบบอร์ดจริงครบแล้ว 2026-07-22
- [x] ไม่ต้องแก้ openapi/README Feature Status (ไม่มี contract/feature ใหม่) —
      ปิดงานแล้วย้ายไฟล์นี้ไป `docs/ref/complete/`
