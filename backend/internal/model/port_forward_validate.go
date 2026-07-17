package model

import (
	"fmt"
	"net"
	"strconv"
	"strings"
)

// ValidatePortForward checks a port-forward entry before its fields are turned
// into nftables prerouting DNAT + forward-accept rules via netlink
// (kernel/real_firewall.go). Rules are built with google/nftables expressions,
// not shell strings, so there is no command-injection surface — but bad values
// (out-of-range ports, non-IPv4 targets, unknown protocols, or a
// range-with-translated-port combination that the expr backend cannot express)
// would produce a broken or silently-wrong rule. This REJECTS rather than
// coerces, mirroring the other model validators.
//
// v1 supports exactly two DNAT shapes (see the port-forward plan, Caution 9):
//   - single external port + InternalPort set  -> dnat to InternalIP:InternalPort
//   - single/range external port, InternalPort empty -> dnat to InternalIP (keep original port)
//
// A port range with a non-empty InternalPort (1:1 range translation) is rejected:
// it cannot be built with plain expressions.
func ValidatePortForward(pf PortForward) error {
	if strings.TrimSpace(pf.Name) == "" {
		return fmt.Errorf("name must not be empty")
	}

	if err := ValidateInterfaceName(pf.InInterface); err != nil {
		return fmt.Errorf("inInterface: %w", err)
	}

	proto := strings.ToLower(strings.TrimSpace(pf.Protocol))
	if proto != "tcp" && proto != "udp" {
		return fmt.Errorf("protocol must be \"tcp\" or \"udp\", got %q", pf.Protocol)
	}

	ip := net.ParseIP(strings.TrimSpace(pf.InternalIP))
	if ip == nil || ip.To4() == nil {
		return fmt.Errorf("internalIP %q is not a valid IPv4 address", pf.InternalIP)
	}

	isRange, err := validatePortSpec(pf.ExternalPort, "externalPort")
	if err != nil {
		return err
	}

	internal := strings.TrimSpace(pf.InternalPort)
	if internal == "" {
		// keep-port DNAT: valid for both single and range external ports.
		return nil
	}

	if isRange {
		// 1:1 range translation is out of scope for v1 (Caution 9).
		return fmt.Errorf("internalPort must be empty when externalPort is a range (%q): range port translation is not supported — leave internalPort empty to keep the original port", pf.ExternalPort)
	}
	if _, err := parsePort(internal); err != nil {
		return fmt.Errorf("internalPort: %w", err)
	}
	return nil
}

// validatePortSpec parses a single ("8080") or range ("8000-8010") port spec and
// reports whether it is a range. It rejects malformed specs and reversed ranges.
func validatePortSpec(spec, field string) (isRange bool, err error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return false, fmt.Errorf("%s must not be empty", field)
	}
	parts := strings.Split(spec, "-")
	switch len(parts) {
	case 1:
		if _, err := parsePort(parts[0]); err != nil {
			return false, fmt.Errorf("%s: %w", field, err)
		}
		return false, nil
	case 2:
		start, err := parsePort(parts[0])
		if err != nil {
			return false, fmt.Errorf("%s start: %w", field, err)
		}
		end, err := parsePort(parts[1])
		if err != nil {
			return false, fmt.Errorf("%s end: %w", field, err)
		}
		if start >= end {
			return false, fmt.Errorf("%s range %q must have start < end", field, spec)
		}
		return true, nil
	default:
		return false, fmt.Errorf("%s %q is not a valid port or range", field, spec)
	}
}

// parsePort parses a 1..65535 port number.
func parsePort(s string) (int, error) {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return 0, fmt.Errorf("port %q is not a number", s)
	}
	if n < 1 || n > 65535 {
		return 0, fmt.Errorf("port %d out of range (1-65535)", n)
	}
	return n, nil
}

// PortForwardsConflict reports whether two enabled port-forwards overlap on
// (InInterface, Protocol, external port), which would make the first rule
// silently shadow the second (Caution 10). Range vs single overlaps are caught.
// It only meaningfully compares entries with the same interface+protocol.
func PortForwardsConflict(a, b PortForward) bool {
	if !strings.EqualFold(a.InInterface, b.InInterface) {
		return false
	}
	if !strings.EqualFold(a.Protocol, b.Protocol) {
		return false
	}
	aStart, aEnd := portRange(a.ExternalPort)
	bStart, bEnd := portRange(b.ExternalPort)
	// overlap if ranges intersect
	return aStart <= bEnd && bStart <= aEnd
}

// portRange returns the inclusive [start,end] a port spec covers; on parse
// failure it returns (0,0) which never overlaps a valid range.
func portRange(spec string) (int, int) {
	spec = strings.TrimSpace(spec)
	parts := strings.Split(spec, "-")
	if len(parts) == 1 {
		p, err := parsePort(parts[0])
		if err != nil {
			return 0, 0
		}
		return p, p
	}
	if len(parts) == 2 {
		start, err1 := parsePort(parts[0])
		end, err2 := parsePort(parts[1])
		if err1 != nil || err2 != nil {
			return 0, 0
		}
		return start, end
	}
	return 0, 0
}
