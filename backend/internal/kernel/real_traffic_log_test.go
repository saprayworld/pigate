package kernel

import (
	"testing"

	"pigate/internal/model"

	nflog "github.com/florianl/go-nflog/v2"
)

// nflogAttrFixture builds an NFLOG Attribute with the given payload and prefix
// pointers, for exercising parseNflogAttr without a live netlink socket.
func nflogAttrFixture(payload *[]byte, prefix *string) nflog.Attribute {
	return nflog.Attribute{Payload: payload, Prefix: prefix}
}

// stubResolveIface is a deterministic interface-index resolver for tests, so
// parseNflogAttr can be exercised without touching real kernel interfaces.
func stubResolveIface(index *uint32) string {
	if index == nil {
		return "-"
	}
	switch *index {
	case 2:
		return "eth0"
	case 3:
		return "eth1"
	}
	return "-"
}

// ipv4Packet builds a minimal 20-byte IPv4 header (IHL=5, no options) followed
// by an optional transport header, for parser fixtures.
func ipv4Packet(proto byte, src, dst [4]byte, transport []byte) []byte {
	pkt := make([]byte, 20)
	pkt[0] = 0x45 // version 4, IHL 5
	pkt[9] = proto
	copy(pkt[12:16], src[:])
	copy(pkt[16:20], dst[:])
	return append(pkt, transport...)
}

// ipv6Packet builds a minimal 40-byte IPv6 header followed by an optional
// transport header.
func ipv6Packet(nextHdr byte, src, dst [16]byte, transport []byte) []byte {
	pkt := make([]byte, 40)
	pkt[0] = 0x60 // version 6
	pkt[6] = nextHdr
	copy(pkt[8:24], src[:])
	copy(pkt[24:40], dst[:])
	return append(pkt, transport...)
}

// tcpUDPHeader returns a 4-byte transport header with the given src/dst ports.
func tcpUDPHeader(srcPort, dstPort uint16) []byte {
	return []byte{
		byte(srcPort >> 8), byte(srcPort & 0xFF),
		byte(dstPort >> 8), byte(dstPort & 0xFF),
	}
}

func TestParsePacketHeader(t *testing.T) {
	tests := []struct {
		name      string
		pkt       []byte
		wantSrc   string
		wantDest  string
		wantProto string
		wantPort  string
	}{
		{
			name:      "IPv4 TCP dport 443",
			pkt:       ipv4Packet(6, [4]byte{192, 168, 1, 10}, [4]byte{8, 8, 8, 8}, tcpUDPHeader(51000, 443)),
			wantSrc:   "192.168.1.10",
			wantDest:  "8.8.8.8",
			wantProto: "TCP",
			wantPort:  "443",
		},
		{
			name:      "IPv4 UDP dport 53",
			pkt:       ipv4Packet(17, [4]byte{192, 168, 1, 20}, [4]byte{1, 1, 1, 1}, tcpUDPHeader(40000, 53)),
			wantSrc:   "192.168.1.20",
			wantDest:  "1.1.1.1",
			wantProto: "UDP",
			wantPort:  "53",
		},
		{
			name:      "IPv4 ICMP no port",
			pkt:       ipv4Packet(1, [4]byte{10, 0, 0, 1}, [4]byte{10, 0, 0, 2}, nil),
			wantSrc:   "10.0.0.1",
			wantDest:  "10.0.0.2",
			wantProto: "ICMP",
			wantPort:  "-",
		},
		{
			name: "IPv6 TCP dport 80",
			pkt: ipv6Packet(6,
				[16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x01},
				[16]byte{0x20, 0x01, 0x0d, 0xb8, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0x02},
				tcpUDPHeader(52000, 80)),
			wantSrc:   "2001:db8::1",
			wantDest:  "2001:db8::2",
			wantProto: "TCP",
			wantPort:  "80",
		},
		{
			name:      "Truncated IPv4 header (payload shorter than 20 bytes)",
			pkt:       []byte{0x45, 0x00, 0x00},
			wantSrc:   "-",
			wantDest:  "-",
			wantProto: "-",
			wantPort:  "-",
		},
		{
			name:      "IPv4 TCP but transport truncated by snaplen",
			pkt:       ipv4Packet(6, [4]byte{192, 168, 1, 30}, [4]byte{9, 9, 9, 9}, []byte{0x01}),
			wantSrc:   "192.168.1.30",
			wantDest:  "9.9.9.9",
			wantProto: "TCP",
			wantPort:  "-", // not enough bytes for the port field → left as "-"
		},
		{
			name:      "Empty payload",
			pkt:       []byte{},
			wantSrc:   "-",
			wantDest:  "-",
			wantProto: "-",
			wantPort:  "-",
		},
		{
			name:      "Unknown IP version nibble",
			pkt:       []byte{0x70, 0x00, 0x00, 0x00},
			wantSrc:   "-",
			wantDest:  "-",
			wantProto: "-",
			wantPort:  "-",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePacketHeader(tt.pkt)
			if got.Src != tt.wantSrc {
				t.Errorf("Src = %q, want %q", got.Src, tt.wantSrc)
			}
			if got.Dest != tt.wantDest {
				t.Errorf("Dest = %q, want %q", got.Dest, tt.wantDest)
			}
			if got.Proto != tt.wantProto {
				t.Errorf("Proto = %q, want %q", got.Proto, tt.wantProto)
			}
			if got.Port != tt.wantPort {
				t.Errorf("Port = %q, want %q", got.Port, tt.wantPort)
			}
		})
	}
}

func TestParseNflogAttr(t *testing.T) {
	payload := ipv4Packet(6, [4]byte{192, 168, 1, 10}, [4]byte{8, 8, 8, 8}, tcpUDPHeader(51000, 443))

	t.Run("DROP prefix", func(t *testing.T) {
		prefix := "[PiGate] FWD DROP  : "
		entry, ok := parseNflogAttr(nflogAttrFixture(&payload, &prefix), stubResolveIface, model.PolicyChainForward)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if entry.Action != "DROP" {
			t.Errorf("Action = %q, want DROP", entry.Action)
		}
		if entry.Chain != model.PolicyChainForward {
			t.Errorf("Chain = %q, want forward", entry.Chain)
		}
	})

	t.Run("ACCEPT prefix", func(t *testing.T) {
		prefix := "[PiGate] FWD ACCEPT: "
		entry, ok := parseNflogAttr(nflogAttrFixture(&payload, &prefix), stubResolveIface, model.PolicyChainForward)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if entry.Action != "PASS" {
			t.Errorf("Action = %q, want PASS", entry.Action)
		}
		if entry.Chain != model.PolicyChainForward {
			t.Errorf("Chain = %q, want forward", entry.Chain)
		}
	})

	t.Run("nil payload", func(t *testing.T) {
		prefix := "[PiGate] FWD DROP  : "
		if _, ok := parseNflogAttr(nflogAttrFixture(nil, &prefix), stubResolveIface, model.PolicyChainForward); ok {
			t.Error("expected ok=false for nil payload")
		}
	})

	t.Run("resolves indev/outdev to interface names", func(t *testing.T) {
		prefix := "[PiGate] FWD ACCEPT: "
		in, out := uint32(2), uint32(3)
		attr := nflogAttrFixture(&payload, &prefix)
		attr.InDev = &in
		attr.OutDev = &out
		entry, ok := parseNflogAttr(attr, stubResolveIface, model.PolicyChainForward)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if entry.InIface != "eth0" {
			t.Errorf("InIface = %q, want eth0", entry.InIface)
		}
		if entry.OutIface != "eth1" {
			t.Errorf("OutIface = %q, want eth1", entry.OutIface)
		}
	})

	t.Run("absent indev/outdev yields dash", func(t *testing.T) {
		prefix := "[PiGate] FWD DROP  : "
		entry, ok := parseNflogAttr(nflogAttrFixture(&payload, &prefix), stubResolveIface, model.PolicyChainForward)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if entry.InIface != "-" || entry.OutIface != "-" {
			t.Errorf("InIface/OutIface = %q/%q, want -/-", entry.InIface, entry.OutIface)
		}
	})

	// New cases per docs/ref/todo/traffic-log-pagination-and-local-traffic-plan.md
	// §2.4: input/output prefixes, including the inconsistent double-space
	// "[PiGate]  INP DROP  : " variant (Caution 3), and an unrecognized/absent
	// prefix falling back to defaultChain.
	prefixCases := []struct {
		name         string
		prefix       string
		defaultChain string
		wantChain    string
		wantAction   string
		wantReason   string
	}{
		{"INP ACCEPT", "[PiGate] INP ACCEPT: ", model.PolicyChainInput, model.PolicyChainInput, "PASS", "Allowed (local-in)"},
		{"INP DROP single space", "[PiGate] INP DROP  : ", model.PolicyChainInput, model.PolicyChainInput, "DROP", "Blocked (local-in)"},
		{"INP DROP double space", "[PiGate]  INP DROP  : ", model.PolicyChainInput, model.PolicyChainInput, "DROP", "Blocked (local-in)"},
		{"OUT ACCEPT", "[PiGate] OUT ACCEPT: ", model.PolicyChainOutput, model.PolicyChainOutput, "PASS", "Allowed (local-out)"},
		{"OUT DROP", "[PiGate] OUT DROP  : ", model.PolicyChainOutput, model.PolicyChainOutput, "DROP", "Blocked (local-out)"},
		{"FWD ACCEPT", "[PiGate] FWD ACCEPT: ", model.PolicyChainForward, model.PolicyChainForward, "PASS", "Allowed (forward)"},
		{"FWD DROP", "[PiGate] FWD DROP  : ", model.PolicyChainForward, model.PolicyChainForward, "DROP", "Blocked (forward)"},
	}
	for _, tc := range prefixCases {
		t.Run(tc.name, func(t *testing.T) {
			prefix := tc.prefix
			entry, ok := parseNflogAttr(nflogAttrFixture(&payload, &prefix), stubResolveIface, tc.defaultChain)
			if !ok {
				t.Fatal("expected ok=true")
			}
			if entry.Chain != tc.wantChain {
				t.Errorf("Chain = %q, want %q", entry.Chain, tc.wantChain)
			}
			if entry.Action != tc.wantAction {
				t.Errorf("Action = %q, want %q", entry.Action, tc.wantAction)
			}
			if entry.Reason != tc.wantReason {
				t.Errorf("Reason = %q, want %q", entry.Reason, tc.wantReason)
			}
		})
	}

	t.Run("missing prefix falls back to defaultChain", func(t *testing.T) {
		entry, ok := parseNflogAttr(nflogAttrFixture(&payload, nil), stubResolveIface, model.PolicyChainInput)
		if !ok {
			t.Fatal("expected ok=true")
		}
		if entry.Chain != model.PolicyChainInput {
			t.Errorf("Chain = %q, want input (defaultChain fallback)", entry.Chain)
		}
		if entry.Action != "PASS" {
			t.Errorf("Action = %q, want PASS (unrecognized -> logged, not dropped)", entry.Action)
		}
	})
}
