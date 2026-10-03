import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { Bell, Check, ChevronLeft, ChevronRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { listNotifications, markAllNotificationsRead, markNotificationRead, type DashboardSpace } from "@/lib/notificationsApi";

function relative(value: string) {
  const seconds = Math.max(0, Math.floor((Date.now() - new Date(value).getTime()) / 1000));
  if (seconds < 60) return "Just now";
  const minutes = Math.floor(seconds / 60);
  if (minutes < 60) return `${minutes}m ago`;
  const hours = Math.floor(minutes / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

const NotificationsPage = ({ space = "customer" }: { space?: DashboardSpace }) => {
  const queryClient = useQueryClient();
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [eventType, setEventType] = useState("");
  const [offset, setOffset] = useState(0);
  const limit = 25;
  const query = useQuery({ queryKey: ["notifications", space, "page", unreadOnly, eventType, offset], queryFn: () => listNotifications(space, { limit, offset, unread: unreadOnly, eventType }), staleTime: 15_000 });
  const refresh = () => queryClient.invalidateQueries({ queryKey: ["notifications", space] });
  const readOne = useMutation({ mutationFn: (id: string) => markNotificationRead(space, id), onSuccess: refresh });
  const readAll = useMutation({ mutationFn: () => markAllNotificationsRead(space), onSuccess: refresh });
  const page = query.data;
  const items = page?.items ?? [];

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div>
          <p className="text-sm font-medium text-primary">Your inbox</p>
          <h1 className="mt-1 text-2xl font-semibold text-foreground">Notifications</h1>
          <p className="mt-1 text-sm text-muted-foreground">Important account, payment, verification, and workspace updates in one place.</p>
        </div>
        <Button variant="outline" onClick={() => readAll.mutate()} disabled={!page?.unread || readAll.isPending}><Check className="mr-2 h-4 w-4" /> Mark all as read</Button>
      </div>
      <div className="flex flex-wrap items-center gap-3 rounded-xl border border-border bg-card p-3">
        <label className="flex items-center gap-2 text-sm text-foreground"><input type="checkbox" checked={unreadOnly} onChange={(event) => { setUnreadOnly(event.target.checked); setOffset(0); }} /> Unread only</label>
        <select value={eventType} onChange={(event) => { setEventType(event.target.value); setOffset(0); }} className="rounded-md border border-input bg-background px-3 py-2 text-sm text-foreground" aria-label="Filter by notification type">
          <option value="">All types</option><option value="payment.succeeded">Payments</option><option value="withdrawal.completed">Withdrawals</option><option value="kyc.verified">KYC</option><option value="security.payout_destination_changed">Security</option><option value="admin.broadcast">Announcements</option>
        </select>
        <span className="ml-auto text-xs text-muted-foreground">{page?.unread ?? 0} unread</span>
      </div>
      <section className="overflow-hidden rounded-xl border border-border bg-card">
        {query.isLoading && <p className="p-6 text-sm text-muted-foreground">Loading notifications…</p>}
        {query.isError && <p className="p-6 text-sm text-destructive">Unable to load notifications right now.</p>}
        {!query.isLoading && !query.isError && items.length === 0 && <div className="flex flex-col items-center gap-2 p-12 text-center"><Bell className="h-8 w-8 text-muted-foreground" /><p className="font-medium text-foreground">You’re all caught up.</p><p className="text-sm text-muted-foreground">New account activity will appear here.</p></div>}
        {items.length > 0 && <ul className="divide-y divide-border">
          {items.map((item) => <li key={item.id} className={`flex gap-3 p-4 ${item.read_at ? "" : "bg-muted/30"}`}>
            <span className={`mt-2 h-2 w-2 shrink-0 rounded-full ${item.read_at ? "bg-transparent" : "bg-primary"}`} aria-label={item.read_at ? "Read" : "Unread"} />
            <div className="min-w-0 flex-1"><div className="flex flex-wrap items-center justify-between gap-2"><p className="font-semibold text-foreground">{item.icon ? `${item.icon} ` : ""}{item.title}</p><time className="text-xs text-muted-foreground" dateTime={item.created_at}>{relative(item.created_at)}</time></div><p className="mt-1 text-sm text-muted-foreground">{item.body}</p>{item.link_url && <Link to={item.link_url} onClick={() => { if (!item.read_at) readOne.mutate(item.id); }} className="mt-2 inline-block text-sm font-medium text-primary hover:underline">Open update →</Link>}</div>
            {!item.read_at && <Button variant="ghost" size="sm" onClick={() => readOne.mutate(item.id)}>Mark read</Button>}
          </li>)}
        </ul>}
      </section>
      <div className="flex items-center justify-between"><Button variant="outline" size="sm" disabled={offset === 0} onClick={() => setOffset(Math.max(0, offset - limit))}><ChevronLeft className="mr-1 h-4 w-4" /> Newer</Button><span className="text-xs text-muted-foreground">{page?.total ? `${offset + 1}–${Math.min(offset + items.length, page.total)} of ${page.total}` : ""}</span><Button variant="outline" size="sm" disabled={!page || offset + items.length >= page.total} onClick={() => setOffset(offset + limit)}>Older <ChevronRight className="ml-1 h-4 w-4" /></Button></div>
    </div>
  );
};

export default NotificationsPage;
