package model

import (
	"fmt"
	"strings"
)

// EncodeDNSNameHex encodes a domain name as an uncompressed DNS wire-format
// RDATA (each label prefixed by its length byte, terminated by a zero byte)
// and returns it as a lowercase, unseparated hex string.
//
// It is used to build the RDATA argument of dnsmasq's generic
// `dns-rr=<fqdn>,2,<hex>` directive (record type 2 = NS) — dnsmasq has no
// dedicated `ns-record=` directive, so publishing an NS record has to go
// through `dns-rr` with a hand-encoded RDATA. Name compression (an offset
// pointer into an earlier occurrence of the same name) is deliberately never
// used here: the offset depends on where the record ends up inside the final
// DNS message, which is not known at config-generation time.
//
// Rules (reject rather than silently truncate/strip, matching the other
// validators in this package): the trailing dot is trimmed before encoding;
// the name must not be empty; every label must be 1-63 bytes of
// [A-Za-z0-9-], must not start or end with '-' (so no empty label from a
// leading/trailing/doubled dot survives either); and the total encoded name
// must not exceed 253 bytes. The name is lowercased before encoding so the
// same name always produces the same hex regardless of the case the user
// typed it in.
func EncodeDNSNameHex(name string) (string, error) {
	trimmed := strings.TrimSuffix(strings.TrimSpace(name), ".")
	if trimmed == "" {
		return "", fmt.Errorf("domain name must not be empty")
	}
	if len(trimmed) > 253 {
		return "", fmt.Errorf("domain name %q exceeds 253 characters", name)
	}

	labels := strings.Split(strings.ToLower(trimmed), ".")
	var sb strings.Builder
	for _, label := range labels {
		if err := validateDNSLabel(label); err != nil {
			return "", fmt.Errorf("domain name %q: %w", name, err)
		}
		sb.WriteByte(byte(len(label)))
		sb.WriteString(label)
	}
	sb.WriteByte(0x00)

	raw := sb.String()
	hexOut := make([]byte, 0, len(raw)*2)
	const hexDigits = "0123456789abcdef"
	for i := 0; i < len(raw); i++ {
		b := raw[i]
		hexOut = append(hexOut, hexDigits[b>>4], hexDigits[b&0x0f])
	}
	return string(hexOut), nil
}

// validateDNSLabel checks a single dot-separated label per the rules
// documented on EncodeDNSNameHex.
func validateDNSLabel(label string) error {
	if label == "" {
		return fmt.Errorf("contains an empty label")
	}
	if len(label) > 63 {
		return fmt.Errorf("label %q exceeds 63 characters", label)
	}
	if label[0] == '-' || label[len(label)-1] == '-' {
		return fmt.Errorf("label %q must not start or end with '-'", label)
	}
	for i := 0; i < len(label); i++ {
		c := label[i]
		isAlphaNum := (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9')
		if !isAlphaNum && c != '-' {
			return fmt.Errorf("label %q contains an invalid character", label)
		}
	}
	return nil
}
