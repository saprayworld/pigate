//go:build linux

package kernel

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"

	"pigate/internal/model"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// RealQos implements QosManager using vishvananda/netlink for direct kernel interaction.
// Requires cap_net_admin capability on the binary.
// Phase 1: Egress (Client Download) HTB shaping.
// Phase 2: Ingress (Client Upload) via IFB redirect.
type RealQos struct{}

func NewRealQos() *RealQos {
	return &RealQos{}
}

// ApplyQosRules is idempotent: it clears existing qdiscs per interface then re-applies
// all enabled rules. Rules are grouped by interface so each interface gets one HTB root
// qdisc with multiple classes under it.
func (q *RealQos) ApplyQosRules(rules []model.QosRule) error {
	// Group rules by interface name
	byIface := groupQosRulesByIface(rules)

	for ifaceName, ifaceRules := range byIface {
		log.Printf("[RealQos] Applying %d rule(s) to interface %s", len(ifaceRules), ifaceName)

		// 1. Clear existing qdiscs (idempotent)
		if err := q.ClearQosRules(ifaceName); err != nil {
			// Log but continue — interface may not have had a qdisc yet
			log.Printf("[RealQos] Clear qdisc on %s (may be clean): %v", ifaceName, err)
		}

		// Count enabled egress rules for this interface
		var enabledEgressRules []model.QosRule
		var enabledIngressRules []model.QosRule
		for _, r := range ifaceRules {
			if r.Status {
				if r.EgressRateMbps > 0 {
					enabledEgressRules = append(enabledEgressRules, r)
				}
				if r.IngressRateMbps > 0 {
					enabledIngressRules = append(enabledIngressRules, r)
				}
			}
		}

		if len(enabledEgressRules) == 0 && len(enabledIngressRules) == 0 {
			log.Printf("[RealQos] No enabled rules for %s, skipping setup", ifaceName)
			continue
		}

		// 2. Get the link
		link, err := netlink.LinkByName(ifaceName)
		if err != nil {
			// Interface not found — skip this interface but don't abort the remaining
			// interfaces' rules (self-healing: a missing/offline NIC must not block QoS
			// for the others). It re-applies on its own when the link comes back via the
			// InterfaceAdded event. Same tolerance pattern as real_routing.go.
			log.Printf("[RealQos] Warning: interface %q not found, skipping its QoS rules: %v", ifaceName, err)
			continue
		}

		// 3. Setup Egress (Client Download) Shaping
		if len(enabledEgressRules) > 0 {
			// Add root HTB qdisc (handle 1:0), default class 1:10
			htb := netlink.NewHtb(netlink.QdiscAttrs{
				LinkIndex: link.Attrs().Index,
				Handle:    netlink.MakeHandle(1, 0),
				Parent:    netlink.HANDLE_ROOT,
			})
			htb.Defcls = 10
			if err := netlink.QdiscAdd(htb); err != nil {
				return fmt.Errorf("[RealQos] add HTB root qdisc on %s: %w", ifaceName, err)
			}
			log.Printf("[RealQos] Added root HTB qdisc on %s", ifaceName)

			// Add HTB classes and U32 filters
			minorID := uint16(10)
			for _, rule := range enabledEgressRules {
				classHandle := netlink.MakeHandle(1, minorID)
				rateBits := uint64(rule.EgressRateMbps) * 1_000_000
				ceilBits := uint64(rule.EgressCeilMbps) * 1_000_000
				if ceilBits <= 0 {
					ceilBits = rateBits
				}

				class := netlink.NewHtbClass(
					netlink.ClassAttrs{
						LinkIndex: link.Attrs().Index,
						Handle:    classHandle,
						Parent:    netlink.MakeHandle(1, 0),
					},
					netlink.HtbClassAttrs{
						Rate: rateBits,
						Ceil: ceilBits,
					},
				)
				if err := netlink.ClassAdd(class); err != nil {
					return fmt.Errorf("[RealQos] add HTB class 1:%d for rule %q: %w", minorID, rule.Name, err)
				}
				log.Printf("[RealQos] Added class 1:%d — rate=%dMbps ceil=%dMbps for rule %q",
					minorID, rule.EgressRateMbps, rule.EgressCeilMbps, rule.Name)

				if err := addQosU32Filters(link, classHandle, rule, uint16(rule.Priority)); err != nil {
					return fmt.Errorf("[RealQos] add filter for rule %q: %w", rule.Name, err)
				}

				minorID++
			}
		}

		// 4. Setup Ingress (Client Upload) Shaping via IFB redirect
		if len(enabledIngressRules) > 0 {
			// Ensure IFB kernel module is loaded
			if err := execCommand("modprobe", "ifb").Run(); err != nil {
				log.Printf("[RealQos] Warning: modprobe ifb failed (ifb may be compiled-in): %v", err)
			}

			ifbName := "ifb-" + ifaceName

			// Attempt to add IFB link dynamically
			la := netlink.NewLinkAttrs()
			la.Name = ifbName
			ifbLink := &netlink.Ifb{LinkAttrs: la}
			if err := netlink.LinkAdd(ifbLink); err != nil {
				// Log but continue (link might already exist)
				log.Printf("[RealQos] LinkAdd %s info: %v", ifbName, err)
			}

			// Look up link to get dynamic attributes/index. If the IFB link is
			// unavailable (e.g. the board has no ifb module and modprobe/LinkAdd
			// above both failed), skip only this interface's ingress shaping instead
			// of aborting the whole sync — the other interfaces' QoS must still be
			// applied. Same skip+log tolerance as the LinkByName above.
			ifb, err := netlink.LinkByName(ifbName)
			if err != nil {
				log.Printf("[RealQos] Warning: IFB link %s not available, skipping ingress shaping for %s: %v", ifbName, ifaceName, err)
				continue
			}

			// Bring IFB link UP
			if err := netlink.LinkSetUp(ifb); err != nil {
				return fmt.Errorf("[RealQos] failed to bring IFB link %s UP: %w", ifbName, err)
			}

			// Add Ingress Qdisc on physical interface
			ingress := &netlink.Ingress{
				QdiscAttrs: netlink.QdiscAttrs{
					LinkIndex: link.Attrs().Index,
					Parent:    netlink.HANDLE_INGRESS,
				},
			}
			if err := netlink.QdiscAdd(ingress); err != nil {
				return fmt.Errorf("[RealQos] add Ingress qdisc on %s: %w", ifaceName, err)
			}

			// Add Redirect Filter (redirect all ingress physical traffic to IFB egress)
			redirectFilter := &netlink.U32{
				FilterAttrs: netlink.FilterAttrs{
					LinkIndex: link.Attrs().Index,
					Parent:    netlink.HANDLE_INGRESS,
					Priority:  1,
					Protocol:  unix.ETH_P_IP,
				},
				RedirIndex: ifb.Attrs().Index,
			}
			if err := netlink.FilterAdd(redirectFilter); err != nil {
				return fmt.Errorf("[RealQos] add redirect filter on %s: %w", ifaceName, err)
			}
			log.Printf("[RealQos] Redirected ingress traffic of %s to %s", ifaceName, ifbName)

			// Add root HTB Qdisc on IFB link (handle 1:0), default class 10
			ifbHtb := netlink.NewHtb(netlink.QdiscAttrs{
				LinkIndex: ifb.Attrs().Index,
				Handle:    netlink.MakeHandle(1, 0),
				Parent:    netlink.HANDLE_ROOT,
			})
			ifbHtb.Defcls = 10
			if err := netlink.QdiscAdd(ifbHtb); err != nil {
				return fmt.Errorf("[RealQos] add HTB root qdisc on %s: %w", ifbName, err)
			}

			// Create classes and filters on IFB link
			minorID := uint16(10)
			for _, rule := range enabledIngressRules {
				classHandle := netlink.MakeHandle(1, minorID)
				rateBits := uint64(rule.IngressRateMbps) * 1_000_000
				ceilBits := uint64(rule.IngressCeilMbps) * 1_000_000
				if ceilBits <= 0 {
					ceilBits = rateBits
				}

				class := netlink.NewHtbClass(
					netlink.ClassAttrs{
						LinkIndex: ifb.Attrs().Index,
						Handle:    classHandle,
						Parent:    netlink.MakeHandle(1, 0),
					},
					netlink.HtbClassAttrs{
						Rate: rateBits,
						Ceil: ceilBits,
					},
				)
				if err := netlink.ClassAdd(class); err != nil {
					return fmt.Errorf("[RealQos] add HTB class 1:%d on %s: %w", minorID, ifbName, err)
				}
				log.Printf("[RealQos] Added Ingress class 1:%d on %s — rate=%dMbps ceil=%dMbps for rule %q",
					minorID, ifbName, rule.IngressRateMbps, rule.IngressCeilMbps, rule.Name)

				if err := addQosU32Filters(ifb, classHandle, rule, uint16(rule.Priority)); err != nil {
					return fmt.Errorf("[RealQos] add filter on %s for rule %q: %w", ifbName, rule.Name, err)
				}

				minorID++
			}
		}
	}

	return nil
}

// ClearQosRules removes the root and ingress qdiscs from the interface, and deletes the IFB link.
func (q *RealQos) ClearQosRules(ifaceName string) error {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", ifaceName, err)
	}

	qdiscs, err := netlink.QdiscList(link)
	if err == nil {
		for _, qd := range qdiscs {
			parent := qd.Attrs().Parent
			if parent == netlink.HANDLE_ROOT || parent == netlink.HANDLE_INGRESS {
				if err := netlink.QdiscDel(qd); err != nil {
					log.Printf("[RealQos] Failed to delete qdisc (%x) on %s: %v", parent, ifaceName, err)
				}
			}
		}
	}

	// Delete IFB link
	ifbName := "ifb-" + ifaceName
	ifbLink, err := netlink.LinkByName(ifbName)
	if err == nil {
		if err := netlink.LinkDel(ifbLink); err != nil {
			log.Printf("[RealQos] Failed to delete IFB link %s: %v", ifbName, err)
		} else {
			log.Printf("[RealQos] Deleted IFB link %s", ifbName)
		}
	}

	return nil
}

// GetIfaceQosStatus returns the combined live qdisc and class state from physical and IFB interface.
func (q *RealQos) GetIfaceQosStatus(ifaceName string) (*model.QosIfaceStatus, error) {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %q not found: %w", ifaceName, err)
	}

	status := &model.QosIfaceStatus{
		Interface: ifaceName,
		Classes:   []model.QosClass{},
	}

	// 1. Fetch Egress qdiscs/classes
	qdiscs, err := netlink.QdiscList(link)
	if err == nil {
		for _, qd := range qdiscs {
			if qd.Attrs().Parent == netlink.HANDLE_ROOT {
				status.HasQdisc = true
				break
			}
		}
	}

	if status.HasQdisc {
		classes, err := netlink.ClassList(link, netlink.MakeHandle(1, 0))
		if err == nil {
			for _, cls := range classes {
				htbCls, ok := cls.(*netlink.HtbClass)
				if !ok {
					continue
				}
				handle := htbCls.Attrs().Handle
				major := handle >> 16
				minor := handle & 0xffff
				rateMbit := htbCls.Rate * 8 / 1_000_000
				ceilMbit := htbCls.Ceil * 8 / 1_000_000
				status.Classes = append(status.Classes, model.QosClass{
					ClassID: fmt.Sprintf("Egress %d:%d", major, minor),
					Rate:    fmt.Sprintf("%dMbit", rateMbit),
					Ceil:    fmt.Sprintf("%dMbit", ceilMbit),
				})
			}
		}
	}

	// 2. Fetch Ingress qdiscs/classes via IFB link
	ifbName := "ifb-" + ifaceName
	ifbLink, err := netlink.LinkByName(ifbName)
	if err == nil {
		status.HasQdisc = true // If IFB link is active, QoS is active

		classes, err := netlink.ClassList(ifbLink, netlink.MakeHandle(1, 0))
		if err == nil {
			for _, cls := range classes {
				htbCls, ok := cls.(*netlink.HtbClass)
				if !ok {
					continue
				}
				handle := htbCls.Attrs().Handle
				major := handle >> 16
				minor := handle & 0xffff
				rateMbit := htbCls.Rate * 8 / 1_000_000
				ceilMbit := htbCls.Ceil * 8 / 1_000_000
				status.Classes = append(status.Classes, model.QosClass{
					ClassID: fmt.Sprintf("Ingress %d:%d", major, minor),
					Rate:    fmt.Sprintf("%dMbit", rateMbit),
					Ceil:    fmt.Sprintf("%dMbit", ceilMbit),
				})
			}
		}
	}

	return status, nil
}

// ─── Helpers ──────────────────────────────────────────────────────────────────

// groupQosRulesByIface groups rules by their Interface field.
func groupQosRulesByIface(rules []model.QosRule) map[string][]model.QosRule {
	m := make(map[string][]model.QosRule)
	for _, r := range rules {
		m[r.Interface] = append(m[r.Interface], r)
	}
	return m
}

// addQosU32Filters adds U32 protocol-ip filters for src and/or dst CIDR matching.
// If both MatchSrcIP and MatchDstIP are empty, a catch-all filter (0.0.0.0/0) is added.
func addQosU32Filters(link netlink.Link, flowID uint32, rule model.QosRule, prio uint16) error {
	hasSrc := rule.MatchSrcIP != "" && rule.MatchSrcIP != "0.0.0.0/0"
	hasDst := rule.MatchDstIP != "" && rule.MatchDstIP != "0.0.0.0/0"

	if !hasSrc && !hasDst {
		// Catch-all: match everything to this class
		return addU32Filter(link, flowID, prio, "", 0)
	}

	if hasSrc {
		_, ipNet, err := net.ParseCIDR(rule.MatchSrcIP)
		if err != nil {
			return fmt.Errorf("invalid MatchSrcIP %q: %w", rule.MatchSrcIP, err)
		}
		// Source IP offset in IPv4 header = 12 bytes
		if err := addU32Filter(link, flowID, prio, ipNet.IP.String(), 12); err != nil {
			return fmt.Errorf("add src filter: %w", err)
		}
	}

	if hasDst {
		_, ipNet, err := net.ParseCIDR(rule.MatchDstIP)
		if err != nil {
			return fmt.Errorf("invalid MatchDstIP %q: %w", rule.MatchDstIP, err)
		}
		// Destination IP offset in IPv4 header = 16 bytes
		if err := addU32Filter(link, flowID, prio+1, ipNet.IP.String(), 16); err != nil {
			return fmt.Errorf("add dst filter: %w", err)
		}
	}

	return nil
}

// addU32Filter creates a single U32 filter. When ipStr is empty, a catch-all is created.
func addU32Filter(link netlink.Link, flowID uint32, prio uint16, ipStr string, offset int32) error {
	filter := &netlink.U32{
		FilterAttrs: netlink.FilterAttrs{
			LinkIndex: link.Attrs().Index,
			Parent:    netlink.MakeHandle(1, 0),
			Priority:  prio,
			Protocol:  unix.ETH_P_IP,
		},
		ClassId: flowID,
	}

	if ipStr != "" {
		ip := net.ParseIP(ipStr).To4()
		if ip == nil {
			return fmt.Errorf("failed to parse IPv4 address: %s", ipStr)
		}
		filter.Sel = &netlink.TcU32Sel{
			Flags: netlink.TC_U32_TERMINAL,
			Keys: []netlink.TcU32Key{
				{
					Mask: binary.BigEndian.Uint32([]byte{255, 255, 255, 255}),
					Val:  binary.BigEndian.Uint32(ip),
					Off:  offset,
				},
			},
		}
	}

	if err := netlink.FilterAdd(filter); err != nil {
		return fmt.Errorf("FilterAdd (offset=%d): %w", offset, err)
	}
	return nil
}
