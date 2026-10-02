import { useMemo } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery } from "@tanstack/react-query";
import { ArrowRight } from "lucide-react";
import { Area, AreaChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { listMyOrgs } from "@/lib/orgApi";
import { fetchCreatorOverview, fetchCreatorSupporters, listCreatorApps, listCreatorOrders } from "@/lib/creatorApi";
import { EnvToggle } from "@/pages/merchantAnalyticsCommon";
import { DateRangePicker, moneyText, useFilterParams } from "@/pages/analyticsCommon";

// Masking helpers (Phase 2 privacy rule): supporter identities stay
// masked — first name plus partially hidden phone, never full details.
function maskPhone(phone?: string) {
  const digits = (phone ?? "").replace(/\D/g, "");
  if (digits.length < 4) return "•••";
  return `+${digits.slice(0, 3)} ••• ••• ${digits.slice(-3)}`;
}

function maskBuyer(name?: string, phone?: string) {
  const first = (name ?? "").trim().split(/\s+/)[0] || "Supporter";
  return `${first} · ${maskPhone(phone)}`;
}

// Creator home dashboard (trimmed vs merchant): total received,
// transaction count, trend chart, and recent supporters with their
// (private, masked) messages. No methods/peak-hours analytics in v1.
const CreatorOverview = () => {
  const { from, to, env, setRange, setEnv } = useFilterParams();
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000 });
  const org = (orgsQuery.data ?? []).find((o) => o.status === "active") ?? orgsQuery.data?.[0];
  const orgId = org?.id ?? "";

  const overviewQuery = useQuery({
    queryKey: ["creator", "analytics", "overview", orgId, from, to, env],
    queryFn: () => fetchCreatorOverview(orgId, { from, to, granularity: "day", environment: env }),
    enabled: !!orgId,
    staleTime: 30_000,
  });
  const customersQuery = useQuery({
    queryKey: ["creator", "analytics", "supporters", orgId, from, to, env],
    queryFn: () => fetchCreatorSupporters(orgId, { from, to, granularity: "day", environment: env }),
    enabled: !!orgId,
    staleTime: 30_000,
  });
  const appsQuery = useQuery({ queryKey: ["creator", "my-apps"], queryFn: () => listCreatorApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const orderQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["creator", app.id, "orders", ""],
      queryFn: () => listCreatorOrders(app.id),
      staleTime: 15_000,
    })),
  });

  const data = overviewQuery.data;
  const chartRows = useMemo(() => {
    return (data?.series ?? []).map((pt) => {
      const row: Record<string, number | string | null> = { bucket: pt.bucket, tx_count: pt.tx_count };
      Object.entries(pt.gross_by_currency ?? {}).forEach(([c, v]) => {
        row[`gross_${c}`] = Number(v);
      });
      return row;
    });
  }, [data]);
  const currencies = useMemo(() => {
    const set = new Set<string>();
    (data?.series ?? []).forEach((pt) => Object.keys(pt.gross_by_currency ?? {}).forEach((c) => set.add(c)));
    return [...set];
  }, [data]);

  const recentSupporters = useMemo(() => {
    return orderQueries
      .flatMap((q) => q.data ?? [])
      .filter((o) => o.status === "paid" && typeof o.metadata?.supporter_message === "string" && (o.metadata.supporter_message as string).trim() !== "")
      .sort((a, b) => +new Date(b.created_at) - +new Date(a.created_at))
      .slice(0, 5);
  }, [orderQueries]);

  const topPayers = customersQuery.data?.top_payers ?? [];
  const loading = orgsQuery.isLoading || overviewQuery.isLoading;

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">
            {org ? `Karibu, ${org.display_name || org.name}` : "Overview"}
          </h2>
          <p className="mt-1 text-sm text-slate-500">
            {org && org.kyc_status !== "verified"
              ? "Sandbox mode — verify your identity to unlock live support payments."
              : "Live mode — your page accepts real support."}
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EnvToggle env={env} onChange={setEnv} />
          <DateRangePicker from={from} to={to} onChange={setRange} />
          {org && org.kyc_status !== "verified" && (
            <Button size="sm" variant="outline" asChild>
              <Link to={`/creator/verify/${org.id}`}>Verify identity <ArrowRight className="h-3.5 w-3.5 ml-1" /></Link>
            </Button>
          )}
          {org?.handle && (
            <Button size="sm" asChild>
              <Link to={`/c/${org.handle}`}>View my page</Link>
            </Button>
          )}
        </div>
      </div>

      {loading ? (
        <div className="mt-6 grid gap-3 sm:grid-cols-3">
          {[0, 1, 2].map((i) => (
            <Card key={i}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}
        </div>
      ) : overviewQuery.error ? (
        <p className="mt-6 text-sm text-red-600">Unable to load overview right now.</p>
      ) : data && (
        <>
          <div className="mt-6 grid gap-3 sm:grid-cols-3">
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Total received</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{moneyText(data.tpv)}</p>
              <Link to="/creator/payments" className="mt-2 inline-block text-xs text-fuchsia-700 hover:underline">View payments →</Link>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Transactions</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{data.tx_count.toLocaleString()}</p>
              <p className="mt-2 text-xs text-slate-400">{customersQuery.data?.payers_total ?? 0} supporters</p>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Top supporter (masked)</p>
              {topPayers.length === 0 ? (
                <p className="mt-1 text-lg font-extrabold text-slate-900">—</p>
              ) : (
                <>
                  <p className="mt-1 font-mono text-sm font-bold text-slate-900">{topPayers[0].masked_id}</p>
                  <p className="text-xs text-slate-400">{topPayers[0].tx_count} payments · {topPayers[0].volume} {topPayers[0].currency}</p>
                </>
              )}
            </CardContent></Card>
          </div>

          <Card className="mt-4"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Support over time (per currency)</h3>
            <div className="mt-2 h-64">
              <ResponsiveContainer width="100%" height="100%">
                <AreaChart data={chartRows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="bucket" tick={{ fontSize: 11 }} minTickGap={24} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Legend />
                  {currencies.map((c, i) => (
                    <Area key={c} type="monotone" dataKey={`gross_${c}`} name={c} stackId="1"
                      stroke={["#0f766e", "#2563eb", "#9333ea"][i % 3]} fill={["#0f766e", "#2563eb", "#9333ea"][i % 3]} fillOpacity={0.25} />
                  ))}
                </AreaChart>
              </ResponsiveContainer>
            </div>
          </CardContent></Card>

          <Card className="mt-4"><CardContent className="p-4">
            <div className="flex items-center justify-between">
              <h3 className="text-sm font-bold text-slate-800">Recent supporters</h3>
              <Link to="/creator/payments" className="text-xs text-fuchsia-700 hover:underline">View all →</Link>
            </div>
            <p className="mt-1 text-xs text-slate-400">Identities masked — messages are private to you.</p>
            {recentSupporters.length === 0 ? (
              <p className="mt-2 text-sm text-slate-500">No supporter messages yet — share your page to get started.</p>
            ) : (
              <Table>
                <TableHeader><TableRow>
                  <TableHead>Supporter</TableHead><TableHead>Message</TableHead><TableHead>Amount</TableHead>
                </TableRow></TableHeader>
                <TableBody>
                  {recentSupporters.map((o) => (
                    <TableRow key={o.id}>
                      <TableCell className="text-xs">{maskBuyer(o.buyer_name, o.buyer_phone)}</TableCell>
                      <TableCell className="max-w-xs text-xs italic text-slate-600">“{String(o.metadata?.supporter_message)}”</TableCell>
                      <TableCell className="text-xs font-medium">{o.amount} {o.currency} <Badge variant="secondary">{o.status}</Badge></TableCell>
                    </TableRow>
                  ))}
                </TableBody>
              </Table>
            )}
          </CardContent></Card>
        </>
      )}
    </div>
  );
};

export default CreatorOverview;
