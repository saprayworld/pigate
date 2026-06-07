import { type AddressObject, initialAddressObjects } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const LOCAL_STORAGE_KEY = "pigate_addresses";

// Helper to get data from LocalStorage (initializes with initialAddressObjects if empty)
function getLocalAddresses(): AddressObject[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialAddressObjects));
    return initialAddressObjects;
  }
  try {
    return JSON.parse(stored);
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
      return getLocalAddresses();
    }

    const response = await fetch(`${API_BASE_URL}/addresses`);
    if (!response.ok) {
      throw new Error(`Failed to fetch addresses: ${response.statusText}`);
    }
    return response.json();
  },

  // Create a new address object
  create: async (
    obj: Omit<AddressObject, "id" | "refPolicies">
  ): Promise<AddressObject> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalAddresses();
      const newAddress: AddressObject = {
        ...obj,
        id: "addr-" + Math.random().toString(36).substring(2, 9),
        refPolicies: [], // New objects start with no policies referencing them
      };
      saveLocalAddresses([...current, newAddress]);
      return newAddress;
    }

    const response = await fetch(`${API_BASE_URL}/addresses`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(obj),
    });
    if (!response.ok) {
      throw new Error(`Failed to create address: ${response.statusText}`);
    }
    return response.json();
  },

  // Update an existing address object
  update: async (
    id: string,
    obj: Omit<AddressObject, "id" | "refPolicies">
  ): Promise<AddressObject> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalAddresses();
      const target = current.find((a) => a.id === id);
      if (!target) {
        throw new Error(`Address object with id ${id} not found`);
      }
      const updatedAddress: AddressObject = {
        ...target,
        ...obj,
      };
      const updatedList = current.map((a) => (a.id === id ? updatedAddress : a));
      saveLocalAddresses(updatedList);
      return updatedAddress;
    }

    const response = await fetch(`${API_BASE_URL}/addresses/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(obj),
    });
    if (!response.ok) {
      throw new Error(`Failed to update address: ${response.statusText}`);
    }
    return response.json();
  },

  // Delete an address object
  delete: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalAddresses();
      const target = current.find((a) => a.id === id);
      if (target && target.refPolicies.length > 0) {
        throw new Error(`Cannot delete address referenced by firewall policies.`);
      }
      const updatedList = current.filter((a) => a.id !== id);
      saveLocalAddresses(updatedList);
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
