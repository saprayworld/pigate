import React, { useState, useEffect, useMemo } from "react"
import {
  Sliders,
  Plus,
  Search,
  Edit,
  Trash2,
  AlertCircle,
  Network,
  RefreshCw,
  Loader2,
  ArrowDown,
  ArrowUp,
  Activity,
  Server
} from "lucide-react"
import { Card, CardHeader, CardTitle, CardDescription, CardContent } from "@/components/ui/card"
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
import { Switch } from "@/components/ui/switch"
import { qosService, type QosRule, type QosIfaceStatus } from "@/services/qosService"
import { interfaceService } from "@/services/interfaceService"
import { type NetworkInterface } from "@/data-mockup/mockData"
import { useAlert } from "@/components/AlertDialogProvider"
import { isValidCidr } from "@/lib/utils"

export default function QoS() {
  const { alert: showAlert, confirm } = useAlert()

  // --- State ---
  const [rules, setRules] = useState<QosRule[]>([])
  const [interfaces, setInterfaces] = useState<NetworkInterface[]>([])
  const [ifaceStatuses, setIfaceStatuses] = useState<Record<string, QosIfaceStatus>>({})
  const [isLoading, setIsLoading] = useState(true)
  const [isSaving, setIsSaving] = useState(false)
  const [isSyncing, setIsSyncing] = useState(false)
  const [searchQuery, setSearchQuery] = useState("")

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingRule, setEditingRule] = useState<QosRule | null>(null)

  // Form Fields
  const [formName, setFormName] = useState("")
  const [formInterface, setFormInterface] = useState("")
  const [formMatchSrcIp, setFormMatchSrcIp] = useState("")
  const [formMatchDstIp, setFormMatchDstIp] = useState("")
  const [formEgressRate, setFormEgressRate] = useState("50")
  const [formEgressCeil, setFormEgressCeil] = useState("100")
  const [formIngressRate, setFormIngressRate] = useState("0")
  const [formIngressCeil, setFormIngressCeil] = useState("0")
  const [formPriority, setFormPriority] = useState("10")
  const [formStatus, setFormStatus] = useState(true)
  const [formDescription, setFormDescription] = useState("")

  const [formError, setFormError] = useState("")

  // --- Load Data ---
  const loadData = async () => {
    try {
      setIsLoading(true)
      const [allRules, allIfaces] = await Promise.all([
        qosService.getAll(),
        interfaceService.getAll()
      ])
      setRules(allRules)
      setInterfaces(allIfaces)

      // Fetch kernel QoS status for each interface
      const statusMap: Record<string, QosIfaceStatus> = {}
      await Promise.all(
        allIfaces.map(async (iface) => {
          try {
            const status = await qosService.getIfaceStatus(iface.name)
            statusMap[iface.name] = status
          } catch (e) {
            console.error(`Failed to get QoS status for ${iface.name}`, e)
          }
        })
      )
      setIfaceStatuses(statusMap)
    } catch (err: any) {
      showAlert("Error", err.message || "Failed to load QoS data")
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    loadData()
  }, [])

  // --- Form Reset ---
  const resetForm = (rule?: QosRule) => {
    setFormError("")
    if (rule) {
      setEditingRule(rule)
      setFormName(rule.name)
      setFormInterface(rule.interface)
      setFormMatchSrcIp(rule.matchSrcIp)
      setFormMatchDstIp(rule.matchDstIp)
      setFormEgressRate(String(rule.egressRateMbps))
      setFormEgressCeil(String(rule.egressCeilMbps))
      setFormIngressRate(String(rule.ingressRateMbps))
      setFormIngressCeil(String(rule.ingressCeilMbps))
      setFormPriority(String(rule.priority))
      setFormStatus(rule.status)
      setFormDescription(rule.description)
    } else {
      setEditingRule(null)
      setFormName("")
      // Select first interface as default if available
      setFormInterface(interfaces[0]?.name || "")
      setFormMatchSrcIp("")
      setFormMatchDstIp("")
      setFormEgressRate("50")
      setFormEgressCeil("100")
      setFormIngressRate("0")
      setFormIngressCeil("0")
      setFormPriority("10")
      setFormStatus(true)
      setFormDescription("")
    }
  }

  // --- Open Modals ---
  const handleOpenCreateModal = () => {
    resetForm()
    setIsModalOpen(true)
  }

  const handleOpenEditModal = (rule: QosRule) => {
    resetForm(rule)
    setIsModalOpen(true)
  }

  // --- Actions ---
  const handleSaveRule = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    // Form Validations
    if (!formName.trim()) {
      setFormError("กรุณากรอกชื่อกฎ QoS")
      return
    }
    if (!formInterface) {
      setFormError("กรุณาเลือกอินเทอร์เฟซ")
      return
    }
    if (formMatchSrcIp && !isValidCidr(formMatchSrcIp)) {
      setFormError("Source IP ต้องเป็นฟอร์แมต CIDR (เช่น 192.168.1.0/24)")
      return
    }
    if (formMatchDstIp && !isValidCidr(formMatchDstIp)) {
      setFormError("Destination IP ต้องเป็นฟอร์แมต CIDR (เช่น 8.8.8.8/32)")
      return
    }

    const egressRate = parseInt(formEgressRate, 10) || 0
    const egressCeil = parseInt(formEgressCeil, 10) || 0
    const ingressRate = parseInt(formIngressRate, 10) || 0
    const ingressCeil = parseInt(formIngressCeil, 10) || 0
    const priority = parseInt(formPriority, 10) || 10

    if (egressRate < 0 || egressCeil < 0 || ingressRate < 0 || ingressCeil < 0) {
      setFormError("ความเร็วแบนด์วิธต้องไม่ต่ำกว่า 0")
      return
    }

    const payload = {
      name: formName.trim(),
      interface: formInterface,
      matchSrcIp: formMatchSrcIp.trim(),
      matchDstIp: formMatchDstIp.trim(),
      egressRateMbps: egressRate,
      egressCeilMbps: egressCeil,
      ingressRateMbps: ingressRate,
      ingressCeilMbps: ingressCeil,
      priority: priority,
      status: formStatus,
      description: formDescription.trim()
    }

    try {
      setIsSaving(true)
      if (editingRule) {
        await qosService.update(editingRule.id, payload)
      } else {
        await qosService.create(payload)
      }
      setIsModalOpen(false)
      loadData()
    } catch (err: any) {
      setFormError(err.message || "Failed to save QoS rule")
    } finally {
      setIsSaving(false)
    }
  }

  const handleDeleteRule = async (rule: QosRule) => {
    const confirmed = await confirm("ลบกฎ QoS", `คุณแน่ใจหรือไม่ที่จะลบกฎ "${rule.name}"? การลบนี้จะถูกซิงก์ไปที่เคอร์เนลทันที`)
    if (!confirmed) return

    try {
      setIsLoading(true)
      await qosService.delete(rule.id)
      loadData()
    } catch (err: any) {
      showAlert("Error", err.message || "Failed to delete QoS rule")
    } finally {
      setIsLoading(false)
    }
  }

  const handleToggleRule = async (rule: QosRule) => {
    try {
      await qosService.toggle(rule.id)
      // Quick updates list status locally for smoother UX before full reload
      setRules((prev) =>
        prev.map((r) => (r.id === rule.id ? { ...r, status: !r.status } : r))
      )
      loadData()
    } catch (err: any) {
      showAlert("Error", err.message || "Failed to toggle QoS status")
    }
  }

  const handleSyncToKernel = async () => {
    try {
      setIsSyncing(true)
      await qosService.sync()
      await loadData()
      showAlert("Success", "ซิงก์การตั้งค่า QoS ไปยังเคอร์เนลเรียบร้อยแล้ว")
    } catch (err: any) {
      showAlert("Error", err.message || "Failed to sync QoS to kernel")
    } finally {
      setIsSyncing(false)
    }
  }

  const handleClearIface = async (ifaceName: string) => {
    const confirmed = await confirm("ล้างค่า QoS บน Interface", `คุณแน่ใจหรือไม่ที่จะปิดการจำกัดแบนด์วิธทั้งหมดบนอินเทอร์เฟซ "${ifaceName}"? กฎทั้งหมดของอินเทอร์เฟซนี้จะถูกปรับเป็น Disabled`)
    if (!confirmed) return

    try {
      setIsLoading(true)
      await qosService.clearIface(ifaceName)
      loadData()
    } catch (err: any) {
      showAlert("Error", err.message || "Failed to clear QoS on interface")
    } finally {
      setIsLoading(false)
    }
  }

  // --- Filter rules based on search ---
  const filteredRules = useMemo(() => {
    return rules.filter((rule) => {
      const q = searchQuery.toLowerCase()
      return (
        rule.name.toLowerCase().includes(q) ||
        rule.interface.toLowerCase().includes(q) ||
        rule.matchSrcIp.toLowerCase().includes(q) ||
        rule.matchDstIp.toLowerCase().includes(q) ||
        rule.description.toLowerCase().includes(q)
      )
    })
  }, [rules, searchQuery])

  if (isLoading && rules.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[400px] space-y-4">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="text-sm text-muted-foreground font-semibold">กำลังโหลดข้อมูลระบบ QoS...</span>
      </div>
    )
  }

  return (
    <div className="space-y-6">
      {/* 1. Header */}
      <div className="flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <Sliders className="h-7 w-7 text-primary fill-primary/10" />
            QoS Bandwidth Limiting
          </h1>
          <p className="text-muted-foreground mt-1">
            การจำกัดความเร็วแบนด์วิธอินเทอร์เน็ต (Traffic Shaping) ตาม Interface หรือ Subnet วงไอพี
          </p>
        </div>
        <div className="flex gap-2">
          <Button
            variant="outline"
            className="gap-2 cursor-pointer border-border"
            onClick={loadData}
          >
            <RefreshCw className="h-4 w-4" />
            Reload
          </Button>
          <Button
            className="gap-2 cursor-pointer bg-primary text-primary-foreground font-medium"
            disabled={isSyncing}
            onClick={handleSyncToKernel}
          >
            {isSyncing ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <Activity className="h-4 w-4" />
            )}
            Sync to Kernel
          </Button>
        </div>
      </div>

      {/* 2. Top Overview Status Card Grid */}
      <div className="grid grid-cols-1 md:grid-cols-2 lg:grid-cols-3 gap-4">
        {interfaces.map((iface) => {
          const status = ifaceStatuses[iface.name]
          const isQosActive = status?.hasQdisc

          return (
            <Card key={iface.id} className="border-border bg-card/60 backdrop-blur-sm">
              <CardHeader className="pb-3 flex flex-row items-start justify-between space-y-0">
                <div className="space-y-1">
                  <div className="flex items-center gap-2">
                    <Network className="h-4 w-4 text-muted-foreground" />
                    <span className="font-bold text-foreground text-sm uppercase">{iface.name}</span>
                    <Badge variant="outline" className="text-[10px] uppercase font-normal text-muted-foreground">
                      {iface.role}
                    </Badge>
                  </div>
                  <CardDescription className="text-[11px] truncate max-w-[180px]">
                    {iface.alias || iface.type}
                  </CardDescription>
                </div>
                <Badge
                  className={`rounded-full px-2 py-0.5 text-[10px] font-semibold tracking-wide border ${isQosActive
                      ? "bg-emerald-500/10 text-emerald-500 border-emerald-500/20"
                      : "bg-muted text-muted-foreground border-border/40"
                    }`}
                >
                  {isQosActive ? "QoS Active" : "No Shaping"}
                </Badge>
              </CardHeader>

              <CardContent className="space-y-4 pt-0">
                {isQosActive ? (
                  <div className="space-y-2">
                    <span className="text-[11px] font-semibold text-muted-foreground uppercase block">
                      Active Kernel Classes
                    </span>
                    <div className="space-y-1 text-xs">
                      {status.classes.map((cls) => {
                        const isIngress = cls.classId.toLowerCase().includes("ingress");
                        return (
                          <div
                            key={cls.classId}
                            className="flex items-center justify-between bg-accent/30 rounded px-2.5 py-1.5 border border-border/30"
                          >
                            <div className="font-semibold text-foreground flex items-center gap-1">
                              <span className="text-[10px] text-muted-foreground bg-accent px-1 rounded">
                                {cls.classId}
                              </span>
                              <span className="truncate max-w-[100px]">{cls.ruleName || "Shared Limit"}</span>
                            </div>
                            <div className="flex items-center gap-2 text-muted-foreground">
                              <span className={`flex items-center gap-0.5 font-semibold text-xs ${isIngress ? "text-amber-500 animate-pulse" : "text-emerald-500"}`}>
                                {isIngress ? <ArrowDown className="h-3.5 w-3.5" /> : <ArrowUp className="h-3.5 w-3.5" />}
                                {cls.ceil}
                              </span>
                            </div>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                ) : (
                  <div className="text-xs text-muted-foreground py-2 flex items-center gap-2">
                    <InfoIcon className="h-4 w-4 text-muted-foreground/60" />
                    <span>แบนด์วิธวิ่งตามความเร็วการ์ดแลนปกติ (Unshaped)</span>
                  </div>
                )}

                <div className="flex justify-end pt-1 border-t border-border/20">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="text-[11px] h-7 px-2.5 text-destructive hover:bg-destructive/10 hover:text-destructive cursor-pointer disabled:opacity-30"
                    disabled={!isQosActive}
                    onClick={() => handleClearIface(iface.name)}
                  >
                    Clear QoS
                  </Button>
                </div>
              </CardContent>
            </Card>
          )
        })}
      </div>

      {/* 3. QoS Rules List Card */}
      <Card className="border-border bg-card">
        <CardHeader className="pb-3 border-b border-border/40 flex flex-col sm:flex-row sm:items-center sm:justify-between gap-4">
          <div>
            <CardTitle className="text-xl font-bold flex items-center gap-2">
              QoS Rules Database
              <Badge variant="secondary" className="rounded-full bg-accent text-accent-foreground text-xs font-semibold px-2 py-0">
                {rules.length}
              </Badge>
            </CardTitle>
            <CardDescription className="text-xs">
              ตารางกำหนดความสำคัญของความเร็วตามวงไอพีต้นทาง ปลายทาง และอินเทอร์เฟซ
            </CardDescription>
          </div>

          <div className="flex items-center gap-3">
            {/* Search Input */}
            <div className="relative w-full sm:w-[220px]">
              <Search className="absolute left-2.5 top-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="ค้นหา QoS rule..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="pl-9 h-9 text-xs border-border bg-card/50"
              />
            </div>
            {/* Add Rule Button */}
            <Button
              className="gap-1.5 h-9 text-xs font-medium cursor-pointer"
              onClick={handleOpenCreateModal}
            >
              <Plus className="h-4 w-4" />
              Add QoS Rule
            </Button>
          </div>
        </CardHeader>

        <CardContent className="p-0">
          <div className="overflow-x-auto">
            <Table>
              <TableHeader className="bg-muted/30">
                <TableRow className="border-border/60">
                  <TableHead className="w-[100px] text-xs font-bold">Status</TableHead>
                  <TableHead className="text-xs font-bold">Rule Name</TableHead>
                  <TableHead className="text-xs font-bold">Interface</TableHead>
                  <TableHead className="text-xs font-bold">Match Src IP</TableHead>
                  <TableHead className="text-xs font-bold">Match Dst IP</TableHead>
                  <TableHead className="text-xs font-bold">Egress Limit (Outgoing)</TableHead>
                  <TableHead className="text-xs font-bold">Ingress Limit (Incoming)</TableHead>
                  <TableHead className="w-[80px] text-xs font-bold text-center">Priority</TableHead>
                  <TableHead className="w-[120px] text-xs font-bold text-center">Actions</TableHead>
                </TableRow>
              </TableHeader>

              <TableBody>
                {filteredRules.length === 0 ? (
                  <TableRow>
                    <TableCell colSpan={9} className="h-28 text-center text-muted-foreground">
                      <div className="flex flex-col items-center justify-center space-y-2">
                        <AlertCircle className="h-8 w-8 text-muted-foreground/60" />
                        <span className="text-sm font-semibold">ไม่พบข้อมูลกฎ QoS</span>
                        <span className="text-xs text-muted-foreground/75">
                          {searchQuery ? "ลองพิมพ์ค้นหาใหม่อีกครั้ง" : "กดปุ่ม Add QoS Rule เพื่อสร้างกฎใหม่"}
                        </span>
                      </div>
                    </TableCell>
                  </TableRow>
                ) : (
                  filteredRules.map((rule) => (
                    <TableRow
                      key={rule.id}
                      className={`border-border/40 hover:bg-muted/10 transition-colors ${!rule.status ? "opacity-60 bg-muted/5" : ""
                        }`}
                    >
                      <TableCell className="align-middle">
                        <Switch
                          checked={rule.status}
                          onCheckedChange={() => handleToggleRule(rule)}
                        />
                      </TableCell>

                      <TableCell className="align-middle font-bold text-foreground">
                        <div className="flex flex-col">
                          <span>{rule.name}</span>
                          {rule.description && (
                            <span className="text-[11px] font-normal text-muted-foreground max-w-[200px] truncate">
                              {rule.description}
                            </span>
                          )}
                        </div>
                      </TableCell>

                      <TableCell className="align-middle font-semibold uppercase text-xs text-foreground">
                        {rule.interface}
                      </TableCell>

                      <TableCell className="align-middle font-mono text-xs">
                        {rule.matchSrcIp || <span className="text-muted-foreground italic">Match All</span>}
                      </TableCell>

                      <TableCell className="align-middle font-mono text-xs">
                        {rule.matchDstIp || <span className="text-muted-foreground italic">Match All</span>}
                      </TableCell>

                      <TableCell className="align-middle">
                        {rule.egressRateMbps > 0 ? (
                          <div className="flex items-center gap-1.5 text-xs text-emerald-500 font-semibold">
                            <ArrowUp className="h-3.5 w-3.5" />
                            <span>{rule.egressRateMbps} Mbps</span>
                            {rule.egressCeilMbps > rule.egressRateMbps && (
                              <span className="text-[10px] text-muted-foreground font-normal">
                                (Max {rule.egressCeilMbps})
                              </span>
                            )}
                          </div>
                        ) : (
                          <Badge variant="outline" className="text-[10px] font-normal text-muted-foreground border-border/40">
                            Unlimited
                          </Badge>
                        )}
                      </TableCell>

                      <TableCell className="align-middle">
                        {rule.ingressRateMbps > 0 ? (
                          <div className="flex items-center gap-1.5 text-xs text-amber-500 font-semibold">
                            <ArrowDown className="h-3.5 w-3.5" />
                            <span>{rule.ingressRateMbps} Mbps</span>
                            {rule.ingressCeilMbps > rule.ingressRateMbps && (
                              <span className="text-[10px] text-muted-foreground font-normal">
                                (Max {rule.ingressCeilMbps})
                              </span>
                            )}
                          </div>
                        ) : (
                          <Badge variant="outline" className="text-[10px] font-normal text-muted-foreground border-border/40">
                            Unlimited
                          </Badge>
                        )}
                      </TableCell>

                      <TableCell className="align-middle text-center text-xs font-semibold">
                        {rule.priority}
                      </TableCell>

                      <TableCell className="align-middle text-center">
                        <div className="flex items-center justify-center gap-2">
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8 rounded-lg text-muted-foreground hover:text-foreground cursor-pointer"
                            onClick={() => handleOpenEditModal(rule)}
                          >
                            <Edit className="h-4 w-4" />
                          </Button>
                          <Button
                            variant="ghost"
                            size="icon"
                            className="h-8 w-8 rounded-lg text-destructive hover:bg-destructive/10 hover:text-destructive cursor-pointer"
                            onClick={() => handleDeleteRule(rule)}
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
          </div>
        </CardContent>
      </Card>

      {/* 4. Info Alert */}
      <Alert className="border-primary/10 bg-primary/5 text-primary">
        <Server className="h-4 w-4 stroke-primary" />
        <AlertTitle className="font-bold text-xs tracking-wider uppercase mb-1">
          QoS Traffic Control Mechanics (HTB Architecture)
        </AlertTitle>
        <AlertDescription className="text-xs leading-relaxed text-muted-foreground">
          ระบบนำหลักการ **HTB (Hierarchical Token Bucket)** ในระดับ Linux Kernel มาปรับความเร็วอินเทอร์เน็ต โดย Egress limit (Outgoing) จะถูกบีบที่ขาการ์ด LAN ขาออก (จำกัดความเร็วส่งออกของบอร์ด) ส่วน Ingress limit (Incoming) ในเฟส 2 จะถูก Redirect ผ่าน virtual interface **IFB** เพื่อจำกัดความเร็วแพ็กเก็ตที่วิ่งเข้าบอร์ด ทุกครั้งที่สร้าง/แก้ไขกฎ QoS ระบบจะจัดกลุ่มและ Sync ข้อมูลแบบ Idempotency เพื่อความถูกต้องของตาราง Kernel Scheduler
        </AlertDescription>
      </Alert>

      {/* 5. Create / Edit Modal Dialog */}
      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent className="max-w-[500px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingRule ? "แก้ไขกฎ QoS" : "เพิ่มกฎ QoS ใหม่"}
            </DialogTitle>
          </DialogHeader>

          {formError && (
            <Alert variant="destructive" className="py-2.5 rounded-lg">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription className="text-xs">{formError}</AlertDescription>
            </Alert>
          )}

          <form onSubmit={handleSaveRule} className="space-y-4 pt-1">
            {/* Rule Name */}
            <div className="space-y-1.5">
              <Label className="text-xs font-bold text-foreground">ชื่อกฎ QoS *</Label>
              <Input
                placeholder="เช่น Limit WiFi Guests, LAN PC-Admin"
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                className="h-9 text-xs border-border bg-card"
                required
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              {/* Interface Selector */}
              <div className="space-y-1.5">
                <Label className="text-xs font-bold text-foreground">อินเทอร์เฟซ *</Label>
                <select
                  value={formInterface}
                  onChange={(e) => setFormInterface(e.target.value)}
                  className="w-full h-9 rounded-lg border border-border bg-card px-3 text-xs outline-none focus:ring-1 focus:ring-primary text-foreground"
                >
                  {interfaces.map((iface) => (
                    <option key={iface.id} value={iface.name}>
                      {iface.name.toUpperCase()} ({iface.alias || iface.role})
                    </option>
                  ))}
                </select>
              </div>

              {/* Priority */}
              <div className="space-y-1.5">
                <Label className="text-xs font-bold text-foreground">ลำดับความสำคัญ (Priority) *</Label>
                <Input
                  type="number"
                  placeholder="10"
                  value={formPriority}
                  onChange={(e) => setFormPriority(e.target.value)}
                  className="h-9 text-xs border-border bg-card"
                  min="1"
                  required
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              {/* Source IP Match */}
              <div className="space-y-1.5">
                <Label className="text-xs font-bold text-foreground flex items-center gap-1">
                  Match Source IP (CIDR)
                  <Badge variant="outline" className="text-[9px] scale-90 py-0 text-muted-foreground border-border">
                    Optional
                  </Badge>
                </Label>
                <Input
                  placeholder="เช่น 192.168.1.0/24"
                  value={formMatchSrcIp}
                  onChange={(e) => setFormMatchSrcIp(e.target.value)}
                  className="h-9 text-xs font-mono border-border bg-card"
                />
              </div>

              {/* Destination IP Match */}
              <div className="space-y-1.5">
                <Label className="text-xs font-bold text-foreground flex items-center gap-1">
                  Match Destination IP (CIDR)
                  <Badge variant="outline" className="text-[9px] scale-90 py-0 text-muted-foreground border-border">
                    Optional
                  </Badge>
                </Label>
                <Input
                  placeholder="เช่น 8.8.8.8/32"
                  value={formMatchDstIp}
                  onChange={(e) => setFormMatchDstIp(e.target.value)}
                  className="h-9 text-xs font-mono border-border bg-card"
                />
              </div>
            </div>

            {/* QoS Bandwidth Limits */}
            <div className="space-y-3 p-3 bg-muted/20 border border-border/40 rounded-xl">
              <span className="text-[11px] font-bold text-muted-foreground uppercase tracking-wider block">
                Bandwidth Limits Config (Mbps)
              </span>

              {/* Egress limits (Outgoing) */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <Label className="text-[11px] text-muted-foreground flex items-center gap-1 font-semibold">
                    <ArrowUp className="h-3 w-3 text-emerald-500" />
                    Egress Rate (Mbps)
                  </Label>
                  <Input
                    type="number"
                    value={formEgressRate}
                    onChange={(e) => setFormEgressRate(e.target.value)}
                    className="h-8.5 text-xs border-border bg-card"
                    placeholder="0 = Unlimited"
                    min="0"
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-[11px] text-muted-foreground flex items-center gap-1 font-semibold">
                    Egress Ceil (Mbps)
                  </Label>
                  <Input
                    type="number"
                    value={formEgressCeil}
                    onChange={(e) => setFormEgressCeil(e.target.value)}
                    className="h-8.5 text-xs border-border bg-card"
                    placeholder="0 = Unlimited"
                    min="0"
                  />
                </div>
              </div>

              {/* Ingress limits (Incoming) */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1">
                  <Label className="text-[11px] text-muted-foreground flex items-center gap-1 font-semibold">
                    <ArrowDown className="h-3 w-3 text-amber-500" />
                    Ingress Rate (Mbps)
                  </Label>
                  <Input
                    type="number"
                    value={formIngressRate}
                    onChange={(e) => setFormIngressRate(e.target.value)}
                    className="h-8.5 text-xs border-border bg-card"
                    placeholder="0 = Unlimited"
                    min="0"
                  />
                </div>
                <div className="space-y-1">
                  <Label className="text-[11px] text-muted-foreground flex items-center gap-1 font-semibold">
                    Ingress Ceil (Mbps)
                  </Label>
                  <Input
                    type="number"
                    value={formIngressCeil}
                    onChange={(e) => setFormIngressCeil(e.target.value)}
                    className="h-8.5 text-xs border-border bg-card"
                    placeholder="0 = Unlimited"
                    min="0"
                  />
                </div>
              </div>
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <Label className="text-xs font-bold text-foreground">รายละเอียดเพิ่มเติม</Label>
              <Input
                placeholder="เช่น บีบความเร็ววง Guest ชั่วคราว"
                value={formDescription}
                onChange={(e) => setFormDescription(e.target.value)}
                className="h-9 text-xs border-border bg-card"
              />
            </div>

            {/* Status Trigger */}
            <div className="flex items-center justify-between py-2 border-y border-border/30">
              <div className="flex flex-col space-y-0.5">
                <span className="text-xs font-bold text-foreground">สถานะการทำงาน</span>
                <span className="text-[11px] text-muted-foreground">เริ่มบีบความเร็วของกฎนี้ทันที</span>
              </div>
              <Switch checked={formStatus} onCheckedChange={setFormStatus} />
            </div>

            {/* Form Actions */}
            <div className="flex justify-end gap-2.5 pt-2">
              <Button
                type="button"
                variant="outline"
                className="h-9 text-xs cursor-pointer border-border"
                onClick={() => setIsModalOpen(false)}
              >
                Cancel
              </Button>
              <Button
                type="submit"
                className="h-9 text-xs font-medium cursor-pointer"
                disabled={isSaving}
              >
                {isSaving && <Loader2 className="h-3 w-3 animate-spin mr-1.5" />}
                {editingRule ? "Save Changes" : "Create Rule"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}

function InfoIcon(props: React.SVGProps<SVGSVGElement>) {
  return (
    <svg
      {...props}
      xmlns="http://www.w3.org/2000/svg"
      width="24"
      height="24"
      viewBox="0 0 24 24"
      fill="none"
      stroke="currentColor"
      strokeWidth="2"
      strokeLinecap="round"
      strokeLinejoin="round"
    >
      <circle cx="12" cy="12" r="10" />
      <path d="M12 16v-4" />
      <path d="M12 8h.01" />
    </svg>
  )
}
