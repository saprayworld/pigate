package model

import (
	"fmt"
)

// ValidatePolicyRule checks a firewall PolicyRule before it is persisted /
// turned into nftables expressions (kernel/real_firewall.go). Chain-specific
// fields that the rule builder ignores for a given chain must still be
// well-formed (or empty/"ALL") so the DB never stores a value that silently
// contradicts what actually gets applied to the kernel — see
// docs/ref/todo/input-output-chain-firewall-plan.md section 2.1.
func ValidatePolicyRule(p PolicyRule) error {
	switch p.Chain {
	case PolicyChainForward, PolicyChainInput, PolicyChainOutput:
	default:
		return fmt.Errorf("chain must be one of %q, %q, %q, got %q", PolicyChainForward, PolicyChainInput, PolicyChainOutput, p.Chain)
	}

	switch p.Chain {
	case PolicyChainInput:
		if p.OutInterface != "" && p.OutInterface != "ALL" {
			return fmt.Errorf("outInterface must be empty or \"ALL\" for chain %q, got %q", p.Chain, p.OutInterface)
		}
		if p.Nat {
			return fmt.Errorf("nat must be false for chain %q", p.Chain)
		}
	case PolicyChainOutput:
		if p.InInterface != "" && p.InInterface != "ALL" {
			return fmt.Errorf("inInterface must be empty or \"ALL\" for chain %q, got %q", p.Chain, p.InInterface)
		}
		if p.Nat {
			return fmt.Errorf("nat must be false for chain %q", p.Chain)
		}
	}

	return nil
}
