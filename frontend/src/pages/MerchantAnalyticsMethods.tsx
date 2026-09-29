import { useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Cell, Legend, Pie, PieChart, ResponsiveContainer, Tooltip } from "recharts";
import { listMyApps } from "@/lib/merchantApi";
import { fetchMerchantMethods } from "@/lib/merchantAnalyticsApi";
import { AppFilter, AnalyticsSubNav, EnvToggle } from "@/pages/merchantAnalyticsCommon";
import { DateRangePicker, moneyText, presetRange, rateText } from "@/pages/analyticsCommon";

const COLORS = ["#0f766e", "#2563eb", "#9333ea", "#dc2626", "#d97706", "#64748b"];

const MerchantAnalyticsMethods = () => {
  const { orgId = "" } = useParams();
  const initial = presetRange("30d");
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);
  const [env, setEnv] = useState<"live" | "sandbox">("live");
  const [appId, setAppId] = useState("");

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const methodsQuery = useQuery({
    queryKey: ["merchant", "analytics", "methods", orgId, from, to, env, appId],
    queryFn: () => fetchMerchantMethods(orgId, { from, to, environment: env, app_id: appId || undefined }),
    staleTime: 30_000,
  });
  const rows = methodsQuery.data ?? [];
  const pieData = rows.map((r) => ({ name: r.channel, value: r.tx_count }));

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Payment methods</h2>
          <p className="mt-1 text-sm text-slate-500">Network mix by transactions, volume and success</p>
          <AnalyticsSubNav orgId={orgId} />
        </div>
        <div className="flex flex-wrap items-center gap-2">
          <EnvToggle env={env} onChange={setEnv} />
          <AppFilter apps={appsQuery.data ?? []} value={appId} onChange={setAppId} />
          <DateRangePicker from={from} to={to} onChange={(f, t) => { setFrom(f); setTo(t); }} />
        </div>
      </div>

      {methodsQuery.isLoading ? (
        <Card className="mt-6"><CardContent className="p-4"><Skeleton className="h-40 w-full" /></CardContent></Card>
      ) : methodsQuery.error ? (
        <p className="mt-6 text-sm text-red-600">Unable to load methods.</p>
      ) : (
        <>
          <Card className="mt-6"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Share by transactions</h3>
            {pieData.length === 0 ? (
              <p className="mt-2 text-sm text-slate-500">No payments in range.</p>
            ) : (
              <div className="mt-2 h-64">
                <ResponsiveContainer width="100%" height="100%">
                  <PieChart>
                    <Pie data={pieData} dataKey="value" nameKey="name" outerRadius={90} label>
                      {pieData.map((_, i) => (
                        <Cell key={i} fill={COLORS[i % COLORS.length]} />
                      ))}
                    </Pie>
                    <Tooltip />
                    <Legend />
                  </PieChart>
                </ResponsiveContainer>
              </div>
            )}
          </CardContent></Card>

          <Card className="mt-4"><CardContent className="overflow-x-auto p-0">
            <Table>
              <TableHeader><TableRow>
                <TableHead>Network</TableHead><TableHead>Transactions</TableHead>
                <TableHead>Volume</TableHead><TableHead>Success</TableHead>
              </TableRow></TableHeader>
              <TableBody>
                {rows.map((r) => (
                  <TableRow key={r.channel}>
                    <TableCell className="font-medium">{r.channel}</TableCell>
                    <TableCell>{r.tx_count}</TableCell>
                    <TableCell className="text-xs">{moneyText(r.volume_by_currency)}</TableCell>
                    <TableCell>{rateText(r.success_rate)}</TableCell>
                  </TableRow>
                ))}
                {rows.length === 0 && (
                  <TableRow><TableCell colSpan={4} className="text-center text-slate-500">No payments in range.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent></Card>
        </>
      )}
    </div>
  );
};

export default MerchantAnalyticsMethods;
