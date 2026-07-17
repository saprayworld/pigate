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

  // Get firewall logs
  getRecentLogs: async (): Promise<FirewallLog[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalLogs();
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/logs`);
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
      const intervalId = setInterval(emit, 3000);
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
