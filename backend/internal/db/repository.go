package db

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"pigate/internal/model"
)

type Repository struct {
	db *sql.DB
}

func NewRepository(db *sql.DB) *Repository {
	return &Repository{db: db}
}

// =========================================================================
// USER AUTHENTICATION
// =========================================================================

func (r *Repository) GetUserByUsername(username string) (*model.User, error) {
	row := r.db.QueryRow("SELECT id, username, password_hash, created_at FROM users WHERE username = ?", username)
	var u model.User
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &u, nil
}

func (r *Repository) ChangePassword(username string, newPasswordHash string) error {
	_, err := r.db.Exec("UPDATE users SET password_hash = ? WHERE username = ?", newPasswordHash, username)
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

func (r *Repository) CreateAddress(addr model.AddressObject) error {
	sysVal := 0
	if addr.System {
		sysVal = 1
	}
	_, err := r.db.Exec("INSERT INTO address_objects (id, name, type, value, system) VALUES (?, ?, ?, ?, ?)",
		addr.ID, addr.Name, addr.Type, addr.Value, sysVal)
	return err
}

func (r *Repository) UpdateAddress(addr model.AddressObject) error {
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

func (r *Repository) CreateService(svc model.ServiceObject) error {
	_, err := r.db.Exec("INSERT INTO service_objects (id, name, protocol, port, type) VALUES (?, ?, ?, ?, ?)",
		svc.ID, svc.Name, svc.Protocol, svc.Port, svc.Type)
	return err
}

func (r *Repository) UpdateService(svc model.ServiceObject) error {
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
			// Skip or throw error? Let's check: if name is a raw IP value, let's create a temporary address object or throw.
			// Standard behavior is to verify. If not found, skip.
			continue
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
			continue
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
			// Sometimes it is formatted like "HTTP (TCP 80)", try to search by prefix name
			// Splitting by whitespace and finding matching prefix
			parts := strings.Split(svcName, " ")
			if len(parts) > 0 {
				_ = tx.QueryRow("SELECT id FROM service_objects WHERE name = ?", parts[0]).Scan(&svcID)
			}
		}
		if svcID != "" {
			_, err = tx.Exec("INSERT INTO policy_services (policy_id, service_id) VALUES (?, ?)", policyID, svcID)
			if err != nil {
				return err
			}
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

func (r *Repository) GetRoutes() ([]model.StaticRoute, error) {
	rows, err := r.db.Query("SELECT id, destination, gateway, interface, metric, description, status, type FROM static_routes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.StaticRoute{}
	for rows.Next() {
		var route model.StaticRoute
		var statInt int
		if err := rows.Scan(&route.ID, &route.Destination, &route.Gateway, &route.Interface, &route.Metric, &route.Description, &statInt, &route.Type); err != nil {
			return nil, err
		}
		route.Status = statInt == 1
		list = append(list, route)
	}
	return list, nil
}

func (r *Repository) GetRouteByID(id string) (*model.StaticRoute, error) {
	row := r.db.QueryRow("SELECT id, destination, gateway, interface, metric, description, status, type FROM static_routes WHERE id = ?", id)
	var route model.StaticRoute
	var statInt int
	err := row.Scan(&route.ID, &route.Destination, &route.Gateway, &route.Interface, &route.Metric, &route.Description, &statInt, &route.Type)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	route.Status = statInt == 1
	return &route, nil
}

func (r *Repository) CreateRoute(route model.StaticRoute) error {
	statVal := 0
	if route.Status {
		statVal = 1
	}
	_, err := r.db.Exec("INSERT INTO static_routes (id, destination, gateway, interface, metric, description, status, type) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
		route.ID, route.Destination, route.Gateway, route.Interface, route.Metric, route.Description, statVal, route.Type)
	return err
}

func (r *Repository) UpdateRoute(route model.StaticRoute) error {
	var rType string
	err := r.db.QueryRow("SELECT type FROM static_routes WHERE id = ?", route.ID).Scan(&rType)
	if err != nil {
		return err
	}
	if rType == "system" {
		return errors.New("cannot update system predefined static routes")
	}

	statVal := 0
	if route.Status {
		statVal = 1
	}
	_, err = r.db.Exec("UPDATE static_routes SET destination = ?, gateway = ?, interface = ?, metric = ?, description = ?, status = ? WHERE id = ?",
		route.Destination, route.Gateway, route.Interface, route.Metric, route.Description, statVal, route.ID)
	return err
}

func (r *Repository) DeleteRoute(id string) error {
	var rType string
	err := r.db.QueryRow("SELECT type FROM static_routes WHERE id = ?", id).Scan(&rType)
	if err != nil {
		return err
	}
	if rType == "system" {
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

	var systemCount int
	querySys := fmt.Sprintf("SELECT COUNT(*) FROM static_routes WHERE type = 'system' AND id IN (%s)", queryIn)
	if err := r.db.QueryRow(querySys, args...).Scan(&systemCount); err != nil {
		return err
	}
	if systemCount > 0 {
		return errors.New("cannot delete system predefined static routes in bulk")
	}

	queryDel := fmt.Sprintf("DELETE FROM static_routes WHERE id IN (%s)", queryIn)
	_, err := r.db.Exec(queryDel, args...)
	return err
}

func (r *Repository) ToggleRouteStatus(id string) error {
	_, err := r.db.Exec("UPDATE static_routes SET status = NOT status WHERE id = ?", id)
	return err
}

// =========================================================================
// DHCP SERVER
// =========================================================================

func (r *Repository) GetDHCPConfig() (*model.DhcpConfig, error) {
	row := r.db.QueryRow("SELECT enabled, interface, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time FROM dhcp_config WHERE id = 1")
	var cfg model.DhcpConfig
	var enabledInt int
	err := row.Scan(&enabledInt, &cfg.Interface, &cfg.StartIP, &cfg.EndIP, &cfg.Gateway, &cfg.Netmask, &cfg.DNS1, &cfg.DNS2, &cfg.LeaseTime)
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
	_, err := r.db.Exec(`UPDATE dhcp_config SET 
		enabled = ?, interface = ?, start_ip = ?, end_ip = ?, gateway = ?, netmask = ?, dns1 = ?, dns2 = ?, lease_time = ? 
		WHERE id = 1`,
		enabledVal, cfg.Interface, cfg.StartIP, cfg.EndIP, cfg.Gateway, cfg.Netmask, cfg.DNS1, cfg.DNS2, cfg.LeaseTime)
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

func (r *Repository) GetInterfaces() ([]model.NetworkInterface, error) {
	rows, err := r.db.Query(`SELECT 
		id, name, alias, role, type, addressing_mode, ip, netmask, gateway, dns1, dns2, mac_address, admin_access, status, speed,
		mac_mode, real_mac_address, randomized_mac, laa_mac_address, randomize_on_reconnect,
		connected_ssid, wifi_security, failover_enabled, backup_ssid, backup_wifi_password, ip_check_timeout, primary_max_retries, failover_cooldown
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
			&iface.ID, &iface.Name, &iface.Alias, &iface.Role, &iface.Type, &iface.AddressingMode,
			&iface.IP, &iface.Netmask, &iface.Gateway, &iface.DNS1, &iface.DNS2, &iface.MacAddress,
			&adminAccessStr, &iface.Status, &iface.Speed,
			&iface.MacMode, &iface.RealMacAddress, &iface.RandomizedMac, &iface.LaaMacAddress, &reconnectInt,
			&iface.ConnectedSSID, &iface.WifiSecurity, &failoverInt, &iface.BackupSSID, &iface.BackupWifiPassword,
			&iface.IPCheckTimeout, &iface.PrimaryMaxRetries, &iface.FailoverCooldown,
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
		id, name, alias, role, type, addressing_mode, ip, netmask, gateway, dns1, dns2, mac_address, admin_access, status, speed,
		mac_mode, real_mac_address, randomized_mac, laa_mac_address, randomize_on_reconnect,
		connected_ssid, wifi_security, failover_enabled, backup_ssid, backup_wifi_password, ip_check_timeout, primary_max_retries, failover_cooldown
		FROM network_interfaces WHERE id = ?`, id)
	var iface model.NetworkInterface
	var adminAccessStr string
	var reconnectInt, failoverInt int
	err := row.Scan(
		&iface.ID, &iface.Name, &iface.Alias, &iface.Role, &iface.Type, &iface.AddressingMode,
		&iface.IP, &iface.Netmask, &iface.Gateway, &iface.DNS1, &iface.DNS2, &iface.MacAddress,
		&adminAccessStr, &iface.Status, &iface.Speed,
		&iface.MacMode, &iface.RealMacAddress, &iface.RandomizedMac, &iface.LaaMacAddress, &reconnectInt,
		&iface.ConnectedSSID, &iface.WifiSecurity, &failoverInt, &iface.BackupSSID, &iface.BackupWifiPassword,
		&iface.IPCheckTimeout, &iface.PrimaryMaxRetries, &iface.FailoverCooldown,
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
	adminAccessStr := strings.Join(iface.AdminAccess, ",")
	reconInt := 0
	if iface.RandomizeOnReconnect != nil && *iface.RandomizeOnReconnect {
		reconInt = 1
	}
	foInt := 0
	if iface.FailoverEnabled != nil && *iface.FailoverEnabled {
		foInt = 1
	}

	_, err := r.db.Exec(`UPDATE network_interfaces SET 
		alias = ?, role = ?, addressing_mode = ?, ip = ?, netmask = ?, gateway = ?, dns1 = ?, dns2 = ?, mac_address = ?, admin_access = ?, 
		mac_mode = ?, real_mac_address = ?, randomized_mac = ?, laa_mac_address = ?, randomize_on_reconnect = ?,
		connected_ssid = ?, wifi_security = ?, failover_enabled = ?, backup_ssid = ?, backup_wifi_password = ?, 
		ip_check_timeout = ?, primary_max_retries = ?, failover_cooldown = ?
		WHERE id = ?`,
		iface.Alias, iface.Role, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, iface.DNS1, iface.DNS2, iface.MacAddress, adminAccessStr,
		iface.MacMode, iface.RealMacAddress, iface.RandomizedMac, iface.LaaMacAddress, reconInt,
		iface.ConnectedSSID, iface.WifiSecurity, foInt, iface.BackupSSID, iface.BackupWifiPassword,
		iface.IPCheckTimeout, iface.PrimaryMaxRetries, iface.FailoverCooldown, iface.ID)
	return err
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
