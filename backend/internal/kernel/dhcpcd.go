//go:build linux

package kernel

import (
	"fmt"
	"log"
	"os"
)

// dhcpcdConfPath is the pigate-owned dhcpcd config file that dhcpcd@.service
// reads via `-f` (see install.sh STEP 2.2). /var/lib/pigate is owned by
// pigate:netdev, so pigate can write it without root.
const dhcpcdConfPath = "/var/lib/pigate/dhcpcd.conf"

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

// SetShareHostname writes dhcpcdConfPath atomically (temp file + rename).
// The content is entirely fixed/whitelisted — never interpolate user-supplied
// strings (including the hostname itself) into this file, since it is read by
// a root-owned service. An empty `hostname` directive is enough to make
// dhcpcd send the system's current hostname via DHCP Option 12; no injection
// surface is introduced by leaving it argument-less.
func (m *RealDhcpcdManager) SetShareHostname(share bool) error {
	content := "# Managed by PiGate. Do not edit manually.\n"
	if share {
		content += "hostname\n"
	}

	tempFile := dhcpcdConfPath + ".tmp"
	if err := os.WriteFile(tempFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("failed to write temporary dhcpcd config: %w", err)
	}
	if err := os.Rename(tempFile, dhcpcdConfPath); err != nil {
		return fmt.Errorf("failed to rename dhcpcd config into place: %w", err)
	}
	log.Printf("[RealDhcpcd] Wrote %s (share hostname: %t)", dhcpcdConfPath, share)
	return nil
}

// RestartDhcpcd restarts the per-interface dhcpcd@ service so a config change
// (e.g. SetShareHostname) takes effect. Causes a brief WAN lease renewal.
func (m *RealDhcpcdManager) RestartDhcpcd(ifaceName string) error {
	unit := dhcpcdUnitName(ifaceName)
	log.Printf("[RealDhcpcd] Restarting %s via D-Bus", unit)
	return RestartServiceViaDBus(unit)
}
