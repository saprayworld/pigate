package api

import (
	"net/http"
)

func RegisterRoutes(s *Server) http.Handler {
	mux := http.NewServeMux()

	// 1. Authentication (Rate-limited, bypass auth header check)
	mux.Handle("POST /api/auth/login", RateLimitMiddleware(http.HandlerFunc(s.HandleLogin)))
	mux.HandleFunc("POST /api/auth/logout", s.HandleLogout)

	// Helper wrapper for authentication-protected endpoints. Order matters:
	// AuthMiddleware runs first (validates session, injects username+role), then
	// RoleReadOnlyMiddleware blocks mutations for non-super_admin roles.
	authRoute := func(pattern string, handler func(http.ResponseWriter, *http.Request)) {
		mux.Handle(pattern, s.AuthMiddleware(RoleReadOnlyMiddleware(http.HandlerFunc(handler))))
	}

	// superAdminRoute restricts a route to super_admin only (including GET), so
	// a read-only admin can't even see the account list.
	superAdminRoute := func(pattern string, handler func(http.ResponseWriter, *http.Request)) {
		mux.Handle(pattern, s.AuthMiddleware(SuperAdminMiddleware(http.HandlerFunc(handler))))
	}

	authRoute("GET /api/auth/session", s.HandleCheckSession)

	// User Management (super_admin only)
	superAdminRoute("GET /api/users", s.HandleGetUsers)
	superAdminRoute("POST /api/users", s.HandleCreateUser)
	superAdminRoute("PUT /api/users/{id}", s.HandleUpdateUser)
	superAdminRoute("DELETE /api/users/{id}", s.HandleDeleteUser)
	superAdminRoute("POST /api/users/{id}/toggle", s.HandleToggleUser)

	// 2. Dashboard Widgets
	authRoute("GET /api/dashboard/stats", s.HandleGetDashboardStats)
	authRoute("GET /api/dashboard/performance", s.HandleGetPerformanceMetrics)
	authRoute("GET /api/dashboard/traffic", s.HandleGetTrafficHistory)
	authRoute("GET /api/dashboard/logs", s.HandleGetRecentLogs)
	authRoute("POST /api/dashboard/logs/clear", s.HandleClearLogs)
	authRoute("GET /api/dashboard/logs/stream", s.HandleLogStream)

	// 3. Network Interfaces
	authRoute("GET /api/interfaces", s.HandleGetInterfaces)
	authRoute("PUT /api/interfaces/{id}", s.HandleUpdateInterface)
	authRoute("PATCH /api/interfaces/{id}", s.HandlePatchInterface)
	authRoute("POST /api/interfaces/{id}/toggle", s.HandleToggleInterface)
	authRoute("POST /api/interfaces/{id}/reset", s.HandleResetInterface)
	authRoute("DELETE /api/interfaces/{id}", s.HandleDeleteInterface)
	authRoute("GET /api/interfaces/{id}/scan", s.HandleScanWifi)
	authRoute("GET /api/interfaces/{id}/wifi-status", s.HandleGetWifiStatus)

	// 4. Firewall Policies
	authRoute("GET /api/policies", s.HandleGetPolicies)
	authRoute("POST /api/policies", s.HandleCreatePolicy)
	authRoute("PUT /api/policies/{id}", s.HandleUpdatePolicy)
	authRoute("DELETE /api/policies/{id}", s.HandleDeletePolicy)
	authRoute("PUT /api/policies/reorder", s.HandleReorderPolicies)
	authRoute("POST /api/policies/{id}/toggle-log", s.HandleTogglePolicyLog)
	authRoute("POST /api/policies/{id}/toggle-status", s.HandleTogglePolicyStatus)
	authRoute("POST /api/policies/apply", s.HandleApplyPolicies)

	// 5. Address Objects
	authRoute("GET /api/addresses", s.HandleGetAddresses)
	authRoute("POST /api/addresses", s.HandleCreateAddress)
	authRoute("PUT /api/addresses/{id}", s.HandleUpdateAddress)
	authRoute("DELETE /api/addresses/{id}", s.HandleDeleteAddress)
	authRoute("POST /api/addresses/bulk-delete", s.HandleBulkDeleteAddresses)

	// 6. Service Objects
	authRoute("GET /api/services", s.HandleGetServices)
	authRoute("POST /api/services", s.HandleCreateService)
	authRoute("PUT /api/services/{id}", s.HandleUpdateService)
	authRoute("DELETE /api/services/{id}", s.HandleDeleteService)

	// 7. Static Routes
	authRoute("GET /api/routes", s.HandleGetRoutes)
	authRoute("GET /api/routes/config", s.HandleGetRoutesConfig)
	authRoute("POST /api/routes", s.HandleCreateRoute)
	authRoute("PUT /api/routes/{id}", s.HandleUpdateRoute)
	authRoute("DELETE /api/routes/{id}", s.HandleDeleteRoute)
	authRoute("POST /api/routes/bulk-delete", s.HandleBulkDeleteRoutes)
	authRoute("POST /api/routes/{id}/toggle", s.HandleToggleRoute)
	authRoute("POST /api/routes/apply", s.HandleApplyRoutes)

	// 8. DHCP Server Settings
	authRoute("GET /api/dhcp/config", s.HandleGetDHCPConfig)
	authRoute("PUT /api/dhcp/config", s.HandleUpdateDHCPConfig)
	authRoute("GET /api/dhcp/configs", s.HandleGetDHCPConfigs)
	authRoute("POST /api/dhcp/configs", s.HandleCreateDHCPConfig)
	authRoute("PUT /api/dhcp/configs/{id}", s.HandleUpdateDHCPConfigByID)
	authRoute("DELETE /api/dhcp/configs/{id}", s.HandleDeleteDHCPConfig)
	authRoute("POST /api/dhcp/configs/{id}/toggle", s.HandleToggleDHCPConfig)
	authRoute("GET /api/dhcp/interfaces", s.HandleGetAvailableInterfaces)
	authRoute("GET /api/dhcp/reservations", s.HandleGetDHCPReservations)
	authRoute("POST /api/dhcp/reservations", s.HandleCreateDHCPReservation)
	authRoute("PUT /api/dhcp/reservations/{id}", s.HandleUpdateDHCPReservation)
	authRoute("DELETE /api/dhcp/reservations/{id}", s.HandleDeleteDHCPReservation)
	authRoute("GET /api/dhcp/leases", s.HandleGetDHCPLeases)
	authRoute("POST /api/dhcp/apply", s.HandleApplyDHCP)

	// 8.1 DNS Server Settings (dnsmasq Local Zone/Records)
	authRoute("GET /api/dns/zones", s.HandleGetDNSZones)
	authRoute("POST /api/dns/zones", s.HandleCreateDNSZone)
	authRoute("PUT /api/dns/zones/{id}", s.HandleUpdateDNSZone)
	authRoute("DELETE /api/dns/zones/{id}", s.HandleDeleteDNSZone)
	authRoute("POST /api/dns/zones/{id}/toggle", s.HandleToggleDNSZone)
	authRoute("GET /api/dns/zones/{id}/records", s.HandleGetDNSRecords)
	authRoute("POST /api/dns/zones/{id}/records", s.HandleCreateDNSRecord)
	authRoute("PUT /api/dns/records/{id}", s.HandleUpdateDNSRecord)
	authRoute("DELETE /api/dns/records/{id}", s.HandleDeleteDNSRecord)
	authRoute("POST /api/dns/apply", s.HandleApplyDNSServer)
	authRoute("POST /api/dns/clear-cache", s.HandleClearDNSCache)
	authRoute("GET /api/dns/settings", s.HandleGetDNSServerSettings)
	authRoute("PUT /api/dns/settings", s.HandleUpdateDNSServerSettings)

	// 9. System Management & Backup
	authRoute("GET /api/system/info", s.HandleGetSystemInfo)
	authRoute("GET /api/system/time", s.HandleGetSystemTime)
	authRoute("PUT /api/system/time", s.HandleUpdateSystemTime)
	authRoute("POST /api/system/time/manual", s.HandleSetManualTime)
	authRoute("GET /api/system/hostname", s.HandleGetHostname)
	authRoute("PUT /api/system/hostname", s.HandleUpdateHostname)
	authRoute("GET /api/system/dns", s.HandleGetDNSConfig)
	authRoute("PUT /api/system/dns", s.HandleUpdateDNSConfig)
	authRoute("PUT /api/system/password", s.HandleChangePassword)
	authRoute("GET /api/system/services", s.HandleGetSystemServices)
	authRoute("POST /api/system/services/{id}/restart", s.HandleRestartService)
	// Reboot/shutdown physically power-cycle the board — super_admin only, made
	// explicit here (same as config export/import) rather than relying on
	// RoleReadOnlyMiddleware to block the POST for lower roles.
	superAdminRoute("POST /api/system/reboot", s.HandleReboot)
	superAdminRoute("POST /api/system/shutdown", s.HandleShutdown)
	// Export/Import handle real Wi-Fi passwords and (optionally) user credential
	// hashes, so both are super_admin only — a read-only admin must not be able
	// to exfiltrate secrets via a backup.
	superAdminRoute("GET /api/system/config/export", s.HandleExportConfig)
	superAdminRoute("POST /api/system/config/import", s.HandleImportConfig)

	// 10. QoS Bandwidth Rules
	authRoute("GET /api/qos/rules", s.HandleGetQosRules)
	authRoute("POST /api/qos/rules", s.HandleCreateQosRule)
	authRoute("GET /api/qos/rules/{id}", s.HandleGetQosRule)
	authRoute("PUT /api/qos/rules/{id}", s.HandleUpdateQosRule)
	authRoute("DELETE /api/qos/rules/{id}", s.HandleDeleteQosRule)
	authRoute("POST /api/qos/rules/{id}/toggle", s.HandleToggleQosRule)
	authRoute("POST /api/qos/sync", s.HandleSyncQosRules)
	authRoute("GET /api/qos/status/{iface}", s.HandleGetQosIfaceStatus)
	authRoute("DELETE /api/qos/iface/{iface}", s.HandleClearQosIface)

	// Serve embedded static frontend files
	serveStatic(mux)

	var handler http.Handler = mux
	if s.disableEdit {
		handler = DisableEditMiddleware(handler)
	}
	// Global CORS Wrapper must be outermost to ensure CORS headers are set on all responses (including 403 Forbidden)
	return CORSMiddleware(handler)
}
