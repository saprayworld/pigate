import {
  type NetworkInterface,
  type WifiScanResult,
  initialNetworkInterfaces,
  mockWifiScanResults,
} from "@/data-mockup/mockData";
import { IS_MOCK_MODE, API_BASE_URL } from "./config";

const LOCAL_STORAGE_KEY = "pigate_interfaces";

// Helper: Generate a valid LAA (Locally Administered Address) MAC Address
function generateRandomMac(): string {
  const hex = "0123456789ABCDEF";
  // The first byte's second nibble must be 2, 6, A, or E for standard LAA
  const laaDigits = ["2", "6", "A", "E"];
  const firstByte =
    hex[Math.floor(Math.random() * 16)] + laaDigits[Math.floor(Math.random() * 4)];
  const parts = [firstByte];
  for (let i = 0; i < 5; i++) {
    parts.push(
      hex[Math.floor(Math.random() * 16)] + hex[Math.floor(Math.random() * 16)]
    );
  }
  return parts.join(":");
}

function getLocalInterfaces(): NetworkInterface[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialNetworkInterfaces));
    return initialNetworkInterfaces;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    console.error("Failed to parse network interfaces, resetting to mock data:", e);
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialNetworkInterfaces));
    return initialNetworkInterfaces;
  }
}

function saveLocalInterfaces(interfaces: NetworkInterface[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(interfaces));
}

// Input payload for creating an 802.1Q VLAN sub-interface.
export interface CreateVlanInput {
  parent: string
  vlanId: number
  alias?: string
  role?: "LAN" | "WAN"
  addressingMode?: "dhcp" | "static"
  ip?: string
  netmask?: string
  gateway?: string
  adminAccess?: string[]
}

export const interfaceService = {
  // Fetch all network interfaces
  getAll: async (): Promise<NetworkInterface[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      return getLocalInterfaces();
    }

    const response = await fetch(`${API_BASE_URL}/interfaces`);
    if (!response.ok) {
      throw new Error(`Failed to fetch network interfaces: ${response.statusText}`);
    }
    return response.json();
  },

  // Update configuration of a network interface
  update: async (
    id: string,
    updates: Partial<NetworkInterface>
  ): Promise<NetworkInterface> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 400));
      const current = getLocalInterfaces();
      const targetIndex = current.findIndex((i) => i.id === id);
      if (targetIndex === -1) {
        throw new Error(`Interface with id ${id} not found`);
      }

      const target = current[targetIndex];

      // Mirror the server-side alias rules (empty -> OS name, unique
      // case-insensitive, must not be another interface's OS name).
      if (updates.alias !== undefined) {
        const alias = updates.alias.trim() === "" ? target.name : updates.alias.trim();
        const lower = alias.toLowerCase();
        const conflict = current.some(
          (i) => i.id !== id &&
            (i.alias.toLowerCase() === lower || i.name.toLowerCase() === lower)
        );
        if (conflict) {
          throw new Error(`interface alias already in use: "${alias}"`);
        }
        updates = { ...updates, alias };
      }

      const updatedIface: NetworkInterface = {
        ...target,
        ...updates,
      };

      // Recalculate effective MAC address if macMode changes or wireless specific fields update
      if (updatedIface.type === "wireless") {
        const defaultRandomMac =
          updatedIface.randomizedMac || generateRandomMac();
        updatedIface.randomizedMac = defaultRandomMac;

        if (updates.macMode) {
          updatedIface.macAddress =
            updates.macMode === "hardware"
              ? updatedIface.realMacAddress || updatedIface.macAddress
              : updates.macMode === "randomized"
              ? defaultRandomMac
              : updatedIface.laaMacAddress || "";
        }
      }

      current[targetIndex] = updatedIface;
      saveLocalInterfaces(current);
      return updatedIface;
    }

    const response = await fetch(`${API_BASE_URL}/interfaces/${id}`, {
      method: "PATCH",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(updates),
    });
    if (!response.ok) {
      throw new Error(`Failed to update interface: ${response.statusText}`);
    }
    return response.json();
  },

  // Toggle status UP/DOWN
  toggleStatus: async (id: string): Promise<NetworkInterface> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalInterfaces();
      const targetIndex = current.findIndex((i) => i.id === id);
      if (targetIndex === -1) {
        throw new Error(`Interface with id ${id} not found`);
      }

      const target = current[targetIndex];
      const nextStatus = target.status === "up" ? "down" : "up";

      const updatedIface: NetworkInterface = {
        ...target,
        status: nextStatus,
      };

      // Rotate randomized MAC if wireless, randomized mode, and rotate-on-reconnect enabled
      if (
        nextStatus === "up" &&
        target.type === "wireless" &&
        target.macMode === "randomized" &&
        target.randomizeOnReconnect
      ) {
        const newMac = generateRandomMac();
        updatedIface.randomizedMac = newMac;
        updatedIface.macAddress = newMac;
      }

      current[targetIndex] = updatedIface;
      saveLocalInterfaces(current);
      return updatedIface;
    }

    const response = await fetch(`${API_BASE_URL}/interfaces/${id}/toggle`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle interface status: ${response.statusText}`);
    }
    return response.json();
  },

  // Fetch live Wi-Fi connection status
  getWifiStatus: async (id: string): Promise<{ 
    state: string; 
    ssid: string; 
    bssid: string; 
    activeMac?: string;
    freq?: number;
    keyMgmt?: string;
    wifiGen?: string;
  }> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return {
        state: "COMPLETED",
        ssid: "MyHome_5G",
        bssid: "00:11:22:33:44:55",
        activeMac: "00:11:22:33:44:55",
        freq: 5745,
        keyMgmt: "WPA3",
        wifiGen: "WiFi 6",
      };
    }

    const response = await fetch(`${API_BASE_URL}/interfaces/${id}/wifi-status`);
    if (!response.ok) {
      throw new Error(`Failed to fetch Wi-Fi status: ${response.statusText}`);
    }
    return response.json();
  },

  // Scan Wi-Fi networks (specifically for wireless interfaces)
  scanWifi: async (id: string): Promise<WifiScanResult[]> => {
    if (IS_MOCK_MODE) {
      // Simulate typical Wi-Fi scanning latency
      await new Promise((resolve) => setTimeout(resolve, 1800));
      return mockWifiScanResults;
    }

    const response = await fetch(`${API_BASE_URL}/interfaces/${id}/scan`);
    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || response.statusText || "Failed to scan Wi-Fi");
    }
    return response.json();
  },

  // Reset interface configuration to default values
  reset: async (id: string): Promise<NetworkInterface> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalInterfaces();
      const targetIndex = current.findIndex((i) => i.id === id);
      if (targetIndex === -1) {
        throw new Error(`Interface with id ${id} not found`);
      }
      const target = current[targetIndex];
      const resetIface: NetworkInterface = {
        ...target,
        alias: target.name,
        role: target.name.includes("wan") || target.name.includes("wlan") ? "WAN" : "LAN",
        addressingMode: target.type === "wireless" || target.name.includes("wan") ? "dhcp" : "static",
        adminAccess: target.name.includes("wan") || target.name.includes("wlan") ? ["PING"] : ["PING", "HTTP", "HTTPS", "SSH"],
        macMode: "hardware",
        randomizeOnReconnect: false,
        failoverEnabled: false,
        backupSsid: "",
        backupWifiPassword: "",
        backupWifiSecurity: "WPA2",
        ipCheckTimeout: 15,
        primaryMaxRetries: 3,
        failoverCooldown: 60,
      };
      current[targetIndex] = resetIface;
      saveLocalInterfaces(current);
      return resetIface;
    }

    const response = await fetch(`${API_BASE_URL}/interfaces/${id}/reset`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to reset interface: ${response.statusText}`);
    }
    return response.json();
  },

  // Create an 802.1Q VLAN sub-interface. The resulting link is named "<parent>.<vlanId>".
  createVlan: async (input: CreateVlanInput): Promise<NetworkInterface> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 400))
      const current = getLocalInterfaces()
      const name = `${input.parent}.${input.vlanId}`
      if (current.some((i) => i.name === name)) {
        throw new Error(`VLAN ${name} already exists`)
      }
      const parent = current.find((i) => i.name === input.parent)
      if (!parent) {
        throw new Error(`Parent interface ${input.parent} not found`)
      }
      if (parent.type !== "ethernet" || parent.subtype === "vlan") {
        throw new Error(`Parent ${input.parent} must be a non-VLAN ethernet interface`)
      }
      const vlanAlias = (input.alias || name).trim() || name
      if (vlanAlias !== name) {
        const lower = vlanAlias.toLowerCase()
        if (current.some((i) => i.alias.toLowerCase() === lower || i.name.toLowerCase() === lower)) {
          throw new Error(`interface alias already in use: "${vlanAlias}"`)
        }
      }
      const mode = input.addressingMode || "dhcp"
      const role = input.role || "LAN"
      const newIface: NetworkInterface = {
        id: `iface-${name}`,
        name,
        alias: vlanAlias,
        role,
        type: "ethernet",
        subtype: "vlan",
        addressingMode: mode,
        ip: mode === "static" ? input.ip || "0.0.0.0" : "0.0.0.0",
        netmask: mode === "static" ? input.netmask || "24" : "24",
        gateway: mode === "static" ? input.gateway || "" : "",
        macAddress: parent.macAddress,
        adminAccess: (input.adminAccess as NetworkInterface["adminAccess"]) ||
          (role === "WAN" ? ["PING"] : ["PING", "HTTP", "HTTPS", "SSH"]),
        status: "up",
        managed: true,
        speed: parent.speed,
        vlanParent: input.parent,
        vlanId: input.vlanId,
      }
      current.push(newIface)
      saveLocalInterfaces(current)
      return newIface
    }

    const response = await fetch(`${API_BASE_URL}/interfaces/vlan`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
    })
    if (!response.ok) {
      let message = `Failed to create VLAN: ${response.statusText}`
      try {
        const body = await response.json()
        if (body?.message) message = body.message
      } catch {
        // response has no JSON body; keep the status-text fallback
      }
      throw new Error(message)
    }
    return response.json()
  },

  // Delete interface from database
  delete: async (id: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalInterfaces();
      const updated = current.filter((i) => i.id !== id);
      saveLocalInterfaces(updated);
      return;
    }

    const response = await fetch(`${API_BASE_URL}/interfaces/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete interface: ${response.statusText}`);
    }
  },
};
