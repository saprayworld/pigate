package service

import (
	"context"
	"log"
	"sync/atomic"
	"time"

	"pigate/internal/db"
	"pigate/internal/kernel"
)

// RuleNameResolver resolves the "r=<token>" id captured in a traffic-log
// entry's RuleID field (see model.FirewallLog, kernel.withRuleToken) into a
// human-readable display name, for the snapshot-on-write ruleName step in
// cmd/pigate/main.go's stampAndPush.
//
// It must be O(1) and allocation-light with zero I/O on the lookup path: it
// is called once per NFLOG event, on the same goroutine that drains the
// netlink socket, so any blocking/slow lookup here risks the kernel dropping
// NFLOG events under load. This is achieved with a lock-free
// atomic.Pointer[map[string]string] snapshot of "policy rule id -> name",
// rebuilt from the DB in the background (systemNames is a small static map
// that never changes, so it needs no snapshotting at all).
type RuleNameResolver struct {
	repo *db.Repository

	// snapshot holds *map[string]string (policy rule id -> name). Replaced
	// wholesale (never mutated in place) so readers never need a lock.
	snapshot atomic.Pointer[map[string]string]
}

// NewRuleNameResolver constructs a resolver with an empty snapshot — safe to
// call Resolve on immediately (everything just resolves to "" until the
// first Refresh completes).
func NewRuleNameResolver(repo *db.Repository) *RuleNameResolver {
	r := &RuleNameResolver{repo: repo}
	empty := map[string]string{}
	r.snapshot.Store(&empty)
	return r
}

// systemRuleNames maps the fixed "sys-*" tokens emitted by structural
// nftables log points (default drop, admin access, docker-compat bypass,
// etc — see kernel.SysToken* constants) to a human-readable label. This
// table is static (no DB/kernel state involved), so it does not need to be
// part of the atomic snapshot and can be looked up directly.
var systemRuleNames = map[string]string{
	kernel.SysTokenNotLocalDrop:       "System: Anti-Spoof / Not-Local Drop",
	kernel.SysTokenDhcpServerAccept:   "System: DHCP Server Accept",
	kernel.SysTokenDhcpClientAccept:   "System: DHCP Client Accept",
	kernel.SysTokenDockerAccept:       "System: Docker Compatibility Accept",
	kernel.SysTokenInputDefaultDrop:   "System: Default Drop (Local-In)",
	kernel.SysTokenForwardDefaultDrop: "System: Default Drop (Forward)",
	kernel.SysTokenAdminPing:          "System: Admin Access (Ping)",
	kernel.SysTokenAdminHTTP:          "System: Admin Access (HTTP)",
	kernel.SysTokenAdminHTTPS:         "System: Admin Access (HTTPS)",
	kernel.SysTokenAdminSSH:           "System: Admin Access (SSH)",
	kernel.SysTokenDNSServerAccept:    "System: DNS Server Accept",
}

// Resolve returns the display name for ruleID (a value of
// model.FirewallLog.RuleID), or "" if it cannot be resolved right now —
// either because ruleID is empty, the rule was deleted/renamed since the
// snapshot, or no snapshot has loaded yet. Callers (stampAndPush) treat ""
// as "no rule name available", never as an error. O(1), no I/O.
func (r *RuleNameResolver) Resolve(ruleID string) string {
	if ruleID == "" {
		return ""
	}
	if name, ok := systemRuleNames[ruleID]; ok {
		return name
	}
	m := r.snapshot.Load()
	if m == nil {
		return ""
	}
	return (*m)[ruleID]
}

// Refresh reloads every policy rule id/name pair from the DB and atomically
// replaces the snapshot. Does I/O (a DB query) — never call this from the
// NFLOG hot path; it is only meant to run from the background ticker in
// StartRuleNameRefresher and from FirewallService's post-sync hook.
func (r *RuleNameResolver) Refresh() error {
	rules, err := r.repo.GetPolicies()
	if err != nil {
		return err
	}
	names := make(map[string]string, len(rules))
	for _, rule := range rules {
		names[rule.ID] = rule.Name
	}
	r.snapshot.Store(&names)
	return nil
}

// ruleNameRefreshInterval is the background refresh cadence. Kept short
// (docs/ref/todo/traffic-log-rule-name-and-domain-plan.md design decision 3)
// because it is the fallback path — the primary freshness guarantee is the
// synchronous refresh FirewallService triggers right after a successful
// SyncFirewallRules (see SetRuleNameRefreshHook), so a newly created rule
// with immediate traffic still gets a name without waiting for this ticker.
const ruleNameRefreshInterval = 5 * time.Second

// StartRuleNameRefresher performs one synchronous Refresh (so the very first
// NFLOG events processed after startup can already resolve rule names —
// see cmd/pigate/main.go for why this must run before the traffic-log
// watchers start), then keeps the snapshot warm with a background ticker
// until ctx is cancelled.
func (r *RuleNameResolver) StartRuleNameRefresher(ctx context.Context) {
	if err := r.Refresh(); err != nil {
		log.Printf("[RuleNameResolver] initial refresh failed: %v", err)
	}
	go func() {
		ticker := time.NewTicker(ruleNameRefreshInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := r.Refresh(); err != nil {
					log.Printf("[RuleNameResolver] refresh failed: %v", err)
				}
			}
		}
	}()
}
