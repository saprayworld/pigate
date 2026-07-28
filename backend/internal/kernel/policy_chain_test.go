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

// TestBuildRuleExpressions_LogSplitOnInputOutput asserts Caution 5: a
// log-enabled input/output rule must be split into a rate-limited log-only
// rule and a separate counter+verdict rule — never a single rule combining
// `limit ... log ... <verdict>`, which would only apply the verdict to
// packets that pass the rate limiter and let the rest fall through.
func TestBuildRuleExpressions_LogSplitOnInputOutput(t *testing.T) {
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
		if len(ruleSets) != 2 {
			t.Fatalf("chain=%s: expected 2 rules when log is enabled, got %d: %+v", chain, len(ruleSets), ruleSets)
		}

		logRule := ruleSets[0]
		if !hasLimit(logRule) {
			t.Errorf("chain=%s: log-only rule must carry a rate Limit, got %+v", chain, logRule)
		}
		logExpr, ok := hasLog(logRule)
		if !ok {
			t.Fatalf("chain=%s: log-only rule missing Log expr, got %+v", chain, logRule)
		}
		if string(logExpr.Data) != "[PiGate] TEST DROP  : " {
			t.Errorf("chain=%s: unexpected log prefix %q", chain, logExpr.Data)
		}
		if hasVerdict(logRule, expr.VerdictDrop) || hasVerdict(logRule, expr.VerdictAccept) {
			t.Errorf("chain=%s: log-only rule must NOT carry a verdict (Caution 5 leak), got %+v", chain, logRule)
		}

		verdictRule := ruleSets[1]
		if !hasVerdict(verdictRule, expr.VerdictDrop) {
			t.Errorf("chain=%s: second rule must carry the DROP verdict, got %+v", chain, verdictRule)
		}
		if _, ok := hasLog(verdictRule); ok {
			t.Errorf("chain=%s: verdict rule must not duplicate the Log expr, got %+v", chain, verdictRule)
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
