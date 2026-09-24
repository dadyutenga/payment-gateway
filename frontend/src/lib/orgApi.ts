import { getAccessToken as readAccessToken } from "@/lib/auth";

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
  },
): Promise<{ data: T }> {
  const token = readAccessToken();
  if (!token) {
    throw new OrgApiError(401, "You need to sign in to continue.", "unauthorized");
  }
  const headers: Record<string, string> = {
    Accept: "application/json",
    Authorization: `Bearer ${token}`,
  };
  if (options?.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }

  const response = await fetch(`${apiBaseUrl}${path}`, {
    method: options?.method ?? "GET",
    headers,
    body: options?.body !== undefined ? JSON.stringify(options.body) : undefined,
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

export async function updateOrg(orgId: string, input: { name: string; business_name?: string }) {
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
