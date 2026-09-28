import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "@/components/ui/sonner";
import AdminRoute from "@/components/AdminRoute";
import MerchantRoute from "@/components/MerchantRoute";
import AdminLayout from "@/pages/AdminLayout";
import CustomerLayout from "@/pages/CustomerLayout";
import SignIn from "@/pages/SignIn";
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
import MerchantWithdrawals from "@/pages/MerchantWithdrawals";
import MerchantWebhooks from "@/pages/MerchantWebhooks";
import MerchantApiKeys from "@/pages/MerchantApiKeys";
import MerchantDeliveries from "@/pages/MerchantDeliveries";
import MerchantSettings from "@/pages/MerchantSettings";
import CreateOrg from "@/pages/CreateOrg";
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
          {/* Merchant (customer) space: /login + /register.
              /signin + /signup are kept as aliases. Operators use
              /admin/login (separate path, audience, session). */}
          <Route path="/login" element={<SignIn />} />
          <Route path="/signin" element={<SignIn />} />
          <Route path="/register" element={<SignUp />} />
          <Route path="/signup" element={<SignUp />} />
          <Route path="/admin/login" element={<SignIn admin />} />
          <Route
            path="/admin"
            element={
              <AdminRoute>
                <AdminLayout />
              </AdminRoute>
            }
          >
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
            <Route index element={<MerchantDashboard />} />
            <Route path="apps" element={<MerchantApps />} />
            <Route path="apps/:id" element={<MerchantAppDetail />} />
            <Route path="payments" element={<MerchantPayments />} />
            <Route path="withdrawals" element={<MerchantWithdrawals />} />
            <Route path="webhooks" element={<MerchantWebhooks />} />
            <Route path="api-keys" element={<MerchantApiKeys />} />
            <Route path="deliveries" element={<MerchantDeliveries />} />
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
            path="/org/:orgId"
            element={
              <MerchantRoute>
                <CustomerLayout />
              </MerchantRoute>
            }
          >
            <Route path="members" element={<OrgMembers />} />
            <Route path="settings" element={<OrgSettings />} />
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
          <Route path="/" element={<Welcome />} />
          <Route path="*" element={<Navigate to="/" replace />} />
        </Routes>
      </BrowserRouter>
    </>
  </QueryClientProvider>
);

export default App;
