import {
  type FirewallLog,
  initialFirewallLogs,
  mockSources,
  mockDestinations,
  mockLogServices,
} from "@/data-mockup/mockData";
import { IS_MOCK_MODE, API_BASE_URL } from "./config";

export interface TrafficData {
  time: string;
  inbound: number;
  outbound: number;
}

/**
 * A log entry as delivered over the SSE stream — the full backend
 * model.FirewallLog shape (superset of the Dashboard's FirewallLog and the
 * Forward Traffic TrafficLog). inIface/outIface are optional so a Dashboard
 * consumer that ignores them stays type-compatible.
 */
export interface SSELogEntry extends FirewallLog {
  inIface?: string;
  outIface?: string;
  // Which nftables chain produced this entry ("forward" | "input" | "output").
  // Optional so a Dashboard consumer that predates this field stays
  // type-compatible; treat a missing value as "unknown".
  chain?: string;
}

export interface CpuDetail {
  usagePercent: number;
  cores: number;
  modelName: string;
  freqMhz: number;
  freqAvailable: boolean;
}

export interface MemDetail {
  usedBytes: number;
  totalBytes: number;
  percent: number;
}

export interface TempDetail {
  celsius: number;
  throttleCelsius: number;
  available: boolean;
}

export interface StorageDetail {
  path: string;
  usedBytes: number;
  totalBytes: number;
  percent: number;
}

// SessionCounts is the conntrack session snapshot shared by PerformanceMetrics
// (SSE push, ~5s) and TrafficDetail (poll, 60s). Total/Max/Available are fresh
// (~5s); TCP/UDP/ICMP/Other come from the conntrack dump the traffic-detail
// poller already does every ~10s, so they can lag Total slightly and are
// capped server-side (protoCapped=true when the dump hit that cap) — do NOT
// stack the per-proto counts on top of total in the same chart line.
export interface SessionCounts {
  total: number;
  tcp: number;
  udp: number;
  icmp: number;
  other: number;
  max: number;
  available: boolean;
  protoSampledAt: string;
  protoCapped: boolean;
}

// SessionHistoryPoint is one sample in the Active Sessions history seeded
// from TrafficDetail.sessionHistory (RAM-only ring on the backend).
export interface SessionHistoryPoint {
  t: string;
  total: number;
  tcp: number;
  udp: number;
  icmp: number;
  other: number;
}

export interface PerformanceMetrics {
  // Flat fields retained for backward-compatibility.
  cpu: number;
  memory: number;
  temp: number;
  // Richer detail objects used by the redesigned dashboard.
  cpuDetail: CpuDetail;
  memDetail: MemDetail;
  tempDetail: TempDetail;
  storage: StorageDetail;
  sessions: SessionCounts;
}

export interface DashboardStats {
  firewallStatus: string;
  totalTrafficInBytes: number;
  totalTrafficOutBytes: number;
  dhcpLeasesCount: number;
  wifiStatus: string;
  wifiSSID: string;
}

export interface SystemInfo {
  hostname: string;
  version: string;
  osName: string;
  boardModel?: string;
  kernelVersion?: string;
  uptimeSeconds: number;
  systemTime: string;
  timezone: string;
}

export interface TrafficBucket {
  ts: string;
  rxBytes: number;
  txBytes: number;
}

export interface TrafficHistory {
  interfaces: string[];
  buckets: TrafficBucket[];
}

export interface TrafficCategorySlice {
  name: string;
  bytes: number;
  percent: number;
}

export interface TopTalker {
  ip: string;
  hostname: string;
  mac: string;
  bytes: number;
  percent: number;
}

export interface TopRule {
  ruleId: string;
  name: string;
  chain: string;
  action: string;
  bytes: number;
  packets: number;
  percent: number;
}

export interface TrafficDetail {
  window: "1h" | "24h";
  observedBytes: number;
  estimated: boolean;
  // "estimated": categories/topTalkers come from the 10s conntrack poll
  // alone. "near-exact": the conntrack DESTROY event listener is also
  // active, crediting each flow's final byte count at teardown (see
  // docs/openapi.yaml TrafficDetail.accuracy).
  accuracy: "estimated" | "near-exact";
  categories: TrafficCategorySlice[];
  topTalkers: TopTalker[];
  topRules: TopRule[];
  generatedAt: string;
  sessions: SessionCounts;
  sessionHistory: SessionHistoryPoint[];
}

// mockSessionCounts derives a SessionCounts snapshot from a given total,
// splitting it into the usual ~70/25/2/3 TCP/UDP/ICMP/Other proportions used
// by every mock branch that needs one (getPerformanceMetrics/getTrafficDetail/
// SSE), so the badges and the chart line stay roughly consistent with total.
function mockSessionCounts(total: number): SessionCounts {
  const t = Math.max(0, Math.round(total));
  const tcp = Math.round(t * 0.7);
  const udp = Math.round(t * 0.25);
  const icmp = Math.round(t * 0.02);
  const other = Math.max(0, t - tcp - udp - icmp);
  return {
    total: t,
    tcp,
    udp,
    icmp,
    other,
    max: 262144,
    available: true,
    protoSampledAt: new Date().toISOString(),
    protoCapped: false,
  };
}

// mockSessionHistory synthesizes 360 points (5s apart, 30 minutes) of a
// smooth Active Sessions wave, used to seed the mock getTrafficDetail
// sessionHistory field the same way the real backend's RAM ring buffer would.
function mockSessionHistory(): SessionHistoryPoint[] {
  const now = Date.now();
  const points: SessionHistoryPoint[] = [];
  for (let i = 359; i >= 0; i--) {
    const ts = new Date(now - i * 5000);
    const total = Math.round(420 + 120 * Math.sin(((359 - i) / 360) * Math.PI * 2));
    const counts = mockSessionCounts(total);
    points.push({
      t: ts.toISOString(),
      total: counts.total,
      tcp: counts.tcp,
      udp: counts.udp,
      icmp: counts.icmp,
      other: counts.other,
    });
  }
  return points;
}

const LOGS_STORAGE_KEY = "pigate_dashboard_logs";

function getLocalLogs(): FirewallLog[] {
  const stored = localStorage.getItem(LOGS_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOGS_STORAGE_KEY, JSON.stringify(initialFirewallLogs));
    return initialFirewallLogs;
  }
  try {
    return JSON.parse(stored);
  } catch {
    return initialFirewallLogs;
  }
}

function saveLocalLogs(logs: FirewallLog[]) {
  localStorage.setItem(LOGS_STORAGE_KEY, JSON.stringify(logs));
}

export const dashboardService = {
  // Get main dashboard general statistics
  getStats: async (): Promise<DashboardStats> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      // Read leases from LocalStorage to make dhcp client count dynamic
      const leasesStored = localStorage.getItem("pigate_dhcp_leases");
      let leaseCount = 18; // default mock fallback
      if (leasesStored) {
        try {
          leaseCount = JSON.parse(leasesStored).length;
        } catch {
          // ignore malformed lease cache, fall back to default count
        }
      }
      return {
        firewallStatus: "Active",
        totalTrafficInBytes: 9_345_678_901,
        totalTrafficOutBytes: 3_987_654_321,
        dhcpLeasesCount: leaseCount,
        wifiStatus: "wlan0 Master",
        wifiSSID: "PiGate-Secure",
      };
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/stats`);
    if (!response.ok) {
      throw new Error(`Failed to fetch dashboard stats: ${response.statusText}`);
    }
    return response.json();
  },

  // Get live CPU, RAM, Temp metrics
  getPerformanceMetrics: async (): Promise<PerformanceMetrics> => {
    if (IS_MOCK_MODE) {
      // Simulate slight network latency
      await new Promise((resolve) => setTimeout(resolve, 150));
      // Generate randomized values matching typical loads
      const cpu = Math.round((12 + Math.random() * 10) * 10) / 10;
      const memPct = Math.round((45 + Math.random() * 15) * 10) / 10;
      const temp = Math.round((47.5 + Math.random() * 2) * 10) / 10;
      const totalMem = 8 * 1024 ** 3;
      const sessTotal = Math.round(420 + 120 * Math.sin(Date.now() / 20000));
      return {
        cpu,
        memory: memPct,
        temp,
        cpuDetail: {
          usagePercent: cpu,
          cores: 4,
          modelName: "Cortex-A76 (mock)",
          freqMhz: 2400,
          freqAvailable: true,
        },
        memDetail: {
          usedBytes: Math.round((totalMem * memPct) / 100),
          totalBytes: totalMem,
          percent: memPct,
        },
        tempDetail: { celsius: temp, throttleCelsius: 80, available: true },
        storage: {
          path: "/",
          usedBytes: Math.round(128 * 1024 ** 3 * 0.32),
          totalBytes: 128 * 1024 ** 3,
          percent: 32,
        },
        sessions: mockSessionCounts(sessTotal),
      };
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/performance`);
    if (!response.ok) {
      throw new Error(`Failed to fetch performance metrics: ${response.statusText}`);
    }
    return response.json();
  },

  // Get host identity for the System Information card
  getSystemInfo: async (): Promise<SystemInfo> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      return {
        hostname: "PiGate-RPI5",
        version: "mock",
        osName: "Raspberry Pi OS (64-bit) (mock)",
        boardModel: "Raspberry Pi 5 Model B (mock)",
        kernelVersion: "6.6.31-mock",
        uptimeSeconds: 273153,
        systemTime: new Date().toISOString(),
        timezone: "Asia/Bangkok",
      };
    }

    const response = await fetch(`${API_BASE_URL}/system/info`);
    if (!response.ok) {
      throw new Error(`Failed to fetch system info: ${response.statusText}`);
    }
    return response.json();
  },

  // Get WAN bandwidth history (RAM ring buffer of 5-minute buckets)
  getTrafficHistory: async (): Promise<TrafficHistory> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      const now = Date.now();
      const buckets: TrafficBucket[] = [];
      for (let i = 47; i >= 0; i--) {
        const ts = new Date(now - i * 5 * 60 * 1000);
        const load = 0.5 + Math.sin((i / 48) * Math.PI * 2) * 0.5;
        buckets.push({
          ts: ts.toISOString(),
          rxBytes: Math.round((2 + load * 8) * 1024 ** 2),
          txBytes: Math.round((0.5 + load * 2) * 1024 ** 2),
        });
      }
      return { interfaces: ["eth0"], buckets };
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/traffic`);
    if (!response.ok) {
      throw new Error(`Failed to fetch traffic history: ${response.statusText}`);
    }
    return response.json();
  },

  // Get the Dashboard "Detailed" tab traffic-analytics cards (Protocol
  // Breakdown / Top Talkers / Top Rules by Traffic). window defaults to "1h".
  // Categories/TopTalkers are estimates (conntrack-derived, see `estimated`);
  // TopRules is exact (nftables' own per-rule counter).
  getTrafficDetail: async (window: "1h" | "24h" = "1h"): Promise<TrafficDetail> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      // Deterministic-ish mock numbers scaled by window so the two ranges
      // visibly differ, matching the shape of the real 5-minute-bucket data.
      const scale = window === "24h" ? 22 : 1;
      const categoriesRaw = [
        { name: "HTTPS", bytes: 5_120_000_000 * scale },
        { name: "DNS", bytes: 640_000_000 * scale },
        { name: "VoIP", bytes: 410_000_000 * scale },
        { name: "Other", bytes: 2_050_000_000 * scale },
      ];
      const observedBytes = categoriesRaw.reduce((sum, c) => sum + c.bytes, 0);
      const categories: TrafficCategorySlice[] = categoriesRaw.map((c) => ({
        ...c,
        percent: Math.round((c.bytes / observedBytes) * 1000) / 10,
      }));

      const talkersRaw = [
        { ip: "192.168.1.50", hostname: "NAS-Server", mac: "AA:11:22:33:44:01", bytes: 4_800_000_000 * scale },
        { ip: "192.168.1.101", hostname: "iPhone-13", mac: "99:88:77:66:55:44", bytes: 2_100_000_000 * scale },
        { ip: "192.168.1.60", hostname: "MacBook-Pro", mac: "AA:11:22:33:44:02", bytes: 1_650_000_000 * scale },
        { ip: "192.168.1.102", hostname: "Android-SmartTV", mac: "AA:BB:CC:DD:EE:FF", bytes: 890_000_000 * scale },
        { ip: "192.168.1.105", hostname: "iPad-Pro", mac: "B4:F1:DA:C8:E2:10", bytes: 320_000_000 * scale },
      ];
      const talkersTotal = talkersRaw.reduce((sum, t) => sum + t.bytes, 0);
      const topTalkers: TopTalker[] = talkersRaw.map((t) => ({
        ...t,
        percent: Math.round((t.bytes / talkersTotal) * 1000) / 10,
      }));

      const rulesRaw = [
        { ruleId: "mock-rule-1", name: "Allow LAN to WAN", chain: "forward", action: "ACCEPT", bytes: 6_400_000_000 * scale, packets: 5_200_000 * scale },
        { ruleId: "mock-rule-2", name: "Allow DNS", chain: "forward", action: "ACCEPT", bytes: 640_000_000 * scale, packets: 900_000 * scale },
        { ruleId: "mock-rule-3", name: "Block Ads Domains", chain: "forward", action: "DROP", bytes: 45_000_000 * scale, packets: 120_000 * scale },
      ];
      const rulesTotal = rulesRaw.reduce((sum, r) => sum + r.bytes, 0);
      const topRules: TopRule[] = rulesRaw.map((r) => ({
        ...r,
        percent: Math.round((r.bytes / rulesTotal) * 1000) / 10,
      }));

      return {
        window,
        observedBytes,
        estimated: true,
        // Mock mode's flow-end event watcher never fails to "subscribe"
        // (kernel.MockTrafficAccounting.WatchFlowEnd has no real socket to
        // fail), so it always reports the phase-2 "near-exact" accuracy —
        // exercising that label in dev without needing a real board.
        accuracy: "near-exact",
        categories,
        topTalkers,
        topRules,
        generatedAt: new Date().toISOString(),
        sessions: mockSessionCounts(420 + 120 * Math.sin(Date.now() / 20000)),
        sessionHistory: mockSessionHistory(),
      };
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/traffic-detail?window=${window}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch traffic detail: ${response.statusText}`);
    }
    return response.json();
  },

  // Get firewall logs. limit defaults to 100 (backend caps at 500) — always
  // pass a limit rather than relying on the server default, since the
  // underlying ring buffer can hold up to 10,000 entries.
  getRecentLogs: async (limit = 100): Promise<FirewallLog[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalLogs().slice(0, limit);
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/logs?limit=${limit}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch recent logs: ${response.statusText}`);
    }
    return response.json();
  },

  // Clear all logs (mock support)
  clearLogs: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      saveLocalLogs([]);
      return;
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/logs/clear`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to clear logs: ${response.statusText}`);
    }
  },

  /**
   * Connect to the Server-Sent Events stream for live firewall/forward-traffic
   * logs. Auth rides on the HttpOnly session cookie (withCredentials); the
   * backend pushes each new entry as a default message event, a `clear` event
   * when the buffer is wiped, and `: ping` heartbeat comments.
   *
   * In mock mode there is no real EventSource — new entries are synthesized on
   * an interval and `onOpen` fires once so the consumer can seed its snapshot,
   * exactly as the real `open` event would.
   *
   * @returns a cleanup function that stops the stream.
   */
  connectSSELogs: (handlers: {
    onLog: (log: SSELogEntry) => void;
    onClear?: () => void;
    onOpen?: () => void;
    onError?: (err: Event) => void;
  }): (() => void) => {
    const { onLog, onClear, onOpen, onError } = handlers;

    if (IS_MOCK_MODE) {
      // Simulate SSE with interval-based generation. Fire onOpen on the next
      // tick so a consumer that refetches its snapshot onOpen behaves the same
      // as against a real stream.
      const openTimer = setTimeout(() => onOpen?.(), 0);
      const intervalId = setInterval(() => {
        onLog(dashboardService.generateMockLog());
      }, 4500);
      return () => {
        clearTimeout(openTimer);
        clearInterval(intervalId);
      };
    }

    // Real SSE connection. withCredentials makes EventSource send the session
    // cookie (needed for the dev cross-origin case; production is same-origin).
    // No token in the URL.
    const url = `${API_BASE_URL}/dashboard/logs/stream`;
    const es = new EventSource(url, { withCredentials: true });

    if (onOpen) es.onopen = () => onOpen();

    es.onmessage = (event) => {
      try {
        const log: SSELogEntry = JSON.parse(event.data);
        onLog(log);
      } catch (e) {
        console.warn("[SSE] Failed to parse log event:", e);
      }
    };

    if (onClear) {
      es.addEventListener("clear", () => onClear());
    }

    if (onError) {
      es.onerror = onError;
    }

    return () => {
      es.close();
    };
  },

  /**
   * Connect to the SSE stream for live host performance metrics
   * (CPU/RAM/temp/storage). Each message is a full `PerformanceMetrics` snapshot
   * the caller replaces wholesale — no dedupe/merge. Auth rides on the session
   * cookie (withCredentials).
   *
   * In mock mode there is no EventSource: `onMetrics` is driven on an interval
   * from the same generator that `getPerformanceMetrics` uses, and `onOpen`
   * fires once so a consumer behaves the same as against a real stream.
   *
   * @returns a cleanup function that stops the stream.
   */
  connectSSEMetrics: (handlers: {
    onMetrics: (m: PerformanceMetrics) => void;
    onOpen?: () => void;
    onError?: (err: Event) => void;
  }): (() => void) => {
    const { onMetrics, onOpen, onError } = handlers;

    if (IS_MOCK_MODE) {
      const openTimer = setTimeout(() => onOpen?.(), 0);
      const emit = () => {
        void dashboardService.getPerformanceMetrics().then(onMetrics);
      };
      emit(); // paint immediately, like the real stream's first snapshot
      const intervalId = setInterval(emit, 5000);
      return () => {
        clearTimeout(openTimer);
        clearInterval(intervalId);
      };
    }

    const url = `${API_BASE_URL}/dashboard/performance/stream`;
    const es = new EventSource(url, { withCredentials: true });

    if (onOpen) es.onopen = () => onOpen();

    es.onmessage = (event) => {
      try {
        const m: PerformanceMetrics = JSON.parse(event.data);
        onMetrics(m);
      } catch (e) {
        console.warn("[SSE] Failed to parse metrics event:", e);
      }
    };

    if (onError) {
      es.onerror = onError;
    }

    return () => {
      es.close();
    };
  },

  // Generate a mock log entry and save it (to simulate live SSE log appending in mock mode)
  generateMockLog: (): SSELogEntry => {
    const randomSrc = mockSources[Math.floor(Math.random() * mockSources.length)];
    const randomDest = mockDestinations[Math.floor(Math.random() * mockDestinations.length)];
    const randomSvc = mockLogServices[Math.floor(Math.random() * mockLogServices.length)];

    const t = new Date();
    const timeStr =
      String(t.getHours()).padStart(2, "0") +
      ":" +
      String(t.getMinutes()).padStart(2, "0") +
      ":" +
      String(t.getSeconds()).padStart(2, "0");

    // Include inIface/outIface so a Forward Traffic consumer of the simulated
    // stream renders complete rows in mock mode too (Dashboard ignores them).
    const newLog: SSELogEntry = {
      id: "log-" + Math.random().toString(36).substring(2, 9),
      time: timeStr,
      action: randomSvc.action as "PASS" | "DROP",
      src: randomSrc,
      dest: randomDest,
      port: randomSvc.port,
      proto: randomSvc.proto,
      inIface: randomSvc.action === "DROP" ? "eth0" : "wlan0",
      outIface: "eth1",
      reason: randomSvc.reason,
    };

    if (IS_MOCK_MODE) {
      const current = getLocalLogs();
      const updated = [newLog, ...current].slice(0, 50);
      saveLocalLogs(updated);
    }

    return newLog;
  },
};
