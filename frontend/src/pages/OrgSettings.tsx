import { FormEvent, useEffect, useState } from "react";
import { Link, useNavigate, useParams } from "react-router-dom";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Clock, Copy, Trash2, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { toast } from "@/components/ui/sonner";
import {
  deleteOrg,
  getLimitsUsage,
  getNotificationPrefs,
  getOrg,
  listKYCAttempts,
  resolveLogoSrc,
  updateNotificationPrefs,
  updateOrg,
  uploadOrgLogo,
  type NotificationPrefs,
  type Organization,
} from "@/lib/orgApi";
import { changePassword, getKYC } from "@/lib/signupApi";
import { listMerchantWithdrawals, listMyApps } from "@/lib/merchantApi";

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : "—";
}

// ---------- Verification status presentation ----------

const KYC_STATUS = {
  verified: { icon: CheckCircle2, classes: "border-emerald-200 bg-emerald-50", iconClass: "text-emerald-600", title: "Verified", copy: "Full access — live API keys and live payments are unlocked." },
  submitted: { icon: Clock, classes: "border-blue-200 bg-blue-50", iconClass: "text-blue-600", title: "Under review", copy: "Sandbox mode only until an admin approves it." },
  pending: { icon: AlertTriangle, classes: "border-amber-200 bg-amber-50", iconClass: "text-amber-600", title: "Not submitted", copy: "Sandbox mode only — submit verification to unlock live payments." },
  rejected: { icon: XCircle, classes: "border-red-200 bg-red-50", iconClass: "text-red-600", title: "Rejected", copy: "Resubmit documents to unlock live payments." },
} as const;

// ---------- General tab ----------

type ProfileForm = {
  name: string;
  business_name: string;
  tin: string;
  address: string;
  phone: string;
  contact_email: string;
  logo_url: string;
  primary_color: string;
};

function formFromOrg(org: Organization): ProfileForm {
  return {
    name: org.name ?? "",
    business_name: org.business_name ?? "",
    tin: org.tin ?? "",
    address: org.address ?? "",
    phone: org.phone ?? "",
    contact_email: org.contact_email ?? "",
    logo_url: org.logo_url ?? "",
    primary_color: org.primary_color ?? "",
  };
}

const GeneralTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const [form, setForm] = useState<ProfileForm>(() => formFromOrg(org));
  const [saving, setSaving] = useState(false);
  const locked = org.kyc_status === "verified";
  const dirty = JSON.stringify(form) !== JSON.stringify(formFromOrg(org));
  const set = (key: keyof ProfileForm) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      await updateOrg(org.id, {
        name: form.name.trim(),
        business_name: form.business_name.trim() || undefined,
        tin: form.tin.trim() || undefined,
        address: form.address.trim() || undefined,
        phone: form.phone.trim() || undefined,
        contact_email: form.contact_email.trim() || undefined,
        logo_url: form.logo_url.trim() || undefined,
        primary_color: form.primary_color.trim() || undefined,
      });
      toast.success("Organization updated.");
      queryClient.invalidateQueries({ queryKey: ["orgs", org.id] });
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update organization."));
    } finally {
      setSaving(false);
    }
  };

  const copyId = () => {
    navigator.clipboard.writeText(org.id).then(() => toast.success("Org ID copied."));
  };

  const field = (
    label: string,
    helper: string,
    key: keyof ProfileForm,
    opts?: { disabled?: boolean; placeholder?: string; type?: string },
  ) => (
    <div>
      <label className="text-sm font-medium text-slate-700">{label}</label>
      <Input
        value={form[key]}
        onChange={set(key)}
        disabled={!isOwner || opts?.disabled}
        placeholder={opts?.placeholder}
        type={opts?.type ?? "text"}
        className="mt-1"
      />
      <p className="mt-1 text-xs text-slate-400">{helper}</p>
    </div>
  );

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <form onSubmit={handleSave} className="space-y-4">
          <div>
            <label className="text-sm font-medium text-slate-700">Org ID</label>
            <div className="mt-1 flex items-center gap-2">
              <code className="flex-1 truncate rounded-md bg-slate-100 px-3 py-2 font-mono text-xs text-slate-600">{org.id}</code>
              <Button type="button" size="sm" variant="outline" onClick={copyId}>
                <Copy className="h-3.5 w-3.5 mr-1" /> Copy
              </Button>
            </div>
            <p className="mt-1 text-xs text-slate-400">Read-only identifier used in API paths and support requests.</p>
          </div>
          {field("Organization name", "Workspace label shown in navigation — internal only.", "name")}
          {field(
            "Business name",
            "Legal / trading name shown to customers and used for verification.",
            "business_name",
            { disabled: locked, placeholder: "Acme Limited" },
          )}
          <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
            {field("TIN", "Tax identification number. Locked after verification.", "tin", { disabled: locked, placeholder: "123456789" })}
            {field("Contact email", "Where customers and LipaGO reach you.", "contact_email", { type: "email", placeholder: "billing@example.com" })}
          </div>
          {field("Business address", "Physical address of the business.", "address", { placeholder: "123 Sam Nujoma Rd, Dar es Salaam" })}
          {field("Business phone", "Business contact number.", "phone", { placeholder: "+255712345678" })}
          {locked && (
            <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs text-amber-900">
              Business name and TIN are locked after verification. Changing them requires re-verification —{" "}
              <Link to={`/onboarding/kyc/${org.id}`} className="font-medium underline">request a change via resubmission</Link>.
            </div>
          )}
          {isOwner ? (
            <Button type="submit" disabled={!dirty || saving}>{saving ? "Saving..." : "Save changes"}</Button>
          ) : (
            <p className="text-sm text-slate-500">Only owners can change settings.</p>
          )}
        </form>
      </CardContent>
    </Card>
  );
};

// ---------- Verification tab ----------

const VerificationTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const status = KYC_STATUS[org.kyc_status] ?? KYC_STATUS.pending;
  const Icon = status.icon;

  const kycQuery = useQuery({
    queryKey: ["orgs", org.id, "kyc"],
    queryFn: () => getKYC(org.id).catch(() => null),
    staleTime: 30_000,
  });
  const attemptsQuery = useQuery({
    queryKey: ["orgs", org.id, "kyc-attempts"],
    queryFn: () => listKYCAttempts(org.id),
    staleTime: 30_000,
  });
  const attempts = attemptsQuery.data ?? [];
  const rejectionReason = kycQuery.data?.submission.rejection_reason || "";

  return (
    <div className="mt-4 space-y-4">
      <Card className={status.classes}>
        <CardContent className="flex items-start gap-3 p-4">
          <Icon className={`mt-0.5 h-5 w-5 shrink-0 ${status.iconClass}`} />
          <div className="text-sm">
            <p className="font-semibold text-slate-900">Verification: {status.title}</p>
            <p className="mt-0.5 text-slate-600">{status.copy}</p>
            {org.kyc_status === "rejected" && rejectionReason && (
              <p className="mt-2 rounded-md bg-white/70 px-3 py-2 text-xs text-red-800">
                <span className="font-semibold">Admin reason: </span>{rejectionReason}
              </p>
            )}
          </div>
        </CardContent>
      </Card>

      {isOwner && org.kyc_status === "pending" && (
        <Button asChild><Link to={`/onboarding/kyc/${org.id}`}>Submit verification</Link></Button>
      )}
      {isOwner && org.kyc_status === "rejected" && (
        <Button asChild><Link to={`/onboarding/kyc/${org.id}`}>Resubmit verification</Link></Button>
      )}

      <Card>
        <CardContent className="p-4">
          <h3 className="text-sm font-bold text-slate-800">Submission history</h3>
          {attemptsQuery.isLoading ? (
            <p className="mt-2 text-sm text-slate-500">Loading history…</p>
          ) : attempts.length === 0 ? (
            <p className="mt-2 text-sm text-slate-500">No submissions yet.</p>
          ) : (
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead>Date</TableHead>
                  <TableHead>Business</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Decision</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {attempts.map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className="text-xs">{formatDate(a.created_at)}</TableCell>
                    <TableCell className="text-xs">{a.business_name || "—"}</TableCell>
                    <TableCell><Badge variant="secondary">{a.status}</Badge></TableCell>
                    <TableCell className="max-w-xs truncate text-xs text-slate-500">
                      {a.status === "rejected" && a.rejection_reason ? a.rejection_reason : a.reviewed_by ? `by ${a.reviewed_by}` : "—"}
                    </TableCell>
                  </TableRow>
                ))}
              </TableBody>
            </Table>
          )}
        </CardContent>
      </Card>
    </div>
  );
};

// ---------- Limits & Fees tab (read-only) ----------

function feeText(app: { fee_type: string; fee_percent: string; fee_fixed: string }) {
  if (app.fee_type === "fixed") return `Flat ${app.fee_fixed} per transaction`;
  if (app.fee_type === "hybrid") return `${app.fee_percent}% + ${app.fee_fixed} flat per transaction`;
  return `${app.fee_percent}% per transaction`;
}

const LimitsTab = ({ org }: { org: Organization }) => {
  const usageQuery = useQuery({
    queryKey: ["orgs", org.id, "limits-usage"],
    queryFn: () => getLimitsUsage(org.id),
    staleTime: 15_000,
  });
  const usage = usageQuery.data;

  return (
    <div className="mt-4 space-y-3">
      <p className="text-xs text-slate-500">
        Read-only — caps are set by platform defaults or an admin override. Ask an admin to adjust them.
        Volume is computed live from the ledger, live environment only.
      </p>
      {usageQuery.isLoading && <p className="text-sm text-slate-500">Loading limits…</p>}
      {usageQuery.error && <p className="text-sm text-red-600">Unable to load limits right now.</p>}
      {(usage?.apps ?? []).map((app) => (
        <Card key={app.app_id}>
          <CardContent className="p-4">
            <div className="flex flex-wrap items-center gap-2">
              <Link to={`/merchant/apps/${app.app_id}`} className="text-sm font-semibold text-blue-600 hover:underline">{app.name}</Link>
              <Badge variant="outline">{feeText(app)}</Badge>
            </div>
            <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="rounded-lg bg-slate-50 px-3 py-2.5">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">
                  Max per transaction · {app.max_txn_source === "org_override" ? "org override" : "platform default"}
                </p>
                <p className="mt-0.5 text-base font-extrabold text-slate-900">{app.max_txn || "—"}</p>
              </div>
              <div className="rounded-lg bg-slate-50 px-3 py-2.5">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">
                  Daily volume cap · {app.daily_cap_source === "org_override" ? "org override" : "platform default"}
                </p>
                <p className="mt-0.5 text-base font-extrabold text-slate-900">{app.daily_cap || "—"}</p>
              </div>
            </div>
            {Object.entries(app.today_volume).map(([currency, used]) => {
              const usedNum = Number(used);
              const capNum = Number(app.daily_cap);
              const pct = capNum > 0 && Number.isFinite(usedNum) ? Math.min(100, (usedNum / capNum) * 100) : 0;
              return (
                <div key={currency} className="mt-3">
                  <div className="flex items-center justify-between text-xs text-slate-500">
                    <span>Today&apos;s live volume ({currency})</span>
                    <span>{used} / {app.daily_cap || "—"}</span>
                  </div>
                  <div className="mt-1 h-2 overflow-hidden rounded-full bg-slate-100">
                    <div className="h-full rounded-full bg-emerald-500 transition-all" style={{ width: `${pct}%` }} />
                  </div>
                </div>
              );
            })}
            {Object.keys(app.today_volume).length === 0 && (
              <p className="mt-3 text-xs text-slate-400">No live volume currencies yet.</p>
            )}
          </CardContent>
        </Card>
      ))}
      {!usageQuery.isLoading && !usageQuery.error && (usage?.apps ?? []).length === 0 && (
        <p className="text-sm text-slate-500">No apps yet — limits apply once you create one.</p>
      )}
    </div>
  );
};

// ---------- Security tab (the signed-in user's own password) ----------

const SecurityTab = () => {
  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [saving, setSaving] = useState(false);

  const handleChange = async (event: FormEvent) => {
    event.preventDefault();
    if (newPassword !== confirmPassword) {
      toast.error("New passwords do not match.");
      return;
    }
    setSaving(true);
    try {
      await changePassword({ current_password: currentPassword, new_password: newPassword });
      toast.success("Password changed.");
      setCurrentPassword("");
      setNewPassword("");
      setConfirmPassword("");
    } catch (err) {
      toast.error(errorMessage(err, "Unable to change password."));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">Change your password</h3>
        <p className="mt-1 text-xs text-slate-500">Applies to your own sign-in — every member manages their own password here.</p>
        <form onSubmit={handleChange} className="mt-4 space-y-4">
          <div>
            <label className="text-sm font-medium text-slate-700">Current password</label>
            <Input type="password" value={currentPassword} onChange={(e) => setCurrentPassword(e.target.value)} required className="mt-1" />
          </div>
          <div>
            <label className="text-sm font-medium text-slate-700">New password</label>
            <Input type="password" value={newPassword} onChange={(e) => setNewPassword(e.target.value)} required minLength={12} className="mt-1" />
            <p className="mt-1 text-xs text-slate-400">At least 12 characters.</p>
          </div>
          <div>
            <label className="text-sm font-medium text-slate-700">Confirm new password</label>
            <Input type="password" value={confirmPassword} onChange={(e) => setConfirmPassword(e.target.value)} required minLength={12} className="mt-1" />
          </div>
          <Button type="submit" disabled={saving}>{saving ? "Changing..." : "Change password"}</Button>
        </form>
      </CardContent>
    </Card>
  );
};

// ---------- Notifications tab ----------

const NOTIF_FIELDS: { key: keyof Omit<NotificationPrefs, "org_id">; label: string; helper: string }[] = [
  { key: "payment_updated", label: "Payment updates", helper: "Paid / failed order events." },
  { key: "payment_refunded", label: "Refunds", helper: "Refund confirmations." },
  { key: "payment_expired", label: "Expiries", helper: "Orders that passed their TTL." },
  { key: "withdrawal_updates", label: "Withdrawals", helper: "Approval and payout state changes." },
  { key: "kyc_decisions", label: "Verification decisions", helper: "Admin approve / reject outcomes." },
];

const NotificationsTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const prefsQuery = useQuery({
    queryKey: ["orgs", org.id, "notif-prefs"],
    queryFn: () => getNotificationPrefs(org.id),
    staleTime: 30_000,
  });
  const [draft, setDraft] = useState<NotificationPrefs | null>(null);
  const [saving, setSaving] = useState(false);
  const prefs = draft ?? prefsQuery.data ?? null;
  const dirty = draft !== null && prefsQuery.data !== undefined &&
    JSON.stringify(draft) !== JSON.stringify(prefsQuery.data);

  const toggle = (key: keyof Omit<NotificationPrefs, "org_id">) => {
    if (!prefs || !isOwner) return;
    setDraft({ ...prefs, [key]: !prefs[key] });
  };

  const handleSave = async () => {
    if (!draft) return;
    setSaving(true);
    try {
      const { org_id: _ignored, ...input } = draft;
      await updateNotificationPrefs(org.id, input);
      toast.success("Notification preferences saved.");
      queryClient.invalidateQueries({ queryKey: ["orgs", org.id, "notif-prefs"] });
      setDraft(null);
    } catch (err) {
      toast.error(errorMessage(err, "Unable to save preferences."));
    } finally {
      setSaving(false);
    }
  };

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">Notifications</h3>
        <p className="mt-1 text-xs text-slate-500">
          Which events this organization wants to hear about. Delivery is log-only until a mail/SMS provider is wired.
        </p>
        {prefsQuery.isLoading && <p className="mt-3 text-sm text-slate-500">Loading preferences…</p>}
        {prefs && (
          <div className="mt-4 space-y-3">
            {NOTIF_FIELDS.map((f) => (
              <label key={f.key} className={`flex items-start gap-3 rounded-lg border border-slate-200 px-3 py-2.5 ${isOwner ? "cursor-pointer" : ""}`}>
                <input
                  type="checkbox"
                  checked={prefs[f.key]}
                  disabled={!isOwner}
                  onChange={() => toggle(f.key)}
                  className="mt-1 h-4 w-4 accent-slate-900"
                />
                <span>
                  <span className="block text-sm font-medium text-slate-800">{f.label}</span>
                  <span className="block text-xs text-slate-500">{f.helper}</span>
                </span>
              </label>
            ))}
            {isOwner ? (
              <Button disabled={!dirty || saving} onClick={handleSave}>{saving ? "Saving..." : "Save preferences"}</Button>
            ) : (
              <p className="text-sm text-slate-500">Only owners can change notification preferences.</p>
            )}
          </div>
        )}
      </CardContent>
    </Card>
  );
};

// ---------- Branding tab ----------

const BrandingTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const [logoUrl, setLogoUrl] = useState(org.logo_url ?? "");
  const [primaryColor, setPrimaryColor] = useState(org.primary_color || "#0f172a");
  const [saving, setSaving] = useState(false);
  const [uploading, setUploading] = useState(false);
  const [logoSrc, setLogoSrc] = useState("");
  const dirty = logoUrl !== (org.logo_url ?? "") || primaryColor !== (org.primary_color || "#0f172a");

  // Uploaded logos live privately — resolve them through the authenticated
  // endpoint; external URLs render directly.
  useEffect(() => {
    let cancelled = false;
    let objectUrl = "";
    (async () => {
      try {
        const src = await resolveLogoSrc(org.id, logoUrl);
        if (!cancelled) {
          if (objectUrl) URL.revokeObjectURL(objectUrl);
          if (src.startsWith("blob:")) objectUrl = src;
          setLogoSrc(src);
        } else if (src.startsWith("blob:")) {
          URL.revokeObjectURL(src);
        }
      } catch {
        if (!cancelled) setLogoSrc("");
      }
    })();
    return () => {
      cancelled = true;
      if (objectUrl) URL.revokeObjectURL(objectUrl);
    };
  }, [org.id, logoUrl]);

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      await updateOrg(org.id, {
        name: org.name,
        business_name: org.business_name,
        tin: org.tin,
        logo_url: logoUrl.trim() || undefined,
        primary_color: primaryColor.trim() || undefined,
      });
      toast.success("Branding saved.");
      queryClient.invalidateQueries({ queryKey: ["orgs", org.id] });
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to save branding."));
    } finally {
      setSaving(false);
    }
  };

  const handleFile = async (file: File | undefined) => {
    if (!file) return;
    if (file.size > 2 << 20) {
      toast.error("Logo must be under 2MB.");
      return;
    }
    if (!/^(image\/jpeg|image\/png|image\/webp)$/.test(file.type)) {
      toast.error("Logo must be a JPEG, PNG, or WEBP image.");
      return;
    }
    setUploading(true);
    try {
      const updated = await uploadOrgLogo(org.id, file);
      setLogoUrl(updated.logo_url ?? "");
      toast.success("Logo uploaded.");
      queryClient.invalidateQueries({ queryKey: ["orgs", org.id] });
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to upload logo."));
    } finally {
      setUploading(false);
    }
  };

  return (
    <div className="mt-4 grid gap-4 lg:grid-cols-2">
      <Card>
        <CardContent className="p-4 sm:p-6">
          <form onSubmit={handleSave} className="space-y-4">
            <div>
              <label className="text-sm font-medium text-slate-700">Upload logo</label>
              <div className="mt-1 flex items-center gap-2">
                <Input
                  type="file"
                  accept="image/jpeg,image/png,image/webp"
                  disabled={!isOwner || uploading}
                  onChange={(e) => { void handleFile(e.target.files?.[0]); e.target.value = ""; }}
                />
              </div>
              <p className="mt-1 text-xs text-slate-400">
                JPEG, PNG, or WEBP, under 2MB. Stored privately — served only to your members. {uploading && "Uploading…"}
              </p>
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">…or paste a logo URL</label>
              <Input value={logoUrl} onChange={(e) => setLogoUrl(e.target.value)} disabled={!isOwner} placeholder="https://…" className="mt-1" />
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Primary color</label>
              <div className="mt-1 flex items-center gap-2">
                <input type="color" value={/^#[0-9a-fA-F]{6}$/.test(primaryColor) ? primaryColor : "#0f172a"} onChange={(e) => setPrimaryColor(e.target.value)} disabled={!isOwner} className="h-10 w-14 cursor-pointer rounded border border-slate-300" />
                <Input value={primaryColor} onChange={(e) => setPrimaryColor(e.target.value)} disabled={!isOwner} placeholder="#0f172a" className="font-mono text-xs" />
              </div>
            </div>
            {isOwner ? (
              <Button type="submit" disabled={!dirty || saving}>{saving ? "Saving..." : "Save branding"}</Button>
            ) : (
              <p className="text-sm text-slate-500">Only owners can change branding.</p>
            )}
          </form>
        </CardContent>
      </Card>
      <Card>
        <CardContent className="p-4 sm:p-6">
          <h3 className="text-sm font-bold text-slate-800">Preview</h3>
          <div className="mt-3 overflow-hidden rounded-lg border border-slate-200">
            <div className="flex items-center gap-2 px-4 py-3" style={{ backgroundColor: /^#[0-9a-fA-F]{6}$/.test(primaryColor) ? primaryColor : "#0f172a" }}>
              {logoSrc ? (
                <img src={logoSrc} alt="" className="h-6 w-6 rounded bg-white object-contain" />
              ) : (
                <span className="flex h-6 w-6 items-center justify-center rounded bg-white/20 text-xs font-bold text-white">
                  {(org.business_name || org.name || "L").slice(0, 1)}
                </span>
              )}
              <span className="text-sm font-bold text-white">{org.business_name || org.name}</span>
            </div>
            <div className="bg-slate-50 px-4 py-3 text-xs text-slate-500">
              Checkout header preview — your brand above the payment button.
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
};

// ---------- Payouts tab (recent destinations, read-only) ----------

function destinationSummary(w: { destination_type: string; destination_details: Record<string, unknown> }) {
  const d = w.destination_details || {};
  if (w.destination_type === "mobile_money") {
    return [d.provider, d.phone].filter(Boolean).join(" · ") || "Mobile money";
  }
  return [d.bank_name, d.account_number].filter(Boolean).join(" · ") || "Bank transfer";
}

const PayoutsTab = () => {
  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const apps = appsQuery.data ?? [];
  const withdrawalQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["merchant", app.id, "withdrawals"],
      queryFn: () => listMerchantWithdrawals(app.id),
      staleTime: 15_000,
    })),
  });
  const seen = new Map<string, { appName: string; summary: string; type: string; lastUsed: string }>();
  withdrawalQueries.forEach((q, i) => {
    (q.data ?? []).forEach((w) => {
      const summary = destinationSummary(w);
      const key = `${w.destination_type}|${summary}`;
      const prev = seen.get(key);
      if (!prev || w.created_at > prev.lastUsed) {
        seen.set(key, { appName: apps[i]?.name ?? "App", summary, type: w.destination_type, lastUsed: w.created_at });
      }
    });
  });
  const destinations = [...seen.values()].sort((a, b) => (a.lastUsed < b.lastUsed ? 1 : -1));

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">Recent payout destinations</h3>
        <p className="mt-1 text-xs text-slate-500">
          Destinations you paid out to before, across all apps. New withdrawals pick a destination per payout under Withdrawals.
        </p>
        {appsQuery.isLoading && <p className="mt-3 text-sm text-slate-500">Loading…</p>}
        {!appsQuery.isLoading && destinations.length === 0 && (
          <p className="mt-3 text-sm text-slate-500">No payouts yet — destinations appear here after your first withdrawal.</p>
        )}
        {destinations.length > 0 && (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Destination</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Last used (app)</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {destinations.map((d) => (
                <TableRow key={`${d.type}|${d.summary}`}>
                  <TableCell className="text-sm font-medium">{String(d.summary)}</TableCell>
                  <TableCell><Badge variant="outline">{String(d.type).replace(/_/g, " ")}</Badge></TableCell>
                  <TableCell className="text-xs text-slate-500">{d.appName} · {formatDate(d.lastUsed)}</TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
};

// ---------- Danger zone ----------

const DangerTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [deleting, setDeleting] = useState(false);

  const handleDelete = async () => {
    if (!window.confirm(`Delete ${org?.name}? Only empty orgs (no apps) can be deleted.`)) return;
    setDeleting(true);
    try {
      await deleteOrg(org.id);
      toast.success("Organization deleted.");
      queryClient.invalidateQueries({ queryKey: ["orgs", "mine"] });
      navigate("/merchant/apps", { replace: true });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to delete organization."));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Card className="mt-4 border-red-200">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-red-700">Danger zone</h3>
        <p className="mt-1 text-xs text-slate-500">
          Deleting the organization is permanent. Only empty organizations (no apps) can be deleted —
          delete or move the apps first.
        </p>
        {isOwner ? (
          <Button variant="destructive" className="mt-4" disabled={deleting} onClick={handleDelete}>
            <Trash2 className="h-3.5 w-3.5 mr-1" />{deleting ? "Deleting..." : "Delete organization"}
          </Button>
        ) : (
          <p className="mt-4 text-sm text-slate-500">Only owners can delete the organization.</p>
        )}
      </CardContent>
    </Card>
  );
};

// ---------- Page ----------

const OrgSettings = () => {
  const { orgId = "" } = useParams();

  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000 });
  const org = orgQuery.data;
  const isOwner = org?.role === "owner";

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Settings — {org?.name ?? "…"}</h2>
        <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
          Organization settings.
          {org && <Badge variant="secondary">{org.role}</Badge>}
        </p>
      </div>

      {orgQuery.isLoading ? (
        <Skeleton className="mt-4 h-40 w-full" />
      ) : org ? (
        <Tabs defaultValue="general" className="mt-4">
          <TabsList className="flex-wrap">
            <TabsTrigger value="general">General</TabsTrigger>
            <TabsTrigger value="verification">Verification</TabsTrigger>
            <TabsTrigger value="limits">Limits &amp; Fees</TabsTrigger>
            <TabsTrigger value="security">Security</TabsTrigger>
            <TabsTrigger value="notifications">Notifications</TabsTrigger>
            <TabsTrigger value="branding">Branding</TabsTrigger>
            <TabsTrigger value="payouts">Payouts</TabsTrigger>
            <TabsTrigger value="danger">Danger zone</TabsTrigger>
          </TabsList>

          <TabsContent value="general"><GeneralTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="verification"><VerificationTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="limits"><LimitsTab org={org} /></TabsContent>
          <TabsContent value="security"><SecurityTab /></TabsContent>
          <TabsContent value="notifications"><NotificationsTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="branding"><BrandingTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="payouts"><PayoutsTab /></TabsContent>
          <TabsContent value="danger"><DangerTab org={org} isOwner={!!isOwner} /></TabsContent>
        </Tabs>
      ) : (
        <p className="mt-4 text-sm text-slate-500">Organization not found.</p>
      )}
    </div>
  );
};

export default OrgSettings;
