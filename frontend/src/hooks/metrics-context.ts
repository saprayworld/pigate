import { createContext } from "react"

import type { PerformanceMetrics } from "@/services/dashboardService"

export type MetricsProviderState = {
  // Latest host telemetry pushed over SSE, or null until the first snapshot
  // arrives. Consumers (Dashboard StatGrid, site-header temp badge) read from
  // this single shared stream instead of each polling /dashboard/performance.
  metrics: PerformanceMetrics | null
}

export const MetricsProviderContext = createContext<MetricsProviderState | undefined>(undefined)
