import { IS_MOCK_MODE, API_BASE_URL } from "./config";

const SESSION_KEY = "pigate_session";

export const authService = {
  // Login method
  login: async (username: string, password: string): Promise<string> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      if (username === "pigate" && password === "pigate") {
        const token = "mock_session_id_" + Math.random().toString(36).substring(2, 9);
        localStorage.setItem(SESSION_KEY, token);
        localStorage.setItem("pigate_must_change_password", "true");
        return token;
      }
      throw new Error("Invalid username or password. (Use pigate / pigate)");
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
    const token = data.token;
    localStorage.setItem(SESSION_KEY, token);
    if (data.mustChangePassword) {
      localStorage.setItem("pigate_must_change_password", "true");
    } else {
      localStorage.removeItem("pigate_must_change_password");
    }
    return token;
  },

  // Logout method
  logout: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      localStorage.removeItem(SESSION_KEY);
      localStorage.removeItem("pigate_must_change_password");
      return;
    }

    await fetch(`${API_BASE_URL}/auth/logout`, { method: "POST" }).catch(() => {});
    localStorage.removeItem(SESSION_KEY);
    localStorage.removeItem("pigate_must_change_password");
  },

  // Check if authenticated
  isAuthenticated: (): boolean => {
    return !!localStorage.getItem(SESSION_KEY);
  },

  // Get current session token
  getToken: (): string | null => {
    return localStorage.getItem(SESSION_KEY);
  },

  // Verify token validity with backend
  checkSession: async (): Promise<{ valid: boolean; username: string; mustChangePassword: boolean }> => {
    const token = localStorage.getItem(SESSION_KEY);
    if (!token) {
      return { valid: false, username: "", mustChangePassword: false };
    }

    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const mustChange = localStorage.getItem("pigate_must_change_password") === "true";
      return { valid: true, username: "pigate", mustChangePassword: mustChange };
    }

    try {
      const response = await fetch(`${API_BASE_URL}/auth/session`, {
        method: "GET",
      });

      if (!response.ok) {
        localStorage.removeItem(SESSION_KEY);
        localStorage.removeItem("pigate_must_change_password");
        return { valid: false, username: "", mustChangePassword: false };
      }

      const data = await response.json();
      if (data.mustChangePassword) {
        localStorage.setItem("pigate_must_change_password", "true");
      } else {
        localStorage.removeItem("pigate_must_change_password");
      }
      return {
        valid: true,
        username: data.username || "pigate",
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
      return { valid: false, username: "", mustChangePassword: false };
    }
  },
};
