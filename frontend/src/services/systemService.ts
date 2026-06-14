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

// Helper to get time settings from LocalStorage
function getLocalTimeSettings(): SystemTimeSettings {
  const stored = localStorage.getItem(TIME_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(TIME_STORAGE_KEY, JSON.stringify(initialSystemTimeSettings));
    return initialSystemTimeSettings;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialSystemTimeSettings;
  }
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

export const systemService = {
  // Get system time settings
  getTimeSettings: async (): Promise<SystemTimeSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalTimeSettings();
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
      saveLocalTimeSettings(settings);
      return settings;
    }

    const response = await fetch(`${API_BASE_URL}/system/time`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(settings),
    });
    if (!response.ok) {
      throw new Error(`Failed to update system time settings: ${response.statusText}`);
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

  // Export full configuration payload
  exportConfig: async (): Promise<any> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 800));

      // Construct a full configuration package from all LocalStorage databases
      const addresses = localStorage.getItem("pigate_addresses");
      const serviceObjects = localStorage.getItem("pigate_service_objects");
      const policies = localStorage.getItem("pigate_policies");
      const routes = localStorage.getItem("pigate_routes");
      const dhcpConfig = localStorage.getItem("pigate_dhcp_config");
      const dhcpReservations = localStorage.getItem("pigate_dhcp_reservations");
      const interfaces = localStorage.getItem("pigate_interfaces");

      return {
        device: "PiGate Firewall Gateway",
        version: "v1.5.0-Release",
        exportedAt: new Date().toISOString(),
        systemSettings: getLocalTimeSettings(),
        servicesStatus: getLocalServices().map((s) => ({ service: s.serviceName, status: s.status })),
        config: {
          addresses: addresses ? JSON.parse(addresses) : null,
          serviceObjects: serviceObjects ? JSON.parse(serviceObjects) : null,
          policies: policies ? JSON.parse(policies) : null,
          routes: routes ? JSON.parse(routes) : null,
          dhcp: {
            config: dhcpConfig ? JSON.parse(dhcpConfig) : null,
            reservations: dhcpReservations ? JSON.parse(dhcpReservations) : null,
          },
          interfaces: interfaces ? JSON.parse(interfaces) : null,
        },
      };
    }

    const response = await fetch(`${API_BASE_URL}/system/config/export`);
    if (!response.ok) {
      throw new Error(`Failed to export configuration: ${response.statusText}`);
    }
    return response.json();
  },

  // Import configuration payload
  importConfig: async (configData: any): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 1500));

      if (!configData || typeof configData !== "object") {
        throw new Error("ข้อมูลไฟล์สำรองไม่ถูกต้อง");
      }

      // Restore system settings if present
      if (configData.systemSettings) {
        saveLocalTimeSettings(configData.systemSettings);
      }

      // Restore databases to LocalStorage
      const cfg = configData.config;
      if (cfg) {
        if (cfg.addresses) localStorage.setItem("pigate_addresses", JSON.stringify(cfg.addresses));
        if (cfg.serviceObjects) localStorage.setItem("pigate_service_objects", JSON.stringify(cfg.serviceObjects));
        if (cfg.policies) localStorage.setItem("pigate_policies", JSON.stringify(cfg.policies));
        if (cfg.routes) localStorage.setItem("pigate_routes", JSON.stringify(cfg.routes));
        if (cfg.dhcp) {
          if (cfg.dhcp.config) localStorage.setItem("pigate_dhcp_config", JSON.stringify(cfg.dhcp.config));
          if (cfg.dhcp.reservations) localStorage.setItem("pigate_dhcp_reservations", JSON.stringify(cfg.dhcp.reservations));
        }
        if (cfg.interfaces) localStorage.setItem("pigate_interfaces", JSON.stringify(cfg.interfaces));
      }
      return;
    }

    const response = await fetch(`${API_BASE_URL}/system/config/import`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(configData),
    });
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || `Failed to import configuration: ${response.statusText}`);
    }
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
