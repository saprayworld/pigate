package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
	"pigate/internal/service"
)

type Server struct {
	repo              *db.Repository
	firewall          kernel.FirewallManager
	network           kernel.NetworkManager
	routing           kernel.RoutingManager
	dhcp              kernel.DhcpManager
	logs              *logs.RingBuffer
	disableEdit       bool
	interfaceService  *service.InterfaceService
	routingService    *service.RoutingService
	firewallService   *service.FirewallService
	dnsService        *service.DNSService
	qosService        *service.QosService
	dhcpServerService *service.DhcpServerService
	dnsServerService  *service.DNSServerService
	hostnameService   *service.HostnameService
	timeService       *service.TimeService
	userService       *service.UserService
	backupService     *service.BackupService
	systemStatus      *service.SystemStatusService
	powerService      *service.PowerService
	eventLog          *service.EventLogService
}

func NewServer(
	repo *db.Repository,
	fw kernel.FirewallManager,
	net kernel.NetworkManager,
	rt kernel.RoutingManager,
	dhcp kernel.DhcpManager,
	l *logs.RingBuffer,
	disableEdit bool,
	ifaceService *service.InterfaceService,
	routingService *service.RoutingService,
	fwService *service.FirewallService,
	dnsService *service.DNSService,
	qosService *service.QosService,
	dhcpServerService *service.DhcpServerService,
	dnsServerService *service.DNSServerService,
	hostnameService *service.HostnameService,
	timeService *service.TimeService,
	userService *service.UserService,
	backupService *service.BackupService,
	systemStatus *service.SystemStatusService,
	powerService *service.PowerService,
	eventLog *service.EventLogService,
) *Server {
	return &Server{
		repo:              repo,
		firewall:          fw,
		network:           net,
		routing:           rt,
		dhcp:              dhcp,
		logs:              l,
		disableEdit:       disableEdit,
		interfaceService:  ifaceService,
		routingService:    routingService,
		firewallService:   fwService,
		dnsService:        dnsService,
		qosService:        qosService,
		dhcpServerService: dhcpServerService,
		dnsServerService:  dnsServerService,
		hostnameService:   hostnameService,
		timeService:       timeService,
		userService:       userService,
		backupService:     backupService,
		systemStatus:      systemStatus,
		powerService:      powerService,
		eventLog:          eventLog,
	}
}

// Helpers
func (s *Server) writeJSON(w http.ResponseWriter, status int, data interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}

func (s *Server) writeError(w http.ResponseWriter, status int, message string) {
	s.writeJSON(w, status, map[string]string{"message": message})
}

func maskInterfacePasswords(iface *model.NetworkInterface) {
	if iface.WifiPassword != nil && *iface.WifiPassword != "" {
		masked := "••••••••"
		iface.WifiPassword = &masked
	}
	if iface.BackupWifiPassword != nil && *iface.BackupWifiPassword != "" {
		masked := "••••••••"
		iface.BackupWifiPassword = &masked
	}
}

func generateRandomToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// logLoginFailed records a failed login attempt. Only the attempted username is
// logged — never the password field (see plan §5.4).
func (s *Server) logLoginFailed(username, reason string) {
	if s.eventLog == nil {
		return
	}
	s.eventLog.Log(model.EventCategoryAuth, "login.failed", model.EventSeverityWarning,
		username, username, "Login failed for "+username+" ("+reason+")")
}

// logEvent records a system event with the authenticated user from the request
// context as actor. Handlers call it only after the operation succeeded (except
// login.failed, which logs directly via s.eventLog). Nil-safe so tests that
// build a Server without an EventLogService keep working.
func (s *Server) logEvent(r *http.Request, category, action, severity, target, msg string) {
	if s.eventLog == nil {
		return
	}
	actor, _ := r.Context().Value(UserContextKey).(string)
	s.eventLog.Log(category, action, severity, actor, target, msg)
}

// =========================================================================
// AUTHENTICATION HANDLERS
// =========================================================================

func (s *Server) HandleLogin(w http.ResponseWriter, r *http.Request) {
	var req model.LoginRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	user, err := s.repo.GetUserByUsername(req.Username)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Database error")
		return
	}
	if user == nil {
		s.logLoginFailed(req.Username, "unknown username")
		s.writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	// Verify Password hash using Bcrypt
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		s.logLoginFailed(req.Username, "wrong password")
		s.writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	// Reject disabled accounts after verifying the password so we don't leak
	// account existence to a wrong-password attempt. This is an internal admin
	// box, so a clear message for the legitimate owner is acceptable.
	if user.Status == model.StatusDisabled {
		s.logLoginFailed(req.Username, "account disabled")
		s.writeError(w, http.StatusUnauthorized, "บัญชีนี้ถูกปิดใช้งาน")
		return
	}

	token := "session_id_" + generateRandomToken()
	AddSession(token, user.Username)

	if s.eventLog != nil {
		s.eventLog.Log(model.EventCategoryAuth, "login.success", model.EventSeverityInfo,
			user.Username, user.Username, "User "+user.Username+" logged in")
	}

	// Set secure cookie
	http.SetCookie(w, &http.Cookie{
		Name:     SessionKey,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   false, // Set to true in HTTPS production
		SameSite: http.SameSiteStrictMode,
		Expires:  time.Now().Add(24 * time.Hour),
	})

	s.writeJSON(w, http.StatusOK, model.LoginResponse{
		Token:              token,
		MustChangePassword: user.IsInitial,
		Role:               user.Role,
	})
}

func (s *Server) HandleLogout(w http.ResponseWriter, r *http.Request) {
	// Extract token
	var token string
	authHeader := r.Header.Get("Authorization")
	if strings.HasPrefix(authHeader, "Bearer ") {
		token = strings.TrimPrefix(authHeader, "Bearer ")
	}
	if token == "" {
		cookie, err := r.Cookie(SessionKey)
		if err == nil {
			token = cookie.Value
		}
	}

	if token != "" {
		RemoveSession(token)
	}

	// Clear cookie
	http.SetCookie(w, &http.Cookie{
		Name:     SessionKey,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})

	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleCheckSession(w http.ResponseWriter, r *http.Request) {
	// AuthMiddleware has already validated the session and injected the real
	// username + role — no hardcoded fallback.
	username, _ := r.Context().Value(UserContextKey).(string)
	role, _ := r.Context().Value(RoleContextKey).(string)

	user, err := s.repo.GetUserByUsername(username)
	mustChangePassword := false
	if err == nil && user != nil {
		mustChangePassword = user.IsInitial
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":              true,
		"username":           username,
		"role":               role,
		"mustChangePassword": mustChangePassword,
	})
}

// =========================================================================
// DASHBOARD HANDLERS
// =========================================================================

func mapWpaState(state string) string {
	switch state {
	case "COMPLETED":
		return "Connected"
	case "DISCONNECTED":
		return "Disconnected"
	case "INACTIVE":
		return "Inactive"
	case "SCANNING":
		return "Scanning"
	case "ASSOCIATING", "AUTHENTICATING", "ASSOCIATED", "4WAY_HANDSHAKE", "GROUP_HANDSHAKE":
		return "Connecting"
	case "INTERFACE_DISABLED":
		return "Disabled"
	default:
		return state
	}
}

func (s *Server) HandleGetDashboardStats(w http.ResponseWriter, r *http.Request) {
	leases, _ := s.dhcp.GetActiveLeases()
	ifaces, _ := s.interfaceService.GetDataLayerInterface()

	wifiSSID := "None"
	wifiStatus := "Disconnected"
	for _, iface := range ifaces {
		if iface.Type == "wireless" {
			if wifiStat, err := s.network.GetWifiStatus(iface.Name); err == nil {
				wifiStatus = mapWpaState(wifiStat.State)
				if wifiStat.SSID != "" {
					wifiSSID = wifiStat.SSID
				} else {
					wifiSSID = "None"
				}
			} else {
				if iface.WifiSSID != nil && *iface.WifiSSID != "" {
					wifiSSID = *iface.WifiSSID
					wifiStatus = "Connected (DB)"
				}
			}
		}
	}

	trafficIn, trafficOut := s.systemStatus.GetTrafficTotals()

	stats := model.DashboardStats{
		FirewallStatus:       "Active",
		TotalTrafficInBytes:  trafficIn,
		TotalTrafficOutBytes: trafficOut,
		DhcpLeasesCount:      len(leases),
		WifiStatus:           wifiStatus,
		WifiSSID:             wifiSSID,
	}

	s.writeJSON(w, http.StatusOK, stats)
}

// HandleGetPerformanceMetrics returns real host telemetry (CPU/mem/temp/storage)
// composed by SystemStatusService. The flat cpu/memory/temp fields are retained
// for backward-compatibility; *Detail objects carry the richer data.
func (s *Server) HandleGetPerformanceMetrics(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.systemStatus.GetSystemMetrics())
}

// HandleGetSystemInfo returns hostname / version / OS / uptime / system time for
// the Dashboard's System Information card.
func (s *Server) HandleGetSystemInfo(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.systemStatus.GetSystemInfo())
}

// HandleGetTrafficHistory returns the RAM-buffered rx/tx history for the
// Bandwidth chart. Buckets accumulate since boot (fewer buckets right after a
// reboot is expected; the frontend copes).
func (s *Server) HandleGetTrafficHistory(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.systemStatus.GetTrafficHistory())
}

func (s *Server) HandleGetRecentLogs(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.logs.GetAll())
}

func (s *Server) HandleClearLogs(w http.ResponseWriter, r *http.Request) {
	s.logs.Clear()
	w.WriteHeader(http.StatusOK)
}

// HandleGetTrafficLogs returns forward-chain packet logs (newest first) from the
// RAM ring buffer, filtered in memory by the query params below. It reads the
// same buffer as the Dashboard Recent Logs widget; it never touches SQLite.
//
//	action  PASS | DROP        (case-insensitive; empty = all)
//	q       substring matched against src/dest/port/proto/interface/reason (case-insensitive)
//	limit   max rows to return (default 100, capped at the buffer capacity)
func (s *Server) HandleGetTrafficLogs(w http.ResponseWriter, r *http.Request) {
	query := r.URL.Query()
	action := strings.ToUpper(strings.TrimSpace(query.Get("action")))
	needle := strings.ToLower(strings.TrimSpace(query.Get("q")))

	all := s.logs.GetAll() // already newest-first
	limit := 100
	if v, err := strconv.Atoi(query.Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > len(all) {
		limit = len(all)
	}

	filtered := make([]model.FirewallLog, 0, limit)
	for _, entry := range all {
		if len(filtered) >= limit {
			break
		}
		if action != "" && strings.ToUpper(entry.Action) != action {
			continue
		}
		if needle != "" {
			hay := strings.ToLower(entry.Src + " " + entry.Dest + " " + entry.Port + " " + entry.Proto + " " + entry.InIface + " " + entry.OutIface + " " + entry.Reason)
			if !strings.Contains(hay, needle) {
				continue
			}
		}
		filtered = append(filtered, entry)
	}
	s.writeJSON(w, http.StatusOK, filtered)
}

// HandleGetSystemEvents returns central event log entries (newest first) with
// optional category/severity/q filters and limit/offset paging.
func (s *Server) HandleGetSystemEvents(w http.ResponseWriter, r *http.Request) {
	if s.eventLog == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Event log service not available")
		return
	}

	query := r.URL.Query()
	category := query.Get("category")
	severity := query.Get("severity")
	q := query.Get("q")

	limit := 50
	if v, err := strconv.Atoi(query.Get("limit")); err == nil && v > 0 {
		limit = v
	}
	if limit > 200 {
		limit = 200
	}
	offset := 0
	if v, err := strconv.Atoi(query.Get("offset")); err == nil && v > 0 {
		offset = v
	}

	events, total, err := s.eventLog.Query(category, severity, q, limit, offset)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"events": events,
		"total":  total,
	})
}

// HandleClearSystemEvents wipes the audit trail. super_admin only (see router);
// EventLogService.Clear immediately re-logs who performed the wipe.
func (s *Server) HandleClearSystemEvents(w http.ResponseWriter, r *http.Request) {
	if s.eventLog == nil {
		s.writeError(w, http.StatusServiceUnavailable, "Event log service not available")
		return
	}
	actor, _ := r.Context().Value(UserContextKey).(string)
	if err := s.eventLog.Clear(actor); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// INTERFACES HANDLERS
// =========================================================================

func (s *Server) HandleGetInterfaces(w http.ResponseWriter, r *http.Request) {
	list, err := s.interfaceService.GetDataLayerInterface()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	for i := range list {
		maskInterfacePasswords(&list[i])
	}
	s.writeJSON(w, http.StatusOK, list)
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	m := make(map[string]int)
	for _, x := range a {
		m[strings.TrimSpace(strings.ToUpper(x))]++
	}
	for _, x := range b {
		m[strings.TrimSpace(strings.ToUpper(x))]--
	}
	for _, count := range m {
		if count != 0 {
			return false
		}
	}
	return true
}

func (s *Server) HandleUpdateInterface(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	var updates model.NetworkInterface
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Check if admin access has changed
	adminAccessChanged := !equalStringSlices(iface.AdminAccess, updates.AdminAccess)

	// Apply updates to existing interface object
	iface.Alias = updates.Alias
	iface.Role = updates.Role
	iface.AddressingMode = updates.AddressingMode
	iface.IP = updates.IP
	iface.Netmask = updates.Netmask
	iface.Gateway = updates.Gateway
	iface.MacAddress = updates.MacAddress
	iface.AdminAccess = updates.AdminAccess
	iface.Status = updates.Status

	if updates.MacMode != nil {
		iface.MacMode = updates.MacMode
	}
	if updates.LaaMacAddress != nil {
		iface.LaaMacAddress = updates.LaaMacAddress
	}
	if updates.RandomizeOnReconnect != nil {
		iface.RandomizeOnReconnect = updates.RandomizeOnReconnect
	}
	if updates.BackupSSID != nil {
		iface.BackupSSID = updates.BackupSSID
	}
	// Safe password updates in PUT: only set if password is not empty and not masked, or if security is Open
	if updates.BackupWifiPassword != nil {
		backupSec := ""
		if updates.BackupWifiSecurity != nil {
			backupSec = *updates.BackupWifiSecurity
		} else if iface.BackupWifiSecurity != nil {
			backupSec = *iface.BackupWifiSecurity
		}
		if *updates.BackupWifiPassword != "••••••••" {
			if *updates.BackupWifiPassword != "" || backupSec == "Open" {
				iface.BackupWifiPassword = updates.BackupWifiPassword
			}
		}
	}
	if updates.WifiSSID != nil {
		iface.WifiSSID = updates.WifiSSID
	}
	if updates.WifiPassword != nil {
		primarySec := ""
		if updates.WifiSecurity != nil {
			primarySec = *updates.WifiSecurity
		} else if iface.WifiSecurity != nil {
			primarySec = *iface.WifiSecurity
		}
		if *updates.WifiPassword != "••••••••" {
			if *updates.WifiPassword != "" || primarySec == "Open" {
				iface.WifiPassword = updates.WifiPassword
			}
		}
	}
	if updates.WifiSecurity != nil {
		iface.WifiSecurity = updates.WifiSecurity
	}
	if updates.BackupWifiSecurity != nil {
		iface.BackupWifiSecurity = updates.BackupWifiSecurity
	}
	if updates.FailoverEnabled != nil {
		iface.FailoverEnabled = updates.FailoverEnabled
	}
	if updates.IPCheckTimeout != nil {
		iface.IPCheckTimeout = updates.IPCheckTimeout
	}
	if updates.PrimaryMaxRetries != nil {
		iface.PrimaryMaxRetries = updates.PrimaryMaxRetries
	}
	if updates.FailoverCooldown != nil {
		iface.FailoverCooldown = updates.FailoverCooldown
	}
	if updates.Metric != nil {
		iface.Metric = updates.Metric
	}

	if err := s.interfaceService.ApplyInterfaceConfig(*iface); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if adminAccessChanged {
		if err := s.syncFirewallRules(); err != nil {
			s.writeError(w, http.StatusInternalServerError, "OS Firewall update failed: "+err.Error())
			return
		}
	}

	s.logEvent(r, model.EventCategoryNetwork, "network.interface_changed", model.EventSeverityInfo,
		iface.Name, "Interface "+iface.Name+" configuration updated")
	maskInterfacePasswords(iface)
	s.writeJSON(w, http.StatusOK, iface)
}

func (s *Server) HandlePatchInterface(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	var body map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Check if admin access has changed
	adminAccessChanged := false
	if val, ok := body["adminAccess"]; ok {
		var access []string
		if err := json.Unmarshal(val, &access); err == nil {
			adminAccessChanged = !equalStringSlices(iface.AdminAccess, access)
			iface.AdminAccess = access
		}
	}

	updateString := func(key string, field *string) {
		if val, ok := body[key]; ok {
			var str string
			if err := json.Unmarshal(val, &str); err == nil {
				*field = str
			}
		}
	}

	updatePtrString := func(key string, field **string) {
		if val, ok := body[key]; ok {
			var str *string
			if err := json.Unmarshal(val, &str); err == nil {
				*field = str
			}
		}
	}

	updatePtrBool := func(key string, field **bool) {
		if val, ok := body[key]; ok {
			var b *bool
			if err := json.Unmarshal(val, &b); err == nil {
				*field = b
			}
		}
	}

	updatePtrInt := func(key string, field **int) {
		if val, ok := body[key]; ok {
			var valInt *int
			if err := json.Unmarshal(val, &valInt); err == nil {
				*field = valInt
			}
		}
	}

	updateString("alias", &iface.Alias)
	updateString("role", &iface.Role)
	updateString("addressingMode", &iface.AddressingMode)
	updateString("ip", &iface.IP)
	updateString("netmask", &iface.Netmask)
	updateString("gateway", &iface.Gateway)
	updateString("macAddress", &iface.MacAddress)
	updateString("status", &iface.Status)

	updatePtrString("wifiSSID", &iface.WifiSSID)
	updatePtrString("wifiSecurity", &iface.WifiSecurity)
	updatePtrString("macMode", &iface.MacMode)
	updatePtrString("laaMacAddress", &iface.LaaMacAddress)
	updatePtrBool("randomizeOnReconnect", &iface.RandomizeOnReconnect)
	updatePtrBool("failoverEnabled", &iface.FailoverEnabled)
	updatePtrString("backupSsid", &iface.BackupSSID)
	updatePtrString("backupWifiSecurity", &iface.BackupWifiSecurity)
	updatePtrInt("ipCheckTimeout", &iface.IPCheckTimeout)
	updatePtrInt("primaryMaxRetries", &iface.PrimaryMaxRetries)
	updatePtrInt("failoverCooldown", &iface.FailoverCooldown)
	updatePtrInt("metric", &iface.Metric) // null clears it back to "unset" (auto)

	// Safe password updates: only if non-empty and not masked, or if security is explicitly set to Open
	if val, ok := body["wifiPassword"]; ok {
		var pass *string
		if err := json.Unmarshal(val, &pass); err == nil {
			secMode := ""
			if iface.WifiSecurity != nil {
				secMode = *iface.WifiSecurity
			}
			if pass != nil && *pass != "••••••••" {
				if *pass != "" || secMode == "Open" {
					iface.WifiPassword = pass
				}
			}
		}
	}

	if val, ok := body["backupWifiPassword"]; ok {
		var pass *string
		if err := json.Unmarshal(val, &pass); err == nil {
			backupSecMode := ""
			if iface.BackupWifiSecurity != nil {
				backupSecMode = *iface.BackupWifiSecurity
			}
			if pass != nil && *pass != "••••••••" {
				if *pass != "" || backupSecMode == "Open" {
					iface.BackupWifiPassword = pass
				}
			}
		}
	}

	if err := s.interfaceService.ApplyInterfaceConfig(*iface); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if adminAccessChanged {
		if err := s.syncFirewallRules(); err != nil {
			s.writeError(w, http.StatusInternalServerError, "OS Firewall update failed: "+err.Error())
			return
		}
	}

	s.logEvent(r, model.EventCategoryNetwork, "network.interface_changed", model.EventSeverityInfo,
		iface.Name, "Interface "+iface.Name+" configuration updated")
	maskInterfacePasswords(iface)
	s.writeJSON(w, http.StatusOK, iface)
}

func (s *Server) HandleToggleInterface(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	nextStatus := "up"
	if iface.Status == "up" {
		nextStatus = "down"
	}

	// Trigger OS action
	err = s.network.ToggleInterface(iface.Name, nextStatus == "up")
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "OS level configuration failed")
		return
	}

	iface.Status = nextStatus
	if err := s.repo.UpdateInterface(*iface); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryNetwork, "network.interface_changed", model.EventSeverityInfo,
		iface.Name, "Interface "+iface.Name+" toggled "+nextStatus)
	maskInterfacePasswords(iface)
	s.writeJSON(w, http.StatusOK, iface)
}

func (s *Server) HandleScanWifi(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	if iface.Type != "wireless" {
		s.writeError(w, http.StatusBadRequest, "Interface is not a wireless interface")
		return
	}

	results, err := s.network.ScanWifi(iface.Name)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, results)
}

func (s *Server) HandleGetWifiStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	if iface.Type != "wireless" {
		s.writeError(w, http.StatusBadRequest, "Interface is not a wireless interface")
		return
	}

	status, err := s.network.GetWifiStatus(iface.Name)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

func (s *Server) HandleDeleteInterface(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	if iface.Status != "offline" {
		s.writeError(w, http.StatusBadRequest, "Cannot delete active interfaces. Only offline interfaces can be deleted.")
		return
	}

	if err := s.repo.DeleteInterface(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{"success": true})
}

func (s *Server) HandleResetInterface(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	if err := s.interfaceService.FlushInterfaceConfig(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// Refreshed default settings from kernel
	refreshed, err := s.interfaceService.GetDataLayerInterfaceByID(id)
	if err != nil || refreshed == nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to load refreshed interface default config")
		return
	}

	maskInterfacePasswords(refreshed)
	s.writeJSON(w, http.StatusOK, refreshed)
}

// =========================================================================
// FIREWALL POLICY HANDLERS
// =========================================================================

func (s *Server) HandleGetPolicies(w http.ResponseWriter, r *http.Request) {
	list, err := s.firewallService.GetPolicies()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleCreatePolicy(w http.ResponseWriter, r *http.Request) {
	var input model.PolicyRuleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	rule := model.PolicyRule{
		ID:           "rule-" + generateRandomToken()[:8],
		Name:         input.Name,
		InInterface:  input.InInterface,
		OutInterface: input.OutInterface,
		Source:       input.Source,
		Destination:  input.Destination,
		Service:      input.Service,
		Action:       input.Action,
		Log:          input.Log,
		Status:       input.Status,
	}

	if err := s.firewallService.CreatePolicy(rule); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.logEvent(r, model.EventCategoryFirewall, "firewall.policy_created", model.EventSeverityInfo,
		rule.Name, "Firewall policy \""+rule.Name+"\" created")
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) HandleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.firewallService.GetPolicyByID(id)
	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "Policy rule not found")
		return
	}

	var input model.PolicyRuleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	rule := model.PolicyRule{
		ID:           id,
		Name:         input.Name,
		InInterface:  input.InInterface,
		OutInterface: input.OutInterface,
		Source:       input.Source,
		Destination:  input.Destination,
		Service:      input.Service,
		Action:       input.Action,
		Log:          input.Log,
		Status:       input.Status,
	}

	if err := s.firewallService.UpdatePolicy(rule); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.logEvent(r, model.EventCategoryFirewall, "firewall.policy_updated", model.EventSeverityInfo,
		rule.Name, "Firewall policy \""+rule.Name+"\" updated")
	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) HandleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target := id
	if p, _ := s.firewallService.GetPolicyByID(id); p != nil {
		target = p.Name
	}
	if err := s.firewallService.DeletePolicy(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryFirewall, "firewall.policy_deleted", model.EventSeverityInfo,
		target, "Firewall policy \""+target+"\" deleted")
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleReorderPolicies(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Policies []model.PolicyRule `json:"policies"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := s.firewallService.ReorderPolicies(body.Policies); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, body.Policies)
}

func (s *Server) HandleTogglePolicyLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.firewallService.TogglePolicyLog(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

func (s *Server) HandleTogglePolicyStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	p, err := s.firewallService.TogglePolicyStatus(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, p)
}

func (s *Server) syncFirewallRules() error {
	return s.firewallService.SyncFirewallRules()
}

func (s *Server) HandleApplyPolicies(w http.ResponseWriter, r *http.Request) {
	if err := s.syncFirewallRules(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OS Firewall update failed: "+err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryFirewall, "firewall.applied", model.EventSeverityInfo,
		"nftables", "Firewall policies applied to kernel")
	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// ADDRESS OBJECTS HANDLERS
// =========================================================================

func (s *Server) HandleGetAddresses(w http.ResponseWriter, r *http.Request) {
	list, err := s.firewallService.GetAddresses()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleCreateAddress(w http.ResponseWriter, r *http.Request) {
	var input model.AddressObjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	addr := model.AddressObject{
		ID:          "addr-" + generateRandomToken()[:8],
		Name:        input.Name,
		Type:        input.Type,
		Value:       input.Value,
		System:      false,
		RefPolicies: []string{},
	}

	if err := s.firewallService.CreateAddress(addr); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, addr)
}

func (s *Server) HandleUpdateAddress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.firewallService.GetAddressByID(id)
	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "Address object not found")
		return
	}

	var input model.AddressObjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	addr := model.AddressObject{
		ID:     id,
		Name:   input.Name,
		Type:   input.Type,
		Value:  input.Value,
		System: false,
	}

	if err := s.firewallService.UpdateAddress(addr); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, addr)
}

func (s *Server) HandleDeleteAddress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.firewallService.DeleteAddress(id); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleBulkDeleteAddresses(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := s.firewallService.BulkDeleteAddresses(body.IDs); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// SERVICE OBJECTS HANDLERS
// =========================================================================

func (s *Server) HandleGetServices(w http.ResponseWriter, r *http.Request) {
	list, err := s.firewallService.GetServices()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleCreateService(w http.ResponseWriter, r *http.Request) {
	var input model.ServiceObjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	svc := model.ServiceObject{
		ID:          "svc-" + generateRandomToken()[:8],
		Name:        input.Name,
		Protocol:    input.Protocol,
		Port:        input.Port,
		Type:        "custom",
		RefPolicies: []string{},
	}

	if err := s.firewallService.CreateService(svc); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, svc)
}

func (s *Server) HandleUpdateService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.firewallService.GetServiceByID(id)
	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "Service object not found")
		return
	}

	var input model.ServiceObjectInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	svc := model.ServiceObject{
		ID:       id,
		Name:     input.Name,
		Protocol: input.Protocol,
		Port:     input.Port,
		Type:     "custom",
	}

	if err := s.firewallService.UpdateService(svc); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, svc)
}

func (s *Server) HandleDeleteService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.firewallService.DeleteService(id); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// STATIC ROUTES HANDLERS
// =========================================================================

func (s *Server) HandleGetRoutes(w http.ResponseWriter, r *http.Request) {
	list, err := s.routingService.GetRouting()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleCreateRoute(w http.ResponseWriter, r *http.Request) {
	var input model.StaticRouteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	route := model.StaticRoute{
		ID:          "route-" + generateRandomToken()[:8],
		Destination: input.Destination,
		Gateway:     input.Gateway,
		Interface:   input.Interface,
		Metric:      input.Metric,
		Description: input.Description,
		Status:      input.Status,
		Type:        "custom",
		Scope:       input.Scope,
		Src:         input.Src,
		Proto:       input.Proto,
	}

	if err := s.routingService.ApplyConfigRoute(route); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryRoute, "route.created", model.EventSeverityInfo,
		route.Destination, "Static route to "+route.Destination+" created")
	s.writeJSON(w, http.StatusOK, route)
}

func (s *Server) HandleUpdateRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var existing *model.StaticRoute
	var err error

	if s.routingService.IsEnableEditSystemRoute() && strings.HasPrefix(id, "route-sys-") {
		routes, getErr := s.routingService.GetRouting()
		if getErr == nil {
			for _, r := range routes {
				if r.ID == id {
					existing = &r
					break
				}
			}
		}
	} else {
		existing, err = s.repo.GetRouteByID(id)
	}

	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "Route not found")
		return
	}

	var input model.StaticRouteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	routeType := "custom"
	if s.routingService.IsEnableEditSystemRoute() && strings.HasPrefix(id, "route-sys-") {
		routeType = existing.Type
	}

	route := model.StaticRoute{
		ID:          id,
		Destination: input.Destination,
		Gateway:     input.Gateway,
		Interface:   input.Interface,
		Metric:      input.Metric,
		Description: input.Description,
		Status:      input.Status,
		Type:        routeType,
		Scope:       input.Scope,
		Src:         input.Src,
		Proto:       input.Proto,
	}

	if err := s.routingService.ApplyConfigRoute(route); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryRoute, "route.updated", model.EventSeverityInfo,
		route.Destination, "Static route to "+route.Destination+" updated")
	s.writeJSON(w, http.StatusOK, route)
}

func (s *Server) HandleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	target := id
	if rt, _ := s.repo.GetRouteByID(id); rt != nil {
		target = rt.Destination
	}
	if err := s.routingService.RemoveConfigRoute(id); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryRoute, "route.deleted", model.EventSeverityInfo,
		target, "Static route to "+target+" deleted")
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleBulkDeleteRoutes(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []string `json:"ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := s.routingService.BulkRemoveConfigRoutes(body.IDs); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleToggleRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.routingService.ToggleConfigRoute(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	var route *model.StaticRoute
	if s.routingService.IsEnableEditSystemRoute() && strings.HasPrefix(id, "route-sys-") {
		routes, err := s.routingService.GetRouting()
		if err == nil {
			for _, r := range routes {
				if r.ID == id {
					route = &r
					break
				}
			}
		}
	} else {
		route, _ = s.repo.GetRouteByID(id)
	}
	s.writeJSON(w, http.StatusOK, route)
}

func (s *Server) HandleApplyRoutes(w http.ResponseWriter, r *http.Request) {
	if err := s.routingService.InitApplyConfig(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OS routing configuration update failed: "+err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryRoute, "route.applied", model.EventSeverityInfo,
		"routing", "Static routes applied to kernel")
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleGetRoutesConfig(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"allowEditSystemRoutes":  s.repo.GetAllowEditSystemRoutes(),
		"prioritizeKernelRoutes": s.repo.GetPrioritizeKernelRoutes(),
		"enableEditSystemRoute":  s.routingService.IsEnableEditSystemRoute(),
	})
}

// =========================================================================
// DHCP SERVER HANDLERS
// =========================================================================

func (s *Server) HandleGetDHCPConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.repo.GetDHCPConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) HandleUpdateDHCPConfig(w http.ResponseWriter, r *http.Request) {
	var cfg model.DhcpConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := s.repo.UpdateDHCPConfig(cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryDhcp, "dhcp.config_changed", model.EventSeverityInfo,
		cfg.Interface, "DHCP server config for "+cfg.Interface+" updated")
	s.writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) HandleGetDHCPReservations(w http.ResponseWriter, r *http.Request) {
	list, err := s.repo.GetDHCPReservations()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleCreateDHCPReservation(w http.ResponseWriter, r *http.Request) {
	var input model.DhcpReservationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	res := model.DhcpReservation{
		ID:         "res-" + generateRandomToken()[:8],
		DeviceName: input.DeviceName,
		MacAddress: input.MacAddress,
		IPAddress:  input.IPAddress,
	}

	if err := s.repo.CreateDHCPReservation(res); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, res)
}

func (s *Server) HandleUpdateDHCPReservation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetDHCPReservationByID(id)
	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "DHCP Reservation not found")
		return
	}

	var input model.DhcpReservationInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	res := model.DhcpReservation{
		ID:         id,
		DeviceName: input.DeviceName,
		MacAddress: input.MacAddress,
		IPAddress:  input.IPAddress,
	}

	if err := s.repo.UpdateDHCPReservation(res); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, res)
}

func (s *Server) HandleDeleteDHCPReservation(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteDHCPReservation(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleGetDHCPLeases(w http.ResponseWriter, r *http.Request) {
	leases, err := s.repo.GetDHCPLeases()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Fallback to active leases from system/kernel if DB is empty
	if len(leases) == 0 {
		leases, err = s.dhcp.GetActiveLeases()
		if err != nil {
			s.writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if leases == nil {
		leases = []model.ActiveDhcpLease{}
	}
	s.writeJSON(w, http.StatusOK, leases)
}

func (s *Server) HandleApplyDHCP(w http.ResponseWriter, r *http.Request) {
	if err := s.dhcpServerService.ApplyAll(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.firewallService.SyncFirewallRules(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryDhcp, "dhcp.applied", model.EventSeverityInfo,
		"dnsmasq", "DHCP server configuration applied")
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleGetDHCPConfigs(w http.ResponseWriter, r *http.Request) {
	cfgs, err := s.repo.GetDHCPConfigs()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cfgs == nil {
		cfgs = []model.DhcpConfig{}
	}
	s.writeJSON(w, http.StatusOK, cfgs)
}

func (s *Server) HandleCreateDHCPConfig(w http.ResponseWriter, r *http.Request) {
	var cfg model.DhcpConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := s.repo.CreateDHCPConfig(cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) HandleUpdateDHCPConfigByID(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var cfg model.DhcpConfig
	if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	cfg.ID = id

	if err := s.repo.UpdateDHCPConfigByID(cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) HandleDeleteDHCPConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteDHCPConfig(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleToggleDHCPConfig(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.ToggleDHCPConfig(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleGetAvailableInterfaces(w http.ResponseWriter, r *http.Request) {
	ifaces, err := s.repo.GetInterfaces()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	cfgs, err := s.repo.GetDHCPConfigs()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	configured := make(map[string]bool)
	for _, c := range cfgs {
		configured[c.Interface] = true
	}

	available := []string{}
	for _, iface := range ifaces {
		if iface.Role == "LAN" && !configured[iface.Name] {
			available = append(available, iface.Name)
		}
	}

	s.writeJSON(w, http.StatusOK, available)
}

// =========================================================================
// SYSTEM SETTINGS & MAINTENANCE HANDLERS
// =========================================================================

func (s *Server) HandleGetSystemTime(w http.ResponseWriter, r *http.Request) {
	settings, err := s.timeService.Get()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, settings)
}

func (s *Server) HandleUpdateSystemTime(w http.ResponseWriter, r *http.Request) {
	var settings model.SystemTimeSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Validation errors are the user's fault (400); anything else is a
	// kernel/D-Bus failure (500).
	if err := service.ValidateTimezone(settings.Timezone); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := service.ValidateNTPServer(settings.NTPServer); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.timeService.Update(settings); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logEvent(r, model.EventCategorySystem, "system.time_changed", model.EventSeverityInfo,
		settings.Timezone, "System time settings updated (timezone "+settings.Timezone+")")

	// Return the fresh state (config + live status) so the UI can refresh.
	updated, err := s.timeService.Get()
	if err != nil {
		s.writeJSON(w, http.StatusOK, settings)
		return
	}
	s.writeJSON(w, http.StatusOK, updated)
}

func (s *Server) HandleSetManualTime(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Datetime string `json:"datetime"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Distinguish validation/state errors (400) from kernel failures (500).
	if _, err := service.ValidateManualTime(body.Datetime); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.timeService.SetManualTime(body.Datetime); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.logEvent(r, model.EventCategorySystem, "system.time_changed", model.EventSeverityInfo,
		"clock", "System clock set manually")

	settings, err := s.timeService.Get()
	if err != nil {
		s.writeJSON(w, http.StatusOK, map[string]string{"message": "ตั้งเวลาสำเร็จ"})
		return
	}
	s.writeJSON(w, http.StatusOK, settings)
}

func (s *Server) HandleGetHostname(w http.ResponseWriter, r *http.Request) {
	settings, err := s.hostnameService.Get()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, settings)
}

func (s *Server) HandleUpdateHostname(w http.ResponseWriter, r *http.Request) {
	var settings model.SystemHostnameSettings
	if err := json.NewDecoder(r.Body).Decode(&settings); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if err := service.ValidateHostname(settings.Hostname); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	if err := s.hostnameService.Update(settings); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategorySystem, "system.hostname_changed", model.EventSeverityInfo,
		settings.Hostname, "Hostname changed to "+settings.Hostname)
	s.writeJSON(w, http.StatusOK, settings)
}

func (s *Server) HandleGetDNSConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.dnsService.GetDNSConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) HandleUpdateDNSConfig(w http.ResponseWriter, r *http.Request) {
	var input model.DNSConfigInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	if input.LocalDomain == "" {
		input.LocalDomain = "pigate.local"
	}

	if err := s.dnsService.UpdateDNSConfig(input); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// System DNS is the upstream source for the local DNS server (dnsmasq).
	// Regenerate pigate-dns.conf so its `server=` lines reflect the new config;
	// otherwise the forwarders stay stale. The System DNS change already
	// succeeded, so a failure here is logged, not surfaced as a request error.
	if err := s.dnsServerService.ApplyAll(); err != nil {
		log.Printf("[HandleUpdateDNSConfig] Warning: failed to regenerate DNS server config after System DNS update: %v", err)
	}

	s.logEvent(r, model.EventCategoryDns, "dns.config_changed", model.EventSeverityInfo,
		"system-dns", "System DNS settings updated (mode "+input.Mode+")")
	s.writeJSON(w, http.StatusOK, input)
}

func (s *Server) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req model.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	// Resolve the authenticated user from context (set by AuthMiddleware) so a
	// user only ever changes their own password — never a hardcoded account.
	username, _ := r.Context().Value(UserContextKey).(string)
	if username == "" {
		s.writeError(w, http.StatusUnauthorized, "Unauthorized")
		return
	}

	user, err := s.repo.GetUserByUsername(username)
	if err != nil || user == nil {
		s.writeError(w, http.StatusInternalServerError, "User context resolution failed")
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword))
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "รหัสผ่านปัจจุบันไม่ถูกต้อง")
		return
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 10)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Crypto generation failed")
		return
	}

	if err := s.repo.ChangePassword(username, string(newHash)); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.logEvent(r, model.EventCategoryAuth, "auth.password_changed", model.EventSeverityInfo,
		username, "User "+username+" changed their password")
	w.WriteHeader(http.StatusOK)
}

// =========================================================================
// USER MANAGEMENT HANDLERS (super_admin only — see router superAdminRoute)
// =========================================================================

// writeUserServiceError maps a UserService error to an HTTP status: a missing
// target is 404, everything else (validation + guard rails) is 400 with the
// service's Thai message surfaced to the UI.
func (s *Server) writeUserServiceError(w http.ResponseWriter, err error) {
	if err == service.ErrUserNotFound {
		s.writeError(w, http.StatusNotFound, err.Error())
		return
	}
	s.writeError(w, http.StatusBadRequest, err.Error())
}

func (s *Server) HandleGetUsers(w http.ResponseWriter, r *http.Request) {
	users, err := s.userService.List()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, users)
}

func (s *Server) HandleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req model.CreateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	user, err := s.userService.Create(req)
	if err != nil {
		s.writeUserServiceError(w, err)
		return
	}
	s.logEvent(r, model.EventCategoryUser, "user.created", model.EventSeverityInfo,
		user.Username, "User "+user.Username+" created (role "+user.Role+")")
	s.writeJSON(w, http.StatusCreated, user)
}

func (s *Server) HandleUpdateUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var req model.UpdateUserRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}
	actor, _ := r.Context().Value(UserContextKey).(string)
	if err := s.userService.Update(actor, id, req); err != nil {
		s.writeUserServiceError(w, err)
		return
	}
	target := id
	if u, _ := s.repo.GetUserByID(id); u != nil {
		target = u.Username
	}
	s.logEvent(r, model.EventCategoryUser, "user.updated", model.EventSeverityInfo,
		target, "User "+target+" updated")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleDeleteUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor, _ := r.Context().Value(UserContextKey).(string)

	// Capture the username before deletion so we can purge lingering sessions.
	target, _ := s.repo.GetUserByID(id)

	if err := s.userService.Delete(actor, id); err != nil {
		s.writeUserServiceError(w, err)
		return
	}
	targetName := id
	if target != nil {
		RemoveSessionsForUser(target.Username)
		targetName = target.Username
	}
	s.logEvent(r, model.EventCategoryUser, "user.deleted", model.EventSeverityWarning,
		targetName, "User "+targetName+" deleted")
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleToggleUser(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	actor, _ := r.Context().Value(UserContextKey).(string)
	if err := s.userService.Toggle(actor, id); err != nil {
		s.writeUserServiceError(w, err)
		return
	}
	// If the account is now disabled, purge its sessions immediately.
	if u, _ := s.repo.GetUserByID(id); u != nil {
		if u.Status == model.StatusDisabled {
			RemoveSessionsForUser(u.Username)
		}
		s.logEvent(r, model.EventCategoryUser, "user.toggled", model.EventSeverityInfo,
			u.Username, "User "+u.Username+" status changed to "+u.Status)
	}
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleGetSystemServices(w http.ResponseWriter, r *http.Request) {
	// Custom Mock System services
	list := []model.NetworkServiceStatus{
		{ID: "srv-1", Name: "Firewall Engine", ServiceName: "nftables", Status: "running"},
		{ID: "srv-2", Name: "DHCP Server", ServiceName: "isc-dhcp-server", Status: "running"},
		{ID: "srv-3", Name: "Network Core Manager", ServiceName: "NetworkManager", Status: "running"},
		{ID: "srv-4", Name: "dnsmasq Daemon", ServiceName: "dnsmasq", Status: "running"},
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	// Trigger service restart via systemd Mock
	w.WriteHeader(http.StatusOK)
}

// HandleReboot restarts the physical host. super_admin only (see router). The
// service delays the actual login1 D-Bus call ~1s so this 200 reaches the
// browser before logind stops pigate.service.
//
// The event MUST be flushed synchronously before powerService fires: once
// logind starts stopping pigate.service, anything still queued in the batch
// writer is lost — the exact failure mode of the old RAM-only logPowerEvent.
func (s *Server) HandleReboot(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(UserContextKey).(string)
	s.logPowerEvent(r, "system.reboot", "Reboot", username)
	if err := s.powerService.Reboot(username); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to reboot: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// HandleShutdown powers off the physical host. super_admin only (see router).
// Same log-then-flush-then-power ordering as HandleReboot.
func (s *Server) HandleShutdown(w http.ResponseWriter, r *http.Request) {
	username, _ := r.Context().Value(UserContextKey).(string)
	s.logPowerEvent(r, "system.shutdown", "Shutdown", username)
	if err := s.powerService.Shutdown(username); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to shutdown: "+err.Error())
		return
	}
	w.WriteHeader(http.StatusOK)
}

// logPowerEvent persists a power action (critical severity) and flushes it to
// SQLite before returning, so it survives the imminent process shutdown.
func (s *Server) logPowerEvent(r *http.Request, action, verb, username string) {
	if s.eventLog == nil {
		return
	}
	if username == "" {
		username = "unknown"
	}
	s.logEvent(r, model.EventCategorySystem, action, model.EventSeverityCritical,
		"host", verb+" requested by "+username)
	if err := s.eventLog.Flush(); err != nil {
		log.Printf("[Power] Failed to flush event log before power action: %v", err)
	}
}

// HandleExportConfig streams a full, typed configuration backup (schema v2).
// Restricted to super_admin (see router) because the payload contains real
// Wi-Fi passwords and, optionally, user credential hashes. Pass ?includeUsers=1
// to embed the users table.
func (s *Server) HandleExportConfig(w http.ResponseWriter, r *http.Request) {
	includeUsers := r.URL.Query().Get("includeUsers") == "1" || r.URL.Query().Get("includeUsers") == "true"
	// Optional passphrase encrypts the config; sent via header (not query) to
	// keep it out of access logs.
	passphrase := r.Header.Get("X-Backup-Passphrase")

	backup, err := s.backupService.Export(includeUsers, passphrase)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Failed to export configuration: "+err.Error())
		return
	}

	// Content-Disposition helps direct endpoint calls; the SPA builds its own
	// filename (§3.1) since it downloads via fetch+Blob.
	filename := fmt.Sprintf("pigate-backup-%s-%s.json",
		sanitizeFilenamePart(backup.Meta.Hostname),
		time.Now().Format("20060102-150405"))
	w.Header().Set("Content-Disposition", "attachment; filename=\""+filename+"\"")
	s.logEvent(r, model.EventCategoryConfig, "config.exported", model.EventSeverityWarning,
		filename, "Configuration exported")
	s.writeJSON(w, http.StatusOK, backup)
}

// HandleImportConfig validates, snapshots, restores (single transaction), and
// re-applies a configuration backup. Restricted to super_admin and blocked in
// -disable-edit mode by DisableEditMiddleware. Returns an ImportResult with
// counts + non-fatal warnings on success, or a 4xx/5xx with the reason (and no
// DB changes) on failure.
func (s *Server) HandleImportConfig(w http.ResponseWriter, r *http.Request) {
	// Cap the request body at 10 MB — a backup is small; anything larger is
	// abuse or corruption.
	r.Body = http.MaxBytesReader(w, r.Body, 10<<20)
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		s.writeError(w, http.StatusBadRequest, "Failed to read request body (max 10 MB): "+err.Error())
		return
	}

	actor, _ := r.Context().Value(UserContextKey).(string)
	var actorID string
	if u, _ := s.repo.GetUserByUsername(actor); u != nil {
		actorID = u.ID
	}

	// includeUsers is driven by whether the file carries users AND the caller
	// opted in via query flag; default is to ignore users in the file.
	includeUsers := r.URL.Query().Get("includeUsers") == "1" || r.URL.Query().Get("includeUsers") == "true"

	result, err := s.backupService.Import(raw, model.ImportOptions{
		IncludeUsers:  includeUsers,
		ActorUserID:   actorID,
		ActorUsername: actor,
		Passphrase:    r.Header.Get("X-Backup-Passphrase"),
	})
	if err != nil {
		// An encrypted backup without a passphrase gets a specific signal so the
		// UI can prompt for one instead of showing a generic failure.
		if errors.Is(err, service.ErrPassphraseRequired) {
			s.writeJSON(w, http.StatusUnprocessableEntity, map[string]interface{}{
				"message":        err.Error(),
				"needPassphrase": true,
			})
			return
		}
		s.writeError(w, http.StatusBadRequest, "Import failed: "+err.Error())
		return
	}

	// Purge sessions of users removed/disabled by the import so they can't keep
	// acting with a stale token.
	for _, uname := range result.RemovedUsernames {
		RemoveSessionsForUser(uname)
	}

	s.logEvent(r, model.EventCategoryConfig, "config.imported", model.EventSeverityWarning,
		"database", "Configuration imported and re-applied")
	s.writeJSON(w, http.StatusOK, result)
}

// sanitizeFilenamePart keeps a hostname safe for use inside a download filename.
func sanitizeFilenamePart(s string) string {
	if s == "" {
		return "pigate"
	}
	var b strings.Builder
	for _, c := range s {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_':
			b.WriteRune(c)
		default:
			b.WriteRune('-')
		}
	}
	return b.String()
}

// =========================================================================
// LOG SSE STREAMING HANDLER
// =========================================================================

func (s *Server) HandleLogStream(w http.ResponseWriter, r *http.Request) {
	// Set SSE HTTP Headers
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "Streaming unsupported!", http.StatusInternalServerError)
		return
	}

	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	// Initial message
	_, _ = w.Write([]byte("event: connected\ndata: connection established\n\n"))
	flusher.Flush()

	clientDone := r.Context().Done()

	for {
		select {
		case <-clientDone:
			return
		case <-ticker.C:
			// Stream live block logs from our circular memory buffer
			logsList := s.logs.GetAll()
			if len(logsList) > 0 {
				data, err := json.Marshal(logsList[0]) // stream latest log
				if err == nil {
					_, _ = w.Write([]byte("data: " + string(data) + "\n\n"))
					flusher.Flush()
				}
			}
		}
	}
}

// =============================================================================
// QoS Handlers
// =============================================================================

// HandleGetQosRules returns all QoS bandwidth rules.
func (s *Server) HandleGetQosRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.qosService.GetRules()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to retrieve QoS rules")
		return
	}
	if rules == nil {
		rules = []model.QosRule{}
	}
	s.writeJSON(w, http.StatusOK, rules)
}

// HandleGetQosRule returns a single QoS rule by ID.
func (s *Server) HandleGetQosRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rule, err := s.qosService.GetRuleByID(id)
	if err != nil {
		s.writeError(w, http.StatusNotFound, "QoS rule not found")
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

// HandleCreateQosRule creates a new QoS rule and applies it to the kernel.
func (s *Server) HandleCreateQosRule(w http.ResponseWriter, r *http.Request) {
	var input model.QosRuleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	if input.Name == "" || input.Interface == "" {
		s.writeError(w, http.StatusBadRequest, "name and interface are required")
		return
	}
	rule, err := s.qosService.CreateRule(input)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to create QoS rule")
		return
	}
	s.writeJSON(w, http.StatusCreated, rule)
}

// HandleUpdateQosRule updates an existing QoS rule and re-syncs the kernel.
func (s *Server) HandleUpdateQosRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var input model.QosRuleInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}
	rule, err := s.qosService.UpdateRule(id, input)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to update QoS rule")
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

// HandleDeleteQosRule removes a QoS rule and re-syncs the kernel.
func (s *Server) HandleDeleteQosRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.qosService.DeleteRule(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to delete QoS rule")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"message": "QoS rule deleted"})
}

// HandleToggleQosRule toggles the enabled/disabled status of a QoS rule.
func (s *Server) HandleToggleQosRule(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	rule, err := s.qosService.ToggleRuleStatus(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to toggle QoS rule status")
		return
	}
	s.writeJSON(w, http.StatusOK, rule)
}

// HandleSyncQosRules forces a full re-sync of all QoS rules from DB to kernel.
func (s *Server) HandleSyncQosRules(w http.ResponseWriter, r *http.Request) {
	if err := s.qosService.SyncToKernel(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to sync QoS rules to kernel")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"message": "QoS rules synced to kernel"})
}

// HandleGetQosIfaceStatus returns the live kernel qdisc/class state for an interface.
func (s *Server) HandleGetQosIfaceStatus(w http.ResponseWriter, r *http.Request) {
	iface := r.PathValue("iface")
	status, err := s.qosService.GetIfaceStatus(iface)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to get QoS status for interface")
		return
	}
	s.writeJSON(w, http.StatusOK, status)
}

// HandleClearQosIface disables all DB rules for an interface and clears the kernel qdisc.
func (s *Server) HandleClearQosIface(w http.ResponseWriter, r *http.Request) {
	iface := r.PathValue("iface")
	if err := s.qosService.ClearIface(iface); err != nil {
		s.writeError(w, http.StatusInternalServerError, "failed to clear QoS for interface")
		return
	}
	s.writeJSON(w, http.StatusOK, map[string]string{"message": "QoS cleared for interface " + iface})
}

// =========================================================================
// DNS SERVER (dnsmasq Local DNS) HANDLERS
// =========================================================================

func (s *Server) HandleGetDNSZones(w http.ResponseWriter, r *http.Request) {
	zones, err := s.repo.GetDNSZones()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if zones == nil {
		zones = []model.DNSZone{}
	}
	s.writeJSON(w, http.StatusOK, zones)
}

func (s *Server) HandleCreateDNSZone(w http.ResponseWriter, r *http.Request) {
	var input model.DNSZoneInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	zone := model.DNSZone{
		ID:              "zone-" + generateRandomToken()[:8],
		ZoneName:        input.ZoneName,
		ForwardTo:       input.ForwardTo,
		AllowedIPs:      input.AllowedIPs,
		IsAuthoritative: input.IsAuthoritative,
		Enabled:         input.Enabled,
		Records:         []model.DNSRecord{},
	}

	if err := s.repo.CreateDNSZone(zone); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, zone)
}

func (s *Server) HandleUpdateDNSZone(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetDNSZoneByID(id)
	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "DNS Zone not found")
		return
	}

	var input model.DNSZoneInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	zone := model.DNSZone{
		ID:              id,
		ZoneName:        input.ZoneName,
		ForwardTo:       input.ForwardTo,
		AllowedIPs:      input.AllowedIPs,
		IsAuthoritative: input.IsAuthoritative,
		Enabled:         input.Enabled,
		Records:         existing.Records,
	}

	if err := s.repo.UpdateDNSZone(zone); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, zone)
}

func (s *Server) HandleDeleteDNSZone(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteDNSZone(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleToggleDNSZone(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.ToggleDNSZone(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleGetDNSRecords(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	records, err := s.repo.GetDNSRecordsByZone(id)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if records == nil {
		records = []model.DNSRecord{}
	}
	s.writeJSON(w, http.StatusOK, records)
}

func (s *Server) HandleCreateDNSRecord(w http.ResponseWriter, r *http.Request) {
	zoneID := r.PathValue("id")
	var input model.DNSRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	record := model.DNSRecord{
		ID:     "rec-" + generateRandomToken()[:8],
		ZoneID: zoneID,
		Name:   input.Name,
		Type:   input.Type,
		Value:  input.Value,
		TTL:    input.TTL,
	}

	if err := s.repo.CreateDNSRecord(record); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, record)
}

func (s *Server) HandleUpdateDNSRecord(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetDNSRecordByID(id)
	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "DNS Record not found")
		return
	}

	var input model.DNSRecordInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	record := model.DNSRecord{
		ID:     id,
		ZoneID: existing.ZoneID,
		Name:   input.Name,
		Type:   input.Type,
		Value:  input.Value,
		TTL:    input.TTL,
	}

	if err := s.repo.UpdateDNSRecord(record); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, record)
}

func (s *Server) HandleDeleteDNSRecord(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteDNSRecord(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleApplyDNSServer(w http.ResponseWriter, r *http.Request) {
	if err := s.dnsServerService.ApplyAll(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.firewallService.SyncFirewallRules(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.logEvent(r, model.EventCategoryDns, "dns.server_applied", model.EventSeverityInfo,
		"dnsmasq", "DNS server zones/records applied")
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleClearDNSCache(w http.ResponseWriter, r *http.Request) {
	if err := s.dnsServerService.ClearCache(); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

// HandleGetDNSServerSettings returns the interfaces the DNS Server is currently bound to.
func (s *Server) HandleGetDNSServerSettings(w http.ResponseWriter, r *http.Request) {
	interfaces, err := s.repo.GetDNSServerInterfaces()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, model.DNSServerSettings{Interfaces: interfaces})
}

// HandleUpdateDNSServerSettings saves the set of real interfaces (from Interface Service)
// the DNS Server should bind to. Kept independent from DHCP Server configuration.
func (s *Server) HandleUpdateDNSServerSettings(w http.ResponseWriter, r *http.Request) {
	var input model.DNSServerSettings
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	realIfaces, err := s.interfaceService.GetDataLayerInterface()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	valid := make(map[string]bool)
	for _, iface := range realIfaces {
		valid[iface.Name] = true
	}
	for _, name := range input.Interfaces {
		if !valid[name] {
			s.writeError(w, http.StatusBadRequest, fmt.Sprintf("interface %s does not exist", name))
			return
		}
	}

	if err := s.repo.SetDNSServerInterfaces(input.Interfaces); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, input)
}
