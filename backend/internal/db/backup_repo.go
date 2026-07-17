package db

import (
	"database/sql"
	"fmt"
	"strings"

	"pigate/internal/model"
)

// GetRawStaticRoutes returns static routes exactly as stored in the DB, without
// merging live kernel routes (GetRoutes) or resolving the "default" gateway
// sentinel to a concrete IP (GetDatabaseRoutes). This raw form is what a backup
// must capture so a restore on another machine/network keeps "default" and the
// original type classification intact.
func (r *Repository) GetRawStaticRoutes() ([]model.StaticRoute, error) {
	rows, err := r.db.Query("SELECT id, destination, gateway, interface, metric, description, status, type, scope, src, proto FROM static_routes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	list := []model.StaticRoute{}
	for rows.Next() {
		var rt model.StaticRoute
		var statInt int
		if err := rows.Scan(&rt.ID, &rt.Destination, &rt.Gateway, &rt.Interface, &rt.Metric, &rt.Description, &statInt, &rt.Type, &rt.Scope, &rt.Src, &rt.Proto); err != nil {
			return nil, err
		}
		rt.Status = statInt == 1
		list = append(list, rt)
	}
	return list, rows.Err()
}

// GetBackupUsers returns all users including their bcrypt password hash for
// inclusion in a backup. Unlike GetUsers (whose model.User hides the hash from
// JSON) this returns the credential material explicitly.
func (r *Repository) GetBackupUsers() ([]model.BackupUser, error) {
	users, err := r.GetUsers()
	if err != nil {
		return nil, err
	}
	out := make([]model.BackupUser, 0, len(users))
	for _, u := range users {
		out = append(out, model.BackupUser{
			ID:           u.ID,
			Username:     u.Username,
			PasswordHash: u.PasswordHash,
			IsInitial:    u.IsInitial,
			Role:         u.Role,
			Status:       u.Status,
			CreatedAt:    u.CreatedAt.Format("2006-01-02T15:04:05Z07:00"),
		})
	}
	return out, nil
}

// Checkpoint flushes the WAL into the main database file. Call this before
// SnapshotDatabase so a file-copy snapshot doesn't miss recently written pages
// that still live only in the -wal file. No-op errors are ignored by callers in
// mock/:memory: mode where WAL isn't enabled.
func (r *Repository) Checkpoint() error {
	_, err := r.db.Exec("PRAGMA wal_checkpoint(TRUNCATE);")
	return err
}

// RestoreConfig replaces all user-editable configuration with the contents of
// cfg inside a single transaction (wipe & restore semantics). System-seeded rows
// are preserved: system address/service objects, and system/defaultgateway
// static routes are never deleted or re-inserted. Interfaces are matched by id
// and updated in place — callers must pre-resolve cfg.Interfaces to rows that
// already exist on this device (see BackupService.Import). Users are only
// touched when includeUsers is true.
//
// Any error rolls the whole transaction back, leaving the original DB untouched.
func (r *Repository) RestoreConfig(cfg model.BackupConfig, includeUsers bool) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// --- 1. Wipe in FK-safe order (children before parents) --------------
	// Junction tables reference firewall_policies (CASCADE) and address/service
	// objects (RESTRICT), so they must go before the objects they point at.
	wipes := []string{
		"DELETE FROM policy_services",
		"DELETE FROM policy_addresses",
		"DELETE FROM firewall_policies",
		"DELETE FROM port_forwards",
		"DELETE FROM address_objects WHERE system = 0",
		"DELETE FROM service_objects WHERE type = 'custom'",
		"DELETE FROM static_routes WHERE type NOT IN ('system', 'defaultgateway')",
		"DELETE FROM qos_rules",
		"DELETE FROM dhcp_reservations",
		"DELETE FROM dhcp_configs",
		"DELETE FROM dns_records",
		"DELETE FROM dns_zones",
	}
	for _, q := range wipes {
		if _, err := tx.Exec(q); err != nil {
			return fmt.Errorf("wipe failed (%s): %w", q, err)
		}
	}

	// --- 2. Address objects (skip system; those were preserved) ----------
	for _, a := range cfg.Addresses {
		if a.System {
			continue
		}
		if _, err := tx.Exec(
			"INSERT INTO address_objects (id, name, type, value, system) VALUES (?, ?, ?, ?, 0)",
			a.ID, a.Name, a.Type, a.Value,
		); err != nil {
			return fmt.Errorf("restore address %q: %w", a.Name, err)
		}
	}

	// --- 3. Service objects (skip system) --------------------------------
	for _, s := range cfg.ServiceObjects {
		if s.Type == "system" {
			continue
		}
		if _, err := tx.Exec(
			"INSERT INTO service_objects (id, name, protocol, port, type) VALUES (?, ?, ?, ?, 'custom')",
			s.ID, s.Name, s.Protocol, s.Port,
		); err != nil {
			return fmt.Errorf("restore service %q: %w", s.Name, err)
		}
	}

	// --- 4. Firewall policies + junction relations -----------------------
	// Preserve the backup's ordering as priority (GetPolicies exported them
	// ordered by priority ASC).
	for i, p := range cfg.Policies {
		logVal, natVal, statVal := boolToInt(p.Log), boolToInt(p.Nat), boolToInt(p.Status)
		if _, err := tx.Exec(
			"INSERT INTO firewall_policies (id, name, in_interface, out_interface, action, log, nat, status, priority) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)",
			p.ID, p.Name, p.InInterface, p.OutInterface, p.Action, logVal, natVal, statVal, i+1,
		); err != nil {
			return fmt.Errorf("restore policy %q: %w", p.Name, err)
		}
		if err := restorePolicyRelations(tx, p); err != nil {
			return err
		}
	}

	// --- 4.1 Port forwards (DNAT) ----------------------------------------
	for _, pf := range cfg.PortForwards {
		if _, err := tx.Exec(
			"INSERT INTO port_forwards (id, name, in_interface, external_port, protocol, internal_ip, internal_port, status) VALUES (?, ?, ?, ?, ?, ?, ?, ?)",
			pf.ID, pf.Name, pf.InInterface, pf.ExternalPort, strings.ToLower(pf.Protocol), pf.InternalIP, pf.InternalPort, boolToInt(pf.Status),
		); err != nil {
			return fmt.Errorf("restore port forward %q: %w", pf.Name, err)
		}
	}

	// --- 5. Static routes (skip system/defaultgateway) -------------------
	for _, rt := range cfg.StaticRoutes {
		if rt.Type == "system" || rt.Type == "defaultgateway" {
			continue
		}
		scope := rt.Scope
		if scope == "" {
			scope = "global"
		}
		proto := rt.Proto
		if proto == "" {
			proto = "static"
		}
		if _, err := tx.Exec(
			"INSERT INTO static_routes (id, destination, gateway, interface, metric, description, status, type, scope, src, proto) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			rt.ID, rt.Destination, rt.Gateway, rt.Interface, rt.Metric, rt.Description, boolToInt(rt.Status), rt.Type, scope, rt.Src, proto,
		); err != nil {
			return fmt.Errorf("restore route %q: %w", rt.Destination, err)
		}
	}

	// --- 6. QoS rules ----------------------------------------------------
	for _, q := range cfg.QosRules {
		if _, err := tx.Exec(
			`INSERT INTO qos_rules (id, name, interface, match_src_ip, match_dst_ip, egress_rate_mbps, egress_ceil_mbps, ingress_rate_mbps, ingress_ceil_mbps, priority, status, description)
			 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			q.ID, q.Name, q.Interface, q.MatchSrcIP, q.MatchDstIP, q.EgressRateMbps, q.EgressCeilMbps, q.IngressRateMbps, q.IngressCeilMbps, q.Priority, boolToInt(q.Status), q.Description,
		); err != nil {
			return fmt.Errorf("restore qos rule %q: %w", q.Name, err)
		}
	}

	// --- 7. DHCP configs + reservations ----------------------------------
	for _, d := range cfg.DhcpConfigs {
		id := d.ID
		if id == "" {
			id = fmt.Sprintf("dhcp-cfg-%s", d.Interface)
		}
		if _, err := tx.Exec(
			"INSERT INTO dhcp_configs (id, interface, enabled, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)",
			id, d.Interface, boolToInt(d.Enabled), d.StartIP, d.EndIP, d.Gateway, d.Netmask, d.DNS1, d.DNS2, d.LeaseTime,
		); err != nil {
			return fmt.Errorf("restore dhcp config for %q: %w", d.Interface, err)
		}
	}
	for _, res := range cfg.DhcpReservations {
		if _, err := tx.Exec(
			"INSERT INTO dhcp_reservations (id, device_name, mac_address, ip_address) VALUES (?, ?, ?, ?)",
			res.ID, res.DeviceName, res.MacAddress, res.IPAddress,
		); err != nil {
			return fmt.Errorf("restore dhcp reservation %q: %w", res.MacAddress, err)
		}
	}

	// --- 8. DNS zones + records ------------------------------------------
	for _, z := range cfg.DnsZones {
		if _, err := tx.Exec(
			"INSERT INTO dns_zones (id, zone_name, forward_to, allowed_ips, is_authoritative, enabled) VALUES (?, ?, ?, ?, ?, ?)",
			z.ID, z.ZoneName, z.ForwardTo, z.AllowedIPs, boolToInt(z.IsAuthoritative), boolToInt(z.Enabled),
		); err != nil {
			return fmt.Errorf("restore dns zone %q: %w", z.ZoneName, err)
		}
		for _, rec := range z.Records {
			ttl := rec.TTL
			if ttl == 0 {
				ttl = 300
			}
			if _, err := tx.Exec(
				"INSERT INTO dns_records (id, zone_id, name, type, value, ttl) VALUES (?, ?, ?, ?, ?, ?)",
				rec.ID, z.ID, rec.Name, rec.Type, rec.Value, ttl,
			); err != nil {
				return fmt.Errorf("restore dns record %q: %w", rec.Name, err)
			}
		}
	}

	// --- 9. Single-row system settings -----------------------------------
	// These rows always exist (seeded); a backup that omits a section (e.g. a
	// legacy v1 file has no systemDns/dnsServerSettings) leaves the existing row
	// untouched rather than writing an invalid empty value.
	if cfg.DnsServerSettings.Interfaces != nil {
		if _, err := tx.Exec("UPDATE dns_server_settings SET interfaces = ? WHERE id = 1", strings.Join(cfg.DnsServerSettings.Interfaces, ",")); err != nil {
			return fmt.Errorf("restore dns server settings: %w", err)
		}
	}
	// System DNS (mode is CHECK-constrained to wan/static).
	if cfg.SystemDns.Mode != "" {
		if _, err := tx.Exec(
			"UPDATE system_dns_settings SET mode = ?, primary_dns = ?, secondary_dns = ?, local_domain = ? WHERE id = 1",
			cfg.SystemDns.Mode, cfg.SystemDns.PrimaryDNS, cfg.SystemDns.SecondaryDNS, cfg.SystemDns.LocalDomain,
		); err != nil {
			return fmt.Errorf("restore system dns: %w", err)
		}
	}
	// System time (Status is live-only and excluded from backup).
	if cfg.SystemTime.Timezone != "" {
		if _, err := tx.Exec(
			"UPDATE system_time_settings SET timezone = ?, ntp_sync = ?, ntp_server = ? WHERE id = 1",
			cfg.SystemTime.Timezone, boolToInt(cfg.SystemTime.NTPSync), cfg.SystemTime.NTPServer,
		); err != nil {
			return fmt.Errorf("restore system time: %w", err)
		}
	}
	// Hostname.
	if cfg.SystemHostname.Hostname != "" {
		if _, err := tx.Exec(
			"UPDATE system_hostname_settings SET hostname = ?, share_with_dhcp = ? WHERE id = 1",
			cfg.SystemHostname.Hostname, boolToInt(cfg.SystemHostname.ShareWithDhcp),
		); err != nil {
			return fmt.Errorf("restore hostname: %w", err)
		}
	}

	// --- 10. Interfaces (update-in-place; matched to existing rows) -------
	for _, iface := range cfg.Interfaces {
		if err := restoreInterface(tx, iface); err != nil {
			return err
		}
	}

	// --- 11. Users (optional) --------------------------------------------
	if includeUsers {
		if _, err := tx.Exec("DELETE FROM users"); err != nil {
			return fmt.Errorf("wipe users: %w", err)
		}
		for _, u := range cfg.Users {
			if _, err := tx.Exec(
				"INSERT INTO users (id, username, password_hash, is_initial, role, status) VALUES (?, ?, ?, ?, ?, ?)",
				u.ID, u.Username, u.PasswordHash, boolToInt(u.IsInitial), u.Role, u.Status,
			); err != nil {
				return fmt.Errorf("restore user %q: %w", u.Username, err)
			}
		}
	}

	return tx.Commit()
}

// restorePolicyRelations reinserts a policy's source/destination/service links,
// resolving object names to ids within the same transaction. Address/service
// objects (system + freshly restored custom) must already be inserted.
func restorePolicyRelations(tx *sql.Tx, p model.PolicyRule) error {
	link := func(names []string, assoc string) error {
		for _, name := range names {
			var addrID string
			if err := tx.QueryRow("SELECT id FROM address_objects WHERE name = ?", name).Scan(&addrID); err != nil {
				return fmt.Errorf("policy %q references missing address object %q", p.Name, name)
			}
			if _, err := tx.Exec("INSERT INTO policy_addresses (policy_id, address_id, association_type) VALUES (?, ?, ?)", p.ID, addrID, assoc); err != nil {
				return err
			}
		}
		return nil
	}
	if err := link(p.Source, "SOURCE"); err != nil {
		return err
	}
	if err := link(p.Destination, "DESTINATION"); err != nil {
		return err
	}
	for _, name := range p.Service {
		var svcID string
		if err := tx.QueryRow("SELECT id FROM service_objects WHERE name = ?", name).Scan(&svcID); err != nil {
			return fmt.Errorf("policy %q references missing service object %q", p.Name, name)
		}
		if _, err := tx.Exec("INSERT INTO policy_services (policy_id, service_id) VALUES (?, ?)", p.ID, svcID); err != nil {
			return err
		}
	}
	return nil
}

// restoreInterface updates the config fields of an existing interface row,
// matched by id. Hardware/runtime identity columns (name, type, mac_address,
// real_mac_address, status, speed) are intentionally not written — the caller
// (BackupService.Import) has already merged the backup's config fields onto the
// live device row, so those columns already hold the device's own values.
func restoreInterface(tx *sql.Tx, iface model.NetworkInterface) error {
	adminAccess := strings.Join(iface.AdminAccess, ",")
	recon := 0
	if iface.RandomizeOnReconnect != nil && *iface.RandomizeOnReconnect {
		recon = 1
	}
	fo := 0
	if iface.FailoverEnabled != nil && *iface.FailoverEnabled {
		fo = 1
	}
	res, err := tx.Exec(`UPDATE network_interfaces SET
		alias = ?, role = ?, addressing_mode = ?, ip = ?, netmask = ?, gateway = ?, metric = ?, admin_access = ?,
		mac_mode = ?, randomized_mac = ?, laa_mac_address = ?, randomize_on_reconnect = ?,
		connected_ssid = ?, wifi_password = ?, wifi_security = ?, failover_enabled = ?, backup_ssid = ?, backup_wifi_password = ?, backup_wifi_security = ?,
		ip_check_timeout = ?, primary_max_retries = ?, failover_cooldown = ?
		WHERE id = ?`,
		iface.Alias, iface.Role, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, iface.Metric, adminAccess,
		iface.MacMode, iface.RandomizedMac, iface.LaaMacAddress, recon,
		iface.WifiSSID, iface.WifiPassword, iface.WifiSecurity, fo, iface.BackupSSID, iface.BackupWifiPassword, iface.BackupWifiSecurity,
		iface.IPCheckTimeout, iface.PrimaryMaxRetries, iface.FailoverCooldown, iface.ID)
	if err != nil {
		return fmt.Errorf("restore interface %q: %w", iface.Name, err)
	}

	// Physical interfaces are always update-in-place (they exist as device rows).
	// VLAN sub-interfaces, however, may not exist on the target board yet — the
	// backup carries them so they can be re-created on boot (issue #20). When the
	// UPDATE matched no row and this is a VLAN, INSERT the full row (identity fields
	// included) so the VLAN survives a restore onto a fresh device.
	if n, _ := res.RowsAffected(); n == 0 && iface.Subtype == "vlan" {
		if _, err := tx.Exec(`INSERT INTO network_interfaces (
			id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, metric, mac_address, admin_access, status, speed,
			mac_mode, randomized_mac, laa_mac_address, randomize_on_reconnect,
			connected_ssid, wifi_password, wifi_security, failover_enabled, backup_ssid, backup_wifi_password, backup_wifi_security,
			ip_check_timeout, primary_max_retries, failover_cooldown, vlan_parent, vlan_id
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			iface.ID, iface.Name, iface.Alias, iface.Role, iface.Type, iface.Subtype, iface.AddressingMode, iface.IP, iface.Netmask, iface.Gateway, iface.Metric, iface.MacAddress, adminAccess, iface.Status, iface.Speed,
			iface.MacMode, iface.RandomizedMac, iface.LaaMacAddress, recon,
			iface.WifiSSID, iface.WifiPassword, iface.WifiSecurity, fo, iface.BackupSSID, iface.BackupWifiPassword, iface.BackupWifiSecurity,
			iface.IPCheckTimeout, iface.PrimaryMaxRetries, iface.FailoverCooldown, iface.VlanParent, iface.VlanID); err != nil {
			return fmt.Errorf("restore vlan interface %q: %w", iface.Name, err)
		}
	}
	return nil
}

func boolToInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
