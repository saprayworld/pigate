import { useState, useMemo, useRef } from "react"
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
  ArrowRightLeft
} from "lucide-react"
import { Card } from "@/components/ui/card"
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
  type ActiveDhcpLease,
  initialDhcpConfig,
  initialDhcpReservations,
  initialActiveDhcpLeases
} from "@/data-mockup/mockData"

export default function DhcpServer() {
  // --- State ---
  const [config, setConfig] = useState<DhcpConfig>(initialDhcpConfig)
  const [reservations, setReservations] = useState<DhcpReservation[]>(initialDhcpReservations)
  const [activeLeases, setActiveLeases] = useState<ActiveDhcpLease[]>(initialActiveDhcpLeases)

  // Search queries
  const [resSearchQuery, setResSearchQuery] = useState("")
  const [leaseSearchQuery, setLeaseSearchQuery] = useState("")

  // Modals state
  const [isResModalOpen, setIsResModalOpen] = useState(false)
  const [editingReservation, setEditingReservation] = useState<DhcpReservation | null>(null)

  // Form states - Configuration Pool
  const [formInterface, setFormInterface] = useState(config.interface)
  const [formStartIp, setFormStartIp] = useState(config.startIp)
  const [formEndIp, setFormEndIp] = useState(config.endIp)
  const [formGateway, setFormGateway] = useState(config.gateway)
  const [formNetmask, setFormNetmask] = useState(config.netmask)
  const [formDns1, setFormDns1] = useState(config.dns1)
  const [formDns2, setFormDns2] = useState(config.dns2)
  const [formLeaseTime, setFormLeaseTime] = useState(config.leaseTime.toString())
  const [configError, setConfigError] = useState("")
  const [configSuccess, setConfigSuccess] = useState("")

  // Form states - Static Reservation Modal
  const [resDeviceName, setResDeviceName] = useState("")
  const [resMacAddress, setResMacAddress] = useState("")
  const [resIpAddress, setResIpAddress] = useState("")
  const [resError, setResError] = useState("")

  // Simulation states
  const [isApplying, setIsApplying] = useState(false)
  const [isApplied, setIsApplied] = useState(true) // Turns false when there are unapplied configurations
  const [isSavingConfig, setIsSavingConfig] = useState(false)
  const [isRefreshingLeases, setIsRefreshingLeases] = useState(false)

  const dialogContentRef = useRef<HTMLDivElement | null>(null)

  // --- Helpers ---
  const ipRegex = /^(?:[0-9]{1,3}\.){3}[0-9]{1,3}$/
  const macRegex = /^[0-9A-Fa-f]{2}(:[0-9A-Fa-f]{2}){5}$/

  const isValidIp = (ip: string) => {
    if (!ipRegex.test(ip)) return false
    const parts = ip.split(".").map(Number)
    return parts.every(part => part >= 0 && part <= 255)
  }

  const ipToNum = (ip: string) => {
    const parts = ip.split(".").map(Number)
    return (parts[0] * 16777216) + (parts[1] * 65536) + (parts[2] * 256) + parts[3]
  }

  // --- Statistics ---
  const stats = useMemo(() => {
    return {
      status: config.enabled ? "Active" : "Inactive",
      poolRange: `${config.startIp} - ${config.endIp}`,
      reservationsCount: reservations.length,
      activeLeasesCount: activeLeases.length
    }
  }, [config, reservations, activeLeases])

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
        lease.ipAddress.toLowerCase().includes(query)
      )
    })
  }, [activeLeases, leaseSearchQuery])

  // --- Handlers ---
  const handleToggleService = (checked: boolean) => {
    setConfig(prev => ({ ...prev, enabled: checked }))
    setIsApplied(false)
  }

  const handleSaveConfig = (e: React.FormEvent) => {
    e.preventDefault()
    setConfigError("")
    setConfigSuccess("")
    setIsSavingConfig(true)

    // Validation
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

    // IP range logic check
    if (ipToNum(formStartIp) > ipToNum(formEndIp)) {
      setConfigError("Starting IP ต้องมีค่าน้อยกว่าหรือเท่ากับ Ending IP")
      setIsSavingConfig(false)
      return
    }

    setTimeout(() => {
      setConfig({
        enabled: config.enabled,
        interface: formInterface,
        startIp: formStartIp,
        endIp: formEndIp,
        gateway: formGateway,
        netmask: formNetmask,
        dns1: formDns1,
        dns2: formDns2,
        leaseTime: leaseTimeVal
      })
      setIsSavingConfig(false)
      setConfigSuccess("บันทึกการตั้งค่า DHCP Pool เรียบร้อยแล้ว")
      setIsApplied(false)
      setTimeout(() => setConfigSuccess(""), 4000)
    }, 1000)
  }

  const handleApplySettings = () => {
    setIsApplying(true)
    setTimeout(() => {
      setIsApplying(false)
      setIsApplied(true)
    }, 1500)
  }

  const handleRefreshLeases = () => {
    setIsRefreshingLeases(true)
    setTimeout(() => {
      setIsRefreshingLeases(false)
      // Simulate adding a lease sometimes on refresh just for visuals
      if (activeLeases.length === 3) {
        setActiveLeases(prev => [
          ...prev,
          {
            id: "lease-4",
            ipAddress: "192.168.1.109",
            macAddress: "40:A3:CC:11:D3:55",
            hostname: "Smart-Thermostat",
            expiresIn: "23 hours, 59 mins"
          }
        ])
      }
    }, 1000)
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

  const handleDeleteReservation = (id: string, name: string) => {
    if (confirm(`คุณต้องการลบการจองไอพีของอุปกรณ์ "${name}" ใช่หรือไม่?`)) {
      setReservations(prev => prev.filter(res => res.id !== id))
      setIsApplied(false)
    }
  }

  const handleSaveReservation = (e: React.FormEvent) => {
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

    if (editingReservation) {
      // Edit
      setReservations(prev => prev.map(r =>
        r.id === editingReservation.id
          ? { ...r, deviceName: name, macAddress: mac, ipAddress: ip }
          : r
      ))
    } else {
      // Create
      const newRes: DhcpReservation = {
        id: "res-" + Math.random().toString(36).substring(2, 9),
        deviceName: name,
        macAddress: mac,
        ipAddress: ip
      }
      setReservations(prev => [...prev, newRes])
    }

    setIsResModalOpen(false)
    setIsApplied(false)
  }

  // Convert an active lease directly into a reservation
  const handleConvertLeaseToReservation = (lease: ActiveDhcpLease) => {
    setEditingReservation(null)
    setResDeviceName(lease.hostname || "Device_Reserved")
    setResMacAddress(lease.macAddress)
    setResIpAddress(lease.ipAddress)
    setResError("")
    setIsResModalOpen(true)
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
          <div className="flex items-center gap-2 bg-neutral-900 border border-neutral-800 rounded-lg px-3 py-1.5">
            <span className="text-xs font-semibold text-muted-foreground uppercase">Service:</span>
            <Switch
              checked={config.enabled}
              onCheckedChange={handleToggleService}
              className="data-[state=checked]:bg-primary"
            />
            <span className={`text-xs font-bold ${config.enabled ? "text-primary" : "text-muted-foreground"}`}>
              {config.enabled ? "ON" : "OFF"}
            </span>
          </div>
        </div>
      </div>

      {/* 2. Stats Dashboard Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">สถานะบริการ</div>
          <div className={`mt-2 text-2xl font-bold font-mono ${config.enabled ? "text-primary" : "text-muted-foreground"}`}>
            {stats.status}
          </div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">ช่วงไอพีที่แจก (IP Pool)</div>
          <div className="mt-2 text-sm font-bold text-foreground font-mono truncate" title={stats.poolRange}>
            {stats.poolRange}
          </div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Network className="h-3.5 w-3.5 text-cyan-400" /> การจองไอพีคงที่ (Static)
          </div>
          <div className="mt-2 text-2xl font-bold text-cyan-400 font-mono">{stats.reservationsCount}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Server className="h-3.5 w-3.5 text-amber-400" /> ไอพีที่จ่ายใช้งานอยู่
          </div>
          <div className="mt-2 text-2xl font-bold text-amber-400 font-mono">{stats.activeLeasesCount}</div>
        </Card>
      </div>

      {/* 3. Main Split Grid (Config Form & Reservations) */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6">
        {/* Left Side: Server Configuration Form */}
        <div className="lg:col-span-5 space-y-6">
          <Card className="bg-card/25 border border-border/50 p-6 space-y-4">
            <div className="border-b border-border/40 pb-3 flex items-center justify-between">
              <h2 className="text-md font-bold text-foreground flex items-center gap-2">
                <Server className="h-4.5 w-4.5 text-primary" />
                IP Address Pool Configuration
              </h2>
            </div>

            <form onSubmit={handleSaveConfig} className="space-y-4 text-sm">
              {configError && (
                <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                  <AlertCircle className="h-4 w-4 text-red-400" />
                  <AlertDescription className="text-red-400 text-xs">{configError}</AlertDescription>
                </Alert>
              )}

              {configSuccess && (
                <Alert className="border-primary/20 bg-primary/5 py-2.5 px-3">
                  <CheckCircle2 className="h-4 w-4 text-primary" />
                  <AlertDescription className="text-primary text-xs">{configSuccess}</AlertDescription>
                </Alert>
              )}

              {/* Field: Interface selection */}
              <div className="space-y-1.5">
                <Label htmlFor="dhcp-iface" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Select Interface (อินเตอร์เฟสหลัก)
                </Label>
                <select
                  id="dhcp-iface"
                  value={formInterface}
                  onChange={(e) => setFormInterface(e.target.value)}
                  className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                >
                  <option value="eth0">eth0 (LAN_Internal) - 192.168.1.1/24</option>
                  <option value="wlan0">wlan0 (WAN_WiFi) - Not Recommended</option>
                </select>
              </div>

              {/* IP Range Grid */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label htmlFor="start-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    Starting IP
                  </Label>
                  <Input
                    id="start-ip"
                    type="text"
                    required
                    value={formStartIp}
                    onChange={(e) => setFormStartIp(e.target.value)}
                    placeholder="เช่น 192.168.1.100"
                    className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="end-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    Ending IP
                  </Label>
                  <Input
                    id="end-ip"
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
                  <Label htmlFor="gateway-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    Default Gateway
                  </Label>
                  <Input
                    id="gateway-ip"
                    type="text"
                    required
                    value={formGateway}
                    onChange={(e) => setFormGateway(e.target.value)}
                    placeholder="เช่น 192.168.1.1"
                    className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="netmask-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    Netmask
                  </Label>
                  <Input
                    id="netmask-ip"
                    type="text"
                    required
                    value={formNetmask}
                    onChange={(e) => setFormNetmask(e.target.value)}
                    placeholder="เช่น 255.255.255.0"
                    className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                  />
                </div>
              </div>

              {/* DNS Grid */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label htmlFor="dns-1" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    DNS Server 1
                  </Label>
                  <Input
                    id="dns-1"
                    type="text"
                    value={formDns1}
                    onChange={(e) => setFormDns1(e.target.value)}
                    placeholder="8.8.8.8"
                    className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="dns-2" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    DNS Server 2
                  </Label>
                  <Input
                    id="dns-2"
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
                <Label htmlFor="lease-time" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Default Lease Time (Seconds)
                </Label>
                <Input
                  id="lease-time"
                  type="number"
                  required
                  min="60"
                  value={formLeaseTime}
                  onChange={(e) => setFormLeaseTime(e.target.value)}
                  placeholder="86400 (24 ชั่วโมง)"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>

              <div className="flex justify-end pt-2">
                <Button
                  type="submit"
                  disabled={isSavingConfig}
                  className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1.5 h-9"
                >
                  {isSavingConfig ? (
                    <>
                      <RefreshCw className="h-4 w-4 animate-spin" />
                      Saving settings...
                    </>
                  ) : (
                    <>
                      <Save className="h-4 w-4" />
                      Save & Restart Service
                    </>
                  )}
                </Button>
              </div>
            </form>
          </Card>
        </div>

        {/* Right Side: Static IP / MAC Reservations */}
        <div className="lg:col-span-7 space-y-6">
          <Card className="bg-card/25 border border-border/50 p-6 space-y-4">
            <div className="border-b border-border/40 pb-3 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
              <h2 className="text-md font-bold text-foreground flex items-center gap-2">
                <Network className="h-4.5 w-4.5 text-cyan-400" />
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

            {/* Toolbar Search */}
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

            {/* Table */}
            <div className="rounded-lg border border-border/50 overflow-hidden bg-background/20">
              <Table>
                <TableHeader>
                  <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
                    <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[35%]">Device Name</th>
                    <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[30%]">MAC Address</th>
                    <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[25%]">Reserved IP</th>
                    <TableHead className="p-3 w-[10%] text-right"></TableHead>
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
                          <Badge variant="outline" className="bg-cyan-500/10 text-cyan-400 border-cyan-500/20 text-xs px-2 py-0.5 rounded font-mono font-medium">
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
        </div>
      </div>

      {/* 4. Active DHCP Leases Section */}
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

        {/* Toolbar Search */}
        <div className="relative">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground pointer-events-none" />
          <Input
            type="text"
            value={leaseSearchQuery}
            onChange={(e) => setLeaseSearchQuery(e.target.value)}
            placeholder="ค้นหา Hostname, IP หรือ MAC Address..."
            className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9"
          />
        </div>

        {/* Table */}
        <div className="rounded-lg border border-border/50 overflow-hidden bg-background/20">
          <Table>
            <TableHeader>
              <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
                <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[25%]">IP Address</th>
                <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[25%]">MAC Address</th>
                <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[25%]">Hostname</th>
                <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[15%]">Expires In</th>
                <TableHead className="p-3 w-[10%] text-right"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {filteredLeases.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="p-8 text-center text-muted-foreground text-xs">
                    ไม่พบอุปกรณ์เชื่อมต่อตามข้อความค้นหา
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
                        <span className="font-semibold text-foreground text-xs">{lease.hostname}</span>
                      </TableCell>
                      <TableCell className="p-3 text-xs text-muted-foreground font-mono">
                        {lease.expiresIn}
                      </TableCell>
                      <TableCell className="p-3 text-right">
                        {isReserved ? (
                          <Badge className="bg-primary/10 text-primary border border-primary/20 text-[10px] px-2 py-0.5 rounded font-semibold font-mono">
                            Reserved
                          </Badge>
                        ) : (
                          <Button
                            variant="ghost"
                            size="sm"
                            onClick={() => handleConvertLeaseToReservation(lease)}
                            className="cursor-pointer text-xs text-cyan-400 hover:text-cyan-300 hover:bg-cyan-500/10 font-medium py-1 px-2.5 h-7 gap-1"
                            title="จองไอพีนี้อย่างถาวร"
                          >
                            <ArrowRightLeft className="h-3 w-3" />
                            Reserve IP
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

      {/* 5. Alert Help Box */}
      <Alert className="border-dashed border-border bg-card/10">
        <Info className="h-4 w-4 text-muted-foreground" />
        <AlertTitle className="font-bold text-foreground mb-0.5">ระบบ DHCP Server (Dynamic Host Configuration Protocol):</AlertTitle>
        <AlertDescription className="text-xs text-muted-foreground leading-relaxed">
          มีหน้าที่ในการแจกจ่ายข้อมูลการเชื่อมต่อเครือข่าย ได้แก่ IP Address, Netmask, Gateway และ DNS Server โดยอัตโนมัติ 
          การประยุกต์ใช้งาน <span className="font-semibold text-cyan-400">MAC / IP Reservation</span> จะช่วยล็อก MAC Address 
          ของอุปกรณ์สำคัญ (เช่น NAS, Printer หรือ Server) เข้ากับไอพีเฉพาะเจาะจง เพื่อไม่ให้อุปกรณ์นั้นได้รับไอพีแอดเดรสเปลี่ยนไปเมื่อหมดเวลาเช่า (Lease Time)
        </AlertDescription>
      </Alert>

      {/* 6. Reservation Add/Edit Modal Dialog */}
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
