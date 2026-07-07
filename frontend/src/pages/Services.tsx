import { useState, useMemo, useRef, useEffect, type ReactNode } from "react"
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
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { type ServiceObject } from "@/data-mockup/mockData"
import { serviceObjectService } from "@/services/serviceObjectService"
import { useAlert } from "@/hooks/useAlert"
import { cn } from "@/lib/utils"

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
  const [formProto, setFormProto] = useState<"TCP" | "UDP" | "TCP/UDP" | "ICMP">("TCP")
  const [formPort, setFormPort] = useState("")
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

  const dialogContentRef = useRef<HTMLDivElement | null>(null)

  // --- Statistics ---
  const stats = useMemo(() => {
    const total = services.length
    const systemCount = services.filter(s => s.type === "system").length
    const customCount = services.filter(s => s.type === "custom").length
    const tcpCount = services.filter(s => s.protocol.includes("TCP")).length
    const udpCount = services.filter(s => s.protocol.includes("UDP")).length
    return { total, systemCount, customCount, tcpCount, udpCount }
  }, [services])

  // --- Filtered Services ---
  const filteredServices = useMemo(() => {
    return services.filter(svc => {
      const matchSearch =
        svc.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        svc.port.toLowerCase().includes(searchQuery.toLowerCase()) ||
        svc.protocol.toLowerCase().includes(searchQuery.toLowerCase())

      const matchProto =
        protoFilter === "All" ||
        (protoFilter === "TCP" && svc.protocol === "TCP") ||
        (protoFilter === "UDP" && svc.protocol === "UDP") ||
        (protoFilter === "TCP/UDP" && svc.protocol === "TCP/UDP") ||
        (protoFilter === "ICMP" && svc.protocol === "ICMP")

      return matchSearch && matchProto
    })
  }, [services, searchQuery, protoFilter])

  // --- CRUD Actions ---
  const openCreateModal = () => {
    setEditingObject(null)
    setFormName("")
    setFormProto("TCP")
    setFormPort("")
    setFormError("")
    setIsModalOpen(true)
  }

  const openEditModal = (svc: ServiceObject) => {
    if (svc.type === "system") return // Safety block
    setEditingObject(svc)
    setFormName(svc.name)
    setFormProto(svc.protocol)
    setFormPort(svc.port)
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

    // 3. Port validation
    let finalPort = formPort.trim()
    if (formProto === "ICMP") {
      finalPort = "-"
    } else {
      if (!finalPort) {
        setFormError("กรุณากรอกข้อมูลพอร์ต")
        return
      }

      // Check single port format (e.g. 80) or range format (e.g. 8000-8010)
      const singlePortRegex = /^\d+$/
      const rangePortRegex = /^(\d+)-(\d+)$/

      if (singlePortRegex.test(finalPort)) {
        const pNum = parseInt(finalPort, 10)
        if (pNum < 1 || pNum > 65535) {
          setFormError("หมายเลขพอร์ตต้องอยู่ระหว่างช่วง 1-65535")
          return
        }
      } else if (rangePortRegex.test(finalPort)) {
        const matches = finalPort.match(rangePortRegex)
        if (matches) {
          const start = parseInt(matches[1], 10)
          const end = parseInt(matches[2], 10)
          if (start < 1 || start > 65535 || end < 1 || end > 65535) {
            setFormError("หมายเลขพอร์ตต้นทางและปลายทางต้องอยู่ระหว่างช่วง 1-65535")
            return
          }
          if (start >= end) {
            setFormError("พอร์ตเริ่มต้นต้องมีค่าน้อยกว่าพอร์ตสิ้นสุดในการระบุช่วงพอร์ต")
            return
          }
        }
      } else {
        setFormError("รูปแบบพอร์ตไม่ถูกต้อง (ต้องระบุเป็นพอร์ตเดี่ยว เช่น 80 หรือแบบช่วง เช่น 8080-8085)")
        return
      }
    }

    try {
      if (editingObject) {
        // Edit
        await serviceObjectService.update(editingObject.id, {
          name: formName,
          protocol: formProto,
          port: finalPort
        })
      } else {
        // Create
        await serviceObjectService.create({
          name: formName,
          protocol: formProto,
          port: finalPort
        })
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
                ระบุโปรโตคอล TCP/UDP และช่วงพอร์ตเพื่อนำไปใช้อ้างอิงเป็นกลุ่มบริการใน Firewall Policy
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
                  <TableHead className="w-[30%] text-xs font-medium text-muted-foreground">Service Name</TableHead>
                  <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Protocol</TableHead>
                  <TableHead className="w-[25%] text-xs font-medium text-muted-foreground">Port Range / Details</TableHead>
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
                  filteredServices.map((svc) => (
                    <TableRow key={svc.id}>
                      <TableCell className="py-3 font-mono text-sm font-medium text-foreground">
                        {svc.name}
                      </TableCell>
                      <TableCell className="py-3">
                        {svc.protocol === "TCP" && (
                          <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-primary">
                            TCP
                          </Badge>
                        )}
                        {svc.protocol === "UDP" && (
                          <Badge variant="outline" className="rounded border-amber-500/20 bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-amber-500">
                            UDP
                          </Badge>
                        )}
                        {svc.protocol === "TCP/UDP" && (
                          <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-primary">
                            TCP/UDP
                          </Badge>
                        )}
                        {svc.protocol === "ICMP" && (
                          <Badge variant="secondary" className="rounded px-1.5 py-0.5 font-mono text-[10px] font-medium">
                            ICMP
                          </Badge>
                        )}
                      </TableCell>
                      <TableCell className="py-3 font-mono text-xs text-muted-foreground">{svc.port}</TableCell>
                      <TableCell className="py-3">
                        {svc.type === "system" ? (
                          <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                            System
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="rounded border-amber-500/20 bg-amber-500/10 px-2 py-0.5 text-[10px] font-medium text-amber-500">
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
                                className="cursor-pointer text-muted-foreground hover:bg-red-500/10 hover:text-red-500"
                                title="ลบวัตถุบริการ"
                              >
                                <Trash2 className="h-4 w-4" />
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

      {/* 3. Create / Edit Dialog */}
      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent ref={dialogContentRef} className="w-full max-w-[500px] gap-4 rounded-xl p-6">
          <DialogHeader className="border-b border-border/50 pb-3">
            <DialogTitle className="text-base font-semibold">
              {editingObject ? "แก้ไขวัตถุบริการพอร์ต" : "สร้างวัตถุบริการพอร์ตใหม่"}
            </DialogTitle>
          </DialogHeader>

          {/* Form */}
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

            {/* Field: Protocol & Port Row */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="form-proto" className="block text-xs font-medium text-muted-foreground">
                  โปรโตคอล (Protocol)
                </Label>
                <select
                  id="form-proto"
                  value={formProto}
                  onChange={(e) => {
                    const nextProto = e.target.value as "TCP" | "UDP" | "TCP/UDP" | "ICMP"
                    setFormProto(nextProto)
                    if (nextProto === "ICMP") {
                      setFormPort("-")
                    } else if (formPort === "-") {
                      setFormPort("")
                    }
                  }}
                  className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                >
                  <option value="TCP">TCP</option>
                  <option value="UDP">UDP</option>
                  <option value="TCP/UDP">TCP/UDP</option>
                  <option value="ICMP">ICMP</option>
                </select>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="form-port" className="block text-xs font-medium text-muted-foreground">
                  หมายเลขพอร์ต (Destination Port) {formProto !== "ICMP" && <span className="text-destructive">*</span>}
                </Label>
                <Input
                  id="form-port"
                  type="text"
                  required={formProto !== "ICMP"}
                  disabled={formProto === "ICMP"}
                  value={formPort}
                  onChange={(e) => setFormPort(e.target.value)}
                  placeholder={formProto === "ICMP" ? "ไม่ต้องระบุพอร์ตสำหรับ ICMP" : "เช่น 3389 หรือ 8000-8010"}
                  className="h-9 font-mono text-sm"
                />
              </div>
            </div>

            {formProto !== "ICMP" && (
              <p className="text-[10px] leading-relaxed text-muted-foreground">
                ระบุเป็นพอร์ตเดี่ยว (เช่น 8080) หรือระบุเป็นช่วงด้วยเครื่องหมายขีด (เช่น 8000-8010) ห้ามมีเว้นวรรค
              </p>
            )}

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
        </DialogContent>
      </Dialog>
    </div>
  )
}
