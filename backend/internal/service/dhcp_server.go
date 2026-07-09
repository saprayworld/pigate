package service

import (
	"context"
	"fmt"
	"log"
	"net"
	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type DhcpServerService struct {
	repo     *db.Repository
	manager  kernel.DhcpManager
	eventLog *EventLogService
}

func NewDhcpServerService(repo *db.Repository, manager kernel.DhcpManager) *DhcpServerService {
	return &DhcpServerService{
		repo:    repo,
		manager: manager,
	}
}

// SetEventLog injects the central event log (wired in main.go after both
// services exist). Nil is tolerated — lease events are then simply not logged.
func (s *DhcpServerService) SetEventLog(eventLog *EventLogService) {
	s.eventLog = eventLog
}

// ApplyAll applies all enabled DHCP configurations and MAC reservations from DB to dnsmasq
func (s *DhcpServerService) ApplyAll() error {
	log.Println("[DhcpServerService] Applying all DHCP configs")

	// 1. Check for interface role conflicts (avoid setting DHCP Server on WAN interface)
	ifaces, err := s.repo.GetInterfaces()
	if err != nil {
		return fmt.Errorf("failed to retrieve interfaces from database: %w", err)
	}

	wanInterfaces := make(map[string]bool)
	for _, iface := range ifaces {
		if iface.Role == "WAN" {
			wanInterfaces[iface.Name] = true
		}
	}

	cfgs, err := s.repo.GetDHCPConfigs()
	if err != nil {
		return fmt.Errorf("failed to retrieve DHCP configs: %w", err)
	}

	enabledCfgs := []model.DhcpConfig{}
	for _, cfg := range cfgs {
		if cfg.Enabled {
			// Check if interface is WAN
			if wanInterfaces[cfg.Interface] {
				return fmt.Errorf("interface %s is used as WAN (DHCP Client) and cannot be configured as a DHCP Server", cfg.Interface)
			}
			enabledCfgs = append(enabledCfgs, cfg)
		}
	}

	// 2. Fetch all DHCP Reservations
	reservations, err := s.repo.GetDHCPReservations()
	if err != nil {
		return fmt.Errorf("failed to retrieve DHCP reservations: %w", err)
	}

	// 3. Apply configurations to dnsmasq
	if err := s.manager.ApplyConfig(enabledCfgs, reservations); err != nil {
		return fmt.Errorf("failed to apply DHCP configuration: %w", err)
	}

	return nil
}

// InitApplyConfig executes ApplyAll on system startup
func (s *DhcpServerService) InitApplyConfig() error {
	log.Println("[DhcpServerService] Initializing DHCP Server configurations")
	// If in mock mode, it shouldn't fail startup if D-Bus is unavailable
	if err := s.ApplyAll(); err != nil {
		log.Printf("[DhcpServerService] Warning during InitApplyConfig: %v", err)
	}
	return nil
}

// findInterfaceForIP maps an IP address to the LAN interface matching its subnet network
func (s *DhcpServerService) findInterfaceForIP(ipStr string, cfgs []model.DhcpConfig) string {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		return ""
	}
	for _, cfg := range cfgs {
		maskParts := net.ParseIP(cfg.Netmask)
		gwIP := net.ParseIP(cfg.Gateway)
		if maskParts != nil && gwIP != nil {
			maskIP := maskParts.To4()
			gwIP4 := gwIP.To4()
			if maskIP != nil && gwIP4 != nil {
				mask := net.IPv4Mask(maskIP[0], maskIP[1], maskIP[2], maskIP[3])
				ipNet := net.IPNet{IP: gwIP4.Mask(mask), Mask: mask}
				if ipNet.Contains(ip) {
					return cfg.Interface
				}
			}
		}
	}
	return ""
}

// StartLeaseWatcher registers context and receives events from D-Bus and caches it to SQLite
func (s *DhcpServerService) StartLeaseWatcher(ctx context.Context) error {
	callback := func(event string, lease model.ActiveDhcpLease) {
		log.Printf("[DhcpServerService] D-Bus lease event: %s, MAC: %s, IP: %s", event, lease.MacAddress, lease.IPAddress)

		var dbErr error
		if event == "added" || event == "updated" {
			// Find configured interface matching the subnet
			cfgs, err := s.repo.GetDHCPConfigs()
			if err == nil {
				lease.Interface = s.findInterfaceForIP(lease.IPAddress, cfgs)
			}
			// dnsmasq also fires lease events on every renew (half the lease
			// time, per client) — logging those would flood the event table.
			// Only log when the MAC is new or its IP actually changed.
			existing, _ := s.repo.GetDHCPLeaseByMAC(lease.MacAddress)
			dbErr = s.repo.UpsertDHCPLease(lease)
			if dbErr == nil && s.eventLog != nil && (existing == nil || existing.IPAddress != lease.IPAddress) {
				s.eventLog.Log(model.EventCategoryDhcp, "dhcp.lease.add", model.EventSeverityInfo,
					model.EventActorSystem, lease.MacAddress,
					"DHCP lease "+lease.IPAddress+" assigned to "+lease.MacAddress+" ("+lease.Hostname+")")
			}
		} else if event == "deleted" {
			dbErr = s.repo.DeleteDHCPLease(lease.MacAddress)
			if dbErr == nil && s.eventLog != nil {
				s.eventLog.Log(model.EventCategoryDhcp, "dhcp.lease.remove", model.EventSeverityInfo,
					model.EventActorSystem, lease.MacAddress,
					"DHCP lease "+lease.IPAddress+" released by "+lease.MacAddress)
			}
		}

		if dbErr != nil {
			log.Printf("[DhcpServerService] Error updating lease in DB: %v", dbErr)
		}
	}

	return s.manager.WatchLeases(ctx, callback)
}
