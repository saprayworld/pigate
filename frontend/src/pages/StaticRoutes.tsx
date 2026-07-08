import { useState, useMemo, useEffect, useCallback } from "react"
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
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card"
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
  Drawer,
  DrawerContent,
  DrawerHeader,
  DrawerTitle,
} from "@/components/ui/drawer"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Alert, AlertDescription } from "@/components/ui/alert"
import { type StaticRoute, type NetworkInterface } from "@/data-mockup/mockData"
import { staticRouteService } from "@/services/staticRouteService"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/hooks/useAlert"
import { isValidIp, isValidCidr } from "@/lib/utils"

// Helper: Dashboard-style stat card (mirrors Dashboard's StatCard)
function StatCard({
  icon: Icon,
  title,
  value,
}: {
  icon: typeof Route
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
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={Route} title="Total Routes" value={stats.total} />
        <StatCard icon={CheckCircle2} title="Active Routes" value={stats.active} />
        <StatCard icon={Network} title="System Subnets" value={stats.system} />
        <StatCard icon={SlidersHorizontal} title="Custom Config" value={stats.custom} />
      </div>

      {/* 2. Routing table */}
      <Card>
        <CardHeader className="flex flex-row flex-wrap items-center justify-between gap-3 space-y-0">
          <CardTitle className="flex items-center gap-2 text-base font-semibold">
            <Route className="h-4 w-4 text-muted-foreground" />
            Routing Table
          </CardTitle>
          <div className="flex flex-wrap items-center gap-3">
            {enableEditSystemRoute && (
              <div className="flex h-8 items-center gap-2 rounded-lg border border-red-500/20 bg-red-500/10 px-3 text-xs font-medium select-none">
                <span className="text-red-500">แก้ไขเส้นทางระบบ</span>
                <Switch
                  checked={uiEditSystemRouteActive}
                  onCheckedChange={handleToggleEditSystemMode}
                  size="sm"
                />
              </div>
            )}
            <Button
              variant="outline"
              size="sm"
              onClick={() => loadRoutes(true)}
              disabled={isLoading}
              className="cursor-pointer gap-2"
            >
              {isLoading ? (
                <Loader2 className="h-4 w-4 animate-spin" />
              ) : (
                <RefreshCw className="h-4 w-4" />
              )}
              Refresh
            </Button>
            <Button size="sm" onClick={openCreateModal} className="cursor-pointer gap-1.5 font-semibold">
              <Plus className="h-4 w-4" />
              Create Static Route
            </Button>
          </div>
        </CardHeader>
        <CardContent className="space-y-4">
          {/* Toolbar (Filters & Search) */}
          <div className="flex flex-col gap-3 md:flex-row md:items-center md:justify-between">
            <div className="flex flex-wrap items-center gap-2.5">
              {/* Type filters */}
              <div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">
                <button
                  onClick={() => setSelectedTypeFilter("all")}
                  className={`cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition ${selectedTypeFilter === "all"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  All Types
                </button>
                <button
                  onClick={() => setSelectedTypeFilter("system")}
                  className={`cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition ${selectedTypeFilter === "system"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  System
                </button>
                <button
                  onClick={() => setSelectedTypeFilter("custom")}
                  className={`cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition ${selectedTypeFilter === "custom"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  Custom
                </button>
              </div>

              {/* Status filters */}
              <div className="flex w-fit gap-0.5 rounded-lg border border-border bg-muted p-0.5">
                <button
                  onClick={() => setSelectedStatusFilter("all")}
                  className={`cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition ${selectedStatusFilter === "all"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  All Status
                </button>
                <button
                  onClick={() => setSelectedStatusFilter("active")}
                  className={`cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition ${selectedStatusFilter === "active"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
                    }`}
                >
                  Active Only
                </button>
                <button
                  onClick={() => setSelectedStatusFilter("inactive")}
                  className={`cursor-pointer rounded-md px-3 py-1 text-xs font-medium transition ${selectedStatusFilter === "inactive"
                    ? "bg-primary text-primary-foreground"
                    : "text-muted-foreground hover:bg-muted hover:text-foreground"
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
                  className="cursor-pointer gap-1.5"
                >
                  <Trash2 className="h-3.5 w-3.5" />
                  Delete Selected ({selectedIds.length})
                </Button>
              )}
            </div>

            {/* Search */}
            <div className="relative w-full md:max-w-xs">
              <Search className="pointer-events-none absolute top-2.5 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="ค้นหา IP เครือข่าย, Gateway หรือคำอธิบาย..."
                className="h-9 pl-8"
              />
            </div>
          </div>

          {/* Table view */}
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[5%]">
                  <input
                    type="checkbox"
                    disabled={selectableRoutes.length === 0}
                    checked={isAllSelected}
                    onChange={(e) => handleSelectAll(e.target.checked)}
                    className="h-4 w-4 cursor-pointer rounded border-input bg-background accent-primary disabled:cursor-not-allowed disabled:opacity-30"
                  />
                </TableHead>
                <TableHead className="w-[22%] text-xs font-medium text-muted-foreground">Destination Network</TableHead>
                <TableHead className="w-[20%] text-xs font-medium text-muted-foreground">Gateway</TableHead>
                <TableHead className="w-[12%] text-xs font-medium text-muted-foreground">Interface</TableHead>
                <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Metric</TableHead>
                <TableHead className="w-[18%] text-xs font-medium text-muted-foreground">Description</TableHead>
                <TableHead className="w-[8%] text-xs font-medium text-muted-foreground">Status</TableHead>
                <TableHead className="w-[5%] text-right text-xs font-medium text-muted-foreground"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={8} className="py-12 text-center text-xs text-muted-foreground">
                    <div className="flex flex-col items-center justify-center gap-2 py-4">
                      <Loader2 className="h-6 w-6 animate-spin text-primary" />
                      <span>กำลังโหลดข้อมูล...</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : filteredRoutes.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={8} className="py-8 text-center text-xs text-muted-foreground">
                    ไม่พบเส้นทางเครือข่ายตามที่ค้นหา
                  </TableCell>
                </TableRow>
              ) : (
                filteredRoutes.map((route) => (
                  <TableRow key={route.id}>
                    <TableCell className="py-3">
                      <input
                        type="checkbox"
                        disabled={isRouteActionDisabled(route)}
                        checked={selectedIds.includes(route.id)}
                        onChange={(e) => handleSelectRow(route.id, e.target.checked)}
                        className="h-4 w-4 cursor-pointer rounded border-input bg-background accent-primary disabled:cursor-not-allowed disabled:opacity-30"
                      />
                    </TableCell>
                    <TableCell className="py-3">
                      <div className="flex flex-col gap-1">
                        <span className="font-mono text-sm font-medium text-foreground">{route.destination}</span>
                        <div className="flex flex-wrap items-center gap-1.5">
                          {route.type === "system" ? (
                            <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 font-mono text-[10px] font-medium text-primary">
                              System
                            </Badge>
                          ) : route.type === "defaultgateway" ? (
                            <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 font-mono text-[10px] font-medium text-primary">
                              Default Gateway
                            </Badge>
                          ) : route.type === "customgateway" ? (
                            <Badge variant="outline" className="rounded border-amber-500/20 bg-amber-500/10 px-1.5 py-0 font-mono text-[10px] font-medium text-amber-500">
                              Custom Gateway
                            </Badge>
                          ) : (
                            <Badge variant="outline" className="rounded border-amber-500/20 bg-amber-500/10 px-1.5 py-0 font-mono text-[10px] font-medium text-amber-500">
                              Custom
                            </Badge>
                          )}
                          {route.kernelOnly && (
                            <Badge variant="outline" className="rounded border-red-500/20 bg-red-500/10 px-1.5 py-0 font-mono text-[10px] font-medium text-red-500">
                              Kernel Only
                            </Badge>
                          )}
                          {route.destination === "0.0.0.0/0" && route.type !== "defaultgateway" && (
                            <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 font-mono text-[10px] font-medium text-primary">
                              Default Gateway
                            </Badge>
                          )}

                          {/* Advanced options badges */}
                          {((route.scope && route.scope !== "global") || route.src || (route.proto && route.proto !== "static" && route.proto !== "120")) && (
                            <>
                              {route.scope && route.scope !== "global" && (
                                <span className="animate-fade-in rounded border border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-primary">
                                  scope: {route.scope}
                                </span>
                              )}
                              {route.src && (
                                <span className="animate-fade-in rounded border border-amber-500/20 bg-amber-500/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-amber-500">
                                  src: {route.src}
                                </span>
                              )}
                              {route.proto && route.proto !== "static" && route.proto !== "120" && (
                                <span className="animate-fade-in rounded border border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-primary">
                                  proto: {route.proto}
                                </span>
                              )}
                            </>
                          )}
                        </div>
                      </div>
                    </TableCell>
                    <TableCell className="py-3 font-mono text-xs">
                      {route.gateway ? (
                        <span className="text-foreground">{route.gateway}</span>
                      ) : (
                        <span className="text-[11px] italic text-muted-foreground/50">Directly Connected</span>
                      )}
                    </TableCell>
                    <TableCell className="py-3">
                      <Badge variant="secondary" className="rounded px-2 py-0.5 font-mono text-xs">
                        {route.interface}
                      </Badge>
                    </TableCell>
                    <TableCell className="py-3 font-mono text-xs text-foreground">{route.metric}</TableCell>
                    <TableCell className="max-w-[150px] truncate py-3 text-xs text-muted-foreground" title={route.description}>
                      {route.description || <span className="italic text-muted-foreground/30">No description</span>}
                    </TableCell>
                    <TableCell className="py-3">
                      <Switch
                        checked={route.status}
                        disabled={isRouteActionDisabled(route)}
                        onCheckedChange={() => handleToggleStatus(route.id)}
                      />
                    </TableCell>
                    <TableCell className="py-3 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="outline"
                          size="icon-sm"
                          disabled={isRouteActionDisabled(route)}
                          onClick={() => openEditModal(route)}
                          className="cursor-pointer text-muted-foreground hover:text-foreground disabled:cursor-not-allowed disabled:opacity-25"
                          title={getEditTitle(route)}
                        >
                          <Edit className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          disabled={isRouteActionDisabled(route)}
                          onClick={() => handleDelete(route.id, route.destination)}
                          className="cursor-pointer text-muted-foreground hover:bg-red-500/10 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-25"
                          title={getDeleteTitle(route)}
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
        <Info className="mt-0.5 h-4 w-4 shrink-0" />
        <span>
          <strong className="text-foreground">การทำงานของ Routing Table:</strong>{" "}
          เมื่อแพ็กเก็ตวิ่งเข้ามาในตัว PiGate ระบบปฏิบัติการจะตรวจสอบ Destination Network จากบนลงล่างตามลำดับของ <strong className="text-foreground">Metric</strong> (ค่ายิ่งต่ำยิ่งมีความสำคัญสูง)
          คุณสามารถจำลองการสลับการเปิด/ปิดสถานะเพื่ออัปเดตตารางและทดลองรันคำสั่งจริงในตัวกล่องจำลอง Linux Terminal ได้ทันที
        </span>
      </div>

      {/* 4. Create / Edit Dialog */}
      <Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingRoute ? "แก้ไขเส้นทางเน็ตเวิร์ก (Static Route)" : "เพิ่มเส้นทางเน็ตเวิร์กใหม่ (Static Route)"}
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

            {/* Field: Destination */}
            <div className="space-y-1.5">
              <Label htmlFor="route-dest" className="block text-xs font-medium text-muted-foreground">
                Destination Subnet IP/Mask <span className="text-destructive">*</span>
              </Label>
              <Input
                id="route-dest"
                type="text"
                required
                value={formDestination}
                onChange={(e) => setFormDestination(e.target.value)}
                placeholder="เช่น 192.168.10.0/24 หรือ 0.0.0.0/0"
                className="h-9 font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                ระบุในรูปแบบ CIDR (เช่น 192.168.10.0/24) หรือระบุ 0.0.0.0/0 สำหรับ Default Route
              </p>
            </div>

            {/* Field: Gateway */}
            <div className="space-y-1.5">
              <Label htmlFor="route-gw" className="block text-xs font-medium text-muted-foreground">
                Gateway IP (ทางผ่าน)
              </Label>
              <Input
                id="route-gw"
                type="text"
                value={formGateway}
                onChange={(e) => setFormGateway(e.target.value)}
                placeholder="เช่น 192.168.1.254 (ปล่อยว่างหากต้องการส่งออก Interface ตรง)"
                className="h-9 font-mono text-sm"
              />
              <p className="mt-0.5 text-[10px] text-muted-foreground">
                ไอพีของอุปกรณ์เกตเวย์/เร้าเตอร์ถัดไปที่เป็นตัวส่งต่อแพ็กเก็ต
              </p>
            </div>

            {/* Grid for Interface & Metric */}
            <div className="grid grid-cols-2 gap-4">
              {/* Field: Interface */}
              <div className="space-y-1.5">
                <Label htmlFor="route-iface" className="block text-xs font-medium text-muted-foreground">
                  Outgoing Interface
                </Label>
                <select
                  id="route-iface"
                  value={formInterface}
                  onChange={(e) => setFormInterface(e.target.value)}
                  className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
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
                <Label htmlFor="route-metric" className="block text-xs font-medium text-muted-foreground">
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
                  className="h-9 font-mono text-sm"
                />
              </div>
            </div>

            {/* Field: Description */}
            <div className="space-y-1.5">
              <Label htmlFor="route-desc" className="block text-xs font-medium text-muted-foreground">
                Description (รายละเอียด)
              </Label>
              <Input
                id="route-desc"
                type="text"
                value={formDescription}
                onChange={(e) => setFormDescription(e.target.value)}
                placeholder="เช่น เชื่อมต่อไปยังสาขาย่อย A, VPN tunnel"
                className="h-9 text-sm"
              />
            </div>

            {/* Collapsible Advanced Section */}
            <div className="overflow-hidden rounded-lg border border-border">
              <button
                type="button"
                onClick={() => setIsAdvancedOpen(!isAdvancedOpen)}
                className="flex w-full cursor-pointer items-center justify-between px-3 py-2 text-xs font-semibold text-foreground transition select-none hover:bg-muted/50"
              >
                <span className="flex items-center gap-1.5">
                  <SlidersHorizontal className="h-3.5 w-3.5 text-muted-foreground" />
                  Advanced Routing Settings (การตั้งค่าขั้นสูง)
                </span>
                {isAdvancedOpen ? <ChevronUp className="h-4 w-4 text-muted-foreground" /> : <ChevronDown className="h-4 w-4 text-muted-foreground" />}
              </button>

              {isAdvancedOpen && (
                <div className="animate-slide-down space-y-3 border-t border-border/50 bg-muted/50 p-3">
                  {/* Grid for Scope & Protocol */}
                  <div className="grid grid-cols-2 gap-3">
                    {/* Advanced Field: Scope */}
                    <div className="space-y-1.5">
                      <Label htmlFor="route-scope" className="block text-xs font-medium text-muted-foreground">
                        Scope (ขอบเขต)
                      </Label>
                      <select
                        id="route-scope"
                        value={formScope}
                        onChange={(e) => setFormScope(e.target.value)}
                        className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                      >
                        <option value="global">global (Global route)</option>
                        <option value="link">link (Direct network link)</option>
                        <option value="host">host (Local host link)</option>
                        <option value="site">site (IPv6 Site-local)</option>
                      </select>
                    </div>

                    {/* Advanced Field: Protocol */}
                    <div className="space-y-1.5">
                      <Label htmlFor="route-proto" className="block text-xs font-medium text-muted-foreground">
                        Protocol (โปรโตคอล)
                      </Label>
                      <select
                        id="route-proto"
                        value={formProto}
                        onChange={(e) => setFormProto(e.target.value)}
                        className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
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
                    <Label htmlFor="route-src" className="block text-xs font-medium text-muted-foreground">
                      Preferred Source IP (src)
                    </Label>
                    <Input
                      id="route-src"
                      type="text"
                      value={formSrc}
                      onChange={(e) => setFormSrc(e.target.value)}
                      placeholder="เช่น 192.168.1.2"
                      className="h-9 bg-background font-mono text-sm"
                    />
                    <p className="mt-0.5 text-[10px] leading-normal text-muted-foreground">
                      IP ของฝั่งส่งที่ต้องการบังคับให้ออกจากอินเตอร์เฟสนี้ (Preferred Source IP)
                    </p>
                  </div>
                </div>
              )}
            </div>

            {/* Field: Status Toggle */}
            <div className="flex items-center justify-between rounded-lg border border-border bg-muted/50 p-3">
              <div className="flex flex-col gap-0.5">
                <span className="text-xs font-semibold text-foreground">เปิดใช้งานทันที (Status)</span>
                <span className="text-[10px] text-muted-foreground">เริ่มเปิดทำงานตารางเชื่อมต่อทันทีที่บันทึก</span>
              </div>
              <Switch
                checked={formStatus}
                onCheckedChange={setFormStatus}
              />
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
              <Button type="submit" className="cursor-pointer px-6 font-semibold">
                Save Route
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
