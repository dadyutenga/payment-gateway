import { Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { listMyOrgs } from "@/lib/orgApi";

// Top-level merchant shortcuts resolve the caller's org (one org per
// account) then land on the org-scoped page.
function useActiveOrgId() {
  return useQuery({
    queryKey: ["orgs", "mine"],
    queryFn: () => listMyOrgs(),
    staleTime: 30_000,
  });
}

function Redirect({ to }: { to: (orgId: string) => string }) {
  const orgsQuery = useActiveOrgId();
  if (orgsQuery.isLoading) {
    return <div className="flex min-h-[40vh] items-center justify-center text-sm text-slate-500">Loading…</div>;
  }
  const orgs = orgsQuery.data ?? [];
  const active = orgs.find((o) => o.status === "active") ?? orgs[0];
  if (!active) {
    return <Navigate to="/onboarding/create-org" replace />;
  }
  return <Navigate to={to(active.id)} replace />;
}

export const MerchantAnalyticsIndex = () => <Redirect to={(id) => `/org/${id}/analytics`} />;
export const MerchantAnalyticsMethodsIndex = () => <Redirect to={(id) => `/org/${id}/analytics/methods`} />;
export const MerchantAnalyticsPeakHoursIndex = () => <Redirect to={(id) => `/org/${id}/analytics/peak-hours`} />;
export const MerchantAnalyticsCustomersIndex = () => <Redirect to={(id) => `/org/${id}/analytics/customers`} />;
export const MerchantAnalyticsFailuresIndex = () => <Redirect to={(id) => `/org/${id}/analytics/failures`} />;
export const MerchantSettlementsIndex = () => <Redirect to={(id) => `/org/${id}/settlements`} />;
export const MerchantTeamIndex = () => <Redirect to={(id) => `/org/${id}/members`} />;
