import { useState, useMemo, useRef, useEffect } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Sliders,
  Plus,
  Search,
  Edit,
  Trash2,
  Lock,
  AlertCircle,
  Info,
  Terminal,
  Loader2
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
import { Alert, AlertDescription } from "@/components/ui/alert"
import { type ServiceObject } from "@/data-mockup/mockData"
import { serviceObjectService } from "@/services/serviceObjectService"
import { useAlert } from "@/hooks/useAlert"

export default function Services() {
  const { alert, confirm } = useAlert()

  // --- State ---
  const [services, setServices] = useState<ServiceObject[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState("")
  const [protoFilter, setProtoFilter] = useState<"All" | "TCP" | "UDP" | "TCP/UDP" | "ICMP">("All")

  // Selected service object for the interactive nftables backend preview
  const [selectedPreviewId, setSelectedPreviewId] = useState<string>("svc-6") // Web_Testing_Pool as default

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

  // Get current active preview service details
  const previewService = useMemo(() => {
    return services.find(s => s.id === selectedPreviewId) || services[0]
  }, [services, selectedPreviewId])

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
        // If we deleted the preview item, reset preview selection
        if (selectedPreviewId === id) {
          setSelectedPreviewId("svc-1")
        }
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
        const newSvc = await serviceObjectService.create({
          name: formName,
          protocol: formProto,
          port: finalPort
        })
        setSelectedPreviewId(newSvc.id)
      }
      await loadServices(false)
      setIsModalOpen(false)
    } catch (err) {
      setFormError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    }
  }

  return (
    <div className="space-y-6">
      {/* 1. Header Area */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Sliders className="h-7 w-7 text-primary fill-primary/10" />
            Services (วัตถุบริการและพอร์ต)
          </h1>
          <p className="text-muted-foreground mt-1">
            ระบุโปรโตคอล TCP/UDP และช่วงพอร์ตเพื่อนำไปใช้อ้างอิงเป็นกลุ่มบริการใน Firewall Policy
          </p>
        </div>
        <div>
          <Button onClick={openCreateModal} className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1.5">
            <Plus className="h-4.5 w-4.5" />
            Create New Service
          </Button>
        </div>
      </div>

      {/* 2. Stats Dashboard Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">บริการทั้งหมด</div>
          <div className="mt-2 text-2xl font-bold text-foreground font-mono">{stats.total}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">ระบบ (System Default)</div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.systemCount}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">กำหนดเอง (Custom Sets)</div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.customCount}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">TCP / UDP ที่รองรับ</div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">
            {stats.tcpCount} <span className="text-xs text-muted-foreground">TCP</span> / {stats.udpCount} <span className="text-xs text-muted-foreground">UDP</span>
          </div>
        </Card>
      </div>

      {/* 3. Toolbar (Filters & Search) */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between bg-card/30 p-4 rounded-xl border border-border/60">
        <div className="flex flex-wrap items-center gap-2">
          {/* Protocol filters */}
          <div className="flex rounded-lg border border-border bg-card p-0.5 gap-0.5">
            {(["All", "TCP", "UDP", "TCP/UDP", "ICMP"] as const).map((proto) => (
              <button
                key={proto}
                onClick={() => setProtoFilter(proto)}
                className={`px-3 py-1 text-xs font-bold rounded-md transition ${protoFilter === proto
                  ? proto === "ICMP"
                    ? "bg-primary text-primary bg-primary"
                    : "bg-primary text-primary-foreground"
                  : "text-muted-foreground hover:text-foreground hover:bg-muted"
                  }`}
              >
                {proto}
              </button>
            ))}
          </div>
        </div>

        {/* Search */}
        <div className="relative w-full md:max-w-xs">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground pointer-events-none" />
          <Input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="ค้นหาชื่อบริการ, โปรโตคอล, พอร์ต..."
            className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9"
          />
        </div>
      </div>

      {/* 4. Table view */}
      <div className="grid gap-6 lg:grid-cols-3">
        {/* Left 2 Columns: Services Table */}
        <Card className="lg:col-span-2 bg-card/25 border border-border/50 overflow-hidden h-fit py-0">
          <Table>
            <TableHeader>
              <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
                <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[30%] font-semibold pl-4">Service Name</th>
                <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[15%] font-semibold">Protocol</th>
                <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[25%] font-semibold">Port Range / Details</th>
                <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[20%] font-semibold">Type</th>
                <TableHead className="p-3 w-[10%] text-right"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={5} className="p-12 text-center text-muted-foreground text-xs">
                    <div className="flex flex-col items-center justify-center gap-2 py-4">
                      <Loader2 className="h-6 w-6 animate-spin text-primary" />
                      <span>กำลังโหลดข้อมูล...</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : filteredServices.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="p-8 text-center text-muted-foreground text-xs">
                    ไม่พบข้อมูลวัตถุบริการที่ค้นหา
                  </TableCell>
                </TableRow>
              ) : (
                filteredServices.map((svc) => (
                  <TableRow
                    key={svc.id}
                    onClick={() => setSelectedPreviewId(svc.id)}
                    className={`border-b border-border/40 hover:bg-muted/15 cursor-pointer transition ${selectedPreviewId === svc.id ? "bg-muted/20 border-l-2 border-l-primary" : ""
                      }`}
                  >
                    <TableCell className="p-3 font-semibold text-foreground pl-4">
                      {svc.name}
                    </TableCell>
                    <TableCell className="p-3">
                      {svc.protocol === "TCP" && (
                        <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 font-mono text-[10px] px-1.5 py-0.5 rounded">
                          TCP
                        </Badge>
                      )}
                      {svc.protocol === "UDP" && (
                        <Badge variant="outline" className="bg-amber-500/10 text-amber-400 border-amber-500/20 font-mono text-[10px] px-1.5 py-0.5 rounded">
                          UDP
                        </Badge>
                      )}
                      {svc.protocol === "TCP/UDP" && (
                        <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 font-mono text-[10px] px-1.5 py-0.5 rounded">
                          TCP/UDP
                        </Badge>
                      )}
                      {svc.protocol === "ICMP" && (
                        <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 font-mono text-[10px] px-1.5 py-0.5 rounded">
                          ICMP
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="p-3 font-mono text-xs text-muted-foreground">{svc.port}</TableCell>
                    <TableCell className="p-3">
                      {svc.type === "system" ? (
                        <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[10px] px-2 py-0.5 rounded">
                          System
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[10px] px-2 py-0.5 rounded">
                          Custom
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="p-3 text-right" onClick={(e) => e.stopPropagation()}>
                      <div className="flex items-center justify-end gap-1">
                        {svc.type === "system" ? (
                          <span className="p-1 rounded text-muted-foreground/45 flex items-center justify-center" title="ระบบกำหนดไว้เริ่มต้น (แก้ไขไม่ได้)">
                            <Lock className="h-3.5 w-3.5" />
                          </span>
                        ) : (
                          <>
                            <Button
                              variant="ghost"
                              size="icon-xs"
                              onClick={() => openEditModal(svc)}
                              className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50"
                              title="แก้ไขวัตถุบริการ"
                            >
                              <Edit className="h-3.5 w-3.5" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-xs"
                              onClick={() => handleDelete(svc.id, svc.name)}
                              className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10"
                              title="ลบวัตถุบริการ"
                            >
                              <Trash2 className="h-3.5 w-3.5" />
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
        </Card>

        {/* Right 1 Column: Interactive Backend Integration Concept Preview */}
        <div className="space-y-4 lg:col-span-1">
          <Card className="bg-card border border-border p-5 rounded-xl flex flex-col gap-4 text-xs">
            <div className="flex items-center gap-2 border-b border-border/60 pb-3">
              <Terminal className="h-5 w-5 text-amber-500" />
              <div>
                <h3 className="font-bold text-foreground text-sm">nftables Named Set Preview</h3>
                <p className="text-[10px] text-muted-foreground">โครงสร้างคำสั่งจำลองบน Linux Kernel</p>
              </div>
            </div>

            {previewService ? (
              <div className="space-y-4">
                <div>
                  <p className="text-muted-foreground mb-1 leading-relaxed">
                    เมื่อคุณบันทึกวัตถุบริการ <strong>{previewService.name}</strong> เข้าไปในระบบ ตัวแปลคำสั่งหลังบ้าน (Python/Go daemon) จะเปลี่ยนวัตถุนั้นเป็น **Named Set** ในหน่วยความจำ `nftables` บน Raspberry Pi ดังนี้:
                  </p>
                </div>

                <div className="bg-muted p-3 rounded-lg border border-border/60 font-mono text-[11px] leading-relaxed text-primary overflow-x-auto whitespace-pre">
                  {previewService.protocol === "ICMP" ? (
                    <>
                      <span className="text-muted-foreground/60"># 1. ICMP ไม่ใช้พอร์ต แต่จะกรองผ่าน protocol โดยตรง</span>
                      {"\n"}
                      <span className="text-foreground font-semibold">nft add rule</span> ip filter FORWARD icmp type echo-request accept
                    </>
                  ) : (
                    <>
                      <span className="text-muted-foreground/60"># 1. สร้าง Named Set สำหรับเก็บ {previewService.protocol} พอร์ต</span>
                      {"\n"}
                      <span className="text-foreground font-semibold">nft add set</span> ip filter set_{previewService.name} &#123;
                      {"\n"}
                      {"  "}type inet_service;
                      {"\n"}
                      &#125;
                      {"\n\n"}
                      <span className="text-muted-foreground/60"># 2. เพิ่มพอร์ต ({previewService.port}) เข้าไปในเซ็ต</span>
                      {"\n"}
                      <span className="text-foreground font-semibold">nft add element</span> ip filter set_{previewService.name} &#123; {previewService.port} &#125;
                      {"\n\n"}
                      <span className="text-muted-foreground/60"># 3. อ้างอิงในกฎ Firewall โดยมีเครื่องหมาย @ นำหน้า</span>
                      {"\n"}
                      <span className="text-foreground font-semibold">nft add rule</span> ip filter FORWARD {previewService.protocol.toLowerCase().split("/")[0]} dport @set_{previewService.name} accept
                    </>
                  )}
                </div>

                <div className="flex items-start gap-2 bg-amber-500/5 border border-amber-500/10 rounded-lg p-3 text-[11px] text-muted-foreground leading-relaxed">
                  <Info className="h-4 w-4 text-amber-500 shrink-0 mt-0.5" />
                  <p>
                    <strong>การเลือกแถวในตาราง:</strong> คุณสามารถคลิกเลือกบริการใด ๆ ในตารางเพื่อแสดงการจำลองการบิลด์คำสั่ง Set ของบริการนั้นได้แบบเรียลไทม์
                  </p>
                </div>
              </div>
            ) : (
              <div className="text-center text-muted-foreground py-8">
                กรุณาคลิกเลือกวัตถุบริการเพื่อแสดงพรีวิวคำสั่ง
              </div>
            )}
          </Card>
        </div>
      </div>

      {/* 5. Create / Edit Dialog */}
      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent ref={dialogContentRef} className="md:max-w-[50vw] lg:max-w-[960px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingObject ? "แก้ไขวัตถุบริการพอร์ต" : "สร้างวัตถุบริการพอร์ตใหม่"}
            </DialogTitle>
          </DialogHeader>

          {/* Form */}
          <form onSubmit={handleSave} className="space-y-4 text-sm">
            {formError && (
              <Alert variant="destructive" className="border-red-500/20 bg-red-500/5 py-2.5 px-3">
                <AlertCircle className="h-4 w-4 text-red-400" />
                <AlertDescription className="text-red-400 text-xs">{formError}</AlertDescription>
              </Alert>
            )}

            {/* Field: Name */}
            <div className="space-y-1.5">
              <Label htmlFor="form-name" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                ชื่อบริการ (Service Name) <span className="text-red-500">*</span>
              </Label>
              <Input
                id="form-name"
                type="text"
                required
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder="เช่น Custom_RDP, API_Port_Range"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono"
              />
              <p className="text-[11px] text-muted-foreground italic">ห้ามเว้นวรรค ใช้ได้เฉพาะอักษรภาษาอังกฤษ ตัวเลข และ _</p>
            </div>

            {/* Field: Protocol & Port Row */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="form-proto" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
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
                  className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                >
                  <option value="TCP">TCP</option>
                  <option value="UDP">UDP</option>
                  <option value="TCP/UDP">TCP/UDP</option>
                  <option value="ICMP">ICMP</option>
                </select>
              </div>

              <div className="space-y-1.5">
                <Label htmlFor="form-port" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  หมายเลขพอร์ต (Destination Port) {formProto !== "ICMP" && <span className="text-red-500">*</span>}
                </Label>
                <Input
                  id="form-port"
                  type="text"
                  required={formProto !== "ICMP"}
                  disabled={formProto === "ICMP"}
                  value={formPort}
                  onChange={(e) => setFormPort(e.target.value)}
                  placeholder={formProto === "ICMP" ? "ไม่ต้องระบุพอร์ตสำหรับ ICMP" : "เช่น 3389 หรือ 8000-8010"}
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono"
                />
              </div>
            </div>

            {formProto !== "ICMP" && (
              <p className="text-[11px] text-muted-foreground italic leading-relaxed">
                ระบุเป็นพอร์ตเดี่ยว (เช่น 8080) หรือระบุเป็นช่วงด้วยเครื่องหมายขีด (เช่น 8000-8010) ห้ามมีเว้นวรรค
              </p>
            )}

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border/40">
              <Button
                type="button"
                variant="ghost"
                onClick={() => setIsModalOpen(false)}
                className="cursor-pointer text-muted-foreground hover:bg-muted/30"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold px-5"
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
