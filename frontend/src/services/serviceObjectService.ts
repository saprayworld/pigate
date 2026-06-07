import { type ServiceObject, initialServiceObjects } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const LOCAL_STORAGE_KEY = "pigate_service_objects";

// Helper to get data from LocalStorage (initializes with initialServiceObjects if empty)
function getLocalServices(): ServiceObject[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialServiceObjects));
    return initialServiceObjects;
  }
  try {
    return JSON.parse(stored);
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
      return getLocalServices();
    }

    const response = await fetch(`${API_BASE_URL}/services`);
    if (!response.ok) {
      throw new Error(`Failed to fetch services: ${response.statusText}`);
    }
    return response.json();
  },

  // Create a new custom service object
  create: async (
    obj: Omit<ServiceObject, "id" | "type" | "refPolicies">
  ): Promise<ServiceObject> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalServices();
      const newService: ServiceObject = {
        ...obj,
        id: "svc-" + Math.random().toString(36).substring(2, 9),
        type: "custom",
        refPolicies: [], // New objects start with no policies referencing them
      };
      saveLocalServices([...current, newService]);
      return newService;
    }

    const response = await fetch(`${API_BASE_URL}/services`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(obj),
    });
    if (!response.ok) {
      throw new Error(`Failed to create service: ${response.statusText}`);
    }
    return response.json();
  },

  // Update an existing custom service object
  update: async (
    id: string,
    obj: Omit<ServiceObject, "id" | "type" | "refPolicies">
  ): Promise<ServiceObject> => {
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
      const updatedService: ServiceObject = {
        ...target,
        ...obj,
      };
      const updatedList = current.map((s) => (s.id === id ? updatedService : s));
      saveLocalServices(updatedList);
      return updatedService;
    }

    const response = await fetch(`${API_BASE_URL}/services/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(obj),
    });
    if (!response.ok) {
      throw new Error(`Failed to update service: ${response.statusText}`);
    }
    return response.json();
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
