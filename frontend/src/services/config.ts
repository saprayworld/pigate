// Global Configuration for PiGate Frontend Services
// Handles switching between local storage mocking and real backend server.

// IS_MOCK_MODE is enabled if:
// 1. VITE_USE_MOCK env variable is "true"
// 2. OR localstorage has "PIGATE_DEV_MODE" set to "mock"
// 3. OR fallback is true for local offline preview/testing.
export const IS_MOCK_MODE =
  import.meta.env.VITE_USE_MOCK === "true" ||
  localStorage.getItem("PIGATE_DEV_MODE") === "mock" ||
  true;

export const API_BASE_URL = import.meta.env.VITE_API_BASE_URL || "/api";
