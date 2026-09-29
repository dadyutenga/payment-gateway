import { useMemo } from "react";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Area, AreaChart, Bar, BarChart, CartesianGrid, Legend, Line, LineChart,
  ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { fetchOverview } from "@/lib/analyticsApi";
import { CsvButton, DateRangePicker, DeltaBadge, moneyText, useFilterParams, rateText } from "@/pages/analyticsCommon";

const AdminAnalyticsOverview = () => {
  const { from, to, setRange } = useFilterParams();

  const overviewQuery = useQuery({
    queryKey: ["admin", "analytics", "overview", from, to],
    queryFn: () => fetchOverview({ from, to, granularity: "day" }),
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

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Analytics overview</h2>
          <p className="mt-1 text-sm text-slate-500">Live payments only · timezone Africa/Dar_es_Salaam</p>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <DateRangePicker from={from} to={to} onChange={setRange} />
          <CsvButton report="top-merchants" query={{ from, to }} />
        </div>
      </div>

      {overviewQuery.isLoading ? (
        <div className="mt-6 grid gap-3 sm:grid-cols-3">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Card key={i}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}
        </div>
      ) : overviewQuery.error ? (
        <p className="mt-6 text-sm text-red-600">Unable to load overview right now.</p>
      ) : data && (
        <>
          <div className="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">TPV (paid, gross)</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{moneyText(data.tpv)}</p>
              <DeltaBadge pct={data.previous_period.tpv_pct_change} />
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Revenue (fees net of refunds)</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{moneyText(data.revenue)}</p>
              <DeltaBadge pct={data.previous_period.revenue_pct_change} />
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Transactions</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{data.tx_count.toLocaleString()}</p>
              <DeltaBadge pct={data.previous_period.tx_pct_change} />
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Success rate</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{rateText(data.success_rate)}</p>
              <p className="text-xs text-slate-400">abandonment {rateText(data.abandonment_rate)} · pending excluded</p>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Median time to pay</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">
                {data.median_ttp_s === null || data.median_ttp_s === undefined ? "—" : `${Math.round(data.median_ttp_s)}s`}
              </p>
              <p className="text-xs text-slate-400">
                p90 {data.p90_ttp_s === null || data.p90_ttp_s === undefined ? "—" : `${Math.round(data.p90_ttp_s)}s`}
              </p>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Merchants &amp; signups</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{data.active_merchants} active</p>
              <p className="text-xs text-slate-400">{data.new_signups} signups · {data.new_orgs} orgs in range</p>
            </CardContent></Card>
          </div>

          <div className="mt-6 grid gap-4 lg:grid-cols-2">
            <Card><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">Volume (paid gross per currency)</h3>
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
        </>
      )}
    </div>
  );
};

export default AdminAnalyticsOverview;
