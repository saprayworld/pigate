package model

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
)

// Whitelist validators for values that end up in the dnsmasq config files
// (pigate-dns.conf / pigate-dhcp.conf). dnsmasq is directive-per-line, so a
// single un-validated newline inside a user string injects an arbitrary
// directive (e.g. `1.2.3.4\naddress=/evil/6.6.6.6`). `dnsmasq --test` cannot
// catch this because the injected line is itself valid config.
//
// These functions REJECT (return an error) rather than strip — mirroring the
// discipline of kernel.SanitizeWpaInput but preferring explicit feedback over
// silently mutating the user's value. They are pure, dependency-free, and live
// in model so the api (handlers), service (config import) and kernel
// (generation-time defense-in-depth) layers can all call them without an import
// cycle.
//
// The regexps are anchored full-match (^...$). Go's regexp treats $ as
// end-of-text (not end-of-line) unless the (?m) flag is set, so an embedded
// \n or \r fails the match — which is exactly the property we rely on.
var (
	// reZoneName also serves for FQDN-shaped values (CNAME/PTR targets): dotted
	// labels of letters, digits and hyphens. No underscore (RFC hostname).
	reZoneName = regexp.MustCompile(`^[a-zA-Z0-9.-]+$`)
	// reHostLabel is a single DNS label — used for DHCP reservation device names.
	reHostLabel = regexp.MustCompile(`^[a-zA-Z0-9-]{1,63}$`)
	// reForwardTo covers a forward-zone upstream: IPv4/IPv6 address, optional
	// #port, or a resolver hostname. Charset only (no newline/space).
	reForwardTo = regexp.MustCompile(`^[a-zA-Z0-9.:#-]+$`)
	// reMAC is a colon- or hyphen-separated 6-octet MAC address.
	reMAC = regexp.MustCompile(`^([0-9a-fA-F]{2})([:-][0-9a-fA-F]{2}){5}$`)
)

// ValidateDNSZone checks the zone name (always) and, for a forward zone, the
// ForwardTo upstream. Records are validated separately via ValidateDNSRecord.
func ValidateDNSZone(z DNSZone) error {
	name := strings.TrimSpace(z.ZoneName)
	if name == "" {
		return fmt.Errorf("zone name must not be empty")
	}
	if !reZoneName.MatchString(name) {
		return fmt.Errorf("zone name %q contains invalid characters (allowed: letters, digits, '.', '-')", z.ZoneName)
	}
	// ForwardTo is only written to the config for forward (non-authoritative)
	// zones, and only when non-empty — validate under the same condition.
	if !z.IsAuthoritative {
		fwd := strings.TrimSpace(z.ForwardTo)
		if fwd != "" && !reForwardTo.MatchString(fwd) {
			return fmt.Errorf("zone %q forwardTo %q contains invalid characters", name, z.ForwardTo)
		}
	}
	return nil
}

// ValidateDNSRecord validates a record's name and its value according to the
// record type, matching exactly what the generator (kernel/dns_server.go) will
// accept — no stricter, so it never rejects a value the writer handles fine.
func ValidateDNSRecord(r DNSRecord) error {
	name := strings.TrimSpace(r.Name)
	if name != "" && name != "@" && !reZoneName.MatchString(name) {
		return fmt.Errorf("record name %q contains invalid characters", r.Name)
	}

	value := strings.TrimSpace(r.Value)
	switch strings.ToUpper(r.Type) {
	case "A":
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() == nil {
			return fmt.Errorf("A record value %q is not a valid IPv4 address", r.Value)
		}
	case "AAAA":
		ip := net.ParseIP(value)
		if ip == nil || ip.To4() != nil {
			return fmt.Errorf("AAAA record value %q is not a valid IPv6 address", r.Value)
		}
	case "CNAME":
		// The writer strips a trailing dot and appends the zone for short names;
		// validate the charset of the user's value as entered.
		target := strings.TrimSuffix(value, ".")
		if target == "" || !reZoneName.MatchString(target) {
			return fmt.Errorf("CNAME record value %q is not a valid target name", r.Value)
		}
	case "MX":
		// Accepted forms: "<pref> <target>" or a bare "<target>". Fields splits
		// on any whitespace (including \n), so reject embedded control chars
		// first, then validate the parsed pieces.
		if strings.ContainsAny(value, "\n\r") {
			return fmt.Errorf("MX record value must not contain newlines")
		}
		// Mirror the writer: >=2 fields → parts[0]=pref, parts[1]=target (rest
		// ignored); exactly 1 field → bare target. Don't validate stricter than
		// the generator accepts.
		parts := strings.Fields(value)
		switch {
		case len(parts) == 0:
			return fmt.Errorf("MX record value must not be empty")
		case len(parts) == 1:
			if !reZoneName.MatchString(parts[0]) {
				return fmt.Errorf("MX record target %q is not a valid name", parts[0])
			}
		default:
			if _, err := strconv.Atoi(parts[0]); err != nil {
				return fmt.Errorf("MX record preference %q is not a number", parts[0])
			}
			if !reZoneName.MatchString(parts[1]) {
				return fmt.Errorf("MX record target %q is not a valid name", parts[1])
			}
		}
	case "TXT":
		// Written inside double quotes, so " would break out; \n/\r would inject
		// a directive. dnsmasq caps a single TXT string at 255 bytes.
		if strings.ContainsAny(value, "\n\r\"") {
			return fmt.Errorf("TXT record value must not contain newlines or double quotes")
		}
		if len(value) > 255 {
			return fmt.Errorf("TXT record value exceeds 255 characters")
		}
	case "PTR":
		if value == "" || !reZoneName.MatchString(value) {
			return fmt.Errorf("PTR record value %q is not a valid name", r.Value)
		}
	default:
		return fmt.Errorf("unsupported DNS record type %q", r.Type)
	}
	return nil
}

// ValidateReservationName checks a DHCP reservation device name. Empty is
// allowed (the writer substitutes a default). Spaces are allowed because the
// writer collapses them to '-'; everything else must survive that substitution
// as a single DNS label — which rejects newlines and control characters.
func ValidateReservationName(name string) error {
	if name == "" {
		return nil
	}
	normalized := strings.ReplaceAll(name, " ", "-")
	if !reHostLabel.MatchString(normalized) {
		return fmt.Errorf("reservation device name %q contains invalid characters (allowed: letters, digits, spaces, '-', max 63)", name)
	}
	return nil
}

// ValidateReservation validates every field of a reservation that is written to
// the dnsmasq config: the MAC address, the reserved IP, and the device name.
// MAC and IP are validated only when both are set, matching the writer, which
// emits a dhcp-host line only for reservations that carry both.
func ValidateReservation(res DhcpReservation) error {
	if res.MacAddress != "" && res.IPAddress != "" {
		if !reMAC.MatchString(strings.TrimSpace(res.MacAddress)) {
			return fmt.Errorf("reservation MAC address %q is not valid", res.MacAddress)
		}
		if net.ParseIP(strings.TrimSpace(res.IPAddress)) == nil {
			return fmt.Errorf("reservation IP address %q is not valid", res.IPAddress)
		}
	}
	return ValidateReservationName(res.DeviceName)
}
