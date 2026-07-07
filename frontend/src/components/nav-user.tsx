import { useNavigate } from "react-router-dom"
import { EllipsisVerticalIcon, Settings, LogOut, Moon, Sun, Power, RefreshCw } from "lucide-react"

import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Switch } from "@/components/ui/switch"
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuGroup,
  DropdownMenuItem,
  DropdownMenuLabel,
  DropdownMenuSeparator,
  DropdownMenuSub,
  DropdownMenuSubContent,
  DropdownMenuSubTrigger,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu"
import {
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
  useSidebar,
} from "@/components/ui/sidebar"
import { usePowerControl } from "@/hooks/usePowerControl"
import { authService } from "@/services/authService"
import { useTheme } from "@/hooks/useTheme"
import { useAlert } from "@/hooks/useAlert"

export function NavUser() {
  const { isMobile } = useSidebar()
  const navigate = useNavigate()
  const { theme, setTheme } = useTheme()
  const { confirm } = useAlert()
  const power = usePowerControl()

  const role = authService.getRole()
  const username = authService.getUsername() || "admin"
  const roleLabel =
    role === "super_admin" ? "Super Admin" : role === "admin_readonly" ? "Read-only Admin" : "User"
  const avatarInitials = username.slice(0, 2).toUpperCase()

  const handleLogout = () => {
    // Clears session token, role, username and the must-change flag.
    void authService.logout()
    navigate("/login")
  }

  const handleReboot = async () => {
    const ok = await confirm(
      "ยืนยันการรีบูตระบบ",
      "คุณต้องการสั่งรีบูตเครื่อง (Restart) บอร์ด PiGate ใช่หรือไม่? การเชื่อมต่อเครือข่ายทั้งหมดผ่านพอร์ต WAN/LAN จะสิ้นสุดชั่วคราวจนกว่าระบบจะกลับมาทำงานอีกครั้ง"
    )
    if (ok) await power.reboot()
  }

  const handleShutdown = async () => {
    const ok = await confirm(
      "ยืนยันการปิดเครื่อง",
      "คุณต้องการสั่งปิดระบบ (Shutdown) ใช่หรือไม่? ตัวเครื่องจะหยุดทำงานและระบบจะตัดการจ่ายกำลังไฟเลี้ยงบอร์ด"
    )
    if (ok) await power.shutdown()
  }

  return (
    <SidebarMenu>
      <SidebarMenuItem>
        <DropdownMenu>
          <DropdownMenuTrigger asChild>
            <SidebarMenuButton
              size="lg"
              className="data-[state=open]:bg-sidebar-accent data-[state=open]:text-sidebar-accent-foreground"
            >
              <Avatar className="h-8 w-8 rounded-lg">
                <AvatarFallback className="rounded-lg bg-primary/10 text-primary">
                  {avatarInitials}
                </AvatarFallback>
              </Avatar>
              <div className="grid flex-1 text-left text-sm leading-tight">
                <span className="truncate font-medium">{username}</span>
                <span className="truncate text-xs text-muted-foreground">{roleLabel}</span>
              </div>
              <EllipsisVerticalIcon className="ml-auto size-4" />
            </SidebarMenuButton>
          </DropdownMenuTrigger>
          <DropdownMenuContent
            className="w-(--radix-dropdown-menu-trigger-width) min-w-56 rounded-lg"
            side={isMobile ? "bottom" : "right"}
            align="end"
            sideOffset={4}
          >
            <DropdownMenuLabel className="p-0 font-normal">
              <div className="flex items-center gap-2 px-1 py-1.5 text-left text-sm">
                <Avatar className="h-8 w-8 rounded-lg">
                  <AvatarFallback className="rounded-lg bg-primary/10 text-primary">
                    {avatarInitials}
                  </AvatarFallback>
                </Avatar>
                <div className="grid flex-1 text-left text-sm leading-tight">
                  <span className="truncate font-medium">{username}</span>
                  <span className="truncate text-xs text-muted-foreground">{roleLabel}</span>
                </div>
              </div>
            </DropdownMenuLabel>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuItem onClick={() => navigate("/system/settings")}>
                <Settings className="size-4" />
                Settings
              </DropdownMenuItem>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
              Appearance
            </DropdownMenuLabel>
            <DropdownMenuItem
              onSelect={(e) => {
                e.preventDefault()
                setTheme(theme === "dark" ? "light" : "dark")
              }}
              className="flex items-center justify-between"
            >
              <div className="flex items-center gap-2">
                {theme === "dark" ? (
                  <Moon className="size-4 text-indigo-400" />
                ) : (
                  <Sun className="size-4 text-amber-500" />
                )}
                <span>Dark Mode</span>
              </div>
              <Switch
                checked={theme === "dark"}
                onCheckedChange={(checked) => setTheme(checked ? "dark" : "light")}
              />
            </DropdownMenuItem>
            <DropdownMenuSeparator />
            <DropdownMenuGroup>
              <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
                System
              </DropdownMenuLabel>
              <DropdownMenuSub>
                <DropdownMenuSubTrigger>
                  <Power className="size-4" />
                  <span>Power</span>
                </DropdownMenuSubTrigger>
                <DropdownMenuSubContent className="min-w-40">
                  <DropdownMenuItem
                    onSelect={() => void handleReboot()}
                    className="text-red-500 focus:bg-destructive/10 focus:text-destructive dark:text-red-400"
                  >
                    <RefreshCw className="size-4" />
                    Restart
                  </DropdownMenuItem>
                  <DropdownMenuItem
                    onSelect={() => void handleShutdown()}
                    className="text-red-500 focus:bg-destructive/10 focus:text-destructive dark:text-red-400"
                  >
                    <Power className="size-4" />
                    Shutdown
                  </DropdownMenuItem>
                </DropdownMenuSubContent>
              </DropdownMenuSub>
            </DropdownMenuGroup>
            <DropdownMenuSeparator />
            <DropdownMenuItem
              onClick={handleLogout}
              className="text-red-500 focus:bg-destructive/10 focus:text-destructive dark:text-red-400"
            >
              <LogOut className="size-4" />
              Sign Out
            </DropdownMenuItem>
          </DropdownMenuContent>
        </DropdownMenu>
      </SidebarMenuItem>
      {power.overlay}
    </SidebarMenu>
  )
}
