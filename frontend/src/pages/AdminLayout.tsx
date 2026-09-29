import { NavLink, Outlet } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { LogOut, Wallet } from "lucide-react";
import { signOut } from "@/lib/auth";
import { getAdminMe } from "@/lib/adminApi";

const NAV_ITEMS = [
  { to: "/admin", label: "Home", end: true },
  { to: "/admin/analytics", label: "Analytics" },
  { to: "/admin/analytics/providers", label: "Provider stats" },
  { to: "/admin/analytics/merchants", label: "Merchants" },
  { to: "/admin/analytics/failures", label: "Failures" },
  { to: "/admin/ops", label: "Ops" },
  { to: "/admin/payments", label: "Payments" },
  { to: "/admin/payments/apps", label: "Apps" },
  { to: "/admin/payments/withdrawals", label: "Withdrawals" },
  { to: "/admin/payments/providers", label: "Pay providers" },
];

const ADMIN_NAV_ITEMS = [
  { to: "/admin/kyc", label: "KYC review" },
];

// Operator-only layout: admin nav, ADMIN identity badge, no org switcher,
// no merchant links, no sandbox banner. Merchant pages live under
// CustomerLayout instead.
const AdminLayout = () => {
  // Admin-only links (KYC review) stay hidden unless the backend confirms
  // the operator session — it re-checks admin on every request regardless.
  const meQuery = useQuery({ queryKey: ["admin", "me"], queryFn: () => getAdminMe(), staleTime: 60_000, retry: false });
  const navItems = [...NAV_ITEMS, ...((meQuery.data?.is_admin ?? false) ? ADMIN_NAV_ITEMS : [])];

  return (
    <div className="min-h-screen bg-slate-50">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between gap-2 px-4 sm:px-6">
          <div className="flex items-center gap-2 font-bold text-slate-900">
            <Wallet className="h-5 w-5" />
            LipaGO
            <span className="rounded bg-red-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-red-700">
              Admin
            </span>
          </div>
          <nav className="flex items-center gap-1">
            {navItems.map((item) => (
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
              onClick={() => { signOut(); window.location.assign("/admin/login"); }}
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
