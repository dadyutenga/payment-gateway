import { useState } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listMerchantEndpoints, listMyApps } from "@/lib/merchantApi";

// Merchant-space Webhooks: every endpoint across all apps, read-only.
// Endpoints are managed per app under My Apps → Manage → Webhooks.
const MerchantWebhooks = () => {
  const [appFilter, setAppFilter] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const appName = (appId: string) => apps.find((a) => a.id === appId)?.name ?? "App";

  const endpointQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "endpoints"],
      queryFn: () => listMerchantEndpoints(app.id),
      staleTime: 15_000,
    })),
  });
  const loading = appsQuery.isLoading || endpointQueries.some((q) => q.isLoading);

  const endpoints = endpointQueries.flatMap((q, i) =>
    (q.data ?? []).map((e) => ({ ...e, app_id: apps[i]?.id ?? e.app_id })),
  ).filter((e) => !appFilter || e.app_id === appFilter);

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Webhooks</h2>
        <p className="mt-1 text-sm text-slate-500">Every webhook endpoint across all your apps. Manage them per app under My Apps → Manage.</p>
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
                  <TableHead>URL</TableHead>
                  <TableHead>Events</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Secret</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {endpoints.map((e) => (
                  <TableRow key={`${e.app_id}-${e.id}`}>
                    <TableCell>
                      <Link to={`/merchant/apps/${e.app_id}`} className="font-medium text-blue-600 hover:underline">
                        {appName(e.app_id)}
                      </Link>
                    </TableCell>
                    <TableCell className="max-w-xs truncate font-mono text-xs">{e.url}</TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-slate-500">{e.event_types.join(", ")}</TableCell>
                    <TableCell><Badge variant="secondary">{e.status}</Badge></TableCell>
                    <TableCell className="text-xs text-slate-500">v{e.secret_version}</TableCell>
                  </TableRow>
                ))}
                {endpoints.length === 0 && (
                  <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No webhook endpoints yet.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

export default MerchantWebhooks;
