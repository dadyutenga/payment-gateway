import { useState } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listMerchantDeliveries, listMyApps } from "@/lib/merchantApi";

// Merchant-space Deliveries: every webhook delivery across all apps,
// read-only. Replays happen per app under My Apps → Manage → Deliveries.
const MerchantDeliveries = () => {
  const [appFilter, setAppFilter] = useState("");
  const [statusFilter, setStatusFilter] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const appName = (appId: string) => apps.find((a) => a.id === appId)?.name ?? "App";

  const deliveryQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "deliveries", statusFilter],
      queryFn: () => listMerchantDeliveries(app.id, statusFilter ? { status: statusFilter } : undefined),
      staleTime: 15_000,
    })),
  });
  const loading = appsQuery.isLoading || deliveryQueries.some((q) => q.isLoading);

  const deliveries = deliveryQueries.flatMap((q, i) =>
    (q.data ?? []).map((d) => ({ ...d, app_id: apps[i]?.id ?? d.app_id })),
  ).filter((d) => !appFilter || d.app_id === appFilter);

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Deliveries</h2>
        <p className="mt-1 text-sm text-slate-500">Every webhook delivery across all your apps. Replays happen per app under My Apps → Manage.</p>
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
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value)}
        >
          {["", "pending", "delivered", "retrying", "failed"].map((s) => (
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
                  <TableHead>Event</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Attempts</TableHead>
                  <TableHead>Last response</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {deliveries.map((d) => (
                  <TableRow key={`${d.app_id}-${d.id}`}>
                    <TableCell>
                      <Link to={`/merchant/apps/${d.app_id}`} className="font-medium text-blue-600 hover:underline">
                        {appName(d.app_id)}
                      </Link>
                    </TableCell>
                    <TableCell className="text-xs text-slate-600">{d.event_type}</TableCell>
                    <TableCell><Badge variant="secondary">{d.status}</Badge></TableCell>
                    <TableCell className="text-xs">{d.attempt_count}</TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-slate-500">
                      {d.last_response_status ?? "—"}{d.last_error ? ` · ${d.last_error}` : ""}
                    </TableCell>
                    <TableCell>{d.created_at ? new Date(d.created_at).toLocaleString() : "—"}</TableCell>
                  </TableRow>
                ))}
                {deliveries.length === 0 && (
                  <TableRow><TableCell colSpan={6} className="text-center text-slate-500">No deliveries yet.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

export default MerchantDeliveries;
