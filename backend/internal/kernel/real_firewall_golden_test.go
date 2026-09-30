package kernel

import (
	"bufio"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"sort"
	"strings"
	"testing"

	"pigate/internal/model"

	"github.com/google/nftables"
	"github.com/mdlayher/netlink"
	"golang.org/x/sys/unix"
)

// Golden snapshot of the legacy (cartesian) user-rule path.
//
// docs/ref/todo/nftables-sets-refactor-plan.md T-01 / Caution 16: the golden
// file testdata/legacy_user_rules.golden was generated from the code as it
// stood BEFORE the nftables sets refactor touched real_firewall.go. It pins
// the exact NFT_MSG_NEWRULE bytes addUserChainRules emits for a fixed policy
// matrix, so that (a) nft-use-sets=false can be proven byte-identical to the
// pre-refactor behavior and (b) set mode can be proven byte-identical for
// policies whose every dimension is a singleton (goldenCase.setsEqual).
//
// Regenerate deliberately with: go test ./internal/kernel -run TestLegacyUserRulesGolden -update
// Never regenerate just to make a failing run pass — a diff here means the
// legacy rule bytes changed.

var updateGolden = flag.Bool("update", false, "rewrite testdata/legacy_user_rules.golden from the current legacy path")

const goldenPath = "testdata/legacy_user_rules.golden"

// goldenCase is one policy of the fixed matrix. setsEqual marks policies that
// set mode must emit byte-for-byte identically to legacy (every dimension a
// singleton after dedupe, or a fail-closed/zero-rule case).
type goldenCase struct {
	name      string
	rule      model.PolicyRule
	setsEqual bool
}

// goldenStubLookup is the deterministic FQDN resolver used by the matrix.
func goldenStubLookup(host string) ([]net.IP, error) {
	switch host {
	case "one.example.com":
		return []net.IP{net.ParseIP("203.0.113.10")}, nil
	case "three.example.com":
		return []net.IP{net.ParseIP("203.0.113.30"), net.ParseIP("203.0.113.10"), net.ParseIP("203.0.113.20")}, nil
	case "v6only.example.com":
		return []net.IP{net.ParseIP("2001:db8::1")}, nil
	case "none.example.com":
		return nil, nil
	default:
		return nil, errors.New("no such host")
	}
}

func goldenObjects() (map[string]model.AddressObject, map[string]model.ServiceObject) {
	ae := func(t, v string) model.AddressEntry { return model.AddressEntry{Type: t, Value: v} }
	addr := func(name string, e ...model.AddressEntry) model.AddressObject {
		return model.AddressObject{ID: "a-" + name, Name: name, Type: e[0].Type, Value: e[0].Value, Entries: e}
	}
	addrs := map[string]model.AddressObject{}
	for _, o := range []model.AddressObject{
		addr("NET10", ae("subnet", "10.0.0.0/8")),
		addr("HOST", ae("subnet", "10.1.0.1/32")),
		addr("NOSLASH", ae("subnet", "10.2.0.5")),
		addr("HOSTBITS", ae("subnet", "10.1.2.3/24")),
		addr("ANY4", ae("subnet", "0.0.0.0/0")),
		addr("RNG", ae("range", "10.0.0.1-10.0.0.9")),
		addr("RNGSP", ae("range", "10.0.0.1 - 10.0.0.9")),
		addr("RNGREV", ae("range", "10.0.0.9-10.0.0.1")),
		addr("RNGTOP", ae("range", "192.168.0.0-255.255.255.255")),
		addr("RNGFULL", ae("range", "0.0.0.0-255.255.255.255")),
		addr("FQ1", ae("fqdn", "one.example.com")),
		addr("FQ3", ae("fqdn", "three.example.com")),
		addr("FQV6", ae("fqdn", "v6only.example.com")),
		addr("FQNONE", ae("fqdn", "none.example.com")),
		addr("FQFAIL", ae("fqdn", "fail.example.com")),
		addr("V6NET", ae("subnet", "2001:db8::/32")),
		addr("BADSUB", ae("subnet", "not-an-ip")),
		addr("BADRNG", ae("range", "10.0.0.1")),
		addr("MULTI", ae("subnet", "10.0.0.0/8"), ae("range", "172.16.0.1-172.16.0.9"), ae("subnet", "192.168.50.7/32")),
		addr("OVERLAP", ae("subnet", "10.0.0.0/8"), ae("subnet", "10.1.0.0/16")),
		addr("ADJ", ae("range", "10.0.0.0-10.0.0.9"), ae("range", "10.0.0.10-10.0.0.20")),
		addr("DUP", ae("subnet", "10.1.0.1/32"), ae("subnet", "10.1.0.1")),
		addr("MIXFQ", ae("fqdn", "one.example.com"), ae("subnet", "10.0.0.0/8")),
		addr("MIXBAD", ae("subnet", "10.0.0.0/8"), ae("subnet", "not-an-ip")),
		addr("ONLYV6MIX", ae("subnet", "2001:db8::/32"), ae("fqdn", "v6only.example.com")),
	} {
		addrs[o.Name] = o
	}

	se := func(p, port string) model.ServiceEntry { return model.ServiceEntry{Protocol: p, Port: port} }
	svc := func(name string, e ...model.ServiceEntry) model.ServiceObject {
		return model.ServiceObject{ID: "s-" + name, Name: name, Protocol: e[0].Protocol, Port: e[0].Port, Entries: e}
	}
	svcs := map[string]model.ServiceObject{}
	for _, o := range []model.ServiceObject{
		svc("TCP22", se("TCP", "22")),
		svc("UDP53", se("UDP", "53")),
		svc("TCPUDP53", se("TCP/UDP", "53")),
		svc("ICMP", se("ICMP", "")),
		svc("ICMPPORT", se("ICMP", "80")),
		svc("TCPRANGE", se("TCP", "8000-8010")),
		svc("TCPANY", se("TCP", "")),
		svc("TCPDASH", se("TCP", "-")),
		svc("TCPFULL", se("TCP", "1-65535")),
		svc("TCPSPFULL", se("TCP", "1 - 65535")),
		svc("TCP3PART", se("TCP", "80-90-100")),
		svc("TCPSPACE", se("TCP", " 80 ")),
		svc("TCP65535", se("TCP", "65535")),
		svc("BADPORT", se("TCP", "abc")),
		svc("BIGPORT", se("TCP", "70000")),
		svc("REVRANGE", se("TCP", "90-80")),
		svc("SCTP", se("SCTP", "80")),
		svc("WEB", se("TCP", "80"), se("TCP", "443"), se("UDP", "53")),
		svc("MIXANY", se("TCP", ""), se("TCP", "80")),
		svc("ICMPMIX", se("ICMP", ""), se("TCP", "80"), se("UDP", "53")),
		svc("RANGES", se("TCP", "80-90"), se("TCP", "85-100"), se("TCP", "443")),
		svc("DUPSVC", se("TCP", "22"), se("TCP", "22")),
		svc("UDPANYTCP80", se("UDP", ""), se("TCP", "80")),
		svc("ONEBAD", se("TCP", "22"), se("TCP", "abc")),
	} {
		svcs[o.Name] = o
	}
	return addrs, svcs
}

// goldenCases builds the fixed matrix: every base policy is emitted once per
// chain (forward/input/output), plus an action x log x nat block.
func goldenCases() []goldenCase {
	type base struct {
		name      string
		src, dst  []string
		svc       []string
		in, out   []string
		legacyIn  string // deprecated scalar, exercises NormalizePolicyRuleInterfaces
		setsEqual bool
	}
	bases := []base{
		{name: "all-all-all", setsEqual: true},
		{name: "explicit-ALL", src: []string{"ALL"}, dst: []string{"ALL"}, svc: []string{"ALL"}, setsEqual: true},
		{name: "subnet-src", src: []string{"NET10"}, setsEqual: true},
		{name: "subnet-dst", dst: []string{"NET10"}, setsEqual: true},
		{name: "host32", src: []string{"HOST"}, dst: []string{"HOST"}, setsEqual: true},
		{name: "noslash-subnet", src: []string{"NOSLASH"}, setsEqual: true},
		{name: "host-bits-subnet", src: []string{"HOSTBITS"}, setsEqual: true},
		{name: "any4-subnet", src: []string{"ANY4"}, setsEqual: true},
		{name: "range", src: []string{"RNG"}, dst: []string{"RNG"}, setsEqual: true},
		{name: "range-spaces", src: []string{"RNGSP"}, setsEqual: true},
		{name: "range-reversed", src: []string{"RNGREV"}},
		{name: "range-top", dst: []string{"RNGTOP"}, setsEqual: true},
		{name: "range-full", src: []string{"RNGFULL"}, setsEqual: true},
		{name: "fqdn-one", src: []string{"FQ1"}, dst: []string{"FQ1"}, setsEqual: true},
		{name: "fqdn-three", src: []string{"FQ3"}},
		{name: "fqdn-v6only", src: []string{"FQV6"}, setsEqual: true},
		{name: "fqdn-none", dst: []string{"FQNONE"}, setsEqual: true},
		{name: "fqdn-fail", dst: []string{"FQFAIL"}, setsEqual: true},
		{name: "v6-subnet", src: []string{"V6NET"}, setsEqual: true},
		{name: "bad-subnet", src: []string{"BADSUB"}, setsEqual: true},
		{name: "bad-range", src: []string{"BADRNG"}, setsEqual: true},
		{name: "unknown-src", src: []string{"NOPE"}, setsEqual: true},
		{name: "unknown-dst", dst: []string{"NOPE"}, setsEqual: true},
		{name: "unknown-svc", svc: []string{"NOPE"}, setsEqual: true},
		{name: "multi-addr", src: []string{"MULTI"}},
		{name: "overlap-addr", src: []string{"OVERLAP"}},
		{name: "adjacent-addr", dst: []string{"ADJ"}},
		{name: "dup-addr", src: []string{"DUP"}},
		{name: "mixed-fqdn-subnet", src: []string{"MIXFQ"}},
		{name: "mixed-bad-entry", src: []string{"MIXBAD"}, setsEqual: true},
		{name: "only-v6-mix", src: []string{"ONLYV6MIX"}, setsEqual: true},
		{name: "multi-name-src", src: []string{"HOST", "NET10"}},
		{name: "src-ALL-plus-name", src: []string{"ALL", "HOST"}},
		{name: "svc-tcp22", svc: []string{"TCP22"}, setsEqual: true},
		{name: "svc-udp53", svc: []string{"UDP53"}, setsEqual: true},
		{name: "svc-tcpudp53", svc: []string{"TCPUDP53"}},
		{name: "svc-icmp", svc: []string{"ICMP"}, setsEqual: true},
		{name: "svc-icmp-with-port", svc: []string{"ICMPPORT"}, setsEqual: true},
		{name: "svc-port-range", svc: []string{"TCPRANGE"}, setsEqual: true},
		{name: "svc-tcp-empty-port", svc: []string{"TCPANY"}, setsEqual: true},
		{name: "svc-tcp-dash", svc: []string{"TCPDASH"}, setsEqual: true},
		{name: "svc-tcp-1-65535", svc: []string{"TCPFULL"}, setsEqual: true},
		{name: "svc-tcp-spaced-1-65535", svc: []string{"TCPSPFULL"}, setsEqual: true},
		{name: "svc-tcp-3-parts", svc: []string{"TCP3PART"}, setsEqual: true},
		{name: "svc-tcp-space-padded", svc: []string{"TCPSPACE"}, setsEqual: true},
		{name: "svc-tcp-65535", svc: []string{"TCP65535"}, setsEqual: true},
		{name: "svc-bad-port", svc: []string{"BADPORT"}, setsEqual: true},
		{name: "svc-big-port", svc: []string{"BIGPORT"}},
		{name: "svc-reversed-range", svc: []string{"REVRANGE"}},
		{name: "svc-sctp", svc: []string{"SCTP"}, setsEqual: true},
		{name: "svc-multi", svc: []string{"WEB"}},
		{name: "svc-tcp-any-plus-port", svc: []string{"MIXANY"}},
		{name: "svc-icmp-tcp-udp", svc: []string{"ICMPMIX"}},
		{name: "svc-overlapping-ranges", svc: []string{"RANGES"}},
		{name: "svc-dup-entry", svc: []string{"DUPSVC"}},
		{name: "svc-udp-any-tcp-port", svc: []string{"UDPANYTCP80"}},
		{name: "svc-one-bad-entry", svc: []string{"ONEBAD"}, setsEqual: true},
		{name: "svc-two-names", svc: []string{"TCP22", "UDP53"}},
		{name: "combo-singletons", src: []string{"HOST"}, dst: []string{"RNG"}, svc: []string{"TCP22"}, in: []string{"eth0"}, out: []string{"eth1"}, setsEqual: true},
		{name: "combo-fqdn-singletons", src: []string{"FQ1"}, dst: []string{"NET10"}, svc: []string{"TCPRANGE"}, in: []string{"eth0"}, setsEqual: true},
		{name: "iface-single-in", in: []string{"eth0"}, setsEqual: true},
		{name: "iface-single-out", out: []string{"eth1"}, setsEqual: true},
		{name: "iface-both", in: []string{"eth0"}, out: []string{"eth1"}, setsEqual: true},
		{name: "iface-legacy-scalar", legacyIn: "wlan0", setsEqual: true},
		{name: "iface-ALL", in: []string{"ALL"}, out: []string{"ALL"}, setsEqual: true},
		{name: "iface-multi-in", in: []string{"eth0", "wlan0", "br0"}},
		{name: "iface-multi-out", out: []string{"eth1", "eth2"}},
		{name: "iface-multi-both", in: []string{"eth0", "wlan0"}, out: []string{"eth1", "eth2"}},
		{name: "full-cartesian", src: []string{"MULTI"}, dst: []string{"OVERLAP"}, svc: []string{"WEB"}, in: []string{"eth0", "wlan0"}, out: []string{"eth1", "eth2"}},
	}

	var out []goldenCase
	for _, chain := range []string{model.PolicyChainForward, model.PolicyChainInput, model.PolicyChainOutput} {
		for _, b := range bases {
			out = append(out, goldenCase{
				name: chain + "/" + b.name,
				rule: model.PolicyRule{
					ID: "r-" + strings.ReplaceAll(b.name, " ", "_"), Name: b.name, Chain: chain, Status: true,
					Action: "ACCEPT", Source: b.src, Destination: b.dst, Service: b.svc,
					InInterfaces: b.in, OutInterfaces: b.out, InInterface: b.legacyIn,
				},
				setsEqual: b.setsEqual,
			})
		}
		for _, action := range []string{"ACCEPT", "DROP"} {
			for _, logOn := range []bool{false, true} {
				for _, natOn := range []bool{false, true} {
					name := fmt.Sprintf("flags-%s-log%t-nat%t", action, logOn, natOn)
					out = append(out, goldenCase{
						name: chain + "/" + name,
						rule: model.PolicyRule{
							ID: "r-" + name, Name: name, Chain: chain, Status: true,
							Action: action, Log: logOn, Nat: natOn,
							Source: []string{"HOST"}, Destination: []string{"NET10"}, Service: []string{"TCP22"},
							InInterfaces: []string{"eth0"}, OutInterfaces: []string{"eth1"},
						},
						setsEqual: true,
					})
				}
			}
		}
		// Disabled rule and a rule whose ID fails the log-token whitelist.
		out = append(out, goldenCase{
			name:      chain + "/disabled",
			rule:      model.PolicyRule{ID: "r-off", Name: "off", Chain: chain, Status: false, Action: "ACCEPT", Log: true},
			setsEqual: true,
		})
		out = append(out, goldenCase{
			name: chain + "/bad-token-id",
			rule: model.PolicyRule{ID: "bad id!", Name: "bad", Chain: chain, Status: true, Action: "DROP", Log: true,
				Source: []string{"HOST"}},
			setsEqual: true,
		})
	}
	return out
}

func stubLookupFunc(t *testing.T, fn func(string) ([]net.IP, error)) {
	t.Helper()
	orig := lookupIP
	lookupIP = fn
	t.Cleanup(func() { lookupIP = orig })
}

// userChainBuilder is the shape shared by addUserChainRules and (later) its
// set-mode twin, so the golden capture can drive either.
type userChainBuilder func(
	conn *nftables.Conn,
	table *nftables.Table,
	nfChain *nftables.Chain,
	chainName string,
	rules []model.PolicyRule,
	addrsMap map[string]model.AddressObject,
	svcsMap map[string]model.ServiceObject,
	fqdnRec *fqdnRecorder,
)

func goldenPrefixes(chainName string) (accept, drop string) {
	switch chainName {
	case model.PolicyChainInput:
		return "[PiGate] INP ACCEPT: ", "[PiGate] INP DROP  : "
	case model.PolicyChainOutput:
		return "[PiGate] OUT ACCEPT: ", "[PiGate] OUT DROP  : "
	}
	return "[PiGate] FWD ACCEPT: ", "[PiGate] FWD DROP  : "
}

// captureNewRules runs build for one policy on a fresh test connection and
// returns the hex of every NFT_MSG_NEWRULE msg.Data, in message order.
func captureNewRules(t *testing.T, c goldenCase, build userChainBuilder) []string {
	t.Helper()
	newRuleType := netlink.HeaderType((unix.NFNL_SUBSYS_NFTABLES << 8) | unix.NFT_MSG_NEWRULE)
	var got []string
	conn, err := nftables.New(nftables.WithTestDial(func(req []netlink.Message) ([]netlink.Message, error) {
		for _, m := range req {
			if m.Header.Type == newRuleType {
				got = append(got, hex.EncodeToString(m.Data))
			}
		}
		return req, nil
	}))
	if err != nil {
		t.Fatalf("nftables.New: %v", err)
	}
	table := conn.AddTable(&nftables.Table{Family: nftables.TableFamilyINet, Name: "pigate_golden"})
	chain := conn.AddChain(&nftables.Chain{Name: c.rule.Chain, Table: table})
	addrs, svcs := goldenObjects()
	build(conn, table, chain, c.rule.Chain, []model.PolicyRule{c.rule}, addrs, svcs, newFQDNRecorder())
	if err := conn.Flush(); err != nil {
		t.Fatalf("Flush: %v", err)
	}
	return got
}

func legacyBuilder(conn *nftables.Conn, table *nftables.Table, nfChain *nftables.Chain, chainName string,
	rules []model.PolicyRule, addrsMap map[string]model.AddressObject, svcsMap map[string]model.ServiceObject,
	fqdnRec *fqdnRecorder) {
	acc, drop := goldenPrefixes(chainName)
	addUserChainRules(conn, table, nfChain, chainName, rules, addrsMap, svcsMap, acc, drop, 4096, fqdnRec)
}

// readGolden parses the golden file into name -> hex rules (insertion order
// kept in names).
func readGolden(t *testing.T) (names []string, rules map[string][]string) {
	t.Helper()
	f, err := os.Open(goldenPath)
	if err != nil {
		t.Fatalf("open golden: %v (generate with -update on UNMODIFIED legacy code)", err)
	}
	defer f.Close()
	rules = map[string][]string{}
	cur := ""
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case line == "" || strings.HasPrefix(line, "# "):
		case strings.HasPrefix(line, "## "):
			cur = strings.TrimPrefix(line, "## ")
			names = append(names, cur)
			rules[cur] = nil
		default:
			rules[cur] = append(rules[cur], line)
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read golden: %v", err)
	}
	return names, rules
}

func TestLegacyUserRulesGolden(t *testing.T) {
	stubLookupFunc(t, goldenStubLookup)
	cases := goldenCases()

	if *updateGolden {
		var sb strings.Builder
		sb.WriteString("# Legacy addUserChainRules NFT_MSG_NEWRULE bytes (hex of msg.Data), one line per nft rule.\n")
		sb.WriteString("# Generated from the pre-sets-refactor code; see real_firewall_golden_test.go. Do not edit by hand.\n")
		for _, c := range cases {
			fmt.Fprintf(&sb, "## %s\n", c.name)
			for _, h := range captureNewRules(t, c, legacyBuilder) {
				sb.WriteString(h + "\n")
			}
		}
		if err := os.WriteFile(goldenPath, []byte(sb.String()), 0o644); err != nil {
			t.Fatalf("write golden: %v", err)
		}
		t.Logf("wrote %s (%d cases)", goldenPath, len(cases))
		return
	}

	names, want := readGolden(t)
	if len(names) != len(cases) {
		t.Fatalf("golden has %d cases, matrix has %d", len(names), len(cases))
	}
	sort.Strings(names)
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := captureNewRules(t, c, legacyBuilder)
			w, ok := want[c.name]
			if !ok {
				t.Fatalf("case missing from golden file")
			}
			if len(got) != len(w) {
				t.Fatalf("rule count: got %d want %d", len(got), len(w))
			}
			for i := range got {
				if got[i] != w[i] {
					t.Errorf("rule %d bytes differ\n got %s\nwant %s", i, got[i], w[i])
				}
			}
		})
	}
}
