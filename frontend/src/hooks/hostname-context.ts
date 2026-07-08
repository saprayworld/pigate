import { createContext } from "react"

export type HostnameProviderState = {
  // Current device hostname. Falls back to "pigate" while loading or on error
  // so the sidebar never shows an empty line (see sidebar-dynamic-hostname-plan).
  hostname: string
  // Optimistically update the shared hostname (e.g. after Settings saves).
  setHostname: (hostname: string) => void
}

export const HostnameProviderContext = createContext<HostnameProviderState | undefined>(undefined)
