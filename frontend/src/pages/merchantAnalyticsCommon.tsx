import { Link, NavLink } from "react-router-dom";
import { Button } from "@/components/ui/button";

export function AnalyticsSubNav({ orgId }: { orgId: string }) {
  const items: { to: string; label: string; end?: boolean }[] = [
    { to: `/org/${orgId}/analytics`, label: "Overview", end: true },
    { to: `/org/${orgId}/analytics/methods`, label: "Methods" },
    { to: `/org/${orgId}/analytics/peak-hours`, label: "Peak hours" },
    { to: `/org/${orgId}/analytics/customers`, label: "Customers" },
    { to: `/org/${orgId}/analytics/failures`, label: "Failures" },
    { to: `/org/${orgId}/settlements`, label: "Settlements" },
  ];
  return (
    <nav className="mt-3 flex flex-wrap items-center gap-1">
      {items.map((item) => (
        <NavLink
          key={item.to}
          to={item.to}
          end={item.end}
          className={({ isActive }) =>
            `rounded-md px-2.5 py-1 text-xs font-medium transition-colors ${
              isActive ? "bg-slate-900 text-white" : "text-slate-600 hover:bg-slate-100"
            }`
          }
        >
          {item.label}
        </NavLink>
      ))}
    </nav>
  );
}

export function EnvToggle({
  env, onChange,
}: {
  env: "live" | "sandbox";
  onChange: (env: "live" | "sandbox") => void;
}) {
  return (
    <div className="flex items-center gap-1.5">
      {(["live", "sandbox"] as const).map((e) => (
        <Button key={e} size="sm" variant={env === e ? "default" : "outline"} onClick={() => onChange(e)}>
          {e === "live" ? "Live" : "Sandbox"}
        </Button>
      ))}
      <span className={`rounded px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide ${env === "live" ? "bg-emerald-100 text-emerald-700" : "bg-amber-100 text-amber-700"}`}>
        {env === "live" ? "Live" : "Sandbox"}
      </span>
    </div>
  );
}

export function AppFilter({
  apps, value, onChange,
}: {
  apps: { id: string; name: string }[];
  value: string;
  onChange: (appId: string) => void;
}) {
  return (
    <select
      aria-label="Filter by app"
      className="h-8 rounded-md border border-slate-300 px-2 text-sm"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      <option value="">All apps</option>
      {apps.map((a) => (
        <option key={a.id} value={a.id}>{a.name}</option>
      ))}
    </select>
  );
}

export function SandboxGuide({ orgId }: { orgId: string }) {
  return (
    <div className="mt-4 rounded-lg border border-amber-200 bg-amber-50 px-4 py-6 text-center text-sm text-amber-900">
      <p className="font-semibold">No sandbox payments yet — make a test payment to see your dashboard.</p>
      <p className="mt-1 text-amber-800">
        Create a sandbox API key under <Link to="/merchant/apps" className="underline">My Apps</Link>, send a test
        order, and come back. Verification unlocks live payments.
      </p>
      <Link to={`/onboarding/kyc/${orgId}`} className="mt-2 inline-block font-medium text-amber-950 underline">
        Open verification →
      </Link>
    </div>
  );
}
