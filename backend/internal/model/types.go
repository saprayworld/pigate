package model

import "time"

// User represents dashboard administrator login credentials
type User struct {
	ID           string    `json:"id"`
	Username     string    `json:"username"`
	PasswordHash string    `json:"-"`
	IsInitial    bool      `json:"isInitial"`
	CreatedAt    time.Time `json:"createdAt"`
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
	ID                   string       `json:"id"`
	Name                 string       `json:"name"`  // e.g. "eth0", "wlan0"
	Alias                string       `json:"alias"` // e.g. "LAN_Internal"
	Role                 string       `json:"role"`  // "LAN", "WAN"
	Type                 string       `json:"type"`  // "ethernet", "wireless"
	Subtype              string       `json:"subtype"` // e.g. "device", "veth", "bridge", "vlan"
	AddressingMode       string       `json:"addressingMode"`
	IP                   string       `json:"ip"`
	Netmask              string       `json:"netmask"`
	Gateway              string       `json:"gateway"`
	MacAddress           string       `json:"macAddress"`
	AdminAccess          []string     `json:"adminAccess"` // PING, HTTP, HTTPS, SSH
	Status               string       `json:"status"`      // "up", "down"
	Speed                string       `json:"speed"`       // e.g. "1000 Mbps"

	WifiSSID             *string      `json:"wifiSSID,omitempty"`
	WifiPassword         *string      `json:"wifiPassword,omitempty"`
	WifiSecurity         *string      `json:"wifiSecurity,omitempty"`
	MacMode              *string      `json:"macMode,omitempty"` // "hardware", "randomized", "laa"
	RealMacAddress       *string      `json:"realMacAddress,omitempty"`
	RandomizedMac        *string      `json:"randomizedMac,omitempty"`
	LaaMacAddress        *string      `json:"laaMacAddress,omitempty"`
	RandomizeOnReconnect *bool        `json:"randomizeOnReconnect,omitempty"`
	FailoverEnabled      *bool        `json:"failoverEnabled,omitempty"`
	BackupSSID           *string      `json:"backupSsid,omitempty"`
	BackupWifiPassword   *string      `json:"backupWifiPassword,omitempty"`
	BackupWifiSecurity   *string      `json:"backupWifiSecurity,omitempty"`
	IPCheckTimeout       *int         `json:"ipCheckTimeout,omitempty"`
	PrimaryMaxRetries    *int         `json:"primaryMaxRetries,omitempty"`
	FailoverCooldown     *int         `json:"failoverCooldown,omitempty"`
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
	State     string `json:"state"` // e.g. "COMPLETED", "DISCONNECTED", "SCANNING", etc.
	SSID      string `json:"ssid"`  // Connected network name
	BSSID     string `json:"bssid"` // MAC address of the connected AP
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
	ExpiresIn  string `json:"expiresIn"`
}

// SystemTimeSettings represents NTP and timezone configurations
type SystemTimeSettings struct {
	Timezone  string `json:"timezone"`
	NTPSync   bool   `json:"ntpSync"`
	NTPServer string `json:"ntpServer"`
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
	ID     string `json:"id"`
	Time   string `json:"time"`
	Action string `json:"action"` // "PASS", "DROP"
	Src    string `json:"src"`
	Dest   string `json:"dest"`
	Port   string `json:"port"`
	Proto  string `json:"proto"`
	Reason string `json:"reason"`
}

// PerformanceMetrics represents hardware state logs
type PerformanceMetrics struct {
	CPU    float64 `json:"cpu"`
	Memory float64 `json:"memory"`
	Temp   float64 `json:"temp"`
}

// DashboardStats represents widgets counters
type DashboardStats struct {
	FirewallStatus  string `json:"firewallStatus"`
	TotalTrafficIn  string `json:"totalTrafficIn"`
	TotalTrafficOut string `json:"totalTrafficOut"`
	DhcpLeasesCount int    `json:"dhcpLeasesCount"`
	WifiStatus      string `json:"wifiStatus"`
	WifiSSID        string `json:"wifiSSID"`
}

// DNSConfig represents system-wide DNS settings
type DNSConfig struct {
	Mode         string               `json:"mode"` // "wan", "static"
	PrimaryDNS   string               `json:"primaryDns"`
	SecondaryDNS string               `json:"secondaryDns"`
	LocalDomain  string               `json:"localDomain"`
	DynamicDNS   []DynamicDNSServer   `json:"dynamicDnsServers"`
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
	ID               string `json:"id"`
	Name             string `json:"name"`
	Interface        string `json:"interface"`        // e.g. "eth0"
	MatchSrcIP       string `json:"matchSrcIp"`       // CIDR e.g. "172.24.25.0/24", empty = match all
	MatchDstIP       string `json:"matchDstIp"`       // CIDR e.g. "0.0.0.0/0", empty = match all
	EgressRateMbps   int    `json:"egressRateMbps"`   // Client Download guaranteed rate, 0 = unlimited
	EgressCeilMbps   int    `json:"egressCeilMbps"`   // Client Download burst ceiling, 0 = unlimited
	IngressRateMbps  int    `json:"ingressRateMbps"`  // Client Upload rate via IFB (Phase 2), 0 = unlimited
	IngressCeilMbps  int    `json:"ingressCeilMbps"`  // Client Upload burst ceiling via IFB (Phase 2)
	Priority         int    `json:"priority"`         // Filter priority (lower = matched first)
	Status           bool   `json:"status"`           // true = enabled, false = disabled
	Description      string `json:"description"`
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
