# Dashboard System Status Design — Backend ดึงค่า System Information / System Status จริง

> เอกสารออกแบบสำหรับฟีเจอร์: ทำให้ Dashboard แสดงข้อมูลจริงจากระบบ
> (CPU / Memory / Temperature / Storage / Uptime / OS / Bandwidth history)
> แทนค่า mock ที่ hardcode อยู่ทั้งใน frontend (`Dashboard.tsx`) และ backend
> (`HandleGetPerformanceMetrics`)
>
> วันที่เขียน: 2026-07-06 · Branch อ้างอิง: `feat/dashboard-redesign`

---

## 0. เป้าหมายและขอบเขต

`frontend/src/pages/Dashboard.tsx` (โฉมใหม่) ต้องการข้อมูลต่อไปนี้ ซึ่งปัจจุบัน
**เป็น mock ทั้งหมดในฝั่ง frontend** (ไม่เรียก API เลยแม้แต่เส้นเดียว):

| Widget บน Dashboard | ข้อมูลที่ต้องใช้ | สถานะ backend ปัจจุบัน |
|---|---|---|
| StatCard: CPU | usage %, จำนวน core, รุ่น CPU, ความถี่, อุณหภูมิ | ❌ simulate (`handlers.go:282` คืนค่าคงที่ 15.4) |
| StatCard: Memory | used / total (bytes), % | ❌ simulate |
| StatCard: Temperature | อุณหภูมิ SoC, เกณฑ์ throttle | ❌ simulate |
| StatCard: Storage | used / total ของ root filesystem, % | ❌ ไม่มีเลย |
| Bandwidth · last 24h | ประวัติ rx/tx เป็น bucket ย้อนหลัง 24 ชม. | ❌ ไม่มีเลย (`stats` คืน string hardcode "8.7 GB") |
| System Information | hostname, เวอร์ชัน PiGate, OS base, uptime, เวลาระบบ | ⚠️ มี `/api/system/hostname`, `/api/system/time` แยกอยู่แล้ว แต่ไม่มี uptime / OS / version |
| Interfaces | ชื่อ, บทบาท (WAN/LAN), สถานะ up/down | ✅ มีแล้ว — ใช้ `/api/interfaces` เดิมได้เลย ไม่ต้องทำใหม่ |
| Recent Alerts | log เหตุการณ์ล่าสุด | ✅ มีแล้ว — ใช้ `/api/dashboard/logs` (ring buffer) เดิมได้ |

**ขอบเขตงานนี้ = ทำ 6 แถวแรกให้เป็นข้อมูลจริง** ส่วน Interfaces / Alerts เป็นแค่งาน wiring ฝั่ง frontend

นอกขอบเขต (ไว้ทีหลัง): system alert จริงจากเหตุการณ์ระบบ (เช่น temp เกินเกณฑ์),
per-client bandwidth, ประวัติ traffic ที่อยู่รอดข้าม reboot

---

## 1. แหล่งข้อมูลจริง (ไม่มี shell exec — อ่านไฟล์ /proc, /sys และ Netlink เท่านั้น)

กฎโปรเจกต์ห้าม `exec.Command` (ห้ามแม้แต่ `vcgencmd`) — ข้อมูลทุกตัวหาได้จาก
การอ่านไฟล์ kernel-virtual filesystem ตรง ๆ ซึ่งปลอดภัยและไม่ต้องการสิทธิ์เพิ่ม:

| ข้อมูล | แหล่ง | ใช้ได้บน WSL? |
|---|---|---|
| CPU usage % | `/proc/stat` (คำนวณ delta jiffies ระหว่าง 2 จุดเวลา) | ✅ |
| CPU model / cores | `/proc/cpuinfo` + `runtime.NumCPU()` | ✅ |
| CPU freq ปัจจุบัน | `/sys/devices/system/cpu/cpu0/cpufreq/scaling_cur_freq` | ⚠️ มักไม่มี → ต้อง optional |
| อุณหภูมิ SoC | `/sys/class/thermal/thermal_zone0/temp` (หน่วย m°C) | ❌ มักไม่มี → ต้อง optional |
| Memory | `/proc/meminfo` (`MemTotal`, `MemAvailable`) | ✅ |
| Storage (root fs) | `unix.Statfs("/")` จาก `golang.org/x/sys/unix` (มีใน go.mod แล้ว) | ✅ |
| Uptime | `/proc/uptime` | ✅ |
| OS base | `/etc/os-release` (`PRETTY_NAME`) | ✅ |
| Board model (เช่น "Raspberry Pi 5") | `/proc/device-tree/model` | ❌ ไม่มีบน x86 → optional |
| Traffic counters (rx/tx bytes ต่อ iface) | `vishvananda/netlink` → `LinkAttrs.Statistics` (มีใน go.mod แล้ว) | ✅ |
| Hostname | ใช้ `HostnameManager` (D-Bus hostnamed) ที่มีอยู่แล้ว / fallback `os.Hostname()` | ✅ |
| เวลาระบบ + timezone | ใช้ `TimeManager` ที่มีอยู่แล้ว / `time.Now()` | ✅ |
| เวอร์ชัน PiGate | ตัวแปร build-time ผ่าน `-ldflags "-X main.version=..."` ใน `build.sh` | ✅ |

> ค่าที่ "optional" ให้ตอบเป็น field ที่มี flag `available: false` หรือ omit ไป
> ห้าม error ทั้ง response เพียงเพราะไฟล์ sysfs ตัวใดตัวหนึ่งไม่มี — ไม่งั้นเทสบน WSL ไม่ได้เลย

---

## 2. สถาปัตยกรรม (ตาม layering เดิมของโปรเจกต์)

```
api/handlers.go ── system_status service ── kernel.SystemStatsManager
                        │                      ├─ real_system_stats.go  (อ่าน /proc, /sys, netlink)
                        │                      └─ mock.go               (ค่า simulate แกว่งเบา ๆ)
                        ├─ CPU sampler (background ticker, cache ค่าไว้ให้ handler)
                        └─ Traffic collector (background ticker + ring buffer ใน RAM)
```

### 2.1 kernel layer — interface ใหม่ `SystemStatsManager`

เพิ่มใน `backend/internal/kernel/interfaces.go`:

```go
// SystemStatsManager abstracts host telemetry reads (/proc, /sys, statfs, netlink
// counters). Read-only: no method mutates system state.
type SystemStatsManager interface {
    GetCPUSnapshot() (*model.CPUSnapshot, error)   // raw jiffies จาก /proc/stat (service เอาไปคิด delta เอง)
    GetCPUInfo() (*model.CPUInfo, error)           // model name, cores, freq (freq optional)
    GetMemoryInfo() (*model.MemoryInfo, error)     // total/available bytes
    GetTemperature() (*model.TemperatureInfo, error) // Available=false เมื่อไม่มี thermal zone
    GetDiskUsage(path string) (*model.DiskUsage, error)
    GetHostInfo() (*model.HostInfo, error)         // os-release, uptime, board model, kernel version
    GetNetCounters() (map[string]model.NetCounters, error) // iface -> rx/tx bytes สะสม
}
```

- **`real_system_stats.go`** (ไฟล์ใหม่) — implement โดยอ่านไฟล์ตามตาราง §1
  ให้ struct รับ path ราก (`procRoot`, `sysRoot`, `etcRoot`) เป็น field ที่ default เป็น
  `/proc`, `/sys`, `/etc` เพื่อให้ unit test ชี้ไป fixture directory ได้
- **`mock.go`** — เพิ่ม mock implementation คืนค่า simulate ที่แกว่งตามเวลา
  (คล้าย pattern เดิมใน `dashboardService.ts` ฝั่ง mock) เพื่อให้ dev บน WSL เห็น dashboard ขยับ

### 2.2 service layer — ไฟล์ใหม่ `backend/internal/service/system_status.go`

`SystemStatusService` มีหน้าที่:

1. **CPU sampler**: goroutine ticker ทุก ~3 วินาที เก็บ `CPUSnapshot` คู่ล่าสุด
   แล้วคำนวณ usage % cache ไว้ (คุ้มครองด้วย `sync.RWMutex`)
   — handler ห้ามคำนวณเองเพราะต้อง sleep ระหว่าง 2 sample ซึ่งจะ block request
2. **Traffic collector**: goroutine ticker ทุก ~10 วินาที อ่าน `GetNetCounters()`
   ของ interface ฝั่ง WAN (ดูจาก role ใน DB ผ่าน repository; fallback = ทุก iface
   ที่ไม่ใช่ loopback) คิด delta สะสมลง **ring buffer ใน RAM**
   (bucket ละ 5 นาที × 288 ช่อง = 24 ชม. — ตาม pattern `logs/ringbuffer.go`,
   **ห้ามลง SQLite** เพื่อถนอม SD card ตาม tech_stack_design.md §8)
3. ประกอบ DTO สำหรับแต่ละ endpoint (`GetSystemMetrics()`, `GetSystemInfo()`,
   `GetTrafficHistory()`, `GetTrafficTotals()`)
4. รูปแบบ lifecycle: มี `Start(ctx)` เรียกจาก `main.go` หลังสร้าง service
   (ตำแหน่งเดียวกับที่ start `netlink_monitor`)

### 2.3 model layer — เพิ่ม struct ใน `backend/internal/model/types.go`

`CPUSnapshot`, `CPUInfo`, `MemoryInfo`, `TemperatureInfo`, `DiskUsage`,
`HostInfo`, `NetCounters`, `SystemMetrics`, `SystemInfo`, `TrafficBucket`
(รายละเอียด JSON ดู §3)

---

## 3. API ที่จะมี (เส้นใหม่ + เส้นที่อัปเกรด)

ทุกเส้นลงทะเบียนผ่าน `authRoute(...)` ใน `router.go` (ต้องผ่าน auth middleware เหมือนเส้นอื่น)

### 3.1 `GET /api/dashboard/performance` — **อัปเกรดของเดิม** (ข้อมูลจริง + field เพิ่ม)

**สำคัญ: คง field แบน `cpu`, `memory`, `temp` เดิมไว้** เพื่อ backward-compat กับ
`dashboardService.ts` ปัจจุบัน แล้วเพิ่ม object รายละเอียด:

```json
{
  "cpu": 34.2, "memory": 63.7, "temp": 58.1,
  "cpuDetail":  { "usagePercent": 34.2, "cores": 4, "modelName": "Cortex-A76", "freqMhz": 2400, "freqAvailable": true },
  "memDetail":  { "usedBytes": 5476083712, "totalBytes": 8589934592, "percent": 63.7 },
  "tempDetail": { "celsius": 58.1, "throttleCelsius": 80, "available": true },
  "storage":    { "path": "/", "usedBytes": 44023414784, "totalBytes": 137438953472, "percent": 32.0 }
}
```

### 3.2 `GET /api/system/info` — **เส้นใหม่** (System Information card)

```json
{
  "hostname": "PiGate-RPI5",
  "version": "2.3.2",
  "osName": "Raspberry Pi OS (64-bit)",
  "boardModel": "Raspberry Pi 5 Model B Rev 1.0",
  "kernelVersion": "6.6.31-v8+",
  "uptimeSeconds": 273153,
  "systemTime": "2026-07-06T09:41:00+07:00",
  "timezone": "Asia/Bangkok"
}
```

- hostname ดึงผ่าน `HostnameService` เดิม / เวลา+timezone ผ่าน `TimeService` เดิม — **ห้าม implement ซ้ำ**
- `uptimeSeconds` ส่งเป็นตัวเลข ให้ frontend เดินนาฬิกาเองระหว่าง poll (แบบที่ mockup ทำอยู่)
- `boardModel`, `kernelVersion` omit ได้ถ้าอ่านไม่ได้ (WSL)

### 3.3 `GET /api/dashboard/traffic` — **เส้นใหม่** (Bandwidth chart)

```json
{
  "interfaces": ["eth0"],
  "buckets": [
    { "ts": "2026-07-06T09:00:00+07:00", "rxBytes": 123456789, "txBytes": 2345678 }
  ]
}
```

- คืน bucket เท่าที่เก็บได้ตั้งแต่ boot (เพิ่งเปิดเครื่อง = ช่องน้อย เป็นเรื่องปกติ ให้ frontend รับมือ)
- frontend เอาไป aggregate เป็นรายชั่วโมงเองสำหรับกราฟ 24h

### 3.4 `GET /api/dashboard/stats` — **แก้ของเดิม**

เปลี่ยน `TotalTrafficIn`/`TotalTrafficOut` จาก string hardcode `"8.7 GB"` เป็นค่าสะสมจริง
จาก traffic collector (แนะนำเปลี่ยน field เป็นตัวเลข bytes เช่น `totalTrafficInBytes`
แล้วให้ frontend format เอง — ต้องแก้ `dashboardService.ts` ให้ตรงกันในเฟสเดียวกัน)

---

## 4. แผนงานเป็นขั้นตอน (ทำทีละเฟส แต่ละเฟส build/test ผ่านก่อนไปต่อ)

### Phase 1 — Model + Kernel layer
| # | งาน | ไฟล์ |
|---|---|---|
| 1.1 | เพิ่ม struct ทั้งหมดตาม §2.3 | `backend/internal/model/types.go` |
| 1.2 | เพิ่ม interface `SystemStatsManager` | `backend/internal/kernel/interfaces.go` |
| 1.3 | สร้าง real implementation (อ่าน /proc, /sys, statfs, netlink) + รองรับ path override เพื่อเทส | `backend/internal/kernel/real_system_stats.go` (ใหม่) |
| 1.4 | เพิ่ม mock implementation (ค่าแกว่งตามเวลา) | `backend/internal/kernel/mock.go` |
| 1.5 | unit test parser ด้วย fixture (/proc/stat, meminfo, os-release ปลอม) | `backend/internal/kernel/real_system_stats_test.go` (ใหม่) |

### Phase 2 — Service layer
| # | งาน | ไฟล์ |
|---|---|---|
| 2.1 | สร้าง `SystemStatusService`: CPU sampler + traffic ring buffer + DTO assembler + `Start(ctx)` | `backend/internal/service/system_status.go` (ใหม่) |
| 2.2 | unit test: delta CPU, bucket rollover, counter reset (delta ติดลบ → นับเป็น 0) | `backend/internal/service/system_status_test.go` (ใหม่) |

### Phase 3 — API + wiring
| # | งาน | ไฟล์ |
|---|---|---|
| 3.1 | แทนที่ `HandleGetPerformanceMetrics` (simulate) ด้วยข้อมูลจริง + เพิ่ม `HandleGetSystemInfo`, `HandleGetTrafficHistory` + แก้ `HandleGetDashboardStats` (traffic จริง) | `backend/internal/api/handlers.go` |
| 3.2 | ลงทะเบียน route ใหม่ (`GET /api/system/info`, `GET /api/dashboard/traffic`) | `backend/internal/api/router.go` |
| 3.3 | ส่ง `SystemStatusService` เข้า `api.NewServer(...)` | `backend/internal/api/*.go`, `backend/cmd/pigate/main.go` |
| 3.4 | main.go: เลือก real/mock ตาม flag `-mock`, สร้าง service, เรียก `Start(ctx)` หลัง netlink monitor | `backend/cmd/pigate/main.go` |
| 3.5 | เพิ่มตัวแปร version + `-ldflags "-X main.version=$VERSION"` | `backend/cmd/pigate/main.go`, `build.sh` |
| 3.6 | handler test | `backend/internal/api/handlers_test.go` |

### Phase 4 — เอกสาร/สัญญา API
| # | งาน | ไฟล์ |
|---|---|---|
| 4.1 | เพิ่ม/แก้ spec 4 เส้นตาม §3 | `docs/openapi.yaml` + copy ไป `frontend/public/openapi.yaml` |
| 4.2 | อัปเดตตาราง Feature Status (Dashboard: Partial → ระบุว่าเหลืออะไร) | `README.md`, `docs/project_status.md` |

### Phase 5 — Frontend wiring
| # | งาน | ไฟล์ |
|---|---|---|
| 5.1 | เพิ่ม `getSystemInfo()`, `getTrafficHistory()` + ขยาย type `PerformanceMetrics` + mock mode ให้ครบทุกเส้น | `frontend/src/services/dashboardService.ts` (หรือย้ายส่วน system ไป `systemService.ts`) |
| 5.2 | แปลง `Dashboard.tsx` จาก mockup → เรียก service จริง: StatCards + SystemInfoCard + BandwidthCard poll ตาม interval, InterfacesCard ใช้ `interfaceService`, AlertsCard ใช้ `getRecentLogs()` | `frontend/src/pages/Dashboard.tsx` |
| 5.3 | `yarn lint` + `yarn build` ผ่าน | `frontend/` |

---

## 5. ข้อควรระวัง

1. **ห้าม shell exec เด็ดขาด** — อย่าเผลอใช้ `vcgencmd measure_temp` หรือ `df`;
   ทุกอย่างอ่านจากไฟล์ /proc//sys หรือ syscall ตาม §1 เท่านั้น
2. **WSL ไม่มี thermal zone / cpufreq / device-tree** — real implementation ต้อง
   degrade อย่างสุภาพ (คืน `available:false` หรือ omit field) ห้าม error ทั้งก้อน
   ห้าม log spam ทุก tick (log ครั้งเดียวตอน start พอ)
3. **CPU % ต้องใช้ 2 sample** — คำนวณใน background sampler เท่านั้น
   handler อ่านค่า cache ผ่าน mutex; ห้าม `time.Sleep` ใน request path
4. **SD card wear** — ประวัติ traffic เก็บใน RAM ring buffer เท่านั้น ห้ามเขียน SQLite
   ทุก tick (ยอมรับว่าข้อมูลหายเมื่อ reboot — เหมือน firewall logs)
5. **Counter ของ interface reset ได้** (iface ถูก re-create, ค่า wrap) —
   delta ติดลบให้นับเป็น 0 อย่าปล่อยให้กราฟติดลบหรือพุ่งผิดปกติ
6. **Goroutine lifecycle** — sampler/collector ต้องรับ `context.Context` และหยุดเมื่อ
   shutdown; ระวัง data race → ป้องกันด้วย `sync.RWMutex` แล้วรัน `go test -race`
7. **อย่า implement hostname/time ซ้ำ** — `/api/system/info` ให้ compose จาก
   `HostnameService` / `TimeService` ที่มีอยู่ (มี D-Bus + mock ครบแล้ว)
8. **Backward compatibility** — `/api/dashboard/performance` ต้องคง field
   `cpu`/`memory`/`temp` แบนไว้จนกว่า frontend จะ migrate เสร็จ (Phase 5)
9. **Mock parity** — ทุก method ใน interface ใหม่ต้องมีทั้ง real และ mock
   ไม่งั้น `-mock=true` (โหมดหลักที่ใช้ dev บน WSL) จะพัง
10. **การทดสอบบน WSL**: `/proc/stat`, `/proc/meminfo`, `statfs`, netlink counters
    ใช้ได้จริงบน WSL → รัน real mode แบบ read-only เทสได้บางส่วน
    ส่วน temp/freq/board ต้องรอเทสบนเครื่องจริง (ผู้ใช้เป็นคนเทสเอง)
11. **Rate/ความถี่ polling ฝั่ง frontend** — metrics poll ทุก ~5 วิ, system info ทุก ~30 วิ,
    traffic ทุก ~60 วิ ก็พอ อย่า poll ถี่จน rate-limit middleware สะดุด

---

## 6. เกณฑ์เสร็จ (Definition of Done)

- [ ] `go test ./...` และ `go test -race ./internal/service/...` ผ่าน
- [ ] `go build` ผ่าน; รัน `-mock=true` แล้ว endpoint ทั้ง 4 เส้นคืนค่า simulate ที่ขยับ
- [ ] รัน real mode บน WSL: CPU/Memory/Storage/Uptime/OS เป็นค่าจริง, temp คืน `available:false` โดยไม่ error
- [ ] `yarn lint` + `yarn build` ผ่าน; Dashboard แสดงข้อมูลจริง (mock mode ก็ยังใช้งานได้)
- [ ] openapi.yaml ตรงกับ response จริง
- [ ] เทสบนเครื่อง Raspberry Pi จริง (ผู้ใช้ดำเนินการ): temp/freq/board model แสดงถูกต้อง
