import { Navigate, useLocation, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { getCustomerToken } from "@/lib/auth";
import { getOrg, listMyOrgs } from "@/lib/orgApi";
import MerchantSettingsPage from "@/pages/MerchantSettingsPage";

// OrgSettingsRouter resolves the legacy /org/:orgId/settings deep link to
// the matching track settings page: creators land on /creator/settings,
// merchants render the merchant settings page. Unauthenticated users go to
// login (the track guard inside each page handles the rest).
export const OrgSettingsRouter = () => {
  const { orgId = "" } = useParams();
  const location = useLocation();
  const authed = !!getCustomerToken();
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000, retry: false, enabled: authed });
  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000, retry: false, enabled: authed });

  if (!authed) {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  if (orgsQuery.isLoading || orgQuery.isLoading) {
    return <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-500">Loading.</div>;
  }
  const org = orgQuery.data ?? (orgsQuery.data ?? []).find((o) => o.id === orgId);
  if ((org?.account_kind ?? "merchant") === "creator") {
    return <Navigate to="/creator/settings" replace state={{ notice: { message: "Creator settings live here — this is your personal workspace." } }} />;
  }
  return <MerchantSettingsPage />;
};

// OrgVerifyRouter resolves the legacy /onboarding/kyc/:orgId deep link to
// the matching track verification flow. Never renders a generic form.
export const OrgVerifyRouter = () => {
  const { orgId = "" } = useParams();
  const location = useLocation();
  const authed = !!getCustomerToken();
  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000, retry: false, enabled: authed });

  if (!authed) {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  if (orgQuery.isLoading) {
    return <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-500">Loading.</div>;
  }
  const kind = (orgQuery.data?.account_kind ?? "merchant") as "merchant" | "creator";
  return <Navigate to={kind === "creator" ? `/creator/verify/${orgId}` : `/merchant/verify/${orgId}`} replace />;
};
