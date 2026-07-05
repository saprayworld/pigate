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
  AlertCircle,
  Loader2
} from "lucide-react"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { type PolicyRule, type AddressObject, type ServiceObject, type NetworkInterface } from "@/data-mockup/mockData"
import { policyService } from "@/services/policyService"
import { addressService } from "@/services/addressService"
import { serviceObjectService } from "@/services/serviceObjectService"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/hooks/useAlert"

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
      className={`border-b border-border/40 hover:bg-muted/15 ${!rule.status ? "bg-muted/5 opacity-55" : ""
        } ${isDragging ? "bg-muted/30" : ""}`}
    >
      {/* 1. Sequence & Drag Handle */}
      <TableCell className="p-3 text-xs text-muted-foreground font-mono">
        <div className="flex items-center gap-2">
          <button
            {...attributes}
            {...listeners}
            className="cursor-grab active:cursor-grabbing p-1 rounded text-muted-foreground/50 hover:text-foreground hover:bg-muted/50 transition outline-none"
            title="ลากเพื่อจัดลำดับความสำคัญ"
          >
            <GripVertical className="h-4 w-4" />
          </button>
          <span>{index + 1}</span>
        </div>
      </TableCell>

      {/* 2. Name */}
      <TableCell className="p-3 font-semibold text-foreground">{rule.name}</TableCell>

      {/* 2.1 In Interface */}
      <TableCell className="p-3">
        <Badge
          variant="outline"
          className="bg-neutral-850/10 text-neutral-500 border-neutral-700/25 dark:text-neutral-400 font-mono text-[10.5px] px-1.5 py-0.5 rounded whitespace-nowrap"
        >
          {(() => {
            const val = rule.inInterface || "ALL";
            if (val === "ALL") return "ALL";
            const iface = interfaces.find((i) => i.name === val);
            return iface ? `${iface.alias || iface.name} (${iface.name})` : val;
          })()}
        </Badge>
      </TableCell>

      {/* 2.2 Out Interface */}
      <TableCell className="p-3">
        <Badge
          variant="outline"
          className="bg-neutral-850/10 text-neutral-500 border-neutral-700/25 dark:text-neutral-400 font-mono text-[10.5px] px-1.5 py-0.5 rounded whitespace-nowrap"
        >
          {(() => {
            const val = rule.outInterface || "ALL";
            if (val === "ALL") return "ALL";
            const iface = interfaces.find((i) => i.name === val);
            return iface ? `${iface.alias || iface.name} (${iface.name})` : val;
          })()}
        </Badge>
      </TableCell>

      {/* 3. Source */}
      <TableCell className="p-3">
        <div className="flex flex-wrap gap-1">
          {rule.source.map((src, i) => (
            <Badge
              key={i}
              variant="outline"
              className="bg-neutral-800/10 text-neutral-600 border-neutral-700/35 dark:text-neutral-300 font-mono text-[10.5px] px-1.5 py-0.5 rounded"
            >
              {src}
            </Badge>
          ))}
        </div>
      </TableCell>

      {/* 4. Destination */}
      <TableCell className="p-3">
        <div className="flex flex-wrap gap-1">
          {rule.destination.map((dst, i) => (
            <Badge
              key={i}
              variant="outline"
              className="bg-neutral-800/10 text-neutral-600 border-neutral-700/35 dark:text-neutral-300 font-mono text-[10.5px] px-1.5 py-0.5 rounded"
            >
              {dst}
            </Badge>
          ))}
        </div>
      </TableCell>

      {/* 5. Service / Port */}
      <TableCell className="p-3">
        <div className="flex flex-wrap gap-1">
          {rule.service.map((svc, i) => (
            <Badge
              key={i}
              variant="outline"
              className="bg-indigo-500/5 text-indigo-400 border-indigo-550/15 font-mono text-[10.5px] px-1.5 py-0.5 rounded"
            >
              {svc}
            </Badge>
          ))}
        </div>
      </TableCell>

      {/* 6. Action */}
      <TableCell className="p-3">
        <Badge
          variant={rule.action === "DROP" ? "destructive" : "outline"}
          className={`font-bold text-[10px] px-2 py-0.5 rounded ${rule.action === "ACCEPT"
            ? "bg-primary/10 text-primary border border-primary/20"
            : "bg-red-500/10 text-red-500 border-red-500/20"
            }`}
        >
          {rule.action}
        </Badge>
      </TableCell>

      {/* 7. Log Switch */}
      <TableCell className="p-3">
        <Switch
          size="sm"
          checked={rule.log}
          onCheckedChange={() => onToggleLog(rule.id)}
        />
      </TableCell>

      {/* 8. Status Enable Switch */}
      <TableCell className="p-3">
        <div className="flex items-center gap-2">
          <Switch
            size="sm"
            checked={rule.status}
            onCheckedChange={() => onToggleStatus(rule.id)}
          />
          <span className={`text-xs ${rule.status ? "text-primary font-semibold" : "text-muted-foreground"}`}>
            {rule.status ? "Enable" : "Disable"}
          </span>
        </div>
      </TableCell>

      {/* 9. Action Buttons */}
      <TableCell className="p-3 text-right">
        <div className="flex items-center justify-end gap-1">
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => onEdit(rule)}
            className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50"
            title="แก้ไขกฎ"
          >
            <Edit className="h-3.5 w-3.5" />
          </Button>
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => onDelete(rule.id)}
            className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10"
            title="ลบกฎ"
          >
            <Trash2 className="h-3.5 w-3.5" />
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

  // Count active / disabled
  const activeCount = useMemo(() => rules.filter((r) => r.status).length, [rules])
  const disabledCount = useMemo(() => rules.filter((r) => !r.status).length, [rules])

  return (
    <div className="space-y-6">
      {/* 1. Header Overview */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Flame className="h-7 w-7 text-primary fill-primary/10" />
            Firewall Policy (กฎไฟร์วอลล์)
          </h1>
          <p className="text-muted-foreground mt-1">
            จัดเรียงลำดับความสำคัญของกฎ (Security Policy Rule Chains) ลากและวางเพื่อเปลี่ยนความสำคัญ
          </p>
        </div>

        <div className="flex items-center gap-3">
          <Button
            onClick={handleApplySettings}
            disabled={isApplying}
            className="cursor-pointer bg-primary hover:bg-primary/90 text-primary-foreground font-bold gap-2 px-4"
          >
            {isApplying ? (
              <>
                <RefreshCw className="h-4 w-4 animate-spin" />
                Applying...
              </>
            ) : (
              <>
                <RefreshCw className="h-4 w-4" />
                Apply Settings
              </>
            )}
          </Button>
        </div>
      </div>

      {/* 2. Reload States Notification */}
      {isApplying && (
        <Alert className="animate-pulse border-blue-500/20 bg-blue-500/5">
          <RefreshCw className="h-4 w-4 text-blue-400 animate-spin" />
          <AlertTitle className="font-semibold text-blue-400">กำลังนำนโยบายไปปรับใช้...</AlertTitle>
          <AlertDescription className="text-muted-foreground text-xs">{applyProgress}</AlertDescription>
        </Alert>
      )}

      {showApplySuccess && (
        <Alert className="animate-fade-in border-primary/20 bg-primary/5">
          <Check className="h-4.5 w-4.5 text-primary" />
          <AlertTitle className="font-semibold text-primary">ปรับใช้สำเร็จ! (nftables reloaded)</AlertTitle>
          <AlertDescription className="text-muted-foreground text-xs">
            บิลด์กฎและนำการตั้งค่านโยบายไฟร์วอลล์ทั้งหมดขึ้นระบบเคอร์เนลเรียบร้อยแล้ว
          </AlertDescription>
          <AlertAction>
            <Button
              variant="ghost"
              size="icon-xs"
              onClick={() => setShowApplySuccess(false)}
              className="text-muted-foreground hover:text-foreground h-5 w-5"
            >
              <X className="h-3.5 w-3.5" />
            </Button>
          </AlertAction>
        </Alert>
      )}

      {/* 3. Toolbar Row */}
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between bg-card/30 p-4 rounded-xl border border-border/60">
        <div className="flex items-center gap-2">
          <Button onClick={openCreateModal} className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/80 gap-1.5 sm:h-9">
            <Plus className="h-4 w-4" />
            Create New Policy
          </Button>
          <div className="text-xs text-muted-foreground px-2 hidden md:block">
            สถานะ: <span className="text-primary font-semibold">{activeCount} เปิดใช้งาน</span> |{" "}
            <span className="text-neutral-400 font-semibold">{disabledCount} ปิดใช้งาน</span>
          </div>
        </div>

        {/* Search Input */}
        <div className="relative w-full sm:max-w-xs">
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground pointer-events-none" />
          <Input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="ค้นหาชื่อกฎ, ไอพี หรือพอร์ต..."
            className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9"
          />
        </div>
      </div>

      {/* 4. Policies Table Container */}
      <Card className="bg-card/25 border border-border/50 overflow-hidden py-0">
        <div className="overflow-x-auto w-full">
          <Table>
            <TableHeader>
              <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[5%] h-auto">Seq.</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[12%] h-auto">Name</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[8%] h-auto">In</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[8%] h-auto">Out</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[15%] h-auto">Source</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[15%] h-auto">Destination</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[15%] h-auto">Service / Port</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[8%] h-auto">Action</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[6%] h-auto">Log</TableHead>
                <TableHead className="p-3 text-[11px] uppercase tracking-wider w-[10%] h-auto">Status</TableHead>
                <TableHead className="p-3 w-[8%] text-right h-auto"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={11} className="p-12 text-center text-muted-foreground text-xs">
                    <div className="flex flex-col items-center justify-center gap-2 py-4">
                      <Loader2 className="h-6 w-6 animate-spin text-primary" />
                      <span>กำลังโหลดข้อมูล...</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : filteredRules.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={11} className="p-8 text-center text-muted-foreground text-xs">
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

              {/* 5. Default Implicit Deny Row */}
              <TableRow className="bg-muted/10 border-t border-border/40 text-muted-foreground/70 hover:bg-muted/10">
                <TableCell className="p-3 text-xs font-mono flex items-center gap-2 pl-6.5">
                  <Lock className="h-3.5 w-3.5 text-muted-foreground/50" />
                  -
                </TableCell>
                <TableCell className="p-3 italic">Implicit Deny</TableCell>
                <TableCell className="p-3 italic">-</TableCell>
                <TableCell className="p-3 italic">-</TableCell>
                <TableCell className="p-3 italic">ALL</TableCell>
                <TableCell className="p-3 italic">ALL</TableCell>
                <TableCell className="p-3 italic">ALL</TableCell>
                <TableCell className="p-3">
                  <Badge variant="destructive" className="font-bold text-[10px] px-2 py-0.5 rounded bg-red-500/10 text-red-500/80 border-red-500/20">
                    DROP
                  </Badge>
                </TableCell>
                <TableCell className="p-3">-</TableCell>
                <TableCell className="p-3 flex items-center gap-1.5 py-4">
                  <Badge variant="outline" className="text-[10px] bg-muted border-border text-muted-foreground px-1.5 py-0.5">
                    System
                  </Badge>
                </TableCell>
                <TableCell></TableCell>
              </TableRow>
            </TableBody>
          </Table>
        </div>
      </Card>

      {/* 6. Footer warnings */}
      <Alert className="border-dashed border-border bg-card/10">
        <AlertCircle className="h-4 w-4 text-muted-foreground" />
        <AlertTitle className="font-bold text-foreground mb-0.5">ข้อแนะนำเกี่ยวกับการจัดเรียงลำดับนโยบาย:</AlertTitle>
        <AlertDescription className="text-xs text-muted-foreground leading-relaxed">
          การตั้งค่ากฎ Firewall จะเรียงลำดับความสำคัญจากบนลงล่าง (First-match wins) กฎที่อยู่ด้านบนสุดจะถูกตรวจสอบและประมวลผลก่อนเสมอ
          หากกฎด้านบนตรงตามเงื่อนไข จะไม่มีการประมวลผลกฎข้อถัดไปลงมา
          หลังจากเพิ่ม แก้ไข หรือจัดเรียงกฎใหม่เรียบร้อยแล้ว จำเป็นต้องกดปุ่ม <span className="font-semibold text-primary">"Apply Settings"</span> ทางขวาบนเพื่ออัปโหลดนโยบายเข้าสู่ระบบ Kernel `nftables` จริง
        </AlertDescription>
      </Alert>

      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent ref={dialogContentRef} className="md:max-w-[85vw] lg:max-w-[960px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingRule ? "แก้ไขนโยบายความปลอดภัย" : "สร้างนโยบายความปลอดภัยใหม่"}
            </DialogTitle>
          </DialogHeader>

          {/* Form */}
          <form onSubmit={handleSaveForm} className="space-y-5 text-sm">
            {/* Row 1: Name & Service/Port */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label htmlFor="form-name" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  ชื่อนโยบาย (Name) <span className="text-red-500">*</span>
                </Label>
                <Input
                  id="form-name"
                  type="text"
                  required
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="เช่น Allow-HTTP-Out, Block-MalIPs"
                  className="bg-background/50 placeholder:text-muted-foreground h-9"
                />
              </div>
              <div className="space-y-1.5">
                <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  บริการ / พอร์ต (Service/Port) <span className="text-red-500">*</span>
                </Label>
                <Combobox
                  multiple={true}
                  required
                  value={formService}
                  onValueChange={(val) => setFormService(val as string[])}
                  items={serviceOptions}
                >
                  <ComboboxChips ref={serviceAnchor} className="bg-background/50 border border-border rounded-lg min-h-9 flex items-center flex-wrap px-2 py-1 gap-1.5 focus-within:ring-1 focus-within:ring-primary/50 focus-within:border-primary/50">
                    <ComboboxValue>
                      {(values: string[]) => (
                        <>
                          {values.map((val) => (
                            <ComboboxChip key={val} className="text-xs">
                              {val}
                            </ComboboxChip>
                          ))}
                          <ComboboxChipsInput placeholder={values.length === 0 ? "เลือกบริการ / พอร์ต..." : ""} className="h-7 text-xs bg-transparent border-none outline-none focus:ring-0" />
                        </>
                      )}
                    </ComboboxValue>
                  </ComboboxChips>
                  <ComboboxContent anchor={serviceAnchor} className="w-[var(--anchor-width)] bg-popover border border-border rounded-lg overflow-hidden">
                    <ComboboxEmpty className="p-2 text-xs text-muted-foreground text-center">ไม่พบข้อมูล</ComboboxEmpty>
                    <ComboboxList className="p-1 max-h-48 overflow-y-auto">
                      {(opt: string) => (
                        <ComboboxItem key={opt} value={opt} className="cursor-pointer hover:bg-muted/80 text-xs">
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
                <Label htmlFor="form-in-interface" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  การ์ดขาเข้า (In Interface) <span className="text-red-500">*</span>
                </Label>
                <select
                  id="form-in-interface"
                  value={formInInterface}
                  onChange={(e) => setFormInInterface(e.target.value)}
                  className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                >
                  {interfaceOptions.map((opt) => (
                    <option key={opt} value={opt}>
                      {opt === "ALL" ? "ALL (ทุกอินเตอร์เฟส)" : (() => {
                        const iface = interfaces.find(i => i.name === opt);
                        return iface ? `${iface.alias || iface.name} (${iface.name})` : opt;
                      })()}
                    </option>
                  ))}
                </select>
              </div>
              <div className="space-y-1.5">
                <Label htmlFor="form-out-interface" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  การ์ดขาออก (Out Interface) <span className="text-red-500">*</span>
                </Label>
                <select
                  id="form-out-interface"
                  value={formOutInterface}
                  onChange={(e) => setFormOutInterface(e.target.value)}
                  className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                >
                  {interfaceOptions.map((opt) => (
                    <option key={opt} value={opt}>
                      {opt === "ALL" ? "ALL (ทุกอินเตอร์เฟส)" : (() => {
                        const iface = interfaces.find(i => i.name === opt);
                        return iface ? `${iface.alias || iface.name} (${iface.name})` : opt;
                      })()}
                    </option>
                  ))}
                </select>
              </div>
            </div>

            {/* Row 2: Source & Destination with Multiple Selection Combobox */}
            <div className="grid gap-4 sm:grid-cols-2">
              <div className="space-y-1.5">
                <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  ต้นทาง (Source IP/Network) <span className="text-red-500">*</span>
                </Label>
                <Combobox
                  multiple={true}
                  required
                  value={formSource}
                  onValueChange={(val) => setFormSource(val as string[])}
                  items={sourceOptions}
                >
                  <ComboboxChips ref={sourceAnchor} className="bg-background/50 border border-border rounded-lg min-h-9 flex items-center flex-wrap px-2 py-1 gap-1.5 focus-within:ring-1 focus-within:ring-primary/50 focus-within:border-primary/50">
                    <ComboboxValue>
                      {(values: string[]) => (
                        <>
                          {values.map((val) => (
                            <ComboboxChip key={val} className="text-xs">
                              {val}
                            </ComboboxChip>
                          ))}
                          <ComboboxChipsInput placeholder={values.length === 0 ? "เลือกต้นทาง..." : ""} className="h-7 text-xs bg-transparent border-none outline-none focus:ring-0" />
                        </>
                      )}
                    </ComboboxValue>
                  </ComboboxChips>
                  <ComboboxContent anchor={sourceAnchor} className="w-[var(--anchor-width)] bg-popover border border-border rounded-lg overflow-hidden">
                    <ComboboxEmpty className="p-2 text-xs text-muted-foreground text-center">ไม่พบข้อมูล</ComboboxEmpty>
                    <ComboboxList className="p-1 max-h-48 overflow-y-auto">
                      {(opt: string) => (
                        <ComboboxItem key={opt} value={opt} className="cursor-pointer hover:bg-muted/80 text-xs">
                          {opt}
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
              </div>

              <div className="space-y-1.5">
                <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  ปลายทาง (Destination IP/Network) <span className="text-red-500">*</span>
                </Label>
                <Combobox
                  multiple={true}
                  required
                  value={formDest}
                  onValueChange={(val) => setFormDest(val as string[])}
                  items={destinationOptions}
                >
                  <ComboboxChips ref={destAnchor} className="bg-background/50 border border-border rounded-lg min-h-9 flex items-center flex-wrap px-2 py-1 gap-1.5 focus-within:ring-1 focus-within:ring-primary/50 focus-within:border-primary/50">
                    <ComboboxValue>
                      {(values: string[]) => (
                        <>
                          {values.map((val) => (
                            <ComboboxChip key={val} className="text-xs">
                              {val}
                            </ComboboxChip>
                          ))}
                          <ComboboxChipsInput placeholder={values.length === 0 ? "เลือกปลายทาง..." : ""} className="h-7 text-xs bg-transparent border-none outline-none focus:ring-0" />
                        </>
                      )}
                    </ComboboxValue>
                  </ComboboxChips>
                  <ComboboxContent anchor={destAnchor} className="w-[var(--anchor-width)] bg-popover border border-border rounded-lg overflow-hidden">
                    <ComboboxEmpty className="p-2 text-xs text-muted-foreground text-center">ไม่พบข้อมูล</ComboboxEmpty>
                    <ComboboxList className="p-1 max-h-48 overflow-y-auto">
                      {(opt: string) => (
                        <ComboboxItem key={opt} value={opt} className="cursor-pointer hover:bg-muted/80 text-xs">
                          {opt}
                        </ComboboxItem>
                      )}
                    </ComboboxList>
                  </ComboboxContent>
                </Combobox>
              </div>
            </div>

            {/* Row 3: Action & Switches */}
            <div className="grid gap-4 sm:grid-cols-2 items-end">
              <div className="space-y-1.5">
                <Label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
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
                  <Label htmlFor="form-log" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider cursor-pointer">บันทึกล็อก (Log)</Label>
                </div>

                <div className="flex items-center gap-2">
                  <Switch
                    id="form-status"
                    checked={formStatus}
                    onCheckedChange={setFormStatus}
                  />
                  <Label htmlFor="form-status" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider cursor-pointer">เปิดใช้งาน (Active)</Label>
                </div>
              </div>
            </div>

            {/* Action Buttons */}
            <div className="flex items-center justify-end gap-3 pt-3 border-t border-border/40">
              <Button
                type="button"
                variant="outline"
                size="sm"
                onClick={() => setIsModalOpen(false)}
                className="cursor-pointer"
              >
                Cancel
              </Button>
              <Button
                type="submit"
                size="sm"
                className="cursor-pointer bg-primary hover:bg-primary/90 text-primary-foreground font-bold"
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
