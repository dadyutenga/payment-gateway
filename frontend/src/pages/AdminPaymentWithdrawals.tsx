import { FormEvent, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Wallet, Check, X, Send, Ban, RefreshCw } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { toast } from "@/components/ui/sonner";
import {
  approveWithdrawal,
  createWithdrawal,
  listPaymentApps,
  listWithdrawals,
  markWithdrawalFailed,
  markWithdrawalPaid,
  rejectWithdrawal,
  retryWithdrawalPayout,
  type PaymentWithdrawal,
  type WithdrawalStatus,
} from "@/lib/adminApi";

const QUERY_KEY = ["admin", "payments-withdrawals"];

const STATUS_STYLES: Record<WithdrawalStatus, string> = {
  requested: "bg-amber-50 text-amber-700 ring-amber-600/15",
  approved: "bg-sky-50 text-sky-700 ring-sky-600/15",
  processing: "bg-violet-50 text-violet-700 ring-violet-600/15",
  rejected: "bg-slate-100 text-slate-600 ring-slate-500/15",
  paid: "bg-emerald-50 text-emerald-700 ring-emerald-600/15",
  failed: "bg-rose-50 text-rose-700 ring-rose-600/15",
};

const STATUS_FILTERS: { label: string; value: string }[] = [
  { label: "All", value: "" },
  { label: "Requested", value: "requested" },
  { label: "Approved", value: "approved" },
  { label: "Processing", value: "processing" },
  { label: "Paid", value: "paid" },
  { label: "Rejected", value: "rejected" },
  { label: "Failed", value: "failed" },
];

function formatDate(value: string) {
  return new Date(value).toLocaleString();
}

function formatMoney(value: string, currency: string) {
  const n = Number(value);
  return `${Number.isFinite(n) ? n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : value} ${currency}`;
}

type DestinationForm = {
  destinationType: "bank" | "mobile_money";
  bankName: string;
  accountName: string;
  accountNumber: string;
  provider: string;
  phone: string;
};

const emptyDestination: DestinationForm = {
  destinationType: "bank",
  bankName: "",
  accountName: "",
  accountNumber: "",
  provider: "",
  phone: "",
};

function destinationSummary(withdrawal: PaymentWithdrawal) {
  const d = withdrawal.destination_details || {};
  if (withdrawal.destination_type === "mobile_money") {
    return [d.provider, d.phone].filter(Boolean).join(" · ") || "Mobile money";
  }
  return [d.bank_name, d.account_number].filter(Boolean).join(" · ") || "Bank transfer";
}

const AdminPaymentWithdrawals = () => {
  const queryClient = useQueryClient();
  const [statusFilter, setStatusFilter] = useState("");

  const [dialogOpen, setDialogOpen] = useState(false);
  const [selectedAppId, setSelectedAppId] = useState("");
  const [amount, setAmount] = useState("");
  const [notes, setNotes] = useState("");
  const [destination, setDestination] = useState<DestinationForm>(emptyDestination);
  const [creating, setCreating] = useState(false);
  const [actingId, setActingId] = useState<string | null>(null);

  const appsQuery = useQuery({
    queryKey: ["admin", "payments-apps"],
    queryFn: () => listPaymentApps().then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const apps = appsQuery.data ?? [];
  const appName = (appId: string) => apps.find((a) => a.id === appId)?.name ?? appId;

  const withdrawalsQuery = useQuery({
    queryKey: [...QUERY_KEY, statusFilter],
    queryFn: () => listWithdrawals(statusFilter ? { status: statusFilter } : undefined).then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 15_000,
  });
  const withdrawals = withdrawalsQuery.data ?? [];
  const loading = withdrawalsQuery.isLoading;

  useEffect(() => {
    if (withdrawalsQuery.error) {
      toast.error(withdrawalsQuery.error instanceof Error ? withdrawalsQuery.error.message : "Unable to load withdrawals.");
    }
  }, [withdrawalsQuery.error]);

  const reload = () => {
    queryClient.invalidateQueries({ queryKey: QUERY_KEY });
    queryClient.invalidateQueries({ queryKey: ["admin", "payment-app-balance"] });
  };

  const resetForm = () => {
    setSelectedAppId("");
    setAmount("");
    setNotes("");
    setDestination(emptyDestination);
  };

  const handleCreate = async (event: FormEvent) => {
    event.preventDefault();
    setCreating(true);
    try {
      const destinationDetails =
        destination.destinationType === "mobile_money"
          ? { provider: destination.provider, phone: destination.phone }
          : { bank_name: destination.bankName, account_name: destination.accountName, account_number: destination.accountNumber };

      await createWithdrawal({
        app_id: selectedAppId,
        amount,
        currency: "TZS",
        destination_type: destination.destinationType,
        destination_details: destinationDetails,
        notes,
      });
      toast.success("Withdrawal requested.");
      setDialogOpen(false);
      resetForm();
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to create withdrawal.");
    } finally {
      setCreating(false);
    }
  };

  const runAction = async (id: string, action: () => Promise<unknown>, successMessage: string) => {
    setActingId(id);
    try {
      await action();
      toast.success(successMessage);
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to update withdrawal.");
    } finally {
      setActingId(null);
    }
  };

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Withdrawals</h2>
          <p className="mt-1 text-sm text-slate-500">Payouts recorded on behalf of each app. Every approval debits that app's ledger balance.</p>
        </div>
        <Dialog open={dialogOpen} onOpenChange={(open) => { setDialogOpen(open); if (!open) resetForm(); }}>
          <DialogTrigger asChild>
            <Button><Plus className="h-4 w-4 mr-1" /> New withdrawal</Button>
          </DialogTrigger>
          <DialogContent className="max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>Request withdrawal</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleCreate} className="space-y-4">
              <div>
                <label className="text-sm font-medium text-slate-700">App</label>
                <select
                  className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                  value={selectedAppId}
                  onChange={(e) => setSelectedAppId(e.target.value)}
                  required
                >
                  <option value="">Select an app</option>
                  {apps.map((app) => (
                    <option key={app.id} value={app.id}>{app.name}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Amount (TZS)</label>
                <Input value={amount} onChange={(e) => setAmount(e.target.value)} required inputMode="decimal" placeholder="50000" className="mt-1" />
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Destination</label>
                <select
                  className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                  value={destination.destinationType}
                  onChange={(e) => setDestination((d) => ({ ...d, destinationType: e.target.value as "bank" | "mobile_money" }))}
                >
                  <option value="bank">Bank transfer</option>
                  <option value="mobile_money">Mobile money</option>
                </select>
              </div>
              {destination.destinationType === "bank" ? (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Input placeholder="Bank name" value={destination.bankName} onChange={(e) => setDestination((d) => ({ ...d, bankName: e.target.value }))} required />
                  <Input placeholder="Account name" value={destination.accountName} onChange={(e) => setDestination((d) => ({ ...d, accountName: e.target.value }))} required />
                  <Input placeholder="Account number" value={destination.accountNumber} onChange={(e) => setDestination((d) => ({ ...d, accountNumber: e.target.value }))} required className="sm:col-span-2" />
                </div>
              ) : (
                <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                  <Input placeholder="Provider (e.g. M-Pesa)" value={destination.provider} onChange={(e) => setDestination((d) => ({ ...d, provider: e.target.value }))} required />
                  <Input placeholder="Phone number" value={destination.phone} onChange={(e) => setDestination((d) => ({ ...d, phone: e.target.value }))} required />
                </div>
              )}
              <div>
                <label className="text-sm font-medium text-slate-700">Notes</label>
                <Input value={notes} onChange={(e) => setNotes(e.target.value)} className="mt-1" placeholder="Optional" />
              </div>
              <DialogFooter>
                <Button type="submit" disabled={creating}>{creating ? "Requesting..." : "Request withdrawal"}</Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <div className="mt-6 flex gap-1.5 overflow-x-auto pb-1">
        {STATUS_FILTERS.map((f) => (
          <button
            key={f.value}
            type="button"
            onClick={() => setStatusFilter(f.value)}
            className={`shrink-0 rounded-full px-3 py-1.5 text-xs font-semibold transition-colors ${
              statusFilter === f.value ? "bg-slate-900 text-white" : "bg-slate-100 text-slate-600 hover:bg-slate-200"
            }`}
          >
            {f.label}
          </button>
        ))}
      </div>

      <div className="mt-4 space-y-3">
        {loading &&
          Array.from({ length: 3 }).map((_, i) => (
            <Card key={`skeleton-${i}`}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}

        {!loading && withdrawalsQuery.error && (
          <Card>
            <CardContent className="flex flex-col items-center gap-2 p-10 text-center">
              <Wallet className="h-8 w-8 text-red-300" />
              <p className="text-sm font-medium text-red-600">Unable to load withdrawals right now.</p>
              <p className="text-xs text-slate-400">
                {withdrawalsQuery.error instanceof Error ? withdrawalsQuery.error.message : "Please try again shortly."}
              </p>
            </CardContent>
          </Card>
        )}

        {!loading && !withdrawalsQuery.error && withdrawals.length === 0 && (
          <Card>
            <CardContent className="flex flex-col items-center gap-2 p-10 text-center">
              <Wallet className="h-8 w-8 text-slate-300" />
              <p className="text-sm font-medium text-slate-600">
                {statusFilter ? `No ${statusFilter} withdrawals.` : "No withdrawals yet."}
              </p>
            </CardContent>
          </Card>
        )}

        {!loading &&
          withdrawals.map((withdrawal) => {
            const busy = actingId === withdrawal.id;
            return (
              <Card key={withdrawal.id}>
                <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
                  <div className="min-w-0">
                    <div className="flex flex-wrap items-center gap-2">
                      <p className="text-sm font-semibold text-slate-900">{appName(withdrawal.app_id)}</p>
                      <Badge className={`ring-1 ring-inset ${STATUS_STYLES[withdrawal.status]}`} variant="outline">{withdrawal.status}</Badge>
                    </div>
                    <p className="mt-1 text-lg font-extrabold text-slate-900">{formatMoney(withdrawal.amount, withdrawal.currency)}</p>
                    <p className="mt-1 text-xs text-slate-500">{destinationSummary(withdrawal)}</p>
                    <p className="mt-1 text-xs text-slate-400">Requested {formatDate(withdrawal.created_at)}</p>
                    {withdrawal.notes && <p className="mt-1 text-xs text-slate-500">Note: {withdrawal.notes}</p>}
                    {withdrawal.provider_payout_id && (
                      <p className="mt-1 text-xs text-slate-400">
                        Payout: {withdrawal.provider} · {withdrawal.provider_payout_id}
                        {withdrawal.provider_status ? ` (${withdrawal.provider_status})` : ""}
                      </p>
                    )}
                    {withdrawal.failure_reason && (
                      <p className="mt-1 text-xs text-rose-600">Payout attempt failed: {withdrawal.failure_reason}</p>
                    )}
                  </div>
                  <div className="flex shrink-0 flex-wrap items-center gap-1.5 self-end sm:self-start">
                    {withdrawal.status === "requested" && (
                      <>
                        <Button
                          size="sm"
                          disabled={busy}
                          onClick={() => runAction(withdrawal.id, () => approveWithdrawal(withdrawal.id), "Withdrawal approved — balance debited.")}
                        >
                          <Check className="h-3.5 w-3.5 mr-1" /> Approve
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy}
                          onClick={() => runAction(withdrawal.id, () => rejectWithdrawal(withdrawal.id), "Withdrawal rejected.")}
                        >
                          <X className="h-3.5 w-3.5 mr-1" /> Reject
                        </Button>
                      </>
                    )}
                    {(withdrawal.status === "approved" || withdrawal.status === "processing") && (
                      <>
                        {withdrawal.status === "approved" && (
                          <Button
                            size="sm"
                            variant="outline"
                            disabled={busy}
                            onClick={() => runAction(withdrawal.id, () => retryWithdrawalPayout(withdrawal.id), "Payout dispatched.")}
                          >
                            <RefreshCw className="h-3.5 w-3.5 mr-1" /> {withdrawal.failure_reason ? "Retry payout" : "Dispatch payout"}
                          </Button>
                        )}
                        <Button
                          size="sm"
                          disabled={busy}
                          onClick={() => runAction(withdrawal.id, () => markWithdrawalPaid(withdrawal.id), "Marked as paid.")}
                        >
                          <Send className="h-3.5 w-3.5 mr-1" /> Mark paid
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy}
                          onClick={() => runAction(withdrawal.id, () => markWithdrawalFailed(withdrawal.id), "Marked as failed — balance restored.")}
                        >
                          <Ban className="h-3.5 w-3.5 mr-1" /> Mark failed
                        </Button>
                      </>
                    )}
                  </div>
                </CardContent>
              </Card>
            );
          })}
      </div>
    </div>
  );
};

export default AdminPaymentWithdrawals;
