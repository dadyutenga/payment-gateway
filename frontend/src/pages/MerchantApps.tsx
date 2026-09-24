import { Link, Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Boxes } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Badge } from "@/components/ui/badge";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { listMyApps } from "@/lib/merchantApi";
import { listMyOrgs } from "@/lib/orgApi";

const MerchantApps = () => {
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
        <Button size="sm" variant="outline" asChild>
          <Link to="/onboarding/create-org">New organization</Link>
        </Button>
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
