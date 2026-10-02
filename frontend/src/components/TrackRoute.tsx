import { useEffect, useState } from "react";
import { Navigate, useLocation } from "react-router-dom";
import { getCustomerToken } from "@/lib/auth";
import { getCustomerMe } from "@/lib/merchantApi";
import { listMyOrgs, type AccountKind } from "@/lib/orgApi";
import { toast } from "@/components/ui/sonner";

type TrackState = "loading" | "allowed" | "signed-out" | "wrong-kind-no-org";

export type TrackNotice = { message: string };

// TrackRoute guards one workspace track. It verifies the session, then
// resolves account_kind from the server (never trusted from the client):
// - unauthenticated → /login?next=…
// - authenticated but on the wrong track → correct dashboard root with a
//   brief notice (not a 404 — a legitimate user on the wrong track)
// - authenticated with no org yet → onboarding for the requested track
const TrackRoute = ({ kind, children }: { kind: AccountKind; children: JSX.Element }) => {
  const [state, setState] = useState<TrackState>("loading");
  const [actual, setActual] = useState<AccountKind | null>(null);
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
        const orgs = await listMyOrgs();
        const active = orgs.find((o) => o.status === "active") ?? orgs[0] ?? null;
        if (!mounted) return;
        if (!active) {
          setState("wrong-kind-no-org");
          return;
        }
        const actualKind = (active.account_kind ?? "merchant") as AccountKind;
        setActual(actualKind);
        setState(actualKind === kind ? "allowed" : "wrong-kind-no-org");
      } catch {
        if (mounted) setState("signed-out");
      }
    })();
    return () => {
      mounted = false;
    };
  }, [kind]);

  if (state === "loading") {
    return <div className="flex min-h-[60vh] items-center justify-center text-sm text-slate-500">Loading.</div>;
  }
  if (state === "signed-out") {
    return <Navigate to={`/login?next=${encodeURIComponent(location.pathname + location.search)}`} replace />;
  }
  if (state === "wrong-kind-no-org" && actual && actual !== kind) {
    const target = actual === "creator" ? "/creator" : "/merchant";
    const message =
      kind === "creator"
        ? "This is a business account — taking you to the merchant workspace."
        : "This is a creator account — taking you to your creator workspace.";
    return <Navigate to={target} replace state={{ notice: { message } satisfies TrackNotice }} />;
  }
  if (state === "wrong-kind-no-org") {
    // Signed in but no workspace yet — start the right onboarding.
    const target = kind === "creator" ? "/creator/setup" : "/merchant/setup";
    if (location.pathname !== target) {
      return <Navigate to={target} replace />;
    }
  }
  return children;
};

// AuthOnly verifies the session without checking the account track — for
// legacy deep links that resolve the track themselves (settings and
// verification routers). Unauthenticated users go to login.
export const AuthOnly = ({ children }: { children: JSX.Element }) => {
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

// TrackNoticeToast surfaces the redirect notice once after a wrong-track
// redirect. Mount inside each track layout.
export const TrackNoticeToast = () => {
  const location = useLocation();
  useEffect(() => {
    const notice = (location.state as { notice?: TrackNotice } | null)?.notice;
    if (notice?.message) {
      toast.message(notice.message);
      window.history.replaceState({}, "");
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);
  return null;
};

export default TrackRoute;
