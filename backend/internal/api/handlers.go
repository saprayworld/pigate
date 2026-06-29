package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
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
	repo             *db.Repository
	firewall         kernel.FirewallManager
	network          kernel.NetworkManager
	routing          kernel.RoutingManager
	dhcp             kernel.DhcpManager
	logs             *logs.RingBuffer
	disableEdit      bool
	interfaceService *service.InterfaceService
	routingService   *service.RoutingService
	firewallService  *service.FirewallService
	dnsService       *service.DNSService
	qosService       *service.QosService
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
) *Server {
	return &Server{
		repo:             repo,
		firewall:         fw,
		network:          net,
		routing:          rt,
		dhcp:             dhcp,
		logs:             l,
		disableEdit:      disableEdit,
		interfaceService: ifaceService,
		routingService:   routingService,
		firewallService:  fwService,
		dnsService:       dnsService,
		qosService:       qosService,
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
		s.writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	// Verify Password hash using Bcrypt
	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.Password))
	if err != nil {
		s.writeError(w, http.StatusUnauthorized, "Invalid username or password")
		return
	}

	token := "session_id_" + generateRandomToken()
	AddSession(token, user.Username)

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
	username, _ := r.Context().Value(UserContextKey).(string)
	if username == "" {
		username = "pigate"
	}

	user, err := s.repo.GetUserByUsername(username)
	mustChangePassword := false
	if err == nil && user != nil {
		mustChangePassword = user.IsInitial
	}

	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"valid":              true,
		"username":           username,
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

	stats := model.DashboardStats{
		FirewallStatus:  "Active",
		TotalTrafficIn:  "8.7 GB",
		TotalTrafficOut: "3.7 GB",
		DhcpLeasesCount: len(leases),
		WifiStatus:      wifiStatus,
		WifiSSID:        wifiSSID,
	}

	s.writeJSON(w, http.StatusOK, stats)
}

func (s *Server) HandleGetPerformanceMetrics(w http.ResponseWriter, r *http.Request) {
	// Simulated values reflecting typical board states
	metrics := model.PerformanceMetrics{
		CPU:    15.4,
		Memory: 42.1,
		Temp:   48.5,
	}
	s.writeJSON(w, http.StatusOK, metrics)
}

func (s *Server) HandleGetRecentLogs(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, s.logs.GetAll())
}

func (s *Server) HandleClearLogs(w http.ResponseWriter, r *http.Request) {
	s.logs.Clear()
	w.WriteHeader(http.StatusOK)
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

	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) HandleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.firewallService.DeletePolicy(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	s.writeJSON(w, http.StatusOK, route)
}

func (s *Server) HandleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.routingService.RemoveConfigRoute(id); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
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
	leases, err := s.dhcp.GetActiveLeases()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, leases)
}

func (s *Server) HandleApplyDHCP(w http.ResponseWriter, r *http.Request) {
	cfg, err := s.repo.GetDHCPConfig()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.dhcp.ApplyConfig(*cfg); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OS DHCP daemon apply failed")
		return
	}

	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// SYSTEM SETTINGS & MAINTENANCE HANDLERS
// =========================================================================

func (s *Server) HandleGetSystemTime(w http.ResponseWriter, r *http.Request) {
	settings, err := s.repo.GetSystemTimeSettings()
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

	if err := s.repo.UpdateSystemTimeSettings(settings); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
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
	s.writeJSON(w, http.StatusOK, input)
}

func (s *Server) HandleChangePassword(w http.ResponseWriter, r *http.Request) {
	var req model.ChangePasswordRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	user, err := s.repo.GetUserByUsername("pigate")
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

	if err := s.repo.ChangePassword("pigate", string(newHash)); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleGetSystemServices(w http.ResponseWriter, r *http.Request) {
	// Custom Mock System services
	list := []model.NetworkServiceStatus{
		{ID: "srv-1", Name: "Firewall Engine", ServiceName: "nftables", Status: "running"},
		{ID: "srv-2", Name: "DHCP Server", ServiceName: "isc-dhcp-server", Status: "running"},
		{ID: "srv-3", Name: "Network Core Manager", ServiceName: "NetworkManager", Status: "running"},
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleRestartService(w http.ResponseWriter, r *http.Request) {
	// Trigger service restart via systemd Mock
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleReboot(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleShutdown(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) HandleExportConfig(w http.ResponseWriter, r *http.Request) {
	// Construct configuration JSON dump
	addrs, _ := s.repo.GetAddresses()
	svcs, _ := s.repo.GetServices()
	policies, _ := s.repo.GetPolicies()
	routes, _ := s.repo.GetRoutes()
	dhcpCfg, _ := s.repo.GetDHCPConfig()
	dhcpRes, _ := s.repo.GetDHCPReservations()
	ifaces, _ := s.repo.GetInterfaces()
	sysTime, _ := s.repo.GetSystemTimeSettings()

	backup := map[string]interface{}{
		"device":         "PiGate Firewall Gateway",
		"version":        "v1.0.0-Release",
		"exportedAt":     time.Now().Format(time.RFC3339),
		"systemSettings": sysTime,
		"config": map[string]interface{}{
			"addresses":      addrs,
			"serviceObjects": svcs,
			"policies":       policies,
			"routes":         routes,
			"dhcp": map[string]interface{}{
				"config":       dhcpCfg,
				"reservations": dhcpRes,
			},
			"interfaces": ifaces,
		},
	}

	s.writeJSON(w, http.StatusOK, backup)
}

func (s *Server) HandleImportConfig(w http.ResponseWriter, r *http.Request) {
	var dump struct {
		SystemSettings *model.SystemTimeSettings `json:"systemSettings"`
		Config         struct {
			Addresses      []model.AddressObject    `json:"addresses"`
			ServiceObjects []model.ServiceObject    `json:"serviceObjects"`
			Policies       []model.PolicyRule       `json:"policies"`
			Routes         []model.StaticRoute      `json:"routes"`
			Interfaces     []model.NetworkInterface `json:"interfaces"`
			DHCP           *struct {
				Config       *model.DhcpConfig       `json:"config"`
				Reservations []model.DhcpReservation `json:"reservations"`
			} `json:"dhcp"`
		} `json:"config"`
	}

	if err := json.NewDecoder(r.Body).Decode(&dump); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload structure: "+err.Error())
		return
	}

	// Begin restoration transactions
	if dump.SystemSettings != nil {
		_ = s.repo.UpdateSystemTimeSettings(*dump.SystemSettings)
	}

	cfg := dump.Config
	for _, a := range cfg.Addresses {
		if a.System {
			continue // skip system seeding duplicate
		}
		_ = s.repo.CreateAddress(a)
	}
	for _, sc := range cfg.ServiceObjects {
		if sc.Type == "system" {
			continue
		}
		_ = s.repo.CreateService(sc)
	}
	for _, p := range cfg.Policies {
		_ = s.repo.CreatePolicy(p)
	}
	for _, r := range cfg.Routes {
		if r.Type == "system" || r.Type == "defaultgateway" {
			continue
		}
		_ = s.repo.CreateRoute(r)
	}
	for _, i := range cfg.Interfaces {
		_ = s.repo.UpdateInterface(i)
	}

	if cfg.DHCP != nil {
		if cfg.DHCP.Config != nil {
			_ = s.repo.UpdateDHCPConfig(*cfg.DHCP.Config)
		}
		for _, dr := range cfg.DHCP.Reservations {
			_ = s.repo.CreateDHCPReservation(dr)
		}
	}

	w.WriteHeader(http.StatusOK)
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
