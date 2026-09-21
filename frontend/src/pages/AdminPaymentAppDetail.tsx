import { useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { ArrowLeft } from "lucide-react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import {
  getAppBalance,
  listLedgerEntries,
  listPaymentApps,
  listWithdrawals,
  searchPaymentOrders,
} from "@/lib/adminApi";

const LEDGER_ENTRY_STYLES: Record<string, string> = {
  payment_credit: "bg-emerald-50 text-emerald-700 ring-emerald-600/15",
  platform_fee_debit: "bg-amber-50 text-amber-700 ring-amber-600/15",
  withdrawal_debit: "bg-rose-50 text-rose-700 ring-rose-600/15",
  withdrawal_reversal_credit: "bg-sky-50 text-sky-700 ring-sky-600/15",
  adjustment_credit: "bg-emerald-50 text-emerald-700 ring-emerald-600/15",
  adjustment_debit: "bg-rose-50 text-rose-700 ring-rose-600/15",
};

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : "—";
}

function formatMoney(value: string | undefined, currency: string) {
  if (!value) return "—";
  const n = Number(value);
  return `${Number.isFinite(n) ? n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : value} ${currency}`;
}

// A negative available balance is a real, meaningful state (a refund
// reversed more than was ever withdrawn) rather than an error.
function isNegativeBalance(value: string | undefined) {
  return typeof value === "string" && Number(value) < 0;
}

function formatFee(app: { fee_type: string; fee_percent: string; fee_fixed: string }) {
  if (app.fee_type === "fixed") return `Fixed ${app.fee_fixed}`;
  if (app.fee_type === "hybrid") return `${app.fee_percent}% + ${app.fee_fixed}`;
  return `${app.fee_percent}%`;
}

const AdminPaymentAppDetail = () => {
  const { id } = useParams<{ id: string }>();
  const [orderStatus, setOrderStatus] = useState("");

  // No single-app GET endpoint exists — the app list is small, so find
  // this app client-side from the same list the apps grid already fetches.
  const appsQuery = useQuery({
    queryKey: ["admin", "payments-apps"],
    queryFn: () => listPaymentApps().then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const app = appsQuery.data?.find((a) => a.id === id);

  const balanceQuery = useQuery({
    queryKey: ["admin", "payment-app-balance", id],
    queryFn: () => getAppBalance(id as string),
    enabled: !!id,
    staleTime: 30_000,
  });
  const balance = balanceQuery.data;

  const ordersQuery = useQuery({
    queryKey: ["admin", "payment-app-orders", id, orderStatus],
    queryFn: () =>
      searchPaymentOrders({ app_id: id, status: orderStatus || undefined }).then((r) => (Array.isArray(r.items) ? r.items : [])),
    enabled: !!id,
    staleTime: 15_000,
  });
  const orders = ordersQuery.data ?? [];

  const ledgerQuery = useQuery({
    queryKey: ["admin", "payment-app-ledger", id],
    queryFn: () => listLedgerEntries(id).then((r) => (Array.isArray(r.items) ? r.items : [])),
    enabled: !!id,
    staleTime: 15_000,
  });
  const ledgerEntries = ledgerQuery.data ?? [];

  const withdrawalsQuery = useQuery({
    queryKey: ["admin", "payment-app-withdrawals", id],
    queryFn: () => listWithdrawals({ appId: id }).then((r) => (Array.isArray(r.items) ? r.items : [])),
    enabled: !!id,
    staleTime: 15_000,
  });
  const withdrawals = withdrawalsQuery.data ?? [];

  return (
    <div>
      <Link to="/admin/payments/apps" className="inline-flex items-center gap-1.5 text-sm text-slate-500 hover:text-slate-700">
        <ArrowLeft className="h-3.5 w-3.5" /> Back to apps
      </Link>

      <div className="mt-3 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          {appsQuery.isLoading ? (
            <Skeleton className="h-8 w-48" />
          ) : (
            <h2 className="text-2xl font-bold text-slate-900">{app?.name ?? "Unknown app"}</h2>
          )}
          <p className="mt-1 text-sm text-slate-500">{app?.description || "No description"}</p>
        </div>
        {app && (
          <div className="flex flex-wrap items-center gap-1.5">
            <Badge variant="secondary">{app.status}</Badge>
            <Badge variant="outline">{formatFee(app)}</Badge>
            <span className="text-xs text-slate-400">Registered {formatDate(app.created_at)}</span>
          </div>
        )}
      </div>

      <div className="mt-6 grid grid-cols-2 gap-4 sm:grid-cols-3 lg:grid-cols-5">
        <Card>
          <CardContent className="p-4">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Available balance</p>
            <p className={`mt-1 text-xl font-extrabold ${isNegativeBalance(balance?.available_balance) ? "text-red-600" : "text-slate-900"}`}>
              {balance ? formatMoney(balance.available_balance, balance.currency) : <Skeleton className="h-6 w-20" />}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Total revenue</p>
            <p className="mt-1 text-xl font-extrabold text-slate-900">
              {balance ? formatMoney(balance.total_revenue, balance.currency) : <Skeleton className="h-6 w-20" />}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Platform fees</p>
            <p className="mt-1 text-xl font-extrabold text-slate-900">
              {balance ? formatMoney(balance.total_platform_fees, balance.currency) : <Skeleton className="h-6 w-20" />}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Withdrawn</p>
            <p className="mt-1 text-xl font-extrabold text-slate-900">
              {balance ? formatMoney(balance.total_withdrawn, balance.currency) : <Skeleton className="h-6 w-20" />}
            </p>
          </CardContent>
        </Card>
        <Card>
          <CardContent className="p-4">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Pending orders</p>
            <p className="mt-1 text-xl font-extrabold text-slate-900">
              {balance ? formatMoney(balance.pending_order_total, balance.currency) : <Skeleton className="h-6 w-20" />}
            </p>
          </CardContent>
        </Card>
      </div>

      <Tabs defaultValue="orders" className="mt-8">
        <TabsList>
          <TabsTrigger value="orders">Orders ({orders.length})</TabsTrigger>
          <TabsTrigger value="ledger">Ledger ({ledgerEntries.length})</TabsTrigger>
          <TabsTrigger value="withdrawals">Withdrawals ({withdrawals.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="orders">
          <div className="mb-3 flex items-center gap-2">
            <select
              className="h-9 rounded-md border border-slate-300 px-2 text-sm"
              value={orderStatus}
              onChange={(e) => setOrderStatus(e.target.value)}
            >
              {["", "pending", "processing", "paid", "failed", "cancelled", "expired", "reversed"].map((s) => (
                <option key={s} value={s}>{s === "" ? "Any status" : s}</option>
              ))}
            </select>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Provider</TableHead>
                <TableHead>Amount</TableHead>
                <TableHead>Buyer</TableHead>
                <TableHead>Reference</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {orders.map((order) => (
                <TableRow key={order.id}>
                  <TableCell>{order.provider}</TableCell>
                  <TableCell>{order.amount} {order.currency}</TableCell>
                  <TableCell>{order.buyer_name || order.buyer_phone || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{order.external_reference || order.provider_order_id || "—"}</TableCell>
                  <TableCell><Badge variant="secondary">{order.status}</Badge></TableCell>
                  <TableCell>{formatDate(order.created_at)}</TableCell>
                </TableRow>
              ))}
              {!ordersQuery.isLoading && orders.length === 0 && (
                <TableRow><TableCell colSpan={6} className="text-center text-slate-500">No orders for this app yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="ledger">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Type</TableHead>
                <TableHead>Amount</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Date</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ledgerEntries.map((entry) => (
                <TableRow key={entry.id}>
                  <TableCell>
                    <Badge className={`ring-1 ring-inset ${LEDGER_ENTRY_STYLES[entry.entry_type] ?? ""}`} variant="outline">
                      {entry.entry_type.replace(/_/g, " ")}
                    </Badge>
                  </TableCell>
                  <TableCell className={entry.direction === "credit" ? "text-emerald-700" : "text-rose-700"}>
                    {entry.direction === "credit" ? "+" : "-"}{entry.amount} {entry.currency}
                  </TableCell>
                  <TableCell className="max-w-xs truncate text-slate-500">{entry.description}</TableCell>
                  <TableCell>{formatDate(entry.created_at)}</TableCell>
                </TableRow>
              ))}
              {!ledgerQuery.isLoading && ledgerEntries.length === 0 && (
                <TableRow><TableCell colSpan={4} className="text-center text-slate-500">No ledger entries for this app yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="withdrawals">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Amount</TableHead>
                <TableHead>Destination</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Requested</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {withdrawals.map((withdrawal) => (
                <TableRow key={withdrawal.id}>
                  <TableCell>{formatMoney(withdrawal.amount, withdrawal.currency)}</TableCell>
                  <TableCell className="capitalize">{withdrawal.destination_type.replace(/_/g, " ")}</TableCell>
                  <TableCell><Badge variant="secondary">{withdrawal.status}</Badge></TableCell>
                  <TableCell>{formatDate(withdrawal.created_at)}</TableCell>
                </TableRow>
              ))}
              {!withdrawalsQuery.isLoading && withdrawals.length === 0 && (
                <TableRow><TableCell colSpan={4} className="text-center text-slate-500">No withdrawals for this app yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default AdminPaymentAppDetail;
