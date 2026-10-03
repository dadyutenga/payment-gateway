import { Navigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { Badge } from "@/components/ui/badge";
import { Skeleton } from "@/components/ui/skeleton";
import { Tabs, TabsContent, TabsList, TabsTrigger } from "@/components/ui/tabs";
import { getIndividualAccount } from "@/lib/orgApi";
import {
  BrandingTab,
  IndividualPayoutDestinationCard,
  DangerTab,
  GeneralTab,
  LimitsTab,
  NotificationsTab,
  SecurityTab,
  SupportPageTab,
  SurveyTab,
  VerificationTab,
} from "@/pages/OrgSettings";

// Individual workspace settings: personal profile, survey, support page,
// individual verification, limits, security, notifications, branding,
// payout destination, danger zone. No team, no business fields.
const IndividualSettingsPage = () => {
  const accountQuery = useQuery({ queryKey: ["individual", "account"], queryFn: () => getIndividualAccount(), staleTime: 30_000 });
  if (accountQuery.isLoading) {
    return <Skeleton className="mt-4 h-40 w-full" />;
  }
  const account = accountQuery.data;
  if (!account) {
    return <Navigate to="/individual/setup" replace />;
  }
  const isOwner = account.role === "owner";

  return (
    <div>
      <div>
        <h2 className="text-2xl font-bold text-slate-900">Settings — {account.display_name || account.name}</h2>
        <p className="mt-1 flex flex-wrap items-center gap-2 text-sm text-slate-500">
          Personal account settings.
          <Badge variant="secondary">{account.role}</Badge>
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

        <TabsContent value="general"><GeneralTab org={account} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="survey"><SurveyTab org={account} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="support"><SupportPageTab org={account} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="verification"><VerificationTab org={account} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="limits"><LimitsTab org={account} track="individual" /></TabsContent>
        <TabsContent value="security"><SecurityTab /></TabsContent>
        <TabsContent value="notifications"><NotificationsTab org={account} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="branding"><BrandingTab org={account} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="payouts"><IndividualPayoutDestinationCard org={account} isOwner={!!isOwner} /></TabsContent>
        <TabsContent value="danger"><DangerTab org={account} isOwner={!!isOwner} homePath="/individual" /></TabsContent>
      </Tabs>
    </div>
  );
};

export default IndividualSettingsPage;
