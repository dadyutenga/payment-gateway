import { FormEvent, useState } from "react";
import { Link, useNavigate, useSearchParams } from "react-router-dom";
import { authenticate, authenticateAdmin } from "@/lib/auth";
import { Button } from "@/components/ui/button";
import { Input } from "@/components/ui/input";
import { toast } from "@/components/ui/sonner";

// Customer space: /login (signin alias kept). Admin space: /admin/login
// (separate login path + token audience, no self-registration).
const SignIn = ({ admin = false }: { admin?: boolean }) => {
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const next = searchParams.get("next") || (admin ? "/admin/payments" : "/merchant/apps");

  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [submitting, setSubmitting] = useState(false);
  const [registering, setRegistering] = useState(false);

  const handleSignIn = async (event: FormEvent) => {
    event.preventDefault();
    setSubmitting(true);
    try {
      if (admin) {
        await authenticateAdmin(email, password);
      } else {
        await authenticate(registering ? "register" : "login", email, password);
      }
      navigate(next, { replace: true });
    } catch (err) {
      toast.error(err instanceof Error ? err.message : "Unable to sign in.");
    } finally {
      setSubmitting(false);
    }
  };

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <div className="w-full max-w-sm rounded-md border border-slate-200 bg-white p-6">
          <h1 className="text-lg font-bold text-slate-900">{admin ? "LipaGO Admin" : "LipaGO"}</h1>
          {admin && (
            <p className="mt-1 text-xs text-slate-500">Operator sign-in — no self-registration. Ask an existing admin for access.</p>
          )}

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
        {!admin && (
          <button type="button" className="mt-4 rounded-sm text-xs text-blue-600 hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-600" onClick={() => setRegistering(!registering)}>
            {registering ? "Already have an account? Sign in" : "Create an account"}
          </button>
        )}
        {!admin && !registering && (
          <p className="mt-2 text-center text-xs text-slate-500">
            New here? <Link to="/register" className="text-blue-600 hover:underline">Sign up</Link>
          </p>
        )}
      </div>
    </div>
  );
};

export default SignIn;
