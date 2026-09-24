import { FormEvent, useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { Plus, Copy, Webhook, CreditCard, List, LayoutGrid, Wallet, Percent, ArrowRight, Users, Trash2, KeyRound, MoreVertical, ExternalLink } from "lucide-react";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
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
import {
  DropdownMenu,
  DropdownMenuContent,
  DropdownMenuItem,
  DropdownMenuTrigger,
} from "@/components/ui/dropdown-menu";
import { toast } from "@/components/ui/sonner";
import {
  addAppMember,
  createPaymentApp,
  createPaymentWebhookEndpoint,
  deletePaymentApp,
  generatePaymentAppAPIKey,
  getAppBalance,
  listAppMembers,
  listPaymentApps,
  listPaymentWebhookEndpoints,
  removeAppMember,
  updateAppFees,
  type PaymentApp,
  type UpdateAppFeesInput,
} from "@/lib/adminApi";

const VIEW_MODE_KEY = "admin-payment-apps-view-mode";

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleDateString() : "—";
}

function formatMoney(value: string | undefined, currency: string) {
  if (!value) return "—";
  const n = Number(value);
  return `${Number.isFinite(n) ? n.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 }) : value} ${currency}`;
}

// A negative available balance is a real, meaningful state (a refund
// reversed more than was ever withdrawn — the app now owes AZsubay) rather
// than an error, so it's never blocked, just called out visually.
function isNegativeBalance(value: string | undefined) {
  return typeof value === "string" && Number(value) < 0;
}

function formatFee(app: PaymentApp) {
  if (app.fee_type === "fixed") return `Fixed ${app.fee_fixed}`;
  if (app.fee_type === "hybrid") return `${app.fee_percent}% + ${app.fee_fixed}`;
  return `${app.fee_percent}%`;
}

const AdminPaymentApps = () => {
  const queryClient = useQueryClient();
  const runWithStepUp = <T,>(action: () => Promise<T>) => action();
  const [deletingAppId, setDeletingAppId] = useState<string | null>(null);

  const [appDialogOpen, setAppDialogOpen] = useState(false);
  const [creatingApp, setCreatingApp] = useState(false);
  const [newAppName, setNewAppName] = useState("");
  const [newAppDescription, setNewAppDescription] = useState("");
  const [revealedAppKey, setRevealedAppKey] = useState<string | null>(null);

  const [webhookDialogOpen, setWebhookDialogOpen] = useState(false);
  const [webhookAppId, setWebhookAppId] = useState("");
  const [webhookUrl, setWebhookUrl] = useState("");
  const [webhookEventTypes, setWebhookEventTypes] = useState("payment.updated");
  const [creatingWebhook, setCreatingWebhook] = useState(false);
  const [revealedSigningSecret, setRevealedSigningSecret] = useState<string | null>(null);

  const [feeApp, setFeeApp] = useState<PaymentApp | null>(null);
  const [feeForm, setFeeForm] = useState<UpdateAppFeesInput>({ fee_type: "percentage", fee_percent: "0", fee_fixed: "0" });
  const [savingFees, setSavingFees] = useState(false);

  const [membersApp, setMembersApp] = useState<PaymentApp | null>(null);
  const [newMemberEmail, setNewMemberEmail] = useState("");
  const [newMemberRole, setNewMemberRole] = useState("developer");
  const [addingMember, setAddingMember] = useState(false);
  const [removingMemberId, setRemovingMemberId] = useState<string | null>(null);

  const [apiKeyApp, setApiKeyApp] = useState<PaymentApp | null>(null);
  const [revealedApiKey, setRevealedApiKey] = useState<string | null>(null);
  const [generatingKey, setGeneratingKey] = useState(false);

  const [viewMode, setViewMode] = useState<"list" | "grid">(() => {
    if (typeof window === "undefined") return "grid";
    return (window.localStorage.getItem(VIEW_MODE_KEY) as "list" | "grid" | null) ?? "grid";
  });

  useEffect(() => {
    window.localStorage.setItem(VIEW_MODE_KEY, viewMode);
  }, [viewMode]);

  const appsQuery = useQuery({
    queryKey: ["admin", "payments-apps"],
    queryFn: () => listPaymentApps().then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const endpointsQuery = useQuery({
    queryKey: ["admin", "payments-webhook-endpoints"],
    queryFn: () => listPaymentWebhookEndpoints().then((data) => (Array.isArray(data) ? data : [])),
    staleTime: 30_000,
  });

  const apps = appsQuery.data ?? [];
  const endpoints = endpointsQuery.data ?? [];
  const loading = appsQuery.isLoading;

  const balanceQueries = useQueries({
    queries: apps.map((app) => ({
      queryKey: ["admin", "payment-app-balance", app.id],
      queryFn: () => getAppBalance(app.id),
      staleTime: 30_000,
    })),
  });
  const balanceByAppId = new Map(apps.map((app, i) => [app.id, balanceQueries[i]?.data]));

  useEffect(() => {
    if (appsQuery.error) toast.error(appsQuery.error instanceof Error ? appsQuery.error.message : "Unable to load payment apps.");
  }, [appsQuery.error]);

  const reload = () => {
    queryClient.invalidateQueries({ queryKey: ["admin", "payments-apps"] });
    queryClient.invalidateQueries({ queryKey: ["admin", "payments-webhook-endpoints"] });
    queryClient.invalidateQueries({ queryKey: ["admin", "payment-app-balance"] });
  };

  // Soft delete only — the app's order/ledger/withdrawal history is kept,
  // it just stops accepting new orders and drops off this list. Gated
  // behind step-up MFA like every other irreversible-feeling admin action.
  const handleDeleteApp = async (app: PaymentApp) => {
    if (!confirm(`Delete "${app.name}"? It will stop accepting orders and disappear from this list. Its order and ledger history is kept.`)) return;
    setDeletingAppId(app.id);
    try {
      await runWithStepUp(() => deletePaymentApp(app.id));
      toast.success(`${app.name} deleted.`);
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to delete app.");
    } finally {
      setDeletingAppId(null);
    }
  };

  const openFeeDialog = (app: PaymentApp) => {
    setFeeApp(app);
    setFeeForm({ fee_type: app.fee_type, fee_percent: app.fee_percent, fee_fixed: app.fee_fixed });
  };

  const handleSaveFees = async (event: FormEvent) => {
    event.preventDefault();
    if (!feeApp) return;
    setSavingFees(true);
    try {
      await updateAppFees(feeApp.id, feeForm);
      toast.success("Fees updated.");
      setFeeApp(null);
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to update fees.");
    } finally {
      setSavingFees(false);
    }
  };

  const membersQuery = useQuery({
    queryKey: ["admin", "payment-app-members", membersApp?.id],
    queryFn: () => listAppMembers(membersApp!.id),
    enabled: !!membersApp,
    staleTime: 15_000,
  });
  const members = membersQuery.data ?? [];

  const openMembersDialog = (app: PaymentApp) => {
    setMembersApp(app);
    setNewMemberEmail("");
  };

  const handleAddMember = async (event: FormEvent) => {
    event.preventDefault();
    if (!membersApp) return;
    setAddingMember(true);
    try {
      await addAppMember(membersApp.id, newMemberEmail.trim(), newMemberRole);
      toast.success("Member added.");
      setNewMemberEmail("");
      queryClient.invalidateQueries({ queryKey: ["admin", "payment-app-members", membersApp.id] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to add member.");
    } finally {
      setAddingMember(false);
    }
  };

  const handleRemoveMember = async (userId: string) => {
    if (!membersApp) return;
    setRemovingMemberId(userId);
    try {
      await removeAppMember(membersApp.id, userId);
      toast.success("Member removed.");
      queryClient.invalidateQueries({ queryKey: ["admin", "payment-app-members", membersApp.id] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to remove member.");
    } finally {
      setRemovingMemberId(null);
    }
  };

  const handleCreateApp = async (event: FormEvent) => {
    event.preventDefault();
    setCreatingApp(true);
    try {
      const result = await createPaymentApp(newAppName.trim(), newAppDescription.trim());
      setRevealedAppKey(result.api_key);
      toast.success("Payment app created.");
      setNewAppName("");
      setNewAppDescription("");
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to create payment app.");
    } finally {
      setCreatingApp(false);
    }
  };

  const openWebhookDialog = (appId: string) => {
    setWebhookAppId(appId);
    setWebhookUrl("");
    setWebhookEventTypes("payment.updated");
    setRevealedSigningSecret(null);
    setWebhookDialogOpen(true);
  };

  const handleCreateWebhook = async (event: FormEvent) => {
    event.preventDefault();
    setCreatingWebhook(true);
    try {
      const eventTypes = webhookEventTypes.split(",").map((t) => t.trim()).filter(Boolean);
      const result = await createPaymentWebhookEndpoint(webhookAppId, webhookUrl.trim(), eventTypes);
      setRevealedSigningSecret(result.signing_secret);
      toast.success("Webhook endpoint created.");
      reload();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to create webhook endpoint.");
    } finally {
      setCreatingWebhook(false);
    }
  };

  const webhookCountByApp = (appId: string) => endpoints.filter((e) => e.app_id === appId).length;

  const openApiKeyDialog = (app: PaymentApp) => {
    setApiKeyApp(app);
    setRevealedApiKey(null);
  };

  const handleGenerateApiKey = async () => {
    if (!apiKeyApp) return;
    setGeneratingKey(true);
    try {
      const result = await runWithStepUp(() => generatePaymentAppAPIKey(apiKeyApp.id));
      setRevealedApiKey(result.api_key);
      toast.success("API key generated. Any previous key for this app is now revoked.");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to generate API key.");
    } finally {
      setGeneratingKey(false);
    }
  };

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Payment Apps</h2>
          <p className="mt-1 text-sm text-slate-500">Every app registered against the AZsubay Payments Gateway.</p>
        </div>
        <div className="flex items-center gap-2">
          <Button variant="outline" asChild>
            <Link to="/admin/payments/withdrawals">
              <Wallet className="h-4 w-4 mr-1.5" /> Withdrawals <ArrowRight className="h-3.5 w-3.5 ml-1.5" />
            </Link>
          </Button>
          <div className="flex items-center rounded-md border border-slate-200 p-0.5">
            <Button size="icon" variant={viewMode === "grid" ? "default" : "ghost"} className="h-8 w-8" title="Grid view" onClick={() => setViewMode("grid")}>
              <LayoutGrid className="h-4 w-4" />
            </Button>
            <Button size="icon" variant={viewMode === "list" ? "default" : "ghost"} className="h-8 w-8" title="List view" onClick={() => setViewMode("list")}>
              <List className="h-4 w-4" />
            </Button>
          </div>
          <Dialog open={appDialogOpen} onOpenChange={(open) => { setAppDialogOpen(open); if (!open) setRevealedAppKey(null); }}>
            <DialogTrigger asChild>
              <Button><Plus className="h-4 w-4 mr-1" /> New app</Button>
            </DialogTrigger>
            <DialogContent className="max-h-[85vh] overflow-y-auto">
              <DialogHeader>
                <DialogTitle>Register payment app</DialogTitle>
              </DialogHeader>
              {revealedAppKey ? (
                <div className="space-y-3">
                  <p className="text-sm text-slate-600">Save this API key now — it will not be shown again.</p>
                  <div className="flex items-center gap-2">
                    <Input readOnly value={revealedAppKey} className="font-mono text-xs" />
                    <Button
                      type="button"
                      size="icon"
                      variant="outline"
                      onClick={() => { navigator.clipboard.writeText(revealedAppKey); toast.success("Copied"); }}
                    >
                      <Copy className="h-4 w-4" />
                    </Button>
                  </div>
                </div>
              ) : (
                <form onSubmit={handleCreateApp} className="space-y-4">
                  <div>
                    <label className="text-sm font-medium text-slate-700">Name</label>
                    <Input value={newAppName} onChange={(e) => setNewAppName(e.target.value)} required className="mt-1" />
                  </div>
                  <div>
                    <label className="text-sm font-medium text-slate-700">Description</label>
                    <Input value={newAppDescription} onChange={(e) => setNewAppDescription(e.target.value)} className="mt-1" />
                  </div>
                  <DialogFooter>
                    <Button type="submit" disabled={creatingApp}>{creatingApp ? "Creating..." : "Create"}</Button>
                  </DialogFooter>
                </form>
              )}
            </DialogContent>
          </Dialog>
        </div>
      </div>

      {viewMode === "grid" ? (
        <div className="mt-6 grid grid-cols-1 gap-4 sm:grid-cols-2 lg:grid-cols-3">
          {loading &&
            Array.from({ length: 6 }).map((_, i) => (
              <Card key={`skeleton-${i}`}>
                <CardContent className="p-4 space-y-3">
                  <div className="flex items-center gap-3">
                    <Skeleton className="h-10 w-10 rounded-xl" />
                    <div className="flex-1 space-y-1.5">
                      <Skeleton className="h-4 w-24" />
                      <Skeleton className="h-3 w-32" />
                    </div>
                  </div>
                  <Skeleton className="h-8 w-full" />
                </CardContent>
              </Card>
            ))}
          {!loading &&
            apps.map((app) => {
              const balance = balanceByAppId.get(app.id);
              return (
                <Card key={app.id} className="transition-shadow hover:shadow-md">
                  <CardContent className="p-4">
                    <div className="flex items-start gap-3">
                      <div className="flex h-10 w-10 shrink-0 items-center justify-center rounded-xl bg-emerald-50 text-emerald-600">
                        <CreditCard size={18} />
                      </div>
                      <div className="min-w-0 flex-1">
                        <p className="truncate text-sm font-semibold text-slate-900">{app.name}</p>
                        <p className="truncate text-xs text-slate-500">{app.description || "No description"}</p>
                      </div>
                    </div>

                    <div className="mt-3 rounded-xl bg-slate-50 px-3 py-2.5">
                      <p className="text-[11px] font-semibold uppercase tracking-wide text-slate-400">Available balance</p>
                      <p className={`mt-0.5 text-lg font-extrabold ${balance && isNegativeBalance(balance.available_balance) ? "text-red-600" : "text-slate-900"}`}>
                        {balance ? formatMoney(balance.available_balance, balance.currency) : <Skeleton className="h-6 w-24" />}
                      </p>
                    </div>

                    <div className="mt-3 flex flex-wrap items-center gap-1.5">
                      <Badge variant="secondary">{app.status}</Badge>
                      <Badge variant="outline">{webhookCountByApp(app.id)} webhook{webhookCountByApp(app.id) === 1 ? "" : "s"}</Badge>
                      <Badge variant="outline" className="gap-1"><Percent size={11} /> {formatFee(app)}</Badge>
                    </div>
                    <p className="mt-3 text-xs text-slate-400">Registered {formatDate(app.created_at)}</p>

                    <div className="mt-3 grid grid-cols-2 gap-1.5">
                      <Button size="sm" variant="outline" asChild>
                        <Link to={`/admin/payments/apps/${app.id}`}>
                          <ExternalLink className="h-3.5 w-3.5 mr-1" /> View
                        </Link>
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => openApiKeyDialog(app)}>
                        <KeyRound className="h-3.5 w-3.5 mr-1" /> API key
                      </Button>
                    </div>
                    <div className="mt-1.5 flex items-center justify-between gap-1.5">
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button size="sm" variant="ghost" className="flex-1 justify-start text-slate-500">
                            <MoreVertical className="h-3.5 w-3.5 mr-1.5" /> More
                          </Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="start">
                          <DropdownMenuItem onClick={() => openMembersDialog(app)}>
                            <Users className="h-3.5 w-3.5 mr-2" /> Members
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => openFeeDialog(app)}>
                            <Percent className="h-3.5 w-3.5 mr-2" /> Fees
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => openWebhookDialog(app.id)}>
                            <Webhook className="h-3.5 w-3.5 mr-2" /> Add webhook
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                      <Button
                        size="sm"
                        variant="ghost"
                        className="text-red-600 hover:bg-red-50 hover:text-red-700"
                        disabled={deletingAppId === app.id}
                        onClick={() => handleDeleteApp(app)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </CardContent>
                </Card>
              );
            })}
          {!loading && apps.length === 0 && <p className="col-span-full text-center text-sm text-slate-500">No payment apps yet.</p>}
        </div>
      ) : (
        <Table className="mt-6">
          <TableHeader>
            <TableRow>
              <TableHead>Name</TableHead>
              <TableHead>Balance</TableHead>
              <TableHead>Fee</TableHead>
              <TableHead>Status</TableHead>
              <TableHead>Webhooks</TableHead>
              <TableHead>Created</TableHead>
              <TableHead></TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {apps.map((app) => {
              const balance = balanceByAppId.get(app.id);
              return (
                <TableRow key={app.id}>
                  <TableCell className="font-medium">
                    <Link to={`/admin/payments/apps/${app.id}`} className="hover:underline">{app.name}</Link>
                  </TableCell>
                  <TableCell className={balance && isNegativeBalance(balance.available_balance) ? "text-red-600 font-medium" : undefined}>
                    {balance ? formatMoney(balance.available_balance, balance.currency) : <Skeleton className="h-4 w-20" />}
                  </TableCell>
                  <TableCell className="text-xs text-slate-500">{formatFee(app)}</TableCell>
                  <TableCell><Badge variant="secondary">{app.status}</Badge></TableCell>
                  <TableCell>{webhookCountByApp(app.id)}</TableCell>
                  <TableCell>{formatDate(app.created_at)}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1.5">
                      <Button size="sm" variant="outline" onClick={() => openApiKeyDialog(app)}>
                        <KeyRound className="h-3.5 w-3.5 mr-1" /> API key
                      </Button>
                      <DropdownMenu>
                        <DropdownMenuTrigger asChild>
                          <Button size="sm" variant="outline"><MoreVertical className="h-3.5 w-3.5" /></Button>
                        </DropdownMenuTrigger>
                        <DropdownMenuContent align="end">
                          <DropdownMenuItem onClick={() => openMembersDialog(app)}>
                            <Users className="h-3.5 w-3.5 mr-2" /> Members
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => openFeeDialog(app)}>
                            <Percent className="h-3.5 w-3.5 mr-2" /> Fees
                          </DropdownMenuItem>
                          <DropdownMenuItem onClick={() => openWebhookDialog(app.id)}>
                            <Webhook className="h-3.5 w-3.5 mr-2" /> Add webhook
                          </DropdownMenuItem>
                        </DropdownMenuContent>
                      </DropdownMenu>
                      <Button
                        size="sm"
                        variant="outline"
                        className="text-red-600 hover:bg-red-50 hover:text-red-700"
                        disabled={deletingAppId === app.id}
                        onClick={() => handleDeleteApp(app)}
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              );
            })}
            {!loading && apps.length === 0 && (
              <TableRow><TableCell colSpan={7} className="text-center text-slate-500">No payment apps yet.</TableCell></TableRow>
            )}
          </TableBody>
        </Table>
      )}

      <Dialog open={webhookDialogOpen} onOpenChange={(open) => { setWebhookDialogOpen(open); if (!open) setRevealedSigningSecret(null); }}>
        <DialogContent className="max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Add webhook endpoint</DialogTitle>
          </DialogHeader>
          {revealedSigningSecret ? (
            <div className="space-y-3">
              <p className="text-sm text-slate-600">Save this signing secret now — it will not be shown again.</p>
              <div className="flex items-center gap-2">
                <Input readOnly value={revealedSigningSecret} className="font-mono text-xs" />
                <Button
                  type="button"
                  size="icon"
                  variant="outline"
                  onClick={() => { navigator.clipboard.writeText(revealedSigningSecret); toast.success("Copied"); }}
                >
                  <Copy className="h-4 w-4" />
                </Button>
              </div>
            </div>
          ) : (
            <form onSubmit={handleCreateWebhook} className="space-y-4">
              <div>
                <label className="text-sm font-medium text-slate-700">Webhook URL</label>
                <Input value={webhookUrl} onChange={(e) => setWebhookUrl(e.target.value)} required type="url" placeholder="https://..." className="mt-1" />
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Event types</label>
                <Input value={webhookEventTypes} onChange={(e) => setWebhookEventTypes(e.target.value)} className="mt-1" />
                <p className="mt-1 text-xs text-slate-500">Comma-separated, e.g. payment.updated</p>
              </div>
              <DialogFooter>
                <Button type="submit" disabled={creatingWebhook}>{creatingWebhook ? "Creating..." : "Create endpoint"}</Button>
              </DialogFooter>
            </form>
          )}
        </DialogContent>
      </Dialog>

      <Dialog open={!!feeApp} onOpenChange={(open) => !open && setFeeApp(null)}>
        <DialogContent className="max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Fees — {feeApp?.name}</DialogTitle>
          </DialogHeader>
          <form onSubmit={handleSaveFees} className="space-y-4">
            <div>
              <label className="text-sm font-medium text-slate-700">Fee type</label>
              <select
                className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                value={feeForm.fee_type}
                onChange={(e) => setFeeForm((f) => ({ ...f, fee_type: e.target.value as UpdateAppFeesInput["fee_type"] }))}
              >
                <option value="percentage">Percentage</option>
                <option value="fixed">Fixed</option>
                <option value="hybrid">Fixed + percentage</option>
              </select>
            </div>
            {feeForm.fee_type !== "fixed" && (
              <div>
                <label className="text-sm font-medium text-slate-700">Percent (%)</label>
                <Input
                  value={feeForm.fee_percent}
                  onChange={(e) => setFeeForm((f) => ({ ...f, fee_percent: e.target.value }))}
                  inputMode="decimal"
                  className="mt-1"
                />
              </div>
            )}
            {feeForm.fee_type !== "percentage" && (
              <div>
                <label className="text-sm font-medium text-slate-700">Fixed amount</label>
                <Input
                  value={feeForm.fee_fixed}
                  onChange={(e) => setFeeForm((f) => ({ ...f, fee_fixed: e.target.value }))}
                  inputMode="decimal"
                  className="mt-1"
                />
              </div>
            )}
            <p className="text-xs text-slate-500">
              Charged automatically on every payment that settles as paid. Existing settled payments are never recalculated.
            </p>
            <DialogFooter>
              <Button type="submit" disabled={savingFees}>{savingFees ? "Saving..." : "Save fees"}</Button>
            </DialogFooter>
          </form>
        </DialogContent>
      </Dialog>

      <Dialog open={!!membersApp} onOpenChange={(open) => !open && setMembersApp(null)}>
        <DialogContent className="max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>Members — {membersApp?.name}</DialogTitle>
          </DialogHeader>

          <div className="space-y-2">
            {membersQuery.isLoading && <Skeleton className="h-14 w-full" />}
            {!membersQuery.isLoading && members.length === 0 && (
              <p className="rounded-lg bg-slate-50 px-3 py-4 text-center text-sm text-slate-500">No members yet.</p>
            )}
            {members.map((member) => (
              <div key={member.user_id} className="flex items-center justify-between gap-2 rounded-lg border border-slate-200 px-3 py-2.5">
                <div className="min-w-0">
                  <p className="truncate text-sm font-medium text-slate-900">{member.full_name || member.email}</p>
                  <p className="truncate text-xs text-slate-500">{member.email}</p>
                </div>
                <div className="flex shrink-0 items-center gap-2">
                  <Badge variant="secondary">{member.role}</Badge>
                  {member.status === "invited" && <Badge variant="outline">invited</Badge>}
                  <Button
                    size="icon"
                    variant="outline"
                    disabled={removingMemberId === member.user_id}
                    onClick={() => handleRemoveMember(member.user_id)}
                  >
                    <Trash2 className="h-3.5 w-3.5" />
                  </Button>
                </div>
              </div>
            ))}
          </div>

          <form onSubmit={handleAddMember} className="flex items-end gap-2 border-t border-slate-100 pt-4">
            <div className="flex-1">
              <label className="text-sm font-medium text-slate-700">Add by email</label>
              <Input
                type="email"
                value={newMemberEmail}
                onChange={(e) => setNewMemberEmail(e.target.value)}
                required
                placeholder="owner@example.com"
                className="mt-1"
              />
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Role</label>
              <select
                className="mt-1 h-10 rounded-md border border-slate-300 px-2 text-sm"
                value={newMemberRole}
                onChange={(e) => setNewMemberRole(e.target.value)}
              >
                <option value="developer">developer</option>
                <option value="finance">finance</option>
                <option value="viewer">viewer</option>
                <option value="owner">owner</option>
              </select>
            </div>
            <Button type="submit" disabled={addingMember}>{addingMember ? "Adding..." : "Add"}</Button>
          </form>
          <p className="text-xs text-slate-500">
            The email must already have an AZsubay account. If it doesn't, ask them to sign up first.
          </p>
        </DialogContent>
      </Dialog>

      <Dialog open={!!apiKeyApp} onOpenChange={(open) => { if (!open) setApiKeyApp(null); }}>
        <DialogContent className="max-h-[85vh] overflow-y-auto">
          <DialogHeader>
            <DialogTitle>{apiKeyApp?.name} — API key</DialogTitle>
          </DialogHeader>
          <div className="space-y-4">
            <p className="text-sm text-slate-600">
              This key authenticates <strong>{apiKeyApp?.name}</strong> against the AZSUBAY Payments Gateway
              (order creation, status checks — sent as the <code className="text-xs">X-Api-Key</code> header). See
              README_PAYMENTS.md for the full integration guide. Generating a new key moves usable keys
              into a 24h grace period instead of revoking them instantly.
            </p>
            {revealedApiKey ? (
              <div className="space-y-3">
                <p className="text-sm font-medium text-slate-700">Save this API key now — it will not be shown again.</p>
                <div className="flex items-center gap-2">
                  <Input readOnly value={revealedApiKey} className="font-mono text-xs" />
                  <Button
                    type="button"
                    size="icon"
                    variant="outline"
                    onClick={() => { navigator.clipboard.writeText(revealedApiKey); toast.success("Copied"); }}
                  >
                    <Copy className="h-4 w-4" />
                  </Button>
                </div>
              </div>
            ) : (
              <DialogFooter>
                <Button type="button" disabled={generatingKey} onClick={handleGenerateApiKey}>
                  {generatingKey ? "Generating..." : "Generate new key"}
                </Button>
              </DialogFooter>
            )}
          </div>
        </DialogContent>
      </Dialog>
    </div>
  );
};

export default AdminPaymentApps;
