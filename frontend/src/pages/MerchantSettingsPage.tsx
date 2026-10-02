import { useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { getOrg } from "@/lib/orgApi";
import {
  BrandingTab,
  DangerTab,
  GeneralTab,
  LimitsTab,
  NotificationsTab,
  PayoutsTab,
  SecurityTab,
  VerificationTab,
} from "@/pages/OrgSettings";

// Merchant workspace settings: business profile, business verification,
// limits & fees, security, notifications, branding, recent payout
// destinations, danger zone. No creator tabs, no team management here
// (Team lives under Members).
const MerchantSettingsPage = () => {
  const { orgId = "" } = useParams();
  const orgQuery = useQuery({ queryKey: ["orgs", orgId], queryFn: () => getOrg(orgId), staleTime: 30_000 });
  const org = orgQuery.data;
  const isOwner = org?.role === "owner";

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Settings — {org?.name ?? "…"}</h2>
        <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
          Organization settings.
          {org && <Badge variant="secondary">{org.role}</Badge>}
        </p>
      </div>

      {orgQuery.isLoading ? (
        <Skeleton className="mt-4 h-40 w-full" />
      ) : org ? (
        <Tabs defaultValue="general" className="mt-4">
          <TabsList className="flex-wrap">
            <TabsTrigger value="general">General</TabsTrigger>
            <TabsTrigger value="verification">Verification</TabsTrigger>
            <TabsTrigger value="limits">Limits &amp; Fees</TabsTrigger>
            <TabsTrigger value="security">Security</TabsTrigger>
            <TabsTrigger value="notifications">Notifications</TabsTrigger>
            <TabsTrigger value="branding">Branding</TabsTrigger>
            <TabsTrigger value="payouts">Payouts</TabsTrigger>
            <TabsTrigger value="danger">Danger zone</TabsTrigger>
          </TabsList>

          <TabsContent value="general"><GeneralTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="verification"><VerificationTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="limits"><LimitsTab org={org} track="merchant" /></TabsContent>
          <TabsContent value="security"><SecurityTab /></TabsContent>
          <TabsContent value="notifications"><NotificationsTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="branding"><BrandingTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="payouts"><PayoutsTab org={org} isOwner={!!isOwner} /></TabsContent>
          <TabsContent value="danger"><DangerTab org={org} isOwner={!!isOwner} homePath="/merchant" /></TabsContent>
        </Tabs>
      ) : (
        <p className="mt-4 text-sm text-slate-500">Organization not found.</p>
      )}
    </div>
  );
};

export default MerchantSettingsPage;
