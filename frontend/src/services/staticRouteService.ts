import { type StaticRoute, initialStaticRoutes } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const LOCAL_STORAGE_KEY = "pigate_static_routes";

// Helper to get data from LocalStorage (initializes with initialStaticRoutes if empty)
function getLocalRoutes(): StaticRoute[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialStaticRoutes));
    return initialStaticRoutes;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    console.error("Failed to parse local routes, resetting to mock data:", e);
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialStaticRoutes));
    return initialStaticRoutes;
  }
}

// Helper to save data to LocalStorage
function saveLocalRoutes(routes: StaticRoute[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(routes));
}

export const staticRouteService = {
  // Fetch all static routing table rows
  getAll: async (): Promise<StaticRoute[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      return getLocalRoutes();
    }

    const response = await fetch(`${API_BASE_URL}/routes`);
    if (!response.ok) {
      throw new Error(`Failed to fetch static routes: ${response.statusText}`);
    }
    return response.json();
  },

  // Fetch routes configuration parameters (such as allowEditSystemRoutes)
  getConfig: async (): Promise<{ allowEditSystemRoutes: boolean }> => {
    if (IS_MOCK_MODE) {
      const stored = localStorage.getItem("pigate_allow_edit_system_routes") === "true";
      return { allowEditSystemRoutes: stored };
    }

    const response = await fetch(`${API_BASE_URL}/routes/config`);
    if (!response.ok) {
      throw new Error(`Failed to fetch routes configuration: ${response.statusText}`);
    }
    return response.json();
  },

  // Create a new custom static route
  create: async (
    obj: Omit<StaticRoute, "id" | "type">
  ): Promise<StaticRoute> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalRoutes();
      const newRoute: StaticRoute = {
        ...obj,
        id: "route-" + Math.random().toString(36).substring(2, 9),
        type: "custom",
      };
      saveLocalRoutes([...current, newRoute]);
      return newRoute;
    }

    const response = await fetch(`${API_BASE_URL}/routes`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(obj),
    });
    if (!response.ok) {
      throw new Error(`Failed to create static route: ${response.statusText}`);
    }
    return response.json();
  },

  // Update a static route
  update: async (
    id: string,
    obj: Omit<StaticRoute, "id" | "type">
  ): Promise<StaticRoute> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalRoutes();
      const target = current.find((r) => r.id === id);
      if (!target) {
        throw new Error(`Static route with id ${id} not found`);
      }
      const allowEdit = localStorage.getItem("pigate_allow_edit_system_routes") === "true";
      if (target.type === "system" && !allowEdit) {
        throw new Error(`Cannot update system predefined static routes`);
      }
      const updatedRoute: StaticRoute = {
        ...target,
        ...obj,
      };
      const updatedList = current.map((r) => (r.id === id ? updatedRoute : r));
      saveLocalRoutes(updatedList);
      return updatedRoute;
    }

    const response = await fetch(`${API_BASE_URL}/routes/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(obj),
    });
    if (!response.ok) {
      throw new Error(`Failed to update static route: ${response.statusText}`);
    }
    return response.json();
  },

  // Delete a static route
  delete: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalRoutes();
      const target = current.find((r) => r.id === id);
      if (!target) {
        throw new Error(`Static route with id ${id} not found`);
      }
      const allowEdit = localStorage.getItem("pigate_allow_edit_system_routes") === "true";
      if (target.type === "system" && !allowEdit) {
        throw new Error(`Cannot delete system predefined static routes`);
      }
      const updatedList = current.filter((r) => r.id !== id);
      saveLocalRoutes(updatedList);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/routes/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete static route: ${response.statusText}`);
    }
    return true;
  },

  // Delete multiple static routes
  deleteMultiple: async (ids: string[]): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalRoutes();
      const targets = current.filter((r) => ids.includes(r.id));
      const hasSystem = targets.some((r) => r.type === "system");
      const allowEdit = localStorage.getItem("pigate_allow_edit_system_routes") === "true";
      if (hasSystem && !allowEdit) {
        throw new Error(`Cannot delete system predefined static routes in bulk`);
      }
      const updatedList = current.filter((r) => !ids.includes(r.id));
      saveLocalRoutes(updatedList);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/routes/bulk-delete`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ ids }),
    });
    if (!response.ok) {
      throw new Error(`Failed to bulk delete static routes: ${response.statusText}`);
    }
    return true;
  },

  // Toggle active status of a route
  toggleStatus: async (id: string): Promise<StaticRoute> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      const current = getLocalRoutes();
      const updatedList = current.map((r) =>
        r.id === id ? { ...r, status: !r.status } : r
      );
      saveLocalRoutes(updatedList);
      return updatedList.find((r) => r.id === id)!;
    }

    const response = await fetch(`${API_BASE_URL}/routes/${id}/toggle`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle static route: ${response.statusText}`);
    }
    return response.json();
  },

  // Apply routing table changes to Kernel
  apply: async (): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 1500));
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/routes/apply`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to apply routing changes: ${response.statusText}`);
    }
    return true;
  },
};
