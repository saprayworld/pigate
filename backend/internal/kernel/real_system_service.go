//go:build linux

package kernel

import (
	"pigate/internal/model"
)

// RealSystemServiceManager implements SystemServiceManager by delegating to
// the D-Bus helpers in dbus_systemd.go (org.freedesktop.systemd1.Manager over
// the system bus) — no shell execution. It never validates or restricts which
// unit it is asked to read/restart; that whitelist policy lives in
// service.SystemServiceService, which is the only caller allowed to turn a
// client-supplied slug into a unit name (see docs/ref/todo/os-service-status-control-plan.md).
type RealSystemServiceManager struct{}

func NewRealSystemServiceManager() *RealSystemServiceManager {
	return &RealSystemServiceManager{}
}

// GetStatus reads the given unit's live ActiveState + LoadState via D-Bus.
func (m *RealSystemServiceManager) GetStatus(unit string) (model.ServiceRuntimeState, error) {
	return GetUnitRuntimeState(unit)
}

// Restart asks systemd to restart the given unit via D-Bus.
func (m *RealSystemServiceManager) Restart(unit string) error {
	return RestartServiceViaDBus(unit)
}
