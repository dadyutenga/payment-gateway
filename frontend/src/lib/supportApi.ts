import { getAdminToken, getCustomerToken } from "@/lib/auth";

export type SupportTrack = "merchant" | "individual";
export type SupportTicket = {
  id: string; org_id: string; org_name?: string; created_by: string; created_by_email?: string;
  subject: string; category: string; priority: "low" | "normal" | "high" | "urgent";
  status: "open" | "pending" | "in_progress" | "resolved" | "closed";
  assigned_admin_id?: string; assigned_email?: string; created_at: string; updated_at: string;
  resolved_at?: string; sla_breach: boolean; messages?: SupportMessage[]; linked_record?: LinkedRecord;
};
export type SupportMessage = { id: string; author_type: "merchant" | "admin"; author_id: string; author_email?: string; body: string; attachments?: SupportAttachment[]; internal_note?: boolean; created_at: string };
export type SupportAttachment = { name: string; content_type: string; size: number };
export type LinkedRecord = { id: string; kind: "order" | "withdrawal"; status: string; amount?: string; currency?: string; created_at: string };
export type SupportList = { items: SupportTicket[]; total: number };

const apiBase = (import.meta.env.VITE_API_BASE_URL?.trim() || (import.meta.env.DEV ? "http://localhost:8080" : window.location.origin)).replace(/\/$/, "");

function customerBase(track: SupportTrack, orgId?: string) {
  return track === "individual" ? "/api/v1/individual/account/support/tickets" : `/api/v1/merchant/orgs/${encodeURIComponent(orgId ?? "")}/support/tickets`;
}
async function request<T>(path: string, token: string | null, options: RequestInit = {}, query?: Record<string, string | undefined>) {
  if (!token) throw new Error("You need to sign in to continue.");
  const url = new URL(`${apiBase}${path}`);
  Object.entries(query ?? {}).forEach(([key, value]) => value && url.searchParams.set(key, value));
  const response = await fetch(url, { ...options, headers: { Accept: "application/json", Authorization: `Bearer ${token}`, ...(options.body && !(options.body instanceof FormData) ? { "Content-Type": "application/json" } : {}), ...(options.headers ?? {}) } });
  const payload = await response.json().catch(() => null) as { data?: T; meta?: { total?: number }; error?: { message?: string } } | null;
  if (!response.ok) throw new Error(payload?.error?.message || "Unable to complete support request.");
  return payload;
}

export async function listSupportTickets(track: SupportTrack, orgId: string | undefined, query: Record<string, string | undefined> = {}) {
  const result = await request<SupportTicket[]>(customerBase(track, orgId), getCustomerToken(), {}, query);
  return { items: result?.data ?? [], total: result?.meta?.total ?? 0 };
}
export async function createSupportTicket(track: SupportTrack, orgId: string | undefined, input: { subject: string; category: string; description: string; linked_order_id?: string; linked_withdrawal_id?: string }) {
  const result = await request<SupportTicket>(customerBase(track, orgId), getCustomerToken(), { method: "POST", body: JSON.stringify(input) });
  return result?.data;
}
export async function getSupportTicket(track: SupportTrack, orgId: string | undefined, ticketId: string) {
  const result = await request<SupportTicket>(`${customerBase(track, orgId)}/${encodeURIComponent(ticketId)}`, getCustomerToken());
  return result?.data;
}
export async function replyToSupportTicket(track: SupportTrack, orgId: string | undefined, ticketId: string, body: string) {
  const result = await request<SupportTicket>(`${customerBase(track, orgId)}/${encodeURIComponent(ticketId)}/messages`, getCustomerToken(), { method: "POST", body: JSON.stringify({ body }) });
  return result?.data;
}
export async function closeSupportTicket(track: SupportTrack, orgId: string | undefined, ticketId: string) {
  const result = await request<SupportTicket>(`${customerBase(track, orgId)}/${encodeURIComponent(ticketId)}/close`, getCustomerToken(), { method: "POST" });
  return result?.data;
}
export async function uploadSupportAttachment(track: SupportTrack, orgId: string | undefined, ticketId: string, file: File, body?: string) {
  const form = new FormData(); form.append("file", file); if (body) form.append("body", body);
  const result = await request<SupportTicket>(`${customerBase(track, orgId)}/${encodeURIComponent(ticketId)}/attachments`, getCustomerToken(), { method: "POST", body: form });
  return result?.data;
}
async function downloadBlob(path: string, token: string | null, filename: string) {
  if (!token) throw new Error("You need to sign in to continue.");
  const response = await fetch(`${apiBase}${path}`, { headers: { Authorization: `Bearer ${token}` } });
  if (!response.ok) throw new Error("Unable to download attachment.");
  const url = URL.createObjectURL(await response.blob());
  const link = document.createElement("a");
  link.href = url; link.download = filename; document.body.appendChild(link); link.click(); link.remove();
  URL.revokeObjectURL(url);
}
export function downloadSupportAttachment(track: SupportTrack, orgId: string | undefined, ticketId: string, messageId: string, filename: string) {
  return downloadBlob(`${customerBase(track, orgId)}/${encodeURIComponent(ticketId)}/messages/${encodeURIComponent(messageId)}/attachment`, getCustomerToken(), filename);
}
export function downloadAdminSupportAttachment(ticketId: string, messageId: string, filename: string) {
  return downloadBlob(`/api/v1/admin/support/tickets/${encodeURIComponent(ticketId)}/messages/${encodeURIComponent(messageId)}/attachment`, getAdminToken(), filename);
}
export async function listAdminSupportTickets(query: Record<string, string | undefined> = {}) {
  const result = await request<SupportTicket[]>("/api/v1/admin/support/tickets", getAdminToken(), {}, query);
  return { items: result?.data ?? [], total: result?.meta?.total ?? 0 };
}
export async function getAdminSupportTicket(ticketId: string) { const result = await request<SupportTicket>(`/api/v1/admin/support/tickets/${encodeURIComponent(ticketId)}`, getAdminToken()); return result?.data; }
export async function replyToAdminSupportTicket(ticketId: string, body: string, internal_note: boolean) { const result = await request<SupportTicket>(`/api/v1/admin/support/tickets/${encodeURIComponent(ticketId)}/messages`, getAdminToken(), { method: "POST", body: JSON.stringify({ body, internal_note }) }); return result?.data; }
export async function updateAdminSupportTicket(ticketId: string, input: { assigned_admin_id?: string; priority?: string; status?: string }) { const result = await request<SupportTicket>(`/api/v1/admin/support/tickets/${encodeURIComponent(ticketId)}`, getAdminToken(), { method: "PATCH", body: JSON.stringify(input) }); return result?.data; }
export async function uploadAdminSupportAttachment(ticketId: string, file: File) { const form = new FormData(); form.append("file", file); const result = await request<SupportTicket>(`/api/v1/admin/support/tickets/${encodeURIComponent(ticketId)}/attachments`, getAdminToken(), { method: "POST", body: form }); return result?.data; }
