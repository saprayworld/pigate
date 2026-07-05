//go:build linux

package kernel

import (
	"fmt"
	"log"

	"github.com/godbus/dbus/v5"
)

const (
	systemd1Dest    = "org.freedesktop.systemd1"
	systemd1Path    = "/org/freedesktop/systemd1"
	systemd1Manager = "org.freedesktop.systemd1.Manager"
	systemd1Unit    = "org.freedesktop.systemd1.Unit"
)

// callSystemdUnitMethod เรียก method บน org.freedesktop.systemd1.Manager
// (StartUnit / StopUnit / RestartUnit) แบบเดียวกันทั้งหมด ต่างกันแค่ชื่อ method
// และ log prefix ของผู้เรียก
func callSystemdUnitMethod(logPrefix, method, serviceName string) error {
	log.Printf("[%s] Attempting to %s: %s", logPrefix, method, serviceName)

	conn, err := dbus.SystemBus()
	if err != nil {
		log.Printf("[%s] Failed to connect to system bus: %v", logPrefix, err)
		return fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object(systemd1Dest, dbus.ObjectPath(systemd1Path))
	var jobPath dbus.ObjectPath
	err = obj.Call(systemd1Manager+"."+method, 0, serviceName, "replace").Store(&jobPath)
	if err != nil {
		log.Printf("[%s] Failed to call %s for %s: %v", logPrefix, method, serviceName, err)
		return fmt.Errorf("D-Bus call %s failed for %s: %w", method, serviceName, err)
	}

	log.Printf("[%s] %s job queued successfully. Job Path: %s", logPrefix, method, jobPath)
	return nil
}

// IsServiceActiveViaDBus เช็กว่า systemd unit กำลังทำงานอยู่หรือไม่ (แทน systemctl is-active)
func IsServiceActiveViaDBus(serviceName string) bool {
	conn, err := dbus.SystemBus()
	if err != nil {
		log.Printf("[D-Bus] Failed to connect to system bus: %v", err)
		return false
	}

	obj := conn.Object(systemd1Dest, dbus.ObjectPath(systemd1Path))
	var unitPath dbus.ObjectPath
	if err := obj.Call(systemd1Manager+".GetUnit", 0, serviceName).Store(&unitPath); err != nil {
		return false
	}

	unitObj := conn.Object(systemd1Dest, unitPath)
	variant, err := unitObj.GetProperty(systemd1Unit + ".ActiveState")
	if err != nil {
		return false
	}

	state, ok := variant.Value().(string)
	return ok && state == "active"
}

// StartServiceViaDBus สั่งรัน systemd unit (แทน systemctl start)
func StartServiceViaDBus(serviceName string) error {
	return callSystemdUnitMethod("D-Bus", "StartUnit", serviceName)
}

// StopServiceViaDBus สั่งหยุด systemd unit (แทน systemctl stop)
func StopServiceViaDBus(serviceName string) error {
	return callSystemdUnitMethod("D-Bus", "StopUnit", serviceName)
}

// RestartServiceViaDBus สั่งรีสตาร์ท systemd unit (แทน systemctl restart)
func RestartServiceViaDBus(serviceName string) error {
	return callSystemdUnitMethod("D-Bus", "RestartUnit", serviceName)
}
