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

export interface PerformanceMetrics {
  cpu: number;
  memory: number;
  temp: number;
}

export interface DashboardStats {
  firewallStatus: string;
  totalTrafficIn: string;
  totalTrafficOut: string;
  dhcpLeasesCount: number;
  wifiStatus: string;
  wifiSSID: string;
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
  } catch (e) {
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
        } catch (e) {}
      }
      return {
        firewallStatus: "Active",
        totalTrafficIn: "8.7 GB",
        totalTrafficOut: "3.7 GB",
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
      const memory = Math.round((14.5 + Math.random() * 1) * 10) / 10;
      const temp = Math.round((47.5 + Math.random() * 2) * 10) / 10;
      return { cpu, memory, temp };
    }

    const response = await fetch(`${API_BASE_URL}/dashboard/performance`);
    if (!response.ok) {
      throw new Error(`Failed to fetch performance metrics: ${response.statusText}`);
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

  // Generate a mock log entry and save it (to simulate live SSE log appending in mock mode)
  generateMockLog: (): FirewallLog => {
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

    const newLog: FirewallLog = {
      id: "log-" + Math.random().toString(36).substring(2, 9),
      time: timeStr,
      action: randomSvc.action as "PASS" | "DROP",
      src: randomSrc,
      dest: randomDest,
      port: randomSvc.port,
      proto: randomSvc.proto,
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
