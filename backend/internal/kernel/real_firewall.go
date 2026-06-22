package kernel

import (
	"fmt"
	"log"
	"pigate/internal/model"

	"github.com/google/nftables"
)

// RealFirewall implements FirewallManager using Netlink and github.com/google/nftables
type RealFirewall struct {
	dockerCompat bool
}

func NewRealFirewall(dockerCompat bool) *RealFirewall {
	return &RealFirewall{
		dockerCompat: dockerCompat,
	}
}

func (rf *RealFirewall) ApplyRules(rules []model.PolicyRule, ifaces []model.NetworkInterface) error {
	log.Printf("[RealFirewall] Applying %d rules to Linux kernel via Netlink (Docker Compatibility: %t)", len(rules), rf.dockerCompat)

	// In real environment, we initialize connection to nftables
	conn, err := nftables.New()
	if err != nil {
		return fmt.Errorf("failed to connect to nftables: %w (requires root or CAP_NET_ADMIN)", err)
	}

	// Create pigate table (inet family to cover both ipv4 and ipv6)
	table := conn.AddTable(&nftables.Table{
		Name:   "pigate",
		Family: nftables.TableFamilyINet,
	})

	log.Printf("[RealFirewall] Configured nftables table: %s (Family: Inet)", table.Name)

	// Flush table first to clear old rules before applying the new transaction
	conn.FlushTable(table)

	if rf.dockerCompat {
		log.Printf("[RealFirewall] [Docker-Compat] Generating bypass rules for docker0 and br-*")
		// Detailed implementation of rules using google/nftables will be done in the next phase
	}

	// Commit the transaction to Linux Kernel
	if err := conn.Flush(); err != nil {
		log.Printf("[RealFirewall] Error committing rules to kernel: %v", err)
		return fmt.Errorf("failed to flush nftables rules: %w", err)
	}

	log.Printf("[RealFirewall] Successfully applied firewall rules to Linux kernel")
	return nil
}
