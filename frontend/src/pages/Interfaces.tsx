import { useState, useMemo, useCallback, useEffect } from "react"
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
  Terminal
} from "lucide-react"
import { Card } from "@/components/ui/card"
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
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



// Helper: Signal strength color
function signalColor(signal: number): string {
  if (signal >= 70) return "text-primary"
  if (signal >= 40) return "text-amber-400"
  return "text-red-400"
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
                ? "bg-amber-400"
                : "bg-red-400"
            : "bg-muted-foreground/20"
            }`}
          style={{ height: `${((i + 1) / bars) * 100}%` }}
        />
      ))}
    </div>
  )
}

// Helper: Generate a valid LAA (Locally Administered Address) MAC Address
function generateRandomMac(): string {
  const hex = "0123456789ABCDEF";
  // The first byte's second nibble must be 2, 6, A, or E for standard LAA
  const laaDigits = ["2", "6", "A", "E"];
  const firstByte = hex[Math.floor(Math.random() * 16)] + laaDigits[Math.floor(Math.random() * 4)];
  const parts = [firstByte];
  for (let i = 0; i < 5; i++) {
    parts.push(
      hex[Math.floor(Math.random() * 16)] + hex[Math.floor(Math.random() * 16)]
    );
  }
  return parts.join(":");
}

const ALL_ACCESS_OPTIONS: AdminAccess[] = ["HTTPS", "HTTP", "PING", "SSH"]

export default function Interfaces() {
  // --- State ---
  const [interfaces, setInterfaces] = useState<NetworkInterface[]>([])
  const [isLoading, setIsLoading] = useState(true)
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
  const [formDns1, setFormDns1] = useState("")
  const [formDns2, setFormDns2] = useState("")
  const [formAccess, setFormAccess] = useState<AdminAccess[]>([])
  const [formError, setFormError] = useState("")

  // Wi-Fi Form State
  const [formSSID, setFormSSID] = useState("")
  const [formWifiPassword, setFormWifiPassword] = useState("")

  // Wi-Fi MAC Address Randomization & LAA Form State
  const [formMacMode, setFormMacMode] = useState<"hardware" | "randomized" | "laa">("hardware")
  const [formLaaMac, setFormLaaMac] = useState("")
  const [formRandomizedMac, setFormRandomizedMac] = useState("")
  const [formRandomizeOnReconnect, setFormRandomizeOnReconnect] = useState(false)

  // Wi-Fi Backup & Failover Form State
  const [formFailoverEnabled, setFormFailoverEnabled] = useState(false)
  const [formBackupSSID, setFormBackupSSID] = useState("")
  const [formBackupWifiPassword, setFormBackupWifiPassword] = useState("")
  const [formIpCheckTimeout, setFormIpCheckTimeout] = useState(15)
  const [formPrimaryMaxRetries, setFormPrimaryMaxRetries] = useState(3)
  const [formFailoverCooldown, setFormFailoverCooldown] = useState(60)

  // Wi-Fi Scanner State
  const [isScanning, setIsScanning] = useState(false)
  const [scanResults, setScanResults] = useState<WifiScanResult[]>([])
  const [showScanResults, setShowScanResults] = useState(false)

  // --- Load Data ---
  const loadData = async () => {
    setIsLoading(true)
    setError("")
    try {
      const data = await interfaceService.getAll()
      setInterfaces(data)
    } catch (err: any) {
      setError(err.message || "Failed to load interfaces.")
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

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
    setFormDns1(iface.dns1)
    setFormDns2(iface.dns2)
    setFormAccess([...iface.adminAccess])
    setFormSSID(iface.connectedSSID || "")
    setFormWifiPassword("")

    // MAC fields
    const defaultRandomMac = iface.randomizedMac || (iface.type === "wireless" ? generateRandomMac() : "")
    setFormMacMode(iface.macMode || "hardware")
    setFormLaaMac(iface.laaMacAddress || "")
    setFormRandomizedMac(defaultRandomMac)
    setFormRandomizeOnReconnect(iface.randomizeOnReconnect ?? false)

    // Failover fields
    setFormFailoverEnabled(iface.failoverEnabled ?? false)
    setFormBackupSSID(iface.backupSsid || "")
    setFormBackupWifiPassword(iface.backupWifiPassword || "")
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
    } catch (err: any) {
      setFormError(err.message || "Failed to scan Wi-Fi.")
    } finally {
      setIsScanning(false)
    }
  }

  const selectSSID = (ssid: string) => {
    setFormSSID(ssid)
    setShowScanResults(false)
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
      setSimActive(false)
      setSimLogs([])
    }
  }, [isEditOpen])

  const handleToggleStatus = async (id: string) => {
    try {
      await interfaceService.toggleStatus(id)
      const data = await interfaceService.getAll()
      setInterfaces(data)
    } catch (err: any) {
      alert("Failed to toggle interface status: " + err.message)
    }
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    if (!editingIface) return

    // Validation: Alias
    const aliasRegex = /^[a-zA-Z0-9_]+$/
    if (!aliasRegex.test(formAlias)) {
      setFormError("ชื่อ Alias ต้องใช้ภาษาอังกฤษ ตัวเลข หรือเครื่องหมาย _ เท่านั้น (ห้ามเว้นวรรค)")
      return
    }

    // Duplicate alias check
    const isDuplicate = interfaces.some(
      i => i.alias.toLowerCase() === formAlias.toLowerCase() && i.id !== editingIface.id
    )
    if (isDuplicate) {
      setFormError(`มีชื่อ Alias "${formAlias}" อยู่ในระบบแล้ว`)
      return
    }

    // Validation for Static mode
    if (formMode === "static") {
      const ipRegex = /^(\d{1,3}\.){3}\d{1,3}$/
      if (!ipRegex.test(formIp)) {
        setFormError("กรุณากรอก IP Address ให้ถูกต้อง (เช่น 192.168.1.1)")
        return
      }
      const maskNum = parseInt(formNetmask)
      if (isNaN(maskNum) || maskNum < 0 || maskNum > 32) {
        setFormError("Netmask ต้องอยู่ในช่วง 0-32")
        return
      }
    }

    // Validation for Wi-Fi
    if (editingIface.type === "wireless") {
      if (!formSSID.trim()) {
        setFormError("กรุณาเลือกหรือระบุ SSID ของเครือข่าย Wi-Fi")
        return
      }

      // Validation for LAA MAC
      if (formMacMode === "laa") {
        const macRegex = /^([0-9A-Fa-f]{2}[:-]){5}([0-9A-Fa-f]{2})$/
        if (!macRegex.test(formLaaMac)) {
          setFormError("รูปแบบ LAA MAC Address ไม่ถูกต้อง (ตัวอย่าง: 9A:11:22:33:44:55)")
          return
        }

        // Check LAA bit (first byte's second nibble must be 2, 6, A, E)
        const secondChar = formLaaMac.charAt(1).toUpperCase()
        if (!["2", "6", "A", "E"].includes(secondChar)) {
          setFormError("ที่อยู่ LAA MAC ไม่ตรงตามมาตรฐาน (อักขระหลักที่ 2 ต้องเป็น 2, 6, A หรือ E เช่น 9A:11:22:...)")
          return
        }
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
        dns1: formMode === "static" ? formDns1 : editingIface.dns1,
        dns2: formMode === "static" ? formDns2 : editingIface.dns2,
        adminAccess: formAccess,
      }

      if (editingIface.type === "wireless") {
        updates.connectedSSID = formSSID
        updates.macMode = formMacMode
        updates.randomizedMac = formRandomizedMac
        updates.laaMacAddress = formLaaMac
        updates.randomizeOnReconnect = formRandomizeOnReconnect
        if (formWifiPassword) {
          updates.wifiPassword = formWifiPassword
        }
        // Failover properties
        updates.failoverEnabled = formFailoverEnabled
        updates.backupSsid = formBackupSSID
        updates.backupWifiPassword = formBackupWifiPassword
        updates.ipCheckTimeout = formIpCheckTimeout
        updates.primaryMaxRetries = formPrimaryMaxRetries
        updates.failoverCooldown = formFailoverCooldown
      }

      await interfaceService.update(editingIface.id, updates)
      await loadData()
      setIsEditOpen(false)
    } catch (err: any) {
      setFormError(err.message || "Failed to update interface.")
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
      <div className="p-6">
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4 text-red-400" />
          <AlertTitle>Error Loading Interfaces</AlertTitle>
          <AlertDescription className="text-xs text-red-400">{error}</AlertDescription>
        </Alert>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 1. Header Area */}
      <div>
        <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
          <Network className="h-7 w-7 text-primary fill-primary/10" />
          Network Interfaces
        </h1>
        <p className="text-muted-foreground mt-1">
          จัดการอินเทอร์เฟซเครือข่าย Physical และ Virtual บนบอร์ด Raspberry Pi
        </p>
      </div>

      {/* 2. Stats Dashboard Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">อินเทอร์เฟซทั้งหมด</div>
          <div className="mt-2 text-2xl font-bold text-foreground font-mono">{stats.total}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Activity className="h-3.5 w-3.5 text-primary" /> Active (UP)
          </div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.up}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Cable className="h-3.5 w-3.5 text-cyan-400" /> Ethernet
          </div>
          <div className="mt-2 text-2xl font-bold text-cyan-400 font-mono">{stats.ethernet}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Wifi className="h-3.5 w-3.5 text-indigo-400" /> Wireless
          </div>
          <div className="mt-2 text-2xl font-bold text-indigo-400 font-mono">{stats.wireless}</div>
        </Card>
      </div>

      {/* 3. Interface Table */}
      <Card className="bg-card/25 border border-border/50 overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[6%] font-semibold">Port</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[15%] font-semibold">Name (Alias)</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[8%] font-semibold">Role</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[20%] font-semibold">IP / Netmask</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[18%] font-semibold">Admin Access</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[10%] font-semibold">Speed</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[10%] font-semibold">Status</th>
              <TableHead className="p-3 w-[13%] text-right text-[11px] uppercase tracking-wider font-semibold">Action</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {interfaces.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="p-8 text-center text-muted-foreground text-xs">
                  ไม่พบอินเทอร์เฟซเครือข่าย
                </TableCell>
              </TableRow>
            ) : (
              interfaces.map((iface) => (
                <TableRow key={iface.id} className="border-b border-border/40 hover:bg-muted/15">
                  {/* Port Icon */}
                  <TableCell className="p-3 text-center">
                    {iface.type === "ethernet" ? (
                      <Cable className="h-5 w-5 text-cyan-400 mx-auto" />
                    ) : (
                      <Wifi className="h-5 w-5 text-indigo-400 mx-auto" />
                    )}
                  </TableCell>

                  {/* Name (Alias) */}
                  <TableCell className="p-3">
                    <div className="font-semibold text-foreground">{iface.name}</div>
                    <div className="text-xs text-muted-foreground mt-0.5">({iface.alias})</div>
                    {iface.type === "wireless" && iface.connectedSSID && iface.status === "up" && (
                      <div className="flex items-center gap-1 mt-1">
                        <Signal className="h-3 w-3 text-indigo-400" />
                        <span className="text-[10px] text-indigo-400 font-mono">{iface.connectedSSID}</span>
                      </div>
                    )}
                  </TableCell>

                  {/* Role */}
                  <TableCell className="p-3">
                    {iface.role === "WAN" ? (
                      <Badge variant="outline" className="bg-red-500/10 text-red-400 border-red-500/20 text-[10px] px-2 py-0.5 rounded font-bold">
                        WAN
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="bg-cyan-500/10 text-cyan-400 border-cyan-500/20 text-[10px] px-2 py-0.5 rounded font-bold">
                        LAN
                      </Badge>
                    )}
                  </TableCell>

                  {/* IP / Netmask */}
                  <TableCell className="p-3">
                    <div className="font-mono text-xs text-foreground">
                      {iface.status === "up" ? `${iface.ip} / ${iface.netmask}` : "—"}
                    </div>
                    <div className="text-[10px] text-muted-foreground mt-0.5">
                      {iface.addressingMode === "dhcp" ? "DHCP" : "Static"}
                    </div>
                  </TableCell>

                  {/* Admin Access */}
                  <TableCell className="p-3">
                    <div className="flex flex-wrap gap-1">
                      {iface.adminAccess.length === 0 ? (
                        <span className="text-xs text-muted-foreground/45 italic">None</span>
                      ) : (
                        iface.adminAccess.map((access) => (
                          <Badge
                            key={access}
                            variant="outline"
                            className="bg-muted/30 text-muted-foreground border-border/40 text-[9px] px-1.5 py-0.5 rounded font-mono"
                          >
                            {access}
                          </Badge>
                        ))
                      )}
                    </div>
                  </TableCell>

                  {/* Speed */}
                  <TableCell className="p-3">
                    <span className="font-mono text-xs text-muted-foreground">
                      {iface.status === "up" ? iface.speed : "—"}
                    </span>
                  </TableCell>

                  {/* Status */}
                  <TableCell className="p-3">
                    {iface.status === "up" ? (
                      <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[10px] px-2 py-0.5 rounded font-bold">
                        UP
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="bg-red-500/10 text-red-400 border-red-500/20 text-[10px] px-2 py-0.5 rounded font-bold">
                        DOWN
                      </Badge>
                    )}
                  </TableCell>

                  {/* Action */}
                  <TableCell className="p-3 text-right">
                    <div className="flex items-center justify-end gap-2">
                      <div className="flex items-center gap-1.5">
                        <span className="text-[10px] text-muted-foreground">{iface.status === "up" ? "ON" : "OFF"}</span>
                        <Switch
                           size="sm"
                          checked={iface.status === "up"}
                          onCheckedChange={() => handleToggleStatus(iface.id)}
                        />
                      </div>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => openEditDialog(iface)}
                        className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50"
                        title="แก้ไขอินเทอร์เฟซ"
                      >
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* 4. MAC Address reference table */}
      <Card className="bg-card/20 border border-border/50 p-4">
        <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground mb-3">
          Hardware Address (MAC)
        </div>
        <div className="grid gap-2 sm:grid-cols-2">
          {interfaces.map((iface) => (
            <div key={iface.id} className="flex flex-col sm:flex-row sm:items-center justify-between bg-muted/10 px-3 py-2 rounded-lg border border-border/30 gap-2">
              <div className="flex items-center gap-2">
                {iface.type === "ethernet" ? (
                  <Cable className="h-3.5 w-3.5 text-cyan-400" />
                ) : (
                  <Wifi className="h-3.5 w-3.5 text-indigo-400" />
                )}
                <span className="text-xs font-semibold text-foreground">{iface.name}</span>
                {iface.type === "wireless" && iface.macMode && iface.macMode !== "hardware" && (
                  <Badge
                    variant="outline"
                    className={`text-[9px] px-1 py-0 rounded font-normal ${iface.macMode === "randomized"
                      ? "bg-indigo-500/10 text-indigo-400 border-indigo-500/20"
                      : "bg-amber-500/10 text-amber-400 border-amber-500/20"
                      }`}
                  >
                    {iface.macMode === "randomized" ? "Randomized" : "LAA"}
                  </Badge>
                )}
                {iface.type === "wireless" && iface.macMode === "randomized" && iface.randomizeOnReconnect && (
                  <Badge
                    variant="outline"
                    className="text-[9px] px-1 py-0 rounded font-normal bg-primary/10 text-primary border-primary/20"
                    title="สุ่มที่อยู่ MAC ใหม่ทุกครั้งที่มีการเชื่อมต่อ"
                  >
                    Rotate
                  </Badge>
                )}
              </div>
              <div className="flex flex-col items-end gap-0.5">
                <span className="text-xs font-mono text-foreground">{iface.macAddress}</span>
                {iface.type === "wireless" && iface.macMode && iface.macMode !== "hardware" && (
                  <span className="text-[10px] font-mono text-muted-foreground">
                    Real: {iface.realMacAddress}
                  </span>
                )}
              </div>
            </div>
          ))}
        </div>
      </Card>

      {/* 5. Warning / Help Box */}
      <Alert className="border-dashed border-border bg-card/10">
        <AlertCircle className="h-4 w-4 text-muted-foreground" />
        <AlertTitle className="font-bold text-foreground mb-0.5">ข้อมูลสำคัญ:</AlertTitle>
        <AlertDescription className="text-xs text-muted-foreground leading-relaxed">
          การเปลี่ยนค่า IP Address หรือ Addressing Mode ของอินเทอร์เฟซอาจทำให้เชื่อมต่อกับบอร์ดไม่ได้ชั่วคราว
          กรุณาตรวจสอบค่าอย่างถี่ถ้วนก่อนบันทึก อินเทอร์เฟซที่ตั้งค่าเป็น <span className="font-semibold text-primary">"LAN"</span> ควรใช้ Static IP
          และอินเทอร์เฟซ <span className="font-semibold text-primary">"WAN"</span> ควรใช้ DHCP เพื่อรับ IP จากเราเตอร์ต้นทาง
        </AlertDescription>
      </Alert>

      {/* 6. Edit Interface Dialog */}
      <Dialog open={isEditOpen} modal={false} onOpenChange={setIsEditOpen}>
        <DialogContent className="w-full md:max-w-[920px] rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up max-h-[90vh] overflow-y-auto">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground flex items-center gap-2">
              {editingIface?.type === "ethernet" ? (
                <Cable className="h-5 w-5 text-cyan-400" />
              ) : (
                <Wifi className="h-5 w-5 text-indigo-400" />
              )}
              Edit Interface: {editingIface?.name}
              <span className="text-sm font-normal text-muted-foreground">({editingIface?.alias})</span>
            </DialogTitle>
          </DialogHeader>

          <form onSubmit={handleSave} className="space-y-4 text-sm">
            {formError && (
              <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                <AlertCircle className="h-4 w-4 text-red-400" />
                <AlertDescription className="text-red-400 text-xs">{formError}</AlertDescription>
              </Alert>
            )}

            {/* Row 1: Alias Name & Port Role */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="form-alias" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Alias Name <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="form-alias"
                  type="text"
                  required
                  value={formAlias}
                  onChange={(e) => setFormAlias(e.target.value)}
                  placeholder="เช่น LAN_Internal, WAN_WiFi"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono"
                />
                <p className="text-[11px] text-muted-foreground italic">ห้ามเว้นวรรค ใช้ได้เฉพาะอักษรภาษาอังกฤษ ตัวเลข และ _</p>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="form-role" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Port Role (หน้าที่ของพอร์ต) <span className="text-red-500">*</span>
                </Label>
                <Select
                  value={formRole}
                  onValueChange={(value: "LAN" | "WAN") => setFormRole(value)}
                >
                  <SelectTrigger id="form-role" className="bg-background/50 border-border/80 h-9 text-xs font-semibold text-foreground">
                    <SelectValue placeholder="เลือกประเภทพอร์ต" />
                  </SelectTrigger>
                  <SelectContent className="border border-border/80 bg-popover text-foreground rounded-md text-xs font-semibold">
                    <SelectItem value="LAN">LAN (วงภายใน)</SelectItem>
                    <SelectItem value="WAN">WAN (ต่อขายนอก / อินเทอร์เน็ต)</SelectItem>
                  </SelectContent>
                </Select>
                <p className="text-[11px] text-muted-foreground italic">LAN สำหรับเครือข่ายภายใน และ WAN สำหรับเชื่อมต่อเครือข่ายภายนอก</p>
              </div>
            </div>

            {/* Field: Addressing Mode */}
            <div className="space-y-1.5">
              <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Addressing Mode
              </Label>
              <div className="flex rounded-lg border border-border bg-background p-0.5 gap-0.5 w-fit">
                <button
                  type="button"
                  onClick={() => setFormMode("dhcp")}
                  className={`px-4 py-1.5 text-xs font-bold rounded-md transition cursor-pointer ${formMode === "dhcp"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
                    }`}
                >
                  DHCP (Auto)
                </button>
                <button
                  type="button"
                  onClick={() => setFormMode("static")}
                  className={`px-4 py-1.5 text-xs font-bold rounded-md transition cursor-pointer ${formMode === "static"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:text-foreground hover:bg-muted/30"
                    }`}
                >
                  Manual (Static)
                </button>
              </div>
            </div>

            {/* Static IP Fields (conditional) */}
            {formMode === "static" && (
              <div className="space-y-3 border border-border/40 rounded-lg p-4 bg-muted/5">
                <div className="text-xs font-semibold text-muted-foreground uppercase tracking-wider flex items-center gap-1.5">
                  <ArrowUpDown className="h-3 w-3" /> Static IP Configuration
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1">
                    <Label htmlFor="form-ip" className="text-[11px] text-muted-foreground">IP Address <span className="text-red-500">*</span></Label>
                    <Input
                      id="form-ip"
                      type="text"
                      value={formIp}
                      onChange={(e) => setFormIp(e.target.value)}
                      placeholder="192.168.1.1"
                      className="bg-background/50 h-8 font-mono text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="form-netmask" className="text-[11px] text-muted-foreground">Netmask (CIDR) <span className="text-red-500">*</span></Label>
                    <Input
                      id="form-netmask"
                      type="text"
                      value={formNetmask}
                      onChange={(e) => setFormNetmask(e.target.value)}
                      placeholder="24"
                      className="bg-background/50 h-8 font-mono text-xs"
                    />
                  </div>
                </div>
                <div className="space-y-1">
                  <Label htmlFor="form-gateway" className="text-[11px] text-muted-foreground">Gateway</Label>
                  <Input
                    id="form-gateway"
                    type="text"
                    value={formGateway}
                    onChange={(e) => setFormGateway(e.target.value)}
                    placeholder="192.168.1.254"
                    className="bg-background/50 h-8 font-mono text-xs"
                  />
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <div className="space-y-1">
                    <Label htmlFor="form-dns1" className="text-[11px] text-muted-foreground">DNS Primary</Label>
                    <Input
                      id="form-dns1"
                      type="text"
                      value={formDns1}
                      onChange={(e) => setFormDns1(e.target.value)}
                      placeholder="8.8.8.8"
                      className="bg-background/50 h-8 font-mono text-xs"
                    />
                  </div>
                  <div className="space-y-1">
                    <Label htmlFor="form-dns2" className="text-[11px] text-muted-foreground">DNS Secondary</Label>
                    <Input
                      id="form-dns2"
                      type="text"
                      value={formDns2}
                      onChange={(e) => setFormDns2(e.target.value)}
                      placeholder="1.1.1.1"
                      className="bg-background/50 h-8 font-mono text-xs"
                    />
                  </div>
                </div>
              </div>
            )}

            {/* Wi-Fi Settings (only for wireless) */}
            {editingIface?.type === "wireless" && (
              <>
                <div className="space-y-3 border border-indigo-500/20 rounded-lg p-4 bg-indigo-500/5">
                  <div className="text-xs font-semibold text-indigo-400 uppercase tracking-wider flex items-center gap-1.5">
                    <Wifi className="h-3.5 w-3.5" /> Wireless Client Settings
                  </div>

                  {/* SSID with Scanner */}
                  <div className="space-y-1.5">
                    <Label htmlFor="form-ssid" className="text-[11px] text-muted-foreground">
                      SSID <span className="text-red-500">*</span>
                    </Label>
                    <div className="flex gap-2">
                      <Input
                        id="form-ssid"
                        type="text"
                        value={formSSID}
                        onChange={(e) => setFormSSID(e.target.value)}
                        placeholder="ชื่อเครือข่าย Wi-Fi"
                        className="bg-background/50 h-8 font-mono text-xs flex-1"
                      />
                      <Button
                        type="button"
                        variant="outline"
                        size="sm"
                        onClick={handleWifiScan}
                        disabled={isScanning}
                        className="cursor-pointer gap-1 text-xs h-8 px-3 border-indigo-500/30 text-indigo-400 hover:bg-indigo-500/10 hover:text-indigo-300"
                      >
                        <RefreshCw className={`h-3 w-3 ${isScanning ? "animate-spin" : ""}`} />
                        {isScanning ? "Scanning..." : "Scan"}
                      </Button>
                    </div>
                  </div>

                  {/* Scan Results */}
                  {showScanResults && (
                    <div className="rounded-lg border border-border/40 bg-background/30 overflow-hidden">
                      {isScanning ? (
                        <div className="flex items-center justify-center gap-2 py-6 text-xs text-muted-foreground">
                          <RefreshCw className="h-4 w-4 animate-spin text-indigo-400" />
                          กำลังค้นหาเครือข่าย Wi-Fi...
                        </div>
                      ) : (
                        <div className="max-h-[200px] overflow-y-auto">
                          {scanResults.map((wifi, idx) => (
                            <button
                              key={idx}
                              type="button"
                              onClick={() => selectSSID(wifi.ssid)}
                              className={`w-full flex items-center justify-between px-3 py-2 text-xs transition cursor-pointer hover:bg-muted/20 border-b border-border/20 last:border-b-0 ${formSSID === wifi.ssid ? "bg-primary/10" : ""
                                }`}
                            >
                              <div className="flex items-center gap-2">
                                <SignalBar signal={wifi.signal} />
                                <span className="font-semibold text-foreground">{wifi.ssid}</span>
                                {wifi.security !== "Open" ? (
                                  <Lock className="h-3 w-3 text-muted-foreground" />
                                ) : (
                                  <Unlock className="h-3 w-3 text-amber-400" />
                                )}
                                {formSSID === wifi.ssid && (
                                  <Check className="h-3 w-3 text-primary" />
                                )}
                              </div>
                              <div className="flex items-center gap-3 text-muted-foreground">
                                <span className={signalColor(wifi.signal)}>{wifi.signal}%</span>
                                <span className="text-[10px]">{wifi.security}</span>
                                <span className="text-[10px]">Ch.{wifi.channel}</span>
                                <Badge variant="outline" className="text-[9px] px-1 py-0 rounded border-border/40">
                                  {wifi.frequency}
                                </Badge>
                              </div>
                            </button>
                          ))}
                        </div>
                      )}
                    </div>
                  )}

                  {/* Wi-Fi Password */}
                  <div className="space-y-1">
                    <Label htmlFor="form-wifi-password" className="text-[11px] text-muted-foreground">
                      Password (PSK)
                    </Label>
                    <Input
                      id="form-wifi-password"
                      type="password"
                      value={formWifiPassword}
                      onChange={(e) => setFormWifiPassword(e.target.value)}
                      placeholder="••••••••"
                      className="bg-background/50 h-8 font-mono text-xs"
                    />
                    <p className="text-[10px] text-muted-foreground italic">เว้นว่างหากไม่ต้องการเปลี่ยนรหัสผ่าน</p>
                  </div>
                </div>

                {/* Wi-Fi MAC Address Settings */}
                <div className="space-y-3 border border-indigo-500/20 rounded-lg p-4 bg-indigo-500/5">
                  <div className="text-xs font-semibold text-indigo-400 uppercase tracking-wider flex items-center gap-1.5">
                    <Shield className="h-3.5 w-3.5" /> MAC Address Settings (การตั้งค่า MAC)
                  </div>
                  {/* MAC Address Mode selection */}
                  <div className="space-y-1.5">
                    <Label htmlFor="form-mac-mode" className="text-[11px] font-semibold text-muted-foreground uppercase tracking-wider block">
                      MAC Address Mode
                    </Label>
                    <Select
                      value={formMacMode}
                      onValueChange={(value: "hardware" | "randomized" | "laa") => setFormMacMode(value)}
                    >
                      <SelectTrigger id="form-mac-mode" size="sm" className="w-full sm:w-[220px] bg-background border-border/80 text-xs font-semibold text-foreground focus-visible:ring-indigo-500/20 focus-visible:border-indigo-500">
                        <SelectValue placeholder="เลือกโหมด MAC Address" />
                      </SelectTrigger>
                      <SelectContent className="border border-border/80 bg-popover text-foreground rounded-md text-xs font-semibold">
                        <SelectItem value="hardware">Hardware MAC (ที่อยู่จริง)</SelectItem>
                        <SelectItem value="randomized">Randomized MAC (สุ่มที่อยู่)</SelectItem>
                        <SelectItem value="laa">LAA MAC (กำหนดเอง)</SelectItem>
                      </SelectContent>
                    </Select>
                  </div>

                  {/* Randomized MAC Details */}
                  {formMacMode === "randomized" && (
                    <div className="space-y-3 pt-1 animate-fade-in">
                      <div className="space-y-1">
                        <Label htmlFor="form-randomized-mac" className="text-[11px] text-muted-foreground">
                          Randomized MAC Address (ค่าที่สุ่มได้)
                        </Label>
                        <div className="flex gap-2">
                          <Input
                            id="form-randomized-mac"
                            type="text"
                            readOnly
                            value={formRandomizedMac}
                            className="bg-background/30 h-8 font-mono text-xs flex-1 cursor-not-allowed select-all border-indigo-500/10"
                          />
                          <Button
                            type="button"
                            variant="outline"
                            size="sm"
                            onClick={() => setFormRandomizedMac(generateRandomMac())}
                            className="cursor-pointer gap-1 text-xs h-8 px-3 border-indigo-500/30 text-indigo-400 hover:bg-indigo-500/10 hover:text-indigo-300"
                          >
                            <RefreshCw className="h-3 w-3" />
                            สุ่มใหม่
                          </Button>
                        </div>
                      </div>

                      <div className="flex items-center justify-between p-2.5 rounded-lg bg-background/30 border border-border/20">
                        <div className="space-y-0.5 pr-2">
                          <Label htmlFor="form-randomize-reconnect" className="text-xs font-semibold text-foreground block cursor-pointer">
                            สุ่มใหม่ทุกครั้งที่เชื่อมต่อใหม่
                          </Label>
                          <span className="text-[10px] text-muted-foreground block leading-relaxed">
                            สุ่ม MAC Address ชุดใหม่โดยอัตโนมัติเมื่อตัดการทำงานหรือสัญญาณหลุด (Reconnect)
                          </span>
                        </div>
                        <Switch
                          id="form-randomize-reconnect"
                          size="sm"
                          checked={formRandomizeOnReconnect}
                          onCheckedChange={formMacMode === "randomized" ? setFormRandomizeOnReconnect : undefined}
                        />
                      </div>
                    </div>
                  )}

                  {/* LAA Details */}
                  {formMacMode === "laa" && (
                    <div className="space-y-1.5 pt-1 animate-fade-in">
                      <Label htmlFor="form-laa-mac" className="text-[11px] text-muted-foreground">
                        Locally Administered MAC Address (LAA) <span className="text-red-500">*</span>
                      </Label>
                      <Input
                        id="form-laa-mac"
                        type="text"
                        value={formLaaMac}
                        onChange={(e) => setFormLaaMac(e.target.value.toUpperCase())}
                        placeholder="เช่น 9A:11:22:33:44:55"
                        className="bg-background/50 h-8 font-mono text-xs border-indigo-500/20 focus-visible:ring-indigo-500"
                      />
                      <p className="text-[10px] text-amber-400/90 italic leading-relaxed">
                        * มาตรฐาน LAA: อักขระหลักที่ 2 ของกลุ่มแรกต้องเป็น 2, 6, A หรือ E (เช่น X2:XX:XX:XX:XX:XX)
                      </p>
                    </div>
                  )}

                  {/* Comparison Panel */}
                  <div className="mt-1 text-xs bg-background/40 p-3 rounded-lg border border-indigo-500/10 space-y-1.5 font-mono">
                    <div className="flex justify-between">
                      <span className="text-muted-foreground">ที่อยู่ MAC จริง (Hardware):</span>
                      <span className="text-foreground">{editingIface?.realMacAddress || editingIface?.macAddress}</span>
                    </div>
                    <div className="flex justify-between border-t border-indigo-500/5 pt-1.5">
                      <span className="text-muted-foreground">ที่อยู่ MAC ที่ใช้จริง (Effective):</span>
                      <span className="text-indigo-400 font-bold">
                        {formMacMode === "hardware"
                          ? (editingIface?.realMacAddress || editingIface?.macAddress)
                          : formMacMode === "randomized"
                            ? formRandomizedMac
                            : formLaaMac || "—"}
                      </span>
                    </div>
                  </div>
                </div>

                {/* Wi-Fi Failover / Backup SSID Settings */}
                <div className="space-y-3 border border-indigo-500/20 rounded-lg p-4 bg-indigo-500/5 mt-4">
                  <div className="flex items-center justify-between">
                    <div className="text-xs font-semibold text-indigo-400 uppercase tracking-wider flex items-center gap-1.5">
                      <Radio className="h-3.5 w-3.5" /> Wi-Fi Backup & Failover (ฟีเจอร์สำรองข้อมูลคลื่น)
                    </div>
                    <Switch
                      size="sm"
                      checked={formFailoverEnabled}
                      onCheckedChange={setFormFailoverEnabled}
                    />
                  </div>

                  {formFailoverEnabled && (
                    <div className="space-y-4 pt-1 animate-fade-in text-xs">
                      {/* Optional Backup SSID */}
                      <div className="grid gap-3 sm:grid-cols-2">
                        <div className="space-y-1.5">
                          <Label htmlFor="form-backup-ssid" className="text-[11px] text-muted-foreground">
                            Backup SSID (ชื่อ Wi-Fi สำรอง)
                          </Label>
                          <Input
                            id="form-backup-ssid"
                            type="text"
                            value={formBackupSSID}
                            onChange={(e) => setFormBackupSSID(e.target.value)}
                            placeholder="ชื่อเครือข่าย Wi-Fi สำรอง"
                            className="bg-background/50 h-8 font-mono text-xs"
                          />
                        </div>
                        <div className="space-y-1.5">
                          <Label htmlFor="form-backup-wifi-password" className="text-[11px] text-muted-foreground">
                            Backup Password (รหัสผ่านสำรอง)
                          </Label>
                          <Input
                            id="form-backup-wifi-password"
                            type="password"
                            value={formBackupWifiPassword}
                            onChange={(e) => setFormBackupWifiPassword(e.target.value)}
                            placeholder="รหัสผ่านสำรอง"
                            className="bg-background/50 h-8 font-mono text-xs"
                          />
                        </div>
                      </div>

                      {/* Settings grid */}
                      <div className="grid gap-3 sm:grid-cols-3">
                        <div className="space-y-1.5">
                          <Label htmlFor="form-ip-check-timeout" className="text-[11px] text-muted-foreground block" title="เวลาที่ใช้ในการตรวจสอบการตอบกลับ IP ก่อนพิจารณาว่าล้มเหลว">
                            IP Check Timeout (วินาที)
                          </Label>
                          <Input
                            id="form-ip-check-timeout"
                            type="number"
                            min="5"
                            max="300"
                            value={formIpCheckTimeout}
                            onChange={(e) => setFormIpCheckTimeout(parseInt(e.target.value) || 15)}
                            className="bg-background/50 h-8 font-mono text-xs"
                          />
                        </div>
                        <div className="space-y-1.5">
                          <Label htmlFor="form-primary-max-retries" className="text-[11px] text-muted-foreground block" title="จำนวนครั้งในการเปิด/ปิดพอร์ตใหม่เพื่อเชื่อมต่อ SSID หลัก">
                            Max Retries (ครั้ง)
                          </Label>
                          <Input
                            id="form-primary-max-retries"
                            type="number"
                            min="1"
                            max="10"
                            value={formPrimaryMaxRetries}
                            onChange={(e) => setFormPrimaryMaxRetries(parseInt(e.target.value) || 3)}
                            className="bg-background/50 h-8 font-mono text-xs"
                          />
                        </div>
                        <div className="space-y-1.5">
                          <Label htmlFor="form-failover-cooldown" className="text-[11px] text-muted-foreground block" title="ระยะหน่วงเวลาก่อนสลับกลับมาลองเชื่อมต่อ SSID หลักอีกครั้ง">
                            Cooldown Delay (วินาที)
                          </Label>
                          <Input
                            id="form-failover-cooldown"
                            type="number"
                            min="10"
                            max="3600"
                            value={formFailoverCooldown}
                            onChange={(e) => setFormFailoverCooldown(parseInt(e.target.value) || 60)}
                            className="bg-background/50 h-8 font-mono text-xs"
                          />
                        </div>
                      </div>

                      {/* Interactive Simulator Section */}
                      <div className="border border-border/60 rounded-lg p-3 bg-background/40 space-y-2">
                        <div className="flex items-center justify-between">
                          <span className="font-semibold text-xs text-foreground flex items-center gap-1.5">
                            <Terminal className="h-3.5 w-3.5 text-indigo-400" /> Failover Simulator (ตัวจำลองการสลับคลื่น)
                          </span>
                          <Button
                            type="button"
                            size="sm"
                            onClick={runFailoverSimulation}
                            disabled={simActive}
                            className="cursor-pointer h-7 px-2.5 bg-indigo-500 text-neutral-950 hover:bg-indigo-400 font-bold gap-1 text-[11px]"
                          >
                            <Play className="h-3 w-3 fill-neutral-950" />
                            {simActive ? "Simulating..." : "Simulate Failover"}
                          </Button>
                        </div>

                        {simLogs.length > 0 && (
                          <div className="bg-muted/50 dark:bg-black/60 rounded p-2 text-[10px] font-mono text-cyan-600 dark:text-cyan-400 max-h-[140px] overflow-y-auto space-y-1 border border-border/50 dark:border-border/20 scrollbar-thin">
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
              <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                <Shield className="h-3 w-3 inline mr-1" />
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
                      className={`flex items-center gap-1.5 px-3 py-1.5 rounded-lg border text-xs font-semibold transition cursor-pointer ${isActive
                        ? "border-primary/40 bg-primary/10 text-primary"
                        : "border-border/40 bg-muted/10 text-muted-foreground hover:bg-muted/20 hover:text-foreground"
                        }`}
                    >
                      <div className={`w-3.5 h-3.5 rounded border flex items-center justify-center ${isActive ? "bg-primary border-primary" : "border-muted-foreground/40"
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
            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border/40">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsEditOpen(false)}
                className="cursor-pointer text-muted-foreground hover:bg-muted/30"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold px-5"
              >
                Save Changes
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
