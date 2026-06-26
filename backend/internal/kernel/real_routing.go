//go:build linux

package kernel

import (
	"fmt"
	"log"
	"net"
	"pigate/internal/model"
	"strconv"
	"strings"

	"github.com/vishvananda/netlink"
)

// RealRouting implements RoutingManager using netlink socket.
// Requires cap_net_admin capability on the binary.
type RealRouting struct {
	allowEditSystemRoutes bool
}

func NewRealRouting(allowEditSystemRoutes bool) *RealRouting {
	return &RealRouting{
		allowEditSystemRoutes: allowEditSystemRoutes,
	}
}

// ApplyRoutes reconciles the kernel routing table with the list of configured routes in DB.
func (r *RealRouting) ApplyRoutes(routes []model.StaticRoute) error {

	log.Printf("[Routing] ApplyRoutes called with %d routes", len(routes))

	// 1. Get all current IPv4 routes in the system
	existingRoutes, err := netlink.RouteList(nil, netlink.FAMILY_V4)
	if err != nil {
		return fmt.Errorf("failed to list kernel routes: %w", err)
	}

	// Helper to get canonical CIDR representation
	canonicalCIDR := func(s string) string {
		_, ipNet, err := net.ParseCIDR(s)
		if err != nil {
			return s
		}
		return ipNet.String()
	}

	// 2. Build maps of target routes
	activeTargetMap := make(map[string]model.StaticRoute)
	allTargetMap := make(map[string]model.StaticRoute)
	for _, route := range routes {
		key := fmt.Sprintf("%s|%s|%s", canonicalCIDR(route.Destination), route.Gateway, route.Interface)
		allTargetMap[key] = route
		if route.Status {
			activeTargetMap[key] = route
		}
	}

	// 3. Reconcile existing routes: delete inactive/removed, update metrics
	for _, rt := range existingRoutes {
		dstStr := "0.0.0.0/0"
		if rt.Dst != nil {
			dstStr = canonicalCIDR(rt.Dst.String())
		}
		gwStr := ""
		if rt.Gw != nil {
			gwStr = rt.Gw.String()
		}
		if gwStr == "0.0.0.0" {
			gwStr = ""
		}

		link, err := netlink.LinkByIndex(rt.LinkIndex)
		if err != nil {
			continue
		}
		ifaceName := link.Attrs().Name

		key := fmt.Sprintf("%s|%s|%s", dstStr, gwStr, ifaceName)

		// Protocol 120 indicates custom static routes created/managed by our application
		isManagedRoute := rt.Protocol == 120

		targetRoute, isActive := activeTargetMap[key]
		if isActive {
			// Matches an active target route. Update priority, protocol, scope, or src if they differ.
			targetScope := parseScope(targetRoute.Scope)
			targetProto := netlink.RouteProtocol(parseProtocol(targetRoute.Proto))
			targetSrc := net.ParseIP(targetRoute.Src)

			srcMatches := (rt.Src == nil && len(targetRoute.Src) == 0) || (rt.Src != nil && rt.Src.Equal(targetSrc))

			if rt.Priority != targetRoute.Metric || rt.Protocol != targetProto || rt.Scope != targetScope || !srcMatches {
				rt.Priority = targetRoute.Metric
				rt.Protocol = targetProto
				rt.Scope = targetScope
				if len(targetRoute.Src) > 0 {
					rt.Src = targetSrc
				} else {
					rt.Src = nil
				}
				_ = netlink.RouteReplace(&rt)
			}
			// Remove from map since it's already active in the kernel
			delete(activeTargetMap, key)
		} else {
			// Does not match any active target route. Determine if we should delete it.
			_, existsInTargetList := allTargetMap[key]

			shouldDelete := false
			if isManagedRoute {
				// Old custom route no longer present/active in target list
				shouldDelete = true
			} else if existsInTargetList && !allTargetMap[key].Status {
				// Predefined/custom route explicitly disabled in target list
				if allTargetMap[key].Type == "custom" || r.allowEditSystemRoutes {
					shouldDelete = true
				}
			} else if !existsInTargetList && r.allowEditSystemRoutes && gwStr != "" {
				// System route deleted from database (not present in DB routes list at all)
				// We only delete if it is a gateway route (has gwStr) to avoid disrupting link local networks
				shouldDelete = true
			}

			if shouldDelete {
				_ = netlink.RouteDel(&rt)
			}
		}
	}

	// 4. Add new active routes not currently present in the kernel
	for _, route := range activeTargetMap {
		link, err := netlink.LinkByName(route.Interface)
		if err != nil {
			// Interface not found — skip this route but don't abort remaining routes
			log.Printf("[Routing] Warning: interface %q not found for route to %s, skipping: %v", route.Interface, route.Destination, err)
			continue
		}

		// Check if interface is UP
		if link.Attrs().Flags&net.FlagUp == 0 {
			log.Printf("[Routing] Warning: interface %q is DOWN, skipping route to %s", route.Interface, route.Destination)
			continue
		}

		// If a gateway is specified, ensure the interface has an active IPv4 address configured
		if route.Gateway != "" {
			addrs, err := netlink.AddrList(link, netlink.FAMILY_V4)
			if err != nil || len(addrs) == 0 {
				log.Printf("[Routing] Warning: interface %q has no active IPv4 address, skipping gateway route to %s via %s", route.Interface, route.Destination, route.Gateway)
				continue
			}
		}

		_, dstNet, err := net.ParseCIDR(route.Destination)
		if err != nil {
			log.Printf("[Routing] Warning: invalid destination network %q for route on %s, skipping: %v", route.Destination, route.Interface, err)
			continue
		}

		var gwIP net.IP
		if route.Gateway != "" {
			gwIP = net.ParseIP(route.Gateway)
			if gwIP == nil {
				log.Printf("[Routing] Warning: invalid gateway IP %q for route to %s, skipping", route.Gateway, route.Destination)
				continue
			}
		}

		targetScope := parseScope(route.Scope)
		targetProto := netlink.RouteProtocol(parseProtocol(route.Proto))
		var srcIP net.IP
		if route.Src != "" {
			srcIP = net.ParseIP(route.Src)
		}

		netlinkRoute := &netlink.Route{
			LinkIndex: link.Attrs().Index,
			Dst:       dstNet,
			Gw:        gwIP,
			Priority:  route.Metric,
			Protocol:  targetProto,
			Scope:     targetScope,
			Src:       srcIP,
		}

		if err := netlink.RouteAdd(netlinkRoute); err != nil {
			// Fallback retry with replace in case it exists in a slightly different configuration
			if err := netlink.RouteReplace(netlinkRoute); err != nil {
				log.Printf("[Routing] Warning: failed to add/replace route to %s via %s on %s: %v", route.Destination, route.Gateway, route.Interface, err)
				continue
			}
		}

		log.Printf("[Routing] Added route to %s via %s on %s", route.Destination, route.Gateway, route.Interface)
	}

	log.Printf("[Routing] ApplyRoutes completed")
	return nil
}

func parseScope(scopeStr string) netlink.Scope {
	switch strings.ToLower(strings.TrimSpace(scopeStr)) {
	case "link":
		return netlink.SCOPE_LINK
	case "host":
		return netlink.SCOPE_HOST
	case "site":
		return netlink.SCOPE_SITE
	case "nowhere":
		return netlink.SCOPE_NOWHERE
	case "global", "":
		return netlink.SCOPE_UNIVERSE
	default:
		if val, err := strconv.Atoi(scopeStr); err == nil {
			return netlink.Scope(val)
		}
		return netlink.SCOPE_UNIVERSE
	}
}

func parseProtocol(protoStr string) int {
	switch strings.ToLower(strings.TrimSpace(protoStr)) {
	case "kernel":
		return 2
	case "boot":
		return 3
	case "static":
		return 4
	case "redirect":
		return 1
	case "":
		return 120
	default:
		if val, err := strconv.Atoi(protoStr); err == nil {
			return val
		}
		return 120
	}
}
