package kernel

import (
	"context"
	"pigate/internal/model"
)

// FirewallManager abstracts nftables kernel modifications
type FirewallManager interface {
	ApplyRules(
		rules []model.PolicyRule,
		ifaces []model.NetworkInterface,
		addrs []model.AddressObject,
		svcs []model.ServiceObject,
		dhcpServerIfaces []string,
		dnsServerIfaces []string,
	) error
}

// NetworkManager abstracts Wi-Fi scanning and interface control
type NetworkManager interface {
	ToggleInterface(name string, up bool) error
	ScanWifi(name string) ([]model.WifiScanResult, error)
	// ConfigureInterface applies IP/mode/gateway to an interface.
	// metric sets the default-route priority in static mode; metric <= 0 means
	// "unset" and falls back to the historical default of 100.
	ConfigureInterface(name string, mode string, ip string, netmask string, gateway string, metric int) error
	ConfigureWifi(name string, ssid string, password string, security string, backupSSID string, backupPassword string, backupSecurity string, macMode string) error
	GetWifiStatus(name string) (*model.WifiConnectionStatus, error)
}

// RoutingManager abstracts netlink route modifications
type RoutingManager interface {
	ApplyRoutes(routes []model.StaticRoute) error
	AddRoute(route model.StaticRoute) error
	DeleteRoute(route model.StaticRoute) error
	SetEnableEditSystemRoute(enable bool)
	// EnforceDefaultRouteMetric ensures the IPv4 default gateway route on ifaceName
	// has the given priority, deleting and re-adding it (preserving proto/scope/src/gw)
	// if the current priority differs. Used to override the metric of dhcpcd-managed
	// default routes for multi-WAN failover ordering. IPv4 only.
	EnforceDefaultRouteMetric(ifaceName string, metric int) error
}

// DhcpManager abstracts DHCP configuration updates and active lease logs parsing
type DhcpManager interface {
	ApplyConfig(cfgs []model.DhcpConfig, reservations []model.DhcpReservation) error
	GetActiveLeases() ([]model.ActiveDhcpLease, error)
	ReloadConfig() error
	WatchLeases(ctx context.Context, callback func(event string, lease model.ActiveDhcpLease)) error
}

// DNSManager abstracts systemd-resolved modifications and status checks
type DNSManager interface {
	GetLinkDNS(ifaceName string) ([]string, error)
	SetLinkDNS(ifaceName string, servers []string) error
	RevertLinkDNS(ifaceName string) error
	SetGlobalDNS(servers []string, searchDomain string) error
}

// QosManager abstracts Linux Traffic Control (tc) via netlink for bandwidth shaping.
// Phase 1: Egress (Client Download) shaping via HTB Qdisc.
// Phase 2: Ingress (Client Upload) shaping via IFB device redirect (not yet implemented).
type QosManager interface {
	// ApplyQosRules rebuilds HTB qdisc + classes + filters on all affected interfaces.
	// It is idempotent: it clears existing rules before re-applying.
	// Only rules with Status=true are applied to the kernel.
	ApplyQosRules(rules []model.QosRule) error

	// ClearQosRules removes the root qdisc from a specific interface,
	// which cascades and removes all classes and filters underneath.
	ClearQosRules(ifaceName string) error

	// GetIfaceQosStatus returns the live qdisc and class state from the kernel
	// for a given interface. Does not read from the database.
	GetIfaceQosStatus(ifaceName string) (*model.QosIfaceStatus, error)
}

// DNSServerManager abstracts local DNS zone configurations and cache clearing
type DNSServerManager interface {
	ApplyZones(zones []model.DNSZone, interfaces []string) error
	ClearCache() error
}

// DhcpcdManager abstracts starting/stopping the per-interface dhcpcd@ systemd
// service. dhcpcd runs as its own root-owned systemd service so its internal
// privilege-separation (chroot + setuid/setgid) works correctly; pigate only
// asks systemd to start/stop it.
type DhcpcdManager interface {
	StartDhcpcd(ifaceName string) error
	StopDhcpcd(ifaceName string) error
	// SetShareHostname writes/clears the `hostname` directive in the pigate-owned
	// dhcpcd config file (/var/lib/pigate/dhcpcd.conf) that dhcpcd@.service reads.
	// share=true makes dhcpcd send DHCP Option 12 with the system's current hostname.
	SetShareHostname(share bool) error
	// RestartDhcpcd restarts the per-interface dhcpcd@ service so a config change
	// (e.g. SetShareHostname) takes effect. Causes a brief WAN lease renewal.
	RestartDhcpcd(ifaceName string) error
}

// HostnameManager abstracts reading/writing the system hostname via
// org.freedesktop.hostname1 (systemd-hostnamed) over D-Bus.
type HostnameManager interface {
	GetHostname() (string, error)
	SetHostname(name string) error
}
