import { type DNSZone, type DNSRecord, type DNSServerSettings, type BlockedDomain, type DNSBlocklist, initialDNSZones, initialDNSServerSettings, initialBlockedDomains, initialDNSBlocklists, DNS_BLOCKLISTS_MAX, DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const ZONES_STORAGE_KEY = "pigate_dns_zones";
const SETTINGS_STORAGE_KEY = "pigate_dns_server_settings";
const BLOCKED_DOMAINS_STORAGE_KEY = "pigate_dns_blocked_domains";
const BLOCKLISTS_STORAGE_KEY = "pigate_dns_blocklists";

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

function getLocalBlocklists(): DNSBlocklist[] {
  const stored = localStorage.getItem(BLOCKLISTS_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(BLOCKLISTS_STORAGE_KEY, JSON.stringify(initialDNSBlocklists));
    return initialDNSBlocklists;
  }
  try {
    return JSON.parse(stored);
  } catch {
    return initialDNSBlocklists;
  }
}

function saveLocalBlocklists(lists: DNSBlocklist[]) {
  localStorage.setItem(BLOCKLISTS_STORAGE_KEY, JSON.stringify(lists));
}

// Reads the backend's real failure reason from the response body
// ({"message": ...}, see api/handlers.go's writeError) instead of throwing a
// generic response.statusText — needed here (unlike the zone/blocked-domain
// functions above) because the blocklist endpoints return specific,
// human-readable reasons (dnsmasq too old, over quota, bad URL, ...) that the
// Blocklists tab (T-12) must be able to show verbatim. Mirrors
// policyService.ts's parseError.
async function parseBlocklistError(response: Response, fallback: string): Promise<never> {
  const errBody = await response.json().catch(() => ({}));
  throw new Error(errBody.message || fallback);
}

// Rough client-side estimate of how many domains an uploaded hosts-format
// file would parse to — mirrors the shape of model.ParseHostsBlocklist
// (backend) closely enough for a believable mock domainCount without
// duplicating its full validation/exclude-list logic: strips '#' comments,
// skips blank lines, drops a leading IP column if present, counts the
// remaining unique lower-cased hostnames.
function estimateHostsDomainCount(text: string): number {
  const seen = new Set<string>();
  for (const rawLine of text.split(/\r?\n/)) {
    const line = rawLine.split("#")[0].trim();
    if (!line) continue;
    const fields = line.split(/\s+/);
    if (fields.length === 0) continue;
    const isIp = /^[0-9a-fA-F:.]+$/.test(fields[0]) && fields.length > 1;
    const hostFields = isIp ? fields.slice(1) : fields;
    for (const host of hostFields) {
      const name = host.toLowerCase().replace(/\.$/, "");
      if (name) seen.add(name);
    }
  }
  return seen.size;
}

// Mirrors backend DNSBlocklistService.checkQuotas' nxdomain-mode-only cap
// (model.DNSBlocklistMaxNXDomainDomains) so the mock upload/create/update/
// toggle paths can exercise the same error path the real backend returns —
// only the nxdomain cap is enforced here (not the overall
// DNSBlocklistMaxTotalDomains cap), matching what T-11 asks for.
function checkLocalNXDomainQuota(current: DNSBlocklist[], excludeId: string, mode: "nxdomain" | "sinkhole", candidateDomains: number, willBeEnabled: boolean) {
  if (!willBeEnabled || mode !== "nxdomain") return;
  let total = candidateDomains;
  for (const l of current) {
    if (l.id === excludeId || !l.enabled || l.blockMode !== "nxdomain") continue;
    total += l.domainCount;
  }
  if (total > DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS) {
    throw new Error(`Enabled nxdomain-mode blocklists would total ${total} domains, exceeding the maximum of ${DNS_BLOCKLIST_NXDOMAIN_MAX_DOMAINS}.`);
  }
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

  // --- DNS blocklist import (docs/ref/todo/dns-blocklist-import-plan.md) ---
  // Bulk subscribe-URL/upload hosts-format blocklists — distinct from the
  // deny-list functions above. Backend endpoints: /api/dns/blocklists (T-08).

  getBlocklists: async (): Promise<DNSBlocklist[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      return getLocalBlocklists();
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocklists`);
    if (!response.ok) {
      await parseBlocklistError(response, `Failed to fetch blocklists: ${response.statusText}`);
    }
    return response.json();
  },

  createBlocklistFromUrl: async (name: string, url: string, blockMode: "sinkhole" | "nxdomain", enabled: boolean): Promise<DNSBlocklist> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      const current = getLocalBlocklists();
      if (current.length >= DNS_BLOCKLISTS_MAX) {
        throw new Error(`Maximum of ${DNS_BLOCKLISTS_MAX} blocklists already reached.`);
      }
      // Mock fetch: no real network round-trip to the subscribe URL — fabricate
      // a plausible domain count instead of actually downloading it.
      const domainCount = Math.floor(Math.random() * 2000) + 500;
      checkLocalNXDomainQuota(current, "", blockMode, domainCount, enabled);
      const now = new Date().toISOString();
      const newList: DNSBlocklist = {
        id: "bl-" + Math.random().toString(36).substring(2, 10),
        name,
        sourceType: "url",
        url,
        blockMode,
        enabled,
        domainCount,
        fileBytes: domainCount * 31,
        sha256: Math.random().toString(36).substring(2, 10).repeat(4).slice(0, 64),
        lastFetchedAt: now,
        lastError: "",
        createdAt: now,
      };
      saveLocalBlocklists([...current, newList]);
      return newList;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocklists`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ name, url, blockMode, enabled }),
    });
    if (!response.ok) {
      await parseBlocklistError(response, `Failed to create blocklist: ${response.statusText}`);
    }
    return response.json();
  },

  uploadBlocklist: async (name: string, file: File, blockMode: "sinkhole" | "nxdomain", enabled: boolean): Promise<DNSBlocklist> => {
    const text = await file.text();

    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      const current = getLocalBlocklists();
      if (current.length >= DNS_BLOCKLISTS_MAX) {
        throw new Error(`Maximum of ${DNS_BLOCKLISTS_MAX} blocklists already reached.`);
      }
      const domainCount = estimateHostsDomainCount(text);
      checkLocalNXDomainQuota(current, "", blockMode, domainCount, enabled);
      const now = new Date().toISOString();
      const newList: DNSBlocklist = {
        id: "bl-" + Math.random().toString(36).substring(2, 10),
        name,
        sourceType: "upload",
        blockMode,
        enabled,
        domainCount,
        fileBytes: file.size,
        sha256: Math.random().toString(36).substring(2, 10).repeat(4).slice(0, 64),
        lastFetchedAt: now,
        lastError: "",
        createdAt: now,
      };
      saveLocalBlocklists([...current, newList]);
      return newList;
    }

    const qs = new URLSearchParams({ name, blockMode, enabled: String(enabled) });
    const response = await fetch(`${API_BASE_URL}/dns/blocklists/upload?${qs.toString()}`, {
      method: "POST",
      headers: {
        "Content-Type": "text/plain",
      },
      body: text,
    });
    if (!response.ok) {
      await parseBlocklistError(response, `Failed to upload blocklist: ${response.statusText}`);
    }
    return response.json();
  },

  updateBlocklist: async (id: string, input: { name: string; url?: string; blockMode: "sinkhole" | "nxdomain"; enabled: boolean }): Promise<DNSBlocklist> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalBlocklists();
      const existing = current.find((l) => l.id === id);
      if (!existing) throw new Error("Blocklist not found");
      checkLocalNXDomainQuota(current, id, input.blockMode, existing.domainCount, input.enabled);
      const updated = current.map((l) =>
        l.id === id
          ? { ...l, name: input.name, url: l.sourceType === "url" ? input.url ?? l.url : l.url, blockMode: input.blockMode, enabled: input.enabled }
          : l
      );
      saveLocalBlocklists(updated);
      const target = updated.find((l) => l.id === id);
      if (!target) throw new Error("Blocklist not found");
      return target;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocklists/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(input),
    });
    if (!response.ok) {
      await parseBlocklistError(response, `Failed to update blocklist: ${response.statusText}`);
    }
    return response.json();
  },

  deleteBlocklist: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalBlocklists();
      saveLocalBlocklists(current.filter((l) => l.id !== id));
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocklists/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      await parseBlocklistError(response, `Failed to delete blocklist: ${response.statusText}`);
    }
    return true;
  },

  toggleBlocklist: async (id: string): Promise<DNSBlocklist> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalBlocklists();
      const existing = current.find((l) => l.id === id);
      if (!existing) throw new Error("Blocklist not found");
      if (!existing.enabled) {
        checkLocalNXDomainQuota(current, id, existing.blockMode, existing.domainCount, true);
      }
      const updated = current.map((l) => (l.id === id ? { ...l, enabled: !l.enabled } : l));
      saveLocalBlocklists(updated);
      const target = updated.find((l) => l.id === id);
      if (!target) throw new Error("Blocklist not found");
      return target;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocklists/${id}/toggle`, {
      method: "POST",
    });
    if (!response.ok) {
      await parseBlocklistError(response, `Failed to toggle blocklist: ${response.statusText}`);
    }
    return response.json();
  },

  refreshBlocklist: async (id: string): Promise<DNSBlocklist> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 800));
      const current = getLocalBlocklists();
      const existing = current.find((l) => l.id === id);
      if (!existing) throw new Error("Blocklist not found");
      if (existing.sourceType !== "url") {
        throw new Error(`Blocklist ${id} is not a subscribe-URL list, cannot refresh.`);
      }
      // Mock refresh: no real network round-trip — fabricate a slightly
      // different domain count so a Refresh visibly does something.
      const domainCount = Math.max(1, existing.domainCount + Math.floor(Math.random() * 21) - 10);
      const updated = current.map((l) =>
        l.id === id
          ? { ...l, domainCount, fileBytes: domainCount * 31, lastFetchedAt: new Date().toISOString(), lastError: "" }
          : l
      );
      saveLocalBlocklists(updated);
      const target = updated.find((l) => l.id === id);
      if (!target) throw new Error("Blocklist not found");
      return target;
    }

    const response = await fetch(`${API_BASE_URL}/dns/blocklists/${id}/refresh`, {
      method: "POST",
    });
    if (!response.ok) {
      await parseBlocklistError(response, `Failed to refresh blocklist: ${response.statusText}`);
    }
    return response.json();
  },
};
