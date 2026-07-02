import { useState, useMemo, useEffect } from "react"
import {
  Globe,
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
  Database,
  Loader2,
  Network
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
import { type DNSZone, type DNSRecord, type NetworkInterface } from "@/data-mockup/mockData"
import { dnsServerService } from "@/services/dnsServerService"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/components/AlertDialogProvider"
import { isValidIp } from "@/lib/utils"

export default function DnsServer() {
  const { alert, confirm } = useAlert()

  // --- State ---
  const [zones, setZones] = useState<DNSZone[]>([])
  const [selectedZoneId, setSelectedZoneId] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(true)

  // Search queries
  const [zoneSearchQuery, setZoneSearchQuery] = useState("")
  const [recordSearchQuery, setRecordSearchQuery] = useState("")

  // Modals state
  const [isZoneModalOpen, setIsZoneModalOpen] = useState(false)
  const [editingZone, setEditingZone] = useState<DNSZone | null>(null)

  const [isRecModalOpen, setIsRecModalOpen] = useState(false)
  const [editingRecord, setEditingRecord] = useState<DNSRecord | null>(null)

  // Form states - Zone Modal
  const [zoneName, setZoneName] = useState("")
  const [isAuthoritative, setIsAuthoritative] = useState(true)
  const [forwardTo, setForwardTo] = useState("")
  const [allowedIps, setAllowedIps] = useState("any")
  const [zoneError, setZoneError] = useState("")

  // Form states - Record Modal
  const [recName, setRecName] = useState("")
  const [recType, setRecType] = useState("A")
  const [recValue, setRecValue] = useState("")
  const [recTtl, setRecTtl] = useState("300")
  const [recError, setRecError] = useState("")

  // Apply & Save states
  const [isApplying, setIsApplying] = useState(false)
  const [isApplied, setIsApplied] = useState(true)
  const [isClearingCache, setIsClearingCache] = useState(false)
  const [isSaving, setIsSaving] = useState(false)

  // Listen Interfaces state — real interfaces from Interface Service, independent of DHCP Server
  const [availableInterfaces, setAvailableInterfaces] = useState<NetworkInterface[]>([])
  const [selectedInterfaces, setSelectedInterfaces] = useState<string[]>([])
  const [isSavingInterfaces, setIsSavingInterfaces] = useState(false)

  // Load DNS Data
  const loadDnsData = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const [data, ifaces, settings] = await Promise.all([
        dnsServerService.getZones(),
        interfaceService.getAll(),
        dnsServerService.getSettings(),
      ])
      setZones(data || [])
      if ((data || []).length > 0 && !selectedZoneId) {
        setSelectedZoneId(data[0].id)
      }
      setAvailableInterfaces((ifaces || []).filter(i => i.role === "LAN"))
      setSelectedInterfaces(settings.interfaces || [])
    } catch (err: any) {
      console.error(err)
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูล DNS Server ได้: " + (err.message || err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    loadDnsData()
  }, [])

  // --- Handlers: Listen Interfaces ---
  const handleToggleInterface = async (name: string, checked: boolean) => {
    const next = checked
      ? [...selectedInterfaces, name]
      : selectedInterfaces.filter(n => n !== name)

    setIsSavingInterfaces(true)
    try {
      await dnsServerService.updateSettings({ interfaces: next })
      setSelectedInterfaces(next)
      setIsApplied(false)
    } catch (err: any) {
      await alert("ข้อผิดพลาด", "ไม่สามารถบันทึก Interface ของ DNS Server ได้: " + (err.message || err))
    } finally {
      setIsSavingInterfaces(false)
    }
  }

  // Selected Zone object
  const selectedZone = useMemo(() => {
    return zones.find(z => z.id === selectedZoneId) || null
  }, [zones, selectedZoneId])

  // Filtered Zones
  const filteredZones = useMemo(() => {
    return zones.filter(z => 
      z.zoneName.toLowerCase().includes(zoneSearchQuery.toLowerCase())
    )
  }, [zones, zoneSearchQuery])

  // Filtered Records
  const filteredRecords = useMemo(() => {
    if (!selectedZone) return []
    if (!selectedZone.records || selectedZone.records.length === 0) return []
    return selectedZone.records.filter(r => 
      r.name.toLowerCase().includes(recordSearchQuery.toLowerCase()) ||
      r.type.toLowerCase().includes(recordSearchQuery.toLowerCase()) ||
      r.value.toLowerCase().includes(recordSearchQuery.toLowerCase())
    )
  }, [selectedZone, recordSearchQuery])

  // --- Handlers: Zone CRUD ---
  const openCreateZoneModal = () => {
    setEditingZone(null)
    setZoneName("")
    setIsAuthoritative(true)
    setForwardTo("")
    setAllowedIps("any")
    setZoneError("")
    setIsZoneModalOpen(true)
  }

  const openEditZoneModal = (zone: DNSZone) => {
    setEditingZone(zone)
    setZoneName(zone.zoneName)
    setIsAuthoritative(zone.isAuthoritative)
    setForwardTo(zone.forwardTo || "")
    setAllowedIps(zone.allowedIps || "any")
    setZoneError("")
    setIsZoneModalOpen(true)
  }

  const handleDeleteZone = async (id: string, name: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบโซน DNS "${name}" ใช่หรือไม่? (ระเบียนในโซนทั้งหมดจะถูกลบไปด้วย)`)) {
      try {
        await dnsServerService.deleteZone(id)
        setZones(prev => prev.filter(z => z.id !== id))
        if (selectedZoneId === id) {
          const remaining = zones.filter(z => z.id !== id)
          setSelectedZoneId(remaining.length > 0 ? remaining[0].id : null)
        }
        setIsApplied(false)
      } catch (err: any) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบโซนได้: " + (err.message || err))
      }
    }
  }

  const handleToggleZone = async (id: string, checked: boolean) => {
    try {
      await dnsServerService.toggleZone(id)
      setZones(prev => prev.map(z => z.id === id ? { ...z, enabled: checked } : z))
      setIsApplied(false)
    } catch (err: any) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปิด/ปิดโซนได้: " + (err.message || err))
    }
  }

  const handleSaveZone = async (e: React.FormEvent) => {
    e.preventDefault()
    setZoneError("")
    setIsSaving(true)

    const name = zoneName.trim()
    const forward = forwardTo.trim()

    if (!name) {
      setZoneError("กรุณากรอกชื่อโซน")
      setIsSaving(false)
      return
    }

    if (!isAuthoritative) {
      if (!forward) {
        setZoneError("โซนประเภท Forward (ส่งต่อ) จำเป็นต้องระบุ IP ของ Upstream Resolver")
        setIsSaving(false)
        return
      }
      if (!isValidIp(forward)) {
        setZoneError("IP สำหรับส่งต่อ (Forward To) ไม่ถูกต้อง")
        setIsSaving(false)
        return
      }
    }

    try {
      const payload = {
        zoneName: name,
        forwardTo: isAuthoritative ? "" : forward,
        allowedIps: allowedIps,
        isAuthoritative: isAuthoritative,
        enabled: editingZone ? editingZone.enabled : true
      }

      if (editingZone) {
        const updated = await dnsServerService.updateZone(editingZone.id, payload)
        // Zone update only touches zone metadata — always keep the records already held
        // locally rather than trusting whatever (possibly empty) records the API echoes back.
        setZones(prev => prev.map(z => z.id === editingZone.id ? { ...z, ...updated, records: z.records } : z))
      } else {
        const created = await dnsServerService.createZone(payload)
        setZones(prev => [...prev, created])
        setSelectedZoneId(created.id)
      }

      setIsZoneModalOpen(false)
      setIsApplied(false)
    } catch (err: any) {
      setZoneError(err.message || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    } finally {
      setIsSaving(false)
    }
  }

  // --- Handlers: Record CRUD ---
  const openCreateRecModal = () => {
    setEditingRecord(null)
    setRecName("")
    setRecType("A")
    setRecValue("")
    setRecTtl("300")
    setRecError("")
    setIsRecModalOpen(true)
  }

  const openEditRecModal = (rec: DNSRecord) => {
    setEditingRecord(rec)
    setRecName(rec.name)
    setRecType(rec.type)
    setRecValue(rec.value)
    setRecTtl(rec.ttl.toString())
    setRecError("")
    setIsRecModalOpen(true)
  }

  const handleDeleteRecord = async (id: string, name: string) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบระเบียน DNS "${name}" ใช่หรือไม่?`)) {
      try {
        await dnsServerService.deleteRecord(id)
        setZones(prev => prev.map(z => {
          if (z.id === selectedZoneId) {
            return {
              ...z,
              records: z.records.filter(r => r.id !== id)
            }
          }
          return z
        }))
        setIsApplied(false)
      } catch (err: any) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบระเบียนได้: " + (err.message || err))
      }
    }
  }

  const handleSaveRecord = async (e: React.FormEvent) => {
    e.preventDefault()
    setRecError("")
    setIsSaving(true)

    const name = recName.trim()
    const value = recValue.trim()
    const ttlVal = parseInt(recTtl, 10)

    if (!value) {
      setRecError("กรุณากรอกค่าระเบียน (Record Value)")
      setIsSaving(false)
      return
    }

    if (isNaN(ttlVal) || ttlVal <= 0) {
      setRecError("TTL ต้องเป็นตัวเลขจำนวนเต็มบวก")
      setIsSaving(false)
      return
    }

    // Type validation
    if (recType === "A" && !isValidIp(value)) {
      setRecError("สำหรับระเบียนประเภท A ค่าของระเบียนต้องเป็น IPv4 แอดเดรสที่ถูกต้อง")
      setIsSaving(false)
      return
    }

    try {
      const payload = {
        name: name || "@",
        type: recType,
        value: value,
        ttl: ttlVal
      }

      if (editingRecord) {
        const updated = await dnsServerService.updateRecord(editingRecord.id, payload)
        setZones(prev => prev.map(z => {
          if (z.id === selectedZoneId) {
            return {
              ...z,
              records: z.records.map(r => r.id === editingRecord.id ? updated : r)
            }
          }
          return z
        }))
      } else {
        const created = await dnsServerService.createRecord(selectedZoneId!, payload)
        setZones(prev => prev.map(z => {
          if (z.id === selectedZoneId) {
            return {
              ...z,
              records: [...z.records, created]
            }
          }
          return z
        }))
      }

      setIsRecModalOpen(false)
      setIsApplied(false)
    } catch (err: any) {
      setRecError(err.message || "เกิดข้อผิดพลาดในการบันทึกข้อมูลระเบียน")
    } finally {
      setIsSaving(false)
    }
  }

  // --- Handlers: Apply & Cache ---
  const handleApplySettings = async () => {
    setIsApplying(true)
    try {
      await dnsServerService.apply()
      setIsApplied(true)
    } catch (err: any) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเริ่มระบบ DNS เข้ากับ OS Kernel ได้: " + (err.message || err))
    } finally {
      setIsApplying(false)
    }
  }

  const handleClearCache = async () => {
    setIsClearingCache(true)
    try {
      await dnsServerService.clearCache()
      await alert("สำเร็จ", "เคลียร์หน่วยความจำแคช DNS ของระบบเรียบร้อยแล้ว")
    } catch (err: any) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเคลียร์หน่วยความจำแคชได้: " + (err.message || err))
    } finally {
      setIsClearingCache(false)
    }
  }

  if (isLoading) {
    return (
      <div className="flex items-center justify-center min-h-[400px]">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="ml-2 text-sm text-muted-foreground">กำลังโหลดข้อมูล DNS Server...</span>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 1. Header Area */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Globe className="h-7 w-7 text-primary fill-primary/10" />
            Local DNS Server (ระบบตั้งชื่อโฮสต์ในเครือข่าย)
          </h1>
          <p className="text-muted-foreground mt-1">
            กำหนดโซนระบบชื่อโดเมนภายในเครือข่าย (Local DNS Zones) และสร้างระเบียนชื่อเครื่อง (DNS Records)
          </p>
        </div>
        <div className="flex items-center gap-3">
          <Button
            onClick={handleClearCache}
            disabled={isClearingCache}
            variant="outline"
            className="cursor-pointer text-xs font-semibold gap-1.5 h-10 border-border bg-card/40 hover:bg-card text-muted-foreground hover:text-foreground"
          >
            <RefreshCw className={`h-3.5 w-3.5 ${isClearingCache ? "animate-spin" : ""}`} />
            Clear Cache
          </Button>

          {!isApplied && (
            <Button
              onClick={handleApplySettings}
              disabled={isApplying}
              className="cursor-pointer bg-amber-500 text-neutral-950 hover:bg-amber-400 font-bold gap-1.5 h-10 px-4 animate-pulse"
            >
              {isApplying ? (
                <>
                  <RefreshCw className="h-4 w-4 animate-spin" />
                  Applying...
                </>
              ) : (
                <>
                  <Check className="h-4.5 w-4.5" />
                  Apply DNS Zones
                </>
              )}
            </Button>
          )}
          {isApplied && (
            <div className="hidden sm:flex items-center gap-1.5 text-xs text-primary bg-primary/10 border border-primary/20 px-3 py-2 rounded-lg font-semibold">
              <CheckCircle2 className="h-4 w-4" />
              DNS Server Synced
            </div>
          )}
        </div>
      </div>

      {/* 1.1 Listen Interfaces — real interfaces from Interface Service, independent of DHCP Server */}
      <Card className="bg-card/25 border border-border/50 p-4 space-y-3">
        <div className="flex items-center justify-between pb-2 border-b border-border/40">
          <h2 className="text-sm font-bold text-foreground flex items-center gap-1.5">
            <Network className="h-4 w-4 text-primary" />
            DNS Server Listen Interfaces
          </h2>
          {isSavingInterfaces && (
            <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
          )}
        </div>
        <p className="text-xs text-muted-foreground">
          เลือก Interface จริงที่มีอยู่ในเครื่อง (ดึงจาก Interface Service) เพื่อใช้เป็น NS/auth-server ของ DNS Server — ค่านี้แยกอิสระจากการตั้งค่า DHCP Server
        </p>
        {availableInterfaces.length === 0 ? (
          <p className="text-xs text-muted-foreground italic">ไม่พบ Interface ที่มี Role เป็น LAN ในระบบ</p>
        ) : (
          <div className="flex flex-wrap gap-2">
            {availableInterfaces.map(iface => {
              const checked = selectedInterfaces.includes(iface.name)
              return (
                <label
                  key={iface.id}
                  className={`flex items-center gap-2 px-3 py-2 rounded-lg border cursor-pointer text-xs font-mono transition-all ${
                    checked
                      ? "bg-primary/10 border-primary/40 text-foreground"
                      : "bg-background/25 border-border/50 text-muted-foreground hover:bg-muted/15"
                  }`}
                >
                  <input
                    type="checkbox"
                    checked={checked}
                    disabled={isSavingInterfaces}
                    onChange={(e) => handleToggleInterface(iface.name, e.target.checked)}
                    className="cursor-pointer accent-primary"
                  />
                  {iface.name}
                  {iface.alias && iface.alias !== iface.name && (
                    <span className="text-muted-foreground/60">({iface.alias})</span>
                  )}
                </label>
              )
            })}
          </div>
        )}
      </Card>

      {/* 2. Split Screen Zones / Records */}
      <div className="grid grid-cols-1 lg:grid-cols-12 gap-6 items-start">
        {/* Left Side: Zones list */}
        <Card className="lg:col-span-4 bg-card/25 border border-border/50 p-4 space-y-4">
          <div className="flex items-center justify-between pb-2 border-b border-border/40">
            <h2 className="text-sm font-bold text-foreground flex items-center gap-1.5">
              <Database className="h-4 w-4 text-primary" />
              DNS Zones ({zones.length})
            </h2>
            <Button
              onClick={openCreateZoneModal}
              size="xs"
              className="cursor-pointer h-7 text-[11px] font-bold px-2.5 bg-primary text-primary-foreground hover:bg-primary/95"
            >
              <Plus className="h-3.5 w-3.5 mr-0.5" />
              Add Zone
            </Button>
          </div>

          <div className="relative">
            <Search className="absolute left-2 top-2.5 h-3.5 w-3.5 text-muted-foreground" />
            <Input
              type="text"
              value={zoneSearchQuery}
              onChange={(e) => setZoneSearchQuery(e.target.value)}
              placeholder="ค้นหาโดเมน/โซน..."
              className="pl-7 bg-background/50 placeholder:text-muted-foreground h-8 text-xs"
            />
          </div>

          <div className="space-y-2 max-h-[480px] overflow-y-auto pr-1">
            {filteredZones.map(zone => (
              <div
                key={zone.id}
                onClick={() => setSelectedZoneId(zone.id)}
                className={`group flex items-center justify-between p-3 rounded-lg border transition-all cursor-pointer ${
                  selectedZoneId === zone.id
                    ? "bg-primary/10 border-primary/40 text-foreground"
                    : "bg-background/25 border-border/50 hover:bg-muted/15 text-muted-foreground hover:text-foreground"
                }`}
              >
                <div className="space-y-1 select-none flex-1">
                  <div className="flex items-center gap-1.5">
                    <span className="font-semibold text-xs font-mono">{zone.zoneName}</span>
                    <Badge variant={zone.isAuthoritative ? "default" : "secondary"} className="text-[9px] px-1 py-0 h-3.5 scale-90">
                      {zone.isAuthoritative ? "Auth" : "Fwd"}
                    </Badge>
                  </div>
                  {!zone.isAuthoritative && zone.forwardTo && (
                    <p className="text-[10px] text-muted-foreground/60 font-mono">
                      {"->"} {zone.forwardTo}
                    </p>
                  )}
                </div>

                <div className="flex items-center gap-1.5 ml-2">
                  <Switch
                    checked={zone.enabled}
                    onCheckedChange={(checked) => handleToggleZone(zone.id, checked)}
                    className="scale-75 cursor-pointer data-[state=checked]:bg-primary"
                  />
                  <div className="opacity-0 group-hover:opacity-100 flex items-center gap-0.5 transition-all">
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      onClick={(e) => {
                        e.stopPropagation()
                        openEditZoneModal(zone)
                      }}
                      className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/40 h-6 w-6"
                    >
                      <Edit className="h-3 w-3" />
                    </Button>
                    <Button
                      variant="ghost"
                      size="icon-xs"
                      onClick={(e) => {
                        e.stopPropagation()
                        handleDeleteZone(zone.id, zone.zoneName)
                      }}
                      className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10 h-6 w-6"
                    >
                      <Trash2 className="h-3 w-3" />
                    </Button>
                  </div>
                </div>
              </div>
            ))}

            {filteredZones.length === 0 && (
              <div className="text-center p-6 text-muted-foreground text-xs">
                {zoneSearchQuery ? "ไม่พบโซนที่ค้นหา" : "ยังไม่มีการสร้าง DNS Zone"}
              </div>
            )}
          </div>
        </Card>

        {/* Right Side: DNS Records list for selected zone */}
        <div className="lg:col-span-8 space-y-6">
          {selectedZone ? (
            <Card className="bg-card/25 border border-border/50 p-6 space-y-4">
              <div className="border-b border-border/40 pb-3 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
                <div>
                  <h2 className="text-md font-bold text-foreground flex items-center gap-2">
                    <Server className="h-4.5 w-4.5 text-primary" />
                    DNS Records ของโซน {selectedZone.zoneName}
                  </h2>
                  <p className="text-xs text-muted-foreground mt-0.5">
                    {selectedZone.isAuthoritative 
                      ? "โซน Authoritative (ระบุชื่อโฮสต์และค่าไอพีโดยตรง)" 
                      : `โซน Forward (ระบบจะทำการส่งต่อคิวรีทั้งหมดไปให้ ${selectedZone.forwardTo})`}
                  </p>
                </div>
                {selectedZone.isAuthoritative && (
                  <Button
                    onClick={openCreateRecModal}
                    size="sm"
                    className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1 h-8 text-xs"
                  >
                    <Plus className="h-3.5 w-3.5" />
                    Add Record
                  </Button>
                )}
              </div>

              {selectedZone.isAuthoritative ? (
                <>
                  <div className="relative">
                    <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground pointer-events-none" />
                    <Input
                      type="text"
                      value={recordSearchQuery}
                      onChange={(e) => setRecordSearchQuery(e.target.value)}
                      placeholder="ค้นหาชื่อระเบียน, ประเภท หรือข้อมูล..."
                      className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9 text-xs"
                    />
                  </div>

                  <div className="rounded-lg border border-border/50 overflow-hidden bg-background/20">
                    <Table>
                      <TableHeader>
                        <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
                          <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[25%]">Host Name</th>
                          <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[15%]">Type</th>
                          <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[40%]">Value</th>
                          <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold w-[10%]">TTL</th>
                          <th className="p-3 w-[10%] text-right"></th>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {filteredRecords.length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={5} className="p-8 text-center text-muted-foreground text-xs">
                              {recordSearchQuery ? "ไม่พบระเบียน DNS ตามข้อความค้นหา" : "ยังไม่มีข้อมูลระเบียน DNS ในโซนนี้"}
                            </TableCell>
                          </TableRow>
                        ) : (
                          filteredRecords.map((rec) => (
                            <TableRow key={rec.id} className="border-b border-border/40 hover:bg-muted/15 font-mono text-xs">
                              <TableCell className="p-3">
                                <span className="text-foreground font-semibold font-sans">{rec.name}</span>
                              </TableCell>
                              <TableCell className="p-3">
                                <Badge variant="outline" className="bg-primary/5 text-primary border-primary/20 text-[10px] py-0 px-1.5 font-bold font-sans">
                                  {rec.type}
                                </Badge>
                              </TableCell>
                              <TableCell className="p-3 font-semibold text-foreground truncate max-w-[200px]" title={rec.value}>
                                {rec.value}
                              </TableCell>
                              <TableCell className="p-3 text-muted-foreground">
                                {rec.ttl}s
                              </TableCell>
                              <TableCell className="p-3 text-right">
                                <div className="flex items-center justify-end gap-1">
                                  <Button
                                    variant="ghost"
                                    size="icon-xs"
                                    onClick={() => openEditRecModal(rec)}
                                    className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50"
                                  >
                                    <Edit className="h-3.5 w-3.5" />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon-xs"
                                    onClick={() => handleDeleteRecord(rec.id, rec.name)}
                                    className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10"
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
                </>
              ) : (
                <div className="p-8 text-center border border-dashed border-border/50 rounded-lg bg-background/10 space-y-2">
                  <Globe className="h-8 w-8 text-muted-foreground/60 mx-auto" />
                  <p className="text-sm font-semibold text-foreground">โซนนี้ได้รับการตั้งค่าประเภทส่งต่อ (Forward Zone)</p>
                  <p className="text-xs text-muted-foreground max-w-md mx-auto leading-relaxed">
                    ระบบจะส่งคำขอความละเอียดชื่อระบบโดเมนทั้งหมดภายใต้โดเมน <span className="font-semibold text-primary">{selectedZone.zoneName}</span> ไปยัง
                    ที่อยู่ IP <span className="font-semibold text-cyan-400 font-mono">{selectedZone.forwardTo}</span> โดยอัตโนมัติ 
                    คุณไม่จำเป็นต้องเพิ่มระเบียน DNS เอง
                  </p>
                </div>
              )}
            </Card>
          ) : (
            <Card className="p-8 text-center border border-dashed border-border/50 rounded-lg bg-card/10">
              <Globe className="h-8 w-8 text-muted-foreground/50 mx-auto mb-2" />
              <p className="text-sm text-muted-foreground">กรุณาเลือกโซนด้านซ้าย หรือกดสร้างโซนใหม่เพื่อจัดการระเบียน DNS</p>
            </Card>
          )}
        </div>
      </div>

      {/* 3. Help alertbox */}
      <Alert className="border-dashed border-border bg-card/10 text-xs">
        <Info className="h-4 w-4 text-muted-foreground" />
        <AlertTitle className="font-bold text-foreground mb-0.5">ระเบียนประเภทต่างๆ ของ DNS Server:</AlertTitle>
        <AlertDescription className="text-muted-foreground leading-relaxed">
          <ul className="list-disc pl-4 space-y-1">
            <li><span className="font-semibold text-primary">A</span>: ชี้โดเมนย่อยไปที่ที่อยู่ IPv4 (เช่น router.pigate.local {"->"} 192.168.1.1)</li>
            <li><span className="font-semibold text-primary">AAAA</span>: ชี้โดเมนย่อยไปที่ที่อยู่ IPv6</li>
            <li><span className="font-semibold text-primary">CNAME</span>: ชื่อสมญา/ส่งต่อไปหาชื่อเครื่องอื่น (เช่น printer.pigate.local {"->"} hp-laser.pigate.local)</li>
            <li><span className="font-semibold text-primary">MX</span>: ชี้เซิร์ฟเวอร์รับส่งอีเมลประจำโดเมน (ระบุรูปแบบ [Preference] [Host] เช่น `10 mail.example.com`)</li>
            <li><span className="font-semibold text-primary">TXT</span>: ระบุข้อมูลข้อความทั่วไป เช่น SPF หรือคีย์ยืนยันตัวตน</li>
          </ul>
        </AlertDescription>
      </Alert>

      {/* 4. Zone Add/Edit Dialog Modal */}
      <Dialog open={isZoneModalOpen} modal={false} onOpenChange={setIsZoneModalOpen}>
        <DialogContent className="max-w-[450px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingZone ? "แก้ไข DNS Zone" : "เพิ่ม DNS Zone ใหม่"}
            </DialogTitle>
          </DialogHeader>

          <form onSubmit={handleSaveZone} className="space-y-4 text-sm">
            {zoneError && (
              <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                <AlertCircle className="h-4 w-4 text-red-400" />
                <AlertDescription className="text-red-400 text-xs">{zoneError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Zone Name */}
            <div className="space-y-1.5">
              <Label htmlFor="zone-name" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Zone Name / Local Domain (ชื่อโซน/โดเมน) <span className="text-red-500">*</span>
              </Label>
              <Input
                id="zone-name"
                type="text"
                required
                value={zoneName}
                onChange={(e) => setZoneName(e.target.value)}
                placeholder="เช่น office.local หรือ internal.net"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
            </div>

            {/* Field: Authoritative Toggle */}
            <div className="flex items-center justify-between bg-background/30 border border-border/40 rounded-lg p-3">
              <div className="space-y-0.5">
                <Label className="text-xs font-bold text-foreground">เป็นโซนหลัก (Authoritative Zone)</Label>
                <p className="text-[10px] text-muted-foreground">
                  เปิดหากต้องการกำหนด DNS Records เอง หรือปิดหากต้องการทำ DNS Forwarding
                </p>
              </div>
              <Switch
                checked={isAuthoritative}
                onCheckedChange={setIsAuthoritative}
                className="data-[state=checked]:bg-primary cursor-pointer scale-90"
              />
            </div>

            {/* Field: Forward To (Conditional) */}
            {!isAuthoritative && (
              <div className="space-y-1.5 animate-slide-in">
                <Label htmlFor="forward-ip" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Forward To Upstream IP (ส่งต่อไปที่เซิร์ฟเวอร์) <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="forward-ip"
                  type="text"
                  required
                  value={forwardTo}
                  onChange={(e) => setForwardTo(e.target.value)}
                  placeholder="เช่น 8.8.8.8 หรือ 1.1.1.1"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
            )}

            {/* Field: Allowed Client IPs */}
            <div className="space-y-1.5">
              <Label htmlFor="allowed-ips" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Allowed Client IPs (กลุ่มไอพีที่อนุญาตคิวรี)
              </Label>
              <Input
                id="allowed-ips"
                type="text"
                value={allowedIps}
                onChange={(e) => setAllowedIps(e.target.value)}
                placeholder="ระบุ any หรือคั่นด้วยลูกน้ำ เช่น 192.168.1.0/24"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border/40">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsZoneModalOpen(false)}
                className="cursor-pointer text-muted-foreground hover:bg-muted/30 h-9"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSaving}
                className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold px-5 h-9"
              >
                {isSaving ? "Saving..." : editingZone ? "Save Changes" : "Create Zone"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>

      {/* 5. Record Add/Edit Dialog Modal */}
      <Dialog open={isRecModalOpen} modal={false} onOpenChange={setIsRecModalOpen}>
        <DialogContent className="max-w-[450px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingRecord ? "แก้ไข DNS Record" : `เพิ่ม DNS Record ในโซน ${selectedZone?.zoneName}`}
            </DialogTitle>
          </DialogHeader>

          <form onSubmit={handleSaveRecord} className="space-y-4 text-sm">
            {recError && (
              <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                <AlertCircle className="h-4 w-4 text-red-400" />
                <AlertDescription className="text-red-400 text-xs">{recError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Host Name */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-name" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Host Name (ชื่อโดเมนย่อย)
              </Label>
              <div className="flex items-center gap-1.5">
                <Input
                  id="rec-name"
                  type="text"
                  value={recName}
                  onChange={(e) => setRecName(e.target.value)}
                  placeholder="@ หรือเว้นว่าง หรือโดเมนย่อย เช่น printer"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs flex-1"
                />
                <span className="text-xs text-muted-foreground font-mono font-semibold">
                  .{selectedZone?.zoneName}
                </span>
              </div>
              <p className="text-[10px] text-muted-foreground/60 italic">
                ใส่ @ หรือเว้นว่าง หากต้องการให้ชี้ไปที่ตัวโดเมนหลักโดยตรง ({selectedZone?.zoneName})
              </p>
            </div>

            {/* Field: Record Type */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-type" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Record Type (ประเภทระเบียน)
              </Label>
              <select
                id="rec-type"
                value={recType}
                onChange={(e) => setRecType(e.target.value)}
                className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
              >
                <option value="A">A (Address)</option>
                <option value="AAAA">AAAA (IPv6 Address)</option>
                <option value="CNAME">CNAME (Alias)</option>
                <option value="MX">MX (Mail Exchange)</option>
                <option value="TXT">TXT (Text)</option>
                <option value="PTR">PTR (Pointer)</option>
              </select>
            </div>

            {/* Field: Value */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-val" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Record Value (ข้อมูลระเบียน) <span className="text-red-500">*</span>
              </Label>
              <Input
                id="rec-val"
                type="text"
                required
                value={recValue}
                onChange={(e) => setRecValue(e.target.value)}
                placeholder={
                  recType === "A" 
                    ? "เช่น 192.168.1.15" 
                    : recType === "CNAME"
                      ? "เช่น pigate.local"
                      : recType === "MX"
                        ? "ระบุลำดับความสำคัญและชื่อเซิร์ฟเวอร์ เช่น 10 mail.example.com"
                        : "ค่าระเบียนตามประเภท"
                }
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
            </div>

            {/* Field: TTL */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-ttl" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                TTL (Seconds)
              </Label>
              <Input
                id="rec-ttl"
                type="number"
                required
                min="30"
                value={recTtl}
                onChange={(e) => setRecTtl(e.target.value)}
                placeholder="300"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border/40">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsRecModalOpen(false)}
                className="cursor-pointer text-muted-foreground hover:bg-muted/30 h-9"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSaving}
                className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold px-5 h-9"
              >
                {isSaving ? "Saving..." : editingRecord ? "Save Record" : "Create Record"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
