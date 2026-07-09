import { useState, useMemo, useEffect } from "react"
import { getErrorMessage } from "@/lib/errors"
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
  Lock,
  Info
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
import { type AddressObject } from "@/data-mockup/mockData"
import { addressService } from "@/services/addressService"
import { useAlert } from "@/hooks/useAlert"
import { cn, isValidCidr, isValidIpRange } from "@/lib/utils"

// Helper: Dashboard-style stat card (mirrors Dashboard's StatCard)
function StatCard({
  icon: Icon,
  title,
  value,
}: {
  icon: typeof BookOpen
  title: string
  value: number
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
        <p className="text-2xl font-bold tracking-tight text-foreground">{value}</p>
      </CardContent>
    </Card>
  )
}

export default function Addresses() {
  const { alert, confirm } = useAlert()

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

  // Fetch logic (used by refresh actions / after save-delete; showLoading=false for those)
  const loadAddresses = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const data = await addressService.getAll()
      setAddresses(data)
    } catch (err) {
      console.error(err)
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูลที่อยู่ไอพีได้: " + getErrorMessage(err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
        const data = await addressService.getAll()
        setAddresses(data)
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูลที่อยู่ไอพีได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

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
      await alert("การดำเนินการล้มเหลว", `ไม่สามารถลบวัตถุระบบ "${name}" ได้`)
      return
    }
    if (obj && obj.refPolicies.length > 0) {
      await alert("การดำเนินการล้มเหลว", `ไม่สามารถลบ "${name}" ได้ เนื่องจากถูกอ้างอิงอยู่ในนโยบายไฟร์วอลล์: ${obj.refPolicies.join(", ")}`)
      return
    }

    if (await confirm("ยืนยันการลบ", `คุณต้องการลบวัตถุที่อยู่ "${name}" ใช่หรือไม่?`)) {
      try {
        await addressService.delete(id)
        setSelectedIds(prev => prev.filter(item => item !== id))
        await loadAddresses(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบข้อมูลได้: " + getErrorMessage(err))
      }
    }
  }

  const handleBulkDelete = async () => {
    // Check if any selected items are system objects
    const systemObjects = addresses.filter(a => selectedIds.includes(a.id) && a.system)
    if (systemObjects.length > 0) {
      const names = systemObjects.map(o => o.name).join(", ")
      await alert("การดำเนินการล้มเหลว", `ไม่สามารถลบวัตถุระบบต่อไปนี้ได้: ${names}`)
      return
    }

    // Check if any selected items are in use
    const usedObjects = addresses.filter(a => selectedIds.includes(a.id) && a.refPolicies.length > 0)
    if (usedObjects.length > 0) {
      const names = usedObjects.map(o => o.name).join(", ")
      await alert("การดำเนินการล้มเหลว", `ไม่สามารถลบวัตถุต่อไปนี้ได้เนื่องจากถูกอ้างอิงอยู่: ${names}`)
      return
    }

    if (await confirm("ยืนยันการลบ", `คุณต้องการลบวัตถุที่เลือกจำนวน ${selectedIds.length} รายการใช่หรือไม่?`)) {
      try {
        await addressService.deleteMultiple(selectedIds)
        setSelectedIds([])
        await loadAddresses(false)
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

    // 3. Strict Value validation based on type
    const trimmedVal = formValue.trim()
    if (!trimmedVal) {
      setFormError("กรุณากรอกข้อมูลค่าที่อยู่ไอพี")
      return
    }
    if (formType === "subnet") {
      if (!isValidCidr(trimmedVal)) {
        setFormError("รูปแบบ Subnet ไม่ถูกต้อง (เช่น 192.168.1.0/24 หรือ 10.0.0.1/32) และค่า Octet ต้องอยู่ในช่วง 0-255")
        return
      }
    } else if (formType === "range") {
      if (!isValidIpRange(trimmedVal)) {
        setFormError("รูปแบบ IP Range ไม่ถูกต้อง (เช่น 192.168.1.100 - 192.168.1.200) และค่าเริ่มต้นต้องน้อยกว่าหรือเท่ากับสิ้นสุด (0-255)")
        return
      }
    } else if (formType === "fqdn") {
      const fqdnRegex = /^(?:[a-zA-Z0-9](?:[a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?\.)+[a-zA-Z]{2,6}$/
      if (!fqdnRegex.test(trimmedVal)) {
        setFormError("รูปแบบ FQDN ไม่ถูกต้อง (เช่น google.com หรือ updates.raspberrypi.org)")
        return
      }
    }

    try {
      if (editingObject) {
        // Edit
        await addressService.update(editingObject.id, {
          name: formName,
          type: formType,
          value: trimmedVal
        })
      } else {
        // Create
        await addressService.create({
          name: formName,
          type: formType,
          value: trimmedVal
        })
      }
      await loadAddresses(false)
      setIsModalOpen(false)
    } catch (err) {
      setFormError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    }
  }

  const typeFilters: { value: typeof selectedTypeFilter; label: string }[] = [
    { value: "all", label: "All" },
    { value: "subnet", label: "Subnet" },
    { value: "range", label: "IP Range" },
    { value: "fqdn", label: "FQDN" },
  ]

  return (
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={BookOpen} title="Total Objects" value={stats.total} />
        <StatCard icon={Network} title="Subnets" value={stats.subnets} />
        <StatCard icon={Layers} title="IP Ranges" value={stats.ranges} />
        <StatCard icon={Globe} title="FQDNs" value={stats.fqdns} />
      </div>

      {/* 2. Address objects table */}
      <Card>
        <CardHeader className="flex flex-col gap-4 space-y-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <BookOpen className="h-4 w-4 text-muted-foreground" />
              Address Objects
              <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                {stats.total}
              </Badge>
            </CardTitle>
            <CardDescription className="text-xs">
              กำหนดค่า IP Address, Subnet หรือ FQDN เพื่อนำไปอ้างอิงใช้ในนโยบายไฟร์วอลล์ซ้ำได้สะดวก
            </CardDescription>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            {/* Search */}
            <div className="relative w-full sm:w-[220px]">
              <Search className="pointer-events-none absolute top-2 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="ค้นหาชื่อ หรือที่อยู่ไอพี..."
                className="h-8 pl-8 text-xs"
              />
            </div>
            <Button size="sm" onClick={openCreateModal} className="cursor-pointer gap-1.5 font-semibold">
              <Plus className="h-4 w-4" />
              Create New Address
            </Button>
          </div>
        </CardHeader>

        <CardContent className="space-y-4">
          {/* Toolbar (Filters & bulk action) */}
          <div className="flex flex-wrap items-center gap-2.5">
            {/* Type filters */}
            <div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">
              {typeFilters.map((f) => (
                <button
                  key={f.value}
                  onClick={() => setSelectedTypeFilter(f.value)}
                  className={cn(
                    "cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition",
                    selectedTypeFilter === f.value
                      ? "bg-primary text-primary-foreground"
                      : "text-muted-foreground hover:bg-muted hover:text-foreground"
                  )}
                >
                  {f.label}
                </button>
              ))}
            </div>

            {/* Bulk Action */}
            {selectedIds.length > 0 && (
              <Button
                variant="destructive"
                size="sm"
                onClick={handleBulkDelete}
                className="cursor-pointer gap-1.5"
              >
                <Trash className="h-3.5 w-3.5" />
                Delete Selected ({selectedIds.length})
              </Button>
            )}
          </div>

          {/* Table view */}
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[5%]">
                  <input
                    type="checkbox"
                    checked={isAllSelected}
                    onChange={(e) => handleSelectAll(e.target.checked)}
                    className="h-4 w-4 cursor-pointer rounded border-input bg-background accent-primary"
                  />
                </TableHead>
                <TableHead className="w-[25%] text-xs font-medium text-muted-foreground">Name</TableHead>
                <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Type</TableHead>
                <TableHead className="w-[35%] text-xs font-medium text-muted-foreground">Details / Value</TableHead>
                <TableHead className="w-[12%] text-xs font-medium text-muted-foreground">Ref. Policies</TableHead>
                <TableHead className="w-[8%] text-right text-xs font-medium text-muted-foreground"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-12 text-center text-xs text-muted-foreground">
                    <div className="flex flex-col items-center justify-center gap-2 py-4">
                      <Loader2 className="h-6 w-6 animate-spin text-primary" />
                      <span>กำลังโหลดข้อมูล...</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : filteredAddresses.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={6} className="py-8 text-center text-xs text-muted-foreground">
                    ไม่พบวัตถุที่อยู่ไอพีตามที่ค้นหา
                  </TableCell>
                </TableRow>
              ) : (
                filteredAddresses.map((addr) => (
                  <TableRow key={addr.id}>
                    <TableCell className="py-3">
                      <input
                        type="checkbox"
                        checked={selectedIds.includes(addr.id)}
                        onChange={(e) => handleSelectRow(addr.id, e.target.checked)}
                        className="h-4 w-4 cursor-pointer rounded border-input bg-background accent-primary"
                      />
                    </TableCell>
                    <TableCell className="py-3 font-mono text-sm font-medium text-foreground">{addr.name}</TableCell>
                    <TableCell className="py-3">
                      {addr.type === "subnet" && (
                        <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                          Subnet
                        </Badge>
                      )}
                      {addr.type === "range" && (
                        <Badge variant="outline" className="rounded border-warning/20 bg-warning/10 px-2 py-0.5 text-[10px] font-medium text-warning">
                          IP Range
                        </Badge>
                      )}
                      {addr.type === "fqdn" && (
                        <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                          FQDN
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="py-3 font-mono text-xs text-muted-foreground">{addr.value}</TableCell>
                    <TableCell className="py-3">
                      {addr.refPolicies.length === 0 ? (
                        <span className="text-xs italic text-muted-foreground/45">None</span>
                      ) : (
                        <div className="flex flex-wrap gap-1">
                          {addr.refPolicies.map((p, i) => (
                            <Badge key={i} variant="secondary" className="rounded px-1.5 py-0.5 font-mono text-[10px]">
                              {p}
                            </Badge>
                          ))}
                        </div>
                      )}
                    </TableCell>
                    <TableCell className="py-3 text-right">
                      <div className="flex items-center justify-end gap-2">
                        {addr.system ? (
                          <span className="flex items-center justify-center p-1 text-muted-foreground/45" title="ระบบกำหนดไว้เริ่มต้น (แก้ไขไม่ได้)">
                            <Lock className="h-4 w-4" />
                          </span>
                        ) : (
                          <>
                            <Button
                              variant="outline"
                              size="icon-sm"
                              onClick={() => openEditModal(addr)}
                              className="cursor-pointer text-muted-foreground hover:text-foreground"
                              title="แก้ไขวัตถุ"
                            >
                              <Edit className="h-4 w-4" />
                            </Button>
                            <Button
                              variant="ghost"
                              size="icon-sm"
                              onClick={() => handleDelete(addr.id, addr.name)}
                              className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                              title="ลบวัตถุ"
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

      {/* 3. Info note */}
      <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          <strong className="text-foreground">การนำไปใช้งาน:</strong>{" "}
          วัตถุที่สร้างขึ้นในหน้านี้จะปรากฏให้เลือกในหน้าจอ <strong className="font-semibold text-primary">"Firewall Policy"</strong> ในช่อง ต้นทาง (Source) และ ปลายทาง (Destination)
          การแก้ไขค่าที่อยู่ไอพีของวัตถุใด ๆ จะมีผลปรับเปลี่ยนการบังคับใช้กฎไฟร์วอลล์ทั้งหมดที่เลือกใช้วัตถุนั้นทันทีโดยอัตโนมัติ
        </span>
      </div>

      {/* 4. Create / Edit Drawer */}
      <Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingObject ? "แก้ไขวัตถุที่อยู่ไอพี" : "สร้างวัตถุที่อยู่ไอพีใหม่"}
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
                ชื่อวัตถุ (Name) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="form-name"
                type="text"
                required
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                placeholder="เช่น Web_Server_Subnet, Blocked_IPs"
                className="h-9 font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">ห้ามเว้นวรรค ใช้ได้เฉพาะอักษรภาษาอังกฤษ ตัวเลข และ _</p>
            </div>

            {/* Field: Type */}
            <div className="space-y-1.5">
              <Label htmlFor="form-type" className="block text-xs font-medium text-muted-foreground">
                ประเภทที่อยู่ (Type)
              </Label>
              <select
                id="form-type"
                value={formType}
                onChange={(e) => {
                  setFormType(e.target.value as "subnet" | "range" | "fqdn")
                  setFormValue("") // Reset value to avoid invalid placeholder confusion
                }}
                className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
              >
                <option value="subnet">Subnet (IP/Netmask)</option>
                <option value="range">IP Range (ช่วงไอพี)</option>
                <option value="fqdn">FQDN (ชื่อโดเมน)</option>
              </select>
            </div>

            {/* Field: Value */}
            <div className="space-y-1.5">
              <Label htmlFor="form-value" className="block text-xs font-medium text-muted-foreground">
                ค่าที่อยู่ไอพี (Value) <span className="text-destructive">*</span>
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
                className="h-9 font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                {formType === "subnet" && "ระบุเป็น CIDR Format เช่น /24 หรือ /32 สำหรับไอพีเดี่ยว"}
                {formType === "range" && "ระบุไอพีเริ่มต้น และไอพีสิ้นสุด คั่นกลางด้วยเครื่องหมาย -"}
                {formType === "fqdn" && "ระบุชื่อโดเมน FQDN ที่ต้องการกรอง เช่น updates.raspberrypi.org"}
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
                Save Object
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
