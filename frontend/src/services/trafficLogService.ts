// Traffic Log API client (backend: /api/logs/traffic)
// Live PASS/DROP packet events from all three firewall chains (forward,
// input, output), served from a shared RAM ring buffer (never persisted).
// Mirrors the FirewallLog schema in openapi.yaml. Clearing reuses the shared
// dashboard-logs clear endpoint.
//
// Cursor pagination (see docs/ref/todo/traffic-log-pagination-and-local-traffic-plan.md
// §2.7): the server (and the mock feed below, to keep -mock dev testable)
// always filters the WHOLE buffer by chain/action/q FIRST, then locates the
// (beforeId, beforeTime) cursor within that filtered result, THEN cuts
// `limit` rows — never the other way around.

import { IS_MOCK_MODE, API_BASE_URL } from "./config";

export type TrafficAction = "PASS" | "DROP";
export type TrafficChain = "forward" | "input" | "output";
// Query-only chain filter value; "local" means input+output combined.
export type TrafficChainFilter = TrafficChain | "local" | "";

export interface TrafficLog {
  id: string;
  time: string; // RFC3339Nano UTC — convert to local time for display
  action: TrafficAction;
  src: string;
  dest: string;
  srcPort: string; // "-" for non-TCP/UDP or if unknown
  port: string; // destination port; "-" for non-TCP/UDP or if unknown
  proto: string;
  inIface: string; // ingress interface name ("-" if unknown)
  outIface: string; // egress interface name ("-" if unknown)
  reason: string;
  chain: TrafficChain;
  // ruleId/ruleName: snapshot-on-write (docs/ref/todo/
  // traffic-log-rule-name-and-domain-plan.md) — resolved once when the
  // entry was logged and never updated afterwards, so a rule rename/delete
  // never changes what an already-fetched row shows. Both omitted when the
  // matching rule/system token couldn't be resolved.
  ruleId?: string;
  ruleName?: string;
  // srcDomain/destDomain/srcHostname/destHostname: enrich-on-read, resolved
  // fresh by the backend on every request from the DNS query cache
  // (domain) with a DHCP lease/reservation fallback (hostname) — never
  // cached/snapshotted client-side either. Omitted when nothing resolves.
  srcDomain?: string;
  destDomain?: string;
  srcHostname?: string;
  destHostname?: string;
}

export interface TrafficLogQuery {
  action?: string; // "PASS" | "DROP" | "" (all)
  q?: string;
  chain?: TrafficChainFilter;
  limit?: number;
  beforeId?: string;
  beforeTime?: string;
}

// TrafficLogBufferUsage mirrors backend model.TrafficLogBufferUsage (GET
// /api/logs/traffic/usage, docs/ref/todo/
// firewall-log-buffer-capacity-plan.md T-08, issue #134) — the shared ring
// buffer's fill state, for the Forward/Local Traffic page header. Numbers
// are for the WHOLE buffer (all three chains share it), not scoped to
// whichever page is showing it.
export interface TrafficLogBufferUsage {
  used: number;
  capacity: number;
  usedPercent: number;
  oldestEntry?: string; // as-stored (RFC3339/RFC3339Nano); empty when the buffer is empty
  newestEntry?: string;
  evicted: number;
}

// ---------------------------------------------------------------------------
// Mock-mode data: a rolling in-memory feed so the page and its filters have
// something live to show without a backend. New entries prepend over time.
// Seeded generously (500+) so infinite-scroll can actually be exercised
// against -mock without a real kernel.
// ---------------------------------------------------------------------------

// Rule/domain/hostname fields below deliberately cover the 5 cases the UI
// (TrafficLogPage) needs to render correctly:
//   1. resolved rule name + resolved domain (row 1)
//   2. resolved rule name + resolved hostname, no domain (row 7 — LAN dest)
//   3. ruleId present but not resolved (deleted/renamed rule — muted display)
//      (row 3)
//   4. no rule id / name at all (row 6)
//   5. system token (structural log point, not a user rule) (row 10)
const MOCK_SAMPLES: Array<Omit<TrafficLog, "id" | "time">> = [
  { chain: "forward", action: "PASS", src: "192.168.1.105", dest: "8.8.8.8", srcPort: "51423", port: "53", proto: "UDP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)", ruleId: "rule-allow-dns", ruleName: "Allow DNS", destDomain: "dns.google" },
  { chain: "forward", action: "PASS", src: "192.168.1.112", dest: "142.250.80.46", srcPort: "54871", port: "443", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)", ruleId: "rule-allow-web", ruleName: "Allow Web Browsing", destDomain: "www.google.com" },
  { chain: "forward", action: "DROP", src: "192.168.1.133", dest: "185.220.101.4", srcPort: "49301", port: "23", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Blocked (forward)", ruleId: "rule-deleted-demo" },
  { chain: "forward", action: "PASS", src: "192.168.1.108", dest: "1.1.1.1", srcPort: "60102", port: "443", proto: "TCP", inIface: "wlan0", outIface: "eth1", reason: "Allowed (forward)", ruleId: "rule-allow-web", ruleName: "Allow Web Browsing", destDomain: "one.one.one.one" },
  { chain: "forward", action: "DROP", src: "192.168.1.140", dest: "45.13.104.9", srcPort: "52117", port: "3389", proto: "TCP", inIface: "wlan0", outIface: "eth1", reason: "Blocked (forward)", ruleId: "rule-block-rdp", ruleName: "Block RDP" },
  { chain: "forward", action: "PASS", src: "192.168.1.101", dest: "140.82.113.3", srcPort: "58221", port: "22", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)" },
  { chain: "input", action: "PASS", src: "192.168.1.10", dest: "192.168.1.1", srcPort: "51422", port: "443", proto: "TCP", inIface: "eth1", outIface: "-", reason: "Allowed (local-in)", ruleId: "sys-admin-https", ruleName: "System: Admin Access (HTTPS)", srcHostname: "laptop-office" },
  { chain: "input", action: "PASS", src: "192.168.1.20", dest: "192.168.1.1", srcPort: "58311", port: "22", proto: "TCP", inIface: "eth1", outIface: "-", reason: "Allowed (local-in)", ruleId: "sys-admin-ssh", ruleName: "System: Admin Access (SSH)", srcHostname: "admin-desktop" },
  { chain: "input", action: "PASS", src: "192.168.1.15", dest: "192.168.1.1", srcPort: "-", port: "-", proto: "ICMP", inIface: "eth1", outIface: "-", reason: "Allowed (local-in)", ruleId: "sys-admin-ping", ruleName: "System: Admin Access (Ping)" },
  { chain: "input", action: "DROP", src: "203.0.113.77", dest: "203.0.113.1", srcPort: "44502", port: "23", proto: "TCP", inIface: "eth0", outIface: "-", reason: "Blocked (local-in)", ruleId: "sys-input-defaultdrop", ruleName: "System: Default Drop (Local-In)" },
  { chain: "input", action: "DROP", src: "198.51.100.5", dest: "203.0.113.1", srcPort: "60123", port: "3389", proto: "TCP", inIface: "eth0", outIface: "-", reason: "Blocked (local-in)", ruleId: "sys-input-defaultdrop", ruleName: "System: Default Drop (Local-In)" },
  { chain: "output", action: "PASS", src: "203.0.113.1", dest: "8.8.8.8", srcPort: "51234", port: "53", proto: "UDP", inIface: "-", outIface: "eth0", reason: "Allowed (local-out)", destDomain: "dns.google" },
  { chain: "output", action: "PASS", src: "203.0.113.1", dest: "129.6.15.28", srcPort: "123", port: "123", proto: "UDP", inIface: "-", outIface: "eth0", reason: "Allowed (local-out)" },
];

// MOCK_CAPACITY is this mock feed's own stand-in "ring buffer capacity" — it
// is NOT the real backend's traffic-log-buffer-capacity default; the mock
// simply picks a smaller number so eviction is actually reachable in a dev
// session. Never surfaced as a literal in the UI — always sent through the
// `capacity` field of TrafficLogBufferUsage below, same as the real backend.
const MOCK_CAPACITY = 5000;

let mockLogs: TrafficLog[] | null = null;
let mockCounter = 0;
let mockEvicted = 0;

function seedMockLogs(): TrafficLog[] {
  if (mockLogs) return mockLogs;
  const now = Date.now();
  mockLogs = [];
  // Seed enough rows to exercise multiple pages of infinite scroll (500/page).
  const seedCount = 900;
  for (let i = 0; i < seedCount; i++) {
    const s = MOCK_SAMPLES[i % MOCK_SAMPLES.length];
    mockLogs.push({
      id: `mock-traffic-${mockCounter++}`,
      time: new Date(now - i * 5000).toISOString(),
      ...s,
    });
  }
  return mockLogs;
}

// Prepend a fresh entry roughly once per call so polling shows movement.
function advanceMockLogs() {
  const logs = seedMockLogs();
  const s = MOCK_SAMPLES[Math.floor(Math.random() * MOCK_SAMPLES.length)];
  logs.unshift({
    ...s,
    id: `mock-traffic-${mockCounter++}`,
    time: new Date().toISOString(),
    src: s.chain === "output" ? s.src : `192.168.1.${100 + Math.floor(Math.random() * 50)}`,
  });
  // Cap generously — this is an in-memory dev feed, not the real ring
  // buffer's configurable capacity, but should comfortably outlast a manual
  // scroll test. Anything trimmed off the tail counts as "evicted", mirroring
  // the real ring buffer's oldest-first eviction.
  if (logs.length > MOCK_CAPACITY) {
    mockEvicted += logs.length - MOCK_CAPACITY;
    logs.length = MOCK_CAPACITY;
  }
}

function chainMatches(entryChain: TrafficChain, filter: TrafficChainFilter): boolean {
  if (!filter) return true;
  if (filter === "local") return entryChain === "input" || entryChain === "output";
  return entryChain === filter;
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

export const trafficLogService = {
  getTrafficLogs: async (query: TrafficLogQuery = {}): Promise<TrafficLog[]> => {
    const { action = "", q = "", chain = "", limit = 100, beforeId = "", beforeTime = "" } = query;

    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 120));
      advanceMockLogs();
      const wantAction = action.toUpperCase();
      const needle = q.toLowerCase();

      // Filter the WHOLE feed first (mirrors backend contract).
      const matched = seedMockLogs().filter(
        (l) =>
          (!wantAction || l.action === wantAction) &&
          chainMatches(l.chain, chain) &&
          (!needle ||
            [l.src, l.dest, l.srcPort, l.port, l.proto, l.inIface, l.outIface, l.reason, l.chain].some((s) =>
              s.toLowerCase().includes(needle)
            ))
      );

      // Then locate the cursor within the filtered result.
      let start = 0;
      if (beforeId) {
        const idx = matched.findIndex((l) => l.id === beforeId);
        if (idx >= 0) {
          start = idx + 1;
        } else if (beforeTime) {
          const cutoff = Date.parse(beforeTime);
          if (Number.isNaN(cutoff)) return [];
          return matched.filter((l) => Date.parse(l.time) < cutoff).slice(0, limit);
        } else {
          return [];
        }
      }
      return matched.slice(start, start + limit);
    }

    const params = new URLSearchParams();
    if (action) params.set("action", action);
    if (q) params.set("q", q);
    if (chain) params.set("chain", chain);
    params.set("limit", String(limit));
    if (beforeId) params.set("beforeId", beforeId);
    if (beforeTime) params.set("beforeTime", beforeTime);

    const response = await fetch(`${API_BASE_URL}/logs/traffic?${params.toString()}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch traffic logs: ${response.statusText}`);
    }
    return response.json();
  },

  // Clears the shared RAM buffer (same endpoint the Dashboard uses).
  clearTrafficLogs: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      mockLogs = [];
      mockEvicted = 0;
      return;
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/logs/clear`, { method: "POST" });
    if (!response.ok) {
      throw new Error(`Failed to clear traffic logs: ${response.statusText}`);
    }
  },

  // Backs the Forward/Local Traffic page's "used X / capacity (Y%) · oldest
  // entry" summary bar (docs/ref/todo/firewall-log-buffer-capacity-plan.md
  // T-08, issue #134).
  getTrafficLogUsage: async (): Promise<TrafficLogBufferUsage> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 60));
      const logs = seedMockLogs(); // newest-first, same convention as the real ring buffer's GetAll()
      const used = logs.length;
      const capacity = MOCK_CAPACITY;
      return {
        used,
        capacity,
        usedPercent: capacity > 0 ? (used / capacity) * 100 : 0,
        oldestEntry: used > 0 ? logs[used - 1].time : undefined,
        newestEntry: used > 0 ? logs[0].time : undefined,
        evicted: mockEvicted,
      };
    }

    const response = await fetch(`${API_BASE_URL}/logs/traffic/usage`);
    if (!response.ok) {
      throw new Error(`Failed to fetch traffic log usage: ${response.statusText}`);
    }
    return response.json();
  },
};
