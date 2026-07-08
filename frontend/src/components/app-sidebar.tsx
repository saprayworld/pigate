import * as React from "react"
import { NavLink, useLocation } from "react-router-dom"
import {
  LayoutDashboard,
  Network,
  Route as RouteIcon,
  Radio,
  Flame,
  BookOpen,
  Sliders,
  Settings,
  Globe,
  Activity,
  Server,
  Users,
} from "lucide-react"

import { NavUser } from "@/components/nav-user"
import { authService } from "@/services/authService"
import { useHostname } from "@/hooks/useHostname"
import {
  Sidebar,
  SidebarContent,
  SidebarFooter,
  SidebarGroup,
  SidebarGroupContent,
  SidebarGroupLabel,
  SidebarHeader,
  SidebarMenu,
  SidebarMenuButton,
  SidebarMenuItem,
} from "@/components/ui/sidebar"

type NavItem = { path: string; label: string; icon: React.ComponentType<{ className?: string }> }
type NavGroup = { title?: string; items: NavItem[] }

export function AppSidebar({ ...props }: React.ComponentProps<typeof Sidebar>) {
  const location = useLocation()
  const isSuperAdmin = authService.getRole() === "super_admin"
  const { hostname } = useHostname()

  const groups: NavGroup[] = [
    {
      items: [{ path: "/dashboard", label: "Dashboard", icon: LayoutDashboard }],
    },
    {
      title: "Network",
      items: [
        { path: "/network/interfaces", label: "Interfaces", icon: Network },
        { path: "/network/dns", label: "DNS Settings", icon: Globe },
        { path: "/network/dns-server", label: "DNS Server", icon: Server },
        { path: "/network/routes", label: "Static Routes", icon: RouteIcon },
        { path: "/network/dhcp", label: "DHCP Server", icon: Radio },
        { path: "/network/qos", label: "QoS Limiting", icon: Activity },
      ],
    },
    {
      title: "Policy & Objects",
      items: [
        { path: "/policy/firewall", label: "Firewall Policy", icon: Flame },
        { path: "/policy/addresses", label: "Addresses", icon: BookOpen },
        { path: "/policy/services", label: "Services", icon: Sliders },
      ],
    },
    {
      title: "System",
      items: [
        { path: "/system/settings", label: "Settings & Maintenance", icon: Settings },
        // User Management is super_admin only; the backend enforces access, this
        // just hides an unusable link from read-only admins.
        ...(isSuperAdmin
          ? [{ path: "/system/users", label: "User Management", icon: Users }]
          : []),
      ],
    },
  ]

  return (
    <Sidebar collapsible="icon" {...props}>
      <SidebarHeader>
        <SidebarMenu>
          <SidebarMenuItem>
            <SidebarMenuButton
              asChild
              className="h-8 data-[slot=sidebar-menu-button]:p-1.5! data-[slot=sidebar-menu-button]:pl-0!"
            >
              <NavLink to="/dashboard">
                <div className="flex aspect-square size-8 items-center justify-center rounded-lg bg-primary/10 text-primary">
                  <Flame className="size-5 fill-primary/20" />
                </div>
                <div className="grid flex-1 text-left text-xs leading-tight">
                  <span className="truncate text-sm font-bold tracking-wider">PiGate</span>
                  <span className="truncate text-xs text-muted-foreground font-mono">{hostname}</span>
                </div>
              </NavLink>
            </SidebarMenuButton>
          </SidebarMenuItem>
        </SidebarMenu>
      </SidebarHeader>

      <SidebarContent>
        {groups.map((group, i) => (
          <SidebarGroup key={group.title ?? `group-${i}`}>
            {group.title && <SidebarGroupLabel className="text-nowrap">{group.title}</SidebarGroupLabel>}
            <SidebarGroupContent>
              <SidebarMenu>
                {group.items.map((item) => {
                  const Icon = item.icon
                  const isActive = location.pathname === item.path
                  return (
                    <SidebarMenuItem key={item.path}>
                      <SidebarMenuButton asChild isActive={isActive} tooltip={item.label}>
                        <NavLink to={item.path}>
                          <Icon className="size-4" />
                          <span>{item.label}</span>
                        </NavLink>
                      </SidebarMenuButton>
                    </SidebarMenuItem>
                  )
                })}
              </SidebarMenu>
            </SidebarGroupContent>
          </SidebarGroup>
        ))}
      </SidebarContent>

      <SidebarFooter>
        <NavUser />
      </SidebarFooter>
    </Sidebar>
  )
}
