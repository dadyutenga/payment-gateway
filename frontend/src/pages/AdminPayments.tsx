import { FormEvent, useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { Table, TableBody, TableCell, TableHead, TableHeader, TableRow } from "@/components/ui/table";
import { Badge } from "@/components/ui/badge";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent, CardHeader, CardTitle } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";
import {
  getPaymentMetrics,
  listLedgerEntries,
  listPaymentApps,
  listPaymentEvents,
  listPaymentWebhookEndpoints,
  listWebhookDeliveries,
  processDeliveries,
  reconcilePayments,
  refundOrder,
  replayFailedDelivery,
  replayPaymentEvent,
  searchPaymentOrders,
} from "@/lib/adminApi";

const LEDGER_ENTRY_STYLES: Record<string, string> = {
  payment_credit: "bg-emerald-50 text-emerald-700 ring-emerald-600/15",
  platform_fee_debit: "bg-amber-50 text-amber-700 ring-amber-600/15",
  withdrawal_debit: "bg-rose-50 text-rose-700 ring-rose-600/15",
  withdrawal_reversal_credit: "bg-sky-50 text-sky-700 ring-sky-600/15",
  adjustment_credit: "bg-emerald-50 text-emerald-700 ring-emerald-600/15",
  adjustment_debit: "bg-rose-50 text-rose-700 ring-rose-600/15",
};

function formatDate(value?: string) {
  return value ? new Date(value).toLocaleString() : "—";
}

const ORDER_STATUSES = ["", "pending", "processing", "paid", "failed", "cancelled", "expired", "reversed"];

const PAYMENTS_QUERY_KEYS = [
  ["admin", "payments-apps"],
  ["admin", "payments-orders"],
  ["admin", "payments-events"],
  ["admin", "payments-deliveries"],
  ["admin", "payments-metrics"],
  ["admin", "payments-webhook-endpoints"],
] as const;

const AdminPayments = () => {
  const queryClient = useQueryClient();

  const [orderQuery, setOrderQuery] = useState("");
  const [orderStatus, setOrderStatus] = useState("");
  const [orderPhone, setOrderPhone] = useState("");
  const [orderApp, setOrderApp] = useState("");
  const [appliedOrderFilter, setAppliedOrderFilter] = useState<Record<string, string | undefined>>({});
  const [refundingOrderId, setRefundingOrderId] = useState<string | null>(null);

  const appsQuery = useQuery({
    queryKey: PAYMENTS_QUERY_KEYS[0],
    queryFn: () => listPaymentApps().then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const ordersQuery = useQuery({
    queryKey: [...PAYMENTS_QUERY_KEYS[1], appliedOrderFilter],
    queryFn: () => searchPaymentOrders(appliedOrderFilter).then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const eventsQuery = useQuery({
    queryKey: PAYMENTS_QUERY_KEYS[2],
    queryFn: () => listPaymentEvents().then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const deliveriesQuery = useQuery({
    queryKey: PAYMENTS_QUERY_KEYS[3],
    queryFn: () => listWebhookDeliveries().then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 30_000,
  });
  const metricsQuery = useQuery({
    queryKey: PAYMENTS_QUERY_KEYS[4],
    queryFn: () => getPaymentMetrics(),
    staleTime: 30_000,
  });
  const endpointsQuery = useQuery({
    queryKey: PAYMENTS_QUERY_KEYS[5],
    queryFn: () => listPaymentWebhookEndpoints().then((data) => (Array.isArray(data) ? data : [])),
    staleTime: 30_000,
  });

  const [ledgerAppFilter, setLedgerAppFilter] = useState("");
  const ledgerQuery = useQuery({
    queryKey: ["admin", "payments-ledger", ledgerAppFilter],
    queryFn: () => listLedgerEntries(ledgerAppFilter || undefined).then((r) => (Array.isArray(r.items) ? r.items : [])),
    staleTime: 15_000,
  });
  const ledgerEntries = ledgerQuery.data ?? [];

  const apps = appsQuery.data ?? [];
  const orders = ordersQuery.data ?? [];
  const events = eventsQuery.data ?? [];
  const deliveries = deliveriesQuery.data ?? [];
  const metrics = metricsQuery.data ?? null;
  const endpoints = endpointsQuery.data ?? [];
  const loading = appsQuery.isLoading || ordersQuery.isLoading || eventsQuery.isLoading || deliveriesQuery.isLoading || metricsQuery.isLoading;

  useEffect(() => {
    const failures = [appsQuery.error, ordersQuery.error, eventsQuery.error, deliveriesQuery.error, metricsQuery.error].filter(Boolean);
    if (failures.length > 0) {
      console.error("Payments data load failures:", failures);
      toast.error(
        failures.length === 5
          ? "Unable to load payments data."
          : `${failures.length} of 5 payments sections failed to load.`,
      );
    }
  }, [appsQuery.error, ordersQuery.error, eventsQuery.error, deliveriesQuery.error, metricsQuery.error]);

  const loadAll = () => {
    PAYMENTS_QUERY_KEYS.forEach((queryKey) => queryClient.invalidateQueries({ queryKey }));
  };

  const handleApplyOrderFilter = (event: FormEvent) => {
    event.preventDefault();
    setAppliedOrderFilter({
      q: orderQuery.trim() || undefined,
      status: orderStatus || undefined,
      phone: orderPhone.trim() || undefined,
      app_id: orderApp || undefined,
    });
  };

  const handleReplayEvent = async (eventID: string) => {
    try {
      await replayPaymentEvent(eventID);
      toast.success("Event replay queued.");
      loadAll();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to replay event.");
    }
  };

  const handleReplayDelivery = async (deliveryID: string) => {
    try {
      await replayFailedDelivery(deliveryID);
      toast.success("Delivery replay queued.");
      loadAll();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to replay delivery.");
    }
  };

  const handleRefundOrder = async (orderId: string) => {
    if (!confirm("Refund this order? This reverses the credit and platform fee on the app's ledger and cannot be undone.")) return;
    setRefundingOrderId(orderId);
    try {
      await refundOrder(orderId);
      toast.success("Order refunded — ledger reversed.");
      loadAll();
      queryClient.invalidateQueries({ queryKey: ["admin", "payments-ledger"] });
      queryClient.invalidateQueries({ queryKey: ["admin", "payment-app-balance"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to refund order.");
    } finally {
      setRefundingOrderId(null);
    }
  };

  const handleReconcile = async () => {
    try {
      const result = await reconcilePayments();
      toast.success(`Reconciled: checked ${result.checked}, updated ${result.updated}.`);
      loadAll();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to reconcile payments.");
    }
  };

  const handleProcessDeliveries = async () => {
    try {
      const result = await processDeliveries();
      toast.success(`Processed: ${result.delivered} delivered, ${result.retrying} retrying, ${result.failed} failed.`);
      loadAll();
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to process deliveries.");
    }
  };

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">Payments</h2>
          <p className="mt-1 text-sm text-slate-500">Orders, provider events, and webhook deliveries. Manage apps under Payments → Apps.</p>
        </div>
        <Button variant="outline" onClick={handleReconcile}>Reconcile now</Button>
      </div>

      <Tabs defaultValue="orders" className="mt-6">
        <div className="overflow-x-auto">
          <TabsList className="w-max">
            <TabsTrigger value="orders">Orders</TabsTrigger>
            <TabsTrigger value="ledger">Ledger</TabsTrigger>
            <TabsTrigger value="webhooks">Webhook endpoints</TabsTrigger>
            <TabsTrigger value="events">Events</TabsTrigger>
            <TabsTrigger value="deliveries">Deliveries</TabsTrigger>
            <TabsTrigger value="metrics">Metrics</TabsTrigger>
          </TabsList>
        </div>

        <TabsContent value="orders">
          <form onSubmit={handleApplyOrderFilter} className="flex flex-wrap items-center gap-2 mb-3">
            <Input placeholder="Search (reference, order id)" value={orderQuery} onChange={(e) => setOrderQuery(e.target.value)} className="h-9 w-56" />
            <select
              className="h-9 rounded-md border border-slate-300 px-2 text-sm"
              value={orderStatus}
              onChange={(e) => setOrderStatus(e.target.value)}
            >
              {ORDER_STATUSES.map((s) => (
                <option key={s} value={s}>{s === "" ? "Any status" : s}</option>
              ))}
            </select>
            <Input placeholder="Phone" value={orderPhone} onChange={(e) => setOrderPhone(e.target.value)} className="h-9 w-40" />
            <select
              className="h-9 rounded-md border border-slate-300 px-2 text-sm"
              value={orderApp}
              onChange={(e) => setOrderApp(e.target.value)}
            >
              <option value="">Any app</option>
              {apps.map((app) => (
                <option key={app.id} value={app.id}>{app.name}</option>
              ))}
            </select>
            <Button type="submit" size="sm" variant="outline">Filter</Button>
          </form>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>App</TableHead>
                <TableHead>Provider</TableHead>
                <TableHead>Amount</TableHead>
                <TableHead>Buyer</TableHead>
                <TableHead>Reference</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {orders.map((order) => (
                <TableRow key={order.id}>
                  <TableCell className="text-xs">
                    {order.app_id ? apps.find((a) => a.id === order.app_id)?.name ?? order.app_id : "—"}
                  </TableCell>
                  <TableCell>{order.provider}</TableCell>
                  <TableCell>{order.amount} {order.currency}</TableCell>
                  <TableCell>{order.buyer_name || order.buyer_phone || "—"}</TableCell>
                  <TableCell className="font-mono text-xs">{order.external_reference || order.provider_order_id || "—"}</TableCell>
                  <TableCell><Badge variant="secondary">{order.status}</Badge></TableCell>
                  <TableCell>{formatDate(order.created_at)}</TableCell>
                  <TableCell className="text-right">
                    {order.status === "paid" && (
                      <Button
                        size="sm"
                        variant="outline"
                        disabled={refundingOrderId === order.id}
                        onClick={() => handleRefundOrder(order.id)}
                      >
                        {refundingOrderId === order.id ? "Refunding..." : "Refund"}
                      </Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!loading && orders.length === 0 && (
                <TableRow><TableCell colSpan={8} className="text-center text-slate-500">No payment orders yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="ledger">
          <div className="mb-3 flex items-center gap-2">
            <select
              className="h-9 rounded-md border border-slate-300 px-2 text-sm"
              value={ledgerAppFilter}
              onChange={(e) => setLedgerAppFilter(e.target.value)}
            >
              <option value="">All apps</option>
              {apps.map((app) => (
                <option key={app.id} value={app.id}>{app.name}</option>
              ))}
            </select>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>App</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Amount</TableHead>
                <TableHead>Description</TableHead>
                <TableHead>Date</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {ledgerEntries.map((entry) => (
                <TableRow key={entry.id}>
                  <TableCell className="text-xs">{apps.find((a) => a.id === entry.app_id)?.name ?? entry.app_id}</TableCell>
                  <TableCell>
                    <Badge className={`ring-1 ring-inset ${LEDGER_ENTRY_STYLES[entry.entry_type] ?? ""}`} variant="outline">
                      {entry.entry_type.replace(/_/g, " ")}
                    </Badge>
                  </TableCell>
                  <TableCell className={entry.direction === "credit" ? "text-emerald-700" : "text-rose-700"}>
                    {entry.direction === "credit" ? "+" : "-"}{entry.amount} {entry.currency}
                  </TableCell>
                  <TableCell className="max-w-xs truncate text-slate-500">{entry.description}</TableCell>
                  <TableCell>{formatDate(entry.created_at)}</TableCell>
                </TableRow>
              ))}
              {!ledgerQuery.isLoading && ledgerEntries.length === 0 && (
                <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No ledger entries yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="webhooks">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>App</TableHead>
                <TableHead>URL</TableHead>
                <TableHead>Event types</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Created</TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {endpoints.map((endpoint) => (
                <TableRow key={endpoint.id}>
                  <TableCell className="font-mono text-xs">{apps.find((a) => a.id === endpoint.app_id)?.name ?? endpoint.app_id}</TableCell>
                  <TableCell className="max-w-xs truncate">{endpoint.url}</TableCell>
                  <TableCell>{endpoint.event_types.join(", ")}</TableCell>
                  <TableCell><Badge variant="secondary">{endpoint.status}</Badge></TableCell>
                  <TableCell>{formatDate(endpoint.created_at)}</TableCell>
                </TableRow>
              ))}
              {!endpointsQuery.isLoading && endpoints.length === 0 && (
                <TableRow><TableCell colSpan={5} className="text-center text-slate-500">No webhook endpoints yet. Add one from Payments → Apps.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="events">
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Provider</TableHead>
                <TableHead>Type</TableHead>
                <TableHead>Signature</TableHead>
                <TableHead>Duplicates</TableHead>
                <TableHead>Error</TableHead>
                <TableHead>Received</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {events.map((event) => (
                <TableRow key={event.id}>
                  <TableCell>{event.provider}</TableCell>
                  <TableCell>{event.event_type}</TableCell>
                  <TableCell>
                    <Badge variant={event.signature_valid ? "secondary" : "destructive"}>
                      {event.signature_valid ? "valid" : "invalid"}
                    </Badge>
                  </TableCell>
                  <TableCell>{event.duplicate_count}</TableCell>
                  <TableCell className="max-w-xs truncate text-red-600">{event.error || "—"}</TableCell>
                  <TableCell>{formatDate(event.received_at)}</TableCell>
                  <TableCell>
                    <Button size="sm" variant="outline" onClick={() => handleReplayEvent(event.id)}>Replay</Button>
                  </TableCell>
                </TableRow>
              ))}
              {!loading && events.length === 0 && (
                <TableRow><TableCell colSpan={7} className="text-center text-slate-500">No payment events yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="deliveries">
          <div className="flex justify-end mb-3">
            <Button size="sm" variant="outline" onClick={handleProcessDeliveries}>Process due deliveries</Button>
          </div>
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead>Endpoint</TableHead>
                <TableHead>Event</TableHead>
                <TableHead>Status</TableHead>
                <TableHead>Attempts</TableHead>
                <TableHead>Next attempt</TableHead>
                <TableHead>Last error</TableHead>
                <TableHead></TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {deliveries.map((delivery) => (
                <TableRow key={delivery.id}>
                  <TableCell className="max-w-xs truncate">{delivery.endpoint_url}</TableCell>
                  <TableCell>{delivery.event_type} <span className="text-slate-400">({delivery.provider})</span></TableCell>
                  <TableCell><Badge variant="secondary">{delivery.status}</Badge></TableCell>
                  <TableCell>{delivery.attempt_count}</TableCell>
                  <TableCell>{formatDate(delivery.next_attempt_at)}</TableCell>
                  <TableCell className="max-w-xs truncate text-red-600">{delivery.last_error || "—"}</TableCell>
                  <TableCell>
                    {delivery.status === "failed" && (
                      <Button size="sm" variant="outline" onClick={() => handleReplayDelivery(delivery.id)}>Replay</Button>
                    )}
                  </TableCell>
                </TableRow>
              ))}
              {!loading && deliveries.length === 0 && (
                <TableRow><TableCell colSpan={7} className="text-center text-slate-500">No webhook deliveries yet.</TableCell></TableRow>
              )}
            </TableBody>
          </Table>
        </TabsContent>

        <TabsContent value="metrics">
          {metrics && (
            <div className="space-y-6">
              <div className="grid grid-cols-1 sm:grid-cols-2 lg:grid-cols-4 gap-4">
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500">Total events</CardTitle></CardHeader>
                  <CardContent><div className="text-2xl font-bold">{metrics.total_events}</div></CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500">Invalid signatures</CardTitle></CardHeader>
                  <CardContent><div className="text-2xl font-bold">{metrics.invalid_signature_events}</div></CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500">Duplicate callbacks</CardTitle></CardHeader>
                  <CardContent><div className="text-2xl font-bold">{metrics.duplicate_callbacks}</div></CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500">Reconciliation candidates</CardTitle></CardHeader>
                  <CardContent><div className="text-2xl font-bold">{metrics.reconciliation_candidates}</div></CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500">Unprocessed events</CardTitle></CardHeader>
                  <CardContent><div className="text-2xl font-bold">{metrics.unprocessed_events}</div></CardContent>
                </Card>
                <Card>
                  <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500">Event errors</CardTitle></CardHeader>
                  <CardContent><div className="text-2xl font-bold">{metrics.event_errors}</div></CardContent>
                </Card>
              </div>

              <div>
                <h3 className="text-sm font-semibold text-slate-700">Orders by status</h3>
                <div className="mt-2 grid grid-cols-2 sm:grid-cols-4 gap-4">
                  {Object.entries(metrics.orders_by_status ?? {}).map(([status, count]) => (
                    <Card key={status}>
                      <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500 capitalize">{status}</CardTitle></CardHeader>
                      <CardContent><div className="text-xl font-bold">{count}</div></CardContent>
                    </Card>
                  ))}
                </div>
              </div>

              <div>
                <h3 className="text-sm font-semibold text-slate-700">Deliveries by status</h3>
                <div className="mt-2 grid grid-cols-2 sm:grid-cols-4 gap-4">
                  {Object.entries(metrics.deliveries_by_status ?? {}).map(([status, count]) => (
                    <Card key={status}>
                      <CardHeader className="pb-2"><CardTitle className="text-sm font-medium text-slate-500 capitalize">{status}</CardTitle></CardHeader>
                      <CardContent><div className="text-xl font-bold">{count}</div></CardContent>
                    </Card>
                  ))}
                </div>
              </div>

              <p className="text-xs text-slate-400">Generated at {formatDate(metrics.generated_at)}</p>
            </div>
          )}
        </TabsContent>
      </Tabs>
    </div>
  );
};

export default AdminPayments;
