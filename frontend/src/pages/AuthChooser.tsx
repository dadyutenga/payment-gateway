import { Link, useSearchParams } from "react-router-dom";
import { Card, CardContent } from "@/components/ui/card";
import { Building2, HeartHandshake } from "lucide-react";

export default function AuthChooser({ mode }: { mode: "login" | "register" }) {
  const [searchParams] = useSearchParams();
  const next = searchParams.get("next") || "";
  const suffix = next ? `?next=${encodeURIComponent(next)}` : "";
  const title = mode === "login" ? "Sign in to LipaGO" : "Create your LipaGO account";

  return (
    <div className="flex min-h-screen items-center justify-center bg-slate-50 px-4">
      <Card className="w-full max-w-lg">
        <CardContent className="p-6">
          <h1 className="text-lg font-bold text-slate-900">{title}</h1>
          <p className="mt-1 text-sm text-slate-500">Choose your workspace. Business and personal accounts are fully separate — no switching.</p>
          <div className="mt-5 grid gap-3 sm:grid-cols-2">
            <Link to={`/merchant/${mode}${suffix}`} className="rounded-md border border-slate-200 bg-white p-4 transition-colors hover:border-slate-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-blue-600">
              <Building2 className="h-5 w-5 text-slate-700" />
              <p className="mt-2 font-semibold text-slate-900">I&apos;m a business</p>
              <p className="mt-1 text-xs text-slate-500">Merchant workspace: org, team, apps, API keys, business verification (TIN).</p>
              <p className="mt-2 text-xs font-medium text-blue-600">Go to Merchant {mode === "login" ? "sign in" : "signup"} →</p>
            </Link>
            <Link to={`/individual/${mode}${suffix}`} className="rounded-md border border-slate-200 bg-white p-4 transition-colors hover:border-slate-400 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-fuchsia-700">
              <HeartHandshake className="h-5 w-5 text-slate-700" />
              <p className="mt-2 font-semibold text-slate-900">I&apos;m an individual</p>
              <p className="mt-1 text-xs text-slate-500">Personal workspace: receive payments, share a support page, and manage payouts. Individual verification (ID + selfie).</p>
              <p className="mt-2 text-xs font-medium text-blue-600">Go to Individual {mode === "login" ? "sign in" : "signup"} →</p>
            </Link>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
