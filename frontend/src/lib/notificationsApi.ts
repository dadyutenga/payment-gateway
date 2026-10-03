import { getAdminToken, getCustomerToken } from "@/lib/auth";

export type DashboardSpace = "customer" | "admin";
export type AppNotification = {
  id: string;
  event_type: string;
  title: string;
  body: string;
  icon?: string;
  severity: "info" | "success" | "warning" | "alert";
  link_url?: string;
  read_at?: string;
  created_at: string;
  description?: string;
  emoji?: string;
  link?: string;
};
export type NotificationsPage = { items: AppNotification[]; total: number; unread: number; limit: number; offset: number };
export type NotificationPreference = { scope_kind: "org" | "user" | "admin"; scope_id: string; event_type: string; channel: "in_app" | "email" | "sms"; enabled: boolean };

const configuredApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
const defaultApiBaseUrl = import.meta.env.DEV ? "http://localhost:8080" : window.location.origin;
const apiBaseUrl = (configuredApiBaseUrl || defaultApiBaseUrl).replace(/\/$/, "");
function tokenFor(space: DashboardSpace) { return space === "admin" ? getAdminToken() : getCustomerToken(); }
function basePath(space: DashboardSpace) { return space === "admin" ? "/api/v1/admin/notifications" : "/api/v1/notifications"; }

async function request<T>(space: DashboardSpace, suffix = "", options: RequestInit = {}, query?: Record<string, string | undefined>) {
  const token = tokenFor(space);
  if (!token) throw new Error("You need to sign in to continue.");
  const url = new URL(`${apiBaseUrl}${basePath(space)}${suffix}`);
  Object.entries(query ?? {}).forEach(([key, value]) => value && url.searchParams.set(key, value));
  const response = await fetch(url, { ...options, headers: { Accept: "application/json", Authorization: `Bearer ${token}`, ...(options.body ? { "Content-Type": "application/json" } : {}), ...(options.headers ?? {}) } });
  const payload = await response.json().catch(() => null) as { data?: T; meta?: Partial<NotificationsPage>; error?: { message?: string } } | null;
  if (!response.ok) throw new Error(payload?.error?.message || "Unable to load notifications.");
  return payload;
}

function normalize(item: AppNotification): AppNotification {
  return { ...item, body: item.body ?? item.description ?? "", icon: item.icon ?? item.emoji, link_url: item.link_url ?? item.link, description: item.body ?? item.description ?? "", emoji: item.icon ?? item.emoji, link: item.link_url ?? item.link };
}

export async function listNotifications(space: DashboardSpace, options: { limit?: number; offset?: number; unread?: boolean; eventType?: string } = {}): Promise<NotificationsPage> {
  const result = await request<AppNotification[]>(space, "", {}, { limit: String(options.limit ?? 25), offset: String(options.offset ?? 0), unread: options.unread ? "true" : undefined, event_type: options.eventType });
  const items = (result?.data ?? []).map(normalize);
  return { items, total: result?.meta?.total ?? items.length, unread: result?.meta?.unread ?? items.filter((item) => !item.read_at).length, limit: result?.meta?.limit ?? options.limit ?? 25, offset: result?.meta?.offset ?? options.offset ?? 0 };
}
export async function markNotificationRead(space: DashboardSpace, id: string) { return request<{ id: string; read: boolean }>(space, `/${encodeURIComponent(id)}/read`, { method: "POST" }); }
export async function markAllNotificationsRead(space: DashboardSpace) { return request<{ updated: number }>(space, "/read-all", { method: "POST" }); }
export async function listNotificationPreferences(space: DashboardSpace) { const result = await request<NotificationPreference[]>(space, "/preferences"); return result?.data ?? []; }
export async function updateNotificationPreference(space: DashboardSpace, preference: { event_type: string; channel: string; enabled: boolean }) { const result = await request<NotificationPreference[]>(space, "/preferences", { method: "PATCH", body: JSON.stringify(preference) }); return result?.data ?? []; }
export type BroadcastPayload = { title: string; body: string; icon?: string; severity: string; target: Record<string, unknown> };
export async function sendAdminBroadcast(payload: BroadcastPayload) { return request<{ id: string; recipients: number }>("admin", "/broadcast", { method: "POST", body: JSON.stringify(payload) }); }
export async function sendAdminOrgNotification(orgId: string, payload: { title: string; body: string; icon?: string; severity: string }) {
  const token = getAdminToken();
  if (!token) throw new Error("You need to sign in to continue.");
  const response = await fetch(`${apiBaseUrl}/api/v1/admin/orgs/${encodeURIComponent(orgId)}/notifications`, { method: "POST", headers: { Accept: "application/json", "Content-Type": "application/json", Authorization: `Bearer ${token}` }, body: JSON.stringify(payload) });
  const result = await response.json().catch(() => null) as { data?: { org_id: string }; error?: { message?: string } } | null;
  if (!response.ok) throw new Error(result?.error?.message || "Unable to send notification.");
  return result?.data;
}
