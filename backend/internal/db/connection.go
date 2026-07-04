package db

import (
	"crypto/rand"
	"database/sql"
	"fmt"
	"log"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"
	_ "modernc.org/sqlite"
)

// InitDB initializes SQLite connection, runs table migrations, and seeds initial data.
func InitDB(dsn string, isMock ...bool) (*sql.DB, error) {
	mockMode := true
	if len(isMock) > 0 {
		mockMode = isMock[0]
	}

	// If it is a file-based DB, ensure the parent directory exists
	if dsn != ":memory:" {
		dir := filepath.Dir(dsn)
		if dir != "." && dir != "/" {
			if err := os.MkdirAll(dir, 0755); err != nil {
				return nil, fmt.Errorf("failed to create db directory: %w", err)
			}
		}
	}

	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	// Enable WAL mode and foreign keys for better concurrency and integrity
	if dsn != ":memory:" {
		if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
			return nil, fmt.Errorf("failed to set WAL mode: %w", err)
		}
	}
	if _, err := db.Exec("PRAGMA foreign_keys=ON;"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	if err := backupDatabase(dsn); err != nil {
		log.Printf("[Warning] Failed to backup database: %v", err)
	}

	if err := migrate(db); err != nil {
		return nil, fmt.Errorf("database migration failed: %w", err)
	}

	if err := seed(db, dsn, mockMode); err != nil {
		return nil, fmt.Errorf("database seeding failed: %w", err)
	}

	return db, nil
}

func migrate(db *sql.DB) error {
	// Rebuild static_routes table if existing schema doesn't support defaultgateway type in CHECK constraint or advanced fields
	var sqlCreate string
	err := db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='static_routes'").Scan(&sqlCreate)
	if err == nil {
		if !strings.Contains(sqlCreate, "'customgateway'") || !strings.Contains(sqlCreate, "scope") {
			migrationQueries := []string{
				"PRAGMA foreign_keys=OFF;",
				`CREATE TABLE static_routes_new (
					id TEXT PRIMARY KEY,
					destination TEXT NOT NULL,
					gateway TEXT NOT NULL,
					interface TEXT NOT NULL,
					metric INTEGER DEFAULT 0,
					description TEXT,
					status INTEGER DEFAULT 1 CHECK(status IN (0, 1)),
					type TEXT NOT NULL CHECK(type IN ('system', 'custom', 'defaultgateway', 'customgateway')),
					scope TEXT DEFAULT 'global',
					src TEXT DEFAULT '',
					proto TEXT DEFAULT 'static'
				);`,
				"INSERT INTO static_routes_new (id, destination, gateway, interface, metric, description, status, type, scope, src, proto) SELECT id, destination, gateway, interface, metric, description, status, type, 'global', '', 'static' FROM static_routes;",
				"DROP TABLE static_routes;",
				"ALTER TABLE static_routes_new RENAME TO static_routes;",
				"PRAGMA foreign_keys=ON;",
			}
			for _, q := range migrationQueries {
				if _, err := db.Exec(q); err != nil {
					return fmt.Errorf("failed to migrate static_routes table: %w (query: %s)", err, q)
				}
			}
		}
	}

	// Rebuild network_interfaces table if existing schema doesn't support 'offline' status in CHECK constraint
	var sqlCreateIface string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='network_interfaces'").Scan(&sqlCreateIface)
	if err == nil {
		if !strings.Contains(sqlCreateIface, "'offline'") {
			migrationQueries := []string{
				"PRAGMA foreign_keys=OFF;",
				`CREATE TABLE network_interfaces_new (
					id TEXT PRIMARY KEY,
					name TEXT UNIQUE NOT NULL,
					alias TEXT NOT NULL,
					role TEXT NOT NULL CHECK(role IN ('LAN', 'WAN')),
					type TEXT NOT NULL CHECK(type IN ('ethernet', 'wireless')),
					addressing_mode TEXT NOT NULL CHECK(addressing_mode IN ('dhcp', 'static')),
					ip TEXT NOT NULL,
					netmask TEXT NOT NULL,
					gateway TEXT NOT NULL,
					mac_address TEXT NOT NULL,
					admin_access TEXT NOT NULL,
					status TEXT NOT NULL CHECK(status IN ('up', 'down', 'offline')),
					speed TEXT NOT NULL,
					connected_ssid TEXT,
					wifi_password TEXT,
					wifi_security TEXT,
					mac_mode TEXT CHECK(mac_mode IN ('hardware', 'randomized', 'laa')),
					real_mac_address TEXT,
					randomized_mac TEXT,
					laa_mac_address TEXT,
					randomize_on_reconnect INTEGER DEFAULT 0,
					failover_enabled INTEGER DEFAULT 0,
					backup_ssid TEXT,
					backup_wifi_password TEXT,
					backup_wifi_security TEXT DEFAULT 'WPA2',
					ip_check_timeout INTEGER,
					primary_max_retries INTEGER,
					failover_cooldown INTEGER
				);`,
				`INSERT INTO network_interfaces_new (
					id, name, alias, role, type, addressing_mode, ip, netmask, gateway, mac_address, admin_access, status, speed,
					connected_ssid, wifi_password, wifi_security, mac_mode, real_mac_address, randomized_mac, laa_mac_address, 
					randomize_on_reconnect, failover_enabled, backup_ssid, backup_wifi_password, backup_wifi_security, ip_check_timeout, primary_max_retries, failover_cooldown
				) SELECT 
					id, name, alias, role, type, addressing_mode, ip, netmask, gateway, mac_address, admin_access, status, speed,
					connected_ssid, wifi_password, wifi_security, mac_mode, real_mac_address, randomized_mac, laa_mac_address, 
					randomize_on_reconnect, failover_enabled, backup_ssid, backup_wifi_password, 'WPA2', ip_check_timeout, primary_max_retries, failover_cooldown 
				FROM network_interfaces;`,
				"DROP TABLE network_interfaces;",
				"ALTER TABLE network_interfaces_new RENAME TO network_interfaces;",
				"PRAGMA foreign_keys=ON;",
			}
			for _, q := range migrationQueries {
				if _, err := db.Exec(q); err != nil {
					return fmt.Errorf("failed to migrate network_interfaces table: %w (query: %s)", err, q)
				}
			}
		}
	}

	// Add subtype column to network_interfaces if it doesn't exist
	var sqlCreateIfaceSubtype string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='network_interfaces'").Scan(&sqlCreateIfaceSubtype)
	if err == nil {
		if !strings.Contains(sqlCreateIfaceSubtype, "subtype") {
			_, err = db.Exec("ALTER TABLE network_interfaces ADD COLUMN subtype TEXT DEFAULT ''")
			if err != nil {
				return fmt.Errorf("failed to add subtype column: %w", err)
			}
		}
	}

	// Add backup_wifi_security column to network_interfaces if it doesn't exist
	var sqlCreateIfaceBackupSecurity string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='network_interfaces'").Scan(&sqlCreateIfaceBackupSecurity)
	if err == nil {
		if !strings.Contains(sqlCreateIfaceBackupSecurity, "backup_wifi_security") {
			_, err = db.Exec("ALTER TABLE network_interfaces ADD COLUMN backup_wifi_security TEXT DEFAULT 'WPA2'")
			if err != nil {
				return fmt.Errorf("failed to add backup_wifi_security column: %w", err)
			}
		}
	}

	// Add metric column to network_interfaces if it doesn't exist.
	// Nullable (no NOT NULL/DEFAULT) so "unset" (NULL) stays distinct from metric 0.
	var sqlCreateIfaceMetric string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='network_interfaces'").Scan(&sqlCreateIfaceMetric)
	if err == nil {
		if !strings.Contains(sqlCreateIfaceMetric, "metric") {
			_, err = db.Exec("ALTER TABLE network_interfaces ADD COLUMN metric INTEGER")
			if err != nil {
				return fmt.Errorf("failed to add metric column: %w", err)
			}
		}
	}

	queries := []string{
		`CREATE TABLE IF NOT EXISTS users (
			id TEXT PRIMARY KEY,
			username TEXT UNIQUE NOT NULL,
			password_hash TEXT NOT NULL,
			is_initial INTEGER DEFAULT 0,
			role TEXT NOT NULL DEFAULT 'super_admin' CHECK(role IN ('super_admin', 'admin_readonly')),
			status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'disabled')),
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS address_objects (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('subnet', 'range', 'fqdn')),
			value TEXT NOT NULL,
			system INTEGER DEFAULT 0 CHECK(system IN (0, 1)),
			comment TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS service_objects (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			protocol TEXT NOT NULL CHECK(protocol IN ('TCP', 'UDP', 'TCP/UDP', 'ICMP')),
			port TEXT NOT NULL,
			type TEXT NOT NULL CHECK(type IN ('system', 'custom')),
			comment TEXT
		);`,

		`CREATE TABLE IF NOT EXISTS firewall_policies (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			in_interface TEXT NOT NULL,
			out_interface TEXT NOT NULL,
			action TEXT NOT NULL CHECK(action IN ('ACCEPT', 'DROP')),
			log INTEGER DEFAULT 0 CHECK(log IN (0, 1)),
			status INTEGER DEFAULT 1 CHECK(status IN (0, 1)),
			priority INTEGER NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS policy_addresses (
			policy_id TEXT NOT NULL,
			address_id TEXT NOT NULL,
			association_type TEXT NOT NULL CHECK(association_type IN ('SOURCE', 'DESTINATION')),
			PRIMARY KEY (policy_id, address_id, association_type),
			FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE,
			FOREIGN KEY (address_id) REFERENCES address_objects(id) ON DELETE RESTRICT
		);`,

		`CREATE TABLE IF NOT EXISTS policy_services (
			policy_id TEXT NOT NULL,
			service_id TEXT NOT NULL,
			PRIMARY KEY (policy_id, service_id),
			FOREIGN KEY (policy_id) REFERENCES firewall_policies(id) ON DELETE CASCADE,
			FOREIGN KEY (service_id) REFERENCES service_objects(id) ON DELETE RESTRICT
		);`,

		`CREATE TABLE IF NOT EXISTS static_routes (
			id TEXT PRIMARY KEY,
			destination TEXT NOT NULL,
			gateway TEXT NOT NULL,
			interface TEXT NOT NULL,
			metric INTEGER DEFAULT 0,
			description TEXT,
			status INTEGER DEFAULT 1 CHECK(status IN (0, 1)),
			type TEXT NOT NULL CHECK(type IN ('system', 'custom', 'defaultgateway', 'customgateway')),
			scope TEXT DEFAULT 'global',
			src TEXT DEFAULT '',
			proto TEXT DEFAULT 'static'
		);`,

		`CREATE TABLE IF NOT EXISTS dhcp_configs (
			id          TEXT PRIMARY KEY,
			interface   TEXT NOT NULL UNIQUE,
			enabled     INTEGER DEFAULT 1 CHECK(enabled IN (0, 1)),
			start_ip    TEXT NOT NULL,
			end_ip      TEXT NOT NULL,
			gateway     TEXT NOT NULL,
			netmask     TEXT NOT NULL,
			dns1        TEXT NOT NULL DEFAULT '8.8.8.8',
			dns2        TEXT NOT NULL DEFAULT '1.1.1.1',
			lease_time  INTEGER NOT NULL DEFAULT 86400,
			created_at  DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at  DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS dhcp_leases (
			mac_address TEXT NOT NULL PRIMARY KEY,
			ip_address  TEXT NOT NULL,
			hostname    TEXT,
			interface   TEXT,
			expires_at  DATETIME
		);`,

		`CREATE TABLE IF NOT EXISTS dns_zones (
			id               TEXT PRIMARY KEY,
			zone_name        TEXT NOT NULL UNIQUE,
			forward_to       TEXT,
			allowed_ips      TEXT,
			is_authoritative INTEGER DEFAULT 1 CHECK(is_authoritative IN (0, 1)),
			enabled          INTEGER DEFAULT 1 CHECK(enabled IN (0, 1)),
			created_at       DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,

		`CREATE TABLE IF NOT EXISTS dns_records (
			id         TEXT PRIMARY KEY,
			zone_id    TEXT NOT NULL,
			name       TEXT NOT NULL,
			type       TEXT NOT NULL CHECK(type IN ('A','AAAA','CNAME','MX','TXT','PTR')),
			value      TEXT NOT NULL,
			ttl        INTEGER DEFAULT 300,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (zone_id) REFERENCES dns_zones(id) ON DELETE CASCADE
		);`,

		// Listen interfaces for the DNS Server (auth-server binding). Kept independent
		// from dhcp_configs so DNS Server interface selection doesn't depend on DHCP Server state.
		`CREATE TABLE IF NOT EXISTS dns_server_settings (
			id         INTEGER PRIMARY KEY CHECK(id = 1),
			interfaces TEXT NOT NULL DEFAULT ''
		);`,

		`CREATE TABLE IF NOT EXISTS dhcp_reservations (
			id TEXT PRIMARY KEY,
			device_name TEXT NOT NULL,
			mac_address TEXT UNIQUE NOT NULL,
			ip_address TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS system_time_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			timezone TEXT NOT NULL,
			ntp_sync INTEGER DEFAULT 1 CHECK(ntp_sync IN (0, 1)),
			ntp_server TEXT NOT NULL
		);`,

		`CREATE TABLE IF NOT EXISTS system_hostname_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			hostname TEXT NOT NULL,
			share_with_dhcp INTEGER DEFAULT 0 CHECK(share_with_dhcp IN (0,1))
		);`,

		`CREATE TABLE IF NOT EXISTS network_interfaces (
			id TEXT PRIMARY KEY,
			name TEXT UNIQUE NOT NULL,
			alias TEXT NOT NULL,
			role TEXT NOT NULL CHECK(role IN ('LAN', 'WAN')),
			type TEXT NOT NULL CHECK(type IN ('ethernet', 'wireless')),
			subtype TEXT DEFAULT '',
			addressing_mode TEXT NOT NULL CHECK(addressing_mode IN ('dhcp', 'static')),
			ip TEXT NOT NULL,
			netmask TEXT NOT NULL,
			gateway TEXT NOT NULL,
			metric INTEGER,
			mac_address TEXT NOT NULL,
			admin_access TEXT NOT NULL, -- comma separated values like "PING,HTTP,SSH"
			status TEXT NOT NULL CHECK(status IN ('up', 'down', 'offline')),
			speed TEXT NOT NULL,
			-- Wireless specific optional fields
			connected_ssid TEXT,
			wifi_password TEXT,
			wifi_security TEXT,
			mac_mode TEXT CHECK(mac_mode IN ('hardware', 'randomized', 'laa')),
			real_mac_address TEXT,
			randomized_mac TEXT,
			laa_mac_address TEXT,
			randomize_on_reconnect INTEGER DEFAULT 0,
			failover_enabled INTEGER DEFAULT 0,
			backup_ssid TEXT,
			backup_wifi_password TEXT,
			backup_wifi_security TEXT DEFAULT 'WPA2',
			ip_check_timeout INTEGER,
			primary_max_retries INTEGER,
			failover_cooldown INTEGER
		);`,

		`CREATE TABLE IF NOT EXISTS system_dns_settings (
			id INTEGER PRIMARY KEY CHECK(id = 1),
			mode TEXT NOT NULL CHECK(mode IN ('wan', 'static')),
			primary_dns TEXT NOT NULL,
			secondary_dns TEXT NOT NULL,
			local_domain TEXT NOT NULL DEFAULT 'pigate.local'
		);`,

		`CREATE TABLE IF NOT EXISTS qos_rules (
			id                TEXT PRIMARY KEY,
			name              TEXT NOT NULL,
			interface         TEXT NOT NULL,
			match_src_ip      TEXT NOT NULL DEFAULT '',
			match_dst_ip      TEXT NOT NULL DEFAULT '',
			egress_rate_mbps  INTEGER NOT NULL DEFAULT 0,
			egress_ceil_mbps  INTEGER NOT NULL DEFAULT 0,
			ingress_rate_mbps INTEGER NOT NULL DEFAULT 0,
			ingress_ceil_mbps INTEGER NOT NULL DEFAULT 0,
			priority          INTEGER NOT NULL DEFAULT 10,
			status            INTEGER NOT NULL DEFAULT 1 CHECK(status IN (0, 1)),
			description       TEXT NOT NULL DEFAULT '',
			created_at        DATETIME DEFAULT CURRENT_TIMESTAMP
		);`,
	}

	for _, query := range queries {
		if _, err := db.Exec(query); err != nil {
			return err
		}
	}

	// Migrate data from old dhcp_config (if it exists) to new dhcp_configs
	var sqlCreateOldDhcpConfig string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='dhcp_config'").Scan(&sqlCreateOldDhcpConfig)
	if err == nil {
		// Old table exists, check if new table has any records. If new table is empty, migrate the old record.
		var newCount int
		err = db.QueryRow("SELECT COUNT(*) FROM dhcp_configs").Scan(&newCount)
		if err == nil && newCount == 0 {
			row := db.QueryRow("SELECT enabled, interface, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time FROM dhcp_config WHERE id = 1")
			var enabled, leaseTime int
			var iface, startIP, endIP, gateway, netmask, dns1, dns2 string
			errScan := row.Scan(&enabled, &iface, &startIP, &endIP, &gateway, &netmask, &dns1, &dns2, &leaseTime)
			if errScan == nil {
				_, errInsert := db.Exec(`INSERT INTO dhcp_configs 
					(id, interface, enabled, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time) 
					VALUES ('dhcp-cfg-default', ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
					iface, enabled, startIP, endIP, gateway, netmask, dns1, dns2, leaseTime)
				if errInsert != nil {
					return fmt.Errorf("failed to migrate data from dhcp_config to dhcp_configs: %w", errInsert)
				}
				log.Println("[Migration] Successfully migrated old DHCP config to dhcp_configs table")
			}
		}
		// Drop old table
		_, errDrop := db.Exec("DROP TABLE dhcp_config;")
		if errDrop != nil {
			return fmt.Errorf("failed to drop old dhcp_config table: %w", errDrop)
		}
		log.Println("[Migration] Successfully dropped old dhcp_config table")
	}

	// Add is_initial column to users table if it doesn't exist
	var sqlCreateUsers string
	err = db.QueryRow("SELECT sql FROM sqlite_master WHERE type='table' AND name='users'").Scan(&sqlCreateUsers)
	if err == nil {
		if !strings.Contains(sqlCreateUsers, "is_initial") {
			_, err = db.Exec("ALTER TABLE users ADD COLUMN is_initial INTEGER DEFAULT 0")
			if err != nil {
				return fmt.Errorf("failed to add is_initial column to users table: %w", err)
			}
		}

		// Add role/status columns for the multi-user system. Detect via the
		// unique CHECK-constraint tokens ('admin_readonly' / 'disabled') rather
		// than the bare column names, which could collide with other substrings.
		// Existing rows (e.g. the legacy "pigate" account) inherit the column
		// DEFAULT of super_admin/active, so an upgraded box never locks out.
		if !strings.Contains(sqlCreateUsers, "admin_readonly") {
			_, err = db.Exec("ALTER TABLE users ADD COLUMN role TEXT NOT NULL DEFAULT 'super_admin' CHECK(role IN ('super_admin', 'admin_readonly'))")
			if err != nil {
				return fmt.Errorf("failed to add role column to users table: %w", err)
			}
		}
		if !strings.Contains(sqlCreateUsers, "'disabled'") {
			_, err = db.Exec("ALTER TABLE users ADD COLUMN status TEXT NOT NULL DEFAULT 'active' CHECK(status IN ('active', 'disabled'))")
			if err != nil {
				return fmt.Errorf("failed to add status column to users table: %w", err)
			}
		}
	}

	// Normalize legacy timezone values. Older seeds stored the display string
	// "Asia/Bangkok (GMT+7:00)" instead of a bare IANA name, which both
	// time.LoadLocation and systemd-timedated reject. Strip anything from the
	// first " (" onwards. Idempotent: values without the suffix are untouched.
	if err := normalizeTimezoneSetting(db); err != nil {
		return fmt.Errorf("failed to normalize legacy timezone value: %w", err)
	}

	return nil
}

// normalizeTimezoneSetting rewrites a legacy timezone value like
// "Asia/Bangkok (GMT+7:00)" to the bare IANA form "Asia/Bangkok". Safe to run
// repeatedly. If the row is missing it is a no-op.
func normalizeTimezoneSetting(db *sql.DB) error {
	var tz string
	err := db.QueryRow("SELECT timezone FROM system_time_settings WHERE id = 1").Scan(&tz)
	if err == sql.ErrNoRows {
		return nil
	}
	if err != nil {
		return err
	}

	normalized := NormalizeTimezone(tz)
	if normalized == tz {
		return nil
	}

	_, err = db.Exec("UPDATE system_time_settings SET timezone = ? WHERE id = 1", normalized)
	if err != nil {
		return err
	}
	log.Printf("[Migration] Normalized legacy timezone %q -> %q", tz, normalized)
	return nil
}

// NormalizeTimezone strips a trailing " (GMT...)"-style suffix from a timezone
// string, returning the bare IANA name. Exported so the config-import path can
// reuse it on old backup files. It does not validate the result — callers that
// need validation must do it separately.
func NormalizeTimezone(tz string) string {
	tz = strings.TrimSpace(tz)
	if idx := strings.Index(tz, " ("); idx >= 0 {
		tz = strings.TrimSpace(tz[:idx])
	}
	return tz
}

func generateRandomPassword(length int) (string, error) {
	const chars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(chars))))
		if err != nil {
			return "", err
		}
		result[i] = chars[num.Int64()]
	}
	return string(result), nil
}

func seed(db *sql.DB, dsn string, mockMode bool) error {
	// 1. Seed Default Admin User if empty
	var count int
	if err := db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		var password string
		var hash []byte
		var err error

		if dsn == ":memory:" {
			// For automated testing in memory, use static password "pigate"
			password = "pigate"
			hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("failed to hash default test password: %w", err)
			}
		} else {
			// For real execution, generate a secure random password
			password, err = generateRandomPassword(16)
			if err != nil {
				return fmt.Errorf("failed to generate random password: %w", err)
			}
			hash, err = bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
			if err != nil {
				return fmt.Errorf("failed to hash random password: %w", err)
			}
		}

		isInitialVal := 1
		if dsn == ":memory:" {
			isInitialVal = 0
		}
		_, err = db.Exec(`INSERT INTO users (id, username, password_hash, is_initial, role, status) VALUES (
			'user-pigate', 'pigate', ?, ?, 'super_admin', 'active'
		)`, string(hash), isInitialVal)
		if err != nil {
			return err
		}

		if dsn != ":memory:" {
			log.Printf("==================================================================")
			log.Printf("  PiGate First Run initialization")
			log.Printf("  Generated random password for user 'pigate': %s", password)
			log.Printf("==================================================================")
		}
	}

	// 2. Seed Default Predefined Address Objects
	var addrCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM address_objects").Scan(&addrCount); err != nil {
		return err
	}
	if addrCount == 0 {
		_, err := db.Exec(`INSERT INTO address_objects (id, name, type, value, system, comment) VALUES 
			('addr-1', 'ALL', 'subnet', '0.0.0.0/0', 1, 'Default fallback subnet object')`)
		if err != nil {
			return err
		}
	}

	// 3. Seed Default Predefined Service Objects
	var svcCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM service_objects").Scan(&svcCount); err != nil {
		return err
	}
	if svcCount == 0 {
		_, err := db.Exec(`INSERT INTO service_objects (id, name, protocol, port, type, comment) VALUES 
			('svc-1', 'ALL', 'TCP/UDP', '1-65535', 'system', 'All services and ports wildcard'),
			('svc-2', 'HTTP', 'TCP', '80', 'system', 'Web plain HTTP service'),
			('svc-3', 'HTTPS', 'TCP', '443', 'system', 'Web secure HTTPS service'),
			('svc-4', 'SSH', 'TCP', '22', 'system', 'Remote Secure Shell service'),
			('svc-5', 'DNS', 'UDP', '53', 'system', 'Domain Name System service'),
			('svc-6', 'ICMP', 'ICMP', '-', 'system', 'Internet Control Message Protocol')`)
		if err != nil {
			return err
		}
	}

	// 4. Seed Default DHCP Configuration
	var dhcpCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM dhcp_configs").Scan(&dhcpCount); err != nil {
		return err
	}
	if dhcpCount == 0 {
		_, err := db.Exec(`INSERT INTO dhcp_configs (id, enabled, interface, start_ip, end_ip, gateway, netmask, dns1, dns2, lease_time) VALUES 
			('dhcp-cfg-default', 0, 'eth0', '192.168.1.100', '192.168.1.200', '192.168.1.1', '255.255.255.0', '8.8.8.8', '1.1.1.1', 86400)`)
		if err != nil {
			return err
		}
	}

	// 5. Seed Default System Time Settings
	var timeCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM system_time_settings").Scan(&timeCount); err != nil {
		return err
	}
	if timeCount == 0 {
		_, err := db.Exec(`INSERT INTO system_time_settings (id, timezone, ntp_sync, ntp_server) VALUES
			(1, 'Asia/Bangkok', 1, 'pool.ntp.org')`)
		if err != nil {
			return err
		}
	}

	// 5.1 Seed Default System Hostname Settings (from actual OS hostname, never hardcoded)
	var hostnameCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM system_hostname_settings").Scan(&hostnameCount); err != nil {
		return err
	}
	if hostnameCount == 0 {
		defaultHostname, err := os.Hostname()
		if err != nil || defaultHostname == "" {
			defaultHostname = "pigate"
		}
		_, err = db.Exec(`INSERT INTO system_hostname_settings (id, hostname, share_with_dhcp) VALUES (1, ?, 0)`, defaultHostname)
		if err != nil {
			return err
		}
	}

	// 6. Seed Default System Interfaces for mock purposes
	if mockMode {
		var ifaceCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM network_interfaces").Scan(&ifaceCount); err != nil {
			return err
		}
		if ifaceCount == 0 {
			_, err := db.Exec(`INSERT INTO network_interfaces (
				id, name, alias, role, type, subtype, addressing_mode, ip, netmask, gateway, mac_address, admin_access, status, speed,
				mac_mode, real_mac_address, randomized_mac, laa_mac_address, randomize_on_reconnect,
				connected_ssid, wifi_security, failover_enabled, backup_ssid, backup_wifi_password, backup_wifi_security, ip_check_timeout, primary_max_retries, failover_cooldown
			) VALUES 
			(
				'iface-1', 'eth0', 'LAN_Internal', 'LAN', 'ethernet', 'device', 'static', '192.168.1.1', '24', '', 'DC:A6:32:AA:BB:C1', 'PING,HTTP,SSH', 'up', '1000 Mbps',
				'hardware', 'DC:A6:32:AA:BB:C1', NULL, NULL, 0, NULL, NULL, 0, NULL, NULL, 'WPA2', NULL, NULL, NULL
			),
			(
				'iface-2', 'wlan0', 'WAN_WiFi', 'WAN', 'wireless', 'device', 'dhcp', '10.0.0.45', '24', '10.0.0.1', '4E:88:2F:BC:A1:90', 'PING', 'up', '72 Mbps',
				'randomized', 'DC:A6:32:AA:BB:C2', '4E:88:2F:BC:A1:90', '9A:11:22:33:44:55', 1,
				'MyHome_5G', 'WPA2-PSK', 0, 'MyHome_2G', 'backupPassword123', 'WPA2', 15, 3, 60
			)`)
			if err != nil {
				return err
			}
		}
	}

	// 7. Seed Default Static Routes (Only custom or customgateway)
	if mockMode {
		var routeCount int
		if err := db.QueryRow("SELECT COUNT(*) FROM static_routes").Scan(&routeCount); err != nil {
			return err
		}
		if routeCount == 0 {
			_, err := db.Exec(`INSERT INTO static_routes (id, destination, gateway, interface, metric, description, status, type, scope, src, proto) VALUES 
				('route-custom-seed', '8.8.8.8/32', '10.0.0.1', 'wlan0', 100, 'Google DNS Route', 1, 'customgateway', 'global', '', 'static')`)
			if err != nil {
				return err
			}
		}
	}

	// 8. Seed Default System DNS settings
	var dnsCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM system_dns_settings").Scan(&dnsCount); err != nil {
		return err
	}
	if dnsCount == 0 {
		_, err := db.Exec(`INSERT INTO system_dns_settings (id, mode, primary_dns, secondary_dns, local_domain)
			VALUES (1, 'static', '1.1.1.1', '8.8.8.8', 'pigate.local')`)
		if err != nil {
			return err
		}
	}

	// 9. Seed Default DNS Server settings (no interfaces selected until user configures)
	var dnsServerSettingsCount int
	if err := db.QueryRow("SELECT COUNT(*) FROM dns_server_settings").Scan(&dnsServerSettingsCount); err != nil {
		return err
	}
	if dnsServerSettingsCount == 0 {
		if _, err := db.Exec(`INSERT INTO dns_server_settings (id, interfaces) VALUES (1, '')`); err != nil {
			return err
		}
	}

	return nil
}

func backupDatabase(dbPath string) error {
	if dbPath == ":memory:" || dbPath == "" {
		return nil
	}
	// Verify if db file exists
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		// Database doesn't exist yet, no need to backup
		return nil
	}

	timestamp := time.Now().Format("20060102-150405")
	backupPath := fmt.Sprintf("%s.backup-%s", dbPath, timestamp)

	input, err := os.ReadFile(dbPath)
	if err != nil {
		return fmt.Errorf("failed to read database file for backup: %w", err)
	}

	err = os.WriteFile(backupPath, input, 0644)
	if err != nil {
		return fmt.Errorf("failed to write database backup file: %w", err)
	}

	log.Printf("[Backup] Database backed up successfully to %s", backupPath)
	return nil
}
