// Classifies the free-text query typed into the Statistics -> DNS page's
// "Top Domains" filter box as a domain search, a still-typing IP address, or
// a complete IP address (docs/ref/todo/statistics-dns-ip-filter-plan.md
// §2.3/T-07) — pure TypeScript, no React/JSX/DOM, same "single source of
// truth" convention as src/lib/statsWindow.ts.
//
// IMPORTANT: this is a UX guard only, NOT a security boundary. The backend
// (GET /api/statistics/dns/ip) always re-validates `ip` with Go's
// net/netip.ParseAddr before touching any service/index — this module's job
// is only to keep the frontend's classification in sync with what that
// backend call will accept/reject, so the UI doesn't fire a request that's
// guaranteed to 400, and so the empty-vs-partial-vs-complete states don't
// flicker while the user is still typing.

export type IpQueryKind = "domain" | "ip-partial" | "ip"

// isHexGroup matches one ':'-separated IPv6 group: 1-4 hex digits.
function isHexGroup(group: string): boolean {
  return /^[0-9a-f]{1,4}$/i.test(group)
}

// isCompleteIPv4 mirrors net/netip.ParseAddr's IPv4 rules closely enough for
// this UX guard: exactly 4 dot-separated groups, each 0-255, and NO leading
// zero (netip.ParseAddr rejects "192.168.01.1" — Caution 8: the frontend
// must agree, or the user gets an avoidable 400 after pressing enter).
function isCompleteIPv4(s: string): boolean {
  const parts = s.split(".")
  if (parts.length !== 4) return false
  for (const part of parts) {
    if (!/^\d{1,3}$/.test(part)) return false
    if (part.length > 1 && part[0] === "0") return false
    const n = Number(part)
    if (n < 0 || n > 255) return false
  }
  return true
}

// isCompleteIPv6 is a reasonably faithful (not exhaustive) implementation of
// RFC 4291 textual representation, including a single "::" compression and
// an optional trailing IPv4-mapped tail (e.g. "::ffff:192.168.1.1") — good
// enough to keep the UX guard from disagreeing with netip.ParseAddr in the
// common cases; any edge case it gets wrong just means one extra round trip
// to the backend, never a security issue (see module doc comment above).
function isCompleteIPv6(s: string): boolean {
  if ((s.match(/::/g) || []).length > 1) return false

  let head = s
  let ipv4GroupsUsed = 0

  // Peel off a trailing IPv4-mapped literal, if any (must be the last
  // ':'-separated segment).
  const lastColon = s.lastIndexOf(":")
  if (lastColon !== -1) {
    const tail = s.slice(lastColon + 1)
    if (tail.includes(".")) {
      if (!isCompleteIPv4(tail)) return false
      ipv4GroupsUsed = 2
      head = s.slice(0, lastColon)
    }
  }

  if (head === "") return false
  if (!/^[0-9a-f:]+$/i.test(head)) return false

  if (head.includes("::")) {
    const idx = head.indexOf("::")
    const left = head.slice(0, idx)
    const right = head.slice(idx + 2)
    const leftGroups = left === "" ? [] : left.split(":")
    const rightGroups = right === "" ? [] : right.split(":")
    if (leftGroups.some((g) => !isHexGroup(g))) return false
    if (rightGroups.some((g) => !isHexGroup(g))) return false
    const total = leftGroups.length + rightGroups.length + ipv4GroupsUsed
    // "::" must represent at least one all-zero group, so total groups
    // (excluding it) must be strictly less than 8.
    return total < 8
  }

  const groups = head.split(":")
  if (groups.some((g) => !isHexGroup(g))) return false
  return groups.length + ipv4GroupsUsed === 8
}

// isIPv4PartialChars: the user has typed only digits and dots (a v4 address
// still being typed), but it isn't complete/valid yet.
function isIPv4PartialChars(s: string): boolean {
  return /^[0-9.]+$/.test(s) && /\d/.test(s)
}

// isIPv6PartialChars: the user has typed a ':' plus only hex-ish characters
// (a v6 address still being typed, possibly with an IPv4-mapped tail), but
// it isn't complete/valid yet.
function isIPv6PartialChars(s: string): boolean {
  return s.includes(":") && /^[0-9a-f:.]+$/i.test(s)
}

// classifyIpQuery is the single entry point every caller (the DNS page,
// tests) should use. `ip` is only populated (trimmed + lowercased) when
// kind === "ip" — the normalized form to send to
// dnsStatisticsService.getIPDomains, matching the canonical form
// net/netip.Addr.String() produces server-side for the same input.
export function classifyIpQuery(raw: string): { kind: IpQueryKind; ip: string } {
  const trimmed = raw.trim()
  if (trimmed === "") {
    return { kind: "domain", ip: "" }
  }
  const lower = trimmed.toLowerCase()

  if (isCompleteIPv4(trimmed) || isCompleteIPv6(lower)) {
    return { kind: "ip", ip: lower }
  }
  if (isIPv4PartialChars(trimmed) || isIPv6PartialChars(lower)) {
    return { kind: "ip-partial", ip: "" }
  }
  return { kind: "domain", ip: "" }
}
