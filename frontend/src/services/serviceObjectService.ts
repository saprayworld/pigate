import { type ServiceEntry, type ServiceObject, initialServiceObjects } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { syncReferences, propagateServiceRename } from "./mockSync"

const LOCAL_STORAGE_KEY = "pigate_service_objects";

// Normalizes a service object payload so `entries` is always populated,
// whether it came from an old localStorage record (pre-multi-value, no
// `entries` at all — see docs/ref/todo/
// multi-value-address-service-objects-plan.md Caution 9) or from the real
// API. If `entries` is missing/empty, it's derived from the legacy
// protocol/port pair; the legacy protocol/port fields are then kept in sync
// with entries[0] so both shapes stay consistent.
function normalizeService<T extends Partial<ServiceObject>>(obj: T): T & Pick<ServiceObject, "protocol" | "port" | "entries"> {
  const entries: ServiceEntry[] =
    Array.isArray(obj.entries) && obj.entries.length > 0
      ? obj.entries
      : [{ protocol: (obj.protocol ?? "TCP") as ServiceEntry["protocol"], port: obj.port ?? "" }];
  return {
    ...obj,
    entries,
    protocol: entries[0].protocol,
    port: entries[0].port,
  };
}

// Helper to get data from LocalStorage (initializes with initialServiceObjects if empty)
function getLocalServices(): ServiceObject[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialServiceObjects));
    return initialServiceObjects;
  }
  try {
    const parsed = JSON.parse(stored);
    return (parsed as ServiceObject[]).map(normalizeService);
  } catch (e) {
    console.error("Failed to parse local services, resetting to mock data:", e);
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialServiceObjects));
    return initialServiceObjects;
  }
}

// Helper to save data to LocalStorage
function saveLocalServices(services: ServiceObject[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(services));
}

export const serviceObjectService = {
  // Fetch all service objects
  getAll: async (): Promise<ServiceObject[]> => {
    if (IS_MOCK_MODE) {
      // Simulate network latency
      await new Promise((resolve) => setTimeout(resolve, 300));
      syncReferences();
      return getLocalServices();
    }

    const response = await fetch(`${API_BASE_URL}/services`);
    if (!response.ok) {
      throw new Error(`Failed to fetch services: ${response.statusText}`);
    }
    const data: ServiceObject[] = await response.json();
    return data.map(normalizeService);
  },

  // Create a new custom service object
  create: async (
    obj: Omit<ServiceObject, "id" | "type" | "refPolicies">
  ): Promise<ServiceObject> => {
    const normalizedObj = normalizeService(obj);
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalServices();
      const newService: ServiceObject = {
        ...normalizedObj,
        id: "svc-" + Math.random().toString(36).substring(2, 9),
        type: "custom",
        refPolicies: [], // New objects start with no policies referencing them
      };
      saveLocalServices([...current, newService]);
      syncReferences();
      return newService;
    }

    const response = await fetch(`${API_BASE_URL}/services`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(normalizedObj),
    });
    if (!response.ok) {
      throw new Error(`Failed to create service: ${response.statusText}`);
    }
    return normalizeService(await response.json());
  },

  // Update an existing custom service object
  update: async (
    id: string,
    obj: Omit<ServiceObject, "id" | "type" | "refPolicies">
  ): Promise<ServiceObject> => {
    const normalizedObj = normalizeService(obj);
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalServices();
      const target = current.find((s) => s.id === id);
      if (!target) {
        throw new Error(`Service object with id ${id} not found`);
      }
      if (target.type === "system") {
        throw new Error(`Cannot update system predefined service objects`);
      }
      const oldName = target.name;
      const oldPort = target.port;
      const oldProto = target.protocol;
      const newName = normalizedObj.name;
      const newPort = normalizedObj.port;
      const newProto = normalizedObj.protocol;

      const updatedService: ServiceObject = {
        ...target,
        ...normalizedObj,
      };
      const updatedList = current.map((s) => (s.id === id ? updatedService : s));
      saveLocalServices(updatedList);

      if (oldName !== newName || oldPort !== newPort || oldProto !== newProto) {
        propagateServiceRename(oldName, oldPort, oldProto, newName, newPort, newProto);
      }
      syncReferences();

      return updatedService;
    }

    const response = await fetch(`${API_BASE_URL}/services/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(normalizedObj),
    });
    if (!response.ok) {
      throw new Error(`Failed to update service: ${response.statusText}`);
    }
    return normalizeService(await response.json());
  },

  // Delete a custom service object
  delete: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalServices();
      const target = current.find((s) => s.id === id);
      if (!target) {
        throw new Error(`Service object with id ${id} not found`);
      }
      if (target.type === "system") {
        throw new Error(`Cannot delete system predefined service objects`);
      }
      if (target.refPolicies.length > 0) {
        throw new Error(`Cannot delete service referenced by firewall policies.`);
      }
      const updatedList = current.filter((s) => s.id !== id);
      saveLocalServices(updatedList);
      syncReferences();
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/services/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete service: ${response.statusText}`);
    }
    return true;
  },
};
