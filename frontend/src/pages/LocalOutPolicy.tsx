import PolicyChainPage from "@/components/policy/PolicyChainPage"

// Local-Out Policy controls the "output" chain — traffic originating from the
// box itself. This chain uses policy accept (default-allow); strict egress
// filtering is out of scope for now (see
// docs/ref/todo/input-output-chain-firewall-plan.md section 0).
export default function LocalOutPolicy() {
  return (
    <PolicyChainPage
      chain="output"
      pageTitle="Local-Out Policy"
      pageDescription="ควบคุมทราฟฟิกที่ออกจากตัวบอร์ดเอง (ค่าเริ่มต้นอนุญาตทั้งหมด) — ลากและวางเพื่อเปลี่ยนความสำคัญ"
    />
  )
}
