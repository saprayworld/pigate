package kernel

import (
	"errors"
	"fmt"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"pigate/internal/model"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	vnl "github.com/vishvananda/netlink"
	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
)

// Opt-in integration tests against the REAL kernel nf_tables, for the
// anonymous-set rule generation (docs/ref/todo/nftables-sets-refactor-plan.md
// T-09). They are skipped unless PIGATE_NFT_NETNS_TEST=1 and MUST run inside a
// throwaway network namespace — they create/replace nft tables:
//
//	cd backend && go test -c -o $SCRATCH/kernel.test ./internal/kernel && \
//	  PIGATE_NFT_NETNS_TEST=1 unshare -rn $SCRATCH/kernel.test -test.run TestNetns -test.v
//
// Caution 12: requireNetns refuses to run (t.Fatal) when any interface other
// than lo is visible, i.e. when it is not inside a fresh namespace.
func requireNetns(t *testing.T) {
	t.Helper()
	if os.Getenv("PIGATE_NFT_NETNS_TEST") != "1" {
		t.Skip("set PIGATE_NFT_NETNS_TEST=1 and run under `unshare -rn` to run kernel integration tests")
	}
	ifs, err := net.Interfaces()
	if err != nil {
		t.Fatalf("net.Interfaces: %v", err)
	}
	for _, i := range ifs {
		if i.Name != "lo" {
			t.Fatalf("refusing to run: interface %q is visible, this is not a fresh network namespace (use `unshare -rn`)", i.Name)
		}
	}
}

// teeBatch forwards to a real connection while recording every set added.
type teeBatch struct {
	inner nftBatch
	sets  []recEvent
	rules int
}

func (b *teeBatch) AddRule(r *nftables.Rule) *nftables.Rule {
	b.rules++
	return b.inner.AddRule(r)
}

func (b *teeBatch) AddSet(s *nftables.Set, elems []nftables.SetElement) error {
	if err := b.inner.AddSet(s, elems); err != nil {
		return err
	}
	b.sets = append(b.sets, recEvent{set: s, elems: elems})
	return nil
}

func elemRepr(e nftables.SetElement) string {
	return fmt.Sprintf("%x|%x|%t", e.Key, e.KeyEnd, e.IntervalEnd)
}

func setRepr(keyType string, elems []nftables.SetElement) string {
	rs := make([]string, len(elems))
	for i, e := range elems {
		rs[i] = elemRepr(e)
	}
	sort.Strings(rs)
	return keyType + " {" + strings.Join(rs, ",") + "}"
}

func netnsConn(t *testing.T) *nftables.Conn {
	t.Helper()
	conn, err := newFirewallConn()
	if err != nil {
		t.Fatalf("open nftables conn: %v", err)
	}
	return conn
}

// newITTable creates (or recreates) an inet test table with one output-hook
// base chain, policy accept, in conn's batch.
func newITTable(conn *nftables.Conn, name string) (*nftables.Table, *nftables.Chain) {
	table := conn.AddTable(&nftables.Table{Name: name, Family: nftables.TableFamilyINet})
	conn.FlushTable(table)
	policyAccept := nftables.ChainPolicyAccept
	chain := conn.AddChain(&nftables.Chain{
		Name: "output", Table: table, Type: nftables.ChainTypeFilter,
		Hooknum: nftables.ChainHookOutput, Priority: nftables.ChainPriorityFilter, Policy: &policyAccept,
	})
	return table, chain
}

func itObjects() (map[string]model.AddressObject, map[string]model.ServiceObject) {
	ae := func(t, v string) model.AddressEntry { return model.AddressEntry{Type: t, Value: v} }
	se := func(p, port string) model.ServiceEntry { return model.ServiceEntry{Protocol: p, Port: port} }
	a := func(n string, e ...model.AddressEntry) model.AddressObject {
		return model.AddressObject{ID: n, Name: n, Type: e[0].Type, Value: e[0].Value, Entries: e}
	}
	v := func(n string, e ...model.ServiceEntry) model.ServiceObject {
		return model.ServiceObject{ID: n, Name: n, Protocol: e[0].Protocol, Port: e[0].Port, Entries: e}
	}
	addrs := map[string]model.AddressObject{}
	for _, o := range []model.AddressObject{
		a("NETS", ae("subnet", "10.0.0.0/8"), ae("subnet", "10.1.0.0/16")), // nested
		a("RANGEADJ", ae("range", "172.16.0.1-172.16.0.9"), ae("range", "172.16.0.10-172.16.0.20")),
		a("HOSTS", ae("subnet", "10.1.0.1/32"), ae("subnet", "10.2.0.5/32"), ae("subnet", "192.168.50.7/32")),
		a("FQ3", ae("fqdn", "three.example.com")),
		a("TOP", ae("range", "240.0.0.0-255.255.255.255")),
		a("ZEROSTART", ae("range", "0.0.0.0-10.1.0.1")),
		a("HOST", ae("subnet", "10.1.0.1/32")),
		a("SUBNETS2", ae("subnet", "203.0.113.0/26"), ae("subnet", "11.0.0.0/8")),
	} {
		addrs[o.Name] = o
	}
	svcs := map[string]model.ServiceObject{}
	for _, o := range []model.ServiceObject{
		v("MIXED", se("ICMP", ""), se("TCP", "80"), se("UDP", "53")),
		v("TCPANY80", se("TCP", ""), se("TCP", "80")),
		v("UDPANYTCP80", se("UDP", ""), se("TCP", "80")),
		v("PORTS", se("TCP", "80-90"), se("TCP", "85-100"), se("TCP", "443")),
		v("PROTOS", se("TCP", ""), se("UDP", "")),
		v("TCP22", se("TCP", "22")),
		v("TCPUDP53", se("TCP/UDP", "53")),
		v("WEB", se("TCP", "80"), se("TCP", "443"), se("UDP", "53")),
	} {
		svcs[o.Name] = o
	}
	return addrs, svcs
}

type itPolicy struct {
	id            string
	src, dst, svc []string
}

func itPolicies() []itPolicy {
	return []itPolicy{
		{"p-nested-subnets", []string{"NETS"}, nil, nil},
		{"p-adjacent-ranges", nil, []string{"RANGEADJ"}, nil},
		{"p-hosts", []string{"HOSTS"}, []string{"HOSTS"}, nil},
		{"p-fqdn-multi-ip", nil, []string{"FQ3"}, nil},
		{"p-mixed-svc", nil, nil, []string{"MIXED"}},
		{"p-tcp-any-and-80", nil, nil, []string{"TCPANY80"}},
		{"p-udp-any-tcp80", nil, nil, []string{"UDPANYTCP80"}},
		{"p-port-ranges", nil, nil, []string{"PORTS"}},
		{"p-proto-set", nil, nil, []string{"PROTOS"}},
		{"p-top-of-space", nil, []string{"TOP"}, nil},
		{"p-zero-start", []string{"ZEROSTART"}, nil, nil},
		{"p-singleton", []string{"HOST"}, []string{"HOST"}, []string{"TCP22"}},
		{"p-tcpudp53", nil, nil, []string{"TCPUDP53"}},
		{"p-full", []string{"NETS"}, []string{"HOSTS", "SUBNETS2"}, []string{"WEB"}},
	}
}

func (p itPolicy) rule(chain string) model.PolicyRule {
	return model.PolicyRule{ID: p.id, Name: p.id, Chain: chain, Status: true, Action: "ACCEPT",
		Source: p.src, Destination: p.dst, Service: p.svc}
}

func itRules(chain string) []model.PolicyRule {
	var out []model.PolicyRule
	for _, p := range itPolicies() {
		out = append(out, p.rule(chain))
	}
	return out
}

func TestNetns_SetsAcceptedByKernel(t *testing.T) {
	requireNetns(t)
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := itObjects()

	conn := netnsConn(t)
	table, chain := newITTable(conn, "pigate_it")
	tee := &teeBatch{inner: conn}
	acc, drop := goldenPrefixes(model.PolicyChainOutput)
	emitted := addUserChainRulesSets(tee, table, chain, model.PolicyChainOutput, itRules(model.PolicyChainOutput), addrs, svcs, acc, drop, 4096, newFQDNRecorder())
	if err := conn.Flush(); err != nil {
		t.Fatalf("Flush (kernel rejected the set-mode batch): %v", err)
	}
	t.Logf("emitted %d nft rules, %d sets", emitted, len(tee.sets))

	rules, err := conn.GetRules(table, chain)
	if err != nil {
		t.Fatalf("GetRules: %v", err)
	}
	if len(rules) != emitted || tee.rules != emitted {
		t.Fatalf("kernel holds %d rules, builder emitted %d (tee %d)", len(rules), emitted, tee.rules)
	}
	sets, err := conn.GetSets(table)
	if err != nil {
		t.Fatalf("GetSets: %v", err)
	}
	if len(sets) != len(tee.sets) {
		t.Fatalf("kernel holds %d sets, AddSet was called %d times", len(sets), len(tee.sets))
	}

	// Compare the kernel's view of every set with what we sent, as a multiset.
	want := map[string]int{}
	for _, s := range tee.sets {
		want[setRepr(s.set.KeyType.Name, s.elems)]++
	}
	got := map[string]int{}
	for _, s := range sets {
		elems, err := conn.GetSetElements(s)
		if err != nil {
			t.Fatalf("GetSetElements(%s): %v", s.Name, err)
		}
		r := setRepr(s.KeyType.Name, elems)
		t.Logf("kernel set %s anon=%t const=%t interval=%t concat=%t: %s", s.Name, s.Anonymous, s.Constant, s.Interval, s.Concatenation, r)
		got[r]++
	}
	for k, n := range want {
		if got[k] != n {
			t.Errorf("set missing/different in kernel (x%d sent, x%d returned):\n  sent %s", n, got[k], k)
		}
	}
	for k, n := range got {
		if want[k] != n {
			t.Logf("kernel returned set not matching any sent set verbatim (x%d): %s", n, k)
		}
	}
}

func TestNetns_NoAnonymousSetLeakAcrossApplies(t *testing.T) {
	requireNetns(t)
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := itObjects()
	acc, drop := goldenPrefixes(model.PolicyChainOutput)

	var perApply []int
	for i := 0; i < 3; i++ {
		conn := netnsConn(t)
		table, chain := newITTable(conn, "pigate_it")
		tee := &teeBatch{inner: conn}
		addUserChainRulesSets(tee, table, chain, model.PolicyChainOutput, itRules(model.PolicyChainOutput), addrs, svcs, acc, drop, 4096, newFQDNRecorder())
		if err := conn.Flush(); err != nil {
			t.Fatalf("apply %d Flush: %v", i+1, err)
		}
		sets, err := conn.GetSets(table)
		if err != nil {
			t.Fatalf("apply %d GetSets: %v", i+1, err)
		}
		if len(sets) != len(tee.sets) {
			t.Fatalf("apply %d: kernel holds %d sets, this apply created %d (leak from earlier applies?)", i+1, len(sets), len(tee.sets))
		}
		perApply = append(perApply, len(sets))
	}
	t.Logf("set count after each apply: %v", perApply)
	if perApply[0] != perApply[1] || perApply[1] != perApply[2] {
		t.Fatalf("set count not stable across applies: %v", perApply)
	}
}

// --- behavioral equivalence: legacy cartesian vs set mode on real packets ---

type vec struct {
	src, dst string
	proto    string // tcp | udp | icmp
	port     int
}

func addLoAddrs(t *testing.T, ips []string) map[string]bool {
	t.Helper()
	lo, err := vnl.LinkByName("lo")
	if err != nil {
		t.Fatalf("LinkByName(lo): %v", err)
	}
	if err := vnl.LinkSetUp(lo); err != nil {
		t.Fatalf("LinkSetUp(lo): %v", err)
	}
	have := map[string]bool{}
	for _, ip := range ips {
		a, err := vnl.ParseAddr(ip + "/32")
		if err != nil {
			t.Fatalf("ParseAddr(%s): %v", ip, err)
		}
		if err := vnl.AddrAdd(lo, a); err != nil {
			t.Logf("cannot add %s to lo (%v); vectors using it are skipped", ip, err)
			continue
		}
		have[ip] = true
	}
	return have
}

// sendVec sends exactly one packet for v from v.src to v.dst (both local
// addresses on lo). Errors after the packet left (RST/refused, ICMP unreach)
// are expected and ignored; only failures to send at all are reported.
func sendVec(v vec) error {
	switch v.proto {
	case "tcp":
		d := net.Dialer{LocalAddr: &net.TCPAddr{IP: net.ParseIP(v.src)}, Timeout: 500 * time.Millisecond}
		c, err := d.Dial("tcp4", net.JoinHostPort(v.dst, strconv.Itoa(v.port)))
		if err == nil {
			c.Close()
			return nil
		}
		if errors.Is(err, syscall.ECONNREFUSED) {
			return nil
		}
		return err
	case "udp":
		c, err := net.DialUDP("udp4", &net.UDPAddr{IP: net.ParseIP(v.src)}, &net.UDPAddr{IP: net.ParseIP(v.dst), Port: v.port})
		if err != nil {
			return err
		}
		defer c.Close()
		_, err = c.Write([]byte("x"))
		return err
	case "icmp":
		pc, err := icmp.ListenPacket("ip4:icmp", v.src)
		if err != nil {
			return err
		}
		defer pc.Close()
		msg := icmp.Message{Type: ipv4.ICMPTypeEcho, Code: 0, Body: &icmp.Echo{ID: 1, Seq: 1, Data: []byte("x")}}
		b, err := msg.Marshal(nil)
		if err != nil {
			return err
		}
		_, err = pc.WriteTo(b, &net.IPAddr{IP: net.ParseIP(v.dst)})
		return err
	}
	return fmt.Errorf("bad proto %q", v.proto)
}

func policyCounters(t *testing.T, conn *nftables.Conn, table *nftables.Table, chain *nftables.Chain) map[string]uint64 {
	t.Helper()
	rules, err := conn.GetRules(table, chain)
	if err != nil {
		t.Fatalf("GetRules(%s): %v", table.Name, err)
	}
	acc := map[string]model.RuleCounter{}
	accumulateRuleCounters(rules, acc)
	out := map[string]uint64{}
	for id, c := range acc {
		out[id] = c.Packets
	}
	return out
}

func TestNetns_LegacyAndSetModeMatchSamePackets(t *testing.T) {
	requireNetns(t)
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := itObjects()

	ips := []string{"10.0.0.9", "10.1.0.1", "10.1.5.5", "10.2.0.5", "11.0.0.1", "172.16.0.5", "172.16.0.10", "172.16.0.21",
		"192.168.50.7", "203.0.113.10", "203.0.113.20", "203.0.113.30", "203.0.113.40", "240.0.0.5"}
	have := addLoAddrs(t, ips)
	if len(have) < 6 {
		t.Fatalf("could only add %d addresses to lo; behavioral test is meaningless", len(have))
	}

	var addrVecs []string
	for _, ip := range ips {
		if have[ip] {
			addrVecs = append(addrVecs, ip)
		}
	}
	// Sources are a subset (every source address appears as a destination
	// too) to keep the run time reasonable: each netlink round trip costs
	// ~20 ms on WSL2.
	var srcVecs []string
	for _, ip := range []string{"10.0.0.9", "10.1.0.1", "10.2.0.5", "11.0.0.1", "192.168.50.7"} {
		if have[ip] {
			srcVecs = append(srcVecs, ip)
		}
	}
	type svcVec struct {
		proto string
		port  int
	}
	svcVecs := []svcVec{{"tcp", 22}, {"tcp", 80}, {"tcp", 85}, {"tcp", 100}, {"tcp", 101}, {"tcp", 443}, {"udp", 53}, {"udp", 5000}, {"tcp", 53}, {"icmp", 0}}

	// Probe that we can send at all (ICMP raw socket may be unavailable).
	icmpOK := sendVec(vec{"10.1.0.1", "10.2.0.5", "icmp", 0}) == nil
	if !icmpOK {
		t.Logf("raw ICMP socket unavailable; ICMP vectors skipped")
	}

	acc, drop := goldenPrefixes(model.PolicyChainOutput)
	conn := netnsConn(t)
	totalVectors, nonZeroPolicies := 0, map[string]bool{}
	zeroSeen := false

	for _, p := range itPolicies() {
		fmt.Fprintf(os.Stderr, "behavioral: policy %s (%d vectors so far)\n", p.id, totalVectors)
		rule := []model.PolicyRule{p.rule(model.PolicyChainOutput)}

		lt, lc := newITTable(conn, "pigate_legacy")
		addUserChainRules(conn, lt, lc, model.PolicyChainOutput, rule, addrs, svcs, acc, drop, 4096, newFQDNRecorder())
		st, sc := newITTable(conn, "pigate_sets")
		addUserChainRulesSets(conn, st, sc, model.PolicyChainOutput, rule, addrs, svcs, acc, drop, 4096, newFQDNRecorder())
		if err := conn.Flush(); err != nil {
			t.Fatalf("%s: Flush: %v", p.id, err)
		}

		// Two connections so both counter dumps can run concurrently.
		legacyConn, setsConn := netnsConn(t), netnsConn(t)
		dump := func() (l, s uint64) {
			done := make(chan uint64, 1)
			go func() { done <- policyCounters(t, legacyConn, lt, lc)[p.id] }()
			s = policyCounters(t, setsConn, st, sc)[p.id]
			return <-done, s
		}
		lb, sb := dump()
		for _, s := range srcVecs {
			for _, d := range addrVecs {
				for _, sv := range svcVecs {
					if sv.proto == "icmp" && !icmpOK {
						continue
					}
					v := vec{s, d, sv.proto, sv.port}
					if err := sendVec(v); err != nil {
						t.Fatalf("%s: cannot send %+v: %v", p.id, v, err)
					}
					la, sa := dump()
					totalVectors++
					if la-lb != sa-sb {
						t.Errorf("%s: vector %+v legacy matched %d packet(s), set mode %d", p.id, v, la-lb, sa-sb)
					}
					if la-lb > 0 {
						nonZeroPolicies[p.id] = true
					} else {
						zeroSeen = true
					}
					lb, sb = la, sa
				}
			}
		}
	}
	t.Logf("compared %d vectors across %d policies; policies that matched at least one vector: %d", totalVectors, len(itPolicies()), len(nonZeroPolicies))
	for _, p := range itPolicies() {
		if !nonZeroPolicies[p.id] {
			// p-top-of-space needs 240.0.0.5 on lo; p-fqdn-multi-ip needs the .10/.20 addresses.
			t.Logf("note: policy %s matched no vector in this environment", p.id)
		}
	}
	if !zeroSeen || len(nonZeroPolicies) < len(itPolicies())/2 {
		t.Fatalf("test has no teeth: zeroSeen=%t matchedPolicies=%d", zeroSeen, len(nonZeroPolicies))
	}
}

// --- full ApplyRules smoke (log, nat) ---

func TestNetns_ApplyRulesSmoke(t *testing.T) {
	requireNetns(t)
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := itObjects()
	var addrList []model.AddressObject
	for _, a := range addrs {
		addrList = append(addrList, a)
	}
	var svcList []model.ServiceObject
	for _, s := range svcs {
		svcList = append(svcList, s)
	}

	rules := []model.PolicyRule{
		{ID: "in1", Name: "in1", Chain: model.PolicyChainInput, Status: true, Action: "ACCEPT", Log: true,
			InInterfaces: []string{"eth0", "eth1"}, Source: []string{"NETS"}, Service: []string{"MIXED"}},
		{ID: "fw1", Name: "fw1", Chain: model.PolicyChainForward, Status: true, Action: "ACCEPT", Log: true, Nat: true,
			InInterfaces: []string{"eth0", "eth1"}, OutInterfaces: []string{"eth2", "eth3"},
			Source: []string{"HOSTS"}, Destination: []string{"SUBNETS2"}, Service: []string{"WEB"}},
		{ID: "out1", Name: "out1", Chain: model.PolicyChainOutput, Status: true, Action: "DROP", Log: true,
			Destination: []string{"RANGEADJ"}, Service: []string{"PORTS"}},
	}
	ifaces := []model.NetworkInterface{{Name: "eth0", AddressingMode: "dhcp", AdminAccess: []string{"SSH", "PING"}}}
	pfs := []model.PortForward{{ID: "pf", Name: "pf", InInterface: "eth0", ExternalPort: "8080", Protocol: "tcp", InternalIP: "192.168.1.10", InternalPort: "80", Status: true}}

	rf := NewRealFirewall(false)
	skipOrFail := func(err error) {
		if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.EOPNOTSUPP) {
			t.Skipf("kernel lacks a netfilter module needed by the full ruleset (e.g. nft_fib, missing on WSL2 5.15): %v", err)
		}
		t.Fatalf("ApplyRules: %v", err)
	}
	if err := rf.ApplyRules(rules, ifaces, addrList, svcList, []string{"eth0"}, []string{"eth0"}, pfs); err != nil {
		skipOrFail(err)
	}

	conn := netnsConn(t)
	table := &nftables.Table{Name: "pigate", Family: nftables.TableFamilyINet}
	count := func() int {
		n := 0
		for _, c := range []string{"input", "forward", "output", "pigate-not-local"} {
			rs, err := conn.GetRules(table, &nftables.Chain{Name: c, Table: table})
			if err != nil {
				t.Fatalf("GetRules(%s): %v", c, err)
			}
			n += len(rs)
		}
		return n
	}
	t.Logf("pigate table holds %d rules after set-mode ApplyRules", count())

	// Counters by rule id are readable back from a set-mode ruleset.
	for _, c := range []string{"input", "forward", "output"} {
		rs, err := conn.GetRules(table, &nftables.Chain{Name: c, Table: table})
		if err != nil {
			t.Fatal(err)
		}
		acc := map[string]model.RuleCounter{}
		accumulateRuleCounters(rs, acc)
		want := map[string]string{"input": "in1", "forward": "fw1", "output": "out1"}[c]
		if _, ok := acc[want]; !ok {
			t.Errorf("chain %s: no counter attributed to rule id %q (got %v)", c, want, acc)
		}
	}

	// The socket-buffer option ran on every dial above; under `unshare -rn`
	// SO_RCVBUFFORCE is EPERM so this exercised the plain SetReadBuffer fallback.
}

// TestNetns_BudgetRejectKeepsExistingRuleset needs only plain nft features
// (the full ApplyRules ruleset also needs nft_fib, absent on some kernels such
// as WSL2): a rejected apply must leave a previously applied "pigate" table
// untouched.
func TestNetns_BudgetRejectKeepsExistingRuleset(t *testing.T) {
	requireNetns(t)

	conn := netnsConn(t)
	table, chain := newITTable(conn, "pigate")
	conn.AddRule(&nftables.Rule{Table: table, Chain: chain, Exprs: []expr.Any{&expr.Counter{}, &expr.Verdict{Kind: expr.VerdictAccept}}})
	if err := conn.Flush(); err != nil {
		t.Fatalf("seed table: %v", err)
	}
	before, err := conn.GetRules(table, chain)
	if err != nil || len(before) != 1 {
		t.Fatalf("seed rules: %d err=%v", len(before), err)
	}

	rf := NewRealFirewall(false)
	rf.SetUseNFTSets(false)
	rf.SetMaxTotalNFTRules(1024)
	if err := rf.ApplyRules(manyAcceptRules(1100), nil, nil, nil, nil, nil, nil); !errors.Is(err, ErrNftRuleBudgetExceeded) {
		t.Fatalf("want ErrNftRuleBudgetExceeded, got %v", err)
	}
	after, err := conn.GetRules(table, chain)
	if err != nil || len(after) != 1 {
		t.Fatalf("previous ruleset changed by a rejected apply: %d rules err=%v", len(after), err)
	}
}

// TestNetns_SetModeLogNatCounterRoundTrip checks, on the real kernel, that
// set-mode rules carrying log + the forward fwmark still accept, and that the
// per-rule counter / log prefix tagging read back by rule id.
func TestNetns_SetModeLogNatCounterRoundTrip(t *testing.T) {
	requireNetns(t)
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := itObjects()

	conn := netnsConn(t)
	table, chain := newITTable(conn, "pigate_it")
	rules := []model.PolicyRule{
		{ID: "fw-log-nat", Name: "fw", Chain: model.PolicyChainForward, Status: true, Action: "ACCEPT", Log: true, Nat: true,
			InInterfaces: []string{"eth0", "eth1"}, OutInterfaces: []string{"eth2", "eth3"},
			Source: []string{"NETS"}, Destination: []string{"HOSTS"}, Service: []string{"MIXED"}},
	}
	acc, drop := goldenPrefixes(model.PolicyChainForward)
	n := addUserChainRulesSets(conn, table, chain, model.PolicyChainForward, rules, addrs, svcs, acc, drop, 4096, nil)
	if err := conn.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	got, err := conn.GetRules(table, chain)
	if err != nil || len(got) != n || n != 2 {
		t.Fatalf("want 2 rules (ICMP + port variants), got %d (emitted %d) err=%v", len(got), n, err)
	}
	counters := map[string]model.RuleCounter{}
	accumulateRuleCounters(got, counters)
	if _, ok := counters["fw-log-nat"]; !ok {
		t.Fatalf("counter not attributed to rule id, got %v", counters)
	}
	for _, r := range got {
		found := false
		for _, e := range r.Exprs {
			if l, ok := e.(*expr.Log); ok {
				found = true
				if !strings.Contains(string(l.Data), "r=fw-log-nat ") {
					t.Errorf("log prefix %q lacks r=fw-log-nat", l.Data)
				}
			}
		}
		if !found {
			t.Errorf("log expression missing in kernel-returned rule")
		}
	}
}
