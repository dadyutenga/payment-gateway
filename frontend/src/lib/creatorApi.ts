import { getCustomerToken } from "@/lib/auth";
import type {
  MerchantAnalyticsQuery,
  MerchantOverviewData,
  CustomerStats,
} from "@/lib/merchantAnalyticsApi";

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

export class CreatorApiError extends Error {
  status: number;
  code?: string;
  details?: Record<string, string>;

  constructor(status: number, message: string, code?: string, details?: Record<string, string>) {
    super(message);
    this.name = "CreatorApiError";
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
  const token = getCustomerToken();
  if (!token) {
    throw new CreatorApiError(401, "You need to sign in to continue.", "unauthorized");
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
    throw new CreatorApiError(
      response.status,
      errorPayload?.error?.message || "The request could not be completed.",
      errorPayload?.error?.code,
      errorPayload?.error?.details,
    );
  }

  const envelope = payload as ApiEnvelope<T>;
  return { data: envelope.data, meta: envelope.meta };
}

// ---------- Types (creator-visible shapes; same ledger core) ----------

export type CreatorApp = {
  id: string;
  name: string;
  description?: string;
  status: string;
  org_id?: string;
};

export type CreatorOrder = {
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
  metadata?: Record<string, unknown>;
  status: string;
  created_at: string;
  updated_at: string;
};

export type CreatorBalance = {
  app_id: string;
  currency: string;
  available_balance: string;
  total_revenue: string;
  total_platform_fees: string;
  total_withdrawn: string;
  pending_order_total: string;
};

export type CreatorWithdrawal = {
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

export type CreateCreatorWithdrawalInput = {
  amount: string;
  currency: string;
  destination_type: "bank" | "mobile_money";
  destination_details: Record<string, unknown>;
  notes?: string;
};

// ---------- Receiving app ----------

export async function listCreatorApps() {
  return (await request<CreatorApp[]>("/api/v1/creator/apps")).data;
}

// ---------- Support payments: balance + orders (scoped to the path app) ----------

export async function listCreatorOrders(appId: string, status?: string) {
  const r = await request<{ items: CreatorOrder[]; total: number }>(
    `/api/v1/creator/apps/${appId}/orders`,
    { query: { status } },
  );
  return Array.isArray((r.data as unknown as { items?: CreatorOrder[] })?.items)
    ? ((r.data as unknown as { items: CreatorOrder[] }).items ?? [])
    : [];
}

export async function getCreatorBalance(appId: string) {
  return (await request<CreatorBalance>(`/api/v1/creator/apps/${appId}/balance`)).data;
}

// ---------- Payouts (scoped to the path app) ----------

function withdrawalItems(r: { data: unknown }): CreatorWithdrawal[] {
  const items = (r.data as unknown as { items?: CreatorWithdrawal[] })?.items;
  return Array.isArray(items) ? (items ?? []) : [];
}

export async function listCreatorWithdrawals(appId: string) {
  return withdrawalItems(await request(`/api/v1/creator/apps/${appId}/withdrawals`));
}

export async function createCreatorWithdrawal(appId: string, input: CreateCreatorWithdrawalInput) {
  return (
    await request<CreatorWithdrawal>(`/api/v1/creator/apps/${appId}/withdrawals`, {
      method: "POST",
      body: input,
    })
  ).data;
}

export async function approveCreatorWithdrawal(appId: string, withdrawalId: string) {
  return (
    await request<CreatorWithdrawal>(`/api/v1/creator/apps/${appId}/withdrawals/${withdrawalId}/approve`, {
      method: "POST",
    })
  ).data;
}

export async function rejectCreatorWithdrawal(appId: string, withdrawalId: string) {
  return (
    await request<CreatorWithdrawal>(`/api/v1/creator/apps/${appId}/withdrawals/${withdrawalId}/reject`, {
      method: "POST",
    })
  ).data;
}

// ---------- Creator analytics: overview + supporters (same rollup core) ----------

async function getAnalytics<T>(path: string, q?: MerchantAnalyticsQuery): Promise<T> {
  const query: Record<string, string | undefined> = {};
  if (q) {
    for (const [k, v] of Object.entries(q)) {
      if (v !== undefined && v !== "") query[k] = String(v);
    }
  }
  return (await request<T>(path, { query })).data;
}

export const fetchCreatorOverview = (orgId: string, q?: MerchantAnalyticsQuery) =>
  getAnalytics<MerchantOverviewData>(`/api/v1/creator/orgs/${orgId}/analytics/overview`, q);

export const fetchCreatorSupporters = (orgId: string, q?: MerchantAnalyticsQuery) =>
  getAnalytics<CustomerStats>(`/api/v1/creator/orgs/${orgId}/analytics/supporters`, q);
