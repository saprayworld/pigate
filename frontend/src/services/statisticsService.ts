import { IS_MOCK_MODE, API_BASE_URL } from "./config";

// Statistics page DTOs (docs/ref/todo/statistics-page-plan.md) — backs the
// standalone /logs/statistics page. Types mirror backend/internal/model/statistics.go
// field-for-field; do not let these drift from the Go structs.

export interface TopHost {
  ip: string;
  hostname: string;
  mac: string;
  bytes: number;
  percent: number;
  // True when ip is a LAN address (RFC1918/link-local/loopback/ULA). A
  // conversation initiated from the internet can appear as "source" too
  // (conntrack Forward tuple) — false in that case.
  private: boolean;
}

export interface TopConversation {
  srcIp: string;
  srcHostname: string;
  dstIp: string;
  dstHostname: string;
  // "TCP"/"UDP"/"ICMP", or "IP:<n>" for anything else.
  proto: string;
  dstPort: number;
  bytes: number;
  percent: number;
}

export interface TopDeniedSource {
  ip: string;
  hostname: string;
  count: number;
  percent: number;
}

export interface TopDeniedPort {
  proto: string;
  port: string;
  count: number;
  percent: number;
}

export interface TrafficStatistics {
  window: "1h" | "24h";
  observedBytes: number;
  accuracy: "estimated" | "near-exact";
  topSources: TopHost[];
  topDestinations: TopHost[];
  topConversations: TopConversation[];
  deniedSources: TopDeniedSource[];
  deniedPorts: TopDeniedPort[];
  // Always true: deniedSources/deniedPorts come from a rate-limited nftables
  // log rule, so they're a sampled approximation, never an exact packet
  // count — never mix them into observedBytes-based percentages.
  deniedSampled: boolean;
  deniedEvents: number;
  // True when any per-bucket tracking map hit its cap during this window —
  // the ranking may be missing entries.
  truncated: boolean;
  generatedAt: string;
}

// mockHosts reuses the same LAN IPs as MockDhcp's hardcoded lease list
// (kernel/mock.go mockFlowTemplates: 192.168.1.101/102/105 -
// iPhone-13/Android-SmartTV/iPad-Pro) so dev mode shows real device names,
// matching the real -mock=true backend.
const mockHosts = [
  { ip: "192.168.1.102", hostname: "Android-SmartTV", mac: "AA:BB:CC:DD:EE:FF" },
  { ip: "192.168.1.101", hostname: "iPhone-13", mac: "99:88:77:66:55:44" },
  { ip: "192.168.1.105", hostname: "iPad-Pro", mac: "B4:F1:DA:C8:E2:10" },
];

const mockDests = [
  { ip: "173.194.76.94", hostname: "173.194.76.94" }, // Android-SmartTV: HTTPS video
  { ip: "142.250.80.46", hostname: "142.250.80.46" }, // iPhone-13: HTTPS streaming
  { ip: "151.101.1.69", hostname: "151.101.1.69" }, // iPad-Pro: HTTP/HTTPS browsing
  { ip: "8.8.8.8", hostname: "8.8.8.8" }, // DNS
  { ip: "64.233.166.127", hostname: "64.233.166.127" }, // VoIP/SIP
];

function mockTopHosts(scale: number, source: { ip: string; hostname: string; mac: string }[]): TopHost[] {
  const rows = source.map((h, i) => ({
    ...h,
    bytes: Math.round((3_000_000_000 - i * 900_000_000) * scale),
  }));
  const total = rows.reduce((sum, r) => sum + r.bytes, 0);
  return rows.map((r) => ({
    ip: r.ip,
    hostname: r.hostname,
    mac: r.mac,
    bytes: r.bytes,
    percent: Math.round((r.bytes / total) * 1000) / 10,
    private: r.ip.startsWith("192.168.") || r.ip.startsWith("10.") || r.ip.startsWith("172."),
  }));
}

function mockTopConversations(scale: number): TopConversation[] {
  const raw = [
    { srcIp: "192.168.1.102", srcHostname: "Android-SmartTV", dstIp: "173.194.76.94", dstHostname: "173.194.76.94", proto: "TCP", dstPort: 443, bytes: 26_000_000 * scale },
    { srcIp: "192.168.1.101", srcHostname: "iPhone-13", dstIp: "142.250.80.46", dstHostname: "142.250.80.46", proto: "TCP", dstPort: 443, bytes: 9_000_000 * scale },
    { srcIp: "192.168.1.105", srcHostname: "iPad-Pro", dstIp: "151.101.1.69", dstHostname: "151.101.1.69", proto: "TCP", dstPort: 443, bytes: 4_500_000 * scale },
    { srcIp: "192.168.1.105", srcHostname: "iPad-Pro", dstIp: "151.101.1.69", dstHostname: "151.101.1.69", proto: "TCP", dstPort: 80, bytes: 3_000_000 * scale },
    { srcIp: "192.168.1.102", srcHostname: "Android-SmartTV", dstIp: "64.233.166.127", dstHostname: "64.233.166.127", proto: "UDP", dstPort: 5060, bytes: 1_200_000 * scale },
  ];
  const total = raw.reduce((sum, r) => sum + r.bytes, 0);
  return raw.map((r) => ({ ...r, percent: Math.round((r.bytes / total) * 1000) / 10 }));
}

function mockDeniedSources(scale: number): TopDeniedSource[] {
  const raw = [
    { ip: "203.0.113.9", hostname: "203.0.113.9", count: Math.round(42 * scale) },
    { ip: "203.0.113.44", hostname: "203.0.113.44", count: Math.round(18 * scale) },
    { ip: "198.51.100.201", hostname: "198.51.100.201", count: Math.round(6 * scale) },
  ];
  const total = raw.reduce((sum, r) => sum + r.count, 0) || 1;
  return raw.map((r) => ({ ...r, percent: Math.round((r.count / total) * 1000) / 10 }));
}

function mockDeniedPorts(scale: number): TopDeniedPort[] {
  const raw = [
    { proto: "TCP", port: "22", count: Math.round(30 * scale) },
    { proto: "TCP", port: "23", count: Math.round(20 * scale) },
    { proto: "UDP", port: "3389", count: Math.round(12 * scale) },
  ];
  const total = raw.reduce((sum, r) => sum + r.count, 0) || 1;
  return raw.map((r) => ({ ...r, percent: Math.round((r.count / total) * 1000) / 10 }));
}

export const statisticsService = {
  // Get the Statistics page cards (Top Source Hosts / Top Destinations / Top
  // Conversations / Top Denied). window defaults to "1h".
  getTrafficStatistics: async (window: "1h" | "24h" = "1h"): Promise<TrafficStatistics> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      const scale = window === "24h" ? 18 : 1;
      const topSources = mockTopHosts(scale, mockHosts);
      const topDestinations = mockTopHosts(scale, mockDests.map((d) => ({ ...d, mac: "" })));
      const observedBytes = topSources.reduce((sum, h) => sum + h.bytes, 0);
      const deniedSources = mockDeniedSources(scale);
      const deniedEvents = deniedSources.reduce((sum, d) => sum + d.count, 0);

      return {
        window,
        observedBytes,
        // Mock's flow-end event watcher never fails to "subscribe", so it
        // always reports the phase-2 "near-exact" accuracy — same as
        // dashboardService's mock branch.
        accuracy: "near-exact",
        topSources,
        topDestinations,
        topConversations: mockTopConversations(scale),
        deniedSources,
        deniedPorts: mockDeniedPorts(scale),
        deniedSampled: true,
        deniedEvents,
        truncated: false,
        generatedAt: new Date().toISOString(),
      };
    }

    const response = await fetch(`${API_BASE_URL}/statistics/traffic?window=${window}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch traffic statistics: ${response.statusText}`);
    }
    return response.json();
  },
};
