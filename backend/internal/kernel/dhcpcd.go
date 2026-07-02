//go:build linux

package kernel

import "log"

// dhcpcdUnitName returns the systemd unit name for the per-interface dhcpcd@.service
// template. dhcpcd itself is no longer exec'd directly by pigate: it now runs as its
// own root-owned systemd service, and pigate only asks systemd (via D-Bus) to
// start/stop it. This lets dhcpcd's internal privilege-separation (chroot +
// setuid/setgid) succeed in full, instead of failing with
// "ps_dropprivs: ... Operation not permitted" the way it did when pigate ran
// dhcpcd directly with only CAP_NET_ADMIN/CAP_NET_RAW.
func dhcpcdUnitName(ifaceName string) string {
	return "dhcpcd@" + ifaceName + ".service"
}

// RealDhcpcdManager starts/stops the per-interface dhcpcd@ systemd service via
// D-Bus, mirroring RealDNSServerManager's use of org.freedesktop.systemd1
// instead of exec'ing systemctl.
type RealDhcpcdManager struct{}

func NewRealDhcpcdManager() *RealDhcpcdManager {
	return &RealDhcpcdManager{}
}

func (m *RealDhcpcdManager) StartDhcpcd(ifaceName string) error {
	unit := dhcpcdUnitName(ifaceName)
	log.Printf("[RealDhcpcd] Starting %s via D-Bus", unit)
	return StartServiceViaDBus(unit)
}

func (m *RealDhcpcdManager) StopDhcpcd(ifaceName string) error {
	unit := dhcpcdUnitName(ifaceName)
	log.Printf("[RealDhcpcd] Stopping %s via D-Bus", unit)
	return StopServiceViaDBus(unit)
}
