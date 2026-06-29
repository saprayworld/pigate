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
// Phase 1: Egress (Client Download) HTB shaping only.
// Phase 2: Ingress (Client Upload) via IFB redirect — not yet implemented.
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

		// 1. Clear existing qdisc (idempotent)
		if err := q.ClearQosRules(ifaceName); err != nil {
			// Log but continue — interface may not have had a qdisc yet
			log.Printf("[RealQos] Clear qdisc on %s (may be clean): %v", ifaceName, err)
		}

		// Count enabled egress rules for this interface
		enabledCount := 0
		for _, r := range ifaceRules {
			if r.Status && r.EgressRateMbps > 0 {
				enabledCount++
			}
		}
		if enabledCount == 0 {
			log.Printf("[RealQos] No enabled egress rules for %s, skipping qdisc setup", ifaceName)
			continue
		}

		// 2. Get the link
		link, err := netlink.LinkByName(ifaceName)
		if err != nil {
			return fmt.Errorf("[RealQos] interface %q not found: %w", ifaceName, err)
		}

		// 3. Add root HTB qdisc (handle 1:0), default class 1:10
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

		// 4. Add an HTB class and U32 filter for each enabled egress rule
		minorID := uint16(10)
		for _, rule := range ifaceRules {
			if !rule.Status || rule.EgressRateMbps <= 0 {
				continue
			}

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

			// 5. Add U32 filters (src IP and/or dst IP)
			if err := addQosU32Filters(link, classHandle, rule, uint16(rule.Priority)); err != nil {
				return fmt.Errorf("[RealQos] add filter for rule %q: %w", rule.Name, err)
			}

			minorID++
		}
	}

	return nil
}

// ClearQosRules removes the root qdisc from an interface, cascading all classes/filters.
func (q *RealQos) ClearQosRules(ifaceName string) error {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return fmt.Errorf("interface %q not found: %w", ifaceName, err)
	}

	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		return fmt.Errorf("list qdiscs on %s: %w", ifaceName, err)
	}

	for _, qd := range qdiscs {
		if qd.Attrs().Parent == netlink.HANDLE_ROOT {
			if err := netlink.QdiscDel(qd); err != nil {
				log.Printf("[RealQos] Failed to delete root qdisc on %s: %v", ifaceName, err)
				return err
			}
			log.Printf("[RealQos] Deleted root qdisc on %s", ifaceName)
		}
	}
	return nil
}

// GetIfaceQosStatus returns the live qdisc and HTB class state from the kernel.
func (q *RealQos) GetIfaceQosStatus(ifaceName string) (*model.QosIfaceStatus, error) {
	link, err := netlink.LinkByName(ifaceName)
	if err != nil {
		return nil, fmt.Errorf("interface %q not found: %w", ifaceName, err)
	}

	status := &model.QosIfaceStatus{
		Interface: ifaceName,
		Classes:   []model.QosClass{},
	}

	qdiscs, err := netlink.QdiscList(link)
	if err != nil {
		return nil, fmt.Errorf("list qdiscs on %s: %w", ifaceName, err)
	}
	for _, qd := range qdiscs {
		if qd.Attrs().Parent == netlink.HANDLE_ROOT {
			status.HasQdisc = true
			break
		}
	}

	if !status.HasQdisc {
		return status, nil
	}

	classes, err := netlink.ClassList(link, netlink.MakeHandle(1, 0))
	if err != nil {
		return nil, fmt.Errorf("list classes on %s: %w", ifaceName, err)
	}

	for _, cls := range classes {
		htbCls, ok := cls.(*netlink.HtbClass)
		if !ok {
			continue
		}
		handle := htbCls.Attrs().Handle
		major := handle >> 16
		minor := handle & 0xffff
		// Convert bytes/sec back to Mbit/s for display
		rateMbit := htbCls.Rate * 8 / 1_000_000
		ceilMbit := htbCls.Ceil * 8 / 1_000_000
		status.Classes = append(status.Classes, model.QosClass{
			ClassID: fmt.Sprintf("%d:%d", major, minor),
			Rate:    fmt.Sprintf("%dMbit", rateMbit),
			Ceil:    fmt.Sprintf("%dMbit", ceilMbit),
		})
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
