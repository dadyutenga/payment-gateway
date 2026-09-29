import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { fetchProviders } from "@/lib/analyticsApi";
import { CsvButton, DateRangePicker, moneyText, presetRange, rateText } from "@/pages/analyticsCommon";

const AdminAnalyticsProviders = () => {
  const initial = presetRange("30d");
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);

  const providersQuery = useQuery({
    queryKey: ["admin", "analytics", "providers", from, to],
    queryFn: () => fetchProviders({ from, to }),
    staleTime: 30_000,
  });
  const providers = providersQuery.data ?? [];

  const shareRows = useMemo(
    () => providers.map((p) => ({ provider: p.provider, tx_count: p.tx_count })),
    [providers],
  );

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Providers</h2>
          <p className="mt-1 text-sm text-slate-500">Live payments only · latency in milliseconds</p>
        </div>
        <DateRangePicker from={from} to={to} onChange={(f, t) => { setFrom(f); setTo(t); }} />
      </div>

      {providersQuery.isLoading ? (
        <Card className="mt-6"><CardContent className="p-4"><Skeleton className="h-40 w-full" /></CardContent></Card>
      ) : providersQuery.error ? (
        <p className="mt-6 text-sm text-red-600">Unable to load provider stats right now.</p>
      ) : (
        <>
          <Card className="mt-6"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Transaction share by provider (count)</h3>
            <div className="mt-2 h-56">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={shareRows} layout="vertical" margin={{ top: 8, right: 8, left: 40, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis type="number" tick={{ fontSize: 11 }} />
                  <YAxis type="category" dataKey="provider" tick={{ fontSize: 11 }} width={90} />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="tx_count" name="Transactions" fill="#0f766e" />
                </BarChart>
              </ResponsiveContainer>
            </div>
          </CardContent></Card>

          <Card className="mt-4"><CardContent className="overflow-x-auto p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Provider</TableHead>
                  <TableHead>Volume (paid)</TableHead>
                  <TableHead>Txns</TableHead>
                  <TableHead>Success</TableHead>
                  <TableHead>Latency p50/p95</TableHead>
                  <TableHead>Failures 1h/24h</TableHead>
                  <TableHead>Channels</TableHead>
                  <TableHead>Top failure</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {providers.map((p) => {
                  const topFailure = Object.entries(p.failures_by_code ?? {}).sort((a, b) => b[1] - a[1])[0];
                  return (
                    <TableRow key={p.provider}>
                      <TableCell className="font-medium">{p.provider}</TableCell>
                      <TableCell className="text-xs">{moneyText(p.volume_by_currency)}</TableCell>
                      <TableCell>{p.tx_count}</TableCell>
                      <TableCell>{rateText(p.success_rate)}</TableCell>
                      <TableCell className="text-xs text-slate-600">
                        {p.median_latency_ms === null || p.median_latency_ms === undefined
                          ? "—"
                          : `${Math.round(p.median_latency_ms)} / ${p.p95_latency_ms === null || p.p95_latency_ms === undefined ? "—" : Math.round(p.p95_latency_ms)} ms`}
                      </TableCell>
                      <TableCell>
                        <span className={p.failures_1h > 0 ? "font-bold text-rose-600" : ""}>{p.failures_1h}</span>
                        {" / "}{p.failures_24h}
                      </TableCell>
                      <TableCell className="max-w-xs truncate text-xs text-slate-500">
                        {Object.entries(p.channel_split ?? {}).map(([c, n]) => `${c} ${n}`).join(" · ") || "—"}
                      </TableCell>
                      <TableCell className="text-xs">
                        {topFailure ? <><Badge variant="outline">{topFailure[0]}</Badge> <span className="text-slate-500">×{topFailure[1]}</span></> : "—"}
                      </TableCell>
                    </TableRow>
                  );
                })}
                {providers.length === 0 && (
                  <TableRow><TableCell colSpan={8} className="text-center text-slate-500">No provider activity in range.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent></Card>
        </>
      )}
    </div>
  );
};

export default AdminAnalyticsProviders;
