import PolicyChainPage from "@/components/policy/PolicyChainPage"

// Firewall Policy controls the "forward" chain — traffic passing through the
// box between interfaces (LAN<->WAN etc). See PolicyChainPage for the shared
// implementation and docs/ref/todo/input-output-chain-firewall-plan.md.
export default function FirewallPolicy() {
  return (
    <PolicyChainPage
      chain="forward"
      pageTitle="Firewall Policy"
      pageDescription="จัดเรียงลำดับความสำคัญของกฎ (Security Policy Rule Chains) ลากและวางเพื่อเปลี่ยนความสำคัญ"
    />
  )
}
