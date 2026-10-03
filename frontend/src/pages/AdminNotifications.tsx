import { FormEvent, useState } from "react";
import { useMutation } from "@tanstack/react-query";
import { Megaphone } from "lucide-react";
import { Button } from "@/components/ui/button";
import { sendAdminBroadcast } from "@/lib/notificationsApi";

const AdminNotifications = () => {
  const [title, setTitle] = useState("");
  const [body, setBody] = useState("");
  const [severity, setSeverity] = useState("info");
  const [target, setTarget] = useState("all");
  const [kycStatus, setKycStatus] = useState("submitted");
  const [status, setStatus] = useState("");
  const send = useMutation({
    mutationFn: () => sendAdminBroadcast({ title, body, severity, target: target === "all" ? { kind: "all" } : target === "kyc_status" ? { kind: "kyc_status", status: kycStatus } : { kind: target } }),
    onSuccess: (result) => { setStatus(`Sent to ${result?.data?.recipients ?? 0} recipients.`); setTitle(""); setBody(""); },
    onError: (error) => setStatus(error instanceof Error ? error.message : "Unable to send notification."),
  });
  const submit = (event: FormEvent) => { event.preventDefault(); setStatus(""); send.mutate(); };
  return (
    <div className="mx-auto max-w-2xl space-y-6">
      <div><p className="text-sm font-medium text-primary">Admin communications</p><h1 className="mt-1 text-2xl font-semibold text-foreground">Send notification</h1><p className="mt-1 text-sm text-muted-foreground">Create a platform announcement or targeted operational message. Account alerts remain in-app even when general channels are muted.</p></div>
      <form onSubmit={submit} className="space-y-4 rounded-xl border border-border bg-card p-5">
        <label className="block text-sm font-medium text-foreground">Audience
          <select value={target} onChange={(event) => setTarget(event.target.value)} className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2"><option value="all">Everyone</option><option value="merchant">All merchants</option><option value="creator">All creators</option><option value="kyc_status">Organizations by KYC status</option></select>
        </label>
        {target === "kyc_status" && <label className="block text-sm font-medium text-foreground">KYC status
          <select value={kycStatus} onChange={(event) => setKycStatus(event.target.value)} className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2"><option value="submitted">Submitted</option><option value="verified">Verified</option><option value="rejected">Rejected</option><option value="pending">Pending</option></select>
        </label>}
        <label className="block text-sm font-medium text-foreground">Title<input required value={title} onChange={(event) => setTitle(event.target.value)} className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2" maxLength={160} /></label>
        <label className="block text-sm font-medium text-foreground">Message<textarea required value={body} onChange={(event) => setBody(event.target.value)} className="mt-1 min-h-32 w-full rounded-md border border-input bg-background px-3 py-2" maxLength={1000} /></label>
        <label className="block text-sm font-medium text-foreground">Severity<select value={severity} onChange={(event) => setSeverity(event.target.value)} className="mt-1 w-full rounded-md border border-input bg-background px-3 py-2"><option value="info">Info</option><option value="success">Success</option><option value="warning">Warning</option><option value="alert">Alert</option></select></label>
        <div className="flex items-center justify-between gap-3"><Button type="submit" disabled={send.isPending}><Megaphone className="mr-2 h-4 w-4" />{send.isPending ? "Sending…" : "Send notification"}</Button>{status && <p className="text-sm text-muted-foreground" role="status">{status}</p>}</div>
      </form>
    </div>
  );
};

export default AdminNotifications;
