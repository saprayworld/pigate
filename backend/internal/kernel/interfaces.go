package kernel

import "pigate/internal/model"

// FirewallManager abstracts nftables kernel modifications
type FirewallManager interface {
	ApplyRules(
		rules []model.PolicyRule,
		ifaces []model.NetworkInterface,
		addrs []model.AddressObject,
		svcs []model.ServiceObject,
	) error
}

// NetworkManager abstracts Wi-Fi scanning and interface control
type NetworkManager interface {
	ToggleInterface(name string, up bool) error
	ScanWifi(name string) ([]model.WifiScanResult, error)
	ConfigureInterface(name string, mode string, ip string, netmask string, gateway string) error
	ConfigureWifi(name string, ssid string, password string, security string, backupSSID string, backupPassword string, backupSecurity string, macMode string) error
	GetWifiStatus(name string) (*model.WifiConnectionStatus, error)
}

// RoutingManager abstracts netlink route modifications
type RoutingManager interface {
	ApplyRoutes(routes []model.StaticRoute) error
	AddRoute(route model.StaticRoute) error
	DeleteRoute(route model.StaticRoute) error
	SetEnableEditSystemRoute(enable bool)
}

// DhcpManager abstracts DHCP configuration updates and active lease logs parsing
type DhcpManager interface {
	ApplyConfig(cfg model.DhcpConfig) error
	GetActiveLeases() ([]model.ActiveDhcpLease, error)
}

// DNSManager abstracts systemd-resolved modifications and status checks
type DNSManager interface {
	GetLinkDNS(ifaceName string) ([]string, error)
	SetLinkDNS(ifaceName string, servers []string) error
	RevertLinkDNS(ifaceName string) error
	SetGlobalDNS(servers []string, searchDomain string) error
}

