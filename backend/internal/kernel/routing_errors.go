package kernel

import "errors"

// This file has NO build tag (unlike real_routing.go) because both the
// service package (errors.Is checks) and cross-platform tests must be able
// to import these sentinels regardless of GOOS — see docs/ref/
// wan-failover-findings.md and the T-20 fix for the WAN failover
// route-disappears bug.

// ErrDefaultRouteMetricConflict is returned (wrapped with %w) by
// RoutingManager.EnforceDefaultRouteMetric when the kernel refuses to add a
// new default route at the requested metric because another interface
// already holds a default route at that exact (dst, tos, priority) FIB key.
// Linux's netlink RouteAdd uses NLM_F_EXCL, which rejects the add with
// EEXIST even though the nexthop (outgoing interface) differs — the FIB
// lookup key for a default route does not include the interface. This is
// the root cause diagnosed for the "active WAN switch deletes the route"
// bug: the old code deleted the losing interface's route BEFORE attempting
// to add the winning interface's route at the same metric, so a failed add
// left NO default route on the previously-active interface at all.
//
// Callers (service.RoutingService.enforceInterfaceMetrics) use
// errors.Is(err, ErrDefaultRouteMetricConflict) to detect this specific,
// recoverable condition (as opposed to a generic kernel failure) and retry
// once the conflicting interface has actually been demoted off that metric.
var ErrDefaultRouteMetricConflict = errors.New("default route metric already in use by another interface")

// ErrDefaultRouteUnreachable is returned (wrapped with %w) by
// EnforceDefaultRouteMetric when the kernel rejects the new route because
// the interface/gateway is not currently reachable (ENETDOWN/ENETUNREACH) —
// e.g. a cable-pull racing the enforcement call. This is distinct from
// ErrDefaultRouteMetricConflict: there is nothing to retry against, the link
// itself is down, and the caller should simply log/report and move on.
var ErrDefaultRouteUnreachable = errors.New("default route target is currently unreachable (interface or gateway down)")
