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

export class AdminApiError extends Error {
  status: number;
  code?: string;
  details?: Record<string, string>;

  constructor(status: number, message: string, code?: string, details?: Record<string, string>) {
    super(message);
    this.name = "AdminApiError";
    this.status = status;
    this.code = code;
    this.details = details;
  }
}

// Set VITE_API_BASE_URL to wherever the backend is deployed. In dev it
// defaults to localhost:8080 (the backend's default port); in a production
// build with nothing set, it falls back to the frontend's own origin,
// which only works if you've set up your own reverse proxy for /api/*.
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

async function getAccessToken() {
  const token = readAccessToken();
  if (!token) {
    throw new AdminApiError(401, "You need to sign in to continue.", "unauthorized");
  }
  return token;
}

async function request<T>(
  path: string,
  options?: {
    method?: "GET" | "POST" | "PATCH" | "DELETE";
    body?: unknown;
    query?: Record<string, string | undefined>;
  },
): Promise<{ data: T; meta?: { total: number; limit: number; offset: number } }> {
  const headers: Record<string, string> = {
    Accept: "application/json",
    Authorization: `Bearer ${await getAccessToken()}`,
  };
  if (options?.body) {
    headers["Content-Type"] = "application/json";
  }

  const response = await fetch(createApiUrl(path, options?.query), {
    method: options?.method ?? "GET",
    headers,
    body: options?.body ? JSON.stringify(options.body) : undefined,
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
    throw new AdminApiError(
      response.status,
      errorPayload?.error?.message || "The request could not be completed.",
      errorPayload?.error?.code,
      errorPayload?.error?.details,
    );
  }

  const envelope = payload as ApiEnvelope<T>;
  return { data: envelope.data, meta: envelope.meta };
}

// ---------- Identity ----------

export async function getAdminMe() {
  return (await request<{ email: string; is_admin: boolean }>("/api/v1/admin/me")).data;
}

// ---------- Payments ----------

export type PaymentApp = {
  id: string;
  name: string;
  description?: string;
  status: string;
  fee_type: "fixed" | "percentage" | "hybrid";
  fee_percent: string;
  fee_fixed: string;
  created_at: string;
  updated_at: string;
};

export type PaymentOrder = {
  id: string;
  app_id?: string;
  provider: string;
  provider_order_id?: string;
  provider_transaction_id?: string;
  external_reference?: string;
  amount: string;
  currency: string;
  buyer_name?: string;
  buyer_email?: string;
  buyer_phone?: string;
  status: string;
  provider_status?: string;
  expires_at?: string;
  created_at: string;
  updated_at: string;
};

export type PaymentEvent = {
  id: string;
  provider: string;
  event_type: string;
  provider_event_id?: string;
  provider_order_id?: string;
  payment_order_id?: string;
  signature_valid: boolean;
  normalized_status?: string;
  received_at: string;
  processed_at?: string;
  duplicate_count: number;
  error?: string;
};

export type PaymentWebhookDelivery = {
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

export type PaymentMetrics = {
  generated_at: string;
  orders_by_status: Record<string, number>;
  deliveries_by_status: Record<string, number>;
  total_events: number;
  invalid_signature_events: number;
  duplicate_callbacks: number;
  unprocessed_events: number;
  event_errors: number;
  reconciliation_candidates: number;
};

type PaymentListResult<T> = { items: T[]; total: number; limit: number; offset: number };

export async function listPaymentApps(limit = 50, offset = 0) {
  const { data } = await request<PaymentListResult<PaymentApp>>("/api/v1/admin/payments/apps", {
    query: { limit: String(limit), offset: String(offset) },
  });
  return { items: data.items ?? [], total: data.total ?? 0 };
}

export function createPaymentApp(name: string, description: string) {
  return request<{ app: PaymentApp; api_key: string }>("/api/v1/admin/payments/apps", {
    method: "POST",
    body: { name, description },
  }).then((r) => r.data);
}

// Revokes any existing active key for the app and issues a new one,
// returned raw exactly once.
export function generatePaymentAppAPIKey(id: string) {
  return request<{ api_key: string }>(`/api/v1/admin/payments/apps/${id}/api-key`, { method: "POST" }).then(
    (r) => r.data,
  );
}

// ---------- Payment providers ----------

export type PaymentProviderAccount = {
  id: string;
  provider: string;
  name: string;
  environment: string;
  base_url?: string;
  is_default: boolean;
  status: string;
  created_at: string;
  updated_at: string;
};

export type CreatePaymentProviderInput = {
  provider: string;
  name: string;
  environment: string;
  base_url?: string;
  credentials: Record<string, string>;
};

export type UpdatePaymentProviderInput = {
  name: string;
  environment: string;
  base_url?: string;
  status: string;
  credentials?: Record<string, string>;
};

export async function listPaymentProviders() {
  return (await request<PaymentProviderAccount[]>("/api/v1/admin/payments/providers")).data;
}

export function createPaymentProvider(input: CreatePaymentProviderInput) {
  return request<PaymentProviderAccount>("/api/v1/admin/payments/providers", { method: "POST", body: input }).then((r) => r.data);
}

export function updatePaymentProvider(id: string, input: UpdatePaymentProviderInput) {
  return request<PaymentProviderAccount>(`/api/v1/admin/payments/providers/${id}`, { method: "PATCH", body: input }).then((r) => r.data);
}

export function deletePaymentProvider(id: string) {
  return request<void>(`/api/v1/admin/payments/providers/${id}`, { method: "DELETE" }).then(() => undefined);
}

export function setDefaultPaymentProvider(id: string) {
  return request<{ updated: boolean }>(`/api/v1/admin/payments/providers/${id}/set-default`, { method: "POST" }).then((r) => r.data);
}

export type PaymentWebhookEndpoint = {
  id: string;
  app_id: string;
  url: string;
  event_types: string[];
  status: string;
  created_at: string;
  updated_at: string;
};

export async function listPaymentWebhookEndpoints(appId?: string) {
  return (await request<PaymentWebhookEndpoint[]>("/api/v1/admin/payments/webhook-endpoints", {
    query: appId ? { app_id: appId } : undefined,
  })).data;
}

export function createPaymentWebhookEndpoint(appId: string, url: string, eventTypes: string[]) {
  return request<{ endpoint: PaymentWebhookEndpoint; signing_secret: string }>("/api/v1/admin/payments/webhook-endpoints", {
    method: "POST",
    body: { app_id: appId, url, event_types: eventTypes },
  }).then((r) => r.data);
}

export function processDeliveries(limit?: number) {
  return request<{ claimed: number; delivered: number; retrying: number; failed: number }>(
    "/api/v1/admin/payments/deliveries/process",
    { method: "POST", query: limit ? { limit: String(limit) } : undefined },
  ).then((r) => r.data);
}

export async function searchPaymentOrders(filter?: Record<string, string | undefined>) {
  const { data } = await request<PaymentListResult<PaymentOrder>>("/api/v1/admin/payments/orders", { query: filter });
  return { items: data.items ?? [], total: data.total ?? 0 };
}

export function refundOrder(orderId: string) {
  return request<PaymentOrder>(`/api/v1/admin/payments/orders/${orderId}/refund`, { method: "POST" }).then((r) => r.data);
}

export async function listPaymentEvents(filter?: Record<string, string | undefined>) {
  const { data } = await request<PaymentListResult<PaymentEvent>>("/api/v1/admin/payments/events", { query: filter });
  return { items: data.items ?? [], total: data.total ?? 0 };
}

export function replayPaymentEvent(eventID: string) {
  return request<{ created_deliveries: number; replayed_deliveries: number; queued_deliveries: number }>(
    `/api/v1/admin/payments/events/${eventID}/replay`,
    { method: "POST" },
  ).then((r) => r.data);
}

export async function listWebhookDeliveries(filter?: Record<string, string | undefined>) {
  const { data } = await request<PaymentListResult<PaymentWebhookDelivery>>("/api/v1/admin/payments/deliveries", {
    query: filter,
  });
  return { items: data.items ?? [], total: data.total ?? 0 };
}

export function replayFailedDelivery(deliveryID: string) {
  return request<{ replayed: boolean }>(`/api/v1/admin/payments/deliveries/${deliveryID}/replay`, {
    method: "POST",
  }).then((r) => r.data);
}

export function reconcilePayments(limit?: number) {
  return request<{ scanned: number; checked: number; updated: number; unchanged: number; failed: number }>(
    "/api/v1/admin/payments/reconcile",
    { query: limit ? { limit: String(limit) } : undefined, method: "POST" },
  ).then((r) => r.data);
}

export async function getPaymentMetrics() {
  return (await request<PaymentMetrics>("/api/v1/admin/payments/metrics")).data;
}

// ---------- Payments ledger, balances, fees, withdrawals ----------

export type CurrencyBalance = {
  currency: string;
  available_balance: string;
  total_revenue: string;
  total_platform_fees: string;
  total_withdrawn: string;
  pending_order_total: string;
  available_balance_seven_days_ago: string;
};

export type AppBalance = {
  app_id: string;
  currency: string;
  available_balance: string;
  total_revenue: string;
  total_platform_fees: string;
  total_withdrawn: string;
  pending_order_total: string;
  available_balance_seven_days_ago: string;
  balances: CurrencyBalance[];
};

export type LedgerEntry = {
  id: string;
  app_id: string;
  payment_order_id?: string;
  withdrawal_id?: string;
  entry_type: "payment_credit" | "platform_fee_debit" | "withdrawal_debit" | "withdrawal_reversal_credit" | "adjustment_credit" | "adjustment_debit" | "refund_debit" | "refund_fee_reversal_credit";
  direction: "credit" | "debit";
  amount: string;
  currency: string;
  description: string;
  created_by?: string;
  created_at: string;
};

export type UpdateAppFeesInput = {
  fee_type: "fixed" | "percentage" | "hybrid";
  fee_percent: string;
  fee_fixed: string;
};

export type WithdrawalStatus = "requested" | "approved" | "processing" | "rejected" | "paid" | "failed";

export type PaymentWithdrawal = {
  id: string;
  app_id: string;
  amount: string;
  currency: string;
  destination_type: "bank" | "mobile_money";
  destination_details: Record<string, unknown>;
  status: WithdrawalStatus;
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

export type CreateWithdrawalInput = {
  app_id: string;
  amount: string;
  currency: string;
  destination_type: "bank" | "mobile_money";
  destination_details: Record<string, unknown>;
  notes?: string;
};

export type AppMember = {
  id: string;
  app_id: string;
  user_id: string;
  email: string;
  full_name?: string;
  added_by: string;
  created_at: string;
};

export async function listAppMembers(appId: string) {
  return (await request<AppMember[]>(`/api/v1/admin/payments/apps/${appId}/members`)).data;
}

export function addAppMember(appId: string, email: string) {
  return request<AppMember>(`/api/v1/admin/payments/apps/${appId}/members`, { method: "POST", body: { email } }).then((r) => r.data);
}

export function removeAppMember(appId: string, userId: string) {
  return request<{ removed: boolean }>(`/api/v1/admin/payments/apps/${appId}/members/${userId}`, { method: "DELETE" }).then((r) => r.data);
}

export async function getAppBalance(appId: string) {
  return (await request<AppBalance>(`/api/v1/admin/payments/apps/${appId}/balance`)).data;
}

export async function listAppLedgerEntries(appId: string, limit = 50, offset = 0) {
  const { data, meta } = await request<LedgerEntry[]>(`/api/v1/admin/payments/apps/${appId}/ledger`, {
    query: { limit: String(limit), offset: String(offset) },
  });
  return { items: data, total: meta?.total ?? data.length };
}

export async function listLedgerEntries(appId?: string, limit = 50, offset = 0) {
  const { data, meta } = await request<LedgerEntry[]>("/api/v1/admin/payments/ledger", {
    query: { app_id: appId, limit: String(limit), offset: String(offset) },
  });
  return { items: data, total: meta?.total ?? data.length };
}

export function updateAppFees(appId: string, input: UpdateAppFeesInput) {
  return request<PaymentApp>(`/api/v1/admin/payments/apps/${appId}/fees`, { method: "PATCH", body: input }).then((r) => r.data);
}

// Soft delete — the app stops accepting orders and drops off admin/merchant
// lists, but its order/ledger/withdrawal history is preserved server-side.
export function deletePaymentApp(appId: string) {
  return request<PaymentApp>(`/api/v1/admin/payments/apps/${appId}`, { method: "DELETE" }).then((r) => r.data);
}

export async function listWithdrawals(filter?: { appId?: string; status?: string }, limit = 50, offset = 0) {
  const { data, meta } = await request<PaymentWithdrawal[]>("/api/v1/admin/payments/withdrawals", {
    query: { app_id: filter?.appId, status: filter?.status, limit: String(limit), offset: String(offset) },
  });
  return { items: data, total: meta?.total ?? data.length };
}

export function createWithdrawal(input: CreateWithdrawalInput) {
  return request<PaymentWithdrawal>("/api/v1/admin/payments/withdrawals", { method: "POST", body: input }).then((r) => r.data);
}

export function approveWithdrawal(id: string) {
  return request<PaymentWithdrawal>(`/api/v1/admin/payments/withdrawals/${id}/approve`, { method: "POST" }).then((r) => r.data);
}

export function rejectWithdrawal(id: string) {
  return request<PaymentWithdrawal>(`/api/v1/admin/payments/withdrawals/${id}/reject`, { method: "POST" }).then((r) => r.data);
}

export function markWithdrawalPaid(id: string) {
  return request<PaymentWithdrawal>(`/api/v1/admin/payments/withdrawals/${id}/mark-paid`, { method: "POST" }).then((r) => r.data);
}

export function markWithdrawalFailed(id: string, notes?: string) {
  return request<PaymentWithdrawal>(`/api/v1/admin/payments/withdrawals/${id}/mark-failed`, { method: "POST", body: { notes: notes ?? "" } }).then((r) => r.data);
}

export function retryWithdrawalPayout(id: string) {
  return request<PaymentWithdrawal>(`/api/v1/admin/payments/withdrawals/${id}/retry-payout`, { method: "POST" }).then((r) => r.data);
}
