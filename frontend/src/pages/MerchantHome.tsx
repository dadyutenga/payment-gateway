import { useQuery } from "@tanstack/react-query";
import { Skeleton } from "@/components/ui/skeleton";
import { listMyOrgs } from "@/lib/orgApi";
import CreatorOverview from "@/pages/CreatorOverview";
import MerchantDashboard from "@/pages/MerchantDashboard";

// Home switch: creators get the trimmed creator overview, merchants keep
// the full merchant dashboard. Same URL (/merchant) for both tracks.
const MerchantHome = () => {
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000 });
  const org = (orgsQuery.data ?? []).find((o) => o.status === "active") ?? orgsQuery.data?.[0];

  if (orgsQuery.isLoading) {
    return <Skeleton className="mt-4 h-64 w-full" />;
  }
  if ((org?.account_kind ?? "merchant") === "creator") {
    return <CreatorOverview />;
  }
  return <MerchantDashboard />;
};

export default MerchantHome;
