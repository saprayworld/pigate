import { useState, useMemo, useRef, useEffect } from "react"
import {
  BookOpen,
  Plus,
  Search,
  Edit,
  Trash2,
  AlertCircle,
  Network,
  Globe,
  Layers,
  Trash,
  Loader2,
  Lock
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
import { type AddressObject } from "@/data-mockup/mockData"
import { addressService } from "@/services/addressService"

export default function Addresses() {
  // --- State ---
  const [addresses, setAddresses] = useState<AddressObject[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState("")
  const [selectedTypeFilter, setSelectedTypeFilter] = useState<"all" | "subnet" | "range" | "fqdn">("all")

  // Selection state for checkboxes
  const [selectedIds, setSelectedIds] = useState<string[]>([])

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingObject, setEditingObject] = useState<AddressObject | null>(null)

  // Form fields
  const [formName, setFormName] = useState("")
  const [formType, setFormType] = useState<"subnet" | "range" | "fqdn">("subnet")
  const [formValue, setFormValue] = useState("")
  const [formError, setFormError] = useState("")

  // Fetch logic
  const loadAddresses = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const data = await addressService.getAll()
      setAddresses(data)
    } catch (err: any) {
      console.error(err)
      alert("ไม่สามารถโหลดข้อมูลที่อยู่ไอพีได้: " + (err.message || err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    loadAddresses()
  }, [])

  const dialogContentRef = useRef<HTMLDivElement | null>(null)

  // --- Statistics ---
  const stats = useMemo(() => {
    const total = addresses.length
    const subnets = addresses.filter(a => a.type === "subnet").length
    const ranges = addresses.filter(a => a.type === "range").length
    const fqdns = addresses.filter(a => a.type === "fqdn").length
    return { total, subnets, ranges, fqdns }
  }, [addresses])

  // --- Filtered Addresses ---
  const filteredAddresses = useMemo(() => {
    return addresses.filter(addr => {
      const matchSearch =
        addr.name.toLowerCase().includes(searchQuery.toLowerCase()) ||
        addr.value.toLowerCase().includes(searchQuery.toLowerCase()) ||
        addr.refPolicies.some(p => p.toLowerCase().includes(searchQuery.toLowerCase()))

      const matchType = selectedTypeFilter === "all" || addr.type === selectedTypeFilter

      return matchSearch && matchType
    })
  }, [addresses, searchQuery, selectedTypeFilter])

  // --- Checkbox Actions ---
  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedIds(filteredAddresses.map(a => a.id))
    } else {
      setSelectedIds([])
    }
  }

  const handleSelectRow = (id: string, checked: boolean) => {
    if (checked) {
      setSelectedIds(prev => [...prev, id])
    } else {
      setSelectedIds(prev => prev.filter(item => item !== id))
    }
  }

  const isAllSelected = useMemo(() => {
    if (filteredAddresses.length === 0) return false
    return filteredAddresses.every(a => selectedIds.includes(a.id))
  }, [filteredAddresses, selectedIds])

  // --- CRUD Actions ---
  const openCreateModal = () => {
    setEditingObject(null)
    setFormName("")
    setFormType("subnet")
    setFormValue("")
    setFormError("")
    setIsModalOpen(true)
  }

  const openEditModal = (obj: AddressObject) => {
    setEditingObject(obj)
    setFormName(obj.name)
    setFormType(obj.type)
    setFormValue(obj.value)
    setFormError("")
    setIsModalOpen(true)
  }

  const handleDelete = async (id: string, name: string) => {
    const obj = addresses.find(a => a.id === id)
    if (obj && obj.system) {
      alert(`ไม่สามารถลบวัตถุระบบ "${name}" ได้`)
      return
    }
    if (obj && obj.refPolicies.length > 0) {
      alert(`ไม่สามารถลบ "${name}" ได้ เนื่องจากถูกอ้างอิงอยู่ในนโยบายไฟร์วอลล์: ${obj.refPolicies.join(", ")}`)
      return
    }

    if (confirm(`คุณต้องการลบวัตถุที่อยู่ "${name}" ใช่หรือไม่?`)) {
      try {
        await addressService.delete(id)
        setSelectedIds(prev => prev.filter(item => item !== id))
        await loadAddresses(false)
      } catch (err: any) {
        alert("ไม่สามารถลบข้อมูลได้: " + (err.message || err))
      }
    }
  }

  const handleBulkDelete = async () => {
    // Check if any selected items are system objects
    const systemObjects = addresses.filter(a => selectedIds.includes(a.id) && a.system)
    if (systemObjects.length > 0) {
      const names = systemObjects.map(o => o.name).join(", ")
      alert(`ไม่สามารถลบวัตถุระบบต่อไปนี้ได้: ${names}`)
      return
    }

    // Check if any selected items are in use
    const usedObjects = addresses.filter(a => selectedIds.includes(a.id) && a.refPolicies.length > 0)
    if (usedObjects.length > 0) {
      const names = usedObjects.map(o => o.name).join(", ")
      alert(`ไม่สามารถลบวัตถุต่อไปนี้ได้เนื่องจากถูกอ้างอิงอยู่: ${names}`)
      return
    }

    if (confirm(`คุณต้องการลบวัตถุที่เลือกจำนวน ${selectedIds.length} รายการใช่หรือไม่?`)) {
      try {
        await addressService.deleteMultiple(selectedIds)
        setSelectedIds([])
        await loadAddresses(false)
      } catch (err: any) {
        alert("ไม่สามารถลบข้อมูลได้: " + (err.message || err))
      }
    }
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    // 1. Validation Name format
    const nameRegex = /^[a-zA-Z0-9_]+$/
    if (!nameRegex.test(formName)) {
      setFormError("ชื่อวัตถุต้องใช้ภาษาอังกฤษ ตัวเลข หรือเครื่องหมาย _ เท่านั้น (ห้ามเว้นวรรค)")
      return
    }

    // 2. Duplicate Name check
    const isDuplicate = addresses.some(
      a => a.name.toLowerCase() === formName.toLowerCase() && (!editingObject || a.id !== editingObject.id)
    )
    if (isDuplicate) {
      setFormError(`มีชื่อวัตถุ "${formName}" อยู่ในระบบแล้ว`)
      return
    }

    // 3. Simple Value validation based on type
    if (!formValue.trim()) {
      setFormError("กรุณากรอกข้อมูลค่าที่อยู่ไอพี")
      return
    }

    try {
      if (editingObject) {
        // Edit
        await addressService.update(editingObject.id, {
          name: formName,
          type: formType,
          value: formValue
        })
      } else {
        // Create
        await addressService.create({
          name: formName,
          type: formType,
          value: formValue
        })
      }
      await loadAddresses(false)
      setIsModalOpen(false)
    } catch (err: any) {
      setFormError(err.message || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    }
  }

  return (
    <div className="space-y-6">
      {/* 1. Header Area */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <BookOpen className="h-7 w-7 text-primary fill-primary/10" />
            Addresses (วัตถุที่อยู่ไอพี)
          </h1>
          <p className="text-muted-foreground mt-1">
            กำหนดค่า IP Address, Subnet หรือ FQDN เพื่อนำไปอ้างอิงใช้ในนโยบายไฟร์วอลล์ซ้ำได้สะดวก
          </p>
        </div>
        <div>
          <Button onClick={openCreateModal} className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1.5">
            <Plus className="h-4.5 w-4.5" />
            Create New Address
          </Button>
        </div>
      </div>

      {/* 2. Stats Dashboard Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">ที่อยู่ไอพีทั้งหมด</div>
          <div className="mt-2 text-2xl font-bold text-foreground font-mono">{stats.total}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Network className="h-3.5 w-3.5 text-primary" /> Subnets (IP/Netmask)
          </div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.subnets}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Layers className="h-3.5 w-3.5 text-amber-400" /> IP Ranges
          </div>
          <div className="mt-2 text-2xl font-bold text-amber-400 font-mono">{stats.ranges}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Globe className="h-3.5 w-3.5 text-cyan-400" /> FQDNs (Domains)
          </div>
          <div className="mt-2 text-2xl font-bold text-cyan-400 font-mono">{stats.fqdns}</div>
        </Card>
      </div>

      {/* 3. Toolbar (Filters & Search) */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between bg-card/30 p-4 rounded-xl border border-border/60">
        <div className="flex flex-wrap items-center gap-2">
          {/* Type filters */}
          <div className="flex rounded-lg border border-border bg-card p-0.5 gap-0.5">
            <button
              onClick={() => setSelectedTypeFilter("all")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition ${selectedTypeFilter === "all"
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              All
            </button>
            <button
              onClick={() => setSelectedTypeFilter("subnet")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition ${selectedTypeFilter === "subnet"
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              Subnet
            </button>
            <button
              onClick={() => setSelectedTypeFilter("range")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition ${selectedTypeFilter === "range"
                ? "bg-amber-500 text-neutral-950"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              IP Range
            </button>
            <button
              onClick={() => setSelectedTypeFilter("fqdn")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition ${selectedTypeFilter === "fqdn"
                ? "bg-cyan-500 text-neutral-950"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              FQDN
            </button>
          </div>

          {/* Bulk Action */}
          {selectedIds.length > 0 && (
            <Button
              variant="destructive"
              size="sm"
              onClick={handleBulkDelete}
              className="font-bold gap-1 h-8 px-3 ml-2"
            >
              <Trash className="h-3.5 w-3.5" />
              Delete Selected ({selectedIds.length})
            </Button>
          )}
        </div>

        {/* Search */}
        <div className="relative w-full md:max-w-xs">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground pointer-events-none" />
          <Input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="ค้นหาชื่อ หรือที่อยู่ไอพี..."
            className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9"
          />
        </div>
      </div>

      {/* 4. Table view */}
      <Card className="bg-card/25 border border-border/50 overflow-hidden">
        <Table>
          <TableHeader>
            <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
              <TableHead className="p-3 w-[5%]">
                <input
                  type="checkbox"
                  checked={isAllSelected}
                  onChange={(e) => handleSelectAll(e.target.checked)}
                  className="rounded border-input bg-background text-primary focus:ring-primary h-3.5 w-3.5 cursor-pointer accent-primary"
                />
              </TableHead>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[25%] font-semibold">Name</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[15%] font-semibold">Type</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[35%] font-semibold">Details / Value</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[12%] font-semibold">Ref. Policies</th>
              <TableHead className="p-3 w-[8%] text-right"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={6} className="p-12 text-center text-muted-foreground text-xs">
                  <div className="flex flex-col items-center justify-center gap-2 py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-primary" />
                    <span>กำลังโหลดข้อมูล...</span>
                  </div>
                </TableCell>
              </TableRow>
            ) : filteredAddresses.length === 0 ? (
              <TableRow>
                <TableCell colSpan={6} className="p-8 text-center text-muted-foreground text-xs">
                  ไม่พบวัตถุที่อยู่ไอพีตามที่ค้นหา
                </TableCell>
              </TableRow>
            ) : (
              filteredAddresses.map((addr) => (
                <TableRow key={addr.id} className="border-b border-border/40 hover:bg-muted/15">
                  <TableCell className="p-3">
                    <input
                      type="checkbox"
                      checked={selectedIds.includes(addr.id)}
                      onChange={(e) => handleSelectRow(addr.id, e.target.checked)}
                      className="rounded border-input bg-background text-primary focus:ring-primary h-3.5 w-3.5 cursor-pointer accent-primary"
                    />
                  </TableCell>
                  <TableCell className="p-3 font-semibold text-foreground">{addr.name}</TableCell>
                  <TableCell className="p-3">
                    {addr.type === "subnet" && (
                      <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[10px] px-2 py-0.5 rounded">
                        Subnet
                      </Badge>
                    )}
                    {addr.type === "range" && (
                      <Badge variant="outline" className="bg-amber-500/10 text-amber-400 border-amber-500/20 text-[10px] px-2 py-0.5 rounded">
                        IP Range
                      </Badge>
                    )}
                    {addr.type === "fqdn" && (
                      <Badge variant="outline" className="bg-cyan-500/10 text-cyan-400 border-cyan-500/20 text-[10px] px-2 py-0.5 rounded">
                        FQDN
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="p-3 font-mono text-xs text-muted-foreground">{addr.value}</TableCell>
                  <TableCell className="p-3">
                    {addr.refPolicies.length === 0 ? (
                      <span className="text-xs text-muted-foreground/45 italic">None</span>
                    ) : (
                      <div className="flex flex-wrap gap-1">
                        {addr.refPolicies.map((p, i) => (
                          <Badge
                            key={i}
                            variant="secondary"
                            className="bg-muted text-muted-foreground font-mono text-[9px] px-1.5 py-0.2 rounded"
                          >
                            {p}
                          </Badge>
                        ))}
                      </div>
                    )}
                  </TableCell>
                  <TableCell className="p-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      {addr.system ? (
                        <span className="p-1 rounded text-muted-foreground/45 flex items-center justify-center" title="ระบบกำหนดไว้เริ่มต้น (แก้ไขไม่ได้)">
                          <Lock className="h-3.5 w-3.5" />
                        </span>
                      ) : (
                        <>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={() => openEditModal(addr)}
                            className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50"
                            title="แก้ไขวัตถุ"
                          >
                            <Edit className="h-3.5 w-3.5" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon-xs"
                            onClick={() => handleDelete(addr.id, addr.name)}
                            className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10"
                            title="ลบวัตถุ"
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

      {/* 5. Warning / Help Box */}
      <Alert className="border-dashed border-border bg-card/10">
        <AlertCircle className="h-4 w-4 text-muted-foreground" />
        <AlertTitle className="font-bold text-foreground mb-0.5">การนำไปใช้งาน:</AlertTitle>
        <AlertDescription className="text-xs text-muted-foreground leading-relaxed">
          วัตถุที่สร้างขึ้นในหน้านี้จะปรากฏให้เลือกในหน้าจอ <span className="font-semibold text-primary">"Firewall Policy"</span> ในช่อง ต้นทาง (Source) และ ปลายทาง (Destination)
          การแก้ไขค่าที่อยู่ไอพีของวัตถุใด ๆ จะมีผลปรับเปลี่ยนการบังคับใช้กฎไฟร์วอลล์ทั้งหมดที่เลือกใช้วัตถุนั้นทันทีโดยอัตโนมัติ
        </AlertDescription>
      </Alert>

      {/* 6. Create / Edit Dialog */}
      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent ref={dialogContentRef} className="max-w-[500px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingObject ? "แก้ไขวัตถุที่อยู่ไอพี" : "สร้างวัตถุที่อยู่ไอพีใหม่"}
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
                ชื่อวัตถุ (Name) <span className="text-red-500">*</span>
              </Label>
              <Input
                id="form-name"
                type="text"
                required
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder="เช่น Web_Server_Subnet, Blocked_IPs"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono"
              />
              <p className="text-[11px] text-muted-foreground italic">ห้ามเว้นวรรค ใช้ได้เฉพาะอักษรภาษาอังกฤษ ตัวเลข และ _</p>
            </div>

            {/* Field: Type */}
            <div className="space-y-1.5">
              <Label htmlFor="form-type" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                ประเภทที่อยู่ (Type)
              </Label>
              <select
                id="form-type"
                value={formType}
                onChange={(e) => {
                  setFormType(e.target.value as "subnet" | "range" | "fqdn")
                  setFormValue("") // Reset value to avoid invalid placeholder confusion
                }}
                className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
              >
                <option value="subnet">Subnet (IP/Netmask)</option>
                <option value="range">IP Range (ช่วงไอพี)</option>
                <option value="fqdn">FQDN (ชื่อโดเมน)</option>
              </select>
            </div>

            {/* Field: Value */}
            <div className="space-y-1.5">
              <Label htmlFor="form-value" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                ค่าที่อยู่ไอพี (Value) <span className="text-red-500">*</span>
              </Label>
              <Input
                id="form-value"
                type="text"
                required
                value={formValue}
                onChange={(e) => setFormValue(e.target.value)}
                placeholder={
                  formType === "subnet"
                    ? "เช่น 192.168.1.0/24 หรือ 10.0.0.10/32"
                    : formType === "range"
                      ? "เช่น 192.168.1.100 - 192.168.1.200"
                      : "เช่น google.com หรือ dev.pigate.local"
                }
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono"
              />
              <p className="text-[11px] text-muted-foreground italic">
                {formType === "subnet" && "ระบุเป็น CIDR Format เช่น /24 หรือ /32 สำหรับไอพีเดี่ยว"}
                {formType === "range" && "ระบุไอพีเริ่มต้น และไอพีสิ้นสุด คั่นกลางด้วยเครื่องหมาย -"}
                {formType === "fqdn" && "ระบุชื่อโดเมน FQDN ที่ต้องการกรอง เช่น updates.raspberrypi.org"}
              </p>
            </div>

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
                Save Object
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
