import { FormEvent, useState } from "react";
import { useParams } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Globe, KeyRound, RotateCcw, Send, Trash2, Ban, Truck } from "lucide-react";
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
  createMerchantEndpoint,
  createMerchantKey,
  deleteMerchantEndpoint,
  listMerchantDeliveries,
  listMerchantEndpoints,
  listMerchantKeys,
  listMyApps,
  replayMerchantDelivery,
  revokeMerchantKey,
  rotateMerchantKey,
  testMerchantEndpoint,
  updateMerchantEndpoint,
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
  const appName = appsQuery.data?.find((a) => a.id === appId)?.name ?? "App";

  const endpointsKey = ["merchant", appId, "endpoints"];
  const keysKey = ["merchant", appId, "keys"];
  const deliveriesKey = ["merchant", appId, "deliveries"];

  const endpointsQuery = useQuery({ queryKey: endpointsKey, queryFn: () => listMerchantEndpoints(appId), staleTime: 15_000 });
  const keysQuery = useQuery({ queryKey: keysKey, queryFn: () => listMerchantKeys(appId), staleTime: 15_000 });
  const deliveriesQuery = useQuery({ queryKey: deliveriesKey, queryFn: () => listMerchantDeliveries(appId), staleTime: 15_000 });

  const endpoints = endpointsQuery.data ?? [];
  const keys = keysQuery.data ?? [];
  const deliveries = deliveriesQuery.data ?? [];

  const [endpointDialogOpen, setEndpointDialogOpen] = useState(false);
  const [endpointUrl, setEndpointUrl] = useState("");
  const [endpointTypes, setEndpointTypes] = useState("payment.updated, payment.refunded, payment.expired");
  const [savingEndpoint, setSavingEndpoint] = useState(false);
  const [revealedSecret, setRevealedSecret] = useState<string | null>(null);

  const [keyDialogOpen, setKeyDialogOpen] = useState(false);
  const [keyEnv, setKeyEnv] = useState("live");
  const [creatingKey, setCreatingKey] = useState(false);
  const [revealedKey, setRevealedKey] = useState<string | null>(null);
  const [actingKeyId, setActingKeyId] = useState<string | null>(null);

  const [testingEndpointId, setTestingEndpointId] = useState<string | null>(null);
  const [testResult, setTestResult] = useState<MerchantTestSendResult | null>(null);
  const [replayingId, setReplayingId] = useState<string | null>(null);
  const [togglingId, setTogglingId] = useState<string | null>(null);

  const reloadAll = () => {
    queryClient.invalidateQueries({ queryKey: endpointsKey });
    queryClient.invalidateQueries({ queryKey: keysKey });
    queryClient.invalidateQueries({ queryKey: deliveriesKey });
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

  const handleCreateKey = async (event: FormEvent) => {
    event.preventDefault();
    setCreatingKey(true);
    setRevealedKey(null);
    try {
      const result = await createMerchantKey(appId, { environment: keyEnv });
      setRevealedKey(result.api_key);
      toast.success("API key created — copy it now, it is never shown again.");
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

  const handleRevokeKey = async (key: MerchantAPIKey) => {
    if (!window.confirm(`Revoke key ${key.prefix}… immediately? In-flight traffic with it stops.`)) return;
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

  const handleReplay = async (deliveryId: string) => {
    setReplayingId(deliveryId);
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
      <div>
        <h2 className="text-2xl font-bold text-slate-900">{appName}</h2>
        <p className="mt-1 text-sm text-slate-500">Merchant self-service — everything here applies to this app only.</p>
      </div>

      <Tabs defaultValue="webhooks" className="mt-6">
        <TabsList>
          <TabsTrigger value="webhooks">Webhooks ({endpoints.length})</TabsTrigger>
          <TabsTrigger value="keys">API keys ({keys.length})</TabsTrigger>
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
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>URL</TableHead>
                <TableHead>Events</TableHead>
                <TableHead>Status</TableHead>
                <TableHead className="text-right">Actions</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {endpoints.map((endpoint) => (
                <TableRow key={endpoint.id}>
                  <TableCell className="max-w-xs truncate font-mono text-xs">{endpoint.url}</TableCell>
                  <TableCell className="text-xs text-slate-500">{endpoint.event_types.join(", ")}</TableCell>
                  <TableCell><Badge variant="secondary">{endpoint.status}</Badge></TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1.5">
                      <Button size="sm" variant="outline" disabled={testingEndpointId === endpoint.id} onClick={() => handleTestEndpoint(endpoint)}>
                        <Send className="h-3.5 w-3.5 mr-1" /> {testingEndpointId === endpoint.id ? "Testing..." : "Test"}
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
                <TableRow><TableCell colSpan={4} className="text-center text-slate-500">No webhook endpoints yet.</TableCell></TableRow>
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
                  <TableCell><Badge variant="secondary">{key.environment}</Badge></TableCell>
                  <TableCell><Badge variant="secondary">{key.status}</Badge></TableCell>
                  <TableCell className="text-xs text-slate-500">{formatDate(key.expires_at)}</TableCell>
                  <TableCell className="text-right">
                    <div className="flex justify-end gap-1.5">
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
                <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No API keys yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
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
