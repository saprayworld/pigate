package kernel

import (
	"errors"
	"fmt"
	"testing"

	"pigate/internal/model"

	"github.com/google/nftables"
	"github.com/mdlayher/netlink"
	"github.com/mdlayher/netlink/nltest"
	"golang.org/x/sys/unix"
)

// swapFirewallConn replaces newFirewallConn with a WithTestDial connection
// for the duration of a test. The returned func reports how many times the
// Flush batches that reached the test dial were started (counted by
// NFNL_MSG_BATCH_BEGIN — google/nftables feeds the dial in chunks, so the
// raw dial-call count is not the Flush count).
func swapFirewallConn(t *testing.T) (flushes func() int) {
	t.Helper()
	calls := 0
	orig := newFirewallConn
	newFirewallConn = func() (*nftables.Conn, error) {
		return nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
			for _, m := range req {
				if m.Header.Type == netlink.HeaderType(unix.NFNL_MSG_BATCH_BEGIN) {
					calls++
				}
			}
			return req, nil
		}))
	}
	t.Cleanup(func() { newFirewallConn = orig })
	return func() int { return calls }
}

func manyAcceptRules(n int) []model.PolicyRule {
	rules := make([]model.PolicyRule, n)
	for i := range rules {
		rules[i] = model.PolicyRule{
			ID: fmt.Sprintf("r%d", i), Name: fmt.Sprintf("r%d", i), Chain: model.PolicyChainInput,
			Status: true, Action: "ACCEPT",
		}
	}
	return rules
}

func TestNftSocketBufferOption_AlwaysNil(t *testing.T) {
	// nltest conn has no real socket: SyscallConn is unsupported, so this
	// exercises the fallback path and the "never fail the dial" contract.
	c := nltest.Dial(func(req []netlink.Message) ([]netlink.Message, error) { return req, nil })
	defer c.Close()
	if err := nftSocketBufferOption(c); err != nil {
		t.Fatalf("nftSocketBufferOption returned %v, want nil", err)
	}

	// A real (unprivileged) netlink socket, when the environment allows one.
	rc, err := netlink.Dial(12 /* NETLINK_NETFILTER */, nil)
	if err != nil {
		t.Logf("real netlink socket unavailable, skipping real-socket half: %v", err)
		return
	}
	defer rc.Close()
	if err := nftSocketBufferOption(rc); err != nil {
		t.Fatalf("nftSocketBufferOption on real socket returned %v, want nil", err)
	}
}

func TestCheckRuleBudget(t *testing.T) {
	if err := checkRuleBudget(1024, 1024); err != nil {
		t.Fatalf("at the limit must pass, got %v", err)
	}
	if err := checkRuleBudget(1025, 1024); !errors.Is(err, ErrNftRuleBudgetExceeded) {
		t.Fatalf("over the limit must wrap ErrNftRuleBudgetExceeded, got %v", err)
	}
}

func TestCountingBatch_CountsRulesSetsElems(t *testing.T) {
	conn, err := nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) { return req, nil }))
	if err != nil {
		t.Fatal(err)
	}
	table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: "t"})
	chain := conn.AddChain(&nftables.Chain{Name: "c", Table: table})
	cb := &countingBatch{inner: conn}
	cb.AddRule(&nftables.Rule{Table: table, Chain: chain})
	cb.AddRule(&nftables.Rule{Table: table, Chain: chain})
	set := &nftables.Set{Table: table, Anonymous: true, Constant: true, KeyType: nftables.TypeInetProto}
	if err := cb.AddSet(set, []nftables.SetElement{{Key: []byte{6}}, {Key: []byte{17}}}); err != nil {
		t.Fatal(err)
	}
	if cb.rules != 2 || cb.sets != 1 || cb.elems != 2 {
		t.Fatalf("got rules=%d sets=%d elems=%d, want 2/1/2", cb.rules, cb.sets, cb.elems)
	}
}

// TestApplyRules_BudgetExceededNeverFlushes asserts an oversized ruleset is
// rejected with ErrNftRuleBudgetExceeded before any message is sent, and that
// the FQDN snapshot is left untouched. Runs in legacy mode, where 1100
// policies are 1100 nft rules.
func TestApplyRules_BudgetExceededNeverFlushes(t *testing.T) {
	flushes := swapFirewallConn(t)
	rf := NewRealFirewall(false)
	rf.SetUseNFTSets(false)
	rf.SetMaxTotalNFTRules(1024)
	rf.fqdnData = map[string][]string{"keep.example.com": {"192.0.2.1"}}

	err := rf.ApplyRules(manyAcceptRules(1100), nil, nil, nil, nil, nil, nil)
	if !errors.Is(err, ErrNftRuleBudgetExceeded) {
		t.Fatalf("expected ErrNftRuleBudgetExceeded, got %v", err)
	}
	if n := flushes(); n != 0 {
		t.Fatalf("Flush reached the kernel %d times, want 0", n)
	}
	if got := rf.FQDNResolutions(); len(got) != 1 || got["keep.example.com"] == nil {
		t.Fatalf("FQDN snapshot must be unchanged after a rejected apply, got %v", got)
	}
}

func TestApplyRules_UnderBudgetFlushesOnce(t *testing.T) {
	flushes := swapFirewallConn(t)
	rf := NewRealFirewall(false)
	rf.SetUseNFTSets(false)
	rf.SetMaxTotalNFTRules(1024)

	if err := rf.ApplyRules(manyAcceptRules(10), nil, nil, nil, nil, nil, nil); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
	if n := flushes(); n != 1 {
		t.Fatalf("Flush reached the kernel %d times, want 1", n)
	}
}

// TestApplyRules_BudgetCountsEveryChain pins countingBatch to the real
// number of NFT_MSG_NEWRULE messages ApplyRules emits (not-local/input/
// forward/output structural + user rules + port-forward accept + pigate_nat
// postrouting/prerouting): a budget of exactly that count passes, one less is
// rejected.
func TestApplyRules_BudgetCountsEveryChain(t *testing.T) {
	newRule := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWRULE)
	newRules := 0
	orig := newFirewallConn
	newFirewallConn = func() (*nftables.Conn, error) {
		return nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
			for _, m := range req {
				if m.Header.Type == newRule {
					newRules++
				}
			}
			return req, nil
		}))
	}
	t.Cleanup(func() { newFirewallConn = orig })

	rules := []model.PolicyRule{
		{ID: "in", Name: "in", Chain: model.PolicyChainInput, Status: true, Action: "ACCEPT"},
		{ID: "fw", Name: "fw", Chain: model.PolicyChainForward, Status: true, Action: "ACCEPT", Nat: true},
		{ID: "out", Name: "out", Chain: model.PolicyChainOutput, Status: true, Action: "DROP"},
	}
	pfs := []model.PortForward{{ID: "p", Name: "p", InInterface: "eth0", ExternalPort: "8080", Protocol: "tcp", InternalIP: "192.168.1.10", InternalPort: "80", Status: true}}
	ifaces := []model.NetworkInterface{{Name: "eth0", AddressingMode: "dhcp", AdminAccess: []string{"SSH"}}}

	rf := NewRealFirewall(true)
	if err := rf.ApplyRules(rules, ifaces, nil, nil, []string{"eth1"}, []string{"eth1"}, pfs); err != nil {
		t.Fatalf("ApplyRules: %v", err)
	}
	want := newRules
	if want == 0 {
		t.Fatal("no NEWRULE messages captured")
	}

	rf.SetMaxTotalNFTRules(want)
	if err := rf.ApplyRules(rules, ifaces, nil, nil, []string{"eth1"}, []string{"eth1"}, pfs); err != nil {
		t.Fatalf("budget == rule count must pass, got %v", err)
	}
	rf.SetMaxTotalNFTRules(want - 1)
	if err := rf.ApplyRules(rules, ifaces, nil, nil, []string{"eth1"}, []string{"eth1"}, pfs); !errors.Is(err, ErrNftRuleBudgetExceeded) {
		t.Fatalf("budget == rule count-1 must be rejected, got %v", err)
	}
}
