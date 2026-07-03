package service

import (
	"fmt"
	"log"
	"net"
	"regexp"
	"strings"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// timezoneCharRegex restricts a timezone string to the characters valid in an
// IANA zone name. This is a defence-in-depth check in front of
// time.LoadLocation — it rejects whitespace and shell/path metacharacters
// before the value is ever handed to timedated.
var timezoneCharRegex = regexp.MustCompile(`^[A-Za-z0-9_+/-]+$`)

// hostnameLabelRE matches a single RFC 1123 DNS label.
var hostnameLabelRE = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// TimeService coordinates timezone / NTP / manual-clock configuration. Config
// (timezone, ntpSync, ntpServer) lives in the DB; the wall-clock time itself is
// runtime-only state and is deliberately never persisted (persisting it and
// re-applying at boot would rewind the clock on every reboot).
type TimeService struct {
	repo *db.Repository
	time kernel.TimeManager
}

func NewTimeService(repo *db.Repository, tm kernel.TimeManager) *TimeService {
	return &TimeService{repo: repo, time: tm}
}

// ValidateTimezone enforces IANA-name character rules and that the zone
// actually resolves via the system tzdata. Exported so the API layer can map a
// failure to HTTP 400.
func ValidateTimezone(tz string) error {
	if tz == "" {
		return fmt.Errorf("กรุณาระบุเขตเวลา (Timezone)")
	}
	if !timezoneCharRegex.MatchString(tz) {
		return fmt.Errorf("รูปแบบเขตเวลาไม่ถูกต้อง (ต้องเป็นชื่อ IANA เช่น Asia/Bangkok)")
	}
	if _, err := time.LoadLocation(tz); err != nil {
		return fmt.Errorf("ไม่รู้จักเขตเวลา %q — ต้องเป็นชื่อ IANA ที่ถูกต้อง", tz)
	}
	return nil
}

// ValidateNTPServer validates the NTP server field. systemd-timesyncd allows a
// space-separated list, so each token is validated individually as either an IP
// or an RFC 1123 hostname. An empty value is allowed (means "use the distro
// default server"). This is a security boundary: the value is written verbatim
// into a root-read ini file, so characters that could inject another directive
// (newline, '[', ']', '=', '#') are rejected outright.
func ValidateNTPServer(server string) error {
	server = strings.TrimSpace(server)
	if server == "" {
		return nil
	}
	if strings.ContainsAny(server, "\n\r[]=#") {
		return fmt.Errorf("ที่อยู่ NTP Server มีอักขระต้องห้าม")
	}
	tokens := strings.Fields(server)
	if len(tokens) == 0 {
		return nil
	}
	for _, tok := range tokens {
		if len(tok) > 253 {
			return fmt.Errorf("ที่อยู่ NTP Server ยาวเกินไป: %q", tok)
		}
		if net.ParseIP(tok) != nil {
			continue
		}
		if !isValidHostname(tok) {
			return fmt.Errorf("ที่อยู่ NTP Server ไม่ถูกต้อง: %q (ต้องเป็น IP หรือ hostname)", tok)
		}
	}
	return nil
}

func isValidHostname(host string) bool {
	host = strings.TrimSuffix(host, ".")
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if !hostnameLabelRE.MatchString(label) {
			return false
		}
	}
	return true
}

// ValidateManualTime parses an RFC3339 datetime and sanity-checks the year so an
// obvious typo can't push the clock to 1970 or 3000 (which would break TLS,
// sessions, DHCP timers, etc.). Returns the parsed time on success.
func ValidateManualTime(datetime string) (time.Time, error) {
	t, err := time.Parse(time.RFC3339, datetime)
	if err != nil {
		return time.Time{}, fmt.Errorf("รูปแบบวันที่/เวลาไม่ถูกต้อง (ต้องเป็น RFC3339 เช่น 2026-07-03T15:04:05+07:00)")
	}
	if y := t.Year(); y < 2020 || y > 2100 {
		return time.Time{}, fmt.Errorf("ปีที่ระบุ (%d) อยู่นอกช่วงที่อนุญาต (2020–2100)", y)
	}
	return t, nil
}

// Get returns the persisted config merged with live kernel status (current
// time + NTP-sync flag). A failure to read live status is non-fatal — the
// config is still returned so the UI can render.
func (s *TimeService) Get() (*model.SystemTimeSettings, error) {
	settings, err := s.repo.GetSystemTimeSettings()
	if err != nil {
		return nil, err
	}

	if status, err := s.time.GetTimeStatus(); err != nil {
		log.Printf("[TimeService] Failed to read live time status: %v", err)
	} else {
		settings.Status = status
	}

	return settings, nil
}

// Update validates and persists the config, then applies it to the OS in a
// deliberate order: timezone first, then the NTP server drop-in, then the NTP
// on/off toggle last (so enabling NTP already picks up the new server). The DB
// is written before the kernel calls so a partial kernel failure still leaves
// the intended config recorded for the next InitApplyConfig.
func (s *TimeService) Update(newSettings model.SystemTimeSettings) error {
	if err := ValidateTimezone(newSettings.Timezone); err != nil {
		return err
	}
	if err := ValidateNTPServer(newSettings.NTPServer); err != nil {
		return err
	}
	newSettings.NTPServer = strings.TrimSpace(newSettings.NTPServer)

	old, err := s.repo.GetSystemTimeSettings()
	if err != nil {
		return err
	}

	// Status is live-only; never persist it.
	toStore := newSettings
	toStore.Status = nil
	if err := s.repo.UpdateSystemTimeSettings(toStore); err != nil {
		return err
	}

	if newSettings.Timezone != old.Timezone {
		if err := s.time.SetTimezone(newSettings.Timezone); err != nil {
			return fmt.Errorf("ไม่สามารถตั้งเขตเวลาได้: %w", err)
		}
	}

	if newSettings.NTPServer != old.NTPServer {
		if err := s.time.SetNTPServer(newSettings.NTPServer); err != nil {
			return fmt.Errorf("ไม่สามารถตั้งค่า NTP Server ได้: %w", err)
		}
	}

	if err := s.time.SetNTP(newSettings.NTPSync); err != nil {
		return fmt.Errorf("ไม่สามารถเปิด/ปิดการซิงค์เวลา (NTP) ได้: %w", err)
	}

	return nil
}

// SetManualTime sets the wall clock by hand. It is rejected while NTP sync is
// enabled in the persisted config: timedated would refuse the D-Bus call anyway,
// but its error message is opaque, so we reject early with a clear message.
func (s *TimeService) SetManualTime(datetime string) error {
	t, err := ValidateManualTime(datetime)
	if err != nil {
		return err
	}

	settings, err := s.repo.GetSystemTimeSettings()
	if err != nil {
		return err
	}
	if settings.NTPSync {
		return fmt.Errorf("ไม่สามารถตั้งเวลาด้วยมือได้ขณะเปิดการซิงค์เวลาอัตโนมัติ (NTP) — กรุณาปิด NTP ก่อน")
	}

	if err := s.time.SetTime(t); err != nil {
		return fmt.Errorf("ไม่สามารถตั้งเวลาได้: %w", err)
	}
	return nil
}

// InitApplyConfig applies the persisted timezone / NTP-server / NTP-toggle to the
// OS at boot. It must NEVER call SetTime — the wall clock is runtime state, and
// re-applying a stored time on every boot would rewind the clock.
func (s *TimeService) InitApplyConfig() error {
	settings, err := s.repo.GetSystemTimeSettings()
	if err != nil {
		return err
	}

	if err := ValidateTimezone(settings.Timezone); err != nil {
		// Don't abort the whole boot on a bad stored value; log and skip so the
		// rest of the config (NTP) still applies.
		log.Printf("[TimeService] Stored timezone %q invalid, skipping: %v", settings.Timezone, err)
	} else if err := s.time.SetTimezone(settings.Timezone); err != nil {
		return fmt.Errorf("failed to apply timezone at startup: %w", err)
	}

	if err := ValidateNTPServer(settings.NTPServer); err != nil {
		log.Printf("[TimeService] Stored NTP server %q invalid, skipping: %v", settings.NTPServer, err)
	} else if err := s.time.SetNTPServer(strings.TrimSpace(settings.NTPServer)); err != nil {
		return fmt.Errorf("failed to apply NTP server at startup: %w", err)
	}

	if err := s.time.SetNTP(settings.NTPSync); err != nil {
		return fmt.Errorf("failed to apply NTP toggle at startup: %w", err)
	}

	return nil
}
