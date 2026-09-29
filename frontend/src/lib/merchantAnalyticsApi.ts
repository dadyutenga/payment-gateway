import { getAccessToken as readAccessToken } from "@/lib/auth";

export class MerchantAnalyticsApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "MerchantAnalyticsApiError";
    this.status = status;
    this.code = code;
  }
}

const configuredApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
const defaultApiBaseUrl = import.meta.env.DEV ? "http://localhost:8080" : window.location.origin;
const apiBaseUrl = (configuredApiBaseUrl || defaultApiBaseUrl).replace(/\/$/, "");

export type MerchantAnalyticsQuery = {
  from?: string;
  to?: string;
  granularity?: "hour" | "day" | "week" | "month";
  provider?: string;
  currency?: string;
  app_id?: string;
  environment?: "live" | "sandbox";
};

function toQueryString(q?: MerchantAnalyticsQuery): string {
  if (!q) return "";
  const params = new URLSearchParams();
  Object.entries(q).forEach(([k, v]) => {
    if (v !== undefined && v !== "") params.set(k, String(v));
  });
  const s = params.toString();
  return s ? `?${s}` : "";
}

async function get<T>(path: string, q?: MerchantAnalyticsQuery): Promise<T> {
  const token = readAccessToken();
  if (!token) throw new MerchantAnalyticsApiError(401, "You need to sign in to continue.", "unauthorized");
  const response = await fetch(`${apiBaseUrl}${path}${toQueryString(q)}`, {
    headers: { Accept: "application/json", Authorization: `Bearer ${token}` },
  });
  const payload = await response.json().catch(() => null) as { data?: T; error?: { code?: string; message?: string } } | null;
  if (!response.ok) {
    throw new MerchantAnalyticsApiError(response.status, payload?.error?.message || "The request could not be completed.", payload?.error?.code);
  }
  return (payload as { data: T }).data;
}

export async function downloadSettlement(orgId: string, format: "csv" | "pdf", q?: MerchantAnalyticsQuery & { currency?: string; limit?: number }) {
  const token = readAccessToken();
  if (!token) throw new MerchantAnalyticsApiError(401, "You need to sign in to continue.", "unauthorized");
  const extra = toQueryString({ ...(q ?? {}), } as MerchantAnalyticsQuery).replace("?", "&");
  const currency = q?.currency ? `&currency=${encodeURIComponent(q.currency)}` : "";
  const limit = q?.limit ? `&limit=${q.limit}` : "";
  const response = await fetch(`${apiBaseUrl}/api/v1/orgs/${orgId}/settlements?format=${format}${extra}${currency}${limit}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) {
    const payload = await response.json().catch(() => null) as { error?: { message?: string } } | null;
    throw new MerchantAnalyticsApiError(response.status, payload?.error?.message || "Unable to build statement.");
  }
  const blob = await response.blob();
  const filename = (response.headers.get("content-disposition")?.match(/filename="([^"]+)"/) ?? [])[1]
    ?? `statement.${format}`;
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = filename;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

// ---------- Types ----------

export type MoneyByCurrency = Record<string, string>;

export type MerchantOverviewData = {
  timezone: string;
  tpv: MoneyByCurrency;
  revenue: MoneyByCurrency;
  tx_count: number;
  success_rate: number | null;
  abandonment_rate: number | null;
  median_ttp_s: number | null;
  p90_ttp_s: number | null;
  previous_period: { tpv_pct_change?: number; revenue_pct_change?: number; tx_pct_change?: number };
  series: { bucket: string; tx_count: number; success_rate: number | null; gross_by_currency: MoneyByCurrency }[];
  avg_order_value: MoneyByCurrency;
};

export type MethodRow = {
  channel: string;
  tx_count: number;
  volume_by_currency: MoneyByCurrency;
  success_rate: number | null;
};

export type PeakHoursData = {
  timezone: string;
  cells: { dow: number; hour: number; tx_count: number; paid_count: number }[];
  best_label: string;
  worst_label: string;
};

export type CustomerStats = {
  timezone: string;
  repeat_rate: number | null;
  payers_total: number;
  payers_repeat: number;
  series: { bucket: string; new_payers: number; returning_payers: number }[];
  top_payers: { masked_id: string; tx_count: number; volume: string; currency: string; last_txn_at: string }[];
  payer_detail_hidden: boolean;
};

export type OrgFailureRow = { code: string; provider: string; count: number; affected_orgs: number; sample_order_ids: string[] };

export type OrgAppRow = {
  app_id: string; name: string; status: string;
  tpv_by_currency: MoneyByCurrency; revenue_by_currency: MoneyByCurrency;
  tx_count: number; success_rate: number | null; refund_rate: number | null;
};

export type SettlementData = {
  statement_id: string;
  org_id: string;
  org_name: string;
  business_name: string;
  from: string;
  to: string;
  generated_at: string;
  timezone: string;
  truncated: boolean;
  blocks: {
    currency: string; gross: string; fees: string; refunds: string;
    fee_reversals: string; net_settled: string;
    opening_balance: string; closing_balance: string;
    by_provider: { provider: string; gross: string; fees: string; count: number }[];
    entries: {
      id: string; entry_type: string; direction: string; amount: string;
      currency: string; order_id?: string; description: string; created_at: string;
    }[];
  }[];
};

// ---------- Fetchers (org-scoped) ----------

const orgBase = (orgId: string) => `/api/v1/orgs/${orgId}`;

export const fetchMerchantOverview = (orgId: string, q?: MerchantAnalyticsQuery) =>
  get<MerchantOverviewData>(`${orgBase(orgId)}/analytics/overview`, q);
export const fetchMerchantMethods = (orgId: string, q?: MerchantAnalyticsQuery) =>
  get<MethodRow[]>(`${orgBase(orgId)}/analytics/methods`, q);
export const fetchMerchantPeakHours = (orgId: string, q?: MerchantAnalyticsQuery) =>
  get<PeakHoursData>(`${orgBase(orgId)}/analytics/peak-hours`, q);
export const fetchMerchantCustomers = (orgId: string, q?: MerchantAnalyticsQuery) =>
  get<CustomerStats>(`${orgBase(orgId)}/analytics/customers`, q);
export const fetchMerchantFailures = (orgId: string, q?: MerchantAnalyticsQuery) =>
  get<OrgFailureRow[]>(`${orgBase(orgId)}/analytics/failures`, q);
export const fetchMerchantApps = (orgId: string, q?: MerchantAnalyticsQuery) =>
  get<OrgAppRow[]>(`${orgBase(orgId)}/analytics/apps`, q);
export const fetchSettlement = (orgId: string, q?: MerchantAnalyticsQuery & { currency?: string; limit?: number }) =>
  get<SettlementData>(`${orgBase(orgId)}/settlements`, q);
