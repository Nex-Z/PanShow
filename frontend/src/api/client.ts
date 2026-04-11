import type { APIError, DirectoryPassword, FileEntry, User } from "../types";

const API_BASE = import.meta.env.VITE_API_BASE_URL ?? "";
const AUTH_TOKEN_KEY = "panshow_auth_token";
const ACCESS_TOKEN_KEY = "panshow_access_token";
const ACCESS_TOKEN_HEADER = "X-PanShow-Access-Token";

type RequestOptions = {
  method?: "GET" | "POST" | "PATCH" | "DELETE";
  body?: unknown;
};

export class RequestError extends Error {
  error: APIError;
  status: number;

  constructor(status: number, error: APIError) {
    super(error.message);
    this.status = status;
    this.error = error;
  }
}

async function request<T>(path: string, options: RequestOptions = {}): Promise<T> {
  const token = localStorage.getItem(AUTH_TOKEN_KEY);
  const accessToken = localStorage.getItem(ACCESS_TOKEN_KEY);
  const response = await fetch(`${API_BASE}${path}`, {
    method: options.method ?? "GET",
    credentials: "include",
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...(accessToken ? { [ACCESS_TOKEN_HEADER]: accessToken } : {}),
      ...(options.body ? { "Content-Type": "application/json" } : {})
    },
    body: options.body ? JSON.stringify(options.body) : undefined
  });

  const nextAccessToken = response.headers.get(ACCESS_TOKEN_HEADER);
  if (nextAccessToken) {
    localStorage.setItem(ACCESS_TOKEN_KEY, nextAccessToken);
  }

  const payload = (await response.json().catch(() => ({}))) as { error?: APIError };
  if (!response.ok) {
    throw new RequestError(response.status, payload.error ?? { code: "request_failed", message: "请求失败" });
  }
  return payload as T;
}

const queryPath = (value: string) => encodeURIComponent(value);

export const api = {
  login: async (username: string, password: string) => {
    const result = await request<{ token: string; user: User }>("/api/auth/login", {
      method: "POST",
      body: { username, password }
    });
    localStorage.setItem(AUTH_TOKEN_KEY, result.token);
    return result;
  },
  logout: async () => {
    try {
      return await request<{ ok: boolean }>("/api/auth/logout", { method: "POST" });
    } finally {
      localStorage.removeItem(AUTH_TOKEN_KEY);
    }
  },
  me: () => request<{ user: User }>("/api/auth/me"),
  listFiles: (path: string, includeReadme = false) =>
    request<{ path: string; entries: FileEntry[]; readme?: string }>(
      `/api/files?path=${queryPath(path)}${includeReadme ? "&includeReadme=true" : ""}`
    ),
  fileDetail: (path: string) => request<{ file: FileEntry }>(`/api/files/detail?path=${queryPath(path)}`),
  readme: (path: string) => request<{ path: string; content: string }>(`/api/readme?path=${queryPath(path)}`),
  download: (path: string) => request<{ url: string; expiresIn: number }>(`/api/files/download?path=${queryPath(path)}`),
  preview: (path: string) => request<{ url: string; expiresIn: number }>(`/api/files/preview?path=${queryPath(path)}`),
  refreshFileCache: (path: string) =>
    request<{ ok: boolean }>("/api/files/cache/refresh", { method: "POST", body: { path } }),
  submitDirectoryPassword: (path: string, password: string) =>
    request<{ ok: boolean; path: string }>("/api/access/password", { method: "POST", body: { path, password } }),
  adminStatus: () => request<{ database: boolean; redis: boolean; r2: boolean }>("/api/admin/status"),
  adminConfig: () => request<{ r2Bucket: string; r2RootPrefix: string; corsOrigins?: string[] }>("/api/admin/config"),
  listUsers: () => request<{ users: User[] }>("/api/admin/users"),
  createUser: (username: string, password: string, role: string) =>
    request<{ user: User }>("/api/admin/users", { method: "POST", body: { username, password, role } }),
  updateUser: (id: number, body: Partial<Pick<User, "active" | "role">> & { password?: string }) =>
    request<{ user: User }>(`/api/admin/users/${id}`, { method: "PATCH", body }),
  listDirectoryPasswords: () =>
    request<{ directoryPasswords: DirectoryPassword[] }>("/api/admin/directory-passwords"),
  createDirectoryPassword: (path: string, password: string, enabled: boolean) =>
    request<{ directoryPassword: DirectoryPassword }>("/api/admin/directory-passwords", {
      method: "POST",
      body: { path, password, enabled }
    }),
  updateDirectoryPassword: (id: number, body: { path?: string; password?: string; enabled?: boolean }) =>
    request<{ directoryPassword: DirectoryPassword }>(`/api/admin/directory-passwords/${id}`, {
      method: "PATCH",
      body
    }),
  disableDirectoryPassword: (id: number) =>
    request<{ ok: boolean }>(`/api/admin/directory-passwords/${id}`, { method: "DELETE" })
};
