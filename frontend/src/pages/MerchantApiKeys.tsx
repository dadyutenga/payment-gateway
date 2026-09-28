import { useState } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listMerchantKeys, listMyApps } from "@/lib/merchantApi";

// Merchant-space API keys: every key prefix across all apps, read-only.
// Raw secrets are shown exactly once at creation, per app under
// My Apps → Manage → API keys.
const MerchantApiKeys = () => {
  const [appFilter, setAppFilter] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const appName = (appId: string) => apps.find((a) => a.id === appId)?.name ?? "App";

  const keyQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "keys"],
      queryFn: () => listMerchantKeys(app.id),
      staleTime: 15_000,
    })),
  });
  const loading = appsQuery.isLoading || keyQueries.some((q) => q.isLoading);

  const keys = keyQueries.flatMap((q, i) =>
    (q.data ?? []).map((k) => ({ ...k, app_id: apps[i]?.id ?? k.app_id })),
  ).filter((k) => !appFilter || k.app_id === appFilter);

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">API keys</h2>
        <p className="mt-1 text-sm text-slate-500">Every API key prefix across all your apps. Secrets are issued per app under My Apps → Manage.</p>
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
                  <TableHead>Prefix</TableHead>
                  <TableHead>Label</TableHead>
                  <TableHead>Environment</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Created</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {keys.map((k) => (
                  <TableRow key={`${k.app_id}-${k.id}`}>
                    <TableCell>
                      <Link to={`/merchant/apps/${k.app_id}`} className="font-medium text-blue-600 hover:underline">
                        {appName(k.app_id)}
                      </Link>
                    </TableCell>
                    <TableCell className="font-mono text-xs">{k.prefix}…</TableCell>
                    <TableCell className="text-xs text-slate-500">{k.label || "—"}</TableCell>
                    <TableCell><Badge variant={k.environment === "live" ? "default" : "secondary"}>{k.environment}</Badge></TableCell>
                    <TableCell><Badge variant="outline">{k.status}</Badge></TableCell>
                    <TableCell>{k.created_at ? new Date(k.created_at).toLocaleString() : "—"}</TableCell>
                  </TableRow>
                ))}
                {keys.length === 0 && (
                  <TableRow><TableCell colSpan={6} className="text-center text-slate-500">No API keys yet.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

export default MerchantApiKeys;
