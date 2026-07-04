import { useState, useMemo, useEffect } from "react"
import {
  Users as UsersIcon,
  Plus,
  Edit,
  Trash2,
  AlertCircle,
  Loader2,
  ShieldCheck,
  Eye,
  User as UserIcon
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
import { Alert, AlertDescription } from "@/components/ui/alert"
import { useAlert } from "@/components/AlertDialogProvider"
import { authService } from "@/services/authService"
import {
  userService,
  type UserAccount,
  type UserRole,
} from "@/services/userService"

const USERNAME_REGEX = /^[a-zA-Z0-9_]{3,32}$/
const MIN_PASSWORD_LENGTH = 8

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
    } catch (err: any) {
      await alert("ข้อผิดพลาด", "ไม่สามารถโหลดรายชื่อผู้ใช้ได้: " + (err.message || err))
    } finally {
      if (showLoading) setIsLoading(false)
    }
  }

  useEffect(() => {
    loadUsers()
  }, [])

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
    } catch (err: any) {
      setFormError(err.message || "เกิดข้อผิดพลาดในการบันทึกข้อมูล")
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
    } catch (err: any) {
      await alert("ไม่สามารถเปลี่ยนสถานะได้", err.message || String(err))
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
    } catch (err: any) {
      await alert("ไม่สามารถลบผู้ใช้ได้", err.message || String(err))
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
    <div className="space-y-6">
      {/* Header */}
      <div className="flex flex-col gap-4 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h1 className="text-3xl font-bold tracking-tight text-foreground flex items-center gap-2">
            <UsersIcon className="h-7 w-7 text-primary fill-primary/10" />
            User Management (จัดการผู้ใช้)
          </h1>
          <p className="text-muted-foreground mt-1">
            สร้าง แก้ไข ปิด/เปิดใช้งาน และกำหนดบทบาทของผู้ดูแลระบบ (เฉพาะ Super Admin)
          </p>
        </div>
        <div>
          <Button
            onClick={openCreateModal}
            className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/90 font-bold gap-1.5"
          >
            <Plus className="h-4.5 w-4.5" />
            Add User
          </Button>
        </div>
      </div>

      {/* Stats */}
      <div className="grid gap-4 grid-cols-2 lg:grid-cols-4">
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">ผู้ใช้ทั้งหมด</div>
          <div className="mt-2 text-2xl font-bold text-foreground font-mono">{stats.total}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Super Admin</div>
          <div className="mt-2 text-2xl font-bold text-primary font-mono">{stats.superAdmins}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">Read-only</div>
          <div className="mt-2 text-2xl font-bold text-foreground font-mono">{stats.readonly}</div>
        </Card>
        <Card className="bg-card/20 border border-border/50 p-4">
          <div className="text-xs font-semibold uppercase tracking-wider text-muted-foreground">ปิดใช้งาน</div>
          <div className="mt-2 text-2xl font-bold text-muted-foreground font-mono">{stats.disabled}</div>
        </Card>
      </div>

      {/* Table */}
      <Card className="bg-card/25 border border-border/50 overflow-hidden h-fit py-0">
        <Table>
          <TableHeader>
            <TableRow className="border-b border-border/50 bg-muted/20 font-semibold text-muted-foreground hover:bg-muted/20">
              <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold pl-4">Username</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Role</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Status</th>
              <th className="p-3 text-left text-[11px] uppercase tracking-wider font-semibold">Created</th>
              <TableHead className="p-3 text-right pr-4">Actions</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {isLoading ? (
              <TableRow>
                <TableCell colSpan={5} className="p-12 text-center text-muted-foreground text-xs">
                  <div className="flex flex-col items-center justify-center gap-2 py-4">
                    <Loader2 className="h-6 w-6 animate-spin text-primary" />
                    <span>กำลังโหลดข้อมูล...</span>
                  </div>
                </TableCell>
              </TableRow>
            ) : users.length === 0 ? (
              <TableRow>
                <TableCell colSpan={5} className="p-8 text-center text-muted-foreground text-xs">
                  ไม่พบข้อมูลผู้ใช้
                </TableCell>
              </TableRow>
            ) : (
              users.map((u) => (
                <TableRow key={u.id} className="border-b border-border/40 hover:bg-muted/15 transition">
                  <TableCell className="p-3 font-semibold text-foreground pl-4">
                    <div className="flex items-center gap-2">
                      <UserIcon className="h-4 w-4 text-muted-foreground" />
                      <span>{u.username}</span>
                      {isSelf(u) && (
                        <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[10px] px-1.5 py-0 rounded">
                          You
                        </Badge>
                      )}
                      {u.isInitial && (
                        <Badge variant="outline" className="bg-muted/40 text-muted-foreground border-border text-[10px] px-1.5 py-0 rounded">
                          ต้องตั้งรหัสผ่าน
                        </Badge>
                      )}
                    </div>
                  </TableCell>
                  <TableCell className="p-3">
                    {u.role === "super_admin" ? (
                      <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[11px] px-2 py-0.5 rounded gap-1">
                        <ShieldCheck className="h-3 w-3" />
                        Super Admin
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="bg-muted/40 text-muted-foreground border-border text-[11px] px-2 py-0.5 rounded gap-1">
                        <Eye className="h-3 w-3" />
                        Read-only
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="p-3">
                    {u.status === "active" ? (
                      <Badge variant="outline" className="bg-primary/10 text-primary border-primary/20 text-[11px] px-2 py-0.5 rounded">
                        Active
                      </Badge>
                    ) : (
                      <Badge variant="outline" className="bg-destructive/10 text-destructive border-destructive/20 text-[11px] px-2 py-0.5 rounded">
                        Disabled
                      </Badge>
                    )}
                  </TableCell>
                  <TableCell className="p-3 text-xs text-muted-foreground">{formatDate(u.createdAt)}</TableCell>
                  <TableCell className="p-3 text-right pr-4">
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
                        variant="ghost"
                        size="icon-xs"
                        onClick={() => openEditModal(u)}
                        className="cursor-pointer text-muted-foreground hover:text-foreground hover:bg-muted/50"
                        title="แก้ไขผู้ใช้"
                      >
                        <Edit className="h-3.5 w-3.5" />
                      </Button>
                      <Button
                        variant="ghost"
                        size="icon-xs"
                        disabled={isSelf(u) || busyId === u.id}
                        onClick={() => handleDelete(u)}
                        className="cursor-pointer text-muted-foreground hover:text-destructive hover:bg-destructive/10 disabled:opacity-40 disabled:cursor-not-allowed"
                        title={isSelf(u) ? "ไม่สามารถลบบัญชีของตัวเองได้" : "ลบผู้ใช้"}
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

      {/* Create / Edit Dialog — modal={false} required for the native role select
          per rules_of_work.md (portal-in-dialog interaction). */}
      <Dialog open={isModalOpen} modal={false} onOpenChange={setIsModalOpen}>
        <DialogContent className="md:max-w-[480px] w-full rounded-xl border border-border bg-card p-6 gap-4 animate-scale-up">
          <DialogHeader className="pb-3 border-b border-border/40">
            <DialogTitle className="text-lg font-bold text-foreground">
              {editingUser ? `แก้ไขผู้ใช้: ${editingUser.username}` : "สร้างผู้ใช้ใหม่"}
            </DialogTitle>
          </DialogHeader>

          <form onSubmit={handleSave} className="space-y-4 text-sm">
            {formError && (
              <Alert variant="destructive" className="border-destructive/20 bg-destructive/5 py-2.5 px-3">
                <AlertCircle className="h-4 w-4 text-destructive" />
                <AlertDescription className="text-destructive text-xs">{formError}</AlertDescription>
              </Alert>
            )}

            {/* Username (immutable on edit) */}
            <div className="space-y-1.5">
              <Label htmlFor="form-username" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
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
                className="bg-background/50 placeholder:text-muted-foreground h-9 font-mono disabled:opacity-60"
              />
              {editingUser ? (
                <p className="text-[11px] text-muted-foreground italic">ชื่อผู้ใช้ไม่สามารถแก้ไขได้หลังสร้าง</p>
              ) : (
                <p className="text-[11px] text-muted-foreground italic">3-32 ตัวอักษร ใช้ได้เฉพาะ a-z A-Z 0-9 และ _</p>
              )}
            </div>

            {/* Role */}
            <div className="space-y-1.5">
              <Label htmlFor="form-role" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                บทบาท (Role)
              </Label>
              <select
                id="form-role"
                value={formRole}
                onChange={(e) => setFormRole(e.target.value as UserRole)}
                className="w-full bg-background border border-border rounded-lg h-9 px-2.5 text-xs text-foreground focus:ring-1 focus:ring-primary focus:border-primary outline-none cursor-pointer"
              >
                <option value="admin_readonly">Read-only Admin (ดูได้อย่างเดียว)</option>
                <option value="super_admin">Super Admin (จัดการได้ทุกอย่าง)</option>
              </select>
              {editingUser && isSelf(editingUser) && (
                <p className="text-[11px] text-muted-foreground italic">
                  หมายเหตุ: คุณไม่สามารถลดบทบาทของตัวเองได้ (ระบบจะปฏิเสธ)
                </p>
              )}
            </div>

            {/* Password */}
            <div className="space-y-1.5">
              <Label htmlFor="form-password" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
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
                className="bg-background/50 placeholder:text-muted-foreground h-9"
                autoComplete="new-password"
              />
              {editingUser && (
                <p className="text-[11px] text-muted-foreground italic">
                  ตั้งรหัสผ่านใหม่ให้ผู้ใช้โดยไม่ต้องรู้รหัสเดิม — ผู้ใช้จะถูกบังคับให้เปลี่ยนรหัสผ่านเมื่อล็อกอินครั้งถัดไป
                </p>
              )}
            </div>

            {/* Confirm password — shown when a password is being set */}
            {(!editingUser || formPassword.length > 0) && (
              <div className="space-y-1.5">
                <Label htmlFor="form-confirm" className="text-xs font-semibold text-muted-foreground uppercase tracking-wider block">
                  ยืนยันรหัสผ่าน (Confirm Password) <span className="text-destructive">*</span>
                </Label>
                <Input
                  id="form-confirm"
                  type="password"
                  required
                  value={formConfirm}
                  onChange={(e) => setFormConfirm(e.target.value)}
                  placeholder="กรอกรหัสผ่านอีกครั้ง"
                  className="bg-background/50 placeholder:text-muted-foreground h-9"
                  autoComplete="new-password"
                />
              </div>
            )}

            {/* Actions */}
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
                disabled={isSaving}
                className="cursor-pointer bg-primary text-primary-foreground hover:bg-primary/95 font-bold px-5 gap-1.5"
              >
                {isSaving && <Loader2 className="h-4 w-4 animate-spin" />}
                {editingUser ? "Save Changes" : "Create User"}
              </Button>
            </div>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  )
}
