import React, { useEffect, useState } from "react"
import { HostnameProviderContext } from "@/hooks/hostname-context"
import { systemService } from "@/services/systemService"

const FALLBACK_HOSTNAME = "pigate"

export function HostnameProvider({ children }: { children: React.ReactNode }) {
  const [hostname, setHostname] = useState<string>(FALLBACK_HOSTNAME)

  useEffect(() => {
    let alive = true
    systemService
      .getHostname()
      .then((s) => {
        if (alive && s.hostname) setHostname(s.hostname)
      })
      .catch(() => {
        // keep fallback; sidebar shows "pigate" rather than an empty line
      })
    return () => {
      alive = false
    }
  }, [])

  return (
    <HostnameProviderContext.Provider value={{ hostname, setHostname }}>
      {children}
    </HostnameProviderContext.Provider>
  )
}
