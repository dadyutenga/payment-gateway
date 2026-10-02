import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "@/components/ui/sonner";
import AdminRoute from "@/components/AdminRoute";
import TrackRoute, { AuthOnly } from "@/components/TrackRoute";
import AdminLayout from "@/pages/AdminLayout";
import AdminDashboard from "@/pages/AdminDashboard";
import AdminOrgDetail from "@/pages/AdminOrgDetail";
import AdminAnalyticsOverview from "@/pages/AdminAnalyticsOverview";
import AdminAnalyticsProviders from "@/pages/AdminAnalyticsProviders";
import AdminAnalyticsMerchants from "@/pages/AdminAnalyticsMerchants";
import AdminAnalyticsFailures from "@/pages/AdminAnalyticsFailures";
import AdminOps from "@/pages/AdminOps";
import AdminAudit from "@/pages/AdminAudit";
import MerchantLayout from "@/pages/MerchantLayout";
import CreatorLayout from "@/pages/CreatorLayout";
import SignIn from "@/pages/SignIn";
import AuthChooser from "@/pages/AuthChooser";
import MerchantLogin from "@/pages/MerchantLogin";
import MerchantRegister from "@/pages/MerchantRegister";
import CreatorLogin from "@/pages/CreatorLogin";
import CreatorRegister from "@/pages/CreatorRegister";
import AdminPayments from "@/pages/AdminPayments";
import AdminPaymentApps from "@/pages/AdminPaymentApps";
import AdminPaymentAppDetail from "@/pages/AdminPaymentAppDetail";
import AdminPaymentWithdrawals from "@/pages/AdminPaymentWithdrawals";
import AdminPaymentProviders from "@/pages/AdminPaymentProviders";
import AdminKYCReview from "@/pages/AdminKYCReview";
import MerchantApps from "@/pages/MerchantApps";
import MerchantAppDetail from "@/pages/MerchantAppDetail";
import MerchantDashboard from "@/pages/MerchantDashboard";
import MerchantPayments from "@/pages/MerchantPayments";
import MyPage from "@/pages/MyPage";
import CreatorOverview from "@/pages/CreatorOverview";
import CreatorPayments from "@/pages/CreatorPayments";
import CreatorPayouts from "@/pages/CreatorPayouts";
import CreatorSettingsPage from "@/pages/CreatorSettingsPage";
import MerchantSettingsPage from "@/pages/MerchantSettingsPage";
import MerchantWithdrawals from "@/pages/MerchantWithdrawals";
import MerchantWebhooks from "@/pages/MerchantWebhooks";
import MerchantApiKeys from "@/pages/MerchantApiKeys";
import MerchantDeliveries from "@/pages/MerchantDeliveries";
import MerchantSettings from "@/pages/MerchantSettings";
import MerchantAnalyticsOverview from "@/pages/MerchantAnalyticsOverview";
import MerchantAnalyticsMethods from "@/pages/MerchantAnalyticsMethods";
import MerchantAnalyticsPeakHours from "@/pages/MerchantAnalyticsPeakHours";
import MerchantAnalyticsCustomers from "@/pages/MerchantAnalyticsCustomers";
import MerchantAnalyticsFailures from "@/pages/MerchantAnalyticsFailures";
import MerchantSettlements from "@/pages/MerchantSettlements";
import {
  MerchantAnalyticsCustomersIndex,
  MerchantAnalyticsFailuresIndex,
  MerchantAnalyticsIndex,
  MerchantAnalyticsMethodsIndex,
  MerchantAnalyticsPeakHoursIndex,
  MerchantSettlementsIndex,
  MerchantTeamIndex,
} from "@/pages/MerchantAnalyticsIndex";
import CreateOrg from "@/pages/CreateOrg";
import CreatorOnboarding from "@/pages/CreatorOnboarding";
import CreatorSupport from "@/pages/CreatorSupport";
import OrgMembers from "@/pages/OrgMembers";
import { OrgSettingsRouter, OrgVerifyRouter } from "@/pages/OrgRouteGate";
import OnboardingKYC from "@/pages/OnboardingKYC";
import Welcome from "@/pages/Welcome";

const queryClient = new QueryClient();

const App = () => (
  <QueryClientProvider client={queryClient}>
    <>
      <Toaster richColors closeButton position="top-right" />
      <BrowserRouter>
        <Routes>
          {/* Separate entry points — kind is chosen by WHICH PAGE, not a toggle.
              /login + /register are chooser pages (old /signin + /signup aliases
              kept as chooser too so bookmarks keep working). Operators use
              /admin/login (separate path, audience, session). */}
          <Route path="/login" element={<AuthChooser mode="login" />} />
          <Route path="/signin" element={<AuthChooser mode="login" />} />
          <Route path="/register" element={<AuthChooser mode="register" />} />
          <Route path="/signup" element={<AuthChooser mode="register" />} />
          <Route path="/merchant/login" element={<MerchantLogin />} />
          <Route path="/merchant/register" element={<MerchantRegister />} />
          <Route path="/creator/login" element={<CreatorLogin />} />
          <Route path="/creator/register" element={<CreatorRegister />} />
          <Route path="/admin/login" element={<SignIn admin />} />
          <Route
            path="/admin"
            element={
              <AdminRoute>
                <AdminLayout />
              </AdminRoute>
            }
          >
            <Route index element={<AdminDashboard />} />
            <Route path="orgs/:orgId" element={<AdminOrgDetail />} />
            <Route path="analytics" element={<AdminAnalyticsOverview />} />
            <Route path="analytics/providers" element={<AdminAnalyticsProviders />} />
            <Route path="analytics/merchants" element={<AdminAnalyticsMerchants />} />
            <Route path="analytics/failures" element={<AdminAnalyticsFailures />} />
            <Route path="ops" element={<AdminOps />} />
            <Route path="audit" element={<AdminAudit />} />
            <Route path="payments" element={<AdminPayments />} />
            <Route path="payments/apps" element={<AdminPaymentApps />} />
            <Route path="payments/apps/:id" element={<AdminPaymentAppDetail />} />
              <Route path="payments/withdrawals" element={<AdminPaymentWithdrawals />} />
            <Route path="payments/providers" element={<AdminPaymentProviders />} />
            <Route path="kyc" element={<AdminKYCReview />} />
          </Route>
          {/* Merchant workspace: kind-guarded shell (emerald). Creators
              landing here are redirected to /creator with a notice. */}
          <Route
            path="/merchant"
            element={
              <TrackRoute kind="merchant">
                <MerchantLayout />
              </TrackRoute>
            }
          >
            <Route index element={<MerchantDashboard />} />
            <Route path="apps" element={<MerchantApps />} />
            <Route path="apps/:id" element={<MerchantAppDetail />} />
            <Route path="payments" element={<MerchantPayments />} />
            <Route path="withdrawals" element={<MerchantWithdrawals />} />
            <Route path="webhooks" element={<MerchantWebhooks />} />
            <Route path="api-keys" element={<MerchantApiKeys />} />
            <Route path="deliveries" element={<MerchantDeliveries />} />
            <Route path="analytics" element={<MerchantAnalyticsIndex />} />
            <Route path="analytics/methods" element={<MerchantAnalyticsMethodsIndex />} />
            <Route path="analytics/peak-hours" element={<MerchantAnalyticsPeakHoursIndex />} />
            <Route path="analytics/customers" element={<MerchantAnalyticsCustomersIndex />} />
            <Route path="analytics/failures" element={<MerchantAnalyticsFailuresIndex />} />
            <Route path="settlements" element={<MerchantSettlementsIndex />} />
            <Route path="team" element={<MerchantTeamIndex />} />
            <Route path="settings" element={<MerchantSettings />} />
          </Route>
          {/* Creator workspace: kind-guarded shell (fuchsia). Merchants
              landing here are redirected to /merchant with a notice. */}
          <Route
            path="/creator"
            element={
              <TrackRoute kind="creator">
                <CreatorLayout />
              </TrackRoute>
            }
          >
            <Route index element={<CreatorOverview />} />
            <Route path="page" element={<MyPage />} />
            <Route path="payments" element={<CreatorPayments />} />
            <Route path="payouts" element={<CreatorPayouts />} />
            <Route path="settings" element={<CreatorSettingsPage />} />
          </Route>
          {/* Legacy generic setup (kept one cycle): still offers the
              Business|Creator choice. New flows use the locked track
              setups below. */}
          <Route
            path="/onboarding/create-org"
            element={
              <AuthOnly>
                <div className="min-h-screen bg-slate-50 py-10">
                  <CreateOrg />
                </div>
              </AuthOnly>
            }
          />
          <Route
            path="/merchant/setup"
            element={
              <TrackRoute kind="merchant">
                <MerchantLayout />
              </TrackRoute>
            }
          >
            <Route index element={<CreateOrg lockedKind="merchant" />} />
          </Route>
          <Route
            path="/creator/setup"
            element={
              <TrackRoute kind="creator">
                <CreatorLayout />
              </TrackRoute>
            }
          >
            <Route index element={<CreateOrg lockedKind="creator" />} />
          </Route>
          {/* Track verification entries. */}
          <Route
            path="/merchant/verify/:orgId"
            element={
              <TrackRoute kind="merchant">
                <MerchantLayout />
              </TrackRoute>
            }
          >
            <Route index element={<OnboardingKYC lockedKind="merchant" />} />
          </Route>
          <Route
            path="/creator/verify/:orgId"
            element={
              <TrackRoute kind="creator">
                <CreatorLayout />
              </TrackRoute>
            }
          >
            <Route index element={<OnboardingKYC lockedKind="creator" />} />
          </Route>
          {/* Legacy org deep links (kept one cycle). Members/analytics/
              settlements are merchant-only; settings/verification resolve
              to the matching track. */}
          <Route
            path="/org/:orgId"
            element={
              <TrackRoute kind="merchant">
                <MerchantLayout />
              </TrackRoute>
            }
          >
            <Route path="members" element={<OrgMembers />} />
            <Route path="analytics" element={<MerchantAnalyticsOverview />} />
            <Route path="analytics/methods" element={<MerchantAnalyticsMethods />} />
            <Route path="analytics/peak-hours" element={<MerchantAnalyticsPeakHours />} />
            <Route path="analytics/customers" element={<MerchantAnalyticsCustomers />} />
            <Route path="analytics/failures" element={<MerchantAnalyticsFailures />} />
            <Route path="settlements" element={<MerchantSettlements />} />
          </Route>
          <Route
            path="/org/:orgId/settings"
            element={
              <AuthOnly>
                <MerchantLayout />
              </AuthOnly>
            }
          >
            <Route index element={<OrgSettingsRouter />} />
          </Route>
          <Route path="/onboarding/kyc/:orgId" element={<OrgVerifyRouter />} />
          <Route
            path="/onboarding/creator/:orgId"
            element={
              <TrackRoute kind="creator">
                <CreatorLayout />
              </TrackRoute>
            }
          >
            <Route index element={<CreatorOnboarding />} />
          </Route>
          <Route path="/" element={<Welcome />} />
          {/* Public creator support page (no auth — handle namespace). */}
          <Route path="/c/:handle" element={<CreatorSupport />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </>
  </QueryClientProvider>
);

export default App;
