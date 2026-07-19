import { useState, useMemo, useEffect } from "react"
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
  Loader2,
  Activity
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
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
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import {
  type DhcpConfig,
  type DhcpReservation,
  type ActiveDhcpLease
} from "@/data-mockup/mockData"
import { dhcpService } from "@/services/dhcpService"
import { useAlert } from "@/hooks/useAlert"
import { isValidIp } from "@/lib/utils"
import { formatIfaceLabel, type IfaceLabelSource } from "@/lib/ifaceLabel"
import { interfaceService } from "@/services/interfaceService"

// Helper: Dashboard-style stat card (mirrors Dashboard's StatCard)
function StatCard({
  icon: Icon,
  title,
  value,
  valueClassName = "",
}: {
  icon: typeof Server
  title: string
  value: string | number
  valueClassName?: string
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
        <p
          className={`truncate text-2xl font-bold tracking-tight text-foreground ${valueClassName}`}
          title={typeof value === "string" ? value : undefined}
        >
          {value}
        </p>
      </CardContent>
    </Card>
  )
}

export default function DhcpServer() {
  const { alert, confirm } = useAlert()

  // --- State ---
  const [configs, setConfigs] = useState<DhcpConfig[]>([])
  const [availableInterfaces, setAvailableInterfaces] = useState<string[]>([])
  // Interface objects for alias-first labels; option values stay OS names.
  const [ifaceObjects, setIfaceObjects] = useState<IfaceLabelSource[]>([])
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

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
        const [cfgs, res, leases, avIface, allIfaces] = await Promise.all([
          dhcpService.getConfigs(),
          dhcpService.getReservations(),
          dhcpService.getActiveLeases(),
          dhcpService.getAvailableInterfaces(),
          interfaceService.getAll()
        ])
        setConfigs(cfgs || [])
        setReservations(res || [])
        setActiveLeases(leases || [])
        setAvailableInterfaces(avIface || [])
        setIfaceObjects(allIfaces || [])
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
      // Trim before sending: the backend validates the values verbatim (no trim)
      // and rejects edge whitespace, matching how the reservation form submits.
      const cfgPayload: DhcpConfig = {
        enabled: editingConfig ? editingConfig.enabled : true,
        interface: formInterface.trim(),
        startIp: formStartIp.trim(),
        endIp: formEndIp.trim(),
        gateway: formGateway.trim(),
        netmask: formNetmask.trim(),
        dns1: formDns1.trim(),
        dns2: formDns2.trim(),
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
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard
          icon={Activity}
          title="สถานะบริการ"
          value={stats.status}
          valueClassName={configs.some(c => c.enabled) ? "text-primary" : "text-muted-foreground"}
        />
        <StatCard icon={Radio} title="อินเตอร์เฟสหลัก" value={stats.activeInterfaces} valueClassName="font-mono text-lg leading-8" />
        <StatCard icon={Network} title="การจองไอพีคงที่ (Static)" value={stats.reservationsCount} />
        <StatCard icon={Server} title="ไอพีที่จ่ายใช้งานอยู่" value={stats.activeLeasesCount} />
      </div>

      {/* 2. IP Address Pools */}
      <Card>
        <CardHeader className="flex flex-col gap-4 space-y-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <Server className="h-4 w-4 text-muted-foreground" />
              IP Address Pools Configuration
            </CardTitle>
            <CardDescription className="text-xs">
              การตั้งค่ากลุ่มจ่ายไอพีอัตโนมัติแยกตามอินเตอร์เฟส
            </CardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            {!isApplied && (
              <Button
                size="sm"
                onClick={handleApplySettings}
                disabled={isApplying}
                className="animate-pulse cursor-pointer gap-1.5 bg-warning font-semibold text-warning-foreground hover:bg-warning/90"
              >
                {isApplying ? (
                  <>
                    <RefreshCw className="h-4 w-4 animate-spin" />
                    Applying DHCP...
                  </>
                ) : (
                  <>
                    <Check className="h-4 w-4" />
                    Apply DHCP Config
                  </>
                )}
              </Button>
            )}
            {isApplied && (
              <div className="flex h-8 items-center gap-1.5 rounded-lg border border-primary/20 bg-primary/10 px-3 text-xs font-medium text-primary">
                <CheckCircle2 className="h-4 w-4" />
                DHCP Service Active
              </div>
            )}
            {availableInterfaces.length > 0 && (
              <Button
                size="sm"
                onClick={openCreateConfigModal}
                className="cursor-pointer gap-1.5 font-semibold"
              >
                <Plus className="h-4 w-4" />
                Add DHCP Config
              </Button>
            )}
          </div>
        </CardHeader>

        <CardContent>
          <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
            {configs.map((cfg) => (
              <div key={cfg.id} className="flex flex-col justify-between gap-3 rounded-lg border border-border bg-muted/50 p-4">
                <div className="space-y-3">
                  <div className="flex items-center justify-between border-b border-border/50 pb-3">
                    <div className="flex items-center gap-2">
                      <Network className="h-4 w-4 shrink-0 text-muted-foreground" />
                      <div>
                        <div className="font-mono text-sm font-semibold text-foreground">{formatIfaceLabel(cfg.interface, ifaceObjects)}</div>
                        <p className="text-[11px] text-muted-foreground">DHCP Server Config</p>
                      </div>
                    </div>
                    <div className="flex items-center gap-2">
                      <Switch
                        size="sm"
                        checked={cfg.enabled}
                        onCheckedChange={(checked) => handleToggleConfig(cfg.id!, checked)}
                        className="cursor-pointer"
                      />
                      <Badge
                        variant="outline"
                        className={`rounded px-1.5 py-0 text-[10px] font-semibold ${cfg.enabled
                          ? "border-primary/20 bg-primary/10 text-primary"
                          : "border-border bg-muted text-muted-foreground"
                          }`}
                      >
                        {cfg.enabled ? "ACTIVE" : "INACTIVE"}
                      </Badge>
                    </div>
                  </div>

                  {/* Config Details */}
                  <div className="grid grid-cols-2 gap-x-4 gap-y-2.5 text-xs">
                    <div>
                      <span className="block text-[10px] font-medium text-muted-foreground">IP Range</span>
                      <span className="font-mono font-medium text-foreground">{cfg.startIp} - {cfg.endIp}</span>
                    </div>
                    <div>
                      <span className="block text-[10px] font-medium text-muted-foreground">Subnet Mask</span>
                      <span className="font-mono font-medium text-foreground">{cfg.netmask}</span>
                    </div>
                    <div>
                      <span className="block text-[10px] font-medium text-muted-foreground">Gateway</span>
                      <span className="font-mono font-medium text-foreground">{cfg.gateway || "—"}</span>
                    </div>
                    <div>
                      <span className="block text-[10px] font-medium text-muted-foreground">Lease Time</span>
                      <span className="font-mono font-medium text-foreground">{cfg.leaseTime}s ({Math.round(cfg.leaseTime / 3600)}h)</span>
                    </div>
                    <div className="col-span-2">
                      <span className="block text-[10px] font-medium text-muted-foreground">DNS Servers</span>
                      <span className="block truncate font-mono font-medium text-foreground">
                        {cfg.dns1}{cfg.dns2 ? `, ${cfg.dns2}` : ""}
                      </span>
                    </div>
                  </div>
                </div>

                <div className="flex items-center justify-end gap-2 border-t border-border/50 pt-3">
                  <Button
                    variant="outline"
                    size="sm"
                    onClick={() => openEditConfigModal(cfg)}
                    className="cursor-pointer gap-1.5 text-xs text-muted-foreground hover:text-foreground"
                  >
                    <Edit className="h-3.5 w-3.5" /> Edit
                  </Button>
                  <Button
                    variant="ghost"
                    size="sm"
                    onClick={() => handleDeleteConfig(cfg.id!, cfg.interface)}
                    className="cursor-pointer gap-1.5 text-xs text-destructive hover:bg-destructive/10 hover:text-destructive"
                  >
                    <Trash2 className="h-3.5 w-3.5" /> Delete
                  </Button>
                </div>
              </div>
            ))}
            {configs.length === 0 && (
              <div className="col-span-full flex flex-col items-center justify-center rounded-lg border border-dashed border-border p-6 text-center">
                <span className="text-sm text-muted-foreground">ยังไม่มีอินเตอร์เฟสเปิดจ่าย DHCP config</span>
              </div>
            )}
          </div>
        </CardContent>
      </Card>

      {/* 3. Reservations & Leases split view */}
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        {/* Left: Reservations */}
        <Card>
          <CardHeader className="flex flex-col gap-3 space-y-0 sm:flex-row sm:items-center sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Network className="h-4 w-4 text-muted-foreground" />
                MAC / IP Reservation
              </CardTitle>
              <CardDescription className="text-xs">จองไอพีแอดเดรสคงที่ให้อุปกรณ์ตาม MAC Address</CardDescription>
            </div>
            <Button
              onClick={openCreateResModal}
              size="sm"
              className="cursor-pointer gap-1.5 font-semibold"
            >
              <Plus className="h-4 w-4" />
              Add Reservation
            </Button>
          </CardHeader>

          <CardContent className="space-y-4">
            <div className="relative">
              <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                value={resSearchQuery}
                onChange={(e) => setResSearchQuery(e.target.value)}
                placeholder="ค้นหา Device Name, MAC หรือ IP Address..."
                className="h-9 pl-8"
              />
            </div>

            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs font-medium text-muted-foreground">Device Name</TableHead>
                  <TableHead className="text-xs font-medium text-muted-foreground">MAC Address</TableHead>
                  <TableHead className="text-xs font-medium text-muted-foreground">Reserved IP</TableHead>
                  <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredReservations.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={4} className="py-8 text-center text-xs text-muted-foreground">
                      {resSearchQuery ? "ไม่พบประวัติการจองตามที่ค้นหา" : "ยังไม่มีประวัติการจอง IP สำหรับอุปกรณ์คงที่"}
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredReservations.map((res) => (
                    <TableRow key={res.id}>
                      <TableCell className="py-3">
                        <span className="text-xs font-medium text-foreground">{res.deviceName}</span>
                      </TableCell>
                      <TableCell className="py-3 font-mono text-xs text-muted-foreground">
                        {res.macAddress}
                      </TableCell>
                      <TableCell className="py-3">
                        <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 font-mono text-xs font-medium text-primary">
                          {res.ipAddress}
                        </Badge>
                      </TableCell>
                      <TableCell className="py-3 text-right">
                        <div className="flex items-center justify-end gap-2">
                          <Button
                            variant="outline"
                            size="icon-sm"
                            onClick={() => openEditResModal(res)}
                            className="cursor-pointer text-muted-foreground hover:text-foreground"
                            title="แก้ไขการจอง"
                          >
                            <Edit className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-sm"
                            onClick={() => handleDeleteReservation(res.id, res.deviceName)}
                            className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                            title="ลบการจอง"
                          >
                            <Trash2 className="h-4 w-4" />
                          </Button>
                        </div>
                      </TableCell>
                    </TableRow>
                  ))
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

        {/* Right: Active Leases */}
        <Card>
          <CardHeader className="flex flex-col gap-3 space-y-0 sm:flex-row sm:items-center sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Server className="h-4 w-4 text-muted-foreground" />
                Active DHCP Leases
              </CardTitle>
              <CardDescription className="text-xs">อุปกรณ์ที่ได้รับไอพีและเชื่อมต่อในระบบขณะนี้</CardDescription>
            </div>
            <Button
              onClick={handleRefreshLeases}
              disabled={isRefreshingLeases}
              variant="outline"
              size="sm"
              className="cursor-pointer gap-2"
            >
              <RefreshCw className={`h-4 w-4 ${isRefreshingLeases ? "animate-spin" : ""}`} />
              Refresh
            </Button>
          </CardHeader>

          <CardContent className="space-y-4">
            <div className="relative">
              <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                value={leaseSearchQuery}
                onChange={(e) => setLeaseSearchQuery(e.target.value)}
                placeholder="ค้นหา Hostname, IP, MAC หรือ Interface..."
                className="h-9 pl-8"
              />
            </div>

            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="text-xs font-medium text-muted-foreground">IP Address</TableHead>
                  <TableHead className="text-xs font-medium text-muted-foreground">MAC Address</TableHead>
                  <TableHead className="text-xs font-medium text-muted-foreground">Hostname</TableHead>
                  <TableHead className="text-xs font-medium text-muted-foreground">Iface</TableHead>
                  <TableHead className="text-xs font-medium text-muted-foreground">Expires</TableHead>
                  <TableHead className="w-[15%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {filteredLeases.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={6} className="py-8 text-center text-xs text-muted-foreground">
                      ไม่พบอุปกรณ์เชื่อมต่อในระบบขณะนี้
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredLeases.map((lease) => {
                    const isReserved = reservations.some(r => r.macAddress === lease.macAddress)
                    return (
                      <TableRow key={lease.id}>
                        <TableCell className="py-3">
                          <span className="font-mono text-xs font-medium text-foreground">{lease.ipAddress}</span>
                        </TableCell>
                        <TableCell className="py-3 font-mono text-xs text-muted-foreground">
                          {lease.macAddress}
                        </TableCell>
                        <TableCell className="py-3">
                          <span className="block max-w-[90px] truncate text-xs font-medium text-foreground" title={lease.hostname}>
                            {lease.hostname}
                          </span>
                        </TableCell>
                        <TableCell className="py-3 font-mono text-xs text-muted-foreground">
                          {lease.interface || "—"}
                        </TableCell>
                        <TableCell className="py-3 font-mono text-[11px] text-muted-foreground">
                          {lease.expiresIn}
                        </TableCell>
                        <TableCell className="py-3 text-right">
                          {isReserved ? (
                            <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 font-mono text-[10px] font-semibold text-primary">
                              Reserved
                            </Badge>
                          ) : (
                            <Button
                              variant="ghost"
                              size="sm"
                              onClick={() => handleConvertLeaseToReservation(lease)}
                              className="h-7 cursor-pointer gap-1 px-2 text-[11px] font-medium text-primary hover:bg-primary/10 hover:text-primary"
                              title="จองไอพีนี้"
                            >
                              <ArrowRightLeft className="h-3 w-3" />
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
          </CardContent>
        </Card>
      </div>

      {/* 4. Info note */}
      <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          <strong className="text-foreground">ระบบ DHCP Server (Dynamic Host Configuration Protocol):</strong>{" "}
          มีหน้าที่ในการแจกจ่ายข้อมูลการเชื่อมต่อเครือข่าย ได้แก่ IP Address, Netmask, Gateway และ DNS Server โดยอัตโนมัติ
          การจ่าย IP แบบหลายอินเตอร์เฟสช่วยให้ PiGate ทำงานแยกเครือข่าย (LAN Segment) ได้ เช่น eth0 และ eth1 โดยไม่เกี่ยวข้องกัน
        </span>
      </div>

      {/* 5. Config Pool Add/Edit Modal Dialog */}
      <Drawer direction="right" open={isConfigModalOpen} onOpenChange={setIsConfigModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingConfig ? `แก้ไขการตั้งค่า DHCP (${formatIfaceLabel(formInterface, ifaceObjects)})` : "สร้างการตั้งค่า DHCP บนอินเตอร์เฟสใหม่"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveConfig} className="space-y-4 text-sm">
            {configError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{configError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Interface Selection */}
            <div className="space-y-1.5">
              <Label htmlFor="modal-dhcp-iface" className="block text-xs font-medium text-muted-foreground">
                Interface (อินเตอร์เฟส)
              </Label>
              {editingConfig ? (
                <Input
                  id="modal-dhcp-iface"
                  type="text"
                  disabled
                  value={formatIfaceLabel(formInterface, ifaceObjects)}
                  className="h-9 font-mono text-sm"
                />
              ) : (
                <select
                  id="modal-dhcp-iface"
                  value={formInterface}
                  onChange={(e) => setFormInterface(e.target.value)}
                  className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                >
                  {availableInterfaces.map(iface => (
                    <option key={iface} value={iface}>{formatIfaceLabel(iface, ifaceObjects)}</option>
                  ))}
                </select>
              )}
            </div>

            {/* IP Pool Range Grid */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="modal-start-ip" className="block text-xs font-medium text-muted-foreground">
                  Starting IP Address <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="modal-start-ip"
                  type="text"
                  required
                  value={formStartIp}
                  onChange={(e) => setFormStartIp(e.target.value)}
                  placeholder="เช่น 192.168.1.100"
                  className="h-9 font-mono text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="modal-end-ip" className="block text-xs font-medium text-muted-foreground">
                  Ending IP Address <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="modal-end-ip"
                  type="text"
                  required
                  value={formEndIp}
                  onChange={(e) => setFormEndIp(e.target.value)}
                  placeholder="เช่น 192.168.1.200"
                  className="h-9 font-mono text-sm"
                />
              </div>
            </div>

            {/* Gateway & Netmask Grid */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="modal-gateway-ip" className="block text-xs font-medium text-muted-foreground">
                  Default Gateway <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="modal-gateway-ip"
                  type="text"
                  required
                  value={formGateway}
                  onChange={(e) => setFormGateway(e.target.value)}
                  placeholder="เช่น 192.168.1.1"
                  className="h-9 font-mono text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="modal-netmask-ip" className="block text-xs font-medium text-muted-foreground">
                  Subnet Mask <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="modal-netmask-ip"
                  type="text"
                  required
                  value={formNetmask}
                  onChange={(e) => setFormNetmask(e.target.value)}
                  placeholder="255.255.255.0"
                  className="h-9 font-mono text-sm"
                />
              </div>
            </div>

            {/* DNS Grid */}
            <div className="grid grid-cols-2 gap-4">
              <div className="space-y-1.5">
                <Label htmlFor="modal-dns-1" className="block text-xs font-medium text-muted-foreground">
                  DNS Server 1
                </Label>
                <Input
                  id="modal-dns-1"
                  type="text"
                  value={formDns1}
                  onChange={(e) => setFormDns1(e.target.value)}
                  placeholder="8.8.8.8"
                  className="h-9 font-mono text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="modal-dns-2" className="block text-xs font-medium text-muted-foreground">
                  DNS Server 2
                </Label>
                <Input
                  id="modal-dns-2"
                  type="text"
                  value={formDns2}
                  onChange={(e) => setFormDns2(e.target.value)}
                  placeholder="1.1.1.1"
                  className="h-9 font-mono text-sm"
                />
              </div>
            </div>

            {/* Lease Time */}
            <div className="space-y-1.5">
              <Label htmlFor="modal-lease-time" className="block text-xs font-medium text-muted-foreground">
                Lease Time (Seconds) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="modal-lease-time"
                type="number"
                required
                min="60"
                value={formLeaseTime}
                onChange={(e) => setFormLeaseTime(e.target.value)}
                placeholder="86400 (24 ชั่วโมง)"
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsConfigModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSavingConfig}
                className="cursor-pointer gap-2 px-6 font-semibold"
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
          </div>
        </DrawerContent>
      </Drawer>

      {/* 6. Reservation Add/Edit Drawer */}
      <Drawer direction="right" open={isResModalOpen} onOpenChange={setIsResModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[450px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingReservation ? "แก้ไขการจองไอพี (Edit Reservation)" : "จองไอพีสำหรับอุปกรณ์ใหม่ (Add Reservation)"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveReservation} className="space-y-4 text-sm">
            {resError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{resError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Device Name */}
            <div className="space-y-1.5">
              <Label htmlFor="res-name" className="block text-xs font-medium text-muted-foreground">
                Device Name (ชื่ออุปกรณ์) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="res-name"
                type="text"
                required
                value={resDeviceName}
                onChange={(e) => setResDeviceName(e.target.value)}
                placeholder="เช่น Printer-Office, CEO-iPad"
                className="h-9 text-sm"
              />
            </div>

            {/* Field: MAC Address */}
            <div className="space-y-1.5">
              <Label htmlFor="res-mac" className="block text-xs font-medium text-muted-foreground">
                MAC Address <span className="text-destructive">*</span>
              </Label>
              <Input
                id="res-mac"
                type="text"
                required
                value={resMacAddress}
                onChange={(e) => setResMacAddress(e.target.value)}
                placeholder="เช่น A1:B2:C3:D4:E5:F6"
                className="h-9 font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                รูปแบบตัวเลขฐานสิบหก 12 ตัวแบ่งด้วยโคลอน (Colon-separated)
              </p>
            </div>

            {/* Field: Reserved IP */}
            <div className="space-y-1.5">
              <Label htmlFor="res-ip" className="block text-xs font-medium text-muted-foreground">
                Reserved IP Address <span className="text-destructive">*</span>
              </Label>
              <Input
                id="res-ip"
                type="text"
                required
                value={resIpAddress}
                onChange={(e) => setResIpAddress(e.target.value)}
                placeholder="เช่น 192.168.1.15"
                className="h-9 font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                แนะนำให้เลือกไอพีที่อยู่นอกกลุ่ม DHCP IP Pool เพื่อหลีกเลี่ยงไอพีชนกัน
              </p>
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsResModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button type="submit" className="cursor-pointer px-6 font-semibold">
                {editingReservation ? "Save Changes" : "Create Reservation"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
