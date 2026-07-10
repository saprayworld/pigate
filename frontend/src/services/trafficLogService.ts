// Forward Traffic Log API client (backend: /api/logs/traffic)
// Live PASS/DROP packet events from the firewall forward chain, served from a
// RAM ring buffer (never persisted). Mirrors the FirewallLog schema in
// openapi.yaml. Clearing reuses the shared dashboard-logs clear endpoint.

import { IS_MOCK_MODE, API_BASE_URL } from "./config";

export type TrafficAction = "PASS" | "DROP";

export interface TrafficLog {
  id: string;
  time: string; // RFC3339 UTC — convert to local time for display
  action: TrafficAction;
  src: string;
  dest: string;
  port: string;
  proto: string;
  inIface: string; // ingress interface name ("-" if unknown)
  outIface: string; // egress interface name ("-" if unknown)
  reason: string;
}

export interface TrafficLogQuery {
  action?: string; // "PASS" | "DROP" | "" (all)
  q?: string;
  limit?: number;
}

// ---------------------------------------------------------------------------
// Mock-mode data: a rolling in-memory feed so the page and its filters have
// something live to show without a backend. New entries prepend over time.
// ---------------------------------------------------------------------------

const MOCK_SAMPLES: Array<Omit<TrafficLog, "id" | "time">> = [
  { action: "PASS", src: "192.168.1.105", dest: "8.8.8.8", port: "53", proto: "UDP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)" },
  { action: "PASS", src: "192.168.1.112", dest: "142.250.80.46", port: "443", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)" },
  { action: "DROP", src: "192.168.1.133", dest: "185.220.101.4", port: "23", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Blocked (forward)" },
  { action: "PASS", src: "192.168.1.108", dest: "1.1.1.1", port: "443", proto: "TCP", inIface: "wlan0", outIface: "eth1", reason: "Allowed (forward)" },
  { action: "DROP", src: "192.168.1.140", dest: "45.13.104.9", port: "3389", proto: "TCP", inIface: "wlan0", outIface: "eth1", reason: "Blocked (forward)" },
  { action: "PASS", src: "192.168.1.101", dest: "140.82.113.3", port: "22", proto: "TCP", inIface: "eth0", outIface: "eth1", reason: "Allowed (forward)" },
];

let mockLogs: TrafficLog[] | null = null;
let mockCounter = 0;

function seedMockLogs(): TrafficLog[] {
  if (mockLogs) return mockLogs;
  const now = Date.now();
  mockLogs = [];
  for (let i = 0; i < 20; i++) {
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
    src: `192.168.1.${100 + Math.floor(Math.random() * 50)}`,
    dest: s.dest,
    port: s.port,
    proto: s.proto,
    inIface: s.inIface,
    outIface: s.outIface,
    action: s.action,
    reason: s.reason,
  });
  if (logs.length > 500) logs.length = 500;
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

export const trafficLogService = {
  getTrafficLogs: async (query: TrafficLogQuery = {}): Promise<TrafficLog[]> => {
    const { action = "", q = "", limit = 100 } = query;

    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 120));
      advanceMockLogs();
      const wantAction = action.toUpperCase();
      const needle = q.toLowerCase();
      return seedMockLogs()
        .filter(
          (l) =>
            (!wantAction || l.action === wantAction) &&
            (!needle ||
              [l.src, l.dest, l.port, l.proto, l.inIface, l.outIface, l.reason].some((s) =>
                s.toLowerCase().includes(needle)
              ))
        )
        .slice(0, limit);
    }

    const params = new URLSearchParams();
    if (action) params.set("action", action);
    if (q) params.set("q", q);
    params.set("limit", String(limit));

    const response = await fetch(`${API_BASE_URL}/logs/traffic?${params.toString()}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch forward traffic logs: ${response.statusText}`);
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
