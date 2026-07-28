package kernel

import (
	"testing"

	"pigate/internal/model"

	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

// Shared address/service maps for the buildRuleExpressions tests below.
func chainTestMaps() (map[string]model.AddressObject, map[string]model.ServiceObject) {
	addrs := map[string]model.AddressObject{
		"LAN": {ID: "a1", Name: "LAN", Type: "subnet", Value: "192.168.1.0/24"},
	}
	svcs := map[string]model.ServiceObject{
		"SSH": {ID: "s1", Name: "SSH", Protocol: "TCP", Port: "22"},
	}
	return addrs, svcs
}

func hasMark(exprs []expr.Any) bool {
	for _, e := range exprs {
		if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyMARK {
			return true
		}
	}
	return false
}

func hasLog(exprs []expr.Any) (*expr.Log, bool) {
	for _, e := range exprs {
		if l, ok := e.(*expr.Log); ok {
			return l, true
		}
	}
	return nil, false
}

func hasLimit(exprs []expr.Any) bool {
	for _, e := range exprs {
		if _, ok := e.(*expr.Limit); ok {
			return true
		}
	}
	return false
}

func hasVerdict(exprs []expr.Any, kind expr.VerdictKind) bool {
	for _, e := range exprs {
		if v, ok := e.(*expr.Verdict); ok && v.Kind == kind {
			return true
		}
	}
	return false
}

// TestBuildRuleExpressions_NoFwmarkOnInputOutput asserts Caution: NAT/fwmark
// must never be set on the input/output chains, even when Nat=true and the
// action is ACCEPT (the one condition that DOES set it on forward).
func TestBuildRuleExpressions_NoFwmarkOnInputOutput(t *testing.T) {
	addrs, svcs := chainTestMaps()

	for _, chain := range []string{model.PolicyChainInput, model.PolicyChainOutput} {
		ruleSets, err := buildRuleExpressions(
			chain,
			"eth0", "eth0", // both set; effective interface is trimmed internally
			"LAN", "LAN", "SSH", "TCP",
			"ACCEPT", false, true, /* nat=true */
			"[PiGate] TEST ACCEPT: ",
			addrs, svcs,
		)
		if err != nil {
			t.Fatalf("chain=%s: unexpected error: %v", chain, err)
		}
		for _, exprs := range ruleSets {
			if hasMark(exprs) {
				t.Errorf("chain=%s: rule must NOT set fwmark, but MetaKeyMARK found in %+v", chain, exprs)
			}
		}
	}

	// Sanity check: forward WITH nat=true+ACCEPT DOES set the mark (this is
	// the existing behaviour this test is contrasting against).
	ruleSets, err := buildRuleExpressions(
		model.PolicyChainForward,
		"eth0", "eth1",
		"LAN", "LAN", "SSH", "TCP",
		"ACCEPT", false, true,
		"[PiGate] FWD ACCEPT: ",
		addrs, svcs,
	)
	if err != nil {
		t.Fatalf("forward: unexpected error: %v", err)
	}
	if len(ruleSets) != 1 || !hasMark(ruleSets[0]) {
		t.Errorf("forward chain with nat=true+ACCEPT should set fwmark, got ruleSets=%+v", ruleSets)
	}
}

// TestBuildRuleExpressions_InputOutputSingleRuleWithNflog asserts the
// traffic-log-pagination plan's §2.6 change: now that input/output logs go
// to NFLOG group LocalNflogGroup (RAM) instead of printk/journald, a
// log-enabled input/output rule is a SINGLE nftables rule carrying match +
// Counter + Log(group=LocalNflogGroup) + Verdict, with no expr.Limit —
// mirroring the forward chain's shape, not the old two-rule split.
func TestBuildRuleExpressions_InputOutputSingleRuleWithNflog(t *testing.T) {
	addrs, svcs := chainTestMaps()

	for _, chain := range []string{model.PolicyChainInput, model.PolicyChainOutput} {
		ruleSets, err := buildRuleExpressions(
			chain,
			"", "", "LAN", "LAN", "SSH", "TCP",
			"DROP", true /* logEnabled */, false,
			"[PiGate] TEST DROP  : ",
			addrs, svcs,
		)
		if err != nil {
			t.Fatalf("chain=%s: unexpected error: %v", chain, err)
		}
		if len(ruleSets) != 1 {
			t.Fatalf("chain=%s: expected exactly 1 rule when log is enabled, got %d: %+v", chain, len(ruleSets), ruleSets)
		}

		rule := ruleSets[0]
		if hasLimit(rule) {
			t.Errorf("chain=%s: rule must NOT carry a rate Limit now that log goes to NFLOG, got %+v", chain, rule)
		}
		if !hasCounter(rule) {
			t.Errorf("chain=%s: rule must carry a Counter, got %+v", chain, rule)
		}
		logExpr, ok := hasLog(rule)
		if !ok {
			t.Fatalf("chain=%s: rule missing Log expr, got %+v", chain, rule)
		}
		if logExpr.Group != LocalNflogGroup {
			t.Errorf("chain=%s: expected Log.Group == LocalNflogGroup (%d), got %d", chain, LocalNflogGroup, logExpr.Group)
		}
		if string(logExpr.Data) != "[PiGate] TEST DROP  : " {
			t.Errorf("chain=%s: unexpected log prefix %q", chain, logExpr.Data)
		}
		if !hasVerdict(rule, expr.VerdictDrop) {
			t.Errorf("chain=%s: rule must carry the DROP verdict, got %+v", chain, rule)
		}
	}
}

// TestBuildRuleExpressions_LimitNeverWithVerdict is the general safety-net
// test called out in the plan's Caution 2 / §2.6: for every combination of
// chain/action/log/nat this project currently generates rules for, any
// nftables rule that carries an expr.Limit must NEVER also carry an
// expr.Verdict in the same rule (a rate-limited verdict rule silently lets
// packets that exceed the limit fall through to the next rule — a security
// leak, not just a style issue). This subsumes the old
// TestBuildRuleExpressions_LogSplitOnInputOutput check with a rule that must
// keep holding regardless of how buildRuleExpressions is refactored later.
func TestBuildRuleExpressions_LimitNeverWithVerdict(t *testing.T) {
	addrs, svcs := chainTestMaps()

	chains := []string{model.PolicyChainForward, model.PolicyChainInput, model.PolicyChainOutput}
	actions := []string{"ACCEPT", "DROP"}
	logOptions := []bool{true, false}
	natOptions := []bool{true, false}

	for _, chain := range chains {
		for _, action := range actions {
			for _, logEnabled := range logOptions {
				for _, nat := range natOptions {
					ruleSets, err := buildRuleExpressions(
						chain,
						"eth0", "eth0",
						"LAN", "LAN", "SSH", "TCP",
						action, logEnabled, nat,
						"[PiGate] TEST : ",
						addrs, svcs,
					)
					if err != nil {
						t.Fatalf("chain=%s action=%s log=%v nat=%v: unexpected error: %v", chain, action, logEnabled, nat, err)
					}
					for _, rule := range ruleSets {
						if hasLimit(rule) && (hasVerdict(rule, expr.VerdictAccept) || hasVerdict(rule, expr.VerdictDrop)) {
							t.Errorf("chain=%s action=%s log=%v nat=%v: rule has BOTH Limit and Verdict (security leak), got %+v",
								chain, action, logEnabled, nat, rule)
						}
					}
				}
			}
		}
	}

	// Also assert it against the three fixed structural log rules elsewhere
	// in real_firewall.go that legitimately DO carry a Limit (notLocal 3.4,
	// AUDIT, input final drop) — none of them may carry a Verdict either.
	fixedLimitLogRules := [][]expr.Any{
		{
			&expr.Limit{Type: expr.LimitTypePkts, Rate: 3, Unit: expr.LimitTimeMinute, Burst: 10, Over: false},
			localLogExpr("[PiGate]  INP DROP  : "),
		},
		{
			&expr.Limit{Type: expr.LimitTypePkts, Rate: 3, Unit: expr.LimitTimeMinute, Burst: 10, Over: false},
			&expr.Log{Key: uint32(1 << unix.NFTA_LOG_PREFIX), Data: []byte("[PiGate] INP AUDIT : ")},
		},
		{
			&expr.Limit{Type: expr.LimitTypePkts, Rate: 10, Unit: expr.LimitTimeSecond, Burst: 20, Over: false},
			localLogExpr("[PiGate] INP DROP  : "),
		},
	}
	for _, rule := range fixedLimitLogRules {
		if hasVerdict(rule, expr.VerdictAccept) || hasVerdict(rule, expr.VerdictDrop) {
			t.Errorf("fixed log-only rule must not carry a Verdict, got %+v", rule)
		}
	}
}

// TestBuildRuleExpressions_ForwardKeepsSingleRuleWithLog asserts the forward
// chain's log+verdict stay combined in one rule (NFLOG target, RAM ring
// buffer — no rate-limit/SD-card concern), unlike input/output above.
func TestBuildRuleExpressions_ForwardKeepsSingleRuleWithLog(t *testing.T) {
	addrs, svcs := chainTestMaps()

	ruleSets, err := buildRuleExpressions(
		model.PolicyChainForward,
		"", "", "LAN", "LAN", "SSH", "TCP",
		"DROP", true, false,
		"[PiGate] FWD DROP  : ",
		addrs, svcs,
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ruleSets) != 1 {
		t.Fatalf("expected exactly 1 rule for forward chain, got %d: %+v", len(ruleSets), ruleSets)
	}
	if hasLimit(ruleSets[0]) {
		t.Errorf("forward chain log rule should not need a rate Limit (NFLOG, not printk), got %+v", ruleSets[0])
	}
	if !hasVerdict(ruleSets[0], expr.VerdictDrop) {
		t.Errorf("forward chain single rule must carry the verdict, got %+v", ruleSets[0])
	}
}

// TestOutputIPv6DropExprs asserts the output chain's fixed IPv6 fail-closed
// rule exists with the expected shape (Caution 13): match nfproto == ipv6,
// then counter, then drop.
func TestOutputIPv6DropExprs(t *testing.T) {
	exprs := outputIPv6DropExprs()

	meta, ok := exprs[0].(*expr.Meta)
	if !ok || meta.Key != expr.MetaKeyNFPROTO {
		t.Fatalf("expected first expr to be Meta{Key: MetaKeyNFPROTO}, got %+v", exprs[0])
	}
	cmp, ok := exprs[1].(*expr.Cmp)
	if !ok || len(cmp.Data) != 1 || cmp.Data[0] != unix.NFPROTO_IPV6 {
		t.Fatalf("expected second expr to compare against NFPROTO_IPV6, got %+v", exprs[1])
	}
	if !hasCounter(exprs) {
		t.Errorf("expected a Counter expr, got %+v", exprs)
	}
	if !hasVerdict(exprs, expr.VerdictDrop) {
		t.Errorf("expected a Drop verdict, got %+v", exprs)
	}
}
