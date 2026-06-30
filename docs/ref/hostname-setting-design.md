# Design: Setting — Hostname

## Requirements (จาก user_ref.md)

1. **Hostname** — กำหนด/แก้ไข Hostname ของเครื่อง
2. **Share hostname with DHCP client** — Toggle ให้ `dhcpcd` (DHCP Client) ส่ง hostname ของ PiGate ไปบอก Router ฝั่ง WAN

> **หมายเหตุ:** "Share hostname with DHCP client" หมายถึง PiGate ในฐานะ **DHCP Client**
> ส่งชื่อตัวเองไปบอก Router ผ่าน DHCP Option 12 ควบคุมผ่าน `/etc/dhcpcd.conf`
> ไม่เกี่ยวกับ DHCP Server ที่แจก IP ให้ Client ในเครือข่าย LAN

---

## สถานะปัจจุบัน

| จุด | สถานะ |
|-----|--------|
| Backend API `/system/hostname` | ❌ ยังไม่มี |
| Frontend Service (`systemService.ts`) | ❌ ยังไม่มี |
| Frontend UI (SettingsMaintenance.tsx) | ❌ ยังไม่มี Section สำหรับ Hostname |
| Dashboard แสดง Hostname | ⚠️ มีอยู่แต่ค่า Hardcode `"PiGate-RPI5"` (บรรทัด 679) |
| DhcpcdService — ApplyHostnameConfig | ❌ ยังไม่มี method นี้ |

---

## ไฟล์ที่ต้องแก้ไข

### Backend (Go)

| ไฟล์ | งาน |
|------|-----|
| `backend/internal/db/connection.go` | เพิ่ม table `system_hostname_settings` + Migration |
| `backend/internal/model/types.go` | เพิ่ม struct `SystemHostnameSettings` |
| `backend/internal/api/handlers.go` | เพิ่ม `HandleGetHostname`, `HandleUpdateHostname` |
| `backend/internal/api/router.go` | Register `GET /system/hostname`, `PUT /system/hostname` |
| `backend/internal/service/dhcpcd.go` | เพิ่ม method `ApplyHostnameConfig(shareWithDhcp bool)` |

### Frontend (React/TypeScript)

| ไฟล์ | งาน |
|------|-----|
| `frontend/src/services/systemService.ts` | เพิ่ม `getHostname()`, `updateHostname()` + Mock |
| `frontend/src/pages/SettingsMaintenance.tsx` | เพิ่ม Card "System Identity" |
| `frontend/src/pages/Dashboard.tsx` | แก้ Hardcode hostname → ดึงจาก API จริง |

---

## รายละเอียดการ Implement

### 1. Database Schema

เพิ่มใน `db/connection.go` ในกลุ่ม `queries []string` (pattern เดียวกับ `system_time_settings`):

```sql
CREATE TABLE IF NOT EXISTS system_hostname_settings (
    id              INTEGER PRIMARY KEY CHECK(id = 1),
    hostname        TEXT NOT NULL DEFAULT 'PiGate-RPI5',
    share_with_dhcp INTEGER DEFAULT 0 CHECK(share_with_dhcp IN (0, 1))
);
```

---

### 2. Model

เพิ่มใน `model/types.go`:

```go
type SystemHostnameSettings struct {
    Hostname      string `json:"hostname"`
    ShareWithDhcp bool   `json:"shareWithDhcp"`
}
```

**Validation rule (RFC 1123):**
- ตัวอักษร `a-z`, `A-Z`, ตัวเลข `0-9` และขีด `-` เท่านั้น
- ห้ามขึ้นต้นหรือลงท้ายด้วย `-`
- ความยาวไม่เกิน 63 ตัวอักษร
- Pattern: `^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`

---

### 3. API Handlers

เพิ่มใน `api/handlers.go`:

```go
// GET /api/system/hostname
func (h *Handler) HandleGetHostname(w http.ResponseWriter, r *http.Request) {
    settings, err := h.repo.GetHostnameSettings()
    // ...
    json.NewEncoder(w).Encode(settings)
}

// PUT /api/system/hostname
func (h *Handler) HandleUpdateHostname(w http.ResponseWriter, r *http.Request) {
    var req model.SystemHostnameSettings
    // 1. Decode body
    // 2. Validate RFC 1123
    // 3. บันทึก SQLite
    // 4. เขียน /etc/hostname (atomic write)
    // 5. hostnamectl set-hostname <name>
    // 6. h.dhcpcdService.ApplyHostnameConfig(req.ShareWithDhcp)
    json.NewEncoder(w).Encode(req)
}
```

---

### 4. DhcpcdService — ApplyHostnameConfig

เพิ่มใน `service/dhcpcd.go`:

ทำหน้าที่ patch `/etc/dhcpcd.conf` แล้ว reconfigure dhcpcd ให้ค่าใหม่มีผลทันที

```go
func (s *DhcpcdService) ApplyHostnameConfig(shareWithDhcp bool) error {
    if s.repo.IsMockMode() {
        log.Printf("[DhcpcdService] [Mock] ApplyHostnameConfig shareWithDhcp=%v", shareWithDhcp)
        return nil
    }

    // อ่าน /etc/dhcpcd.conf
    content, err := os.ReadFile("/etc/dhcpcd.conf")
    if err != nil {
        return fmt.Errorf("failed to read dhcpcd.conf: %w", err)
    }

    // ลบบรรทัดที่เกี่ยวกับ hostname option เดิมออก
    lines := strings.Split(string(content), "\n")
    filtered := []string{}
    for _, line := range lines {
        trimmed := strings.TrimSpace(line)
        if trimmed == "hostname" || trimmed == "nohook hostname" {
            continue
        }
        filtered = append(filtered, line)
    }

    // เพิ่ม option ใหม่
    if shareWithDhcp {
        filtered = append(filtered, "hostname")
    } else {
        filtered = append(filtered, "nohook hostname")
    }

    newContent := strings.Join(filtered, "\n")

    // Atomic write: เขียน temp file แล้ว rename
    tmpFile := "/etc/dhcpcd.conf.tmp"
    if err := os.WriteFile(tmpFile, []byte(newContent), 0644); err != nil {
        return fmt.Errorf("failed to write temp dhcpcd.conf: %w", err)
    }
    if err := os.Rename(tmpFile, "/etc/dhcpcd.conf"); err != nil {
        return fmt.Errorf("failed to rename dhcpcd.conf: %w", err)
    }

    // สั่ง reconfigure dhcpcd ให้โหลดค่าใหม่
    cmd := execCommand("sudo", "dhcpcd", "--reconfigure")
    if err := cmd.Run(); err != nil {
        log.Printf("[DhcpcdService] Warning: failed to reconfigure dhcpcd: %v", err)
    }

    log.Printf("[DhcpcdService] ApplyHostnameConfig done: shareWithDhcp=%v", shareWithDhcp)
    return nil
}
```

---

### 5. Frontend Service

เพิ่มใน `services/systemService.ts`:

```ts
const HOSTNAME_STORAGE_KEY = "pigate_hostname";

export interface SystemHostnameSettings {
  hostname: string;
  shareWithDhcp: boolean;
}

const initialHostnameSettings: SystemHostnameSettings = {
  hostname: "PiGate-RPI5",
  shareWithDhcp: false,
};

// เพิ่มใน systemService object:
getHostname: async (): Promise<SystemHostnameSettings> => {
  if (IS_MOCK_MODE) {
    await new Promise((resolve) => setTimeout(resolve, 200));
    const stored = localStorage.getItem(HOSTNAME_STORAGE_KEY);
    return stored ? JSON.parse(stored) : initialHostnameSettings;
  }
  const response = await fetch(`${API_BASE_URL}/system/hostname`);
  if (!response.ok) throw new Error("Failed to fetch hostname settings");
  return response.json();
},

updateHostname: async (settings: SystemHostnameSettings): Promise<SystemHostnameSettings> => {
  if (IS_MOCK_MODE) {
    await new Promise((resolve) => setTimeout(resolve, 300));
    localStorage.setItem(HOSTNAME_STORAGE_KEY, JSON.stringify(settings));
    return settings;
  }
  const response = await fetch(`${API_BASE_URL}/system/hostname`, {
    method: "PUT",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(settings),
  });
  if (!response.ok) throw new Error("Failed to update hostname settings");
  return response.json();
},
```

---

### 6. Frontend UI — SettingsMaintenance.tsx

เพิ่ม Card ใหม่ "System Identity" ก่อน Card เวลา/NTP ใน Tab "settings":

```
┌──────────────────────────────────────────────┐
│  🖥️  System Identity                          │
├──────────────────────────────────────────────┤
│  Hostname                                     │
│  [  PiGate-RPI5                          ]   │
│  ตัวอักษร a-z, 0-9 และขีด "-" เท่านั้น        │
│                                               │
│  Share hostname with DHCP clients             │
│  ส่งชื่อเครื่องไปบอก Router ฝั่ง WAN  [ ◯ ]  │
│                                               │
│                           [💾 Save Changes]  │
└──────────────────────────────────────────────┘
```

State ที่ต้องเพิ่ม:

```tsx
const [hostname, setHostname] = useState("")
const [shareWithDhcp, setShareWithDhcp] = useState(false)
const [hostnameLoading, setHostnameLoading] = useState(false)
const [hostnameFeedback, setHostnameFeedback] = useState<{ type: "success" | "error"; message: string } | null>(null)
```

---

### 7. Frontend — Dashboard.tsx

แก้บรรทัด 679 จาก Hardcode เป็น Dynamic:

```tsx
// เดิม
<span className="font-semibold text-foreground">PiGate-RPI5</span>

// แก้เป็น — เพิ่ม state และ load ตอน mount
const [systemHostname, setSystemHostname] = useState("PiGate-RPI5")

useEffect(() => {
  systemService.getHostname().then((s) => setSystemHostname(s.hostname)).catch(() => {})
}, [])

<span className="font-semibold text-foreground">{systemHostname}</span>
```

---

## Flow เมื่อ User กด Save

```
User กด Save Hostname
        │
        ▼
PUT /api/system/hostname
        │
        ├─ 1. Validate hostname (RFC 1123)
        ├─ 2. บันทึก SQLite (system_hostname_settings)
        ├─ 3. เขียน /etc/hostname  (atomic write)
        ├─ 4. hostnamectl set-hostname <name>
        └─ 5. dhcpcdService.ApplyHostnameConfig(shareWithDhcp)
                    │
                    ├─ shareWithDhcp=true  → /etc/dhcpcd.conf: "hostname"
                    └─ shareWithDhcp=false → /etc/dhcpcd.conf: "nohook hostname"
                               → dhcpcd --reconfigure
```

---

## ลำดับการ Implement (แนะนำ)

```
1. db/connection.go      — เพิ่ม table + seed default
2. model/types.go        — เพิ่ม struct
3. db/repository.go      — เพิ่ม GetHostnameSettings, SaveHostnameSettings
4. service/dhcpcd.go     — เพิ่ม ApplyHostnameConfig
5. api/handlers.go       — เพิ่ม handlers
6. api/router.go         — register routes
7. systemService.ts      — เพิ่ม service functions + mock
8. SettingsMaintenance   — เพิ่ม UI Card
9. Dashboard.tsx         — แก้ hardcode hostname
```
