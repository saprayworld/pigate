import { IS_MOCK_MODE, API_BASE_URL } from "./config";

const SESSION_KEY = "pigate_session";
const ROLE_KEY = "pigate_role";
const USERNAME_KEY = "pigate_username";

export type UserRole = "super_admin" | "admin_readonly";

// Mock accounts, kept in sync with userService's mock seed so role-based UI can
// be exercised without a backend. Only used when IS_MOCK_MODE is true.
const MOCK_ACCOUNTS: Record<string, { password: string; role: UserRole }> = {
  pigate: { password: "pigate", role: "super_admin" },
  viewer: { password: "viewer", role: "admin_readonly" },
};

function storeSession(token: string, role: string, username: string, mustChange: boolean) {
  localStorage.setItem(SESSION_KEY, token);
  localStorage.setItem(ROLE_KEY, role || "");
  localStorage.setItem(USERNAME_KEY, username || "");
  if (mustChange) {
    localStorage.setItem("pigate_must_change_password", "true");
  } else {
    localStorage.removeItem("pigate_must_change_password");
  }
}

function clearSession() {
  localStorage.removeItem(SESSION_KEY);
  localStorage.removeItem(ROLE_KEY);
  localStorage.removeItem(USERNAME_KEY);
  localStorage.removeItem("pigate_must_change_password");
}

export const authService = {
  // Login method
  login: async (username: string, password: string): Promise<string> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      const acct = MOCK_ACCOUNTS[username];
      if (acct && acct.password === password) {
        const token = "mock_session_id_" + Math.random().toString(36).substring(2, 9);
        // In mock mode the seeded accounts behave as already-initialized.
        storeSession(token, acct.role, username, false);
        return token;
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
    storeSession(data.token, data.role || "", username, !!data.mustChangePassword);
    return data.token;
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

  // Check if authenticated
  isAuthenticated: (): boolean => {
    return !!localStorage.getItem(SESSION_KEY);
  },

  // Get current session token
  getToken: (): string | null => {
    return localStorage.getItem(SESSION_KEY);
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
    const token = localStorage.getItem(SESSION_KEY);
    if (!token) {
      return { valid: false, username: "", role: null, mustChangePassword: false };
    }

    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const mustChange = localStorage.getItem("pigate_must_change_password") === "true";
      const role = authService.getRole();
      const username = localStorage.getItem(USERNAME_KEY) || "pigate";
      return { valid: true, username, role, mustChangePassword: mustChange };
    }

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
      storeSession(token, role, data.username || "", !!data.mustChangePassword);
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
