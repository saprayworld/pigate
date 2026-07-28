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
  port: string;
  proto: string;
  inIface: string; // ingress interface name ("-" if unknown)
  outIface: string; // egress interface name ("-" if unknown)
  reason: string;
  chain: TrafficChain;
}

export interface TrafficLogQuery {
  action?: string; // "PASS" | "DROP" | "" (all)
  q?: string;
  chain?: TrafficChainFilter;
  limit?: number;
  beforeId?: string;
  beforeTime?: string;
}

// ---------------------------------------------------------------------------
// Mock-mode data: a rolling in-memory feed so the page and its filters have
// something live to show without a backend. New entries prepend over time.
// Seeded generously (500+) so infinite-scroll can actually be exercised
// against -mock without a real kernel.
// ---------------------------------------------------------------------------

const MOCK_SAMPLES: Array<Omit<TrafficLog, "id" | "time">> = [
  { chain: "forward", action: "PASS", src: "192.168.1.105", dest: "8.8.8.8", port: "53", proto: "UDP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)" },
  { chain: "forward", action: "PASS", src: "192.168.1.112", dest: "142.250.80.46", port: "443", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)" },
  { chain: "forward", action: "DROP", src: "192.168.1.133", dest: "185.220.101.4", port: "23", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Blocked (forward)" },
  { chain: "forward", action: "PASS", src: "192.168.1.108", dest: "1.1.1.1", port: "443", proto: "TCP", inIface: "wlan0", outIface: "eth1", reason: "Allowed (forward)" },
  { chain: "forward", action: "DROP", src: "192.168.1.140", dest: "45.13.104.9", port: "3389", proto: "TCP", inIface: "wlan0", outIface: "eth1", reason: "Blocked (forward)" },
  { chain: "forward", action: "PASS", src: "192.168.1.101", dest: "140.82.113.3", port: "22", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)" },
  { chain: "input", action: "PASS", src: "192.168.1.10", dest: "192.168.1.1", port: "443", proto: "TCP", inIface: "eth1", outIface: "-", reason: "Allowed (local-in)" },
  { chain: "input", action: "PASS", src: "192.168.1.20", dest: "192.168.1.1", port: "22", proto: "TCP", inIface: "eth1", outIface: "-", reason: "Allowed (local-in)" },
  { chain: "input", action: "PASS", src: "192.168.1.15", dest: "192.168.1.1", port: "-", proto: "ICMP", inIface: "eth1", outIface: "-", reason: "Allowed (local-in)" },
  { chain: "input", action: "DROP", src: "203.0.113.77", dest: "203.0.113.1", port: "23", proto: "TCP", inIface: "eth0", outIface: "-", reason: "Blocked (local-in)" },
  { chain: "input", action: "DROP", src: "198.51.100.5", dest: "203.0.113.1", port: "3389", proto: "TCP", inIface: "eth0", outIface: "-", reason: "Blocked (local-in)" },
  { chain: "output", action: "PASS", src: "203.0.113.1", dest: "8.8.8.8", port: "53", proto: "UDP", inIface: "-", outIface: "eth0", reason: "Allowed (local-out)" },
  { chain: "output", action: "PASS", src: "203.0.113.1", dest: "129.6.15.28", port: "123", proto: "UDP", inIface: "-", outIface: "eth0", reason: "Allowed (local-out)" },
];

let mockLogs: TrafficLog[] | null = null;
let mockCounter = 0;

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
    id: `mock-traffic-${mockCounter++}`,
    time: new Date().toISOString(),
    src: s.chain === "output" ? s.src : `192.168.1.${100 + Math.floor(Math.random() * 50)}`,
    dest: s.dest,
    port: s.port,
    proto: s.proto,
    inIface: s.inIface,
    outIface: s.outIface,
    action: s.action,
    reason: s.reason,
    chain: s.chain,
  });
  // Cap generously — this is an in-memory dev feed, not the real 10,000-entry
  // ring buffer, but should comfortably outlast a manual scroll test.
  if (logs.length > 5000) logs.length = 5000;
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
            [l.src, l.dest, l.port, l.proto, l.inIface, l.outIface, l.reason, l.chain].some((s) =>
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
      return;
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/logs/clear`, { method: "POST" });
    if (!response.ok) {
      throw new Error(`Failed to clear traffic logs: ${response.statusText}`);
    }
  },
};
