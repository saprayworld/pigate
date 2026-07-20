# USB Wi-Fi Startup Race — self-heal interface ที่ enumerate ไม่ทันตอน boot (issue #76)

> แผนงานแก้บั๊ก: หลัง restart ทั้งบอร์ด USB Wi-Fi (เช่น `wlx0cef1548ff2b`) ที่ kernel
> enumerate ช้า จะถูก startup apply ข้าม ("does not exist in kernel. Skipping.") แล้วถูก
> `seedKnownLinks()` นับเป็น "known" ก่อนเคยได้ event ทำให้ไม่มีวันเกิด `InterfaceAdded`
> → self-heal ไม่ทำงาน → config ไม่ถูก apply จนกว่าจะ restart service เอง
>
> เขียนเมื่อ: 2026-07-20 · Reference branch: `main` (แยก `fix/usb-wifi-startup-race`)
> อ้างอิง: GitHub issue #76 (ต่อยอด design จาก issue #48 self-healing event bus)

## 0. เป้าหมายและขอบเขต

- **เป้าหมาย:** reboot บอร์ดที่มี USB Wi-Fi ตั้งค่าไว้ใน DB แล้ว interface นั้นต้องกลับมา
  ใช้งานได้เอง (Wi-Fi associate + IP + dhcpcd/routing/firewall-related re-apply ครบ)
  โดย**ไม่ต้อง restart pigate service** ไม่ว่า interface จะ enumerate ก่อน, ระหว่าง,
  หรือหลังช่วง startup apply ของ pigate
- **เงื่อนไขทางเทคนิค:** interface ที่ startup apply "ข้ามไป" เพราะยังไม่อยู่ใน kernel
  ต้องได้ event `InterfaceAdded` (จริงหรือ synthetic) เสมอเมื่อมันโผล่มา — ปิดหน้าต่าง race
  ระหว่าง `InitApplyConfigurationAtStartup()` (main.go step 6.1) กับ `seedKnownLinks()`
  (ใน `NetlinkMonitor.Start()`, step 6.5)
- **Out of scope:**
  - ไม่ทำ periodic health checker / ตรวจ 169.254 (นั่นคือ issue #78 — ดู
    `docs/ref/todo/dhcpcd-link-local-fallback-notes.md`)
  - ไม่เปลี่ยน design "self-heal หนักเฉพาะ `InterfaceAdded`, ไม่ฟัง `LinkChanged`"
    (กัน re-apply storm ตาม issue #48)
  - ไม่แตะ frontend / API / DB schema / install.sh ใด ๆ

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

**สรุป root cause:** USB Wi-Fi enumerate เสร็จ**ในหน้าต่าง** 6.1→6.5 → ถูกข้ามตอน apply
และถูก seed เป็น known → event แรกที่ได้จริงเป็น `LinkChanged` ไม่ใช่ `InterfaceAdded`
→ self-heal ไม่ยิง งานกระจุกที่ **backend service layer 2 ไฟล์ + main.go** เท่านั้น

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

### Task T-02 — เทสต์การจดรายชื่อ
**File:** `backend/internal/service/interface_test.go` (แก้ไข — ล้อ `TestInitApplyConfigurationAtStartup:77`)
- เคส: DB มี iface ที่ไม่อยู่ใน kernel → ชื่อโผล่ใน `StartupSkippedInterfaces()`;
  iface ที่อยู่ใน kernel → ไม่โผล่; เรียก Init ซ้ำ → slice ถูก reset ไม่สะสม
- **เสร็จเมื่อ:** `go test ./internal/service/... -run TestInitApply` ผ่าน

### Task T-03 — synthetic InterfaceAdded ใน NetlinkMonitor
**File:** `backend/internal/service/netlink_monitor.go` (แก้ไข)
- เปลี่ยน signature เป็น `Start(ctx context.Context, missedAtStartup []string)`
  (call site เดียวคือ main.go — BackupService ใช้แค่ Pause/Resume ไม่กระทบ)
- เพิ่ม `publishMissedStartupLinks(known, missed)` ตาม §2 และเรียกใน goroutine
  ของ `Start()` **ทันทีหลัง** `seedKnownLinks()` (`:~102`) ก่อนเข้า select loop
- mock mode: return ก่อนถึง goroutine อยู่แล้ว (`:56-59`) — synthetic path ไม่รันใน mock
- **เสร็จเมื่อ:** คอมไพล์ผ่าน; log message ระบุชัดว่าเป็น synthetic (มีคำว่า startup window)

### Task T-04 — เทสต์ synthetic event
**File:** `backend/internal/service/netlink_monitor_test.go` (แก้ไข — ใช้ helper `collectEvents`/`expectEvent` เดิม)
- เคส: missed name อยู่ใน known → ได้ `InterfaceAdded` พร้อม Up/Running ตรงกับ seed;
  missed name ไม่อยู่ใน known → ไม่มี event; missed ว่าง → ไม่มี event;
  หลัง synthetic แล้ว RTM_NEWLINK จริงของ index นั้นตามมา → ยังถูก dedupe เป็น
  duplicate/LinkChanged ปกติ (known มีอยู่แล้ว ไม่เกิด `InterfaceAdded` ซ้ำ)
- **เสร็จเมื่อ:** `go test ./internal/service/... -run TestNetlinkMonitor` ผ่าน (รวม `-race`)

### Task T-05 — ต่อสายใน main.go
**File:** `backend/cmd/pigate/main.go` (แก้ไข `:~398`)
- `netlinkMonitor.Start(monitorCtx, ifaceService.StartupSkippedInterfaces())`
- **ห้ามย้ายตำแหน่ง** step 6.5 — synthetic event ต้องยิงหลังทุก startup apply เสร็จ
  และหลัง subscriber ทุกตัวถูก register แล้วเท่านั้น
- **เสร็จเมื่อ:** `go build ./...` + `go test ./...` ผ่านทั้ง repo

> **สิ่งที่ไม่ต้องทำ:** ไม่แตะ `event_bus.go`/subscribers (synthetic event ใช้ contract
> เดิมเป๊ะ), ไม่มี kernel capability ใหม่ (ไม่ต้องแก้ `interfaces.go`/`real_*.go`/`mock.go`),
> ไม่มี route/openapi/frontend/migration/install.sh — เพราะทั้งหมดเป็นการเดินสาย
> ข้อมูลภายใน service layer ที่มีอยู่แล้ว

## 4. API ที่เกี่ยวข้อง

ไม่มี — ไม่มี route ใหม่/เปลี่ยน ไม่กระทบ `-disable-edit` (ไม่มี mutation ผ่าน HTTP;
เป็น internal self-heal ล้วน)

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
4. **Residual race ที่ยอมรับ:** link ที่ apply สำเร็จตอน 6.1 แล้วถูกถอด+เสียบใหม่
   *ภายใน* หน้าต่าง 6.1→6.5 พอดี จะถูก seed เป็น known โดย config อาจหาย —
   ไม่อยู่ใน skip-list จึงไม่ได้ synthetic event เคสนี้แคบมาก (ต้อง replug ในไม่กี่วินาที
   ของ boot) และแนว periodic checker ของ #78 จะช่วยรับในอนาคต — บันทึกไว้ ไม่แก้รอบนี้
5. **ทดสอบบนบอร์ดจริงต้องมี physical access** — งานนี้แตะพฤติกรรม boot ของ
   interface/WAN; ถ้า self-heal ทำงานผิดอาจเสีย network ทางเข้าเครื่อง ให้ทดสอบเมื่อ
   เข้าถึงหน้าเครื่อง/จอ-คีย์บอร์ดได้เท่านั้น (mock mode ครอบคลุมได้แค่ unit-level
   เพราะ `Start()` ไม่ subscribe ใน mock — เทสต์ synthetic path ต้องเรียก helper ตรง)
6. **งานนี้แตะย่าน self-heal/netlink → sensitive** ตามนโยบายโปรเจกต์ — PR ต้องผ่าน
   review เข้ม โดยเฉพาะเงื่อนไขการยิง event (ข้อ 1) และ lock ordering (ข้อ 3)

## 6. Final Acceptance (ทดสอบรวมครั้งเดียวหลังทุก Task เสร็จ — สำหรับ ai-qa)

- [ ] `cd backend && go build ./... && go vet ./... && go test -race ./...` เขียวทั้งหมด
- [ ] Unit: เทสต์ใหม่ T-02/T-04 ผ่านครบทุกเคสที่ระบุ
- [ ] เทสต์เดิมทั้งหมดของ `interface_test.go`, `netlink_monitor_test.go`,
      `dhcpcd_test.go`, `backup_test.go` ยังผ่าน (ไม่มี regression จาก signature ใหม่)
- [ ] บอร์ดจริง (physical access): ตั้งค่า USB Wi-Fi ใน DB → reboot ทั้งบอร์ด →
      log ต้องเห็นลำดับ "Skipping." → "publishing synthetic InterfaceAdded" →
      "[Self-heal] Interface ... returned" → Wi-Fi associate + ได้ IP ใช้งานได้
      **โดยไม่ restart pigate service**
- [ ] บอร์ดจริง: interface ที่ enumerate ทัน (เช่น `wlan1`/`eth0`) พฤติกรรมเดิมทุกอย่าง —
      **ไม่มี** synthetic event ให้ตัวที่ apply ปกติ (เช็ค log ว่าไม่มี InterfaceAdded ซ้ำ)
- [ ] บอร์ดจริง: reboot ปกติ (ไม่มี USB Wi-Fi ช้า) → ไม่มี re-apply เพิ่มเติม/พายุ event ใน log
- [ ] Backup import ระหว่างระบบรัน → restore สำเร็จเหมือนเดิม (path `backup.go:482`
      ไม่พังจากการเปลี่ยนแปลง) และไม่มี synthetic event ยิงหลัง restore
- [ ] Code บน branch `fix/usb-wifi-startup-race` → PR เข้า `main` (ห้าม push ตรง)

## 7. Checklist (Definition of Done)

- [ ] T-01 `service/interface.go` — skip-list + accessor (signature เดิม)
- [ ] T-02 `service/interface_test.go` — เทสต์ skip-list
- [ ] T-03 `service/netlink_monitor.go` — `Start(ctx, missed)` + `publishMissedStartupLinks`
- [ ] T-04 `service/netlink_monitor_test.go` — เทสต์ synthetic event + dedupe หลัง synthetic
- [ ] T-05 `cmd/pigate/main.go` — ต่อสาย Start
- [ ] Final Acceptance §6 ครบทุกข้อ
- [ ] ไม่ต้องแก้ openapi/README Feature Status (ไม่มี contract/feature ใหม่) —
      ปิดงานแล้วย้ายไฟล์นี้ไป `docs/ref/complete/`
