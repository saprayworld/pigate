package service

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"pigate/internal/db"
	"pigate/internal/model"
)

// This file implements the Multi-WAN Failover Phase 2 controller
// (docs/ref/todo/multi-wan-failover-plan.md Task 15). It decides which
// configured model.WanUplink should be "active" (carry the default route)
// based on health data from WanMonitor, and enforces that decision purely
// through RoutingService's metric-override API (routing.go, Task 14) — this
// file is DELIBERATELY forbidden from importing the low-level OS-control
// package at all (D-2): every kernel touch goes through RoutingService so
// Task 14's precedence rules (static-route bypass, snapshot/restore) are the
// single place that ever talks to the OS routing table. Do not add that
// import here.
//
// D-7 (docs/ref/todo/multi-wan-failover-plan.md, and model/wan_uplink.go's
// WanStateDegraded doc comment): "degraded" is a display-only health state.
// Nothing in this file's decision logic (decideActiveUplink) ever reads or
// reacts to model.WanStateDegraded — only model.WanStateUp/WanStateDown
// matter to a failover decision. Do not add a "degraded triggers failover"
// toggle back without re-reading D-7's rationale.

// wanFailoverTickInterval is the controller's own ticker cadence — shorter
// than WanMonitor's own cadence intentionally, so a health-state change
// WanMonitor just wrote is picked up promptly rather than compounding an
// extra multi-second lag on top of the probe round itself.
const wanFailoverTickInterval = 2 * time.Second

// wanFailoverStartupGrace bounds how long the controller waits, after
// Start(ctx), before it is willing to make its very first AUTO-mode
// decision even if some enabled uplink's health is still "unknown" (never
// successfully probed) — see decideActiveUplink's FirstDecisionDone input
// and computeFirstDecisionDone below. Manual mode is never gated by this
// (a manual override must always take effect immediately, D-2/T-15 plan).
const wanFailoverStartupGrace = 60 * time.Second

// Decision B (docs/ref/todo/multi-wan-failover-plan.md, approved
// 2026-09-07): the failover controller's default-route metric band is FIXED
// (not derived from the user-configurable model.NetworkInterface.Metric),
// so the ordering the kernel actually sees is always unambiguous and
// independent of whatever an operator separately typed into the Interfaces
// page. Every other currently-enabled uplink gets a "standby" metric far
// above the active band, spread out by Priority (lower Priority number =
// lower/better standby metric = tried first by the kernel if the active
// route ever disappeared for an unrelated reason) so a tie between two
// standby uplinks can never happen.
//
// Decision F (docs/ref/todo/multi-wan-failover-plan.md, approved
// 2026-09-09, refines Decision B): the active metric is no longer a single
// value shared by whichever uplink happens to be active — it is now
// per-uplink (wanFailoverActiveMetricBase + Priority), so every
// currently-enabled uplink owns its OWN permanent slot in BOTH the active
// and standby bands at ALL times, active or not. This closes the exact
// collision Decision B's single shared "active" value left open: with only
// one fixed active metric, the uplink being PROMOTED into it and the uplink
// being DEMOTED off it briefly want that identical metric during a switch —
// precisely the netlink NLM_F_EXCL/EEXIST race that caused the WAN failover
// route-disappears bug (docs/ref/wan-failover-findings.md). This is
// collision-free ONLY because WAN uplink Priority is enforced unique across
// every wan_uplinks row (DB UNIQUE index + validation, see
// db/connection.go/db/wan_repo.go) — Priority 1..16 maps 1:1 onto the active
// band 51..66 (model.WanReservedActiveMetricMin/Max) and the standby band
// 1010..1160 (within the wider reserved model.WanReservedStandbyMetricMin/
// Max range, which also blocks a manually-configured interface Metric from
// landing in either band — see model.ValidateWanUplinkInterfaceMetric).
const (
	wanFailoverActiveMetricBase  = 50
	wanFailoverStandbyMetricBase = 1000
)

// wanFailoverBlackholeReasonPrefix marks decideActiveUplink's "nothing
// healthy" reason string so tick() can recognize it (log-once, critical
// severity) without decideActiveUplink itself doing any I/O — kept as a
// prefix (not equality) because the full reason also names live state, but
// tick() only needs to know THIS particular reason occurred.
const wanFailoverBlackholeReasonPrefix = "no healthy (non-stale) uplink available"

// WanFailoverController is the Phase 2 automatic/manual failover decision
// loop. It never talks to the kernel directly (see the file doc comment
// above) — every effect goes through routing's metric-override API.
type WanFailoverController struct {
	repo     *db.Repository
	monitor  *WanMonitor
	routing  *RoutingService
	eventLog *EventLogService
	bus      *NetEventBus

	startedAt time.Time
	// kick lets an external caller (T-16's API layer, after a settings/
	// manual-override change) wake the tick loop immediately instead of
	// waiting up to wanFailoverTickInterval — buffered 1 and non-blocking so
	// Kick() can never itself block a caller (e.g. an HTTP handler).
	kick chan struct{}

	mu                sync.Mutex
	activeUplinkID    string
	lastSwitchAt      time.Time
	lastSwitchReason  string
	firstDecisionDone bool
	// lastSettings/settingsBaseline back Status()'s Enabled/Mode fields and
	// logSettingsChangeIfAny's transition detection; settingsBaseline only
	// becomes true after the first tick ever observes a settings row, so
	// startup never spuriously logs a "changed from the zero value" event.
	lastSettings     model.WanFailoverSettings
	settingsBaseline bool
	// blackholeLogged/bypassedLogged are "log this specific condition only on
	// the transition INTO it" guards — see logBlackholeOnce/
	// clearBlackholeLogged and checkBypassed.
	blackholeLogged bool
	// bypassedLogged is keyed by interface name, not a single bool, because
	// the ACTIVE uplink can switch directly from one bypassed interface to a
	// DIFFERENT bypassed interface with no non-bypassed tick in between — a
	// single shared flag would then stay "already true" and silently swallow
	// the warning for the new interface (QA finding, Minor 3).
	bypassedLogged map[string]bool
}

// NewWanFailoverController constructs the controller. Start(ctx) must be
// called separately once startup wiring is complete (mirrors WanMonitor).
func NewWanFailoverController(repo *db.Repository, monitor *WanMonitor, routing *RoutingService, eventLog *EventLogService, bus *NetEventBus) *WanFailoverController {
	return &WanFailoverController{
		repo:           repo,
		monitor:        monitor,
		routing:        routing,
		eventLog:       eventLog,
		bus:            bus,
		kick:           make(chan struct{}, 1),
		bypassedLogged: make(map[string]bool),
	}
}

// Start launches the periodic background loop. Safe to call even when the
// kill switch is off (WanFailoverSettings.Enabled==false, the shipped
// default) — tick() reads that flag on every pass and does nothing but a
// cheap idempotent cleanup call in that case (see handleDisabled).
func (c *WanFailoverController) Start(ctx context.Context) {
	c.mu.Lock()
	c.startedAt = time.Now()
	c.mu.Unlock()
	go c.run(ctx)
}

func (c *WanFailoverController) run(ctx context.Context) {
	t := time.NewTicker(wanFailoverTickInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			c.tick(now)
		case <-c.kick:
			c.tick(time.Now())
		}
	}
}

// Kick wakes the controller's tick loop immediately instead of waiting up
// to wanFailoverTickInterval. Non-blocking: if a kick is already pending
// (unlikely — the loop drains it within wanFailoverTickInterval at worst),
// this is a no-op rather than blocking the caller.
func (c *WanFailoverController) Kick() {
	select {
	case c.kick <- struct{}{}:
	default:
	}
}

// tick is one pass of the controller. Guard order mirrors WanMonitor.tick:
// bus-pause first (skip the whole tick during a backup import so the
// controller never races a config restore).
func (c *WanFailoverController) tick(now time.Time) {
	if c.bus.IsPaused() {
		return
	}

	settingsPtr, err := c.repo.GetWanFailoverSettings()
	if err != nil {
		log.Printf("[WanFailover] failed to read failover settings: %v", err)
		return
	}
	settings := *settingsPtr

	c.logSettingsChangeIfAny(settings)

	if !settings.Enabled {
		c.handleDisabled()
		return
	}

	uplinks, err := c.repo.GetWanUplinks()
	if err != nil {
		log.Printf("[WanFailover] failed to read uplinks: %v", err)
		return
	}

	states := make(map[string]model.WanUplinkState, len(uplinks))
	for _, st := range c.monitor.GetStates() {
		states[st.UplinkID] = st
	}

	c.mu.Lock()
	current := c.activeUplinkID
	lastSwitchAt := c.lastSwitchAt
	firstDecisionDone := c.firstDecisionDone
	c.mu.Unlock()

	if !firstDecisionDone {
		firstDecisionDone = c.computeFirstDecisionDone(uplinks, states, now)
		c.mu.Lock()
		c.firstDecisionDone = firstDecisionDone
		c.mu.Unlock()
	}

	target, reason, changed := decideActiveUplink(wanFailoverDecideInput{
		Now:               now,
		Uplinks:           uplinks,
		States:            states,
		Settings:          settings,
		Current:           current,
		LastSwitchAt:      lastSwitchAt,
		FirstDecisionDone: firstDecisionDone,
	})

	if strings.HasPrefix(reason, wanFailoverBlackholeReasonPrefix) {
		c.logBlackholeOnce(reason)
	} else {
		c.clearBlackholeLogged()
	}

	if changed {
		c.applySwitch(current, target, reason, now)
	}

	if target == "" {
		// No decision has ever been made yet (boot grace, or manual mode
		// pointing at a nonexistent uplink from a cold start) — nothing to
		// enforce.
		return
	}

	activeUplink, found := findUplinkByID(uplinks, target)
	c.enforceOverrides(target, uplinks)
	if found {
		c.checkBypassed(activeUplink)
	}
}

// handleDisabled is the kill-switch-off path: clears any lingering metric
// overrides EXACTLY ONCE (ClearAllFailoverMetricOverrides is itself
// idempotent — returns false once nothing is left to clear — so repeated
// disabled ticks stay silent, never reconciling/spamming the kernel every
// tick) and resets the controller's own notion of "active uplink" so a
// future re-enable starts from a clean decision rather than silently
// resuming a possibly-stale one.
func (c *WanFailoverController) handleDisabled() {
	if c.routing.ClearAllFailoverMetricOverrides() {
		if err := c.routing.ReconcileKernelRoutingTable(); err != nil {
			log.Printf("[WanFailover] Warning: failed to reconcile kernel routing after clearing overrides (kill switch off): %v", err)
		}
	}

	c.mu.Lock()
	c.activeUplinkID = ""
	c.firstDecisionDone = false
	c.blackholeLogged = false
	c.bypassedLogged = make(map[string]bool)
	c.mu.Unlock()
}

// computeFirstDecisionDone implements the plan's boot-grace rule: the
// controller makes no AUTO-mode decision at all until either every
// currently-enabled uplink has a known (non-WanStateUnknown) health state,
// or wanFailoverStartupGrace has elapsed since Start(ctx) — whichever comes
// first. Manual mode is never gated by this (see decideActiveUplink).
func (c *WanFailoverController) computeFirstDecisionDone(uplinks []model.WanUplink, states map[string]model.WanUplinkState, now time.Time) bool {
	c.mu.Lock()
	startedAt := c.startedAt
	c.mu.Unlock()

	if now.Sub(startedAt) >= wanFailoverStartupGrace {
		return true
	}
	for _, u := range uplinks {
		if !u.Status {
			continue
		}
		st, ok := states[u.ID]
		if !ok || st.State == model.WanStateUnknown {
			return false
		}
	}
	return true
}

// applySwitch records a real active-uplink switch (controller state +
// event log). from may be "" (the very first decision ever made).
func (c *WanFailoverController) applySwitch(from, to, reason string, now time.Time) {
	c.mu.Lock()
	c.activeUplinkID = to
	c.lastSwitchAt = now
	c.lastSwitchReason = reason
	c.mu.Unlock()

	if c.eventLog == nil {
		return
	}
	fromLabel := from
	if fromLabel == "" {
		fromLabel = "(none)"
	}
	c.eventLog.Log(model.EventCategoryNetwork, "wan-failover", model.EventSeverityWarning,
		model.EventActorSystem, to,
		fmt.Sprintf("WAN failover switched active uplink %s -> %s: %s", fromLabel, to, reason))
}

// enforceOverrides builds this tick's desired metric-override map (Decision
// B's active/standby band, one entry per currently-enabled uplink) and hands
// it to routing.SetFailoverMetricOverrides in a single atomic call.
// ReconcileKernelRoutingTable is only invoked when that call reports a real
// change — repeating the exact same desired map every tick (the common
// case: nothing changed since the last tick) must never touch the kernel or
// log anything (plan: "ป้องกัน log/kernel churn").
func (c *WanFailoverController) enforceOverrides(active string, uplinks []model.WanUplink) {
	desired := make(map[string]int, len(uplinks))
	for _, u := range uplinks {
		if !u.Status {
			continue
		}
		if u.ID == active {
			desired[u.Interface] = wanFailoverActiveMetricBase + u.Priority
		} else {
			desired[u.Interface] = wanFailoverStandbyMetricBase + 10*u.Priority
		}
	}

	if c.routing.SetFailoverMetricOverrides(desired) {
		if err := c.routing.ReconcileKernelRoutingTable(); err != nil {
			log.Printf("[WanFailover] Warning: failed to reconcile kernel routing after a metric-override change: %v", err)
		}
	}
}

// checkBypassed logs a one-time warning when the CURRENTLY ACTIVE uplink's
// own interface is reported by routing.FailoverBypassedInterfaces() — i.e.
// its failover override is currently inert because an active DB static
// 0.0.0.0/0 route on that same interface outranks it (T-14 precedence level
// 1). The "already logged" guard is tracked PER INTERFACE (bypassedLogged),
// not as one shared flag: the active uplink can switch directly from one
// bypassed interface to a different bypassed interface with no non-bypassed
// tick in between, and a single shared flag would then wrongly stay "already
// logged" and swallow the warning for the new interface (QA finding, Minor
// 3). Clears the current interface's entry the moment it is no longer
// bypassed, so a future recurrence on that same interface logs again.
func (c *WanFailoverController) checkBypassed(activeUplink model.WanUplink) {
	isBypassed := false
	for _, name := range c.routing.FailoverBypassedInterfaces() {
		if name == activeUplink.Interface {
			isBypassed = true
			break
		}
	}

	c.mu.Lock()
	alreadyLogged := c.bypassedLogged[activeUplink.Interface]
	if isBypassed {
		c.bypassedLogged[activeUplink.Interface] = true
	} else {
		delete(c.bypassedLogged, activeUplink.Interface)
	}
	c.mu.Unlock()

	if isBypassed && !alreadyLogged {
		log.Printf("[WanFailover] Warning: active uplink %q's interface %s is bypassed by an active static 0.0.0.0/0 route — its metric override has no effect until that route is removed/disabled", activeUplink.ID, activeUplink.Interface)
	}
}

// logBlackholeOnce logs a CRITICAL event exactly once per "every uplink is
// down" episode (never once per tick while it persists) — see
// clearBlackholeLogged for the reset side.
func (c *WanFailoverController) logBlackholeOnce(reason string) {
	c.mu.Lock()
	already := c.blackholeLogged
	c.blackholeLogged = true
	c.mu.Unlock()
	if already {
		return
	}

	log.Printf("[WanFailover] CRITICAL: %s", reason)
	if c.eventLog != nil {
		c.eventLog.Log(model.EventCategoryNetwork, "wan-failover", model.EventSeverityCritical,
			model.EventActorSystem, "", "WAN failover: "+reason)
	}
}

// clearBlackholeLogged resets logBlackholeOnce's guard once the "nothing
// healthy" condition is no longer the case, so a future recurrence logs
// again.
func (c *WanFailoverController) clearBlackholeLogged() {
	c.mu.Lock()
	c.blackholeLogged = false
	c.mu.Unlock()
}

// logSettingsChangeIfAny logs an event whenever Enabled or Mode actually
// changes since the last-observed settings row (never on the very first
// observation ever, which would otherwise spuriously "change" from the zero
// value at startup).
func (c *WanFailoverController) logSettingsChangeIfAny(settings model.WanFailoverSettings) {
	c.mu.Lock()
	prev := c.lastSettings
	baseline := c.settingsBaseline
	c.lastSettings = settings
	c.settingsBaseline = true
	c.mu.Unlock()

	if !baseline {
		return
	}
	if prev.Enabled == settings.Enabled && prev.Mode == settings.Mode {
		return
	}
	if c.eventLog == nil {
		return
	}
	c.eventLog.Log(model.EventCategoryNetwork, "wan-failover", model.EventSeverityInfo,
		model.EventActorSystem, "",
		fmt.Sprintf("WAN failover settings changed: enabled %v -> %v, mode %q -> %q", prev.Enabled, settings.Enabled, prev.Mode, settings.Mode))
}

// Status returns the controller's current live status for the api layer
// (T-16) — a cheap, lock-only read (Bypassed is routing's own already-
// lock-protected accessor).
func (c *WanFailoverController) Status() model.WanFailoverStatus {
	c.mu.Lock()
	activeUplinkID := c.activeUplinkID
	lastSwitchAtT := c.lastSwitchAt
	lastSwitchReason := c.lastSwitchReason
	settings := c.lastSettings
	c.mu.Unlock()

	lastSwitchAt := ""
	if !lastSwitchAtT.IsZero() {
		lastSwitchAt = lastSwitchAtT.UTC().Format(time.RFC3339)
	}

	return model.WanFailoverStatus{
		Enabled:                 settings.Enabled,
		Mode:                    settings.Mode,
		ActiveUplinkID:          activeUplinkID,
		LastSwitchAt:            lastSwitchAt,
		LastSwitchReason:        lastSwitchReason,
		Bypassed:                c.routing.FailoverBypassedInterfaces(),
		EnforceFailedInterfaces: c.routing.EnforceFailedInterfaces(),
	}
}

// findUplinkByID is a small local helper (uplinks lists here are always
// short — at most a handful of WAN paths — so a linear scan is fine and
// keeps callers simple).
func findUplinkByID(uplinks []model.WanUplink, id string) (model.WanUplink, bool) {
	for _, u := range uplinks {
		if u.ID == id {
			return u, true
		}
	}
	return model.WanUplink{}, false
}

// wanFailoverDecideInput is decideActiveUplink's pure input. now/uplinks/
// states/settings/current/lastSwitchAt/firstDecisionDone are all
// caller-supplied snapshots — this function performs NO I/O and touches no
// package-level or controller state, so it is fully unit-testable in
// isolation (mirrors wan_monitor.go's decideState/selectProbeMethod shape).
type wanFailoverDecideInput struct {
	Now      time.Time
	Uplinks  []model.WanUplink
	States   map[string]model.WanUplinkState // keyed by WanUplink.ID
	Settings model.WanFailoverSettings
	// Current is the presently-active uplink ID, or "" if none has ever been
	// decided yet.
	Current string
	// LastSwitchAt is the zero value if no switch has ever happened yet.
	LastSwitchAt time.Time
	// FirstDecisionDone gates AUTO mode's very first decision — see
	// WanFailoverController.computeFirstDecisionDone. Always treated as
	// irrelevant in MANUAL mode (a manual override is never delayed by
	// startup grace).
	FirstDecisionDone bool
}

// decideActiveUplink is the pure decision core (docs/ref/todo/
// multi-wan-failover-plan.md Task 15). It returns the uplink ID that SHOULD
// be active after this tick (target), a human-readable reason (always
// non-empty), and whether this represents an actual switch away from
// Current (changed). A false changed does NOT mean "no reason to report" —
// e.g. a rejected switch (dampening/revert-delay) or the "nothing healthy"
// anti-blackhole case both explain themselves via reason while changed stays
// false and target stays Current.
func decideActiveUplink(in wanFailoverDecideInput) (target string, reason string, changed bool) {
	if !in.Settings.Enabled {
		return in.Current, "failover disabled", false
	}

	var desired, desiredReason string

	if in.Settings.Mode == model.WanFailoverModeManual {
		manualID := strings.TrimSpace(in.Settings.ManualUplinkID)
		_, exists := findUplinkByID(in.Uplinks, manualID)
		if manualID == "" || !exists {
			return in.Current, fmt.Sprintf("manual mode but manualUplinkId %q does not refer to a configured uplink; keeping %q active", manualID, in.Current), false
		}
		desired = manualID
		desiredReason = fmt.Sprintf("manual override: forced to uplink %q", manualID)
	} else {
		// Auto mode: never gated by startup grace once past it, and D-7
		// structurally cannot select on WanStateDegraded — only WanStateUp
		// (and Decision E's !Stale) ever qualifies a candidate.
		if !in.FirstDecisionDone {
			return in.Current, "waiting for startup grace / initial health data before making the first failover decision", false
		}

		best := ""
		bestPriority := 0
		for _, u := range in.Uplinks {
			if !u.Status {
				continue
			}
			st, ok := in.States[u.ID]
			if !ok || st.State != model.WanStateUp || st.Stale {
				continue
			}
			if best == "" || u.Priority < bestPriority {
				best = u.ID
				bestPriority = u.Priority
			}
		}
		if best == "" {
			return in.Current, fmt.Sprintf("%s; keeping the last-known active uplink to avoid a total blackout", wanFailoverBlackholeReasonPrefix), false
		}
		desired = best
		desiredReason = fmt.Sprintf("auto mode: uplink %q is the highest-priority healthy, non-stale uplink", desired)
	}

	if desired == in.Current {
		return in.Current, desiredReason + " (already active)", false
	}

	// Dampening: at most one AUTO-mode switch per MinHoldSeconds, guarding
	// against an automated decision flapping back and forth. Manual mode is
	// DELIBERATELY exempt (owner decision, QA Major 2 — see also
	// docs/tech_stack_design.md §11): forcing ManualUplinkID is a direct,
	// already-authenticated super_admin action, not unattended automation at
	// risk of flapping, and must always take effect immediately — the same
	// principle wanFailoverStartupGrace/FirstDecisionDone already applies to
	// manual mode above.
	if in.Settings.Mode != model.WanFailoverModeManual && !in.LastSwitchAt.IsZero() {
		elapsed := in.Now.Sub(in.LastSwitchAt)
		hold := time.Duration(in.Settings.MinHoldSeconds) * time.Second
		if elapsed < hold {
			return in.Current, fmt.Sprintf("switch to %q suppressed: only %s since the last switch (MinHoldSeconds=%d)", desired, elapsed.Round(time.Second), in.Settings.MinHoldSeconds), false
		}
	}

	// Revert delay: only applies in AUTO mode, and only when desired outranks
	// (lower Priority number = better) the CURRENTLY active uplink — i.e.
	// this is a revert back to a higher-priority uplink after having failed
	// away from it, not any generic switch (a switch AWAY from a now-down
	// active uplink is never held up by this either way). Manual mode is
	// DELIBERATELY exempt from this too (QA round-2 finding on top of Major
	// 2, owner decision): the same "authenticated super_admin action, not
	// flap-risk automation" reasoning as the MinHold exemption above applies
	// equally here — neither anti-flap mechanism may ever silently swallow a
	// manual override (previously this one did: decideActiveUplink returned
	// changed=false with no guard, so POST /api/wan/failover/override
	// answered 200 OK while the kernel state never actually moved).
	if in.Settings.Mode != model.WanFailoverModeManual && in.Current != "" {
		if curPriority, ok := priorityOf(in.Uplinks, in.Current); ok {
			if desiredPriority, ok2 := priorityOf(in.Uplinks, desired); ok2 && desiredPriority < curPriority {
				lastChangeAt := parseWanStateChangeTime(in.States[desired].LastChangeAt)
				hold := time.Duration(in.Settings.RevertDelaySeconds) * time.Second
				if lastChangeAt.IsZero() || in.Now.Sub(lastChangeAt) < hold {
					return in.Current, fmt.Sprintf("revert to higher-priority uplink %q suppressed: not yet healthy for RevertDelaySeconds=%d", desired, in.Settings.RevertDelaySeconds), false
				}
			}
		}
	}

	return desired, desiredReason, true
}

// priorityOf looks up an uplink's configured Priority by ID.
func priorityOf(uplinks []model.WanUplink, id string) (int, bool) {
	for _, u := range uplinks {
		if u.ID == id {
			return u.Priority, true
		}
	}
	return 0, false
}

// parseWanStateChangeTime parses a model.WanUplinkState.LastChangeAt
// (RFC3339, possibly empty) into a time.Time, returning the zero value for
// "" or a malformed string rather than erroring — callers treat the zero
// value as "unknown, so do not allow a revert yet" (fail closed).
func parseWanStateChangeTime(s string) time.Time {
	if s == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}
	}
	return t
}
