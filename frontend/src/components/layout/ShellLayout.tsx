import { useState } from "react"
import { NavLink, Outlet, useLocation, useNavigate } from "react-router-dom"
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
  User,
  Menu
} from "lucide-react"

export default function ShellLayout() {
  const [isMobileMenuOpen, setIsMobileMenuOpen] = useState(false)
  const [isUserDropdownOpen, setIsUserDropdownOpen] = useState(false)
  const location = useLocation()
  const navigate = useNavigate()

  // Map path to display title
  const getPageTitle = (pathname: string) => {
    switch (pathname) {
      case "/":
      case "/dashboard":
        return "Dashboard"
      case "/network/interfaces":
        return "Network Interfaces"
      case "/network/routes":
        return "Static Routes"
      case "/network/dhcp":
        return "DHCP Server"
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
        { path: "/network/routes", label: "Static Routes", icon: Route },
        { path: "/network/dhcp", label: "DHCP Server", icon: Radio }
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
    <div className="flex h-full flex-col bg-neutral-950 border-r border-neutral-900 text-neutral-300">
      {/* Brand Header */}
      <div className="flex h-16 items-center gap-2 px-6 border-b border-neutral-900 bg-neutral-950">
        <div className="flex h-9 w-9 items-center justify-center rounded-lg bg-emerald-500/10 border border-emerald-500/20 text-emerald-400">
          <Flame className="h-5 w-5 fill-emerald-500/20" />
        </div>
        <span className="text-xl font-bold tracking-wider text-white">PiGate</span>
        <span className="rounded-full bg-emerald-950 px-2 py-0.5 text-[10px] font-medium text-emerald-400 border border-emerald-900/50">v1.0</span>
      </div>

      {/* Navigation Links */}
      <div className="flex-1 overflow-y-auto px-4 py-6 space-y-6">
        {menuGroups.map((group, groupIdx) => (
          <div key={groupIdx} className="space-y-1">
            {group.title && (
              <span className="px-3 text-xs font-semibold text-neutral-500 uppercase tracking-wider block">
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
                      ? "bg-neutral-900 text-emerald-400"
                      : "hover:bg-neutral-900/50 hover:text-white text-neutral-400"
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
      <div className="p-4 border-t border-neutral-900 bg-neutral-950/50">
        <button
          onClick={handleLogout}
          className="flex w-full items-center gap-3 px-3 py-2.5 rounded-lg text-sm font-medium text-neutral-400 hover:bg-red-950/20 hover:text-red-400 border border-transparent hover:border-red-900/50 transition duration-200"
        >
          <LogOut className="h-4.5 w-4.5" />
          <span>Sign Out</span>
        </button>
      </div>
    </div>
  )

  return (
    <div className="flex min-h-svh bg-neutral-950 text-neutral-100 font-sans antialiased">
      {/* Desktop Sidebar (hidden on mobile) */}
      <aside className="hidden md:flex md:w-64 md:flex-col md:fixed md:inset-y-0 z-20">
        <SidebarContent />
      </aside>

      {/* Mobile Sidebar Backing */}
      {isMobileMenuOpen && (
        <div
          className="fixed inset-0 bg-black/60 backdrop-blur-sm z-30 md:hidden"
          onClick={() => setIsMobileMenuOpen(false)}
        />
      )}

      {/* Mobile Sidebar Panel */}
      <div
        className={`fixed top-0 bottom-0 left-0 w-64 bg-neutral-950 z-40 transform transition-transform duration-300 md:hidden ${isMobileMenuOpen ? "translate-x-0" : "-translate-x-full"
          }`}
      >
        <SidebarContent />
      </div>

      {/* Main Content Area */}
      <div className="flex-1 md:pl-64 flex flex-col min-w-0">
        {/* Topbar */}
        <header className="sticky top-0 z-10 flex h-16 w-full items-center justify-between border-b border-neutral-900 bg-neutral-950/80 backdrop-blur-md px-6 shadow-sm">
          {/* Topbar Left */}
          <div className="flex items-center gap-4">
            <button
              onClick={() => setIsMobileMenuOpen(true)}
              className="md:hidden p-1.5 rounded-lg text-neutral-400 hover:bg-neutral-900 hover:text-white"
            >
              <Menu className="h-6 w-6" />
            </button>
            <h2 className="text-lg font-semibold tracking-tight text-white hidden sm:block">
              {getPageTitle(location.pathname)}
            </h2>
          </div>

          {/* Topbar Right (Stats & Profiles) */}
          <div className="flex items-center gap-4">
            {/* Pi Resources Stats */}
            <div className="flex items-center gap-2 sm:gap-3 text-xs">
              {/* CPU */}
              <div className="flex items-center gap-1.5 rounded-full border border-neutral-800 bg-neutral-900/60 px-3 py-1 text-neutral-300">
                <Cpu className="h-3.5 w-3.5 text-emerald-400" />
                <span className="hidden lg:inline text-neutral-500">CPU</span>
                <span className="font-semibold text-emerald-400">15%</span>
              </div>

              {/* RAM */}
              <div className="flex items-center gap-1.5 rounded-full border border-neutral-800 bg-neutral-900/60 px-3 py-1 text-neutral-300">
                <HardDrive className="h-3.5 w-3.5 text-cyan-400" />
                <span className="hidden lg:inline text-neutral-500">RAM</span>
                <span className="font-semibold text-cyan-400">42%</span>
              </div>

              {/* Temp */}
              <div className="flex items-center gap-1.5 rounded-full border border-neutral-800 bg-neutral-900/60 px-3 py-1 text-neutral-300">
                <Thermometer className="h-3.5 w-3.5 text-amber-400" />
                <span className="hidden lg:inline text-neutral-500">Temp</span>
                <span className="font-semibold text-amber-400">48°C</span>
              </div>

              {/* Power status */}
              <div className="hidden sm:flex items-center gap-1.5 rounded-full border border-neutral-800 bg-neutral-900/60 px-3 py-1 text-neutral-300">
                <Zap className="h-3.5 w-3.5 text-emerald-500" />
                <span className="hidden lg:inline text-neutral-500">Power</span>
                <span className="font-semibold text-emerald-500">OK</span>
              </div>
            </div>

            {/* Profile Dropdown */}
            <div className="relative">
              <button
                onClick={() => setIsUserDropdownOpen(!isUserDropdownOpen)}
                className="flex items-center gap-1.5 rounded-lg border border-neutral-800 bg-neutral-900 px-3 py-1.5 text-sm font-medium hover:bg-neutral-800 transition"
              >
                <div className="flex h-5 w-5 items-center justify-center rounded-full bg-emerald-500/10 text-emerald-400">
                  <User className="h-3 w-3" />
                </div>
                <span className="text-white hidden sm:inline">admin</span>
                <ChevronDown className="h-4 w-4 text-neutral-400" />
              </button>

              {isUserDropdownOpen && (
                <>
                  <div
                    className="fixed inset-0 z-10"
                    onClick={() => setIsUserDropdownOpen(false)}
                  />
                  <div className="absolute right-0 mt-2 w-48 origin-top-right rounded-xl border border-neutral-800 bg-neutral-900 p-2 shadow-2xl ring-1 ring-black/5 focus:outline-none z-20">
                    <button
                      onClick={() => {
                        setIsUserDropdownOpen(false)
                        navigate("/system/settings")
                      }}
                      className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-neutral-300 hover:bg-neutral-800 transition"
                    >
                      <Settings className="h-4 w-4" />
                      <span>Settings</span>
                    </button>
                    <hr className="border-neutral-800 my-1" />
                    <button
                      onClick={() => {
                        setIsUserDropdownOpen(false)
                        handleLogout()
                      }}
                      className="flex w-full items-center gap-2 rounded-lg px-3 py-2 text-sm text-red-400 hover:bg-red-950/20 transition"
                    >
                      <LogOut className="h-4 w-4" />
                      <span>Sign Out</span>
                    </button>
                  </div>
                </>
              )}
            </div>
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
