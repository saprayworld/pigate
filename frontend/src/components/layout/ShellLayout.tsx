import { useState, useEffect } from "react"
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom"
import { dashboardService } from "@/services/dashboardService"
import {
  LayoutDashboard,
  Network,
  Route,
  Radio,
  Flame,
  BookOpen,
  Sliders,
  Settings,
  LogOut,
  ChevronDown,
  Cpu,
  HardDrive,
  Thermometer,
  Zap,
  Menu,
  Moon,
  Sun,
  Globe,
  Activity
} from "lucide-react"
import { Badge } from "@/components/ui/badge"
import { Avatar, AvatarFallback } from "@/components/ui/avatar"
import { Switch } from "@/components/ui/switch"
import {
  DropdownMenu,
  DropdownMenuTrigger,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuSeparator,
  DropdownMenuLabel,
  DropdownMenuGroup
} from "@/components/ui/dropdown-menu"
import { useTheme } from "@/components/ThemeProvider"

export default function ShellLayout() {
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()
  const { theme, setTheme } = useTheme()

  // Live performance metrics for Topbar badges
  const [topbarCpu, setTopbarCpu] = useState<number | null>(null)
  const [topbarMem, setTopbarMem] = useState<number | null>(null)
  const [topbarTemp, setTopbarTemp] = useState<number | null>(null)

  useEffect(() => {
    const fetchPerf = async () => {
      try {
        const perf = await dashboardService.getPerformanceMetrics()
        setTopbarCpu(perf.cpu)
        setTopbarMem(perf.memory)
        setTopbarTemp(perf.temp)
      } catch (err) { /* silently ignore */ }
    }
    fetchPerf()
    const interval = setInterval(fetchPerf, 5000)
    return () => clearInterval(interval)
  }, [])

  // Map path to display title
  const getPageTitle = (pathname: string) => {
    switch (pathname) {
      case "/":
      case "/dashboard":
        return "Dashboard"
      case "/network/interfaces":
        return "Network Interfaces"
      case "/network/dns":
        return "DNS Settings"
      case "/network/routes":
        return "Static Routes"
      case "/network/dhcp":
        return "DHCP Server"
      case "/network/qos":
        return "QoS Bandwidth Limiting"
      case "/policy/firewall":
        return "Firewall Policy"
      case "/policy/addresses":
        return "Addresses (Objects)"
      case "/policy/services":
        return "Services (Objects)"
      case "/system/settings":
        return "Settings & Maintenance"
      default:
        return "PiGate Controller"
    }
  }

  const handleLogout = () => {
    localStorage.removeItem("pigate_session")
    navigate("/login")
  }

  const menuGroups = [
    {
      items: [
        { path: "/dashboard", label: "Dashboard", icon: LayoutDashboard }
      ]
    },
    {
      title: "Network",
      items: [
        { path: "/network/interfaces", label: "Interfaces", icon: Network },
        { path: "/network/dns", label: "DNS Settings", icon: Globe },
        { path: "/network/routes", label: "Static Routes", icon: Route },
        { path: "/network/dhcp", label: "DHCP Server", icon: Radio },
        { path: "/network/qos", label: "QoS Limiting", icon: Activity }
      ]
    },
    {
      title: "Policy & Objects",
      items: [
        { path: "/policy/firewall", label: "Firewall Policy", icon: Flame },
        { path: "/policy/addresses", label: "Addresses", icon: BookOpen },
        { path: "/policy/services", label: "Services", icon: Sliders }
      ]
    },
    {
      title: "System",
      items: [
        { path: "/system/settings", label: "Settings & Maintenance", icon: Settings }
      ]
    }
  ]

  const SidebarContent = () => (
    <div className="flex h-full flex-col bg-sidebar border-r border-sidebar-border text-sidebar-foreground">
      {/* Brand Header */}
      <div className="flex h-16 items-center gap-2 px-6 border-b border-sidebar-border bg-sidebar">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-primary/10 border border-primary/20 text-primary">
          <Flame className="h-5 w-5 fill-primary/20" />
        </div>
        <span className="text-xl font-bold tracking-wider text-sidebar-foreground">PiGate</span>
        <Badge variant="outline" className="bg-primary/10 text-primary border border-primary/20 hover:bg-primary/20 h-4.5 rounded-full px-1.5 text-[10px]">v1.0</Badge>
      </div>

      {/* Navigation Links */}
      <div className="flex-1 overflow-y-auto px-4 py-6 space-y-6">
        {menuGroups.map((group, groupIdx) => (
          <div key={groupIdx} className="space-y-1">
            {group.title && (
              <span className="px-3 text-xs font-semibold text-sidebar-foreground/50 uppercase tracking-wider block">
                {group.title}
              </span>
            )}
            <div className="space-y-1 pt-1">
              {group.items.map((item, itemIdx) => (
                <NavLink
                  key={itemIdx}
                  to={item.path}
                  onClick={() => setIsMobileMenuOpen(false)}
                  className={({ isActive }) =>
                    `flex items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium transition duration-200 ${isActive
                      ? "bg-sidebar-accent text-primary font-semibold"
                      : "hover:bg-sidebar-accent/50 hover:text-sidebar-foreground text-sidebar-foreground/75"
                    }`
                  }
                >
                  <item.icon className="h-4.5 w-4.5" />
                  <span>{item.label}</span>
                </NavLink>
              ))}
            </div>
          </div>
        ))}
      </div>

      {/* User footer */}
      <div className="p-4 border-t border-sidebar-border bg-sidebar/50">
        <button
          onClick={handleLogout}
          className="flex w-full items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium text-sidebar-foreground/75 hover:bg-destructive/10 hover:text-destructive border border-transparent hover:border-destructive/20 transition duration-200 cursor-pointer"
        >
          <LogOut className="h-4.5 w-4.5" />
          <span>Sign Out</span>
        </button>
      </div>
    </div>
  )

  return (
    <div className="flex min-h-svh bg-background text-foreground font-sans antialiased">
      {/* Desktop Sidebar (hidden on mobile) */}
      <aside className="hidden md:flex md:w-64 md:flex-col md:fixed md:inset-y-0 z-20">
        <SidebarContent />
      </aside>

      {/* Mobile Sidebar Backing */}
      {isMobileMenuOpen && (
        <div
          className="fixed inset-0 bg-black/60 z-30 md:hidden"
          onClick={() => setIsMobileMenuOpen(false)}
        />
      )}

      {/* Mobile Sidebar Panel */}
      <div
        className={`fixed top-0 bottom-0 left-0 w-64 bg-sidebar z-40 transform transition-transform duration-300 md:hidden ${isMobileMenuOpen ? "translate-x-0" : "-translate-x-full"
          }`}
      >
        <SidebarContent />
      </div>

      {/* Main Content Area */}
      <div className="flex-1 md:pl-64 flex flex-col min-w-0">
        {/* Topbar */}
        <header className="sticky top-0 z-10 flex h-16 w-full items-center justify-between border-b border-border bg-background px-6">
          {/* Topbar Left */}
          <div className="flex items-center gap-4">
            <button
              onClick={() => setIsMobileMenuOpen(true)}
              className="md:hidden p-1.5 rounded-lg text-muted-foreground hover:bg-accent hover:text-accent-foreground"
            >
              <Menu className="h-6 w-6" />
            </button>
            <h2 className="text-lg font-semibold tracking-tight text-foreground hidden sm:block">
              {getPageTitle(location.pathname)}
            </h2>
          </div>

          {/* Topbar Right (Stats & Profiles) */}
          <div className="flex items-center gap-4">
            {/* Pi Resources Stats */}
            <div className="flex items-center gap-2 sm:gap-3">
              {/* CPU */}
              <Badge variant="outline" className="flex items-center gap-1.5 rounded-full border-border bg-card/60 px-3 py-1 text-foreground hover:bg-card/60 h-7 text-xs font-normal">
                <Cpu className="h-3.5 w-3.5 text-primary" />
                <span className="hidden lg:inline text-muted-foreground">CPU</span>
                <span className={`font-semibold ${topbarCpu !== null && topbarCpu >= 85 ? 'text-red-500' : topbarCpu !== null && topbarCpu >= 50 ? 'text-amber-500' : 'text-primary'}`}>
                  {topbarCpu !== null ? `${topbarCpu}%` : '—'}
                </span>
              </Badge>

              {/* RAM */}
              <Badge variant="outline" className="flex items-center gap-1.5 rounded-full border-border bg-card/60 px-3 py-1 text-foreground hover:bg-card/60 h-7 text-xs font-normal">
                <HardDrive className="h-3.5 w-3.5 text-cyan-500 dark:text-cyan-400" />
                <span className="hidden lg:inline text-muted-foreground">RAM</span>
                <span className="font-semibold text-cyan-500 dark:text-cyan-400">
                  {topbarMem !== null ? `${topbarMem}%` : '—'}
                </span>
              </Badge>

              {/* Temp */}
              <Badge variant="outline" className="flex items-center gap-1.5 rounded-full border-border bg-card/60 px-3 py-1 text-foreground hover:bg-card/60 h-7 text-xs font-normal">
                <Thermometer className="h-3.5 w-3.5 text-amber-500 dark:text-amber-400" />
                <span className="hidden lg:inline text-muted-foreground">Temp</span>
                <span className={`font-semibold ${topbarTemp !== null && topbarTemp >= 70 ? 'text-red-500' : topbarTemp !== null && topbarTemp >= 50 ? 'text-amber-500 dark:text-amber-400' : 'text-amber-500 dark:text-amber-400'}`}>
                  {topbarTemp !== null ? `${topbarTemp}°C` : '—'}
                </span>
              </Badge>

              {/* Power status */}
              <Badge variant="outline" className="hidden sm:flex items-center gap-1.5 rounded-full border-border bg-card/60 px-3 py-1 text-foreground hover:bg-card/60 h-7 text-xs font-normal">
                <Zap className="h-3.5 w-3.5 text-primary" />
                <span className="hidden lg:inline text-muted-foreground">Power</span>
                <span className="font-semibold text-primary">OK</span>
              </Badge>
            </div>

            {/* Profile Dropdown with shadcn components */}
            <DropdownMenu>
              <DropdownMenuTrigger asChild>
                <button className="flex items-center gap-1.5 rounded-lg border border-border bg-card px-3 py-1.5 text-sm font-medium hover:bg-accent hover:text-accent-foreground transition outline-none cursor-pointer">
                  <Avatar size="sm">
                    <AvatarFallback className="bg-primary/10 text-primary">
                      AD
                    </AvatarFallback>
                  </Avatar>
                  <span className="text-foreground hidden sm:inline">admin</span>
                  <ChevronDown className="h-4 w-4 text-muted-foreground" />
                </button>
              </DropdownMenuTrigger>
              <DropdownMenuContent align="end" className="w-56 p-1 rounded-xl bg-popover text-popover-foreground border border-border">
                <DropdownMenuLabel>
                  <div className="flex flex-col space-y-1">
                    <p className="text-sm font-medium leading-none text-foreground">admin</p>
                    <p className="text-xs leading-none text-muted-foreground">admin@pigate.local</p>
                  </div>
                </DropdownMenuLabel>
                <DropdownMenuSeparator />

                <DropdownMenuGroup>
                  <DropdownMenuItem
                    onClick={() => navigate("/system/settings")}
                    className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm cursor-pointer transition"
                  >
                    <Settings className="h-4 w-4" />
                    <span>Settings</span>
                  </DropdownMenuItem>
                </DropdownMenuGroup>

                {/* Appearance Section */}
                <DropdownMenuSeparator />
                <DropdownMenuLabel className="text-xs font-normal text-muted-foreground">
                  Appearance
                </DropdownMenuLabel>
                <DropdownMenuItem
                  onSelect={(e) => {
                    e.preventDefault();
                    setTheme(theme === "dark" ? "light" : "dark");
                  }}
                  className="cursor-pointer flex items-center justify-between"
                >
                  <div className="flex items-center">
                    {theme === "dark" ? (
                      <Moon className="w-4 h-4 mr-2 text-indigo-400" />
                    ) : (
                      <Sun className="w-4 h-4 mr-2 text-amber-500" />
                    )}
                    <span>Dark Mode</span>
                  </div>
                  <Switch
                    checked={theme === "dark"}
                    onCheckedChange={(checked) => setTheme(checked ? "dark" : "light")}
                  />
                </DropdownMenuItem>

                <DropdownMenuSeparator />
                <DropdownMenuItem
                  onClick={handleLogout}
                  className="flex items-center gap-2 rounded-lg px-3 py-2 text-sm text-red-500 dark:text-red-400 focus:bg-destructive/10 focus:text-destructive cursor-pointer transition"
                >
                  <LogOut className="h-4 w-4" />
                  <span>Sign Out</span>
                </DropdownMenuItem>
              </DropdownMenuContent>
            </DropdownMenu>
          </div>
        </header>

        {/* Workspace Panel */}
        <main className="flex-1 p-6 overflow-y-auto">
          <Outlet />
        </main>
      </div>
    </div>
  )
}
