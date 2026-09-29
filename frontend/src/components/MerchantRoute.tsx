import { useEffect, useState } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { getCustomerToken, signOutCustomer } from "@/lib/auth";
import { getCustomerMe } from "@/lib/merchantApi";

const MerchantRoute = ({ children }: { children: JSX.Element }) => {
  const [state, setState] = useState<"loading" | "allowed" | "signed-out">("loading");
  const location = useLocation();

  useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        if (!getCustomerToken()) {
          if (mounted) setState("signed-out");
          return;
        }
        await getCustomerMe();
        if (mounted) setState("allowed");
      } catch {
        if (mounted) setState("signed-out");
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  if (state === "loading") {
    return <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-500">Loading.</div>;
  }
  if (state === "signed-out") {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  return children;
};

export default MerchantRoute;
