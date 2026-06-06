import { useState, useMemo } from "react"
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
  AlertCircle
} from "lucide-react"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
import { type PolicyRule, initialPolicyRules } from "@/data-mockup/mockData"

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
  onEdit: (rule: PolicyRule) => void
  onDelete: (id: string) => void
  onToggleStatus: (id: string) => void
  onToggleLog: (id: string) => void
}

// Drag & Drop Row component
function SortableRow({ rule, index, onEdit, onDelete, onToggleStatus, onToggleLog }: SortableRowProps) {
  const { attributes, listeners, setNodeRef, transform, transition, isDragging } = useSortable({ id: rule.id })

  const style = {
    transform: CSS.Transform.toString(transform),
    transition,
    zIndex: isDragging ? 10 : 1,
    opacity: isDragging ? 0.5 : 1
  }

  return (
    <tr
      ref={setNodeRef}
      style={style}
      className={`border-b border-border/40 transition duration-200 hover:bg-muted/15 ${!rule.status ? "bg-muted/5 opacity-55" : ""
        } ${isDragging ? "bg-muted/30" : ""}`}
    >
      {/* 1. Sequence & Drag Handle */}
      <td className="p-3 text-xs text-muted-foreground font-mono">
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
      </td>

      {/* 2. Name */}
      <td className="p-3 font-semibold text-foreground">{rule.name}</td>

      {/* 3. Source */}
      <td className="p-3">
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
      </td>

      {/* 4. Destination */}
      <td className="p-3">
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
      </td>

      {/* 5. Service / Port */}
      <td className="p-3">
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
      </td>

      {/* 6. Action */}
      <td className="p-3">
        <Badge
          variant={rule.action === "DROP" ? "destructive" : "outline"}
          className={`font-bold text-[10px] px-2 py-0.5 rounded ${rule.action === "ACCEPT"
            ? "bg-primary/10 text-primary border border-primary/20"
            : "bg-red-500/10 text-red-500 border-red-500/20"
            }`}
        >
          {rule.action}
        </Badge>
      </td>

      {/* 7. Log Switch */}
      <td className="p-3">
        <Switch
          size="sm"
          checked={rule.log}
          onCheckedChange={() => onToggleLog(rule.id)}
        />
      </td>

      {/* 8. Status Enable Switch */}
      <td className="p-3">
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
      </td>

      {/* 9. Action Buttons */}
      <td className="p-3 text-right">
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
      </td>
    </tr>
  )
}

export default function FirewallPolicy() {
  // --- State for Policies ---
  const [rules, setRules] = useState<PolicyRule[]>(initialPolicyRules)

  // --- Search and Filters State ---
  const [searchQuery, setSearchQuery] = useState<string>("")

  // --- Reload nftables Simulation ---
  const [isApplying, setIsApplying] = useState<boolean>(false)
  const [showApplySuccess, setShowApplySuccess] = useState<boolean>(false)
  const [applyProgress, setApplyProgress] = useState<string>("")

  const handleApplySettings = () => {
    setIsApplying(true)
    setShowApplySuccess(false)
    setApplyProgress("กำลังตรวจสอบโครงสร้างของกฎไฟร์วอลล์...")

    setTimeout(() => {
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
    }, 600)
  }

  // --- Modal Forms State ---
  const [isModalOpen, setIsModalOpen] = useState<boolean>(false)
  const [editingRule, setEditingRule] = useState<PolicyRule | null>(null)

  // Form Fields
  const [formName, setFormName] = useState<string>("")
  const [formSource, setFormSource] = useState<string>("")
  const [formDest, setFormDest] = useState<string>("")
  const [formService, setFormService] = useState<string>("")
  const [formAction, setFormAction] = useState<"ACCEPT" | "DROP">("ACCEPT")
  const [formLog, setFormLog] = useState<boolean>(false)
  const [formStatus, setFormStatus] = useState<boolean>(true)

  // --- Dialog control triggers ---
  const openCreateModal = () => {
    setEditingRule(null)
    setFormName("")
    setFormSource("LAN_Network")
    setFormDest("ALL (Internet)")
    setFormService("HTTPS (443)")
    setFormAction("ACCEPT")
    setFormLog(false)
    setFormStatus(true)
    setIsModalOpen(true)
  }

  const openEditModal = (rule: PolicyRule) => {
    setEditingRule(rule)
    setFormName(rule.name)
    setFormSource(rule.source.join(", "))
    setFormDest(rule.destination.join(", "))
    setFormService(rule.service.join(", "))
    setFormAction(rule.action)
    setFormLog(rule.log)
    setFormStatus(rule.status)
    setIsModalOpen(true)
  }

  const handleSaveForm = (e: React.FormEvent) => {
    e.preventDefault()

    if (!formName.trim()) return

    const parsedSources = formSource
      .split(",")
      .map((s) => s.trim())
      .filter((s) => s.length > 0)
    const parsedDests = formDest
      .split(",")
      .map((d) => d.trim())
      .filter((d) => d.length > 0)
    const parsedSvcs = formService
      .split(",")
      .map((v) => v.trim())
      .filter((v) => v.length > 0)

    if (editingRule) {
      // Edit mode
      setRules((prev) =>
        prev.map((r) =>
          r.id === editingRule.id
            ? {
              ...r,
              name: formName,
              source: parsedSources,
              destination: parsedDests,
              service: parsedSvcs,
              action: formAction,
              log: formLog,
              status: formStatus
            }
            : r
        )
      )
    } else {
      // Create mode
      const newRule: PolicyRule = {
        id: "rule-" + Math.random().toString(36).substring(2, 9),
        name: formName,
        source: parsedSources.length ? parsedSources : ["ALL"],
        destination: parsedDests.length ? parsedDests : ["ALL"],
        service: parsedSvcs.length ? parsedSvcs : ["ALL"],
        action: formAction,
        log: formLog,
        status: formStatus
      }
      setRules((prev) => [...prev, newRule])
    }

    setIsModalOpen(false)
  }

  // --- Action Toggles inside Row ---
  const handleToggleLog = (id: string) => {
    setRules((prev) => prev.map((r) => (r.id === id ? { ...r, log: !r.log } : r)))
  }

  const handleToggleStatus = (id: string) => {
    setRules((prev) => prev.map((r) => (r.id === id ? { ...r, status: !r.status } : r)))
  }

  const handleDeleteRule = (id: string) => {
    if (confirm("คุณแน่ใจหรือไม่ที่จะลบนโยบายความปลอดภัยนี้?")) {
      setRules((prev) => prev.filter((r) => r.id !== id))
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

  const handleDragEnd = (event: DragEndEvent) => {
    const { active, over } = event

    if (over && active.id !== over.id) {
      setRules((items) => {
        const oldIndex = items.findIndex((item) => item.id === active.id)
        const newIndex = items.findIndex((item) => item.id === over.id)
        return arrayMove(items, oldIndex, newIndex)
      })
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
        <div className="rounded-xl border border-blue-500/20 bg-blue-500/5 p-4 animate-pulse flex items-center gap-3">
          <RefreshCw className="h-5 w-5 text-blue-400 animate-spin" />
          <div className="text-sm">
            <span className="font-semibold text-blue-400 block">กำลังนำนโยบายไปปรับใช้...</span>
            <span className="text-muted-foreground text-xs">{applyProgress}</span>
          </div>
        </div>
      )}

      {showApplySuccess && (
        <div className="rounded-xl border border-primary/20 bg-primary/5 p-4 animate-fade-in flex items-center justify-between gap-3">
          <div className="flex items-center gap-3">
            <div className="flex h-8 w-8 items-center justify-center rounded-full bg-primary/10 text-primary border border-primary/20">
              <Check className="h-4.5 w-4.5" />
            </div>
            <div className="text-sm">
              <span className="font-semibold text-primary block">ปรับใช้สำเร็จ! (nftables reloaded)</span>
              <span className="text-muted-foreground text-xs">
                บิลด์กฎและนำการตั้งค่านโยบายไฟร์วอลล์ทั้งหมดขึ้นระบบเคอร์เนลเรียบร้อยแล้ว
              </span>
            </div>
          </div>
          <Button
            variant="ghost"
            size="icon-xs"
            onClick={() => setShowApplySuccess(false)}
            className="text-muted-foreground hover:text-foreground"
          >
            <X className="h-4 w-4" />
          </Button>
        </div>
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
          <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
          <input
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
            placeholder="ค้นหาชื่อกฎ, ไอพี หรือพอร์ต..."
            className="h-9 w-full rounded-lg border border-border bg-background/50 pl-8 pr-3 text-xs placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/50"
          />
        </div>
      </div>

      {/* 4. Policies Table Container */}
      <Card className="bg-card/25 border border-border/50 overflow-hidden">
        <div className="overflow-x-auto">
          <table className="w-full border-collapse text-left text-sm">
            <thead>
              <tr className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground">
                <th className="p-3 text-[11px] uppercase tracking-wider w-[8%]">Seq.</th>
                <th className="p-3 text-[11px] uppercase tracking-wider w-[15%]">Name</th>
                <th className="p-3 text-[11px] uppercase tracking-wider w-[18%]">Source</th>
                <th className="p-3 text-[11px] uppercase tracking-wider w-[18%]">Destination</th>
                <th className="p-3 text-[11px] uppercase tracking-wider w-[18%]">Service / Port</th>
                <th className="p-3 text-[11px] uppercase tracking-wider w-[8%]">Action</th>
                <th className="p-3 text-[11px] uppercase tracking-wider w-[8%]">Log</th>
                <th className="p-3 text-[11px] uppercase tracking-wider w-[10%]">Status</th>
                <th className="p-3 w-[8%] text-right"></th>
              </tr>
            </thead>
            <tbody>
              {filteredRules.length === 0 ? (
                <tr>
                  <td colSpan={9} className="p-8 text-center text-muted-foreground text-xs">
                    ไม่พบนโยบายไฟร์วอลล์ที่ค้นหา
                  </td>
                </tr>
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
              <tr className="bg-muted/10 border-t border-border/40 text-muted-foreground/70">
                <td className="p-3 text-xs font-mono flex items-center gap-2 pl-6.5">
                  <Lock className="h-3.5 w-3.5 text-muted-foreground/50" />
                  -
                </td>
                <td className="p-3 italic">Implicit Deny</td>
                <td className="p-3 italic">ALL</td>
                <td className="p-3 italic">ALL</td>
                <td className="p-3 italic">ALL</td>
                <td className="p-3">
                  <Badge variant="destructive" className="font-bold text-[10px] px-2 py-0.5 rounded bg-red-500/10 text-red-500/80 border-red-500/20">
                    DROP
                  </Badge>
                </td>
                <td className="p-3">-</td>
                <td className="p-3 flex items-center gap-1.5 py-4">
                  <Badge variant="outline" className="text-[10px] bg-neutral-900 border-neutral-800 text-muted-foreground px-1.5 py-0.5">
                    System
                  </Badge>
                </td>
                <td></td>
              </tr>
            </tbody>
          </table>
        </div>
      </Card>

      {/* 6. Footer warnings */}
      <div className="flex items-start gap-2.5 rounded-xl border border-dashed border-border bg-card/10 p-4 text-xs text-muted-foreground leading-relaxed">
        <AlertCircle className="h-4.5 w-4.5 text-muted-foreground shrink-0 mt-0.5" />
        <div>
          <span className="font-bold text-foreground block mb-0.5">ข้อแนะนำเกี่ยวกับการจัดเรียงลำดับนโยบาย:</span>
          การตั้งค่ากฎ Firewall จะเรียงลำดับความสำคัญจากบนลงล่าง (First-match wins) กฎที่อยู่ด้านบนสุดจะถูกตรวจสอบและประมวลผลก่อนเสมอ
          หากกฎด้านบนตรงตามเงื่อนไข จะไม่มีการประมวลผลกฎข้อถัดไปลงมา
          หลังจากเพิ่ม แก้ไข หรือจัดเรียงกฎใหม่เรียบร้อยแล้ว จำเป็นต้องกดปุ่ม <span className="font-semibold text-primary">"Apply Settings"</span> ทางขวาบนเพื่ออัปโหลดนโยบายเข้าสู่ระบบ Kernel `nftables` จริง
        </div>
      </div>

      {/* 7. Dialog Modal (Create & Edit Overlay) */}
      {isModalOpen && (
        <div className="fixed inset-0 z-50 flex items-center justify-center bg-black/60 p-4 animate-fade-in">
          <div className="w-full max-w-lg rounded-xl border border-border bg-card p-6 space-y-4 animate-scale-up">
            {/* Header */}
            <div className="flex items-center justify-between pb-3 border-b border-border/40">
              <h3 className="text-lg font-bold text-foreground">
                {editingRule ? "แก้ไขนโยบายความปลอดภัย" : "สร้างนโยบายความปลอดภัยใหม่"}
              </h3>
              <Button
                variant="ghost"
                size="icon-xs"
                onClick={() => setIsModalOpen(false)}
                className="text-muted-foreground hover:text-foreground cursor-pointer"
              >
                <X className="h-4.5 w-4.5" />
              </Button>
            </div>

            {/* Form */}
            <form onSubmit={handleSaveForm} className="space-y-4 text-sm">
              {/* Name */}
              <div className="space-y-1.5">
                <label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  ชื่อนโยบาย (Name) <span className="text-red-500">*</span>
                </label>
                <input
                  type="text"
                  required
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="เช่น Allow-HTTP-Out, Block-MalIPs"
                  className="h-9 w-full rounded-lg border border-border bg-background/50 px-3 py-1 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/50"
                />
              </div>

              {/* Source & Dest Grid */}
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    ต้นทาง (Source IP/Network)
                  </label>
                  <input
                    type="text"
                    value={formSource}
                    onChange={(e) => setFormSource(e.target.value)}
                    placeholder="เช่น LAN_Network, 192.168.1.100"
                    className="h-9 w-full rounded-lg border border-border bg-background/50 px-3 py-1 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/50"
                  />
                  <span className="text-[10.5px] text-muted-foreground">คั่นด้วยเครื่องหมายจุลภาค (,)</span>
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    ปลายทาง (Destination IP/Network)
                  </label>
                  <input
                    type="text"
                    value={formDest}
                    onChange={(e) => setFormDest(e.target.value)}
                    placeholder="เช่น ALL (Internet), Web_Host"
                    className="h-9 w-full rounded-lg border border-border bg-background/50 px-3 py-1 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/50"
                  />
                </div>
              </div>

              {/* Service & Action Grid */}
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    บริการ / พอร์ต (Service/Port)
                  </label>
                  <input
                    type="text"
                    value={formService}
                    onChange={(e) => setFormService(e.target.value)}
                    placeholder="เช่น HTTP (80), TCP (22), ALL"
                    className="h-9 w-full rounded-lg border border-border bg-background/50 px-3 py-1 text-sm placeholder:text-muted-foreground focus-visible:outline-none focus-visible:ring-1 focus-visible:ring-primary/50"
                  />
                </div>
                <div className="space-y-1.5">
                  <label className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                    การจัดการ (Action)
                  </label>
                  <div className="flex rounded-md border border-border bg-background/30 p-0.5 h-9 text-xs">
                    {(["ACCEPT", "DROP"] as const).map((act) => (
                      <button
                        key={act}
                        type="button"
                        onClick={() => setFormAction(act)}
                        className={`flex-1 rounded text-center cursor-pointer font-bold transition ${formAction === act
                          ? act === "ACCEPT"
                            ? "bg-primary/10 text-primary border border-primary/20 font-semibold"
                            : "bg-red-500/10 text-red-400 border border-red-500/20 font-semibold"
                          : "text-muted-foreground hover:text-foreground"
                          }`}
                      >
                        {act}
                      </button>
                    ))}
                  </div>
                </div>
              </div>

              {/* Switches Area */}
              <div className="flex items-center gap-6 pt-2">
                <div className="flex items-center gap-2">
                  <Switch
                    checked={formLog}
                    onCheckedChange={setFormLog}
                  />
                  <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">บันทึกล็อก (Log Packet)</span>
                </div>

                <div className="flex items-center gap-2">
                  <Switch
                    checked={formStatus}
                    onCheckedChange={setFormStatus}
                  />
                  <span className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">เปิดใช้งานทันที (Active)</span>
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
          </div>
        </div>
      )}
    </div>
  )
}
