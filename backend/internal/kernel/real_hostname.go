//go:build linux

package kernel

import (
	"fmt"
	"log"
	"os"

	"github.com/godbus/dbus/v5"
)

const (
	hostname1Dest = "org.freedesktop.hostname1"
	hostname1Path = "/org/freedesktop/hostname1"
)

// RealHostnameManager sets/reads the system hostname via org.freedesktop.hostname1
// (systemd-hostnamed) over D-Bus. hostnamed itself writes /etc/hostname atomically,
// so pigate never touches root-owned files directly for this feature.
type RealHostnameManager struct{}

func NewRealHostnameManager() *RealHostnameManager {
	return &RealHostnameManager{}
}

// GetHostname reads the StaticHostname property; falls back to os.Hostname()
// if hostnamed hasn't set a static hostname (e.g. fresh install).
func (m *RealHostnameManager) GetHostname() (string, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return "", fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object(hostname1Dest, dbus.ObjectPath(hostname1Path))
	variant, err := obj.GetProperty(hostname1Dest + ".StaticHostname")
	if err == nil {
		if name, ok := variant.Value().(string); ok && name != "" {
			return name, nil
		}
	}

	return os.Hostname()
}

// SetHostname sets both the static hostname (persisted to /etc/hostname by
// hostnamed) and the transient/kernel hostname, so the change takes effect
// immediately without a reboot.
func (m *RealHostnameManager) SetHostname(name string) error {
	conn, err := dbus.SystemBus()
	if err != nil {
		return fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object(hostname1Dest, dbus.ObjectPath(hostname1Path))

	if err := obj.Call(hostname1Dest+".SetStaticHostname", 0, name, false).Err; err != nil {
		return fmt.Errorf("failed to set static hostname via D-Bus: %w", err)
	}

	if err := obj.Call(hostname1Dest+".SetHostname", 0, name, false).Err; err != nil {
		return fmt.Errorf("failed to set transient hostname via D-Bus: %w", err)
	}

	log.Printf("[RealHostname] Hostname set to %q via D-Bus", name)
	return nil
}
