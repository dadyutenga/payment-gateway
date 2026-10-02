import { useQuery } from "@tanstack/react-query";
import { Skeleton } from "@/components/ui/skeleton";
import { listMyOrgs } from "@/lib/orgApi";
import CreatorPayments from "@/pages/CreatorPayments";
import MerchantPayments from "@/pages/MerchantPayments";

const PaymentsRoute = () => {
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000 });
  const org = (orgsQuery.data ?? []).find((item) => item.status === "active") ?? orgsQuery.data?.[0];

  if (orgsQuery.isLoading) return <Skeleton className="mt-4 h-64 w-full" />;
  return (org?.account_kind ?? "merchant") === "creator" ? <CreatorPayments /> : <MerchantPayments />;
};

export default PaymentsRoute;