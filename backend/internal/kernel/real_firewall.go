package kernel

import (
	"encoding/binary"
	"fmt"
	"log"
	"net"
	"pigate/internal/model"
	"strconv"
	"strings"

	"github.com/google/nftables"
	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

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
) error {
	log.Printf("[RealFirewall] Applying %d rules to Linux kernel via Netlink (Docker Compatibility: %t, Addresses: %d, Services: %d)",
		len(rules), rf.dockerCompat, len(addrs), len(svcs))

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

	// Rule 3.4: limit rate 3/minute burst 10 packets log prefix "[PiGate]  INP DROP  : "
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: notLocalChain,
		Exprs: []expr.Any{
			&expr.Limit{Type: expr.LimitTypePkts, Rate: 3, Unit: expr.LimitTimeMinute, Burst: 10, Over: false},
			&expr.Log{
				Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
				Data: []byte("[PiGate]  INP DROP  : "),
			},
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

	// udp dport { 137, 138, 67, 68 } drop
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
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
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
				&expr.Log{
					Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
					Data: []byte("[PiGate] INP ACCEPT: "),
				},
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
				&expr.Log{
					Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
					Data: []byte("[PiGate] INP ACCEPT: "),
				},
				&expr.Verdict{Kind: expr.VerdictAccept},
			},
		})
	}

	// Admin Access rules per interface in input
	for _, iface := range ifaces {
		addAdminAccessRules(conn, table, inputChain, iface.Name, iface.AdminAccess)
	}

	// --- Section 4: Final Drop Log ---
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: inputChain,
		Exprs: []expr.Any{
			&expr.Log{
				Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
				Data: []byte("[PiGate] INP DROP  : "),
			},
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

	// User rules in forward
	for _, r := range rules {
		if !r.Status {
			continue
		}

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
						logPrefix := "[PiGate] FWD ACCEPT: "
						if r.Action == "DROP" {
							logPrefix = "[PiGate] FWD DROP  : "
						}
						exprs, err := buildRuleExpressions(
							r.InInterface, r.OutInterface,
							src, dest, svc, proto,
							r.Action, r.Log, logPrefix,
							addrsMap, svcsMap,
						)
						if err != nil {
							log.Printf("[RealFirewall] Skip forward rule %q combination (%s,%s,%s): %v", r.Name, src, dest, svc, err)
							continue
						}
						conn.AddRule(&nftables.Rule{
							Table: table,
							Chain: forwardChain,
							Exprs: exprs,
						})
					}
				}
			}
		}
	}

	// Final Drop Log in forward
	conn.AddRule(&nftables.Rule{
		Table: table,
		Chain: forwardChain,
		Exprs: []expr.Any{
			&expr.Log{
				Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
				Data: []byte("[PiGate] FWD DROP  : "),
			},
		},
	})

	// 6. Setup NAT table and chain for masquerading on WAN interfaces
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

	for _, iface := range ifaces {
		if strings.ToUpper(iface.Role) == "WAN" {
			conn.AddRule(&nftables.Rule{
				Table: natTable,
				Chain: natChain,
				Exprs: []expr.Any{
					&expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1},
					&expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(iface.Name)},
					&expr.Masq{},
				},
			})
			log.Printf("[RealFirewall] Configured NAT masquerade on WAN interface: %s", iface.Name)
		}
	}

	// Commit everything to the Linux Kernel
	if err := conn.Flush(); err != nil {
		log.Printf("[RealFirewall] Error committing rules to kernel: %v", err)
		return fmt.Errorf("failed to flush nftables rules: %w", err)
	}

	log.Printf("[RealFirewall] Successfully applied firewall rules to Linux kernel")
	return nil
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

func buildRuleExpressions(
	inInterface, outInterface string,
	srcName, destName string,
	svcName, proto string,
	action string,
	logEnabled bool,
	logPrefix string,
	addrsMap map[string]model.AddressObject,
	svcsMap map[string]model.ServiceObject,
) ([]expr.Any, error) {
	var exprs []expr.Any

	// 1. Input Interface
	if inInterface != "" && inInterface != "ALL" {
		exprs = append(exprs, &expr.Meta{Key: expr.MetaKeyIIFNAME, Register: 1})
		exprs = append(exprs, &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(inInterface)})
	}

	// 2. Output Interface
	if outInterface != "" && outInterface != "ALL" {
		exprs = append(exprs, &expr.Meta{Key: expr.MetaKeyOIFNAME, Register: 1})
		exprs = append(exprs, &expr.Cmp{Op: expr.CmpOpEq, Register: 1, Data: padInterfaceName(outInterface)})
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

	// 6. Add Counter
	exprs = append(exprs, &expr.Counter{})

	// 7. Add Log (if enabled)
	if logEnabled {
		exprs = append(exprs, &expr.Log{
			Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
			Data: []byte(logPrefix),
		})
	}

	// 8. Add Verdict
	if action == "ACCEPT" {
		exprs = append(exprs, &expr.Verdict{Kind: expr.VerdictAccept})
	} else {
		exprs = append(exprs, &expr.Verdict{Kind: expr.VerdictDrop})
	}

	return exprs, nil
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
					&expr.Log{
						Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
						Data: []byte("[PiGate] INP ACCEPT: "),
					},
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
						&expr.Log{
							Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
							Data: []byte("[PiGate] INP ACCEPT: "),
						},
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
					&expr.Log{
						Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
						Data: []byte("[PiGate] INP ACCEPT: "),
					},
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
					&expr.Log{
						Key:  uint32(1 << unix.NFTA_LOG_PREFIX),
						Data: []byte("[PiGate] INP ACCEPT: "),
					},
					&expr.Verdict{Kind: expr.VerdictAccept},
				},
			})
		}
	}
}
