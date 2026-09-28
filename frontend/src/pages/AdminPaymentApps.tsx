import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { CreditCard, List, LayoutGrid, Wallet, Percent, ArrowRight, ExternalLink } from "lucide-react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import {
  getAppBalance,
  listPaymentApps,
  listPaymentWebhookEndpoints,
  type PaymentApp,
} from "@/lib/adminApi";

const VIEW_MODE_KEY = "admin-payment-apps-view-mode";

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleDateString() : "—";
}

function formatMoney(value: string | undefined, currency: string) {
  if (!value) return "—";
  const n = Number(value);
  return `${Number.isFinite(n) ? n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : value} ${currency}`;
}

// A negative available balance is a real, meaningful state (a refund
// reversed more than was ever withdrawn — the app now owes LipaGO) rather
// than an error, so it's never blocked, just called out visually.
function isNegativeBalance(value: string | undefined) {
  return typeof value === "string" && Number(value) < 0;
}

function formatFee(app: PaymentApp) {
  if (app.fee_type === "fixed") return `Fixed ${app.fee_fixed}`;
  if (app.fee_type === "hybrid") return `${app.fee_percent}% + ${app.fee_fixed}`;
  return `${app.fee_percent}%`;
}

// Operator oversight: every app registered against LipaGO, read-only.
// App creation, API keys, webhooks, fees, members, and deletion are
// merchant-side concerns now (merchant app detail) — operators keep the
// cross-org view plus payout recording under Withdrawals.
const AdminPaymentApps = () => {
  const [viewMode, setViewMode] = useState<"list" | "grid">(() => {
    if (typeof window === "undefined") return "grid";
    return (window.localStorage.getItem(VIEW_MODE_KEY) as "list" | "grid" | null) ?? "grid";
  });

  useEffect(() => {
    window.localStorage.setItem(VIEW_MODE_KEY, viewMode);
  }, [viewMode]);

  const appsQuery = useQuery({
    queryKey: ["admin", "payments-apps"],
    queryFn: () => listPaymentApps().then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const endpointsQuery = useQuery({
    queryKey: ["admin", "payments-webhook-endpoints"],
    queryFn: () => listPaymentWebhookEndpoints().then((data) => (Array.isArray(data) ? data : [])),
    staleTime: 30_000,
  });

  const apps = appsQuery.data ?? [];
  const endpoints = endpointsQuery.data ?? [];
  const loading = appsQuery.isLoading;

  const balanceQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["admin", "payment-app-balance", app.id],
      queryFn: () => getAppBalance(app.id),
      staleTime: 30_000,
    })),
  });
  const balanceByAppId = new Map(apps.map((app, i) => [app.id, balanceQueries[i]?.data]));

  useEffect(() => {
    if (appsQuery.error) toast.error(appsQuery.error instanceof Error ? appsQuery.error.message : "Unable to load payment apps.");
  }, [appsQuery.error]);

  const webhookCountByApp = (appId: string) => endpoints.filter((e) => e.app_id === appId).length;

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Payment Apps</h2>
          <p className="mt-1 text-sm text-slate-500">Oversight across every organization — read-only. Apps are managed merchant-side.</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" asChild>
            <Link to="/admin/payments/withdrawals">
              <Wallet className="h-4 w-4 mr-1.5" /> Withdrawals <ArrowRight className="h-3.5 w-3.5 ml-1.5" />
            </Link>
          </Button>
          <div className="flex items-center rounded-md border border-slate-200 p-0.5">
            <Button size="icon" variant={viewMode === "grid" ? "default" : "ghost"} className="h-8 w-8" title="Grid view" onClick={() => setViewMode("grid")}>
              <LayoutGrid className="h-4 w-4" />
            </Button>
            <Button size="icon" variant={viewMode === "list" ? "default" : "ghost"} className="h-8 w-8" title="List view" onClick={() => setViewMode("list")}>
              <List className="h-4 w-4" />
            </Button>
          </div>
        </div>
      </div>

      {viewMode === "grid" ? (
        <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {loading &&
            Array.from({ length: 6 }).map((_, i) => (
              <Card key={`skeleton-${i}`}>
                <CardContent className="p-4 space-y-3">
                  <div className="flex items-center gap-3">
                    <Skeleton className="h-10 w-10 rounded-xl" />
                    <div className="flex-1 space-y-1.5">
                      <Skeleton className="h-4 w-24" />
                      <Skeleton className="h-3 w-32" />
                    </div>
                  </div>
                  <Skeleton className="h-8 w-full" />
                </CardContent>
              </Card>
            ))}
          {!loading &&
            apps.map((app) => {
              const balance = balanceByAppId.get(app.id);
              return (
                <Card key={app.id} className="transition-shadow hover:shadow-md">
                  <CardContent className="p-4">
                    <div className="flex items-start gap-3">
                      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600">
                        <CreditCard size={18} />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-semibold text-slate-900">{app.name}</p>
                        <p className="truncate text-xs text-slate-500">{app.description || "No description"}</p>
                      </div>
                    </div>

                    <div className="mt-3 rounded-xl bg-slate-50 px-3 py-2.5">
                      <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Available balance</p>
                      <p className={`mt-0.5 text-lg font-extrabold ${balance && isNegativeBalance(balance.available_balance) ? "text-red-600" : "text-slate-900"}`}>
                        {balance ? formatMoney(balance.available_balance, balance.currency) : <Skeleton className="h-6 w-24" />}
                      </p>
                    </div>

                    <div className="mt-3 flex flex-wrap items-center gap-1.5">
                      <Badge variant="secondary">{app.status}</Badge>
                      <Badge variant="outline">{webhookCountByApp(app.id)} webhook{webhookCountByApp(app.id) === 1 ? "" : "s"}</Badge>
                      <Badge variant="outline" className="gap-1"><Percent size={11} /> {formatFee(app)}</Badge>
                    </div>
                    <p className="mt-3 text-xs text-slate-400">Registered {formatDate(app.created_at)}</p>

                    <div className="mt-3">
                      <Button size="sm" variant="outline" className="w-full" asChild>
                        <Link to={`/admin/payments/apps/${app.id}`}>
                          <ExternalLink className="h-3.5 w-3.5 mr-1" /> View
                        </Link>
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          {!loading && apps.length === 0 && <p className="col-span-full text-center text-sm text-slate-500">No payment apps yet.</p>}
        </div>
      ) : (
        <Table className="mt-6">
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Balance</TableHead>
              <TableHead>Fee</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Webhooks</TableHead>
              <TableHead>Created</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {apps.map((app) => {
              const balance = balanceByAppId.get(app.id);
              return (
                <TableRow key={app.id}>
                  <TableCell className="font-medium">
                    <Link to={`/admin/payments/apps/${app.id}`} className="hover:underline">{app.name}</Link>
                  </TableCell>
                  <TableCell className={balance && isNegativeBalance(balance.available_balance) ? "text-red-600 font-medium" : undefined}>
                    {balance ? formatMoney(balance.available_balance, balance.currency) : <Skeleton className="h-4 w-20" />}
                  </TableCell>
                  <TableCell className="text-xs text-slate-500">{formatFee(app)}</TableCell>
                  <TableCell><Badge variant="secondary">{app.status}</Badge></TableCell>
                  <TableCell>{webhookCountByApp(app.id)}</TableCell>
                  <TableCell>{formatDate(app.created_at)}</TableCell>
                  <TableCell className="text-right">
                    <Button size="sm" variant="outline" asChild>
                      <Link to={`/admin/payments/apps/${app.id}`}>View</Link>
                    </Button>
                  </TableCell>
                </TableRow>
              );
            })}
            {!loading && apps.length === 0 && (
              <TableRow><TableCell colSpan={7} className="text-center text-slate-500">No payment apps yet.</TableCell></TableRow>
            )}
          </TableBody>
        </Table>
      )}
    </div>
  );
};

export default AdminPaymentApps;
