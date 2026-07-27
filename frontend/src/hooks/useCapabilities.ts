import { useContext } from "react"
import { CapabilitiesProviderContext } from "@/hooks/capability-context"
import type { CapabilityStatus } from "@/services/capabilityService"

export const useCapabilities = () => {
  const context = useContext(CapabilitiesProviderContext)
  if (context === undefined)
    throw new Error("useCapabilities must be used within a CapabilitiesProvider")
  return context
}

// Convenience lookup for a single capability by id (e.g. "firewall"). Returns
// undefined while loading, on fetch failure, or if the id is unknown — callers
// (CapabilityBanner) must treat "undefined" as "don't render anything", never
// as "unavailable".
export const useCapability = (id: string): CapabilityStatus | undefined => {
  const { capabilities } = useCapabilities()
  return capabilities.find((c) => c.id === id)
}
