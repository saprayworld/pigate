//go:build linux

package kernel

import (
	"fmt"
	"log"

	"github.com/godbus/dbus/v5"
)

const (
	login1Dest = "org.freedesktop.login1"
	login1Path = "/org/freedesktop/login1"
)

// RealPowerManager controls host power state via org.freedesktop.login1
// (systemd-logind) over D-Bus. It never shells out to reboot/shutdown/systemctl
// (no-shell-execution constraint) and runs as the unprivileged pigate user,
// relying on a Polkit rule (see install.sh) to authorize the login1
// reboot/power-off actions.
type RealPowerManager struct{}

func NewRealPowerManager() *RealPowerManager {
	return &RealPowerManager{}
}

// Reboot asks logind to restart the machine. interactive=false means Polkit
// returns an error immediately if the pigate user isn't authorized, instead of
// blocking on an (impossible for a headless daemon) auth prompt.
func (m *RealPowerManager) Reboot() error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object(login1Dest, dbus.ObjectPath(login1Path))
	if err := obj.Call(login1Dest+".Manager.Reboot", 0, false).Err; err != nil {
		return fmt.Errorf("failed to reboot via login1 D-Bus: %w", err)
	}

	log.Printf("[RealPower] Reboot requested via login1 D-Bus")
	return nil
}

// PowerOff asks logind to halt the machine. See Reboot for the interactive=false
// rationale.
func (m *RealPowerManager) PowerOff() error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object(login1Dest, dbus.ObjectPath(login1Path))
	if err := obj.Call(login1Dest+".Manager.PowerOff", 0, false).Err; err != nil {
		return fmt.Errorf("failed to power off via login1 D-Bus: %w", err)
	}

	log.Printf("[RealPower] PowerOff requested via login1 D-Bus")
	return nil
}
