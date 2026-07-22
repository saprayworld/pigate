package service

import (
	"context"
	"fmt"
	"log"
	"net"
	"sync"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
	"pigate/internal/model"

	"github.com/vishvananda/netlink"
)

// DhcpHealthChecker is a background self-heal loop (issue #78) that detects
// interfaces in DHCP mode which are carrier-ready (isUp && isRunning) but
// stuck without a real IPv4 address — either holding only a link-local
// 169.254.x.x/16 (APIPA) address or no IPv4 address at all — and self-heals
// them: it deletes the stray 169.254 address when a real IP coexists on the
// same interface, or restarts dhcpcd for that interface when there is no
// real IP to preserve. All thresholds are read live from
// dhcp_health_settings on every tick so they can be tuned at runtime via the
// REST API without restarting pigate.
//
// See docs/ref/todo/dhcpcd-link-local-fallback-plan.md for the full design
// rationale, including why this is a periodic ticker (not event-driven —
// the "no IPv4 at all" case never fires an Address netlink event to hook
// into) and the state-machine decisions codified in decideNextState.
type DhcpHealthChecker struct {
	repo          *db.Repository
	ifaceService  *InterfaceService
	dhcpcdService *DhcpcdService
	network       kernel.NetworkManager
	eventLog      *EventLogService
	bus           *NetEventBus

	mu     sync.Mutex
	states map[string]*ifaceHealthState
}

// ifaceHealthState is per-interface, RAM-only bookkeeping for the checker.
// It is deliberately never persisted to SQLite (SD-card write-cycle
// preservation, tech_stack_design.md §8): a pigate restart resets it, which
// is an accepted known limitation (see Caution 12 in the plan).
type ifaceHealthState struct {
	strikes int
	// runningSince is the time isRunning was last observed to become true for
	// this interface (or zero if not currently tracked as running). Used to
	// enforce MinRunningSeconds before counting any strikes, so a fresh
	// reconnect is given time to settle.
	runningSince time.Time
	// restartsSinceRecover counts restarts performed since the interface was
	// last genuinely healthy (hasReal && !hasLinkLocal). Only reset on true
	// recovery, not on every loss of eligibility (Caution 5).
	restartsSinceRecover int
	lastRestartAt        time.Time
	// ceilingLogged ensures the "restart ceiling reached" event is logged
	// only once per episode, not on every tick while stuck.
	ceilingLogged bool
}

// healthAction is the outcome of decideNextState for one interface on one tick.
type healthAction int

const (
	actionNone healthAction = iota
	actionDeleteAddr
	actionRestart
	actionRestartSkippedBackoff
	actionRestartCeilingReached
)

// NewDhcpHealthChecker constructs the checker. It does not start any
// goroutine by itself — call Start(ctx) once startup wiring is complete.
func NewDhcpHealthChecker(
	repo *db.Repository,
	ifaceService *InterfaceService,
	dhcpcdService *DhcpcdService,
	network kernel.NetworkManager,
	eventLog *EventLogService,
	bus *NetEventBus,
) *DhcpHealthChecker {
	return &DhcpHealthChecker{
		repo:          repo,
		ifaceService:  ifaceService,
		dhcpcdService: dhcpcdService,
		network:       network,
		eventLog:      eventLog,
		bus:           bus,
		states:        make(map[string]*ifaceHealthState),
	}
}

// Start launches the periodic background loop. It is a background self-heal
// loop like NetlinkMonitor, not part of the startup-apply sequence, so
// main.go calls it after netlinkMonitor.Start(...) rather than inserting it
// into the 6.0-6.5 apply steps.
func (c *DhcpHealthChecker) Start(ctx context.Context) {
	go c.run(ctx)
}

func (c *DhcpHealthChecker) run(ctx context.Context) {
	// Seed the ticker from whatever is in the DB right now; every tick below
	// re-reads CheckIntervalSeconds and resets the ticker if it changed, so a
	// runtime settings change via the API takes effect without a restart.
	interval := defaultDhcpHealthCheckInterval
	if settings, err := c.repo.GetDhcpHealthSettings(); err == nil && settings.CheckIntervalSeconds > 0 {
		interval = time.Duration(settings.CheckIntervalSeconds) * time.Second
	}

	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			settings, err := c.repo.GetDhcpHealthSettings()
			if err != nil {
				log.Printf("[DhcpHealthChecker] failed to read settings: %v", err)
				continue
			}
			c.tick(*settings)

			newInterval := time.Duration(settings.CheckIntervalSeconds) * time.Second
			if newInterval > 0 && newInterval != interval {
				log.Printf("[DhcpHealthChecker] check interval changed %s -> %s; resetting ticker", interval, newInterval)
				interval = newInterval
				t.Reset(interval)
			}
		}
	}
}

// defaultDhcpHealthCheckInterval seeds the ticker before the first DB read
// only; the seeded row's own default (60s) is what actually ships (T-04).
const defaultDhcpHealthCheckInterval = 60 * time.Second

// tick runs one pass over every DHCP-mode interface. Guard order matters
// (see plan T-09): mock-mode guard first (never touch real netlink in mock
// mode), then the enabled flag, then bus-pause (skip the whole tick during a
// backup import so the checker never races a config restore).
func (c *DhcpHealthChecker) tick(settings model.DhcpHealthSettings) {
	if c.repo.IsMockMode() {
		return
	}
	if !settings.Enabled {
		return
	}
	if c.bus.IsPaused() {
		return
	}

	ifaces, err := c.ifaceService.GetDataLayerInterface()
	if err != nil {
		log.Printf("[DhcpHealthChecker] failed to list interfaces: %v", err)
		return
	}

	now := time.Now()
	seen := make(map[string]bool, len(ifaces))
	for _, iface := range ifaces {
		if iface.AddressingMode != "dhcp" {
			continue
		}
		seen[iface.Name] = true
		c.tickInterface(iface, settings, now)
	}

	// Drop RAM state for interfaces no longer present/DHCP so the map doesn't
	// grow unbounded across VLAN/interface churn. State is RAM-only anyway
	// (Caution 12), so this loses nothing that survives a restart regardless.
	c.mu.Lock()
	for name := range c.states {
		if !seen[name] {
			delete(c.states, name)
		}
	}
	c.mu.Unlock()
}

// tickInterface evaluates and, if warranted, acts on a single DHCP-mode
// interface. Live link flags are read via netlink.LinkByName directly
// (mirroring the existing precedent in dhcpcd.go's syncActiveInterface/
// SyncInterface — Caution 2 in the plan); reading/deleting IPv4 addresses is
// the new capability added in T-01/T-02 and goes through the kernel
// interface as required.
func (c *DhcpHealthChecker) tickInterface(iface model.NetworkInterface, settings model.DhcpHealthSettings, now time.Time) {
	name := iface.Name

	var isUp, isRunning bool
	if link, err := netlink.LinkByName(name); err == nil {
		if attrs := link.Attrs(); attrs != nil {
			isUp = attrs.Flags&net.FlagUp != 0
			isRunning = attrs.Flags&net.FlagRunning != 0
		}
	}
	// A link lookup failure (e.g. a USB Wi-Fi adapter unplugged) is treated
	// the same as "not up/not running" — decideNextState's eligibility gate
	// below handles it uniformly with no special case.

	c.mu.Lock()
	state, ok := c.states[name]
	if !ok {
		state = &ifaceHealthState{}
		c.states[name] = state
	}
	// Cheap pre-check so we don't bother reading live addresses from the
	// kernel while ineligible or still inside the MinRunningSeconds settle
	// window — decideNextState would discard hasReal/hasLinkLocal in that
	// case anyway (mirrors the pseudocode ordering in plan §2.2).
	minRunningGuardActive := !(isUp && isRunning) ||
		state.runningSince.IsZero() ||
		now.Sub(state.runningSince) < time.Duration(settings.MinRunningSeconds)*time.Second
	c.mu.Unlock()

	var hasReal, hasLinkLocal bool
	var addrs []string
	if isUp && isRunning && !minRunningGuardActive {
		var err error
		addrs, err = c.network.GetIPv4Addresses(name)
		if err != nil {
			log.Printf("[DhcpHealthChecker] failed to read IPv4 addresses for %s: %v", name, err)
			return
		}
		hasReal, hasLinkLocal = classifyAddrs(addrs)
	}

	c.mu.Lock()
	action := decideNextState(state, isUp, isRunning, hasReal, hasLinkLocal, settings, now)
	c.mu.Unlock()

	if action == actionNone || action == actionRestartSkippedBackoff {
		return
	}

	c.executeAction(iface, addrs, action)
}

// classifyAddrs splits a list of CIDR-formatted IPv4 addresses (as returned
// by kernel.NetworkManager.GetIPv4Addresses) into whether it contains at
// least one non-link-local ("real") address and/or at least one
// 169.254.x.x/16 link-local (APIPA) address. Pure and side-effect free so it
// can be unit tested without any kernel access.
func classifyAddrs(addrs []string) (hasReal, hasLinkLocal bool) {
	for _, cidr := range addrs {
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			// Tolerate a bare IP (no mask) in case a caller ever passes one.
			ip = net.ParseIP(cidr)
			if ip == nil {
				continue
			}
		}
		if ip.IsLinkLocalUnicast() {
			hasLinkLocal = true
		} else {
			hasReal = true
		}
	}
	return hasReal, hasLinkLocal
}

// findLinkLocalCIDR returns the first 169.254.x.x/16 CIDR in addrs, or "" if
// none is present. Used to pass the exact address (not a generic mask) to
// kernel.NetworkManager.DeleteAddress.
func findLinkLocalCIDR(addrs []string) string {
	for _, cidr := range addrs {
		ip, _, err := net.ParseCIDR(cidr)
		if err != nil {
			continue
		}
		if ip.IsLinkLocalUnicast() {
			return cidr
		}
	}
	return ""
}

// decideNextState is the pure core of the state machine: given the live
// link flags, classified addresses, current settings and time, it mutates
// state in place and returns the action the caller should perform. It has
// no side effects beyond mutating *state, so it is fully unit-testable
// without any kernel/DB access (T-10).
//
// State-machine rules enforced here (mandatory, see plan T-09 "ข้อตัดสินใจ
// state-machine"):
//  1. Any real action taken (deleteAddr OR restart) resets strikes to 0
//     immediately, but does NOT reset restartsSinceRecover/ceilingLogged —
//     those only reset on a genuine return to health (hasReal && !hasLinkLocal).
//  2. The deleteAddr branch has no backoff/ceiling of its own; the strike
//     reset from rule 1 is what naturally rate-limits repeated deletes.
//  3. An interface already isUp && isRunning on the very first tick (no
//     prior state) gets runningSince = now, deferring strike counting by one
//     MinRunningSeconds window.
func decideNextState(state *ifaceHealthState, isUp, isRunning, hasReal, hasLinkLocal bool, settings model.DhcpHealthSettings, now time.Time) healthAction {
	// Eligibility gate: uniform across every interface type (no Wi-Fi-only
	// carve-out like dhcpcd.go's applyDhcpcdDecisionLocked — Caution 3).
	// Skip completely: no strike counted, and the running-since timer is
	// cleared so a later reconnect starts its MinRunningSeconds window fresh.
	if !(isUp && isRunning) {
		state.strikes = 0
		state.runningSince = time.Time{}
		return actionNone
	}

	// Decision 3: first time this interface is observed already up+running,
	// start the running-since clock now rather than assuming it has been
	// running indefinitely.
	if state.runningSince.IsZero() {
		state.runningSince = now
	}

	if now.Sub(state.runningSince) < time.Duration(settings.MinRunningSeconds)*time.Second {
		return actionNone
	}

	// Genuinely healthy: full reset, including the backoff/ceiling bookkeeping
	// (Caution 5 — only a true recovery resets those, not every eligibility loss).
	if hasReal && !hasLinkLocal {
		state.strikes = 0
		state.restartsSinceRecover = 0
		state.ceilingLogged = false
		return actionNone
	}

	state.strikes++
	if state.strikes < settings.ConsecutiveStrikes {
		return actionNone
	}

	switch {
	case hasReal && hasLinkLocal:
		// Rule 2: no backoff/ceiling of its own; rule 1's strike reset below
		// is what limits how often this can fire.
		state.strikes = 0
		return actionDeleteAddr
	case now.Sub(state.lastRestartAt) < time.Duration(settings.RestartBackoffSeconds)*time.Second:
		return actionRestartSkippedBackoff
	case state.restartsSinceRecover >= settings.MaxRestartsBeforePause:
		if state.ceilingLogged {
			return actionNone
		}
		state.ceilingLogged = true
		return actionRestartCeilingReached
	default:
		state.strikes = 0
		state.restartsSinceRecover++
		state.lastRestartAt = now
		return actionRestart
	}
}

// executeAction performs the real, side-effecting part of an action decided
// by decideNextState, and logs it via EventLogService. Every real action
// (deleteAddr, restart, ceiling-reached notice) is logged; severities per
// plan T-09: warning for detect/restart, info for a successful address
// deletion, error for hitting the restart ceiling.
func (c *DhcpHealthChecker) executeAction(iface model.NetworkInterface, addrs []string, action healthAction) {
	name := iface.Name

	switch action {
	case actionDeleteAddr:
		cidr := findLinkLocalCIDR(addrs)
		if cidr == "" {
			// Should not happen (decideNextState only returns this when
			// hasLinkLocal was true), but guard defensively.
			return
		}
		c.eventLog.Log(model.EventCategoryDhcp, "dhcp.linklocal_detected", model.EventSeverityWarning,
			model.EventActorSystem, name,
			fmt.Sprintf("Interface %s is stuck with a link-local address (%s) alongside a real IP; removing the stray address", name, cidr))
		if err := c.network.DeleteAddress(name, cidr); err != nil {
			c.eventLog.Log(model.EventCategoryDhcp, "dhcp.linklocal_delete_failed", model.EventSeverityError,
				model.EventActorSystem, name,
				fmt.Sprintf("Failed to remove link-local address %s from %s: %v", cidr, name, err))
			return
		}
		c.eventLog.Log(model.EventCategoryDhcp, "dhcp.linklocal_delete_ok", model.EventSeverityInfo,
			model.EventActorSystem, name,
			fmt.Sprintf("Removed stray link-local address %s from %s", cidr, name))

	case actionRestart:
		c.eventLog.Log(model.EventCategoryDhcp, "dhcp.linklocal_detected", model.EventSeverityWarning,
			model.EventActorSystem, name,
			fmt.Sprintf("Interface %s is stuck without a usable IPv4 address; restarting dhcpcd", name))
		if err := c.dhcpcdService.RestartForHealthCheck(name); err != nil {
			c.eventLog.Log(model.EventCategoryDhcp, "dhcp.restart_failed", model.EventSeverityError,
				model.EventActorSystem, name,
				fmt.Sprintf("Failed to restart dhcpcd for %s: %v", name, err))
			return
		}
		c.eventLog.Log(model.EventCategoryDhcp, "dhcp.restart_ok", model.EventSeverityWarning,
			model.EventActorSystem, name,
			fmt.Sprintf("Restarted dhcpcd for %s after repeated link-local/no-IP detection", name))

	case actionRestartCeilingReached:
		// Actionable, not generic (Caution 11): evidence from real-hardware
		// testing shows a plain dhcpcd restart may not help when the root
		// cause is on the AP/SSID side rather than the client, so the
		// message names the interface/SSID and points at the AP/DHCP server
		// instead of implying "restart again and it'll be fine".
		target := name
		if iface.Type == "wireless" && iface.WifiSSID != nil && *iface.WifiSSID != "" {
			target = fmt.Sprintf("%s (SSID %q)", name, *iface.WifiSSID)
		}
		c.eventLog.Log(model.EventCategoryDhcp, "dhcp.restart_ceiling_reached", model.EventSeverityError,
			model.EventActorSystem, name,
			fmt.Sprintf("Interface %s has repeatedly failed to recover a usable IPv4 address even after dhcpcd restarts and has hit the configured restart ceiling; pigate will stop restarting it automatically — check the access point/DHCP server for %s, a client-side restart alone is unlikely to fix an AP-side issue", name, target))
	}
}

// GetSettings returns the current DHCP health-checker settings from the DB.
func (c *DhcpHealthChecker) GetSettings() (*model.DhcpHealthSettings, error) {
	return c.repo.GetDhcpHealthSettings()
}

// UpdateSettings validates the given settings' ranges and persists them.
// Validation lives here (not in the API handler) per plan T-09.
func (c *DhcpHealthChecker) UpdateSettings(settings model.DhcpHealthSettings) error {
	if settings.CheckIntervalSeconds < 10 || settings.CheckIntervalSeconds > 3600 {
		return fmt.Errorf("checkIntervalSeconds must be between 10 and 3600")
	}
	if settings.ConsecutiveStrikes < 1 || settings.ConsecutiveStrikes > 20 {
		return fmt.Errorf("consecutiveStrikes must be between 1 and 20")
	}
	if settings.MinRunningSeconds < 0 || settings.MinRunningSeconds > 600 {
		return fmt.Errorf("minRunningSeconds must be between 0 and 600")
	}
	if settings.RestartBackoffSeconds < 0 || settings.RestartBackoffSeconds > 3600 {
		return fmt.Errorf("restartBackoffSeconds must be between 0 and 3600")
	}
	if settings.MaxRestartsBeforePause < 1 || settings.MaxRestartsBeforePause > 20 {
		return fmt.Errorf("maxRestartsBeforePause must be between 1 and 20")
	}
	return c.repo.UpdateDhcpHealthSettings(settings)
}
