package kernel

import (
	"context"
	"time"

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
		portForwards []model.PortForward,
	) error
}

// TrafficLogManager streams forward-chain PASS/DROP packet events into the app.
// The real implementation subscribes to an NFLOG netlink group (the forward-chain
// log statements are configured to log to a group instead of printk); the mock
// implementation synthesizes events so dev/mock mode has a live log feed.
// WatchForwardTraffic blocks until ctx is cancelled, invoking cb once per event.
// cb must return promptly — implementations must not let a slow consumer stall
// the netlink read loop (see real_traffic_log.go).
type TrafficLogManager interface {
	WatchForwardTraffic(ctx context.Context, cb func(model.FirewallLog)) error
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
	// CreateVlan creates an 802.1Q VLAN sub-interface named "<parent>.<vlanID>"
	// on top of the given parent interface (e.g. CreateVlan("eth0", 100) -> "eth0.100").
	CreateVlan(parent string, vlanID int) error
	// DeleteVlan removes a VLAN link previously created on this host. It must
	// refuse to delete a link whose kernel type is not "vlan" (a guard against
	// deleting a physical interface such as eth0/wlan0).
	DeleteVlan(name string) error
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

// DNSServerManager abstracts local DNS zone configurations and cache clearing.
// upstreamServers carries the explicit forward resolvers (from System DNS) that
// dnsmasq should use, replacing the broken resolvconf-populated resolv.conf.
type DNSServerManager interface {
	ApplyZones(zones []model.DNSZone, interfaces []string, upstreamServers []string) error
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

// SystemStatsManager abstracts host telemetry reads (/proc, /sys, statfs,
// netlink counters). It is strictly read-only: no method mutates system state.
// Implementations must degrade gracefully — a missing sysfs node (thermal zone,
// cpufreq, device-tree) yields an "unavailable" field, never a whole-response
// error, so the mock-free real path still works on WSL / x86 dev boxes.
type SystemStatsManager interface {
	// GetCPUSnapshot returns raw cumulative jiffies from /proc/stat. The service
	// computes usage% from the delta between two snapshots — a single call alone
	// is not a usage figure.
	GetCPUSnapshot() (*model.CPUSnapshot, error)
	// GetCPUInfo returns model name, core count, and current frequency
	// (FreqAvailable=false when cpufreq is absent).
	GetCPUInfo() (*model.CPUInfo, error)
	// GetMemoryInfo returns total/used bytes from /proc/meminfo.
	GetMemoryInfo() (*model.MemoryInfo, error)
	// GetTemperature returns SoC temperature; Available=false when no thermal
	// zone is present.
	GetTemperature() (*model.TemperatureInfo, error)
	// GetDiskUsage returns filesystem usage for the given mount path via statfs.
	GetDiskUsage(path string) (*model.DiskUsage, error)
	// GetHostInfo returns OS/board/kernel identity and uptime.
	GetHostInfo() (*model.HostInfo, error)
	// GetNetCounters returns cumulative rx/tx byte counters keyed by interface name.
	GetNetCounters() (map[string]model.NetCounters, error)
}

// HostnameManager abstracts reading/writing the system hostname via
// org.freedesktop.hostname1 (systemd-hostnamed) over D-Bus.
type HostnameManager interface {
	GetHostname() (string, error)
	SetHostname(name string) error
}

// TimeManager abstracts timezone / NTP / manual-clock control via
// org.freedesktop.timedate1 (systemd-timedated) over D-Bus, plus the
// systemd-timesyncd drop-in used to point NTP at a custom server.
type TimeManager interface {
	// GetTimeStatus reads live state (current time + whether NTP has synced).
	GetTimeStatus() (*model.TimeStatus, error)
	// SetTimezone sets the IANA timezone (timedated writes /etc/localtime).
	SetTimezone(tz string) error
	// SetNTP enables/disables automatic time sync (timedated starts/stops
	// and enables/disables systemd-timesyncd).
	SetNTP(enable bool) error
	// SetTime sets the wall clock manually. Rejected by timedated while NTP
	// is enabled — callers must guard against that first.
	SetTime(t time.Time) error
	// SetNTPServer writes the pigate-owned timesyncd drop-in with the given
	// server(s) and restarts timesyncd (only while NTP is enabled). An empty
	// server clears the drop-in back to distro defaults.
	SetNTPServer(server string) error
}

// PowerManager abstracts host power control via org.freedesktop.login1
// (systemd-logind) over D-Bus. Both operations are irreversible: Reboot
// restarts the board and PowerOff halts it (requiring physical intervention
// to power back on). logind performs a graceful shutdown, stopping services
// (including pigate.service) so SQLite closes cleanly on its own.
type PowerManager interface {
	Reboot() error
	PowerOff() error
}
