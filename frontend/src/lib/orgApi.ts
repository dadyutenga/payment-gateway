import { getCustomerToken } from "@/lib/auth";

type ApiEnvelope<T> = {
  data: T;
  meta?: { total: number; limit: number; offset: number };
};

type ApiErrorEnvelope = {
  error?: {
    code?: string;
    message?: string;
    details?: Record<string, string>;
  };
};

export class OrgApiError extends Error {
  status: number;
  code?: string;
  details?: Record<string, string>;

  constructor(status: number, message: string, code?: string, details?: Record<string, string>) {
    super(message);
    this.name = "OrgApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

const configuredApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
const defaultApiBaseUrl = import.meta.env.DEV ? "http://localhost:8080" : window.location.origin;
const apiBaseUrl = (configuredApiBaseUrl || defaultApiBaseUrl).replace(/\/$/, "");

async function request<T>(
  path: string,
  options?: {
    method?: "GET" | "POST" | "PATCH" | "DELETE";
    body?: unknown;
    formData?: FormData;
  },
): Promise<{ data: T }> {
  const token = getCustomerToken();
  if (!token) {
    throw new OrgApiError(401, "You need to sign in to continue.", "unauthorized");
  }
  const headers: Record<string, string> = {
    Accept: "application/json",
    Authorization: `Bearer ${token}`,
  };
  let body: BodyInit | undefined;
  if (options?.formData) {
    body = options.formData;
  } else if (options?.body !== undefined) {
    headers["Content-Type"] = "application/json";
    body = JSON.stringify(options.body);
  }

  const response = await fetch(`${apiBaseUrl}${path}`, {
    method: options?.method ?? "GET",
    headers,
    body,
  });

  if (response.status === 204) {
    return { data: undefined as T };
  }

  const contentType = response.headers.get("content-type") ?? "";
  const payload = contentType.includes("application/json")
    ? ((await response.json()) as ApiEnvelope<T> | ApiErrorEnvelope)
    : null;

  if (!response.ok) {
    const errorPayload = payload as ApiErrorEnvelope | null;
    throw new OrgApiError(
      response.status,
      errorPayload?.error?.message || "The request could not be completed.",
      errorPayload?.error?.code,
      errorPayload?.error?.details,
    );
  }

  return { data: (payload as ApiEnvelope<T>).data };
}

// ---------- Types ----------

export type OrgRole = "owner" | "finance" | "developer" | "viewer";

export type Organization = {
  id: string;
  name: string;
  slug: string;
  kyc_status: "pending" | "submitted" | "verified" | "rejected";
  business_name?: string;
  tin?: string;
  address?: string;
  phone?: string;
  contact_email?: string;
  logo_url?: string;
  primary_color?: string;
  live_max_txn_amount?: string;
  live_daily_volume_cap?: string;
  created_at: string;
  updated_at: string;
};

export type OrganizationWithRole = Organization & {
  role: OrgRole;
  status: "invited" | "active";
};

export type OrgMember = {
  org_id: string;
  user_id: string;
  email: string;
  full_name?: string;
  phone?: string;
  role: OrgRole;
  invited_by?: string;
  status: "invited" | "active";
  created_at: string;
};

export const ORG_ROLES: { value: OrgRole; label: string; description: string }[] = [
  { value: "owner", label: "Owner", description: "Everything, including members and deleting the org." },
  { value: "finance", label: "Finance", description: "Balances, reports, withdrawals. No webhooks, keys, or members." },
  { value: "developer", label: "Developer", description: "Apps, webhooks, keys, delivery logs. No withdrawals." },
  { value: "viewer", label: "Viewer", description: "Read-only." },
];

// ---------- Organizations ----------

export async function listMyOrgs() {
  return (await request<OrganizationWithRole[]>("/api/v1/orgs")).data;
}

export async function createOrg(input: { name: string; business_name?: string }) {
  return (await request<OrganizationWithRole>("/api/v1/orgs", { method: "POST", body: input })).data;
}

export async function getOrg(orgId: string) {
  return (await request<OrganizationWithRole>(`/api/v1/orgs/${orgId}`)).data;
}

export async function updateOrg(orgId: string, input: {
  name: string;
  business_name?: string;
  tin?: string;
  address?: string;
  phone?: string;
  contact_email?: string;
  logo_url?: string;
  primary_color?: string;
}) {
  return (await request<Organization>(`/api/v1/orgs/${orgId}`, { method: "PATCH", body: input })).data;
}

export async function deleteOrg(orgId: string) {
  await request<unknown>(`/api/v1/orgs/${orgId}`, { method: "DELETE" });
}

// ---------- Members ----------

export async function listOrgMembers(orgId: string) {
  return (await request<OrgMember[]>(`/api/v1/orgs/${orgId}/members`)).data;
}

export async function inviteOrgMember(orgId: string, input: { email: string; role: OrgRole }) {
  return (await request<OrgMember>(`/api/v1/orgs/${orgId}/invites`, { method: "POST", body: input })).data;
}

export async function acceptOrgInvite(orgId: string) {
  return (await request<OrgMember>(`/api/v1/orgs/${orgId}/accept`, { method: "POST" })).data;
}

export async function changeOrgMemberRole(orgId: string, userId: string, role: OrgRole) {
  return (
    await request<OrgMember>(`/api/v1/orgs/${orgId}/members/${userId}`, { method: "PATCH", body: { role } })
  ).data;
}

export async function removeOrgMember(orgId: string, userId: string) {
  await request<unknown>(`/api/v1/orgs/${orgId}/members/${userId}`, { method: "DELETE" });
}

export async function leaveOrg(orgId: string) {
  await request<unknown>(`/api/v1/orgs/${orgId}/leave`, { method: "POST" });
}

// ---------- KYC history (immutable submit/decide trail) ----------

export type KYCAttempt = {
  id: string;
  org_id: string;
  business_name: string;
  tin: string;
  id_document_url: string;
  status: "submitted" | "verified" | "rejected";
  rejection_reason?: string;
  reviewed_by?: string;
  reviewed_at?: string;
  created_at: string;
};

export async function listKYCAttempts(orgId: string) {
  return (await request<KYCAttempt[]>(`/api/v1/orgs/${orgId}/kyc/attempts`)).data;
}

// ---------- Limits usage (effective caps + today's live volume + fees) ----------

export type OrgAppUsage = {
  app_id: string;
  name: string;
  fee_type: string;
  fee_percent: string;
  fee_fixed: string;
  max_txn: string;
  max_txn_source: "org_override" | "platform";
  daily_cap: string;
  daily_cap_source: "org_override" | "platform";
  today_volume: Record<string, string>;
};

export type OrgLimitsUsage = {
  max_txn: string;
  max_txn_source: "org_override" | "platform";
  daily_cap: string;
  daily_cap_source: "org_override" | "platform";
  apps: OrgAppUsage[];
};

export async function getLimitsUsage(orgId: string) {
  return (await request<OrgLimitsUsage>(`/api/v1/merchant/orgs/${orgId}/limits-usage`)).data;
}

// ---------- Notification preferences ----------

export type NotificationPrefs = {
  org_id: string;
  payment_updated: boolean;
  payment_refunded: boolean;
  payment_expired: boolean;
  withdrawal_updates: boolean;
  kyc_decisions: boolean;
};

export async function getNotificationPrefs(orgId: string) {
  return (await request<NotificationPrefs>(`/api/v1/orgs/${orgId}/notification-prefs`)).data;
}

export async function updateNotificationPrefs(orgId: string, input: Omit<NotificationPrefs, "org_id">) {
  return (
    await request<NotificationPrefs>(`/api/v1/orgs/${orgId}/notification-prefs`, { method: "PATCH", body: input })
  ).data;
}

// ---------- Logo upload (multipart) + authenticated serving ----------

export async function uploadOrgLogo(orgId: string, file: File) {
  const formData = new FormData();
  formData.append("logo", file);
  return request<Organization>(`/api/v1/orgs/${orgId}/logo`, { method: "POST", formData });
}

// resolveLogoSrc maps the stored logo location to something an <img> can
// use: external URLs pass through, uploaded paths fetch through the
// authenticated logo endpoint as a blob URL.
export async function resolveLogoSrc(orgId: string, logoUrl?: string): Promise<string> {
  const loc = (logoUrl ?? "").trim();
  if (!loc) return "";
  if (/^https?:\/\//i.test(loc)) return loc;
  const token = getCustomerToken();
  if (!token) throw new OrgApiError(401, "You need to sign in to continue.", "unauthorized");
  const response = await fetch(`${apiBaseUrl}/api/v1/orgs/${orgId}/logo`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) throw new OrgApiError(response.status, "Unable to load the logo.");
  return URL.createObjectURL(await response.blob());
}