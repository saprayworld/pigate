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

	settings, err := s.repo.GetDNSServerSettings()
	if err != nil {
		return fmt.Errorf("failed to retrieve DNS server settings from database: %w", err)
	}

	upstreams := s.resolveUpstreams(settings)

	blocked, err := s.repo.GetBlockedDomains()
	if err != nil {
		return fmt.Errorf("failed to retrieve blocked domains from database: %w", err)
	}

	// QueryLogging is the only DNS Statistics field that affects the dnsmasq
	// config (TTL/cap are pure service-layer parameters — plan T-07: "ไม่ต้อง
	// ส่ง TTL/cap ไม่เกี่ยวกับ dnsmasq").
	if err := s.manager.ApplyZones(enabledZones, settings.Interfaces, upstreams, settings.QueryLogging, blocked); err != nil {
		return fmt.Errorf("failed to apply DNS zone configurations: %w", err)
	}

	return nil
}

// resolveUpstreams picks the upstream DNS servers dnsmasq should forward to,
// depending on settings.UpstreamMode (docs/ref/todo/
// dns-server-settings-tab-and-upstream-plan.md §2/T-03):
//   - "custom": settings.UpstreamServers only — DNSService is never consulted.
//   - "system" (default, backward compatible): drawn from the System DNS
//     configuration. This is a *read-only* snapshot taken at generate-time —
//     never cached, never written back. In "wan" mode the effective upstreams
//     live on the per-link DNS of systemd-resolved, which only
//     DNSService.GetDNSConfig() aggregates. Saving System DNS itself never
//     triggers this method (that write path was removed — T-06); the value
//     only takes effect the next time the DNS Server's own config is
//     generated (Apply DNS Zones / settings save / boot / restore).
func (s *DNSServerService) resolveUpstreams(settings model.DNSServerSettings) []string {
	if settings.UpstreamMode == model.DNSUpstreamModeCustom {
		return sanitizeUpstreams(settings.UpstreamServers)
	}

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

// sanitizeUpstreams trims, drops empties/loopback addresses/values that don't
// parse as an IP, and de-duplicates a list of upstream DNS server candidates.
// Loopback (127.0.0.0/8, ::1) is excluded to avoid a forwarding loop between
// dnsmasq and systemd-resolved's stub resolver. Values that fail
// net.ParseIP are dropped here too (defense-in-depth — T-01/T-05 already
// reject them at the API boundary for the "custom" path, but "system" values
// come from DNSService, and DB-imported settings can bypass the handler).
func sanitizeUpstreams(servers []string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, raw := range servers {
		ip := strings.TrimSpace(raw)
		if ip == "" || seen[ip] {
			continue
		}
		parsed := net.ParseIP(ip)
		if parsed == nil || parsed.IsLoopback() {
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
