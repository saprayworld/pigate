import React, { useEffect, useState } from "react"

import { MetricsProviderContext } from "@/hooks/metrics-context"
import {
  dashboardService,
  type PerformanceMetrics,
} from "@/services/dashboardService"

/**
 * Opens a SINGLE SSE metrics stream for the whole authenticated shell and shares
 * the latest PerformanceMetrics snapshot via context. Mounted once at the layout
 * level so the Dashboard StatGrid and the site-header temp badge read from one
 * connection instead of each polling /dashboard/performance (which previously ran
 * two independent 5s polls of the same endpoint). Keeping it to one connection
 * also avoids stacking persistent connections against the HTTP/1.1 ~6-per-host cap
 * alongside the log stream.
 */
export function MetricsProvider({ children }: { children: React.ReactNode }) {
  const [metrics, setMetrics] = useState<PerformanceMetrics | null>(null)

  useEffect(() => {
    const stop = dashboardService.connectSSEMetrics({
      onMetrics: (m) => setMetrics(m),
      // EventSource hides the HTTP status, so a 401 on reconnect (session
      // revoked/expired) is invisible here. A one-shot fetch routes that failure
      // through the fetch wrapper's 401 -> /login redirect (config.ts). This
      // provider is mounted on every authenticated page, so this is what bounces
      // an expired session to login.
      onError: () => {
        dashboardService.getPerformanceMetrics().catch(() => {
          /* redirect (if any) handled by the fetch wrapper */
        })
      },
    })
    return stop
  }, [])

  return (
    <MetricsProviderContext.Provider value={{ metrics }}>
      {children}
    </MetricsProviderContext.Provider>
  )
}
