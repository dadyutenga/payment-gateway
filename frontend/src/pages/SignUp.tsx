import { FormEvent, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { authenticate } from "@/lib/auth";
import { requestOTP, verifyOTP } from "@/lib/signupApi";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { Card, CardContent } from "@/components/ui/card";
import { toast } from "@/components/ui/sonner";

type Step = "credentials" | "verify-email";

const SignUp = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const next = searchParams.get("next") || "/onboarding/create-org";

  const [step, setStep] = useState<Step>("credentials");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState(false);

  const handleCredentials = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      await authenticate("register", email, password);
      try {
        await requestOTP({ channel: "email", purpose: "email_verify" });
        toast.success("We sent a verification code to your email.");
      } catch {
        toast.message("Account created. Email delivery is not configured on this deployment — an operator can verify your account.");
      }
      setStep("verify-email");
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to create account.");
    } finally {
      setBusy(false);
    }
  };

  const handleVerify = async (event: FormEvent) => {
    event.preventDefault();
    setBusy(true);
    try {
      await verifyOTP({ channel: "email", purpose: "email_verify", code: code.trim() });
      toast.success("Email verified.");
      navigate(next, { replace: true });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to verify code.");
    } finally {
      setBusy(false);
    }
  };

  const handleSkip = () => navigate(next, { replace: true });

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <Card className="w-full max-w-sm">
        <CardContent className="p-6">
          <h1 className="text-lg font-bold text-slate-900">Create your account</h1>
          <p className="mt-1 text-sm text-slate-500">
            {step === "credentials"
              ? "Merchant signup — email + password. Verification unlocks live payments later."
              : "Enter the 6-digit code we emailed you."}
          </p>

          {step === "credentials" ? (
            <form onSubmit={handleCredentials} className="mt-5 space-y-4">
              <div>
                <label className="text-sm font-medium text-slate-700">Email</label>
                <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus className="mt-1" />
              </div>
              <div>
                <label className="text-sm font-medium text-slate-700">Password</label>
                <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={12} className="mt-1" />
                <p className="mt-1 text-xs text-slate-400">At least 12 characters.</p>
              </div>
              <Button type="submit" className="w-full" disabled={busy}>
                {busy ? "Creating..." : "Create account"}
              </Button>
            </form>
          ) : (
            <form onSubmit={handleVerify} className="mt-5 space-y-4">
              <div>
                <label className="text-sm font-medium text-slate-700">Verification code</label>
                <Input
                  inputMode="numeric"
                  pattern="[0-9]{6}"
                  maxLength={6}
                  value={code}
                  onChange={(e) => setCode(e.target.value.replace(/\D/g, ""))}
                  required
                  autoFocus
                  className="mt-1 tracking-[0.3em]"
                  placeholder="000000"
                />
              </div>
              <Button type="submit" className="w-full" disabled={busy || code.length !== 6}>
                {busy ? "Verifying..." : "Verify email"}
              </Button>
              <button type="button" onClick={handleSkip} className="w-full text-center text-xs text-slate-400 hover:underline">
                Skip for now (sandbox only until verified)
              </button>
            </form>
          )}

          <p className="mt-4 text-center text-xs text-slate-500">
            Already have an account?{" "}
            <Link to="/signin" className="text-blue-600 hover:underline">Sign in</Link>
          </p>
        </CardContent>
      </Card>
    </div>
  );
};

export default SignUp;
