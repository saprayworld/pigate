import { useState, useMemo, useEffect, type ReactNode } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  ArrowRightLeft,
  Plus,
  Search,
  Edit,
  Trash2,
  AlertCircle,
  Power,
  PowerOff,
  Loader2,
  Globe,
  Info,
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
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
import { type PortForward, type NetworkInterface } from "@/data-mockup/mockData"
import { portForwardService } from "@/services/portForwardService"
import { interfaceService } from "@/services/interfaceService"
import { useAlert } from "@/hooks/useAlert"

function StatCard({
  icon: Icon,
  title,
  value,
}: {
  icon: typeof ArrowRightLeft
  title: string
  value: ReactNode
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
        <div className="text-2xl font-bold tracking-tight text-foreground">{value}</div>
      </CardContent>
    </Card>
  )
}

// ifaceLabel renders a friendly "eth0 (WAN_Uplink)" label when an alias exists.
function ifaceLabel(name: string, interfaces: NetworkInterface[]): string {
  const iface = interfaces.find((i) => i.name === name)
  if (iface && iface.alias) return `${name} (${iface.alias})`
  return name
}

const IPV4_REGEX = /^(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}$/

export default function PortForwarding() {
  const { alert, confirm } = useAlert()

  // --- State ---
  const [forwards, setForwards] = useState<PortForward[]>([])
  const [interfaces, setInterfaces] = useState<NetworkInterface[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [searchQuery, setSearchQuery] = useState("")

  // Modal State
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editing, setEditing] = useState<PortForward | null>(null)
  const [saving, setSaving] = useState(false)

  // Form fields
  const [formName, setFormName] = useState("")
  const [formIface, setFormIface] = useState("")
  const [formProto, setFormProto] = useState<"tcp" | "udp">("tcp")
  const [formExtPort, setFormExtPort] = useState("")
  const [formIntIP, setFormIntIP] = useState("")
  const [formIntPort, setFormIntPort] = useState("")
  const [formStatus, setFormStatus] = useState(true)
  const [formError, setFormError] = useState("")

  const loadData = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const [pfs, ifaces] = await Promise.all([
        portForwardService.getAll(),
        interfaceService.getAll(),
      ])
      setForwards(pfs)
      setInterfaces(ifaces)
    } catch (err) {
      console.error(err)
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูล Port Forwarding ได้: " + getErrorMessage(err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    const initialLoad = async () => {
      try {
        const [pfs, ifaces] = await Promise.all([
          portForwardService.getAll(),
          interfaceService.getAll(),
        ])
        setForwards(pfs)
        setInterfaces(ifaces)
      } catch (err) {
        console.error(err)
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดข้อมูล Port Forwarding ได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

  // --- Statistics ---
  const stats = useMemo(() => {
    const total = forwards.length
    const enabled = forwards.filter((f) => f.status).length
    const tcp = forwards.filter((f) => f.protocol === "tcp").length
    const udp = forwards.filter((f) => f.protocol === "udp").length
    return { total, enabled, tcp, udp }
  }, [forwards])

  const filtered = useMemo(() => {
    const q = searchQuery.toLowerCase()
    return forwards.filter(
      (f) =>
        f.name.toLowerCase().includes(q) ||
        f.inInterface.toLowerCase().includes(q) ||
        f.externalPort.toLowerCase().includes(q) ||
        f.internalIP.toLowerCase().includes(q),
    )
  }, [forwards, searchQuery])

  // WAN interfaces are the natural external side, but allow any interface.
  const wanFirst = useMemo(() => {
    return [...interfaces].sort((a, b) => {
      if (a.role === b.role) return a.name.localeCompare(b.name)
      return a.role === "WAN" ? -1 : 1
    })
  }, [interfaces])

  // --- CRUD Actions ---
  const openCreateModal = () => {
    setEditing(null)
    setFormName("")
    setFormIface(wanFirst[0]?.name ?? "")
    setFormProto("tcp")
    setFormExtPort("")
    setFormIntIP("")
    setFormIntPort("")
    setFormStatus(true)
    setFormError("")
    setIsModalOpen(true)
  }

  const openEditModal = (pf: PortForward) => {
    setEditing(pf)
    setFormName(pf.name)
    setFormIface(pf.inInterface)
    setFormProto(pf.protocol)
    setFormExtPort(pf.externalPort)
    setFormIntIP(pf.internalIP)
    setFormIntPort(pf.internalPort)
    setFormStatus(pf.status)
    setFormError("")
    setIsModalOpen(true)
  }

  const handleDelete = async (pf: PortForward) => {
    if (await confirm("ยืนยันการลบ", `คุณต้องการลบ Port Forward "${pf.name}" ใช่หรือไม่?`)) {
      try {
        await portForwardService.delete(pf.id)
        await loadData(false)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถลบข้อมูลได้: " + getErrorMessage(err))
      }
    }
  }

  const handleToggleStatus = async (pf: PortForward) => {
    try {
      await portForwardService.update(pf.id, { ...pf, status: !pf.status })
      await loadData(false)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถเปลี่ยนสถานะได้: " + getErrorMessage(err))
    }
  }

  const validatePortSpec = (spec: string): boolean => {
    const single = /^\d+$/
    const range = /^(\d+)-(\d+)$/
    if (single.test(spec)) {
      const p = parseInt(spec, 10)
      return p >= 1 && p <= 65535
    }
    const m = spec.match(range)
    if (m) {
      const s = parseInt(m[1], 10)
      const e = parseInt(m[2], 10)
      return s >= 1 && e <= 65535 && s < e
    }
    return false
  }

  const isRange = (spec: string) => /^\d+-\d+$/.test(spec.trim())

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    if (!formName.trim()) {
      setFormError("กรุณากรอกชื่อ (Name)")
      return
    }
    if (!formIface) {
      setFormError("กรุณาเลือก External Interface")
      return
    }
    const ext = formExtPort.trim()
    if (!validatePortSpec(ext)) {
      setFormError("External Port ไม่ถูกต้อง (พอร์ตเดี่ยว เช่น 8080 หรือช่วง เช่น 8000-8010, ค่า 1-65535)")
      return
    }
    if (!IPV4_REGEX.test(formIntIP.trim())) {
      setFormError("Internal IP ต้องเป็น IPv4 ที่ถูกต้อง เช่น 192.168.1.10")
      return
    }
    const intPort = formIntPort.trim()
    if (isRange(ext) && intPort !== "") {
      setFormError("การส่งต่อแบบช่วงพอร์ต (range) ต้องเว้น Internal Port ให้ว่าง (ระบบจะคงพอร์ตเดิมไว้)")
      return
    }
    if (intPort !== "") {
      const p = parseInt(intPort, 10)
      if (!/^\d+$/.test(intPort) || p < 1 || p > 65535) {
        setFormError("Internal Port ต้องอยู่ระหว่าง 1-65535 หรือเว้นว่างเพื่อคงพอร์ตเดิม")
        return
      }
    }

    const payload = {
      name: formName.trim(),
      inInterface: formIface,
      externalPort: ext,
      protocol: formProto,
      internalIP: formIntIP.trim(),
      internalPort: intPort,
      status: formStatus,
    }

    setSaving(true)
    try {
      if (editing) {
        await portForwardService.update(editing.id, payload)
      } else {
        await portForwardService.create(payload)
      }
      await loadData(false)
      setIsModalOpen(false)
    } catch (err) {
      setFormError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    } finally {
      setSaving(false)
    }
  }

  return (
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={ArrowRightLeft} title="Total Rules" value={stats.total} />
        <StatCard icon={Power} title="Enabled" value={stats.enabled} />
        <StatCard icon={Globe} title="TCP" value={stats.tcp} />
        <StatCard icon={Globe} title="UDP" value={stats.udp} />
      </div>

      {/* 2. Table */}
      <Card>
        <CardHeader className="flex flex-col gap-4 space-y-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <ArrowRightLeft className="h-4 w-4 text-muted-foreground" />
              Port Forwarding (DNAT)
              <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                {stats.total}
              </Badge>
            </CardTitle>
            <CardDescription className="text-xs">
              เปิดบริการในเครือข่าย LAN ออกสู่ภายนอก โดยแปลงปลายทาง (DNAT) จาก External Interface:Port ไปยังเครื่องภายใน
            </CardDescription>
          </div>

          <div className="flex flex-wrap items-center gap-3">
            <div className="relative w-full sm:w-[200px]">
              <Search className="pointer-events-none absolute top-2 left-2.5 h-4 w-4 text-muted-foreground" />
              <Input
                type="text"
                value={searchQuery}
                onChange={(e) => setSearchQuery(e.target.value)}
                placeholder="ค้นหาชื่อ, IP, พอร์ต..."
                className="h-8 pl-8 text-xs"
              />
            </div>
            <Button size="sm" onClick={openCreateModal} className="cursor-pointer gap-1.5 font-semibold">
              <Plus className="h-4 w-4" />
              Create Port Forward
            </Button>
          </div>
        </CardHeader>

        <CardContent className="space-y-4">
          {/* Hairpin / NAT-loopback caveat */}
          <Alert className="px-3 py-2.5">
            <Info className="h-4 w-4" />
            <AlertDescription className="text-xs text-muted-foreground">
              เข้าถึงบริการจากภายนอกผ่าน External Interface Address เท่านั้น — ยังไม่รองรับ hairpin/NAT-loopback
              (เครื่องใน LAN เรียก external address ของตัวเอง) ให้เครื่องใน LAN ใช้ Internal IP โดยตรง
            </AlertDescription>
          </Alert>

          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="w-[18%] text-xs font-medium text-muted-foreground">Name</TableHead>
                <TableHead className="w-[16%] text-xs font-medium text-muted-foreground">Ext Interface</TableHead>
                <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Proto</TableHead>
                <TableHead className="w-[14%] text-xs font-medium text-muted-foreground">Ext Port</TableHead>
                <TableHead className="w-[22%] text-xs font-medium text-muted-foreground">Internal Target</TableHead>
                <TableHead className="w-[10%] text-xs font-medium text-muted-foreground">Status</TableHead>
                <TableHead className="w-[10%] text-right text-xs font-medium text-muted-foreground"></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={7} className="py-12 text-center text-xs text-muted-foreground">
                    <div className="flex flex-col items-center justify-center gap-2 py-4">
                      <Loader2 className="h-6 w-6 animate-spin text-primary" />
                      <span>กำลังโหลดข้อมูล...</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : filtered.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={7} className="py-8 text-center text-xs text-muted-foreground">
                    ยังไม่มีรายการ Port Forwarding
                  </TableCell>
                </TableRow>
              ) : (
                filtered.map((pf) => (
                  <TableRow key={pf.id}>
                    <TableCell className="py-3 font-medium text-foreground">{pf.name}</TableCell>
                    <TableCell className="py-3 font-mono text-xs text-muted-foreground">
                      {ifaceLabel(pf.inInterface, interfaces)}
                    </TableCell>
                    <TableCell className="py-3">
                      <Badge
                        variant="outline"
                        className="rounded border-primary/20 bg-primary/10 px-1.5 py-0.5 font-mono text-[10px] font-medium text-primary uppercase"
                      >
                        {pf.protocol}
                      </Badge>
                    </TableCell>
                    <TableCell className="py-3 font-mono text-xs text-foreground">{pf.externalPort}</TableCell>
                    <TableCell className="py-3 font-mono text-xs text-muted-foreground">
                      {pf.internalIP}
                      {pf.internalPort ? (
                        <span className="text-foreground">:{pf.internalPort}</span>
                      ) : (
                        <span className="text-muted-foreground/60"> (keep port)</span>
                      )}
                    </TableCell>
                    <TableCell className="py-3">
                      {pf.status ? (
                        <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[10px] font-medium text-primary">
                          Enabled
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="rounded px-2 py-0.5 text-[10px] font-medium">
                          Disabled
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="py-3 text-right">
                      <div className="flex items-center justify-end gap-2">
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleToggleStatus(pf)}
                          className="cursor-pointer text-muted-foreground hover:text-foreground"
                          title={pf.status ? "ปิดการใช้งาน" : "เปิดการใช้งาน"}
                        >
                          {pf.status ? <PowerOff className="h-4 w-4" /> : <Power className="h-4 w-4" />}
                        </Button>
                        <Button
                          variant="outline"
                          size="icon-sm"
                          onClick={() => openEditModal(pf)}
                          className="cursor-pointer text-muted-foreground hover:text-foreground"
                          title="แก้ไข"
                        >
                          <Edit className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          onClick={() => handleDelete(pf)}
                          className="cursor-pointer text-muted-foreground hover:bg-destructive/10 hover:text-destructive"
                          title="ลบ"
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

      {/* 3. Create / Edit Drawer */}
      <Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[500px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editing ? "แก้ไข Port Forward" : "สร้าง Port Forward ใหม่"}
            </DrawerTitle>
          </DrawerHeader>

          <div className="flex-1 overflow-y-auto p-4">
            <form onSubmit={handleSave} className="space-y-4 text-sm">
              {formError && (
                <Alert variant="destructive" className="px-3 py-2.5">
                  <AlertCircle className="h-4 w-4" />
                  <AlertDescription className="text-xs">{formError}</AlertDescription>
                </Alert>
              )}

              {/* Name */}
              <div className="space-y-1.5">
                <Label htmlFor="pf-name" className="block text-xs font-medium text-muted-foreground">
                  ชื่อ (Name) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="pf-name"
                  type="text"
                  required
                  value={formName}
                  onChange={(e) => setFormName(e.target.value)}
                  placeholder="เช่น Web_Server, RDP_Office"
                  className="h-9 text-sm"
                />
              </div>

              {/* External Interface + Protocol */}
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="pf-iface" className="block text-xs font-medium text-muted-foreground">
                    External Interface <span className="text-destructive">*</span>
                  </Label>
                  <select
                    id="pf-iface"
                    value={formIface}
                    onChange={(e) => setFormIface(e.target.value)}
                    className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                  >
                    <option value="" disabled>
                      เลือก Interface
                    </option>
                    {wanFirst.map((iface) => (
                      <option key={iface.name} value={iface.name}>
                        {ifaceLabel(iface.name, interfaces)}
                        {iface.role === "WAN" ? " — WAN" : ""}
                      </option>
                    ))}
                  </select>
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="pf-proto" className="block text-xs font-medium text-muted-foreground">
                    Protocol
                  </Label>
                  <select
                    id="pf-proto"
                    value={formProto}
                    onChange={(e) => setFormProto(e.target.value as "tcp" | "udp")}
                    className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
                  >
                    <option value="tcp">TCP</option>
                    <option value="udp">UDP</option>
                  </select>
                </div>
              </div>

              {/* External Port */}
              <div className="space-y-1.5">
                <Label htmlFor="pf-extport" className="block text-xs font-medium text-muted-foreground">
                  External Port <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="pf-extport"
                  type="text"
                  required
                  value={formExtPort}
                  onChange={(e) => setFormExtPort(e.target.value)}
                  placeholder="เช่น 8080 หรือช่วง 8000-8010"
                  className="h-9 font-mono text-sm"
                />
                <p className="mt-0.5 text-[10px] text-muted-foreground">
                  พอร์ตที่เปิดรับจากภายนอก บนที่อยู่ของ External Interface (พอร์ตเดี่ยวหรือช่วง)
                </p>
              </div>

              {/* Internal IP + Port */}
              <div className="grid gap-4 sm:grid-cols-2">
                <div className="space-y-1.5">
                  <Label htmlFor="pf-intip" className="block text-xs font-medium text-muted-foreground">
                    Internal IP <span className="text-destructive">*</span>
                  </Label>
                  <Input
                    id="pf-intip"
                    type="text"
                    required
                    value={formIntIP}
                    onChange={(e) => setFormIntIP(e.target.value)}
                    placeholder="192.168.1.10"
                    className="h-9 font-mono text-sm"
                  />
                </div>

                <div className="space-y-1.5">
                  <Label htmlFor="pf-intport" className="block text-xs font-medium text-muted-foreground">
                    Internal Port
                  </Label>
                  <Input
                    id="pf-intport"
                    type="text"
                    disabled={isRange(formExtPort)}
                    value={formIntPort}
                    onChange={(e) => setFormIntPort(e.target.value)}
                    placeholder={isRange(formExtPort) ? "คงพอร์ตเดิม (range)" : "เว้นว่าง = คงพอร์ตเดิม"}
                    className="h-9 font-mono text-sm"
                  />
                </div>
              </div>
              <p className="text-[10px] leading-relaxed text-muted-foreground">
                เว้น Internal Port ว่างไว้เพื่อคงพอร์ตเดิม (จำเป็นสำหรับช่วงพอร์ต) — การแปลงพอร์ตแบบช่วง (range→range) ยังไม่รองรับ
              </p>

              {/* Status */}
              <div className="flex items-center justify-between rounded-md border border-border/60 px-3 py-2.5">
                <div className="space-y-0.5">
                  <Label className="text-xs font-medium text-foreground">เปิดใช้งาน (Enabled)</Label>
                  <p className="text-[10px] text-muted-foreground">รายการที่เปิดใช้งานจะถูกนำไปใช้กับ firewall ทันที</p>
                </div>
                <Switch checked={formStatus} onCheckedChange={setFormStatus} className="cursor-pointer" />
              </div>

              {/* Actions */}
              <div className="flex items-center justify-end gap-3 border-t border-border/50 pt-4">
                <Button
                  type="button"
                  variant="ghost"
                  onClick={() => setIsModalOpen(false)}
                  className="cursor-pointer text-muted-foreground"
                >
                  Cancel
                </Button>
                <Button type="submit" disabled={saving} className="cursor-pointer px-6 font-semibold">
                  {saving && <Loader2 className="mr-1.5 h-4 w-4 animate-spin" />}
                  Save
                </Button>
              </div>
            </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
