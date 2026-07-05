package service

import (
	"fmt"
	"log"
	"net"
	"strings"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

type DNSServerService struct {
	repo       *db.Repository
	manager    kernel.DNSServerManager
	dnsService *DNSService
}

func NewDNSServerService(repo *db.Repository, manager kernel.DNSServerManager, dnsService *DNSService) *DNSServerService {
	return &DNSServerService{
		repo:       repo,
		manager:    manager,
		dnsService: dnsService,
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

	interfaces, err := s.repo.GetDNSServerInterfaces()
	if err != nil {
		return fmt.Errorf("failed to retrieve DNS server interfaces from database: %w", err)
	}

	upstreams := s.resolveUpstreams()

	if err := s.manager.ApplyZones(enabledZones, interfaces, upstreams); err != nil {
		return fmt.Errorf("failed to apply DNS zone configurations: %w", err)
	}

	return nil
}

// resolveUpstreams collects the explicit upstream DNS servers dnsmasq should
// forward to, drawn from the System DNS configuration (never read from the repo
// directly: in "wan" mode the effective upstreams live on the per-link DNS of
// systemd-resolved, which only DNSService.GetDNSConfig() aggregates).
func (s *DNSServerService) resolveUpstreams() []string {
	if s.dnsService == nil {
		return nil
	}

	cfg, err := s.dnsService.GetDNSConfig()
	if err != nil {
		log.Printf("[DNSServerService] Warning: cannot read system DNS config: %v", err)
		return nil
	}

	var servers []string
	if cfg.Mode == "static" {
		if cfg.PrimaryDNS != "" {
			servers = append(servers, cfg.PrimaryDNS)
		}
		if cfg.SecondaryDNS != "" {
			servers = append(servers, cfg.SecondaryDNS)
		}
	} else { // mode == "wan": use the DNS the WAN link got via DHCP
		for _, d := range cfg.DynamicDNS {
			servers = append(servers, d.DNSServers...)
		}
	}

	return sanitizeUpstreams(servers)
}

// sanitizeUpstreams trims, drops empties/loopback addresses, and de-duplicates a
// list of upstream DNS server IPs. Loopback (127.0.0.0/8, ::1) is excluded to
// avoid a forwarding loop between dnsmasq and systemd-resolved's stub resolver.
func sanitizeUpstreams(servers []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range servers {
		ip := strings.TrimSpace(raw)
		if ip == "" || seen[ip] {
			continue
		}
		if parsed := net.ParseIP(ip); parsed != nil && parsed.IsLoopback() {
			continue
		}
		seen[ip] = true
		out = append(out, ip)
	}
	return out
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
