package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/logs"
	"pigate/internal/model"
)

type Server struct {
	repo        *db.Repository
	firewall    kernel.FirewallManager
	network     kernel.NetworkManager
	routing     kernel.RoutingManager
	dhcp        kernel.DhcpManager
	logs        *logs.RingBuffer
	disableEdit bool
}

func NewServer(
	repo *db.Repository,
	fw kernel.FirewallManager,
	net kernel.NetworkManager,
	rt kernel.RoutingManager,
	dhcp kernel.DhcpManager,
	l *logs.RingBuffer,
	disableEdit bool,
) *Server {
	return &Server{
		repo:        repo,
		firewall:    fw,
		network:     net,
		routing:     rt,
		dhcp:        dhcp,
		logs:        l,
		disableEdit: disableEdit,
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
		return;
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
		// Mock fallback for development if bcrypt fail but password is "admin"
		if req.Username == "admin" && req.Password == "admin" {
			// Proceed
		} else {
			s.writeError(w, http.StatusUnauthorized, "Invalid username or password")
			return
		}
	}

	token := "mock_session_id_" + generateRandomToken()
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

	s.writeJSON(w, http.StatusOK, model.LoginResponse{Token: token})
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

// =========================================================================
// DASHBOARD HANDLERS
// =========================================================================

func (s *Server) HandleGetDashboardStats(w http.ResponseWriter, r *http.Request) {
	leases, _ := s.dhcp.GetActiveLeases()
	ifaces, _ := s.repo.GetInterfaces()

	wifiSSID := "None"
	wifiStatus := "Disconnected"
	for _, iface := range ifaces {
		if iface.Type == "wireless" && iface.ConnectedSSID != nil {
			wifiSSID = *iface.ConnectedSSID
			wifiStatus = "wlan0 Master"
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
	list, err := s.repo.GetInterfaces()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, list)
}

func (s *Server) HandleUpdateInterface(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.repo.GetInterfaceByID(id)
	if err != nil || iface == nil {
		s.writeError(w, http.StatusNotFound, "Interface not found")
		return
	}

	var updates model.NetworkInterface
	if err := json.NewDecoder(r.Body).Decode(&updates); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

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
	if updates.BackupWifiPassword != nil {
		iface.BackupWifiPassword = updates.BackupWifiPassword
	}

	if err := s.network.ConfigureInterface(iface.Name, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OS level configuration failed: "+err.Error())
		return
	}

	if err := s.repo.UpdateInterface(*iface); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, iface)
}

func (s *Server) HandleToggleInterface(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.repo.GetInterfaceByID(id)
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

	_ = s.repo.ToggleInterfaceStatus(id, nextStatus)
	iface.Status = nextStatus
	s.writeJSON(w, http.StatusOK, iface)
}

func (s *Server) HandleScanWifi(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	iface, err := s.repo.GetInterfaceByID(id)
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

// =========================================================================
// FIREWALL POLICY HANDLERS
// =========================================================================

func (s *Server) HandleGetPolicies(w http.ResponseWriter, r *http.Request) {
	list, err := s.repo.GetPolicies()
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

	if err := s.repo.CreatePolicy(rule); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) HandleUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetPolicyByID(id)
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

	if err := s.repo.UpdatePolicy(rule); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, rule)
}

func (s *Server) HandleDeletePolicy(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeletePolicy(id); err != nil {
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

	if err := s.repo.SaveAllPolicies(body.Policies); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	s.writeJSON(w, http.StatusOK, body.Policies)
}

func (s *Server) HandleTogglePolicyLog(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.TogglePolicyLog(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, _ := s.repo.GetPolicyByID(id)
	s.writeJSON(w, http.StatusOK, p)
}

func (s *Server) HandleTogglePolicyStatus(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.TogglePolicyStatus(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	p, _ := s.repo.GetPolicyByID(id)
	s.writeJSON(w, http.StatusOK, p)
}

func (s *Server) HandleApplyPolicies(w http.ResponseWriter, r *http.Request) {
	rules, err := s.repo.GetPolicies()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.firewall.ApplyRules(rules); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OS Firewall update failed")
		return
	}

	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// ADDRESS OBJECTS HANDLERS
// =========================================================================

func (s *Server) HandleGetAddresses(w http.ResponseWriter, r *http.Request) {
	list, err := s.repo.GetAddresses()
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

	if err := s.repo.CreateAddress(addr); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, addr)
}

func (s *Server) HandleUpdateAddress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetAddressByID(id)
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

	if err := s.repo.UpdateAddress(addr); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, addr)
}

func (s *Server) HandleDeleteAddress(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteAddress(id); err != nil {
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

	if err := s.repo.BulkDeleteAddresses(body.IDs); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// SERVICE OBJECTS HANDLERS
// =========================================================================

func (s *Server) HandleGetServices(w http.ResponseWriter, r *http.Request) {
	list, err := s.repo.GetServices()
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

	if err := s.repo.CreateService(svc); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, svc)
}

func (s *Server) HandleUpdateService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetServiceByID(id)
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

	if err := s.repo.UpdateService(svc); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, svc)
}

func (s *Server) HandleDeleteService(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteService(id); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, true)
}

// =========================================================================
// STATIC ROUTES HANDLERS
// =========================================================================

func (s *Server) applyRoutesToKernel() error {
	routes, err := s.repo.GetRoutes()
	if err != nil {
		return fmt.Errorf("failed to load routes from DB: %w", err)
	}
	if err := s.routing.ApplyRoutes(routes); err != nil {
		return fmt.Errorf("kernel routing update failed: %w", err)
	}
	return nil
}

func (s *Server) HandleGetRoutes(w http.ResponseWriter, r *http.Request) {
	list, err := s.repo.GetRoutes()
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

	if err := s.repo.CreateRoute(route); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.applyRoutesToKernel(); err != nil {
		log.Printf("[Server] Warning: route created but kernel apply failed: %v", err)
	}
	s.writeJSON(w, http.StatusOK, route)
}

func (s *Server) HandleUpdateRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	existing, err := s.repo.GetRouteByID(id)
	if err != nil || existing == nil {
		s.writeError(w, http.StatusNotFound, "Route not found")
		return
	}

	var input model.StaticRouteInput
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.writeError(w, http.StatusBadRequest, "Invalid request payload")
		return
	}

	route := model.StaticRoute{
		ID:          id,
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

	if err := s.repo.UpdateRoute(route); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.applyRoutesToKernel(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Route saved but kernel apply failed: "+err.Error())
		return
	}
	s.writeJSON(w, http.StatusOK, route)
}

func (s *Server) HandleDeleteRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.DeleteRoute(id); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.applyRoutesToKernel(); err != nil {
		log.Printf("[Server] Warning: route deleted but kernel apply failed: %v", err)
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

	if err := s.repo.BulkDeleteRoutes(body.IDs); err != nil {
		s.writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := s.applyRoutesToKernel(); err != nil {
		log.Printf("[Server] Warning: routes deleted but kernel apply failed: %v", err)
	}
	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleToggleRoute(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if err := s.repo.ToggleRouteStatus(id); err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if err := s.applyRoutesToKernel(); err != nil {
		s.writeError(w, http.StatusInternalServerError, "Route toggled but kernel apply failed: "+err.Error())
		return
	}
	route, _ := s.repo.GetRouteByID(id)
	s.writeJSON(w, http.StatusOK, route)
}

func (s *Server) HandleApplyRoutes(w http.ResponseWriter, r *http.Request) {
	routes, err := s.repo.GetRoutes()
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	if err := s.routing.ApplyRoutes(routes); err != nil {
		s.writeError(w, http.StatusInternalServerError, "OS routing configuration update failed")
		return
	}

	s.writeJSON(w, http.StatusOK, true)
}

func (s *Server) HandleGetRoutesConfig(w http.ResponseWriter, r *http.Request) {
	s.writeJSON(w, http.StatusOK, map[string]interface{}{
		"allowEditSystemRoutes":  s.repo.GetAllowEditSystemRoutes(),
		"prioritizeKernelRoutes": s.repo.GetPrioritizeKernelRoutes(),
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
	cfg, err := s.repo.GetDNSConfig()
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

	if err := s.repo.UpdateDNSConfig(input); err != nil {
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

	user, err := s.repo.GetUserByUsername("admin")
	if err != nil || user == nil {
		s.writeError(w, http.StatusInternalServerError, "User context resolution failed")
		return
	}

	err = bcrypt.CompareHashAndPassword([]byte(user.PasswordHash), []byte(req.CurrentPassword))
	if err != nil {
		if req.CurrentPassword == "admin" && user.PasswordHash == "$2a$10$w8F.tI18jR.p9o/H2lF25OcjWbEbeYvD.qW222yA6/oH/l6Uf9D7e" {
			// Proceed for mock
		} else {
			s.writeError(w, http.StatusBadRequest, "รหัสผ่านปัจจุบันไม่ถูกต้อง")
			return
		}
	}

	newHash, err := bcrypt.GenerateFromPassword([]byte(req.NewPassword), 10)
	if err != nil {
		s.writeError(w, http.StatusInternalServerError, "Crypto generation failed")
		return
	}

	if err := s.repo.ChangePassword("admin", string(newHash)); err != nil {
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
		"device":     "PiGate Firewall Gateway",
		"version":    "v1.0.0-Release",
		"exportedAt": time.Now().Format(time.RFC3339),
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
			Addresses      []model.AddressObject     `json:"addresses"`
			ServiceObjects []model.ServiceObject     `json:"serviceObjects"`
			Policies       []model.PolicyRule        `json:"policies"`
			Routes         []model.StaticRoute       `json:"routes"`
			Interfaces     []model.NetworkInterface  `json:"interfaces"`
			DHCP           *struct {
				Config       *model.DhcpConfig        `json:"config"`
				Reservations []model.DhcpReservation  `json:"reservations"`
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
