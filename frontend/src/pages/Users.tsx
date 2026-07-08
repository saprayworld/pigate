import { useState, useMemo, useEffect } from "react"
import { getErrorMessage } from "@/lib/errors"
import {
  Users as UsersIcon,
  Plus,
  Edit,
  Trash2,
  AlertCircle,
  Loader2,
  ShieldCheck,
  Eye,
  Ban,
  User as UserIcon
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
import { useAlert } from "@/hooks/useAlert"
import { authService } from "@/services/authService"
import {
  userService,
  type UserAccount,
  type UserRole,
} from "@/services/userService"

const USERNAME_REGEX = /^[a-zA-Z0-9_]{3,32}$/
const MIN_PASSWORD_LENGTH = 8

// Helper: Dashboard-style stat card (mirrors Dashboard's StatCard)
function StatCard({
  icon: Icon,
  title,
  value,
}: {
  icon: typeof UsersIcon
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

export default function Users() {
  const { alert, confirm } = useAlert()
  const currentUsername = authService.getUsername()

  // --- State ---
  const [users, setUsers] = useState<UserAccount[]>([])
  const [isLoading, setIsLoading] = useState(true)
  const [busyId, setBusyId] = useState<string | null>(null)

  // Modal state
  const [isModalOpen, setIsModalOpen] = useState(false)
  const [editingUser, setEditingUser] = useState<UserAccount | null>(null)

  // Form fields
  const [formUsername, setFormUsername] = useState("")
  const [formPassword, setFormPassword] = useState("")
  const [formConfirm, setFormConfirm] = useState("")
  const [formRole, setFormRole] = useState<UserRole>("admin_readonly")
  const [formError, setFormError] = useState("")
  const [isSaving, setIsSaving] = useState(false)

  const loadUsers = async (showLoading = true) => {
    if (showLoading) setIsLoading(true)
    try {
      const data = await userService.getAll()
      setUsers(data)
    } catch (err) {
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดรายชื่อผู้ใช้ได้: " + getErrorMessage(err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    // isLoading already starts true; avoid a synchronous setState in the effect body
    const initialLoad = async () => {
      try {
        const data = await userService.getAll()
        setUsers(data)
      } catch (err) {
        await alert("ข้อผิดพลาด", "ไม่สามารถโหลดรายชื่อผู้ใช้ได้: " + getErrorMessage(err))
      } finally {
        setIsLoading(false)
      }
    }
    initialLoad()
  }, [alert])

  const stats = useMemo(() => {
    const total = users.length
    const superAdmins = users.filter((u) => u.role === "super_admin").length
    const readonly = users.filter((u) => u.role === "admin_readonly").length
    const disabled = users.filter((u) => u.status === "disabled").length
    return { total, superAdmins, readonly, disabled }
  }, [users])

  const isSelf = (u: UserAccount) => !!currentUsername && u.username === currentUsername

  // --- Modal helpers ---
  const openCreateModal = () => {
    setEditingUser(null)
    setFormUsername("")
    setFormPassword("")
    setFormConfirm("")
    setFormRole("admin_readonly")
    setFormError("")
    setIsModalOpen(true)
  }

  const openEditModal = (u: UserAccount) => {
    setEditingUser(u)
    setFormUsername(u.username)
    setFormPassword("")
    setFormConfirm("")
    setFormRole(u.role)
    setFormError("")
    setIsModalOpen(true)
  }

  const handleSave = async (e: React.FormEvent) => {
    e.preventDefault()
    setFormError("")

    // Password is required on create; optional on edit (blank = keep current).
    const wantsPassword = !editingUser || formPassword.length > 0

    if (!editingUser) {
      if (!USERNAME_REGEX.test(formUsername)) {
        setFormError("ชื่อผู้ใช้ต้องมี 3-32 ตัวอักษร ใช้ได้เฉพาะ a-z A-Z 0-9 และ _")
        return
      }
    }

    if (wantsPassword) {
      if (formPassword.length < MIN_PASSWORD_LENGTH) {
        setFormError(`รหัสผ่านต้องมีอย่างน้อย ${MIN_PASSWORD_LENGTH} ตัวอักษร`)
        return
      }
      if (formPassword !== formConfirm) {
        setFormError("รหัสผ่านและการยืนยันรหัสผ่านไม่ตรงกัน")
        return
      }
    }

    setIsSaving(true)
    try {
      if (editingUser) {
        await userService.update(editingUser.id, {
          role: formRole,
          ...(wantsPassword ? { password: formPassword } : {}),
        })
      } else {
        await userService.create({
          username: formUsername,
          password: formPassword,
          role: formRole,
        })
      }
      await loadUsers(false)
      setIsModalOpen(false)
    } catch (err) {
      setFormError(getErrorMessage(err) || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
    } finally {
      setIsSaving(false)
    }
  }

  const handleToggle = async (u: UserAccount) => {
    if (isSelf(u)) return
    setBusyId(u.id)
    try {
      await userService.toggle(u.id)
      await loadUsers(false)
    } catch (err) {
      await alert("ไม่สามารถเปลี่ยนสถานะได้", getErrorMessage(err))
    } finally {
      setBusyId(null)
    }
  }

  const handleDelete = async (u: UserAccount) => {
    if (isSelf(u)) return
    const ok = await confirm(
      "ยืนยันการลบผู้ใช้",
      `คุณต้องการลบบัญชีผู้ใช้ "${u.username}" ใช่หรือไม่? การกระทำนี้ไม่สามารถย้อนกลับได้`
    )
    if (!ok) return
    setBusyId(u.id)
    try {
      await userService.remove(u.id)
      await loadUsers(false)
    } catch (err) {
      await alert("ไม่สามารถลบผู้ใช้ได้", getErrorMessage(err))
    } finally {
      setBusyId(null)
    }
  }

  const formatDate = (iso: string) => {
    const d = new Date(iso)
    if (isNaN(d.getTime())) return "-"
    return d.toLocaleDateString("th-TH", { year: "numeric", month: "short", day: "numeric" })
  }

  return (
    <div className="space-y-4">
      {/* 1. Stats overview */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <StatCard icon={UsersIcon} title="Total Users" value={stats.total} />
        <StatCard icon={ShieldCheck} title="Super Admin" value={stats.superAdmins} />
        <StatCard icon={Eye} title="Read-only" value={stats.readonly} />
        <StatCard icon={Ban} title="Disabled" value={stats.disabled} />
      </div>

      {/* 2. Users table */}
      <Card>
        <CardHeader className="flex flex-col gap-4 space-y-0 sm:flex-row sm:items-center sm:justify-between">
          <div className="space-y-1">
            <CardTitle className="flex items-center gap-2 text-base font-semibold">
              <UsersIcon className="h-4 w-4 text-muted-foreground" />
              User Accounts
              <Badge variant="secondary" className="rounded-full px-2 py-0 text-xs font-semibold">
                {stats.total}
              </Badge>
            </CardTitle>
            <CardDescription className="text-xs">
              สร้าง แก้ไข ปิด/เปิดใช้งาน และกำหนดบทบาทของผู้ดูแลระบบ (เฉพาะ Super Admin)
            </CardDescription>
          </div>

          <Button size="sm" onClick={openCreateModal} className="cursor-pointer gap-1.5 font-semibold">
            <Plus className="h-4 w-4" />
            Add User
          </Button>
        </CardHeader>

        <CardContent>
          <Table>
            <TableHeader>
              <TableRow className="hover:bg-transparent">
                <TableHead className="text-xs font-medium text-muted-foreground">Username</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Role</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Status</TableHead>
                <TableHead className="text-xs font-medium text-muted-foreground">Created</TableHead>
                <TableHead className="text-right text-xs font-medium text-muted-foreground">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {isLoading ? (
                <TableRow>
                  <TableCell colSpan={5} className="py-12 text-center text-xs text-muted-foreground">
                    <div className="flex flex-col items-center justify-center gap-2 py-4">
                      <Loader2 className="h-6 w-6 animate-spin text-primary" />
                      <span>กำลังโหลดข้อมูล...</span>
                    </div>
                  </TableCell>
                </TableRow>
              ) : users.length === 0 ? (
                <TableRow>
                  <TableCell colSpan={5} className="py-8 text-center text-xs text-muted-foreground">
                    ไม่พบข้อมูลผู้ใช้
                  </TableCell>
                </TableRow>
              ) : (
                users.map((u) => (
                  <TableRow key={u.id}>
                    <TableCell className="py-3 font-medium text-foreground">
                      <div className="flex items-center gap-2">
                        <UserIcon className="h-4 w-4 text-muted-foreground" />
                        <span>{u.username}</span>
                        {isSelf(u) && (
                          <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-1.5 py-0 text-[10px] text-primary">
                            You
                          </Badge>
                        )}
                        {u.isInitial && (
                          <Badge variant="outline" className="rounded border-amber-500/20 bg-amber-500/10 px-1.5 py-0 text-[10px] text-amber-500">
                            ต้องตั้งรหัสผ่าน
                          </Badge>
                        )}
                      </div>
                    </TableCell>
                    <TableCell className="py-3">
                      {u.role === "super_admin" ? (
                        <Badge variant="outline" className="gap-1 rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[11px] font-medium text-primary">
                          <ShieldCheck className="h-3 w-3" />
                          Super Admin
                        </Badge>
                      ) : (
                        <Badge variant="secondary" className="gap-1 rounded px-2 py-0.5 text-[11px] font-medium">
                          <Eye className="h-3 w-3" />
                          Read-only
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="py-3">
                      {u.status === "active" ? (
                        <Badge variant="outline" className="rounded border-primary/20 bg-primary/10 px-2 py-0.5 text-[11px] font-medium text-primary">
                          Active
                        </Badge>
                      ) : (
                        <Badge variant="outline" className="rounded border-red-500/20 bg-red-500/10 px-2 py-0.5 text-[11px] font-medium text-red-500">
                          Disabled
                        </Badge>
                      )}
                    </TableCell>
                    <TableCell className="py-3 text-xs text-muted-foreground">{formatDate(u.createdAt)}</TableCell>
                    <TableCell className="py-3 text-right">
                      <div className="flex items-center justify-end gap-3">
                        {/* Enable/disable toggle — hidden for self (backend guard) */}
                        <div className="flex items-center gap-1.5" title={isSelf(u) ? "ไม่สามารถปิดใช้งานบัญชีของตัวเองได้" : "เปิด/ปิดใช้งาน"}>
                          <Switch
                            checked={u.status === "active"}
                            disabled={isSelf(u) || busyId === u.id}
                            onCheckedChange={() => handleToggle(u)}
                          />
                        </div>
                        <Button
                          variant="outline"
                          size="icon-sm"
                          onClick={() => openEditModal(u)}
                          className="cursor-pointer text-muted-foreground hover:text-foreground"
                          title="แก้ไขผู้ใช้"
                        >
                          <Edit className="h-4 w-4" />
                        </Button>
                        <Button
                          variant="ghost"
                          size="icon-sm"
                          disabled={isSelf(u) || busyId === u.id}
                          onClick={() => handleDelete(u)}
                          className="cursor-pointer text-muted-foreground hover:bg-red-500/10 hover:text-red-500 disabled:cursor-not-allowed disabled:opacity-40"
                          title={isSelf(u) ? "ไม่สามารถลบบัญชีของตัวเองได้" : "ลบผู้ใช้"}
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

      {/* Create / Edit Drawer — no modal={false} needed; the native role select
          works fine under the default modal behavior (see rules_of_work.md §1.3). */}
      <Drawer direction="right" open={isModalOpen} onOpenChange={setIsModalOpen}>
        <DrawerContent className="data-[vaul-drawer-direction=right]:sm:max-w-[480px]">
          <DrawerHeader className="border-b border-border/50">
            <DrawerTitle className="text-base font-semibold">
              {editingUser ? `แก้ไขผู้ใช้: ${editingUser.username}` : "สร้างผู้ใช้ใหม่"}
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

            {/* Username (immutable on edit) */}
            <div className="space-y-1.5">
              <Label htmlFor="form-username" className="block text-xs font-medium text-muted-foreground">
                ชื่อผู้ใช้ (Username) <span className="text-destructive">*</span>
              </Label>
              <Input
                id="form-username"
                type="text"
                required
                disabled={!!editingUser}
                value={formUsername}
                onChange={(e) => setFormUsername(e.target.value)}
                placeholder="เช่น operator_a"
                className="h-9 font-mono disabled:opacity-60"
              />
              {editingUser ? (
                <p className="mt-0.5 text-[10px] text-muted-foreground">ชื่อผู้ใช้ไม่สามารถแก้ไขได้หลังสร้าง</p>
              ) : (
                <p className="mt-0.5 text-[10px] text-muted-foreground">3-32 ตัวอักษร ใช้ได้เฉพาะ a-z A-Z 0-9 และ _</p>
              )}
            </div>

            {/* Role */}
            <div className="space-y-1.5">
              <Label htmlFor="form-role" className="block text-xs font-medium text-muted-foreground">
                บทบาท (Role)
              </Label>
              <select
                id="form-role"
                value={formRole}
                onChange={(e) => setFormRole(e.target.value as UserRole)}
                className="h-9 w-full cursor-pointer rounded-md border border-input bg-background px-2.5 text-sm text-foreground outline-none focus:border-primary focus:ring-1 focus:ring-primary"
              >
                <option value="admin_readonly">Read-only Admin (ดูได้อย่างเดียว)</option>
                <option value="super_admin">Super Admin (จัดการได้ทุกอย่าง)</option>
              </select>
              {editingUser && isSelf(editingUser) && (
                <p className="mt-0.5 text-[10px] text-muted-foreground">
                  หมายเหตุ: คุณไม่สามารถลดบทบาทของตัวเองได้ (ระบบจะปฏิเสธ)
                </p>
              )}
            </div>

            {/* Password */}
            <div className="space-y-1.5">
              <Label htmlFor="form-password" className="block text-xs font-medium text-muted-foreground">
                {editingUser ? "รีเซ็ตรหัสผ่าน (Reset Password)" : "รหัสผ่าน (Password)"}
                {!editingUser && <span className="text-destructive"> *</span>}
              </Label>
              <Input
                id="form-password"
                type="password"
                required={!editingUser}
                value={formPassword}
                onChange={(e) => setFormPassword(e.target.value)}
                placeholder={editingUser ? "เว้นว่างไว้ = ไม่เปลี่ยนรหัสผ่าน" : "อย่างน้อย 8 ตัวอักษร"}
                className="h-9"
                autoComplete="new-password"
              />
              {editingUser && (
                <p className="mt-0.5 text-[10px] text-muted-foreground">
                  ตั้งรหัสผ่านใหม่ให้ผู้ใช้โดยไม่ต้องรู้รหัสเดิม — ผู้ใช้จะถูกบังคับให้เปลี่ยนรหัสผ่านเมื่อล็อกอินครั้งถัดไป
                </p>
              )}
            </div>

            {/* Confirm password — shown when a password is being set */}
            {(!editingUser || formPassword.length > 0) && (
              <div className="space-y-1.5">
                <Label htmlFor="form-confirm" className="block text-xs font-medium text-muted-foreground">
                  ยืนยันรหัสผ่าน (Confirm Password) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="form-confirm"
                  type="password"
                  required
                  value={formConfirm}
                  onChange={(e) => setFormConfirm(e.target.value)}
                  placeholder="กรอกรหัสผ่านอีกครั้ง"
                  className="h-9"
                  autoComplete="new-password"
                />
              </div>
            )}

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
              <Button
                type="submit"
                disabled={isSaving}
                className="cursor-pointer gap-1.5 px-6 font-semibold"
              >
                {isSaving && <Loader2 className="h-4 w-4 animate-spin" />}
                {editingUser ? "Save Changes" : "Create User"}
              </Button>
            </div>
          </form>
          </div>
        </DrawerContent>
      </Drawer>
    </div>
  )
}
