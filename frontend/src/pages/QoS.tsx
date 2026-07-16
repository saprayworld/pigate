import React, { useState, useEffect, useMemo } from "react"
import { getErrorMessage } from "@/lib/errors"
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
  Server,
  Info
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
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { Switch } from "@/components/ui/switch"
import { qosService, type QosRule, type QosIfaceStatus } from "@/services/qosService"
import { interfaceService } from "@/services/interfaceService"
import { type NetworkInterface } from "@/data-mockup/mockData"
import { useAlert } from "@/hooks/useAlert"
import { cn, isValidCidr } from "@/lib/utils"
import { ifaceLabel, formatIfaceLabel } from "@/lib/ifaceLabel"

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
    } catch (err) {
      showAlert("Error", getErrorMessage(err) || "Failed to load QoS data")
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
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
      } catch (err) {
        showAlert("Error", getErrorMessage(err) || "Failed to load QoS data")
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [showAlert])

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
    } catch (err) {
      setFormError(getErrorMessage(err) || "Failed to save QoS rule")
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
    } catch (err) {
      showAlert("Error", getErrorMessage(err) || "Failed to delete QoS rule")
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
    } catch (err) {
      showAlert("Error", getErrorMessage(err) || "Failed to toggle QoS status")
    }
  }

  const handleSyncToKernel = async () => {
    try {
      setIsSyncing(true)
      await qosService.sync()
      await loadData()
      showAlert("Success", "ซิงก์การตั้งค่า QoS ไปยังเคอร์เนลเรียบร้อยแล้ว")
    } catch (err) {
      showAlert("Error", getErrorMessage(err) || "Failed to sync QoS to kernel")
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
    } catch (err) {
      showAlert("Error", getErrorMessage(err) || "Failed to clear QoS on interface")
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

  // Ingress (IFB) capability: warn only when a fetched status explicitly reports
  // it unsupported. `=== false` keeps interfaces whose status failed to load or
  // is still pending from becoming a false positive (see qos-system.md).
  const ingressUnsupported = useMemo(
    () => Object.values(ifaceStatuses).some((s) => s.ingressSupported === false),
    [ifaceStatuses]
  )

  if (isLoading && rules.length === 0) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[400px] space-y-4">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="text-sm text-muted-foreground font-semibold">กำลังโหลดข้อมูลระบบ QoS...</span>
      </div>
    )
  }

  return (
    <div className="space-y-4">
      {/* 0. IFB ingress capability warning */}
      {ingressUnsupported && (
        <Alert className="border-warning/30 bg-warning/10 px-3 py-2.5 text-warning">
          <AlertCircle className="h-4 w-4 text-warning" />
          <AlertDescription className="text-warning">
            <span className="font-semibold">Kernel นี้ไม่มี IFB module</span> — รองรับเฉพาะ QoS ขา
            Egress (download) เท่านั้น, กฎ Ingress (upload) จะถูกข้ามและไม่ถูก apply จริง
          </AlertDescription>
        </Alert>
      )}

      {/* 1. Per-interface QoS status cards */}
      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 lg:grid-cols-3">
        {interfaces.map((iface) => {
          const status = ifaceStatuses[iface.name]
          const isQosActive = status?.hasQdisc

          return (
            <Card key={iface.id} size="sm" className="gap-3">
              <CardHeader className="flex flex-row items-start justify-between gap-2 space-y-0">
                <div className="space-y-1">
                  <CardTitle className="flex items-center gap-2 text-sm font-medium">
                    <Network className="h-4 w-4 shrink-0 text-muted-foreground" />
                    <span className="font-mono text-foreground">{ifaceLabel(iface)}</span>
                    <Badge variant="outline" className="rounded px-1.5 py-0 text-[10px] font-normal uppercase text-muted-foreground">
                      {iface.role}
                    </Badge>
                  </CardTitle>
                  <CardDescription className="max-w-[180px] truncate text-[11px]">
                    {iface.type}
                  </CardDescription>
                </div>
                <Badge
                  variant="outline"
                  className={cn(
                    "rounded px-2 py-0.5 text-[10px] font-semibold",
                    isQosActive
                      ? "border-primary/20 bg-primary/10 text-primary"
                      : "border-border bg-muted text-muted-foreground"
                  )}
                >
                  {isQosActive ? "QoS Active" : "No Shaping"}
                </Badge>
              </CardHeader>

              <CardContent className="space-y-3">
                {isQosActive ? (
                  <div className="space-y-2">
                    <span className="block text-xs font-medium text-muted-foreground">
                      Active Kernel Classes
                    </span>
                    <div className="space-y-1 text-xs">
                      {status.classes.map((cls) => {
                        const isIngress = cls.classId.toLowerCase().includes("ingress");
                        return (
                          <div
                            key={cls.classId}
                            className="flex items-center justify-between rounded-lg border border-border bg-muted/50 px-2.5 py-1.5"
                          >
                            <div className="flex items-center gap-1.5 font-medium text-foreground">
                              <span className="rounded bg-muted px-1 font-mono text-[10px] text-muted-foreground">
                                {cls.classId}
                              </span>
                              <span className="max-w-[100px] truncate">{cls.ruleName || "Shared Limit"}</span>
                            </div>
                            <span className={cn(
                              "flex items-center gap-0.5 text-xs font-semibold",
                              isIngress ? "text-warning" : "text-primary"
                            )}>
                              {isIngress ? <ArrowDown className="h-3.5 w-3.5" /> : <ArrowUp className="h-3.5 w-3.5" />}
                              {cls.ceil}
                            </span>
                          </div>
                        );
                      })}
                    </div>
                  </div>
                ) : (
                  <div className="flex items-center gap-2 py-2 text-xs text-muted-foreground">
                    <Info className="h-4 w-4 shrink-0 text-muted-foreground/60" />
                    <span>แบนด์วิธวิ่งตามความเร็วการ์ดแลนปกติ (Unshaped)</span>
                  </div>
                )}

                <div className="flex justify-end border-t border-border/50 pt-2">
                  <Button
                    variant="ghost"
                    size="sm"
                    className="h-7 cursor-pointer px-2.5 text-[11px] text-destructive hover:bg-destructive/10 hover:text-destructive disabled:opacity-30"
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

      {/* 2. QoS Rules List Card */}
      <Card>
        <CardHeader className="flex flex-col gap-4 space-y-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <Sliders className="h-4 w-4 text-muted-foreground" />
              QoS Rules Database
              <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                {rules.length}
              </Badge>
            </CardTitle>
            <CardDescription className="text-xs">
              ตารางกำหนดความสำคัญของความเร็วตามวงไอพีต้นทาง ปลายทาง และอินเทอร์เฟซ
            </CardDescription>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            {/* Search Input */}
            <div className="relative w-full sm:w-[200px]">
              <Search className="pointer-events-none absolute top-2 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                placeholder="ค้นหา QoS rule..."
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                className="h-8 pl-8 text-xs"
              />
            </div>
            <Button
              variant="outline"
              size="sm"
              className="cursor-pointer gap-2"
              onClick={loadData}
            >
              <RefreshCw className="h-4 w-4" />
              Reload
            </Button>
            <Button
              variant="outline"
              size="sm"
              className="cursor-pointer gap-2"
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
            {/* Add Rule Button */}
            <Button
              size="sm"
              className="cursor-pointer gap-1.5 font-semibold"
              onClick={handleOpenCreateModal}
            >
              <Plus className="h-4 w-4" />
              Add QoS Rule
            </Button>
          </div>
        </CardHeader>

        <CardContent>
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[100px] text-xs font-medium text-muted-foreground">Status</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Rule Name</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Interface</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Match Src IP</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Match Dst IP</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Egress Limit (Outgoing)</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Ingress Limit (Incoming)</TableHead>
                <TableHead className="w-[80px] text-center text-xs font-medium text-muted-foreground">Priority</TableHead>
                <TableHead className="w-[120px] text-center text-xs font-medium text-muted-foreground">Actions</TableHead>
              </TableRow>
            </TableHeader>

            <TableBody>
              {filteredRules.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={9} className="h-28 text-center text-muted-foreground">
                    <div className="flex flex-col items-center justify-center gap-2">
                      <AlertCircle className="h-8 w-8 text-muted-foreground/60" />
                      <span className="text-sm font-medium">ไม่พบข้อมูลกฎ QoS</span>
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
                    className={cn(!rule.status && "opacity-60")}
                  >
                    <TableCell className="py-3">
                      <Switch
                        checked={rule.status}
                        onCheckedChange={() => handleToggleRule(rule)}
                      />
                    </TableCell>

                    <TableCell className="py-3 font-medium text-foreground">
                      <div className="flex flex-col">
                        <span>{rule.name}</span>
                        {rule.description && (
                          <span className="max-w-[200px] truncate text-[11px] font-normal text-muted-foreground">
                            {rule.description}
                          </span>
                        )}
                      </div>
                    </TableCell>

                    <TableCell className="py-3">
                      <Badge variant="secondary" className="rounded px-2 py-0.5 font-mono text-xs">
                        {formatIfaceLabel(rule.interface, interfaces)}
                      </Badge>
                    </TableCell>

                    <TableCell className="py-3 font-mono text-xs">
                      {rule.matchSrcIp || <span className="italic text-muted-foreground">Match All</span>}
                    </TableCell>

                    <TableCell className="py-3 font-mono text-xs">
                      {rule.matchDstIp || <span className="italic text-muted-foreground">Match All</span>}
                    </TableCell>

                    <TableCell className="py-3">
                      {rule.egressRateMbps > 0 ? (
                        <div className="flex items-center gap-1.5 text-xs font-semibold text-primary">
                          <ArrowUp className="h-3.5 w-3.5" />
                          <span>{rule.egressRateMbps} Mbps</span>
                          {rule.egressCeilMbps > rule.egressRateMbps && (
                            <span className="text-[10px] font-normal text-muted-foreground">
                              (Max {rule.egressCeilMbps})
                            </span>
                          )}
                        </div>
                      ) : (
                        <Badge variant="outline" className="rounded px-1.5 py-0 text-[10px] font-normal text-muted-foreground">
                          Unlimited
                        </Badge>
                      )}
                    </TableCell>

                    <TableCell className="py-3">
                      {rule.ingressRateMbps > 0 ? (
                        <div className="flex items-center gap-1.5 text-xs font-semibold text-warning">
                          <ArrowDown className="h-3.5 w-3.5" />
                          <span className={cn(ingressUnsupported && "line-through opacity-60")}>
                            {rule.ingressRateMbps} Mbps
                          </span>
                          {rule.ingressCeilMbps > rule.ingressRateMbps && (
                            <span className="text-[10px] font-normal text-muted-foreground">
                              (Max {rule.ingressCeilMbps})
                            </span>
                          )}
                          {ingressUnsupported && (
                            <AlertCircle
                              className="h-3.5 w-3.5"
                              aria-label="Ingress ไม่ถูก apply: kernel ไม่มี IFB module"
                            />
                          )}
                        </div>
                      ) : (
                        <Badge variant="outline" className="rounded px-1.5 py-0 text-[10px] font-normal text-muted-foreground">
                          Unlimited
                        </Badge>
                      )}
                    </TableCell>

                    <TableCell className="py-3 text-center font-mono text-xs text-foreground">
                      {rule.priority}
                    </TableCell>

                    <TableCell className="py-3 text-center">
                      <div className="flex items-center justify-center gap-2">
                        <Button
                          variant="outline"
                          size="icon-sm"
                          className="cursor-pointer text-muted-foreground hover:text-foreground"
                          onClick={() => handleOpenEditModal(rule)}
                          title="แก้ไขกฎ QoS"
                        >
                          <Edit className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                          onClick={() => handleDeleteRule(rule)}
                          title="ลบกฎ QoS"
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
        </CardContent>
      </Card>

      {/* 3. Info note */}
      <div className="flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
        <Server className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          <strong className="text-foreground">QoS Traffic Control Mechanics (HTB Architecture):</strong>{" "}
          ระบบนำหลักการ <strong className="text-foreground">HTB (Hierarchical Token Bucket)</strong> ในระดับ Linux Kernel มาปรับความเร็วอินเทอร์เน็ต โดย Egress limit (Outgoing) จะถูกบีบที่ขาการ์ด LAN ขาออก (จำกัดความเร็วส่งออกของบอร์ด) ส่วน Ingress limit (Incoming) ในเฟส 2 จะถูก Redirect ผ่าน virtual interface <strong className="text-foreground">IFB</strong> เพื่อจำกัดความเร็วแพ็กเก็ตที่วิ่งเข้าบอร์ด ทุกครั้งที่สร้าง/แก้ไขกฎ QoS ระบบจะจัดกลุ่มและ Sync ข้อมูลแบบ Idempotency เพื่อความถูกต้องของตาราง Kernel Scheduler
        </span>
      </div>

      {/* 4. Create / Edit Modal Dialog */}
      <Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingRule ? "แก้ไขกฎ QoS" : "เพิ่มกฎ QoS ใหม่"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
          {formError && (
            <Alert variant="destructive" className="px-3 py-2.5">
              <AlertCircle className="h-4 w-4" />
              <AlertDescription className="text-xs">{formError}</AlertDescription>
            </Alert>
          )}

          <form onSubmit={handleSaveRule} className="space-y-4">
            {/* Rule Name */}
            <div className="space-y-1.5">
              <Label htmlFor="qos-name" className="block text-xs font-medium text-muted-foreground">
                ชื่อกฎ QoS <span className="text-destructive">*</span>
              </Label>
              <Input
                id="qos-name"
                placeholder="เช่น Limit WiFi Guests, LAN PC-Admin"
                value={formName}
                onChange={(e) => setFormName(e.target.value)}
                className="h-9 text-sm"
                required
              />
            </div>

            <div className="grid grid-cols-2 gap-4">
              {/* Interface Selector */}
              <div className="space-y-1.5">
                <Label htmlFor="qos-iface" className="block text-xs font-medium text-muted-foreground">
                  อินเทอร์เฟซ <span className="text-destructive">*</span>
                </Label>
                <select
                  id="qos-iface"
                  value={formInterface}
                  onChange={(e) => setFormInterface(e.target.value)}
                  className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                >
                  {interfaces.map((iface) => (
                    <option key={iface.id} value={iface.name}>
                      {ifaceLabel(iface)}
                    </option>
                  ))}
                </select>
              </div>

              {/* Priority */}
              <div className="space-y-1.5">
                <Label htmlFor="qos-priority" className="block text-xs font-medium text-muted-foreground">
                  ลำดับความสำคัญ (Priority) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="qos-priority"
                  type="number"
                  placeholder="10"
                  value={formPriority}
                  onChange={(e) => setFormPriority(e.target.value)}
                  className="h-9 font-mono text-sm"
                  min="1"
                  required
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              {/* Source IP Match */}
              <div className="space-y-1.5">
                <Label htmlFor="qos-src" className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
                  Match Source IP (CIDR)
                  <Badge variant="outline" className="rounded px-1 py-0 text-[9px] font-normal text-muted-foreground">
                    Optional
                  </Badge>
                </Label>
                <Input
                  id="qos-src"
                  placeholder="เช่น 192.168.1.0/24"
                  value={formMatchSrcIp}
                  onChange={(e) => setFormMatchSrcIp(e.target.value)}
                  className="h-9 font-mono text-sm"
                />
              </div>

              {/* Destination IP Match */}
              <div className="space-y-1.5">
                <Label htmlFor="qos-dst" className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
                  Match Destination IP (CIDR)
                  <Badge variant="outline" className="rounded px-1 py-0 text-[9px] font-normal text-muted-foreground">
                    Optional
                  </Badge>
                </Label>
                <Input
                  id="qos-dst"
                  placeholder="เช่น 8.8.8.8/32"
                  value={formMatchDstIp}
                  onChange={(e) => setFormMatchDstIp(e.target.value)}
                  className="h-9 font-mono text-sm"
                />
              </div>
            </div>

            {/* QoS Bandwidth Limits */}
            <div className="space-y-3 rounded-lg border border-border bg-muted/50 p-4">
              <div className="flex items-center gap-1.5 text-xs font-semibold text-foreground">
                <Sliders className="h-3.5 w-3.5 text-muted-foreground" /> Bandwidth Limits Config (Mbps)
              </div>

              {/* Egress limits (Outgoing) */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label htmlFor="qos-egress-rate" className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
                    <ArrowUp className="h-3 w-3 text-primary" />
                    Egress Rate (Mbps)
                  </Label>
                  <Input
                    id="qos-egress-rate"
                    type="number"
                    value={formEgressRate}
                    onChange={(e) => setFormEgressRate(e.target.value)}
                    className="h-9 bg-background font-mono text-sm"
                    placeholder="0 = Unlimited"
                    min="0"
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="qos-egress-ceil" className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
                    Egress Ceil (Mbps)
                  </Label>
                  <Input
                    id="qos-egress-ceil"
                    type="number"
                    value={formEgressCeil}
                    onChange={(e) => setFormEgressCeil(e.target.value)}
                    className="h-9 bg-background font-mono text-sm"
                    placeholder="0 = Unlimited"
                    min="0"
                  />
                </div>
              </div>

              {/* Ingress limits (Incoming) */}
              <div className="grid grid-cols-2 gap-4">
                <div className="space-y-1.5">
                  <Label htmlFor="qos-ingress-rate" className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
                    <ArrowDown className="h-3 w-3 text-warning" />
                    Ingress Rate (Mbps)
                  </Label>
                  <Input
                    id="qos-ingress-rate"
                    type="number"
                    value={formIngressRate}
                    onChange={(e) => setFormIngressRate(e.target.value)}
                    className="h-9 bg-background font-mono text-sm"
                    placeholder="0 = Unlimited"
                    min="0"
                    disabled={ingressUnsupported}
                  />
                </div>
                <div className="space-y-1.5">
                  <Label htmlFor="qos-ingress-ceil" className="flex items-center gap-1 text-xs font-medium text-muted-foreground">
                    Ingress Ceil (Mbps)
                  </Label>
                  <Input
                    id="qos-ingress-ceil"
                    type="number"
                    value={formIngressCeil}
                    onChange={(e) => setFormIngressCeil(e.target.value)}
                    className="h-9 bg-background font-mono text-sm"
                    placeholder="0 = Unlimited"
                    min="0"
                    disabled={ingressUnsupported}
                  />
                </div>
              </div>

              {ingressUnsupported && (
                <p className="flex items-start gap-1.5 text-[11px] leading-relaxed text-warning">
                  <AlertCircle className="mt-0.5 h-3 w-3 shrink-0" />
                  Kernel นี้ไม่มี IFB module — ตั้งค่า Ingress ไม่ได้ ค่าที่มีอยู่เดิมของกฎยังถูกเก็บไว้แต่จะไม่ถูก apply
                </p>
              )}
            </div>

            {/* Description */}
            <div className="space-y-1.5">
              <Label htmlFor="qos-desc" className="block text-xs font-medium text-muted-foreground">
                รายละเอียดเพิ่มเติม
              </Label>
              <Input
                id="qos-desc"
                placeholder="เช่น บีบความเร็ววง Guest ชั่วคราว"
                value={formDescription}
                onChange={(e) => setFormDescription(e.target.value)}
                className="h-9 text-sm"
              />
            </div>

            {/* Status Trigger */}
            <div className="flex items-center justify-between rounded-lg border border-border bg-muted/50 p-3">
              <div className="flex flex-col gap-0.5">
                <span className="text-xs font-semibold text-foreground">สถานะการทำงาน</span>
                <span className="text-[10px] text-muted-foreground">เริ่มบีบความเร็วของกฎนี้ทันที</span>
              </div>
              <Switch checked={formStatus} onCheckedChange={setFormStatus} />
            </div>

            {/* Form Actions */}
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
                disabled={isSaving}
              >
                {isSaving && <Loader2 className="mr-1.5 h-3 w-3 animate-spin" />}
                {editingRule ? "Save Changes" : "Create Rule"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
