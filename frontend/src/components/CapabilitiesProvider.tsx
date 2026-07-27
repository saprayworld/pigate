import React, { useCallback, useEffect, useState } from "react"

import { CapabilitiesProviderContext } from "@/hooks/capability-context"
import { getCapabilities, type CapabilityStatus } from "@/services/capabilityService"

// Refetch cadence — capability rarely changes at runtime (docs plan §2.4), so
// this just keeps a long-lived tab reasonably fresh; it is not a substitute
// for the explicit "ตรวจสอบอีกครั้ง" refresh() below.
const REFETCH_INTERVAL_MS = 5 * 60 * 1000

/**
 * Fetches kernel capability status (issue #94) once at mount and on a 5-minute
 * timer, sharing the result via context for CapabilityBanner instances across
 * every page. A fetch failure (e.g. backend restarting) is swallowed and
 * leaves `loaded` false — callers must not show an "unavailable" banner from
 * that state (docs plan §5 Caution 6: no false alarms).
 */
export function CapabilitiesProvider({ children }: { children: React.ReactNode }) {
  const [capabilities, setCapabilities] = useState<CapabilityStatus[]>([])
  const [loaded, setLoaded] = useState(false)

  const fetchCapabilities = useCallback((force: boolean) => {
    let alive = true
    getCapabilities(force)
      .then((res) => {
        if (!alive) return
        setCapabilities(res.capabilities)
        setLoaded(true)
      })
      .catch(() => {
        // Fetch failed (e.g. backend restarting) — keep whatever we had
        // before (or stay unloaded) rather than flashing a false alarm.
      })
    return () => {
      alive = false
    }
  }, [])

  useEffect(() => {
    const cleanup = fetchCapabilities(false)
    const interval = setInterval(() => fetchCapabilities(false), REFETCH_INTERVAL_MS)
    return () => {
      cleanup()
      clearInterval(interval)
    }
  }, [fetchCapabilities])

  const refresh = useCallback(() => {
    fetchCapabilities(true)
  }, [fetchCapabilities])

  return (
    <CapabilitiesProviderContext.Provider value={{ capabilities, loaded, refresh }}>
      {children}
    </CapabilitiesProviderContext.Provider>
  )
}
