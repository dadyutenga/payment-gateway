import { FormEvent, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, KeyRound, RotateCcw, Send, Trash2, Ban, Truck, RefreshCw, Plus, Check, X, Wallet } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
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
  approveMerchantWithdrawal,
  createMerchantEndpoint,
  createMerchantKey,
  createMerchantWithdrawal,
  deleteMerchantEndpoint,
  listMerchantDeliveries,
  listMerchantEndpoints,
  listMerchantKeys,
  listMerchantWithdrawals,
  listMyApps,
  rejectMerchantWithdrawal,
  replayMerchantDelivery,
  revokeMerchantKey,
  rotateMerchantEndpointSecret,
  rotateMerchantKey,
  testMerchantEndpoint,
  updateMerchantApp,
  updateMerchantEndpoint,
  updateMerchantKeyLabel,
  MerchantApiError,
  type MerchantAPIKey,
  type MerchantTestSendResult,
  type MerchantWebhookEndpoint,
} from "@/lib/merchantApi";

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : "—";
}

function errorMessage(err: unknown, fallback: string) {
  return err instanceof Error ? err.message : fallback;
}

const MerchantAppDetail = () => {
  const { id: appId = "" } = useParams();
  const queryClient = useQueryClient();

  const appsQuery = useQuery({ queryKey: ["merchant", "my-apps"], queryFn: () => listMyApps(), staleTime: 30_000 });
  const app = appsQuery.data?.find((a) => a.id === appId);
  const appName = app?.name ?? "App";

  const [renameDialogOpen, setRenameDialogOpen] = useState(false);
  const [renameName, setRenameName] = useState("");
  const [renameDescription, setRenameDescription] = useState("");
  const [savingRename, setSavingRename] = useState(false);

  const endpointsKey = ["merchant", appId, "endpoints"];
  const keysKey = ["merchant", appId, "keys"];
  const deliveriesKey = ["merchant", appId, "deliveries"];
  const withdrawalsKey = ["merchant", appId, "withdrawals"];

  const endpointsQuery = useQuery({ queryKey: endpointsKey, queryFn: () => listMerchantEndpoints(appId), staleTime: 15_000 });
  const keysQuery = useQuery({ queryKey: keysKey, queryFn: () => listMerchantKeys(appId), staleTime: 15_000 });
  const deliveriesQuery = useQuery({ queryKey: deliveriesKey, queryFn: () => listMerchantDeliveries(appId), staleTime: 15_000 });
  const withdrawalsQuery = useQuery({ queryKey: withdrawalsKey, queryFn: () => listMerchantWithdrawals(appId), staleTime: 15_000 });

  const endpoints = endpointsQuery.data ?? [];
  const keys = keysQuery.data ?? [];
  const deliveries = deliveriesQuery.data ?? [];
  const withdrawals = withdrawalsQuery.data ?? [];

  const [endpointDialogOpen, setEndpointDialogOpen] = useState(false);
  const [endpointUrl, setEndpointUrl] = useState("");
  const [endpointTypes, setEndpointTypes] = useState("payment.updated, payment.refunded, payment.expired");
  const [savingEndpoint, setSavingEndpoint] = useState(false);
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);

  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const [keyEnv, setKeyEnv] = useState("live");
  const [keyLabel, setKeyLabel] = useState("");
  const [creatingKey, setCreatingKey] = useState(false);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const [actingKeyId, setActingKeyId] = useState<string | null>(null);
  const [labelKey, setLabelKey] = useState<MerchantAPIKey | null>(null);
  const [labelValue, setLabelValue] = useState("");
  const [savingLabel, setSavingLabel] = useState(false);

  const [testingEndpointId, setTestingEndpointId] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<MerchantTestSendResult | null>(null);
  const [rotatingSecretId, setRotatingSecretId] = useState<string | null>(null);
  const [revealedEndpointSecret, setRevealedEndpointSecret] = useState<string | null>(null);
  const [replayingId, setReplayingId] = useState<string | null>(null);
  const [togglingId, setTogglingId] = useState<string | null>(null);

  const [withdrawalDialogOpen, setWithdrawalDialogOpen] = useState(false);
  const [withdrawalAmount, setWithdrawalAmount] = useState("");
  const [withdrawalNotes, setWithdrawalNotes] = useState("");
  const [withdrawalDestType, setWithdrawalDestType] = useState<"bank" | "mobile_money">("bank");
  const [withdrawalBankName, setWithdrawalBankName] = useState("");
  const [withdrawalAccountName, setWithdrawalAccountName] = useState("");
  const [withdrawalAccountNumber, setWithdrawalAccountNumber] = useState("");
  const [withdrawalProvider, setWithdrawalProvider] = useState("");
  const [withdrawalPhone, setWithdrawalPhone] = useState("");
  const [creatingWithdrawal, setCreatingWithdrawal] = useState(false);
  const [actingWithdrawalId, setActingWithdrawalId] = useState<string | null>(null);

  const handleCreateWithdrawal = async (event: FormEvent) => {
    event.preventDefault();
    setCreatingWithdrawal(true);
    try {
      await createMerchantWithdrawal(appId, {
        amount: withdrawalAmount,
        currency: "TZS",
        destination_type: withdrawalDestType,
        destination_details:
          withdrawalDestType === "mobile_money"
            ? { provider: withdrawalProvider, phone: withdrawalPhone }
            : { bank_name: withdrawalBankName, account_name: withdrawalAccountName, account_number: withdrawalAccountNumber },
        notes: withdrawalNotes || undefined,
      });
      toast.success("Withdrawal requested.");
      setWithdrawalDialogOpen(false);
      setWithdrawalAmount("");
      setWithdrawalNotes("");
      setWithdrawalBankName("");
      setWithdrawalAccountName("");
      setWithdrawalAccountNumber("");
      setWithdrawalProvider("");
      setWithdrawalPhone("");
      reloadAll();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to request withdrawal."));
    } finally {
      setCreatingWithdrawal(false);
    }
  };

  const runWithdrawalAction = async (id: string, action: () => Promise<unknown>, successMessage: string) => {
    setActingWithdrawalId(id);
    try {
      await action();
      toast.success(successMessage);
      reloadAll();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update withdrawal."));
    } finally {
      setActingWithdrawalId(null);
    }
  };

  function withdrawalDestinationSummary(w: { destination_type: string; destination_details: Record<string, unknown> }) {
    const d = w.destination_details || {};
    if (w.destination_type === "mobile_money") {
      return [d.provider, d.phone].filter(Boolean).join(" · ") || "Mobile money";
    }
    return [d.bank_name, d.account_number].filter(Boolean).join(" · ") || "Bank transfer";
  }

  const reloadAll = () => {
    queryClient.invalidateQueries({ queryKey: endpointsKey });
    queryClient.invalidateQueries({ queryKey: keysKey });
    queryClient.invalidateQueries({ queryKey: deliveriesKey });
    queryClient.invalidateQueries({ queryKey: withdrawalsKey });
  };

  const handleCreateEndpoint = async (event: FormEvent) => {
    event.preventDefault();
    setSavingEndpoint(true);
    setRevealedSecret(null);
    try {
      const result = await createMerchantEndpoint(appId, {
        url: endpointUrl,
        event_types: endpointTypes.split(",").map((s) => s.trim()).filter(Boolean),
      });
      setRevealedSecret(result.signing_secret);
      toast.success("Webhook endpoint created.");
      setEndpointUrl("");
      reloadAll();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to create endpoint."));
    } finally {
      setSavingEndpoint(false);
    }
  };

  const handleToggleEndpoint = async (endpoint: MerchantWebhookEndpoint) => {
    setTogglingId(endpoint.id);
    try {
      await updateMerchantEndpoint(appId, endpoint.id, { status: endpoint.status === "active" ? "disabled" : "active" });
      toast.success(endpoint.status === "active" ? "Endpoint disabled." : "Endpoint enabled.");
      queryClient.invalidateQueries({ queryKey: endpointsKey });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update endpoint."));
    } finally {
      setTogglingId(null);
    }
  };

  const handleDeleteEndpoint = async (endpoint: MerchantWebhookEndpoint) => {
    if (!window.confirm(`Delete webhook endpoint ${endpoint.url}? Deliveries to it stop immediately.`)) return;
    try {
      await deleteMerchantEndpoint(appId, endpoint.id);
      toast.success("Endpoint deleted.");
      queryClient.invalidateQueries({ queryKey: endpointsKey });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to delete endpoint."));
    }
  };

  const handleTestEndpoint = async (endpoint: MerchantWebhookEndpoint) => {
    setTestingEndpointId(endpoint.id);
    setTestResult(null);
    try {
      const result = await testMerchantEndpoint(appId, endpoint.id);
      setTestResult(result);
      if (result.ok) {
        toast.success(`Endpoint answered ${result.status_code}.`);
      } else {
        toast.error(result.error || `Endpoint answered ${result.status_code}.`);
      }
    } catch (err) {
      toast.error(errorMessage(err, "Unable to test endpoint."));
    } finally {
      setTestingEndpointId(null);
    }
  };

  const handleRename = async (event: FormEvent) => {
    event.preventDefault();
    setSavingRename(true);
    try {
      await updateMerchantApp(appId, { name: renameName.trim(), description: renameDescription.trim() || undefined });
      toast.success("App renamed.");
      setRenameDialogOpen(false);
      queryClient.invalidateQueries({ queryKey: ["merchant", "my-apps"] });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to rename app."));
    } finally {
      setSavingRename(false);
    }
  };

  const handleCreateKey = async (event: FormEvent) => {
    event.preventDefault();
    setCreatingKey(true);
    setRevealedKey(null);
    try {
      const result = await createMerchantKey(appId, { environment: keyEnv, label: keyLabel.trim() || undefined });
      setRevealedKey(result.api_key);
      toast.success("API key created — copy it now, it is never shown again.");
      setKeyLabel("");
      reloadAll();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to create API key."));
    } finally {
      setCreatingKey(false);
    }
  };

  const handleRotateKey = async (key: MerchantAPIKey) => {
    if (!window.confirm(`Rotate key ${key.prefix}…? Usable keys stay valid for a 24h grace period.`)) return;
    setActingKeyId(key.id);
    setRevealedKey(null);
    try {
      const result = await rotateMerchantKey(appId, key.id);
      setRevealedKey(result.api_key);
      toast.success("New key issued — old keys keep working for 24h.");
      reloadAll();
    } catch (err) {
      toast.error(errorMessage(err, "Unable to rotate API key."));
    } finally {
      setActingKeyId(null);
    }
  };

  const handleRevokeKey = async (key: MerchantAPIKey) => {    if (!window.confirm(`Revoke key ${key.prefix}… immediately? In-flight traffic with it stops.`)) return;
    setActingKeyId(key.id);
    try {
      await revokeMerchantKey(appId, key.id);
      toast.success("API key revoked.");
      queryClient.invalidateQueries({ queryKey: keysKey });
    } catch (err) {
      if (err instanceof MerchantApiError && err.code === "not_found") {
        toast.error("API key not found.");
      } else {
        toast.error(errorMessage(err, "Unable to revoke API key."));
      }
    } finally {
      setActingKeyId(null);
    }
  };

  const openLabelDialog = (key: MerchantAPIKey) => {
    setLabelKey(key);
    setLabelValue(key.label ?? "");
  };

  const handleSaveLabel = async (event: FormEvent) => {
    event.preventDefault();
    if (!labelKey) return;
    setSavingLabel(true);
    try {
      await updateMerchantKeyLabel(appId, labelKey.id, labelValue.trim());
      toast.success("Key label updated.");
      setLabelKey(null);
      queryClient.invalidateQueries({ queryKey: keysKey });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to update label."));
    } finally {
      setSavingLabel(false);
    }
  };

  const handleRotateEndpointSecret = async (endpoint: MerchantWebhookEndpoint) => {
    if (!window.confirm(`Rotate the signing secret for ${endpoint.url}? The old secret stops verifying immediately — update your receiver first.`)) return;
    setRotatingSecretId(endpoint.id);
    setRevealedEndpointSecret(null);
    try {
      const result = await rotateMerchantEndpointSecret(appId, endpoint.id);
      setRevealedEndpointSecret(result.signing_secret);
      toast.success(`Signing secret rotated (now v${result.endpoint.secret_version}) — copy it now.`);
      queryClient.invalidateQueries({ queryKey: endpointsKey });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to rotate signing secret."));
    } finally {
      setRotatingSecretId(null);
    }
  };

  const handleReplay = async (deliveryId: string) => {    setReplayingId(deliveryId);
    try {
      await replayMerchantDelivery(appId, deliveryId);
      toast.success("Delivery re-queued.");
      queryClient.invalidateQueries({ queryKey: deliveriesKey });
    } catch (err) {
      toast.error(errorMessage(err, "Unable to replay delivery."));
    } finally {
      setReplayingId(null);
    }
  };

  return (
    <div>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">{appName}</h2>
          <p className="mt-1 text-sm text-slate-500">
            Merchant self-service — everything here applies to this app only.
            {app?.description ? ` ${app.description}` : ""}
          </p>
        </div>
        <Dialog open={renameDialogOpen} onOpenChange={(open) => {
          setRenameDialogOpen(open);
          if (open && app) { setRenameName(app.name); setRenameDescription(app.description ?? ""); }
        }}>
          <DialogTrigger asChild>
            <Button size="sm" variant="outline">Rename</Button>
          </DialogTrigger>
          <DialogContent className="max-h-[85vh] overflow-y-auto">
            <DialogHeader>
              <DialogTitle>Rename app</DialogTitle>
            </DialogHeader>
            <form onSubmit={handleRename} className="space-y-4">
              <div>
                <label className="text-sm font-medium text-slate-700">Name</label>
                <Input value={renameName} onChange={(e) => setRenameName(e.target.value)} required maxLength={100} className="mt-1" />
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Description</label>
                <Input value={renameDescription} onChange={(e) => setRenameDescription(e.target.value)} maxLength={500} className="mt-1" />
              </div>
              <DialogFooter>
                <Button type="submit" disabled={savingRename}>{savingRename ? "Saving..." : "Save"}</Button>
              </DialogFooter>
            </form>
          </DialogContent>
        </Dialog>
      </div>

      <Tabs defaultValue="webhooks" className="mt-6">
        <TabsList>
          <TabsTrigger value="webhooks">Webhooks ({endpoints.length})</TabsTrigger>
          <TabsTrigger value="keys">API keys ({keys.length})</TabsTrigger>
          <TabsTrigger value="withdrawals">Withdrawals ({withdrawals.length})</TabsTrigger>
          <TabsTrigger value="deliveries">Deliveries ({deliveries.length})</TabsTrigger>
        </TabsList>

        <TabsContent value="webhooks">
          <div className="mb-3 flex justify-end">
            <Dialog open={endpointDialogOpen} onOpenChange={(open) => { setEndpointDialogOpen(open); if (!open) setRevealedSecret(null); }}>
              <DialogTrigger asChild>
                <Button size="sm"><Globe className="h-3.5 w-3.5 mr-1" /> Add endpoint</Button>
              </DialogTrigger>
              <DialogContent className="max-h-[85vh] overflow-y-auto">
                <DialogHeader>
                  <DialogTitle>Add webhook endpoint</DialogTitle>
                </DialogHeader>
                {revealedSecret ? (
                  <div className="space-y-3">
                    <p className="text-sm text-slate-600">Signing secret — copy it now, it is never shown again.</p>
                    <code className="block break-all rounded-md bg-slate-100 p-3 font-mono text-xs">{revealedSecret}</code>
                    <DialogFooter>
                      <Button onClick={() => { setEndpointDialogOpen(false); setRevealedSecret(null); }}>Done</Button>
                    </DialogFooter>
                  </div>
                ) : (
                  <form onSubmit={handleCreateEndpoint} className="space-y-4">
                    <div>
                      <label className="text-sm font-medium text-slate-700">URL</label>
                      <Input value={endpointUrl} onChange={(e) => setEndpointUrl(e.target.value)} required placeholder="https://example.com/webhooks/payments" className="mt-1" />
                    </div>
                    <div>
                      <label className="text-sm font-medium text-slate-700">Event types (comma-separated)</label>
                      <Input value={endpointTypes} onChange={(e) => setEndpointTypes(e.target.value)} className="mt-1" />
                    </div>
                    <DialogFooter>
                      <Button type="submit" disabled={savingEndpoint}>{savingEndpoint ? "Adding..." : "Add endpoint"}</Button>
                    </DialogFooter>
                  </form>
                )}
              </DialogContent>
            </Dialog>
          </div>
          {testResult && (
            <Card className="mb-3">
              <CardContent className="p-4 text-sm">
                <p className="font-semibold text-slate-900">Last test-send: {testResult.ok ? `OK (${testResult.status_code})` : "failed"}</p>
                {!testResult.ok && testResult.error && <p className="mt-1 text-xs text-rose-600">{testResult.error}</p>}
                <p className="mt-1 text-xs text-slate-400">{testResult.endpoint_url} · {formatDate(testResult.delivered_at)}</p>
              </CardContent>
            </Card>
          )}
          {revealedEndpointSecret && (
            <Card className="mb-3">
              <CardContent className="p-4">
                <p className="text-sm font-medium text-slate-700">New signing secret (copy now — shown once, old secret already invalid):</p>
                <code className="mt-1 block break-all rounded-md bg-slate-100 p-3 font-mono text-xs">{revealedEndpointSecret}</code>
                <Button size="sm" variant="outline" className="mt-2" onClick={() => setRevealedEndpointSecret(null)}>Dismiss</Button>
              </CardContent>
            </Card>
          )}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>URL</TableHead>
                <TableHead>Events</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Secret</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {endpoints.map((endpoint) => (
                <TableRow key={endpoint.id}>
                  <TableCell className="max-w-xs truncate font-mono text-xs">{endpoint.url}</TableCell>
                  <TableCell className="text-xs text-slate-500">{endpoint.event_types.join(", ")}</TableCell>
                  <TableCell><Badge variant="secondary">{endpoint.status}</Badge></TableCell>
                  <TableCell className="text-xs text-slate-500">v{endpoint.secret_version ?? 1}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1.5">
                      <Button size="sm" variant="outline" disabled={testingEndpointId === endpoint.id} onClick={() => handleTestEndpoint(endpoint)}>
                        <Send className="h-3.5 w-3.5 mr-1" /> {testingEndpointId === endpoint.id ? "Testing..." : "Test"}
                      </Button>
                      <Button size="sm" variant="outline" disabled={rotatingSecretId === endpoint.id} onClick={() => handleRotateEndpointSecret(endpoint)}>
                        <RefreshCw className="h-3.5 w-3.5 mr-1" /> {rotatingSecretId === endpoint.id ? "Rotating..." : "Rotate secret"}
                      </Button>
                      <Button size="sm" variant="outline" disabled={togglingId === endpoint.id} onClick={() => handleToggleEndpoint(endpoint)}>
                        {endpoint.status === "active" ? "Disable" : "Enable"}
                      </Button>
                      <Button size="sm" variant="outline" onClick={() => handleDeleteEndpoint(endpoint)}>
                        <Trash2 className="h-3.5 w-3.5" />
                      </Button>
                    </div>
                  </TableCell>
                </TableRow>
              ))}
              {!endpointsQuery.isLoading && endpoints.length === 0 && (
                <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No webhook endpoints yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="keys">
          <div className="mb-3 flex justify-end">
            <Dialog open={keyDialogOpen} onOpenChange={(open) => { setKeyDialogOpen(open); if (!open) setRevealedKey(null); }}>
              <DialogTrigger asChild>
                <Button size="sm"><KeyRound className="h-3.5 w-3.5 mr-1" /> New key</Button>
              </DialogTrigger>
              <DialogContent className="max-h-[85vh] overflow-y-auto">
                <DialogHeader>
                  <DialogTitle>Create API key</DialogTitle>
                </DialogHeader>
                {revealedKey ? (
                  <div className="space-y-3">
                    <p className="text-sm text-slate-600">Copy it now — the secret is never shown again, only its prefix.</p>
                    <code className="block break-all rounded-md bg-slate-100 p-3 font-mono text-xs">{revealedKey}</code>
                    <DialogFooter>
                      <Button onClick={() => { setKeyDialogOpen(false); setRevealedKey(null); }}>Done</Button>
                    </DialogFooter>
                  </div>
                ) : (
                  <form onSubmit={handleCreateKey} className="space-y-4">
                    <div>
                      <label className="text-sm font-medium text-slate-700">Environment</label>
                      <select
                        className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                        value={keyEnv}
                        onChange={(e) => setKeyEnv(e.target.value)}
                      >
                        <option value="live">live</option>
                        <option value="sandbox">sandbox</option>
                      </select>
                      <p className="mt-1 text-xs text-slate-400">Live keys need a verified organization.</p>
                    </div>
                    <div>
                      <label className="text-sm font-medium text-slate-700">Label <span className="font-normal text-slate-400">(optional, max 60 chars)</span></label>
                      <Input value={keyLabel} onChange={(e) => setKeyLabel(e.target.value)} maxLength={60} placeholder="production server" className="mt-1" />
                    </div>
                    <DialogFooter>
                      <Button type="submit" disabled={creatingKey}>{creatingKey ? "Creating..." : "Create key"}</Button>
                    </DialogFooter>
                  </form>
                )}
              </DialogContent>
            </Dialog>
          </div>
          {revealedKey && !keyDialogOpen && (
            <Card className="mb-3">
              <CardContent className="p-4">
                <p className="text-sm font-medium text-slate-700">Latest key (copy now — shown once):</p>
                <code className="mt-1 block break-all rounded-md bg-slate-100 p-3 font-mono text-xs">{revealedKey}</code>
              </CardContent>
            </Card>
          )}
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Prefix</TableHead>
                <TableHead>Label</TableHead>
                <TableHead>Env</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Expires</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {keys.map((key) => (
                <TableRow key={key.id}>
                  <TableCell className="font-mono text-xs">{key.prefix}…</TableCell>
                  <TableCell className="max-w-40 truncate text-xs text-slate-600">{key.label || "—"}</TableCell>
                  <TableCell><Badge variant="secondary">{key.environment}</Badge></TableCell>
                  <TableCell><Badge variant="secondary">{key.status}</Badge></TableCell>
                  <TableCell className="text-xs text-slate-500">{formatDate(key.expires_at)}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1.5">
                      <Button size="sm" variant="outline" disabled={actingKeyId === key.id} onClick={() => openLabelDialog(key)}>
                        Label
                      </Button>
                      {key.status !== "revoked" && (
                        <Button size="sm" variant="outline" disabled={actingKeyId === key.id} onClick={() => handleRotateKey(key)}>
                          <RotateCcw className="h-3.5 w-3.5 mr-1" /> Rotate
                        </Button>
                      )}
                      {key.status !== "revoked" && (
                        <Button size="sm" variant="outline" disabled={actingKeyId === key.id} onClick={() => handleRevokeKey(key)}>
                          <Ban className="h-3.5 w-3.5 mr-1" /> Revoke
                        </Button>
                      )}
                    </div>
                  </TableCell>
                </TableRow>
              ))}
              {!keysQuery.isLoading && keys.length === 0 && (
                <TableRow><TableCell colSpan={6} className="text-center text-slate-500">No API keys yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
          <Dialog open={labelKey !== null} onOpenChange={(open) => { if (!open) setLabelKey(null); }}>
            <DialogContent className="max-h-[85vh] overflow-y-auto">
              <DialogHeader>
                <DialogTitle>Key label — {labelKey?.prefix}…</DialogTitle>
              </DialogHeader>
              <form onSubmit={handleSaveLabel} className="space-y-4">
                <div>
                  <label className="text-sm font-medium text-slate-700">Label (max 60 chars, empty clears it)</label>
                  <Input value={labelValue} onChange={(e) => setLabelValue(e.target.value)} maxLength={60} className="mt-1" />
                </div>
                <DialogFooter>
                  <Button type="submit" disabled={savingLabel}>{savingLabel ? "Saving..." : "Save label"}</Button>
                </DialogFooter>
              </form>
            </DialogContent>
          </Dialog>
        </TabsContent>

        <TabsContent value="withdrawals">
          <div className="mb-3 flex items-center justify-between">
            <p className="text-xs text-slate-500">Request payouts from this app's balance. Finance/owner roles only.</p>
            <Dialog open={withdrawalDialogOpen} onOpenChange={setWithdrawalDialogOpen}>
              <DialogTrigger asChild>
                <Button size="sm"><Plus className="h-3.5 w-3.5 mr-1" /> New withdrawal</Button>
              </DialogTrigger>
              <DialogContent className="max-h-[85vh] overflow-y-auto">
                <DialogHeader>
                  <DialogTitle>Request withdrawal</DialogTitle>
                </DialogHeader>
                <form onSubmit={handleCreateWithdrawal} className="space-y-4">
                  <div>
                    <label className="text-sm font-medium text-slate-700">Amount (TZS)</label>
                    <Input value={withdrawalAmount} onChange={(e) => setWithdrawalAmount(e.target.value)} required inputMode="decimal" placeholder="50000" className="mt-1" />
                  </div>
                  <div>
                    <label className="text-sm font-medium text-slate-700">Destination</label>
                    <select
                      className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                      value={withdrawalDestType}
                      onChange={(e) => setWithdrawalDestType(e.target.value as "bank" | "mobile_money")}
                    >
                      <option value="bank">Bank transfer</option>
                      <option value="mobile_money">Mobile money</option>
                    </select>
                  </div>
                  {withdrawalDestType === "bank" ? (
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <Input placeholder="Bank name" value={withdrawalBankName} onChange={(e) => setWithdrawalBankName(e.target.value)} required />
                      <Input placeholder="Account name" value={withdrawalAccountName} onChange={(e) => setWithdrawalAccountName(e.target.value)} required />
                      <Input placeholder="Account number" value={withdrawalAccountNumber} onChange={(e) => setWithdrawalAccountNumber(e.target.value)} required className="sm:col-span-2" />
                    </div>
                  ) : (
                    <div className="grid grid-cols-1 gap-3 sm:grid-cols-2">
                      <Input placeholder="Provider (e.g. M-Pesa)" value={withdrawalProvider} onChange={(e) => setWithdrawalProvider(e.target.value)} required />
                      <Input placeholder="Phone number" value={withdrawalPhone} onChange={(e) => setWithdrawalPhone(e.target.value)} required />
                    </div>
                  )}
                  <div>
                    <label className="text-sm font-medium text-slate-700">Notes</label>
                    <Input value={withdrawalNotes} onChange={(e) => setWithdrawalNotes(e.target.value)} className="mt-1" placeholder="Optional" />
                  </div>
                  <DialogFooter>
                    <Button type="submit" disabled={creatingWithdrawal}>{creatingWithdrawal ? "Requesting..." : "Request withdrawal"}</Button>
                  </DialogFooter>
                </form>
              </DialogContent>
            </Dialog>
          </div>
          <div className="space-y-3">
            {withdrawalsQuery.isLoading && <p className="text-sm text-slate-500">Loading withdrawals…</p>}
            {!withdrawalsQuery.isLoading && withdrawals.length === 0 && (
              <Card><CardContent className="flex flex-col items-center gap-2 p-10 text-center">
                <Wallet className="h-8 w-8 text-slate-300" />
                <p className="text-sm font-medium text-slate-600">No withdrawals yet.</p>
              </CardContent></Card>
            )}
            {withdrawals.map((w) => {
              const busy = actingWithdrawalId === w.id;
              return (
                <Card key={w.id}>
                  <CardContent className="flex flex-col gap-3 p-4 sm:flex-row sm:items-start sm:justify-between">
                    <div className="min-w-0">
                      <div className="flex flex-wrap items-center gap-2">
                        <p className="text-sm font-semibold text-slate-900">{w.amount} {w.currency}</p>
                        <Badge variant="outline">{w.status}</Badge>
                      </div>
                      <p className="mt-1 text-xs text-slate-500">{withdrawalDestinationSummary(w)}</p>
                      <p className="mt-1 text-xs text-slate-400">Requested {formatDate(w.created_at)}</p>
                      {w.notes && <p className="mt-1 text-xs text-slate-500">Note: {w.notes}</p>}
                      {w.failure_reason && <p className="mt-1 text-xs text-rose-600">Payout attempt failed: {w.failure_reason}</p>}
                    </div>
                    {w.status === "requested" && (
                      <div className="flex shrink-0 flex-wrap items-center gap-1.5 self-end sm:self-start">
                        <Button
                          size="sm"
                          disabled={busy}
                          onClick={() => runWithdrawalAction(w.id, () => approveMerchantWithdrawal(appId, w.id), "Withdrawal approved — balance debited.")}
                        >
                          <Check className="h-3.5 w-3.5 mr-1" /> Approve
                        </Button>
                        <Button
                          size="sm"
                          variant="outline"
                          disabled={busy}
                          onClick={() => runWithdrawalAction(w.id, () => rejectMerchantWithdrawal(appId, w.id), "Withdrawal rejected.")}
                        >
                          <X className="h-3.5 w-3.5 mr-1" /> Reject
                        </Button>
                      </div>
                    )}
                  </CardContent>
                </Card>
              );
            })}
          </div>
        </TabsContent>

        <TabsContent value="deliveries">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Event</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Attempts</TableHead>
                <TableHead>Last response</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deliveries.map((delivery) => (
                <TableRow key={delivery.id}>
                  <TableCell className="text-xs text-slate-600">{delivery.event_type}<br /><span className="text-slate-400">{formatDate(delivery.created_at)}</span></TableCell>
                  <TableCell><Badge variant="secondary">{delivery.status}</Badge></TableCell>
                  <TableCell className="text-xs">{delivery.attempt_count}</TableCell>
                  <TableCell className="max-w-xs truncate text-xs text-slate-500">
                    {delivery.last_response_status ?? "—"}{delivery.last_error ? ` · ${delivery.last_error}` : ""}
                  </TableCell>
                  <TableCell className="text-right">
                    {delivery.status === "failed" && (
                      <Button size="sm" variant="outline" disabled={replayingId === delivery.id} onClick={() => handleReplay(delivery.id)}>
                        <Truck className="h-3.5 w-3.5 mr-1" /> {replayingId === delivery.id ? "Queuing..." : "Replay"}
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!deliveriesQuery.isLoading && deliveries.length === 0 && (
                <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No deliveries yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default MerchantAppDetail;
