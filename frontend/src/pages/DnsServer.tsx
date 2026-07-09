import { useState, useMemo, useEffect } from "react"
import { getErrorMessage } from "@/lib/errors"
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
import { type DNSZone, type DNSRecord, type NetworkInterface } from "@/data-mockup/mockData"
import { dnsServerService } from "@/services/dnsServerService"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/hooks/useAlert"
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

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body.
    // selectedZoneId is still its initial null value on this first-mount load.
    const initialLoad = async () => {
      try {
        const [data, ifaces, settings] = await Promise.all([
          dnsServerService.getZones(),
          interfaceService.getAll(),
          dnsServerService.getSettings(),
        ])
        setZones(data || [])
        if ((data || []).length > 0) {
          setSelectedZoneId(data[0].id)
        }
        setAvailableInterfaces((ifaces || []).filter(i => i.role === "LAN"))
        setSelectedInterfaces(settings.interfaces || [])
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูล DNS Server ได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

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
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถบันทึก Interface ของ DNS Server ได้: " + getErrorMessage(err))
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
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบโซนได้: " + getErrorMessage(err))
      }
    }
  }

  const handleToggleZone = async (id: string, checked: boolean) => {
    try {
      await dnsServerService.toggleZone(id)
      setZones(prev => prev.map(z => z.id === id ? { ...z, enabled: checked } : z))
      setIsApplied(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปิด/ปิดโซนได้: " + getErrorMessage(err))
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
    } catch (err) {
      setZoneError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
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
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบระเบียนได้: " + getErrorMessage(err))
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
    } catch (err) {
      setRecError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูลระเบียน")
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
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเริ่มระบบ DNS เข้ากับ OS Kernel ได้: " + getErrorMessage(err))
    } finally {
      setIsApplying(false)
    }
  }

  const handleClearCache = async () => {
    setIsClearingCache(true)
    try {
      await dnsServerService.clearCache()
      await alert("สำเร็จ", "เคลียร์หน่วยความจำแคช DNS ของระบบเรียบร้อยแล้ว")
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเคลียร์หน่วยความจำแคชได้: " + getErrorMessage(err))
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
    <div className="space-y-4">
      {/* 1. Listen Interfaces — real interfaces from Interface Service, independent of DHCP Server */}
      <Card>
        <CardHeader className="flex flex-col gap-3 space-y-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <Network className="h-4 w-4 text-muted-foreground" />
              DNS Server Listen Interfaces
              {isSavingInterfaces && (
                <Loader2 className="h-3.5 w-3.5 animate-spin text-muted-foreground" />
              )}
            </CardTitle>
            <CardDescription className="text-xs">
              เลือก Interface จริงที่มีอยู่ในเครื่อง (ดึงจาก Interface Service) เพื่อใช้เป็น NS/auth-server ของ DNS Server — ค่านี้แยกอิสระจากการตั้งค่า DHCP Server
            </CardDescription>
          </div>
          <div className="flex flex-wrap items-center gap-3">
            <Button
              onClick={handleClearCache}
              disabled={isClearingCache}
              variant="outline"
              size="sm"
              className="cursor-pointer gap-2"
            >
              <RefreshCw className={`h-4 w-4 ${isClearingCache ? "animate-spin" : ""}`} />
              Clear Cache
            </Button>
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
                    Applying...
                  </>
                ) : (
                  <>
                    <Check className="h-4 w-4" />
                    Apply DNS Zones
                  </>
                )}
              </Button>
            )}
            {isApplied && (
              <div className="flex h-8 items-center gap-1.5 rounded-lg border border-primary/20 bg-primary/10 px-3 text-xs font-medium text-primary">
                <CheckCircle2 className="h-4 w-4" />
                DNS Server Synced
              </div>
            )}
          </div>
        </CardHeader>
        <CardContent>
          {availableInterfaces.length === 0 ? (
            <p className="text-xs italic text-muted-foreground">ไม่พบ Interface ที่มี Role เป็น LAN ในระบบ</p>
          ) : (
            <div className="flex flex-wrap gap-2">
              {availableInterfaces.map(iface => {
                const checked = selectedInterfaces.includes(iface.name)
                return (
                  <label
                    key={iface.id}
                    className={`flex cursor-pointer items-center gap-2 rounded-lg border px-3 py-2 font-mono text-xs transition ${
                      checked
                        ? "border-primary/40 bg-primary/10 text-foreground"
                        : "border-border bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      disabled={isSavingInterfaces}
                      onChange={(e) => handleToggleInterface(iface.name, e.target.checked)}
                      className="h-4 w-4 cursor-pointer accent-primary"
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
        </CardContent>
      </Card>

      {/* 2. Split Screen Zones / Records */}
      <div className="grid grid-cols-1 items-start gap-4 lg:grid-cols-12">
        {/* Left Side: Zones list */}
        <Card className="lg:col-span-4">
          <CardHeader className="flex flex-row items-center justify-between space-y-0">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <Database className="h-4 w-4 text-muted-foreground" />
              DNS Zones
              <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                {zones.length}
              </Badge>
            </CardTitle>
            <Button
              onClick={openCreateZoneModal}
              size="sm"
              className="cursor-pointer gap-1.5 font-semibold"
            >
              <Plus className="h-4 w-4" />
              Add Zone
            </Button>
          </CardHeader>

          <CardContent className="space-y-3">
            <div className="relative">
              <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                value={zoneSearchQuery}
                onChange={(e) => setZoneSearchQuery(e.target.value)}
                placeholder="ค้นหาโดเมน/โซน..."
                className="h-9 pl-8 text-xs"
              />
            </div>

            <div className="max-h-[480px] space-y-2 overflow-y-auto pr-1">
              {filteredZones.map(zone => (
                <div
                  key={zone.id}
                  onClick={() => setSelectedZoneId(zone.id)}
                  className={`flex cursor-pointer items-center justify-between rounded-lg border p-3 transition ${
                    selectedZoneId === zone.id
                      ? "border-primary/40 bg-primary/10 text-foreground"
                      : "border-border bg-muted/50 text-muted-foreground hover:bg-muted hover:text-foreground"
                  }`}
                >
                  <div className="flex-1 select-none space-y-1">
                    <div className="flex items-center gap-1.5">
                      <span className="font-mono text-xs font-semibold">{zone.zoneName}</span>
                      <Badge
                        variant="outline"
                        className={`rounded px-1.5 py-0 text-[10px] font-medium ${zone.isAuthoritative
                          ? "border-primary/20 bg-primary/10 text-primary"
                          : "border-border bg-muted text-muted-foreground"
                          }`}
                      >
                        {zone.isAuthoritative ? "Auth" : "Fwd"}
                      </Badge>
                    </div>
                    {!zone.isAuthoritative && zone.forwardTo && (
                      <p className="font-mono text-[10px] text-muted-foreground/60">
                        {"->"} {zone.forwardTo}
                      </p>
                    )}
                  </div>

                  <div className="ml-2 flex items-center gap-2">
                    <Switch
                      size="sm"
                      checked={zone.enabled}
                      onCheckedChange={(checked) => handleToggleZone(zone.id, checked)}
                      className="cursor-pointer"
                    />
                    <div className="flex items-center gap-1">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={(e) => {
                          e.stopPropagation()
                          openEditZoneModal(zone)
                        }}
                        className="cursor-pointer text-muted-foreground hover:bg-muted hover:text-foreground"
                        title="แก้ไขโซน"
                      >
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        onClick={(e) => {
                          e.stopPropagation()
                          handleDeleteZone(zone.id, zone.zoneName)
                        }}
                        className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                        title="ลบโซน"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </div>
                </div>
              ))}

              {filteredZones.length === 0 && (
                <div className="p-6 text-center text-xs text-muted-foreground">
                  {zoneSearchQuery ? "ไม่พบโซนที่ค้นหา" : "ยังไม่มีการสร้าง DNS Zone"}
                </div>
              )}
            </div>
          </CardContent>
        </Card>

        {/* Right Side: DNS Records list for selected zone */}
        <div className="space-y-4 lg:col-span-8">
          {selectedZone ? (
            <Card>
              <CardHeader className="flex flex-col gap-3 space-y-0 sm:flex-row sm:items-center sm:justify-between">
                <div className="space-y-1">
                  <CardTitle className="flex items-center gap-2 text-base font-semibold">
                    <Server className="h-4 w-4 text-muted-foreground" />
                    DNS Records ของโซน <span className="font-mono">{selectedZone.zoneName}</span>
                  </CardTitle>
                  <CardDescription className="text-xs">
                    {selectedZone.isAuthoritative
                      ? "โซน Authoritative (ระบุชื่อโฮสต์และค่าไอพีโดยตรง)"
                      : `โซน Forward (ระบบจะทำการส่งต่อคิวรีทั้งหมดไปให้ ${selectedZone.forwardTo})`}
                  </CardDescription>
                </div>
                {selectedZone.isAuthoritative && (
                  <Button
                    onClick={openCreateRecModal}
                    size="sm"
                    className="cursor-pointer gap-1.5 font-semibold"
                  >
                    <Plus className="h-4 w-4" />
                    Add Record
                  </Button>
                )}
              </CardHeader>

              <CardContent className="space-y-4">
                {selectedZone.isAuthoritative ? (
                  <>
                    <div className="relative">
                      <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
                      <Input
                        type="text"
                        value={recordSearchQuery}
                        onChange={(e) => setRecordSearchQuery(e.target.value)}
                        placeholder="ค้นหาชื่อระเบียน, ประเภท หรือข้อมูล..."
                        className="h-9 pl-8 text-xs"
                      />
                    </div>

                    <Table>
                      <TableHeader>
                        <TableRow className="hover:bg-transparent">
                          <TableHead className="w-[25%] text-xs font-medium text-muted-foreground">Host Name</TableHead>
                          <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Type</TableHead>
                          <TableHead className="w-[40%] text-xs font-medium text-muted-foreground">Value</TableHead>
                          <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">TTL</TableHead>
                          <TableHead className="w-[10%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                        </TableRow>
                      </TableHeader>
                      <TableBody>
                        {filteredRecords.length === 0 ? (
                          <TableRow>
                            <TableCell colSpan={5} className="py-8 text-center text-xs text-muted-foreground">
                              {recordSearchQuery ? "ไม่พบระเบียน DNS ตามข้อความค้นหา" : "ยังไม่มีข้อมูลระเบียน DNS ในโซนนี้"}
                            </TableCell>
                          </TableRow>
                        ) : (
                          filteredRecords.map((rec) => (
                            <TableRow key={rec.id}>
                              <TableCell className="py-3">
                                <span className="text-xs font-medium text-foreground">{rec.name}</span>
                              </TableCell>
                              <TableCell className="py-3">
                                <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] font-medium text-primary">
                                  {rec.type}
                                </Badge>
                              </TableCell>
                              <TableCell className="max-w-[200px] truncate py-3 font-mono text-xs font-medium text-foreground" title={rec.value}>
                                {rec.value}
                              </TableCell>
                              <TableCell className="py-3 font-mono text-xs text-muted-foreground">
                                {rec.ttl}s
                              </TableCell>
                              <TableCell className="py-3 text-right">
                                <div className="flex items-center justify-end gap-2">
                                  <Button
                                    variant="outline"
                                    size="icon-sm"
                                    onClick={() => openEditRecModal(rec)}
                                    className="cursor-pointer text-muted-foreground hover:text-foreground"
                                    title="แก้ไขระเบียน"
                                  >
                                    <Edit className="h-4 w-4" />
                                  </Button>
                                  <Button
                                    variant="ghost"
                                    size="icon-sm"
                                    onClick={() => handleDeleteRecord(rec.id, rec.name)}
                                    className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                    title="ลบระเบียน"
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
                  </>
                ) : (
                  <div className="space-y-2 rounded-lg border border-dashed border-border p-8 text-center">
                    <Globe className="mx-auto h-8 w-8 text-muted-foreground/60" />
                    <p className="text-sm font-semibold text-foreground">โซนนี้ได้รับการตั้งค่าประเภทส่งต่อ (Forward Zone)</p>
                    <p className="mx-auto max-w-md text-xs leading-relaxed text-muted-foreground">
                      ระบบจะส่งคำขอความละเอียดชื่อระบบโดเมนทั้งหมดภายใต้โดเมน <strong className="text-foreground">{selectedZone.zoneName}</strong> ไปยัง
                      ที่อยู่ IP <strong className="font-mono text-foreground">{selectedZone.forwardTo}</strong> โดยอัตโนมัติ
                      คุณไม่จำเป็นต้องเพิ่มระเบียน DNS เอง
                    </p>
                  </div>
                )}
              </CardContent>
            </Card>
          ) : (
            <Card>
              <CardContent className="flex flex-col items-center justify-center gap-2 py-8 text-center">
                <Globe className="h-8 w-8 text-muted-foreground/50" />
                <p className="text-sm text-muted-foreground">กรุณาเลือกโซนด้านซ้าย หรือกดสร้างโซนใหม่เพื่อจัดการระเบียน DNS</p>
              </CardContent>
            </Card>
          )}
        </div>
      </div>

      {/* 3. Info note */}
      <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <div>
          <strong className="text-foreground">ระเบียนประเภทต่างๆ ของ DNS Server:</strong>
          <ul className="mt-1 list-disc space-y-1 pl-4">
            <li><strong className="text-foreground">A</strong>: ชี้โดเมนย่อยไปที่ที่อยู่ IPv4 (เช่น router.pigate.local {"->"} 192.168.1.1)</li>
            <li><strong className="text-foreground">AAAA</strong>: ชี้โดเมนย่อยไปที่ที่อยู่ IPv6</li>
            <li><strong className="text-foreground">CNAME</strong>: ชื่อสมญา/ส่งต่อไปหาชื่อเครื่องอื่น (เช่น printer.pigate.local {"->"} hp-laser.pigate.local)</li>
            <li><strong className="text-foreground">MX</strong>: ชี้เซิร์ฟเวอร์รับส่งอีเมลประจำโดเมน (ระบุรูปแบบ [Preference] [Host] เช่น 10 mail.example.com)</li>
            <li><strong className="text-foreground">TXT</strong>: ระบุข้อมูลข้อความทั่วไป เช่น SPF หรือคีย์ยืนยันตัวตน</li>
          </ul>
        </div>
      </div>

      {/* 4. Zone Add/Edit Dialog Modal */}
      <Drawer direction="right" open={isZoneModalOpen} onOpenChange={setIsZoneModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[450px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingZone ? "แก้ไข DNS Zone" : "เพิ่ม DNS Zone ใหม่"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveZone} className="space-y-4 text-sm">
            {zoneError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{zoneError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Zone Name */}
            <div className="space-y-1.5">
              <Label htmlFor="zone-name" className="block text-xs font-medium text-muted-foreground">
                Zone Name / Local Domain (ชื่อโซน/โดเมน) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="zone-name"
                type="text"
                required
                value={zoneName}
                onChange={(e) => setZoneName(e.target.value)}
                placeholder="เช่น office.local หรือ internal.net"
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Field: Authoritative Toggle */}
            <div className="flex items-center justify-between rounded-lg border border-border bg-muted/50 p-3">
              <div className="space-y-0.5">
                <Label className="text-xs font-semibold text-foreground">เป็นโซนหลัก (Authoritative Zone)</Label>
                <p className="text-[10px] text-muted-foreground">
                  เปิดหากต้องการกำหนด DNS Records เอง หรือปิดหากต้องการทำ DNS Forwarding
                </p>
              </div>
              <Switch
                checked={isAuthoritative}
                onCheckedChange={setIsAuthoritative}
                className="cursor-pointer"
              />
            </div>

            {/* Field: Forward To (Conditional) */}
            {!isAuthoritative && (
              <div className="animate-slide-in space-y-1.5">
                <Label htmlFor="forward-ip" className="block text-xs font-medium text-muted-foreground">
                  Forward To Upstream IP (ส่งต่อไปที่เซิร์ฟเวอร์) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="forward-ip"
                  type="text"
                  required
                  value={forwardTo}
                  onChange={(e) => setForwardTo(e.target.value)}
                  placeholder="เช่น 8.8.8.8 หรือ 1.1.1.1"
                  className="h-9 font-mono text-sm"
                />
              </div>
            )}

            {/* Field: Allowed Client IPs */}
            <div className="space-y-1.5">
              <Label htmlFor="allowed-ips" className="block text-xs font-medium text-muted-foreground">
                Allowed Client IPs (กลุ่มไอพีที่อนุญาตคิวรี)
              </Label>
              <Input
                id="allowed-ips"
                type="text"
                value={allowedIps}
                onChange={(e) => setAllowedIps(e.target.value)}
                placeholder="ระบุ any หรือคั่นด้วยลูกน้ำ เช่น 192.168.1.0/24"
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsZoneModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSaving}
                className="cursor-pointer px-6 font-semibold"
              >
                {isSaving ? "Saving..." : editingZone ? "Save Changes" : "Create Zone"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>

      {/* 5. Record Add/Edit Drawer */}
      <Drawer direction="right" open={isRecModalOpen} onOpenChange={setIsRecModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[450px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingRecord ? "แก้ไข DNS Record" : `เพิ่ม DNS Record ในโซน ${selectedZone?.zoneName}`}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSaveRecord} className="space-y-4 text-sm">
            {recError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{recError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Host Name */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-name" className="block text-xs font-medium text-muted-foreground">
                Host Name (ชื่อโดเมนย่อย)
              </Label>
              <div className="flex items-center gap-1.5">
                <Input
                  id="rec-name"
                  type="text"
                  value={recName}
                  onChange={(e) => setRecName(e.target.value)}
                  placeholder="@ หรือเว้นว่าง หรือโดเมนย่อย เช่น printer"
                  className="h-9 flex-1 font-mono text-sm"
                />
                <span className="font-mono text-xs font-semibold text-muted-foreground">
                  .{selectedZone?.zoneName}
                </span>
              </div>
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                ใส่ @ หรือเว้นว่าง หากต้องการให้ชี้ไปที่ตัวโดเมนหลักโดยตรง ({selectedZone?.zoneName})
              </p>
            </div>

            {/* Field: Record Type */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-type" className="block text-xs font-medium text-muted-foreground">
                Record Type (ประเภทระเบียน)
              </Label>
              <select
                id="rec-type"
                value={recType}
                onChange={(e) => setRecType(e.target.value)}
                className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
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
              <Label htmlFor="rec-val" className="block text-xs font-medium text-muted-foreground">
                Record Value (ข้อมูลระเบียน) <span className="text-destructive">*</span>
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
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Field: TTL */}
            <div className="space-y-1.5">
              <Label htmlFor="rec-ttl" className="block text-xs font-medium text-muted-foreground">
                TTL (Seconds) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="rec-ttl"
                type="number"
                required
                min="30"
                value={recTtl}
                onChange={(e) => setRecTtl(e.target.value)}
                placeholder="300"
                className="h-9 font-mono text-sm"
              />
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsRecModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                disabled={isSaving}
                className="cursor-pointer px-6 font-semibold"
              >
                {isSaving ? "Saving..." : editingRecord ? "Save Record" : "Create Record"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
