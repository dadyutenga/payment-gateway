import { FormEvent, useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, CheckCircle2, Clock, Copy, Trash2, XCircle } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { toast } from "@/components/ui/sonner";
import {
  deleteOrg,
  deleteIndividualAccount,
  getIndividualLimitsUsage,
  getLimitsUsage,
  getIndividualPayoutDestination,
  listKYCAttempts,
  listIndividualKYCAttempts,
  listOrgMembers,
  resolveLogoSrc,
  resolveIndividualLogoSrc,
  saveIndividualPayoutDestination,
  updateOrg,
  updateIndividualAccount,
  uploadOrgLogo,
  uploadIndividualLogo,
  type Organization,
} from "@/lib/orgApi";
import IndividualSurveyForm, { loadIndividualSurvey } from "@/components/IndividualSurveyForm";
import SupportPageEditor from "@/components/SupportPageEditor";
import { changePassword, getKYC, getIndividualKYC, getOwnProfile, requestOTP, updateOwnProfile } from "@/lib/signupApi";
import { listMerchantWithdrawals, listMyApps } from "@/lib/merchantApi";
import { listNotificationPreferences, updateNotificationPreference } from "@/lib/notificationsApi";

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
  display_name: string;
  handle: string;
  bio: string;
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
    display_name: org.display_name ?? "",
    handle: org.handle ?? "",
    bio: org.bio ?? "",
  };
}

export const isIndividualAccount = (org: Organization) => (org.account_kind ?? "merchant") === "creator";

// Track-specific verification entry: business and individual verification
// are separate flows, so every link must point at the matching one.
export const verifyPathFor = (org: Organization) =>
  isIndividualAccount(org) ? "/individual/verify" : `/merchant/verify/${org.id}`;

export const GeneralTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const [form, setForm] = useState<ProfileForm>(() => formFromOrg(org));
  const [saving, setSaving] = useState(false);
  const locked = org.kyc_status === "verified";
  const isIndividual = isIndividualAccount(org);
  const dirty = JSON.stringify(form) !== JSON.stringify(formFromOrg(org));
  const set = (key: keyof ProfileForm) => (e: React.ChangeEvent<HTMLInputElement>) =>
    setForm((f) => ({ ...f, [key]: e.target.value }));

  // Organization owner section: first active owner, with contact from
  // their profile (members list carries full_name/phone when filled in).
  const membersQuery = useQuery({
    queryKey: ["orgs", org.id, "members"],
    queryFn: () => listOrgMembers(org.id),
    staleTime: 30_000,
  });
  const owner = (membersQuery.data ?? []).find((m) => m.role === "owner" && m.status === "active");

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      const input = {
        name: form.name.trim(),
        business_name: isIndividual ? undefined : form.business_name.trim() || undefined,
        tin: isIndividual ? undefined : form.tin.trim() || undefined,
        address: form.address.trim() || undefined,
        phone: form.phone.trim() || undefined,
        contact_email: form.contact_email.trim() || undefined,
        logo_url: form.logo_url.trim() || undefined,
        primary_color: form.primary_color.trim() || undefined,
        display_name: isIndividual ? form.display_name.trim() || undefined : undefined,
        handle: isIndividual ? form.handle.trim().toLowerCase() || undefined : undefined,
        bio: isIndividual ? form.bio.trim() || undefined : undefined,
      };
      await (isIndividual ? updateIndividualAccount(input) : updateOrg(org.id, input));
      toast.success(isIndividual ? "Personal profile updated." : "Organization updated.");
      queryClient.invalidateQueries({ queryKey: isIndividual ? ["individual", "account"] : ["orgs", org.id] });
      queryClient.invalidateQueries({ queryKey: isIndividual ? ["individual", "account"] : ["orgs", "mine"] });
    } catch (err) {
      toast.error(errorMessage(err, isIndividual ? "Unable to update personal profile." : "Unable to update organization."));
    } finally {
      setSaving(false);
    }
  };

  const copyId = () => {
    navigator.clipboard.writeText(org.id).then(() => toast.success(isIndividual ? "Account ID copied." : "Org ID copied."));
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
          <div className="rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5">
            <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">{isIndividual ? "Personal account" : "Organization owner"}</p>
            {membersQuery.isLoading ? (
              <p className="mt-1 text-sm text-slate-500">Loading owner…</p>
            ) : owner ? (
              <div className="mt-1 text-sm">
                <p className="font-semibold text-slate-900">{owner.full_name || owner.email}</p>
                <p className="text-slate-500">{owner.email}{owner.phone ? ` · ${owner.phone}` : ""}</p>
                {(!owner.full_name || !owner.phone) && (
                  <p className="mt-1 text-xs text-amber-700">
                    Owner profile incomplete — {isOwner ? "complete it under Security → My profile." : "ask the owner to complete it under Security → My profile."}
                  </p>
                )}
              </div>
            ) : (
              <p className="mt-1 text-sm text-slate-500">No active owner found.</p>
            )}
          </div>
          <div>
            <label className="text-sm font-medium text-slate-700">{isIndividual ? "Account ID" : "Org ID"}</label>
            <div className="mt-1 flex items-center gap-2">
              <code className="flex-1 truncate rounded-md bg-slate-100 px-3 py-2 font-mono text-xs text-slate-600">{org.id}</code>
              <Button type="button" size="sm" variant="outline" onClick={copyId}>
                <Copy className="h-3.5 w-3.5 mr-1" /> Copy
              </Button>
            </div>
            <p className="mt-1 text-xs text-slate-400">Read-only identifier used in API paths and support requests.</p>
          </div>
          {!isIndividual && field("Organization name", "Workspace label shown in navigation — internal only.", "name")}
          {isIndividual ? (
            <>
              {field("Display name", "Public name on your support page.", "display_name", { placeholder: "Amina Creates" })}
              {field("Handle", "Your public link: /c/<handle>. Contact support to change it later.", "handle", { placeholder: "amina.creates" })}
              {field("Bio", "Short public intro shown to supporters (500 chars).", "bio", { placeholder: "I make videos about…" })}
              <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
                {field("Contact email", "Where supporters and LipaGO reach you.", "contact_email", { type: "email", placeholder: "hello@example.com" })}
                {field("Contact phone", "Your contact number.", "phone", { placeholder: "+255712345678" })}
              </div>
            </>
          ) : (
            <>
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
            </>
          )}
          {!isIndividual && locked && (
            <div className="rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs text-amber-900">
              Business name and TIN are locked after verification. Changing them requires re-verification —{" "}
              <Link to={verifyPathFor(org)} className="font-medium underline">request a change via resubmission</Link>.
            </div>
          )}
          {isIndividual && org.handle && (
            <div className="rounded-lg border border-emerald-200 bg-emerald-50 px-3 py-2.5 text-xs text-emerald-900">
              Your public support page: <Link to={`/c/${org.handle}`} className="font-medium underline">/c/{org.handle}</Link>
              {" "}— share it anywhere. {org.kyc_status === "verified" ? "Live support payments enabled." : "Sandbox only until ID verification passes."}
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

export const VerificationTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const status = KYC_STATUS[org.kyc_status] ?? KYC_STATUS.pending;
  const Icon = status.icon;
  const individual = isIndividualAccount(org);

  const kycQuery = useQuery({
    queryKey: ["orgs", org.id, "kyc"],
    queryFn: () => (individual ? getIndividualKYC() : getKYC(org.id)).catch(() => null),
    staleTime: 30_000,
  });
  const attemptsQuery = useQuery({
    queryKey: ["orgs", org.id, "kyc-attempts"],
    queryFn: () => individual ? listIndividualKYCAttempts() : listKYCAttempts(org.id),
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
        <Button asChild><Link to={verifyPathFor(org)}>Submit verification</Link></Button>
      )}
      {isOwner && org.kyc_status === "rejected" && (
        <Button asChild><Link to={verifyPathFor(org)}>Resubmit verification</Link></Button>
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
                  <TableHead>{individual ? "Identity" : "Business"}</TableHead>
                  <TableHead>Status</TableHead>
                  <TableHead>Decision</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {attempts.map((a) => (
                  <TableRow key={a.id}>
                    <TableCell className="text-xs">{formatDate(a.created_at)}</TableCell>
                    <TableCell className="text-xs">{a.full_name || a.business_name || "—"}</TableCell>
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

export const LimitsTab = ({ org, track = "merchant" }: { org: Organization; track?: "merchant" | "individual" | "creator" }) => {
  const isIndividualTrack = track === "individual" || track === "creator";
  const usageQuery = useQuery({
    queryKey: ["orgs", org.id, "limits-usage", track],
    queryFn: () => (isIndividualTrack ? getIndividualLimitsUsage() : getLimitsUsage(org.id)),
    staleTime: 15_000,
  });
  const usage = usageQuery.data;

  return (
    <div className="mt-4 space-y-3">
      <p className="text-xs text-slate-500">
        Read-only — caps are set by platform defaults or an admin override. Ask an admin to adjust them.
        Volume is computed live from the ledger, live environment only.
        {isIndividualTrack && (
          <> Personal accounts start on a stricter tier than businesses — an admin can raise your account individually.</>
        )}
      </p>
      {usageQuery.isLoading && <p className="text-sm text-slate-500">Loading limits…</p>}
      {usageQuery.error && <p className="text-sm text-red-600">Unable to load limits right now.</p>}
      {(usage?.apps ?? []).map((app) => (
        <Card key={app.app_id}>
          <CardContent className="p-4">
            <div className="flex flex-wrap items-center gap-2">
              {isIndividualTrack ? (
                <span className="text-sm font-semibold text-slate-800">{app.name}</span>
              ) : (
                <Link to={`/merchant/apps/${app.app_id}`} className="text-sm font-semibold text-blue-600 hover:underline">{app.name}</Link>
              )}
              <Badge variant="outline">{feeText(app)}</Badge>
            </div>
            <div className="mt-3 grid grid-cols-1 gap-3 sm:grid-cols-2">
              <div className="rounded-lg bg-slate-50 px-3 py-2.5">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">
                  Max per transaction · {sourceLabel(app.max_txn_source)}
                </p>
                <p className="mt-0.5 text-base font-extrabold text-slate-900">{app.max_txn || "—"}</p>
              </div>
              <div className="rounded-lg bg-slate-50 px-3 py-2.5">
                <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">
                  Daily volume cap · {sourceLabel(app.daily_cap_source)}
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

function sourceLabel(source: string) {
  if (source === "org_override") return "org override";
  if (source === "platform_creator") return "individual tier default";
  return "platform default";
}

// ---------- Security tab (own profile + own password) ----------

export const SecurityTab = () => {
  const queryClient = useQueryClient();
  const profileQuery = useQuery({ queryKey: ["auth", "profile"], queryFn: () => getOwnProfile(), staleTime: 30_000 });
  const profile = profileQuery.data;
  const [fullName, setFullName] = useState<string | null>(null);
  const [phone, setPhone] = useState<string | null>(null);
  const [savingProfile, setSavingProfile] = useState(false);
  const profileDirty =
    profile !== undefined &&
    ((fullName ?? profile.full_name ?? "") !== (profile.full_name ?? "") ||
      (phone ?? profile.phone ?? "") !== (profile.phone ?? ""));

  const [currentPassword, setCurrentPassword] = useState("");
  const [newPassword, setNewPassword] = useState("");
  const [confirmPassword, setConfirmPassword] = useState("");
  const [saving, setSaving] = useState(false);

  const handleSaveProfile = async (event: FormEvent) => {
    event.preventDefault();
    setSavingProfile(true);
    try {
      await updateOwnProfile({
        full_name: (fullName ?? profile?.full_name ?? "").trim(),
        phone: (phone ?? profile?.phone ?? "").trim(),
      });
      toast.success("Profile saved.");
      queryClient.invalidateQueries({ queryKey: ["auth", "profile"] });
      queryClient.invalidateQueries({ queryKey: ["orgs"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to save profile."));
    } finally {
      setSavingProfile(false);
    }
  };

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
    <div className="mt-4 space-y-4">
    <Card>
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">My profile</h3>
        <p className="mt-1 text-xs text-slate-500">Your display name and phone — used for your personal account and verification contact.</p>
        {profileQuery.isLoading ? (
          <p className="mt-3 text-sm text-slate-500">Loading profile…</p>
        ) : profile ? (
          <form onSubmit={handleSaveProfile} className="mt-4 space-y-4">
            <div>
              <label className="text-sm font-medium text-slate-700">Full name</label>
              <Input value={fullName ?? profile.full_name ?? ""} onChange={(e) => setFullName(e.target.value)} maxLength={100} placeholder="Amina Juma" className="mt-1" />
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Phone</label>
              <Input value={phone ?? profile.phone ?? ""} onChange={(e) => setPhone(e.target.value)} inputMode="tel" placeholder="+255712345678" className="mt-1" />
              <p className="mt-1 text-xs text-slate-400">Tanzanian mobile — also used for SMS verification codes.</p>
            </div>
            <Button type="submit" disabled={!profileDirty || savingProfile}>{savingProfile ? "Saving..." : "Save profile"}</Button>
          </form>
        ) : (
          <p className="mt-3 text-sm text-red-600">Unable to load profile.</p>
        )}
      </CardContent>
    </Card>
    <Card>
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
    </div>
  );
};

// ---------- Notifications tab ----------

const NOTIF_FIELDS = [
  { key: "payment.succeeded", label: "Payment updates", helper: "Successful payment events." },
  { key: "payment.failed", label: "Failed payments", helper: "Payment failures that need attention." },
  { key: "payment.refunded", label: "Refunds", helper: "Refund confirmations." },
  { key: "payment.expired", label: "Expiries", helper: "Orders that passed their TTL." },
  { key: "withdrawal.completed", label: "Withdrawals", helper: "Approval and payout state changes." },
  { key: "kyc.verified", label: "Verification decisions", helper: "Admin approve / reject outcomes." },
  { key: "security.payout_destination_changed", label: "Security changes", helper: "Payout destination and API key changes." },
];
const NOTIF_CHANNELS = [{ key: "in_app", label: "In-app" }, { key: "email", label: "Email" }, { key: "sms", label: "SMS" }] as const;
const CRITICAL_EVENTS = new Set(["payment.failed", "withdrawal.completed", "kyc.verified", "security.payout_destination_changed"]);

export const NotificationsTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const prefsQuery = useQuery({
    queryKey: ["notifications", "preferences"],
    queryFn: () => listNotificationPreferences("customer"),
    staleTime: 30_000,
  });
  const update = useMutation({ mutationFn: (preference: { event_type: string; channel: string; enabled: boolean }) => updateNotificationPreference("customer", preference), onSuccess: () => { queryClient.invalidateQueries({ queryKey: ["notifications", "preferences"] }); toast.success("Notification preference saved."); }, onError: (err) => toast.error(errorMessage(err, "Unable to save preference.")) });
  const enabled = (eventType: string, channel: string) => prefsQuery.data?.find((item) => item.event_type === eventType && item.channel === channel)?.enabled ?? true;

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">Notifications</h3>
        <p className="mt-1 text-xs text-slate-500">Choose delivery channels for account activity. Critical in-app alerts cannot be disabled.</p>
        {prefsQuery.isLoading && <p className="mt-3 text-sm text-slate-500">Loading preferences…</p>}
        <div className="mt-4 space-y-3">{NOTIF_FIELDS.map((field) => <div key={field.key} className="rounded-lg border border-slate-200 px-3 py-3"><div className="flex flex-wrap items-center justify-between gap-3"><span><span className="block text-sm font-medium text-slate-800">{field.label}</span><span className="block text-xs text-slate-500">{field.helper}</span></span><div className="flex items-center gap-3">{NOTIF_CHANNELS.map((channel) => { const locked = channel.key === "in_app" && CRITICAL_EVENTS.has(field.key); return <label key={channel.key} className="flex items-center gap-1 text-xs text-slate-600"><input type="checkbox" checked={enabled(field.key, channel.key)} disabled={!isOwner || locked || update.isPending} onChange={(event) => update.mutate({ event_type: field.key, channel: channel.key, enabled: event.target.checked })} className="h-4 w-4 accent-slate-900" />{channel.label}</label>; })}</div></div></div>)}</div>
        {!isOwner && <p className="mt-3 text-sm text-slate-500">Only owners can change workspace notification preferences.</p>}
      </CardContent>
    </Card>
  );
};

// ---------- Branding tab ----------

export const BrandingTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const isIndividual = isIndividualAccount(org);
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
        const src = await (isIndividual ? resolveIndividualLogoSrc(logoUrl) : resolveLogoSrc(org.id, logoUrl));
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
  }, [isIndividual, org.id, logoUrl]);

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      const input = {
        name: org.name,
        business_name: org.business_name,
        tin: org.tin,
        display_name: org.display_name,
        handle: org.handle,
        bio: org.bio,
        logo_url: logoUrl.trim() || undefined,
        primary_color: primaryColor.trim() || undefined,
      };
      await (isIndividual ? updateIndividualAccount(input) : updateOrg(org.id, input));
      toast.success("Branding saved.");
      queryClient.invalidateQueries({ queryKey: isIndividual ? ["individual", "account"] : ["orgs", org.id] });
      queryClient.invalidateQueries({ queryKey: isIndividual ? ["individual", "account"] : ["orgs", "mine"] });
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
      const updated = await (isIndividual ? uploadIndividualLogo(file) : uploadOrgLogo(org.id, file));
      setLogoUrl(updated.logo_url ?? "");
      toast.success("Logo uploaded.");
      queryClient.invalidateQueries({ queryKey: isIndividual ? ["individual", "account"] : ["orgs", org.id] });
      queryClient.invalidateQueries({ queryKey: isIndividual ? ["individual", "account"] : ["orgs", "mine"] });
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

export const PayoutsTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  if (org && isIndividualAccount(org)) {
    return <IndividualPayoutDestinationCard org={org} isOwner={isOwner} />;
  }
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

// ---------- Individual payout destination (OTP-gated, 24h cooling) ----------

const MOBILE_PROVIDERS = ["mpesa", "tigo", "airtel", "halotel"] as const;

export const IndividualPayoutDestinationCard = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const destQuery = useQuery({
    queryKey: ["individual", "account", "payout-destination"],
    queryFn: () => getIndividualPayoutDestination(),
    staleTime: 15_000,
  });
  const dest = destQuery.data ?? null;
  const cooling = dest ? new Date(dest.effective_at).getTime() > Date.now() : false;

  const [provider, setProvider] = useState("mpesa");
  const [phone, setPhone] = useState("");
  const [accountName, setAccountName] = useState("");
  const [channel, setChannel] = useState<"sms" | "email">("sms");
  const [otpPhone, setOtpPhone] = useState("");
  const [code, setCode] = useState("");
  const [codeSent, setCodeSent] = useState(false);
  const [working, setWorking] = useState(false);

  const reload = () => queryClient.invalidateQueries({ queryKey: ["individual", "account", "payout-destination"] });

  const handleRequestCode = async () => {
    setWorking(true);
    try {
      await requestOTP({ channel, purpose: "payout_destination", phone: channel === "sms" ? otpPhone.trim() || undefined : undefined });
      setCodeSent(true);
      toast.success(channel === "sms" ? "Code sent by SMS — it expires in 10 minutes." : "Code sent by email — it expires in 10 minutes.");
    } catch (err) {
      toast.error(errorMessage(err, "Unable to send a code."));
    } finally {
      setWorking(false);
    }
  };

  const handleSave = async (event: FormEvent) => {
    event.preventDefault();
    setWorking(true);
    try {
      await saveIndividualPayoutDestination({
        provider,
        phone: phone.trim(),
        account_name: accountName.trim(),
        otp_channel: channel,
        otp_code: code.trim(),
      });
      toast.success(dest ? "Destination change saved — 24h cooling applies." : "Payout destination saved.");
      setCode("");
      setCodeSent(false);
      reload();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to save the payout destination."));
    } finally {
      setWorking(false);
    }
  };

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">Payout destination</h3>
        <p className="mt-1 text-xs text-slate-500">
          One personal mobile-money number, tied to your verified identity. Changes need an OTP code and
          take effect after a 24h cooling period — withdrawals must target the effective destination.
        </p>

        {destQuery.isLoading ? (
          <p className="mt-3 text-sm text-slate-500">Loading…</p>
        ) : dest ? (
          <div className="mt-3 rounded-lg border border-slate-200 bg-slate-50 px-3 py-2.5 text-sm">
            <p className="font-semibold text-slate-900">{dest.phone} <Badge variant="outline">{dest.provider}</Badge></p>
            <p className="mt-1 text-slate-600">{dest.account_name}</p>
            <div className="mt-2 flex flex-wrap gap-1.5">
              <Badge variant="secondary">identity: matched</Badge>
              <Badge variant="outline" title={dest.name_match_detail || undefined}>
                provider name check: {dest.name_match === "unavailable" ? "unavailable" : dest.name_match}
              </Badge>
              {cooling && <Badge variant="secondary">cooling until {formatDate(dest.effective_at)}</Badge>}
            </div>
            {dest.name_match === "unavailable" && dest.name_match_detail && (
              <p className="mt-2 text-xs text-slate-400">{dest.name_match_detail}</p>
            )}
          </div>
        ) : (
          <p className="mt-3 text-sm text-slate-500">No payout destination yet — withdrawals stay blocked until you save one.</p>
        )}

        {!isOwner ? (
          <p className="mt-3 text-sm text-slate-500">Only owners can change the payout destination.</p>
        ) : org.kyc_status !== "verified" ? (
          <p className="mt-3 rounded-lg border border-amber-200 bg-amber-50 px-3 py-2.5 text-xs text-amber-900">
            Verify your identity first — the destination must match your verified name.
          </p>
        ) : (
          <form onSubmit={handleSave} className="mt-4 space-y-4">
            <div className="grid grid-cols-1 gap-4 sm:grid-cols-2">
              <div>
                <label className="text-sm font-medium text-slate-700">Provider</label>
                <select value={provider} onChange={(e) => setProvider(e.target.value)} className="mt-1 h-10 w-full rounded-md border border-slate-300 bg-white px-2 text-sm">
                  {MOBILE_PROVIDERS.map((p) => (
                    <option key={p} value={p}>{p}</option>
                  ))}
                </select>
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Mobile-money number</label>
                <Input value={phone} onChange={(e) => setPhone(e.target.value)} required inputMode="tel" placeholder={dest?.phone || "+255712345678"} className="mt-1" />
              </div>
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Registered account name</label>
              <Input value={accountName} onChange={(e) => setAccountName(e.target.value)} required maxLength={120} placeholder="Exactly as on your ID" className="mt-1" />
              <p className="mt-1 text-xs text-slate-400">Must match your verified identity name — saves are refused on mismatch.</p>
            </div>
            <div className="rounded-lg border border-slate-200 p-3">
              <p className="text-sm font-medium text-slate-700">Confirm with a code {dest ? "(required for every change)" : ""}</p>
              <div className="mt-2 grid grid-cols-1 gap-2 sm:grid-cols-3">
                <select value={channel} onChange={(e) => { setChannel(e.target.value as "sms" | "email"); setCodeSent(false); }} className="h-10 rounded-md border border-slate-300 bg-white px-2 text-sm">
                  <option value="sms">SMS</option>
                  <option value="email">Email</option>
                </select>
                {channel === "sms" && (
                  <Input value={otpPhone} onChange={(e) => setOtpPhone(e.target.value)} inputMode="tel" placeholder="Code to +255…" className="sm:col-span-1" />
                )}
                <Button type="button" variant="outline" disabled={working} onClick={handleRequestCode}>
                  {working && !codeSent ? "Sending…" : codeSent ? "Resend code" : "Send code"}
                </Button>
              </div>
              <Input value={code} onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))} required inputMode="numeric" maxLength={6} placeholder="6-digit code" className="mt-2" />
            </div>
            <Button type="submit" disabled={working}>{working ? "Saving…" : dest ? "Change destination (24h cooling)" : "Save destination"}</Button>
          </form>
        )}
      </CardContent>
    </Card>
  );
};

// ---------- Survey tab (individual onboarding answers, editable) ----------

export const SurveyTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  const queryClient = useQueryClient();
  const surveyQuery = useQuery({
    queryKey: ["individual", "account", "survey"],
    queryFn: () => loadIndividualSurvey(),
    staleTime: 30_000,
  });

  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">Onboarding survey</h3>
        <p className="mt-1 text-xs text-slate-500">
          Your launch answers — used for safe starting defaults. They never raise live limits on their own.
        </p>
        {surveyQuery.isLoading ? (
          <p className="mt-3 text-sm text-slate-500">Loading answers…</p>
        ) : !isOwner ? (
          <p className="mt-3 text-sm text-slate-500">Only owners can change these answers.</p>
        ) : (
          <div className="mt-4">
            <IndividualSurveyForm
              account={{ ...org, role: "owner", status: "active" }}
              initial={surveyQuery.data}
              submitLabel="Save answers"
              onSaved={() => queryClient.invalidateQueries({ queryKey: ["individual", "account", "survey"] })}
            />
          </div>
        )}
      </CardContent>
    </Card>
  );
};

// ---------- Support page tab (individual receiving config) ----------

export const SupportPageTab = ({ org, isOwner }: { org: Organization; isOwner: boolean }) => {
  return (
    <Card className="mt-4">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-slate-800">Support page</h3>
        <p className="mt-1 text-xs text-slate-500">
          What supporters see at <span className="font-mono">/c/{org.handle || "…"}</span>. Payments run through the same order path as merchant checkouts.
        </p>
        <SupportPageEditor org={org} isOwner={isOwner} track={isIndividualAccount(org) ? "individual" : "merchant"} />
      </CardContent>
    </Card>
  );
};

// ---------- Danger zone ----------

export const DangerTab = ({ org, isOwner, homePath = "/merchant/apps" }: { org: Organization; isOwner: boolean; homePath?: string }) => {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [deleting, setDeleting] = useState(false);
  const isPersonal = isIndividualAccount(org);

  const handleDelete = async () => {
    if (!window.confirm(`Delete ${isPersonal ? (org?.display_name || "your personal account") : org?.name}? Only empty accounts (no apps) can be deleted.`)) return;
    setDeleting(true);
    try {
      await (isPersonal ? deleteIndividualAccount() : deleteOrg(org.id));
      toast.success(isPersonal ? "Personal account deleted." : "Organization deleted.");
      queryClient.invalidateQueries({ queryKey: isPersonal ? ["individual", "account"] : ["orgs", "mine"] });
      navigate(homePath, { replace: true });
    } catch (err) {
      toast.error(errorMessage(err, isPersonal ? "Unable to delete personal account." : "Unable to delete organization."));
    } finally {
      setDeleting(false);
    }
  };

  return (
    <Card className="mt-4 border-red-200">
      <CardContent className="p-4 sm:p-6">
        <h3 className="text-sm font-bold text-red-700">Danger zone</h3>
        <p className="mt-1 text-xs text-slate-500">
          {isPersonal
            ? "Deleting your personal account is permanent. Only accounts with no apps can be deleted."
            : "Deleting the organization is permanent. Only empty organizations (no apps) can be deleted — delete or move the apps first."}
        </p>
        {isOwner ? (
          <Button variant="destructive" className="mt-4" disabled={deleting} onClick={handleDelete}>
            <Trash2 className="h-3.5 w-3.5 mr-1" />{deleting ? "Deleting..." : isPersonal ? "Delete personal account" : "Delete organization"}
          </Button>
        ) : (
          <p className="mt-4 text-sm text-slate-500">{isPersonal ? "Only the account owner can delete this account." : "Only owners can delete the organization."}</p>
        )}
      </CardContent>
    </Card>
  );
};

// ---------- Page split ----------
//
// The old shared OrgSettings page was split into two distinct pages —
// MerchantSettingsPage (business tabs) and IndividualSettingsPage (personal
// tabs) — composed from the tab building blocks above. There is no shared
// settings page anymore.
