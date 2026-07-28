import PolicyChainPage from "@/components/policy/PolicyChainPage"

// Local-In Policy controls the "input" chain — traffic destined to the box
// itself (e.g. the web UI, SSH, ping). Rules here are always evaluated AFTER
// each interface's own Admin Access accept rules, so a bad rule here can
// never lock the operator out of the device (structural guarantee — see
// docs/ref/todo/input-output-chain-firewall-plan.md section 2.2).
export default function LocalInPolicy() {
  return (
    <PolicyChainPage
      chain="input"
      pageTitle="Local-In Policy"
      pageDescription="ควบคุมทราฟฟิกที่มุ่งหน้าเข้าตัวบอร์ดเอง (เช่น หน้าเว็บ, SSH, ping) — ลากและวางเพื่อเปลี่ยนความสำคัญ"
    />
  )
}
