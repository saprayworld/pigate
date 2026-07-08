import { useContext } from "react"
import { HostnameProviderContext } from "@/hooks/hostname-context"

export const useHostname = () => {
  const context = useContext(HostnameProviderContext)
  if (context === undefined)
    throw new Error("useHostname must be used within a HostnameProvider")
  return context
}
