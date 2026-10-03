import { Link } from "react-router-dom";
import { Wallet, Store, HeartHandshake, ShieldCheck, ArrowRight } from "lucide-react";
import { Button } from "@/components/ui/button";
import { Card, CardContent } from "@/components/ui/card";

// Public landing: brief intro plus the two doors — merchant self-service
// and operator sign-in. No session required, no API calls.
const Welcome = () => {
  return (
    <div className="min-h-screen bg-slate-50">
      <div className="mx-auto max-w-4xl px-4 py-14 sm:px-6">
        <div className="flex items-center gap-2 font-bold text-slate-900">
          <Wallet className="h-6 w-6" />
          <span className="text-xl">LipaGO</span>
        </div>
        <h1 className="mt-6 text-3xl font-extrabold tracking-tight text-slate-900 sm:text-4xl">
          Accept payments through multiple providers, one ledger.
        </h1>
        <p className="mt-3 max-w-2xl text-sm leading-6 text-slate-600 sm:text-base">
          LipaGO lets your app take mobile-money payments, tracks a real
          per-app ledger balance, and pays out via withdrawals. Businesses verify their business (KYC) and individuals verify their identity
          to unlock live payments — sandbox mode works immediately.
        </p>

        <div className="mt-8 grid gap-4 sm:grid-cols-2 lg:grid-cols-3">
          <Card>
            <CardContent className="flex h-full flex-col p-6">
              <div className="flex items-center gap-2">
                <Store className="h-5 w-5 text-emerald-600" />
                <h2 className="text-lg font-bold text-slate-900">Merchants</h2>
                <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-emerald-700">
                  Business
                </span>
              </div>
              <ul className="mt-3 list-disc space-y-1 pl-5 text-sm text-slate-600">
                <li>Create a business account and your organization</li>
                <li>Build apps, webhook endpoints, and API keys</li>
                <li>Test end-to-end in sandbox, then verify to go live</li>
              </ul>
              <div className="mt-5 flex flex-wrap gap-2">
                <Button asChild>
                  <Link to="/merchant/register">Create account <ArrowRight className="h-3.5 w-3.5 ml-1" /></Link>
                </Button>
                <Button variant="outline" asChild>
                  <Link to="/merchant/login">Merchant sign in</Link>
                </Button>
              </div>
              <p className="mt-3 text-xs text-slate-400">
                Already set up? <Link to="/merchant/apps" className="text-blue-600 hover:underline">My apps →</Link>
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="flex h-full flex-col p-6">
              <div className="flex items-center gap-2">
                <HeartHandshake className="h-5 w-5 text-fuchsia-600" />
                <h2 className="text-lg font-bold text-slate-900">Individuals</h2>
                <span className="rounded bg-fuchsia-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-fuchsia-700">
                  Individual
                </span>
              </div>
              <ul className="mt-3 list-disc space-y-1 pl-5 text-sm text-slate-600">
                <li>Create a personal account — no team or business setup</li>
                <li>Receive payments, share a support page, or collect tips</li>
                <li>Verify your identity to unlock live payouts</li>
              </ul>
              <div className="mt-5 flex flex-wrap gap-2">
                <Button asChild>
                  <Link to="/creator/register">Create account <ArrowRight className="h-3.5 w-3.5 ml-1" /></Link>
                </Button>
                <Button variant="outline" asChild>
                  <Link to="/creator/login">Individual sign in</Link>
                </Button>
              </div>
              <p className="mt-3 text-xs text-slate-400">
                Already set up? <Link to="/creator" className="text-fuchsia-700 hover:underline">My workspace →</Link>
              </p>
            </CardContent>
          </Card>

          <Card>
            <CardContent className="flex h-full flex-col p-6">
              <div className="flex items-center gap-2">
                <ShieldCheck className="h-5 w-5 text-red-600" />
                <h2 className="text-lg font-bold text-slate-900">Operators</h2>
                <span className="rounded bg-red-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-red-700">
                  Admin
                </span>
              </div>
              <ul className="mt-3 list-disc space-y-1 pl-5 text-sm text-slate-600">
                <li>Review the KYC queue and verify organizations</li>
                <li>Oversight across every app, order, and withdrawal</li>
                <li>Record payouts and manage providers</li>
              </ul>
              <div className="mt-5 flex flex-wrap gap-2">
                <Button asChild>
                  <Link to="/admin/login">Operator sign in <ArrowRight className="h-3.5 w-3.5 ml-1" /></Link>
                </Button>
                <Button variant="outline" asChild>
                  <Link to="/admin">Admin overview</Link>
                </Button>
              </div>
              <p className="mt-3 text-xs text-slate-400">
                No self-registration — operators are created by an existing admin.
              </p>
            </CardContent>
          </Card>
        </div>

        <p className="mt-8 text-center text-xs text-slate-400">
          Sandbox is free to explore — live payments unlock after verification.
        </p>
      </div>
    </div>
  );
};

export default Welcome;
