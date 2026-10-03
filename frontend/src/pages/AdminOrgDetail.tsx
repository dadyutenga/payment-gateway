import { FormEvent, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery } from "@tanstack/react-query";
import { ArrowLeft, Send } from "lucide-react";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { fetchAdminOrg } from "@/lib/analyticsApi";
import { sendAdminOrgNotification } from "@/lib/notificationsApi";
import { Button } from "@/components/ui/button";

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : "—";
}

// Operator single-org view (linked from analytics merchant rows):
// profile, verification evidence + history, members. Mutations live in
// KYC review (approve/reject/limits) and merchant Settings.
const AdminOrgDetail = () => {
  const { orgId = "" } = useParams();
  const detailQuery = useQuery({
    queryKey: ["admin", "org", orgId],
    queryFn: () => fetchAdminOrg(orgId),
    staleTime: 30_000,
  });
  const detail = detailQuery.data;
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [severity, setSeverity] = useState("info");
  const [notice, setNotice] = useState("");
  const send = useMutation({ mutationFn: () => sendAdminOrgNotification(orgId, { title, body, severity }), onSuccess: () => { setTitle(""); setBody(""); setNotice("Notification sent."); }, onError: (error) => setNotice(error instanceof Error ? error.message : "Unable to send notification.") });
  const submitNotification = (event: FormEvent) => { event.preventDefault(); setNotice(""); send.mutate(); };

  return (
    <div>
      <Link to="/admin" className="inline-flex items-center gap-1.5 text-sm text-slate-500 hover:text-slate-700">
        <ArrowLeft className="h-3.5 w-3.5" /> Back to dashboard
      </Link>

      {detailQuery.isLoading ? (
        <Skeleton className="mt-4 h-48 w-full" />
      ) : detailQuery.error || !detail ? (
        <p className="mt-4 text-sm text-red-600">Unable to load organization.</p>
      ) : (
        <>
          <div className="mt-3">
            <h2 className="text-2xl font-bold text-slate-900">{detail.org.name}</h2>
            <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
              <Badge variant="secondary">{detail.org.kyc_status}</Badge>
              {(detail.org as { account_kind?: string }).account_kind === "creator" && <Badge variant="outline">creator</Badge>}
              <span className="font-mono text-xs">{detail.org.id}</span>
            </p>
          </div>

          <form onSubmit={submitNotification} className="mt-4 rounded-xl border border-border bg-card p-4">
            <div className="flex items-center gap-2"><Send className="h-4 w-4 text-primary" /><h3 className="text-sm font-semibold text-foreground">Send notification to this organization</h3></div>
            <div className="mt-3 grid gap-3 sm:grid-cols-2"><input required value={title} onChange={(event) => setTitle(event.target.value)} placeholder="Title" className="rounded-md border border-input bg-background px-3 py-2 text-sm" maxLength={160} /><input required value={body} onChange={(event) => setBody(event.target.value)} placeholder="Short message" className="rounded-md border border-input bg-background px-3 py-2 text-sm" maxLength={1000} /><select value={severity} onChange={(event) => setSeverity(event.target.value)} className="rounded-md border border-input bg-background px-3 py-2 text-sm" aria-label="Notification severity"><option value="info">Info</option><option value="success">Success</option><option value="warning">Warning</option><option value="alert">Alert</option></select></div>
            <div className="mt-3 flex items-center gap-3"><Button size="sm" type="submit" disabled={send.isPending}>{send.isPending ? "Sending…" : "Send notification"}</Button>{notice && <span className="text-xs text-muted-foreground" role="status">{notice}</span>}</div>
          </form>

          <div className="mt-4 grid gap-4 lg:grid-cols-2">
            <Card><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">Profile</h3>
              <dl className="mt-2 space-y-1 text-sm">
                {(detail.org as { account_kind?: string }).account_kind === "creator" ? (
                  <>
                    <div className="flex justify-between gap-2"><dt className="text-slate-500">Display name</dt><dd className="font-medium">{(detail.org as { display_name?: string }).display_name || "—"}</dd></div>
                    <div className="flex justify-between gap-2"><dt className="text-slate-500">Handle</dt><dd className="font-mono text-xs">{(detail.org as { handle?: string }).handle || "—"}</dd></div>
                  </>
                ) : (
                  <>
                    <div className="flex justify-between gap-2"><dt className="text-slate-500">Business</dt><dd className="font-medium">{detail.org.business_name || "—"}</dd></div>
                    <div className="flex justify-between gap-2"><dt className="text-slate-500">TIN</dt><dd className="font-medium">{detail.org.tin || "—"}</dd></div>
                  </>
                )}
                <div className="flex justify-between gap-2"><dt className="text-slate-500">Slug</dt><dd className="font-mono text-xs">{detail.org.slug}</dd></div>
                <div className="flex justify-between gap-2"><dt className="text-slate-500">Live caps</dt><dd className="text-xs">{detail.org.live_max_txn_amount || "default"} / {detail.org.live_daily_volume_cap || "default"}</dd></div>
              </dl>
              <Link to="/admin/kyc" className="mt-3 inline-block text-xs text-blue-600 hover:underline">Open in KYC review →</Link>
            </CardContent></Card>

            <Card><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">Members ({detail.members.length})</h3>
              <Table>
                <TableHeader><TableRow><TableHead>Email</TableHead><TableHead>Role</TableHead><TableHead>Status</TableHead></TableRow></TableHeader>
                <TableBody>
                  {detail.members.map((m) => (
                    <TableRow key={m.user_id}>
                      <TableCell className="text-xs">
                        <p className="font-medium">{m.full_name || m.email}</p>
                        <p className="text-slate-400">{m.email}{m.phone ? ` · ${m.phone}` : ""}</p>
                      </TableCell>
                      <TableCell><Badge variant="secondary">{m.role}</Badge></TableCell>
                      <TableCell className="text-xs">{m.status}</TableCell>
                    </TableRow>
                  ))}
                  {detail.members.length === 0 && (
                    <TableRow><TableCell colSpan={3} className="text-center text-slate-500">No members.</TableCell></TableRow>
                  )}
                </TableBody>
              </Table>
            </CardContent></Card>
          </div>

          {(detail.org as { account_kind?: string }).account_kind === "creator" ? (
            <Card className="mt-4"><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">
                Current individual verification <Badge variant="outline" className="border-fuchsia-300 bg-fuchsia-50 text-fuchsia-700">creator</Badge>
              </h3>
              {!detail.kyc ? (
                <p className="mt-2 text-sm text-slate-500">Never submitted.</p>
              ) : (
                <dl className="mt-2 space-y-1 text-sm">
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">Full legal name</dt><dd className="font-medium">{detail.kyc.full_name || "—"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">ID type</dt><dd className="font-medium">{detail.kyc.id_type || "—"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">ID number</dt><dd className="font-mono text-xs">{detail.kyc.id_number || "—"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">Date of birth</dt><dd className="font-medium">{detail.kyc.dob || "—"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">ID front</dt><dd className="text-xs">{detail.kyc.id_document_url ? "on file" : "missing"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">ID back</dt><dd className="text-xs">{detail.kyc.id_document_back_url ? "on file" : "not provided"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">Selfie</dt><dd className="text-xs">{detail.kyc.selfie_url ? "on file" : "missing"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">Submitted</dt><dd className="text-xs">{formatDate(detail.kyc.submitted_at)}</dd></div>
                  {detail.kyc.rejection_reason && (
                    <div className="flex justify-between gap-2"><dt className="text-slate-500">Rejection reason</dt><dd className="max-w-xs text-right text-xs text-rose-600">{detail.kyc.rejection_reason}</dd></div>
                  )}
                </dl>
              )}
              <Link to="/admin/kyc" className="mt-3 inline-block text-xs text-blue-600 hover:underline">Open in KYC review →</Link>
            </CardContent></Card>
          ) : (
            <Card className="mt-4"><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">
                Current business verification <Badge variant="outline" className="border-emerald-300 bg-emerald-50 text-emerald-700">business</Badge>
              </h3>
              {!detail.kyc ? (
                <p className="mt-2 text-sm text-slate-500">Never submitted.</p>
              ) : (
                <dl className="mt-2 space-y-1 text-sm">
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">Business name</dt><dd className="font-medium">{detail.kyc.business_name || "—"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">TIN</dt><dd className="font-mono text-xs">{detail.kyc.tin || "—"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">Document</dt><dd className="text-xs">{detail.kyc.id_document_url ? "on file" : "missing"}</dd></div>
                  <div className="flex justify-between gap-2"><dt className="text-slate-500">Submitted</dt><dd className="text-xs">{formatDate(detail.kyc.submitted_at)}</dd></div>
                  {detail.kyc.rejection_reason && (
                    <div className="flex justify-between gap-2"><dt className="text-slate-500">Rejection reason</dt><dd className="max-w-xs text-right text-xs text-rose-600">{detail.kyc.rejection_reason}</dd></div>
                  )}
                </dl>
              )}
              <Link to="/admin/kyc" className="mt-3 inline-block text-xs text-blue-600 hover:underline">Open in KYC review →</Link>
            </CardContent></Card>
          )}

          {detail.survey && (            <Card className="mt-4"><CardContent className="p-4">
              <h3 className="text-sm font-bold text-slate-800">
                Onboarding survey{" "}
                <Badge variant={detail.survey.suggested_risk_tier === "high" ? "destructive" : "secondary"}>
                  {detail.survey.suggested_risk_tier} risk
                </Badge>
              </h3>
              <dl className="mt-2 space-y-1 text-sm">
                <div className="flex justify-between gap-2"><dt className="text-slate-500">Category</dt><dd className="font-medium">{detail.survey.category_other || detail.survey.category}</dd></div>
                <div className="flex justify-between gap-2"><dt className="text-slate-500">Heard via</dt><dd className="font-medium">{detail.survey.referral_source}</dd></div>
                <div className="flex justify-between gap-2"><dt className="text-slate-500">Use cases</dt><dd className="font-medium text-right">{detail.survey.use_cases.join(", ") || "—"}</dd></div>
                <div className="flex justify-between gap-2"><dt className="text-slate-500">Expected volume</dt><dd className="font-medium">{detail.survey.expected_volume_band}</dd></div>
                <div className="flex justify-between gap-2"><dt className="text-slate-500">Expected txns</dt><dd className="font-medium">{detail.survey.expected_txn_band}</dd></div>
              </dl>
              <p className="mt-2 text-xs text-slate-400">Segmentation only — never raises live limits.</p>
            </CardContent></Card>
          )}

          <Card className="mt-4"><CardContent className="p-4">
            <h3 className="text-sm font-bold text-slate-800">Verification history</h3>
            {detail.attempts.length === 0 ? (
              <p className="mt-2 text-sm text-slate-500">Never submitted.</p>
            ) : (
              <Table>
                <TableHeader><TableRow><TableHead>Date</TableHead><TableHead>Status</TableHead><TableHead>Decision</TableHead></TableRow></TableHeader>
                <TableBody>
                  {detail.attempts.map((a) => (
                    <TableRow key={a.id}>
                      <TableCell className="text-xs">{formatDate(a.created_at)}</TableCell>
                      <TableCell><Badge variant="secondary">{a.status}</Badge></TableCell>
                      <TableCell className="max-w-md truncate text-xs text-slate-500">
                        {a.status === "rejected" && a.rejection_reason ? a.rejection_reason : a.reviewed_by ? `by ${a.reviewed_by}` : "—"}
                      </TableCell>
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

export default AdminOrgDetail;
