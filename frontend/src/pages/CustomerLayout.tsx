import { NavLink, Outlet } from "react-router-dom";
import { LogOut, Wallet } from "lucide-react";
import { signOut } from "@/lib/auth";
import SandboxModeBanner from "@/components/SandboxModeBanner";

const NAV_ITEMS = [
  { to: "/merchant/apps", label: "My Apps" },
  { to: "/merchant/payments", label: "Payments" },
  { to: "/merchant/withdrawals", label: "Withdrawals" },
  { to: "/merchant/webhooks", label: "Webhooks" },
  { to: "/merchant/api-keys", label: "API Keys" },
  { to: "/merchant/deliveries", label: "Deliveries" },
  { to: "/merchant/settings", label: "Settings" },
];

// Merchant (customer) space layout: one org per account, so no org
// switcher — merchant nav plus the sandbox/KYC banner. Never renders admin
// links — operators use AdminLayout instead.
const CustomerLayout = () => {
  return (
    <div className="min-h-screen bg-slate-50">
      <header className="border-b border-slate-200 bg-white">
        <div className="mx-auto flex h-14 max-w-6xl items-center justify-between gap-2 px-4 sm:px-6">
          <div className="flex items-center gap-2 font-bold text-slate-900">
            <Wallet className="h-5 w-5" />
            LipaGO
            <span className="rounded bg-emerald-100 px-1.5 py-0.5 text-[10px] font-bold uppercase tracking-wide text-emerald-700">
              Merchant
            </span>
          </div>
          <nav className="flex items-center gap-1">
            {NAV_ITEMS.map((item) => (
              <NavLink
                key={item.to}
                to={item.to}
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
              onClick={() => { signOut(); window.location.assign("/login"); }}
              className="ml-2 flex h-8 w-8 items-center justify-center rounded-md text-slate-500 hover:bg-slate-100"
            >
              <LogOut className="h-4 w-4" />
            </button>
          </nav>
        </div>
      </header>
      <main className="mx-auto max-w-6xl px-4 py-6 sm:px-6">
        <SandboxModeBanner />
        <Outlet />
      </main>
    </div>
  );
};

export default CustomerLayout;
