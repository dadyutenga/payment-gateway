import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { RefreshCw } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  fetchNegativeBalances, fetchStuckOrders, fetchUnreconciled,
  fetchWebhookHealth, fetchWithdrawalStats,
} from "@/lib/analyticsApi";
import { CsvButton } from "@/pages/analyticsCommon";

function Section({
  title, count, critical, children,
}: {
  title: string;
  count?: number;
  critical?: boolean;
  children: React.ReactNode;
}) {
  return (
    <Card className="mt-4">
      <CardContent className="p-4">
        <div className="flex items-center justify-between">
          <h3 className="text-sm font-bold text-slate-800">{title}</h3>
          {count !== undefined && (
            <Badge variant={count > 0 ? (critical ? "destructive" : "secondary") : "outline"}>{count}</Badge>
          )}
        </div>
        <div className="mt-3">{children}</div>
      </CardContent>
    </Card>
  );
}

const AdminOps = () => {
  const [autoRefresh, setAutoRefresh] = useState(false);
  const refetch = autoRefresh ? 45_000 : false;

  const stuckQuery = useQuery({
    queryKey: ["admin", "ops", "stuck"], queryFn: () => fetchStuckOrders({ stuck_minutes: 30 }),
    staleTime: 30_000, refetchInterval: refetch,
  });
  const unreconciledQuery = useQuery({
    queryKey: ["admin", "ops", "unreconciled"], queryFn: () => fetchUnreconciled(),
    staleTime: 30_000, refetchInterval: refetch,
  });
  const negativeQuery = useQuery({
    queryKey: ["admin", "ops", "negative"], queryFn: () => fetchNegativeBalances(),
    staleTime: 30_000, refetchInterval: refetch,
  });
  const withdrawalsQuery = useQuery({
    queryKey: ["admin", "ops", "withdrawals"], queryFn: () => fetchWithdrawalStats(),
    staleTime: 30_000, refetchInterval: refetch,
  });
  const webhooksQuery = useQuery({
    queryKey: ["admin", "ops", "webhooks"], queryFn: () => fetchWebhookHealth(),
    staleTime: 30_000, refetchInterval: refetch,
  });

  const stuck = stuckQuery.data ?? [];
  const unreconciled = unreconciledQuery.data ?? [];
  const negative = negativeQuery.data ?? [];
  const wstats = withdrawalsQuery.data;
  const whealth = webhooksQuery.data;

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Operations</h2>
          <p className="mt-1 text-sm text-slate-500">Exceptions that need a human · red is bad, amber needs a look</p>
        </div>
        <Button size="sm" variant={autoRefresh ? "default" : "outline"} onClick={() => setAutoRefresh(!autoRefresh)}>
          <RefreshCw className="h-3.5 w-3.5 mr-1" /> Auto-refresh {autoRefresh ? "on (45s)" : "off"}
        </Button>
      </div>

      <Section title="Stuck orders (pending/processing > 30 min)" count={stuck.length} critical={stuck.length > 0}>
        <CsvButton report="stuck-orders" />
        {stuckQuery.isLoading ? <Skeleton className="mt-2 h-20 w-full" /> : stuck.length === 0 ? (
          <p className="mt-2 text-sm text-slate-500">Clear — nothing stuck.</p>
        ) : (
          <Table>
            <TableHeader><TableRow>
              <TableHead>Order</TableHead><TableHead>Org</TableHead><TableHead>Provider</TableHead>
              <TableHead>Status</TableHead><TableHead>Amount</TableHead><TableHead>Age</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {stuck.map((o) => (
                <TableRow key={o.order_id}>
                  <TableCell className="font-mono text-xs">{o.order_id.slice(0, 8)}…</TableCell>
                  <TableCell className="text-xs">
                    {o.org_id ? <Link to={`/admin/orgs/${o.org_id}`} className="text-blue-600 hover:underline">{o.org_name || o.org_id.slice(0, 8)}</Link> : "—"}
                  </TableCell>
                  <TableCell className="text-xs">{o.provider}</TableCell>
                  <TableCell><Badge variant="secondary">{o.status}</Badge></TableCell>
                  <TableCell className="text-xs">{o.amount} {o.currency}</TableCell>
                  <TableCell className="text-xs font-semibold text-amber-700">{o.age_minutes.toFixed(0)} min</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Section>

      <Section title="Unreconciled (paid⇄ledger mismatches)" count={unreconciled.length} critical={unreconciled.length > 0}>
        <CsvButton report="unreconciled" />
        {unreconciledQuery.isLoading ? <Skeleton className="mt-2 h-20 w-full" /> : unreconciled.length === 0 ? (
          <p className="mt-2 text-sm text-slate-500">Ledger and provider agree.</p>
        ) : (
          <Table>
            <TableHeader><TableRow>
              <TableHead>Kind</TableHead><TableHead>Order</TableHead><TableHead>Amount</TableHead><TableHead>Age</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {unreconciled.map((u, i) => (
                <TableRow key={`${u.kind}-${u.order_id}-${i}`}>
                  <TableCell><Badge variant="destructive">{u.kind.replace(/_/g, " ")}</Badge></TableCell>
                  <TableCell className="font-mono text-xs">{u.order_id.slice(0, 8)}…</TableCell>
                  <TableCell className="text-xs">{u.amount} {u.currency}</TableCell>
                  <TableCell className="text-xs">{u.age_hours.toFixed(1)} h</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Section>

      <Section title="Negative balances" count={negative.length} critical={negative.length > 0}>
        <CsvButton report="negative-balances" />
        {negativeQuery.isLoading ? <Skeleton className="mt-2 h-20 w-full" /> : negative.length === 0 ? (
          <p className="mt-2 text-sm text-slate-500">No negative balances.</p>
        ) : (
          <Table>
            <TableHeader><TableRow>
              <TableHead>App</TableHead><TableHead>Org</TableHead><TableHead>Balance</TableHead>
            </TableRow></TableHeader>
            <TableBody>
              {negative.map((n) => (
                <TableRow key={`${n.app_id}-${n.currency}`}>
                  <TableCell className="text-xs">{n.app_name}</TableCell>
                  <TableCell className="text-xs">
                    {n.org_id ? <Link to={`/admin/orgs/${n.org_id}`} className="text-blue-600 hover:underline">{n.org_name}</Link> : n.org_name}
                  </TableCell>
                  <TableCell className="font-bold text-rose-600">{n.balance} {n.currency}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </Section>

      <Section title="Withdrawal aging (open)" count={wstats?.pending_count} critical={(wstats?.pending_count ?? 0) > 0}>
        {withdrawalsQuery.isLoading ? <Skeleton className="mt-2 h-20 w-full" /> : wstats && (
          <div className="flex flex-wrap gap-2">
            {wstats.aging.map((b) => (
              <div key={b.bucket} className={`rounded-lg border px-3 py-2 text-xs ${b.count > 0 && (b.bucket === "1-3d" || b.bucket === ">3d") ? "border-amber-300 bg-amber-50" : "border-slate-200"}`}>
                <p className="font-bold text-slate-900">{b.bucket}: {b.count}</p>
                <p className="text-slate-500">{Object.entries(b.by_currency).map(([c, v]) => `${v} ${c}`).join(" · ") || "—"}</p>
              </div>
            ))}
            <Link to="/admin/payments/withdrawals" className="self-center text-xs text-blue-600 hover:underline">Open withdrawals →</Link>
          </div>
        )}
      </Section>

      <Section
        title="Webhook health"
        count={whealth ? whealth.stuck_processing + whealth.top_offenders.length : undefined}
        critical={(whealth?.stuck_processing ?? 0) > 0}
      >
        {webhooksQuery.isLoading ? <Skeleton className="mt-2 h-20 w-full" /> : whealth && (
          <div className="text-sm text-slate-600">
            <p>
              Success <strong>{whealth.success_rate === null || whealth.success_rate === undefined ? "—" : `${(whealth.success_rate * 100).toFixed(1)}%`}</strong>
              {" · "}retry {whealth.retry_rate === null || whealth.retry_rate === undefined ? "—" : `${(whealth.retry_rate * 100).toFixed(1)}%`}
              {" · "}p95 {whealth.p95_latency_s === null || whealth.p95_latency_s === undefined ? "—" : `${whealth.p95_latency_s.toFixed(1)}s`}
              {" · "}stuck processing <strong className={whealth.stuck_processing > 0 ? "text-rose-600" : ""}>{whealth.stuck_processing}</strong>
            </p>
            {whealth.top_offenders.length > 0 && (
              <Table>
                <TableHeader><TableRow><TableHead>Endpoint</TableHead><TableHead>Failed</TableHead><TableHead>Last error</TableHead></TableRow></TableHeader>
                <TableBody>
                  {whealth.top_offenders.map((o) => (
                    <TableRow key={o.endpoint_id}>
                      <TableCell className="max-w-xs truncate font-mono text-xs">{o.url}</TableCell>
                      <TableCell className="font-bold text-rose-600">{o.failed}</TableCell>
                      <TableCell className="max-w-xs truncate text-xs text-slate-500">{o.last_error || "—"}</TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </div>
        )}
      </Section>
    </div>
  );
};

export default AdminOps;
