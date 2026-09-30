import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { toast } from "@/components/ui/sonner";
import { listMerchantOrders, listMyApps } from "@/lib/merchantApi";

// Merchant-space Payments: every order across every app you can access.
// Backend scopes each app's orders by membership — this page only merges
// what the caller is already allowed to see.
const MerchantPayments = () => {
  const [appFilter, setAppFilter] = useState("");
  const [orderStatus, setOrderStatus] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const appName = (appId?: string) => apps.find((a) => a.id === appId)?.name ?? "App";

  const orderQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "orders", orderStatus],
      queryFn: () => listMerchantOrders(app.id, orderStatus || undefined),
      staleTime: 15_000,
    })),
  });
  const loading = appsQuery.isLoading || orderQueries.some((q) => q.isLoading);
  const failed = orderQueries.find((q) => q.error)?.error;
  useEffect(() => {
    if (failed) toast.error(failed instanceof Error ? failed.message : "Unable to load payments.");
  }, [failed]);

  const orders = orderQueries.flatMap((q, i) =>
    (q.data ?? []).map((o) => ({ ...o, app_id: apps[i]?.id ?? o.app_id })),
  ).filter((o) => !appFilter || o.app_id === appFilter);

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Payments</h2>
        <p className="mt-1 text-sm text-slate-500">Every payment order across all your apps. Per-app detail lives under My Apps → Manage.</p>
      </div>

      <div className="mt-4 flex flex-wrap items-center gap-2">
        <select
          aria-label="Filter by app"
          className="h-9 rounded-md border border-slate-300 px-2 text-sm"
          value={appFilter}
          onChange={(e) => setAppFilter(e.target.value)}
        >
          <option value="">All apps</option>
          {apps.map((a) => (
            <option key={a.id} value={a.id}>{a.name}</option>
          ))}
        </select>
        <select
          aria-label="Filter by status"
          className="h-9 rounded-md border border-slate-300 px-2 text-sm"
          value={orderStatus}
          onChange={(e) => setOrderStatus(e.target.value)}
        >
          {["", "pending", "paid", "failed", "expired"].map((s) => (
            <option key={s} value={s}>{s === "" ? "Any status" : s}</option>
          ))}
        </select>
      </div>

      <Card className="mt-4">
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-2 p-4"><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>App</TableHead>
                  <TableHead>Provider</TableHead>
                  <TableHead>Amount</TableHead>
                  <TableHead>Buyer</TableHead>
                  <TableHead>Reference</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {orders.map((order) => (
                  <TableRow key={`${order.app_id}-${order.id}`}>
                    <TableCell>
                      <Link to={`/merchant/apps/${order.app_id}`} className="font-medium text-blue-600 hover:underline">
                        {appName(order.app_id)}
                      </Link>
                    </TableCell>
                    <TableCell>{order.provider}</TableCell>
                    <TableCell>{order.amount} {order.currency}</TableCell>
                    <TableCell>
                      {order.buyer_name || order.buyer_phone || "—"}
                      {typeof order.metadata?.supporter_message === "string" && order.metadata.supporter_message.trim() !== "" && (
                        <p className="mt-0.5 max-w-xs truncate text-xs italic text-slate-500" title={order.metadata.supporter_message as string}>
                          “{order.metadata.supporter_message as string}”
                        </p>
                      )}
                    </TableCell>
                    <TableCell className="max-w-xs truncate font-mono text-xs">{order.external_reference || order.provider_order_id || "—"}</TableCell>
                    <TableCell><Badge variant="secondary">{order.status}</Badge></TableCell>
                    <TableCell>{order.created_at ? new Date(order.created_at).toLocaleString() : "—"}</TableCell>
                  </TableRow>
                ))}
                {orders.length === 0 && (
                  <TableRow><TableCell colSpan={7} className="text-center text-slate-500">No payments yet.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

export default MerchantPayments;
