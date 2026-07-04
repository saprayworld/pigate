import {
  type SystemTimeSettings,
  type NetworkServiceStatus,
  initialSystemTimeSettings,
  initialNetworkServices,
} from "@/data-mockup/mockData";
import { IS_MOCK_MODE, API_BASE_URL } from "./config";

const TIME_STORAGE_KEY = "pigate_system_time";
const SERVICES_STORAGE_KEY = "pigate_network_services";
const DNS_STORAGE_KEY = "pigate_system_dns";
const HOSTNAME_STORAGE_KEY = "pigate_system_hostname";

export interface SystemHostnameSettings {
  hostname: string;
  shareWithDhcp: boolean;
}

// Backup file schema (v2) — mirrors backend model.BackupFile. `config` is left
// loosely typed because the page only reads meta for the confirm preview and
// otherwise round-trips the payload opaquely.
export interface BackupMeta {
  device: string;
  hostname: string;
  appVersion: string;
  schemaVersion: number;
  exportedAt: string;
  checksum: string;
  includeUsers: boolean;
}

export interface BackupFile {
  meta: BackupMeta;
  config: Record<string, unknown>;
}

// Mirrors backend model.ImportResult.
export interface ImportResult {
  schemaVersion: number;
  counts: Record<string, number>;
  warnings: string[];
  interfacesChanged: boolean;
  usersImported: boolean;
}

const initialHostnameSettings: SystemHostnameSettings = {
  hostname: "PiGate-RPI5",
  shareWithDhcp: false,
};

export interface DynamicDNSServer {
  interfaceName: string;
  interfaceAlias: string;
  dnsServers: string[];
}

export interface DNSConfig {
  mode: "wan" | "static";
  primaryDns: string;
  secondaryDns: string;
  localDomain: string;
  dynamicDnsServers: DynamicDNSServer[];
}

const initialDNSConfig: DNSConfig = {
  mode: "static",
  primaryDns: "1.1.1.1",
  secondaryDns: "8.8.8.8",
  localDomain: "pigate.local",
  dynamicDnsServers: [
    {
      interfaceName: "wlan0",
      interfaceAlias: "WAN_WiFi",
      dnsServers: ["192.168.0.1", "8.8.4.4"],
    },
  ],
};

function getLocalDNSConfig(): DNSConfig {
  const stored = localStorage.getItem(DNS_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(DNS_STORAGE_KEY, JSON.stringify(initialDNSConfig));
    return initialDNSConfig;
  }
  try {
    const parsed = JSON.parse(stored);
    if (!parsed.localDomain) {
      parsed.localDomain = "pigate.local";
    }
    return parsed;
  } catch (e) {
    return initialDNSConfig;
  }
}

function saveLocalDNSConfig(cfg: { mode: string; primaryDns: string; secondaryDns: string; localDomain: string }) {
  const current = getLocalDNSConfig();
  const updated = {
    ...current,
    mode: cfg.mode as "wan" | "static",
    primaryDns: cfg.primaryDns,
    secondaryDns: cfg.secondaryDns,
    localDomain: cfg.localDomain,
  };
  localStorage.setItem(DNS_STORAGE_KEY, JSON.stringify(updated));
}

// Strip a legacy " (GMT+7:00)"-style suffix from a stored timezone, returning
// the bare IANA name. Mirrors the backend NormalizeTimezone migration so old
// mock-mode localStorage values don't break the <Select>.
function normalizeTimezone(tz: string): string {
  if (!tz) return tz;
  const idx = tz.indexOf(" (");
  return (idx >= 0 ? tz.slice(0, idx) : tz).trim();
}

// Helper to get time settings from LocalStorage
function getLocalTimeSettings(): SystemTimeSettings {
  const stored = localStorage.getItem(TIME_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(TIME_STORAGE_KEY, JSON.stringify(initialSystemTimeSettings));
    return initialSystemTimeSettings;
  }
  try {
    const parsed = JSON.parse(stored) as SystemTimeSettings;
    parsed.timezone = normalizeTimezone(parsed.timezone);
    return parsed;
  } catch (e) {
    return initialSystemTimeSettings;
  }
}

// Build a simulated live status for mock mode: current device time + a synced
// flag that mirrors the NTP toggle.
function mockTimeStatus(settings: SystemTimeSettings): SystemTimeSettings {
  return {
    ...settings,
    status: {
      currentTime: new Date().toISOString(),
      ntpSynchronized: settings.ntpSync,
    },
  };
}

// Helper to save time settings
function saveLocalTimeSettings(settings: SystemTimeSettings) {
  localStorage.setItem(TIME_STORAGE_KEY, JSON.stringify(settings));
}

// Helper to get services from LocalStorage
function getLocalServices(): NetworkServiceStatus[] {
  const stored = localStorage.getItem(SERVICES_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(SERVICES_STORAGE_KEY, JSON.stringify(initialNetworkServices));
    return initialNetworkServices;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialNetworkServices;
  }
}

// Helper to save services
function saveLocalServices(services: NetworkServiceStatus[]) {
  localStorage.setItem(SERVICES_STORAGE_KEY, JSON.stringify(services));
}

// Helper to get hostname settings from LocalStorage
function getLocalHostnameSettings(): SystemHostnameSettings {
  const stored = localStorage.getItem(HOSTNAME_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(HOSTNAME_STORAGE_KEY, JSON.stringify(initialHostnameSettings));
    return initialHostnameSettings;
  }
  try {
    return JSON.parse(stored);
  } catch {
    return initialHostnameSettings;
  }
}

// Helper to save hostname settings
function saveLocalHostnameSettings(settings: SystemHostnameSettings) {
  localStorage.setItem(HOSTNAME_STORAGE_KEY, JSON.stringify(settings));
}

export const systemService = {
  // Get system time settings
  getTimeSettings: async (): Promise<SystemTimeSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return mockTimeStatus(getLocalTimeSettings());
    }

    const response = await fetch(`${API_BASE_URL}/system/time`);
    if (!response.ok) {
      throw new Error(`Failed to fetch system time settings: ${response.statusText}`);
    }
    return response.json();
  },

  // Save system time settings
  updateTimeSettings: async (settings: SystemTimeSettings): Promise<SystemTimeSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      // status is live-only; never persist it
      const { status, ...toSave } = settings;
      void status;
      saveLocalTimeSettings(toSave);
      return mockTimeStatus(toSave);
    }

    const response = await fetch(`${API_BASE_URL}/system/time`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(settings),
    });
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || `Failed to update system time settings: ${response.statusText}`);
    }
    return response.json();
  },

  // Set the wall clock manually (only permitted while NTP sync is off)
  setManualTime: async (datetime: string): Promise<SystemTimeSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalTimeSettings();
      if (current.ntpSync) {
        throw new Error("ไม่สามารถตั้งเวลาด้วยมือได้ขณะเปิดการซิงค์เวลาอัตโนมัติ (NTP) — กรุณาปิด NTP ก่อน");
      }
      return {
        ...current,
        status: { currentTime: new Date(datetime).toISOString(), ntpSynchronized: false },
      };
    }

    const response = await fetch(`${API_BASE_URL}/system/time/manual`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ datetime }),
    });
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || `Failed to set manual time: ${response.statusText}`);
    }
    return response.json();
  },

  // Get device hostname + DHCP hostname-sharing setting
  getHostname: async (): Promise<SystemHostnameSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalHostnameSettings();
    }

    const response = await fetch(`${API_BASE_URL}/system/hostname`);
    if (!response.ok) {
      throw new Error(`Failed to fetch hostname settings: ${response.statusText}`);
    }
    return response.json();
  },

  // Save device hostname + DHCP hostname-sharing setting
  updateHostname: async (settings: SystemHostnameSettings): Promise<SystemHostnameSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      saveLocalHostnameSettings(settings);
      return settings;
    }

    const response = await fetch(`${API_BASE_URL}/system/hostname`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(settings),
    });
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || `Failed to update hostname settings: ${response.statusText}`);
    }
    return response.json();
  },

  // Change Admin Password
  changePassword: async (currentPassword: string, newPassword: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 400));
      // Just simulate success/failure
      if (currentPassword === "error") {
        throw new Error("รหัสผ่านปัจจุบันไม่ถูกต้อง");
      }
      return;
    }

    const response = await fetch(`${API_BASE_URL}/system/password`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ currentPassword, newPassword }),
    });
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || `Failed to change password: ${response.statusText}`);
    }
  },

  // Get network services status
  getServices: async (): Promise<NetworkServiceStatus[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalServices();
    }

    const response = await fetch(`${API_BASE_URL}/system/services`);
    if (!response.ok) {
      throw new Error(`Failed to fetch system services: ${response.statusText}`);
    }
    return response.json();
  },

  // Restart a service
  restartService: async (id: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      // Set to stopped first, wait 1s, then run again
      const current = getLocalServices();
      const idx = current.findIndex((s) => s.id === id);
      if (idx !== -1) {
        current[idx].status = "stopped";
        saveLocalServices(current);
      }

      await new Promise((resolve) => setTimeout(resolve, 1500));

      const updated = getLocalServices();
      const updatedIdx = updated.findIndex((s) => s.id === id);
      if (updatedIdx !== -1) {
        updated[updatedIdx].status = "running";
        saveLocalServices(updated);
      }
      return;
    }

    const response = await fetch(`${API_BASE_URL}/system/services/${id}/restart`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to restart service: ${response.statusText}`);
    }
  },

  // Power actions
  reboot: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      return;
    }

    const response = await fetch(`${API_BASE_URL}/system/reboot`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to reboot system: ${response.statusText}`);
    }
  },

  shutdown: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      return;
    }

    const response = await fetch(`${API_BASE_URL}/system/shutdown`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to shutdown system: ${response.statusText}`);
    }
  },

  // Export full configuration payload (schema v2). Pass includeUsers to embed
  // the users table (bcrypt hashes) — super_admin only on the backend. When
  // passphrase is set, the backend AES-256-GCM encrypts the config section.
  exportConfig: async (includeUsers = false, passphrase = ""): Promise<BackupFile> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 800));

      // Build a v2-shaped payload from LocalStorage so mock-mode UI testing sees
      // the same structure the backend returns.
      const parse = (key: string): unknown[] => {
        const raw = localStorage.getItem(key);
        try {
          return raw ? JSON.parse(raw) : [];
        } catch {
          return [];
        }
      };
      const parseObj = (key: string): unknown => {
        const raw = localStorage.getItem(key);
        try {
          return raw ? JSON.parse(raw) : null;
        } catch {
          return null;
        }
      };

      const hostname = getLocalHostnameSettings().hostname;
      const dhcpCfg = parseObj("pigate_dhcp_config");
      const config: Record<string, unknown> = {
        interfaces: parse("pigate_interfaces"),
        staticRoutes: parse("pigate_routes"),
        addresses: parse("pigate_addresses"),
        serviceObjects: parse("pigate_service_objects"),
        policies: parse("pigate_policies"),
        dhcpConfigs: dhcpCfg ? [dhcpCfg] : [],
        dhcpReservations: parse("pigate_dhcp_reservations"),
        dnsZones: parse("pigate_dns_zones"),
        dnsServerSettings: { interfaces: [] },
        systemDns: getLocalDNSConfig(),
        qosRules: parse("pigate_qos_rules"),
        systemTime: getLocalTimeSettings(),
        systemHostname: getLocalHostnameSettings(),
        ...(includeUsers ? { users: parse("pigate_users") } : {}),
      };

      return {
        meta: {
          device: "PiGate Firewall Gateway",
          hostname,
          appVersion: "v0.1.0-pre",
          schemaVersion: 2,
          exportedAt: new Date().toISOString(),
          checksum: "sha256:mock",
          includeUsers,
        },
        config,
      };
    }

    const qs = includeUsers ? "?includeUsers=1" : "";
    const headers: Record<string, string> = {};
    if (passphrase) headers["X-Backup-Passphrase"] = passphrase;
    const response = await fetch(`${API_BASE_URL}/system/config/export${qs}`, { headers });
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || `Failed to export configuration: ${response.statusText}`);
    }
    return response.json();
  },

  // Import a configuration backup. Returns the backend's ImportResult (counts +
  // warnings). When includeUsers is true and the file carries users, the backend
  // restores accounts too.
  importConfig: async (configData: unknown, includeUsers = false, passphrase = ""): Promise<ImportResult> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 1500));

      if (!configData || typeof configData !== "object") {
        throw new Error("ข้อมูลไฟล์สำรองไม่ถูกต้อง");
      }

      const file = configData as Partial<BackupFile> & Record<string, unknown>;
      // Accept both v2 (meta+config) and legacy v1 (config at top level) shapes.
      const cfg = (file.config ?? {}) as Record<string, any>;

      // Restore recognised sections back into LocalStorage.
      if (Array.isArray(cfg.addresses)) localStorage.setItem("pigate_addresses", JSON.stringify(cfg.addresses));
      if (Array.isArray(cfg.serviceObjects)) localStorage.setItem("pigate_service_objects", JSON.stringify(cfg.serviceObjects));
      if (Array.isArray(cfg.policies)) localStorage.setItem("pigate_policies", JSON.stringify(cfg.policies));
      if (Array.isArray(cfg.staticRoutes)) localStorage.setItem("pigate_routes", JSON.stringify(cfg.staticRoutes));
      else if (Array.isArray(cfg.routes)) localStorage.setItem("pigate_routes", JSON.stringify(cfg.routes));
      if (Array.isArray(cfg.dhcpConfigs) && cfg.dhcpConfigs[0]) localStorage.setItem("pigate_dhcp_config", JSON.stringify(cfg.dhcpConfigs[0]));
      if (Array.isArray(cfg.dhcpReservations)) localStorage.setItem("pigate_dhcp_reservations", JSON.stringify(cfg.dhcpReservations));
      if (Array.isArray(cfg.interfaces)) localStorage.setItem("pigate_interfaces", JSON.stringify(cfg.interfaces));
      if (Array.isArray(cfg.dnsZones)) localStorage.setItem("pigate_dns_zones", JSON.stringify(cfg.dnsZones));
      if (Array.isArray(cfg.qosRules)) localStorage.setItem("pigate_qos_rules", JSON.stringify(cfg.qosRules));
      if (cfg.systemTime) saveLocalTimeSettings(cfg.systemTime);

      const count = (v: unknown) => (Array.isArray(v) ? v.length : 0);
      return {
        schemaVersion: (file.meta?.schemaVersion as number) ?? 1,
        counts: {
          interfaces: count(cfg.interfaces),
          staticRoutes: count(cfg.staticRoutes ?? cfg.routes),
          addresses: count(cfg.addresses),
          serviceObjects: count(cfg.serviceObjects),
          policies: count(cfg.policies),
          dhcpConfigs: count(cfg.dhcpConfigs),
          dhcpReservations: count(cfg.dhcpReservations),
          dnsZones: count(cfg.dnsZones),
          qosRules: count(cfg.qosRules),
          users: includeUsers ? count(cfg.users) : 0,
        },
        warnings: [],
        interfacesChanged: count(cfg.interfaces) > 0,
        usersImported: includeUsers && count(cfg.users) > 0,
      };
    }

    const qs = includeUsers ? "?includeUsers=1" : "";
    const headers: Record<string, string> = { "Content-Type": "application/json" };
    if (passphrase) headers["X-Backup-Passphrase"] = passphrase;
    const response = await fetch(`${API_BASE_URL}/system/config/import${qs}`, {
      method: "POST",
      headers,
      body: JSON.stringify(configData),
    });
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || `Failed to import configuration: ${response.statusText}`);
    }
    return response.json();
  },

  // Get system DNS settings
  getDNSConfig: async (): Promise<DNSConfig> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalDNSConfig();
    }

    const response = await fetch(`${API_BASE_URL}/system/dns`);
    if (!response.ok) {
      throw new Error(`Failed to fetch system DNS settings: ${response.statusText}`);
    }
    return response.json();
  },

  // Save system DNS settings
  updateDNSConfig: async (cfg: { mode: string; primaryDns: string; secondaryDns: string; localDomain: string }): Promise<any> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      saveLocalDNSConfig(cfg);
      return cfg;
    }

    const response = await fetch(`${API_BASE_URL}/system/dns`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(cfg),
    });
    if (!response.ok) {
      throw new Error(`Failed to update system DNS settings: ${response.statusText}`);
    }
    return response.json();
  },
};
