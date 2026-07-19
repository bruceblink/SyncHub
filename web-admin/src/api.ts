export const api = "/api/v1";
export const tokenKey = "synchub.accessToken";
export const userKey = "synchub.user";
export const refreshTokenKey = "synchub.refreshToken";

export type User = { id: string; email: string; status: string };
export type FileNode = {
  id: string;
  name: string;
  path: string;
  node_type: "file" | "directory";
  size: number;
  version: number;
  updated_at: string;
};
export type FileListResponse = { items: FileNode[]; next_cursor: string };
export type AuthResponse = {
  user: User;
  tokens: { access_token: string; refresh_token: string; expires_in: number };
};

export function storeAuth(data: AuthResponse) {
  localStorage.setItem(tokenKey, data.tokens.access_token);
  localStorage.setItem(refreshTokenKey, data.tokens.refresh_token);
  localStorage.setItem(userKey, JSON.stringify(data.user));
}

export function clearAuth() {
  localStorage.removeItem(tokenKey);
  localStorage.removeItem(refreshTokenKey);
  localStorage.removeItem(userKey);
}
export type UploadSession = { upload_id: string };
export type StorageUsage = {
  file_count: number;
  bytes_used: number;
  quota_bytes: number;
};
export type APIKey = {
  id: string;
  name: string;
  application: "kvideo" | "latestnews";
  key_prefix: string;
  last_used_at: string | null;
  revoked_at: string | null;
  created_at: string;
};
export type Subscription = {
  plan: "free" | "pro";
  status: "active" | "past_due" | "canceled" | "expired";
  currency: string;
  unit_amount: number;
  billing_interval: "month" | "year";
  expires_at: string | null;
  current_period_end: string | null;
  cancel_at_period_end: boolean;
  provider: string | null;
};
export type BillingOverview = {
  subscription: Subscription;
  payment_provider_configured: boolean;
};
export type RequestOptions = {
  token?: string;
  method?: string;
  body?: unknown;
  headers?: Record<string, string>;
};

export function errorMessage(error: unknown) {
  return error instanceof Error ? error.message : "请求失败";
}

export async function request<T>(
  path: string,
  { token, method = "GET", body, headers = {} }: RequestOptions = {},
): Promise<T> {
  const isBinary = body instanceof Blob;
  const response = await fetch(`${api}${path}`, {
    method,
    headers: {
      ...(token ? { Authorization: `Bearer ${token}` } : {}),
      ...headers,
      ...(body && !isBinary ? { "Content-Type": "application/json" } : {}),
    },
    body:
      body === undefined ? undefined : isBinary ? body : JSON.stringify(body),
  });
  const payload: { code?: number; message?: string; data?: T } = await response
    .json()
    .catch(() => ({}));
  if (!response.ok || payload.code !== 0 || payload.data === undefined)
    throw new Error(payload.message || "请求失败");
  return payload.data;
}

export function formatSize(size: number) {
  if (!size) return "--";
  const units = ["B", "KB", "MB", "GB", "TB"];
  const power = Math.min(
    Math.floor(Math.log(size) / Math.log(1024)),
    units.length - 1,
  );
  return `${(size / 1024 ** power).toFixed(power ? 1 : 0)} ${units[power]}`;
}

export function formatDate(value: string | null | undefined, fallback = "--") {
  if (!value) return fallback;
  const date = new Date(value);
  if (!Number.isFinite(date.getTime())) return fallback;
  return new Intl.DateTimeFormat("zh-CN", {
    dateStyle: "medium",
    timeStyle: "short",
  }).format(date);
}
