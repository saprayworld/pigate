import { IS_MOCK_MODE, API_BASE_URL } from "./config"

// Public IP Info card (docs/ref/todo/statistics-host-ipinfo-plan.md T-08) —
// GET /api/statistics/ipinfo?ip=… backend proxy in front of ipinfo.io.
// IpInfoLookup mirrors backend/internal/model/ipinfo.go's IPInfoLookup
// field-for-field; every field except `ip` is optional (omitempty on the Go
// side too), since the current provider (no token) doesn't guarantee any of
// them and a future provider swap may return a different subset (plan §7).

export interface IpInfoLookup {
  ip: string
  hostname?: string
  city?: string
  region?: string
  country?: string
  countryName?: string
  org?: string
  asn?: string
  asName?: string
  timezone?: string
  loc?: string
  source?: "cache" | "live"
  cachedAt?: string
}

// disabledSymbol distinguishes "the feature is off" (backend 404) from every
// other failure — the card renders a specific "disabled" state for this,
// never a red error (plan T-09 / T-08: "404 ต้องแปลเป็นสถานะ ฟีเจอร์ปิดอยู่").
export class IpInfoDisabledError extends Error {
  constructor() {
    super("ipinfo feature disabled")
    this.name = "IpInfoDisabledError"
  }
}

// --- Module-level cache (10 min TTL) ----------------------------------------
// The Host page auto-refreshes every 10s (StatisticsTrafficHost.tsx); without
// this cache every refresh tick would re-fetch the SAME ip, defeating the
// backend's own cache/rate-limit and making the UI feel laggy for no reason
// (plan T-08 explicit requirement).
const CACHE_TTL_MS = 10 * 60 * 1000

interface CacheEntry {
  value: IpInfoLookup | null // null = cached "disabled" result
  disabled: boolean
  expiresAt: number
}

const cache = new Map<string, CacheEntry>()

function getCached(ip: string): CacheEntry | undefined {
  const entry = cache.get(ip)
  if (!entry) return undefined
  if (Date.now() > entry.expiresAt) {
    cache.delete(ip)
    return undefined
  }
  return entry
}

function setCached(ip: string, value: IpInfoLookup | null, disabled: boolean) {
  cache.set(ip, { value, disabled, expiresAt: Date.now() + CACHE_TTL_MS })
}

// --- Mock data ---------------------------------------------------------------
// Deterministic fixtures for the IPs already used elsewhere in mock mode
// (trafficStatisticsService.ts's mockExtraDests) so drilling into them in
// `yarn dev` shows plausible data end to end.
const mockIpInfoData: Record<string, IpInfoLookup> = {
  "8.8.8.8": {
    ip: "8.8.8.8",
    hostname: "dns.google",
    city: "Mountain View",
    region: "California",
    country: "US",
    org: "AS15169 Google LLC",
    asn: "AS15169",
    asName: "Google LLC",
    timezone: "America/Los_Angeles",
    loc: "37.4056,-122.0775",
  },
  "142.250.196.14": {
    ip: "142.250.196.14",
    hostname: "142.250.196.14",
    city: "Mountain View",
    region: "California",
    country: "US",
    org: "AS15169 Google LLC",
    asn: "AS15169",
    asName: "Google LLC",
    timezone: "America/Los_Angeles",
  },
  "104.16.132.229": {
    ip: "104.16.132.229",
    city: "San Francisco",
    region: "California",
    country: "US",
    org: "AS13335 Cloudflare, Inc.",
    asn: "AS13335",
    asName: "Cloudflare, Inc.",
    timezone: "America/Los_Angeles",
  },
  "2606:4700:4700::1111": {
    ip: "2606:4700:4700::1111",
    hostname: "one.one.one.one",
    country: "AU",
    org: "AS13335 Cloudflare, Inc.",
    asn: "AS13335",
    asName: "Cloudflare, Inc.",
  },
}

function mockLookup(ip: string): IpInfoLookup {
  const fixture = mockIpInfoData[ip]
  if (fixture) return fixture
  return {
    ip,
    city: "Mockville",
    region: "Mock Region",
    country: "US",
    org: "AS64500 Mock Org",
    asn: "AS64500",
    asName: "Mock Org",
  }
}

// getIpInfo fetches Public IP Info for ip, using the module cache above.
// Encoding of `ip` happens HERE ONLY (plan T-08: "encode ที่นี่ที่เดียว —
// ห้าม double-encode") — callers must pass the raw, decoded ip string.
async function getIpInfo(ip: string): Promise<IpInfoLookup> {
  const cached = getCached(ip)
  if (cached) {
    if (cached.disabled) throw new IpInfoDisabledError()
    return cached.value as IpInfoLookup
  }

  if (IS_MOCK_MODE) {
    await new Promise((resolve) => setTimeout(resolve, 150))
    const result = mockLookup(ip)
    setCached(ip, result, false)
    return result
  }

  const response = await fetch(`${API_BASE_URL}/statistics/ipinfo?ip=${encodeURIComponent(ip)}`)
  if (response.status === 404) {
    setCached(ip, null, true)
    throw new IpInfoDisabledError()
  }
  if (!response.ok) {
    // Deliberately NOT cached — a transient failure (rate limit, upstream
    // timeout) should be retried on the next auto-refresh tick, not stick
    // around for the full 10-minute TTL.
    let message = `Failed to fetch ip info: ${response.statusText}`
    try {
      const body = await response.json()
      if (body?.message) message = body.message
    } catch {
      // ignore — keep the generic message
    }
    throw new Error(message)
  }

  const result: IpInfoLookup = await response.json()
  setCached(ip, result, false)
  return result
}

export const ipinfoService = {
  getIpInfo,
}
