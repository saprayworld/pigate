package model

import "time"

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

// LoginResponse represents login token payload
type LoginResponse struct {
	Token              string `json:"token"`
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
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Type        string   `json:"type"` // "subnet", "range", "fqdn"
	Value       string   `json:"value"`
	System      bool     `json:"system"`
	RefPolicies []string `json:"refPolicies"`
}

// AddressObjectInput represents fields to create or update an AddressObject
type AddressObjectInput struct {
	Name    string `json:"name"`
	Type    string `json:"type"` // "subnet", "range", "fqdn"
	Value   string `json:"value"`
	Comment string `json:"comment,omitempty"`
}

// ServiceObject represents firewall port definitions
type ServiceObject struct {
	ID          string   `json:"id"`
	Name        string   `json:"name"`
	Protocol    string   `json:"protocol"` // "TCP", "UDP", "TCP/UDP", "ICMP"
	Port        string   `json:"port"`
	Type        string   `json:"type"` // "system", "custom"
	RefPolicies []string `json:"refPolicies"`
}

// ServiceObjectInput represents fields to create or update a ServiceObject
type ServiceObjectInput struct {
	Name     string `json:"name"`
	Protocol string `json:"protocol"` // "TCP", "UDP", "TCP/UDP", "ICMP"
	Port     string `json:"port"`
	Comment  string `json:"comment,omitempty"`
}

// PolicyRule represents a single nftables rule definition
type PolicyRule struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	InInterface  string   `json:"inInterface"`
	OutInterface string   `json:"outInterface"`
	Source       []string `json:"source"`
	Destination  []string `json:"destination"`
	Service      []string `json:"service"`
	Action       string   `json:"action"` // "ACCEPT", "DROP"
	Log          bool     `json:"log"`
	Status       bool     `json:"status"` // Enabled/Disabled
	Priority     int      `json:"-"`      // Ordering precedence
}

// PolicyRuleInput represents input parameters to create or edit a rule
type PolicyRuleInput struct {
	Name         string   `json:"name"`
	InInterface  string   `json:"inInterface"`
	OutInterface string   `json:"outInterface"`
	Source       []string `json:"source"`
	Destination  []string `json:"destination"`
	Service      []string `json:"service"`
	Action       string   `json:"action"` // "ACCEPT", "DROP"
	Log          bool     `json:"log"`
	Status       bool     `json:"status"`
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
	MacMode              *string `json:"macMode,omitempty"` // "hardware", "randomized", "laa"
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

// NetworkServiceStatus represents critical host systemd service status
type NetworkServiceStatus struct {
	ID          string `json:"id"`
	Name        string `json:"name"` // Human-readable
	ServiceName string `json:"serviceName"`
	Status      string `json:"status"` // "running", "stopped", "failed"
}

// FirewallLog represents live packet filter block logs
type FirewallLog struct {
	ID       string `json:"id"`
	Time     string `json:"time"`
	Action   string `json:"action"` // "PASS", "DROP"
	Src      string `json:"src"`
	Dest     string `json:"dest"`
	Port     string `json:"port"`
	Proto    string `json:"proto"`
	InIface  string `json:"inIface"`  // ingress interface name (NFLOG indev), "-" if unknown
	OutIface string `json:"outIface"` // egress interface name (NFLOG outdev), "-" if unknown
	Reason   string `json:"reason"`
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
}

// QosClass represents a single active HTB class on an interface.
type QosClass struct {
	ClassID  string `json:"classId"`  // e.g. "1:10"
	Rate     string `json:"rate"`     // human-readable e.g. "50Mbit"
	Ceil     string `json:"ceil"`     // human-readable e.g. "100Mbit"
	RuleName string `json:"ruleName"` // matched rule name from DB (may be empty)
}
