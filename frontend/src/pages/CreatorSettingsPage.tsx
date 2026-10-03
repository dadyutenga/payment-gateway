import { Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { listMyOrgs } from "@/lib/orgApi";
import {
  BrandingTab,
  CreatorPayoutDestinationCard,
  DangerTab,
  GeneralTab,
  LimitsTab,
  NotificationsTab,
  SecurityTab,
  SupportPageTab,
  SurveyTab,
  VerificationTab,
} from "@/pages/OrgSettings";

// Creator workspace settings: personal profile, survey, support page,
// individual verification, limits, security, notifications, branding,
// payout destination, danger zone. No team, no business fields.
const CreatorSettingsPage = () => {
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 30_000 });
  if (orgsQuery.isLoading) {
    return <Skeleton className="mt-4 h-40 w-full" />;
  }
  const org = (orgsQuery.data ?? []).find((o) => o.status === "active") ?? orgsQuery.data?.[0];
  if (!org) {
    return <Navigate to="/creator/setup" replace />;
  }
  const isOwner = org.role === "owner";

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Settings — {org.display_name || org.name}</h2>
        <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
          Personal account settings.
          <Badge variant="secondary">{org.role}</Badge>
          <Badge variant="outline">individual</Badge>
        </p>
      </div>

      <Tabs defaultValue="general" className="mt-4">
        <TabsList className="flex-wrap">
          <TabsTrigger value="general">General</TabsTrigger>
          <TabsTrigger value="survey">Survey</TabsTrigger>
          <TabsTrigger value="support">Support page</TabsTrigger>
          <TabsTrigger value="verification">Verification</TabsTrigger>
          <TabsTrigger value="limits">Limits</TabsTrigger>
          <TabsTrigger value="security">Security</TabsTrigger>
          <TabsTrigger value="notifications">Notifications</TabsTrigger>
          <TabsTrigger value="branding">Branding</TabsTrigger>
          <TabsTrigger value="payouts">Payout destination</TabsTrigger>
          <TabsTrigger value="danger">Danger zone</TabsTrigger>
        </TabsList>

        <TabsContent value="general"><GeneralTab org={org} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="survey"><SurveyTab org={org} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="support"><SupportPageTab org={org} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="verification"><VerificationTab org={org} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="limits"><LimitsTab org={org} track="creator" /></TabsContent>
        <TabsContent value="security"><SecurityTab /></TabsContent>
        <TabsContent value="notifications"><NotificationsTab org={org} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="branding"><BrandingTab org={org} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="payouts"><CreatorPayoutDestinationCard org={org} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="danger"><DangerTab org={org} isOwner={!!isOwner} homePath="/creator" /></TabsContent>
      </Tabs>
    </div>
  );
};

export default CreatorSettingsPage;
