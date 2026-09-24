import { useEffect, useState } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { getAccessToken, signOut } from "@/lib/auth";
import { getAdminMe } from "@/lib/adminApi";

// Any signed-in user may use merchant self-service (admins included) —
// per-app access is enforced server-side via membership, never here.
const MerchantRoute = ({ children }: { children: JSX.Element }) => {
  const [state, setState] = useState<"loading" | "allowed" | "signed-out">("loading");
  const location = useLocation();

  useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        if (!getAccessToken()) {
          if (mounted) setState("signed-out");
          return;
        }
        await getAdminMe();
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
    return <Navigate to={`/signin?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  return children;
};

export default MerchantRoute;
