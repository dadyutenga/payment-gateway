import { BrowserRouter, Navigate, Route, Routes } from "react-router-dom";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { Toaster } from "@/components/ui/sonner";
import AdminRoute from "@/components/AdminRoute";
import AdminLayout from "@/pages/AdminLayout";
import SignIn from "@/pages/SignIn";
import AdminPayments from "@/pages/AdminPayments";
import AdminPaymentApps from "@/pages/AdminPaymentApps";
import AdminPaymentAppDetail from "@/pages/AdminPaymentAppDetail";
import AdminPaymentWithdrawals from "@/pages/AdminPaymentWithdrawals";
import AdminPaymentProviders from "@/pages/AdminPaymentProviders";

const queryClient = new QueryClient();

const App = () => (
  <QueryClientProvider client={queryClient}>
    <>
      <Toaster richColors closeButton position="top-right" />
      <BrowserRouter>
        <Routes>
          <Route path="/signin" element={<SignIn />} />
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
          </Route>
          <Route path="/" element={<Navigate to="/admin/payments" replace />} />
          <Route path="*" element={<Navigate to="/admin/payments" replace />} />
        </Routes>
      </BrowserRouter>
    </>
  </QueryClientProvider>
);

export default App;
