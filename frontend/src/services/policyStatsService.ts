import { type PolicyChain, type PolicyRule } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { policyService } from "./policyService"

// Firewall Policy per-rule usage statistics (docs/ref/todo/
// firewall-policy-rule-usage-stats-plan.md). Types mirror
// backend/internal/model/types.go's PolicyRuleStat/PolicyRuleStats
// field-for-field (see docs/openapi.yaml for the source of truth). Four
// important limitations carried over from GET /api/policies/stats — see its
// docs/openapi.yaml description for the full wording:
//  1. Snapshot since the last successful Apply Settings, not lifetime.
//  2. percent/totalBytes/totalPackets are always computed across EVERY
//     chain, regardless of the chain filter passed to getStats().
//  3. lastMatchedAt/lastMatchedSource is a hybrid: "log" (precise, requires
//     the rule's Log flag) or "counter" (poll-based fallback, ±10s).
//  4. Disabled rules never appear in `rules` at all.

export interface PolicyRuleStat {
  ruleId: string
  name: string
  chain: PolicyChain
  action: "ACCEPT" | "DROP"
  log: boolean
  status: boolean
  bytes: number
  packets: number
  percent: number
  unused: boolean
  lastMatchedAt?: string
  lastMatchedSource?: "log" | "counter"
  // monitored/monitoredBytes/monitoredPackets/monitoredSince surface the
  // persisted opt-in counter (docs/ref/todo/
  // fqdn-retry-and-monitored-counters-plan.md D-6, issue #141) — a separate
  // accounting from bytes/packets/percent above, which remain "since the
  // last successful apply" only. monitoredSince is empty when monitored is
  // false.
  monitored: boolean
  monitoredBytes?: number
  monitoredPackets?: number
  monitoredSince?: string
}

export interface PolicyRuleStats {
  rules: PolicyRuleStat[]
  totalBytes: number
  totalPackets: number
  countersSince?: string
  available: boolean
}

// hashId is a small deterministic (non-cryptographic) string hash, used only
// to synthesize reproducible mock data per rule id — never random, so a
// mock-mode dev session shows stable numbers across renders/reloads.
function hashId(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) >>> 0
  }
  return h
}

// buildMockStats synthesizes a PolicyRuleStats response from the current
// mock-mode policy rules (all chains — percent is always computed across all
// chains per the plan's Design decision 2), deterministic per rule id so
// every run exercises: some Unused rules (bytes=0), some resolved via the
// "log" source (Log-enabled rules), some via the "counter" fallback source.
function buildMockStats(rules: PolicyRule[], chain?: PolicyChain): PolicyRuleStats {
  const enabled = rules.filter((r) => r.status)

  const synthesized = enabled.map((r) => {
    const h = hashId(r.id)
    const unused = h % 4 === 3
    const bytes = unused ? 0 : 4000 + (h % 40000)
    const packets = unused ? 0 : Math.max(1, Math.round(bytes / (64 + (h % 900))))

    let lastMatchedAt: string | undefined
    let lastMatchedSource: "log" | "counter" | undefined
    if (!unused) {
      const secondsAgo = 5 + (h % 3600)
      lastMatchedAt = new Date(Date.now() - secondsAgo * 1000).toISOString()
      lastMatchedSource = r.log ? "log" : "counter"
    }

    // Monitored totals are always synthesized to be >= the since-apply bytes
    // above (a persisted running total across many applies must never look
    // smaller than "since the last apply" — that would be nonsensical), and
    // monitoredSince is always well in the past so it's visibly distinct
    // from countersSince below.
    const monitoredBytes = r.monitored ? bytes + 50000 + (h % 200000) : undefined
    const monitoredPackets = r.monitored
      ? packets + Math.max(1, Math.round((50000 + (h % 200000)) / (64 + (h % 900))))
      : undefined
    const monitoredSince = r.monitored
      ? new Date(Date.now() - (3600 + (h % 604800)) * 1000).toISOString()
      : undefined

    return { rule: r, bytes, packets, unused, lastMatchedAt, lastMatchedSource, monitoredBytes, monitoredPackets, monitoredSince }
  })

  const totalBytes = synthesized.reduce((sum, s) => sum + s.bytes, 0)
  const totalPackets = synthesized.reduce((sum, s) => sum + s.packets, 0)

  const rows: PolicyRuleStat[] = synthesized
    .filter((s) => !chain || s.rule.chain === chain)
    .map((s) => ({
      ruleId: s.rule.id,
      name: s.rule.name,
      chain: s.rule.chain,
      action: s.rule.action,
      log: s.rule.log,
      status: s.rule.status,
      bytes: s.bytes,
      packets: s.packets,
      percent: totalBytes > 0 ? Math.round((s.bytes / totalBytes) * 1000) / 10 : 0,
      unused: s.unused,
      lastMatchedAt: s.lastMatchedAt,
      lastMatchedSource: s.lastMatchedSource,
      monitored: s.rule.monitored,
      monitoredBytes: s.monitoredBytes,
      monitoredPackets: s.monitoredPackets,
      monitoredSince: s.monitoredSince,
    }))
    .sort((a, b) => b.bytes - a.bytes || a.ruleId.localeCompare(b.ruleId))

  return {
    rules: rows,
    totalBytes,
    totalPackets,
    countersSince: new Date(Date.now() - 90_000).toISOString(),
    available: true,
  }
}

async function parseError(response: Response, fallback: string): Promise<never> {
  const errBody = await response.json().catch(() => ({}))
  throw new Error(errBody.message || fallback)
}

export const policyStatsService = {
  // GET /api/policies/stats. chain filters the returned rows only — percent/
  // totalBytes/totalPackets are always computed across every chain (see the
  // limitations note above).
  getStats: async (chain?: PolicyChain): Promise<PolicyRuleStats> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      const allRules = await policyService.getAll()
      return buildMockStats(allRules, chain)
    }

    const qs = chain ? `?chain=${encodeURIComponent(chain)}` : ""
    const response = await fetch(`${API_BASE_URL}/policies/stats${qs}`)
    if (!response.ok) {
      await parseError(response, `Failed to fetch policy usage statistics: ${response.statusText}`)
    }
    return response.json()
  },
}
