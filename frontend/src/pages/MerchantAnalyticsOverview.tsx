import { useMemo, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Legend, Line, LineChart,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { listMyApps } from "@/lib/merchantApi";
import { fetchMerchantApps, fetchMerchantOverview } from "@/lib/merchantAnalyticsApi";
import { AppFilter, AnalyticsSubNav, EnvToggle, SandboxGuide } from "@/pages/merchantAnalyticsCommon";
import { DateRangePicker, DeltaBadge, moneyText, presetRange, rateText } from "@/pages/analyticsCommon";

const MerchantAnalyticsOverview = () => {
  const { orgId = "" } = useParams();
  const initial = presetRange("30d");
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);
  const [env, setEnv] = useState<"live" | "sandbox">("live");
  const [appId, setAppId] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const appsTableQuery = useQuery({
    queryKey: ["merchant", "analytics", "apps", orgId, from, to, env],
    queryFn: () => fetchMerchantApps(orgId, { from, to, environment: env }),
    staleTime: 30_000,
  });
  const overviewQuery = useQuery({
    queryKey: ["merchant", "analytics", "overview", orgId, from, to, env, appId],
    queryFn: () => fetchMerchantOverview(orgId, { from, to, granularity: "day", environment: env, app_id: appId || undefined }),
    staleTime: 30_000,
  });
  const data = overviewQuery.data;

  const chartRows = useMemo(() => {
    return (data?.series ?? []).map((pt) => {
      const row: Record<string, number | string | null> = {
        bucket: pt.bucket,
        tx_count: pt.tx_count,
        success_rate: pt.success_rate === null || pt.success_rate === undefined ? null : pt.success_rate * 100,
      };
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

  const COLORS = ["#0f766e", "#2563eb", "#9333ea", "#dc2626", "#d97706"];
  const showGuide = !overviewQuery.isLoading && !overviewQuery.error && env === "sandbox" && (data?.tx_count ?? 0) === 0;

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Analytics</h2>
          <p className="mt-1 text-sm text-slate-500">Revenue, volume and quality · Africa/Dar_es_Salaam</p>
          <AnalyticsSubNav orgId={orgId} />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EnvToggle env={env} onChange={setEnv} />
          <AppFilter apps={appsQuery.data ?? []} value={appId} onChange={setAppId} />
          <DateRangePicker from={from} to={to} onChange={(f, t) => { setFrom(f); setTo(t); }} />
        </div>
      </div>

      {showGuide && <SandboxGuide orgId={orgId} />}

      {overviewQuery.isLoading ? (
        <div className="mt-6 grid gap-3 sm:grid-cols-3">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Card key={i}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}
        </div>
      ) : overviewQuery.error ? (
        <p className="mt-6 text-sm text-red-600">Unable to load analytics right now.</p>
      ) : data && !showGuide && (
        <>
          <div className="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Revenue (net of fees &amp; refunds)</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{moneyText(data.revenue)}</p>
              <DeltaBadge pct={data.previous_period.revenue_pct_change} />
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Gross volume</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{moneyText(data.tpv)}</p>
              <DeltaBadge pct={data.previous_period.tpv_pct_change} />
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Transactions</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{data.tx_count.toLocaleString()}</p>
              <DeltaBadge pct={data.previous_period.tx_pct_change} />
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Success / abandonment</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{rateText(data.success_rate)}</p>
              <p className="text-xs text-slate-400">abandonment {rateText(data.abandonment_rate)}</p>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Median time to pay</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">
                {data.median_ttp_s === null || data.median_ttp_s === undefined ? "—" : `${Math.round(data.median_ttp_s)}s`}
              </p>
              <p className="text-xs text-slate-400">
                avg order {moneyText(data.avg_order_value)}
              </p>
            </CardContent></Card>
          </div>

          <div className="mt-6 grid gap-4 lg:grid-cols-2">
            <Card><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">Revenue over time (per currency)</h3>
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
                        stroke={COLORS[i % COLORS.length]} fill={COLORS[i % COLORS.length]} fillOpacity={0.25} />
                    ))}
                  </AreaChart>
                </ResponsiveContainer>
              </div>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">Transactions</h3>
              <div className="mt-2 h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <BarChart data={chartRows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                    <CartesianGrid strokeDasharray="3 3" />
                    <XAxis dataKey="bucket" tick={{ fontSize: 11 }} minTickGap={24} />
                    <YAxis tick={{ fontSize: 11 }} />
                    <Tooltip />
                    <Bar dataKey="tx_count" name="Transactions" fill="#0f766e" />
                  </BarChart>
                </ResponsiveContainer>
              </div>
            </CardContent></Card>
          </div>

          <Card className="mt-4"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Success rate trend (%)</h3>
            <div className="mt-2 h-56">
              <ResponsiveContainer width="100%" height="100%">
                <LineChart data={chartRows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="bucket" tick={{ fontSize: 11 }} minTickGap={24} />
                  <YAxis tick={{ fontSize: 11 }} domain={[0, 100]} />
                  <Tooltip />
                  <Line type="monotone" dataKey="success_rate" name="Success %" stroke="#2563eb" dot={false} connectNulls />
                </LineChart>
              </ResponsiveContainer>
            </div>
          </CardContent></Card>

          <Card className="mt-4"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Apps comparison</h3>
            {appsTableQuery.isLoading ? (
              <p className="mt-2 text-sm text-slate-500">Loading apps…</p>
            ) : (
              <Table>
                <TableHeader><TableRow>
                  <TableHead>App</TableHead><TableHead>Volume</TableHead>
                  <TableHead>Txns</TableHead><TableHead>Success</TableHead><TableHead>Refunds</TableHead>
                </TableRow></TableHeader>
                <TableBody>
                  {(appsTableQuery.data ?? []).map((a) => (
                    <TableRow key={a.app_id}>
                      <TableCell className="font-medium">{a.name}</TableCell>
                      <TableCell className="text-xs">{moneyText(a.tpv_by_currency)}</TableCell>
                      <TableCell>{a.tx_count}</TableCell>
                      <TableCell>{rateText(a.success_rate)}</TableCell>
                      <TableCell>{rateText(a.refund_rate)}</TableCell>
                    </TableRow>
                  ))}
                  {(appsTableQuery.data ?? []).length === 0 && (
                    <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No apps yet.</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            )}
          </CardContent></Card>
        </>
      )}
    </div>
  );
};

export default MerchantAnalyticsOverview;
