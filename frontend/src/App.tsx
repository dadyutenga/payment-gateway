import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "@/components/ui/sonner";
import AdminRoute from "@/components/AdminRoute";
import MerchantRoute from "@/components/MerchantRoute";
import AdminLayout from "@/pages/AdminLayout";
import AdminDashboard from "@/pages/AdminDashboard";
import AdminOrgDetail from "@/pages/AdminOrgDetail";
import AdminAnalyticsOverview from "@/pages/AdminAnalyticsOverview";
import AdminAnalyticsProviders from "@/pages/AdminAnalyticsProviders";
import AdminAnalyticsMerchants from "@/pages/AdminAnalyticsMerchants";
import AdminAnalyticsFailures from "@/pages/AdminAnalyticsFailures";
import AdminOps from "@/pages/AdminOps";
import AdminAudit from "@/pages/AdminAudit";
import CustomerLayout from "@/pages/CustomerLayout";
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
import MerchantHome from "@/pages/MerchantHome";
import MyPage from "@/pages/MyPage";
import PaymentsRoute from "@/pages/PaymentsRoute";
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
import OrgSettings from "@/pages/OrgSettings";
import SignUp from "@/pages/SignUp";
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
          <Route
            path="/merchant"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
            }
          >
            <Route index element={<MerchantHome />} />
            <Route path="page" element={<MyPage />} />
            <Route path="apps" element={<MerchantApps />} />
            <Route path="apps/:id" element={<MerchantAppDetail />} />
            <Route path="payments" element={<PaymentsRoute />} />
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
          <Route
            path="/onboarding/create-org"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
            }
          >
            <Route index element={<CreateOrg />} />
          </Route>
          <Route
            path="/merchant/setup"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
            }
          >
            <Route index element={<CreateOrg lockedKind="merchant" />} />
          </Route>
          <Route
            path="/creator/setup"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
            }
          >
            <Route index element={<CreateOrg lockedKind="creator" />} />
          </Route>
          <Route
            path="/org/:orgId"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
            }
          >
            <Route path="members" element={<OrgMembers />} />
            <Route path="settings" element={<OrgSettings />} />
            <Route path="analytics" element={<MerchantAnalyticsOverview />} />
            <Route path="analytics/methods" element={<MerchantAnalyticsMethods />} />
            <Route path="analytics/peak-hours" element={<MerchantAnalyticsPeakHours />} />
            <Route path="analytics/customers" element={<MerchantAnalyticsCustomers />} />
            <Route path="analytics/failures" element={<MerchantAnalyticsFailures />} />
            <Route path="settlements" element={<MerchantSettlements />} />
          </Route>
          <Route
            path="/onboarding/kyc/:orgId"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
            }
          >
            <Route index element={<OnboardingKYC />} />
          </Route>
          <Route
            path="/onboarding/creator/:orgId"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
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
