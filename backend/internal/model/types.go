package model

import (
	"fmt"
	"strconv"
	"strings"
	"time"
)

// User roles. super_admin can do everything (including managing other users);
// admin_readonly can view every page but cannot perform any mutation.
const (
	RoleSuperAdmin    = "super_admin"
	RoleAdminReadonly = "admin_readonly"
)

// User account statuses.
const (
	StatusActive   = "active"
	StatusDisabled = "disabled"
)

// User represents dashboard administrator login credentials
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsInitial    bool      `json:"isInitial"`
	Role         string    `json:"role"`
	Status       string    `json:"status"`
	CreatedAt    time.Time `json:"createdAt"`
}

// CreateUserRequest represents fields to create a new user (super_admin only).
type CreateUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
}

// UpdateUserRequest represents fields to update a user. Password is optional:
// when present (non-nil) it resets the target user's password and forces a
// change on next login. Username is immutable and therefore not included.
type UpdateUserRequest struct {
	Role     string  `json:"role"`
	Password *string `json:"password"`
}

// LoginRequest represents login input fields
type LoginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// LoginResponse represents the login result. The session token is delivered
// only via the HttpOnly Set-Cookie header (never in this body) so that XSS in
// the SPA cannot read it — see docs/ref/complete/cookie-only-session-auth-plan.md.
type LoginResponse struct {
	MustChangePassword bool   `json:"mustChangePassword"`
	Role               string `json:"role"`
}

// ChangePasswordRequest represents input fields to update admin password
type ChangePasswordRequest struct {
	CurrentPassword string `json:"currentPassword"`
	NewPassword     string `json:"newPassword"`
}

// AddressObject represents IP/Subnet definitions
type AddressObject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Type string `json:"type"` // "subnet", "range", "fqdn"
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Value       string   `json:"value"`
	System      bool     `json:"system"`
	RefPolicies []string `json:"refPolicies"`
	// Entries holds the multi-value entries for this address object. Additive
	// field — see docs/ref/todo/multi-value-address-service-objects-plan.md
	// Caution 1: MUST keep omitempty on every new field here, otherwise old
	// backup files (encoded before this field existed) will fail checksum
	// verification and become un-importable.
	Entries []AddressEntry `json:"entries,omitempty"`
}

// AddressObjectInput represents fields to create or update an AddressObject
type AddressObjectInput struct {
	Name string `json:"name"`
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Type string `json:"type"` // "subnet", "range", "fqdn"
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
	// Entries holds the multi-value entries for this address object. Additive
	// field — see docs/ref/todo/multi-value-address-service-objects-plan.md
	// Caution 1: MUST keep omitempty on every new field here, otherwise old
	// backup files (encoded before this field existed) will fail checksum
	// verification and become un-importable.
	Entries []AddressEntry `json:"entries,omitempty"`
}

// ServiceObject represents firewall port definitions
type ServiceObject struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Protocol string `json:"protocol"` // "TCP", "UDP", "TCP/UDP", "ICMP"
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Port        string   `json:"port"`
	Type        string   `json:"type"` // "system", "custom"
	RefPolicies []string `json:"refPolicies"`
	// Entries holds the multi-value entries for this service object. Additive
	// field — see docs/ref/todo/multi-value-address-service-objects-plan.md
	// Caution 1: MUST keep omitempty on every new field here, otherwise old
	// backup files (encoded before this field existed) will fail checksum
	// verification and become un-importable.
	Entries []ServiceEntry `json:"entries,omitempty"`
}

// ServiceObjectInput represents fields to create or update a ServiceObject
type ServiceObjectInput struct {
	Name string `json:"name"`
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Protocol string `json:"protocol"` // "TCP", "UDP", "TCP/UDP", "ICMP"
	// Deprecated: compat mirror of Entries[0] only. Kept temporarily for old
	// clients and to allow downgrading the binary (SQLite versions in the
	// field do not support DROP COLUMN easily and the column is NOT NULL
	// with a CHECK constraint) — it will be removed in the next major
	// version. Must never be used to generate firewall rules.
	Port    string `json:"port"`
	Comment string `json:"comment,omitempty"`
	// Entries holds the multi-value entries for this service object. Additive
	// field — see docs/ref/todo/multi-value-address-service-objects-plan.md
	// Caution 1: MUST keep omitempty on every new field here, otherwise old
	// backup files (encoded before this field existed) will fail checksum
	// verification and become un-importable.
	Entries []ServiceEntry `json:"entries,omitempty"`
}

// NormalizeAddressObject keeps the legacy Type/Value fields and the new
// Entries slice in sync (see the Deprecated notes on AddressObject). It is
// nil-safe and idempotent:
//   - if Entries is empty but legacy Type/Value is set, Entries is populated
//     with a single entry mirroring the legacy value;
//   - if Entries is non-empty, the legacy Type/Value fields are always
//     overwritten from Entries[0] (Entries is the source of truth once set).
func NormalizeAddressObject(a *AddressObject) {
	if a == nil {
		return
	}
	if len(a.Entries) == 0 {
		if a.Type != "" || a.Value != "" {
			a.Entries = []AddressEntry{{Type: a.Type, Value: a.Value}}
		}
		return
	}
	a.Type = a.Entries[0].Type
	a.Value = a.Entries[0].Value
}

// NormalizeAddressObjectInput is the *AddressObjectInput counterpart of
// NormalizeAddressObject, used by handlers before persisting a create/update
// request. See NormalizeAddressObject for the sync rules.
func NormalizeAddressObjectInput(a *AddressObjectInput) {
	if a == nil {
		return
	}
	if len(a.Entries) == 0 {
		if a.Type != "" || a.Value != "" {
			a.Entries = []AddressEntry{{Type: a.Type, Value: a.Value}}
		}
		return
	}
	a.Type = a.Entries[0].Type
	a.Value = a.Entries[0].Value
}

// NormalizeServiceObject keeps the legacy Protocol/Port fields and the new
// Entries slice in sync (see the Deprecated notes on ServiceObject). It is
// nil-safe and idempotent:
//   - if Entries is empty but legacy Protocol/Port is set, Entries is
//     populated with a single entry mirroring the legacy value;
//   - if Entries is non-empty, the legacy Protocol/Port fields are always
//     overwritten from Entries[0] (Entries is the source of truth once set).
func NormalizeServiceObject(s *ServiceObject) {
	if s == nil {
		return
	}
	if len(s.Entries) == 0 {
		if s.Protocol != "" || s.Port != "" {
			s.Entries = []ServiceEntry{{Protocol: s.Protocol, Port: s.Port}}
		}
		return
	}
	s.Protocol = s.Entries[0].Protocol
	s.Port = s.Entries[0].Port
}

// NormalizeServiceObjectInput is the *ServiceObjectInput counterpart of
// NormalizeServiceObject, used by handlers before persisting a create/update
// request. See NormalizeServiceObject for the sync rules.
func NormalizeServiceObjectInput(s *ServiceObjectInput) {
	if s == nil {
		return
	}
	if len(s.Entries) == 0 {
		if s.Protocol != "" || s.Port != "" {
			s.Entries = []ServiceEntry{{Protocol: s.Protocol, Port: s.Port}}
		}
		return
	}
	s.Protocol = s.Entries[0].Protocol
	s.Port = s.Entries[0].Port
}

// Policy chain identifiers — which nftables base chain a PolicyRule targets.
// See docs/ref/todo/input-output-chain-firewall-plan.md for the full design.
const (
	PolicyChainForward = "forward"
	PolicyChainInput   = "input"
	PolicyChainOutput  = "output"
)

// NormalizePolicyChain returns c unchanged if it is a known chain, otherwise
// falls back to "forward" for backward compatibility with rows/clients that
// predate the chain field (empty string, old DB rows, old API clients).
func NormalizePolicyChain(c string) string {
	switch c {
	case PolicyChainForward, PolicyChainInput, PolicyChainOutput:
		return c
	default:
		return PolicyChainForward
	}
}

// PolicyRule represents a single nftables rule definition
type PolicyRule struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Chain        string   `json:"chain"` // "forward" (default), "input", "output"
	InInterface  string   `json:"inInterface"`
	OutInterface string   `json:"outInterface"`
	Source       []string `json:"source"`
	Destination  []string `json:"destination"`
	Service      []string `json:"service"`
	Action       string   `json:"action"` // "ACCEPT", "DROP"
	Log          bool     `json:"log"`
	Nat          bool     `json:"nat"`    // Source NAT (masquerade to outgoing interface address)
	Status       bool     `json:"status"` // Enabled/Disabled
	Priority     int      `json:"-"`      // Ordering precedence
	// Monitored opts this rule into persisted traffic counters (bytes/packets)
	// that accumulate in SQLite across ApplyRules/restarts instead of
	// resetting on every apply, unlike the ephemeral "since last apply"
	// counters (docs/ref/todo/fqdn-retry-and-monitored-counters-plan.md D-6,
	// issue #141). An independent flag — no interaction with chain/action.
	// `omitempty` is deliberate here (unlike Log/Nat/Status above, which
	// predate the backup-checksum feature): PolicyRule is embedded in
	// model.BackupConfig.Policies, and BackupService's import re-marshals
	// the decoded config to verify its checksum BEFORE any field
	// normalization runs (see backup.go's decodeBackup Caution 1) — a
	// pre-#141 backup file has no "monitored" key at all, so omitting it
	// when false (the zero value every such file decodes to) keeps the
	// re-marshaled bytes identical to the original, exactly like
	// AddressObject.Entries/ServiceObject.Entries above.
	Monitored bool `json:"monitored,omitempty"`
}

// PolicyRuleInput represents input parameters to create or edit a rule
type PolicyRuleInput struct {
	Name         string   `json:"name"`
	Chain        string   `json:"chain"` // "forward" (default), "input", "output"
	InInterface  string   `json:"inInterface"`
	OutInterface string   `json:"outInterface"`
	Source       []string `json:"source"`
	Destination  []string `json:"destination"`
	Service      []string `json:"service"`
	Action       string   `json:"action"` // "ACCEPT", "DROP"
	Log          bool     `json:"log"`
	Nat          bool     `json:"nat"` // Source NAT (masquerade to outgoing interface address)
	Status       bool     `json:"status"`
	// Monitored — see PolicyRule.Monitored doc comment above.
	Monitored bool `json:"monitored"`
}

// PortForward represents a DNAT / port-forward entry (FortiGate VIP style).
// Traffic hitting InInterface's local address on ExternalPort (proto Protocol)
// is translated to InternalIP:InternalPort. See docs/ref/complete port-forward
// design and kernel/real_firewall.go for the prerouting DNAT + auto forward-accept.
type PortForward struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	InInterface  string `json:"inInterface"`  // external interface, e.g. "eth0"
	ExternalPort string `json:"externalPort"` // single ("8080") or range ("8000-8010", keep-port only)
	Protocol     string `json:"protocol"`     // "tcp" | "udp"
	InternalIP   string `json:"internalIP"`   // LAN target IPv4
	InternalPort string `json:"internalPort"` // single port; empty => keep original port (required for ranges)
	Status       bool   `json:"status"`       // Enabled/Disabled
}

// PortForwardInput represents input parameters to create or edit a port-forward.
type PortForwardInput struct {
	Name         string `json:"name"`
	InInterface  string `json:"inInterface"`
	ExternalPort string `json:"externalPort"`
	Protocol     string `json:"protocol"`
	InternalIP   string `json:"internalIP"`
	InternalPort string `json:"internalPort"`
	Status       bool   `json:"status"`
}

// NetworkInterface represents hardware or virtual network cards configuration
type NetworkInterface struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`    // e.g. "eth0", "wlan0"
	Alias          string   `json:"alias"`   // e.g. "LAN_Internal"
	Role           string   `json:"role"`    // "LAN", "WAN"
	Type           string   `json:"type"`    // "ethernet", "wireless"
	Subtype        string   `json:"subtype"` // e.g. "device", "veth", "bridge", "vlan"
	AddressingMode string   `json:"addressingMode"`
	IP             string   `json:"ip"`
	Netmask        string   `json:"netmask"`
	Gateway        string   `json:"gateway"`
	Metric         *int     `json:"metric,omitempty"` // nil = auto (static: 100, dhcp: dhcpcd default); sets default-route priority for WAN failover
	MacAddress     string   `json:"macAddress"`
	AdminAccess    []string `json:"adminAccess"` // PING, HTTP, HTTPS, SSH
	Status         string   `json:"status"`      // "up", "down"
	Managed        bool     `json:"managed"`     // true = has a config row in DB (pigate has configured it); computed, not persisted
	Speed          string   `json:"speed"`       // e.g. "1000 Mbps"

	// VLAN (802.1Q) sub-interface fields. Non-nil only for rows with Subtype == "vlan".
	// Immutable after creation (changing VLAN ID/parent means delete + recreate).
	VlanParent *string `json:"vlanParent,omitempty"` // parent interface name, e.g. "eth0"
	VlanID     *int    `json:"vlanId,omitempty"`     // 802.1Q VLAN ID, 1–4094

	WifiSSID             *string `json:"wifiSSID,omitempty"`
	WifiPassword         *string `json:"wifiPassword,omitempty"`
	WifiSecurity         *string `json:"wifiSecurity,omitempty"`
	MacMode              *string `json:"macMode,omitempty"`    // "hardware", "randomized", "laa"
	Prefer5GHz           *bool   `json:"prefer5GHz,omitempty"` // true = restrict radio to 5GHz channels (freq_list) in wpa_supplicant config
	RealMacAddress       *string `json:"realMacAddress,omitempty"`
	RandomizedMac        *string `json:"randomizedMac,omitempty"`
	LaaMacAddress        *string `json:"laaMacAddress,omitempty"`
	RandomizeOnReconnect *bool   `json:"randomizeOnReconnect,omitempty"`
	FailoverEnabled      *bool   `json:"failoverEnabled,omitempty"`
	BackupSSID           *string `json:"backupSsid,omitempty"`
	BackupWifiPassword   *string `json:"backupWifiPassword,omitempty"`
	BackupWifiSecurity   *string `json:"backupWifiSecurity,omitempty"`
	IPCheckTimeout       *int    `json:"ipCheckTimeout,omitempty"`
	PrimaryMaxRetries    *int    `json:"primaryMaxRetries,omitempty"`
	FailoverCooldown     *int    `json:"failoverCooldown,omitempty"`
}

// CreateVlanInput represents input parameters to create an 802.1Q VLAN sub-interface.
// The resulting link name is always "<Parent>.<VlanID>" (e.g. "eth0.100").
type CreateVlanInput struct {
	Parent         string   `json:"parent"`         // parent interface name (must be an existing ethernet, non-vlan)
	VlanID         int      `json:"vlanId"`         // 802.1Q VLAN ID, 1–4094
	Alias          string   `json:"alias"`          // display alias
	Role           string   `json:"role"`           // "LAN" | "WAN"
	AddressingMode string   `json:"addressingMode"` // "dhcp" | "static"
	IP             string   `json:"ip"`             // static IP (when static)
	Netmask        string   `json:"netmask"`        // CIDR prefix length (when static)
	Gateway        string   `json:"gateway"`        // gateway (optional, when static)
	AdminAccess    []string `json:"adminAccess"`    // PING, HTTP, HTTPS, SSH
}

// WifiScanResult represents SSID scanner results
type WifiScanResult struct {
	SSID      string `json:"ssid"`
	Signal    int    `json:"signal"` // 0-100
	Security  string `json:"security"`
	Channel   int    `json:"channel"`
	Frequency string `json:"frequency"` // "2.4 GHz" or "5 GHz"
}

// WifiConnectionStatus represents the current real-time state of a Wi-Fi connection
type WifiConnectionStatus struct {
	State     string `json:"state"`     // e.g. "COMPLETED", "DISCONNECTED", "SCANNING", etc.
	SSID      string `json:"ssid"`      // Connected network name
	BSSID     string `json:"bssid"`     // MAC address of the connected AP
	ActiveMac string `json:"activeMac"` // The currently active/effective MAC address of the interface
	Freq      int    `json:"freq"`      // Frequency in MHz (e.g. 5180)
	KeyMgmt   string `json:"keyMgmt"`   // Security protocol (e.g. "WPA3", "WPA2", "Open")
	WifiGen   string `json:"wifiGen"`   // WiFi Generation (e.g. "WiFi 6", "WiFi 5", "WiFi 4")
}

// StaticRoute represents a gateway route configuration
type StaticRoute struct {
	ID          string `json:"id"`
	Destination string `json:"destination"` // e.g. "192.168.10.0/24"
	Gateway     string `json:"gateway"`     // empty if direct
	Interface   string `json:"interface"`   // e.g. "eth0"
	Metric      int    `json:"metric"`
	Description string `json:"description"`
	Status      bool   `json:"status"` // Active/Inactive
	Type        string `json:"type"`   // "system", "custom", "defaultgateway"
	Scope       string `json:"scope"`  // "global", "link", "host", "site", etc.
	Src         string `json:"src"`    // preferred source IP
	Proto       string `json:"proto"`  // "kernel", "boot", "static", "120", etc.
	KernelOnly  bool   `json:"kernelOnly"`
}

// StaticRouteInput represents inputs to create or update a StaticRoute
type StaticRouteInput struct {
	Destination string `json:"destination"`
	Gateway     string `json:"gateway"`
	Interface   string `json:"interface"`
	Metric      int    `json:"metric"`
	Description string `json:"description"`
	Status      bool   `json:"status"`
	Scope       string `json:"scope"`
	Src         string `json:"src"`
	Proto       string `json:"proto"`
}

// DhcpConfig represents DHCP server main settings
type DhcpConfig struct {
	ID        string `json:"id"`
	Enabled   bool   `json:"enabled"`
	Interface string `json:"interface"`
	StartIP   string `json:"startIp"`
	EndIP     string `json:"endIp"`
	Gateway   string `json:"gateway"`
	Netmask   string `json:"netmask"`
	DNS1      string `json:"dns1"`
	DNS2      string `json:"dns2"`
	LeaseTime int    `json:"leaseTime"`
	Domain    string `json:"domain"`
}

// DhcpReservation represents MAC to reserved IP bindings
type DhcpReservation struct {
	ID         string `json:"id"`
	DeviceName string `json:"deviceName"`
	MacAddress string `json:"macAddress"`
	IPAddress  string `json:"ipAddress"`
}

// DhcpReservationInput represents input to add or edit a reservation
type DhcpReservationInput struct {
	DeviceName string `json:"deviceName"`
	MacAddress string `json:"macAddress"`
	IPAddress  string `json:"ipAddress"`
}

// ActiveDhcpLease represents a live DHCP lease log parsed mapping
type ActiveDhcpLease struct {
	ID         string `json:"id"`
	IPAddress  string `json:"ipAddress"`
	MacAddress string `json:"macAddress"`
	Hostname   string `json:"hostname"`
	Interface  string `json:"interface"`
	ExpiresIn  string `json:"expiresIn"`
	ExpiresAt  string `json:"expiresAt"`
}

// TimeStatus carries live time state read from the kernel (systemd-timedated),
// as opposed to the DB-persisted configuration. It is read-only from the API's
// perspective and never written back to the DB.
type TimeStatus struct {
	CurrentTime     string `json:"currentTime"`     // RFC3339, device local time
	NTPSynchronized bool   `json:"ntpSynchronized"` // true once timesyncd has synced
}

// SystemTimeSettings represents NTP and timezone configurations.
// Status is populated only on GET (live kernel state); PUT callers omit it.
type SystemTimeSettings struct {
	Timezone  string      `json:"timezone"`
	NTPSync   bool        `json:"ntpSync"`
	NTPServer string      `json:"ntpServer"`
	Status    *TimeStatus `json:"status,omitempty"`
}

// SystemHostnameSettings represents device hostname and DHCP-client hostname sharing
type SystemHostnameSettings struct {
	Hostname      string `json:"hostname"`
	ShareWithDhcp bool   `json:"shareWithDhcp"`
}

// DhcpHealthSettings holds the tunable thresholds for the DHCP link-local
// (169.254.x.x APIPA) fallback health-checker (issue #78). It controls how
// aggressively the background checker self-heals interfaces in DHCP mode
// that are carrier-ready (isUp && isRunning) but stuck without a real IPv4
// address, via a mix of stray-address deletion and dhcpcd restarts.
type DhcpHealthSettings struct {
	Enabled                bool `json:"enabled"`
	CheckIntervalSeconds   int  `json:"checkIntervalSeconds"`
	ConsecutiveStrikes     int  `json:"consecutiveStrikes"`
	MinRunningSeconds      int  `json:"minRunningSeconds"`
	RestartBackoffSeconds  int  `json:"restartBackoffSeconds"`
	MaxRestartsBeforePause int  `json:"maxRestartsBeforePause"`
}

// ServiceRuntimeState carries the live systemd unit state read via D-Bus
// (org.freedesktop.systemd1.Unit's ActiveState + LoadState properties). It is
// the kernel layer's raw return value; the service layer maps it into the
// simpler display status used by NetworkServiceStatus.Status.
type ServiceRuntimeState struct {
	ActiveState string // systemd ActiveState, e.g. "active", "inactive", "failed"
	Loaded      bool   // true when LoadState == "loaded" (unit file exists)
}

// NetworkServiceStatus represents critical host systemd service status
type NetworkServiceStatus struct {
	ID             string `json:"id"`
	Name           string `json:"name"` // Human-readable
	ServiceName    string `json:"serviceName"`
	Status         string `json:"status"`         // "running", "stopped", "failed", "unavailable"
	RestartAllowed bool   `json:"restartAllowed"` // false = read-only entry (e.g. pigate.service itself), no restart button
}

// FirewallLog represents live packet filter block logs
type FirewallLog struct {
	ID       string `json:"id"`
	Time     string `json:"time"`
	Action   string `json:"action"` // "PASS", "DROP"
	Src      string `json:"src"`
	Dest     string `json:"dest"`
	SrcPort  string `json:"srcPort"`
	Port     string `json:"port"` // destination port
	Proto    string `json:"proto"`
	InIface  string `json:"inIface"`  // ingress interface name (NFLOG indev), "-" if unknown
	OutIface string `json:"outIface"` // egress interface name (NFLOG outdev), "-" if unknown
	Reason   string `json:"reason"`
	// Chain identifies which nftables chain produced this entry: one of
	// PolicyChainForward/PolicyChainInput/PolicyChainOutput (see constants
	// above). Empty string means "unknown" and the UI must tolerate that.
	Chain string `json:"chain"`

	// RuleID/RuleName are captured once at write time ("snapshot-on-write"):
	// resolved from the nftables log prefix at the moment the log entry is
	// created (see cmd/pigate/main.go stampAndPush) and never updated again.
	// If the matching policy rule is later renamed or deleted, entries
	// already in the ring buffer keep showing the name as it was at match
	// time. Both fields are empty for structural log points (e.g. default
	// drop, anti-spoof) that are not tied to a single user-configured rule,
	// or when the rule id couldn't be resolved yet (before the first
	// rule-name snapshot refresh).
	RuleID   string `json:"ruleId,omitempty"`
	RuleName string `json:"ruleName,omitempty"`

	// SrcDomain/DestDomain/SrcHostname/DestHostname are enrich-on-read: the
	// API layer resolves them fresh from the DNS query cache (domain) and
	// DHCP lease table (hostname) every time a log entry is served, they are
	// never persisted in the ring buffer. This preserves the existing
	// privacy contract - turning off DNS Query Logging clears domain names
	// immediately, including for already-buffered log entries.
	SrcDomain    string `json:"srcDomain,omitempty"`
	DestDomain   string `json:"destDomain,omitempty"`
	SrcHostname  string `json:"srcHostname,omitempty"`
	DestHostname string `json:"destHostname,omitempty"`
}

// SystemEvent is a single audit/event log entry, persisted to SQLite via the
// EventLogService batch writer (never written row-by-row — SD card wear).
type SystemEvent struct {
	ID       int64  `json:"id"`
	Time     string `json:"time"`     // RFC3339 UTC
	Category string `json:"category"` // see EventCategory* constants
	Action   string `json:"action"`   // e.g. "login.failed", "dhcp.lease.add"
	Severity string `json:"severity"` // see EventSeverity* constants
	Actor    string `json:"actor"`    // username or "system"
	Target   string `json:"target"`   // affected object (user/interface/policy name)
	Message  string `json:"message"`  // human-readable message for the UI
}

// SystemEvent categories
const (
	EventCategoryAuth     = "auth"
	EventCategoryUser     = "user"
	EventCategoryNetwork  = "network"
	EventCategoryFirewall = "firewall"
	EventCategoryRoute    = "route"
	EventCategoryDhcp     = "dhcp"
	EventCategoryDns      = "dns"
	EventCategoryQos      = "qos"
	EventCategorySystem   = "system"
	EventCategoryConfig   = "config"
)

// SystemEvent severities
const (
	EventSeverityInfo     = "info"
	EventSeverityWarning  = "warning"
	EventSeverityError    = "error"
	EventSeverityCritical = "critical"
)

// EventActorSystem is the actor recorded for events not initiated by a logged-in user.
const EventActorSystem = "system"

// DashboardStats represents widgets counters. TotalTraffic{In,Out}Bytes are the
// cumulative rx/tx byte totals observed since boot (RAM-only, reset on reboot);
// the frontend formats them for display.
type DashboardStats struct {
	FirewallStatus       string `json:"firewallStatus"`
	TotalTrafficInBytes  uint64 `json:"totalTrafficInBytes"`
	TotalTrafficOutBytes uint64 `json:"totalTrafficOutBytes"`
	DhcpLeasesCount      int    `json:"dhcpLeasesCount"`
	WifiStatus           string `json:"wifiStatus"`
	WifiSSID             string `json:"wifiSSID"`
}

// =============================================================================
// System Status / Telemetry Types (Dashboard)
//
// These back the real System Information / System Status widgets. All values
// are read-only host telemetry (/proc, /sys, statfs, netlink counters) — no
// method that produces them ever mutates system state. Fields flagged as
// "optional/available" degrade gracefully on environments (WSL, x86) that lack
// the relevant sysfs node, rather than failing the whole response.
// =============================================================================

// CPUSnapshot holds raw cumulative CPU jiffies from the aggregate "cpu" line of
// /proc/stat. Usage% is derived by the service layer from the delta between two
// snapshots taken a few seconds apart (a single snapshot has no meaning).
type CPUSnapshot struct {
	Idle  uint64 // idle + iowait jiffies
	Total uint64 // sum of all jiffie fields (user, nice, system, idle, iowait, ...)
}

// CPUInfo describes static CPU identity plus the current scaling frequency.
// FreqAvailable is false when /sys cpufreq is absent (common on WSL / VMs).
type CPUInfo struct {
	Cores         int     `json:"cores"`
	ModelName     string  `json:"modelName"`
	FreqMHz       float64 `json:"freqMhz"`
	FreqAvailable bool    `json:"freqAvailable"`
}

// MemoryInfo is RAM usage derived from /proc/meminfo (used = total - available).
// Its JSON shape doubles as the `memDetail` object in the performance response.
type MemoryInfo struct {
	UsedBytes  uint64  `json:"usedBytes"`
	TotalBytes uint64  `json:"totalBytes"`
	Percent    float64 `json:"percent"`
}

// TemperatureInfo is the SoC temperature. Available=false when no thermal zone
// exists (WSL / x86 dev boxes); Celsius is meaningless in that case.
type TemperatureInfo struct {
	Celsius         float64 `json:"celsius"`
	ThrottleCelsius float64 `json:"throttleCelsius"`
	Available       bool    `json:"available"`
}

// DiskUsage is filesystem usage for a mount path (from unix.Statfs). Its JSON
// shape doubles as the `storage` object in the performance response.
type DiskUsage struct {
	Path       string  `json:"path"`
	UsedBytes  uint64  `json:"usedBytes"`
	TotalBytes uint64  `json:"totalBytes"`
	Percent    float64 `json:"percent"`
}

// HostInfo carries OS / board / kernel / uptime identity. BoardModel and
// KernelVersion are best-effort; an empty string means "unavailable" and the
// API omits the corresponding field.
type HostInfo struct {
	OSName        string
	BoardModel    string
	KernelVersion string
	UptimeSeconds int64
}

// NetCounters holds cumulative rx/tx byte counters for one interface, read from
// netlink LinkAttrs.Statistics.
type NetCounters struct {
	RxBytes uint64
	TxBytes uint64
}

// CPUDetail is the `cpuDetail` object in the performance response: static CPU
// identity plus the live usage percentage computed by the sampler.
type CPUDetail struct {
	UsagePercent  float64 `json:"usagePercent"`
	Cores         int     `json:"cores"`
	ModelName     string  `json:"modelName"`
	FreqMHz       float64 `json:"freqMhz"`
	FreqAvailable bool    `json:"freqAvailable"`
}

// SystemMetrics is the /api/dashboard/performance response. The flat cpu/memory/
// temp fields are retained for backward-compatibility with the current
// dashboardService.ts; the *Detail objects carry the richer new data.
type SystemMetrics struct {
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	Temp   float64 `json:"temp"`

	CPUDetail  CPUDetail       `json:"cpuDetail"`
	MemDetail  MemoryInfo      `json:"memDetail"`
	TempDetail TemperatureInfo `json:"tempDetail"`
	Storage    DiskUsage       `json:"storage"`
	Sessions   SessionCounts   `json:"sessions"`
}

// SessionCounts is the conntrack session snapshot shared by both the SSE
// /dashboard/performance/stream push and the /dashboard/traffic-detail
// response. Total/Max/Available come straight from
// /proc/sys/net/netfilter/nf_conntrack_count|max (fresh, ~5s cadence — the
// session sampler ticker). TCP/UDP/ICMP/Other are counted from the conntrack
// dump the traffic-accounting poller already does every 10s, so they can lag
// Total by up to one poll cycle and are capped at kernel.MaxFlowsPerDump
// (ProtoCapped=true when the dump hit that cap) — do NOT stack the per-proto
// counts on top of Total in the same chart line, they are different
// freshness/sources and will not sum cleanly.
type SessionCounts struct {
	Total          int    `json:"total"`
	TCP            int    `json:"tcp"`
	UDP            int    `json:"udp"`
	ICMP           int    `json:"icmp"`
	Other          int    `json:"other"`
	Max            int    `json:"max"`
	Available      bool   `json:"available"`
	ProtoSampledAt string `json:"protoSampledAt"` // RFC3339 UTC; "" if never sampled yet
	ProtoCapped    bool   `json:"protoCapped"`
}

// SessionHistoryPoint is one sample in the RAM-only Active Sessions ring
// buffer (see TrafficStatsService session sampler) used to seed the
// Dashboard "Active Sessions" chart. Total-only line data — TCP/UDP/ICMP/
// Other are carried for the badges, not for stacking into the chart.
type SessionHistoryPoint struct {
	T     string `json:"t"` // RFC3339 UTC
	Total int    `json:"total"`
	TCP   int    `json:"tcp"`
	UDP   int    `json:"udp"`
	ICMP  int    `json:"icmp"`
	Other int    `json:"other"`
}

// SystemInfo is the /api/system/info response (System Information card).
// BoardModel/KernelVersion are omitted when unreadable (e.g. WSL).
type SystemInfo struct {
	Hostname      string `json:"hostname"`
	Version       string `json:"version"`
	OSName        string `json:"osName"`
	BoardModel    string `json:"boardModel,omitempty"`
	KernelVersion string `json:"kernelVersion,omitempty"`
	UptimeSeconds int64  `json:"uptimeSeconds"`
	SystemTime    string `json:"systemTime"`
	Timezone      string `json:"timezone"`
}

// TrafficBucket is one time-bucketed rx/tx delta in the bandwidth history.
type TrafficBucket struct {
	Ts      string `json:"ts"` // RFC3339 bucket start (device local time)
	RxBytes uint64 `json:"rxBytes"`
	TxBytes uint64 `json:"txBytes"`
}

// TrafficHistory is the /api/dashboard/traffic response.
type TrafficHistory struct {
	Interfaces []string        `json:"interfaces"`
	Buckets    []TrafficBucket `json:"buckets"`
}

// FlowSample is one conntrack flow observed by kernel.TrafficAccountingManager.
// DumpFlows (docs/ref/todo/dashboard-traffic-detail-plan.md §2.1). SrcIP is the
// pre-NAT LAN-side source (Forward tuple), so it can be used directly as the
// Top Talkers key. Key deliberately excludes the flow's start time (see
// flowKeyFromParts and traffic-accounting-accuracy-phase2-plan.md Caution 2):
// TimeStart is only populated when net.netfilter.nf_conntrack_timestamp is on,
// and the poll and DESTROY-event paths must derive the same key regardless of
// that sysctl, or a mismatch silently double-counts every flow.
type FlowSample struct {
	Key     string // hash of (family, proto, srcIP, srcPort, dstIP, dstPort)
	SrcIP   string
	DstIP   string
	Proto   uint8
	DstPort uint16
	// BytesOrig is the SrcIP -> DstIP direction (conntrack Forward tuple /
	// CTA_COUNTERS_ORIG), always pre-NAT relative to SrcIP above.
	BytesOrig uint64
	// BytesReply is the DstIP -> SrcIP direction (conntrack Reverse tuple /
	// CTA_COUNTERS_REPLY).
	BytesReply uint64
}

// TotalBytes returns the combined byte count of both directions.
func (f FlowSample) TotalBytes() uint64 { return f.BytesOrig + f.BytesReply }

// RuleCounter is the bytes/packets nftables has counted for one DB policy
// rule id, summed across every nft rule expansion that carries that id in its
// UserData comment (see real_firewall.go applyUserRules / plan §2.3).
type RuleCounter struct {
	Bytes   uint64
	Packets uint64
}

// MonitoredCounter is one rule's persisted, opt-in traffic counter (docs/
// ref/todo/fqdn-retry-and-monitored-counters-plan.md D-5/D-6, issue #141).
// Unlike RuleCounter (ephemeral, resets on every ApplyRules), this
// accumulates across applies and process restarts until the user explicitly
// resets it or turns Monitor off (which deletes the row). StartedAt/
// UpdatedAt are RFC3339 UTC timestamp strings.
type MonitoredCounter struct {
	RuleID    string
	Bytes     uint64
	Packets   uint64
	StartedAt string
	UpdatedAt string
	// EndpointsEvicted is the cumulative number of policy_rule_endpoints rows
	// evicted (LRU) for this rule since Monitor was last turned on/reset
	// (docs/ref/todo/persisted-rule-endpoints-plan.md E-D3/E-D4, issue #141
	// follow-up) — mirrors the policy_rule_counters.endpoints_evicted column.
	EndpointsEvicted uint64
}

// Endpoint direction constants shared by the recorder (service layer),
// repository (db layer) and model (this file) for policy_rule_endpoints —
// see docs/ref/todo/persisted-rule-endpoints-plan.md E-D3, issue #141
// follow-up. Using these constants everywhere (instead of scattering the
// literal strings "src"/"dst"/"svc" across layers) keeps the CHECK
// constraint in db/connection.go and Go code in sync.
const (
	EndpointDirectionSrc = "src"
	EndpointDirectionDst = "dst"
	EndpointDirectionSvc = "svc"
)

// PersistedEndpoint is one row (existing or pending-in-RAM) of
// policy_rule_endpoints — docs/ref/todo/persisted-rule-endpoints-plan.md
// E-D3, issue #141 follow-up. Direction is one of the EndpointDirection*
// constants above; Key is an IP literal for src/dst or "PROTO/PORT" for svc.
// FirstSeenAt/LastSeenAt are RFC3339 UTC timestamp strings.
type PersistedEndpoint struct {
	RuleID      string
	Direction   string
	Key         string
	Count       int
	FirstSeenAt string
	LastSeenAt  string
}

// TrafficCategorySlice is one Protocol Breakdown segment — a Service-Object-
// defined category (or "Other") with its share of ObservedBytes.
type TrafficCategorySlice struct {
	Name    string  `json:"name"`
	Bytes   uint64  `json:"bytes"`
	Percent float64 `json:"percent"`
}

// TopTalker is one row of the Top Talkers card: a LAN host ranked by observed
// bytes in the requested window. Hostname/MAC are enriched from DHCP leases/
// reservations; IP alone is used as the fallback display name.
type TopTalker struct {
	IP       string  `json:"ip"`
	Hostname string  `json:"hostname"`
	MAC      string  `json:"mac"`
	Bytes    uint64  `json:"bytes"`
	Percent  float64 `json:"percent"`
}

// TopRule is one row of the Top Rules by Traffic card, sourced from the exact
// nftables rule counter (not an estimate, unlike TrafficCategorySlice/TopTalker).
type TopRule struct {
	RuleID  string  `json:"ruleId"`
	Name    string  `json:"name"`
	Chain   string  `json:"chain"`
	Action  string  `json:"action"`
	Bytes   uint64  `json:"bytes"`
	Packets uint64  `json:"packets"`
	Percent float64 `json:"percent"`
}

// PolicyRuleStat is one row of the /api/policies/stats response — per-rule
// usage since the last successful SyncFirewallRules apply (see
// docs/ref/todo/firewall-policy-rule-usage-stats-plan.md, Design decision 1).
// Deliberately NOT merged into PolicyRule (also used by create/update/backup
// import-export — adding runtime fields there would leak into those paths).
type PolicyRuleStat struct {
	RuleID  string `json:"ruleId"`
	Name    string `json:"name"`
	Chain   string `json:"chain"`
	Action  string `json:"action"`
	Log     bool   `json:"log"`
	Status  bool   `json:"status"`
	Bytes   uint64 `json:"bytes"`
	Packets uint64 `json:"packets"`
	// Percent is always computed against the total across every chain (Design
	// decision 2), regardless of the ?chain= filter applied to the response.
	Percent float64 `json:"percent"`
	// Unused is true when the rule is enabled but has not matched any traffic
	// since the last successful apply (Bytes==0 && Packets==0). Always false
	// for disabled rules — they show "—" client-side instead, not "Unused".
	Unused bool `json:"unused"`
	// LastMatchedAt is an RFC3339(Nano) timestamp string, or "" if unknown
	// (never matched since apply, or the evidence fell out of the ring
	// buffer/poll baseline). See LastMatchedSource for how it was derived.
	LastMatchedAt string `json:"lastMatchedAt,omitempty"`
	// LastMatchedSource is "log" (resolved from a ring-buffer traffic log
	// entry — precise, requires the rule's Log flag to be on), "counter"
	// (fallback: the 10s nft-counter poller observed a delta — ±10s accuracy),
	// or "" when LastMatchedAt is empty.
	LastMatchedSource string `json:"lastMatchedSource,omitempty"`
	// Monitored/MonitoredBytes/MonitoredPackets/MonitoredSince surface the
	// persisted opt-in counter (docs/ref/todo/
	// fqdn-retry-and-monitored-counters-plan.md D-6, issue #141) — a
	// separate accounting from Bytes/Packets/Percent above, which remain
	// "since the last successful apply" only. MonitoredSince is an RFC3339
	// UTC timestamp string ("started_at"), empty when Monitored is false.
	Monitored        bool   `json:"monitored"`
	MonitoredBytes   uint64 `json:"monitoredBytes,omitempty"`
	MonitoredPackets uint64 `json:"monitoredPackets,omitempty"`
	MonitoredSince   string `json:"monitoredSince,omitempty"`
}

// PolicyRuleStats is the full /api/policies/stats response. TotalBytes is the
// sum across every chain (used as the Percent denominator for every rule,
// including when ?chain= filters Rules to one chain — Design decision 2).
// CountersSince is the last successful firewall apply time (RFC3339 UTC, or
// "" before the first apply); nftables counters reset to 0 on every apply
// (Design decision 1), so it doubles as "stats collected since".
type PolicyRuleStats struct {
	Rules         []PolicyRuleStat `json:"rules"`
	TotalBytes    uint64           `json:"totalBytes"`
	TotalPackets  uint64           `json:"totalPackets"`
	CountersSince string           `json:"countersSince,omitempty"`
	// Available is false when the underlying kernel counter dump has never
	// succeeded yet (e.g. very first poll tick after startup) — the UI shows
	// a "not available yet" state instead of a misleading all-zero table.
	Available bool `json:"available"`
}

// EndpointHit is one aggregated IP (source or destination) observed matching
// a policy rule in the traffic-log ring buffer — see
// docs/ref/todo/firewall-rule-matched-endpoints-plan.md. Count is the number
// of log entries (NOT bytes/packets — the NFLOG payload carries no packet
// size). Name resolution precedence for display is AddressName (user-defined
// Address Object) then Domain (DNS reverse cache) then Hostname (DHCP lease) —
// see plan §2 decision 5. FromRule is true when AddressName equals one of the
// rule's own Source/Destination address objects.
type EndpointHit struct {
	IP          string `json:"ip"`
	Count       int    `json:"count"`
	FirstSeenAt string `json:"firstSeenAt"`
	LastSeenAt  string `json:"lastSeenAt"`
	AddressName string `json:"addressName"`
	Hostname    string `json:"hostname"`
	Domain      string `json:"domain"`
	FromRule    bool   `json:"fromRule"`
}

// ServiceHit is one aggregated proto/port pair observed matching a policy
// rule in the traffic-log ring buffer. Count is the number of log entries.
// Port is "-" for ICMP/ICMPv6 (as produced by the NFLOG parser — never
// converted to "0"). FromRule is true when ServiceName equals the rule's own
// Service object (matched loosely, tolerating the trailing-suffix quirk of
// resolveService).
type ServiceHit struct {
	Proto       string `json:"proto"`
	Port        string `json:"port"`
	Count       int    `json:"count"`
	FirstSeenAt string `json:"firstSeenAt"`
	LastSeenAt  string `json:"lastSeenAt"`
	ServiceName string `json:"serviceName"`
	FromRule    bool   `json:"fromRule"`
}

// PolicyRuleEndpoints is the /api/policies/{id}/endpoints response — per-rule
// "who matched this rule" troubleshooting view. Originally derived entirely
// by scanning the in-memory traffic-log ring buffer on request (Option A,
// docs/ref/todo/firewall-rule-matched-endpoints-plan.md §2 decision 1); now
// extended (docs/ref/todo/persisted-rule-endpoints-plan.md E-D6, issue #141
// follow-up) with a second, persisted source of truth for rules that have
// Monitor enabled — see Source below. Hard limitations (surfaced in the UI,
// not hidden):
//  1. Only rules with Log enabled ever have data (LogEnabled=false means the
//     lists are always empty, not an error) — true in BOTH modes (E-D7).
//  2. Counts are log-entry counts, not bytes — NFLOG carries no packet size.
//  3. When Source=="buffer": the data window equals the ring buffer's
//     current capacity/contents; clearing the traffic log makes this data
//     disappear immediately. When Source=="persisted": data survives
//     Apply/restart/clearing the traffic log, but only counts from the
//     moment Monitor was turned on (or last reset) — no backfill from the
//     ring buffer (E-D6).
//  4. Only new connections (ct state new) and DROPped packets are logged, so
//     counts are not a full packet/byte tally — see pages/ForwardTraffic.tsx.
type PolicyRuleEndpoints struct {
	RuleID             string        `json:"ruleId"`
	RuleName           string        `json:"ruleName"`
	Chain              string        `json:"chain"`
	LogEnabled         bool          `json:"logEnabled"`
	MatchedEntries     int           `json:"matchedEntries"`
	UniqueSources      int           `json:"uniqueSources"`
	UniqueDestinations int           `json:"uniqueDestinations"`
	UniqueServices     int           `json:"uniqueServices"`
	Limit              int           `json:"limit"`
	Truncated          bool          `json:"truncated"`
	ScannedEntries     int           `json:"scannedEntries"`
	BufferOldestAt     string        `json:"bufferOldestAt,omitempty"`
	Sources            []EndpointHit `json:"sources"`
	Destinations       []EndpointHit `json:"destinations"`
	Services           []ServiceHit  `json:"services"`

	// Source/CollectingSince/Capped/Evicted/MaxPerDirection are new fields
	// added by docs/ref/todo/persisted-rule-endpoints-plan.md E-D6 (issue
	// #141 follow-up).
	//
	// Source is "persisted" when this response was read from the
	// policy_rule_endpoints SQLite table (rule has Monitor enabled and the
	// feature's kill switch is on) or "buffer" for the original ring-buffer
	// scan behavior (unchanged). When Source=="persisted", ScannedEntries is
	// always 0 and BufferOldestAt is always "" — they only have meaning for
	// the buffer path.
	Source string `json:"source"`
	// CollectingSince is an RFC3339 UTC timestamp string ("this rule's
	// Monitor started_at"), set only when Source=="persisted". Data begins
	// counting from zero at this instant — never backfilled from data the
	// ring buffer already had (E-D6).
	CollectingSince string `json:"collectingSince,omitempty"`
	// Capped is true when at least one direction (src/dst/svc) currently
	// holds exactly MaxPerDirection rows, meaning LRU eviction is actively
	// discarding the least-recently-seen entries for that direction.
	Capped bool `json:"capped"`
	// Evicted is the cumulative count of rows evicted for this rule since
	// Monitor was last turned on/reset (policy_rule_counters.endpoints_evicted).
	Evicted int `json:"evicted"`
	// MaxPerDirection is the cap (rows per (rule, direction) pair) in effect
	// — surfaces monitored-endpoints-max-per-rule so the UI never has to
	// hardcode the number (Caution 14 of the plan above).
	MaxPerDirection int `json:"maxPerDirection"`
}

// TrafficDetail is the /api/dashboard/traffic-detail response backing the
// Dashboard "Detailed" tab's Protocol Breakdown / Top Talkers / Top Rules
// cards. ObservedBytes is the total the conntrack-based collector actually
// saw in the window — Categories/TopTalkers percentages are computed against
// this figure, NOT against the WAN total from /dashboard/traffic (Caution 8);
// Estimated is kept for backward compatibility (always true for the
// conntrack-derived figures), independent of TopRules which is exact
// (kernel-counted). Accuracy is the phase-2 refinement of the same signal:
// "estimated" when only the 10s poll is feeding the collector, "near-exact"
// once the conntrack DESTROY event listener (kernel.WatchFlowEnd) is also
// active — see docs/ref/todo/traffic-accounting-accuracy-phase2-plan.md.
type TrafficDetail struct {
	// Window is one of "15m", "30m", "1h", "3h", "6h", "12h", "24h" (docs/ref/
	// todo/statistics-window-granularity-plan.md §0 D-2). An unrecognized
	// value sent to the API (including empty) falls back to "1h" server-side
	// and is never returned here.
	Window         string                 `json:"window"` // "15m" | "30m" | "1h" | "3h" | "6h" | "12h" | "24h"
	ObservedBytes  uint64                 `json:"observedBytes"`
	Estimated      bool                   `json:"estimated"`
	Accuracy       string                 `json:"accuracy"` // "estimated" | "near-exact"
	Categories     []TrafficCategorySlice `json:"categories"`
	TopTalkers     []TopTalker            `json:"topTalkers"`
	TopRules       []TopRule              `json:"topRules"`
	GeneratedAt    string                 `json:"generatedAt"`
	Sessions       SessionCounts          `json:"sessions"`
	SessionHistory []SessionHistoryPoint  `json:"sessionHistory"`
}

// ParsePortSpec parses a "8080" or "8000-8010" port spec into an inclusive
// range (single ports return start==end). This is the single canonical port-
// spec parser shared by the firewall rule builder (kernel/real_firewall.go)
// and the dashboard traffic-detail categorizer (service/traffic_stats.go) —
// do not write a second implementation; the two must never drift (plan
// Caution 13).
func ParsePortSpec(spec string) (start, end int, err error) {
	spec = strings.TrimSpace(spec)
	parts := strings.Split(spec, "-")
	switch len(parts) {
	case 1:
		p, err := strconv.Atoi(strings.TrimSpace(parts[0]))
		if err != nil || p < 1 || p > 65535 {
			return 0, 0, fmt.Errorf("invalid port %q", spec)
		}
		return p, p, nil
	case 2:
		s, err1 := strconv.Atoi(strings.TrimSpace(parts[0]))
		e, err2 := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err1 != nil || err2 != nil || s < 1 || e > 65535 || s > e {
			return 0, 0, fmt.Errorf("invalid port range %q", spec)
		}
		return s, e, nil
	default:
		return 0, 0, fmt.Errorf("invalid port spec %q", spec)
	}
}

// DNSConfig represents system-wide DNS settings
type DNSConfig struct {
	Mode         string             `json:"mode"` // "wan", "static"
	PrimaryDNS   string             `json:"primaryDns"`
	SecondaryDNS string             `json:"secondaryDns"`
	LocalDomain  string             `json:"localDomain"`
	DynamicDNS   []DynamicDNSServer `json:"dynamicDnsServers"`
}

// DNSConfigInput represents payload to update DNS configuration
type DNSConfigInput struct {
	Mode         string `json:"mode"`
	PrimaryDNS   string `json:"primaryDns"`
	SecondaryDNS string `json:"secondaryDns"`
	LocalDomain  string `json:"localDomain"`
}

// DynamicDNSServer represents DNS servers obtained dynamically from WAN interfaces
type DynamicDNSServer struct {
	InterfaceName  string   `json:"interfaceName"`
	InterfaceAlias string   `json:"interfaceAlias"`
	DNSServers     []string `json:"dnsServers"`
}

// =============================================================================
// QoS Types
// =============================================================================

// QosRule represents a bandwidth shaping rule per network interface.
// Phase 1: EgressRateMbps/EgressCeilMbps (Client Download) via HTB Qdisc.
// Phase 2: IngressRateMbps/IngressCeilMbps (Client Upload) via IFB device.
// A value of 0 for Rate/Ceil means unlimited (no shaping applied).
type QosRule struct {
	ID              string `json:"id"`
	Name            string `json:"name"`
	Interface       string `json:"interface"`       // e.g. "eth0"
	MatchSrcIP      string `json:"matchSrcIp"`      // CIDR e.g. "172.24.25.0/24", empty = match all
	MatchDstIP      string `json:"matchDstIp"`      // CIDR e.g. "0.0.0.0/0", empty = match all
	EgressRateMbps  int    `json:"egressRateMbps"`  // Client Download guaranteed rate, 0 = unlimited
	EgressCeilMbps  int    `json:"egressCeilMbps"`  // Client Download burst ceiling, 0 = unlimited
	IngressRateMbps int    `json:"ingressRateMbps"` // Client Upload rate via IFB (Phase 2), 0 = unlimited
	IngressCeilMbps int    `json:"ingressCeilMbps"` // Client Upload burst ceiling via IFB (Phase 2)
	Priority        int    `json:"priority"`        // Filter priority (lower = matched first)
	Status          bool   `json:"status"`          // true = enabled, false = disabled
	Description     string `json:"description"`
}

// QosRuleInput is the create/update payload for QosRule.
type QosRuleInput struct {
	Name            string `json:"name"`
	Interface       string `json:"interface"`
	MatchSrcIP      string `json:"matchSrcIp"`
	MatchDstIP      string `json:"matchDstIp"`
	EgressRateMbps  int    `json:"egressRateMbps"`
	EgressCeilMbps  int    `json:"egressCeilMbps"`
	IngressRateMbps int    `json:"ingressRateMbps"`
	IngressCeilMbps int    `json:"ingressCeilMbps"`
	Priority        int    `json:"priority"`
	Status          bool   `json:"status"`
	Description     string `json:"description"`
}

// QosIfaceStatus represents live kernel qdisc/class state for a network interface.
type QosIfaceStatus struct {
	Interface string     `json:"interface"`
	HasQdisc  bool       `json:"hasQdisc"`
	Classes   []QosClass `json:"classes"`
	// IngressSupported reports whether the kernel has the IFB module available
	// (probed once at startup). When false, ingress (upload) shaping is skipped
	// and only egress (download) QoS is applied.
	IngressSupported bool `json:"ingressSupported"`
}

// QosClass represents a single active HTB class on an interface.
type QosClass struct {
	ClassID  string `json:"classId"`  // e.g. "1:10"
	Rate     string `json:"rate"`     // human-readable e.g. "50Mbit"
	Ceil     string `json:"ceil"`     // human-readable e.g. "100Mbit"
	RuleName string `json:"ruleName"` // matched rule name from DB (may be empty)
}
