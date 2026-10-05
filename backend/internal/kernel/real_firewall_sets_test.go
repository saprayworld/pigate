package kernel

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"net"
	"reflect"
	"strings"
	"testing"

	"pigate/internal/model"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// NOTE: tests that call AddSet (directly or via a recBatch / real test conn)
// must not use t.Parallel(): google/nftables allocates anonymous set IDs from
// an unsynchronized package-level counter (plan Caution 11).

// --- pure helpers (T-04) ---

func ip4(s string) uint32 {
	v := net.ParseIP(s).To4()
	return uint32(v[0])<<24 | uint32(v[1])<<16 | uint32(v[2])<<8 | uint32(v[3])
}

func TestMergeIntervals(t *testing.T) {
	cases := []struct {
		name string
		in   []interval
		want []interval
	}{
		{"empty", nil, nil},
		{"single", []interval{{5, 9}}, []interval{{5, 9}}},
		{"overlap", []interval{{1, 10}, {5, 20}}, []interval{{1, 20}}},
		{"containment", []interval{{1, 100}, {5, 20}}, []interval{{1, 100}}},
		{"adjacent", []interval{{1, 9}, {10, 20}}, []interval{{1, 20}}},
		{"gap of one stays split", []interval{{1, 9}, {11, 20}}, []interval{{1, 9}, {11, 20}}},
		{"duplicates", []interval{{3, 3}, {3, 3}, {3, 3}}, []interval{{3, 3}}},
		{"unsorted", []interval{{50, 60}, {1, 2}, {10, 20}}, []interval{{1, 2}, {10, 20}, {50, 60}}},
		{"full space", []interval{{0, 0xFFFFFFFF}, {5, 6}}, []interval{{0, 0xFFFFFFFF}}},
		{"ends at max", []interval{{0xFFFFFFF0, 0xFFFFFFFF}, {0xFFFFFF00, 0xFFFFFFEF}}, []interval{{0xFFFFFF00, 0xFFFFFFFF}}},
		{"port 65535 adjacent", []interval{{65000, 65534}, {65535, 65535}}, []interval{{65000, 65535}}},
		{"empty interval dropped", []interval{{9, 1}, {2, 3}}, []interval{{2, 3}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := mergeIntervals(c.in)
			if !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
}

func TestEncodeIPv4IntervalElems(t *testing.T) {
	type el struct {
		key uint32
		end bool
	}
	flat := func(es []nftables.SetElement) []el {
		out := make([]el, len(es))
		for i, e := range es {
			if len(e.Key) != 4 {
				t.Fatalf("key len %d", len(e.Key))
			}
			out[i] = el{uint32(e.Key[0])<<24 | uint32(e.Key[1])<<16 | uint32(e.Key[2])<<8 | uint32(e.Key[3]), e.IntervalEnd}
		}
		return out
	}
	cases := []struct {
		name string
		in   []interval
		want []el
	}{
		{"10.0.0.0/8", []interval{{ip4("10.0.0.0"), ip4("10.255.255.255")}},
			[]el{{0, true}, {ip4("10.0.0.0"), false}, {ip4("11.0.0.0"), true}}},
		{"range from 0.0.0.0", []interval{{0, 5}}, []el{{0, false}, {6, true}}},
		{"0.0.0.0/0", []interval{{0, 0xFFFFFFFF}}, []el{{0, false}}},
		{"/32", []interval{{ip4("10.0.0.7"), ip4("10.0.0.7")}},
			[]el{{0, true}, {ip4("10.0.0.7"), false}, {ip4("10.0.0.8"), true}}},
		{"ends at 255.255.255.255", []interval{{ip4("192.168.0.0"), 0xFFFFFFFF}},
			[]el{{0, true}, {ip4("192.168.0.0"), false}}},
		{"two intervals", []interval{{10, 19}, {30, 39}},
			[]el{{0, true}, {10, false}, {20, true}, {30, false}, {40, true}}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := flat(encodeIPv4IntervalElems(c.in)); !reflect.DeepEqual(got, c.want) {
				t.Fatalf("got %v want %v", got, c.want)
			}
		})
	}
	if encodeIPv4IntervalElems(nil) != nil {
		t.Fatal("empty input must encode to no elements")
	}
}

func TestEncodeProtoPortElems(t *testing.T) {
	got := encodeProtoPortElems([]protoPorts{{proto: 6, ports: []interval{{80, 90}}}, {proto: 17, ports: []interval{{53, 53}}}})
	if len(got) != 2 {
		t.Fatalf("got %d elems", len(got))
	}
	if hex.EncodeToString(got[0].Key) != "0600000000500000" || hex.EncodeToString(got[0].KeyEnd) != "06000000005a0000" {
		t.Fatalf("TCP 80-90: key %x keyend %x", got[0].Key, got[0].KeyEnd)
	}
	if hex.EncodeToString(got[1].Key) != "1100000000350000" || hex.EncodeToString(got[1].KeyEnd) != "1100000000350000" {
		t.Fatalf("UDP 53: key %x keyend %x (KeyEnd must be sent even when start==end)", got[1].Key, got[1].KeyEnd)
	}
	if got[0].IntervalEnd || got[1].IntervalEnd {
		t.Fatal("concat elements must not use IntervalEnd")
	}
}

func TestEncodeIfnameAndProtoElems(t *testing.T) {
	e := encodeIfnameElems([]string{"eth0", "wlan0", "eth0"})
	if len(e) != 2 || !bytes.Equal(e[0].Key, padInterfaceName("eth0")) || !bytes.Equal(e[1].Key, padInterfaceName("wlan0")) || len(e[0].Key) != 16 {
		t.Fatalf("ifname elems: %+v", e)
	}
	p := encodeProtoElems([]byte{17, 6, 17, 1})
	if len(p) != 3 || p[0].Key[0] != 1 || p[1].Key[0] != 6 || p[2].Key[0] != 17 {
		t.Fatalf("proto elems: %+v", p)
	}
}

func TestSvcComboAtom(t *testing.T) {
	tc := func(proto, port string) svcCombo {
		return svcCombo{hasFilter: true, objName: "X", protocol: proto, port: port}
	}
	cases := []struct {
		name     string
		in       svcCombo
		want     svcAtom
		nonEmpty bool
		wantErr  bool
	}{
		{"empty port", tc("TCP", ""), svcAtom{proto: 6, anyPort: true}, true, false},
		{"dash", tc("UDP", "-"), svcAtom{proto: 17, anyPort: true}, true, false},
		{"1-65535", tc("TCP", "1-65535"), svcAtom{proto: 6, anyPort: true}, true, false},
		{"padded 1-65535", tc("TCP", " 1-65535 "), svcAtom{proto: 6, anyPort: true}, true, false},
		{"spaced 1 - 65535 is a range", tc("TCP", "1 - 65535"), svcAtom{proto: 6, start: 1, end: 65535}, true, false},
		{"padded single", tc("TCP", " 80 "), svcAtom{proto: 6, start: 80, end: 80}, true, false},
		{"range", tc("UDP", "8000-8010"), svcAtom{proto: 17, start: 8000, end: 8010}, true, false},
		{"three parts is proto only", tc("TCP", "80-90-100"), svcAtom{proto: 6, anyPort: true}, true, false},
		{"icmp ignores port", tc("ICMP", "80"), svcAtom{proto: 1, anyPort: true}, true, false},
		{"icmp", tc("ICMP", ""), svcAtom{proto: 1, anyPort: true}, true, false},
		{"65535", tc("TCP", "65535"), svcAtom{proto: 6, start: 65535, end: 65535}, true, false},
		{"port 0", tc("TCP", "0"), svcAtom{proto: 6, start: 0, end: 0}, true, false},
		{"reversed", tc("TCP", "90-80"), svcAtom{}, false, false},
		{"not a number", tc("TCP", "abc"), svcAtom{}, false, true},
		{"bad range end", tc("TCP", "80-xyz"), svcAtom{}, false, true},
		{"out of range", tc("TCP", "70000"), svcAtom{}, false, true},
		{"range out of range", tc("TCP", "80-70000"), svcAtom{}, false, true},
		{"unsupported proto", tc("SCTP", "80"), svcAtom{}, false, true},
		{"ALL has no atom", svcCombo{}, svcAtom{}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, nonEmpty, err := svcComboAtom(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if nonEmpty != c.nonEmpty || (nonEmpty && got != c.want) {
				t.Fatalf("got %+v nonEmpty=%v want %+v nonEmpty=%v", got, nonEmpty, c.want, c.nonEmpty)
			}
		})
	}
}

func TestAddrComboInterval(t *testing.T) {
	sub := func(v string) addrCombo {
		return addrCombo{hasFilter: true, objName: "O", entry: model.AddressEntry{Type: "subnet", Value: v}}
	}
	rng := func(v string) addrCombo {
		return addrCombo{hasFilter: true, objName: "O", entry: model.AddressEntry{Type: "range", Value: v}}
	}
	cases := []struct {
		name     string
		in       addrCombo
		want     interval
		nonEmpty bool
		wantErr  bool
	}{
		{"/8", sub("10.0.0.0/8"), interval{ip4("10.0.0.0"), ip4("10.255.255.255")}, true, false},
		{"host bits masked", sub("10.1.2.3/24"), interval{ip4("10.1.2.0"), ip4("10.1.2.255")}, true, false},
		{"missing slash is /32", sub("10.2.0.5"), interval{ip4("10.2.0.5"), ip4("10.2.0.5")}, true, false},
		{"explicit /32", sub("10.1.0.1/32"), interval{ip4("10.1.0.1"), ip4("10.1.0.1")}, true, false},
		{"/0", sub("0.0.0.0/0"), interval{0, 0xFFFFFFFF}, true, false},
		{"padded", sub("  10.0.0.0/8 "), interval{ip4("10.0.0.0"), ip4("10.255.255.255")}, true, false},
		{"ipv6 rejected", sub("2001:db8::/32"), interval{}, false, true},
		{"garbage", sub("nope"), interval{}, false, true},
		{"range", rng("10.0.0.1-10.0.0.9"), interval{ip4("10.0.0.1"), ip4("10.0.0.9")}, true, false},
		{"range spaces", rng("10.0.0.1 - 10.0.0.9"), interval{ip4("10.0.0.1"), ip4("10.0.0.9")}, true, false},
		{"range to top", rng("192.168.0.0-255.255.255.255"), interval{ip4("192.168.0.0"), 0xFFFFFFFF}, true, false},
		{"range reversed is empty", rng("10.0.0.9-10.0.0.1"), interval{}, false, false},
		{"range one part", rng("10.0.0.1"), interval{}, false, true},
		{"range v6", rng("::1-::2"), interval{}, false, true},
		{"fqdn resolved", addrCombo{hasFilter: true, objName: "O", entry: model.AddressEntry{Type: "fqdn", Value: "x"}, resolvedFQDNIP: net.ParseIP("203.0.113.10")},
			interval{ip4("203.0.113.10"), ip4("203.0.113.10")}, true, false},
		{"unknown type", addrCombo{hasFilter: true, objName: "O", entry: model.AddressEntry{Type: "mac", Value: "x"}}, interval{}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, nonEmpty, err := addrComboInterval(c.in)
			if (err != nil) != c.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, c.wantErr)
			}
			if err != nil {
				return
			}
			if nonEmpty != c.nonEmpty || (nonEmpty && got != c.want) {
				t.Fatalf("got %v nonEmpty=%v want %v nonEmpty=%v", got, nonEmpty, c.want, c.nonEmpty)
			}
		})
	}
}

func TestNewAnonSets_AlwaysAnonymousConstant(t *testing.T) {
	tbl := &nftables.Table{Name: "t", Family: nftables.TableFamilyINet}
	for name, s := range map[string]*nftables.Set{
		"ipv4": newAnonIPv4Set(tbl), "protoport": newAnonProtoPortSet(tbl),
		"proto": newAnonProtoSet(tbl), "ifname": newAnonIfnameSet(tbl),
	} {
		if !s.Anonymous || !s.Constant {
			t.Errorf("%s: must be Anonymous+Constant", name)
		}
	}
	if !newAnonIPv4Set(tbl).Interval || newAnonIPv4Set(tbl).Concatenation {
		t.Error("ipv4: Interval only")
	}
	pp := newAnonProtoPortSet(tbl)
	if !pp.Interval || !pp.Concatenation || pp.KeyType.Bytes != 8 {
		t.Errorf("protoport: Interval+Concatenation, 8-byte key, got %+v", pp)
	}
	if newAnonProtoSet(tbl).Interval || newAnonIfnameSet(tbl).Interval {
		t.Error("proto/ifname sets must not be interval sets")
	}
}

// --- recording batch (T-07) ---

type recEvent struct {
	set   *nftables.Set
	elems []nftables.SetElement
	rule  *nftables.Rule
}

// recBatch is an nftBatch that records AddSet/AddRule in order and assigns
// set IDs/names the way nftables.Conn.AddSet does.
type recBatch struct {
	events []recEvent
	nextID uint32
}

func (r *recBatch) AddRule(rule *nftables.Rule) *nftables.Rule {
	r.events = append(r.events, recEvent{rule: rule})
	return rule
}

func (r *recBatch) AddSet(s *nftables.Set, elems []nftables.SetElement) error {
	if s.Anonymous && !s.Constant {
		return fmt.Errorf("anonymous sets must be constant")
	}
	r.nextID++
	s.ID = r.nextID
	if s.Anonymous {
		s.Name = "__set%d"
	}
	r.events = append(r.events, recEvent{set: s, elems: elems})
	return nil
}

func (r *recBatch) rules() []*nftables.Rule {
	var out []*nftables.Rule
	for _, e := range r.events {
		if e.rule != nil {
			out = append(out, e.rule)
		}
	}
	return out
}

func (r *recBatch) sets() []recEvent {
	var out []recEvent
	for _, e := range r.events {
		if e.set != nil {
			out = append(out, e)
		}
	}
	return out
}

func lookupsOf(rule *nftables.Rule) []*expr.Lookup {
	var out []*expr.Lookup
	for _, e := range rule.Exprs {
		if l, ok := e.(*expr.Lookup); ok {
			out = append(out, l)
		}
	}
	return out
}

func cmpsOf(rule *nftables.Rule) []*expr.Cmp {
	var out []*expr.Cmp
	for _, e := range rule.Exprs {
		if c, ok := e.(*expr.Cmp); ok {
			out = append(out, c)
		}
	}
	return out
}

func metaKeysOf(rule *nftables.Rule) []expr.MetaKey {
	var out []expr.MetaKey
	for _, e := range rule.Exprs {
		if m, ok := e.(*expr.Meta); ok {
			out = append(out, m.Key)
		}
	}
	return out
}

func setTypeNames(sets []recEvent) []string {
	out := make([]string, len(sets))
	for i, s := range sets {
		out[i] = s.set.KeyType.Name
	}
	return out
}

// runSets builds policies in set mode on a recBatch.
func runSets(t *testing.T, chain string, rules []model.PolicyRule, addrs map[string]model.AddressObject,
	svcs map[string]model.ServiceObject, maxAtoms int, rec *fqdnRecorder) *recBatch {
	t.Helper()
	rb := &recBatch{}
	tbl := &nftables.Table{Name: "pigate", Family: nftables.TableFamilyINet}
	ch := &nftables.Chain{Name: chain, Table: tbl}
	acc, drop := goldenPrefixes(chain)
	addUserChainRulesSets(rb, tbl, ch, chain, rules, addrs, svcs, acc, drop, maxAtoms, rec)
	return rb
}

func runLegacy(t *testing.T, chain string, rules []model.PolicyRule, addrs map[string]model.AddressObject,
	svcs map[string]model.ServiceObject, rec *fqdnRecorder) *recBatch {
	t.Helper()
	rb := &recBatch{}
	tbl := &nftables.Table{Name: "pigate", Family: nftables.TableFamilyINet}
	ch := &nftables.Chain{Name: chain, Table: tbl}
	acc, drop := goldenPrefixes(chain)
	addUserChainRules(rb, tbl, ch, chain, rules, addrs, svcs, acc, drop, 4096, rec)
	return rb
}

func pol(id, chain string, src, dst, svc []string) model.PolicyRule {
	return model.PolicyRule{ID: id, Name: id, Chain: chain, Status: true, Action: "ACCEPT", Source: src, Destination: dst, Service: svc}
}

func s(v ...string) []string { return v }

// assertSetBindingInvariants checks, over a whole batch: every set is
// Anonymous+Constant, non-empty, added before the rule whose Lookup references
// it, and referenced by exactly one Lookup.
func assertSetBindingInvariants(t *testing.T, rb *recBatch) {
	t.Helper()
	added := map[uint32]bool{}
	refs := map[uint32]int{}
	for _, ev := range rb.events {
		if ev.set != nil {
			if !ev.set.Anonymous || !ev.set.Constant {
				t.Errorf("set %d is not Anonymous+Constant", ev.set.ID)
			}
			if len(ev.elems) == 0 {
				t.Errorf("set %d is empty (must fail closed)", ev.set.ID)
			}
			added[ev.set.ID] = true
			continue
		}
		for _, lk := range lookupsOf(ev.rule) {
			if !added[lk.SetID] {
				t.Errorf("lookup references set %d before its AddSet", lk.SetID)
			}
			if lk.SetID == 0 || lk.SetName == "" {
				t.Errorf("lookup built before AddSet assigned ID/name: %+v", lk)
			}
			refs[lk.SetID]++
		}
	}
	for id := range added {
		if refs[id] != 1 {
			t.Errorf("set %d referenced by %d lookups, want exactly 1", id, refs[id])
		}
	}
}

func TestSetMode_SingletonsMatchLegacyGolden(t *testing.T) {
	stubLookupFunc(t, goldenStubLookup)
	setsBuilder := func(conn *nftables.Conn, table *nftables.Table, nfChain *nftables.Chain, chainName string,
		rules []model.PolicyRule, addrsMap map[string]model.AddressObject, svcsMap map[string]model.ServiceObject,
		fqdnRec *fqdnRecorder) {
		acc, drop := goldenPrefixes(chainName)
		addUserChainRulesSets(conn, table, nfChain, chainName, rules, addrsMap, svcsMap, acc, drop, 4096, fqdnRec)
	}
	_, want := readGolden(t)
	checked := 0
	for _, c := range goldenCases() {
		if !c.setsEqual {
			continue
		}
		checked++
		t.Run(c.name, func(t *testing.T) {
			newSet := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWSET)
			sets := 0
			var got []string
			newRule := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWRULE)
			conn, err := nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
				for _, m := range req {
					switch m.Header.Type {
					case newSet:
						sets++
					case newRule:
						got = append(got, hex.EncodeToString(m.Data))
					}
				}
				return req, nil
			}))
			if err != nil {
				t.Fatal(err)
			}
			table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: "pigate_golden"})
			chain := conn.AddChain(&nftables.Chain{Name: c.rule.Chain, Table: table})
			addrs, svcs := goldenObjects()
			setsBuilder(conn, table, chain, c.rule.Chain, []model.PolicyRule{c.rule}, addrs, svcs, newFQDNRecorder())
			if err := conn.Flush(); err != nil {
				t.Fatal(err)
			}
			if sets != 0 {
				t.Errorf("singleton policy emitted %d sets, want 0", sets)
			}
			w := want[c.name]
			if len(got) != len(w) {
				t.Fatalf("rule count: got %d want %d", len(got), len(w))
			}
			for i := range got {
				if got[i] != w[i] {
					t.Errorf("rule %d differs from legacy golden\n got %s\nwant %s", i, got[i], w[i])
				}
			}
		})
	}
	if checked < 100 {
		t.Fatalf("only %d singleton golden cases checked; matrix shrank?", checked)
	}
}

func TestSetMode_MultiDimensionCollapsesToOneRule(t *testing.T) {
	stubLookupFunc(t, goldenStubLookup)
	addrs := map[string]model.AddressObject{
		"S3": {Name: "S3", Entries: []model.AddressEntry{{Type: "subnet", Value: "10.0.0.0/24"}, {Type: "subnet", Value: "10.1.0.0/24"}, {Type: "subnet", Value: "10.2.0.0/24"}}},
		"D2": {Name: "D2", Entries: []model.AddressEntry{{Type: "subnet", Value: "192.168.1.0/24"}, {Type: "range", Value: "172.16.0.1-172.16.0.9"}}},
	}
	svcs := map[string]model.ServiceObject{
		"P2": {Name: "P2", Entries: []model.ServiceEntry{{Protocol: "TCP", Port: "80"}, {Protocol: "TCP", Port: "443"}}},
	}
	r := pol("r1", model.PolicyChainForward, s("S3"), s("D2"), s("P2"))
	r.InInterfaces = s("eth0", "eth1", "eth2")

	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{r}, addrs, svcs, 4096, nil)
	if n := len(rb.rules()); n != 1 {
		t.Fatalf("set mode: %d rules, want 1", n)
	}
	want := []string{"ifname", "ipv4_addr", "ipv4_addr", "inet_proto . inet_service"}
	if got := setTypeNames(rb.sets()); !reflect.DeepEqual(got, want) {
		t.Fatalf("set key types %v, want %v", got, want)
	}
	assertSetBindingInvariants(t, rb)

	legacy := runLegacy(t, model.PolicyChainForward, []model.PolicyRule{r}, addrs, svcs, nil)
	if n := len(legacy.rules()); n < 36 {
		t.Fatalf("legacy produced %d rules, want >= 36", n)
	}
}

func TestSetMode_RuleReductionVsLegacy(t *testing.T) {
	var srcE, dstE []model.AddressEntry
	for i := 0; i < 10; i++ {
		srcE = append(srcE, model.AddressEntry{Type: "subnet", Value: fmt.Sprintf("10.%d.0.0/16", i)})
		dstE = append(dstE, model.AddressEntry{Type: "subnet", Value: fmt.Sprintf("172.%d.0.0/16", 16+i)})
	}
	var svcE []model.ServiceEntry
	for i := 0; i < 5; i++ {
		svcE = append(svcE, model.ServiceEntry{Protocol: "TCP/UDP", Port: fmt.Sprint(1000 + i)})
	}
	addrs := map[string]model.AddressObject{"S": {Name: "S", Entries: srcE}, "D": {Name: "D", Entries: dstE}}
	svcs := map[string]model.ServiceObject{"V": {Name: "V", Entries: svcE}}
	r := pol("big", model.PolicyChainForward, s("S"), s("D"), s("V"))
	r.InInterfaces = s("eth0", "eth1", "eth2")

	if n := len(runLegacy(t, model.PolicyChainForward, []model.PolicyRule{r}, addrs, svcs, nil).rules()); n < 3000 {
		t.Fatalf("legacy: %d rules, want >= 3000", n)
	}
	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{r}, addrs, svcs, 4096, nil)
	if n := len(rb.rules()); n > 2 {
		t.Fatalf("set mode: %d rules, want <= 2", n)
	}
	assertSetBindingInvariants(t, rb)
}

func TestSetMode_IcmpPlusPortsSplitsIntoTwoRules(t *testing.T) {
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := goldenObjects()
	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{pol("r", model.PolicyChainForward, nil, nil, s("ICMPMIX"))}, addrs, svcs, 4096, nil)
	rules := rb.rules()
	if len(rules) != 2 {
		t.Fatalf("got %d rules, want 2", len(rules))
	}
	// Rule A: ip protocol == 1 (ICMP, no port check, no set).
	a := rules[0]
	if len(lookupsOf(a)) != 0 {
		t.Errorf("rule A must not use a set")
	}
	if cs := cmpsOf(a); len(cs) != 1 || !bytes.Equal(cs[0].Data, []byte{1}) {
		t.Errorf("rule A must compare proto to [1], got %+v", cs)
	}
	// Rule B: concat set with TCP 80 and UDP 53.
	b := rules[1]
	if len(lookupsOf(b)) != 1 {
		t.Fatalf("rule B must use exactly one lookup")
	}
	sets := rb.sets()
	if len(sets) != 1 || !sets[0].set.Concatenation || len(sets[0].elems) != 2 {
		t.Fatalf("want one concat set with 2 elements, got %+v", sets)
	}
	assertSetBindingInvariants(t, rb)
}

func TestSetMode_ProtoAnyAbsorbsSameProtoPort(t *testing.T) {
	addrs, svcs := goldenObjects()
	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{pol("r", model.PolicyChainForward, nil, nil, s("MIXANY"))}, addrs, svcs, 4096, nil)
	rules := rb.rules()
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	if len(rb.sets()) != 0 {
		t.Fatalf("TCP any + TCP 80 must not need a set, got %d", len(rb.sets()))
	}
	if cs := cmpsOf(rules[0]); len(cs) != 1 || !bytes.Equal(cs[0].Data, []byte{6}) {
		t.Fatalf("want only proto==6, got %+v", cs)
	}
	// UDP any + TCP 80 is NOT absorbed: two rules, second is a single TCP port.
	rb = runSets(t, model.PolicyChainForward, []model.PolicyRule{pol("r", model.PolicyChainForward, nil, nil, s("UDPANYTCP80"))}, addrs, svcs, 4096, nil)
	if len(rb.rules()) != 2 || len(rb.sets()) != 0 {
		t.Fatalf("UDP any + TCP 80: want 2 rules and no set, got %d rules %d sets", len(rb.rules()), len(rb.sets()))
	}
}

func TestSetMode_PortRangesMergeIntoOneInterval(t *testing.T) {
	addrs, svcs := goldenObjects()
	// RANGES = TCP 80-90, 85-100, 443 -> [80,100] + [443,443] in one concat set.
	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{pol("r", model.PolicyChainForward, nil, nil, s("RANGES"))}, addrs, svcs, 4096, nil)
	sets := rb.sets()
	if len(rb.rules()) != 1 || len(sets) != 1 || len(sets[0].elems) != 2 {
		t.Fatalf("want 1 rule with one 2-element concat set, got %d rules %+v", len(rb.rules()), sets)
	}
	if hex.EncodeToString(sets[0].elems[0].Key) != "0600000000500000" || hex.EncodeToString(sets[0].elems[0].KeyEnd) != "0600000000640000" {
		t.Fatalf("first interval not merged to 80-100: %x..%x", sets[0].elems[0].Key, sets[0].elems[0].KeyEnd)
	}
}

func TestSetMode_AllInListDropsCondition(t *testing.T) {
	addrs, svcs := goldenObjects()
	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{pol("r", model.PolicyChainForward, s("ALL", "HOST"), nil, nil)}, addrs, svcs, 4096, nil)
	rules := rb.rules()
	if len(rules) != 1 {
		t.Fatalf("got %d rules, want 1", len(rules))
	}
	for _, e := range rules[0].Exprs {
		if p, ok := e.(*expr.Payload); ok {
			t.Fatalf("src [ALL, HOST] must have no IP match, found %+v", p)
		}
	}
	if len(rb.sets()) != 0 {
		t.Fatal("no set expected")
	}
}

func TestSetMode_FailClosedOnEmptyDimension(t *testing.T) {
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := goldenObjects()
	cases := []struct {
		name          string
		src, dst, svc []string
		fqdnKey       string
	}{
		{"unknown src", s("NOPE"), nil, nil, ""},
		{"only ipv6 src", s("V6NET"), nil, nil, ""},
		{"only unresolvable fqdn src", s("FQNONE"), nil, nil, "none.example.com"},
		{"resolve failure fqdn dst", nil, s("FQFAIL"), nil, "fail.example.com"},
		{"ipv6-only fqdn", s("FQV6"), nil, nil, "v6only.example.com"},
		{"unknown dst", nil, s("NOPE"), nil, ""},
		{"unknown svc", nil, nil, s("NOPE"), ""},
		{"only bad svc", nil, nil, s("BADPORT"), ""},
		{"only reversed svc", nil, nil, s("REVRANGE"), ""},
		{"only reversed addr range", s("RNGREV"), nil, nil, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := newFQDNRecorder()
			rules := []model.PolicyRule{
				pol("bad", model.PolicyChainForward, c.src, c.dst, c.svc),
				pol("good", model.PolicyChainForward, s("HOST"), nil, nil),
			}
			rb := runSets(t, model.PolicyChainForward, rules, addrs, svcs, 4096, rec)
			if len(rb.rules()) != 1 {
				t.Fatalf("got %d rules, want exactly the good policy's 1", len(rb.rules()))
			}
			if len(rb.sets()) != 0 {
				t.Fatalf("no set may be emitted, got %d", len(rb.sets()))
			}
			if c.fqdnKey != "" {
				v, ok := rec.snapshot()[c.fqdnKey]
				if !ok || len(v) != 0 {
					t.Fatalf("fqdn key %q must be recorded with an empty value, snapshot=%v", c.fqdnKey, rec.snapshot())
				}
			}
		})
	}
}

func TestSetMode_OverlappingSubnetsMergeToOneInterval(t *testing.T) {
	addrs, svcs := goldenObjects()
	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{pol("r", model.PolicyChainForward, s("OVERLAP"), nil, nil)}, addrs, svcs, 4096, nil)
	sets := rb.sets()
	if len(rb.rules()) != 1 || len(sets) != 1 {
		t.Fatalf("want 1 rule 1 set, got %d/%d", len(rb.rules()), len(sets))
	}
	// 10.0.0.0/8 absorbs 10.1.0.0/16: 0.0.0.0(end), 10.0.0.0, 11.0.0.0(end).
	if len(sets[0].elems) != 3 {
		t.Fatalf("want 3 elements for one merged interval, got %d", len(sets[0].elems))
	}
}

func TestSetMode_FQDNMultiIPAndFQDNRecorderParity(t *testing.T) {
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := goldenObjects()
	rules := []model.PolicyRule{pol("r", model.PolicyChainForward, s("FQ3", "MIXFQ"), s("NET10"), nil)}

	recSets := newFQDNRecorder()
	rb := runSets(t, model.PolicyChainForward, rules, addrs, svcs, 4096, recSets)
	recLegacy := newFQDNRecorder()
	runLegacy(t, model.PolicyChainForward, rules, addrs, svcs, recLegacy)

	if len(rb.rules()) != 1 {
		t.Fatalf("got %d rules, want 1", len(rb.rules()))
	}
	if !reflect.DeepEqual(recSets.snapshot(), recLegacy.snapshot()) {
		t.Fatalf("fqdn snapshot differs: sets=%v legacy=%v", recSets.snapshot(), recLegacy.snapshot())
	}
	sets := rb.sets()
	// 203.0.113.10/.20/.30 (.10 also via MIXFQ) + 10.0.0.0/8 = 4 intervals:
	// leading 0.0.0.0 end element + start/end pair each.
	if len(sets) != 1 || len(sets[0].elems) != 9 {
		t.Fatalf("want 1 set with 9 elements, got %d sets %+v", len(sets), sets)
	}
	assertSetBindingInvariants(t, rb)
}

func TestSetMode_ChainScopingAndNatMark(t *testing.T) {
	addrs, svcs := goldenObjects()
	mk := func(chain string, nat bool, action string) *nftables.Rule {
		r := pol("r", chain, nil, nil, nil)
		r.InInterfaces, r.OutInterfaces = s("eth0", "eth1"), s("eth2", "eth3")
		r.Nat, r.Action = nat, action
		rb := runSets(t, chain, []model.PolicyRule{r}, addrs, svcs, 4096, nil)
		if len(rb.rules()) != 1 {
			t.Fatalf("%s: got %d rules", chain, len(rb.rules()))
		}
		assertSetBindingInvariants(t, rb)
		return rb.rules()[0]
	}
	hasMetaKey := func(r *nftables.Rule, k expr.MetaKey, src bool) bool {
		for _, e := range r.Exprs {
			if m, ok := e.(*expr.Meta); ok && m.Key == k && m.SourceRegister == src {
				return true
			}
		}
		return false
	}

	in := mk(model.PolicyChainInput, true, "ACCEPT")
	if hasMetaKey(in, expr.MetaKeyOIFNAME, false) || !hasMetaKey(in, expr.MetaKeyIIFNAME, false) {
		t.Error("input chain: iifname only, never oifname")
	}
	out := mk(model.PolicyChainOutput, true, "ACCEPT")
	if hasMetaKey(out, expr.MetaKeyIIFNAME, false) || !hasMetaKey(out, expr.MetaKeyOIFNAME, false) {
		t.Error("output chain: oifname only, never iifname")
	}
	if hasMetaKey(in, expr.MetaKeyMARK, true) || hasMetaKey(out, expr.MetaKeyMARK, true) {
		t.Error("fwmark must never appear on input/output")
	}
	fwd := mk(model.PolicyChainForward, true, "ACCEPT")
	if !hasMetaKey(fwd, expr.MetaKeyIIFNAME, false) || !hasMetaKey(fwd, expr.MetaKeyOIFNAME, false) || !hasMetaKey(fwd, expr.MetaKeyMARK, true) {
		t.Error("forward+nat+ACCEPT: iif, oif and fwmark expected")
	}
	if hasMetaKey(mk(model.PolicyChainForward, true, "DROP"), expr.MetaKeyMARK, true) {
		t.Error("fwmark must not be set on DROP")
	}
	if hasMetaKey(mk(model.PolicyChainForward, false, "ACCEPT"), expr.MetaKeyMARK, true) {
		t.Error("fwmark must not be set without nat")
	}
}

func TestSetMode_UserDataAndLogPrefix(t *testing.T) {
	addrs, svcs := goldenObjects()
	for _, chain := range []string{model.PolicyChainForward, model.PolicyChainInput, model.PolicyChainOutput} {
		r := pol("rule-42", chain, s("NET10", "RNG"), nil, s("WEB"))
		r.Log = true
		rb := runSets(t, chain, []model.PolicyRule{r}, addrs, svcs, 4096, nil)
		if len(rb.rules()) == 0 {
			t.Fatalf("%s: no rules", chain)
		}
		wantUD := userdata.AppendString(nil, userdata.TypeComment, "rule-42")
		for _, rule := range rb.rules() {
			if !bytes.Equal(rule.UserData, wantUD) {
				t.Errorf("%s: UserData %x, want %x", chain, rule.UserData, wantUD)
			}
			found := false
			for _, e := range rule.Exprs {
				if l, ok := e.(*expr.Log); ok {
					found = true
					if !strings.Contains(string(l.Data), "r=rule-42 ") {
						t.Errorf("%s: log prefix %q lacks r=<id>", chain, l.Data)
					}
				}
			}
			if !found {
				t.Errorf("%s: log expr missing", chain)
			}
		}
	}
}

func TestSetMode_AtomCapSkipsPolicy(t *testing.T) {
	var entries []model.AddressEntry
	for i := 0; i < 65; i++ {
		entries = append(entries, model.AddressEntry{Type: "subnet", Value: fmt.Sprintf("10.0.%d.0/24", i)})
	}
	addrs := map[string]model.AddressObject{
		"BIG":   {Name: "BIG", Entries: entries},
		"SMALL": {Name: "SMALL", Entries: entries[:3]},
	}
	rules := []model.PolicyRule{
		pol("big", model.PolicyChainForward, s("BIG"), nil, nil),
		pol("small", model.PolicyChainForward, s("SMALL"), nil, nil),
	}
	rb := runSets(t, model.PolicyChainForward, rules, addrs, nil, 64, nil)
	got := rb.rules()
	if len(got) != 1 {
		t.Fatalf("got %d rules, want only the small policy", len(got))
	}
	if !bytes.Equal(got[0].UserData, userdata.AppendString(nil, userdata.TypeComment, "small")) {
		t.Fatal("the emitted rule is not the small policy")
	}
	// Exactly at the cap is fine.
	rb = runSets(t, model.PolicyChainForward, rules[:1], addrs, nil, 65, nil)
	if len(rb.rules()) != 1 {
		t.Fatalf("65 atoms with cap 65 must be emitted, got %d rules", len(rb.rules()))
	}
}

func TestSetMode_DisabledAndOtherChainSkipped(t *testing.T) {
	r1 := pol("off", model.PolicyChainForward, nil, nil, nil)
	r1.Status = false
	r2 := pol("other", model.PolicyChainInput, nil, nil, nil)
	rb := runSets(t, model.PolicyChainForward, []model.PolicyRule{r1, r2}, nil, nil, 4096, nil)
	if len(rb.rules()) != 0 {
		t.Fatalf("got %d rules, want 0", len(rb.rules()))
	}
}

// TestApplyRules_DispatchOnUseSets proves the rf.useSets switch: false emits
// the exact legacy bytes for the user chains (and no NEWSET), true emits sets.
func TestApplyRules_DispatchOnUseSets(t *testing.T) {
	stubLookupFunc(t, goldenStubLookup)
	addrs, svcs := goldenObjects()
	addrList := make([]model.AddressObject, 0, len(addrs))
	for _, a := range addrs {
		addrList = append(addrList, a)
	}
	svcList := make([]model.ServiceObject, 0, len(svcs))
	for _, v := range svcs {
		svcList = append(svcList, v)
	}
	policies := []model.PolicyRule{
		pol("p-in", model.PolicyChainInput, s("MULTI"), nil, s("WEB")),
		pol("p-fwd", model.PolicyChainForward, s("HOST"), s("OVERLAP"), s("TCP22")),
		pol("p-out", model.PolicyChainOutput, nil, s("NET10"), s("TCPUDP53")),
	}

	newRule := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWRULE)
	newSet := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWSET)
	run := func(useSets bool) (ruleBytes []string, sets int) {
		orig := newFirewallConn
		newFirewallConn = func() (*nftables.Conn, error) {
			return nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
				for _, m := range req {
					switch m.Header.Type {
					case newRule:
						ruleBytes = append(ruleBytes, hex.EncodeToString(m.Data))
					case newSet:
						sets++
					}
				}
				return req, nil
			}))
		}
		defer func() { newFirewallConn = orig }()
		rf := NewRealFirewall(false)
		rf.SetUseNFTSets(useSets)
		if err := rf.ApplyRules(policies, nil, addrList, svcList, nil, nil, nil); err != nil {
			t.Fatalf("ApplyRules(useSets=%v): %v", useSets, err)
		}
		return
	}

	legacyBytes, legacySets := run(false)
	setBytes, setSets := run(true)
	if legacySets != 0 {
		t.Errorf("useSets=false emitted %d sets", legacySets)
	}
	if setSets == 0 {
		t.Errorf("useSets=true emitted no sets for multi-value policies")
	}
	if len(setBytes) >= len(legacyBytes) {
		t.Errorf("set mode must emit fewer rules: sets=%d legacy=%d", len(setBytes), len(legacyBytes))
	}

	// Legacy user-chain bytes must appear, in order, inside the useSets=false stream.
	want := legacyUserBytes(t, policies, addrs, svcs)
	if !containsSubsequence(legacyBytes, want) {
		t.Errorf("useSets=false ApplyRules stream does not contain the direct legacy addUserChainRules bytes")
	}
}

// legacyUserBytes renders the legacy user-rule NEWRULE bytes for policies into
// tables/chains named exactly as ApplyRules names them ("pigate"/input|forward|output).
func legacyUserBytes(t *testing.T, policies []model.PolicyRule, addrs map[string]model.AddressObject,
	svcs map[string]model.ServiceObject) []string {
	t.Helper()
	newRule := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWRULE)
	var out []string
	for _, chain := range []string{model.PolicyChainInput, model.PolicyChainForward, model.PolicyChainOutput} {
		conn, err := nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
			for _, m := range req {
				if m.Header.Type == newRule {
					out = append(out, hex.EncodeToString(m.Data))
				}
			}
			return req, nil
		}))
		if err != nil {
			t.Fatal(err)
		}
		table := conn.AddTable(&nftables.Table{Name: "pigate", Family: nftables.TableFamilyINet})
		nfChain := conn.AddChain(&nftables.Chain{Name: chain, Table: table})
		acc, drop := goldenPrefixes(chain)
		addUserChainRules(conn, table, nfChain, chain, policies, addrs, svcs, acc, drop, 4096, nil)
		if err := conn.Flush(); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func containsSubsequence(hay, needle []string) bool {
	if len(needle) == 0 {
		return true
	}
	j := 0
	for _, h := range hay {
		if h == needle[j] {
			j++
			if j == len(needle) {
				return true
			}
		}
	}
	return false
}
