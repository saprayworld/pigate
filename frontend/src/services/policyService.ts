import { type PolicyChain, type PolicyRule, initialPolicyRules } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"
import { syncReferences } from "./mockSync"

const LOCAL_STORAGE_KEY = "pigate_firewall_policies";

// Rows saved before the input/output chain feature shipped have no `chain`
// field at all — normalize to "forward" so every consumer of getLocalPolicies
// can rely on chain always being set (mirrors the backend's
// model.NormalizePolicyChain / migration DEFAULT 'forward').
function normalizeChain(rules: PolicyRule[]): PolicyRule[] {
  return rules.map((r) => (r.chain ? r : { ...r, chain: "forward" as PolicyChain }));
}

// Helper to get data from LocalStorage (initializes with initialPolicyRules if empty)
function getLocalPolicies(): PolicyRule[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialPolicyRules));
    return initialPolicyRules;
  }
  try {
    return normalizeChain(JSON.parse(stored));
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

// Reads the backend's real failure reason from the response body ({"message": ...},
// see api/handlers.go's writeError) instead of throwing a generic
// response.statusText (which just says "Internal Server Error" and hides e.g.
// the real nftables error) — mirrors userService.ts's parseError.
async function parseError(response: Response, fallback: string): Promise<never> {
  const errBody = await response.json().catch(() => ({}));
  throw new Error(errBody.message || fallback);
}

export const policyService = {
  // Fetch firewall rules. chain filters to one chain (forward/input/output);
  // omitted returns every chain (used e.g. by mock sync).
  getAll: async (chain?: PolicyChain): Promise<PolicyRule[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      syncReferences();
      const all = getLocalPolicies();
      return chain ? all.filter((r) => r.chain === chain) : all;
    }

    const qs = chain ? `?chain=${encodeURIComponent(chain)}` : "";
    const response = await fetch(`${API_BASE_URL}/policies${qs}`);
    if (!response.ok) {
      await parseError(response, `Failed to fetch policies: ${response.statusText}`);
    }
    return response.json();
  },

  // Save one chain's rules list in the new order (used after reordering/drag-and-drop).
  // Only policies belonging to `chain` are touched — other chains' rules and
  // ordering are left untouched (backend rejects an id that doesn't belong to
  // `chain`, see docs/ref/todo/input-output-chain-firewall-plan.md Caution 3).
  saveAll: async (chain: PolicyChain, policies: PolicyRule[]): Promise<PolicyRule[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const others = getLocalPolicies().filter((r) => r.chain !== chain);
      saveLocalPolicies([...others, ...policies]);
      syncReferences();
      return policies;
    }

    const response = await fetch(`${API_BASE_URL}/policies/reorder`, {
      method: "PUT",
      headers: {
        "Content-Type": "application/json",
      },
      body: JSON.stringify({ chain, policies }),
    });
    if (!response.ok) {
      await parseError(response, `Failed to reorder policies: ${response.statusText}`);
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
      syncReferences();
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
      await parseError(response, `Failed to create policy: ${response.statusText}`);
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
      syncReferences();
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
      await parseError(response, `Failed to update policy: ${response.statusText}`);
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
      syncReferences();
      return true;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      await parseError(response, `Failed to delete policy: ${response.statusText}`);
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
      syncReferences();
      return updatedList.find((r) => r.id === id)!;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}/toggle-log`, {
      method: "POST",
    });
    if (!response.ok) {
      await parseError(response, `Failed to toggle log: ${response.statusText}`);
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
      syncReferences();
      return updatedList.find((r) => r.id === id)!;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}/toggle-status`, {
      method: "POST",
    });
    if (!response.ok) {
      await parseError(response, `Failed to toggle status: ${response.statusText}`);
    }
    return response.json();
  },

  // Toggle the persisted "Monitor" opt-in on a rule (docs/ref/todo/
  // fqdn-retry-and-monitored-counters-plan.md D-6/T-11, issue #141). Turning
  // it on starts a persisted running total from 0; turning it off discards
  // it permanently (the caller is responsible for a confirm dialog before
  // calling this with the intent to turn it off).
  toggleMonitor: async (id: string): Promise<PolicyRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      const current = getLocalPolicies();
      const updatedList = current.map((r) =>
        r.id === id ? { ...r, monitored: !r.monitored } : r
      );
      saveLocalPolicies(updatedList);
      syncReferences();
      return updatedList.find((r) => r.id === id)!;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}/toggle-monitor`, {
      method: "POST",
    });
    if (!response.ok) {
      await parseError(response, `Failed to toggle monitor: ${response.statusText}`);
    }
    return response.json();
  },

  // Reset a rule's persisted Monitor counter back to 0 (docs/ref/todo/
  // fqdn-retry-and-monitored-counters-plan.md D-6/T-11). Caller is
  // responsible for a confirm dialog before calling this.
  resetMonitorCounter: async (id: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 150));
      return;
    }

    const response = await fetch(`${API_BASE_URL}/policies/${id}/monitor/reset`, {
      method: "POST",
    });
    if (!response.ok) {
      await parseError(response, `Failed to reset monitor counter: ${response.statusText}`);
    }
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
      await parseError(response, `Failed to apply policy to kernel: ${response.statusText}`);
    }
    return true;
  },
};
