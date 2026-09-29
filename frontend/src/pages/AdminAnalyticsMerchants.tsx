import { useState } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Copy } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  Bar, BarChart, CartesianGrid, Legend, ResponsiveContainer, Tooltip, XAxis, YAxis,
} from "recharts";
import { toast } from "@/components/ui/sonner";
import {
  fetchChurn, fetchDormant, fetchFunnel, fetchTopMerchants,
} from "@/lib/analyticsApi";
import { CsvButton, DateRangePicker, moneyText, presetRange, rateText } from "@/pages/analyticsCommon";

const AdminAnalyticsMerchants = () => {
  const initial = presetRange("30d");
  const [from, setFrom] = useState(initial.from);
  const [to, setTo] = useState(initial.to);
  const [sort, setSort] = useState("tx_count");

  const topQuery = useQuery({
    queryKey: ["admin", "analytics", "top", from, to, sort],
    queryFn: () => fetchTopMerchants({ from, to, sort, per_page: 50 }),
    staleTime: 30_000,
  });
  const funnelQuery = useQuery({
    queryKey: ["admin", "analytics", "funnel", from, to],
    queryFn: () => fetchFunnel({ from, to, granularity: "day" }),
    staleTime: 30_000,
  });
  const dormantQuery = useQuery({
    queryKey: ["admin", "analytics", "dormant"],
    queryFn: () => fetchDormant(),
    staleTime: 60_000,
  });
  const churnQuery = useQuery({
    queryKey: ["admin", "analytics", "churn"],
    queryFn: () => fetchChurn(),
    staleTime: 60_000,
  });

  const copyEmail = (email?: string) => {
    if (!email) {
      toast.error("No contact email on file.");
      return;
    }
    navigator.clipboard.writeText(email).then(() => toast.success("Contact email copied."));
  };

  const funnel = funnelQuery.data;
  const funnelRows = (funnel?.series ?? []).map((pt) => ({
    bucket: pt.bucket,
    Signups: pt.signups,
    Verified: pt.emails_verified,
    Orgs: pt.orgs_created,
    Submitted: pt.kyc_submitted,
    KYCVerified: pt.kyc_verified,
    FirstLive: pt.first_live_txn,
  }));

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Merchants</h2>
          <p className="mt-1 text-sm text-slate-500">Top tenants, signup funnel, dormant and churn risk</p>
        </div>
        <DateRangePicker from={from} to={to} onChange={(f, t) => { setFrom(f); setTo(t); }} />
      </div>

      <Tabs defaultValue="top" className="mt-4">
        <TabsList className="flex-wrap">
          <TabsTrigger value="top">Top</TabsTrigger>
          <TabsTrigger value="signups">Signups funnel</TabsTrigger>
          <TabsTrigger value="dormant">Dormant ({(dormantQuery.data ?? []).length})</TabsTrigger>
          <TabsTrigger value="churn">Churn risk ({(churnQuery.data ?? []).length})</TabsTrigger>
        </TabsList>

        <TabsContent value="top">
          <div className="mb-3 flex items-center gap-2">
            <select
              aria-label="Sort merchants"
              className="h-9 rounded-md border border-slate-300 px-2 text-sm"
              value={sort}
              onChange={(e) => setSort(e.target.value)}
            >
              <option value="tx_count">Sort: transactions</option>
              <option value="tpv">Sort: TPV (TZS)</option>
              <option value="revenue">Sort: revenue (TZS)</option>
              <option value="success_rate">Sort: success rate</option>
            </select>
            <CsvButton report="top-merchants" query={{ from, to, sort }} />
          </div>
          <Card><CardContent className="overflow-x-auto p-0">
            {topQuery.isLoading ? (
              <div className="p-4"><Skeleton className="h-40 w-full" /></div>
            ) : topQuery.error ? (
              <p className="p-4 text-sm text-red-600">Unable to load top merchants.</p>
            ) : (
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Organization</TableHead>
                    <TableHead>TPV</TableHead>
                    <TableHead>Revenue</TableHead>
                    <TableHead>Txns</TableHead>
                    <TableHead>Success</TableHead>
                    <TableHead>Refunds</TableHead>
                    <TableHead>Trend</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {(topQuery.data?.items ?? []).map((m) => (
                    <TableRow key={m.org_id}>
                      <TableCell>
                        <Link to={`/admin/orgs/${m.org_id}`} className="font-medium text-blue-600 hover:underline">{m.org_name}</Link>
                        <br /><Badge variant="secondary">{m.kyc_status}</Badge>
                      </TableCell>
                      <TableCell className="text-xs">{moneyText(m.tpv_by_currency)}</TableCell>
                      <TableCell className="text-xs">{moneyText(m.revenue_by_currency)}</TableCell>
                      <TableCell>{m.tx_count}</TableCell>
                      <TableCell>{rateText(m.success_rate)}</TableCell>
                      <TableCell>{rateText(m.refund_rate)}</TableCell>
                      <TableCell className="text-xs">
                        {m.trend_pct === undefined || m.trend_pct === null ? "—" : (
                          <span className={m.trend_pct >= 0 ? "text-emerald-600" : "text-rose-600"}>
                            {m.trend_pct >= 0 ? "▲" : "▼"} {Math.abs(m.trend_pct).toFixed(1)}%
                          </span>
                        )}
                      </TableCell>
                    </TableRow>
                  ))}
                  {(topQuery.data?.items ?? []).length === 0 && (
                    <TableRow><TableCell colSpan={7} className="text-center text-slate-500">No merchants with live orders in range.</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            )}
          </CardContent></Card>
        </TabsContent>

        <TabsContent value="signups">
          <div className="mb-3 flex items-center gap-2">
            <CsvButton report="signups" query={{ from, to, granularity: "day" }} />
            {funnel && (
              <span className="text-xs text-slate-500">
                Signup→live conversion {funnel.totals.signup_to_live_pct === undefined || funnel.totals.signup_to_live_pct === null
                  ? "—" : `${funnel.totals.signup_to_live_pct.toFixed(1)}%`}
              </span>
            )}
          </div>
          <Card><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Funnel per day</h3>
            <div className="mt-2 h-64">
              <ResponsiveContainer width="100%" height="100%">
                <BarChart data={funnelRows} margin={{ top: 8, right: 8, left: 0, bottom: 0 }}>
                  <CartesianGrid strokeDasharray="3 3" />
                  <XAxis dataKey="bucket" tick={{ fontSize: 11 }} minTickGap={24} />
                  <YAxis tick={{ fontSize: 11 }} />
                  <Tooltip />
                  <Legend />
                  <Bar dataKey="Signups" fill="#94a3b8" />
                  <Bar dataKey="Verified" fill="#60a5fa" />
                  <Bar dataKey="Submitted" fill="#f59e0b" />
                  <Bar dataKey="KYCVerified" fill="#10b981" />
                  <Bar dataKey="FirstLive" fill="#0f766e" />
                </BarChart>
              </ResponsiveContainer>
            </div>
            {funnel && (
              <div className="mt-3 grid grid-cols-2 gap-2 text-xs text-slate-600 sm:grid-cols-3">
                {Object.entries(funnel.median_hours_between_steps ?? {}).map(([k, v]) => (
                  <p key={k}>{k.replace(/_/g, " ")}: <strong>{v === null || v === undefined ? "—" : `${v.toFixed(1)}h`}</strong></p>
                ))}
              </div>
            )}
          </CardContent></Card>
        </TabsContent>

        <TabsContent value="dormant">
          <div className="mb-3"><CsvButton report="dormant" /></div>
          <Card><CardContent className="overflow-x-auto p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Organization</TableHead>
                  <TableHead>Reason</TableHead>
                  <TableHead>Contact</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(dormantQuery.data ?? []).map((m) => (
                  <TableRow key={m.org_id}>
                    <TableCell>
                      <Link to={`/admin/orgs/${m.org_id}`} className="font-medium text-blue-600 hover:underline">{m.org_name}</Link>
                    </TableCell>
                    <TableCell className="text-xs text-slate-600">{m.reason}</TableCell>
                    <TableCell>
                      <Button size="sm" variant="outline" onClick={() => copyEmail(m.contact_email)}>
                        <Copy className="h-3.5 w-3.5 mr-1" /> Copy email
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
                {(dormantQuery.data ?? []).length === 0 && !dormantQuery.isLoading && (
                  <TableRow><TableCell colSpan={3} className="text-center text-slate-500">No dormant merchants.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent></Card>
        </TabsContent>

        <TabsContent value="churn">
          <div className="mb-3"><CsvButton report="churn-risk" /></div>
          <Card><CardContent className="overflow-x-auto p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Organization</TableHead>
                  <TableHead>Why flagged</TableHead>
                  <TableHead>Drop</TableHead>
                  <TableHead>Contact</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {(churnQuery.data ?? []).map((m) => (
                  <TableRow key={m.org_id}>
                    <TableCell>
                      <Link to={`/admin/orgs/${m.org_id}`} className="font-medium text-blue-600 hover:underline">{m.org_name}</Link>
                    </TableCell>
                    <TableCell className="max-w-md text-xs text-slate-600">{m.reason}</TableCell>
                    <TableCell className="font-bold text-rose-600">
                      {m.drop_pct === undefined || m.drop_pct === null ? "—" : `${m.drop_pct.toFixed(0)}%`}
                    </TableCell>
                    <TableCell>
                      <Button size="sm" variant="outline" onClick={() => copyEmail(m.contact_email)}>
                        <Copy className="h-3.5 w-3.5 mr-1" /> Copy email
                      </Button>
                    </TableCell>
                  </TableRow>
                ))}
                {(churnQuery.data ?? []).length === 0 && !churnQuery.isLoading && (
                  <TableRow><TableCell colSpan={4} className="text-center text-slate-500">No churn-risk merchants.</TableCell></TableRow>
                )}
              </TableBody>
            </Table>
          </CardContent></Card>
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default AdminAnalyticsMerchants;
