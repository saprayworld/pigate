import {
  type NetworkInterface,
  type WifiPreset,
  type WifiPresetMacMode,
  type WifiPresetSecurity,
  initialWifiPresets,
} from "@/data-mockup/mockData"
import { IS_MOCK_MODE, API_BASE_URL } from "./config"

const PRESETS_LOCAL_STORAGE_KEY = "pigate_wifi_presets"
// Same key interfaceService.ts uses — the mock apply flow updates a preset's
// target interface in place, mirroring what the real /apply endpoint does
// server-side against the DB.
const INTERFACES_LOCAL_STORAGE_KEY = "pigate_interfaces"

// Input payload for create/update. Mirrors backend WifiPresetInput: password is
// optional and write-only — never present on anything read back from getAll().
export interface WifiPresetInput {
  name: string
  ssid: string
  security: WifiPresetSecurity
  password?: string
  macMode?: WifiPresetMacMode
}

// Internal mock-only shape that also carries the plaintext password. This never
// leaves this module — every public mock function strips it back down to
// WifiPreset (hasPassword only) before returning, exactly like the real
// backend's model.SanitizeWifiPresetForRead.
interface StoredWifiPreset extends WifiPresetInput {
  id: string
}

function getLocalPresets(): StoredWifiPreset[] {
  const stored = localStorage.getItem(PRESETS_LOCAL_STORAGE_KEY)
  if (!stored) {
    const seeded: StoredWifiPreset[] = initialWifiPresets.map((p) => ({
      id: p.id,
      name: p.name,
      ssid: p.ssid,
      security: p.security,
      macMode: p.macMode || "",
      // Seed data never carries real plaintext passwords; hasPassword drives
      // whether the mock treats the preset as "having" a password.
      password: p.hasPassword ? "mock-password" : "",
    }))
    localStorage.setItem(PRESETS_LOCAL_STORAGE_KEY, JSON.stringify(seeded))
    return seeded
  }
  try {
    return JSON.parse(stored)
  } catch (e) {
    console.error("Failed to parse local wifi presets, resetting to mock data:", e)
    localStorage.removeItem(PRESETS_LOCAL_STORAGE_KEY)
    return getLocalPresets()
  }
}

function saveLocalPresets(presets: StoredWifiPreset[]) {
  localStorage.setItem(PRESETS_LOCAL_STORAGE_KEY, JSON.stringify(presets))
}

function toPublic(p: StoredWifiPreset): WifiPreset {
  return {
    id: p.id,
    name: p.name,
    ssid: p.ssid,
    security: p.security,
    macMode: p.macMode,
    hasPassword: !!p.password,
  }
}

function getLocalInterfaces(): NetworkInterface[] {
  const stored = localStorage.getItem(INTERFACES_LOCAL_STORAGE_KEY)
  if (!stored) return []
  try {
    return JSON.parse(stored)
  } catch (e) {
    console.error("Failed to parse local network interfaces:", e)
    return []
  }
}

function saveLocalInterfaces(interfaces: NetworkInterface[]) {
  localStorage.setItem(INTERFACES_LOCAL_STORAGE_KEY, JSON.stringify(interfaces))
}

// Extracts a human-readable message from a JSON error body ({"message": "..."})
// the backend always sends via writeError, falling back to the HTTP status text.
async function extractErrorMessage(response: Response, fallback: string): Promise<string> {
  try {
    const body = await response.json()
    if (body?.message) return body.message
  } catch {
    // response has no JSON body; keep the fallback
  }
  return fallback
}

export const wifiPresetService = {
  // List all saved Wi-Fi presets. Never carries a password — only hasPassword.
  getAll: async (): Promise<WifiPreset[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300))
      return getLocalPresets().map(toPublic)
    }

    const response = await fetch(`${API_BASE_URL}/wifi-presets`)
    if (!response.ok) {
      throw new Error(
        await extractErrorMessage(response, `Failed to fetch wifi presets: ${response.statusText}`)
      )
    }
    return response.json()
  },

  // Create a new saved Wi-Fi network.
  create: async (input: WifiPresetInput): Promise<WifiPreset> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350))
      const current = getLocalPresets()
      const name = input.name.trim()
      if (!name) {
        throw new Error("Preset name must not be empty")
      }
      if (current.some((p) => p.name.toLowerCase() === name.toLowerCase())) {
        throw new Error("a wifi preset with this name already exists")
      }
      const created: StoredWifiPreset = {
        id: "wifi-preset-" + Math.random().toString(36).substring(2, 9),
        name,
        ssid: input.ssid,
        security: input.security,
        macMode: input.macMode || "",
        password: input.password || "",
      }
      saveLocalPresets([...current, created])
      return toPublic(created)
    }

    const response = await fetch(`${API_BASE_URL}/wifi-presets`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
    })
    if (!response.ok) {
      throw new Error(
        await extractErrorMessage(response, `Failed to create wifi preset: ${response.statusText}`)
      )
    }
    return response.json()
  },

  // Update an existing preset. An empty password keeps the currently stored one.
  update: async (id: string, input: WifiPresetInput): Promise<WifiPreset> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 350))
      const current = getLocalPresets()
      const targetIndex = current.findIndex((p) => p.id === id)
      if (targetIndex === -1) {
        throw new Error("Wi-Fi preset not found")
      }
      const target = current[targetIndex]
      const name = input.name.trim()
      if (!name) {
        throw new Error("Preset name must not be empty")
      }
      if (current.some((p) => p.id !== id && p.name.toLowerCase() === name.toLowerCase())) {
        throw new Error("a wifi preset with this name already exists")
      }
      const updated: StoredWifiPreset = {
        ...target,
        name,
        ssid: input.ssid,
        security: input.security,
        macMode: input.macMode || "",
        // Empty submitted password = keep the currently stored credential.
        password: input.password ? input.password : target.password,
      }
      current[targetIndex] = updated
      saveLocalPresets(current)
      return toPublic(updated)
    }

    const response = await fetch(`${API_BASE_URL}/wifi-presets/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(input),
    })
    if (!response.ok) {
      throw new Error(
        await extractErrorMessage(response, `Failed to update wifi preset: ${response.statusText}`)
      )
    }
    return response.json()
  },

  // Delete a saved preset. Never touches interfaces that previously applied it
  // (a preset is a one-way template, not a live link).
  remove: async (id: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 250))
      const current = getLocalPresets()
      if (!current.some((p) => p.id === id)) {
        throw new Error("Wi-Fi preset not found")
      }
      saveLocalPresets(current.filter((p) => p.id !== id))
      return
    }

    const response = await fetch(`${API_BASE_URL}/wifi-presets/${id}`, {
      method: "DELETE",
    })
    if (!response.ok) {
      throw new Error(
        await extractErrorMessage(response, `Failed to delete wifi preset: ${response.statusText}`)
      )
    }
  },

  // Server-side apply: fills the preset's ssid/security/macMode (+ password,
  // which never travels through the browser) into the target interface's
  // primary or backup Wi-Fi slot, then returns the updated interface with
  // passwords masked. The request body only ever carries {interfaceId, slot}.
  apply: async (
    presetId: string,
    params: { interfaceId: string; slot: "primary" | "backup" }
  ): Promise<NetworkInterface> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 400))
      const preset = getLocalPresets().find((p) => p.id === presetId)
      if (!preset) {
        throw new Error("Wi-Fi preset not found")
      }
      if (params.slot !== "primary" && params.slot !== "backup") {
        throw new Error("invalid slot: must be \"primary\" or \"backup\"")
      }
      const interfaces = getLocalInterfaces()
      const targetIndex = interfaces.findIndex((i) => i.id === params.interfaceId)
      if (targetIndex === -1) {
        throw new Error("interface not found")
      }
      const target = interfaces[targetIndex]
      if (target.type !== "wireless") {
        throw new Error("target interface is not wireless")
      }

      // Mock never surfaces the preset's real password — same masked
      // placeholder the real backend response uses after maskInterfacePasswords.
      const maskedPassword = preset.password ? "••••••••" : ""
      const updated: NetworkInterface = { ...target }
      if (params.slot === "primary") {
        updated.wifiSSID = preset.ssid
        updated.wifiSecurity = preset.security
        updated.wifiPassword = maskedPassword
        if (preset.macMode) {
          updated.macMode = preset.macMode as NetworkInterface["macMode"]
        }
      } else {
        updated.backupSsid = preset.ssid
        updated.backupWifiSecurity = preset.security
        updated.backupWifiPassword = maskedPassword
      }

      interfaces[targetIndex] = updated
      saveLocalInterfaces(interfaces)
      return updated
    }

    const response = await fetch(`${API_BASE_URL}/wifi-presets/${presetId}/apply`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(params),
    })
    if (!response.ok) {
      throw new Error(
        await extractErrorMessage(response, `Failed to apply wifi preset: ${response.statusText}`)
      )
    }
    return response.json()
  },
}
