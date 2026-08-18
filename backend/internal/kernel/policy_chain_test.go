package kernel

import (
	"net"
	"reflect"
	"strings"
	"testing"

	"pigate/internal/model"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// lanCombo/sshCombo are the single-entry addrCombo/svcCombo fixtures the
// buildRuleExpressions tests below use in place of the old
// name+addrsMap/svcsMap lookups (buildRuleExpressions no longer resolves
// object names itself — that now happens one level up via addressCombos/
// serviceCombos, see T-04/T-05 of docs/ref/todo/
// multi-value-address-service-objects-plan.md).
func lanCombo() addrCombo {
	return addrCombo{hasFilter: true, objName: "LAN", entry: model.AddressEntry{Type: "subnet", Value: "192.168.1.0/24"}}
}

func sshCombo() svcCombo {
	return svcCombo{hasFilter: true, objName: "SSH", protocol: "TCP", port: "22"}
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
	for _, chain := range []string{model.PolicyChainInput, model.PolicyChainOutput} {
		ruleSets, err := buildRuleExpressions(
			chain,
			[]string{"eth0"}, []string{"eth0"}, // both set; effective interface is trimmed internally
			lanCombo(), lanCombo(), sshCombo(),
			"ACCEPT", false, true, /* nat=true */
			"[PiGate] TEST ACCEPT: ",
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
		[]string{"eth0"}, []string{"eth1"},
		lanCombo(), lanCombo(), sshCombo(),
		"ACCEPT", false, true,
		"[PiGate] FWD ACCEPT: ",
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
	for _, chain := range []string{model.PolicyChainInput, model.PolicyChainOutput} {
		ruleSets, err := buildRuleExpressions(
			chain,
			nil, nil, lanCombo(), lanCombo(), sshCombo(),
			"DROP", true /* logEnabled */, false,
			"[PiGate] TEST DROP  : ",
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
						[]string{"eth0"}, []string{"eth0"},
						lanCombo(), lanCombo(), sshCombo(),
						action, logEnabled, nat,
						"[PiGate] TEST : ",
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
	ruleSets, err := buildRuleExpressions(
		model.PolicyChainForward,
		nil, nil, lanCombo(), lanCombo(), sshCombo(),
		"DROP", true, false,
		"[PiGate] FWD DROP  : ",
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

// stubLookupIP overrides the package-level lookupIP var for the duration of
// a test and restores it afterwards, so FQDN address-object tests don't
// depend on real DNS.
func stubLookupIP(t *testing.T, ips []net.IP, err error) {
	t.Helper()
	orig := lookupIP
	lookupIP = func(string) ([]net.IP, error) { return ips, err }
	t.Cleanup(func() { lookupIP = orig })
}

// TestAddressCombos_FQDNExpandsToOneComboPerResolvedIPv4 asserts that an
// address object with an "fqdn" entry resolving to several IPv4 addresses
// expands into one addrCombo per resolved address (not just the first),
// since this project does not use nftables sets (D-2) to express "match any
// of these IPs" — every combo must become its own nft rule downstream.
func TestAddressCombos_FQDNExpandsToOneComboPerResolvedIPv4(t *testing.T) {
	want := []net.IP{
		net.ParseIP("203.0.113.10").To4(),
		net.ParseIP("203.0.113.11").To4(),
		net.ParseIP("203.0.113.12").To4(),
	}
	stubLookupIP(t, want, nil)

	addrs := map[string]model.AddressObject{
		"EXAMPLE": {ID: "a2", Name: "EXAMPLE", Entries: []model.AddressEntry{{Type: "fqdn", Value: "example.test"}}},
	}

	combos, err := addressCombos("EXAMPLE", addrs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(combos) != len(want) {
		t.Fatalf("expected %d combos (one per resolved IPv4), got %d: %+v", len(want), len(combos), combos)
	}
	seen := make(map[string]bool)
	for _, c := range combos {
		if !c.hasFilter || c.resolvedFQDNIP == nil {
			t.Fatalf("expected every combo to be a resolved FQDN filter, got %+v", c)
		}
		seen[c.resolvedFQDNIP.String()] = true
	}
	for _, ip := range want {
		if !seen[ip.String()] {
			t.Errorf("resolved IP %s was not turned into a combo", ip)
		}
	}
}

// TestAddressCombos_FQDNResolvedIPsAreCapped asserts the maxFQDNResolvedIPs
// safety cap: a domain resolving to an unusually large number of A records
// still produces a bounded number of combos instead of growing unbounded.
func TestAddressCombos_FQDNResolvedIPsAreCapped(t *testing.T) {
	var many []net.IP
	for i := 0; i < maxFQDNResolvedIPs+12; i++ {
		many = append(many, net.IPv4(203, 0, 113, byte(i)).To4())
	}
	stubLookupIP(t, many, nil)

	addrs := map[string]model.AddressObject{
		"BIGFAN": {ID: "a3", Name: "BIGFAN", Entries: []model.AddressEntry{{Type: "fqdn", Value: "bigfanout.test"}}},
	}

	combos, err := addressCombos("BIGFAN", addrs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(combos) != maxFQDNResolvedIPs {
		t.Fatalf("expected the resolved-IP count to be capped at %d, got %d", maxFQDNResolvedIPs, len(combos))
	}
}

// TestAddressCombos_FQDNResolveFailureSkipsOnlyThatEntry asserts plan
// Caution 3: a failing "fqdn" entry is skipped (no combo, no error from
// addressCombos), while a sibling entry in the same object still expands
// normally.
func TestAddressCombos_FQDNResolveFailureSkipsOnlyThatEntry(t *testing.T) {
	stubLookupIP(t, nil, &net.DNSError{Err: "no such host", IsNotFound: true})

	addrs := map[string]model.AddressObject{
		"MIXED": {ID: "a4", Name: "MIXED", Entries: []model.AddressEntry{
			{Type: "fqdn", Value: "does-not-resolve.test"},
			{Type: "subnet", Value: "10.1.2.3/32"},
		}},
	}

	combos, err := addressCombos("MIXED", addrs, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(combos) != 1 {
		t.Fatalf("expected only the subnet entry to survive, got %d combos: %+v", len(combos), combos)
	}
	if combos[0].entry.Type != "subnet" || combos[0].entry.Value != "10.1.2.3/32" {
		t.Errorf("expected the surviving combo to be the subnet entry, got %+v", combos[0])
	}
}

// TestBuildRuleExpressions_FQDNComboMatchesResolvedIP is an end-to-end check
// that a resolved-FQDN addrCombo produces a valid single-IP match rule via
// buildRuleExpressions (not just addressCombos in isolation).
func TestBuildRuleExpressions_FQDNComboMatchesResolvedIP(t *testing.T) {
	resolvedIP := net.ParseIP("203.0.113.42").To4()
	fqdnCombo := addrCombo{
		hasFilter:      true,
		objName:        "EXAMPLE",
		entry:          model.AddressEntry{Type: "fqdn", Value: "example.test"},
		resolvedFQDNIP: resolvedIP,
	}

	ruleSets, err := buildRuleExpressions(
		model.PolicyChainForward,
		nil, nil,
		fqdnCombo, addrCombo{hasFilter: false}, sshCombo(),
		"ACCEPT", false, false,
		"[PiGate] FWD ACCEPT: ",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ruleSets) != 1 {
		t.Fatalf("expected exactly 1 rule, got %d: %+v", len(ruleSets), ruleSets)
	}

	var found bool
	for _, e := range ruleSets[0] {
		if c, ok := e.(*expr.Cmp); ok && c.Op == expr.CmpOpEq && net.IP(c.Data).Equal(resolvedIP) {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a Cmp against the resolved IP %s, got %+v", resolvedIP, ruleSets[0])
	}
	if !hasVerdict(ruleSets[0], expr.VerdictAccept) {
		t.Errorf("expected the ACCEPT verdict, got %+v", ruleSets[0])
	}
}

// TestBuildRuleExpressions_SingleValueAddressStillYieldsOneRule is a
// regression guard: subnet/range address objects (the overwhelmingly common
// case) must keep producing exactly one nft rule per combination, unchanged
// by the FQDN multi-IP expansion added alongside this test.
func TestBuildRuleExpressions_SingleValueAddressStillYieldsOneRule(t *testing.T) {
	ruleSets, err := buildRuleExpressions(
		model.PolicyChainForward,
		nil, nil,
		lanCombo(), addrCombo{hasFilter: false}, sshCombo(),
		"ACCEPT", false, false,
		"[PiGate] FWD ACCEPT: ",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ruleSets) != 1 {
		t.Fatalf("expected exactly 1 rule for a single-value subnet object, got %d: %+v", len(ruleSets), ruleSets)
	}
}

// TestBuildRuleExpressions_SingleInterfaceByteIdentical is a regression guard
// (docs/ref/todo/multi-interface-firewall-rule-plan.md T-06/T-10, plan
// Caution 3): a single-interface (or ALL/empty) in/out list must still
// produce exactly the same iifname/oifname exprs as before this function
// accepted lists.
func TestBuildRuleExpressions_SingleInterfaceByteIdentical(t *testing.T) {
	ruleSets, err := buildRuleExpressions(
		model.PolicyChainForward,
		[]string{"eth0"}, []string{"eth1"},
		lanCombo(), lanCombo(), sshCombo(),
		"ACCEPT", false, false,
		"[PiGate] FWD ACCEPT: ",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ruleSets) != 1 {
		t.Fatalf("expected exactly 1 rule for single in/out interfaces, got %d: %+v", len(ruleSets), ruleSets)
	}
	exprs := ruleSets[0]
	var iifCmp, oifCmp *expr.Cmp
	for i, e := range exprs {
		if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyIIFNAME {
			iifCmp = exprs[i+1].(*expr.Cmp)
		}
		if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyOIFNAME {
			oifCmp = exprs[i+1].(*expr.Cmp)
		}
	}
	if iifCmp == nil || string(iifCmp.Data[:4]) != "eth0" {
		t.Errorf("expected iifname cmp for eth0, got %+v", iifCmp)
	}
	if oifCmp == nil || string(oifCmp.Data[:4]) != "eth1" {
		t.Errorf("expected oifname cmp for eth1, got %+v", oifCmp)
	}

	// ALL / empty must not emit any iifname/oifname expr at all.
	ruleSetsAll, err := buildRuleExpressions(
		model.PolicyChainForward,
		[]string{"ALL"}, nil,
		lanCombo(), lanCombo(), sshCombo(),
		"ACCEPT", false, false,
		"[PiGate] FWD ACCEPT: ",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ruleSetsAll) != 1 {
		t.Fatalf("expected exactly 1 rule for ALL/empty interfaces, got %d", len(ruleSetsAll))
	}
	for _, e := range ruleSetsAll[0] {
		if m, ok := e.(*expr.Meta); ok && (m.Key == expr.MetaKeyIIFNAME || m.Key == expr.MetaKeyOIFNAME) {
			t.Errorf("expected no iifname/oifname expr for ALL/empty interfaces, got %+v", ruleSetsAll[0])
		}
	}
}

// TestBuildRuleExpressions_MultiInterfaceCartesian is the core D-1 Option A
// assertion: in=[eth0,wlan0], out=[eth1] on the forward chain must produce
// exactly 2 rulesets (one per in x out pair), each carrying the correct
// iifname/oifname match.
func TestBuildRuleExpressions_MultiInterfaceCartesian(t *testing.T) {
	ruleSets, err := buildRuleExpressions(
		model.PolicyChainForward,
		[]string{"eth0", "wlan0"}, []string{"eth1"},
		lanCombo(), lanCombo(), sshCombo(),
		"ACCEPT", false, false,
		"[PiGate] FWD ACCEPT: ",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ruleSets) != 2 {
		t.Fatalf("expected exactly 2 rulesets (2 in x 1 out), got %d: %+v", len(ruleSets), ruleSets)
	}

	var gotIn []string
	for _, exprs := range ruleSets {
		var iifName, oifName string
		for i, e := range exprs {
			if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyIIFNAME {
				iifName = strings.TrimRight(string(exprs[i+1].(*expr.Cmp).Data), "\x00")
			}
			if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyOIFNAME {
				oifName = strings.TrimRight(string(exprs[i+1].(*expr.Cmp).Data), "\x00")
			}
		}
		if oifName != "eth1" {
			t.Errorf("expected oifname=eth1 on every ruleset, got %q", oifName)
		}
		gotIn = append(gotIn, iifName)
	}
	if !reflect.DeepEqual(gotIn, []string{"eth0", "wlan0"}) {
		t.Errorf("expected iifname order [eth0 wlan0] (in as outer loop), got %v", gotIn)
	}
}

// TestBuildRuleExpressions_InputChainIgnoresOutInterfaces asserts the
// chain-scoped list clearing still holds with multiple interfaces: passing a
// multi-value outIfaces on the input chain must not produce any oifname expr
// or multiply the ruleset count.
func TestBuildRuleExpressions_InputChainIgnoresOutInterfaces(t *testing.T) {
	ruleSets, err := buildRuleExpressions(
		model.PolicyChainInput,
		[]string{"eth0", "wlan0"}, []string{"eth1", "eth2"},
		lanCombo(), lanCombo(), sshCombo(),
		"ACCEPT", false, false,
		"[PiGate] INP ACCEPT: ",
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(ruleSets) != 2 {
		t.Fatalf("expected exactly 2 rulesets (2 in x 1 ignored-out), got %d: %+v", len(ruleSets), ruleSets)
	}
	for _, exprs := range ruleSets {
		for _, e := range exprs {
			if m, ok := e.(*expr.Meta); ok && m.Key == expr.MetaKeyOIFNAME {
				t.Errorf("input chain must never emit an oifname expr, got %+v", exprs)
			}
		}
	}
}

// TestAddUserChainRules_ExpansionCapAppliesAcrossInterfacePairs asserts the
// existing maxExpandedRulesPerPolicy cap still truncates expansion once the
// new in x out multiplier is added to the source x dest x service cartesian
// product (plan Caution 2: "ห้าม bypass"). Uses nftables.WithTestDial (the
// same pattern the google/nftables package's own tests use) so AddRule's
// buffered netlink messages can be counted via Flush without touching a real
// netlink socket.
func TestAddUserChainRules_ExpansionCapAppliesAcrossInterfacePairs(t *testing.T) {
	rule := model.PolicyRule{
		ID:           "cap-test",
		Name:         "cap test",
		Chain:        model.PolicyChainForward,
		Status:       true,
		Action:       "ACCEPT",
		InInterfaces: []string{"eth0", "eth1", "wlan0"}, // 3 in x 1 out = 3 combos per (src,dst,svc)
		Source:       []string{"LAN"},
		Destination:  []string{"LAN"},
		Service:      []string{"SSH"},
	}
	addrsMap := map[string]model.AddressObject{
		"LAN": {ID: "a1", Name: "LAN", Type: "subnet", Value: "192.168.1.0/24"},
	}
	svcsMap := map[string]model.ServiceObject{
		"SSH": {ID: "s1", Name: "SSH", Protocol: "TCP", Port: "22"},
	}

	newRuleHeaderType := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWRULE)
	var newRuleCount int
	conn, err := nftables.New(nftables.WithTestDial(
		func(req []netlink.Message) ([]netlink.Message, error) {
			for _, msg := range req {
				if msg.Header.Type == newRuleHeaderType {
					newRuleCount++
				}
			}
			return req, nil
		}))
	if err != nil {
		t.Fatalf("nftables.New: %v", err)
	}

	table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: "pigate_test"})
	chain := conn.AddChain(&nftables.Chain{Name: "forward_test", Table: table})

	addUserChainRules(conn, table, chain, model.PolicyChainForward, []model.PolicyRule{rule}, addrsMap, svcsMap,
		"[PiGate] FWD ACCEPT: ", "[PiGate] FWD DROP  : ", 2 /* cap smaller than the 3 in x out combos */, nil)

	if err := conn.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}

	if newRuleCount > 2 {
		t.Errorf("expected addUserChainRules to stop at the cap (2), got %d rules added", newRuleCount)
	}
	if newRuleCount == 0 {
		t.Errorf("expected at least 1 rule to be added before hitting the cap")
	}
}
