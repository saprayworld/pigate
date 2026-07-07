import { useState, useMemo, useRef, useEffect } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Flame,
  Plus,
  Search,
  GripVertical,
  Check,
  X,
  Edit,
  Trash2,
  RefreshCw,
  Lock,
  Info,
  Loader2,
  ListChecks,
  ShieldCheck,
  ShieldX,
  Ban
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { type PolicyRule, type AddressObject, type ServiceObject, type NetworkInterface } from "@/data-mockup/mockData"
import { policyService } from "@/services/policyService"
import { addressService } from "@/services/addressService"
import { serviceObjectService } from "@/services/serviceObjectService"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/hooks/useAlert"
import { cn } from "@/lib/utils"

// shadcn UI component imports
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
import { Alert, AlertTitle, AlertDescription, AlertAction } from "@/components/ui/alert"
import { Tabs, TabsList, TabsTrigger } from "@/components/ui/tabs"
import {
  Combobox,
  ComboboxChips,
  ComboboxChip,
  ComboboxChipsInput,
  ComboboxContent,
  ComboboxList,
  ComboboxItem,
  ComboboxEmpty,
  ComboboxValue,
  useComboboxAnchor,
} from "@/components/ui/combobox"

// Dnd-kit imports
import {
  DndContext,
  closestCenter,
  KeyboardSensor,
  PointerSensor,
  useSensor,
  useSensors
} from "@dnd-kit/core"
import type { DragEndEvent } from "@dnd-kit/core"
import {
  arrayMove,
  SortableContext,
  sortableKeyboardCoordinates,
  verticalListSortingStrategy,
  useSortable
} from "@dnd-kit/sortable"
import { CSS } from "@dnd-kit/utilities"
import { restrictToVerticalAxis } from "@dnd-kit/modifiers"

// Helper: Dashboard-style stat card (mirrors Dashboard's StatCard)
function StatCard({
  icon: Icon,
  title,
  value,
}: {
  icon: typeof Flame
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

// Helper: render an interface value as a readable label
function ifaceLabel(val: string | undefined, interfaces: NetworkInterface[]) {
  const v = val || "ALL"
  if (v === "ALL") return "ALL"
  const iface = interfaces.find((i) => i.name === v)
  return iface ? `${iface.alias || iface.name} (${iface.name})` : v
}

// Props for Sortable Row component
interface SortableRowProps {
  rule: PolicyRule
  index: number
  interfaces: NetworkInterface[]
  onEdit: (rule: PolicyRule) => void
  onDelete: (id: string) => void
  onToggleStatus: (id: string) => void
  onToggleLog: (id: string) => void
}

// Drag & Drop Row component
function SortableRow({ rule, index, interfaces, onEdit, onDelete, onToggleStatus, onToggleLog }: SortableRowProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: rule.id })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 10 : 1,
    opacity: isDragging ? 0.5 : 1
  }

  return (
    <TableRow
      ref={setNodeRef}
      style={style}
      className={cn(!rule.status && "opacity-60", isDragging && "bg-muted/50")}
    >
      {/* 1. Sequence & Drag Handle */}
      <TableCell className="py-3 font-mono text-xs text-muted-foreground">
        <div className="flex items-center gap-2">
          <button
            {...attributes}
            {...listeners}
            className="cursor-grab rounded p-1 text-muted-foreground/50 outline-none transition hover:bg-muted hover:text-foreground active:cursor-grabbing"
            title="ลากเพื่อจัดลำดับความสำคัญ"
          >
            <GripVertical className="h-4 w-4" />
          </button>
          <span>{index + 1}</span>
        </div>
      </TableCell>

      {/* 2. Name */}
      <TableCell className="py-3 text-sm font-medium text-foreground">{rule.name}</TableCell>

      {/* 2.1 In Interface */}
      <TableCell className="py-3">
        <Badge variant="secondary" className="rounded px-2 py-0.5 font-mono text-xs whitespace-nowrap">
          {ifaceLabel(rule.inInterface, interfaces)}
        </Badge>
      </TableCell>

      {/* 2.2 Out Interface */}
      <TableCell className="py-3">
        <Badge variant="secondary" className="rounded px-2 py-0.5 font-mono text-xs whitespace-nowrap">
          {ifaceLabel(rule.outInterface, interfaces)}
        </Badge>
      </TableCell>

      {/* 3. Source */}
      <TableCell className="py-3">
        <div className="flex flex-wrap gap-1">
          {rule.source.map((src, i) => (
            <Badge key={i} variant="secondary" className="rounded px-1.5 py-0.5 font-mono text-[11px]">
              {src}
            </Badge>
          ))}
        </div>
      </TableCell>

      {/* 4. Destination */}
      <TableCell className="py-3">
        <div className="flex flex-wrap gap-1">
          {rule.destination.map((dst, i) => (
            <Badge key={i} variant="secondary" className="rounded px-1.5 py-0.5 font-mono text-[11px]">
              {dst}
            </Badge>
          ))}
        </div>
      </TableCell>

      {/* 5. Service / Port */}
      <TableCell className="py-3">
        <div className="flex flex-wrap gap-1">
          {rule.service.map((svc, i) => (
            <Badge
              key={i}
              variant="outline"
              className="rounded border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[11px] font-medium text-primary"
            >
              {svc}
            </Badge>
          ))}
        </div>
      </TableCell>

      {/* 6. Action */}
      <TableCell className="py-3">
        <Badge
          variant="outline"
          className={cn(
            "rounded px-2 py-0.5 text-[10px] font-bold",
            rule.action === "ACCEPT"
              ? "border-primary/20 bg-primary/10 text-primary"
              : "border-red-500/20 bg-red-500/10 text-red-500"
          )}
        >
          {rule.action}
        </Badge>
      </TableCell>

      {/* 7. Log Switch */}
      <TableCell className="py-3">
        <Switch
          size="sm"
          checked={rule.log}
          onCheckedChange={() => onToggleLog(rule.id)}
        />
      </TableCell>

      {/* 8. Status Enable Switch */}
      <TableCell className="py-3">
        <div className="flex items-center gap-2">
          <Switch
            size="sm"
            checked={rule.status}
            onCheckedChange={() => onToggleStatus(rule.id)}
          />
          <span className={cn("text-xs", rule.status ? "font-semibold text-primary" : "text-muted-foreground")}>
            {rule.status ? "Enable" : "Disable"}
          </span>
        </div>
      </TableCell>

      {/* 9. Action Buttons */}
      <TableCell className="py-3 text-right">
        <div className="flex items-center justify-end gap-2">
          <Button
            variant="outline"
            size="icon-sm"
            onClick={() => onEdit(rule)}
            className="cursor-pointer text-muted-foreground hover:text-foreground"
            title="แก้ไขกฎ"
          >
            <Edit className="h-4 w-4" />
          </Button>
          <Button
            variant="ghost"
            size="icon-sm"
            onClick={() => onDelete(rule.id)}
            className="cursor-pointer text-muted-foreground hover:bg-red-500/10 hover:text-red-500"
            title="ลบกฎ"
          >
            <Trash2 className="h-4 w-4" />
          </Button>
        </div>
      </TableCell>
    </TableRow>
  )
}

export default function FirewallPolicy() {
  const { alert, confirm } = useAlert()
  // --- State for Policies, Addresses, Services, and Interfaces ---
  const [rules, setRules] = useState<PolicyRule[]>([])
  const [addressObjects, setAddressObjects] = useState<AddressObject[]>([])
  const [serviceObjects, setServiceObjects] = useState<ServiceObject[]>([])
  const [interfaces, setInterfaces] = useState<NetworkInterface[]>([])
  const [isLoading, setIsLoading] = useState(true)

  // --- Search and Filters State ---
  const [searchQuery, setSearchQuery] = useState<string>("")

  // --- Reload nftables Simulation ---
  const [isApplying, setIsApplying] = useState<boolean>(false)
  const [showApplySuccess, setShowApplySuccess] = useState<boolean>(false)
  const [applyProgress, setApplyProgress] = useState<string>("")

  // Fetch logic
  const loadPolicies = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const [policyData, addressData, serviceData, interfaceData] = await Promise.all([
        policyService.getAll(),
        addressService.getAll(),
        serviceObjectService.getAll(),
        interfaceService.getAll()
      ])
      setRules(policyData)
      setAddressObjects(addressData)
      setServiceObjects(serviceData)
      setInterfaces(interfaceData)
    } catch (err) {
      console.error(err)
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูลไฟร์วอลล์ได้: " + getErrorMessage(err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  // Generate options dynamically from current address and service objects
  const sourceOptions = useMemo(() => {
    const base = ["ALL"]
    addressObjects.forEach((a) => {
      if (!base.includes(a.name)) base.push(a.name)
    })
    return base
  }, [addressObjects])

  const destinationOptions = useMemo(() => {
    const base = ["ALL"]
    addressObjects.forEach((a) => {
      if (!base.includes(a.name)) base.push(a.name)
    })
    return base
  }, [addressObjects])

  const serviceOptions = useMemo(() => {
    const base = ["ALL"]
    serviceObjects.forEach((s) => {
      if (!base.includes(s.name)) base.push(s.name)
    })
    return base
  }, [serviceObjects])

  const interfaceOptions = useMemo(() => {
    const base = ["ALL"]
    interfaces.forEach((i) => {
      if (!base.includes(i.name)) base.push(i.name)
    })
    return base
  }, [interfaces])

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
        const [policyData, addressData, serviceData, interfaceData] = await Promise.all([
          policyService.getAll(),
          addressService.getAll(),
          serviceObjectService.getAll(),
          interfaceService.getAll()
        ])
        setRules(policyData)
        setAddressObjects(addressData)
        setServiceObjects(serviceData)
        setInterfaces(interfaceData)
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูลไฟร์วอลล์ได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

  const handleApplySettings = async () => {
    setIsApplying(true)
    setShowApplySuccess(false)
    setApplyProgress("กำลังตรวจสอบโครงสร้างของกฎไฟร์วอลล์...")

    try {
      await policyService.apply()
      setApplyProgress("กำลังรวบรวมคำสั่ง nftables สำหรับ Linux Kernel...")

      setTimeout(() => {
        setApplyProgress("กำลังโหลดตารางและโซนความปลอดภัยเข้าไปที่ Netfilter...")
        setTimeout(() => {
          setIsApplying(false)
          setShowApplySuccess(true)
          // Hide success message automatically after 5 seconds
          setTimeout(() => {
            setShowApplySuccess(false)
          }, 5000)
        }, 600)
      }, 600)
    } catch (err) {
      setIsApplying(false)
      await alert("ข้อผิดพลาด", "ไม่สามารถใช้การตั้งค่าได้: " + getErrorMessage(err))
    }
  }

  // --- Modal Forms State ---
  const [isModalOpen, setIsModalOpen] = useState<boolean>(false)
  const [editingRule, setEditingRule] = useState<PolicyRule | null>(null)

  // Combobox anchors for positioning the popup relative to the chips container
  const sourceAnchor = useComboboxAnchor()
  const destAnchor = useComboboxAnchor()
  const serviceAnchor = useComboboxAnchor()

  // Ref for DialogContent to container-portal the combobox popups
  const dialogContentRef = useRef<HTMLDivElement | null>(null)

  // Form Fields
  const [formName, setFormName] = useState<string>("")
  const [formInInterface, setFormInInterface] = useState<string>("ALL")
  const [formOutInterface, setFormOutInterface] = useState<string>("ALL")
  const [formSource, setFormSource] = useState<string[]>([])
  const [formDest, setFormDest] = useState<string[]>([])
  const [formService, setFormService] = useState<string[]>([])
  const [formAction, setFormAction] = useState<"ACCEPT" | "DROP">("ACCEPT")
  const [formLog, setFormLog] = useState<boolean>(false)
  const [formStatus, setFormStatus] = useState<boolean>(true)

  // --- Dialog control triggers ---
  const openCreateModal = () => {
    setEditingRule(null)
    setFormName("")
    setFormInInterface("ALL")
    setFormOutInterface("ALL")
    setFormSource([])
    setFormDest([])
    setFormService([])
    setFormAction("ACCEPT")
    setFormLog(false)
    setFormStatus(true)
    setIsModalOpen(true)
  }

  const openEditModal = (rule: PolicyRule) => {
    setEditingRule(rule)
    setFormName(rule.name)
    setFormInInterface(rule.inInterface || "ALL")
    setFormOutInterface(rule.outInterface || "ALL")
    setFormSource([...rule.source])
    setFormDest([...rule.destination])
    setFormService([...rule.service])
    setFormAction(rule.action)
    setFormLog(rule.log)
    setFormStatus(rule.status)
    setIsModalOpen(true)
  }

  const handleSaveForm = async (e: React.FormEvent) => {
    e.preventDefault()

    if (!formName.trim()) return

    const parsedSvcs = formService.length ? formService : ["ALL"]

    try {
      if (editingRule) {
        // Edit mode
        await policyService.update(editingRule.id, {
          name: formName,
          inInterface: formInInterface,
          outInterface: formOutInterface,
          source: formSource.length ? formSource : ["ALL"],
          destination: formDest.length ? formDest : ["ALL"],
          service: parsedSvcs,
          action: formAction,
          log: formLog,
          status: formStatus
        })
      } else {
        // Create mode
        await policyService.create({
          name: formName,
          inInterface: formInInterface,
          outInterface: formOutInterface,
          source: formSource.length ? formSource : ["ALL"],
          destination: formDest.length ? formDest : ["ALL"],
          service: parsedSvcs,
          action: formAction,
          log: formLog,
          status: formStatus
        })
      }
      await loadPolicies(false)
      setIsModalOpen(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถบันทึกกฎไฟร์วอลล์ได้: " + getErrorMessage(err))
    }
  }

  // --- Action Toggles inside Row ---
  const handleToggleLog = async (id: string) => {
    try {
      await policyService.toggleLog(id)
      await loadPolicies(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปลี่ยนค่าสถานะ Log ได้: " + getErrorMessage(err))
    }
  }

  const handleToggleStatus = async (id: string) => {
    try {
      await policyService.toggleStatus(id)
      await loadPolicies(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปลี่ยนสถานะใช้งานกฎได้: " + getErrorMessage(err))
    }
  }

  const handleDeleteRule = async (id: string) => {
    if (await confirm("ยืนยันการลบ", "คุณแน่ใจหรือไม่ที่จะลบนโยบายความปลอดภัยนี้?")) {
      try {
        await policyService.delete(id)
        await loadPolicies(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบกฎได้: " + getErrorMessage(err))
      }
    }
  }

  // --- Dnd-kit Configuration ---
  const sensors = useSensors(
    useSensor(PointerSensor, {
      activationConstraint: {
        distance: 5 // drag only triggers after pointer moved 5px
      }
    }),
    useSensor(KeyboardSensor, {
      coordinateGetter: sortableKeyboardCoordinates
    })
  )

  const handleDragEnd = async (event: DragEndEvent) => {
    const { active, over } = event

    if (over && active.id !== over.id) {
      const oldIndex = rules.findIndex((item) => item.id === active.id)
      const newIndex = rules.findIndex((item) => item.id === over.id)
      const newRules = arrayMove(rules, oldIndex, newIndex)

      // Optimistically update local state to render immediately
      setRules(newRules)

      try {
        await policyService.saveAll(newRules)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถบันทึกการจัดลำดับกฎใหม่ได้: " + getErrorMessage(err))
        await loadPolicies(false)
      }
    }
  }

  // --- Filtered Rules ---
  const filteredRules = useMemo(() => {
    return rules.filter((rule) => {
      const query = searchQuery.trim().toLowerCase()
      if (!query) return true

      return (
        rule.name.toLowerCase().includes(query) ||
        rule.source.some((s) => s.toLowerCase().includes(query)) ||
        rule.destination.some((d) => d.toLowerCase().includes(query)) ||
        rule.service.some((s) => s.toLowerCase().includes(query)) ||
        rule.action.toLowerCase().includes(query)
      )
    })
  }, [rules, searchQuery])

  // --- Statistics ---
  const stats = useMemo(() => {
    const total = rules.length
    const active = rules.filter((r) => r.status).length
    const disabled = rules.filter((r) => !r.status).length
    const deny = rules.filter((r) => r.action === "DROP").length
    return { total, active, disabled, deny }
  }, [rules])

  return (
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={ListChecks} title="Total Policies" value={stats.total} />
        <StatCard icon={ShieldCheck} title="Active Rules" value={stats.active} />
        <StatCard icon={Ban} title="Disabled Rules" value={stats.disabled} />
        <StatCard icon={ShieldX} title="Deny (DROP) Rules" value={stats.deny} />
      </div>

      {/* 2. Reload States Notification */}
      {isApplying && (
        <Alert className="animate-pulse border-primary/20 bg-primary/5">
          <RefreshCw className="h-4 w-4 animate-spin text-primary" />
          <AlertTitle className="font-semibold text-primary">กำลังนำนโยบายไปปรับใช้...</AlertTitle>
          <AlertDescription className="text-xs text-muted-foreground">{applyProgress}</AlertDescription>
        </Alert>
      )}

      {showApplySuccess && (
        <Alert className="animate-fade-in border-primary/20 bg-primary/5">
          <Check className="h-4 w-4 text-primary" />
          <AlertTitle className="font-semibold text-primary">ปรับใช้สำเร็จ! (nftables reloaded)</AlertTitle>
          <AlertDescription className="text-xs text-muted-foreground">
            บิลด์กฎและนำการตั้งค่านโยบายไฟร์วอลล์ทั้งหมดขึ้นระบบเคอร์เนลเรียบร้อยแล้ว
          </AlertDescription>
          <AlertAction>
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => setShowApplySuccess(false)}
              className="h-5 w-5 text-muted-foreground hover:text-foreground"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </AlertAction>
        </Alert>
      )}

      {/* 3. Policies Table Card */}
      <Card>
        <CardHeader className="flex flex-col gap-4 space-y-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <Flame className="h-4 w-4 text-muted-foreground" />
              Firewall Policy
              <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                {stats.total}
              </Badge>
            </CardTitle>
            <CardDescription className="text-xs">
              จัดเรียงลำดับความสำคัญของกฎ (Security Policy Rule Chains) ลากและวางเพื่อเปลี่ยนความสำคัญ
            </CardDescription>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            {/* Search Input */}
            <div className="relative w-full sm:w-[220px]">
              <Search className="pointer-events-none absolute top-2 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="ค้นหาชื่อกฎ, ไอพี หรือพอร์ต..."
                className="h-8 pl-8 text-xs"
              />
            </div>
            <Button
              variant="outline"
              size="sm"
              onClick={handleApplySettings}
              disabled={isApplying}
              className="cursor-pointer gap-2"
            >
              {isApplying ? (
                <RefreshCw className="h-4 w-4 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4" />
              )}
              {isApplying ? "Applying..." : "Apply Settings"}
            </Button>
            <Button size="sm" onClick={openCreateModal} className="cursor-pointer gap-1.5 font-semibold">
              <Plus className="h-4 w-4" />
              Create New Policy
            </Button>
          </div>
        </CardHeader>

        <CardContent>
          <div className="w-full overflow-x-auto">
            <Table>
              <TableHeader>
                <TableRow className="hover:bg-transparent">
                  <TableHead className="w-[5%] text-xs font-medium text-muted-foreground">Seq.</TableHead>
                  <TableHead className="w-[12%] text-xs font-medium text-muted-foreground">Name</TableHead>
                  <TableHead className="w-[8%] text-xs font-medium text-muted-foreground">In</TableHead>
                  <TableHead className="w-[8%] text-xs font-medium text-muted-foreground">Out</TableHead>
                  <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Source</TableHead>
                  <TableHead className="w-[15%] text-xs font-medium text-muted-foreground">Destination</TableHead>
                  <TableHead className="w-[14%] text-xs font-medium text-muted-foreground">Service / Port</TableHead>
                  <TableHead className="w-[8%] text-xs font-medium text-muted-foreground">Action</TableHead>
                  <TableHead className="w-[6%] text-xs font-medium text-muted-foreground">Log</TableHead>
                  <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Status</TableHead>
                  <TableHead className="w-[8%] text-right text-xs font-medium text-muted-foreground"></TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {isLoading ? (
                  <TableRow>
                    <TableCell colSpan={11} className="py-12 text-center text-xs text-muted-foreground">
                      <div className="flex flex-col items-center justify-center gap-2 py-4">
                        <Loader2 className="h-6 w-6 animate-spin text-primary" />
                        <span>กำลังโหลดข้อมูล...</span>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : filteredRules.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={11} className="py-8 text-center text-xs text-muted-foreground">
                      ไม่พบนโยบายไฟร์วอลล์ที่ค้นหา
                    </TableCell>
                  </TableRow>
                ) : (
                  <DndContext
                    sensors={sensors}
                    collisionDetection={closestCenter}
                    onDragEnd={handleDragEnd}
                    modifiers={[restrictToVerticalAxis]}
                  >
                    <SortableContext items={filteredRules.map((r) => r.id)} strategy={verticalListSortingStrategy}>
                      {filteredRules.map((rule, idx) => (
                        <SortableRow
                          key={rule.id}
                          rule={rule}
                          index={idx}
                          interfaces={interfaces}
                          onEdit={openEditModal}
                          onDelete={handleDeleteRule}
                          onToggleStatus={handleToggleStatus}
                          onToggleLog={handleToggleLog}
                        />
                      ))}
                    </SortableContext>
                  </DndContext>
                )}

                {/* Default Implicit Deny Row */}
                <TableRow className="border-t border-border bg-muted/30 text-muted-foreground hover:bg-muted/30">
                  <TableCell className="py-3 font-mono text-xs">
                    <div className="flex items-center gap-2 pl-1">
                      <Lock className="h-3.5 w-3.5 text-muted-foreground/60" />
                      -
                    </div>
                  </TableCell>
                  <TableCell className="py-3 text-sm font-medium italic">Implicit Deny</TableCell>
                  <TableCell className="py-3 italic">-</TableCell>
                  <TableCell className="py-3 italic">-</TableCell>
                  <TableCell className="py-3 italic">ALL</TableCell>
                  <TableCell className="py-3 italic">ALL</TableCell>
                  <TableCell className="py-3 italic">ALL</TableCell>
                  <TableCell className="py-3">
                    <Badge variant="outline" className="rounded border-red-500/20 bg-red-500/10 px-2 py-0.5 text-[10px] font-bold text-red-500">
                      DROP
                    </Badge>
                  </TableCell>
                  <TableCell className="py-3">-</TableCell>
                  <TableCell className="py-3">
                    <Badge variant="secondary" className="rounded px-1.5 py-0.5 text-[10px]">
                      System
                    </Badge>
                  </TableCell>
                  <TableCell></TableCell>
                </TableRow>
              </TableBody>
            </Table>
          </div>
        </CardContent>
      </Card>

      {/* 4. Info note */}
      <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          <strong className="text-foreground">ข้อแนะนำเกี่ยวกับการจัดเรียงลำดับนโยบาย:</strong>{" "}
          การตั้งค่ากฎ Firewall จะเรียงลำดับความสำคัญจากบนลงล่าง (First-match wins) กฎที่อยู่ด้านบนสุดจะถูกตรวจสอบและประมวลผลก่อนเสมอ
          หากกฎด้านบนตรงตามเงื่อนไข จะไม่มีการประมวลผลกฎข้อถัดไปลงมา
          หลังจากเพิ่ม แก้ไข หรือจัดเรียงกฎใหม่เรียบร้อยแล้ว จำเป็นต้องกดปุ่ม{" "}
          <strong className="font-semibold text-primary">"Apply Settings"</strong> เพื่ออัปโหลดนโยบายเข้าสู่ระบบ Kernel `nftables` จริง
        </span>
      </div>

      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent ref={dialogContentRef} className="w-full gap-4 rounded-xl p-6 md:max-w-[85vw] lg:max-w-[960px]">
          <DialogHeader className="border-b border-border/50 pb-3">
            <DialogTitle className="text-base font-semibold">
              {editingRule ? "แก้ไขนโยบายความปลอดภัย" : "สร้างนโยบายความปลอดภัยใหม่"}
            </DialogTitle>
          </DialogHeader>

          {/* Form */}
          <form onSubmit={handleSaveForm} className="space-y-4 text-sm">
            {/* Row 1: Name & Service/Port */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="form-name" className="block text-xs font-medium text-muted-foreground">
                  ชื่อนโยบาย (Name) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="form-name"
                  type="text"
                  required
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="เช่น Allow-HTTP-Out, Block-MalIPs"
                  className="h-9 text-sm"
                />
              </div>
              <div className="space-y-1.5">
                <Label className="block text-xs font-medium text-muted-foreground">
                  บริการ / พอร์ต (Service/Port) <span className="text-destructive">*</span>
                </Label>
                <Combobox
                  multiple={true}
                  required
                  value={formService}
                  onValueChange={(val) => setFormService(val as string[])}
                  items={serviceOptions}
                >
                  <ComboboxChips ref={serviceAnchor} className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1 focus-within:border-primary focus-within:ring-1 focus-within:ring-primary">
                    <ComboboxValue>
                      {(values: string[]) => (
                        <>
                          {values.map((val) => (
                            <ComboboxChip key={val} className="text-xs">
                              {val}
                            </ComboboxChip>
                          ))}
                          <ComboboxChipsInput placeholder={values.length === 0 ? "เลือกบริการ / พอร์ต..." : ""} className="h-7 border-none bg-transparent text-xs outline-none focus:ring-0" />
                        </>
                      )}
                    </ComboboxValue>
                  </ComboboxChips>
                  <ComboboxContent anchor={serviceAnchor} className="w-[var(--anchor-width)] overflow-hidden rounded-lg border border-border bg-popover">
                    <ComboboxEmpty className="p-2 text-center text-xs text-muted-foreground">ไม่พบข้อมูล</ComboboxEmpty>
                    <ComboboxList className="max-h-48 overflow-y-auto p-1">
                      {(opt: string) => (
                        <ComboboxItem key={opt} value={opt} className="cursor-pointer text-xs hover:bg-muted/80">
                          {opt}
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
              </div>
            </div>

            {/* Row 1.5: In Interface & Out Interface */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="form-in-interface" className="block text-xs font-medium text-muted-foreground">
                  การ์ดขาเข้า (In Interface) <span className="text-destructive">*</span>
                </Label>
                <select
                  id="form-in-interface"
                  value={formInInterface}
                  onChange={(e) => setFormInInterface(e.target.value)}
                  className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                >
                  {interfaceOptions.map((opt) => (
                    <option key={opt} value={opt}>
                      {opt === "ALL" ? "ALL (ทุกอินเตอร์เฟส)" : ifaceLabel(opt, interfaces)}
                    </option>
                  ))}
                </select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="form-out-interface" className="block text-xs font-medium text-muted-foreground">
                  การ์ดขาออก (Out Interface) <span className="text-destructive">*</span>
                </Label>
                <select
                  id="form-out-interface"
                  value={formOutInterface}
                  onChange={(e) => setFormOutInterface(e.target.value)}
                  className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                >
                  {interfaceOptions.map((opt) => (
                    <option key={opt} value={opt}>
                      {opt === "ALL" ? "ALL (ทุกอินเตอร์เฟส)" : ifaceLabel(opt, interfaces)}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            {/* Row 2: Source & Destination with Multiple Selection Combobox */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="block text-xs font-medium text-muted-foreground">
                  ต้นทาง (Source IP/Network) <span className="text-destructive">*</span>
                </Label>
                <Combobox
                  multiple={true}
                  required
                  value={formSource}
                  onValueChange={(val) => setFormSource(val as string[])}
                  items={sourceOptions}
                >
                  <ComboboxChips ref={sourceAnchor} className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1 focus-within:border-primary focus-within:ring-1 focus-within:ring-primary">
                    <ComboboxValue>
                      {(values: string[]) => (
                        <>
                          {values.map((val) => (
                            <ComboboxChip key={val} className="text-xs">
                              {val}
                            </ComboboxChip>
                          ))}
                          <ComboboxChipsInput placeholder={values.length === 0 ? "เลือกต้นทาง..." : ""} className="h-7 border-none bg-transparent text-xs outline-none focus:ring-0" />
                        </>
                      )}
                    </ComboboxValue>
                  </ComboboxChips>
                  <ComboboxContent anchor={sourceAnchor} className="w-[var(--anchor-width)] overflow-hidden rounded-lg border border-border bg-popover">
                    <ComboboxEmpty className="p-2 text-center text-xs text-muted-foreground">ไม่พบข้อมูล</ComboboxEmpty>
                    <ComboboxList className="max-h-48 overflow-y-auto p-1">
                      {(opt: string) => (
                        <ComboboxItem key={opt} value={opt} className="cursor-pointer text-xs hover:bg-muted/80">
                          {opt}
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
              </div>

              <div className="space-y-1.5">
                <Label className="block text-xs font-medium text-muted-foreground">
                  ปลายทาง (Destination IP/Network) <span className="text-destructive">*</span>
                </Label>
                <Combobox
                  multiple={true}
                  required
                  value={formDest}
                  onValueChange={(val) => setFormDest(val as string[])}
                  items={destinationOptions}
                >
                  <ComboboxChips ref={destAnchor} className="flex min-h-9 flex-wrap items-center gap-1.5 rounded-md border border-input bg-background px-2 py-1 focus-within:border-primary focus-within:ring-1 focus-within:ring-primary">
                    <ComboboxValue>
                      {(values: string[]) => (
                        <>
                          {values.map((val) => (
                            <ComboboxChip key={val} className="text-xs">
                              {val}
                            </ComboboxChip>
                          ))}
                          <ComboboxChipsInput placeholder={values.length === 0 ? "เลือกปลายทาง..." : ""} className="h-7 border-none bg-transparent text-xs outline-none focus:ring-0" />
                        </>
                      )}
                    </ComboboxValue>
                  </ComboboxChips>
                  <ComboboxContent anchor={destAnchor} className="w-[var(--anchor-width)] overflow-hidden rounded-lg border border-border bg-popover">
                    <ComboboxEmpty className="p-2 text-center text-xs text-muted-foreground">ไม่พบข้อมูล</ComboboxEmpty>
                    <ComboboxList className="max-h-48 overflow-y-auto p-1">
                      {(opt: string) => (
                        <ComboboxItem key={opt} value={opt} className="cursor-pointer text-xs hover:bg-muted/80">
                          {opt}
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
              </div>
            </div>

            {/* Row 3: Action & Switches */}
            <div className="grid items-end gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="block text-xs font-medium text-muted-foreground">
                  การจัดการ (Action)
                </Label>
                <Tabs value={formAction} onValueChange={(val) => setFormAction(val as "ACCEPT" | "DROP")} className="w-full">
                  <TabsList className="grid w-full grid-cols-2">
                    <TabsTrigger value="ACCEPT" className="font-bold data-active:text-primary dark:data-active:text-primary">ACCEPT</TabsTrigger>
                    <TabsTrigger value="DROP" className="font-bold data-active:text-red-500 dark:data-active:text-red-400">DROP</TabsTrigger>
                  </TabsList>
                </Tabs>
              </div>
              <div className="flex items-center gap-6 pb-2.5">
                <div className="flex items-center gap-2">
                  <Switch
                    id="form-log"
                    checked={formLog}
                    onCheckedChange={setFormLog}
                  />
                  <Label htmlFor="form-log" className="cursor-pointer text-xs font-medium text-muted-foreground">บันทึกล็อก (Log)</Label>
                </div>

                <div className="flex items-center gap-2">
                  <Switch
                    id="form-status"
                    checked={formStatus}
                    onCheckedChange={setFormStatus}
                  />
                  <Label htmlFor="form-status" className="cursor-pointer text-xs font-medium text-muted-foreground">เปิดใช้งาน (Active)</Label>
                </div>
              </div>
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
                Save Policy
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
