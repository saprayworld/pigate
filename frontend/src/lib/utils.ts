import { clsx, type ClassValue } from "clsx"
import { twMerge } from "tailwind-merge"

export function cn(...inputs: ClassValue[]) {
  return twMerge(clsx(inputs))
}

export function isValidIp(ip: string): boolean {
  const ipv4Regex = /^(?:(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)\.){3}(?:25[0-5]|2[0-4][0-9]|[01]?[0-9][0-9]?)$/
  return ipv4Regex.test(ip.trim())
}

export function ipToLong(ip: string): number {
  const parts = ip.trim().split(".").map(Number)
  return (parts[0] * 16777216) + (parts[1] * 65536) + (parts[2] * 256) + parts[3]
}

export function isValidCidr(cidr: string): boolean {
  const parts = cidr.trim().split("/")
  if (parts.length !== 2) return false
  const [ip, maskStr] = parts
  if (!isValidIp(ip)) return false
  const mask = Number(maskStr)
  if (isNaN(mask) || mask < 0 || mask > 32 || maskStr !== String(mask)) return false
  return true
}

export function isValidIpRange(range: string): boolean {
  const parts = range.split("-").map(p => p.trim())
  if (parts.length !== 2) return false
  const [start, end] = parts
  if (!isValidIp(start) || !isValidIp(end)) return false
  return ipToLong(start) <= ipToLong(end)
}

// Mirrors backend reZoneName (model.ValidateDhcpConfig / ValidateDNSZone):
// letters, digits, '.', '-' only, full-match (so no space/newline), max 253
// chars (RFC 1035). Used for the DHCP scope Domain field (option 15).
export function isValidDomain(domain: string): boolean {
  if (domain.length > 253) return false
  return /^[a-zA-Z0-9.-]+$/.test(domain)
}

