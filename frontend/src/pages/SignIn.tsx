import { FormEvent, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { authenticate } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";

const SignIn = () => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const next = searchParams.get("next") || "/admin/payments";

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [registering, setRegistering] = useState(false);

  const handleSignIn = async (event: FormEvent) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      await authenticate(registering ? "register" : "login", email, password);
      navigate(next, { replace: true });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to sign in.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <div className="w-full max-w-sm rounded-xl border border-slate-200 bg-white p-6 shadow-sm">
        <h1 className="text-lg font-bold text-slate-900">Payments Gateway Admin</h1>

          <form onSubmit={handleSignIn} className="mt-5 space-y-4">
            <div>
              <label className="text-sm font-medium text-slate-700">Email</label>
              <Input type="email" value={email} onChange={(e) => setEmail(e.target.value)} required autoFocus className="mt-1" />
            </div>
            <div>
              <label className="text-sm font-medium text-slate-700">Password</label>
              <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required minLength={12} className="mt-1" />
            </div>
            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? "Please wait..." : registering ? "Create account" : "Sign in"}
            </Button>
          </form>
        <button type="button" className="mt-4 text-xs text-blue-600 hover:underline" onClick={() => setRegistering(!registering)}>
          {registering ? "Already have an account? Sign in" : "Create an account"}
        </button>
      </div>
    </div>
  );
};

export default SignIn;
