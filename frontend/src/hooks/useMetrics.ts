import { useContext } from "react"

import { MetricsProviderContext } from "@/hooks/metrics-context"

export const useMetrics = () => {
  const context = useContext(MetricsProviderContext)
  if (context === undefined)
    throw new Error("useMetrics must be used within a MetricsProvider")
  return context
}
