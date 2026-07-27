import { createContext } from "react"
import type { CapabilityStatus } from "@/services/capabilityService"

export type CapabilitiesProviderState = {
  // Capabilities fetched from the backend. Empty until the first successful
  // fetch resolves — a fetch failure leaves this empty rather than throwing
  // a false "unavailable" alarm (docs/ref/todo/kernel-capability-detection-plan.md
  // §5 Caution 6).
  capabilities: CapabilityStatus[]
  // True once at least one fetch has completed successfully.
  loaded: boolean
  // Force a fresh probe (bypasses the backend's ~30s cache), e.g. the
  // CapabilityBanner "ตรวจสอบอีกครั้ง" button.
  refresh: () => void
}

export const CapabilitiesProviderContext = createContext<CapabilitiesProviderState | undefined>(undefined)
