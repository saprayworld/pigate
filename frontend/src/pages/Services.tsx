import { useState, useMemo, useEffect, type ReactNode } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Sliders,
  SlidersHorizontal,
  Plus,
  Search,
  Edit,
  Trash2,
  Lock,
  AlertCircle,
  Network,
  ShieldCheck,
  Loader2
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { type ServiceEntry, type ServiceObject } from "@/data-mockup/mockData"
import { serviceObjectService } from "@/services/serviceObjectService"
import { useAlert } from "@/hooks/useAlert"
import { cn } from "@/lib/utils"

// UI-side cap on how many rows the form allows adding — a soft guardrail
// only; the backend enforces the authoritative cap from its own config
// (DefaultMaxObjectEntries) and its rejection is surfaced as a formError
// (see handleSave's catch block) rather than silently swallowed.
const MAX_FORM_ENTRIES = 64

const SINGLE_PORT_REGEX = /^\d+$/
const RANGE_PORT_REGEX = /^(\d+)-(\d+)$/

let entryRowKeySeq = 0
function nextEntryRowKey() {
  entryRowKeySeq += 1
  return `row-${entryRowKeySeq}`
}

type FormEntryRow = { key: string; protocol: ServiceEntry["protocol"]; port: string }

// Falls back to the legacy protocol/port pair when `entries` is empty/absent
// (e.g. very old localStorage records) — mirrors normalizeService() in
// serviceObjectService.ts but kept local so this component doesn't need to
// import it directly.
function serviceEntries(svc: ServiceObject): ServiceEntry[] {
  return svc.entries && svc.entries.length > 0
    ? svc.entries
    : [{ protocol: svc.protocol, port: svc.port }]
}

function protocolBadge(protocol: ServiceEntry["protocol"], key?: string | number) {
  if (protocol === "TCP") {
    return (
      <Badge key={key} variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-primary">
        TCP
      </Badge>
    )
  }
  if (protocol === "UDP") {
    return (
      <Badge key={key} variant="outline" className="rounded border-warning/20 bg-warning/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-warning">
        UDP
      </Badge>
    )
  }
  if (protocol === "TCP/UDP") {
    return (
      <Badge key={key} variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-primary">
        TCP/UDP
      </Badge>
    )
  }
  return (
    <Badge key={key} variant="secondary" className="rounded px-1.5 py-0.5 font-mono text-[10px] font-medium">
      ICMP
    </Badge>
  )
}

// Helper: Dashboard-style stat card (mirrors Dashboard's StatCard, value accepts a node)
function StatCard({
  icon: Icon,
  title,
  value,
}: {
  icon: typeof Sliders
  title: string
  value: ReactNode
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
        <div className="text-2xl font-bold tracking-tight text-foreground">{value}</div>
      </CardContent>
    </Card>
  )
}

export default function Services() {
  const { alert, confirm } = useAlert()

  // --- State ---
  const [services, setServices] = useState<ServiceObject[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState("")
  const [protoFilter, setProtoFilter] = useState<"All" | "TCP" | "UDP" | "TCP/UDP" | "ICMP">("All")

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingObject, setEditingObject] = useState<ServiceObject | null>(null)

  // Form fields
  const [formName, setFormName] = useState("")
  const [formEntries, setFormEntries] = useState<FormEntryRow[]>([
    { key: nextEntryRowKey(), protocol: "TCP", port: "" },
  ])
  const [formError, setFormError] = useState("")

  // Fetch logic
  const loadServices = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const data = await serviceObjectService.getAll()
      setServices(data)
    } catch (err) {
      console.error(err)
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูลวัตถุบริการได้: " + getErrorMessage(err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
        const data = await serviceObjectService.getAll()
        setServices(data)
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูลวัตถุบริการได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

  // --- Statistics ---
  const stats = useMemo(() => {
    const total = services.length
    const systemCount = services.filter(s => s.type === "system").length
    const customCount = services.filter(s => s.type === "custom").length
    const tcpCount = services.filter(s => serviceEntries(s).some(e => e.protocol.includes("TCP"))).length
    const udpCount = services.filter(s => serviceEntries(s).some(e => e.protocol.includes("UDP"))).length
    return { total, systemCount, customCount, tcpCount, udpCount }
  }, [services])

  // --- Filtered Services ---
  const filteredServices = useMemo(() => {
    return services.filter(svc => {
      const entries = serviceEntries(svc)
      const query = searchQuery.toLowerCase()
      const matchSearch =
        svc.name.toLowerCase().includes(query) ||
        entries.some(e => e.port.toLowerCase().includes(query) || e.protocol.toLowerCase().includes(query))

      const matchProto =
        protoFilter === "All" || entries.some(e => e.protocol === protoFilter)

      return matchSearch && matchProto
    }).sort((a, b) => a.name.localeCompare(b.name))
  }, [services, searchQuery, protoFilter])

  // --- CRUD Actions ---
  const openCreateModal = () => {
    setEditingObject(null)
    setFormName("")
    setFormEntries([{ key: nextEntryRowKey(), protocol: "TCP", port: "" }])
    setFormError("")
    setIsModalOpen(true)
  }

  const openEditModal = (svc: ServiceObject) => {
    if (svc.type === "system") return // Safety block
    setEditingObject(svc)
    setFormName(svc.name)
    setFormEntries(
      serviceEntries(svc).map(e => ({ key: nextEntryRowKey(), protocol: e.protocol, port: e.port }))
    )
    setFormError("")
    setIsModalOpen(true)
  }

  const handleDelete = async (id: string, name: string) => {
    const svc = services.find(s => s.id === id)
    if (!svc) return
    if (svc.type === "system") {
      await alert("การดำเนินการล้มเหลว", `ไม่สามารถลบวัตถุบริการของระบบ (System Predefined) "${name}" ได้`)
      return
    }

    if (svc.refPolicies.length > 0) {
      await alert("การดำเนินการล้มเหลว", `ไม่สามารถลบ "${name}" ได้ เนื่องจากถูกอ้างอิงอยู่ในนโยบายไฟร์วอลล์: ${svc.refPolicies.join(", ")}`)
      return
    }

    if (await confirm("ยืนยันการลบ", `คุณต้องการลบวัตถุบริการ "${name}" ใช่หรือไม่?`)) {
      try {
        await serviceObjectService.delete(id)
        await loadServices(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบข้อมูลได้: " + getErrorMessage(err))
      }
    }
  }

  // --- Form row management ---
  const addEntryRow = () => {
    setFormEntries(prev => (prev.length >= MAX_FORM_ENTRIES ? prev : [...prev, { key: nextEntryRowKey(), protocol: "TCP", port: "" }]))
  }

  const removeEntryRow = (key: string) => {
    setFormEntries(prev => (prev.length <= 1 ? prev : prev.filter(r => r.key !== key)))
  }

  const updateEntryProtocol = (key: string, protocol: ServiceEntry["protocol"]) => {
    setFormEntries(prev =>
      prev.map(r => {
        if (r.key !== key) return r
        if (protocol === "ICMP") return { ...r, protocol, port: "-" }
        return { ...r, protocol, port: r.port === "-" ? "" : r.port }
      })
    )
  }

  const updateEntryPort = (key: string, port: string) => {
    setFormEntries(prev => prev.map(r => (r.key === key ? { ...r, port } : r)))
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    // 1. Validation Name format
    const nameRegex = /^[a-zA-Z0-9_]+$/
    if (!nameRegex.test(formName)) {
      setFormError("ชื่อบริการต้องใช้ภาษาอังกฤษ ตัวเลข หรือเครื่องหมาย _ เท่านั้น (ห้ามเว้นวรรค)")
      return
    }

    // 2. Duplicate Name check
    const isDuplicate = services.some(
      s => s.name.toLowerCase() === formName.toLowerCase() && (!editingObject || s.id !== editingObject.id)
    )
    if (isDuplicate) {
      setFormError(`มีชื่อวัตถุบริการ "${formName}" อยู่ในระบบแล้ว`)
      return
    }

    // 3. Per-row port validation
    for (let i = 0; i < formEntries.length; i++) {
      const row = formEntries[i]
      const rowLabel = `แถวที่ ${i + 1}`

      if (row.protocol === "ICMP") {
        continue // port is locked to "-"
      }

      const port = row.port.trim()
      if (!port) {
        setFormError(`${rowLabel}: กรุณากรอกข้อมูลพอร์ต`)
        return
      }

      if (SINGLE_PORT_REGEX.test(port)) {
        const pNum = parseInt(port, 10)
        if (pNum < 1 || pNum > 65535) {
          setFormError(`${rowLabel}: หมายเลขพอร์ตต้องอยู่ระหว่างช่วง 1-65535`)
          return
        }
      } else if (RANGE_PORT_REGEX.test(port)) {
        const matches = port.match(RANGE_PORT_REGEX)
        if (matches) {
          const start = parseInt(matches[1], 10)
          const end = parseInt(matches[2], 10)
          if (start < 1 || start > 65535 || end < 1 || end > 65535) {
            setFormError(`${rowLabel}: หมายเลขพอร์ตต้นทางและปลายทางต้องอยู่ระหว่างช่วง 1-65535`)
            return
          }
          if (start >= end) {
            setFormError(`${rowLabel}: พอร์ตเริ่มต้นต้องมีค่าน้อยกว่าพอร์ตสิ้นสุดในการระบุช่วงพอร์ต`)
            return
          }
        }
      } else {
        setFormError(`${rowLabel}: รูปแบบพอร์ตไม่ถูกต้อง (ต้องระบุเป็นพอร์ตเดี่ยว เช่น 80 หรือแบบช่วง เช่น 8080-8085)`)
        return
      }
    }

    // 4. Normalize entries and check duplicates within the form
    const normalizedEntries: ServiceEntry[] = formEntries.map(r => ({
      protocol: r.protocol,
      port: r.protocol === "ICMP" ? "-" : r.port.trim(),
    }))

    const seen = new Set<string>()
    for (let i = 0; i < normalizedEntries.length; i++) {
      const key = `${normalizedEntries[i].protocol}|${normalizedEntries[i].port}`
      if (seen.has(key)) {
        setFormError(`แถวที่ ${i + 1}: รายการโปรโตคอล/พอร์ตนี้ซ้ำกับแถวอื่นในฟอร์ม`)
        return
      }
      seen.add(key)
    }

    try {
      const payload = {
        name: formName,
        protocol: normalizedEntries[0].protocol,
        port: normalizedEntries[0].port,
        entries: normalizedEntries,
      }
      if (editingObject) {
        // Edit
        await serviceObjectService.update(editingObject.id, payload)
      } else {
        // Create
        await serviceObjectService.create(payload)
      }
      await loadServices(false)
      setIsModalOpen(false)
    } catch (err) {
      setFormError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    }
  }

  const protoFilters = ["All", "TCP", "UDP", "TCP/UDP", "ICMP"] as const

  return (
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={Sliders} title="Total Services" value={stats.total} />
        <StatCard icon={ShieldCheck} title="System Default" value={stats.systemCount} />
        <StatCard icon={SlidersHorizontal} title="Custom Sets" value={stats.customCount} />
        <StatCard
          icon={Network}
          title="TCP / UDP"
          value={
            <>
              {stats.tcpCount} <span className="text-xs font-normal text-muted-foreground">TCP</span>
              {" / "}
              {stats.udpCount} <span className="text-xs font-normal text-muted-foreground">UDP</span>
            </>
          }
        />
      </div>

      {/* 2. Services table */}
      <Card>
        <CardHeader className="flex flex-col gap-4 space-y-0 sm:flex-row sm:items-center sm:justify-between">
            <div className="space-y-1">
              <CardTitle className="flex items-center gap-2 text-base font-semibold">
                <Sliders className="h-4 w-4 text-muted-foreground" />
                Service Objects
                <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                  {stats.total}
                </Badge>
              </CardTitle>
              <CardDescription className="text-xs">
                ระบุโปรโตคอล TCP/UDP และช่วงพอร์ต (รองรับหลายรายการต่อวัตถุ) เพื่อนำไปใช้อ้างอิงเป็นกลุ่มบริการใน Firewall Policy
              </CardDescription>
            </div>

            <div className="flex flex-wrap items-center gap-3">
              {/* Search */}
              <div className="relative w-full sm:w-[200px]">
                <Search className="pointer-events-none absolute top-2 left-2.5 h-4 w-4 text-muted-foreground" />
                <Input
                  type="text"
                  value={searchQuery}
                  onChange={(e) => setSearchQuery(e.target.value)}
                  placeholder="ค้นหาบริการ, พอร์ต..."
                  className="h-8 pl-8 text-xs"
                />
              </div>
              <Button size="sm" onClick={openCreateModal} className="cursor-pointer gap-1.5 font-semibold">
                <Plus className="h-4 w-4" />
                Create New Service
              </Button>
            </div>
          </CardHeader>

          <CardContent className="space-y-4">
            {/* Protocol filters */}
            <div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">
              {protoFilters.map((proto) => (
                <button
                  key={proto}
                  onClick={() => setProtoFilter(proto)}
                  className={cn(
                    "cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition",
                    protoFilter === proto
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground"
                  )}
                >
                  {proto}
                </button>
              ))}
            </div>

            {/* Table view */}
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-[25%] text-xs font-medium text-muted-foreground">Service Name</TableHead>
                  <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Protocol</TableHead>
                  <TableHead className="w-[30%] text-xs font-medium text-muted-foreground">Port Range / Details</TableHead>
                  <TableHead className="w-[20%] text-xs font-medium text-muted-foreground">Type</TableHead>
                  <TableHead className="w-[10%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  <TableRow>
                    <TableCell colSpan={5} className="py-12 text-center text-xs text-muted-foreground">
                      <div className="flex flex-col items-center justify-center gap-2 py-4">
                        <Loader2 className="h-6 w-6 animate-spin text-primary" />
                        <span>กำลังโหลดข้อมูล...</span>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : filteredServices.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={5} className="py-8 text-center text-xs text-muted-foreground">
                      ไม่พบข้อมูลวัตถุบริการที่ค้นหา
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredServices.map((svc) => {
                    const entries = serviceEntries(svc)
                    const uniqueProtocols = Array.from(new Set(entries.map(e => e.protocol)))
                    const visibleEntries = entries.slice(0, 3)
                    const hiddenCount = entries.length - visibleEntries.length

                    return (
                      <TableRow key={svc.id}>
                        <TableCell className="py-3 font-mono text-sm font-medium text-foreground">
                          {svc.name}
                        </TableCell>
                        <TableCell className="py-3">
                          {uniqueProtocols.length > 1 ? (
                            <Badge variant="outline" className="rounded border-warning/20 bg-warning/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-warning">
                              Mixed
                            </Badge>
                          ) : (
                            protocolBadge(uniqueProtocols[0])
                          )}
                        </TableCell>
                        <TableCell className="py-3">
                          <div className="flex flex-wrap items-center gap-1">
                            {visibleEntries.map((e, i) => (
                              <Badge key={i} variant="secondary" className="rounded px-1.5 py-0.5 font-mono text-[10px] text-muted-foreground">
                                {e.protocol}: {e.port}
                              </Badge>
                            ))}
                            {hiddenCount > 0 && (
                              <span className="text-[10px] text-muted-foreground">+{hiddenCount} more</span>
                            )}
                          </div>
                        </TableCell>
                        <TableCell className="py-3">
                          {svc.type === "system" ? (
                            <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                              System
                            </Badge>
                          ) : (
                            <Badge variant="outline" className="rounded border-warning/20 bg-warning/10 px-2 py-0.5 text-[10px] font-medium text-warning">
                              Custom
                            </Badge>
                          )}
                        </TableCell>
                        <TableCell className="py-3 text-right">
                          <div className="flex items-center justify-end gap-2">
                            {svc.type === "system" ? (
                              <span className="flex items-center justify-center p-1 text-muted-foreground/45" title="ระบบกำหนดไว้เริ่มต้น (แก้ไขไม่ได้)">
                                <Lock className="h-4 w-4" />
                              </span>
                            ) : (
                              <>
                                <Button
                                  variant="outline"
                                  size="icon-sm"
                                  onClick={() => openEditModal(svc)}
                                  className="cursor-pointer text-muted-foreground hover:text-foreground"
                                  title="แก้ไขวัตถุบริการ"
                                >
                                  <Edit className="h-4 w-4" />
                                </Button>
                                <Button
                                  variant="ghost"
                                  size="icon-sm"
                                  onClick={() => handleDelete(svc.id, svc.name)}
                                  className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                                  title="ลบวัตถุบริการ"
                                >
                                  <Trash2 className="h-4 w-4" />
                                </Button>
                              </>
                            )}
                          </div>
                        </TableCell>
                      </TableRow>
                    )
                  })
                )}
              </TableBody>
            </Table>
          </CardContent>
        </Card>

      {/* 3. Create / Edit Dialog */}
      <Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingObject ? "แก้ไขวัตถุบริการพอร์ต" : "สร้างวัตถุบริการพอร์ตใหม่"}
            </DrawerTitle>
          </DrawerHeader>

          {/* Form */}
          <div className="flex-1 overflow-y-auto p-4">
          <form onSubmit={handleSave} className="space-y-4 text-sm">
            {formError && (
              <Alert variant="destructive" className="px-3 py-2.5">
                <AlertCircle className="h-4 w-4" />
                <AlertDescription className="text-xs">{formError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Name */}
            <div className="space-y-1.5">
              <Label htmlFor="form-name" className="block text-xs font-medium text-muted-foreground">
                ชื่อบริการ (Service Name) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="form-name"
                type="text"
                required
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder="เช่น Custom_RDP, API_Port_Range"
                className="h-9 font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">ห้ามเว้นวรรค ใช้ได้เฉพาะอักษรภาษาอังกฤษ ตัวเลข และ _</p>
            </div>

            {/* Field: Protocol/Port entries */}
            <div className="space-y-1.5">
              <Label className="block text-xs font-medium text-muted-foreground">
                โปรโตคอล / พอร์ต (Protocol / Port) <span className="text-destructive">*</span>
              </Label>

              <div className="space-y-2">
                {formEntries.map((row, idx) => (
                  <div key={row.key} className="flex items-start gap-2">
                    <select
                      aria-label={`โปรโตคอล แถวที่ ${idx + 1}`}
                      value={row.protocol}
                      onChange={(e) => updateEntryProtocol(row.key, e.target.value as ServiceEntry["protocol"])}
                      className="h-9 w-[130px] shrink-0 cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                    >
                      <option value="TCP">TCP</option>
                      <option value="UDP">UDP</option>
                      <option value="TCP/UDP">TCP/UDP</option>
                      <option value="ICMP">ICMP</option>
                    </select>

                    <Input
                      aria-label={`พอร์ต แถวที่ ${idx + 1}`}
                      type="text"
                      disabled={row.protocol === "ICMP"}
                      value={row.port}
                      onChange={(e) => updateEntryPort(row.key, e.target.value)}
                      placeholder={row.protocol === "ICMP" ? "ไม่ต้องระบุพอร์ตสำหรับ ICMP" : "เช่น 3389 หรือ 8000-8010"}
                      className="h-9 flex-1 font-mono text-sm"
                    />

                    <Button
                      type="button"
                      variant="ghost"
                      size="icon-sm"
                      onClick={() => removeEntryRow(row.key)}
                      disabled={formEntries.length <= 1}
                      className="mt-0.5 shrink-0 cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive disabled:cursor-not-allowed disabled:opacity-40"
                      title="ลบรายการนี้"
                    >
                      <Trash2 className="h-4 w-4" />
                    </Button>
                  </div>
                ))}
              </div>

              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={addEntryRow}
                disabled={formEntries.length >= MAX_FORM_ENTRIES}
                className="cursor-pointer gap-1.5 text-xs disabled:cursor-not-allowed"
              >
                <Plus className="h-3.5 w-3.5" />
                เพิ่มรายการ
              </Button>

              <p className="mt-0.5 text-[10px] leading-relaxed text-muted-foreground">
                ระบุพอร์ตเป็นเลขเดี่ยว (เช่น 8080) หรือช่วงด้วยเครื่องหมายขีด (เช่น 8000-8010) ห้ามมีเว้นวรรค เลือก ICMP เพื่อไม่ต้องระบุพอร์ต
              </p>
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsModalOpen(false)}
                className="cursor-pointer text-muted-foreground"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                className="cursor-pointer px-6 font-semibold"
              >
                Save Service
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
