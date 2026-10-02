import { useEffect, useMemo, useState } from "react";
import { useQueries, useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { toast } from "@/components/ui/sonner";
import { listMerchantOrders, listMyApps } from "@/lib/merchantApi";

function maskPhone(phone?: string) {
  const digits = (phone ?? "").replace(/\D/g, "");
  if (digits.length < 4) return "•••";
  return `+${digits.slice(0, 3)} ••• ••• ${digits.slice(-3)}`;
}

function maskedSupporter(name?: string, phone?: string) {
  const firstName = (name ?? "").trim().split(/\s+/)[0] || "Supporter";
  return `${firstName} · ${maskPhone(phone)}`;
}

const CreatorPayments = () => {
  const [status, setStatus] = useState("");
  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const orderQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["creator", app.id, "payments", status],
      queryFn: () => listMerchantOrders(app.id, status || undefined),
      staleTime: 15_000,
    })),
  });
  const loading = appsQuery.isLoading || orderQueries.some((query) => query.isLoading);
  const failed = orderQueries.find((query) => query.error)?.error;

  useEffect(() => {
    if (failed) toast.error(failed instanceof Error ? failed.message : "Unable to load payments.");
  }, [failed]);

  const payments = useMemo(() => orderQueries
    .flatMap((query) => query.data ?? [])
    .filter((order) => order.metadata?.kind === "support")
    .sort((a, b) => +new Date(b.created_at) - +new Date(a.created_at)), [orderQueries]);

  return (
    <div>
      <h2 className="text-2xl font-bold text-slate-900">Payments</h2>
      <p className="mt-1 text-sm text-slate-500">Support payments received through your page. Supporter messages are private to you.</p>

      <div className="mt-4 flex justify-end">
        <select aria-label="Filter by status" className="h-9 rounded-md border border-slate-300 px-2 text-sm" value={status} onChange={(event) => setStatus(event.target.value)}>
          {["", "pending", "paid", "failed", "expired"].map((value) => (
            <option key={value} value={value}>{value || "Any status"}</option>
          ))}
        </select>
      </div>

      <Card className="mt-4">
        <CardContent className="p-0">
          {loading ? (
            <div className="space-y-2 p-4"><Skeleton className="h-10 w-full" /><Skeleton className="h-10 w-full" /></div>
          ) : (
            <Table>
              <TableHeader><TableRow>
                <TableHead>Supporter</TableHead><TableHead>Message</TableHead><TableHead>Amount</TableHead>
                <TableHead>Provider</TableHead><TableHead>Status</TableHead><TableHead>Received</TableHead>
              </TableRow></TableHeader>
              <TableBody>
                {payments.map((payment) => (
                  <TableRow key={payment.id}>
                    <TableCell className="text-xs">{maskedSupporter(payment.buyer_name, payment.buyer_phone)}</TableCell>
                    <TableCell className="max-w-sm text-xs italic text-slate-600">
                      {typeof payment.metadata?.supporter_message === "string" && payment.metadata.supporter_message.trim()
                        ? `“${payment.metadata.supporter_message}”`
                        : "—"}
                    </TableCell>
                    <TableCell>{payment.amount} {payment.currency}</TableCell>
                    <TableCell>{payment.provider}</TableCell>
                    <TableCell><Badge variant="secondary">{payment.status}</Badge></TableCell>
                    <TableCell className="text-xs">{payment.created_at ? new Date(payment.created_at).toLocaleString() : "—"}</TableCell>
                  </TableRow>
                ))}
                {payments.length === 0 && <TableRow><TableCell colSpan={6} className="text-center text-slate-500">No support payments yet.</TableCell></TableRow>}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

export default CreatorPayments;