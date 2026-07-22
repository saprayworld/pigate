//go:build linux

package kernel

import (
	"fmt"
	"log"

	"github.com/godbus/dbus/v5"

	"pigate/internal/model"
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

// GetUnitRuntimeState reads a systemd unit's live ActiveState + LoadState via
// D-Bus (Manager.GetUnit + Unit property reads) — the same mechanism as
// IsServiceActiveViaDBus, but returning both fields instead of a single bool
// so the caller can distinguish "not installed" (LoadState != loaded) from
// "installed but stopped". A unit that has never been loaded (no unit file
// present at all, e.g. an optional package that isn't installed) makes
// GetUnit fail with org.freedesktop.systemd1.NoSuchUnit — that specific case
// is not treated as a D-Bus/transport error, it is mapped to
// ServiceRuntimeState{Loaded: false} so callers can render "unavailable"
// instead of a hard failure.
func GetUnitRuntimeState(serviceName string) (model.ServiceRuntimeState, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return model.ServiceRuntimeState{}, fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}

	obj := conn.Object(systemd1Dest, dbus.ObjectPath(systemd1Path))
	var unitPath dbus.ObjectPath
	if err := obj.Call(systemd1Manager+".GetUnit", 0, serviceName).Store(&unitPath); err != nil {
		if dbusErr, ok := err.(dbus.Error); ok && dbusErr.Name == "org.freedesktop.systemd1.NoSuchUnit" {
			return model.ServiceRuntimeState{ActiveState: "inactive", Loaded: false}, nil
		}
		return model.ServiceRuntimeState{}, fmt.Errorf("D-Bus GetUnit failed for %s: %w", serviceName, err)
	}

	unitObj := conn.Object(systemd1Dest, unitPath)
	activeVariant, err := unitObj.GetProperty(systemd1Unit + ".ActiveState")
	if err != nil {
		return model.ServiceRuntimeState{}, fmt.Errorf("failed to read ActiveState for %s: %w", serviceName, err)
	}
	loadVariant, err := unitObj.GetProperty(systemd1Unit + ".LoadState")
	if err != nil {
		return model.ServiceRuntimeState{}, fmt.Errorf("failed to read LoadState for %s: %w", serviceName, err)
	}

	activeState, _ := activeVariant.Value().(string)
	loadState, _ := loadVariant.Value().(string)

	return model.ServiceRuntimeState{
		ActiveState: activeState,
		Loaded:      loadState == "loaded",
	}, nil
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
