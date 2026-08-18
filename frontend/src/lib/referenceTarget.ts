// Classifies a table-cell value (an IP, a CIDR, a domain, or a "no
// popover" value like "ALL"/an Address Object name) for the reference popover
// (docs/ref/todo/reference-popover-plan.md Step 5) — deciding which content
// component ReferenceTrigger should render, and whether it should even bind a
// hover handler at all.
//
// IMPORTANT: same "UX guard only, NOT a security boundary" convention as
// classifyIpQuery (lib/ipQuery.ts) — the backend independently re-validates
// ip/domain (netip.ParseAddr / model.NormalizeQueryDomain) on every request
// this triggers. This module's only job is to avoid firing a request that's
// guaranteed to 400 and to decide the popover's shape.
//
// Deliberately reuses classifyIpQuery for IP detection rather than writing a
// second IPv4/IPv6 parser (plan Step 5: "ห้ามเขียน parser IP ใหม่").

import { classifyIpQuery } from "./ipQuery"

export type ReferenceTargetKind = "ip" | "cidr" | "domain" | "none"

export interface ReferenceTarget {
  kind: ReferenceTargetKind
  // Normalized value: lowercased IP/domain for "ip"/"domain", "ip/prefix"
  // for "cidr" (prefix normalized to a plain integer string), "" for "none".
  value: string
}

const NONE: ReferenceTarget = { kind: "none", value: "" }

// classifyReferenceTarget is the single entry point every caller (Logs,
// Policy, Statistics wiring) should use.
export function classifyReferenceTarget(raw: string): ReferenceTarget {
  const trimmed = raw.trim()
  if (trimmed === "") return NONE

  // "ALL" (the Policy page's wildcard address/service value) never gets a
  // popover — there is nothing to reference.
  if (trimmed.toUpperCase() === "ALL") return NONE

  const slashIdx = trimmed.indexOf("/")
  if (slashIdx !== -1) {
    const ipPart = trimmed.slice(0, slashIdx)
    const prefixPart = trimmed.slice(slashIdx + 1)
    const classified = classifyIpQuery(ipPart)
    if (classified.kind !== "ip") return NONE

    if (!/^\d{1,3}$/.test(prefixPart)) return NONE
    const prefix = Number(prefixPart)
    const isV6 = classified.ip.includes(":")
    const maxPrefix = isV6 ? 128 : 32
    if (prefix < 0 || prefix > maxPrefix) return NONE

    // /32 and /128 are a single host, not a range — treat as a plain "ip"
    // target so it gets the full IpReferenceContent (Domain/scope/etc.)
    // instead of the CIDR "range, no popover network call" treatment (plan
    // Step 5: "/32 และ /128 ตีเป็น ip").
    if (prefix === maxPrefix) {
      return { kind: "ip", value: classified.ip }
    }
    return { kind: "cidr", value: `${classified.ip}/${prefix}` }
  }

  const classified = classifyIpQuery(trimmed)
  if (classified.kind === "ip") return { kind: "ip", value: classified.ip }
  if (classified.kind === "ip-partial") {
    // A value that looks like it's still being typed as an IP (shouldn't
    // normally reach a rendered table cell, but if it does there is nothing
    // useful to hover) is not a valid reference target either way.
    return NONE
  }

  // Everything else classifyIpQuery calls "domain" is treated as a domain
  // here too, EXCEPT values that don't look like a real hostname at all
  // (e.g. an Address Object's display name with spaces) — those fall
  // through to "none" so ReferenceTrigger never fires a request that's
  // guaranteed to 400 model.NormalizeQueryDomain server-side.
  if (/^[a-z0-9*]([a-z0-9*_-]*[a-z0-9*])?(\.[a-z0-9*]([a-z0-9*_-]*[a-z0-9*])?)*$/i.test(trimmed)) {
    return { kind: "domain", value: trimmed.toLowerCase().replace(/\.$/, "") }
  }
  return NONE
}
