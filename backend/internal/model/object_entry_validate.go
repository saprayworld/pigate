package model

import (
	"fmt"
	"net"
	"strings"
)

// AddressEntry is one address match within an AddressObject. An AddressObject
// can hold multiple entries (multi-value objects, see docs/ref/todo/
// multi-value-address-service-objects-plan.md) — each entry is the unit that
// gets turned into exactly one nft match/rule fragment when the object is
// expanded. Type is per-entry (not per-object), so subnet/range/fqdn entries
// can be mixed within the same object (plan D-1).
type AddressEntry struct {
	Type  string `json:"type"`  // "subnet" | "range" | "fqdn"
	Value string `json:"value"`
}

// ServiceEntry is one protocol/port match within a ServiceObject. A
// ServiceObject can hold multiple entries — each entry is the unit that gets
// turned into exactly one nft match/rule fragment when the object is
// expanded. Protocol is per-entry (not per-object), so e.g. a TCP entry and a
// UDP entry can be mixed within the same object (plan D-1).
type ServiceEntry struct {
	Protocol string `json:"protocol"` // "TCP" | "UDP" | "TCP/UDP" | "ICMP"
	Port     string `json:"port"`
}

// DefaultMaxObjectEntries is the fallback cap used only when the caller does
// not have a resolved config value on hand (maxEntries <= 0 passed to
// ValidateAddressEntries/ValidateServiceEntries below). It must be kept in
// sync with config.Defaults().MaxObjectEntries; the value actually enforced
// in production comes from the file-only "max-object-entries" config key
// (plan §2.1, D-3), not from this constant.
const DefaultMaxObjectEntries = 64

// isValidFQDN reports whether s is a syntactically valid fully-qualified
// domain name. Lifted verbatim from db/repository.go's isValidFQDN so both
// the single-value legacy validator and the multi-value entry validator
// apply the exact same rule.
func isValidFQDN(s string) bool {
	if len(s) == 0 || len(s) > 253 {
		return false
	}
	labels := strings.Split(s, ".")
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 {
			return false
		}
		if label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if !((c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '-') {
				return false
			}
		}
	}
	return true
}

// ValidateAddressEntry checks a single address entry. Logic is lifted
// verbatim from db/repository.go's validateAddressObject (minus the Name
// check, which is an object-level concern, not an entry-level one).
func ValidateAddressEntry(e AddressEntry) error {
	if e.Type != "subnet" && e.Type != "range" && e.Type != "fqdn" {
		return fmt.Errorf("invalid address object type: %s", e.Type)
	}

	switch e.Type {
	case "subnet":
		_, _, err := net.ParseCIDR(e.Value)
		if err != nil {
			return fmt.Errorf("invalid subnet value %q: %w", e.Value, err)
		}
	case "range":
		parts := strings.Split(e.Value, "-")
		if len(parts) != 2 {
			return fmt.Errorf("invalid IP range value %q: must be in format START-END", e.Value)
		}
		ipStartStr := strings.TrimSpace(parts[0])
		ipEndStr := strings.TrimSpace(parts[1])
		ipStart := net.ParseIP(ipStartStr)
		ipEnd := net.ParseIP(ipEndStr)
		if ipStart == nil {
			return fmt.Errorf("invalid start IP %q in range %q", ipStartStr, e.Value)
		}
		if ipEnd == nil {
			return fmt.Errorf("invalid end IP %q in range %q", ipEndStr, e.Value)
		}
		if (ipStart.To4() != nil) != (ipEnd.To4() != nil) {
			return fmt.Errorf("IP range family mismatch: %s and %s must be of same IP version", ipStartStr, ipEndStr)
		}
	case "fqdn":
		if !isValidFQDN(e.Value) {
			return fmt.Errorf("invalid FQDN value %q", e.Value)
		}
	}
	return nil
}

// ValidateServiceEntry checks a single service entry. Protocol/ICMP rules are
// lifted verbatim from db/repository.go's validateServiceObject (minus the
// Name check, which is an object-level concern, not an entry-level one); the
// port spec itself is parsed by the single canonical model.ParsePortSpec
// parser (model/types.go) instead of duplicating start/end port parsing here
// (plan Caution 5 — validate in one place only).
func ValidateServiceEntry(e ServiceEntry) error {
	if e.Protocol != "TCP" && e.Protocol != "UDP" && e.Protocol != "TCP/UDP" && e.Protocol != "ICMP" {
		return fmt.Errorf("invalid protocol: %s", e.Protocol)
	}

	portStr := strings.TrimSpace(e.Port)
	if e.Protocol == "ICMP" {
		if portStr != "-" {
			return fmt.Errorf("ICMP service port must be '-'")
		}
		return nil
	}

	if portStr == "" {
		return fmt.Errorf("invalid port format %q: must be a single port or range (e.g. 80 or 80-88)", portStr)
	}
	if _, _, err := ParsePortSpec(portStr); err != nil {
		return fmt.Errorf("invalid port %q: %w", portStr, err)
	}
	return nil
}

// ValidateAddressEntries checks a full set of address entries for an
// AddressObject: at least one entry, no more than maxEntries (falling back
// to DefaultMaxObjectEntries when the caller passes maxEntries <= 0, i.e. no
// resolved config value on hand), every entry individually valid, and no
// duplicate entries (compared as trim+lowercase of "type|value").
func ValidateAddressEntries(es []AddressEntry, maxEntries int) error {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxObjectEntries
	}
	if len(es) == 0 {
		return fmt.Errorf("at least one address entry is required")
	}
	if len(es) > maxEntries {
		return fmt.Errorf("too many address entries: %d exceeds the maximum of %d", len(es), maxEntries)
	}

	seen := make(map[string]struct{}, len(es))
	for _, e := range es {
		if err := ValidateAddressEntry(e); err != nil {
			return err
		}
		key := strings.ToLower(strings.TrimSpace(e.Type)) + "|" + strings.ToLower(strings.TrimSpace(e.Value))
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate address entry: type=%q value=%q", e.Type, e.Value)
		}
		seen[key] = struct{}{}
	}
	return nil
}

// ValidateServiceEntries checks a full set of service entries for a
// ServiceObject: at least one entry, no more than maxEntries (falling back
// to DefaultMaxObjectEntries when the caller passes maxEntries <= 0, i.e. no
// resolved config value on hand), every entry individually valid, and no
// duplicate entries (compared as trim+lowercase of "protocol|port").
func ValidateServiceEntries(es []ServiceEntry, maxEntries int) error {
	if maxEntries <= 0 {
		maxEntries = DefaultMaxObjectEntries
	}
	if len(es) == 0 {
		return fmt.Errorf("at least one service entry is required")
	}
	if len(es) > maxEntries {
		return fmt.Errorf("too many service entries: %d exceeds the maximum of %d", len(es), maxEntries)
	}

	seen := make(map[string]struct{}, len(es))
	for _, e := range es {
		if err := ValidateServiceEntry(e); err != nil {
			return err
		}
		key := strings.ToLower(strings.TrimSpace(e.Protocol)) + "|" + strings.ToLower(strings.TrimSpace(e.Port))
		if _, dup := seen[key]; dup {
			return fmt.Errorf("duplicate service entry: protocol=%q port=%q", e.Protocol, e.Port)
		}
		seen[key] = struct{}{}
	}
	return nil
}
