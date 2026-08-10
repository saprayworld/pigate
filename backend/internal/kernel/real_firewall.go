package kernel

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"pigate/internal/model"
	"regexp"
	"strconv"
	"strings"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"github.com/google/nftables/userdata"
	"golang.org/x/sys/unix"
)

// --- Rule-match log tagging (docs/ref/todo/traffic-log-rule-name-and-domain-plan.md) ---
//
// Every nftables log point (per-rule accept/drop as well as structural
// points like admin-access/default-drop) gets an "r=<token> " marker
// appended to its log prefix Data. The NFLOG reader (real_traffic_log.go)
// extracts the token back out and the service layer resolves it to a
// display name — per-rule tokens are PolicyRule.ID (resolved dynamically,
// snapshot-on-write), system tokens are the SysToken* constants below
// (resolved via a static table, never change). This only changes the Data
// byte-string of expr.Log nodes; it never adds/removes/reorders rules or
// changes verdicts (tech_stack_design.md §4.3, Caution: do not touch the 4
// section structure).
const (
	SysTokenNotLocalDrop       = "sys-notlocal-drop"
	SysTokenDhcpServerAccept   = "sys-dhcp-server-accept"
	SysTokenDhcpClientAccept   = "sys-dhcp-client-accept"
	SysTokenDockerAccept       = "sys-docker-accept"
	SysTokenInputDefaultDrop   = "sys-input-defaultdrop"
	SysTokenForwardDefaultDrop = "sys-forward-defaultdrop"
	SysTokenAdminPing          = "sys-admin-ping"
	SysTokenAdminHTTP          = "sys-admin-http"
	SysTokenAdminHTTPS         = "sys-admin-https"
	SysTokenAdminSSH           = "sys-admin-ssh"
	SysTokenDNSServerAccept    = "sys-dns-server-accept"
)

// logTokenPattern is a strict whitelist for anything allowed into an
// nftables log prefix as a "r=<token>" marker. PolicyRule.ID is never
// validated anywhere upstream — model.ValidatePolicyRule skips the ID
// field entirely, and a restored config backup can write an arbitrary
// string into it — so this is the only gate between untrusted DB content
// and the kernel log prefix. Deliberately whitelist-only (no
// escaping/truncation of bad input): 1-32 bytes of ASCII
// letters/digits/underscore/hyphen, matching the token shape PiGate itself
// generates for rule/object IDs.
var logTokenPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)

// maxLogPrefixBytes bounds the combined "<base><r=token> " log prefix so it
// stays well under nftables/NFLOG's own log-prefix limit (128 bytes).
const maxLogPrefixBytes = 120

// withRuleToken appends a sanitized "r=<token> " marker to base. If token
// fails the whitelist, or the combined prefix would exceed
// maxLogPrefixBytes, the token is silently dropped and base is returned
// unchanged — callers must not treat that as an error, just "no rule name
// available for this log line".
func withRuleToken(base, token string) string {
	if !logTokenPattern.MatchString(token) {
		return base
	}
	tagged := base + "r=" + token + " "
	if len(tagged) > maxLogPrefixBytes {
		return base
	}
	return tagged
}

// RealFirewall implements FirewallManager using Netlink and github.com/google/nftables
type RealFirewall struct {
	dockerCompat bool
}

func NewRealFirewall(dockerCompat bool) *RealFirewall {
	return &RealFirewall{
		dockerCompat: dockerCompat,
	}
}

func (rf *RealFirewall) ApplyRules(
	rules []model.PolicyRule,
	ifaces []model.NetworkInterface,
	addrs []model.AddressObject,
	svcs []model.ServiceObject,
	dhcpServerIfaces []string,
	dnsServerIfaces []string,
	portForwards []model.PortForward,
) error {
	log.Printf("[RealFirewall] Applying %d rules to Linux kernel via Netlink (Docker Compatibility: %t, Addresses: %d, Services: %d, PortForwards: %d)",
		len(rules), rf.dockerCompat, len(addrs), len(svcs), len(portForwards))

	// Connect to nftables netlink interface
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to connect to nftables: %w (requires root or CAP_NET_ADMIN)", err)
	}

	// 1. Build lookup helper maps for address and service objects
	addrsMap := make(map[string]model.AddressObject)
	for _, a := range addrs {
		addrsMap[a.Name] = a
	}
	svcsMap := make(map[string]model.ServiceObject)
	for _, s := range svcs {
		svcsMap[s.Name] = s
	}

	// 2. Setup the main "pigate" filter table (inet family to cover IPv4 and IPv6)
	table := conn.AddTable(&nftables.Table{
		Name:   "pigate",
		Family: nftables.TableFamilyINet,
	})

	// Flush table first to wipe any old rules in this transaction
	conn.FlushTable(table)

	// 3. Setup the custom "pigate-not-local" chain for anti-IP spoofing and drop logging limit
	notLocalChain := conn.AddChain(&nftables.Chain{
		Name:  "pigate-not-local",
		Table: table,
	})

	// Add rules to "pigate-not-local":
	// Rule 3.1: fib daddr type local return
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: notLocalChain,
		Exprs: []expr.Any{
			&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: uint32ToBytes(2)}, // RTN_LOCAL = 2
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})

	// Rule 3.2: fib daddr type multicast return
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: notLocalChain,
		Exprs: []expr.Any{
			&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: uint32ToBytes(5)}, // RTN_MULTICAST = 5
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})

	// Rule 3.3: fib daddr type broadcast return
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: notLocalChain,
		Exprs: []expr.Any{
			&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: uint32ToBytes(3)}, // RTN_BROADCAST = 3
			&expr.Verdict{Kind: expr.VerdictReturn},
		},
	})

	// Rule 3.4: limit rate 3/minute burst 10 packets, log prefix "[PiGate]  INP DROP  : "
	// to NFLOG group LocalNflogGroup (Local Traffic page) instead of printk —
	// log-only rule (no verdict), so keeping the rate limit here is safe.
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: notLocalChain,
		Exprs: []expr.Any{
			&expr.Limit{Type: expr.LimitTypePkts, Rate: 3, Unit: expr.LimitTimeMinute, Burst: 10, Over: false},
			localLogExpr(withRuleToken("[PiGate]  INP DROP  : ", SysTokenNotLocalDrop)),
		},
	})

	// Rule 3.5: drop
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: notLocalChain,
		Exprs: []expr.Any{
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})

	// 4. Setup base "input" chain
	policyDrop := nftables.ChainPolicyDrop
	inputChain := conn.AddChain(&nftables.Chain{
		Name:     "input",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookInput,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &policyDrop,
	})

	// --- Section 1: Sanity & Drop Checks ---
	// ct state established,related accept
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: uint32ToBytes(6), Xor: uint32ToBytes(0)}, // 2 | 4 = 6
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: uint32ToBytes(0)},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// ct state invalid drop
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: uint32ToBytes(1), Xor: uint32ToBytes(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: uint32ToBytes(0)},
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})

	// iifname "lo" accept
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName("lo")},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// icmp type { destination-unreachable, time-exceeded, parameter-problem, echo-request } accept
	for _, icmpType := range []byte{3, 11, 12, 8} {
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{1}}, // ICMP
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 0, Len: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{icmpType}},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	}

	// udp dport 67 iifname <X> accept — DHCP Server (dnsmasq) on authorized LAN interfaces.
	// Must precede the generic drop loop below: nftables evaluates rules top-down and an
	// accept here terminates evaluation before the unconditional drop on port 67 is reached.
	for _, ifaceName := range dhcpServerIfaces {
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(ifaceName)},
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{17}}, // UDP
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(67 >> 8), byte(67 & 0xFF)}},
				localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenDhcpServerAccept)),
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	}

	// udp dport 68 iifname <X> accept — DHCP Client replies (offers/acks) on interfaces
	// configured with addressingMode "dhcp" (e.g. a DHCP-client WAN port). These replies are
	// frequently sent to the broadcast address, so they don't reliably match the ct
	// established/related entry created by the original DHCPDISCOVER and would otherwise be
	// caught by the generic port-68 drop below, leaving the interface unable to obtain a lease.
	for _, iface := range ifaces {
		if iface.AddressingMode != "dhcp" {
			continue
		}
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(iface.Name)},
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{17}}, // UDP
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(68 >> 8), byte(68 & 0xFF)}},
				localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenDhcpClientAccept)),
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	}

	// udp dport { 137, 138, 67, 68 } drop — 67 here still protects interfaces that are
	// NOT running DHCP Server (rogue/unsolicited DHCP traffic); authorized interfaces
	// were already accepted above. 68 here still protects interfaces that are NOT
	// configured as a DHCP client (unsolicited DHCP reply traffic); DHCP-client interfaces
	// were already accepted above.
	for _, port := range []uint16{137, 138, 67, 68} {
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{17}}, // UDP
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(port >> 8), byte(port & 0xFF)}},
				&expr.Verdict{Kind: expr.VerdictDrop},
			},
		})
	}

	// tcp dport { 139, 445 } drop
	for _, port := range []uint16{139, 445} {
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{6}}, // TCP
				&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(port >> 8), byte(port & 0xFF)}},
				&expr.Verdict{Kind: expr.VerdictDrop},
			},
		})
	}

	// fib daddr type broadcast drop
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: uint32ToBytes(3)}, // RTN_BROADCAST = 3
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})

	// jump pigate-not-local
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Verdict{Kind: expr.VerdictJump, Chain: "pigate-not-local"},
		},
	})

	// ip daddr 224.0.0.251 udp dport 5353 accept
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: net.ParseIP("224.0.0.251").To4()},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{17}},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(5353 >> 8), byte(5353 & 0xFF)}},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// ip daddr 239.255.255.250 udp dport 1900 accept
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: net.ParseIP("239.255.255.250").To4()},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{17}},
			&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{byte(1900 >> 8), byte(1900 & 0xFF)}},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// --- Section 2: Audit Point ---
	// This is the one log statement in input that deliberately stays on
	// printk/journald rather than moving to NFLOG: it fires for EVERY packet
	// reaching section 2 with no verdict, so sending it to the ring buffer
	// would duplicate every entry (once here, once at the real ACCEPT/DROP)
	// and flood the Local Traffic page with undecided packets — it's a
	// kernel-level debug tap, not a user-facing event (see plan §2.5). It was
	// previously unrated (the single biggest SD-card write source in this
	// file); add the same rate limit used elsewhere for log-only rules.
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Limit{Type: expr.LimitTypePkts, Rate: 3, Unit: expr.LimitTimeMinute, Burst: 10, Over: false},
			&expr.Log{
				Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
				Data: []byte("[PiGate] INP AUDIT : "),
			},
		},
	})

	// --- Section 3: Dynamic Accepts ---
	// Docker Compat Bypass rules in input
	if rf.dockerCompat {
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName("docker0")},
				localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenDockerAccept)),
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})

		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: inputChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Bitwise{
					SourceRegister: 1,
					DestRegister:   1,
					Len:            16,
					Mask:           append([]byte{0xff, 0xff, 0xff}, make([]byte, 13)...),
					Xor:            make([]byte, 16),
				},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: append([]byte("br-"), make([]byte, 13)...)},
				localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenDockerAccept)),
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	}

	// Admin Access rules per interface in input
	for _, iface := range ifaces {
		addAdminAccessRules(conn, table, inputChain, iface.Name, iface.AdminAccess)
	}

	// DNS Server (dnsmasq) access rules per interface in input
	addDNSServerAccessRules(conn, table, inputChain, dnsServerIfaces)

	// --- Section 3b: User input rules from the DB (Local-In Policy page) ---
	// MUST stay after Admin Access + DNS server accept above (section 3a) and
	// before the final drop log below — nftables is first-match, so a user
	// DROP rule can never precede (and therefore can never shadow) the
	// interface's own Admin Access accept, which is the structural guarantee
	// that a bad rule here cannot lock the operator out of the web UI/SSH
	// (plan section 2.2, Caution 8).
	addUserChainRules(conn, table, inputChain, model.PolicyChainInput, rules, addrsMap, svcsMap,
		"[PiGate] INP ACCEPT: ", "[PiGate] INP DROP  : ")

	// --- Section 4: Final Drop Log ---
	// Highest-volume log point in the whole file (catches every unsolicited
	// WAN packet/port-scan noise the earlier sections didn't accept), so it
	// gets the highest rate limit of the three log-only rules. No verdict
	// here — the drop itself comes from the chain's policy drop — so adding
	// a limit is safe (see buildRuleExpressions comment / Caution 2).
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Limit{Type: expr.LimitTypePkts, Rate: 10, Unit: expr.LimitTimeSecond, Burst: 20, Over: false},
			localLogExpr(withRuleToken("[PiGate] INP DROP  : ", SysTokenInputDefaultDrop)),
		},
	})

	// 5. Setup base "forward" chain
	forwardChain := conn.AddChain(&nftables.Chain{
		Name:     "forward",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookForward,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &policyDrop,
	})

	// ct state established,related accept in forward
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: forwardChain,
		Exprs: []expr.Any{
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: uint32ToBytes(6), Xor: uint32ToBytes(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: uint32ToBytes(0)},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// ct state invalid drop in forward
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: forwardChain,
		Exprs: []expr.Any{
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: uint32ToBytes(1), Xor: uint32ToBytes(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: uint32ToBytes(0)},
			&expr.Verdict{Kind: expr.VerdictDrop},
		},
	})

	// Docker Compat Bypass rules in forward
	if rf.dockerCompat {
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: forwardChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName("docker0")},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})

		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: forwardChain,
			Exprs: []expr.Any{
				&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
				&expr.Bitwise{
					SourceRegister: 1,
					DestRegister:   1,
					Len:            16,
					Mask:           append([]byte{0xff, 0xff, 0xff}, make([]byte, 13)...),
					Xor:            make([]byte, 16),
				},
				&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: append([]byte("br-"), make([]byte, 13)...)},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	}

	// Port-forward auto forward-accept rules.
	// A DNAT'd packet (dst rewritten to the internal host in prerouting) still
	// traverses this forward(filter) chain and would hit the final drop-log
	// unless explicitly accepted. We inject one accept per enabled port-forward
	// here — AFTER the docker-compat bypass but BEFORE the user policy rules — so
	// a broad user DROP rule can never shadow a port-forward the operator turned
	// on (disable is done on the entry itself). No fwmark 0x1 is set, so these
	// flows are NOT masqueraded by the policy-SNAT postrouting rule and the
	// internal server sees the external client's real source IP. See the
	// port-forward plan §2 and Caution 1/2.
	for _, pf := range portForwards {
		if !pf.Status {
			continue
		}
		exprs, err := buildPortForwardAcceptExprs(pf)
		if err != nil {
			log.Printf("[RealFirewall] Skip port-forward %q forward-accept: %v", pf.Name, err)
			continue
		}
		conn.AddRule(&nftables.Rule{
			Table: table,
			Chain: forwardChain,
			Exprs: exprs,
		})
	}

	// User rules in forward
	addUserChainRules(conn, table, forwardChain, model.PolicyChainForward, rules, addrsMap, svcsMap,
		"[PiGate] FWD ACCEPT: ", "[PiGate] FWD DROP  : ")

	// Final Drop Log in forward — also to the NFLOG group (see forwardLogExpr).
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: forwardChain,
		Exprs: []expr.Any{
			forwardLogExpr(withRuleToken("[PiGate] FWD DROP  : ", SysTokenForwardDefaultDrop)),
		},
	})

	// 5.5. Setup base "output" chain (new — Local-Out Policy page).
	// Unlike input/forward, output uses policy accept (default-allow): this is
	// a deliberate scope decision (plan section 0 "นอกขอบเขต") — strict egress
	// filtering (policy drop) needs allow-rules for DNS/NTP/DHCP client/apt/
	// dnsmasq upstream first, or the box cuts its own network access. Uses a
	// dedicated policyAccept var, NOT the policyDrop used by input/forward
	// above — accidentally reusing policyDrop here would drop 100% of the
	// box's own outgoing traffic the instant this is applied (Caution 8).
	policyAccept := nftables.ChainPolicyAccept
	outputChain := conn.AddChain(&nftables.Chain{
		Name:     "output",
		Table:    table,
		Type:     nftables.ChainTypeFilter,
		Hooknum:  nftables.ChainHookOutput,
		Priority: nftables.ChainPriorityFilter,
		Policy:   &policyAccept,
	})

	// ct state established,related accept — protects replies of sessions the
	// box itself opened (e.g. an admin's open web UI/SSH session) from being
	// cut by a DROP rule the user adds later (plan section 2.3).
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: outputChain,
		Exprs: []expr.Any{
			&expr.Ct{Key: expr.CtKeySTATE, Register: 1},
			&expr.Bitwise{SourceRegister: 1, DestRegister: 1, Len: 4, Mask: uint32ToBytes(6), Xor: uint32ToBytes(0)},
			&expr.Cmp{Op: expr.CmpOpNeq, Register: 1, Data: uint32ToBytes(0)},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// oifname "lo" accept
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: outputChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName("lo")},
			&expr.Verdict{Kind: expr.VerdictAccept},
		},
	})

	// meta nfproto ipv6 counter drop — see plan section 2.3.1 / Caution 13:
	// input/forward already fail-closed for IPv6 by accident (a protocol
	// offset bug that only matches the IPv4 header shape), but output is a
	// brand-new policy-accept chain with no such accident protecting it, and
	// user address objects can't express an IPv6 DROP rule to close the gap
	// themselves. Must come before any user output rule below so it can never
	// be shadowed by one. Factored into outputIPv6DropExprs so
	// policy_chain_test.go can assert this exact rule exists (Caution 13).
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: outputChain,
		Exprs: outputIPv6DropExprs(),
	})

	// User rules in output (Local-Out Policy page). No final drop log —
	// chain policy is accept, so anything not matched by a user DROP rule
	// above simply falls through to the implicit accept.
	addUserChainRules(conn, table, outputChain, model.PolicyChainOutput, rules, addrsMap, svcsMap,
		"[PiGate] OUT ACCEPT: ", "[PiGate] OUT DROP  : ")

	// 6. Setup NAT table and chain for policy-based source NAT.
	// Source NAT is now driven per firewall policy (the policy's "NAT" toggle),
	// not by interface Role. Policies with NAT enabled tag accepted packets with
	// fwmark 0x1 in the forward chain (see buildRuleExpressions); this single
	// postrouting rule masquerades every marked packet to its outgoing interface
	// address. This covers LAN→WAN and LAN-to-LAN NAT alike, since masquerade
	// always uses the address of the actual egress interface.
	natTable := conn.AddTable(&nftables.Table{
		Name:   "pigate_nat",
		Family: nftables.TableFamilyIPv4, // family ip
	})
	conn.FlushTable(natTable)

	natChain := conn.AddChain(&nftables.Chain{
		Name:     "postrouting",
		Table:    natTable,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPostrouting,
		Priority: nftables.ChainPriorityNATSource,
	})

	conn.AddRule(&nftables.Rule{
		Table: natTable,
		Chain: natChain,
		Exprs: []expr.Any{
			&expr.Meta{Key: expr.MetaKeyMARK, Register: 1},
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{0x01, 0x00, 0x00, 0x00}},
			&expr.Masq{},
		},
	})
	log.Printf("[RealFirewall] Configured policy-based source NAT (masquerade on fwmark 0x1)")

	// Prerouting DNAT chain for port-forwarding (FortiGate VIP).
	// This MUST be added in the same apply/flush pass as the postrouting chain
	// above: pigate_nat is flushed and rebuilt on every ApplyRules, so a separate
	// method would wipe one of the two chains (plan Caution 5). Priority
	// NATDest runs DNAT before the routing decision, so the rewritten internal
	// destination is what routing/forward see.
	dnatChain := conn.AddChain(&nftables.Chain{
		Name:     "prerouting",
		Table:    natTable,
		Type:     nftables.ChainTypeNAT,
		Hooknum:  nftables.ChainHookPrerouting,
		Priority: nftables.ChainPriorityNATDest,
	})

	dnatCount := 0
	for _, pf := range portForwards {
		if !pf.Status {
			continue
		}
		exprs, err := buildPortForwardDNATExprs(pf)
		if err != nil {
			log.Printf("[RealFirewall] Skip port-forward %q DNAT: %v", pf.Name, err)
			continue
		}
		conn.AddRule(&nftables.Rule{
			Table: natTable,
			Chain: dnatChain,
			Exprs: exprs,
		})
		dnatCount++
	}
	if dnatCount > 0 {
		log.Printf("[RealFirewall] Configured %d port-forward DNAT rule(s) in prerouting", dnatCount)
	}

	// Commit everything to the Linux Kernel
	if err := conn.Flush(); err != nil {
		log.Printf("[RealFirewall] Error committing rules to kernel: %v", err)
		return fmt.Errorf("failed to flush nftables rules: %w", err)
	}

	log.Printf("[RealFirewall] Successfully applied firewall rules to Linux kernel")
	return nil
}

// forwardLogExpr builds the nftables log expression used by the forward chain.
// Unlike the input-chain logs (which stay on printk / dmesg), forward-chain logs
// are directed to NFLOG group ForwardNflogGroup so the in-app listener can read
// them (real_traffic_log.go). The prefix travels in NFULA_PREFIX; snaplen copies
// only enough bytes to parse the IP + transport headers, not the whole payload.
func forwardLogExpr(logPrefix string) *expr.Log {
	return &expr.Log{
		Key:     (1 << unix.NFTA_LOG_GROUP) | (1 << unix.NFTA_LOG_PREFIX) | (1 << unix.NFTA_LOG_SNAPLEN),
		Group:   ForwardNflogGroup,
		Snaplen: ForwardNflogSnaplen,
		Data:    []byte(logPrefix),
	}
}

// localLogExpr is forwardLogExpr's twin for the input/output chains: it
// directs their ACCEPT/DROP packet logs to LocalNflogGroup (Local Traffic
// page) instead of printk/journald (SD card write), so no rate limiting is
// required here — unlike printk, NFLOG overflow is dropped at the socket/
// channel level in real_traffic_log.go without touching disk. See plan
// docs/ref/todo/traffic-log-pagination-and-local-traffic-plan.md §2.5.
func localLogExpr(logPrefix string) *expr.Log {
	return &expr.Log{
		Key:     (1 << unix.NFTA_LOG_GROUP) | (1 << unix.NFTA_LOG_PREFIX) | (1 << unix.NFTA_LOG_SNAPLEN),
		Group:   LocalNflogGroup,
		Snaplen: ForwardNflogSnaplen,
		Data:    []byte(logPrefix),
	}
}

// padInterfaceName pads interface name string to exactly 16 bytes for Netlink comparison
func padInterfaceName(name string) []byte {
	b := make([]byte, 16)
	copy(b, name)
	return b
}

// uint32ToBytes converts a uint32 value to 4 bytes in native byte order
func uint32ToBytes(val uint32) []byte {
	b := make([]byte, 4)
	binary.NativeEndian.PutUint32(b, val)
	return b
}

// portToBytes converts a port number to 2 big-endian bytes (network order),
// matching how ports are compared/loaded elsewhere in this file.
func portToBytes(p int) []byte {
	return []byte{byte(p >> 8), byte(p & 0xFF)}
}

// protoNumber maps a port-forward protocol string to its IP protocol number.
func protoNumber(proto string) (byte, error) {
	switch strings.ToLower(strings.TrimSpace(proto)) {
	case "tcp":
		return 6, nil
	case "udp":
		return 17, nil
	default:
		return 0, fmt.Errorf("unsupported protocol %q (expected tcp/udp)", proto)
	}
}

// parsePortSpec parses a "8080" or "8000-8010" spec into an inclusive range.
// single ports return start==end. Delegates to model.ParsePortSpec, the one
// canonical implementation shared with the dashboard traffic-detail
// categorizer (plan Caution 13) — do not reimplement this logic here.
func parsePortSpec(spec string) (start, end int, err error) {
	return model.ParsePortSpec(spec)
}

// dportMatchExprs builds the transport-header destination-port match for a
// single port or a range (payload @ transport header offset 2, len 2).
func dportMatchExprs(spec string) ([]expr.Any, error) {
	start, end, err := parsePortSpec(spec)
	if err != nil {
		return nil, err
	}
	load := &expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2}
	if start == end {
		return []expr.Any{
			load,
			&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: portToBytes(start)},
		}, nil
	}
	return []expr.Any{
		load,
		&expr.Cmp{Op: expr.CmpOpGte, Register: 1, Data: portToBytes(start)},
		&expr.Cmp{Op: expr.CmpOpLte, Register: 1, Data: portToBytes(end)},
	}, nil
}

// buildPortForwardDNATExprs builds the prerouting DNAT rule for one port-forward:
//
//	iifname==<ext> && fib daddr type local && <proto> dport==<extPort>
//	  => dnat to internalIP[:internalPort]
//
// The `fib daddr type local` guard is essential: without it, traffic merely
// transiting the external interface (destined elsewhere) would also be DNAT'd.
// When InternalPort is empty the port is kept (keep-port DNAT), which is the
// only supported shape for a port range (plan Caution 9).
func buildPortForwardDNATExprs(pf model.PortForward) ([]expr.Any, error) {
	protoVal, err := protoNumber(pf.Protocol)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(pf.InternalIP)).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid internal IPv4 %q", pf.InternalIP)
	}

	exprs := []expr.Any{
		// iifname == external interface
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(pf.InInterface)},
		// fib daddr type local (only DNAT packets addressed to this host)
		&expr.Fib{Register: 1, ResultADDRTYPE: true, FlagDADDR: true},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: uint32ToBytes(2)}, // RTN_LOCAL = 2
		// ip protocol == tcp/udp
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{protoVal}},
	}

	dport, err := dportMatchExprs(pf.ExternalPort)
	if err != nil {
		return nil, fmt.Errorf("externalPort: %w", err)
	}
	exprs = append(exprs, dport...)

	exprs = append(exprs, &expr.Counter{})

	// Destination address into reg 1.
	exprs = append(exprs, &expr.Immediate{Register: 1, Data: ip})

	internal := strings.TrimSpace(pf.InternalPort)
	if internal == "" {
		// keep-port DNAT: rewrite address only, conntrack preserves the port.
		exprs = append(exprs, &expr.NAT{
			Type:       expr.NATTypeDestNAT,
			Family:     unix.NFPROTO_IPV4,
			RegAddrMin: 1,
		})
		return exprs, nil
	}

	// Translated single port: dnat to internalIP:internalPort.
	start, end, err := parsePortSpec(pf.ExternalPort)
	if err != nil {
		return nil, fmt.Errorf("externalPort: %w", err)
	}
	if start != end {
		return nil, fmt.Errorf("port-range translation to a fixed internalPort is unsupported; leave internalPort empty to keep the port")
	}
	p, err := strconv.Atoi(internal)
	if err != nil || p < 1 || p > 65535 {
		return nil, fmt.Errorf("invalid internalPort %q", pf.InternalPort)
	}
	exprs = append(exprs, &expr.Immediate{Register: 2, Data: portToBytes(p)})
	exprs = append(exprs, &expr.NAT{
		Type:        expr.NATTypeDestNAT,
		Family:      unix.NFPROTO_IPV4,
		RegAddrMin:  1,
		RegProtoMin: 2,
	})
	return exprs, nil
}

// buildPortForwardAcceptExprs builds the forward-chain accept rule that lets a
// DNAT'd packet through (its dst is now internalIP:<port>):
//
//	iif==<ext> && ip daddr==internalIP && <proto> dport==<port> counter accept
//
// The matched dport is the *post-DNAT* port: internalPort when translated, or
// the (kept) external port spec otherwise.
func buildPortForwardAcceptExprs(pf model.PortForward) ([]expr.Any, error) {
	protoVal, err := protoNumber(pf.Protocol)
	if err != nil {
		return nil, err
	}
	ip := net.ParseIP(strings.TrimSpace(pf.InternalIP)).To4()
	if ip == nil {
		return nil, fmt.Errorf("invalid internal IPv4 %q", pf.InternalIP)
	}

	exprs := []expr.Any{
		// iifname == external interface
		&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(pf.InInterface)},
		// ip daddr == internal host
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 16, Len: 4},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: ip},
		// ip protocol == tcp/udp
		&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{protoVal}},
	}

	// Post-DNAT destination port: translated port (single) or kept external spec.
	portSpec := strings.TrimSpace(pf.InternalPort)
	if portSpec == "" {
		portSpec = pf.ExternalPort
	}
	dport, err := dportMatchExprs(portSpec)
	if err != nil {
		return nil, fmt.Errorf("internalPort: %w", err)
	}
	exprs = append(exprs, dport...)

	exprs = append(exprs, &expr.Counter{})
	exprs = append(exprs, &expr.Verdict{Kind: expr.VerdictAccept})
	return exprs, nil
}

func resolveService(name string, svcsMap map[string]model.ServiceObject) (model.ServiceObject, bool) {
	if s, ok := svcsMap[name]; ok {
		return s, true
	}
	parts := strings.Split(name, " ")
	if len(parts) > 0 {
		if s, ok := svcsMap[parts[0]]; ok {
			return s, true
		}
	}
	return model.ServiceObject{}, false
}

func buildIPMatchExpressions(name string, addrsMap map[string]model.AddressObject, offset uint32) ([]expr.Any, error) {
	addr, ok := addrsMap[name]
	if !ok {
		return nil, fmt.Errorf("address object %q not found", name)
	}

	var exprs []expr.Any
	switch addr.Type {
	case "subnet":
		val := strings.TrimSpace(addr.Value)
		if !strings.Contains(val, "/") {
			val += "/32"
		}
		_, ipNet, err := net.ParseCIDR(val)
		if err != nil {
			return nil, fmt.Errorf("invalid subnet value %q for %q: %w", addr.Value, name, err)
		}
		ipBytes := ipNet.IP.To4()
		if ipBytes == nil {
			return nil, fmt.Errorf("only IPv4 subnets are supported: %q", addr.Value)
		}
		maskBytes := []byte(ipNet.Mask)

		// Check if it is a /32 subnet. If so, direct equality match
		if ipNet.Mask[0] == 255 && ipNet.Mask[1] == 255 && ipNet.Mask[2] == 255 && ipNet.Mask[3] == 255 {
			exprs = append(exprs, &expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseNetworkHeader,
				Offset:       offset,
				Len:          4,
			})
			exprs = append(exprs, &expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     ipBytes,
			})
		} else {
			exprs = append(exprs, &expr.Payload{
				DestRegister: 1,
				Base:         expr.PayloadBaseNetworkHeader,
				Offset:       offset,
				Len:          4,
			})
			exprs = append(exprs, &expr.Bitwise{
				SourceRegister: 1,
				DestRegister:   1,
				Len:            4,
				Mask:           maskBytes,
				Xor:            []byte{0, 0, 0, 0},
			})
			exprs = append(exprs, &expr.Cmp{
				Op:       expr.CmpOpEq,
				Register: 1,
				Data:     ipBytes,
			})
		}

	case "range":
		parts := strings.Split(addr.Value, "-")
		if len(parts) != 2 {
			return nil, fmt.Errorf("invalid range value %q for %q", addr.Value, name)
		}
		startIP := net.ParseIP(strings.TrimSpace(parts[0])).To4()
		endIP := net.ParseIP(strings.TrimSpace(parts[1])).To4()
		if startIP == nil || endIP == nil {
			return nil, fmt.Errorf("invalid IP range %q", addr.Value)
		}

		exprs = append(exprs, &expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       offset,
			Len:          4,
		})
		exprs = append(exprs, &expr.Cmp{
			Op:       expr.CmpOpGte,
			Register: 1,
			Data:     startIP,
		})
		exprs = append(exprs, &expr.Cmp{
			Op:       expr.CmpOpLte,
			Register: 1,
			Data:     endIP,
		})

	case "fqdn":
		ips, err := net.LookupIP(addr.Value)
		if err != nil {
			log.Printf("[RealFirewall] Warning: failed to resolve FQDN %q: %v", addr.Value, err)
			return nil, err
		}

		var ipv4s []net.IP
		for _, ip := range ips {
			if ip4 := ip.To4(); ip4 != nil {
				ipv4s = append(ipv4s, ip4)
			}
		}

		if len(ipv4s) == 0 {
			return nil, fmt.Errorf("no IPv4 address found for FQDN %q", addr.Value)
		}

		log.Printf("[RealFirewall] Resolved FQDN %s to %s (matching first IP)", addr.Value, ipv4s[0])
		exprs = append(exprs, &expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       offset,
			Len:          4,
		})
		exprs = append(exprs, &expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     ipv4s[0],
		})
	}

	return exprs, nil
}

// buildRuleExpressions builds the nftables rule(s) for one (interface,
// address, service, protocol) combination of a user PolicyRule, for any of
// the three chains (docs/ref/todo/input-output-chain-firewall-plan.md
// section 2.4). chain controls three things:
//  1. iifname/oifname are only meaningful for their respective direction —
//     outInterface is ignored on the input chain, inInterface is ignored on
//     the output chain (defense in depth; model.ValidatePolicyRule already
//     forces the unused field to "" / "ALL" before a rule reaches here).
//  2. Only the forward chain ever sets the fwmark 0x1 used for policy-based
//     source NAT (Caution: "ห้ามใส่ fwmark/NAT ในขา input/output" — NAT only
//     makes sense for traffic actually being routed through the box).
//  3. Logging: forward-chain logs go to NFLOG group ForwardNflogGroup
//     (forwardLogExpr) and input/output-chain logs go to NFLOG group
//     LocalNflogGroup (localLogExpr) — both write to an in-RAM ring buffer,
//     not journald/SD card, so neither needs rate limiting and both can
//     safely share one rule with the counter+verdict. Caution 5/Caution 2
//     (docs/ref/todo/input-output-chain-firewall-plan.md,
//     traffic-log-pagination-and-local-traffic-plan.md §2.6) still applies
//     as a general rule going forward: a rate-limited `limit ... <verdict>`
//     rule only applies its verdict to packets that pass the limiter,
//     silently letting the rest fall through to the next rule — so a
//     log-only rule that DOES carry an expr.Limit (e.g. the three
//     structural log points in real_firewall.go: notLocal 3.4, AUDIT, input
//     final drop) must never also carry an expr.Verdict.
//
// Returns one []expr.Any per nftables rule to add, in order.
// outputIPv6DropExprs returns the fixed rule expressions for "meta nfproto
// ipv6 counter drop", the safety line added to the output chain right after
// the two keep-alive rules and before any user output rule (plan section
// 2.3.1, Caution 13). Factored out so it can be asserted directly in
// policy_chain_test.go without needing a real nftables/netlink connection.
func outputIPv6DropExprs() []expr.Any {
	return []expr.Any{
		&expr.Meta{Key: expr.MetaKeyNFPROTO, Register: 1},
		&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{unix.NFPROTO_IPV6}},
		&expr.Counter{},
		&expr.Verdict{Kind: expr.VerdictDrop},
	}
}

// addUserChainRules expands every enabled PolicyRule in rules that targets
// chainName into one or more nftables rules appended to nfChain, via
// buildRuleExpressions. Used for all three chains (forward/input/output) —
// see docs/ref/todo/input-output-chain-firewall-plan.md section 2.4.
func addUserChainRules(
	conn *nftables.Conn,
	table *nftables.Table,
	nfChain *nftables.Chain,
	chainName string,
	rules []model.PolicyRule,
	addrsMap map[string]model.AddressObject,
	svcsMap map[string]model.ServiceObject,
	acceptLogPrefix, dropLogPrefix string,
) {
	for _, r := range rules {
		if !r.Status || r.Chain != chainName {
			continue
		}

		// Tag every nft rule this DB rule expands into with its DB id, so the
		// traffic-detail collector can sum nftables' own per-rule counter back
		// to the DB rule it belongs to (docs/ref/todo/dashboard-traffic-detail-plan.md
		// §2.3). Purely additive: UserData is a separate rule attribute, not
		// part of Exprs, so it cannot change match/verdict behavior or rule
		// count/order.
		ruleUserData := userdata.AppendString(nil, userdata.TypeComment, r.ID)

		sources := r.Source
		if len(sources) == 0 {
			sources = []string{"ALL"}
		}
		destinations := r.Destination
		if len(destinations) == 0 {
			destinations = []string{"ALL"}
		}
		services := r.Service
		if len(services) == 0 {
			services = []string{"ALL"}
		}

		for _, src := range sources {
			for _, dest := range destinations {
				for _, svc := range services {
					var protocols []string
					if svc == "ALL" {
						protocols = []string{"ALL"}
					} else if s, ok := resolveService(svc, svcsMap); ok {
						if strings.ToUpper(s.Protocol) == "TCP/UDP" {
							protocols = []string{"TCP", "UDP"}
						} else {
							protocols = []string{strings.ToUpper(s.Protocol)}
						}
					} else {
						protocols = []string{"ALL"}
					}

					for _, proto := range protocols {
						logPrefix := acceptLogPrefix
						if r.Action == "DROP" {
							logPrefix = dropLogPrefix
						}
						// Tag the log prefix with this DB rule's id, so a matching NFLOG
						// entry can be traced back to the exact PolicyRule that produced
						// it (see withRuleToken doc comment above). Omitted silently if
						// r.ID fails the whitelist or the prefix would overflow.
						logPrefix = withRuleToken(logPrefix, r.ID)
						ruleSets, err := buildRuleExpressions(
							chainName,
							r.InInterface, r.OutInterface,
							src, dest, svc, proto,
							r.Action, r.Log, r.Nat, logPrefix,
							addrsMap, svcsMap,
						)
						if err != nil {
							log.Printf("[RealFirewall] Skip %s rule %q combination (%s,%s,%s): %v", chainName, r.Name, src, dest, svc, err)
							continue
						}
						for _, exprs := range ruleSets {
							conn.AddRule(&nftables.Rule{
								Table:    table,
								Chain:    nfChain,
								Exprs:    exprs,
								UserData: ruleUserData,
							})
						}
					}
				}
			}
		}
	}
}

func buildRuleExpressions(
	chain string,
	inInterface, outInterface string,
	srcName, destName string,
	svcName, proto string,
	action string,
	logEnabled bool,
	nat bool,
	logPrefix string,
	addrsMap map[string]model.AddressObject,
	svcsMap map[string]model.ServiceObject,
) ([][]expr.Any, error) {
	// Chain-scoped interface fields: input has no meaningful egress
	// interface, output has no meaningful ingress interface.
	effInInterface, effOutInterface := inInterface, outInterface
	if chain == model.PolicyChainInput {
		effOutInterface = ""
	}
	if chain == model.PolicyChainOutput {
		effInInterface = ""
	}

	var exprs []expr.Any

	// 1. Input Interface
	if effInInterface != "" && effInInterface != "ALL" {
		exprs = append(exprs, &expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1})
		exprs = append(exprs, &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(effInInterface)})
	}

	// 2. Output Interface
	if effOutInterface != "" && effOutInterface != "ALL" {
		exprs = append(exprs, &expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1})
		exprs = append(exprs, &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(effOutInterface)})
	}

	// 3. Source IP
	if srcName != "" && srcName != "ALL" {
		srcExprs, err := buildIPMatchExpressions(srcName, addrsMap, 12)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, srcExprs...)
	}

	// 4. Destination IP
	if destName != "" && destName != "ALL" {
		destExprs, err := buildIPMatchExpressions(destName, addrsMap, 16)
		if err != nil {
			return nil, err
		}
		exprs = append(exprs, destExprs...)
	}

	// 5. Service / Protocol
	if svcName != "" && svcName != "ALL" {
		svc, ok := resolveService(svcName, svcsMap)
		if !ok {
			return nil, fmt.Errorf("service object %q not found", svcName)
		}

		var protoVal byte
		switch proto {
		case "TCP":
			protoVal = 6
		case "UDP":
			protoVal = 17
		case "ICMP":
			protoVal = 1
		default:
			switch strings.ToUpper(svc.Protocol) {
			case "TCP":
				protoVal = 6
			case "UDP":
				protoVal = 17
			case "ICMP":
				protoVal = 1
			default:
				return nil, fmt.Errorf("unsupported protocol %q for service %q", svc.Protocol, svcName)
			}
		}

		// Match IP protocol
		exprs = append(exprs, &expr.Payload{
			DestRegister: 1,
			Base:         expr.PayloadBaseNetworkHeader,
			Offset:       9,
			Len:          1,
		})
		exprs = append(exprs, &expr.Cmp{
			Op:       expr.CmpOpEq,
			Register: 1,
			Data:     []byte{protoVal},
		})

		if protoVal != 1 { // Non-ICMP, check port
			portStr := strings.TrimSpace(svc.Port)
			if portStr != "" && portStr != "-" && portStr != "1-65535" {
				parts := strings.Split(portStr, "-")
				if len(parts) == 1 {
					portNum, err := strconv.Atoi(parts[0])
					if err != nil {
						return nil, fmt.Errorf("invalid port %q: %w", parts[0], err)
					}
					portBytes := []byte{byte(portNum >> 8), byte(portNum & 0xFF)}

					exprs = append(exprs, &expr.Payload{
						DestRegister: 1,
						Base:         expr.PayloadBaseTransportHeader,
						Offset:       2,
						Len:          2,
					})
					exprs = append(exprs, &expr.Cmp{
						Op:       expr.CmpOpEq,
						Register: 1,
						Data:     portBytes,
					})
				} else if len(parts) == 2 {
					startPort, err := strconv.Atoi(strings.TrimSpace(parts[0]))
					if err != nil {
						return nil, fmt.Errorf("invalid start port %q: %w", parts[0], err)
					}
					endPort, err := strconv.Atoi(strings.TrimSpace(parts[1]))
					if err != nil {
						return nil, fmt.Errorf("invalid end port %q: %w", parts[1], err)
					}
					startBytes := []byte{byte(startPort >> 8), byte(startPort & 0xFF)}
					endBytes := []byte{byte(endPort >> 8), byte(endPort & 0xFF)}

					exprs = append(exprs, &expr.Payload{
						DestRegister: 1,
						Base:         expr.PayloadBaseTransportHeader,
						Offset:       2,
						Len:          2,
					})
					exprs = append(exprs, &expr.Cmp{
						Op:       expr.CmpOpGte,
						Register: 1,
						Data:     startBytes,
					})
					exprs = append(exprs, &expr.Cmp{
						Op:       expr.CmpOpLte,
						Register: 1,
						Data:     endBytes,
					})
				}
			}
		}
	}

	verdictExpr := func() expr.Any {
		if action == "ACCEPT" {
			return &expr.Verdict{Kind: expr.VerdictAccept}
		}
		return &expr.Verdict{Kind: expr.VerdictDrop}
	}

	if chain == model.PolicyChainForward {
		// Forward chain: counter, then (if enabled) an NFLOG log expr — NFLOG
		// writes to an in-RAM ring buffer, not journald/SD card, so no rate
		// limiting is required and combining log+verdict in one rule is
		// safe. Then the fwmark for policy-based source NAT, then verdict.
		fwdExprs := append([]expr.Any{}, exprs...)
		fwdExprs = append(fwdExprs, &expr.Counter{})
		if logEnabled {
			fwdExprs = append(fwdExprs, forwardLogExpr(logPrefix))
		}
		// Source NAT mark (policy-based NAT, forward chain only — Caution:
		// "ห้ามใส่ fwmark/NAT ในขา input/output"). When the policy has NAT
		// enabled and accepts the traffic, tag the packet with fwmark 0x1;
		// the pigate_nat postrouting chain masquerades every packet carrying
		// this mark to the outgoing interface address ("Use Outgoing
		// Interface Address"). Only meaningful on ACCEPT — a DROPped packet
		// never reaches postrouting, so we skip the mark for anything else.
		if nat && action == "ACCEPT" {
			fwdExprs = append(fwdExprs, &expr.Immediate{Register: 1, Data: []byte{0x01, 0x00, 0x00, 0x00}})
			fwdExprs = append(fwdExprs, &expr.Meta{Key: expr.MetaKeyMARK, SourceRegister: true, Register: 1})
		}
		fwdExprs = append(fwdExprs, verdictExpr())
		return [][]expr.Any{fwdExprs}, nil
	}

	// input/output chain: now that the log goes to NFLOG group
	// LocalNflogGroup (in-RAM, no SD card write) instead of printk, there is
	// no need to rate-limit it, so it can share a single rule with the
	// counter+verdict exactly like the forward branch above (plan §2.6). No
	// fwmark/NAT here (input/output are never subject to policy-based
	// source NAT — Caution: "ห้ามใส่ fwmark/NAT ในขา input/output").
	localExprs := append([]expr.Any{}, exprs...)
	localExprs = append(localExprs, &expr.Counter{})
	if logEnabled {
		localExprs = append(localExprs, localLogExpr(logPrefix))
	}
	localExprs = append(localExprs, verdictExpr())
	return [][]expr.Any{localExprs}, nil
}

func addAdminAccessRules(
	conn *nftables.Conn,
	table *nftables.Table,
	chain *nftables.Chain,
	ifaceName string,
	adminAccess []string,
) {
	for _, access := range adminAccess {
		access = strings.ToUpper(strings.TrimSpace(access))
		if access == "" {
			continue
		}

		switch access {
		case "PING":
			conn.AddRule(&nftables.Rule{
				Table: table,
				Chain: chain,
				Exprs: []expr.Any{
					&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(ifaceName)},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{1}}, // ICMP
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 0, Len: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{8}}, // Echo request
					localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenAdminPing)),
					&expr.Verdict{Kind: expr.VerdictAccept},
				},
			})

		case "HTTP":
			ports := []uint16{80, 2479}
			for _, port := range ports {
				portBytes := []byte{byte(port >> 8), byte(port & 0xFF)}
				conn.AddRule(&nftables.Rule{
					Table: table,
					Chain: chain,
					Exprs: []expr.Any{
						&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
						&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(ifaceName)},
						&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
						&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{6}}, // TCP
						&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
						&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: portBytes},
						localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenAdminHTTP)),
						&expr.Verdict{Kind: expr.VerdictAccept},
					},
				})
			}

		case "HTTPS":
			portBytes := []byte{byte(443 >> 8), byte(443 & 0xFF)}
			conn.AddRule(&nftables.Rule{
				Table: table,
				Chain: chain,
				Exprs: []expr.Any{
					&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(ifaceName)},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{6}}, // TCP
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: portBytes},
					localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenAdminHTTPS)),
					&expr.Verdict{Kind: expr.VerdictAccept},
				},
			})

		case "SSH":
			portBytes := []byte{byte(22 >> 8), byte(22 & 0xFF)}
			conn.AddRule(&nftables.Rule{
				Table: table,
				Chain: chain,
				Exprs: []expr.Any{
					&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(ifaceName)},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{6}}, // TCP
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: portBytes},
					localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenAdminSSH)),
					&expr.Verdict{Kind: expr.VerdictAccept},
				},
			})
		}
	}
}

// addDNSServerAccessRules opens TCP+UDP port 53 (DNS) on interfaces where the local
// DNS Server (dnsmasq) is configured to listen, per dns_server_settings.
func addDNSServerAccessRules(
	conn *nftables.Conn,
	table *nftables.Table,
	chain *nftables.Chain,
	dnsServerIfaces []string,
) {
	portBytes := []byte{byte(53 >> 8), byte(53 & 0xFF)}
	for _, ifaceName := range dnsServerIfaces {
		for _, protoVal := range []byte{6, 17} { // TCP, UDP
			conn.AddRule(&nftables.Rule{
				Table: table,
				Chain: chain,
				Exprs: []expr.Any{
					&expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(ifaceName)},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseNetworkHeader, Offset: 9, Len: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: []byte{protoVal}},
					&expr.Payload{DestRegister: 1, Base: expr.PayloadBaseTransportHeader, Offset: 2, Len: 2},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: portBytes},
					localLogExpr(withRuleToken("[PiGate] INP ACCEPT: ", SysTokenDNSServerAccept)),
					&expr.Verdict{Kind: expr.VerdictAccept},
				},
			})
		}
	}
}
