import { type DNSZone, type DNSRecord, type DNSServerSettings, type BlockedDomain, initialDNSZones, initialDNSServerSettings, initialBlockedDomains } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const ZONES_STORAGE_KEY = "pigate_dns_zones";
const SETTINGS_STORAGE_KEY = "pigate_dns_server_settings";
const BLOCKED_DOMAINS_STORAGE_KEY = "pigate_dns_blocked_domains";

function getLocalZones(): DNSZone[] {
  const stored = localStorage.getItem(ZONES_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(ZONES_STORAGE_KEY, JSON.stringify(initialDNSZones));
    return initialDNSZones;
  }
  try {
    return JSON.parse(stored);
  } catch {
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
    // Merge with defaults so a value saved to localStorage before
    // upstreamMode/upstreamServers existed still comes back with them set
    // (mock-mode equivalent of the backend's migration DEFAULT 'system').
    return { ...initialDNSServerSettings, ...JSON.parse(stored) };
  } catch {
    return initialDNSServerSettings;
  }
}

function saveLocalSettings(settings: DNSServerSettings) {
  localStorage.setItem(SETTINGS_STORAGE_KEY, JSON.stringify(settings));
}

function getLocalBlockedDomains(): BlockedDomain[] {
  const stored = localStorage.getItem(BLOCKED_DOMAINS_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(BLOCKED_DOMAINS_STORAGE_KEY, JSON.stringify(initialBlockedDomains));
    return initialBlockedDomains;
  }
  try {
    return JSON.parse(stored);
  } catch {
    return initialBlockedDomains;
  }
}

function saveLocalBlockedDomains(domains: BlockedDomain[]) {
  localStorage.setItem(BLOCKED_DOMAINS_STORAGE_KEY, JSON.stringify(domains));
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

  getBlockedDomains: async (): Promise<BlockedDomain[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalBlockedDomains();
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocked-domains`);
    if (!response.ok) {
      throw new Error(`Failed to fetch blocked domains: ${response.statusText}`);
    }
    return response.json();
  },

  createBlockedDomain: async (domain: Omit<BlockedDomain, "id" | "createdAt">): Promise<BlockedDomain> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalBlockedDomains();
      const newDomain: BlockedDomain = {
        ...domain,
        id: "blk-" + Math.random().toString(36).substring(2, 9),
        createdAt: new Date().toISOString(),
      };
      saveLocalBlockedDomains([...current, newDomain]);
      return newDomain;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocked-domains`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(domain),
    });
    if (!response.ok) {
      throw new Error(`Failed to create blocked domain: ${response.statusText}`);
    }
    return response.json();
  },

  updateBlockedDomain: async (id: string, domain: Omit<BlockedDomain, "id" | "createdAt">): Promise<BlockedDomain> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalBlockedDomains();
      const updated = current.map((d) => (d.id === id ? { ...d, ...domain } : d));
      saveLocalBlockedDomains(updated);
      const target = updated.find((d) => d.id === id);
      if (!target) throw new Error("Blocked domain not found");
      return target;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocked-domains/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(domain),
    });
    if (!response.ok) {
      throw new Error(`Failed to update blocked domain: ${response.statusText}`);
    }
    return response.json();
  },

  deleteBlockedDomain: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalBlockedDomains();
      saveLocalBlockedDomains(current.filter((d) => d.id !== id));
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocked-domains/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete blocked domain: ${response.statusText}`);
    }
    return true;
  },

  toggleBlockedDomain: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalBlockedDomains();
      const updated = current.map((d) => (d.id === id ? { ...d, enabled: !d.enabled } : d));
      saveLocalBlockedDomains(updated);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocked-domains/${id}/toggle`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle blocked domain: ${response.statusText}`);
    }
    return true;
  },
};
