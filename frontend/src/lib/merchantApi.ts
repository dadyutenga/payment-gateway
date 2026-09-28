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

export class MerchantApiError extends Error {
  status: number;
  code?: string;
  details?: Record<string, string>;

  constructor(status: number, message: string, code?: string, details?: Record<string, string>) {
    super(message);
    this.name = "MerchantApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

const configuredApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
const defaultApiBaseUrl = import.meta.env.DEV ? "http://localhost:8080" : window.location.origin;
const apiBaseUrl = (configuredApiBaseUrl || defaultApiBaseUrl).replace(/\/$/, "");

function createApiUrl(path: string, query?: Record<string, string | undefined>) {
  const url = new URL(path, `${apiBaseUrl}/`);
  if (query) {
    Object.entries(query).forEach(([key, value]) => {
      if (value) {
        url.searchParams.set(key, value);
      }
    });
  }
  return url;
}

async function request<T>(
  path: string,
  options?: {
    method?: "GET" | "POST" | "PATCH" | "DELETE";
    body?: unknown;
    query?: Record<string, string | undefined>;
  },
): Promise<{ data: T; meta?: { total: number; limit: number; offset: number } }> {
  const token = readAccessToken();
  if (!token) {
    throw new MerchantApiError(401, "You need to sign in to continue.", "unauthorized");
  }
  const headers: Record<string, string> = {
    Accept: "application/json",
    Authorization: `Bearer ${token}`,
  };
  if (options?.body !== undefined) {
    headers["Content-Type"] = "application/json";
  }

  const response = await fetch(createApiUrl(path, options?.query), {
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
    throw new MerchantApiError(
      response.status,
      errorPayload?.error?.message || "The request could not be completed.",
      errorPayload?.error?.code,
      errorPayload?.error?.details,
    );
  }

  const envelope = payload as ApiEnvelope<T>;
  return { data: envelope.data, meta: envelope.meta };
}

// ---------- Types (merchant-visible shapes) ----------

export type MerchantApp = {
  id: string;
  name: string;
  description?: string;
  status: string;
  org_id?: string;
};

export type MerchantWebhookEndpoint = {
  id: string;
  app_id: string;
  url: string;
  event_types: string[];
  status: string;
  secret_version: number;
  created_at: string;
  updated_at: string;
};

export type MerchantWebhookEndpointResult = {
  endpoint: MerchantWebhookEndpoint;
  signing_secret: string;
};

export type MerchantAPIKey = {
  id: string;
  app_id: string;
  prefix: string;
  label?: string;
  status: "active" | "rotating" | "revoked";
  environment: "live" | "sandbox";
  created_at: string;
  expires_at?: string;
  last_used_at?: string;
};

export type MerchantAPIKeyResult = {
  key: MerchantAPIKey;
  api_key: string;
};

export type MerchantDelivery = {
  id: string;
  event_id: string;
  endpoint_id: string;
  endpoint_url: string;
  app_id: string;
  status: string;
  attempt_count: number;
  next_attempt_at: string;
  last_attempt_at?: string;
  last_response_status?: number;
  last_error?: string;
  delivered_at?: string;
  created_at: string;
  event_type: string;
  provider: string;
};

export type MerchantTestSendResult = {
  ok: boolean;
  status_code: number;
  error?: string;
  endpoint_url: string;
  delivered_at: string;
};

// ---------- Apps ----------

export async function listMyApps() {
  return (await request<MerchantApp[]>("/api/v1/merchant/apps")).data;
}

export async function createMerchantApp(input: { org_id: string; name: string; description?: string }) {
  return (
    await request<{ app: MerchantApp; api_key: string }>("/api/v1/merchant/apps", {
      method: "POST",
      body: input,
    })
  ).data;
}

export async function updateMerchantApp(appId: string, input: { name: string; description?: string }) {
  return (
    await request<MerchantApp>(`/api/v1/merchant/apps/${appId}`, {
      method: "PATCH",
      body: input,
    })
  ).data;
}

// ---------- Webhook endpoints (scoped to the path app) ----------

export async function listMerchantEndpoints(appId: string) {
  return (await request<MerchantWebhookEndpoint[]>(`/api/v1/merchant/apps/${appId}/webhook-endpoints`)).data;
}

export async function createMerchantEndpoint(appId: string, input: { url: string; event_types?: string[] }) {
  return (
    await request<MerchantWebhookEndpointResult>(`/api/v1/merchant/apps/${appId}/webhook-endpoints`, {
      method: "POST",
      body: input,
    })
  ).data;
}

export async function updateMerchantEndpoint(
  appId: string,
  endpointId: string,
  input: { url?: string; event_types?: string[]; status?: string },
) {
  return (
    await request<MerchantWebhookEndpoint>(`/api/v1/merchant/apps/${appId}/webhook-endpoints/${endpointId}`, {
      method: "PATCH",
      body: input,
    })
  ).data;
}

export async function deleteMerchantEndpoint(appId: string, endpointId: string) {
  await request<unknown>(`/api/v1/merchant/apps/${appId}/webhook-endpoints/${endpointId}`, { method: "DELETE" });
}

export async function testMerchantEndpoint(appId: string, endpointId: string) {
  return (
    await request<MerchantTestSendResult>(`/api/v1/merchant/apps/${appId}/webhook-endpoints/${endpointId}/test-send`, {
      method: "POST",
    })
  ).data;
}

export async function rotateMerchantEndpointSecret(appId: string, endpointId: string) {
  return (
    await request<MerchantWebhookEndpointResult>(`/api/v1/merchant/apps/${appId}/webhook-endpoints/${endpointId}/rotate-secret`, {
      method: "POST",
    })
  ).data;
}

// ---------- API keys (prefixes only; raw secret returned once) ----------

export async function listMerchantKeys(appId: string) {
  return (await request<MerchantAPIKey[]>(`/api/v1/merchant/apps/${appId}/api-keys`)).data;
}

export async function createMerchantKey(appId: string, input?: { environment?: string; label?: string }) {
  return (
    await request<MerchantAPIKeyResult>(`/api/v1/merchant/apps/${appId}/api-keys`, {
      method: "POST",
      body: input ?? {},
    })
  ).data;
}

export async function updateMerchantKeyLabel(appId: string, keyId: string, label: string) {
  await request<unknown>(`/api/v1/merchant/apps/${appId}/api-keys/${keyId}`, {
    method: "PATCH",
    body: { label },
  });
}

export async function rotateMerchantKey(appId: string, keyId: string) {
  return (
    await request<MerchantAPIKeyResult>(`/api/v1/merchant/apps/${appId}/api-keys/${keyId}/rotate`, {
      method: "POST",
    })
  ).data;
}

export async function revokeMerchantKey(appId: string, keyId: string) {
  await request<unknown>(`/api/v1/merchant/apps/${appId}/api-keys/${keyId}/revoke`, { method: "POST" });
}

// ---------- Payments: balance + orders (scoped to the path app) ----------

export type MerchantOrder = {
  id: string;
  app_id?: string;
  provider: string;
  provider_order_id?: string;
  external_reference?: string;
  amount: string;
  currency: string;
  buyer_name?: string;
  buyer_email?: string;
  buyer_phone?: string;
  status: string;
  created_at: string;
  updated_at: string;
};

export type MerchantBalance = {
  app_id: string;
  currency: string;
  available_balance: string;
  total_revenue: string;
  total_platform_fees: string;
  total_withdrawn: string;
  pending_order_total: string;
};

export async function listMerchantOrders(appId: string, status?: string) {
  const r = await request<{ items: MerchantOrder[]; total: number }>(
    `/api/v1/merchant/apps/${appId}/orders`,
    { query: { status } },
  );
  return Array.isArray((r.data as unknown as { items?: MerchantOrder[] })?.items)
    ? ((r.data as unknown as { items: MerchantOrder[] }).items ?? [])
    : [];
}

export async function getMerchantBalance(appId: string) {
  return (await request<MerchantBalance>(`/api/v1/merchant/apps/${appId}/balance`)).data;
}

// ---------- Withdrawals (scoped to the path app) ----------

export type MerchantWithdrawal = {
  id: string;
  app_id: string;
  amount: string;
  currency: string;
  destination_type: "bank" | "mobile_money";
  destination_details: Record<string, unknown>;
  status: "requested" | "approved" | "processing" | "rejected" | "paid" | "failed";
  requested_by: string;
  approved_by?: string;
  notes?: string;
  provider?: string;
  provider_payout_id?: string;
  provider_status?: string;
  failure_reason?: string;
  dispatched_at?: string;
  created_at: string;
  updated_at: string;
};

export type CreateMerchantWithdrawalInput = {
  amount: string;
  currency: string;
  destination_type: "bank" | "mobile_money";
  destination_details: Record<string, unknown>;
  notes?: string;
};

export async function listMerchantWithdrawals(appId: string) {
  const r = await request<{ items: MerchantWithdrawal[]; total: number }>(
    `/api/v1/merchant/apps/${appId}/withdrawals`,
  );
  return Array.isArray((r.data as unknown as { items?: MerchantWithdrawal[] })?.items)
    ? ((r.data as unknown as { items: MerchantWithdrawal[] }).items ?? [])
    : [];
}

export async function createMerchantWithdrawal(appId: string, input: CreateMerchantWithdrawalInput) {
  return (
    await request<MerchantWithdrawal>(`/api/v1/merchant/apps/${appId}/withdrawals`, {
      method: "POST",
      body: input,
    })
  ).data;
}

export async function approveMerchantWithdrawal(appId: string, withdrawalId: string) {
  return (
    await request<MerchantWithdrawal>(`/api/v1/merchant/apps/${appId}/withdrawals/${withdrawalId}/approve`, {
      method: "POST",
    })
  ).data;
}

export async function rejectMerchantWithdrawal(appId: string, withdrawalId: string) {
  return (
    await request<MerchantWithdrawal>(`/api/v1/merchant/apps/${appId}/withdrawals/${withdrawalId}/reject`, {
      method: "POST",
    })
  ).data;
}

// ---------- Delivery logs ----------

export async function listMerchantDeliveries(appId: string, query?: { status?: string; endpoint_id?: string }) {
  const r = await request<{ items: MerchantDelivery[]; total: number }>(
    `/api/v1/merchant/apps/${appId}/deliveries`,
    { query: { status: query?.status, endpoint_id: query?.endpoint_id } },
  );
  return Array.isArray((r.data as unknown as { items?: MerchantDelivery[] })?.items)
    ? ((r.data as unknown as { items: MerchantDelivery[] }).items ?? [])
    : [];
}

export async function replayMerchantDelivery(appId: string, deliveryId: string) {
  await request<unknown>(`/api/v1/merchant/apps/${appId}/deliveries/${deliveryId}/replay`, { method: "POST" });
}
