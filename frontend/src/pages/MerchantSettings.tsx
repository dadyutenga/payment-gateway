import { Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { listMyOrgs } from "@/lib/orgApi";

// Top-level merchant Settings shortcut: one org per account, so this
// resolves the caller's organization and lands on its settings page.
const MerchantSettings = () => {
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000 });
  if (orgsQuery.isLoading) {
    return <div className="flex min-h-[40vh] items-center justify-center text-sm text-slate-500">Loading…</div>;
  }
  const orgs = orgsQuery.data ?? [];
  const active = orgs.find((o) => o.status === "active") ?? orgs[0];
  if (!active) {
    return <Navigate to="/onboarding/create-org" replace />;
  }
  return <Navigate to={`/org/${active.id}/settings`} replace />;
};

export default MerchantSettings;
