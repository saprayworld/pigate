import { IS_MOCK_MODE, API_BASE_URL } from "./config";

// Mirrors backend model.CapabilityStatus / model.SystemCapabilities
// (docs/openapi.yaml /system/capabilities, issue #94).
export interface CapabilityStatus {
  id: string;
  name: string;
  available: boolean;
  degraded: boolean;
  reason: string;
  detail: string;
  checkedAt: string;
}

export interface SystemCapabilities {
  mock: boolean;
  checkedAt: string;
  capabilities: CapabilityStatus[];
}

// Kept in sync with the backend registry (kernel/real_capability.go,
// kernel/mock.go) and the Thai display names in
// service/system_capability.go's capabilityCatalog.
const MOCK_CAPABILITIES: CapabilityStatus[] = [
  { id: "firewall", name: "Firewall (nftables)", available: true, degraded: false, reason: "mock", detail: "กำลังรันในโหมดจำลอง (mock) — ไม่มีการเรียก kernel จริง", checkedAt: new Date().toISOString() },
  { id: "dbus", name: "D-Bus System Bus", available: true, degraded: false, reason: "mock", detail: "กำลังรันในโหมดจำลอง (mock) — ไม่มีการเรียก kernel จริง", checkedAt: new Date().toISOString() },
  { id: "dnsmasq", name: "DHCP/DNS Forwarder (dnsmasq)", available: true, degraded: false, reason: "mock", detail: "กำลังรันในโหมดจำลอง (mock) — ไม่มีการเรียก kernel จริง", checkedAt: new Date().toISOString() },
  { id: "resolved", name: "DNS Resolver (systemd-resolved)", available: true, degraded: false, reason: "mock", detail: "กำลังรันในโหมดจำลอง (mock) — ไม่มีการเรียก kernel จริง", checkedAt: new Date().toISOString() },
];

// Get kernel capability detection status. Pass force=true for the "ตรวจสอบใหม่"
// button, which bypasses the backend's ~30s cache.
export async function getCapabilities(force = false): Promise<SystemCapabilities> {
  if (IS_MOCK_MODE) {
    await new Promise((resolve) => setTimeout(resolve, 150));
    return { mock: true, checkedAt: new Date().toISOString(), capabilities: MOCK_CAPABILITIES };
  }

  const qs = force ? "?force=1" : "";
  const response = await fetch(`${API_BASE_URL}/system/capabilities${qs}`);
  if (!response.ok) {
    const errBody = await response.json().catch(() => ({}));
    throw new Error(errBody.message || `Failed to fetch system capabilities: ${response.statusText}`);
  }
  return response.json();
}
