import { IS_MOCK_MODE, API_BASE_URL } from "./config";

const SESSION_KEY = "pigate_session";

export const authService = {
  // Login method
  login: async (username: string, password: string): Promise<string> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 500));
      if (username === "admin" && password === "admin") {
        const token = "mock_session_id_" + Math.random().toString(36).substring(2, 9);
        localStorage.setItem(SESSION_KEY, token);
        return token;
      }
      throw new Error("Invalid username or password. (Use admin / admin)");
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
    return token;
  },

  // Logout method
  logout: async (): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 200));
      localStorage.removeItem(SESSION_KEY);
      return;
    }

    await fetch(`${API_BASE_URL}/auth/logout`, { method: "POST" }).catch(() => {});
    localStorage.removeItem(SESSION_KEY);
  },

  // Check if authenticated
  isAuthenticated: (): boolean => {
    return !!localStorage.getItem(SESSION_KEY);
  },

  // Get current session token
  getToken: (): string | null => {
    return localStorage.getItem(SESSION_KEY);
  },
};
