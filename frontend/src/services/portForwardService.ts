import { type PortForward, initialPortForwards } from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const LOCAL_STORAGE_KEY = "pigate_port_forwards"

function getLocalPortForwards(): PortForward[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY)
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialPortForwards))
    return initialPortForwards
  }
  try {
    return JSON.parse(stored)
  } catch (e) {
    console.error("Failed to parse local port forwards, resetting:", e)
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialPortForwards))
    return initialPortForwards
  }
}

function saveLocalPortForwards(list: PortForward[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(list))
}

export type PortForwardInput = Omit<PortForward, "id">

// extractError pulls the backend's JSON {"message": ...} error body so the UI
// can surface the specific validation/overlap reason instead of a bare status.
async function extractError(response: Response, fallback: string): Promise<string> {
  try {
    const body = await response.json()
    if (body && typeof body.message === "string" && body.message) {
      return body.message
    }
  } catch {
    // non-JSON body; fall through
  }
  return `${fallback}: ${response.statusText}`
}

export const portForwardService = {
  // Fetch all port-forward entries
  getAll: async (): Promise<PortForward[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300))
      return getLocalPortForwards()
    }

    const response = await fetch(`${API_BASE_URL}/port-forwards`)
    if (!response.ok) {
      throw new Error(`Failed to fetch port forwards: ${response.statusText}`)
    }
    return response.json()
  },

  // Create a new port-forward entry (backend re-applies the firewall)
  create: async (pf: PortForwardInput): Promise<PortForward> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350))
      const current = getLocalPortForwards()
      const newPf: PortForward = { ...pf, id: "pf-" + Math.random().toString(36).substring(2, 9) }
      saveLocalPortForwards([...current, newPf])
      return newPf
    }

    const response = await fetch(`${API_BASE_URL}/port-forwards`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(pf),
    })
    if (!response.ok) {
      throw new Error(await extractError(response, "Failed to create port forward"))
    }
    return response.json()
  },

  // Update an existing port-forward entry (backend re-applies the firewall)
  update: async (id: string, pf: PortForwardInput): Promise<PortForward> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350))
      const current = getLocalPortForwards()
      const target = current.find((r) => r.id === id)
      if (!target) throw new Error(`Port forward with id ${id} not found`)
      const updated: PortForward = { ...target, ...pf }
      saveLocalPortForwards(current.map((r) => (r.id === id ? updated : r)))
      return updated
    }

    const response = await fetch(`${API_BASE_URL}/port-forwards/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(pf),
    })
    if (!response.ok) {
      throw new Error(await extractError(response, "Failed to update port forward"))
    }
    return response.json()
  },

  // Delete a port-forward entry (backend re-applies the firewall)
  delete: async (id: string): Promise<boolean> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200))
      const current = getLocalPortForwards()
      saveLocalPortForwards(current.filter((r) => r.id !== id))
      return true
    }

    const response = await fetch(`${API_BASE_URL}/port-forwards/${id}`, {
      method: "DELETE",
    })
    if (!response.ok) {
      throw new Error(`Failed to delete port forward: ${response.statusText}`)
    }
    return true
  },
}
