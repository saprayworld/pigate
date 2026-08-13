import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { policyService } from "./policyService"

// GET /api/policies/{id}/endpoints — "which IP/service matched this rule"
// troubleshooting view (docs/ref/todo/firewall-rule-matched-endpoints-plan.md).
// Types mirror backend/internal/model/types.go's EndpointHit/ServiceHit/
// PolicyRuleEndpoints field-for-field (see docs/openapi.yaml for the source
// of truth). Hard limitations carried over from that endpoint's description:
//  1. Requires the rule's Log flag to be on — logEnabled=false means the
//     lists are always empty, not an error.
//  2. count is a number of log entries, not bytes/packets.
//  3. The data window equals the traffic-log ring buffer's current
//     contents; clearing the traffic log makes this data disappear
//     immediately, and a low-traffic rule can be pushed out by a
//     high-traffic one.
//  4. Only new connections and DROPped packets are logged (same NFLOG
//     behavior as the Traffic Log pages), so counts are not a full tally.
// Name precedence for display: addressName (user-defined Address Object)
// wins over domain (DNS reverse cache), which wins over hostname (DHCP
// lease) — deliberately different from the Traffic Log pages, where domain
// comes before hostname and neither knows about Address Objects at all.

export interface EndpointHit {
  ip: string
  count: number
  firstSeenAt: string
  lastSeenAt: string
  addressName: string
  hostname: string
  domain: string
  fromRule: boolean
}

export interface ServiceHit {
  proto: string
  port: string
  count: number
  firstSeenAt: string
  lastSeenAt: string
  serviceName: string
  fromRule: boolean
}

export interface PolicyRuleEndpoints {
  ruleId: string
  ruleName: string
  chain: "forward" | "input" | "output"
  logEnabled: boolean
  matchedEntries: number
  uniqueSources: number
  uniqueDestinations: number
  uniqueServices: number
  limit: number
  truncated: boolean
  scannedEntries: number
  bufferOldestAt?: string
  sources: EndpointHit[]
  destinations: EndpointHit[]
  services: ServiceHit[]
}

// hashId is a small deterministic (non-cryptographic) string hash, used only
// to synthesize reproducible mock data per rule id — never random, so a
// mock-mode dev session shows stable numbers across renders/reloads (mirrors
// policyStatsService.ts's hashId).
function hashId(id: string): number {
  let h = 0
  for (let i = 0; i < id.length; i++) {
    h = (h * 31 + id.charCodeAt(i)) >>> 0
  }
  return h
}

function isoMinutesAgo(minutes: number): string {
  return new Date(Date.now() - minutes * 60_000).toISOString()
}

// buildMockHit synthesizes one EndpointHit, cycling deterministically through
// the 4 name-resolution cases the plan requires mock mode to exercise: has
// addressName (wins display), has only domain, has only hostname, has none
// (shows raw IP only).
function buildMockHit(seed: number, index: number, ownName?: string): EndpointHit {
  const variant = (seed + index) % 4
  const ip = `192.168.${1 + (seed % 20)}.${10 + ((seed + index * 7) % 240)}`
  const count = 5 + ((seed + index * 13) % 400)

  let addressName = ""
  let domain = ""
  let hostname = ""
  switch (variant) {
    case 0:
      addressName = ownName ?? `AddrGroup_${index}`
      break
    case 1:
      domain = `host-${index}.example.com`
      break
    case 2:
      hostname = `device-${index}`
      break
    default:
      // no name at all — raw IP only
      break
  }

  return {
    ip,
    count,
    firstSeenAt: isoMinutesAgo(30 + index),
    lastSeenAt: isoMinutesAgo(index),
    addressName,
    hostname,
    domain,
    fromRule: variant === 0,
  }
}

const MOCK_SERVICE_CATALOG: Array<{ proto: string; port: string; serviceName: string }> = [
  { proto: "TCP", port: "443", serviceName: "HTTPS" },
  { proto: "TCP", port: "80", serviceName: "HTTP" },
  { proto: "UDP", port: "53", serviceName: "DNS" },
  { proto: "TCP", port: "22", serviceName: "" }, // deliberately unmatched -> raw PROTO/PORT
  { proto: "ICMP", port: "-", serviceName: "" },
]

function buildMockService(seed: number, index: number, ownServiceName?: string): ServiceHit {
  const entry = MOCK_SERVICE_CATALOG[(seed + index) % MOCK_SERVICE_CATALOG.length]
  const serviceName = entry.serviceName || (index === 0 && ownServiceName ? ownServiceName : entry.serviceName)
  return {
    proto: entry.proto,
    port: entry.port,
    count: 3 + ((seed + index * 11) % 300),
    firstSeenAt: isoMinutesAgo(40 + index),
    lastSeenAt: isoMinutesAgo(index),
    serviceName,
    fromRule: !!serviceName && serviceName === ownServiceName,
  }
}

async function buildMockEndpoints(ruleId: string, limit: number): Promise<PolicyRuleEndpoints> {
  const rules = await policyService.getAll()
  const rule = rules.find((r) => r.id === ruleId)
  const h = hashId(ruleId)

  const chain = rule?.chain ?? "forward"
  const ruleName = rule?.name ?? ruleId
  // logEnabled=false is one deterministic mock case (1 in 5 rule ids) so the
  // "enable Log to see data" empty state is exercisable without depending on
  // the actual rule.log flag.
  const logEnabled = rule ? rule.log && h % 5 !== 0 : h % 5 !== 0

  if (!logEnabled) {
    return {
      ruleId,
      ruleName,
      chain,
      logEnabled: false,
      matchedEntries: 0,
      uniqueSources: 0,
      uniqueDestinations: 0,
      uniqueServices: 0,
      limit,
      truncated: false,
      scannedEntries: 3000 + (h % 7000),
      bufferOldestAt: isoMinutesAgo(120),
      sources: [],
      destinations: [],
      services: [],
    }
  }

  const ownAddressName = rule?.source?.[0]
  const ownServiceName = rule?.service?.[0]

  // uniqueX is deliberately larger than the returned list for at least one
  // category so truncated=true is exercised in mock mode too.
  const uniqueSources = 3 + (h % 15)
  const uniqueDestinations = 4 + ((h >> 2) % 20)
  const uniqueServices = 2 + ((h >> 4) % 6)

  const sourceCount = Math.min(limit, uniqueSources)
  const destCount = Math.min(limit, uniqueDestinations)
  const serviceCount = Math.min(limit, uniqueServices)

  const sources = Array.from({ length: sourceCount }, (_, i) => buildMockHit(h, i, ownAddressName))
    .sort((a, b) => b.count - a.count || a.ip.localeCompare(b.ip))
  const destinations = Array.from({ length: destCount }, (_, i) => buildMockHit(h + 101, i, undefined))
    .sort((a, b) => b.count - a.count || a.ip.localeCompare(b.ip))
  const services = Array.from({ length: serviceCount }, (_, i) => buildMockService(h, i, ownServiceName))
    .sort((a, b) => b.count - a.count || a.proto.localeCompare(b.proto))

  const truncated = uniqueSources > sourceCount || uniqueDestinations > destCount || uniqueServices > serviceCount

  return {
    ruleId,
    ruleName,
    chain,
    logEnabled: true,
    matchedEntries: sources.reduce((sum, s) => sum + s.count, 0),
    uniqueSources,
    uniqueDestinations,
    uniqueServices,
    limit,
    truncated,
    scannedEntries: 3000 + (h % 7000),
    bufferOldestAt: isoMinutesAgo(120),
    sources,
    destinations,
    services,
  }
}

async function parseError(response: Response, fallback: string): Promise<never> {
  const errBody = await response.json().catch(() => ({}))
  throw new Error(errBody.message || fallback)
}

export const policyEndpointsService = {
  // GET /api/policies/{ruleId}/endpoints?limit=N. limit defaults to 10 (must
  // be 1-50 server-side; the caller is responsible for passing a valid
  // value, mirroring the backend's own validation).
  getRuleEndpoints: async (ruleId: string, limit = 10): Promise<PolicyRuleEndpoints> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150))
      return buildMockEndpoints(ruleId, limit)
    }

    const response = await fetch(
      `${API_BASE_URL}/policies/${encodeURIComponent(ruleId)}/endpoints?limit=${encodeURIComponent(String(limit))}`
    )
    if (!response.ok) {
      await parseError(response, `Failed to fetch rule endpoints: ${response.statusText}`)
    }
    return response.json()
  },
}
