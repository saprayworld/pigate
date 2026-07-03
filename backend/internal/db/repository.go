package db

import (
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"pigate/internal/model"

	"github.com/vishvananda/netlink"
)

type Repository struct {
	db                     *sql.DB
	mockMode               bool
	mockFromReal           bool
	allowEditSystemRoutes  bool
	prioritizeKernelRoutes bool
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{
		db:                     db,
		mockMode:               true, // default to true for safety
		mockFromReal:           false,
		allowEditSystemRoutes:  false,
		prioritizeKernelRoutes: false, // default to false
	}
}

func (r *Repository) SetMockMode(mockMode bool, mockFromReal bool) {
	r.mockMode = mockMode
	r.mockFromReal = mockFromReal
}

func (r *Repository) IsMockMode() bool {
	return r.mockMode
}

func (r *Repository) IsMockFromReal() bool {
	return r.mockFromReal
}

func (r *Repository) SetAllowEditSystemRoutes(allow bool) {
	r.allowEditSystemRoutes = allow
}

func (r *Repository) GetAllowEditSystemRoutes() bool {
	return r.allowEditSystemRoutes
}

func (r *Repository) SetPrioritizeKernelRoutes(prioritize bool) {
	r.prioritizeKernelRoutes = prioritize
}

func (r *Repository) GetPrioritizeKernelRoutes() bool {
	return r.prioritizeKernelRoutes
}

// =========================================================================
// USER AUTHENTICATION
// =========================================================================

func (r *Repository) GetUserByUsername(username string) (*model.User, error) {
	row := r.db.QueryRow("SELECT id, username, password_hash, is_initial, created_at FROM users WHERE username = ?", username)
	var u model.User
	var isInitInt int
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &isInitInt, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.IsInitial = isInitInt == 1
	return &u, nil
}

func (r *Repository) ChangePassword(username string, newPasswordHash string) error {
	_, err := r.db.Exec("UPDATE users SET password_hash = ?, is_initial = 0 WHERE username = ?", newPasswordHash, username)
	return err
}

// =========================================================================
// ADDRESS OBJECTS
// =========================================================================

func (r *Repository) GetAddresses() ([]model.AddressObject, error) {
	rows, err := r.db.Query("SELECT id, name, type, value, system FROM address_objects")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.AddressObject{}
	for rows.Next() {
		var addr model.AddressObject
		var sysInt int
		if err := rows.Scan(&addr.ID, &addr.Name, &addr.Type, &addr.Value, &sysInt); err != nil {
			return nil, err
		}
		addr.System = sysInt == 1
		addr.RefPolicies = []string{} // default empty array

		// Query referenced policy names
		refRows, err := r.db.Query(`
			SELECT DISTINCT fp.name 
			FROM policy_addresses pa 
			JOIN firewall_policies fp ON pa.policy_id = fp.id 
			WHERE pa.address_id = ?`, addr.ID)
		if err == nil {
			for refRows.Next() {
				var pName string
				if err := refRows.Scan(&pName); err == nil {
					addr.RefPolicies = append(addr.RefPolicies, pName)
				}
			}
			refRows.Close()
		}

		list = append(list, addr)
	}
	return list, nil
}

func (r *Repository) GetAddressByID(id string) (*model.AddressObject, error) {
	row := r.db.QueryRow("SELECT id, name, type, value, system FROM address_objects WHERE id = ?", id)
	var addr model.AddressObject
	var sysInt int
	err := row.Scan(&addr.ID, &addr.Name, &addr.Type, &addr.Value, &sysInt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	addr.System = sysInt == 1
	addr.RefPolicies = []string{}

	refRows, err := r.db.Query(`
		SELECT DISTINCT fp.name 
		FROM policy_addresses pa 
		JOIN firewall_policies fp ON pa.policy_id = fp.id 
		WHERE pa.address_id = ?`, addr.ID)
	if err == nil {
		defer refRows.Close()
		for refRows.Next() {
			var pName string
			if err := refRows.Scan(&pName); err == nil {
				addr.RefPolicies = append(addr.RefPolicies, pName)
			}
		}
	}

	return &addr, nil
}

func isValidFQDN(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

func (r *Repository) validateAddressObject(addr model.AddressObject) error {
	if len(strings.TrimSpace(addr.Name)) == 0 {
		return errors.New("address object name cannot be empty")
	}
	if addr.Type != "subnet" && addr.Type != "range" && addr.Type != "fqdn" {
		return fmt.Errorf("invalid address object type: %s", addr.Type)
	}

	switch addr.Type {
	case "subnet":
		_, _, err := net.ParseCIDR(addr.Value)
		if err != nil {
			return fmt.Errorf("invalid subnet value %q: %w", addr.Value, err)
		}
	case "range":
		parts := strings.Split(addr.Value, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid IP range value %q: must be in format START-END", addr.Value)
		}
		ipStartStr := strings.TrimSpace(parts[0])
		ipEndStr := strings.TrimSpace(parts[1])
		ipStart := net.ParseIP(ipStartStr)
		ipEnd := net.ParseIP(ipEndStr)
		if ipStart == nil {
			return fmt.Errorf("invalid start IP %q in range %q", ipStartStr, addr.Value)
		}
		if ipEnd == nil {
			return fmt.Errorf("invalid end IP %q in range %q", ipEndStr, addr.Value)
		}
		if (ipStart.To4() != nil) != (ipEnd.To4() != nil) {
			return fmt.Errorf("IP range family mismatch: %s and %s must be of same IP version", ipStartStr, ipEndStr)
		}
	case "fqdn":
		if !isValidFQDN(addr.Value) {
			return fmt.Errorf("invalid FQDN value %q", addr.Value)
		}
	}
	return nil
}

func (r *Repository) CreateAddress(addr model.AddressObject) error {
	if err := r.validateAddressObject(addr); err != nil {
		return err
	}
	sysVal := 0
	if addr.System {
		sysVal = 1
	}
	_, err := r.db.Exec("INSERT INTO address_objects (id, name, type, value, system) VALUES (?, ?, ?, ?, ?)",
		addr.ID, addr.Name, addr.Type, addr.Value, sysVal)
	return err
}

func (r *Repository) UpdateAddress(addr model.AddressObject) error {
	if err := r.validateAddressObject(addr); err != nil {
		return err
	}
	// Check system lock
	var system int
	err := r.db.QueryRow("SELECT system FROM address_objects WHERE id = ?", addr.ID).Scan(&system)
	if err != nil {
		return err
	}
	if system == 1 {
		return errors.New("cannot update system predefined address objects")
	}

	_, err = r.db.Exec("UPDATE address_objects SET name = ?, type = ?, value = ? WHERE id = ?",
		addr.Name, addr.Type, addr.Value, addr.ID)
	return err
}

func (r *Repository) DeleteAddress(id string) error {
	// Check system and references
	var system int
	err := r.db.QueryRow("SELECT system FROM address_objects WHERE id = ?", id).Scan(&system)
	if err != nil {
		return err
	}
	if system == 1 {
		return errors.New("cannot delete system predefined address objects")
	}

	var refCount int
	err = r.db.QueryRow("SELECT COUNT(*) FROM policy_addresses WHERE address_id = ?", id).Scan(&refCount)
	if err != nil {
		return err
	}
	if refCount > 0 {
		return errors.New("cannot delete address object referenced by firewall policies")
	}

	_, err = r.db.Exec("DELETE FROM address_objects WHERE id = ?", id)
	return err
}

func (r *Repository) BulkDeleteAddresses(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	// Build query placeholders
	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	queryIn := strings.Join(placeholders, ",")

	// Check if any is system
	var systemCount int
	querySys := fmt.Sprintf("SELECT COUNT(*) FROM address_objects WHERE system = 1 AND id IN (%s)", queryIn)
	if err := r.db.QueryRow(querySys, args...).Scan(&systemCount); err != nil {
		return err
	}
	if systemCount > 0 {
		return errors.New("cannot delete system predefined address objects in bulk")
	}

	// Check if any is referenced
	var refCount int
	queryRefs := fmt.Sprintf("SELECT COUNT(*) FROM policy_addresses WHERE address_id IN (%s)", queryIn)
	if err := r.db.QueryRow(queryRefs, args...).Scan(&refCount); err != nil {
		return err
	}
	if refCount > 0 {
		return errors.New("cannot delete address objects referenced by firewall policies")
	}

	queryDel := fmt.Sprintf("DELETE FROM address_objects WHERE id IN (%s)", queryIn)
	_, err := r.db.Exec(queryDel, args...)
	return err
}

func (r *Repository) AddressNameExists(name string) (bool, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM address_objects WHERE name = ?", name).Scan(&count)
	return count > 0, err
}

// =========================================================================
// SERVICE OBJECTS
// =========================================================================

func (r *Repository) GetServices() ([]model.ServiceObject, error) {
	rows, err := r.db.Query("SELECT id, name, protocol, port, type FROM service_objects")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.ServiceObject{}
	for rows.Next() {
		var svc model.ServiceObject
		if err := rows.Scan(&svc.ID, &svc.Name, &svc.Protocol, &svc.Port, &svc.Type); err != nil {
			return nil, err
		}
		svc.RefPolicies = []string{}

		// Query referenced policy names
		refRows, err := r.db.Query(`
			SELECT DISTINCT fp.name 
			FROM policy_services ps 
			JOIN firewall_policies fp ON ps.policy_id = fp.id 
			WHERE ps.service_id = ?`, svc.ID)
		if err == nil {
			for refRows.Next() {
				var pName string
				if err := refRows.Scan(&pName); err == nil {
					svc.RefPolicies = append(svc.RefPolicies, pName)
				}
			}
			refRows.Close()
		}

		list = append(list, svc)
	}
	return list, nil
}

func (r *Repository) GetServiceByID(id string) (*model.ServiceObject, error) {
	row := r.db.QueryRow("SELECT id, name, protocol, port, type FROM service_objects WHERE id = ?", id)
	var svc model.ServiceObject
	err := row.Scan(&svc.ID, &svc.Name, &svc.Protocol, &svc.Port, &svc.Type)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	svc.RefPolicies = []string{}

	refRows, err := r.db.Query(`
		SELECT DISTINCT fp.name 
		FROM policy_services ps 
		JOIN firewall_policies fp ON ps.policy_id = fp.id 
		WHERE ps.service_id = ?`, svc.ID)
	if err == nil {
		defer refRows.Close()
		for refRows.Next() {
			var pName string
			if err := refRows.Scan(&pName); err == nil {
				svc.RefPolicies = append(svc.RefPolicies, pName)
			}
		}
	}

	return &svc, nil
}

func isValidPort(pStr string) bool {
	pStr = strings.TrimSpace(pStr)
	port, err := strconv.Atoi(pStr)
	if err != nil {
		return false
	}
	return port >= 1 && port <= 65535
}

func (r *Repository) validateServiceObject(svc model.ServiceObject) error {
	if len(strings.TrimSpace(svc.Name)) == 0 {
		return errors.New("service object name cannot be empty")
	}
	if svc.Protocol != "TCP" && svc.Protocol != "UDP" && svc.Protocol != "TCP/UDP" && svc.Protocol != "ICMP" {
		return fmt.Errorf("invalid protocol: %s", svc.Protocol)
	}

	portStr := strings.TrimSpace(svc.Port)
	if svc.Protocol == "ICMP" {
		if portStr != "-" {
			return fmt.Errorf("ICMP service port must be '-'")
		}
		return nil
	}

	// Non-ICMP protocols require valid numeric ports or port ranges
	parts := strings.Split(portStr, "-")
	if len(parts) == 1 {
		if !isValidPort(parts[0]) {
			return fmt.Errorf("invalid port %q: must be a number between 1 and 65535", parts[0])
		}
	} else if len(parts) == 2 {
		startStr := strings.TrimSpace(parts[0])
		endStr := strings.TrimSpace(parts[1])
		if !isValidPort(startStr) {
			return fmt.Errorf("invalid start port %q in range %q: must be a number between 1 and 65535", startStr, portStr)
		}
		if !isValidPort(endStr) {
			return fmt.Errorf("invalid end port %q in range %q: must be a number between 1 and 65535", endStr, portStr)
		}
		start, _ := strconv.Atoi(startStr)
		end, _ := strconv.Atoi(endStr)
		if start > end {
			return fmt.Errorf("invalid port range %q: start port %d cannot be greater than end port %d", portStr, start, end)
		}
	} else {
		return fmt.Errorf("invalid port format %q: must be a single port or range (e.g. 80 or 80-88)", portStr)
	}

	return nil
}

func (r *Repository) CreateService(svc model.ServiceObject) error {
	if err := r.validateServiceObject(svc); err != nil {
		return err
	}
	_, err := r.db.Exec("INSERT INTO service_objects (id, name, protocol, port, type) VALUES (?, ?, ?, ?, ?)",
		svc.ID, svc.Name, svc.Protocol, svc.Port, svc.Type)
	return err
}

func (r *Repository) UpdateService(svc model.ServiceObject) error {
	if err := r.validateServiceObject(svc); err != nil {
		return err
	}
	var sType string
	err := r.db.QueryRow("SELECT type FROM service_objects WHERE id = ?", svc.ID).Scan(&sType)
	if err != nil {
		return err
	}
	if sType == "system" {
		return errors.New("cannot update system predefined service objects")
	}

	_, err = r.db.Exec("UPDATE service_objects SET name = ?, protocol = ?, port = ? WHERE id = ?",
		svc.Name, svc.Protocol, svc.Port, svc.ID)
	return err
}

func (r *Repository) DeleteService(id string) error {
	var sType string
	err := r.db.QueryRow("SELECT type FROM service_objects WHERE id = ?", id).Scan(&sType)
	if err != nil {
		return err
	}
	if sType == "system" {
		return errors.New("cannot delete system predefined service objects")
	}

	var refCount int
	err = r.db.QueryRow("SELECT COUNT(*) FROM policy_services WHERE service_id = ?", id).Scan(&refCount)
	if err != nil {
		return err
	}
	if refCount > 0 {
		return errors.New("cannot delete service object referenced by firewall policies")
	}

	_, err = r.db.Exec("DELETE FROM service_objects WHERE id = ?", id)
	return err
}

func (r *Repository) ServiceNameExists(name string) (bool, error) {
	var count int
	err := r.db.QueryRow("SELECT COUNT(*) FROM service_objects WHERE name = ?", name).Scan(&count)
	return count > 0, err
}

// =========================================================================
// FIREWALL POLICIES
// =========================================================================

func (r *Repository) GetPolicies() ([]model.PolicyRule, error) {
	rows, err := r.db.Query("SELECT id, name, in_interface, out_interface, action, log, status FROM firewall_policies ORDER BY priority ASC")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.PolicyRule{}
	for rows.Next() {
		var p model.PolicyRule
		var logInt, statInt int
		if err := rows.Scan(&p.ID, &p.Name, &p.InInterface, &p.OutInterface, &p.Action, &logInt, &statInt); err != nil {
			return nil, err
		}
		p.Log = logInt == 1
		p.Status = statInt == 1
		p.Source = []string{}
		p.Destination = []string{}
		p.Service = []string{}

		// Load Sources & Destinations
		addrRows, err := r.db.Query(`
			SELECT ao.name, pa.association_type 
			FROM policy_addresses pa 
			JOIN address_objects ao ON pa.address_id = ao.id 
			WHERE pa.policy_id = ?`, p.ID)
		if err == nil {
			for addrRows.Next() {
				var aName, assoc string
				if err := addrRows.Scan(&aName, &assoc); err == nil {
					if assoc == "SOURCE" {
						p.Source = append(p.Source, aName)
					} else {
						p.Destination = append(p.Destination, aName)
					}
				}
			}
			addrRows.Close()
		}

		// Load Services
		svcRows, err := r.db.Query(`
			SELECT so.name 
			FROM policy_services ps 
			JOIN service_objects so ON ps.service_id = so.id 
			WHERE ps.policy_id = ?`, p.ID)
		if err == nil {
			for svcRows.Next() {
				var sName string
				if err := svcRows.Scan(&sName); err == nil {
					p.Service = append(p.Service, sName)
				}
			}
			svcRows.Close()
		}

		list = append(list, p)
	}
	return list, nil
}

func (r *Repository) GetPolicyByID(id string) (*model.PolicyRule, error) {
	row := r.db.QueryRow("SELECT id, name, in_interface, out_interface, action, log, status FROM firewall_policies WHERE id = ?", id)
	var p model.PolicyRule
	var logInt, statInt int
	err := row.Scan(&p.ID, &p.Name, &p.InInterface, &p.OutInterface, &p.Action, &logInt, &statInt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	p.Log = logInt == 1
	p.Status = statInt == 1
	p.Source = []string{}
	p.Destination = []string{}
	p.Service = []string{}

	// Load Sources/Destinations
	addrRows, err := r.db.Query(`
		SELECT ao.name, pa.association_type 
		FROM policy_addresses pa 
		JOIN address_objects ao ON pa.address_id = ao.id 
		WHERE pa.policy_id = ?`, p.ID)
	if err == nil {
		defer addrRows.Close()
		for addrRows.Next() {
			var aName, assoc string
			if err := addrRows.Scan(&aName, &assoc); err == nil {
				if assoc == "SOURCE" {
					p.Source = append(p.Source, aName)
				} else {
					p.Destination = append(p.Destination, aName)
				}
			}
		}
	}

	// Load Services
	svcRows, err := r.db.Query(`
		SELECT so.name 
		FROM policy_services ps 
		JOIN service_objects so ON ps.service_id = so.id 
		WHERE ps.policy_id = ?`, p.ID)
	if err == nil {
		defer svcRows.Close()
		for svcRows.Next() {
			var sName string
			if err := svcRows.Scan(&sName); err == nil {
				p.Service = append(p.Service, sName)
			}
		}
	}

	return &p, nil
}

func (r *Repository) CreatePolicy(p model.PolicyRule) error {
	if len(strings.TrimSpace(p.Name)) == 0 {
		return errors.New("policy name cannot be empty")
	}
	if p.Action != "ACCEPT" && p.Action != "DROP" {
		return errors.New("policy action must be ACCEPT or DROP")
	}
	if len(p.Source) == 0 {
		return errors.New("policy must have at least one source address object")
	}
	if len(p.Destination) == 0 {
		return errors.New("policy must have at least one destination address object")
	}
	if len(p.Service) == 0 {
		return errors.New("policy must have at least one service object")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Get next priority value
	var maxPriority int
	err = tx.QueryRow("SELECT COALESCE(MAX(priority), 0) FROM firewall_policies").Scan(&maxPriority)
	if err != nil {
		return err
	}

	logVal := 0
	if p.Log {
		logVal = 1
	}
	statVal := 0
	if p.Status {
		statVal = 1
	}

	_, err = tx.Exec("INSERT INTO firewall_policies (id, name, in_interface, out_interface, action, log, status, priority) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		p.ID, p.Name, p.InInterface, p.OutInterface, p.Action, logVal, statVal, maxPriority+1)
	if err != nil {
		return err
	}

	// Insert association mappings
	if err := r.savePolicyRelations(tx, p.ID, p.Source, p.Destination, p.Service); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) UpdatePolicy(p model.PolicyRule) error {
	if len(strings.TrimSpace(p.Name)) == 0 {
		return errors.New("policy name cannot be empty")
	}
	if p.Action != "ACCEPT" && p.Action != "DROP" {
		return errors.New("policy action must be ACCEPT or DROP")
	}
	if len(p.Source) == 0 {
		return errors.New("policy must have at least one source address object")
	}
	if len(p.Destination) == 0 {
		return errors.New("policy must have at least one destination address object")
	}
	if len(p.Service) == 0 {
		return errors.New("policy must have at least one service object")
	}

	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	logVal := 0
	if p.Log {
		logVal = 1
	}
	statVal := 0
	if p.Status {
		statVal = 1
	}

	_, err = tx.Exec("UPDATE firewall_policies SET name = ?, in_interface = ?, out_interface = ?, action = ?, log = ?, status = ? WHERE id = ?",
		p.Name, p.InInterface, p.OutInterface, p.Action, logVal, statVal, p.ID)
	if err != nil {
		return err
	}

	// Delete old references
	_, err = tx.Exec("DELETE FROM policy_addresses WHERE policy_id = ?", p.ID)
	if err != nil {
		return err
	}
	_, err = tx.Exec("DELETE FROM policy_services WHERE policy_id = ?", p.ID)
	if err != nil {
		return err
	}

	// Reinsert mapping relations
	if err := r.savePolicyRelations(tx, p.ID, p.Source, p.Destination, p.Service); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) savePolicyRelations(tx *sql.Tx, policyID string, sources, destinations, services []string) error {
	// Source Mapping
	for _, srcName := range sources {
		var addrID string
		err := tx.QueryRow("SELECT id FROM address_objects WHERE name = ?", srcName).Scan(&addrID)
		if err != nil {
			return fmt.Errorf("source address object %q does not exist", srcName)
		}
		_, err = tx.Exec("INSERT INTO policy_addresses (policy_id, address_id, association_type) VALUES (?, ?, 'SOURCE')", policyID, addrID)
		if err != nil {
			return err
		}
	}

	// Destination Mapping
	for _, destName := range destinations {
		var addrID string
		err := tx.QueryRow("SELECT id FROM address_objects WHERE name = ?", destName).Scan(&addrID)
		if err != nil {
			return fmt.Errorf("destination address object %q does not exist", destName)
		}
		_, err = tx.Exec("INSERT INTO policy_addresses (policy_id, address_id, association_type) VALUES (?, ?, 'DESTINATION')", policyID, addrID)
		if err != nil {
			return err
		}
	}

	// Service Mapping
	for _, svcName := range services {
		var svcID string
		err := tx.QueryRow("SELECT id FROM service_objects WHERE name = ?", svcName).Scan(&svcID)
		if err != nil {
			// Try matching prefix name for e.g. "HTTP (TCP 80)"
			parts := strings.Split(svcName, " ")
			if len(parts) > 0 {
				err = tx.QueryRow("SELECT id FROM service_objects WHERE name = ?", parts[0]).Scan(&svcID)
			}
		}
		if err != nil || svcID == "" {
			return fmt.Errorf("service object %q does not exist", svcName)
		}
		_, err = tx.Exec("INSERT INTO policy_services (policy_id, service_id) VALUES (?, ?)", policyID, svcID)
		if err != nil {
			return err
		}
	}

	return nil
}

func (r *Repository) DeletePolicy(id string) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete related references first
	_, _ = tx.Exec("DELETE FROM policy_addresses WHERE policy_id = ?", id)
	_, _ = tx.Exec("DELETE FROM policy_services WHERE policy_id = ?", id)

	_, err = tx.Exec("DELETE FROM firewall_policies WHERE id = ?", id)
	if err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) SaveAllPolicies(policies []model.PolicyRule) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	for idx, p := range policies {
		_, err := tx.Exec("UPDATE firewall_policies SET priority = ? WHERE id = ?", idx+1, p.ID)
		if err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (r *Repository) TogglePolicyLog(id string) error {
	_, err := r.db.Exec("UPDATE firewall_policies SET log = NOT log WHERE id = ?", id)
	return err
}

func (r *Repository) TogglePolicyStatus(id string) error {
	_, err := r.db.Exec("UPDATE firewall_policies SET status = NOT status WHERE id = ?", id)
	return err
}

// =========================================================================
// STATIC ROUTES
// =========================================================================

func (r *Repository) GetDatabaseRoutes() ([]model.StaticRoute, error) {
	rows, err := r.db.Query("SELECT id, destination, gateway, interface, metric, description, status, type, scope, src, proto FROM static_routes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var dbRoutes []model.StaticRoute
	for rows.Next() {
		var rt model.StaticRoute
		var statInt int
		if err := rows.Scan(&rt.ID, &rt.Destination, &rt.Gateway, &rt.Interface, &rt.Metric, &rt.Description, &statInt, &rt.Type, &rt.Scope, &rt.Src, &rt.Proto); err != nil {
			return nil, err
		}
		rt.Status = statInt == 1

		// Resolve "default" gateway to the active default gateway IP
		if rt.Gateway == "default" {
			rt.Gateway = r.GetDefaultGatewayIP(rt.Interface)
			if rt.Gateway == "" {
				rt.Gateway = r.GetDefaultGatewayIP("")
			}
		}

		if rt.Gateway == "" {
			rt.Type = "custom"
		} else {
			rt.Type = "customgateway"
		}

		dbRoutes = append(dbRoutes, rt)
	}
	return dbRoutes, nil
}

func (r *Repository) GetRoutes() ([]model.StaticRoute, error) {
	dbRoutes, err := r.GetDatabaseRoutes()
	if err != nil {
		return nil, err
	}

	// Fetch routes from kernel
	kernelRoutes, err := r.GetKernelRoutes()
	if err != nil {
		log.Printf("[Routing] Warning: Failed to fetch kernel routes in GetRoutes: %v", err)
	}

	// Helper to get canonical CIDR representation
	canonicalCIDR := func(s string) string {
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return s
		}
		return ipNet.String()
	}

	type routeCompareKey struct {
		dest  string
		gw    string
		iface string
	}

	// Build map of kernel routes for fast lookup
	kMap := make(map[routeCompareKey]model.StaticRoute)
	for _, kr := range kernelRoutes {
		key := routeCompareKey{
			dest:  canonicalCIDR(kr.Destination),
			gw:    strings.TrimSpace(kr.Gateway),
			iface: strings.TrimSpace(kr.Interface),
		}
		kMap[key] = kr
	}

	var result []model.StaticRoute
	matchedKeys := make(map[routeCompareKey]bool)

	// Merge DB routes with kernel routes
	for _, dbRoute := range dbRoutes {
		key := routeCompareKey{
			dest:  canonicalCIDR(dbRoute.Destination),
			gw:    strings.TrimSpace(dbRoute.Gateway),
			iface: strings.TrimSpace(dbRoute.Interface),
		}

		if kr, found := kMap[key]; found {
			matchedKeys[key] = true
			if r.prioritizeKernelRoutes {
				// Prioritize kernel metric and properties,
				// but KEEP the DB status and DB type classification.
				dbRoute.Metric = kr.Metric
				dbRoute.Scope = kr.Scope
				dbRoute.Src = kr.Src
				dbRoute.Proto = kr.Proto
			}
		} else {
			// Not found in kernel — for system/defaultgateway types mark as inactive since they can't be active without being in kernel.
			// Custom/customgateway routes retain their DB status (user may want them applied later via Apply button).
			if r.prioritizeKernelRoutes && (dbRoute.Type == "system" || dbRoute.Type == "defaultgateway") {
				dbRoute.Status = false
			}
		}
		result = append(result, dbRoute)
	}

	// Add routes that exist only in kernel
	for _, kr := range kernelRoutes {
		key := routeCompareKey{
			dest:  canonicalCIDR(kr.Destination),
			gw:    strings.TrimSpace(kr.Gateway),
			iface: strings.TrimSpace(kr.Interface),
		}
		if !matchedKeys[key] {
			// Generate a canonical ID for it
			routeID := fmt.Sprintf("route-sys-%s-%s-%s",
				strings.ReplaceAll(canonicalCIDR(kr.Destination), "/", "_"),
				strings.ReplaceAll(kr.Gateway, ".", "_"),
				kr.Interface,
			)
			kr.ID = routeID
			result = append(result, kr)
		}
	}

	return result, nil
}

func (r *Repository) GetRouteByID(id string) (*model.StaticRoute, error) {
	row := r.db.QueryRow("SELECT id, destination, gateway, interface, metric, description, status, type, scope, src, proto FROM static_routes WHERE id = ?", id)
	var route model.StaticRoute
	var statInt int
	err := row.Scan(&route.ID, &route.Destination, &route.Gateway, &route.Interface, &route.Metric, &route.Description, &statInt, &route.Type, &route.Scope, &route.Src, &route.Proto)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	route.Status = statInt == 1

	// Resolve "default" gateway to the active default gateway IP
	if route.Gateway == "default" {
		route.Gateway = r.GetDefaultGatewayIP(route.Interface)
		if route.Gateway == "" {
			route.Gateway = r.GetDefaultGatewayIP("")
		}
	}

	if route.Gateway == "" {
		route.Type = "custom"
	} else {
		route.Type = "customgateway"
	}

	return &route, nil
}

func (r *Repository) CreateRoute(route model.StaticRoute) error {
	statVal := 0
	if route.Status {
		statVal = 1
	}
	if route.Scope == "" {
		route.Scope = "global"
	}
	if route.Proto == "" {
		route.Proto = "static"
	}
	_, err := r.db.Exec("INSERT INTO static_routes (id, destination, gateway, interface, metric, description, status, type, scope, src, proto) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
		route.ID, route.Destination, route.Gateway, route.Interface, route.Metric, route.Description, statVal, route.Type, route.Scope, route.Src, route.Proto)
	return err
}

func (r *Repository) UpdateRoute(route model.StaticRoute) error {
	var rType string
	err := r.db.QueryRow("SELECT type FROM static_routes WHERE id = ?", route.ID).Scan(&rType)
	if err != nil {
		return err
	}
	if rType == "system" && !r.allowEditSystemRoutes {
		return errors.New("cannot update system predefined static routes")
	}

	statVal := 0
	if route.Status {
		statVal = 1
	}
	if route.Scope == "" {
		route.Scope = "global"
	}
	if route.Proto == "" {
		route.Proto = "static"
	}
	_, err = r.db.Exec("UPDATE static_routes SET destination = ?, gateway = ?, interface = ?, metric = ?, description = ?, status = ?, type = ?, scope = ?, src = ?, proto = ? WHERE id = ?",
		route.Destination, route.Gateway, route.Interface, route.Metric, route.Description, statVal, route.Type, route.Scope, route.Src, route.Proto, route.ID)
	return err
}

func (r *Repository) DeleteRoute(id string) error {
	var rType string
	err := r.db.QueryRow("SELECT type FROM static_routes WHERE id = ?", id).Scan(&rType)
	if err != nil {
		return err
	}
	if rType == "system" && !r.allowEditSystemRoutes {
		return errors.New("cannot delete system predefined static routes")
	}

	_, err = r.db.Exec("DELETE FROM static_routes WHERE id = ?", id)
	return err
}

func (r *Repository) BulkDeleteRoutes(ids []string) error {
	if len(ids) == 0 {
		return nil
	}

	placeholders := make([]string, len(ids))
	args := make([]interface{}, len(ids))
	for i, id := range ids {
		placeholders[i] = "?"
		args[i] = id
	}
	queryIn := strings.Join(placeholders, ",")

	if !r.allowEditSystemRoutes {
		var systemCount int
		querySys := fmt.Sprintf("SELECT COUNT(*) FROM static_routes WHERE type = 'system' AND id IN (%s)", queryIn)
		if err := r.db.QueryRow(querySys, args...).Scan(&systemCount); err != nil {
			return err
		}
		if systemCount > 0 {
			return errors.New("cannot delete system predefined static routes in bulk")
		}
	}

	queryDel := fmt.Sprintf("DELETE FROM static_routes WHERE id IN (%s)", queryIn)
	_, err := r.db.Exec(queryDel, args...)
	return err
}

func (r *Repository) ToggleRouteStatus(id string) error {
	var rType string
	err := r.db.QueryRow("SELECT type FROM static_routes WHERE id = ?", id).Scan(&rType)
	if err != nil {
		return err
	}
	// Only block toggling of 'system' routes (directly connected) when allowEditSystemRoutes is false.
	// 'defaultgateway' and 'custom' routes are always toggleable.
	if rType == "system" && !r.allowEditSystemRoutes {
		return errors.New("cannot toggle status of system predefined static routes")
	}

	_, err = r.db.Exec("UPDATE static_routes SET status = NOT status WHERE id = ?", id)
	return err
}

// =========================================================================
// DHCP SERVER
// =========================================================================

func (r *Repository) GetDHCPConfig() (*model.DhcpConfig, error) {
	row := r.db.QueryRow("SELECT id, enabled, interface, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time FROM dhcp_configs LIMIT 1")
	var cfg model.DhcpConfig
	var enabledInt int
	err := row.Scan(&cfg.ID, &enabledInt, &cfg.Interface, &cfg.StartIP, &cfg.EndIP, &cfg.Gateway, &cfg.Netmask, &cfg.DNS1, &cfg.DNS2, &cfg.LeaseTime)
	if err != nil {
		return nil, err
	}
	cfg.Enabled = enabledInt == 1
	return &cfg, nil
}

func (r *Repository) UpdateDHCPConfig(cfg model.DhcpConfig) error {
	enabledVal := 0
	if cfg.Enabled {
		enabledVal = 1
	}
	_, err := r.db.Exec(`UPDATE dhcp_configs SET 
		enabled = ?, interface = ?, start_ip = ?, end_ip = ?, gateway = ?, netmask = ?, dns1 = ?, dns2 = ?, lease_time = ? 
		WHERE id = 'dhcp-cfg-default' OR id = ?`,
		enabledVal, cfg.Interface, cfg.StartIP, cfg.EndIP, cfg.Gateway, cfg.Netmask, cfg.DNS1, cfg.DNS2, cfg.LeaseTime, cfg.ID)
	return err
}

func (r *Repository) GetDHCPConfigs() ([]model.DhcpConfig, error) {
	rows, err := r.db.Query("SELECT id, enabled, interface, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time FROM dhcp_configs")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.DhcpConfig
	for rows.Next() {
		var cfg model.DhcpConfig
		var enabledInt int
		err := rows.Scan(&cfg.ID, &enabledInt, &cfg.Interface, &cfg.StartIP, &cfg.EndIP, &cfg.Gateway, &cfg.Netmask, &cfg.DNS1, &cfg.DNS2, &cfg.LeaseTime)
		if err != nil {
			return nil, err
		}
		cfg.Enabled = enabledInt == 1
		list = append(list, cfg)
	}
	return list, nil
}

func (r *Repository) GetDHCPConfigByInterface(iface string) (*model.DhcpConfig, error) {
	row := r.db.QueryRow("SELECT id, enabled, interface, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time FROM dhcp_configs WHERE interface = ?", iface)
	var cfg model.DhcpConfig
	var enabledInt int
	err := row.Scan(&cfg.ID, &enabledInt, &cfg.Interface, &cfg.StartIP, &cfg.EndIP, &cfg.Gateway, &cfg.Netmask, &cfg.DNS1, &cfg.DNS2, &cfg.LeaseTime)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	cfg.Enabled = enabledInt == 1
	return &cfg, nil
}

func (r *Repository) CreateDHCPConfig(cfg model.DhcpConfig) error {
	enabledVal := 0
	if cfg.Enabled {
		enabledVal = 1
	}
	if cfg.ID == "" {
		cfg.ID = fmt.Sprintf("dhcp-cfg-%s", cfg.Interface)
	}
	_, err := r.db.Exec(`INSERT INTO dhcp_configs 
		(id, interface, enabled, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time) 
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		cfg.ID, cfg.Interface, enabledVal, cfg.StartIP, cfg.EndIP, cfg.Gateway, cfg.Netmask, cfg.DNS1, cfg.DNS2, cfg.LeaseTime)
	return err
}

func (r *Repository) UpdateDHCPConfigByID(cfg model.DhcpConfig) error {
	enabledVal := 0
	if cfg.Enabled {
		enabledVal = 1
	}
	_, err := r.db.Exec(`UPDATE dhcp_configs SET 
		interface = ?, enabled = ?, start_ip = ?, end_ip = ?, gateway = ?, netmask = ?, dns1 = ?, dns2 = ?, lease_time = ?, updated_at = CURRENT_TIMESTAMP 
		WHERE id = ?`,
		cfg.Interface, enabledVal, cfg.StartIP, cfg.EndIP, cfg.Gateway, cfg.Netmask, cfg.DNS1, cfg.DNS2, cfg.LeaseTime, cfg.ID)
	return err
}

func (r *Repository) DeleteDHCPConfig(id string) error {
	_, err := r.db.Exec("DELETE FROM dhcp_configs WHERE id = ?", id)
	return err
}

func (r *Repository) ToggleDHCPConfig(id string) error {
	_, err := r.db.Exec("UPDATE dhcp_configs SET enabled = NOT enabled, updated_at = CURRENT_TIMESTAMP WHERE id = ?", id)
	return err
}

// DHCP Leases DB methods
func (r *Repository) UpsertDHCPLease(lease model.ActiveDhcpLease) error {
	_, err := r.db.Exec(`INSERT INTO dhcp_leases (mac_address, ip_address, hostname, interface, expires_at) 
		VALUES (?, ?, ?, ?, ?) 
		ON CONFLICT(mac_address) DO UPDATE SET 
		ip_address = excluded.ip_address, 
		hostname = excluded.hostname, 
		interface = excluded.interface, 
		expires_at = excluded.expires_at`,
		lease.MacAddress, lease.IPAddress, lease.Hostname, lease.Interface, lease.ExpiresAt)
	return err
}

func (r *Repository) DeleteDHCPLease(macAddress string) error {
	_, err := r.db.Exec("DELETE FROM dhcp_leases WHERE mac_address = ?", macAddress)
	return err
}

func (r *Repository) GetDHCPLeases() ([]model.ActiveDhcpLease, error) {
	rows, err := r.db.Query("SELECT mac_address, ip_address, hostname, interface, expires_at FROM dhcp_leases")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.ActiveDhcpLease
	for rows.Next() {
		var lease model.ActiveDhcpLease
		var expiresAt sql.NullTime
		var hostname, iface sql.NullString
		err := rows.Scan(&lease.MacAddress, &lease.IPAddress, &hostname, &iface, &expiresAt)
		if err != nil {
			return nil, err
		}
		lease.Hostname = hostname.String
		lease.Interface = iface.String
		if expiresAt.Valid {
			lease.ExpiresAt = expiresAt.Time.Format(time.RFC3339)
			// Compute ExpiresIn for backward compatibility UI
			duration := time.Until(expiresAt.Time)
			if duration > 0 {
				hours := int(duration.Hours())
				mins := int(duration.Minutes()) % 60
				lease.ExpiresIn = fmt.Sprintf("%d hours, %d mins", hours, mins)
			} else {
				lease.ExpiresIn = "Expired"
			}
		} else {
			lease.ExpiresIn = "Infinite"
		}
		lease.ID = fmt.Sprintf("lease-%s", lease.MacAddress)
		list = append(list, lease)
	}
	return list, nil
}

func (r *Repository) ClearDHCPLeases() error {
	_, err := r.db.Exec("DELETE FROM dhcp_leases")
	return err
}

func (r *Repository) GetDHCPReservations() ([]model.DhcpReservation, error) {
	rows, err := r.db.Query("SELECT id, device_name, mac_address, ip_address FROM dhcp_reservations")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.DhcpReservation{}
	for rows.Next() {
		var res model.DhcpReservation
		if err := rows.Scan(&res.ID, &res.DeviceName, &res.MacAddress, &res.IPAddress); err != nil {
			return nil, err
		}
		list = append(list, res)
	}
	return list, nil
}

func (r *Repository) GetDHCPReservationByID(id string) (*model.DhcpReservation, error) {
	row := r.db.QueryRow("SELECT id, device_name, mac_address, ip_address FROM dhcp_reservations WHERE id = ?", id)
	var res model.DhcpReservation
	err := row.Scan(&res.ID, &res.DeviceName, &res.MacAddress, &res.IPAddress)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &res, nil
}

func (r *Repository) CreateDHCPReservation(res model.DhcpReservation) error {
	_, err := r.db.Exec("INSERT INTO dhcp_reservations (id, device_name, mac_address, ip_address) VALUES (?, ?, ?, ?)",
		res.ID, res.DeviceName, res.MacAddress, res.IPAddress)
	return err
}

func (r *Repository) UpdateDHCPReservation(res model.DhcpReservation) error {
	_, err := r.db.Exec("UPDATE dhcp_reservations SET device_name = ?, mac_address = ?, ip_address = ? WHERE id = ?",
		res.DeviceName, res.MacAddress, res.IPAddress, res.ID)
	return err
}

func (r *Repository) DeleteDHCPReservation(id string) error {
	_, err := r.db.Exec("DELETE FROM dhcp_reservations WHERE id = ?", id)
	return err
}

// =========================================================================
// NETWORK INTERFACES
// =========================================================================

// DetectAddressingMode checks whether a network interface is configured via DHCP or static IP.
// It probes common DHCP lease/PID file locations used by dhclient, dhcpcd, and systemd-networkd.
func DetectAddressingMode(ifaceName string, ifaceIndex int) string {
	// Check dhclient PID files
	dhclientPaths := []string{
		fmt.Sprintf("/run/dhclient-%s.pid", ifaceName),
		fmt.Sprintf("/run/dhclient.%s.pid", ifaceName),
		fmt.Sprintf("/var/run/dhclient-%s.pid", ifaceName),
		fmt.Sprintf("/var/run/dhclient.%s.pid", ifaceName),
	}
	for _, p := range dhclientPaths {
		if _, err := os.Stat(p); err == nil {
			return "dhcp"
		}
	}

	// Check dhclient lease files
	dhclientLeaseFiles := []string{
		fmt.Sprintf("/var/lib/dhcp/dhclient.%s.leases", ifaceName),
		fmt.Sprintf("/var/lib/dhclient/dhclient.%s.leases", ifaceName),
	}
	for _, p := range dhclientLeaseFiles {
		if _, err := os.Stat(p); err == nil {
			return "dhcp"
		}
	}

	// Check dhcpcd lease files
	dhcpcdPaths := []string{
		fmt.Sprintf("/var/lib/dhcpcd/%s.lease", ifaceName),
		fmt.Sprintf("/var/lib/dhcpcd5/%s.lease", ifaceName),
	}
	for _, p := range dhcpcdPaths {
		if _, err := os.Stat(p); err == nil {
			return "dhcp"
		}
	}

	// Check systemd-networkd lease file (named by interface index)
	systemdLeasePath := fmt.Sprintf("/run/systemd/netif/leases/%d", ifaceIndex)
	if _, err := os.Stat(systemdLeasePath); err == nil {
		return "dhcp"
	}

	// Check NetworkManager DHCP lease (nm-dhcp-*)
	nmLeasePath := fmt.Sprintf("/var/lib/NetworkManager/dhclient-%s.conf", ifaceName)
	if _, err := os.Stat(nmLeasePath); err == nil {
		return "dhcp"
	}

	// Check /run/NetworkManager/dhcp directory for lease files
	nmRunLeases := []string{
		fmt.Sprintf("/run/NetworkManager/dhcp-%s.conf", ifaceName),
		fmt.Sprintf("/run/NetworkManager/dhclient-%s.conf", ifaceName),
	}
	for _, p := range nmRunLeases {
		if _, err := os.Stat(p); err == nil {
			return "dhcp"
		}
	}

	// Fall back to static
	return "static"
}

// GetGatewayForInterface reads /proc/net/route to find the default gateway for the given interface.
func GetGatewayForInterface(ifaceName string) string {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return ""
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		// fields[0]=Iface, fields[1]=Destination, fields[2]=Gateway
		if fields[0] != ifaceName {
			continue
		}
		// Default route has destination 00000000
		if fields[1] != "00000000" {
			continue
		}
		gwHex := fields[2]
		if len(gwHex) != 8 {
			continue
		}
		// Little-endian hex -> IP
		var b [4]byte
		for i := 0; i < 4; i++ {
			v, err := strconv.ParseUint(gwHex[i*2:i*2+2], 16, 8)
			if err != nil {
				break
			}
			b[i] = byte(v)
		}
		gwIP := fmt.Sprintf("%d.%d.%d.%d", b[3], b[2], b[1], b[0])
		if gwIP == "0.0.0.0" {
			return ""
		}
		return gwIP
	}
	return ""
}

// NOTE: DNS is managed globally via system_dns_settings (SyncDNSFromOS / DNS Settings page).
// Per-interface dns1/dns2 fields have been removed.

// GetInterfaceSpeed reads the link speed from /sys/class/net/<iface>/speed.
// Returns a human-readable string such as "1000 Mbps" or "unknown" if not available.
func GetInterfaceSpeed(ifaceName string) string {
	speedPath := fmt.Sprintf("/sys/class/net/%s/speed", ifaceName)
	data, err := os.ReadFile(speedPath)
	if err != nil {
		return "unknown"
	}
	speedStr := strings.TrimSpace(string(data))
	speedMbps, err := strconv.Atoi(speedStr)
	if err != nil || speedMbps <= 0 {
		return "unknown"
	}
	if speedMbps >= 1000 {
		return fmt.Sprintf("%d Gbps", speedMbps/1000)
	}
	return fmt.Sprintf("%d Mbps", speedMbps)
}

func GetDeviceType(ifaceName string) string {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		log.Printf("[DB] Failed to get link type for %s via netlink: %v", ifaceName, err)
		return "unknown"
	}
	return link.Type()
}

func (r *Repository) GetInterfaces() ([]model.NetworkInterface, error) {
	return r.GetInterfacesFromDB()
}

func (r *Repository) GetInterfacesFromDB() ([]model.NetworkInterface, error) {
	rows, err := r.db.Query(`SELECT 
		id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, metric, mac_address, admin_access, status, speed,
		mac_mode, real_mac_address, randomized_mac, laa_mac_address, randomize_on_reconnect,
		connected_ssid, wifi_password, wifi_security, failover_enabled, backup_ssid, backup_wifi_password, backup_wifi_security, ip_check_timeout, primary_max_retries, failover_cooldown
		FROM network_interfaces`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.NetworkInterface{}
	for rows.Next() {
		var iface model.NetworkInterface
		var adminAccessStr string
		var reconnectInt, failoverInt int
		err := rows.Scan(
			&iface.ID, &iface.Name, &iface.Alias, &iface.Role, &iface.Type, &iface.Subtype, &iface.AddressingMode,
			&iface.IP, &iface.Netmask, &iface.Gateway, &iface.Metric, &iface.MacAddress,
			&adminAccessStr, &iface.Status, &iface.Speed,
			&iface.MacMode, &iface.RealMacAddress, &iface.RandomizedMac, &iface.LaaMacAddress, &reconnectInt,
			&iface.WifiSSID, &iface.WifiPassword, &iface.WifiSecurity, &failoverInt, &iface.BackupSSID, &iface.BackupWifiPassword,
			&iface.BackupWifiSecurity, &iface.IPCheckTimeout, &iface.PrimaryMaxRetries, &iface.FailoverCooldown,
		)
		if err != nil {
			return nil, err
		}
		iface.AdminAccess = []string{}
		if adminAccessStr != "" {
			iface.AdminAccess = strings.Split(adminAccessStr, ",")
		}
		recon := reconnectInt == 1
		iface.RandomizeOnReconnect = &recon
		fo := failoverInt == 1
		iface.FailoverEnabled = &fo

		list = append(list, iface)
	}

	return list, nil
}

func (r *Repository) GetInterfaceByID(id string) (*model.NetworkInterface, error) {
	row := r.db.QueryRow(`SELECT 
		id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, metric, mac_address, admin_access, status, speed,
		mac_mode, real_mac_address, randomized_mac, laa_mac_address, randomize_on_reconnect,
		connected_ssid, wifi_password, wifi_security, failover_enabled, backup_ssid, backup_wifi_password, backup_wifi_security, ip_check_timeout, primary_max_retries, failover_cooldown
		FROM network_interfaces WHERE id = ?`, id)
	var iface model.NetworkInterface
	var adminAccessStr string
	var reconnectInt, failoverInt int
	err := row.Scan(
		&iface.ID, &iface.Name, &iface.Alias, &iface.Role, &iface.Type, &iface.Subtype, &iface.AddressingMode,
		&iface.IP, &iface.Netmask, &iface.Gateway, &iface.Metric, &iface.MacAddress,
		&adminAccessStr, &iface.Status, &iface.Speed,
		&iface.MacMode, &iface.RealMacAddress, &iface.RandomizedMac, &iface.LaaMacAddress, &reconnectInt,
		&iface.WifiSSID, &iface.WifiPassword, &iface.WifiSecurity, &failoverInt, &iface.BackupSSID, &iface.BackupWifiPassword,
		&iface.BackupWifiSecurity, &iface.IPCheckTimeout, &iface.PrimaryMaxRetries, &iface.FailoverCooldown,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	iface.AdminAccess = []string{}
	if adminAccessStr != "" {
		iface.AdminAccess = strings.Split(adminAccessStr, ",")
	}
	recon := reconnectInt == 1
	iface.RandomizeOnReconnect = &recon
	fo := failoverInt == 1
	iface.FailoverEnabled = &fo

	return &iface, nil
}

func (r *Repository) UpdateInterface(iface model.NetworkInterface) error {
	log.Printf("[DB] Updating interface: %v", iface)

	adminAccessStr := strings.Join(iface.AdminAccess, ",")
	reconInt := 0
	if iface.RandomizeOnReconnect != nil && *iface.RandomizeOnReconnect {
		reconInt = 1
	}
	foInt := 0
	if iface.FailoverEnabled != nil && *iface.FailoverEnabled {
		foInt = 1
	}

	res, err := r.db.Exec(`UPDATE network_interfaces SET
		alias = ?, role = ?, addressing_mode = ?, ip = ?, netmask = ?, gateway = ?, metric = ?, mac_address = ?, admin_access = ?, status = ?,
		mac_mode = ?, real_mac_address = ?, randomized_mac = ?, laa_mac_address = ?, randomize_on_reconnect = ?,
		connected_ssid = ?, wifi_password = ?, wifi_security = ?, failover_enabled = ?, backup_ssid = ?, backup_wifi_password = ?, backup_wifi_security = ?,
		ip_check_timeout = ?, primary_max_retries = ?, failover_cooldown = ?
		WHERE id = ?`,
		iface.Alias, iface.Role, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, iface.Metric, iface.MacAddress, adminAccessStr, iface.Status,
		iface.MacMode, iface.RealMacAddress, iface.RandomizedMac, iface.LaaMacAddress, reconInt,
		iface.WifiSSID, iface.WifiPassword, iface.WifiSecurity, foInt, iface.BackupSSID, iface.BackupWifiPassword, iface.BackupWifiSecurity,
		iface.IPCheckTimeout, iface.PrimaryMaxRetries, iface.FailoverCooldown, iface.ID)
	if err != nil {
		return err
	}

	rows, err := res.RowsAffected()
	if err == nil && rows == 0 {
		// Insert since it does not exist in the database
		_, err = r.db.Exec(`INSERT INTO network_interfaces (
			id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, metric, mac_address, admin_access, status, speed,
			mac_mode, real_mac_address, randomized_mac, laa_mac_address, randomize_on_reconnect,
			connected_ssid, wifi_password, wifi_security, failover_enabled, backup_ssid, backup_wifi_password, backup_wifi_security, ip_check_timeout, primary_max_retries, failover_cooldown
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			iface.ID, iface.Name, iface.Alias, iface.Role, iface.Type, iface.Subtype, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, iface.Metric, iface.MacAddress, adminAccessStr, iface.Status, iface.Speed,
			iface.MacMode, iface.RealMacAddress, iface.RandomizedMac, iface.LaaMacAddress, reconInt,
			iface.WifiSSID, iface.WifiPassword, iface.WifiSecurity, foInt, iface.BackupSSID, iface.BackupWifiPassword, iface.BackupWifiSecurity, iface.IPCheckTimeout, iface.PrimaryMaxRetries, iface.FailoverCooldown)
		return err
	}
	log.Printf("[DB] Interface updated successfully")
	return nil
}

func (r *Repository) ToggleInterfaceStatus(id string, status string) error {
	_, err := r.db.Exec("UPDATE network_interfaces SET status = ? WHERE id = ?", status, id)
	return err
}

// =========================================================================
// SYSTEM TIME SETTINGS
// =========================================================================

func (r *Repository) GetSystemTimeSettings() (*model.SystemTimeSettings, error) {
	row := r.db.QueryRow("SELECT timezone, ntp_sync, ntp_server FROM system_time_settings WHERE id = 1")
	var settings model.SystemTimeSettings
	var ntpInt int
	err := row.Scan(&settings.Timezone, &ntpInt, &settings.NTPServer)
	if err != nil {
		return nil, err
	}
	settings.NTPSync = ntpInt == 1
	return &settings, nil
}

func (r *Repository) UpdateSystemTimeSettings(settings model.SystemTimeSettings) error {
	ntpVal := 0
	if settings.NTPSync {
		ntpVal = 1
	}
	_, err := r.db.Exec("UPDATE system_time_settings SET timezone = ?, ntp_sync = ?, ntp_server = ? WHERE id = 1",
		settings.Timezone, ntpVal, settings.NTPServer)
	return err
}

// CreateInterfaceForTest inserts a network interface for testing purposes.
func (r *Repository) CreateInterfaceForTest(iface model.NetworkInterface) error {
	adminAccessStr := strings.Join(iface.AdminAccess, ",")
	reconInt := 0
	if iface.RandomizeOnReconnect != nil && *iface.RandomizeOnReconnect {
		reconInt = 1
	}
	foInt := 0
	if iface.FailoverEnabled != nil && *iface.FailoverEnabled {
		foInt = 1
	}
	_, err := r.db.Exec(`INSERT INTO network_interfaces (
		id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, metric, mac_address, admin_access, status, speed,
		mac_mode, real_mac_address, randomize_on_reconnect, failover_enabled
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		iface.ID, iface.Name, iface.Alias, iface.Role, iface.Type, iface.Subtype, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, iface.Metric, iface.MacAddress, adminAccessStr, iface.Status, iface.Speed,
		iface.MacMode, iface.RealMacAddress, reconInt, foInt)
	return err
}

func (r *Repository) DeleteInterface(id string) error {
	_, err := r.db.Exec("DELETE FROM network_interfaces WHERE id = ?", id)
	return err
}

// GetDNSConfig retrieves system-wide DNS settings
func (r *Repository) GetDNSConfig() (*model.DNSConfig, error) {
	row := r.db.QueryRow("SELECT mode, primary_dns, secondary_dns, local_domain FROM system_dns_settings WHERE id = 1")
	var cfg model.DNSConfig
	err := row.Scan(&cfg.Mode, &cfg.PrimaryDNS, &cfg.SecondaryDNS, &cfg.LocalDomain)
	if err != nil {
		return nil, err
	}

	// Populate dynamic DNS servers
	dynServers, err := r.GetDynamicDNSServers()
	if err == nil {
		cfg.DynamicDNS = dynServers
	} else {
		cfg.DynamicDNS = []model.DynamicDNSServer{}
	}

	return &cfg, nil
}

// UpdateDNSConfig updates the system-wide DNS configuration
func (r *Repository) UpdateDNSConfig(cfg model.DNSConfigInput) error {
	if cfg.Mode != "wan" && cfg.Mode != "static" {
		return fmt.Errorf("invalid mode: %s", cfg.Mode)
	}
	_, err := r.db.Exec("UPDATE system_dns_settings SET mode = ?, primary_dns = ?, secondary_dns = ?, local_domain = ? WHERE id = 1",
		cfg.Mode, cfg.PrimaryDNS, cfg.SecondaryDNS, cfg.LocalDomain)
	return err
}

// GetDynamicDNSServers finds WAN interfaces configured with DHCP and returns their DNS configurations
func (r *Repository) GetDynamicDNSServers() ([]model.DynamicDNSServer, error) {
	rows, err := r.db.Query("SELECT name, alias, ip FROM network_interfaces WHERE role = 'WAN' AND addressing_mode = 'dhcp'")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var list []model.DynamicDNSServer
	for rows.Next() {
		var name, alias, ip string
		if err := rows.Scan(&name, &alias, &ip); err == nil {
			dnsList := []string{"192.168.0.1", "8.8.4.4"}
			if name == "eth0" {
				dnsList = []string{"172.20.160.1", "8.8.8.8"}
			}
			list = append(list, model.DynamicDNSServer{
				InterfaceName:  name,
				InterfaceAlias: alias,
				DNSServers:     dnsList,
			})
		}
	}
	return list, nil
}

// SyncRoutesFromOS fetches static routes from /proc/net/route and updates the database.
func (r *Repository) parseProcNetRoute() ([]model.StaticRoute, error) {
	data, err := os.ReadFile("/proc/net/route")
	if err != nil {
		return nil, fmt.Errorf("failed to read /proc/net/route: %w", err)
	}

	lines := strings.Split(string(data), "\n")
	if len(lines) <= 1 {
		return nil, nil
	}

	// Helper to get canonical CIDR representation
	canonicalCIDR := func(s string) string {
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return s
		}
		return ipNet.String()
	}

	var list []model.StaticRoute
	for _, line := range lines[1:] {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 8 {
			continue
		}

		iface := fields[0]
		destHex := fields[1]
		gwHex := fields[2]
		metricStr := fields[6]
		maskHex := fields[7]

		destIP, err := parseHexIP(destHex)
		if err != nil {
			continue
		}

		gwIP, err := parseHexIP(gwHex)
		if err != nil {
			continue
		}

		maskIPStr, err := parseHexIP(maskHex)
		if err != nil {
			continue
		}

		prefixLen := 32
		maskIP := net.ParseIP(maskIPStr)
		if maskIP != nil {
			mask := net.IPMask(maskIP.To4())
			ones, _ := mask.Size()
			prefixLen = ones
		}

		metric, _ := strconv.Atoi(metricStr)
		destination := fmt.Sprintf("%s/%d", destIP, prefixLen)
		gateway := gwIP
		if gateway == "0.0.0.0" {
			gateway = ""
		}

		routeType := "system"
		if destination == "0.0.0.0/0" {
			routeType = "defaultgateway"
		}

		routeID := fmt.Sprintf("route-sys-%s-%s-%s",
			strings.ReplaceAll(canonicalCIDR(destination), "/", "_"),
			strings.ReplaceAll(gateway, ".", "_"),
			iface,
		)

		routeScope := "global"
		routeProto := "static"
		if gateway == "" {
			routeScope = "link"
			routeProto = "kernel"
		} else if destination == "0.0.0.0/0" {
			routeProto = "boot"
		}

		list = append(list, model.StaticRoute{
			ID:          routeID,
			Destination: destination,
			Gateway:     gateway,
			Interface:   iface,
			Metric:      metric,
			Description: "System route fetched from kernel",
			Status:      true,
			Type:        routeType,
			Scope:       routeScope,
			Src:         "",
			Proto:       routeProto,
		})
	}
	return list, nil
}

func (r *Repository) GetKernelRoutes() ([]model.StaticRoute, error) {
	if r.mockMode {
		return []model.StaticRoute{
			{ID: "route-sys-0_0_0_0_0-10_0_0_1-wlan0", Destination: "0.0.0.0/0", Gateway: "10.0.0.1", Interface: "wlan0", Metric: 100, Description: "System route fetched from kernel", Status: true, Type: "defaultgateway", Scope: "global", Src: "", Proto: "boot"},
			{ID: "route-sys-192_168_1_0_24--eth0", Destination: "192.168.1.0/24", Gateway: "", Interface: "eth0", Metric: 0, Description: "System route fetched from kernel", Status: true, Type: "system", Scope: "link", Src: "192.168.1.1", Proto: "kernel"},
			{ID: "route-sys-10_0_0_0_24--wlan0", Destination: "10.0.0.0/24", Gateway: "", Interface: "wlan0", Metric: 0, Description: "System route fetched from kernel", Status: true, Type: "system", Scope: "link", Src: "10.0.0.45", Proto: "kernel"},
		}, nil
	}
	if runtime.GOOS != "linux" {
		return nil, nil
	}
	return r.parseProcNetRoute()
}

// GetDefaultGatewayIP finds the active default gateway IP from the kernel routes.
func (r *Repository) GetDefaultGatewayIP(iface string) string {
	kr, err := r.GetKernelRoutes()
	if err != nil {
		return ""
	}
	for _, route := range kr {
		if route.Destination == "0.0.0.0/0" && (iface == "" || route.Interface == iface) {
			return route.Gateway
		}
	}
	return ""
}

// SyncRoutesFromOS fetches static routes from /proc/net/route and updates the database.
func (r *Repository) SyncRoutesFromOS() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	kernelRoutes, err := r.parseProcNetRoute()
	if err != nil {
		return err
	}

	// Helper to get canonical CIDR representation
	canonicalCIDR := func(s string) string {
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return s
		}
		return ipNet.String()
	}

	type routeKey struct {
		dest  string
		gw    string
		iface string
	}

	// 1. Fetch custom routes from DB to reconcile their status and prevent duplicates
	rowsCustom, err := r.db.Query("SELECT id, destination, gateway, interface, status FROM static_routes WHERE type IN ('custom', 'customgateway')")
	if err != nil {
		return fmt.Errorf("failed to fetch custom routes: %w", err)
	}
	customRoutes := make(map[routeKey]*model.StaticRoute)
	for rowsCustom.Next() {
		var rt model.StaticRoute
		var statVal int
		if err := rowsCustom.Scan(&rt.ID, &rt.Destination, &rt.Gateway, &rt.Interface, &statVal); err == nil {
			rt.Status = statVal == 1

			// Resolve gateway IP if placeholder is used
			resolvedGw := rt.Gateway
			if resolvedGw == "default" {
				resolvedGw = r.GetDefaultGatewayIP(rt.Interface)
				if resolvedGw == "" {
					resolvedGw = r.GetDefaultGatewayIP("")
				}
			}

			key := routeKey{
				dest:  canonicalCIDR(rt.Destination),
				gw:    strings.TrimSpace(resolvedGw),
				iface: strings.TrimSpace(rt.Interface),
			}
			customRoutes[key] = &rt
		}
	}
	rowsCustom.Close()

	// 2. Track which custom routes were found active in OS
	foundCustom := make(map[string]bool)

	for _, kr := range kernelRoutes {
		key := routeKey{
			dest:  canonicalCIDR(kr.Destination),
			gw:    strings.TrimSpace(kr.Gateway),
			iface: strings.TrimSpace(kr.Interface),
		}

		if custRt, found := customRoutes[key]; found {
			foundCustom[custRt.ID] = true
		}
	}

	// 4. Update status of custom routes in DB based on presence in kernel
	for _, custRt := range customRoutes {
		isActiveInOS := foundCustom[custRt.ID]
		if isActiveInOS && !custRt.Status {
			_, _ = r.db.Exec("UPDATE static_routes SET status = 1 WHERE id = ?", custRt.ID)
		} else if !isActiveInOS && custRt.Status {
			_, _ = r.db.Exec("UPDATE static_routes SET status = 0 WHERE id = ?", custRt.ID)
		}
	}

	// 5. Delete old system and defaultgateway routes (do NOT insert them back)
	_, err = r.db.Exec("DELETE FROM static_routes WHERE type IN ('system', 'defaultgateway')")
	if err != nil {
		return fmt.Errorf("failed to clear old system routes: %w", err)
	}

	return nil
}

func parseHexIP(hexStr string) (string, error) {
	if len(hexStr) != 8 {
		return "", fmt.Errorf("invalid hex IP length: %s", hexStr)
	}
	// The hex string is little-endian format (e.g. "0022A8C0" for 192.168.34.0)
	var ipBytes [4]byte
	for i := 0; i < 4; i++ {
		b, err := strconv.ParseUint(hexStr[i*2:i*2+2], 16, 8)
		if err != nil {
			return "", err
		}
		ipBytes[i] = byte(b)
	}
	// Format bytes in little-endian order (reverse byte index)
	return fmt.Sprintf("%d.%d.%d.%d", ipBytes[3], ipBytes[2], ipBytes[1], ipBytes[0]), nil
}

// ClearInterfaces deletes all records from network_interfaces.
func (r *Repository) ClearInterfaces() error {
	_, err := r.db.Exec("DELETE FROM network_interfaces")
	return err
}

// SyncDNSFromOS reads nameservers from /etc/resolv.conf on Linux and updates the database.
func (r *Repository) SyncDNSFromOS() error {
	if runtime.GOOS != "linux" {
		return nil
	}

	data, err := os.ReadFile("/etc/resolv.conf")
	if err != nil {
		return fmt.Errorf("failed to read /etc/resolv.conf: %w", err)
	}

	var nameservers []string
	localDomain := "pigate.local"

	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "#") || line == "" {
			continue
		}

		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}

		if fields[0] == "nameserver" {
			nameservers = append(nameservers, fields[1])
		} else if fields[0] == "search" || fields[0] == "domain" {
			localDomain = fields[1]
		}
	}

	primaryDNS := ""
	secondaryDNS := ""

	if len(nameservers) > 0 {
		primaryDNS = nameservers[0]
	}
	if len(nameservers) > 1 {
		secondaryDNS = nameservers[1]
	}

	// If no nameservers found, fallback to defaults
	if primaryDNS == "" {
		primaryDNS = "1.1.1.1"
	}
	if secondaryDNS == "" {
		secondaryDNS = "8.8.8.8"
	}

	_, err = r.db.Exec(`UPDATE system_dns_settings SET 
		mode = 'static', primary_dns = ?, secondary_dns = ?, local_domain = ?
		WHERE id = 1`, primaryDNS, secondaryDNS, localDomain)
	if err != nil {
		return fmt.Errorf("failed to update DNS settings in DB: %w", err)
	}

	return nil
}

// =========================================================================
// DNS SERVER (dnsmasq)
// =========================================================================

func (r *Repository) GetDNSZones() ([]model.DNSZone, error) {
	rows, err := r.db.Query("SELECT id, zone_name, forward_to, allowed_ips, is_authoritative, enabled FROM dns_zones")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	zones := []model.DNSZone{}
	for rows.Next() {
		var z model.DNSZone
		var isAuth, enabled int
		err := rows.Scan(&z.ID, &z.ZoneName, &z.ForwardTo, &z.AllowedIPs, &isAuth, &enabled)
		if err != nil {
			return nil, err
		}
		z.IsAuthoritative = isAuth == 1
		z.Enabled = enabled == 1
		
		// Load records for each zone
		records, err := r.GetDNSRecordsByZone(z.ID)
		if err == nil && records != nil {
			z.Records = records
		} else {
			z.Records = []model.DNSRecord{}
		}

		zones = append(zones, z)
	}
	return zones, nil
}

func (r *Repository) GetDNSZoneByID(id string) (*model.DNSZone, error) {
	row := r.db.QueryRow("SELECT id, zone_name, forward_to, allowed_ips, is_authoritative, enabled FROM dns_zones WHERE id = ?", id)
	var z model.DNSZone
	var isAuth, enabled int
	err := row.Scan(&z.ID, &z.ZoneName, &z.ForwardTo, &z.AllowedIPs, &isAuth, &enabled)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	z.IsAuthoritative = isAuth == 1
	z.Enabled = enabled == 1

	records, err := r.GetDNSRecordsByZone(z.ID)
	if err == nil && records != nil {
		z.Records = records
	} else {
		z.Records = []model.DNSRecord{}
	}

	return &z, nil
}

func (r *Repository) CreateDNSZone(zone model.DNSZone) error {
	isAuth := 0
	if zone.IsAuthoritative {
		isAuth = 1
	}
	enabled := 0
	if zone.Enabled {
		enabled = 1
	}
	_, err := r.db.Exec("INSERT INTO dns_zones (id, zone_name, forward_to, allowed_ips, is_authoritative, enabled) VALUES (?, ?, ?, ?, ?, ?)",
		zone.ID, zone.ZoneName, zone.ForwardTo, zone.AllowedIPs, isAuth, enabled)
	return err
}

func (r *Repository) UpdateDNSZone(zone model.DNSZone) error {
	isAuth := 0
	if zone.IsAuthoritative {
		isAuth = 1
	}
	enabled := 0
	if zone.Enabled {
		enabled = 1
	}
	_, err := r.db.Exec("UPDATE dns_zones SET zone_name = ?, forward_to = ?, allowed_ips = ?, is_authoritative = ?, enabled = ? WHERE id = ?",
		zone.ZoneName, zone.ForwardTo, zone.AllowedIPs, isAuth, enabled, zone.ID)
	return err
}

func (r *Repository) DeleteDNSZone(id string) error {
	_, err := r.db.Exec("DELETE FROM dns_zones WHERE id = ?", id)
	return err
}

func (r *Repository) ToggleDNSZone(id string) error {
	_, err := r.db.Exec("UPDATE dns_zones SET enabled = NOT enabled WHERE id = ?", id)
	return err
}

func (r *Repository) GetDNSRecordsByZone(zoneID string) ([]model.DNSRecord, error) {
	rows, err := r.db.Query("SELECT id, zone_id, name, type, value, ttl FROM dns_records WHERE zone_id = ?", zoneID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	records := []model.DNSRecord{}
	for rows.Next() {
		var rec model.DNSRecord
		err := rows.Scan(&rec.ID, &rec.ZoneID, &rec.Name, &rec.Type, &rec.Value, &rec.TTL)
		if err != nil {
			return nil, err
		}
		records = append(records, rec)
	}
	return records, nil
}

func (r *Repository) GetDNSRecordByID(id string) (*model.DNSRecord, error) {
	row := r.db.QueryRow("SELECT id, zone_id, name, type, value, ttl FROM dns_records WHERE id = ?", id)
	var rec model.DNSRecord
	err := row.Scan(&rec.ID, &rec.ZoneID, &rec.Name, &rec.Type, &rec.Value, &rec.TTL)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &rec, nil
}

func (r *Repository) CreateDNSRecord(record model.DNSRecord) error {
	_, err := r.db.Exec("INSERT INTO dns_records (id, zone_id, name, type, value, ttl) VALUES (?, ?, ?, ?, ?, ?)",
		record.ID, record.ZoneID, record.Name, record.Type, record.Value, record.TTL)
	return err
}

func (r *Repository) UpdateDNSRecord(record model.DNSRecord) error {
	_, err := r.db.Exec("UPDATE dns_records SET name = ?, type = ?, value = ?, ttl = ? WHERE id = ?",
		record.Name, record.Type, record.Value, record.TTL, record.ID)
	return err
}

func (r *Repository) DeleteDNSRecord(id string) error {
	_, err := r.db.Exec("DELETE FROM dns_records WHERE id = ?", id)
	return err
}

// GetDNSServerInterfaces returns the real interfaces the DNS Server is configured
// to bind to. Stored independently from dhcp_configs.
func (r *Repository) GetDNSServerInterfaces() ([]string, error) {
	var stored string
	err := r.db.QueryRow("SELECT interfaces FROM dns_server_settings WHERE id = 1").Scan(&stored)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(stored) == "" {
		return []string{}, nil
	}
	return strings.Split(stored, ","), nil
}

// SetDNSServerInterfaces replaces the set of interfaces the DNS Server binds to.
func (r *Repository) SetDNSServerInterfaces(interfaces []string) error {
	_, err := r.db.Exec("UPDATE dns_server_settings SET interfaces = ? WHERE id = 1", strings.Join(interfaces, ","))
	return err
}
