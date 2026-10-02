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
  approveCreatorWithdrawal,
  getCreatorBalance,
  listCreatorApps,
  listCreatorWithdrawals,
  rejectCreatorWithdrawal,
} from "@/lib/creatorApi";

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

// Creator-space Payouts: balances across the receiving app plus payout
// requests with approve/reject inline (sole owner). The payout destination
// itself (OTP-verified mobile money) is managed under Settings → Payout
// destination — withdrawals always pay there.
const CreatorPayouts = () => {
  const queryClient = useQueryClient();
  const [actingId, setActingId] = useState<string | null>(null);

  const appsQuery = useQuery({ queryKey: ["creator", "my-apps"], queryFn: () => listCreatorApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];

  const balanceQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["creator", app.id, "balance"],
      queryFn: () => getCreatorBalance(app.id),
      staleTime: 15_000,
    })),
  });
  const withdrawalQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["creator", app.id, "withdrawals"],
      queryFn: () => listCreatorWithdrawals(app.id),
      staleTime: 15_000,
    })),
  });
  const loading = appsQuery.isLoading || balanceQueries.some((q) => q.isLoading) || withdrawalQueries.some((q) => q.isLoading);

  const withdrawals = withdrawalQueries.flatMap((q, i) =>
    (q.data ?? []).map((w) => ({ ...w, app_id: apps[i]?.id ?? w.app_id })),
  );

  const reload = () => queryClient.invalidateQueries({ queryKey: ["creator"] });

  const runAction = async (appId: string, id: string, action: () => Promise<unknown>, successMessage: string) => {
    setActingId(id);
    try {
      await action();
      toast.success(successMessage);
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update payout."));
    } finally {
      setActingId(null);
    }
  };

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Payouts</h2>
        <p className="mt-1 text-sm text-slate-500">
          Balances and payout requests for your support earnings. Payouts always go to your verified destination —{" "}
          <Link to="/creator/settings" className="font-medium text-fuchsia-700 hover:underline">manage it under Settings → Payout destination</Link>.
        </p>
      </div>

      {loading ? (
        <Card className="mt-4"><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
      ) : (
        <>
          <div className="mt-4 grid gap-3 sm:grid-cols-2">
            {balanceQueries.map((q, i) => (
              <Card key={apps[i]?.id ?? i}>
                <CardContent className="p-4">
                  <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">
                    Available · {apps[i]?.name ?? "App"}
                  </p>
                  <p className="mt-1 text-xl font-extrabold text-slate-900">
                    {q.data ? `${q.data.available_balance} ${q.data.currency}` : "—"}
                  </p>
                  {q.data && (
                    <p className="mt-1 text-xs text-slate-400">
                      Total received {q.data.total_revenue} {q.data.currency} · Withdrawn {q.data.total_withdrawn} {q.data.currency}
                    </p>
                  )}
                </CardContent>
              </Card>
            ))}
            {apps.length === 0 && (
              <Card><CardContent className="p-4 text-sm text-slate-500">
                No receiving app yet — enable your support page under <Link to="/creator/page" className="font-medium text-fuchsia-700 hover:underline">My Page</Link>.
              </CardContent></Card>
            )}
          </div>

          <div className="mt-4 space-y-3">
            {withdrawals.length === 0 && (
              <Card><CardContent className="flex flex-col items-center gap-2 p-10 text-center">
                <p className="text-sm font-medium text-slate-600">No payouts yet.</p>
              </CardContent></Card>
            )}
            {withdrawals.map((w) => {
              const busy = actingId === w.id;
              return (
                <Card key={`${w.app_id}-${w.id}`}>
                  <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <span className="text-sm font-semibold text-slate-800">{apps.find((a) => a.id === w.app_id)?.name ?? "Support app"}</span>
                        <Badge variant="outline">{w.status}</Badge>
                      </div>
                      <p className="mt-1 text-lg font-extrabold text-slate-900">{w.amount} {w.currency}</p>
                      <p className="mt-1 text-xs text-slate-400">Requested {w.created_at ? new Date(w.created_at).toLocaleString() : "—"}</p>
                      {w.notes && <p className="mt-1 text-xs text-slate-500">Note: {w.notes}</p>}
                    </div>
                    {w.status === "requested" && (
                      <div className="flex shrink-0 flex-wrap items-center gap-1.5 self-end sm:self-start">
                        <Button size="sm" disabled={busy} onClick={() => runAction(w.app_id, w.id, () => approveCreatorWithdrawal(w.app_id, w.id), "Payout approved — balance debited.")}>
                          <Check className="h-3.5 w-3.5 mr-1" /> Approve
                        </Button>
                        <Button size="sm" variant="outline" disabled={busy} onClick={() => runAction(w.app_id, w.id, () => rejectCreatorWithdrawal(w.app_id, w.id), "Payout rejected.")}>
                          <X className="h-3.5 w-3.5 mr-1" /> Reject
                        </Button>
                      </div>
                    )}
                  </CardContent>
                </Card>
              );
            })}
          </div>
        </>
      )}
    </div>
  );
};

export default CreatorPayouts;
