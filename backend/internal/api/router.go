package api

import (
	"net/http"
)

func RegisterRoutes(s *Server) http.Handler {
	mux := http.NewServeMux()

	// 1. Authentication (Rate-limited, bypass auth header check)
	mux.Handle("POST /api/auth/login", RateLimitMiddleware(http.HandlerFunc(s.HandleLogin)))
	mux.HandleFunc("POST /api/auth/logout", s.HandleLogout)

	// Helper wrapper for authentication-protected endpoints
	authRoute := func(pattern string, handler func(http.ResponseWriter, *http.Request)) {
		mux.Handle(pattern, AuthMiddleware(http.HandlerFunc(handler)))
	}

	// 2. Dashboard Widgets
	authRoute("GET /api/dashboard/stats", s.HandleGetDashboardStats)
	authRoute("GET /api/dashboard/performance", s.HandleGetPerformanceMetrics)
	authRoute("GET /api/dashboard/logs", s.HandleGetRecentLogs)
	authRoute("POST /api/dashboard/logs/clear", s.HandleClearLogs)
	authRoute("GET /api/dashboard/logs/stream", s.HandleLogStream)

	// 3. Network Interfaces
	authRoute("GET /api/interfaces", s.HandleGetInterfaces)
	authRoute("PUT /api/interfaces/{id}", s.HandleUpdateInterface)
	authRoute("POST /api/interfaces/{id}/toggle", s.HandleToggleInterface)
	authRoute("GET /api/interfaces/{id}/scan", s.HandleScanWifi)

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
	authRoute("GET /api/dhcp/reservations", s.HandleGetDHCPReservations)
	authRoute("POST /api/dhcp/reservations", s.HandleCreateDHCPReservation)
	authRoute("PUT /api/dhcp/reservations/{id}", s.HandleUpdateDHCPReservation)
	authRoute("DELETE /api/dhcp/reservations/{id}", s.HandleDeleteDHCPReservation)
	authRoute("GET /api/dhcp/leases", s.HandleGetDHCPLeases)
	authRoute("POST /api/dhcp/apply", s.HandleApplyDHCP)

	// 9. System Management & Backup
	authRoute("GET /api/system/time", s.HandleGetSystemTime)
	authRoute("PUT /api/system/time", s.HandleUpdateSystemTime)
	authRoute("GET /api/system/dns", s.HandleGetDNSConfig)
	authRoute("PUT /api/system/dns", s.HandleUpdateDNSConfig)
	authRoute("PUT /api/system/password", s.HandleChangePassword)
	authRoute("GET /api/system/services", s.HandleGetSystemServices)
	authRoute("POST /api/system/services/{id}/restart", s.HandleRestartService)
	authRoute("POST /api/system/reboot", s.HandleReboot)
	authRoute("POST /api/system/shutdown", s.HandleShutdown)
	authRoute("GET /api/system/config/export", s.HandleExportConfig)
	authRoute("POST /api/system/config/import", s.HandleImportConfig)

	var handler http.Handler = mux
	if s.disableEdit {
		handler = DisableEditMiddleware(handler)
	}
	// Global CORS Wrapper must be outermost to ensure CORS headers are set on all responses (including 403 Forbidden)
	return CORSMiddleware(handler)
}
