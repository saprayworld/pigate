package service

import (
	"context"
	"log"
	"sort"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"
)

// FQDNRefresher is a background ticker (issue #141, docs/ref/todo/
// fqdn-retry-and-monitored-counters-plan.md D-1) that periodically
// re-resolves every FQDN address-object entry the last successful
// ApplyRules actually used, and triggers exactly one
// FirewallService.SyncFirewallRules() call per tick when — and only when —
// at least one FQDN's resolved IPv4 set has changed. This solves the "boot
// before DNS is ready" problem (a FQDN that failed to resolve on the first
// apply used to stay unresolved forever, until something else happened to
// trigger a reapply) without reapplying on every tick regardless of change,
// which would flush nft counters (colliding with the persisted "Monitor"
// counters feature, D-4/D-5) and generate log noise for no reason.
//
// Modeled on DhcpHealthChecker's ticker shape (dhcp_health_checker.go):
// single goroutine, a resettable time.Ticker, and a strict 3-level guard
// order on every tick (mock mode -> enabled -> bus paused).
type FQDNRefresher struct {
	repo           *db.Repository
	firewall       *FirewallService
	fwKernel       kernel.FirewallManager
	bus            *NetEventBus
	eventLog       *EventLogService
	enabled        bool
	steadyInterval time.Duration
	retryInterval  time.Duration
}

// NewFQDNRefresher constructs the refresher. It does not start any
// goroutine itself — call Start(ctx) once startup wiring is complete.
// enabled/steadyInterval/retryInterval come from the resolved
// config.Config's fqdn-refresh-* keys (file-only, D-3) — this package does
// not import internal/config directly (same rationale as other services'
// config-derived constructor parameters).
func NewFQDNRefresher(repo *db.Repository, firewall *FirewallService, fwKernel kernel.FirewallManager, bus *NetEventBus, eventLog *EventLogService, enabled bool, steadyInterval, retryInterval time.Duration) *FQDNRefresher {
	return &FQDNRefresher{
		repo:           repo,
		firewall:       firewall,
		fwKernel:       fwKernel,
		bus:            bus,
		eventLog:       eventLog,
		enabled:        enabled,
		steadyInterval: steadyInterval,
		retryInterval:  retryInterval,
	}
}

// Start launches the periodic background loop.
func (r *FQDNRefresher) Start(ctx context.Context) {
	go r.run(ctx)
}

func (r *FQDNRefresher) run(ctx context.Context) {
	interval := r.steadyInterval
	if interval <= 0 {
		interval = 300 * time.Second
	}
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			anyUnresolved := r.tick()

			newInterval := r.steadyInterval
			if anyUnresolved {
				newInterval = r.retryInterval
			}
			if newInterval <= 0 {
				newInterval = interval
			}
			if newInterval != interval {
				log.Printf("[FQDNRefresher] switching cadence %s -> %s (anyUnresolved=%t)", interval, newInterval, anyUnresolved)
				interval = newInterval
				t.Reset(interval)
			}
		}
	}
}

// tick runs one refresh pass. Guard order (mock mode -> enabled -> bus
// paused) mirrors DhcpHealthChecker.tick — mock mode is checked first so
// this never touches anything kernel-shaped in dev/mock runs; bus-paused is
// checked last so a backup import in progress never races a reapply here
// (Caution 5). Returns whether at least one FQDN entry is still
// unresolved after this pass, so run() can pick the retry/steady cadence.
func (r *FQDNRefresher) tick() (anyUnresolved bool) {
	if r.repo.IsMockMode() {
		return false
	}
	if !r.enabled {
		return false
	}
	if r.bus.IsPaused() {
		return false
	}

	snapshot := r.fwKernel.FQDNResolutions()
	if len(snapshot) == 0 {
		return false
	}

	current := make(map[string][]string, len(snapshot))
	for fqdn := range snapshot {
		ips, err := kernel.ResolveFQDNIPv4(fqdn)
		if err != nil {
			current[fqdn] = nil
			continue
		}
		strs := make([]string, len(ips))
		for i, ip := range ips {
			strs[i] = ip.String()
		}
		current[fqdn] = strs
	}

	changed, unresolved := diffResolutions(snapshot, current)
	anyUnresolved = unresolved

	if len(changed) == 0 {
		return anyUnresolved
	}

	log.Printf("[FQDNRefresher] detected resolution change for %d FQDN(s): %v — triggering SyncFirewallRules", len(changed), changed)
	if err := r.firewall.SyncFirewallRules(); err != nil {
		log.Printf("[FQDNRefresher] SyncFirewallRules failed after FQDN change: %v", err)
		return anyUnresolved
	}

	if r.eventLog != nil {
		msg := "FQDN re-resolved with a changed IPv4 set, firewall rules re-applied: " + joinFQDNChangeDesc(changed, snapshot, current)
		r.eventLog.Log(model.EventCategoryFirewall, "firewall.fqdn_refreshed", model.EventSeverityInfo, "system", "", msg)
	}

	return anyUnresolved
}

// diffResolutions is a pure function (no kernel/DB access) so it can be unit
// tested directly: given the FQDN -> resolved-IPv4-strings snapshot the last
// ApplyRules used (old) and a freshly re-resolved set (new, same keys),
// returns the list of FQDN names whose resolved IP set changed (order-
// sensitive comparison — see D-2: both sides are already sorted+capped by
// the shared ResolveFQDNIPv4 helper, so an order difference IS a real
// change, not just resolver reordering) and whether at least one FQDN in
// new resolved to nothing (anyUnresolved — drives the retry/steady cadence
// choice in run()).
func diffResolutions(old, new map[string][]string) (changed []string, anyUnresolved bool) {
	keys := make([]string, 0, len(old))
	for k := range old {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, fqdn := range keys {
		newIPs := new[fqdn]
		if len(newIPs) == 0 {
			anyUnresolved = true
		}
		if !stringSlicesEqual(old[fqdn], newIPs) {
			changed = append(changed, fqdn)
		}
	}
	return changed, anyUnresolved
}

func stringSlicesEqual(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// joinFQDNChangeDesc renders a short "fqdn: old -> new" summary for the
// event log message, for every FQDN in changed.
func joinFQDNChangeDesc(changed []string, old, new map[string][]string) string {
	out := ""
	for i, fqdn := range changed {
		if i > 0 {
			out += "; "
		}
		out += fqdn + ": " + describeIPs(old[fqdn]) + " -> " + describeIPs(new[fqdn])
	}
	return out
}

func describeIPs(ips []string) string {
	if len(ips) == 0 {
		return "(unresolved)"
	}
	out := ips[0]
	for _, ip := range ips[1:] {
		out += "," + ip
	}
	return out
}
