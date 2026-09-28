import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ArrowRight, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listMyOrgs } from "@/lib/orgApi";
import {
  getMerchantBalance,
  listMerchantOrders,
  listMerchantWithdrawals,
  listMyApps,
} from "@/lib/merchantApi";

// Merchant home dashboard: org verification state, balances across apps,
// recent payments, and pending withdrawals — with doors to every section.
const MerchantDashboard = () => {
  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const org = (orgsQuery.data ?? []).find((o) => o.status === "active") ?? orgsQuery.data?.[0];

  const balanceQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "balance"],
      queryFn: () => getMerchantBalance(app.id),
      staleTime: 15_000,
    })),
  });
  const orderQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "orders", ""],
      queryFn: () => listMerchantOrders(app.id),
      staleTime: 15_000,
    })),
  });
  const withdrawalQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "withdrawals"],
      queryFn: () => listMerchantWithdrawals(app.id),
      staleTime: 15_000,
    })),
  });

  const loading = appsQuery.isLoading || orgsQuery.isLoading;
  const appName = (appId?: string) => apps.find((a) => a.id === appId)?.name ?? "App";

  const totals = new Map<string, number>();
  balanceQueries.forEach((q) => {
    const b = q.data;
    if (!b) return;
    totals.set(b.currency, (totals.get(b.currency) ?? 0) + Number(b.available_balance || 0));
  });

  const recentOrders = orderQueries
    .flatMap((q, i) => (q.data ?? []).map((o) => ({ ...o, app_id: apps[i]?.id ?? o.app_id })))
    .sort((a, b) => +new Date(b.created_at) - +new Date(a.created_at))
    .slice(0, 5);

  const pendingWithdrawals = withdrawalQueries
    .flatMap((q, i) => (q.data ?? []).map((w) => ({ ...w, app_id: apps[i]?.id ?? w.app_id })))
    .filter((w) => w.status === "requested" || w.status === "approved");

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">
            {org ? `Karibu, ${org.name}` : "Dashboard"}
          </h2>
          <p className="mt-1 text-sm text-slate-500">
            {org && org.kyc_status !== "verified"
              ? "Sandbox mode — verify your organization to unlock live payments."
              : "Live mode — your organization is verified."}
          </p>
        </div>
        <div className="flex items-center gap-2">
          {org && org.kyc_status !== "verified" && (
            <Button size="sm" variant="outline" asChild>
              <Link to={`/onboarding/kyc/${org.id}`}>Verify organization <ArrowRight className="h-3.5 w-3.5 ml-1" /></Link>
            </Button>
          )}
          <Button size="sm" asChild>
            <Link to="/merchant/apps"><Plus className="h-4 w-4 mr-1" /> New app</Link>
          </Button>
        </div>
      </div>

      {loading ? (
        <div className="mt-6 grid gap-3 sm:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Card key={i}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}
        </div>
      ) : (
        <>
          <div className="mt-6 grid gap-3 sm:grid-cols-3">
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Available balance</p>
              {totals.size === 0 && <p className="mt-1 text-lg font-extrabold text-slate-900">—</p>}
              {[...totals.entries()].map(([currency, total]) => (
                <p key={currency} className="mt-1 text-lg font-extrabold text-slate-900">
                  {total.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} {currency}
                </p>
              ))}
              <Link to="/merchant/payments" className="mt-2 inline-block text-xs text-blue-600 hover:underline">View payments →</Link>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Apps</p>
              <p className="mt-1 text-lg font-extrabold text-slate-900">{apps.length}</p>
              <Link to="/merchant/apps" className="mt-2 inline-block text-xs text-blue-600 hover:underline">Manage apps →</Link>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Awaiting payout action</p>
              <p className="mt-1 text-lg font-extrabold text-slate-900">{pendingWithdrawals.length}</p>
              <Link to="/merchant/withdrawals" className="mt-2 inline-block text-xs text-blue-600 hover:underline">Review withdrawals →</Link>
            </CardContent></Card>
          </div>

          <div className="mt-6 flex items-center justify-between">
            <h3 className="text-sm font-bold text-slate-800">Recent payments</h3>
            <Link to="/merchant/payments" className="text-xs text-blue-600 hover:underline">View all →</Link>
          </div>
          <Card className="mt-2">
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>App</TableHead>
                    <TableHead>Amount</TableHead>
                    <TableHead>Status</TableHead>
                    <TableHead>Created</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {recentOrders.map((o) => (
                    <TableRow key={`${o.app_id}-${o.id}`}>
                      <TableCell className="font-medium">{appName(o.app_id)}</TableCell>
                      <TableCell>{o.amount} {o.currency}</TableCell>
                      <TableCell><Badge variant="secondary">{o.status}</Badge></TableCell>
                      <TableCell>{o.created_at ? new Date(o.created_at).toLocaleString() : "—"}</TableCell>
                    </TableRow>
                  ))}
                  {recentOrders.length === 0 && (
                    <TableRow><TableCell colSpan={4} className="text-center text-slate-500">No payments yet — create an app and test in sandbox.</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      )}
    </div>
  );
};

export default MerchantDashboard;
