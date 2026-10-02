import { getAdminToken } from "@/lib/auth";

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

export class AdminOrgApiError extends Error {
  status: number;
  code?: string;
  details?: Record<string, string>;

  constructor(status: number, message: string, code?: string, details?: Record<string, string>) {
    super(message);
    this.name = "AdminOrgApiError";
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
  const token = getAdminToken();
  if (!token) {
    throw new AdminOrgApiError(401, "You need to sign in to continue.", "unauthorized");
  }
  const headers: Record<string, string> = {
    Accept: "application/json",
    Authorization: `Bearer ${token}`,
  };
  let body: BodyInit | undefined;
  if (options?.body !== undefined) {
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
    throw new AdminOrgApiError(
      response.status,
      errorPayload?.error?.message || "The request could not be completed.",
      errorPayload?.error?.code,
      errorPayload?.error?.details,
    );
  }

  return { data: (payload as ApiEnvelope<T>).data };
}

// ---------- Admin: KYC review queue, decisions, live limits ----------

export type KYCQueueItem = {
  org_id: string;
  org_name: string;
  slug: string;
  kyc_status: string;
  account_kind?: string;
  business_name: string;
  tin: string;
  full_name?: string;
  id_type?: string;
  id_number?: string;
  category?: string;
  dob?: string;
  suggested_risk_tier?: string;
  expected_volume_band?: string;
  expected_txn_band?: string;
  has_document: boolean;
  has_back_document?: boolean;
  has_selfie?: boolean;
  submitted_at: string;
  rejection_reason?: string;
  owner_email?: string;
  owner_name?: string;
  owner_phone?: string;
};

export async function listKYCQueue(status?: string) {
  const query = status ? `?status=${encodeURIComponent(status)}` : "";
  return (await request<KYCQueueItem[]>(`/api/v1/admin/orgs/kyc-queue${query}`)).data;
}

export async function approveKYC(orgId: string) {
  return (await request<Organization>(`/api/v1/admin/orgs/${orgId}/kyc/approve`, { method: "POST" })).data;
}

export async function rejectKYC(orgId: string, reason: string) {
  return (
    await request<Organization>(`/api/v1/admin/orgs/${orgId}/kyc/reject`, { method: "POST", body: { reason } })
  ).data;
}

export async function updateOrgLimits(orgId: string, input: { live_max_txn_amount?: string; live_daily_volume_cap?: string }) {
  return (
    await request<Organization>(`/api/v1/admin/orgs/${orgId}/limits`, { method: "PATCH", body: input })
  ).data;
}

// ---------- Admin: platform stats (home dashboard) ----------

export type PlatformStats = {
  customers: number;
  admins: number;
  organizations: number;
  orgs_by_kyc: Record<string, number>;
  kyc_awaiting_review: number;
  apps: number;
  withdrawals_by_status: Record<string, number>;
};

export async function getPlatformStats() {
  return (await request<PlatformStats>("/api/v1/admin/stats")).data;
}

// fetchKYCDocument downloads an org's ID document as a blob (admin review
// path — the member route requires org membership reviewers don't have).
export async function fetchKYCDocument(orgId: string): Promise<{ blob: Blob; contentType: string }> {
  const token = getAdminToken();
  if (!token) {
    throw new AdminOrgApiError(401, "You need to sign in to continue.", "unauthorized");
  }
  const response = await fetch(`${apiBaseUrl}/api/v1/admin/orgs/${orgId}/kyc/document`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) {
    throw new AdminOrgApiError(response.status, "Unable to load the verification document.");
  }
  const blob = await response.blob();
  return { blob, contentType: response.headers.get("content-type") ?? "application/octet-stream" };
}

// fetchKYCSelfie downloads a creator org's v1 selfie photo as a blob
// (admin review path — reviewers compare it against the ID document).
export async function fetchKYCSelfie(orgId: string): Promise<{ blob: Blob; contentType: string }> {
  const token = getAdminToken();
  if (!token) {
    throw new AdminOrgApiError(401, "You need to sign in to continue.", "unauthorized");
  }
  const response = await fetch(`${apiBaseUrl}/api/v1/admin/orgs/${orgId}/kyc/selfie`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) {
    throw new AdminOrgApiError(response.status, "Unable to load the selfie photo.");
  }
  const blob = await response.blob();
  return { blob, contentType: response.headers.get("content-type") ?? "application/octet-stream" };
}

// ---------- Admin: Org detail (for analytics drill-down) ----------

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

export async function getAdminOrgDetail(orgId: string) {
  return (await request<Organization>(`/api/v1/admin/orgs/${orgId}`)).data;
}

// ---------- Admin: Org suspension/unsuspension ----------

export async function suspendOrg(orgId: string, reason: string) {
  return (await request<Organization>(`/api/v1/admin/orgs/${orgId}/suspend`, { method: "POST", body: { reason } })).data;
}

export async function unsuspendOrg(orgId: string) {
  return (await request<Organization>(`/api/v1/admin/orgs/${orgId}/unsuspend`, { method: "POST" })).data;
}

// ---------- Admin: Audit log ----------

export type AuditEntry = {
  id: string;
  actor_id: string;
  actor_email: string;
  action: string;
  target_type: string;
  target_id: string;
  before: string;
  after: string;
  ip: string;
  created_at: string;
};

export async function listAudit(action?: string, actor?: string, limit = 50, offset = 0) {
  const params = new URLSearchParams();
  if (action) params.set("action", action);
  if (actor) params.set("actor", actor);
  params.set("limit", String(limit));
  params.set("offset", String(offset));
  return (await request<AuditEntry[]>(`/api/v1/admin/audit-log?${params.toString()}`)).data;
}