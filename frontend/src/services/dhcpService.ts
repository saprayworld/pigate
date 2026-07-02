import {
  type DhcpConfig,
  type DhcpReservation,
  type ActiveDhcpLease,
  initialDhcpConfig,
  initialDhcpConfigs,
  initialDhcpReservations,
  initialActiveDhcpLeases,
} from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const CONFIGS_STORAGE_KEY = "pigate_dhcp_configs";
const RESERVATIONS_STORAGE_KEY = "pigate_dhcp_reservations";
const LEASES_STORAGE_KEY = "pigate_dhcp_leases";

// Config helpers
function getLocalConfigs(): DhcpConfig[] {
  const stored = localStorage.getItem(CONFIGS_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(CONFIGS_STORAGE_KEY, JSON.stringify(initialDhcpConfigs));
    return initialDhcpConfigs;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialDhcpConfigs;
  }
}

function saveLocalConfigs(configs: DhcpConfig[]) {
  localStorage.setItem(CONFIGS_STORAGE_KEY, JSON.stringify(configs));
}

// Reservations helpers
function getLocalReservations(): DhcpReservation[] {
  const stored = localStorage.getItem(RESERVATIONS_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(RESERVATIONS_STORAGE_KEY, JSON.stringify(initialDhcpReservations));
    return initialDhcpReservations;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialDhcpReservations;
  }
}

function saveLocalReservations(reservations: DhcpReservation[]) {
  localStorage.setItem(RESERVATIONS_STORAGE_KEY, JSON.stringify(reservations));
}

// Leases helpers
function getLocalLeases(): ActiveDhcpLease[] {
  const stored = localStorage.getItem(LEASES_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LEASES_STORAGE_KEY, JSON.stringify(initialActiveDhcpLeases));
    return initialActiveDhcpLeases;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialActiveDhcpLeases;
  }
}

function saveLocalLeases(leases: ActiveDhcpLease[]) {
  localStorage.setItem(LEASES_STORAGE_KEY, JSON.stringify(leases));
}

export const dhcpService = {
  // Get all DHCP configurations
  getConfigs: async (): Promise<DhcpConfig[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalConfigs();
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/configs`);
    if (!response.ok) {
      throw new Error(`Failed to fetch DHCP configs: ${response.statusText}`);
    }
    return response.json();
  },

  // Get main configuration (for compatibility)
  getConfig: async (): Promise<DhcpConfig> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const list = getLocalConfigs();
      return list[0] || initialDhcpConfig;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/config`);
    if (!response.ok) {
      throw new Error(`Failed to fetch DHCP config: ${response.statusText}`);
    }
    return response.json();
  },

  // Create a new DHCP Config
  createConfig: async (config: Omit<DhcpConfig, "id">): Promise<DhcpConfig> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalConfigs();
      const newCfg: DhcpConfig = {
        ...config,
        id: "dhcp-cfg-" + config.interface,
      };
      saveLocalConfigs([...current, newCfg]);
      return newCfg;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/configs`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(config),
    });
    if (!response.ok) {
      throw new Error(`Failed to create DHCP config: ${response.statusText}`);
    }
    return response.json();
  },

  // Update a DHCP Configuration
  updateConfig: async (id: string, config: DhcpConfig): Promise<DhcpConfig> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      const current = getLocalConfigs();
      const updatedList = current.map((c) => (c.id === id ? config : c));
      saveLocalConfigs(updatedList);
      return config;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/configs/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(config),
    });
    if (!response.ok) {
      throw new Error(`Failed to update DHCP config: ${response.statusText}`);
    }
    return response.json();
  },

  // Delete a DHCP Configuration
  deleteConfig: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalConfigs();
      const updatedList = current.filter((c) => c.id !== id);
      saveLocalConfigs(updatedList);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/configs/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete DHCP config: ${response.statusText}`);
    }
    return true;
  },

  // Toggle a DHCP Configuration
  toggleConfig: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalConfigs();
      const updatedList = current.map((c) => c.id === id ? { ...c, enabled: !c.enabled } : c);
      saveLocalConfigs(updatedList);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/configs/${id}/toggle`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle DHCP config: ${response.statusText}`);
    }
    return true;
  },

  // Get available LAN interfaces that can be configured
  getAvailableInterfaces: async (): Promise<string[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const configs = getLocalConfigs();
      const configured = configs.map(c => c.interface);
      const allIfaces = ["eth0", "eth1", "eth2"];
      return allIfaces.filter(i => !configured.includes(i) && i !== "wlan0");
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/interfaces`);
    if (!response.ok) {
      throw new Error(`Failed to fetch available DHCP interfaces: ${response.statusText}`);
    }
    return response.json();
  },

  // Get reservations list
  getReservations: async (): Promise<DhcpReservation[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 250));
      return getLocalReservations();
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/reservations`);
    if (!response.ok) {
      throw new Error(`Failed to fetch DHCP reservations: ${response.statusText}`);
    }
    return response.json();
  },

  // Create a reservation
  createReservation: async (
    res: Omit<DhcpReservation, "id">
  ): Promise<DhcpReservation> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalReservations();
      const newRes: DhcpReservation = {
        ...res,
        id: "res-" + Math.random().toString(36).substring(2, 9),
      };
      saveLocalReservations([...current, newRes]);
      return newRes;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/reservations`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(res),
    });
    if (!response.ok) {
      throw new Error(`Failed to create reservation: ${response.statusText}`);
    }
    return response.json();
  },

  // Update a reservation
  updateReservation: async (
    id: string,
    res: Omit<DhcpReservation, "id">
  ): Promise<DhcpReservation> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalReservations();
      const target = current.find((r) => r.id === id);
      if (!target) {
        throw new Error(`Reservation not found`);
      }
      const updatedRes: DhcpReservation = {
        ...target,
        ...res,
      };
      const updatedList = current.map((r) => (r.id === id ? updatedRes : r));
      saveLocalReservations(updatedList);
      return updatedRes;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/reservations/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(res),
    });
    if (!response.ok) {
      throw new Error(`Failed to update reservation: ${response.statusText}`);
    }
    return response.json();
  },

  // Delete a reservation
  deleteReservation: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalReservations();
      const updatedList = current.filter((r) => r.id !== id);
      saveLocalReservations(updatedList);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/reservations/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete reservation: ${response.statusText}`);
    }
    return true;
  },

  // Get active client leases
  getActiveLeases: async (refresh = false): Promise<ActiveDhcpLease[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 400));
      const current = getLocalLeases();
      if (refresh && current.length === 3) {
        const updated = [
          ...current,
          {
            id: "lease-4",
            ipAddress: "192.168.1.109",
            macAddress: "40:A3:CC:11:D3:55",
            hostname: "Smart-Thermostat",
            interface: "eth0",
            expiresIn: "23 hours, 59 mins",
          },
        ];
        saveLocalLeases(updated);
        return updated;
      }
      return current;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/leases${refresh ? "?refresh=true" : ""}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch active leases: ${response.statusText}`);
    }
    return response.json();
  },

  // Apply DHCP configuration changes to system service
  apply: async (): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/apply`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to apply DHCP configuration: ${response.statusText}`);
    }
    return true;
  },
};
