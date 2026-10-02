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

export type AccountKind = "merchant" | "creator";

export type Organization = {
  id: string;
  name: string;
  slug: string;
  kyc_status: "pending" | "submitted" | "verified" | "rejected";
  account_kind: AccountKind;
  business_name?: string;
  tin?: string;
  display_name?: string;
  handle?: string;
  bio?: string;
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

export async function createOrg(input: { name: string; business_name?: string; account_kind?: AccountKind; display_name?: string; handle?: string; bio?: string }) {
  return (await request<OrganizationWithRole>("/api/v1/orgs", { method: "POST", body: input })).data;
}

// Server-set kind endpoints: the kind comes from the URL, never from a
// client-supplied field. New UI uses these; the shared POST /api/v1/orgs
// stays for one deploy cycle so stale clients keep working.
export async function createMerchantOrg(input: { name: string; business_name?: string }) {
  return (await request<OrganizationWithRole>("/api/v1/orgs/merchant", { method: "POST", body: input })).data;
}

export async function createCreatorOrg(input: { name: string; display_name: string; handle: string; bio?: string }) {
  return (await request<OrganizationWithRole>("/api/v1/orgs/creator", { method: "POST", body: input })).data;
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
  display_name?: string;
  handle?: string;
  bio?: string;
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
  full_name?: string;
  id_type?: string;
  id_number?: string;
  dob?: string;
  id_document_url: string;
  id_document_back_url?: string;
  selfie_url?: string;
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

export type LimitSource = "org_override" | "platform" | "platform_creator";

export type OrgAppUsage = {
  app_id: string;
  name: string;
  fee_type: string;
  fee_percent: string;
  fee_fixed: string;
  max_txn: string;
  max_txn_source: LimitSource;
  daily_cap: string;
  daily_cap_source: LimitSource;
  today_volume: Record<string, string>;
};

export type OrgLimitsUsage = {
  max_txn: string;
  max_txn_source: LimitSource;
  daily_cap: string;
  daily_cap_source: LimitSource;
  apps: OrgAppUsage[];
};

export async function getLimitsUsage(orgId: string) {
  return (await request<OrgLimitsUsage>(`/api/v1/merchant/orgs/${orgId}/limits-usage`)).data;
}

// ---------- Creator payout destinations (OTP-gated, 24h cooling) ----------

export type PayoutDestination = {
  id: string;
  org_id: string;
  provider: string;
  phone: string;
  account_name: string;
  name_match: "unavailable" | "matched" | "mismatched";
  name_match_detail?: string;
  otp_verified_at?: string;
  effective_at: string;
  created_at: string;
  updated_at: string;
};

export async function getPayoutDestination(orgId: string): Promise<PayoutDestination | null> {
  return (await request<PayoutDestination | null>(`/api/v1/merchant/orgs/${orgId}/payout-destination`)).data;
}

export async function savePayoutDestination(
  orgId: string,
  input: { provider: string; phone: string; account_name: string; otp_channel: string; otp_code: string },
) {
  return (
    await request<PayoutDestination>(`/api/v1/merchant/orgs/${orgId}/payout-destination`, { method: "POST", body: input })
  ).data;
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

// ---------- Creator support page (public) ----------

export type SupportPageLink = {
  id: string;
  label: string;
  amount_mode: "fixed" | "open";
  amount?: string;
};

export type SupportPage = {
  display_name: string;
  handle: string;
  bio?: string;
  logo_url?: string;
  primary_color?: string;
  category: string;
  currency: string;
  environment: "live" | "sandbox";
  enabled: boolean;
  min_amount: string;
  max_amount: string;
  links: SupportPageLink[];
  providers: string[];
};

export type SupportOrder = {
  id: string;
  provider: string;
  provider_order_id?: string;
  amount: string;
  currency: string;
  status: string;
  created_at: string;
};

export async function getSupportPage(handle: string): Promise<SupportPage> {
  const response = await fetch(`${apiBaseUrl}/api/v1/c/${encodeURIComponent(handle)}`, {
    headers: { Accept: "application/json" },
  });
  if (!response.ok) {
    throw new OrgApiError(response.status, "Creator not found.", "not_found");
  }
  const payload = (await response.json()) as { data: SupportPage };
  return payload.data;
}

export async function createSupportOrder(
  handle: string,
  input: {
    link_id?: string;
    amount: string;
    currency?: string;
    provider: string;
    buyer_name: string;
    buyer_email: string;
    buyer_phone: string;
    supporter_message?: string;
  },
): Promise<SupportOrder> {
  const response = await fetch(`${apiBaseUrl}/api/v1/c/${encodeURIComponent(handle)}/support`, {
    method: "POST",
    headers: { Accept: "application/json", "Content-Type": "application/json" },
    body: JSON.stringify(input),
  });
  const payload = (await response.json().catch(() => null)) as {
    data?: SupportOrder;
    error?: { code?: string; message?: string; details?: Record<string, string> };
  } | null;
  if (!response.ok || !payload?.data) {
    throw new OrgApiError(
      response.status,
      payload?.error?.message || "Unable to create the support payment.",
      payload?.error?.code,
      payload?.error?.details,
    );
  }
  return payload.data;
}

// ---------- Creator public profile (legacy alias — use SupportPage) ----------

export type CreatorPublicProfile = {
  display_name: string;
  handle: string;
  bio?: string;
  logo_url?: string;
  primary_color?: string;
};

export async function getCreatorPublic(handle: string): Promise<CreatorPublicProfile> {
  return getSupportPage(handle);
}

// ---------- Creator support settings (merchant management) ----------

export type SupportSettingsLink = {
  id: string;
  org_id: string;
  label: string;
  amount_mode: "fixed" | "open";
  amount?: string;
  sort_order: number;
  active: boolean;
  created_at: string;
};

export type SupportSettings = {
  org_id: string;
  support_app_id?: string;
  min_amount?: string;
  max_amount?: string;
  show_supporters_wall: boolean;
  links: SupportSettingsLink[];
  created_at: string;
  updated_at: string;
};

export type SupportSettingsInput = {
  support_app_id?: string;
  min_amount?: string;
  max_amount?: string;
  links: { label: string; amount_mode: "fixed" | "open"; amount?: string }[];
};

export async function getSupportSettings(orgId: string) {
  return (await request<SupportSettings>(`/api/v1/merchant/orgs/${orgId}/support-settings`)).data;
}

export async function updateSupportSettings(orgId: string, input: SupportSettingsInput) {
  return (
    await request<SupportSettings>(`/api/v1/merchant/orgs/${orgId}/support-settings`, { method: "PUT", body: input })
  ).data;
}

export async function enableSupportPage(orgId: string) {
  return (
    await request<SupportSettings>(`/api/v1/merchant/orgs/${orgId}/support-page/enable`, { method: "POST" })
  ).data;
}

// ---------- Creator survey (onboarding answers, editable) ----------

export type CreatorSurvey = {
  org_id: string;
  category: string;
  category_other?: string;
  referral_source: string;
  use_cases: string[];
  expected_volume_band: string;
  expected_txn_band: string;
  suggested_risk_tier: "standard" | "elevated" | "high";
  created_at: string;
  updated_at: string;
};

export type CreatorSurveyInput = {
  display_name?: string;
  category: string;
  category_other?: string;
  referral_source: string;
  use_cases: string[];
  expected_volume_band: string;
  expected_txn_band: string;
};

export async function getCreatorSurvey(orgId: string) {
  return (await request<CreatorSurvey>(`/api/v1/orgs/${orgId}/creator-survey`)).data;
}

export async function saveCreatorSurvey(orgId: string, input: CreatorSurveyInput) {
  return (await request<CreatorSurvey>(`/api/v1/orgs/${orgId}/creator-survey`, { method: "PUT", body: input })).data;
}

// Deprecated: kind switching is disabled server-side (410). Kept one
// deploy cycle so stale callers get the server's message, not a crash.
export async function switchCreatorToMerchant(orgId: string) {
  return (await request<Organization>(`/api/v1/orgs/${orgId}/switch-kind`, { method: "POST" })).data;
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