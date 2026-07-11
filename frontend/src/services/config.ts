// Global Configuration for PiGate Frontend Services
// Handles switching between local storage mocking and real backend server.

// IS_MOCK_MODE is enabled if:
// 1. VITE_USE_MOCK env variable is "true"
// 2. OR localstorage has "PIGATE_DEV_MODE" set to "mock"
// 3. OR fallback is true for local offline preview/testing.
export const IS_MOCK_MODE = false;

export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "/api";

type PiGateWindow = Window & { __pigate_fetch_hooked__?: boolean };

// Automatically hook the global window.fetch so every /api/ request carries the
// HttpOnly session cookie. The session token lives only in that cookie now
// (cookie-only auth) — no Authorization: Bearer header is sent. `credentials:
// "include"` is required for the dev cross-origin case (localhost:5173 ->
// localhost:8081); production is same-origin and would send the cookie anyway.
if (typeof window !== "undefined" && !(window as PiGateWindow).__pigate_fetch_hooked__) {
  const originalFetch = window.fetch;
  window.fetch = async (input, init) => {
    let url = "";
    if (typeof input === "string") {
      url = input;
    } else if (input instanceof URL) {
      url = input.href;
    } else if (input) {
      url = input.url || "";
    }

    if (url.includes("/api/")) {
      init = { ...init, credentials: "include" };
    }
    const response = await originalFetch(input, init);

    // Automatically handle session expiration/invalidation. The cookie is the
    // source of truth; here we just clear the JS-side UI hints and bounce to
    // /login when a protected call comes back 401.
    if (url.includes("/api/") && response.status === 401) {
      if (!url.includes("/auth/session") && !url.includes("/auth/login")) {
        localStorage.removeItem("pigate_logged_in");
        localStorage.removeItem("pigate_role");
        localStorage.removeItem("pigate_username");
        localStorage.removeItem("pigate_must_change_password");
        window.location.href = "/login";
      }
    }

    return response;
  };
  (window as PiGateWindow).__pigate_fetch_hooked__ = true;
}
