package kernel

import "pigate/internal/model"

// FirewallManager abstracts nftables kernel modifications
type FirewallManager interface {
	ApplyRules(rules []model.PolicyRule, ifaces []model.NetworkInterface) error
}

// NetworkManager abstracts Wi-Fi scanning and interface control
type NetworkManager interface {
	ToggleInterface(name string, up bool) error
	ScanWifi(name string) ([]model.WifiScanResult, error)
	ConfigureInterface(name string, mode string, ip string, netmask string, gateway string) error
}

// RoutingManager abstracts netlink route modifications
type RoutingManager interface {
	ApplyRoutes(routes []model.StaticRoute) error
}

// DhcpManager abstracts DHCP configuration updates and active lease logs parsing
type DhcpManager interface {
	ApplyConfig(cfg model.DhcpConfig) error
	GetActiveLeases() ([]model.ActiveDhcpLease, error)
}
