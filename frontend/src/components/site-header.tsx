import { useLocation } from "react-router-dom"
import { Thermometer } from "lucide-react"

import { Badge } from "@/components/ui/badge"
import { Separator } from "@/components/ui/separator"
import { SidebarTrigger } from "@/components/ui/sidebar"
import { useMetrics } from "@/hooks/useMetrics"
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
  // Sourced from the shared SSE metrics stream (MetricsProvider) — no own poll.
  // Before the first snapshot arrives, keep the badge shown with a "—" placeholder
  // rather than hiding it, so it doesn't flash in on connect.
  const { metrics } = useMetrics()
  const tempAvailable = metrics ? metrics.tempDetail.available : true
  const temp =
    metrics && metrics.tempDetail.available ? metrics.tempDetail.celsius : null

  const tempColor =
    temp !== null && temp >= 70
      ? "text-destructive"
      : "text-warning"

  return (
    <header className="flex h-[48px] shrink-0 items-center gap-2 border-b transition-[width,height] ease-linear">
      <div className="flex w-full items-center justify-center gap-1 px-4 lg:gap-2 lg:px-6">
        <SidebarTrigger className="-ml-1" />
        <Separator orientation="vertical" className="mx-2 data-[orientation=vertical]:h-4 self-center!" />
        <h1 className="text-base font-medium">{title}</h1>

        <div className="ml-auto flex items-center gap-2">
          {tempAvailable && (
            <Badge
              variant="outline"
              className="flex h-7 items-center gap-1.5 rounded-full border-border bg-card/60 px-3 text-xs font-normal hover:bg-card/60"
            >
              <Thermometer className="h-3.5 w-3.5 text-warning" />
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
