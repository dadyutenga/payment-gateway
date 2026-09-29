import { useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { listMyApps } from "@/lib/merchantApi";
import { fetchMerchantCustomers } from "@/lib/merchantAnalyticsApi";
import { AppFilter, AnalyticsSubNav, EnvToggle } from "@/pages/merchantAnalyticsCommon";
import { DateRangePicker, useFilterParams, rateText } from "@/pages/analyticsCommon";

const MerchantAnalyticsCustomers = () => {
  const { orgId = "" } = useParams();
  const { from, to, env, setRange, setEnv } = useFilterParams();
  const [appId, setAppId] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const customersQuery = useQuery({
    queryKey: ["merchant", "analytics", "customers", orgId, from, to, env, appId],
    queryFn: () => fetchMerchantCustomers(orgId, { from, to, granularity: "day", environment: env, app_id: appId || undefined }),
    staleTime: 30_000,
  });
  const data = customersQuery.data;
  const chartRows = (data?.series ?? []).map((pt) => ({
    bucket: pt.bucket, New: pt.new_payers, Returning: pt.returning_payers,
  }));

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Customers</h2>
          <p className="mt-1 text-sm text-slate-500">Repeat rate and masked top payers — full numbers never shown</p>
          <AnalyticsSubNav orgId={orgId} />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EnvToggle env={env} onChange={setEnv} />
          <AppFilter apps={appsQuery.data ?? []} value={appId} onChange={setAppId} />
          <DateRangePicker from={from} to={to} onChange={setRange} />
        </div>
      </div>

      {customersQuery.isLoading ? (
        <Card className="mt-6"><CardContent className="p-4"><Skeleton className="h-40 w-full" /></CardContent></Card>
      ) : customersQuery.error ? (
        <p className="mt-6 text-sm text-red-600">Unable to load customer analytics.</p>
      ) : data && (
        <>
          <div className="mt-6 grid gap-3 sm:grid-cols-3">
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Repeat-customer rate</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{rateText(data.repeat_rate)}</p>
              <p className="text-xs text-slate-400">2+ payments / all payers</p>
            </CardContent></Card>
            <Card><CardContent className="p-4">
              <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Payers</p>
              <p className="mt-1 text-xl font-extrabold text-slate-900">{data.payers_total}</p>
              <p className="text-xs text-slate-400">{data.payers_repeat} returning</p>
            </CardContent></Card>
          </div>

          <Card className="mt-4"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">New vs returning payers</h3>
            <div className="mt-2 h-64">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={chartRows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="bucket" tick={{ fontSize: 11 }} minTickGap={24} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="New" stackId="payers" fill="#2563eb" />
                  <Bar dataKey="Returning" stackId="payers" fill="#0f766e" />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </CardContent></Card>

          <Card className="mt-4"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Top payers (masked)</h3>
            {data.payer_detail_hidden ? (
              <p className="mt-2 text-sm text-slate-500">Your role hides payer-level detail — aggregates above still apply.</p>
            ) : data.top_payers.length === 0 ? (
              <p className="mt-2 text-sm text-slate-500">No repeat payers in range.</p>
            ) : (
              <Table>
                <TableHeader><TableRow>
                  <TableHead>Payer</TableHead><TableHead>Payments</TableHead>
                  <TableHead>Volume</TableHead><TableHead>Last payment</TableHead>
                </TableRow></TableHeader>
                <TableBody>
                  {data.top_payers.map((p) => (
                    <TableRow key={`${p.masked_id}-${p.currency}`}>
                      <TableCell className="font-mono text-xs">{p.masked_id}</TableCell>
                      <TableCell>{p.tx_count}</TableCell>
                      <TableCell className="text-xs">{p.volume} {p.currency}</TableCell>
                      <TableCell className="text-xs">{p.last_txn_at ? new Date(p.last_txn_at).toLocaleString() : "—"}</TableCell>
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

export default MerchantAnalyticsCustomers;
