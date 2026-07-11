import { IS_MOCK_MODE, API_BASE_URL } from "./config";

// The real session token lives only in an HttpOnly cookie (cookie-only auth) —
// JS can neither read nor store it. LOGGED_IN_KEY is a non-secret UI hint that
// tells the route guards a login has happened; the cookie + GET /auth/session
// remain the real source of truth.
const LOGGED_IN_KEY = "pigate_logged_in";
const ROLE_KEY = "pigate_role";
const USERNAME_KEY = "pigate_username";
// Legacy key from the pre-cookie-only versions; may hold a stale token on
// machines that logged in before this change. Purged on load and on logout.
const LEGACY_SESSION_KEY = "pigate_session";

export type UserRole = "super_admin" | "admin_readonly";

// One-time cleanup: drop any token left in localStorage by an older version so
// it can't linger as garbage or be exfiltrated by XSS after the fact.
if (typeof localStorage !== "undefined") {
  localStorage.removeItem(LEGACY_SESSION_KEY);
}

// Mock accounts, kept in sync with userService's mock seed so role-based UI can
// be exercised without a backend. Only used when IS_MOCK_MODE is true.
const MOCK_ACCOUNTS: Record<string, { password: string; role: UserRole }> = {
  pigate: { password: "pigate", role: "super_admin" },
  viewer: { password: "viewer", role: "admin_readonly" },
};

function storeSession(role: string, username: string, mustChange: boolean) {
  localStorage.setItem(LOGGED_IN_KEY, "true");
  localStorage.setItem(ROLE_KEY, role || "");
  localStorage.setItem(USERNAME_KEY, username || "");
  if (mustChange) {
    localStorage.setItem("pigate_must_change_password", "true");
  } else {
    localStorage.removeItem("pigate_must_change_password");
  }
}

function clearSession() {
  localStorage.removeItem(LOGGED_IN_KEY);
  localStorage.removeItem(ROLE_KEY);
  localStorage.removeItem(USERNAME_KEY);
  localStorage.removeItem("pigate_must_change_password");
  // Also clear the legacy token key so it doesn't survive a logout.
  localStorage.removeItem(LEGACY_SESSION_KEY);
}

export const authService = {
  // Login method. On success the backend sets the HttpOnly session cookie; here
  // we only record the non-secret UI hints (logged-in flag, role, username).
  login: async (username: string, password: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      const acct = MOCK_ACCOUNTS[username];
      if (acct && acct.password === password) {
        // In mock mode the seeded accounts behave as already-initialized.
        storeSession(acct.role, username, false);
        return;
      }
      throw new Error("Invalid username or password. (Try pigate / pigate or viewer / viewer)");
    }

    const response = await fetch(`${API_BASE_URL}/auth/login`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ username, password }),
    });

    if (!response.ok) {
      const errBody = await response.json().catch(() => ({}));
      throw new Error(errBody.message || "Invalid username or password");
    }

    const data = await response.json();
    storeSession(data.role || "", username, !!data.mustChangePassword);
  },

  // Logout method
  logout: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      clearSession();
      return;
    }

    await fetch(`${API_BASE_URL}/auth/logout`, { method: "POST" }).catch(() => {});
    clearSession();
  },

  // Check if authenticated. This only reflects the non-secret logged-in hint;
  // the cookie + GET /auth/session are the real authority (see checkSession).
  isAuthenticated: (): boolean => {
    return localStorage.getItem(LOGGED_IN_KEY) === "true";
  },

  // Get the cached role of the current user (from the last login/checkSession).
  getRole: (): UserRole | null => {
    const role = localStorage.getItem(ROLE_KEY);
    return role === "super_admin" || role === "admin_readonly" ? role : null;
  },

  // Get the cached username of the current user.
  getUsername: (): string | null => {
    return localStorage.getItem(USERNAME_KEY);
  },

  // Verify token validity with backend
  checkSession: async (): Promise<{
    valid: boolean;
    username: string;
    role: UserRole | null;
    mustChangePassword: boolean;
  }> => {
    if (IS_MOCK_MODE) {
      if (localStorage.getItem(LOGGED_IN_KEY) !== "true") {
        return { valid: false, username: "", role: null, mustChangePassword: false };
      }
      await new Promise((resolve) => setTimeout(resolve, 300));
      const mustChange = localStorage.getItem("pigate_must_change_password") === "true";
      const role = authService.getRole();
      const username = localStorage.getItem(USERNAME_KEY) || "pigate";
      return { valid: true, username, role, mustChangePassword: mustChange };
    }

    // No local token gate anymore — the HttpOnly cookie decides. Hit the backend
    // directly and sync the UI hints from the result (401 -> clear + invalid).
    try {
      const response = await fetch(`${API_BASE_URL}/auth/session`, {
        method: "GET",
      });

      if (!response.ok) {
        clearSession();
        return { valid: false, username: "", role: null, mustChangePassword: false };
      }

      const data = await response.json();
      const role: string = data.role || "";
      storeSession(role, data.username || "", !!data.mustChangePassword);
      return {
        valid: true,
        username: data.username || "",
        role: role === "super_admin" || role === "admin_readonly" ? role : null,
        mustChangePassword: !!data.mustChangePassword,
      };
    } catch (err) {
      // In case of network errors, we might want to keep the session
      // but for absolute security or simplicity, we can assume it's valid
      // or return invalid. Let's return valid: false if it's explicitly 401/403,
      // but for standard offline issues we can keep it or fail.
      // Usually, if the backend is down, we don't want to lock out the user,
      // but the requirement says "ตรวจสอบ Session ปัจจุบัน กับ backend" and
      // "ตอนนี้ Frontend เช็คแค่ว่ามี Session แต่ไม่ได้ตรวจสอบความถูกต้องกับ Backend".
      // Let's treat connection issues as invalid or log them.
      console.error("Session check failed:", err);
      return { valid: false, username: "", role: null, mustChangePassword: false };
    }
  },
};
