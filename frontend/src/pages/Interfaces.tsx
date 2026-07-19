import { useState, useMemo, useCallback, useEffect } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Network,
  Wifi,
  Cable,
  Edit,
  RefreshCw,
  Shield,
  Signal,
  Lock,
  Unlock,
  AlertCircle,
  Activity,
  ArrowUpDown,
  Check,
  Radio,
  Play,
  Terminal,
  Trash2,
  RotateCcw,
  Layers,
  Link as LinkIcon,
  GitMerge,
  HelpCircle,
  Fingerprint,
  Info,
  Plus
} from "lucide-react"
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from "@/components/ui/table"
import {
  Drawer,
  DrawerContent,
  DrawerDescription,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertTitle, AlertDescription } from "@/components/ui/alert"
import { Switch } from "@/components/ui/switch"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import {
  type NetworkInterface,
  type AdminAccess,
  type AddressingMode,
  type WifiScanResult
} from "@/data-mockup/mockData"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/hooks/useAlert"
import { isValidIp } from "@/lib/utils"
import { ifaceLabel } from "@/lib/ifaceLabel"



// Helper: Signal strength color
function signalColor(signal: number): string {
  if (signal >= 70) return "text-primary"
  if (signal >= 40) return "text-warning"
  return "text-destructive"
}

// Helper: Signal bar fill for visual indicator
function SignalBar({ signal }: { signal: number }) {
  const bars = 5
  const filled = Math.round((signal / 100) * bars)
  return (
    <div className="flex items-end gap-0.5 h-3.5">
      {Array.from({ length: bars }, (_, i) => (
        <div
          key={i}
          className={`w-[3px] rounded-sm transition-all ${i < filled
            ? signal >= 70
              ? "bg-primary"
              : signal >= 40
                ? "bg-warning"
                : "bg-destructive"
            : "bg-muted-foreground/20"
            }`}
          style={{ height: `${((i + 1) / bars) * 100}%` }}
        />
      ))}
    </div>
  )
}



// Helper: Dashboard-style stat card (mirrors Dashboard's StatCard)
function StatCard({
  icon: Icon,
  title,
  value,
}: {
  icon: typeof Network
  title: string
  value: number
}) {
  return (
    <Card size="sm" className="gap-0">
      <CardHeader className="space-y-0">
        <CardTitle className="flex items-center gap-2 text-sm font-medium text-muted-foreground">
          <Icon className="h-4 w-4 shrink-0" />
          <span className="text-foreground">{title}</span>
        </CardTitle>
      </CardHeader>
      <CardContent className="pt-3">
        <p className="text-2xl font-bold tracking-tight text-foreground">{value}</p>
      </CardContent>
    </Card>
  )
}

const ALL_ACCESS_OPTIONS: AdminAccess[] = ["HTTPS", "HTTP", "PING", "SSH"]

function getInterfaceIcon(type: string, subtype?: string, className = "h-5 w-5 mx-auto") {
  const displayType = subtype || type

  if (type === "wireless") {
    return <Wifi className={`${className} text-primary`} />
  }

  switch (displayType) {
    case "wireless":
      return <Wifi className={`${className} text-primary`} />
    case "vlan":
      return <Layers className={`${className} text-primary`} />
    case "veth":
      return <LinkIcon className={`${className} text-primary`} />
    case "bridge":
      return <GitMerge className={`${className} text-primary`} />
    case "device":
    case "ethernet":
      return <Cable className={`${className} text-primary`} />
    case "loopback":
      return <RotateCcw className={`${className} text-primary`} />
    case "tunnel":
      return <Network className={`${className} text-primary`} />
    default:
      return <HelpCircle className={`${className} text-muted-foreground`} />
  }
}

export default function Interfaces() {
  const { alert, confirm } = useAlert()
  // --- State ---
  const [interfaces, setInterfaces] = useState<NetworkInterface[]>([])
  // Offline interfaces (config row in DB, no live kernel link) are hidden by default;
  // the toolbar switch reveals them so their config can be deleted (issue #49).
  const [showOffline, setShowOffline] = useState(false)
  const [isLoading, setIsLoading] = useState(true)
  const [isRefreshing, setIsRefreshing] = useState(false)
  const [error, setError] = useState("")

  // Edit Dialog State
  const [isEditOpen, setIsEditOpen] = useState(false)
  const [editingIface, setEditingIface] = useState<NetworkInterface | null>(null)

  // Form State
  const [formAlias, setFormAlias] = useState("")
  const [formRole, setFormRole] = useState<"LAN" | "WAN">("LAN")
  const [formMode, setFormMode] = useState<AddressingMode>("dhcp")
  const [formIp, setFormIp] = useState("")
  const [formNetmask, setFormNetmask] = useState("")
  const [formGateway, setFormGateway] = useState("")
  const [formMetric, setFormMetric] = useState("") // empty string = unset (auto)
  const [formAccess, setFormAccess] = useState<AdminAccess[]>([])
  const [formError, setFormError] = useState("")

  // Wi-Fi Form State
  const [formSSID, setFormSSID] = useState("")
  const [formWifiPassword, setFormWifiPassword] = useState("")
  const [formWifiSecurity, setFormWifiSecurity] = useState("WPA2")

  // Wi-Fi MAC Address Randomization & LAA Form State
  const [formMacMode, setFormMacMode] = useState<"hardware" | "randomized">("hardware")

  // Wi-Fi Radio Band Preference
  const [formPrefer5GHz, setFormPrefer5GHz] = useState(false)

  // Wi-Fi Backup & Failover Form State
  const [formFailoverEnabled, setFormFailoverEnabled] = useState(false)
  const [formBackupSSID, setFormBackupSSID] = useState("")
  const [formBackupWifiPassword, setFormBackupWifiPassword] = useState("")
  const [formBackupWifiSecurity, setFormBackupWifiSecurity] = useState("WPA2")
  const [formIpCheckTimeout, setFormIpCheckTimeout] = useState(15)
  const [formPrimaryMaxRetries, setFormPrimaryMaxRetries] = useState(3)
  const [formFailoverCooldown, setFormFailoverCooldown] = useState(60)

  // Wi-Fi Scanner State
  const [isScanning, setIsScanning] = useState(false)
  const [scanResults, setScanResults] = useState<WifiScanResult[]>([])
  const [showScanResults, setShowScanResults] = useState(false)

  // Create VLAN Dialog State
  const [isCreateVlanOpen, setIsCreateVlanOpen] = useState(false)
  const [vlanParent, setVlanParent] = useState("")
  const [vlanId, setVlanId] = useState("")
  const [vlanAlias, setVlanAlias] = useState("")
  const [vlanRole, setVlanRole] = useState<"LAN" | "WAN">("LAN")
  const [vlanMode, setVlanMode] = useState<AddressingMode>("dhcp")
  const [vlanIp, setVlanIp] = useState("")
  const [vlanNetmask, setVlanNetmask] = useState("")
  const [vlanGateway, setVlanGateway] = useState("")
  const [vlanAccess, setVlanAccess] = useState<AdminAccess[]>(["PING", "HTTP", "SSH"])
  const [vlanError, setVlanError] = useState("")
  const [vlanSubmitting, setVlanSubmitting] = useState(false)

  // --- Load Data ---
  const loadData = async (silent = false) => {
    if (silent) {
      setIsRefreshing(true)
    } else {
      setIsLoading(true)
    }
    setError("")
    try {
      const data = await interfaceService.getAll()
      setInterfaces(data)
    } catch (err) {
      setError(getErrorMessage(err) || "Failed to load interfaces.")
    } finally {
      setIsLoading(false)
      setIsRefreshing(false)
    }
  }

  const [wifiLiveStatuses, setWifiLiveStatuses] = useState<Record<string, { 
    state: string; 
    ssid: string; 
    activeMac?: string;
    freq?: number;
    keyMgmt?: string;
    wifiGen?: string;
  }>>({})

  useEffect(() => {
    // isLoading/error already start at their reset values; avoid a synchronous
    // setState in the effect body
    const initialLoad = async () => {
      try {
        const data = await interfaceService.getAll()
        setInterfaces(data)
      } catch (err) {
        setError(getErrorMessage(err) || "Failed to load interfaces.")
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [])

  useEffect(() => {
    interfaces.forEach(async (iface) => {
      if (iface.type === "wireless" && iface.status === "up") {
        try {
          const status = await interfaceService.getWifiStatus(iface.id)
          setWifiLiveStatuses((prev) => ({
            ...prev,
            [iface.id]: { 
              state: status.state, 
              ssid: status.ssid, 
              activeMac: status.activeMac,
              freq: status.freq,
              keyMgmt: status.keyMgmt,
              wifiGen: status.wifiGen
            }
          }))
        } catch (e) {
          console.error("Failed to fetch live wifi status:", e)
        }
      } else if (iface.type === "wireless" && iface.status !== "up") {
        setWifiLiveStatuses((prev) => ({
          ...prev,
          [iface.id]: { state: "DISCONNECTED", ssid: "" }
        }))
      }
    })
  }, [interfaces])

  // --- Statistics ---
  const stats = useMemo(() => {
    const total = interfaces.length
    const up = interfaces.filter(i => i.status === "up").length
    const down = interfaces.filter(i => i.status === "down").length
    const ethernet = interfaces.filter(i => i.type === "ethernet").length
    const wireless = interfaces.filter(i => i.type === "wireless").length
    return { total, up, down, ethernet, wireless }
  }, [interfaces])

  // --- Actions ---
  const openEditDialog = useCallback((iface: NetworkInterface) => {
    setEditingIface(iface)
    setFormAlias(iface.alias)
    setFormRole(iface.role || "LAN")
    setFormMode(iface.addressingMode)
    setFormIp(iface.ip)
    setFormNetmask(iface.netmask)
    setFormGateway(iface.gateway)
    setFormMetric(iface.metric != null ? String(iface.metric) : "")
    setFormAccess([...iface.adminAccess])
    setFormSSID(iface.wifiSSID || "")

    // เพื่อความปลอดภัย จะไม่รับค่าจาก api มาใส่ และจะอัปเดตข้อมูลเมื่อผู้ใช้แก้ไขเท่านั้น
    setFormWifiPassword("")

    setFormWifiSecurity(iface.wifiSecurity || "WPA2")

    // MAC fields
    setFormMacMode(iface.macMode === "randomized" ? "randomized" : "hardware")

    // Radio band preference
    setFormPrefer5GHz(iface.prefer5GHz ?? false)

    // Failover fields
    setFormFailoverEnabled(iface.failoverEnabled ?? false)
    setFormBackupSSID(iface.backupSsid || "")

    // เพื่อความปลอดภัย จะไม่รับค่าจาก api มาใส่ และจะอัปเดตข้อมูลเมื่อผู้ใช้แก้ไขเท่านั้น
    setFormBackupWifiPassword("")

    setFormBackupWifiSecurity(iface.backupWifiSecurity || "WPA2")
    setFormIpCheckTimeout(iface.ipCheckTimeout ?? 15)
    setFormPrimaryMaxRetries(iface.primaryMaxRetries ?? 3)
    setFormFailoverCooldown(iface.failoverCooldown ?? 60)

    setFormError("")
    setScanResults([])
    setShowScanResults(false)
    setIsEditOpen(true)
  }, [])

  const toggleAccess = (access: AdminAccess) => {
    setFormAccess(prev =>
      prev.includes(access) ? prev.filter(a => a !== access) : [...prev, access]
    )
  }

  const handleWifiScan = async () => {
    if (!editingIface) return
    setIsScanning(true)
    setScanResults([])
    setShowScanResults(true)
    try {
      const results = await interfaceService.scanWifi(editingIface.id)
      setScanResults(results)
    } catch (err) {
      setFormError(getErrorMessage(err) || "Failed to scan Wi-Fi.")
    } finally {
      setIsScanning(false)
    }
  }

  const selectSSID = (ssid: string) => {
    setFormSSID(ssid)
    setShowScanResults(false)
    const matched = scanResults.find((r) => r.ssid === ssid)
    if (matched) {
      if (matched.security.includes("WPA3")) {
        setFormWifiSecurity("WPA3")
      } else if (matched.security.includes("WPA2") || matched.security.includes("WPA-PSK")) {
        setFormWifiSecurity("WPA2")
      } else if (matched.security === "Open") {
        setFormWifiSecurity("Open")
      }
    }
  }

  const [simActive, setSimActive] = useState(false)
  const [simLogs, setSimLogs] = useState<string[]>([])

  const runFailoverSimulation = () => {
    if (simActive) return
    setSimActive(true)
    setSimLogs([])

    const addLog = (msg: string) => {
      const time = new Date().toLocaleTimeString()
      setSimLogs((prev) => [...prev, `[${time}] ${msg}`])
    }

    addLog("เริ่มการจำลองสถานการณ์ Wi-Fi Failover...")

    // Step 1: Connect primary
    setTimeout(() => {
      addLog(`[SSID หลัก: ${formSSID || "wlan0_primary"}] กำลังตรวจสอบการได้รับ IP Address...`)

      // Step 2: Failed retry 1
      setTimeout(() => {
        addLog(`[SSID หลัก: ${formSSID || "wlan0_primary"}] ตรวจสอบ: ไม่พบ IP Address (IP: 0.0.0.0)`)
        addLog(`[SSID หลัก: ${formSSID || "wlan0_primary"}] กำลังสั่งปิด/เปิดอินเตอร์เฟสใหม่ (Restart Interface ครั้งที่ 1/${formPrimaryMaxRetries})...`)

        // Step 3: Failed retry 2
        setTimeout(() => {
          addLog(`[SSID หลัก: ${formSSID || "wlan0_primary"}] ตรวจสอบ: ยังไม่พบ IP Address`)

          if (formPrimaryMaxRetries >= 2) {
            addLog(`[SSID หลัก: ${formSSID || "wlan0_primary"}] กำลังสั่งปิด/เปิดอินเตอร์เฟสใหม่ (Restart Interface ครั้งที่ 2/${formPrimaryMaxRetries})...`)
          }

          // Step 4: Failover action
          setTimeout(() => {
            addLog(`[SSID หลัก: ${formSSID || "wlan0_primary"}] การเชื่อมต่อ SSID หลักล้มเหลว (ลองใหม่ครบ ${formPrimaryMaxRetries} ครั้ง)`)

            if (formBackupSSID) {
              addLog(`[สลับคลื่นสำรอง] กำลังเปลี่ยนไปใช้ SSID สำรอง: "${formBackupSSID}"...`)

              // Step 5: Backup connection
              setTimeout(() => {
                addLog(`[SSID สำรอง: ${formBackupSSID}] กำลังพยายามเชื่อมต่อและตรวจสอบ IP Address...`)

                // Step 6: Backup success
                setTimeout(() => {
                  addLog(`[SSID สำรอง: ${formBackupSSID}] ได้รับ IP Address (10.0.50.222) สำเร็จ!`)
                  addLog(`สถานะปัจจุบัน: ทำงานปกติผ่านเครือข่ายสำรอง (Failover Active)`)
                  setSimActive(false)
                }, 1500)
              }, 1200)
            } else {
              addLog(`[ผลลัพธ์] ไม่พบ SSID สำรองระบุไว้ (Optional Backup SSID is empty)`)
              addLog(`[Cooldown] หน่วงเวลา ${formFailoverCooldown} วินาที ก่อนเริ่มสลับกลับมาลองเชื่อมต่อ SSID หลักอีกครั้ง...`)
              setSimActive(false)
            }
          }, 1500)
        }, 1500)
      }, 1500)
    }, 1000)
  }

  useEffect(() => {
    if (!isEditOpen) {
      // Dialog can also close via Radix's own onOpenChange (overlay click/Escape),
      // not just our explicit close handlers, so this must react to isEditOpen itself.
      // eslint-disable-next-line react-hooks/set-state-in-effect
      setSimActive(false)
      setSimLogs([])
    }
  }, [isEditOpen])

  const handleToggleStatus = async (id: string) => {
    try {
      await interfaceService.toggleStatus(id)
      const data = await interfaceService.getAll()
      setInterfaces(data)
    } catch (err) {
      await alert("ข้อผิดพลาด", "Failed to toggle interface status: " + getErrorMessage(err))
    }
  }

  const handleDeleteInterface = async (id: string, name: string) => {
    const ok = await confirm(
      "ยืนยันการลบอินเทอร์เฟซ",
      `คุณต้องการลบอินเทอร์เฟซ ${name} ออกจากฐานข้อมูลใช่หรือไม่?\nการดำเนินการนี้ไม่สามารถย้อนกลับได้`
    )
    if (!ok) return

    try {
      await interfaceService.delete(id)
      await alert("สำเร็จ", "ลบอินเทอร์เฟซออกจากฐานข้อมูลเรียบร้อยแล้ว")
      await loadData()
    } catch (err) {
      await alert("ข้อผิดพลาด", "Failed to delete interface: " + getErrorMessage(err))
    }
  }

  // Offline interfaces are hidden unless the toolbar switch is on. offlineCount drives the
  // "N hidden" hint shown next to the switch.
  const offlineCount = useMemo(
    () => interfaces.filter((i) => i.status === "offline").length,
    [interfaces]
  )
  const visibleInterfaces = useMemo(
    () => (showOffline ? interfaces : interfaces.filter((i) => i.status !== "offline")),
    [interfaces, showOffline]
  )

  // Eligible VLAN parents: ethernet interfaces that are not themselves VLANs and are not
  // offline (can't build a VLAN on a phantom parent).
  const vlanParentOptions = useMemo(
    () =>
      interfaces.filter(
        (i) => i.type === "ethernet" && i.subtype !== "vlan" && i.status !== "offline"
      ),
    [interfaces]
  )

  const openCreateVlanDialog = useCallback(() => {
    setVlanParent("")
    setVlanId("")
    setVlanAlias("")
    setVlanRole("LAN")
    setVlanMode("dhcp")
    setVlanIp("")
    setVlanNetmask("")
    setVlanGateway("")
    setVlanAccess(["PING", "HTTP", "SSH"])
    setVlanError("")
    setIsCreateVlanOpen(true)
  }, [])

  const toggleVlanAccess = (access: AdminAccess) => {
    setVlanAccess((prev) =>
      prev.includes(access) ? prev.filter((a) => a !== access) : [...prev, access]
    )
  }

  const handleDeleteVlan = async (id: string, name: string) => {
    const ok = await confirm(
      "ยืนยันการลบ VLAN",
      `คุณต้องการลบ VLAN ${name} ใช่หรือไม่?\n` +
        `การดำเนินการนี้จะลบทั้ง VLAN link ออกจากเคอร์เนลและการตั้งค่าในฐานข้อมูล และไม่สามารถย้อนกลับได้\n` +
        `หากคุณกำลังเชื่อมต่อกับบอร์ดผ่าน VLAN นี้ การเชื่อมต่ออาจหลุด`
    )
    if (!ok) return

    try {
      await interfaceService.delete(id)
      await alert("สำเร็จ", `ลบ VLAN ${name} เรียบร้อยแล้ว`)
      await loadData()
    } catch (err) {
      await alert("ข้อผิดพลาด", "Failed to delete VLAN: " + getErrorMessage(err))
    }
  }

  const handleCreateVlan = async (e: React.FormEvent) => {
    e.preventDefault()
    setVlanError("")

    if (!vlanParent) {
      setVlanError("กรุณาเลือก Parent Interface")
      return
    }
    const idNum = Number(vlanId)
    if (!Number.isInteger(idNum) || idNum < 1 || idNum > 4094) {
      setVlanError("VLAN ID ต้องเป็นจำนวนเต็มในช่วง 1–4094")
      return
    }
    const newName = `${vlanParent}.${idNum}`
    if (interfaces.some((i) => i.name === newName)) {
      setVlanError(`VLAN ${newName} มีอยู่ในระบบแล้ว`)
      return
    }
    if (vlanAlias && !/^[a-zA-Z0-9_]+$/.test(vlanAlias)) {
      setVlanError("ชื่อ Alias ต้องใช้ภาษาอังกฤษ ตัวเลข หรือเครื่องหมาย _ เท่านั้น (ห้ามเว้นวรรค)")
      return
    }
    if (vlanAlias && interfaces.some((i) => i.alias.toLowerCase() === vlanAlias.toLowerCase())) {
      setVlanError(`มีชื่อ Alias "${vlanAlias}" อยู่ในระบบแล้ว`)
      return
    }
    if (vlanAlias && interfaces.some((i) => i.name.toLowerCase() === vlanAlias.toLowerCase())) {
      setVlanError(`"${vlanAlias}" เป็นชื่อจริงของ interface อื่น ใช้เป็น Alias ไม่ได้`)
      return
    }
    if (vlanMode === "static") {
      if (!isValidIp(vlanIp)) {
        setVlanError("กรุณากรอก IP Address ให้ถูกต้อง (เช่น 192.168.100.1)")
        return
      }
      const maskNum = parseInt(vlanNetmask)
      if (isNaN(maskNum) || maskNum < 0 || maskNum > 32) {
        setVlanError("Netmask ต้องอยู่ในช่วง 0-32")
        return
      }
    }

    setVlanSubmitting(true)
    try {
      await interfaceService.createVlan({
        parent: vlanParent,
        vlanId: idNum,
        alias: vlanAlias || undefined,
        role: vlanRole,
        addressingMode: vlanMode,
        ip: vlanMode === "static" ? vlanIp : undefined,
        netmask: vlanMode === "static" ? vlanNetmask : undefined,
        gateway: vlanMode === "static" ? vlanGateway : undefined,
        adminAccess: vlanAccess,
      })
      setIsCreateVlanOpen(false)
      await loadData()
      await alert("สำเร็จ", `สร้าง VLAN ${newName} เรียบร้อยแล้ว`)
    } catch (err) {
      setVlanError(getErrorMessage(err) || "Failed to create VLAN.")
    } finally {
      setVlanSubmitting(false)
    }
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    if (!editingIface) return

    // Validation: Alias — mirrors the server rules. Empty = default to the OS
    // name (normalized server-side); alias == own name is always legal (e.g. the
    // VLAN default "eth0.100" contains a dot the pattern would reject).
    if (formAlias !== "" && formAlias !== editingIface.name) {
      const aliasRegex = /^[a-zA-Z0-9_]+$/
      if (!aliasRegex.test(formAlias)) {
        setFormError("ชื่อ Alias ต้องใช้ภาษาอังกฤษ ตัวเลข หรือเครื่องหมาย _ เท่านั้น (ห้ามเว้นวรรค)")
        return
      }

      // Duplicate alias check (case-insensitive)
      const isDuplicate = interfaces.some(
        i => i.alias.toLowerCase() === formAlias.toLowerCase() && i.id !== editingIface.id
      )
      if (isDuplicate) {
        setFormError(`มีชื่อ Alias "${formAlias}" อยู่ในระบบแล้ว`)
        return
      }

      // Alias must not equal another interface's OS name — labels would be ambiguous
      const collidesName = interfaces.some(
        i => i.name.toLowerCase() === formAlias.toLowerCase() && i.id !== editingIface.id
      )
      if (collidesName) {
        setFormError(`"${formAlias}" เป็นชื่อจริงของ interface อื่น ใช้เป็น Alias ไม่ได้`)
        return
      }
    }

    // Validation for Static mode
    if (formMode === "static") {
      if (!isValidIp(formIp)) {
        setFormError("กรุณากรอก IP Address ให้ถูกต้อง (เช่น 192.168.1.1) และค่า Octet ต้องอยู่ในช่วง 0-255")
        return
      }
      const maskNum = parseInt(formNetmask)
      if (isNaN(maskNum) || maskNum < 0 || maskNum > 32) {
        setFormError("Netmask ต้องอยู่ในช่วง 0-32")
        return
      }
    }

    // Validation for Route Metric (applies to all addressing modes; empty = unset/auto)
    if (formMetric.trim() !== "") {
      const metricNum = Number(formMetric)
      if (!Number.isInteger(metricNum) || metricNum < 1 || metricNum > 9999) {
        setFormError("Route Metric ต้องเป็นจำนวนเต็มในช่วง 1–9999 (เว้นว่างเพื่อใช้ค่าอัตโนมัติ)")
        return
      }
    }

    // Validation for Wi-Fi
    if (editingIface.type === "wireless") {
      if (!formSSID.trim()) {
        setFormError("กรุณาเลือกหรือระบุ SSID ของเครือข่าย Wi-Fi")
        return
      }

      // Wi-Fi Failover validations
      if (formFailoverEnabled) {
        if (formIpCheckTimeout < 5) {
          setFormError("เวลาตรวจสอบ IP ต้องไม่น้อยกว่า 5 วินาที")
          return
        }
        if (formPrimaryMaxRetries < 1) {
          setFormError("จำนวนครั้งในการลองเชื่อมต่อต้องไม่น้อยกว่า 1 ครั้ง")
          return
        }
        if (formFailoverCooldown < 10) {
          setFormError("ระยะหน่วงเวลาลองใหม่ต้องไม่น้อยกว่า 10 วินาที")
          return
        }
      }
    }

    try {
      const updates: Partial<NetworkInterface> = {
        alias: formAlias,
        role: formRole,
        addressingMode: formMode,
        ip: formMode === "static" ? formIp : editingIface.ip,
        netmask: formMode === "static" ? formNetmask : editingIface.netmask,
        gateway: formMode === "static" ? formGateway : editingIface.gateway,
        // null explicitly clears the metric back to "unset" (auto) on the backend
        metric: formMetric.trim() === "" ? null : parseInt(formMetric),
        adminAccess: formAccess,
      }

      if (editingIface.type === "wireless") {
        updates.wifiSSID = formSSID
        updates.macMode = formMacMode
        updates.prefer5GHz = formPrefer5GHz
        updates.randomizedMac = ""
        updates.laaMacAddress = ""
        updates.randomizeOnReconnect = false
        if (formWifiPassword) {
          updates.wifiPassword = formWifiPassword
        }
        updates.wifiSecurity = formWifiSecurity

        // Failover properties
        updates.failoverEnabled = formFailoverEnabled
        updates.backupSsid = formBackupSSID
        if (formBackupWifiPassword) {
          updates.backupWifiPassword = formBackupWifiPassword
        }
        updates.backupWifiSecurity = formBackupWifiSecurity
        updates.ipCheckTimeout = formIpCheckTimeout
        updates.primaryMaxRetries = formPrimaryMaxRetries
        updates.failoverCooldown = formFailoverCooldown
      }

      await interfaceService.update(editingIface.id, updates)
      await loadData()
      setIsEditOpen(false)
    } catch (err) {
      setFormError(getErrorMessage(err) || "Failed to update interface.")
    }
  }

  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[400px] space-y-4">
        <RefreshCw className="h-8 w-8 animate-spin text-primary" />
        <span className="text-sm text-muted-foreground font-semibold">กำลังโหลดข้อมูล Interfaces...</span>
      </div>
    )
  }

  if (error) {
    return (
      <Alert variant="destructive">
        <AlertCircle className="h-4 w-4" />
        <AlertTitle>Error Loading Interfaces</AlertTitle>
        <AlertDescription className="text-xs">{error}</AlertDescription>
      </Alert>
    )
  }

  return (
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={Network} title="Total Interfaces" value={stats.total} />
        <StatCard icon={Activity} title="Active (UP)" value={stats.up} />
        <StatCard icon={Cable} title="Ethernet" value={stats.ethernet} />
        <StatCard icon={Wifi} title="Wireless" value={stats.wireless} />
      </div>

      {/* 2. Interface Table */}
      <Card>
        <CardHeader className="flex flex-row items-center justify-between space-y-0">
          <CardTitle className="flex items-center gap-2 text-base font-semibold">
            <Network className="h-4 w-4 text-muted-foreground" />
            Interface List
          </CardTitle>
          <div className="flex items-center gap-2">
            <div className="mr-1 flex items-center gap-2">
              <Switch
                id="show-offline"
                checked={showOffline}
                onCheckedChange={setShowOffline}
                className="cursor-pointer"
              />
              <Label htmlFor="show-offline" className="cursor-pointer text-xs text-muted-foreground">
                แสดง offline interfaces
                {offlineCount > 0 && (
                  <span className="ml-1 font-semibold text-warning">({offlineCount})</span>
                )}
              </Label>
            </div>
            <Button
              size="sm"
              onClick={openCreateVlanDialog}
              className="cursor-pointer gap-2"
            >
              <Plus className="h-4 w-4" />
              Create VLAN
            </Button>
            <Button
              variant="outline"
              size="sm"
              onClick={() => loadData(true)}
              disabled={isLoading || isRefreshing}
              className="cursor-pointer gap-2"
            >
              <RefreshCw className={`h-4 w-4 ${isRefreshing ? "animate-spin" : ""}`} />
              Refresh
            </Button>
          </div>
        </CardHeader>
        <CardContent>
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[6%] text-xs font-medium text-muted-foreground">Port</TableHead>
                <TableHead className="w-[25%] text-xs font-medium text-muted-foreground">Name (Alias)</TableHead>
                <TableHead className="w-[5%] text-xs font-medium text-muted-foreground">Role</TableHead>
                <TableHead className="w-[20%] text-xs font-medium text-muted-foreground">IP / Netmask</TableHead>
                <TableHead className="w-[18%] text-xs font-medium text-muted-foreground">Admin Access</TableHead>
                <TableHead className="w-[5%] text-xs font-medium text-muted-foreground">Speed</TableHead>
                <TableHead className="w-[5%] text-xs font-medium text-muted-foreground">Status</TableHead>
                <TableHead className="w-[13%] text-right text-xs font-medium text-muted-foreground">Action</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {visibleInterfaces.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className="py-8 text-center text-xs text-muted-foreground">
                    ไม่พบอินเทอร์เฟซเครือข่าย
                  </TableCell>
                </TableRow>
              ) : (
                visibleInterfaces.map((iface) => (
                  <TableRow key={iface.id}>
                    {/* Port Icon */}
                    <TableCell className="py-3 text-center">
                      {getInterfaceIcon(iface.type, iface.subtype, "h-5 w-5 mx-auto")}
                    </TableCell>

                    {/* Name (Alias) */}
                    <TableCell className="py-3">
                      <div className="font-medium text-foreground">
                        {iface.alias && iface.alias !== iface.name ? iface.alias : iface.name}
                      </div>
                      {iface.alias && iface.alias !== iface.name && (
                        <div className="mt-0.5 text-xs text-muted-foreground">({iface.name})</div>
                      )}
                      <div className="mt-1.5 flex flex-wrap items-center gap-1.5">
                        <Badge variant="secondary" className="rounded px-1.5 py-0 font-mono text-[10px] font-medium capitalize">
                          {iface.subtype || iface.type}
                        </Badge>
                        {
                          iface.type === "wireless" ? (
                            wifiLiveStatuses[iface.id]?.state === "COMPLETED" ? (
                              <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-semibold text-primary">
                                Connected
                              </Badge>
                            ) : wifiLiveStatuses[iface.id]?.state === "SCANNING" ? (
                              <Badge variant="outline" className="rounded border-warning/20 bg-warning/10 px-1.5 py-0 text-[10px] font-semibold text-warning">
                                Scanning
                              </Badge>
                            ) : wifiLiveStatuses[iface.id]?.state === "ASSOCIATING" ||
                              wifiLiveStatuses[iface.id]?.state === "AUTHENTICATING" ||
                              wifiLiveStatuses[iface.id]?.state === "ASSOCIATED" ||
                              wifiLiveStatuses[iface.id]?.state === "4WAY_HANDSHAKE" ||
                              wifiLiveStatuses[iface.id]?.state === "GROUP_HANDSHAKE" ? (
                              <Badge variant="outline" className="rounded border-warning/20 bg-warning/10 px-1.5 py-0 text-[10px] font-semibold text-warning">
                                Connecting
                              </Badge>
                            ) : (
                              <Badge variant="outline" className="rounded border-destructive/20 bg-destructive/10 px-1.5 py-0 text-[10px] font-semibold text-destructive">
                                Disconnected
                              </Badge>
                            )
                          ) : <></>
                        }
                        {
                          iface.type === "wireless" && iface.status === "up" && wifiLiveStatuses[iface.id] ? (
                            <>
                              {wifiLiveStatuses[iface.id].freq ? (
                                <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-semibold text-primary">
                                  {wifiLiveStatuses[iface.id].freq} MHz
                                </Badge>
                              ) : null}
                              {wifiLiveStatuses[iface.id].wifiGen ? (
                                <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-semibold text-primary">
                                  {wifiLiveStatuses[iface.id].wifiGen}
                                </Badge>
                              ) : null}
                              {wifiLiveStatuses[iface.id].keyMgmt ? (
                                <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-semibold text-primary">
                                  {wifiLiveStatuses[iface.id].keyMgmt}
                                </Badge>
                              ) : null}
                            </>
                          ) : <></>
                        }
                        {iface.type === "wireless" && iface.status === "up" && wifiLiveStatuses[iface.id]?.ssid && (
                          <div className="flex items-center gap-1 ml-0.5">
                            <Signal className="h-3 w-3 text-primary" />
                            <span className="text-[10px] text-primary font-mono">{wifiLiveStatuses[iface.id].ssid}</span>
                          </div>
                        )}
                      </div>
                    </TableCell>

                    {/* Role */}
                    <TableCell className="py-3">
                      {iface.role === "WAN" ? (
                        <Badge variant="outline" className="rounded border-destructive/20 bg-destructive/10 px-2 py-0.5 text-[10px] font-semibold text-destructive">
                          WAN
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[10px] font-semibold text-primary">
                          LAN
                        </Badge>
                      )}
                    </TableCell>

                    {/* IP / Netmask */}
                    <TableCell className="py-3">
                      <div className="font-mono text-xs text-foreground">
                        {iface.status === "up" ? `${iface.ip} / ${iface.netmask}` : "—"}
                      </div>
                      <div className="mt-0.5 text-[10px] text-muted-foreground">
                        {iface.addressingMode === "dhcp" ? "DHCP" : "Static"}
                        {iface.metric != null && (
                          <span className="ml-1.5 font-mono text-primary/80">metric {iface.metric}</span>
                        )}
                      </div>
                    </TableCell>

                    {/* Admin Access */}
                    <TableCell className="py-3">
                      <div className="flex flex-wrap gap-1">
                        {iface.adminAccess.length === 0 ? (
                          <span className="text-xs italic text-muted-foreground/45">None</span>
                        ) : (
                          iface.adminAccess.map((access) => (
                            <Badge
                              key={access}
                              variant="outline"
                              className="rounded px-1.5 py-0 font-mono text-[10px] text-muted-foreground"
                            >
                              {access}
                            </Badge>
                          ))
                        )}
                      </div>
                    </TableCell>

                    {/* Speed */}
                    <TableCell className="py-3">
                      <span className="font-mono text-xs text-muted-foreground">
                        {iface.status === "up" ? iface.speed : "—"}
                      </span>
                    </TableCell>

                    {/* Status */}
                    <TableCell className="py-3">
                      <div className="flex flex-wrap items-center gap-1">
                        {iface.status === "up" ? (
                          <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[10px] font-semibold text-primary">
                            UP
                          </Badge>
                        ) : iface.status === "offline" ? (
                          <Badge variant="outline" className="animate-pulse rounded border-warning/20 bg-warning/10 px-2 py-0.5 text-[10px] font-semibold text-warning">
                            OFFLINE
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="rounded border-destructive/20 bg-destructive/10 px-2 py-0.5 text-[10px] font-semibold text-destructive">
                            DOWN
                          </Badge>
                        )}
                        {/* Unmanaged: exists in kernel but has no config row in DB.
                            Use `=== false` (not falsy) so cached mock data without the
                            field is treated as managed. */}
                        {iface.managed === false && (
                          <Badge variant="outline" className="rounded border-border bg-muted/50 px-1.5 py-0.5 text-[10px] font-medium text-muted-foreground">
                            UNMANAGED
                          </Badge>
                        )}
                      </div>
                    </TableCell>

                    {/* Action */}
                    <TableCell className="py-3 text-right">
                      <div className="flex items-center justify-end gap-4">
                        {iface.status === "offline" ? (
                          // Offline row: the config exists in the DB but has no live kernel
                          // link, so the only meaningful action is to delete that config.
                          // Reset/edit/toggle and the separate VLAN-delete button are all
                          // omitted here — they'd either duplicate this or 409 (edit).
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => handleDeleteInterface(iface.id, iface.name)}
                            className="cursor-pointer text-destructive hover:bg-destructive/10 hover:text-destructive"
                            title="ลบ config ออกจากฐานข้อมูล"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        ) : (
                          <>
                            <div className="flex items-center gap-2">
                              <span className="text-xs text-muted-foreground">{iface.status === "up" ? "ON" : "OFF"}</span>
                              <Switch
                                checked={iface.status === "up"}
                                onCheckedChange={() => handleToggleStatus(iface.id)}
                              />
                            </div>
                            {/* VLAN sub-interfaces created by pigate can be deleted at any
                                time (link + config), unlike physical ports which must be
                                offline first. */}
                            {iface.subtype === "vlan" && iface.managed !== false && (
                              <Button
                                variant="ghost"
                                size="icon-sm"
                                onClick={() => handleDeleteVlan(iface.id, iface.name)}
                                className="cursor-pointer text-destructive hover:bg-destructive/10 hover:text-destructive"
                                title="ลบ VLAN"
                              >
                                <Trash2 className="h-4 w-4" />
                              </Button>
                            )}
                            <Button
                              variant="outline"
                              size="icon-sm"
                              onClick={() => openEditDialog(iface)}
                              className="cursor-pointer text-muted-foreground hover:text-foreground"
                              title="แก้ไขอินเทอร์เฟซ"
                            >
                              <Edit className="h-4 w-4" />
                            </Button>
                          </>
                        )}
                      </div>
                    </TableCell>
                  </TableRow>
                ))
              )}
            </TableBody>
          </Table>
        </CardContent>
      </Card>

      {/* 3. MAC Address reference */}
      <Card>
        <CardHeader className="space-y-0">
          <CardTitle className="flex items-center gap-2 text-base font-semibold">
            <Fingerprint className="h-4 w-4 text-muted-foreground" />
            Hardware Address (MAC)
          </CardTitle>
        </CardHeader>
        <CardContent>
          <div className="grid gap-2 sm:grid-cols-2">
            {visibleInterfaces.map((iface) => (
              <div key={iface.id} className="flex flex-col justify-between gap-2 rounded-lg border border-border bg-muted/50 px-3 py-2 sm:flex-row sm:items-center">
                <div className="flex items-center gap-2">
                  {iface.type === "ethernet" ? (
                    <Cable className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  ) : (
                    <Wifi className="h-3.5 w-3.5 shrink-0 text-muted-foreground" />
                  )}
                  <span className="text-xs font-semibold text-foreground">{iface.name}</span>
                  {iface.type === "wireless" && iface.macMode === "randomized" && (
                    <Badge
                      variant="outline"
                      className="rounded border-primary/20 bg-primary/10 px-1 py-0 text-[9px] font-normal text-primary"
                    >
                      Randomized
                    </Badge>
                  )}
                </div>
                <div className="flex flex-col items-end gap-0.5">
                  <span className="font-mono text-xs text-foreground">
                    {iface.type === "wireless" && iface.macMode === "randomized"
                      ? (wifiLiveStatuses[iface.id]?.activeMac || "สุ่มอัตโนมัติเมื่อเชื่อมต่อ")
                      : iface.macAddress}
                  </span>
                  {iface.type === "wireless" && iface.macMode === "randomized" && (
                    <span className="font-mono text-[10px] text-muted-foreground">
                      Real: {iface.realMacAddress || iface.macAddress}
                    </span>
                  )}
                </div>
              </div>
            ))}
          </div>
        </CardContent>
      </Card>

      {/* 4. Info note */}
      <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          <strong className="text-foreground">ข้อมูลสำคัญ:</strong>{" "}
          การเปลี่ยนค่า IP Address หรือ Addressing Mode ของอินเทอร์เฟซอาจทำให้เชื่อมต่อกับบอร์ดไม่ได้ชั่วคราว
          กรุณาตรวจสอบค่าอย่างถี่ถ้วนก่อนบันทึก อินเทอร์เฟซที่ตั้งค่าเป็น <strong className="text-foreground">"LAN"</strong> ควรใช้ Static IP
          และอินเทอร์เฟซ <strong className="text-foreground">"WAN"</strong> ควรใช้ DHCP เพื่อรับ IP จากเราเตอร์ต้นทาง
        </span>
      </div>

      {/* 5. Edit Interface Dialog */}
      <Drawer direction="right" open={isEditOpen} onOpenChange={setIsEditOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[920px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="flex items-center gap-2 text-base font-semibold">
              {editingIface && getInterfaceIcon(editingIface.type, editingIface.subtype, "h-4 w-4")}
              Edit Interface: {editingIface ? ifaceLabel(editingIface) : ""}
              {editingIface && (
                <Badge variant="secondary" className="ml-1 rounded px-1.5 py-0 font-mono text-[10px] font-medium capitalize">
                  {editingIface.subtype || editingIface.type}
                </Badge>
              )}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSave} className="space-y-4 text-sm">
            {formError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{formError}</AlertDescription>
              </Alert>
            )}

            {/* Row 1: Alias Name & Port Role */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="form-alias" className="block text-xs font-medium text-muted-foreground">
                  Alias Name <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="form-alias"
                  type="text"
                  required
                  value={formAlias}
                  onChange={(e) => setFormAlias(e.target.value)}
                  placeholder="เช่น LAN_Internal, WAN_WiFi"
                  className="h-9 font-mono text-sm"
                />
                <p className="mt-0.5 text-[10px] text-muted-foreground">ห้ามเว้นวรรค ใช้ได้เฉพาะอักษรภาษาอังกฤษ ตัวเลข และ _</p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="form-role" className="block text-xs font-medium text-muted-foreground">
                  Port Role (หน้าที่ของพอร์ต) <span className="text-destructive">*</span>
                </Label>
                <Select
                  value={formRole}
                  onValueChange={(value: "LAN" | "WAN") => setFormRole(value)}
                >
                  <SelectTrigger id="form-role" className="h-9 w-full text-xs font-medium">
                    <SelectValue placeholder="เลือกประเภทพอร์ต" />
                  </SelectTrigger>
                  <SelectContent className="text-xs font-medium">
                    <SelectItem value="LAN">LAN (วงภายใน)</SelectItem>
                    <SelectItem value="WAN">WAN (ต่อขายนอก / อินเทอร์เน็ต)</SelectItem>
                  </SelectContent>
                </Select>
                <p className="mt-0.5 text-[10px] text-muted-foreground">LAN สำหรับเครือข่ายภายใน และ WAN สำหรับเชื่อมต่อเครือข่ายภายนอก</p>
              </div>
            </div>

            {/* Field: Addressing Mode */}
            <div className="space-y-2">
              <Label className="block text-xs font-medium text-muted-foreground">
                Addressing Mode
              </Label>
              <div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">
                <button
                  type="button"
                  onClick={() => setFormMode("dhcp")}
                  className={`cursor-pointer rounded-md px-4 py-1.5 text-xs font-medium transition ${formMode === "dhcp"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  DHCP (Auto)
                </button>
                <button
                  type="button"
                  onClick={() => setFormMode("static")}
                  className={`cursor-pointer rounded-md px-4 py-1.5 text-xs font-medium transition ${formMode === "static"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  Manual (Static)
                </button>
              </div>
            </div>

            {/* Static IP Fields (conditional) */}
            {formMode === "static" && (
              <div className="space-y-3 rounded-lg border border-border bg-muted/50 p-4">
                <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                  <ArrowUpDown className="h-3.5 w-3.5 text-muted-foreground" /> Static IP Configuration
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label htmlFor="form-ip" className="text-xs font-medium text-muted-foreground">IP Address <span className="text-destructive">*</span></Label>
                    <Input
                      id="form-ip"
                      type="text"
                      value={formIp}
                      onChange={(e) => setFormIp(e.target.value)}
                      placeholder="192.168.1.1"
                      className="h-9 font-mono text-sm"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="form-netmask" className="text-xs font-medium text-muted-foreground">Netmask (CIDR) <span className="text-destructive">*</span></Label>
                    <Input
                      id="form-netmask"
                      type="text"
                      value={formNetmask}
                      onChange={(e) => setFormNetmask(e.target.value)}
                      placeholder="24"
                      className="h-9 font-mono text-sm"
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="form-gateway" className="text-xs font-medium text-muted-foreground">Gateway</Label>
                  <Input
                    id="form-gateway"
                    type="text"
                    value={formGateway}
                    onChange={(e) => setFormGateway(e.target.value)}
                    placeholder="192.168.1.254"
                    className="h-9 font-mono text-sm"
                  />
                </div>
              </div>
            )}

            {/* Route Metric (all addressing modes — used for multi-WAN failover ordering) */}
            <div className="space-y-2 rounded-lg border border-border bg-muted/50 p-4">
              <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                <ArrowUpDown className="h-3.5 w-3.5 text-muted-foreground" /> Route Metric (ลำดับความสำคัญ Gateway)
              </div>
              <Input
                id="form-metric"
                type="number"
                min={1}
                max={9999}
                value={formMetric}
                onChange={(e) => setFormMetric(e.target.value)}
                placeholder="ว่าง = อัตโนมัติ"
                className="h-9 font-mono text-sm"
              />
              <p className="text-[10px] leading-relaxed text-muted-foreground">
                กำหนด priority ของ default gateway route (0.0.0.0/0) สำหรับ interface นี้
                — <strong className="text-foreground">เลขน้อยกว่า = ถูกเลือกใช้ก่อน</strong> (ใช้จัดลำดับ WAN หลัก/สำรอง).
                เว้นว่างเพื่อใช้ค่าอัตโนมัติ (static = 100, dhcp = ตาม dhcpcd). มีผลกับ IPv4 เท่านั้น
                และการสลับ WAN อาจทำให้ session ที่ค้างอยู่ (รวมถึงหน้านี้) สะดุดชั่วขณะ
              </p>
            </div>

            {/* Wi-Fi Settings (only for wireless) */}
            {editingIface?.type === "wireless" && (
              <>
                <div className="space-y-3 rounded-lg border border-border bg-muted/50 p-4">
                  <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                    <Wifi className="h-3.5 w-3.5 text-muted-foreground" /> Wireless Client Settings
                  </div>

                  {/* SSID with Scanner */}
                  <div className="space-y-1.5">
                    <Label htmlFor="form-ssid" className="text-xs font-medium text-muted-foreground">
                      SSID <span className="text-destructive">*</span>
                    </Label>
                    <div className="flex gap-2">
                      <Input
                        id="form-ssid"
                        type="text"
                        value={formSSID}
                        onChange={(e) => setFormSSID(e.target.value)}
                        placeholder="ชื่อเครือข่าย Wi-Fi"
                        className="h-9 flex-1 font-mono text-sm"
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={handleWifiScan}
                        disabled={isScanning}
                        className="h-9 cursor-pointer gap-1.5 px-3 text-xs"
                      >
                        <RefreshCw className={`h-3.5 w-3.5 ${isScanning ? "animate-spin" : ""}`} />
                        {isScanning ? "Scanning..." : "Scan"}
                      </Button>
                    </div>
                  </div>

                  {/* Scan Results */}
                  {showScanResults && (
                    <div className="overflow-hidden rounded-lg border border-border bg-background">
                      {isScanning ? (
                        <div className="flex items-center justify-center gap-2 py-6 text-xs text-muted-foreground">
                          <RefreshCw className="h-4 w-4 animate-spin text-primary" />
                          กำลังค้นหาเครือข่าย Wi-Fi...
                        </div>
                      ) : (
                        <div className="max-h-[200px] overflow-y-auto">
                          {scanResults.map((wifi, idx) => (
                            <button
                              key={idx}
                              type="button"
                              onClick={() => selectSSID(wifi.ssid)}
                              className={`flex w-full cursor-pointer items-center justify-between border-b border-border/50 px-3 py-2 text-xs transition last:border-b-0 hover:bg-muted/50 ${formSSID === wifi.ssid ? "bg-primary/10" : ""
                                }`}
                            >
                              <div className="flex items-center gap-2">
                                <SignalBar signal={wifi.signal} />
                                <span className="font-semibold text-foreground">{wifi.ssid}</span>
                                {wifi.security !== "Open" ? (
                                  <Lock className="h-3 w-3 text-muted-foreground" />
                                ) : (
                                  <Unlock className="h-3 w-3 text-warning" />
                                )}
                                {formSSID === wifi.ssid && (
                                  <Check className="h-3 w-3 text-primary" />
                                )}
                              </div>
                              <div className="flex items-center gap-3 text-muted-foreground">
                                <span className={signalColor(wifi.signal)}>{wifi.signal}%</span>
                                <span className="text-[10px]">{wifi.security}</span>
                                <span className="text-[10px]">Ch.{wifi.channel}</span>
                                <Badge variant="outline" className="rounded px-1 py-0 text-[9px]">
                                  {wifi.frequency}
                                </Badge>
                              </div>
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Wi-Fi Password & Security */}
                  <div className="grid gap-3 sm:grid-cols-2">
                    <div className="space-y-1.5">
                      <Label htmlFor="form-wifi-security" className="block text-xs font-medium text-muted-foreground">
                        Key Management (ระบบความปลอดภัย)
                      </Label>
                      <Select
                        value={formWifiSecurity}
                        onValueChange={(value) => setFormWifiSecurity(value)}
                      >
                        <SelectTrigger id="form-wifi-security" className="h-9 w-full text-xs font-medium">
                          <SelectValue placeholder="เลือกประเภทความปลอดภัย" />
                        </SelectTrigger>
                        <SelectContent className="text-xs font-medium">
                          <SelectItem value="Open">Open (ไม่มีรหัสผ่าน)</SelectItem>
                          <SelectItem value="WPA2">WPA2 (แนะนำทั่วไป)</SelectItem>
                          <SelectItem value="WPA3">WPA3 (SAE-only)</SelectItem>
                          <SelectItem value="WPA2/WPA3">WPA2/WPA3 Mixed (Transition)</SelectItem>
                        </SelectContent>
                      </Select>
                    </div>
                    {formWifiSecurity !== "Open" && (
                      <div className="space-y-1.5">
                        <Label htmlFor="form-wifi-password" className="block text-xs font-medium text-muted-foreground">
                          Password (PSK)
                        </Label>
                        <Input
                          id="form-wifi-password"
                          type="password"
                          value={formWifiPassword}
                          onChange={(e) => setFormWifiPassword(e.target.value)}
                          placeholder="••••••••"
                          className="h-9 w-full font-mono text-sm"
                        />
                        <p className="mt-0.5 text-[10px] text-muted-foreground">เว้นว่างหากไม่ต้องการเปลี่ยนรหัสผ่าน</p>
                      </div>
                    )}
                  </div>
                </div>

                {/* Wi-Fi MAC Address Settings */}
                <div className="space-y-3 rounded-lg border border-border bg-muted/50 p-4">
                  <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                    <Shield className="h-3.5 w-3.5 text-muted-foreground" /> MAC Address Settings (การตั้งค่า MAC)
                  </div>
                  {/* MAC Address Mode selection */}
                  <div className="space-y-1.5">
                    <Label htmlFor="form-mac-mode" className="block text-xs font-medium text-muted-foreground">
                      MAC Address Mode
                    </Label>
                    <Select
                      value={formMacMode}
                      onValueChange={(value: "hardware" | "randomized") => setFormMacMode(value)}
                    >
                      <SelectTrigger id="form-mac-mode" size="sm" className="w-full text-xs font-medium sm:w-[220px]">
                        <SelectValue placeholder="เลือกโหมด MAC Address" />
                      </SelectTrigger>
                      <SelectContent className="text-xs font-medium">
                        <SelectItem value="hardware">Hardware MAC (ที่อยู่จริง)</SelectItem>
                        <SelectItem value="randomized">Random MAC (สุ่มที่อยู่โดย wpa_supplicant)</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {/* Comparison Panel */}
                  <div className="mt-1 space-y-1.5 rounded-lg border border-border bg-background p-3 font-mono text-xs">
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">ที่อยู่ MAC จริง (Hardware):</span>
                      <span className="text-foreground">{editingIface?.realMacAddress || editingIface?.macAddress}</span>
                    </div>
                    <div className="flex justify-between border-t border-border/50 pt-1.5">
                      <span className="text-muted-foreground">ที่อยู่ MAC ที่ใช้งานจริง (Active):</span>
                      <span className="font-semibold text-primary">
                        {formMacMode === "hardware"
                          ? (editingIface?.realMacAddress || editingIface?.macAddress)
                          : (editingIface && wifiLiveStatuses[editingIface.id]?.activeMac) || "สุ่มอัตโนมัติเมื่อเชื่อมต่อ"}
                      </span>
                    </div>
                  </div>

                  {/* Radio Band Preference */}
                  <div className="flex items-center justify-between border-t border-border/50 pt-3">
                    <div className="space-y-0.5">
                      <Label htmlFor="form-prefer-5ghz" className="block text-xs font-medium text-foreground">
                        Prefer 5GHz
                      </Label>
                      <p className="text-[10px] text-muted-foreground">
                        ล็อกจับเฉพาะคลื่น 5GHz เพื่อความเร็วสูงสุด
                      </p>
                    </div>
                    <Switch
                      id="form-prefer-5ghz"
                      size="sm"
                      checked={formPrefer5GHz}
                      onCheckedChange={setFormPrefer5GHz}
                    />
                  </div>
                </div>

                {/* Wi-Fi Failover / Backup SSID Settings */}
                <div className="space-y-3 rounded-lg border border-border bg-muted/50 p-4">
                  <div className="flex items-center justify-between">
                    <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                      <Radio className="h-3.5 w-3.5 text-muted-foreground" /> Wi-Fi Backup & Failover (ฟีเจอร์สำรองข้อมูลคลื่น)
                    </div>
                    <Switch
                      size="sm"
                      checked={formFailoverEnabled}
                      onCheckedChange={setFormFailoverEnabled}
                    />
                  </div>

                  {formFailoverEnabled && (
                    <div className="space-y-4 pt-1 animate-fade-in text-xs">
                      {/* Optional Backup SSID & Security */}
                      <div className="grid gap-3 sm:grid-cols-2">
                        <div className="space-y-1.5">
                          <Label htmlFor="form-backup-ssid" className="text-xs font-medium text-muted-foreground">
                            Backup SSID (ชื่อ Wi-Fi สำรอง)
                          </Label>
                          <Input
                            id="form-backup-ssid"
                            type="text"
                            value={formBackupSSID}
                            onChange={(e) => setFormBackupSSID(e.target.value)}
                            placeholder="ชื่อเครือข่าย Wi-Fi สำรอง"
                            className="h-9 font-mono text-sm"
                          />
                        </div>
                        <div className="space-y-1.5">
                          <Label htmlFor="form-backup-wifi-security" className="block text-xs font-medium text-muted-foreground">
                            Backup Security (ระบบความปลอดภัยสำรอง)
                          </Label>
                          <Select
                            value={formBackupWifiSecurity}
                            onValueChange={(value) => setFormBackupWifiSecurity(value)}
                          >
                            <SelectTrigger id="form-backup-wifi-security" className="h-9 w-full text-xs font-medium">
                              <SelectValue placeholder="เลือกประเภทความปลอดภัย" />
                            </SelectTrigger>
                            <SelectContent className="text-xs font-medium">
                              <SelectItem value="Open">Open (ไม่มีรหัสผ่าน)</SelectItem>
                              <SelectItem value="WPA2">WPA2 (แนะนำทั่วไป)</SelectItem>
                              <SelectItem value="WPA3">WPA3 (SAE-only)</SelectItem>
                              <SelectItem value="WPA2/WPA3">WPA2/WPA3 Mixed (Transition)</SelectItem>
                            </SelectContent>
                          </Select>
                        </div>
                      </div>

                      {/* Backup Password (conditional) */}
                      {formBackupWifiSecurity !== "Open" && (
                        <div className="max-w-md space-y-1.5">
                          <Label htmlFor="form-backup-wifi-password" className="block text-xs font-medium text-muted-foreground">
                            Backup Password (รหัสผ่านสำรอง)
                          </Label>
                          <Input
                            id="form-backup-wifi-password"
                            type="password"
                            value={formBackupWifiPassword}
                            onChange={(e) => setFormBackupWifiPassword(e.target.value)}
                            placeholder="รหัสผ่านสำรอง"
                            className="h-9 w-full font-mono text-sm"
                          />
                          <p className="mt-0.5 text-[10px] text-muted-foreground">เว้นว่างหากไม่ต้องการเปลี่ยนรหัสผ่านสำรอง</p>
                        </div>
                      )}

                      {/* Settings grid */}
                      <div className="grid gap-3 sm:grid-cols-3">
                        <div className="space-y-1.5">
                          <Label htmlFor="form-ip-check-timeout" className="block text-xs font-medium text-muted-foreground" title="เวลาที่ใช้ในการตรวจสอบการตอบกลับ IP ก่อนพิจารณาว่าล้มเหลว">
                            IP Check Timeout (วินาที)
                          </Label>
                          <Input
                            id="form-ip-check-timeout"
                            type="number"
                            min="5"
                            max="300"
                            value={formIpCheckTimeout}
                            onChange={(e) => setFormIpCheckTimeout(parseInt(e.target.value) || 15)}
                            className="h-9 font-mono text-sm"
                          />
                        </div>
                        <div className="space-y-1.5">
                          <Label htmlFor="form-primary-max-retries" className="block text-xs font-medium text-muted-foreground" title="จำนวนครั้งในการเปิด/ปิดพอร์ตใหม่เพื่อเชื่อมต่อ SSID หลัก">
                            Max Retries (ครั้ง)
                          </Label>
                          <Input
                            id="form-primary-max-retries"
                            type="number"
                            min="1"
                            max="10"
                            value={formPrimaryMaxRetries}
                            onChange={(e) => setFormPrimaryMaxRetries(parseInt(e.target.value) || 3)}
                            className="h-9 font-mono text-sm"
                          />
                        </div>
                        <div className="space-y-1.5">
                          <Label htmlFor="form-failover-cooldown" className="block text-xs font-medium text-muted-foreground" title="ระยะหน่วงเวลาก่อนสลับกลับมาลองเชื่อมต่อ SSID หลักอีกครั้ง">
                            Cooldown Delay (วินาที)
                          </Label>
                          <Input
                            id="form-failover-cooldown"
                            type="number"
                            min="10"
                            max="3600"
                            value={formFailoverCooldown}
                            onChange={(e) => setFormFailoverCooldown(parseInt(e.target.value) || 60)}
                            className="h-9 font-mono text-sm"
                          />
                        </div>
                      </div>

                      {/* Interactive Simulator Section */}
                      <div className="space-y-2 rounded-lg border border-border bg-background p-3">
                        <div className="flex items-center justify-between">
                          <span className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                            <Terminal className="h-3.5 w-3.5 text-muted-foreground" /> Failover Simulator (ตัวจำลองการสลับคลื่น)
                          </span>
                          <Button
                            type="button"
                            size="sm"
                            onClick={runFailoverSimulation}
                            disabled={simActive}
                            className="h-7 cursor-pointer gap-1 px-2.5 text-[11px] font-semibold"
                          >
                            <Play className="h-3 w-3 fill-primary-foreground" />
                            {simActive ? "Simulating..." : "Simulate Failover"}
                          </Button>
                        </div>

                        {simLogs.length > 0 && (
                          <div className="scrollbar-thin max-h-[140px] space-y-1 overflow-y-auto rounded-md border border-border bg-muted/50 p-2 font-mono text-[10px] text-primary">
                            {simLogs.map((log, idx) => (
                              <div key={idx} className="leading-relaxed whitespace-pre-wrap">{log}</div>
                            ))}
                          </div>
                        )}
                      </div>
                    </div>
                  )}
                </div>
              </>
            )}

            {/* Admin Access Checkboxes */}
            <div className="space-y-2">
              <Label className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <Shield className="h-3.5 w-3.5" />
                Admin Access (Management)
              </Label>
              <div className="flex flex-wrap gap-2">
                {ALL_ACCESS_OPTIONS.map((access) => {
                  const isActive = formAccess.includes(access)
                  return (
                    <button
                      key={access}
                      type="button"
                      onClick={() => toggleAccess(access)}
                      className={`flex cursor-pointer items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition ${isActive
                        ? "border-primary/40 bg-primary/10 text-primary"
                        : "border-border bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground"
                        }`}
                    >
                      <div className={`flex h-3.5 w-3.5 items-center justify-center rounded border ${isActive ? "border-primary bg-primary" : "border-muted-foreground/40"
                        }`}>
                        {isActive && <Check className="h-2.5 w-2.5 text-primary-foreground" />}
                      </div>
                      {access}
                    </button>
                  )
                })}
              </div>
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsEditOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button type="submit" className="cursor-pointer px-6 font-semibold">
                Save Changes
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>

      {/* 6. Create VLAN Drawer (right side, mirrors the Edit Interface drawer).
          Uses plain Select (not Combobox), so default behavior is fine. */}
      <Drawer direction="right" open={isCreateVlanOpen} onOpenChange={setIsCreateVlanOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[560px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="flex items-center gap-2 text-base font-semibold">
              <Layers className="h-4 w-4 text-primary" />
              Create VLAN Interface
            </DrawerTitle>
            <DrawerDescription className="text-xs">
              สร้าง VLAN (802.1Q) บน interface ที่มีอยู่ — ชื่อจะถูกตั้งเป็น
              <span className="mx-1 font-mono text-foreground">
                {vlanParent && vlanId ? `${vlanParent}.${vlanId}` : "<parent>.<id>"}
              </span>
              และจะถูกสร้างคืนอัตโนมัติทุกครั้งที่รีบูต
            </DrawerDescription>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleCreateVlan} className="space-y-4 text-sm">
            {vlanError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{vlanError}</AlertDescription>
              </Alert>
            )}

            {/* Parent + VLAN ID */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="vlan-parent" className="block text-xs font-medium text-muted-foreground">
                  Parent Interface <span className="text-destructive">*</span>
                </Label>
                <Select value={vlanParent} onValueChange={setVlanParent}>
                  <SelectTrigger id="vlan-parent" className="h-9 w-full text-xs font-medium">
                    <SelectValue placeholder="เลือก interface" />
                  </SelectTrigger>
                  <SelectContent className="text-xs font-medium">
                    {vlanParentOptions.length === 0 ? (
                      <div className="px-2 py-1.5 text-xs text-muted-foreground">
                        ไม่พบ ethernet interface ที่ใช้ได้
                      </div>
                    ) : (
                      vlanParentOptions.map((p) => (
                        <SelectItem key={p.id} value={p.name}>
                          {ifaceLabel(p)}
                        </SelectItem>
                      ))
                    )}
                  </SelectContent>
                </Select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="vlan-id" className="block text-xs font-medium text-muted-foreground">
                  VLAN ID (1–4094) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="vlan-id"
                  type="number"
                  min={1}
                  max={4094}
                  value={vlanId}
                  onChange={(e) => setVlanId(e.target.value)}
                  placeholder="100"
                  className="h-9 font-mono text-sm"
                />
              </div>
            </div>

            {/* Alias + Role */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="vlan-alias" className="block text-xs font-medium text-muted-foreground">
                  Alias Name
                </Label>
                <Input
                  id="vlan-alias"
                  type="text"
                  value={vlanAlias}
                  onChange={(e) => setVlanAlias(e.target.value)}
                  placeholder={vlanParent && vlanId ? `${vlanParent}.${vlanId}` : "เว้นว่าง = ใช้ชื่อ link"}
                  className="h-9 font-mono text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="vlan-role" className="block text-xs font-medium text-muted-foreground">
                  Port Role
                </Label>
                <Select value={vlanRole} onValueChange={(v: "LAN" | "WAN") => setVlanRole(v)}>
                  <SelectTrigger id="vlan-role" className="h-9 w-full text-xs font-medium">
                    <SelectValue />
                  </SelectTrigger>
                  <SelectContent className="text-xs font-medium">
                    <SelectItem value="LAN">LAN (วงภายใน)</SelectItem>
                    <SelectItem value="WAN">WAN (ต่อขายนอก / อินเทอร์เน็ต)</SelectItem>
                  </SelectContent>
                </Select>
              </div>
            </div>

            {/* Addressing mode */}
            <div className="space-y-2">
              <Label className="block text-xs font-medium text-muted-foreground">Addressing Mode</Label>
              <div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">
                <button
                  type="button"
                  onClick={() => setVlanMode("dhcp")}
                  className={`cursor-pointer rounded-md px-4 py-1.5 text-xs font-medium transition ${vlanMode === "dhcp"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  DHCP (Auto)
                </button>
                <button
                  type="button"
                  onClick={() => setVlanMode("static")}
                  className={`cursor-pointer rounded-md px-4 py-1.5 text-xs font-medium transition ${vlanMode === "static"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  Manual (Static)
                </button>
              </div>
            </div>

            {vlanMode === "static" && (
              <div className="space-y-3 rounded-lg border border-border bg-muted/50 p-4">
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1.5">
                    <Label htmlFor="vlan-ip" className="text-xs font-medium text-muted-foreground">
                      IP Address <span className="text-destructive">*</span>
                    </Label>
                    <Input
                      id="vlan-ip"
                      type="text"
                      value={vlanIp}
                      onChange={(e) => setVlanIp(e.target.value)}
                      placeholder="192.168.100.1"
                      className="h-9 font-mono text-sm"
                    />
                  </div>
                  <div className="space-y-1.5">
                    <Label htmlFor="vlan-netmask" className="text-xs font-medium text-muted-foreground">
                      Netmask (CIDR) <span className="text-destructive">*</span>
                    </Label>
                    <Input
                      id="vlan-netmask"
                      type="text"
                      value={vlanNetmask}
                      onChange={(e) => setVlanNetmask(e.target.value)}
                      placeholder="24"
                      className="h-9 font-mono text-sm"
                    />
                  </div>
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="vlan-gateway" className="text-xs font-medium text-muted-foreground">Gateway</Label>
                  <Input
                    id="vlan-gateway"
                    type="text"
                    value={vlanGateway}
                    onChange={(e) => setVlanGateway(e.target.value)}
                    placeholder="192.168.100.254"
                    className="h-9 font-mono text-sm"
                  />
                </div>
              </div>
            )}

            {/* Admin Access */}
            <div className="space-y-2">
              <Label className="flex items-center gap-1.5 text-xs font-medium text-muted-foreground">
                <Shield className="h-3.5 w-3.5" />
                Admin Access (Management)
              </Label>
              <div className="flex flex-wrap gap-2">
                {ALL_ACCESS_OPTIONS.map((access) => {
                  const isActive = vlanAccess.includes(access)
                  return (
                    <button
                      key={access}
                      type="button"
                      onClick={() => toggleVlanAccess(access)}
                      className={`flex cursor-pointer items-center gap-1.5 rounded-lg border px-3 py-1.5 text-xs font-medium transition ${isActive
                        ? "border-primary/40 bg-primary/10 text-primary"
                        : "border-border bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground"
                        }`}
                    >
                      <div className={`flex h-3.5 w-3.5 items-center justify-center rounded border ${isActive ? "border-primary bg-primary" : "border-muted-foreground/40"
                        }`}>
                        {isActive && <Check className="h-2.5 w-2.5 text-primary-foreground" />}
                      </div>
                      {access}
                    </button>
                  )
                })}
              </div>
            </div>

            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsCreateVlanOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button type="submit" disabled={vlanSubmitting} className="cursor-pointer px-6 font-semibold">
                {vlanSubmitting ? "Creating..." : "Create VLAN"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
