import { type DNSZone, type DNSRecord, type DNSServerSettings, initialDNSZones, initialDNSServerSettings } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const ZONES_STORAGE_KEY = "pigate_dns_zones";
const SETTINGS_STORAGE_KEY = "pigate_dns_server_settings";

function getLocalZones(): DNSZone[] {
  const stored = localStorage.getItem(ZONES_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(ZONES_STORAGE_KEY, JSON.stringify(initialDNSZones));
    return initialDNSZones;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialDNSZones;
  }
}

function saveLocalZones(zones: DNSZone[]) {
  localStorage.setItem(ZONES_STORAGE_KEY, JSON.stringify(zones));
}

function getLocalSettings(): DNSServerSettings {
  const stored = localStorage.getItem(SETTINGS_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(SETTINGS_STORAGE_KEY, JSON.stringify(initialDNSServerSettings));
    return initialDNSServerSettings;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    return initialDNSServerSettings;
  }
}

function saveLocalSettings(settings: DNSServerSettings) {
  localStorage.setItem(SETTINGS_STORAGE_KEY, JSON.stringify(settings));
}

export const dnsServerService = {
  getZones: async (): Promise<DNSZone[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalZones();
    }

    const response = await fetch(`${API_BASE_URL}/dns/zones`);
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS zones: ${response.statusText}`);
    }
    return response.json();
  },

  createZone: async (zone: Omit<DNSZone, "id" | "records">): Promise<DNSZone> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalZones();
      const newZone: DNSZone = {
        ...zone,
        id: "zone-" + Math.random().toString(36).substring(2, 9),
        records: [],
      };
      saveLocalZones([...current, newZone]);
      return newZone;
    }

    const response = await fetch(`${API_BASE_URL}/dns/zones`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(zone),
    });
    if (!response.ok) {
      throw new Error(`Failed to create DNS zone: ${response.statusText}`);
    }
    return response.json();
  },

  updateZone: async (id: string, zone: Omit<DNSZone, "id" | "records">): Promise<DNSZone> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalZones();
      const updated = current.map((z) => {
        if (z.id === id) {
          return { ...z, ...zone };
        }
        return z;
      });
      saveLocalZones(updated);
      const target = updated.find((z) => z.id === id);
      if (!target) throw new Error("Zone not found");
      return target;
    }

    const response = await fetch(`${API_BASE_URL}/dns/zones/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(zone),
    });
    if (!response.ok) {
      throw new Error(`Failed to update DNS zone: ${response.statusText}`);
    }
    return response.json();
  },

  deleteZone: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalZones();
      const filtered = current.filter((z) => z.id !== id);
      saveLocalZones(filtered);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/zones/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete DNS zone: ${response.statusText}`);
    }
    return true;
  },

  toggleZone: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalZones();
      const updated = current.map((z) => {
        if (z.id === id) {
          return { ...z, enabled: !z.enabled };
        }
        return z;
      });
      saveLocalZones(updated);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/zones/${id}/toggle`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle DNS zone: ${response.statusText}`);
    }
    return true;
  },

  getRecords: async (zoneId: string): Promise<DNSRecord[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const zones = getLocalZones();
      const zone = zones.find((z) => z.id === zoneId);
      return zone ? zone.records : [];
    }

    const response = await fetch(`${API_BASE_URL}/dns/zones/${zoneId}/records`);
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS records: ${response.statusText}`);
    }
    return response.json();
  },

  createRecord: async (zoneId: string, record: Omit<DNSRecord, "id" | "zoneId">): Promise<DNSRecord> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const currentZones = getLocalZones();
      const newRec: DNSRecord = {
        ...record,
        id: "rec-" + Math.random().toString(36).substring(2, 9),
        zoneId: zoneId,
      };
      const updated = currentZones.map((z) => {
        if (z.id === zoneId) {
          return { ...z, records: [...z.records, newRec] };
        }
        return z;
      });
      saveLocalZones(updated);
      return newRec;
    }

    const response = await fetch(`${API_BASE_URL}/dns/zones/${zoneId}/records`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(record),
    });
    if (!response.ok) {
      throw new Error(`Failed to create DNS record: ${response.statusText}`);
    }
    return response.json();
  },

  updateRecord: async (id: string, record: Omit<DNSRecord, "id" | "zoneId">): Promise<DNSRecord> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const currentZones = getLocalZones();
      let updatedRec: DNSRecord | null = null;
      const updated = currentZones.map((z) => {
        const hasRec = z.records.some((r) => r.id === id);
        if (hasRec) {
          const recs = z.records.map((r) => {
            if (r.id === id) {
              updatedRec = { ...r, ...record };
              return updatedRec;
            }
            return r;
          });
          return { ...z, records: recs };
        }
        return z;
      });
      if (!updatedRec) throw new Error("Record not found");
      saveLocalZones(updated);
      return updatedRec;
    }

    const response = await fetch(`${API_BASE_URL}/dns/records/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(record),
    });
    if (!response.ok) {
      throw new Error(`Failed to update DNS record: ${response.statusText}`);
    }
    return response.json();
  },

  deleteRecord: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const currentZones = getLocalZones();
      const updated = currentZones.map((z) => {
        return {
          ...z,
          records: z.records.filter((r) => r.id !== id),
        };
      });
      saveLocalZones(updated);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/records/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete DNS record: ${response.statusText}`);
    }
    return true;
  },

  apply: async (): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 1000));
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/apply`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to apply DNS settings: ${response.statusText}`);
    }
    return true;
  },

  clearCache: async (): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/clear-cache`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to clear DNS cache: ${response.statusText}`);
    }
    return true;
  },

  getSettings: async (): Promise<DNSServerSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalSettings();
    }

    const response = await fetch(`${API_BASE_URL}/dns/settings`);
    if (!response.ok) {
      throw new Error(`Failed to fetch DNS server settings: ${response.statusText}`);
    }
    return response.json();
  },

  updateSettings: async (settings: DNSServerSettings): Promise<DNSServerSettings> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      saveLocalSettings(settings);
      return settings;
    }

    const response = await fetch(`${API_BASE_URL}/dns/settings`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(settings),
    });
    if (!response.ok) {
      throw new Error(`Failed to update DNS server settings: ${response.statusText}`);
    }
    return response.json();
  },
};
