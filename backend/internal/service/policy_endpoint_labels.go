package service

import (
	"math/big"
	"net/netip"
	"strings"

	"pigate/internal/model"
)

// addrRange is one Address Object reduced to an inclusive [start, end] IP
// range (subnets and ranges both fold down to this — see addrMatcher), plus
// its user-defined name and a precomputed size used to pick the narrowest
// match when ranges overlap (plan §2 decision 6).
type addrRange struct {
	name  string
	start netip.Addr
	end   netip.Addr
	size  *big.Int // end - start + 1, used only to compare specificity
}

// addrMatcher resolves a raw IP string to the name of the narrowest
// user-defined Address Object (type "subnet" or "range" only — "fqdn"
// objects hold a domain name, not an IP range, and are skipped) that
// contains it. It is a pure, panic-free function of its input: built once
// per HTTP request from the current Address Object list (plan T-04). Bad or
// unparsable object values are skipped silently rather than erroring the
// whole request — user config for one object must never break lookups for
// every other object.
type addrMatcher struct {
	ranges []addrRange
}

// newAddrMatcher builds a matcher from a snapshot of Address Objects. Safe
// to call with a nil/empty slice (Match then always returns ok=false).
//
// An Address Object may hold multiple Entries (plan
// docs/ref/todo/multi-value-address-service-objects-plan.md T-08) — each
// entry contributes its own addrRange, and every range for the same object
// carries that object's Name. This is intentional: a single object with N
// subnet/range entries ends up with N ranges in m.ranges, so Match can still
// pick the narrowest one across all objects and entries.
func newAddrMatcher(objs []model.AddressObject) *addrMatcher {
	m := &addrMatcher{}
	for _, o := range objs {
		for _, e := range o.Entries {
			switch strings.ToLower(strings.TrimSpace(e.Type)) {
			case "subnet":
				if r, ok := subnetToRange(e.Value); ok {
					m.ranges = append(m.ranges, addrRange{name: o.Name, start: r.start, end: r.end, size: rangeSize(r.start, r.end)})
				}
			case "range":
				if start, end, ok := parseRangeValue(e.Value); ok {
					m.ranges = append(m.ranges, addrRange{name: o.Name, start: start, end: end, size: rangeSize(start, end)})
				}
			default:
				// "fqdn" and anything unrecognized: no IP range to match against.
			}
		}
	}
	return m
}

// Match returns the name of the narrowest Address Object range containing
// ip, or ok=false if none matches (including when ip itself fails to parse
// — never panics). Ties (identical size) are broken by ascending name for
// determinism.
func (m *addrMatcher) Match(ip string) (name string, ok bool) {
	if m == nil {
		return "", false
	}
	addr, err := netip.ParseAddr(strings.TrimSpace(ip))
	if err != nil {
		return "", false
	}
	addr = addr.Unmap() // normalize IPv4-mapped IPv6 (::ffff:a.b.c.d) to plain IPv4

	var best *addrRange
	for i := range m.ranges {
		r := &m.ranges[i]
		if r.start.Is4() != addr.Is4() {
			continue // family mismatch (IPv4 vs IPv6), never comparable
		}
		if addr.Compare(r.start) < 0 || addr.Compare(r.end) > 0 {
			continue
		}
		if best == nil {
			best = r
			continue
		}
		cmp := r.size.Cmp(best.size)
		if cmp < 0 || (cmp == 0 && r.name < best.name) {
			best = r
		}
	}
	if best == nil {
		return "", false
	}
	return best.name, true
}

type ipRange struct {
	start netip.Addr
	end   netip.Addr
}

// subnetToRange parses an Address Object "subnet" value, which may or may
// not include a "/prefix" suffix (bare IP defaults to /32, mirroring
// buildIPMatchExpressions in real_firewall.go), into an inclusive range.
func subnetToRange(value string) (ipRange, bool) {
	val := strings.TrimSpace(value)
	if val == "" {
		return ipRange{}, false
	}
	if !strings.Contains(val, "/") {
		addr, err := netip.ParseAddr(val)
		if err != nil {
			return ipRange{}, false
		}
		if addr.Is4() {
			val += "/32"
		} else {
			val += "/128"
		}
	}
	prefix, err := netip.ParsePrefix(val)
	if err != nil {
		return ipRange{}, false
	}
	prefix = prefix.Masked()
	start := prefix.Addr()
	end := lastAddrInPrefix(prefix)
	return ipRange{start: start, end: end}, true
}

// lastAddrInPrefix computes the broadcast/last address of prefix by setting
// every host bit to 1.
func lastAddrInPrefix(prefix netip.Prefix) netip.Addr {
	addr := prefix.Addr()
	bytes := addr.AsSlice()
	bits := prefix.Bits()
	for i := range bytes {
		bitOffset := i * 8
		if bitOffset+8 <= bits {
			continue // fully within the network portion, keep as-is
		}
		if bitOffset >= bits {
			bytes[i] = 0xFF
			continue
		}
		// Partial byte: keep the top (bits-bitOffset) bits, set the rest to 1.
		keep := bits - bitOffset
		mask := byte(0xFF) >> uint(keep)
		bytes[i] |= mask
	}
	out, ok := netip.AddrFromSlice(bytes)
	if !ok {
		return addr
	}
	return out
}

// parseRangeValue parses an Address Object "range" value ("a-b").
// Malformed values (missing dash, unparsable ends, reversed order, mixed
// families) are rejected — never a partial match.
func parseRangeValue(value string) (start, end netip.Addr, ok bool) {
	parts := strings.SplitN(value, "-", 2)
	if len(parts) != 2 {
		return netip.Addr{}, netip.Addr{}, false
	}
	s, err1 := netip.ParseAddr(strings.TrimSpace(parts[0]))
	e, err2 := netip.ParseAddr(strings.TrimSpace(parts[1]))
	if err1 != nil || err2 != nil {
		return netip.Addr{}, netip.Addr{}, false
	}
	if s.Is4() != e.Is4() {
		return netip.Addr{}, netip.Addr{}, false
	}
	if s.Compare(e) > 0 {
		return netip.Addr{}, netip.Addr{}, false
	}
	return s, e, true
}

// rangeSize returns end - start + 1 as a big.Int, used only to compare
// "narrowness" between overlapping ranges — big.Int avoids overflow for
// IPv6's full 128-bit address space.
func rangeSize(start, end netip.Addr) *big.Int {
	s := new(big.Int).SetBytes(start.AsSlice())
	e := new(big.Int).SetBytes(end.AsSlice())
	size := new(big.Int).Sub(e, s)
	size.Add(size, big.NewInt(1))
	return size
}
