import { IS_MOCK_MODE, API_BASE_URL } from "./config"

export interface QosRule {
  id: string
  name: string
  interface: string
  matchSrcIp: string
  matchDstIp: string
  egressRateMbps: number
  egressCeilMbps: number
  ingressRateMbps: number
  ingressCeilMbps: number
  priority: number
  status: boolean
  description: string
}

export interface QosClass {
  classId: string
  rate: string
  ceil: string
  ruleName: string
}

export interface QosIfaceStatus {
  interface: string
  hasQdisc: boolean
  classes: QosClass[]
  // Whether the kernel has the IFB module (probed at backend startup). When
  // false, ingress (upload) shaping is skipped and only egress is applied.
  ingressSupported: boolean
}

const LOCAL_STORAGE_KEY = "pigate_qos_rules";

const initialQosRules: QosRule[] = [
  {
    id: "qos-1",
    name: "Standard LAN Limit",
    interface: "eth0",
    matchSrcIp: "192.168.1.0/24",
    matchDstIp: "0.0.0.0/0",
    egressRateMbps: 50,
    egressCeilMbps: 100,
    ingressRateMbps: 10,
    ingressCeilMbps: 20,
    priority: 10,
    status: true,
    description: "Default limit for the LAN subnet to prevent hogging WAN bandwidth"
  }
];

function getLocalQosRules(): QosRule[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialQosRules));
    return initialQosRules;
  }
  try {
    return JSON.parse(stored);
  } catch (e) {
    console.error("Failed to parse local QoS rules, resetting to initial data:", e);
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialQosRules));
    return initialQosRules;
  }
}

function saveLocalQosRules(rules: QosRule[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(rules));
}

export const qosService = {
  getAll: async (): Promise<QosRule[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      return getLocalQosRules();
    }

    const response = await fetch(`${API_BASE_URL}/qos/rules`);
    if (!response.ok) {
      throw new Error(`Failed to fetch QoS rules: ${response.statusText}`);
    }
    return response.json();
  },

  create: async (rule: Omit<QosRule, "id">): Promise<QosRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalQosRules();
      const newRule: QosRule = {
        ...rule,
        id: "qos-" + Math.random().toString(36).substring(2, 9),
      };
      saveLocalQosRules([...current, newRule]);
      return newRule;
    }

    const response = await fetch(`${API_BASE_URL}/qos/rules`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(rule),
    });
    if (!response.ok) {
      throw new Error(`Failed to create QoS rule: ${response.statusText}`);
    }
    return response.json();
  },

  update: async (id: string, rule: Omit<QosRule, "id">): Promise<QosRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalQosRules();
      const idx = current.findIndex((r) => r.id === id);
      if (idx === -1) throw new Error("QoS rule not found");
      const updatedRule: QosRule = { ...rule, id };
      current[idx] = updatedRule;
      saveLocalQosRules(current);
      return updatedRule;
    }

    const response = await fetch(`${API_BASE_URL}/qos/rules/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(rule),
    });
    if (!response.ok) {
      throw new Error(`Failed to update QoS rule: ${response.statusText}`);
    }
    return response.json();
  },

  delete: async (id: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalQosRules();
      saveLocalQosRules(current.filter((r) => r.id !== id));
      return;
    }

    const response = await fetch(`${API_BASE_URL}/qos/rules/${id}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to delete QoS rule: ${response.statusText}`);
    }
  },

  toggle: async (id: string): Promise<QosRule> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      const current = getLocalQosRules();
      const idx = current.findIndex((r) => r.id === id);
      if (idx === -1) throw new Error("QoS rule not found");
      current[idx].status = !current[idx].status;
      saveLocalQosRules(current);
      return current[idx];
    }

    const response = await fetch(`${API_BASE_URL}/qos/rules/${id}/toggle`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to toggle QoS rule status: ${response.statusText}`);
    }
    return response.json();
  },

  sync: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      return;
    }

    const response = await fetch(`${API_BASE_URL}/qos/sync`, {
      method: "POST",
    });
    if (!response.ok) {
      throw new Error(`Failed to sync QoS rules to kernel: ${response.statusText}`);
    }
  },

  getIfaceStatus: async (iface: string): Promise<QosIfaceStatus> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const rules = getLocalQosRules().filter((r) => r.interface === iface && r.status);
      const classes: QosClass[] = [];
      rules.forEach((r, i) => {
        if (r.egressRateMbps > 0) {
          classes.push({
            classId: `Egress 1:${10 + i}`,
            rate: `${r.egressRateMbps}Mbit`,
            ceil: `${r.egressCeilMbps}Mbit`,
            ruleName: r.name
          });
        }
        if (r.ingressRateMbps > 0) {
          classes.push({
            classId: `Ingress 1:${10 + i}`,
            rate: `${r.ingressRateMbps}Mbit`,
            ceil: `${r.ingressCeilMbps}Mbit`,
            ruleName: r.name
          });
        }
      });

      return {
        interface: iface,
        hasQdisc: classes.length > 0,
        classes: classes,
        ingressSupported: true
      };
    }

    const response = await fetch(`${API_BASE_URL}/qos/status/${iface}`);
    if (!response.ok) {
      throw new Error(`Failed to fetch QoS interface status: ${response.statusText}`);
    }
    return response.json();
  },

  clearIface: async (iface: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const current = getLocalQosRules();
      const updated = current.map((r) => {
        if (r.interface === iface) {
          return { ...r, status: false };
        }
        return r;
      });
      saveLocalQosRules(updated);
      return;
    }

    const response = await fetch(`${API_BASE_URL}/qos/iface/${iface}`, {
      method: "DELETE",
    });
    if (!response.ok) {
      throw new Error(`Failed to clear QoS on interface: ${response.statusText}`);
    }
  }
}
