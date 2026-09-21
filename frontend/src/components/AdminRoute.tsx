import { useEffect, useState } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { getAccessToken, signOut } from "@/lib/auth";
import { getAdminMe } from "@/lib/adminApi";

type GateState = "loading" | "allowed" | "signed-out" | "not-admin";

// The backend is the source of truth for both the signed session and role.
const AdminRoute = ({ children }: { children: JSX.Element }) => {
  const [state, setState] = useState<GateState>("loading");
  const [email, setEmail] = useState("");
  const location = useLocation();

  useEffect(() => {
    let mounted = true;
    (async () => {
      try {
        if (!getAccessToken()) {
          if (mounted) setState("signed-out");
          return;
        }
        const me = await getAdminMe();
        if (mounted) {
          setEmail(me.email);
          setState(me.is_admin ? "allowed" : "not-admin");
        }
      } catch {
        if (mounted) setState("signed-out");
      }
    })();
    return () => {
      mounted = false;
    };
  }, []);

  if (state === "loading") {
    return <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-500">Loading…</div>;
  }
  if (state === "signed-out") {
    return <Navigate to={`/signin?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  if (state === "not-admin") {
    return (
      <div className="flex min-h-[60vh] flex-col items-center justify-center gap-2 px-4 text-center">
        <h1 className="text-lg font-bold text-slate-900">Not an admin</h1>
        <p className="max-w-sm text-sm text-slate-500">
          {email} does not have an administrator role for this gateway.
        </p>
        <button
          type="button"
          className="mt-2 text-sm font-medium text-blue-600 hover:underline"
          onClick={() => { signOut(); window.location.assign("/signin"); }}
        >
          Sign out
        </button>
      </div>
    );
  }
  return children;
};

export default AdminRoute;
