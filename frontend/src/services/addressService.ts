import { type AddressEntry, type AddressObject, initialAddressObjects } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { syncReferences, propagateAddressRename } from "./mockSync"

const LOCAL_STORAGE_KEY = "pigate_addresses";

// Normalizes an address object payload so `entries` is always populated,
// whether it came from an old localStorage record (pre-multi-value, no
// `entries` at all — see docs/ref/todo/
// multi-value-address-service-objects-plan.md Caution 9) or from the real
// API. If `entries` is missing/empty, it's derived from the legacy
// type/value pair; the legacy type/value fields are then kept in sync with
// entries[0] so both shapes stay consistent.
function normalizeAddress<T extends Partial<AddressObject>>(obj: T): T & Pick<AddressObject, "type" | "value" | "entries"> {
  const entries: AddressEntry[] =
    Array.isArray(obj.entries) && obj.entries.length > 0
      ? obj.entries
      : [{ type: (obj.type ?? "subnet") as AddressEntry["type"], value: obj.value ?? "" }];
  return {
    ...obj,
    entries,
    type: entries[0].type,
    value: entries[0].value,
  };
}

// Helper to get data from LocalStorage (initializes with initialAddressObjects if empty)
function getLocalAddresses(): AddressObject[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialAddressObjects));
    return initialAddressObjects;
  }
  try {
    const parsed = JSON.parse(stored);
    return (parsed as AddressObject[]).map(normalizeAddress);
  } catch (e) {
    console.error("Failed to parse local addresses, resetting to mock data:", e);
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialAddressObjects));
    return initialAddressObjects;
  }
}

// Helper to save data to LocalStorage
function saveLocalAddresses(addresses: AddressObject[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(addresses));
}

export const addressService = {
  // Fetch all address objects
  getAll: async (): Promise<AddressObject[]> => {
    if (IS_MOCK_MODE) {
      // Simulate network latency
      await new Promise((resolve) => setTimeout(resolve, 300));
      syncReferences();
      return getLocalAddresses();
    }

    const response = await fetch(`${API_BASE_URL}/addresses`);
    if (!response.ok) {
      throw new Error(`Failed to fetch addresses: ${response.statusText}`);
    }
    const data: AddressObject[] = await response.json();
    return data.map(normalizeAddress);
  },

  // Create a new address object
  create: async (
    obj: Omit<AddressObject, "id" | "refPolicies" | "system">
  ): Promise<AddressObject> => {
    const normalizedObj = normalizeAddress(obj);
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalAddresses();
      const newAddress: AddressObject = {
        ...normalizedObj,
        id: "addr-" + Math.random().toString(36).substring(2, 9),
        system: false,
        refPolicies: [], // New objects start with no policies referencing them
      };
      saveLocalAddresses([...current, newAddress]);
      syncReferences();
      return newAddress;
    }

    const response = await fetch(`${API_BASE_URL}/addresses`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(normalizedObj),
    });
    if (!response.ok) {
      throw new Error(`Failed to create address: ${response.statusText}`);
    }
    return normalizeAddress(await response.json());
  },

  // Update an existing address object
  update: async (
    id: string,
    obj: Omit<AddressObject, "id" | "refPolicies" | "system">
  ): Promise<AddressObject> => {
    const normalizedObj = normalizeAddress(obj);
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalAddresses();
      const target = current.find((a) => a.id === id);
      if (!target) {
        throw new Error(`Address object with id ${id} not found`);
      }
      if (target.system) {
        throw new Error(`Cannot update system predefined address objects`);
      }
      const oldName = target.name;
      const newName = normalizedObj.name;
      const updatedAddress: AddressObject = {
        ...target,
        ...normalizedObj,
      };
      const updatedList = current.map((a) => (a.id === id ? updatedAddress : a));
      saveLocalAddresses(updatedList);
      if (oldName !== newName) {
        propagateAddressRename(oldName, newName);
      }
      syncReferences();
      return updatedAddress;
    }

    const response = await fetch(`${API_BASE_URL}/addresses/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(normalizedObj),
    });
    if (!response.ok) {
      throw new Error(`Failed to update address: ${response.statusText}`);
    }
    return normalizeAddress(await response.json());
  },

  // Delete an address object
  delete: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalAddresses();
      const target = current.find((a) => a.id === id);
      if (!target) {
        throw new Error(`Address object with id ${id} not found`);
      }
      if (target.system) {
        throw new Error(`Cannot delete system predefined address objects`);
      }
      if (target.refPolicies.length > 0) {
        throw new Error(`Cannot delete address referenced by firewall policies.`);
      }
      const updatedList = current.filter((a) => a.id !== id);
      saveLocalAddresses(updatedList);
      syncReferences();
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/addresses/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete address: ${response.statusText}`);
    }
    return true;
  },

  // Delete multiple address objects
  deleteMultiple: async (ids: string[]): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalAddresses();
      const targets = current.filter((a) => ids.includes(a.id));
      const systemObjects = targets.filter((a) => a.system);
      if (systemObjects.length > 0) {
        throw new Error(
          `Cannot delete system predefined address objects: ${systemObjects
            .map((a) => a.name)
            .join(", ")}`
        );
      }
      const usedInPolicies = targets.filter((a) => a.refPolicies.length > 0);
      if (usedInPolicies.length > 0) {
        throw new Error(
          `Cannot delete addresses referenced by firewall policies: ${usedInPolicies
            .map((a) => a.name)
            .join(", ")}`
        );
      }
      const updatedList = current.filter((a) => !ids.includes(a.id));
      saveLocalAddresses(updatedList);
      syncReferences();
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/addresses/bulk-delete`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ ids }),
    });
    if (!response.ok) {
      throw new Error(`Failed to bulk delete addresses: ${response.statusText}`);
    }
    return true;
  },
};
