package model

import (
	"reflect"
	"strings"
	"testing"
)

func basePolicyRule(chain string) PolicyRule {
	return PolicyRule{
		ID:          "r1",
		Name:        "test rule",
		Chain:       chain,
		Source:      []string{"any"},
		Destination: []string{"any"},
		Service:     []string{"ALL"},
		Action:      "ACCEPT",
	}
}

func TestNormalizePolicyRuleInterfaces(t *testing.T) {
	cases := []struct {
		name       string
		in         PolicyRule
		wantIn     []string
		wantOut    []string
		wantInScl  string
		wantOutScl string
	}{
		{
			name:       "empty everything defaults to ALL",
			in:         PolicyRule{},
			wantIn:     []string{"ALL"},
			wantOut:    []string{"ALL"},
			wantInScl:  "ALL",
			wantOutScl: "ALL",
		},
		{
			name:       "seeds list from legacy scalar",
			in:         PolicyRule{InInterface: "eth0", OutInterface: "eth1"},
			wantIn:     []string{"eth0"},
			wantOut:    []string{"eth1"},
			wantInScl:  "eth0",
			wantOutScl: "eth1",
		},
		{
			name:       "list wins over scalar when both present",
			in:         PolicyRule{InInterface: "eth9", InInterfaces: []string{"eth0", "wlan0"}},
			wantIn:     []string{"eth0", "wlan0"},
			wantOut:    []string{"ALL"},
			wantInScl:  "eth0",
			wantOutScl: "ALL",
		},
		{
			name:       "trims whitespace and drops empty entries",
			in:         PolicyRule{InInterfaces: []string{" eth0 ", "", "  "}},
			wantIn:     []string{"eth0"},
			wantOut:    []string{"ALL"},
			wantInScl:  "eth0",
			wantOutScl: "ALL",
		},
		{
			name:       "drops duplicates",
			in:         PolicyRule{InInterfaces: []string{"eth0", "eth0", "wlan0"}},
			wantIn:     []string{"eth0", "wlan0"},
			wantOut:    []string{"ALL"},
			wantInScl:  "eth0",
			wantOutScl: "ALL",
		},
		{
			name:       "ALL mixed with others collapses to just ALL",
			in:         PolicyRule{InInterfaces: []string{"eth0", "ALL", "wlan0"}},
			wantIn:     []string{"ALL"},
			wantOut:    []string{"ALL"},
			wantInScl:  "ALL",
			wantOutScl: "ALL",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			p := c.in
			NormalizePolicyRuleInterfaces(&p)
			if !reflect.DeepEqual(p.InInterfaces, c.wantIn) {
				t.Errorf("InInterfaces = %v, want %v", p.InInterfaces, c.wantIn)
			}
			if !reflect.DeepEqual(p.OutInterfaces, c.wantOut) {
				t.Errorf("OutInterfaces = %v, want %v", p.OutInterfaces, c.wantOut)
			}
			if p.InInterface != c.wantInScl {
				t.Errorf("InInterface = %q, want %q", p.InInterface, c.wantInScl)
			}
			if p.OutInterface != c.wantOutScl {
				t.Errorf("OutInterface = %q, want %q", p.OutInterface, c.wantOutScl)
			}

			// Idempotency: normalizing again must not change anything further.
			p2 := p
			NormalizePolicyRuleInterfaces(&p2)
			if !reflect.DeepEqual(p, p2) {
				t.Errorf("NormalizePolicyRuleInterfaces is not idempotent: first=%+v second=%+v", p, p2)
			}
		})
	}

	// nil-safety
	NormalizePolicyRuleInterfaces(nil)
	NormalizePolicyRuleInputInterfaces(nil)
}

func TestNormalizePolicyRuleInputInterfaces(t *testing.T) {
	in := PolicyRuleInput{InInterface: "eth0"}
	NormalizePolicyRuleInputInterfaces(&in)
	if !reflect.DeepEqual(in.InInterfaces, []string{"eth0"}) {
		t.Errorf("InInterfaces = %v, want [eth0]", in.InInterfaces)
	}
	if in.InInterface != "eth0" {
		t.Errorf("InInterface = %q, want eth0", in.InInterface)
	}
}

func TestValidatePolicyRule_ChainInterfaceRules(t *testing.T) {
	// input chain must not have a real outInterface
	p := basePolicyRule(PolicyChainInput)
	p.OutInterface = "eth1"
	NormalizePolicyRuleInterfaces(&p)
	if err := ValidatePolicyRule(p); err == nil {
		t.Fatalf("expected error for input chain with outInterface set")
	}

	// input chain with OutInterfaces=["ALL"] must be accepted
	p = basePolicyRule(PolicyChainInput)
	NormalizePolicyRuleInterfaces(&p)
	if err := ValidatePolicyRule(p); err != nil {
		t.Fatalf("unexpected error for input chain with ALL out: %v", err)
	}

	// output chain must not have a real inInterface
	p = basePolicyRule(PolicyChainOutput)
	p.InInterface = "eth0"
	NormalizePolicyRuleInterfaces(&p)
	if err := ValidatePolicyRule(p); err == nil {
		t.Fatalf("expected error for output chain with inInterface set")
	}

	// output chain with InInterfaces=["ALL"] must be accepted
	p = basePolicyRule(PolicyChainOutput)
	NormalizePolicyRuleInterfaces(&p)
	if err := ValidatePolicyRule(p); err != nil {
		t.Fatalf("unexpected error for output chain with ALL in: %v", err)
	}

	// forward chain allows both directions to be set
	p = basePolicyRule(PolicyChainForward)
	p.InInterfaces = []string{"eth0", "wlan0"}
	p.OutInterfaces = []string{"eth1"}
	NormalizePolicyRuleInterfaces(&p)
	if err := ValidatePolicyRule(p); err != nil {
		t.Fatalf("unexpected error for forward chain multi-interface rule: %v", err)
	}
}

func TestValidatePolicyRule_InterfaceNameChecks(t *testing.T) {
	// interface name too long (>15 chars) must be rejected
	p := basePolicyRule(PolicyChainForward)
	p.InInterfaces = []string{strings.Repeat("a", 16)}
	if err := ValidatePolicyRule(p); err == nil {
		t.Fatalf("expected error for interface name longer than 15 chars")
	}

	// interface name with forbidden characters must be rejected
	p = basePolicyRule(PolicyChainForward)
	p.InInterfaces = []string{"eth0;rm"}
	if err := ValidatePolicyRule(p); err == nil {
		t.Fatalf("expected error for interface name with invalid characters")
	}

	// duplicate interface names in the same direction must be rejected
	p = basePolicyRule(PolicyChainForward)
	p.InInterfaces = []string{"eth0", "eth0"}
	if err := ValidatePolicyRule(p); err == nil {
		t.Fatalf("expected error for duplicate interface names")
	}

	// valid 15-char name and mixed valid names must pass
	p = basePolicyRule(PolicyChainForward)
	p.InInterfaces = []string{strings.Repeat("a", 15), "wlan0"}
	if err := ValidatePolicyRule(p); err != nil {
		t.Fatalf("unexpected error for valid interface names: %v", err)
	}
}

func TestValidatePolicyInterfaces(t *testing.T) {
	// default cap (maxPerDirection <= 0) is DefaultMaxPolicyInterfacesPerDirection
	names := make([]string, DefaultMaxPolicyInterfacesPerDirection)
	for i := range names {
		names[i] = "eth" + string(rune('0'+i))
	}
	if err := ValidatePolicyInterfaces(names, 0); err != nil {
		t.Fatalf("expected no error at exactly the default cap, got %v", err)
	}
	if err := ValidatePolicyInterfaces(append(names, "eth9"), 0); err == nil {
		t.Fatalf("expected error exceeding the default cap")
	}

	// explicit cap from config takes priority over the default
	if err := ValidatePolicyInterfaces([]string{"eth0", "eth1", "eth2"}, 2); err == nil {
		t.Fatalf("expected error exceeding an explicit cap of 2")
	}
	if err := ValidatePolicyInterfaces([]string{"eth0", "eth1"}, 2); err != nil {
		t.Fatalf("unexpected error at exactly an explicit cap of 2: %v", err)
	}
}
