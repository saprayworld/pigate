// Central Event Log API client (backend: /api/logs/events)
// Mirrors the SystemEvent schema in openapi.yaml.

import { IS_MOCK_MODE, API_BASE_URL } from "./config";

export type EventSeverity = "info" | "warning" | "error" | "critical";

export type EventCategory =
  | "auth"
  | "user"
  | "network"
  | "firewall"
  | "route"
  | "dhcp"
  | "dns"
  | "qos"
  | "system"
  | "config";

export interface SystemEvent {
  id: number;
  time: string; // RFC3339 UTC — convert to local time for display
  category: EventCategory;
  action: string;
  severity: EventSeverity;
  actor: string;
  target: string;
  message: string;
}

export interface EventQuery {
  category?: string;
  severity?: string;
  q?: string;
  limit?: number;
  offset?: number;
}

export interface EventPage {
  events: SystemEvent[];
  total: number;
}

// ---------------------------------------------------------------------------
// Mock-mode data: generated once per session, in memory (no backend involved)
// ---------------------------------------------------------------------------

let mockEvents: SystemEvent[] | null = null;

function getMockEvents(): SystemEvent[] {
  if (mockEvents) return mockEvents;

  const templates: Array<
    Pick<SystemEvent, "category" | "action" | "severity" | "actor" | "target" | "message">
  > = [
    { category: "system", action: "system.boot", severity: "info", actor: "system", target: "host", message: "PiGate backend started (version mock)" },
    { category: "auth", action: "login.success", severity: "info", actor: "pigate", target: "pigate", message: "User pigate logged in" },
    { category: "auth", action: "login.failed", severity: "warning", actor: "intruder", target: "intruder", message: "Login failed for intruder (unknown username)" },
    { category: "user", action: "user.created", severity: "info", actor: "pigate", target: "viewer", message: "User viewer created (role admin_readonly)" },
    { category: "network", action: "network.interface_changed", severity: "info", actor: "pigate", target: "wlan0", message: "Interface wlan0 configuration updated" },
    { category: "firewall", action: "firewall.applied", severity: "info", actor: "pigate", target: "nftables", message: "Firewall policies applied to kernel" },
    { category: "route", action: "route.created", severity: "info", actor: "pigate", target: "8.8.8.8/32", message: "Static route to 8.8.8.8/32 created" },
    { category: "dhcp", action: "dhcp.lease.add", severity: "info", actor: "system", target: "DC:A6:32:AA:BB:C1", message: "DHCP lease 192.168.1.105 assigned to DC:A6:32:AA:BB:C1 (laptop)" },
    { category: "dns", action: "dns.server_applied", severity: "info", actor: "pigate", target: "dnsmasq", message: "DNS server zones/records applied" },
    { category: "config", action: "config.exported", severity: "warning", actor: "pigate", target: "pigate-backup.json", message: "Configuration exported" },
    { category: "system", action: "system.reboot", severity: "critical", actor: "pigate", target: "host", message: "Reboot requested by pigate" },
  ];

  const events: SystemEvent[] = [];
  const now = Date.now();
  for (let i = 0; i < 60; i++) {
    const t = templates[i % templates.length];
    events.push({
      id: 60 - i,
      time: new Date(now - i * 7 * 60 * 1000).toISOString(),
      ...t,
    });
  }
  mockEvents = events;
  return events;
}

// ---------------------------------------------------------------------------
// Service
// ---------------------------------------------------------------------------

export const logService = {
  getEvents: async (query: EventQuery = {}): Promise<EventPage> => {
    const { category = "", severity = "", q = "", limit = 50, offset = 0 } = query;

    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      const needle = q.toLowerCase();
      const filtered = getMockEvents().filter(
        (ev) =>
          (!category || ev.category === category) &&
          (!severity || ev.severity === severity) &&
          (!needle ||
            [ev.message, ev.action, ev.actor, ev.target].some((s) =>
              s.toLowerCase().includes(needle)
            ))
      );
      return { events: filtered.slice(offset, offset + limit), total: filtered.length };
    }

    const params = new URLSearchParams();
    if (category) params.set("category", category);
    if (severity) params.set("severity", severity);
    if (q) params.set("q", q);
    params.set("limit", String(limit));
    params.set("offset", String(offset));

    const response = await fetch(`${API_BASE_URL}/logs/events?${params.toString()}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch system events: ${response.statusText}`);
    }
    return response.json();
  },

  // super_admin only — the backend records who cleared the log.
  clearEvents: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      mockEvents = [];
      return;
    }

    const response = await fetch(`${API_BASE_URL}/logs/events/clear`, { method: "POST" });
    if (!response.ok) {
      throw new Error(`Failed to clear system events: ${response.statusText}`);
    }
  },
};
