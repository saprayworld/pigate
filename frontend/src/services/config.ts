// Global Configuration for PiGate Frontend Services
// Handles switching between local storage mocking and real backend server.

// IS_MOCK_MODE is enabled if:
// 1. VITE_USE_MOCK env variable is "true"
// 2. OR localstorage has "PIGATE_DEV_MODE" set to "mock"
// 3. OR fallback is true for local offline preview/testing.
export const IS_MOCK_MODE = false;

export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "/api";

// Automatically hook the global window.fetch to attach the Authorization header if available
if (typeof window !== "undefined" && !(window as any).__pigate_fetch_hooked__) {
  const originalFetch = window.fetch;
  window.fetch = async (input, init) => {
    let url = "";
    if (typeof input === "string") {
      url = input;
    } else if (input instanceof URL) {
      url = input.href;
    } else if (input) {
      url = (input as any).url || "";
    }

    if (url.includes("/api/")) {
      const token = localStorage.getItem("pigate_session");
      if (token) {
        const headers = new Headers(init?.headers);
        if (!headers.has("Authorization")) {
          headers.set("Authorization", `Bearer ${token}`);
        }
        init = { ...init, headers };
      }
    }
    const response = await originalFetch(input, init);

    // Automatically handle session expiration/invalidation
    if (url.includes("/api/") && response.status === 401) {
      if (!url.includes("/auth/session") && !url.includes("/auth/login")) {
        localStorage.removeItem("pigate_session");
        localStorage.removeItem("pigate_must_change_password");
        window.location.href = "/login";
      }
    }

    return response;
  };
  (window as any).__pigate_fetch_hooked__ = true;
}
