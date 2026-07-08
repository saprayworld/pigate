package service

import (
	"log"
	"time"

	"pigate/internal/kernel"
)

// powerCommandDelay is how long we wait after returning the HTTP response before
// actually asking logind to reboot/power off. It gives the API server time to
// flush the 200 back to the browser before logind starts stopping
// pigate.service, so the frontend sees success instead of a dropped connection.
const powerCommandDelay = 1 * time.Second

// PowerService coordinates host power control (reboot / shutdown). It drives the
// kernel.PowerManager (login1 over D-Bus in production, a no-op mock in dev).
//
// The real command is fired from a delayed goroutine (see powerCommandDelay), so
// callers get an immediate nil and the HTTP response is flushed first. As a
// consequence, an error from the actual D-Bus call cannot be reported back to
// the client — it is only logged.
type PowerService struct {
	mgr kernel.PowerManager
}

func NewPowerService(mgr kernel.PowerManager) *PowerService {
	return &PowerService{mgr: mgr}
}

// Reboot logs the audit trail and schedules a graceful reboot ~1s later. It
// returns immediately so the HTTP handler can flush its response.
func (s *PowerService) Reboot(requestedBy string) error {
	log.Printf("[Power] Reboot requested by %q — executing in %s", requestedBy, powerCommandDelay)
	time.AfterFunc(powerCommandDelay, func() {
		if err := s.mgr.Reboot(); err != nil {
			log.Printf("[Power] Reboot command failed: %v", err)
		}
	})
	return nil
}

// Shutdown logs the audit trail and schedules a graceful power-off ~1s later. It
// returns immediately so the HTTP handler can flush its response.
func (s *PowerService) Shutdown(requestedBy string) error {
	log.Printf("[Power] Shutdown requested by %q — executing in %s", requestedBy, powerCommandDelay)
	time.AfterFunc(powerCommandDelay, func() {
		if err := s.mgr.PowerOff(); err != nil {
			log.Printf("[Power] Power-off command failed: %v", err)
		}
	})
	return nil
}
