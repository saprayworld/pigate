import { useEffect, useState } from "react"
import { useLocation } from "react-router-dom"
import { Thermometer } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { dashboardService } from "@/services/dashboardService"
import { cn } from "@/lib/utils"

const TITLES: Record<string, string> = {
  "/": "Dashboard",
  "/dashboard": "Dashboard",
  "/network/interfaces": "Network Interfaces",
  "/network/dns": "DNS Settings",
  "/network/dns-server": "Local DNS Server",
  "/network/routes": "Static Routes",
  "/network/dhcp": "DHCP Server",
  "/network/qos": "QoS Bandwidth Limiting",
  "/policy/firewall": "Firewall Policy",
  "/policy/addresses": "Addresses (Objects)",
  "/policy/services": "Services (Objects)",
  "/system/settings": "Settings & Maintenance",
  "/system/users": "User Management",
}

export function SiteHeader() {
  const location = useLocation()
  const title = TITLES[location.pathname] ?? "PiGate Controller"

  // Live SoC temperature badge (unavailable on hosts without a thermal zone).
  const [temp, setTemp] = useState<number | null>(null)
  const [tempAvailable, setTempAvailable] = useState(true)

  useEffect(() => {
    let active = true
    const fetchPerf = async () => {
      try {
        const perf = await dashboardService.getPerformanceMetrics()
        if (!active) return
        setTempAvailable(perf.tempDetail.available)
        setTemp(perf.tempDetail.available ? perf.tempDetail.celsius : null)
      } catch {
        /* silently ignore */
      }
    }
    fetchPerf()
    const id = setInterval(fetchPerf, 5000)
    return () => {
      active = false
      clearInterval(id)
    }
  }, [])

  const tempColor =
    temp !== null && temp >= 70
      ? "text-red-500"
      : "text-amber-500 dark:text-amber-400"

  return (
    <header className="flex h-14 shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear">
      <div className="flex w-full items-center gap-1 px-4 lg:gap-2 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator orientation="vertical" className="mx-2 data-[orientation=vertical]:h-4" />
        <h1 className="text-base font-medium">{title}</h1>

        <div className="ml-auto flex items-center gap-2">
          {tempAvailable && (
            <Badge
              variant="outline"
              className="flex h-7 items-center gap-1.5 rounded-full border-border bg-card/60 px-3 text-xs font-normal hover:bg-card/60"
            >
              <Thermometer className="h-3.5 w-3.5 text-amber-500 dark:text-amber-400" />
              <span className="hidden text-muted-foreground lg:inline">Temp</span>
              <span className={cn("font-semibold", tempColor)}>
                {temp !== null ? `${temp}°C` : "—"}
              </span>
            </Badge>
          )}
        </div>
      </div>
    </header>
  )
}
