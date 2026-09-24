import { NavLink, Outlet, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { LogOut, Wallet } from "lucide-react";
import { signOut } from "@/lib/auth";
import { listMyOrgs } from "@/lib/orgApi";

const NAV_ITEMS = [
  { to: "/admin/payments", label: "Overview", end: true },
  { to: "/admin/payments/apps", label: "Apps" },
  { to: "/admin/payments/withdrawals", label: "Withdrawals" },
  { to: "/admin/payments/providers", label: "Providers" },
  { to: "/merchant/apps", label: "Merchant" },
];

function currentOrgId(): string {
  try {
    return localStorage.getItem("payments_gateway_org_id") ?? "";
  } catch {
    return "";
  }
}

const AdminLayout = () => {
  const navigate = useNavigate();
  const orgsQuery = useQuery({ queryKey: ["orgs", "mine"], queryFn: () => listMyOrgs(), staleTime: 60_000, retry: false });
  const orgs = orgsQuery.data ?? [];

  const handleOrgChange = (orgId: string) => {
    try {
      if (orgId) {
        localStorage.setItem("payments_gateway_org_id", orgId);
      } else {
        localStorage.removeItem("payments_gateway_org_id");
      }
    } catch {
      /* ignore */
    }
    if (orgId) {
      navigate(`/org/${orgId}/members`);
    }
  };

  return (
    <div className="min-h-screen bg-slate-50">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between gap-2 px-4 sm:px-6">
          <div className="flex items-center gap-2 font-bold text-slate-900">
            <Wallet className="h-5 w-5" />
            Payments Gateway
          </div>
          <nav className="flex items-center gap-1">
            {orgs.length > 0 && (
              <select
                aria-label="Organization"
                className="mr-1 h-8 max-w-40 truncate rounded-md border border-slate-300 px-1.5 text-xs text-slate-700"
                value={orgs.some((o) => o.id === currentOrgId()) ? currentOrgId() : ""}
                onChange={(e) => handleOrgChange(e.target.value)}
              >
                <option value="">All orgs</option>
                {orgs.map((o) => (
                  <option key={o.id} value={o.id}>{o.name} · {o.role}</option>
                ))}
              </select>
            )}
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
                end={item.end}
                className={({ isActive }) =>
                  `rounded-md px-3 py-1.5 text-sm font-medium transition-colors ${
                    isActive ? "bg-slate-900 text-white" : "text-slate-600 hover:bg-slate-100"
                  }`
                }
              >
                {item.label}
              </NavLink>
            ))}
            <button
              type="button"
              title="Sign out"
              onClick={() => { signOut(); window.location.assign("/signin"); }}
              className="ml-2 flex h-8 w-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100"
            >
              <LogOut className="h-4 w-4" />
            </button>
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6 sm:px-6">
        <Outlet />
      </main>
    </div>
  );
};

export default AdminLayout;
