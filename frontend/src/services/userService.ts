import { IS_MOCK_MODE, API_BASE_URL } from "./config";

export type UserRole = "super_admin" | "admin_readonly";
export type UserStatus = "active" | "disabled";

export interface UserAccount {
  id: string;
  username: string;
  isInitial: boolean;
  role: UserRole;
  status: UserStatus;
  createdAt: string;
}

export interface CreateUserPayload {
  username: string;
  password: string;
  role: UserRole;
}

export interface UpdateUserPayload {
  role: UserRole;
  // Optional: when provided, resets the user's password (forces change on next login).
  password?: string;
}

const LOCAL_STORAGE_KEY = "pigate_users";

// Mock seed kept in sync with authService's MOCK_ACCOUNTS so the two behave
// consistently when IS_MOCK_MODE is on.
const initialMockUsers: UserAccount[] = [
  {
    id: "user-pigate",
    username: "pigate",
    isInitial: false,
    role: "super_admin",
    status: "active",
    createdAt: new Date("2024-01-01T00:00:00Z").toISOString(),
  },
  {
    id: "user-viewer",
    username: "viewer",
    isInitial: false,
    role: "admin_readonly",
    status: "active",
    createdAt: new Date("2024-01-02T00:00:00Z").toISOString(),
  },
];

function getLocalUsers(): UserAccount[] {
  const stored = localStorage.getItem(LOCAL_STORAGE_KEY);
  if (!stored) {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialMockUsers));
    return initialMockUsers;
  }
  try {
    return JSON.parse(stored);
  } catch {
    localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(initialMockUsers));
    return initialMockUsers;
  }
}

function saveLocalUsers(users: UserAccount[]) {
  localStorage.setItem(LOCAL_STORAGE_KEY, JSON.stringify(users));
}

async function parseError(response: Response, fallback: string): Promise<never> {
  const errBody = await response.json().catch(() => ({}));
  throw new Error(errBody.message || fallback);
}

export const userService = {
  getAll: async (): Promise<UserAccount[]> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      return getLocalUsers();
    }
    const response = await fetch(`${API_BASE_URL}/users`);
    if (!response.ok) {
      return parseError(response, "Failed to fetch users");
    }
    return response.json();
  },

  create: async (payload: CreateUserPayload): Promise<UserAccount> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const users = getLocalUsers();
      if (users.some((u) => u.username === payload.username)) {
        throw new Error(`ชื่อผู้ใช้ "${payload.username}" ถูกใช้งานแล้ว`);
      }
      const newUser: UserAccount = {
        id: "user-" + Math.random().toString(36).substring(2, 12),
        username: payload.username,
        isInitial: true,
        role: payload.role,
        status: "active",
        createdAt: new Date().toISOString(),
      };
      saveLocalUsers([...users, newUser]);
      return newUser;
    }
    const response = await fetch(`${API_BASE_URL}/users`, {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!response.ok) {
      return parseError(response, "Failed to create user");
    }
    return response.json();
  },

  update: async (id: string, payload: UpdateUserPayload): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const users = getLocalUsers();
      const idx = users.findIndex((u) => u.id === id);
      if (idx === -1) throw new Error("ไม่พบผู้ใช้ที่ระบุ");
      users[idx].role = payload.role;
      if (payload.password) {
        users[idx].isInitial = true;
      }
      saveLocalUsers(users);
      return;
    }
    const response = await fetch(`${API_BASE_URL}/users/${id}`, {
      method: "PUT",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(payload),
    });
    if (!response.ok) {
      await parseError(response, "Failed to update user");
    }
  },

  remove: async (id: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      saveLocalUsers(getLocalUsers().filter((u) => u.id !== id));
      return;
    }
    const response = await fetch(`${API_BASE_URL}/users/${id}`, { method: "DELETE" });
    if (!response.ok) {
      await parseError(response, "Failed to delete user");
    }
  },

  toggle: async (id: string): Promise<void> => {
    if (IS_MOCK_MODE) {
      await new Promise((resolve) => setTimeout(resolve, 300));
      const users = getLocalUsers();
      const idx = users.findIndex((u) => u.id === id);
      if (idx === -1) throw new Error("ไม่พบผู้ใช้ที่ระบุ");
      users[idx].status = users[idx].status === "active" ? "disabled" : "active";
      saveLocalUsers(users);
      return;
    }
    const response = await fetch(`${API_BASE_URL}/users/${id}/toggle`, { method: "POST" });
    if (!response.ok) {
      await parseError(response, "Failed to toggle user status");
    }
  },
};
