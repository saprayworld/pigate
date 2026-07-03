//go:build linux

package kernel

import (
	"fmt"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/godbus/dbus/v5"

	"pigate/internal/model"
)

const (
	timedate1Dest = "org.freedesktop.timedate1"
	timedate1Path = "/org/freedesktop/timedate1"

	// timesyncd drop-in owned by pigate (see install.sh STEP for creation +
	// ACL). timedate1 has no API for the NTP server, so we write it here and
	// restart timesyncd, mirroring the systemd-resolved drop-in pattern.
	timesyncdDropInPath = "/etc/systemd/timesyncd.conf.d/50-pigate.conf"
	timesyncdService    = "systemd-timesyncd.service"
)

// RealTimeManager controls the system clock, timezone and NTP via
// org.freedesktop.timedate1 (systemd-timedated) over D-Bus. timedated itself
// writes /etc/localtime and toggles systemd-timesyncd, so pigate never touches
// those root-owned paths directly. The one exception is the NTP *server* list,
// which has no D-Bus API and is written to a pigate-owned timesyncd drop-in.
type RealTimeManager struct{}

func NewRealTimeManager() *RealTimeManager {
	return &RealTimeManager{}
}

func timedate1Object() (dbus.BusObject, error) {
	conn, err := dbus.SystemBus()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to D-Bus system bus: %w", err)
	}
	return conn.Object(timedate1Dest, dbus.ObjectPath(timedate1Path)), nil
}

// GetTimeStatus reads the live clock and NTP-sync flag from timedated. The
// current time is derived from the TimeUSec property (microseconds since the
// Unix epoch, UTC) and returned in the device's local timezone.
func (m *RealTimeManager) GetTimeStatus() (*model.TimeStatus, error) {
	obj, err := timedate1Object()
	if err != nil {
		return nil, err
	}

	status := &model.TimeStatus{}

	if v, err := obj.GetProperty(timedate1Dest + ".TimeUSec"); err == nil {
		if usec, ok := v.Value().(uint64); ok && usec > 0 {
			t := time.UnixMicro(int64(usec)).Local()
			status.CurrentTime = t.Format(time.RFC3339)
		}
	}
	// Fallback to the process clock if the property was unreadable.
	if status.CurrentTime == "" {
		status.CurrentTime = time.Now().Format(time.RFC3339)
	}

	if v, err := obj.GetProperty(timedate1Dest + ".NTPSynchronized"); err == nil {
		if synced, ok := v.Value().(bool); ok {
			status.NTPSynchronized = synced
		}
	}

	return status, nil
}

// SetTimezone sets the IANA timezone. timedated updates /etc/localtime and
// /etc/timezone atomically. interactive=false so no polkit agent prompt.
func (m *RealTimeManager) SetTimezone(tz string) error {
	obj, err := timedate1Object()
	if err != nil {
		return err
	}
	if err := obj.Call(timedate1Dest+".SetTimezone", 0, tz, false).Err; err != nil {
		return fmt.Errorf("failed to set timezone via D-Bus: %w", err)
	}
	log.Printf("[RealTime] Timezone set to %q via D-Bus", tz)
	return nil
}

// SetNTP enables/disables automatic time synchronisation. timedated
// starts+enables (or stops+disables) systemd-timesyncd accordingly.
func (m *RealTimeManager) SetNTP(enable bool) error {
	obj, err := timedate1Object()
	if err != nil {
		return err
	}
	if err := obj.Call(timedate1Dest+".SetNTP", 0, enable, false).Err; err != nil {
		return fmt.Errorf("failed to set NTP state via D-Bus: %w", err)
	}
	log.Printf("[RealTime] NTP sync set to %t via D-Bus", enable)
	return nil
}

// SetTime sets the wall clock to an absolute time. timedated rejects this while
// NTP is enabled — the service layer guards against that before calling here.
func (m *RealTimeManager) SetTime(t time.Time) error {
	obj, err := timedate1Object()
	if err != nil {
		return err
	}
	usec := t.UnixMicro()
	// args: usec_utc (int64), relative (bool), interactive (bool)
	if err := obj.Call(timedate1Dest+".SetTime", 0, usec, false, false).Err; err != nil {
		return fmt.Errorf("failed to set time via D-Bus: %w", err)
	}
	log.Printf("[RealTime] Clock set to %s via D-Bus", t.Format(time.RFC3339))
	return nil
}

// SetNTPServer writes the pigate-owned timesyncd drop-in and restarts timesyncd
// if NTP is currently active (so the new server takes effect immediately). An
// empty server removes the drop-in, reverting to the distro default server.
// The value is expected to be pre-validated by the service layer.
func (m *RealTimeManager) SetNTPServer(server string) error {
	if server == "" {
		if err := os.Remove(timesyncdDropInPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove timesyncd drop-in: %w", err)
		}
		log.Printf("[RealTime] Cleared custom NTP server (removed drop-in)")
		return m.restartTimesyncdIfActive()
	}

	content := fmt.Sprintf("[Time]\nNTP=%s\n", server)

	// Atomic write: temp file in the same directory as the target so the
	// os.Rename stays within one filesystem, then replace the drop-in.
	dir := filepath.Dir(timesyncdDropInPath)
	tmp, err := os.CreateTemp(dir, ".50-pigate.conf.*.tmp")
	if err != nil {
		return fmt.Errorf("failed to create temp timesyncd config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op if rename succeeded

	if _, err := tmp.WriteString(content); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to write temp timesyncd config: %w", err)
	}
	if err := tmp.Chmod(0644); err != nil {
		tmp.Close()
		return fmt.Errorf("failed to chmod temp timesyncd config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp timesyncd config: %w", err)
	}

	if err := os.Rename(tmpPath, timesyncdDropInPath); err != nil {
		return fmt.Errorf("failed to install timesyncd drop-in: %w", err)
	}
	log.Printf("[RealTime] Wrote NTP server %q to %s", server, timesyncdDropInPath)

	return m.restartTimesyncdIfActive()
}

// restartTimesyncdIfActive restarts systemd-timesyncd only when NTP is
// currently enabled. When NTP is disabled the drop-in is dormant, and when NTP
// is about to be (re-)enabled the subsequent SetNTP start already reads the new
// config — so an unconditional restart would be wasteful or spurious.
func (m *RealTimeManager) restartTimesyncdIfActive() error {
	obj, err := timedate1Object()
	if err != nil {
		return err
	}
	ntpOn := false
	if v, err := obj.GetProperty(timedate1Dest + ".NTP"); err == nil {
		if on, ok := v.Value().(bool); ok {
			ntpOn = on
		}
	}
	if !ntpOn {
		return nil
	}
	if err := RestartServiceViaDBus(timesyncdService); err != nil {
		return fmt.Errorf("failed to restart %s: %w", timesyncdService, err)
	}
	return nil
}
