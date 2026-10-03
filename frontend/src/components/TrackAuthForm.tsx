import { FormEvent, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { authenticateTrack } from "@/lib/auth";
import {
  clearPendingNames,
  setPendingBusinessName,
  setPendingDisplayName,
  setTrackIntent,
} from "@/lib/trackIntent";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";
import { requestOTP } from "@/lib/signupApi";
import type { AccountKind } from "@/lib/orgApi";

const COPY: Record<AccountKind, { loginTitle: string; loginSub: string; registerTitle: string; registerSub: string }> = {
  merchant: {
    loginTitle: "Business sign in",
    loginSub: "Access your merchant workspace — apps, payments, team, settlements.",
    registerTitle: "Create your business account",
    registerSub: "For companies and organizations. Team members, API keys, business verification (TIN).",
  },
  creator: {
    loginTitle: "Individual sign in",
    loginSub: "Access your personal workspace — receive payments, support page, and payouts.",
    registerTitle: "Create your personal account",
    registerSub: "For any individual. Receive payments, share a support page, or collect tips — no business setup required."
  },
};

export default function TrackAuthForm({ kind, mode }: { kind: AccountKind; mode: "login" | "register" }) {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const next = searchParams.get("next") || "";
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [extra, setExtra] = useState("");
  const [handle, setHandle] = useState("");
  const [busy, setBusy] = useState(false);

  const copy = COPY[kind];
  const isCreator = kind === "creator";
  const oppositeLogin = isCreator ? "/merchant/login" : "/creator/login";
  const oppositeRegister = isCreator ? "/merchant/register" : "/creator/register";
  const ownOtherMode = mode === "login" ? (isCreator ? "/creator/register" : "/merchant/register") : isCreator ? "/creator/login" : "/merchant/login";

  const setupPath = kind === "creator" ? "/creator/setup" : "/merchant/setup";

  const handleSubmit = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      await authenticateTrack(kind, mode, email, password, mode === "register" && isCreator
        ? { display_name: extra.trim(), handle: handle.trim().toLowerCase() }
        : undefined);
      setTrackIntent(kind);
      if (mode === "register") {
        if (isCreator) setPendingDisplayName(extra.trim());
        else setPendingBusinessName(extra.trim());
        try {
          await requestOTP({ channel: "email", purpose: "email_verify" });
        } catch {
          /* mailer may be unconfigured in dev — non-fatal */
        }
        clearPendingNamesCheck();
        navigate(next || setupPath, { replace: true });
        return;
      }
      navigate(next || (isCreator ? "/creator" : "/merchant"), { replace: true });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to continue.");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <Card className="w-full max-w-sm">
        <CardContent className="p-6">
          <p className="text-xs font-semibold uppercase tracking-wide text-slate-400">
            {isCreator ? "Individual / Personal" : "Merchant / Business"}
          </p>
          <h1 className="mt-1 text-lg font-bold text-slate-900">{mode === "login" ? copy.loginTitle : copy.registerTitle}</h1>
          <p className="mt-1 text-sm text-slate-500">{mode === "login" ? copy.loginSub : copy.registerSub}</p>

          <form onSubmit={handleSubmit} className="mt-5 space-y-4">
            {mode === "register" &&
              (isCreator ? (
                <>
                  <div>
                    <label className="text-sm font-medium text-slate-700">Display name</label>
                    <Input value={extra} onChange={(e) => setExtra(e.target.value)} required maxLength={100} placeholder="Amina Creates" className="mt-1" />
                  </div>
                  <div>
                    <label className="text-sm font-medium text-slate-700">Handle</label>
                    <Input value={handle} onChange={(e) => setHandle(e.target.value.toLowerCase().replace(/[^a-z0-9._-]/g, ""))} required minLength={3} maxLength={30} placeholder="amina.creates" className="mt-1" />
                    <p className="mt-1 text-xs text-slate-400">Your personal support link. No business setup is required.</p>
                  </div>
                </>
              ) : (
                <div>
                  <label className="text-sm font-medium text-slate-700">Business name</label>
                  <Input value={extra} onChange={(e) => setExtra(e.target.value)} required placeholder="Acme Limited" className="mt-1" />
                  <p className="mt-1 text-xs text-slate-400">Legal or trading name. Used for business verification.</p>
                </div>
              ))}
            <div>
              <label className="text-sm font-medium text-slate-700">Email</label>
              <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus className="mt-1" />
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Password</label>
              <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={12} className="mt-1" />
              {mode === "register" && <p className="mt-1 text-xs text-slate-400">At least 12 characters.</p>}
            </div>
            <Button type="submit" className="w-full" disabled={busy}>
              {busy ? "Please wait..." : mode === "login" ? "Sign in" : "Create account"}
            </Button>
          </form>

          <p className="mt-4 text-center text-xs text-slate-500">
            {mode === "login" ? (
              <>
                New here?{" "}
                <Link to={ownOtherMode + (next ? `?next=${encodeURIComponent(next)}` : "")} className="text-blue-600 hover:underline">
                  Create a {isCreator ? "personal" : "business"} account
                </Link>
              </>
            ) : (
              <>
                Already have an account?{" "}
                <Link to={ownOtherMode + (next ? `?next=${encodeURIComponent(next)}` : "")} className="text-blue-600 hover:underline">
                  Sign in
                </Link>
              </>
            )}
          </p>
          <p className="mt-2 text-center text-xs text-slate-500">
            {isCreator ? (
              <>
                Signing up as a business instead?{" "}
                <Link to={mode === "login" ? oppositeLogin : oppositeRegister} className="text-blue-600 hover:underline">
                  Go to Merchant {mode === "login" ? "sign in" : "signup"} →
                </Link>
              </>
            ) : (
              <>
                Individual account?{" "}
                <Link to={mode === "login" ? oppositeLogin : oppositeRegister} className="text-blue-600 hover:underline">
                  Go to Individual {mode === "login" ? "sign in" : "signup"} →
                </Link>
              </>
            )}
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

function clearPendingNamesCheck() {
  clearPendingNames();
}
