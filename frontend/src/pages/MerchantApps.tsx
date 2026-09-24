import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Boxes } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";
import { Skeleton } from "@/components/ui/skeleton";
import { listMyApps } from "@/lib/merchantApi";

const MerchantApps = () => {
  const appsQuery = useQuery({
    queryKey: ["merchant", "my-apps"],
    queryFn: () => listMyApps(),
    staleTime: 30_000,
  });
  const apps = appsQuery.data ?? [];

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">My apps</h2>
        <p className="mt-1 text-sm text-slate-500">
          Apps you are a member of. Everything here is scoped to one app — webhooks, API keys, and delivery logs.
        </p>
      </div>

      <div className="mt-6 grid gap-3 sm:grid-cols-2">
        {appsQuery.isLoading &&
          Array.from({ length: 2 }).map((_, i) => (
            <Card key={`skeleton-${i}`}><CardContent className="p-4"><Skeleton className="h-16 w-full" /></CardContent></Card>
          ))}

        {!appsQuery.isLoading && apps.length === 0 && (
          <Card className="sm:col-span-2">
            <CardContent className="flex flex-col items-center gap-2 p-10 text-center">
              <Boxes className="h-8 w-8 text-slate-300" />
              <p className="text-sm font-medium text-slate-600">No apps yet.</p>
              <p className="text-xs text-slate-400">Ask an administrator to add you to an app.</p>
            </CardContent>
          </Card>
        )}

        {apps.map((app) => (
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
  );
};

export default MerchantApps;
