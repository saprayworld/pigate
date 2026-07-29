//go:build linux

package kernel

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"

	"pigate/internal/model"
)

// maxTrackedFlowsPerDump caps how many conntrack flows a single DumpFlows call
// processes (per IP family), so a port-scan/DDoS that inflates the conntrack
// table cannot turn a routine poll into an unbounded memory/CPU spike (plan
// Caution 5).
const maxTrackedFlowsPerDump = 50000

// RealTrafficAccounting implements TrafficAccountingManager using conntrack
// (via vishvananda/netlink) for per-flow byte accounting and nftables rule
// counters (via google/nftables) for per-DB-rule byte accounting. Both
// methods are read-only.
type RealTrafficAccounting struct{}

func NewRealTrafficAccounting() *RealTrafficAccounting {
	return &RealTrafficAccounting{}
}

// DumpFlows returns a snapshot of the conntrack table (IPv4 + IPv6). Each
// netlink.ConntrackTableList call opens and closes its own netlink socket
// internally (no persistent fd held by this method), so there is nothing to
// leak here even if this is called on every poll tick. IPv4 and IPv6 are
// dumped independently and a failure on one family (e.g. IPv6 disabled on
// the host) is logged and skipped rather than aborting the whole dump (plan
// Caution 10); only when both families fail does this return an error.
func (r *RealTrafficAccounting) DumpFlows() ([]model.FlowSample, error) {
	var samples []model.FlowSample
	var v4Err, v6Err error

	if flows, err := safeConntrackList(unix.AF_INET); err != nil {
		v4Err = err
		log.Printf("[RealTrafficAccounting] IPv4 conntrack dump failed: %v", err)
	} else {
		samples = append(samples, flowsToSamples(flows, maxTrackedFlowsPerDump)...)
	}

	if flows, err := safeConntrackList(unix.AF_INET6); err != nil {
		v6Err = err
		log.Printf("[RealTrafficAccounting] IPv6 conntrack dump failed: %v", err)
	} else {
		remaining := maxTrackedFlowsPerDump
		if len(samples) < maxTrackedFlowsPerDump {
			remaining = maxTrackedFlowsPerDump - len(samples)
		} else {
			remaining = 0
		}
		samples = append(samples, flowsToSamples(flows, remaining)...)
	}

	if v4Err != nil && v6Err != nil {
		return nil, fmt.Errorf("conntrack dump failed for both IPv4 (%v) and IPv6 (%v)", v4Err, v6Err)
	}
	return samples, nil
}

// safeConntrackList wraps netlink.ConntrackTableList with a panic recovery
// guard: a malformed/unexpected netlink payload from the kernel must degrade
// this poller, never crash the whole pigate process (plan Caution: "ไม่ panic
// บน payload ผิดรูป").
func safeConntrackList(family netlink.InetFamily) (flows []*netlink.ConntrackFlow, err error) {
	defer func() {
		if rec := recover(); rec != nil {
			err = fmt.Errorf("recovered from panic while parsing conntrack payload: %v", rec)
		}
	}()
	return netlink.ConntrackTableList(netlink.ConntrackTable, family)
}

// flowsToSamples converts up to limit conntrack flows into model.FlowSample,
// skipping any flow that lacks a usable Forward tuple (defensive against a
// malformed/partial entry) rather than letting a nil pointer panic the poller.
func flowsToSamples(flows []*netlink.ConntrackFlow, limit int) []model.FlowSample {
	if limit <= 0 {
		return nil
	}
	out := make([]model.FlowSample, 0, min(len(flows), limit))
	for _, f := range flows {
		if f == nil || f.Forward.SrcIP == nil || f.Forward.DstIP == nil {
			continue
		}
		if len(out) >= limit {
			break
		}
		bytes := f.Forward.Bytes + f.Reverse.Bytes
		out = append(out, model.FlowSample{
			Key:     flowKey(f),
			SrcIP:   f.Forward.SrcIP.String(),
			DstIP:   f.Forward.DstIP.String(),
			Proto:   f.Forward.Protocol,
			DstPort: f.Forward.DstPort,
			Bytes:   bytes,
		})
	}
	return out
}

// flowKey hashes the poll-side conntrack flow's 5-tuple into the same stable
// string that real_conntrack_events.go computes for the equivalent DESTROY
// event, via the shared flowKeyFromParts (see its doc comment for why
// TimeStart is deliberately NOT part of the key as of Phase 2).
func flowKey(f *netlink.ConntrackFlow) string {
	return flowKeyFromParts(f.FamilyType, f.Forward.Protocol,
		f.Forward.SrcIP.String(), f.Forward.SrcPort,
		f.Forward.DstIP.String(), f.Forward.DstPort)
}

// flowKeyFromParts hashes (family, proto, srcIP, srcPort, dstIP, dstPort)
// into a stable string shared by both the poll path (flowKey above, via
// netlink.ConntrackFlow) and the conntrack DESTROY event path
// (real_conntrack_events.go, via CTA_TUPLE_ORIG attributes). The two callers
// MUST produce an identical key for the same flow, or the service-layer
// aggregator (service/traffic_stats.go onFlowEnd) will treat a DESTROY event
// as a brand-new, never-seen flow and credit its full byte count on top of
// whatever the poll already counted — a silent double-count (plan Caution 2).
//
// Deliberately excludes conntrack's TimeStart (CTA_TIMESTAMP), even though a
// 5-tuple can in principle be reused by a new flow shortly after the old one
// died (NAT port reuse). TimeStart is only populated when
// net.netfilter.nf_conntrack_timestamp=1, and T-01 (verifying that sysctl's
// default on the target board) was out of scope for this phase — see
// docs/ref/todo/traffic-accounting-accuracy-phase2-plan.md T-01. Parsing
// CTA_TIMESTAMP on the event side while the poll side sees a zero/absent
// TimeStart (or vice versa) would itself break key parity and cause the
// double-count this function exists to prevent, which is strictly worse than
// the rare 5-tuple-reuse mis-attribution this omission accepts.
func flowKeyFromParts(family uint8, proto uint8, srcIP string, srcPort uint16, dstIP string, dstPort uint16) string {
	h := sha256.New()
	fmt.Fprintf(h, "%d|%d|%s|%d|%s|%d",
		family, proto, srcIP, srcPort, dstIP, dstPort)
	return hex.EncodeToString(h.Sum(nil))
}

// DumpRuleCounters reads the current nftables per-rule packet/byte counters
// for every user policy rule across all three chains (input/forward/output),
// summing every nft rule expansion that shares a DB rule id in its UserData
// comment (see real_firewall.go applyUserRules / addUserChainRules). Rules
// with no UserData (the fixed structural rules — ct-state checks, DHCP/DNS
// access, final drop-log, etc.) are skipped; they have no DB rule id to
// attribute traffic to.
func (r *RealTrafficAccounting) DumpRuleCounters() (map[string]model.RuleCounter, error) {
	conn, err := nftables.New()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to nftables: %w", err)
	}

	table := &nftables.Table{Name: "pigate", Family: nftables.TableFamilyINet}
	out := make(map[string]model.RuleCounter)

	var lastErr error
	chains := []string{"input", "forward", "output"}
	okCount := 0
	for _, chainName := range chains {
		chain := &nftables.Chain{Name: chainName, Table: table}
		rules, err := conn.GetRules(table, chain)
		if err != nil {
			log.Printf("[RealTrafficAccounting] Failed to read %q chain rule counters: %v", chainName, err)
			lastErr = err
			continue
		}
		okCount++
		accumulateRuleCounters(rules, out)
	}

	if okCount == 0 {
		return nil, fmt.Errorf("failed to read rule counters from any chain: %w", lastErr)
	}
	return out, nil
}

// accumulateRuleCounters decodes each rule's UserData comment (a DB rule id)
// and sums its expr.Counter into out. A rule with no UserData or no counter
// expression is silently skipped (structural rule, not a user policy rule).
func accumulateRuleCounters(rules []*nftables.Rule, out map[string]model.RuleCounter) {
	for _, rule := range rules {
		if rule == nil || len(rule.UserData) == 0 {
			continue
		}
		ruleID, ok := userdata.GetString(rule.UserData, userdata.TypeComment)
		if !ok || ruleID == "" {
			continue
		}
		for _, e := range rule.Exprs {
			counter, ok := e.(*expr.Counter)
			if !ok {
				continue
			}
			cur := out[ruleID]
			cur.Bytes += counter.Bytes
			cur.Packets += counter.Packets
			out[ruleID] = cur
		}
	}
}
