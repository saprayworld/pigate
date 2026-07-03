package service

import (
	"fmt"
	"log"
	"regexp"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

var hostnameLabelRegex = regexp.MustCompile(`^[a-zA-Z0-9]([a-zA-Z0-9-]{0,61}[a-zA-Z0-9])?$`)

// HostnameService coordinates the device hostname (via kernel.HostnameManager /
// org.freedesktop.hostname1) and, optionally, sharing that hostname with the
// WAN DHCP server via dhcpcd Option 12 (kernel.DhcpcdManager).
type HostnameService struct {
	repo         *db.Repository
	hostname     kernel.HostnameManager
	dhcpcd       kernel.DhcpcdManager
	ifaceService *InterfaceService
}

func NewHostnameService(repo *db.Repository, hostname kernel.HostnameManager, dhcpcd kernel.DhcpcdManager, ifaceService *InterfaceService) *HostnameService {
	return &HostnameService{
		repo:         repo,
		hostname:     hostname,
		dhcpcd:       dhcpcd,
		ifaceService: ifaceService,
	}
}

// ValidateHostname enforces RFC 1123 label rules (also enforced by hostnamed,
// but we validate here first so both the PUT API and config import paths get
// a clear Thai error message instead of an opaque D-Bus error). Exported so
// the API handler can distinguish a validation failure (400) from a kernel/
// D-Bus failure (500).
func ValidateHostname(name string) error {
	if name == "" {
		return fmt.Errorf("กรุณาระบุชื่อ Hostname")
	}
	if len(name) > 63 {
		return fmt.Errorf("Hostname ต้องมีความยาวไม่เกิน 63 ตัวอักษร")
	}
	if !hostnameLabelRegex.MatchString(name) {
		return fmt.Errorf("Hostname ต้องประกอบด้วยตัวอักษร a-z, A-Z, ตัวเลข 0-9 และเครื่องหมาย - เท่านั้น (ห้ามขึ้นต้นหรือลงท้ายด้วย -)")
	}
	return nil
}

// Get returns the hostname settings stored in the database. If the DB row is
// somehow missing, it falls back to reading the live kernel hostname.
func (s *HostnameService) Get() (*model.SystemHostnameSettings, error) {
	settings, err := s.repo.GetHostnameSettings()
	if err == nil {
		return settings, nil
	}

	name, kErr := s.hostname.GetHostname()
	if kErr != nil {
		return nil, err
	}
	return &model.SystemHostnameSettings{Hostname: name, ShareWithDhcp: false}, nil
}

// Update validates and persists the new hostname settings, applies the
// hostname via D-Bus, and — only when the effective DHCP-shared value changes
// (share toggled, or hostname changed while share=on) — rewrites the dhcpcd
// config and restarts dhcpcd@ on active DHCP interfaces. Restarting dhcpcd
// briefly interrupts the WAN lease, so this is intentionally not done on
// every save.
func (s *HostnameService) Update(newSettings model.SystemHostnameSettings) error {
	if err := ValidateHostname(newSettings.Hostname); err != nil {
		return err
	}

	oldSettings, err := s.repo.GetHostnameSettings()
	if err != nil {
		return err
	}

	if err := s.repo.UpdateHostnameSettings(newSettings); err != nil {
		return err
	}

	if err := s.hostname.SetHostname(newSettings.Hostname); err != nil {
		return fmt.Errorf("ไม่สามารถตั้งค่า Hostname ผ่าน D-Bus ได้: %w", err)
	}

	hostnameChanged := oldSettings.Hostname != newSettings.Hostname
	shareChanged := oldSettings.ShareWithDhcp != newSettings.ShareWithDhcp
	needsDhcpcdUpdate := shareChanged || (hostnameChanged && newSettings.ShareWithDhcp)

	if needsDhcpcdUpdate {
		if err := s.dhcpcd.SetShareHostname(newSettings.ShareWithDhcp); err != nil {
			return fmt.Errorf("ไม่สามารถบันทึกการตั้งค่า share hostname ให้ dhcpcd ได้: %w", err)
		}
		s.restartDhcpOnActiveInterfaces()
	}

	return nil
}

// InitApplyConfig applies the DB-stored hostname + DHCP-share setting to the
// kernel at boot, before dhcpcd@ services are started (so the first DHCP
// request already carries the correct hostname). No restart is needed here.
func (s *HostnameService) InitApplyConfig() error {
	settings, err := s.repo.GetHostnameSettings()
	if err != nil {
		return err
	}

	if err := s.hostname.SetHostname(settings.Hostname); err != nil {
		return fmt.Errorf("failed to apply hostname at startup: %w", err)
	}

	if err := s.dhcpcd.SetShareHostname(settings.ShareWithDhcp); err != nil {
		return fmt.Errorf("failed to apply dhcpcd share-hostname config at startup: %w", err)
	}

	return nil
}

// restartDhcpOnActiveInterfaces restarts dhcpcd@ only on interfaces that are
// currently DHCP-addressed and up, so the new config takes effect. Failures
// are logged, not returned, since the hostname/share change has already been
// persisted and applied — a restart failure shouldn't roll that back.
func (s *HostnameService) restartDhcpOnActiveInterfaces() {
	ifaces, err := s.ifaceService.GetDataLayerInterface()
	if err != nil {
		log.Printf("[HostnameService] Failed to list interfaces for dhcpcd restart: %v", err)
		return
	}

	for _, iface := range ifaces {
		if iface.AddressingMode != "dhcp" || iface.Status != "up" {
			continue
		}
		log.Printf("[HostnameService] Restarting dhcpcd@%s to apply hostname sharing change", iface.Name)
		if err := s.dhcpcd.RestartDhcpcd(iface.Name); err != nil {
			log.Printf("[HostnameService] Failed to restart dhcpcd for %s: %v", iface.Name, err)
		}
	}
}
