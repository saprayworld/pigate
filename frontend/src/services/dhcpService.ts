import {
  type DhcpConfig,
  type DhcpReservation,
  type ActiveDhcpLease,
  initialDhcpConfig,
  initialDhcpReservations,
  initialActiveDhcpLeases,
} from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const CONFIG_STORAGE_KEY = "pigate_dhcp_config";
const RESERVATIONS_STORAGE_KEY = "pigate_dhcp_reservations";
const LEASES_STORAGE_KEY = "pigate_dhcp_leases";

// Config helpers
function getLocalConfig(): DhcpConfig {
  const stored = localStorage.getItem(CONFIG_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(CONFIG_STORAGE_KEY, JSON.stringify(initialDhcpConfig));
    return initialDhcpConfig;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialDhcpConfig;
  }
}

function saveLocalConfig(config: DhcpConfig) {
  localStorage.setItem(CONFIG_STORAGE_KEY, JSON.stringify(config));
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
  // Get main configuration
  getConfig: async (): Promise<DhcpConfig> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalConfig();
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/config`);
    if (!response.ok) {
      throw new Error(`Failed to fetch DHCP config: ${response.statusText}`);
    }
    return response.json();
  },

  // Save DHCP Configuration
  updateConfig: async (config: DhcpConfig): Promise<DhcpConfig> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      saveLocalConfig(config);
      return config;
    }

    const response = await fetch(`${API_BASE_URL}/dhcp/config`, {
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
