import { Outlet } from "react-router-dom"

import { AppSidebar } from "@/components/app-sidebar"
import { HostnameProvider } from "@/components/HostnameProvider"
import { SiteHeader } from "@/components/site-header"
import { SidebarInset, SidebarProvider } from "@/components/ui/sidebar"

export default function ShellLayout() {
  return (
    <HostnameProvider>
      <SidebarProvider
        style={
          {
            "--sidebar-width": "16rem",
          } as React.CSSProperties
        }
      >
        <AppSidebar variant="inset" />
        <SidebarInset>
          <SiteHeader />
          <main className="flex-1 overflow-y-auto p-4 md:p-6">
            <Outlet />
          </main>
        </SidebarInset>
      </SidebarProvider>
    </HostnameProvider>
  )
}
