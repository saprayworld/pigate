import React, { useState, useEffect, useMemo } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Settings,
  Activity,
  Lock,
  Clock,
  Database,
  RefreshCw,
  Power,
  ShieldAlert,
  CheckCircle,
  AlertCircle,
  FileDown,
  FileUp,
  Loader2,
  HelpCircle,
  Server,
  CalendarClock
} from "lucide-react"
import { Card, CardContent, CardDescription, CardHeader, CardTitle } from "@/components/ui/card"
import { Button } from "@/components/ui/button"
import { Input } from "@/components/ui/input"
import { Label } from "@/components/ui/label"
import { Switch } from "@/components/ui/switch"
import { Alert, AlertDescription, AlertTitle } from "@/components/ui/alert"
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
import { Tabs, TabsList, TabsTrigger, TabsContent } from "@/components/ui/tabs"
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from "@/components/ui/select"
import { Badge } from "@/components/ui/badge"
import {
  type SystemTimeSettings,
  type NetworkServiceStatus
} from "@/data-mockup/mockData"
import { systemService, type SystemHostnameSettings, type ImportResult } from "@/services/systemService"
import { usePowerControl } from "@/hooks/usePowerControl"
import { authService } from "@/services/authService"
import { useAlert } from "@/hooks/useAlert"
import { useHostname } from "@/hooks/useHostname"
import { buildTimeZoneOptions } from "@/lib/timezones"
import { cn } from "@/lib/utils"

// Shared success/error feedback banner used by every settings form.
function FeedbackAlert({ feedback }: { feedback: { type: "success" | "error"; message: string } | null }) {
  if (!feedback) return null
  const ok = feedback.type === "success"
  return (
    <Alert
      variant={ok ? "default" : "destructive"}
      className={cn("px-3 py-2.5", ok && "border-primary/20 bg-primary/5 text-primary")}
    >
      {ok ? <CheckCircle className="h-4 w-4 text-primary" /> : <AlertCircle className="h-4 w-4" />}
      <AlertDescription className={cn("text-xs", ok && "text-primary")}>
        {feedback.message}
      </AlertDescription>
    </Alert>
  )
}

// Shape of an uploaded backup file once JSON.parse'd; only the fields read for
// the import preview/confirmation flow are modeled here.
interface ParsedBackupFile {
  meta?: {
    hostname?: string
    exportedAt?: string
    encrypted?: boolean
  }
  config?: Record<string, unknown>
}

export default function SettingsMaintenance() {
  const { alert, confirm } = useAlert()
  // Keeps the sidebar brand line in sync when hostname is saved here.
  const { setHostname: setSharedHostname } = useHostname()
  // --- States ---
  const [activeTab, setActiveTab] = useState("settings")

  // Password States
  const [currentPassword, setCurrentPassword] = useState("")
  const [newPassword, setNewPassword] = useState("")
  const [confirmNewPassword, setConfirmNewPassword] = useState("")
  const [passwordFeedback, setPasswordFeedback] = useState<{ type: "success" | "error"; message: string } | null>(null)

  // Time & NTP States
  const [timeSettings, setTimeSettings] = useState<SystemTimeSettings>({
    timezone: "Asia/Bangkok",
    ntpSync: true,
    ntpServer: "pool.ntp.org"
  })
  const [timeFeedback, setTimeFeedback] = useState<{ type: "success" | "error"; message: string } | null>(null)

  // Manual clock (only usable while NTP sync is off)
  const [manualDateTime, setManualDateTime] = useState("")
  const [isSettingTime, setIsSettingTime] = useState(false)

  // Timezone options (400+ zones from the browser IANA db, with GMT offsets).
  // Recomputed only when the selected zone changes so a legacy/unknown stored
  // value is still injected as a selectable option.
  const timeZoneOptions = useMemo(
    () => buildTimeZoneOptions(timeSettings.timezone),
    [timeSettings.timezone]
  )

  // Hostname & DHCP-share States
  const [hostnameSettings, setHostnameSettings] = useState<SystemHostnameSettings>({
    hostname: "",
    shareWithDhcp: false
  })
  const [hostnameFeedback, setHostnameFeedback] = useState<{ type: "success" | "error"; message: string } | null>(null)

  // Power control — action state/overlays are shared with the sidebar user menu
  // via usePowerControl; this page owns only the confirmation dialog toggle.
  const [powerDialog, setPowerDialog] = useState<"reboot" | "shutdown" | null>(null)
  const power = usePowerControl()

  // Backup & Restore States
  const [importFile, setImportFile] = useState<File | null>(null)
  const [backupFeedback, setBackupFeedback] = useState<{ type: "success" | "error"; message: string } | null>(null)
  const [isExporting, setIsExporting] = useState(false)
  const [isImporting, setIsImporting] = useState(false)
  const [includeUsers, setIncludeUsers] = useState(false)
  const [importWarnings, setImportWarnings] = useState<string[]>([])
  const [exportPassphrase, setExportPassphrase] = useState("")
  const [importPassphrase, setImportPassphrase] = useState("")
  const [importFileEncrypted, setImportFileEncrypted] = useState(false)

  // Backup & Restore is gated to super_admin: the payload can contain Wi-Fi
  // passwords and user credential hashes, and the backend endpoints are
  // super_admin-only. UX gating here keeps a read-only admin from a dead end.
  const isSuperAdmin = authService.getRole() === "super_admin"

  // Services States
  const [services, setServices] = useState<NetworkServiceStatus[]>([])
  const [restartingServiceId, setRestartingServiceId] = useState<string | null>(null)
  const [isLoading, setIsLoading] = useState(true)
  const [error, setError] = useState("")

  // --- Load Data ---
  const loadData = async () => {
    setIsLoading(true)
    setError("")
    try {
      const [timeData, servicesData, hostnameData] = await Promise.all([
        systemService.getTimeSettings(),
        systemService.getServices(),
        systemService.getHostname(),
      ])
      setTimeSettings(timeData)
      setServices(servicesData)
      setHostnameSettings(hostnameData)
    } catch (err) {
      setError(getErrorMessage(err) || "Failed to load system settings.")
    } finally {
      setIsLoading(false)
    }
  }

  useEffect(() => {
    // isLoading/error already start at their reset values; avoid a synchronous
    // setState in the effect body
    const initialLoad = async () => {
      try {
        const [timeData, servicesData, hostnameData] = await Promise.all([
          systemService.getTimeSettings(),
          systemService.getServices(),
          systemService.getHostname(),
        ])
        setTimeSettings(timeData)
        setServices(servicesData)
        setHostnameSettings(hostnameData)
      } catch (err) {
        setError(getErrorMessage(err) || "Failed to load system settings.")
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [])

  // --- Handlers ---

  // Password Update
  const handlePasswordChange = async (e: React.FormEvent) => {
    e.preventDefault()
    setPasswordFeedback(null)

    if (!currentPassword || !newPassword || !confirmNewPassword) {
      setPasswordFeedback({ type: "error", message: "กรุณากรอกข้อมูลให้ครบทุกช่อง" })
      return
    }

    if (newPassword.length < 8) {
      setPasswordFeedback({ type: "error", message: "รหัสผ่านใหม่ต้องมีความยาวอย่างน้อย 8 ตัวอักษร" })
      return
    }

    if (newPassword !== confirmNewPassword) {
      setPasswordFeedback({ type: "error", message: "รหัสผ่านใหม่และการยืนยันรหัสผ่านไม่ตรงกัน" })
      return
    }

    try {
      await systemService.changePassword(currentPassword, newPassword)
      setPasswordFeedback({ type: "success", message: "เปลี่ยนรหัสผ่านผู้ดูแลระบบเรียบร้อยแล้ว!" })
      setCurrentPassword("")
      setNewPassword("")
      setConfirmNewPassword("")
    } catch (err) {
      setPasswordFeedback({ type: "error", message: getErrorMessage(err) || "ไม่สามารถเปลี่ยนรหัสผ่านได้" })
    }
  }

  // Save Time & NTP Settings
  const handleSaveTimeSettings = async (e: React.FormEvent) => {
    e.preventDefault()
    setTimeFeedback(null)

    if (timeSettings.ntpSync && !timeSettings.ntpServer.trim()) {
      setTimeFeedback({ type: "error", message: "กรุณาระบุที่อยู่ของ NTP Server สำหรับซิงค์เวลา" })
      return
    }

    try {
      const updated = await systemService.updateTimeSettings(timeSettings)
      setTimeSettings(updated)
      setTimeFeedback({ type: "success", message: "บันทึกการตั้งค่าระบบเวลา และ NTP สำเร็จ!" })
    } catch (err) {
      setTimeFeedback({ type: "error", message: getErrorMessage(err) || "ไม่สามารถบันทึกการตั้งค่าเวลาได้" })
    }
  }

  // Set the wall clock manually. Converts the <input type="datetime-local">
  // (local, no timezone) value to a full RFC3339 timestamp the backend accepts.
  const handleSetManualTime = async () => {
    setTimeFeedback(null)
    if (!manualDateTime) {
      setTimeFeedback({ type: "error", message: "กรุณาเลือกวันที่และเวลาที่ต้องการตั้ง" })
      return
    }
    const parsed = new Date(manualDateTime)
    if (isNaN(parsed.getTime())) {
      setTimeFeedback({ type: "error", message: "รูปแบบวันที่/เวลาไม่ถูกต้อง" })
      return
    }

    setIsSettingTime(true)
    try {
      const updated = await systemService.setManualTime(parsed.toISOString())
      setTimeSettings(updated)
      setManualDateTime("")
      setTimeFeedback({ type: "success", message: "ตั้งเวลาระบบด้วยมือสำเร็จ!" })
    } catch (err) {
      setTimeFeedback({ type: "error", message: getErrorMessage(err) || "ไม่สามารถตั้งเวลาได้" })
    } finally {
      setIsSettingTime(false)
    }
  }

  // Formats a RFC3339 status timestamp for display in the device's locale.
  const formatStatusTime = (iso?: string): string => {
    if (!iso) return "—"
    const d = new Date(iso)
    if (isNaN(d.getTime())) return "—"
    return d.toLocaleString()
  }

  // Save Hostname & Share-with-DHCP Settings
  const HOSTNAME_REGEX = /^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$/
  const handleSaveHostnameSettings = async (e: React.FormEvent) => {
    e.preventDefault()
    setHostnameFeedback(null)

    if (!hostnameSettings.hostname.trim()) {
      setHostnameFeedback({ type: "error", message: "กรุณาระบุชื่อ Hostname" })
      return
    }
    if (!HOSTNAME_REGEX.test(hostnameSettings.hostname)) {
      setHostnameFeedback({ type: "error", message: "Hostname ต้องประกอบด้วยตัวอักษร a-z, A-Z, ตัวเลข 0-9 และเครื่องหมาย - เท่านั้น (ห้ามขึ้นต้นหรือลงท้ายด้วย -)" })
      return
    }

    try {
      await systemService.updateHostname(hostnameSettings)
      // Optimistically sync the sidebar brand line without a page refresh.
      setSharedHostname(hostnameSettings.hostname)
      setHostnameFeedback({ type: "success", message: "บันทึกการตั้งค่า Hostname สำเร็จ! (การเชื่อมต่อ WAN อาจสะดุดชั่วขณะหากเปิด/แก้ไขการ share hostname)" })
    } catch (err) {
      setHostnameFeedback({ type: "error", message: getErrorMessage(err) || "ไม่สามารถบันทึกการตั้งค่า Hostname ได้" })
    }
  }

  // Reboot / Shutdown — close the confirm dialog then hand off to the shared
  // power controller (drives the backend + full-screen status overlays).
  const handleConfirmReboot = () => {
    setPowerDialog(null)
    void power.reboot()
  }

  const handleConfirmShutdown = () => {
    setPowerDialog(null)
    void power.shutdown()
  }

  // Build the download filename per the backup naming convention:
  // pigate-backup-<hostname>-<YYYYMMDD-HHmmss>.json
  const buildBackupFilename = (hostname: string): string => {
    const safeHost = (hostname || "pigate").replace(/[^a-zA-Z0-9_-]/g, "-")
    const d = new Date()
    const p = (n: number) => String(n).padStart(2, "0")
    const ts = `${d.getFullYear()}${p(d.getMonth() + 1)}${p(d.getDate())}-${p(d.getHours())}${p(d.getMinutes())}${p(d.getSeconds())}`
    return `pigate-backup-${safeHost}-${ts}.json`
  }

  // Export Config
  const handleExportConfig = async () => {
    setIsExporting(true)
    setBackupFeedback(null)
    setImportWarnings([])

    try {
      const payload = await systemService.exportConfig(includeUsers, exportPassphrase.trim())
      const blob = new Blob([JSON.stringify(payload, null, 2)], { type: "application/json" })
      const url = URL.createObjectURL(blob)
      const a = document.createElement("a")
      a.href = url
      a.download = buildBackupFilename(payload?.meta?.hostname ?? "pigate")
      document.body.appendChild(a)
      a.click()
      document.body.removeChild(a)
      URL.revokeObjectURL(url)

      const encNote = exportPassphrase.trim() ? " (เข้ารหัสด้วยรหัสผ่านแล้ว)" : ""
      setBackupFeedback({
        type: "success",
        message: includeUsers
          ? `ส่งออกไฟล์สำรองข้อมูลสำเร็จ (รวมบัญชีผู้ใช้)${encNote} — ไฟล์มีรหัสผ่าน Wi-Fi และข้อมูลบัญชี โปรดเก็บรักษาอย่างปลอดภัย`
          : `ส่งออกไฟล์สำรองข้อมูล (Configuration Export) สำเร็จแล้ว!${encNote} ไฟล์มีรหัสผ่าน Wi-Fi โปรดเก็บรักษาอย่างปลอดภัย`,
      })
    } catch (err) {
      setBackupFeedback({ type: "error", message: getErrorMessage(err) || "ไม่สามารถส่งออกไฟล์สำรองข้อมูลได้" })
    } finally {
      setIsExporting(false)
    }
  }

  // Import Config — confirm (with a preview from the file's meta) before the
  // destructive wipe & restore.
  const handleImportConfig = async (e: React.FormEvent) => {
    e.preventDefault()
    setBackupFeedback(null)
    setImportWarnings([])

    if (!importFile) {
      setBackupFeedback({ type: "error", message: "กรุณาเลือกไฟล์ JSON ของคุณที่บันทึกไว้ก่อนกดปุ่มนำเข้า" })
      return
    }
    if (!importFile.name.endsWith(".json")) {
      setBackupFeedback({ type: "error", message: "รูปแบบไฟล์ไม่ถูกต้อง โปรดใช้ไฟล์นามสกุล .json เท่านั้น" })
      return
    }

    let parsed: ParsedBackupFile
    try {
      parsed = JSON.parse(await importFile.text())
    } catch {
      setBackupFeedback({ type: "error", message: "ไฟล์ไม่ใช่ JSON ที่ถูกต้อง" })
      return
    }

    // Encrypted files need a passphrase before anything can be previewed.
    const isEncrypted = parsed?.meta?.encrypted === true
    if (isEncrypted && !importPassphrase.trim()) {
      setBackupFeedback({ type: "error", message: "ไฟล์นี้ถูกเข้ารหัส กรุณากรอกรหัสผ่านสำหรับถอดรหัสก่อนนำเข้า" })
      return
    }

    // Build a human preview from the file metadata (v2) or fall back for v1.
    const meta = parsed?.meta
    const cfg = parsed?.config ?? {}
    const userCount = Array.isArray(cfg.users) ? cfg.users.length : 0
    const fileHasUsers = userCount > 0
    const sectionLine = (label: string, v: unknown) =>
      Array.isArray(v) && v.length > 0 ? `\n• ${label}: ${v.length}` : ""
    const previewLines = [
      meta?.hostname ? `อุปกรณ์ต้นทาง: ${meta.hostname}` : "ไฟล์รูปแบบเดิม (v1)",
      meta?.exportedAt ? `สำรองเมื่อ: ${new Date(meta.exportedAt).toLocaleString()}` : "",
      isEncrypted ? "\n🔒 ไฟล์ถูกเข้ารหัส — จะถอดรหัสด้วยรหัสผ่านที่กรอกไว้" : "",
      sectionLine("Interfaces", cfg.interfaces),
      sectionLine("Static Routes", cfg.staticRoutes ?? cfg.routes),
      sectionLine("Address Objects", cfg.addresses),
      sectionLine("Service Objects", cfg.serviceObjects),
      sectionLine("Firewall Policies", cfg.policies),
      sectionLine("DHCP Configs", cfg.dhcpConfigs),
      sectionLine("DNS Zones", cfg.dnsZones),
      sectionLine("QoS Rules", cfg.qosRules),
      fileHasUsers ? `\n• บัญชีผู้ใช้: ${userCount}` : "",
    ]
      .filter(Boolean)
      .join("")

    const willImportUsers = includeUsers && fileHasUsers
    const warningBody =
      `${previewLines}\n\n` +
      "⚠️ การนำเข้าจะ เขียนทับการตั้งค่าทั้งหมด บนอุปกรณ์นี้ (แบบ Replace) แล้วสั่ง Apply ใหม่ทันที\n" +
      "• การเปลี่ยน IP ของ Interface อาจทำให้หลุดการเชื่อมต่อกับหน้าจัดการนี้ และต้องเชื่อมต่อใหม่\n" +
      (willImportUsers
        ? "• จะเขียนทับบัญชีผู้ใช้ทั้งหมดด้วยข้อมูลในไฟล์ (บัญชีของคุณจะถูกคงไว้เพื่อกันหลุดสิทธิ์)\n"
        : fileHasUsers
          ? "• ไฟล์มีบัญชีผู้ใช้ แต่จะไม่ถูกนำเข้า (ไม่ได้เปิดตัวเลือก “รวมบัญชีผู้ใช้”)\n"
          : "") +
      "\nต้องการดำเนินการต่อหรือไม่?"

    const ok = await confirm("ยืนยันการนำเข้าและเขียนทับคอนฟิก", warningBody)
    if (!ok) return

    setIsImporting(true)
    try {
      const result: ImportResult = await systemService.importConfig(parsed, includeUsers, importPassphrase.trim())

      const total = Object.values(result.counts || {}).reduce((a, b) => a + b, 0)
      setBackupFeedback({
        type: "success",
        message: `นำเข้าไฟล์ "${importFile.name}" สำเร็จ! คืนค่า ${total} รายการและสั่ง Apply เรียบร้อยแล้ว${result.interfacesChanged ? " — การตั้งค่า Interface มีการเปลี่ยนแปลง อาจต้องเชื่อมต่อใหม่" : ""}`,
      })
      setImportWarnings(result.warnings || [])
      setImportFile(null)
      setImportPassphrase("")
      setImportFileEncrypted(false)
      const fileInput = document.getElementById("import-file-input") as HTMLInputElement
      if (fileInput) fileInput.value = ""

      await loadData()
    } catch (err) {
      setBackupFeedback({ type: "error", message: getErrorMessage(err) || "ไม่สามารถนำเข้าไฟล์สำรองข้อมูลได้" })
    } finally {
      setIsImporting(false)
    }
  }

  // Restart Service Action
  const handleRestartService = async (id: string) => {
    setRestartingServiceId(id)
    try {
      await systemService.restartService(id)
      const updatedServices = await systemService.getServices()
      setServices(updatedServices)
    } catch (err) {
      await alert("ข้อผิดพลาด", "Failed to restart service: " + getErrorMessage(err))
    } finally {
      setRestartingServiceId(null)
    }
  }

  // --- Render Loading / Error States ---
  if (isLoading) {
    return (
      <div className="flex flex-col items-center justify-center min-h-[400px] space-y-4">
        <Loader2 className="h-8 w-8 animate-spin text-primary" />
        <span className="text-sm text-muted-foreground font-semibold">กำลังโหลดข้อมูล Settings...</span>
      </div>
    )
  }

  if (error) {
    return (
      <div className="p-6">
        <Alert variant="destructive">
          <AlertCircle className="h-4 w-4" />
          <AlertTitle>Error Loading Settings</AlertTitle>
          <AlertDescription className="text-xs">{error}</AlertDescription>
        </Alert>
      </div>
    )
  }

  // Reboot / shutdown / powered-off full-screen overlays are rendered by the
  // shared usePowerControl().overlay (see the end of this component's tree).

  return (
    <div className="space-y-4">
      {power.overlay}
      {/* Tabs selection */}
      <Tabs value={activeTab} onValueChange={setActiveTab} className="w-full">
        <TabsList className="grid grid-cols-2 max-w-[320px]">
          <TabsTrigger value="settings" className="font-bold flex items-center gap-1.5">
            <Settings className="h-4 w-4" /> Setup Settings
          </TabsTrigger>
          <TabsTrigger value="maintenance" className="font-bold flex items-center gap-1.5">
            <Activity className="h-4 w-4" /> Maintenance
          </TabsTrigger>
        </TabsList>

        {/* ==================== TAB 1: SETTINGS ==================== */}
        <TabsContent value="settings" className="mt-4 space-y-4">
          <div className="grid gap-4 md:grid-cols-2">

            {/* Card: Administrator Password */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base font-semibold">
                  <Lock className="h-4 w-4 text-muted-foreground" />
                  Administrator Password (รหัสผ่านผู้ดูแลระบบ)
                </CardTitle>
                <CardDescription className="text-xs text-muted-foreground">
                  เปลี่ยนรหัสผ่านสำหรับเข้าสู่ระบบส่วนควบคุม PiGate Web Panel
                </CardDescription>
              </CardHeader>
              <CardContent>
                <form onSubmit={handlePasswordChange} className="space-y-4">
                  <FeedbackAlert feedback={passwordFeedback} />

                  <div className="space-y-1.5">
                    <Label htmlFor="curr-pass" className="block text-xs font-medium text-muted-foreground">
                      Current Password (รหัสผ่านปัจจุบัน)
                    </Label>
                    <Input
                      id="curr-pass"
                      type="password"
                      placeholder="รหัสผ่านปัจจุบัน"
                      value={currentPassword}
                      onChange={(e) => setCurrentPassword(e.target.value)}
                      className="h-9"
                    />
                  </div>

                  <div className="space-y-1.5">
                    <Label htmlFor="new-pass" className="block text-xs font-medium text-muted-foreground">
                      New Password (รหัสผ่านใหม่)
                    </Label>
                    <Input
                      id="new-pass"
                      type="password"
                      placeholder="รหัสผ่านใหม่ (ไม่ต่ำกว่า 8 อักษร)"
                      value={newPassword}
                      onChange={(e) => setNewPassword(e.target.value)}
                      className="h-9"
                    />
                  </div>

                  <div className="space-y-1.5">
                    <Label htmlFor="conf-pass" className="block text-xs font-medium text-muted-foreground">
                      Confirm New Password (ยืนยันรหัสผ่านใหม่)
                    </Label>
                    <Input
                      id="conf-pass"
                      type="password"
                      placeholder="ยืนยันรหัสผ่านใหม่"
                      value={confirmNewPassword}
                      onChange={(e) => setConfirmNewPassword(e.target.value)}
                      className="h-9"
                    />
                  </div>

                  <Button
                    type="submit"
                    className="cursor-pointer mt-2 w-full gap-2 font-semibold"
                  >
                    <Lock className="h-4 w-4" />
                    Change Password
                  </Button>
                </form>
              </CardContent>
            </Card>

            {/* Card: System Identity (Hostname + Share with DHCP) */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base font-semibold">
                  <Server className="h-4 w-4 text-muted-foreground" />
                  System Identity (ชื่อเครื่อง)
                </CardTitle>
                <CardDescription className="text-xs text-muted-foreground">
                  กำหนดชื่อเครื่อง (Hostname) ของอุปกรณ์เกตเวย์ และเลือกว่าจะส่งชื่อนี้ไปบอก Router ฝั่ง WAN ผ่าน DHCP หรือไม่
                </CardDescription>
              </CardHeader>
              <CardContent>
                <form onSubmit={handleSaveHostnameSettings} className="space-y-4">
                  <FeedbackAlert feedback={hostnameFeedback} />

                  <div className="space-y-1.5">
                    <Label htmlFor="hostname" className="block text-xs font-medium text-muted-foreground">
                      Hostname (ชื่อเครื่อง)
                    </Label>
                    <Input
                      id="hostname"
                      type="text"
                      value={hostnameSettings.hostname}
                      onChange={(e) => setHostnameSettings(prev => ({ ...prev, hostname: e.target.value }))}
                      className="h-9 font-mono"
                      placeholder="เช่น PiGate-RPI5"
                    />
                    <p className="text-[11px] text-muted-foreground italic">
                      ใช้ได้เฉพาะตัวอักษร a-z, A-Z, ตัวเลข 0-9 และเครื่องหมาย - (ไม่เกิน 63 ตัวอักษร)
                    </p>
                  </div>

                  <div className="space-y-1.5 pt-2">
                    <div className="flex items-center justify-between">
                      <Label htmlFor="share-hostname" className="cursor-pointer text-xs font-medium text-muted-foreground">
                        Share hostname with DHCP (ส่งชื่อเครื่องไปบอก Router ฝั่ง WAN)
                      </Label>
                      <Switch
                        id="share-hostname"
                        checked={hostnameSettings.shareWithDhcp}
                        onCheckedChange={(checked) => setHostnameSettings(prev => ({ ...prev, shareWithDhcp: checked }))}
                      />
                    </div>
                    <p className="text-[11px] text-muted-foreground italic">
                      หากเปิดใช้งาน dhcpcd จะส่งชื่อเครื่องนี้ไปบอก Router ผ่าน DHCP Option 12 — การเปลี่ยนค่านี้อาจทำให้การเชื่อมต่อ WAN สะดุดชั่วขณะ (renew lease)
                    </p>
                  </div>

                  <Button
                    type="submit"
                    className="cursor-pointer mt-2 w-full gap-2 font-semibold"
                  >
                    <Server className="h-4 w-4" />
                    Save Hostname Settings
                  </Button>
                </form>
              </CardContent>
            </Card>

            {/* Card: System Time & NTP */}
            <Card>
              <CardHeader>
                <CardTitle className="flex items-center gap-2 text-base font-semibold">
                  <Clock className="h-4 w-4 text-muted-foreground" />
                  System Time & NTP (เวลาและเขตเวลา)
                </CardTitle>
                <CardDescription className="text-xs text-muted-foreground">
                  กำหนดเขตเวลาของอุปกรณ์เกตเวย์ และเปิดการซิงค์เวลาอัตโนมัติผ่านอินเทอร์เน็ต
                </CardDescription>
              </CardHeader>
              <CardContent>
                <form onSubmit={handleSaveTimeSettings} className="space-y-4">
                  <FeedbackAlert feedback={timeFeedback} />

                  {/* Live status: current device time + sync state */}
                  <div className="flex items-center justify-between rounded-lg border border-border bg-muted/50 px-3 py-2.5">
                    <div className="space-y-0.5">
                      <p className="text-[11px] font-medium text-muted-foreground">
                        เวลาปัจจุบันของเครื่อง
                      </p>
                      <p className="font-mono text-sm text-foreground">
                        {formatStatusTime(timeSettings.status?.currentTime)}
                      </p>
                    </div>
                    {timeSettings.status?.ntpSynchronized ? (
                      <Badge variant="default" className="gap-1 bg-primary/10 text-primary">
                        <CheckCircle className="h-3 w-3" />
                        Synchronized
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="gap-1">
                        <AlertCircle className="h-3 w-3" />
                        Not synced
                      </Badge>
                    )}
                  </div>

                  <div className="space-y-1.5">
                    <Label htmlFor="timezone" className="block text-xs font-medium text-muted-foreground">
                      Time Zone (เขตเวลา)
                    </Label>
                    <Select
                      value={timeSettings.timezone}
                      onValueChange={(value) => setTimeSettings(prev => ({ ...prev, timezone: value }))}
                    >
                      <SelectTrigger id="timezone" className="w-full h-9 text-xs bg-background">
                        <SelectValue placeholder="เลือกเขตเวลา" />
                      </SelectTrigger>
                      <SelectContent className="max-h-72">
                        {timeZoneOptions.map((tz) => (
                          <SelectItem key={tz.value} value={tz.value} className="text-xs">
                            {tz.label}
                          </SelectItem>
                        ))}
                      </SelectContent>
                    </Select>
                  </div>

                  <div className="space-y-3 pt-2">
                    <div className="flex items-center justify-between">
                      <Label htmlFor="ntp-sync" className="cursor-pointer text-xs font-medium text-muted-foreground">
                        NTP Server Sync (เปิดใช้งานการซิงค์เวลาอัตโนมัติ)
                      </Label>
                      <Switch
                        id="ntp-sync"
                        checked={timeSettings.ntpSync}
                        onCheckedChange={(checked) => setTimeSettings(prev => ({ ...prev, ntpSync: checked }))}
                      />
                    </div>

                    <div className="space-y-1.5">
                      <Label htmlFor="ntp-server" className="block text-xs font-medium text-muted-foreground">
                        NTP Server Address
                      </Label>
                      <Input
                        id="ntp-server"
                        type="text"
                        disabled={!timeSettings.ntpSync}
                        value={timeSettings.ntpServer}
                        onChange={(e) => setTimeSettings(prev => ({ ...prev, ntpServer: e.target.value }))}
                        className="h-9 font-mono"
                        placeholder="เช่น pool.ntp.org"
                      />
                      <p className="text-[11px] text-muted-foreground italic">
                        ระบุที่อยู่ไอพีหรือโดเมนเนมของ NTP Server ที่จะไปดึงค่าเวลา (คั่นหลายตัวด้วยช่องว่างได้)
                      </p>
                    </div>
                  </div>

                  <Button
                    type="submit"
                    className="cursor-pointer mt-2 w-full gap-2 font-semibold"
                  >
                    <Clock className="h-4 w-4" />
                    Save Time Settings
                  </Button>

                  {/* Manual clock — only when NTP sync is off (timedated rejects
                      SetTime while NTP is on, so we hide it rather than error) */}
                  {!timeSettings.ntpSync && (
                    <div className="mt-2 space-y-2 border-t border-border/50 pt-4">
                      <Label htmlFor="manual-time" className="block text-xs font-medium text-muted-foreground">
                        ตั้งวันที่/เวลาด้วยมือ
                      </Label>
                      <div className="flex gap-2">
                        <Input
                          id="manual-time"
                          type="datetime-local"
                          value={manualDateTime}
                          onChange={(e) => setManualDateTime(e.target.value)}
                          className="h-9 font-mono"
                        />
                        <Button
                          type="button"
                          onClick={handleSetManualTime}
                          disabled={isSettingTime || !manualDateTime}
                          className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold gap-2 h-9 shrink-0"
                        >
                          {isSettingTime ? <Loader2 className="h-4 w-4 animate-spin" /> : <CalendarClock className="h-4 w-4" />}
                          Set Time
                        </Button>
                      </div>
                      <p className="text-[11px] text-muted-foreground italic leading-relaxed">
                        ⚠️ การตั้งเวลาด้วยมืออาจทำให้เซสชัน/โทเคนหมดอายุผิดเวลา และการตรวจสอบใบรับรอง TLS ผิดพลาดได้ —
                        แนะนำให้ใช้การซิงค์อัตโนมัติ (NTP) เป็นหลัก และหากบอร์ดไม่มีถ่าน RTC เวลาที่ตั้งไว้อาจเพี้ยนหลังไฟดับ
                      </p>
                    </div>
                  )}
                </form>
              </CardContent>
            </Card>

          </div>
        </TabsContent>

        {/* ==================== TAB 2: MAINTENANCE ==================== */}
        <TabsContent value="maintenance" className="mt-4 space-y-4">
          <div className="grid gap-4 md:grid-cols-2">

            {/* Left Column Container */}
            <div className="space-y-4">

              {/* Card: System Actions (Power control) */}
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base font-semibold">
                    <Power className="h-4 w-4 text-muted-foreground" />
                    System Actions (ควบคุมพลังงานบอร์ด)
                  </CardTitle>
                  <CardDescription className="text-xs text-muted-foreground">
                    รีบูตระบบ หรือสั่งปิดเครื่องอุปกรณ์ Raspberry Pi 5 เมื่อต้องการหยุดทำงาน
                  </CardDescription>
                </CardHeader>
                <CardContent className="space-y-4">
                  <p className="text-xs text-muted-foreground leading-relaxed">
                    การทำรายการพลังงานจะทำการปิดการเชื่อมต่อเครือข่ายและเกตเวย์ทั้งหมดชั่วขณะ โปรดตรวจสอบการทำงานของผู้ใช้ที่เชื่อมต่อผ่านเกตเวย์นี้อยู่
                  </p>
                  <div className="flex flex-wrap gap-3 pt-2">
                    <Button
                      type="button"
                      variant="destructive"
                      onClick={() => setPowerDialog("reboot")}
                      className="cursor-pointer gap-1.5"
                    >
                      <RefreshCw className="h-4 w-4" />
                      Reboot System (รีบูตเครื่อง)
                    </Button>
                    <Button
                      type="button"
                      variant="destructive"
                      onClick={() => setPowerDialog("shutdown")}
                      className="cursor-pointer gap-1.5"
                    >
                      <Power className="h-4 w-4" />
                      Shutdown System (ปิดเครื่อง)
                    </Button>
                  </div>
                </CardContent>
              </Card>

              {/* Card: Backup & Restore */}
              <Card>
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base font-semibold">
                    <Database className="h-4 w-4 text-muted-foreground" />
                    Backup & Restore (สำรองและคืนค่าคอนฟิก)
                  </CardTitle>
                  <CardDescription className="text-xs text-muted-foreground">
                    ดาวน์โหลดหรืออัปโหลดไฟล์ข้อมูลนโยบายความปลอดภัยและรายการวัตถุ
                  </CardDescription>
                </CardHeader>
                <CardContent>
                  {!isSuperAdmin ? (
                    <Alert className="border-border/50 bg-muted/30 py-2.5 px-3">
                      <ShieldAlert className="h-4 w-4 text-muted-foreground" />
                      <AlertDescription className="text-xs text-muted-foreground">
                        เฉพาะผู้ดูแลระบบสูงสุด (Super Admin) เท่านั้นที่สามารถสำรอง/คืนค่าคอนฟิกได้ เนื่องจากไฟล์อาจมีรหัสผ่าน Wi-Fi และข้อมูลบัญชีผู้ใช้
                      </AlertDescription>
                    </Alert>
                  ) : (
                  <form onSubmit={handleImportConfig} className="space-y-4">
                    <FeedbackAlert feedback={backupFeedback} />

                    {importWarnings.length > 0 && (
                      <Alert className="border-border/50 bg-muted/30 py-2.5 px-3">
                        <AlertCircle className="h-4 w-4 text-muted-foreground" />
                        <AlertTitle className="text-xs font-semibold">คำเตือนระหว่างนำเข้า ({importWarnings.length})</AlertTitle>
                        <AlertDescription className="text-[11px] text-muted-foreground">
                          <ul className="list-disc pl-4 space-y-0.5 mt-1">
                            {importWarnings.map((w, i) => (
                              <li key={i}>{w}</li>
                            ))}
                          </ul>
                        </AlertDescription>
                      </Alert>
                    )}

                    {/* Export Section */}
                    <div className="space-y-3 border-b border-border/50 pb-4">
                      <Label className="block text-xs font-medium text-muted-foreground">
                        สำรองข้อมูลปัจจุบัน (Configuration Export)
                      </Label>

                      <div className="flex items-start justify-between gap-3 rounded-lg border border-border bg-muted/50 px-3 py-2">
                        <div className="space-y-0.5">
                          <Label htmlFor="include-users-switch" className="text-xs font-medium cursor-pointer">
                            รวมบัญชีผู้ใช้ (Include user accounts)
                          </Label>
                          <p className="text-[10px] text-muted-foreground">
                            ไฟล์จะมีข้อมูลบัญชีและรหัสผ่าน (แบบเข้ารหัส) เพิ่มความเสี่ยง โปรดเก็บรักษาอย่างดี
                          </p>
                        </div>
                        <Switch
                          id="include-users-switch"
                          checked={includeUsers}
                          onCheckedChange={setIncludeUsers}
                          className="mt-0.5"
                        />
                      </div>

                      <div className="space-y-1">
                        <Label htmlFor="export-passphrase" className="text-xs font-medium">
                          เข้ารหัสไฟล์ด้วยรหัสผ่าน (ไม่บังคับ)
                        </Label>
                        <Input
                          id="export-passphrase"
                          type="password"
                          autoComplete="new-password"
                          placeholder="ปล่อยว่างเพื่อไม่เข้ารหัส"
                          value={exportPassphrase}
                          onChange={(e) => setExportPassphrase(e.target.value)}
                          className="h-9 text-xs"
                        />
                        <p className="text-[10px] text-muted-foreground">
                          หากตั้งรหัสผ่าน เนื้อหาคอนฟิกจะถูกเข้ารหัส (AES-256-GCM) — ต้องใช้รหัสผ่านเดิมตอนนำเข้า และกู้คืนไม่ได้หากลืม
                        </p>
                      </div>

                      <p className="text-[10px] text-muted-foreground italic flex items-start gap-1">
                        <ShieldAlert className="h-3 w-3 mt-0.5 shrink-0" />
                        {exportPassphrase.trim()
                          ? "ไฟล์จะถูกเข้ารหัส — เก็บรหัสผ่านไว้ให้ดี"
                          : "ไฟล์สำรองมีรหัสผ่าน Wi-Fi ในรูปแบบข้อความ โปรดจัดเก็บไฟล์ในที่ปลอดภัย (แนะนำให้ตั้งรหัสผ่านเข้ารหัส)"}
                      </p>

                      <Button
                        type="button"
                        onClick={handleExportConfig}
                        disabled={isExporting}
                        variant="outline"
                        className="cursor-pointer gap-1.5 font-semibold"
                      >
                        {isExporting ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          <FileDown className="h-4 w-4" />
                        )}
                        Export Configuration File (.json)
                      </Button>
                    </div>

                    {/* Import Section */}
                    <div className="space-y-3 pt-2">
                      <Label className="block text-xs font-medium text-muted-foreground">
                        คืนค่าจากไฟล์สำรอง (Configuration Import)
                      </Label>
                      <div className="space-y-2">
                        <input
                          id="import-file-input"
                          type="file"
                          accept=".json"
                          onChange={async (e) => {
                            const f = e.target.files && e.target.files.length > 0 ? e.target.files[0] : null
                            setImportFile(f)
                            setImportPassphrase("")
                            setImportFileEncrypted(false)
                            if (f) {
                              try {
                                const parsed = JSON.parse(await f.text())
                                setImportFileEncrypted(parsed?.meta?.encrypted === true)
                              } catch {
                                /* invalid JSON is reported on submit */
                              }
                            }
                          }}
                          className="w-full border border-border rounded-lg text-xs text-muted-foreground file:mr-3 file:py-1.5 file:px-3 file:rounded-l-lg file:border-0 file:border-r file:border-border file:bg-primary file:text-primary-foreground file:text-xs file:font-semibold cursor-pointer file:cursor-pointer"
                        />
                        <p className="text-[10px] text-muted-foreground italic">
                          * ระบบจะ เขียนทับคอนฟิกทั้งหมด (Replace) แล้วสั่ง Apply ใหม่ทันที — การเปลี่ยน IP ของ Interface อาจทำให้หลุดการเชื่อมต่อ
                        </p>
                      </div>

                      {importFileEncrypted && (
                        <div className="space-y-1">
                          <Label htmlFor="import-passphrase" className="text-xs font-medium flex items-center gap-1">
                            <Lock className="h-3 w-3" /> รหัสผ่านสำหรับถอดรหัสไฟล์
                          </Label>
                          <Input
                            id="import-passphrase"
                            type="password"
                            autoComplete="off"
                            placeholder="กรอกรหัสผ่านที่ใช้ตอนส่งออก"
                            value={importPassphrase}
                            onChange={(e) => setImportPassphrase(e.target.value)}
                            className="h-9 text-xs"
                          />
                        </div>
                      )}

                      <Button
                        type="submit"
                        disabled={isImporting || !importFile || (importFileEncrypted && !importPassphrase.trim())}
                        className="cursor-pointer w-full gap-1.5 font-semibold"
                      >
                        {isImporting ? (
                          <Loader2 className="h-4 w-4 animate-spin" />
                        ) : (
                          <FileUp className="h-4 w-4" />
                        )}
                        Import & Apply Config
                      </Button>
                    </div>
                  </form>
                  )}
                </CardContent>
              </Card>

            </div>

            {/* Right Column Container: Network services list */}
            <div className="space-y-6">

              {/* Card: Services Control */}
              <Card className="h-full">
                <CardHeader>
                  <CardTitle className="flex items-center gap-2 text-base font-semibold">
                    <Activity className="h-4 w-4 text-muted-foreground" />
                    Network Services Status (ควบคุมบริการย่อย)
                  </CardTitle>
                  <CardDescription className="text-xs text-muted-foreground">
                    ควบคุมและดูสถานะบริการที่สำคัญในระบบปฏิบัติการ Linux ของ PiGate
                  </CardDescription>
                </CardHeader>
                <CardContent className="p-0">
                  <Table>
                    <TableHeader>
                      <TableRow className="hover:bg-transparent">
                        <TableHead className="text-xs font-medium text-muted-foreground">Service Engine</TableHead>
                        <TableHead className="w-[30%] text-xs font-medium text-muted-foreground">Status</TableHead>
                        <TableHead className="w-[25%] text-right text-xs font-medium text-muted-foreground">Actions</TableHead>
                      </TableRow>
                    </TableHeader>
                    <TableBody>
                      {services.map((srv) => (
                        <TableRow key={srv.id}>
                          <TableCell className="py-3 font-medium text-foreground">
                            <div>{srv.name}</div>
                            <div className="mt-0.5 font-mono text-[10px] text-muted-foreground">({srv.serviceName})</div>
                          </TableCell>
                          <TableCell className="py-3">
                            {srv.status === "running" && (
                              <span className="flex items-center gap-1.5 text-xs font-semibold text-primary">
                                <span className="h-2 w-2 rounded-full bg-primary" />
                                Running
                              </span>
                            )}
                            {srv.status === "stopped" && (
                              <span className="flex items-center gap-1.5 text-xs font-semibold text-destructive">
                                <span className="h-2 w-2 rounded-full bg-destructive" />
                                Restarting...
                              </span>
                            )}
                          </TableCell>
                          <TableCell className="py-3 text-right">
                            <Button
                              variant="outline"
                              size="sm"
                              disabled={restartingServiceId !== null}
                              onClick={() => handleRestartService(srv.id)}
                              className="cursor-pointer gap-1.5"
                            >
                              {restartingServiceId === srv.id ? (
                                <Loader2 className="h-3.5 w-3.5 animate-spin text-primary" />
                              ) : (
                                <RefreshCw className="h-3.5 w-3.5 text-muted-foreground" />
                              )}
                              Restart
                            </Button>
                          </TableCell>
                        </TableRow>
                      ))}
                    </TableBody>
                  </Table>

                  <div className="mx-4 mb-4 mt-2 flex gap-2 rounded-lg border border-border bg-muted/50 p-3 text-xs leading-relaxed text-muted-foreground">
                    <HelpCircle className="mt-0.5 h-4 w-4 shrink-0" />
                    <span>
                      บริการเครือข่ายเหล่านี้ทำงานอยู่บนระบบปฏิบัติการหลักของบอร์ด การรีสตาร์ทบริการอาจทำให้ทราฟฟิกบางประเภทหยุดทำงานชั่วครู่
                    </span>
                  </div>
                </CardContent>
              </Card>

            </div>

          </div>
        </TabsContent>
      </Tabs>

      {/* Reboot System Confirmation Dialog */}
      <Dialog open={powerDialog === "reboot"} modal={false} onOpenChange={(open) => !open && setPowerDialog(null)}>
        <DialogContent className="w-full max-w-[400px] gap-4 rounded-xl p-6">
          <DialogHeader className="border-b border-border/50 pb-3">
            <DialogTitle className="flex items-center gap-2 text-base font-semibold">
              <ShieldAlert className="h-4 w-4 text-destructive" />
              ยืนยันการรีบูตระบบ
            </DialogTitle>
          </DialogHeader>
          <div className="py-2 text-sm leading-relaxed text-muted-foreground">
            คุณต้องการสั่ง <span className="font-semibold text-destructive">รีบูตเครื่อง (Reboot)</span> บอร์ด PiGate ใช่หรือไม่? การเชื่อมต่อเครือข่ายทั้งหมดผ่านพอร์ต WAN/LAN จะสิ้นสุดชั่วคราวจนกว่าระบบจะกลับมาทำงานอีกครั้ง
          </div>
          <div className="flex items-center justify-end gap-3 pt-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => setPowerDialog(null)}
              className="cursor-pointer text-muted-foreground"
            >
              ยกเลิก
            </Button>
            <Button
              type="button"
              variant="destructive"
              onClick={handleConfirmReboot}
              className="cursor-pointer font-semibold"
            >
              ยืนยัน รีบูต
            </Button>
          </div>
        </DialogContent>
      </Dialog>

      {/* Shutdown System Confirmation Dialog */}
      <Dialog open={powerDialog === "shutdown"} modal={false} onOpenChange={(open) => !open && setPowerDialog(null)}>
        <DialogContent className="w-full max-w-[400px] gap-4 rounded-xl p-6">
          <DialogHeader className="border-b border-border/50 pb-3">
            <DialogTitle className="flex items-center gap-2 text-base font-semibold">
              <ShieldAlert className="h-4 w-4 text-destructive" />
              ยืนยันการปิดเครื่อง
            </DialogTitle>
          </DialogHeader>
          <div className="py-2 text-sm leading-relaxed text-muted-foreground">
            คุณต้องการสั่ง <span className="font-semibold text-foreground">ปิดระบบ (Shutdown)</span> ใช่หรือไม่? ตัวเครื่องจะหยุดทำงานและระบบจะตัดการจ่ายกำลังไฟเลี้ยงบอร์ด
          </div>
          <div className="flex items-center justify-end gap-3 pt-2">
            <Button
              type="button"
              variant="ghost"
              onClick={() => setPowerDialog(null)}
              className="cursor-pointer text-muted-foreground"
            >
              ยกเลิก
            </Button>
            <Button
              type="button"
              variant="destructive"
              className="cursor-pointer font-semibold"
              onClick={handleConfirmShutdown}
            >
              ยืนยัน ปิดเครื่อง
            </Button>
          </div>
        </DialogContent>
      </Dialog>
    </div>
  )
}
