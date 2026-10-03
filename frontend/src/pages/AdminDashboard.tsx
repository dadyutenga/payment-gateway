import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowRight } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { getPlatformStats, listKYCQueue } from "@/lib/adminOrgApi";
import DashboardGreeting from "@/components/DashboardGreeting";
import OutstandingTasks from "@/components/OutstandingTasks";
import { ClipboardCheck } from "lucide-react";

// Operator home dashboard: tenant counts, verification funnel, payout
// workload, and the actionable KYC queue — doors to every admin section.
const AdminDashboard = () => {
  const statsQuery = useQuery({ queryKey: ["admin", "stats"], queryFn: () => getPlatformStats(), staleTime: 30_000 });
  const queueQuery = useQuery({
    queryKey: ["admin", "kyc-queue", "submitted"],
    queryFn: () => listKYCQueue("submitted"),
    staleTime: 30_000,
  });
  const stats = statsQuery.data;
  const queue = (queueQuery.data ?? []).slice(0, 5);
  const loading = statsQuery.isLoading;

  const cards: { label: string; value: number | undefined; to: string; link: string }[] = [
    { label: "Customers", value: stats?.customers, to: "/admin/kyc", link: "Review queue →" },
    { label: "Organizations", value: stats?.organizations, to: "/admin/kyc", link: "Review queue →" },
    { label: "Apps", value: stats?.apps, to: "/admin/payments/apps", link: "View apps →" },
    { label: "KYC awaiting review", value: stats?.kyc_awaiting_review, to: "/admin/kyc", link: "Review now →" },
    { label: "Operators", value: stats?.admins, to: "/admin/payments/providers", link: "Providers →" },
    {
      label: "Withdrawals requested",
      value: stats?.withdrawals_by_status?.requested,
      to: "/admin/payments/withdrawals",
      link: "Record payouts →",
    },
  ];

  return (
    <div>
      <DashboardGreeting space="admin" description="Review platform health, verification workload, and payout operations from one place." />

      {(stats?.kyc_awaiting_review ?? 0) > 0 && <OutstandingTasks items={[{ icon: ClipboardCheck, label: "Review pending KYC", status: `${stats?.kyc_awaiting_review} awaiting review`, action: { label: "Open review queue", to: "/admin/kyc" } }]} />}

      {loading ? (
        <div className="mt-6 grid gap-3 sm:grid-cols-3">
          {[0, 1, 2, 3, 4, 5].map((i) => (
            <Card key={i}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}
        </div>
      ) : stats ? (
        <>
          <div className="mt-6 grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
            {cards.map((c) => (
              <Card key={c.label}>
                <CardContent className="p-4">
                  <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">{c.label}</p>
                  <p className="mt-1 text-2xl font-extrabold text-slate-900">{c.value ?? 0}</p>
                  <Link to={c.to} className="mt-2 inline-block text-xs text-blue-600 hover:underline">{c.link}</Link>
                </CardContent>
              </Card>
            ))}
          </div>

          <div className="mt-6 grid gap-4 lg:grid-cols-2">
            <Card>
              <CardContent className="p-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-sm font-bold text-slate-800">Organizations by verification</h3>
                  <Link to="/admin/kyc" className="text-xs text-blue-600 hover:underline">KYC review <ArrowRight className="inline h-3 w-3" /></Link>
                </div>
                <div className="mt-3 space-y-2">
                  {["pending", "submitted", "verified", "rejected"].map((s) => (
                    <div key={s} className="flex items-center justify-between text-sm">
                      <span className="flex items-center gap-2">
                        <Badge variant="secondary">{s}</Badge>
                      </span>
                      <span className="font-bold text-slate-900">{stats.orgs_by_kyc?.[s] ?? 0}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
            <Card>
              <CardContent className="p-4">
                <div className="flex items-center justify-between">
                  <h3 className="text-sm font-bold text-slate-800">Withdrawals by status</h3>
                  <Link to="/admin/payments/withdrawals" className="text-xs text-blue-600 hover:underline">Withdrawals <ArrowRight className="inline h-3 w-3" /></Link>
                </div>
                <div className="mt-3 space-y-2">
                  {(Object.keys(stats.withdrawals_by_status ?? {}).length === 0) && (
                    <p className="text-sm text-slate-500">No withdrawals yet.</p>
                  )}
                  {Object.entries(stats.withdrawals_by_status ?? {}).map(([s, n]) => (
                    <div key={s} className="flex items-center justify-between text-sm">
                      <span className="flex items-center gap-2">
                        <Badge variant="secondary">{s}</Badge>
                      </span>
                      <span className="font-bold text-slate-900">{n}</span>
                    </div>
                  ))}
                </div>
              </CardContent>
            </Card>
          </div>

          <div className="mt-6 flex items-center justify-between">
            <h3 className="text-sm font-bold text-slate-800">KYC queue — oldest first</h3>
            <Link to="/admin/kyc" className="text-xs text-blue-600 hover:underline">Open review queue →</Link>
          </div>
          <Card className="mt-2">
            <CardContent className="p-0">
              <Table>
                <TableHeader>
                  <TableRow>
                    <TableHead>Organization</TableHead>
                    <TableHead>Business</TableHead>
                    <TableHead>Submitted</TableHead>
                  </TableRow>
                </TableHeader>
                <TableBody>
                  {queue.map((item) => (
                    <TableRow key={item.org_id}>
                      <TableCell>
                        <Link to="/admin/kyc" className="font-medium text-blue-600 hover:underline">{item.org_name}</Link>
                      </TableCell>
                      <TableCell className="text-xs text-slate-500">{item.business_name || "—"}</TableCell>
                      <TableCell className="text-xs">{item.submitted_at ? new Date(item.submitted_at).toLocaleString() : "—"}</TableCell>
                    </TableRow>
                  ))}
                  {queue.length === 0 && (
                    <TableRow><TableCell colSpan={3} className="text-center text-slate-500">Queue is clear — nothing awaiting review.</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent>
          </Card>
        </>
      ) : (
        <p className="mt-6 text-sm text-red-600">Unable to load platform stats right now.</p>
      )}
    </div>
  );
};

export default AdminDashboard;
