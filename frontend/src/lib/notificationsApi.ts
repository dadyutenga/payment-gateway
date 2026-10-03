import { getAdminToken, getCustomerToken } from "@/lib/auth";

export type DashboardSpace = "customer" | "admin";

export type AppNotification = {
  id: string;
  title: string;
  description: string;
  emoji?: string;
  link?: string;
  read_at?: string;
  created_at: string;
};

const configuredApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
const defaultApiBaseUrl = import.meta.env.DEV ? "http://localhost:8080" : window.location.origin;
const apiBaseUrl = (configuredApiBaseUrl || defaultApiBaseUrl).replace(/\/$/, "");

function tokenFor(space: DashboardSpace) {
  return space === "admin" ? getAdminToken() : getCustomerToken();
}

function pathFor(space: DashboardSpace) {
  return space === "admin" ? "/api/v1/admin/auth/notifications" : "/api/v1/auth/notifications";
}

async function request<T>(space: DashboardSpace, path = "") {
  const token = tokenFor(space);
  if (!token) throw new Error("You need to sign in to continue.");
  const response = await fetch(`${apiBaseUrl}${pathFor(space)}${path}`, {
    headers: { Accept: "application/json", Authorization: `Bearer ${token}` },
    method: path ? "PATCH" : "GET",
  });
  const payload = await response.json().catch(() => null) as { data?: T; error?: { message?: string } } | null;
  if (!response.ok) throw new Error(payload?.error?.message || "Unable to load notifications.");
  return payload?.data as T;
}

export function listNotifications(space: DashboardSpace) {
  return request<AppNotification[]>(space);
}

export function markAllNotificationsRead(space: DashboardSpace) {
  return request<{ updated: number }>(space, "/read-all");
}
