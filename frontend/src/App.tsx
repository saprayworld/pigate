import React from "react"
import { BrowserRouter, Routes, Route, Navigate } from "react-router-dom"
import { ThemeProvider } from "@/components/ThemeProvider"
import { AlertDialogProvider } from "@/components/AlertDialogProvider"
import ShellLayout from "@/components/layout/ShellLayout"
import Dashboard from "@/pages/Dashboard"
import Interfaces from "@/pages/Interfaces"
import StaticRoutes from "@/pages/StaticRoutes"
import DhcpServer from "@/pages/DhcpServer"
import FirewallPolicy from "@/pages/FirewallPolicy"
import Addresses from "@/pages/Addresses"
import Services from "@/pages/Services"
import SettingsMaintenance from "@/pages/SettingsMaintenance"
import Login from "@/pages/Login"

// A simple authentication route guard
function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = !!localStorage.getItem("pigate_session")
  return isAuthenticated ? <>{children}</> : <Navigate to="/login" replace />
}

export default function App() {
  return (
    <ThemeProvider defaultTheme="dark" storageKey="pigate-ui-theme">
      <AlertDialogProvider>
        <BrowserRouter>

        <Routes>
          {/* Public Login Route */}
          <Route path="/login" element={<Login />} />

          {/* Protected Admin Routes under Shell Layout */}
          <Route
            path="/"
            element={
              <ProtectedRoute>
                <ShellLayout />
              </ProtectedRoute>
            }
          >
            {/* Index route redirects to /dashboard */}
            <Route index element={<Navigate to="/dashboard" replace />} />
            
            <Route path="dashboard" element={<Dashboard />} />
            
            {/* Network Routes */}
            <Route path="network">
              <Route path="interfaces" element={<Interfaces />} />
              <Route path="routes" element={<StaticRoutes />} />
              <Route path="dhcp" element={<DhcpServer />} />
            </Route>

            {/* Policy & Objects Routes */}
            <Route path="policy">
              <Route path="firewall" element={<FirewallPolicy />} />
              <Route path="addresses" element={<Addresses />} />
              <Route path="services" element={<Services />} />
            </Route>

            {/* System Routes */}
            <Route path="system">
              <Route path="settings" element={<SettingsMaintenance />} />
            </Route>
          </Route>

          {/* Fallback Catch-All Redirect */}
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
     </AlertDialogProvider>
    </ThemeProvider>
  )
}