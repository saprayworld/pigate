package kernel

import (
	"bytes"
	"net"
	"testing"

	"pigate/internal/model"

	"github.com/google/nftables/expr"
	"golang.org/x/sys/unix"
)

func TestBuildPortForwardDNATExprs_SingleTranslated(t *testing.T) {
	pf := model.PortForward{
		Name: "web", InInterface: "eth0", ExternalPort: "8080",
		Protocol: "tcp", InternalIP: "192.168.1.10", InternalPort: "80", Status: true,
	}
	exprs, err := buildPortForwardDNATExprs(pf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Must guard with `fib daddr type local` so transit traffic isn't DNAT'd.
	if !hasLocalFibGuard(exprs) {
		t.Errorf("DNAT rule missing `fib daddr type local` guard")
	}

	nat := findNAT(exprs)
	if nat == nil {
		t.Fatalf("no NAT expression found")
	}
	if nat.Type != expr.NATTypeDestNAT {
		t.Errorf("NAT type = %v, want DestNAT", nat.Type)
	}
	if nat.Family != unix.NFPROTO_IPV4 {
		t.Errorf("NAT family = %d, want IPv4", nat.Family)
	}
	if nat.RegAddrMin == 0 || nat.RegProtoMin == 0 {
		t.Errorf("translated DNAT must set both RegAddrMin and RegProtoMin, got addr=%d proto=%d", nat.RegAddrMin, nat.RegProtoMin)
	}

	// The internal IP must appear as an Immediate loaded into the addr register.
	wantIP := net.ParseIP("192.168.1.10").To4()
	if !hasImmediate(exprs, nat.RegAddrMin, wantIP) {
		t.Errorf("no Immediate loading internal IP %s into reg %d", wantIP, nat.RegAddrMin)
	}
	// Internal port 80 big-endian into the proto register.
	if !hasImmediate(exprs, nat.RegProtoMin, []byte{0x00, 0x50}) {
		t.Errorf("no Immediate loading internal port 80 into reg %d", nat.RegProtoMin)
	}
}

func TestBuildPortForwardDNATExprs_RangeKeepPort(t *testing.T) {
	pf := model.PortForward{
		Name: "range", InInterface: "eth0", ExternalPort: "8000-8010",
		Protocol: "udp", InternalIP: "10.0.0.5", InternalPort: "", Status: true,
	}
	exprs, err := buildPortForwardDNATExprs(pf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	nat := findNAT(exprs)
	if nat == nil {
		t.Fatalf("no NAT expression found")
	}
	// Keep-port: address is rewritten but the port register must be unset.
	if nat.RegAddrMin == 0 {
		t.Errorf("keep-port DNAT must still set RegAddrMin")
	}
	if nat.RegProtoMin != 0 {
		t.Errorf("keep-port DNAT must NOT set RegProtoMin, got %d", nat.RegProtoMin)
	}
}

func TestBuildPortForwardDNATExprs_RangeTranslatedRejected(t *testing.T) {
	pf := model.PortForward{
		Name: "bad", InInterface: "eth0", ExternalPort: "8000-8010",
		Protocol: "tcp", InternalIP: "10.0.0.5", InternalPort: "9000", Status: true,
	}
	if _, err := buildPortForwardDNATExprs(pf); err == nil {
		t.Fatalf("expected error for range->fixed-port translation, got nil")
	}
}

func TestBuildPortForwardAcceptExprs(t *testing.T) {
	pf := model.PortForward{
		Name: "web", InInterface: "eth0", ExternalPort: "8080",
		Protocol: "tcp", InternalIP: "192.168.1.10", InternalPort: "80", Status: true,
	}
	exprs, err := buildPortForwardAcceptExprs(pf)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Ends with an accept verdict.
	last, ok := exprs[len(exprs)-1].(*expr.Verdict)
	if !ok || last.Kind != expr.VerdictAccept {
		t.Errorf("accept rule must end with an Accept verdict")
	}
	// Has a counter (per-entry hit visibility).
	if !hasCounter(exprs) {
		t.Errorf("accept rule missing Counter")
	}
	// Matches the post-DNAT internal IP as ip daddr.
	wantIP := net.ParseIP("192.168.1.10").To4()
	if !hasCmp(exprs, wantIP) {
		t.Errorf("accept rule must match internal IP %s", wantIP)
	}
	// Post-DNAT dport is the translated internal port (80).
	if !hasCmp(exprs, []byte{0x00, 0x50}) {
		t.Errorf("accept rule must match internal dport 80")
	}
}

// --- helpers ---

func findNAT(exprs []expr.Any) *expr.NAT {
	for _, e := range exprs {
		if n, ok := e.(*expr.NAT); ok {
			return n
		}
	}
	return nil
}

func hasLocalFibGuard(exprs []expr.Any) bool {
	for i, e := range exprs {
		f, ok := e.(*expr.Fib)
		if !ok || !f.FlagDADDR || !f.ResultADDRTYPE {
			continue
		}
		// next expr should compare against RTN_LOCAL (2)
		if i+1 < len(exprs) {
			if c, ok := exprs[i+1].(*expr.Cmp); ok && bytes.Equal(c.Data, uint32ToBytes(2)) {
				return true
			}
		}
	}
	return false
}

func hasImmediate(exprs []expr.Any, reg uint32, data []byte) bool {
	for _, e := range exprs {
		if im, ok := e.(*expr.Immediate); ok && im.Register == reg && bytes.Equal(im.Data, data) {
			return true
		}
	}
	return false
}

func hasCounter(exprs []expr.Any) bool {
	for _, e := range exprs {
		if _, ok := e.(*expr.Counter); ok {
			return true
		}
	}
	return false
}

func hasCmp(exprs []expr.Any, data []byte) bool {
	for _, e := range exprs {
		if c, ok := e.(*expr.Cmp); ok && bytes.Equal(c.Data, data) {
			return true
		}
	}
	return false
}
