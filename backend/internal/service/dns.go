package service

import (
	"fmt"
	"log"
	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type DNSService struct {
	repo   *db.Repository
	dnsMgr kernel.DNSManager
}

func NewDNSService(repo *db.Repository, dnsMgr kernel.DNSManager) *DNSService {
	return &DNSService{
		repo:   repo,
		dnsMgr: dnsMgr,
	}
}

// GetDNSConfig retrieves the current DNS configuration, combining the database state with real-time OS-level DNS info for active WAN links.
func (s *DNSService) GetDNSConfig() (*model.DNSConfig, error) {
	cfg, err := s.repo.GetDNSConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to retrieve DNS config from database: %w", err)
	}

	// Query DB for interface configs to locate active WAN interfaces
	interfaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		log.Printf("[DNSService] Warning: Failed to retrieve network interfaces from DB: %v", err)
		return cfg, nil
	}

	cfg.DynamicDNS = []model.DynamicDNSServer{}
	for _, iface := range interfaces {
		if iface.Role == "WAN" && iface.Status == "up" {
			dnsList, err := s.dnsMgr.GetLinkDNS(iface.Name)
			if err == nil && len(dnsList) > 0 {
				cfg.DynamicDNS = append(cfg.DynamicDNS, model.DynamicDNSServer{
					InterfaceName:  iface.Name,
					InterfaceAlias: iface.Alias,
					DNSServers:     dnsList,
				})
			} else if err != nil {
				log.Printf("[DNSService] Warning: Failed to read link DNS for %s: %v", iface.Name, err)
			}
		}
	}

	return cfg, nil
}

// UpdateDNSConfig saves the updated configuration payload to the DB and applies it to systemd-resolved immediately.
func (s *DNSService) UpdateDNSConfig(input model.DNSConfigInput) error {
	if input.Mode != "wan" && input.Mode != "static" {
		return fmt.Errorf("invalid mode: %s", input.Mode)
	}

	if err := s.repo.UpdateDNSConfig(input); err != nil {
		return fmt.Errorf("failed to update DNS config in database: %w", err)
	}

	return s.ApplyDNSConfig()
}

// ApplyDNSConfig synchronizes SQLite DNS configurations with systemd-resolved links and global drop-ins.
func (s *DNSService) ApplyDNSConfig() error {
	cfg, err := s.repo.GetDNSConfig()
	if err != nil {
		return fmt.Errorf("failed to fetch DNS config: %w", err)
	}

	interfaces, err := s.repo.GetInterfacesFromDB()
	if err != nil {
		return fmt.Errorf("failed to query network interfaces: %w", err)
	}

	if cfg.Mode == "static" {
		var servers []string
		if cfg.PrimaryDNS != "" {
			servers = append(servers, cfg.PrimaryDNS)
		}
		if cfg.SecondaryDNS != "" {
			servers = append(servers, cfg.SecondaryDNS)
		}

		log.Printf("[DNSService] Applying global static DNS: %v, Local domain: %s", servers, cfg.LocalDomain)
		if err := s.dnsMgr.SetGlobalDNS(servers, cfg.LocalDomain); err != nil {
			log.Printf("[DNSService] Warning: SetGlobalDNS failed: %v", err)
		}

		// Configure static DNS for each WAN link
		for _, iface := range interfaces {
			if iface.Role == "WAN" {
				log.Printf("[DNSService] Applying DNS %v to WAN link %s", servers, iface.Name)
				if err := s.dnsMgr.SetLinkDNS(iface.Name, servers); err != nil {
					log.Printf("[DNSService] Warning: Failed to set link DNS for %s: %v", iface.Name, err)
				}
			}
		}
	} else {
		log.Printf("[DNSService] Restoring dynamic DHCP DNS mode. Clearing static configuration.")
		// Revert to DHCP default resolution (remove drop-in)
		if err := s.dnsMgr.SetGlobalDNS([]string{}, ""); err != nil {
			log.Printf("[DNSService] Warning: Reverting Global DNS failed: %v", err)
		}

		// Revert each WAN link to DHCP configurations
		for _, iface := range interfaces {
			if iface.Role == "WAN" {
				log.Printf("[DNSService] Reverting WAN link %s to dynamic DHCP DNS", iface.Name)
				if err := s.dnsMgr.RevertLinkDNS(iface.Name); err != nil {
					log.Printf("[DNSService] Warning: Failed to revert link DNS for %s: %v", iface.Name, err)
				}
			}
		}
	}

	return nil
}
