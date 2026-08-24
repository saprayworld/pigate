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

	// blockedDomainsSink is optional (nil until SetBlockedDomainsSink is
	// called by main.go, mirroring PolicyStatsService.SetDomainLookup's
	// pattern) — wired to StatisticsService.SetBlockedDomains so ApplyAll
	// can prime the RAM-only deny-list matcher behind the "Blocked Domain
	// Query" statistics feature (docs/ref/todo/
	// dns-blocked-query-statistics-plan.md T-08). NOT a NewDNSServerService
	// parameter, to avoid changing that constructor's existing signature.
	blockedDomainsSink func([]model.BlockedDomain)

	// blocklistProvider (docs/ref/todo/dns-blocklist-import-plan.md T-05) is
	// optional (nil until SetBlocklistProvider is called by main.go) — wired
	// to *DNSBlocklistService so ApplyAll can pull the current blocklist
	// manifest and forward every enabled, non-empty list to
	// kernel.ApplyZones. NOT a NewDNSServerService parameter, for the same
	// reason as blockedDomainsSink above (avoid changing that constructor's
	// existing signature).
	blocklistProvider *DNSBlocklistService

	// blocklistSink mirrors blockedDomainsSink but for the bulk-import
	// blocklist feature's own statistics index
	// (StatisticsService.SetBlocklists, T-06) — invoked with the SAME
	// blocklists that were just applied, only AFTER ApplyZones succeeds.
	blocklistSink func([]model.DNSBlocklist)
}

func NewDNSServerService(repo *db.Repository, manager kernel.DNSServerManager, dnsService *DNSService) *DNSServerService {
	return &DNSServerService{
		repo:       repo,
		manager:    manager,
		dnsService: dnsService,
	}
}

// SetBlockedDomainsSink wires the callback ApplyAll invokes, right after a
// successful ApplyZones, with the exact same deny-list it just applied
// (docs/ref/todo/dns-blocked-query-statistics-plan.md T-08). In production
// this is statisticsService.SetBlockedDomains — main.go must call this
// BEFORE InitApplyConfig() so the index is primed at boot, not just from the
// first later Apply DNS Zones. Safe to call with nil (the zero value —
// ApplyAll simply skips the callback then, e.g. in tests that never wire a
// StatisticsService).
func (s *DNSServerService) SetBlockedDomainsSink(fn func([]model.BlockedDomain)) {
	s.blockedDomainsSink = fn
}

// SetBlocklistProvider wires the *DNSBlocklistService ApplyAll pulls the
// current blocklist manifest from (docs/ref/todo/
// dns-blocklist-import-plan.md T-05). In production this is main.go's
// DNSBlocklistService instance. Not a constructor parameter, mirroring
// SetBlockedDomainsSink above. Safe to leave unset (nil) — ApplyAll then
// simply passes no blocklists to kernel.ApplyZones, e.g. in tests.
func (s *DNSServerService) SetBlocklistProvider(p *DNSBlocklistService) {
	s.blocklistProvider = p
}

// SetBlocklistSink wires the callback ApplyAll invokes, right after a
// successful ApplyZones, with the same blocklist manifest it just used to
// build the applied refs (docs/ref/todo/dns-blocklist-import-plan.md T-06).
// In production this is statisticsService.SetBlocklists. Safe to call with
// nil (ApplyAll simply skips the callback).
func (s *DNSServerService) SetBlocklistSink(fn func([]model.DNSBlocklist)) {
	s.blocklistSink = fn
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
	// blocklists (docs/ref/todo/dns-blocklist-import-plan.md T-05): pulled
	// from blocklistProvider (if wired) and filtered down to exactly the
	// lists ApplyZones' kernel implementation is allowed to reference — only
	// Enabled lists with DomainCount>0 (a list with 0 domains has nothing
	// useful to render, and a disabled list must not be enforced).
	var manifestBlocklists []model.DNSBlocklist
	var blocklistRefs []model.BlocklistRef
	if s.blocklistProvider != nil {
		manifestBlocklists = s.blocklistProvider.List()
		for _, l := range manifestBlocklists {
			if l.Enabled && l.DomainCount > 0 {
				blocklistRefs = append(blocklistRefs, model.BlocklistRef{ID: l.ID, BlockMode: l.BlockMode})
			}
		}
	}

	if err := s.manager.ApplyZones(enabledZones, settings.Interfaces, upstreams, settings.QueryLogging, blocked, blocklistRefs); err != nil {
		return fmt.Errorf("failed to apply DNS zone configurations: %w", err)
	}

	// Prime/refresh the RAM-only deny-list matcher behind the "Blocked
	// Domain Query" statistics feature with the SAME list that was just
	// successfully applied (docs/ref/todo/dns-blocked-query-statistics-plan.md
	// T-08) — deliberately AFTER the ApplyZones success check above, never
	// before: a DB row that failed to apply must not be reflected as
	// "currently enforced" by the statistics classifier. A panic/error from
	// the sink itself must never fail ApplyAll (this is display-only
	// statistics wiring, not a boot/apply dependency).
	if s.blockedDomainsSink != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[DNSServerService] Warning: blocked-domains sink panicked: %v", r)
				}
			}()
			s.blockedDomainsSink(blocked)
		}()
	}

	// Same pattern as blockedDomainsSink above, for the blocklist import
	// feature's statistics index (T-06) — invoked with the manifest snapshot
	// that produced blocklistRefs above, only after ApplyZones succeeded.
	if s.blocklistSink != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("[DNSServerService] Warning: blocklist sink panicked: %v", r)
				}
			}()
			s.blocklistSink(manifestBlocklists)
		}()
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
