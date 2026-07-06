import { useState, useMemo, useRef, useEffect, useCallback } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Route,
  Plus,
  Search,
  Edit,
  Trash2,
  AlertCircle,
  Network,
  RefreshCw,
  CheckCircle2,
  SlidersHorizontal,
  Info,
  Loader2,
  ChevronDown,
  ChevronUp
} from "lucide-react"
import { Card } from "@/components/ui/card"
import { Badge } from "@/components/ui/badge"
import { Button } from "@/components/ui/button"
import { Switch } from "@/components/ui/switch"
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
import { type StaticRoute, type NetworkInterface } from "@/data-mockup/mockData"
import { staticRouteService } from "@/services/staticRouteService"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/hooks/useAlert"
import { isValidIp, isValidCidr } from "@/lib/utils"

export default function StaticRoutes() {
  const { alert, confirm } = useAlert()

  // --- State ---
  const [routes, setRoutes] = useState<StaticRoute[]>([])
  const [interfaces, setInterfaces] = useState<NetworkInterface[]>([])
  const [allowEditSystemRoutes, setAllowEditSystemRoutes] = useState(false)
  const [enableEditSystemRoute, setEnableEditSystemRoute] = useState(false)
  const [uiEditSystemRouteActive, setUiEditSystemRouteActive] = useState(false)
  const [isLoading, setIsLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState("")
  const [selectedTypeFilter, setSelectedTypeFilter] = useState<"all" | "system" | "custom">("all")
  const [selectedStatusFilter, setSelectedStatusFilter] = useState<"all" | "active" | "inactive">("all")

  // Selection state for checkboxes (custom routes only)
  const [selectedIds, setSelectedIds] = useState<string[]>([])

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingRoute, setEditingRoute] = useState<StaticRoute | null>(null)

  // Form fields
  const [formDestination, setFormDestination] = useState("")
  const [formGateway, setFormGateway] = useState("")
  const [formInterface, setFormInterface] = useState("eth0")
  const [formMetric, setFormMetric] = useState("100")
  const [formDescription, setFormDescription] = useState("")
  const [formStatus, setFormStatus] = useState(true)
  const [formError, setFormError] = useState("")

  // Advanced fields
  const [formScope, setFormScope] = useState("global")
  const [formSrc, setFormSrc] = useState("")
  const [formProto, setFormProto] = useState("static")
  const [isAdvancedOpen, setIsAdvancedOpen] = useState(false)

  const dialogContentRef = useRef<HTMLDivElement | null>(null)

  // Fetch logic
  const loadRoutes = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const [routesData, configData, interfacesData] = await Promise.all([
        staticRouteService.getAll(),
        staticRouteService.getConfig(),
        interfaceService.getAll()
      ])
      setRoutes(routesData)
      setAllowEditSystemRoutes(configData.allowEditSystemRoutes)
      setEnableEditSystemRoute(configData.enableEditSystemRoute)
      setInterfaces(interfacesData)
    } catch (err) {
      console.error(err)
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดตารางเส้นทางได้: " + getErrorMessage(err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
        const [routesData, configData, interfacesData] = await Promise.all([
          staticRouteService.getAll(),
          staticRouteService.getConfig(),
          interfaceService.getAll()
        ])
        setRoutes(routesData)
        setAllowEditSystemRoutes(configData.allowEditSystemRoutes)
        setEnableEditSystemRoute(configData.enableEditSystemRoute)
        setInterfaces(interfacesData)
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดตารางเส้นทางได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

  const handleToggleEditSystemMode = async (checked: boolean) => {
    if (checked) {
      const confirmed = await confirm(
        "คำเตือนความปลอดภัย",
        "คุณแน่ใจหรือไม่ที่จะเปิดโหมดแก้ไขเส้นทางระบบปฏิบัติการ (System Route)? การกระทำนี้จำเป็นต้องทำด้วยความระมัดระวังอย่างยิ่ง เนื่องจากอาจส่งผลกระทบต่อการเชื่อมต่ออินเทอร์เน็ตและบริการเครือข่ายทั้งหมดของระบบ"
      )
      if (confirmed) {
        setUiEditSystemRouteActive(true)
      } else {
        setUiEditSystemRouteActive(false)
      }
    } else {
      setUiEditSystemRouteActive(false)
    }
  }

  // --- Statistics ---
  const stats = useMemo(() => {
    const total = routes.length
    const active = routes.filter(r => r.status).length
    const system = routes.filter(r => r.type === "system" || r.type === "defaultgateway").length
    const custom = routes.filter(r => r.type === "custom" || r.type === "customgateway").length
    return { total, active, system, custom }
  }, [routes])

  // --- Filtered Routes ---
  const filteredRoutes = useMemo(() => {
    return routes.filter(route => {
      const matchSearch =
        route.destination.toLowerCase().includes(searchQuery.toLowerCase()) ||
        route.gateway.toLowerCase().includes(searchQuery.toLowerCase()) ||
        route.interface.toLowerCase().includes(searchQuery.toLowerCase()) ||
        route.description.toLowerCase().includes(searchQuery.toLowerCase())

      const matchType =
        selectedTypeFilter === "all" ||
        route.type === selectedTypeFilter ||
        (selectedTypeFilter === "system" && route.type === "defaultgateway") ||
        (selectedTypeFilter === "custom" && route.type === "customgateway")

      const matchStatus =
        selectedStatusFilter === "all" ||
        (selectedStatusFilter === "active" && route.status) ||
        (selectedStatusFilter === "inactive" && !route.status)

      return matchSearch && matchType && matchStatus
    })
  }, [routes, searchQuery, selectedTypeFilter, selectedStatusFilter])

  const isRouteActionDisabled = useCallback((route: StaticRoute) => {
    if (uiEditSystemRouteActive) return false
    if (route.kernelOnly) return true
    if (route.type === "system" && !allowEditSystemRoutes) return true
    return false
  }, [uiEditSystemRouteActive, allowEditSystemRoutes])

  const getEditTitle = (route: StaticRoute) => {
    if (uiEditSystemRouteActive) {
      if (route.kernelOnly || route.type === "system") {
        return "แก้ไขเส้นทางระบบ (แก้ไขในเคอร์เนลโดยตรง)"
      }
      return "แก้ไขเส้นทาง"
    }
    if (route.kernelOnly) return "เส้นทางระดับระบบปฏิบัติการเท่านั้น ไม่สามารถแก้ไขได้"
    if (route.type === "system" && !allowEditSystemRoutes) return "ไม่สามารถแก้ไขข้อมูลระบบได้"
    return "แก้ไขเส้นทาง"
  }

  const getDeleteTitle = (route: StaticRoute) => {
    if (uiEditSystemRouteActive) {
      if (route.kernelOnly || route.type === "system") {
        return "ลบเส้นทางระบบ (ลบออกจากเคอร์เนลโดยตรง)"
      }
      return "ลบเส้นทาง"
    }
    if (route.kernelOnly) return "เส้นทางระดับระบบปฏิบัติการเท่านั้น ไม่สามารถลบได้"
    if (route.type === "system" && !allowEditSystemRoutes) return "ไม่สามารถลบข้อมูลระบบได้"
    return "ลบเส้นทาง"
  }

  // --- Checkbox Actions (Only Custom / Default Gateway Routes or all routes if system route editing is allowed) ---
  const selectableRoutes = useMemo(() => {
    return filteredRoutes.filter(r => !isRouteActionDisabled(r))
  }, [filteredRoutes, isRouteActionDisabled])

  const handleSelectAll = (checked: boolean) => {
    if (checked) {
      setSelectedIds(selectableRoutes.map(r => r.id))
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
    if (selectableRoutes.length === 0) return false
    return selectableRoutes.every(r => selectedIds.includes(r.id))
  }, [selectableRoutes, selectedIds])

  // --- Toggle individual route status ---
  const handleToggleStatus = async (id: string) => {
    try {
      await staticRouteService.toggleStatus(id)
      await loadRoutes(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปลี่ยนสถานะเส้นทางได้: " + getErrorMessage(err))
    }
  }

  // --- CRUD Actions ---
  const openCreateModal = () => {
    setEditingRoute(null)
    setFormDestination("")
    setFormGateway("")
    setFormInterface(interfaces[0]?.name || "eth0")
    setFormMetric("100")
    setFormDescription("")
    setFormStatus(true)
    setFormError("")
    setFormScope("global")
    setFormSrc("")
    setFormProto("120")
    setIsAdvancedOpen(false)
    setIsModalOpen(true)
  }

  const openEditModal = (route: StaticRoute) => {
    if (route.type === "defaultgateway" && !uiEditSystemRouteActive) {
      alert(
        "แก้ไขเกตเวย์หลัก",
        "เส้นทางเกตเวย์หลัก (Default Gateway) ถูกจัดการโดยอัตโนมัติผ่านการตั้งค่าการ์ดเครือข่าย กรุณาไปแก้ไขที่หน้าตั้งค่าพอร์ตเชื่อมต่อ (Interfaces) แทน"
      )
      return
    }
    setEditingRoute(route)
    setFormDestination(route.destination)
    setFormGateway(route.gateway)
    setFormInterface(route.interface)
    setFormMetric(route.metric.toString())
    setFormDescription(route.description)
    setFormStatus(route.status)
    setFormError("")
    setFormScope(route.scope || "global")
    setFormSrc(route.src || "")
    setFormProto(route.proto || "120")
    setIsAdvancedOpen(!!(
      (route.scope && route.scope !== "global") ||
      route.src ||
      (route.proto && route.proto !== "static" && route.proto !== "120")
    ))
    setIsModalOpen(true)
  }

  const handleDelete = async (id: string, dest: string) => {
    const route = routes.find(r => r.id === id)
    if (route && isRouteActionDisabled(route)) {
      await alert("การดำเนินการล้มเหลว", "ไม่สามารถลบ System Route ของระบบปฏิบัติการได้")
      return
    }

    if (await confirm("ยืนยันการลบ", `คุณต้องการลบเส้นทางไปยัง "${dest}" ใช่หรือไม่?`)) {
      try {
        await staticRouteService.delete(id)
        setSelectedIds(prev => prev.filter(item => item !== id))
        await loadRoutes(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบเส้นทางได้: " + getErrorMessage(err))
      }
    }
  }

  const handleBulkDelete = async () => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบเส้นทางที่เลือกจำนวน ${selectedIds.length} รายการใช่หรือไม่?`)) {
      try {
        await staticRouteService.deleteMultiple(selectedIds)
        setSelectedIds([])
        await loadRoutes(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบเส้นทางได้: " + getErrorMessage(err))
      }
    }
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    const dest = formDestination.trim()
    const gw = formGateway.trim()
    const metricVal = parseInt(formMetric, 10)
    const scope = formScope.trim()
    const src = formSrc.trim()
    const proto = formProto.trim()

    // 1. Validation Destination CIDR format (must be valid CIDR, or 0.0.0.0/0 for default route)
    if (dest !== "0.0.0.0/0" && !isValidCidr(dest)) {
      setFormError("รูปแบบ Destination Network ไม่ถูกต้อง (เช่น 192.168.10.0/24 หรือ 0.0.0.0/0 สำหรับ Default) และค่า Octet ต้องอยู่ในช่วง 0-255")
      return
    }

    // 2. Validation Gateway IP format (if provided)
    if (gw && !isValidIp(gw)) {
      setFormError("รูปแบบ Gateway IP Address ไม่ถูกต้อง (ต้องเป็น IP เช่น 192.168.1.254) และค่า Octet ต้องอยู่ในช่วง 0-255")
      return
    }

    // 3. Validation Metric
    if (isNaN(metricVal) || metricVal < 0) {
      setFormError("ค่า Metric ต้องเป็นตัวเลขจำนวนเต็มตั้งแต่ 0 ขึ้นไป")
      return
    }

    // 4. Preferred Source IP Validation
    if (src && !isValidIp(src)) {
      setFormError("รูปแบบ Preferred Source IP Address (src) ไม่ถูกต้อง")
      return
    }

    // 5. Duplicate Check
    const isDuplicate = routes.some(
      r => r.destination === dest &&
        r.metric === metricVal &&
        (!editingRoute || r.id !== editingRoute.id)
    )
    if (isDuplicate) {
      setFormError(`มีเส้นทางเครือข่าย "${dest}" ที่มี Metric ${metricVal} อยู่แล้ว`)
      return
    }

    try {
      if (editingRoute) {
        // Edit
        await staticRouteService.update(editingRoute.id, {
          destination: dest,
          gateway: gw,
          interface: formInterface,
          metric: metricVal,
          description: formDescription,
          status: formStatus,
          scope: scope,
          src: src,
          proto: proto
        })
      } else {
        // Create
        await staticRouteService.create({
          destination: dest,
          gateway: gw,
          interface: formInterface,
          metric: metricVal,
          description: formDescription,
          status: formStatus,
          scope: scope,
          src: src,
          proto: proto
        })
      }
      await loadRoutes(false)
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
            <Route className="h-7 w-7 text-primary fill-primary/10" />
            Static Routes
          </h1>
          <p className="text-muted-foreground mt-1">
            กำหนดค่าตารางการกำหนดเส้นทางเพื่อนำส่งข้อมูลออกสู่เครือข่ายย่อยต่าง ๆ
          </p>
        </div>
        <div className="flex items-center gap-3">
          {enableEditSystemRoute && (
            <div className="flex items-center gap-2 bg-rose-500/10 border border-rose-500/20 px-3 h-10 rounded-lg text-xs font-semibold select-none">
              <span className="text-rose-400">แก้ไขเส้นทางระบบ</span>
              <Switch
                checked={uiEditSystemRouteActive}
                onCheckedChange={handleToggleEditSystemMode}
                size="sm"
              />
            </div>
          )}
          <Button
            onClick={() => loadRoutes(true)}
            disabled={isLoading}
            variant="outline"
            className="cursor-pointer border-border hover:bg-muted text-foreground font-bold gap-1.5 h-10 px-4"
          >
            {isLoading ? (
              <Loader2 className="h-4 w-4 animate-spin" />
            ) : (
              <RefreshCw className="h-4.5 w-4.5" />
            )}
            Refresh
          </Button>
          <Button onClick={openCreateModal} className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1.5 h-10">
            <Plus className="h-4.5 w-4.5" />
            Create Static Route
          </Button>
        </div>
      </div>

      {/* 2. Stats Dashboard Cards */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">เส้นทางจัดเก็บทั้งหมด</div>
          <div className="mt-2 text-2xl font-bold text-foreground font-mono">{stats.total}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <CheckCircle2 className="h-3.5 w-3.5 text-primary" /> Active Routes (ใช้งาน)
          </div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.active}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <Network className="h-3.5 w-3.5 text-primary" /> System Subnets
          </div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.system}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground flex items-center gap-1.5">
            <SlidersHorizontal className="h-3.5 w-3.5 text-amber-400" /> Custom Config
          </div>
          <div className="mt-2 text-2xl font-bold text-amber-400 font-mono">{stats.custom}</div>
        </Card>
      </div>



      {/* 4. Toolbar (Filters & Search) */}
      <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between bg-card/30 p-4 rounded-xl border border-border/60">
        <div className="flex flex-wrap items-center gap-2.5">
          {/* Type filters */}
          <div className="flex rounded-lg border border-border bg-card p-0.5 gap-0.5">
            <button
              onClick={() => setSelectedTypeFilter("all")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition cursor-pointer ${selectedTypeFilter === "all"
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              All Types
            </button>
            <button
              onClick={() => setSelectedTypeFilter("system")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition cursor-pointer ${selectedTypeFilter === "system"
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              System
            </button>
            <button
              onClick={() => setSelectedTypeFilter("custom")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition cursor-pointer ${selectedTypeFilter === "custom"
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              Custom
            </button>
          </div>

          {/* Status filters */}
          <div className="flex rounded-lg border border-border bg-card p-0.5 gap-0.5">
            <button
              onClick={() => setSelectedStatusFilter("all")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition cursor-pointer ${selectedStatusFilter === "all"
                ? "bg-primary/20 text-primary border border-primary/20"
                : "text-muted-foreground hover:text-foreground hover:bg-muted border border-transparent"
                }`}
            >
              All Status
            </button>
            <button
              onClick={() => setSelectedStatusFilter("active")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition cursor-pointer ${selectedStatusFilter === "active"
                ? "bg-primary text-primary-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              Active Only
            </button>
            <button
              onClick={() => setSelectedStatusFilter("inactive")}
              className={`px-3 py-1 text-xs font-bold rounded-md transition cursor-pointer ${selectedStatusFilter === "inactive"
                ? "bg-muted text-foreground"
                : "text-muted-foreground hover:text-foreground hover:bg-muted"
                }`}
            >
              Inactive
            </button>
          </div>

          {/* Bulk Action */}
          {selectedIds.length > 0 && (
            <Button
              variant="destructive"
              size="sm"
              onClick={handleBulkDelete}
              className="cursor-pointer font-bold gap-1 h-8 px-3 ml-2"
            >
              <Trash2 className="h-3.5 w-3.5" />
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
            placeholder="ค้นหา IP เครือข่าย, Gateway หรือคำอธิบาย..."
            className="pl-8 bg-background/50 placeholder:text-muted-foreground h-9"
          />
        </div>
      </div>

      {/* 5. Table view */}
      <Card className="bg-card/25 border border-border/50 overflow-hidden py-0">
        <Table>
          <TableHeader>
            <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
              <TableHead className="p-3 w-[5%]">
                <input
                  type="checkbox"
                  disabled={selectableRoutes.length === 0}
                  checked={isAllSelected}
                  onChange={(e) => handleSelectAll(e.target.checked)}
                  className="rounded border-input bg-background text-primary focus:ring-primary h-3.5 w-3.5 cursor-pointer accent-primary disabled:opacity-30 disabled:cursor-not-allowed"
                />
              </TableHead>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[22%] font-semibold">Destination Network</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[20%] font-semibold">Gateway</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[12%] font-semibold">Interface</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[10%] font-semibold">Metric</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[18%] font-semibold">Description</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider w-[8%] font-semibold">Status</th>
              <TableHead className="p-3 w-[5%] text-right"></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={8} className="p-12 text-center text-muted-foreground text-xs">
                  <div className="flex flex-col items-center justify-center gap-2 py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-primary" />
                    <span>กำลังโหลดข้อมูล...</span>
                  </div>
                </TableCell>
              </TableRow>
            ) : filteredRoutes.length === 0 ? (
              <TableRow>
                <TableCell colSpan={8} className="p-8 text-center text-muted-foreground text-xs">
                  ไม่พบเส้นทางเครือข่ายตามที่ค้นหา
                </TableCell>
              </TableRow>
            ) : (
              filteredRoutes.map((route) => (
                <TableRow key={route.id} className="border-b border-border/40 hover:bg-muted/15">
                  <TableCell className="p-3">
                    <input
                      type="checkbox"
                      disabled={isRouteActionDisabled(route)}
                      checked={selectedIds.includes(route.id)}
                      onChange={(e) => handleSelectRow(route.id, e.target.checked)}
                      className="rounded border-input bg-background text-primary focus:ring-primary h-3.5 w-3.5 cursor-pointer accent-primary disabled:opacity-30 disabled:cursor-not-allowed"
                    />
                  </TableCell>
                  <TableCell className="p-3">
                    <div className="flex flex-col gap-1">
                      <span className="font-mono text-sm font-semibold text-foreground">{route.destination}</span>
                      <div className="flex items-center gap-1.5">
                        {route.type === "system" ? (
                          <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[9px] px-1.5 py-0.2 rounded font-mono font-medium">
                            System
                          </Badge>
                        ) : route.type === "defaultgateway" ? (
                          <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[9px] px-1.5 py-0.2 rounded font-mono font-medium">
                            Default Gateway
                          </Badge>
                        ) : route.type === "customgateway" ? (
                          <Badge variant="outline" className="bg-amber-500/10 text-amber-400 border-amber-500/20 text-[9px] px-1.5 py-0.2 rounded font-mono font-medium">
                            Custom Gateway
                          </Badge>
                        ) : (
                          <Badge variant="outline" className="bg-amber-500/10 text-amber-400 border-amber-500/20 text-[9px] px-1.5 py-0.2 rounded font-mono font-medium">
                            Custom
                          </Badge>
                        )}
                        {route.kernelOnly && (
                          <Badge variant="outline" className="bg-rose-500/10 text-rose-400 border-rose-500/20 text-[9px] px-1.5 py-0.2 rounded font-mono font-medium">
                            Kernel Only
                          </Badge>
                        )}
                        {route.destination === "0.0.0.0/0" && route.type !== "defaultgateway" && (
                          <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[9px] px-1.5 py-0.2 rounded font-mono">
                            Default Gateway
                          </Badge>
                        )}

                        {/* Advanced options badges */}
                        {((route.scope && route.scope !== "global") || route.src || (route.proto && route.proto !== "static" && route.proto !== "120")) && (
                          <>
                            {route.scope && route.scope !== "global" && (
                              <span className="text-[10px] text-primary bg-primary/10 border border-primary/20 px-1.5 py-0.5 rounded font-mono font-medium animate-fade-in">
                                scope: {route.scope}
                              </span>
                            )}
                            {route.src && (
                              <span className="text-[10px] text-amber-400 bg-amber-500/10 border border-amber-500/20 px-1.5 py-0.5 rounded font-mono font-medium animate-fade-in">
                                src: {route.src}
                              </span>
                            )}
                            {route.proto && route.proto !== "static" && route.proto !== "120" && (
                              <span className="text-[10px] text-primary bg-primary/10 border border-primary/20 px-1.5 py-0.5 rounded font-mono font-medium animate-fade-in">
                                proto: {route.proto}
                              </span>
                            )}
                          </>
                        )}
                      </div>
                    </div>
                  </TableCell>
                  <TableCell className="p-3 font-mono text-xs">
                    {route.gateway ? (
                      <span className="text-foreground">{route.gateway}</span>
                    ) : (
                      <span className="text-muted-foreground/50 italic text-[11px]">Directly Connected</span>
                    )}
                  </TableCell>
                  <TableCell className="p-3">
                    <Badge variant="secondary" className="font-mono text-xs px-2 py-0.5 rounded">
                      {route.interface}
                    </Badge>
                  </TableCell>
                  <TableCell className="p-3 font-mono text-xs text-foreground">{route.metric}</TableCell>
                  <TableCell className="p-3 text-xs text-muted-foreground max-w-[150px] truncate" title={route.description}>
                    {route.description || <span className="text-muted-foreground/30 italic">No description</span>}
                  </TableCell>
                  <TableCell className="p-3">
                    <Switch
                      checked={route.status}
                      disabled={isRouteActionDisabled(route)}
                      onCheckedChange={() => handleToggleStatus(route.id)}
                      className="data-[state=checked]:bg-primary"
                    />
                  </TableCell>
                  <TableCell className="p-3 text-right">
                    <div className="flex items-center justify-end gap-1">
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        disabled={isRouteActionDisabled(route)}
                        onClick={() => openEditModal(route)}
                        className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50 disabled:opacity-25 disabled:cursor-not-allowed"
                        title={getEditTitle(route)}
                      >
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        disabled={isRouteActionDisabled(route)}
                        onClick={() => handleDelete(route.id, route.destination)}
                        className="cursor-pointer text-muted-foreground hover:text-red-500 hover:bg-red-500/10 disabled:opacity-25 disabled:cursor-not-allowed"
                        title={getDeleteTitle(route)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))
            )}
          </TableBody>
        </Table>
      </Card>

      {/* 6. Alert Help Box */}
      <Alert className="border-dashed border-border bg-card/10">
        <Info className="h-4 w-4 text-muted-foreground" />
        <AlertTitle className="font-bold text-foreground mb-0.5">การทำงานของ Routing Table:</AlertTitle>
        <AlertDescription className="text-xs text-muted-foreground leading-relaxed">
          เมื่อแพ็กเก็ตวิ่งเข้ามาในตัว PiGate ระบบปฏิบัติการจะตรวจสอบ Destination Network จากบนลงล่างตามลำดับของ <span className="font-semibold text-primary">Metric</span> (ค่ายิ่งต่ำยิ่งมีความสำคัญสูง)
          คุณสามารถจำลองการสลับการเปิด/ปิดสถานะเพื่ออัปเดตตารางและทดลองรันคำสั่งจริงในตัวกล่องจำลอง Linux Terminal ได้ทันที
        </AlertDescription>
      </Alert>

      {/* 7. Create / Edit Dialog */}
      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent ref={dialogContentRef} className="max-w-[500px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingRoute ? "แก้ไขเส้นทางเน็ตเวิร์ก (Static Route)" : "เพิ่มเส้นทางเน็ตเวิร์กใหม่ (Static Route)"}
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

            {/* Field: Destination */}
            <div className="space-y-1.5">
              <Label htmlFor="route-dest" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Destination Subnet IP/Mask <span className="text-red-500">*</span>
              </Label>
              <Input
                id="route-dest"
                type="text"
                required
                value={formDestination}
                onChange={(e) => setFormDestination(e.target.value)}
                placeholder="เช่น 192.168.10.0/24 หรือ 0.0.0.0/0"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
              <p className="text-[11px] text-muted-foreground italic">
                ระบุในรูปแบบ CIDR (เช่น 192.168.10.0/24) หรือระบุ 0.0.0.0/0 สำหรับ Default Route
              </p>
            </div>

            {/* Field: Gateway */}
            <div className="space-y-1.5">
              <Label htmlFor="route-gw" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Gateway IP (ทางผ่าน)
              </Label>
              <Input
                id="route-gw"
                type="text"
                value={formGateway}
                onChange={(e) => setFormGateway(e.target.value)}
                placeholder="เช่น 192.168.1.254 (ปล่อยว่างหากต้องการส่งออก Interface ตรง)"
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
              />
              <p className="text-[11px] text-muted-foreground italic">
                ไอพีของอุปกรณ์เกตเวย์/เร้าเตอร์ถัดไปที่เป็นตัวส่งต่อแพ็กเก็ต
              </p>
            </div>

            {/* Grid for Interface & Metric */}
            <div className="grid grid-cols-2 gap-4">
              {/* Field: Interface */}
              <div className="space-y-1.5">
                <Label htmlFor="route-iface" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Outgoing Interface
                </Label>
                <select
                  id="route-iface"
                  value={formInterface}
                  onChange={(e) => setFormInterface(e.target.value)}
                  className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                >
                  {interfaces.map((iface) => (
                    <option key={iface.id} value={iface.name}>
                      {iface.name} ({iface.alias || iface.role})
                    </option>
                  ))}
                  {interfaces.length === 0 && (
                    <>
                      <option value="eth0">eth0 (LAN Port)</option>
                      <option value="wlan0">wlan0 (Wireless WAN)</option>
                    </>
                  )}
                </select>
              </div>

              {/* Field: Metric */}
              <div className="space-y-1.5">
                <Label htmlFor="route-metric" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  Metric (ลำดับความสำคัญ)
                </Label>
                <Input
                  id="route-metric"
                  type="number"
                  required
                  min="0"
                  value={formMetric}
                  onChange={(e) => setFormMetric(e.target.value)}
                  placeholder="เช่น 10, 100"
                  className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono text-xs"
                />
              </div>
            </div>

            {/* Field: Description */}
            <div className="space-y-1.5">
              <Label htmlFor="route-desc" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                Description (รายละเอียด)
              </Label>
              <Input
                id="route-desc"
                type="text"
                value={formDescription}
                onChange={(e) => setFormDescription(e.target.value)}
                placeholder="เช่น เชื่อมต่อไปยังสาขาย่อย A, VPN tunnel"
                className="bg-background/50 placeholder:text-muted-foreground h-9 text-xs"
              />
            </div>

            {/* Collapsible Advanced Section */}
            <div className="border border-border/60 rounded-lg overflow-hidden bg-card/10">
              <button
                type="button"
                onClick={() => setIsAdvancedOpen(!isAdvancedOpen)}
                className="w-full flex items-center justify-between px-3 py-2 text-xs font-bold text-muted-foreground hover:bg-muted/20 transition cursor-pointer select-none"
              >
                <span className="flex items-center gap-1.5">
                  <SlidersHorizontal className="h-3.5 w-3.5 text-primary" />
                  Advanced Routing Settings (การตั้งค่าขั้นสูง)
                </span>
                {isAdvancedOpen ? <ChevronUp className="h-4 w-4" /> : <ChevronDown className="h-4 w-4" />}
              </button>

              {isAdvancedOpen && (
                <div className="p-3 border-t border-border/40 space-y-3 bg-muted/5 animate-slide-down">
                  {/* Grid for Scope & Protocol */}
                  <div className="grid grid-cols-2 gap-3">
                    {/* Advanced Field: Scope */}
                    <div className="space-y-1.5">
                      <Label htmlFor="route-scope" className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider block">
                        Scope (ขอบเขต)
                      </Label>
                      <select
                        id="route-scope"
                        value={formScope}
                        onChange={(e) => setFormScope(e.target.value)}
                        className="w-full bg-background border border-border rounded-lg h-8 px-2 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                      >
                        <option value="global">global (Global route)</option>
                        <option value="link">link (Direct network link)</option>
                        <option value="host">host (Local host link)</option>
                        <option value="site">site (IPv6 Site-local)</option>
                      </select>
                    </div>

                    {/* Advanced Field: Protocol */}
                    <div className="space-y-1.5">
                      <Label htmlFor="route-proto" className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider block">
                        Protocol (โปรโตคอล)
                      </Label>
                      <select
                        id="route-proto"
                        value={formProto}
                        onChange={(e) => setFormProto(e.target.value)}
                        className="w-full bg-background border border-border rounded-lg h-8 px-2 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
                      >
                        <option value="120">120 (PiGate Custom)</option>
                        {
                          uiEditSystemRouteActive && (
                            <>
                              <option value="static">static (Static Route)</option>
                              <option value="kernel">kernel (OS Kernel Auto)</option>
                              <option value="boot">boot (System Startup)</option>
                            </>
                          )
                        }
                      </select>
                    </div>
                  </div>

                  {/* Advanced Field: Src IP */}
                  <div className="space-y-1.5">
                    <Label htmlFor="route-src" className="text-[10px] font-semibold text-muted-foreground uppercase tracking-wider block">
                      Preferred Source IP (src)
                    </Label>
                    <Input
                      id="route-src"
                      type="text"
                      value={formSrc}
                      onChange={(e) => setFormSrc(e.target.value)}
                      placeholder="เช่น 192.168.1.2"
                      className="bg-background/50 placeholder:text-muted-foreground h-8 font-mono text-xs"
                    />
                    <p className="text-[10px] text-muted-foreground italic leading-normal">
                      IP ของฝั่งส่งที่ต้องการบังคับให้ออกจากอินเตอร์เฟสนี้ (Preferred Source IP)
                    </p>
                  </div>
                </div>
              )}
            </div>

            {/* Field: Status Toggle */}
            <div className="flex items-center justify-between bg-muted/20 p-2.5 rounded-lg border border-border/40">
              <div className="flex flex-col gap-0.5">
                <span className="text-xs font-bold text-foreground">เปิดใช้งานทันที (Status)</span>
                <span className="text-[10px] text-muted-foreground">เริ่มเปิดทำงานตารางเชื่อมต่อทันทีที่บันทึก</span>
              </div>
              <Switch
                checked={formStatus}
                onCheckedChange={setFormStatus}
                className="data-[state=checked]:bg-primary"
              />
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
                Save Route
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
