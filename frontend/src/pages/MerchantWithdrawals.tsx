import { useState } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, X } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { toast } from "@/components/ui/sonner";
import {
  approveMerchantWithdrawal,
  listMerchantWithdrawals,
  listMyApps,
  rejectMerchantWithdrawal,
} from "@/lib/merchantApi";

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

// Merchant-space Withdrawals: every payout request across all apps, with
// approve/reject inline (finance/owner). New requests are created per app
// under My Apps → Manage → Withdrawals.
const MerchantWithdrawals = () => {
  const queryClient = useQueryClient();
  const [appFilter, setAppFilter] = useState("");
  const [actingId, setActingId] = useState<string | null>(null);

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const appName = (appId: string) => apps.find((a) => a.id === appId)?.name ?? "App";

  const withdrawalQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "withdrawals"],
      queryFn: () => listMerchantWithdrawals(app.id),
      staleTime: 15_000,
    })),
  });
  const loading = appsQuery.isLoading || withdrawalQueries.some((q) => q.isLoading);

  const withdrawals = withdrawalQueries.flatMap((q, i) =>
    (q.data ?? []).map((w) => ({ ...w, app_id: apps[i]?.id ?? w.app_id })),
  ).filter((w) => !appFilter || w.app_id === appFilter);

  const reload = () => queryClient.invalidateQueries({ queryKey: ["merchant"] });

  const runAction = async (appId: string, id: string, action: () => Promise<unknown>, successMessage: string) => {
    setActingId(id);
    try {
      await action();
      toast.success(successMessage);
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update withdrawal."));
    } finally {
      setActingId(null);
    }
  };

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Withdrawals</h2>
        <p className="mt-1 text-sm text-slate-500">Payout requests across all your apps. New requests are created per app under My Apps → Manage.</p>
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

      <div className="mt-4 space-y-3">
        {loading && <Card><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>}
        {!loading && withdrawals.length === 0 && (
          <Card><CardContent className="flex flex-col items-center gap-2 p-10 text-center">
            <p className="text-sm font-medium text-slate-600">No withdrawals yet.</p>
          </CardContent></Card>
        )}
        {withdrawals.map((w) => {
          const busy = actingId === w.id;
          return (
            <Card key={`${w.app_id}-${w.id}`}>
              <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
                <div className="min-w-0">
                  <div className="flex flex-wrap items-center gap-2">
                    <Link to={`/merchant/apps/${w.app_id}`} className="text-sm font-semibold text-blue-600 hover:underline">
                      {appName(w.app_id)}
                    </Link>
                    <Badge variant="outline">{w.status}</Badge>
                  </div>
                  <p className="mt-1 text-lg font-extrabold text-slate-900">{w.amount} {w.currency}</p>
                  <p className="mt-1 text-xs text-slate-400">Requested {w.created_at ? new Date(w.created_at).toLocaleString() : "—"}</p>
                  {w.notes && <p className="mt-1 text-xs text-slate-500">Note: {w.notes}</p>}
                </div>
                {w.status === "requested" && (
                  <div className="flex shrink-0 flex-wrap items-center gap-1.5 self-end sm:self-start">
                    <Button size="sm" disabled={busy} onClick={() => runAction(w.app_id, w.id, () => approveMerchantWithdrawal(w.app_id, w.id), "Withdrawal approved — balance debited.")}>
                      <Check className="h-3.5 w-3.5 mr-1" /> Approve
                    </Button>
                    <Button size="sm" variant="outline" disabled={busy} onClick={() => runAction(w.app_id, w.id, () => rejectMerchantWithdrawal(w.app_id, w.id), "Withdrawal rejected.")}>
                      <X className="h-3.5 w-3.5 mr-1" /> Reject
                    </Button>
                  </div>
                )}
              </CardContent>
            </Card>
          );
        })}
      </div>
    </div>
  );
};

export default MerchantWithdrawals;
