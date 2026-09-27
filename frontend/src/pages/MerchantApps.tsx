import { FormEvent, useState } from "react";
import { Link, Navigate } from "react-router-dom";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Boxes, Plus } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Input } from "@/components/ui/input";
import { Skeleton } from "@/components/ui/skeleton";
import {
  Dialog,
  DialogContent,
  DialogFooter,
  DialogHeader,
  DialogTitle,
  DialogTrigger,
} from "@/components/ui/dialog";
import { toast } from "@/components/ui/sonner";
import { createMerchantApp, listMyApps } from "@/lib/merchantApi";
import { listMyOrgs } from "@/lib/orgApi";

const MerchantApps = () => {
  const queryClient = useQueryClient();
  const appsQuery = useQuery({
    queryKey: ["merchant", "my-apps"],
    queryFn: () => listMyApps(),
    staleTime: 30_000,
  });
  const orgsQuery = useQuery({
    queryKey: ["orgs", "mine"],
    queryFn: () => listMyOrgs(),
    staleTime: 60_000,
  });
  const apps = appsQuery.data ?? [];
  const orgs = orgsQuery.data ?? [];
  const orgName = (orgId?: string) => orgs.find((o) => o.id === orgId)?.name;

  // App creation needs the develop permission (owner/developer) — the
  // backend enforces it; the picker only offers eligible orgs.
  const creatableOrgs = orgs.filter((o) => o.status === "active" && (o.role === "owner" || o.role === "developer"));
  const [appDialogOpen, setAppDialogOpen] = useState(false);
  const [newAppOrgId, setNewAppOrgId] = useState("");
  const [newAppName, setNewAppName] = useState("");
  const [newAppDescription, setNewAppDescription] = useState("");
  const [creatingApp, setCreatingApp] = useState(false);
  const [revealedAppKey, setRevealedAppKey] = useState<string | null>(null);

  const handleCreateApp = async (event: FormEvent) => {
    event.preventDefault();
    setCreatingApp(true);
    setRevealedAppKey(null);
    try {
      const result = await createMerchantApp({
        org_id: newAppOrgId,
        name: newAppName.trim(),
        description: newAppDescription.trim() || undefined,
      });
      setRevealedAppKey(result.api_key);
      toast.success("App created — copy the API key now, it is never shown again.");
      setNewAppName("");
      setNewAppDescription("");
      queryClient.invalidateQueries({ queryKey: ["merchant", "my-apps"] });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to create app.");
    } finally {
      setCreatingApp(false);
    }
  };

  // No org yet → onboarding owns this state (Block 1 checkpoint: org
  // creation is the entry point to merchant self-service).
  if (!orgsQuery.isLoading && orgs.length === 0) {
    return <Navigate to="/onboarding/create-org" replace />;
  }

  const grouped = new Map<string, { name: string; apps: typeof apps }>();
  for (const app of apps) {
    const key = app.org_id || "unknown";
    const entry = grouped.get(key) ?? { name: orgName(app.org_id) ?? "Organization", apps: [] };
    entry.apps.push(app);
    grouped.set(key, entry);
  }

  return (
    <div>
      <div className="flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
        <div>
          <h2 className="text-2xl font-bold text-slate-900">My apps</h2>
          <p className="mt-1 text-sm text-slate-500">
            Apps you can access, grouped by organization. Everything is scoped to one app.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <Dialog open={appDialogOpen} onOpenChange={(open) => { setAppDialogOpen(open); if (!open) setRevealedAppKey(null); }}>
            <DialogTrigger asChild>
              <Button size="sm" disabled={creatableOrgs.length === 0}><Plus className="h-4 w-4 mr-1" /> New app</Button>
            </DialogTrigger>
            <DialogContent className="max-h-[85vh] overflow-y-auto">
              <DialogHeader>
                <DialogTitle>Create app</DialogTitle>
              </DialogHeader>
              {revealedAppKey ? (
                <div className="space-y-3">
                  <p className="text-sm text-slate-600">Save this API key now — it will not be shown again.</p>
                  <code className="block break-all rounded-md bg-slate-100 p-3 font-mono text-xs">{revealedAppKey}</code>
                  <DialogFooter>
                    <Button onClick={() => { setAppDialogOpen(false); setRevealedAppKey(null); }}>Done</Button>
                  </DialogFooter>
                </div>
              ) : (
                <form onSubmit={handleCreateApp} className="space-y-4">
                  <div>
                    <label className="text-sm font-medium text-slate-700">Organization</label>
                    <select
                      className="mt-1 w-full rounded-md border border-slate-300 px-3 py-2 text-sm"
                      value={newAppOrgId}
                      onChange={(e) => setNewAppOrgId(e.target.value)}
                      required
                    >
                      <option value="">Select organization…</option>
                      {creatableOrgs.map((o) => (
                        <option key={o.id} value={o.id}>{o.name} · {o.kyc_status}</option>
                      ))}
                    </select>
                    <p className="mt-1 text-xs text-slate-400">Unverified orgs get a sandbox key; verified orgs get a live key.</p>
                  </div>
                  <div>
                    <label className="text-sm font-medium text-slate-700">Name</label>
                    <Input value={newAppName} onChange={(e) => setNewAppName(e.target.value)} required maxLength={100} className="mt-1" />
                  </div>
                  <div>
                    <label className="text-sm font-medium text-slate-700">Description <span className="font-normal text-slate-400">(optional)</span></label>
                    <Input value={newAppDescription} onChange={(e) => setNewAppDescription(e.target.value)} maxLength={500} className="mt-1" />
                  </div>
                  <DialogFooter>
                    <Button type="submit" disabled={creatingApp || !newAppOrgId}>{creatingApp ? "Creating..." : "Create app"}</Button>
                  </DialogFooter>
                </form>
              )}
            </DialogContent>
          </Dialog>
          <Button size="sm" variant="outline" asChild>
            <Link to="/onboarding/create-org">New organization</Link>
          </Button>
        </div>
      </div>

      {(appsQuery.isLoading || orgsQuery.isLoading) && (
        <div className="mt-6 grid gap-3 sm:grid-cols-2">
          {Array.from({ length: 2 }).map((_, i) => (
            <Card key={`skeleton-${i}`}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}
        </div>
      )}

      {!appsQuery.isLoading && !orgsQuery.isLoading && apps.length === 0 && (
        <Card className="mt-6">
          <CardContent className="flex flex-col items-center gap-2 p-10 text-center">
            <Boxes className="h-8 w-8 text-slate-300" />
            <p className="text-sm font-medium text-slate-600">No apps yet.</p>
            <p className="text-xs text-slate-400">Ask an organization owner to add you to an app.</p>
          </CardContent>
        </Card>
      )}

      {[...grouped.entries()].map(([orgId, group]) => (
        <div key={orgId} className="mt-6">
          <div className="mb-2 flex items-center gap-2">
            <h3 className="text-sm font-bold text-slate-800">{group.name}</h3>
            <Link to={`/org/${orgId}/members`} className="text-xs text-blue-600 hover:underline">Members</Link>
            <Link to={`/org/${orgId}/settings`} className="text-xs text-blue-600 hover:underline">Settings</Link>
            <Badge variant="outline">{group.apps.length} app{group.apps.length === 1 ? "" : "s"}</Badge>
          </div>
          <div className="grid gap-3 sm:grid-cols-2">
            {group.apps.map((app) => (
              <Card key={app.id}>
                <CardContent className="flex items-center justify-between gap-3 p-4">
                  <div className="min-w-0">
                    <p className="truncate text-sm font-semibold text-slate-900">{app.name}</p>
                    <p className="mt-0.5 text-xs text-slate-500">{app.status}</p>
                  </div>
                  <Button size="sm" variant="outline" asChild>
                    <Link to={`/merchant/apps/${app.id}`}>Manage</Link>
                  </Button>
                </CardContent>
              </Card>
            ))}
          </div>
        </div>
      ))}
    </div>
  );
};

export default MerchantApps;
