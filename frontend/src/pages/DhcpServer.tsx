import { useState, useMemo, useRef, useEffect } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Radio,
  Plus,
  Search,
  Edit,
  Trash2,
  AlertCircle,
  RefreshCw,
  Check,
  CheckCircle2,
  Info,
  Server,
  Network,
  Save,
  ArrowRightLeft,
  Loader2
} from "lucide-react"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import {
  Table,
  TableBody,
  TableCell,
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
import {
  type DhcpConfig,
  type DhcpReservation,
  type ActiveDhcpLease
} from "@/data-mockup/mockData"
import { dhcpService } from "@/services/dhcpService"
import { useAlert } from "@/hooks/useAlert"
import { isValidIp } from "@/lib/utils"

export default function DhcpServer() {
  const { alert, confirm } = useAlert()

  // --- State ---
  const [configs, setConfigs] = useState<DhcpConfig[]>([])
  const [availableInterfaces, setAvailableInterfaces] = useState<string[]>([])
  const [reservations, setReservations] = useState<DhcpReservation[]>([])
  const [activeLeases, setActiveLeases] = useState<ActiveDhcpLease[]>([])
  const [isLoading, setIsLoading] = useState(true)

  // Search queries
  const [resSearchQuery, setResSearchQuery] = useState("")
  const [leaseSearchQuery, setLeaseSearchQuery] = useState("")

  // Modals state
  const [isConfigModalOpen, setIsConfigModalOpen] = useState(false)
  const [editingConfig, setEditingConfig] = useState<DhcpConfig | null>(null)
  const [isResModalOpen, setIsResModalOpen] = useState(false)
  const [editingReservation, setEditingReservation] = useState<DhcpReservation | null>(null)

  // Form states - Configuration Pool
  const [formInterface, setFormInterface] = useState("")
  const [formStartIp, setFormStartIp] = useState("")
  const [formEndIp, setFormEndIp] = useState("")
  const [formGateway, setFormGateway] = useState("")
  const [formNetmask, setFormNetmask] = useState("255.255.255.0")
  const [formDns1, setFormDns1] = useState("8.8.8.8")
  const [formDns2, setFormDns2] = useState("1.1.1.1")
  const [formLeaseTime, setFormLeaseTime] = useState("86400")
  const [configError, setConfigError] = useState("")

  // Form states - Static Reservation Modal
  const [resDeviceName, setResDeviceName] = useState("")
  const [resMacAddress, setResMacAddress] = useState("")
  const [resIpAddress, setResIpAddress] = useState("")
  const [resError, setResError] = useState("")

  // Apply & Save states
  const [isApplying, setIsApplying] = useState(false)
  const [isApplied, setIsApplied] = useState(true)
  const [isSavingConfig, setIsSavingConfig] = useState(false)
  const [isRefreshingLeases, setIsRefreshingLeases] = useState(false)

  const dialogContentRef = useRef<HTMLDivElement | null>(null)

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
        const [cfgs, res, leases, avIface] = await Promise.all([
          dhcpService.getConfigs(),
          dhcpService.getReservations(),
          dhcpService.getActiveLeases(),
          dhcpService.getAvailableInterfaces()
        ])
        setConfigs(cfgs || [])
        setReservations(res || [])
        setActiveLeases(leases || [])
        setAvailableInterfaces(avIface || [])
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูล DHCP ได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

  // --- Helpers ---
  const macRegex = /^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$/

  const ipToNum = (ip: string) => {
    const parts = ip.split(".").map(Number)
    return (parts[0] * 16777216) + (parts[1] * 65536) + (parts[2] * 256) + parts[3]
  }

  // --- Statistics ---
  const stats = useMemo(() => {
    const activeConfigs = configs.filter(c => c.enabled)
    const interfacesStr = activeConfigs.map(c => c.interface).join(", ")
    return {
      status: activeConfigs.length > 0 ? "Active" : "Inactive",
      activeInterfaces: interfacesStr || "—",
      reservationsCount: reservations.length,
      activeLeasesCount: activeLeases.length
    }
  }, [configs, reservations, activeLeases])

  // --- Filtered lists ---
  const filteredReservations = useMemo(() => {
    return reservations.filter(res => {
      const query = resSearchQuery.toLowerCase()
      return (
        res.deviceName.toLowerCase().includes(query) ||
        res.macAddress.toLowerCase().includes(query) ||
        res.ipAddress.toLowerCase().includes(query)
      )
    })
  }, [reservations, resSearchQuery])

  const filteredLeases = useMemo(() => {
    return activeLeases.filter(lease => {
      const query = leaseSearchQuery.toLowerCase()
      return (
        lease.hostname.toLowerCase().includes(query) ||
        lease.macAddress.toLowerCase().includes(query) ||
        lease.ipAddress.toLowerCase().includes(query) ||
        (lease.interface && lease.interface.toLowerCase().includes(query))
      )
    })
  }, [activeLeases, leaseSearchQuery])

  // --- Config Form CRUD ---
  const openCreateConfigModal = async () => {
    try {
      const avIface = await dhcpService.getAvailableInterfaces() || []
      setAvailableInterfaces(avIface)
      if (avIface.length === 0) {
        await alert("แจ้งเตือน", "ไม่เหลือ LAN Interface ว่างที่พร้อมสำหรับเปิด DHCP Server")
        return
      }
      setEditingConfig(null)
      setFormInterface(avIface[0])
      setFormStartIp("")
      setFormEndIp("")
      setFormGateway("")
      setFormNetmask("255.255.255.0")
      setFormDns1("8.8.8.8")
      setFormDns2("1.1.1.1")
      setFormLeaseTime("86400")
      setConfigError("")
      setIsConfigModalOpen(true)
    } catch (e) {
      console.error(e)
    }
  }

  const openEditConfigModal = (cfg: DhcpConfig) => {
    setEditingConfig(cfg)
    setFormInterface(cfg.interface)
    setFormStartIp(cfg.startIp)
    setFormEndIp(cfg.endIp)
    setFormGateway(cfg.gateway)
    setFormNetmask(cfg.netmask)
    setFormDns1(cfg.dns1)
    setFormDns2(cfg.dns2)
    setFormLeaseTime(cfg.leaseTime.toString())
    setConfigError("")
    setIsConfigModalOpen(true)
  }

  const handleToggleConfig = async (id: string, checked: boolean) => {
    try {
      await dhcpService.toggleConfig(id)
      setConfigs(prev => prev.map(c => c.id === id ? { ...c, enabled: checked } : c))
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปิด/ปิดบริการ DHCP ได้: " + getErrorMessage(err))
    }
  }

  const handleDeleteConfig = async (id: string, name: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบคอนฟิก DHCP ของอินเตอร์เฟส "${name}" ใช่หรือไม่?`)) {
      try {
        await dhcpService.deleteConfig(id)
        setConfigs(prev => prev.filter(c => c.id !== id))
        const avIface = await dhcpService.getAvailableInterfaces()
        setAvailableInterfaces(avIface)
        setIsApplied(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบคอนฟิกได้: " + getErrorMessage(err))
      }
    }
  }

  const handleSaveConfig = async (e: React.FormEvent) => {
    e.preventDefault()
    setConfigError("")
    setIsSavingConfig(true)

    // Validations
    if (!isValidIp(formStartIp)) {
      setConfigError("Starting IP Address ไม่ถูกต้อง")
      setIsSavingConfig(false)
      return
    }
    if (!isValidIp(formEndIp)) {
      setConfigError("Ending IP Address ไม่ถูกต้อง")
      setIsSavingConfig(false)
      return
    }
    if (!isValidIp(formGateway)) {
      setConfigError("Default Gateway IP Address ไม่ถูกต้อง")
      setIsSavingConfig(false)
      return
    }
    if (!isValidIp(formNetmask)) {
      setConfigError("Subnet Mask IP Address ไม่ถูกต้อง")
      setIsSavingConfig(false)
      return
    }
    if (formDns1 && !isValidIp(formDns1)) {
      setConfigError("DNS Server 1 IP Address ไม่ถูกต้อง")
      setIsSavingConfig(false)
      return
    }
    if (formDns2 && !isValidIp(formDns2)) {
      setConfigError("DNS Server 2 IP Address ไม่ถูกต้อง")
      setIsSavingConfig(false)
      return
    }

    const leaseTimeVal = parseInt(formLeaseTime, 10)
    if (isNaN(leaseTimeVal) || leaseTimeVal <= 0) {
      setConfigError("Lease Time ต้องเป็นตัวเลขจำนวนเต็มที่มากกว่า 0")
      setIsSavingConfig(false)
      return
    }

    if (ipToNum(formStartIp) > ipToNum(formEndIp)) {
      setConfigError("Starting IP ต้องมีค่าน้อยกว่าหรือเท่ากับ Ending IP")
      setIsSavingConfig(false)
      return
    }

    try {
      const cfgPayload: DhcpConfig = {
        enabled: editingConfig ? editingConfig.enabled : true,
        interface: formInterface,
        startIp: formStartIp,
        endIp: formEndIp,
        gateway: formGateway,
        netmask: formNetmask,
        dns1: formDns1,
        dns2: formDns2,
        leaseTime: leaseTimeVal
      }

      if (editingConfig) {
        cfgPayload.id = editingConfig.id
        await dhcpService.updateConfig(editingConfig.id!, cfgPayload)
        setConfigs(prev => prev.map(c => c.id === editingConfig.id ? { ...cfgPayload } : c))
      } else {
        const newCfg = await dhcpService.createConfig(cfgPayload)
        setConfigs(prev => [...prev, newCfg])
      }

      const avIface = await dhcpService.getAvailableInterfaces()
      setAvailableInterfaces(avIface)

      setIsConfigModalOpen(false)
      setIsSavingConfig(false)
      setIsApplied(false)
    } catch (err) {
      setIsSavingConfig(false)
      setConfigError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    }
  }

  const handleApplySettings = async () => {
    setIsApplying(true)
    try {
      await dhcpService.apply()
      setIsApplying(false)
      setIsApplied(true)
    } catch (err) {
      setIsApplying(false)
      await alert("ข้อผิดพลาด", "ไม่สามารถเริ่มระบบ DHCP เข้ากับ OS Kernel ได้: " + getErrorMessage(err))
    }
  }

  const handleRefreshLeases = async () => {
    setIsRefreshingLeases(true)
    try {
      const leases = await dhcpService.getActiveLeases(true)
      setActiveLeases(leases)
    } catch (err) {
      console.error(err)
    } finally {
      setIsRefreshingLeases(false)
    }
  }

  // --- Static Reservations CRUD ---
  const openCreateResModal = () => {
    setEditingReservation(null)
    setResDeviceName("")
    setResMacAddress("")
    setResIpAddress("")
    setResError("")
    setIsResModalOpen(true)
  }

  const openEditResModal = (res: DhcpReservation) => {
    setEditingReservation(res)
    setResDeviceName(res.deviceName)
    setResMacAddress(res.macAddress)
    setResIpAddress(res.ipAddress)
    setResError("")
    setIsResModalOpen(true)
  }

  const handleDeleteReservation = async (id: string, name: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบการจองไอพีของอุปกรณ์ "${name}" ใช่หรือไม่?`)) {
      try {
        await dhcpService.deleteReservation(id)
        setReservations(prev => prev.filter(res => res.id !== id))
        setIsApplied(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบข้อมูลการจองได้: " + getErrorMessage(err))
      }
    }
  }

  const handleSaveReservation = async (e: React.FormEvent) => {
    e.preventDefault()
    setResError("")

    const name = resDeviceName.trim()
    const mac = resMacAddress.trim().toUpperCase()
    const ip = resIpAddress.trim()

    if (!name) {
      setResError("กรุณากรอกชื่ออุปกรณ์")
      return
    }
    if (!macRegex.test(mac)) {
      setResError("รูปแบบ MAC Address ไม่ถูกต้อง (ต้องเป็น XX:XX:XX:XX:XX:XX)")
      return
    }
    if (!isValidIp(ip)) {
      setResError("รูปแบบ IP Address ไม่ถูกต้อง")
      return
    }

    // Check duplicates
    const isIpDuplicate = reservations.some(
      r => r.ipAddress === ip && (!editingReservation || r.id !== editingReservation.id)
    )
    if (isIpDuplicate) {
      setResError(`IP Address ${ip} ถูกจองไว้โดยอุปกรณ์อื่นแล้ว`)
      return
    }

    const isMacDuplicate = reservations.some(
      r => r.macAddress === mac && (!editingReservation || r.id !== editingReservation.id)
    )
    if (isMacDuplicate) {
      setResError(`MAC Address ${mac} ถูกจองไว้โดยอุปกรณ์อื่นแล้ว`)
      return
    }

    try {
      if (editingReservation) {
        await dhcpService.updateReservation(editingReservation.id, {
          deviceName: name,
          macAddress: mac,
          ipAddress: ip
        })
        setReservations(prev => prev.map(r =>
          r.id === editingReservation.id
            ? { ...r, deviceName: name, macAddress: mac, ipAddress: ip }
            : r
        ))
      } else {
        const newRes = await dhcpService.createReservation({
          deviceName: name,
          macAddress: mac,
          ipAddress: ip
        })
        setReservations(prev => [...prev, newRes])
      }
      setIsResModalOpen(false)
      setIsApplied(false)
    } catch (err) {
      setResError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูลการจอง")
    }
  }

  const handleConvertLeaseToReservation = (lease: ActiveDhcpLease) => {
    setEditingReservation(null)
    setResDeviceName(lease.hostname || "Device_Reserved")
    setResMacAddress(lease.macAddress)
    setResIpAddress(lease.ipAddress)
    setResError("")
    setIsResModalOpen(true)
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="ml-2 text-sm text-muted-foreground">กำลังโหลดข้อมูล DHCP...</span>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 1. Header Area */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Radio className="h-7 w-7 text-primary fill-primary/10" />
            DHCP Server (ระบบจ่ายไอพีแอดเดรส)
          </h1>
          <p className="text-muted-foreground mt-1">
            กำหนดค่าการแจกจ่ายไอพีแอดเดรสอัตโนมัติภายในเครือข่าย และจัดการการจองไอพีคงที่ (Static Leases)
          </p>
        </div>
        <div className="flex items-center gap-3">
          {!isApplied && (
            <Button
              onClick={handleApplySettings}
              disabled={isApplying}
              className="cursor-pointer bg-amber-500 text-neutral-950 hover:bg-amber-400 font-bold gap-1.5 h-10 px-4 animate-pulse"
            >
              {isApplying ? (
                <>
                  <RefreshCw className="h-4 w-4 animate-spin" />
                  Applying DHCP...
                </>
              ) : (
                <>
                  <Check className="h-4.5 w-4.5" />
                  Apply DHCP Config
                </>
              )}
            </Button>
          )}
          {isApplied && (
            <div className="hidden sm:flex items-center gap-1.5 text-xs text-primary bg-primary/10 border border-primary/20 px-3 py-2 rounded-lg font-semibold">
              <CheckCircle2 className="h-4 w-4" />
              DHCP Service Active
            </div>
          )}
        </div>
      </div>

      {/* 2. Stats Dashboard Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">สถานะบริการ</div>
          <div className={`mt-2 text-2xl font-bold font-mono ${configs.some(c => c.enabled) ? "text-primary" : "text-muted-foreground"}`}>
            {stats.status}
          </div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">อินเตอร์เฟสหลัก</div>
          <div className="mt-2 text-sm font-bold text-foreground font-mono truncate" title={stats.activeInterfaces}>
            {stats.activeInterfaces}
          </div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Network className="h-3.5 w-3.5 text-primary" /> การจองไอพีคงที่ (Static)
          </div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.reservationsCount}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Server className="h-3.5 w-3.5 text-amber-400" /> ไอพีที่จ่ายใช้งานอยู่
          </div>
          <div className="mt-2 text-2xl font-bold text-amber-400 font-mono">{stats.activeLeasesCount}</div>
        </Card>
      </div>

      {/* 3. Config Cards Section */}
      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <h2 className="text-lg font-bold text-foreground flex items-center gap-2">
            <Server className="h-5 w-5 text-primary" />
            IP Address Pools Configuration (การตั้งค่ากลุ่มจ่ายไอพี)
          </h2>
          {availableInterfaces.length > 0 && (
            <Button
              onClick={openCreateConfigModal}
              size="sm"
              className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1 h-8 text-xs"
            >
              <Plus className="h-3.5 w-3.5" />
              Add DHCP Config
            </Button>
          )}
        </div>

        <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-6">
          {configs.map((cfg) => (
            <Card key={cfg.id} className="bg-card/25 border border-border/50 p-6 space-y-4 flex flex-col justify-between">
              <div className="space-y-3">
                <div className="flex items-center justify-between border-b border-border/40 pb-3">
                  <div className="flex items-center gap-2">
                    <Network className="h-5 w-5 text-primary" />
                    <div>
                      <h3 className="font-bold text-foreground text-sm">{cfg.interface}</h3>
                      <p className="text-[11px] text-muted-foreground">DHCP Server Config</p>
                    </div>
                  </div>
                  <div className="flex items-center gap-2">
                    <Switch
                      checked={cfg.enabled}
                      onCheckedChange={(checked) => handleToggleConfig(cfg.id!, checked)}
                      className="data-[state=checked]:bg-primary scale-75 cursor-pointer"
                    />
                    <Badge variant={cfg.enabled ? "default" : "secondary"} className="text-[10px] px-1.5 font-bold">
                      {cfg.enabled ? "ACTIVE" : "INACTIVE"}
                    </Badge>
                  </div>
                </div>

                {/* Config Details */}
                <div className="grid grid-cols-2 gap-y-2.5 gap-x-4 text-xs font-mono">
                  <div>
                    <span className="text-muted-foreground block text-[10px] uppercase font-semibold tracking-wider font-sans">IP Range</span>
                    <span className="text-foreground font-medium">{cfg.startIp} - {cfg.endIp}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground block text-[10px] uppercase font-semibold tracking-wider font-sans">Subnet Mask</span>
                    <span className="text-foreground font-medium">{cfg.netmask}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground block text-[10px] uppercase font-semibold tracking-wider font-sans">Gateway</span>
                    <span className="text-foreground font-medium">{cfg.gateway || "—"}</span>
                  </div>
                  <div>
                    <span className="text-muted-foreground block text-[10px] uppercase font-semibold tracking-wider font-sans">Lease Time</span>
                    <span className="text-foreground font-medium">{cfg.leaseTime}s ({Math.round(cfg.leaseTime / 3600)}h)</span>
                  </div>
                  <div className="col-span-2">
                    <span className="text-muted-foreground block text-[10px] uppercase font-semibold tracking-wider font-sans">DNS Servers</span>
                    <span className="text-foreground font-medium truncate block">
                      {cfg.dns1}{cfg.dns2 ? `, ${cfg.dns2}` : ""}
                    </span>
                  </div>
                </div>
              </div>

              <div className="flex items-center justify-end gap-2 border-t border-border/30 pt-3 mt-2">
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => openEditConfigModal(cfg)}
                  className="cursor-pointer text-xs gap-1 hover:bg-muted/50 h-8 px-2"
                >
                  <Edit className="h-3.5 w-3.5" /> Edit
                </Button>
                <Button
                  variant="ghost"
                  size="sm"
                  onClick={() => handleDeleteConfig(cfg.id!, cfg.interface)}
                  className="cursor-pointer text-xs gap-1 text-red-400 hover:text-red-300 hover:bg-red-500/10 h-8 px-2"
                >
                  <Trash2 className="h-3.5 w-3.5" /> Delete
                </Button>
              </div>
            </Card>
          ))}
          {configs.length === 0 && (
            <Card className="border border-dashed border-border p-6 flex flex-col items-center justify-center text-center col-span-full">
              <span className="text-muted-foreground text-sm">ยังไม่มีอินเตอร์เฟสเปิดจ่าย DHCP config</span>
            </Card>
          )}
        </div>
      </div>

      {/* 4. Reservations & Leases split view */}
      <div className="grid grid-cols-1 lg:grid-cols-2 gap-6">
        {/* Left: Reservations */}
        <Card className="bg-card/25 border border-border/50 p-6 space-y-4">
          <div className="border-b border-border/40 pb-3 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-md font-bold text-foreground flex items-center gap-2">
              <Network className="h-4.5 w-4.5 text-primary" />
              MAC / IP Reservation (จองไอพีแอดเดรสคงที่)
            </h2>
            <Button
              onClick={openCreateResModal}
              size="sm"
              className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1 h-8 text-xs"
            >
              <Plus className="h-3.5 w-3.5" />
              Add Reservation
            </Button>
          </div>

          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground pointer-events-none" />
            <Input
              type="text"
              value={resSearchQuery}
              onChange={(e) => setResSearchQuery(e.target.value)}
              placeholder="ค้นหา Device Name, MAC หรือ IP Address..."
              className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9"
            />
          </div>

          <div className="rounded-lg border border-border/50 overflow-hidden bg-background/20">
            <Table>
              <TableHeader>
                <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Device Name</th>
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">MAC Address</th>
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Reserved IP</th>
                  <th className="p-3 w-[15%] text-right"></th>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredReservations.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="p-8 text-center text-muted-foreground text-xs">
                      {resSearchQuery ? "ไม่พบประวัติการจองตามที่ค้นหา" : "ยังไม่มีประวัติการจอง IP สำหรับอุปกรณ์คงที่"}
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredReservations.map((res) => (
                    <TableRow key={res.id} className="border-b border-border/40 hover:bg-muted/15">
                      <TableCell className="p-3">
                        <span className="font-semibold text-foreground text-xs">{res.deviceName}</span>
                      </TableCell>
                      <TableCell className="p-3 font-mono text-xs text-muted-foreground">
                        {res.macAddress}
                      </TableCell>
                      <TableCell className="p-3 font-mono text-xs">
                        <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-xs px-2 py-0.5 rounded font-mono font-medium">
                          {res.ipAddress}
                        </Badge>
                      </TableCell>
                      <TableCell className="p-3 text-right">
                        <div className="flex items-center justify-end gap-1">
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={() => openEditResModal(res)}
                            className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50"
                            title="แก้ไขการจอง"
                          >
                            <Edit className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={() => handleDeleteReservation(res.id, res.deviceName)}
                            className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10"
                            title="ลบการจอง"
                          >
                            <Trash2 className="h-3.5 w-3.5" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </div>
        </Card>

        {/* Right: Active Leases */}
        <Card className="bg-card/25 border border-border/50 p-6 space-y-4">
          <div className="border-b border-border/40 pb-3 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
            <h2 className="text-md font-bold text-foreground flex items-center gap-2">
              <Server className="h-4.5 w-4.5 text-amber-400" />
              Active DHCP Leases (อุปกรณ์ที่เชื่อมต่อในระบบขณะนี้)
            </h2>
            <Button
              onClick={handleRefreshLeases}
              disabled={isRefreshingLeases}
              variant="outline"
              size="sm"
              className="cursor-pointer font-bold gap-1.5 h-8 text-xs border-border bg-card/40 hover:bg-card text-muted-foreground hover:text-foreground"
            >
              <RefreshCw className={`h-3.5 w-3.5 ${isRefreshingLeases ? "animate-spin" : ""}`} />
              Refresh
            </Button>
          </div>

          <div className="relative">
            <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground pointer-events-none" />
            <Input
              type="text"
              value={leaseSearchQuery}
              onChange={(e) => setLeaseSearchQuery(e.target.value)}
              placeholder="ค้นหา Hostname, IP, MAC หรือ Interface..."
              className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9"
            />
          </div>

          <div className="rounded-lg border border-border/50 overflow-hidden bg-background/20">
            <Table>
              <TableHeader>
                <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">IP Address</th>
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">MAC Address</th>
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Hostname</th>
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Iface</th>
                  <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Expires</th>
                  <th className="p-3 w-[15%] text-right"></th>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredLeases.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="p-8 text-center text-muted-foreground text-xs">
                      ไม่พบอุปกรณ์เชื่อมต่อในระบบขณะนี้
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredLeases.map((lease) => {
                    const isReserved = reservations.some(r => r.macAddress === lease.macAddress)
                    return (
                      <TableRow key={lease.id} className="border-b border-border/40 hover:bg-muted/15">
                        <TableCell className="p-3">
                          <span className="font-mono text-xs font-semibold text-foreground">{lease.ipAddress}</span>
                        </TableCell>
                        <TableCell className="p-3 font-mono text-xs text-muted-foreground">
                          {lease.macAddress}
                        </TableCell>
                        <TableCell className="p-3">
                          <span className="font-semibold text-foreground text-xs truncate max-w-[90px] block" title={lease.hostname}>
                            {lease.hostname}
                          </span>
                        </TableCell>
                        <TableCell className="p-3 text-xs text-muted-foreground font-mono">
                          {lease.interface || "—"}
                        </TableCell>
                        <TableCell className="p-3 text-[11px] text-muted-foreground font-mono">
                          {lease.expiresIn}
                        </TableCell>
                        <TableCell className="p-3 text-right">
                          {isReserved ? (
                            <Badge className="bg-primary/10 text-primary border border-primary/20 text-[9px] px-1.5 py-0.5 rounded font-semibold font-mono">
                              Reserved
                            </Badge>
                          ) : (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleConvertLeaseToReservation(lease)}
                              className="cursor-pointer text-[10px] text-primary hover:text-primary hover:bg-primary/10 font-medium py-0.5 px-1.5 h-6 gap-0.5"
                              title="จองไอพีนี้"
                            >
                              <ArrowRightLeft className="h-2.5 w-2.5" />
                              Reserve
                            </Button>
                          )}
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </div>
        </Card>
      </div>

      {/* 5. Alert Help Box */}
      <Alert className="border-dashed border-border bg-card/10">
        <Info className="h-4 w-4 text-muted-foreground" />
        <AlertTitle className="font-bold text-foreground mb-0.5">ระบบ DHCP Server (Dynamic Host Configuration Protocol):</AlertTitle>
        <AlertDescription className="text-xs text-muted-foreground leading-relaxed">
          มีหน้าที่ในการแจกจ่ายข้อมูลการเชื่อมต่อเครือข่าย ได้แก่ IP Address, Netmask, Gateway และ DNS Server โดยอัตโนมัติ
          การจ่าย IP แบบหลายอินเตอร์เฟสช่วยให้ PiGate ทำงานแยกเครือข่าย (LAN Segment) ได้ เช่น eth0 และ eth1 โดยไม่เกี่ยวข้องกัน
        </AlertDescription>
      </Alert>

      {/* 6. Config Pool Add/Edit Modal Dialog */}
      <Dialog open={isConfigModalOpen} modal={false} onOpenChange={setIsConfigModalOpen}>
        <DialogContent ref={dialogContentRef} className="max-w-[500px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingConfig ? `แก้ไขการตั้งค่า DHCP (${formInterface})` : "สร้างการตั้งค่า DHCP บนอินเตอร์เฟสใหม่"}
            </DialogTitle>
          </DialogHeader>

          <form onSubmit={handleSaveConfig} className="space-y-4 text-sm">
            {configError && (
              <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                <AlertCircle className="h-4 w-4 text-red-400" />
                <AlertDescription className="text-red-400 text-xs">{configError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Interface Selection */}
            <div className="space-y-1.5">
              <Label htmlFor="modal-dhcp-iface" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Interface (อินเตอร์เฟส)
              </Label>
              {editingConfig ? (
                <Input
                  id="modal-dhcp-iface"
                  type="text"
                  disabled
                  value={formInterface}
                  className="bg-muted border border-border rounded-lg h-9 px-2.5 text-xs text-muted-foreground"
                />
              ) : (
                <select
                  id="modal-dhcp-iface"
                  value={formInterface}
                  onChange={(e) => setFormInterface(e.target.value)}
                  className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                >
                  {availableInterfaces.map(iface => (
                    <option key={iface} value={iface}>{iface}</option>
                  ))}
                </select>
              )}
            </div>

            {/* IP Pool Range Grid */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="modal-start-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Starting IP Address
                </Label>
                <Input
                  id="modal-start-ip"
                  type="text"
                  required
                  value={formStartIp}
                  onChange={(e) => setFormStartIp(e.target.value)}
                  placeholder="เช่น 192.168.1.100"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="modal-end-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Ending IP Address
                </Label>
                <Input
                  id="modal-end-ip"
                  type="text"
                  required
                  value={formEndIp}
                  onChange={(e) => setFormEndIp(e.target.value)}
                  placeholder="เช่น 192.168.1.200"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
            </div>

            {/* Gateway & Netmask Grid */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="modal-gateway-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Default Gateway
                </Label>
                <Input
                  id="modal-gateway-ip"
                  type="text"
                  required
                  value={formGateway}
                  onChange={(e) => setFormGateway(e.target.value)}
                  placeholder="เช่น 192.168.1.1"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="modal-netmask-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Subnet Mask
                </Label>
                <Input
                  id="modal-netmask-ip"
                  type="text"
                  required
                  value={formNetmask}
                  onChange={(e) => setFormNetmask(e.target.value)}
                  placeholder="255.255.255.0"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
            </div>

            {/* DNS Grid */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="modal-dns-1" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  DNS Server 1
                </Label>
                <Input
                  id="modal-dns-1"
                  type="text"
                  value={formDns1}
                  onChange={(e) => setFormDns1(e.target.value)}
                  placeholder="8.8.8.8"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="modal-dns-2" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  DNS Server 2
                </Label>
                <Input
                  id="modal-dns-2"
                  type="text"
                  value={formDns2}
                  onChange={(e) => setFormDns2(e.target.value)}
                  placeholder="1.1.1.1"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
            </div>

            {/* Lease Time */}
            <div className="space-y-1.5">
              <Label htmlFor="modal-lease-time" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Lease Time (Seconds)
              </Label>
              <Input
                id="modal-lease-time"
                type="number"
                required
                min="60"
                value={formLeaseTime}
                onChange={(e) => setFormLeaseTime(e.target.value)}
                placeholder="86400 (24 ชั่วโมง)"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border/40">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsConfigModalOpen(false)}
                className="cursor-pointer text-muted-foreground hover:bg-muted/30 h-9"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSavingConfig}
                className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold px-5 h-9"
              >
                {isSavingConfig ? (
                  <>
                    <RefreshCw className="h-4 w-4 animate-spin" />
                    Saving...
                  </>
                ) : (
                  <>
                    <Save className="h-4 w-4" />
                    {editingConfig ? "Save Config" : "Create Config"}
                  </>
                )}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      {/* 7. Reservation Add/Edit Modal Dialog */}
      <Dialog open={isResModalOpen} modal={false} onOpenChange={setIsResModalOpen}>
        <DialogContent ref={dialogContentRef} className="max-w-[450px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingReservation ? "แก้ไขการจองไอพี (Edit Reservation)" : "จองไอพีสำหรับอุปกรณ์ใหม่ (Add Reservation)"}
            </DialogTitle>
          </DialogHeader>

          <form onSubmit={handleSaveReservation} className="space-y-4 text-sm">
            {resError && (
              <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                <AlertCircle className="h-4 w-4 text-red-400" />
                <AlertDescription className="text-red-400 text-xs">{resError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Device Name */}
            <div className="space-y-1.5">
              <Label htmlFor="res-name" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Device Name (ชื่ออุปกรณ์) <span className="text-red-500">*</span>
              </Label>
              <Input
                id="res-name"
                type="text"
                required
                value={resDeviceName}
                onChange={(e) => setResDeviceName(e.target.value)}
                placeholder="เช่น Printer-Office, CEO-iPad"
                className="bg-background/50 placeholder:text-muted-foreground h-9 text-xs"
              />
            </div>

            {/* Field: MAC Address */}
            <div className="space-y-1.5">
              <Label htmlFor="res-mac" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                MAC Address <span className="text-red-500">*</span>
              </Label>
              <Input
                id="res-mac"
                type="text"
                required
                value={resMacAddress}
                onChange={(e) => setResMacAddress(e.target.value)}
                placeholder="เช่น A1:B2:C3:D4:E5:F6"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
              <p className="text-[11px] text-muted-foreground italic">
                รูปแบบตัวเลขฐานสิบหก 12 ตัวแบ่งด้วยโคลอน (Colon-separated)
              </p>
            </div>

            {/* Field: Reserved IP */}
            <div className="space-y-1.5">
              <Label htmlFor="res-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Reserved IP Address <span className="text-red-500">*</span>
              </Label>
              <Input
                id="res-ip"
                type="text"
                required
                value={resIpAddress}
                onChange={(e) => setResIpAddress(e.target.value)}
                placeholder="เช่น 192.168.1.15"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
              <p className="text-[11px] text-muted-foreground italic">
                แนะนำให้เลือกไอพีที่อยู่นอกกลุ่ม DHCP IP Pool เพื่อหลีกเลี่ยงไอพีชนกัน
              </p>
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border/40">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsResModalOpen(false)}
                className="cursor-pointer text-muted-foreground hover:bg-muted/30 h-9"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold px-5 h-9"
              >
                {editingReservation ? "Save Changes" : "Create Reservation"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
