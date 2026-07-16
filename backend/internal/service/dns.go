package service

import (
	"fmt"
	"log"
	"sync"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type DNSService struct {
	repo   *db.Repository
	dnsMgr kernel.DNSManager

	// mu guards lastSig against concurrent ApplyDNSConfig calls — it is invoked
	// both from the HTTP handler (UpdateDNSConfig) and from the netlink event bus
	// goroutine (InterfaceAdded self-heal), which can race.
	mu sync.Mutex
	// lastSig is the signature of the config last applied successfully. When an
	// incoming apply matches it, we skip the work entirely — most importantly the
	// systemd-resolved restart — so a Wi-Fi flap storm no longer thrashes DNS
	// while the config never actually changed (issue #57).
	lastSig string
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

	// Idempotency guard: skip the whole apply (and the systemd-resolved restart it
	// triggers) when nothing changed since the last successful apply. This is what
	// stops a Wi-Fi scan/reconnect flap from restarting DNS repeatedly while the
	// config is identical (issue #57). The signature covers every field that
	// affects the resolved drop-in.
	sig := fmt.Sprintf("%s|%s|%s|%s", cfg.Mode, cfg.PrimaryDNS, cfg.SecondaryDNS, cfg.LocalDomain)
	s.mu.Lock()
	defer s.mu.Unlock()
	if sig == s.lastSig {
		log.Printf("[DNSService] DNS config unchanged, skipping re-apply (no resolved restart)")
		return nil
	}

	if cfg.Mode == "static" {
		var servers []string
		if cfg.PrimaryDNS != "" {
			servers = append(servers, cfg.PrimaryDNS)
		}
		if cfg.SecondaryDNS != "" {
			servers = append(servers, cfg.SecondaryDNS)
		}

		// Global drop-in only. We intentionally do NOT push per-link DNS via
		// resolve1.Link.SetDNS: it requires a per-link Polkit privilege the pigate
		// user lacks (it was failing with "Permission denied" on every call), so
		// working resolution already comes entirely from the global drop-in. The
		// per-link loop only produced noise and errors — see issue #57.
		log.Printf("[DNSService] Applying global static DNS: %v, Local domain: %s", servers, cfg.LocalDomain)
		if err := s.dnsMgr.SetGlobalDNS(servers, cfg.LocalDomain); err != nil {
			log.Printf("[DNSService] Warning: SetGlobalDNS failed: %v", err)
			return fmt.Errorf("failed to apply global DNS: %w", err)
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

	// Record what we just applied so an identical subsequent call short-circuits.
	s.lastSig = sig
	return nil
}
