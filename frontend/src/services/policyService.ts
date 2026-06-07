import { type PolicyRule, initialPolicyRules } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const LOCAL_STORAGE_KEY = "pigate_firewall_policies";

// Helper to get data from LocalStorage (initializes with initialPolicyRules if empty)
function getLocalPolicies(): PolicyRule[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialPolicyRules));
    return initialPolicyRules;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    console.error("Failed to parse local policies, resetting to mock data:", e);
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialPolicyRules));
    return initialPolicyRules;
  }
}

// Helper to save data to LocalStorage
function saveLocalPolicies(policies: PolicyRule[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(policies));
}

export const policyService = {
  // Fetch all firewall rules
  getAll: async (): Promise<PolicyRule[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      return getLocalPolicies();
    }

    const response = await fetch(`${API_BASE_URL}/policies`);
    if (!response.ok) {
      throw new Error(`Failed to fetch policies: ${response.statusText}`);
    }
    return response.json();
  },

  // Save the entire rules list (used after reordering/drag-and-drop)
  saveAll: async (policies: PolicyRule[]): Promise<PolicyRule[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      saveLocalPolicies(policies);
      return policies;
    }

    const response = await fetch(`${API_BASE_URL}/policies/reorder`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ policies }),
    });
    if (!response.ok) {
      throw new Error(`Failed to reorder policies: ${response.statusText}`);
    }
    return response.json();
  },

  // Create a new firewall rule
  create: async (
    rule: Omit<PolicyRule, "id">
  ): Promise<PolicyRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalPolicies();
      const newRule: PolicyRule = {
        ...rule,
        id: "rule-" + Math.random().toString(36).substring(2, 9),
      };
      saveLocalPolicies([...current, newRule]);
      return newRule;
    }

    const response = await fetch(`${API_BASE_URL}/policies`, {
      method: "POST",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(rule),
    });
    if (!response.ok) {
      throw new Error(`Failed to create policy: ${response.statusText}`);
    }
    return response.json();
  },

  // Update a firewall rule
  update: async (
    id: string,
    rule: Omit<PolicyRule, "id">
  ): Promise<PolicyRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350));
      const current = getLocalPolicies();
      const target = current.find((r) => r.id === id);
      if (!target) {
        throw new Error(`Policy rule with id ${id} not found`);
      }
      const updatedRule: PolicyRule = {
        ...target,
        ...rule,
      };
      const updatedList = current.map((r) => (r.id === id ? updatedRule : r));
      saveLocalPolicies(updatedList);
      return updatedRule;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify(rule),
    });
    if (!response.ok) {
      throw new Error(`Failed to update policy: ${response.statusText}`);
    }
    return response.json();
  },

  // Delete a firewall rule
  delete: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalPolicies();
      const updatedList = current.filter((r) => r.id !== id);
      saveLocalPolicies(updatedList);
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete policy: ${response.statusText}`);
    }
    return true;
  },

  // Toggle log state of a rule
  toggleLog: async (id: string): Promise<PolicyRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      const current = getLocalPolicies();
      const updatedList = current.map((r) =>
        r.id === id ? { ...r, log: !r.log } : r
      );
      saveLocalPolicies(updatedList);
      return updatedList.find((r) => r.id === id)!;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}/toggle-log`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle log: ${response.statusText}`);
    }
    return response.json();
  },

  // Toggle active status of a rule
  toggleStatus: async (id: string): Promise<PolicyRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      const current = getLocalPolicies();
      const updatedList = current.map((r) =>
        r.id === id ? { ...r, status: !r.status } : r
      );
      saveLocalPolicies(updatedList);
      return updatedList.find((r) => r.id === id)!;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}/toggle-status`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle status: ${response.statusText}`);
    }
    return response.json();
  },

  // Apply settings to Kernel (nftables reload)
  apply: async (): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      // Simulation is already handled on the frontend via sequential timeouts,
      // but we resolve this API call quickly to verify connectivity
      await new Promise((resolve) => setTimeout(resolve, 500));
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/policies/apply`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to apply policy to kernel: ${response.statusText}`);
    }
    return true;
  },
};
