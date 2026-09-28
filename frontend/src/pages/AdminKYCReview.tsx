import { FormEvent, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { CheckCircle2, FileText, Settings2, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from "@/components/ui/dialog";
import { toast } from "@/components/ui/sonner";
import {
  approveKYC,
  fetchKYCDocument,
  listKYCQueue,
  rejectKYC,
  updateOrgLimits,
  type KYCQueueItem,
} from "@/lib/orgApi";

const STATUS_TABS = ["submitted", "all", "verified", "rejected"] as const;

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : "—";
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

const AdminKYCReview = () => {
  const queryClient = useQueryClient();
  const [status, setStatus] = useState<string>("submitted");

  const queueQuery = useQuery({
    queryKey: ["admin", "kyc-queue", status],
    queryFn: () => listKYCQueue(status === "submitted" ? undefined : status),
    staleTime: 15_000,
  });
  const items = queueQuery.data ?? [];

  const [actingOrgId, setActingOrgId] = useState<string | null>(null);
  const [rejectItem, setRejectItem] = useState<KYCQueueItem | null>(null);
  const [rejectReason, setRejectReason] = useState("");
  const [limitsItem, setLimitsItem] = useState<KYCQueueItem | null>(null);
  const [limitsMaxTxn, setLimitsMaxTxn] = useState("");
  const [limitsDailyCap, setLimitsDailyCap] = useState("");
  const [viewingDocId, setViewingDocId] = useState<string | null>(null);

  const reload = () => queryClient.invalidateQueries({ queryKey: ["admin", "kyc-queue"] });

  const handleApprove = async (item: KYCQueueItem) => {
    if (!window.confirm(`Approve verification for ${item.org_name}? Live payments unlock immediately.`)) return;
    setActingOrgId(item.org_id);
    try {
      await approveKYC(item.org_id);
      toast.success(`${item.org_name} verified.`);
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to approve."));
    } finally {
      setActingOrgId(null);
    }
  };

  const handleReject = async (event: FormEvent) => {
    event.preventDefault();
    if (!rejectItem) return;
    setActingOrgId(rejectItem.org_id);
    try {
      await rejectKYC(rejectItem.org_id, rejectReason.trim());
      toast.success(`${rejectItem.org_name} rejected.`);
      setRejectItem(null);
      setRejectReason("");
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to reject."));
    } finally {
      setActingOrgId(null);
    }
  };

  const openLimits = (item: KYCQueueItem) => {
    setLimitsItem(item);
    setLimitsMaxTxn("");
    setLimitsDailyCap("");
  };

  const handleSaveLimits = async (event: FormEvent) => {
    event.preventDefault();
    if (!limitsItem) return;
    setActingOrgId(limitsItem.org_id);
    try {
      await updateOrgLimits(limitsItem.org_id, {
        live_max_txn_amount: limitsMaxTxn.trim(),
        live_daily_volume_cap: limitsDailyCap.trim(),
      });
      toast.success("Live limits updated (empty = platform default).");
      setLimitsItem(null);
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update limits."));
    } finally {
      setActingOrgId(null);
    }
  };

  const handleViewDocument = async (item: KYCQueueItem) => {
    setViewingDocId(item.org_id);
    try {
      const { blob } = await fetchKYCDocument(item.org_id);
      const url = URL.createObjectURL(blob);
      window.open(url, "_blank", "noopener");
      window.setTimeout(() => URL.revokeObjectURL(url), 60_000);
    } catch (err) {
      toast.error(errorMessage(err, "Unable to load document."));
    } finally {
      setViewingDocId(null);
    }
  };

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">KYC review</h2>
        <p className="mt-1 text-sm text-slate-500">
          Approve or reject organization verification. Approval unlocks live API keys and live payments.
        </p>
      </div>

      <div className="mt-4 flex gap-1.5">
        {STATUS_TABS.map((tab) => (
          <Button key={tab} size="sm" variant={status === tab ? "default" : "outline"} onClick={() => setStatus(tab)}>
            {tab}
          </Button>
        ))}
      </div>

      {queueQuery.isLoading ? (
        <Skeleton className="mt-4 h-48 w-full" />
      ) : items.length === 0 ? (
        <Card className="mt-4">
          <CardContent className="p-10 text-center text-sm text-slate-500">
            Nothing in the {status} queue.
          </CardContent>
        </Card>
      ) : (
        <Card className="mt-4">
          <CardContent className="overflow-x-auto p-0">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Organization</TableHead>
                  <TableHead>Business / TIN</TableHead>
                  <TableHead>Owner</TableHead>
                  <TableHead>Submitted</TableHead>
                  <TableHead>Doc</TableHead>
                  <TableHead className="text-right">Actions</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {items.map((item) => (
                  <TableRow key={item.org_id}>
                    <TableCell>
                      <p className="text-sm font-semibold text-slate-900">{item.org_name}</p>
                      <p className="text-xs text-slate-400">{item.slug} · <Badge variant="secondary">{item.kyc_status}</Badge></p>
                      {item.rejection_reason && (
                        <p className="mt-0.5 text-xs text-rose-600">Rejected: {item.rejection_reason}</p>
                      )}
                    </TableCell>
                    <TableCell className="text-xs text-slate-600">
                      {item.business_name || "—"}<br />
                      <span className="text-slate-400">TIN {item.tin || "—"}</span>
                    </TableCell>
                    <TableCell className="text-xs text-slate-600">
                      {item.owner_name || item.owner_email || "—"}
                      {item.owner_email && item.owner_name && <><br /><span className="text-slate-400">{item.owner_email}</span></>}
                      {item.owner_phone && <><br /><span className="text-slate-400">{item.owner_phone}</span></>}
                    </TableCell>
                    <TableCell className="text-xs text-slate-500">{formatDate(item.submitted_at)}</TableCell>
                    <TableCell>
                      {item.has_document ? (
                        <Button size="sm" variant="outline" disabled={viewingDocId === item.org_id} onClick={() => handleViewDocument(item)}>
                          <FileText className="h-3.5 w-3.5 mr-1" /> {viewingDocId === item.org_id ? "Loading..." : "View"}
                        </Button>
                      ) : (
                        <span className="text-xs text-slate-400">none</span>
                      )}
                    </TableCell>
                    <TableCell className="text-right">
                      <div className="flex justify-end gap-1.5">
                        {item.kyc_status === "submitted" && (
                          <>
                            <Button size="sm" variant="outline" disabled={actingOrgId === item.org_id} onClick={() => handleApprove(item)}>
                              <CheckCircle2 className="h-3.5 w-3.5 mr-1" /> Approve
                            </Button>
                            <Button size="sm" variant="outline" disabled={actingOrgId === item.org_id} onClick={() => { setRejectItem(item); setRejectReason(""); }}>
                              <XCircle className="h-3.5 w-3.5 mr-1" /> Reject
                            </Button>
                          </>
                        )}
                        <Button size="sm" variant="outline" onClick={() => openLimits(item)}>
                          <Settings2 className="h-3.5 w-3.5 mr-1" /> Limits
                        </Button>
                      </div>
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          </CardContent>
        </Card>
      )}

      <Dialog open={rejectItem !== null} onOpenChange={(open) => { if (!open) setRejectItem(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Reject {rejectItem?.org_name}?</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleReject} className="space-y-4">
            <div>
              <label className="text-sm font-medium text-slate-700">Reason (shown to the organization)</label>
              <Input value={rejectReason} onChange={(e) => setRejectReason(e.target.value)} required maxLength={500} className="mt-1" placeholder="e.g. ID document is unreadable" />
            </div>
            <DialogFooter>
              <Button type="submit" variant="destructive" disabled={actingOrgId !== null}>Reject verification</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={limitsItem !== null} onOpenChange={(open) => { if (!open) setLimitsItem(null); }}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Live limits — {limitsItem?.org_name}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSaveLimits} className="space-y-4">
            <p className="text-xs text-slate-500">Per-org overrides. Leave a field empty to use the platform default.</p>
            <div>
              <label className="text-sm font-medium text-slate-700">Max per transaction</label>
              <Input value={limitsMaxTxn} onChange={(e) => setLimitsMaxTxn(e.target.value)} inputMode="decimal" placeholder="platform default" className="mt-1" />
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Max daily volume (per currency)</label>
              <Input value={limitsDailyCap} onChange={(e) => setLimitsDailyCap(e.target.value)} inputMode="decimal" placeholder="platform default" className="mt-1" />
            </div>
            <DialogFooter>
              <Button type="submit" disabled={actingOrgId !== null}>Save limits</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default AdminKYCReview;
