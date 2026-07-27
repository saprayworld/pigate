import { AlertTriangle, RefreshCw } from "lucide-react"

import { Alert, AlertAction, AlertDescription, AlertTitle } from "@/components/ui/alert"
import { Button } from "@/components/ui/button"
import { useCapability, useCapabilities } from "@/hooks/useCapabilities"

/**
 * Warns when a kernel capability (e.g. "firewall") is unavailable or
 * degraded in this environment (issue #94 — real mode on WSL where nftables
 * doesn't actually work). Renders nothing when the capability is fully
 * healthy or its status hasn't loaded yet — a fetch failure must never be
 * mistaken for "unavailable" (docs/ref/todo/kernel-capability-detection-plan.md
 * §5 Caution 6).
 */
export function CapabilityBanner({ id }: { id: string }) {
  const capability = useCapability(id)
  const { refresh } = useCapabilities()

  if (!capability) return null
  if (capability.available && !capability.degraded) return null

  const variant = capability.available ? "default" : "destructive"

  return (
    <Alert variant={variant} className={!capability.available ? undefined : "border-warning/30 bg-warning/10"}>
      <AlertTriangle className={`h-4 w-4 ${capability.available ? "text-warning" : ""}`} />
      <AlertTitle className={capability.available ? "font-semibold text-warning" : "font-semibold"}>
        {capability.name} {capability.available ? "ทำงานได้ไม่สมบูรณ์" : "ใช้งานไม่ได้บนเครื่องนี้"}
      </AlertTitle>
      <AlertDescription>{capability.detail}</AlertDescription>
      <AlertAction>
        <Button variant="ghost" size="sm" onClick={refresh}>
          <RefreshCw className="h-3.5 w-3.5" />
          ตรวจสอบอีกครั้ง
        </Button>
      </AlertAction>
    </Alert>
  )
}
