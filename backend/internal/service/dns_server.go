package service

import (
	"fmt"
	"log"
	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type DNSServerService struct {
	repo    *db.Repository
	manager kernel.DNSServerManager
}

func NewDNSServerService(repo *db.Repository, manager kernel.DNSServerManager) *DNSServerService {
	return &DNSServerService{
		repo:    repo,
		manager: manager,
	}
}

// ApplyAll applies all enabled DNS Zones and their records to dnsmasq
func (s *DNSServerService) ApplyAll() error {
	log.Println("[DNSServerService] Applying all DNS zones configurations")

	zones, err := s.repo.GetDNSZones()
	if err != nil {
		return fmt.Errorf("failed to retrieve DNS zones from database: %w", err)
	}

	enabledZones := []model.DNSZone{}
	for _, z := range zones {
		if z.Enabled {
			enabledZones = append(enabledZones, z)
		}
	}

	dhcpConfigs, err := s.repo.GetDHCPConfigs()
	if err != nil {
		return fmt.Errorf("failed to retrieve DHCP configs from database: %w", err)
	}

	var interfaces []string
	for _, cfg := range dhcpConfigs {
		if cfg.Enabled {
			interfaces = append(interfaces, cfg.Interface)
		}
	}

	if len(interfaces) == 0 {
		interfaces = []string{"eth0"}
	}

	if err := s.manager.ApplyZones(enabledZones, interfaces); err != nil {
		return fmt.Errorf("failed to apply DNS zone configurations: %w", err)
	}

	return nil
}

// InitApplyConfig applies DNS settings on boot
func (s *DNSServerService) InitApplyConfig() error {
	log.Println("[DNSServerService] Initializing DNS Server configurations")
	if err := s.ApplyAll(); err != nil {
		log.Printf("[DNSServerService] Warning during InitApplyConfig: %v", err)
	}
	return nil
}

// ClearCache clears the dnsmasq DNS cache
func (s *DNSServerService) ClearCache() error {
	return s.manager.ClearCache()
}
