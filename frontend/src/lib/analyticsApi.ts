import { getAdminToken } from "@/lib/auth";

export class AnalyticsApiError extends Error {
  status: number;
  code?: string;
  constructor(status: number, message: string, code?: string) {
    super(message);
    this.name = "AnalyticsApiError";
    this.status = status;
    this.code = code;
  }
}

const configuredApiBaseUrl = import.meta.env.VITE_API_BASE_URL?.trim();
const defaultApiBaseUrl = import.meta.env.DEV ? "http://localhost:8080" : window.location.origin;
const apiBaseUrl = (configuredApiBaseUrl || defaultApiBaseUrl).replace(/\/$/, "");

export type AnalyticsQuery = {
  from?: string;
  to?: string;
  granularity?: "hour" | "day" | "week" | "month";
  provider?: string;
  currency?: string;
  org_id?: string;
  sort?: string;
  page?: number;
  per_page?: number;
  dormant_days?: number;
  churn_drop_pct?: number;
  stuck_minutes?: number;
};

function toQueryString(q?: AnalyticsQuery): string {
  if (!q) return "";
  const params = new URLSearchParams();
  Object.entries(q).forEach(([k, v]) => {
    if (v !== undefined && v !== "") params.set(k, String(v));
  });
  const s = params.toString();
  return s ? `?${s}` : "";
}

async function get<T>(path: string, q?: AnalyticsQuery): Promise<T> {
  const token = getAdminToken();
  if (!token) throw new AnalyticsApiError(401, "You need to sign in to continue.", "unauthorized");
  const response = await fetch(`${apiBaseUrl}${path}${toQueryString(q)}`, {
    headers: { Accept: "application/json", Authorization: `Bearer ${token}` },
  });
  const payload = await response.json().catch(() => null) as { data?: T; error?: { code?: string; message?: string } } | null;
  if (!response.ok) {
    throw new AnalyticsApiError(response.status, payload?.error?.message || "The request could not be completed.", payload?.error?.code);
  }
  return (payload as { data: T }).data;
}

export async function downloadReportCSV(report: string, q?: AnalyticsQuery) {
  const token = getAdminToken();
  if (!token) throw new AnalyticsApiError(401, "You need to sign in to continue.", "unauthorized");
  const response = await fetch(`${apiBaseUrl}/api/v1/admin/analytics/export/${report}?format=csv${toQueryString(q).replace("?", "&")}`, {
    headers: { Authorization: `Bearer ${token}` },
  });
  if (!response.ok) throw new AnalyticsApiError(response.status, "Unable to build export.");
  const blob = await response.blob();
  const url = URL.createObjectURL(blob);
  const a = document.createElement("a");
  a.href = url;
  a.download = `lipago-${report}.csv`;
  document.body.appendChild(a);
  a.click();
  a.remove();
  setTimeout(() => URL.revokeObjectURL(url), 30_000);
}

// ---------- Types (mirror backend/internal/modules/analytics/model.go) ----------

export type MoneyByCurrency = Record<string, string>;

export type OverviewData = {
  timezone: string;
  from: string;
  to: string;
  tpv: MoneyByCurrency;
  revenue: MoneyByCurrency;
  tx_count: number;
  success_rate: number | null;
  abandonment_rate: number | null;
  median_ttp_s: number | null;
  p90_ttp_s: number | null;
  active_merchants: number;
  new_signups: number;
  new_orgs: number;
  previous_period: { tpv_pct_change?: number; revenue_pct_change?: number; tx_pct_change?: number };
  series: { bucket: string; tx_count: number; success_rate: number | null; gross_by_currency: MoneyByCurrency }[];
};

export type ProviderStat = {
  provider: string;
  volume_by_currency: MoneyByCurrency;
  tx_count: number;
  success_rate: number | null;
  failures_by_code: Record<string, number>;
  median_latency_ms: number | null;
  p95_latency_ms: number | null;
  failures_1h: number;
  failures_24h: number;
  last_failure_at?: string;
  revenue_by_currency: MoneyByCurrency;
  channel_split: Record<string, number>;
};

export type MerchantRow = {
  org_id: string;
  org_name: string;
  kyc_status: string;
  tpv_by_currency: MoneyByCurrency;
  revenue_by_currency: MoneyByCurrency;
  tx_count: number;
  success_rate: number | null;
  refund_rate: number | null;
  trend_pct?: number;
};

export type MerchantList = { items: MerchantRow[]; total: number; page: number; per_page: number };

export type FunnelData = {
  timezone: string;
  series: { bucket: string; signups: number; emails_verified: number; orgs_created: number; kyc_submitted: number; kyc_verified: number; first_live_txn: number }[];
  totals: { signups: number; emails_verified: number; orgs_created: number; kyc_submitted: number; kyc_verified: number; first_live_txn: number; signup_to_live_pct?: number };
  median_hours_between_steps: Record<string, number | null>;
};

export type FlaggedMerchant = {
  org_id: string;
  org_name: string;
  reason: string;
  last_txn_at?: string;
  drop_pct?: number;
  contact_email?: string;
};

export type FailureRow = { code: string; provider: string; count: number; affected_orgs: number; sample_order_ids: string[] };

export type WithdrawalStats = {
  timezone: string;
  pending_count: number;
  pending_by_currency: MoneyByCurrency;
  approval_queue: number;
  aging: { bucket: string; count: number; by_currency: MoneyByCurrency }[];
  failed_payouts: number;
  avg_payout_hours: number | null;
};

export type WebhookHealth = {
  timezone: string;
  success_rate: number | null;
  retry_rate: number | null;
  p95_latency_s: number | null;
  stuck_processing: number;
  top_offenders: { endpoint_id: string; url: string; app_id: string; failed: number; last_error?: string }[];
};

export type StuckOrder = {
  order_id: string; app_id: string; org_id: string; org_name: string;
  provider: string; status: string; amount: string; currency: string;
  age_minutes: number; updated_at: string;
};

export type UnreconciledItem = {
  kind: string; order_id: string; app_id: string; org_id: string;
  amount: string; currency: string; age_hours: number;
};

export type NegativeBalance = {
  app_id: string; app_name: string; org_id: string; org_name: string;
  currency: string; balance: string;
};

export type AdminOrgDetail = {
  org: {
    id: string; name: string; slug: string; kyc_status: string;
    business_name?: string; tin?: string; live_max_txn_amount?: string; live_daily_volume_cap?: string;
  };
  members: { user_id: string; email: string; full_name?: string; phone?: string; role: string; status: string }[];
  kyc?: { business_name: string; tin: string; submitted_at: string; reviewed_by?: string; reviewed_at?: string; rejection_reason?: string };
  attempts: { id: string; status: string; created_at: string; reviewed_by?: string; rejection_reason?: string }[];
};

// ---------- Fetchers ----------

const BASE = "/api/v1/admin/analytics";

export const fetchOverview = (q?: AnalyticsQuery) => get<OverviewData>(`${BASE}/overview`, q);
export const fetchProviders = (q?: AnalyticsQuery) => get<ProviderStat[]>(`${BASE}/providers`, q);
export const fetchTopMerchants = (q?: AnalyticsQuery) => get<MerchantList>(`${BASE}/merchants/top`, q);
export const fetchFunnel = (q?: AnalyticsQuery) => get<FunnelData>(`${BASE}/merchants/signups`, q);
export const fetchDormant = (q?: AnalyticsQuery) => get<FlaggedMerchant[]>(`${BASE}/merchants/dormant`, q);
export const fetchChurn = (q?: AnalyticsQuery) => get<FlaggedMerchant[]>(`${BASE}/merchants/churn-risk`, q);
export const fetchFailures = (q?: AnalyticsQuery) => get<FailureRow[]>(`${BASE}/failures`, q);
export const fetchWithdrawalStats = (q?: AnalyticsQuery) => get<WithdrawalStats>(`${BASE}/withdrawals`, q);
export const fetchWebhookHealth = (q?: AnalyticsQuery) => get<WebhookHealth>(`${BASE}/webhooks`, q);
export const fetchStuckOrders = (q?: AnalyticsQuery) => get<StuckOrder[]>(`${BASE}/ops/stuck-orders`, q);
export const fetchUnreconciled = (q?: AnalyticsQuery) => get<UnreconciledItem[]>(`${BASE}/ops/unreconciled`, q);
export const fetchNegativeBalances = () => get<NegativeBalance[]>(`${BASE}/ops/negative-balances`);
export const fetchAdminOrg = (orgId: string) => get<AdminOrgDetail>(`/api/v1/admin/orgs/${orgId}`);
