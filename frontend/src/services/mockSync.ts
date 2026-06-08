// Helper to synchronize references between Firewall Policies, Address Objects, and Service Objects in Mock Mode.

const POLICIES_KEY = "pigate_firewall_policies";
const ADDRESSES_KEY = "pigate_addresses";
const SERVICES_KEY = "pigate_service_objects";

/**
 * Recalculates and updates the `refPolicies` arrays for all Address and Service objects
 * based on the current Firewall Policies stored in localStorage.
 */
export function syncReferences() {
  if (typeof window === "undefined") return;

  const policiesStr = localStorage.getItem(POLICIES_KEY);
  const addressesStr = localStorage.getItem(ADDRESSES_KEY);
  const servicesStr = localStorage.getItem(SERVICES_KEY);

  // If mock data isn't initialized yet, we do nothing and let the services initialize it first
  if (!policiesStr || !addressesStr || !servicesStr) return;

  try {
    const policies = JSON.parse(policiesStr);
    const addresses = JSON.parse(addressesStr);
    const services = JSON.parse(servicesStr);

    let addressesChanged = false;
    let servicesChanged = false;

    // 1. Recalculate Address Object references
    const updatedAddresses = addresses.map((addr: any) => {
      // An address is referenced if its name or value is in a policy's source or destination list
      const refs = policies
        .filter((policy: any) => {
          const srcMatch = policy.source && (policy.source.includes(addr.name) || policy.source.includes(addr.value));
          const destMatch = policy.destination && (policy.destination.includes(addr.name) || policy.destination.includes(addr.value));
          return srcMatch || destMatch;
        })
        .map((policy: any) => policy.name);

      const uniqueRefs = Array.from(new Set(refs)) as string[];
      
      // Check if references have changed
      const currentRefs = addr.refPolicies || [];
      const isSame =
        currentRefs.length === uniqueRefs.length &&
        currentRefs.every((val: string) => uniqueRefs.includes(val));

      if (!isSame) {
        addressesChanged = true;
        return { ...addr, refPolicies: uniqueRefs };
      }
      return addr;
    });

    // 2. Recalculate Service Object references
    const updatedServices = services.map((svc: any) => {
      // A service is referenced if its name matches a policy's service list,
      // or if the policy service is formatted as "ServiceName (port)"
      const refs = policies
        .filter((policy: any) => {
          if (!policy.service) return false;
          return policy.service.some((ps: string) => {
            if (ps === svc.name) return true;
            // Matches "ServiceName (anything)"
            const nameEscaped = svc.name.replace(/[-\/\\^$*+?.()|[\]{}]/g, "\\$&");
            const regex = new RegExp(`^${nameEscaped}\\s*\\(.*\\)$`);
            return regex.test(ps);
          });
        })
        .map((policy: any) => policy.name);

      const uniqueRefs = Array.from(new Set(refs)) as string[];

      const currentRefs = svc.refPolicies || [];
      const isSame =
        currentRefs.length === uniqueRefs.length &&
        currentRefs.every((val: string) => uniqueRefs.includes(val));

      if (!isSame) {
        servicesChanged = true;
        return { ...svc, refPolicies: uniqueRefs };
      }
      return svc;
    });

    if (addressesChanged) {
      localStorage.setItem(ADDRESSES_KEY, JSON.stringify(updatedAddresses));
    }
    if (servicesChanged) {
      localStorage.setItem(SERVICES_KEY, JSON.stringify(updatedServices));
    }
  } catch (e) {
    console.error("Error synchronizing mock references:", e);
  }
}

/**
 * Propagates an Address Object rename to all Firewall Policies referencing the old name.
 */
export function propagateAddressRename(oldName: string, newName: string) {
  if (typeof window === "undefined" || !oldName || !newName || oldName === newName) return;

  const policiesStr = localStorage.getItem(POLICIES_KEY);
  if (!policiesStr) return;

  try {
    const policies = JSON.parse(policiesStr);
    let policiesChanged = false;

    const updatedPolicies = policies.map((policy: any) => {
      let policyChanged = false;
      let newSource = policy.source || [];
      let newDest = policy.destination || [];

      if (newSource.includes(oldName)) {
        newSource = newSource.map((s: string) => (s === oldName ? newName : s));
        policyChanged = true;
      }

      if (newDest.includes(oldName)) {
        newDest = newDest.map((d: string) => (d === oldName ? newName : d));
        policyChanged = true;
      }

      if (policyChanged) {
        policiesChanged = true;
        return { ...policy, source: newSource, destination: newDest };
      }
      return policy;
    });

    if (policiesChanged) {
      localStorage.setItem(POLICIES_KEY, JSON.stringify(updatedPolicies));
    }
  } catch (e) {
    console.error("Error propagating address rename in mock policies:", e);
  }
}

/**
 * Propagates a Service Object rename to all Firewall Policies referencing the old name.
 */
export function propagateServiceRename(oldName: string, _oldPort: string, _oldProto: string, newName: string, newPort: string, newProto: string) {
  if (typeof window === "undefined" || !oldName || !newName) return;

  const policiesStr = localStorage.getItem(POLICIES_KEY);
  if (!policiesStr) return;

  try {
    const policies = JSON.parse(policiesStr);
    let policiesChanged = false;

    // Helper to check if a policy service matches the old service name or old formatted service name
    const matchAndReplace = (ps: string) => {
      if (ps === oldName) return newName;

      // Match "oldName (port)" or "oldName (proto port)"
      const oldNameEscaped = oldName.replace(/[-\/\\^$*+?.()|[\]{}]/g, "\\$&");
      const regex = new RegExp(`^${oldNameEscaped}\\s*\\((.*)\\)$`);
      if (regex.test(ps)) {
        // Replace with new name and new port
        if (newProto === "ICMP") {
          return `${newName} (ICMP)`;
        }
        return `${newName} (${newPort})`;
      }
      return ps;
    };

    const updatedPolicies = policies.map((policy: any) => {
      let policyChanged = false;
      let newService = policy.service || [];

      const mappedService = newService.map((ps: string) => {
        const replaced = matchAndReplace(ps);
        if (replaced !== ps) {
          policyChanged = true;
        }
        return replaced;
      });

      if (policyChanged) {
        policiesChanged = true;
        return { ...policy, service: mappedService };
      }
      return policy;
    });

    if (policiesChanged) {
      localStorage.setItem(POLICIES_KEY, JSON.stringify(updatedPolicies));
    }
  } catch (e) {
    console.error("Error propagating service rename in mock policies:", e);
  }
}
