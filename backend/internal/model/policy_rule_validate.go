package model

import (
	"fmt"
	"strings"
)

// ValidatePolicyRule checks a firewall PolicyRule before it is persisted /
// turned into nftables expressions (kernel/real_firewall.go). Chain-specific
// fields that the rule builder ignores for a given chain must still be
// well-formed (or empty/"ALL") so the DB never stores a value that silently
// contradicts what actually gets applied to the kernel — see
// docs/ref/todo/input-output-chain-firewall-plan.md section 2.1. Callers must
// have already run NormalizePolicyRuleInterfaces on p (InInterfaces/
// OutInterfaces are assumed non-empty here); ValidatePolicyRule itself does
// not normalize.
func ValidatePolicyRule(p PolicyRule) error {
	switch p.Chain {
	case PolicyChainForward, PolicyChainInput, PolicyChainOutput:
	default:
		return fmt.Errorf("chain must be one of %q, %q, %q, got %q", PolicyChainForward, PolicyChainInput, PolicyChainOutput, p.Chain)
	}

	inIfaces := p.InInterfaces
	if len(inIfaces) == 0 {
		inIfaces = []string{p.InInterface}
	}
	outIfaces := p.OutInterfaces
	if len(outIfaces) == 0 {
		outIfaces = []string{p.OutInterface}
	}

	switch p.Chain {
	case PolicyChainInput:
		if !isAllOrEmptyInterfaceList(outIfaces) {
			return fmt.Errorf("outInterface must be empty or \"ALL\" for chain %q, got %q", p.Chain, strings.Join(outIfaces, ","))
		}
		if p.Nat {
			return fmt.Errorf("nat must be false for chain %q", p.Chain)
		}
	case PolicyChainOutput:
		if !isAllOrEmptyInterfaceList(inIfaces) {
			return fmt.Errorf("inInterface must be empty or \"ALL\" for chain %q, got %q", p.Chain, strings.Join(inIfaces, ","))
		}
		if p.Nat {
			return fmt.Errorf("nat must be false for chain %q", p.Chain)
		}
	}

	if err := validatePolicyInterfaceNames(inIfaces); err != nil {
		return fmt.Errorf("inInterfaces: %w", err)
	}
	if err := validatePolicyInterfaceNames(outIfaces); err != nil {
		return fmt.Errorf("outInterfaces: %w", err)
	}

	return nil
}

// isAllOrEmptyInterfaceList reports whether names represents "no
// interface restriction" for the purposes of the input/output chain
// checks above: empty, a single "" entry, or a single "ALL" entry.
func isAllOrEmptyInterfaceList(names []string) bool {
	if len(names) == 0 {
		return true
	}
	if len(names) == 1 && (names[0] == "" || names[0] == "ALL") {
		return true
	}
	return false
}

// validatePolicyInterfaceNames checks that every non-"ALL" entry in names is
// a syntactically valid interface name (ValidateInterfaceName — <=15 chars +
// whitelist, see dns_validate.go) and that the list has no duplicates. It
// does not enforce a count cap; see ValidatePolicyInterfaces for that.
func validatePolicyInterfaceNames(names []string) error {
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		if name == "" || name == "ALL" {
			continue
		}
		if err := ValidateInterfaceName(name); err != nil {
			return err
		}
		if _, dup := seen[name]; dup {
			return fmt.Errorf("duplicate interface name: %q", name)
		}
		seen[name] = struct{}{}
	}
	return nil
}

// ValidatePolicyInterfaces checks a single direction's interface list
// against the configurable per-direction cap: falls back to
// DefaultMaxPolicyInterfacesPerDirection when maxPerDirection <= 0 (i.e. no
// resolved config value on hand). Mirrors ValidateAddressEntries in
// object_entry_validate.go. This only checks the count — name syntax and
// duplicates are already covered by ValidatePolicyRule.
func ValidatePolicyInterfaces(names []string, maxPerDirection int) error {
	if maxPerDirection <= 0 {
		maxPerDirection = DefaultMaxPolicyInterfacesPerDirection
	}
	if len(names) > maxPerDirection {
		return fmt.Errorf("too many interfaces: %d exceeds the maximum of %d", len(names), maxPerDirection)
	}
	return nil
}
